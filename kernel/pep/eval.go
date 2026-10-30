package pep

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"vl/market"
)

const minuteMs = 60_000

// Evaluate is the whole pep decision for ONE instant.
//
// bars are 1m bars (ascending OpenTime, real-UTC epoch ms, OPEN-stamped); a bar
// whose CloseTime >= now is still forming and is ignored. now is the evaluation
// instant (the live loop passes the newest closed bar's CloseTime+1). cfg is the
// resolved settings; st is the caller's durable one-trade-per-day memory.
//
// It returns either one Intent (enter, or flat) or one or more named Refusals.
// It reads no clock, mutates nothing, and logs nothing.
func Evaluate(bars []market.Kline, now int64, cfg Config, st State) Result {
	res := Result{Style: cfg.Style}
	refuse := func(code string, sev Severity, format string, a ...any) Result {
		res.Refusals = append(res.Refusals, Refusal{Code: code, Severity: sev, Detail: fmt.Sprintf(format, a...)})
		return res
	}

	if err := cfg.Validate(); err != nil {
		return refuse(CodeConfigInvalid, SevBlock, "%v", err)
	}
	loc, err := chicago()
	if err != nil || loc == nil {
		return refuse(CodeTZUnavailable, SevBlock, "America/Chicago zoneinfo unavailable: %v", err)
	}

	nowT := time.UnixMilli(now).In(loc)
	y, mo, d := nowT.Date()
	day := nowT.Format("2006-01-02")
	res.Day = day
	at := func(minOfDay int) int64 {
		return time.Date(y, mo, d, minOfDay/60, minOfDay%60, 0, 0, loc).UnixMilli()
	}
	boxMin := cfg.BoxMinutes()
	boxStart := at(cfg.BoxStartMin)
	boxEnd := boxStart + int64(boxMin)*minuteMs
	flatAt := at(cfg.FlatMin)

	// Only CLOSED bars; the forming tail is dropped, never read.
	closed := bars
	for len(closed) > 0 && closed[len(closed)-1].CloseTime >= now {
		closed = closed[:len(closed)-1]
		res.Diag.Dropped++
	}
	res.Diag.Bars = len(closed)

	// The time exit comes first and does not depend on fresh data: a position
	// still open at/after the flat time, or carried over from an earlier CT day,
	// is flattened at the last price whatever else is wrong.
	if st.PositionOpen && (st.Day != day || now >= flatAt) {
		reason := fmt.Sprintf("flat time %s CT reached", clock(cfg.FlatMin))
		if st.Day != day {
			reason = fmt.Sprintf("position carried over from CT day %q", st.Day)
		}
		res.Intents = append(res.Intents, Intent{
			Kind: KindFlat, Style: cfg.Style, Contracts: cfg.Contracts, Day: day, Reason: reason,
		})
		return res
	}

	if !sortedAscending(closed) {
		return refuse(CodeBarsUnordered, SevBlock, "1m bars are not strictly ascending by open time (duplicate or out-of-order minute)")
	}

	trades := st.Trades
	if st.Day != day {
		trades = 0
	}
	res.Diag.TradesTaken = trades
	res.Diag.EntriesAllowed = cfg.MaxTradesPerDay

	// The three refusals below end the pass before the box is judged, but the box
	// is still a FACT of the day the card and the chart draw: it is measured here
	// as a diagnostic (never a reason to refuse, never an order). Without this the
	// box vanished from the card the moment the day's trade was taken.
	diagBox := func() {
		if now < boxEnd {
			return
		}
		if b, miss := measureBox(closed, boxStart, boxEnd, boxMin, loc); len(miss) == 0 {
			b.Size = snap(b.High-b.Low, cfg.TickSize)
			b.Drawn = true
			res.Box = b
		}
	}
	if now >= flatAt {
		diagBox()
		return refuse(CodeAfterFlat, SevWait, "after the %s CT flat time: no new entries", clock(cfg.FlatMin))
	}
	if st.PositionOpen {
		diagBox()
		return refuse(CodePositionOpen, SevWait, "a pep position is already open")
	}
	if trades >= cfg.MaxTradesPerDay {
		diagBox()
		return refuse(CodeMaxTrades, SevWait, "%d of %d trades already taken on %s", trades, cfg.MaxTradesPerDay, day)
	}
	if now < boxEnd {
		return refuse(CodeBoxNotComplete, SevWait, "box %s-%s CT not complete yet", clock(cfg.BoxStartMin), clock(cfg.BoxStartMin+boxMin))
	}
	if len(closed) == 0 {
		return refuse(CodeNoBars, SevBlock, "no closed 1m bars")
	}
	maxAge := int64(cfg.SignalMaxAgeMin) * minuteMs
	if lag := now - (closed[len(closed)-1].CloseTime + 1); lag > maxAge {
		return refuse(CodeBarsStale, SevBlock, "newest closed 1m bar is %ds old (limit %ds)", lag/1000, maxAge/1000)
	}

	// The box: every minute of [boxStart, boxEnd) must be present.
	box, missing := measureBox(closed, boxStart, boxEnd, boxMin, loc)
	if len(missing) > 0 {
		return refuse(CodeBoxMissingMin, SevBlock, "box minute(s) missing: %s", strings.Join(missing, ","))
	}
	box.Size = snap(box.High-box.Low, cfg.TickSize)
	box.Drawn = true
	res.Box = box
	if box.Size <= 0 {
		return refuse(CodeBoxFlat, SevBlock, "box high %.2f == low %.2f", box.High, box.Low)
	}
	if cfg.MaxBoxPoints > 0 && box.Size > cfg.MaxBoxPoints {
		return refuse(CodeBoxTooWide, SevBlock, "box %.2f pts exceeds the %.2f pt limit", box.Size, cfg.MaxBoxPoints)
	}

	switch cfg.Style {
	case StyleA:
		return evalA(res, closed, now, cfg, trades, day)
	default:
		return evalE(res, closed, now, cfg, trades, day)
	}
}

