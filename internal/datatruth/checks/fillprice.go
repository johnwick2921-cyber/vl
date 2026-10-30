package checks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/internal/datatruth"
	"vl/logger"
	"vl/safe"
)

// ── m447 (release #54) — DATA-TRUTH CHECK "fill_price" ───────────────────────
//
// OWNER PRINCIPLE (2026-10-29): "nothing related to data loss or incorrect data
// can continue to trade."
//
// A fill is a print that really happened at NT8, so its price must lie inside
// the market the bot's own bars saw at that moment. A fill far OUTSIDE the stored
// 1m bars of its minute means one of two things: the bar data lost the print (the
// price data the bot decides from is wrong), or the fill price the bot recorded is
// wrong (a mis-parsed frame, a stale price, the wrong contract). Both are
// "incorrect data", and either one poisons stops, targets and P&L.
//
// WHAT IT DOES. Every entry and exit fill the bot records is judged once:
//
//   - reference = the 1m bars of the fill minute, plus FwdMin bars after it and
//     BackMin before it (a fill's timestamp is the frame's, and an exit frame
//     arrives a beat after the print), high/low +- TolTicks. Measured on the
//     last 30 days of real fills (see the test) the real worst case outside the
//     fill-minute bar alone was 1.0 point, and ZERO broker fills (trader_fills
//     exits, trader_positions entries) were outside the window band. The one
//     source that does fire is the price the bot RECORDED on a closed position
//     (trader_positions.exit_price): 2 of 95 in 30 days (positions 635, 639),
//     both genuinely wrong. The tolerance is a margin over measured reality.
//   - when no bar of that minute exists yet the fill WAITS (the forming bar is
//     not in the ring until it closes) and is judged when it arrives; when a hole
//     means the bar never will, the order's own price +- SlipTicks (or the AddOn's
//     reported slippage) is the reference; with neither, it is counted UNJUDGED -
//     a missing bar is the bar-gap checks' episode, not a second one here.
//   - a far fill raises ONE alert and a counter.
//   - the SECOND far fill EVENT of the same CME session on the same ACCOUNT pauses
//     NEW ENTRIES of the traders on that account until the owner acknowledges
//     (Ack) or the next session starts; a far fill AFTER an Ack pauses again. It
//     is a pause on entries only: the exit, stop, trail, close, protection and
//     flatten paths never consult it.
//   - ONE strike per fill EVENT, not per frame (m451): a split bracket sends one
//     position_close frame per leg and an entry sends a fill frame per partial,
//     each carrying the same bad print; the caller names the event (FillObs.Event:
//     the account's signal, and for an exit its seq) and a second frame of an event
//     already counted is logged, not struck, not alerted. The first far print never
//     pauses on its own, whatever number of frames it arrives in.
//   - the strikes are PER ACCOUNT (m451): a far fill recorded on Sim101 is no
//     evidence about SimAccount1's fills, so it pauses the traders bound to
//     Sim101 only. A fill that names no account strikes the symbol for everyone.
//   - the strikes and the owner's acknowledgement survive a restart (m451,
//     EnableState): a deploy is a kill -9 and a standing pause the owner has not
//     acknowledged must not vanish with the process.
//   - an UNREADABLE state file is itself a block (m55r3): when the guard cannot
//     read what a standing pause was, it cannot tell that none was standing, so
//     the symbol's NEW entries (every account) stay paused until the owner
//     acknowledges. The unreadable file is MOVED ASIDE (state-path + ".bad-<utc>",
//     never deleted, never overwritten) and the block is itself persisted, so a
//     second restart cannot lift it.
//   - persistence is OFF the exit path (m55r3): a strike snapshot is queued to a
//     single ordered, coalescing writer; the fill handlers (exits included) never
//     wait on disk.
//   - a fill the guard CANNOT JUDGE because its own record is malformed (a price
//     <= 0, no fill time, no id) is itself a wrong recorded price: it is counted
//     as a far fill and logged (m451), never silently dropped.
//
// It is NOT a slippage monitor. A stop-market that fills 20 points through its
// stop in a fast market is a real market move: the print is inside the bar band,
// so it is never far here.

// CheckFillPrice is the registered check name.
const CheckFillPrice = "fill_price"

// FillParams are the thresholds.
type FillParams struct {
	// TolTicks is how far outside the bar band a fill may sit (default 12 ticks
	// = 3 points on MNQ/ES).
	TolTicks float64
	// BackMin / FwdMin: 1m bars before / after the fill minute in the band
	// (defaults 2 / 1).
	BackMin, FwdMin int
	// SlipTicks is the order-price fallback tolerance when no bar exists
	// (default 40 ticks = 10 points).
	SlipTicks float64
	// Strikes far fills in one session pause the symbol's entries (default 2).
	Strikes int
	// PendingMax: a fill still not judgeable this long after it happened is
	// counted UNJUDGED and dropped (default 10 minutes).
	PendingMax time.Duration
}

// DefaultFillParams returns the defaults documented on FillParams.
func DefaultFillParams() FillParams {
	return FillParams{TolTicks: 12, BackMin: 2, FwdMin: 1, SlipTicks: 40, Strikes: 2, PendingMax: 10 * time.Minute}
}

