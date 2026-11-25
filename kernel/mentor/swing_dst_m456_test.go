package mentor

import (
	"math"
	"sort"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
)

// m456 (release #56) — the EMA34 stale walker vs the DST-short 4h buckets.
//
// holedFourHBuckets walked buckets as e = s + 4h in REAL ms, but the 17:00-CT
// anchored kernel buckets are not all 4h long: on the fall-back Saturday the
// session's last bucket starts 16:00 CST and is ONE hour long (the 17:00 CST
// anchor cuts it); on the spring-forward day the last bucket is three hours.
// The walker read [16:00,20:00) as one bucket, so with the tape at Sun 17:05 CT
// the 17:00-17:05 bars made "covered" stop at 17:05 and the 17:05-20:00 OPEN
// time counted as an uncovered tail: a hole on a hole-free tape.
//
// These pins drive the PRODUCTION call sites (SeedFull and the live
// Tick -> SwingTick -> bucketClosesBetween flip) on a hole-free tape that
// follows the REAL CME calendar (kernel.IsCMEOpen: weekends, the daily break,
// holidays), across the 2026-11-01 fall-back and the 2027-03-14 spring-forward.

// calTape is a hole-free 1m tape on the real CME calendar.
func calTape(from, to time.Time) []market.Kline {
	var out []market.Kline
	i := 0
	for t := from; t.Before(to); t = t.Add(time.Minute) {
		if !kernel.IsCMEOpen(t) {
			continue
		}
		i++
		p := 29000 + 150*math.Sin(float64(i)/700) + 40*math.Sin(float64(i)/97)
		ot := t.UnixMilli()
		out = append(out, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: p, High: p + 1, Low: p - 1, Close: p, Final: true})
	}
	return out
}

// dstWindows are (tape start, probe from, probe to) around each DST change:
// the tape begins well over 34 4h closes before the probe so the line is warm.
var dstWindows = []struct {
	name           string
	tape0, tape1   time.Time
	probe0, probe1 time.Time
}{
	{"fall-back 2026-11-01", time.Date(2026, 9, 20, 17, 0, 0, 0, time.UTC), time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)},
	{"spring-forward 2027-03-14", time.Date(2027, 1, 24, 17, 0, 0, 0, time.UTC), time.Date(2027, 3, 21, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 3, 13, 12, 0, 0, 0, time.UTC), time.Date(2027, 3, 16, 8, 0, 0, 0, time.UTC)},
}

// ctWall re-reads a UTC-built wall clock as America/Chicago (the windows above
// are written as plain dates; the CT instant is what the bot sees).
func ctWall(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, ctime())
}

func TestSwingSeed_DSTBoundaries_HoleFreeTape_NeverStale(t *testing.T) {
	for _, w := range dstWindows {
		all := calTape(ctWall(w.tape0), ctWall(w.tape1))
		n, bad := 0, 0
		for now := ctWall(w.probe0); now.Before(ctWall(w.probe1)); now = now.Add(29 * time.Minute) {
			nm := now.UnixMilli()
			hist := all[:sort.Search(len(all), func(k int) bool { return all[k].CloseTime+1 > nm })]
			e := New(lrProdCfg())
			SeedFull(e, hist, keyLevel1HBars(hist), nm)
			n++
			if e.State.Swing.LineStale {
				bad++
				if bad <= 3 {
					t.Errorf("%s: the SEED marked a hole-free tape STALE at %s (holed %v)", w.name, kernel.FormatCT(now), e.State.Swing.HoledBuckets)
				}
			}
		}
		t.Logf("%s: %d seeds, %d stale", w.name, n, bad)
	}
}

func TestSwingTick_DSTBoundaries_HoleFreeTape_NeverStale(t *testing.T) {
	for _, w := range dstWindows {
		all := calTape(ctWall(w.tape0), ctWall(w.tape1))
		a, b := ctWall(w.probe0).UnixMilli(), ctWall(w.probe1).UnixMilli()
		start := 0
		for start < len(all) && all[start].CloseTime+1 <= a {
			start++
		}
		e := New(lrProdCfg())
		hist := all[:start]
		SeedFull(e, hist, keyLevel1HBars(hist), a)
		if e.State.Swing.LineStale {
			t.Fatalf("%s: control: the seed before the window is stale", w.name)
		}
		stale := 0
		for i := start; i < len(all); i++ {
			now := all[i].CloseTime + 1
			if now > b {
				break
			}
			if (now/60000)%5 != 0 {
				continue
			}
			lw := i + 1 - mentorTickWindow
			if lw < 0 {
				lw = 0
			}
			e.Tick(all[lw:i+1], now)
			if e.State.Swing.LineStale {
				stale++
				if stale == 1 {
					t.Errorf("%s: the LIVE flip went STALE on a hole-free tape at %s (bucket %s)", w.name, kernel.FormatCT(time.UnixMilli(now)), kernel.FormatCT(time.UnixMilli(e.State.Swing.BucketStart)))
				}
			}
		}
	}
}
