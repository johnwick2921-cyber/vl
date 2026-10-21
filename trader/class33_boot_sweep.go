package trader

import (
	"fmt"
	"strings"
	"sync"

	"time"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/trader/types"
)

// ── CLASS 33 (2026-09-02) — BOOT-TIME ARM SWEEP ──────────────────────────────
// The armed_orders ledger survives a restart; the ORDERS it describes live at
// NinjaTrader. 2026-09-02 00:16 CT a cutover landed with S1 @29044 and S3
// @29068.05 resting: the old process died, its broker orders did not, and they
// sat with NO listener for 15 minutes until the stale-window reconcile
// cancelled them at 00:31:48 — while the new binary re-armed its own S1/S3, so
// for minutes TWO S3 orders existed at the broker. A fill on the dead
// process's order would have been a position nobody's stop was attached to.
//
// This sweep runs ONCE per process per trader, at the head of the armed
// subsystem, BEFORE anything is authored or placed. Sweepable rows stamped by
// a DIFFERENT boot receive cancellation intent; their rows stay cancel_pending
// until confirmPendingCancels verifies a persisted broker snapshot. Pending
// rows are deliberately excluded from this sweep (B2: the sweep never writes
// 'cancelled' at send time — that is F8's rule, everywhere).
// It generalises the 0C shadow sweep (armed_executor.go — "the first cycle
// after boot IS the boot-time sweep") from shadowed conditions to ALL pre-boot
// arms. The stale-window reconcile stays exactly as it is: the backstop.

// BootSweepReason is the ledger state_reason written on every swept row.
// 0B: the prefix is the CONTRACT — store.IsBootSweepReason keys on it, so a
// swept row re-authorizes under the same plan version while owner cancels stay
// sticky. Never reword the leading token.
const BootSweepReason = store.BootSweepReasonPrefix + ": pre-boot order, process restarted"

// bootSweepDone marks the traders whose sweep has COMPLETED. A deferred sweep
// (NT8 link not ready) never marks — it retries on the next cycle, because a
// sweep that could not reach the broker has proved nothing.
var bootSweepDone sync.Map // trader id → struct{}

// ResetBootSweepForTest clears the per-trader latch (fixtures only).
func ResetBootSweepForTest() { bootSweepDone = sync.Map{} }

// sweepPreBootArms is the wired entry point: it resolves the NT8 link and
// delegates. Deferring (no link) is LOUD and leaves the latch unset.
func (at *AutoTrader) sweepPreBootArms(ledger *store.ArmedOrderStore) {
	if _, done := bootSweepDone.Load(at.id); done {
		return
	}
	nt := at.armedTrader()
	if nt == nil {
		at.logWarnf("🛡 boot sweep DEFERRED (class 33): NT8 link not ready — pre-boot arms are UNVERIFIED this cycle; retrying next cycle")
		return
	}
	at.sweepPreBootArmsWith(ledger, func(signalID string) error {
		// B2: the send rides the same broker-book gate as every other cancel —
		// a sweep that cannot prove the order exists refuses and retries next
		// cycle rather than 'cancelling' on faith.
		if !at.cancelSignalIfSafe(nt.CancelOrder, signalID, "boot sweep", time.Now()) {
			return fmt.Errorf("cancel refused or send failed")
		}
		return nil
	})
}

