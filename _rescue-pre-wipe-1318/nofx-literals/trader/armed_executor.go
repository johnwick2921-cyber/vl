package trader

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	ntwire "nofx/provider/ninjatrader"
	"nofx/store"
	"nofx/telemetry"
	ntTrader "nofx/trader/ninjatrader"
)

// conditionsBootLogged dedupes the per-trader resolved-condition-map boot log
// (0C shadow demotion, 2026-08-31) — once per trader, like armedSubs.
var conditionsBootLogged sync.Map

// conditionShadowedFor (0C, owner ruling 2026-08-31) resolves one scenario
// condition's live|shadow status through the SAME config chain as every other
// knob: session override > strategy base > env > defaults (class-8: quote the
// RESOLVED value, never the file default).
func (at *AutoTrader) conditionShadowedFor(condition, session string) bool {
	cfg := at.config.StrategyConfig
	if cfg == nil || cfg.DayPlan == nil {
		return kernel.IsConditionShadowed(condition, nil, nil, kernel.ShadowConditionsEnv())
	}
	base := cfg.DayPlan.ConditionStatus
	var sessionMap map[string]string
	if ov := cfg.DayPlan.SessionOverride(session); ov != nil && ov.ConditionStatus != nil {
		sessionMap = *ov.ConditionStatus
	}
	return kernel.IsConditionShadowed(condition, base, sessionMap, kernel.ShadowConditionsEnv())
}

