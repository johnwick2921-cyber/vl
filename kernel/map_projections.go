package kernel

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"vl/market"
)

// W3 D5 — EXTENSION TARGETS EXIST BEFORE PRICE ARRIVES.
//
// On 2026-09-03 the market ran +483 pts. Once price left the highest seated
// level the map held nothing above it, so there was no target and the fade book
// kept selling. These producers put references BEYOND the mapped range.
//
// Every projection is a TARGET or an OBSTACLE. None is ever an entry candidate
// — BuildMapWithProjections refuses candidacy for anything carrying
// Projection=true, and the pin TestE4_PriceBeyondTheMapCarriesAProjectionAboveIt
// asserts it.
//
// A projection is ARITHMETIC, not an observed level, and it says so: every one
// carries the word `projection` and the method it came from, on the card and in
// the model's table. Methods (c) and (d) are [I] — invented, unvalidated —
// until E4 measures them.

// SessionExtremeATRMult is k in "session open ± k × daily ATR" (D5c). [I].
// Env SESSION_EXTREME_ATR_K overrides; never a literal at a call site.
func SessionExtremeATRMult() float64 {
	if v := os.Getenv("SESSION_EXTREME_ATR_K"); v != "" {
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && n > 0 {
			return n
		}
	}
	return SessionExtremeATRMultDefault
}

// MapDailyBarCount is how many CLOSED daily bars the prior-week projector
// reads. Two weeks of trading days plus slack, so a holiday-shortened prior
// week is still fully covered.
const MapDailyBarCount = 20

// MapRoundNumberStep is the round-number spacing used BEYOND the mapped range
// (D5b). 100 index points on MNQ — the same magnitude traders quote ("29,700"),
// and coarse enough that a projection list stays short. [I] until E4.
const MapRoundNumberStep = 100.0

// SessionExtremeATRMultDefault — one daily ATR either side of the open. Chosen
// as the plainest possible starting point, NOT as a measured edge: a day that
// travels a full daily ATR from its open is ordinary, so this projects the
// ordinary case rather than an outlier. [I] until E4.
const SessionExtremeATRMultDefault = 1.0

// ProjectPriorWeekExtremes returns PWH/PWL from the DAILY bar source.
//
// C6 (verified 2026-09-09): the 1m ring can NEVER satisfy priorWeekMinBars=4320
// (levels_multiday.go:224), and the const block's own comment says that is
// CORRECT — "those anchors must come from a multi-day source, not the ring"
// (:219-221). candidate_pool has held ZERO PWH/PWL rows, ever.
//
// So this does NOT relax that guard. Relaxing it would compute a "prior-week
// high" from 33 hours of data, which is not a prior-week high at all. It reads
// daily bars instead — the source the const block names.
//
// `daily` must be CLOSED daily bars, oldest first. `now` is the caller's clock
// (A28); nothing here reads the wall clock.
func ProjectPriorWeekExtremes(daily []market.Kline, now time.Time) []MapCandidate {
	if len(daily) == 0 {
		return nil
	}
	// R22 TIME-WEEKLY (P2-A). The prior week is the CME week before the one
	// governing `now` — the SAME week convention as the weekly read
	// (weekStartMonday: Sunday belongs to the week it opens). Each native daily
	// row is placed by its TRADING date, not its open stamp: NT8 open-stamps a
	// daily row at 00:00 CT of the calendar day BEFORE its trading day, so the
	// old open-stamp window took Tue..Fri of last week plus THIS week's Monday
	// and dropped last Monday (measured: 215.5 pts off on PWH, 254.75 on PWL),
	// and on a Sunday evening it projected the week before last.
	thisWeekStart := weekStartMonday(now)
	priorWeekStart := thisWeekStart.AddDate(0, 0, -7)

	var hi, lo float64
	var days int
	for _, b := range daily {
		td := NativeDailyTradingDate(b)
		if td.Before(priorWeekStart) || !td.Before(thisWeekStart) {
			continue
		}
		days++
		if hi == 0 || b.High > hi {
			hi = b.High
		}
		if lo == 0 || b.Low < lo {
			lo = b.Low
		}
	}
	// A24 — an uncovered prior week produces NOTHING, never a zero or a guess.
	// The caller reports pwh/pwl seatable=no and the boot line says so.
	if days == 0 || hi <= 0 || lo <= 0 {
		return nil
	}
	label := priorWeekStart.Format("2006-01-02")
	method := fmt.Sprintf("prior-week %s (daily bars, %d day(s))", label, days)
	return []MapCandidate{
		projectionAt(hi, "PWH", KindPWH, method),
		projectionAt(lo, "PWL", KindPWL, method),
	}
}

