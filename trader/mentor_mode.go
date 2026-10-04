package trader

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
)

// ── MENTOR MODE P3 (owner order 2026-10-02 22:2x, option 2) ────────────────
//
// Mentor mode sizes like the mentor (5 contracts normally, up to 20 on strong
// setups, SIM ONLY). The AI mode keeps the 0B size clamp and its suspended
// exits, byte-identical. mentor_mode is per strategy and OFF by default (L4).
//
// Every refusal and every size choice is LOGGED and COUNTED (tier + why).

// Mentor size-table defaults (CTO defaults; the owner changes them per
// strategy via the knobs above).
const (
	mentorBaseContractsDefault       = 5
	mentorConfluenceContractsDefault = 10
	mentorBigContractsDefault        = 20 // the hard cap
	mentorReducedContractsDefault    = 3
	mentorSwing4HContractsDefault    = 3
	mentorSpentDayContractsDefault   = 2
	mentorMaxContractsDefault        = 20

	mentorTargetBigPts       = 30.0 // target ≥ this for the big tier
	mentorRoomBigMultiple    = 2.0  // room ≥ this × risk for the big tier
	mentorStopTwentiesMinPts = 20.0 // stop 20–25 pts → reduced [D3.3 p1 @ 01:09]
	mentorStopTwentiesMaxPts = 25.0
	// R8 (RULES-FIX v3): a SWING4H stop of 30–60 pts is allowed, ~100 is
	// refused, and SWING4H is EXEMPT from the 25-pt ceiling.
	mentorSwingStopMaxPts = 60.0
	// R9 (RULES-FIX v3): the target is never smaller than the stop; on a spent
	// day (cap 15) any setup whose stop is over 15 is skipped.
	mentorSpentDayStopCapPts = 15.0
)

// mentorTierInputs is everything the size table reads. The trader computes
// these from the intent + evaluator filters; tests pin the table itself.
type mentorTierInputs struct {
	Setup         string  // "ISB", "PHL", "PLH", "SWING4H"
	StopPts       float64 // |entry - stop|
	TargetPts     float64 // |target - entry|
	RoomMultiple  float64 // reward/risk actually available
	Confluence    bool    // box/zone + key level + 5m trigger agreeing (§6)
	HTFAgree      bool    // 4h AND 1h agree
	SpentDay      bool    // §7 spent day
	StrongDay     bool    // S9: 5m candles running 50–80 pts → size 1–2
	ISBOldExtreme bool    // ISB at an old high/low → reduce size, tier 3 [D4.1 p1 rule 2]
	ISBInRange    bool    // ISB traded inside a range → reduce size, tier 3 [D4.1 p1 rule 3]
}

// mentorSizeChoice is the tier decision: contracts, the tier name and why.
type mentorSizeChoice struct {
	Contracts int
	Tier      string
	Why       string
}

// mentorContractsFor is THE size table (pure — the call site the tests pin).
// Order matters: spent → ISB reductions → twenties → big → confluence →
// swing4h → base, every branch capped at maxContracts. A non-positive max
// refuses (fail-closed). B12 (CTO 1791041016051): the stop-in-the-twenties
// cut wins over confluence sizing — a 20–25 pt stop is 3, never 10/20.
func mentorContractsFor(in mentorTierInputs, base, conf, big, reduced, swing4h, spentCap, maxContracts int) (mentorSizeChoice, error) {
	if maxContracts <= 0 {
		return mentorSizeChoice{}, fmt.Errorf("mentor size: mentor_max_contracts not set (max=%d)", maxContracts)
	}
	clamp := func(n int) int {
		if n > maxContracts {
			return maxContracts
		}
		return n
	}
	if in.SpentDay {
		return mentorSizeChoice{Contracts: clamp(spentCap), Tier: "spent_day", Why: "§7 spent day — hold 1–2 only [D5.1 p1 @ 16:13]"}, nil
	}
	// ISB at an old high/low → reduce size, tier 3 (owner ruling 00:1x CT,
	// written rule 2, D4.1 p1). The flag comes from DS-103's evaluator and is
	// only ever set for ISB setups. It beats big/confluence — the location
	// REDUCES whatever the setup would otherwise earn.
	if in.ISBOldExtreme {
		return mentorSizeChoice{Contracts: clamp(3), Tier: "isb_old_extreme", Why: "ISB at an old high/low → reduce size, tier 3 [D4.1 p1 written rule 2]"}, nil
	}
	// ISB traded inside a range → reduce size, tier 3 (written rule 3, D4.1 p1
	// @08:05/09:40: "Khi trade isb in-range bắt buộc giảm size"). Ranks the
	// same as the old-extreme reduction; strong day and spent day (2) win.
	if in.ISBInRange {
		return mentorSizeChoice{Contracts: clamp(3), Tier: "isb_in_range", Why: "ISB traded inside a range → reduce size, tier 3 [D4.1 p1 written rule 3 @08:05/09:40]"}, nil
	}
	// B12 (CTO 1791041016051): a stop in the twenties cuts to 3 and wins over
	// confluence sizing — "reduce size or don't trade" [D3.3 p1 @ 01:09] is a
	// rule about the STOP, so it outranks a 10/20 the confluence would earn.
	if in.StopPts >= mentorStopTwentiesMinPts && in.StopPts <= mentorStopTwentiesMaxPts {
		return mentorSizeChoice{Contracts: clamp(reduced), Tier: "reduced", Why: fmt.Sprintf(
			"stop %.1f pts in the twenties → reduce size or don't trade [D3.3 p1 @ 01:09]", in.StopPts)}, nil
	}
	if in.Confluence && in.HTFAgree && in.RoomMultiple >= mentorRoomBigMultiple && in.TargetPts >= mentorTargetBigPts {
		return mentorSizeChoice{Contracts: clamp(big), Tier: "big", Why: fmt.Sprintf(
			"confluence + 4h&1h agree + room %.1fx ≥ %.0fx + target %.1f pts ≥ %.0f (hard cap %d)",
			in.RoomMultiple, mentorRoomBigMultiple, in.TargetPts, mentorTargetBigPts, maxContracts)}, nil
	}
	if in.Confluence {
		return mentorSizeChoice{Contracts: clamp(conf), Tier: "confluence", Why: "box/zone + key level + 5m trigger agreeing (§6)"}, nil
	}
	if strings.EqualFold(in.Setup, "SWING4H") {
		// S9 (D5.2 p2 @05:21–05:57): a strong day — 5m candles running 50–80
		// pts — cuts the SWING to 1–2 ("50–60 điểm cứ vô 1-2 men kill... nhưng
		// chỉ cùng 4 giờ"): the cut was said of the swing entry, not every
		// setup. The table takes 2, the top of the band.
		if in.StrongDay {
			return mentorSizeChoice{Contracts: clamp(2), Tier: "strong_day", Why: "strong day — the SWING sizes 1–2 [D5.2 p2 @05:21–05:57]"}, nil
		}
		return mentorSizeChoice{Contracts: clamp(swing4h), Tier: "swing4h", Why: "SWING4H setup — 3 [D5.2 p1]"}, nil
	}
	return mentorSizeChoice{Contracts: clamp(base), Tier: "base", Why: fmt.Sprintf(
		"base setup at a location (setup %s, stop %.1f pts)", in.Setup, in.StopPts)}, nil
}

