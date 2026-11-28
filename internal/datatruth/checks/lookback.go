package checks

import (
	"fmt"
	"math"
	"time"

	"vl/internal/datatruth"
)

// ── CHECK 3: INDICATOR LOOKBACK COMPLETENESS ─────────────────────────────────
//
// What the code reads (derived 2026-10-29, not assumed):
//
//	mentor evaluator (kernel/mentor/seed.go, swing4h.go)
//	  1m EMA 34 and EMA 9      emaFromCloses over the closed 1m history
//	  2m EMA 34/9              built from the same 1m (covered by the 1m series)
//	  4h swing EMA 34          the recurrence over every closed 17:00-CT 4h bucket
//	                           built from 1m -> 1h -> 4h (FourHEMA34Warmup 102)
//	  prior-session levels     keyLevelsFromCandles over 1H RTH candles
//	AI path (market/data_klines.go, store/strategy.go defaults)
//	  EMA 20 / EMA 50, RSI 7 / RSI 14, ATR 14 (and ATR 3), MACD 12/26
//
// An EMA's value depends on every bar inside its warm-up (market.EMAWarmupBars:
// the residual of the seed is <= 1%); a windowed indicator depends on exactly
// its period. A bar missing INSIDE that span silently changes the value - the
// recurrence just skips a step. JudgeSeries prices the damage instead of
// banning every hole: each missing slot carries the weight it has in the
// indicator (k(1-k)^age for an EMA, 1/period for a window) and
//
//	price-unit indicators (EMAs):  blocked when  sum(weight) x S  >  TolPts,
//	      S = median |close-to-close step| of the frame over the lookback - the
//	      size one missing observation could plausibly move the value;
//	unit-less indicators (ATR/RSI/MACD): blocked when sum(weight) > MaxWeight.
//
// P2-6 (the 4h swing line, trade-recheck 10-29) is judged by JudgeSwing4h from
// the bucket closes: see below.

// SeriesSpec is one indicator's lookback requirement.
type SeriesSpec struct {
	Name       string
	TFMin      int
	Period     int
	EMA        bool    // true: EMA weights; false: window weights (period slots)
	PriceUnits bool    // true: judged in points against TolPts
	TolPts     float64 // price-unit tolerance
	MaxWeight  float64 // unit-less tolerance (share of the indicator's weight mass)
	// Range marks an indicator that reads the bar's HIGH/LOW (ATR); the others
	// read closes only (BarError splits the damage of a wrong bar the same way).
	Range bool
	// Reader names the only entry path whose decisions read this indicator
	// (datatruth.ReaderAI / ReaderMentor); "" = read by every path. The mentor
	// evaluator reads the 1m EMA 34/9 and nothing of the AI path's 3m/5m set; the
	// AI path is held to the whole list (it may select any timeframe).
	Reader string
	// Extreme marks a raw high/low READ over the last Period slots: the bar's
	// high or low enters the result at FULL weight (a max / min, not an average),
	// so an error of more than ImpactTolPts moves it for as long as the bar stays
	// inside the window - Period slots - not for a decayed share of it. Only the
	// bar's RANGE error reaches it (BarError.RangeErr). Held for Period x TFMin.
	Extreme bool
	// PresenceOnly marks a read that needs the bar to EXIST (a hole is judged
	// like any other) but whose value error is judged elsewhere: it contributes
	// no hold to HoldMinutesFor (the strong-day range flip is CompareFrames').
	PresenceOnly bool
}

// MentorRangeReadMin is how many 1m slots a wrong HIGH/LOW stays in what the
// mentor reads (release #55 review round 1, finding 2). The mentor consumes raw
// 1m highs and lows - the Globex session extreme the day gate measures
// (day_gate.go GlobexMeasure), rolling_phl.go's buy stop on the candle extreme,
// box.go / isb_box.go / trendline.go levels and the 3m/5m/15m candles filters.go
// builds from the 1m - and a wrong extreme sits in those for the session, not for
// the 30 minutes a close-error would. It is the cap (HoldCapMin): the longest any
// single bar can pause entries.
const MentorRangeReadMin = HoldCapMin

// EMAWarmupBars mirrors market.EMAWarmupBars (seed residual 1%). A pin in
// provider/ninjatrader compares the two so they cannot drift.
func EMAWarmupBars(period int) int {
	if period <= 1 {
		return period
	}
	k := math.Ceil(math.Log(0.01) / math.Log(1-2.0/float64(period+1)))
	return period + int(k)
}

