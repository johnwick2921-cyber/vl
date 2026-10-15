package ninjatrader

import (
	"net"
	"testing"
	"time"
)

// WAVE 26 — stale-print guard, at the production call site (BarCache.Upsert).
// The 10-14 20:00 shape: a live 1m print right after a reconnect whose close is
// ~823 pts (2.7%) from both its own open and the previous close — the AddOn's
// reloaded series, not a trade.

const (
	// prevBarWireT is the 19:59 minute's wire CLOSE stamp (stores at 1792025940000).
	prevBarWireT = 1792026000000
	// staleBarWireT is the 20:00 minute's wire CLOSE stamp (stores at 1792026000000).
	staleBarWireT = 1792026060000
	// staleBarOpenT is the stored OPEN stamp the tests look the stale bar up by.
	staleBarOpenT = 1792026000000
)

func staleRig() *BarCache {
	c := NewBarCache(2500)
	// Pre-reconnect, the ring already holds a live bar (the 10-14 shape: the
	// process had been trading, so detectScaleMismatch's one-shot liveSeen was
	// already spent and could NOT catch the stale print — this guard is what
	// must). Seed 19:58, then a live 19:59 (prev close 29930.75).
	c.SeedHistorical("MNQ", "1m", []Bar{
		{T: prevBarWireT - 60000, O: 29915.0, H: 29930.0, L: 29905.0, C: 29922.0},
	})
	c.Upsert("MNQ", "1m", []Bar{
		{T: prevBarWireT, O: 29922.0, H: 29949.0, L: 29917.75, C: 29930.75},
	})
	// The NT8 restart: reconnect arms the stale-print guard.
	c.NoteReconnect(time.Now())
	return c
}

func stalePrintBar() Bar {
	return Bar{T: staleBarWireT, O: 29931.25, H: 30754.0, L: 29927.0, C: 30753.75, V: 976}
}

func barAt(c *BarCache, sym, tf string, t int64) (Bar, bool) {
	for _, b := range c.Get(sym, tf) {
		if b.T == t {
			return b, true
		}
	}
	return Bar{}, false
}

// The stale print is quarantined (mixed), never stored as live.
func TestStalePrintQuarantinedOnReconnect(t *testing.T) {
	stalePrintQuarantined.Store(0)
	c := staleRig()
	c.Upsert("MNQ", "1m", []Bar{stalePrintBar()})

	got, ok := barAt(c, "MNQ", "1m", staleBarOpenT)
	if !ok {
		t.Fatalf("the 20:00 bar is not in the ring")
	}
	if got.Source != BarSourceMixed {
		t.Fatalf("the stale print must be quarantined (mixed), got source=%q bar=%+v", got.Source, got)
	}
	if got.C != 30753.75 {
		t.Fatalf("quarantine must not edit values (A24): close=%v", got.C)
	}
	if n := StalePrintQuarantined(); n != 1 {
		t.Fatalf("quarantine counter = %d, want 1", n)
	}
}

// A real fast market — a 1% climb over several minutes — must NOT be
// quarantined: every minute's close stays near its own open.
func TestNormalFastMarketNotQuarantined(t *testing.T) {
	stalePrintQuarantined.Store(0)
	c := staleRig()
	live := []Bar{
		{T: staleBarWireT, O: 29930.75, H: 29980.0, L: 29925.0, C: 29978.0},
		{T: staleBarWireT + 60000, O: 29978.0, H: 30030.0, L: 29972.0, C: 30028.0},
		{T: staleBarWireT + 120000, O: 30028.0, H: 30080.0, L: 30022.0, C: 30078.0},
	}
	c.Upsert("MNQ", "1m", live)

	if n := StalePrintQuarantined(); n != 0 {
		t.Fatalf("a real fast market must not be quarantined: counter=%d", n)
	}
	if got, _ := barAt(c, "MNQ", "1m", staleBarOpenT+120000); got.Source != BarSourceLive {
		t.Fatalf("the real live bar must stay live, got source=%q", got.Source)
	}
}

