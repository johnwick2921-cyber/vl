package checks

import (
	"testing"
	"time"
)

type frames struct {
	M1  [][]any `json:"m1"`
	F3  [][]any `json:"3m"`
	F5  [][]any `json:"5m"`
	F15 [][]any `json:"15m"`
}

func ct(t *testing.T, s string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestCrossFrame1027RestartTruncatedBars pins the one real mismatch of the last
// two days: after the 08:34 CT reconnect on 10-27 the live 5m (row 2045884),
// 3m (row 2045706) and 15m bars were BORN at the first live minute of their
// bucket and lack its first minutes (open 43 pt, low 56 pt wrong on the 5m).
func TestCrossFrame1027RestartTruncatedBars(t *testing.T) {
	var f frames
	loadJSON(t, "frames_1027.json", &f)
	m1 := toBars(f.M1)
	cases := []struct {
		tf   int
		rows [][]any
		bar  string
		eval string
	}{
		{5, f.F5, "2026-10-27 08:30", "2026-10-27 08:36"},
		{3, f.F3, "2026-10-27 08:33", "2026-10-27 08:37"},
		{15, f.F15, "2026-10-27 08:30", "2026-10-27 08:46"},
	}
	for _, c := range cases {
		htf := toBars(c.rows)
		mm, judged, _ := CompareFrames(m1, htf, c.tf, ct(t, c.eval), nil, DefaultCross())
		if judged == 0 {
			t.Fatalf("%dm: nothing judged", c.tf)
		}
		found := false
		for _, m := range mm {
			if m.T == ct(t, c.bar).UnixMilli() {
				found = true
			}
		}
		if !found {
			t.Errorf("%dm bar %s: the truncated native bar was NOT flagged (%d mismatches)", c.tf, c.bar, len(mm))
		}
		// the same frame is clean once the window has moved past it
		if later, _, _ := CompareFrames(m1, htf, c.tf, ct(t, c.eval).Add(40*time.Minute), nil, DefaultCross()); len(later) != 0 {
			t.Errorf("%dm: still flagged 40 min later: %+v", c.tf, later)
		}
	}
}

// TestCrossFrameCleanBarsAgree: the untouched 5m bars of the same morning agree
// with their 1m to the digit, so the check does not fire on them.
func TestCrossFrameCleanBarsAgree(t *testing.T) {
	var f frames
	loadJSON(t, "frames_1027.json", &f)
	m1 := toBars(f.M1)
	htf := toBars(f.F5)
	mm, judged, _ := CompareFrames(m1, htf, 5, ct(t, "2026-10-27 09:05"), nil, CrossParams{WindowMin: 30, PriceTol: 0.5, VolRel: 0.40, VolAbsMin: 200})
	for _, m := range mm {
		if m.T == ct(t, "2026-10-27 08:30").UnixMilli() {
			continue // the one known-bad bar is outside this window by construction; tolerate if it slips in
		}
		t.Errorf("clean 5m flagged: %s", m.Detail)
	}
	if judged < 3 {
		t.Fatalf("judged only %d", judged)
	}
}

// TestCrossFrameHealedByRepair: replacing the truncated bar with the 1m-built
// values (the hole re-request path) clears the mismatch.
func TestCrossFrameHealedByRepair(t *testing.T) {
	var f frames
	loadJSON(t, "frames_1027.json", &f)
	m1 := toBars(f.M1)
	htf := toBars(f.F5)
	at := ct(t, "2026-10-27 08:30").UnixMilli()
	for i := range htf {
		if htf[i].T == at {
			o, h, l, c, v := 0.0, 0.0, 0.0, 0.0, 0.0
			n := 0
			for _, b := range m1 {
				if b.T >= at && b.T < at+5*minuteMs {
					if n == 0 {
						o, h, l = b.O, b.H, b.L
					}
					if b.H > h {
						h = b.H
					}
					if b.L < l {
						l = b.L
					}
					c = b.C
					v += b.V
					n++
				}
			}
			htf[i] = Bar{T: at, O: o, H: h, L: l, C: c, V: v}
		}
	}
	if mm, _, _ := CompareFrames(m1, htf, 5, ct(t, "2026-10-27 08:36"), nil, DefaultCross()); len(mm) != 0 {
		t.Fatalf("healed frame still flagged: %+v", mm)
	}
}

// TestCrossFrameSkipsIncomplete1m: a bucket with a 1m hole is the hole verdict's
// business - skipped (and counted), never judged.
func TestCrossFrameSkipsIncomplete1m(t *testing.T) {
	var f frames
	loadJSON(t, "frames_1027.json", &f)
	var m1 []Bar
	for _, b := range toBars(f.M1) {
		if b.T == ct(t, "2026-10-27 08:32").UnixMilli() {
			continue
		}
		m1 = append(m1, b)
	}
	mm, _, skipped := CompareFrames(m1, toBars(f.F5), 5, ct(t, "2026-10-27 08:36"), nil, DefaultCross())
	if skipped == 0 || len(mm) != 0 {
		t.Fatalf("hole bucket must be skipped, not judged: skipped=%d mm=%v", skipped, mm)
	}
}