// armedPlaceTicks is the placement band (ARM_PLACE_TICKS, default 100): the
// resting limit is placed once price comes within this many ticks of entry.
func armedPlaceTicks() int {
	if v := os.Getenv("ARM_PLACE_TICKS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 100
}

// armMinRR is the R:R floor for the GATE-AT-ARM chain only
// (ARM_MIN_RR, default 2.0). Autopsy-response wave (2026-08-27): armed limits
// fill AT the level (better entry by construction, no stale risk) and the one
// refused arm replayed +$108 — the global entry floor (3.0) is NOT lowered;
// AI-proposed market entries keep their own gate unchanged.
// R1 (owner ruling 2026-09-03) — ONE R:R FLOOR. The Studio value
// min_risk_reward_ratio governs BOTH the arm seam and the decision path;
// ARM_MIN_RR is DELETED, env and code default alike.
//
// Two floors from two sources was the defect: the bound strategy "MNQ"
// (a5b7662e) carries 2, while armMinRR() returned its own default 2.0 from an
// env var nobody had set. They agreed by coincidence, so nothing looked wrong —
// and a Studio save moving the floor would have moved only one of them.
// Behaviour is unchanged today (both read 2.0); the SOURCE is now single.
func resolvedMinRR(cfg *store.StrategyConfig) float64 {
	// The rule lives in store.ResolveMinRiskReward so the Settings page can
	// narrate the SAME resolution the arm seam performs, source included.
	v, _ := store.ResolveMinRiskReward(cfg)
	return v
}

// armMinRRFor is the arm seam's floor — the SAME resolver the decision path
// uses. Kept as a named function so the call sites read as a policy, not a
// field access.
func (at *AutoTrader) armMinRRFor(cfg *store.StrategyConfig) float64 {
	if cfg == nil && at != nil {
		cfg = at.config.StrategyConfig
	}
	return resolvedMinRR(cfg)
}

// armedWorkingStaleMin is the reconnect/reconcile safety net
// (ARM_WORKING_STALE_MIN, default 15): a working row with no order_update for
// this long is cancelled with an honest reason.
// E7 (entry-mechanics 2026-08-30) — stop-entry knobs. The resolvers live in
// the kernel (kernel.StopEntrySeamOn / StopEntryOffsetTicks / RetestWaitBars)
// so the boot ledger and the executor share one source of truth.
func stopEntrySeamOn() bool     { return kernel.StopEntrySeamOn() }
func stopEntryOffsetTicks() int { return kernel.StopEntryOffsetTicks() }
func retestWaitBars() int       { return kernel.RetestWaitBars() }

// stopEntryFallbackDue (E7, pure) — the breakout-retest fallback window: a
// stop-entry leg is due when NO bar has touched its entry level within the
// last RETEST_WAIT_BARS 1m bars AND at least RETEST_WAIT_BARS bars elapsed
// since the plan's birth (no retest came → chase with a stop beyond the
// break candle instead of waiting forever).
func stopEntryFallbackDue(bars []market.Kline, entryPx, sinceMs, nowMs int64) bool {
	need := retestWaitBars()
	if len(bars) < need {
		return false
	}
	var closedSinceBirth int
	for i := len(bars) - 1; i >= 0 && closedSinceBirth < need; i-- {
		b := bars[i]
		if b.CloseTime >= nowMs {
			continue
		}
		if b.OpenTime < sinceMs {
			break
		}
		closedSinceBirth++
		if b.Low <= float64(entryPx) && b.High >= float64(entryPx) {
			return false // a retest touch came in-window — the limit logic owns it
		}
	}
	return closedSinceBirth >= need
}

func armedWorkingStaleMin() int {
	if v := os.Getenv("ARM_WORKING_STALE_MIN"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 15
}

// armedSubs holds each trader's order_update stream (created once per trader).
var armedSubs sync.Map

// armedTrader type-asserts the bound trader to the TCPTrader (nil when the
// trader is crypto or unwired — the engine then stays dormant).
func (at *AutoTrader) armedTrader() *ntTrader.TCPTrader {
	nt, _ := at.trader.(*ntTrader.TCPTrader)
	return nt
}

// ARMED-ORDER EXECUTOR — Wave 2 (2026-08-27).
//
// PHASE 1 (this file): the ARMING CONTRACT. The AI authorizes arming per
// scenario (plan.scenarios[].arm); Go evaluates gates AT ARM TIME and keeps the
// durable armed_orders ledger. Placement as a working NT8 order is PHASE 2 —
// Phase 1 rows stay state=armed. Everything here is dormant until a plan
// actually carries arm specs (no behavior change to today's trading).

// maybeManageArmedOrders runs every cycle (called from runCycle). It is a no-op
// unless day_plan is on and armed specs exist. snap is the structure snapshot
// for the HTF veto gate.

// declineHadFreshMet (S5, autopsy-response wave) — true when the active plan
// has a scenario whose confirm{} is machine-MET and NOT stale at this instant:
// the honest-wait leak the autopsy quantified (declines while a FRESH confirm
// was live). Mirrors RenderConfirmLines' staleness rule.
func (at *AutoTrader) declineHadFreshMet() bool {
	plan := kernel.ActivePlanFor(at.id, at.futuresSymbol())
	if plan == nil {
		return false
	}
	var bars []market.Kline
	if market.FuturesBarsProvider != nil {
		bars = market.FuturesBarsProvider(at.futuresSymbol(), kernel.AISVPBarInterval, kernel.AISVPBarCount)
	}
	if len(bars) == 0 {
		return false
	}
	nowMs := time.Now().UnixMilli()
	nowPrice := bars[len(bars)-1].Close
	atr5m := market.ExportCalculateATR(kernel.AcceptanceBars(bars, "2x5m"), 14)
	for _, s := range plan.Doc.Scenarios {
		if s.Confirm == nil {
			continue
		}
		v := kernel.EvaluateConfirm(*s.Confirm, bars, plan.BirthMs, nowMs)
		if !v.Met {
			continue
		}
		if atr5m > 0 && math.Abs(nowPrice-s.Confirm.RefPrice) > kernel.StaleConfirmATR()*atr5m {
			continue // stale-MET is NOT the leak class
		}
		return true
	}
	return false
}

func (at *AutoTrader) maybeManageArmedOrders(snap map[string]kernel.StructureState) {
	if !at.dayPlanEnabled() || at.store == nil || at.exchange != "ninjatrader" {
		return
	}
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		return
	}
	// CLASS 33 (2026-09-02) — BOOT SWEEP FIRST. Before ANY authoring, gating,
	// cancelling or placement in this process: every non-terminal row stamped
	// by a DEAD process is cancelled at the broker and in the ledger. This is
	// the head of the armed subsystem, so sweep-before-arm is guaranteed by
	// position — runArmedPlacement is reached from BELOW this line only.
	at.sweepPreBootArms(ledger)
	now := time.Now()

	// 1.4 — plan → dormant/no_trade/absent = ALL its armed orders cancelled
	// instantly. Re-arm does NOT auto-re-arm (fresh AI authorization required).
	// 2.4 — no active session (EOD flat) cancels everything too.
	plan := kernel.ActivePlanFor(at.id, at.futuresSymbol())
	_, sessOK := at.sessionRegistry(now).ActiveSession(now)
	reason := ""
	if plan == nil {
		if !sessOK {
			reason = "session ended (EOD flat)"
		} else {
			reason = "no active plan"
		}
	} else {
		row, err := at.store.Plan().GetLatestPlanForTraderSession(kernel.PlanTradeDateFor(plan), plan.Session, at.id)
		if err != nil || row == nil {
			reason = "plan row unavailable"
		} else if row.Lifecycle != "active" {
			reason = fmt.Sprintf("plan lifecycle %q", row.Lifecycle)
		}
	}
	if reason != "" {
		// S-list closer: synchronous (ack-waited) cancel on session end and
		// dormancy too — the resting limit must be dead BEFORE any flatten or
		// the next cycle, not up to 2m later.
		if n, unacked := at.cancelArmedOrdersSync(reason); n > 0 {
			at.logWarnf("🔒 armed cancel: %s — %d order(s) disarmed", reason, n)
			if unacked > 0 {
				at.logWarnf("⚠️ armed cancel: %d unacked after retry (ledger cancelled; wire reconciles next cycle)", unacked)
			}
		}
		return
	}

	// CLASS 47 F4 (owner-ruled, 2026-09-02) — STALE-ARM EXPIRY. A NEVER-PLACED
	// arm (no broker signal id) whose plan version has been superseded describes
	// a setup the planner already replaced; with no signal id there is nothing
	// at the broker to orphan. On 09-02 one such v5 row stayed non-terminal
	// across six versions and held the class-33 cutover gate's leg 4 shut ~5 h.
	// PLACED/WORKING rows are untouched — they belong to the sweep and the
	// stale-window reconcile, not here.
	if ids, xerr := ledger.SupersedeUnplacedArms(at.id, plan.PlanID, plan.Version); xerr != nil {
		at.logWarnf("⏱ stale-arm expiry failed for %s v%d: %v", plan.PlanID, plan.Version, xerr)
	} else if len(ids) > 0 {
		n := 0
		if c, cerr := store.IncArmSuperseded(at.store); cerr == nil {
			n = c
		}
		at.logWarnf("⏱ stale arms SUPERSEDED (class 47): %d never-placed row(s) %v retired at plan %s v%d — they held the cutover gate open. Recorded total=%d",
			len(ids), ids, plan.PlanID, plan.Version, n)
	}

	// 2. arm evaluation for the ACTIVE plan's arm specs.
	doc := plan.Doc
	cfg := at.GetStrategyConfig()
	if cfg == nil {
		return
	}
	var bars []market.Kline
	if market.FuturesBarsProvider != nil {
		bars = market.FuturesBarsProvider(at.futuresSymbol(), kernel.AISVPBarInterval, kernel.AISVPBarCount)
	}
	// One ATR5m math, both seams (no-trade-rider 2026-09-03): the SAME 5m ATR
	// the decision path's EntryGate reads — never PlanDATRFor (DAILY ATR).
	atr5m := armSeamATR5mFromBars(bars)
	// D4 (2026-09-04): last close, for the far-arm counter. 0 when the tape is
	// empty — farArmFactor treats that as UNKNOWN and never flags.
	armPrice := 0.0
	if len(bars) > 0 {
		armPrice = bars[len(bars)-1].Close
	}
	minQuality := ""
	if dp := at.dayPlanCfg(); dp != nil {
		minQuality = dp.MinScenarioQualityFor(plan.Session)
	}
	// 0C (2026-08-31) — once per trader, print the RESOLVED condition-status
	// map (class-8: resolved, never the file default) so the live journal
	// self-documents the demotion even though the process-level boot line can
	// only see defaults+env.
	if _, logged := conditionsBootLogged.LoadOrStore(at.id, true); !logged {
		base := map[string]string(nil)
		if at.config.StrategyConfig != nil && at.config.StrategyConfig.DayPlan != nil {
			base = at.config.StrategyConfig.DayPlan.ConditionStatus
		}
		at.logInfof("%s (per-trader resolved, 0C shadow demotion)", kernel.ConditionStatusLedger(base, nil, kernel.ShadowConditionsEnv()))
	}
	for _, sc := range doc.Scenarios {
		if sc.Arm == nil || !sc.Arm.Enabled {
			continue
		}
		// E4 (2026-08-30) — split-entry legs: a two-leg arm writes one ledger
		// row PER leg (LegIndex 0/1, LegCount 2). A single arm is leg 0 of a
		// one-row pair (LegCount 0 = legacy shape).
		legs := sc.Arm.Legs
		if len(legs) == 0 {
			// D3 (2026-09-04): the entry TYPE follows the condition, from the
			// same table the planner prompt is rendered from. This was
			// hardcoded "limit", which is wrong for a reclaim — it only
			// becomes valid once price trades back THROUGH the level.
			kind, refusal := armLegKindFor(sc, kernel.PlanArmLeg{})
			if refusal != "" {
				at.logWarnf("✕ armed %s NOT authored — %s", sc.ID, refusal)
				continue
			}
			legs = []kernel.PlanArmLeg{{Entry: sc.Arm.Entry, Stop: sc.Arm.Stop, Target: sc.Arm.Target,
				WaitConfirm: sc.Arm.WaitConfirm, Rule: "touch", Kind: kind}}
		}
		legCount := 0
		if len(sc.Arm.Legs) == 2 {
			legCount = 2
		}
		// FIX 5 (class 27, owner-added 2026-08-31) — split-leg sanity: an arm
		// may not author more legs than the account can hold. leg_count >
		// position capacity = reject AT WRITE. Live proof: S3 authored 2 legs
		// on a 1-lot account and leg 1's fill NETTED the open S1 long (an
		// unlogged exit). Capacity defaults to 1 (every live order sizes to 1
		// contract); an explicit max_contracts_per_order ≥ 2 raises it.
		if capN := at.armLegCapacity(); legCount > capN {
			key := plan.PlanID + ":" + strconv.Itoa(plan.Version) + ":" + sc.ID + ":legcap"
			if armRefusalChanged(&at.armRefusalLast, key, "split_leg_capacity") {
				at.logWarnf("⚔️ arm REFUSED %s %s: split_leg_capacity — authors %d legs but account capacity is %d (class 27; set max_contracts_per_order ≥ 2 to allow split arms)", plan.Session, sc.ID, legCount, capN)
			}
			continue
		}
		// 0C (owner ruling 2026-08-31) — SHADOW DEMOTION. A shadowed condition
		// MAY be authored, validated and E8-scored (that data IS the wave's
		// justification), but NO order may ever reach the wire: the placement
		// engine only acts on state "armed", so a "shadowed" ledger row is inert
		// by construction AND invisible in the executor prompt (armedLines
		// renders armed/working only). Resting orders authored before this
		// ruling are cancelled here — the first cycle after boot IS the
		// boot-time sweep (4.3).
		if at.conditionShadowedFor(sc.Condition, plan.Session) {
			if rows, lerr := ledger.ListNonTerminal(at.id); lerr == nil {
				for _, rr := range rows {
					if rr.TraderID == at.id && rr.PlanID == plan.PlanID && rr.Scenario == sc.ID &&
						(rr.State == "working" || rr.State == "armed") {
						if nt := at.armedTrader(); nt != nil && rr.SignalID != "" {
							if cerr := nt.CancelOrder(rr.SignalID); cerr == nil {
								_ = ledger.SetState(rr.ID, "shadowed", "condition_shadowed")
								at.logWarnf("✕ armed cancel (condition_shadowed): %s %s signal=%s", plan.Session, sc.ID, rr.SignalID)
							}
						} else {
							_ = ledger.SetState(rr.ID, "shadowed", "condition_shadowed")
						}
					}
				}
			}
			telemetry.IncShadowedArmRefusal()
			key := plan.PlanID + ":" + strconv.Itoa(plan.Version) + ":" + sc.ID + ":shadow"
			if armRefusalChanged(&at.armRefusalLast, key, "condition_shadowed") {
				at.logWarnf("⚔️ arm REFUSED %s %s: condition_shadowed (%s is SHADOW — authored + E8-scored, never placed)", plan.Session, sc.ID, sc.Condition)
			}
			// AUTHOR the would-have-been rows in the inert shadowed state —
			// plan/scenario/arm lineage stays on record for the Sep-9 court.
			side := strings.ToLower(strings.TrimSpace(sc.Direction))
			for li, leg := range legs {
				// D3 (2026-09-04): the shadowed author is a SECOND row-construction
				// site and it took the model's kind on trust while the live path
				// derived it. A row written for the record must carry the same
				// kind and condition the live path would have given it, or the
				// record is of something that was never going to be placed.
				shadowKind, shadowRefusal := armLegKindFor(sc, leg)
				if shadowRefusal != "" {
					at.logWarnf("✕ armed %s leg %d NOT authored (shadowed) — %s", sc.ID, li+1, shadowRefusal)
					continue
				}
				row := &store.ArmedOrderDB{
					TraderID: at.id, PlanID: plan.PlanID, Version: plan.Version, Session: plan.Session,
					Scenario: sc.ID, Side: side, EntryPx: leg.Entry, StopPx: leg.Stop, TargetPx: leg.Target,
					State: "shadowed", StateReason: "condition_shadowed", EntryClass: "armed_fill",
					CreatedAt: now, UpdatedAt: now, LegIndex: li, LegCount: legCount,
					Kind: shadowKind, Condition: sc.Condition,
				}
				if existing, err := ledger.ListNonTerminal(at.id); err == nil {
					for i := range existing {
						if existing[i].TraderID == at.id && existing[i].PlanID == row.PlanID && existing[i].Scenario == sc.ID && existing[i].LegIndex == row.LegIndex {
							row.ID = existing[i].ID
							break
						}
					}
				}
				if row.ID != 0 {
					_ = ledger.SetState(row.ID, "shadowed", "condition_shadowed")
				}
				if err := ledger.UpsertArm(row); err != nil {
					at.logWarnf("⚔️ shadowed arm write failed %s %s leg %d: %v", plan.Session, sc.ID, li+1, err)
				}
			}
			continue
		}
		for li, leg := range legs {
			// 0B (2026-09-02) — STOP ANCHORED TO SEATED STRUCTURE. Compose the
			// leg's stop BEFORE every downstream consumer (the gate's R:R and
			// min-SL legs, the ledger row, the churn guard, placement): stop =
			// beyond the nearest seated level on the risk side + clearance,
			// floored at MIN_SL_ATR_MULT×ATR5m, widest wins, never tighter than
			// authored. Logged once per (plan, version, scenario, leg, stop).
			if comp := composeArmStop(strings.ToLower(strings.TrimSpace(sc.Direction)), leg.Entry, leg.Stop, atr5m,
				market.FuturesTickSize(at.futuresSymbol()), doc.Levels, kernel.MinSLATRMult(),
				kernel.MinSLTickClearance, armStopAnchorMaxATR()); comp.Stop != leg.Stop || comp.Unanchored {
				skey := plan.PlanID + ":" + strconv.Itoa(plan.Version) + ":" + sc.ID + ":leg" + strconv.Itoa(li+1) + ":stop"
				if armRefusalChanged(&at.armStopCompLast, skey, fmt.Sprintf("%.2f/%s", comp.Stop, comp.Bound)) {
					at.logInfof("%s", armStopCompositionLine(plan.Session, sc.ID, li+1, sc.Direction, comp, atr5m, kernel.MinSLATRMult()))
					// OWNER RULING 1 (0B): ARM_STOP_ANCHOR_MAX_ATR 3.0 is a
					// PROVISIONAL [I] default, reviewed at n≥30 dead zones. The
					// count is RECORDED (class-35 law), never inferred from logs.
					if comp.Unanchored && at.store != nil {
						if n, cerr := store.IncStopUnanchored(at.store); cerr != nil {
							at.logWarnf("🛑 stop_unanchored counter write failed: %v", cerr)
						} else {
							at.logWarnf("🛑 stop_unanchored %s %s leg %d — no seated level within %.1f×ATR5m on the risk side; ATR floor governs. Recorded n=%d (provisional bound reviewed at n≥%d).",
								plan.Session, sc.ID, li+1, armStopAnchorMaxATR(), n, store.StopUnanchoredReviewN)
						}
					}
				}
				leg.Stop = comp.Stop
			}
			// S2b chained arm (autopsy-response wave): wait_confirm legs stay
			// DORMANT until the chain confirm is machine-MET. E4: leg 1 chains
			// on confirm2 (1m_mss|1x5m_close); a legacy single arm chains on
			// its own confirm{}.
			if leg.WaitConfirm {
				chain := sc.Confirm2
				if len(sc.Arm.Legs) == 0 {
					chain = sc.Confirm
				}
				if chain == nil {
					continue
				}
				v := kernel.EvaluateConfirm(*chain, bars, plan.BirthMs, now.UnixMilli())
				if !v.Met {
					continue
				}
				at.logInfof("⚔️ arm %s leg %d wait_confirm MET (%s) — arming", sc.ID, li+1, leg.Rule)
			}
			// gates AT ARM TIME — a resting order is a pre-passed entry; each gate
			// input that changes materially later triggers a cancel (1.3).
			if verdict := at.armGateVerdictFor(sc, leg, biasDirectionFor(doc.Bias.Direction), snap, atr5m, minQuality, cfg, plan.Session); verdict != "" {
				// F4 (LONDON-FORENSICS 2026-08-28) — log the REFUSED verdict ONCE
				// per arm-spec (the same infeasible arm re-refused every cycle
				// printed ~120 lines/session); silent until the spec changes.
				// GAR-F6 (2026-08-28): the comparison VALUE is the verdict CLASS,
				// not the ATR-bearing string — live ATR drift re-logged the same
				// refusal every few minutes (LONDON S4 min-SL 18.29→18.67).
				// PRE-REOPEN F3 (2026-08-28) — the missing 1.3 clause: a gate input
				// that changes materially while the arm is WORKING cancels it the
				// same cycle (the LONDON S4 class stayed resting through repeated
				// re-refusals until the 08:30 sweep).
				if rows, lerr := ledger.ListNonTerminal(at.id); lerr == nil {
					for _, r := range rows {
						if r.TraderID == at.id && r.PlanID == plan.PlanID && r.Scenario == sc.ID &&
							r.State == "working" && r.SignalID != "" {
							if nt := at.armedTrader(); nt != nil {
								if cerr := nt.CancelOrder(r.SignalID); cerr == nil {
									_ = ledger.SetState(r.ID, "cancelled", "gate changed: "+armRefusalClass(verdict))
									at.logWarnf("✕ armed cancel (gate changed %s): %s %s", armRefusalClass(verdict), plan.Session, sc.ID)
								}
							}
						}
					}
				}
				key := plan.PlanID + ":" + strconv.Itoa(plan.Version) + ":" + sc.ID + ":leg" + strconv.Itoa(li+1)
				if armRefusalChanged(&at.armRefusalLast, key, armRefusalClass(verdict)) {
					// OWNER RULING 2 (0B): more R:R refusals with the wider stops
					// is the intended trade — the COST side of the stop floor.
					// Recorded per session-day and per class (one distinct
					// arm-spec per bump, never per re-refusal cycle) so it can be
					// quoted against the benefit later.
					class := armRefusalClass(verdict)
					shown := ""
					if at.store != nil {
						if n, cerr := store.IncArmRefusal(at.store, at.id, kernel.PlanTradeDateFor(plan), plan.Session, class); cerr != nil {
							at.logWarnf("⚔️ arm refusal counter write failed: %v", cerr)
						} else {
							shown = fmt.Sprintf(" · %s refusals this session: %d", class, n)
						}
					}
					at.logWarnf("⚔️ arm REFUSED %s %s leg %d: %s%s", plan.Session, sc.ID, li+1, verdict, shown)
				}
				continue
			}
			side := strings.ToLower(strings.TrimSpace(sc.Direction))
			// FIX 4 (class 27, owner-added 2026-08-31) — ONE-LIVE-ARM GUARD. On a
			// netting account a second arm is not a second trade: an opposite-side
			// entry fill NETs the open position — an unlogged exit of the first
			// (live proof 2026-08-31: the S3 SellShort fill silently closed the
			// S1 long; its +$92.00 vanished from the ledger for 26 minutes). Refuse
			// opposite-side arm entries while a position is open, and cancel an
			// already-resting opposite-side order the same cycle. The only escape:
			// a leg explicitly authored as an exit/flip leg (kind "exit").
			if verdict := at.oneLiveArmGuard(sc, leg, side); verdict != "" {
				if rows, lerr := ledger.ListNonTerminal(at.id); lerr == nil {
					for _, rr := range rows {
						if rr.TraderID == at.id && rr.PlanID == plan.PlanID && rr.Scenario == sc.ID &&
							rr.LegIndex == li && (rr.State == "working" || rr.State == "armed") && rr.SignalID != "" {
							if nt := at.armedTrader(); nt != nil {
								if cerr := nt.CancelOrder(rr.SignalID); cerr == nil {
									_ = ledger.SetState(rr.ID, "cancelled", "one_live_arm_guard")
									at.logWarnf("✕ armed cancel (one_live_arm_guard): %s %s leg %d", plan.Session, sc.ID, li+1)
								}
							}
						}
					}
				}
				key := plan.PlanID + ":" + strconv.Itoa(plan.Version) + ":" + sc.ID + ":leg" + strconv.Itoa(li+1)
				if armRefusalChanged(&at.armRefusalLast, key, "one_live_arm_guard") {
					at.logWarnf("⚔️ arm REFUSED %s %s leg %d: %s", plan.Session, sc.ID, li+1, verdict)
				}
				continue
			}
			// CLASS 48 — the ONE canonical entry gate, shared with the decision
			// path. The arm chain above (armGateVerdictFor, oneLiveArmGuard,
			// shadow demotion, stop composition) is the arm's own history; this
			// re-runs the SAME function the market entry runs so an arm can never
			// be held to a weaker standard than a decision entry. Refusals are
			// logged AND recorded per path (arm-refusal counters), and an
			// existing resting arm for this spec is cancelled the same cycle.
			if greason, refused := at.entryGateForArm(plan, sc, leg, side, biasDirectionFor(doc.Bias.Direction), atr5m); refused {
				if rows, lerr := ledger.ListNonTerminal(at.id); lerr == nil {
					for _, rr := range rows {
						if rr.TraderID == at.id && rr.PlanID == plan.PlanID && rr.Scenario == sc.ID &&
							rr.LegIndex == li && rr.SignalID != "" {
							if nt := at.armedTrader(); nt != nil {
								if cerr := nt.CancelOrder(rr.SignalID); cerr == nil {
									_ = ledger.SetState(rr.ID, "cancelled", "entry_gate: "+armRefusalClass(greason))
									at.logWarnf("✕ armed cancel (entry_gate): %s %s leg %d", plan.Session, sc.ID, li+1)
								}
							}
						}
					}
				}
				key := plan.PlanID + ":" + strconv.Itoa(plan.Version) + ":" + sc.ID + ":leg" + strconv.Itoa(li+1)
				if armRefusalChanged(&at.armRefusalLast, key, "entry_gate:"+armRefusalClass(greason)) {
					at.recordEntryGateRefusal("arm", at.futuresSymbol(), "open_"+side, greason, plan)
				}
				continue
			}
			// D3: every leg's kind is derived from the condition and an
			// authored contradiction is refused by name (A9 — one line, with
			// the reason).
			legKind, kindRefusal := armLegKindFor(sc, leg)
			if kindRefusal != "" {
				at.logWarnf("✕ armed %s leg %d NOT authored — %s", sc.ID, li+1, kindRefusal)
				continue
			}

			// D4 (2026-09-04) — FAR-ARM COUNTER, WARN-first. Nothing is refused
			// for being far; a week of counts decides the threshold. Per side,
			// because the 09-02 evidence was one-sided.
			telemetry.IncArmAuthored()
			if f := farArmFactor(leg.Entry, armPrice, atr5m); armIsFar(leg.Entry, armPrice, atr5m) {
				telemetry.IncFarArm(side)
				at.logWarnf("📏 arm far: %s %s entry %.2f is %.2f pts / %.1f×ATR5m from price %.2f (counted, not refused)",
					sc.ID, side, leg.Entry, math.Abs(leg.Entry-armPrice), f, armPrice)
			}
			row := &store.ArmedOrderDB{
				TraderID: at.id, PlanID: plan.PlanID, Version: plan.Version, Session: plan.Session,
				Scenario: sc.ID, Side: side, EntryPx: leg.Entry, StopPx: leg.Stop, TargetPx: leg.Target,
				State: "armed", EntryClass: "armed_fill", CreatedAt: now, UpdatedAt: now,
				LegIndex: li, LegCount: legCount, Kind: legKind, Condition: sc.Condition,
			}
			existing, err := ledger.ListNonTerminal(at.id)
			if err == nil {
				for i := range existing {
					if existing[i].TraderID == at.id && existing[i].PlanID == row.PlanID && existing[i].Scenario == sc.ID && existing[i].LegIndex == row.LegIndex {
						row.ID = existing[i].ID // already in the ledger — leave state (churn guard applies to placement)
						break
					}
				}
			}
			if row.ID == 0 {
				if err := ledger.UpsertArm(row); err != nil {
					at.logWarnf("⚔️ arm write failed %s %s leg %d: %v", plan.Session, sc.ID, li+1, err)
					continue
				}
				// PRE-REOPEN F3 (2026-08-28) — the authored log fires ONCE per
				// spec (dedup by plan:version:scenario + prices); the dead-row
				// re-log spam (69+ lines/day) came from logging every cycle.
				// F4 (2026-09-03) — the dedup value carries the row's STATE.
				// "⚔️ armed NY S1 leg 1 short limit" re-logged 4× on 09-03
				// after row 35 was already filled: ListNonTerminal excludes
				// filled/cancelled rows, so a terminal row is not found, row.ID
				// stays 0, and this branch runs again. State in the value means
				// a re-log is at least visible as a state change — and the
				// guard below means a terminal row does not log "armed" at all.
				akey := plan.PlanID + ":" + strconv.Itoa(plan.Version) + ":" + sc.ID + ":leg" + strconv.Itoa(li+1)
				// The dedup VALUE carries no ATR-derived price. leg.Stop drifts
				// with live ATR, so a price-bearing value changed every cycle and
				// suppressed nothing — the five post-fill lines of 09-03 each
				// carried a different stop (29354.91 · 29352.65 · 29354.44 ·
				// 29352.40 · 29354.86). This is the GAR-F6 lesson the refusal
				// path learned and the authored path never did.
				aval := fmt.Sprintf("%s entry=%.2f", side, leg.Entry)
				if armedActually(row.ID, row.State) && armRefusalChanged(&at.armAuthoredLast, akey, aval) {
					at.logInfof("⚔️ armed %s %s leg %d %s limit %.2f SL %.2f TP %.2f (tick-managed placement is Phase 2)", plan.Session, sc.ID, li+1, side, leg.Entry, leg.Stop, leg.Target)
				}
			} else {
				// CHURN GUARD (2.1): re-spec a working arm's bracket only when the
				// plan moved SL or TP by ≥ 2 ticks (cancel+re-place on modify).
				tick := market.FuturesTickSize(at.futuresSymbol())
				if tick <= 0 {
					tick = 0.25
				}
				if row.State == "working" && churnNeedsModify(row.StopPx, row.TargetPx, leg.Stop, leg.Target, tick) {
					if nt := at.armedTrader(); nt != nil {
						_ = nt.ModifyBracket(row.SignalID, leg.Stop, leg.Target)
						at.logInfof("📌 armed %s leg %d bracket modify (churn guard) SL %.2f→%.2f TP %.2f→%.2f",
							sc.ID, li+1, row.StopPx, leg.Stop, row.TargetPx, leg.Target)
					}
				}
				row.EntryPx, row.StopPx, row.TargetPx = leg.Entry, leg.Stop, leg.Target
				row.Version = plan.Version
				_ = ledger.UpsertArm(row)
			}
		}
	}

	// E8 (2026-08-30) — shadow A/B counterfactual logger (Sep-9's courtroom):
	// per armed scenario, log the 4 rule counterfactuals once per plan version.
	// ZERO effect on real paths — writes ONLY the ab_confirm_log table.
	// 0C (2026-08-31): rows carry the complete would-have-been trade and
	// is_counterfactual=true for shadowed conditions.
	for _, sc := range doc.Scenarios {
		at.logShadowAB(plan, sc, bars, atr5m, now.UnixMilli())
	}

	// E4 (2026-08-30) — split-sibling law: EITHER leg's STOP-OUT cancels the
	// sibling's unfilled order (no doubling into a failed level). Runs on the
	// existing cancel machinery; session-end/news/dormant cancel paths already
	// cover BOTH legs (cancel-all by trader).
	at.cancelSplitSiblingOnStopOut(ledger)

	// PHASE 2 — placement engine (armed → working within the tick band), wire
	// cancel/modify, and the order_update event machine.
	at.runArmedPlacement(bars, plan.BirthMs)
}

// biasDirectionFor normalizes the plan bias direction ("" → empty).
func biasDirectionFor(dir string) string {
	return strings.ToLower(strings.TrimSpace(dir))
}

// oneLiveArmGuard (class 27 FIX 4, owner-added 2026-08-31) — refuse an
// opposite-side arm entry while a position is open. On a netting account a
// second arm is not a second trade: its fill NETs the open position, an
// unlogged exit of the first. A leg explicitly authored as an exit/flip leg
// (kind "exit") is the only escape. "" = pass.
func (at *AutoTrader) oneLiveArmGuard(sc kernel.PlanScenario, leg kernel.PlanArmLeg, side string) string {
	if at.store == nil || side == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(leg.Kind), "exit") {
		return "" // explicitly authored exit/flip leg for the open position
	}
	opens, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil || len(opens) == 0 {
		return ""
	}
	sym := market.Normalize(at.futuresSymbol())
	for _, p := range opens {
		if !strings.EqualFold(p.Symbol, sym) {
			continue
		}
		// ONE OPEN POSITION PER INSTRUMENT (owner ruling 2026-09-03). This
		// used to `continue` on a same-side match — "outside this guard's
		// scope" — which is how a new plan version could re-authorize a
		// terminal row and ADD to a position that was still open. Both sides
		// are refused now, and EntryGate leg 7 says the same thing on both
		// paths; this legacy chain is kept in step deliberately so an arm can
		// never be held to the weaker of two standards.
		ver := fmt.Sprintf("v%d", p.PlanVersion)
		if p.PlanVersion <= 0 {
			ver = "version not recorded"
		}
		return fmt.Sprintf("one_open_position: %s arm %s refused — position %d open (%s %s %s on %s); no adds, no flips (owner ruling 2026-09-03)",
			side, sc.ID, p.ID, ver, p.CitedScenarioID, strings.ToLower(p.Side), sym)
	}
	return ""
}

