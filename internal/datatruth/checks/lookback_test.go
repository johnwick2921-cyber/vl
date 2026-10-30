package checks

import (
	"math"
	"strings"
	"testing"
	"time"
)

type swingFile struct {
	BotLine   float64 `json:"botLine"`
	NativeAll []float64
	Buckets   []struct {
		Start  int64    `json:"start"`
		C1     *float64 `json:"c1"`
		C5     *float64 `json:"c5"`
		Native *float64 `json:"native"`
	} `json:"buckets"`
}

func loadSwing(t *testing.T) (swingFile, []Bucket) {
	var s swingFile
	loadJSON(t, "swing4h_1028.json", &s)
	var raw struct {
		NativeAll []float64 `json:"nativeAll"`
	}
	loadJSON(t, "swing4h_1028.json", &raw)
	s.NativeAll = raw.NativeAll
	var bs []Bucket
	for _, b := range s.Buckets {
		x := Bucket{Start: b.Start}
		if b.C1 != nil {
			x.Close, x.Has = *b.C1, true
		}
		if b.C5 != nil {
			x.Alt, x.HasAlt = *b.C5, true
		}
		bs = append(bs, x)
	}
	return s, bs
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// TestSwing4hP26Explained pins every number of the P2-6 investigation on the
// real 4h bars of MNQ 12-26 (bucket 10-28 17:00 CT).
func TestSwing4hP26Explained(t *testing.T) {
	s, bs := loadSwing(t)
	// (a) the bot's line is EXACTLY the recurrence over the 1m-built closes
	var c1 []float64
	for _, b := range bs {
		if b.Has {
			c1 = append(c1, b.Close)
		}
	}
	if got := EMAFromCloses(c1, 34); !near(got, 29645.53, 0.01) || !near(got, s.BotLine, 0.01) {
		t.Fatalf("1m-built swing line = %.2f, want the bot's 29645.53 (snapshot %.3f)", got, s.BotLine)
	}
	if len(c1) != 201 {
		t.Fatalf("1m-built closes = %d, want 201", len(c1))
	}
	// (b) the trade-recheck's 29637.87 included NT8's 94 off-grid stub rows
	if got := EMAFromCloses(s.NativeAll, 34); !near(got, 29637.87, 0.01) || len(s.NativeAll) != 677 {
		t.Fatalf("EMA over all %d native 4h rows = %.2f, want 29637.87", len(s.NativeAll), got)
	}
	// (c) the on-grid native closes give the truth 29645.10, for any depth >= 200
	var nat []float64
	for _, b := range s.Buckets {
		if b.Native != nil {
			nat = append(nat, *b.Native)
		}
	}
	if got := EMAFromCloses(nat, 34); !near(got, 29645.10, 0.05) {
		t.Fatalf("on-grid native EMA = %.2f, want ~29645.10", got)
	}
	// (d) the whole defect: ONE bucket (10-26 17:00 CT, 4 h hole in the 1m)
	v := JudgeSwing4h(bs, 34, s.BotLine, 1.0)
	if !near(v.Repaired, 29645.11, 0.05) {
		t.Fatalf("repaired line %.2f, want 29645.11", v.Repaired)
	}
	holeAt := ct(t, "2026-10-26 17:00").UnixMilli()
	missingRecent := 0
	for _, b := range bs {
		if !b.Has && b.Start >= holeAt-1 {
			missingRecent++
			if b.Start != holeAt || !b.HasAlt {
				t.Fatalf("the recent hole must be the 10-26 17:00 bucket, repairable from the 5m: %+v", b)
			}
		}
	}
	if missingRecent != 1 {
		t.Fatalf("recent holes = %d, want exactly 1", missingRecent)
	}
	// 0.43 pt < the 1.0 pt tolerance: MEASURED clear, no pause
	if v.Blocked {
		t.Fatalf("a 0.43 pt discrepancy must not pause: %s", v.Why)
	}
	if d := math.Abs(s.BotLine - v.Repaired); !near(d, 0.42, 0.05) {
		t.Fatalf("measured disagreement %.2f, want 0.42", d)
	}
}

// TestSwing4hPausesWhenTheHoleMatters: the same real series with the NEWEST
// bucket (10-28 13:00, age 0, weight 5.7%) dropped from the 1m but present in
// the 5m is repairable and measured; dropped from every frame it is unknowable
// and pauses; and a tight tolerance turns the measured 0.43 pt into a pause.
func TestSwing4hPausesWhenTheHoleMatters(t *testing.T) {
	s, bs := loadSwing(t)
	last := len(bs) - 1

	// unknowable: gone from every frame
	b2 := append([]Bucket(nil), bs...)
	b2[last].Has, b2[last].HasAlt = false, false
	if v := JudgeSwing4h(b2, 34, s.BotLine, 1.0); !v.Blocked || v.Unrepairable != 1 {
		t.Fatalf("a close that exists in no frame must pause: %+v", v)
	}

	// measured: bot line computed WITHOUT the newest close, repaired with it
	b3 := append([]Bucket(nil), bs...)
	b3[last].Has = false // still HasAlt
	bot := EMAFromCloses(closes1m(b3), 34)
	v := JudgeSwing4h(b3, 34, bot, 1.0)
	if !v.Blocked || math.Abs(bot-v.Repaired) < 1.0 {
		t.Fatalf("newest-bucket hole: bot %.2f repaired %.2f must pause: %+v", bot, v.Repaired, v)
	}

	// a tolerance tighter than the real 0.43 pt turns the 10-28 case into a pause
	if v := JudgeSwing4h(bs, 34, s.BotLine, 0.25); !v.Blocked {
		t.Fatalf("0.25 pt tolerance must pause on the measured 0.42 pt: %+v", v)
	}
}

func closes1m(bs []Bucket) (c []float64) {
	for _, b := range bs {
		if b.Has {
			c = append(c, b.Close)
		}
	}
	return
}

// TestBuckets4hFromMinutes: Buckets4h reproduces the 17:00-CT-anchored closes
// and flags a bucket absent from the 1m but present in the 5m (the P2-6 shape),
// across a DST fall-back day.
func TestBuckets4hFromMinutes(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	start := ct(t, "2026-10-26 09:00")
	var m1, m5 []Bar
	c := 100.0
	for tm := start; tm.Before(ct(t, "2026-10-28 09:00")); tm = tm.Add(time.Minute) {
		c += 0.25
		m1 = append(m1, Bar{T: tm.UnixMilli(), O: c, H: c, L: c, C: c, V: 1})
		if tm.Minute()%5 == 0 {
			m5 = append(m5, Bar{T: tm.UnixMilli(), O: c, H: c, L: c, C: c + 1.25, V: 5})
		}
	}
	// punch the 10-27 01:00 bucket out of the 1m only
	hole0, hole1 := ct(t, "2026-10-27 01:00").UnixMilli(), ct(t, "2026-10-27 05:00").UnixMilli()
	var m1h []Bar
	for _, b := range m1 {
		if b.T >= hole0 && b.T < hole1 {
			continue
		}
		m1h = append(m1h, b)
	}
	bs := Buckets4h(m1h, m5, nil, ct(t, "2026-10-28 09:00"), loc)
	if len(bs) == 0 {
		t.Fatal("no buckets")
	}
	found := false
	for _, b := range bs {
		if b.Start == hole0 {
			found = true
			if b.Has || !b.HasAlt {
				t.Fatalf("hole bucket: %+v", b)
			}
		} else if !b.Has {
			t.Fatalf("unexpected hole: %+v", b)
		}
	}
	if !found {
		t.Fatal("hole bucket missing from the list")
	}
	// DST fall-back (2026-11-01): iteration must terminate and keep order
	var dm []Bar
	for tm := ct(t, "2026-10-31 17:00"); tm.Before(ct(t, "2026-11-02 09:00")); tm = tm.Add(time.Minute) {
		dm = append(dm, Bar{T: tm.UnixMilli(), O: 1, H: 1, L: 1, C: 1, V: 1})
	}
	db := Buckets4h(dm, nil, nil, ct(t, "2026-11-02 09:00"), loc)
	for i := 1; i < len(db); i++ {
		if db[i].Start <= db[i-1].Start {
			t.Fatalf("DST: buckets not strictly increasing: %v", db)
		}
	}
	if len(db) < 8 {
		t.Fatalf("DST day: %d buckets", len(db))
	}
}

func steady(tfMin int, n int, end time.Time, step float64) []Bar {
	span := int64(tfMin) * minuteMs
	last := (end.UnixMilli() - span) / span * span
	var out []Bar
	c := 30000.0
	for i := n - 1; i >= 0; i-- {
		c += step
		out = append(out, Bar{T: last - int64(i)*span, O: c, H: c + 2, L: c - 2, C: c, V: 100})
	}
	return out
}

func without(bars []Bar, ts ...int64) []Bar {
	var out []Bar
next:
	for _, b := range bars {
		for _, t := range ts {
			if b.T == t {
				continue next
			}
		}
		out = append(out, b)
	}
	return out
}

func TestJudgeSeriesWeightsAndUnits(t *testing.T) {
	now := ct(t, "2026-10-28 11:00")
	bars := steady(1, 400, now, 3)
	spec := SeriesSpec{Name: "ema34_1m", TFMin: 1, Period: 34, EMA: true, PriceUnits: true, TolPts: 0.5}
	if b, why := JudgeSeries(spec, bars, now, nil); b {
		t.Fatalf("complete series blocked: %s", why)
	}
	last := bars[len(bars)-1].T
	// one missing minute at age 0: 5.7% x 3 pt = 0.17 pt < 0.5: allowed
	if b, why := JudgeSeries(spec, without(bars, last), now, nil); b {
		t.Fatalf("one missing recent minute must be tolerated: %s", why)
	}
	// four missing recent minutes: ~0.6 pt > 0.5: pause
	if b, _ := JudgeSeries(spec, without(bars, last, last-60000, last-120000, last-180000), now, nil); !b {
		t.Fatal("four missing recent minutes must pause")
	}
	// the same four, 70 minutes old: weight (1-k)^70 -> negligible: fine
	old := bars[len(bars)-71].T
	if b, why := JudgeSeries(spec, without(bars, old, old-60000, old-120000, old-180000), now, nil); b {
		t.Fatalf("an old hole decays to nothing: %s", why)
	}
	// unit-less window: ATR14 with 2 of 14 missing (14%) > 10%
	atr := SeriesSpec{Name: "atr14_5m", TFMin: 5, Period: 14, MaxWeight: 0.10}
	b5 := steady(5, 100, now, 3)
	l5 := b5[len(b5)-1].T
	if b, _ := JudgeSeries(atr, without(b5, l5-300_000), now, nil); b {
		t.Fatal("one of 14 missing (7%) is within 10%")
	}
	if b, _ := JudgeSeries(atr, without(b5, l5-300_000, l5-600_000), now, nil); !b {
		t.Fatal("two of 14 missing (14%) must pause")
	}
	// a total blackout (10-26 17:00-21:57 CT: not one 1m bar) cannot be priced -> pause
	if b, why := JudgeSeries(spec, nil, now, nil); !b || !strings.Contains(why, "no data") {
		t.Fatalf("empty series must pause: %v %q", b, why)
	}
	// closed hours are not holes: the calendar is honoured
	closed := func(ms int64) bool { return false }
	if b, why := JudgeSeries(spec, nil, now, OpenFn(closed)); b {
		t.Fatalf("market closed -> nothing expected: %s", why)
	}
}

func TestEMAWarmupBarsMatchesMarket(t *testing.T) {
	// market.EMAWarmupBars(34) == 113 pinned against the
	// production function in provider/ninjatrader (datatruth_wire_test.go).
	if EMAWarmupBars(34) != 113 || EMAWarmupBars(1) != 1 {
		t.Fatalf("EMAWarmupBars(34)=%d", EMAWarmupBars(34))
	}
}

func TestSessionExtremes(t *testing.T) {
	from, to := ct(t, "2026-10-27 17:00").UnixMilli(), ct(t, "2026-10-28 17:00").UnixMilli()
	m1 := []Bar{{T: from + 60000, H: 100, L: 90}, {T: from + 120000, H: 105, L: 95}}
	m5 := []Bar{{T: from, H: 105, L: 90}}
	if b, why := SessionExtremes(m1, m5, from, to, 2); b {
		t.Fatalf("agree: %s", why)
	}
	m1[1].H = 98 // 1m lost the high
	if b, _ := SessionExtremes(m1, m5, from, to, 2); !b {
		t.Fatal("a session high only the 5m holds must pause")
	}
	if b, _ := SessionExtremes(nil, m5, from, to, 2); !b {
		t.Fatal("no 1m for the session: cannot verify -> pause")
	}
}
