package mentor

import (
	"math"
	"strconv"
	"strings"
	"time"

	"vl/market"
)

// Limits is the G1 leg budget + G2 loss box state machine (DS-107, CTO build
// job 2026-10-03 13:00:13Z). It is simulated on CLOSED candles exactly like
// DS-103's E2: a stop entry is FILLED when price reaches the entry level
// (high >= entry for longs, low <= entry for shorts); a filled trade is a
// LOSS when its stop trades before its target; an order that never fills
// before its expiry is NO loss. That pins the CTO's three E2 cases
// (12:50:13Z) for every place: (a) entry touched (filled), then the stop
// touched -> blocked; (b) entry never touched before the expiry -> NOT
// blocked; (c) entry filled, then the target reached first -> NOT blocked.
//
// G1 LEG BUDGET (PLAN Filters, 05-EXTRA 2.7, X15 @00:36-02:46): inside ONE
// leg — from the PHL/PLH up to the old extreme — there are at most 2
// entries: the PHL/PLH and, if it appears, a same-direction ISB. A stop-out
// inside the leg closes it (no more entries); a NEW leg exists only on a 1m
// CLOSE beyond the prior high (long) / low (short). Knobs: LegBudgetEnabled
// (default ON), LegResetOn ("close" default | "touch").
//
// G2 LOSS BOX AT A PLACE (PLAN Filters, D4.2 p2 @01:56, @03:48): after a
// LOSS at a place (entry filled, then the stop hit), no re-entry there until
// price departs. B22 (FOMO extra @04:47-06:22; D4.2 p2 @02:41-04:06; X7
// @10:11-11:05) — the loss area is STRUCTURE:
//
//   - LONG loss: the area is left when a 1m candle CLOSES above the prior
//     swing high (the nearest old extreme on the target side at entry, the
//     same machinery the PHL uses) or closes below the wave low (the lowest
//     low from the entry through the stop-out candle). Mirror for SHORT.
//   - No prior swing high: only the wave break ends it, or the day ends;
//     LossDeparturePts (default OFF) stays as a numeric fallback.
//   - A loss at a BOX: a 1m candle COMPLETELY outside the box, wicks
//     included.
//
// Two losses at one place -> that place is OFF FOR THE DAY (cleared at the
// trading-day rollover). The loss candle itself is never its own departure.
//
// The leg is created at FILL (not placement), the budget is consulted at
// placement, and a stop-out closes the leg — all three exactly as the replay
// (engine.py LegBudget). G2 places (CTO R-b 2026-10-03): the setup's PLACE —
// the level price, the box edge, or the EMA — carried on the intent
// (AnchorKey/Anchor); a plain ISB carries none and is not loss-boxed. The
// G1 leg extreme is separate: the nearest old extreme beyond the entry.
type Limits struct {
	Long  *Leg
	Short *Leg
	// Places is the per-place loss registry, keyed by the quarter-tick
	// anchor price (round(anchor*4)) — the replay's key. It lives for one
	// trading day.
	Places map[string]*Place
	// Refusals is the B-rules refusal ledger for the G1/G2 drops (CTO
	// 13:51:31Z): leg_budget_second_phl / leg_budget_stopped /
	// leg_budget_full / loss_box_blocked / loss_box_off_day /
	// orphan_not_location.
	Refusals map[string]int
	// Counters is the FU-1 fill-path counter ledger (not drops): a real fill
	// registered from the armed ROW (record_fill_from_row, after a restart
	// dropped the in-memory pend), and an empty receipt id that was dropped
	// before any lookup (record_fill_no_receipt). Surfaced in the funnel
	// beside Refusals.
	Counters map[string]int

	dayKey string
	pend   []*pendOrder
	open   []*openTrade
	// filled is the FU-1 receipt dedupe: the entry signal ids whose REAL fill
	// has already registered (a partial-then-full, or a replay, registers once).
	filled map[string]bool
}

