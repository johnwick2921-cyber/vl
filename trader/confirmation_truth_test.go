package trader

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"vl/kernel"
	"vl/market"
	"vl/store"
)

func confirmationTrader(t *testing.T) (*AutoTrader, *store.Store, *kernel.ActivePlan, *[]market.Kline, time.Time) {
	t.Helper()
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ConditionStatus: map[string]string{"fvg_entry": "live"}}, RiskControl: store.RiskControlConfig{MinRiskRewardRatio: 1.5}}
	// ONE SETUP (dispatch 102, 2026-09-11): this fixture exercises the WIDE book
	// (an fvg_entry play); the switch OFF restores it byte-identically (E2).
	oneSetupOff(&cfg)
	at, st := resetTrader(t, cfg)
	at.config.NinjaTraderSymbol = "MNQ"
	base := time.Date(2026, 9, 8, 10, 0, 0, 0, chicagoLoc())
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(armedDoc()), &doc); err != nil {
		t.Fatal(err)
	}
	doc.Scenarios[0].Confirm = &kernel.PlanConfirm{Rule: "1x5m_close", RefPrice: 100, Side: "above"}
	doc.Scenarios[0].Arm.WaitConfirm = true
	pid := store.MakePlanIDForTrader(at.id, "2026-09-08", "NY")
	blob, _ := json.Marshal(doc)
	version, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: "2026-09-08", Session: "NY", StrategyID: at.id, Lifecycle: "active", Doc: string(blob), CreatedAt: base.Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	plan := &kernel.ActivePlan{Doc: doc, PlanID: pid, Session: "NY", Version: version, BirthMs: base.Add(-time.Minute).UnixMilli()}
	var tape []market.Kline
	for i := 0; i < 5; i++ {
		o := base.Add(time.Duration(i) * time.Minute).UnixMilli()
		tape = append(tape, market.Kline{OpenTime: o, CloseTime: o + 59999, Open: 101, High: 102, Low: 99, Close: 101})
	}
	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return tape }
	kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{ActivePlan: func(string) *kernel.ActivePlan { return plan }})
	t.Cleanup(func() {
		market.FuturesBarsProvider = old
		kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{})
	})
	return at, st, plan, &tape, base
}

func TestConfirmationTruthArmAndDeclineCallSites(t *testing.T) {
	at, st, _, _, base := confirmationTrader(t)
	at.maybeManageArmedOrdersAt(nil, base.Add(time.Minute))
	rows, err := st.ArmedOrders().ListNonTerminal(at.id)
	if err != nil || len(rows) != 0 {
		t.Fatalf("forming 5m bucket must not authorize an arm: rows=%d err=%v", len(rows), err)
	}
	if at.declineHadFreshMetAt(base.Add(time.Minute)) {
		t.Fatal("decline counter must not count a forming confirm")
	}
	at.maybeManageArmedOrdersAt(nil, base.Add(5*time.Minute))
	rows, err = st.ArmedOrders().ListNonTerminal(at.id)
	if err != nil || len(rows) != 1 {
		t.Fatalf("same call site must authorize after close: rows=%d err=%v", len(rows), err)
	}
	if !at.declineHadFreshMetAt(base.Add(5 * time.Minute)) {
		t.Fatal("closed confirmation must reach the decline consumer")
	}
}

func TestConfirmationTruthArmSequenceCallSite(t *testing.T) {
	at, st, plan, tape, base := confirmationTrader(t)
	sc := &plan.Doc.Scenarios[0]
	sc.Confirm = &kernel.PlanConfirm{Rule: "touch", RefPrice: 100, Side: "below"}
	sc.Confirm2 = &kernel.PlanConfirm{Rule: "1x5m_close", RefPrice: 100, Side: "above"}
	for i := range *tape {
		(*tape)[i].Low = 100.5
	}
	o := base.Add(5 * time.Minute).UnixMilli()
	*tape = append(*tape, market.Kline{OpenTime: o, CloseTime: o + 59999, Open: 99, High: 100.5, Low: 98, Close: 99})
	at.maybeManageArmedOrdersAt(nil, base.Add(6*time.Minute))
	rows, _ := st.ArmedOrders().ListNonTerminal(at.id)
	if len(rows) != 0 {
		t.Fatal("arm must not authorize an out-of-order sequence")
	}
	if at.declineHadFreshMetAt(base.Add(6 * time.Minute)) {
		t.Fatal("decline consumer must judge the whole ordered scenario")
	}
}

func TestConfirmationTruthStoredVerdictAndDesk(t *testing.T) {
	at, st, plan, _, base := confirmationTrader(t)
	now := base.Add(time.Minute)
	at.recordScenarioStateAt(now)
	raw, err := st.GetSystemConfig(store.ScenarioMetaKey(at.id, plan.PlanID, plan.Version))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"closed":false`) || !strings.Contains(raw, `"met":false`) {
		t.Fatalf("stored versioned verdict must carry forming evidence: %s", raw)
	}
	found := false
	for _, l := range at.DeskStripAt(now).Lines {
		if l.Key == "confirmation" {
			found = true
			if !strings.Contains(l.Text, "NOT MET") || !strings.Contains(l.Text, "10:05") || l.AsOfMs != now.UnixMilli() {
				t.Fatalf("desk must read the stored dated verdict: %+v", l)
			}
		}
	}
	if !found {
		t.Fatal("desk is missing the recorded confirmation row")
	}
}