// A gap open — the open 1% from the previous close but the close near its own
// open — must NOT be quarantined (a real gap is not a stale print).
func TestGapOpenNotQuarantined(t *testing.T) {
	stalePrintQuarantined.Store(0)
	c := staleRig()
	gap := Bar{T: staleBarWireT, O: 30300.0, H: 30310.0, L: 30295.0, C: 30305.0} // open 1.2% above prev close 29930.75
	c.Upsert("MNQ", "1m", []Bar{gap})

	if n := StalePrintQuarantined(); n != 0 {
		t.Fatalf("a gap open must not be quarantined: counter=%d", n)
	}
	if got, _ := barAt(c, "MNQ", "1m", staleBarOpenT); got.Source != BarSourceLive {
		t.Fatalf("the gap-open bar must stay live, got source=%q", got.Source)
	}
}

// Outside the 60 s reconnect window the live feed is trusted exactly as before.
func TestStalePrintOutsideWindowTrusted(t *testing.T) {
	stalePrintQuarantined.Store(0)
	c := NewBarCache(2500)
	c.SeedHistorical("MNQ", "1m", []Bar{
		{T: prevBarWireT - 60000, O: 29915.0, H: 29930.0, L: 29905.0, C: 29922.0},
	})
	c.Upsert("MNQ", "1m", []Bar{
		{T: prevBarWireT, O: 29922.0, H: 29949.0, L: 29917.75, C: 29930.75},
	})
	c.NoteReconnect(time.Now().Add(-61 * time.Second)) // reconnect 61 s ago
	c.Upsert("MNQ", "1m", []Bar{stalePrintBar()})

	if n := StalePrintQuarantined(); n != 0 {
		t.Fatalf("outside the window the feed is trusted: counter=%d", n)
	}
	if got, _ := barAt(c, "MNQ", "1m", staleBarOpenT); got.Source != BarSourceLive {
		t.Fatalf("outside the window the bar must stay live, got source=%q", got.Source)
	}
}

// F2 — the judge looks at the HIGH and LOW too, not only the close. A stale
// high with a real close (live ticks return the close to the real price while
// the 820-pt wick survives) must still be quarantined.
func TestStaleHighRealCloseQuarantined(t *testing.T) {
	stalePrintQuarantined.Store(0)
	c := staleRig()
	bar := Bar{T: staleBarWireT, O: 29931.25, H: 30754.0, L: 29927.0, C: 29954.0, V: 976}
	c.Upsert("MNQ", "1m", []Bar{bar})

	got, _ := barAt(c, "MNQ", "1m", staleBarOpenT)
	if got.Source != BarSourceMixed {
		t.Fatalf("a stale HIGH must quarantine the bar even with a real close, got source=%q bar=%+v", got.Source, got)
	}
	if n := StalePrintQuarantined(); n != 1 {
		t.Fatalf("quarantine counter = %d, want 1", n)
	}
}

// F2 — a minute the guard quarantined stays quarantined: a later "clean" update
// of the same T must not re-promote it to live.
func TestQuarantinedMinuteStaysQuarantined(t *testing.T) {
	stalePrintQuarantined.Store(0)
	c := staleRig()
	c.Upsert("MNQ", "1m", []Bar{stalePrintBar()})
	if n := StalePrintQuarantined(); n != 1 {
		t.Fatalf("first update must quarantine: counter=%d", n)
	}
	// A later live tick returns the close (and high) to the real price; the
	// minute must STAY mixed — a clean update must not re-promote it to live.
	clean := Bar{T: staleBarWireT, O: 29931.25, H: 29954.0, L: 29927.0, C: 29954.0, V: 980}
	c.Upsert("MNQ", "1m", []Bar{clean})
	if got, _ := barAt(c, "MNQ", "1m", staleBarOpenT); got.Source != BarSourceMixed {
		t.Fatalf("a quarantined minute must stay quarantined, got source=%q", got.Source)
	}
	if n := StalePrintQuarantined(); n != 1 {
		t.Fatalf("a clean update must not re-count: counter=%d, want 1", n)
	}
}