// Leg is one G1 leg per side.
type Leg struct {
	Side      Side
	Extreme   float64 // the old extreme the leg runs to (the prior high/low)
	Entries   int     // fills registered in this leg
	PHLFilled bool    // a PHL/PLH (not an ISB) already filled in this leg
	Stopped   bool    // a stop-out inside the leg closed it
}

// Place is the G2 loss box at one place, keyed by the setup's place (R-b).
// Anchor is the loss price. The B22 structural bounds are captured at the
// stop-out: Box+Lo/Hi for a box place (a candle fully outside frees it),
// Side+Swing (the prior swing high/low on the target side at entry) and Wave
// (the wave low/high from the entry through the stop-out candle).
type Place struct {
	Anchor  float64
	Losses  int
	Blocked bool
	OffDay  bool // two losses — the place is off for the day
	DayKey  string
	just    bool // blocked on the CURRENT candle: no departure check yet

	Box   bool    // the place is a box: freed only by a candle FULLY outside it
	Lo    float64 // box bounds (Box)
	Hi    float64
	Side  Side    // trade side of the loss
	Swing float64 // prior swing high (long) / low (short); 0 = none
	Wave  float64 // wave low (long) / high (short) from entry through the stop
}

type pendOrder struct {
	side   Side
	entry  float64
	stop   float64
	target float64
	expiry int64
	isISB  bool
	armID  string  // the intent's ArmID — a CancelArm drops this pend (X15-5)
	legExt float64 // G1 extreme carried from placement; 0 = none
	anchor float64 // G2 loss price (level / EMA / box midpoint); 0 = none
	place  string  // G2 place key (AnchorKey); "" = use the quarter-tick
	swing  float64 // B22: prior swing high/low on the target side at entry
	box    bool
	lo     float64 // box bounds (box)
	hi     float64
	// realFill (FU-1): the pend is filled ONLY by RecordFill (the broker's
	// real fill callback), never by simulate's candle-touch fill. A
	// never-placed order can then never phantom-fill and spend the budget.
	realFill bool
}

type openTrade struct {
	side   Side
	stop   float64
	target float64
	anchor float64
	place  string
	swing  float64
	box    bool
	lo     float64
	hi     float64
	wave   float64 // wave low (long) / high (short), tracked from the fill candle
}

// Apply is the single Tick hook (DS-103 merges the call): it advances the
// closed-candle simulation with cur (the just-closed 1m candle), applies the
// G1 leg budget and G2 loss box to the emitted entries, and registers the
// surviving entries for the next tick's simulation. prev is the candle
// before cur (its close is the leg-reset input for the "close" knob).
func (l *Limits) refuse(reason string) {
	if l.Refusals == nil {
		l.Refusals = map[string]int{}
	}
	l.Refusals[reason]++
}

// count records a non-drop FU-1 fill-path event (a fill registered from the
// row, an empty receipt id) in the Counters ledger.
func (l *Limits) count(reason string) {
	if l.Counters == nil {
		l.Counters = map[string]int{}
	}
	l.Counters[reason]++
}

