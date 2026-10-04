package store

import "fmt"

// ── P&L-TRUTH WAVE (2026-09-01) — corrected-column law (A22) on every
// prompt-facing surface. ─────────────────────────────────────────────────────
//
// The model must never read a fabricated track record. Every aggregator that
// sums, averages or ranks P&L reads pnl_corrected ONLY; a NULL row is
// UNRESOLVED — counted, excluded, never coerced to realized_pnl. The raw
// column is legitimately read in exactly two places: when WRITING
// pnl_corrected (the correction / per-close guard) and by the correction
// tooling itself. The build-time guard (pnl_surface_guard_test.go) scans the
// source for any other aggregation over the raw column and fails the build.

// PnLSurface names one strict-corrected aggregator (the registry the boot line
// counts and the guard test verifies exists).
type PnLSurface struct {
	Name string
	File string
}

// PnLSurfaces is the registry of strict-corrected P&L aggregators.
func PnLSurfaces() []PnLSurface {
	return []PnLSurface{
		{Name: "GetPositionStats", File: "store/position_query.go"},
		{Name: "CountConsecutiveLossesSince", File: "store/position_query.go"},
		{Name: "GetSessionDayActivity", File: "store/position_query.go"},
		{Name: "MentorDayActivity", File: "store/position_query.go"},
		{Name: "GetLedgerDayTotal", File: "store/pnl_surface_guard.go"},
		{Name: "GetFullStats", File: "store/position_query.go"},
		{Name: "GetRecentTrades", File: "store/position_query.go"},
		{Name: "GetSymbolStats", File: "store/position_query.go"},
		{Name: "GetHoldingTimeStats", File: "store/position_query.go"},
		{Name: "GetDirectionStats", File: "store/position_query.go"},
		{Name: "GetHistorySummary", File: "store/position_history.go"},
		{Name: "toolGetTradeHistory", File: "agent/tools.go"},
		{Name: "attachTradeContext", File: "trader/auto_trader_loop.go"},
	}
}

// PnLSurfacesBootLine is the boot-block guard line. "0 raw" is a build-time
// guarantee (the guard test fails the build on any raw aggregation), stated
// at boot so the running binary's contract is verifiable from the log alone.
func PnLSurfacesBootLine() string {
	return fmt.Sprintf("P&L surfaces: %d aggregators strict-corrected, 0 raw (corrected-column guard; unresolved rows counted + excluded, never coerced)", len(PnLSurfaces()))
}

// LedgerDayTotal is the dashboard-header figure: the SAME rule as the
// position-history footer (web/src/components/trader/PositionHistory.tsx
// computeDayTotal): rows CLOSED inside the window, close_reason not
// unresolved / reconcile_flat (incl. duplicates — those are reconcile_flat
// rows) / test-seam, pnl_corrected present. Unresolved rows in the window are
// counted and excluded.
type LedgerDayTotal struct {
	Total      float64 `json:"ledger_day_pnl"`
	Resolved   int     `json:"ledger_day_resolved"`
	Unresolved int     `json:"ledger_day_unresolved"`
}

// GetLedgerDayTotal computes the ledger day total for [startMs, endMs) (epoch
// ms, the caller picks the CT calendar day), optionally scoped to one NT
// account. Strict pnl_corrected; no raw read.
func (s *PositionStore) GetLedgerDayTotal(traderID, account string, startMs, endMs int64) (LedgerDayTotal, error) {
	var out LedgerDayTotal
	var rows []TraderPosition
	q := s.db.Where("trader_id = ? AND status = ? AND exit_time >= ? AND exit_time < ? AND close_reason NOT IN (?, ?, ?)",
		traderID, "CLOSED", startMs, endMs, CloseReasonReconcileFlat, CloseReasonUnresolved, CloseReasonTestSeam)
	if account != "" {
		q = q.Where("account = ?", account)
	}
	if err := q.Find(&rows).Error; err != nil {
		return out, fmt.Errorf("ledger day total: %w", err)
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
	out.Total = float64(int64(out.Total*100+0.5*sign(out.Total))) / 100 // 2dp, like the footer
	return out, nil
}

func sign(x float64) float64 {
	if x < 0 {
		return -1
	}
	return 1
}
