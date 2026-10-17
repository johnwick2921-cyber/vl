package trader

import (
	"context"
	"encoding/json"
	"math"
	"nofx/kernel"
	"nofx/market"
	"nofx/researchsnapshot"
	"nofx/store"
	"path/filepath"
	"testing"
	"time"
)

func TestStageAAuthoringAttemptRepairProductionPath(t *testing.T) {
	at := plannerTestTrader(t)
	a, err := researchsnapshot.Open(filepath.Join(t.TempDir(), "research.db"), "test-stage-a")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	r := researchsnapshot.NewRecorder(a, 32, func(string) {})
	researchsnapshot.Install(r)
	defer func() { researchsnapshot.Install(nil); r.Close() }()
	now := time.Date(2026, 9, 8, 1, 0, 0, 0, time.UTC)
	start := now
	trace := &researchsnapshot.PlanTrace{SnapshotID: "read-repair", Model: "fixture", ConfigVersion: "config-fixture", Clock: func() time.Time { return now }}
	planID := at.store.Plan().ResolvePlanID("2026-09-07", "ASIA", at.id)
	_, err = at.store.Plan().AppendPlan(&store.PlanDB{PlanID: planID, StrategyID: at.id, TradeDate: "2026-09-07", Session: "ASIA", Doc: validTraderPlanJSON})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	version, lc, err := at.runPlannerReadCoreObserved(func() time.Time { return now }, trace, "ASIA", "2026-09-07", "owner_reset", "fixture", "hash", "", "config-fixture", "", "original prompt", kernel.PlanFacts{}, nil, nil, nil, true, func(prompt string) (string, error) {
		calls++
		if calls == 1 {
			now = now.Add(683700 * time.Millisecond)
			return "{invalid-json}", nil
		}
		now = now.Add(423300 * time.Millisecond)
		return validTraderPlanJSON, nil
	})
	if err != nil || version != 2 || lc != "active" || calls != 2 {
		t.Fatalf("production retry result version=%d lifecycle=%s calls=%d err=%v", version, lc, calls, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = r.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := a.Export(ctx, start.UnixMilli(), now.UnixMilli()+1)
	if err != nil {
		t.Fatal(err)
	}
	var bundle researchsnapshot.Bundle
	if err = json.Unmarshal(raw, &bundle); err != nil {
		t.Fatal(err)
	}
	attempts := map[int]map[string]any{}
	published := false
	for _, f := range bundle.Objects["plan"] {
		var fields map[string]any
		_ = json.Unmarshal(f.Fields, &fields)
		if f.Event != nil && *f.Event == "attempt_verdict" {
			attempts[int(fields["attempt"].(float64))] = fields
		}
		if f.Event != nil && *f.Event == "published" {
			published = fields["plan_version"] == float64(2) && fields["accepted_output"] != nil && fields["normalization"] != nil
		}
	}
	if len(attempts) != 2 {
		t.Fatalf("lost attempts: n=%d", len(attempts))
	}
	if attempts[1]["duration_ms"] != float64(683700) || attempts[2]["duration_ms"] != float64(423300) {
		t.Fatalf("audit duration sequence lost: first=%v repair=%v", attempts[1]["duration_ms"], attempts[2]["duration_ms"])
	}
	if attempts[1]["rejection_reason"] == nil || attempts[2]["attempt_mode"] != "repair" || !published {
		t.Fatalf("missing reject/repair/publication linkage: first=%v mode=%v published=%v", attempts[1]["rejection_reason"], attempts[2]["attempt_mode"], published)
	}
}

func TestStageACandidateProductionAssembly(t *testing.T) {
	at := plannerTestTrader(t)
	at.config.NinjaTraderSymbol = "MNQ"
	a, err := researchsnapshot.Open(filepath.Join(t.TempDir(), "research.db"), "assembly-fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	r := researchsnapshot.NewRecorder(a, 32, func(string) {})
	researchsnapshot.Install(r)
	defer func() { researchsnapshot.Install(nil); r.Close() }()
	// The current assembly boundary owns its clock; these old completed bars
	// deliberately force recording of unknown/old source history without a trade.
	base := time.Date(2026, 9, 7, 18, 0, 0, 0, time.UTC)
	bars := make([]market.Kline, 300)
	for i := range bars {
		stamp := base.Add(time.Duration(i) * time.Minute)
		px := 30000 + float64(i%20)
		bars[i] = market.Kline{OpenTime: stamp.UnixMilli(), CloseTime: stamp.Add(time.Minute).UnixMilli(), Open: px, High: px + 3, Low: px - 3, Close: px + 1, Volume: 100}
	}
	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return bars }
	defer func() { market.FuturesBarsProvider = old }()
	in := at.assemblePlannerInputWithCtx("ASIA", "2026-09-07", "", nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = r.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	b, err := a.Export(ctx, 0, math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	var bundle researchsnapshot.Bundle
	_ = json.Unmarshal(b, &bundle)
	if in.ResearchSnapshotID == "" || len(bundle.Objects["candidate"]) == 0 {
		t.Fatalf("production assembly lost candidate hook: id=%q counts=%v", in.ResearchSnapshotID, bundle.Manifest.Counts)
	}
	for _, f := range bundle.Objects["candidate"] {
		if f.SnapshotID == nil || *f.SnapshotID != in.ResearchSnapshotID {
			t.Fatal("candidate/read link lost")
		}
	}
}

func TestStageAPermissionAndOutcomeProductionPaths(t *testing.T) {
	at := plannerTestTrader(t)
	at.config.NinjaTraderSymbol = "MNQ"
	a, err := researchsnapshot.Open(filepath.Join(t.TempDir(), "research.db"), "permission-fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	r := researchsnapshot.NewRecorder(a, 32, func(string) {})
	researchsnapshot.Install(r)
	defer func() { researchsnapshot.Install(nil); r.Close() }()
	now := time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC)
	plan := &kernel.ActivePlan{PlanID: "2026-09-08:NY:fixture", Session: "NY", Version: 2, BirthMs: now.Add(-time.Hour).UnixMilli(), Doc: kernel.PlanDoc{Levels: []kernel.PlanLevel{{Price: 30000, Label: "ONH", Grade: "A"}}, Scenarios: []kernel.PlanScenario{{ID: "S1", Direction: "short", Condition: "reject", Trigger: "reject 30000", Confirm: &kernel.PlanConfirm{Rule: "1x5m_close", RefPrice: 30000, Side: "below"}}}}}
	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline {
		return barsClosingAboveSince(now.Add(-25*time.Minute), 30000, 20)
	}
	kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{ActivePlan: func(string) *kernel.ActivePlan { return plan }})
	defer func() {
		market.FuturesBarsProvider = old
		kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{})
	}()
	at.recordScenarioStateAt(now)
	// Already-graded close still records its source fact before analytics skip it.
	at.recordClosedTradeAnalyticsAt(now, &store.TraderPosition{ID: 42, Symbol: "MNQ", EntryTime: now.Add(-time.Minute).UnixMilli(), ExitTime: now.UnixMilli(), EntryPrice: 30000, ExitPrice: 30001, Quantity: 1, AdherenceGrade: "A", RealizedPnL: 999})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = r.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	b, err := a.Export(ctx, 0, math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	var bundle researchsnapshot.Bundle
	_ = json.Unmarshal(b, &bundle)
	if len(bundle.Objects["scenario"]) == 0 || len(bundle.Objects["exec"]) != 1 {
		t.Fatalf("production record hooks absent: %v", bundle.Manifest.Counts)
	}
	var f map[string]any
	_ = json.Unmarshal(bundle.Objects["exec"][0].Fields, &f)
	if f["pnl_corrected"] != nil || f["outcome_exclusion"] != "UNRESOLVED: pnl_corrected is NULL" {
		t.Fatalf("raw P&L laundered into research: corrected=%v exclusion=%v", f["pnl_corrected"], f["outcome_exclusion"])
	}
}

type researchDiscardSink struct{}

func (researchDiscardSink) Save(context.Context, []researchsnapshot.Fact) error { return nil }

// Measures the actual per-read capture adapters separately from scoring, whose
// incremental cost is measured against the pre-wave source. Disk runs off-thread.
func BenchmarkStageAReadCaptureAdapters(b *testing.B) {
	r := researchsnapshot.NewRecorder(researchDiscardSink{}, 128, func(string) {})
	researchsnapshot.Install(r)
	defer func() { researchsnapshot.Install(nil); r.Close() }()
	now := time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC)
	raw := []kernel.DetectedLevel{{Kind: kernel.KindPDH, Price: 30000, Lo: 30000, Hi: 30000, Label: "PDH"}}
	scored, _ := kernel.ScoreLevelsMinGradeFull(raw, 30001, 100, nil, 1, 1.5, "")
	in := kernel.PlannerInput{Now: now, Levels: scored, ResearchSnapshotID: "benchmark-read"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recordResearchCandidatesAt("benchmark-read", "MNQ", raw, scored, now, now)
		recordResearchInputAt("benchmark-read", in, "system", "fixture-model", now)
		trace := &researchsnapshot.PlanTrace{SnapshotID: "benchmark-read", Model: "fixture-model"}
		trace.BeginAt(1, "author", "prompt", now)
		trace.ReplyAt(validTraderPlanJSON, nil, now.Add(time.Second))
		trace.FinishAt(nil, now.Add(time.Second))
		trace.PublishedAt("fixture-plan", 1, validTraderPlanJSON, now.Add(time.Second))
	}
	b.StopTimer()
}