func (l *Limits) Apply(out []Intent, prev, cur market.Kline, now int64, levels []Level, cfg Config) []Intent {
	day := tradingDayKey(time.UnixMilli(now).In(ctime()))
	if l.dayKey != "" && l.dayKey != day {
		// New trading day: G2 places (including off-for-day) and the
		// unfinished simulations die with the day. Legs persist — the
		// replay keeps one LegBudget for the whole run.
		l.dayKey = day
		l.Places = nil
		l.pend = nil
		l.open = nil
		l.filled = nil  // FU-1: the receipt dedupe dies with the trading day
		l.Counters = nil // FU-1: the fill-path counters die with the day
	} else if l.dayKey == "" {
		l.dayKey = day
	}

	// G1: the reset is known at the close of the PREVIOUS candle and gates
	// this candle's entries (the replay's pc_of(m)).
	l.resetBreaks(prev, cur, cfg)

	// Simulate the orders placed on earlier ticks and the open trades
	// against the just-closed candle.
	l.simulate(cur, now, levels)

	// G2 departure (B22 structural): a blocked place clears when —
	//   box:   a 1m candle lies COMPLETELY outside the box (wicks included);
	//   level/EMA: a close beyond the prior swing (long: above the swing
	//   high; short: below the swing low) or beyond the wave (long: below
	//   the wave low; short: above the wave high). With no swing, only the
	//   wave break ends it (or the day ends); LossDeparturePts > 0 is the
	//   numeric fallback for that swing-less case (default OFF).
	// Off-for-day places never clear. Loss counts survive the day — two
	// losses at one place turn it off for the day.
	for _, p := range l.Places {
		if p.just {
			p.just = false
			continue
		}
		if !p.Blocked || p.OffDay {
			continue
		}
		free := false
		switch {
		case p.Box:
			free = cur.High < p.Lo || cur.Low > p.Hi
		default:
			if p.Side == SideShort {
				free = (p.Swing != 0 && cur.Close < p.Swing) || cur.Close > p.Wave
			} else {
				free = (p.Swing != 0 && cur.Close > p.Swing) || cur.Close < p.Wave
			}
			if !free && p.Swing == 0 && p.Anchor != 0 && cfg.LossDeparturePts > 0 {
				free = abs(cur.Close-p.Anchor) >= cfg.LossDeparturePts // numeric fallback, OFF by default
			}
		}
		if free {
			p.Blocked = false
		}
	}

	kept := out[:0]
	for _, in := range out {
		switch in.Action {
		case PlaceStopEntry:
			ext := nearestOldExtreme(levels, in.Price, in.Side)
			if cfg.LegBudgetEnabled {
				if ban := l.legVerdict(in.Side, false); ban != "" {
					l.refuse(ban) // G1: second PHL / leg closed by a stop-out
					continue
				}
			}
			ref := normalizePlace(in, levels)
			if ref.orphan {
				l.refuse("orphan_not_location") // K2: not a location
				continue
			}
			if p := l.blockedPlace(ref, in.Price); p != nil {
				if p.OffDay {
					l.refuse("loss_box_off_day")
				} else {
					l.refuse("loss_box_blocked")
				}
				continue // G2: the place is boxed after a loss
			}
			kept = append(kept, in)
			l.pend = append(l.pend, &pendOrder{
				side:   in.Side,
				entry:  in.Price,
				stop:   in.Stop,
				target: in.Target,
				expiry: pendExpiry(in, now),
				armID:  in.ArmID,
				legExt: ext,
				anchor: ref.anchor,
				place:  ref.key,
				swing:  ext, // B22: the prior swing on the target side at entry
				box:    ref.box,
				lo:     ref.lo,
				hi:     ref.hi,
				realFill: cfg.RealFillOnly, // FU-1: live arms fill by the real fill, never the candle
			})
		case PlaceStopLimitEntry:
			if cfg.LegBudgetEnabled {
				if ban := l.legVerdict(in.Side, true); ban != "" {
					l.refuse(ban) // G1: the leg is full (the PHL + one ISB)
					continue
				}
			}
			// D2-17 (D4.2-13): an ISB participates in G2 — a blocked band
			// refuses the entry, and the ISB's own stop-out boxes its
			// entry-to-stop band.
			if p := l.blockedPlace(placeRef{}, in.Price); p != nil {
				if p.OffDay {
					l.refuse("loss_box_off_day")
				} else {
					l.refuse("loss_box_blocked")
				}
				continue
			}
			kept = append(kept, in)
			l.pend = append(l.pend, &pendOrder{
				side:   in.Side,
				entry:  in.Price,
				stop:   in.Stop,
				target: in.Target,
				expiry: pendExpiry(in, now),
				isISB:  true,
				armID:  in.ArmID,
				anchor: in.Price, // the ISB band: the loss is boxed by price band
				place:  isbBandKey(in.Price, in.Stop),
				box:    true,
				lo:     math.Min(in.Price, in.Stop),
				hi:     math.Max(in.Price, in.Stop),
				realFill: cfg.RealFillOnly, // FU-1: live arms fill by the real fill, never the candle
			})
		default:
			if in.Action == CancelArm {
				l.dropPend(in.ArmID) // X15-5: a cancelled order must never phantom-fill
			}
			kept = append(kept, in) // cancels, extends, moves — never filtered
		}
	}
	return kept
}

