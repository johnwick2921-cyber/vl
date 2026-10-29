package checks

import (
	"testing"
	"time"

	"vl/internal/datatruth"
)

// bad_1019 extended 5 hours past the bad prints (rows from data.db, read-only):
// the 10-19 13:58Z (row 2020662, 82 pt) and 14:19Z (row 2020732, 163 pt) bad
// prints sit in the store as `live` rows and stayed in every range read that day.
func loadBad1019Long(t *testing.T) (mnq, es []Bar, rowOf map[int64]float64) {
	t.Helper()
	win := loadWindows(t)["bad_1019_0858"]
	var after struct {
		Mnq [][]any `json:"mnq"`
		Es  [][]any `json:"es"`
	}
	loadJSON(t, "bad_1019_after.json", &after)
	rowOf = map[int64]float64{}
	for _, r := range append(append([][]any{}, win.Mnq...), after.Mnq...) {
		rowOf[int64(f(r[0]))] = rowID(r)
	}
	return toBars(append(append([][]any{}, win.Mnq...), after.Mnq...)), toBars(append(append([][]any{}, win.Es...), after.Es...)), rowOf
}

func upTo(b []Bar, ms int64) []Bar {
	var o []Bar
	for _, x := range b {
		if x.T <= ms {
			o = append(o, x)
		}
	}
	return o
}

func suspectAt(mnq, es []Bar, rowOf map[int64]float64, p BadPrintParams, now time.Time, row float64) *Suspect {
	for _, s := range SuspectBars(upTo(mnq, now.UnixMilli()), upTo(es, now.UnixMilli()), now, p) {
		s := s
		if rowOf[s.T] == row {
			return &s
		}
	}
	return nil
}

