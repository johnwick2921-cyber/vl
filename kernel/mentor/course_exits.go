package mentor

import (
	"fmt"
	"strings"
	"time"

	"vl/market"
)

// ── RELEASE #25 COURSE-EXITS KNOBS (lane mentor-course-exits, owner "do all
// now" 2026-10-14) ───────────────────────────────────────────────────────────
//
// Every item is its own switch, defaulted by the COMMON-25 knob rule: (a) the
// course says it and no owner ruling or open question goes against it → ON;
// (b) a ruling or an open question goes against it, or the course is silent →
// OFF / today's behaviour. The trader resolves each stored knob onto this
// block (trader/mentor_course_exits.go); an absent key runs the default below.

// EMA 9 visit-cap modes (CX-8).
const (
	EMA9VisitShared   = "shared"    // today: every EMA 9 touch counts against ONE "ema9" budget
	EMA9VisitPerLevel = "per_level" // each EMA 9 price is its own level (like the trigger retest)
	EMA9VisitExempt   = "exempt"    // EMA 9 is never visit-capped (like EMA 34)
)

// CourseExitsCfg is the kernel half of the release #25 course-exits knobs.
type CourseExitsCfg struct {
	// SwingEMA34IsFirstTarget (CX-1, case a, default ON): the swing's 5m
	// EMA 34 IS its first target — "Target đầu tiên anh chị, Hold thẳng về
	// email 34 05 phút luôn cho em" [D5.2 p2 @01:40–01:48] — so it is never
	// also the OBSTACLE the swing room rule measures (R68 refused every swing
	// whose on-side 5m EMA 34 sat closer than 2R). false = R68 (pre-#25).
	SwingEMA34IsFirstTarget bool
	// ReverseISBAsPHL (CX-6, case b, default ON — course-defaults-30, owner
	// "fix all"): tag the reverse ISB at EMA 9
	// as the PHL (long) / PLH (short) the mentor calls it — "Nó là bộ tên solo
	// 2, chứ nó không phải là Insa 3" [D5.4 @03:52–03:56], "em lại gọi là
	// Potential Low 2" [@07:33–07:37]. The tag moves the exit to the PHL/PLH
	// model (no ISB candle-3 partial, the resonance watch) and the leg budget
	// to the PHL slot; the stop stays the inside bar ± 1.5 (Q-D4 open).
	ReverseISBAsPHL bool
	// EMA9VisitCap (CX-8, case b, default "shared" — the course states no EMA 9
	// visit rule): shared | per_level | exempt.
	EMA9VisitCap string
	// ISBMaxStopPts (CX-9, default 25 — the course's ">25 skip" [D3.3 p1 @02:04],
	// stricter than the pre-m186 owner order "move to 30"): refuse an ISB (and the
	// reverse ISB) whose stop is ABOVE this many points. A stop in the twenties
	// [20, 25] is the REDUCED tier (mentorContractsFor, [D3.3 p1 @01:09 "mình giảm
	// size lại"]), not a refusal. The mentor's two lines reconcile as: D4.1 p1
	// @05:41 "một setup ISB mà nó hai mươi mấy điểm… đừng vô" (skip the twenties)
	// vs D3.3 p1 @01:09 "giảm size lại" (reduce); the stricter reading wins:
	// 20–25 reduces, >25 refuses.
	ISBMaxStopPts float64
	// KeypairCandleRatio (CX-10, default 1 = today): the X4 keypair rule sits
	// out when the gap to the next key level is ≤ ratio × the largest recent
	// 1m candle ("một cái nến nó bằng cái range của 2 cái level rồi thì ngồi
	// im" [X4 @06:31]). No course ratio is spoken → 1.
	KeypairCandleRatio float64
	// BoxFreshRTHPair (CX-11, case b, default OFF): the FTGH/FTGL latch gets a
	// FRESH pair at the 08:30 CT RTH open, built from RTH candles only (today:
	// one pair per 17:00 CT session).
	BoxFreshRTHPair bool
	// BoxMaxHeightPts (CX-11, case b, default 0 = off): a built box taller than
	// this is not a box (never latched, never a location/ban). The course
	// states no height.
	BoxMaxHeightPts float64
}

