package trader

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// P0-B (2026-08-18) — ASIA CLOCK. Two defects, two guarantees:
//  1. the designed 16:30 read must fire even though IsCMEOpen(now) is false
//     during the 16:00–17:00 CME maintenance break (it builds from STORED data);
//  2. one session instance maps to EXACTLY one plan chain across the midnight
//     trade-date roll — no second plan at 00:30 CT.

func asiaClockTrader(t *testing.T) (*AutoTrader, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "asia.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	enabled := true
	at := &AutoTrader{
		id: "t1", exchange: "ninjatrader", store: st,
		config: AutoTraderConfig{NinjaTraderSymbol: "MNQ", StrategyConfig: &store.StrategyConfig{
			DayPlan: &store.DayPlanConfig{PlanEnabled: true, Sessions: []store.DayPlanSessionOverride{
				{Session: "ASIA", Enable: &enabled},
			}},
		}},
	}
	at.mcpClient = &planClient{} // planner returns validTraderPlanJSON
	return at, st
}

func ctTime(t *testing.T, y int, mo time.Month, d, hh, mm int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(y, mo, d, hh, mm, 0, 0, loc)
}

// waitPlan polls the plan store for the first read's write (F6, LONDON-
// FORENSICS 2026-08-28: the first read is async so its 3-attempt planner call
// can never stall the executor loop 19 minutes again).
func waitPlan(t *testing.T, st *store.Store, tradeDate, session, traderID string) *store.PlanDB {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if row, _ := st.Plan().GetLatestPlanForTraderSession(tradeDate, session, traderID); row != nil {
			return row
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

func TestP0BAsiaReadFiresAt1630WhileMarketClosed(t *testing.T) {
	at, st := asiaClockTrader(t)
	prev := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		// Stored-data path: supply a realistic prior session so DailyRangeProxy
		// resolves (~100 pts) instead of the thin one-bar fallback that the
		// P0.2 target-reachability rule would trip over. rthTapeBars builds a
		// deterministic full-session tape anchored to the trader clock, so the
		// machine table never carries the London-tail ONH/LDN-H=15650 that
		// collided with the mock's "RN 15650" under P0.4-H (r40).
		return rthTapeBars(traderNow())
	}

	// Tuesday 16:30 CT — the CME maintenance break (open−30 read, owner
	// ruling 2026-08-31). IsCMEOpen(now) == false.
	now := ctTime(t, 2026, 8, 18, 16, 30)
	if kernel.IsCMEOpen(now) {
		t.Fatalf("fixture: 16:30 must be inside the maintenance break (IsCMEOpen=false)")
	}
	// Pin the trader clock to the scenario's read instant so the scheduled
	// read and the tape are coherent and deterministic (r40).
	pinTraderNow(t, now)

	at.maybeRunSessionReadsAt(now)
	defer drainReReads(t) // CTO M4: join the async re-read before the seam resets

	row := waitPlan(t, st, "2026-08-18", "ASIA", "t1")
	if row == nil {
		t.Fatalf("the 16:30 ASIA read must fire while the market is closed")
	}
	if row.Lifecycle != "active" || row.TriggerReason != "ASIA_scheduled_read" {
		t.Fatalf("plan row wrong: %+v", row)
	}
}

func TestP0BAsiaReadDoesNotFireOutsideItsWindow(t *testing.T) {
	at, st := asiaClockTrader(t)
	// 16:29 CT — before ReadCT 16:30. Nothing may fire.
	at.maybeRunSessionReadsAt(ctTime(t, 2026, 8, 18, 16, 29))
	defer drainReReads(t) // CTO M4: join the async re-read before the seam resets
	if row, _ := st.Plan().GetLatestPlanForTraderSession("2026-08-18", "ASIA", "t1"); row != nil {
		t.Fatalf("16:29 is outside the read window — no plan may be written, got %+v", row)
	}
	// Sunday 16:30 WITHOUT the weekly doc landed — the A2 sequencing gate defers.
	at.maybeRunSessionReadsAt(ctTime(t, 2026, 8, 23, 16, 30))
	time.Sleep(300 * time.Millisecond)
	if row, _ := st.Plan().GetLatestPlanForTraderSession("2026-08-23", "ASIA", "t1"); row != nil {
		t.Fatalf("Sunday 16:30 with no weekly doc must DEFER, got %+v", row)
	}
	// Land the weekly doc (governing week of Sunday 2026-08-23 → Monday 08-24).
	monday := kernel.WeekGoverningMonday(ctTime(t, 2026, 8, 23, 16, 30)).Format("2006-01-02")
	wj, _ := json.Marshal(kernel.WeeklyDoc{WeeklyLevels: []kernel.WeeklyLevel{{Name: "PWH", Px: 15650}, {Name: "PWL", Px: 15550}}, Narrative: "facts only"})
	if _, err := st.Plan().AppendPlan(&store.PlanDB{
		PlanID:     st.Plan().ResolvePlanID(monday, "WEEKLY", at.id),
		StrategyID: at.id, TradeDate: monday, Session: "WEEKLY",
		TriggerReason: "test_weekly", Lifecycle: "active", Doc: string(wj),
	}); err != nil {
		t.Fatalf("weekly seed: %v", err)
	}
	// Now the Sunday read fires (weekly 16:30 → ASIA follows, same-cycle retry).
	at.maybeRunSessionReadsAt(ctTime(t, 2026, 8, 23, 16, 31))
	if row := waitPlan(t, st, "2026-08-23", "ASIA", "t1"); row == nil {
		t.Fatalf("Sunday 17:00 is a live session open — the 16:30 Sunday read must fire after the weekly doc lands")
	}
}

func TestP0BMidnightRollWritesNoSecondPlan(t *testing.T) {
	at, st := asiaClockTrader(t)
	prev := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		now := time.Now().UnixMilli()
		return []market.Kline{{OpenTime: now - 600_000, High: 15610, Low: 15590, Close: 15600, CloseTime: now - 300_000}}
	}

	// The 08-18 ASIA instance (opens 17:00 Tue) already has its chain.
	if _, err := st.Plan().AppendPlan(&store.PlanDB{
		PlanID: store.MakePlanID("2026-08-18", "ASIA"), StrategyID: "t1",
		TradeDate: "2026-08-18", Session: "ASIA", Lifecycle: "active", Doc: "{}",
	}); err != nil {
		t.Fatal(err)
	}

	// Wednesday 00:30 CT — inside the SAME instance's tail. The chain date must
	// still resolve to 2026-08-18, so the dedupe sees the existing plan and NO
	// second plan is written.
	at.maybeRunSessionReadsAt(ctTime(t, 2026, 8, 19, 0, 30))
	defer drainReReads(t) // CTO M4: join the async re-read before the seam resets

	var n int64
	st.GormDB().Model(&store.PlanDB{}).Where("trade_date = ? AND session = ?", "2026-08-18", "ASIA").Count(&n)
	if n != 1 {
		t.Fatalf("the midnight roll must NOT write a second plan for the same instance; count=%d", n)
	}
	if row, _ := st.Plan().GetLatestPlanForTraderSession("2026-08-19", "ASIA", "t1"); row != nil {
		t.Fatalf("no plan may be keyed to the next date mid-instance, got %+v", row)
	}
}