// armLegCapacity (class 27 FIX 5, owner-added 2026-08-31) — the account's leg
// capacity for arm authoring. Default 1: every live order sizes to 1 contract,
// so a split (2-leg) arm on this account is refused AT WRITE. An explicit
// per-strategy max_contracts_per_order ≥ 2 raises the capacity and re-enables
// split arms.
func (at *AutoTrader) armLegCapacity() int {
	explicit := 0
	if at.config.StrategyConfig != nil {
		explicit = at.config.StrategyConfig.RiskControl.MaxContractsPerOrder
	}
	return splitLegCapacity(explicit)
}

// splitLegCapacity is the pure leg-capacity resolver (test seam): an explicit
// positive max-contracts value IS the capacity; unset → 1 (netting-safe).
func splitLegCapacity(explicitMaxContracts int) int {
	if explicitMaxContracts > 0 {
		return explicitMaxContracts
	}
	return 1
}

// logShadowAB (E8) writes the 4 counterfactual confirm-fill rows for one armed
// scenario — once per (plan, version, scenario, rule). Advisory/report-only:
// nothing here feeds a gate or a prompt. HARDENED (2026-08-30 cutover panic): a
// report-only path must NEVER take the trading loop down — recover + log.
//
// 0C (2026-08-31): every row now carries the COMPLETE would-have-been trade
// (condition, authored stop/target/RR, ATR(5m), MFE/MAE in R + ATR units,
// time-to bars, net-of-friction P&L, the ambiguous flag) and
// is_counterfactual=true for SHADOWED conditions — the demotion's whole
// justification is this data, so shadowed setups must score exactly like
// placed ones.
func (at *AutoTrader) logShadowAB(plan *kernel.ActivePlan, sc kernel.PlanScenario, bars []market.Kline, atr5m float64, nowMs int64) {
	defer func() {
		if r := recover(); r != nil {
			at.logWarnf("⚠️ ab-confirm shadow recovered from panic: %v (report-only path — real paths untouched)", r)
		}
	}()
	if at.store == nil || plan == nil || len(bars) == 0 {
		return
	}
	rows := kernel.ShadowABForScenario(sc, bars, at.futuresSymbol(), plan.BirthMs, nowMs)
	if len(rows) == 0 {
		return
	}
	shadowed := at.conditionShadowedFor(sc.Condition, plan.Session)
	// CLASS 39 — stamp the counterfactual row when this scenario's arm was
	// normalized at plan write (legs dropped), with the dropped legs as JSON, so
	// the effect of normalizing instead of rejecting is measurable later.
	norm := kernel.ArmNormalizationFor(&plan.Doc, sc.ID)
	ac := at.store.AbConfirm()
	now := time.Now()
	for _, r := range rows {
		if ac.Has(plan.PlanID, plan.Version, sc.ID, r.Rule) {
			continue
		}
		mfeAtr, maeAtr := 0.0, 0.0
		if atr5m > 0 {
			mfeAtr = r.MFE / atr5m
			maeAtr = r.MAE / atr5m
		}
		if err := ac.Upsert(&store.AbConfirmLogDB{
			TraderID: at.id, PlanID: plan.PlanID, Version: plan.Version, Session: plan.Session,
			Scenario: sc.ID, Rule: r.Rule, FillPx: r.FillPx, MFE: r.MFE, MAE: r.MAE,
			Outcome: r.Outcome, TimeToFillMs: r.TimeToFillMs,
			Condition: sc.Condition, EntryPx: r.FillPx, StopPx: r.StopPx, TargetPx: r.TargetPx,
			RR: r.RR, Atr5m: atr5m, MfeR: r.MFER, MaeR: r.MAER, MfeAtr: mfeAtr, MaeAtr: maeAtr,
			TimeToMFEBars: r.TimeToMFEBars, TimeToMAEBars: r.TimeToMAEBars,
			TimeToResolveBars: r.TimeToResolveBars, NetPnL: r.NetPnL,
			Ambiguous: r.Ambiguous, IsCounterfactual: shadowed,
			Normalized: norm != nil, DroppedLegs: kernel.DroppedLegsJSON(norm),
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			at.logWarnf("ab-confirm shadow write failed %s %s: %v", sc.ID, r.Rule, err)
		}
	}
}

// splitSiblingCancelDecision (E4, pure) — given one split pair (legs of the
// same plan:scenario, LegCount=2) and the CLOSED positions of that trader,
// decide which sibling rows to cancel: a FILLED leg whose position CLOSED at
// its STOP (exit within 2 ticks of the leg's stop price) voids the level, so
// the sibling's unfilled order must die — no doubling into a failed level.
// A target-out or an open position cancels NOTHING.
func splitSiblingCancelDecision(pair []store.ArmedOrderDB, closed []store.TraderPosition, tick float64) []store.ArmedOrderDB {
	if tick <= 0 {
		tick = 0.25
	}
	var out []store.ArmedOrderDB
	stopHit := false
	for _, leg := range pair {
		if leg.State != "filled" || leg.SignalID == "" {
			continue
		}
		for _, p := range closed {
			if p.EntryOrderID != leg.SignalID || p.TraderID != leg.TraderID {
				continue
			}
			if math.Abs(p.ExitPrice-leg.StopPx) <= 2*tick && math.Abs(p.ExitPrice-leg.TargetPx) > 2*tick {
				stopHit = true // the filled leg stopped out
			}
		}
	}
	if !stopHit {
		return nil
	}
	for _, leg := range pair {
		if leg.State == "armed" || leg.State == "working" {
			out = append(out, leg)
		}
	}
	return out
}

// cancelSplitSiblingOnStopOut (E4) — the wire half of the split-sibling law,
// riding the existing cancel machinery (cancel + ledger state + ack seam).
func (at *AutoTrader) cancelSplitSiblingOnStopOut(ledger *store.ArmedOrderStore) {
	if ledger == nil {
		return
	}
	rows, err := ledger.ListNonTerminal(at.id)
	if err != nil || len(rows) == 0 {
		return
	}
	// Group split pairs.
	pairs := map[string][]store.ArmedOrderDB{}
	for _, r := range rows {
		if r.TraderID != at.id || r.LegCount != 2 {
			continue
		}
		key := r.PlanID + ":" + r.Scenario
		pairs[key] = append(pairs[key], r)
	}
	if len(pairs) == 0 {
		return
	}
	// Collect the filled legs' signal ids.
	var sigs []string
	for _, p := range pairs {
		for _, r := range p {
			if r.State == "filled" && r.SignalID != "" {
				sigs = append(sigs, r.SignalID)
			}
		}
	}
	if len(sigs) == 0 {
		return
	}
	closedPtrs, err := at.store.Position().ListClosedByEntryOrderIDs(at.id, sigs)
	if err != nil || len(closedPtrs) == 0 {
		return
	}
	closed := make([]store.TraderPosition, 0, len(closedPtrs))
	for _, cp := range closedPtrs {
		closed = append(closed, *cp)
	}
	tick := market.FuturesTickSize(at.futuresSymbol())
	for _, p := range pairs {
		for _, sibling := range splitSiblingCancelDecision(p, closed, tick) {
			reason := "sibling stopped out — split contract (E4)"
			nt := at.armedTrader()
			if nt != nil && sibling.SignalID != "" {
				if cerr := nt.CancelOrder(sibling.SignalID); cerr == nil {
					_ = ledger.SetState(sibling.ID, "cancelled", reason)
					at.logWarnf("✕ armed cancel %s %s leg %d: %s", sibling.Session, sibling.Scenario, sibling.LegIndex+1, reason)
					continue
				}
			}
			if sibling.SignalID == "" {
				// Still just an authorization — kill it in the ledger; placement
				// will never fire for it.
				_ = ledger.SetState(sibling.ID, "cancelled", reason)
				at.logWarnf("✕ armed cancel %s %s leg %d: %s", sibling.Session, sibling.Scenario, sibling.LegIndex+1, reason)
			}
		}
	}
}

// armedLines renders the per-cycle ARMED: lines for the executor prompt.
func (at *AutoTrader) armedLines() string {
	if at.store == nil {
		return ""
	}
	rows, err := at.store.ArmedOrders().ListNonTerminal(at.id)
	if err != nil || len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	for _, r := range rows {
		if r.TraderID != at.id {
			continue
		}
		glyph := map[string]string{"armed": "⏳ armed", "working": "📌 working"}[r.State]
		if glyph == "" {
			continue
		}
		kind := "limit"
		if r.Kind == "stop_entry" {
			kind = "stop"
		}
		fmt.Fprintf(&b, "ARMED: %s %s %s %s %.2f SL %.2f TP %.2f (%s)\n", r.Scenario, r.Side, r.State, kind, r.EntryPx, r.StopPx, r.TargetPx, glyph)
	}
	if b.Len() == 0 {
		return ""
	}
	return b.String()
}

// runArmedPlacement drives the armed→working transition, the churn guard, and
// the order_update event machine. No-op unless a TCPTrader is bound.
// sinceMs = the plan's birth (the E7 stop-entry fallback window is measured
// from it).
func (at *AutoTrader) runArmedPlacement(bars []market.Kline, sinceMs int64) {
	nt := at.armedTrader()
	if nt == nil {
		return
	}
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		return
	}
	now := time.Now()
	price := 0.0
	if len(bars) > 0 {
		price = bars[len(bars)-1].Close
	}
	tick := market.FuturesTickSize(at.futuresSymbol())
	if tick <= 0 {
		tick = 0.25
	}
	band := float64(armedPlaceTicks()) * tick

	rows, err := ledger.ListNonTerminal(at.id)
	if err != nil {
		return
	}
	for _, r := range rows {
		if r.TraderID != at.id {
			continue
		}
		switch r.State {
		case "armed":
			// E7 — stop-entry legs (breakout-retest fallback / breakdown
			// immediate alternative): never placed until the far-side AddOn
			// proves the frame (STOP_ENTRY_SEAM) AND the no-retest window has
			// elapsed (RETEST_WAIT_BARS). The stop trigger sits
			// STOP_ENTRY_OFFSET_TICKS beyond the level.
			if r.Kind == "stop_entry" {
				if !stopEntrySeamOn() {
					continue // seam off → the leg stays armed (never on the wire)
				}
				// D3 (2026-09-04): the window belongs to the E7 FALLBACK only.
				// A reclaim's buy stop IS the entry — waiting for a no-retest
				// window would miss the reclaim it exists to catch.
				if stopEntryNeedsRetestWindow(r.Condition) &&
					!stopEntryFallbackDue(bars, int64(r.EntryPx), sinceMs, now.UnixMilli()) {
					continue // still inside the retest window
				}
				offset := float64(stopEntryOffsetTicks()) * tick
				trigger := r.EntryPx - offset
				if r.Side == "long" {
					trigger = r.EntryPx + offset
				}
				// WRONG-SIDE GUARD (2026-08-30 E7 incident class): a stop-market
				// whose trigger the market has already traded through fires the
				// instant it reaches NT8. Never place it — cancel the arm.
				if price > 0 && limitMarketableWrongSide(price, trigger, r.Side) {
					_ = ledger.SetState(r.ID, "cancelled", "trigger already traded through — never placed")
					at.logWarnf("✕ armed %s stop-entry cancelled — price %.2f already %s the trigger %.2f (never placed)", r.Scenario, price, throughWord(r.Side), trigger)
					continue
				}
				sid, perr := nt.PlaceStopEntry(at.futuresSymbol(), r.Side, 1, trigger, r.StopPx, r.TargetPx)
				if perr != nil {
					at.logWarnf("📌 stop-entry place failed %s: %v", r.Scenario, perr)
					continue
				}
				_ = ledger.SetSignal(r.ID, sid)
				_ = ledger.SetState(r.ID, "working", "")
				at.logInfof("📌 armed %s → WORKING stop-entry %.2f signal=%s (no retest in %d bars, offset %dt)", r.Scenario, trigger, sid, retestWaitBars(), stopEntryOffsetTicks())
				continue
			}
			if price > 0 && limitMarketableWrongSide(price, r.EntryPx, r.Side) {
				// WRONG-SIDE GUARD (2026-08-30 E7 incident class): price has
				// accepted through the level — a limit would fill INSTANTLY at
				// a worse price (the S2 re-place loop: fill → stop-out →
				// re-arm → fill…). Cancel the arm; manual-cancel-wins.
				_ = ledger.SetState(r.ID, "cancelled", "level accepted through — marketable, never placed")
				at.logWarnf("✕ armed %s cancelled — price %.2f already %s entry %.2f (marketable, never placed)", r.Scenario, price, throughWord(r.Side), r.EntryPx)
				continue
			}
			if price > 0 && math.Abs(price-r.EntryPx) <= band {
				sid, perr := nt.PlaceLimitEntry(at.futuresSymbol(), r.Side, 1, r.EntryPx, r.StopPx, r.TargetPx)
				if perr != nil {
					at.logWarnf("📌 armed place failed %s: %v", r.Scenario, perr)
					continue
				}
				_ = ledger.SetSignal(r.ID, sid)
				_ = ledger.SetState(r.ID, "working", "")
				at.logInfof("📌 armed %s → WORKING limit %.2f signal=%s (band ±%.0ft)", r.Scenario, r.EntryPx, sid, band/tick)
			}
		}
	}
	// reconnect/reconcile safety net (separate pass — cancelFn is the wire seam).
	at.reconcileStaleWorking(ledger, rows, now, armedWorkingStaleMin(), func(sid string) { _ = nt.CancelOrder(sid) })
	at.consumeArmedOrderUpdates(nt, ledger)
}