// resetBreaks — G1: a new leg starts only on a break beyond the prior
// extreme. "close" (default): the previous candle's CLOSE strictly beyond
// the extreme, known at its close, gates the next candle. "touch": this
// candle's wick reaching the extreme.
func (l *Limits) resetBreaks(prev, cur market.Kline, cfg Config) {
	if cfg.LegResetOn == "touch" {
		if l.Long != nil && cur.High >= l.Long.Extreme {
			l.Long = nil
		}
		if l.Short != nil && cur.Low <= l.Short.Extreme {
			l.Short = nil
		}
		return
	}
	if l.Long != nil && prev.Close > l.Long.Extreme {
		l.Long = nil
	}
	if l.Short != nil && prev.Close < l.Short.Extreme {
		l.Short = nil
	}
}

// simulate advances the closed-candle fill/loss simulation one candle.
func (l *Limits) simulate(cur market.Kline, now int64, levels []Level) {
	keepP := l.pend[:0]
	for _, p := range l.pend {
		if p.realFill {
			// FU-1: a real-fill pend is filled ONLY by RecordFill (the
			// broker's real fill callback), never by a candle touching the
			// entry. A never-placed order can then never phantom-fill and
			// spend the budget. Expiry still applies (the broker cancels an
			// unfilled order at the same time).
			if p.expiry != 0 && now >= p.expiry {
				continue // expired unfilled — NOT a loss (case b)
			}
			if p.expiry == 0 {
				continue // one-candle rest (mirror of the sim path below)
			}
			keepP = append(keepP, p)
			continue
		}
		filled := p.side == SideLong && cur.High >= p.entry ||
			p.side == SideShort && cur.Low <= p.entry
		if filled {
			// Entry touched = filled. A stop-out on the SAME candle is a
			// loss (the trade existed for the span of the candle) — the
			// open-trade loop below catches it.
			l.fill(p, cur.Low, cur.High, levels)
			continue
		}
		if p.expiry != 0 && now >= p.expiry {
			continue // never touched before the expiry — NOT a loss (case b)
		}
		if p.expiry == 0 {
			// X15-5: a zero-expiry level/box order rests exactly ONE candle
			// live (the trader's N12 next-candle default, mentor_tick.go);
			// here it has just missed that candle, so it expires — it must
			// not sit forever and phantom-fill on a later candle.
			continue
		}
		keepP = append(keepP, p)
	}
	l.pend = keepP

	keepO := l.open[:0]
	for _, t := range l.open {
		// B22: the wave grows with every candle the trade is open, the
		// stop-out candle included.
		if t.side == SideLong && cur.Low < t.wave {
			t.wave = cur.Low
		}
		if t.side == SideShort && cur.High > t.wave {
			t.wave = cur.High
		}
		stopped := t.side == SideLong && cur.Low <= t.stop ||
			t.side == SideShort && cur.High >= t.stop
		if stopped {
			l.loss(t) // filled, then the stop hit — case (a)
			continue
		}
		targetHit := t.side == SideLong && cur.High >= t.target ||
			t.side == SideShort && cur.Low <= t.target
		if targetHit {
			continue // the target reached first — NOT a loss (case c)
		}
		keepO = append(keepO, t)
	}
	l.open = keepO
}