// measureBox reads the opening box off the closed bars: the high and low of
// every minute of [boxStart, boxEnd). missing lists (CT clock) each minute that
// has no bar; a box with a missing minute is NOT a box. It reads nothing else.
func measureBox(closed []market.Kline, boxStart, boxEnd int64, boxMin int, loc *time.Location) (Box, []string) {
	i0 := sort.Search(len(closed), func(i int) bool { return closed[i].OpenTime >= boxStart })
	box := Box{Start: boxStart, End: boxEnd}
	var missing []string
	idx := i0
	for m := 0; m < boxMin; m++ {
		want := boxStart + int64(m)*minuteMs
		if idx < len(closed) && closed[idx].OpenTime == want {
			b := closed[idx]
			if m == 0 {
				box.High, box.Low = b.High, b.Low
			} else {
				if b.High > box.High {
					box.High = b.High
				}
				if b.Low < box.Low {
					box.Low = b.Low
				}
			}
			idx++
			continue
		}
		missing = append(missing, clockOfMs(want, loc))
	}
	return box, missing
}

// distances turns the box into the stop and target distances: the stop is
// rounded OUTWARD (up) to a tick, the target to the nearest tick.
func distances(box float64, slFrac, tpFrac, tick float64, rounding SLRounding) (sl, tp float64) {
	if rounding == RoundNearest {
		return RoundTick(slFrac*box, tick), RoundTick(tpFrac*box, tick)
	}
	return CeilTick(slFrac*box, tick), RoundTick(tpFrac*box, tick)
}

func buildEntry(cfg Config, res Result, side Side, entry float64, sigOpen, sigClose int64, reason string) (Intent, *Refusal) {
	sl, tp := distances(res.Box.Size, cfg.SLFraction(), cfg.TPFraction(), cfg.TickSize, cfg.SLRounding)
	if sl < cfg.TickSize || tp < cfg.TickSize {
		return Intent{}, &Refusal{Code: CodeStopTooTight, Severity: SevBlock,
			Detail: fmt.Sprintf("stop %.2f / target %.2f below one tick %.2f (box %.2f)", sl, tp, cfg.TickSize, res.Box.Size)}
	}
	entry = snap(entry, cfg.TickSize)
	in := Intent{
		Kind: KindEnter, Side: side, Style: cfg.Style, Contracts: cfg.Contracts,
		Entry: entry, StopDist: sl, TargetDist: tp,
		BoxHigh: res.Box.High, BoxLow: res.Box.Low, BoxSize: res.Box.Size,
		SignalOpen: sigOpen, SignalClose: sigClose, Day: res.Day, Reason: reason,
	}
	if side == SideLong {
		in.Stop = snap(entry-sl, cfg.TickSize)
		in.Target = snap(entry+tp, cfg.TickSize)
	} else {
		in.Stop = snap(entry+sl, cfg.TickSize)
		in.Target = snap(entry-tp, cfg.TickSize)
	}
	return in, nil
}

