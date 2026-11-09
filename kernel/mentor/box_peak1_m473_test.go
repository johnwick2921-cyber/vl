package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// m473 B2 — FTGH/FTGL construction: two same-role peaks pair when level
// (a horizontal line from peak 1 touches peak 2), and the box anchors on
// PEAK 1 even when peak 2 has the higher wick.
// "Nếu đỉnh 2 có râu cao hơn ... mình sẽ bay theo đỉnh 1, cái chuyện đó là chuyện
// bắt buộc ... Tui lấy cái râu đỉnh 1 làm chuẩn" [Oct D3.3 @ 19:31] (OctD3.3-R30);
// level test = a horizontal line, not the eye [Oct D3.3 @ 14:20] (R23); FTGL is
// the mirror [Oct D3.3 @ 43:14] (R54). Frames: /home/hoang/mm-course/oct/scenes/
// Ngày 3.3 FTGH FTGL/s_00882.jpg (Fri 30 Oct 1m: peak 1 ~08:00 high 28,817.25,
// horizontal line tagged at 28,817.25 touching peak 2 ~08:29), s_01267.jpg.

// m473PeakTape builds a 1m tape from 09:00 CT Fri 30 Oct 2026 whose highs are
// h[i], lows 28800 (range 8-20 pts, so flatNearPts is about 5-6).
func m473PeakTape(highs []float64) ([]market.Kline, time.Time) {
	t0 := time.Date(2026, time.October, 30, 9, 0, 0, 0, ctime())
	var out []market.Kline
	for i, h := range highs {
		ot := t0.UnixMilli() + int64(i)*60_000
		out = append(out, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: 28803, High: h, Low: 28800, Close: 28804})
	}
	return out, time.UnixMilli(out[len(out)-1].OpenTime + 60_000).In(ctime())
}

func m473FTGH(boxes []Box) *Box {
	for i := range boxes {
		if boxes[i].Kind == FTGH {
			b := boxes[i]
			return &b
		}
	}
	return nil
}

// TestM473B2Peak2HigherWickBoxAnchorsOnPeak1 — peak 1 28,817.25 (idx 3), peak 2
// 28,820 (idx 8) is 2.75 higher yet level (< flatNearPts): the box top is peak 1's
// wick 28,817.25, not 28,820.
func TestM473B2Peak2HigherWickBoxAnchorsOnPeak1(t *testing.T) {
	withFlatNear(t)
	bars, now := m473PeakTape([]float64{28808, 28809, 28810, 28817.25, 28812, 28810, 28811, 28813, 28820, 28814, 28812})
	b := m473FTGH(BoxesBuild(bars, DefaultBoxCfg(), now))
	if b == nil {
		t.Fatal("level peaks (2.75 apart) must form an FTGH box")
	}
	if b.Top != 28817.25 {
		t.Fatalf("FTGH top = %v, want peak 1's wick 28817.25 (peak 1 is the standard)", b.Top)
	}
}

// TestM473B2PeakOneHigherStillPeak1 (regression guard, passes on dev) — mirror order: peak 1 higher, peak 2 level
// below: anchored on peak 1 as before.
func TestM473B2PeakOneHigherStillPeak1(t *testing.T) {
	withFlatNear(t)
	bars, now := m473PeakTape([]float64{28808, 28809, 28810, 28820, 28812, 28810, 28811, 28813, 28817.25, 28814, 28812})
	b := m473FTGH(BoxesBuild(bars, DefaultBoxCfg(), now))
	if b == nil || b.Top != 28820 {
		t.Fatalf("FTGH = %+v, want top 28820 (peak 1)", b)
	}
}

// TestM473B2UnlevelPeaksNoBox — REGRESSION GUARD (passes on dev too; the
// red-first proof is TestM473B2PeakTwoLowerButUnlevelNoBox): peak 2 is 15 pts
// above peak 1: the horizontal line from peak 1 does not touch it, so no box.
func TestM473B2UnlevelPeaksNoBox(t *testing.T) {
	withFlatNear(t)
	bars, now := m473PeakTape([]float64{28808, 28809, 28810, 28817.25, 28812, 28810, 28811, 28813, 28832.25, 28814, 28812})
	if b := m473FTGH(BoxesBuild(bars, DefaultBoxCfg(), now)); b != nil {
		t.Fatalf("peaks 15 pts apart are not level, got box %+v", b)
	}
}

// TestM473B2FTGLMirrorAnchorsOnBottom1 — bottom 2 has the LOWER wick but is
// level: box bottom is bottom 1's wick. [Oct D3.3 @ 43:14] (R54).
func TestM473B2FTGLMirrorAnchorsOnBottom1(t *testing.T) {
	withFlatNear(t)
	t0 := time.Date(2026, time.October, 30, 9, 0, 0, 0, ctime())
	lows := []float64{28812, 28811, 28810, 28807.25, 28808, 28810, 28809, 28807, 28804.5, 28806, 28808}
	var bars []market.Kline
	for i, l := range lows {
		ot := t0.UnixMilli() + int64(i)*60_000
		bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: l + 4, High: l + 10, Low: l, Close: l + 3})
	}
	now := time.UnixMilli(bars[len(bars)-1].OpenTime + 60_000).In(ctime())
	var got *Box
	for _, b := range BoxesBuild(bars, DefaultBoxCfg(), now) {
		if b.Kind == FTGL {
			bb := b
			got = &bb
		}
	}
	if got == nil || got.Bottom != 28807.25 {
		t.Fatalf("FTGL = %+v, want bottom 28807.25 (bottom 1)", got)
	}
}