// fill converts a pending order into a FILL: it registers the G1 leg and
// opens the G2 loss trade. lo/hi seed the B22 wave (the fill candle's
// low/high); levels feeds registerLeg's old-extreme fallback when the pend
// carries no leg extreme (nil for a real fill — the pend's legExt wins).
func (l *Limits) fill(p *pendOrder, lo, hi float64, levels []Level) {
	l.registerLeg(p.side, p.legExt, levels, p.entry, p.isISB)
	wave := lo
	if p.side == SideShort {
		wave = hi
	}
	t := &openTrade{
		side:   p.side,
		stop:   p.stop,
		target: p.target,
		anchor: p.anchor,
		place:  p.place,
		swing:  p.swing,
		box:    p.box,
		lo:     p.lo,
		hi:     p.hi,
		wave:   wave, // the fill candle is the entry candle
	}
	if stopOutOn(t, lo, hi) {
		// P2-3: the fill candle already crossed the stop (a late drain misses
		// the same-candle stop-out) — register the G2 loss immediately.
		l.loss(t)
		return
	}
	l.open = append(l.open, t)
}

// FillRow is the FU-1 row-fallback evidence (P1-2): when a real fill has no
// pend (a restart dropped the in-memory pends), the armed ledger ROW still
// carries the trade. A fill registered from the row must never be dropped.
type FillRow struct {
	Side   Side
	Entry  float64
	Stop   float64
	Target float64
	ISB    bool
}

// fillRow registers a REAL fill from the armed row (no pend — a restart) the
// same way fill does, reconstructing the G2 place from the row: an ISB boxes
// its entry-to-stop band; every other setup keys on the quarter-tick entry.
// legExt is left 0 so registerLeg's old-extreme fallback resolves it against
// levels.
func (l *Limits) fillRow(row *FillRow, lo, hi float64, levels []Level) {
	isISB := row.ISB
	anchor := row.Entry
	place := ""
	box := isISB
	var blo, bhi float64
	if isISB {
		place = isbBandKey(row.Entry, row.Stop)
		blo, bhi = math.Min(row.Entry, row.Stop), math.Max(row.Entry, row.Stop)
	}
	p := &pendOrder{
		side:   row.Side,
		entry:  row.Entry,
		stop:   row.Stop,
		target: row.Target,
		isISB:  isISB,
		anchor: anchor,
		place:  place,
		box:    box,
		lo:     blo,
		hi:     bhi,
		// swing is left 0: without the intent's AnchorKey the B22 swing
		// departure is unavailable; the wave break still frees the place.
	}
	l.fill(p, lo, hi, levels)
}

// stopOutOn reports whether the fill candle (lo/hi) already crossed the trade's
// stop — a same-candle stop-out the open-trade loop would otherwise miss when
// the drain runs a bar late.
func stopOutOn(t *openTrade, lo, hi float64) bool {
	return t.side == SideLong && lo <= t.stop ||
		t.side == SideShort && hi >= t.stop
}

// takePend removes and returns ONE still-pending order with the given ArmID
// (FU-1). It is the RecordFill half of dropPend: the real fill consumes the
// placeholder the placement registered.
func (l *Limits) takePend(armID string) *pendOrder {
	if armID == "" {
		return nil
	}
	for i, p := range l.pend {
		if p.armID == armID {
			l.pend = append(l.pend[:i], l.pend[i+1:]...)
			return p
		}
	}
	return nil
}

// RecordFill registers a REAL broker fill for a pending mentor entry (FU-1):
// the trader's fill callback delivers it through the queued drain in
// mentorEvalOnce, under mentorEvalMu. It is the G1 leg budget + G2 loss box
// input for a real-fill-only evaluator — the simulated candle-touch fill is
// NOT consulted for these arms, so a never-placed order can never spend the
// budget. Idempotent per receipt id (the entry signal id): the first receipt
// registers the leg and opens the loss trade; a retransmit or a
// partial-then-full is a no-op. A receipt whose pend is already gone is
// registered from the armed ROW when one is supplied (a restart dropped the
// pends — never drop a real fill), else counted, never fabricated. lo/hi seed
// the B22 wave (the fill candle's low/high); levels feeds the row-fallback's
// old-extreme resolution.
func (l *Limits) RecordFill(receiptID, armID string, lo, hi float64, row *FillRow, levels []Level) {
	if receiptID == "" {
		l.count("record_fill_no_receipt") // P2-5: empty receipt id, counted
		return
	}
	if l.filled[receiptID] {
		return // dedupe: one receipt registers once (partial-then-full / replay)
	}
	p := l.takePend(armID)
	if p != nil {
		l.fill(p, lo, hi, nil)
		l.markFilled(receiptID)
		return
	}
	if row != nil && row.Entry > 0 {
		// P1-2: no pend — a restart dropped the in-memory pends. The armed row
		// reached the broker: register from it, never drop a real fill.
		l.fillRow(row, lo, hi, levels)
		l.count("record_fill_from_row")
		l.markFilled(receiptID)
		return
	}
	l.refuse("record_fill_no_pend")
}

