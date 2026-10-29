package trader

import (
	"fmt"
	"strings"
	"sync"
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
//	mentor arm, SWING4H                    admitChain  (admitArm+Swing)       deep
//	mentor arm AUTHORING                   mentorPlaceIntent                  intraday | deep (swing)
//	mentor armed-pass placement            mentorOnlyPlacementPass            intraday
//	SIM test seams (TestArmPlace / Stop)   dataIntegrityTestSeamRefusal       deep
//	debug test trade (POST /api/debug/     DebugNTTestTrade                   deep
//	  nt-test-trade, a MARKET entry)         (the only door api/ has to the wire)
//
// WINDOWS (code-derived, in OPEN 1m minutes before now):
//
//	intraday = ntTrader.IntegrityIntradayOpenMin = 1500 = mentorBars1mDepth.
//	  The mentor evaluator reads the live 1m ring (market.FuturesBarsProvider
//	  ("MNQ","1m",mentorBars1mDepth), mentor_tick.go) and AGGREGATES every other
//	  frame from it (2m/5m/15m/1h/4h, the §7 Globex run window 17:00→08:30,
//	  the RTH day, boxes, ORB, day latch). The 5m feed it also reads
//	  (mentor_tick.go, "5m", 12 bars) lies inside the same minutes.
//	deep = ntTrader.IntegrityDeepOpenMin = 34 x 240 = 8160.
//	  SWING4H reads the 4h EMA 34 (seeded from the stored 1m, mentor_tick.go
//	  mentorSeedBars1mN); the AI / planner / Picture / agent paths read the
//	  native 4h structure (kernel.HTFVetoNativeTF, HTFVetoNativeBarsAsk) and
//	  1m x kernel.AISVPBarCount (2000, inside this). 34 candles is the EMA's own
//	  period; older candles carry < 0.75% weight each.
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

// integrityVerdictFn is the tracker read (ntTrader.IntegrityVerdictFor); a var
// so the trader-side pins can feed a verdict without a socket fixture. The
// verdict's own pins (the real tracker, the real calendar, the 10-26 data) live
// in trader/ninjatrader/bar_integrity_test.go.
var integrityVerdictFn = ntTrader.IntegrityVerdictFor

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
}

func (v dataVerdict) isData() bool {
	return v.Class == ntTrader.IntegrityClassHole || v.Class == ntTrader.IntegrityClassUnknown
}

// integrityWindowFor picks the window the admitted path reads.
func integrityWindowFor(in admitIntent) int {
	if in.Path == admitArm && in.MentorArm && !in.Swing {
		return ntTrader.IntegrityIntradayOpenMin
	}
	return ntTrader.IntegrityDeepOpenMin
}

// mentorIntegrityWindow is the window a mentor intent reads: the SWING4H setup
// (4h EMA 34) the deep one, every intraday setup the 1m series the evaluator
// aggregates.
func mentorIntegrityWindow(in mentor.Intent) int {
	if strings.EqualFold(strings.TrimSpace(in.Setup), "SWING4H") || strings.HasPrefix(strings.TrimSpace(in.ArmID), "swing-") {
		return ntTrader.IntegrityDeepOpenMin
	}
	return ntTrader.IntegrityIntradayOpenMin
}

// integrityApplies — NinjaTrader futures traders only; unarmed (no NT8 bridge)
// the verdict is inert, like the stale-feed block without a bar provider.
func (at *AutoTrader) integrityApplies() bool {
	return at != nil && at.exchange == "ninjatrader" && ntTrader.IntegrityArmed()
}

// dataIntegrityVerdictAt is THE verdict. Legs: (c) stale feed — when withStale,
// through the mentor block (its episode lines kept) for a mentor trader, else
// the arm path's armFeedStale; then (a)/(b) from the hole tracker.
func (at *AutoTrader) dataIntegrityVerdictAt(openMin int, now time.Time, withStale bool) dataVerdict {
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
	v := integrityVerdictFn(sym, openMin, now)
	if !v.Blocked {
		return dataVerdict{}
	}
	out := dataVerdict{Blocked: true, Class: v.Class, Holes: v.Holes, Windows: v.Windows()}
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
	unfillable := false
	for _, h := range v.Holes {
		parts = append(parts, fmt.Sprintf("%s (%d open min, %s)", h.Window, h.Missing, h.State))
		unfillable = unfillable || h.Unfillable
	}
	s := fmt.Sprintf("missing %s 1m bars %s — %s", sym, strings.Join(parts, "; "), ntTrader.IntegrityAction)
	if unfillable {
		s += "; NT8 has no history the bot can ask for, so trading stays paused until the stretch leaves the data window or the history is reloaded and re-measured"
	}
	return s
}

