package kernel

import (
	"math"
	"time"

	"vl/market"
)

// P1.1 — session-tagged bars → multi-day level extractor.
//
// Pure + deterministic: given intraday bars, the session registry, and `now`,
// derive the day-trader map's structural levels (facts=Go). Bars are tagged by
// CME session (via the P0.3 registry) and grouped by CT calendar day / futures
// session-day, per the contract's level naming (PDH/PDL/PDC = the prior CME
// session, 17:00→16:00 CT — m239).
//
// Only levels that HAVE data are emitted — a missing window (cold start, thin
// cache) simply omits that level, which the WARMING badge surfaces honestly. No
// backfill: the map warms forward as the durable session-profile store (P1.3)
// accumulates.

type calDayAgg struct {
	high, low       float64
	closePx         float64
	closeOpenTime   int64
	closeTime       int64 // recording only; never selects a price
	rthCloseTime    int64
	rthCount        int
	rthHigh, rthLow float64
	hasRTH          bool
	count           int // closed bars in this CME session-day bucket (m239)
}

func newCalDayAgg() *calDayAgg {
	return &calDayAgg{
		high: math.Inf(-1), low: math.Inf(1),
		rthHigh: math.Inf(-1), rthLow: math.Inf(1),
	}
}

// ExtractMultiDayLevels returns the structural map levels relative to `now`:
// PDH/PDL/PDC (prior CME session day, 17:00→16:00 CT — m239), RTH-H/L (that
// session's NY session), AS/LDN/ON (the current overnight of now's futures
// session-day), and PW/PM (prior week / month). Closed bars only. PDC reads the
// native 1d close when the symbol is known (m239a) — see ExtractMultiDayLevelsFor.
func ExtractMultiDayLevels(bars []market.Kline, reg SessionRegistry, now time.Time) []DetectedLevel {
	return ExtractMultiDayLevelsFor("", bars, reg, now)
}

