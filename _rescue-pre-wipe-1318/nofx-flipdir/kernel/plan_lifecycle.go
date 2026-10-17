package kernel

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"nofx/market"
)

// P3.6 — plan lifecycle helpers (pure, Go-side; no plan mutation).

// ActivationWindowK is the default activation half-width in daily-ATR multiples.
const ActivationWindowK = 1.5

// ResolveProximityK (grand-audit response F2, 2026-08-28) — the ONE clamp for
// the day-trade lock value. Valid 0.1–3.0 (the Guide/UI range); anything else
// falls back to the default. BOTH consumers (the bot gate proximityFilterATR
// and the engine prompt path) resolve through here so a 0.3 owner retune can
// never be silently dropped by a divergent clamp (the engine side alone still
// had a 0.5 floor — the archaeology found no revert, the clamp never allowed
// the retune on that path).
func ResolveProximityK(v float64) float64 {
	if v >= 0.1 && v <= 3.0 {
		return v
	}
	return ActivationWindowK
}

// ActivePlanLevels filters plan levels to those currently within k×dATR of price
// — the executor's live candidate set (the "activation window"). Levels beyond
// re-activate when price returns. Pure: it never mutates the plan/cache. k≤0 →
// default 1.5; dATR≤0 → all levels (can't scale).
func ActivePlanLevels(levels []PlanLevel, price, dATR, k float64) []PlanLevel {
	if dATR <= 0 || price <= 0 {
		return levels
	}
	if k <= 0 {
		k = ActivationWindowK
	}
	band := k * dATR
	out := make([]PlanLevel, 0, len(levels))
	for _, l := range levels {
		if math.Abs(l.Price-price) <= band {
			out = append(out, l)
		}
	}
	return out
}

// PlanIsDead reports whether the plan's thesis is spent: it has levels and EVERY
// one has been TOUCHED and then accepted through (consumed) — the evaluator-
// detected death signal that triggers a re-plan (P3.6). A level price never
// reached is NOT consumed (being consistently on one side of a distant level is
// not "acceptance through" it). A plan with no levels never dies this way.
//
// Deprecated in favour of PlanIsDeadSince: judging a plan against the WHOLE bar
// cache asks "was this level ever traded through in the last ~33 hours", which is
// true of almost any level the moment real history exists. Retained so existing
// callers/tests keep compiling; it passes sinceMs=0 (no lower bound).
func PlanIsDead(doc PlanDoc, bars []market.Kline, rule string, now int64) bool {
	return PlanIsDeadSince(doc, bars, rule, 0, now)
}

// PlanIsDeadSince is PlanIsDead judged ONLY on bars that closed at/after sinceMs
// — normally the moment the plan was written.
//
// THE BUG THIS FIXES (2026-08-17): the caller fed the full 2000×1m cache (~33
// hours), so every level inside the last day and a half's range counted as both
// touched and accepted-through the instant a plan was born. 2026-08-16:ASIA died
// five times in a row that way — v1..v5 each held 6 perfectly good levels
// (EQL 30199.5 / ONH 30203 / ONL 30166.25 …) clustered in a ~57pt band that the
// session had obviously traded through earlier — until the re-plan budget ran out
// and writeNoTradePlan stored a levels:null NO-TRADE plan. The card then showed
// "No levels in this plan", which is what the owner saw on "every version".
//
// It is latent whenever the cache is thin and fires the moment real history
// arrives, which is exactly what happened when the weekend bar-starvation
// (AddOn watchdog livelock, 7aa521a1) ended and 2000 bars/TF were seeded.
//
// A plan can only be invalidated by what the market did AFTER it was written.
func PlanIsDeadSince(doc PlanDoc, bars []market.Kline, rule string, sinceMs, now int64) bool {
	if len(doc.Levels) == 0 {
		return false
	}
	if sinceMs > 0 {
		bars = barsSince(bars, sinceMs)
		// Too little post-plan history to judge — a plan is never dead on no evidence.
		if len(bars) == 0 {
			return false
		}
	}
	// SECOND ROOT CAUSE, same P0: acceptance must be counted on the timeframe the
	// rule names. ClosesBeyond counts BARS, not minutes, and the series handed in
	// is the 1-minute SVP cache — so "2x5m" (two 5-minute closes = 10 minutes of
	// acceptance) was being decided by two ONE-minute closes. Measured at v5's real
	// death check: every level in the stack read "consumed" while price sat at
	// 30193.50, inside the stack — PDL had 997 consecutive closes above it, EQL
	// 1275 below. That is a proximity test, not a consumption test: any level more
	// than two 1m closes away from price is automatically "accepted through".
	judge := AcceptanceBars(bars, rule)
	for _, l := range doc.Levels {
		touched := levelTouched(judge, l.Price, now)
		consumed := !LevelStillValidOn(judge, l.Price, rule, now)
		if !(touched && consumed) {
			return false // this level is still in play → plan not dead
		}
	}
	return true // every level touched AND accepted through → dead
}

