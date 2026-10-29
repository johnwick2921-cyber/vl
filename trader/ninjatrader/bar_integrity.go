package ninjatrader

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/telemetry"
)

// ── DATA-INTEGRITY GATE (m445, release #54) ─────────────────────────────────
//
// Owner, 2026-10-29: "this is a financial product, not a game — nothing related
// to data loss can continue to trade."
//
// Before this, the ONLY data gate on a new entry was the stale-feed block (the
// live 1m older than ~75 s). The bar-hole tracker (bar_holes.go) knew about
// every missing stretch of 1m bars, and NOTHING read it for a decision:
// BarHolesSnapshot fed /api/health and no entry path consulted it. After a
// reconnect the bot traded on incomplete bars (2026-10-26: 297 open minutes of
// MNQ 1m, 17:00–21:57 CT, never refilled).
//
// IntegrityVerdictFor is the ONE verdict per symbol. The trader package asks it
// at the single admission chain every entry crosses (admitEntry) and at the
// mentor's two authoring seams, and folds the existing stale-feed leg into it
// (trader/data_integrity_gate.go).
//
// THE VERDICT IS BLOCKED WHEN ANY OF:
//
//	(a) a hole — refilling, unfilled, RETIRED or untracked; anything but filled —
//	    overlaps the data window the decision reads for the symbol;
//	(b) the detector is UNKNOWN: no tracker wired (no bar store), the boot scan
//	    has not judged this symbol yet this process, a store read left a minute
//	    unjudged, or the detector is more than integrityLagBars behind the
//	    cache. Unknown is BLOCKED (fail-closed), never "no holes";
//	(c) the stale-feed condition — added by the trader-side caller, which owns
//	    the clock-step hold and the monotonic receipt age.
//
// Resumes only on a MEASUREMENT: a hole leaves the tracker when its window is
// re-read from the store and no stretch of ≥ minGap open minutes is missing
// (fillLocked), or when it ages out of the window. A retired hole is NOT a
// fill: the tracker used to drop it, which would have read as "all clear" while
// the minutes were still gone — it is kept here and re-measured.
//
// WHAT A "HOLE" IS (inherited, stated so nobody reads more into the gate than
// it has): ≥ BARS_HOLE_MIN_GAP_MIN (default 3) consecutive OPEN minutes with no
// stored 1m bar between two stored bars. One or two missing minutes are not
// tracked. The CME break, weekends and holiday closes are never holes
// (barHoleExpected).

// IntegrityIntradayOpenMin is the data window of every INTRADAY mentor setup,
// in OPEN 1m minutes: the 1m series the mentor evaluator reads each pass
// (trader.mentorBars1mDepth = 1500; pinned equal by the trader package). Every
// frame the evaluator uses — 2m, 5m, 15m, 1h, 4h, the §7 run window, the day
// latch, the boxes — is AGGREGATED from this slice, so a 1m hole anywhere in
// it corrupts all of them.
const IntegrityIntradayOpenMin = 1500

// IntegrityDeepOpenMin is the window of the entries that also read the 4h
// structure: the SWING4H setup (4h EMA 34), and the AI / planner / Picture /
// agent paths (kernel.HTFVetoNativeTF = "4h" structure; 1m SVP/FVG/tape of
// AISVPBarCount = 2000, which this contains). 34 four-hour candles x 240 open
// minutes = 8160: the EMA's own period. A candle older than that carries under
// 0.75% of the EMA's weight, so a few missing minutes there cannot move it.
const IntegrityDeepOpenMin = 34 * 240

// IntegrityAction is the one instruction the owner is given for a hole.
const IntegrityAction = "NT8: Reload All Historical Data (or wait for the auto re-request)"

