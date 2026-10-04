package trader

import (
	"strconv"
	"strings"
	"time"

	"vl/store"
	"vl/telemetry"
)

// ── W-EXEC-TRUTH W0 (G1 + a) — the armed path at its send point ────────────
//
// G1: an 'armed' row a later pass placed without re-running the authoring
// gates. Proven by execution before this wave: a daily_force_flat refusal and
// a limit placement for the SAME leg in ONE pass. The authoring loop now
// records the legs whose every gate passed THIS pass (armAdmission) and the
// placement places only those. Then admitEntry — the one admission chain —
// runs for the row at its send point.

// armAdmission is the set of legs this pass's authoring gates admitted, keyed
// plan|scenario|leg (version-insensitive: the row's version is refreshed each
// pass by the authoring loop).
type armAdmission map[string]bool

func armAdmitKey(planID, scenario string, leg int) string {
	return planID + "|" + scenario + "|" + strconv.Itoa(leg)
}

func (a armAdmission) admit(planID, scenario string, leg int) {
	a[armAdmitKey(planID, scenario, leg)] = true
}

// retract withdraws an admission this pass granted: a leg whose ledger write
// was REFUSED (W5 R13 — the row under its key belongs to another opportunity)
// was not authored, so no placement may ride its admit.
func (a armAdmission) retract(planID, scenario string, leg int) {
	delete(a, armAdmitKey(planID, scenario, leg))
}

// armAdmitted is the placement-time check for one armed row: G1 first, then
// the one admission chain. false = do not place (the row stays armed); the
// second value is the refusal ("<class>: <text>") so a caller can report it
// (W3: the strict nudge's verdict and the zone row's last_verdict) — it used
// to be discarded here.
func (at *AutoTrader) armAdmitted(r store.ArmedOrderDB, side string, price float64, now time.Time, admitted armAdmission) (bool, string) {
	key := r.PlanID + ":" + r.Scenario + ":leg" + strconv.Itoa(r.LegIndex+1)
	// P0-b (mentor injector, CTO 1791040400571): the mentor injector is its
	// OWN authoring pass — a mentor-authored row is admitted without the
	// planner's admission set (the injector just authored it THIS cycle), and
	// then runs the SAME admitEntry gate chain as every other arm. With a
	// mentor-mode row the admitted-set membership would otherwise be
	// impossible: the planner's admission only names the AI plan's legs.
	if !mentorAuthoredRow(r) {
		// FAIL-CLOSED (CTO M2): no admitted set means no authoring pass stands in
		// front of this placement — nothing is admitted. A nil default would let a
		// direct caller (W3's event-driven pass) bypass G1 silently.
		if admitted == nil || !admitted[armAdmitKey(r.PlanID, r.Scenario, r.LegIndex)] {
			why := "not admitted this pass"
			if admitted == nil {
				why = "no authoring pass"
			}
			if at.admitLast.changed("arm-g1|"+key, why) {
				at.logWarnf("⏸ armed %s leg %d NOT placed — %s (G1); it stays armed until a pass admits it", r.Scenario, r.LegIndex+1, why)
				// counted once per change, like every arm refusal
				telemetry.IncGateBlock(at.id, "arm_not_admitted")
			}
			return false, "arm_not_admitted: " + why
		}
	}
	at.admitLast.clear("arm-g1|" + key)
	action := "open_long"
	if strings.EqualFold(side, "short") {
		action = "open_short"
	}
	if refusal, refused := at.admitEntry(admitIntent{
		Path: admitArm, Symbol: at.futuresSymbol(), Action: action, Now: now, Key: key, Price: price,
		Source: r.Source, // W5 D21 — a Picture-sourced row faces Picture's running / Day Plan checks
	}); refused {
		return false, refusal
	}
	return true, ""
}
