package kernel

import (
	"testing"
	"time"

	"vl/market"
)

func barAt(loc *time.Location, y int, mo time.Month, d, h, mi int, o, hi, lo, c float64) market.Kline {
	open := time.Date(y, mo, d, h, mi, 0, 0, loc)
	return market.Kline{
		OpenTime:  open.UnixMilli(),
		Open:      o,
		High:      hi,
		Low:       lo,
		Close:     c,
		CloseTime: open.Add(time.Minute).UnixMilli() - 1,
	}
}

func levelMap(levels []DetectedLevel) map[LevelKind]float64 {
	m := map[LevelKind]float64{}
	for _, l := range levels {
		m[l.Kind] = l.Price
	}
	return m
}

// TestStartOfWeekCTSundayRollsForward — bug-hunt #32: a Sunday belongs to the
// FOLLOWING Monday (the CME week opens Sunday 17:00 CT), not the ISO week's
// previous Monday. The prior-week window (PWH/PWL) was otherwise a week stale
// on Sunday evening. Mutant (ISO convention: Sunday → previous Monday) → RED.
func TestStartOfWeekCTSundayRollsForward(t *testing.T) {
	loc := chicago()
	cases := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{"Sunday evening", time.Date(2026, 10, 18, 18, 0, 0, 0, loc), time.Date(2026, 10, 19, 0, 0, 0, 0, loc)},
		{"Monday midnight", time.Date(2026, 10, 19, 0, 0, 0, 0, loc), time.Date(2026, 10, 19, 0, 0, 0, 0, loc)},
		{"Friday morning", time.Date(2026, 8, 14, 10, 0, 0, 0, loc), time.Date(2026, 8, 10, 0, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		if got := startOfWeekCT(c.in); !got.Equal(c.want) {
			t.Fatalf("%s: startOfWeekCT(%v) = %v, want %v", c.name, c.in.Format("Mon 01-02 15:04"), got.Format("Mon 01-02 15:04"), c.want.Format("Mon 01-02 15:04"))
		}
	}
}

func TestExtractMultiDayLevels(t *testing.T) {
	loc := chicago()
	reg := DefaultSessionRegistry()
	// now = Friday 2026-08-14 10:00 CT (mid-RTH). Futures session-day = 08-13.
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, loc)

	bars := []market.Kline{
		// prior month (July) + prior week (Aug 3-9) singletons — THIN, omitted.
		barAt(loc, 2026, 7, 20, 12, 0, 14750, 14800, 14700, 14750),
		barAt(loc, 2026, 8, 5, 12, 0, 15050, 15100, 15000, 15050),
	}
	// Prior calendar day 08-13 with FULL coverage (P0.4-F: ≥900 closed bars) —
	// flat 15520 fill across 00:00-16:59, then the specific RTH/extreme bars.
	for mi := 0; mi < 17*60; mi++ {
		bars = append(bars, barAt(loc, 2026, 8, 13, mi/60, mi%60, 15520, 15530, 15510, 15520))
	}
	bars = append(bars,
		barAt(loc, 2026, 8, 13, 9, 0, 15540, 15580, 15500, 15540),  // NY (RTHH 15580)
		barAt(loc, 2026, 8, 13, 14, 0, 15500, 15560, 15450, 15550), // NY (RTHL 15450)
		barAt(loc, 2026, 8, 13, 18, 0, 15570, 15620, 15560, 15600), // Asia (day high 15620, close 15600)
		// current overnight (futures-day 08-13): more Asia (after midnight) + London
		barAt(loc, 2026, 8, 14, 0, 30, 15600, 15610, 15530, 15570), // Asia (calendar 08-14)
		barAt(loc, 2026, 8, 14, 4, 0, 15550, 15590, 15480, 15500),  // London
		// today's developing RTH (closed 09:00 bar) — not itself a level
		barAt(loc, 2026, 8, 14, 9, 0, 15600, 15650, 15550, 15600), // NY
	)

	m := levelMap(ExtractMultiDayLevels(bars, reg, now))

	want := map[LevelKind]float64{
		KindPDH:  15620, // 08-13 calendar high (incl. evening Asia)
		KindPDL:  15450, // 08-13 calendar low
		KindPDC:  15600, // 08-13 last close (18:00 bar)
		KindRTHH: 15580, // 08-13 NY high only
		KindRTHL: 15450, // 08-13 NY low
		KindASH:  15620, // overnight Asia high
		KindASL:  15530, // overnight Asia low
		KindLDNH: 15590, // overnight London high
		KindLDNL: 15480, // overnight London low
		KindONH:  15620, // composite
		KindONL:  15480,
		// P0.4-F: prior week/month buckets with singleton coverage are THIN —
		// HTF anchors are omitted rather than fabricated from a 1-bar window.
	}
	for k, v := range want {
		got, ok := m[k]
		if !ok {
			t.Fatalf("missing level %s", k)
		}
		if got != v {
			t.Fatalf("%s = %v want %v", k, got, v)
		}
	}
	for _, k := range []LevelKind{KindPWH, KindPWL, KindPMH, KindPML} {
		if _, ok := m[k]; ok {
			t.Fatalf("thin-coverage bucket must NOT emit %s (P0.4-F)", k)
		}
	}
	if len(m) != len(want) {
		t.Fatalf("unexpected extra levels: got %d kinds, want %d (%v)", len(m), len(want), m)
	}
}