const (
	// IntegrityClassHole / IntegrityClassUnknown are the verdict's reason
	// classes — the gate-block counter names and the /api/health reasons.
	IntegrityClassHole    = "data_integrity_hole"
	IntegrityClassUnknown = "data_integrity_unknown"

	// integrityLagBars — how many closed 1m bars the cache may hold beyond the
	// newest the detector judged before the detector counts as lagging. One bar
	// is the persister working on the bar that just closed.
	integrityLagBars = 2

	integrityRetiredCap     = 64
	integrityRetiredMaxAge  = 30 * 24 * time.Hour
	integrityRemeasureEvery = 60 * time.Second
)

// integrityArmed is set when the NT8 bars bridge is wired into the market
// package (wireFuturesBarsProvider): from then on the bot reads live NT8 bars,
// and the gate is armed. Unarmed (a crypto trader, a sandbox, a unit test with
// a synthetic provider) there is no NT8 data to lose and the verdict is inert —
// the same boundary the stale-feed block draws on market.FuturesBarsProvider.
var integrityArmed atomic.Bool

// IntegrityArmed reports whether the gate applies in this process.
func IntegrityArmed() bool { return integrityArmed.Load() }

// SetIntegrityArmedForTest arms or disarms the gate and returns the restore.
// TEST-ONLY.
func SetIntegrityArmedForTest(on bool) (restore func()) {
	prev := integrityArmed.Swap(on)
	return func() { integrityArmed.Store(prev) }
}

// IntegrityHole is one hole the verdict names. Every field is READ.
type IntegrityHole struct {
	Symbol, Contract string
	FromMs, ToMs     int64
	Missing          int    // open minutes still missing at the last measure
	State            string // refilling | unfilled | retired | untracked
	Window           string // "10-26 17:00–21:57 CT" (holeWindowCT)
	// Unfillable — no re-request will refill it (retired / untracked, or
	// unfilled with the slow retries spent): it pauses trading until it ages out
	// of the window or NT8's history is reloaded and the hole re-measured.
	Unfillable bool
}

// IntegrityVerdict is the gate's answer for one symbol and window.
type IntegrityVerdict struct {
	Armed   bool
	Blocked bool
	Class   string // "" | IntegrityClassHole | IntegrityClassUnknown
	Unknown string // the reason, when the detector is unknown
	Holes   []IntegrityHole
	// WindowFromMs is the oldest minute the window reads; CoveredFromMs the
	// oldest minute the detector has judged for the symbol (0 = none yet). A
	// window older than the coverage is not judged beyond it — disclosed here,
	// never read as clean.
	WindowFromMs  int64
	CoveredFromMs int64
}

// Windows renders every hole's window, in CT.
func (v IntegrityVerdict) Windows() []string {
	out := make([]string, 0, len(v.Holes))
	for _, h := range v.Holes {
		out = append(out, h.Window)
	}
	return out
}

// IntegrityVerdictFor is THE data-integrity verdict for `symbol`, judged over
// the last `openMinutes` OPEN 1m minutes before `now`.
func IntegrityVerdictFor(symbol string, openMinutes int, now time.Time) IntegrityVerdict {
	if !integrityArmed.Load() {
		return IntegrityVerdict{}
	}
	t := barHoles.Load()
	if t == nil {
		return IntegrityVerdict{Armed: true, Blocked: true, Class: IntegrityClassUnknown,
			Unknown: "the bar-hole detector is not running (no bar store wired) — missing bars cannot be seen"}
	}
	return t.integrity(symbol, openMinutes, now)
}

// integrityMemo remembers the window-start walk for one wall minute: walking
// 8160 open minutes through the calendar on every admission would be wasteful,
// and the answer only changes when the minute does.
type integrityMemo struct {
	mu sync.Mutex
	at map[int]memoStart // openMinutes -> start
}

type memoStart struct{ minute, from int64 }

func (m *integrityMemo) get(openMin int, minute int64) (int64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.at[openMin]; ok && s.minute == minute {
		return s.from, true
	}
	return 0, false
}

func (m *integrityMemo) put(openMin int, minute, from int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.at == nil {
		m.at = map[int]memoStart{}
	}
	m.at[openMin] = memoStart{minute: minute, from: from}
}

