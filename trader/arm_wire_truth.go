package trader

import (
	"fmt"
	"strings"

	"vl/store"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
)

// F-7 (release #22 CANCEL-SETTLE, review-1014 EXECUTION.md F-7) — THE LEDGER
// RECORDS WHAT WENT ON THE WIRE.
//
// Every entry send rounds its bracket in the sender (tcp_trader.go:
// WireBracket — the stop AWAY from the entry, the target to the nearest tick;
// on a mentor split the leg-1 TP re-based on the trigger and rounded by
// wireLeg1TP). The armed_orders row kept the AUTHORED numbers, so the ledger
// disagreed with NT8 on every off-grid price (rows 347, 350, 356, 362).
//
// recordArmWirePrices runs at each placement site right after a successful
// send and writes the frame's prices onto the row (one ledger write, CAS on the
// signal id). It changes NO rounding and sends nothing: the stop and target
// come from the SAME pure function the sender calls with the SAME arguments,
// and the split from the sender's own record of what the frame carried
// (SplitSentFor). TestR22LedgerRecordsTheWirePricesOnEveryPlacementPath et al.
// read the real frame bytes off a loopback AddOn and compare.

// armWirePriceWriter is the ledger seam; *store.ArmedOrderStore satisfies it.
// A ledger fake without it (the stop-entry unit fakes) records nothing.
type armWirePriceWriter interface {
	RecordWirePrices(id int64, signalID string, w store.ArmWirePrices) error
}

// sentSplitReader is the sender's record of the split one frame carried;
// *trader/ninjatrader.TCPTrader satisfies it.
type sentSplitReader interface {
	SplitSentFor(signalID string) (ntTrader.SentSplit, bool)
}

// recordArmWirePrices writes the frame's stop/target (and, when splitFrom is
// the sender of a split-capable frame, its leg1_qty/leg1_tp) onto the row.
// entry/stop/target are EXACTLY the arguments the placement passed to the
// sender. splitFrom is nil for a send that cannot carry a split.
func (at *AutoTrader) recordArmWirePrices(ledger any, r store.ArmedOrderDB, sid, side string, entry, stop, target float64, splitFrom any) {
	w, ok := ledger.(armWirePriceWriter)
	if !ok || strings.TrimSpace(sid) == "" {
		return
	}
	tick := ntTrader.InstrumentTickSize(at.futuresSymbol())
	_, wStop, wTarget, err := ntTrader.WireBracket(side, entry, stop, target, tick)
	if err != nil {
		// Unreachable after a successful send (the sender ran the same
		// function and would have refused) — but never silent.
		telemetry.RecordError(at.id, "armed_wire_prices_unrecorded", fmt.Sprintf("%s signal=%s: %v", r.Scenario, shortID(sid), err), telemetry.CostNone)
		at.logWarnf("⚠️ armed %s signal=%s: wire prices NOT recorded — %v; the row keeps the authored stop %.2f target %.2f", r.Scenario, shortID(sid), err, stop, target)
		return
	}
	wp := store.ArmWirePrices{Stop: wStop, Target: wTarget}
	if sr, ok := splitFrom.(sentSplitReader); ok && splitFrom != nil {
		wp.Split = true
		if sp, sent := sr.SplitSentFor(sid); sent {
			wp.Leg1Qty, wp.Leg1TP = sp.Leg1Qty, sp.Leg1TP
		}
	}
	if err := w.RecordWirePrices(r.ID, sid, wp); err != nil {
		telemetry.RecordError(at.id, "armed_wire_prices_unrecorded", fmt.Sprintf("%s signal=%s: %v", r.Scenario, shortID(sid), err), telemetry.CostNone)
		at.logWarnf("⚠️ armed %s signal=%s: wire prices NOT recorded (wire stop %.2f target %.2f): %v — the row keeps the authored stop %.2f target %.2f",
			r.Scenario, shortID(sid), wStop, wTarget, err, stop, target)
		return
	}
	moved := ntTrader.WireMoved(stop, wStop, tick) || ntTrader.WireMoved(target, wTarget, tick)
	leg := ""
	if wp.Split {
		moved = moved || ntTrader.WireMoved(r.Leg1TP, wp.Leg1TP, tick)
		leg = fmt.Sprintf(" · leg1 %d@%.2f (authored %s@%.2f)", wp.Leg1Qty, wp.Leg1TP, intPtrText(r.Leg1Qty), r.Leg1TP)
	}
	if moved {
		at.logInfof("🧾 armed %s signal=%s: ledger set to the WIRE prices — stop %.2f (authored %.4f) target %.2f (authored %.4f)%s",
			r.Scenario, shortID(sid), wStop, stop, wTarget, target, leg)
	}
}

// intPtrText prints a nullable count as read: n/a when absent (L7).
func intPtrText(p *int) string {
	if p == nil {
		return "n/a"
	}
	return fmt.Sprintf("%d", *p)
}