func (l *Limits) markFilled(receiptID string) {
	if l.filled == nil {
		l.filled = map[string]bool{}
	}
	l.filled[receiptID] = true
}

// registerLeg records a FILL in the side's leg, creating the leg at the
// extreme it runs to. ext 0 falls back to the nearest old extreme beyond
// the entry (the replay's most-recent swing fallback); no extreme -> no leg
// (the replay skips registration too). isISB distinguishes the fill kind
// (X15-4): a PHL/PLH fill stamps PHLFilled so a 2nd PHL/PLH is refused
// while an ISB-then-PHL is allowed.
func (l *Limits) registerLeg(side Side, ext float64, levels []Level, entry float64, isISB bool) {
	if ext == 0 {
		ext = nearestOldExtreme(levels, entry, side)
	}
	if ext == 0 {
		return
	}
	leg := l.leg(side)
	if leg == nil {
		leg = &Leg{Side: side, Extreme: ext}
		l.setLeg(side, leg)
	}
	leg.Entries++
	if !isISB {
		leg.PHLFilled = true
	}
}

// loss registers a stop-out: one loss at the place (G2) and the leg is
// closed (G1). The B22 structural bounds (swing, wave, box edges) are
// captured from the trade — a later loss at the same place re-measures
// them.
func (l *Limits) loss(t *openTrade) {
	pid := placeID(t.place, t.anchor)
	if pid != "" {
		if l.Places == nil {
			l.Places = map[string]*Place{}
		}
		p := l.Places[pid]
		if p == nil {
			p = &Place{DayKey: l.dayKey}
			l.Places[pid] = p
		}
		p.Anchor = t.anchor
		p.Box = t.box
		p.Lo = t.lo
		p.Hi = t.hi
		p.Side = t.side
		p.Swing = t.swing
		p.Wave = t.wave
		p.Losses++
		p.Blocked = true
		p.just = true
		if p.Losses >= 2 {
			p.OffDay = true // two losses at one place — off for the day
		}
	}
	if leg := l.leg(t.side); leg != nil {
		leg.Stopped = true // a stop-out inside the leg closes it
	}
}

// legVerdict is the G1 budget check at PLACEMENT time ("" = allowed). The
// names mirror the replay's counters.
func (l *Limits) legVerdict(side Side, isISB bool) string {
	leg := l.leg(side)
	if leg == nil {
		return ""
	}
	if leg.Stopped {
		return "leg_budget_stopped"
	}
	if isISB {
		if leg.Entries >= 2 {
			return "leg_budget_full"
		}
		return ""
	}
	if leg.PHLFilled {
		return "leg_budget_second_phl" // at most ONE PHL/PLH per leg (X15-4)
	}
	return ""
}

// dropPend removes every still-pending order with the given ArmID. A
// CancelArm from the evaluator (level through-the-level / window-end, ISB
// 4th-candle) must remove the simulated order so it never phantom-fills
// later and spends the budget (X15-5). Already-filled orders live in `open`
// and are NOT undone — a fill that happened is real.
func (l *Limits) dropPend(armID string) {
	if armID == "" {
		return
	}
	keep := l.pend[:0]
	for _, p := range l.pend {
		if p.armID == armID {
			continue
		}
		keep = append(keep, p)
	}
	l.pend = keep
}

