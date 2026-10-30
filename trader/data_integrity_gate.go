package trader

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/internal/ownerpush"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
)

// ── DATA-INTEGRITY GATE, TRADER SIDE (m445, release #54) ────────────────────
//
// Owner, 2026-10-29: "this is a financial product, not a game — nothing
// related to data loss can continue to trade."
//
// ONE fail-closed verdict per symbol (ntTrader.IntegrityVerdictFor: a hole that
// is not filled overlaps the data window, OR the detector is unknown) joined
// with the stale-feed condition, consulted by EVERY path that can open risk:
//
//	path                                   seam                               window
//	AI decision (open_long/open_short)     admitChain  (admitDecision)        deep
//	agent-chat manual entry                admitChain  (admitAgent)           deep
//	planner / Day-Plan arm placement       admitChain  (admitArm, via         deep
//	  (limit, stop-entry, market_in_zone)    armAdmitted at all 4 wire sites)
//	Picture HTF entry                      admitChain  (admitPicture)         deep
//	mentor arm, intraday setups            admitChain  (admitArm+MentorArm)   intraday
//	mentor arm, SWING4H                    admitChain  (admitArm+Swing)       swing
//	mentor arm AUTHORING                   mentorPlaceIntent                  intraday | swing
//	mentor armed-pass placement            mentorOnlyPlacementPass            intraday
//	SIM test seams (TestArmPlace / Stop)   dataIntegrityTestSeamRefusal       deep
//	debug test trade (POST /api/debug/     DebugNTTestTrade                   deep
//	  nt-test-trade, a MARKET entry)         (the only door api/ has to the wire)
//
// WINDOWS (code-derived, in OPEN 1m minutes before now):
//
//	intraday = ntTrader.IntegrityIntradayOpenMin = 1500 = mentorBars1mDepth.
//	  The mentor evaluator reads the live 1m ring (market.FuturesBarsProvider
//	  ("MNQ","1m",mentorBars1mDepth), mentor_tick.go) and AGGREGATES the frames
//	  it builds PER PASS from it (2m/5m/15m/1h/4h, the §7 Globex run window
//	  17:00→08:30, the RTH day, boxes, ORB). The 5m feed it also reads
//	  (mentor_tick.go, "5m", 12 bars) lies inside the same minutes. The SEEDED
//	  state is not inside this slice: see the key-level leg below.
//	deep = ntTrader.IntegrityDeepOpenMin = 12000 = plannerCandleTapeBars.
//	  Per-reader table in trader/ninjatrader/bar_integrity.go (round 4, D2). The
//	  AI / planner / Picture / agent paths read a NATIVE 4h frame (kernel
//	  .HTFVetoNativeTF — a 1m hole cannot corrupt it), 1m x kernel.AISVPBarCount
//	  (2000) and the structure map's 1h/15m/5m rebuilt from the 1m store
//	  (structureMapBarsAsk 2500); the WIDEST 1m reader is the planner's candle
//	  tape (plannerCandleTapeBars 12000: 8-session daily table, weekly line,
//	  realized-vol baseline), so one window serves the non-mentor paths.
//	key-level legs (every MENTOR verdict, any window) — the evaluator's seeded
//	  state (1H RTH key levels with "No lookback cap", open-HTF lines, day
//	  latch, trendlines) is built once from the contract-STITCHED 1m history
//	  (mentor_tick.go mentorFullKeyLevelHistory) and persists for the life of
//	  the process, so the 1500-minute intraday window does not cover it (review
//	  round 2, reviewer A, D1). (1) the tracker's own RTH holes of any age
//	  (ntTrader.IntegrityVerdictForMentor); (2) the RTH holes in the stitched
//	  series the seed walked, measured at every seed and re-seed
//	  (data_integrity_seed.go, ntTrader.SeriesRTHHoles) within the 90-day reach
//	  (ntTrader.IntegritySeedReachDays = the 1m retention). A hole a history
//	  reload fills bumps the refill generation, the evaluator re-seeds, the next
//	  measurement is clear. Known limit: a hole older than the reach is not
//	  judged (nothing the owner can do refills it).
//	swing = ntTrader.IntegritySwingOpenMin = (102+1) x 240 = 24720.
//	  SWING4H's 4h EMA 34 is gated on, and computed over, the WARM-UP history
//	  (kernel/mentor.FourHEMA34Warmup = 102 closed 4h candles, seeded from the
//	  stored 1m, mentor_tick.go mentorSeedBars1mN) — a candle 34 back still
//	  carries ~0.8% of the EMA and everything older ~14% together.
//
// NEVER blocked or delayed by this gate: exits, stops, BE/trail moves, closes,
// protection, flatten, reconciliation. It sits in the ENTRY chain only; the
// monitor beat below only CANCELS resting entries.
//
// BLOCKED also means: resting (unfilled) entry orders for the symbol are
// withdrawn with a named reason (withdraw.go, the existing entry-only cancel),
// ONE WARN + ONE owner push per episode, ONE INFO + push on resume, a counter
// per reason, /api/health trading_paused, and the dashboard banner. Resume is
// automatic and only on a MEASUREMENT (the hole tracker re-read the window from
// the store and found it filled) or when the hole ages out of the window — no
// persisted "all clear": a restart re-derives everything from the tracker.

