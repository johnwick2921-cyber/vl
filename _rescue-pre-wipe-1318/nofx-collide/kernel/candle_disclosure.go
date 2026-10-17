package kernel

import (
	"fmt"
	"strings"
	"time"

	"nofx/market"
)

// ── D1 — HELD-vs-CLAIMED (wave BARS HORIZON, 2026-09-09) ─────────────────────
//
// THE HEADING LIED, AND THE MODEL WAS TOLD TO TRUST IT.
//
// BuildPlannerCandleTablesAt truncates only when the aggregated slice is LONG
// and baked the row count into the title as a literal, so the title said 8 over
// however many rows existed. MEASURED over the stored prompts
// (planner_rejected_prompts, n=54 carrying a Candles block, ids 70..142; and
// n=55 ids 70..143 when re-measured at 18:3x CT the same day — the population
// grows, the shape does not):
//
//	15m  "(last 12)" rendered 12 rows in 54 of 54
//	1h   "(last 12)" rendered 12 rows in 54 of 54
//	4h   "(last 8)"  rendered  8 rows in 54 of 54
//	daily "(last 8)" rendered  2 rows in 19 and 3 rows in 35 — NEVER 8, in 0 of 54
//	                 (re-measured: 2 rows in 20 and 3 rows in 35 — NEVER 8, 0 of 55)
//
// And the oldest daily row was a PARTIAL session candle presented as a whole
// one: DailySessionBars (kernel/weekly_bias.go) takes the first bar it SEES as
// the session Open with no completeness check. Live instance, prompt id 142:
// the oldest daily row is stamped "09-07 02:39" for a session that opened at
// 09-06 17:00 CT. Meanwhile kernel/planner_prompt.go instructs the model
// "On conflict, trust the candles and say so".
//
// So every table now states what it HOLDS against what it ASKED for — including
// the tables that are complete, because a disclosure that appears only on
// failure is one the reader learns to skim past — and every row whose window is
// only partly held is MARKED.
//
// A24 ABSOLUTE: a missing or partial candle is MARKED. Nothing in this file
// synthesises, interpolates or carries forward a bar to complete a window, and
// no O/H/L/C/V a table renders is changed by any of it.
//
// A10: nothing here gates, refuses, blocks or blanks. It only discloses.

// rowCoverageStepMs is the tape's own interval. Coverage is measured in OPEN
// 1m intervals because the 1m tape is what every one of these tables is
// aggregated FROM — a 4h row is not "4 hours of history", it is however many
// 1m bars happened to land in that bucket.
const rowCoverageStepMs int64 = 60_000

// RowCoverage is ONE rendered row measured against the CME session calendar.
//
// UNKNOWN IS NOT ZERO (A24). When Unknown != "" the counts below were never
// computed and must not be read as "nothing missing".
type RowCoverage struct {
	HeldBars      int    // 1m bars that landed inside this row's window
	OpenIntervals int    // OPEN 1m intervals in the same window, bounded by the observable clock; -1 = UNKNOWN
	Forming       bool   // the window extends past the newest observable minute
	Unknown       string // why coverage could not be computed; "" = computed
	WindowOpenMs  int64  // the row's TRUE window start (17:00 CT roll, or the bucket floor)
	FirstHeldMs   int64  // the open time of the first bar actually held in it
}

// Partial reports a computed shortfall — held < the open intervals the window
// contains. False when coverage is UNKNOWN: an uncomputed answer is never a
// clean one.
func (c RowCoverage) Partial() bool {
	return c.Unknown == "" && c.OpenIntervals >= 0 && c.HeldBars < c.OpenIntervals
}

// Marked reports whether this row carries any marker at all.
func (c RowCoverage) Marked() bool {
	return c.Unknown != "" || c.Forming || c.Partial() || c.overHeld()
}

func (c RowCoverage) overHeld() bool {
	return c.Unknown == "" && c.OpenIntervals >= 0 && c.HeldBars > c.OpenIntervals
}

