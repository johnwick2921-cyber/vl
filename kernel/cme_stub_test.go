package kernel

import (
	"testing"
	"time"
)

// TestReanchorCMEStubCorrectsThe1600Stub pins m134 DS-108 (d): the NT8-native
// 2h/4h bar that ends at the 16:00 CT RTH close is stored 1h early — 12:00 for
// 4h, 14:00 for 2h — and must be re-anchored +1h to the 17:00-CT-anchored
// boundary (13:00 / 15:00). MUTANT: drop the re-anchor (return openMs) → RED.
func TestReanchorCMEStubCorrectsThe1600Stub(t *testing.T) {
	loc := CTLocation()
	ms := func(y int, mo time.Month, d, h, m int) int64 {
		return time.Date(y, mo, d, h, m, 0, 0, loc).UnixMilli()
	}
	cases := []struct {
		tf     string
		openMs int64
		want   int64
	}{
		// The 16:00 stub — 1h early → +1h.
		{"4h", ms(2026, 10, 20, 12, 0), ms(2026, 10, 20, 13, 0)},
		{"2h", ms(2026, 10, 20, 14, 0), ms(2026, 10, 20, 15, 0)},
		// A normal on-boundary bar is untouched.
		{"4h", ms(2026, 10, 20, 9, 0), ms(2026, 10, 20, 9, 0)},
		{"4h", ms(2026, 10, 20, 17, 0), ms(2026, 10, 20, 17, 0)},
		{"2h", ms(2026, 10, 20, 11, 0), ms(2026, 10, 20, 11, 0)},
		{"2h", ms(2026, 10, 20, 13, 0), ms(2026, 10, 20, 13, 0)},
		// Non-2h/4h timeframes are never touched.
		{"1m", ms(2026, 10, 20, 15, 59), ms(2026, 10, 20, 15, 59)},
		{"5m", ms(2026, 10, 20, 15, 0), ms(2026, 10, 20, 15, 0)},
		{"1h", ms(2026, 10, 20, 15, 0), ms(2026, 10, 20, 15, 0)},
	}
	for _, c := range cases {
		if got := ReanchorCMEStub(c.tf, c.openMs); got != c.want {
			t.Fatalf("ReanchorCMEStub(%q, %s) = %s, want %s",
				c.tf, time.UnixMilli(c.openMs).In(loc).Format("15:04"), time.UnixMilli(got).In(loc).Format("15:04"), time.UnixMilli(c.want).In(loc).Format("15:04"))
		}
	}
}

// TestReanchorCMEStubDST pins the fix across the CDT/CST boundary: the stub is
// the bar that ENDS at 16:00 CT, whatever the UTC offset is that day.
func TestReanchorCMEStubDST(t *testing.T) {
	loc := CTLocation()
	// 2026-03-09 is still CST (UTC-6); 16:00 CT = 22:00 UTC.
	mar := time.Date(2026, 3, 9, 12, 0, 0, 0, loc).UnixMilli() // 4h stub open 12:00 CT
	if got := ReanchorCMEStub("4h", mar); got != mar+int64(time.Hour/time.Millisecond) {
		t.Fatalf("CST stub: got %s, want +1h", time.UnixMilli(got).In(loc).Format("15:04"))
	}
	// 2026-07-20 is CDT (UTC-5); 16:00 CT = 21:00 UTC.
	jul := time.Date(2026, 7, 20, 12, 0, 0, 0, loc).UnixMilli()
	if got := ReanchorCMEStub("4h", jul); got != jul+int64(time.Hour/time.Millisecond) {
		t.Fatalf("CDT stub: got %s, want +1h", time.UnixMilli(got).In(loc).Format("15:04"))
	}
}
