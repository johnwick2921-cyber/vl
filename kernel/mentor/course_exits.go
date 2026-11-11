package mentor

import (
	"fmt"
	"sort"
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
	// ISBMaxStopPts (CX-9, default 20 since m466 G23 D4.1-R12): refuse an ISB
	// (and the reverse ISB) whose entry-to-stop is ABOVE this many points —
	// "nếu như một cái setup inside bar mà nó hai mươi mấy điểm, làm ơn giúp
	// em đừng vô, làm ơn đừng vô… lệnh inside bar phải là một lệnh có stop
	// loss nhỏ" [D4.1 p1 @05:41–06:08]; his own stop "20 points (really under
	// 20)" [D4.1 p1 @02:39–03:20]. D4.1 is LATER than D3.3-R1/R4 (≤ 25 ok,
	// 20–25 "giảm size" [D3.3 p1 @01:09–02:09]), so for an ISB the 20–25
	// reduce option is gone (unreachable at default). PHL/PLH keep the D3.3-R1
	// 25 (Config.StopCeilingPts). Pre-#61 default 25 (W-MENTOR-EXACT); a
	// stored mentor_isb_max_pts still wins.
	ISBMaxStopPts float64
	// KeypairCandleRatio (CX-10, default 1 = today): the X4 keypair rule sits
	// out when the gap to the next key level is ≤ ratio × the current closed
	// 1m candle (High-Low, eval.go) ("một cái nến nó bằng cái range của 2 cái level rồi thì ngồi
	// im" [X4 @06:31]). No course ratio is spoken → 1.
	KeypairCandleRatio float64
	// BoxFreshRTHPair (CX-11 case b; m429 default ON, owner ruling 2026-10-25
	// "if there is a new box use it, if not use the old box"): a fresh
	// FTGH/FTGL that forms in RTH (after 08:30 CT, built from RTH candles only)
	// REPLACES the evening box of THAT role; a role with no fresh RTH box keeps
	// its evening box. OFF: one box per role per 17:00 CT session.
	BoxFreshRTHPair bool
	// BoxMaxHeightPts (CX-11, case b, default 0 = off): a built box taller than
	// this is not a box (never latched, never a location/ban). The course
	// states no height.
	BoxMaxHeightPts float64

	// ── m466 P3 (release #61, owner order 2026-11-02 "fix everything the video
	// check found"). Top-level mentor_* knobs, resolved in
	// trader/mentor_course_exits.go like the CX-* block above. ──────────────

	// Open5mBreak (G55 D4.4-R40, knob mentor_open5m_break): what counts as the
	// break of the opening 5m range in the open window (open5mRangeFilter).
	// "1m_close" (default, "" reads the same) = a CLOSED 1m candle at or after
	// 08:35 CT closing outside the range — the course's break minimum, "một
	// cây nến một phút… đóng bên ngoài cái box là at least" [D4.1 p1
	// @20:36–21:03]; the mentor acted at the 08:43 1m escape [D4.4 p2
	// @05:15–05:36 "nó break được cái range khung 5 phút, nó đi lên rồi, tôi
	// biết tôi đánh"]. "5m_close" = the pre-#61 rule (a closed 5m bucket only,
	// up to 4 minutes later).
	Open5mBreak string
	// NewsWaitMode (G56 D4.4-R22, knob mentor_news_wait_mode): what the CE-3
	// news wait (news_wait_both_lines ON) requires. "both" (default, "" reads
	// the same) = the last close beyond the 4h line AND the pre-print 1h line
	// on the trade side — the course's first method, "Đợi canh nó nằm trên hai
	// cái đường line này, kiếm inside bar đu vô" [D4.4 p1 @21:48–22:12]; with
	// no (or a stale, D4.4-R12) 1h line the 4h alone ("1h ko có gì hết").
	// "4h_only" = the pre-#61 rule (the course's "Hoặc… chỉ cần đợi nó nằm ở
	// trên cái đường line 4 giờ").
	NewsWaitMode string
	// NewsReadLeadMin (G58 D4.4-R6, knob mentor_news_read_lead_min, default 30):
	// on a print day with news_bars_excluded ON, the 1h/4h trigger read
	// freezes this many minutes BEFORE the print (07:00 for a 07:30 print) —
	// "Một giờ thì đợi gần trước news, tầm 30 phút" / board "(trước news 30')"
	// [D4.4 p1 @05:28–05:36; board 14:33–15:36]. The order hold stays
	// −10m/+5m (the trader's F11 window). 0 = freeze from the print window's
	// own start (07:20, the pre-#61 behaviour). A lead SHORTER than the
	// window's −10m never starts the freeze later than the window.
	NewsReadLeadMin int
}

