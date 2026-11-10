package mentor

import (
	"fmt"
	"sort"

	"vl/market"
)

// PHL / PLH [§2.2] — R2 mechanics, verbatim [D2.2 p1 @ 19:34–19:58, 23:22]:
//
//   - PHL = LONG: in an uptrend price pulls back; a buy stop sits at the
//     PREVIOUS candle's high and the candle that breaks it fills you
//     [@ 19:34–19:58]. STOP = the low of the candle that was broken
//     [@ 04:58, 07:28]. TARGET near the old high, not at it [@ 07:33].
//   - PLH = SHORT, the exact mirror: a downtrend bounce; sell stop at the
//     previous candle's low; stop = the high of the broken candle
//     [@ 23:22–23:52, 11:50–11:59].
//   - The entry must be far from the old high: wait for 1–2 more pullback
//     candles [p2 @ 05:25, 07:02] → PHLMinCandlesFromExtreme.
//   - A HIGHER low is required — a flat low is an FTGL, not a PHL (and the
//     mirror for PLH) → PHLPLHR2's priorSwing.
//   - At a level, the touching reference candle IS the previous candle
//     (§3 first-touch reference) — the stop order sits on its extreme,
//     NO buffer.

// PHLPLH builds the setup intent from a reject touch (§3) at a level, or
// reports why it is not a trade. barIdx and extremeIdx are indexes into the
// 1m series the evaluator ticks on. Compatibility wrapper: the higher-low
// check is skipped (priorSwing 0) — the evaluator should call PHLPLHR2.
func PHLPLH(t Touch, oldExtreme Level, extremeIdx, barIdx int, cfg Config) (Intent, bool, string) {
	return PHLPLHR2(t, oldExtreme, extremeIdx, barIdx, 0, cfg)
}

// PHLPLHR2 is the R2 PHL/PLH: priorSwing is the previous same-role swing
// price (0 = skip the higher-low / lower-high check). The target is NEAR
// the old extreme, not at it.
func PHLPLHR2(t Touch, oldExtreme Level, extremeIdx, barIdx int, priorSwing float64, cfg Config) (Intent, bool, string) {
	return phlPLHR2(t, oldExtreme, extremeIdx, barIdx, priorSwing, nil, cfg, false)
}

// PHLPLHR2Levels is PHLPLHR2 with B15 (CTO 20:48:40Z): the target is the
// FIRST obstacle in the way — the nearest level beyond the entry (the same
// nextLevelBeyond the ISB and box paths use), capped at the old extreme
// minus PHLTargetShyPts [D3.3 p1 @05:18-05:34: "target là về những level kế
// tiếp… những cái mà nó ngán đường trên đường đi"]. The room rule and the
// 1:1 floor are measured to that target. nil levels = the old behaviour.
func PHLPLHR2Levels(t Touch, oldExtreme Level, extremeIdx, barIdx int, priorSwing float64, levels []Level, cfg Config) (Intent, bool, string) {
	return phlPLHR2(t, oldExtreme, extremeIdx, barIdx, priorSwing, levels, cfg, false)
}