// windowFromLocked is the open time of the openMinutes-th OPEN minute back from
// `now` (the one calendar, t.isOpen): the oldest minute the window reads.
func (t *barHoleTracker) windowFromLocked(openMinutes int, now time.Time) int64 {
	const minute = int64(60_000)
	cur := now.UnixMilli() / minute * minute
	if from, ok := t.integ.get(openMinutes, cur); ok {
		return from
	}
	open, m := 0, cur
	// bounded: openMinutes open minutes are always found inside a few weeks
	for steps := 0; steps < openMinutes*4+10_000 && open < openMinutes; steps++ {
		m -= minute
		if t.isOpen(time.UnixMilli(m)) {
			open++
		}
	}
	t.integ.put(openMinutes, cur, m)
	return m
}

func (t *barHoleTracker) noteJudgeErrLocked(key string, err error) {
	t.judgeErr[key] = err.Error()
}

func (t *barHoleTracker) noteJudgedLocked(key string, from int64) {
	delete(t.judgeErr, key)
	if cur, ok := t.coveredFrom[key]; !ok || from < cur {
		t.coveredFrom[key] = from
	}
}

// retainRetiredLocked keeps a hole the tracker has stopped chasing.
func (t *barHoleTracker) retainRetiredLocked(h *barHole) {
	c := *h
	c.State = "retired"
	t.keepLocked(&c)
}

// retainLocked keeps a hole the tracker could not track at all (the cap).
func (t *barHoleTracker) retainLocked(symbol, contract string, fromMs, toMs int64, missing int, state string) {
	t.keepLocked(&barHole{Symbol: symbol, Contract: contract, FromMs: fromMs, ToMs: toMs,
		MissingAtDetect: missing, Missing: missing, State: state})
}

func (t *barHoleTracker) keepLocked(h *barHole) {
	for _, x := range t.retired {
		if x.Symbol == h.Symbol && x.Contract == h.Contract && x.FromMs == h.FromMs && x.ToMs == h.ToMs {
			return
		}
	}
	t.retired = append(t.retired, h)
	if len(t.retired) > integrityRetiredCap {
		// the oldest goes: it is the first to age out of every window anyway
		sort.Slice(t.retired, func(i, j int) bool { return t.retired[i].ToMs > t.retired[j].ToMs })
		t.retired = t.retired[:integrityRetiredCap]
	}
}

// remeasureRetired re-reads each retained hole's window from the store (at most
// once a minute): a hole NT8 has since filled (the owner reloaded history and a
// replay carried it) is dropped — the gate resumes on the measurement, never on
// the passage of time. A hole older than integrityRetiredMaxAge is dropped
// unmeasured: it is outside every window.
func (t *barHoleTracker) remeasureRetired(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.retired) == 0 || (!t.lastRemeasure.IsZero() && now.Sub(t.lastRemeasure) < integrityRemeasureEvery) {
		return
	}
	t.lastRemeasure = now
	keep := t.retired[:0]
	for _, h := range t.retired {
		if time.UnixMilli(h.ToMs).Before(now.Add(-integrityRetiredMaxAge)) {
			continue
		}
		filled, missing, err := t.measureLocked(h)
		if err != nil {
			t.sayErrOnce("measure-retired", fmt.Sprintf("🕳 retired-hole re-measure failed for %s %s (%v) — the hole stays listed and the data-integrity gate stays blocked on it", h.Symbol, holeWindowCT(h.FromMs, h.ToMs), err))
			keep = append(keep, h)
			continue
		}
		if filled {
			telemetry.IncBarsHoleFilled(h.Attempts)
			t.say("INFO", fmt.Sprintf("✅ bars hole FILLED (%s, re-measured): %s %s %s on %s — the store now holds the minutes; the data-integrity gate no longer counts it",
				h.State, h.Symbol, barHoleTF, holeWindowCT(h.FromMs, h.ToMs), h.Contract))
			NoteHistoryRefill(h.Symbol, barHoleTF, h.MissingAtDetect, fmt.Sprintf("bars hole FILLED %s %s %s", h.Symbol, barHoleTF, holeWindowCT(h.FromMs, h.ToMs)))
			continue
		}
		h.Missing = missing
		keep = append(keep, h)
	}
	t.retired = keep
}

