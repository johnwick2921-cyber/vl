package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

// f2PinClock pins the mentor clock seam to a fixed mid-session instant far from
// the 17:00 CT CME session boundary and returns it. mentorRebuildLimitsAtBoot
// reads mentorClockNow() (the seam) while the seed below uses the returned
// instant — the two must be the SAME instant. The old time.Now() seed made
// these tests wall-clock flaky: pos.EntryTime = now-10m lands in the PREVIOUS
// session (skipped by the session-day filter) whenever now is within 10 minutes
// after 17:00 CT.
func f2PinClock(t *testing.T) time.Time {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, kernel.CTLocation())
	setMentorNowSource(func() time.Time { return now })
	t.Cleanup(func() { setMentorNowSource(nil) })
	return now
}

// #13 — Limits (the G1 leg budget + G2 loss box) is in-memory only, so a
// restart wiped "two losses at one place = OFF" and the stopped-leg block.
// mentorRebuildLimitsAtBoot replays today's session-day FILLED mentor arms into
// RecordFill (row fallback) and today's CLOSED stop-outs into RealExit(wasStop),
// re-deriving the legs, the per-place loss counts and the stopped-leg block.
// MUTANT: drop the mentorRebuildLimitsAtBoot call at the end of mentorSeedAtStart
// (or make it a no-op) → the rebuilt Limits stay empty after a restart → RED.
func TestMentorRebuildLimitsAtBoot(t *testing.T) {
	at, st := f2Trader(t)
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	at.mentorEval.State.Levels = []mentor.Level{
		{Key: "old_extreme:100", Kind: mentor.KindOldExtreme, Price: 100},
		{Key: "key_level:97", Kind: mentor.KindKeyLevel, Price: 97},
	}

	now := f2PinClock(t)
	sig := "aa07e583-0000-0000-0000-000000000099"
	// A FILLED mentor PHL arm from today.
	arm := f2Arm(sig, "PHL", 2, 0, 90, 88, 99, nil)
	arm.UpdatedAt = now
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	// The same signal already stopped out today (CloseReasonStop).
	closed := &store.TraderPosition{
		TraderID: "tF2", ExchangeID: "NT8", ExchangeType: "ninjatrader",
		Symbol: "MNQ", Side: "LONG", EntryQuantity: 2, Quantity: 0,
		EntryPrice: 90, EntryOrderID: sig, EntryTime: now.Add(-10 * time.Minute).UnixMilli(),
		ExitPrice: 88, ExitTime: now.Add(-5 * time.Minute).UnixMilli(),
		CloseReason: store.CloseReasonStop, Status: "CLOSED", Source: "system",
	}
	// CreateOpenPosition keeps an explicit Status (Create() forces OPEN).
	if err := st.Position().CreateOpenPosition(closed); err != nil {
		t.Fatal(err)
	}

	at.mentorRebuildLimitsAtBoot()

	lim := &at.mentorEval.State.Limits
	if lim.Long == nil || lim.Long.Extreme != 100 || lim.Long.Entries != 1 {
		t.Fatalf("the filled PHL arm must rebuild the long leg at extreme 100, got %+v", lim.Long)
	}
	if !lim.Long.Stopped {
		t.Fatalf("the replayed stop-out must set Leg.Stopped, got %+v", lim.Long)
	}
	// The G2 loss box is registered (the row-fallback keys a non-ISB place on
	// the quarter-tick entry 90).
	foundLoss := false
	for _, p := range lim.Places {
		if p.Losses == 1 && p.Blocked {
			foundLoss = true
		}
	}
	if !foundLoss {
		t.Fatalf("the replayed stop-out must register one G2 loss, places=%+v", lim.Places)
	}
}