// mentorCounters records every size tier and every refusal for the session
// ("which tier and why" — the spec's audit trail).
var (
	mentorCounterMu sync.Mutex
	mentorCounters  = map[string]int{}
)

func mentorCount(kind string) {
	mentorCounterMu.Lock()
	mentorCounters[kind]++
	mentorCounterMu.Unlock()
}

// MentorCountSnapshot returns a copy of the mentor counters (tests + reports).
func MentorCountSnapshot() map[string]int {
	mentorCounterMu.Lock()
	defer mentorCounterMu.Unlock()
	out := make(map[string]int, len(mentorCounters))
	for k, v := range mentorCounters {
		out[k] = v
	}
	return out
}

// ResetMentorCountersForTest clears the per-process mentor counters.
func ResetMentorCountersForTest() {
	mentorCounterMu.Lock()
	mentorCounters = map[string]int{}
	mentorCounterMu.Unlock()
}

// mentorKnobs resolves the per-strategy size knobs with the CTO defaults.
func (at *AutoTrader) mentorKnobs() (base, conf, big, reduced, swing4h, spentCap, maxContracts int) {
	base, conf, big = mentorBaseContractsDefault, mentorConfluenceContractsDefault, mentorBigContractsDefault
	reduced, swing4h, spentCap = mentorReducedContractsDefault, mentorSwing4HContractsDefault, mentorSpentDayContractsDefault
	maxContracts = mentorMaxContractsDefault
	if at.config.StrategyConfig == nil {
		return
	}
	rc := at.config.StrategyConfig.RiskControl
	if rc.MentorBaseContracts > 0 {
		base = rc.MentorBaseContracts
	}
	if rc.MentorConfluenceContracts > 0 {
		conf = rc.MentorConfluenceContracts
	}
	if rc.MentorBigContracts > 0 {
		big = rc.MentorBigContracts
	}
	if rc.MentorReducedContracts > 0 {
		reduced = rc.MentorReducedContracts
	}
	if rc.MentorSwing4HContracts > 0 {
		swing4h = rc.MentorSwing4HContracts
	}
	if rc.MentorSpentDayContracts > 0 {
		spentCap = rc.MentorSpentDayContracts
	}
	if rc.MentorMaxContracts > 0 {
		maxContracts = rc.MentorMaxContracts
	}
	return
}

// mentorEnabled reports the per-strategy switch (OFF by default).
func (at *AutoTrader) mentorEnabled() bool {
	return at.config.StrategyConfig != nil && at.config.StrategyConfig.RiskControl.MentorMode
}

// mentorMaxContracts is the mentor-mode ceiling for resolveMaxContracts: the
// 0B size clamp (maxFuturesContracts=2) does NOT apply to a mentor-mode
// trader. AI mode is untouched (mentorEnabled false → the existing path).
func (at *AutoTrader) mentorMaxContracts() (int, bool) {
	if !at.mentorEnabled() {
		return 0, false
	}
	_, _, _, _, _, _, mx := at.mentorKnobs()
	return mx, true
}

// mentorRiskControl returns the trader's risk-control config (nil when there
// is no strategy bound).
func (at *AutoTrader) mentorRiskControl() *store.RiskControlConfig {
	if at.config.StrategyConfig == nil {
		return nil
	}
	return &at.config.StrategyConfig.RiskControl
}

// ── KNOB ROUTING (CTO 1791033257041) — defaults as ruled ───────────────────
//
// The evaluator's G1/L1/E4/location knobs ride the strategy config like the
// other mentor knobs. Each resolver is the single defaults site, and
// mentorEvaluatorConfig applies every one of them to the evaluator.

// mentorLegBudgetEnabled — G1 leg budget: nil → ON (default).
func mentorLegBudgetEnabled(rc *store.RiskControlConfig) bool {
	return rc == nil || rc.MentorLegBudgetEnabled == nil || *rc.MentorLegBudgetEnabled
}

// mentorLegResetOn — G1 parity knob: "close" default; a bad value fails
// closed to "close" and is counted.
func mentorLegResetOn(rc *store.RiskControlConfig) string {
	if rc != nil {
		switch v := strings.ToLower(strings.TrimSpace(rc.MentorLegResetOn)); v {
		case "close", "touch":
			return v
		case "":
		default:
			mentorCount("leg_reset_on_bad_value")
		}
	}
	return "close"
}

