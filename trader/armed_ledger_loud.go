package trader

import (
	"errors"
	"fmt"
	"sync"

	"vl/store"
	"vl/telemetry"
)

// P1-E (audit 2026-09-26) — armed-order lifecycle writes must never fail
// silently. armLifecycleWrite is the one chokepoint every SetState /
// RequestCancel discard site rides: on failure it logs WARN (DB sink), records
// a counter on the errors API, and — when the row still describes a live broker
// order (it carries a signal) — fails the slot closed so the next placement is
// refused until a later successful write clears it (the reconcile passes retry
// these writes).
//
// Without the latch, a row whose cancel/fill write failed leaves the ledger
// describing an order the broker no longer matches, and the next pass could
// place a second order on top (the class-81 stacking shape).

type armedLedgerFailKey struct {
	trader, plan, scenario string
	leg                    int
}

var armedLedgerFailures sync.Map // armedLedgerFailKey → struct{}

func armedLedgerFailKeyOf(r store.ArmedOrderDB) armedLedgerFailKey {
	// REL18-A #6: the latch is trader-scoped — under the trader-keyed plan_id,
	// B's write must never clear (or set) A's fail-closed latch.
	return armedLedgerFailKey{r.TraderID, r.PlanID, r.Scenario, r.LegIndex}
}

func (at *AutoTrader) armLifecycleWrite(op string, r store.ArmedOrderDB, err error) {
	if err == nil {
		armedLedgerFailures.Delete(armedLedgerFailKeyOf(r))
		return
	}
	// Release #22 F-1: a cancel request refused because the row is already
	// terminal is a REFUSAL, not a failed write — the ledger already matches
	// the broker's settled outcome, so the slot must not fail closed for it.
	// The store logged it (once per row outcome) and counted it (gate-block
	// request_cancel_terminal + arm:request_cancel_terminal_refused).
	if errors.Is(err, store.ErrArmCancelTerminal) {
		return
	}
	telemetry.RecordError(at.id, "armed_ledger_write_failed",
		fmt.Sprintf("%s %s/%s leg %d signal=%s state=%s: %v", op, r.PlanID, r.Scenario, r.LegIndex+1, shortID(r.SignalID), r.State, err),
		telemetry.CostNone)
	at.logWarnf("⚠️ armed ledger %s FAILED for row %d %s/%s leg %d (signal=%s state=%s): %v",
		op, r.ID, r.PlanID, r.Scenario, r.LegIndex+1, shortID(r.SignalID), r.State, err)
	if r.SignalID != "" {
		armedLedgerFailures.Store(armedLedgerFailKeyOf(r), struct{}{})
	}
}

// armSlotBlockedForPlacement reports whether the slot's last lifecycle write
// failed and no successful write has cleared it since.
func armSlotBlockedForPlacement(r store.ArmedOrderDB) bool {
	_, ok := armedLedgerFailures.Load(armedLedgerFailKeyOf(r))
	return ok
}

// resetArmedLedgerFailuresForTest clears the latch (fixtures only).
func resetArmedLedgerFailuresForTest() { armedLedgerFailures = sync.Map{} }