// phlPLHR2 is the shared PHL/PLH core.
func phlPLHR2(t Touch, oldExtreme Level, extremeIdx, barIdx int, priorSwing float64, levels []Level, cfg Config, confluence bool) (Intent, bool, string) {
	if !cfg.Enabled {
		return Intent{}, false, "mentor mode off"
	}
	if t.Outcome != TouchReject {
		return Intent{}, false, "not a reject touch — no PHL/PLH [D5.2 p1 @ 19:51]"
	}
	side, _, ok := RejectEntry(t, cfg)
	if !ok {
		return Intent{}, false, "no reject entry"
	}
	// m472 A3: entry ray = candle extreme +/- 1.0, stop ray = the other extreme
	// -/+ 1.0 (2 pts total, risk = range + 2.0) [Oct D2.3 @ 46:20-47:20]; supersedes
	// Sept D2.2-R10 0.50/0.50 (the example below). The 25-pt stop ceiling now bites
	// at a 23-pt candle range.
	// R2: the buy stop sits at the PREVIOUS candle's high; stop = the low
	// of that broken candle. The previous candle = the touch reference.
	// SUPERSEDED by the Oct D2.3 rays above; D2.2-R10 [p1 @04:14–05:43, screen — release #59 fix round, mentor-exact]:
	// the entry RAY is the signal high + 0.50 (29,395.75 → 29,396.25) and the
	// stop RAY the signal low − 0.50 (29,386.50 → 29,386.00) —
	// PHLEntryBufferPts / PHLStopBufferPts. The structural checks below (the
	// EMA 34 nuance, the higher low / lower high) read the candle's own
	// extreme; the stop buffer is added last.
	price := t.RefBar.High + cfg.PHLEntryBufferPts
	stop := t.RefBar.Low
	if side == SideShort {
		price = t.RefBar.Low - cfg.PHLEntryBufferPts
		stop = t.RefBar.High
	}
	emaStop := false
	// D3.3 p1 @00:40–01:05 — the EMA34 stop nuance: when the location is the EMA
	// 34 and the reference candle only TOUCHED it (its wick did not pass through),
	// the stop goes right AT the EMA 34 (covering it), no extra room. A wick that
	// already passed through keeps the normal beyond-the-wick stop.
	if (t.LevelKey == string(KindEMA34) || t.LevelKey == string(KindEMA34HTF)) && t.PriceAtTouch > 0 {
		if side == SideLong && t.PriceAtTouch < stop {
			stop = t.PriceAtTouch
			emaStop = true
		}
		if side == SideShort && t.PriceAtTouch > stop {
			stop = t.PriceAtTouch
			emaStop = true
		}
	}
	if priorSwing != 0 {
		if side == SideLong && stop <= priorSwing {
			return Intent{}, false, "not a higher low — a flat low is an FTGL, not a PHL [D2.2 p3 @08:45–10:02]"

		}
		if side == SideShort && stop >= priorSwing {
			return Intent{}, false, "not a lower high — a flat high is an FTGH, not a PLH [D2.2 p3 @ 14:03–14:22]"
		}
	}
	// D2.2-R10: the stop ray sits PHLStopBufferPts beyond the candle extreme —
	// not when the D3.3 EMA 34 nuance put it AT the EMA ("no extra room").
	if !emaStop {
		if side == SideShort {
			stop += cfg.PHLStopBufferPts
		} else {
			stop -= cfg.PHLStopBufferPts
		}
	}
	// The old extreme must be on the TARGET side: long → old high above;
	// short → old low below.
	onTargetSide := side == SideLong && oldExtreme.Price > price ||
		side == SideShort && oldExtreme.Price < price
	if !onTargetSide {
		return Intent{}, false, "old extreme not on the target side"
	}
	if barIdx-extremeIdx < cfg.PHLMinCandlesFromExtreme {
		return Intent{}, false, "too close to the old extreme — wait 1–2 more pullback candles [D2.2 p2 @ 05:25, 07:02]"
	}
	// Target: NEAR the old extreme, not exactly at it [D2.2 p1 @ 07:33];
	// B15: capped by the FIRST obstacle in the way when levels are given.
	target := phlTarget(oldExtreme, price, side, levels, cfg)
	risk := price - stop
	reward := target - price
	if side == SideShort {
		risk = stop - price
		reward = price - target
	}
	if risk <= 0 || reward <= 0 {
		return Intent{}, false, "degenerate stop/target geometry"
	}
	// Stop-size ladder (owner order 2026-11-10): ON lets a stop up to
	// mentor_wide_stop_max_pts (40) through to the trader's size table, which
	// sizes it down [OctD2.2-R73 @01:12:32, OctD2.3-R47 @48:12]; OFF = the D3.3 25 exactly.
	if ceil := StopLadderCeiling(cfg, cfg.StopCeilingPts); risk > ceil {
		if ceil > cfg.StopCeilingPts {
			return Intent{}, false, fmt.Sprintf("%s (%g pts) — do not trade [\"40 điểm… Mình không trade\" OctD2.3-R47 @48:12, R48 @49:38]", PHLWideStopReasonPrefix, ceil)
		}
		return Intent{}, false, "stop over the 25-pt ceiling — not worth trading [D3.3 p1 @ 02:04]"
	}
	// E-2: the D1.2 floor is checked BEFORE the room knob — when the
	// target is closer than the stop, the floor is the binding constraint
	// and is the reason the ledger names (the room rule cannot pass when
	// the floor fails; roomMultiple >= 2).
	if reward < risk {
		return Intent{}, false, targetCloserThanStopReason
	}
	if refuse, why := roomRefusal(price, stop, target, confluence, targetRoomMultiple(levels, target, cfg.RoomMultiple)); refuse { // m465 D4.3-R17: 1R for a 5m-wick target
		return Intent{}, false, why
	}
	setup := "PHL"
	if side == SideShort {
		setup = "PLH"
	}
	return Intent{
		Action:   PlaceStopEntry,
		Setup:    setup,
		Side:     side,
		Price:    price,
		Stop:     stop,
		Target:   target,
		RefBarMs: t.RefBar.CloseTime,
		Reason:   "PHL/PLH: buy stop at the previous candle's high, stop at the broken candle's low, target near the old extreme [D2.2 p1 @ 19:34, 04:58, 07:33]",
	}, true, ""
}

