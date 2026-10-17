package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"vl/internal/envcompat"
	"vl/logger"
)

// ADHERENCE REGRADE (owner ruling 2026-09-03) — flag-guarded migration for the
// rows that carry FULL lineage and still hold the off-plan D they were graded
// with before that lineage arrived.
//
// Why they exist: the fill frame lands before the position row materializes, so
// the grade is computed with no citation (base D). The reconcile stamps the
// lineage minutes later. Nothing regrades — RepairArmedLineage cleared the
// grade only when it read "F", and an uncited close reaches F only under a
// penalty, so clean rows kept a D that is now provably wrong.
//
// PROVABLY wrong, not merely suspicious: a cited close whose direction matched
// grades base A, and the two penalties (InNoTrade, !InKillzone) step down at
// most twice, reaching C. Base A cannot reach D. So plan_matched=1 with a real
// scenario and grade D can ONLY mean the row was graded while Cited was false.
//
// The migration CLEARS the grade; it never writes one. The W5 analytics then
// regrade the close with the lineage in hand, which is the same mechanism
// RepairArmedLineage uses — no second grader, no invented verdict.

// stuckAdherenceWhere is the ONE predicate both the scan and the write use.
//
// Every clause earns its place, and three rows in the live data prove it:
//
//	plan_version > 0 AND cited_scenario_id <> ''  — lineage actually present
//	cited_scenario_id <> 'off-plan'               — 530 cites the literal
//	                                                sentinel; its D is correct
//	source <> 'e7_farside_test'                   — 572 is an ARMED_TEST_SEAM
//	                                                artifact, not a trade
//	plan_matched = 1                              — 582 has matched=0, so its
//	                                                base is C and D is honest
//	plan_band NOT IN ('off_band','struct')        — those bases are B, and
//	                                                B − 2 penalties IS D
//	adherence_grade = 'D'                         — the impossible letter
//
// Drop any one of them and the migration promotes a row that earned its grade.
const stuckAdherenceWhere = `status = 'CLOSED'
	AND plan_version > 0
	AND cited_scenario_id <> ''
	AND cited_scenario_id <> 'off-plan'
	AND source <> 'e7_farside_test'
	AND plan_matched = 1
	AND plan_band NOT IN ('off_band','struct')
	AND adherence_grade = 'D'`

// AdherenceRegradeEnabled reports whether the migration is armed. Default OFF:
// this rewrites published grades, so it runs only when the operator says so.
func AdherenceRegradeEnabled() bool {
	v := strings.TrimSpace(os.Getenv("ADHERENCE_REGRADE"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// StuckAdherenceRows lists the ids the migration would clear. Read-only, so the
// count can be quoted before anything is written.
func (s *PositionStore) StuckAdherenceRows() ([]int64, error) {
	var ids []int64
	err := s.db.Model(&TraderPosition{}).Where(stuckAdherenceWhere).
		Order("id").Pluck("id", &ids).Error
	return ids, err
}

// RegradeStuckAdherence clears the grade on every qualifying row so the W5
// analytics recompute it. Idempotent: a cleared row no longer matches.
func (s *PositionStore) RegradeStuckAdherence() (int, error) {
	ids, err := s.StuckAdherenceRows()
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	res := s.db.Model(&TraderPosition{}).Where("id IN ?", ids).
		Update("adherence_grade", "")
	if res.Error != nil {
		return 0, fmt.Errorf("adherence regrade: %w", res.Error)
	}
	logger.Infof("🩹 adherence regrade: cleared %d stuck off-plan D grade(s) for recompute — ids %v", res.RowsAffected, ids)
	return int(res.RowsAffected), nil
}

// AdherenceDistribution is the before/after the report quotes.
func (s *PositionStore) AdherenceDistribution() (map[string]int, error) {
	type row struct {
		Grade string
		N     int
	}
	var rows []row
	err := s.db.Model(&TraderPosition{}).
		Select("adherence_grade as grade, count(*) as n").
		Where("status = 'CLOSED' AND adherence_grade <> ''").
		Group("adherence_grade").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, r := range rows {
		out[r.Grade] = r.N
	}
	return out, nil
}

// BackupBeforeRegrade takes an online sqlite3 backup to
// ~/vl-backups/adherence-regrade/<stamp>.db before the migration writes.
//
// The guarded-write protocol requires a backup first, and this write rewrites
// published grades. It uses the same online .backup mechanism the C1 timer
// uses, so it is safe against a live process. A failure here ABORTS the
// migration: no backup, no write.
func BackupBeforeRegrade(dbPath, stamp string) (string, error) {
	root := envcompat.BackupRoot() // R5 removes: vl-backups wins, nofx-backups only when ~/nofx exists
	if root == "" {
		return "", fmt.Errorf("resolve home for the backup dir")
	}
	dir := filepath.Join(root, "adherence-regrade")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	dst := filepath.Join(dir, stamp+".db")
	if _, err := os.Stat(dst); err == nil {
		return dst, nil // already taken for this stamp — idempotent
	}
	cmd := exec.Command("sqlite3", dbPath, fmt.Sprintf(".backup '%s'", dst))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("sqlite3 backup: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return dst, nil
}

// AdherenceRegradeBootLine is the boot block's line. Every number READ.
//
// It reports what is PENDING when the flag is off, so the count is visible
// without arming anything — and what was CLEARED when it ran.
func AdherenceRegradeBootLine(pending, regraded int, enabled bool, backup string) string {
	if !enabled {
		return fmt.Sprintf("adherence regraded=0 (ADHERENCE_REGRADE off) · %d row(s) pending: full lineage still holding an off-plan D", pending)
	}
	b := backup
	if b == "" {
		b = "none"
	}
	return fmt.Sprintf("adherence regraded=%d (flag on, backup %s) — grades CLEARED for recompute, never written; a genuinely uncited close keeps its D", regraded, b)
}
