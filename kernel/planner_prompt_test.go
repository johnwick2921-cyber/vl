package kernel

import (
	"strings"
	"testing"
)

func samplePlannerInput() PlannerInput {
	return PlannerInput{
		TradeDate: "2026-08-14",
		Session:   "NY",
		ReadKind:  "read 08:00 CT (live)",
		Price:     15600,
		DATR:      120,
		Regime:    RegimeBlock{TrendDaily: "up", Trend1h: "up", ATR14: 118, ATRRegime: "NORMAL", ATRPercentile: 55, VIXRegime: "unavailable"},
		Levels: []ScoredLevel{
			{DetectedLevel: DetectedLevel{Kind: KindPDH, Price: 15620, Label: "PDH"}, Grade: "A", Fresh: "fresh", Distance: 20},
			{DetectedLevel: DetectedLevel{Kind: KindPDL, Price: 15450, Label: "PDL"}, Grade: "A", Fresh: "fresh", Distance: -150},
		},
		StructureSummary: []string{"D: up, above EMA200", "1h: up, holding VWAP"},
		OvernightStory:   "swept ONH, reclaimed",
		PriorDayStory:    "trend up into the close",
		Calendar:         []PlannerCalendarEvent{{TimeCT: "13:00", Currency: "USD", Title: "FOMC", Impact: "T1"}},
		DigestChain:      []string{"yesterday: trend day, +2R captured", "Mon: balance"},
		OwnerNote:        "respect PDH, don't chase",
	}
}

func TestBuildPlannerPrompt(t *testing.T) {
	p := BuildPlannerPrompt(samplePlannerInput())
	for _, want := range []string{
		"DAY-PLAN READER", "trade_date 2026-08-14 · session NY",
		"REGIME: trend D=up", "Ranked levels", "PDH", "Calendar", "FOMC", "HARD no-trade blackout",
		"OWNER GUIDANCE", "<<<OWNER_GUIDANCE\nrespect PDH, don't chase\nOWNER_GUIDANCE>>>", `"reasoning"`, // R22 R4: the delimited owner block "sweep_reclaim", "death_condition",
		`"quality": "A+|A|B|C"`, "C = machine-demoted",
		// A1/A2 (2026-08-26) — anchor-role truth + condition×session guidance.
		"Anchor roles", "LIQUIDITY/BREAKOUT references", "Fade ONLY on a confirmed sweep-reclaim",
		"Condition×session guidance", "reject-based setups are best in NY RTH",
		"sweep_reclaim requires the reclaim CLOSE",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("planner prompt missing %q\n---\n%s", want, p)
		}
	}
	// A1 — the old fade-resistance framing must be gone.
	for _, old := range []string{"fade resistance", "ONH is resistance — fade it", "fade the ONH"} {
		if strings.Contains(strings.ToLower(p), old) {
			t.Fatalf("stale ONH fade framing %q must be absent", old)
		}
	}
}

// TestSamplePlannerPrompt logs the assembled prompt for the exit-bar report.
func TestSamplePlannerPrompt(t *testing.T) {
	t.Logf("\n%s", BuildPlannerPrompt(samplePlannerInput()))
}

// F0.4-T2 (2026-08-24) — T2 caution events must NEVER become no-trade stops:
// the calendar section says so per-event, and the output contract forbids it.
// Live finding: the model added "Bessent Speaks (T2)" to no_trade on its own.
func TestPlannerPromptT2CautionIsNeverNoTrade(t *testing.T) {
	in := samplePlannerInput()
	in.Calendar = []PlannerCalendarEvent{
		{TimeCT: "13:00", Currency: "USD", Title: "Treasury Sec Bessent Speaks", Impact: "T2"},
		{TimeCT: "09:30", Currency: "USD", Title: "CPI", Impact: "T1"},
	}
	p := BuildPlannerPrompt(in)
	for _, want := range []string{
		"Treasury Sec Bessent Speaks", "caution — NOT a no-trade blackout",
		// SUPERSEDED (2026-09-03): the tag used to ORDER the author to add T1
		// blackouts to no_trade, which the dictated instruction now forbids.
		// The blackout is still marked HARD and still reaches the model.
		"CPI", "HARD no-trade blackout — the machine writes and enforces it",
		"T2 caution event is NEVER added to no_trade",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q\n---\n%s", want, p)
		}
	}
}