// integrityVerdictFn is the tracker read (ntTrader.IntegrityVerdictFor, or for
// a MENTOR trader ntTrader.IntegrityVerdictForMentor — keyLevels true: the
// tracker's RTH holes of any age block it; the seeded-series leg follows in
// dataIntegrityVerdictAt); a var so the trader-side pins can feed a verdict
// without a socket fixture. The verdict's own pins (the real tracker, the real
// calendar, the 10-26 data) live in trader/ninjatrader/bar_integrity_test.go.
var integrityVerdictFn = func(symbol string, openMin int, now time.Time, keyLevels bool) ntTrader.IntegrityVerdict {
	if keyLevels {
		return ntTrader.IntegrityVerdictForMentor(symbol, openMin, now)
	}
	return ntTrader.IntegrityVerdictFor(symbol, openMin, now)
}

// integrityUnknownGrace — how long an UNKNOWN detector may stand (CME open)
// before it is told to the owner and resting entries are withdrawn. A normal
// boot scan lands with the first live 1m bar; the gate itself refuses from the
// first second, but a restart must not push or cancel on every boot.
const integrityUnknownGrace = 5 * time.Minute

// dataVerdict is the joined verdict for one symbol and window.
type dataVerdict struct {
	Blocked bool
	Class   string // data_integrity_hole | data_integrity_unknown | stale_data | clock_step
	Reason  string // the refusal text
	Holes   []ntTrader.IntegrityHole
	Windows []string // the holes' windows, CT
	// Anchored marks a verdict read on a window whose START is a fixed instant
	// (pep: the box start minus the warm-up) and which therefore GROWS with now.
	// A hole inside such a window never "clears by itself": the tracker's clear
	// time is for a fixed-size window that slides, so it is stripped and the
	// wording says the truth (rereview m461 P2, GATES lens).
	Anchored bool
}

func (v dataVerdict) isData() bool {
	return v.Class == ntTrader.IntegrityClassHole || v.Class == ntTrader.IntegrityClassUnknown || v.Class == integrityClassDataTruth
}

// integrityWindowFor picks the window the admitted path reads.
func integrityWindowFor(in admitIntent) int {
	if in.Path == admitArm && in.MentorArm {
		if in.Swing {
			return ntTrader.IntegritySwingOpenMin
		}
		return ntTrader.IntegrityIntradayOpenMin
	}
	return ntTrader.IntegrityDeepOpenMin
}

// mentorIntegrityWindow is the window a mentor intent reads: the SWING4H setup
// (4h EMA 34) the deep one, every intraday setup the 1m series the evaluator
// aggregates.
func mentorIntegrityWindow(in mentor.Intent) int {
	if strings.EqualFold(strings.TrimSpace(in.Setup), "SWING4H") || strings.HasPrefix(strings.TrimSpace(in.ArmID), "swing-") {
		return ntTrader.IntegritySwingOpenMin
	}
	return ntTrader.IntegrityIntradayOpenMin
}