// DefaultCourseExitsCfg returns the knob-rule defaults.
func DefaultCourseExitsCfg() CourseExitsCfg {
	return CourseExitsCfg{
		SwingEMA34IsFirstTarget: true,
		ReverseISBAsPHL:         true, // course-defaults-30 [D5.4 @03:52–03:56, @07:33]
		EMA9VisitCap:            EMA9VisitShared,
		KeypairCandleRatio:      1,
		ISBMaxStopPts:           25, // the course's ">25 skip" [D3.3 p1 @02:04]; 20–25 is the reduced tier
	}
}

// NormalizeEMA9VisitCap maps a stored value onto a mode; ok=false for a value
// that is not one of the three (the caller counts it and runs "shared").
func NormalizeEMA9VisitCap(v string) (mode string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", EMA9VisitShared:
		return EMA9VisitShared, true
	case EMA9VisitPerLevel, "per-level", "perlevel":
		return EMA9VisitPerLevel, true
	case EMA9VisitExempt:
		return EMA9VisitExempt, true
	}
	return EMA9VisitShared, false
}

// visitKeyFor is visitKey with the CX-8 EMA 9 mode: per_level keys an EMA 9
// touch at its price (each spot its own door), every other mode keeps the
// constant "ema9" key.
func visitKeyFor(lvl Level, ema9Mode string) string {
	if lvl.Kind == KindEMA9 && ema9Mode == EMA9VisitPerLevel {
		return fmt.Sprintf("%s@%.2f", lvl.Key, lvl.Price)
	}
	return visitKey(lvl)
}

// visitCapAppliesFor is visitCapApplies with the CX-8 EMA 9 mode: exempt takes
// EMA 9 out of the knock-knock cap the way EMA 34 already is.
func visitCapAppliesFor(lvl Level, ema9Mode string) bool {
	if lvl.Kind == KindEMA9 && ema9Mode == EMA9VisitExempt {
		return false
	}
	return visitCapApplies(lvl)
}

// swingRoomFilterRefuses is the runSwing room decision (CX-1): with the knob
// ON the 5m EMA 34 is the swing's first target and never its obstacle, so the
// R68 room refusal does not run; OFF runs R68 unchanged.
func swingRoomFilterRefuses(cfg Config, in Intent, obstacle float64) bool {
	if cfg.CourseExits.SwingEMA34IsFirstTarget {
		return false
	}
	return swingRoomRefused(in, obstacle, cfg.RoomMultiple)
}

// isbOverMaxStop (CX-9): an ISB stop ABOVE the knob is refused (strictly — the
// knob value itself is the reduced band's top, not refused). 0 → 25 default.
func isbOverMaxStop(stopPts float64, cfg Config) bool {
	return cfg.CourseExits.ISBMaxStopPts > 0 && stopPts > cfg.CourseExits.ISBMaxStopPts
}

// keypairCandleRatio (CX-10) resolves the ratio; ≤ 0 → 1 (today).
func keypairCandleRatio(cfg Config) float64 {
	if cfg.CourseExits.KeypairCandleRatio > 0 {
		return cfg.CourseExits.KeypairCandleRatio
	}
	return 1
}

// reverseISBRetag (CX-6): with the knob ON the reverse ISB carries the setup
// the mentor names — PHL for a long, PLH for a short — and the ISB location
// sizing flags STAY (P2-2): a retagged reverse ISB inside a range or at an
// old extreme still sizes 3 (written rules 2–3), never base 5. OFF: unchanged.
func reverseISBRetag(in Intent, cfg Config) Intent {
	if !cfg.CourseExits.ReverseISBAsPHL {
		return in
	}
	if in.Side == SideLong {
		in.Setup = "PHL"
	} else {
		in.Setup = "PLH"
	}
	// P2-2: keep in.Flag — the ISB location flags (FlagISBAtOldExtreme /
	// FlagISBInRange) cut a reverse ISB to 3 exactly like a forward ISB.
	in.Reason = "PHL/PLH (" + in.Reason + ") — the mentor reads a reverse ISB as the with-trend Potential Low 2 / Lower High [D5.4 @03:52–03:56, @07:33–07:37; mentor_reverse_isb_as_phl]"
	return in
}

// boxTooTall (CX-11) reports whether a built box exceeds the height knob.
func boxTooTall(b Box, cfg Config) bool {
	return cfg.CourseExits.BoxMaxHeightPts > 0 && b.Top-b.Bottom > cfg.CourseExits.BoxMaxHeightPts
}

