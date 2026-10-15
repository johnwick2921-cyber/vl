package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── r22 RISK-GATES — VERIFICATION FIRST: the findings, reproduced RED ────────
//
// These two tests use ONLY symbols that exist at the release base (9accae11f),
// so they compile there and FAIL there — the [A] reproduction of RISK-PNL P1-3
// at the production call sites — and pass after the fix:
//
//   - the 60-second monitor beat (at.monitorTick) never acted on a breached
//     daily loss limit: no latch, no withdraw of the resting entry, no flatten,
//     and the agent door still admitted a new entry;
//   - the mentor's placement never evaluated max daily trades.

func rbSeedClosed(t *testing.T, st *store.Store, traderID string, exit time.Time, pnl float64) {
	t.Helper()
	p := &store.TraderPosition{TraderID: traderID, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryQuantity: 1, EntryPrice: 100, ExitPrice: 100 + pnl/2, RealizedPnL: pnl, PnlCorrected: &pnl,
		Status: "CLOSED", CloseReason: "sync",
		EntryTime: exit.Add(-5 * time.Minute).UnixMilli(), ExitTime: exit.UnixMilli(),
		CreatedAt: exit.UnixMilli(), UpdatedAt: exit.UnixMilli()}
	if err := st.GormDB().Create(p).Error; err != nil {
		t.Fatal(err)
	}
}

// P1-3 (a)+(b)+(c): a bot loss of -150 against an ENFORCED $100 limit, a
// resting planner entry at the broker and an open bot position. The beat must
// withdraw the entry, flatten the position, and the agent door must refuse.
func TestBaseRedMonitorBeatActsOnABreachedDailyLoss(t *testing.T) {
	f := withdrawFixture(t, "rb-breach", func(c *store.StrategyConfig) {
		on := true // both switches saved ON — the owner turned the gate on
		c.RiskControl.GuardrailsEnabled, c.RiskControl.DailyLossEnabled = &on, &on
		c.RiskControl.DailyLossLimitUSD = 100
	})
	row, sid := f.placeResting(t)
	t1 := f.now.Add(time.Minute)
	rbSeedClosed(t, f.st, f.at.id, f.now.Add(-30*time.Minute), -150)
	open := &store.TraderPosition{TraderID: f.at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryQuantity: 1, EntryPrice: 100, Status: "OPEN",
		EntryTime: f.now.Add(-10 * time.Minute).UnixMilli(), CreatedAt: f.now.UnixMilli(), UpdatedAt: f.now.UnixMilli()}
	if err := f.st.GormDB().Create(open).Error; err != nil {
		t.Fatal(err)
	}
	f.w.srv.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "long", Quantity: 1, AvgPrice: 100}})
	f.at.peakPnLCache = map[string]float64{}
	f.setLiveBook(t1, wdEntryOrder(sid))
	t.Cleanup(func() { kernel.ClearDailyForceFlat(f.at.id) })

	f.at.monitorTick(t1)
	f.w.flush(t)

	if n := f.w.count(ntwire.FrameCancelOrder, sid); n == 0 {
		t.Fatalf("BREACH NOT ACTED ON: the 60s beat sent no cancel for the resting entry %s after a -150 loss against an enforced $100 limit", shortID(sid))
	}
	if got := f.rowByID(t, row.ID); !strings.Contains(got.StateReason, "daily_force_flat") {
		t.Fatalf("the resting entry must be withdrawn for daily_force_flat, got state=%q reason=%q", got.State, got.StateReason)
	}
	closes := 0
	for _, fr := range f.w.all() {
		if fr.Type == ntwire.FrameClosePosition {
			closes++
		}
	}
	if closes == 0 {
		t.Fatal("NO FLATTEN: the bot's open position was not flattened after the daily loss limit was hit")
	}
	if r, refused := f.at.AdmitManualEntryBracketAt("MNQ", "open_long", 90, 130, t1); !refused || !strings.Contains(r, "daily_loss") {
		t.Fatalf("a new entry after the breach must be refused by the daily loss limit, got refused=%v %q", refused, r)
	}
}

// R5: the owner's max_daily_trades switch, ON with max 2, two entries today —
// the mentor's placement must be refused (it was never evaluated).
func TestBaseRedMentorPlacementObeysMaxDailyTrades(t *testing.T) {
	on := true
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{
		MentorMode: true, GuardrailsEnabled: &on, MaxDailyTradesEnabled: &on, MaxDailyTrades: 2}})
	at.id = "rb-mentor-max"
	t.Cleanup(func() { kernel.ClearDailyForceFlat(at.id) })
	wireMentorPlacementSeams(t, at)
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, kernel.CTLocation())
	setMentorNowSource(func() time.Time { return now })
	t.Cleanup(func() { setMentorNowSource(nil) })
	var placed int
	mentorPlaceRecorderForTest = func(mentor.Intent, int) { placed++ }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })
	rbSeedClosed(t, st, at.id, now.Add(-40*time.Minute), 20)
	rbSeedClosed(t, st, at.id, now.Add(-20*time.Minute), 20)

	at.mentorPlaceIntent(mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12},
		mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}, 1000, 1100)
	if placed != 0 {
		t.Fatalf("MAX DAILY TRADES IGNORED ON THE MENTOR PATH: 2 entries today against max 2 (switch ON), and the mentor placed anyway (placed=%d)", placed)
	}
}
