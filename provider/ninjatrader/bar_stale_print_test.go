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

// Release #27 — the guard must never hide a REAL higher-timeframe bar. On 10-15
// the #26 guard (0.5% on every TF) hid the forming 1d and 3d bars of MNQ and ES
// in the reconnect window after the boot. A real daily bar whose high sits 3%
// from its open and from the previous close is NOT quarantined.
func TestRealDailyBarNotQuarantined(t *testing.T) {
	stalePrintQuarantined.Store(0)
	c := NewBarCache(2500)
	// History, then a live bar (the scale-break check's one-shot is spent, as on
	// a running bot), then the reconnect that arms the guard.
	// Wire stamps are bar CLOSE times (open + 1d); the ring stores open stamps.
	w := int64(staleBarOpenT + 86_400_000)
	c.SeedHistorical("MNQ", "1d", []Bar{{T: w - 2*86_400_000, O: 30700, H: 30850, L: 30650, C: 30800}})
	c.Upsert("MNQ", "1d", []Bar{{T: w - 86_400_000, O: 30800, H: 30905, L: 30140, C: 30151}})
	c.NoteReconnect(time.Now())
	day := Bar{T: w, O: 30151, H: 30159, L: 29247, C: 29300} // −3% day
	c.Upsert("MNQ", "1d", []Bar{day})
	got, ok := barAt(c, "MNQ", "1d", staleBarOpenT)
	if !ok || got.Source != BarSourceLive {
		t.Fatalf("a real daily bar must stay live, got ok=%v source=%q", ok, got.Source)
	}
	if n := StalePrintQuarantined(); n != 0 {
		t.Fatalf("quarantine counter = %d, want 0 for a 1d bar", n)
	}
}

// A real 1m bar with a 0.64% extreme (the 10-14 07:30 CPI minute's size) right
// after a reconnect is NOT quarantined under the 2% bound.
func TestRealBigMinuteNotQuarantined(t *testing.T) {
	stalePrintQuarantined.Store(0)
	c := NewBarCache(2500)
	prevC := 30842.25
	w := int64(staleBarOpenT + 60_000) // wire stamp = close time
	c.SeedHistorical("MNQ", "1m", []Bar{{T: w - 120_000, O: 30838, H: 30846, L: 30832, C: 30840}})
	c.Upsert("MNQ", "1m", []Bar{{T: w - 60_000, O: 30840, H: 30851, L: 30835, C: prevC}})
	c.NoteReconnect(time.Now())
	cpi := Bar{T: w, O: 30842.25, H: 30851.75, L: 30645.25, C: 30645.25} // −0.64%
	c.Upsert("MNQ", "1m", []Bar{cpi})
	got, _ := barAt(c, "MNQ", "1m", staleBarOpenT)
	if got.Source != BarSourceLive {
		t.Fatalf("a real 0.64%% minute must stay live, got source=%q", got.Source)
	}
}

// ── release #28: HTF cross-check against the bar's own 1m bars ─────────────

// htfRig builds a ring whose 1m bars cover [startOpen, startOpen+n min) cleanly
// (a gentle walk around base), as a running bot would hold them.
func htfRig(startOpen int64, n int, base float64) *BarCache {
	c := NewBarCache(2500)
	var hist []Bar
	for i := 0; i < n; i++ {
		o := base + float64(i%5)
		hist = append(hist, Bar{T: startOpen + int64(i)*60_000 + 60_000, O: o, H: o + 3, L: o - 3, C: o + 1})
	}
	c.SeedHistorical("MNQ", "1m", hist[:n-1])
	c.Upsert("MNQ", "1m", hist[n-1:]) // one live bar: the scale-break one-shot is spent
	return c
}

// The 10-14 shape on the 1h bar: the 20:00 hour's real 1m bars sit near 29,930,
// the HTF bar carries the stale high 30,754 → quarantined.
func TestHTFStaleHighQuarantinedAgainstItsOwnMinutes(t *testing.T) {
	stalePrintQuarantined.Store(0)
	open := int64(staleBarOpenT) // 20:00 CT 10-14
	c := htfRig(open, 60, 29930)
	c.NoteReconnect(time.Now())
	c.Upsert("MNQ", "1h", []Bar{{T: open + 3_600_000, O: 29931.25, H: 30754.0, L: 29927.0, C: 29954.25}})
	got, ok := barAt(c, "MNQ", "1h", open)
	if !ok || got.Source != BarSourceMixed {
		t.Fatalf("a 1h bar whose high its own 1m bars never traded must be quarantined, got ok=%v source=%q", ok, got.Source)
	}
}

// A real 1h bar with a 3% range is NOT quarantined when its 1m bars carry that
// range (the reference, not a fixed percentage, is the judge).
func TestHTFRealBigHourNotQuarantined(t *testing.T) {
	stalePrintQuarantined.Store(0)
	open := int64(staleBarOpenT)
	// the hour's real 1m bars, one of which (20:30) ran to 30,830: a real 3% hour
	c := NewBarCache(2500)
	var hist []Bar
	for i := 0; i < 60; i++ {
		b := Bar{T: open + int64(i)*60_000 + 60_000, O: 29930, H: 29933, L: 29927, C: 29931}
		if i == 30 {
			b = Bar{T: open + int64(i)*60_000 + 60_000, O: 29960, H: 30830, L: 29950, C: 30800}
		}
		hist = append(hist, b)
	}
	c.SeedHistorical("MNQ", "1m", hist[:59])
	c.Upsert("MNQ", "1m", hist[59:])
	c.NoteReconnect(time.Now())
	c.Upsert("MNQ", "1h", []Bar{{T: open + 3_600_000, O: 29931.25, H: 30830, L: 29927.0, C: 30800}})
	got, _ := barAt(c, "MNQ", "1h", open)
	if got.Source != BarSourceLive {
		t.Fatalf("a real 3%% hour must stay live, got source=%q", got.Source)
	}
	if n := StalePrintQuarantined(); n != 0 {
		t.Fatalf("quarantine counter = %d, want 0", n)
	}
}

