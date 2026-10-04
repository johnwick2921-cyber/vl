package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// T1 TIME AUDIT (CTO 12:55:56Z): one table test per wall-clock-bearing
// function, each row run on a CDT date (Sep) AND a CST date (Jan) with
// REAL-UTC epoch inputs — a 1-hour or 5-hour shift goes RED. The convention
// (EPOCH RULING 2026-10-03): bars carry real UTC epoch ms; the CT read goes
// through America/Chicago, never raw division on the epoch.

type auditDate struct {
	label string
	y     int
	mo    time.Month
	d     int
}

var auditDates = []auditDate{
	{"CDT", 2026, time.September, 15},
	{"CST", 2026, time.January, 15},
}

// auditMs returns the REAL-UTC epoch ms of the given CT wall time.
func auditMs(y int, mo time.Month, d, hh, mm, ss int) int64 {
	return time.Date(y, mo, d, hh, mm, ss, 0, ctime()).UnixMilli()
}

// rthBars builds one real-UTC RTH bar (2026-09-15 CT, 09:00 + i minutes) —
// the replacement for the wall-epoch literal CloseTime offsets in tests.
func rthBars(i int, o, h, l, c float64) market.Kline {
	ot := auditMs(2026, 9, 15, 9, 0, 0) + int64(i)*60_000
	return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
}
func auditDateFmt(y int, mo time.Month, d int) string {
	return time.Date(y, mo, d, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

func TestTimeAuditTradingDayKey(t *testing.T) {
	for _, c := range auditDates {
		if got := tradingDayKey(time.UnixMilli(auditMs(c.y, c.mo, c.d, 16, 59, 59)).In(ctime())); got != auditDateFmt(c.y, c.mo, c.d) {
			t.Fatalf("%s 16:59:59: key %q, want %s", c.label, got, auditDateFmt(c.y, c.mo, c.d))
		}
		if got := tradingDayKey(time.UnixMilli(auditMs(c.y, c.mo, c.d, 17, 0, 0)).In(ctime())); got != auditDateFmt(c.y, c.mo, c.d+1) {
			t.Fatalf("%s 17:00:00: key %q, want %s (17:00 CT flips to the next trading day)", c.label, got, auditDateFmt(c.y, c.mo, c.d+1))
		}
	}
}

func TestTimeAuditSessionBounds(t *testing.T) {
	for _, c := range auditDates {
		open, close := sessionBounds(auditMs(c.y, c.mo, c.d, 10, 0, 0), ctime())
		wantOpen := auditMs(c.y, c.mo, c.d-1, 17, 0, 0)
		wantClose := auditMs(c.y, c.mo, c.d, 8, 30, 0)
		if open != wantOpen || close != wantClose {
			t.Fatalf("%s 10:00: session = [%d, %d], want [%d, %d] (17:00 CT open, 08:30 CT close)",
				c.label, open, close, wantOpen, wantClose)
		}
	}
}

func TestTimeAuditLatchDayFreezesAt0830(t *testing.T) {
	g := DefaultDayGate()
	for _, c := range auditDates {
		// pre-open: live, never latched (Key "").
		if l := LatchDay(DayLatch{}, auditMs(c.y, c.mo, c.d, 8, 29, 59), ctime(), 10, true, false, g); l.Key != "" {
			t.Fatalf("%s 08:29:59: latch key %q, want \"\" (live before the open)", c.label, l.Key)
		}
		// at/after 08:30 CT: latched for the trading day.
		key := tradingDayKey(time.UnixMilli(auditMs(c.y, c.mo, c.d, 8, 30, 0)).In(ctime()))
		l := LatchDay(DayLatch{}, auditMs(c.y, c.mo, c.d, 8, 30, 0), ctime(), 10, true, false, g)
		if l.Key != key {
			t.Fatalf("%s 08:30:00: latch key %q, want %q", c.label, l.Key, key)
		}
		// later the same day: still frozen on the same key.
		if l2 := LatchDay(l, auditMs(c.y, c.mo, c.d, 12, 0, 0), ctime(), 999, true, false, g); l2.Key != key || l2.Verdict != l.Verdict {
			t.Fatalf("%s 12:00: latch %+v, want frozen %+v", c.label, l2, l)
		}
	}
}

func TestTimeAuditGlobexRunFreezeAt0830(t *testing.T) {
	for _, c := range auditDates {
		bars := []market.Kline{
			{OpenTime: auditMs(c.y, c.mo, c.d-1, 17, 0, 0), High: 100, Low: 90},
			{OpenTime: auditMs(c.y, c.mo, c.d, 8, 29, 0), High: 200, Low: 90},
		}
		// at 08:30:00 the window freezes to [17:00, 08:30) → the 08:29 bar joins.
		run, ok := GlobexRun(bars, auditMs(c.y, c.mo, c.d, 8, 30, 0), ctime())
		if !ok || run != 110 {
			t.Fatalf("%s 08:30:00: run = (%v, %v), want (110, true) — frozen 17:00→08:30 window", c.label, run, ok)
		}
		// after 17:00 CT the window belongs to the NEW session [17:00 same
		// day, 08:30 next day): the old session's bars fall outside it.
		if _, ok := GlobexRun(bars, auditMs(c.y, c.mo, c.d, 17, 30, 0), ctime()); ok {
			t.Fatalf("%s 17:30: run measured, want the old session's bars outside the new window", c.label)
		}
	}
}

func TestTimeAuditFourHBucketStart(t *testing.T) {
	for _, c := range auditDates {
		// 4h buckets anchored at 17:00 CT: [17,21), [21,01), [01,05), …
		for _, tt := range []struct {
			dayOff       int // days relative to c.d
			hh, mm       int
			wantOff      int // anchor day offset
			wantH, wantM int
		}{
			{-1, 16, 59, -1, 13, 0}, // [13:00,17:00) bucket on the 4h grid from 17:00
			{0, 17, 0, 0, 17, 0},
			{0, 20, 59, 0, 17, 0},
			{0, 21, 0, 0, 21, 0},
			{1, 0, 59, 0, 21, 0},
			{1, 1, 0, 1, 1, 0},
		} {
			got := fourHBucketStart(auditMs(c.y, c.mo, c.d+tt.dayOff, tt.hh, tt.mm, 0), ctime())
			want := auditMs(c.y, c.mo, c.d+tt.wantOff, tt.wantH, tt.wantM, 0)
			if got != want {
				t.Fatalf("%s %02d:%02d CT: bucket start %d, want %d", c.label, tt.hh, tt.mm, got, want)
			}
		}
	}
}

func TestTimeAuditORBAnchors(t *testing.T) {
	for _, c := range auditDates {
		mk := func(hh, mm int, o, h, l, cl float64) market.Kline {
			ot := auditMs(c.y, c.mo, c.d, hh, mm, 0)
			return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: cl}
		}
		bars := []market.Kline{
			mk(8, 30, 24820, 24844, 24814, 24830),
			mk(8, 31, 24830, 24836, 24816, 24820),
			mk(8, 32, 24840, 24870, 24830, 24860),
		}
		orb := ORBAdvance(ORB{}, bars, auditMs(c.y, c.mo, c.d, 8, 33, 0)-1)
		if !orb.Drawn || orb.High != 24844 || orb.Low != 24814 {
			t.Fatalf("%s: ORB = %+v, want drawn 24844/24814 — real-UTC bars must find the 08:30/08:31 anchor", c.label, orb)
		}
		if orb.Escaped != SideLong {
			t.Fatalf("%s: escape = %q, want long on the 08:32 candle", c.label, orb.Escaped)
		}
	}
}

func TestTimeAuditRTHOnly(t *testing.T) {
	for _, c := range auditDates {
		mk := func(hh, mm int) market.Kline {
			ot := auditMs(c.y, c.mo, c.d, hh, mm, 0)
			return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: 1, High: 1, Low: 1, Close: 1}
		}
		bars := []market.Kline{
			mk(8, 29),  // pre-open → dropped
			mk(8, 30),  // first RTH minute → kept
			mk(14, 59), // last RTH minute → kept
			mk(15, 0),  // RTH close → dropped
		}
		got := rthOnly(bars)
		if len(got) != 2 {
			ids := []int{}
			for _, b := range got {
				_, hh, mm := ctOf(b.OpenTime)
				ids = append(ids, hh*60+mm)
			}
			t.Fatalf("%s: rthOnly kept %v, want [08:30, 14:59]", c.label, ids)
		}
	}
}

