package kernel

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/censuswalk"
	"vl/market"
)

// W-EXEC-TRUTH W2 — the confirm resolver, pinned at the production call sites:
// EvaluateScenarioConfirm / EvaluateConfirm (the evaluator every consumer
// calls), RenderConfirmLines (engine_analysis.go's executor-prompt call),
// ValidateBreakdownContinueScenarios (auto_trader_planner.go's write-time call)
// and ParsePlanDocCappedWithMinRR (the planner write site's parse).

func bdScenario(rule string) PlanScenario {
	sc := PlanScenario{ID: "S1", Condition: "breakdown_continue", Direction: "short",
		Trigger: "closes below 100.00", Invalid: "a 5m close above 100.00",
		Breakdown: &PlanBreakdownContinue{Level: 100, LevelLabel: "PDL", EntryMode: "pullback"}}
	if rule != "" {
		sc.Confirm = &PlanConfirm{Rule: rule, RefPrice: 100, Side: "below"}
	}
	return sc
}

// noTouchBars — closes 95, High 96: every close beyond a 100 short level, no
// bar reaches back to it (so leg 2 never fires and leg 1 is read cleanly).
func noTouchBars(base int64, n int) []market.Kline {
	closes := make([]float64, n)
	for i := range closes {
		closes[i] = 95
	}
	return confirmBars(base, closes...)
}

// 1-close: a STORED 1x5m_close is MET on the first completed close even when
// the env says 2 — the env is never consulted for a stored rule.
func TestConfirmResolverStoredOneCloseIgnoresEnv(t *testing.T) {
	t.Setenv("BD_MIN_CLOSES", "2")
	base := truthBase()
	sc := bdScenario("1x5m_close")
	r, ok := ResolveScenarioConfirm(sc)
	if !ok || r.Rule != "1x5m_close" || r.Closes != 1 || r.Source != ConfirmSourceStored || r.RefPrice != 100 || r.Side != "below" {
		t.Fatalf("stored 1x5m_close must resolve to 1 stored close: %+v", r)
	}
	v := EvaluateScenarioConfirm(sc, noTouchBars(base, 5), base, base+5*60_000)
	if len(v.Legs) != 2 || !v.Legs[0].Met || v.Legs[0].Rule != "1x5m_close" || v.RuleSource != ConfirmSourceStored || v.Legs[0].RuleSource != ConfirmSourceStored {
		t.Fatalf("BD_MIN_CLOSES=2 must not be consulted for a stored 1x5m_close: %+v", v)
	}
	out := RenderConfirmLines(PlanDoc{Scenarios: []PlanScenario{sc}}, noTouchBars(base, 5), base, base+5*60_000, 95, 0)
	if !strings.Contains(out, "S1 confirm: leg 1/2 1x5m close — MET · leg 2/2 retest fail — NOT MET") {
		t.Fatalf("render must print the stored rule MET:\n%s", out)
	}
	// With nothing stored, the env IS the authoring default — named.
	d, _ := ResolveScenarioConfirm(bdScenario(""))
	if d.Rule != "2x5m_close" || d.Closes != 2 || d.Source != ConfirmSourceAuthoringDefault || d.Why != "authoring default (no confirm{} stored)" {
		t.Fatalf("no stored confirm → the BD_MIN_CLOSES authoring default, named: %+v", d)
	}
	if got := d.Label(); got != "2x5m_close [BD_MIN_CLOSES=2 · authoring default (no confirm{} stored)]" {
		t.Fatalf("label: %s", got)
	}
	dv := EvaluateScenarioConfirm(bdScenario(""), noTouchBars(base, 10), base, base+10*60_000)
	if dv.Rule != "2x5m_close" || dv.RuleSource != ConfirmSourceAuthoringDefault || dv.Legs[0].RuleSource != ConfirmSourceAuthoringDefault || !dv.Legs[0].Met {
		t.Fatalf("the recorded verdict names the authoring default on the overall AND leg 1: %+v", dv)
	}
}

