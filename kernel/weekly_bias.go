package kernel

// WEEKLY-BIAS WAVE (2026-08-30) — W1 weekly computation layer. Pure Go, derives
// everything from the certified stored 1m bars table; zero new data vendors.
//
// Boundaries: the week runs Sunday 17:00 CT (first print) → Friday 16:00 CT
// (last print) — the SAME roll convention as IsCMEOpen/CMESessionDayKey
// (cme_calendar.go). No second boundary helper is invented here.
//
// GOLDEN RULE (repaint law): the in-progress week is NEVER part of bias
// evidence. CompletedWeekCandles excludes it by construction; the fixture
// proves it with current-week bars present in the input.
//
// Structure tags vs the predecessor: HH (higher high + higher low) ·
// outside (higher high + lower low — the engulfing "HL") · LL (lower high +
// lower low) · LH (lower high + higher low) · inside (contained).

import (
	"fmt"
	"strings"
	"time"

	"vl/market"
)

// WeekCandle is one COMPLETED CME week's OHLCV + structure tag.
type WeekCandle struct {
	WeekStart string  // "2006-01-02" — the week's Monday (governing date)
	Open      float64 // Sunday 17:00 CT first print
	High      float64
	Low       float64
	Close     float64 // Friday ≤16:00 CT last print
	Volume    float64
	StructTag string // HH | outside | LL | LH | inside
}

// WeeklyRefs are the single-week reference levels: weekly_open is the CURRENT
// week's first print (allowed as a LEVEL only — never bias evidence until the
// A-rules validate it); PWH/PWL/PWC come from the PRIOR completed week.
type WeeklyRefs struct {
	WeeklyOpen float64
	PWH        float64
	PWL        float64
	PWC        float64
}

// NWOG is a weekend gap: Friday's last print (≤16:00 CT) → Sunday's first print
// (≥17:00 CT). CE is the gap midpoint; Filled = any 1m bar traded through CE
// since birth.
type NWOG struct {
	Born   string // "2006-01-02" — the Sunday the gap was born
	Hi     float64
	Lo     float64
	CE     float64
	Filled bool
}

// IPDARange is one trailing trading-day range (20/40/60) + the current price's
// premium/discount position inside it (0.0 = at low, 1.0 = at high, -1 = not
// enough history — rendered "insufficient", never fake numbers).
type IPDARange struct {
	Days   int
	High   float64
	Low    float64
	PosPct float64
}

// weekStartMonday returns the Monday governing the CME week containing t,
// anchored on the CALENDAR date (not the session-day start — Sunday MORNING's
// session-day is Saturday's, which would mis-map the week). The Sunday 17:00 CT
// session belongs to the FOLLOWING Monday's week (Sunday's prints are the new
// week's first prints).
func weekStartMonday(t time.Time) time.Time {
	ct := t.In(CTLocation())
	d := time.Date(ct.Year(), ct.Month(), ct.Day(), 0, 0, 0, 0, CTLocation())
	if wd := d.Weekday(); wd == time.Sunday {
		return d.AddDate(0, 0, 1)
	}
	return d.AddDate(0, 0, -int(d.Weekday()-time.Monday))
}

// weekCompletedAt reports whether the week governing `start` (its Monday) has
// COMPLETED by `now`: that week's Friday 16:00 CT close is strictly past.
func weekCompletedAt(start, now time.Time) bool {
	friday := start.AddDate(0, 0, 4)
	closeAt := time.Date(friday.Year(), friday.Month(), friday.Day(), 16, 0, 0, 0, CTLocation())
	return now.After(closeAt)
}