// mentorLvlRevisitMinPts — L1 knob: the extra departure distance a closed
// non-touching candle needs to END a visit. Default 0 (he never states one).
func mentorLvlRevisitMinPts(rc *store.RiskControlConfig) float64 {
	if rc != nil && rc.MentorLvlRevisitMinPts > 0 {
		return rc.MentorLvlRevisitMinPts
	}
	return 0
}

// mentorEmaMaxCross30m — E4 knob: refuse the EMA34 setup when the close
// crossed the line this many times over the last 30 closed 1m candles.
// Default 0 = OFF (base).
func mentorEmaMaxCross30m(rc *store.RiskControlConfig) int {
	if rc != nil && rc.MentorEmaMaxCross30m > 0 {
		return rc.MentorEmaMaxCross30m
	}
	return 0
}

// mentorLocationTriggerFilter — the 5m trigger filter at locations (L3: kept
// ON in the base). nil → ON.
func mentorLocationTriggerFilter(rc *store.RiskControlConfig) bool {
	return rc == nil || rc.MentorLocationTriggerFilter == nil || *rc.MentorLocationTriggerFilter
}

// mentorLossDeparturePts is the optional fixed-points fallback for the B22
// structural departure rule. Zero (unset) leaves the fallback OFF.
func mentorLossDeparturePts(rc *store.RiskControlConfig) float64 {
	if rc != nil && rc.MentorLossDeparturePts > 0 {
		return rc.MentorLossDeparturePts
	}
	return 0
}

// mentorSuppressAIEntry is the AI-entries-off half of the mode switch: a
// mentor-mode trader takes NO AI open decisions (closes, flattens and safety
// paths are untouched). Returns the refusal, "" when allowed.
func (at *AutoTrader) mentorSuppressAIEntry(d *kernel.Decision) string {
	if !at.mentorEnabled() {
		return ""
	}
	if d.MentorSourced {
		return ""
	}
	switch d.Action {
	case "open_long", "open_short":
		mentorCount("ai_entry_suppressed")
		telemetry.IncGateBlock(at.id, "mentor_ai_entries_off")
		return "mentor_mode: AI entries are OFF — every entry comes from the mentor evaluator"
	}
	return ""
}

// mentorSkipsAIDecision reports whether the AI decision cycle must skip its
// LLM call (P0 fix/mentor-ai-off): with mentor_mode ON the AI's entries are
// refused at the admission chain anyway, so the call would spend tokens on a
// decision that cannot place. OFF → false, byte-identical.
func (at *AutoTrader) mentorSkipsAIDecision() bool {
	return at.mentorEnabled()
}

// mentorSuppressAIClose is the close half of the AI-off scope (CTO 15:17:23Z,
// D2.1 @02:47): while mentor_mode is ON, an AI close decision is refused when
// the open position was opened by a mentor-sourced decision — the mentor
// driver owns the exits. Mentor-sourced closes pass. The ownership flag is
// cleared by the flat sweep; a stale flag only over-refuses (fail-safe).
func (at *AutoTrader) mentorSuppressAIClose(d *kernel.Decision) string {
	if !at.mentorEnabled() || d.MentorSourced {
		return ""
	}
	side := "long"
	if d.Action == "close_short" {
		side = "short"
	}
	if !at.positionMentorOwned[d.Symbol+"_"+side] {
		return ""
	}
	mentorCount("ai_close_suppressed")
	telemetry.IncGateBlock(at.id, "mentor_ai_closes_off")
	return "mentor_mode: AI closes are OFF on a mentor-owned position — the mentor driver owns the exit"
}

// mentorAdmitRefusal is the ONE admission-chain form of the AI-entries-off
// mode switch (P0 fix/mentor-ai-off, DS-106): it refuses every non-mentor
// entry on every producer path — decision, agent-chat, arm, picture. A
// mentor-sourced decision passes (the mentor's own placement must reach the
// executor). A mentor-authored armed row (the injector's ledger path) passes
// via MentorArm even though it carries no Decision — the bundle routes every
// mentor entry as an armed-ledger row (CTO 20:14:30Z). Planner arms, the
// agent door and Picture stay refused. With mentor_mode OFF it returns "" —
// byte-identical.
func (at *AutoTrader) mentorAdmitRefusal(in admitIntent) string {
	if !at.mentorEnabled() {
		return ""
	}
	if in.Decision != nil && in.Decision.MentorSourced {
		return ""
	}
	if in.Path == admitArm && in.MentorArm {
		return ""
	}
	return "mentor_mode: AI entries are OFF — every entry comes from the mentor evaluator"
}

// mentorPastDailyHaltCutoff (B3 N6) is the mentor's one hard last-entry: no new
// mentor entry after 15:45 CT — the 15-minute lead into the 16:00 CT daily
// maintenance break (which CMEClosedReason then refuses 16:00–17:00).
func (at *AutoTrader) mentorPastDailyHaltCutoff(now time.Time) (string, bool) {
	ct := now.In(kernel.CTLocation())
	if ct.Hour() == 15 && ct.Minute() >= 45 {
		return "past 15:45 CT (CME daily halt) — no new mentor entries until the 17:00 reopen", true
	}
	return "", false
}

// isSwingPosition (N4) reports whether an open position is a SWING4H mentor
// position (identified by its cited arm id "swing-…"), which is EXEMPT from the
// intraday EOD flat.
func isSwingPosition(p *store.TraderPosition) bool {
	if p == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(p.CitedScenarioID)), "swing-")
}

