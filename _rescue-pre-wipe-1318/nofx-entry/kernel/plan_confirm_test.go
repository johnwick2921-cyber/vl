package kernel

import (
	"strings"
	"testing"

	"nofx/market"
)

func confirmBars(base int64, closes ...float64) []market.Kline {
	var out []market.Kline
	for i, cl := range closes {
		o := base + int64(i)*60_000
		out = append(out, market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: cl, High: cl + 1, Low: cl - 1, Close: cl})
	}
	return out
}

// C1 — authored rule == evaluated rule (with A2's semantics): 1x5m_close needs
// ONE 5m bucket beyond; 2x5m_close needs two. (E1: the 15m variant is dead.)
func TestConfirmRuleIdentity(t *testing.T) {
	base := int64(1_700_000_100_000)
	base -= base % 900_000 // 15m-aligned
	// ten 1m bars: first 5m bucket closes below 100, second closes above.
	bars := confirmBars(base, 99, 99, 99, 99, 99, 101, 101, 101, 101, 101)
	oneBelow := PlanConfirm{Rule: "1x5m_close", RefPrice: 100, Side: "below"}
	if v := EvaluateConfirm(oneBelow, bars, base-1, base+10*60_000); !v.Met {
		t.Fatalf("1x5m_close below: one 5m close beyond must be MET (%s)", v.Detail)
	}
	twoBelow := PlanConfirm{Rule: "2x5m_close", RefPrice: 100, Side: "below"}
	if v := EvaluateConfirm(twoBelow, bars, base-1, base+10*60_000); v.Met {
		t.Fatalf("2x5m_close below: ONE close must NOT satisfy two (%s)", v.Detail)
	}
	touch := PlanConfirm{Rule: "touch", RefPrice: 100, Side: "below"}
	if v := EvaluateConfirm(touch, bars, base-1, base+10*60_000); !v.Met {
		t.Fatalf("touch: highs/lows straddle 100 (%s)", v.Detail)
	}
}

// The MET detail line carries the last rule-TF close (the executor-prompt fact).
func TestConfirmDetailCarriesLastClose(t *testing.T) {
	base := int64(1_700_003_600_000)
	base -= base % 900_000
	bars := confirmBars(base, 99, 99, 99, 99, 99)
	v := EvaluateConfirm(PlanConfirm{Rule: "1x5m_close", RefPrice: 100, Side: "below"}, bars, base-1, base+5*60_000)
	if !strings.Contains(v.Detail, "close") {
		t.Fatalf("detail must state the last close: %q", v.Detail)
	}
}

// Validator: confirm enum + object↔prose number agreement (the A3 contract) +
// E1's named 15m rejection.
func TestConfirmValidator(t *testing.T) {
	d := validBaseDoc()
	// E1 — the dead variant is rejected BY NAME (never a generic enum error).
	d.Scenarios[0].Confirm = &PlanConfirm{Rule: "15m_close", RefPrice: 29648.25, Side: "above"}
	if err := ValidatePlanDocWithCaps(d, 8, 3); err == nil || !strings.Contains(err.Error(), "confirm_rule_15m_removed") {
		t.Fatalf("15m confirm must fail with the NAMED message (got %v)", err)
	}
	// E2 — a reject fade must carry a touch confirm (fade_requires_touch).
	d = validBaseDoc()
	d.Scenarios[0].Confirm = &PlanConfirm{Rule: "1x5m_close", RefPrice: 29648.25, Side: "below"}
	if err := ValidatePlanDocWithCaps(d, 8, 3); err == nil || !strings.Contains(err.Error(), "fade_requires_touch") {
		t.Fatalf("close-confirm on a reject fade must fail with fade_requires_touch (got %v)", err)
	}
	// The coherent shape for a reject: touch at the level, price in prose.
	d = validBaseDoc()
	d.Scenarios[0].Confirm = &PlanConfirm{Rule: "touch", RefPrice: 29648.25, Side: "below"}
	if err := ValidatePlanDocWithCaps(d, 8, 3); err != nil {
		t.Fatalf("coherent touch confirm must pass: %v", err)
	}
	d.Scenarios[0].Confirm.Rule = "3x1m"
	if err := ValidatePlanDocWithCaps(d, 8, 3); err == nil || !strings.Contains(err.Error(), "confirm.rule") {
		t.Fatalf("bad rule must fail (got %v)", err)
	}
	d.Scenarios[0].Confirm.Rule = "touch"
	d.Scenarios[0].Confirm.RefPrice = 29000 // not in the prose
	if err := ValidatePlanDocWithCaps(d, 8, 3); err == nil || !strings.Contains(err.Error(), "confirm.ref_price") {
		t.Fatalf("prose mismatch must fail (got %v)", err)
	}
}