// D4 + F1 (release #55 review): a bad print used to expire 30 minutes after it
// printed while it sat in the lookbacks for hours. It must keep entries paused
// for as long as it moves an indicator THAT READER READS by more than that
// indicator's tolerance, charged only the part of the error the indicator
// reads - and no longer. The first calibration charged the whole high/low
// excursion to EMA/RSI/MACD too: the 163 pt wick of row 2020732, which closes
// 7.5 pt from where the next bar opens, held 4 hours as a "163 pt close error".
func TestBadPrintHoldsWhileItStillInfluencesIndicators(t *testing.T) {
	mnq, es, rowOf := loadBad1019Long(t)
	specs := DefaultSpecs([]int{3, 5})
	// the close-reading indicators only (the F1 pricing: EMA / RSI / MACD read the
	// CLOSE, ATR the range); the mentor's raw high/low read (Extreme) is priced below
	var closeSpecs []SeriesSpec
	for _, sp := range specs {
		if !sp.Extreme {
			closeSpecs = append(closeSpecs, sp)
		}
	}
	prod := DefaultBadPrint().WithHold(specs)
	times := map[float64]int64{}
	for ts, id := range rowOf {
		times[id] = ts
	}
	// the two real bad prints: (row, range error X, close error)
	for _, c := range []struct {
		row            float64
		x, closeErr    float64
		aiHold, mentor int // minutes
	}{
		{2020662, 82.25, 69.25, 75, 37}, // 08:58: a real close error (the next bar opens 69 pt away)
		{2020732, 167.25, 7.5, 75, 6},   // 09:19: a wick that closes back; only ATR sees the range
	} {
		badT := times[c.row]
		be := BarError{T: badT, CloseErr: c.closeErr, RangeErr: c.x}
		if h := HoldMinutesFor(specs, datatruth.ReaderAI, be); h != c.aiHold {
			t.Errorf("row %v: AI hold %d min, want %d (ATR14 on the 3m/5m reads the range; EMA/RSI/MACD read the close)", c.row, h, c.aiHold)
		}
		if h := HoldMinutesFor(closeSpecs, datatruth.ReaderMentor, be); h != c.mentor {
			t.Errorf("row %v: mentor close-read hold %d min, want %d (the 1m EMA34/9 read the close)", c.row, h, c.mentor)
		}
		// review round 1, finding 2: the mentor ALSO reads raw 1m highs/lows (day gate,
		// rolling PHL, box, trendline, the 1m-built HTF candles), so a wrong high/low
		// (82 pt, 167 pt) holds it for the whole range-read window, whatever the close
		// error: the wick that closes back is no longer a 6 minute event for it.
		if h := HoldMinutesFor(specs, datatruth.ReaderMentor, be); h != MentorRangeReadMin {
			t.Errorf("row %v: mentor hold %d min, want %d (a wrong 1m high/low stays in what the mentor reads)", c.row, h, MentorRangeReadMin)
		}
		// the OLD charge (whole excursion to every spec) is the defect
		if old := HoldMinutes(specs, c.x); c.row == 2020732 && old < 200 {
			t.Fatalf("fixture: the pre-F1 charge of a %.0f pt wick is expected to be hours, got %d", c.x, old)
		}
		for _, off := range []int{1, 31, 60, c.aiHold - 2} {
			now := time.UnixMilli(badT + int64(off)*minuteMs + 30_000)
			s := suspectAt(mnq, es, rowOf, prod, now, c.row)
			if s == nil {
				t.Errorf("row %v must still pause the AI reader %d min after it printed (hold %d min)", c.row, off, c.aiHold)
				continue
			}
			if s.CloseErr != c.closeErr {
				t.Errorf("row %v CloseErr %v, want %v", c.row, s.CloseErr, c.closeErr)
			}
			// the mentor is held for the whole range-read window; past the AI path's own hold only it is
			wantMentor := off < MentorRangeReadMin
			gotMentor := len(s.Readers) == 0 || s.Readers[0] == datatruth.ReaderMentor
			if gotMentor != wantMentor {
				t.Errorf("row %v at +%d min: mentor held=%v, want %v (readers %v)", c.row, off, gotMentor, wantMentor, s.Readers)
			}
		}
		// ... the AI path is released once the bar's influence on its indicators has decayed ...
		if now := time.UnixMilli(badT + int64(c.aiHold+2)*minuteMs + 30_000); true {
			s := suspectAt(mnq, es, rowOf, prod, now, c.row)
			if s == nil || len(s.Readers) != 1 || s.Readers[0] != datatruth.ReaderMentor {
				t.Errorf("row %v at +%d min: only the mentor reader may still be held, got %+v", c.row, c.aiHold+2, s)
			}
		}
		// ... and nobody for ever: the mentor's window is bounded by the cap
		if now := time.UnixMilli(badT + int64(MentorRangeReadMin+2)*minuteMs + 30_000); suspectAt(mnq, es, rowOf, prod, now, c.row) != nil {
			t.Errorf("row %v must clear once the range-read window (%d min) has passed", c.row, MentorRangeReadMin)
		}
	}
	// the OLD age-based window (Hold == nil) clears at 30 min: that is the defect
	old := DefaultBadPrint()
	if now := time.UnixMilli(times[2020732] + 61*minuteMs); suspectAt(mnq, es, rowOf, old, now, 2020732) != nil {
		t.Fatal("fixture: the age-only rule is expected to have expired by 60 min")
	}
	// the raw high/low read sees ONLY the range error: a wick that closes exactly
	// back (close error 0) is held for the window, a wrong close on a right range
	// (range error 0) is not held by it, and 2 pt (ImpactTolPts) is the floor
	if h := HoldMinutesFor(specs, datatruth.ReaderMentor, BarError{CloseErr: 0, RangeErr: 60}); h != MentorRangeReadMin {
		t.Errorf("a 60 pt wick that closes back: mentor hold %d, want %d", h, MentorRangeReadMin)
	}
	if h := HoldMinutesFor([]SeriesSpec{{Name: "mentor_range_1m", TFMin: 1, Period: MentorRangeReadMin, Extreme: true, Reader: datatruth.ReaderMentor}},
		datatruth.ReaderMentor, BarError{CloseErr: 60, RangeErr: 0}); h != 0 {
		t.Errorf("a wrong close on a right range is not a raw high/low error: hold %d, want 0", h)
	}
	if h := HoldMinutesFor(specs, datatruth.ReaderMentor, BarError{CloseErr: 0, RangeErr: ImpactTolPts}); h != 0 {
		t.Errorf("a range error of exactly %.0f pt moves nothing by MORE than the tolerance: hold %d, want 0", ImpactTolPts, h)
	}
	// the hold still scales with the error: a bigger close error holds longer
	if a, b := HoldMinutesFor(closeSpecs, datatruth.ReaderMentor, BarError{CloseErr: 30, RangeErr: 30}), HoldMinutesFor(closeSpecs, datatruth.ReaderMentor, BarError{CloseErr: 69, RangeErr: 69}); a >= b {
		t.Errorf("hold must scale with the close error: 30 pt -> %d min, 69 pt -> %d min", a, b)
	}
	// a 1m close reaches a 5m close only on the LAST minute of the 5m bucket
	last := BarError{T: ct(t, "2026-10-19 08:59").UnixMilli(), CloseErr: 60, RangeErr: 60}
	mid := BarError{T: ct(t, "2026-10-19 08:58").UnixMilli(), CloseErr: 60, RangeErr: 60}
	ai5 := []SeriesSpec{{Name: "ai_ema20_5m", TFMin: 5, Period: 20, EMA: true, PriceUnits: true, TolPts: 1.0, Reader: datatruth.ReaderAI}}
	if HoldMinutesFor(ai5, datatruth.ReaderAI, last) == 0 || HoldMinutesFor(ai5, datatruth.ReaderAI, mid) != 0 {
		t.Errorf("a 1m close error must reach a 5m EMA only through the bucket's last minute: last=%d mid=%d",
			HoldMinutesFor(ai5, datatruth.ReaderAI, last), HoldMinutesFor(ai5, datatruth.ReaderAI, mid))
	}
}

