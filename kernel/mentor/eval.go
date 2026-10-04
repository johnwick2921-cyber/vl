package mentor

import (
	"encoding/json"
	"fmt"
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
	// ORB is the §7 step 0 opening-range gate state (drawn at 08:32 CT, escape
	// latches on the first 1m body close outside). Per-session-day.
	ORB ORB `json:"orb,omitempty"`

	// Seeded state (P0 1791009358785): built by Seed from the STORED bars and
	// updated incrementally — never rebuilt from the live slice.
	SeedLevels       []Level `json:"seed_levels,omitempty"` // 1H RTH key levels, full stored history
	Seed1HWatermark  int64   `json:"seed_1h_watermark,omitempty"`
	Seed1HLastColour bool    `json:"seed_1h_last_colour,omitempty"`
	Seed1mWatermark  int64   `json:"seed_1m_watermark,omitempty"`
	EMA34            float64 `json:"ema34,omitempty"` // 1m EMA 34 (incremental)
	EMA9             float64 `json:"ema9,omitempty"`  // 1m EMA 9 (incremental)

	// E2 (CTO 12:27:25Z): the EMA34 loss machinery — the pending stop of the
	// last emitted EMA setup, and the one-loss block until a departure.
	EmaPendingSide  Side    `json:"ema_pending_side,omitempty"`
	EmaPendingEntry float64 `json:"ema_pending_entry,omitempty"`
	EmaPendingStop  float64 `json:"ema_pending_stop,omitempty"`
	EmaBlocked      bool    `json:"ema_blocked,omitempty"`
	// ArmSeq names the next arm.
	ArmSeq int `json:"arm_seq"`
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
	seeded   bool
	missing  []string
	seedLine string
}

func New(cfg Config) *Evaluator {
	return &Evaluator{Cfg: cfg, State: State{
		Touches:       map[string]Touch{},
		ISBOnly:       map[string]bool{},
		DeletedLevels: map[string]bool{},
		ISBArms:       map[string]ISBArm{},
	}}
}

