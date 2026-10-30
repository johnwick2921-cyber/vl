package mentor

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"vl/kernel"
	"vl/market"
)

// State is everything the evaluator carries between ticks. It is rebuildable
// from bars plus the ledger (Replay): the evaluator never stores more than
// this, and a restart recomputes it deterministically from the bar history.
type State struct {
	// Touches holds the first-touch classification per level key (§3).
	Touches map[string]Touch `json:"touches,omitempty"`
	// ISBOnly marks levels invalidated by a wrong-way close — only ISBs may
	// ever trade there after [D5.2 p2 @ 20:48].
	ISBOnly map[string]bool `json:"isb_only,omitempty"`
	// DeletedLevels holds key levels deleted by a CLOSED 1H candle whose BODY
	// closed through (KEY-LEVEL ruling 5b, slide 31–32): they stay deleted for
	// the session. A 1H wick through does NOT delete.
	DeletedLevels map[string]bool `json:"deleted_levels,omitempty"`
	// ISBArms are the live inside-bar orders (stacking counter, §2.1).
	ISBArms map[string]ISBArm `json:"isb_arms,omitempty"`
	// LastISBEvalClose is the CloseTime of the last exec candle the ISB pair was
	// evaluated on. fold #565 (D4): when the exec-2m schedule reads the ISB on
	// 2m candles, a 1m tick on an odd minute sees the SAME 2m candle — the ISB
	// pair (and the reverse ISB) is evaluated only when the exec candle
	// ADVANCED, so the closed 2m pair is never re-emitted one minute later and
	// refused as a stale intent at the trader.
	LastISBEvalClose int64 `json:"last_isb_eval_close,omitempty"`
	// BoxRefs records, per live box key, the OpenTime (ms) of the last return
	// visit already evaluated — every return trades (R1, D3.2 p2 @ 06:25),
	// each exactly once. UR-FIX class: the value is a TIME anchor, not a bar
	// index — the live window slides (the provider returns the last ~1500
	// closed bars), so a stored INDEX drifted one bar per new 1m close and
	// the incremental walk stalled after the newest bar.
	BoxRefs map[string]int64 `json:"box_refs,omitempty"`
	// Limits is the G1 leg budget + G2 loss box state machine (DS-107).
	Limits Limits `json:"limits,omitempty"`
	// Levels is the last tick's level set (FU-1 P1-2): the fill drain reads it
	// to resolve a row-fallback fill's leg extreme via the old-extreme fallback
	// when the in-memory pend was dropped by a restart.
	Levels []Level `json:"levels,omitempty"`
	// ChartLevels / ChartTrendlines (release #22, T7) are the SAME tick's set
	// AFTER every append — Levels plus the trigger lines, the EMA 34 location,
	// the box edges and the trendlines. DISPLAY ONLY (truth panel / chart);
	// nothing that trades reads them, so Levels above stays byte-identical.
	ChartLevels     []Level     `json:"-"`
	ChartTrendlines []Trendline `json:"-"`
	// ChartEMA (m134) is the display-only per-bar EMA 9/34 curve, read from the
	// same tick as ChartTrendlines. Nothing that trades reads it.
	ChartEMA []ChartEMA `json:"-"`
	// ChartSeededAt (m422) is the ms instant SeedChartDisplay filled the three
	// display sets above at boot; the first real Tick clears it. Display only.
	ChartSeededAt int64 `json:"-"`
	// Refusals is the B-rules refusal ledger (CTO 13:51:31Z): every filter
	// that DROPS an intent names its reason and counts it, like the replay
	// funnel stages — DS-105 diffs these against the replay.
	Refusals map[string]int `json:"refusals,omitempty"`
	// Counters is the B-rules NON-refusal event ledger (m296): events that
	// neither drop nor refuse an intent but must still be diffable against the
	// replay funnel (e.g. the open5m fail-open on a missing range).
	Counters map[string]int `json:"counters,omitempty"`
	// Trigger is the 5m trigger-line state (§5.1).
	Trigger TriggerLine `json:"trigger"`
	// HTF is the §5.4 4h/1h direction state (DS-106, fold item 3).
	HTF HTF `json:"htf,omitempty"`
	// Day is the §7 pre-session verdict latch (DS-106, fold item 4).
	Day DayLatch `json:"day_latch,omitempty"`
	// DayRecheck is the one-way DayOff recheck (owner ruling 2026-10-09): a
	// latched DayOff clears the moment the 4h/1h directions agree on a later
	// closed 1h bar — once cleared, never re-latched.
	DayRecheck DayRecheck `json:"day_recheck,omitempty"`
	// DayGateReport is the once-per-day record of the §7 day-gate decision
	// (owner ruling 2026-10-09): the gate's own inputs, captured the moment the
	// 08:30 latch freezes, so the per-day INFO line reads them.
	DayGateReport DayGateReport `json:"day_gate_report,omitempty"`
	// OpenHTF (MENTOR-NY item 6) — the 4h/1h directions the 08:30 day-gate
	// decision READ, replayed by a seed that ran AFTER today's 08:30 (a boot
	// mid-session) from the same closed 1m history, cut at the 08:30 read.
	// Empty Key = not computed (seeded before 08:30, or never seeded).
	OpenHTF OpenHTFRead `json:"open_htf,omitempty"`
	// Swing is the §8 4h-EMA34 swing state (DS-106 slice; rebuildable by
	// replaying bars through SwingTick).
	Swing SwingState `json:"swing,omitempty"`
	// ISBBox is the R5 5m-ISB rest box (nil = none standing). Rebuildable by
	// replaying the closed 5m buckets + 1m escapes.
	ISBBox *ISBBox `json:"isb_box,omitempty"`
	// LastISBBoxAt is the 5m ISB candle open of the last box built. After an
	// escape the same closed pair is still the latest one until the next 5m
	// candle closes, so without it the escaped box is rebuilt on the very
	// next tick and flaps on and off (CTO parity ruling 2026-10-04).
	LastISBBoxAt int64 `json:"last_isb_box_at,omitempty"`
	// LatchedBoxes pins the FIRST FTGH and FIRST FTGL drawn per trading day —
	// never redrawn on a later higher high / lower low ("không vẽ cái box
	// khác… y nguyên đó tới cuối ngày" [D4.1 p2 @02:39–02:57]). Keyed by
	// role ("ftgh"/"ftgl"); the box lives until the day ends.
	LatchedBoxes map[string]Box `json:"latched_boxes,omitempty"`
	// LatchedBoxesDay is the trading day the latch belongs to; on a new day
	// the latch is cleared and the first box of the new day is drawn.
	LatchedBoxesDay string `json:"latched_boxes_day,omitempty"`
	// PostEscapeBoxes (B52, release #29) holds the NEW-zone boxes latched
	// after the standing box escaped: one per further escape, outside the old
	// range. Order is formation order; the last of a role is that role's
	// active box.
	PostEscapeBoxes []Box `json:"post_escape_boxes,omitempty"`
	// TallBoxNoted (CX-11) dedups the "box too tall" note per box.
	TallBoxNoted map[string]bool `json:"-"`
	// ISBBox15m is the 15m ISB rest box (D4.1-25): the latest CLOSED 15m
	// inside-bar candle, boxed like R5. Nil = none standing. Rebuildable by
	// replaying the closed 15m buckets + 1m escapes.
	ISBBox15m *ISBBox `json:"isb_box_15m,omitempty"`
	// LastISBBox15mAt is the 15m ISB candle open of the last box built (the
	// same flap guard as LastISBBoxAt).
	LastISBBox15mAt int64 `json:"last_isb_box_15m_at,omitempty"`
	// ISBBox30m is the 30m ISB rest box (D4.2-07): the latest CLOSED 30m
	// inside-bar candle, boxed like R5. Nil = none standing.
	ISBBox30m *ISBBox `json:"isb_box_30m,omitempty"`
	// LastISBBox30mAt is the 30m ISB candle open of the last box built.
	LastISBBox30mAt int64 `json:"last_isb_box_30m_at,omitempty"`
	// SchoolOneSide / SchoolOneUpgraded — B20 flip tracking: a school-1 entry
	// was emitted without the 5m trigger agreeing; when the trigger later
	// flips to that side the evaluator emits ConfluenceUpgrade once.
	SchoolOneSide     Side `json:"school_one_side,omitempty"`
	SchoolOneUpgraded bool `json:"school_one_upgraded,omitempty"`
	// Visits / VisitsDay — B23 per-day visit counts per level key ("knock
	// knock", D1.3 p1 @10:32–11:43). Rebuilt by replaying the touch loop.
	Visits    map[string]int `json:"visits,omitempty"`
	VisitsDay string         `json:"visits_day,omitempty"`
	// VisitCapRefused — B23 visit-cap refusal ONCE per visit (CTO 01:19Z,
	// DS-105): the PHL/PLH reject path re-reads a capped rejected level every
	// tick, so the per-tick e.refuse inflated level_visit_cap (12,375 for a
	// handful of real refusals) and hid real refusals in the funnel line. A
	// per-level mark makes it fire once; the touch loop clears the mark on
	// visit departure so a NEW visit re-arms it.
	VisitCapRefused map[string]bool `json:"visit_cap_refused,omitempty"`
	// PHLRefusedVisit — r22 (P2-1): the once-per-visit marks of the PHL/PLH
	// refusals that used to be SILENT (EMA gate, location, 5m box, mid-range,
	// resting arm). Keyed "<level key>|<reason>"; cleared on the level's visit
	// departure and at the day rollover.
	PHLRefusedVisit map[string]bool `json:"phl_refused_visit,omitempty"`
	// ORB is the §7 step 0 opening-range gate state (drawn at 08:32 CT, escape
	// latches on the first 1m body close outside). Per-session-day.
	ORB ORB `json:"orb,omitempty"`

	// Seeded state (P0 1791009358785): built by Seed from the STORED bars and
	// updated incrementally — never rebuilt from the live slice.
	SeedLevels      []Level `json:"seed_levels,omitempty"` // 1H RTH key levels, full stored history
	Seed1HWatermark int64   `json:"seed_1h_watermark,omitempty"`
	// Seed1HBars is the full 1H RTH candle series (stored history at seed,
	// extended incrementally per tick). The KEY-LEVEL deletion check runs
	// against it per tick — re-aggregating the whole slice per level per tick
	// is O(levels x bars) and timed the replay out.
	Seed1HBars       []market.Kline `json:"seed_1h_bars,omitempty"`
	Seed1HLastColour bool           `json:"seed_1h_last_colour,omitempty"`
	// Seed1HDeletionWatermark is the OpenTime of the last 1H RTH candle whose
	// BODY-close deletion has been evaluated (KEYLEVEL-FULL-HISTORY, release
	// #10): deletion is computed ONCE over the full seeded history, then only
	// the NEW candles past this watermark are walked per tick — never the
	// whole series per level per tick (the O(levels x bars) replay killer).
	Seed1HDeletionWatermark int64   `json:"seed_1h_deletion_watermark,omitempty"`
	Seed1mWatermark         int64   `json:"seed_1m_watermark,omitempty"`
	EMA34                   float64 `json:"ema34,omitempty"` // 1m EMA 34 (incremental)
	EMA9                    float64 `json:"ema9,omitempty"`  // 1m EMA 9 (incremental)
	// EMA34Warmed (m327 b) marks that the seed actually folded ≥1 closed 1m bar
	// into State.EMA34 — a nil-bar / fail-closed seed leaves it false, so the
	// EMA-location level must NOT read the garbage State.EMA34 that seededLevels
	// would advance from 0. Never reset to false once the EMA is warm.
	EMA34Warmed bool `json:"ema34_warmed,omitempty"`

	// M401: the incremental 2m EMA 34/9 over CLOSED 2m buckets, the frame the
	// EMA levels + EMA34 location read outside 08:30–09:00 CT when
	// Exec2mAfter30m is ON. Only maintained while the knob is ON (zero and
	// omitted otherwise). Seed2mWatermark is the CloseTime of the last folded
	// 2m bucket.
	EMA34x2         float64 `json:"ema34_2m,omitempty"`
	EMA9x2          float64 `json:"ema9_2m,omitempty"`
	Seed2mWatermark int64   `json:"seed_2m_watermark,omitempty"`
	EMA2mWarmed     bool    `json:"ema2m_warmed,omitempty"`
	// EMAFrame is the frame (1 or 2) the EMA lines were last read on; 0 = not
	// read yet (or knob OFF). A 1<->2 change starts a fresh visit for the EMA
	// keys only (resetEMAVisitsOnFrameSwitch).
	EMAFrame int `json:"ema_frame,omitempty"`

	// E2 (CTO 12:27:25Z): the EMA34 loss machinery — the pending stop of the
	// last emitted EMA setup, and the one-loss block until a departure.
	EmaPendingSide   Side    `json:"ema_pending_side,omitempty"`
	EmaPendingEntry  float64 `json:"ema_pending_entry,omitempty"`
	EmaPendingStop   float64 `json:"ema_pending_stop,omitempty"`
	EmaPendingTarget float64 `json:"ema_pending_target,omitempty"`
	EmaPendingExpiry int64   `json:"ema_pending_expiry,omitempty"`
	EmaPendingFilled bool    `json:"ema_pending_filled,omitempty"`
	EmaLossPrice     float64 `json:"ema_loss_price,omitempty"`
	EmaLossBarTime   int64   `json:"ema_loss_bar_time,omitempty"`
	EmaBlocked       bool    `json:"ema_blocked,omitempty"`
	// EmaPendingArmID (m425a S2) is the "lvl-" ArmID the pending belongs to, so
	// a refused/dropped intent can retract exactly its own pending (a refused
	// EMA34 intent must never be booked as a loss — ema_rules.go).
	EmaPendingArmID string `json:"ema_pending_arm_id,omitempty"`
	// RefusedLevelTouch (m425a S2) maps a level key to the CloseTime of the
	// touch reference bar whose intent the TRADER refused. The refusal does not
	// end the touch (it stays TouchReject), so without this the level was
	// re-emitted every minute for the rest of the visit.
	RefusedLevelTouch map[string]int64 `json:"refused_level_touch,omitempty"`
	// EmaBlockDayKey is the 17:00 CT session-day key of the E2 block (B8): the
	// block lifts at the next rollover, like the G2 loss boxes.
	EmaBlockDayKey string `json:"ema_block_day_key,omitempty"`
	// EmaLossZoneLo/Hi — CE-6 (release #25): the losing EMA trade's
	// entry-to-stop zone. With EmaLossRelock "until_zone_exit" the block lifts
	// when a CLOSED 1m candle lies completely outside it (wicks included)
	// [D4.2 p2 @01:33–01:39].
	EmaLossZoneLo float64 `json:"ema_loss_zone_lo,omitempty"`
	EmaLossZoneHi float64 `json:"ema_loss_zone_hi,omitempty"`
	// LastISBBoxDir / LastISBBoxEscape — CE-1 (release #25): the direction of
	// the last 5m ISB box drawn (LastISBBoxAt) and the side it escaped to (""
	// while it stands). fifteenMinISBGate reads them.
	LastISBBoxDir    Side `json:"last_isb_box_dir,omitempty"`
	LastISBBoxEscape Side `json:"last_isb_box_escape,omitempty"`
	// TFConflictLatched — CE-2 (release #25, tf_conflict_end both_boxes): a
	// 15m/5m conflict was seen and not every box has escaped yet.
	TFConflictLatched bool `json:"tf_conflict_latched,omitempty"`
	// ArmSeq names the next arm.
	ArmSeq int `json:"arm_seq"`
	// LevelArms are the RESTING level orders (B6, 10-03 ruling): a level
	// order has no one-candle expiry — it rests until a later candle closes
	// through its level or its own 15:00 CT expiry — LevelArmExpiry(PlacedAt).
	// Keyed by the level key so a resting order is never re-emitted (B16: one
	// intent per reference).
	LevelArms map[string]LevelArm `json:"level_arms,omitempty"`
	// RollingPHL is the D2.2 pullback-continuation rolling stop-entry state
	// (m216: the pullback leg only — level touches stay first-touch-fixed).
	RollingPHL RollingPHL `json:"rolling_phl,omitempty"`
}

// MarshalState renders the state for the ledger (rebuild source).
func MarshalState(s State) ([]byte, error) { return json.Marshal(s) }

// UnmarshalState restores a state from the ledger.
func UnmarshalState(b []byte) (State, error) {
	var s State
	err := json.Unmarshal(b, &s)
	if s.Touches == nil {
		s.Touches = map[string]Touch{}
	}
	if s.ISBOnly == nil {
		s.ISBOnly = map[string]bool{}
	}
	if s.VisitCapRefused == nil {
		s.VisitCapRefused = map[string]bool{}
	}
	if s.DeletedLevels == nil {
		s.DeletedLevels = map[string]bool{}
	}
	if s.ISBArms == nil {
		s.ISBArms = map[string]ISBArm{}
	}
	if s.BoxRefs == nil {
		s.BoxRefs = map[string]int64{}
	}
	return s, err
}

// SetSeamCovered hands the evaluator the roll-seam minutes of the stitched
// series the NEXT SeedFull / ReseedState reads (RollStitcher.SeamCovered):
// minutes some contract stores that the cut dropped from the series. The 4h
// EMA34 hole scan counts a minute present when ANY contract stores it, so a
// quarterly roll seam is not read as a data hole (m456 defect 2). Pass nil when
// the seed series is not a stitch. The slice is copied and sorted.
func (e *Evaluator) SetSeamCovered(seam []int64) {
	e.seamCovered = append([]int64(nil), seam...)
	sort.Slice(e.seamCovered, func(i, j int) bool { return e.seamCovered[i] < e.seamCovered[j] })
}

// Evaluator runs the mentor rules once per closed 1m candle. It emits intents
// only; nothing here places an order (P3's injector does). Ticks are pure
// functions of (bars, now, state): the same history always rebuilds the same
// state.
type Evaluator struct {
	Cfg   Config
	State State

	// P0 seeding: seeded/missing set by Seed; seedLine is the one-line report.
	seeded  bool
	missing []string
	// depth / depth4hBucket / depthMet: the live per-source depth the
	// fail-closed gate re-checks every Tick (advanceDepth).
	depth         seedDepth
	depth4hBucket int64
	depthMet      string
	seedLine      string
	// seamCovered (m456 defect 2): minutes a contract of the stitch STORES that
	// the stitched series handed to the seed lacks (RollStitcher.SeamCovered,
	// ascending 1m open times). The swing line's hole scan counts them as
	// present. Set by SetSeamCovered before every SeedFull / ReseedState; not
	// part of State (the series it describes is rebuilt on every seed).
	seamCovered []int64
	// tfMemo caches barsTF aggregations within ONE Tick (FU-2): one build per
	// (tf, slice identity) per Tick, cleared at the start of each Tick.
	// Evaluator-owned on purpose — two traders run two evaluators, each with
	// its own memo (a package-global cache would cross the streams).
	tfMemo map[int]tfMemoEntry
	// boxFrameTF is the frame new boxes draw on for the Tick in flight (m410),
	// "" = the configured base frame. Set at the top of the box section.
	boxFrameTF string
	// pullbackWarn5m is the X11 pullback warning for THIS tick (transient, not
	// serialized): the ENTRY SIDE the warning applies to ("" = none). A 5m
	// uptrend whose latest closed candle broke the prior one's low warns LONG
	// entries; the mirror (5m downtrend, prior high broken) warns SHORT entries
	// → size down [X11 @13:21–15:02]. C52-W10 (B20).
	pullbackWarn5m Side
	// notes are the "changed on this tick" lines (release #22 LINES-RESTART:
	// a 4h/1h trigger move, a box latched or cleared, a trendline turning
	// valid or dying, a swing-line gap fold). The trader drains them with
	// TakeNotes after each Tick and logs each one; capped so a replay that
	// never drains cannot grow it without bound.
	notes []string
	// Trendline change tracking (release #22, R4): the VALID lines seen last
	// tick (a valid line leaves only by a 5m-close break or the session end —
	// each is a change line), and the crossed-between-anchors pairs already
	// counted today. Rebuilt from bars on restart; the first tick after a
	// restart only initialises them (no burst of "became valid" lines).
	tlDay     string
	tlValid   map[string]bool
	tlTF      map[string]int // trendline key → TFMin (0 = 1m; 60/240 = persistent HTF) [D2.3 p1 @04:23–05:41]
	tlCrossed map[string]bool
	tlCarry   map[string]carriedTL // m407: valid intraday lines latched across the 08:30/09:00 build-frame switch
	tlHTF     map[string]Trendline // live PERSISTENT HTF lines by key — anchors kept across the window slide [D2.3 p1 @04:23–05:41]
}

// maxNotes bounds the undrained note buffer (replays and tests never drain).
const maxNotes = 64

// note records one change line for the trader to log (READ values only).
func (e *Evaluator) note(format string, args ...any) {
	if len(e.notes) >= maxNotes {
		e.notes = e.notes[1:]
	}
	e.notes = append(e.notes, fmt.Sprintf(format, args...))
}

// noteTriggerMove records a 4h/1h trigger-line move (direction or line price)
// with the bar that fired it — the live record a later restart line can be
// checked against (release #22: the moves were never logged, so two boot
// lines 7 h apart could not be told apart from a restart flip).
func (e *Evaluator) noteTriggerMove(name string, prev, cur TriggerLine, bars []market.Kline) {
	if cur.Dir == "" || (cur.Dir == prev.Dir && cur.Price == prev.Price && cur.MovedAt == prev.MovedAt) {
		return
	}
	from := "none"
	if prev.Dir != "" {
		from = fmt.Sprintf("%s @%.2f", prev.Dir, prev.Price)
	}
	at := "n/a"
	if n := len(bars); n > 0 {
		at = kernel.TableTimeCT(time.UnixMilli(bars[n-1].OpenTime))
	}
	e.note("%s trigger moved %s → %s @%.2f (bucket %s CT, broke on the %s CT bar, read intrabar) [D3.4 p1 @08:19]",
		name, from, cur.Dir, cur.Price, kernel.TableTimeCT(time.UnixMilli(cur.MovedAt)), at)
}

// htFTrendlines maintains the PERSISTENT higher-timeframe trendlines in
// evaluator state [D2.3 p1 @04:23–05:41]: the anchors are kept across the
// window slide (the 25-hour rebuild window does NOT delete a 1h/4h line), a
// line dies only on a close-through on its OWN timeframe, and a 3rd touch
// latches once it happens. New lines whose anchors are inside the current
// window are folded in; every held line is then break-checked by time. It
// returns the live lines (to join the level set) and the ones that broke
// this tick (for the note).
func (e *Evaluator) htFTrendlines(bars []market.Kline, now time.Time) (live, broken []Trendline) {
	if e.tlHTF == nil {
		e.tlHTF = map[string]Trendline{}
	}
	// Bucket each timeframe ONCE per tick — the held-state loop below runs per
	// line and must not re-bucket the whole window for each one.
	tfBars := map[int][]market.Kline{60: e.barsTFMemo(bars, 60), 240: e.barsTFMemo(bars, 240)}
	b5 := e.barsTFMemo(bars, 5) // m428 R1: an HTF line breaks on a closed 5m candle wholly beyond it
	for _, tf := range []int{60, 240} {
		for _, tl := range trendlinesBuildTFFrames(bars, tfBars[tf], b5, tf, now) {
			k := tl.key()
			if old, ok := e.tlHTF[k]; ok && old.ValidAt > 0 && tl.ValidAt == 0 {
				continue // keep the latched-valid line; never regress it
			}
			e.tlHTF[k] = tl
		}
	}
	for k, tl := range e.tlHTF {
		btf := tfBars[tl.TFMin]
		if btf == nil {
			btf = e.barsTFMemo(bars, tl.TFMin)
		}
		if tl.Dead || trendlineHTFBroken(tl, b5, now) {
			tl.Dead = true
			delete(e.tlHTF, k)
			broken = append(broken, tl)
			continue
		}
		if tl.ValidAt == 0 {
			if va := trendlineHTFTouch(tl, btf, now); va > 0 {
				tl.ValidAt = va
			}
		}
		e.tlHTF[k] = tl
		live = append(live, tl)
	}
	return live, broken
}