// Real market moves still never pause with the long hold: every minute of the
// 10-19 stretch (13:10Z..19:30Z, news included), evaluated bar-by-bar at the
// newest-bar view, flags ONLY the two real bad prints.
func TestBadPrintLongHoldFlagsOnlyTheRealBadPrints(t *testing.T) {
	mnq, es, rowOf := loadBad1019Long(t)
	prod := DefaultBadPrint().WithHold(DefaultSpecs([]int{3, 5}))
	flagged := map[float64]bool{}
	for _, b := range mnq {
		now := time.UnixMilli(b.T + 30_000)
		for _, s := range SuspectBars(upTo(mnq, now.UnixMilli()), upTo(es, now.UnixMilli()), now, prod) {
			flagged[rowOf[s.T]] = true
		}
	}
	if len(flagged) != 2 || !flagged[2020662] || !flagged[2020732] {
		t.Fatalf("expected exactly rows 2020662 and 2020732, got %v", flagged)
	}
}

// D4: the 10-27 08:30 CT 5m bar (row 2045884, 56 pt wrong low) stays in the 5m
// EMA20/50, ATR14, RSI and MACD windows for hours. It used to stop pausing 12
// minutes after it closed; with the influence hold it pauses past 30 minutes.
func TestCrossFrameHoldsWhileTheWrongBarInfluencesIndicators(t *testing.T) {
	var fr frames
	loadJSON(t, "frames_1027.json", &fr)
	m1, h5 := toBars(fr.M1), toBars(fr.F5)
	prod := DefaultCross().WithHold(DefaultSpecs([]int{3, 5}))
	at := ct(t, "2026-10-27 08:30").UnixMilli()
	has := func(now time.Time, p CrossParams) bool {
		mm, _, _ := CompareFrames(m1, h5, 5, now, nil, p)
		for _, m := range mm {
			if m.T == at {
				return true
			}
		}
		return false
	}
	hold := HoldMinutes(DefaultSpecs([]int{5}), 56.5, 5)
	if hold < 60 {
		t.Fatalf("a 56 pt wrong 5m low must hold for over an hour, got %d min", hold)
	}
	for _, ev := range []string{"2026-10-27 08:36", "2026-10-27 08:50", "2026-10-27 09:09"} { // 1, 15 and 34 min after it closed
		if !has(ct(t, ev), prod) {
			t.Errorf("%s: the truncated 5m bar must still pause (hold %d min)", ev, hold)
		}
	}
	if has(ct(t, "2026-10-27 09:09"), DefaultCross()) {
		t.Fatal("fixture: the age-only rule is expected to have expired by 34 min")
	}
	// a sub-point tick skew has no lasting influence on any indicator
	if h := HoldMinutes(DefaultSpecs([]int{5}), 0.75, 5); h != 0 {
		t.Errorf("a 0.75 pt skew must hold only for the floor, got %d min", h)
	}
	if h := HoldMinutes(DefaultSpecs([]int{5}), 1.75, 5); h != 0 {
		t.Errorf("the largest corpus tick-skew (1.75 pt) must hold only for the floor, got %d min", h)
	}
	if h := HoldMinutes(DefaultSpecs([]int{5}), 9.75, 5); h > 30 {
		t.Errorf("a 9.75 pt error is below every tolerance but EMA20's brief tail, got %d min", h)
	}
	if h := HoldMinutes(DefaultSpecs([]int{5}), 16, 5); h <= 0 || h > 60 {
		t.Errorf("a 16 pt error moves RSI7 (1/7 per bar) for its 7-bar window and EMA20 for a few bars, got %d min", h)
	}
	if h := HoldMinutes(DefaultSpecs([]int{3, 5}), 56.5, 15); h != 0 {
		t.Errorf("a frame no indicator reads holds only for its own length, got %d min", h)
	}
}

// D4: a 15m bar is the newest 15m bar for 15 minutes; judging it for only 12
// let entries resume for 3 minutes while it was still wrong (a flap).
func TestCrossFrameJudgesAFrameForAtLeastItsOwnLength(t *testing.T) {
	var fr frames
	loadJSON(t, "frames_1027.json", &fr)
	m1, h15 := toBars(fr.M1), toBars(fr.F15)
	at := ct(t, "2026-10-27 08:30").UnixMilli() // closes 08:45
	flagged := func(ev string) bool {
		mm, _, _ := CompareFrames(m1, h15, 15, ct(t, ev), nil, DefaultCross())
		for _, m := range mm {
			if m.T == at {
				return true
			}
		}
		return false
	}
	if !flagged("2026-10-27 08:58") { // 13 min after the close: past the old 12-min window
		t.Error("the 15m bar is still the newest 15m bar 13 min after it closed: it must still be judged")
	}
	if flagged("2026-10-27 09:02") { // 17 min: no longer the newest, no Hold wired
		t.Error("no hold wired: it must stop at the frame length")
	}
}
