package trader

import (
	"strings"
	"time"

	"vl/kernel"
	"vl/store"
)

// ── W-EXEC-TRUTH W0 (f) — WITHDRAW RESTING ENTRIES, ENTRIES ONLY ────────────
//
// A hold used to REFUSE new entries only: a resting armed limit or stop kept
// resting and could FILL during an update. And a loss-limit trip (the
// consecutive-loss breaker, the daily force-flat) refused new entries too,
// while an entry already resting at NT8 could still fill after the halt
// (defect 7 — money safety, CTO Q14).
//
// withdrawEntriesIfDue runs from monitorTick — the per-trader wall-clock beat
// that keeps running when runCycle does not (CME closed, EOD flat, a stopped
// scan) — whenever something asks for a withdraw:
//
//	maintenance  a hold written with --withdraw-entries (never implied)
//	install      an update job is pending (requested → preflight): the
//	             install-pending freeze (EXECUTION P2-2, 10-14)
//	breaker      the consecutive-loss halt has tripped
//	force-flat   the daily force-flat has tripped
//
// It cancels ENTRIES ONLY: every armed row that carries a broker signal and
// is not terminal, through the same filled-arm guard (cancelSafetyFor) and the
// AddOn's entry-only cancel_order; the row goes to cancel_pending. A pending
// cancel is NEVER shown as complete: it becomes 'cancelled' only on received
// evidence — an order_update, or the order's absence from a fresh PERSISTED
// snapshot (confirmPendingCancels). Protection, reconciliation and exits are
// never touched. An arm with no signal was never sent: it stays armed and the
// admission gate refuses it.
//
// ARMED rows are the complete set of resting entries: Picture never rests an
// entry (its send is a market order — it fills or is refused, it does not
// wait at a price), and the AI path's entries are market orders too.

// WithdrawReasonPrefix marks a withdraw in the ledger's state_reason. The
// store owns it: every later lifecycle write APPENDS to a withdrawn row's
// reason after store.WithdrawReasonSep instead of replacing it, so the view
// below still finds the row after a re-request, an order_update or a
// snapshot confirm (store.reasonKeepingWithdraw).
const WithdrawReasonPrefix = store.WithdrawReasonPrefix

// withdrawHead is the reason head a withdraw writes for why.
func withdrawHead(why string) string { return WithdrawReasonPrefix + why }

// withdrawDueReason says why resting entries must be withdrawn now ("" = no).
func (at *AutoTrader) withdrawDueReason(now time.Time) string {
	if st, ok := maintenanceState(); ok && st.Held && !st.Corrupt && st.Hold.WithdrawEntries {
		return "maintenance job " + st.Hold.JobID
	}
	// EXECUTION P2-2 (10-14): the install-pending freeze stopped NEW entries
	// only; a planner limit already RESTING at NT8 stayed working, and the
	// preflight's ledger_exposure leg then refused the install (job 30a8bbf6 /
	// row 363, by another route). While a job is pending, resting entries are
	// withdrawn too — entries only, the same path as a --withdraw-entries hold.
	if v := InstallPendingStatus(now); v.Pending {
		job := v.JobID
		if job == "" {
			job = "n/a"
		}
		return "install_pending job " + job
	}
	if r := kernel.DailyForceFlatReasonAt(at.id, now); r != "" { // r22: the beat's clock judges the latch's session-day
		return "daily_force_flat"
	}
	if _, halted := at.consecutiveLossHaltedAt(now); halted {
		return "consecutive_loss"
	}
	return ""
}

// withdrawEntriesIfDue is the monitorTick hook.
func (at *AutoTrader) withdrawEntriesIfDue(now time.Time) {
	if at == nil || at.store == nil {
		return
	}
	why := at.withdrawDueReason(now)
	// m445: missing bars withdraw resting entries too (named in the ledger row).
	// swingOnly = only the deep window is blocked, so only SWING4H rows go: an
	// intraday arm placed on a clean intraday window must not be cancelled and
	// re-authored every beat.
	var keep func(store.ArmedOrderDB) bool
	if why == "" {
		var swingOnly bool
		if why, swingOnly = at.integrityWithdrawReason(now); why == "" {
			return
		} else if swingOnly {
			keep = isSwingRow
		}
	}
	ledger := at.store.ArmedOrders()
	if n := at.withdrawRestingEntriesWhere(ledger, why, now, keep); n > 0 {
		at.logWarnf("🧹 withdraw (%s): %d resting entry order(s) sent a cancel — each stays cancel_pending until the broker confirms", why, n)
	}
	var resend func(string) error
	if nt := at.armedTrader(); nt != nil {
		resend = func(sid string) error {
			// A refused re-request is NOT a re-request (canon 35):
			// confirmPendingCancels neither records nor counts it.
			if !at.cancelSignalIfSafe(nt.CancelOrder, sid, "withdraw re-request", now) {
				return errCancelRefused
			}
			return nil
		}
	}
	at.confirmPendingCancels(ledger, resend, now)
}

// withdrawRestingEntries sends the entry-only cancel for every resting entry
// of this trader and moves it to cancel_pending. Returns how many it asked.
func (at *AutoTrader) withdrawRestingEntries(ledger *store.ArmedOrderStore, why string, now time.Time) int {
	return at.withdrawRestingEntriesWhere(ledger, why, now, nil)
}

