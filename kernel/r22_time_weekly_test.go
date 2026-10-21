package kernel

// R22 TIME-WEEKLY — kernel pins on REAL persisted bars (data.db read-only
// export, MNQ 12-26): the prior-week map projection, the native-daily trading
// date across DST, and dATR from complete sessions. Every test drives the
// production call site with the production bar provider seam
// (market.FuturesBarsProvider), never a hand-built helper input (canon 53).

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"vl/market"
	"vl/store"
)

func r22LoadBars(t *testing.T, name string) []market.Kline {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var rows [][6]float64
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	out := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		out = append(out, market.Kline{OpenTime: int64(r[0]), Open: r[1], High: r[2], Low: r[3], Close: r[4], Volume: r[5]})
	}
	return out
}

func r22Native1d(t *testing.T) []market.Kline {
	d := r22LoadBars(t, "mnq1226_native_1d_through_20261013.json")
	for i := range d {
		d[i].CloseTime = d[i].OpenTime + 86_400_000 - 1
	}
	return d
}

func r22Bars1m(t *testing.T) []market.Kline {
	b := r22LoadBars(t, "mnq1226_1m_2000_before_20261014_0144ct.json")
	for i := range b {
		b[i].CloseTime = b[i].OpenTime + 60_000 - 1
	}
	return b
}

// r22ServeAt installs a FuturesBarsProvider that serves the native daily rows
// the cache holds at `now` (a row exists once its 17:00 CT session opened) and
// the given 1m bars.
func r22ServeAt(t *testing.T, daily, oneMin []market.Kline, now time.Time) {
	t.Helper()
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(_, tf string, count int) []market.Kline {
		var src []market.Kline
		switch tf {
		case "1d":
			for _, b := range daily {
				st := time.UnixMilli(b.OpenTime).In(CTLocation())
				if !time.Date(st.Year(), st.Month(), st.Day(), 17, 0, 0, 0, CTLocation()).After(now) {
					src = append(src, b)
				}
			}
		case "1m":
			src = oneMin
		default:
			return nil
		}
		if count > 0 && len(src) > count {
			src = src[len(src)-count:]
		}
		return src
	}
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
}

func r22PWHL(t *testing.T, now time.Time) (pwh, pwl float64, method string) {
	t.Helper()
	scored := []ScoredLevel{{DetectedLevel: DetectedLevel{Price: 31000}}}
	for _, c := range mapProjectionsFor(scored, nil, "MNQ", 31000, now) {
		switch c.Names[0] {
		case "PWH":
			pwh, method = c.Price, c.ProjectionMethod
		case "PWL":
			pwl = c.Price
		}
	}
	return pwh, pwl, method
}

// P2-A — the map's prior-week PWH/PWL keyed each native daily row by its OPEN
// STAMP, which is the calendar day BEFORE its trading day: the window took
// Tue..Fri of last week plus THIS week's Monday and dropped last Monday.
func TestR22PriorWeekProjectionUsesTradingDays(t *testing.T) {
	daily := r22Native1d(t)

	// Wed 2026-10-07 10:00 CT — prior week = trading days Mon 09-28 → Fri 10-02
	// = native rows stamped Sun 09-27 … Thu 10-01:
	//   H = max(30920.75, 30725.25, 30906.00, 31150.25, 31282.75) = 31282.75
	//   L = min(30356.50, 30371.75, 30511.75, 30531.50, 30760.75) = 30356.50
	// (the open-stamp window read rows 09-28 … 10-04 → 31371.00 / 30371.75,
	// i.e. it took THIS week's Monday 10-05 high and dropped last Monday's low).
	now := time.Date(2026, 10, 7, 10, 0, 0, 0, CTLocation())
	r22ServeAt(t, daily, nil, now)
	pwh, pwl, method := r22PWHL(t, now)
	if pwh != 31282.75 || pwl != 30356.50 {
		t.Errorf("Wed 10-07: PWH %.2f PWL %.2f (%s); want 31282.75 / 30356.50 (trading days Mon 09-28 → Fri 10-02)", pwh, pwl, method)
	}

	// Sun 2026-10-11 18:00 CT — the new week's session is OPEN (it belongs to
	// Monday 10-12), so the prior week is the one that just closed: Mon 10-05
	// → Fri 10-09 = rows 10-04 … 10-08 → H 31616.50, L 30792.00.
	now = time.Date(2026, 10, 11, 18, 0, 0, 0, CTLocation())
	r22ServeAt(t, daily, nil, now)
	pwh, pwl, method = r22PWHL(t, now)
	if pwh != 31616.50 || pwl != 30792.00 {
		t.Errorf("Sun 10-11 18:00 (Monday's session open): PWH %.2f PWL %.2f (%s); want 31616.50 / 30792.00 (the week that just closed)", pwh, pwl, method)
	}
}

