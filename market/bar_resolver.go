package market

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ── BAR-SOURCE WAVE (2026-09-02) — ONE RESOLVER ─────────────────────────────
// Before this there was no single answer to "give me completed <tf> bars".
// Two unconnected layers existed — the NT8 BarCache (native per-TF, memory
// only) and the SQLite `bars` table (1m only) — plus four hand-rolled 1m→TF
// aggregators. The weekly reader built weeks from the 1m table, which starts
// 2026-08-19, so it saw TWO completed weeks and rendered "thin · low" while
// the cache held 383 native weekly bars back to 2019-05-03 (measured live on
// 0465a10b, 2026-09-02). This file is the one door.
//
// THREE-DEEP LADDER (owner ruling 2026-09-02), encoded as DATA, not as
// special cases: native TF → aggregate from the next native TF down → own-1m
// last. For weekly that is 1w → 1d → 1m, and own-1m should never be reached.

// BarSource names where a resolved series came from. It travels WITH the bars
// so no caller has to guess, and so a mixed answer is impossible.
type BarSource string

const (
	SourceNT8Native BarSource = "nt8"     // the provider's own bars for this TF
	SourceNT8Agg    BarSource = "nt8_agg" // aggregated from a finer NT8-native TF
	SourceOwn1m     BarSource = "own1m"   // aggregated from our persisted 1m
	SourceNone      BarSource = "unavailable"
)

// BarSeries is a resolved answer: the bars, where they came from, and the
// earliest timestamp available for that TF from that source.
type BarSeries struct {
	TF         string
	Bars       []Kline
	Source     BarSource
	FromTF     string // the TF actually read (== TF for native)
	EarliestMs int64
	Completed  bool // always true — a forming bar is never returned
}

// tfMinutes is the ONE table of timeframe durations. Every ladder, bucket and
// completeness test reads it; no caller carries a literal.
var tfMinutes = map[string]int{
	"1m": 1, "3m": 3, "5m": 5, "15m": 15, "30m": 30,
	"1h": 60, "2h": 120, "4h": 240, "6h": 360, "8h": 480, "12h": 720,
	"1d": 1440, "3d": 4320, "1w": 10080,
}

// TFMinutes returns a timeframe's length in minutes (0 = unknown).
func TFMinutes(tf string) int { return tfMinutes[strings.ToLower(strings.TrimSpace(tf))] }

// barLadder is the per-TF fallback chain, as DATA. First entry is the TF
// itself (native); each later entry is a finer TF to aggregate up from. The
// resolver walks it in order and stops at the first source with bars.
var barLadder = map[string][]string{
	// 1w does NOT start at native 1w — see ladderExclusions.
	"1w":  {"1d", "1m"},
	"3d":  {"3d", "1d", "1m"},
	"1d":  {"1d", "1h", "1m"},
	"12h": {"12h", "1h", "1m"},
	"8h":  {"8h", "1h", "1m"},
	"6h":  {"6h", "1h", "1m"},
	"4h":  {"4h", "1h", "1m"},
	"2h":  {"2h", "1h", "1m"},
	"1h":  {"1h", "15m", "1m"},
	"30m": {"30m", "15m", "1m"},
	"15m": {"15m", "5m", "1m"},
	"5m":  {"5m", "1m"},
	"3m":  {"3m", "1m"},
	"1m":  {"1m"},
}

// ladderExclusions records a native TF DELIBERATELY kept out of a ladder, with
// the reason. An omission without a reason is indistinguishable from an
// oversight, and the next wave would "fix" it back in.
var ladderExclusions = map[string]string{
	"1w:1w": "NT8 native 1w bars run Friday 00:00 → Thursday 23:59 (measured on the live cache 2026-09-02: bars stamped 2026-08-21 Fri and 2026-08-28 Fri). Our weekly vocabulary is Monday-governed throughout (weekStartMonday; \"Sunday 17:00 CT first print\", \"Friday ≤16:00 CT last print\"; PWH/PWL from the prior Monday week). A Friday-stamped weekly bar STRADDLES two of our weeks and cannot be re-bucketed without inventing data, so the weekly READ is built from native 1d re-stamped as CME session days (kernel.SessionDaysFromNativeDaily: Sunday 17:00 CT open → Friday 16:00 CT close). This resolver's own 1w answer is epoch-floored (Thursday 00:00 UTC buckets) and is a reachability report for the boot line ONLY — never weekly facts (R22 P1-A: it put a Thursday close into PWC). Class 7: one stamp convention at one chokepoint.",
}

