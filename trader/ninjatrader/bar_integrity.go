package ninjatrader

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	"vl/kernel"
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
// (trader.mentorBars1mDepth = 1500; pinned equal by the trader package). The
// frames the evaluator builds PER PASS — 2m, 5m, 15m, 1h, 4h, the §7 run
// window, the boxes — are aggregated from this slice, so a 1m hole anywhere in
// it corrupts all of them.
//
// This window is NOT everything a mentor decision stands on (review round 2,
// reviewer A, D1): the evaluator's SEEDED state — the 1H RTH key levels
// (kernel/mentor.KeyLevels, "No lookback cap"), the open-HTF lines, the day
// latch, the trendlines — is built once from the contract-stitched 1m history
// (trader.mentorFullKeyLevelHistory) and lives for the life of the process. Two
// legs close that, both for mentor decisions only:
//
//   - IntegrityVerdictForMentor: any unfilled hole the TRACKER holds that takes
//     RTH minutes (08:30–15:00 CT), whatever its age (a hole detected live, or by
//     the boot scan inside its reach, stays in force until it is measured filled);
//   - SeriesRTHHoles (bar_integrity_series.go): the RTH holes in the stitched
//     series the seed actually walked, measured by the trader at every seed and
//     re-seed — the tracker follows one contract and cannot see that series.
const IntegrityIntradayOpenMin = 1500

// ── WHAT EACH READER READS (m445 round 4, D2) ──────────────────────────────
//
// A window is the 1m the reader STANDS ON, counted in OPEN minutes. The table is
// derived from the code each row names — the trader package pins every constant
// to that code (TestIntegrityWindowsEqualTheCodeTheyDescribe):
//
//	reader                                     frames / source                      1m lookback (open min)
//	mentor per-pass evaluator (intraday setups) 2m..4h aggregated from the 1m tape   1500  mentorBars1mDepth
//	AI decision / agent / Picture               4h/1h/15m/5m NATIVE per-frame subs;  2500  structureMapBarsAsk (structure map
//	                                            1m only for SVP/FVG/tape (2000) and        rebuilds 1h/15m/5m from the 1m store),
//	                                            the structure map                          AISVPBarCount 2000 inside it
//	planner arm (the rows the planner rests)    8-session daily-candle table, weekly 12000 plannerCandleTapeBars (barsWithStoreDepth)
//	                                            line, realized-vol baseline
//	mentor SWING4H                              4h aggregated FROM 1m, EMA 34 warm-up 24720 (102+1) x 240
//
// The 4h the AI / planner / Picture / agent paths read is a NATIVE 4h frame, not
// built from the bot's 1m store (kernel.HTFVetoNativeTF, WithNativeHTFStructure,
// the per-frame subscriptions; bar_cache.go Aggregate1mToTF: "2h/4h are NOT built
// here"): a hole in the 1m store cannot corrupt a native 4h candle, so the old
// "34 four-hour candles" derivation of the deep window was wrong in kind (round 3,
// reviewer B). Only the mentor evaluator aggregates 4h from 1m, so only SWING4H
// carries a 4h-candle window.
//
// IntegrityDeepOpenMin is the window of every NON-mentor entry path (AI decision,
// agent, Picture, planner arm). The widest 1m reader among them is the planner's
// candle tape, so one window serves them all: fail-closed on the wider reader (a
// hole 2500..12000 open minutes back pauses a decision/agent/Picture entry that
// would not have read it — disclosed, and no live trader runs those paths while
// mentor mode is on, since mentor mode refuses every non-mentor entry).
const IntegrityDeepOpenMin = 12000

// IntegritySwingOpenMin is the window of the SWING4H setup: the mentor's 4h
// EMA 34 is gated on its WARM-UP — kernel/mentor.FourHEMA34Warmup = 102 closed
// 4h candles ("the floor the fail-closed gate actually reads", seed.go) — and
// is computed over that whole seeded history. A candle 34 back still carries
// ~0.8% of the EMA and everything older ~14% together, so the 34-candle window
// the other paths use is too short for it. 102 candles + the boundary candle.
// (Pinned equal to the mentor constant by the trader package.)
const IntegritySwingOpenMin = (102 + 1) * 240

// IntegrityAction is the instruction for a hole NT8 can still replay (its first
// missing minute is inside the subscription's last bars_back 1m bars, about 33
// trading hours, on the contract the subscription still names).
const IntegrityAction = "NT8: Reload All Historical Data (or wait for the auto re-request)"

