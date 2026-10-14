package trader

import (
	"fmt"
	"sort"
	"time"

	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── W-ONE-BUTTON M2 — the maintenance status (GET /api/maintenance) and the
// 🔒 boot line. Both READ the hold file, the entry barrier and the current
// connection's record; a value the process does not know is null on the API
// and n/a on the boot line — never a guess.

// MaintenanceAckView is the current connection's maintenance_ack.
type MaintenanceAckView struct {
	Received       string `json:"received"` // UTC, from the monotonic age of the ack
	AgeMs          int64  `json:"age_ms"`
	Held           bool   `json:"held"`
	JobID          string `json:"job_id"`
	QueuedCommands int    `json:"queued_commands"`
	BuildID        string `json:"build_id"`
	AcceptSeq      uint64 `json:"accept_seq"`
}

// MaintenanceStatusView is GET /api/maintenance.
type MaintenanceStatusView struct {
	Held          bool                `json:"held"`
	State         string              `json:"state"` // clear | held | unreadable | unconfigured
	JobID         *string             `json:"job_id"`
	Since         *string             `json:"since"`
	Reason        string              `json:"reason,omitempty"`
	InFlightSends int64               `json:"in_flight_sends"`
	Drained       bool                `json:"drained"`
	AddonAck      *MaintenanceAckView `json:"addon_ack"`
	// Withdraw is the resting-entry withdraw a hold asked for (--withdraw-
	// entries): which rows are still pending the broker's confirmation and
	// which are confirmed. null when no hold asks for one (W-EXEC-TRUTH W0 (f)).
	Withdraw *WithdrawView `json:"withdraw"`
	// InstallPending (release #22): an update job is in a pre-hold state and
	// NEW entries are frozen until it ends or times out. Always present.
	InstallPending InstallPendingView `json:"install_pending"`
}

// MaintenanceStatus reads the installation's maintenance state. loaded is the
// TraderManager's set (the wire is read through its first NT8 TCP trader).
func MaintenanceStatus(loaded map[string]*AutoTrader) MaintenanceStatusView {
	v := MaintenanceStatusView{State: "unconfigured"}
	if st, configured := maintenanceState(); configured {
		switch {
		case !st.Held:
			v.State = "clear"
		case st.Corrupt:
			v.Held, v.State, v.Reason = true, "unreadable", maintenanceReason(st)
		default:
			job, since := st.Hold.JobID, st.Hold.Since
			v.Held, v.State, v.JobID, v.Since, v.Reason = true, "held", &job, &since, maintenanceReason(st)
		}
	}
	v.Withdraw = maintenanceWithdrawView(storeOf(loaded))
	v.InstallPending = InstallPendingStatus()
	v.InFlightSends = MaintenanceInFlight()
	v.Drained = MaintenanceDrained()
	if w, ok := installationWireView(ntTradersOf(loaded)); ok && w.Connected && w.HasAck && w.Rec.Ack != nil {
		a := w.Rec.Ack
		v.AddonAck = &MaintenanceAckView{
			Received: time.Now().Add(-w.AckAge).UTC().Format(time.RFC3339Nano), AgeMs: w.AckAge.Milliseconds(),
			Held: a.Held, JobID: a.JobID, QueuedCommands: a.QueuedCommands, BuildID: a.BuildID, AcceptSeq: w.Rec.AcceptSeq,
		}
	}
	return v
}

// MaintenanceBootLine is the 🔒 boot line, every field READ:
// 🔒 maintenance: hold=<clear|held|unreadable|unconfigured> job=<id|n/a>
// since=<since|n/a> addon_ack=<held|released> job=<id> build=<id>|n/a
func MaintenanceBootLine(loaded map[string]*AutoTrader) string {
	v := MaintenanceStatus(loaded)
	job, since, ack := "n/a", "n/a", "n/a"
	if v.JobID != nil {
		job = *v.JobID
	}
	if v.Since != nil {
		since = *v.Since
	}
	if a := v.AddonAck; a != nil {
		state := "released"
		if a.Held {
			state = "held"
		}
		ack = fmt.Sprintf("%s job=%s build=%s", state, orNA(a.JobID), orNA(a.BuildID))
	}
	return fmt.Sprintf("🔒 maintenance: hold=%s job=%s since=%s addon_ack=%s", v.State, job, since, ack)
}

// ntTradersOf returns the NT8 TCP traders of a loaded set, by id.
func ntTradersOf(loaded map[string]*AutoTrader) []*AutoTrader {
	ids := make([]string, 0, len(loaded))
	for id := range loaded {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []*AutoTrader
	for _, id := range ids {
		if at := loaded[id]; at != nil {
			if _, ok := at.trader.(*ntTrader.TCPTrader); ok {
				out = append(out, at)
			}
		}
	}
	return out
}

// orNA prints a value the AddOn did not send as n/a (L7), never as empty.
func orNA(s string) string {
	if s == "" {
		return "n/a"
	}
	return s
}

// storeOf returns the store of any loaded trader (they share one database).
func storeOf(loaded map[string]*AutoTrader) *store.Store {
	for _, at := range loaded {
		if at != nil && at.store != nil {
			return at.store
		}
	}
	return nil
}