// ExtractMultiDayLevelsFor is ExtractMultiDayLevels for a named instrument: PDC
// is the SAME source as SETT (the native 1d close). With no native daily row
// PDC is the prior session's last 1m close, labelled "PDC·last print".
func ExtractMultiDayLevelsFor(symbol string, bars []market.Kline, reg SessionRegistry, now time.Time) []DetectedLevel {
	loc := chicago()
	nowMs := now.UnixMilli()
	nowCT := now.In(loc)
	nowFut := CMESessionDayKey(now)

	weekStart := startOfWeekCT(nowCT)
	priorWeekStart := weekStart.AddDate(0, 0, -7)
	priorWeekEnd := weekStart
	monthStart := time.Date(nowCT.Year(), nowCT.Month(), 1, 0, 0, 0, 0, loc)
	priorMonthStart := monthStart.AddDate(0, -1, 0)
	priorMonthEnd := monthStart

	cal := map[string]*calDayAgg{} // CME session-day buckets (PDH/PDL/PDC/RTH)
	calDays := map[string]int{}    // calendar-day bar counts (PW/PM coverage only)
	var asH, asL, ldnH, ldnL float64 = math.Inf(-1), math.Inf(1), math.Inf(-1), math.Inf(1)
	var hasAS, hasLDN bool
	var asClose, ldnClose, pwClose, pmClose int64
	var asCount, ldnCount, pwCount, pmCount int
	var pwH, pwL float64 = math.Inf(-1), math.Inf(1)
	var pmH, pmL float64 = math.Inf(-1), math.Inf(1)
	var hasPW, hasPM bool

	for i := range bars {
		b := bars[i]
		if b.CloseTime >= nowMs {
			continue // closed bars only
		}
		bt := time.UnixMilli(b.OpenTime).In(loc)
		// m239: PDH/PDL/PDC bucket by the CME session day (17:00→16:00 CT), NOT
		// the calendar day — a calendar bucket mixes the prior session's evening
		// into the next day's levels. PDC = that session's last close.
		sessKey := CMESessionDayKey(bt)
		calDays[bt.Format("2006-01-02")]++

		a := cal[sessKey]
		if a == nil {
			a = newCalDayAgg()
			cal[sessKey] = a
		}
		a.count++
		if b.High > a.high {
			a.high = b.High
		}
		if b.Low < a.low {
			a.low = b.Low
		}
		if b.OpenTime >= a.closeOpenTime {
			a.closePx = b.Close
			a.closeOpenTime = b.OpenTime
			a.closeTime = b.CloseTime
		}

		session := ""
		if s, ok := reg.ActiveSession(bt); ok {
			session = s.Name
		}
		if session == SessionNY {
			a.rthCount++
			if b.CloseTime > a.rthCloseTime {
				a.rthCloseTime = b.CloseTime
			}
			a.hasRTH = true
			if b.High > a.rthHigh {
				a.rthHigh = b.High
			}
			if b.Low < a.rthLow {
				a.rthLow = b.Low
			}
		}

		// Overnight of the CURRENT futures session-day.
		if CMESessionDayKey(bt) == nowFut {
			switch session {
			case SessionAsia:
				asCount++
				if b.CloseTime > asClose {
					asClose = b.CloseTime
				}
				hasAS = true
				if b.High > asH {
					asH = b.High
				}
				if b.Low < asL {
					asL = b.Low
				}
			case SessionLondon:
				ldnCount++
				if b.CloseTime > ldnClose {
					ldnClose = b.CloseTime
				}
				hasLDN = true
				if b.High > ldnH {
					ldnH = b.High
				}
				if b.Low < ldnL {
					ldnL = b.Low
				}
			}
		}

		if !bt.Before(priorWeekStart) && bt.Before(priorWeekEnd) {
			pwCount++
			if b.CloseTime > pwClose {
				pwClose = b.CloseTime
			}
			hasPW = true
			if b.High > pwH {
				pwH = b.High
			}
			if b.Low < pwL {
				pwL = b.Low
			}
		}
		if !bt.Before(priorMonthStart) && bt.Before(priorMonthEnd) {
			pmCount++
			if b.CloseTime > pmClose {
				pmClose = b.CloseTime
			}
			hasPM = true
			if b.High > pmH {
				pmH = b.High
			}
			if b.Low < pmL {
				pmL = b.Low
			}
		}
	}

	var out []DetectedLevel

	// Prior CME session day (PDH/PDL/PDC + that session's RTH). Coverage-guarded
	// (P0.4-F, 2026-08-25): a truncated bucket — bot down part of the day, or a
	// fresh boot whose 1m ring only reaches the prior session's afternoon — yields
	// a WRONG low/high/close. A session with < priorDayMinBars closed bars is
	// OMITTED.
	if priorSess := mostRecentPriorDay(cal, nowFut, priorDayMinBars); priorSess != "" {
		a := cal[priorSess]
		// m239a: PDC is the SAME source as SETT — the native 1d close (the
		// ~15:00 CT settle). With no native daily row it falls back to the
		// session's last 1m close, labelled "PDC·last print", never a settle.
		// The prior session's trading date is its start date + 1 day.
		priorStart, _ := time.ParseInLocation("2006-01-02", priorSess, chicago())
		pdcTradingDate := priorStart.AddDate(0, 0, 1).Format("2006-01-02")
		pdcPx, pdcLabel := a.closePx, "PDC·last print"
		if settle, _ := nativeDailyCloseFor(symbol, pdcTradingDate); settle > 0 {
			pdcPx, pdcLabel = settle, "PDC"
		}
		out = append(out,
			lineLevel(KindPDH, a.high, "PDH", priorSess, true),
			lineLevel(KindPDL, a.low, "PDL", priorSess, true),
			lineLevel(KindPDC, pdcPx, pdcLabel, priorSess, true),
		)
		if a.hasRTH {
			out = append(out,
				lineLevel(KindRTHH, a.rthHigh, "RTH-H", priorSess, false),
				lineLevel(KindRTHL, a.rthLow, "RTH-L", priorSess, false),
			)
		}
	}

	// Current overnight (Asia / London / composite ON).
	nowFutDate := CMESessionDayStart(now).Format("2006-01-02")
	if hasAS {
		out = append(out,
			lineLevel(KindASH, asH, "AS-H", nowFutDate, false),
			lineLevel(KindASL, asL, "AS-L", nowFutDate, false),
		)
	}
	if hasLDN {
		out = append(out,
			lineLevel(KindLDNH, ldnH, "LDN-H", nowFutDate, false),
			lineLevel(KindLDNL, ldnL, "LDN-L", nowFutDate, false),
		)
	}
	if hasAS || hasLDN {
		onH := math.Max(asH, ldnH)
		onL := math.Min(asL, ldnL)
		out = append(out,
			lineLevel(KindONH, onH, "ONH", nowFutDate, false),
			lineLevel(KindONL, onL, "ONL", nowFutDate, false),
		)
	}

	// Prior week / month (HTF). Coverage-guarded (P0.4-F): the 1m ring holds
	// only ~33h, so a "prior week" bucket built from it is at best a few
	// hours wide — emitting PWH/PWL from that window fabricated supply like
	// the bogus PWL 29220.25 that seven NY plans traded as overhead supply.
	// These HTF anchors need real multi-day coverage; thin buckets are
	// omitted (the durable session-profile store is their proper source).
	if hasPW && pwCovered(calDays, priorWeekStart, priorWeekEnd, priorWeekMinBars) {
		wLabel := priorWeekStart.Format("2006-01-02")
		out = append(out,
			lineLevel(KindPWH, pwH, "PWH", wLabel, true),
			lineLevel(KindPWL, pwL, "PWL", wLabel, true),
		)
	}
	if hasPM && pmCovered(calDays, priorMonthStart, priorMonthEnd, priorMonthMinBars) {
		mLabel := priorMonthStart.Format("2006-01")
		out = append(out,
			lineLevel(KindPMH, pmH, "PMH", mLabel, true),
			lineLevel(KindPML, pmL, "PML", mLabel, true),
		)
	}

	for i := range out {
		l := out[i]
		closeMs, count := int64(0), 0
		switch l.Kind {
		case KindPDH, KindPDL, KindPDC:
			if a := cal[l.OriginDate]; a != nil {
				closeMs, count = a.closeTime, a.count
			}
		case KindRTHH, KindRTHL:
			if a := cal[l.OriginDate]; a != nil {
				closeMs, count = a.rthCloseTime, a.rthCount
			}
		case KindASH, KindASL:
			closeMs, count = completedSessionSourceClose(asClose, SessionAsia, reg, now), asCount
		case KindLDNH, KindLDNL:
			closeMs, count = completedSessionSourceClose(ldnClose, SessionLondon, reg, now), ldnCount
		case KindONH, KindONL:
			closeMs, count = completedSessionSourceClose(ldnClose, SessionLondon, reg, now), asCount+ldnCount
		case KindPWH, KindPWL:
			closeMs, count = pwClose, pwCount
		case KindPMH, KindPML:
			closeMs, count = pmClose, pmCount
		}
		out[i] = WithFormationClose(l, closeMs, count, "existing_bucket_last_source_close", now)
	}
	return out
}

