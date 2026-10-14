package kernel

import (
	"fmt"
	"vl/logger"
	"vl/market"
	"vl/telemetry"
)

// GateRefusalError is a NON-FIXABLE risk-gate refusal (C6 executor plan gate).
// The bounded parse-retry loop must NOT feed it back to the model as a parse
// error — the model cannot fix a missing/dead plan, and yesterday the three
// blind NY proposals each burned a retry loop instead of surfacing as a clean
// logged risk_check_error (F2, 2026-08-27-london-drought.md).
type GateRefusalError struct{ Reason string }

func (e *GateRefusalError) Error() string { return e.Reason }

// futuresMaxNotionalLeverage is the notional sanity ceiling for CME futures
// in the risk gate: max position notional = equity × this. Futures are
// leveraged instruments (a $50k account holds ~$60k MNQ notional on ~$2.2k
// margin), so the crypto equity×ratio cap is wrong. This is a coarse ceiling
// that rejects absurd sizes; precise contract sizing + clamp happens in the
// executor (auto_trader_orders.go).
const futuresMaxNotionalLeverage = 20.0

// ============================================================================
// Decision Validation
// ============================================================================

func validateDecisions(decisions []Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int, btcEthPosRatio, altcoinPosRatio float64, minRiskReward float64, minConfidence int, maxNotionalLev float64, ctx *Context) error {
	for i := range decisions {
		if err := validateDecision(&decisions[i], accountEquity, btcEthLeverage, altcoinLeverage, btcEthPosRatio, altcoinPosRatio, minRiskReward, minConfidence, maxNotionalLev, ctx); err != nil {
			return fmt.Errorf("decision #%d validation failed: %w", i+1, err)
		}
	}
	return nil
}