// mentorRuleGate is the injector-side R8/R9 gate: it refuses intents the
// evaluator should never have let through, fail-closed, before any sizing.
// (R8) SWING4H: stop 30–60 allowed, ≥100 refused, exempt from the 25-pt
// ceiling. (R9) the target is never smaller than the stop; on a spent day
// (cap 15) a stop over 15 skips. Defence in depth (CTO 1791058442006): the
// stop/target points are read from the GEOMETRY (abs(Price−Stop),
// abs(Target−Price)), never a bare intent field an emit site forgot to set,
// and an entry with no Setup tag is refused here. Returns the skip reason,
// "" when the intent passes.
func mentorRuleGate(in mentor.Intent, extra mentorTierInputs) string {
	if in.Setup == "" {
		return "untagged setup: no Setup tag — refuse fail-closed [kernel owner ruling, CTO 1791058442006]"
	}
	swing := strings.EqualFold(in.Setup, "SWING4H")
	// GEOMETRY GATE (CTO 1791058631982): the trader's own backstop, before
	// any sizing — a missing or inverted geometry is refused fail-closed
	// (reason "bad geometry"). A Target-0 intraday entry would otherwise
	// fall back to ~30000 pts of fake target and size BIG (20) with no
	// take-profit at all. The swing carries no fixed target: stop-side only.
	if !swing {
		if in.Stop <= 0 || in.Target <= 0 {
			return "bad geometry: missing stop/target — refuse fail-closed [CTO 1791058631982]"
		}
		switch in.Side {
		case mentor.SideLong:
			if !(in.Stop < in.Price && in.Price < in.Target) {
				return fmt.Sprintf("bad geometry: long needs stop %.2f < entry %.2f < target %.2f [CTO 1791058631982]", in.Stop, in.Price, in.Target)
			}
		case mentor.SideShort:
			if !(in.Target < in.Price && in.Price < in.Stop) {
				return fmt.Sprintf("bad geometry: short needs target %.2f < entry %.2f < stop %.2f [CTO 1791058631982]", in.Target, in.Price, in.Stop)
			}
		default:
			return fmt.Sprintf("bad geometry: unknown side %q [CTO 1791058631982]", in.Side)
		}
	} else {
		if in.Stop <= 0 {
			return "bad geometry: missing swing stop — refuse fail-closed [CTO 1791058631982]"
		}
		switch in.Side {
		case mentor.SideLong:
			if !(in.Stop < in.Price) {
				return "bad geometry: long swing stop must sit below the entry [CTO 1791058631982]"
			}
		case mentor.SideShort:
			if !(in.Stop > in.Price) {
				return "bad geometry: short swing stop must sit above the entry [CTO 1791058631982]"
			}
		default:
			return fmt.Sprintf("bad geometry: unknown side %q [CTO 1791058631982]", in.Side)
		}
	}
	stop := mentorIntentRisk(in)
	target := mentorIntentTargetPts(in)
	if swing {
		if stop > mentorSwingStopMaxPts {
			return fmt.Sprintf("R8: SWING4H stop %.1f pts — ~100 is refused (allowed 30–60, no 25-pt ceiling for the swing)", stop)
		}
	} else {
		if stop > mentorStopTwentiesMaxPts {
			return fmt.Sprintf("R8: stop %.1f pts over the 25-pt ceiling — skip (the swing is the only exemption)", stop)
		}
	}
	if target > 0 && target < stop {
		return fmt.Sprintf("R9: target %.1f pts smaller than the stop %.1f pts — never trade it [D1.2 p1 @ 07:48–09:00]", target, stop)
	}
	// R13 [D5.2 §6]: DayOff does not stop the swing, so the spent-day stop cap
	// must not either — the overnight SWING4H is exempt from the R9 15-pt cap.
	if extra.SpentDay && !swing && stop > mentorSpentDayStopCapPts {
		return fmt.Sprintf("R9: spent day cap 15 — stop %.1f pts skips", stop)
	}
	return ""
}

// mentorSizeFor chooses the tier for one intent, logs it and counts it. The
// tier inputs beyond the intent's own fields (confluence, 4h/1h agreement,
// spent day) come from the caller's evaluator context.
// mentorSizeForHook is a test seam: if set, mentorSizeFor calls it first — the
// B20 upgrade pin asserts the upgrade path NEVER re-runs the size table.
var mentorSizeForHook func()

func (at *AutoTrader) mentorSizeFor(in mentor.Intent, extra mentorTierInputs) (mentorSizeChoice, error) {
	// B20 size-unchanged pin: the upgrade path must never re-run the table.
	if mentorSizeForHook != nil {
		mentorSizeForHook()
	}
	extra.Setup = in.Setup
	// ISB size rules 2 and 3 (D4.1 p1 @08:05/09:40, written): the evaluator
	// stamps Intent.Flag on an ISB at an old high/low or inside a range; here
	// the flags reach the size table, which cuts both to tier 3. Set at the ONE
	// sizing call site so no caller can forget them.
	extra.ISBOldExtreme = extra.ISBOldExtreme || mentor.HasFlag(in.Flag, mentor.FlagISBAtOldExtreme)
	extra.ISBInRange = extra.ISBInRange || mentor.HasFlag(in.Flag, mentor.FlagISBInRange)
	// Defence in depth (CTO 1791058442006): the tier inputs are the GEOMETRY
	// (abs(Price−Stop), abs(Target−Price)), never a bare intent field an
	// emit site forgot to set — a swing sized as a base trade is the bug this
	// closes.
	extra.StopPts = mentorIntentRisk(in)
	extra.TargetPts = mentorIntentTargetPts(in)
	if extra.RoomMultiple == 0 && extra.TargetPts > 0 && extra.StopPts > 0 {
		extra.RoomMultiple = extra.TargetPts / extra.StopPts
	}
	base, conf, big, reduced, swing4h, spentCap, mx := at.mentorKnobs()
	choice, err := mentorContractsFor(extra, base, conf, big, reduced, swing4h, spentCap, mx)
	if err != nil {
		mentorCount("refused_no_max")
		at.logErrorf("🧑‍🏫 mentor size refused: %v", err)
		return mentorSizeChoice{}, err
	}
	mentorCount("tier_" + choice.Tier)
	at.logInfof("🧑‍🏫 mentor size: %d contracts (tier %s) — %s [setup %s, stop %.1f pts]",
		choice.Contracts, choice.Tier, choice.Why, in.Setup, in.StopPts)
	return choice, nil
}

