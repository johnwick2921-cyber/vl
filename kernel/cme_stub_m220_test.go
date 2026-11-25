package kernel

import (
	"testing"
	"time"
)

func stubMs(y int, mo time.Month, d, h, mi int) int64 {
	return time.Date(y, mo, d, h, mi, 0, 0, CTLocation()).UnixMilli()
}

// TestReanchorCMEStubCorrectsSessionCloseStubAllTFs pins #34 (m220) across the
// whole 2h/4h/6h/8h/12h family: the 23h CME session does not divide by any of
// them, so each day's last (short) bar closes at 16:00 CT and NT8 stamps it one
// hour early. In CDT the open-stamped stubs are 2h:14:00, 4h:12:00, 6h:10:00,
// 8h:08:00, 12h:04:00 — each must shift +1h to the 17:00-anchored boundary.
func TestReanchorCMEStubCorrectsSessionCloseStubAllTFs(t *testing.T) {
	cases := []struct {
		tf          string
		wrong, want int64
	}{
		{"2h", stubMs(2026, 10, 20, 14, 0), stubMs(2026, 10, 20, 15, 0)},
		{"4h", stubMs(2026, 10, 20, 12, 0), stubMs(2026, 10, 20, 13, 0)},
		{"6h", stubMs(2026, 10, 20, 10, 0), stubMs(2026, 10, 20, 11, 0)},
		{"8h", stubMs(2026, 10, 20, 8, 0), stubMs(2026, 10, 20, 9, 0)},
		{"12h", stubMs(2026, 10, 20, 4, 0), stubMs(2026, 10, 20, 5, 0)},
	}
	for _, c := range cases {
		if got := ReanchorCMEStub(c.tf, c.wrong); got != c.want {
			t.Errorf("ReanchorCMEStub(%q, %d) = %d, want %d", c.tf, c.wrong, got, c.want)
		}
	}
}

// TestReanchorCMEStubLeavesOtherBarsUntouched pins that a full-length bar and
// any non-coarse TF are never moved — the correction is stub-only.
func TestReanchorCMEStubLeavesOtherBarsUntouched(t *testing.T) {
	for _, tf := range []string{"2h", "4h", "6h", "8h", "12h"} {
		open := stubMs(2026, 10, 20, 17, 0) // 17:00 CT open — a full-length bar, not the 16:00 stub
		if got := ReanchorCMEStub(tf, open); got != open {
			t.Errorf("ReanchorCMEStub(%q, 17:00) moved a non-stub bar: %d → %d", tf, open, got)
		}
	}
	if got := ReanchorCMEStub("1h", stubMs(2026, 10, 20, 15, 0)); got != stubMs(2026, 10, 20, 15, 0) {
		t.Errorf("ReanchorCMEStub moved a 1h bar: got %d", got)
	}
}

// TestReanchorCMEStubAcrossOffsets pins DST safety for the new 6h/8h/12h cases:
// the +1h shift must hold in BOTH CT offsets (CDT and CST).
func TestReanchorCMEStubAcrossOffsetsAllTFs(t *testing.T) {
	// July (CDT, UTC-5): 4h stub stored 12:00 CDT → 13:00 CDT.
	jul := stubMs(2026, 7, 1, 12, 0)
	if got := ReanchorCMEStub("4h", jul); got != jul+int64(time.Hour/time.Millisecond) {
		t.Fatalf("CDT 4h stub = %d, want %d", got, jul+int64(time.Hour/time.Millisecond))
	}
	// March 2 (CST, UTC-6, before the 2nd-Sunday DST switch): 8h stub stored
	// 08:00 CST → 09:00 CST.
	mar := stubMs(2026, 3, 2, 8, 0)
	if got := ReanchorCMEStub("8h", mar); got != mar+int64(time.Hour/time.Millisecond) {
		t.Fatalf("CST 8h stub = %d, want %d", got, mar+int64(time.Hour/time.Millisecond))
	}
}