// DropArm removes the still-pending simulated order for an ArmID (X-07): the
// TRADER calls it when it refuses an emitted entry intent (R8 25-pt ceiling,
// window, done-after-win, news, no-chase, N4, MENTOR_PLACE off), so a
// trader-side refusal never phantom-fills on a later candle and spends the G1
// leg budget or opens a G2 loss box. Already-filled orders live in `open` and
// are NOT undone — a fill that happened is real.
func (l *Limits) DropArm(armID string) {
	l.dropPend(armID)
}

func (l *Limits) leg(side Side) *Leg {
	if side == SideLong {
		return l.Long
	}
	return l.Short
}

func (l *Limits) setLeg(side Side, leg *Leg) {
	if side == SideLong {
		l.Long = leg
		return
	}
	l.Short = leg
}

// nearestOldExtreme returns the nearest old-extreme (swing) level beyond
// price in the trade direction — the G1 leg extreme and the G2 anchor.
// 0 = none.
func nearestOldExtreme(levels []Level, price float64, side Side) float64 {
	best := 0.0
	for _, lvl := range levels {
		if lvl.Kind != KindOldExtreme {
			continue
		}
		switch side {
		case SideLong:
			if lvl.Price > price && (best == 0 || lvl.Price < best) {
				best = lvl.Price
			}
		case SideShort:
			if lvl.Price < price && (best == 0 || lvl.Price > best) {
				best = lvl.Price
			}
		}
	}
	return best
}

// placeKey is the quarter-tick anchor key — the replay's round(anchor*4).
func placeKey(price float64) string {
	return strconv.FormatInt(int64(math.Round(price*4)), 10)
}

// placeID is the G2 registry key: the setup's place key when the emit site
// named one (the level key, the box edge key, the EMA key), else the
// quarter-tick anchor price. "" = no place (a plain ISB).
func placeID(anchorKey string, anchor float64) string {
	if anchorKey != "" {
		return anchorKey
	}
	if anchor != 0 {
		return placeKey(anchor)
	}
	return ""
}

// isbBandKey is the G2 registry key for an ISB loss: its entry-to-stop price
// band (D2-17, D4.2-13) — two ISB losses in the SAME band turn that band off
// for the day, like any other place.
func isbBandKey(entry, stop float64) string {
	return "isb:" + placeKey(math.Min(entry, stop)) + ":" + placeKey(math.Max(entry, stop))
}

// blockedPlace is the G2 placement check (D2-17): an entry is refused when its
// place is boxed — by the EXACT place key, or by PRICE BAND (the entry lies
// inside the loss's [wave, swing] area / ISB band), so a different level a few
// points away inside the same area is also refused, both sides.
func (l *Limits) blockedPlace(ref placeRef, price float64) *Place {
	if pid := placeID(ref.key, ref.anchor); pid != "" {
		if p := l.Places[pid]; p != nil && (p.Blocked || p.OffDay) {
			return p
		}
	}
	for _, p := range l.Places {
		if !p.Blocked && !p.OffDay {
			continue
		}
		if p.insideBand(price) {
			return p
		}
	}
	return nil
}

// insideBand reports whether a price lies inside the place's blocked area —
// the box edges for a box place, else the [wave, swing] structural band of a
// level/EMA loss (D2-17, D4.2-13).
func (p *Place) insideBand(price float64) bool {
	if p.Box {
		return price >= p.Lo && price <= p.Hi
	}
	if p.Swing == 0 || p.Wave == 0 {
		return false // no structural band — the exact key is the only match
	}
	lo, hi := p.Swing, p.Wave
	if lo > hi {
		lo, hi = hi, lo
	}
	return price >= lo && price <= hi
}

// placeRef is the normalized G2 place for one entry.
type placeRef struct {
	key    string
	anchor float64
	box    bool
	lo     float64 // box bounds (box)
	hi     float64
	orphan bool
}