// ProjectRoundNumbersBeyond returns round-number references OUTSIDE the mapped
// range (D5b). The in-range RN detector (levels_intraday.go, KindRound) is
// untouched — this extends the WINDOW only, and only outward.
//
// `lo`/`hi` bound the mapped range; `step` is the round-number spacing; `reach`
// is how far beyond each edge to project.
func ProjectRoundNumbersBeyond(lo, hi, step, reach float64) []MapCandidate {
	if step <= 0 || reach <= 0 || hi <= 0 || lo <= 0 || hi < lo {
		return nil
	}
	var out []MapCandidate
	// Above the mapped high.
	for p := math.Ceil(hi/step) * step; p <= hi+reach; p += step {
		if p <= hi {
			continue
		}
		out = append(out, projectionAt(p, fmt.Sprintf("RN %.0f", p), KindRound,
			fmt.Sprintf("round number beyond the range (step %s)", trimFloat(step))))
	}
	// Below the mapped low.
	for p := math.Floor(lo/step) * step; p >= lo-reach; p -= step {
		if p >= lo {
			continue
		}
		out = append(out, projectionAt(p, fmt.Sprintf("RN %.0f", p), KindRound,
			fmt.Sprintf("round number beyond the range (step %s)", trimFloat(step))))
	}
	return out
}

// ProjectSessionExtreme returns the ATR-projected session extremes (D5c):
// session open ± k × daily ATR, k resolved by SessionExtremeATRMult. [I].
func ProjectSessionExtreme(sessionOpen, dailyATR float64) []MapCandidate {
	if sessionOpen <= 0 || dailyATR <= 0 {
		return nil
	}
	k := SessionExtremeATRMult()
	method := fmt.Sprintf("session open %s ± %s×daily-ATR %s [I]",
		trimFloat(sessionOpen), trimFloat(k), trimFloat(dailyATR))
	return []MapCandidate{
		projectionAt(sessionOpen+k*dailyATR, "ATR-PROJ-H", KindRound, method),
		projectionAt(sessionOpen-k*dailyATR, "ATR-PROJ-L", KindRound, method),
	}
}

// ProjectMeasuredMove returns the measured move of the last completed swing
// (D5d): the swing's own height carried forward from the breakout. [I].
//
// Direction follows the break: a break ABOVE swingHi projects up by the swing
// height; a break BELOW swingLo projects down by it.
func ProjectMeasuredMove(swingLo, swingHi, breakPrice float64) []MapCandidate {
	if swingLo <= 0 || swingHi <= 0 || swingHi <= swingLo || breakPrice <= 0 {
		return nil
	}
	height := swingHi - swingLo
	method := fmt.Sprintf("measured move of the %s pt swing %s→%s [I]",
		trimFloat(height), trimFloat(swingLo), trimFloat(swingHi))
	switch {
	case breakPrice >= swingHi:
		return []MapCandidate{projectionAt(swingHi+height, "MM-UP", KindRound, method)}
	case breakPrice <= swingLo:
		return []MapCandidate{projectionAt(swingLo-height, "MM-DN", KindRound, method)}
	}
	// Inside the swing there is no completed break, so there is no measured
	// move. Nothing is emitted rather than a guess (A24).
	return nil
}

// projectionAt builds one projected reference. Grade is deliberately EMPTY: a
// projection is arithmetic and carries no detector grade, and an empty grade
// renders as "—" rather than as a fabricated letter (canon 49).
func projectionAt(price float64, name string, kind LevelKind, method string) MapCandidate {
	return MapCandidate{
		Price:            price,
		Names:            []string{name},
		Kinds:            []LevelKind{kind},
		Grade:            "",
		Fresh:            "projection",
		MergedCount:      1,
		MergedCredit:     1,
		Projection:       true,
		ProjectionMethod: method,
	}
}

// DailySourceInstalled reports whether a daily bar source is reachable, so the
// boot line can say pwh/pwl seatable=yes|no from a READ rather than a guess.
func DailySourceInstalled() bool { return market.FuturesBarsProvider != nil }

// DailyBarsFor reads CLOSED daily bars for a symbol from the injected futures
// provider — the same accessor kernel already uses for the void scope
// (void_scope.go:85) and the clock probes. Returns nil when no provider is
// installed, so a caller can report pwh/pwl seatable=no rather than guessing.
func DailyBarsFor(symbol string, count int) []market.Kline {
	if market.FuturesBarsProvider == nil || count <= 0 {
		return nil
	}
	return market.FuturesBarsProvider(symbol, "1d", count)
}