// DefaultSpecs lists the indicators above for the given HTF frames the AI path
// reads (the mentor's 1m EMAs are always present).
func DefaultSpecs(aiFramesMin []int) []SeriesSpec {
	specs := []SeriesSpec{
		{Name: "mentor_ema34_1m", TFMin: 1, Period: 34, EMA: true, PriceUnits: true, TolPts: 0.5},
		{Name: "mentor_ema9_1m", TFMin: 1, Period: 9, EMA: true, PriceUnits: true, TolPts: 0.5},
		// the mentor's raw 1m high/low reads (day gate, rolling PHL, box, trendline,
		// the 1m-built HTF candles): a wrong extreme is held for the whole window
		{Name: "mentor_range_1m", TFMin: 1, Period: MentorRangeReadMin, Extreme: true, Reader: datatruth.ReaderMentor},
		// the mentor's strong-day test reads the last 12 NATIVE 5m bars
		// (trader/mentor_tick.go): a lost one is a hole in what the mentor reads,
		// and a single lost bar can be the strong one, so none may be missing
		// (MaxWeight below 1/12). Its value error is CompareFrames' range flip.
		{Name: "mentor_strongday_5m", TFMin: 5, Period: MentorStrongDayBars, MaxWeight: 0.05, PresenceOnly: true, Reader: datatruth.ReaderMentor},
	}
	for _, tf := range aiFramesMin {
		f := fmt.Sprintf("%dm", tf)
		specs = append(specs,
			SeriesSpec{Name: "ai_ema20_" + f, TFMin: tf, Period: 20, EMA: true, PriceUnits: true, TolPts: 1.0, Reader: datatruth.ReaderAI},
			SeriesSpec{Name: "ai_ema50_" + f, TFMin: tf, Period: 50, EMA: true, PriceUnits: true, TolPts: 1.0, Reader: datatruth.ReaderAI},
			SeriesSpec{Name: "ai_atr14_" + f, TFMin: tf, Period: 14, MaxWeight: 0.10, Range: true, Reader: datatruth.ReaderAI},
			SeriesSpec{Name: "ai_rsi14_" + f, TFMin: tf, Period: 14, MaxWeight: 0.10, Reader: datatruth.ReaderAI},
			SeriesSpec{Name: "ai_rsi7_" + f, TFMin: tf, Period: 7, MaxWeight: 0.15, Reader: datatruth.ReaderAI},
			SeriesSpec{Name: "ai_macd26_" + f, TFMin: tf, Period: 26, EMA: true, MaxWeight: 0.10, Reader: datatruth.ReaderAI},
		)
	}
	return specs
}

func (s SeriesSpec) span() int {
	if s.EMA {
		return EMAWarmupBars(s.Period)
	}
	return s.Period + 1
}

func (s SeriesSpec) weight(age int) float64 {
	if s.EMA {
		k := 2.0 / float64(s.Period+1)
		return k * math.Pow(1-k, float64(age))
	}
	if age < s.Period {
		return 1.0 / float64(s.Period)
	}
	return 0
}

// JudgeSeries judges one indicator over `bars` (the frame's closed history,
// ascending). open marks exchange-open slots so a weekend / the 16:00-17:00 CT
// break is never a hole. The verdict is deterministic in (bars, now).
func JudgeSeries(spec SeriesSpec, bars []Bar, now time.Time, open OpenFn) (blocked bool, why string) {
	span := int64(spec.TFMin) * minuteMs
	nowMs := now.UnixMilli()
	// newest closed slot
	t := (nowMs - span) / span * span
	n := spec.span()
	var missW float64
	var missed []int64
	var closes []float64
	slots := 0
	for guard := 0; slots < n && guard < n*400; guard++ {
		if !open.open(t) {
			t -= span
			continue
		}
		if b, ok := findT(bars, t); ok {
			closes = append(closes, b.C)
		} else {
			missW += spec.weight(slots)
			missed = append(missed, t)
		}
		slots++
		t -= span
	}
	if len(missed) == 0 {
		return false, ""
	}
	if len(closes) < 3 {
		// every (or all but two) of the slots the indicator reads are missing:
		// no step scale can be measured, so nothing can be priced - fail closed.
		return true, fmt.Sprintf("%s: %d of the last %d %dm slots missing and only %d present - the indicator has no data to stand on", spec.Name, len(missed), n, spec.TFMin, len(closes))
	}
	if spec.PriceUnits {
		var steps []float64
		for i := 0; i+1 < len(closes); i++ { // closes are newest-first
			steps = append(steps, abs(closes[i]-closes[i+1]))
		}
		s := median(steps)
		if est := missW * s; est > spec.TolPts {
			return true, fmt.Sprintf("%s: %d of the last %d %dm slots missing (e.g. %s), weight %.1f%% x step %.2f pt = %.2f pt > %.2f pt tolerance",
				spec.Name, len(missed), n, spec.TFMin, time.UnixMilli(missed[0]).UTC().Format("01-02 15:04Z"), 100*missW, s, est, spec.TolPts)
		}
		return false, ""
	}
	if missW > spec.MaxWeight {
		return true, fmt.Sprintf("%s: %d of the last %d %dm slots missing (e.g. %s), %.1f%% of the indicator's weight > %.1f%%",
			spec.Name, len(missed), n, spec.TFMin, time.UnixMilli(missed[0]).UTC().Format("01-02 15:04Z"), 100*missW, 100*spec.MaxWeight)
	}
	return false, ""
}