// integrityApplies — NinjaTrader futures traders only; unarmed (no NT8 bridge)
// the verdict is inert, like the stale-feed block without a bar provider.
func (at *AutoTrader) integrityApplies() bool {
	return at != nil && at.exchange == "ninjatrader" && ntTrader.IntegrityArmed()
}

// dataIntegrityVerdictAt is THE entry verdict: the m445 legs (integrityVerdictAt
// — stale feed, missing bars, unknown detector) and, only when those are clear,
// the m446/m447 data-truth leg (dataTruthVerdictAt — bad print, frame mismatch,
// indicator lookback, ingest loss, contract roll, broker agreement, fill price).
// Release #55 (integrator): every path that asks "may I open risk?" crosses this
// ONE function, so folding the data-truth leg here is what makes every entry path
// consult it. The pause-episode beat and the withdraw decision ask the m445 legs
// alone (integrityVerdictAt): data-truth carries its own episode and owner alert,
// and does not withdraw resting entries (m446: it pauses NEW entries only).
func (at *AutoTrader) dataIntegrityVerdictAt(openMin int, now time.Time, withStale bool) dataVerdict {
	if v := at.integrityVerdictAt(openMin, now, withStale); v.Blocked {
		return v
	}
	return at.dataTruthVerdictAt(openMin, now)
}

// integrityVerdictAt is the m445 verdict. Legs: (c) stale feed — when withStale,
// through the mentor block (its episode lines kept) for a mentor trader, else
// the arm path's armFeedStale; then (a)/(b) from the hole tracker.
func (at *AutoTrader) integrityVerdictAt(openMin int, now time.Time, withStale bool) dataVerdict {
	if at == nil {
		return dataVerdict{}
	}
	if withStale {
		stale, why := false, ""
		if at.mentorEnabled() {
			stale, why = at.mentorStaleDataBlockedWhy(now)
		} else {
			stale, why = at.armFeedStale(now)
		}
		if stale {
			class := "stale_data"
			if strings.HasPrefix(why, "clock_step") {
				class = "clock_step"
			}
			return dataVerdict{Blocked: true, Class: class, Reason: why}
		}
	}
	if !at.integrityApplies() {
		return dataVerdict{}
	}
	sym := at.futuresSymbol()
	// a mentor trader decides on the evaluator's SEEDED state (key levels over the
	// whole stored history) as well as the per-pass window: it asks the mentor verdict
	v := integrityVerdictFn(sym, openMin, now, at.mentorEnabled())
	if !v.Blocked {
		if at.mentorEnabled() {
			return at.seedSeriesVerdict(now) // the key-level leg: holes in the series the seed walked
		}
		return dataVerdict{}
	}
	if at.pepEnabled() && v.Class == ntTrader.IntegrityClassHole {
		// pep's window is anchored at a fixed start and grows (pep_admit.go): the
		// tracker's ClearsAtMs belongs to a sliding fixed-size window, so no hole
		// of a pep verdict is promised to clear by itself. Copy: the tracker's own
		// slice is shared.
		holes := make([]ntTrader.IntegrityHole, len(v.Holes))
		copy(holes, v.Holes)
		for i := range holes {
			holes[i].ClearsAtMs = 0
		}
		v.Holes = holes
	}
	out := dataVerdict{Blocked: true, Class: v.Class, Holes: v.Holes, Windows: v.Windows(), Anchored: at.pepEnabled()}
	if v.Class == ntTrader.IntegrityClassHole {
		out.Reason = integrityHoleReason(sym, v)
	} else {
		out.Reason = fmt.Sprintf("missing-data check UNKNOWN for %s: %s (fail-closed)", sym, v.Unknown)
	}
	return out
}