// limitMarketableWrongSide (E7 incident guard, pure) reports whether price has
// already traded THROUGH a resting limit in the trade's direction — i.e. a
// long's entry sits above the market (buy limit marketable) or a short's entry
// sits below it. Placing in that state fills instantly at a worse price.
func limitMarketableWrongSide(price, entry float64, side string) bool {
	if price <= 0 || entry <= 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "long":
		return price < entry
	case "short":
		return price > entry
	}
	return false
}

// throughWord is the human word for "the market has traded through" per side.
func throughWord(side string) string {
	if strings.EqualFold(side, "short") {
		return "above"
	}
	return "below"
}

// churnNeedsModify — the churn guard predicate: the plan re-spec'd a working
// arm's SL or TP by ≥ 2 ticks (2.1). Pure for tests.
func churnNeedsModify(oldStop, oldTarget, newStop, newTarget, tick float64) bool {
	return math.Abs(oldStop-newStop) >= 2*tick || math.Abs(oldTarget-newTarget) >= 2*tick
}

// armRefusalClass (GAR-F6) — the refusal's verdict CLASS, stripped of live
// ATR numbers. The dedup key uses the class so an ATR drift (e.g. min-SL
// "18.29×ATR5m" → "18.67×ATR5m") does not re-log the same refusal.
func armRefusalClass(verdict string) string {
	v := strings.ToLower(verdict)
	switch {
	// INVALIDATION (2026-09-03) — first, because it is the only class that says
	// the SETUP is gone rather than that the trade is badly shaped. It must not
	// fall into "other" and disappear from the tally.
	case strings.Contains(v, "invalidated at"):
		return "invalidated"
	case strings.HasPrefix(v, "r:r"):
		return "rr"
	case strings.Contains(v, "too close"):
		return "min_sl"
	case strings.Contains(v, "veto"):
		return "veto"
	case strings.Contains(v, "not armable") || strings.Contains(v, "non-armable"):
		return "not_armable"
	default:
		return "other"
	}
}