// trackTrendlines counts each crossed-between-anchors pair once per trading
// day (refusal "trendline_crossed_between_anchors") and records a change line
// when a line turns VALID (a location) or a valid line is broken by a 5m
// close. The session roll clears both sets — the lines die at day end
// [D4.1 p2 @01:17].
func (e *Evaluator) trackTrendlines(lines, broken []Trendline, crossed []string, now int64) {
	day := tradingDayKey(time.UnixMilli(now).In(ctime()))
	first := e.tlValid == nil
	if e.tlDay != day {
		if e.tlDay != "" && len(e.tlValid) > 0 {
			// Only the 1m lines die at the session end [D4.1 p2 @01:17]; the
			// 1h/4h lines persist [D2.3 p1 @04:23–05:41] and are kept.
			n := 0
			for k := range e.tlValid {
				if e.tlTF[k] == 0 {
					n++
				}
			}
			if n > 0 {
				e.note("trendlines deleted at the session end: %d valid 1m line(s) of trading day %s [D4.1 p2 @01:17]", n, e.tlDay)
			}
		}
		e.tlDay = day
		if e.tlValid == nil {
			e.tlValid = map[string]bool{}
		} else {
			for k, tf := range e.tlTF {
				if tf == 0 {
					delete(e.tlValid, k)
					delete(e.tlTF, k)
				}
			}
		}
		if e.tlTF == nil {
			e.tlTF = map[string]int{}
		}
		e.tlCrossed = map[string]bool{}
	}
	for _, k := range crossed {
		if !e.tlCrossed[k] {
			e.tlCrossed[k] = true
			e.refuse("trendline_crossed_between_anchors")
		}
	}
	// Persistent HTF lines that broke THIS tick are already removed from
	// tlHTF; they never enter `lines`, so they are noted here.
	for _, tl := range broken {
		k := tl.key()
		if !e.tlValid[k] {
			continue // existed but never valid — a silent discard
		}
		delete(e.tlValid, k)
		delete(e.tlTF, k)
		e.note("trendline BROKEN by an HTF close %s %.2f→%.2f (%s → %s CT) — discarded [D2.3 p1 @04:23–05:41]",
			tl.Side, tl.P0Px, tl.P1Px, kernel.CloseHHMMCT(time.UnixMilli(tl.P0T)), kernel.CloseHHMMCT(time.UnixMilli(tl.P1T)))
	}
	for _, tl := range lines {
		k := tl.key()
		switch {
		case tl.ValidAt > 0 && !tl.Dead && !e.tlValid[k]:
			e.tlValid[k] = true
			e.tlTF[k] = tl.TFMin
			if !first {
				if tl.TFMin > 0 {
					e.note("trendline VALID (3rd touch, a return) %s %.2f→%.2f (%s → %s CT) — a PERSISTENT %dm location until an HTF close breaks it [D2.3 p1 @04:23–05:41]",
						tl.Side, tl.P0Px, tl.P1Px, kernel.CloseHHMMCT(time.UnixMilli(tl.P0T)), kernel.CloseHHMMCT(time.UnixMilli(tl.P1T)), tl.TFMin)
				} else {
					e.note("trendline VALID (3rd touch, a return) %s %.2f→%.2f (%s → %s CT) — a location until a 5m close breaks it or the session ends [D3.2 p2 @03:45]",
						tl.Side, tl.P0Px, tl.P1Px, kernel.CloseHHMMCT(time.UnixMilli(tl.P0T)), kernel.CloseHHMMCT(time.UnixMilli(tl.P1T)))
				}
			}
		case tl.Dead && e.tlValid[k]:
			delete(e.tlValid, k)
			delete(e.tlTF, k)
			if tl.TFMin > 0 {
				e.note("trendline BROKEN by an HTF close %s %.2f→%.2f (%s → %s CT) — discarded [D2.3 p1 @04:23–05:41]",
					tl.Side, tl.P0Px, tl.P1Px, kernel.CloseHHMMCT(time.UnixMilli(tl.P0T)), kernel.CloseHHMMCT(time.UnixMilli(tl.P1T)))
			} else {
				e.note("trendline BROKEN by a 5m close %s %.2f→%.2f (%s → %s CT) — discarded [X9 @06:20–06:47]",
					tl.Side, tl.P0Px, tl.P1Px, kernel.CloseHHMMCT(time.UnixMilli(tl.P0T)), kernel.CloseHHMMCT(time.UnixMilli(tl.P1T)))
			}
		}
	}
}

// TakeNotes returns the change lines recorded since the last call, then
// clears them — the trader logs each once.
// noteORBEscape records an ORB escape (the first of the day) or a SIDE SWITCH
// (a later body escape beyond the other edge) with the candle that made it —
// READ values only. Nothing is noted while the side is unchanged.
func (e *Evaluator) noteORBEscape(prev, cur ORB, c market.Kline) {
	if cur.Escaped == "" || (cur.Day == prev.Day && cur.Escaped == prev.Escaped) {
		return
	}
	edge, word := cur.High, "above the high"
	if cur.Escaped == SideShort {
		edge, word = cur.Low, "below the low"
	}
	at := kernel.TableTimeCT(time.UnixMilli(c.OpenTime))
	if cur.Day == prev.Day && prev.Escaped != "" {
		e.note("ORB side SWITCHED %s → %s: the %s CT 1m body (open %.2f, close %.2f) is wholly %s %.2f — %s entries only now [X7 @07:42–07:51 \"cho tới khi nào nó crack\"; D4.1 p1 @20:36]",
			prev.Escaped, cur.Escaped, at, c.Open, c.Close, word, edge, cur.Escaped)
		return
	}
	e.note("ORB escape %s: the %s CT 1m body (open %.2f, close %.2f) is wholly %s %.2f (ORB %.2f–%.2f) — %s entries only [X5 @02:57–03:02; D4.1 p1 @20:36]",
		cur.Escaped, at, c.Open, c.Close, word, edge, cur.Low, cur.High, cur.Escaped)
}

func (e *Evaluator) TakeNotes() []string {
	n := e.notes
	e.notes = nil
	return n
}

func New(cfg Config) *Evaluator {
	return &Evaluator{Cfg: cfg, State: State{
		Touches:         map[string]Touch{},
		ISBOnly:         map[string]bool{},
		VisitCapRefused: map[string]bool{},
		DeletedLevels:   map[string]bool{},
		ISBArms:         map[string]ISBArm{},
		BoxRefs:         map[string]int64{},
	}}
}

// Levels computes the full mentor level set from the 1m history: colour-change
// key levels (§4.3), EMA 34/9 (§8/§11), and the ONE nearest unbroken old high
// and old low (swingOldExtremeLevels, picked from the bot's own swing pivots
// [DS-106 §1: kernel/levels_swing.go SwingPointLevels]).
// swingPointNow is the clock handed to kernel.SwingPointLevels. That function
// drops a bucket whose CloseTime >= now, and kernel.aggregateBars gives a
// bucket CloseTime = open+interval (exclusive end, unlike barsTF's -1). With
// now = the instant the last bar closed (CloseTime+1 of the 1m bar) that bucket
// would still read as forming for one more bar, so the mentor passes now+1.
// Mentor-local on purpose: aggregateBars is shared with the live structure
// engine and must not change.
func swingPointNow(now int64) int64 { return now + 1 }

func Levels(bars []market.Kline, cfg Config, now int64) []Level {
	var out []Level
	out = append(out, KeyLevels(bars, cfg)...)
	out = append(out, EMALevelsAt(bars, cfg, now)...)
	out = append(out, swingOldExtremeLevels(bars, now)...)
	return withFlatSR(out, bars, cfg, now) // m428 R2: flat swing pairs are support/resistance levels
}

// swingOldExtremeLevels is the old-high/low set — the ONE source the trading
// code AND the chart read (r22 P1). The course has ONE nearest old high above
// price and ONE nearest old low below it on the 1-minute trading chart: the
// first rung of the target ladder ("nearest old high → EMA 34 → the next
// level" [D3.3 p1 @04:11, 07:12]), the PHL/PLH target ("gần đỉnh cũ" [D4.1 p1
// @07:27]) and ISB written rule 2 ("hạn chế trade isb ở ngay đỉnh hoặc đáy cũ"
// [D4.1 p1 @08:05 written]). The candidates are the bot's swing pivots
// (kernel.SwingPointLevels, 5m+15m — every pivot price is a 1m wick); from
// them it keeps:
//
//   - only the UNBROKEN ones: a pivot high is broken once a later CLOSED 1m
//     candle trades above it (a low: below it) — "a candle that breaks the
//     previous high" is the wick, the candle need not close [law 2, D3.4 p1
//     @08:19]. The test reads the 1m tape up to the candle BEFORE the newest
//     one, so the candle being judged (the ISB candle, the touch) is measured
//     against the extremes that stood before it;
//   - the NEAREST unbroken high and the nearest unbroken low. An unbroken high
//     sits above every later candle, so the nearest is the lowest one (the most
//     recent); mirror for the low.
//
// Each line carries its explicit Role (OldHigh / OldLow) from the pivot kind.
// A pivot with no confirmation instant cannot be proven unbroken and is
// skipped (fail closed: no line without evidence). The old build returned all
// 12 raw pivots (3 highs + 3 lows per TF), duplicated and mostly broken: on
// 10-13 the ISB "at an old high/low" zone covered 183/390 RTH candles.
func swingOldExtremeLevels(bars []market.Kline, now int64) []Level {
	if len(bars) < 2 {
		return nil
	}
	swings := kernel.SwingPointLevelsAll(bars, time.UnixMilli(swingPointNow(now))) // N7: unbroken test over the WHOLE lookback, then the nearest
	if len(swings) == 0 {
		return nil
	}
	tape := bars[:len(bars)-1] // the closed 1m candles before the newest one
	var hi, lo Level
	for _, d := range swings {
		isHigh := d.Kind == kernel.KindSWGH
		if !isHigh && d.Kind != kernel.KindSWGL {
			continue
		}
		if d.FormedCloseMs == nil {
			continue
		}
		at := *d.FormedCloseMs
		if oldExtremeBroken(tape, d.Price, at, isHigh) {
			continue
		}
		l := Level{
			Key:    fmt.Sprintf("%s:%.2f", KindOldExtreme, d.Price),
			Kind:   KindOldExtreme,
			Price:  d.Price,
			AtTime: at,
			Role:   OldLow,
		}
		if isHigh {
			l.Role = OldHigh
			if hi.Kind == "" || d.Price < hi.Price || d.Price == hi.Price && at > hi.AtTime {
				hi = l
			}
			continue
		}
		if lo.Kind == "" || d.Price > lo.Price || d.Price == lo.Price && at > lo.AtTime {
			lo = l
		}
	}
	var out []Level
	if hi.Kind != "" {
		out = append(out, hi)
	}
	if lo.Kind != "" {
		out = append(out, lo)
	}
	return out
}

// oldExtremeBroken reports whether a closed 1m candle that opened at or after
// the pivot's confirmation instant traded beyond it — above a high, below a
// low (a touch AT the price is not a break). The candles between the pivot and
// its confirmation cannot break it: the fractal requires them to stay inside.
func oldExtremeBroken(tape []market.Kline, price float64, confirmedAt int64, isHigh bool) bool {
	i := sort.Search(len(tape), func(i int) bool { return tape[i].OpenTime >= confirmedAt })
	for ; i < len(tape); i++ {
		if isHigh && tape[i].High > price || !isHigh && tape[i].Low < price {
			return true
		}
	}
	return false
}

// withoutDeleted drops the deleted key levels from the level set (deletion is
// by key; recomputed levels re-derive the same keys).
func withoutDeleted(levels []Level, deleted map[string]bool) []Level {
	if len(deleted) == 0 {
		return levels
	}
	out := make([]Level, 0, len(levels))
	for _, l := range levels {
		if !deleted[l.Key] {
			out = append(out, l)
		}
	}
	return out
}

// handleTouchIntents applies a level's touch intents to the evaluator
// state. BOX RULING part 2: the invalid-level rule does NOT apply to box
// edges — a box dies only on escape (1m body closes outside) or at the end
// of the day — so a wrong-way close at a box edge emits nothing and never
// marks the edge ISB-only.
func handleTouchIntents(e *Evaluator, lvl Level, intents []Intent) []Intent {
	var out []Intent
	for _, in := range intents {
		// CTO (release 10-05-1, replay noise): the wrong-way touch's CancelArm
		// names no order. Bind it to the level's RESTING order when one exists;
		// with none resting there is nothing to cancel, so emit nothing — an
		// id-less cancel only became a "REFUSED: unknown ArmID" WARN in the
		// trader, thousands per day once moving lines re-arm per visit.
		if in.Action == CancelArm && in.ArmID == "" {
			arm, resting := e.State.LevelArms[lvl.Key]
			if !resting || arm.ArmID == "" {
				continue
			}
			in.ArmID = arm.ArmID
		}
		if in.Action == LevelInvalid {
			if isBoxEdge(lvl) {
				continue
			}
			// MENTOR-NY item 5: the first touch closed on the wrong side → the
			// level is invalid for PHL/PLH [D5.2 p1 @19:51–20:25]. Course-
			// faithful but a large hidden suppressor (RTH 10-12 75, 10-13 65,
			// 10-14 25) the funnel never showed — counted here, once per
			// invalidation (the touch machinery emits it once per visit).
			if !e.State.ISBOnly[lvl.Key] {
				e.refuse("level_invalid_first_touch")
			}
			e.State.ISBOnly[lvl.Key] = true
		}
		out = append(out, in)
	}
	return out
}

// rthWindowEndMin is the RTH window CLOSE in CT minutes (15:00). It is now
// read only by orb.go (the ORB's 08:30–15:00 RTH close boundary) — level
// orders no longer use it; they expire at LevelArmExpiry(PlacedAt).
const rthWindowEndMin = 15 * 60

// LevelArm is one resting level order (B6): the touch level it was placed
// at, its side and its placement candle — the cancel sweep closes it when a
// LATER candle closes through the level or at its own 15:00 CT expiry
// (LevelArmExpiry(PlacedAt)).
type LevelArm struct {
	ArmID      string  `json:"arm_id"`
	Side       Side    `json:"side"`
	LevelPrice float64 `json:"level_price"`
	PlacedAt   int64   `json:"placed_at"` // the placement candle's CloseTime
	// ExecTFMin (C52 W3, A4b) is the exec frame (1/2/5) the touch was read on.
	// The close-through cancel reads the last CLOSED candle of THAT frame - the
	// course rule is "áp dụng trên mọi khung thời gian" [Ngày 2.3 part1.srt
	// @21:24]. 0/1 = the 1m bar (byte-identical to the pre-W3 read; also what a
	// state restored from before this field carries).
	ExecTFMin int `json:"exec_tf_min,omitempty"`
}

// ClearLevelArm (D2-44, item 11) drops the LevelArms entry for one level order
// by its ArmID. The TRADER calls it when it cancels or expires a "lvl-" arm on
// its own side (the N12 expiry sweep, the injector cancel) so the evaluator
// stops believing the order still rests and re-emits the level on the next
// valid touch. Idempotent — a level already cleared by levelArmCancels (the
// close-through sweep) is a no-op, and an unknown ArmID is a no-op.
func (e *Evaluator) ClearLevelArm(armID string) {
	if e.State.LevelArms == nil {
		return
	}
	for key, arm := range e.State.LevelArms {
		if arm.ArmID == armID {
			delete(e.State.LevelArms, key)
			return
		}
	}
}

// levelArmCancels is the B6 cancel sweep [D2.3 p1 @18:01–19:12, recovered
// @23:48]: a resting level order is cancelled when a LATER closed candle
// CLOSES THROUGH the level ("Minh cancel"), or at its own 15:00 CT expiry —
// LevelArmExpiry(PlacedAt). The placement candle itself never cancels.
// conflictArmCancels cancels every live mentor arm BY ArmID when the D4.2-03
// 15m/5m conflict fires. The old one-shot veto emitted a CancelArm with NO
// ArmID and the trader refused it as "unknown arm" — a resting arm survived
// the conflict and could fill against it.
func (e *Evaluator) conflictArmCancels() []Intent {
	var out []Intent
	for id := range e.State.ISBArms {
		out = append(out, Intent{Action: CancelArm, ArmID: id,
			Reason: "15m/5m ISB conflict — cancel all arms [D4.2 p1 @ 13:59–14:53]"})
		delete(e.State.ISBArms, id)
	}
	for key, arm := range e.State.LevelArms {
		out = append(out, Intent{Action: CancelArm, ArmID: arm.ArmID,
			Reason: "15m/5m ISB conflict — cancel all arms [D4.2 p1 @ 13:59–14:53]"})
		delete(e.State.LevelArms, key)
	}
	return out
}

func (e *Evaluator) levelArmCancels(bars []market.Kline, cur market.Kline, now int64) []Intent {
	if len(e.State.LevelArms) == 0 {
		return nil
	}
	var out []Intent
	for key, arm := range e.State.LevelArms {
		// The rolling pullback order is NOT a level order: its "level" is the
		// old extreme far above/below the entry, so the close-through sweep
		// must never cancel it (price is always on the pullback side of the
		// old extreme). The rolling path owns its own invalidation and the
		// trader's reconcile clears it on fill.
		if key == rollingLevelKey {
			continue
		}
		through := false
		// C52 W3 (A4b): the close-through is read on the frame the order was
		// placed on - "áp dụng trên mọi khung thời gian" [Ngày 2.3 part1.srt
		// @21:24] - i.e. the last CLOSED candle of arm.ExecTFMin, never a 1m
		// wick-close inside a forming 2m/5m candle that would still close back.
		// A 1m arm (ExecTFMin 0/1, or the exec read fell back to 1m) keeps the
		// last 1m bar, byte-identical to the pre-W3 read.
		chk := cur
		if arm.ExecTFMin > 1 {
			if cb := e.closedBucketsTFMemo(bars, arm.ExecTFMin, now); len(cb) > 0 {
				chk = cb[len(cb)-1]
			} else {
				chk = market.Kline{} // no closed candle of that frame yet: nothing can close through
			}
		}
		if chk.CloseTime > arm.PlacedAt {
			through = arm.Side == SideLong && chk.Close < arm.LevelPrice ||
				arm.Side == SideShort && chk.Close > arm.LevelPrice
		}
		// FIX-LEVEL-WINDOW (owner order 2026-10-13 ~21:2x CT): a level order
		// RESTS until ITS OWN 15:00 CT expiry — LevelArmExpiry(arm.PlacedAt),
		// the ONE definition the trader's injector (mentorIntentExpiry) and the
		// leg budget's pend simulation already read. The old wall-clock read
		// (ctOf(now) ≥ 15:00) was TRUE every minute 15:00–23:59, so an evening
		// level arm was registered and cancelled in the SAME Tick. An arm placed
		// at 10:00 CT still cancels at 15:00 the same day; one placed at 16:10
		// now rests until 15:00 the NEXT day (or an earlier close-through).
		expired := now >= LevelArmExpiry(arm.PlacedAt)
		if through || expired {
			out = append(out, Intent{Action: CancelArm, ArmID: arm.ArmID,
				Reason: "level order rest cancelled — a later candle closed through the level [D2.3 p1 @23:16–24:03], or the order's own 15:00 CT expiry passed [derived: LevelArmExpiry — no course timestamp states a 15:00 expiry; level_lifetime.go flags it as an inference]"})
			delete(e.State.LevelArms, key)
			// C52 W3 (A4a): "cancel, và chẳng làm gì khác hết" [Ngày 2.3
			// part1.recovered.txt @23:48-24:02] - a close-through is a cancel and
			// NOTHING else. The through-candle still "touches" a support from above
			// (the touch stays TouchReject with the same RefBar), so without this
			// the PHL loop re-placed the same stop on the very next tick. Spend the
			// touch reference exactly as DropArm does for a trader refusal: a NEW
			// reference candle (new visit) may trade again. Only the close-through
			// spends it - the 15:00 expiry is the owner's window, not a level death.
			if through {
				if tr, ok := e.State.Touches[key]; ok && tr.RefBar.CloseTime != 0 {
					if e.State.RefusedLevelTouch == nil {
						e.State.RefusedLevelTouch = map[string]int64{}
					}
					e.State.RefusedLevelTouch[key] = tr.RefBar.CloseTime
				}
			}
		}
	}
	return out
}

// refuse records a B-rules refusal: every drop carries a named reason and
// a counter (the replay's funnel-stage parity — CTO 13:51:31Z).
func (e *Evaluator) refuse(reason string) {
	if e.State.Refusals == nil {
		e.State.Refusals = map[string]int{}
	}
	e.State.Refusals[reason]++
}

// count records a B-rules NON-refusal event (m296): an event that neither drops
// nor refuses an intent but must still be diffable against the replay funnel.
func (e *Evaluator) count(reason string) {
	if e.State.Counters == nil {
		e.State.Counters = map[string]int{}
	}
	e.State.Counters[reason]++
}

// boxBanFilter drops entry intents whose entry price — or whose reference
// candle close — sits INSIDE a box: "NEVER trade inside the box, neither the
// candle nor your entry point" [D3.2 p1 @ 06:59]. Leaving the box is NOT itself
// an entry — after a body exits you still wait for a normal setup, which is then
// tradable [D3.2 p1 @08:38–08:54; D3.2 p2 @07:38–07:50] (by construction: a
// break is never an entry, only setups are).
func boxBanFilter(out []Intent, boxes []Box, cur market.Kline) (kept []Intent, refusals []string) {
	if len(boxes) == 0 {
		return out, nil
	}
	kept = out[:0:0]
	for _, in := range out {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) &&
			(InsideAnyBox(boxes, cur.Close) || InsideAnyBox(boxes, in.Price)) {
			refusals = append(refusals, "inside_any_box")
			continue
		}
		kept = append(kept, in)
	}
	return kept, refusals
}

// phlISBPreferenceFilter — D3.3 p1 @08:12–08:56: when the SAME closed bar at a
// box produces BOTH a box-edge reject (Setup "BOX" — the mentor's "potential
// high/low") and an inside bar (ISB) on the SAME side, the mentor goes to the
// inside bar ("Nên về xuống InstaBar… em lại an toàn với InstaBar hơn") because
// its stop is smaller. Keep the ISB and refuse the box reject — but ONLY when
// the ISB's stop distance is <= the box reject's (his reason is the smaller
// stop); otherwise both stay and the case is noted (never a silent drop).
// Opposite sides: no precedence — the existing gates decide.
func phlISBPreferenceFilter(out []Intent) (kept []Intent, refusals []string, notes []string) {
	type key struct {
		side Side
		ref  int64
	}
	isbStop := make(map[key]float64)
	for _, in := range out {
		if in.Setup == "ISB" && (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) {
			isbStop[key{in.Side, in.RefBarMs}] = abs(in.Price - in.Stop)
		}
	}
	if len(isbStop) == 0 {
		return out, nil, nil
	}
	kept = out[:0:0]
	for _, in := range out {
		if in.Setup != "BOX" || (in.Action != PlaceStopEntry && in.Action != PlaceStopLimitEntry) {
			kept = append(kept, in)
			continue
		}
		isb, ok := isbStop[key{in.Side, in.RefBarMs}]
		if !ok {
			kept = append(kept, in)
			continue
		}
		boxStop := abs(in.Price - in.Stop)
		if isb <= boxStop {
			refusals = append(refusals, "phl_isb_preferred_at_box")
			continue
		}
		notes = append(notes, fmt.Sprintf("phl_isb_keep_both: ISB stop %.1f > box stop %.1f at the box — keeping both [D3.3 p1 @08:12–08:56]", isb, boxStop))
		kept = append(kept, in)
	}
	return kept, refusals, notes
}

// triggerBoxZoneFilter drops non-swing place entries refused by the B1
// trigger zone (between an FTGL and the buy line, mirror FTGH + sell line).
func triggerBoxZoneFilter(out []Intent, t TriggerLine, boxes []Box) (kept []Intent, refusals []string) {
	if t.Dir == "" || len(boxes) == 0 {
		return out, nil
	}
	kept = out[:0:0]
	for _, in := range out {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && !strings.HasPrefix(in.Reason, "swing") {
			if ok, _ := triggerBoxZoneVerdict(t, boxes, in.Price); !ok {
				if t.Dir == SideLong {
					refusals = append(refusals, "trigger_ftgl_buy_zone")
				} else {
					refusals = append(refusals, "trigger_ftgh_sell_zone")
				}
				continue
			}
		}
		kept = append(kept, in)
	}
	return kept, refusals
}