// evalA: the FIRST 1m bar after the box to trade through an edge decides the
// side; style A takes that one break and nothing else that day.
func evalA(res Result, closed []market.Kline, now int64, cfg Config, trades int, day string) Result {
	refuse := func(code string, sev Severity, format string, a ...any) Result {
		res.Refusals = append(res.Refusals, Refusal{Code: code, Severity: sev, Detail: fmt.Sprintf(format, a...)})
		return res
	}
	if trades >= 1 {
		return refuse(CodeMaxTrades, SevWait, "style A takes the first break only; it was taken (%d trade(s) today)", trades)
	}
	loc, _ := chicago()
	box := res.Box
	i := sort.Search(len(closed), func(k int) bool { return closed[k].OpenTime >= box.End })
	want := box.End
	for ; i < len(closed); i++ {
		b := closed[i]
		if b.OpenTime != want {
			return refuse(CodeGapAfterBox, SevBlock, "1m bar %s CT missing after the box before any break: the first break cannot be told",
				clockOfMs(want, loc))
		}
		want += minuteMs
		up := b.High > box.High
		dn := b.Low < box.Low
		if !up && !dn {
			continue
		}
		res.Diag.FirstBreakOpen = b.OpenTime
		var side Side
		switch {
		case up && dn:
			res.Diag.BreakBothEdges = true
			switch {
			case b.Open > box.High: // opened above the high: the high broke first, for certain
				side = SideLong
			case b.Open < box.Low:
				side = SideShort
			case cfg.A.BothEdges == BothEdgesNearestOpen:
				dUp, dDn := box.High-b.Open, b.Open-box.Low
				switch {
				case dUp < dDn:
					side = SideLong
				case dDn < dUp:
					side = SideShort
				default:
					return refuse(CodeBothEdges, SevBlock, "1m bar %s CT traded through both box edges from the exact middle", clockOfMs(b.OpenTime, loc))
				}
			default:
				return refuse(CodeBothEdges, SevBlock, "1m bar %s CT traded through BOTH box edges; the first one cannot be told from a 1m bar (policy %s)",
					clockOfMs(b.OpenTime, loc), cfg.A.BothEdges)
			}
		case up:
			side = SideLong
		default:
			side = SideShort
		}
		age := now - (b.CloseTime + 1)
		res.Diag.SignalAgeMillis = age
		res.Diag.Signals = 1
		if age > int64(cfg.SignalMaxAgeMin)*minuteMs {
			return refuse(CodeStaleSignal, SevBlock, "first break at %s CT is %ds old (limit %ds): not entered late",
				clockOfMs(b.OpenTime, loc), age/1000, int64(cfg.SignalMaxAgeMin)*60)
		}
		entry := box.High
		if side == SideShort {
			entry = box.Low
		}
		// A bar that OPENS beyond the edge traded through it at the open.
		if side == SideLong && b.Open > entry {
			entry = b.Open
		}
		if side == SideShort && b.Open < entry {
			entry = b.Open
		}
		in, ref := buildEntry(cfg, res, side, entry, b.OpenTime, b.CloseTime,
			fmt.Sprintf("A: first %s through the %.2f-%.2f box at %s CT", map[Side]string{SideLong: "break up", SideShort: "break down"}[side],
				box.Low, box.High, clockOfMs(b.OpenTime, loc)))
		if ref != nil {
			res.Refusals = append(res.Refusals, *ref)
			return res
		}
		res.Intents = append(res.Intents, in)
		return res
	}
	return refuse(CodeNoBreakYet, SevWait, "no 1m bar has traded through the %.2f-%.2f box yet", box.Low, box.High)
}