// PlanDeathDetail names WHAT killed a plan, so a death can never again be a bare
// "DIED" line. 2026-08-16 lost a whole session to six deaths whose only record
// was five identical log lines carrying no condition and no price.
type PlanDeathDetail struct {
	// Killer is the human-readable condition, e.g.
	// "all 6 levels touched and accepted through (last: ONH 30203.00)".
	Killer string
	// Price is the latest closed price at the moment of the check.
	Price float64
	// Levels lists each level with the evidence that consumed it.
	Levels []string
}

// DescribePlanDeath explains a death decision using the SAME window and timeframe
// the decision itself used, so the explanation can never disagree with the
// verdict. Returns ok=false when the plan is not dead.
func DescribePlanDeath(doc PlanDoc, bars []market.Kline, rule string, sinceMs, now int64) (PlanDeathDetail, bool) {
	if !PlanIsDeadSince(doc, bars, rule, sinceMs, now) {
		return PlanDeathDetail{}, false
	}
	judge := AcceptanceBars(barsSince(bars, sinceMs), rule)
	price, _ := referenceClose(judge, now)
	d := PlanDeathDetail{Price: price}
	last := ""
	for _, l := range doc.Levels {
		f := EvaluateLevelFacts(judge, l.Price, DirAbove, rule, 3, now)
		up, dn := f.ClosesBeyondUp, f.ClosesBeyondDown
		side, n := "above", up
		if dn > up {
			side, n = "below", dn
		}
		d.Levels = append(d.Levels, fmt.Sprintf("%s %.2f accepted %s (%d× %dm closes)",
			l.Label, l.Price, side, n, AcceptanceIntervalMinutes(rule)))
		last = fmt.Sprintf("%s %.2f", l.Label, l.Price)
	}
	d.Killer = fmt.Sprintf("all %d levels touched and accepted through on %s bars (last: %s); price %.2f",
		len(doc.Levels), rule, last, price)
	return d, true
}

// barsSince returns the bars whose OPEN time is at/after sinceMs — the window a
// plan may legitimately be judged on. Ascending order is preserved.
func barsSince(bars []market.Kline, sinceMs int64) []market.Kline {
	for i := range bars {
		if bars[i].OpenTime >= sinceMs {
			return bars[i:]
		}
	}
	return nil
}

// BarsSince is the EXPORTED window helper (P1c): every consumer that judges
// acceptance/consumption must window the series to what happened AFTER the
// thing it is judging was born — the full-cache, windowless evaluation is what
// burned fresh levels in minutes (2026-08-17 NY: 12 of 12 levels "done").
func BarsSince(bars []market.Kline, sinceMs int64) []market.Kline {
	return barsSince(bars, sinceMs)
}

// LevelTouchedOn reports whether price reached (touched) the level on the
// RULE-TIMEFRAME series since the window start — the gate that separates
// "accepted through" from "price merely sits beyond the level".
func LevelTouchedOn(bars []market.Kline, level float64, rule string, nowMs int64) bool {
	return levelTouched(AcceptanceBars(bars, rule), level, nowMs)
}

// ConsumedSince reports the P1c consumption verdict for one level, windowed:
// the level is consumed ONLY IF it was touched in-window AND then accepted
// through (need consecutive rule-TF closes beyond) — a wick alone never
// consumes, and sitting beyond the level without touching it never consumes.
func ConsumedSince(bars []market.Kline, level float64, rule string, sinceMs, nowMs int64) bool {
	w := BarsSince(bars, sinceMs)
	return LevelTouchedOn(w, level, rule, nowMs) && !LevelStillValidOn(w, level, rule, nowMs)
}

// levelTouched reports whether any closed bar's range bracketed the level.
func levelTouched(bars []market.Kline, level float64, now int64) bool {
	for i := range bars {
		b := bars[i]
		if b.CloseTime >= now {
			continue
		}
		if b.Low <= level && b.High >= level {
			return true
		}
	}
	return false
}

// conditionRule maps a PlanCondition.Rule to the acceptance-rule string the
// counters understand. E1 (entry-mechanics 2026-08-30): the 15m condition
// variant is DEAD — new authorship is schema-rejected
// (condition_rule_15m_removed). Legacy stored docs keep evaluating below.
func conditionRule(c PlanCondition) string {
	switch c.Rule {
	case "15m_close": // legacy: stored pre-entry-mechanics docs still evaluate
		return "15m-close"
	case "5m_close":
		// A2 (fail-register wave, 2026-08-20): 5m_close used to silently map to
		// "2x5m" — a plan authored to die on ONE 5m close required TWO, and the
		// death log printed the author's rule next to the count it did not use
		// (anatomy FAIL F4). It now evaluates AS AUTHORED: one 5m close.
		return "5m-close"
	}
	return "2x5m"
}

