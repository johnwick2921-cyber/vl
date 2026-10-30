package trader

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// ── Release #25 PK-2 (STRICT-12345 F-3) — EXIT ON THESIS DEATH ──────────────
//
// 649, 650, 652 and 653 were each held past the moment their own plan said the
// thesis was dead (report §1 "plan while in the trade"): 649's plan went
// DORMANT at 21:00:01, 650's chain wrote a SHORT v4 at 00:23:05, 652's
// authored invalidation (5m close > 30810.57) printed at 03:00, 653's
// (5m close < 30506.25) at 09:00 — and every one rode to the full stop.
//
// Knob day_plan.planner_exit_on_thesis_death (store.ResolvePlannerExitOnThesisDeath),
// DEFAULT OFF = nothing here runs. On the 60-s monitor beat (monitorTick):
//   - invalidation_only → close a planner position at market when a 5m close
//     COMPLETED AFTER THE FILL is beyond its cited scenario's authored
//     invalidation (kernel.EvaluateAuthoredInvalidationAt — the ONE grammar:
//     "5m close above|below <price>", "2x5m close …"; anything else stays
//     UNKNOWN and never exits);
//   - all → also when the plan version that authored it goes dormant
//     (lifecycle dormant / superseded:flip / superseded:death), or when a
//     NEWER version of the same plan chain carries the opposite bias.
// The exit is flattenPosition — the bot's own close path (EOD flat, daily-loss
// flat): CloseLong/CloseShort on THIS trader's bound account, the whole
// position, then the bracket cancelled. Reduce-only by construction: it closes
// the open quantity, it never opens.
//
// NEVER for: a mentor-mode trader, a mentor arm (armed_orders.origin=mentor or
// a mentor plan id), a MANUAL position (origin=manual), a position not opened
// by an armed planner entry, or one whose arm row cannot be found (the
// position is then left alone and the reason logged once — an exit needs a
// positive identification).

// ThesisDeathExitClass is the exit counter (gate-block table + recorded).
const ThesisDeathExitClass = "thesis_death_exit"

// Thesis-death reasons (counted as thesis_death_exit_<reason>).
const (
	ThesisDeathReasonInvalidation = "invalidation"
	ThesisDeathReasonPlanDormant  = "plan_dormant"
	ThesisDeathReasonOpposite     = "opposite_plan"
)

// thesisDeathResend: a position the ledger still shows open after an exit was
// sent is re-sent at most this often (the daily-loss flatten cadence).
const thesisDeathResend = 2 * time.Minute

var (
	thesisDeathMu      sync.Mutex
	thesisDeathSent    = map[string]time.Time{}
	thesisDeathSkipLog = map[string]bool{}
)

// planDormantLifecycle reports whether a plan row's lifecycle says its thesis
// is dead: dormant, or a dormant row superseded by its re-read.
func planDormantLifecycle(lifecycle string) bool {
	l := strings.ToLower(strings.TrimSpace(lifecycle))
	return l == "dormant" || strings.HasPrefix(l, "superseded:")
}

// oppositeBias reports whether a plan bias is the opposite side of a position.
func oppositeBias(bias, side string) bool {
	b := strings.ToLower(strings.TrimSpace(bias))
	s := positionSide(side)
	return (s == "long" && b == "short") || (s == "short" && b == "long")
}

// thesisDeathEligible is the pure identification: "" = a planner position this
// switch may exit; otherwise why it is left alone.
func thesisDeathEligible(p store.TraderPosition, arm *store.ArmedOrderDB) string {
	switch {
	case store.IsManualOrigin(p.Origin):
		return "manual position (origin=manual)"
	case p.Source != "armed_entry":
		return "not an armed planner entry (source=" + p.Source + ")"
	case strings.TrimSpace(p.PlanID) == "" || mentorPlanIDIsMentor(p.PlanID):
		return "no planner plan link (plan_id=" + p.PlanID + ")"
	case arm == nil:
		return "its arm row is not found — no positive identification"
	case isMentorArmOrigin(*arm):
		return "mentor arm (origin=mentor)"
	}
	return ""
}

