package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// The evaluator is ticked with BarCloseInstant(lastBar) — the instant the bar
// closed — exactly as production (trader mentorEvalOnce) does. Every pin below
// drives Tick with that clock and asserts the gate fires on the eval of the bar
// whose close completes it, not one bar later. Restoring an OpenTime clock
// (BarCloseInstant → last.OpenTime) turns each of them RED.

func clockBar(openMs int64, o, h, l, c float64) market.Kline {
	return market.Kline{OpenTime: openMs, CloseTime: openMs + 59_999, Open: o, High: h, Low: l, Close: c}
}

func clockCfg() Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	return cfg
}

func clockHM(ms int64) string { return time.UnixMilli(ms).In(ctime()).Format("15:04:05.000") }

// flatDay returns flat 1m bars from `from` (minute of day) up to but not
// including `to`.
func flatDay(day int64, from, to int) []market.Kline {
	var out []market.Kline
	for m := from; m < to; m++ {
		out = append(out, clockBar(day+int64(m)*60_000, 100, 101, 99, 100))
	}
	return out
}

// B-clock (a): the ORB escape. orb.go tests `cur.CloseTime > now` to ask whether
// the last candle has closed; with an OpenTime clock that is always true and the
// escape could never latch (the ORB gate, default ON, then refuses every entry).
func TestClockORBEscapeLatchesOnItsCloseInstant(t *testing.T) {
	day := ctMs(t, 0, 0, 0)
	at := func(h, m int) int64 { return day + int64(h*60+m)*60_000 }
	bars := flatDay(day, 6*60+30, 8*60+30)
	bars = append(bars,
		clockBar(at(8, 30), 100, 101, 99, 100),
		clockBar(at(8, 31), 100, 101, 99.5, 100),
		clockBar(at(8, 32), 100, 105.5, 100, 105), // closes outside the 08:30–08:31 box
		clockBar(at(8, 33), 105, 106, 104, 105),
	)
	e := New(clockCfg())
	escapedAt, drawnAt := "", ""
	for i := 2; i <= len(bars); i++ {
		last := bars[i-1]
		e.Tick(bars[:i], BarCloseInstant(last))
		if e.State.ORB.Drawn && drawnAt == "" {
			drawnAt = clockHM(last.OpenTime)
		}
		if e.State.ORB.Escaped != "" && escapedAt == "" {
			escapedAt = clockHM(last.OpenTime)
		}
	}
	if drawnAt != "08:31:00.000" {
		t.Fatalf("ORB drawn on the eval of the bar opened %q, want 08:31:00.000 (the 08:32:00 close that completes the 2-minute candle)", drawnAt)
	}
	if escapedAt != "08:32:00.000" {
		t.Fatalf("ORB escape latched on the eval of the bar opened %q, want 08:32:00.000 (the first 1m close outside the box)", escapedAt)
	}
	if e.State.ORB.Escaped != SideLong {
		t.Fatalf("ORB escape = %q, want long", e.State.ORB.Escaped)
	}
}

// B-clock (b): a 5m candle counts as closed on the eval of its LAST 1m bar.
// The 5m ISB box is built from the two latest CLOSED 5m buckets.
func TestClockFiveMinuteBucketClosedOnItsCloseInstant(t *testing.T) {
	day := ctMs(t, 0, 0, 0)
	at := func(m int) int64 { return day + int64(9*60+m)*60_000 }
	mk := func(m int, o, h, l, c float64) market.Kline { return clockBar(at(m), o, h, l, c) }
	bars := []market.Kline{
		// bucket A (09:00–09:04): a green mother candle, range 97–106.
		mk(0, 99, 106, 97, 105),
		mk(1, 100, 105.5, 99.5, 104),
		mk(2, 101, 104.5, 100, 103),
		mk(3, 102, 104, 101, 103.5),
		mk(4, 101, 104, 100.5, 105),
		// bucket B (09:05–09:09): its body sits inside A's range → 5m ISB.
		mk(5, 101, 104, 100.5, 102),
		mk(6, 101, 104, 100.5, 102),
		mk(7, 101, 104, 100.5, 102),
		mk(8, 101, 104, 100.5, 102),
		mk(9, 101, 104, 100.5, 102),
	}
	e := New(clockCfg())
	boxAt := ""
	for i := 2; i <= len(bars); i++ {
		last := bars[i-1]
		e.Tick(bars[:i], BarCloseInstant(last))
		if e.State.ISBBox != nil && boxAt == "" {
			boxAt = clockHM(last.OpenTime)
		}
	}
	if boxAt != "09:09:00.000" {
		t.Fatalf("5m ISB box first stood on the eval of the bar opened %q, want 09:09:00.000 (the 09:10:00 close of bucket B)", boxAt)
	}
}

