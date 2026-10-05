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
	// BoxRefs records, per live box key, the bar index of the last return
	// visit already evaluated — every return trades (R1, D3.2 p2 @ 06:25),
	// each exactly once.
	BoxRefs map[string]int `json:"box_refs,omitempty"`
	// Limits is the G1 leg budget + G2 loss box state machine (DS-107).
	Limits Limits `json:"limits,omitempty"`
	// Refusals is the B-rules refusal ledger (CTO 13:51:31Z): every filter
	// that DROPS an intent names its reason and counts it, like the replay
	// funnel stages — DS-105 diffs these against the replay.
	Refusals map[string]int `json:"refusals,omitempty"`
	// Trigger is the 5m trigger-line state (§5.1).
	Trigger TriggerLine `json:"trigger"`
	// HTF is the §5.4 4h/1h direction state (DS-106, fold item 3).
	HTF HTF `json:"htf,omitempty"`
	// Day is the §7 pre-session verdict latch (DS-106, fold item 4).
	Day DayLatch `json:"day_latch,omitempty"`
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
	Seed1mWatermark  int64          `json:"seed_1m_watermark,omitempty"`
	EMA34            float64        `json:"ema34,omitempty"` // 1m EMA 34 (incremental)
	EMA9             float64        `json:"ema9,omitempty"`  // 1m EMA 9 (incremental)

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
	// ArmSeq names the next arm.
	ArmSeq int `json:"arm_seq"`
	// LevelArms are the RESTING level orders (B6, 10-03 ruling): a level
	// order has no one-candle expiry — it rests until a later candle closes
	// through its level or the RTH window ends. Keyed by the level key so a
	// resting order is never re-emitted (B16: one intent per reference).
	LevelArms map[string]LevelArm `json:"level_arms,omitempty"`
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
	if s.DeletedLevels == nil {
		s.DeletedLevels = map[string]bool{}
	}
	if s.ISBArms == nil {
		s.ISBArms = map[string]ISBArm{}
	}
	if s.BoxRefs == nil {
		s.BoxRefs = map[string]int{}
	}
	return s, err
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
}

func New(cfg Config) *Evaluator {
	return &Evaluator{Cfg: cfg, State: State{
		Touches:       map[string]Touch{},
		ISBOnly:       map[string]bool{},
		DeletedLevels: map[string]bool{},
		ISBArms:       map[string]ISBArm{},
		BoxRefs:       map[string]int{},
	}}
}

// Levels computes the full mentor level set from the 1m history: colour-change
// key levels (§4.3), EMA 34/9 (§8/§11), and the old highs/lows — the bot's own
// swing levels reused [DS-106 §1: kernel/levels_swing.go SwingPointLevels].
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
	out = append(out, EMALevels(bars, cfg)...)
	for _, d := range kernel.SwingPointLevels(bars, time.UnixMilli(swingPointNow(now))) {
		switch d.Kind {
		case kernel.KindSWGH, kernel.KindSWGL:
			out = append(out, Level{
				Key:   fmt.Sprintf("%s:%.2f", KindOldExtreme, d.Price),
				Kind:  KindOldExtreme,
				Price: d.Price,
			})
		}
	}
	return out
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
			e.State.ISBOnly[lvl.Key] = true
		}
		out = append(out, in)
	}
	return out
}

// rthWindowEndMin is the RTH window end in CT minutes (15:00) — B6 cancels
// resting level orders at the window end.
const rthWindowEndMin = 15 * 60