// ── the 4h swing line (P2-6) ─────────────────────────────────────────────────
//
// INVESTIGATION (2026-10-29, [A] each number reproduced from data/data.db MNQ
// 12-26): the bot's swing line at the 10-28 17:00 CT bucket was 29645.53
// (snapshot swing.line 29645.529 on arms 475/477). The trade-recheck read it as
// 7.7 pt above "the EMA of stored 4h closes" 29637.87. Both numbers are
// explained, and the 7.7 pt is NOT a bot error:
//
//	29645.53  = EMA34 recurrence over the 201 closed 17:00-anchored 4h closes
//	            built from the stored 1m (09-07 09:00 .. 10-28 13:00): the bot's
//	            line to the cent.
//	29637.87  = the same recurrence over ALL 677 native 4h rows incl. 94
//	            OFF-GRID stub rows (NT8 writes a 12:00 CT partial bar every
//	            day, 06-19 08:00 on others; e.g. rows at 06-12 12:00): they are
//	            not 17:00-anchored candles and must not be in the recurrence.
//	29645.10  = the same recurrence over the 583 ON-GRID native 4h rows. Identical
//	            for any depth >= 200. This is the truth.
//	29645.11  = the bot's 1m-built series WITH the one missing bucket restored.
//
// So the only real defect is 0.43 pt: the 1m store has ZERO rows for the whole
// 10-26 17:00-21:00 CT bucket (4 h hole; 5m and native 4h have it, close
// 29515.75) and the seed's 1m->1h->4h aggregation silently skips a bucket it
// cannot build - one step of the recurrence lost, decayed by (1-k)^12 to 0.43 pt.
// The 4h list also holds 76 more partial/empty 1m buckets (09-07..09-11 were
// never in the 1m store at all).
//
// JudgeSwing4h therefore compares the evaluator's line with the line recomputed
// from the best data ANY frame holds: a bucket missing from the 1m is filled
// from the independent frame (5m). It blocks only when that MEASURED
// disagreement exceeds TolPts, or when a bucket inside the warm-up exists in NO
// frame (an unknowable close). On 10-28 the disagreement is 0.42 pt: no pause
// (the course puts the line's own accuracy at 5-10 pt, the touch band is +-5).

// Bucket is one closed 17:00-CT-anchored 4h bucket.
type Bucket struct {
	Start  int64
	Close  float64 // from the 1m (what the evaluator sees); valid when Has
	Has    bool
	Alt    float64 // from the independent frame (5m); valid when HasAlt
	HasAlt bool
}

// EMAFromCloses is the evaluator's recurrence (kernel/mentor emaFromCloses):
// seeded with the first close, k = 2/(period+1).
func EMAFromCloses(closes []float64, period int) float64 {
	if len(closes) == 0 || period <= 0 {
		return 0
	}
	k := 2.0 / float64(period+1)
	e := closes[0]
	for _, c := range closes[1:] {
		e += k * (c - e)
	}
	return e
}

// Swing4hVerdict is the measured result.
type Swing4hVerdict struct {
	Line         float64 // recurrence over what the 1m holds
	Repaired     float64 // recurrence with every bucket any frame holds
	Missing      int     // buckets absent from the 1m
	Unrepairable int     // buckets absent from every frame (inside the warm-up)
	Blocked      bool
	Why          string
}

