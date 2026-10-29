package checks

import (
	"testing"
	"time"
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

// D4: a bad print used to expire 30 minutes after it printed while it sat in
// the lookbacks for hours. It must keep entries paused for as long as it moves
// any indicator by more than that indicator's tolerance - and no longer.
func TestBadPrintHoldsWhileItStillInfluencesIndicators(t *testing.T) {
	mnq, es, rowOf := loadBad1019Long(t)
	specs := DefaultSpecs([]int{3, 5})
	prod := DefaultBadPrint().WithHold(specs)
	var badT int64
	for ts, id := range rowOf {
		if id == 2020732 {
			badT = ts
		}
	}
	x := 167.25 // |h-l| of row 2020732: 29855.0 - 29687.75
	hold := HoldMinutes(specs, x)
	if hold < 120 || hold > HoldCapMin {
		t.Fatalf("a 167 pt wick must hold for hours, bounded by the cap; got %d min", hold)
	}
	for _, off := range []int{1, 31, 60, 120, hold - 2} {
		now := time.UnixMilli(badT + int64(off)*minuteMs + 30_000)
		if suspectAt(mnq, es, rowOf, prod, now, 2020732) == nil {
			t.Errorf("row 2020732 (163 pt wick) must still pause %d min after it printed (hold %d min)", off, hold)
		}
	}
	// ... and not for ever: once its influence has decayed below every tolerance
	if now := time.UnixMilli(badT + int64(hold+2)*minuteMs + 30_000); suspectAt(mnq, es, rowOf, prod, now, 2020732) != nil {
		t.Errorf("row 2020732 must clear once its influence has decayed (%d min)", hold)
	}
	// the smaller 82 pt print (row 2020662) holds for less
	if h2 := HoldMinutes(specs, 82); h2 >= hold || h2 < 60 {
		t.Errorf("hold must scale with the error: 82 pt -> %d min, 167 pt -> %d min", h2, hold)
	}
	// the OLD age-based window (Hold == nil) clears at 30 min: that is the defect
	old := DefaultBadPrint()
	if now := time.UnixMilli(badT + 61*minuteMs); suspectAt(mnq, es, rowOf, old, now, 2020732) != nil {
		t.Fatal("fixture: the age-only rule is expected to have expired by 60 min")
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