// ExcludedNative returns why a native TF is excluded from its own ladder ("" =
// not excluded). The boot line and the report READ this rather than restating
// it (A24).
func ExcludedNative(tf string) string {
	tf = strings.ToLower(strings.TrimSpace(tf))
	return ladderExclusions[tf+":"+tf]
}

// LadderFor returns the resolved fallback chain for a TF (nil = unknown TF).
// Exported so the boot line and the report quote the real ladder, never a
// hand-written copy of it (A24).
func LadderFor(tf string) []string {
	l, ok := barLadder[strings.ToLower(strings.TrimSpace(tf))]
	if !ok {
		return nil
	}
	out := make([]string, len(l))
	copy(out, l)
	return out
}

// LadderTFs returns every TF the resolver knows, coarsest first — the boot
// line and the export iterate this rather than a literal list.
func LadderTFs() []string {
	out := make([]string, 0, len(barLadder))
	for tf := range barLadder {
		out = append(out, tf)
	}
	sort.Slice(out, func(i, j int) bool { return tfMinutes[out[i]] > tfMinutes[out[j]] })
	return out
}

// BarFetcher reads raw (possibly forming) bars for one TF. Production wires
// the NT8 BarCache; fixtures wire a map. count<=0 means "all available".
type BarFetcher func(symbol, tf string, count int) []Kline

// Own1mFetcher reads our PERSISTED 1m bars for a window. Production wires the
// bars table; fixtures wire a slice.
type Own1mFetcher func(symbol string, fromMs, toMs int64) []Kline

// BarResolver answers CompletedBars/CompletedBar. Both fetchers may be nil —
// a nil fetcher is simply a rung the ladder skips.
type BarResolver struct {
	Native BarFetcher
	Own1m  Own1mFetcher
	Now    func() time.Time // nil = time.Now (fixture seam for completeness)
}

func (r *BarResolver) now() time.Time {
	if r == nil || r.Now == nil {
		return time.Now()
	}
	return r.Now()
}

// CompletedBars resolves COMPLETED bars for tf in [fromMs, toMs). A bar is
// complete when its period has closed — the repaint law: a forming bar is
// never returned, from any rung of the ladder.
func (r *BarResolver) CompletedBars(symbol, tf string, fromMs, toMs int64) (BarSeries, error) {
	tf = strings.ToLower(strings.TrimSpace(tf))
	mins := tfMinutes[tf]
	if mins == 0 {
		return BarSeries{TF: tf, Source: SourceNone}, fmt.Errorf("unknown timeframe %q", tf)
	}
	ladder := barLadder[tf]
	if len(ladder) == 0 {
		return BarSeries{TF: tf, Source: SourceNone}, fmt.Errorf("no ladder for %q", tf)
	}
	nowMs := r.now().UnixMilli()
	if toMs <= 0 || toMs > nowMs {
		toMs = nowMs
	}

	for i, from := range ladder {
		var raw []Kline
		var src BarSource
		switch {
		case from == "1m" && i > 0:
			if r.Own1m == nil {
				continue
			}
			raw = r.Own1m(symbol, fromMs, toMs)
			src = SourceOwn1m
		default:
			if r.Native == nil {
				continue
			}
			raw = r.Native(symbol, from, 0)
			if from == tf {
				src = SourceNT8Native
			} else {
				src = SourceNT8Agg
			}
		}
		if len(raw) == 0 {
			continue
		}
		var bars []Kline
		if from == tf {
			bars = dropForming(raw, mins, nowMs)
		} else {
			bars = dropForming(AggregateToTF(raw, tfMinutes[from], mins), mins, nowMs)
		}
		bars = windowed(bars, fromMs, toMs)
		if len(bars) == 0 {
			continue
		}
		return BarSeries{TF: tf, Bars: bars, Source: src, FromTF: from,
			EarliestMs: bars[0].OpenTime, Completed: true}, nil
	}
	return BarSeries{TF: tf, Source: SourceNone}, nil
}