// PlanConditionFiredSince evaluates a STRUCTURED death/flip predicate on the
// rule timeframe, windowed to what happened AFTER the plan was written
// (P1c touch-gate: the level must have been touched in-window before acceptance
// counts). Returns (fired, human reason).
//
// PLAN-LIFECYCLE HYSTERESIS (2026-08-27): invalidation requires ALL of
//
//	(a) CLOSE-based only — wicks through the line do nothing (the touch gate
//	    brackets the RAW line; the count below is closes beyond the BUFFERED
//	    line, so a wick alone can never count);
//	(b) the line buffered by FLIP_ATR_BUFFER (default 0.5) × ATR14(5m) beyond
//	    the stated level;
//	(c) FLIP_CONFIRM_CLOSES (default 2) CONSECUTIVE decision-TF closes beyond.
//
// A single close beyond the raw line inside the buffer no longer invalidates.
func PlanConditionFiredSince(c PlanCondition, bars []market.Kline, sinceMs, nowMs int64) (bool, string) {
	if c.Price <= 0 {
		return false, ""
	}
	rule := conditionRule(c)
	w := BarsSince(bars, sinceMs)
	if len(w) == 0 {
		return false, ""
	}
	judge := AcceptanceBars(w, rule)
	if !levelTouched(judge, c.Price, nowMs) {
		return false, ""
	}
	dir := DirBelow
	if c.Side == "above" {
		dir = DirAbove
	}
	// (b) buffered line — ATR14 on 5m buckets; a thin window (ATR=0) degrades
	// to the raw line, never to an invented buffer.
	buf := FlipATRBuffer() * market.ExportCalculateATR(AcceptanceBars(w, "2x5m"), 14)
	if buf < 0 {
		buf = 0
	}
	line := c.Price - buf
	if c.Side == "above" {
		line = c.Price + buf
	}
	// (c) consecutive closes beyond the buffered line, floored at the confirm count.
	n := RuleClosesBeyond(w, line, dir, rule, nowMs)
	need := RuleAcceptanceNeed(rule)
	if need < FlipConfirmCloses() {
		need = FlipConfirmCloses()
	}
	sideWord := "below"
	if c.Side == "above" {
		sideWord = "above"
	}
	if n >= need {
		return true, fmt.Sprintf("%s close %s %.2f (buffer %.1f×ATR14, %d× %dm closes)", c.Rule, sideWord, line, FlipATRBuffer(), n, AcceptanceIntervalMinutes(rule))
	}
	return false, ""
}

// PlanConditionClearedSince is the REARM half of the hysteresis pair: the
// dormant plan re-arms when price closes back on the VALID side of the same
// buffered line (opposite side, same buffer magnitude, same consecutive-close
// count). A condition with no price re-arms immediately (nothing to judge).
func PlanConditionClearedSince(c PlanCondition, bars []market.Kline, sinceMs, nowMs int64) (bool, string) {
	if c.Price <= 0 {
		return true, "no machine condition (re-arm immediately)"
	}
	rule := conditionRule(c)
	w := BarsSince(bars, sinceMs)
	if len(w) == 0 {
		return false, ""
	}
	buf := FlipATRBuffer() * market.ExportCalculateATR(AcceptanceBars(w, "2x5m"), 14)
	if buf < 0 {
		buf = 0
	}
	// valid side is the OPPOSITE of the fired side, judged on the SAME buffered
	// line mirrored across the raw level (below-condition → closes above
	// price+buffer; above-condition → closes below price−buffer). Symmetric.
	dir := DirAbove
	line := c.Price + buf
	if c.Side == "above" {
		dir = DirBelow
		line = c.Price - buf
	}
	n := RuleClosesBeyond(w, line, dir, rule, nowMs)
	need := RuleAcceptanceNeed(rule)
	if need < FlipConfirmCloses() {
		need = FlipConfirmCloses()
	}
	sideWord := "above"
	if c.Side == "above" {
		sideWord = "below"
	}
	if n >= need {
		return true, fmt.Sprintf("%s close back %s %.2f (buffer %.1f×ATR14, %d× %dm closes)", c.Rule, sideWord, line, FlipATRBuffer(), n, AcceptanceIntervalMinutes(rule))
	}
	return false, ""
}

// FlipATRBuffer is the deadband, in ATR14(5m) multiples, added to a structured
// death/flip line before closes count against it (FLIP_ATR_BUFFER, default 0.5).
func FlipATRBuffer() float64 {
	if v := strings.TrimSpace(os.Getenv("FLIP_ATR_BUFFER")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			return f
		}
	}
	return 0.5
}