// CompletedWeekCandles aggregates 1m bars into COMPLETED CME weeks (oldest →
// latest), excluding the in-progress week (repaint law). Returns at most `max`
// candles; max ≤ 0 → no cap (depth-guard counting).
func CompletedWeekCandles(bars1m []market.Kline, now time.Time, max int) []WeekCandle {
	type agg struct {
		start                  time.Time
		open, high, low, close float64
		vol                    float64
	}
	m := map[string]*agg{}
	var order []string
	for _, b := range bars1m {
		key := weekStartMonday(time.UnixMilli(b.OpenTime)).Format("2006-01-02")
		a := m[key]
		if a == nil {
			m[key] = &agg{start: weekStartMonday(time.UnixMilli(b.OpenTime)), open: b.Open, high: b.High, low: b.Low, close: b.Close, vol: b.Volume}
			order = append(order, key)
			continue
		}
		if b.High > a.high {
			a.high = b.High
		}
		if b.Low < a.low {
			a.low = b.Low
		}
		a.close = b.Close
		a.vol += b.Volume
	}
	var out []WeekCandle
	for _, key := range order {
		a := m[key]
		if !weekCompletedAt(a.start, now) {
			continue // GOLDEN RULE — in-progress week is never bias evidence
		}
		out = append(out, WeekCandle{WeekStart: key, Open: a.open, High: a.high, Low: a.low, Close: a.close, Volume: a.vol})
	}
	if max > 0 && len(out) > max {
		out = out[len(out)-max:]
	}
	for i := 1; i < len(out); i++ {
		prev := out[i-1]
		hh := out[i].High > prev.High
		lh := out[i].Low > prev.Low
		hl := out[i].High < prev.High
		ll := out[i].Low < prev.Low
		switch {
		case hh && lh:
			out[i].StructTag = "HH"
		case hh && ll:
			out[i].StructTag = "outside"
		case hl && ll:
			out[i].StructTag = "LL"
		case hl && lh:
			out[i].StructTag = "LH"
		default:
			out[i].StructTag = "inside"
		}
	}
	return out
}

// PriorWeekRefs returns weekly_open (current week's first print — level-only),
// PWH/PWL/PWC from the PRIOR COMPLETED week. ok=false when no completed week
// exists yet (thin history).
func PriorWeekRefs(bars1m []market.Kline, now time.Time) (WeeklyRefs, bool) {
	var refs WeeklyRefs
	for _, b := range bars1m {
		if !weekCompletedAt(weekStartMonday(time.UnixMilli(b.OpenTime)), now) && refs.WeeklyOpen == 0 {
			refs.WeeklyOpen = b.Open
		}
	}
	weeks := CompletedWeekCandles(bars1m, now, 1)
	if len(weeks) == 0 {
		return refs, false
	}
	p := weeks[len(weeks)-1]
	refs.PWH, refs.PWL, refs.PWC = p.High, p.Low, p.Close
	return refs, true
}