// Marker is the text appended to the rendered row. "" = the window is fully
// held and closed, and the row needs no qualification.
func (c RowCoverage) Marker() string {
	switch {
	case c.Unknown != "":
		return "  ❓COVERAGE-UNKNOWN — " + c.Unknown
	case c.Forming && c.Partial():
		return fmt.Sprintf("  ⏳FORMING ⚠PARTIAL — the window is still open AND holds only %d of the %d open 1m intervals that have CLOSED so far (the minute in progress is counted on neither side)",
			c.HeldBars, c.OpenIntervals)
	case c.Forming:
		return fmt.Sprintf("  ⏳FORMING — the window has not closed; holds %d of %d open 1m intervals that have CLOSED so far (the minute in progress is counted on neither side)",
			c.HeldBars, c.OpenIntervals)
	case c.Partial() && c.FirstHeldMs > c.WindowOpenMs:
		// FRONT TRUNCATED — this is the DailySessionBars defect exactly: the
		// row's Open is whatever bar happened to be first, not the session open.
		return fmt.Sprintf("  ⚠PARTIAL — holds %d of the %d open 1m intervals in this window; its Open is the first bar HELD (%s CT), not the window open (%s CT)",
			c.HeldBars, c.OpenIntervals,
			ClockHHMMCT(time.UnixMilli(c.FirstHeldMs)), ClockHHMMCT(time.UnixMilli(c.WindowOpenMs)))
	case c.Partial():
		// INTERIOR HOLE — the row starts where it should; bars are missing
		// inside it. Saying "its Open is not the window open" here would print
		// the SAME clock twice and state a false reason (found in the live
		// render against the store's 2026-09-09 01:28→13:05 CT hole).
		return fmt.Sprintf("  ⚠PARTIAL — opens at the window open (%s CT) but holds only %d of the %d open 1m intervals; %d are missing INSIDE this window",
			ClockHHMMCT(time.UnixMilli(c.WindowOpenMs)), c.HeldBars, c.OpenIntervals, c.OpenIntervals-c.HeldBars)
	case c.overHeld():
		return fmt.Sprintf("  ℹ️HELD %d bars against %d open 1m intervals — some are stamped inside a closed window",
			c.HeldBars, c.OpenIntervals)
	}
	return ""
}

// ClockHHMMCT renders an instant as "HH:MM" CT. One formatter, so a marker and
// a table row can never disagree about the clock they speak (A11).
func ClockHHMMCT(t time.Time) string { return CloseHHMMCT(t) }

// observableEndMs is the end of the newest 1m interval that has actually CLOSED
// at `now` — i.e. `now` floored to the minute grid.
//
// IT USED TO ADD ONE STEP, AND THAT WAS AN OFF-BY-ONE THAT FIRED ON EVERY
// HEALTHY READ (found in review, 2026-09-09). Rounding UP counted the minute
// still IN PROGRESS as an interval the tape ought to hold; bars are delivered
// on close, so a perfectly gapless tape was short by exactly that one bar and
// the newest row of all four tables rendered "⏳FORMING ⚠PARTIAL — holds only
// 1309 of the 1310 open 1m intervals" while the TAPE line six lines above it
// said "gaps 0". Measured on a gapless 09-08 17:00 → 09-09 14:48 CT tape at
// now=14:49:30 CT: 15m 4 of 5 · 1h 49 of 50 · 4h 229 of 230 · daily 1309 of
// 1310, all four ⚠PARTIAL, all four wrong. A marker on every read is a marker
// on nothing.
//
// THE MINUTE IN PROGRESS IS NOW COUNTED ON NEITHER SIDE: it is excluded from
// the denominator HERE and from the held count in aggregateCoverage /
// sessionCoverage, so the answer is the same whether or not the forming bar
// has been delivered yet, and a REAL tail hole still trips ⚠PARTIAL.
func observableEndMs(now time.Time) int64 {
	ms := now.UnixMilli()
	return ms - mod(ms, rowCoverageStepMs)
}

