package trader

import (
	"fmt"
	"time"

	"nofx/kernel"
	"nofx/store"
)

// ITEM 3 (2026-08-17) — THE OWNER'S MANUAL ESCAPE HATCH.
//
// The planner only reads on its own schedule and on death. When a plan is wrong
// but not dead — or the owner has just learned something the plan does not know —
// there was no way to ask for a fresh read short of waiting. This is that button.
//
// It SPENDS one re-plan from the same budget the automatic path uses, so it can
// never be used to talk the bot past its own limits, and the new version is
// written through the NORMAL path with trigger_reason "owner_reread" so the
// history shows who asked.

// RereadRefusal explains why a manual re-read is not available, in the owner's
// language. Empty Reason means it IS available.
type RereadRefusal struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
	Session string `json:"session,omitempty"`
	// Budget as it stands right now, so the confirm step can say what it costs.
	ReplansLeft int `json:"replans_left"`
	ReplanCap   int `json:"replan_cap"`
	Version     int `json:"version"`
}

// CanForceReread reports whether a manual re-read may run right now, and why not
// when it may not. Read-only: it never writes.
func (at *AutoTrader) CanForceReread(now time.Time) RereadRefusal {
	if !at.dayPlanEnabled() || at.store == nil {
		return RereadRefusal{Reason: "the day plan is off for this trader"}
	}
	if _, held := MaintenanceHeld(); held { // W-ONE-BUTTON M2 site 5
		return RereadRefusal{Reason: maintenanceHoldPlanReason}
	}
	reg := at.sessionRegistry(now)
	sess, ok := reg.ActiveSession(now)
	if !ok {
		return RereadRefusal{Reason: "no session is active right now — the planner reads at the session open"}
	}
	if runnable, why := at.sessionRunnable(sess); !runnable {
		reason := why
		if reason == "" {
			reason = fmt.Sprintf("the %s session is not enabled for this strategy", sess.Name)
		}
		return RereadRefusal{Session: sess.Name, Reason: reason}
	}
	if !kernel.IsCMEOpen(now) {
		return RereadRefusal{Session: sess.Name, Reason: "the market is closed (holiday or weekend)"}
	}

	tradeDate := sessionChainDate(sess, now)
	cap := at.replanCapFor(sess.Name)
	row, err := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, sess.Name, at.id)
	if err != nil {
		return RereadRefusal{Session: sess.Name, Reason: "could not read the current plan"}
	}
	if row == nil {
		// No plan yet today: the first read is not a re-plan and costs no budget.
		return RereadRefusal{Allowed: true, Session: sess.Name, ReplanCap: cap, ReplansLeft: cap, Version: 0}
	}
	if store.IsMachinePlan(row) {
		// W-EXEC-TRUTH W5 (CTO 1790194913337) — a MACHINE plan (the Picture
		// no-plan door) is "no plan" here, exactly as for the scheduler: the
		// read is the chain's first AI read — allowed, and it spends nothing
		// (the write site skips the spend when it supersedes a machine plan).
		// Version is the machine row's, so owner edits on it still carry.
		return RereadRefusal{Allowed: true, Session: sess.Name, ReplanCap: cap, ReplansLeft: cap, Version: row.Version}
	}
	// CLASS 35 — the RECORDED budget (same seam as the death gate and the card).
	budget := store.GetReplanBudget(at.store, at.id, tradeDate, sess.Name, cap)
	out := RereadRefusal{
		Session:     sess.Name,
		Version:     row.Version,
		ReplanCap:   cap,
		ReplansLeft: budget.Left(),
	}
	if row.Lifecycle == "no_trade" {
		out.Reason = "this session has already been closed out with a NO-TRADE plan (the owner reset is the escape hatch)"
		return out
	}
	if !budget.May() {
		out.Reason = fmt.Sprintf("the re-read budget for %s is spent (%d of %d used)", sess.Name, budget.Used, cap)
		return out
	}
	out.Allowed = true
	return out
}