// CompletedBar returns the single COMPLETED bar of tf containing instant t
// (nil when t falls in a bar that has not closed, or nothing is available).
func (r *BarResolver) CompletedBar(symbol, tf string, t time.Time) (*Kline, BarSeries, error) {
	mins := tfMinutes[strings.ToLower(strings.TrimSpace(tf))]
	if mins == 0 {
		return nil, BarSeries{TF: tf, Source: SourceNone}, fmt.Errorf("unknown timeframe %q", tf)
	}
	span := int64(mins) * 60000
	start := t.UnixMilli() / span * span
	s, err := r.CompletedBars(symbol, tf, start, start+span)
	if err != nil || len(s.Bars) == 0 {
		return nil, s, err
	}
	for i := range s.Bars {
		if s.Bars[i].OpenTime == start {
			return &s.Bars[i], s, nil
		}
	}
	return nil, s, nil
}

// dropForming removes any bar whose period has not closed at nowMs. This is
// the repaint law at ONE chokepoint — every rung passes through it.
func dropForming(in []Kline, mins int, nowMs int64) []Kline {
	span := int64(mins) * 60000
	out := in[:0:0]
	for _, b := range in {
		if b.OpenTime+span <= nowMs {
			out = append(out, b)
		}
	}
	return out
}

func windowed(in []Kline, fromMs, toMs int64) []Kline {
	out := in[:0:0]
	for _, b := range in {
		if b.OpenTime >= fromMs && b.OpenTime < toMs {
			out = append(out, b)
		}
	}
	return out
}

// AggregateToTF rolls finer bars up into dstMins buckets on the SAME stamp
// convention as the 1m persister: the bucket is keyed by its OPEN time, floor-
// aligned to the epoch (class 7 — one stamp convention at one chokepoint).
// Input must be ascending by OpenTime.
func AggregateToTF(in []Kline, srcMins, dstMins int) []Kline {
	if len(in) == 0 || dstMins <= 0 || srcMins <= 0 || dstMins < srcMins {
		return nil
	}
	span := int64(dstMins) * 60000
	var out []Kline
	var cur *Kline
	for i := range in {
		b := in[i]
		start := b.OpenTime / span * span
		if cur == nil || cur.OpenTime != start {
			if cur != nil {
				out = append(out, *cur)
			}
			c := b
			c.OpenTime = start
			c.CloseTime = start + span - 1
			cur = &c
			continue
		}
		if b.High > cur.High {
			cur.High = b.High
		}
		if b.Low < cur.Low {
			cur.Low = b.Low
		}
		cur.Close = b.Close
		cur.Volume += b.Volume
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}

// StampAligned reports whether a native series uses OUR epoch-floor stamp
// convention for its TF. NT8's weekly series does NOT (Fri→Thu), which is why
// 1w is excluded from the weekly ladder; this guard catches the same class of
// mismatch on any other TF the provider changes underneath us, instead of
// letting mis-bucketed bars reach a consumer silently.
func StampAligned(bars []Kline, tf string) (bool, int64) {
	mins := TFMinutes(tf)
	if mins == 0 || len(bars) == 0 {
		return true, 0
	}
	span := int64(mins) * 60000
	for _, b := range bars {
		if off := b.OpenTime % span; off != 0 {
			return false, off
		}
	}
	return true, 0
}

// StampConvention names the calendar a PROVIDER-NATIVE series for tf is
// stamped on. Persisted rows carry it so a research reader can never mistake
// an NT8 weekly bar for one of our Monday weeks (owner ruling 2026-09-02:
// persist native 1w labelled, for research only — no consumer reads it).
//
//	epoch_floor — bucket start is floor-aligned to the epoch (our convention)
//	fri_thu     — NT8 weekly: Friday 00:00 → Thursday 23:59
func StampConvention(tf string) string {
	if strings.EqualFold(strings.TrimSpace(tf), "1w") {
		return "fri_thu"
	}
	return "epoch_floor"
}
