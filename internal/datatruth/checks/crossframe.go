package checks

import (
	"fmt"
	"time"

	"vl/internal/datatruth"
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

// ── CALIBRATION (release #55 review F1-F3; every number from the corpus) ────
//
// Over 10-14 .. 10-29 the first draft paused 1,401 minutes on cross_frame, and
// two classes of its records were immaterial to every reader of the frame:
//
//	tick-skew     45 records whose worst field is <= 2 pt (10-21 17:40Z 5m low
//	              +0.75, 10-22 00:00 3m/5m high +1.75, 10-29 15:33 3m close 2.0):
//	              141 exclusive minutes, each a bare 12-15 min floor. "Never pause
//	              on data that is not lost": a closed live frame that differs from
//	              the 1m by a few ticks is the documented tick-boundary skew, and
//	              no indicator moves by more than its own tolerance (HoldMinutesFor
//	              = 0). PriceTol (0.5 pt) stays the RECORD threshold - every such
//	              record is counted (datatruth.Count, /api/health counted) - while
//	              PauseTol (= ImpactTolPts, 2 pt, strictly above) is the PAUSE threshold.
//	              Most of the corpus records exceed 2 pt and are real.
//	15m volume    15 records, all quiet overnight bars, price equal to the tick,
//	              native volume 2-4x the 1m-built (27321 vs 6852 at 10-19 20:00):
//	              175 exclusive minutes. No indicator spec reads 15m and nothing the
//	              entry paths consume reads 15m volume, so a volume-only mismatch on
//	              a frame outside VolPauseTFs is counted, not paused. 3m/5m volume
//	              still pauses (it feeds the AI path's volume reads).
//
// READERS. A closed native frame is read only by the AI path (3m/5m/15m
// indicators) - the mentor evaluator builds every frame it trades from the 1m
// ring and reads ONE native frame, the 5m, for the strong-day test (any of the
// last 12 closed bars with range >= StrongDayPts). So a mismatch concerns the
// mentor only when it FLIPS that test on a 5m bar, and then for the 12 bars the
// test reads (60 min); everything else on a frame the mentor never reads is not
// the mentor's pause (datatruth.ReaderMentor).

// MentorStrongDayBars is how many closed native 5m bars the mentor's strong-day
// test reads (trader/mentor_tick.go FuturesBarsProvider("MNQ","5m",12)).
const MentorStrongDayBars = 12

// CrossParams are the tunables.
type CrossParams struct {
	// WindowMin: the floor of how long a bar stays judged after it CLOSED -
	// the bars the trigger rules read (12 = the newest two 5m bars, four 3m
	// bars). Per frame the window is never shorter than the frame itself (a 15m
	// bar is the newest 15m bar for 15 minutes; judging it for 12 let entries
	// resume for 3 minutes while it was still wrong and made back-to-back
	// episodes).
	WindowMin int
	// HoldFor, when set, maps (reader, frame minutes, what the bar got wrong) to
	// how many minutes the wrong bar must keep THAT READER's entries paused: its
	// influence on the indicators that reader reads on that frame
	// (HoldMinutesFor). A big mismatch is judged for that long, a tick-skew only
	// for the floor. A mismatch clears early when the bar is repaired (it is
	// re-judged on every call). nil = the floor only.
	HoldFor  func(reader string, tfMin int, e BarError) int
	PriceTol float64 // pt: |native - built| per OHLC field that is RECORDED as a mismatch
	// PauseTol is the worst-field error that PAUSES (strictly above, like ImpactTolPts: "moves an indicator by MORE than 2 pt"); 0 = PriceTol (every
	// recorded price mismatch pauses - the pre-#55 behaviour, kept for fixtures).
	PauseTol  float64
	VolRel    float64 // relative volume tolerance
	VolAbsMin float64 // volume diffs below this many contracts are ignored
	// VolPauseTFs lists the frames whose VOLUME mismatch pauses; a volume-only
	// mismatch on any other frame is counted. nil = every frame.
	VolPauseTFs []int
	// StrongDayPts > 0 turns reader scoping on: the AI reader is held by every
	// material mismatch, the mentor reader only by a 5m range flip across this
	// threshold (mirrors trader mentorStrongDayPtsMin; pinned there).
	StrongDayPts float64
}

// DefaultCross: 0.5 pt (two ticks) to record, 2 pt to pause, and 40% volume above
// 200 contracts. 99.4% of closed 3m/5m/15m bars equal their 1m exactly; the rest
// are real defects of the stored span (bars born after a reconnect or finished on
// a dropped frame; 10-21 00:35 5m: native volume 300 vs 5720 built, 26.75 pt off)
// or tick-skew (counted, not paused: see CALIBRATION).
func DefaultCross() CrossParams {
	return CrossParams{WindowMin: 12, PriceTol: 0.5, PauseTol: ImpactTolPts, VolRel: 0.40, VolAbsMin: 200,
		VolPauseTFs: []int{3, 5}, StrongDayPts: 50}
}

func (p CrossParams) volPauses(tfMin int) bool {
	if p.VolPauseTFs == nil {
		return true
	}
	for _, tf := range p.VolPauseTFs {
		if tf == tfMin {
			return true
		}
	}
	return false
}

// FrameMismatch is one disagreeing HTF bar.
type FrameMismatch struct {
	TFMin  int
	T      int64
	Detail string
	// Readers names the entry paths still held by this bar (empty = every path).
	Readers []string
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
	floor := p.WindowMin
	if floor < tfMin {
		floor = tfMin
	}
	scan := floor
	if p.HoldFor != nil {
		scan = HoldCapMin
	}
	scoped := p.StrongDayPts > 0
	pauseTol := p.PauseTol
	if pauseTol <= 0 {
		pauseTol = p.PriceTol
	}
	from := nowMs - int64(scan)*minuteMs - span // open time of the oldest bar that could still be inside its hold
	m := byT(tailFrom(m1, from))
	for _, h := range tailFrom(htf, from) {
		if h.T+span > nowMs || h.T+span <= nowMs-int64(scan)*minuteMs { // closed, and closed inside the scan
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
		worst := 0.0
		chk := func(name string, native, built float64) {
			if d := abs(native - built); d > p.PriceTol {
				bad = append(bad, fmt.Sprintf("%s native %.2f vs 1m-built %.2f", name, native, built))
				if d > worst {
					worst = d
				}
			}
		}
		chk("open", h.O, o)
		chk("high", h.H, hi)
		chk("low", h.L, lo)
		chk("close", h.C, c)
		volBad := false
		if d := abs(h.V - v); d >= p.VolAbsMin && v > 0 && d/v > p.VolRel {
			volBad = true
			bad = append(bad, fmt.Sprintf("volume native %.0f vs 1m-built %.0f", h.V, v))
		}
		if len(bad) == 0 {
			continue
		}
		age := (nowMs - (h.T + span)) / minuteMs
		// what the bar got wrong, split by what an indicator reads (F1)
		be := BarError{Frame: true, CloseErr: abs(h.C - c)}
		be.RangeErr = abs(h.H - hi)
		if d := abs(h.L - lo); d > be.RangeErr {
			be.RangeErr = d
		}
		holdOf := func(reader string) int {
			if p.HoldFor == nil {
				return 0
			}
			return p.HoldFor(reader, tfMin, be)
		}
		pricePause := worst > 0 && worst > pauseTol
		var readers []string
		allHeld := false
		if scoped {
			// AI reader: any material mismatch on a frame its indicators read
			aiHold := holdOf(datatruth.ReaderAI)
			if pricePause || aiHold > 0 || (volBad && p.volPauses(tfMin)) {
				w := floor
				if aiHold > w {
					w = aiHold
				}
				if age < int64(w) {
					readers = append(readers, datatruth.ReaderAI)
				}
			}
			// mentor reader: the 5m strong-day test only
			if tfMin == 5 && p.StrongDayPts > 0 && (h.H-h.L >= p.StrongDayPts) != (hi-lo >= p.StrongDayPts) {
				if age < int64(MentorStrongDayBars*tfMin) {
					readers = append(readers, datatruth.ReaderMentor)
				}
			}
			if len(readers) == 2 {
				readers, allHeld = nil, true
			}
			if len(readers) == 0 && !allHeld {
				countImmaterial(tfMin, h.T, age, floor, worst, volBad, bad)
				continue
			}
		} else {
			// unscoped (fixtures / pre-#55 parameters): one verdict for everyone
			hold := holdOf("")
			if !(pricePause || hold > 0 || (volBad && p.volPauses(tfMin))) {
				countImmaterial(tfMin, h.T, age, floor, worst, volBad, bad)
				continue
			}
			w := floor
			if hold > w {
				w = hold
			}
			if age >= int64(w) {
				continue
			}
		}
		out = append(out, FrameMismatch{TFMin: tfMin, T: h.T, Readers: readers,
			Detail: fmt.Sprintf("%dm bar %s disagrees with the 1m it is made of: %s", tfMin, time.UnixMilli(h.T).UTC().Format("01-02 15:04Z"), joinS(bad))})
	}
	return out, judged, skipped
}

// countImmaterial notes a recorded mismatch that pauses nobody (tick-skew under
// PauseTol, volume on a frame no entry path reads): counted once per bar, only
// while the bar is still inside the floor window (a boot must not re-announce
// eight hours of old records).
func countImmaterial(tfMin int, t, age int64, floor int, worst float64, volBad bool, bad []string) {
	if age >= int64(floor) {
		return
	}
	kind := "tick-skew"
	if worst == 0 && volBad {
		kind = "volume-only"
	}
	datatruth.Count(CheckCrossFrame, fmt.Sprintf("%dm|%d|%s", tfMin, t, kind),
		fmt.Sprintf("%dm bar %s (%s, worst price error %.2f pt): %s", tfMin, time.UnixMilli(t).UTC().Format("01-02 15:04Z"), kind, worst, joinS(bad)))
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
