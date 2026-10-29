package checks

import (
	"fmt"
	"time"
)

// ── CHECK 2: CROSS-FRAME AGREEMENT ───────────────────────────────────────────
//
// INVESTIGATION (2026-10-29): over 14 Sep - 29 Oct, 99.4% of NT8's closed
// 3m/5m/15m MNQ bars equal the same span built from the stored 1m exactly. The
// ONE mismatch > 0.5 pt in the last two days (since 10-27 00:00 CT) is a single
// event that shows in three frames:
//
//	5m  row 2045884  10-27 08:30  native o 29604.25 l 29596.75 v 11424
//	                              built  o 29561.0   l 29540.25 v 62463   (43 / 56 pt, vol 5x)
//	3m  row 2045706  10-27 08:33  native o 29604.25 l 29581.0 v 25436
//	                              built  o 29579.5   l 29574.75 v 35548
//	15m 08:30                      same truncation
//
// The native 5m 08:30 bar IS the 08:34 1m bar (o 29604.25 h 29630.5 l 29596.75
// c 29625.75 v 11424 = 1m row 2045075 to the digit): the bot (re)connected at
// 08:34, the 1m minutes 08:27-08:33 came back as `historical` replay (rows
// 2052872-2052878), and the live HTF bar was BORN at the first live minute of
// its bucket - it never saw the first four minutes of its own 5m. A 56 pt wrong
// low on the 5m the trigger line reads. (A similar 10-26 03:39 event: 3m/15m
// 03:30, 17-82 pt.) The remaining sub-point diffs (0.25-1.75, e.g. 10-22 00:00
// 3m/5m high +1.75) are tick-boundary skew of live frames.
//
// THE RULE: every CLOSED HTF bar whose 1m minutes are all present (open-slot
// aware) must match the span built from the 1m: O/H/L/C within PriceTol, and
// volume within VolRel (a relative bound that only counts above VolAbsMin
// contracts). Buckets with missing 1m are SKIPPED here (that is the hole
// verdict's job) and counted. Only bars inside the decision window are judged.

// CrossParams are the tunables.
type CrossParams struct {
	WindowMin int
	PriceTol  float64 // pt: |native - built| allowed per OHLC field
	VolRel    float64 // relative volume tolerance
	VolAbsMin float64 // volume diffs below this many contracts are ignored
}

// DefaultCross: 0.5 pt (two ticks) and 25% volume - clean-day tick-boundary skew
// is <= 0.25 on 99.4% of bars; the 0.5-2 pt skew bars would pause entries for
// at most the window and are real inconsistencies of the same stored span.
func DefaultCross() CrossParams {
	return CrossParams{WindowMin: 30, PriceTol: 0.5, VolRel: 0.25, VolAbsMin: 50}
}

// FrameMismatch is one disagreeing HTF bar.
type FrameMismatch struct {
	TFMin  int
	T      int64
	Detail string
}

// CompareFrames judges htf (tfMin-minute bars) against m1. judged/skipped count
// the buckets compared / left to the hole verdict.
func CompareFrames(m1, htf []Bar, tfMin int, now time.Time, open OpenFn, p CrossParams) (out []FrameMismatch, judged, skipped int) {
	if p.WindowMin <= 0 {
		p = DefaultCross()
	}
	if tfMin <= 1 {
		return nil, 0, 0
	}
	span := int64(tfMin) * minuteMs
	nowMs := now.UnixMilli()
	from := nowMs - int64(p.WindowMin)*minuteMs
	m := byT(m1)
	for _, h := range htf {
		if h.T+span > nowMs || h.T+span <= from { // closed, and inside the window
			continue
		}
		var o, hi, lo, c, v float64
		n := 0
		complete := true
		for t := h.T; t < h.T+span; t += minuteMs {
			b, ok := m[t]
			if !ok {
				if open.open(t) {
					complete = false
					break
				}
				continue
			}
			if n == 0 {
				o, hi, lo = b.O, b.H, b.L
			}
			if b.H > hi {
				hi = b.H
			}
			if b.L < lo {
				lo = b.L
			}
			c = b.C
			v += b.V
			n++
		}
		if !complete || n == 0 {
			skipped++
			continue
		}
		judged++
		var bad []string
		chk := func(name string, native, built float64) {
			if abs(native-built) > p.PriceTol {
				bad = append(bad, fmt.Sprintf("%s native %.2f vs 1m-built %.2f", name, native, built))
			}
		}
		chk("open", h.O, o)
		chk("high", h.H, hi)
		chk("low", h.L, lo)
		chk("close", h.C, c)
		if d := abs(h.V - v); d >= p.VolAbsMin && v > 0 && d/v > p.VolRel {
			bad = append(bad, fmt.Sprintf("volume native %.0f vs 1m-built %.0f", h.V, v))
		}
		if len(bad) > 0 {
			out = append(out, FrameMismatch{TFMin: tfMin, T: h.T,
				Detail: fmt.Sprintf("%dm bar %s disagrees with the 1m it is made of: %s", tfMin, time.UnixMilli(h.T).UTC().Format("01-02 15:04Z"), joinS(bad))})
		}
	}
	return out, judged, skipped
}

func joinS(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += "; "
		}
		out += s
	}
	return out
}