func (p FillParams) norm() FillParams {
	d := DefaultFillParams()
	if p.TolTicks <= 0 {
		p.TolTicks = d.TolTicks
	}
	if p.BackMin < 0 {
		p.BackMin = d.BackMin
	}
	if p.FwdMin < 0 {
		p.FwdMin = d.FwdMin
	}
	if p.SlipTicks <= 0 {
		p.SlipTicks = d.SlipTicks
	}
	if p.Strikes <= 0 {
		p.Strikes = d.Strikes
	}
	if p.PendingMax <= 0 {
		p.PendingMax = d.PendingMax
	}
	return p
}

// FillObs is one fill as the fill handlers see it.
type FillObs struct {
	ID     string // stable per fill: a replay of the same frame must be the same ID
	Symbol string
	Kind   string // "entry" | "exit"
	Side   string
	Price  float64
	AtMs   int64   // when it printed (ms UTC)
	Tick   float64 // the symbol's tick size
	// Account is the NT8 account the fill printed on ("" = unknown: the strike then
	// counts for every trader on the symbol, the fail-closed reading).
	Account string
	// Event names the real-world fill this frame belongs to (m451): every frame of
	// one event carries the same value, so the legs of a split bracket and the
	// partials of one entry are ONE strike. "" = each ID is its own event.
	Event string
	// OrderPx is the order's own price (0 = unknown). SlipTicks / HasSlip carry
	// the AddOn's reported slippage against the order when it sent one.
	OrderPx   float64
	SlipTicks float64
	HasSlip   bool

	bad string // set by Record: why the fill's own record cannot be judged
}

// FillVerdict is one fill's judgment.
type FillVerdict struct {
	Judged bool   // false = could not judge yet / at all
	Far    bool   // only meaningful when Judged
	Detail string // quotes the numbers
}