// sweepPreBootArmsWith is the seam: cancelFn is the wire (nt.CancelOrder in
// production, a recorder in fixtures). Returns the number of rows swept.
func (at *AutoTrader) sweepPreBootArmsWith(ledger *store.ArmedOrderStore, cancelFn func(signalID string) error) int {
	if ledger == nil || cancelFn == nil {
		return 0
	}
	if _, done := bootSweepDone.Load(at.id); done {
		return 0
	}
	bootID := store.ProcessBootID()
	rows, err := ledger.ListPreBoot(at.id, bootID)
	if err != nil {
		// A24: never conclude "nothing to sweep" from a failed read.
		at.logWarnf("🛡 boot sweep FAILED to read the ledger (class 33): %v — pre-boot arms UNVERIFIED, retrying next cycle", err)
		return 0
	}
	swept, skipped, failed := 0, 0, 0
	for _, r := range rows {
		if r.TraderID != at.id {
			continue
		}
		if r.SignalID == "" {
			// Authorized but never placed: nothing exists at the broker, so
			// cancelling it would silently disable the play for this version.
			// Left alone deliberately — the executor places it under THIS
			// process and stamps it.
			skipped++
			at.logInfof("🛡 boot sweep: %s %s pre-boot but never placed (no signal id) — left armed for this process to place", r.Session, r.Scenario)
			continue
		}
		if cerr := cancelFn(r.SignalID); cerr != nil {
			// The order may still be live: do NOT mark the ledger terminal on
			// a failed cancel — that would hide a live order behind a clean
			// ledger. Retried next cycle (the latch stays unset below).
			failed++
			at.logWarnf("🛡 boot sweep CANCEL FAILED (class 33): %s %s signal=%s entry=%.2f — order may still be LIVE at the broker: %v",
				r.Session, r.Scenario, r.SignalID, r.EntryPx, cerr)
			continue
		}
		// B2: the send is a REQUEST, never a settlement. The row becomes
		// cancel_pending with the boot_sweep reason; confirmPendingCancels
		// settles it through ConfirmCancel with a persisted snapshot id once a
		// fresh post-request book shows the order absent.
		if rerr := ledger.RequestCancel(r.ID, BootSweepReason, time.Now().UnixMilli()); rerr != nil {
			failed++
			at.logWarnf("🛡 boot sweep: cancel requested at the broker but the ledger request FAILED for %s %s: %v", r.Session, r.Scenario, rerr)
			continue
		}
		swept++
		at.logWarnf("🛡 boot sweep cancel REQUESTED for pre-boot arm (class 33): %s %s %s entry=%.2f stop=%.2f signal=%s authored_by_boot=%q this_boot=%q — cancel_pending until the broker book confirms; the process that placed it is gone",
			r.Session, r.Scenario, r.Side, r.EntryPx, r.StopPx, r.SignalID, r.BootID, bootID)
	}
	if swept > 0 {
		// B2: the recorded counter counts the SEND (the request), not the
		// settlement — a requested cancel the book later shows never existed is
		// still a swept send.
		if _, ierr := store.IncBootSwept(at.store, swept); ierr != nil {
			at.logWarnf("🛡 boot sweep: counter write failed: %v", ierr)
		}
	}
	if failed > 0 {
		// Unfinished business — retry next cycle rather than latch a partial sweep.
		at.logWarnf("🛡 boot sweep INCOMPLETE (class 33): %d row(s) could not be cancelled — NOT latching; retrying next cycle", failed)
		return swept
	}
	bootSweepDone.Store(at.id, struct{}{})
	// F12: leg 4's source is RESOLVED, not asserted. Before this wave the line
	// said "leg4=ledger" as a literal; once the broker answers, that literal is
	// a lie printed at every boot.
	book, acct, sym := at.brokerBook()
	at.logInfof("%s", BootSweepBootLine(swept, skipped, Leg4SourceLabel(book, acct, sym, time.Now())))
	at.logInfof("🔌 %s", ntwire.AddonBuildLine(at.farSideBuildID(), ntwire.ExpectedAddonBuild))
	at.logInfof("🔌 %s", ntwire.OrderSnapshotLineAt(book, acct, sym, time.Now()))
	return swept
}

// BootSweepBootLine is the pure boot line (fixture-pinned wording). leg4Source
// is passed in rather than written here: F12 made it a resolved value, and a
// literal in a boot line is a claim that cannot fail (A24).
func BootSweepBootLine(swept, skippedUnplaced int, leg4Source string) string {
	// B2: the count is the SEND (cancel requests), not settled rows.
	return fmt.Sprintf("🛡 cutover safety (class 33): gate legs=5 · leg4=%s · boot sweep requested cancel on %d pre-boot arm(s) (%d authorized-but-never-placed left for this process)",
		leg4Source, swept, skippedUnplaced)
}