// ── EXITS — EXIT-SPEC-v3 on SPLIT LEGS AT ENTRY (rulings 2026-10-03) ──────
//
// A mentor entry of n contracts = leg 1 (ceil(n/2), its OWN TP) + leg 2 (the
// rest, the runner). n = 1 → a single leg. NO reduce_position anywhere in the
// mentor path — the frame stays in the AddOn, unwired. The exit is a FORK,
// chosen by whether the resonance appears (D2.4 p1):
//   A. RESONANCE — a PHL/PLH filled, then an ISB in the SAME direction within
//      3 candles of the fill: BOTH stops → BE at that moment and leg 1's TP is
//      modified OUT to the runner's target — NO 1:1 scale-out, NO candle
//      trail. Let it run; EOD flat still applies.
//   B. NO RESONANCE (normal) — once price has covered HALF THE DISTANCE TO
//      LEG 1'S TARGET both legs' stops → BE (move_stop on each, the live R:R
//      stays 1:1); leg 1 exits at its +1R TP (native bracket); the runner
//      trails behind each CLOSED 1m candle (move_stop); exit on the first
//      candle that takes the prior candle's extreme. Knob trail_tf: 1m
//      default, 30s/45s allowed, off = video-8 legacy (SUPERSEDED — kept as a
//      knob). ISB: leg 1's TP is modified to the fill-candle close (a limit
//      at or through the market) — the partial is mandatory [D1.4 p1
//      @12:23–13:27]; the runner continues.
//   C. CONFLUENCE — leg 1's TP at ≥1:2 set AT ENTRY; the stop does not move up.
//   D. SPENT DAY — the runner is capped at 2 contracts; the 15-pt cap (R9)
//      still applies.
//   SWING — hold by the 4h (DS-106's rules).
//
// The LIVE drive loop over filled mentor positions lands with P1 (#309); these
// pure functions are the rules that loop will call.

// mentorPosition is one open mentor trade's exit-driver state.
type mentorPosition struct {
	Symbol    string
	Side      string // "long" | "short"
	Origin    string // the entry setup: "PHL", "PLH", "ISB", "SWING4H"
	Entry     float64
	Stop      float64
	Target    float64 // the runner's target (A pushes leg 1's TP out to it)
	R         float64
	Contracts int
	Leg1      int     // ceil(n/2), its own TP
	Leg2      int     // the runner
	Mode      string  // "A-resonance", "B", "C", "swing"
	ArmedBE   bool    // stops are at entry (B: half the target distance seen; A: resonance armed)
	Scaled    bool    // leg 1's +1R TP candle seen → the runner's trail begins
	Leg1TP    float64 // leg 1's TP (0 → the +1R default: entry ± R)
}

// ── SPLIT LEGS AT ENTRY (CTO ruling 2026-10-03 05:42Z) ─────────────────────
//
// Size n from the tier table → leg 1 (ceil(n/2), its OWN TP at +1R) + leg 2
// (the rest, the runner). n = 1 → a single leg, no scale-out. No
// reduce_position anywhere in the mentor path.

// mentorSplitLegs splits n into leg 1 (ceil(n/2)) and leg 2 (the runner).
// runnerCap (spent day: 2) caps the runner so at most that many contracts run
// after leg 1. Parity: ceil(n/2) equals DS-108's replay half.
func mentorSplitLegs(n, runnerCap int) (leg1, leg2 int) {
	if n <= 0 {
		return 0, 0
	}
	leg1 = (n + 1) / 2
	leg2 = n - leg1
	if runnerCap > 0 && leg2 > runnerCap {
		leg2 = runnerCap
	}
	return leg1, leg2
}

// mentorSpentDayRunnerCap is D: at most 2 contracts run after leg 1.
const mentorSpentDayRunnerCap = 2

// mentorLeg1TPForC is the C (confluence) leg-1 target: hold to at least 1:2 —
// leg 1's TP at 2× risk, set AT ENTRY; the stop never moves up.
func mentorLeg1TPForC(entry, r float64, side string) float64 {
	if side == "short" {
		return entry - 2*r
	}
	return entry + 2*r
}

// mentorConfluenceForIntent is the R2 STUB (CTO 1791029620038: "use a stub
// flag until DS-106's lands"): reports whether the intent carries the
// confluence flag (box edge + key level inside the box or within 2 pts of its
// edge + 5m trigger agrees). nil → false — C and size 10 never fire until
// DS-103's tagged intents replace this seam.
var mentorConfluenceForIntent func(in mentor.Intent) bool

// mentorConfluenceFlag resolves the stub seam for one intent.
func mentorConfluenceFlag(in mentor.Intent) bool {
	return mentorConfluenceForIntent != nil && mentorConfluenceForIntent(in)
}

// mentorExtraFor builds the size-table inputs for ONE intent at the eval site.
// A5 (CTO 1791041016051): the §7 spent-day flag rides the intent (stamped by
// the evaluator) — before this line the flag existed in the table but was
// never SET, so the spent_day tier (2) and the R9 15-pt stop cap never fired.
// The 4h+1h agreement rides the intent the same way (HTFAgree).
func mentorExtraFor(in mentor.Intent, strongDay bool) mentorTierInputs {
	return mentorTierInputs{
		StrongDay:  strongDay,
		Confluence: mentorConfluenceFlag(in),
		SpentDay:   in.SpentDay,
		// the evaluator stamps the 4h+1h agreement on the entry intent; before
		// this line HTFAgree was never set, so the 20-contract tier was
		// unreachable (SETTINGS-VS-LESSONS-1004 P1-2).
		HTFAgree: in.HTFAgree,
	}
}

// mentorIntentRisk is |entry − stop| from the intent (StopPts when the emit
// site set it, the geometry otherwise).
func mentorIntentRisk(in mentor.Intent) float64 {
	r := in.StopPts
	if r <= 0 {
		r = math.Abs(in.Price - in.Stop)
	}
	return r
}