// midRangeBoxed reports whether price sits between an FTGL floor box below
// and an FTGH ceiling box above — the mid-range ban with NO width threshold
// (CTO 1791003862333): "between two boxes: NO PHL, NO PLH, only the ISB"
// [D3.2 p2 @08:34–08:55; D2.2 p3 @14:03–16:03 "chỉ được đánh insaba"].
func midRangeBoxed(boxes []Box, price float64) bool {
	var floor, ceil bool
	for _, b := range boxes {
		if b.Kind == FTGL && b.Top < price {
			floor = true
		}
		if b.Kind == FTGH && b.Bottom > price {
			ceil = true
		}
	}
	return floor && ceil
}

// inNarrowRange (item 21, D4.1-06) reports whether price sits between two KEY
// levels closer than minPts (the ping-pong minimum, 50) — a too-small range
// ("range quá nhỏ" [D4.2 p2 @06:25–06:43]) is still a range for the ISB size
// cut: the market is running inside a range too small to touch.
func inNarrowRange(price float64, levels []Level, minPts float64) bool {
	if minPts <= 0 {
		return false
	}
	floor, ceil := 0.0, 0.0
	for _, l := range levels {
		if l.Kind != KindKeyLevel {
			continue
		}
		if l.Price < price && l.Price > floor {
			floor = l.Price
		}
		if l.Price > price && (ceil == 0 || l.Price < ceil) {
			ceil = l.Price
		}
	}
	if floor <= 0 || ceil <= 0 {
		return false
	}
	return ceil-floor < minPts
}

// isbInRange (item 21, D4.1-06) reports whether an ISB's candle sits "in range"
// — the compulsory size cut [D4.1 p1 written rule 3 "Khi trade isb in-range bắt
// buộc giảm size"]. The range is WIDER than midRangeBoxed:
//   - between an FTGL below and an FTGH above (midRangeBoxed, unchanged);
//   - inside the standing 5m ISB rest box (State.ISBBox);
//   - between two key levels closer than the ping-pong minimum;
//   - (once built) inside a 15m ISB range (D4.1-25) — add that condition here.
func isbInRange(price float64, levels []Level, boxes []Box, isbBox *ISBBox, pingPongMin float64) bool {
	if midRangeBoxed(boxes, price) {
		return true
	}
	if isbBox != nil && price >= isbBox.Low && price <= isbBox.High {
		return true
	}
	if inNarrowRange(price, levels, pingPongMin) {
		return true
	}
	// A 15m ISB range (D4.1-25) is not built yet; when it lands, price inside
	// it must also set the in-range flag.
	return false
}

// isbArmActive reports whether an ISB arm for the SAME side is still live
// (CTO parity ruling 1791008332386 #1: ONE active ISB arm per side — the
// reference is the FIRST ISB; a new arm forms only after the previous one is
// filled, cancelled or expired).
func isbArmActive(arms map[string]ISBArm, side Side) bool {
	for _, arm := range arms {
		if arm.Inside >= 0 && arm.Side == side {
			return true
		}
	}
	return false
}

// DropArm removes the evaluator's arm for an ArmID whose placement the trader
// refused as a DEAD setup (never-add, done-after-win, stop-after-loss, window,
// news, daily-loss, expiry-missing, bad side, or the stale-data authoring
// refusal). Dropping the arm lets the next same-side setup proceed instead of
// being suppressed by isb_arm_active / a phantom swing Pending, and stops the
// follow-up ExtendArm/CancelArm/MoveStopBE intents for an order that was never
// authored. It touches nothing else — a FILLED swing position (Swing.Pos) is
// never dropped here.
func (e *Evaluator) DropArm(armID string) {
	if e == nil || armID == "" {
		return
	}
	// m425a S2: a trader-side refusal of a LEVEL/PHL intent ends that touch for
	// that level — remember the reference bar so the (still TouchReject) level
	// is not re-emitted every minute. The rolling pullback order is exempt: it
	// re-arms on a NEW reference candle by design.
	for key, arm := range e.State.LevelArms {
		if arm.ArmID != armID || key == rollingLevelKey {
			continue
		}
		if tr, ok := e.State.Touches[key]; ok && tr.RefBar.CloseTime != 0 {
			if e.State.RefusedLevelTouch == nil {
				e.State.RefusedLevelTouch = map[string]int64{}
			}
			e.State.RefusedLevelTouch[key] = tr.RefBar.CloseTime
		}
	}
	e.unregisterArm(armID)
}

// SwingBEApplied is the executor's receipt that the swing's break-even stop is
// on the broker (release #54, N3). Until it arrives the evaluator keeps
// re-emitting the BE once per closed 5m bar. A receipt for an arm that is not
// the open swing is ignored; the return says whether it was recorded.
func (e *Evaluator) SwingBEApplied(armID string) bool {
	if e == nil {
		return false
	}
	return e.State.Swing.BEApplied(armID)
}

// SwingEnded is the receipt that the swing's position is gone — its stop, its
// target or its hold-close filled, or the account holds no position carrying
// the arm's own signal (release #54, N2). It clears Swing.Pos so a finished
// swing can never send a BE or a market close against a later trade. A
// receipt for an arm that is not the open swing is ignored; the return says
// whether Pos was cleared.
func (e *Evaluator) SwingEnded(armID string) bool {
	if e == nil {
		return false
	}
	return e.State.Swing.Ended(armID)
}

// unregisterArm removes EVERY evaluator registration of an entry ArmID that
// will never be authored (m425a S1): ISBArms, the swing Pending, the LevelArms
// entry (incl. the rolling PHL order and its RollingPHL state) and the EMA34
// pending. A phantom arm blocks the next same-side setup (isb_arm_active /
// phl_level_arm_resting) and emits ExtendArm/CancelArm for an order that was
// never authored. A FILLED swing position is never touched. Idempotent.
func (e *Evaluator) unregisterArm(armID string) {
	if e == nil || armID == "" {
		return
	}
	delete(e.State.ISBArms, armID)
	if e.State.Swing.Pending != nil && e.State.Swing.Pending.ArmID == armID {
		e.State.Swing.Pending = nil
	}
	for key, arm := range e.State.LevelArms {
		if arm.ArmID == armID {
			delete(e.State.LevelArms, key)
		}
	}
	if e.State.RollingPHL.ArmID == armID {
		e.State.RollingPHL = RollingPHL{}
	}
	if e.State.EmaPendingArmID == armID {
		e.State.EmaPendingSide = ""
		e.State.EmaPendingFilled = false
		e.State.EmaPendingArmID = ""
	}
}

// heldArmIDs lists every entry ArmID the evaluator currently has registered:
// ISBArms, the swing Pending, the LevelArms (incl. the rolling PHL order) and
// the EMA34 pending. It is the "before" snapshot of the m431 exit pass.
func (e *Evaluator) heldArmIDs() map[string]bool {
	ids := make(map[string]bool, len(e.State.ISBArms)+len(e.State.LevelArms)+3)
	for id := range e.State.ISBArms {
		ids[id] = true
	}
	if p := e.State.Swing.Pending; p != nil && p.ArmID != "" {
		ids[p.ArmID] = true
	}
	for _, arm := range e.State.LevelArms {
		if arm.ArmID != "" {
			ids[arm.ArmID] = true
		}
	}
	if e.State.RollingPHL.ArmID != "" {
		ids[e.State.RollingPHL.ArmID] = true
	}
	if e.State.EmaPendingArmID != "" {
		ids[e.State.EmaPendingArmID] = true
	}
	return ids
}

// unregisterDroppedArms is the single exit pass (m425a S1, widened by m431):
// every arm registered since the `held` snapshot whose entry is not among the
// intents leaving Tick was dropped by some filter after it registered, so it is
// un-registered — it would otherwise block the next same-side setup and later
// emit ExtendArm/CancelArm for an order that was never authored. An arm that
// was already held when the Tick began is left alone (its lifecycle is the
// cancel/extend path's).
func (e *Evaluator) unregisterDroppedArms(held map[string]bool, out []Intent) {
	survived := entryArmIDs(out)
	for id := range e.heldArmIDs() {
		if !held[id] && !survived[id] {
			e.unregisterArm(id)
		}
	}
}

// entryArmIDs lists the ArmIDs of the entry intents (the placements that
// registered an arm) in out.
func entryArmIDs(out []Intent) map[string]bool {
	ids := map[string]bool{}
	for _, in := range out {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.ArmID != "" {
			ids[in.ArmID] = true
		}
	}
	return ids
}

// isbFlags returns the ISB size flags for the injector (rule 2: at an old
// high/low; rule 3: in a range), joined with "|" when both apply.
func isbFlags(cur market.Kline, levels []Level, boxes []Box, oldExtremeBandPts float64) string {
	var flags []string
	if touchesOldExtreme(cur, levels, oldExtremeBandPts) {
		flags = append(flags, FlagISBAtOldExtreme)
	}
	if midRangeBoxed(boxes, cur.Close) {
		flags = append(flags, FlagISBInRange)
	}
	return strings.Join(flags, "|")
}

// isbFlagsFor is the production flag resolver: isbFlags plus the WIDENED rule-3
// "in range" (item 21) — the standing 5m ISB box and the narrow (< ping-pong
// minimum) range also set isb_in_range, so an ISB inside them gets the

// compulsory size cut [D4.1 p1 @04:47–05:04 written rule 3].
// joinFlag ORs a flag onto a "|"-joined flag string (idempotent).
func joinFlag(flags, want string) string {
	if flags == "" {
		return want
	}
	if HasFlag(flags, want) {
		return flags
	}
	return flags + "|" + want
}

// pullbackWarning5m reports the X11 pullback warning as the ENTRY SIDE it
// warns ("" = no warning): in a 5m UPTREND, a closed 5m candle that breaks the
// previous candle's LOW means "market chuẩn bị pullback … hạn chế vô lệnh,
// giảm size lại cũng được" — the warning is for a LONG, i.e. trading WITH the
// 5m trend; the mirror ("cũng ngược lại": 5m downtrend, the next candle breaks
// the previous candle's HIGH) warns a SHORT [Go over lớp học part1.srt
// @00:13:21–00:15:19, quote @00:14:50–00:15:02]. A SHORT taken in the uptrend's
// own pullback is not warned (C52-W10 B20: it was cut before, the warning was
// side-blind). The 5m trend is the swing STRUCTURE read (structureTrend5m,
// higher high + higher low / lower high + lower low), not "the previous candle
// is green"; no clear structure means no warning.
func pullbackWarning5m(b5 []market.Kline) Side {
	if len(b5) < 2 {
		return ""
	}
	prev, cur := b5[len(b5)-2], b5[len(b5)-1]
	switch structureTrend5m(b5) {
	case SideLong:
		if cur.Low < prev.Low {
			return SideLong
		}
	case SideShort:
		if cur.High > prev.High {
			return SideShort
		}
	}
	return ""
}

// isbInRangeGapPts is the "in range" key-level gap for the ISB size cut: the
// W11 B27 knob when set, else the ping-pong minimum it was borrowed from
// (X4/D4.2), so a stored ping_pong_min_gap_pts keeps reading the same.
func (c Config) isbInRangeGapPts() float64 {
	if c.ISBInRangeGapPts > 0 {
		return c.ISBInRangeGapPts
	}
	return c.PingPongMinGapPts
}

func (e *Evaluator) isbFlagsFor(cur market.Kline, side Side, levels []Level, boxes []Box) string {
	flags := isbFlags(cur, levels, boxes, e.Cfg.ISBOldExtremePts)
	if e.pullbackWarn5m != "" && side == e.pullbackWarn5m {
		flags = joinFlag(flags, FlagPullbackWarn)
	}
	if HasFlag(flags, FlagISBInRange) {
		return flags
	}
	if isbInRange(cur.Close, levels, boxes, e.State.ISBBox, e.Cfg.isbInRangeGapPts()) {
		if flags != "" {
			flags += "|"
		}
		flags += FlagISBInRange
	}
	return flags
}

// touchesOldExtreme reports whether the candle's range reaches an old high/low
// within ±bandPts (the "isb_at_old_extreme" size flag, written rule 2 D4.1 p1).
// The course gives no distance for "ngay đỉnh/đáy cũ" [Ngày 4.1 Go over ngày 3
// part1 @00:04:31–00:05:19]; the band is ours (Config.ISBOldExtremePts,
// default 2 — release #52 W11 B27).
func touchesOldExtreme(cur market.Kline, levels []Level, bandPts float64) bool {
	for _, l := range levels {
		if l.Kind != KindOldExtreme {
			continue
		}
		if cur.High >= l.Price-bandPts && cur.Low <= l.Price+bandPts {
			return true
		}
	}
	return false
}

// isBoxEdge reports whether the level is a box edge (BOX RULING part 2:
// the invalid-level rule does not apply to box edges).
func isBoxEdge(l Level) bool {
	return l.Kind == KindFTGHEdge || l.Kind == KindFTGLEdge
}

// closedBuckets returns the 5m buckets with the still-forming one dropped
// (B4): a bucket whose close time has not been reached by `now` is forming.
//
// CONTRACT (FU-3): `now` must be an instant STRICTLY AFTER the newest closed
// 1m bar's close — in the Tick path that is BarCloseInstant(last) =
// last.CloseTime + 1 (see eval.go BarCloseInstant); at seed it is the wall
// clock (also after the newest closed bar). A bucket whose recorded close
// equals the newest closed bar's close is KEPT (its CloseTime < now); only a
// bucket whose close is still in the FUTURE is dropped. A caller passing
// last.CloseTime VERBATIM (no +1) would over-drop the just-completed bucket.
func closedBuckets(bars []market.Kline, now int64, cfg Config) []market.Kline {
	b5 := barsTF(bars, 5)
	if len(b5) > 0 && b5[len(b5)-1].CloseTime >= now {
		return b5[:len(b5)-1]
	}
	return b5
}

// tfMemoEntry is one barsTF memo slot: the input slice identity (length +
// first/last OpenTime) plus the built buckets. The identity guard makes a hit
// byte-identical to a fresh barsTF — a DIFFERENT slice with the same tf (e.g.
// the HTF feed vs the raw 1m tape) never reads another slice's buckets.
type tfMemoEntry struct {
	n      int
	first  int64
	last   int64
	bucket []market.Kline
}

// barsTFMemo returns barsTF(bars, tfMin), built at most once per Tick for a
// given (tf, slice identity). tfMin <= 1 and empty slices fall through to
// barsTF's own early return (no memo). Cleared at the start of each Tick.
func (e *Evaluator) barsTFMemo(bars []market.Kline, tfMin int) []market.Kline {
	if tfMin <= 1 || len(bars) == 0 {
		return bars
	}
	first, last := bars[0].OpenTime, bars[len(bars)-1].OpenTime
	if en, ok := e.tfMemo[tfMin]; ok && en.n == len(bars) && en.first == first && en.last == last {
		return en.bucket
	}
	b := barsTF(bars, tfMin)
	if e.tfMemo == nil {
		e.tfMemo = make(map[int]tfMemoEntry, 4)
	}
	e.tfMemo[tfMin] = tfMemoEntry{n: len(bars), first: first, last: last, bucket: b}
	return b
}

// closedBucketsMemo is closedBuckets reading the per-Tick memo.
func (e *Evaluator) closedBucketsMemo(bars []market.Kline, now int64) []market.Kline {
	b5 := e.barsTFMemo(bars, 5)
	if len(b5) > 0 && b5[len(b5)-1].CloseTime >= now {
		return b5[:len(b5)-1]
	}
	return b5
}

// eveningSlow reports whether the evening/overnight read should drop to the 5m:
// the night is "slow" when the 1m high-low range since the start hour (default
// 15:00 CT, the RTH close) is under EveningSlowPts (default 100). X11 names
// 100–200 pts as a STRONG night (2m stays fine); the 100-pt cutoff, the 15:00
// start and the cumulative window are ours [derived: X11 @17:40–18:26; owner
// Q6 2026-10-25 fixed no number] — release #52 W11 B10 exposes all three as
// knobs (evening_slow_pts / evening_slow_from_hour / evening_slow_window_min),
// defaults unchanged.
func eveningSlow(now int64, bars []market.Kline, cfg Config) bool {
	if !inEveningWindow(now) {
		return false
	}
	pts := cfg.EveningSlowPts
	if pts <= 0 {
		pts = 100
	}
	hour := cfg.EveningSlowFromHour
	if hour < 15 || hour > 17 {
		hour = 15
	}
	t := time.UnixMilli(now).In(ctime())
	since := time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, ctime()).UnixMilli()
	if now < since {
		// Overnight 00:00–02:59 CT (or before the start hour): the evening
		// belongs to yesterday's start hour.
		prev := t.AddDate(0, 0, -1)
		since = time.Date(prev.Year(), prev.Month(), prev.Day(), hour, 0, 0, 0, ctime()).UnixMilli()
	}
	if w := cfg.EveningSlowWindowMin; w > 0 {
		if from := now - int64(w)*60_000; from > since {
			since = from
		}
	}
	var hi, lo float64
	have := false
	for _, b := range bars {
		if b.OpenTime < since {
			continue
		}
		if !have {
			hi, lo, have = b.High, b.Low, true
			continue
		}
		if b.High > hi {
			hi = b.High
		}
		if b.Low < lo {
			lo = b.Low
		}
	}
	if !have {
		return false // no evening bars yet — stay on the 2m
	}
	return hi-lo < pts
}

// closedBucketsTFMemo is closedBucketsTF reading the per-Tick memo.
func (e *Evaluator) closedBucketsTFMemo(bars []market.Kline, tfMin int, now int64) []market.Kline {
	agg := e.barsTFMemo(bars, tfMin)
	if len(agg) > 0 && agg[len(agg)-1].CloseTime >= now {
		agg = agg[:len(agg)-1]
	}
	return agg
}

// reverseISBEMA9 is the EMA 9 the reverse ISB (R7) tests the exec candle
// against, on the frame the pair is read on (C52-W4, A5 part 1) [Ngày 5.4 ISB
// đảo chiều part1.srt @00:08:15–00:08:40: "anh trade khung nào thì anh bật khung
// đó, khung M1 cũng được, khung M2 cũng được"].
//
//   - 1m exec frame: the 1m EMA 9, unchanged.
//   - 2m exec frame: the 2m EMA 9 - the SAME line the bot draws and the EMA 9
//     level carries (State.EMA9x2) when that incremental line has folded
//     exactly through execCur (Seed2mWatermark == execCur.CloseTime proves it is
//     in step with the candle under test); otherwise the EMA over the closed 2m
//     buckets (EMAOnTF's arithmetic).
//   - 5m evening exec frame: UNCHANGED, the 1m EMA 9. There is no course basis
//     for the evening frame (owner Q9 unanswered), so the 5m path is left alone.
func (e *Evaluator) reverseISBEMA9(bars []market.Kline, execCur market.Kline, execTFMin int, now int64) float64 {
	if execTFMin == 2 {
		if e.State.EMA2mWarmed && e.State.EMA9x2 > 0 && e.State.Seed2mWatermark == execCur.CloseTime {
			return e.State.EMA9x2
		}
		if b2 := e.closedBucketsTFMemo(bars, 2, now); len(b2) > 0 {
			return emaValue(b2, e.Cfg.EMAPeriod9)
		}
	}
	return emaValue(bars, e.Cfg.EMAPeriod9)
}

// nextLevelBeyond is the §6 target ladder [D3.3 p1 @ 05:07]: the NEAREST level
// beyond price in the trade direction ("the first thing standing in your way").
// 0 = no level beyond.
// targetFloorOK is the D1.2 floor: "the target is never smaller than the
// stop" [D1.2 p1 @ 07:48] — |target−entry| must be >= |entry−stop|. It holds
// for EVERY setup's intent (CTO E-2 2026-10-03T15:12Z): the room knob may
// tighten a target but never loosens this floor.
func targetFloorOK(entry, stop, target float64) bool {
	return abs(target-entry) >= abs(entry-stop)
}

// roomRefusal is the D5.3 room rule [@09:16–10:17] shared by every setup that
// has a target: the room to the first available level (|target−entry|) must be
// at least roomMultiple × the FIRST take-profit distance. The first take-profit
// is leg 1 — the 1:1 partial [D2.2 p3 @12:13; D1.2 p1 @07:41–08:45] — whose
// distance is Leg1RiskMultiple(confluence) × the risk: 1R normally, 2R for the
// confluence tier [D3.4 p3 @07:52–08:07]. So the room is ≥ 2R normally and ≥ 4R
// for a confluence entry. roomMultiple <= 0 disables the check. One counter
// "room" for every path (ISB, reverse ISB, box, PHL/PLH; the swing keeps R68).
func roomRefusal(price, stop, target float64, confluence bool, roomMultiple float64) (refuse bool, why string) {
	if roomMultiple <= 0 {
		return false, ""
	}
	risk := abs(stop - price)
	reward := abs(target - price)
	if risk <= 0 || reward <= 0 {
		return true, "degenerate stop/target geometry"
	}
	leg1 := risk * Leg1RiskMultiple(confluence)
	if reward < roomMultiple*leg1 {
		return true, fmt.Sprintf("room: reward %.2f pts < %.2f pts (%.2fx the leg-1 %.0fR take-profit) [D5.3 p1 @09:16–10:17 · D2.2 p3 @12:13 · D1.2 p1 @07:41–08:45]", reward, roomMultiple*leg1, roomMultiple, Leg1RiskMultiple(confluence))
	}
	return false, ""
}

// spentDayRoomRefusal — DS-104 P2-3 / MENTOR-NY item 1 (course wins, R5 ON):
// on a SPENT day the room to the LEVEL must also be at least roomMultiple × the
// CAPPED take-profit — the course's own worked figure: "Bây giờ dù target của
// em là 15 điểm … từ điểm entry xuống target nó phải từ 30 điểm đổ lên"
// [D5.3 p1 @09:32–09:50], target 15 → room 30. roomRefusal's 2× leg-1 (2R)
// still applies beside it; the two agree only at a 15-pt stop, and for a
// smaller stop 2R alone admitted a level the course rejects. capped is the
// take-profit after CapTargetForDay (the level itself when it sits inside the
// cap). Any other verdict, or roomMultiple <= 0, never refuses here.
func spentDayRoomRefusal(price, level, capped float64, v DayVerdict, roomMultiple float64) (refuse bool, why string) {
	if v != DaySpent || roomMultiple <= 0 {
		return false, ""
	}
	tp := abs(capped - price)
	room := abs(level - price)
	if tp <= 0 || room < roomMultiple*tp {
		return true, fmt.Sprintf("room: spent day — room %.2f pts to the level < %.2f pts (%.2fx the %.2f-pt capped take-profit) [D5.3 p1 @09:32–09:50 \"dù target của em là 15 điểm … 30 điểm đổ lên\"]", room, roomMultiple*tp, roomMultiple, tp)
	}
	return false, ""
}

// triggerBoxZoneVerdict is the B1 no-trade zone (10-03 ruling, D3.4 p1
// @16:38–17:19): between an FTGL below and the BUY trigger line above there
// is NO trade ("khỏi đánh, đợi nó thoát ra khỏi 2 cái") — mirror: an FTGH
// above and the SELL line below. Price must escape BOTH. This replaces the
// misread "between two opposing trigger lines" band (there is only ONE
// line, moved on a reversal).
func triggerBoxZoneVerdict(t TriggerLine, boxes []Box, price float64) (ok bool, reason string) {
	if t.Dir == "" {
		return true, ""
	}
	switch t.Dir {
	case SideLong:
		for _, b := range boxes {
			if b.Kind == FTGL && b.Top < t.Price && price >= b.Top && price <= t.Price {
				return false, "between the FTGL and the buy trigger line — no trade, wait to escape both [D3.4 p1 @ 16:38–17:19]"
			}
		}
	case SideShort:
		for _, b := range boxes {
			if b.Kind == FTGH && b.Bottom > t.Price && price <= b.Bottom && price >= t.Price {
				return false, "between the sell trigger line and the FTGH — no trade, wait to escape both [D3.4 p1 @ 16:38–17:19]"
			}
		}
	}
	return true, ""
}