// JudgeSwing4h judges the swing line. botLine > 0 is the evaluator's live value
// (compared against the repaired line); <= 0 uses the 1m-built recurrence.
func JudgeSwing4h(buckets []Bucket, period int, botLine, tolPts float64) Swing4hVerdict {
	var v Swing4hVerdict
	var c1, cr []float64
	warm := EMAWarmupBars(period)
	for i, b := range buckets {
		if b.Has {
			c1 = append(c1, b.Close)
			cr = append(cr, b.Close)
			continue
		}
		v.Missing++
		if b.HasAlt {
			cr = append(cr, b.Alt)
		} else if len(buckets)-i <= warm {
			v.Unrepairable++
		}
	}
	v.Line = EMAFromCloses(c1, period)
	v.Repaired = EMAFromCloses(cr, period)
	line := v.Line
	if botLine > 0 {
		line = botLine
	}
	switch {
	case len(cr) < period:
		v.Blocked, v.Why = true, fmt.Sprintf("swing 4h EMA%d: only %d closed 4h buckets in any frame (need %d)", period, len(cr), period)
	case v.Unrepairable > 0:
		v.Blocked, v.Why = true, fmt.Sprintf("swing 4h EMA%d: %d bucket(s) inside the warm-up exist in no frame - the line cannot be verified", period, v.Unrepairable)
	case abs(line-v.Repaired) > tolPts:
		v.Blocked, v.Why = true, fmt.Sprintf("swing 4h EMA%d line %.2f disagrees with %.2f recomputed from all frames by %.2f pt (> %.2f pt); %d bucket(s) missing from the 1m", period, line, v.Repaired, abs(line-v.Repaired), tolPts, v.Missing)
	}
	return v
}

// Buckets4h builds the closed 17:00-anchored 4h buckets from m1 (what the
// evaluator uses) and m5 (the independent frame), oldest first. A bucket is
// EXPECTED when the exchange is open for at least one of its minutes; a bucket
// with no data in either frame is still returned (Has=HasAlt=false) so the
// judge sees the unknowable ones. loc is America/Chicago.
func Buckets4h(m1, m5 []Bar, open OpenFn, now time.Time, loc *time.Location) []Bucket {
	if len(m1) == 0 && len(m5) == 0 {
		return nil
	}
	start := func(ms int64) int64 {
		t := time.UnixMilli(ms).In(loc)
		a := time.Date(t.Year(), t.Month(), t.Day(), 17, 0, 0, 0, loc)
		if t.Before(a) {
			a = a.AddDate(0, 0, -1)
		}
		d := t.Sub(a)
		return a.Add(d - d%(4*time.Hour)).UnixMilli()
	}
	const fourH = int64(4 * time.Hour / time.Millisecond)
	type acc struct {
		c   float64
		t   int64
		has bool
	}
	one, five := map[int64]acc{}, map[int64]acc{}
	fold := func(dst map[int64]acc, bars []Bar) (lo, hi int64) {
		lo = math.MaxInt64
		for _, b := range bars {
			s := start(b.T)
			if a, ok := dst[s]; !ok || b.T >= a.t {
				dst[s] = acc{c: b.C, t: b.T, has: true}
			}
			if s < lo {
				lo = s
			}
			if s > hi {
				hi = s
			}
		}
		return
	}
	lo1, hi1 := fold(one, m1)
	lo5, hi5 := fold(five, m5)
	lo, hi := lo1, hi1
	if lo5 < lo {
		lo = lo5
	}
	if hi5 > hi {
		hi = hi5
	}
	cur := start(now.UnixMilli())
	// nextAnchor is the first 17:00-local instant after s: a bucket never runs
	// past it, because the clock-derived grid restarts there. On a DST day the
	// 17:00 anchors are 23 or 25 hours apart, so "s + 4h" can land on a point
	// that is neither on the old grid nor on the new one - the P1 of the
	// release #55 review: on Sun 2026-11-01 the 22:00Z "bucket" (16:00 CST) was
	// returned as expected because the 23:00Z open falls inside s+4h, but every
	// real bar maps to the 23:00Z bucket, so the phantom bucket held no data in
	// ANY frame and JudgeSwing4h called it unrepairable for the whole warm-up
	// (about 26 days of SWING4H pause on a perfect feed). The spring-forward
	// twin (Sun 2027-03-14, 19:00Z) had the same shape.
	nextAnchor := func(ms int64) int64 {
		t := time.UnixMilli(ms).In(loc)
		a := time.Date(t.Year(), t.Month(), t.Day(), 17, 0, 0, 0, loc)
		if !a.After(t) {
			a = a.AddDate(0, 0, 1)
		}
		return a.UnixMilli()
	}
	var out []Bucket
	for s := start(lo); s <= hi && s < cur; {
		end := s + fourH
		if a := nextAnchor(s); a < end {
			end = a // the short (or, on a 25 h day, the 1 h stub) bucket before the anchor
		}
		next := end
		expected := false
		for t := s; t < end; t += 15 * minuteMs {
			if open.open(t) {
				expected = true
				break
			}
		}
		if !expected {
			s = next
			continue
		}
		b := Bucket{Start: s}
		if a, ok := one[s]; ok {
			b.Close, b.Has = a.c, true
		}
		if a, ok := five[s]; ok {
			b.Alt, b.HasAlt = a.c, true
		}
		out = append(out, b)
		s = next
	}
	return out
}