// m466 P3 knob values (the ONE spelling the kernel, the trader resolver, the
// boot line and the guide share).
const (
	Open5mBreak1mClose = "1m_close"
	Open5mBreak5mClose = "5m_close"
	NewsWaitBoth       = "both"
	NewsWait4hOnly     = "4h_only"
	// NewsReadLeadMinDefault — D4.4-R6 "tầm 30 phút" [D4.4 p1 @05:28–05:36].
	NewsReadLeadMinDefault = 30
)

// DefaultCourseExitsCfg returns the knob-rule defaults.
func DefaultCourseExitsCfg() CourseExitsCfg {
	return CourseExitsCfg{
		SwingEMA34IsFirstTarget: true,
		BoxFreshRTHPair:         true, // m429 R4: owner ruling 2026-10-25
		ReverseISBAsPHL:         true, // course-defaults-30 [D5.4 @03:52–03:56, @07:33]
		EMA9VisitCap:            EMA9VisitShared,
		KeypairCandleRatio:      1,
		ISBMaxStopPts:           20,                     // m466 G23: "hai mươi mấy điểm… đừng vô" [D4.1 p1 @05:41–06:08] (later than D3.3's 25)
		Open5mBreak:             Open5mBreak1mClose,     // m466 G55 D4.4-R40
		NewsWaitMode:            NewsWaitBoth,           // m466 G56 D4.4-R22
		NewsReadLeadMin:         NewsReadLeadMinDefault, // m466 G58 D4.4-R6
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

// isbOverMaxStop (CX-9): an ISB stop ABOVE the knob is refused — "hai mươi
// mấy điểm, làm ơn giúp em đừng vô" [D4.1 p1 @05:41–06:08] (m466 G23, default
// 20; 20.0 itself is taken — his own "20 points" [D4.1 p1 @02:39–03:20]). The
// knob has no OFF — the trader resolves unset/0 to the default.
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

// boxLatchDayKey is the latch key: the trading day. m429: the old "|rth"
// suffix is gone — the latch no longer clears at 08:30; the fresh RTH box
// replaces the evening box PER ROLE (freshRTHReplaces), so a role with no
// fresh box keeps its evening box, and a restart inside RTH keeps the seeded
// latch (the seed stored the plain day key, the tick compared "...|rth").
func boxLatchDayKey(now int64, cfg Config) string {
	return tradingDayKey(time.UnixMilli(now).In(ctime()))
}

// freshRTHReplaces (m429 R4) reports whether nb (a box built from the RTH
// slice) replaces old (the role's latched box): the knob is ON, nb formed in
// RTH (08:30 <= t < 17:00 CT) and old formed before that day's 08:30 open. An
// RTH-formed latched box is never replaced (the first RTH box stands).
//
// D3-G1: with BoxPreOpenLookbackMin L > 0 the replacement window opens L
// minutes BEFORE 08:30 — a pair formed in that window from pre-open peaks (the
// Fri 30 Oct FTGH from the ~08:00 and ~08:29 peaks, see preOpenBoxes) is the
// morning box, and any latched box formed before that window (the overnight
// pair) is the one it replaces. L = 0 is the pre-D3-G1 rule exactly.
func (e *Evaluator) freshRTHReplaces(old, nb Box) bool {
	if !e.Cfg.CourseExits.BoxFreshRTHPair {
		return false
	}
	lo := rthOpenMs(nb.FormedAtMs) - preOpenLookbackMs(e.Cfg)
	t := time.UnixMilli(nb.FormedAtMs).In(ctime())
	if nb.FormedAtMs < lo || t.Hour()*60+t.Minute() >= 17*60 {
		return false
	}
	if old.FormedAtMs < lo {
		return true
	}
	// A box formed INSIDE the pre-open window is provisional only against the
	// pair the open candle itself confirms: peak 2 of the course's pair is the
	// 08:29 candle, confirmed by the 08:30 bar, so that box forms exactly at the
	// open — one minute after the first RTH tick could already have latched a
	// smaller pre-open pair. Any later box never displaces it (first RTH box
	// stands).
	return nb.FormedAtMs == rthOpenMs(nb.FormedAtMs) && old.FormedAtMs < nb.FormedAtMs
}

// preOpenLookbackMs is the D3-G1 pre-open window in ms (0 = off).
func preOpenLookbackMs(cfg Config) int64 {
	if cfg.BoxPreOpenLookbackMin <= 0 {
		return 0
	}
	return int64(cfg.BoxPreOpenLookbackMin * 60_000)
}

// dropPreRTHPost retires the same-role post-escape boxes that formed before
// the RTH open nb formed after: they belonged to the evening box nb replaced.
func dropPreRTHPost(post []Box, nb Box) []Box {
	open := rthOpenMs(nb.FormedAtMs)
	out := post[:0:0]
	for _, p := range post {
		if p.Kind == nb.Kind && p.FormedAtMs < open {
			continue
		}
		out = append(out, p)
	}
	return out
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

// preOpenPeakSpanMs is how far back the pre-open pair search reads for PEAK 1:
// the worked example's peak 1 is the ~08:00 candle, 30 minutes before the open
// [Oct D3.3 @ 17:13-17:25; E3 frames s_00866/s_00977], plus the two bars of left
// context swings3 needs. [B: the example fixes only that 30 minutes is enough.]
const preOpenPeakSpanMs = 32 * 60_000

// preOpenBoxes (D3-G1) is the box the course draws BEFORE the open: "trước khi
// Market Open tầm 10 phút thì em coi có … nếu mà nó có thì em vẽ" [Oct D3.3 @
// 42:00] (OctD3.3-R52). It is BoxesBuild over the last preOpenPeakSpanMs of
// pre-open bars (the Fri 30 Oct FTGH: peak 1 ~08:00, peak 2 08:29, neither the
// session extreme [R23/R24]) and keeps only a box that FORMED inside the
// BoxPreOpenLookbackMin minutes up to the open. A pair that formed earlier
// (Thu 29 Oct: the 29,700 pair at 08:11) is not the morning box — the pair the
// RTH build finds is. Nil when the knob is off or `now` is outside RTH.
func (e *Evaluator) preOpenBoxes(bars []market.Kline, now int64, build BoxCfg) []Box {
	lb := preOpenLookbackMs(e.Cfg)
	if lb <= 0 || !boxRTHWindow(now, e.Cfg) {
		return nil
	}
	open := rthOpenMs(now)
	from := open - preOpenPeakSpanMs
	i := sort.Search(len(bars), func(i int) bool { return bars[i].OpenTime >= from })
	var out []Box
	for _, b := range BoxesBuild(bars[i:], build, time.UnixMilli(now)) {
		if b.FormedAtMs >= open-lb && b.FormedAtMs <= open {
			out = append(out, b)
		}
	}
	return out
}

// boxesBuildRTH is BoxesBuild over the box build slice (boxBuildBars) with the
// D3-G1 pre-open box taking its role first: it formed before any RTH box can.
func (e *Evaluator) boxesBuildRTH(bars []market.Kline, now int64, build BoxCfg) []Box {
	boxes := BoxesBuild(boxBuildBars(bars, now, e.Cfg), build, time.UnixMilli(now))
	for _, pb := range e.preOpenBoxes(bars, now, build) {
		replaced := false
		for i := range boxes {
			if boxes[i].Kind == pb.Kind {
				boxes[i] = pb
				replaced = true
			}
		}
		if !replaced {
			boxes = append(boxes, pb)
		}
	}
	return boxes
}

// buildBoxes (CX-11) is the evaluator's box build: BoxesBuild over the
// boxBuildBars slice; with the fresh-RTH pair in force the formation index is
// re-resolved against the FULL window (the latch, the touch count and the box
// return path index the full window, never the RTH slice). Knob OFF: exactly
// BoxesBuild(bars, …).
func (e *Evaluator) buildBoxes(bars []market.Kline, now int64) []Box {
	src := boxBuildBars(bars, now, e.Cfg)
	base := e.baseBoxCfg()
	build := e.boxFrameCfg(now)
	boxes := e.boxesBuildRTH(bars, now, build)
	if build.TF != base.TF {
		for i := range boxes {
			boxes[i].TF = build.TF // m410: drawn on the frame traded at draw time
		}
	}
	if len(src) != len(bars) && len(boxes) > 0 {
		full := barsForTF(bars, build.TF)
		for i := range boxes {
			if idx, ok := resolveBarIndex(full, boxes[i].FormedAtMs); ok {
				boxes[i].FormedAt = idx
			}
		}
	}
	return e.filterTallBoxes(boxes)
}

// baseBoxCfg is the configured box frame (Config.Box, default when unset).
func (e *Evaluator) baseBoxCfg() BoxCfg {
	cfg := e.Cfg.Box
	if cfg.TF == "" {
		cfg = DefaultBoxCfg()
	}
	return e.withBoxFlags(cfg)
}

// withBoxFlags overlays the box knobs that live on Config (not Config.Box) onto
// a BoxCfg, without defaulting anything else: D3-G2 box_swing_both_roles, D3-G7 box_flip_reversible.
func (e *Evaluator) withBoxFlags(cfg BoxCfg) BoxCfg {
	cfg.OneRoleSwings = !e.Cfg.BoxSwingBothRoles
	cfg.FlipPermanent = !e.Cfg.BoxFlipReversible // D3-G7
	return cfg
}

// boxFrameCfg is the frame a box is DRAWN on at `now` (m410). "Anh chị trade
// khung nào anh chị là vẽ khung đó ... trade khung 2 phút, anh chị vẽ khung 2
// phút" [D4.1 p1 @22:57-23:14]: with Exec2mAfter30m ON the boxes are drawn on
// closed 2m candles at every minute the exec schedule trades the 2m
// (exec2mReads2m: everything except 08:30 <= t < 09:00 CT), and stay on the 1m
// through the RTH-open +30 minutes. Only the default 1m base frame is
// overridden (an explicit 2m/5mha setting is the owner's choice); knob OFF
// gives the base frame exactly.
func (e *Evaluator) boxFrameCfg(now int64) BoxCfg {
	cfg := e.baseBoxCfg()
	if e.Cfg.Exec2mAfter30m && cfg.TF == "1m" && exec2mReads2m(now) {
		cfg.TF = "2m"
	}
	return cfg
}

// boxBuildCfg is the frame NEW boxes draw on for the tick in flight (set by
// Tick from boxFrameCfg); with no tick frame set it is cfg unchanged.
func (e *Evaluator) boxBuildCfg(cfg BoxCfg) BoxCfg {
	if e.boxFrameTF != "" {
		cfg.TF = e.boxFrameTF
	}
	return cfg
}

// ISBMaxStopReason is the CX-9 refusal reason ISBStopVerdict returns.
const ISBMaxStopReason = "ISB stop above mentor_isb_max_pts — refused [CX-9 knob, default 20 — \"hai mươi mấy điểm… đừng vô\" D4.1 p1 @05:41–06:08]"

// isbStopRefusal names the ISB stop refusal counter: <prefix>stop_over_max for
// the CX-9 knob. (The pre-30 <prefix>stop_twenties name is retired with the
// twenties skip it counted.)
func isbStopRefusal(prefix, why string) string {
	return prefix + "stop_over_max"
}