// integrityHoleReason is the refusal / push text for a hole verdict.
func integrityHoleReason(sym string, v ntTrader.IntegrityVerdict) string {
	parts := make([]string, 0, len(v.Holes))
	for _, h := range v.Holes {
		clears := ""
		if lb := h.ClearsAtLabel(); lb != "" {
			clears = ", clears by itself " + lb
		}
		parts = append(parts, fmt.Sprintf("%s (%d open min, %s%s)", h.Window, h.Missing, h.State, clears))
	}
	// the remedy is per hole (round 4): a reload only for a stretch NT8 can still
	// replay — for an older one it is stated what DOES clear it, and when
	return fmt.Sprintf("missing %s 1m bars %s — %s", sym, strings.Join(parts, "; "), ntTrader.IntegrityActionFor(v.Holes))
}

// dataIntegrityAdmitRefusal is admitChain's gate: ("", false) = may proceed.
// Legs (a) and (b); the stale leg stays at the chain's own steps (mentor block
// at its seams, armFeedStale as the last step) so every policy refusal keeps
// naming itself first.
func (at *AutoTrader) dataIntegrityAdmitRefusal(in admitIntent, now time.Time) (class, text string, refused bool) {
	var v dataVerdict
	if in.Path == admitPep {
		// PEP reads the box and a short EMA warm-up: its OWN window and reader
		// (pep_admit.go), never the 12000-minute AI window.
		v = at.pepDataVerdictAt(now, false)
	} else {
		v = at.dataIntegrityVerdictAt(integrityWindowFor(in), now, false)
	}
	if !v.Blocked {
		return "", "", false
	}
	return v.Class, v.refusalPrefix() + v.Reason, true
}

// refusalPrefix names the refusal: data_integrity for the m445 legs, data_truth
// for the m446/m447 checks.
func (v dataVerdict) refusalPrefix() string {
	if v.Class == integrityClassDataTruth {
		return "data_truth: "
	}
	return "data_integrity: "
}

// dataIntegrityTestSeamRefusal gates the SIM test-arm seams (TestArmPlace /
// TestArmPlaceStop): they put a real order on the SIM wire outside admitEntry.
func (at *AutoTrader) dataIntegrityTestSeamRefusal(now time.Time) string {
	if v := at.dataIntegrityVerdictAt(ntTrader.IntegrityDeepOpenMin, now, false); v.Blocked {
		return v.refusalPrefix() + v.Reason
	}
	return ""
}

// DebugNTTestTrade is the ONLY door POST /api/debug/nt-test-trade has to the
// wire: a deterministic 1-contract MARKET entry with a bracket on the NT8 SIM
// account. It "bypasses the AI + risk gate" by design, so it must cross the
// data-integrity verdict here — a SIM test order is still an entry, and a
// market entry over missing bars is the worst kind (review round 1, D1). The
// api package must never call TCPTrader.DebugPlaceTestTrade itself
// (api/data_integrity_debug_door_test.go pins that).
func (at *AutoTrader) DebugNTTestTrade(side string) (map[string]interface{}, error) {
	if reason := at.dataIntegrityTestSeamRefusal(time.Now()); reason != "" {
		return nil, fmt.Errorf("test-trade denied: %s", reason)
	}
	nt := at.armedTrader()
	if nt == nil {
		return nil, fmt.Errorf("trader is not an NT8 TCP trader (NT_TRANSPORT=tcp required)")
	}
	return nt.DebugPlaceTestTrade(side)
}

// ── the monitor beat: episodes, push, withdraw ──────────────────────────────

type integrityEpisode struct {
	mu           sync.Mutex
	active       bool
	since        time.Time
	class        string
	key          string
	unknownSince time.Time
	pushed       bool
	scope        string // pauseScopeAll | pauseScopeSwing — what the standing pause really refuses

	// m452 (data_beats_bounded.go): one data-gate beat in flight at a time.
	beatInFlight  atomic.Bool
	beatStallSaid atomic.Bool
}

// What a standing pause refuses (review round 6, D-new1). A MENTOR trader's two
// windows are different readers: blocked in the intraday window = every mentor
// setup is refused; blocked ONLY in the swing window = SWING4H alone. The log,
// the owner push and the resume text used to say "every path" whatever the
// window — on the deploy scenario (10-26 hole, swing window only) they claimed
// a pause of intraday setups that trade normally.
const (
	pauseScopeAll   = "all"
	pauseScopeSwing = "swing"
)