// thesisDeathFor judges ONE eligible position: the reason ("" = alive) and the
// evidence detail.
func (at *AutoTrader) thesisDeathFor(p store.TraderPosition, mode string, now time.Time) (string, string) {
	row, err := at.store.Plan().GetPlan(p.PlanID, p.PlanVersion)
	if err != nil || row == nil {
		return "", ""
	}
	var doc kernel.PlanDoc
	docOK := json.Unmarshal([]byte(row.Doc), &doc) == nil
	if docOK && market.FuturesBarsProvider != nil {
		for _, sc := range doc.Scenarios {
			if !strings.EqualFold(sc.ID, p.CitedScenarioID) {
				continue
			}
			v := kernel.EvaluateAuthoredInvalidationAt(sc, market.FuturesBarsProvider(p.Symbol, "1m", 15), now)
			if v.Known && v.Invalidated && v.At.UnixMilli() > p.EntryTime {
				return ThesisDeathReasonInvalidation, fmt.Sprintf("%s %s", sc.ID, v.Reason)
			}
			break
		}
	}
	if mode != store.ThesisDeathAll {
		return "", ""
	}
	if planDormantLifecycle(row.Lifecycle) {
		return ThesisDeathReasonPlanDormant, fmt.Sprintf("plan %s v%d lifecycle=%s", p.PlanID, p.PlanVersion, row.Lifecycle)
	}
	if latest, lerr := at.store.Plan().GetLatestPlan(p.PlanID); lerr == nil && latest != nil && latest.Version > p.PlanVersion {
		var ld kernel.PlanDoc
		if json.Unmarshal([]byte(latest.Doc), &ld) == nil && oppositeBias(ld.Bias.Direction, p.Side) {
			return ThesisDeathReasonOpposite, fmt.Sprintf("newer plan %s v%d bias=%s against the %s position", p.PlanID, latest.Version, strings.ToLower(ld.Bias.Direction), positionSide(p.Side))
		}
	}
	return "", ""
}

// maybeExitOnThesisDeath is the monitor-beat consumer. OFF → returns at once.
func (at *AutoTrader) maybeExitOnThesisDeath(now time.Time) {
	if at == nil || at.config.StrategyConfig == nil {
		return
	}
	mode, _ := store.ResolvePlannerExitOnThesisDeath(at.dayPlanCfg())
	if mode == store.ThesisDeathOff || at.store == nil || at.trader == nil {
		return
	}
	if at.modeOwnsExits() {
		return // the mentor's exits own every mentor-mode position (R3); pep's fixed stop/target and flat own every pep position
	}
	positions, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil {
		at.logWarnf("🪦 thesis-death exit: open-position read failed (%v) — nothing judged this beat", err)
		return
	}
	for i := range positions {
		if positions[i] == nil {
			continue
		}
		p := *positions[i]
		key := at.id + "|" + strconv.FormatInt(p.ID, 10)
		var arm *store.ArmedOrderDB
		if p.EntryOrderID != "" {
			arm, _ = at.store.ArmedOrders().FindBySignal(at.id, p.EntryOrderID)
		}
		if why := thesisDeathEligible(p, arm); why != "" {
			thesisDeathMu.Lock()
			seen := thesisDeathSkipLog[key]
			thesisDeathSkipLog[key] = true
			thesisDeathMu.Unlock()
			if !seen {
				at.logInfof("🪦 thesis-death exit: position %d (%s %s) left alone — %s", p.ID, p.Symbol, p.Side, why)
			}
			continue
		}
		reason, detail := at.thesisDeathFor(p, mode, now)
		if reason == "" {
			continue
		}
		thesisDeathMu.Lock()
		last, sent := thesisDeathSent[key]
		if sent && now.Sub(last) < thesisDeathResend && !now.Before(last) {
			thesisDeathMu.Unlock()
			continue
		}
		thesisDeathSent[key] = now
		thesisDeathMu.Unlock()
		resend := ""
		if sent {
			resend = " (RE-SENT — the ledger still shows it open)"
		} else {
			at.countPlannerKnob(ThesisDeathExitClass, p.PlanTradeDate, p.PlanSession)
			at.countPlannerKnob(ThesisDeathExitClass+"_"+reason, p.PlanTradeDate, p.PlanSession)
		}
		at.logWarnf("🪦 thesis-death EXIT: position %d (%s %s qty %.0f, %s v%d %s)%s reason=%s — %s · closing at market on this trader's account (planner_exit_on_thesis_death=%s)",
			p.ID, p.Symbol, p.Side, p.Quantity, p.PlanID, p.PlanVersion, p.CitedScenarioID, resend, reason, detail, mode)
		at.flattenPosition(&p, "🪦 THESIS-DEATH EXIT")
	}
}