// B-clock (c): the B6 window end. A resting level order is cancelled at 15:00 CT;
// the bar opened 14:59 closes at 15:00:00, so that is the eval that cancels it.
func TestClockWindowEndCancelOnItsCloseInstant(t *testing.T) {
	day := ctMs(t, 0, 0, 0)
	bars := flatDay(day, 13*60, 15*60+3)
	e := New(clockCfg())
	cancelledAt := ""
	for i := 2; i <= len(bars); i++ {
		last := bars[i-1]
		if last.OpenTime == day+int64(14*60+57)*60_000 {
			// the order rests; price (100) never closes through its level (50)
			e.State.LevelArms = map[string]LevelArm{"k": {ArmID: "a", Side: SideLong, LevelPrice: 50, PlacedAt: last.CloseTime - 600_000}}
		}
		for _, in := range e.Tick(bars[:i], BarCloseInstant(last)) {
			if in.Action == CancelArm && in.ArmID == "a" && cancelledAt == "" {
				cancelledAt = clockHM(last.OpenTime)
			}
		}
	}
	if cancelledAt != "14:59:00.000" {
		t.Fatalf("the window-end cancel fired on the eval of the bar opened %q, want 14:59:00.000 (it closes at 15:00:00)", cancelledAt)
	}
}

// B-clock (d): a swing order decided on the bar that closes at a 4h boundary
// belongs to the NEW 4h candle; with an OpenTime clock its expiry was already in
// the past (12:59:59.999) at the moment it was placed.
func TestClockSwingExpiryOnTheBoundaryBar(t *testing.T) {
	day := ctMs(t, 0, 0, 0)
	last := clockBar(day+int64(12*60+59)*60_000, 100, 101, 99, 100) // closes 13:00:00 CT, a 4h boundary
	now := BarCloseInstant(last)
	exp := swingExpiry(now)
	if exp <= now {
		t.Fatalf("swing expiry %s is not after the placement instant %s", clockHM(exp), clockHM(now))
	}
	if want := day + int64(17*60)*60_000 - 1; exp != want {
		t.Fatalf("swing expiry = %s, want %s (the close of the 13:00–17:00 4h candle)", clockHM(exp), clockHM(want))
	}
}

// B-clock (e): the incremental 1m EMA includes the bar that just closed — the
// seeded evaluator must equal a full rebuild at the same instant.
func TestClockSeededEMAIncludesTheJustClosedBar(t *testing.T) {
	day := ctMs(t, 0, 0, 0)
	var bars []market.Kline
	cl := 100.0
	for m := 6 * 60; m < 6*60+240; m++ {
		cl += 0.25
		bars = append(bars, clockBar(day+int64(m)*60_000, cl-0.125, cl+0.25, cl-0.25, cl))
	}
	cfg := clockCfg()
	e := New(cfg)
	seedN := 200
	Seed(e, bars[:seedN], BarCloseInstant(bars[seedN-1]))
	for i := seedN + 1; i <= len(bars); i++ {
		e.Tick(bars[:i], BarCloseInstant(bars[i-1]))
		var closes []float64
		for _, b := range bars[:i] {
			closes = append(closes, b.Close)
		}
		if want := emaFromCloses(closes, cfg.EMAPeriod34); math.Abs(e.State.EMA34-want) > 1e-9 {
			t.Fatalf("after the bar opened %s EMA34 = %.6f, want %.6f (the full rebuild incl. the just-closed bar)", clockHM(bars[i-1].OpenTime), e.State.EMA34, want)
		}
	}
}