// P2-B (consumer side) — the cache's fixed-24h open stamp moves across DST;
// the trading date recovered from it must not.
func TestR22NativeDailyTradingDateAcrossDST(t *testing.T) {
	loc := CTLocation()
	cases := []struct {
		closeStamp time.Time // NT8's stamp: 00:00 CT of the trading date
		want       string
	}{
		{time.Date(2026, 10, 13, 0, 0, 0, 0, loc), "2026-10-13"}, // ordinary
		{time.Date(2026, 11, 2, 0, 0, 0, 0, loc), "2026-11-02"},  // after fall-back: open stamp Sun 01:00 CDT
		{time.Date(2027, 3, 15, 0, 0, 0, 0, loc), "2027-03-15"},  // after spring-forward (Sun 03-14): open stamp SAT 23:00 CST
		{time.Date(2026, 11, 27, 0, 0, 0, 0, loc), "2026-11-27"}, // half-day
	}
	for _, c := range cases {
		b := market.Kline{OpenTime: c.closeStamp.Add(-24 * time.Hour).UnixMilli()} // exactly what openStampBars stores
		td := NativeDailyTradingDate(b)
		if got := td.Format("2006-01-02"); got != c.want {
			t.Errorf("open stamp %s → trading date %s, want %s",
				time.UnixMilli(b.OpenTime).In(loc).Format("Mon 01-02 15:04 MST"), got, c.want)
		}
		days := SessionDaysFromNativeDaily([]market.Kline{b})
		open := time.UnixMilli(days[0].OpenTime).In(loc)
		if open.Hour() != 17 || open.AddDate(0, 0, 1).Format("2006-01-02") != c.want {
			t.Errorf("session-day stamp %s is not 17:00 CT the day before %s", open.Format("Mon 01-02 15:04 MST"), c.want)
		}
		if weekStartMonday(open).Format("2006-01-02") != weekStartMonday(td).Format("2006-01-02") {
			t.Errorf("%s: the session-day bar lands in week %s, its trading date in %s",
				c.want, weekStartMonday(open).Format("01-02"), weekStartMonday(td).Format("01-02"))
		}
	}
}

// dATR — at 01:44 CT 10-14 the 2,000-bar ring held 96 bars of the 10-11
// session (range 45.00), all 1,380 of 10-12 (314.25) and 524 of the
// developing 10-13 session. The proxy averaged the 96-bar stub as a complete
// day: (45.00 + 314.25)/2 = 179.62 — logged live as "proximity band ±179pt"
// (vl_2026-10-13.log, 10-14 01:44:05). The two COMPLETE sessions it spans are
// 10-11 (native 1d H 31290.00 L 30842.50 = 447.50) and 10-12 (314.25): ~380.
func TestR22DATRFromCompleteSessions(t *testing.T) {
	now := time.Date(2026, 10, 14, 1, 44, 0, 0, CTLocation())
	bars := r22Bars1m(t)
	r22ServeAt(t, r22Native1d(t), bars, now)

	_, _, dATR := AssembleScoredLevels("r22", bars, DefaultSessionRegistry(), "MNQ", 12, now, 1)
	t.Logf("dATR at 10-14 01:44 CT = %.2f", dATR)
	if math.Abs(dATR-380.875) > 1.0 {
		t.Errorf("dATR = %.2f; want ~380.9 (mean of the two COMPLETE sessions 10-11 447.50 and 10-12 314.25) — "+
			"a truncated 96-bar stub averaged as a full day gives 179.62", dATR)
	}
}

// dATR, intraday fallback (no daily source answers): the front-truncated
// 96-bar stub of the 10-11 session is NOT a complete day and must not be
// averaged. The only complete session in the ring is 10-12 (H−L 314.25).
func TestR22DATRIntradayFallbackDropsTruncatedDay(t *testing.T) {
	now := time.Date(2026, 10, 14, 1, 44, 0, 0, CTLocation())
	bars := r22Bars1m(t)
	r22ServeAt(t, nil, bars, now) // no native 1d

	_, _, dATR := AssembleScoredLevels("r22", bars, DefaultSessionRegistry(), "MNQ", 12, now, 1)
	if math.Abs(dATR-314.25) > 1e-9 {
		t.Errorf("intraday-fallback dATR = %.2f; want 314.25 (the one complete session 10-12) — "+
			"179.62 means the 96-bar stub of 10-11 was averaged as a full day", dATR)
	}
}

func r22VolumeKinds(levels []DetectedLevel) map[LevelKind][]float64 {
	out := map[LevelKind][]float64{}
	for _, l := range levels {
		out[l.Kind] = append(out[l.Kind], l.Price)
	}
	return out
}

