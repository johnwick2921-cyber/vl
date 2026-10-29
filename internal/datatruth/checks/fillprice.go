package checks

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"vl/internal/datatruth"
	"vl/logger"
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
//     fill-minute bar alone was 1.0 point, and ZERO fills were outside the
//     window band - the tolerance is a margin over measured reality, not a guess.
//   - when no bar of that minute exists yet the fill WAITS (the forming bar is
//     not in the ring until it closes) and is judged when it arrives; when a hole
//     means the bar never will, the order's own price +- SlipTicks (or the AddOn's
//     reported slippage) is the reference; with neither, it is counted UNJUDGED -
//     a missing bar is the bar-gap checks' episode, not a second one here.
//   - a far fill raises ONE alert and a counter.
//   - the SECOND far fill of the same CME session for the symbol pauses NEW
//     ENTRIES until the owner acknowledges (Ack) or the next session starts. It
//     is a pause on entries only: the exit, stop, trail, close, protection and
//     flatten paths never consult it.
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
	// OrderPx is the order's own price (0 = unknown). SlipTicks / HasSlip carry
	// the AddOn's reported slippage against the order when it sent one.
	OrderPx   float64
	SlipTicks float64
	HasSlip   bool
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
	ids     []string
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
}

// FillGuard judges fills, counts far ones per session and exposes the check.
type FillGuard struct {
	mu      sync.Mutex
	p       FillParams
	alert   func(kind, text string)
	session func(time.Time) string
	seen    map[string]bool
	pending []pendingFill
	strikes map[string]*strikeState // by symbol
	stats   FillStats
}

// NewFillGuard builds a guard. session maps an instant to its CME session key
// (kernel.CMESessionDayKey in production); nil keys everything to one session.
func NewFillGuard(p FillParams, session func(time.Time) string) *FillGuard {
	if session == nil {
		session = func(time.Time) string { return "" }
	}
	return &FillGuard{p: p.norm(), session: session, seen: map[string]bool{}, strikes: map[string]*strikeState{}}
}

// DefaultFills is the process-wide guard the fill handlers record into. It is
// inert until the wiring gives it a session function and an alert sender.
var DefaultFills = NewFillGuard(DefaultFillParams(), nil)

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
	if o.ID == "" || o.Price <= 0 || o.AtMs <= 0 {
		return
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
	g.stats.Far++
	sess := g.session(time.UnixMilli(o.AtMs))
	st := g.strikes[o.Symbol]
	if st == nil || st.session != sess {
		st = &strikeState{session: sess}
		g.strikes[o.Symbol] = st
	}
	st.ids = append(st.ids, o.ID)
	st.details = append(st.details, v.Detail)
	n := len(st.ids)
	alert := g.alert
	strikes := g.p.Strikes
	g.mu.Unlock()

	text := fmt.Sprintf("DATA TRUTH: FILL PRICE FAR from the market - %s %s %s: %s. Either the bars lost this print or the recorded fill price is wrong. Far fills this session for %s: %d of %d (the %s pauses new entries until you acknowledge or the next session).",
		o.Symbol, o.Kind, o.Side, v.Detail, o.Symbol, n, strikes, ordinal(strikes))
	logger.Warnf("datatruth: %s [fill id %s]", text, o.ID)
	if alert != nil {
		alert(fmt.Sprintf("data_truth_fill_price_far|%s|%s", o.Symbol, o.ID), text)
	}
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

// Ack is the owner acknowledging the symbol's far fills: entries resume until
// the next far fill. Returns whether a pause was standing.
func (g *FillGuard) Ack(symbol string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.strikes[symbol]
	if st == nil || st.acked || len(st.ids) < g.p.Strikes {
		return false
	}
	st.acked = true
	return true
}

// Stats returns the counters.
func (g *FillGuard) Stats() FillStats {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.stats
	s.Pending = len(g.pending)
	return s
}

// Check is the datatruth.CheckFn: blocked when the symbol has Strikes far fills
// this session and the owner has not acknowledged. It sweeps the waiting fills
// first, so a pause is never a minute behind the bars that prove it.
func (g *FillGuard) Check() datatruth.CheckFn {
	return func(symbol string, now time.Time) (bool, string, error) {
		g.Sweep(now)
		g.mu.Lock()
		defer g.mu.Unlock()
		st := g.strikes[symbol]
		if st == nil {
			return false, "", nil
		}
		if st.session != g.session(now) {
			return false, "", nil // the next session: the strikes are spent
		}
		if len(st.ids) < g.p.Strikes || st.acked {
			return false, "", nil
		}
		ids := append([]string(nil), st.ids...)
		sort.Strings(ids)
		return true, fmt.Sprintf("%d far fill(s) this session (ids %s): %s - acknowledge via POST /api/risk/ack-fill-price?symbol=%s or wait for the next session",
			len(st.ids), strings.Join(ids, ","), strings.Join(st.details, " | "), symbol), nil
	}
}

// RegisterFillPrice registers the check for g.
func RegisterFillPrice(g *FillGuard) {
	datatruth.Register(CheckFillPrice, g.Check())
}