// normalizePlace maps the raw emit-site place to the G2 key (CTO K1/K2,
// 13:15:27Z; box ruling 13:20:08Z):
//
//	K1 — every EMA keys on its CONSTANT line key ("ema34"/"ema9"/
//	"ema34_htf", which is what the EMA levels already carry); a moving line
//	cannot be keyed by price. The anchor is the loss-time price.
//	K2 — an old-extreme entry keys on the COINCIDENT KEY LEVEL within
//	±LocationCoincidePts (the level that makes it a location). An old
//	extreme with no coincident key level is NOT a location: orphan=true —
//	the trade should not exist and the caller drops it.
//	BOX — a box is ONE place: the edge suffix (":top"/":bottom") is
//	stripped, the anchor is the box midpoint, and the bounds are the box
//	edges (B22: the box is left by a candle fully outside it).
//	Everything else (key levels, trigger retest, plain prices) passes
//	through unchanged.
func normalizePlace(in Intent, levels []Level) placeRef {
	ref := placeRef{key: in.AnchorKey, anchor: in.Anchor}
	if ref.key == "" {
		return placeRef{} // a plain ISB — no place at all
	}
	switch {
	case ref.key == string(KindEMA34) || ref.key == string(KindEMA9) || ref.key == string(KindEMA34HTF):
		return ref // constant line key, loss price for departure
	case strings.HasPrefix(ref.key, string(KindOldExtreme)):
		if kl := nearestKeyLevelWithin(levels, ref.anchor, LocationCoincidePts); kl != nil {
			ref.key, ref.anchor = kl.Key, kl.Price
			return ref
		}
		return placeRef{orphan: true} // K2: not a location — the trade should not exist
	case strings.HasPrefix(ref.key, "ftgh:") || strings.HasPrefix(ref.key, "ftgl:"):
		// B22 follow-up (CTO 20:39:46Z): boxEntryIntent carries the
		// UNSUFFIXED box key (b.Key = ftgh:<top>:<bottom>, box_trade.go);
		// the level-edge path carries the suffixed one. Either way it is
		// ONE box place, with or without the suffix.
		base := strings.TrimSuffix(strings.TrimSuffix(ref.key, ":top"), ":bottom")
		lo, hi, ok := boxBounds(levels, base)
		if !ok {
			lo, hi, ok = boxKeyBounds(base) // the bounds live in the key itself
		}
		if !ok {
			lo, hi = ref.anchor, ref.anchor
		}
		return placeRef{key: base, anchor: (lo + hi) / 2, box: true, lo: lo, hi: hi}
	}
	return ref
}

// boxBounds returns the live box edges for base (the box key without the
// edge suffix); ok = BOTH edges were found among the levels.
func boxBounds(levels []Level, base string) (lo, hi float64, ok bool) {
	var gotLo, gotHi bool
	for _, l := range levels {
		switch l.Key {
		case base + ":bottom":
			lo, gotLo = l.Price, true
		case base + ":top":
			hi, gotHi = l.Price, true
		}
	}
	return lo, hi, gotLo && gotHi
}

// boxKeyBounds parses the bounds from the box key itself
// ("ftgh:<top>:<bottom>", box.go — b.Key carries both edges).
func boxKeyBounds(key string) (lo, hi float64, ok bool) {
	parts := strings.Split(key, ":")
	if len(parts) != 3 {
		return 0, 0, false
	}
	top, err1 := strconv.ParseFloat(parts[1], 64)
	bot, err2 := strconv.ParseFloat(parts[2], 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return bot, top, true
}

// nearestKeyLevelWithin is the coincident key level within tol of price
// (K2): the level that makes an old extreme a location.
func nearestKeyLevelWithin(levels []Level, price, tol float64) *Level {
	var best *Level
	for i := range levels {
		l := &levels[i]
		if l.Kind != KindKeyLevel || abs(l.Price-price) > tol {
			continue
		}
		if best == nil || abs(l.Price-price) < abs(best.Price-price) {
			best = l
		}
	}
	return best
}
