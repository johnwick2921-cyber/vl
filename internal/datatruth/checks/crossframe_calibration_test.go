package checks

import (
	"testing"
	"time"

	"vl/internal/datatruth"
)

// Release #55 review F1-F3: cross_frame calibration on the REAL rows (MNQ 12-26,
// read-only export of data/data.db on 2026-10-29, rowids in the fixture).

type calWindow struct {
	M1  [][]any `json:"m1"`
	F3  [][]any `json:"3m"`
	F5  [][]any `json:"5m"`
	F15 [][]any `json:"15m"`
}

func loadCal(t *testing.T, name string) (m1 []Bar, htf map[int][]Bar) {
	t.Helper()
	var all map[string]calWindow
	loadJSON(t, "frames_calibration_1029.json", &all)
	w, ok := all[name]
	if !ok {
		t.Fatalf("no window %q", name)
	}
	return toBars(w.M1), map[int][]Bar{3: toBars(w.F3), 5: toBars(w.F5), 15: toBars(w.F15)}
}

func calProd() CrossParams { return DefaultCross().WithHold(DefaultSpecs([]int{3, 5})) }

// compareAll runs the production CompareFrames over the 3/5/15 frames.
func compareAll(m1 []Bar, htf map[int][]Bar, now time.Time, p CrossParams) []FrameMismatch {
	var out []FrameMismatch
	for _, tf := range []int{3, 5, 15} {
		mm, _, _ := CompareFrames(m1, htf[tf], tf, now, nil, p)
		out = append(out, mm...)
	}
	return out
}

func counted(t *testing.T) int64 { return datatruth.Snapshot()[CheckCrossFrame].Counted }

// F2: closed live frames that differ from the 1m by a few ticks (the documented
// tick-boundary skew) are RECORDED (counted, visible on /api/health) but pause
// nobody: no indicator moves by more than its own tolerance. Real rows:
// 10-21 12:40 CT 5m low +0.75 pt, 10-22 00:00 CT 3m/5m high +1.75 pt,
// 10-29 15:33 CT 3m close 2.00 pt.
//
// MUTANT C1: DefaultCross().PauseTol = 0 (every recorded price mismatch pauses) -> RED.
func TestCrossFrameTickSkewIsCountedNotPaused(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	datatruth.Register(CheckCrossFrame, func(string, time.Time) (bool, string, error) { return false, "", nil })
	cases := []struct {
		win, eval string
	}{
		{"skew_1021_1240", "2026-10-21 12:46"}, // 5m 12:40 low +0.75
		{"skew_1022_0000", "2026-10-22 00:07"}, // 3m 00:00 + 5m 00:00 high +1.75
		{"skew_1029_1533", "2026-10-29 15:37"}, // 3m 15:33 close +2.00 (not MORE than 2)
	}
	for _, c := range cases {
		m1, htf := loadCal(t, c.win)
		before := counted(t)
		// every minute across the skew bar's whole floor window: never a pause
		for off := 0; off <= 10; off++ {
			now := ct(t, c.eval).Add(time.Duration(off-3) * time.Minute)
			if mm := compareAll(m1, htf, now, calProd()); len(mm) != 0 {
				t.Fatalf("%s @%s: tick-skew paused: %+v", c.win, now.Format("15:04"), mm)
			}
		}
		if counted(t) == before {
			t.Errorf("%s: the skew record was neither paused NOR counted - an immaterial mismatch must stay visible", c.win)
		}
	}
	// the same data with the pre-#55 threshold pauses: the calibration is the only difference
	old := calProd()
	old.PauseTol = 0
	m1, htf := loadCal(t, "skew_1021_1240")
	if mm := compareAll(m1, htf, ct(t, "2026-10-21 12:46"), old); len(mm) == 0 {
		t.Fatal("fixture: PauseTol=0 must reproduce the pre-#55 pause on the 0.75 pt skew")
	}
}

// F2: 15m volume-only mismatch (price equal to the tick, native volume 2-4x the
// 1m-built overnight - 10-19 20:00 CT 27321 vs 6852) is counted, not paused; the
// same volume error on a 3m/5m bar still pauses.
//
// MUTANT C2: DefaultCross().VolPauseTFs = nil (volume pauses on every frame) -> RED.
func TestCrossFrame15mVolumeOnlyIsCountedNotPaused(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	datatruth.Register(CheckCrossFrame, func(string, time.Time) (bool, string, error) { return false, "", nil })
	m1, htf := loadCal(t, "vol15_1019_2000")
	var bar15 Bar
	for _, b := range htf[15] {
		if b.T == ct(t, "2026-10-19 20:00").UnixMilli() {
			bar15 = b
		}
	}
	if bar15.V < 25000 {
		t.Fatalf("fixture: the 10-19 20:00 CT 15m bar should carry the inflated 27321 volume, got %v", bar15.V)
	}
	for _, ev := range []string{"2026-10-19 20:16", "2026-10-19 20:25", "2026-10-19 20:31"} {
		if mm := compareAll(m1, htf, ct(t, ev), calProd()); len(mm) != 0 {
			t.Errorf("%s: a volume-only 15m mismatch paused: %+v", ev, mm)
		}
	}
	if counted(t) == 0 {
		t.Error("the 15m volume-only record must be counted")
	}
	// a 5m bar with the same volume problem DOES pause
	h5 := append([]Bar(nil), htf[5]...)
	at := ct(t, "2026-10-19 20:00").UnixMilli()
	for i := range h5 {
		if h5[i].T == at {
			h5[i].V = 3
		}
	}
	mm := compareAll(m1, map[int][]Bar{3: htf[3], 5: h5, 15: htf[15]}, ct(t, "2026-10-19 20:06"), calProd())
	found := false
	for _, m := range mm {
		found = found || (m.TFMin == 5 && m.T == at)
	}
	if !found {
		t.Fatalf("a 5m volume mismatch must still pause: %+v", mm)
	}
	// volume-pausing everywhere (the pre-#55 setting) pauses the 15m too
	old := calProd()
	old.VolPauseTFs = nil
	if mm := compareAll(m1, htf, ct(t, "2026-10-19 20:16"), old); len(mm) == 0 {
		t.Fatal("fixture: VolPauseTFs=nil must reproduce the pre-#55 15m volume pause")
	}
}