// 2-close: a STORED 2x5m_close needs TWO completed closes beyond the level —
// the defect counted BD_MIN_CLOSES=1 and read it MET on the first.
func TestConfirmResolverStoredTwoCloses(t *testing.T) {
	t.Setenv("BD_MIN_CLOSES", "1")
	base := truthBase()
	sc := bdScenario("2x5m_close")
	doc := PlanDoc{Scenarios: []PlanScenario{sc}}
	bars := noTouchBars(base, 10)
	one := EvaluateScenarioConfirm(sc, bars[:5], base, base+5*60_000)
	if one.Legs[0].Met || one.Rule != "2x5m_close" || !strings.Contains(one.Legs[0].Detail, "best run 1/2 completed closes below 100.00") {
		t.Fatalf("ONE close cannot satisfy a stored 2x5m_close: %+v", one.Legs[0])
	}
	if out := RenderConfirmLines(doc, bars[:5], base, base+5*60_000, 95, 0); !strings.Contains(out, "leg 1/2 2x5m close — NOT MET") {
		t.Fatalf("render must name the stored rule NOT MET after one close:\n%s", out)
	}
	two := EvaluateScenarioConfirm(sc, bars, base, base+10*60_000)
	if !two.Legs[0].Met || two.Legs[0].EventMs != base+10*60_000 {
		t.Fatalf("the SECOND completed close satisfies it, at its bucket close: %+v", two.Legs[0])
	}
	if out := RenderConfirmLines(doc, bars, base, base+10*60_000, 95, 0); !strings.Contains(out, "leg 1/2 2x5m close — MET") {
		t.Fatalf("render after two closes:\n%s", out)
	}
	st := BreakdownContinueState(sc, bars, base, base+10*60_000)
	if st.Rule.Closes != 2 || st.Rule.Source != ConfirmSourceStored || st.Leg1At != base+10*60_000 {
		t.Fatalf("the state every consumer reads carries the stored count: %+v", st)
	}
}

// touch: unchanged — a resolver-read rule that has no count. The first minute
// whose range contains the ref (a Low exactly AT the ref counts); its event
// instant is that minute's close, so a forming touch has none.
func TestConfirmResolverTouchUnchanged(t *testing.T) {
	base := truthBase()
	bars := confirmBars(base, 103, 102, 101)
	bars[2].Low = 100 // exactly at the ref
	c := PlanConfirm{Rule: "touch", RefPrice: 100, Side: "below"}
	if r := ResolveConfirm(c); r.Kind != ConfirmKindTouch || r.Source != ConfirmSourceStored || r.Closes != 0 || r.HoldMin != 0 {
		t.Fatalf("touch resolves to itself: %+v", r)
	}
	// Unchanged semantics: a forming minute's touch reads MET but has NO
	// ordered event instant yet (it cannot anchor a leg 2).
	if v := EvaluateConfirm(c, bars, base, base+3*60_000-1); !v.Met || v.EventKnown {
		t.Fatalf("a forming touch: MET without an event instant: %+v", v)
	}
	if v := EvaluateConfirm(c, bars[:2], base, base+2*60_000); v.Met {
		t.Fatalf("no bar reached the ref yet: %+v", v)
	}
	v := EvaluateConfirm(c, bars, base, base+3*60_000)
	if !v.Met || v.EventMs != base+3*60_000 || !strings.HasPrefix(v.Detail, "level touched") || v.RuleSource != ConfirmSourceStored {
		t.Fatalf("touch at the closed minute: %+v", v)
	}
	// A stored touch on a waterfall play is not its close rule: named default.
	sc := bdScenario("touch")
	r, _ := ResolveScenarioConfirm(sc)
	if r.Source != ConfirmSourceAuthoringDefault || !strings.Contains(r.Why, `stored confirm.rule "touch" is not a continuation close rule`) {
		t.Fatalf("an illegal stored continuation rule falls back to the NAMED default: %+v", r)
	}
}