// mentorIntentTargetPts is |target − entry| from the intent (TargetPts when
// the emit site set it, the geometry otherwise) — the rule gate and the size
// table read this, never a bare in.TargetPts.
func mentorIntentTargetPts(in mentor.Intent) float64 {
	if in.TargetPts > 0 {
		return in.TargetPts
	}
	return math.Abs(in.Target - in.Price)
}

// mentorExitFork chooses the exit branch AT ENTRY from the intent flags (CTO
// 1791029620038 — the fork is DS-102's, wired now, driven when the P1 exit
// loop lands):
//
//	swing — SWING4H: hold by the 4h (DS-106's rules).
//	C     — the confluence flag: hold ≥ 1:2, stop never moves up, size 10
//	        (20 with 4h+1h agree + room ≥ 2× + target ≥ 30). Leg 1's TP at
//	        2× risk is set AT ENTRY (mentorLeg1TPForC).
//	B     — normal: BE once price covers half the distance to leg 1's target,
//	        leg 1 exits at its +1R TP, the runner trails each closed 1m
//	        candle. A PHL/PLH fill starts as B with the resonance watch armed:
//	        a same-direction ISB within 3 candles flips it to A
//	        (mentorMaybeArmResonance).
func mentorExitFork(in mentor.Intent, confluence bool) (mode string, leg1TP float64, why string) {
	switch {
	case in.Setup == "SWING4H":
		return "swing", 0, "SWING4H: hold by the 4h (DS-106's rules)"
	case confluence:
		r := mentorIntentRisk(in)
		side := "long"
		if in.Side == mentor.SideShort {
			side = "short"
		}
		return "C", mentorLeg1TPForC(in.Price, r, side), "confluence flag: hold ≥ 1:2, stop never moves up"
	case in.Setup == "PHL" || in.Setup == "PLH":
		return "B", 0, "PHL/PLH: B at entry, resonance watch armed — a same-direction ISB within 3 candles flips to A (BE, no trail, no 1:1 scale-out)"
	default:
		return "B", 0, "normal: BE at half the distance to leg 1's target, leg 1 TP +1R, runner trails each closed 1m candle"
	}
}

// modifyBracketWire modifies leg 1's TP (modify_bracket). nil → the action is
// logged + counted, never sent.
var modifyBracketWire func(leg string, newTP float64) error

// flattenLegWire flattens ONE leg (fail-closed on lost protection).
var flattenLegWire func(leg string) error

// mentorLegProtectedSource confirms a leg's protection on the NEXT snapshot
// (nil → nothing is ever confirmed → the leg is flattened).
var mentorLegProtectedSource func(leg string) bool

// mentorExitResult is one closed candle's driver output.
type mentorExitResult struct {
	NewStop      float64  // the stop for the MoveStops legs
	MoveStops    []string // legs to move_stop to NewStop
	ModifyTP     *float64 // modify_bracket leg 1's TP to this (nil = no change)
	Leg1AtTarget bool     // the candle crossed leg 1's +1R TP (the runner's trail begins next candle)
	ExitPrice    float64
	ExitReason   string
	Exited       bool
}

// mentorApplyExitResult sends the leg actions. A failed or unwired
// modify_bracket / move_stop is logged AND counted — never silent.
func (at *AutoTrader) mentorApplyExitResult(nt *ntTrader.TCPTrader, side string, res mentorExitResult) {
	for _, leg := range res.MoveStops {
		if moveStopWire == nil {
			mentorCount("move_stop_unwired")
			at.logWarnf("🧑‍🏫 mentor %s move_stop unwired (stop %.2f) — logged, not sent", leg, res.NewStop)
			continue
		}
		if err := moveStopWire(nt, side, res.NewStop); err != nil {
			mentorCount("move_stop_failed")
			at.logErrorf("🧑‍🏫 mentor %s move_stop FAILED: %v", leg, err)
			continue
		}
		mentorCount("move_stop_sent_" + leg)
	}
	if res.ModifyTP != nil {
		if modifyBracketWire == nil {
			mentorCount("modify_bracket_unwired")
			at.logWarnf("🧑‍🏫 mentor leg1 modify_bracket unwired (TP %.2f) — logged, not sent", *res.ModifyTP)
		} else if err := modifyBracketWire("leg1", *res.ModifyTP); err != nil {
			mentorCount("modify_bracket_failed")
			at.logErrorf("🧑‍🏫 mentor leg1 modify_bracket FAILED: %v", err)
		} else {
			mentorCount("modify_bracket_sent")
		}
	}
}

// mentorConfirmLegProtection is the next-snapshot fail-closed check: a leg
// whose protection cannot be confirmed is FLATTENED (that leg only).
func (at *AutoTrader) mentorConfirmLegProtection(legs []string) {
	for _, leg := range legs {
		protected := mentorLegProtectedSource != nil && mentorLegProtectedSource(leg)
		if protected {
			continue
		}
		mentorCount("leg_protection_lost_" + leg)
		at.logWarnf("🧑‍🏫 mentor %s protection unconfirmed — FLATTENING that leg (fail-closed)", leg)
		if flattenLegWire != nil {
			if err := flattenLegWire(leg); err != nil {
				mentorCount("flatten_leg_failed")
				at.logErrorf("🧑‍🏫 mentor %s flatten FAILED: %v", leg, err)
			}
		}
	}
}

// mentorBEHalfDistance is the B BE trigger (X1, CTO 1791031960407, PLAN Exits B
// D1.2 p1 @10:31–15:20): HALF THE DISTANCE TO THE TRADE'S TARGET (the intent's
// target — e.g. near the old high), NOT half of leg 1's +1R take-profit.
// That is +0.5R only when the trade target is 1:1; a 2R target arms BE at
// +1R. Returns the signed entry-relative distance (negative for shorts).
// A position built without its trade target falls back to leg 1's TP, then
// the +1R default — never fabricates a deeper BE.
func mentorBEHalfDistance(pos mentorPosition) float64 {
	tp := pos.Target
	if tp == 0 {
		tp = pos.Leg1TP
		if tp == 0 {
			if pos.Side == "short" {
				tp = pos.Entry - pos.R
			} else {
				tp = pos.Entry + pos.R
			}
		}
	}
	return (tp - pos.Entry) / 2
}

