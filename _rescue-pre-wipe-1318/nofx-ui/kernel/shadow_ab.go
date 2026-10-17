package kernel

import (
	"math"
	"strings"

	"nofx/market"
)

// SHADOW A/B COUNTERFACTUALS (E8, entry-mechanics 2026-08-30) — Sep-9's
// courtroom table. Per armed/authored setup, compute 4 counterfactual fills
// (touch · 1x5m_close · 2x5m_close · 1m_mss) by 1m replay: fill px, MFE, MAE,
// target/stop outcome, time-to-fill. PURE — computes only; the logger writes.
// ZERO effect on real paths (nothing here feeds a gate, a prompt, or a wire).

// ShadowABRow is one counterfactual entry's replay verdict. 0C (2026-08-31)
// extends it to the FULL would-have-been trade: intrabar MFE/MAE in points,
// R-multiples AND ATR units, time-to-MFE / time-to-resolution in bars, the
// net-of-friction P&L, and the AMBIGUOUS flag.
type ShadowABRow struct {
	Rule         string  // touch | 1x5m_close | 2x5m_close | 1m_mss
	FillPx       float64 // the counterfactual fill (0 = never filled)
	MFE          float64 // max favorable excursion in pts
	MAE          float64 // max adverse excursion in pts
	Outcome      string  // target | stop | open
	TimeToFillMs int64   // fill bar open − sinceMs
	// 0C extension — the complete would-have-been trade.
	StopPx            float64 // authored stop (original sign, long convention)
	TargetPx          float64 // authored target
	RR                float64 // authored reward:risk (from fill)
	MFER              float64 // MFE in R-multiples
	MAER              float64 // MAE in R-multiples
	MFEATR            float64 // MFE in ATR units (ATR supplied by the logger)
	MAEATR            float64 // MAE in ATR units
	TimeToMFEBars     int     // bars from fill to the MFE peak
	TimeToMAEBars     int     // bars from fill to the MAE trough
	TimeToResolveBars int     // bars from fill to the stop/target bar (0 if still open)
	NetPnL            float64 // net-of-friction P&L in USD (friction below)
	Ambiguous         bool    // a replay bar contained BOTH stop and target
}

// ShadowFrictionTicks is the round-trip friction assumed for counterfactual
// net-of-friction P&L: 2 ticks per contract (1 tick adverse slippage per side).
// Documented so the Sep-9 court can re-price it — never a hidden assumption.
const ShadowFrictionTicks = 2.0

