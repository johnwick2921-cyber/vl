package trader

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// P7 — levels are market FACTS; the plan is an opinion about them. A no-trade
// decision (re-plans exhausted OR fail-closed read) must never erase the map:
// the NO-TRADE doc carries the current detector/scorer output, and when the
// detector genuinely has nothing, the doc SAYS so.

func noTradeTestTrader(t *testing.T, bars func(symbol, tf string, count int) []market.Kline) *AutoTrader {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Plan().Close(); _ = st.Close() })
	prev := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	market.FuturesBarsProvider = bars
	return &AutoTrader{
		id: "t1", exchange: "ninjatrader", store: st,
		config: AutoTraderConfig{NinjaTraderSymbol: "MNQ", StrategyConfig: &store.StrategyConfig{
			DayPlan: &store.DayPlanConfig{PlanEnabled: true},
		}},
	}
}

func oneBarFeed() func(string, string, int) []market.Kline {
	return func(symbol, tf string, count int) []market.Kline {
		now := time.Now().UnixMilli()
		return []market.Kline{{OpenTime: now - 600_000, High: 15610, Low: 15590, Close: 15600, CloseTime: now - 300_000}}
	}
}

func TestWriteNoTradePlanKeepsTheMap(t *testing.T) {
	at := noTradeTestTrader(t, oneBarFeed())
	at.writeNoTradePlan("ASIA", "2026-08-16", "re-plans exhausted (4/4)")

	row, err := at.store.Plan().GetLatestPlanForSession("2026-08-16", "ASIA")
	if err != nil || row == nil {
		t.Fatalf("plan row missing: %v", err)
	}
	if row.Lifecycle != "no_trade" {
		t.Fatalf("lifecycle = %q, want no_trade", row.Lifecycle)
	}
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(row.Doc), &doc); err != nil {
		t.Fatalf("doc unmarshal: %v", err)
	}
	if len(doc.Levels) == 0 {
		t.Fatalf("NO-TRADE plan must carry the detected level map; doc has no levels")
	}
	if len(doc.Scenarios) != 1 || doc.Scenarios[0].ID != "S0" {
		t.Fatalf("scenarios must stay the single no-trade placeholder, got %+v", doc.Scenarios)
	}
	if doc.Bias.Direction != "neutral" {
		t.Fatalf("bias must stay neutral")
	}
}

func TestWriteNoTradePlanSaysSoWhenDetectorDark(t *testing.T) {
	at := noTradeTestTrader(t, func(string, string, int) []market.Kline { return nil })
	at.writeNoTradePlan("ASIA", "2026-08-16", "re-plans exhausted (4/4)")

	row, _ := at.store.Plan().GetLatestPlanForSession("2026-08-16", "ASIA")
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(row.Doc), &doc); err != nil {
		t.Fatalf("doc unmarshal: %v", err)
	}
	if len(doc.Levels) != 0 {
		t.Fatalf("dark detector must yield no levels, got %d", len(doc.Levels))
	}
	joined := ""
	for _, nt := range doc.NoTrade {
		joined += nt + " "
	}
	if !strings.Contains(joined, "detector data unavailable") {
		t.Fatalf("empty levels must carry the explicit 'detector data unavailable' reason, no_trade=%v", doc.NoTrade)
	}
}

func TestFailClosedReadKeepsTheMap(t *testing.T) {
	at := noTradeTestTrader(t, oneBarFeed())
	ver, lc, err := at.runPlannerReadCore("NY", "2026-08-16", "deepseek-reasoner", "hashF", "", "",
		func() (string, error) { return "", errors.New("context deadline exceeded") })
	if err != nil {
		t.Fatalf("fail-closed must not error: %v", err)
	}
	if lc != "no_trade" || ver == 0 {
		t.Fatalf("expected a NO-TRADE plan, got lc=%q ver=%d", lc, ver)
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-08-16", "NY")
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(row.Doc), &doc); err != nil {
		t.Fatalf("doc unmarshal: %v", err)
	}
	if len(doc.Levels) == 0 {
		t.Fatalf("the fail-closed doc must still carry the map")
	}
	if len(doc.Scenarios) != 1 || doc.Scenarios[0].ID != "S0" {
		t.Fatalf("scenarios must stay empty-of-plays")
	}
}