// LevelArm is one resting level order (B6): the touch level it was placed
// at, its side and its placement candle — the cancel sweep closes it when a
// LATER candle closes through the level or at the RTH window end.
type LevelArm struct {
	ArmID      string  `json:"arm_id"`
	Side       Side    `json:"side"`
	LevelPrice float64 `json:"level_price"`
	PlacedAt   int64   `json:"placed_at"` // the placement candle's CloseTime
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
// CLOSES THROUGH the level ("Minh cancel"), or at the RTH window end
// (15:00 CT). The placement candle itself never cancels.
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

func (e *Evaluator) levelArmCancels(cur market.Kline, now int64) []Intent {
	if len(e.State.LevelArms) == 0 {
		return nil
	}
	_, hh, mm := ctOf(now)
	windowEnd := hh*60+mm >= rthWindowEndMin
	var out []Intent
	for key, arm := range e.State.LevelArms {
		through := false
		if cur.CloseTime > arm.PlacedAt {
			through = arm.Side == SideLong && cur.Close < arm.LevelPrice ||
				arm.Side == SideShort && cur.Close > arm.LevelPrice
		}
		if through || windowEnd {
			out = append(out, Intent{Action: CancelArm, ArmID: arm.ArmID,
				Reason: "level order rest cancelled — a later candle closed through the level, or the window ended [D2.3 p1 @18:01–19:12; recovered @23:48]"})
			delete(e.State.LevelArms, key)
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

// boxBanFilter drops entry intents whose entry price — or whose reference
// candle close — sits INSIDE a box: "NEVER trade inside the box, neither the
// candle nor your entry point" [D3.2 p1 @ 06:59].
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
// (CTO 1791003862333): "between two boxes: NO PHL, NO PLH, only the ISB".
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

// isbFlags returns the ISB size flags for the injector (rule 2: at an old
// high/low; rule 3: in a range), joined with "|" when both apply.
func isbFlags(cur market.Kline, levels []Level, boxes []Box) string {
	var flags []string
	if touchesOldExtreme(cur, levels) {
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
// compulsory size cut [D4.1 p1 written rule 3].
func (e *Evaluator) isbFlagsFor(cur market.Kline, levels []Level, boxes []Box) string {
	flags := isbFlags(cur, levels, boxes)
	if HasFlag(flags, FlagISBInRange) {
		return flags
	}
	if isbInRange(cur.Close, levels, boxes, e.State.ISBBox, e.Cfg.PingPongMinGapPts) {
		if flags != "" {
			flags += "|"
		}
		flags += FlagISBInRange
	}
	return flags
}

// touchesOldExtreme reports whether the candle's range reaches an old high/low
// within ±2 pts (the "isb_at_old_extreme" size flag, written rule 2 D4.1 p1).
func touchesOldExtreme(cur market.Kline, levels []Level) bool {
	for _, l := range levels {
		if l.Kind != KindOldExtreme {
			continue
		}
		if cur.High >= l.Price-2 && cur.Low <= l.Price+2 {
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
func closedBuckets(bars []market.Kline, now int64, cfg Config) []market.Kline {
	b5 := barsTF(bars, 5)
	if len(b5) > 0 && b5[len(b5)-1].CloseTime >= now {
		return b5[:len(b5)-1]
	}
	return b5
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

// roomRefusal is the D5.3 room rule [@09:16] shared by every setup that has a
// target: the room to target (|target−entry|) must be at least roomMultiple ×
// the risk (|entry−stop|). roomMultiple <= 0 disables the check. One counter
// "room" for every path (ISB, reverse ISB, swing reject, PHL/PLH). The same
// 2R arithmetic the near-box rule (row 24) applies to box-edge distance.
func roomRefusal(price, stop, target, roomMultiple float64) (refuse bool, why string) {
	if roomMultiple <= 0 {
		return false, ""
	}
	risk := abs(stop - price)
	reward := abs(target - price)
	if risk <= 0 || reward <= 0 {
		return true, "degenerate stop/target geometry"
	}
	if reward < roomMultiple*risk {
		return true, fmt.Sprintf("room rule: reward %.2f pts < %.2f pts (%.2fx risk) [D5.3 p1 @ 09:16]", reward, roomMultiple*risk, roomMultiple)
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

func nextLevelBeyond(levels []Level, price float64, side Side) float64 {
	best := 0.0
	for _, l := range levels {
		if l.Kind == KindTrendline {
			continue // a trendline is a location, never a target (X9)
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
	// A5 + P0 sizing gap (CTO 20:13:25Z): ONE stamp where intents LEAVE Tick —
	// the geometry (StopPts/TargetPts), the spent-day flag, and the
	// untagged-setup drop. The defer covers every return path, including the
	// early ISB missing-target return.
	defer func() {
		if out == nil {
			return
		}
		kept, refusals := stampLeave(out, e.State.Day.Verdict)
		for _, r := range refusals {
			e.refuse(r)
		}
		out = kept
		stampHTFAgree(out, e.State.HTF)
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
	var b60 []market.Kline
	if e.seeded {
		b60 = e.State.Seed1HBars // incremental (seed + ticks), O(new) per tick
	} else {
		b60 = keyLevel1HBars(bars) // cold: one aggregation per tick
	}
	for _, l := range levels {
		if l.Kind != KindKeyLevel || e.State.DeletedLevels[l.Key] {
			continue
		}
		if levelDeletedBy1HBody(l, b60, now) {
			e.State.DeletedLevels[l.Key] = true
		}
	}
	levels = withoutDeleted(levels, e.State.DeletedLevels)

	// 5m trigger line advances every 1m close (aggregated 5m bars).
	e.State.Trigger = TriggerTick(e.State.Trigger, barsTF(bars, 5), 5, e.Cfg)

	// §5.4 HTF direction (DS-106, fold item 3): the 4h/1h lines advance with
	// the same bar history. Item 18 part 1: a red-folder 07:30 print candle
	// must not move the 1h/4h trigger lines [D4.4 p1 @18:13, @22:15] — the
	// day's print windows (plumbed from the trader's calendar) are dropped
	// from the HTF feed. Empty windows = no print today = byte-identical.
	htfBars := htfFeedBars(bars, e.Cfg.PrintWindows)
	e.State.HTF = HTFAdvance(e.State.HTF, barsTF(htfBars, 240), barsTF(htfBars, 60), e.Cfg)

	// §7 day gate (fold item 4): Globex run → per-trading-day latch (L1/L2).
	dg := DayGate{SpentPts: e.Cfg.DayGateSpentPts, TargetCapPts: e.Cfg.DayGateTargetCapPts}
	run, haveRun := GlobexRun(bars, now, ctime())
	e.State.Day = LatchDay(e.State.Day, now, ctime(), run, haveRun, HTFConflict(e.State.HTF), dg)
	// B23: the visit counters belong to the latched trading day — a new day
	// starts every level's cap over.
	if e.State.Day.Key != e.State.VisitsDay {
		e.State.Visits = map[string]int{}
		e.State.VisitsDay = e.State.Day.Key
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
	// D4.4-15: the 4h/1h trigger lines join the TARGET LADDER (never the
	// location set) — a trade's target can be the higher-TF trigger line when
	// it sits between the entry and the next level [D4.4 p2 @05:04–06:02
	// "TARGET MÌNH VỀ LẠI 4 GIỜ nè anh chị… CÁI LỆNH TRĂM ĐIỂM của mình LÀ VỀ
	// ĐÂY"]. Target-only: the touch loop skips KindHTFTrigger and
	// levelIsLocation returns false for it.
	if e.State.HTF.FourH.Dir != "" && e.State.HTF.FourH.Price != 0 {
		levels = append(levels, Level{Key: "htf_4h_trigger", Kind: KindHTFTrigger, Price: e.State.HTF.FourH.Price})
	}
	if e.State.HTF.OneH.Dir != "" && e.State.HTF.OneH.Price != 0 {
		levels = append(levels, Level{Key: "htf_1h_trigger", Kind: KindHTFTrigger, Price: e.State.HTF.OneH.Price})
	}
	// D4.3 wick microscalp (advanced, knob OFF by default): 2+ consecutive 5m
	// candles rejecting with same-way wicks put a bounded target at their far
	// wick — target-only (the touch loop skips KindWickMicroscalp).
	if e.Cfg.WickMicroscalpEnabled {
		if wl, ok := wickMicroscalpLevel(closedBuckets(bars, now, e.Cfg), e.State.Trigger.Dir); ok {
			levels = append(levels, wl)
		}
	}
	if el, ok := EMALocationLevel(bars, e.Cfg); ok {
		levels = append(levels, el)
	}

	// §7 step 0: the ORB gate advances every tick (drawn at 08:32 CT,
	// escape latched on the first 1m body close outside).
	e.State.ORB = ORBAdvance(e.State.ORB, bars, now)

	// §4.1 FTGH/FTGL boxes (BOX RULING part 2): built per tick; the edges
	// join the level set as locations (used again and again) and the box
	// interior bans entries.
	boxes := BoxesBuild(bars, e.Cfg.Box, time.UnixMilli(now))
	levels = append(levels, BoxEdgeLocations(boxes)...)

	// X9 (slide 27) + DAY-3 row 27: trendlines as LOCATIONS — two same-role
	// swings joined, valid only after the 3rd touch, discarded by a 5m close
	// through. They join the level set location-only (never a target); a
	// trendline next to a box edge is suppressed (box beats trendline).
	trendlines := TrendlinesBuild(bars, time.UnixMilli(now))
	levels = append(levels, TrendlineLevels(trendlines, boxes, bars)...)

	// §3: first-touch classification per level. BOX edges are OUT of this
	// loop (BOX PATH DECISION, CTO 12:38:50Z): the box path is DS-106's
	// boxEntryIntent — the level path treating each edge as a separate line
	// would double-trade a box return (a candle inside the box can look like
	// an approach from above). The edges stay in `levels` for location /
	// InsideAnyBox / boxBanFilter / midRangeBoxed checks.
	for _, lvl := range levels {
		if lvl.Kind == KindFTGHEdge || lvl.Kind == KindFTGLEdge || lvl.Kind == KindHTFTrigger || lvl.Kind == KindWickMicroscalp {
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
		intents := visitTick(&tr, lvl, bars[len(bars)-2], bars[len(bars)-1], e.Cfg)
		if !wasNone && tr.Outcome == TouchNone && moving {
			// visit departure: the touching candle is gone — clear the ISB-only
			// state scoped to that visit. This is the ONLY reset for a moving
			// line; drift alone keeps the classification (item 22 fix).
			delete(e.State.ISBOnly, lvl.Key)
		}
		e.State.Touches[lvl.Key] = tr
		if e.State.ISBOnly[lvl.Key] {
			continue // still invalid (no drift, no departure this tick)
		}
		// B23: each NEW reject classification is one visit of that level
		// today (a revisit can only classify after the departure reset, so
		// None -> Reject is exactly one visit).
		if wasNone && tr.Outcome == TouchReject {
			if e.State.Visits == nil {
				e.State.Visits = map[string]int{}
			}
			e.State.Visits[lvl.Key]++
		}
		out = append(out, handleTouchIntents(e, lvl, intents)...)
	}

	// §2.1: ISB on the last closed pair. R1: the direction is the CANDLE-1
	// colour, not the trigger line (RULES-FIX-v3).
	prev, cur := bars[len(bars)-2], bars[len(bars)-1]

	// arms placed on THIS tick are skipped by the stacking loop below.
	justPlaced := map[string]bool{}

	// R5 (RULES-FIX-v3): the 5m ISB rest box. While it stands it replaces the
	// trigger line for gating; nothing trades inside it except a same-direction
	// 1m ISB, and it is deleted when a 1m BODY closes outside [D3.4 p2 @
	// 00:14–13:00; D4.1 p1 @ 21:38].
	if e.State.ISBBox != nil {
		if escaped, _ := ISBBoxEscape(*e.State.ISBBox, cur); escaped {
			e.State.ISBBox = nil
		}
	} else if cb := closedBuckets(bars, now, e.Cfg); len(cb) >= 2 {
		// One box per 5m ISB pair: an escaped box is gone for good [D3.4 p2
		// @ 12:02], so the same pair never builds it again.
		if bx, ok := ISBBoxFrom5m(cb[len(cb)-2], cb[len(cb)-1]); ok && bx.AtTime != e.State.LastISBBoxAt {
			e.State.ISBBox = &bx
			e.State.LastISBBoxAt = bx.AtTime
		}
	}

	// DS-103 item 4 (D4.2-06): the 15m/5m conflict and the MTF-alignment
	// mode-C read use the SAME CLOSED buckets — computed once per tick.
	cb5 := closedBuckets(bars, now, e.Cfg)
	cb15 := closedBucketsTF(bars, 15, now)
	cb30 := closedBucketsTF(bars, 30, now)

	// D4.1-25: the 15m ISB rest box — box the latest CLOSED 15m inside-bar
	// candle (the REAL 15m TF, B8) and keep it until a 1m BODY closes outside.
	// While it stands, entries may only go its direction and PHL/PLH + box
	// returns are refused inside it [D4.1 p2 @07:35–08:11; D4.2 p1 @01:06,
	// 04:49].
	if e.State.ISBBox15m != nil {
		if escaped, _ := ISBBoxEscape(*e.State.ISBBox15m, cur); escaped {
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
		if escaped, _ := ISBBoxEscape(*e.State.ISBBox30m, cur); escaped {
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
	conflict := mtfConflict(e.State.ISBBox, e.State.ISBBox15m)
	if conflict {
		out = append(out, e.conflictArmCancels()...)
	}

	if !conflict && IsISB(prev, cur) {
		if dirOK, trigSide, _ := TriggerVerdict(e.State.Trigger, cur.Close); dirOK {
			// The conflict reads the standing boxes above (D4.2-03); the
			// still-forming 5m bucket is dropped [D4.2 p1 @ 05:10: "a
			// 15-minute candle is only confirmed once CLOSED; trade from
			// the next one"]. B8: the 15m side is the REAL 15m TF.
			{
				// OWNER RULING 2026-10-03 ("exactly like he said"): the ISB is
				// NOT location-gated — "inside bar lúc nào cũng có thể take
				// risk… trong range, trên range, ngoài range, dưới range"
				// [D4.1 p1 @ 05:15]. His conditions stay: the trigger zone
				// (above), the R5 box (below), the twenties skip, and a size
				// flag at an old high/low.
				//
				// fold item 3: entries only with the 4h trigger direction
				// (1h agreeing or silent); fold item 4: a DayOff shuts the
				// machine off for the day.
				if htfOK, htfSide, _ := HTFVerdict(e.State.HTF); !htfOK {
					e.refuse("isb_htf_blocked")
				} else {
					// R1 (RULES-FIX-v3): the order is a STOP-LIMIT in the
					// CANDLE-1 colour direction (the only skip: a stop in
					// the twenties) [D1.4 p1 @ 09:20–10:20, 24:41–24:55].
					// R5: while the box stands nothing trades inside it except a
					// SAME-direction 1m ISB — compute the verdict up front so an
					// allowed ISB still falls through to the emit below.
					boxBlocked := false
					if e.State.ISBBox != nil && !crossingISBs(cb5, 3) {
						if allowed, r := ISBBoxAllows(*e.State.ISBBox, prev, cur); !allowed {
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
						if allowed, r := ISBBoxAllows(*e.State.ISBBox15m, prev, cur); !allowed {
							box15Blocked, _ = true, r
						}
					}
					// D4.2-07: the 30m box gates the ISB the same way — never
					// trade against a 30m ISB inside its range; skipped when the
					// 30m ISBs are crossing.
					box30Blocked := false
					if e.State.ISBBox30m != nil && !crossingISBs(cb30, 3) {
						if allowed, r := ISBBoxAllows(*e.State.ISBBox30m, prev, cur); !allowed {
							box30Blocked, _ = true, r
						}
					}
					side, chosen, ok, _ := ISBStopLimitOrder(prev, cur, e.Cfg)
					if !ok {
						e.refuse("isb_stop_twenties")
					} else if isbArmActive(e.State.ISBArms, side) {
						// ONE ARM PER SIDE (CTO parity ruling 1791008332386 #1):
						// stacking extends the EXISTING arm — no new arm.
					} else if boxBlocked {
						// R5: an opposite-direction ISB inside the box — no entry
						e.refuse("isb_box_blocked")
					} else if box15Blocked {
						// D4.1-25: an ISB against the 15m box direction — no entry
						e.refuse("isb_box_blocked_15m")
					} else if box30Blocked {
						// D4.2-07: an ISB against the 30m box direction — no entry
						e.refuse("isb_box_blocked_30m")
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
						target := nextLevelBeyond(levels, chosen.Price, side)
						if target == 0 {
							e.refuse("isb_missing_target")
						} else if !targetFloorOK(chosen.Price, chosen.Stop, target) {
							e.refuse("isb_target_below_floor")
						} else {
							chosen.Target = target
							// B9 [D5.1 p1 @16:24, @19:11-20:07]: ISBs obey the spent-day
							// cap too ("15 điểm bán, 10 điểm bán"). A cap that breaks the
							// 1:1 floor refuses (same reason as an uncapped short target).
							capped := CapTargetForDay(chosen, e.State.Day.Verdict, dg)
							if !targetFloorOK(capped.Price, capped.Stop, capped.Target) {
								e.refuse("isb_target_below_floor")
							} else if refuse, _ := roomRefusal(capped.Price, capped.Stop, capped.Target, e.Cfg.RoomMultiple); refuse {
								// Item 25 [D5.3 p1 @09:16]: the ISB now obeys the
								// room rule too — reward to the (capped) target must
								// be at least RoomMultiple × risk. One counter "room".
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
								// @ 08:05/09:40]) — the range is the same mid-range test as
								// the PHL/PLH ban.
								chosen.Flag = e.isbFlagsFor(cur, levels, boxes)
								// N12: a single ISB fills by the close of the NEXT 1m candle
								// or it is cancelled ("cancel if the next candle does not
								// fill" [D1.4 p1 @ 18:32–18:45]); stacking extends it below.
								chosen.ExpiryMs = cur.CloseTime + 60_000
								e.State.ArmSeq++
								id := fmt.Sprintf("isb-%d", e.State.ArmSeq)
								// the ISB candle is the 1st inside candle (Inside=1), so the
								// stacking loop must skip this arm on the placement bar.
								e.State.ISBArms[id] = ISBArm{FirstBar: cur, Inside: 0, Side: side}
								justPlaced[id] = true
								chosen.ArmID = id
								// D4.2-06 part 1: mode C also fires on timeframe
								// agreement — 15m = 5m = entry side AND the 5m
								// trigger agrees [D4.2 p1 @14:57].
								chosen.Confluence = MTFConfluence(side, e.State.Trigger, chosen.Price, cb5, cb15)
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
	// trades WITH the trend [D5.4].
	if e.Cfg.ISBReverseEMA9Enabled && IsISB(prev, cur) {
		if r := dayGateRefusal(e.State.Day.Verdict); r != "" {
			e.refuse("isbrev_" + r)
		} else if in, ok, _ := ReverseISBAtEMA9(prev, cur, emaValue(bars, e.Cfg.EMAPeriod9), e.State.Trigger.Dir, e.Cfg); ok {
			// N12: an R7 reverse ISB fills by the close of the NEXT 1m candle
			// (the R1 family rule).
			in.ExpiryMs = cur.CloseTime + 60_000
			// R85 (CTO, release #4): now that the reverse ISB can place, it
			// runs through the SAME gates as the normal ISB — the twenties
			// stop skip [D4.1 p1 @ 05:41], the 4h HTF verdict and side, and
			// the near-box rule (row 24) — before any target is set.
			htfOK, htfSide, _ := HTFVerdict(e.State.HTF)
			target := 0.0
			if _, stopOK, _ := ISBStopVerdict(cur, e.Cfg); !stopOK {
				e.refuse("isbrev_stop_twenties")
			} else if !htfOK {
				e.refuse("isbrev_htf_blocked")
			} else if htfSide != "" && in.Side != htfSide {
				e.refuse("isbrev_htf_side_mismatch")
			} else if refuse, _ := nearBoxRefusal(boxes, in.Price, in.Side, e.Cfg.NearBoxRoomMultiple, abs(in.Price-in.Stop)); refuse {
				e.refuse("near_box")
			} else if target = nextLevelBeyond(levels, in.Price, in.Side); target == 0 {
				// Item 26 / R81 (RELEASE #4): the reverse ISB needs a target —
				// Q7 open → the normal ISB target rule: the next level beyond
				// in the trade direction, with the 1:1 floor and the cap.
				e.refuse("isbrev_missing_target")
			} else if !targetFloorOK(in.Price, in.Stop, target) {
				e.refuse("isbrev_target_below_floor")
			} else {
				in.Target = target
				capped := CapTargetForDay(in, e.State.Day.Verdict, dg)
				if !targetFloorOK(capped.Price, capped.Stop, capped.Target) {
					e.refuse("isbrev_target_below_floor")
				} else if refuse, _ := roomRefusal(capped.Price, capped.Stop, capped.Target, e.Cfg.RoomMultiple); refuse {
					// Item 25: the reverse ISB runs the same room rule as the
					// normal ISB. One counter "room".
					e.refuse("room")
				} else {
					in.Target = capped.Target
					// R85: with the reverse ON, the normal ISB must not arm the
					// counter-trend side on the same candle pair (two opposite
					// stop orders on one candle) — drop any same-pair normal ISB
					// already emitted this tick AND unregister its arm.
					var suppressed []string
					out, suppressed = suppressSamePairCounterISB(out, in, cur.CloseTime)
					for _, id := range suppressed {
						delete(e.State.ISBArms, id)
						delete(justPlaced, id)
					}
					out = append(out, in)
				}
			}
		}
	}

	// Stacking ticks for live arms (§2.1): bodies staying inside the first ISB.
	for id, arm := range e.State.ISBArms {
		if arm.Inside < 0 {
			delete(e.State.ISBArms, id)
			continue
		}
		if justPlaced[id] {
			continue // placed this tick: the ISB candle is Inside 1 already
		}
		var cancelled bool
		for _, in := range ISBStackTick(&arm, cur, e.Cfg) {
			in.ArmID = id
			if in.Action == CancelArm {
				cancelled = true
			}
			out = append(out, in)
		}
		if arm.Inside < 0 || cancelled {
			delete(e.State.ISBArms, id) // escaped, or the 4th candle cancelled it
		} else {
			// N12: while the body stays inside I1 the expiry is pushed to the
			// close of the NEXT 1m candle (the cancels — body outside I1 at
			// once, candle 4 — come from ISBStackTick).
			out = append(out, Intent{Action: ExtendArm, ArmID: id, ExpiryMs: cur.CloseTime + 60_000, Reason: "ISB stacking: inside I1 — extend the expiry [N12, D4.2 p2 @ 08:21–09:01]"})
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
	if !conflict {
		for _, lvl := range levels {
			tr := e.State.Touches[lvl.Key]
			if tr.Outcome != TouchReject || e.State.ISBOnly[lvl.Key] {
				continue
			}
			// B23 visit cap ("knock knock", D1.3 p1 @10:32–11:43): the first
			// LevelMaxVisits visits of the day trade; the rest refuse.
			if e.Cfg.LevelMaxVisits > 0 && e.State.Visits[lvl.Key] > e.Cfg.LevelMaxVisits {
				e.refuse("level_visit_cap")
				continue
			}
			// E2 + E4: the EMA34 setup is gated on the loss block and the
			// 30-minute crossing knob.
			if isEMA34(lvl) && !emaSetupAllowed(e, lvl, bars, e.Cfg) {
				continue
			}
			// LOCATION GATE (fold item 1): a PHL/PLH entry level must be a real
			// location — a bare old high/low is not one.
			if !levelIsLocation(lvl, levels) {
				continue
			}
			side, price, ok := RejectEntry(tr, e.Cfg)
			if !ok {
				continue
			}
			// LocTriggerFilter (CTO 13:20:22Z): false switches the 5m-trigger
			// filter off for LEVEL rejects (the box path honours it separately).
			if e.Cfg.LocTriggerFilter && e.Cfg.TriggerSchool != 1 {
				if dirOK, trigSide, _ := TriggerVerdict(e.State.Trigger, price); !dirOK || trigSide != "" && trigSide != side {
					e.refuse("isb_trigger_side")
					continue
				}
			}
			// B21 X4 (@06:31–08:52): the gap between the touched level and the
			// next level must exceed the largest 1m candle of the last 30 closed
			// bars — a candle as big as the rank of the two levels: sit out. The
			// scan is strict (beyond the touched level, excluding the level itself).
			if e.Cfg.PingPongCandleMaxPts > 0 {
				var next float64
				for _, l2 := range levels {
					// The pair is the touched level and the next KEY level he
					// drew — EMA lines, trigger retests, box edges and bare tape
					// swings are not part of the rank between two levels (X4).
					if l2.Key == lvl.Key || l2.Kind != KindKeyLevel {
						continue
					}
					if side == SideLong && l2.Price > lvl.Price && (next == 0 || l2.Price < next) {
						next = l2.Price
					} else if side == SideShort && l2.Price < lvl.Price && (next == 0 || l2.Price > next) {
						next = l2.Price
					}
				}
				if next != 0 && abs(next-lvl.Price) <= largestCandlePts(bars, e.Cfg.PingPongCandleLookback) {
					e.refuse("keypair_candle_too_big")
					continue
				}
			}
			if allowed, _ := SetupPermittedVerdict("PHL", levels, price, e.Cfg); !allowed {
				continue
			}
			// MID-RANGE ban via boxes (CTO 1791003862333): between an FTGL
			// below and an FTGH above there is NO PHL, NO PLH, regardless of
			// width — only the ISB.
			if midRangeBoxed(boxes, price) {
				continue
			}
			// R5: nothing trades inside the standing 5m-ISB box except a
			// same-direction 1m ISB — PHL/PLH never.
			if e.State.ISBBox != nil && cur.Close > e.State.ISBBox.Low && cur.Close < e.State.ISBBox.High {
				continue
			}
			// D4.1-25: the 15m box refuses PHL/PLH inside it too ("đợi nó thoát
			// khỏi range khung 15 phút rồi mới trade").
			if e.State.ISBBox15m != nil && cur.Close > e.State.ISBBox15m.Low && cur.Close < e.State.ISBBox15m.High {
				e.refuse("phl_15m_box_ban")
				continue
			}
			// D4.2-07: the 30m box refuses PHL/PLH inside it too.
			if e.State.ISBBox30m != nil && cur.Close > e.State.ISBBox30m.Low && cur.Close < e.State.ISBBox30m.High {
				e.refuse("phl_30m_box_ban")
				continue
			}
			// B16 (10-03 ruling, D5.3 p1 @20:40–22:12): ONE intent per reference —
			// while a level order RESTS at this level, the level is not re-emitted
			// (no double size on repeated touches).
			if _, resting := e.State.LevelArms[lvl.Key]; resting {
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
			if refuse, _ := nearBoxRefusal(boxes, tref, side, e.Cfg.NearBoxRoomMultiple, tr.RefBar.High-tr.RefBar.Low); refuse {
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
			in, ok, reason := PHLPLHGatedR2Levels(tr, ex.level, ex.idx, len(bars)-1, prior, levels, e.Cfg, e.State.HTF, e.State.Day.Verdict, dg)
			if !ok {
				// B-rules (13:51:31Z): EVERY drop names a reason and counts it.
				e.refuse(phlRefusalKey(reason))
				continue
			}
			// Row 56 (a) [D3.2 p1 @ 21:53–23:08]: a trigger-retest level trades
			// ONLY in the trigger's direction, whatever the school — a buy-line
			// retest rejecting from below is NOT a short [CTO ruling 2026-10-04].
			if lvl.Kind == KindTriggerRetest && in.Side != e.State.Trigger.Dir {
				e.refuse("trigger_retest_wrong_way")
				continue
			}
			// B6 (10-03 ruling): a LEVEL order RESTS — the one-candle expiry is
			// the ISB rule only [D1.4 p1 @18:32]. ExpiryMs stays 0; the cancel
			// sweep (levelArmCancels) closes it through the level or at the
			// window end.
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
			// D2-49 / D2.4 p1 @01:56: the mode-A runner target goes BEYOND the
			// old high ("resonance breaks the old high 70–80%"). Stamp the next
			// level beyond the old extreme; 0 = none → the runner's native TP
			// is removed (exit = BE stop or EOD flat).
			in.RunnerTarget = nextLevelBeyond(levels, ex.level.Price, side)
			if e.State.LevelArms == nil {
				e.State.LevelArms = map[string]LevelArm{}
			}
			e.State.LevelArms[lvl.Key] = LevelArm{ArmID: id, Side: in.Side, LevelPrice: lvl.Price, PlacedAt: cur.CloseTime}
			// B20: a school-1 entry taken without trigger agreement arms the
			// flip upgrade.
			if e.Cfg.TriggerSchool == 1 {
				if ok, ts, _ := TriggerVerdict(e.State.Trigger, in.Price); !ok || ts != in.Side {
					e.State.SchoolOneSide = in.Side
					e.State.SchoolOneUpgraded = false
				}
			}
			if isEMA34(lvl) {
				e.State.EmaPendingSide = in.Side
				e.State.EmaPendingEntry = in.Price
				e.State.EmaPendingStop = in.Stop
				e.State.EmaPendingTarget = in.Target
				e.State.EmaPendingExpiry = in.ExpiryMs
				e.State.EmaPendingFilled = false
			}
			// D4.2-06 part 1: mode C also fires on timeframe agreement —
			// 15m = 5m = entry side AND the 5m trigger agrees [D4.2 p1 @14:57].
			in.Confluence = MTFConfluence(in.Side, e.State.Trigger, in.Price, cb5, cb15)
			out = append(out, in)
		}
	}

	// B6 cancel sweep: a resting level order dies when a later candle
	// closes through its level, or at the RTH window end.
	out = append(out, e.levelArmCancels(cur, now)...)

	// §4.1 box trades (BOX RULING R1 2026-10-03, ONE path = DS-106's
	// boxEntryIntent — CTO 12:38:50Z): every return visit of every live box is
	// evaluated, not only the first [D3.2 p2 @ 06:25]. The R2 confluence flag
	// rides the emitted intent. BoxRefs dedups: a return is evaluated exactly
	// once, on the tick its reference candle closes. The box edges are OUT of
	// the level touch loop above, so a return emits exactly ONE entry.
	if e.State.BoxRefs == nil {
		e.State.BoxRefs = map[string]int{}
	}
	boxCfg := DefaultBoxCfg()
	// D4.2-03: while the 15m/5m conflict stands, NO setup trades — box
	// returns included (the old one-shot veto covered only the ISB path).
	if !conflict {
		for _, b := range boxes {
			last := e.State.BoxRefs[b.Key]
			// Incremental walk from the last evaluated reference (O(new bars) per
			// tick, not O(tape)) — the full BoxReturnBars walk was the 437s replay.
			start := b.FormedAt + 1
			if last+1 > start {
				start = last + 1
			}
			for _, r := range BoxReturnBarsFrom(bars, b, start, boxCfg) {
				if r.RefBar <= last {
					continue
				}
				// A10: the day gate fires BEFORE any box state is recorded
				// (BoxRefs), and names the blocked setup.
				if r := dayGateRefusal(e.State.Day.Verdict); r != "" {
					e.refuse("box_" + r)
					continue
				}
				e.State.BoxRefs[b.Key] = r.RefBar
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
						e.refuse("isb_trigger_side")
						continue
					}
				}
				for _, in := range boxEntryIntent(bars[r.RefBar], b, boxes, levels, e.State.Trigger, bars, e.Cfg) {
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
						} else {
							out = append(out, capped)
							// B20: school-1 box entry without trigger agreement
							// arms the flip upgrade.
							if e.Cfg.TriggerSchool == 1 {
								if ok, ts, _ := TriggerVerdict(e.State.Trigger, capped.Price); !ok || ts != capped.Side {
									e.State.SchoolOneSide = capped.Side
									e.State.SchoolOneUpgraded = false
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

	// BOX RULING part 2: InsideAnyBox — "NEVER trade inside the box" — neither
	// the candle nor the entry point [D3.2 p1 @ 06:59].
	out, refused := boxBanFilter(out, boxes, bars[len(bars)-1])
	for _, r := range refused {
		e.refuse(r)
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
	out = append(e.State.Limits.Apply(intraday, bars[len(bars)-2], bars[len(bars)-1], now, levels, e.Cfg), swings...)

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
// (low) equals the level price — the ≥3-candle distance [D4.1 p1 @ 09:40
// written] is measured from there.
func oldExtremeIndexes(levels []Level, bars []market.Kline) []oldExtreme {
	var out []oldExtreme
	for _, l := range levels {
		if l.Kind != KindOldExtreme {
			continue
		}
		idx := -1
		isHigh := false
		for i := len(bars) - 1; i >= 0; i-- {
			if abs(bars[i].High-l.Price) < 0.26 {
				idx, isHigh = i, true
				break
			}
			if abs(bars[i].Low-l.Price) < 0.26 {
				idx, isHigh = i, false
				break
			}
		}
		if idx >= 0 {
			out = append(out, oldExtreme{level: l, idx: idx, isHigh: isHigh})
		}
	}
	return out
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
	case strings.HasPrefix(reason, "room rule"):
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
func nearestOldExtremeOnSide(extremes []oldExtreme, price float64, side Side) (oldExtreme, bool) {
	var best oldExtreme
	found := false
	for _, o := range extremes {
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

// runSwing evaluates the §8 4h-EMA34 swing on FINAL 5m bars (the forming
// bucket excluded — CTO wiring ruling 2026-10-03) and returns its intents.
// The swing is NOT gated on the 5m trigger zone by default: §8 is a
// self-contained 4h → 5m procedure and nothing in D5.2 ties it to the 5m
// trigger lines. The knob SwingRespects5mZone (default false) turns the zone
// gate on [C]: not stated in the method (CTO swing ruling, mails
// 1791001124127 / 1791001760445).
func runSwing(e *Evaluator, bars []market.Kline, now int64) []Intent {
	closed := closedBuckets(bars, now, e.Cfg)
	ints := SwingTick(&e.State.Swing, closed, e.Cfg.Swing, now)
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
	// 5m bar (00-METHOD.md §8: "Wait for a LITERAL touch" [p2 @ 09:15]).
	ints = dropStaleSwingIntents(ints, closed)
	kept, dropped := swingZoneGate(ints, e.State.Trigger, e.Cfg.Swing.Respects5mZone)
	// C5: the trigger-zone drop names its reason.
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
			if swingRoomRefused(in, obstacle, e.Cfg.RoomMultiple) {
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

// swingZoneGate drops swing intents whose entry price sits between two
// opposing trigger lines, but ONLY when the knob is on (respect = true).
func swingZoneGate(ints []Intent, t TriggerLine, respect bool) ([]Intent, int) {
	if !respect {
		return ints, 0
	}
	out := make([]Intent, 0, len(ints))
	dropped := 0
	for _, in := range ints {
		if ok, _, _ := TriggerVerdict(t, in.Price); !ok {
			dropped++ // in the two-trigger zone — no trade at all there
			continue
		}
		out = append(out, in)
	}
	return out, dropped
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
		e.State.SeedLevels, e.State.Seed1HLastColour =
			keyLevelsAppend(e.State.SeedLevels, c, e.State.Seed1HLastColour, e.Cfg.KeyLevelPrunePts)
		e.State.Seed1HWatermark = c.OpenTime
		e.State.Seed1HBars = append(e.State.Seed1HBars, c)
	}

	var out []Level
	out = append(out, e.State.SeedLevels...)
	out = append(out, Level{Key: string(KindEMA34), Kind: KindEMA34, Price: e.State.EMA34})
	out = append(out, Level{Key: string(KindEMA9), Kind: KindEMA9, Price: e.State.EMA9})
	for _, d := range kernel.SwingPointLevels(bars, time.UnixMilli(swingPointNow(now))) {
		switch d.Kind {
		case kernel.KindSWGH, kernel.KindSWGL:
			out = append(out, Level{
				Key:   fmt.Sprintf("%s:%.2f", KindOldExtreme, d.Price),
				Kind:  KindOldExtreme,
				Price: d.Price,
			})
		}
	}
	return out
}

// swingRoomRefused is the swing reject's room rule (item 25 + R68, CTO fold):
// the obstacle — the 5m EMA34 when it sits on the trade side — must be at
// least roomMultiple × risk from the entry. No on-side obstacle = no refusal.
func swingRoomRefused(in Intent, obstacle, roomMultiple float64) bool {
	if obstacle == 0 || !targetOnSide(in.Side, in.Price, obstacle) {
		return false
	}
	refuse, _ := roomRefusal(in.Price, in.Stop, obstacle, roomMultiple)
	return refuse
}
