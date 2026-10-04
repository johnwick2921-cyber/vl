package mentor

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
	return phlPLHR2(t, oldExtreme, extremeIdx, barIdx, priorSwing, nil, cfg)
}

// PHLPLHR2Levels is PHLPLHR2 with B15 (CTO 20:48:40Z): the target is the
// FIRST obstacle in the way — the nearest level beyond the entry (the same
// nextLevelBeyond the ISB and box paths use), capped at the old extreme
// minus PHLTargetShyPts [D3.3 p1 @05:18-05:34: "target là về những level kế
// tiếp… những cái mà nó ngán đường trên đường đi"]. The room rule and the
// 1:1 floor are measured to that target. nil levels = the old behaviour.
func PHLPLHR2Levels(t Touch, oldExtreme Level, extremeIdx, barIdx int, priorSwing float64, levels []Level, cfg Config) (Intent, bool, string) {
	return phlPLHR2(t, oldExtreme, extremeIdx, barIdx, priorSwing, levels, cfg)
}

// phlPLHR2 is the shared PHL/PLH core.
func phlPLHR2(t Touch, oldExtreme Level, extremeIdx, barIdx int, priorSwing float64, levels []Level, cfg Config) (Intent, bool, string) {
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
	// R2: the buy stop sits at the PREVIOUS candle's high; stop = the low
	// of that broken candle. The previous candle = the touch reference.
	price := t.RefBar.High
	stop := t.RefBar.Low
	if side == SideShort {
		price = t.RefBar.Low
		stop = t.RefBar.High
	}
	if priorSwing != 0 {
		if side == SideLong && stop <= priorSwing {
			return Intent{}, false, "not a higher low — a flat low is an FTGL, not a PHL [D2.2 p1 R2]"
		}
		if side == SideShort && stop >= priorSwing {
			return Intent{}, false, "not a lower high — a flat high is an FTGH, not a PLH [D2.2 p1 R2]"
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
	if risk > cfg.StopCeilingPts {
		return Intent{}, false, "stop over the 25-pt ceiling — not worth trading [D3.3 p1 @ 02:04]"
	}
	// E-2: the D1.2 floor is checked BEFORE the room knob — when the
	// target is closer than the stop, the floor is the binding constraint
	// and is the reason the ledger names (the room rule cannot pass when
	// the floor fails; roomMultiple >= 2).
	if reward < risk {
		return Intent{}, false, targetCloserThanStopReason
	}
	if reward < cfg.RoomMultiple*risk {
		return Intent{}, false, "room rule: reward < " + fnum(cfg.RoomMultiple) + "x risk — not enough room [D5.3 p1 @ 09:16]"
	}
	setup := "PHL"
	if side == SideShort {
		setup = "PLH"
	}
	return Intent{
		Action: PlaceStopEntry,
		Setup:  setup,
		Side:   side,
		Price:  price,
		Stop:   stop,
		Target: target,
		Reason: "PHL/PLH: buy stop at the previous candle's high, stop at the broken candle's low, target near the old extreme [D2.2 p1 @ 19:34, 04:58, 07:33]",
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
	ob := nextLevelBeyond(levels, price, side)
	if ob == 0 {
		return target
	}
	switch side {
	case SideLong:
		if ob < target {
			return ob // a key level / EMA 34 / box edge stands in the way
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
//		flips [D4.4 p1 @ 16:00].
//
//	  §7 (fold item 4): a SPENT day with a 4h/1h conflict shuts the day off
//	  (latched at the 08:30 read — a later 1h flip does not reopen it); a
//	  spent day that agrees caps the target at TargetCapPts
//	  ("15 điểm bán, 10 điểm bán") [D5.1 p1 @ 15:57]; an unmeasured run
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
	return phlPLHGatedR2(t, oldExtreme, extremeIdx, barIdx, priorSwing, nil, cfg, htf, day, dg)
}

// PHLPLHGatedR2Levels is the B15 call the evaluator should use once DS-103
// merges the patch: the same gates with the first-obstacle target.
func PHLPLHGatedR2Levels(t Touch, oldExtreme Level, extremeIdx, barIdx int, priorSwing float64, levels []Level, cfg Config, htf HTF, day DayVerdict, dg DayGate) (Intent, bool, string) {
	return phlPLHGatedR2(t, oldExtreme, extremeIdx, barIdx, priorSwing, levels, cfg, htf, day, dg)
}

func phlPLHGatedR2(t Touch, oldExtreme Level, extremeIdx, barIdx int, priorSwing float64, levels []Level, cfg Config, htf HTF, day DayVerdict, dg DayGate) (Intent, bool, string) {
	in, ok, reason := phlPLHR2(t, oldExtreme, extremeIdx, barIdx, priorSwing, levels, cfg)
	if !ok {
		return in, false, reason
	}
	if htfOK, side, htfReason := HTFVerdict(htf); !htfOK {
		return in, false, "HTF direction gate: no trade — " + htfReason
	} else if side != in.Side {
		return in, false, "HTF direction gate: entry side " + string(in.Side) + " against the " + string(side) + " trigger — entries only with the 4h direction [D4.4 p1 @ 16:00]"
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
			return in, false, "spent day: stop over the 15-pt cap — skip the setup [R9, D1.2 p1 @ 07:48–09:00]"
		}
	}
	return CapTargetForDay(in, day, dg), true, ""
}