// displacement: a DISTINCT rule — measured over every judged bucket the same
// way whether the stored close rule is 1x or 2x (the largest excursion here is
// the FIRST close, before a 2x rule completes leg 1).
func TestConfirmResolverDisplacementUnchangedAcrossCloseRules(t *testing.T) {
	t.Setenv("BD_MAX_LEVEL_DIST_ATR", "100")
	base := truthBase()
	bars := confirmBars(base, 99, 99, 99, 99, 99, 99.5, 99.5, 99.5, 99.5, 99.5)
	bars[2].Low = 97 // the displacement: 3.00 pts, inside the first bucket
	for i := range bars {
		bars[i].High = 99.75 // no retest touch
	}
	now := base + 10*60_000
	scope := VoidScope{Bars: bars, SinceMs: base}
	var msgs []string
	for _, rule := range []string{"1x5m_close", "2x5m_close"} {
		sc := bdScenario(rule)
		st := BreakdownContinueState(sc, bars, base, now)
		if !st.Leg1Met || st.BreakLegPts != 3 {
			t.Fatalf("%s: leg 1 met with displacement 3.00 measured over every judged bucket: %+v", rule, st)
		}
		err := ValidateBreakdownContinueScenarios(&PlanDoc{Scenarios: []PlanScenario{sc}}, scope, 15, 99.5, now)
		if err == nil || !strings.Contains(err.Error(), "measured displacement 3.00 pts < BD_MIN_DISP_ATR 1.0×ATR5m (15.0 pts)") {
			t.Fatalf("%s: the displacement floor is its own rule: %v", rule, err)
		}
		msgs = append(msgs, err.Error())
	}
	if msgs[0] != msgs[1] {
		t.Fatalf("displacement verdict must not depend on the close rule:\n%s\n%s", msgs[0], msgs[1])
	}
}

// forming: the second bucket's close instant −1 / 0 / +1 ms. The guard is the
// shared EvaluateBucketClose predicate, unchanged.
func TestConfirmResolverFormingBoundary(t *testing.T) {
	t.Setenv("BD_MIN_CLOSES", "1")
	base := truthBase()
	sc := bdScenario("2x5m_close")
	bars := noTouchBars(base, 10)
	closeMs := base + 10*60_000
	for _, d := range []int64{-1, 0, 1} {
		v := EvaluateScenarioConfirm(sc, bars, base, closeMs+d)
		if v.Legs[0].Met != (d >= 0) {
			t.Fatalf("%+dms: leg 1 MET=%t (%s)", d, v.Legs[0].Met, v.Legs[0].Detail)
		}
		if d < 0 && (v.Legs[0].Bucket == nil || v.Legs[0].Bucket.Closed || v.Legs[0].Refusal != "forming_bucket") {
			t.Fatalf("-1ms: the forming bucket is named, never counted: %+v", v.Legs[0])
		}
	}
}

// duplicate: nine distinct minutes plus a second copy of one of them is NINE
// minutes of hold, not ten. (Pre-W2 the copy counted: "10/10" MET.)
func TestConfirmResolverDuplicateMinuteCountsOnce(t *testing.T) {
	t.Setenv("ACCEPT_HOLD_MIN", "")
	base := truthBase()
	nine := confirmBars(base, 99, 99, 99, 99, 99, 99, 99, 99, 99)
	bars := append(append(append([]market.Kline{}, nine[:5]...), nine[4]), nine[5:]...)
	_, rep0 := ConfirmationTapeCounters()
	v := EvaluateConfirm(PlanConfirm{Rule: "time_hold", RefPrice: 100, Side: "below"}, bars, base, base+9*60_000)
	if v.Met || !strings.Contains(v.Detail, "9/10 min") {
		t.Fatalf("a repeated minute must count once (9/10 NOT MET): %s", v.Detail)
	}
	if _, rep1 := ConfirmationTapeCounters(); rep1 <= rep0 {
		t.Fatalf("the same-minute replacement must be recorded: %d → %d", rep0, rep1)
	}
}