// mentorExitB applies the v3 B rules to ONE closed 1m candle (pure) on the
// SPLIT-LEGS model: leg 1 carries its own TP at +1R set AT ENTRY, so the
// driver never scales — it only arms BE once price has covered HALF THE
// DISTANCE TO LEG 1'S TARGET (mentorBEHalfDistance — +0.5R only for a 1:1
// target; BOTH legs), records leg 1's +1R crossing (the runner's trail begins
// on the NEXT candle), and trails the runner behind each CLOSED candle (long:
// the candle's low, short: its high) unless trail is off (video-8 legacy
// knob). For an ISB trade the fill-candle close modifies leg 1's TP to the
// current price — the partial is mandatory [D1.4 p1 @12:23–13:27], replacing
// the +1R TP for ISB only. A candle trading through both the stop and a
// further level takes the WORSE outcome (the stop). No look-ahead: the trail
// moves only AFTER a candle closes.
func mentorExitB(pos mentorPosition, c, h, l float64, trail bool) mentorExitResult {
	res := mentorExitResult{NewStop: pos.Stop}
	long := pos.Side == "long"
	half := mentorBEHalfDistance(pos) // half the distance to leg 1's target
	var hitStop, hitHalfR, hit1R bool
	if long {
		hitStop = l <= pos.Stop
		hitHalfR = h >= pos.Entry+half
		hit1R = h >= pos.Entry+pos.R
	} else {
		hitStop = h >= pos.Stop
		hitHalfR = l <= pos.Entry+half
		hit1R = l <= pos.Entry-pos.R
	}
	if !pos.ArmedBE {
		if hitStop && (hitHalfR || hit1R) {
			res.ExitPrice, res.ExitReason, res.Exited = pos.Stop, "stop(worse-same-bar)", true
			return res
		}
		if hitStop {
			res.ExitPrice, res.ExitReason, res.Exited = pos.Stop, "stop", true
			return res
		}
		// ISB EXIT [D1.4]: the fill candle's close closes leg 1 — modify its
		// TP to the current price (a limit at or through the market). The
		// runner continues.
		if pos.Origin == "ISB" {
			tp := c
			res.ModifyTP = &tp
			if hitHalfR {
				res.NewStop = pos.Entry
				res.MoveStops = []string{"leg1", "leg2"} // BE for BOTH legs
			}
			return res
		}
		if hit1R {
			// one candle crossed both +0.5R and +1R: BE for both legs and leg
			// 1's +1R TP is hit (the runner trails from the NEXT candle).
			res.NewStop = pos.Entry
			res.MoveStops = []string{"leg1", "leg2"}
			res.Leg1AtTarget = true
			return res
		}
		if hitHalfR {
			res.NewStop = pos.Entry
			res.MoveStops = []string{"leg1", "leg2"} // arm BE for BOTH legs
		}
		return res
	}
	if hitStop {
		reason := "be"
		if (long && pos.Stop > pos.Entry) || (!long && pos.Stop < pos.Entry) {
			reason = "trail"
		}
		res.ExitPrice, res.ExitReason, res.Exited = pos.Stop, reason, true
		return res
	}
	if !pos.Scaled {
		if hit1R {
			res.Leg1AtTarget = true // leg 1 exits at its own TP; the runner trails next
		}
		return res
	}
	// after leg 1's target: the runner trails (knob trail_tf). off → the stop
	// never moves.
	if trail {
		if long {
			if l > pos.Stop {
				res.NewStop = l
				res.MoveStops = []string{"leg2"}
			}
		} else {
			if h < pos.Stop {
				res.NewStop = h
				res.MoveStops = []string{"leg2"}
			}
		}
	}
	return res
}

// mentorResonanceMaxCandles is the A window: the resonance ISB must appear
// within 3 candles of the PHL/PLH fill [D2.4 p1 @01:36–02:59].
const mentorResonanceMaxCandles = 3

// mentorMaybeArmResonance flips an open PHL/PLH position into mode A the
// moment an ISB in the SAME direction appears within 3 candles of the fill:
// the REST's stop goes to BREAK-EVEN immediately, NO candle trail. R-RES
// (CTO 1791040643329, D2.4 p1 @02:17-02:29 "@03:36-03:45"): the resonance
// STILL takes the 1:1 partial — "RISK REWARD 1-1 toi van se ban bot" — so
// leg 1's +1R TP STAYS (no modify, returned 0); leg 2 runs with its stop at
// BE. Returns whether it armed and the modify-bracket TP (0 = no modify).
func mentorMaybeArmResonance(pos *mentorPosition, isbSide string, barsSinceFill int) (armed bool, modifyTP float64) {
	if pos == nil || pos.Mode == "A-resonance" {
		return false, 0
	}
	if pos.Origin != "PHL" && pos.Origin != "PLH" {
		return false, 0 // only a PHL/PLH fill can resonate
	}
	if barsSinceFill < 1 || barsSinceFill > mentorResonanceMaxCandles {
		return false, 0
	}
	if !strings.EqualFold(pos.Side, isbSide) {
		return false, 0
	}
	pos.Mode = "A-resonance"
	pos.ArmedBE = true
	pos.Stop = pos.Entry
	mentorCount("resonance_armed")
	// R-RES: leg 1's +1R take-profit STAYS resting — no TP modify.
	return true, 0
}