// B-clock (f): a pending order lives until the close of the NEXT 1m candle
// (expiry = placement bar CloseTime+60_000). It must be gone on the eval of that
// next bar; with an OpenTime clock it survived one bar longer and could fill.
func TestClockPendingOrderExpiresOnTheNextBarEval(t *testing.T) {
	day := ctMs(t, 0, 0, 0)
	bars := flatDay(day, 9*60, 9*60+60)
	placed := 40 // the EMA pending order was decided on bars[placed]
	e := New(clockCfg())
	var gone string
	for i := 2; i <= len(bars); i++ {
		last := bars[i-1]
		if i-1 == placed {
			e.State.EmaPendingSide = SideLong
			e.State.EmaPendingEntry = 1e9 // never touched
			e.State.EmaPendingStop = 1
			e.State.EmaPendingTarget = 2e9
			e.State.EmaPendingExpiry = last.CloseTime + 60_000
		}
		e.Tick(bars[:i], BarCloseInstant(last))
		if i-1 > placed && e.State.EmaPendingSide == "" && gone == "" {
			gone = clockHM(last.OpenTime)
		}
	}
	if want := clockHM(bars[placed+1].OpenTime); gone != want {
		t.Fatalf("pending order was dropped on the eval of the bar opened %q, want %q (the next bar, whose close is the expiry)", gone, want)
	}
}

// B-clock (g): a 5m swing high is confirmed by the K buckets after it. The last
// of them closes exactly at the eval instant; kernel.aggregateBars gives a bucket
// CloseTime = open+interval (exclusive), so the mentor hands SwingPointLevels
// now+1 (swingPointNow) — otherwise the swing appears one bar late.
func TestClockSwingPointSeenOnTheClosingBar(t *testing.T) {
	day := ctMs(t, 0, 0, 0)
	var bars []market.Kline
	const nb = 30
	peak := nb - 1 - 2 // two confirming 5m buckets after the peak (K=2)
	for b := 0; b < nb; b++ {
		hi, lo := 101.0+0.1*float64(b%3), 99.0-0.1*float64(b%4)
		if b == peak {
			hi, lo = 120, 100
		}
		for m := 0; m < 5; m++ {
			bars = append(bars, clockBar(day+int64(9*60+b*5+m)*60_000, 100, hi, lo, 100))
		}
	}
	last := bars[len(bars)-1] // the last 1m of the last 5m bucket: that bucket closes at the eval instant
	found := false
	for _, l := range Levels(bars, clockCfg(), BarCloseInstant(last)) {
		if l.Kind == KindOldExtreme && l.Price == 120 {
			found = true
		}
	}
	if !found {
		t.Fatal("the 5m swing high confirmed by the bucket closing at the eval instant was not seen")
	}
}

// B-clock (h): the COLD key-level deletion test must treat a candle as closed by
// its SCHEDULED close. keyLevel1HBars stamps a candle with its last 1m bar's
// CloseTime, so a forming mid-hour candle would otherwise look closed at the
// close instant of that 1m bar and delete a level with a half-built body.
func TestClockFormingHourNeverDeletesALevel(t *testing.T) {
	day := ctMs(t, 0, 0, 0)
	at := func(h, m int) int64 { return day + int64(h*60+m)*60_000 }
	lvl := Level{Kind: KindKeyLevel, Price: 100, AtTime: at(8, 0)}
	forming := market.Kline{OpenTime: at(8, 30), Open: 99, High: 102, Low: 98, Close: 101, CloseTime: at(9, 10) + 59_999}
	last1m := clockBar(at(9, 10), 100, 101, 99, 101)
	if levelDeletedBy1HBody(lvl, []market.Kline{forming}, BarCloseInstant(last1m)) {
		t.Fatal("a forming 1H candle (09:10 of the 08:30–09:30 hour) deleted a level")
	}
	closed := forming
	closed.CloseTime = at(9, 29) + 59_999
	last1m = clockBar(at(9, 29), 100, 101, 99, 101)
	if !levelDeletedBy1HBody(lvl, []market.Kline{closed}, BarCloseInstant(last1m)) {
		t.Fatal("a 1H candle that closed at 09:30:00 did not delete the level it crossed")
	}
}
