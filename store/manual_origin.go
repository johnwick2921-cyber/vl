package store

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"vl/internal/installpath"
)

// ── R22 PNL-TRUTH — MANUAL NT8 TRADES ARE NOT THE BOT'S RECORD ───────────────
//
// OWNER RULING R1 (2026-10-14, "hoang's record shows −$4,226; remove"): four
// MANUAL NT8 shorts (#608, #609, #637, #640 — Σ −3,202.00) were adopted into
// hoang's ledger by the reconcile loop and made up 75.8% of a −4,226.18
// track record whose bot-placed part is −1,024.18 over 164 trades. They are
// removed from the bot record by EXCLUSION. History is never deleted: the rows
// stay in trader_positions and in the history list, carry origin='manual', and
// every bot P&L surface shows them as a separate, counted line.
//
// ONE RULE (canon 28). Before this wave each reader had its own idea of
// "foreign": deskRealizedToday dropped plan_id='UNRESOLVABLE' rows WITHOUT
// counting them, every other reader kept them. The origin column is the one
// fact; BotOriginSQL is the one predicate; every aggregator applies it.

const (
	// OriginBot — the bot placed this position (the column default).
	OriginBot = "bot"
	// OriginManual — an NT8 position the bot ADOPTED with no originating
	// order/arm of its own (reconcile "manual/NT8-side" verdict).
	OriginManual = "manual"
)

// BotOriginSQL is THE predicate every bot P&L aggregator applies. NULL or an
// empty value reads as bot: a row written before the column existed was written by the
// bot's own paths (the reconcile adoptions are marked by the migration below).
const BotOriginSQL = "COALESCE(origin, '') <> 'manual'"

// ManualOriginSQL is its complement — the rows the separate line counts.
const ManualOriginSQL = "COALESCE(origin, '') = 'manual'"

// IsManualOrigin is the Go twin of ManualOriginSQL, for per-row readers.
func IsManualOrigin(origin string) bool {
	return strings.EqualFold(strings.TrimSpace(origin), OriginManual)
}

// ManualExcluded is the separate line: the manual rows a bot figure left out.
// Counts, never rates; the ids make every figure reproducible (sample-id law).
type ManualExcluded struct {
	Trades     int     `json:"manual_excluded_trades"`     // CLOSED manual rows in scope
	PnL        float64 `json:"manual_excluded_pnl"`        // Σ pnl_corrected over the RESOLVED ones
	Unresolved int     `json:"manual_excluded_unresolved"` // manual rows with NULL pnl_corrected (not summed)
	IDs        []int64 `json:"manual_excluded_ids"`
}