// mentorExitHold applies the stop-only holds: the stop NEVER moves and nothing
// scales. C (§6 confluence): hold to at least 1:2, the stop does not move up.
// A (resonance): the stop is already at BE and runs. A touch exits at the
// stop; EOD flat is the caller's rule.
func mentorExitHold(pos mentorPosition, l, h float64, reason string) (exitPrice float64, exitReason string, exited bool) {
	var hitStop bool
	if pos.Side == "long" {
		hitStop = l <= pos.Stop
	} else {
		hitStop = h >= pos.Stop
	}
	if hitStop {
		return pos.Stop, reason, true
	}
	return 0, "", false
}

// mentorExitC applies the confluence hold (C): the stop NEVER moves and there
// is NO scale-out — hold to at least 1:2. Leg 1's TP at 2× risk is set AT
// ENTRY (mentorLeg1TPForC); the driver never modifies it.
func mentorExitC(pos mentorPosition, l, h float64) (exitPrice float64, exitReason string, exited bool) {
	return mentorExitHold(pos, l, h, "stop")
}

// mentorExitA applies the resonance hold (A): leg 2's stop is at BE, no
// candle trail — leg 1's +1R TP STAYS resting (R-RES: the 1:1 partial is
// still taken). EOD flat still applies.
func mentorExitA(pos mentorPosition, l, h float64) (exitPrice float64, exitReason string, exited bool) {
	return mentorExitHold(pos, l, h, "resonance_be")
}

// mentorTrailTFDefault is the B candle-trail timeframe: 1m (the course frame
// D2.4 p1 @08:35 is on the 1-MINUTE chart; 30s/45s allowed @09:07).
const mentorTrailTFDefault = "1m"

// mentorTrailTF resolves the trail_tf knob. "" → 1m; 1m/30s/45s pass; off =
// the video-8 legacy "never trail on the 1m" (SUPERSEDED, kept as a knob);
// any other value refuses to trail fail-closed and is counted.
func (at *AutoTrader) mentorTrailTF() string {
	raw := ""
	if at.config.StrategyConfig != nil {
		raw = at.config.StrategyConfig.RiskControl.MentorTrailTF
	}
	switch v := strings.ToLower(strings.TrimSpace(raw)); v {
	case "":
		return mentorTrailTFDefault
	case "1m", "30s", "45s", "off":
		return v
	default:
		mentorCount("trail_tf_bad_value")
		at.logWarnf("🧑‍🏫 mentor trail_tf %q invalid (1m/30s/45s/off) — trail refused for safety", raw)
		return "off"
	}
}

// mentorTrailEnabled reports whether the candle trail runs (off = video-8).
func mentorTrailEnabled(tf string) bool { return tf != "off" }

// mentorSpentDayClamp is the §7 rule (D): at most 2 contracts LEFT RUNNING on
// a spent day; the 15-pt stop cap (R9) still applies upstream in the gate.
func mentorSpentDayClamp(contracts, spentCap int) int {
	if spentCap <= 0 {
		return contracts
	}
	if contracts > spentCap {
		return spentCap
	}
	return contracts
}

// mentorNeverWiden is the pure guard (owner ruling (c), D1.2 p2 @00:08): a
// stop amendment that increases open risk is refused — a long stop may only
// move UP, a short stop only DOWN. Equal is not a widen.
func mentorNeverWiden(side string, curStop, newStop float64) (refuse bool, why string) {
	switch {
	case side == "long" && newStop < curStop:
		return true, fmt.Sprintf("stop widen refused: long stop %.2f → %.2f increases open risk [D1.2 p2 @00:08]", curStop, newStop)
	case side == "short" && newStop > curStop:
		return true, fmt.Sprintf("stop widen refused: short stop %.2f → %.2f increases open risk [D1.2 p2 @00:08]", curStop, newStop)
	}
	return false, ""
}

// mentorOpenStopSource is the current open stop for the never-widen guard
// (nil → no open mentor position known; the live driver sets it from the
// position registry at P1).
var mentorOpenStopSource func() (float64, bool)

// mentorMoveStop sends a mentor stop move through the SAME last hop the AI
// mechanisms use, but WITHOUT the 0B suspension gate: EXIT_MECHS_SUSPENDED does
// NOT apply to mentor mode (the AI mechanisms keep it — both sides are pinned
// by TestMentorExitMechSuspensionAppliesToAIOnly). Every move passes the
// never-widen guard (c) first: an amendment that increases open risk never
// reaches the wire.
func (at *AutoTrader) mentorMoveStop(nt *ntTrader.TCPTrader, side string, newStop float64) error {
	// FAIL-CLOSED (c): with mentor mode ON and no open-stop source, refuse the
	// move — the stop stays where it is.
	if at.mentorEnabled() && mentorOpenStopSource == nil {
		mentorCount("stop_move_no_source")
		at.logWarnf("🧑‍🏫 mentor stop move refused: open-stop source not wired (fail-closed) — the stop stays where it is")
		return fmt.Errorf("mentor stop move refused: open-stop source not wired (fail-closed)")
	}
	if mentorOpenStopSource != nil {
		if cur, ok := mentorOpenStopSource(); ok {
			if refuse, why := mentorNeverWiden(side, cur, newStop); refuse {
				mentorCount("widen_refused")
				at.logWarnf("🧑‍🏫 %s", why)
				return fmt.Errorf("mentor stop move refused: %s", why)
			}
		}
	}
	return moveStopWire(nt, side, newStop)
}

// mentorLogPositionState dumps the driver state for the daily log.
func (at *AutoTrader) mentorLogPositionState(pos *mentorPosition, event string) {
	at.logInfof("🧑‍🏫 mentor exit driver [%s]: %s side=%s origin=%s entry=%.2f stop=%.2f R=%.2f contracts=%d mode=%s armedBE=%v scaled=%v",
		event, pos.Symbol, pos.Side, pos.Origin, pos.Entry, pos.Stop, pos.R, pos.Contracts, pos.Mode, pos.ArmedBE, pos.Scaled)
}