// TestM473B2LevelBoundaryIsFlatNearPts — the pair is level exactly when
// |p1-p2| <= flatNearPts (owner R2 tolerance; the video states only "a
// horizontal line touches peak 2", no number [Oct D3.3 @ 14:20] R23). Sweeps
// peak 2 up a 0.25 grid and checks accept == (d <= tol) at every step, so the
// boundary (== accepted, + one tick rejected) is pinned, not only far points.
func TestM473B2LevelBoundaryIsFlatNearPts(t *testing.T) {
	withFlatNear(t)
	sawAccept, sawReject := false, false
	for d := 0.0; d <= 14; d += 0.25 {
		bars, now := m473PeakTape([]float64{28808, 28809, 28810, 28817.25, 28812, 28810, 28811, 28813, 28817.25 + d, 28814, 28812})
		tol := flatNearPts(bars, 8)
		got := m473FTGH(BoxesBuild(bars, DefaultBoxCfg(), now)) != nil
		if got != (d <= tol) {
			t.Fatalf("d=%.2f tol=%.4f: box=%v want %v", d, tol, got, d <= tol)
		}
		sawAccept = sawAccept || got
		sawReject = sawReject || !got
	}
	if !sawAccept || !sawReject {
		t.Fatalf("sweep must cross the boundary (accept=%v reject=%v)", sawAccept, sawReject)
	}
}

// TestM473B2VideoBarSizePairIsLevel — bars the size of the on-screen 1m bars
// before the open (frame s_00882: bodies+wicks ~10-20 pts at ~20 pts/28 px),
// peak 1 28,817.25 and peak 2 28,820 (R23, R28: ~2.75 apart) are level [B: the
// bar height is read off the frame, not from data].
func TestM473B2VideoBarSizePairIsLevel(t *testing.T) {
	withFlatNear(t)
	t0 := time.Date(2026, time.October, 30, 8, 0, 0, 0, ctime())
	highs := []float64{28800, 28805, 28817.25, 28808, 28803, 28800, 28806, 28804, 28802, 28805, 28810, 28820, 28812, 28806}
	var bars []market.Kline
	for i, h := range highs {
		ot := t0.UnixMilli() + int64(i)*60_000
		bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: h - 6, High: h, Low: h - 12, Close: h - 5})
	}
	now := time.UnixMilli(bars[len(bars)-1].OpenTime + 60_000).In(ctime())
	b := m473FTGH(BoxesBuild(bars, DefaultBoxCfg(), now))
	if b == nil || b.Top != 28817.25 {
		t.Fatalf("FTGH = %+v, want top 28817.25 (peak 1) from the video-sized tape", b)
	}
}

// TestM473B2FTGLUnlevelNoBox — mirror of the unlevel FTGH case.
func TestM473B2FTGLUnlevelNoBox(t *testing.T) {
	withFlatNear(t)
	t0 := time.Date(2026, time.October, 30, 9, 0, 0, 0, ctime())
	lows := []float64{28812, 28811, 28810, 28807.25, 28808, 28810, 28809, 28807, 28792, 28806, 28808}
	var bars []market.Kline
	for i, l := range lows {
		ot := t0.UnixMilli() + int64(i)*60_000
		bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: l + 4, High: l + 10, Low: l, Close: l + 3})
	}
	now := time.UnixMilli(bars[len(bars)-1].OpenTime + 60_000).In(ctime())
	for _, b := range BoxesBuild(bars, DefaultBoxCfg(), now) {
		if b.Kind == FTGL {
			t.Fatalf("bottoms 15 pts apart are not level, got FTGL %+v", b)
		}
	}
}

// TestM473B2PeakTwoLowerButUnlevelNoBox — RED on the pre-m473 rule: the legacy
// "extreme + first later swing that does not exceed it" would box peak 1 28,832
// with peak 2 28,817 (15 below, not level). A horizontal line from peak 1 does
// not touch peak 2 [Oct D3.3 @ 14:20] (R23), so the new rule makes no box.
func TestM473B2PeakTwoLowerButUnlevelNoBox(t *testing.T) {
	withFlatNear(t)
	highs := []float64{28808, 28809, 28810, 28832, 28812, 28810, 28811, 28813, 28817, 28812, 28810}
	bars, now := m473PeakTape(highs)
	if b := m473FTGH(BoxesBuild(bars, DefaultBoxCfg(), now)); b != nil {
		t.Fatalf("peaks 15 pts apart are not level, got box %+v", b)
	}
	old := boxPeak1Level
	boxPeak1Level = false
	defer func() { boxPeak1Level = old }()
	if m473FTGH(BoxesBuild(bars, DefaultBoxCfg(), now)) == nil {
		t.Fatalf("control: the legacy construction boxes this tape, so the test discriminates")
	}
}

// TestM473B2EarlyTrivialPairDoesNotBeatTheRealPeaks — an early level pair of
// small swings (28,808 twice) precedes the prominent level peaks 28,830 /
// 28,831. The video never says "first swing in time" (round-2 video lens); the
// day's extreme anchors, so the box is the prominent peaks (peak 1 = 28,830).
func TestM473B2EarlyTrivialPairDoesNotBeatTheRealPeaks(t *testing.T) {
	withFlatNear(t)
	highs := []float64{28804, 28805, 28808, 28806, 28805, 28808, 28806, 28810, 28830, 28812, 28813, 28814, 28831, 28812, 28810}
	bars, now := m473PeakTape(highs)
	b := m473FTGH(BoxesBuild(bars, DefaultBoxCfg(), now))
	if b == nil || b.Top != 28830 {
		t.Fatalf("FTGH = %+v, want the prominent peaks (top 28830, peak 1), not the early 28808 pair", b)
	}
}
