package kernel

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// m239 — three level bugs (CTO value check, independent recompute).
// Pins at the production path, one mutant per fix.

func m239Bar(loc *time.Location, y int, mo time.Month, d, h, mi int, o, hi, lo, c, vol float64) market.Kline {
	open := time.Date(y, mo, d, h, mi, 0, 0, loc)
	return market.Kline{OpenTime: open.UnixMilli(), CloseTime: open.Add(time.Minute).UnixMilli() - 1, Open: o, High: hi, Low: lo, Close: c, Volume: vol}
}

// Fix 1 — PDH/PDL/PDC are the prior CME session (17:00→16:00 CT), not the
// calendar day. At 05:46 CT the current session's evening (the prior calendar
// day's 17:00–23:59) must NOT shape PDH. Mutant: bucket by bt.Format("2006-01-02")
// instead of CMESessionDayKey → the evening high 200 leaks into PDH → RED.
func TestM239PDHSessionDayNotCalendar(t *testing.T) {
	loc := chicago()
	now := time.Date(2026, 10, 21, 5, 46, 0, 0, loc)
	var bars []market.Kline
	// The prior session (10-19 17:00 → 10-20 16:00): 1020 closed bars, high 100.
	for i := 0; i < 1020; i++ {
		s := time.Date(2026, 10, 19, 17, 0, 0, 0, loc).Add(time.Duration(i) * time.Minute)
		bars = append(bars, market.Kline{OpenTime: s.UnixMilli(), CloseTime: s.Add(time.Minute).UnixMilli() - 1, Open: 90, High: 100, Low: 85, Close: 95, Volume: 10})
	}
	// The CURRENT session's evening (10-20 17:00–19:59): a high of 200. With
	// calendar-day bucketing this is the "prior day" 10-20; with session-day
	// bucketing it is the CURRENT session (10-20) and must not be PDH.
	for i := 0; i < 180; i++ {
		s := time.Date(2026, 10, 20, 17, 0, 0, 0, loc).Add(time.Duration(i) * time.Minute)
		bars = append(bars, market.Kline{OpenTime: s.UnixMilli(), CloseTime: s.Add(time.Minute).UnixMilli() - 1, Open: 150, High: 200, Low: 140, Close: 190, Volume: 10})
	}
	got := map[LevelKind]float64{}
	for _, l := range ExtractMultiDayLevels(bars, DefaultSessionRegistry(), now) {
		got[l.Kind] = l.Price
	}
	if got[KindPDH] != 100 {
		t.Fatalf("PDH = %v, want 100 (the prior session 10-19's high, NOT the current session's evening high 200)", got[KindPDH])
	}
	if got[KindPDL] != 85 {
		t.Fatalf("PDL = %v, want 85 (the prior session's low)", got[KindPDL])
	}
	if got[KindPDC] != 95 {
		t.Fatalf("PDC = %v, want 95 (the prior session's last close)", got[KindPDC])
	}
}

// Fix 2 — SETT is the native 1d close (the ~15:00 CT settle), never the
// session's last 1m print. With no daily source it is ABSENT. Mutant: fall back
// to the session's last 1m close → SETT returns the wrong value instead of nil
// → RED.
func TestM239SETTIsNativeDailyClose(t *testing.T) {
	loc := chicago()
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, loc)
	var bars []market.Kline
	for i := 0; i < 1020; i++ {
		s := time.Date(2026, 10, 20, 17, 0, 0, 0, loc).Add(time.Duration(i) * time.Minute)
		bars = append(bars, market.Kline{OpenTime: s.UnixMilli(), CloseTime: s.Add(time.Minute).UnixMilli() - 1, Open: 100, High: 101, Low: 99, Close: 100, Volume: 10})
	}

	// No daily source → SETT must be ABSENT (never the last 1m print).
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = nil
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	if v := levelMap(SETTLevelsFor("MNQ", bars, now)); len(v) != 0 {
		t.Fatalf("SETT with no daily source must be absent, got %v", v)
	}

	// A native 1d bar for the prior session's trading date (10-20) → SETT = its
	// close. The native daily bar's OpenTime + 24h + slack maps to the trading
	// date (weekly_session_days.go).
	settle := 29675.25
	dailyOpen := time.Date(2026, 10, 19, 0, 0, 0, 0, loc)
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		if tf != "1d" {
			return nil
		}
		return []market.Kline{{OpenTime: dailyOpen.UnixMilli(), CloseTime: dailyOpen.Add(24*time.Hour).UnixMilli() - 1, Open: 30000, High: 30114, Low: 29726.75, Close: settle}}
	}
	v := levelMap(SETTLevelsFor("MNQ", bars, now))
	if v[KindSETT] != settle {
		t.Fatalf("SETT = %v, want the native 1d close %.2f", v[KindSETT], settle)
	}
}

// Fix 3 — one POC: the prior-day profile uses the SAME SVP definition as the
// chart (1.25-pt rows, volume spread by range overlap), so there is no second
// close-binned POC 7+ pts away. Mutant: revert profileLevels to the 120
// close-binned histogram → the POC diverges from the SVP POC → RED.
func TestM239POCMatchesSVP(t *testing.T) {
	loc := chicago()
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, loc)
	var bars []market.Kline
	// A distribution with clear max-volume at the 29853.125 row (SVPRowHeight
	// 1.25) and volume spread across the range — enough shape that the 120-bin
	// close profile and the SVP rows would disagree.
	for i := 0; i < 120; i++ {
		s := time.Date(2026, 10, 20, 17, 0, 0, 0, loc).Add(time.Duration(i) * time.Minute)
		bars = append(bars, market.Kline{OpenTime: s.UnixMilli(), CloseTime: s.Add(time.Minute).UnixMilli() - 1, Open: 29850, High: 29860, Low: 29840, Close: 29853, Volume: 50})
	}
	prof := BuildSVPProfile(bars, now)
	if len(prof.Sessions) == 0 {
		t.Fatal("SVP fixture empty")
	}
	wantPOC := prof.Sessions[0].POC

	got := levelMap(profileLevels(bars, "2026-10-20", ""))
	if got[KindPOC] != wantPOC {
		t.Fatalf("profileLevels POC = %v, want the SVP POC %v (one definition)", got[KindPOC], wantPOC)
	}
	// The SVP POC sits on the 1.25-pt row grid: (n+0.5)*1.25.
	if math.Mod(wantPOC, SVPRowHeight) != 0.625 {
		t.Fatalf("SVP POC %.4f is not a row midpoint on the %.2f-pt grid", wantPOC, SVPRowHeight)
	}
}