// withdrawRestingEntriesWhere is withdrawRestingEntries limited to the rows
// `only` accepts (nil = every resting entry).
func (at *AutoTrader) withdrawRestingEntriesWhere(ledger *store.ArmedOrderStore, why string, now time.Time, only func(store.ArmedOrderDB) bool) int {
	rows, err := ledger.ListNonTerminal(at.id)
	if err != nil {
		at.logWarnf("🧹 withdraw (%s): ledger read failed — nothing withdrawn this beat: %v", why, err)
		return 0
	}
	nt := at.armedTrader()
	if nt == nil {
		// No NinjaTrader wire: no cancel can be sent, so no row may claim
		// one was (cancel_pending with nothing in flight is a fabricated state).
		if at.admitLast.changed("withdraw|no-wire", why) {
			at.logWarnf("🧹 withdraw (%s): no NinjaTrader trader bound — nothing withdrawn, nothing marked cancel_pending", why)
		}
		return 0
	}
	at.admitLast.clear("withdraw|no-wire")
	n := 0
	for _, r := range rows {
		if r.TraderID != at.id || strings.TrimSpace(r.SignalID) == "" ||
			store.IsTerminalArmState(r.State) || r.State == store.StateCancelPending {
			continue
		}
		if only != nil && !only(r) {
			continue
		}
		// The filled-arm guard: a row the ledger still calls working may have
		// filled — cancelling it by signal would reach its bracket.
		if v := at.cancelSafetyFor(r, now); !v.Allow {
			if at.admitLast.changed("withdraw|"+r.SignalID, v.Why) {
				at.logWarnf("🛟 withdraw cancel REFUSED %s signal=%s — %s", r.Scenario, shortID(r.SignalID), v.Why)
			}
			continue
		}
		if cerr := nt.CancelOrder(r.SignalID); cerr != nil {
			at.logWarnf("✕ withdraw cancel SEND failed %s signal=%s: %v", r.Scenario, shortID(r.SignalID), cerr)
		}
		// Recorded whether or not the send returned nil: the row is at the
		// broker until evidence says otherwise (class 81).
		if err := ledger.RequestCancel(r.ID, withdrawHead(why), now.UnixMilli()); err != nil {
			at.logWarnf("✕ withdraw: ledger write failed for %s: %v", r.Scenario, err)
			continue
		}
		n++
	}
	return n
}

// WithdrawView is the withdraw half of GET /api/maintenance: the rows the
// current job's withdraw asked NinjaTrader to cancel. A pending cancel is
// never shown as done, and a fill is never shown as a withdraw.
//
//	pending    cancel_pending — asked, no evidence yet
//	confirmed  cancelled — an order_update or a fresh persisted snapshot
//	           proved the order gone
//	filled     the entry FILLED before the cancel landed: a position exists
//	ended      any other terminal state (rejected, expired, …), with the state
//
// When the ledger could not be read the lists are ABSENT (null) and Unread
// says why — never an empty list for rows nobody read (L7).
type WithdrawView struct {
	Requested bool            `json:"requested"`
	Pending   []int64         `json:"pending"`
	Confirmed []int64         `json:"confirmed"`
	Filled    []int64         `json:"filled"`
	Ended     []WithdrawEnded `json:"ended"`
	Unread    string          `json:"unread,omitempty"`
}

// WithdrawEnded is a withdrawn row that ended in a state other than
// cancelled or filled.
type WithdrawEnded struct {
	ID    int64  `json:"id"`
	State string `json:"state"`
}

// maintenanceWithdrawView reads the withdraw for the current hold's job; nil
// when no hold asks for one.
func maintenanceWithdrawView(st *store.Store) *WithdrawView {
	ms, ok := maintenanceState()
	if !ok || !ms.Held || ms.Corrupt || !ms.Hold.WithdrawEntries {
		return nil
	}
	v := &WithdrawView{Requested: true}
	if st == nil {
		v.Unread = "no trader loaded — the ledger was not read"
		return v
	}
	rows, err := st.ArmedOrders().ListWithdrawn(withdrawHead("maintenance job " + ms.Hold.JobID))
	if err != nil {
		v.Unread = "ledger read failed: " + err.Error()
		return v
	}
	v.Pending, v.Confirmed, v.Filled, v.Ended = []int64{}, []int64{}, []int64{}, []WithdrawEnded{}
	for _, r := range rows {
		switch state := strings.ToLower(strings.TrimSpace(r.State)); {
		case state == store.StateCancelPending:
			v.Pending = append(v.Pending, r.ID)
		case store.IsCancelledArmState(state):
			v.Confirmed = append(v.Confirmed, r.ID)
		case state == store.StateFilled:
			v.Filled = append(v.Filled, r.ID)
		case store.IsTerminalArmState(state):
			v.Ended = append(v.Ended, WithdrawEnded{ID: r.ID, State: r.State})
		default:
			// Not terminal and not cancel_pending: the cancel was asked and the
			// row moved on without evidence either way — it is still pending.
			v.Pending = append(v.Pending, r.ID)
		}
	}
	return v
}
