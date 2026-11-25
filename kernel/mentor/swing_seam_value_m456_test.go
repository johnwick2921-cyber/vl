package mentor

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
)

// ── m456 round 2 (reviewer B): a roll seam must reach the 4h EMA34 as a FOLD ──
//
// Round 1 silenced the stale flag for seam minutes (the hole scan counted them
// present) but the line is built from the stitched series, which LACKS those
// minutes, so the bucket's close was still missing from the recurrence: false
// FRESH, up to 1.25 pt wrong for ~4 days on the real 09-26 -> 12-26 roll. The
// origin fix puts the seam bars (the head's stored bars the cut dropped, newest
// scale) into the series the swing line is aggregated from, so the line equals
// a full recompute on the union of every contract's stored minutes.

// seamUnionSeries builds a synthetic 1m tape (real CME calendar, a price that
// moves every minute) from..to, split into the stitched series (lacks the open
// bucket seamFrom..seamTo, as a roll cut does) and the seam bars (what the head
// stored there, priced +7 so the fold is measurable).
func seamUnionSeries(from, to, seamFrom, seamTo time.Time) (stitched, seam, union []market.Kline) {
	i := 0
	for m := from; m.Before(to); m = m.Add(time.Minute) {
		if !kernel.IsCMEOpen(m) {
			continue
		}
		i++
		p := 100 + 15*math.Sin(float64(i)/700) + float64(i)/900
		in := !m.Before(seamFrom) && m.Before(seamTo)
		if in {
			p += 7
		}
		ms := m.UnixMilli()
		k := market.Kline{OpenTime: ms, CloseTime: ms + 59_999, Open: p, High: p + 0.5, Low: p - 0.5, Close: p, Volume: 10}
		union = append(union, k)
		if in {
			seam = append(seam, k)
		} else {
			stitched = append(stitched, k)
		}
	}
	return
}

func TestSeedFull_RollSeamBarsAreFoldedIntoTheSwingLine(t *testing.T) {
	loc := ctime()
	sf, st := time.Date(2026, 9, 10, 1, 0, 0, 0, loc), time.Date(2026, 9, 10, 5, 0, 0, 0, loc)
	stitched, seam, union := seamUnionSeries(time.Date(2026, 9, 6, 17, 0, 0, 0, loc), time.Date(2026, 9, 16, 9, 0, 0, 0, loc), sf, st)
	if len(seam) < 200 {
		t.Fatalf("setup: seam bucket has only %d bars", len(seam))
	}
	now := time.Date(2026, 9, 16, 10, 0, 0, 0, loc).UnixMilli()

	ref := New(lrProdCfg())
	SeedFull(ref, union, keyLevel1HBars(union), now)
	if ref.State.Swing.LineStale {
		t.Fatalf("setup: the union tape is hole-free, got stale %v", ref.State.Swing.HoledBuckets)
	}

	// control: the stitched series alone is missing the bucket (a real hole).
	bare := New(lrProdCfg())
	SeedFull(bare, stitched, keyLevel1HBars(stitched), now)
	if !bare.State.Swing.LineStale || bare.State.Swing.EmaCount != ref.State.Swing.EmaCount-1 {
		t.Fatalf("control: without the seam bars the bucket is a hole and one fold short: stale=%v count %d vs %d", bare.State.Swing.LineStale, bare.State.Swing.EmaCount, ref.State.Swing.EmaCount)
	}

	for _, reseed := range []bool{false, true} {
		e := New(lrProdCfg())
		e.SetSeamBars(seam)
		if reseed {
			SeedFull(e, stitched, keyLevel1HBars(stitched), now)
			e.SetSeamBars(seam)
			ReseedState(e, stitched, keyLevel1HBars(stitched), now)
		} else {
			SeedFull(e, stitched, keyLevel1HBars(stitched), now)
		}
		sw := e.State.Swing
		if sw.LineStale || sw.EmaCount != ref.State.Swing.EmaCount || math.Abs(sw.Line-ref.State.Swing.Line) > 1e-9 {
			t.Fatalf("reseed=%v: the seeded line must equal the recompute on the union of stored minutes: line %.6f vs %.6f, count %d vs %d, stale=%v", reseed, sw.Line, ref.State.Swing.Line, sw.EmaCount, ref.State.Swing.EmaCount, sw.LineStale)
		}
	}
}

