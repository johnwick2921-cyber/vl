package trader

import (
	"testing"
	"time"

	"vl/store"
)

// FIX-DONE-AFTER-WIN-NY-DAY (owner ruling 2026-10-09): the done-after-win "day"
// is the NY day (08:30 CT → next 08:30 CT), not the CME 17:00 session day. These
// pins drive the production call site (mentorDoneAfterWinGate) with the REAL
// production seams (at.mentorWireProductionSeams reads the store's closed-trade
// P&L through the boundary).

func doneAfterWinRig(t *testing.T, dayStart string) (*AutoTrader, *store.Store) {
	t.Helper()
	rc := store.RiskControlConfig{MentorMode: true} // MentorDoneAfterWin nil → ON
	if dayStart != "" {
		rc.MentorDoneAfterWinDayStart = dayStart
	}
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: rc})
	at.mentorWireProductionSeams() // the REAL day-read seams, not test injection
	t.Cleanup(func() {
		mentorDayNetSource = nil
		mentorClosedProfitSource = nil
		mentorClosedLossSource = nil
		mentorNowSource = nil
	})
	return at, st
}

// nyDayClock builds a CT instant in Oct 2026 (day 1..31).
func nyDayClock(d, h, m int) time.Time {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		loc = time.FixedZone("CDT", -5*60*60)
	}
	return time.Date(2026, 10, d, h, m, 0, 0, loc)
}

func seedClosedWin(t *testing.T, st *store.Store, traderID string, exitMs int64, pnl float64) {
	t.Helper()
	// Raw GORM insert: PositionStore.Create FORCES Status="OPEN", so a CLOSED
	// row must go straight through the DB handle (the same path
	// TestMentorProductionWiring uses).
	if err := st.GormDB().Create(&store.TraderPosition{
		TraderID: traderID, Account: "", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryPrice: 100, ExitPrice: 100 + pnl, RealizedPnL: pnl,
		PnlCorrected: fp(pnl), Status: "CLOSED", CloseReason: "sync", Source: "sync",
		EntryTime: exitMs - 60_000, ExitTime: exitMs, CreatedAt: exitMs, UpdatedAt: exitMs,
	}).Error; err != nil {
		t.Fatalf("seed closed win: %v", err)
	}
}

func pinDoneAfterWinClock(t *testing.T, d, h, m int) {
	t.Helper()
	fixed := nyDayClock(d, h, m)
	mentorNowSource = func() time.Time { return fixed }
}

// (a)+(b) the #641 shape: a win at 18:06 CT must no longer block the next NY
// open — refused at 02:00 (still the same NY day), allowed at 08:31 (the day
// rolled at 08:30).
func TestDoneAfterWinNYDayBoundary(t *testing.T) {
	at, st := doneAfterWinRig(t, "")
	seedClosedWin(t, st, at.id, nyDayClock(8, 18, 6).UnixMilli(), 55)

	pinDoneAfterWinClock(t, 9, 2, 0)
	if refuse, _ := at.mentorDoneAfterWinGate(); !refuse {
		t.Fatal("(b) 02:00 next day is still the win's NY day — must refuse")
	}

	pinDoneAfterWinClock(t, 9, 8, 31)
	if refuse, _ := at.mentorDoneAfterWinGate(); refuse {
		t.Fatal("(a) 08:31 next day is a NEW NY day — the 18:06 win must NOT refuse")
	}
}

// (c) a NY win at 10:00 blocks until 08:30 the next day: refused at 18:00 and at
// 08:29, allowed at 08:30.
func TestDoneAfterWinNYWinBlocksUntilNextOpen(t *testing.T) {
	at, st := doneAfterWinRig(t, "")
	seedClosedWin(t, st, at.id, nyDayClock(9, 10, 0).UnixMilli(), 40)

	pinDoneAfterWinClock(t, 9, 18, 0)
	if refuse, _ := at.mentorDoneAfterWinGate(); !refuse {
		t.Fatal("(c) 18:00 same NY day — must refuse")
	}
	pinDoneAfterWinClock(t, 10, 8, 29)
	if refuse, _ := at.mentorDoneAfterWinGate(); !refuse {
		t.Fatal("(c) 08:29 next day is still the win's NY day — must refuse")
	}
	pinDoneAfterWinClock(t, 10, 8, 30)
	if refuse, _ := at.mentorDoneAfterWinGate(); refuse {
		t.Fatal("(c) 08:30 next day is a NEW NY day — must allow")
	}
}