// ForceReread runs a planner read for the CURRENT session on the owner's demand.
// It re-checks eligibility itself — the caller's earlier check may be stale — and
// returns the refusal it acted on. Fail-closed: the planner's own write path
// handles a bad read by storing a NO-TRADE plan and alerting, and the previous
// plan is never mutated either way.
func (at *AutoTrader) ForceReread(now time.Time) (RereadRefusal, error) {
	gate := at.CanForceReread(now)
	if !gate.Allowed {
		return gate, fmt.Errorf("%s", gate.Reason)
	}
	// F6 — CLOCK-HOLD (2026-08-30): an owner-requested re-read is also plan
	// authoring — never run it on a clock known broken.
	if deferred, widen, _, _ := at.clockHoldAuthoring(); deferred {
		refusal := RereadRefusal{Allowed: false,
			Reason: fmt.Sprintf("clock-hold: re-read deferred (|drift| %dms > tolerance %dms) — no plan written; fix the host clock / NTP, then re-read again (F6)",
				widen, kernel.C2ToleranceMs())}
		at.logErrorf("🕰 clock-hold: owner re-read DEFERRED — %s", refusal.Reason)
		return refusal, fmt.Errorf("%s", refusal.Reason)
	}
	reg := at.sessionRegistry(now)
	sess, _ := reg.ActiveSession(now)
	tradeDate := sessionChainDate(sess, now)
	// W5 — the chain's latest row decides what this read costs: over a
	// MACHINE plan it is the first AI read (free, like no plan at all).
	latest, lErr := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, gate.Session, at.id)
	overMachine := lErr == nil && store.IsMachinePlan(latest)
	if overMachine {
		at.logInfof("🗓️ OWNER RE-READ requested for %s %s over a machine Picture plan (v%d) — the first AI read of the chain; no re-read spent (%d of %d left).",
			tradeDate, gate.Session, gate.Version, gate.ReplansLeft, gate.ReplanCap)
	} else {
		at.logInfof("🗓️ OWNER RE-READ requested for %s %s (v%d, %d of %d re-reads left) — spending one.",
			tradeDate, gate.Session, gate.Version, gate.ReplansLeft, gate.ReplanCap)
	}
	at.emitAlert("P2", "owner-reread",
		fmt.Sprintf("reread:%s:%s:v%d", tradeDate, gate.Session, gate.Version),
		fmt.Sprintf("%s plan re-read on request", gate.Session),
		fmt.Sprintf("You asked for a fresh read of the %s plan. It spends one of the %d re-reads for this session.",
			gate.Session, gate.ReplanCap))
	// C9/D6 (2026-08-25) — re-verify the budget against the LATEST row right
	// before the read: a death re-plan racing this request could already have
	// spent the last re-plan (the pre-claim CanForceReread TOCTOU).
	// W5 — a machine plan is "no plan": nothing is spent, so nothing to re-verify.
	if lErr == nil && latest != nil && !overMachine {
		if !store.GetReplanBudget(at.store, at.id, tradeDate, gate.Session, gate.ReplanCap).May() {
			return RereadRefusal{Allowed: false, Session: gate.Session, Reason: "the re-read budget was spent by a concurrent re-plan — try the owner reset instead"}, nil
		}
	}
	// C9 (2026-08-25) — capture the claim result: a lost claim (another read
	// already in flight) or a failed preflight must be an HONEST outcome, not a
	// silent success.
	performed := at.runPlannerReadWithTriggerClaimedCtx(gate.Session, tradeDate, "owner_reread", "", nil, true)
	if !performed {
		return RereadRefusal{
			Allowed: true, // the budget gate passed; the read itself was skipped
			Session: gate.Session,
			Reason:  "a planner read for this session was already in flight (or the bar window is stale) — this re-read did not run; retry after it completes",
		}, nil
	}
	// C9 — sticky owner edits survive the owner re-read, exactly like the death
	// and MSS-wake paths.
	if fresh, fErr := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, gate.Session, at.id); fErr == nil && fresh != nil && fresh.Version != gate.Version {
		at.carryOwnerEditsInto(fresh.PlanID, gate.Version, fresh.Version)
	}
	return gate, nil
}
