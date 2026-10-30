package trader

import (
	"time"

	"vl/kernel"
	"vl/store"
	"vl/telemetry"
)

// W6 — in-app alert emit (the audit's dead wire: AlertStore.Emit had zero
// production callers). emitAlert is the single gated + deduped producer. P0 =
// pop-up + banner-until-ack · P1 = feed · P2 = digest. Gated on day_plan (the
// alert center only renders for a futures day-plan trader) EXCEPT for ops kinds
// (W26, 2026-10-15): a trader that is DOWN, or whose feed/link/disk/backup/
// updater/bar-hole guard tripped, must always reach the owner — the alert
// center is the only surface a down trader has. Plan-content alerts keep the
// day-plan gate.
func (at *AutoTrader) emitAlert(level, kind, eventID, title, body string) {
	if at.store == nil {
		return
	}
	if !at.dayPlanEnabled() && !isOpsAlertKind(kind) {
		return
	}
	row := &store.AlertDB{
		TraderID:  at.id,
		Level:     level,
		Kind:      kind,
		EventID:   eventID, // dedupe (idempotent bus) — repeats are silent no-ops
		Title:     title,
		Body:      body,
		CreatedAt: time.Now().Unix(),
	}
	created, err := at.store.Alert().Emit(row)
	if err == nil && created {
		// m432: a NEW ops P0 also reaches the owner's phone (outbound, async,
		// deduped, rate-limited; never blocks this caller).
		at.maybeOwnerPush(row, ownerPushClock())
	}
	if err != nil {
		// P0-cleanup — an alert that vanishes on DB write failure is a silent
		// hole in the one owner-facing surface.
		at.logErrorf("🚨 alert EMIT failed (%s %s): %v — body: %s", level, kind, err, body)
		telemetry.RecordError(at.id, "alert_emit_failed", err.Error(), telemetry.CostNone)
	}
}

// isOpsAlertKind reports whether an alert kind is an OPS condition (down, link,
// dead-man, disk, backup, updater, bar hole) that must reach the owner even
// when the trader has no day plan. Plan-content alerts (plan-died, fill, close,
// level-*, overlays-*, mentor-*, decision-*, …) keep the day-plan gate. The
// disk / backup / updater / bar-hole kinds do not exist yet — add them here
// when their producer lands.
func isOpsAlertKind(kind string) bool {
	switch kind {
	case "trader_down_no_evaluator", // the run-owner "trader is DOWN" P0
		"feed-down",                // the NT8 link/feed down P0
		alertKindBarHoleUnfilled,   // m432: NT8 has no history for a bar hole — reload it
		"dead_man_reconcile_stuck", // DS-102 #538: dead-man reconcile block with no upper bound
		// m226: protection alerts are the only signal for a position whose stop is
		// missing, partial or stuck — they must reach the desk even when the
		// trader runs without a day plan (every mentor trader).
		"unprotected_position",
		"protection_partial",
		"protection_pending_stuck",
		"protection_over_covered",
		"pep_mode_conflict", // PEP MODE: a trader refused at start for an illegal mode combination
		"pep_flat_failed",   // PEP MODE: a pep position is still open after repeated flat attempts
		alertKindStopMoveNotApplied,
		alertKindCloseRejected: // m442 #19: NT8 refused an exit - the position is still open
		return true
	}
	return false
}

// ackedAlertPruneDays is how old an ACKED P2 alert must be before the daily
// prune hides it from the feed (the row survives — audit, not amnesia).
const ackedAlertPruneDays = 7

// maybePruneAckedAlerts runs the alert-feed prune ONCE per CME session-day.
// PruneAckedOlderThan has existed since the alert center shipped and had ZERO
// production callers (B-audit 2026-08-18): the feed grew without bound. P0/P1
// are never auto-pruned.
func (at *AutoTrader) maybePruneAckedAlerts(now time.Time) {
	if !at.dayPlanEnabled() || at.store == nil {
		return
	}
	day := kernel.CMESessionDayKey(now)
	if at.lastAlertPruneDay == day {
		return
	}
	at.lastAlertPruneDay = day
	cutoff := now.AddDate(0, 0, -ackedAlertPruneDays).Unix()
	n, err := at.store.Alert().PruneAckedOlderThan(at.id, cutoff, now.Unix())
	if err != nil {
		at.logErrorf("🧹 alert prune failed: %v", err)
		return
	}
	if n > 0 {
		at.logInfof("🧹 alert prune: hid %d acked P2 alert(s) older than %d days", n, ackedAlertPruneDays)
	}
}