// The instructions for a hole NO reload reaches (round 4, D1): the old text sent
// the owner to a reload that replays only the last ~2000 1m bars and cannot
// return days-old minutes, nor an expired contract's. What clears it is stated
// with the moment it clears by itself (IntegrityHole.ClearsAtMs).
const (
	integrityImportHow = "POST /api/admin/bars/import for that contract (HISTORICAL_IMPORT_SEAM=on; NT8 must be able to serve the range)"
	// IntegrityActionRetired — a tracker hole outside NT8's replay: the tracker
	// re-reads its window from the store about once a minute.
	IntegrityActionRetired = "no NT8 reload reaches this stretch (it is older than the ~2000 1m bars NT8 replays, or on a contract the subscription no longer serves); it resumes by itself when the minutes are imported — " + integrityImportHow + " — read back within a minute, or at the time shown"
	// IntegrityActionSeeded — a hole in the series the mentor was seeded from:
	// measured at the seed, so an import needs a restart to be seen.
	IntegrityActionSeeded = "no NT8 reload reaches this stretch (it is days old, on a contract NT8 no longer replays); it clears when the minutes are imported — " + integrityImportHow + " — and the bot is restarted (the seed measures the series again at start), or at the time shown"
	// IntegrityActionSpent — inside NT8's replay but the bot's own re-requests are used up.
	IntegrityActionSpent = "NT8: Reload All Historical Data, then restart the bot (its automatic re-requests are used up)"
)

const (
	// IntegrityClassHole / IntegrityClassUnknown are the verdict's reason
	// classes — the gate-block counter names and the /api/health reasons.
	IntegrityClassHole    = "data_integrity_hole"
	IntegrityClassUnknown = "data_integrity_unknown"

	// integrityLagBars — how many closed 1m bars the cache may hold beyond the
	// newest the detector judged before the detector counts as lagging. One bar
	// is the persister working on the bar that just closed.
	integrityLagBars = 2

	// integrityCoverTolMs — how much later than the window start the detector's
	// coverage may begin before the window counts as partly unjudged.
	integrityCoverTolMs = int64(2 * 60_000)

	integrityRetiredCap = 64
	// a retained hole is outside every judgement once it is older than the 1m
	// retention (90 d) — the key-level leg reaches that far back
	integrityRetiredMaxAge  = 100 * 24 * time.Hour
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
	// ClearsAtMs — the instant this hole stops pausing BY ITSELF, whatever the
	// owner does (it leaves the decision's data window, or the 90-day key-level
	// reach). 0 = unknown. A READ value, computed from the calendar.
	ClearsAtMs int64
}

// Remedy is the instruction that actually works for THIS hole.
func (h IntegrityHole) Remedy() string {
	switch {
	case h.State == "seeded":
		return IntegrityActionSeeded
	case h.State == "retired":
		return IntegrityActionRetired
	case h.State == holeStateUnfilled && h.Unfillable:
		return IntegrityActionSpent
	default:
		return IntegrityAction
	}
}

// IntegrityActionFor joins the DISTINCT remedies of the holes (in hole order);
// the reload text when none is named.
func IntegrityActionFor(holes []IntegrityHole) string {
	var parts []string
	seen := map[string]bool{}
	for _, h := range holes {
		if r := h.Remedy(); !seen[r] {
			seen[r] = true
			parts = append(parts, r)
		}
	}
	if len(parts) == 0 {
		return IntegrityAction
	}
	return strings.Join(parts, " | ")
}

// ClearsAtLabel renders ClearsAtMs for the owner ("11-01 14:23 CT"), "" when unknown.
func (h IntegrityHole) ClearsAtLabel() string {
	if h.ClearsAtMs <= 0 {
		return ""
	}
	return kernel.TableTimeCT(time.UnixMilli(h.ClearsAtMs)) + " CT"
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
	return t.integrity(symbol, openMinutes, now, false)
}

// IntegrityVerdictForMentor is the verdict a MENTOR decision stands under: the
// same window judgement as IntegrityVerdictFor, plus the tracker's key-level
// leg — an unfilled hole the tracker holds that takes RTH minutes blocks,
// whatever its age, because the evaluator's key levels (built once, kept for the
// life of the process) may have been walked over those candles (see
// IntegrityIntradayOpenMin). The seeded-series leg is the trader's
// (data_integrity_seed.go).
func IntegrityVerdictForMentor(symbol string, openMinutes int, now time.Time) IntegrityVerdict {
	if !integrityArmed.Load() {
		return IntegrityVerdict{}
	}
	t := barHoles.Load()
	if t == nil {
		return IntegrityVerdict{Armed: true, Blocked: true, Class: IntegrityClassUnknown,
			Unknown: "the bar-hole detector is not running (no bar store wired) — missing bars cannot be seen"}
	}
	return t.integrity(symbol, openMinutes, now, true)
}

