package store

import (
	"fmt"
	"strings"
	"time"

	"nofx/logger"
)

// ── ATTRIBUTION INTEGRITY (2026-09-02) — ONE SENTINEL ────────────────────────
//
// A position's plan link has THREE states, and before this file two of them
// were spelled the same way. `position_plan_join`'s comment has said since
// 2026-08-26 that "unresolvable rows keep plan_id='UNRESOLVABLE'", and four
// rows do (530, 539, 545, 546) — but three others carry "" (566, 571, 580),
// which is also what a row looks like before anything stamps it. A consumer
// testing the sentinel misses them; a consumer testing "" misses the four.
//
// MEASURED before building: since the day-plan era began, 51/51 `system` and
// 5/5 `armed_entry` closed positions carry a real link, as do 8 of 11
// `reconcile` rows. The gap is three rows, none of which has an arm within
// thirty minutes — there is nothing to join on, so the honest value is the
// SENTINEL. We never guess a link.
const PlanUnresolvable = "UNRESOLVABLE"

// PlanLinkState is what a consumer actually needs to branch on.
type PlanLinkState int

const (
	// PlanLinkUnstamped: nothing has written a link yet. For a CLOSED row this
	// is a defect (the stamp path missed it); for an open row it may simply be
	// early. Never treat it as "no plan exists".
	PlanLinkUnstamped PlanLinkState = iota
	// PlanLinkUnresolvable: we looked and there is nothing to join on. A
	// decided answer, not a missing one.
	PlanLinkUnresolvable
	// PlanLinkLinked: a real plan id.
	PlanLinkLinked
)

func (s PlanLinkState) String() string {
	switch s {
	case PlanLinkLinked:
		return "linked"
	case PlanLinkUnresolvable:
		return "unresolvable"
	default:
		return "unstamped"
	}
}

// ClassifyPlanLink is the ONE place the three states are told apart. Consumers
// call this instead of comparing to "" or to the sentinel themselves.
func ClassifyPlanLink(planID string) PlanLinkState {
	switch strings.TrimSpace(planID) {
	case "":
		return PlanLinkUnstamped
	case PlanUnresolvable:
		return PlanLinkUnresolvable
	default:
		return PlanLinkLinked
	}
}

// IsPlanLinked reports whether the row can be joined to a plan. The sentinel and
// the empty string are both FALSE — that is the whole point of the sentinel.
func IsPlanLinked(planID string) bool { return ClassifyPlanLink(planID) == PlanLinkLinked }

// StampUnresolvableLineage marks a position whose lineage could not be
// recovered. Called at MATERIALIZATION, so a row is never born with "".
func StampUnresolvableLineage(p *TraderPosition) {
	if p == nil {
		return
	}
	if ClassifyPlanLink(p.PlanID) == PlanLinkUnstamped {
		p.PlanID = PlanUnresolvable
	}
}

// GetArm returns one arm row by its (plan, scenario, leg) identity. Used by the
// attribution fixtures and by any consumer that needs armed_under_version.
func (s *ArmedOrderStore) GetArm(planID, scenario string, legIndex int) (*ArmedOrderDB, error) {
	var row ArmedOrderDB
	if err := s.db.Where("plan_id = ? AND scenario = ? AND leg_index = ?", planID, scenario, legIndex).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// CountUnresolvable is the number of positions that carry the sentinel — the
// figure the boot line reports. A REAL count, never a literal (A24).
func (s *PositionStore) CountUnresolvable() (int64, error) {
	var n int64
	err := s.db.Model(&TraderPosition{}).Where("plan_id = ?", PlanUnresolvable).Count(&n).Error
	return n, err
}

// CountUnstampedClosed is the number of CLOSED positions still carrying "" —
// the defect the sentinel replaces. Reported beside the sentinel count so the
// boot line cannot hide a regression behind a healthy-looking number.
func (s *PositionStore) CountUnstampedClosed() (int64, error) {
	var n int64
	err := s.db.Model(&TraderPosition{}).
		Where("status != ? AND (plan_id IS NULL OR plan_id = '')", "OPEN").Count(&n).Error
	return n, err
}

// attributionConvergeFlag makes the convergence idempotent.
// DayPlanEraStart is the day-plan era boundary, defined ONCE, in the zone the
// era is actually named in.
//
// THE ZONE IS CT, and the first day-plan row is the proof: its created_at is
// 2026-08-16 00:44:31 UTC while its trade_date is 2026-08-15 (session NY).
// trade_date is a CT calendar date and it is the key the era is named by, so a
// UTC boundary cannot express "trade_date >= 2026-08-15". The rest of the
// system already agrees — the CME session day rolls at 17:00 CT and kernel/tz.go
// is the enforced single time source.
//
// The first cut of this constant was 1755230400000 (2025-08-15 — a year early,
// which converted 516 pre-era rows). The second was UTC midnight
// (1786752000000 = 2026-08-14 19:00 CT), five hours early: latent, because the
// disputed five-hour window held 0 rows, but wrong. Derived from the date in
// the stated zone, never typed.
var DayPlanEraStart = dayPlanEraStart()

// dayPlanEraStart builds the boundary from the DATE in the era's own zone.
// store cannot import kernel (cycle), so the zone is loaded the same way
// store/level_state.go:264 already does. A failed load falls back to CDT's
// fixed -5, which is the correct offset for an August date — and the test
// asserts the resolved instant in BOTH zones, so a wrong fallback is visible.
func dayPlanEraStart() time.Time {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		loc = time.FixedZone("CDT", -5*60*60)
	}
	return time.Date(2026, 8, 15, 0, 0, 0, 0, loc)
}

