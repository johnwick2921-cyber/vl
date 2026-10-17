package trader

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/market"
	ntwire "nofx/provider/ninjatrader"
	"nofx/store"
	"nofx/telemetry"
	ntTrader "nofx/trader/ninjatrader"
)

// SHADOW DEMOTION (0C, owner ruling 2026-08-31) — gate-level tests.
// fvg_entry + breakout_retest are SHADOW: authorable, validatable, E8-scored —
// but NO order may ever reach the wire.

// shadowEnableTestSession stores a whole-day TEST session in the persisted
// registry so the arm manager runs deterministically regardless of the wall
// clock (the manager reads the stored registry — H7 seam).
func shadowEnableTestSession(t *testing.T, st *store.Store) string {
	t.Helper()
	reg := kernel.SessionRegistry{Sessions: []kernel.SessionDef{
		{Name: "TEST", WindowStartCT: "00:00", WindowEndCT: "23:59", ReadCT: "00:00", FlatCT: "23:59", Enabled: true},
	}}
	blob, _ := json.Marshal(reg)
	if err := st.SetSystemConfig(kernel.SessionRegistryConfigKey, string(blob)); err != nil {
		t.Fatal(err)
	}
	return "TEST"
}

func shadowPlanAt(t *testing.T, at *AutoTrader, st *store.Store, doc string) string {
	t.Helper()
	pid := shadowPlanAtTime(t, at, st, doc, time.Now())
	installActivePlanProvider(at, st) // Existing callers retain their live provider clock.
	return pid
}

func shadowPlanAtTime(t *testing.T, at *AutoTrader, st *store.Store, doc string, now time.Time) string {
	t.Helper()
	sessName := shadowEnableTestSession(t, st)
	// The provider's sessionRunnable gate requires the strategy to declare the
	// session (registry Enabled=true alone is not enough).
	cfg := at.config.StrategyConfig
	cfg.DayPlan.SessionsEnabled = []string{sessName}
	td, _ := kernel.PlanChainTradeDate(&kernel.SessionDef{Name: sessName, WindowStartCT: "00:00", WindowEndCT: "23:59"}, now)
	pid := store.MakePlanIDForTrader(at.id, td, sessName)
	if _, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: td, Session: sessName, StrategyID: at.id, Lifecycle: "active", Doc: doc, CreatedAt: now.Add(-30 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	installActivePlanProviderAt(at, st, func() time.Time { return now })
	return pid
}

func shadowBarsNear(entry float64) []market.Kline {
	return shadowBarsNearAt(entry, time.Now())
}

func shadowBarsNearAt(entry float64, now time.Time) []market.Kline {
	base := now.Add(-80 * time.Minute).Truncate(time.Minute).UnixMilli()
	out := make([]market.Kline, 0, 80)
	for i := 0; i < 80; i++ {
		cl := entry - 2 + float64(i)*0.05 // drifts up to entry over the tape
		o := base + int64(i)*60_000
		out = append(out, market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: cl, High: cl + 0.5, Low: cl - 0.5, Close: cl})
	}
	return out
}

func shadowWireHarness(t *testing.T, cfg store.StrategyConfig) (*AutoTrader, *store.Store, chan ntwire.SignalPayload, chan ntwire.CancelOrderPayload) {
	t.Helper()
	return shadowWireHarnessAt(t, cfg, time.Now())
}

