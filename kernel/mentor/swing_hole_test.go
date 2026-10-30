package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// m456 (release #55): the 4h EMA34 swing line was silently +0.42 pt off on
// 10-28 evening (+10.8 pt on the 10-27 morning) because the 1m store lacked
// 10-26 17:00-21:57 CT. fourHClosedBuckets / bucketClosesBetween skip a 4h
// bucket that has no bars ("a weekend, the halt") and cannot tell that from a
// DATA HOLE inside an open CME session, so the whole bucket's EMA fold vanished
// and the line stayed warm + tradeable. These pins drive the PRODUCTION call
// sites (SeedFull, ReseedState, SwingTick via Tick).

// holeSessionMinutes is a CME-shaped 1m tape (closed Fri 16:00 -> Sun 17:00 and
// the daily 16:00-17:00 break) whose price swings enough that a lost 4h close
// moves the EMA measurably. skip drops minutes (a data hole).
func holeSessionMinutes(from, to time.Time, skip func(t time.Time) bool) []market.Kline {
	var out []market.Kline
	i := 0
	for t := from; t.Before(to); t = t.Add(time.Minute) {
		wd, h := t.Weekday(), t.Hour()
		if wd == time.Saturday || (wd == time.Friday && h >= 16) || (wd == time.Sunday && h < 17) || h == 16 {
			continue
		}
		i++
		if skip != nil && skip(t) {
			continue
		}
		p := 29000 + 150*math.Sin(float64(i)/700) + 40*math.Sin(float64(i)/97)
		ot := t.UnixMilli()
		out = append(out, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: p, High: p + 1, Low: p - 1, Close: p, Final: true})
	}
	return out
}

func holeSeed(bars []market.Kline, now int64) *Evaluator {
	e := New(DefaultConfig())
	SeedFull(e, bars, keyLevel1HBars(bars), now)
	return e
}

func hole(lo, hi time.Time) func(time.Time) bool {
	return func(t time.Time) bool { return !t.Before(lo) && t.Before(hi) }
}

// TestSeedSwingLine_InteriorFourHHole_MarkedStale: the 10-26 shape (Monday
// evening 17:00-21:57 CT missing, 21:58 onward present).
func TestSeedSwingLine_InteriorFourHHole_MarkedStale(t *testing.T) {
	loc := ctime()
	from := time.Date(2026, 9, 27, 17, 0, 0, 0, loc)
	to := time.Date(2026, 10, 8, 10, 30, 0, 0, loc)
	now := to.UnixMilli()
	lo := time.Date(2026, 10, 5, 17, 0, 0, 0, loc)
	hi := time.Date(2026, 10, 5, 21, 58, 0, 0, loc)
	full := holeSeed(holeSessionMinutes(from, to, nil), now)
	holed := holeSeed(holeSessionMinutes(from, to, hole(lo, hi)), now)
	if full.State.Swing.LineStale {
		t.Fatalf("control: a complete series (weekend + daily halts) must not be stale")
	}
	diff := holed.State.Swing.Line - full.State.Swing.Line
	if math.Abs(diff) < 0.05 {
		t.Fatalf("fixture too weak: %.4f", diff)
	}
	if !holed.State.Swing.LineStale {
		t.Errorf("seeded swing line silently %+.3f pts off (EmaCount %d vs %d) and not marked stale", diff, holed.State.Swing.EmaCount, full.State.Swing.EmaCount)
	}
	if line := holed.SeedLine(0); !containsStr(line, "swing line STALE: no bars in open 4h bucket 2026-10-05 17:00 CT") {
		t.Errorf("the boot line must name the holed bucket: %q", line)
	}
	if line := full.SeedLine(0); containsStr(line, "STALE") {
		t.Errorf("a complete series must not print STALE: %q", line)
	}
}

// A hole whose 4h bucket is far behind the line (older than the horizon) has
// decayed to nothing: it must NOT wedge swing entries until the next restart.
func TestSeedSwingLine_AncientHole_NotStale(t *testing.T) {
	loc := ctime()
	from := time.Date(2026, 8, 3, 17, 0, 0, 0, loc)
	to := time.Date(2026, 10, 8, 10, 30, 0, 0, loc)
	lo := time.Date(2026, 8, 5, 17, 0, 0, 0, loc)
	hi := time.Date(2026, 8, 5, 21, 58, 0, 0, loc)
	e := holeSeed(holeSessionMinutes(from, to, hole(lo, hi)), to.UnixMilli())
	if e.State.Swing.LineStale {
		t.Errorf("a hole >%d buckets behind the line is below 0.01 pt of weight; it must not mark the line stale", swingHoleHorizonBuckets(DefaultConfig().Swing.EMAPeriod))
	}
}