// A 1m hole inside the period means the 1m reference may miss a real extreme:
// the HTF bar is NOT judged (fail open, as before release #28).
func TestHTFNotJudgedOverA1mHole(t *testing.T) {
	stalePrintQuarantined.Store(0)
	open := int64(staleBarOpenT)
	c := NewBarCache(2500)
	var hist []Bar
	for i := 0; i < 60; i++ {
		if i >= 10 && i < 40 { // 30-minute hole
			continue
		}
		hist = append(hist, Bar{T: open + int64(i)*60_000 + 60_000, O: 29930, H: 29933, L: 29927, C: 29931})
	}
	c.SeedHistorical("MNQ", "1m", hist[:len(hist)-1])
	c.Upsert("MNQ", "1m", hist[len(hist)-1:])
	c.NoteReconnect(time.Now())
	c.Upsert("MNQ", "1h", []Bar{{T: open + 3_600_000, O: 29931.25, H: 30754.0, L: 29927.0, C: 29954.25}})
	got, _ := barAt(c, "MNQ", "1h", open)
	if got.Source != BarSourceLive {
		t.Fatalf("over a 1m hole the HTF bar must not be judged, got source=%q", got.Source)
	}
}

// A daily bar is never judged by the HTF cross-check (its stamp does not match
// its session minutes); the 10-15 case: a real daily bar must never be hidden.
func TestHTFDailyBeyondTheRingNotJudged(t *testing.T) {
	stalePrintQuarantined.Store(0)
	dayOpen := int64(staleBarOpenT) - 3*3_600_000 // 17:00 CT
	c := htfRig(int64(staleBarOpenT), 60, 29930)  // the ring starts at 20:00, after the day's open
	c.NoteReconnect(time.Now())
	c.Upsert("MNQ", "1d", []Bar{{T: dayOpen + 86_400_000, O: 30151, H: 30159, L: 29247, C: 29300}})
	got, _ := barAt(c, "MNQ", "1d", dayOpen)
	if got.Source != BarSourceLive {
		t.Fatalf("a daily bar the 1m ring does not cover must not be judged, got source=%q", got.Source)
	}
}

// Outside the reconnect window the HTF bar is trusted, exactly as the 1m guard.
func TestHTFOutsideWindowTrusted(t *testing.T) {
	stalePrintQuarantined.Store(0)
	open := int64(staleBarOpenT)
	c := htfRig(open, 60, 29930)
	c.NoteReconnect(time.Now().Add(-2 * stalePrintGuardWindow))
	c.Upsert("MNQ", "1h", []Bar{{T: open + 3_600_000, O: 29931.25, H: 30754.0, L: 29927.0, C: 29954.25}})
	got, _ := barAt(c, "MNQ", "1h", open)
	if got.Source != BarSourceLive {
		t.Fatalf("outside the window the HTF bar is trusted, got source=%q", got.Source)
	}
}

// Even with full 1m coverage of a FORMING daily bar, the daily bar is not judged
// (stamp/session mismatch). Stamps are relative to now so the bar is forming
// and its minutes so far are complete — only the TF list keeps it unjudged.
func TestHTFDailyNeverJudgedEvenWithCoverage(t *testing.T) {
	stalePrintQuarantined.Store(0)
	nowMin := time.Now().Truncate(time.Minute).UnixMilli()
	open := nowMin - 30*60_000
	c := htfRig(open, 30, 29930) // 1m bars for every minute from open to now-1
	c.NoteReconnect(time.Now())
	c.Upsert("MNQ", "1d", []Bar{{T: open + 86_400_000, O: 29931.25, H: 30754.0, L: 29927.0, C: 29931.0}})
	got, _ := barAt(c, "MNQ", "1d", open)
	if got.Source != BarSourceLive {
		t.Fatalf("a daily bar must never be judged by the HTF check, got source=%q", got.Source)
	}
}

// The same forming shape on a 1h bar IS judged (control for the daily test).
func TestHTFFormingHourJudged(t *testing.T) {
	stalePrintQuarantined.Store(0)
	nowMin := time.Now().Truncate(time.Minute).UnixMilli()
	open := nowMin - 30*60_000
	c := htfRig(open, 30, 29930)
	c.NoteReconnect(time.Now())
	c.Upsert("MNQ", "1h", []Bar{{T: open + 3_600_000, O: 29931.25, H: 30754.0, L: 29927.0, C: 29931.0}})
	got, _ := barAt(c, "MNQ", "1h", open)
	if got.Source != BarSourceMixed {
		t.Fatalf("a forming 1h bar with a stale high must be quarantined, got source=%q", got.Source)
	}
}