// pauseWording is the owner-facing text of one pause, derived from its scope
// and its governing verdict so the WARN log, the push and the scope-change
// lines can never disagree.
type pauseWording struct {
	refuses  string // what is refused
	withdraw string // what is cancelled
	tail     string // when it ends
}

func integrityPauseWording(scope string, gov dataVerdict) pauseWording {
	w := pauseWording{
		refuses:  "NEW entries refused on every path",
		withdraw: "resting entry orders are withdrawn",
	}
	if scope == pauseScopeSwing {
		w.refuses = "NEW SWING4H entries refused (intraday mentor setups trade normally)"
		w.withdraw = "only resting SWING4H entry orders are withdrawn"
	}
	if gov.Anchored {
		// PEP: nothing rests an entry order (the entry is one market order with
		// its bracket), and the window never slides past a hole today.
		w.withdraw = "pep rests no entry order, so nothing is withdrawn"
	}
	// the end of the pause is the governing hole's own clear time, or an import /
	// fill — never a promise the data does not keep
	if gov.Class == ntTrader.IntegrityClassHole && gov.Anchored {
		w.tail = "Pep reads the data from before the box start up to now, so this hole keeps NEW pep entries paused for the rest of the CT day; it ends sooner only when the missing minutes are measured filled in (imported). The next CT day's window starts after it."
	} else if gov.Class == ntTrader.IntegrityClassHole {
		var latest *ntTrader.IntegrityHole
		complete := len(gov.Holes) > 0
		for i := range gov.Holes {
			h := &gov.Holes[i]
			if h.ClearsAtMs <= 0 {
				complete = false
				continue
			}
			if latest == nil || h.ClearsAtMs > latest.ClearsAtMs {
				latest = h
			}
		}
		if complete && latest != nil {
			w.tail = "Clears by itself " + latest.ClearsAtLabel() + ", or sooner when the missing minutes are filled in (imported)."
		} else {
			w.tail = "Clears when the missing minutes are measured filled in (imported), or the hole leaves the data window."
		}
	} else {
		w.tail = "Resumes once the missing-data check has judged the window."
	}
	return w
}

// integrityPauseScope — what the standing pause refuses for this trader, given
// the verdict of each window in integrityWindows(now) order.
func (at *AutoTrader) integrityPauseScope(blocked []bool) string {
	if at.mentorEnabled() && len(blocked) > 1 && !blocked[0] {
		return pauseScopeSwing
	}
	return pauseScopeAll
}

// integrityWindows is every window this trader's entries read.
func (at *AutoTrader) integrityWindows(now time.Time) []int {
	if at.pepEnabled() {
		// a pep trader enters only through the pep path, on pep's own window
		return []int{at.pepIntegrityOpenMinAt(now)}
	}
	if at.mentorEnabled() {
		// a mentor trader enters only through mentor arms: intraday setups and
		// SWING4H (the swing window contains the deep one)
		return []int{ntTrader.IntegrityIntradayOpenMin, ntTrader.IntegritySwingOpenMin}
	}
	return []int{ntTrader.IntegrityDeepOpenMin}
}

