package trader

import (
	"testing"
	"time"

	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// m463 — D5.2-R27 [D5.2 p3 @12:16–13:18 "9h đó là xong rồi. Là cây nến này kết
// thúc, mình không có lệnh nào hết"]: when the 4h candle that carries the line
// closes, the instance is over and NO order may remain.
//
// Trade 678 (row 410): the 13:00 CT candle's swing order had its expiry at
// 16:59:59.999 — inside the 16:00–17:00 break — and filled at 17:00:40 on the
// reopen. m244 sweeps due expiries at the FIRST OPEN cycle after the break, but
// the closed path backs off 3 minutes, so that cycle can land minutes after the
// 17:00 reopen print: the race is narrowed, not closed. m463 closes it on both
// sides: the kernel ends a swing order at the MARKET close when the CME shuts
// inside its candle (expiry 15:59:59.999 — the 13:00 candle's last bar is
// 15:55), and the closed path sweeps a due expiry on EVERY closed cycle, so the
// order is cancelled during the break, an hour before the reopen.

// TestM463ClosedCycleSweepsSwingArmEndedAtTheClose — at 16:01 CT (closed) the
// working swing arm whose expiry is the 16:00 close is cancelled; a not-yet-due
// arm is untouched. MUTANT: drop the closed-path sweep call in
// cmeSessionClosedSkip → the arm stays working through the break → RED.
func TestM463ClosedCycleSweepsSwingArmEndedAtTheClose(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	now := b3Clock(16, 1) // Wed 2026-09-23 16:01 CT — the daily break
	if srv := at.armedTrader().GetServer(); srv != nil {
		srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	}
	open := true
	at.cmePrevOpen = &open // the previous cycle (15:59) was open
	prev := testNow
	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = prev })

	closeMs := b3Clock(16, 0).UnixMilli()
	seedM244Arm(t, at, ledger, "swing-1300", store.StateWorking, "sig-swing-1300", closeMs-1)
	seedM244Arm(t, at, ledger, "future", store.StateWorking, "sig-future", b3Clock(21, 0).UnixMilli())

	if !at.cmeSessionClosedSkip() {
		t.Fatal("16:01 CT is the daily break — cmeSessionClosedSkip must skip")
	}
	if got := readArmRow(t, ledger, "swing-1300"); got.State != store.StateCancelPending {
		t.Fatalf("the swing arm whose 4h candle ended at the 16:00 close must be cancelled during the break, got state=%q", got.State)
	}
	if got := readArmRow(t, ledger, "future"); got.State != store.StateWorking {
		t.Fatalf("a not-yet-due arm must be untouched, got state=%q", got.State)
	}
}

// TestM463SwingExpiryFromTheEvaluatorEndsAtTheClose — the injector's expiry for
// a SWING4H intent is the kernel's (Intent.ExpiryMs, set by runSwing); the
// trader's own fallback (mentorSwingExpiry, used only when the kernel left it
// 0) must not reach past the close either.
func TestM463SwingExpiryFromTheEvaluatorEndsAtTheClose(t *testing.T) {
	bar := b3Clock(15, 19).UnixMilli() + 59_999 // the 15:19 bar closes 15:20
	in := mentor.Intent{Setup: "SWING4H", ArmID: "swing-x"}
	got := mentorIntentExpiry(in, bar)
	if want := b3Clock(16, 0).UnixMilli() - 1; got != want {
		t.Fatalf("swing fallback expiry = %s, want %s (the 16:00 close ends the 13:00 candle)",
			time.UnixMilli(got).Format("15:04:05.000"), time.UnixMilli(want).Format("15:04:05.000"))
	}
	// a normal bucket keeps its own end (the 09:00 candle → 12:59:59.999)
	bar = b3Clock(9, 40).UnixMilli() + 59_999
	if got, want := mentorIntentExpiry(in, bar), b3Clock(13, 0).UnixMilli()-1; got != want {
		t.Fatalf("09:00 candle expiry = %s, want %s", time.UnixMilli(got).Format("15:04:05.000"), time.UnixMilli(want).Format("15:04:05.000"))
	}
}