// JudgeFill is the pure judgment. bars is the 1m ring, ascending.
func JudgeFill(o FillObs, bars []Bar, p FillParams) FillVerdict {
	p = p.norm()
	tick := o.Tick
	if tick <= 0 {
		tick = 0.25
	}
	m := o.AtMs / minuteMs * minuteMs
	lo, hi := m-int64(p.BackMin)*minuteMs, m+int64(p.FwdMin)*minuteMs
	var bandLo, bandHi float64
	n := 0
	haveFillMinute := false
	var newest int64 // newest bar of ANY kind at or after the window start
	fillBarFinal := false
	for _, b := range tailFrom(bars, lo) {
		if b.T > newest {
			newest = b.T
		}
		if b.T > hi {
			continue
		}
		if b.H <= 0 || b.L <= 0 {
			continue
		}
		if n == 0 || b.L < bandLo {
			bandLo = b.L
		}
		if n == 0 || b.H > bandHi {
			bandHi = b.H
		}
		n++
		if b.T == m {
			haveFillMinute = true
			fillBarFinal = b.Final
		}
	}
	// A bar still FORMING carries only the range so far: a print milliseconds ago
	// may not be in it yet. It is judged once the minute has closed (the bar is
	// final, or a later bar exists).
	if haveFillMinute && !fillBarFinal && newest <= m {
		haveFillMinute = false
	}
	if haveFillMinute {
		tol := p.TolTicks * tick
		switch {
		case o.Price < bandLo-tol:
			return FillVerdict{true, true, fmt.Sprintf("%s fill %.2f is %.2f below the lowest low %.2f of the %d stored 1m bar(s) around its minute (tolerance %.2f)", o.Kind, o.Price, bandLo-o.Price, bandLo, n, tol)}
		case o.Price > bandHi+tol:
			return FillVerdict{true, true, fmt.Sprintf("%s fill %.2f is %.2f above the highest high %.2f of the %d stored 1m bar(s) around its minute (tolerance %.2f)", o.Kind, o.Price, o.Price-bandHi, bandHi, n, tol)}
		}
		return FillVerdict{true, false, fmt.Sprintf("%s fill %.2f inside the 1m band %.2f..%.2f", o.Kind, o.Price, bandLo, bandHi)}
	}
	// No bar for the fill minute. If a LATER bar exists the minute is a hole that
	// will not fill in on its own: fall back to the order's own price.
	if newest > m {
		if o.OrderPx > 0 {
			if d := absf(o.Price - o.OrderPx); d > p.SlipTicks*tick {
				return FillVerdict{true, true, fmt.Sprintf("%s fill %.2f is %.2f from its order price %.2f (limit %.2f) and the fill minute has no stored 1m bar to confirm it", o.Kind, o.Price, d, o.OrderPx, p.SlipTicks*tick)}
			}
			return FillVerdict{true, false, fmt.Sprintf("%s fill %.2f within %.2f of its order price %.2f (no 1m bar for the minute)", o.Kind, o.Price, p.SlipTicks*tick, o.OrderPx)}
		}
		if o.HasSlip {
			if absf(o.SlipTicks) > p.SlipTicks {
				return FillVerdict{true, true, fmt.Sprintf("%s fill %.2f reports %.1f ticks of slippage (limit %.0f) and the fill minute has no stored 1m bar to confirm it", o.Kind, o.Price, o.SlipTicks, p.SlipTicks)}
			}
			return FillVerdict{true, false, fmt.Sprintf("%s fill %.2f reports %.1f ticks of slippage (no 1m bar for the minute)", o.Kind, o.Price, o.SlipTicks)}
		}
		return FillVerdict{false, false, "no stored 1m bar for the fill minute and no order price to fall back on"}
	}
	return FillVerdict{false, false, "the fill minute's 1m bar has not been stored yet"}
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// RingFn reads the symbol's 1m ring (the live cache, as decisions read it).
type RingFn func(symbol string) ([]Bar, error)

type strikeState struct {
	session string
	account string // upper-cased; "" = the fill named no account (strikes everyone on the symbol)
	lastMs  int64  // newest fill time (ms) the state holds: orders sessions without parsing their keys
	ids     []string
	events  []string // parallel to ids: the fill event each strike counted (m451)
	details []string
	acked   bool
}

type pendingFill struct {
	o    FillObs
	ring RingFn
}

// FillStats are the counters (recorded, never inferred).
type FillStats struct {
	Judged   int64 `json:"judged"`
	Far      int64 `json:"far"`
	Unjudged int64 `json:"unjudged"`
	Pending  int   `json:"pending"`
	// FarRepeat counts far FRAMES of a fill event that was already struck (the
	// other legs / partials of one print): recorded, not a new strike (m451).
	FarRepeat int64 `json:"far_repeat"`
	// Malformed counts fills whose own record could not be judged (price <= 0, no
	// time, no id); each was counted as a far fill (m451).
	Malformed int64 `json:"malformed"`
}

// StrikeKey is the ONE canonical key of the per-symbol strike map, applied where
// a symbol ENTERS it (apply), where the pause is read (Check) and where the owner
// lifts it (Ack): the instrument root, trimmed and upper-cased, so a fill frame
// that says "mnq" or "MNQ 12-26" strikes the same entry the gate and the
// acknowledgement read as "MNQ" (m447 review N2 - canon rule: one canonicalizer
// per identifier, called where the value enters).
func StrikeKey(symbol string) string {
	f := strings.Fields(strings.ToUpper(strings.TrimSpace(symbol)))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// FillGuard judges fills, counts far ones per session and exposes the check.
type FillGuard struct {
	mu      sync.Mutex
	p       FillParams
	alert   func(kind, text string)
	session func(time.Time) string
	seen    map[string]bool
	pending []pendingFill
	strikes map[string]*strikeState // by symbol + account (strikeMapKey)
	stats   FillStats

	// persistence (m451): "" = memory only
	statePath string
	stateSeq  int64 // guarded by mu: bumped at every snapshot
	// unreadable (m55r3): non-nil when the state file existed but could not be
	// read at EnableState - a block for every symbol until the owner acks it.
	unreadable *unreadableState
	// noPersist: the unreadable file could not be moved aside, so nothing may be
	// written over it (the block then lives in memory only).
	noPersist bool

	// The asynchronous writer (m55r3). persist() only queues; one drain goroutine
	// at a time writes the newest snapshot, so an older snapshot never lands after
	// a newer one and a burst of strikes costs one write.
	wmu      sync.Mutex
	wpend    *fillSnap
	wrunning bool
	widle    []chan struct{} // closed when the writer next goes idle (Flush)
	fileSeq  int64           // drain only: newest snapshot written
}

// unreadableState is the standing block raised by an unreadable state file.
type unreadableState struct {
	Since   int64           // ms UTC
	Why     string          // the read/parse error
	BadFile string          // where the unreadable file was moved ("" = it could not be moved)
	Acked   map[string]bool // StrikeKey(symbol) -> the owner acknowledged it for that symbol
}

type fillSnap struct {
	path string
	b    []byte
	seq  int64
}

// strikeMapKey is the key of the strike map: the canonical symbol and the
// upper-cased account.
func strikeMapKey(symbol, account string) string {
	return StrikeKey(symbol) + "|" + strings.ToUpper(strings.TrimSpace(account))
}

// NewFillGuard builds a guard. session maps an instant to its CME session key
// (kernel.CMESessionDayKey in production); nil keys everything to one session.
func NewFillGuard(p FillParams, session func(time.Time) string) *FillGuard {
	if session == nil {
		session = func(time.Time) string { return "" }
	}
	return &FillGuard{p: p.norm(), session: session, seen: map[string]bool{}, strikes: map[string]*strikeState{}}
}

// defaultFills is the process-wide guard the fill handlers record into. It is
// inert until the wiring gives it a session function and an alert sender. It is
// an atomic pointer, not a bare variable: a test swaps it while goroutines
// leaked by earlier tests (a TCPTrader's fill consumer) may still read it, and a
// bare package variable made that a DATA RACE (m447 review defect 4).
var defaultFills atomic.Pointer[FillGuard]

func init() { defaultFills.Store(NewFillGuard(DefaultFillParams(), nil)) }

// DefaultFills returns the process-wide guard.
func DefaultFills() *FillGuard { return defaultFills.Load() }

// SetDefaultFills installs g as the process-wide guard and returns the previous
// one (so a test restores it with SetDefaultFills(prev)).
func SetDefaultFills(g *FillGuard) *FillGuard { return defaultFills.Swap(g) }

// SetAlert installs the one-alert-per-far-fill sender (ownerpush in production).
func (g *FillGuard) SetAlert(fn func(kind, text string)) {
	g.mu.Lock()
	g.alert = fn
	g.mu.Unlock()
}

// SetSession installs the session-key function.
func (g *FillGuard) SetSession(fn func(time.Time) string) {
	if fn == nil {
		return
	}
	g.mu.Lock()
	g.session = fn
	g.mu.Unlock()
}

// Record is the fill-handler hook: judge o now if the bars allow, else keep it
// until they do. A replay of the same ID is ignored. Never blocks on I/O beyond
// the ring read; never returns an error to the caller (a fill handler must not
// fail because the check did).
func (g *FillGuard) Record(o FillObs, ring RingFn, now time.Time) {
	// A fill whose own record cannot be judged is a wrong recorded price: it is
	// COUNTED as a far fill and logged, never silently dropped (m451). It is
	// attributed to the session it arrived in (it has no usable time).
	switch {
	case o.Price <= 0:
		o.bad = fmt.Sprintf("%s fill price %.4f is not a price (a mis-parsed frame or a zeroed field) and cannot be judged against the market", o.Kind, o.Price)
	case o.AtMs <= 0:
		o.bad = fmt.Sprintf("%s fill %.2f has no usable fill time (%d) and cannot be placed in a market minute to be judged", o.Kind, o.Price, o.AtMs)
	case o.ID == "":
		o.bad = fmt.Sprintf("%s fill %.2f arrived with no fill id and cannot be tracked or deduplicated", o.Kind, o.Price)
	}
	if o.bad != "" {
		if o.AtMs <= 0 {
			o.AtMs = now.UnixMilli()
		}
		if o.ID == "" {
			o.ID = fmt.Sprintf("malformed|%s|%s|%s|%.4f|%d", o.Account, o.Symbol, o.Side, o.Price, o.AtMs)
		}
		g.mu.Lock()
		g.stats.Malformed++
		g.mu.Unlock()
		logger.Warnf("datatruth: fill_price cannot judge a malformed fill record - counted as a FAR fill (fail closed): %s [fill id %s, account %q]", o.bad, o.ID, o.Account)
	}
	g.mu.Lock()
	if g.seen[o.ID] {
		g.mu.Unlock()
		return
	}
	g.seen[o.ID] = true
	if len(g.seen) > 4096 { // bounded: ids are only for same-session replays
		for k := range g.seen {
			delete(g.seen, k)
			if len(g.seen) < 2048 {
				break
			}
		}
		g.seen[o.ID] = true
	}
	g.mu.Unlock()
	if o.bad != "" {
		g.apply(o, FillVerdict{Judged: true, Far: true, Detail: o.bad}, now)
		return
	}
	if !g.tryJudge(o, ring, now) {
		g.mu.Lock()
		g.pending = append(g.pending, pendingFill{o, ring})
		g.stats.Pending = len(g.pending)
		g.mu.Unlock()
	}
}

func (g *FillGuard) tryJudge(o FillObs, ring RingFn, now time.Time) bool {
	var bars []Bar
	if ring != nil {
		var err error
		if bars, err = ring(o.Symbol); err != nil {
			bars = nil
		}
	}
	v := JudgeFill(o, bars, g.p)
	if !v.Judged {
		return false
	}
	g.apply(o, v, now)
	return true
}

func (g *FillGuard) apply(o FillObs, v FillVerdict, now time.Time) {
	g.mu.Lock()
	g.stats.Judged++
	if !v.Far {
		g.mu.Unlock()
		return
	}
	sess := g.session(time.UnixMilli(o.AtMs))
	key := strikeMapKey(o.Symbol, o.Account)
	st := g.strikes[key]
	event := o.Event
	if event == "" {
		event = o.ID
	}
	// ONE strike per fill EVENT (m451): another frame of an event already struck in
	// this session (the next leg of a split bracket, the next partial of an entry)
	// is the SAME print. It is counted as a repeat and logged; it neither strikes
	// again nor alerts again.
	if st != nil && st.session == sess {
		for _, e := range st.events {
			if e == event {
				g.stats.FarRepeat++
				g.mu.Unlock()
				logger.Infof("datatruth: fill_price: another frame of fill event %s (%s %s %s %.2f, fill id %s) - already counted as a strike, not a new one", event, o.Symbol, o.Kind, o.Side, o.Price, o.ID)
				return
			}
		}
	}
	g.stats.Far++
	// A far fill that belongs to an OLDER session than the one the state holds (a
	// late or retried exit receipt, a replay after a reconnect or restart) must
	// not touch the current session's strikes: replacing the state would wipe the
	// standing pause and Check would read the new, empty state as clear (m447
	// round 3 D2 - fail-OPEN). Older is decided by the fill's own time, not by
	// comparing session keys. Such a fill is still counted and alerted - it is a
	// real far print - but its session is not the one entries trade in.
	late := st != nil && st.session != sess && o.AtMs < st.lastMs
	n, strikes := 0, g.p.Strikes
	if !late {
		if st == nil || st.session != sess {
			st = &strikeState{session: sess, account: strings.ToUpper(strings.TrimSpace(o.Account))}
			g.strikes[key] = st
		}
		// An owner acknowledgement spends the strikes seen so far, not the future: a
		// far fill AFTER the ack is "the next far fill" the contract promises to pause
		// on (m447 review defect 2 - acked was never cleared, so no later far fill
		// ever paused again).
		st.acked = false
		if o.AtMs > st.lastMs {
			st.lastMs = o.AtMs
		}
		st.ids = append(st.ids, o.ID)
		st.events = append(st.events, event)
		st.details = append(st.details, v.Detail)
		n = len(st.ids)
	}
	alert := g.alert
	g.mu.Unlock()
	if !late {
		g.persist()
	}

	who := ""
	if strings.TrimSpace(o.Account) != "" {
		who = " on account " + strings.TrimSpace(o.Account)
	}
	var text string
	if late {
		text = fmt.Sprintf("DATA TRUTH: FILL PRICE FAR from the market - %s %s %s%s: %s. This fill belongs to an EARLIER session than the one now trading (a late or replayed receipt), so it does not count toward this session's pause; the recorded price is still wrong - check the bars or the fill record.",
			o.Symbol, o.Kind, o.Side, who, v.Detail)
	} else {
		text = fmt.Sprintf("DATA TRUTH: FILL PRICE FAR from the market - %s %s %s%s: %s. Either the bars lost this print or the recorded fill price is wrong. %s",
			o.Symbol, o.Kind, o.Side, who, v.Detail, farCountNote(o.Symbol, who, o.Account, n, strikes))
	}
	logger.Warnf("datatruth: %s [fill id %s]", text, o.ID)
	if alert != nil {
		alert(fmt.Sprintf("data_truth_fill_price_far|%s|%s", o.Symbol, o.ID), text)
	}
}

// farCountNote words where the far-fill count stands. Up to the limit it is "n of
// N (the Nth pauses ...)"; PAST it (n > N: a far fill after an acknowledgement
// clears the ack, so the pause stands again at once) it must not read "3 of 2 (the
// 2nd pauses ...)" - the pause is already on (release #55 round 3, review 4 D3).
func farCountNote(symbol, who, account string, n, strikes int) string {
	if n > strikes {
		return fmt.Sprintf("Far fill events this session for %s%s: %d, past the limit of %d - new entries%s are PAUSED (again, if you had acknowledged) until you acknowledge or the next session; any further far fill pauses again.",
			symbol, who, n, strikes, forWho(account))
	}
	return fmt.Sprintf("Far fill events this session for %s%s: %d of %d (the %s pauses new entries%s until you acknowledge or the next session).",
		symbol, who, n, strikes, ordinal(strikes), forWho(account))
}

// forWho words the scope of the pause.
func forWho(account string) string {
	if a := strings.TrimSpace(account); a != "" {
		return " of the traders on account " + a
	}
	return ""
}

func ordinal(n int) string {
	switch n {
	case 1:
		return "1st"
	case 2:
		return "2nd"
	case 3:
		return "3rd"
	}
	return fmt.Sprintf("%dth", n)
}

// Sweep re-judges every waiting fill; those whose bars have arrived are judged,
// those past PendingMax are counted unjudged and dropped.
func (g *FillGuard) Sweep(now time.Time) {
	g.mu.Lock()
	waiting := g.pending
	g.pending = nil
	g.mu.Unlock()
	var keep []pendingFill
	var dropped []FillObs
	for _, pf := range waiting {
		if g.tryJudge(pf.o, pf.ring, now) {
			continue
		}
		if now.Sub(time.UnixMilli(pf.o.AtMs)) > g.p.PendingMax {
			dropped = append(dropped, pf.o)
			continue
		}
		keep = append(keep, pf)
	}
	g.mu.Lock()
	g.pending = append(keep, g.pending...)
	g.stats.Pending = len(g.pending)
	g.stats.Unjudged += int64(len(dropped))
	g.mu.Unlock()
	for _, o := range dropped {
		logger.Warnf("datatruth: fill_price could not judge %s %s %s @ %.2f (id %s): no stored 1m bar for its minute after %s - counted UNJUDGED (a missing bar is the bar-gap checks' episode)",
			o.Symbol, o.Kind, o.Side, o.Price, o.ID, g.p.PendingMax)
	}
}

// Ack is the owner acknowledging the symbol's far fills on EVERY account:
// entries resume until the next far fill. Returns whether a pause was standing.
func (g *FillGuard) Ack(symbol string) bool { return g.ack(symbol, "", false) }

// AckAccount acknowledges one account's far fills only (m451): the pause is per
// account, so its remedy is too.
func (g *FillGuard) AckAccount(symbol, account string) bool { return g.ack(symbol, account, true) }

func (g *FillGuard) ack(symbol, account string, only bool) bool {
	g.mu.Lock()
	prefix := StrikeKey(symbol) + "|"
	want := strings.ToUpper(strings.TrimSpace(account))
	any := false
	// The unreadable-state block is symbol-wide: only the account-less ack lifts it.
	if u := g.unreadable; u != nil && !only {
		if k := StrikeKey(symbol); !u.Acked[k] {
			u.Acked[k] = true
			any = true
		}
	}
	for k, st := range g.strikes {
		if !strings.HasPrefix(k, prefix) || (only && st.account != want) {
			continue
		}
		if st.acked || len(st.ids) < g.p.Strikes {
			continue
		}
		st.acked = true
		any = true
	}
	g.mu.Unlock()
	if any {
		g.persist()
		// An acknowledgement is an owner action, not an exit: wait (bounded) for it
		// to reach the disk so a restart right after cannot lose it.
		g.Flush(2 * time.Second)
	}
	return any
}

// Stats returns the counters.
func (g *FillGuard) Stats() FillStats {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.stats
	s.Pending = len(g.pending)
	return s
}

// Check is the unscoped datatruth.CheckFn: blocked when any account of the symbol
// has Strikes far fill events this session and the owner has not acknowledged. It
// sweeps the waiting fills first, so a pause is never a minute behind the bars
// that prove it. Production registers CheckScoped (who the pause is FOR).
func (g *FillGuard) Check() datatruth.CheckFn {
	sc := g.CheckScoped()
	return func(symbol string, now time.Time) (bool, string, error) {
		b, why, _, err := sc(symbol, now)
		return b, why, err
	}
}

// CheckScoped is the datatruth.ScopedCheckFn: the same verdict plus the accounts
// it is for. A standing state that names no account is for everyone.
func (g *FillGuard) CheckScoped() datatruth.ScopedCheckFn {
	return func(symbol string, now time.Time) (bool, string, []string, error) {
		g.Sweep(now)
		g.mu.Lock()
		defer g.mu.Unlock()
		prefix := StrikeKey(symbol) + "|"
		keys := make([]string, 0, len(g.strikes))
		for k := range g.strikes {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		cur := g.session(now)
		var parts, scopes []string
		everyone := false
		for _, k := range keys {
			st := g.strikes[k]
			if st.session != cur {
				continue // the next session: the strikes are spent
			}
			if len(st.ids) < g.p.Strikes || st.acked {
				continue
			}
			ids := append([]string(nil), st.ids...)
			sort.Strings(ids)
			sym := StrikeKey(symbol)
			who, ackURL := "", "POST "+AckBaseURL()+"/api/risk/ack-fill-price?symbol="+sym
			if st.account != "" {
				who = "account " + st.account + ": "
				ackURL += "&account=" + st.account
				scopes = append(scopes, datatruth.ScopeAccount(st.account))
			} else {
				everyone = true
			}
			parts = append(parts, fmt.Sprintf("%s%d far fill(s) this session (ids %s): %s - acknowledge via %s or wait for the next session",
				who, len(st.ids), strings.Join(ids, ","), strings.Join(dedupeStrings(st.details), " | "), ackURL))
		}
		if u := g.unreadable; u != nil && !u.Acked[StrikeKey(symbol)] {
			everyone = true
			parts = append(parts, u.describe(g.statePath, StrikeKey(symbol)))
		}
		if len(parts) == 0 {
			return false, "", nil, nil
		}
		if everyone {
			scopes = nil
		}
		// The remedy has to be runnable by the owner as written (round 3, review 4
		// D6: a bare relative path names no host and no login). The route is behind
		// the login: there is no button for it.
		parts = append(parts, ackHowTo())
		return true, strings.Join(parts, "; "), scopes, nil
	}
}

// ackBase is the address the bot's API answers on, set at boot from the config
// port (trader.RegisterFillPrice). Unset = the default port on this machine.
var ackBase atomic.Value

// SetAckBaseURL records the scheme://host:port the acknowledgement route is on.
func SetAckBaseURL(base string) { ackBase.Store(strings.TrimRight(strings.TrimSpace(base), "/")) }

// AckBaseURL is what the pause text prints before /api/risk/ack-fill-price.
func AckBaseURL() string {
	if b, _ := ackBase.Load().(string); b != "" {
		return b
	}
	return "http://127.0.0.1:8080"
}

// ackHowTo is the line that makes the acknowledgement URL runnable: the route is
// behind the login and there is no button for it in the web app.
func ackHowTo() string {
	return "to run the acknowledgement: first log in (POST " + AckBaseURL() + "/api/login with your email and password returns a token), then send the POST above with the HTTP header Authorization set to Bearer, a space, then the token - from the machine the bot runs on; the web app has no button for it"
}

// fillResume is the resume tail of the fill_price OWNER PUSH. The push is capped
// (datatruth.PushRunes) and the detail is clipped to fit, so the remedy lives here,
// in the part that is never clipped: the exact login and acknowledgement calls for
// the block that stands, worded so ownerpush.Scrub leaves it intact (no
// "Authorization:" pair, which it redacts as a credential).
func fillResume(symbol string, scopes []string) string {
	base := AckBaseURL()
	url := base + "/api/risk/ack-fill-price?symbol=" + StrikeKey(symbol)
	var accts []string
	for _, sc := range scopes {
		if a := strings.TrimPrefix(sc, "acct:"); a != sc && a != "" {
			accts = append(accts, a)
		}
	}
	switch len(accts) {
	case 0:
	case 1:
		url += "&account=" + accts[0]
	default:
		url += "&account=<one of " + strings.Join(accts, "/") + ">, once per account"
	}
	return "entries resume when you acknowledge it (on the bot's machine: POST " + base + "/api/login with your email and password returns a token, then POST " + url + " with the HTTP header Authorization set to Bearer, a space, the token; the web app has no button) or when the next CME session starts - not by themselves"
}

// ── persistence (m451) ───────────────────────────────────────────────────────

const fillStateVersion = 1

// fillStateMaxAge: a strike state whose newest fill is older than this is dropped
// from the file (a CME session is a day; the weekend is the longest gap).
const fillStateMaxAge = 96 * time.Hour

type fillStateStrike struct {
	Account string   `json:"account"`
	Session string   `json:"session"`
	LastMs  int64    `json:"last_ms"`
	IDs     []string `json:"ids"`
	Events  []string `json:"events"`
	Details []string `json:"details"`
	Acked   bool     `json:"acked"`
}

type fillStateUnreadable struct {
	Since   int64    `json:"since"`
	Why     string   `json:"why"`
	BadFile string   `json:"bad_file"`
	Acked   []string `json:"acked,omitempty"`
}

type fillStateDisk struct {
	Version int                        `json:"version"`
	SavedAt int64                      `json:"saved_at"`
	Strikes map[string]fillStateStrike `json:"strikes"`
	// Unreadable (m55r3) carries the unreadable-state block across a restart: the
	// next boot reads a perfectly good (fresh) file, and must still know that the
	// owner has not acknowledged the loss of the one before it.
	Unreadable *fillStateUnreadable `json:"unreadable,omitempty"`
}

// describe words the unreadable-state block for the pause text (symbol is the
// canonical StrikeKey the ack URL names).
func (u *unreadableState) describe(path, symbol string) string {
	moved := "it could NOT be moved aside, so nothing is written over it and the block lives in memory only"
	if u.BadFile != "" {
		moved = "the unreadable file was moved aside, not deleted, to " + u.BadFile
	}
	return fmt.Sprintf("the fill_price state file %s was UNREADABLE (%s) at the last restart, so the bot cannot tell whether a far-fill pause was standing - %s; new entries stay paused for every account until you check the far fills and acknowledge via POST /api/risk/ack-fill-price?symbol=%s (or restore a repaired copy of the state file at that path before the next restart)",
		path, u.Why, moved, symbol)
}

// EnableState loads path (when it exists) into the strike map and from then on
// rewrites it, atomically and off the caller's path, at every strike and every
// acknowledgement - the way the owner-push held file survives a kill -9 deploy.
// Returns how many standing strike states it restored.
//
// An UNREADABLE file (a read error, bad JSON, an unknown version) fails CLOSED
// (m55r3): the guard cannot tell whether a pause was standing, so it installs a
// block for every symbol (until the owner acknowledges), tells the owner, and
// MOVES the file aside to path + ".bad-<utc>" before anything is written - the
// evidence is never overwritten and never deleted. If it cannot be moved,
// nothing is ever written over it (the block then lives in memory only).
func (g *FillGuard) EnableState(path string) (restored int, err error) {
	if path == "" {
		return 0, nil
	}
	b, rerr := os.ReadFile(path)
	var d fillStateDisk
	switch {
	case os.IsNotExist(rerr):
	case rerr != nil:
		err = rerr
	default:
		if jerr := json.Unmarshal(b, &d); jerr != nil {
			err = jerr
			d = fillStateDisk{}
		} else if d.Version != fillStateVersion {
			err = fmt.Errorf("state version %d, want %d", d.Version, fillStateVersion)
			d = fillStateDisk{}
		}
	}
	var u *unreadableState
	movedTo := ""
	if err != nil {
		u = &unreadableState{Since: time.Now().UnixMilli(), Why: err.Error(), Acked: map[string]bool{}}
		bad, merr := moveAside(path, time.Now())
		if merr != nil {
			movedTo = fmt.Sprintf("it could NOT be moved aside (%v) and nothing will be written over it", merr)
		} else {
			u.BadFile = bad
		}
		g.mu.Lock()
		g.noPersist = merr != nil
		g.mu.Unlock()
	} else if d.Unreadable != nil {
		u = &unreadableState{Since: d.Unreadable.Since, Why: d.Unreadable.Why, BadFile: d.Unreadable.BadFile, Acked: map[string]bool{}}
		for _, k := range d.Unreadable.Acked {
			u.Acked[k] = true
		}
	}
	g.mu.Lock()
	g.statePath = path
	g.unreadable = u
	cutoff := time.Now().Add(-fillStateMaxAge).UnixMilli()
	for k, s := range d.Strikes {
		if s.LastMs < cutoff || len(s.IDs) == 0 {
			continue
		}
		st := &strikeState{session: s.Session, account: s.Account, lastMs: s.LastMs, ids: s.IDs, events: s.Events, details: s.Details, acked: s.Acked}
		for len(st.events) < len(st.ids) { // a hand-edited file: every strike keeps an event
			st.events = append(st.events, st.ids[len(st.events)])
		}
		g.strikes[k] = st
		for _, id := range st.ids {
			g.seen[id] = true // a replay of a counted fill after the restart is not a new one
		}
		restored++
	}
	alert := g.alert
	g.mu.Unlock()
	if err != nil {
		if u.BadFile != "" {
			movedTo = "the unreadable file was moved aside, not deleted, to " + u.BadFile
		}
		text := fmt.Sprintf("DATA TRUTH: the fill_price state file %s is unreadable (%v) - any standing far-fill strikes or acknowledgement from before this restart are UNKNOWN. NEW entries are PAUSED for every symbol and account until you acknowledge via POST /api/risk/ack-fill-price?symbol=MNQ (once per symbol you trade). Exits, stops and protection are unaffected; %s.",
			path, err, movedTo)
		logger.Errorf("datatruth: %s", text)
		if alert != nil {
			alert("data_truth_fill_price_state|"+path, text)
		}
	} else if restored > 0 {
		logger.Warnf("datatruth: fill_price restored %d standing strike state(s) from %s", restored, path)
	}
	g.persist()
	g.Flush(2 * time.Second) // boot, not an exit: the unreadable record is on disk before we go on
	return restored, err
}

// moveAside renames path to path + ".bad-<utc stamp>" (a suffix is added while
// that name is taken: nothing is ever overwritten). A file that does not exist
// needs no move. Never deletes.
func moveAside(path string, now time.Time) (string, error) {
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	base := path + ".bad-" + now.UTC().Format("20060102T150405Z")
	dst := base
	for i := 1; ; i++ {
		if _, err := os.Lstat(dst); os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", err
		}
		if i > 1000 {
			return "", fmt.Errorf("no free name for %s", base)
		}
		dst = fmt.Sprintf("%s-%d", base, i)
	}
	if err := os.Rename(path, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// persist snapshots the strike map under g.mu and QUEUES it for the writer. It
// never touches the disk itself: it is called from apply, which runs inline in
// the exit-recording path (recordClose), and an exit must never wait on an fsync
// (m55r3). Concurrent snapshots are ordered by sequence and coalesced - only the
// newest is written - and the write is the same temp-file + fsync + rename, so a
// crash leaves the previous or the new complete file, as before. Flush waits for
// the writer to go idle (the owner-action and boot callers, tests).
func (g *FillGuard) persist() {
	g.mu.Lock()
	path := g.statePath
	if path == "" || g.noPersist {
		g.mu.Unlock()
		return
	}
	g.stateSeq++
	seq := g.stateSeq
	d := fillStateDisk{Version: fillStateVersion, SavedAt: time.Now().Unix(), Strikes: map[string]fillStateStrike{}}
	cutoff := time.Now().Add(-fillStateMaxAge).UnixMilli()
	for k, st := range g.strikes {
		if st.lastMs < cutoff {
			continue
		}
		d.Strikes[k] = fillStateStrike{Account: st.account, Session: st.session, LastMs: st.lastMs,
			IDs: append([]string(nil), st.ids...), Events: append([]string(nil), st.events...),
			Details: append([]string(nil), st.details...), Acked: st.acked}
	}
	if u := g.unreadable; u != nil {
		du := &fillStateUnreadable{Since: u.Since, Why: u.Why, BadFile: u.BadFile}
		for k, ok := range u.Acked {
			if ok {
				du.Acked = append(du.Acked, k)
			}
		}
		sort.Strings(du.Acked)
		d.Unreadable = du
	}
	g.mu.Unlock()
	b, err := json.Marshal(d)
	if err != nil {
		logger.Errorf("datatruth: fill_price state encode: %v", err)
		return
	}
	g.wmu.Lock()
	if g.wpend == nil || seq > g.wpend.seq {
		g.wpend = &fillSnap{path: path, b: b, seq: seq}
	}
	start := !g.wrunning
	g.wrunning = true
	g.wmu.Unlock()
	if start {
		safe.GoNamed("fill_price state writer", g.drain)
	}
}

// drain is the single writer: it writes the newest queued snapshot until none is
// left, then goes idle and releases the Flush waiters.
func (g *FillGuard) drain() {
	// A panic must not leave the writer marked running forever (every later
	// persist would queue and never write): reset, release the waiters, and let
	// the panic net log it.
	defer func() {
		if r := recover(); r != nil {
			g.wmu.Lock()
			g.wrunning = false
			w := g.widle
			g.widle = nil
			g.wmu.Unlock()
			for _, c := range w {
				close(c)
			}
			panic(r)
		}
	}()
	for {
		g.wmu.Lock()
		s := g.wpend
		g.wpend = nil
		if s == nil {
			g.wrunning = false
			w := g.widle
			g.widle = nil
			g.wmu.Unlock()
			for _, c := range w {
				close(c)
			}
			return
		}
		g.wmu.Unlock()
		if s.seq < g.fileSeq {
			continue
		}
		if err := writeFileAtomic(s.path, s.b); err != nil {
			logger.Errorf("datatruth: fill_price state write %s: %v - a restart now would forget the standing strikes", s.path, err)
			continue
		}
		g.fileSeq = s.seq
	}
}

// Flush waits up to timeout for every queued state write to finish; true = the
// writer is idle (everything queued before the call is on disk or has failed
// loudly). Never called from the exit path.
func (g *FillGuard) Flush(timeout time.Duration) bool {
	g.wmu.Lock()
	if !g.wrunning && g.wpend == nil {
		g.wmu.Unlock()
		return true
	}
	c := make(chan struct{})
	g.widle = append(g.widle, c)
	g.wmu.Unlock()
	select {
	case <-c:
		return true
	case <-time.After(timeout):
		return false
	}
}

// writeFileAtomic: temp file beside the target, fsync, rename (the owner-push
// state file's way).
func writeFileAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp" // replaced by the next write, consumed by the rename
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RegisterFillPrice registers the check for g.
func RegisterFillPrice(g *FillGuard) {
	// this pause does NOT end by itself when the data measures clear: it ends on
	// the owner's acknowledgement or the next CME session (review 4 D5)
	datatruth.SetResumeNote(CheckFillPrice, "entries resume when you acknowledge it (the how-to is in the pause text above) or when the next CME session starts - not by themselves")
	datatruth.SetResumeFunc(CheckFillPrice, fillResume)
	datatruth.RegisterScoped(CheckFillPrice, g.CheckScoped())
}

// dedupeStrings keeps the first of each identical string, in order (a far fill
// reported by several frames repeats the same detail; the push has 600 runes).
func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, x := range in {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