// TestMentorRebuildLimitsSkipsSwingAndOldDays: the swing never enters Limits, and
// a prior session-day's fill must not re-spend today's budget.
func TestMentorRebuildLimitsSkipsSwingAndOldDays(t *testing.T) {
	at, st := f2Trader(t)
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	at.mentorEval.State.Levels = []mentor.Level{{Key: "old_extreme:100", Kind: mentor.KindOldExtreme, Price: 100}}

	now := f2PinClock(t)
	// A SWING4H arm (exempt) and an old-day PHL arm (stale).
	swing := f2Arm("sig-sw", "SWING4H", 2, 0, 90, 88, 99, nil)
	swing.UpdatedAt = now
	_ = st.ArmedOrders().UpsertArm(swing)
	old := f2Arm("sig-old", "PHL", 2, 0, 90, 88, 99, nil)
	old.UpdatedAt = now.Add(-48 * time.Hour)
	_ = st.ArmedOrders().UpsertArm(old)

	at.mentorRebuildLimitsAtBoot()
	if lim := &at.mentorEval.State.Limits; lim.Long != nil {
		t.Fatalf("swing + old-day arms must not rebuild a leg, got %+v", lim.Long)
	}
}

// TestMentorRebuildLimitsBootBEStopDropsNotBooks is the m308 P2(b) pin: a
// CloseReasonStop close whose fill is NOT a loss (a break-even stop) must DROP
// the open trade via RealExit(id, false), so a later retrace through the
// ORIGINAL stop books no phantom stop-out. The old bare `continue` left the
// trade in l.open and the retrace booked a phantom stop-out. MUTANT: restore the
// bare continue on the not-a-loss branch → the retrace sets Long.Stopped → RED.
func TestMentorRebuildLimitsBootBEStopDropsNotBooks(t *testing.T) {
	at, st := f2Trader(t)
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	at.mentorEval.State.Levels = []mentor.Level{
		{Key: "old_extreme:100", Kind: mentor.KindOldExtreme, Price: 100},
	}

	now := f2PinClock(t)
	sig := "aa07e583-0000-0000-0000-0000000000be"
	// A FILLED mentor PHL arm from today (entry 90, original stop 88).
	arm := f2Arm(sig, "PHL", 2, 0, 90, 88, 99, nil)
	arm.UpdatedAt = now
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	// The same signal closed at BREAK-EVEN (exit 90 == entry) with CloseReasonStop.
	closed := &store.TraderPosition{
		TraderID: "tF2", ExchangeID: "NT8", ExchangeType: "ninjatrader",
		Symbol: "MNQ", Side: "LONG", EntryQuantity: 2, Quantity: 0,
		EntryPrice: 90, EntryOrderID: sig, EntryTime: now.Add(-10 * time.Minute).UnixMilli(),
		ExitPrice: 90, ExitTime: now.Add(-5 * time.Minute).UnixMilli(),
		CloseReason: store.CloseReasonStop, Status: "CLOSED", Source: "system",
	}
	if err := st.Position().CreateOpenPosition(closed); err != nil {
		t.Fatal(err)
	}

	at.mentorRebuildLimitsAtBoot()

	lim := &at.mentorEval.State.Limits
	if lim.Long == nil {
		t.Fatal("the filled PHL arm must rebuild the long leg")
	}
	if lim.Long.Stopped {
		t.Fatal("a break-even stop close must NOT set Leg.Stopped")
	}

	// A later candle retraces through the ORIGINAL stop 88: with the trade
	// dropped nothing books; with the old bare continue it would phantom-book.
	ct := time.Date(2026, 10, 2, 9, 20, 0, 0, time.UTC).UnixMilli()
	prev := market.Kline{OpenTime: ct, CloseTime: ct + 60_000, Open: 89, High: 90, Low: 88.5, Close: 89}
	cur := market.Kline{OpenTime: ct + 60_000, CloseTime: ct + 120_000, Open: 88.5, High: 89, Low: 87, Close: 88}
	lim.Apply(nil, prev, cur, cur.CloseTime, at.mentorEval.State.Levels, at.mentorEval.Cfg)

	if lim.Long.Stopped {
		t.Fatal("a BE-dropped trade must not phantom-book a later retrace stop-out")
	}
}