// The REAL 09-26 -> 12-26 roll (read-only export, VL_ROLL_REAL_DIR): at every
// seed instant across the roll the production stitch + seam seed equals the
// reference seeded on the union (stitched + the head's bars at the seam
// minutes), line AND fold count AND stale flag. Reviewer B measured max 1.2506 pt.
func TestM456RealRollSeamLineEqualsUnionRecompute(t *testing.T) {
	dir := os.Getenv("VL_ROLL_REAL_DIR")
	if dir == "" {
		t.Skip("VL_ROLL_REAL_DIR not set (read-only store export of MNQ 09-26 / 12-26)")
	}
	rd := func(c, tf string, ms int64) []market.Kline {
		return loadRollCSV(t, filepath.Join(dir, c+"_"+tf+".csv"), ms)
	}
	older := Contract1M{Contract: "MNQ 09-26", Bars: rd("MNQ_09-26", "1m", 60_000), Bars1H: rd("MNQ_09-26", "1h", 3_600_000)}
	newest := Contract1M{Contract: "MNQ 12-26", Bars: rd("MNQ_12-26", "1m", 60_000), Bars1H: rd("MNQ_12-26", "1h", 3_600_000)}
	s := NewRollStitcher(newest)
	if !s.Add(older) {
		t.Fatalf("stitch refused")
	}
	stitched, h1, _, _ := s.Result()
	headAt := map[int64]market.Kline{}
	for _, b := range newest.Bars {
		headAt[b.OpenTime] = b
	}
	var seam []market.Kline
	for _, ms := range s.SeamCovered() {
		seam = append(seam, headAt[ms])
	}
	union := append(append([]market.Kline(nil), stitched...), seam...)
	sort.Slice(union, func(i, j int) bool { return union[i].OpenTime < union[j].OpenTime })
	if got := s.SeamBars(); len(got) != len(seam) || !reflect.DeepEqual(got, seam) {
		t.Fatalf("SeamBars must be exactly the head's bars at the SeamCovered minutes: %d vs %d", len(got), len(seam))
	}
	t.Logf("stitched %d, seam %d, union %d", len(stitched), len(seam), len(union))
	if len(seam) == 0 {
		t.Fatal("setup: the real roll has seam minutes")
	}
	cut := func(in []market.Kline, nm int64) []market.Kline {
		return in[:sort.Search(len(in), func(k int) bool { return in[k].CloseTime+1 > nm })]
	}
	loc := ctime()
	n, worst := 0, 0.0
	for d := time.Date(2026, 9, 11, 17, 5, 0, 0, loc); d.Before(time.Date(2026, 10, 14, 0, 0, 0, 0, loc)); d = d.Add(157 * time.Minute) {
		if !kernel.IsCMEOpen(d) {
			continue
		}
		nm := d.UnixMilli()
		hist, u := cut(stitched, nm), cut(union, nm)
		ref := New(lrProdCfg())
		SeedFull(ref, u, keyLevel1HBars(u), nm)
		e := New(lrProdCfg())
		e.SetSeamBars(seam)
		SeedFull(e, hist, h1, nm)
		e3 := New(lrProdCfg())
		e3.SetSeamBars(seam)
		SeedFull(e3, hist, h1, nm)
		e3.SetSeamBars(seam)
		ReseedState(e3, hist, h1, nm)
		n++
		for tag, x := range map[string]*Evaluator{"seed": e, "reseed": e3} {
			dd := math.Abs(x.State.Swing.Line - ref.State.Swing.Line)
			if dd > worst {
				worst = dd
			}
			if dd > 1e-9 || x.State.Swing.EmaCount != ref.State.Swing.EmaCount || x.State.Swing.LineStale != ref.State.Swing.LineStale {
				t.Errorf("%s at %s: line %.4f vs union %.4f, count %d vs %d, stale %v vs %v", tag, kernel.FormatCT(d), x.State.Swing.Line, ref.State.Swing.Line, x.State.Swing.EmaCount, ref.State.Swing.EmaCount, x.State.Swing.LineStale, ref.State.Swing.LineStale)
			}
		}
	}
	t.Logf("seeds %d, worst |line - union| %.6f", n, worst)
}