// FlipConfirmCloses is the minimum CONSECUTIVE decision-TF closes beyond the
// buffered line required for invalidation/re-arm (FLIP_CONFIRM_CLOSES, default 2).
func FlipConfirmCloses() int {
	if v := strings.TrimSpace(os.Getenv("FLIP_CONFIRM_CLOSES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 5 {
			return n
		}
	}
	return 2
}

// DormantMinHoldMin is the flap guard: a plan that just went dormant cannot
// re-arm until this many minutes have passed (DORMANT_MIN_HOLD_MIN, default 5).
// Together with the buffered-close re-arm predicate it stops dormant→rearm→
// dormant cycling in chop (one state-change per 5m at most).
func DormantMinHoldMin() int64 {
	if v := strings.TrimSpace(os.Getenv("DORMANT_MIN_HOLD_MIN")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			return n
		}
	}
	return 5
}

// PlanDeathOrFlipSince reports whether the plan died by ANY machine path since
// its birth: (1) the planner's own structured death condition (P0.3),
// (2) the structured flip condition (a fired flip IS a death — the plan's
// thesis inverted), (3) the legacy all-levels-consumed fallback. Returns the
// killer line for the log/card.
func PlanDeathOrFlipSince(doc PlanDoc, bars []market.Kline, rule string, sinceMs, now int64) (string, bool) {
	if fired, reason := PlanConditionFiredSince(orZero(doc.DeathStructured), bars, sinceMs, now); fired {
		return "death-condition: " + reason, true
	}
	if fired, reason := PlanConditionFiredSince(orZero(doc.FlipStructured), bars, sinceMs, now); fired {
		to := doc.FlipStructured.FlipTo
		if to == "" {
			to = "the other side"
		}
		return "flip-condition: " + reason + " → bias " + to, true
	}
	if PlanIsDeadSince(doc, bars, rule, sinceMs, now) {
		return "all levels consumed", true
	}
	return "", false
}

func orZero(c *PlanCondition) PlanCondition {
	if c == nil {
		return PlanCondition{}
	}
	return *c
}

// PlanDeathOrFlipSinceFresh is PlanDeathOrFlipSince plus the G7 staleness
// gate: a condition whose rule-TF series is provably stale is SKIPPED this
// cycle (it neither fires nor clears — the next fresh cycle judges it) and is
// reported in skipped for the flip_eval_skipped log. The legacy
// all-levels-consumed fallback is guarded the same way. Semantics on fresh
// bars are byte-identical to PlanDeathOrFlipSince.
func PlanDeathOrFlipSinceFresh(doc PlanDoc, bars []market.Kline, rule string, sinceMs, now int64) (killer string, fired bool, skipped []string) {
	conds := []struct {
		name string
		c    PlanCondition
	}{
		{"death", orZero(doc.DeathStructured)},
		{"flip", orZero(doc.FlipStructured)},
	}
	for _, cc := range conds {
		if cc.c.Price <= 0 {
			continue
		}
		if allowed, age, why := FlipEvalAllowed(bars, cc.c.Rule, now); !allowed {
			skipped = append(skipped, cc.name+"="+why+" (age "+ageString(age)+")")
			continue
		}
		if fired, reason := PlanConditionFiredSince(cc.c, bars, sinceMs, now); fired {
			// G3 (regime wave 2026-08-21) — FLIP HYSTERESIS: a freshly-written
			// plan cannot flip back within FLIP_MIN_HOLD_MIN of its birth.
			// Death is evaluated FIRST above, so a breached death line always
			// wins during the hold — the hold only suppresses the flip leg.
			if cc.name == "flip" {
				if age := now - sinceMs; sinceMs > 0 && age < FlipMinHoldMin()*60_000 {
					skipped = append(skipped, "flip=hold (plan age "+ageString(age)+" < "+strconv.FormatInt(FlipMinHoldMin(), 10)+"min)")
					continue
				}
			}
			if cc.name == "death" {
				return "death-condition: " + reason, true, skipped
			}
			to := doc.FlipStructured.FlipTo
			if to == "" {
				to = "the other side"
			}
			return "flip-condition: " + reason + " → bias " + to, true, skipped
		}
	}
	if allowed, age, why := FlipEvalAllowed(bars, rule, now); allowed {
		if PlanIsDeadSince(doc, bars, rule, sinceMs, now) {
			return "all levels consumed", true, skipped
		}
	} else {
		skipped = append(skipped, "legacy-consumption="+why+" (age "+ageString(age)+")")
	}
	return "", false, skipped
}

// ageString renders a millisecond age for the skip log ("stale_bars (age 125s)").
func ageString(ms int64) string {
	if ms <= 0 {
		return "0s"
	}
	return strconv.FormatInt(ms/1000, 10) + "s"
}