// F3: the 10-29 08:15-08:30 CT event, on the real rows. The 1m is complete for
// 08:15-08:29 and the native 15m (row 2058087), 5m (2058088) and 3m (2058086) are
// SNAPSHOTS of the forming bar that stop at ~08:27: close 28.5 pt off, volume
// 6523 vs 15501 (the ingest queue was pinned at 4096/4096 and dropping intrabar
// frames). That is INCORRECT data in the frames the AI path reads, so the AI
// reader must pause - and the mentor reader too, but only because the 5m's range
// (28.0 native vs 54.5 built) flips its strong-day test across 50 pt; never for
// the 3m / 15m it does not read.
func TestCrossFrame1029OpenTruncationScopesToReaders(t *testing.T) {
	m1, htf := loadCal(t, "trunc_1029_0815")
	// at 08:31 (1 min after the 08:30 close of the 08:25 5m)
	mm := compareAll(m1, htf, ct(t, "2026-10-29 08:31"), calProd())
	var ai, mentor, all []string
	for _, m := range mm {
		d := m.Detail
		switch {
		case len(m.Readers) == 0:
			all = append(all, d)
		case m.Readers[0] == datatruth.ReaderMentor:
			mentor = append(mentor, d)
		default:
			ai = append(ai, d)
		}
	}
	if len(mm) == 0 {
		t.Fatal("the truncated native bars must be flagged at the open")
	}
	// the 5m is the only frame the mentor reads, and it flips the strong-day test
	got5 := false
	for _, m := range mm {
		if m.TFMin == 5 && m.Concerns(datatruth.ReaderMentor) {
			got5 = true
		}
		if m.TFMin != 5 && m.Concerns(datatruth.ReaderMentor) && len(m.Readers) != 0 {
			t.Errorf("%dm mismatch must not concern the mentor reader: %+v", m.TFMin, m)
		}
	}
	if !got5 {
		t.Errorf("the 5m truncation flips the mentor's strong-day test and must concern it: ai=%d mentor=%d all=%d", len(ai), len(mentor), len(all))
	}
	// the AI path stays held until the wrong 5m bar's influence has decayed; the
	// 08:25 5m closed 08:30: 75 min (ATR14, 28.5/14 > 2) -> still held at 09:44, clear at 09:46
	if mm := compareAll(m1, htf, ct(t, "2026-10-29 09:44"), calProd()); !anyConcerns(mm, datatruth.ReaderAI) {
		t.Errorf("AI reader must still be held 74 min after the truncated 5m closed")
	}
	if mm := compareAll(m1, htf, ct(t, "2026-10-29 09:46"), calProd()); anyConcerns(mm, datatruth.ReaderAI) {
		t.Errorf("AI reader must be released once the 5m bar's influence has decayed: %+v", mm)
	}
	// the mentor reads the bar for its 12 closed 5m bars: held to 09:30, released after
	if mm := compareAll(m1, htf, ct(t, "2026-10-29 09:25"), calProd()); !anyConcerns(mm, datatruth.ReaderMentor) {
		t.Errorf("mentor reader must be held while the bar is inside its 12-bar strong-day window")
	}
	if mm := compareAll(m1, htf, ct(t, "2026-10-29 09:35"), calProd()); anyConcerns(mm, datatruth.ReaderMentor) {
		t.Errorf("mentor reader must be released once the bar left the 12-bar window: %+v", mm)
	}
}

func anyConcerns(mm []FrameMismatch, reader string) bool {
	for _, m := range mm {
		if (&m).Concerns(reader) {
			return true
		}
	}
	return false
}

// Concerns mirrors datatruth.Reason.Concerns for a frame mismatch.
func (m *FrameMismatch) Concerns(reader string) bool {
	return datatruth.Reason{Readers: m.Readers}.Concerns(reader)
}

// A mismatch on a frame the mentor never reads, or one that does not flip its
// strong-day test, is not the mentor's pause - the AI path's alone.
//
// MUTANT C3: the mentor reader is charged every mismatch (scoped := false path
// removed) -> RED.
func TestCrossFrameMentorIsOnlyHeldByAStrongDayFlip(t *testing.T) {
	m1, htf := loadCal(t, "skew_1022_0000")
	// make the 00:00 5m close 30 pt wrong (material to the AI path) without
	// changing its range across 50 pt: the mentor must NOT be held
	h5 := append([]Bar(nil), htf[5]...)
	at := ct(t, "2026-10-22 00:00").UnixMilli()
	for i := range h5 {
		if h5[i].T == at {
			h5[i].C += 30
			h5[i].H += 30
		}
	}
	mm := compareAll(m1, map[int][]Bar{3: htf[3], 5: h5, 15: htf[15]}, ct(t, "2026-10-22 00:06"), calProd())
	if !anyConcerns(mm, datatruth.ReaderAI) {
		t.Fatalf("a 30 pt wrong 5m close must hold the AI reader: %+v", mm)
	}
	if anyConcerns(mm, datatruth.ReaderMentor) {
		t.Fatalf("a 5m error that does not flip the strong-day test must not hold the mentor: %+v", mm)
	}
}