func TestTimeAuditBucketOpen4h(t *testing.T) {
	for _, c := range auditDates {
		// the 4h bucket is anchored at 17:00 CT, DST-aware: on a CDT date
		// 17:00 CT = 22:00 UTC; on a CST date = 23:00 UTC. Wall arithmetic
		// shifts the anchor by the zone offset and goes RED.
		for _, tt := range []struct {
			hh, mm       int
			dayOff       int
			wantOff      int
			wantH, wantM int
		}{
			{16, 59, -1, -1, 13, 0}, // [13:00,17:00) bucket
			{17, 0, 0, 0, 17, 0},
			{20, 59, 0, 0, 17, 0},
			{21, 0, 0, 0, 21, 0},
			{0, 59, 1, 0, 21, 0},
			{1, 0, 1, 1, 1, 0},
		} {
			got := bucketOpen(auditMs(c.y, c.mo, c.d+tt.dayOff, tt.hh, tt.mm, 0), 240)
			want := auditMs(c.y, c.mo, c.d+tt.wantOff, tt.wantH, tt.wantM, 0)
			if got != want {
				t.Fatalf("%s %02d:%02d CT: bucketOpen(240) = %d, want %d", c.label, tt.hh, tt.mm, got, want)
			}
		}
		// 1h/5m/15m/1m buckets stay plain TF flooring of real-UTC epoch ms.
		for _, tf := range []int{1, 5, 15, 60} {
			ms := auditMs(c.y, c.mo, c.d, 9, 7, 0)
			want := auditMs(c.y, c.mo, c.d, 9, 7-7%tf, 0)
			if got := bucketOpen(ms, tf); got != want {
				t.Fatalf("%s tf=%d: bucketOpen = %d, want %d", c.label, tf, got, want)
			}
		}
	}
}