// G2.2 (2026-08-24) — the HTF zones section: zones that lose the top-8 seat
// race still reach the model as a dedicated confluence section.
func TestPlannerPromptHTFZonesSection(t *testing.T) {
	in := samplePlannerInput()
	in.HTFZones = []ScoredLevel{
		{DetectedLevel: DetectedLevel{Kind: KindDemand, Price: 29050, Label: "Demand·1h", HTF: true, TF: "1h"}, Grade: "B", Fresh: "fresh", Distance: -100},
		{DetectedLevel: DetectedLevel{Kind: KindSupply, Price: 29300, Label: "Supply·4h", HTF: true, TF: "4h"}, Grade: "C", Fresh: "fresh", Distance: 150},
	}
	p := BuildPlannerPrompt(in)
	for _, want := range []string{
		"## HTF zones", "Demand·1h", "Supply·4h", "(HTF zone)",
		"you MUST include at least ONE HTF zone row",
		"the nearest 1h supply/demand zone row in that section MUST be one of your included rows",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q\n---\n%s", want, p)
		}
	}
}

// 1h wave (2026-08-25) — the 1h mandate is CONDITIONAL: no 1h S/D zone in the
// section → the mandate line must NOT appear (same conditional pattern as the
// G2.2 HTF mandate fix).
func TestPlannerPrompt1HMandateConditional(t *testing.T) {
	in := samplePlannerInput()
	in.HTFZones = []ScoredLevel{
		{DetectedLevel: DetectedLevel{Kind: KindSupply, Price: 29300, Label: "Supply·4h", HTF: true, TF: "4h"}, Grade: "C", Fresh: "fresh", Distance: 150},
	}
	p := BuildPlannerPrompt(in)
	if !strings.Contains(p, "you MUST include at least ONE HTF zone row") {
		t.Fatalf("HTF mandate must stay when zones exist: %s", p)
	}
	if strings.Contains(p, "nearest 1h supply/demand zone") {
		t.Fatalf("1h mandate must be absent without a 1h S/D zone: %s", p)
	}
	in.HTFZones = nil
	p = BuildPlannerPrompt(in)
	if strings.Contains(p, "you MUST include at least ONE HTF zone row") {
		t.Fatalf("HTF mandate must be absent without zones: %s", p)
	}
}

// ── CLASS 50b — the dual-label line (live-bias replay ruling 53498adb) ──────

func TestBiasLabelLineShape(t *testing.T) {
	got := BiasLabelLine("LONG", "Short", "Neutral")
	if got != "bias: AI long · tree short · regime neutral" {
		t.Fatalf("label shape = %q, want 'bias: AI long · tree short · regime neutral'", got)
	}
	if got := BiasLabelLine("", "", ""); got != "bias: AI neutral · tree neutral · regime neutral" {
		t.Fatalf("empty legs normalize to neutral: %q", got)
	}
	// THE LAW: the line never imposes a MUST on either leg.
	if strings.Contains(strings.ToLower(got), "must") {
		t.Fatalf("label line carries a MUST: %q", got)
	}
}

func TestTreeCallWordBranches(t *testing.T) {
	// branch 1 / mirror / branch 3 / branch 5 veto
	if c := TreeCallWord(105, 100, 95, 102); c != "long" {
		t.Fatalf("branch 1: %q", c)
	}
	if c := TreeCallWord(90, 100, 95, 102); c != "short" {
		t.Fatalf("branch 1 mirror: %q", c)
	}
	if c := TreeCallWord(103, 100, 95, 102); c != "long" {
		t.Fatalf("branch 3 long: %q", c)
	}
	// branch 3 short: below PDC but NOT in discount (pos ≥ 50% of the range)
	if c := TreeCallWord(98, 100, 95, 98.5); c != "short" {
		t.Fatalf("branch 3 short: %q", c)
	}
	// premium veto: price at 75% of range → long disallowed
	if c := TreeCallWord(98.75, 100, 95, 98); c != "neutral" {
		t.Fatalf("branch 5 premium veto: %q", c)
	}
	// discount veto: price at 25% of range → short disallowed
	if c := TreeCallWord(96.25, 100, 95, 99); c != "neutral" {
		t.Fatalf("branch 5 discount veto: %q", c)
	}
	if c := TreeCallWord(0, 0, 0, 0); c != "neutral" {
		t.Fatalf("zero inputs: %q", c)
	}
}

func TestRegimeCallWord(t *testing.T) {
	if c := RegimeCallWord(RegimeBlock{TrendDaily: "up", Trend1h: "up"}); c != "up" {
		t.Fatalf("both up: %q", c)
	}
	if c := RegimeCallWord(RegimeBlock{TrendDaily: "down", Trend1h: "down"}); c != "down" {
		t.Fatalf("both down: %q", c)
	}
	if c := RegimeCallWord(RegimeBlock{TrendDaily: "up", Trend1h: "down"}); c != "neutral" {
		t.Fatalf("mixed: %q", c)
	}
	if c := RegimeCallWord(RegimeBlock{TrendDaily: "flat", Trend1h: "flat"}); c != "neutral" {
		t.Fatalf("flat: %q", c)
	}
}