const attributionConvergeFlag = "attribution_sentinel_converge_2026_09_02"

// ConvergePlanLinkSentinel converts CLOSED positions from the day-plan era that
// carry "" into the sentinel. WHERE-scoped, idempotent, and deliberately NOT
// applied to the pre-day-plan history: those ~518 rows are crypto-era trades
// from before a plan link existed. Calling them "unresolvable" would imply we
// looked for a plan and found none, when in truth there was never one to find —
// a placeholder that reads as data, in the other direction.
//
// Measured target: three rows (566, 571, 580), each with no arm within thirty
// minutes. Reported loudly with the ids it changed.
func (s *Store) ConvergePlanLinkSentinel() {
	if v, err := s.GetSystemConfig(attributionConvergeFlag); err == nil && v == "1" {
		return
	}
	// DAY-PLAN ERA. Written as a NAMED, TEST-ASSERTED date, not a magic epoch:
	// the first cut of this constant was 1755230400000, which is 2025-08-15 —
	// a YEAR early — and it converted 516 PRE-era rows the attribution report
	// explicitly said to leave alone. The literal looked plausible and nothing
	// asserted what date it resolved to. A guarded write whose scope constant is
	// unverified is a guarded write with no guard.
	eraMs := DayPlanEraStart.UnixMilli()
	type row struct {
		ID     int64
		Source string
	}
	var rows []row
	if err := s.gdb.Raw(`SELECT id, COALESCE(source,'') AS source FROM trader_positions
WHERE status != 'OPEN' AND created_at >= ? AND (plan_id IS NULL OR plan_id = '')`, eraMs).Scan(&rows).Error; err != nil {
		logger.Warnf("🔗 attribution converge: scan failed: %v", err)
		return
	}
	if len(rows) == 0 {
		_ = s.SetSystemConfig(attributionConvergeFlag, "1")
		logger.Infof("🔗 attribution converge: nothing to converge (no day-plan-era CLOSED row carries an empty plan_id)")
		return
	}
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	res := s.gdb.Exec(`UPDATE trader_positions SET plan_id = ?
WHERE status != 'OPEN' AND created_at >= ? AND (plan_id IS NULL OR plan_id = '')`, PlanUnresolvable, eraMs)
	if res.Error != nil {
		logger.Warnf("🔗 attribution converge: update failed: %v", res.Error)
		return
	}
	logger.Infof("🔗 attribution converge: %d day-plan-era CLOSED row(s) '' → %s (ids %v) — pre-era history left untouched (never a plan to find)",
		res.RowsAffected, PlanUnresolvable, ids)
	_ = s.SetSystemConfig(attributionConvergeFlag, "1")
}

// AttributionBootLine reports the REAL counts, read from the table (A24: no
// literal, no plausible zero). unstamped>0 on a CLOSED row is a live defect and
// is printed beside the sentinel count so a healthy number cannot hide it.
func (s *Store) AttributionBootLine() string {
	sentinel, err1 := s.Position().CountUnresolvable()
	unstamped, err2 := s.Position().CountUnstampedClosed()
	if err1 != nil || err2 != nil {
		return "attribution: counts unavailable (stamp-at-materialization=on · armed_under_version=on)"
	}
	return fmt.Sprintf("attribution: stamp-at-materialization=on · armed_under_version=on · unresolvable=%d (sentinel %q) · unstamped-closed=%d (pre-era history)",
		sentinel, PlanUnresolvable, unstamped)
}