// Holidays (calendar CLOSED) and the weekend are not data holes.
func TestSeedSwingLine_ClosedHolidayBucket_NotStale(t *testing.T) {
	loc := ctime()
	// Thanksgiving 2026-11-26 (calendar row: closed or shortened); drop its whole day
	// of bars the way the exchange does.
	from := time.Date(2026, 11, 15, 17, 0, 0, 0, loc)
	to := time.Date(2026, 12, 1, 10, 30, 0, 0, loc)
	skip := hole(time.Date(2026, 11, 26, 0, 0, 0, 0, loc), time.Date(2026, 11, 26, 17, 0, 0, 0, loc))
	bars := holeSessionMinutes(from, to, skip)
	e := holeSeed(bars, to.UnixMilli())
	if e.State.Swing.LineStale {
		t.Errorf("Thanksgiving is a calendar-closed day, not a data hole — the line must stay tradeable")
	}
}

// ReseedState (the hole-fill re-seed) recomputes the mark: a refilled store
// clears it, a still-holed store keeps it.
func TestReseedState_SwingStaleFollowsTheStore(t *testing.T) {
	loc := ctime()
	from := time.Date(2026, 9, 27, 17, 0, 0, 0, loc)
	to := time.Date(2026, 10, 8, 10, 30, 0, 0, loc)
	now := to.UnixMilli()
	lo := time.Date(2026, 10, 5, 17, 0, 0, 0, loc)
	hi := time.Date(2026, 10, 5, 21, 58, 0, 0, loc)
	holedBars := holeSessionMinutes(from, to, hole(lo, hi))
	fullBars := holeSessionMinutes(from, to, nil)
	e := holeSeed(holedBars, now)
	if !e.State.Swing.LineStale {
		t.Fatalf("precondition: the holed seed must be stale")
	}
	ReseedState(e, holedBars, keyLevel1HBars(holedBars), now)
	if !e.State.Swing.LineStale {
		t.Errorf("a re-seed from a still-holed store must stay stale")
	}
	rep := ReseedState(e, fullBars, keyLevel1HBars(fullBars), now)
	if e.State.Swing.LineStale {
		t.Errorf("a re-seed from the refilled store must clear the stale mark")
	}
	want := holeSeed(fullBars, now).State.Swing.Line
	if math.Abs(e.State.Swing.Line-want) > 1e-9 {
		t.Errorf("refilled re-seed line %.6f != fresh seed %.6f (report %+v)", e.State.Swing.Line, want, rep.After)
	}
}

// The live flip: a warm line that flips across a 4h bucket which has NO bars
// during an open session (feed hole inside the slice) must fail closed instead
// of folding nothing for it.
func TestSwingFlip_MissingOpenBucketInSlice_MarksStale(t *testing.T) {
	full := swingGapTape(t, 12)
	var bars []market.Kline
	lo, hi := ctMs(t, 7, 17, 0), ctMs(t, 7, 21, 0) // Thu 10-08 17:00-21:00 CT bucket
	for _, b := range full {
		if b.OpenTime >= lo && b.OpenTime < hi {
			continue
		}
		bars = append(bars, b)
	}
	seedAt := ctMs(t, 7, 8, 0)
	e := New(lrProdCfg())
	Seed(e, closedOnly(bars, seedAt), seedAt)
	if e.State.Swing.EmaCount < FourHEMA34Min {
		t.Fatalf("control: the seeded swing line must be warm (%d closes)", e.State.Swing.EmaCount)
	}
	if e.State.Swing.LineStale {
		t.Fatalf("control: the seed ends before the hole")
	}
	swingTickRange(e, bars, seedAt, ctMs(t, 8, 2, 0))
	if !e.State.Swing.LineStale {
		t.Errorf("the flip across the 17:00-21:00 CT bucket with no bars must mark the line stale (line %.2f, %d closes)", e.State.Swing.Line, e.State.Swing.EmaCount)
	}
}
