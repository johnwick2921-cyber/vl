package kernel

// R22 TIME-WEEKLY (2026-10-14) — CME SESSION DAYS FROM NATIVE DAILY BARS.
//
// THE DEFECT THIS CLOSES (P1-A). The weekly read resolved "1w" through the bar
// resolver, which rolled native daily bars into 10080-minute buckets floored
// to the Unix epoch — and 1970-01-01 was a THURSDAY. Every "week" the reader
// saw ran Thursday 00:00 UTC → Thursday, and CompletedWeekCandles re-labelled
// each one with a Monday. The live doc for week 2026-10-12 carried PWL
// 30760.75 (last Friday-but-one's low) and PWC 30969.50 (a THURSDAY close);
// the truth is 30792.00 / 31108.75. Weekly bars also starved IPDA (17 "days")
// and NWOG (no bar ever sat on a Friday or a Sunday evening), so both
// sections were always empty.
//
// THE CONVENTION. A CME week runs Sunday 17:00 CT (first print) → Friday
// 16:00 CT (last print). The weekly code (weekStartMonday, LastNWOGs,
// DailySessionBars) already speaks that convention for any bar stamped at
// its OPEN. A native daily bar is NOT stamped at its open: NT8 stamps it at
// its END, 00:00 CT of its trading date, and the cache converts that to an
// "open" by subtracting a fixed 24h (provider/ninjatrader/bar_cache.go
// openStampBars). So the row for trading day Tue 10-13 sits at Mon 10-12
// 00:00 CT, and across a DST change the fixed 24h lands it on 01:00 CDT or
// on the PREVIOUS calendar day (Mon 2027-03-08 → Sat 23:00 CST).
//
// SessionDaysFromNativeDaily re-stamps every native daily bar at its REAL
// session open — 17:00 CT the calendar day before its trading date — so the
// existing weekly vocabulary buckets it exactly as it buckets 1m bars. One
// convention (the bar's own open), one chokepoint, and DST cannot move it.

import (
	"sort"
	"time"

	"vl/market"
)

// nativeDailySlack absorbs the DST day length when the cache's fixed-24h open
// stamp is mapped back to the trading date. The close stamp (open + 24h) is
// 00:00 CT of the trading date; +2h keeps the answer on that date for a 23h,
// 24h or 25h day, and for a provider that stamps the close at 16:00 CT.
const nativeDailySlack = 2 * time.Hour

// NativeDailyTradingDate returns the CME TRADING date (00:00 CT) a
// provider-native daily bar represents. See the file comment for the stamp
// convention it inverts.
func NativeDailyTradingDate(b market.Kline) time.Time {
	t := time.UnixMilli(b.OpenTime).Add(24*time.Hour + nativeDailySlack).In(CTLocation())
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, CTLocation())
}

// SessionOpenForTradingDate is the 17:00 CT session open of a trading date:
// the calendar day before it (Sunday for Monday; Monday 17:00 for Tuesday
// after a closed Monday).
func SessionOpenForTradingDate(td time.Time) time.Time {
	d := td.In(CTLocation())
	return time.Date(d.Year(), d.Month(), d.Day()-1, 17, 0, 0, 0, CTLocation())
}

// SessionDaysFromNativeDaily converts provider-native daily bars into CME
// session-day bars stamped at their session OPEN (17:00 CT the day before the
// trading date) with CloseTime at the trading date's 16:00 CT close. Output is
// ascending, one bar per trading date (a later duplicate replaces an earlier
// one — the cache's newest snapshot wins).
func SessionDaysFromNativeDaily(raw []market.Kline) []market.Kline {
	if len(raw) == 0 {
		return nil
	}
	byDate := make(map[string]market.Kline, len(raw))
	for _, b := range raw {
		td := NativeDailyTradingDate(b)
		c := b
		c.OpenTime = SessionOpenForTradingDate(td).UnixMilli()
		c.CloseTime = time.Date(td.Year(), td.Month(), td.Day(), 16, 0, 0, 0, CTLocation()).UnixMilli() - 1
		byDate[td.Format("2006-01-02")] = c
	}
	out := make([]market.Kline, 0, len(byDate))
	for _, b := range byDate {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenTime < out[j].OpenTime })
	return out
}

// sessionTradingWeekday is the weekday of the TRADING date the bar's session
// belongs to: a bar opening at/after 17:00 CT trades for the next calendar
// day. Works for 1m bars and for session-open-stamped daily bars alike.
func sessionTradingWeekday(openMs int64) time.Weekday {
	return CMESessionDayStart(time.UnixMilli(openMs)).AddDate(0, 0, 1).Weekday()
}