// dataIntegrityBeat runs on the 60 s monitor beat (monitorTick): it advances
// the pause episode — ONE WARN + ONE owner push at the start, ONE INFO + push
// at the end — even when no entry is being attempted. Idempotent.
func (at *AutoTrader) dataIntegrityBeat(now time.Time) {
	if !at.integrityApplies() {
		return
	}
	var worst, gov dataVerdict
	var blockedAt []bool
	for _, w := range at.integrityWindows(now) {
		v := at.integrityVerdictAt(w, now, false)
		blockedAt = append(blockedAt, v.Blocked)
		if v.Blocked && !gov.Blocked {
			gov = v // the first blocked window governs the scope and its end
		}
		if v.Blocked && (!worst.Blocked || (v.Class == ntTrader.IntegrityClassHole && worst.Class != ntTrader.IntegrityClassHole)) {
			worst = v
		}
	}
	sym := at.futuresSymbol()
	e := &at.integrity
	e.mu.Lock()
	defer e.mu.Unlock()
	if worst.Blocked {
		scope := at.integrityPauseScope(blockedAt)
		wd := integrityPauseWording(scope, gov)
		// The unknown-detector grace is measured in OPEN time only (review round
		// 2, reviewer B probe): a restart over the weekend sits unknown through
		// the whole closure — nothing arrives to judge — and counting that would
		// push "UNKNOWN" and withdraw resting entries 20 s after the Sunday
		// reopen, before the first live bar had a chance to land the boot scan.
		// The clock starts at the first beat that finds the market OPEN.
		if worst.Class == ntTrader.IntegrityClassUnknown && kernel.IsCMEOpen(now) {
			if e.unknownSince.IsZero() {
				e.unknownSince = now
			}
		} else {
			e.unknownSince = time.Time{}
		}
		if !e.active {
			e.active, e.since, e.class, e.scope = true, now, worst.Class, scope
			// m452: the trader id is part of the owner-push KIND (the text before the
			// first "|" is ownerpush's 3-per-hour budget bucket). "data-paused|<id>|ts"
			// put every trader on one MNQ hole in ONE bucket, so three traders spent the
			// whole budget on their first pushes and the widen / narrow / resume pushes
			// (or a second hole in the hour) were held for up to 90 minutes.
			e.key = ownerPushPausedKey(at.id, now)
			e.pushed = false
			telemetry.IncDataIntegrityEpisode(worst.Class)
			at.logWarnf("⏸ TRADING PAUSED (%s): %s — %s; %s; exits, stops, BE/trail moves, closes and protection are unaffected. %s", worst.Class, worst.Reason, wd.refuses, wd.withdraw, wd.tail)
		} else if worst.Class == ntTrader.IntegrityClassHole && e.class != worst.Class {
			e.class = worst.Class // unknown -> a named hole: the owner message below now has a window to give
			telemetry.IncDataIntegrityEpisode(worst.Class)
		}
		if e.active && e.scope != scope {
			// the standing pause WIDENED or NARROWED (a window cleared or a new
			// hole reached the intraday window): say what it refuses NOW — the
			// owner was told the earlier scope, and a widening pause is news
			prev := e.scope
			e.scope = scope
			if scope == pauseScopeAll {
				at.logWarnf("⏸ TRADING PAUSE WIDENED (%s -> every entry path): %s — %s; %s. %s", prev, worst.Reason, wd.refuses, wd.withdraw, wd.tail)
				if e.pushed && kernel.IsCMEOpen(now) {
					ownerpush.Push(e.key+"|widen", fmt.Sprintf("⏸ vl TRADING PAUSE WIDENED — %s: %s. %s; %s; exits, stops and protection are unaffected. %s",
						at.ownerPushTraderLabel(), worst.Reason, wd.refuses, wd.withdraw, wd.tail))
				}
			} else {
				at.logInfof("▶ TRADING PAUSE NARROWED (every entry path -> SWING4H only): intraday mentor setups trade normally again. %s", wd.tail)
				if e.pushed && kernel.IsCMEOpen(now) {
					ownerpush.Push(e.key+"|narrow", fmt.Sprintf("▶ vl TRADING PAUSE NARROWED — %s: intraday mentor setups trade normally again; %s. %s",
						at.ownerPushTraderLabel(), wd.refuses, wd.tail))
				}
			}
		}
		if !e.pushed && kernel.IsCMEOpen(now) {
			ready := worst.Class == ntTrader.IntegrityClassHole ||
				(worst.Class == ntTrader.IntegrityClassUnknown && now.Sub(e.unknownSince) >= integrityUnknownGrace)
			if ready {
				e.pushed = true
				ownerpush.Push(e.key, fmt.Sprintf("⏸ vl TRADING PAUSED — %s: %s. %s; %s; exits, stops and protection are unaffected. %s",
					at.ownerPushTraderLabel(), worst.Reason, wd.refuses, wd.withdraw, wd.tail))
			}
		}
		return
	}
	e.unknownSince = time.Time{}
	if e.active {
		held := now.Sub(e.since).Round(time.Second)
		key := e.key
		wasPushed := e.pushed
		e.active, e.pushed = false, false
		telemetry.IncDataIntegrityResume()
		// Resume is on a MEASURED fill — or the stretch aged out of the data
		// window, which is NOT the bars coming back, and the text says which
		// (round 3, reviewer B, D4): a hole the tracker still lists unfilled is
		// named as left behind.
		left := ntTrader.IntegrityUnfilledHoles(sym)
		var logTail, pushTail string
		if len(left) == 0 {
			logTail = fmt.Sprintf("%s bar data is complete again in every data window", sym)
			pushTail = fmt.Sprintf("%s bar data is complete again (measured filled)", sym)
		} else {
			ws := make([]string, 0, len(left))
			for _, h := range left {
				ws = append(ws, h.Window)
			}
			logTail = fmt.Sprintf("%s has no missing bars inside the data windows any more — but %s was NOT refilled; it only left the window", sym, strings.Join(ws, "; "))
			pushTail = fmt.Sprintf("%s: no missing bars inside the data window any more. NOT refilled (it only aged out of the window): %s", sym, strings.Join(ws, "; "))
		}
		at.logInfof("▶ TRADING RESUMED: %s (paused %s) — NEW entries are allowed again.", logTail, held)
		if wasPushed {
			ownerpush.Push(ownerPushResumedKey(key),
				fmt.Sprintf("✅ vl TRADING RESUMED — %s: %s (paused %s).", at.ownerPushTraderLabel(), pushTail, held))
		} else {
			ownerpush.Cancel(key) // never delivered the pause: do not deliver it after the resume
		}
	}
}