// filterTallBoxes (CX-11) drops boxes taller than the knob BEFORE the latch,
// counted "box_too_tall" and noted once per box edge pair.
func (e *Evaluator) filterTallBoxes(boxes []Box) []Box {
	if e.Cfg.CourseExits.BoxMaxHeightPts <= 0 || len(boxes) == 0 {
		return boxes
	}
	out := boxes[:0:0]
	for _, b := range boxes {
		if boxTooTall(b, e.Cfg) {
			e.refuse("box_too_tall")
			key := fmt.Sprintf("%s %.2f–%.2f", b.Kind, b.Bottom, b.Top)
			if e.State.TallBoxNoted == nil {
				e.State.TallBoxNoted = map[string]bool{}
			}
			if !e.State.TallBoxNoted[key] {
				e.State.TallBoxNoted[key] = true
				e.note("box %s is %.2f pts tall > mentor_box_max_height_pts %.2f — not a box (never latched) [CX-11]", key, b.Top-b.Bottom, e.Cfg.CourseExits.BoxMaxHeightPts)
			}
			continue
		}
		out = append(out, b)
	}
	return out
}

// rthOpenMs is today's 08:30 CT in ms for the instant now (the RTH open of the
// CT calendar day now falls on).
func rthOpenMs(now int64) int64 {
	t := time.UnixMilli(now).In(ctime())
	return time.Date(t.Year(), t.Month(), t.Day(), 8, 30, 0, 0, ctime()).UnixMilli()
}

// boxRTHWindow (CX-11) reports whether the fresh-RTH-pair knob is in force at
// now: 08:30 ≤ CT < 17:00 (the RTH half of the trading day).
func boxRTHWindow(now int64, cfg Config) bool {
	if !cfg.CourseExits.BoxFreshRTHPair {
		return false
	}
	t := time.UnixMilli(now).In(ctime())
	m := t.Hour()*60 + t.Minute()
	return m >= 8*60+30 && m < 17*60
}

// boxLatchDayKey (CX-11) is the latch key: the trading day, plus "|rth" while
// the fresh-RTH-pair knob is in force — so the latch clears ONCE at 08:30 CT
// and the RTH pair is drawn fresh.
func boxLatchDayKey(now int64, cfg Config) string {
	k := tradingDayKey(time.UnixMilli(now).In(ctime()))
	if boxRTHWindow(now, cfg) {
		k += "|rth"
	}
	return k
}

// boxBuildBars (CX-11) is the bar slice the boxes are built from: every bar
// (today), or only the bars that opened at/after 08:30 CT while the fresh-RTH
// pair is in force.
func boxBuildBars(bars []market.Kline, now int64, cfg Config) []market.Kline {
	if !boxRTHWindow(now, cfg) {
		return bars
	}
	from := rthOpenMs(now)
	for i, b := range bars {
		if b.OpenTime >= from {
			return bars[i:]
		}
	}
	return nil
}

// buildBoxes (CX-11) is the evaluator's box build: BoxesBuild over the
// boxBuildBars slice; with the fresh-RTH pair in force the formation index is
// re-resolved against the FULL window (the latch, the touch count and the box
// return path index the full window, never the RTH slice). Knob OFF: exactly
// BoxesBuild(bars, …).
func (e *Evaluator) buildBoxes(bars []market.Kline, now int64) []Box {
	src := boxBuildBars(bars, now, e.Cfg)
	boxes := BoxesBuild(src, e.Cfg.Box, time.UnixMilli(now))
	if len(src) != len(bars) && len(boxes) > 0 {
		cfg := e.Cfg.Box
		if cfg.TF == "" {
			cfg = DefaultBoxCfg()
		}
		full := barsForTF(bars, cfg.TF)
		for i := range boxes {
			if idx, ok := resolveBarIndex(full, boxes[i].FormedAtMs); ok {
				boxes[i].FormedAt = idx
			}
		}
	}
	return e.filterTallBoxes(boxes)
}

// ISBMaxStopReason is the CX-9 refusal reason ISBStopVerdict returns.
const ISBMaxStopReason = "ISB stop above mentor_isb_max_pts — refused [CX-9 knob, default 25 — the course's >25 skip (D3.3 p1 @02:04); 20–25 is the reduced tier]"

// isbStopRefusal names the ISB stop refusal counter: <prefix>stop_over_max for
// the CX-9 knob. (The pre-30 <prefix>stop_twenties name is retired with the
// twenties skip it counted.)
func isbStopRefusal(prefix, why string) string {
	return prefix + "stop_over_max"
}