// targetCloserThanStopReason is the D1.2 floor refusal ("the target is
// never smaller than the stop" [D1.2 p1 @ 07:48]) — the call site routes it
// to the refusal ledger (CTO E-2 2026-10-03T15:12Z: the floor holds for
// EVERY setup's intent).
const targetCloserThanStopReason = "target closer than the stop — the target is never smaller than the stop [D1.2 p1 @ 07:48]"

// phlTarget — B15 (CTO 20:48:40Z): the target is the FIRST obstacle in the
// way [D3.3 p1 @05:18-05:34] — the nearest level beyond the entry (the same
// nextLevelBeyond the ISB and box paths use), capped at the old extreme
// minus PHLTargetShyPts ("gần đỉnh cũ", D2.2 p1 @06:11). nil levels = the
// old extreme minus the shy only.
func phlTarget(oldExtreme Level, price float64, side Side, levels []Level, cfg Config) float64 {
	target := oldExtreme.Price - cfg.PHLTargetShyPts
	if side == SideShort {
		target = oldExtreme.Price + cfg.PHLTargetShyPts
	}
	if levels == nil {
		return target
	}
	// R3 (release #30 fold): the EMA 9 / EMA 34 lines are PARTIAL points, not
	// the trade target — an EMA must not by itself kill a PHL/PLH.
	ob := nextLevelBeyondNonEMA(levels, price, side)
	if ob == 0 {
		return target
	}
	switch side {
	case SideLong:
		if ob < target {
			return ob // a key level / old high-low / box edge stands in the way
		}
	case SideShort:
		if ob > target {
			return ob
		}
	}
	return target
}

// PHLPLHGated is the call site the evaluator uses for every PHL/PLH: the
// §2.2 rules in PHLPLH, then the two direction gates on top —
//
//		§5.4 (fold item 3): entries only WITH the 4h trigger direction; the 1h
//		agreeing or silent. A 1h opposite the 4h refuses the setup until it
//		flips [D4.4 p1 @15:09–15:34].
//
//	  §7 (fold item 4): a SPENT day with a 4h/1h conflict shuts the day off
//	  (latched at the 08:30 read — a later 1h flip does not reopen it); a
//	  spent day that agrees caps the target at TargetCapPts
//	  ("15 điểm bán, 10 điểm bán") [D5.1 p1 @19:56–20:07]; an unmeasured run
//	  fails closed ("any trade you are vague about — don't" [§12]). On a
//	  NORMAL day a conflict is §5.4 case 3 — the HTF gate above sits out
//	  per tick until the 1h flips, not a day off.
//
// A touch-and-reject at a level IS a PHL/PLH and carries these rules
// unchanged (fold item 2) — the location itself is gated upstream by the
// evaluator's key-level gate (DS-103's item 1).
func PHLPLHGated(t Touch, oldExtreme Level, extremeIdx, barIdx int, cfg Config, htf HTF, day DayVerdict, dg DayGate) (Intent, bool, string) {
	return PHLPLHGatedR2(t, oldExtreme, extremeIdx, barIdx, 0, cfg, htf, day, dg)
}

// PHLPLHGatedR2 is PHLPLHGated with R2's prior same-role swing: priorSwing is
// the previous same-role swing price (0 = skip the higher-low / lower-high
// check). Wired by DS-103 at the evaluator's PHL/PLH call site (CTO box mail
// 1791003269412: "the PHLPLHR2 call with the prior same-role swing").
func PHLPLHGatedR2(t Touch, oldExtreme Level, extremeIdx, barIdx int, priorSwing float64, cfg Config, htf HTF, day DayVerdict, dg DayGate) (Intent, bool, string) {
	return phlPLHGatedR2(t, oldExtreme, extremeIdx, barIdx, priorSwing, nil, cfg, htf, day, dg, false)
}