// validateDecision code-enforces the trade rules on one decision. ctx (nil-safe)
// supplies the F1 entry reference (MarketDataMap[symbol].CurrentPrice) and the
// TraderID for the rr_gate counter; pass nil in unit tests that don't exercise R:R.
func validateDecision(d *Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int, btcEthPosRatio, altcoinPosRatio float64, minRiskReward float64, minConfidence int, maxNotionalLev float64, ctx *Context) error {
	validActions := map[string]bool{
		"open_long":   true,
		"open_short":  true,
		"close_long":  true,
		"close_short": true,
		"hold":        true,
		"wait":        true,
	}

	if !validActions[d.Action] {
		return fmt.Errorf("invalid action: %s", d.Action)
	}

	if d.Action == "open_long" || d.Action == "open_short" {
		maxLeverage := altcoinLeverage
		posRatio := altcoinPosRatio
		maxPositionValue := accountEquity * posRatio
		isFutures := market.IsCMEFuturesSymbol(d.Symbol)
		switch {
		case isFutures:
			// CME futures: a notional sanity ceiling (equity × the configured
			// multiplier; Chunk 3 made it editable, default 20). maxNotionalLev<=0
			// → the cap is DISABLED (master/toggle off): use a huge ceiling so it
			// never binds. The executor does the precise contract sizing + clamp.
			if maxNotionalLev > 0 {
				maxPositionValue = accountEquity * maxNotionalLev
			} else {
				maxPositionValue = accountEquity * 1e9
			}
		case d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT":
			maxLeverage = btcEthLeverage
			posRatio = btcEthPosRatio
			maxPositionValue = accountEquity * posRatio
		}

		if d.Leverage <= 0 {
			return fmt.Errorf("leverage must be greater than 0: %d", d.Leverage)
		}
		if d.Leverage > maxLeverage {
			logger.Infof("⚠️  [Leverage Fallback] %s leverage exceeded (%dx > %dx), auto-adjusting to limit %dx",
				d.Symbol, d.Leverage, maxLeverage, maxLeverage)
			d.Leverage = maxLeverage
		}
		if d.PositionSizeUSD <= 0 {
			return fmt.Errorf("position size must be greater than 0: %.2f", d.PositionSizeUSD)
		}

		const minPositionSizeGeneral = 12.0
		const minPositionSizeBTCETH = 60.0

		if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
			if d.PositionSizeUSD < minPositionSizeBTCETH {
				return fmt.Errorf("%s opening amount too small (%.2f USDT), must be ≥%.2f USDT", d.Symbol, d.PositionSizeUSD, minPositionSizeBTCETH)
			}
		} else {
			if d.PositionSizeUSD < minPositionSizeGeneral {
				return fmt.Errorf("opening amount too small (%.2f USDT), must be ≥%.2f USDT", d.PositionSizeUSD, minPositionSizeGeneral)
			}
		}

		tolerance := maxPositionValue * 0.01
		if d.PositionSizeUSD > maxPositionValue+tolerance {
			switch {
			case isFutures:
				return fmt.Errorf("%s futures notional cannot exceed %.0f USD (%.0fx account equity), actual: %.0f", d.Symbol, maxPositionValue, maxNotionalLev, d.PositionSizeUSD)
			case d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT":
				return fmt.Errorf("BTC/ETH single coin position value cannot exceed %.0f USDT (%.1fx account equity), actual: %.0f", maxPositionValue, posRatio, d.PositionSizeUSD)
			default:
				return fmt.Errorf("altcoin single coin position value cannot exceed %.0f USDT (%.1fx account equity), actual: %.0f", maxPositionValue, posRatio, d.PositionSizeUSD)
			}
		}
		if d.StopLoss <= 0 || d.TakeProfit <= 0 {
			return fmt.Errorf("stop loss and take profit must be greater than 0")
		}

		if d.Action == "open_long" {
			if d.StopLoss >= d.TakeProfit {
				return fmt.Errorf("for long positions, stop loss price must be less than take profit price")
			}
		} else {
			if d.StopLoss <= d.TakeProfit {
				return fmt.Errorf("for short positions, stop loss price must be greater than take profit price")
			}
		}

		// F1 — REAL risk:reward, computed from the ACTUAL entry reference. The old
		// code synthesized the entry 20% into the SL→TP range, which pinned R:R at
		// exactly 4.0 and made min_risk_reward_ratio a TAUTOLOGY (3.0 always passed;
		// >4 could never pass). Entry reference for a MARKET futures/crypto entry is
		// the current-price snapshot at decision time (the prompt's snapshot); an
		// explicit AI limit price (d.Price) wins when supplied. Wrong-side / zero-risk
		// is validateDecision's authority (B2 price-sanity is absolute-distance only),
		// so a non-positive risk is rejected HERE — it does not duplicate B2.
		entryRef := d.Price
		entrySource := "ai_limit_price"
		if entryRef <= 0 {
			entrySource = "current_price_snapshot"
			if ctx != nil {
				if md := ctx.MarketDataMap[d.Symbol]; md != nil {
					entryRef = md.CurrentPrice
				}
			}
		}

		// STRATEGY STUDIO PHASE 1: the R/R floor is the per-strategy
		// min_risk_reward_ratio (CODE ENFORCED), not a hardcoded 3.0. The store
		// clamps a saved value to ≥1; this ≤0 fallback only guards an unset call.
		effRR := minRiskReward
		if effRR <= 0 {
			effRR = 3.0
		}

		if entryRef <= 0 {
			// No entry reference (no market data for the symbol) → cannot honestly
			// evaluate R:R. Fail-open: skip the gate and let the stale-data /
			// price-sanity gates handle a dead feed. Never fabricate an entry.
			logger.Infof("ℹ️ R:R gate SKIPPED for %s %s — no entry reference (no market data); deferring to price-sanity / stale-data gates.", d.Symbol, d.Action)
		} else {
			var risk, reward float64
			if d.Action == "open_long" {
				risk = entryRef - d.StopLoss
				reward = d.TakeProfit - entryRef
			} else {
				risk = d.StopLoss - entryRef
				reward = entryRef - d.TakeProfit
			}
			traderID := ""
			if ctx != nil {
				traderID = ctx.TraderID
			}
			if risk <= 0 {
				// Stop at/through the entry → no risk to reward. This is the
				// wrong-side condition validateDecision owns (B2 doesn't catch it).
				telemetry.IncGateBlock(traderID, "rr_gate")
				logger.Warnf("📐 R:R REJECT %s %s: entry=%.4f (%s) SL=%.4f TP=%.4f → risk=%.4f ≤ 0 (stop at/through entry) — no risk to reward.",
					d.Symbol, d.Action, entryRef, entrySource, d.StopLoss, d.TakeProfit, risk)
				return fmt.Errorf("invalid risk: entry %.4f is at/through stop %.4f for %s (risk=%.4f ≤ 0) — no risk to reward", entryRef, d.StopLoss, d.Action, risk)
			}
			rr := reward / risk
			verdict := "PASS"
			if rr < effRR {
				verdict = "FAIL"
			}
			logger.Infof("📐 R:R eval %s %s: entry=%.4f (%s) SL=%.4f TP=%.4f → risk=%.4f reward=%.4f R:R=%.2f (min %.2f) → %s",
				d.Symbol, d.Action, entryRef, entrySource, d.StopLoss, d.TakeProfit, risk, reward, rr, effRR, verdict)
			if rr < effRR {
				telemetry.IncGateBlock(traderID, "rr_gate")
				return fmt.Errorf("risk/reward ratio too low (%.2f:1), must be ≥%.2f:1 [entry %.4f (%s) risk %.4f reward %.4f] [stop loss: %.4f take profit: %.4f]",
					rr, effRR, entryRef, entrySource, risk, reward, d.StopLoss, d.TakeProfit)
			}
		}

		// STRATEGY STUDIO PHASE 1: min confidence is now CODE ENFORCED — reject a
		// low-confidence open when min_confidence is configured (0 = disabled →
		// back-compat for strategies that never set it). Applies to crypto + futures.
		if minConfidence > 0 && d.Confidence < minConfidence {
			return fmt.Errorf("confidence too low (%d), must be ≥%d to open position", d.Confidence, minConfidence)
		}

		// A3 (2026-08-26) — MIN-SL VALIDATION. Research-grounded: 15/27 week
		// losers stopped-too-tight (MFE ≥ 0.5×SL before the stop-out). Two legs:
		//  (1) |entry−SL| ≥ MIN_SL_ATR_MULT × ATR(14, 5m Wilder) — env, default
		//      1.5, 0 = off. The ATR rides ctx.Structure["5m"].Atr (same series
		//      the swing engine uses; no hardcoded width).
		//  (2) Level clearance: for a cited scenario, the SL must sit BEYOND the
		//      anchor level/zone far edge by ≥ MinSLTickClearance ticks — stops
		//      parked at the level itself get run (week evidence: 5/5 biggest
		//      losers stopped 5–44 pts from any seated level).
		// Refusal feeds the existing re-validate retry ("sl_too_tight: … widen or
		// skip"). Fail-open: no ATR / no anchor → WARN + pass.
		if ctx != nil && (d.Action == "open_long" || d.Action == "open_short") {
			if mult := MinSLATRMult(); mult > 0 && entryRef > 0 && d.StopLoss > 0 {
				atr := 0.0
				if st, ok := ctx.Structure["5m"]; ok {
					atr = st.Atr
				}
				if atr > 0 {
					dist := 0.0
					if d.Action == "open_long" {
						dist = entryRef - d.StopLoss
					} else {
						dist = d.StopLoss - entryRef
					}
					if blocked, msg := MinSLVerdict(d.Action, dist, atr, mult); blocked {
						telemetry.IncGateBlock(ctx.TraderID, "min_sl_gate")
						logger.Warnf("🛑 MIN-SL REJECT %s %s: %s", d.Symbol, d.Action, msg)
						return fmt.Errorf("%s", msg)
					}
				} else {
					logger.Warnf("🛑 MIN-SL SKIPPED %s %s — no 5m ATR in structure snapshot (fail-open)", d.Symbol, d.Action)
				}
				// Leg 2 — level clearance for a cited scenario anchor.
				if anchor, ok := MinSLAnchorFor(ctx, d); ok {
					tick := market.FuturesTickSize(d.Symbol)
					if tick <= 0 {
						tick = 0.25
					}
					clear := float64(MinSLTickClearance) * tick
					violated := false
					if d.Action == "open_long" {
						violated = d.StopLoss > anchor-clear
					} else {
						violated = d.StopLoss < anchor+clear
					}
					if violated {
						telemetry.IncGateBlock(ctx.TraderID, "min_sl_gate")
						return fmt.Errorf("sl_too_tight: stop %.2f does not clear the cited level %.2f by ≥%d tick(s) — widen or skip", d.StopLoss, anchor, MinSLTickClearance)
					}
				}
			}
		}

		// G1 (regime wave 2026-08-21) — HTF VETO. Position in the gate chain:
		// AFTER the min-confidence gate, BEFORE the decision proceeds to sizing
		// and execution. An entry opposing the CONFIRMED HTF trend (G2) is
		// refused; RANGING/unconfirmed and detector-unavailable FAIL OPEN
		// (WARN + pass). ctx == nil (unit tests) → gate dormant.
		if ctx != nil && ctx.HTFVetoEnabled {
			if blocked, msg := HTFVetoVerdict(ctx.Structure, d.Action, ctx.HTFVetoTF); blocked {
				telemetry.IncGateBlock(ctx.TraderID, "htf_veto")
				logger.Warnf("🛡️ HTF VETO %s %s: %s", d.Symbol, d.Action, msg)
				return fmt.Errorf("%s", msg)
			}
		}

		// G4 (regime wave 2026-08-21) — TRANSITION STAND-DOWN: while an
		// unconfirmed counter-trend CHoCH/MSS is outstanding on the plan's bias
		// TF, NEW entries in the PLAN'S direction are paused (the card shows
		// "⏸ TRANSITION"). Counter-direction entries are never paused by this —
		// the flip owns that job. ctx == nil → dormant.
		if ctx != nil && ctx.TransitionActive {
			if blocked, msg := TransitionStanddownVerdict(d.Action, ctx.TransitionActive, ctx.TransitionDir, ctx.TransitionDetail); blocked {
				telemetry.IncGateBlock(ctx.TraderID, "transition_standdown")
				logger.Warnf("⏸ TRANSITION STAND-DOWN %s %s: %s", d.Symbol, d.Action, msg)
				return fmt.Errorf("%s", msg)
			}
		}

		// C6 (2026-08-25) — EXECUTOR DEAD-PLAN GATE: when the active day plan
		// is machine-dead (or planless with day_plan on), NEW entries are
		// refused. Position management (closes/trails) is NOT blocked.
		if ctx != nil && ctx.ExecutorPlanDead != "" {
			if blocked, msg := ExecutorPlanDeadVerdict(d.Action, ctx.ExecutorPlanDead); blocked {
				telemetry.IncGateBlock(ctx.TraderID, "executor_plan_dead")
				logger.Warnf("🚧 EXECUTOR PLAN GATE %s %s: %s", d.Symbol, d.Action, msg)
				// HONEST C6 (2026-08-27): a typed gate refusal — returned to the
				// caller as a clean, logged risk_check_error, NEVER a parse-error
				// retry loop (the model cannot fix a missing plan).
				return &GateRefusalError{Reason: msg}
			}
		}

		// R4 (2026-08-25) — min_scenario_quality gate: with a floor of A or B,
		// an entry citing a scenario graded below the floor is refused. Default
		// C = no restriction (today's behavior byte-identical). ctx == nil →
		// dormant. Position management is NOT blocked.
		if ctx != nil && ctx.MinScenarioQuality != "" && ctx.MinScenarioQuality != "C" {
			if blocked, msg := MinScenarioQualityVerdict(d.Action, d.CitedScenario, ctx.MinScenarioQuality, ctx.PlanScenarioQuality); blocked {
				telemetry.IncGateBlock(ctx.TraderID, "scenario_below_min_quality")
				logger.Warnf("🚧 SCENARIO QUALITY GATE %s %s: %s", d.Symbol, d.Action, msg)
				return fmt.Errorf("%s", msg)
			}
		}
	}

	return nil
}