// suppressSamePairCounterISB (R85, RELEASE #4 item 26) drops the normal ISB
// that arms the counter-trend side on the SAME candle pair when the reverse
// ISB fired — one candle must never carry two opposite stop orders. It also
// returns the dropped arms' ArmIDs so the caller can unregister them from the
// one-arm-per-side map (a dangling arm would stack an entry that never
// materialized).
func suppressSamePairCounterISB(out []Intent, in Intent, refBarMs int64) (kept []Intent, suppressed []string) {
	kept = out[:0]
	for _, o := range out {
		if o.Setup == "ISB" && o.RefBarMs == refBarMs && o.Side != in.Side {
			if o.ArmID != "" {
				suppressed = append(suppressed, o.ArmID)
			}
			continue
		}
		kept = append(kept, o)
	}
	return kept, suppressed
}

// trendlineTargets mirrors every trendline location as a target-only level
// (CE-11/B12). The trendline LOCATION itself is never a target.
func trendlineTargets(levels []Level) []Level {
	var out []Level
	for _, l := range levels {
		if l.Kind == KindTrendline {
			out = append(out, Level{Key: "target:" + l.Key, Kind: KindTrendlineTarget, Price: l.Price, AtTime: l.AtTime})
		}
	}
	return out
}

// phlLocationBan is the PHL/PLH LOCATION-ban ladder, extracted from the level
// touch loop (m436) so the rolling PHL/PLH (rolling_phl.go) calls the SAME
// functions instead of a copy. It returns the refusal name ("" = the location
// is permitted) and whether the caller counts it once per visit (true) or per
// evaluation (false) — exactly the counting the touch loop always used. The
// order is the touch loop's: X4 keypair candle, mid-range, mid-range between
// boxes, then the standing 5m / 15m / 30m ISB box.
//
//   - keypair_candle_too_big — a candle as big as the rank of the two levels:
//     sit out ("một cái nến nó bằng cái range của 2 cái level rồi thì ngồi im"
//     [X4 @06:31]); MENTOR-NY item 2: "the candle" is the current closed 1m
//     candle; the pair is the anchor level and the next KEY level beyond it.
//   - phl_mid_range — "Ở giữa rank không được đánh potential high low, chỉ
//     được đánh insaba" [D2.2 p3 @14:03–16:03; D3.2 p2 @08:34].
//   - phl_mid_range_boxed — between an FTGL below and an FTGH above no PHL/PLH
//     [D3.2 p2 @08:34–08:55] (trendline exemption: CE-11/B13, knob).
//   - phl_5m_box_ban / phl_15m_box_ban / phl_30m_box_ban — inside a standing
//     ISB box "No PHL, no PLH, no opposite ISB" [D3.4 p2 @07:27–10:35; D3.2 p2
//     @08:51; D3.4 p3 @01:44]; the 15m/30m boxes: D4.1-25 / D4.2-07.
//
// lvl is the level the setup is anchored on (the touched level on the level
// path; the old extreme the leg targets on the rolling path).
func (e *Evaluator) phlLocationBan(lvl Level, side Side, price float64, cur market.Kline, levels []Level, boxes []Box) (name string, oncePerVisit bool) {
	if e.Cfg.PingPongCandleMaxPts > 0 {
		var next float64
		for _, l2 := range levels {
			// The pair is the anchor level and the next KEY level he drew — EMA
			// lines, trigger retests, box edges and bare tape swings are not part
			// of the rank between two levels (X4). The scan is strict (beyond the
			// level, excluding the level itself).
			if l2.Key == lvl.Key || l2.Kind != KindKeyLevel {
				continue
			}
			if side == SideLong && l2.Price > lvl.Price && (next == 0 || l2.Price < next) {
				next = l2.Price
			} else if side == SideShort && l2.Price < lvl.Price && (next == 0 || l2.Price > next) {
				next = l2.Price
			}
		}
		if next != 0 && abs(next-lvl.Price) <= keypairCandleRatio(e.Cfg)*(cur.High-cur.Low) { // MENTOR-NY item 2 (current candle) x CX-10 ratio (default 1)
			return "keypair_candle_too_big", true
		}
	}
	if allowed, _ := SetupPermittedVerdict("PHL", levels, price, e.Cfg); !allowed {
		return "phl_mid_range", false
	}
	if midRangeBoxedRefuses(e.Cfg, lvl, boxes, price) {
		return "phl_mid_range_boxed", true
	}
	if e.State.ISBBox != nil && cur.Close > e.State.ISBBox.Low && cur.Close < e.State.ISBBox.High {
		return "phl_5m_box_ban", true
	}
	if e.State.ISBBox15m != nil && cur.Close > e.State.ISBBox15m.Low && cur.Close < e.State.ISBBox15m.High {
		return "phl_15m_box_ban", false
	}
	if e.State.ISBBox30m != nil && cur.Close > e.State.ISBBox30m.Low && cur.Close < e.State.ISBBox30m.High {
		return "phl_30m_box_ban", false
	}
	return "", false
}

// phlNearBoxRefuses is the Day-3 row 24 near-box refusal for a PHL/PLH (the
// "near_box" counter), shared by the level touch loop and the rolling path
// (m436): the nearest box edge in the trade direction must be at least
// NearBoxRoomMultiple x the reference candle's range away [D3.2 p1
// @21:53–23:08]; between two boxes is exempt (row 25). W11 B11: the PHL/PLH leg
// is our extension of the course's ISB-only example (NearBoxRoomPHL).
func phlNearBoxRefuses(cfg Config, boxes []Box, tref float64, side Side, refRange float64) bool {
	refuse, _ := nearBoxRefusal(boxes, tref, side, cfg.NearBoxRoomMultiple, refRange)
	return cfg.NearBoxRoomPHL && refuse
}

// midRangeBoxedRefuses is the level-path mid-range ban (CTO 1791003862333 —
// between an FTGL below and an FTGH above no PHL/PLH) with the CE-11/B13
// exemption: knob trendline_between_boxes ON lets a TRENDLINE reject through
// ("Đụng trendline, nên đóng dưới, đánh ngược lại xuống dưới… dưới cái box"
// [D3.2 p2 @10:56–11:20]). Default OFF = every level refused, as before.
func midRangeBoxedRefuses(cfg Config, lvl Level, boxes []Box, price float64) bool {
	if !midRangeBoxed(boxes, price) {
		return false
	}
	return !(cfg.TrendlineBetweenBoxes && lvl.Kind == KindTrendline)
}

func nextLevelBeyond(levels []Level, price float64, side Side) float64 {
	best := 0.0
	for _, l := range levels {
		if l.Kind == KindTrendline {
			continue // the LOCATION line is never a target; its KindTrendlineTarget mirror is (CE-11/B12, knob)
		}
		if !oldExtremeRoleFits(l, side) {
			continue // r22 P1-4: a long never targets an old LOW (mirror short)
		}
		switch side {
		case SideLong:
			if l.Price > price && (best == 0 || l.Price < best) {
				best = l.Price
			}
		case SideShort:
			if l.Price < price && (best == 0 || l.Price > best) {
				best = l.Price
			}
		}
	}
	return best
}

// nextLevelBeyondNonEMA is nextLevelBeyond that skips the EMA 9 / EMA 34 lines
// as the target — they are PARTIAL points along the way, not the trade target
// [D3.3 p1 @05:15–07:38]. Used by the PHL/PLH target pick (R3, release #30
// fold): an EMA must not by itself kill a PHL/PLH. The EMA is the fallback
// target only when no other level stands beyond.
func nextLevelBeyondNonEMA(levels []Level, price float64, side Side) float64 {
	best := 0.0
	bestEMA := 0.0
	for _, l := range levels {
		if l.Kind == KindTrendline {
			continue
		}
		if !oldExtremeRoleFits(l, side) {
			continue
		}
		onSide := side == SideLong && l.Price > price || side == SideShort && l.Price < price
		if !onSide {
			continue
		}
		if isEMAPartialKind(l.Kind) {
			if bestEMA == 0 || (side == SideLong && l.Price < bestEMA) || (side == SideShort && l.Price > bestEMA) {
				bestEMA = l.Price
			}
			continue
		}
		if best == 0 || (side == SideLong && l.Price < best) || (side == SideShort && l.Price > best) {
			best = l.Price
		}
	}
	if best != 0 {
		return best
	}
	return bestEMA
}

// runnerPartialLine (release #31, leg-3 bracket) is the runner's FIRST partial
// line: the nearest target line strictly BETWEEN the leg-1 exit (the 1:1 point,
// "bán bớt ở 1:1" [D2.2 p3 @12:13]) and the trade target, in the trade
// direction [D3.3 p1 @04:11–04:17 "những cái chỗ này là những cái chỗ anh chị
// bán bớt"]. 0 = no line between them → no leg-3 split (the runner rides to the
// trade target as one pair). The trade target itself is NOT a partial line: a
// line at or past it returns 0 (the runner's TP already owns the target).
func runnerPartialLine(levels []Level, entry, stop, target float64, side Side) float64 {
	if entry == 0 || stop == 0 || target == 0 {
		return 0
	}
	leg1TP := 2*entry - stop // the 1:1 reflection of the entry across the stop
	line := nextLevelBeyond(levels, leg1TP, side)
	if line == 0 {
		return 0
	}
	switch side {
	case SideLong:
		if line >= target {
			return 0 // the only line beyond leg 1 is the trade target itself
		}
		return line
	case SideShort:
		if line <= target {
			return 0
		}
		return line
	}
	return 0
}

// nextLevelBeyondRoom is nextLevelBeyond for the room-checked paths (ISB,
// reverse ISB, box). REL-5 audit #2: a TARGET-ONLY level (4h trigger, wick
// microscalp) closer than roomMultiple × risk is skipped and the search falls
// through to the next level beyond; a key level or box edge inside 2R still
// becomes the target (and the room rule then refuses it). The 4h line stays a
// valid target whenever it is ≥ 2R away [D4.4 p2 @05:04].
//
// Release #30 (D3.3 p1 @05:15–07:38): the EMA 9 / EMA 34 lines are PARTIAL
// points along the way, not the trade target — "Ta ghẹt tôi đầu tiên phải là
// về gần đỉnh cũ trước đó nè … Tiếp tục là email 34 nè … những cái chỗ này là
// những cái chỗ anh chị bán bớt". So an EMA must not by itself kill the trade:
// the nearest EMA is skipped as the target and the floor/room read the next
// non-EMA level (the nearest old high/low, key level, trendline target, HTF
// line). An EMA becomes the target only when NO other level stands beyond.
func nextLevelBeyondRoom(levels []Level, entry, stop float64, side Side, roomMultiple float64) float64 {
	risk := abs(entry - stop)
	best := 0.0
	bestEMA := 0.0
	for _, l := range levels {
		if l.Kind == KindTrendline {
			continue // the LOCATION line is never a target; its KindTrendlineTarget mirror is (CE-11/B12, knob)
		}
		if !oldExtremeRoleFits(l, side) {
			continue // r22 P1-4: a long never targets an old LOW (mirror short)
		}
		onSide := side == SideLong && l.Price > entry || side == SideShort && l.Price < entry
		if !onSide {
			continue
		}
		if isEMAPartialKind(l.Kind) {
			// release #30: an EMA line is a partial point, never the trade
			// target while another level stands beyond.
			if bestEMA == 0 || (side == SideLong && l.Price < bestEMA) || (side == SideShort && l.Price > bestEMA) {
				bestEMA = l.Price
			}
			continue
		}
		if (l.Kind == KindHTFTrigger || l.Kind == KindWickMicroscalp) && roomMultiple > 0 && risk > 0 {
			if abs(l.Price-entry) < roomMultiple*risk {
				continue // target-only level too close to hold 2R room — fall through
			}
		}
		if best == 0 || (side == SideLong && l.Price < best) || (side == SideShort && l.Price > best) {
			best = l.Price
		}
	}
	if best != 0 {
		return best
	}
	return bestEMA
}

// isEMAPartialKind reports whether a level kind is one of the EMA lines the
// course names as PARTIAL points, not the trade target (release #30, D3.3 p1
// @05:15–07:38): the 1m EMA 34, the 1m EMA 9, and the location-gate EMA 34.
func isEMAPartialKind(k LevelKind) bool {
	return k == KindEMA9 || k == KindEMA34 || k == KindEMA34HTF
}

// isMovingLineKey (item 22) reports whether a level key is one of the MOVING
// lines — the 1m EMA34, the EMA34HTF location line, or the trigger-retest line.
// These are re-priced every bar, so their wrong-way "invalid" state is scoped to
// the touching candle: a visit DEPARTURE clears it (the course never makes a
// line dead for good [D5.2 p2 @20:48]). Drift alone does NOT clear it — the EMA
// drifts every tick and a drift reset re-invalidates every tick (item 22 fix).
// Key levels are stable and keep ISB-only until the session day rolls or a
// closed 1H body deletes them.
func isMovingLineKey(key string) bool {
	return key == string(KindEMA34) || key == string(KindEMA34HTF) || key == string(KindTriggerRetest) ||
		strings.HasPrefix(key, string(KindTrendline)+":")
}

// visitKey is a level's knock-knock identity for the B23 visit cap (r22
// P1-2). Knock knock is about ONE level being hit again and again — "a level
// tested many times will certainly break… good support = tested 1, 2, at most
// 3 times" [D1.3 p1 @10:23–11:43; deck slide 17 "càng test nhiều lần → càng
// dễ phá"] — one spot hammered on a door. The trigger line is a fixed price
// only BETWEEN reversals: it moves "only on a reversal — một lần một thôi"
// [D3.4 p1 @11:48–13:33], and each position is a new support/resistance line
// [D3.4 p2 @20:27]. So its identity is the key AT ITS PRICE: the old constant
// key "trigger_retest" counted ~115 line positions per session as ONE level
// (82 of 88 retest rejects refused on 10-13). Every other touched level keeps
// its key (key levels, old extremes, a trendline's own anchors; box edges are
// outside the touch loop and never visit-counted).
func visitKey(lvl Level) string {
	if lvl.Kind == KindTriggerRetest {
		return fmt.Sprintf("%s@%.2f", lvl.Key, lvl.Price)
	}
	return lvl.Key
}

// visitCapApplies reports whether the B23 visit cap governs a level. The EMA
// 34 lines are re-priced every candle — never one spot — and the course gives
// them their OWN repeat rules instead of knock knock: price "xiên lên xiên
// xuống" across the line → turn it off [D4.2 p1 @20:49–24:29] (E4,
// EmaMaxCross30m) and after a loss at the line turn it off until price escapes
// [D4.2 p2 @01:23] (E2, EmaBlocked). Those still gate every EMA reject; the
// visit count is kept for the panel but never caps the line (r22 P1-2: 59 of 65
// EMA 34 rejects on 10-13 were refused by a cap that counted every EMA price
// as one level).
func visitCapApplies(lvl Level) bool {
	return lvl.Kind != KindEMA34 && lvl.Kind != KindEMA34HTF
}

// refuseOncePerVisit counts a PHL/PLH-path refusal ONCE per visit of the
// level (r22, P2-1): the PHL loop re-reads a rejected level on every tick while
// its touch stands, so a bare e.refuse would count the same refusal every
// minute (the level_visit_cap inflation CTO 01:19Z fixed). The mark clears on
// the level's visit departure (touch loop) and at the day rollover.
func (e *Evaluator) refuseOncePerVisit(levelKey, reason string) {
	k := levelKey + "|" + reason
	if e.State.PHLRefusedVisit[k] {
		return
	}
	if e.State.PHLRefusedVisit == nil {
		e.State.PHLRefusedVisit = map[string]bool{}
	}
	e.State.PHLRefusedVisit[k] = true
	e.refuse(reason)
}

// clearPHLRefusedVisit drops every once-per-visit refusal mark of one level
// (its visit departed — a new visit counts again).
func (e *Evaluator) clearPHLRefusedVisit(levelKey string) {
	prefix := levelKey + "|"
	for k := range e.State.PHLRefusedVisit {
		if strings.HasPrefix(k, prefix) {
			delete(e.State.PHLRefusedVisit, k)
		}
	}
}

// Tick evaluates the newest closed 1m candle. bars is the closed history up to
// now (the bot's BarCache slice); now is the current time for the swing
// detector's closed-bar filter. Returns the intents for this candle.
// stampLeave is the leave-stamp + untagged-setup safety net, extracted pure
// so the net itself is pinnable: an untagged ENTRY is dropped (refusal
// untagged_setup), a tagged entry gets its geometry stamped
// (StopPts/TargetPts) and the spent-day flag, and a non-entry action passes
// untouched. The defer in Tick applies the returned refusals through
// e.refuse so the B-rules counter stays single-sourced.
func stampLeave(out []Intent, verdict DayVerdict) ([]Intent, []string) {
	spent := verdict == DaySpent
	kept := out[:0]
	var refusals []string
	for _, in := range out {
		if in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry {
			if in.Setup == "" {
				refusals = append(refusals, "untagged_setup")
				continue
			}
			in.StopPts = abs(in.Price - in.Stop)
			in.TargetPts = abs(in.Target - in.Price)
		}
		in.SpentDay = spent
		kept = append(kept, in)
	}
	return kept, refusals
}

// BarCloseInstant is the evaluator clock for a just-closed 1m bar: the instant
// it closed (CloseTime is the bar's last millisecond, so +1). Every closedness
// test inside Tick (CloseTime >= now), every time-of-day gate (08:32 ORB, 15:00
// window end, 4h boundary) and every order expiry reads `now` as THAT instant,
// so a bar is always evaluated as CLOSED and no gate shifts by a minute.
// Production (trader mentorEvalOnce) and every replay/harness driver call Tick
// with this value — never with the bar's OpenTime.
func BarCloseInstant(last market.Kline) int64 { return last.CloseTime + 1 }