// P2 — the keep-quarantined freeze is STALE-PRINT only. A bar the SCALE-BREAK
// guard (detectScaleMismatch) marked mixed must re-promote to live when a later
// clean update of the same minute arrives — it is not in the stale-quarantined
// set, so the freeze must not touch it.
func TestScaleBreakMixedNotFrozen(t *testing.T) {
	stalePrintQuarantined.Store(0)
	c := NewBarCache(2500)
	// Flat seed on the old scale (the 10-14 ES replay side, close 7743.00).
	// prevBarWireT is the 19:59 wire CLOSE stamp; openStampBars stores both the
	// seed and the live bar one interval earlier.
	c.SeedHistorical("ES", "1m", []Bar{
		{T: prevBarWireT - 60000, O: 7743.0, H: 7743.0, L: 7743.0, C: 7743.0, V: 100},
	})
	liveOpenT := int64(prevBarWireT - 60000) // the 19:59 minute's stored OPEN stamp
	// First live bar on the real scale → detectScaleMismatch stamps it MIXED.
	first := Bar{T: prevBarWireT, O: 7843.0, H: 7846.0, L: 7841.0, C: 7845.25}
	c.Upsert("ES", "1m", []Bar{first})
	got, ok := barAt(c, "ES", "1m", liveOpenT)
	if !ok || got.Source != BarSourceMixed {
		t.Fatalf("scale-break must stamp the first live bar mixed, got source=%q ok=%v", got.Source, ok)
	}
	// A later clean update of the SAME minute: the scale-break mixed bar must
	// re-promote to live — only a stale-print quarantine stays frozen.
	clean := Bar{T: prevBarWireT, O: 7843.0, H: 7846.0, L: 7841.0, C: 7844.5}
	c.Upsert("ES", "1m", []Bar{clean})
	if got, _ := barAt(c, "ES", "1m", liveOpenT); got.Source != BarSourceLive {
		t.Fatalf("a scale-break mixed bar must re-promote to live, got source=%q", got.Source)
	}
	if n := StalePrintQuarantined(); n != 0 {
		t.Fatalf("the scale-break path must not touch the stale-print counter: %d", n)
	}
}

// F1 — the guard window is armed by a bars_subscribe resend (the gap-backfill /
// scale-break re-request), not only by a TCP accept.
func TestBarsSubscribeResendArmsStalePrintGuard(t *testing.T) {
	stalePrintQuarantined.Store(0)
	s := NewTCPServer(nil)
	srv, cli := net.Pipe()
	defer srv.Close()
	defer cli.Close()
	s.conn = srv
	s.barsSubscribe.Timeframes = []string{"1m"}
	s.barsSubscribe.BarsBack = 2000
	s.markFeedConnectedForTest(true)
	// Drain the subscribe frame the resend writes.
	go func() {
		for {
			if _, ok := readSubscribeFrame(t, cli, time.Second); !ok {
				return
			}
		}
	}()
	// The guard is EXPIRED — no TCP accept armed it here.
	s.barCache.NoteReconnect(time.Now().Add(-time.Hour))
	// The 10-14 shape: a live bar already seen (liveSeen spent), so the
	// scale-break one-shot cannot catch the stale print.
	s.barCache.SeedHistorical("MNQ", "1m", []Bar{
		{T: prevBarWireT - 60000, O: 29915.0, H: 29930.0, L: 29905.0, C: 29922.0},
	})
	s.barCache.Upsert("MNQ", "1m", []Bar{
		{T: prevBarWireT, O: 29922.0, H: 29949.0, L: 29917.75, C: 29930.75},
	})

	if err := s.RequestHistoryReplayAt("MNQ", time.Now()); err != nil {
		t.Fatalf("re-request: %v", err)
	}

	s.barCache.Upsert("MNQ", "1m", []Bar{stalePrintBar()})
	if n := StalePrintQuarantined(); n != 1 {
		t.Fatalf("the bars_subscribe resend must arm the guard: quarantined=%d, want 1", n)
	}
}