// PHLPLHGatedR2Levels is the B15 call the evaluator should use once DS-103
// merges the patch: the same gates with the first-obstacle target.
func PHLPLHGatedR2Levels(t Touch, oldExtreme Level, extremeIdx, barIdx int, priorSwing float64, levels []Level, cfg Config, htf HTF, day DayVerdict, dg DayGate, confluence bool) (Intent, bool, string) {
	return phlPLHGatedR2(t, oldExtreme, extremeIdx, barIdx, priorSwing, levels, cfg, htf, day, dg, confluence)
}

func phlPLHGatedR2(t Touch, oldExtreme Level, extremeIdx, barIdx int, priorSwing float64, levels []Level, cfg Config, htf HTF, day DayVerdict, dg DayGate, confluence bool) (Intent, bool, string) {
	in, ok, reason := phlPLHR2(t, oldExtreme, extremeIdx, barIdx, priorSwing, levels, cfg, confluence)
	if !ok {
		return in, false, reason
	}
	// m462: the ONE shared 4h/1h check — all session by default (D5.1-R9/R11).
	// The reason carries the kind so phlRefusalName names it per kind:
	// phl_htf_no_4h / phl_htf_conflict / phl_htf_side_mismatch.
	if r, why := HTFEntryCheck(htf, in.Side); r != "" {
		return in, false, "HTF direction gate: " + string(r) + " — no trade — " + why
	}
	// A10 (CTO 20:15:49Z): ONE day gate — DayOff AND DayNotMeasured refuse
	// every intraday setup through the same helper (dayGateRefusal).
	if r := dayGateRefusal(day); r != "" {
		return in, false, "day gate: " + r
	}
	if day == DaySpent {
		risk := in.Price - in.Stop
		if in.Side == SideShort {
			risk = in.Stop - in.Price
		}
		// R9: on a spent day (cap 15), skip any setup whose stop is over 15
		if risk > dg.TargetCapPts {
			return in, false, "spent day: stop over the 15-pt cap — skip the setup [derived: D5.1 p1 @20:00 '15 điểm bán' cap + D1.2 p1 @07:48 1:1 floor ⇒ stop ≤ 15]"
		}
	}
	// MENTOR-NY item 1 (supersedes B7/L13): the room rule already ran in
	// phlPLHR2 on the LEVEL target (the first obstacle / the old extreme), and
	// that is the course's measure — "Dù target của em là 15 điểm … từ điểm
	// entry xuống target nó phải từ 30 điểm đổ lên" [D5.3 p1 @09:32–09:50]: the
	// room is to the level even when the take-profit is the 15-pt spent cap.
	// B7/L13 re-measured it on the CAPPED target, which demanded stop ≤ 7.5 on
	// every SPENT day. The cap still shortens the take-profit, and the R9
	// stop ≤ 15 check above keeps the capped take-profit on the 1:1 floor.
	capped := CapTargetForDay(in, day, dg)
	// DS-104 P2-3: on a SPENT day the room to the level must also be ≥
	// RoomMultiple × the capped take-profit — "target 15 → room 30"
	// [D5.3 p1 @09:32–09:50]. in.Target is the level (pre-cap).
	if refuse, why := spentDayRoomRefusal(in.Price, in.Target, capped.Target, day, cfg.RoomMultiple); refuse {
		return in, false, why
	}
	return capped, true, ""
}

// phlFlatExtremeRefusal — m465 (release #60, D2.2-R64/R65): a PHL needs a low
// HIGHER than the previous low, and a FLAT low is not one: "đang đi kiếm một
// cái lệnh có đáy cao hơn đáy trước đó; mà ngay chỗ này nó ngang phè" [D2.2 p3
// @07:44–09:46]; "Cái chỗ này anh đang gọi nó là failure to go lower"
// [@09:46–10:15] (on screen the pink box 29,369.00–~29,380.25 over the repeated
// lows). The strict check in phlPLHR2 (stop <= priorSwing →
// phl_not_higher_low) let a low one tick higher through. "Flat" is the
// owner-ruled m428 R2 "same price" measure (flatNearPts — half the average
// candle range of the last 20 closed traded-frame candles), so no number is
// invented here; near is passed in by the caller (phlFlatNear).
//
// Long: refused phl_flat_low_is_ftgl when 0 < ref.Low − priorSwing ≤ near.
// Short (mirror, "a flat high is an FTGH, not a PLH" [D2.2 p3 @14:03–14:22]):
// refused plh_flat_high_is_ftgh when 0 < priorSwing − ref.High ≤ near. A low at
// or under the prior low is left to phl_not_higher_low. The flat pair itself is
// already a flat support/resistance LEVEL (trendline_flat.go), so the FTGL
// reading of the same place stays tradeable. Knob PHLFlatLow "strict" = the
// pre-#60 strict comparison only (returns ""). priorSwing 0 = no check.
func phlFlatExtremeRefusal(cfg Config, side Side, ref market.Kline, priorSwing, near float64) string {
	if cfg.PHLFlatLow == PHLFlatLowStrict || priorSwing == 0 || near <= 0 {
		return ""
	}
	switch side {
	case SideLong:
		if d := ref.Low - priorSwing; d > 0 && d <= near {
			return "phl_flat_low_is_ftgl"
		}
	case SideShort:
		if d := priorSwing - ref.High; d > 0 && d <= near {
			return "plh_flat_high_is_ftgh"
		}
	}
	return ""
}