func mod(a, b int64) int64 {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

// firstOpenGridMs is the first 1m grid instant at or after startMs at which the
// CME is OPEN, snapped to the tape's own grid, or startMs when the window holds
// no open instant at all.
//
// WHY THE RAW BUCKET FLOOR WAS NOT GOOD ENOUGH: a 4h bucket opens on the
// 19:00/23:00/03:00/07:00/11:00/15:00 CT grid, and the Sunday 15:00 bucket
// opens while the market is SHUT — CME reopens at 17:00. Printing "its Open is
// the first bar HELD (17:30 CT), not the window open (15:00 CT)" states a
// reason that is false: 15:00 CT was never an open window, so nothing was
// truncated off the front. Same class as the interior-hole false reason this
// file already fixed, one bucket over.
func firstOpenGridMs(startMs, endMs int64) int64 {
	if IsCMEOpen(time.UnixMilli(startMs)) {
		return startMs
	}
	n := NextCMEOpen(time.UnixMilli(startMs)).UnixMilli()
	if d := mod(n-startMs, rowCoverageStepMs); d != 0 {
		n += rowCoverageStepMs - d
	}
	if n >= endMs || n <= startMs {
		return startMs
	}
	return n
}

// absentRowScanCap bounds the between-row walks below. A walk that exceeds it
// reports UNKNOWN (-1) with the reason, never a truncated number as fact (A24).
// It is a var only so a pin can lower it; production never assigns it.
var absentRowScanCap = 20_000

// ── WHOLE ROWS THAT ARE NOT THERE ───────────────────────────────────────────
//
// A ROW THAT DOES NOT EXIST IS MEASURED BY NOTHING. RowCoverage measures INSIDE
// a rendered row; the heading counts ROWS. AggregateBars emits no row for a
// bucket that holds no bars, so a whole absent bucket is invisible to both, and
// the table declares itself complete across a hole. Reproduced (2026-09-09):
// a tape holding 09-08 18:00–19:00 and 20:00–22:00 CT, market open throughout,
// rendered
//
//	### 15m — HELD 12 of 12 requested rows · every row PRINTED here is COMPLETE
//	09-08 18:45 … / 09-08 20:00 …
//
// with FOUR whole 15m windows missing between two adjacent-looking rows, under
// a prompt that tells the model "On conflict, trust the candles". That is this
// wave's own thesis — a COUNT cannot express a SPAN or a HOLE — reproduced one
// layer down, inside the fix. So the rows that are NOT there are now counted
// and named on the row that follows them.
//
// A CLOSED WINDOW IS NOT AN ABSENT ONE: a bucket with zero OPEN 1m intervals
// (a weekend, a holiday, the daily 16:00–17:00 CT halt) is correctly NOT
// counted — nothing was ever expected there.

// absentAggregateRows returns, per rendered row, how many whole bucket windows
// between it and the row before it hold at least one OPEN 1m interval yet are
// absent from the tape. -1 = UNKNOWN (scan capped). Element 0 is always 0:
// nothing is known about what precedes the first rendered row.
func absentAggregateRows(rows []market.Kline, bucketMs int64) []int {
	out := make([]int, len(rows))
	for i := 1; i < len(rows); i++ {
		n, steps := 0, 0
		for t := rows[i-1].OpenTime + bucketMs; t < rows[i].OpenTime; t += bucketMs {
			steps++
			if steps > absentRowScanCap {
				n = -1
				break
			}
			if open, ok := OpenIntervalsBetween(t, t+bucketMs, rowCoverageStepMs, barHorizonScanCap); ok && open > 0 {
				n++
			}
		}
		out[i] = n
	}
	return out
}

// absentSessionRows is absentAggregateRows for the session-day table, stepping
// the 17:00 CT roll instead of an epoch-floor grid.
func absentSessionRows(rows []market.Kline) []int {
	out := make([]int, len(rows))
	for i := 1; i < len(rows); i++ {
		n, steps := 0, 0
		cur := CMESessionDayStart(time.UnixMilli(rows[i].OpenTime))
		for t := NextSessionRollCT(CMESessionDayStart(time.UnixMilli(rows[i-1].OpenTime))); t.Before(cur); t = NextSessionRollCT(t) {
			steps++
			if steps > absentRowScanCap {
				n = -1
				break
			}
			if open, ok := OpenIntervalsBetween(t.UnixMilli(), NextSessionRollCT(t).UnixMilli(), rowCoverageStepMs, barHorizonScanCap); ok && open > 0 {
				n++
			}
		}
		out[i] = n
	}
	return out
}

// absentRowNote is the marker prepended to the row that FOLLOWS absent rows.
// unit is the row's own name ("15m", "1h", "4h", "session-day").
func absentRowNote(n int, unit string, prevOpenMs, thisOpenMs int64) string {
	switch {
	case n < 0:
		return fmt.Sprintf("  ⛔ABSENT-BEFORE UNKNOWN — the walk between %s CT and %s CT exceeded its cap, so the number of missing %s rows is NOT computed (do not read it as zero)",
			ClockHHMMCT(time.UnixMilli(prevOpenMs)), ClockHHMMCT(time.UnixMilli(thisOpenMs)), unit)
	case n > 0:
		return fmt.Sprintf("  ⛔%d×%s ABSENT BEFORE THIS ROW — the tape holds no bars between %s CT and %s CT, so the row ABOVE is NOT the adjacent window",
			n, unit, ClockHHMMCT(time.UnixMilli(prevOpenMs)), ClockHHMMCT(time.UnixMilli(thisOpenMs)))
	}
	return ""
}

// measureRow measures ONE row window [startMs, endMs) against the calendar.
// `now` is the caller's clock (A28); this function never reads one.
func measureRow(startMs, endMs int64, held int, firstHeldMs int64, now time.Time) RowCoverage {
	c := RowCoverage{HeldBars: held, OpenIntervals: -1, WindowOpenMs: firstOpenGridMs(startMs, endMs), FirstHeldMs: firstHeldMs}
	obs := observableEndMs(now)
	capMs := endMs
	if obs < capMs {
		capMs = obs
		c.Forming = true
	}
	// The calendar answers only for years someone has maintained. An uncovered
	// year is not a year of normal days — it is UNKNOWN, never guessed
	// (kernel/session_calendar.json discipline, inherited here verbatim).
	if st := SessionStateAt(time.UnixMilli(startMs)); st.UncoveredFallback {
		c.Unknown = fmt.Sprintf("%d is not covered by kernel/session_calendar.json — nobody has checked this year, so the open-interval count is not computed",
			time.UnixMilli(startMs).In(CTLocation()).Year())
		return c
	}
	open, ok := OpenIntervalsBetween(startMs, capMs, rowCoverageStepMs, barHorizonScanCap)
	if !ok {
		c.Unknown = fmt.Sprintf("the open-interval scan exceeded its cap of %d steps", barHorizonScanCap)
		return c
	}
	c.OpenIntervals = open
	return c
}

// ── the four tables, each measured ──────────────────────────────────────────

// aggregateCoverage measures every row of an epoch-floor aggregate table
// (15m/1h/4h) against the 1m tape it was built from. rows must be the SAME
// slice AggregateBars produced, in the same order.
func aggregateCoverage(bars1m []market.Kline, rows []market.Kline, bucketMs int64, now time.Time) []RowCoverage {
	obs := observableEndMs(now)
	held := map[int64]int{}
	first := map[int64]int64{}
	for _, b := range bars1m {
		k := b.OpenTime / bucketMs * bucketMs
		if _, ok := first[k]; !ok {
			first[k] = b.OpenTime
		}
		if b.OpenTime >= obs {
			continue // the minute IN PROGRESS — counted on neither side (observableEndMs)
		}
		held[k]++
	}
	out := make([]RowCoverage, 0, len(rows))
	for _, r := range rows {
		out = append(out, measureRow(r.OpenTime, r.OpenTime+bucketMs, held[r.OpenTime], first[r.OpenTime], now))
	}
	return out
}

// sessionCoverage measures every row of the daily session-candle table. It
// derives each row's key from the ROW ITSELF (DailySessionBars stamps a bucket
// with its first held bar's open time), so the coverage list can never drift
// out of alignment with the bars — the two are keyed by the same function.
func sessionCoverage(bars1m []market.Kline, rows []market.Kline, now time.Time) []RowCoverage {
	obs := observableEndMs(now)
	held := map[string]int{}
	for _, b := range bars1m {
		if b.OpenTime >= obs {
			continue // the minute IN PROGRESS — counted on neither side (observableEndMs)
		}
		held[CMESessionDayKey(time.UnixMilli(b.OpenTime))]++
	}
	out := make([]RowCoverage, 0, len(rows))
	for _, r := range rows {
		start := CMESessionDayStart(time.UnixMilli(r.OpenTime))
		end := NextSessionRollCT(start)
		key := CMESessionDayKey(time.UnixMilli(r.OpenTime))
		out = append(out, measureRow(start.UnixMilli(), end.UnixMilli(), held[key], r.OpenTime, now))
	}
	return out
}

// tableHeading is THE held-vs-claimed string. It is designed to be unambiguous
// to a language model, not merely readable: it names the two numbers, it never
// leaves the requested count implied, and it states in words that the missing
// rows are ABSENT rather than flat, because a model that sees three rows under
// a heading promising eight will otherwise reason about five candles it cannot
// see.
func tableHeading(label string, held, asked int, cov []RowCoverage, absent []int) string {
	var clauses []string
	if asked > held {
		clauses = append(clauses, fmt.Sprintf(
			"SHORT BY %d — the 1m tape does not reach back far enough; the %d older rows are ABSENT from this prompt, not flat, and MUST NOT be inferred",
			asked-held, asked-held))
	}
	// WHOLE ROWS BETWEEN HELD ROWS. This clause exists because "held" counts
	// rows, and a row that does not exist is not a held row: without it the
	// heading said "all held rows COMPLETE" across an 11h45m outage (it now says
	// "every row PRINTED here is COMPLETE", and only when nothing is absent).
	gone, unknown := 0, 0
	for _, n := range absent {
		switch {
		case n < 0:
			unknown++
		case n > 0:
			gone += n
		}
	}
	if gone > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"%d WHOLE ROWS ABSENT BETWEEN HELD ROWS — the tape has no bars there, so two rows printed next to each other are NOT necessarily adjacent windows; the ⛔ marker names each break",
			gone))
	}
	if unknown > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"%d break(s) between held rows could NOT be measured (scan capped) — absent-row count UNKNOWN, never read as zero", unknown))
	}
	marked := 0
	for _, c := range cov {
		if c.Marked() {
			marked++
		}
	}
	if marked == 0 {
		clauses = append(clauses, "every row PRINTED here is COMPLETE")
	} else {
		clauses = append(clauses, fmt.Sprintf("%d of %d printed rows are MARKED on the row itself (⚠PARTIAL / ⏳FORMING / ❓COVERAGE-UNKNOWN)", marked, held))
	}
	return fmt.Sprintf("### %s — HELD %d of %d requested rows · %s", label, held, asked, strings.Join(clauses, " · "))
}