// TestExtractMultiDayLevelsTruncatedPriorDay (P0.4-F, 2026-08-25) — a prior-day
// bucket with only a few closed bars (bot-down gap / fresh boot) must NOT emit
// PDH/PDL/PDC. Live proof: ASIA v4 anchored flip on "PDL 29263.25" which was
// Friday's close region read off a Friday-afternoon-only bucket.
func TestExtractMultiDayLevelsTruncatedPriorDay(t *testing.T) {
	loc := chicago()
	reg := DefaultSessionRegistry()
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, loc)
	bars := []market.Kline{
		// prior day 08-13: only 3 closed bars (truncated afternoon) — NOT covered.
		barAt(loc, 2026, 8, 13, 12, 0, 15540, 15580, 15500, 15540),
		barAt(loc, 2026, 8, 13, 13, 0, 15500, 15560, 15450, 15550),
		barAt(loc, 2026, 8, 13, 14, 0, 15570, 15620, 15560, 15600),
	}
	m := levelMap(ExtractMultiDayLevels(bars, reg, now))
	for _, k := range []LevelKind{KindPDH, KindPDL, KindPDC, KindRTHH, KindRTHL} {
		if _, ok := m[k]; ok {
			t.Fatalf("truncated prior-day bucket must NOT emit %s (P0.4-F)", k)
		}
	}
}

func TestExtractMultiDayLevelsColdStart(t *testing.T) {
	reg := DefaultSessionRegistry()
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, chicago())
	// No bars → no levels (warms forward, honest empty).
	if got := ExtractMultiDayLevels(nil, reg, now); len(got) != 0 {
		t.Fatalf("cold start should emit no levels, got %v", got)
	}
}

func TestExtractMultiDayLevelsOvernightComposite(t *testing.T) {
	loc := chicago()
	reg := DefaultSessionRegistry()
	now := time.Date(2026, 8, 14, 7, 0, 0, 0, loc) // during London, pre-RTH
	bars := []market.Kline{
		barAt(loc, 2026, 8, 13, 20, 0, 15500, 15560, 15490, 15520), // Asia
		barAt(loc, 2026, 8, 14, 3, 0, 15520, 15540, 15470, 15500),  // London
	}
	m := levelMap(ExtractMultiDayLevels(bars, reg, now))
	// ONH = max(Asia high, London high); ONL = min(lows).
	if m[KindONH] != 15560 || m[KindONL] != 15470 {
		t.Fatalf("ON composite = %v/%v want 15560/15470", m[KindONH], m[KindONL])
	}
	if m[KindASH] != 15560 || m[KindLDNL] != 15470 {
		t.Fatalf("AS/LDN extremes = %v/%v", m[KindASH], m[KindLDNL])
	}
}

func TestExtractMultiDayLevelsNoPriorDay(t *testing.T) {
	loc := chicago()
	reg := DefaultSessionRegistry()
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, loc)
	// Only TODAY's RTH bars — no prior calendar day, no overnight → no PDH, no ON.
	bars := []market.Kline{
		barAt(loc, 2026, 8, 14, 9, 0, 15600, 15650, 15550, 15600),
	}
	m := levelMap(ExtractMultiDayLevels(bars, reg, now))
	if _, ok := m[KindPDH]; ok {
		t.Fatalf("only today's bars → PDH must be absent")
	}
	if _, ok := m[KindONH]; ok {
		t.Fatalf("no overnight bars → ONH must be absent")
	}
}