// armRefusalChanged (F4) — true when this arm-spec's refusal verdict is new or
// changed (the caller logs); false when the identical verdict was already
// logged for the same spec (the caller stays silent).
func armRefusalChanged(last *map[string]string, key, verdict string) bool {
	if *last == nil {
		*last = map[string]string{}
	}
	if prev, ok := (*last)[key]; ok && prev == verdict {
		return false
	}
	(*last)[key] = verdict
	return true
}

// workingStale — the reconnect predicate: no order_update for the stale window.
func workingStale(updatedAt, now time.Time, staleMin int) bool {
	return now.Sub(updatedAt) > time.Duration(staleMin)*time.Minute
}

// reconcileStaleWorking cancels working rows that have seen no order_update for
// the stale window (reconnect safety net). cancelFn issues the NT8 cancel — the
// ledger flips to cancelled with the reason regardless.
func (at *AutoTrader) reconcileStaleWorking(ledger *store.ArmedOrderStore, rows []store.ArmedOrderDB, now time.Time, staleMin int, cancelFn func(signalID string)) {
	for _, r := range rows {
		if r.TraderID != at.id || r.State != "working" {
			continue
		}
		if !workingStale(r.UpdatedAt, now, staleMin) {
			continue
		}
		if r.SignalID != "" && cancelFn != nil {
			cancelFn(r.SignalID)
		}
		_ = ledger.SetState(r.ID, "cancelled", "no order_update within stale window (reconnect/reconcile)")
		at.logWarnf("✕ armed %s cancelled — no order_update for %dm (reconnect/reconcile)", r.Scenario, staleMin)
	}
}