// ownerPushPausedKey is the dedupe key of a trader's pause episode. The text
// before the first "|" is the owner-push budget kind, so it carries the trader id:
// each trader has its own 3-per-hour bucket (m452).
func ownerPushPausedKey(traderID string, now time.Time) string {
	return fmt.Sprintf("data-paused-%s|%d", traderID, now.Unix())
}

// ownerPushResumedKey is the resume push's key for the pause key `paused`; the
// same per-trader kind shape.
func ownerPushResumedKey(paused string) string {
	return "data-resumed-" + strings.TrimPrefix(paused, "data-paused-")
}

// integrityWithdrawReason says whether resting entries must be withdrawn now
// because of missing data, and whether only SWING rows (the deep window alone
// is blocked). Holes only — and an unknown detector only after
// integrityUnknownGrace — so a restart or a 75 s feed hiccup never churns the
// ledger. The reason is NAMED in the ledger row (withdrawHead).
func (at *AutoTrader) integrityWithdrawReason(now time.Time) (reason string, swingOnly bool) {
	if !at.integrityApplies() {
		return "", false
	}
	ws := at.integrityWindows(now)
	for i, w := range ws {
		v := at.integrityVerdictAt(w, now, false)
		if !v.Blocked {
			continue
		}
		if v.Class == ntTrader.IntegrityClassUnknown {
			at.integrity.mu.Lock()
			since := at.integrity.unknownSince
			at.integrity.mu.Unlock()
			if since.IsZero() || now.Sub(since) < integrityUnknownGrace {
				continue
			}
		}
		// a mentor trader's FIRST window is the intraday one; blocked there =
		// every resting row goes, blocked only in the deep window = swing rows.
		// (Only SWING4H rows read the deep window on a mentor trader because
		// mentor mode refuses every non-mentor entry, so no planner / AI row
		// rests on one; if that ever changes this must withdraw them too.)
		return "data_integrity: " + v.Reason, at.mentorEnabled() && i > 0
	}
	return "", false
}

// isSwingRow reports whether an armed row is the SWING4H setup.
func isSwingRow(r store.ArmedOrderDB) bool {
	return strings.EqualFold(strings.TrimSpace(r.Condition), "SWING4H")
}