func (e *Evaluator) Tick(bars []market.Kline, now int64) (out []Intent) {
	// FU-2: the per-Tick barsTF memo is scoped to ONE Tick — clear it before
	// anything aggregates, so a new bar always rebuilds the buckets it must.
	e.tfMemo = nil
	// CE-11 FLAG-TARGET (ported from #416): the live flag walls and the
	// current bar's open time, captured at the trendline build for the defer's
	// target pass (zero until then, so an early return leaves them inert).
	var liveFlagWalls flagWalls
	var curBarOpenTime int64
	// A5 + P0 sizing gap (CTO 20:13:25Z): ONE stamp where intents LEAVE Tick —
	// the geometry (StopPts/TargetPts), the spent-day flag, and the
	// untagged-setup drop. The defer covers every return path, including the
	// early ISB missing-target return.
	// P2-N2 (release #23): the school-1 entries this tick emitted WITHOUT
	// trigger agreement. B20's flip upgrade is armed only by the ones that
	// LEAVE Tick — a box return the box ban, the trigger zone, the ORB or the
	// leg budget then refused used to arm it anyway (10-14 08:24: a historical
	// FTGH return set it and the bot logged a spurious "confluence upgrade
	// IGNORED").
	var schoolOne []schoolOneKey
	// m431 (release #52): ONE un-registration mechanism for a dropped entry.
	// Snapshot the ArmIDs the evaluator already held when this Tick began; on
	// the way out (every return path, AFTER the leave-stamp and the deferred
	// flag-wall target pass) any arm that was registered DURING this Tick and
	// whose entry is not among the intents that actually leave is un-registered.
	// The old m425a S1 pass sat mid-Tick and so missed every drop made after it:
	// the flag_wall_room refusal left ISBArms[isb-17] registered on the 10-23
	// 12:22 tape, which then emitted ExtendArm/CancelArm for an order never
	// authored.
	armsAtStart := e.heldArmIDs()
	defer func() {
		defer func() { e.unregisterDroppedArms(armsAtStart, out) }()
		if out == nil {
			return
		}
		kept, refusals := stampLeave(out, e.State.Day.Verdict)
		for _, r := range refusals {
			e.refuse(r)
		}
		out = kept
		out = applyFlagWallTarget(out, liveFlagWalls, curBarOpenTime, e.Cfg, e.refuse) // CE-11 (knob flag_wall_target, default ON — course-defaults-30)
		stampHTFAgree(out, e.State.HTF)
		e.armSchoolOne(out, schoolOne)
	}()
	if !e.Cfg.Enabled || len(bars) < 2 {
		return nil
	}
	levels := Levels(bars, e.Cfg, now)
	if e.seeded {
		levels = e.seededLevels(bars, now)
	}

	// KEY-LEVEL RULING (item 5b, slide 31–32): a CLOSED 1H candle whose BODY
	// closed through a key level DELETES it; a 1H wick through does not. The
	// deletion is permanent for the session and the level leaves the set at
	// once — it can no longer be touched, located, or laddered to.
	if e.seeded {
		// KEYLEVEL-FULL-HISTORY (release #10): deletion was computed ONCE over
		// the full seeded history; per tick only the NEW closed 1H candles past
		// the watermark are walked — never the whole series per level.
		e.advanceKeyLevelDeletion(levels, now)
	} else {
		// Cold path (no Seed): one aggregation per tick, full walk.
		b60 := keyLevel1HBars(bars)
		for _, l := range levels {
			if l.Kind != KindKeyLevel || e.State.DeletedLevels[l.Key] {
				continue
			}
			if levelDeletedBy1HBody(l, b60, now) {
				e.State.DeletedLevels[l.Key] = true
			}
		}
	}
	levels = withoutDeleted(levels, e.State.DeletedLevels)
	e.State.Levels = levels // FU-1 P1-2: the fill drain reads this for the row fallback

	// 5m trigger line advances every 1m close (aggregated 5m bars).
	e.State.Trigger = TriggerTick(e.State.Trigger, e.barsTFMemo(bars, 5), 5, e.Cfg)

	// §5.4 HTF direction (DS-106, fold item 3): the 4h/1h lines advance with
	// the same bar history. Item 18 part 1: a red-folder 07:30 print candle
	// must not move the 1h/4h trigger lines [D4.4 p1 @18:13, @22:15] — the
	// day's print windows (plumbed from the trader's calendar) are dropped
	// from the HTF feed. Empty windows = no print today = byte-identical.
	// CE-4 (release #25, knob news_bars_excluded) was REWORKED (release #29):
	// ON no longer deletes the 07:20–08:30 1m bars (that corrupted every later
	// 1h/4h candle ALL DAY); it FREEZES the pre-print 1h/4h trigger read until
	// the 08:30 CT day-gate read — "tính cái lúc trước news… Còn cái khúc mà
	// news ra rồi thì không tính" [D4.4 p1 @18:13–18:25]. The candles stay
	// whole (htfFeed = the full bars), and after 08:30 the lines resume on the
	// whole candles. OFF = the item-18 print-window drop, byte-identical.
	htfFeed := bars
	if !e.Cfg.NewsBarsExcluded {
		htfFeed = htfFeedBars(bars, e.Cfg.PrintWindows)
	}
	prevHTF := e.State.HTF
	if !newsHTFFrozen(e.Cfg, now) {
		// B3: the 1m bar being ticked is the break minute of a forming-bucket
		// move — MovedAt records it, not the bucket open.
		var breakAt int64
		if len(bars) > 0 {
			breakAt = bars[len(bars)-1].OpenTime
		}
		e.State.HTF = HTFAdvanceAt(e.State.HTF, e.barsTFMemo(htfFeed, 240), e.barsTFMemo(htfFeed, 60), e.Cfg, breakAt)
	}
	e.noteTriggerMove("4h", prevHTF.FourH, e.State.HTF.FourH, bars)
	e.noteTriggerMove("1h", prevHTF.OneH, e.State.HTF.OneH, bars)
	// D4.4-11: with HTFGateNewsOnly ON (the default, the mentor's rule) the
	// 4h/1h direction gate applies on a print day from the print window's
	// start (07:20 CT) through the 08:30 CT open (A9 — the post-print news
	// trade follows the 4h) and in the 08:30–09:30 CT open window (60 min is
	// ours, B28); no 07:20–07:35 gate on a day without a print (R7); OFF (owner decision A 2026-10-21 m134
	// all-day, REVERTED by #45) applies it all day. The gate covers ISB,
	// reverse-ISB, PHL/PLH and box authoring — SWING4H is exempt (runSwing
	// never consults HTFVerdict) and planner/AI entries carry their own G1 HTF
	// veto.
	e.State.HTF.GateOff = !HTFGateActive(now, e.Cfg)

	// §7 day gate (fold item 4): Globex run → per-trading-day latch (L1/L2).
	dg := DayGate{SpentPts: e.Cfg.DayGateSpentPts, TargetCapPts: e.Cfg.DayGateTargetCapPts}
	m := GlobexMeasure(bars, now, ctime())
	e.State.Day = LatchDay(e.State.Day, now, ctime(), m.Run, m.OK, HTFConflict(e.State.HTF), dg)
	// Owner ruling 2026-10-09: the per-day day-gate log line reads the gate's
	// OWN inputs — record them the instant the 08:30 latch freezes (once per
	// trading day; the freeze is the first tick where the Day latch carries a
	// key for a day it has not yet reported).
	if e.State.Day.Key != "" && e.State.DayGateReport.Key != e.State.Day.Key {
		e.State.DayGateReport = DayGateReport{
			Key:      e.State.Day.Key,
			Verdict:  e.State.Day.Verdict,
			RunPts:   m.Run,
			HaveRun:  m.OK,
			HiPx:     m.Hi,
			HiAt:     m.HiAt,
			LoPx:     m.Lo,
			LoAt:     m.LoAt,
			FourHDir: e.State.HTF.FourH.Dir,
			OneHDir:  e.State.HTF.OneH.Dir,
			Conflict: HTFConflict(e.State.HTF),
			SpentPts: dg.SpentPts,
			ReadAt:   now,
		}
		// MENTOR-NY item 6: a boot after 08:30 carries what the 08:30
		// decision read, so the line can show both values.
		if o := e.State.OpenHTF; o.Key != "" && o.Key == e.State.Day.Key {
			e.State.DayGateReport.BootRead = true
			e.State.DayGateReport.OpenFourHDir = o.FourHDir
			e.State.DayGateReport.OpenOneHDir = o.OneHDir
		}
	}
	// Owner ruling 2026-10-09: a latched DayOff re-reads the 4h/1h directions
	// on every CLOSED 1h bar and clears the moment they agree (one-way). The
	// closed 1h bars are the same print-filtered feed the HTF lines advanced
	// on, with the still-forming bucket dropped.
	if e.Cfg.DayOffRecheck {
		e.State.Day = recheckDayOff(e.State.Day, &e.State.DayRecheck, e.State.HTF, e.closedBucketsTFMemo(htfFeed, 60, now), now, ctime())
	}
	// B23: the visit counters belong to the latched trading day — a new day
	// starts every level's cap over.
	if e.State.Day.Key != e.State.VisitsDay {
		e.State.Visits = map[string]int{}
		e.State.VisitsDay = e.State.Day.Key
		e.State.PHLRefusedVisit = nil // r22: a new day re-arms the once-per-visit counters
		// item 22: a KEY level's ISB-only invalidity is scoped to the session
		// day (the course never makes a level dead for good [D5.2 p2 @20:48]).
		// The moving lines reset on visit departure in the touch loop below,
		// so only the stable keys reset here.
		for key := range e.State.ISBOnly {
			if !isMovingLineKey(key) {
				delete(e.State.ISBOnly, key)
			}
		}
	}

	// the location set grows by the trigger-line retest and the EMA 34
	// location line (fold item 1; R3: 1m default).
	if e.State.Trigger.Dir != "" && e.State.Trigger.Price != 0 {
		levels = append(levels, Level{Key: string(KindTriggerRetest), Kind: KindTriggerRetest, Price: e.State.Trigger.Price})
	}
	// D4.4-15: the 4h trigger line joins the TARGET LADDER (never the
	// location set) — a trade's target can be the 4h trigger line when
	// it sits between the entry and the next level [D4.4 p2 @05:04–06:02
	// "TARGET MÌNH VỀ LẠI 4 GIỜ nè anh chị… CÁI LỆNH TRĂM ĐIỂM của mình LÀ VỀ
	// ĐÂY"]. Target-only: the touch loop skips KindHTFTrigger and
	// levelIsLocation returns false for it.
	if e.State.HTF.FourH.Dir != "" && e.State.HTF.FourH.Price != 0 {
		levels = append(levels, Level{Key: "htf_4h_trigger", Kind: KindHTFTrigger, Price: e.State.HTF.FourH.Price})
	}
	// Release #52 B5: the 1H trigger line is NOT a target. The
	// course names the 4h line as the target ("Về lại 4 giờ nè anh chị" [D4.4
	// p2 @05:04–05:44], one worked example) and never the 1h; the 1h line only
	// gates direction (htf_direction.go). It used to join the ladder at every
	// hour (htf_1h_trigger) and stopped ISB/PHL targets short at a 1h line.
	// D4.3 wick microscalp (advanced, knob OFF by default): 2+ consecutive 5m
	// candles rejecting with same-way wicks put a bounded target at their far
	// wick — target-only (the touch loop skips KindWickMicroscalp).
	if e.Cfg.WickMicroscalpEnabled {
		if wl, ok := wickMicroscalpLevel(e.closedBucketsMemo(bars, now), e.State.Trigger.Dir); ok {
			levels = append(levels, wl)
		}
	}
	if el, ok := e.emaLocationLevel(bars, e.Cfg, now); ok {
		levels = append(levels, el)
	}
	e.resetEMAVisitsOnFrameSwitch(now)

	// §7 step 0: the ORB gate advances every tick (drawn at 08:32 CT; the
	// side is the side of the LATEST 1m candle whose BODY is wholly outside —
	// open AND close [D4.1 p1 @20:36; X5 @02:57; X7 @07:42]). Every escape and
	// every side switch is logged once, with the candle that made it.
	prevORB := e.State.ORB
	e.State.ORB = ORBAdvanceMode(prevORB, bars, now, e.Cfg.OrbEscapeCompletely)
	e.noteORBEscape(prevORB, e.State.ORB, bars[len(bars)-1])

	// §4.1 FTGH/FTGL boxes (BOX RULING part 2): built per tick; the edges
	// join the level set as locations (used again and again) and the box
	// interior bans entries.
	// m410: the frame new boxes draw on this tick.
	e.boxFrameTF = e.boxFrameCfg(now).TF
	boxes := e.buildBoxes(bars, now) // CX-11: fresh RTH pair + max height (knobs; OFF = BoxesBuild)
	// D4.1-22: the first box per role per day is latched — never redrawn on a
	// later higher high / lower low ("không vẽ cái box khác… y nguyên đó tới
	// cuối ngày" [D4.1 p2 @02:39–02:57]). A new trading day clears the latch.
	// Release #22 (P2-2): the latch belongs to the TRADING day (17:00 CT
	// roll, the same tradingDayKey BoxesBuild filters on) — never the day-gate
	// key, which is "" until 08:30 and so cleared the latch at the RTH open
	// and redrew both boxes on a higher high / lower low.
	if boxDay := boxLatchDayKey(now, e.Cfg); e.State.LatchedBoxesDay != boxDay {
		if e.State.LatchedBoxesDay != "" && len(e.State.LatchedBoxes) > 0 {
			e.note("box latch cleared at the 17:00 CT session roll: trading day %s → %s (FTGH/FTGL redraw from the new session only) [D4.1 p2 @02:58]", e.State.LatchedBoxesDay, boxDay)
		}
		e.State.LatchedBoxes = nil
		e.State.PostEscapeBoxes = nil // B52: the new-zone boxes die with the day too
		e.State.LatchedBoxesDay = boxDay
	}
	boxes = e.latchBoxes(boxes, bars, e.Cfg.Box)
	levels = append(levels, BoxEdgeLocations(boxes)...)

	// X9 (slide 27) + DAY-3 row 27: trendlines as LOCATIONS — two same-role
	// swings joined, valid only after the 3rd touch, discarded by a 5m close
	// through. They join the level set location-only (never a target); a
	// trendline next to a box edge is suppressed (box beats trendline).
	exec2m := e.Cfg.Exec2mAfter30m && exec2mReads2m(now)
	trendlines, crossed := trendlinesBuildExecB5(bars, e.barsTFMemo(bars, 5), time.UnixMilli(now), exec2m)
	frame := 1
	if exec2m {
		frame = 2
	}
	trendlines = e.carryIntradayLines(trendlines, bars, time.UnixMilli(now), frame, e.Cfg.Exec2mAfter30m)
	htfLive, htfBroken := e.htFTrendlines(bars, time.UnixMilli(now))
	trendlines = append(trendlines, htfLive...)
	liveFlagWalls = buildFlagWalls(trendlines) // CE-11 FLAG-TARGET (#416)
	curBarOpenTime = bars[len(bars)-1].OpenTime
	e.trackTrendlines(trendlines, htfBroken, crossed, now)
	trendlines = dropDeadTrendlines(trendlines) // m428 R3: a broken line is deleted — not drawn, not a level, not carried (trackTrendlines already noted it)
	tlLevels, tlRefused := TrendlineLevels(trendlines, boxes, levels, bars)
	for _, k := range tlRefused {
		e.refuse("tl_first_test_level_preferred:" + k)
	}
	levels = append(levels, tlLevels...)
	e.State.ChartLevels, e.State.ChartTrendlines = append([]Level(nil), levels...), trendlines // T7: display set read AFTER every append
	e.State.ChartEMA = emaSeriesChartSched(bars, e.Cfg)                                        // m134: display EMA curve
	e.State.ChartSeededAt = 0                                                                  // m422: a real Tick replaces the boot display seed
	// CE-11/B12 (release #25, knob trendline_is_target, default ON): each
	// valid trendline ALSO joins the target ladder at its current price —
	// "Target là về những level kế tiếp… về trendline" [D3.3 p1 @05:18–05:35;
	// @07:57–08:04]. Target-only (the touch loop and the location gate skip
	// KindTrendlineTarget); the display set above is unchanged.
	if e.Cfg.TrendlineIsTarget {
		levels = append(levels, trendlineTargets(levels)...)
	}

	// §2.1 / §2.2 exec-frame schedule (F2, W-MENTOR-TRANSCRIPT-TRUTH): the
	// X5-10 knob (Exec2mAfter30m, default ON since release #31) applies to
	// EVERY setup the transcript names — the ISB pair AND PHL/PLH ("Lớp học
	// của mình chỉ only đúng 3 setup: inside bar, potential high or low,
	// potential low or high" [X11 @07:09–07:30]; "Mình trade khung 2 phút …
	// potential high low" [X8 @15:52–15:57]). The frame is 1m at most 2m —
	// "chúng ta chỉ trade ở khung 1 phút, maximum là khung 2 phút thôi"
	// [D1.4 p1 @01:45–01:51]:
	//   • RTH open +30 minutes (08:30 ≤ t < 09:00 CT) → 1-MINUTE read only
	//     ("Trong dòng 30 phút đầu giờ em vẫn sẽ trade khung 1 phút … sau 30
	//     phút em sẽ chuyển qua khung 2 phút" [X5 @00:41–01:14; X11
	//     @17:08–17:25]).
	//   • a SLOW evening/overnight (15:00 CT → 03:00 CT, overnight range under
	//     100 pts) → 5-MINUTE read ("Trade khung 5 phút only / Cho buổi tối"
	//     [X11 @17:40–18:32, the quote is @18:03–18:07; the transcript has no
	//     "đi chậm thì đánh khung 5 phút thôi" sentence — that wording was ours];
	//     the 100-pt cutoff is ours — X11 only names 100–200 pts as a STRONG
	//     night where the 2m stays fine [derived: X11 @17:40–18:26]).
	//   • every other minute → 2-MINUTE read ("khung 2 phút ÍT BỊ QUÉT RÂU" —
	//     the 1m wicks sweep stops [X5 @00:16; X11 @17:27]), a strong evening,
	//     overnight and the Europe open (3am Texas) included.
	// What STAYS on the 1m: the box/ORB escapes and every higher-TF read — they
	// are defined on the 1m body close and are NOT one of the three named
	// setups. While the knob is OFF, execPrev/execCur == prev/cur and the read
	// is byte-identical to the 1m-only build.
	prev, cur := bars[len(bars)-2], bars[len(bars)-1]
	execPrev, execCur, execTFMin := prev, cur, 1
	if e.Cfg.Exec2mAfter30m {
		want := 2
		switch {
		case !exec2mReads2m(now):
			want = 1
		case inEveningWindow(now) && eveningSlow(now, bars, e.Cfg):
			want = 5
		}
		// closedBucketsTF drops the still-forming last bucket: on a 1m close
		// that OPENS a 2m/5m bucket, barsTF would flush a half-formed candle and
		// the read would see it. The read must be the previous two CLOSED
		// candles (the cb15/cb30 helper path). If the chosen TF has fewer than 2
		// closed buckets (early in the evening), fall back down 5 → 2 → 1.
		for tf := want; tf > 1; tf = 2 {
			if b2 := e.closedBucketsTFMemo(bars, tf, now); len(b2) >= 2 {
				execPrev, execCur = b2[len(b2)-2], b2[len(b2)-1]
				execTFMin = tf
				break
			}
			if tf == 2 {
				break // no 2 closed 2m candles yet → the 1m read stays
			}
		}
	}
	// X11 pullback warning (size cut, this tick): a 5m candle that broke the
	// prior 5m candle's low in a 5m uptrend (mirror: the prior high in a 5m
	// downtrend) [X11 @13:21–15:02, quote @14:50–15:00]. C52-W10 (B20).
	e.pullbackWarn5m = pullbackWarning5m(e.closedBucketsTFMemo(bars, 5, now))

	// §3: first-touch classification per level. BOX edges are OUT of this
	// loop (BOX PATH DECISION, CTO 12:38:50Z): the box path is DS-106's
	// boxEntryIntent — the level path treating each edge as a separate line
	// would double-trade a box return (a candle inside the box can look like
	// an approach from above). The edges stay in `levels` for location /
	// InsideAnyBox / boxBanFilter / midRangeBoxed checks.
	for _, lvl := range levels {
		if lvl.Kind == KindFTGHEdge || lvl.Kind == KindFTGLEdge || lvl.Kind == KindHTFTrigger || lvl.Kind == KindWickMicroscalp || lvl.Kind == KindTrendlineTarget {
			continue
		}
		moving := isMovingLineKey(lvl.Key)
		tr := e.State.Touches[lvl.Key]
		// item 22: a KEY level stays ISB-only for the session day (frozen touch,
		// no re-read); a MOVING line keeps its visit machinery running so a
		// DEPARTURE can clear the invalidity — the course scopes "invalid" to
		// the touching candle [D5.2 p2 @20:48], never the line. Item 22 fix
		// (CTO 00:19Z, DS-105 replay): drift alone NEVER resets a moving line —
		// the EMA drifts every tick, and a drift reset re-touches and
		// re-invalidates every tick (level_invalid 40→446/day, visit cap
		// 293→14,875). The reset is only the visit-departure below.
		if e.State.ISBOnly[lvl.Key] && !moving {
			continue // invalid key level: no PHL/PLH, and touches need no re-read
		}
		wasNone := tr.Outcome == TouchNone
		// F2: the touch classification reads the EXEC frame — the closed 2m
		// candles when the knob is ON and past the RTH open +30 window, the 1m
		// otherwise (the 1m stays for the first 30 minutes [X5 @00:41–01:14]).
		tp, tc := prev, cur
		if execTFMin > 1 {
			tp, tc = execPrev, execCur
		}
		intents := visitTick(&tr, lvl, tp, tc, e.Cfg)
		if !wasNone && tr.Outcome == TouchNone {
			// visit departure: the touching candle is gone. Clear the visit-cap
			// refusal mark for EVERY level (a new visit re-arms the once-per-visit
			// level_visit_cap), and clear the ISB-only state ONLY for a moving
			// line (the ONLY reset for a moving line — drift alone keeps the
			// classification, item 22 fix).
			delete(e.State.VisitCapRefused, lvl.Key)
			e.clearPHLRefusedVisit(lvl.Key)
			if moving {
				delete(e.State.ISBOnly, lvl.Key)
			}
		}
		e.State.Touches[lvl.Key] = tr
		if e.State.ISBOnly[lvl.Key] {
			continue // still invalid (no drift, no departure this tick)
		}
		// B23: each NEW reject classification is one visit of that level
		// today (a revisit can only classify after the departure reset, so
		// None -> Reject is exactly one visit). r22 P1-2: the visit is counted
		// against the level's REAL identity (visitKey) — the trigger line at
		// THIS price, not every position the line ever held.
		if wasNone && tr.Outcome == TouchReject {
			if e.State.Visits == nil {
				e.State.Visits = map[string]int{}
			}
			e.State.Visits[visitKeyFor(lvl, e.Cfg.CourseExits.EMA9VisitCap)]++
		}
		out = append(out, handleTouchIntents(e, lvl, intents)...)
	}

	// §2.1: ISB on the last closed pair. R1: the direction is the CANDLE-1
	// colour, not the trigger line (RULES-FIX-v3). prev/cur and the exec frame
	// (execPrev/execCur/execTFMin) were computed before the touch loop (F2,
	// W-MENTOR-TRANSCRIPT-TRUTH) so the ISB pair and the PHL/PLH touch
	// classification read the SAME closed 2m candles.
	// fold #565 (D4): the ISB pair is evaluated only when the exec candle
	// ADVANCED — a 2m read on an odd 1m minute sees the same 2m candle as the
	// previous tick, so the placement (and the reverse ISB) runs once per 2m
	// candle, never re-emitted one minute later.
	// m417 F2: ADVANCED means STRICTLY newer (`>`, not `!=`). On a frame flip
	// (5m→2m at 18:15) the exec candle can be OLDER than the watermark — the
	// 2m close 18:14 vs the 5m close 18:15 — and `!=` would move the watermark
	// BACKWARDS and re-evaluate the ISB pair on a stale candle.
	isbEval := execCur.CloseTime > e.State.LastISBEvalClose
	if isbEval {
		e.State.LastISBEvalClose = execCur.CloseTime
	}

	// arms placed on THIS tick are skipped by the stacking loop below.
	justPlaced := map[string]bool{}

	// R5 (RULES-FIX-v3): the 5m ISB rest box. While it stands it replaces the
	// trigger line for gating; nothing trades inside it except a same-direction
	// 1m ISB, and it is deleted when a 1m BODY closes outside [D3.4 p2 @
	// 00:14–13:00; D4.1 p1 @ 21:38].
	if e.State.ISBBox != nil {
		// CE-5 (release #25): the escape test is the isb_box_escape knob —
		// body (default) = open AND close beyond the edge [D4.1 p1 @20:52–21:02].
		if escaped, dir := ISBBoxEscapeWith(*e.State.ISBBox, cur, e.Cfg.ISBBoxEscapeMode); escaped {
			e.State.ISBBox = nil
			e.State.LastISBBoxEscape = dir // CE-1: which way the 5m box resolved
		}
	} else if cb := e.closedBucketsMemo(bars, now); len(cb) >= 2 {
		// One box per 5m ISB pair: an escaped box is gone for good [D3.4 p2
		// @ 12:02], so the same pair never builds it again.
		if bx, ok := ISBBoxFrom5m(cb[len(cb)-2], cb[len(cb)-1]); ok && bx.AtTime != e.State.LastISBBoxAt {
			e.State.ISBBox = &bx
			e.State.LastISBBoxAt = bx.AtTime
			e.State.LastISBBoxDir = bx.Dir // CE-1
			e.State.LastISBBoxEscape = ""  // CE-1: standing, not resolved yet
		}
	}

	// DS-103 item 4 (D4.2-06): the 15m/5m conflict and the MTF-alignment
	// mode-C read use the SAME CLOSED buckets — computed once per tick.
	cb5 := e.closedBucketsMemo(bars, now)
	cb15 := e.closedBucketsTFMemo(bars, 15, now)
	cb30 := e.closedBucketsTFMemo(bars, 30, now)

	// D4.1-25: the 15m ISB rest box — box the latest CLOSED 15m inside-bar
	// candle (the REAL 15m TF, B8) and keep it until a 1m BODY closes outside.
	// While it stands, entries may only go its direction and PHL/PLH + box
	// returns are refused inside it [D4.1 p2 @07:35–08:11; D4.2 p1 @01:06,
	// 04:49].
	if e.State.ISBBox15m != nil {
		if escaped, _ := ISBBoxEscapeWith(*e.State.ISBBox15m, cur, e.Cfg.ISBBoxEscapeMode); escaped { // CE-5
			e.State.ISBBox15m = nil
		}
	} else if len(cb15) >= 2 {
		if bx, ok := ISBBoxFrom5m(cb15[len(cb15)-2], cb15[len(cb15)-1]); ok && bx.AtTime != e.State.LastISBBox15mAt {
			e.State.ISBBox15m = &bx
			e.State.LastISBBox15mAt = bx.AtTime
		}
	}

	// D4.2-07: the 30m ISB rest box — the same machinery as the 15m box on the
	// 30m TF. "Never trade against a 15m/30m ISB inside its range" [D4.2 p1
	// @16:28–16:46, 18:02–18:36].
	if e.State.ISBBox30m != nil {
		if escaped, _ := ISBBoxEscapeWith(*e.State.ISBBox30m, cur, e.Cfg.ISBBoxEscapeMode); escaped { // CE-5
			e.State.ISBBox30m = nil
		}
	} else if len(cb30) >= 2 {
		if bx, ok := ISBBoxFrom5m(cb30[len(cb30)-2], cb30[len(cb30)-1]); ok && bx.AtTime != e.State.LastISBBox30mAt {
			e.State.ISBBox30m = &bx
			e.State.LastISBBox30mAt = bx.AtTime
		}
	}

	// D4.2-03: the 15m/5m conflict is a standing STATE, not a one-shot pair
	// veto. Opposite 15m and 5m box directions → "làm ơn đừng trade luôn" —
	// cancel every live arm BY ArmID and refuse ALL setups until one of the
	// two boxes escapes [D4.2 p1 @13:59–14:53, @07:37–08:00].
	// CE-2 (release #25, knob tf_conflict_end): "both_boxes" holds the
	// conflict until BOTH boxes have escaped [D4.2 p1 @07:54–08:02 "Đợi nó
	// thoát cả luôn"]; "first_box" (default) is the pre-#25 read.
	conflict, conflictHeld, latch := tfConflictHeld(e.Cfg.TFConflictEnd, e.State.ISBBox, e.State.ISBBox15m, e.State.TFConflictLatched)
	e.State.TFConflictLatched = latch
	if conflict {
		out = append(out, e.conflictArmCancels()...)
		if IsISB(execPrev, execCur) {
			if conflictHeld {
				// The held half is new behaviour: name it (L7) — an ISB candidate
				// refused only because the conflict waits for the 15m box too.
				e.refuse("isb_tf_conflict_both_boxes")
			} else {
				// P2-1 (L7): in first_box mode the conflict dropped this ISB
				// silently — name and count the refusal. No behaviour change:
				// the ISB is still refused (the !conflict path below does not run).
				e.refuse("isb_tf_conflict")
			}
		}
	}

	if isbEval && !conflict && IsISB(execPrev, execCur) {
		// D4.1-23: while the 5m ISB rest box stands, the box — not the 5m
		// trigger line — governs the 1m ISB. The trigger PRICE-side filter is
		// skipped entirely ("khung 5 phút kêu làm gì, làm cái đó" [D4.1 p2
		// @04:06–06:45]; METHOD §5.2 "do not use the trigger line while it
		// stands"); the box's own direction gate (ISBBoxAllows) runs below.
		var trigSide Side
		dirOK := true
		if e.State.ISBBox == nil {
			dirOK, trigSide, _ = TriggerVerdict(e.State.Trigger, cur.Close)
		}
		if dirOK {
			// The conflict reads the standing boxes above (D4.2-03); the
			// still-forming 5m bucket is dropped [D4.2 p1 @ 05:10: "a
			// 15-minute candle is only confirmed once CLOSED; trade from
			// the next one"]. B8: the 15m side is the REAL 15m TF.
			{
				// OWNER RULING 2026-10-03 ("exactly like he said"): the ISB is
				// NOT location-gated — "inside bar lúc nào cũng có thể take
				// risk… trong range, trên range, ngoài range, dưới range"
				// [D4.1 p1 @05:19–05:34; the range words are D3.4 p3 @03:23–03:37]. His conditions stay: the trigger zone
				// (above), the R5 box (below), the stop ceiling (stop > ISBMaxStopPts, default 25, is skipped; 20–25 is the reduced tier), and a size
				// flag at an old high/low.
				//
				// fold item 3: entries only with the 4h trigger direction
				// (1h agreeing or silent); fold item 4: a DayOff shuts the
				// machine off for the day.
				if htfOK, htfSide, _ := HTFVerdict(e.State.HTF); !htfOK {
					e.refuse("isb_htf_blocked")
				} else {
					// R1 (RULES-FIX-v3): the order is a STOP-LIMIT in the
					// CANDLE-1 colour direction [D1.4 p1 @ 09:20–10:20, 24:41–24:55].
					// (The stop-size skip is DS-104's ISB stop band — D3.3/D4.1.)
					// R5: while the box stands nothing trades inside it except a
					// SAME-direction 1m ISB — compute the verdict up front so an
					// allowed ISB still falls through to the emit below.
					boxBlocked := false
					if e.State.ISBBox != nil && !crossingISBs(cb5, 3) {
						if allowed, r := ISBBoxAllows(*e.State.ISBBox, execPrev, execCur); !allowed {
							boxBlocked, _ = true, r
						}
					}
					// D4.1-25: the 15m box gates the ISB exactly like the 5m box —
					// inside it only a 1m ISB in the 15m box's direction [D4.1 p2
					// @07:35–08:11; D4.2 p1 @03:40–04:45]. The escalation ladder
					// (D3.4 p2 @17:07–17:49) skips this gate when the 15m ISBs
					// are crossing.
					box15Blocked := false
					if e.State.ISBBox15m != nil && !crossingISBs(cb15, 3) {
						if allowed, r := ISBBoxAllows(*e.State.ISBBox15m, execPrev, execCur); !allowed {
							box15Blocked, _ = true, r
						}
					}
					// D4.2-07: the 30m box gates the ISB the same way — never
					// trade against a 30m ISB inside its range; skipped when the
					// 30m ISBs are crossing.
					box30Blocked := false
					if e.State.ISBBox30m != nil && !crossingISBs(cb30, 3) {
						if allowed, r := ISBBoxAllows(*e.State.ISBBox30m, execPrev, execCur); !allowed {
							box30Blocked, _ = true, r
						}
					}
					// CE-1 (release #25, knob 15m_ib_requires_5m_isb): inside a
					// standing 15m ISB box the 5m must show an ISB the same way
					// before a 1m ISB trades [D4.2 p1 @14:24–14:34, @17:09–17:13].
					fifteenRefusal := ""
					if e.State.ISBBox15m != nil && !crossingISBs(cb15, 3) {
						fifteenRefusal = fifteenMinISBGate(e.Cfg.FifteenMinIBRule, e.State.ISBBox15m, e.State.ISBBox, e.State.LastISBBoxAt, e.State.LastISBBoxDir, e.State.LastISBBoxEscape)
					}
					side, chosen, ok, why := ISBStopLimitOrder(execPrev, execCur, e.Cfg)
					if !ok {
						e.refuse(isbStopRefusal("isb_", why))
					} else if isbArmActive(e.State.ISBArms, side) {
						// ONE ARM PER SIDE (CTO parity ruling 1791008332386 #1):
						// stacking extends the EXISTING arm — no new arm.
						// REL-5 audit #3: name the skip instead of a silent drop.
						e.refuse("isb_arm_active")
					} else if boxBlocked {
						// R5: an opposite-direction ISB inside the box — no entry
						e.refuse("isb_box_blocked")
					} else if box15Blocked {
						// D4.1-25: an ISB against the 15m box direction — no entry
						e.refuse("isb_box_blocked_15m")
					} else if box30Blocked {
						// D4.2-07: an ISB against the 30m box direction — no entry
						e.refuse("isb_box_blocked_30m")
					} else if fifteenRefusal != "" {
						// CE-1: no same-way 5m ISB inside the 15m box (or option
						// 1: no trade until the 15m box escapes).
						e.refuse("isb_" + fifteenRefusal)
					} else if e.State.ISBBox == nil && trigSide != "" && side != "" && side != trigSide {
						// I1: while NO R5 5m ISB box stands, the LIVE 5m trigger
						// line governs the ISB side — a short ISB above a buy line
						// (or a long below a sell line) is refused [D3.4 p1
						// @09:30–10:38, @17:39; p3 @02:24; D4.3 @14:40]. While a
						// box stands, that box's direction governs instead — the
						// R5 box rule [isb_box.go: ISBBoxAllows, D3.4 p2
						// @00:14–13:00] already refused an opposite-direction ISB
						// above (boxBlocked), so this check is skipped entirely.
						e.refuse("isb_trigger_side_mismatch")
					} else if side != "" && htfSide != "" && side != htfSide {
						// ISB direction against the 4h — no entry
						e.refuse("isb_htf_side_mismatch")
					} else if r := dayGateRefusal(e.State.Day.Verdict); r != "" {
						// A10: day off OR not-measured — no intraday mentor
						// entries today (named per setup).
						e.refuse("isb_" + r)
					} else if refuse, _ := nearBoxRefusal(boxes, chosen.Price, side, e.Cfg.NearBoxRoomMultiple, abs(chosen.Price-chosen.Stop)); refuse {
						// Day-3 row 24 [D3.2 p1 @ 21:53–23:08]: the entry is
						// too close to the nearest box edge in the trade
						// direction — "sát box" — skip it.
						e.refuse("near_box")
					} else {
						// §6 [D3.3 p1 @ 05:07]: "TARGET LÀ VỀ NHỮNG LEVEL KẾ
						// TIẾP" — a setup gives entry, stop AND target
						// [D4.1 p1 @ 01:39]; no level beyond → no trade — a
						// missing (or sub-1:1) target skips ONLY this ISB, never
						// the rest of the tick (CTO E-1/E-2 2026-10-03T15:12Z).
						target := nextLevelBeyondRoom(levels, chosen.Price, chosen.Stop, side, e.Cfg.RoomMultiple)
						if target == 0 {
							e.refuse("isb_missing_target")
						} else if !targetFloorOK(chosen.Price, chosen.Stop, target) {
							e.refuse("isb_target_below_floor")
						} else {
							chosen.Target = target
							// D4.2-06 part 1: confluence is computed BEFORE the room
							// check — a confluence entry's leg 1 is 2R, so its room is 4R.
							confluence := MTFConfluence(side, cb5, cb15)
							// B9 [D5.1 p1 @16:24, @19:11-20:07]: ISBs obey the spent-day
							// cap too ("15 điểm bán, 10 điểm bán"). A cap that breaks the
							// 1:1 floor refuses (same reason as an uncapped short target).
							capped := CapTargetForDay(chosen, e.State.Day.Verdict, dg)
							if !targetFloorOK(capped.Price, capped.Stop, capped.Target) {
								e.refuse("isb_target_below_floor")
							} else if refuse, _ := roomRefusal(chosen.Price, chosen.Stop, target, confluence, e.Cfg.RoomMultiple); refuse {
								// The room to the first level must be >= RoomMultiple ×
								// the leg-1 take-profit (1R; 2R for confluence).
								// MENTOR-NY item 1: the room is measured to the LEVEL
								// (`target`, before the spent-day cap), never to the
								// capped take-profit — "Dù target của em là 15 điểm …
								// từ điểm entry xuống target nó phải từ 30 điểm đổ lên"
								// [D5.3 p1 @09:32–09:50]. The cap still shortens the
								// take-profit and the 1:1 floor above still reads it.
								e.refuse("room")
							} else if refuse, _ := spentDayRoomRefusal(chosen.Price, target, capped.Target, e.State.Day.Verdict, e.Cfg.RoomMultiple); refuse {
								// SPENT day: room ≥ 2× the capped take-profit too
								// ("target 15 → room 30", D5.3 p1 @09:32–09:50).
								e.refuse("room")
							} else {
								// The emitted intent carries the CAPPED target: `capped`
								// above fed only the floor check, so a spent-day ISB went
								// out with the full target (CapTargetForDay changes the
								// target only; Price and Stop are untouched).
								chosen.Target = capped.Target
								// ISB size flags for the injector: rule 2 (at an old
								// high/low → REDUCE SIZE) and rule 3 (in a range → REDUCE
								// SIZE, "Khi trade isb in-range bắt buộc giảm size" [D4.1 p1
								// @ 04:37–05:04]) — the range is the same mid-range test as
								// the PHL/PLH ban.
								chosen.Flag = e.isbFlagsFor(execCur, chosen.Side, levels, boxes)
								// N12: a single ISB fills by the close of the NEXT
								// candle on the execution timeframe (1m default; 2m
								// under X5-10) or it is cancelled ("cancel if the
								// next candle does not fill" [D1.4 p1 @
								// 18:32–18:45]); stacking extends it below.
								chosen.ExpiryMs = execCur.CloseTime + int64(execTFMin)*60_000
								// X8 @18:02–18:14 "trade khung nào thì gồng theo khung
								// đó": the runner trails on the frame the entry was
								// READ on — 1m, 2m or 5m (m417: the 1m stamp is REAL,
								// so the X8 trap-dodge reads the 1m on a 1m entry).
								chosen.ExecTFMin = execTFMin
								e.State.ArmSeq++
								id := fmt.Sprintf("isb-%d", e.State.ArmSeq)
								// the ISB candle is the 1st inside candle (Inside=1), so the
								// stacking loop must skip this arm on the placement bar.
								// fold #565 (D1): the arm records the frame it was READ on —
								// the stacking count advances on that frame's candles, not
								// on every 1m bar.
								e.State.ISBArms[id] = ISBArm{FirstBar: execCur, Inside: 0, Side: side, TFMin: execTFMin, LastTick: execCur.CloseTime}
								justPlaced[id] = true
								chosen.ArmID = id
								// D4.2-06 part 1: mode C also fires on timeframe
								// agreement — 15m = 5m = entry side AND the 5m
								// trigger agrees [D4.2 p1 @14:57].
								chosen.Confluence = confluence
								out = append(out, chosen)
							}
						}
					}
				}
			}
		} else {
			e.refuse("isb_trigger_side")
		}
	}

	// R7 (RULES-FIX-v3, behind its own knob, default ON since R-C): the reverse
	// ISB at EMA 9 — an ISB that points AGAINST the trend with price at the EMA 9
	// trades WITH the trend [D5.4 p1 @04:16–04:20].
	//
	// P2-1 (L7): the reverse ISB was dropped silently while the 15m/5m conflict
	// stood. Name and count it; no behaviour change (the !conflict path below
	// still does not run). Release #30: isbrev_tf_conflict / isbrev_day_off
	// count only a REAL reverse-ISB candidate (an ISB at the EMA 9 pointing
	// against the trend) — before they fired for every ISB.
	//
	// C52-W4 (A5, frame fix): the EMA 9 is read on the frame the pair is read on —
	// "anh trade khung nào thì anh bật khung đó, khung M1 cũng được, khung M2
	// cũng được" [Ngày 5.4 ISB đảo chiều part1.srt @00:08:15–00:08:40]. It used
	// to be the raw 1m EMA 9 against 2m candles. The 5m evening path is NOT
	// changed (Q9 unanswered): it keeps the 1m EMA 9.
	revEma9 := e.reverseISBEMA9(bars, execCur, execTFMin, now)
	revIn, revCandidate, _ := ReverseISBAtEMA9(execPrev, execCur, revEma9, e.State.Trigger.Dir, e.Cfg)
	if isbEval && conflict && e.Cfg.ISBReverseEMA9Enabled && revCandidate {
		e.refuse("isbrev_tf_conflict")
	}
	// B6 (L14): the reverse ISB must stand behind the SAME standing gates as the
	// normal ISB — the 15m/5m conflict, the ISB boxes, and the trigger side — and
	// fail closed when the 5m trigger has no direction yet (it used to fall
	// through to SHORT unconditionally).
	if isbEval && !conflict && e.Cfg.ISBReverseEMA9Enabled && IsISB(execPrev, execCur) {
		if r := dayGateRefusal(e.State.Day.Verdict); revCandidate && r != "" {
			e.refuse("isbrev_" + r)
		} else if e.State.Trigger.Dir == "" {
			// B6: with no 5m trigger there is no trend to reverse — R7 always
			// resolved to SHORT here before. Fail closed [D5.4: "đánh theo xu
			// hướng" — the trend must exist].
			e.refuse("isbrev_no_trigger")
		} else if revCandidate {
			in := revIn
			// N12: an R7 reverse ISB fills by the close of the NEXT candle on
			// the execution timeframe (the R1 family rule).
			in.ExpiryMs = execCur.CloseTime + int64(execTFMin)*60_000
			// X8 @18:02–18:14: the trail frame follows the entry frame
			// (1m, 2m or 5m — m417).
			in.ExecTFMin = execTFMin
			// R85 (CTO, release #4): now that the reverse ISB can place, it
			// runs through the SAME gates as the normal ISB — the stop-ceiling
			// skip [D4.1 p1 @ 05:41; ISBStopVerdict, default 25], the 4h HTF
			// verdict and side, and the near-box rule (row 24) — before any
			// target is set.
			// B6: plus the SAME ISB-box gates (R5 / D4.1-25 / D4.2-07) and the
			// trigger-side guard the normal ISB runs.
			boxBlocked := false
			if e.State.ISBBox != nil && !crossingISBs(cb5, 3) {
				if allowed, _ := ISBBoxAllows(*e.State.ISBBox, execPrev, execCur); !allowed {
					boxBlocked = true
				}
			}
			box15Blocked := false
			if e.State.ISBBox15m != nil && !crossingISBs(cb15, 3) {
				if allowed, _ := ISBBoxAllows(*e.State.ISBBox15m, execPrev, execCur); !allowed {
					box15Blocked = true
				}
			}
			box30Blocked := false
			if e.State.ISBBox30m != nil && !crossingISBs(cb30, 3) {
				if allowed, _ := ISBBoxAllows(*e.State.ISBBox30m, execPrev, execCur); !allowed {
					box30Blocked = true
				}
			}
			// CE-1: the same 15m rule as the normal ISB.
			fifteenRefusal := ""
			if e.State.ISBBox15m != nil && !crossingISBs(cb15, 3) {
				fifteenRefusal = fifteenMinISBGate(e.Cfg.FifteenMinIBRule, e.State.ISBBox15m, e.State.ISBBox, e.State.LastISBBoxAt, e.State.LastISBBoxDir, e.State.LastISBBoxEscape)
			}
			htfOK, htfSide, _ := HTFVerdict(e.State.HTF)
			target := 0.0
			if _, stopOK, why := ISBStopVerdict(execCur, e.Cfg); !stopOK {
				e.refuse(isbStopRefusal("isbrev_", why))
			} else if boxBlocked {
				e.refuse("isbrev_box_blocked")
			} else if box15Blocked {
				e.refuse("isbrev_box_blocked_15m")
			} else if box30Blocked {
				e.refuse("isbrev_box_blocked_30m")
			} else if fifteenRefusal != "" {
				e.refuse("isbrev_" + fifteenRefusal) // CE-1
			} else if !htfOK {
				e.refuse("isbrev_htf_blocked")
			} else if htfSide != "" && in.Side != htfSide {
				e.refuse("isbrev_htf_side_mismatch")
			} else if in.Side != e.State.Trigger.Dir {
				// B6: the 5m trigger side governs [D3.4 p1 @ 09:30–10:38] —
				// defensive (R7 already builds in.Side == Trigger.Dir once the
				// direction is set), kept for parity with the normal ISB.
				e.refuse("isbrev_trigger_side_mismatch")
			} else if refuse, _ := nearBoxRefusal(boxes, in.Price, in.Side, e.Cfg.NearBoxRoomMultiple, abs(in.Price-in.Stop)); refuse {
				e.refuse("near_box")
			} else if target = nextLevelBeyondRoom(levels, in.Price, in.Stop, in.Side, e.Cfg.RoomMultiple); target == 0 {
				// Item 26 / R81 (RELEASE #4): the reverse ISB needs a target —
				// Q7 open → the normal ISB target rule: the next level beyond
				// in the trade direction, with the 1:1 floor and the cap.
				e.refuse("isbrev_missing_target")
			} else if !targetFloorOK(in.Price, in.Stop, target) {
				e.refuse("isbrev_target_below_floor")
			} else {
				in.Target = target
				// Confluence is computed before the room check — a confluence
				// reverse ISB's leg 1 is 2R, so its room is 4R.
				confluence := MTFConfluence(in.Side, cb5, cb15)
				capped := CapTargetForDay(in, e.State.Day.Verdict, dg)
				if !targetFloorOK(capped.Price, capped.Stop, capped.Target) {
					e.refuse("isbrev_target_below_floor")
				} else if refuse, _ := roomRefusal(in.Price, in.Stop, target, confluence, e.Cfg.RoomMultiple); refuse {
					// Item 25: the reverse ISB runs the same room rule as the
					// normal ISB. One counter "room". MENTOR-NY item 1: measured
					// to the LEVEL, not the spent-day capped take-profit
					// [D5.3 p1 @09:32–09:50].
					e.refuse("room")
				} else if refuse, _ := spentDayRoomRefusal(in.Price, target, capped.Target, e.State.Day.Verdict, e.Cfg.RoomMultiple); refuse {
					// SPENT day: "target 15 → room 30" [D5.3 p1 @09:32–09:50].
					e.refuse("room")
				} else {
					in.Target = capped.Target
					// B6: the same size flags as the normal ISB — rule 2 (at an
					// old high/low → reduce) and rule 3 (in range → reduce)
					// [D4.1 p1 @04:31–05:04; extended to the reverse ISB by bot inference].
					in.Flag = e.isbFlagsFor(execCur, in.Side, levels, boxes)
					// R85: with the reverse ON, the normal ISB must not arm the
					// counter-trend side on the same candle pair (two opposite
					// stop orders on one candle) — drop any same-pair normal ISB
					// already emitted this tick AND unregister its arm.
					var suppressed []string
					out, suppressed = suppressSamePairCounterISB(out, in, execCur.CloseTime)
					for _, id := range suppressed {
						delete(e.State.ISBArms, id)
						delete(justPlaced, id)
					}
					in.Confluence = confluence
					out = append(out, reverseISBRetag(in, e.Cfg)) // CX-6 knob (default ON = tag PHL/PLH — course-defaults-30)
				}
			}
		}
	}

	// Stacking ticks for live arms (§2.1): bodies staying inside the first ISB.
	// fold #565 (D1) + m406: each arm advances on ITS OWN frame's closed candle —
	// a 2m arm counts 2m candles, a 1m arm counts 1m candles exactly as before,
	// and a 5m arm (the evening-slow leg) counts 5m candles. The pre-m406 code
	// hard-coded 2 here, so a 5m arm ticked on 2m candles and its 4th-candle
	// cancel landed on the wrong candle size [N12, D4.2 p2 @08:21–09:01].
	armBuckets := map[int][]market.Kline{} // closed buckets per arm TF (memoised)
	for id, arm := range e.State.ISBArms {
		if arm.Inside < 0 {
			delete(e.State.ISBArms, id)
			continue
		}
		if justPlaced[id] {
			continue // placed this tick: the ISB candle is Inside 1 already
		}
		tickBar := cur
		if arm.TFMin > 1 {
			buckets, ok := armBuckets[arm.TFMin]
			if !ok {
				buckets = e.closedBucketsTFMemo(bars, arm.TFMin, now)
				armBuckets[arm.TFMin] = buckets
			}
			if len(buckets) == 0 {
				continue // no closed candle yet on this arm's frame
			}
			tickBar = buckets[len(buckets)-1]
			if tickBar.CloseTime <= arm.LastTick {
				continue // the arm's candle has not advanced this tick
			}
		}
		var cancelled bool
		for _, in := range ISBStackTick(&arm, tickBar, e.Cfg) {
			in.ArmID = id
			if in.Action == CancelArm {
				cancelled = true
			}
			out = append(out, in)
		}
		arm.LastTick = tickBar.CloseTime
		if arm.Inside < 0 || cancelled {
			delete(e.State.ISBArms, id) // escaped, or the 4th candle cancelled it
		} else {
			// N12: while the body stays inside I1 the expiry is pushed to the
			// close of the NEXT candle on the arm's OWN frame (the cancels —
			// body outside I1 at once, candle 4 — come from ISBStackTick).
			tf := arm.TFMin
			if tf <= 0 {
				tf = 1
			}
			out = append(out, Intent{Action: ExtendArm, ArmID: id, ExpiryMs: tickBar.CloseTime + int64(tf)*60_000, Reason: "ISB stacking: inside I1 — extend the expiry [N12, D4.2 p2 @ 08:21–09:01]"})
			e.State.ISBArms[id] = arm
		}
	}

	// §2.2: PHL/PLH from a fresh reject touch against an old extreme,
	// gated by trigger side, mid-range and the setup's own gates.
	oldExtremes := oldExtremeIndexes(levels, bars)
	// E2: watch the last emitted EMA stop; block the EMA line on a loss.
	// Item 16 (CTO 23:49Z): watch the line that TRADES — KindEMA34HTF first,
	// the plain KindEMA34 as a fallback.
	emaPrice := 0.0
	for _, lvl := range levels {
		if lvl.Kind == KindEMA34HTF {
			emaPrice = lvl.Price
			break
		}
	}
	if emaPrice == 0 {
		for _, lvl := range levels {
			if lvl.Kind == KindEMA34 {
				emaPrice = lvl.Price
				break
			}
		}
	}
	if emaPrice != 0 {
		emaLossTick(e, emaPrice, cur, now)
	}

	// D4.2-03: while the 15m/5m conflict stands, NO setup trades — PHL/PLH
	// included (the old one-shot veto covered only the ISB path).
	// P2-1 (L7): a conflict drop of a candidate PHL/PLH was silent — name and
	// count it. Release #30: count EVERY rejected level in the tick (the `break`
	// used to count only the first). No behaviour change: the level still does
	// not trade.
	if conflict {
		for _, lvl := range levels {
			tr := e.State.Touches[lvl.Key]
			if tr.Outcome != TouchReject || e.State.ISBOnly[lvl.Key] {
				continue
			}
			e.refuse("phl_tf_conflict")
		}
	}
	if !conflict {
		for _, lvl := range levels {
			tr := e.State.Touches[lvl.Key]
			if tr.Outcome != TouchReject || e.State.ISBOnly[lvl.Key] {
				continue
			}
			// m425a S2: the TRADER refused this touch's intent (DropArm recorded
			// the reference bar) — the touch stays TouchReject but does not
			// re-emit; a NEW touch (new reference bar) is evaluated normally.
			if ref, refused := e.State.RefusedLevelTouch[lvl.Key]; refused && ref == tr.RefBar.CloseTime {
				continue
			}
			// B23 visit cap ("knock knock", D1.3 p1 @10:32–11:43): the first
			// LevelMaxVisits visits of the day trade; the rest refuse. The
			// refusal counts ONCE per visit (CTO 01:19Z, DS-105): a capped
			// rejected level is re-read every tick while it sits in TouchReject,
			// so a bare e.refuse inflated the counter (12,375 refusals for a
			// handful of capped levels) and hid real refusals in the funnel.
			// VisitCapRefused marks the level once; the touch loop clears it on
			// visit departure so a NEW visit re-arms the refusal.
			// r22 P1-2: the cap reads the level's REAL identity (visitKey) and
			// never applies to the EMA lines (visitCapApplies).
			if e.Cfg.LevelMaxVisits > 0 && visitCapAppliesFor(lvl, e.Cfg.CourseExits.EMA9VisitCap) && e.State.Visits[visitKeyFor(lvl, e.Cfg.CourseExits.EMA9VisitCap)] > e.Cfg.LevelMaxVisits {
				if !e.State.VisitCapRefused[lvl.Key] {
					e.refuse("level_visit_cap")
					e.State.VisitCapRefused[lvl.Key] = true
				}
				continue
			}
			// E2 + E4: the EMA34 setup is gated on the loss block and the
			// 30-minute crossing knob. r22 (P2-1): named + counted once per visit.
			if isEMA34(lvl) && !emaSetupAllowed(e, lvl, bars, e.Cfg) {
				if e.State.EmaBlocked {
					e.refuseOncePerVisit(lvl.Key, "phl_ema_loss_block")
				} else {
					e.refuseOncePerVisit(lvl.Key, "phl_ema_crossing_30m")
				}
				continue
			}
			// LOCATION GATE (fold item 1): a PHL/PLH entry level must be a real
			// location — a bare old high/low is not one.
			if !levelIsLocation(lvl, levels) {
				e.refuseOncePerVisit(lvl.Key, "phl_not_location")
				continue
			}
			side, price, ok := RejectEntry(tr, e.Cfg)
			if !ok {
				e.refuseOncePerVisit(lvl.Key, "phl_no_reject_entry")
				continue
			}
			// LocTriggerFilter (CTO 13:20:22Z): false switches the 5m-trigger
			// filter off for LEVEL rejects (the box path honours it separately).
			// CE-9 (release #25, knob level_entries_need_trigger_side, default
			// OFF — the owner ruled school 1): ON binds the trigger side on every
			// LEVEL reject under school 1 too — "Bất kỳ trường hợp nào đi nữa…
			// nằm ở dưới đường buy trigger không được đánh" [D3.4 p1
			// @17:48–18:00]; school 1 then stays at the FTGL/FTGH box only.
			if e.Cfg.LocTriggerFilter && (e.Cfg.TriggerSchool != 1 || e.Cfg.LevelEntriesNeedTriggerSide) {
				if dirOK, trigSide, _ := TriggerVerdict(e.State.Trigger, price); !dirOK || trigSide != "" && trigSide != side {
					if e.Cfg.TriggerSchool == 1 {
						// CE-9: named apart from school 2's stage; once per visit
						// (a rejected level is re-read every tick — r22 P2-1).
						e.refuseOncePerVisit(lvl.Key, "phl_level_trigger_side")
					} else {
						// MENTOR-NY item 5: once per visit of the level, not per tick.
						e.refuseOncePerVisit(lvl.Key, "isb_trigger_side")
					}
					continue
				}
			}
			// The PHL/PLH LOCATION bans (X4 keypair candle, mid-range, mid-range
			// between boxes, the standing 5m/15m/30m ISB box) live in ONE helper
			// shared with the m436 rolling PHL/PLH (phlLocationBan, below): the
			// course bans them for EVERY PHL/PLH, whichever path arms it.
			if name, once := e.phlLocationBan(lvl, side, price, cur, levels, boxes); name != "" {
				if once {
					e.refuseOncePerVisit(lvl.Key, name)
				} else {
					e.refuse(name)
				}
				continue
			}
			// B16 (10-03 ruling, D5.3 p1 @20:40–22:12): ONE intent per reference —
			// while a level order RESTS at this level, the level is not re-emitted
			// (no double size on repeated touches).
			if _, resting := e.State.LevelArms[lvl.Key]; resting {
				e.refuseOncePerVisit(lvl.Key, "phl_level_arm_resting")
				continue
			}
			// B14b: the target is the NEAREST old extreme on the trade side —
			// a failed nearest high is a SKIP, never a farther old high
			// [D2.2 p2 @03:07–03:14].
			side, _, sideOK := RejectEntry(tr, e.Cfg)
			if !sideOK {
				e.refuse("phl_no_reject_entry")
				continue
			}
			// DS-107 patch (20:54:52Z): ONE nearest target-side extreme, picked
			// above the touch candle (long: > RefBar.High / short: < RefBar.Low);
			// a failed nearest is a SKIP, never a farther old extreme.
			tref := tr.RefBar.High
			if side == SideShort {
				tref = tr.RefBar.Low
			}
			ex, found := nearestOldExtremeOnSide(oldExtremes, tref, side)
			if !found {
				e.refuse("phl_no_old_extreme_on_side")
				continue
			}
			// B14b: the NEAREST target-side extreme must sit at least
			// PHLMinCandlesFromExtreme candles from the touch bar — the same
			// "too close" refusal phlPLHR2 raises. Checked here so the near-box
			// rule (next) does not pre-empt this structural skip.
			if len(bars)-1-ex.idx < e.Cfg.PHLMinCandlesFromExtreme {
				e.refuse("phl_too_close_to_extreme")
				continue
			}
			// Day-3 row 24 [D3.2 p1 @ 21:53–23:08]: refuse a setup whose nearest
			// box edge IN the trade direction is closer than NearBoxRoomMultiple ×
			// its own risk; between two boxes (row 25) is exempt. It fires BEFORE
			// the room rule — the room rule's target reads the box edge as the
			// first obstacle, so the same proximity would otherwise surface as
			// phl_room_rule instead of the near_box counter.
			// W11 B11: the PHL/PLH leg of this refusal is our extension of the
			// course's ISB-only example (NearBoxRoomPHL, default ON).
			if phlNearBoxRefuses(e.Cfg, boxes, tref, side, tr.RefBar.High-tr.RefBar.Low) {
				e.refuse("near_box")
				continue
			}
			// D2-28 (CTO fold, release #4): the higher-low / lower-high check
			// reads the STRUCTURAL low the leg to the old high started from —
			// "Đối chiếu với những cái ĐÁY bên tay trái… SHIFT CẤU TRÚC"
			// [D2.2 p3 @02:30–04:18] — taken from the TAPE (the level set is
			// pruned by the significance filter), and it fails CLOSED: no left
			// bars means no check is possible, so the PHL is refused rather than
			// emitted unchecked.
			prior, okLeft := priorLeftExtremeOnTape(bars, ex, oldExtremes)
			if !okLeft {
				e.refuse("phl_no_left_low")
				continue
			}
			// Confluence is computed BEFORE the PHL room check: a confluence PHL's
			// leg 1 is 2R, so its room is RoomMultiple × 2R (4R). C52 W1 B9: the
			// MTF read no longer takes an entry price (no trigger conjunct).
			confluence := MTFConfluence(side, cb5, cb15)
			in, ok, reason := PHLPLHGatedR2Levels(tr, ex.level, ex.idx, len(bars)-1, prior, levels, e.Cfg, e.State.HTF, e.State.Day.Verdict, dg, confluence)
			if !ok {
				// B-rules (13:51:31Z): EVERY drop names a reason and counts it.
				// MENTOR-NY item 5: the room drop counts ONCE per visit of the
				// level (it re-evaluated — and re-counted — every tick the touch
				// stood); every other PHL reason is unchanged.
				if key := phlRefusalKey(reason); key == "room" {
					e.refuseOncePerVisit(lvl.Key, key)
				} else {
					e.refuse(key)
				}
				continue
			}
			// Row 56 (a) [D3.4 p2 @20:51–21:27 (also @21:55–22:10)]: a
			// trigger-retest level trades ONLY in the trigger's direction,
			// whatever the school — a buy-line retest rejecting from below is NOT
			// a short [CTO ruling 2026-10-04].
			if lvl.Kind == KindTriggerRetest && in.Side != e.State.Trigger.Dir {
				e.refuse("trigger_retest_wrong_way")
				continue
			}
			// B6 (10-03 ruling): a LEVEL order RESTS — the one-candle expiry is
			// the ISB rule only [D1.4 p1 @18:32]. ExpiryMs stays 0; the cancel
			// sweep (levelArmCancels) closes it through the level or at its own
			// 15:00 CT expiry (LevelArmExpiry(PlacedAt)).
			// G2 place (CTO R-b): the setup's PLACE — the touch level. For the
			// EMA the anchor is the loss-time price (the stop, K1).
			e.State.ArmSeq++
			id := fmt.Sprintf("lvl-%d", e.State.ArmSeq)
			in.ArmID = id
			in.AnchorKey = lvl.Key
			in.Anchor = lvl.Price
			if lvl.Kind == KindEMA34 || lvl.Kind == KindEMA9 || lvl.Kind == KindEMA34HTF {
				in.Anchor = in.Stop
			}
			// F2: the PHL/PLH was read on the exec frame — stamp it so the
			// runner trails on closed candles of THAT frame ("trade khung nào
			// thì gồng theo khung đó" [X8 @18:02–18:14]); 1m, 2m or 5m (m417).
			in.ExecTFMin = execTFMin
			// D2-49 / D2.4 p1 @01:56: the mode-A runner target goes BEYOND the
			// old high ("resonance breaks the old high 70–80%"). Stamp the next
			// level beyond the old extreme; 0 = none → the runner's native TP
			// is removed (exit = BE stop or EOD flat).
			in.RunnerTarget = nextLevelBeyond(levels, ex.level.Price, side)
			if e.State.LevelArms == nil {
				e.State.LevelArms = map[string]LevelArm{}
			}
			e.State.LevelArms[lvl.Key] = LevelArm{ArmID: id, Side: in.Side, LevelPrice: lvl.Price, PlacedAt: cur.CloseTime, ExecTFMin: execTFMin}
			// B20: a school-1 entry taken without trigger agreement arms the
			// flip upgrade — only once it SURVIVES every later filter (P2-N2,
			// release #23: armed in the Tick defer, never from a refused intent).
			if e.Cfg.TriggerSchool == 1 {
				if ok, ts, _ := TriggerVerdict(e.State.Trigger, in.Price); !ok || ts != in.Side {
					schoolOne = append(schoolOne, schoolOneKeyOf(in))
				}
			}
			if isEMA34(lvl) {
				e.State.EmaPendingSide = in.Side
				e.State.EmaPendingEntry = in.Price
				e.State.EmaPendingStop = in.Stop
				e.State.EmaPendingTarget = in.Target
				// #7: a level ('lvl-') order carries ExpiryMs=0 by design (it
				// rests until LevelArmExpiry); using 0 here made emaLossTick see
				// `now > 0` (always true) and drop the pending as 'expired
				// unfilled' on the very next tick, so E2/CE-6 never engaged.
				// Resolve the same way Limits.pendExpiry does.
				e.State.EmaPendingExpiry = pendExpiry(in, cur.CloseTime)
				e.State.EmaPendingFilled = false
				e.State.EmaPendingArmID = id
			}
			// D4.2-06 part 1: mode C also fires on timeframe agreement —
			// 15m = 5m = entry side AND the 5m trigger agrees [D4.2 p1 @14:57].
			in.Confluence = confluence
			out = append(out, in)
		}
	}

	// B6 cancel sweep: a resting level order dies when a later candle
	// closes through its level, or at its own 15:00 CT expiry
	// (LevelArmExpiry(PlacedAt)).
	out = append(out, e.levelArmCancels(bars, cur, now)...)

	// §4.1 box trades (BOX RULING R1 2026-10-03, ONE path = DS-106's
	// boxEntryIntent — CTO 12:38:50Z): every return visit of every live box is
	// evaluated, not only the first [D3.2 p2 @ 06:25]. The R2 confluence flag
	// rides the emitted intent. BoxRefs dedups: a return is evaluated exactly
	// once, on the tick its reference candle closes. The box edges are OUT of
	// the level touch loop above, so a return emits exactly ONE entry.
	if e.State.BoxRefs == nil {
		e.State.BoxRefs = map[string]int64{}
	}
	boxCfg := e.baseBoxCfg() // C15 hygiene: the configured box frame, same as latchBoxes (was DefaultBoxCfg)
	// D4.2-03: while the 15m/5m conflict stands, NO setup trades — box
	// returns included (the old one-shot veto covered only the ISB path).
	if !conflict {
		for _, b := range boxes {
			// UR-FIX REL9: BoxRefs stores the last evaluated reference's
			// OpenTime (ms), not its bar index. Re-resolve the index against
			// the CURRENT (sliding) window each tick; an anchor that slid out
			// of the window is fail-closed — the last reference is older than
			// every bar in this window, so nothing here was evaluated before
			// and the walk restarts at the formation bar (lastIdx = -1).
			lastMs := e.State.BoxRefs[b.Key]
			lastIdx := -1
			if lastMs != 0 {
				if idx, ok := resolveBarIndexAtOrBefore(bars, lastMs); ok {
					lastIdx = idx
				}
			}
			// Incremental walk from the last evaluated reference (O(new bars) per
			// tick, not O(tape)) — the full BoxReturnBars walk was the 437s replay.
			start := b.FormedAt + 1
			if lastIdx+1 > start {
				start = lastIdx + 1
			}
			for _, r := range BoxReturnBarsFrom(bars, b, start, boxCfg) {
				if r.RefBar <= lastIdx {
					continue
				}
				// Release #30: consume the return FIRST — every return is
				// evaluated exactly once, refused or not. Before this moved
				// below the day gate, a day-off-refused return was re-walked
				// every tick (10-15: 798 box_day_off from 24 returns), and once
				// the ban lifted the stale returns were walked as fresh entries.
				e.State.BoxRefs[b.Key] = bars[r.RefBar].OpenTime
				// A10: the day gate fires and names the blocked setup.
				if r := dayGateRefusal(e.State.Day.Verdict); r != "" {
					e.refuse("box_" + r)
					continue
				}
				// B11 [D3.4 p2 @07:58–08:21]: inside the standing 5m-ISB box only
				// a same-direction ISB trades ("em chỉ đánh inside bar cùng
				// chiều") — box trades never.
				if e.State.ISBBox != nil && bars[r.RefBar].Close > e.State.ISBBox.Low && bars[r.RefBar].Close < e.State.ISBBox.High {
					e.refuse("box_isb_ban")
					continue
				}
				// D4.1-25: the 15m box refuses box returns inside it too.
				if e.State.ISBBox15m != nil && bars[r.RefBar].Close > e.State.ISBBox15m.Low && bars[r.RefBar].Close < e.State.ISBBox15m.High {
					e.refuse("box_isb_ban_15m")
					continue
				}
				// D4.2-07: the 30m box refuses box returns inside it too.
				if e.State.ISBBox30m != nil && bars[r.RefBar].Close > e.State.ISBBox30m.Low && bars[r.RefBar].Close < e.State.ISBBox30m.High {
					e.refuse("box_isb_ban_30m")
					continue
				}
				if !BoxReturnReject(b, bars[r.RefBar]) {
					continue
				}
				// D4.3-04: no box return against the 5m trend — "từ trái qua
				// phải TREND ĐANG TĂNG. [Vẽ] FAILURE TO GO HIGHER SAO ĐÁNH?"
				// [D4.3 @08:53–09:06]. The box's CURRENT role (D14 flip) sets
				// the side: effective FTGH → short (refused in an uptrend),
				// effective FTGL → long (refused in a downtrend).
				// CE-8 (release #25, knob d43_trend_source, default
				// trigger_line): "structure" reads the 5m swing structure
				// instead — "Nhìn từ bên trái qua bên phải… Đỉnh cao, đỉnh thấp
				// hơn, đỉnh thấp hơn — … Giảm" [D4.3 @12:43–12:54].
				trend := e.State.Trigger.Dir
				if e.Cfg.D43TrendSource == D43TrendStructure {
					trend = structureTrend5m(e.closedBucketsMemo(bars, now))
				}
				if trend == SideLong && boxEffectiveKind(b) == FTGH {
					e.refuse("box_against_5m_trend")
					continue
				}
				if trend == SideShort && boxEffectiveKind(b) == FTGL {
					e.refuse("box_against_5m_trend")
					continue
				}
				// C5: name the trigger-side drop that boxEntryIntent also gates.
				// D14 "Uno Reverse": the trigger side is the box's CURRENT role —
				// a flipped FTGH checks the LONG side.
				if e.Cfg.LocTriggerFilter && e.Cfg.TriggerSchool != 1 {
					var tSide Side
					var tPrice float64
					if boxEffectiveKind(b) == FTGL {
						tSide, tPrice = SideLong, bars[r.RefBar].High
					} else {
						tSide, tPrice = SideShort, bars[r.RefBar].Low
					}
					if ok, ts, _ := TriggerVerdict(e.State.Trigger, tPrice); !ok || ts != "" && ts != tSide {
						e.refuse("box_trigger_side")
						continue
					}
				}
				boxIntents, boxWhy := boxEntryIntentWhy(bars[r.RefBar], b, boxes, levels, e.State.Trigger, bars, e.Cfg)
				if boxWhy != "" {
					e.refuse(boxWhy) // m425b S4b: no silent drop inside boxEntryIntent
				}
				for _, in := range boxIntents {
					// B9 [D5.1 p1 @16:24, @19:11–20:07]: box trades obey
					// the same day/HTF gates as every other setup —
					// the 4h/1h direction, the day-off and the spent cap.
					if htfOK, htfSide, _ := HTFVerdict(e.State.HTF); !htfOK {
						e.refuse("box_htf_blocked")
					} else if in.Side != "" && htfSide != "" && in.Side != htfSide {
						e.refuse("box_htf_side_mismatch")
					} else {
						capped := CapTargetForDay(in, e.State.Day.Verdict, dg)
						if capped.Target != in.Target && !targetFloorOK(capped.Price, capped.Stop, capped.Target) {
							// the CAP pulled the target inside the stop distance —
							// refuse rather than emit a sub-floor intent.
							e.refuse("box_target_below_floor")
						} else if refuse, _ := roomRefusal(in.Price, in.Stop, in.Target, in.Confluence, e.Cfg.RoomMultiple); refuse {
							// MENTOR-NY item 1 (supersedes B7/L13): the room rule
							// reads the LEVEL the box trade targets (in.Target,
							// before the spent-day cap), not the capped 15-pt
							// take-profit. B7/L13 measured the capped target, which
							// on a SPENT day demanded stop ≤ 7.5; the course measures
							// the room to the level even when the take-profit is 15
							// — "Dù target của em là 15 điểm … từ điểm entry xuống
							// target nó phải từ 30 điểm đổ lên" [D5.3 p1 @09:32–09:50].
							// The cap and the 1:1 floor on the capped target stand.
							e.refuse("room")
						} else if refuse, _ := spentDayRoomRefusal(in.Price, in.Target, capped.Target, e.State.Day.Verdict, e.Cfg.RoomMultiple); refuse {
							// SPENT day: "target 15 → room 30" [D5.3 p1 @09:32–09:50].
							e.refuse("room")
						} else {
							out = append(out, capped)
							// B20: school-1 box entry without trigger agreement
							// arms the flip upgrade — once it survives the
							// later filters (P2-N2: armed in the Tick defer).
							if e.Cfg.TriggerSchool == 1 {
								if ok, ts, _ := TriggerVerdict(e.State.Trigger, capped.Price); !ok || ts != capped.Side {
									schoolOne = append(schoolOne, schoolOneKeyOf(capped))
								}
							}
						}
					}
				}
			}
		}
	}

	// B20 flip upgrade: a school-1 entry is pending trigger agreement — the
	// 5m trigger has now flipped to that side → confluence (once).
	if e.State.SchoolOneSide != "" && !e.State.SchoolOneUpgraded {
		if ok, ts, _ := TriggerVerdict(e.State.Trigger, cur.Close); ok && ts == e.State.SchoolOneSide {
			out = append(out, Intent{
				Action: ConfluenceUpgrade,
				Side:   e.State.SchoolOneSide,
				Price:  cur.Close,
				Reason: "5m trigger flipped to the school-1 entry side — confluence upgrade (hold >= 1:2, exit C) [D3.4 p3 @10:14–11:05]",
			})
			e.State.SchoolOneUpgraded = true
		}
	}

	// §8 SWING4H (DS-106 slice) on FINAL 5m bars only — the still-forming
	// bucket is excluded (CTO wiring ruling 2026-10-03).
	out = append(out, runSwing(e, bars, now)...)

	// §2.2 D2.2 pullback-continuation: the ROLLING PHL/PLH stop entry (m216
	// scope — the pullback leg only; a drawn-level touch stays first-touch-fixed
	// in the touch loop above). It is gated by the same 15m/5m conflict the
	// level and box paths are.
	if !conflict {
		out = append(out, e.rollingPHLTick(bars, cur, execCur, execTFMin, oldExtremes, levels, boxes, dg)...)
	}

	// BOX RULING part 2: InsideAnyBox — "NEVER trade inside the box" — neither
	// the candle nor the entry point [D3.2 p1 @ 06:59].
	// m425a S1 / m431: an entry the late filters below (or the limits hook, or
	// the deferred leave passes) drop is un-registered by Tick's single exit
	// pass (unregisterDroppedArms).
	out, refused := boxBanFilter(out, boxes, bars[len(bars)-1])
	for _, r := range refused {
		e.refuse(r)
	}

	// D3.3 p1 @08:12–08:56: a box-edge reject and an inside bar on the same
	// closed bar, same side — the mentor takes the inside bar (smaller stop).
	out, refused, noted := phlISBPreferenceFilter(out)
	for _, r := range refused {
		e.refuse(r)
	}
	for _, n := range noted {
		e.note("%s", n)
	}

	// B1 trigger zone (10-03 ruling): between an FTGL and the buy line (mirror:
	// FTGH + sell line) NO entry, ISB included; price must escape both. The
	// swing runs its own machine and is exempt (orbGateFilter pattern).
	out, refused = triggerBoxZoneFilter(out, e.State.Trigger, boxes)
	for _, r := range refused {
		e.refuse(r)
	}

	// ORB gate ("ĐIỀU BẮT BUỘC" [X11 @16:43]): every intraday entry is gated
	// on the opening range; the §8 swing is exempt (orbGateFilter). No ORB for
	// pre-market [X5 @05:42] — before the 08:30 CT RTH open it does not block.
	out, refused = orbGateFilter(out, e.State.ORB, now, e.Cfg)
	for _, r := range refused {
		e.refuse(r)
	}

	// CE-3 (release #25, knob news_wait_both_lines, default ON —
	// course-defaults-30): after a red-folder print, until the 08:30 CT open,
	// an entry waits for the close beyond the 4h trigger line [D4.4 p1
	// @21:55–22:12]. The 30-second ISB of the same lesson [@23:08–23:13] is
	// NOT built: no sub-minute bars exist.
	out, refused = newsWaitFilter(out, e.State.HTF, bars[len(bars)-1].Close, now, e.Cfg)
	for _, r := range refused {
		e.refuse(r)
	}

	// D4.4 p2 @04:41–06:00: at the open (08:30–09:30 CT), a with-4h entry is
	// allowed only after a 5m candle has closed outside the opening 5m range
	// in the 4h's direction — "chuyển về khung 5 phút … nó break được cái rank
	// khung 5 phút". The swing is exempt; off-4h entries stay the HTF gate's.
	out, refused, open5mNotes := open5mRangeFilter(out, e.State.HTF, bars, now)
	for _, r := range refused {
		e.refuse(r)
	}
	for _, n := range open5mNotes {
		e.note("%s", n)
		if n == open5mMissingNote {
			e.count(open5mMissingNote)
		}
	}

	// Seed depth advances from the evaluator's own state as bars arrive, so a
	// source that was short at boot clears without a restart.
	if e.seeded {
		e.advanceDepth(bars, now)
	}
	// P0 fail-closed: seeded with a missing source → no ENTRIES until every
	// source meets its warm-up (cancels still flow — an arm left open must be
	// closable). A short 4h EMA blocks only SWING4H entries (it feeds only the
	// swing line); any other missing source blocks all entries.
	if e.seeded && len(e.missing) > 0 {
		out, refused = scopedFailClosedFilter(out, e.missing)
		for _, r := range refused {
			e.refuse(r)
		}
	}

	// G1/G2 limits hook (DS-107): the leg budget and the loss box apply to
	// intraday entries; the §8 swing runs its own machine and is exempt.
	intraday := out[:0]
	var swings []Intent
	for _, in := range out {
		if strings.HasPrefix(in.Reason, "swing") {
			swings = append(swings, in)
			continue
		}
		intraday = append(intraday, in)
	}
	// REL-5 audit #3: a leg-budget / loss-box drop must not leave a stale
	// ISBArms entry (a stale arm suppresses a legitimate same-side ISB via
	// isbArmActive).
	keptIntraday := e.State.Limits.Apply(intraday, bars[len(bars)-2], bars[len(bars)-1], now, levels, e.Cfg)
	out = append(keptIntraday, swings...)
	// release #31 (leg-3 bracket): stamp the runner's FIRST partial line on
	// every entry intent — the first target line strictly between the 1:1 leg-1
	// exit and the trade target. The trader sends leg2_qty/leg2_tp only when the
	// far-side AddOn proves the leg-3 build AND the runner is ≥ 2 contracts;
	// 0 here = no line in between → the runner rides to the trade target.
	for i := range out {
		in := &out[i]
		if in.Action != PlaceStopEntry && in.Action != PlaceStopLimitEntry {
			continue
		}
		in.RunnerPartialTP = runnerPartialLine(levels, in.Price, in.Stop, in.Target, in.Side)
	}

	return out
}