// integrityMemo remembers the window-start walk for one wall minute: walking
// 24720 open minutes through the calendar on every admission would be wasteful,
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
		// The oldest goes — but a dropped hole is a hole nobody lists any more,
		// and once the newer ones are filled it would read as clear (review round
		// 2, reviewer A). So an eviction is remembered: the verdict turns UNKNOWN
		// (blocked, fail-closed) for the rest of the process; a restart re-scans
		// the store and re-lists whatever is really missing.
		sort.Slice(t.retired, func(i, j int) bool { return t.retired[i].ToMs > t.retired[j].ToMs })
		t.retired = t.retired[:integrityRetiredCap]
		t.retiredEvicted = true
	}
}

// remeasureRetired re-reads, at most once a minute and from the STORE only (no
// wire send), every hole the tracker has stopped asking NT8 about: the RETIRED /
// untracked ones, and the SPENT ones (unfilled, slow re-requests used up). A
// hole NT8 has since filled (the owner reloaded history and a replay carried
// it) is dropped — the gate resumes on the measurement, never on the passage of
// time. Without the spent leg a hole whose retries ran out stayed in the
// tracker unmeasured for the life of the process, so a reload that DID refill
// it never lifted the pause (review round 1, reviewer B). A retained hole older
// than integrityRetiredMaxAge is dropped unmeasured: it is outside every
// window.
func (t *barHoleTracker) remeasureRetired(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var spent []*barHole
	for _, h := range t.holes {
		// m444 keeps a retired hole IN t.holes (State retired, never scheduled
		// again): it is measured here like a spent one, so a refill lifts the pause.
		if (h.saidSlowCap || h.State == holeStateRetired) && h.measureAt.IsZero() {
			spent = append(spent, h)
		}
	}
	if len(t.retired) == 0 && len(spent) == 0 {
		return
	}
	if !t.lastRemeasure.IsZero() && now.Sub(t.lastRemeasure) < integrityRemeasureEvery {
		return
	}
	t.lastRemeasure = now
	for _, h := range spent {
		filled, missing, err := t.measureLocked(h)
		if err != nil {
			t.sayErrOnce("measure-spent", fmt.Sprintf("🕳 spent-hole re-measure failed for %s %s (%v) — the hole stays listed and the data-integrity gate stays blocked on it", h.Symbol, holeWindowCT(h.FromMs, h.ToMs), err))
			continue
		}
		if filled {
			t.fillLocked(h, now)
			continue
		}
		h.Missing = missing
	}
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
// keyLevels adds the mentor's key-level leg (see IntegrityVerdictForMentor).
func (t *barHoleTracker) integrity(symbol string, openMinutes int, now time.Time, keyLevels bool) IntegrityVerdict {
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
		// A window that reaches back further than the detector has judged is
		// NOT clean — it is unjudged (review round 1, D2). The boot scan reaches
		// the deep window's start, so this fires only if that ever stops being
		// true; it then fails closed instead of reading "no holes" off a
		// stretch nobody looked at.
		if v.CoveredFromMs > from+integrityCoverTolMs {
			v.Unknown = fmt.Sprintf("the hole detector has judged %s only from %s but this decision reads back to %s — the older minutes are unjudged",
				symbol, kernel.TableTimeCT(time.UnixMilli(v.CoveredFromMs)), kernel.TableTimeCT(time.UnixMilli(from)))
		}
		if v.Unknown == "" && w != nil && w.BarCache() != nil {
			if newest := newestFinal1m(w, symbol); newest > 0 {
				if lag := (newest - t.checked[key]) / 60_000; lag >= integrityLagBars {
					v.Unknown = fmt.Sprintf("the hole detector is %d closed 1m bar(s) behind the cache for %s — the newest bars are unjudged", lag, symbol)
				}
			}
		}
	}

	if v.Unknown == "" && t.retiredEvicted {
		v.Unknown = "more holes were retained than the list holds and the oldest were dropped — which stretches are still missing cannot be said until the next restart re-scans the store"
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
		clears := int64(0)
		if missing > 0 {
			clears = t.windowClearsAtLocked(h.ToMs, openMinutes)
			// Round 5, DEF-A: for a MENTOR verdict a hole that takes RTH minutes
			// does NOT stop pausing when it leaves the window — the key-level leg
			// below picks it up (the seeded 1H RTH walk stands on it) until the
			// minutes are measured filled or it leaves the 90-day reach. The
			// "clears by itself" instant is therefore the LATER of the two; the
			// window-only instant would promise a resume that never comes.
			if keyLevels && h.ToMs > now.Add(-IntegritySeedReachDays*24*time.Hour).UnixMilli() && t.rthMissing(h.FromMs, h.ToMs) > 0 {
				if kl := h.ToMs + IntegritySeedReachDays*24*60*60_000; kl > clears {
					clears = kl
				}
			}
		}
		if missing == 0 && keyLevels && h.ToMs > now.Add(-IntegritySeedReachDays*24*time.Hour).UnixMilli() {
			// the key-level leg: a hole older than the window still blocks a
			// mentor decision when it takes RTH minutes (the seeded 1H RTH walk) —
			// until it is measured filled or leaves the 90-day reach, the same
			// reach the seeded-series leg uses (round 4: the two legs agree)
			missing = t.rthMissing(h.FromMs, h.ToMs)
			clears = h.ToMs + IntegritySeedReachDays*24*60*60_000
		}
		if missing == 0 {
			return
		}
		win := t.openWindowCT(h.FromMs, h.ToMs)
		v.Holes = append(v.Holes, IntegrityHole{
			Symbol: h.Symbol, Contract: h.Contract, FromMs: h.FromMs, ToMs: h.ToMs,
			Missing: h.Missing, State: state, Window: win, ClearsAtMs: clears,
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

// windowClearsAtLocked is the instant a hole whose missing stretch ends at toMs
// leaves a window of openMinutes OPEN minutes by itself: the window reads
// [from, cur) with `from` the openMinutes-th open minute back from cur, so the
// last missing minute L is out of it once openMinutes open minutes lie strictly
// between L and cur — the minute after the openMinutes-th open minute that
// follows L. Memoised: a 24720-minute walk must not repeat on every admission.
func (t *barHoleTracker) windowClearsAtLocked(toMs int64, openMinutes int) int64 {
	const minute = int64(60_000)
	// L = the last OPEN minute of the missing stretch [.., toMs)
	l := toMs - minute
	for steps := 0; steps < 24*60 && !t.isOpen(time.UnixMilli(l)); steps++ {
		l -= minute
	}
	k := [2]int64{l, int64(openMinutes)}
	if v, ok := t.clearsMemo[k]; ok {
		return v
	}
	open, m := 0, l
	for steps := 0; steps < openMinutes*4+10_000 && open < openMinutes; steps++ {
		m += minute
		if t.isOpen(time.UnixMilli(m)) {
			open++
		}
	}
	if open < openMinutes {
		return 0
	}
	at := m + minute
	if t.clearsMemo == nil {
		t.clearsMemo = map[[2]int64]int64{}
	}
	t.clearsMemo[k] = at
	return at
}

// IntegrityUnfilledHoles lists, whatever their age, every hole the tracker still
// holds for the symbol that is NOT measured filled. A resume reads it: a pause
// that ended with an entry here ended because the stretch LEFT the data window,
// not because the minutes came back — and the message must not say otherwise
// (round 3, reviewer B, D4). Empty (not nil-meaningful): no tracker, none held.
func IntegrityUnfilledHoles(symbol string) []IntegrityHole {
	t := barHoles.Load()
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []IntegrityHole
	add := func(h *barHole) {
		if strings.EqualFold(h.Symbol, symbol) {
			out = append(out, IntegrityHole{Symbol: h.Symbol, Contract: h.Contract, FromMs: h.FromMs, ToMs: h.ToMs,
				Missing: h.Missing, State: h.State, Window: t.openWindowCT(h.FromMs, h.ToMs)})
		}
	}
	for _, h := range t.holes {
		add(h)
	}
	for _, h := range t.retired {
		add(h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FromMs < out[j].FromMs })
	return out
}

// openWindowCT renders what the OWNER reads for a hole: the first to the last
// missing OPEN minute, in CT. The tracker's span starts at the first minute after
// the last stored bar, which across the 16:00 break is a CLOSED minute
// ("16:00–17:10" for a ten-minute reopen lag).
func (t *barHoleTracker) openWindowCT(fromMs, toMs int64) string {
	return openWindowCTWith(t.isOpen, fromMs, toMs)
}

func openWindowCTWith(isOpen func(time.Time) bool, fromMs, toMs int64) string {
	const minute = int64(60_000)
	first, last := int64(0), int64(0)
	for m := fromMs; m < toMs; m += minute {
		if isOpen(time.UnixMilli(m)) {
			if first == 0 {
				first = m
			}
			last = m
		}
	}
	if first == 0 {
		return holeWindowCT(fromMs, toMs)
	}
	return holeWindowCT(first, last+minute)
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
	// ClearsAt — per hole, when it stops pausing by itself ("10-26 17:00–21:57
	// CT clears by itself 11-02 09:12 CT"); absent when unknown.
	ClearsAt []string `json:"clears_at,omitempty"`
}

// The pause scopes /api/health names.
const (
	PausedScopeAll    = "all_entries"            // every entry path
	PausedScopeDeep   = "swing_and_deep_entries" // AI / planner / Picture / agent + SWING4H
	PausedScopeMentor = "mentor_entries"         // every mentor setup (the key-level legs)
	PausedScopeSwing  = "swing_entries"          // SWING4H only
	// release #55 round 3 (review 4 D1): the data-truth checks pause by READER or by
	// TRADER/ACCOUNT, and /api/health used to call every one of them all_entries.
	PausedScopeAI      = "ai_entries"     // AI / planner / Picture / agent paths only (mentor traders trade)
	PausedScopeTraders = "trader_entries" // only the traders / accounts the check names (data_truth_scopes)
	// PausedScopeMixed: a trader/account-scoped block stands NEXT TO a narrower reader
	// or SWING4H block (round 3 re-review P2). Either single scope sentence would
	// claim something false ("AI/planner entries trade normally" while an AI trader on
	// the blocked account is paused), so the banner says only that SOME entries are
	// paused and sends the owner to the per-check list.
	PausedScopeMixed = "mixed_entries"
)

// WidestPausedScope is the scope that says everything that is paused and no
// less. The mentor legs and the AI / planner / Picture legs are different
// readers: with BOTH paused no entry path is left, so the honest name is
// all_entries — naming only the mentor (or only the deep) scope would tell the
// owner the other half still trades (round 5, DEF-B). "" when none is paused.
func WidestPausedScope(scopes []string) string {
	has := map[string]bool{}
	for _, s := range scopes {
		has[s] = true
	}
	switch {
	case has[PausedScopeAll], has[PausedScopeMentor] && (has[PausedScopeDeep] || has[PausedScopeAI]):
		return PausedScopeAll
	case has[PausedScopeTraders] && (has[PausedScopeMentor] || has[PausedScopeDeep] || has[PausedScopeAI] || has[PausedScopeSwing]):
		return PausedScopeMixed
	case has[PausedScopeMentor]:
		return PausedScopeMentor
	case has[PausedScopeDeep], has[PausedScopeAI] && has[PausedScopeSwing]:
		return PausedScopeDeep
	case has[PausedScopeAI]:
		return PausedScopeAI
	case has[PausedScopeSwing]:
		return PausedScopeSwing
	case has[PausedScopeTraders]:
		return PausedScopeTraders
	}
	return ""
}

// DataTruthPausedScope is the pause scope /api/health reports for ONE blocking
// data-truth check, from what the bot really does with it (the gate obeys a
// reason only for the readers it names and only for the traders/accounts it is
// for - trader/data_truth_gate.go dataTruthReasons): a trader/account-scoped block
// is PausedScopeTraders; a block naming the mentor reader only is PausedScopeMentor,
// the AI reader only PausedScopeAI; the swing line check (no readers) is the
// SWING4H scope; everything else - and any reader this function does not know
// (fail wide) - is PausedScopeAll. A banner never claims more than the bot refuses.
func DataTruthPausedScope(check string, readers, scopes []string) string {
	if len(scopes) > 0 {
		return PausedScopeTraders
	}
	if len(readers) == 0 {
		if check == checks.CheckLookbackSwing4h {
			return PausedScopeSwing
		}
		return PausedScopeAll
	}
	mentor, ai := false, false
	for _, r := range readers {
		switch r {
		case datatruth.ReaderMentor:
			mentor = true
		case datatruth.ReaderAI:
			ai = true
		default:
			return PausedScopeAll
		}
	}
	switch {
	case mentor && ai:
		return PausedScopeAll
	case mentor:
		return PausedScopeMentor
	}
	return PausedScopeAI
}

// mergeIntegrityLegs joins the verdicts of several legs into one explanation:
// hole-class legs contribute their holes (de-duplicated, the LATER clear time
// when a hole is in both), otherwise the first unknown text.
func mergeIntegrityLegs(legs []IntegrityVerdict) IntegrityVerdict {
	var out IntegrityVerdict
	type hk struct {
		c    string
		f, t int64
	}
	idx := map[hk]int{}
	for _, v := range legs {
		if !v.Blocked {
			continue
		}
		out.Blocked = true
		if v.Class == IntegrityClassHole {
			out.Class = IntegrityClassHole
			for _, h := range v.Holes {
				k := hk{h.Contract, h.FromMs, h.ToMs}
				if i, ok := idx[k]; ok {
					if h.ClearsAtMs > out.Holes[i].ClearsAtMs {
						out.Holes[i].ClearsAtMs = h.ClearsAtMs
					}
					continue
				}
				idx[k] = len(out.Holes)
				out.Holes = append(out.Holes, h)
			}
		} else if out.Class == "" {
			out.Class, out.Unknown = v.Class, v.Unknown
		}
	}
	sort.Slice(out.Holes, func(i, j int) bool { return out.Holes[i].FromMs < out.Holes[j].FromMs })
	return out
}

// PausedView is one symbol's pause state as /api/health serves it.
type PausedView struct {
	Symbol string `json:"symbol"`
	Paused bool   `json:"paused"`
	Scope  string `json:"scope,omitempty"` // the WIDEST paused scope (WidestPausedScope)
	// Scopes — every leg that is blocked, so a narrower one is never lost behind
	// a wider one (round 5, DEF-B).
	Scopes  []string       `json:"scopes,omitempty"`
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
		intra := t.integrity(s, IntegrityIntradayOpenMin, now, false)
		deep := t.integrity(s, IntegrityDeepOpenMin, now, false)
		swing := t.integrity(s, IntegritySwingOpenMin, now, false)
		// the mentor's key-level leg (an RTH hole anywhere in the stored
		// history) pauses MENTOR entries only: the AI / planner / Picture paths
		// never read the seeded key levels
		keyLv := t.integrity(s, IntegrityIntradayOpenMin, now, true)
		pv := PausedView{Symbol: s, Reasons: []PausedReason{}}
		// EVERY blocked leg is named (round 5, DEF-B): the scope used to be the
		// first leg in a fixed order, so a mentor-leg pause hid behind a deep
		// one and the banner claimed "intraday mentor setups trade normally"
		// while the mentor gate refused them.
		var legs []IntegrityVerdict // the verdicts that explain the pause
		if intra.Blocked {
			pv.Scopes = append(pv.Scopes, PausedScopeAll)
		}
		if deep.Blocked {
			pv.Scopes = append(pv.Scopes, PausedScopeDeep)
		}
		if keyLv.Blocked {
			pv.Scopes = append(pv.Scopes, PausedScopeMentor)
		}
		if swing.Blocked {
			pv.Scopes = append(pv.Scopes, PausedScopeSwing)
		}
		pv.Scope = WidestPausedScope(pv.Scopes)
		switch {
		case intra.Blocked:
			legs = []IntegrityVerdict{intra}
		case deep.Blocked && keyLv.Blocked:
			legs = []IntegrityVerdict{deep, keyLv} // both stand: the holes of both, each with its own clear time
		case deep.Blocked:
			legs = []IntegrityVerdict{deep}
		case keyLv.Blocked:
			legs = []IntegrityVerdict{keyLv}
		case swing.Blocked:
			legs = []IntegrityVerdict{swing}
		}
		if len(legs) > 0 {
			worst := mergeIntegrityLegs(legs)
			pv.Paused = true
			pv.Action = IntegrityAction
			r := PausedReason{Class: worst.Class, Window: worst.Windows()}
			if worst.Class == IntegrityClassHole {
				pv.Action = IntegrityActionFor(worst.Holes) // the remedy that works for THESE holes (round 4)
				r.Detail = fmt.Sprintf("%d hole(s) in the data window", len(worst.Holes))
				for _, h := range worst.Holes {
					if lb := h.ClearsAtLabel(); lb != "" {
						r.ClearsAt = append(r.ClearsAt, h.Window+" clears by itself "+lb)
					}
				}
			} else {
				r.Detail = worst.Unknown
			}
			pv.Reasons = append(pv.Reasons, r)
		}
		views = append(views, pv)
	}
	return views, true
}