func shadowWireHarnessAt(t *testing.T, cfg store.StrategyConfig, now time.Time) (*AutoTrader, *store.Store, chan ntwire.SignalPayload, chan ntwire.CancelOrderPayload) {
	t.Helper()
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })

	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	waitAddonRegistered(t, s) // CTO M7: the producer must not race the accept
	t.Cleanup(func() { _ = conn.Close() })

	sigs := make(chan ntwire.SignalPayload, 8)
	cancels := make(chan ntwire.CancelOrderPayload, 8)
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			switch env.Type {
			case ntwire.FrameSignal:
				var p ntwire.SignalPayload
				if json.Unmarshal(env.Payload, &p) == nil {
					sigs <- p
				}
			case ntwire.FrameCancelOrder:
				var p ntwire.CancelOrderPayload
				if json.Unmarshal(env.Payload, &p) == nil {
					cancels <- p
				}
			}
		}
	}()

	st, err := store.New(filepath.Join(t.TempDir(), "shadow.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	tr := ntTrader.NewTCPTrader(s, "MNQ", "Sim101")

	// THE BOOK THE ONE-CONTRACT GUARD READS (2026-09-06). A real AddOn emits an
	// order_snapshot every N seconds whether or not the book has anything in
	// it, and the account guard refuses on an ABSENT book by design — an
	// unverifiable book is not an empty book. Without this seed the harness
	// represents a dark AddOn, not a flat account, and every placement test
	// here would be asserting the wrong thing.
	if snaps := s.OrderSnapshots(); snaps != nil {
		snaps.PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	}

	at := &AutoTrader{id: "trader-1", exchange: "ninjatrader", store: st, trader: tr}
	at.config.StrategyConfig = &cfg
	at.mcpClient = &fakeDecisionClient{}
	return at, st, sigs, cancels
}

// 7.1 + 7.2 — a shadowed condition's scenario AUTHORS (ledger row in the inert
// shadowed state) and VALIDATES (ArmSpecValid untouched); placement refusal
// increments the counter.
func TestShadowDemotionAuthorsInertRow(t *testing.T) {
	before := telemetry.ShadowedArmRefusalCount()
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	cfg.RiskControl.MinRiskRewardRatio = 2 // R1 (2026-09-03): the arm floor is the Studio value; this fixture arms at R:R 2.0
	at, st := resetTrader(t, cfg)
	pid := shadowPlanAt(t, at, st, armedDoc()) // fvg_entry

	at.maybeManageArmedOrdersAt(nil, armTestClock(t, at))

	rows, err := st.ArmedOrders().ListForPlan(pid)
	if err != nil || len(rows) != 1 {
		t.Fatalf("shadowed arm must still AUTHOR a ledger row, got %d (err=%v)", len(rows), err)
	}
	r := rows[0]
	if r.State != "shadowed" || r.StateReason != "condition_shadowed" {
		t.Fatalf("shadowed row must be state=shadowed reason=condition_shadowed, got %+v", r)
	}
	if telemetry.ShadowedArmRefusalCount() != before+1 {
		t.Fatalf("arms_refused_shadowed must increment, before=%d after=%d", before, telemetry.ShadowedArmRefusalCount())
	}
	// The validator is untouched (7.1) — an fvg_entry arm still validates.
	var d kernel.PlanDoc
	if err := json.Unmarshal([]byte(armedDoc()), &d); err != nil {
		t.Fatal(err)
	}
	if err := kernel.ArmSpecValid(d.Scenarios[0]); err != nil {
		t.Fatalf("fvg_entry arm must still validate (validator read-only this wave): %v", err)
	}
}

// 7.5 (trader side) — E8 writes a COMPLETE counterfactual row for the shadowed
// setup, flagged is_counterfactual, with the condition + authored prices.
func TestShadowDemotionE8WritesCounterfactual(t *testing.T) {
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	cfg.RiskControl.MinRiskRewardRatio = 2 // R1 (2026-09-03): the arm floor is the Studio value; this fixture arms at R:R 2.0
	at, st := resetTrader(t, cfg)
	doc := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long", Conviction: "low", FlipCondition: "n/a"},
		Levels: []kernel.PlanLevel{{Price: 100, Label: "PDH", Grade: "A", Instruction: "fade"}},
		Scenarios: []kernel.PlanScenario{{ID: "S1", Trigger: "t", Condition: "fvg_entry", Direction: "long",
			TargetChain: []float64{110}, Invalid: "i", Quality: "B",
			Confirm: &kernel.PlanConfirm{Rule: "touch", RefPrice: 100, Side: "above"},
			Fvg:     &kernel.PlanFvgEntry{Lo: 98, Hi: 102, CE: 100, EntryMode: "ce", DisplacementATR: 1.5, OriginLevel: "ONH", Direction: "long"},
			Arm:     &kernel.PlanArmSpec{Enabled: true, Entry: 100, Stop: 95, Target: 110}},
		},
		NoTrade: []string{}, DeathCondition: "n/a",
	}
	blob, _ := json.Marshal(doc)
	shadowPlanAt(t, at, st, string(blob)) // fvg_entry, entry 100
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return shadowBarsNear(100) }
	t.Cleanup(func() { market.FuturesBarsProvider = prev })

	at.maybeManageArmedOrdersAt(nil, armTestClock(t, at))

	var n int64
	if err := st.DB().QueryRow("SELECT COUNT(*) FROM ab_confirm_log WHERE condition='fvg_entry' AND is_counterfactual=1").Scan(&n); err != nil || n == 0 {
		t.Fatalf("E8 counterfactual rows = %d err=%v, want ≥1 for the shadowed fvg setup", n, err)
	}
	var incomplete int64
	if err := st.DB().QueryRow("SELECT COUNT(*) FROM ab_confirm_log WHERE condition='fvg_entry' AND (stop_px=0 OR target_px=0 OR rr=0 OR net_pnl IS NULL)").Scan(&incomplete); err != nil || incomplete != 0 {
		t.Fatalf("counterfactual rows must carry the complete trade (stop/target/RR/net), incomplete=%d err=%v", incomplete, err)
	}
}