// ShadowABForScenario computes the counterfactual rows for ONE armed scenario.
// Needs the arm bracket (entry ref from Confirm.RefPrice; stop/target from the
// arm) — non-armed scenarios have no bracket to replay and return nil.
// symbol names the instrument for point value + tick friction (MNQ default).
func ShadowABForScenario(sc PlanScenario, bars []market.Kline, symbol string, sinceMs, nowMs int64) []ShadowABRow {
	if sc.Arm == nil || !sc.Arm.Enabled || sc.Confirm == nil || sc.Confirm.RefPrice <= 0 {
		return nil
	}
	ref := sc.Confirm.RefPrice
	stop, target := sc.Arm.Stop, sc.Arm.Target
	if stop <= 0 || target <= 0 {
		return nil
	}
	pv := market.FuturesPointValue(symbol)
	if pv <= 0 {
		pv = 2 // MNQ $2/pt
	}
	tick := market.FuturesTickSize(symbol)
	if tick <= 0 {
		tick = 0.25
	}
	frictionUSD := ShadowFrictionTicks * tick * pv
	above := strings.EqualFold(sc.Confirm.Side, "above")
	long := strings.EqualFold(strings.TrimSpace(sc.Direction), "long")
	if !long {
		// mirror signs so MFE is always favorable-positive in the replay
		// (short: price DOWN is favorable).
		stop, target = -stop, -target
		ref = -ref
		above = !above
	}
	w := BarsSince(bars, sinceMs)
	if len(w) == 0 {
		return nil
	}
	type fillAt struct {
		rule   string
		barIdx int
		px     float64
	}
	var fills []fillAt
	// touch — the first bar whose range straddles the level fills AT the level.
	for i := range w {
		b := w[i]
		if b.CloseTime >= nowMs {
			continue
		}
		if b.Low <= ref && b.High >= ref {
			fills = append(fills, fillAt{"touch", i, ref})
			break
		}
	}
	// close rules — the qualifying close beyond ref in side; 1x/2x count
	// CONSECUTIVE 5m closes. The fill bar is the FIRST 1m bar of the
	// qualifying 5m bucket, found by OpenTime (bucket boundaries are
	// absolute-epoch aligned, so index math like i*5 is WRONG when the window
	// starts mid-bucket or spans <5 bars — the 2026-08-30 cutover panic).
	barIdxForBucket := func(bucketOpenMs int64) int {
		for j := range w {
			if w[j].OpenTime >= bucketOpenMs {
				return j
			}
		}
		return 0
	}
	closeFill := func(rule string, need int) fillAt {
		five := AggregateBars(w, 5*60_000)
		run := 0
		for i := range five {
			b := five[i]
			if b.CloseTime >= nowMs {
				continue
			}
			beyond := (above && b.Close > ref) || (!above && b.Close < ref)
			if beyond {
				run++
				if run >= need {
					return fillAt{rule, barIdxForBucket(b.OpenTime), b.Close}
				}
			} else {
				run = 0
			}
		}
		return fillAt{}
	}
	if f := closeFill("1x5m_close", 1); f.rule != "" {
		fills = append(fills, f)
	}
	if f := closeFill("2x5m_close", 2); f.rule != "" {
		fills = append(fills, f)
	}
	// 1m_mss — the break bar close (index into w so the replay starts there).
	if m := EvaluateMSS(w, sc.Confirm.Side, nowMs); m.Met {
		idx := -1
		for i := range w {
			if w[i].OpenTime == m.BreakTimeMs {
				idx = i
				break
			}
		}
		fills = append(fills, fillAt{"1m_mss", idx, m.BreakClose})
	}
	if len(fills) == 0 {
		return nil
	}
	out := make([]ShadowABRow, 0, len(fills))
	for _, f := range fills {
		row := ShadowABRow{Rule: f.rule, FillPx: f.px}
		if row.FillPx <= 0 {
			continue
		}
		// Replay from the fill bar forward: MFE/MAE vs stop/target. Intrabar
		// stop+target ambiguity resolves AGAINST the trade (stop first, R9) and
		// is FLAGGED (0C) — ambiguous rows must never pass as clean wins.
		start := 0
		if f.barIdx >= 0 && f.barIdx < len(w) {
			start = f.barIdx
			row.TimeToFillMs = w[f.barIdx].OpenTime - sinceMs
		}
		row.StopPx, row.TargetPx = sc.Arm.Stop, sc.Arm.Target
		risk := row.FillPx - stop // favorable-sign convention: risk is positive
		if risk > 0 {
			row.RR = (target - row.FillPx) / risk
		}
		mfe, mae, outcome := 0.0, 0.0, "open"
		mfeBar, maeBar := 0, 0
		resolvedBar := -1
		ambiguous := false
		// The fill bar itself may contain BOTH stop and target — flag it too.
		for i := start; i < len(w); i++ {
			b := w[i]
			if b.CloseTime >= nowMs {
				continue
			}
			hi, lo := b.High, b.Low
			if !long {
				hi, lo = -lo, -hi
			}
			if lo <= stop && hi >= target {
				ambiguous = true
			}
			// adverse first (R9): a bar that spans both stop and target counts
			// as a stop-out.
			if lo <= stop {
				mae = math.Max(mae, row.FillPx-stop)
				resolvedBar = i - start
				outcome = "stop"
				break
			}
			if hi >= target {
				mfe = math.Max(mfe, target-row.FillPx)
				resolvedBar = i - start
				outcome = "target"
				break
			}
			pxHi := hi - row.FillPx
			if pxHi > mfe {
				mfe = pxHi
				mfeBar = i - start
			}
			pxLo := row.FillPx - lo
			if pxLo > mae {
				mae = pxLo
				maeBar = i - start
			}
		}
		row.MFE, row.MAE, row.Outcome = mfe, mae, outcome
		row.Ambiguous = ambiguous
		row.TimeToMFEBars = mfeBar
		row.TimeToMAEBars = maeBar
		if resolvedBar >= 0 {
			row.TimeToResolveBars = resolvedBar
		}
		if risk > 0 {
			row.MFER = mfe / risk
			row.MAER = mae / risk
		}
		// Net-of-friction P&L: resolved exit (target|stop) or the last close
		// for still-open rows, times point value, minus round-trip friction.
		exitPx := 0.0
		switch outcome {
		case "target":
			exitPx = target
		case "stop":
			exitPx = stop
		case "open":
			for i := len(w) - 1; i >= start; i-- {
				if w[i].CloseTime < nowMs {
					exitPx = w[i].Close
					break
				}
			}
		}
		if exitPx != 0 {
			row.NetPnL = (exitPx-row.FillPx)*pv - frictionUSD
		}
		out = append(out, row)
	}
	return out
}