// evalE: on a CLOSED candle the EMA (or the close) crossing a box edge.
func evalE(res Result, closed []market.Kline, now int64, cfg Config, trades int, day string) Result {
	refuse := func(code string, sev Severity, format string, a ...any) Result {
		res.Refusals = append(res.Refusals, Refusal{Code: code, Severity: sev, Detail: fmt.Sprintf(format, a...)})
		return res
	}
	loc, _ := chicago()
	box := res.Box
	cs, partial := buildCandles(closed, cfg.E.EMATFMin, now)
	fillEMA(cs, cfg.E.EMAPeriod)
	res.Diag.Candles = len(cs)
	res.Diag.PartialCandles = partial
	if len(cs) > 0 {
		res.Diag.LastEMA = cs[len(cs)-1].EMA
	}
	warm := cfg.WarmupCandles()
	ref := func(c candle) float64 {
		if cfg.E.Source == SourceClose {
			return c.Close
		}
		return c.EMA
	}
	type sig struct {
		i    int
		side Side
	}
	var sigs []sig
	notWarm := false
	tfMs := int64(cfg.E.EMATFMin) * minuteMs
	for i := 1; i < len(cs); i++ {
		c := cs[i]
		if c.End < box.End { // the box is not final until its last minute has closed
			continue
		}
		if i < warm {
			notWarm = true
			continue
		}
		p := cs[i-1]
		if p.End != c.Start || c.End-c.Start != tfMs {
			continue // a skipped (partial) candle sits between: no cross is inferred across a hole
		}
		pr, cr := ref(p), ref(c)
		switch {
		case pr <= box.High && cr > box.High:
			sigs = append(sigs, sig{i, SideLong})
		case pr >= box.Low && cr < box.Low:
			sigs = append(sigs, sig{i, SideShort})
		}
	}
	res.Diag.Signals = len(sigs)
	if trades >= len(sigs) {
		if notWarm && len(sigs) == 0 {
			return refuse(CodeEMANotWarm, SevBlock, "only %d complete %dm candles; %d needed before the EMA%d may signal",
				len(cs), cfg.E.EMATFMin, warm, cfg.E.EMAPeriod)
		}
		if len(cs) == 0 {
			return refuse(CodeEMANotWarm, SevBlock, "no complete %dm candle", cfg.E.EMATFMin)
		}
		return refuse(CodeNoCrossYet, SevWait, "no closed %dm candle has crossed the %.2f-%.2f box yet (source %s)",
			cfg.E.EMATFMin, box.Low, box.High, cfg.E.Source)
	}
	s := sigs[trades]
	c := cs[s.i]
	age := now - c.End
	res.Diag.SignalAgeMillis = age
	if age > int64(cfg.SignalMaxAgeMin)*minuteMs {
		return refuse(CodeStaleSignal, SevBlock, "cross at the %s CT candle close is %ds old (limit %ds): not entered late",
			clockOfMs(c.End, loc), age/1000, int64(cfg.SignalMaxAgeMin)*60)
	}
	in, rf := buildEntry(cfg, res, s.side, c.Close, c.Start, c.End-1,
		fmt.Sprintf("E: %dm candle closing %s CT, %s crossed the %.2f-%.2f box (%s)", cfg.E.EMATFMin, clockOfMs(c.End, loc), cfg.E.Source,
			box.Low, box.High, map[Side]string{SideLong: "up", SideShort: "down"}[s.side]))
	if rf != nil {
		res.Refusals = append(res.Refusals, *rf)
		return res
	}
	res.Intents = append(res.Intents, in)
	return res
}

// buildCandles aggregates 1m bars into COMPLETE tf-minute candles aligned to
// the UTC epoch (every CT offset is a whole hour, so these are CT-even minutes
// for tf | 60). A bucket missing any 1m bar is NOT a candle (partial counts it).
// The bucket still forming at now is neither.
func buildCandles(closed []market.Kline, tf int, now int64) (cs []candle, partial int) {
	ms := int64(tf) * minuteMs
	i := 0
	for i < len(closed) {
		bs := (closed[i].OpenTime / ms) * ms
		j := i
		var c candle
		n := 0
		for j < len(closed) && closed[j].OpenTime < bs+ms {
			b := closed[j]
			if n == 0 {
				c = candle{Start: bs, End: bs + ms, High: b.High, Low: b.Low, Close: b.Close}
			} else {
				if b.High > c.High {
					c.High = b.High
				}
				if b.Low < c.Low {
					c.Low = b.Low
				}
				c.Close = b.Close
			}
			n++
			j++
		}
		switch {
		case bs+ms > now:
			// still forming
		case n == tf:
			cs = append(cs, c)
		default:
			partial++
		}
		i = j
	}
	return cs, partial
}

func sortedAscending(bars []market.Kline) bool {
	for i := 1; i < len(bars); i++ {
		if bars[i].OpenTime <= bars[i-1].OpenTime {
			return false
		}
	}
	return true
}

func clock(minOfDay int) string { return fmt.Sprintf("%02d:%02d", minOfDay/60, minOfDay%60) }

func clockOfMs(ms int64, loc *time.Location) string {
	return time.UnixMilli(ms).In(loc).Format("15:04 MST") // zone-suffixed: kernel/tz_guard_test.go
}
