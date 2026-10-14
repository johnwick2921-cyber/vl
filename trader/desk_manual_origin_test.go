package trader

import (
	"strings"
	"testing"
	"time"

	"vl/store"
)

// R22 PNL-TRUTH (owner ruling R1, canon 28) — the DAY line and the mentor
// daily-loss gate read store.BotRealizedPnL: a manual NT8 row is excluded and
// shown as its own line, while a bot row whose plan lineage is UNRESOLVABLE is
// still the bot's (the old private rule dropped it silently, by plan_id).
func TestDeskDayReadsTheOneBotRule(t *testing.T) {
	cfg := store.StrategyConfig{RiskControl: store.RiskControlConfig{DailyLossLimitUSD: 100}}
	at, st := resetTrader(t, cfg)
	now := dailyLossNow()
	seedClosedLoss(t, st, at.id, now, -50) // bot
	exit := now.Add(-3 * time.Minute)
	manual := -1127.5
	if err := st.GormDB().Create(&store.TraderPosition{TraderID: at.id, Account: "Sim101", Symbol: "MNQ", Side: "SHORT",
		Quantity: 20, EntryPrice: 31208.775, ExitPrice: 31236.9625, PnlCorrected: &manual, Status: "CLOSED",
		CloseReason: "manual", Source: "reconcile", PlanID: store.PlanUnresolvable, Origin: store.OriginManual,
		EntryTime: exit.Add(-2 * time.Minute).UnixMilli(), ExitTime: exit.UnixMilli()}).Error; err != nil {
		t.Fatal(err)
	}
	untagged := -20.0
	if err := st.GormDB().Create(&store.TraderPosition{TraderID: at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryPrice: 100, ExitPrice: 90, PnlCorrected: &untagged, Status: "CLOSED",
		CloseReason: "stop", Source: "reconcile", PlanID: store.PlanUnresolvable,
		EntryTime: exit.Add(-2 * time.Minute).UnixMilli(), ExitTime: exit.UnixMilli()}).Error; err != nil {
		t.Fatal(err)
	}

	realized, n, unresolved := at.deskRealizedToday(now)
	if realized != -70 || n != 2 || unresolved != 0 {
		t.Fatalf("deskRealizedToday = %+.2f over %d (%d unresolved), want −70.00 over 2 (manual −1127.50 excluded; the untagged bot row counted)",
			realized, n, unresolved)
	}
	line := at.deskDay(now).Text
	if !strings.Contains(line, "realized -70.00 USD on 2 trade(s)") || !strings.Contains(line, "Manual (excluded): 1 trade, −$1,127.50") {
		t.Fatalf("DAY line must show the bot figure and the manual line, got %q", line)
	}
	// The mentor daily-loss gate (limit 100) reads the same helper: −70 is inside.
	if refuse, why := at.mentorDailyLossGate(now); refuse {
		t.Fatalf("a −70 bot day against a $100 limit must allow (the manual −1127.50 is not the bot's), got %q", why)
	}
}
