package ninjatrader

import (
	"testing"
	"time"
)

// Release #22 OPS-SAFETY: LastLiveReceipt is the stale gate's MONOTONIC leg.
// A historical seed never counts (a replay is not proof the feed is alive);
// an accepted live frame does, stamped with a monotonic-carrying time.Now().
func TestBarCacheLastLiveReceiptOnlyLiveFramesCount(t *testing.T) {
	c := NewBarCache(0)
	t0 := int64(1_790_000_040_000)
	c.SeedHistorical("MNQ", "1m", []Bar{{T: t0, O: 1, H: 2, L: 1, C: 2, V: 5}})
	if _, ok := c.LastLiveReceipt("MNQ", "1m"); ok {
		t.Fatalf("a historical seed must not count as a live receipt")
	}
	before := time.Now()
	c.Upsert("MNQ", "1m", []Bar{{T: t0 + 60_000, O: 2, H: 3, L: 2, C: 3, V: 7}})
	recv, ok := c.LastLiveReceipt("MNQ", "1m")
	if !ok || recv.Before(before) {
		t.Fatalf("an accepted live frame must stamp the receipt, got %v ok=%v", recv, ok)
	}
	if recv.Round(0) == recv {
		t.Fatalf("the receipt must carry the monotonic reading (time.Now()), so time.Since ignores wall steps")
	}
	// An out-of-order bar the ring never held is refused and does not refresh it.
	c.Upsert("MNQ", "1m", []Bar{{T: t0 - 600_000, O: 2, H: 3, L: 2, C: 3, V: 7}})
	if again, _ := c.LastLiveReceipt("MNQ", "1m"); !again.Equal(recv) {
		t.Fatalf("a refused out-of-order bar must not refresh the receipt")
	}
	if n := c.PurgeSymbol("MNQ"); n == 0 {
		t.Fatalf("purge removed nothing")
	}
	if _, ok := c.LastLiveReceipt("MNQ", "1m"); ok {
		t.Fatalf("a purged symbol must drop its receipt too")
	}
}