// 7.3 — NO order frame reaches the wire for a shadowed condition, asserted on
// the LOOPBACK WIRE (not internal state).
func TestShadowDemotionNoWireFrameOnLoopback(t *testing.T) {
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	cfg.RiskControl.MinRiskRewardRatio = 2 // R1 (2026-09-03): the arm floor is the Studio value; this fixture arms at R:R 2.0
	at, st, sigs, _ := shadowWireHarness(t, cfg)
	pid := shadowPlanAt(t, at, st, armedDoc()) // fvg_entry, entry 100
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return shadowBarsNear(100) }
	t.Cleanup(func() { market.FuturesBarsProvider = prev })

	at.maybeManageArmedOrdersAt(nil, armTestClock(t, at))

	select {
	case s := <-sigs:
		t.Fatalf("SHADOWED condition placed an order on the wire: %+v", s)
	case <-time.After(400 * time.Millisecond):
		// expected silence — the choke point held.
	}
	rows, _ := st.ArmedOrders().ListForPlan(pid)
	if len(rows) != 1 || rows[0].State != "shadowed" {
		t.Fatalf("expected exactly one inert shadowed row, got %+v", rows)
	}
}

// 7.4 — a live condition's arm places normally (regression pin, same wire).
func TestLiveConditionPlacesOnLoopback(t *testing.T) {
	// One fixture clock: 10:00 CT, outside lunch; TEST session is explicit.
	now := time.Date(2026, time.September, 11, 15, 0, 0, 0, time.UTC)
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	// ONE SETUP (dispatch 102, 2026-09-11): this fixture exercises the WIDE book
	// (a non-reject play / no map); the switch OFF restores it byte-identically (E2).
	oneSetupOff(&cfg)
	structuralTestPolicy(&cfg, .5)
	cfg.RiskControl.MinRiskRewardRatio = 2 // R1 (2026-09-03): the arm floor is the Studio value; this fixture arms at R:R 2.0
	at, st, sigs, _ := shadowWireHarnessAt(t, cfg, now)
	live := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long", Conviction: "low", FlipCondition: "n/a"},
		Levels: []kernel.PlanLevel{{Price: 100, Label: "PDH", Grade: "A", Instruction: "fade"}},
		Scenarios: []kernel.PlanScenario{{ID: "S1", Trigger: "t", Condition: "reject", Direction: "long",
			TargetChain: []float64{110}, Invalid: "i", Quality: "B",
			Confirm: &kernel.PlanConfirm{Rule: "touch", RefPrice: 100, Side: "above"},
			Arm:     &kernel.PlanArmSpec{Enabled: true, Entry: 100, Stop: 95, Target: 110}},
		},
		NoTrade: []string{}, DeathCondition: "n/a",
	}
	structuralTestMap(&live, structuralTestZone{100, 95.5, 100, "PDH"}, structuralTestZone{110, 110, 111, "target"})
	blob, _ := json.Marshal(live)
	shadowPlanAtTime(t, at, st, string(blob), now)
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return shadowBarsNearAt(100, now) }
	t.Cleanup(func() { market.FuturesBarsProvider = prev })

	at.maybeManageArmedOrdersAt(nil, now)

	select {
	case s := <-sigs:
		if s.SignalID == "" {
			t.Fatalf("live arm placed an empty signal: %+v", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("live condition did NOT place — regression in the arm seam")
	}
}

// 7.6 — a resting order for a shadowed condition (authored before this wave)
// is cancelled on the first cycle, with its signal id quoted.
func TestShadowedRestingOrderCancelledAtBoot(t *testing.T) {
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	cfg.RiskControl.MinRiskRewardRatio = 2 // R1 (2026-09-03): the arm floor is the Studio value; this fixture arms at R:R 2.0
	at, st, _, cancels := shadowWireHarness(t, cfg)
	now := time.Now()
	sessName := shadowEnableTestSession(t, st)
	cfg.DayPlan.SessionsEnabled = []string{sessName}
	td, _ := kernel.PlanChainTradeDate(&kernel.SessionDef{Name: sessName, WindowStartCT: "00:00", WindowEndCT: "23:59"}, now)
	pid := store.MakePlanIDForTrader(at.id, td, sessName)
	if _, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: td, Session: sessName, StrategyID: at.id, Lifecycle: "active", Doc: armedDoc(), CreatedAt: now.Add(-30 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	// Pre-wave authoring: a RESTING order for the shadowed fvg scenario.
	if err := st.ArmedOrders().UpsertArm(&store.ArmedOrderDB{TraderID: at.id, PlanID: pid, Version: 1, Session: sessName, Scenario: "S1", Side: "long", EntryPx: 100, StopPx: 95, TargetPx: 110, State: "working", SignalID: "sig-pre-wave"}); err != nil {
		t.Fatal(err)
	}
	installActivePlanProvider(at, st)
	at.maybeManageArmedOrdersAt(nil, armTestClock(t, at))

	select {
	case c := <-cancels:
		if c.SignalID != "sig-pre-wave" {
			t.Fatalf("boot sweep must cancel the resting shadowed order's signal, got %q", c.SignalID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no cancel_order for the resting shadowed order")
	}
	rows, _ := st.ArmedOrders().ListForPlan(pid)
	for _, r := range rows {
		if r.SignalID == "sig-pre-wave" && r.State != "shadowed" {
			t.Fatalf("resting order must be swept to shadowed, got %+v", r)
		}
	}
}

// 7.7 — flipping the condition to live in CONFIG makes it placeable again
// (proves the map is config-driven, not hardcoded).
func TestConfigFlipToLiveAllowsArming(t *testing.T) {
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true,
		ConditionStatus: map[string]string{"fvg_entry": "live"}}}
	// ONE SETUP (dispatch 102, 2026-09-11): this fixture exercises the WIDE book
	// (a non-reject play / no map); the switch OFF restores it byte-identically (E2).
	oneSetupOff(&cfg)
	cfg.RiskControl.MinRiskRewardRatio = 2 // R1 (2026-09-03): the arm floor is the Studio value; this fixture arms at R:R 2.0
	at, st := resetTrader(t, cfg)
	if at.conditionShadowedFor("fvg_entry", "NY") {
		t.Fatal("config live must resolve live")
	}
	shadowPlanAt(t, at, st, armedDoc())
	at.maybeManageArmedOrdersAt(nil, armTestClock(t, at))
	// The live-configured condition must arm normally (state armed, not shadowed).
	allRows, err2 := st.ArmedOrders().ListNonTerminal(at.id)
	if err2 != nil || len(allRows) != 1 || allRows[0].State != "armed" {
		t.Fatalf("live-configured condition must arm normally, got %+v (err=%v)", allRows, err2)
	}
}