// dataIntegrityAdmitRefusal is admitChain's gate: ("", false) = may proceed.
// Legs (a) and (b); the stale leg stays at the chain's own steps (mentor block
// at its seams, armFeedStale as the last step) so every policy refusal keeps
// naming itself first.
func (at *AutoTrader) dataIntegrityAdmitRefusal(in admitIntent, now time.Time) (class, text string, refused bool) {
	if !at.integrityApplies() {
		return "", "", false
	}
	v := at.dataIntegrityVerdictAt(integrityWindowFor(in), now, false)
	if !v.Blocked {
		return "", "", false
	}
	return v.Class, "data_integrity: " + v.Reason, true
}

// dataIntegrityTestSeamRefusal gates the SIM test-arm seams (TestArmPlace /
// TestArmPlaceStop): they put a real order on the SIM wire outside admitEntry.
func (at *AutoTrader) dataIntegrityTestSeamRefusal(now time.Time) string {
	if v := at.dataIntegrityVerdictAt(ntTrader.IntegrityDeepOpenMin, now, false); v.Blocked {
		return "data_integrity: " + v.Reason
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
}

// integrityWindows is every window this trader's entries read.
func (at *AutoTrader) integrityWindows() []int {
	if at.mentorEnabled() {
		return []int{ntTrader.IntegrityIntradayOpenMin, ntTrader.IntegrityDeepOpenMin}
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
	var worst dataVerdict
	for _, w := range at.integrityWindows() {
		v := at.dataIntegrityVerdictAt(w, now, false)
		if v.Blocked && (!worst.Blocked || (v.Class == ntTrader.IntegrityClassHole && worst.Class != ntTrader.IntegrityClassHole)) {
			worst = v
		}
	}
	sym := at.futuresSymbol()
	e := &at.integrity
	e.mu.Lock()
	defer e.mu.Unlock()
	if worst.Blocked {
		if worst.Class == ntTrader.IntegrityClassUnknown {
			if e.unknownSince.IsZero() {
				e.unknownSince = now
			}
		} else {
			e.unknownSince = time.Time{}
		}
		if !e.active {
			e.active, e.since, e.class = true, now, worst.Class
			e.key = fmt.Sprintf("data-paused|%s|%d", at.id, now.Unix())
			e.pushed = false
			telemetry.IncDataIntegrityEpisode(worst.Class)
			at.logWarnf("⏸ TRADING PAUSED (%s): %s — NEW entries refused on every path; resting entry orders are withdrawn; exits, stops, BE/trail moves, closes and protection are unaffected. Resumes by itself once the bars are measured filled.", worst.Class, worst.Reason)
		} else if worst.Class == ntTrader.IntegrityClassHole && e.class != worst.Class {
			e.class = worst.Class // unknown -> a named hole: the owner message below now has a window to give
			telemetry.IncDataIntegrityEpisode(worst.Class)
		}
		if !e.pushed && kernel.IsCMEOpen(now) {
			ready := worst.Class == ntTrader.IntegrityClassHole ||
				(worst.Class == ntTrader.IntegrityClassUnknown && now.Sub(e.unknownSince) >= integrityUnknownGrace)
			if ready {
				e.pushed = true
				ownerpush.Push(e.key, fmt.Sprintf("⏸ vl TRADING PAUSED — %s: %s. New entries are blocked and resting entry orders are being cancelled; exits, stops and protection are unaffected.",
					at.ownerPushTraderLabel(), worst.Reason))
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
		at.logInfof("▶ TRADING RESUMED: %s bar data is complete again in every data window (paused %s) — NEW entries are allowed again.", sym, held)
		if wasPushed {
			ownerpush.Push("data-resumed|"+strings.TrimPrefix(key, "data-paused|"),
				fmt.Sprintf("✅ vl TRADING RESUMED — %s: %s bar data is complete again (paused %s).", at.ownerPushTraderLabel(), sym, held))
		} else {
			ownerpush.Cancel(key) // never delivered the pause: do not deliver it after the resume
		}
	}
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
	ws := at.integrityWindows()
	for i, w := range ws {
		v := at.dataIntegrityVerdictAt(w, now, false)
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
