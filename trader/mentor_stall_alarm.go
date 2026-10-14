package trader

import (
	"fmt"
	"strings"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/telemetry"
)

// ── STALLED-SETUP ALARM (release #22, owner order 10-13 ~23:00 CT) ─────────
//
// A setup kind the mentor evaluator keeps EMITTING but that never reaches the
// ledger (armed_orders) is stalled, and it can sit silent for days (the
// 10-08..10-13 level-order bug: 0 lvl- rows while the evaluator kept finding
// level touches). This file maps each placement intent to a canonical kind,
// feeds the telemetry emit/placed counters, and raises the once-per-day P1
// alert when a kind crosses "emitted >= 3, placed = 0" while mentor mode is on
// and the market is open.

// mentorSetupKinds is the canonical kind order for the stalled-setup surface.
var mentorSetupKinds = []string{"lvl", "isb", "isb-reverse", "phl/plh", "box", "swing"}

// mentorSetupKind maps one placement intent to the canonical setup kind the
// stalled-setup alarm counts. Order matters:
//   - a "lvl-" ArmID is the resting level order (the PHL/PLH setup emitted as a
//     first-touch level arm — the 10-13 bug's kind);
//   - the reverse ISB shares Setup "ISB" with the normal ISB but is a stop
//     ENTRY, while the normal ISB is a stop-LIMIT entry;
//   - "phl/plh" is the defensive branch for a PHL/PLH that ever emits WITHOUT a
//     level-arm id (today every PHL/PLH becomes a lvl- arm, so "lvl" wins).
func mentorSetupKind(in mentor.Intent) string {
	if in.Action != mentor.PlaceStopEntry && in.Action != mentor.PlaceStopLimitEntry {
		return "" // only placement intents are "seen" candidates
	}
	if strings.HasPrefix(strings.TrimSpace(in.ArmID), "lvl-") {
		return "lvl"
	}
	switch in.Setup {
	case "ISB":
		if in.Action == mentor.PlaceStopLimitEntry {
			return "isb"
		}
		return "isb-reverse"
	case "PHL", "PLH":
		return "phl/plh"
	case "BOX":
		return "box"
	case "SWING4H":
		return "swing"
	}
	return ""
}

// mentorCountSetupEmitted records one placement intent the evaluator emitted.
// Call it at the dispatch site — the intent reached the injector, whatever the
// rule gate does next.
func (at *AutoTrader) mentorCountSetupEmitted(in mentor.Intent) {
	if k := mentorSetupKind(in); k != "" {
		telemetry.IncSetupStallEmitted(at.id, k)
	}
}

// mentorCountSetupPlaced records one placement intent that reached the ledger.
// Call it where the armed_orders row is authored (mentorArmIntent).
func (at *AutoTrader) mentorCountSetupPlaced(in mentor.Intent) {
	if k := mentorSetupKind(in); k != "" {
		telemetry.IncSetupStallPlaced(at.id, k)
	}
}

// mentorStallAlarmCheck fires the stalled-setup P1 alert once per kind per
// session day. It is alert-only (no trading behaviour changes) and runs after
// the evaluator's intents are dispatched each tick.
func (at *AutoTrader) mentorStallAlarmCheck(now time.Time) {
	if !kernel.IsCMEOpen(now) {
		return
	}
	for _, a := range telemetry.SetupStallAlertDue(at.id) {
		window := kernel.CMESessionDayStart(now).Format("2006-01-02")
		title := fmt.Sprintf("Mentor setup stalled: %s", a.Kind)
		body := fmt.Sprintf("%s: %d emitted, %d reached the ledger since %s 17:00 CT", a.Kind, a.Emitted, a.Placed, window)
		at.emitAlert("P1", "mentor-setup-stalled", fmt.Sprintf("mentor-setup-stalled:%s:%s", at.id, a.Kind), title, body)
		at.logWarnf("🧑‍🏫 mentor setup stalled: %s", body)
	}
}