// LastNWOGs computes the last `n` weekend gaps over the whole bars table
// (completed and current weeks — the gap is born, not completed).
// CE = midpoint; Filled = any 1m bar traded through CE since the gap's birth.
func LastNWOGs(bars1m []market.Kline, now time.Time, n int) []NWOG {
	if n <= 0 {
		n = 5
	}
	type gap struct {
		prevFriClose float64
		prevFriTime  int64
		sunOpen      float64
		sunOpenTime  int64
	}
	gaps := map[string]*gap{}
	var order []string
	for _, b := range bars1m {
		t := time.UnixMilli(b.OpenTime)
		ct := t.In(CTLocation())
		key := weekStartMonday(t).Format("2006-01-02")
		// R22 TIME-WEEKLY: the Friday side is classified by the bar's TRADING
		// day (the session it belongs to), not the wall-clock weekday of its
		// stamp. For 1m bars the last Friday-session print is still Fri ≤16:00
		// CT (the last one wins). For DAILY session bars — stamped at their
		// 17:00 CT open, so Friday's day sits on THURSDAY 17:00 — only the
		// trading-day rule finds it; the wall-clock rule found nothing and the
		// weekly read's NWOG section was always empty.
		friday := sessionTradingWeekday(b.OpenTime) == time.Friday
		if friday {
			// The Friday close pairs with the FOLLOWING Sunday's open — the gap
			// belongs to the next week (born at that Sunday 17:00 CT).
			key = weekStartMonday(t).AddDate(0, 0, 7).Format("2006-01-02")
		}
		switch {
		case friday: // Friday-session last prints (≤16:00 CT)
			if gaps[key] == nil {
				gaps[key] = &gap{}
				order = append(order, key)
			}
			gaps[key].prevFriClose = b.Close
			gaps[key].prevFriTime = b.OpenTime
		case ct.Weekday() == time.Sunday && ct.Hour() >= 17: // Sunday first prints (≥17:00 CT) — a daily session bar opens exactly here
			if gaps[key] == nil {
				gaps[key] = &gap{}
				order = append(order, key)
			}
			if gaps[key].sunOpenTime == 0 {
				gaps[key].sunOpen = b.Open
				gaps[key].sunOpenTime = b.OpenTime
			}
		}
	}
	var out []NWOG
	for _, key := range order {
		g := gaps[key]
		if g.sunOpenTime == 0 || g.prevFriTime == 0 {
			continue // incomplete pair (thin edge weeks)
		}
		hi, lo := g.prevFriClose, g.sunOpen
		if lo > hi {
			hi, lo = lo, hi
		}
		og := NWOG{Born: key, Hi: hi, Lo: lo, CE: (hi + lo) / 2}
		for _, b := range bars1m {
			// >= : the birth bar itself counts. For 1m bars that is the first
			// minute; for daily session bars it is the whole Monday session,
			// which a strict > would have skipped.
			if b.OpenTime >= g.sunOpenTime && b.High >= og.CE && b.Low <= og.CE {
				og.Filled = true
				break
			}
		}
		out = append(out, og)
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// DailySessionBars aggregates 1m bars into CME session-day (17:00 CT roll)
// candles — daily = OUR session day, not calendar UTC (spec 1.4).
func DailySessionBars(bars1m []market.Kline) []market.Kline {
	m := map[string]*market.Kline{}
	var order []string
	for _, b := range bars1m {
		key := CMESessionDayKey(time.UnixMilli(b.OpenTime))
		k := m[key]
		if k == nil {
			c := b
			m[key] = &c
			order = append(order, key)
			continue
		}
		if b.High > k.High {
			k.High = b.High
		}
		if b.Low < k.Low {
			k.Low = b.Low
		}
		k.Close = b.Close
		k.Volume += b.Volume
	}
	out := make([]market.Kline, 0, len(order))
	for _, key := range order {
		out = append(out, *m[key])
	}
	return out
}

// IPDA returns the trailing 20/40/60 trading-day highest-high / lowest-low
// ranges + the current price's position within each (% of range). Days with
// insufficient history render PosPct = -1 ("insufficient history").
func IPDA(daily []market.Kline, price float64) []IPDARange {
	out := []IPDARange{}
	for _, n := range []int{20, 40, 60} {
		if len(daily) < n {
			out = append(out, IPDARange{Days: n, PosPct: -1})
			continue
		}
		win := daily[len(daily)-n:]
		hi, lo := win[0].High, win[0].Low
		for _, d := range win {
			if d.High > hi {
				hi = d.High
			}
			if d.Low < lo {
				lo = d.Low
			}
		}
		pos := -1.0
		if hi > lo {
			pos = (price - lo) / (hi - lo)
		}
		out = append(out, IPDARange{Days: n, High: hi, Low: lo, PosPct: pos})
	}
	return out
}

// CompletedWeekCount is the depth guard's numerator: completed weeks in the
// bars table. < 4 → thin_history (conviction forced low by the validator).
func CompletedWeekCount(bars1m []market.Kline, now time.Time) int {
	return len(CompletedWeekCandles(bars1m, now, 0))
}

// RenderRow renders one WeekCandle as a prompt-table row.
func (w WeekCandle) RenderRow() string {
	tag := w.StructTag
	if tag == "" {
		tag = "first"
	}
	return fmt.Sprintf("%s  %.2f  %.2f  %.2f  %.2f  %.0f  %s", w.WeekStart, w.Open, w.High, w.Low, w.Close, w.Volume, tag)
}

// HasDayOfWeekTokens reports whether s contains day-of-week reasoning tokens
// (validator r5 — the folklore law: day-of-week reasoning is FORBIDDEN).
func HasDayOfWeekTokens(s string) bool {
	low := strings.ToLower(s)
	for _, tok := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
		if strings.Contains(low, tok) {
			return true
		}
	}
	return false
}