// consumeArmedOrderUpdates subscribes ONCE to the trader's order_update
// stream and drains pending events into the ledger.
//
// 2026-08-27 bug: the old code called nt.OrderUpdates() as the LoadOrStore
// argument on EVERY cycle — the argument is evaluated first, and
// SubscribeOrderUpdatesFor CLOSES+replaces the channel on each subscribe.
// The map then held a closed channel forever: the drain read 310,808
// zero-value payloads in 15s at 13:34:48 and the consumer was permanently
// dead. Now: subscribe only on the miss path, and self-heal (delete the map
// entry) if the channel is ever closed.
func (at *AutoTrader) consumeArmedOrderUpdates(nt *ntTrader.TCPTrader, ledger *store.ArmedOrderStore) {
	// S-list closer: the subscription is now created via armedUpdateStream —
	// subscribe exactly once on the miss path, never re-subscribing per cycle.
	ch := at.armedUpdateStream(nt)
	if ch == nil {
		return
	}
	for {
		select {
		case u, open := <-ch:
			if !open {
				armedSubs.Delete(at.id)
				at.logWarnf("📡 armed order_update channel closed — re-subscribing next cycle")
				return
			}
			at.onArmedOrderUpdate(u, ledger)
		default:
			return
		}
	}
}

// Armed order_update frame receipt — FORENSICS HYGIENE (2026-08-28): the
// per-frame INFO log at this site ate 1.48GB of journal in one hour
// (25k lines/s during an NT8 order_update storm at 13:35-14:35 CT, the
// 3.6h-retention S-finding). The frame is now logged at DEBUG sampled 1-in-N
// (T8 pattern) and the receive path stays provable via a 1-line/min INFO
// summary with per-state counts.
var (
	armedOUFrames  atomic.Int64 // frames since the last summary
	armedOUSample  atomic.Int64 // per-frame sample counter
	armedOUByState sync.Map     // state string -> *atomic.Int64
	armedOULastSum atomic.Int64 // unix secs of the last summary
)