// phlFlatNear is the m428 R2 "same price" distance for the flat-low check, read
// on the PHL's execution frame (execTFMin: 1, 2 or 5 minutes) over the candles
// closed at or before execCur — the candle the PHL reference is read on.
func phlFlatNear(bars []market.Kline, execTFMin int, execCur market.Kline) float64 {
	fb := bars
	if execTFMin > 1 {
		fb = closedBucketsTF(bars, execTFMin, execCur.CloseTime+1)
	}
	if len(fb) == 0 {
		return 0
	}
	return flatNearPts(fb, len(fb)-1)
}

// phlPullbackRefusal — G41 D2.2-R59/R64 (m466 P2, release #61): the higher-low
// test reads the WHOLE pullback, not only the signal candle. "nghĩa là anh
// đang đi kiếm một cái lệnh có đáy cao hơn đáy trước đó" [D2.2 p3
// @08:25–09:25]; the lows are compared with the lows on the left [@02:51–03:41].
// phlPLHR2's check (stop <= priorSwing → phl_not_higher_low) reads only the
// signal candle's low, so a pullback that had ALREADY printed a lower low — the
// structure shifted — passed as soon as its newest candle's low sat higher.
//
// Long: refused phl_pullback_lower_low when the lowest Low of
// bars[exIdx+1 .. refIdx] (the candles after the old high up to and including
// the signal candle) is at or under prior (the leg-start low,
// priorLeftExtremeOnTape). Short mirror: the highest High at or over prior →
// plh_pullback_higher_high. exIdx is the old extreme's 1m index, refIdx the
// signal candle's (phlRefIdx), ref the signal candle itself. prior 0, an empty
// pullback (refIdx <= exIdx), a signal candle that itself fails the test (left
// to phl_not_higher_low / phl_not_lower_high downstream) or knob
// PHLPullbackLow "signal" → "" (the pre-#61 signal-only check stands).
// The m464 rolling ladder keeps its own rules and does not call this.
func phlPullbackRefusal(cfg Config, side Side, bars []market.Kline, exIdx, refIdx int, ref market.Kline, prior float64) string {
	if cfg.PHLPullbackLow == PHLPullbackLowSignal || prior == 0 || exIdx < 0 || refIdx <= exIdx {
		return ""
	}
	// The signal candle's OWN extreme at or beyond prior is phlPLHR2's
	// phl_not_higher_low / phl_not_lower_high — that name stands.
	if side == SideLong && ref.Low <= prior || side == SideShort && ref.High >= prior {
		return ""
	}
	if refIdx >= len(bars) {
		refIdx = len(bars) - 1
	}
	if refIdx <= exIdx {
		return ""
	}
	switch side {
	case SideLong:
		lo := bars[exIdx+1].Low
		for i := exIdx + 2; i <= refIdx; i++ {
			if bars[i].Low < lo {
				lo = bars[i].Low
			}
		}
		if lo <= prior {
			return "phl_pullback_lower_low"
		}
	case SideShort:
		hi := bars[exIdx+1].High
		for i := exIdx + 2; i <= refIdx; i++ {
			if bars[i].High > hi {
				hi = bars[i].High
			}
		}
		if hi >= prior {
			return "plh_pullback_higher_high"
		}
	}
	return ""
}

// phlRefIdx is the 1m index of the signal (touch reference) candle: the last
// 1m bar that closed at or before ref's close — on a 2m/5m exec frame that is
// the bucket's last minute, so bars[exIdx+1 .. refIdx] covers every minute of
// the signal candle. -1 when no bar qualifies.
func phlRefIdx(bars []market.Kline, ref market.Kline) int {
	i := sort.Search(len(bars), func(i int) bool { return bars[i].CloseTime > ref.CloseTime })
	return i - 1
}