func TestTimeAuditSeedTodayWindow(t *testing.T) {
	for _, c := range auditDates {
		if got := sessionKeyCT(auditMs(c.y, c.mo, c.d, 9, 0, 0)); got != auditDateFmt(c.y, c.mo, c.d) {
			t.Fatalf("%s 09:00: session key = %s, want %s", c.label, got, auditDateFmt(c.y, c.mo, c.d))
		}
		if got := sessionKeyCT(auditMs(c.y, c.mo, c.d, 18, 0, 0)); got != auditDateFmt(c.y, c.mo, c.d+1) {
			t.Fatalf("%s 18:00: session key = %s, want %s (the 17:00 CT flip moves it to the next day)", c.label, got, auditDateFmt(c.y, c.mo, c.d+1))
		}
		// a bar in the overnight half of the session belongs to the same key.
		if got := sessionKeyCT(auditMs(c.y, c.mo, c.d-1, 20, 0, 0)); got != auditDateFmt(c.y, c.mo, c.d) {
			t.Fatalf("%s prev 20:00: session key = %s, want %s (17:00 CT opens the NEXT key)", c.label, got, auditDateFmt(c.y, c.mo, c.d))
		}
	}
}

func TestTimeAuditCTOf(t *testing.T) {
	for _, c := range auditDates {
		_, hh, mm := ctOf(auditMs(c.y, c.mo, c.d, 8, 30, 0))
		if hh != 8 || mm != 30 {
			t.Fatalf("%s: ctOf(08:30 CT) = %02d:%02d — the CT read must go through America/Chicago", c.label, hh, mm)
		}
		_, hh, mm = ctOf(auditMs(c.y, c.mo, c.d, 16, 59, 59))
		if hh != 16 || mm != 59 {
			t.Fatalf("%s: ctOf(16:59:59 CT) = %02d:%02d", c.label, hh, mm)
		}
	}
}