// out-of-order: a late copy of an earlier bucket's minute cannot re-open that
// bucket as a phantom extra close. (Pre-W2: buckets [Z, A, Z'] → run 2 → MET.)
func TestConfirmResolverOutOfOrderNoPhantomBucket(t *testing.T) {
	base := truthBase()
	bars := confirmBars(base, 99, 101, 101, 101, 101, 99, 99, 99, 99, 99) // Z closes 101 (above), A closes 99 (below)
	late := bars[0]                                                       // minute 0 of Z, close 99 — delivered after A
	bars = append(bars, late)
	now := base + 10*60_000
	drop0, _ := ConfirmationTapeCounters()
	v := EvaluateConfirm(PlanConfirm{Rule: "2x5m_close", RefPrice: 100, Side: "below"}, bars, base, now)
	if v.Met || !strings.Contains(v.Detail, "best run 1/2") {
		t.Fatalf("a late earlier-bucket minute must not mint a second close: %s", v.Detail)
	}
	if drop1, _ := ConfirmationTapeCounters(); drop1 <= drop0 {
		t.Fatalf("the dropped bar must be recorded: %d → %d", drop0, drop1)
	}
	buckets := confirmationBuckets(bars, base, now, 5)
	for i := 1; i < len(buckets); i++ {
		if buckets[i].OpenTime <= buckets[i-1].OpenTime {
			t.Fatalf("bucket open times must strictly increase: %d then %d", buckets[i-1].OpenTime, buckets[i].OpenTime)
		}
	}
	if len(buckets) != 2 {
		t.Fatalf("two real buckets, no phantom: %d", len(buckets))
	}
	// A late PRE-BIRTH bar stays out of the window too (BarsSince kept it).
	pre := append(confirmBars(base+5*60_000, 99, 99, 99, 99, 99), confirmBars(base, 99)...)
	if w := confirmationTape(pre, base+5*60_000, now); len(w) != 5 {
		t.Fatalf("a pre-birth bar after the window start must not leak in: %d bars", len(w))
	}
}

type row435Fixture struct {
	RowID     int            `json:"rowid"`
	Birth     int64          `json:"birth_ms"`
	Scenarios []PlanScenario `json:"scenarios"`
	Bars      []struct {
		T             int64 `json:"open_time_ms"`
		O, H, L, C, V float64
	} `json:"bars"`
}

func loadRow435(t *testing.T) (row435Fixture, []market.Kline) {
	t.Helper()
	var fx row435Fixture
	b, err := os.ReadFile("testdata/confirm_resolver/row435.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &fx); err != nil {
		t.Fatal(err)
	}
	var bars []market.Kline
	for _, x := range fx.Bars {
		bars = append(bars, market.Kline{OpenTime: x.T, CloseTime: x.T + 59_999, Open: x.O, High: x.H, Low: x.L, Close: x.C, Volume: x.V})
	}
	return fx, bars
}

// completedBefore cuts the tape as the replay harness does: minutes whose
// close instant precedes now.
func completedBefore(bars []market.Kline, now int64) []market.Kline {
	var out []market.Kline
	for _, b := range bars {
		if b.CloseTime < now {
			out = append(out, b)
		}
	}
	return out
}

