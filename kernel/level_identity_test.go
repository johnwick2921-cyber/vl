package kernel

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"vl/levelidentity"
	"vl/market"
)

func TestIdentityE1ParserKeepsAuthoredID(t *testing.T) {
	raw := strings.Replace(validPlanJSON, `"id": "S1"`, `"id": "S1", "level_id": "candidate-one"`, 1)
	doc, err := ParsePlanDoc(raw)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"level_id":"candidate-one"`) {
		t.Fatal("production ParsePlanDoc discarded authored level_id")
	}
}

func identityCloseForTest(l DetectedLevel) *int64 {
	f := reflect.ValueOf(l).FieldByName("FormedCloseMs")
	if !f.IsValid() || f.IsNil() {
		return nil
	}
	return f.Interface().(*int64)
}

func TestIdentityE5OpeningRangeKeepsTradingFields(t *testing.T) {
	open := time.Date(2026, 9, 10, 8, 30, 0, 0, CTLocation())
	now := open.Add(6 * time.Minute)
	var bars []market.Kline
	for i := 0; i < 5; i++ {
		start := open.Add(time.Duration(i) * time.Minute)
		bars = append(bars, market.Kline{OpenTime: start.UnixMilli(), CloseTime: start.Add(time.Minute).UnixMilli(), Open: 100, High: 102 + float64(i), Low: 98, Close: 100, Volume: 10})
	}
	levels := OpeningRangeLevels(bars, DefaultSessionRegistry(), now)
	for _, l := range levels {
		if l.Kind != KindORH && l.Kind != KindORL {
			continue
		}
		close := identityCloseForTest(l)
		if close == nil || *close != open.Add(5*time.Minute).UnixMilli() {
			t.Fatalf("OR %s has no recorded actual 08:35 formation close: %v", l.Kind, close)
		}
		if l.FormedAtMs != 0 || l.TF != "" {
			t.Fatal("identity capture changed legacy wake/grading inputs")
		}
	}
	if len(levels) == 0 {
		t.Fatal("fixture did not reach the production OR emitter")
	}
}

func identityFixture(t *testing.T) PlanLevel {
	t.Helper()
	now := time.Date(2026, 9, 10, 8, 36, 0, 0, CTLocation())
	l := WithFormationClose(lineLevel(KindORH, 20000, "OR-H", "2026-09-10", true), now.Add(-time.Minute).UnixMilli(), 5, "window_close", now)
	CaptureIdentityContext([]DetectedLevel{}, "MNQ", "1m")
	l.IdentitySymbol, l.FormationTF = "MNQ", "1m"
	out := CandidateIdentity(l)
	if out.ID == nil {
		t.Fatal("fixture missing complete identity")
	}
	return out
}

func TestIdentityE2DistinctCloseAndMissingInputs(t *testing.T) {
	a := identityFixture(t)
	b := a
	later := *a.FormedCloseMs + 60000
	b.FormedCloseMs = &later
	b.ID, _ = levelidentity.ID(IdentityInputs(b))
	if b.ID == nil || *a.ID == *b.ID {
		t.Fatal("different formation closes aliased")
	}
	for _, field := range []string{"symbol", "kind", "lo", "hi", "origin_date", "tf", "formed_close_ms"} {
		in := IdentityInputs(a)
		switch field {
		case "symbol":
			in.Symbol = ""
		case "kind":
			in.Kind = ""
		case "lo":
			in.Lo = nil
		case "hi":
			in.Hi = nil
		case "origin_date":
			in.OriginDate = ""
		case "tf":
			in.TF = ""
		case "formed_close_ms":
			in.FormedCloseMs = nil
		}
		if id, reason := levelidentity.ID(in); id != nil || !strings.Contains(reason, field) {
			t.Fatalf("partial hash for missing %s: %v %s", field, id, reason)
		}
	}
}

func TestIdentityE3WarnOnlyAndE4EvaluatorUnchanged(t *testing.T) {
	l := identityFixture(t)
	unknown := "not-on-map"
	doc := &PlanDoc{Levels: []PlanLevel{{Price: 20005, Label: "other"}}, Scenarios: []PlanScenario{
		{ID: "S1", LevelID: l.ID, Trigger: "hold 20005", Condition: "hold", Direction: "long"},
		{ID: "S2"}, {ID: "S3", LevelID: &unknown},
	}}
	now := time.Date(2026, 9, 10, 8, 36, 0, 0, CTLocation()).UnixMilli()
	before := EvaluateScenario(doc.Scenarios[0], doc.Levels, nil, 20005, 100, 1, "2x5m", true, now)
	warnings := StampAuthoredIdentity(doc, []MapCandidate{{ID: l.ID, Identity: l}})
	after := EvaluateScenario(doc.Scenarios[0], doc.Levels, nil, 20005, 100, 1, "2x5m", true, now)
	if !reflect.DeepEqual(before, after) || after.Anchor != 20005 {
		t.Fatal("identity changed trading evaluator", before, after)
	}
	if warnings.Named != 1 || warnings.Unnamed != 1 || warnings.Unresolved != 1 || len(doc.Scenarios) != 3 {
		t.Fatalf("WARN did not preserve/count authoring: %+v", warnings)
	}
	r := warnings.Scenarios["S1"]
	if r.Level == nil || r.Level.Price != 20000 || !r.Disagreed || r.EvaluatorAnchor == nil || *r.EvaluatorAnchor != 20005 {
		t.Fatalf("identity lost authority for recording: %+v", r)
	}
}

func TestIdentityE5RoundNumbersNeverInventFormation(t *testing.T) {
	levels := RoundNumberLevels(20000, 100, 1)
	if len(levels) == 0 {
		t.Fatal("no round fixture")
	}
	CaptureIdentityContext(levels, "MNQ", "1m")
	for _, l := range levels {
		r := CandidateIdentity(l)
		if r.ID != nil || r.FormedCloseMs != nil {
			t.Fatal("round number invented identity")
		}
	}
}

func TestIdentityE7MapOnlyAddsIDColumn(t *testing.T) {
	l := identityFixture(t)
	cs := []MapCandidate{{ID: l.ID, Identity: l, Price: l.Price, Names: []string{"OR-H"}, Grade: "A"}}
	old := RenderMapBlock(cs, 20001)
	with := RenderIdentityMapBlock(cs, 20001)
	if strings.ReplaceAll(with, "  id="+*l.ID, "") != old || with == old {
		t.Fatalf("non-column prompt drift\nold=%s\nnew=%s", old, with)
	}
}

func TestIdentityE5PriorCalendarBucketAndIBClose(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, CTLocation())
	var prior []market.Kline
	start := now.AddDate(0, 0, -1)
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, CTLocation())
	for i := 0; i < 24*60; i++ {
		s := start.Add(time.Duration(i) * time.Minute)
		prior = append(prior, market.Kline{OpenTime: s.UnixMilli(), CloseTime: s.Add(time.Minute).UnixMilli() - 1, Open: 20000, High: 20005, Low: 19995, Close: 20001, Volume: 10})
	}
	levels := ExtractMultiDayLevels(prior, DefaultSessionRegistry(), now)
	checked := 0
	// m239: PDH/PDL/PDC are the prior CME session (17:00→16:00 CT). The slice's
	// 09-09 17:00–23:59 bars are in the CURRENT session, so the prior session's
	// last source close is the 16:59 bar, not the 23:59 bar.
	lastPriorSessionBar := prior[16*60+59]
	for _, l := range levels {
		switch l.Kind {
		case KindPDH, KindPDL, KindPDC:
			checked++
			if l.FormedCloseMs == nil || *l.FormedCloseMs != lastPriorSessionBar.CloseTime || l.FormedAtMs != 0 {
				t.Fatalf("prior session source close lost %+v", l)
			}
		}
	}
	if checked != 3 {
		t.Fatalf("did not exercise all prior lines: %d", checked)
	}
	open := time.Date(2026, 9, 10, 8, 30, 0, 0, CTLocation())
	var bars []market.Kline
	for i := 0; i < 60; i++ {
		s := open.Add(time.Duration(i) * time.Minute)
		bars = append(bars, market.Kline{OpenTime: s.UnixMilli(), CloseTime: s.Add(time.Minute).UnixMilli(), Open: 20000, High: 20005, Low: 19995, Close: 20000, Volume: 10})
	}
	checked = 0
	for _, l := range OpeningRangeLevels(bars, DefaultSessionRegistry(), now) {
		if l.Kind == KindIBH || l.Kind == KindIBL {
			checked++
			if l.FormedCloseMs == nil || *l.FormedCloseMs != open.Add(time.Hour).UnixMilli() {
				t.Fatalf("IB close: %+v", l)
			}
		}
	}
	if checked != 6 { // two bounds and four existing extensions
		t.Fatalf("IB fixture empty %d", checked)
	}
}

func TestIdentityE5VWAPAnchorAndIncompleteWindow(t *testing.T) {
	now := time.Date(2026, 9, 10, 18, 0, 0, 0, CTLocation())
	start := CMESessionDayStart(now)
	bars := []market.Kline{}
	for i := 0; i < 3; i++ {
		s := start.Add(time.Duration(i) * time.Minute)
		bars = append(bars, market.Kline{OpenTime: s.UnixMilli(), CloseTime: s.Add(time.Minute).UnixMilli(), High: 20005, Low: 19995, Close: 20000, Volume: 10})
	}
	levels := SessionVWAPLevels(bars, now)
	if len(levels) == 0 {
		t.Fatal("empty VWAP fixture")
	}
	for _, l := range levels {
		if l.FormedCloseMs == nil || *l.FormedCloseMs != start.UnixMilli() || l.FormedAtMs != 0 {
			t.Fatalf("VWAP anchor wrong %+v", l)
		}
	}
	for _, l := range SessionVWAPLevels(bars[1:], now) {
		if l.FormedCloseMs != nil {
			t.Fatal("missing source anchor was invented")
		}
	}
}

func TestIdentityDuplicateScenarioNamesStayAmbiguous(t *testing.T) {
	l := identityFixture(t)
	doc := &PlanDoc{IdentityLevels: []PlanLevel{l}, Scenarios: []PlanScenario{{ID: "S1", LevelID: l.ID}, {ID: "S2", LevelID: l.ID}}}
	if EpisodeScenarioByID(l.ID, doc) != nil {
		t.Fatal("picked a scenario arbitrarily")
	}
}

func TestIdentityMergedMembersUseRecordedMembership(t *testing.T) {
	now := time.Date(2026, 9, 10, 8, 36, 0, 0, CTLocation())
	closeMs := now.Add(-time.Minute).UnixMilli()
	a := DetectedLevel{Kind: KindORH, Price: 20000, Lo: 20000, Hi: 20000, Label: "OR-H", OriginDate: "2026-09-10", IdentitySymbol: "MNQ", FormationTF: "1m", FormedCloseMs: &closeMs}
	b := a
	b.Kind = KindPDH
	b.Price = 20001
	b.Lo = 20001
	b.Hi = 20001
	b.Label = "PDH"
	cs := BuildMapCandidates([]ScoredLevel{{DetectedLevel: a, Score: 2}, {DetectedLevel: b, Score: 1}}, 20000, 10, MapCandidateOpts{})
	if len(cs) != 1 || cs[0].ID == nil {
		t.Fatal("fixture did not merge")
	}
	doc := &PlanDoc{Scenarios: []PlanScenario{{ID: "S1", LevelID: cs[0].ID}}}
	StampAuthoredIdentity(doc, cs)
	if got := EpisodeLevelID(b, doc); got == nil || *got != *cs[0].ID {
		t.Fatal("known merged member lost the named primary ID")
	}
	b.FormedCloseMs = nil
	if EpisodeLevelID(b, doc) != nil {
		t.Fatal("missing formation was guessed by price")
	}
}