// oldExtreme holds an old high/low and its index in the 1m history.
type oldExtreme struct {
	level  Level
	idx    int
	isHigh bool // the extreme matches the bar HIGH (swing high) vs the LOW
}

// oldExtremeIndexes maps old-extreme levels to the index of their defining bar.
// The level was drawn at a swing point; the index is the last bar whose high
// (for an old HIGH) or low (for an old LOW) equals the level price — the
// ≥3-candle distance [D4.1 p1 @ 09:40 written] is measured from there.
// r22 P1-4: the role is the level's EXPLICIT Role, never inferred from the
// tape. The old scan took the role from the LAST bar whose high OR low sat
// within 0.26 of the price, so a later bar revisiting an old high relabelled
// it a "low" (10-13 replay: 42 of 269 PHL target picks had the wrong role —
// the SETUPS audit counted 40 of 221 — and those picks were refused as
// not_higher_low / not_lower_high). A level with no role cannot be a PHL/PLH
// extreme (fail closed).
func oldExtremeIndexes(levels []Level, bars []market.Kline) []oldExtreme {
	var out []oldExtreme
	for _, l := range levels {
		if l.Kind != KindOldExtreme || l.Role != OldHigh && l.Role != OldLow {
			continue
		}
		isHigh := l.Role == OldHigh
		idx := -1
		for i := len(bars) - 1; i >= 0; i-- {
			px := bars[i].Low
			if isHigh {
				px = bars[i].High
			}
			if abs(px-l.Price) < 0.26 {
				idx = i
				break
			}
		}
		if idx >= 0 {
			out = append(out, oldExtreme{level: l, idx: idx, isHigh: isHigh})
		}
	}
	return out
}