// row 435 (09-21 LONDON v5, created 05:25:30.117 CDT): S1 breakdown_continue
// short, confirm {2x5m_close, 30264, below}, breakdown {30264 ONH, pullback},
// arm 30264/30287/30191.75. Every 5m close after birth is below 30264:
// 05:25–05:30 → 30245.75, 05:30–05:35 → 30259.50, 05:35–05:40 → 30259.00
// (high 30273 — the retest). Pre-W2 (BD_MIN_CLOSES=1 synthesized) leg 1 was
// MET at the 05:30 close; the stored 2x5m_close is MET at the 05:35 close.
func TestConfirmResolverRow435(t *testing.T) {
	t.Setenv("BD_MIN_CLOSES", "1") // the live env [A]
	fx, bars := loadRow435(t)
	if fx.RowID != 435 || len(fx.Scenarios) != 2 || fx.Scenarios[0].ID != "S1" || fx.Scenarios[0].Confirm == nil || fx.Scenarios[0].Confirm.Rule != "2x5m_close" {
		t.Fatalf("fixture: row 435 S1 must store 2x5m_close: %+v", fx.Scenarios)
	}
	s1 := fx.Scenarios[0]
	legacy := s1 // what the defect evaluated: the BD_MIN_CLOSES=1 synthesized rule
	c1 := *s1.Confirm
	c1.Rule = "1x5m_close"
	legacy.Confirm = &c1
	loc, _ := time.LoadLocation("America/Chicago")
	at := func(h, m int) int64 { return time.Date(2026, 9, 21, h, m, 0, 0, loc).UnixMilli() }
	doc := PlanDoc{Scenarios: fx.Scenarios}

	n0530 := at(5, 30)
	tape := completedBefore(bars, n0530)
	if v := EvaluateScenarioConfirm(legacy, tape, fx.Birth, n0530); !v.Legs[0].Met {
		t.Fatalf("before (the defect's 1-close reading) leg 1 MET at 05:30: %+v", v.Legs[0])
	}
	v := EvaluateScenarioConfirm(s1, tape, fx.Birth, n0530)
	if v.Legs[0].Met || v.Met || v.Rule != "2x5m_close" || v.RuleSource != ConfirmSourceStored {
		t.Fatalf("after: the stored 2x5m_close is NOT MET on one close at 05:30: %+v", v)
	}
	out := RenderConfirmLines(doc, tape, fx.Birth, n0530, tape[len(tape)-1].Close, StaleConfirmATR5m(tape))
	if !strings.Contains(out, "  S1 confirm: leg 1/2 2x5m close — NOT MET · leg 2/2 retest fail — NOT MET → overall NOT MET (") {
		t.Fatalf("05:30 render:\n%s", out)
	}
	// S3 is a stored 1x5m_close reclaim — a non-continuation rule: its verdict
	// and run are those of the base (f2ac79eb) render. R22 F1: a reclaim counts
	// closes only after its sweep, so the reference now names the sweep bar
	// (05:26) instead of plan publication (05:25) — same NOT MET, same 0/1.
	const s3Base = "  S3 confirm: 1x5m close above 30264.00 — NOT MET (best run 0/1 completed closes above 30264.00 · 5m bucket 2026-09-21 05:25 CT–2026-09-21 05:30 CT · close 2026-09-21 05:30 CT · closed=true · reference=2026-09-21 05:26 CT (sweep of the ref (first closed 1m bar through it)))\n"
	if !strings.Contains(out, s3Base) {
		t.Fatalf("S3 (non-continuation) must render byte-identically:\n%s", out)
	}

	n0535 := at(5, 35)
	tape = completedBefore(bars, n0535)
	v = EvaluateScenarioConfirm(s1, tape, fx.Birth, n0535)
	if !v.Legs[0].Met || v.Legs[0].EventMs != n0535 || v.Legs[1].Met || v.Met {
		t.Fatalf("after: leg 1 MET at the 05:35 close (second close), leg 2 pending: %+v", v)
	}

	n0540 := at(5, 40)
	tape = completedBefore(bars, n0540)
	v = EvaluateScenarioConfirm(s1, tape, fx.Birth, n0540)
	if !v.Met || !v.Legs[1].Met || v.Legs[1].EventMs != n0540 {
		t.Fatalf("the 05:35–05:40 retest (high 30273, close 30259.00) fails to reclaim → overall MET at 05:40: %+v", v)
	}
	if got := ConfirmGatedRuleLabel(s1); got != "leg 1 2x5m_close [stored] → leg 2 retest_fail" {
		t.Fatalf("the arm log label: %s", got)
	}
}