// E1 — legacy stored docs still parse: a stored 15m confirm must remain
// EVALUABLE by the acceptance machinery (legacy tolerance) even though new
// authorship is schema-rejected. This is the load-path contract.
func TestLegacy15mConfirmStillEvaluates(t *testing.T) {
	base := int64(1_700_000_100_000)
	base -= base % 900_000 // 15m-aligned
	bars := confirmBars(base, 99, 99, 99, 99, 99)
	legacy := PlanConfirm{Rule: "15m_close", RefPrice: 100, Side: "below"}
	if v := EvaluateConfirm(legacy, bars, base-1, base+5*60_000); !v.Met {
		t.Fatalf("a legacy stored 15m confirm must still evaluate MET (%s)", v.Detail)
	}
	if got := conditionRule(PlanCondition{Rule: "15m_close"}); got != "15m-close" {
		t.Fatalf("legacy condition rule mapping changed: %q", got)
	}
}

// The prompt advisory renders one line per confirmed scenario, none when absent.
func TestRenderConfirmLines(t *testing.T) {
	doc := *validBaseDoc()
	if RenderConfirmLines(doc, nil, 0, 0, 0, 0) != "" {
		t.Fatal("no confirm objects → no advisory block")
	}
	doc.Scenarios[0].Confirm = &PlanConfirm{Rule: "touch", RefPrice: 29648.25, Side: "below"}
	out := RenderConfirmLines(doc, confirmBars(1_700_000_100_000-(1_700_000_100_000%900_000), 99, 99, 99), 1, 1_700_000_400_000, 0, 0)
	if !strings.Contains(out, "S1 confirm:") || !strings.Contains(out, "advisory") {
		t.Fatalf("advisory block malformed:\n%s", out)
	}
}

// S2 (mega-research 2026-08-26) — the stale parenthetical is ATR5m-based:
// |now − ref| must be STRICTLY greater than STALE_CONFIRM_ATR (default 2.0) ×
// ATR5m; a missing ATR fail-opens (silent skip); NOT MET never annotates.
func TestStaleConfirmAnnotationMath(t *testing.T) {
	if got := StaleConfirmATR(); got != 2.0 {
		t.Fatalf("default STALE_CONFIRM_ATR must be 2.0 (register S2), got %v", got)
	}
	met := func(ref float64) ConfirmVerdict {
		return ConfirmVerdict{Rule: "1x5m_close", RefPrice: ref, Side: "above", Met: true, Detail: "d"}
	}
	s := PlanScenario{ID: "S1", Direction: "long"}
	// atr=1.0, default 2.0×: fires at 2.1, silent at 1.9 (strict inequality).
	if a := staleConfirmAnnotation(s, met(100), 102.1, 1.0); !strings.Contains(a, "stale — written 100.00 context, price now 102.10; treat as expired") {
		t.Fatalf("2.1 > 2.0×1.0 must be stale, got %q", a)
	}
	if a := staleConfirmAnnotation(s, met(100), 101.9, 1.0); a != "" {
		t.Fatalf("1.9 ≤ 2.0×1.0 must NOT be stale, got %q", a)
	}
	// env override: 1.0× fires at 1.1.
	t.Setenv("STALE_CONFIRM_ATR", "1.0")
	if a := staleConfirmAnnotation(s, met(100), 101.1, 1.0); !strings.Contains(a, "stale") {
		t.Fatalf("env override 1.0× must fire at 1.1, got %q", a)
	}
	// missing ATR → fail-open: skip the annotation, never gate.
	if a := staleConfirmAnnotation(s, met(100), 120, 0); a != "" {
		t.Fatalf("ATR=0 must fail-open silent, got %q", a)
	}
	// NOT MET never gets the stale tag.
	notMet := ConfirmVerdict{Rule: "1x5m_close", RefPrice: 100, Side: "above", Met: false, Detail: "d"}
	if a := staleConfirmAnnotation(s, notMet, 120, 1.0); a != "" {
		t.Fatalf("NOT MET must not be annotated, got %q", a)
	}
}