// Coverage minimums for multi-day buckets (P0.4-F, 2026-08-25): a prior-day
// bucket under 15h of closed 1m bars is truncated (boot gap / bot-down window)
// and its low/high/close are unreliable. Prior-week needs ≥3 full days and
// prior-month ≥7 full days — with the ~33h 1m ring these can never pass, which
// is CORRECT: those anchors must come from a multi-day source, not the ring.
const (
	priorDayMinBars   = 900   // 15h of 1m bars
	priorWeekMinBars  = 4320  // 3 days
	priorMonthMinBars = 10080 // 7 days
)

// mostRecentPriorDay returns the latest calendar-day key strictly before today
// that has data AND meets the coverage minimum, or "" if none.
func mostRecentPriorDay(cal map[string]*calDayAgg, todayCal string, minBars int) string {
	best := ""
	for k, a := range cal {
		if k < todayCal && k > best && a.count >= minBars {
			best = k
		}
	}
	return best
}

// pwCovered reports whether the [start, end) week window has ≥ minBars closed
// bars across its calendar-day counts.
func pwCovered(calDays map[string]int, start, end time.Time, minBars int) bool {
	n := 0
	for k, c := range calDays {
		d, err := time.ParseInLocation("2006-01-02", k, chicago())
		if err != nil {
			continue
		}
		if !d.Before(start) && d.Before(end) {
			n += c
		}
	}
	return n >= minBars
}

// pmCovered reports whether the [start, end) month window has ≥ minBars closed
// bars across its calendar-day counts.
func pmCovered(calDays map[string]int, start, end time.Time, minBars int) bool {
	return pwCovered(calDays, start, end, minBars)
}

// startOfWeekCT returns Monday 00:00 CT of the CME week t belongs to: Sunday
// rolls FORWARD to the next Monday (the CME week opens Sunday 17:00 CT, so a
// Sunday-evening instant belongs to the week starting the NEXT day). Same
// convention as weekStartMonday — the two must agree or PWH/PWL drift a week
// on Sunday evening [bug-hunt #32].
func startOfWeekCT(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	if wd := d.Weekday(); wd == time.Sunday {
		return d.AddDate(0, 0, 1)
	}
	return d.AddDate(0, 0, -int(d.Weekday()-time.Monday))
}