// symbolScannedLocked reports whether the detector has judged `symbol` this
// process — on the contract the wire currently names when it names one.
func (t *barHoleTracker) symbolScannedLocked(symbol, cur string) (key string, ok bool) {
	if cur != "" {
		k := holeKey(symbol, cur)
		_, ok := t.checked[k]
		return k, ok
	}
	for k := range t.checked {
		if strings.EqualFold(strings.SplitN(k, "|", 2)[0], symbol) {
			return k, true
		}
	}
	return "", false
}

// integrity answers for one symbol and window (see IntegrityVerdictFor).
func (t *barHoleTracker) integrity(symbol string, openMinutes int, now time.Time) IntegrityVerdict {
	v := IntegrityVerdict{Armed: true}
	t.mu.Lock()
	defer t.mu.Unlock()
	from := t.windowFromLocked(openMinutes, now)
	v.WindowFromMs = from

	cur := ""
	var w holeWire
	if w = t.wire(); w != nil {
		if f, ok := w.CurrentContract(symbol); ok {
			cur = f.Contract
		}
	}

	// (b) the detector must have KNOWN: scanned, readable, not lagging.
	key, scanned := t.symbolScannedLocked(symbol, cur)
	switch {
	case !scanned:
		v.Unknown = fmt.Sprintf("the boot hole scan has not judged %s yet this process (no live 1m bar read back) — missing bars cannot be ruled out", symbol)
	case t.judgeErr[key] != "":
		v.Unknown = fmt.Sprintf("a bar-store read failed for %s (%s) — the minute it covered is unjudged", symbol, t.judgeErr[key])
	default:
		v.CoveredFromMs = t.coveredFrom[key]
		if w != nil && w.BarCache() != nil {
			if newest := newestFinal1m(w, symbol); newest > 0 {
				if lag := (newest - t.checked[key]) / 60_000; lag >= integrityLagBars {
					v.Unknown = fmt.Sprintf("the hole detector is %d closed 1m bar(s) behind the cache for %s — the newest bars are unjudged", lag, symbol)
				}
			}
		}
	}

	// (a) every hole that is not filled and overlaps the window.
	add := func(h *barHole, state string) {
		if !strings.EqualFold(h.Symbol, symbol) {
			return
		}
		if cur != "" && h.Contract != cur {
			return // another contract's minutes are not in the series the bot reads
		}
		lo := h.FromMs
		if from > lo {
			lo = from
		}
		const minute = int64(60_000)
		missing := 0
		for m := lo; m < h.ToMs && m <= now.UnixMilli(); m += minute {
			if t.isOpen(time.UnixMilli(m)) {
				missing++
			}
		}
		if missing == 0 {
			return
		}
		// The window the OWNER reads: the first to the last missing OPEN minute.
		// The tracker's span starts at the first minute after the last stored
		// bar, which across the 16:00 break is a CLOSED minute ("16:00–17:10"
		// for a ten-minute reopen lag).
		first, last := int64(0), int64(0)
		for m := h.FromMs; m < h.ToMs; m += minute {
			if t.isOpen(time.UnixMilli(m)) {
				if first == 0 {
					first = m
				}
				last = m
			}
		}
		win := holeWindowCT(h.FromMs, h.ToMs)
		if first != 0 {
			win = holeWindowCT(first, last+minute)
		}
		v.Holes = append(v.Holes, IntegrityHole{
			Symbol: h.Symbol, Contract: h.Contract, FromMs: h.FromMs, ToMs: h.ToMs,
			Missing: h.Missing, State: state, Window: win,
			Unfillable: state == "retired" || state == "untracked" || (state == holeStateUnfilled && h.saidSlowCap),
		})
	}
	for _, h := range t.holes {
		add(h, h.State)
	}
	for _, h := range t.retired {
		add(h, h.State)
	}
	sort.Slice(v.Holes, func(i, j int) bool { return v.Holes[i].FromMs < v.Holes[j].FromMs })

	switch {
	case len(v.Holes) > 0:
		v.Blocked, v.Class = true, IntegrityClassHole
	case v.Unknown != "":
		v.Blocked, v.Class = true, IntegrityClassUnknown
	}
	return v
}