// Levels computes the full mentor level set from the 1m history: colour-change
// key levels (§4.3), EMA 34/9 (§8/§11), and the old highs/lows — the bot's own
// swing levels reused [DS-106 §1: kernel/levels_swing.go SwingPointLevels].
func Levels(bars []market.Kline, cfg Config, now int64) []Level {
	var out []Level
	out = append(out, KeyLevels(bars, cfg)...)
	out = append(out, EMALevels(bars, cfg)...)
	for _, d := range kernel.SwingPointLevels(bars, time.UnixMilli(now)) {
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

// boxBanFilter drops entry intents whose entry price — or whose reference
// candle close — sits INSIDE a box: "NEVER trade inside the box, neither the
// candle nor your entry point" [D3.2 p1 @ 06:59].
func boxBanFilter(out []Intent, boxes []Box, cur market.Kline) []Intent {
	if len(boxes) == 0 {
		return out
	}
	kept := out[:0:0]
	for _, in := range out {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) &&
			(InsideAnyBox(boxes, cur.Close) || InsideAnyBox(boxes, in.Price)) {
			continue
		}
		kept = append(kept, in)
	}
	return kept
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
		flags = append(flags, "isb_at_old_extreme")
	}
	if midRangeBoxed(boxes, cur.Close) {
		flags = append(flags, "isb_in_range")
	}
	return strings.Join(flags, "|")
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
func nextLevelBeyond(levels []Level, price float64, side Side) float64 {
	best := 0.0
	for _, l := range levels {
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

// freshTouch resets a classified touch when the level's price has moved away
// from where it was touched (the EMA case): a touch against the line at one
// price is not a touch of the same line after it drifted.
func freshTouch(tr Touch, lvl Level) Touch {
	if tr.Outcome != TouchNone && abs(lvl.Price-tr.PriceAtTouch) > 0.01 {
		return Touch{LevelKey: lvl.Key}
	}
	return tr
}

// Tick evaluates the newest closed 1m candle. bars is the closed history up to
// now (the bot's BarCache slice); now is the current time for the swing
// detector's closed-bar filter. Returns the intents for this candle.
func (e *Evaluator) Tick(bars []market.Kline, now int64) []Intent {
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
	for _, l := range levels {
		if l.Kind != KindKeyLevel || e.State.DeletedLevels[l.Key] {
			continue
		}
		if levelDeletedBy1HBody(l, bars, now) {
			e.State.DeletedLevels[l.Key] = true
		}
	}
	levels = withoutDeleted(levels, e.State.DeletedLevels)

	var out []Intent

	// 5m trigger line advances every 1m close (aggregated 5m bars).
	e.State.Trigger = TriggerTick(e.State.Trigger, barsTF(bars, 5), 5, e.Cfg)

	// §5.4 HTF direction (DS-106, fold item 3): the 4h/1h lines advance with
	// the same bar history.
	e.State.HTF = HTFAdvance(e.State.HTF, barsTF(bars, 240), barsTF(bars, 60), e.Cfg)

	// §7 day gate (fold item 4): Globex run → per-trading-day latch (L1/L2).
	dg := DayGate{SpentPts: e.Cfg.DayGateSpentPts, TargetCapPts: e.Cfg.DayGateTargetCapPts}
	run, haveRun := GlobexRun(bars, now, ctime())
	e.State.Day = LatchDay(e.State.Day, now, ctime(), run, haveRun, HTFConflict(e.State.HTF), dg)

	// the location set grows by the trigger-line retest and the EMA 34
	// location line (fold item 1; R3: 1m default).
	if e.State.Trigger.Dir != "" && e.State.Trigger.Price != 0 {
		levels = append(levels, Level{Key: string(KindTriggerRetest), Kind: KindTriggerRetest, Price: e.State.Trigger.Price})
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

	// §3: first-touch classification per level.
	for _, lvl := range levels {
		tr := e.State.Touches[lvl.Key]
		if e.State.ISBOnly[lvl.Key] {
			continue // invalid level: no PHL/PLH, and touches need no re-read
		}
		// a MOVING line (EMA) that drifted away from where it was touched is
		// a fresh line for touch purposes — reset the classification.
		tr = freshTouch(tr, lvl)
		intents := visitTick(&tr, lvl, bars[len(bars)-2], bars[len(bars)-1], e.Cfg)
		e.State.Touches[lvl.Key] = tr
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
		if bx, ok := ISBBoxFrom5m(cb[len(cb)-2], cb[len(cb)-1]); ok {
			e.State.ISBBox = &bx
		}
	}

	if IsISB(prev, cur) {
		if dirOK, _, _ := TriggerVerdict(e.State.Trigger, cur.Close); dirOK {
			// B4: the 15m/5m conflict reads CLOSED buckets only — the
			// still-forming 5m bucket is dropped [D4.2 p1 @ 05:10: "a
			// 15-minute candle is only confirmed once CLOSED; trade from
			// the next one"].
			if conflict := ISBConflictVerdict(closedBuckets(bars, now, e.Cfg)); !conflict {
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
				if htfOK, htfSide, htfReason := HTFVerdict(e.State.HTF); !htfOK {
					_ = htfReason
				} else {
					// R1 (RULES-FIX-v3): the order is a STOP-LIMIT in the
					// CANDLE-1 colour direction (the only skip: a stop in
					// the twenties) [D1.4 p1 @ 09:20–10:20, 24:41–24:55].
					side, chosen, ok, reason := ISBStopLimitOrder(prev, cur, e.Cfg)
					// R5: while the box stands nothing trades inside it except a
					// SAME-direction 1m ISB — compute the verdict up front so an
					// allowed ISB still falls through to the emit below.
					boxBlocked := false
					if e.State.ISBBox != nil {
						if allowed, r := ISBBoxAllows(*e.State.ISBBox, prev, cur); !allowed {
							boxBlocked, _ = true, r
						}
					}
					if !ok {
						_ = reason // twenties — no entry
					} else if isbArmActive(e.State.ISBArms, side) {
						// ONE ARM PER SIDE (CTO parity ruling 1791008332386 #1):
						// stacking extends the EXISTING arm — no new arm.
					} else if boxBlocked {
						// R5: an opposite-direction ISB inside the box — no entry
					} else if side != "" && htfSide != "" && side != htfSide {
						// ISB direction against the 4h — no entry
					} else if e.State.Day.Verdict == DayOff {
						// day off — no mentor entries today
					} else {
						// §6 [D3.3 p1 @ 05:07]: "TARGET LÀ VỀ NHỮNG LEVEL KẾ
						// TIẾP" — a setup gives entry, stop AND target
						// [D4.1 p1 @ 01:39]; no level beyond → no trade.
						if target := nextLevelBeyond(levels, chosen.Price, side); target != 0 {
							chosen.Target = target
						} else {
							return stampDayState(out, e.State.Day.Verdict) // missing target — not a setup [D4.1 p1 @ 01:45]
						}
						// ISB size flags for the injector: rule 2 (at an old
						// high/low → REDUCE SIZE) and rule 3 (in a range → REDUCE
						// SIZE, "Khi trade isb in-range bắt buộc giảm size" [D4.1 p1
						// @ 08:05/09:40]) — the range is the same mid-range test as
						// the PHL/PLH ban.
						chosen.Flag = isbFlags(cur, levels, boxes)
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
						out = append(out, chosen)
					}
				}
			} else {
				out = append(out, Intent{Action: CancelArm, Reason: "15m/5m ISB conflict — no trade [D4.2 p1 @ 14:35]"})
			}
		}
	}

	// R7 (RULES-FIX-v3, behind its own knob, default OFF): the reverse ISB
	// at EMA 9 — an ISB that points AGAINST the trend with price at the EMA 9
	// trades WITH the trend [D5.4].
	if e.Cfg.ISBReverseEMA9Enabled && IsISB(prev, cur) {
		if in, ok, _ := ReverseISBAtEMA9(prev, cur, emaValue(bars, e.Cfg.EMAPeriod9), e.State.Trigger.Dir, e.Cfg); ok {
			// N12: an R7 reverse ISB fills by the close of the NEXT 1m candle
			// (the R1 family rule).
			in.ExpiryMs = cur.CloseTime + 60_000
			out = append(out, in)
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
	emaPrice := 0.0
	for _, lvl := range levels {
		if lvl.Kind == KindEMA34 {
			emaPrice = lvl.Price
			break
		}
	}
	if emaPrice != 0 {
		emaLossTick(e, emaPrice, cur)
	}

	for _, lvl := range levels {
		tr := e.State.Touches[lvl.Key]
		if tr.Outcome != TouchReject || e.State.ISBOnly[lvl.Key] {
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
		if dirOK, trigSide, _ := TriggerVerdict(e.State.Trigger, price); !dirOK || trigSide != "" && trigSide != side {
			continue
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
		for _, ex := range oldExtremes {
			in, ok, _ := PHLPLHGatedR2(tr, ex.level, ex.idx, len(bars)-1, priorSameRole(ex, oldExtremes), e.Cfg, e.State.HTF, e.State.Day.Verdict, dg)
			if ok {
				// N12: a level touch fills by the close of the NEXT 1m candle.
				in.ExpiryMs = cur.CloseTime + 60_000
				if isEMA34(lvl) {
					e.State.EmaPendingSide = in.Side
					e.State.EmaPendingEntry = in.Price
					e.State.EmaPendingStop = in.Stop
				}
				out = append(out, in)
				break
			}
		}
	}

	// §8 SWING4H (DS-106 slice) on FINAL 5m bars only — the still-forming
	// bucket is excluded (CTO wiring ruling 2026-10-03).
	out = append(out, runSwing(e, bars, now)...)

	// BOX RULING part 2: InsideAnyBox — "NEVER trade inside the box" — neither
	// the candle nor the entry point [D3.2 p1 @ 06:59].
	out = boxBanFilter(out, boxes, bars[len(bars)-1])

	// ORB gate ("ĐIỀU BẮT BUỘC" [X11 @16:43]): every intraday entry is gated
	// on the opening range; the §8 swing is exempt (orbGateFilter).
	out = orbGateFilter(out, e.State.ORB, e.Cfg)

	// P0 fail-closed: seeded with a missing source → no ENTRIES, ever (cancels
	// still flow — an arm left open must be closable).
	if e.seeded && len(e.missing) > 0 {
		out = failClosedFilter(out)
	}

	return stampDayState(out, e.State.Day.Verdict)
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
	return swingZoneGate(ints, e.State.Trigger, e.Cfg.Swing.Respects5mZone)
}

// swingExpiry is the close of the CURRENT 4h candle (session-anchored at
// 17:00 CT): an unfilled swing order lives until then (CTO 1791008594562).
func swingExpiry(now int64) int64 {
	return bucketOpen(now, 240) + 240*60_000 - 1
}

// swingZoneGate drops swing intents whose entry price sits between two
// opposing trigger lines, but ONLY when the knob is on (respect = true).
func swingZoneGate(ints []Intent, t TriggerLine, respect bool) []Intent {
	if !respect {
		return ints
	}
	out := make([]Intent, 0, len(ints))
	for _, in := range ints {
		if ok, _, _ := TriggerVerdict(t, in.Price); !ok {
			continue // in the two-trigger zone — no trade at all there
		}
		out = append(out, in)
	}
	return out
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
	for _, b := range bars {
		if b.CloseTime <= e.State.Seed1mWatermark || b.CloseTime > now {
			continue
		}
		e.State.EMA34 += k34 * (b.Close - e.State.EMA34)
		e.State.EMA9 += k9 * (b.Close - e.State.EMA9)
		e.State.Seed1mWatermark = b.CloseTime
	}

	// Extend the 1H RTH key-level walk with the candles that closed since the
	// seed watermark (colour-change level at the candle OPEN, prune newest-first).
	for _, c := range keyLevel1HBars(bars) {
		if c.OpenTime <= e.State.Seed1HWatermark || c.CloseTime > now {
			continue
		}
		e.State.SeedLevels, e.State.Seed1HLastColour =
			keyLevelsAppend(e.State.SeedLevels, c, e.State.Seed1HLastColour, e.Cfg.KeyLevelPrunePts)
		e.State.Seed1HWatermark = c.OpenTime
	}

	var out []Level
	out = append(out, e.State.SeedLevels...)
	out = append(out, Level{Key: string(KindEMA34), Kind: KindEMA34, Price: e.State.EMA34})
	out = append(out, Level{Key: string(KindEMA9), Kind: KindEMA9, Price: e.State.EMA9})
	for _, d := range kernel.SwingPointLevels(bars, time.UnixMilli(now)) {
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