// row 452 S2 (09-22 ASIA v1): acceptance long, confirm {time_hold, 31009.75,
// above}, trigger "…holds above it for 3 minutes of 1m closes." No duration
// was stored, so it resolved to the 10-minute authoring default. The write
// site now refuses that candidate; hold_min 3 passes and is counted as 3.
func TestConfirmResolverRow452HoldMinutes(t *testing.T) {
	t.Setenv("ACCEPT_HOLD_MIN", "") // default 10 — the live env [A]
	var fx struct {
		RowID int             `json:"rowid"`
		Doc   json.RawMessage `json:"doc"`
	}
	b, err := os.ReadFile("testdata/confirm_resolver/row452.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &fx); err != nil || fx.RowID != 452 {
		t.Fatalf("fixture: %v", err)
	}
	var doc PlanDoc
	if err = json.Unmarshal(fx.Doc, &doc); err != nil {
		t.Fatal(err)
	}
	var s2 PlanScenario
	for _, s := range doc.Scenarios {
		if s.ID == "S2" {
			s2 = s
		}
	}
	if s2.Confirm == nil || s2.Confirm.Rule != "time_hold" || s2.Confirm.HoldMin != nil || !strings.Contains(s2.Trigger, "3 minutes of 1m closes") {
		t.Fatalf("fixture: row 452 S2 stores time_hold with no hold_min: %+v", s2)
	}
	r, _ := ResolveScenarioConfirm(s2)
	if r.Kind != ConfirmKindTimeHold || r.HoldMin != 10 || r.Source != ConfirmSourceAuthoringDefault || r.Why != "authoring default (no hold_min stored)" {
		t.Fatalf("stored row 452 S2 resolves to the NAMED authoring default: %+v", r)
	}
	// The write site (auto_trader_planner.go → ParsePlanDocCappedWithMinRR).
	_, err = ParsePlanDocCappedWithMinRR(string(fx.Doc), 12, 5, 2.0)
	if err == nil || !strings.Contains(err.Error(), "scenario[1].confirm: the prose states a 3-minute hold but confirm.hold_min is absent") || !strings.Contains(err.Error(), "(hold_min: 3)") {
		t.Fatalf("the new-authoring write must refuse prose-3 with hold_min absent: %v", err)
	}
	// History untouched: the stored-read path never runs the prose check.
	if _, err := parsePlanDocument(string(fx.Doc), 12, 5, false, AuthoringOpts{}); err != nil {
		t.Fatalf("a stored row is never re-judged for hold_min: %v", err)
	}
	withHold := func(n int) string {
		var m map[string]any
		_ = json.Unmarshal(fx.Doc, &m)
		for _, s := range m["scenarios"].([]any) {
			sm := s.(map[string]any)
			if sm["id"] == "S2" {
				sm["confirm"].(map[string]any)["hold_min"] = n
			}
		}
		out, _ := json.Marshal(m)
		return string(out)
	}
	if _, err := ParsePlanDocCappedWithMinRR(withHold(10), 12, 5, 2.0); err == nil || !strings.Contains(err.Error(), "confirm.hold_min 10 disagrees with the prose, which states a 3-minute hold") {
		t.Fatalf("hold_min 10 against prose 3 must be refused: %v", err)
	}
	d3, err := ParsePlanDocCappedWithMinRR(withHold(3), 12, 5, 2.0)
	if err != nil {
		t.Fatalf("hold_min 3 matching the prose must pass the write site: %v", err)
	}
	var s23 PlanScenario
	for _, s := range d3.Scenarios {
		if s.ID == "S2" {
			s23 = s
		}
	}
	r3, _ := ResolveScenarioConfirm(s23)
	if r3.HoldMin != 3 || r3.Source != ConfirmSourceStored {
		t.Fatalf("the stored 3 is the count: %+v", r3)
	}
	// …and the evaluator counts THREE minutes, not the env's ten.
	base := truthBase()
	bars := confirmBars(base, 31012, 31012, 31012)
	if v := EvaluateConfirm(*s23.Confirm, bars, base, base+3*60_000); !v.Met || !strings.Contains(v.Detail, "3/3 min") || v.RuleSource != ConfirmSourceStored {
		t.Fatalf("stored hold_min 3 is MET after three completed 1m closes: %+v", v)
	}
	if v := EvaluateConfirm(*s2.Confirm, bars, base, base+3*60_000); v.Met || !strings.Contains(v.Detail, "3/10 min") || v.RuleSource != ConfirmSourceAuthoringDefault {
		t.Fatalf("the stored row without hold_min keeps the named 10-minute default: %+v", v)
	}
}

func TestConfirmResolverHoldMinStructural(t *testing.T) {
	three, zero := 3, 0
	if err := validateConfirmHoldMin(0, "confirm", &PlanConfirm{Rule: "1x5m_close", HoldMin: &three}); err == nil || !strings.Contains(err.Error(), "only valid with rule time_hold") {
		t.Fatalf("hold_min on a close rule: %v", err)
	}
	if err := validateConfirmHoldMin(0, "confirm", &PlanConfirm{Rule: "time_hold", HoldMin: &zero}); err == nil || !strings.Contains(err.Error(), "must be > 0") {
		t.Fatalf("hold_min 0: %v", err)
	}
	if err := validateConfirmHoldMin(0, "confirm", &PlanConfirm{Rule: "time_hold"}); err != nil {
		t.Fatalf("absent hold_min is structurally legal (the default applies, named): %v", err)
	}
}