// ledgerOpenOrders renders THIS trader's non-terminal rows. Leg 4 separately
// counts unplaced ARMED rows; placed/unconfirmed rows cross-check the broker.
//
// UG-2 (release #24): every row is an order on THIS trader's instrument
// (at.futuresSymbol()). A query for another symbol — the chart's ES/AAPL/
// EUR-USD tab — answers `[]` (that symbol has no ledger rows), not this
// trader's MNQ orders. An EMPTY symbol is the whole-book question (the dead-man
// reconcile probe, the post-reconnect sweep) and keeps every row.
func (at *AutoTrader) ledgerOpenOrders(symbol string) ([]types.OpenOrder, error) {
	if at.store == nil || at.store.ArmedOrders() == nil {
		return nil, fmt.Errorf("armed_orders ledger unavailable")
	}
	if !ledgerSymbolMatches(symbol, at.futuresSymbol()) {
		return []types.OpenOrder{}, nil
	}
	rows, err := at.store.ArmedOrders().ListNonTerminal(at.id)
	if err != nil {
		return nil, err
	}
	out := make([]types.OpenOrder, 0, len(rows))
	for _, r := range rows {
		if r.TraderID != at.id {
			continue
		}
		side := "BUY"
		posSide := "LONG"
		// T6 (class 28): UpsertArm stores the side UPPER ("SHORT"); a literal
		// "short" compare served every SHORT arm as BUY/LONG on
		// /api/open-orders and the chart. One canonicalizer, read side too
		// (legacy lowercase rows predate the write-side fix).
		if store.CanonicalArmSide(r.Side) == "SHORT" {
			side, posSide = "SELL", "SHORT"
		}
		typ := "LIMIT"
		if r.Kind == "stop_entry" {
			typ = "STOP_MARKET"
		}
		status := "NEW"
		switch strings.ToLower(strings.TrimSpace(r.State)) {
		case store.StateArmed:
			status = "ARMED" // authorized, NOT yet at the broker
		case store.StatePlacePending:
			// SENT, NOT CONFIRMED (2026-09-07). Nothing may render this as an
			// order resting at the broker: on 2026-09-07 the chart drew a line
			// for arm 117 — at the STOP price, labelled "Limit" — while NT8 had
			// already refused it and the book stayed empty for 33 minutes.
			status = "PENDING — awaiting broker receipt"
		case store.StateCancelPending:
			// T29 (release #22): a cancel was SENT and not yet confirmed. It is
			// not a resting order the owner can rely on (row 363 stayed
			// cancel_pending for hours after the broker book showed it gone).
			status = "CANCEL_PENDING — cancel sent, not yet confirmed"
		}
		// T5 (release #22): the row's own signed size — a mentor arm carries
		// its contracts (row 362 = 3); a NULL (planner) row keeps the 1 it
		// always rendered. Price is the ENTRY; StopPrice is the bracket stop.
		qty := 1.0
		if r.Contracts != nil && *r.Contracts > 0 {
			qty = float64(*r.Contracts)
		}
		// UG-5 (release #24): a split mentor arm carries leg 1's OWN take-profit
		// for leg1_qty of its contracts (row 362: 2 of 3 at 30725.25); TargetPx
		// is the runner's. Both ride the row, so both are served.
		var leg1TP float64
		var leg1Qty int
		if r.Leg1Qty != nil && *r.Leg1Qty > 0 && r.Leg1TP > 0 {
			leg1TP, leg1Qty = r.Leg1TP, *r.Leg1Qty
		}
		out = append(out, types.OpenOrder{
			OrderID: r.SignalID, Symbol: at.futuresSymbol(), Side: side, PositionSide: posSide,
			Type: typ, Price: r.EntryPx, StopPrice: r.StopPx, Quantity: qty, Status: status,
			ArmState: r.State, TargetPrice: r.TargetPx, ZoneLo: r.ZoneLo, ZoneHi: r.ZoneHi,
			Leg1TP: leg1TP, Leg1Qty: leg1Qty,
			Source: "armed_orders ledger (broker acceptance is a separate observation)",
		})
	}
	return out, nil
}

// ledgerSymbolMatches (UG-2) reports whether a queried symbol names the
// trader's instrument: "" = the whole book; otherwise the same CME root
// ("MNQ", "MNQZ6", "MNQ 12-26" all name MNQ), and a non-futures symbol only on
// an exact (case-insensitive) match.
func ledgerSymbolMatches(query, traderSymbol string) bool {
	q := strings.TrimSpace(query)
	if q == "" {
		return true
	}
	if qr, tr := market.FuturesRoot(q), market.FuturesRoot(traderSymbol); qr != "" || tr != "" {
		return qr == tr
	}
	return strings.EqualFold(q, strings.TrimSpace(traderSymbol))
}