// oldExtremeRoleFits reports whether an old extreme can serve a trade in the
// given direction: a LONG targets an old HIGH, never an old LOW; a SHORT an old
// LOW, never an old HIGH (r22 P1-4 — "target gần đỉnh cũ", near the OLD HIGH
// for a PHL [D4.1 p1 @07:27]). Every non-old-extreme level fits; an old
// extreme with no recorded role fits too (a hand-built level) — the evaluator
// always stamps the role (swingOldExtremeLevels).
func oldExtremeRoleFits(l Level, side Side) bool {
	if l.Kind != KindOldExtreme || l.Role == "" {
		return true
	}
	return (l.Role == OldHigh) == (side == SideLong)
}

// priorSameRole returns the most recent old extreme BEFORE ex.idx with the
// same role (high vs low) — the prior same-role swing for R2's higher-low /
// lower-high check. 0 = none.
func priorSameRole(ex oldExtreme, extremes []oldExtreme) float64 {
	best := -1
	for _, o := range extremes {
		if o.idx < ex.idx && o.isHigh == ex.isHigh && o.idx > best {
			best = o.idx
		}
	}
	for _, o := range extremes {
		if o.idx == best {
			return o.level.Price
		}
	}
	return 0
}

// priorLeftExtremeOnTape — D2-28 [D2.2 p3 @02:30–04:18], CTO fold: the
// structural extreme the leg to the old high/low started from, read from the
// TAPE. For a long (ex = an old HIGH) it is the lowest Low of the bars after
// the previous same-role old extreme left of ex (else the tape start) up to and
// including ex's own bar; mirrored (highest High) for a short. ok=false when there is
// no bar to read — the caller refuses (fail-closed), never skips the check.
func priorLeftExtremeOnTape(bars []market.Kline, ex oldExtreme, extremes []oldExtreme) (float64, bool) {
	start := 0
	for _, o := range extremes {
		if o.idx < ex.idx && o.isHigh == ex.isHigh && o.idx+1 > start {
			start = o.idx + 1
		}
	}
	// Inclusive of the old-extreme bar itself: when one candle made both the
	// leg's low and the old high, that candle's low IS where the leg started.
	end := ex.idx + 1
	if end > len(bars) {
		end = len(bars)
	}
	if start >= end {
		return 0, false
	}
	v := bars[start].Low
	if !ex.isHigh {
		v = bars[start].High
	}
	for i := start + 1; i < end; i++ {
		if ex.isHigh {
			if bars[i].Low < v {
				v = bars[i].Low
			}
		} else if bars[i].High > v {
			v = bars[i].High
		}
	}
	return v, true
}