func armedOrderUpdateLogSample() int64 {
	if v := os.Getenv("ARMED_ORDER_UPDATE_LOG_SAMPLE"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 500
}

func logArmedOrderUpdateSummary() {
	now := time.Now().Unix()
	if now-armedOULastSum.Load() < 60 {
		return
	}
	if !armedOULastSum.CompareAndSwap(armedOULastSum.Load(), now) {
		return
	}
	n := armedOUFrames.Swap(0)
	var b strings.Builder
	fmt.Fprintf(&b, "📡 armed order_update summary (1-line/min): frames=%d", n)
	armedOUByState.Range(func(k, v any) bool {
		if c := v.(*atomic.Int64).Swap(0); c > 0 {
			fmt.Fprintf(&b, " %s=%d", k, c)
		}
		return true
	})
	logger.Infof("%s", b.String())
}

// onArmedOrderUpdate applies one NT8 order state change to the armed ledger.
func (at *AutoTrader) onArmedOrderUpdate(u ntwire.OrderUpdatePayload, ledger *store.ArmedOrderStore) {
	// Frame-receipt proof (cutover confirmation wave): the C# dispatcher's
	// receive path stays provable from the journal via the 1-line/min summary;
	// the per-frame content is DEBUG + 1-in-N sampled (FORENSICS HYGIENE —
	// this was the 1.48GB/hour journal flood).
	armedOUFrames.Add(1)
	if c, _ := armedOUByState.LoadOrStore(u.State, &atomic.Int64{}); c.(*atomic.Int64).Add(1) == 0 {
		_ = c // kept for clarity: the value-add already happened
	}
	if armedOUSample.Add(1)%armedOrderUpdateLogSample() == 0 {
		logger.Debugf("📡 armed order_update frame: state=%s signal=%s acct=%s fill=%.2f",
			u.State, u.SignalID, u.Account, u.FillPrice)
	}
	logArmedOrderUpdateSummary()
	rows, err := ledger.ListNonTerminal(at.id)
	if err != nil {
		return
	}
	for _, r := range rows {
		if r.TraderID != at.id || r.SignalID != u.SignalID {
			continue
		}
		switch strings.ToLower(u.State) {
		case "filled", "partfilled":
			_ = ledger.SetState(r.ID, "filled", "fill@"+strconv.FormatFloat(u.FillPrice, 'f', 2, 64))
			_ = ledger.SetFillPrice(r.ID, u.FillPrice)
			_ = ledger.Touch(r.ID)
			// F3 (2026-08-30 E7 incident) — materialize the OPEN row at FILL
			// time, before the stamp: the sub-60s round-trip class (fill →
			// stop-out inside one snapshot interval) meant reconcile never saw
			// the position open, so the priced close parked forever and NT8's
			// equity diverged from the ledger.
			at.materializeArmedEntry(r, u)
			at.stampArmedFillLineage(r, u.FillPrice)
			at.logInfof("⚡ armed fill %s @ %.2f (entry_class=armed_fill — stale_reeval NOT applied)", r.Scenario, u.FillPrice)
		case "rejected":
			_ = ledger.SetState(r.ID, "cancelled", "NT8 reject")
			at.logWarnf("✕ armed %s NT8-REJECTED — disarmed", r.Scenario)
		case "cancelled":
			_ = ledger.SetState(r.ID, "cancelled", "cancelled in NT8")
			at.logInfof("✕ armed %s cancelled in NT8", r.Scenario)
		}
		return
	}
}

// materializeArmedEntry (F3, 2026-08-30 E7 incident) creates the OPEN position
// row from the armed fill at FILL time when no row exists yet. The ledger row
// carries the fill truth (signal, fill price, plan attribution), so the sub-60s
// round-trip becomes ledger-visible and the priced close the far side sends
// finds its open row on the normal sync path.
func (at *AutoTrader) materializeArmedEntry(r store.ArmedOrderDB, u ntwire.OrderUpdatePayload) {
	if at.store == nil || u.FillPrice <= 0 || r.SignalID == "" {
		return
	}
	// CLASS-27 FIX 3 (2026-08-31): rows are written with UPPERCASE side (the
	// canonical form every lookup uses); the dedupe checks BOTH cases so a
	// legacy lowercase row can never let a duplicate materialize (the 577+578
	// class — the old dedupe queried the lowercase ledger side against the
	// uppercase store convention and missed).
	side := strings.ToUpper(strings.TrimSpace(r.Side))
	if side == "" {
		return
	}
	if pos, err := at.store.Position().GetOpenPositionBySymbol(at.id, at.futuresSymbol(), side); err == nil && pos != nil {
		return // already materialized (reconcile won the race)
	}
	if pos, err := at.store.Position().GetOpenPositionBySymbol(at.id, at.futuresSymbol(), strings.ToLower(side)); err == nil && pos != nil {
		return // legacy lowercase row already exists for the same fill
	}
	tradeDate := r.PlanID
	if i := strings.Index(r.PlanID, ":"); i > 0 {
		tradeDate = r.PlanID[:i]
	}
	nowMs := time.Now().UTC().UnixMilli()
	row := &store.TraderPosition{
		TraderID:           at.id,
		ExchangeType:       "ninjatrader",
		ExchangePositionID: fmt.Sprintf("armed_%s_%d", r.SignalID, nowMs),
		Symbol:             at.futuresSymbol(),
		Side:               side,
		Quantity:           1,
		EntryQuantity:      1,
		EntryPrice:         u.FillPrice,
		EntryTime:          nowMs,
		EntryOrderID:       r.SignalID,
		Leverage:           1,
		Status:             "OPEN",
		Source:             "armed_entry",
		Account:            u.Account,
		PlanID:             r.PlanID,
		PlanVersion:        r.Version,
		PlanTradeDate:      tradeDate,
		PlanSession:        r.Session,
		CitedScenarioID:    r.Scenario,
		CreatedAt:          nowMs,
		UpdatedAt:          nowMs,
	}
	if err := at.store.Position().CreateOpenPosition(row); err != nil {
		at.logWarnf("🧩 armed fill %s materialize OPEN failed: %v", r.Scenario, err)
		return
	}
	at.logInfof("🧩 armed fill %s @ %.2f materialized OPEN (source=armed_entry — sub-60s round-trips are ledger-visible)", r.Scenario, u.FillPrice)
	// E1 (wave 1A) — the excursion row's entry half. An armed fill carries its
	// own levels in the ledger row, so nothing has to be resolved later.
	at.excursionOnOpen(row, r.StopPx, r.TargetPx, plannerATR5m(at.futuresSymbol()))
}

// stampArmedFillLineage links the freshly-filled position row to the plan the
// arm cited — the same fields AI entries carry (S3 SetPlanLinkFull).
func (at *AutoTrader) stampArmedFillLineage(r store.ArmedOrderDB, fillPrice float64) {
	pos, err := at.store.Position().GetOpenPositionBySymbol(at.id, at.futuresSymbol(), r.Side)
	if err != nil || pos == nil {
		// PRE-REOPEN F4 (2026-08-28) — the fill frame precedes position
		// materialization (all 4 live fills hit this race). The LEDGER row
		// carries the pending marker; the reconcile materialization path
		// (StampArmedLineageIfMatched) completes the stamp and clears it.
		if e2 := at.store.ArmedOrders().SetState(r.ID, "filled", fmt.Sprintf("%s;stamp_pending", r.StateReason)); e2 != nil {
			at.logWarnf("⚡ armed fill %s: pending-marker write failed: %v", r.Scenario, e2)
			return
		}
		at.logInfof("⚡ armed fill %s @ %.2f: position row not materialized yet — stamp pending (reconcile completes it)", r.Scenario, fillPrice)
		return
	}
	tradeDate := r.PlanID
	if i := strings.Index(r.PlanID, ":"); i > 0 {
		tradeDate = r.PlanID[:i]
	}
	if err := at.store.Position().SetPlanLinkFull(pos.ID, r.Version, r.Scenario, true, "armed_fill", r.PlanID, tradeDate, r.Session); err != nil {
		at.logWarnf("⚡ armed fill lineage stamp failed: %v", err)
	}
	// F3 (2026-09-03) — the contracts the fill delivered, on the same path that
	// stamps lineage. Row 35 read filled with fill_quantity=0 beside a position
	// of quantity 1.
	if err := at.store.ArmedOrders().SetFillQuantity(r.ID, int(pos.Quantity)); err != nil {
		at.logWarnf("⚡ armed fill quantity stamp failed: %v", err)
	}
	// F2 (2026-09-03) — the fill line names the version the arm BELONGS to,
	// not whatever version is live by the time it fills.
	at.logInfof("⚡ armed fill %s: armed under v%d %s %s (%s) · qty %.0f",
		r.Scenario, armedUnderVersionOf(r), r.Scenario, r.Side, r.EntryClass, pos.Quantity)
}

// armedUnderVersionOf reads the version an arm was FIRST authorized under,
// falling back to the mutable Version only when the attribution column was
// never stamped (rows created before 2026-09-03 10:28 carry 0 — armed rows 35
// and 36 are both such rows).
func armedUnderVersionOf(r store.ArmedOrderDB) int {
	if r.ArmedUnderVersion > 0 {
		return r.ArmedUnderVersion
	}
	return r.Version
}

// armGateVerdict runs the arm-time gate chain for a SINGLE arm (legacy shape).
// Empty string = pass.
func (at *AutoTrader) armGateVerdict(sc kernel.PlanScenario, biasDirection string, snap map[string]kernel.StructureState, atr5m float64, minQuality string, cfg *store.StrategyConfig, session string) string {
	if sc.Arm == nil {
		return "no arm"
	}
	return at.armGateVerdictFor(sc, kernel.PlanArmLeg{Entry: sc.Arm.Entry, Stop: sc.Arm.Stop, Target: sc.Arm.Target}, biasDirection, snap, atr5m, minQuality, cfg, session)
}

// armGateVerdictFor runs the arm-time gate chain for ONE LEG's prices (E4: the
// split legs gate independently — each leg is a pre-passed entry of its own).
// The min-confidence gate is N/A for arms — the AI's authorization IS the
// confidence signal (no per-scenario confidence exists to check).
func (at *AutoTrader) armGateVerdictFor(sc kernel.PlanScenario, leg kernel.PlanArmLeg, biasDirection string, snap map[string]kernel.StructureState, atr5m float64, minQuality string, cfg *store.StrategyConfig, session string) string {
	a := sc.Arm
	if err := kernel.ArmSpecValid(sc); err != nil {
		return err.Error()
	}
	side := strings.ToLower(strings.TrimSpace(sc.Direction))
	if side != "long" && side != "short" {
		return fmt.Sprintf("direction %q not armable", sc.Direction)
	}
	// plan_mode direction — the plan is the law, same as the entry path.
	//
	// R2 (owner ruling 2026-09-03): this passed "" and so ALWAYS resolved the
	// strategy-level mode, silently dropping a per-session override. A session
	// set to direction (or strict) was honoured on the decision path and
	// ignored at the arm seam — the same plan, two different laws.
	if at.planModeFor(session) == "direction" {
		bias := strings.ToLower(strings.TrimSpace(biasDirection))
		if bias != "" && bias != side {
			return fmt.Sprintf("against plan bias %q (plan_mode=direction)", bias)
		}
	}
	// quality floor (min_scenario_quality).
	if minQuality != "" {
		if kernel.ScenarioQualityRank(sc.Quality) < kernel.ScenarioQualityRank(minQuality) {
			return fmt.Sprintf("quality %s below min_scenario_quality %s", sc.Quality, minQuality)
		}
	}
	// R:R gate — ARM_MIN_RR (default 2.0), the gate-at-arm floor. Autopsy
	// response (2026-08-27): resting limits fill AT the level (better entry by
	// construction) — the global 3.0 floor for AI market entries is untouched.
	rr := 0.0
	if side == "long" && leg.Entry > leg.Stop && leg.Stop > 0 {
		rr = (leg.Target - leg.Entry) / (leg.Entry - leg.Stop)
	} else if side == "short" && leg.Stop > leg.Entry && leg.Entry > 0 {
		rr = (leg.Entry - leg.Target) / (leg.Stop - leg.Entry)
	}
	if rr+1e-9 < at.armMinRRFor(cfg) {
		return fmt.Sprintf("R:R %.2f below arm min %.2f (studio min_risk_reward_ratio)", rr, at.armMinRRFor(cfg))
	}
	// min-SL — the same floor (×ATR5m) the entry path enforces.
	if atr5m > 0 {
		dist := leg.Entry - leg.Stop
		if side == "short" {
			dist = leg.Stop - leg.Entry
		}
		if dist+1e-9 < kernel.MinSLATRMult()*atr5m {
			return fmt.Sprintf("stop %.2f too close (%.2f < %.2f = %.1f×ATR5m)", leg.Stop, dist, kernel.MinSLATRMult()*atr5m, kernel.MinSLATRMult())
		}
	}
	// HTF veto — the same veto the entry path enforces, AND the same switch.
	//
	// R3 (owner ruling 2026-09-03): this ran unconditionally. regime.htf_veto
	// = false turned the veto off for the decision path and left the arm chain
	// vetoing forever, so an owner who switched it off still could not arm.
	// One switch, both consumers.
	if cfg != nil && !cfg.HTFVetoEnabled() {
		// off by owner ruling — fall through to the remaining gates
	} else if blocked, vetoReason := kernel.HTFVetoVerdict(snap, "open_"+side, kernel.HTFVetoTF()); blocked {
		return "HTF veto: " + vetoReason
	}
	_ = a
	return ""
}

// cancelArmedOrders moves non-terminal rows for THIS trader to cancelled with a
// reason. Returns the count.
func (at *AutoTrader) cancelArmedOrders(reason string) int {
	rows, err := at.store.ArmedOrders().ListNonTerminal(at.id)
	if err != nil {
		return 0
	}
	n := 0
	for _, r := range rows {
		if r.TraderID != at.id {
			continue
		}
		if err := at.store.ArmedOrders().SetState(r.ID, "cancelled", reason); err == nil {
			n++
			at.logInfof("✕ armed cancel %s %s: %s", r.Scenario, r.PlanID, reason)
		}
	}
	return n
}

// ── S-LIST CLOSER (2026-08-27) — synchronous armed cancel ────────────────────
// The EOD race (deep-verify hole 11): enforceEODFlatAt flattened POSITIONS
// only, and the armed cancel ran on the NEXT cycle — a working limit could
// fill up to one 2m cycle AFTER the flat. Every lifecycle path that flattens
// or disarms around a session boundary (EOD flat, session end, dormancy, T1
// force-flat) now cancels working arms FIRST, SYNCHRONOUSLY: the NT8 cancel
// frame is sent, then the shared order_update stream is drained until the ack
// lands or the deadline passes (one retry). Acked or not the ledger flips to
// cancelled and the flatten proceeds — the cancel-before-flatten WIRE ORDER is
// what kills the window, and the flatten is never held hostage by a stuck ack.

// armedCancelAckTimeout is the per-order ack wait (ARMED_CANCEL_ACK_TIMEOUT_MS,
// default 2000). One retry ⇒ a stuck ack costs ≤ 2× this before the flatten
// proceeds.
func armedCancelAckTimeout() time.Duration {
	if v := os.Getenv("ARMED_CANCEL_ACK_TIMEOUT_MS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return 2000 * time.Millisecond
}

// armedSyncSeam is the fixture seam for the synchronous cancel (nil = prod TCP).
type armedSyncSeam struct {
	Cancel  func(signalID string) error
	Stream  func() <-chan ntwire.OrderUpdatePayload
	Timeout time.Duration // 0 = armedCancelAckTimeout()
}

// armedUpdateStream returns THIS trader's shared order_update subscription,
// creating it exactly once on the miss path. NEVER LoadOrStore with
// nt.OrderUpdates() as the eager argument — the argument evaluates FIRST and
// SubscribeOrderUpdatesFor closes+replaces the consumer's channel on every
// cycle (the 2026-08-27 consumer-death bug).
func (at *AutoTrader) armedUpdateStream(nt *ntTrader.TCPTrader) <-chan ntwire.OrderUpdatePayload {
	if v, ok := armedSubs.Load(at.id); ok {
		ch, _ := v.(<-chan ntwire.OrderUpdatePayload)
		return ch
	}
	ch := nt.OrderUpdates()
	v, _ := armedSubs.LoadOrStore(at.id, ch)
	stored, _ := v.(<-chan ntwire.OrderUpdatePayload)
	return stored
}

// cancelArmedOrdersSync cancels every non-terminal armed row for THIS trader
// with ack-waited wire cancels (one retry per order). Returns the rows
// cancelled and the rows whose ack never arrived (ledger flipped anyway).
func (at *AutoTrader) cancelArmedOrdersSync(reason string) (n, unacked int) {
	if at.store == nil {
		return 0, 0
	}
	if s := at.armedSyncSeam; s != nil {
		timeout := s.Timeout
		if timeout <= 0 {
			timeout = armedCancelAckTimeout()
		}
		return at.cancelArmedOrdersSyncWith(reason, timeout, s.Cancel, s.Stream)
	}
	nt := at.armedTrader()
	if nt == nil {
		return at.cancelArmedOrders(reason), 0
	}
	return at.cancelArmedOrdersSyncWith(reason, armedCancelAckTimeout(), nt.CancelOrder,
		func() <-chan ntwire.OrderUpdatePayload { return at.armedUpdateStream(nt) })
}

// cancelArmedOrdersSyncWith is the pure body: per-row cancel + ack drain. Every
// frame drained is applied through the SAME onArmedOrderUpdate the cycle
// consumer uses, so no ledger state is lost and no second subscription is ever
// made (a second subscribe would close the consumer's channel).
func (at *AutoTrader) cancelArmedOrdersSyncWith(reason string, timeout time.Duration, cancelFn func(string) error, src func() <-chan ntwire.OrderUpdatePayload) (n, unacked int) {
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		return 0, 0
	}
	rows, err := ledger.ListNonTerminal(at.id)
	if err != nil {
		return 0, 0
	}
	var mine []store.ArmedOrderDB
	for _, r := range rows {
		if r.TraderID == at.id {
			mine = append(mine, r)
		}
	}
	for _, r := range mine {
		if r.State != "working" || r.SignalID == "" || cancelFn == nil || src == nil {
			_ = ledger.SetState(r.ID, "cancelled", reason)
			n++
			continue
		}
		acked := false
		for attempt := 1; attempt <= 2 && !acked; attempt++ {
			if err := cancelFn(r.SignalID); err != nil {
				at.logWarnf("⚠️ armed sync cancel send %s signal=%s failed: %v", r.Scenario, r.SignalID, err)
			}
			ch := src()
			if ch == nil {
				break
			}
			deadline := time.Now().Add(timeout)
			for !acked && ch != nil {
				remaining := time.Until(deadline)
				if remaining <= 0 {
					break
				}
				timer := time.NewTimer(remaining)
				select {
				case u, open := <-ch:
					timer.Stop()
					if !open {
						ch = nil // stream closed — the consumer self-heals next cycle
						break
					}
					at.onArmedOrderUpdate(u, ledger)
					acked = !at.armedRowStillActive(ledger, r.ID)
				case <-timer.C:
				}
			}
		}
		if acked {
			n++
		} else {
			_ = ledger.SetState(r.ID, "cancelled", reason+" (ack timeout — flatten proceeds)")
			unacked++
			at.logWarnf("⚠️ armed sync cancel UNACKED %s signal=%s after retry — ledger cancelled, flatten proceeds", r.Scenario, r.SignalID)
		}
	}
	return n, unacked
}

// armedRowStillActive reports whether the ledger row is still non-terminal.
func (at *AutoTrader) armedRowStillActive(ledger *store.ArmedOrderStore, id int64) bool {
	rows, err := ledger.ListNonTerminal(at.id)
	if err != nil {
		return true // unknown → keep waiting until the deadline
	}
	for _, r := range rows {
		if r.ID == id {
			return true
		}
	}
	return false
}

// ── E2 DEBUG SEAM (2026-08-27, level-truth wave ruling "a") ─────────────────
// POST /api/armed/test-arm drives the REAL placement path with a TEST-E2 row so
// the armed-orders cutover can be proven end-to-end (place → 📌 working in the
// NT8 Orders tab → cancel → ✕ chain) even when the planner can't produce an
// active plan. Gated by env ARMED_TEST_SEAM=on (default OFF) AND the bound
// account being SIM — a debug endpoint that places orders must not exist
// unarmed.

// armedTestSeamOn reads the env gate.
func armedTestSeamOn() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("ARMED_TEST_SEAM")))
	return v == "on" || v == "1" || v == "true"
}