// Line renders the owner-visible separate line, e.g.
// "Manual (excluded): 4 trades, −$3,202.00". An empty set says so in words.
func (m ManualExcluded) Line() string {
	if m.Trades == 0 {
		return "Manual (excluded): none"
	}
	sign := "+"
	if m.PnL < 0 {
		sign = "−"
	}
	s := fmt.Sprintf("Manual (excluded): %d trade%s, %s$%s", m.Trades, plural(m.Trades), sign, money(math.Abs(m.PnL)))
	if m.Unresolved > 0 {
		s += fmt.Sprintf(" (%d unresolved, not summed)", m.Unresolved)
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// money formats 3202 as "3,202.00".
func money(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String() + frac
}

// ManualExcludedSince counts the CLOSED manual rows of one trader that exited
// at or after sinceMs (0 = all time), optionally scoped to one NT account.
// Test-seam rows are not trades and are never counted here either.
func (s *PositionStore) ManualExcludedSince(traderID, account string, sinceMs int64) (ManualExcluded, error) {
	var out ManualExcluded
	q := s.db.Where("trader_id = ? AND status = ? AND close_reason <> ? AND exit_time >= ?",
		traderID, "CLOSED", CloseReasonTestSeam, sinceMs).Where(ManualOriginSQL)
	if account != "" {
		q = q.Where("account = ?", account)
	}
	var rows []TraderPosition
	if err := q.Order("id ASC").Find(&rows).Error; err != nil {
		return out, fmt.Errorf("manual excluded: %w", err)
	}
	for _, r := range rows {
		out.Trades++
		out.IDs = append(out.IDs, r.ID)
		if UnknownPnLReason(r.CloseReason) {
			out.Unresolved++
			continue
		}
		pnl, resolved := r.CorrectedPnL()
		if !resolved {
			out.Unresolved++
			continue
		}
		out.PnL += pnl
	}
	out.PnL = math.Round(out.PnL*100) / 100
	return out, nil
}

// BotDayPnL is the bot-only realized P&L for one CME session day.
type BotDayPnL struct {
	SinceMs    int64          `json:"since_ms"`
	Total      float64        `json:"bot_realized_pnl"`
	Resolved   int            `json:"bot_resolved"`
	Unresolved int            `json:"bot_unresolved"` // NULL pnl_corrected — excluded, counted (A22)
	Manual     ManualExcluded `json:"manual"`
}

// BotRealizedPnL is THE helper for any gate or surface that needs "what did the
// BOT realize this session day": CLOSED rows that exited at or after
// sessionDayStartMs (the caller passes kernel.CMESessionDayStart(now) — 17:00
// CT), unknown-P&L and test-seam reasons out, strict pnl_corrected, manual rows
// EXCLUDED and returned as their own counted line. account "" = trader-wide.
func (s *PositionStore) BotRealizedPnL(traderID string, sessionDayStartMs int64, account ...string) (BotDayPnL, error) {
	out := BotDayPnL{SinceMs: sessionDayStartMs}
	acct := ""
	if len(account) > 0 {
		acct = account[0]
	}
	q := s.db.Where("trader_id = ? AND status = ? AND exit_time >= ? AND close_reason NOT IN (?, ?, ?)",
		traderID, "CLOSED", sessionDayStartMs, CloseReasonReconcileFlat, CloseReasonUnresolved, CloseReasonTestSeam).
		Where(BotOriginSQL)
	if acct != "" {
		q = q.Where("account = ?", acct)
	}
	var rows []TraderPosition
	if err := q.Find(&rows).Error; err != nil {
		return out, fmt.Errorf("bot realized pnl: %w", err)
	}
	for _, r := range rows {
		pnl, resolved := r.CorrectedPnL()
		if !resolved {
			out.Unresolved++
			continue
		}
		out.Resolved++
		out.Total += pnl
	}
	out.Total = math.Round(out.Total*100) / 100
	m, err := s.ManualExcludedSince(traderID, acct, sessionDayStartMs)
	if err != nil {
		return out, err
	}
	out.Manual = m
	return out, nil
}

// MarkManualOrigin flags ONE adopted position as manual. WHERE-scoped by id AND
// trader id, and idempotent: a row already manual is left alone and reports
// false. It never touches P&L, close_reason or plan_id.
func (s *PositionStore) MarkManualOrigin(id int64, traderID string) (bool, error) {
	tx := s.db.Model(&TraderPosition{}).
		Where("id = ? AND trader_id = ?", id, traderID).Where(BotOriginSQL).
		Update("origin", OriginManual)
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected == 1, nil
}

// ── THE BOOT MIGRATION (R1) ───────────────────────────────────────────────────

// ManualOriginTarget is one row the owner ruled manual, with the evidence it
// must still carry for the migration to touch it.
type ManualOriginTarget struct {
	ID       int64
	TraderID string
}

// R1ManualOriginTargets — EXACTLY the four rows of owner ruling R1, each pinned
// to hoang's trader id. Verified read-only 2026-10-14 (data.db): all four are
// source='reconcile', plan_id='UNRESOLVABLE', an empty entry_order_id, 40/40/20/20
// contracts, each with a "MATERIALIZED untracked NT8 position … manual/NT8-side
// entry" log line and no armed_orders row of 20+ contracts ever filled.
var R1ManualOriginTargets = []ManualOriginTarget{
	{ID: 608, TraderID: "8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265"},
	{ID: 609, TraderID: "8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265"},
	{ID: 637, TraderID: "8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265"},
	{ID: 640, TraderID: "8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265"},
}

// manualOriginEvidenceSQL is the adoption fingerprint a target must still
// carry. A row that does not look like a reconcile adoption is NEVER marked —
// it is counted as skipped (fail-closed: hiding a real bot trade from the bot
// record and the bot-only risk reads is the harmful direction).
const manualOriginEvidenceSQL = "source = 'reconcile' AND plan_id = 'UNRESOLVABLE' AND COALESCE(entry_order_id, '') = ''"

// ManualOriginCounts is what one migration run measured. Counts, never rates.
type ManualOriginCounts struct {
	Pending   int     // targets that would be marked now
	Marked    int     // marked by this run
	Already   int     // already manual (idempotent re-run)
	Skipped   int     // absent, another trader's id, or no adoption fingerprint
	MarkedIDs []int64 // the ids this run marked
}

// classifyManualOriginTargets reads, never writes.
func (s *PositionStore) classifyManualOriginTargets() (pending, already, skipped []int64, err error) {
	for _, t := range R1ManualOriginTargets {
		var row TraderPosition
		r := s.db.Where("id = ? AND trader_id = ?", t.ID, t.TraderID).Limit(1).Find(&row)
		if r.Error != nil {
			return nil, nil, nil, r.Error
		}
		if r.RowsAffected == 0 {
			skipped = append(skipped, t.ID)
			continue
		}
		if IsManualOrigin(row.Origin) {
			already = append(already, t.ID)
			continue
		}
		var n int64
		if e := s.db.Model(&TraderPosition{}).
			Where("id = ? AND trader_id = ?", t.ID, t.TraderID).Where(manualOriginEvidenceSQL).
			Count(&n).Error; e != nil {
			return nil, nil, nil, e
		}
		if n != 1 {
			skipped = append(skipped, t.ID)
			continue
		}
		pending = append(pending, t.ID)
	}
	return pending, already, skipped, nil
}

// PendingManualOriginWork reports what the migration WOULD do, without writing.
func (s *PositionStore) PendingManualOriginWork() (ManualOriginCounts, error) {
	p, a, sk, err := s.classifyManualOriginTargets()
	return ManualOriginCounts{Pending: len(p), Already: len(a), Skipped: len(sk)}, err
}

// RunManualOriginMigration marks EXACTLY the R1 targets manual. Every UPDATE is
// WHERE-scoped by id AND trader id AND the adoption fingerprint AND "not yet
// manual", so a second run marks nothing. Nothing is deleted; P&L, close
// reason and plan id are untouched.
func (s *PositionStore) RunManualOriginMigration() (ManualOriginCounts, error) {
	pending, already, skipped, err := s.classifyManualOriginTargets()
	c := ManualOriginCounts{Pending: len(pending), Already: len(already), Skipped: len(skipped)}
	if err != nil {
		return c, err
	}
	for _, t := range R1ManualOriginTargets {
		tx := s.db.Model(&TraderPosition{}).
			Where("id = ? AND trader_id = ?", t.ID, t.TraderID).
			Where(manualOriginEvidenceSQL).Where(BotOriginSQL).
			Update("origin", OriginManual)
		if tx.Error != nil {
			return c, fmt.Errorf("mark #%d manual: %w", t.ID, tx.Error)
		}
		if tx.RowsAffected == 1 {
			c.Marked++
			c.MarkedIDs = append(c.MarkedIDs, t.ID)
		}
	}
	return c, nil
}

// BackupBeforeManualOrigin copies the DB before the R1 write. No backup, no
// write — the contract BackupBeforeWaveA / BackupBeforeRegrade enforce.
func BackupBeforeManualOrigin(dbPath, stamp string) (string, error) {
	root := installpath.BackupRoot()
	if root == "" {
		return "", fmt.Errorf("resolve home for the backup dir")
	}
	dir := filepath.Join(root, "r22-manual-origin")
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

// ManualOriginBootLine is the boot line. Every figure is READ from the table
// (the marked set across all traders) plus what this boot's run measured.
func (s *PositionStore) ManualOriginBootLine(ran ManualOriginCounts, backup string) string {
	var n int64
	var sum struct{ Total *float64 }
	s.db.Model(&TraderPosition{}).Where("status = ?", "CLOSED").Where(ManualOriginSQL).Count(&n)
	s.db.Model(&TraderPosition{}).Select("SUM(pnl_corrected) AS total").
		Where("status = ? AND pnl_corrected IS NOT NULL", "CLOSED").Where(ManualOriginSQL).Scan(&sum)
	total := "n/a"
	if sum.Total != nil {
		total = fmt.Sprintf("%+.2f", *sum.Total)
	}
	b := "none"
	if backup != "" {
		b = filepath.Base(backup)
	}
	return fmt.Sprintf("manual origin (R1): closed manual rows=%d Σ=%s (excluded from every bot P&L figure, shown as their own line) · this boot marked=%d %v already=%d skipped=%d · backup=%s",
		n, total, ran.Marked, ran.MarkedIDs, ran.Already, ran.Skipped, b)
}
