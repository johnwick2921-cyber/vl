package ninjatrader

import (
	"testing"
	"time"

	"vl/kernel"
)

// TestBarCacheReanchorsTheCMEStubAtIngress pins m169 DS-108 item 1 at the
// PRODUCTION seam: the NT8-native 2h/4h 16:00-close stub arrives CLOSE-stamped
// (16:00 CT), and the cache must hold it OPEN-stamped AND re-anchored +1h —
// 13:00 for 4h, 15:00 for 2h — so the ring and the store agree on ONE stamp
// (before this the ring kept 12:00/14:00 and the store wrote 13:00/15:00, which
// doubled the stub in the chart splice and at boot backfill). MUTANT: drop the
// reanchorStubBars call → the ring holds 12:00/14:00 → RED.
func TestBarCacheReanchorsTheCMEStubAtIngress(t *testing.T) {
	loc := kernel.CTLocation()
	ms := func(h, m int) int64 {
		return time.Date(2026, 10, 20, h, m, 0, 0, loc).UnixMilli()
	}
	c := NewBarCache(64)

	// Historical replay frame: close-stamped at the 16:00 CT RTH close.
	c.SeedHistorical("MNQ", "4h", []Bar{{T: ms(16, 0), O: 1, H: 2, L: 0.5, C: 1.5, V: 7}})
	c.SeedHistorical("MNQ", "2h", []Bar{{T: ms(16, 0), O: 1, H: 2, L: 0.5, C: 1.5, V: 7}})

	if got := c.Get("MNQ", "4h")[0].T; got != ms(13, 0) {
		t.Fatalf("4h stub must be re-anchored at ingress: got %s, want 13:00 CT",
			time.UnixMilli(got).In(loc).Format("15:04"))
	}
	if got := c.Get("MNQ", "2h")[0].T; got != ms(15, 0) {
		t.Fatalf("2h stub must be re-anchored at ingress: got %s, want 15:00 CT",
			time.UnixMilli(got).In(loc).Format("15:04"))
	}

	// A live frame for the same stubs goes through Upsert, which must also
	// re-anchor (the ring never holds NT8's 12:00/14:00 again).
	c2 := NewBarCache(64)
	c2.Upsert("MNQ", "4h", []Bar{{T: ms(16, 0), O: 1, H: 2, L: 0.5, C: 1.5, V: 7}})
	c2.Upsert("MNQ", "2h", []Bar{{T: ms(16, 0), O: 1, H: 2, L: 0.5, C: 1.5, V: 7}})
	if got := c2.Get("MNQ", "4h")[0].T; got != ms(13, 0) {
		t.Fatalf("live 4h stub must be re-anchored at ingress: got %s, want 13:00 CT",
			time.UnixMilli(got).In(loc).Format("15:04"))
	}
	if got := c2.Get("MNQ", "2h")[0].T; got != ms(15, 0) {
		t.Fatalf("live 2h stub must be re-anchored at ingress: got %s, want 15:00 CT",
			time.UnixMilli(got).In(loc).Format("15:04"))
	}

	// A normal on-boundary 4h bar (open 17:00 CT → close 21:00) is untouched.
	c3 := NewBarCache(64)
	c3.Upsert("MNQ", "4h", []Bar{{T: ms(21, 0), O: 1, H: 2, L: 0.5, C: 1.5, V: 7}})
	if got := c3.Get("MNQ", "4h")[0].T; got != ms(17, 0) {
		t.Fatalf("a normal 4h bar must not shift: got %s, want 17:00 CT",
			time.UnixMilli(got).In(loc).Format("15:04"))
	}
}