// T1 site 4: the 1h RTH anchor — candles open at :30; a bar at hh:29
// belongs to the previous hour's candle. Wall-epoch division on real-UTC
// bars mis-anchors by the zone offset (5h CDT / 6h CST), so rthHourAnchor
// reads America/Chicago.
func TestTimeAuditRTHHourAnchor(t *testing.T) {
	anchorCases := []struct {
		hh, mm int
		wantHh int
		inD    int // input day offset from c.d
		wantD  int // want day offset from c.d
	}{
		{8, 29, 7, 0, 0},    // 08:29 → 07:30 candle
		{8, 30, 8, 0, 0},    // 08:30 → 08:30 candle
		{9, 29, 8, 0, 0},    // 09:29 → 08:30 candle
		{9, 30, 9, 0, 0},    // 09:30 → 09:30 candle
		{15, 0, 14, 0, 0},   // 15:00 → 14:30 candle (:00 < :30)
		{15, 30, 15, 0, 0},  // 15:30 → 15:30 candle
		{0, 10, 23, -1, -2}, // 00:10 → previous day's 23:30 candle
	}
	for _, c := range auditDates {
		for _, tc := range anchorCases {
			got := rthHourAnchor(auditMs(c.y, c.mo, c.d+tc.inD, tc.hh, tc.mm, 0))
			want := auditMs(c.y, c.mo, c.d+tc.wantD, tc.wantHh, 30, 0)
			if got != want {
				t.Fatalf("%s %02d:%02d: anchor = %s, want %s",
					c.label, tc.hh, tc.mm,
					time.UnixMilli(got).In(ctime()).Format("2006-01-02 15:04"),
					time.UnixMilli(want).In(ctime()).Format("2006-01-02 15:04"))
			}
		}
	}
	// rthMinuteOf: the CT wall minute-of-day, not the UTC one.
	for _, c := range auditDates {
		if got := rthMinuteOf(auditMs(c.y, c.mo, c.d, 8, 30, 0)); got != 8*60+30 {
			t.Fatalf("%s rthMinuteOf(08:30 CT) = %d, want 510", c.label, got)
		}
		if got := rthMinuteOf(auditMs(c.y, c.mo, c.d, 15, 0, 0)); got != 15*60 {
			t.Fatalf("%s rthMinuteOf(15:00 CT) = %d, want 900 (the rthEnd cutoff must read CT)", c.label, got)
		}
	}
	// production call site A (aggregation path): keyLevel1HBars on 1-min
	// bars must anchor 09:00–09:29 CT at 08:30 CT and open the next candle
	// at 09:30 CT, in both zones.
	for _, c := range auditDates {
		var bars []market.Kline
		for i := 0; i < 60; i++ { // 09:00 → 09:59 CT
			ot := auditMs(c.y, c.mo, c.d, 9, 0, 0) + int64(i)*60_000
			bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: 1, High: 2, Low: 0.5, Close: 1.5})
		}
		got := keyLevel1HBars(bars)
		if len(got) != 2 {
			t.Fatalf("%s: keyLevel1HBars(09:00–09:59) = %d candles, want 2", c.label, len(got))
		}
		if got[0].OpenTime != auditMs(c.y, c.mo, c.d, 8, 30, 0) || got[1].OpenTime != auditMs(c.y, c.mo, c.d, 9, 30, 0) {
			t.Fatalf("%s: anchors = [%s, %s], want [08:30, 09:30] CT",
				c.label,
				time.UnixMilli(got[0].OpenTime).In(ctime()).Format("15:04"),
				time.UnixMilli(got[1].OpenTime).In(ctime()).Format("15:04"))
		}
	}
	// production call site B (pre-bucketed path): bars ≥1h apart pass
	// through the rthMinuteOf RTH filter unchanged — a UTC read would drop
	// 10:00 CT (15:00/16:00 UTC at the rthEnd edge) in both zones.
	for _, c := range auditDates {
		bars := []market.Kline{
			{OpenTime: auditMs(c.y, c.mo, c.d, 8, 30, 0), CloseTime: auditMs(c.y, c.mo, c.d, 8, 30, 0) + 59_999, Open: 1, High: 2, Low: 0.5, Close: 1.5},
			{OpenTime: auditMs(c.y, c.mo, c.d, 10, 0, 0), CloseTime: auditMs(c.y, c.mo, c.d, 10, 0, 0) + 59_999, Open: 1, High: 2, Low: 0.5, Close: 1.5},
		}
		got := keyLevel1HBars(bars)
		if len(got) != 2 {
			t.Fatalf("%s: pre-bucketed filter dropped an RTH bar (%d left, want 2)", c.label, len(got))
		}
		if got[0].OpenTime != bars[0].OpenTime || got[1].OpenTime != bars[1].OpenTime {
			t.Fatalf("%s: pre-bucketed passthrough re-anchored bars", c.label)
		}
	}
}