// armedSeamStateLabel is the boot-line spelling of the seam state.
func armedSeamStateLabel() string {
	if armedTestSeamOn() {
		return "ON"
	}
	return "off"
}

// armedSeamDenied returns the blocker when the seam is gated off ("" = allowed).
func (at *AutoTrader) armedSeamDenied() string {
	if !armedTestSeamOn() {
		return "ARMED_TEST_SEAM is off"
	}
	if !strings.EqualFold(at.currentAccountName(), "Sim101") {
		return "seam is SIM-only (bound account " + at.currentAccountName() + ")"
	}
	return ""
}

// TestArmPlace places a resting limit on the REAL wire path (TCPTrader
// PlaceLimitEntry — the same call runArmedPlacement makes) with a ledger row
// tagged TEST-E2, skipping the price band (the tester pins the price).
func (at *AutoTrader) TestArmPlace(side string, entry, stop, target float64) (store.ArmedOrderDB, error) {
	var out store.ArmedOrderDB
	if reason := at.armedSeamDenied(); reason != "" {
		return out, fmt.Errorf("test-arm denied: %s", reason)
	}
	nt := at.armedTrader()
	if nt == nil {
		return out, fmt.Errorf("no TCPTrader bound")
	}
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		return out, fmt.Errorf("no armed ledger")
	}
	side = strings.ToLower(strings.TrimSpace(side))
	if side != "long" && side != "short" {
		return out, fmt.Errorf("side must be long|short")
	}
	if entry <= 0 || stop <= 0 || target <= 0 {
		return out, fmt.Errorf("entry/stop/target must be > 0")
	}
	sid, perr := nt.PlaceLimitEntry(at.futuresSymbol(), side, 1, entry, stop, target)
	if perr != nil {
		return out, perr
	}
	row := &store.ArmedOrderDB{
		TraderID: at.id,
		PlanID:   "TEST-E2:" + sid,
		Session:  "TEST-E2",
		Scenario: "TEST-E2",
		Side:     side,
		EntryPx:  entry,
		StopPx:   stop,
		TargetPx: target,
	}
	if err := ledger.UpsertArm(row); err != nil {
		return out, fmt.Errorf("ledger upsert: %w", err)
	}
	_ = ledger.SetSignal(row.ID, sid)
	_ = ledger.SetState(row.ID, "working", "")
	row.SignalID = sid
	row.State = "working"
	at.logInfof("🧪 TEST-E2 arm → WORKING limit %.2f signal=%s (seam)", entry, sid)
	return *row, nil
}

// TestArmPlaceStop (E7, entry-mechanics far-side proof) places a STOP-MARKET
// entry on the REAL wire path (TCPTrader PlaceStopEntry — the same call the
// breakdown stop-entry seam makes) with a TEST-E7 ledger row. The tester pins
// the trigger FAR from the market so the order rests (never fills) until the
// cancel proof. Same gates as TestArmPlace: env ARMED_TEST_SEAM=on AND SIM.
func (at *AutoTrader) TestArmPlaceStop(side string, trigger, stop, target float64) (store.ArmedOrderDB, error) {
	var out store.ArmedOrderDB
	if reason := at.armedSeamDenied(); reason != "" {
		return out, fmt.Errorf("test-arm denied: %s", reason)
	}
	nt := at.armedTrader()
	if nt == nil {
		return out, fmt.Errorf("no TCPTrader bound")
	}
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		return out, fmt.Errorf("no armed ledger")
	}
	side = strings.ToLower(strings.TrimSpace(side))
	if side != "long" && side != "short" {
		return out, fmt.Errorf("side must be long|short")
	}
	if trigger <= 0 || stop <= 0 || target <= 0 {
		return out, fmt.Errorf("entry(trigger)/stop/target must be > 0")
	}
	sid, perr := nt.PlaceStopEntry(at.futuresSymbol(), side, 1, trigger, stop, target)
	if perr != nil {
		return out, perr
	}
	row := &store.ArmedOrderDB{
		TraderID: at.id,
		PlanID:   "TEST-E7:" + sid,
		Session:  "TEST-E7",
		Scenario: "TEST-E7",
		Side:     side,
		EntryPx:  trigger,
		StopPx:   stop,
		TargetPx: target,
	}
	if err := ledger.UpsertArm(row); err != nil {
		return out, fmt.Errorf("ledger upsert: %w", err)
	}
	_ = ledger.SetSignal(row.ID, sid)
	_ = ledger.SetState(row.ID, "working", "")
	row.SignalID = sid
	row.State = "working"
	at.logInfof("🧪 TEST-E7 stop-entry → WORKING stop_entry trigger %.2f signal=%s (seam)", trigger, sid)
	return *row, nil
}

// TestArmCancel cancels a seam row's NT8 order on the real wire and flips the
// row to cancelled with an honest reason.
func (at *AutoTrader) TestArmCancel(signalID string) error {
	if reason := at.armedSeamDenied(); reason != "" {
		return fmt.Errorf("test-arm denied: %s", reason)
	}
	nt := at.armedTrader()
	if nt == nil {
		return fmt.Errorf("no TCPTrader bound")
	}
	ledger := at.store.ArmedOrders()
	if ledger == nil {
		return fmt.Errorf("no armed ledger")
	}
	signalID = strings.TrimSpace(signalID)
	if signalID == "" {
		return fmt.Errorf("signal_id required")
	}
	if err := nt.CancelOrder(signalID); err != nil {
		return fmt.Errorf("cancel on wire: %w", err)
	}
	rows, _ := ledger.ListNonTerminal(at.id)
	for _, r := range rows {
		if r.TraderID == at.id && r.SignalID == signalID {
			_ = ledger.SetState(r.ID, "cancelled", "test seam cancel")
			at.logInfof("🧪 TEST-E2 cancel sent signal=%s (row %d → cancelled)", signalID, r.ID)
		}
	}
	return nil
}

// armedActually reports whether an UpsertArm call really armed something, and
// so whether "⚔️ armed …" may be logged (F4, corrected 2026-09-03).
//
// The first cut of this guard read row.State — which is the DESIRED state the
// caller built ("armed"), never the persisted one — so it passed every time and
// changed nothing. The load-bearing signal is the ID: UpsertArm sets it on a
// create and on a live-row update, and LEAVES IT ZERO when the
// MANUAL-CANCEL-WINS rule declines a same-version terminal row
// (store/armed_orders.go:206). That decline is exactly the post-fill case: the
// store correctly refused to re-arm, returned nil, and the caller announced an
// arm that never happened — five times after the 09:03:53 fill.
func armedActually(id int64, state string) bool {
	if id == 0 {
		return false // the upsert declined; nothing was armed
	}
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "filled", "cancelled", "expired":
		return false
	}
	return true
}