// phlRefusalKey names every PHL/PLH drop for the refusal ledger
// (B-rules 13:51:31Z — the E-2 floor, the B14 skip and every gate).
func phlRefusalKey(reason string) string {
	switch {
	case reason == targetCloserThanStopReason:
		return "phl_target_below_floor"
	case strings.HasPrefix(reason, "not a higher low"):
		return "phl_not_higher_low"
	case strings.HasPrefix(reason, "not a lower high"):
		return "phl_not_lower_high"
	case strings.HasPrefix(reason, "too close to the old extreme"):
		return "phl_too_close_to_extreme"
	case strings.HasPrefix(reason, "old extreme not on the target side"):
		return "phl_extreme_wrong_side"
	case strings.HasPrefix(reason, "room:"):
		// The D5.3 room refusal — the reason text is "room: reward …" (the
		// room-rule-d fold renamed the old "room rule:" prefix). Map it to the
		// shared "room" counter the ISB / reverse-ISB / box paths use, so a PHL
		// room drop never falls through to the default "phl_refused".
		return "room"
	case strings.HasPrefix(reason, "stop over the 25-pt ceiling"):
		return "phl_stop_ceiling"
	case strings.HasPrefix(reason, "degenerate stop/target"):
		return "phl_degenerate_geometry"
	case strings.HasPrefix(reason, "HTF direction gate"):
		return "phl_htf_blocked"
	case strings.HasPrefix(reason, "day gate: day_off"):
		return "phl_day_off"
	case strings.HasPrefix(reason, "day gate: day_not_measured"):
		return "phl_day_not_measured"
	case strings.HasPrefix(reason, "spent day: stop over the 15-pt cap"):
		return "phl_spent_stop_cap"
	case strings.HasPrefix(reason, "no reject entry"):
		return "phl_no_reject_entry"
	default:
		return "phl_refused"
	}
}

// nearestOldExtremeOnSide — B14b [D2.2 p2 @03:07–03:14]: the target is the
// NEAREST old extreme on the trade side only; a failed nearest high is a
// SKIP ("mình xác định ra mình không có target"), never a farther old high.
//
// r22 P1-4: only an extreme of the trade's ROLE is a candidate — a long
// targets an old HIGH, a short an old LOW (o.isHigh, which oldExtremeIndexes
// takes from the level's explicit Role).
func nearestOldExtremeOnSide(extremes []oldExtreme, price float64, side Side) (oldExtreme, bool) {
	var best oldExtreme
	found := false
	for _, o := range extremes {
		if o.isHigh != (side == SideLong) {
			continue // a long never targets an old LOW (mirror short)
		}
		if side == SideLong && !(o.level.Price > price) {
			continue
		}
		if side == SideShort && !(o.level.Price < price) {
			continue
		}
		if !found || abs(o.level.Price-price) < abs(best.level.Price-price) {
			best = o
			found = true
		}
	}
	return best, found
}

// dropStaleSwingLineEntries refuses every SWING4H entry while the swing line
// is stale (a feed gap beyond the slice, F-3) — counted as
// swing_line_gap_stale; cancels and every other action pass.
func (e *Evaluator) dropStaleSwingLineEntries(ints []Intent) []Intent {
	if !e.State.Swing.LineStale {
		return ints
	}
	kept := ints[:0]
	for _, in := range ints {
		if in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry {
			e.refuse("swing_line_gap_stale")
			continue
		}
		kept = append(kept, in)
	}
	return kept
}

// runSwing evaluates the §8 4h-EMA34 swing on FINAL 5m bars (the forming
// bucket excluded — CTO wiring ruling 2026-10-03) and returns its intents.
// The swing is NOT gated on the 5m trigger zone by default: §8 is a
// self-contained 4h → 5m procedure and nothing in D5.2 ties it to the 5m
// trigger lines. The knob SwingRespects5mZone (default false) turns the zone
// gate on [C]: not stated in the method (CTO swing ruling, mails
// 1791001124127 / 1791001760445).
func runSwing(e *Evaluator, bars []market.Kline, now int64) []Intent {
	closed := closedBuckets(bars, now, e.Cfg)
	wasStale, prevCount, prevLine := e.State.Swing.LineStale, e.State.Swing.EmaCount, e.State.Swing.Line
	ints := SwingTick(&e.State.Swing, closed, e.Cfg.Swing, now)
	if f := e.State.Swing.Folded; f > 1 && e.State.Swing.EmaCount == prevCount+f {
		e.note("swing 4h EMA34 line folded %d 4h closes after a feed gap: %.2f → %.2f (%d closes) [F-3]", f, prevLine, e.State.Swing.Line, e.State.Swing.EmaCount)
	}
	if e.State.Swing.LineStale && !wasStale {
		e.note("swing 4h EMA34 line STALE: a feed gap longer than the live slice, or an open-session 4h bucket with no bars, hid 4h closes — SWING4H entries refused until a restart reseeds the line (held at %.2f, %d closes) [F-3]", e.State.Swing.Line, e.State.Swing.EmaCount)
	}
	ints = e.dropStaleSwingLineEntries(ints)
	// SWING EXPIRY (CTO 1791008594562): an unfilled swing order lives until the
	// close of the CURRENT 4h candle (ruling 1791008277195) — NOT the 2nd
	// leeway 5m candle. SwingTick still emits its own CancelArm for the leeway.
	for i := range ints {
		if ints[i].Action == PlaceStopEntry {
			ints[i].ExpiryMs = swingExpiry(now)
		}
	}
	// S2: a boot/reload must never turn OLD touches into live orders — drop
	// any swing entry whose reference candle is older than the newest closed
	// 5m bar (00-METHOD.md §8: "Wait for a LITERAL touch" [D5.2 p2 @ 18:36–18:41]).
	ints = dropStaleSwingIntents(ints, closed)
	kept, dropped := swingZoneGate(ints, e.State.Trigger, e.Cfg.Swing.Respects5mZone)
	// C5: the trigger-side drop names its reason (isb_trigger_side).
	for i := 0; i < dropped; i++ {
		e.refuse("isb_trigger_side")
	}
	// Item 25 [D5.3 p1 @09:16] (CTO fold): the swing REJECT obeys the room
	// rule as the course states it — "room to the obstacle ≥ ~2× the target
	// you want" (R68). The swing's target is the 1:1 first (R43, "TARGET 1-1
	// TRƯỚC"), so its obstacle — the 5m EMA34 when it sits on the trade side —
	// must be at least RoomMultiple × risk away. No on-side EMA = no obstacle
	// on record = no room refusal (comparing the 1R TARGET itself against 2R
	// would refuse every swing). One counter "room". The swing ISB
	// (through-close re-entry, stop AT the line "even if it feels big" [table])
	// is exempt — the method's own big-stop rule governs that entry.
	obstacle := swingTargetEMA(closed, e.Cfg.Swing)
	out := make([]Intent, 0, len(kept))
	for _, in := range kept {
		if in.Action == PlaceStopEntry && in.Setup == "SWING4H" && strings.Contains(in.Reason, "reject touch") {
			if swingRoomFilterRefuses(e.Cfg, in, obstacle) { // CX-1: OFF = R68; ON = the EMA is the target
				e.refuse("room")
				continue
			}
		}
		out = append(out, in)
	}
	return out
}

// swingExpiry is the close of the CURRENT 4h candle (session-anchored at
// 17:00 CT): an unfilled swing order lives until then (CTO 1791008594562).
func swingExpiry(now int64) int64 {
	return bucketOpen(now, 240) + 240*60_000 - 1
}

// dropStaleSwingIntents (S2) drops swing entry intents whose reference candle
// (the ArmID's embedded 5m bar time) is older than the newest closed 5m bar.
// The seed stamps Swing.LastBarTime so the first tick never WALKS the old
// bars; this is the second line of defence — even if the watermark is missing,
// an old touch must never become a live order.
func dropStaleSwingIntents(ints []Intent, closed []market.Kline) []Intent {
	if len(closed) == 0 {
		return ints
	}
	newest := closed[len(closed)-1].OpenTime
	out := make([]Intent, 0, len(ints))
	for _, in := range ints {
		if in.Action == PlaceStopEntry && in.Setup == "SWING4H" && in.ArmID != "" {
			if ref, err := strconv.ParseInt(strings.TrimPrefix(in.ArmID, "swing-"), 10, 64); err == nil && ref < newest {
				continue
			}
		}
		out = append(out, in)
	}
	return out
}

// swingZoneGate drops swing intents on the wrong side of the ONE 5m trigger
// line (TriggerVerdict: a buy line allows longs above it only, a sell line
// shorts below it only), but ONLY when the knob is on (respect = true). It
// does NOT test the FTGL-to-buy-line / FTGH-to-sell-line no-trade zone — that
// is triggerBoxZoneVerdict, which the swing path does not call. The old
// "between two opposing trigger lines" wording was a misread: there is one
// line, moved on a reversal [D3.4 p1 @16:38–17:19; B1 10-03 ruling].
func swingZoneGate(ints []Intent, t TriggerLine, respect bool) ([]Intent, int) {
	if !respect {
		return ints, 0
	}
	out := make([]Intent, 0, len(ints))
	dropped := 0
	for _, in := range ints {
		if ok, _, _ := TriggerVerdict(t, in.Price); !ok {
			dropped++ // wrong side of the one trigger line — no trade there
			continue
		}
		out = append(out, in)
	}
	return out, dropped
}

// emaLocationLevel (m327 b) is the location-gate EMA 34 line, from ONE source.
// At EMALocationTFMinutes==1 the line IS the evaluator's own incremental 1m
// EMA 34 (State.EMA34) — the same number the KindEMA34 level carries, never a
// second EMA computed over the tick slice's head (which used to diverge by the
// slice-head seed). At any other TF it is the emaValue over the requested TF's
// closed buckets (EMALocationLevel). It only reads State.EMA34 once the seed
// actually folded a 1m bar into it (EMA34Warmed): a nil-bar / fail-closed seed
// leaves it cold and seededLevels would advance State.EMA34 from 0 into garbage
// — in that case it falls back to EMALocationLevel.
//
// M401: with Exec2mAfter30m ON the line follows the execution-frame schedule
// (emaScheduleTF): the 2m EMA 34 outside 08:30–09:00 CT — State.EMA34x2, the
// same number the KindEMA34 level carries then — and the 1m line inside it.
func (e *Evaluator) emaLocationLevel(bars []market.Kline, cfg Config, now int64) (Level, bool) {
	tf := emaScheduleTF(cfg, cfg.EMALocationTFMinutes, now)
	if tf == 2 && cfg.EMALocationTFMinutes == 1 && e != nil && e.State.EMA2mWarmed && e.State.EMA34x2 > 0 {
		return Level{Key: string(KindEMA34HTF), Kind: KindEMA34HTF, Price: e.State.EMA34x2}, true
	}
	if tf == 2 && cfg.EMALocationTFMinutes == 1 && e != nil && e.seeded && !e.State.EMA2mWarmed {
		tf = 1 // seeded but the 2m line has not formed: the 1m line stands (as the KindEMA34 level does)
	}
	if tf == 1 && cfg.EMALocationTFMinutes == 1 && e != nil && e.State.EMA34Warmed && e.State.EMA34 > 0 {
		return Level{Key: string(KindEMA34HTF), Kind: KindEMA34HTF, Price: e.State.EMA34}, true
	}
	return EMALocationLevelAt(bars, cfg, now)
}

// seededLevels is the Tick level source after Seed: SeedLevels (full stored
// 1H history) + the incremental 1m EMAs + today's old extremes. The seeded set
// is EXTENDED incrementally as new CLOSED candles arrive — never rebuilt from
// the live slice (that is the whole point: the slice is ~2 days and cold).
func (e *Evaluator) seededLevels(bars []market.Kline, now int64) []Level {
	// Incremental 1m EMA 34/9: the EMA recurrence over the closed bars the
	// seed has not seen yet. O(new bars) per tick, not O(all bars).
	k34 := 2.0 / float64(e.Cfg.EMAPeriod34+1)
	k9 := 2.0 / float64(e.Cfg.EMAPeriod9+1)
	// Index-start at the first bar past the watermark: scanning the whole
	// (growing) slice every tick is the O(n^2) replay killer. CloseTime is
	// non-decreasing, so sort.Search is exact.
	i := sort.Search(len(bars), func(i int) bool { return bars[i].CloseTime > e.State.Seed1mWatermark })
	for ; i < len(bars); i++ {
		b := bars[i]
		if b.CloseTime > now {
			break
		}
		e.State.EMA34 += k34 * (b.Close - e.State.EMA34)
		e.State.EMA9 += k9 * (b.Close - e.State.EMA9)
		e.State.Seed1mWatermark = b.CloseTime
		e.depth.oneM++
	}

	// Extend the 1H RTH key-level walk with the candles that closed since the
	// seed watermark (colour-change level at the candle OPEN, prune newest-first).
	// Feed keyLevel1HBars only the tail from the first bar of the next
	// anchored candle: the full-slice walk is O(n) allocs per tick (site 4
	// made it rthHourAnchor/rthMinuteOf per bar) = O(n^2) over the tape.
	// The tail starts on a bucket boundary, so aggregation is identical.
	start := sort.Search(len(bars), func(i int) bool {
		return rthHourAnchor(bars[i].OpenTime) > e.State.Seed1HWatermark
	})
	for _, c := range keyLevel1HBars(bars[start:]) {
		if c.OpenTime <= e.State.Seed1HWatermark || keyLevel1HCandleCloseTime(c.OpenTime) > now {
			continue
		}
		e.State.Seed1HWatermark = c.OpenTime
		e.State.Seed1HBars = append(e.State.Seed1HBars, c)
		// m202 #28: rebuild the kept set from the FULL 1H series (newest-first
		// against the FINAL kept set — keyLevelsFromCandles) instead of the old
		// incremental prune, which dropped a level a fresh restart keeps: a chain
		// A<B<C with |A-B|<prune, |B-C|<prune, |A-C|>=prune kept A on the full
		// walk but dropped it incrementally. Same rule as the seed at seed.go:216.
		e.State.SeedLevels = keyLevelsFromCandles(e.State.Seed1HBars, e.Cfg.KeyLevelPrunePts)
		e.State.Seed1HLastColour = candleColour(c)
	}

	// M401: fold the closed 2m buckets the same incremental way, and read the
	// EMA 9/34 levels on the frame the execution schedule names at `now`.
	ema34, ema9 := e.State.EMA34, e.State.EMA9
	if e.Cfg.Exec2mAfter30m && e.Cfg.EMATFMinutes <= 1 {
		e.foldClosed2mEMA(bars, now)
		if emaScheduleTF(e.Cfg, e.Cfg.EMATFMinutes, now) == 2 && e.State.EMA2mWarmed {
			ema34, ema9 = e.State.EMA34x2, e.State.EMA9x2
		}
	}

	var out []Level
	out = append(out, e.State.SeedLevels...)
	out = append(out, Level{Key: string(KindEMA34), Kind: KindEMA34, Price: ema34})
	out = append(out, Level{Key: string(KindEMA9), Kind: KindEMA9, Price: ema9})
	out = append(out, swingOldExtremeLevels(bars, now)...)
	return withFlatSR(out, bars, e.Cfg, now) // m428 R2
}

// resetEMAVisitsOnFrameSwitch starts a FRESH visit for the EMA 9 / EMA 34 /
// EMA 34 location keys when the execution-frame schedule moves the EMA lines
// between the 1m and the 2m (08:30 and 09:00 CT). Touch, ISB-only and visit-cap
// state is keyed by the CONSTANT level key, so without this a wrong-way first
// touch recorded against the 1m line would invalidate the new 2m line, which sits
// at a different price. Only those three keys are touched; no other level.
func (e *Evaluator) resetEMAVisitsOnFrameSwitch(now int64) {
	if !e.Cfg.Exec2mAfter30m || e.Cfg.EMATFMinutes > 1 {
		return
	}
	frame := emaScheduleTF(e.Cfg, e.Cfg.EMATFMinutes, now)
	prev := e.State.EMAFrame
	e.State.EMAFrame = frame
	if prev == 0 || prev == frame {
		return
	}
	for _, key := range []string{string(KindEMA34), string(KindEMA9), string(KindEMA34HTF)} {
		delete(e.State.Touches, key)
		delete(e.State.ISBOnly, key)
		delete(e.State.VisitCapRefused, key)
		delete(e.State.Visits, key)
		for vk := range e.State.Visits { // EMA 9 per-level mode: "ema9@price"
			if strings.HasPrefix(vk, key+"@") {
				delete(e.State.Visits, vk)
			}
		}
	}
}

// foldClosed2mEMA advances the incremental 2m EMA 34/9 over the 2m buckets that
// CLOSED since Seed2mWatermark (CloseTime < now — the forming bucket is never
// folded). Only a SEEDED 2m EMA advances (see seedHistoryState).
func (e *Evaluator) foldClosed2mEMA(bars []market.Kline, now int64) {
	if !e.State.EMA2mWarmed {
		return // never cold-started off the tick slice: the 1m line stands
	}
	start := sort.Search(len(bars), func(i int) bool { return bars[i].OpenTime > e.State.Seed2mWatermark })
	if start >= len(bars) {
		return
	}
	k34 := 2.0 / float64(e.Cfg.EMAPeriod34+1)
	k9 := 2.0 / float64(e.Cfg.EMAPeriod9+1)
	for _, b := range barsTF(bars[start:], 2) {
		if b.CloseTime >= now {
			break // still forming
		}
		e.State.EMA34x2 += k34 * (b.Close - e.State.EMA34x2)
		e.State.EMA9x2 += k9 * (b.Close - e.State.EMA9x2)
		e.State.Seed2mWatermark = b.CloseTime
	}
}

// advanceKeyLevelDeletion walks the KEY-LEVEL deletion over only the NEW
// closed 1H RTH candles past Seed1HDeletionWatermark (KEYLEVEL-FULL-HISTORY,
// release #10). At Seed the deletion is computed ONCE over the full history;
// from then on each candle is checked exactly once, when it closes. A level is
// deleted when ANY later closed candle's BODY crossed it (open one side, close
// the other — a wick through does NOT delete). The watermark advances with the
// walk, so the steady-state cost is O(new candles x levels), never the old
// O(levels x bars) full-series scan per level per tick.
func (e *Evaluator) advanceKeyLevelDeletion(levels []Level, now int64) {
	b60 := e.State.Seed1HBars
	if n := len(b60); n > 0 && keyLevel1HCandleCloseTime(b60[n-1].OpenTime) > now {
		b60 = b60[:n-1] // the forming candle has not closed
	}
	start := sort.Search(len(b60), func(i int) bool {
		return b60[i].OpenTime > e.State.Seed1HDeletionWatermark
	})
	for i := start; i < len(b60); i++ {
		b := b60[i]
		for _, l := range levels {
			if l.Kind != KindKeyLevel || e.State.DeletedLevels[l.Key] {
				continue
			}
			if b.CloseTime < l.AtTime {
				continue // this 1H candle closed before the level existed
			}
			crossed := b.Open < l.Price && b.Close > l.Price ||
				b.Open > l.Price && b.Close < l.Price
			if crossed {
				e.State.DeletedLevels[l.Key] = true
			}
		}
		e.State.Seed1HDeletionWatermark = b.OpenTime
	}
}

// swingRoomRefused is the swing reject's room rule (item 25 + R68, CTO fold):
// the obstacle — the 5m EMA34 when it sits on the trade side — must be at
// least roomMultiple × risk from the entry. No on-side obstacle = no refusal.
func swingRoomRefused(in Intent, obstacle, roomMultiple float64) bool {
	if obstacle == 0 || !targetOnSide(in.Side, in.Price, obstacle) {
		return false
	}
	// The swing keeps R68: its leg-1 target is "at least 1:1" (R43), so the
	// confluence=false leg-1 distance (1R) is the correct one here.
	refuse, _ := roomRefusal(in.Price, in.Stop, obstacle, false, roomMultiple)
	return refuse
}