// S2 — StaleConfirmATR5m: 5m-bucket Wilder ATR14 from the 1m snapshot; 0 when
// the series is too short for a Wilder seed.
func TestStaleConfirmATR5m(t *testing.T) {
	if got := StaleConfirmATR5m(nil); got != 0 {
		t.Fatalf("empty bars → 0, got %v", got)
	}
	base := int64(1_700_003_600_000)
	base -= base % 300_000
	// 80 one-minute bars of growing closes (16 five-min buckets) → non-zero
	// Wilder ATR14 on the 5m aggregation.
	bars := make([]market.Kline, 80)
	for i := range bars {
		o := base + int64(i)*60_000
		c := 100.0 + float64(i)
		bars[i] = market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: c - 1, High: c + 1, Low: c - 2, Close: c}
	}
	if got := StaleConfirmATR5m(bars); got <= 0 {
		t.Fatalf("80-bar 1m series must yield ATR > 0, got %v", got)
	}
	// under 14 five-min buckets (69 one-min bars) → no Wilder seed → 0.
	if got := StaleConfirmATR5m(bars[:20]); got != 0 {
		t.Fatalf("short series must fail-open to 0, got %v", got)
	}
}

// ADDENDUM S — render: two opposite-direction MET confirms with drifted price
// get the stale parenthetical AND one CONFLICT trailer; a single-direction MET
// near its ref renders clean (no CONFLICT, no stale).
func TestRenderConfirmLinesStaleConflict(t *testing.T) {
	base := int64(1_700_003_600_000)
	base -= base % 300_000                             // 5m-aligned
	bars := confirmBars(base, 105, 105, 105, 105, 105) // above 100 AND below 110
	doc := PlanDoc{Scenarios: []PlanScenario{
		{ID: "S1", Direction: "long", Confirm: &PlanConfirm{Rule: "1x5m_close", RefPrice: 100, Side: "above"}},
		{ID: "S3", Direction: "short", Confirm: &PlanConfirm{Rule: "1x5m_close", RefPrice: 110, Side: "below"}},
	}}
	out := RenderConfirmLines(doc, bars, base-1, base+5*60_000, 104.0, 1.5)
	for _, want := range []string{
		"S1 confirm:", "S3 confirm:",
		"stale — written 100.00 context, price now 104.00; treat as expired",
		"stale — written 110.00 context, price now 104.00; treat as expired",
		"CONFLICT: opposing confirms MET — structural ambiguity, default WAIT unless fresh trigger",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}

	// single direction, price still near its ref → clean render.
	doc2 := PlanDoc{Scenarios: []PlanScenario{
		{ID: "S1", Direction: "long", Confirm: &PlanConfirm{Rule: "1x5m_close", RefPrice: 100, Side: "above"}},
	}}
	out2 := RenderConfirmLines(doc2, bars, base-1, base+5*60_000, 100.5, 1.5)
	if strings.Contains(out2, "CONFLICT:") || strings.Contains(out2, "stale —") {
		t.Fatalf("single-direction near-ref render must be clean:\n%s", out2)
	}
}