// (d) the knob "17:00" restores today's behaviour: the same 18:06 win still
// blocks at 08:31 next day (the CME session day rolls at 17:00, not 08:30).
// This proves (a) is not vacuous — the 08:30 boundary is what makes (a) pass.
func TestDoneAfterWinKnob1700RestoresCMEDay(t *testing.T) {
	at, st := doneAfterWinRig(t, "17:00")
	seedClosedWin(t, st, at.id, nyDayClock(8, 18, 6).UnixMilli(), 55)

	pinDoneAfterWinClock(t, 9, 8, 31)
	if refuse, _ := at.mentorDoneAfterWinGate(); !refuse {
		t.Fatal("(d) knob 17:00: the 18:06 win is still in the CME session day at 08:31 next day — must refuse")
	}
}

// (e) a NULL pnl_corrected closed row is UNRESOLVED → fail-closed (refuse).
func TestDoneAfterWinUnresolvedStillRefuses(t *testing.T) {
	at, st := doneAfterWinRig(t, "")
	if err := st.GormDB().Create(&store.TraderPosition{
		TraderID: at.id, Account: "", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryPrice: 100, ExitPrice: 155, RealizedPnL: 55,
		PnlCorrected: nil, Status: "CLOSED", CloseReason: "sync", Source: "sync",
		EntryTime: nyDayClock(8, 18, 6).UnixMilli() - 60_000,
		ExitTime:  nyDayClock(8, 18, 6).UnixMilli(),
		CreatedAt: nyDayClock(8, 18, 6).UnixMilli(),
		UpdatedAt: nyDayClock(8, 18, 6).UnixMilli(),
	}).Error; err != nil {
		t.Fatalf("seed unresolved: %v", err)
	}

	pinDoneAfterWinClock(t, 8, 18, 7)
	if refuse, why := at.mentorDoneAfterWinGate(); !refuse || why == "" {
		t.Fatalf("(e) an unresolved day must refuse fail-closed, got refuse=%v why=%q", refuse, why)
	}
}

// TestStopAfterLossKeepsCMEDay (FOLD-468, CTO M2): stop-after-loss must KEEP the
// CME 17:00 session day — a loss at 10:00 CT refuses at 16:30 (same CME day) and
// does NOT refuse at 17:30 (the CME day rolled), even though the NY-day (08:30)
// boundary would still count it. RED (named): wiring lossDayActivity to
// mentorDayStartAt(08:30) makes the 17:30 case refuse.
func TestStopAfterLossKeepsCMEDay(t *testing.T) {
	at, st := doneAfterWinRig(t, "") // NY-day knob is the DEFAULT 08:30; the loss gate must NOT follow it
	at.config.StrategyConfig.RiskControl.MentorStopAfterLoss = bp(true)

	// A mentor loss closed at 10:00 CT.
	if err := st.GormDB().Create(&store.TraderPosition{
		TraderID: at.id, Account: "", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryPrice: 100, ExitPrice: 60, RealizedPnL: -40,
		PnlCorrected: fp(-40), Status: "CLOSED", CloseReason: "sync", Source: "sync",
		EntryTime: nyDayClock(8, 10, 0).UnixMilli() - 60_000,
		ExitTime:  nyDayClock(8, 10, 0).UnixMilli(),
		CreatedAt: nyDayClock(8, 10, 0).UnixMilli(),
		UpdatedAt: nyDayClock(8, 10, 0).UnixMilli(),
	}).Error; err != nil {
		t.Fatalf("seed closed loss: %v", err)
	}

	// 16:30 same day: the CME day (17:00 → 17:00) still holds the 10:00 loss.
	pinDoneAfterWinClock(t, 8, 16, 30)
	if refuse, _ := at.mentorStopAfterLossGate(); !refuse {
		t.Fatal("16:30 same CME day: the 10:00 loss must still refuse")
	}

	// 17:30: the CME day rolled at 17:00 — the loss is yesterday's.
	pinDoneAfterWinClock(t, 8, 17, 30)
	if refuse, _ := at.mentorStopAfterLossGate(); refuse {
		t.Fatal("17:30 new CME day: the 10:00 loss must NOT refuse (boundary is 17:00, not 08:30)")
	}

	// Distinguishing assertion: the NY-day (08:30) boundary WOULD still count the
	// 10:00 loss at 17:30 (08:30 ≤ 10:00 < next 08:30), so a drift to the NY-day
	// boundary would flip the 17:30 case to refuse — this test catches that drift.
	nySince, ok := mentorDayStartAt(mentorDoneAfterWinDayStartDefault, nyDayClock(8, 17, 30))
	if !ok || nySince.After(nyDayClock(8, 10, 0)) {
		t.Fatalf("NY-day boundary at 17:30 = %v — the 10:00 loss would be excluded, so the distinguishing assertion is broken", nySince)
	}
}