// candleTapeLine discloses the tape every table below is aggregated from. The
// prompt disclosed its tape depth NOWHERE before this wave.
func candleTapeLine(h BarHorizon) string {
	oldest, newest, age := "UNKNOWN", "UNKNOWN", "UNKNOWN"
	if h.OldestOpenMs >= 0 {
		oldest = TableTimeCT(time.UnixMilli(h.OldestOpenMs).In(CTLocation()))
	}
	if h.NewestOpenMs >= 0 {
		newest = TableTimeCT(time.UnixMilli(h.NewestOpenMs).In(CTLocation()))
	}
	if h.OldestAgeMs >= 0 {
		// Minute resolution: the live render printed "320h36m50.387s ago", and a
		// millisecond-precise age invites a model to reason about precision that
		// carries no information. Rounding DOWN, never up — the tape is never
		// claimed older than it is.
		age = msDurTxt(h.OldestAgeMs / 60_000 * 60_000)
	}
	return fmt.Sprintf(
		"TAPE: %d 1m bars HELD of %d requested · oldest %s CT (%s ago) · newest %s CT · gaps %s open 1m intervals missing INSIDE that span. Every table below is aggregated from THIS tape and can be no deeper than it.",
		h.Served, h.Requested, oldest, age, newest, intTxt(h.GapCount))
}

// candleMarkerLegend explains the row markers once, and states the A24 rule the
// whole block obeys.
const candleMarkerLegend = "ROW MARKERS: ⚠PARTIAL = only part of the row's window is held, so its O/H/L/C come from the bars HELD and not from the whole window · ⏳FORMING = the window has not closed yet (the minute in progress is counted on neither side) · ⛔N×tf ABSENT BEFORE THIS ROW = that many WHOLE open-market windows between this row and the one above are missing from the tape, so the two rows are NOT adjacent windows · ❓COVERAGE-UNKNOWN = the session calendar cannot classify this window. A missing bar is NEVER interpolated, carried forward or synthesised — it is simply absent, and the row says so."