// newestFinal1m is the open time of the cache's newest FINAL 1m bar (0 = none).
func newestFinal1m(w holeWire, symbol string) int64 {
	bars := w.BarCache().Get(symbol, barHoleTF)
	for i := len(bars) - 1; i >= 0; i-- {
		if bars[i].Final {
			return bars[i].T
		}
	}
	return 0
}

// ── /api/health: trading_paused ─────────────────────────────────────────────

// PausedReason is one reason a symbol is paused — a READ value.
type PausedReason struct {
	Class  string   `json:"class"`
	Detail string   `json:"detail"`
	Window []string `json:"windows,omitempty"` // the holes' windows, CT
}

// PausedView is one symbol's pause state as /api/health serves it.
type PausedView struct {
	Symbol  string         `json:"symbol"`
	Paused  bool           `json:"paused"`
	Scope   string         `json:"scope,omitempty"` // all_entries | swing_and_deep_entries
	Reasons []PausedReason `json:"reasons"`
	Action  string         `json:"action,omitempty"`
}

// TradingPausedSnapshot is the health surface's read: one view per symbol the
// detector knows, judged over BOTH windows. armed=false (no NT8 bridge) → the
// field is ABSENT, never a fabricated "not paused" (A24).
func TradingPausedSnapshot(now time.Time) (views []PausedView, armed bool) {
	if !integrityArmed.Load() {
		return nil, false
	}
	t := barHoles.Load()
	if t == nil {
		return []PausedView{{Symbol: "", Paused: true, Scope: "all_entries", Action: IntegrityAction,
			Reasons: []PausedReason{{Class: IntegrityClassUnknown, Detail: "the bar-hole detector is not running (no bar store wired)"}}}}, true
	}
	t.mu.Lock()
	seen := map[string]bool{}
	var syms []string
	add := func(s string) {
		if s != "" && !seen[strings.ToUpper(s)] {
			seen[strings.ToUpper(s)] = true
			syms = append(syms, s)
		}
	}
	for k := range t.checked {
		add(strings.SplitN(k, "|", 2)[0])
	}
	for _, h := range t.holes {
		add(h.Symbol)
	}
	for _, h := range t.retired {
		add(h.Symbol)
	}
	t.mu.Unlock()
	sort.Strings(syms)
	if len(syms) == 0 {
		// armed, a tracker, and NOT ONE symbol judged yet: the detector has
		// not seen the market this process — unknown, never "nothing paused"
		return []PausedView{{Symbol: "", Paused: true, Scope: "all_entries", Action: IntegrityAction,
			Reasons: []PausedReason{{Class: IntegrityClassUnknown, Detail: "the boot hole scan has not judged any symbol yet this process"}}}}, true
	}
	for _, s := range syms {
		intra := t.integrity(s, IntegrityIntradayOpenMin, now)
		deep := t.integrity(s, IntegrityDeepOpenMin, now)
		pv := PausedView{Symbol: s, Reasons: []PausedReason{}}
		worst := deep
		if intra.Blocked {
			pv.Scope, worst = "all_entries", intra
		} else if deep.Blocked {
			pv.Scope = "swing_and_deep_entries"
		}
		if worst.Blocked {
			pv.Paused = true
			pv.Action = IntegrityAction
			r := PausedReason{Class: worst.Class, Window: worst.Windows()}
			if worst.Class == IntegrityClassHole {
				r.Detail = fmt.Sprintf("%d hole(s) in the data window", len(worst.Holes))
			} else {
				r.Detail = worst.Unknown
			}
			pv.Reasons = append(pv.Reasons, r)
		}
		views = append(views, pv)
	}
	return views, true
}