func TestProseHoldMinutes(t *testing.T) {
	for prose, want := range map[string]string{
		"Price breaks above 31009.75, retests it, and holds above it for 3 minutes of 1m closes.": "3",
		"1m close below 31009.75 breaks the acceptance and invalidates the long.":                 "",
		"holds for three consecutive 1m closes above 100":                                         "3",
		"a 1-minute close above 100 then a ten-minute hold":                                       "10",
		"a 5m close above 100, then holds 2 mins; void on a 15 minute close below":                "2",
		"no minutes here": "",
	} {
		if got := joinInts(ProseHoldMinutes(prose)); got != want {
			t.Errorf("%q → %q, want %q", prose, got, want)
		}
	}
}

// Source-scan pin: the env authoring defaults are read ONLY by the resolver,
// the boot ledger and the planner prompt's default text; no "%dx5m_close"
// synthesis outside the resolver; plan death/flip never call the resolver.
func TestConfirmResolverSourceScanPin(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	offenders, err := confirmResolverOffenders(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range offenders {
		t.Error(o)
	}
}

// confirmResolverOffenders is the shared scan: every non-test .go file under
// root (censuswalk) is checked against the allowed map; offenders are returned
// as "rel:line message" strings.
func confirmResolverOffenders(root string) (offenders []string, err error) {
	allowed := map[string]map[string]bool{
		"bdConfirmCloses": {"kernel/confirm_resolver.go": true, "kernel/entry_law.go": true, "kernel/planner_prompt.go": true},
		"AcceptHoldMin":   {"kernel/confirm_resolver.go": true, "kernel/entry_law.go": true},
	}
	files, werr := censuswalk.NonTestGoFiles(root)
	if werr != nil {
		return nil, werr
	}
	for _, cf := range files {
		path := cf.Path
		rel := cf.Rel
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				name := ""
				switch fn := x.Fun.(type) {
				case *ast.Ident:
					name = fn.Name
				case *ast.SelectorExpr:
					name = fn.Sel.Name
				}
				if files, ok := allowed[name]; ok && !files[rel] {
					offenders = append(offenders, fmt.Sprintf("%s:%d calls %s() — the env authoring default is read only by the resolver (and the boot ledger / prompt default text)", rel, fset.Position(x.Pos()).Line, name))
				}
				if (name == "ResolveConfirm" || name == "ResolveScenarioConfirm") && rel == "kernel/plan_lifecycle.go" {
					offenders = append(offenders, fmt.Sprintf("%s:%d — plan death/flip keep their own rules; the confirm resolver never answers for them", rel, fset.Position(x.Pos()).Line))
				}
			case *ast.BasicLit:
				if x.Kind == token.STRING && strings.Contains(x.Value, "%dx5m_close") && rel != "kernel/confirm_resolver.go" {
					offenders = append(offenders, fmt.Sprintf("%s:%d synthesizes a close rule from a number — only the resolver may", rel, fset.Position(x.Pos()).Line))
				}
			}
			return true
		})
	}
	return offenders, nil
}

// TestConfirmResolverScanSeesNestedSkipNamedDirs plants a bdConfirmCloses()
// call in EVERY censuswalk.NestedProbeDirs directory of a synthetic module and
// asserts the scan reports every one. With the old any-depth SkipDir the dirs
// named like a root skip were invisible (CLASS 258).
func TestConfirmResolverScanSeesNestedSkipNamedDirs(t *testing.T) {
	root := t.TempDir()
	dirs := censuswalk.NestedProbeDirs()
	for _, dir := range dirs {
		full := filepath.Join(root, filepath.FromSlash(dir))
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
		src := "package " + censuswalk.PackageName(dir) + "\n\nfunc offender() { _ = bdConfirmCloses() }\n"
		if err := os.WriteFile(filepath.Join(full, "offender.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	offenders, err := confirmResolverOffenders(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, o := range offenders {
		seen[strings.SplitN(o, ":", 2)[0]] = true
	}
	var missed []string
	for _, dir := range dirs {
		if !seen[dir+"/offender.go"] {
			missed = append(missed, dir)
		}
	}
	if len(missed) > 0 {
		t.Fatalf("the confirm-resolver scan skipped %d of %d nested probe dirs — a skip by NAME at depth exempts compiled packages (CLASS 258):\n\t%s",
			len(missed), len(dirs), strings.Join(missed, "\n\t"))
	}
}