// F-1 — Monday's "prior day" is the prior CME SESSION (Friday), not the
// calendar day before the Sunday 17:00 open (Saturday, which has no bars).
// Real ring: the 2,000 closed 1m bars before Mon 2026-10-12 08:07 CT (1,095
// of Friday's session 10-08, 905 of Monday's). Oracles (independent Python
// over the same rows): pdVWAP 31103.9049, SETT 31123.25 (Fri 15:59 close),
// session VWAP 31154.0093, eVWAP anchored at FRIDAY 15:00 31152.0020.
func TestR22MondayPriorDayIsFridaysSession(t *testing.T) {
	now := time.Date(2026, 10, 12, 8, 7, 0, 0, CTLocation())
	bars := r22LoadBars(t, "mnq1226_1m_2000_before_20261012_0807ct.json")
	for i := range bars {
		bars[i].CloseTime = bars[i].OpenTime + 60_000 - 1
	}
	got := r22VolumeKinds(VolumeLevels(bars, now))

	// N4 (release #23): this ring holds only 1,095 of Friday's 1,380 bars — a
	// FRONT-TRUNCATED session. pdVWAP 31103.90 and the profile were computed
	// from that tail (the rescan's oracle: +11.33 against the whole session at
	// 08:30); with no store to read the whole session from, both are now
	// REFUSED rather than drawn wrong. The whole-session read is pinned in
	// levels_volume_n4_test.go.
	// m239a: SETT is the native 1d close (the ~15:00 CT settle). The symbol-less
	// path has no daily source, so SETT falls back to the prior session's last
	// 1m print — LABELLED "SETT·last print", never the settle. That last print
	// (31123.25) is known even though the session is front-truncated (the
	// fallback reads the slice directly, not the truncation-guarded reader).
	if v := got[KindPDVWAP]; len(v) != 0 {
		t.Errorf("Monday pdVWAP = %v from a front-truncated Friday session; want it refused (N4)", v)
	}
	if v := got[KindSETT]; len(v) != 1 || v[0] != 31123.25 {
		t.Errorf("Monday SETT = %v; want [31123.25] (m239a: no native 1d close → last 1m print fallback)", v)
	}
	if len(got[KindVAH]) != 0 || len(got[KindVAL]) != 0 {
		t.Errorf("Monday prior-day profile drawn from a front-truncated Friday session; want it refused (N4)")
	}
	ev := got[KindEVWAP]
	if len(ev) != 1 || math.Abs(ev[0]-31152.0020) > 0.01 {
		t.Errorf("Monday eVWAP = %v; want [31152.00] (anchored at FRIDAY's 15:00 cash close) — 31154.01 is the session VWAP duplicate", ev)
	}
}

// P3-1 — MID-O (overnight 17:00 → 08:30 midpoint) vanished every evening:
// from 17:00 to 23:59 the window was [today 17:00, today 08:30] — empty.
func TestR22MIDOPresentInTheEvening(t *testing.T) {
	now := time.Date(2026, 10, 13, 23, 30, 0, 0, CTLocation())
	bars := r22Bars1m(t) // 10-11 17:00 … 10-14 01:43; closedBars(now) keeps up to 23:29
	got := r22VolumeKinds(VolumeLevels(bars, now))
	if len(got[KindMIDO]) != 1 {
		t.Fatalf("MID-O at 23:30 CT = %v; want one level from the 17:00→23:29 overnight so far", got[KindMIDO])
	}
	hi, lo := math.Inf(-1), math.Inf(1)
	start := time.Date(2026, 10, 13, 17, 0, 0, 0, CTLocation()).UnixMilli()
	for _, b := range bars {
		if b.OpenTime >= start && b.CloseTime < now.UnixMilli() {
			hi, lo = math.Max(hi, b.High), math.Min(lo, b.Low)
		}
	}
	if want := (hi + lo) / 2; got[KindMIDO][0] != want {
		t.Errorf("MID-O = %.2f, want %.2f", got[KindMIDO][0], want)
	}
}

// F-2 render side — a configured EMA that is not warmed on this TF's depth
// renders n/a in BOTH prompts (FormatIndicatorState is shared by the executor
// and the planner mirror), never a silently missing row.
func TestR22FormatIndicatorStateUnwarmedEMAIsNA(t *testing.T) {
	var sb strings.Builder
	FormatIndicatorState(&sb, &market.TimeframeSeriesData{EMAByPeriod: map[int][]float64{20: {101.5}, 200: {}}},
		store.IndicatorConfig{EnableEMA: true})
	out := sb.String()
	if !strings.Contains(out, "EMA20: [101.5000]") || !strings.Contains(out, "EMA200: n/a (not warmed — needs 661 bars)") {
		t.Fatalf("want EMA20 values and 'EMA200: n/a (not warmed — needs 661 bars)', got:\n%s", out)
	}
}
