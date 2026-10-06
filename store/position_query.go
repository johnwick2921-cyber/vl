package store

import (
	"fmt"
	"math"
	"strings"
)

// TraderStats trading statistics metrics.
//
// P&L-TRUTH WAVE (2026-09-01, corrected-column law A22): every figure here is
// computed over RESOLVED rows only (pnl_corrected NOT NULL). TotalTrades IS the
// resolved count; UnresolvedExcluded is how many closed rows were left out
// because their P&L is unknown — returned alongside so no surface can show a
// total without its exclusion count.
type TraderStats struct {
	TotalTrades        int     `json:"total_trades"`        // == ResolvedTrades (compat)
	ResolvedTrades     int     `json:"resolved_trades"`     // rows with pnl_corrected present
	UnresolvedExcluded int     `json:"unresolved_excluded"` // rows with pnl_corrected NULL — excluded, never coerced
	WinTrades          int     `json:"win_trades"`
	LossTrades         int     `json:"loss_trades"`
	WinRate            float64 `json:"win_rate"`
	ProfitFactor       float64 `json:"profit_factor"`
	SharpeRatio        float64 `json:"sharpe_ratio"`
	TotalPnL           float64 `json:"total_pnl"`
	TotalFee           float64 `json:"total_fee"`
	AvgWin             float64 `json:"avg_win"`
	AvgLoss            float64 `json:"avg_loss"`
	MaxDrawdownPct     float64 `json:"max_drawdown_pct"`
}

// GetPositionStats gets position statistics
func (s *PositionStore) GetPositionStats(traderID string) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	type result struct {
		Total int
		Wins  int
		// 0A-2 (2026-08-31): explicit column tags — GORM's default snake-casing
		// mapped TotalPnL→"total_pn_l" and silently scanned 0 from the
		// "total_pnl" alias (the surface under-reported P&L as zero).
		TotalPnL float64 `gorm:"column:total_pnl"`
		TotalFee float64 `gorm:"column:total_fee"`
	}
	var r result

	err := s.db.Model(&TraderPosition{}).
		// P&L-TRUTH WAVE: strict pnl_corrected — the raw column is never read
		// here (the WHERE already excludes NULL; the old COALESCE fallback was a
		// dead-but-dangerous reference).
		Select("COUNT(*) as total, SUM(CASE WHEN pnl_corrected > 0 THEN 1 ELSE 0 END) as wins, COALESCE(SUM(pnl_corrected), 0) as total_pnl, COALESCE(SUM(fee), 0) as total_fee").
		// A-2 (2026-08-28, bar-truth wave): legacy CLOSED rows with
		// pnl_corrected NULL (317 sync + 37 reconcile_flat — never
		// re-verified against stored prices) are EXCLUDED from every
		// ruled-from aggregate: no silent NULLs in any table we rule
		// from. The excluded count is surfaced for visibility.
		// 0A-2 (2026-08-31): unknown-P&L + e7 test-seam reasons excluded too.
		Where("trader_id = ? AND status = ? AND pnl_corrected IS NOT NULL AND close_reason NOT IN (?, ?, ?)", traderID, "CLOSED", CloseReasonReconcileFlat, CloseReasonUnresolved, CloseReasonTestSeam).
		Scan(&r).Error
	if err != nil {
		return nil, err
	}

	// A-2 — the excluded class, surfaced (never silently folded in).
	var excluded int64
	if err := s.db.Model(&TraderPosition{}).
		Where("trader_id = ? AND status = ? AND pnl_corrected IS NULL", traderID, "CLOSED").
		Count(&excluded).Error; err == nil {
		stats["excluded_null_pnl"] = excluded
	}

	stats["total_trades"] = r.Total
	stats["resolved_trades"] = r.Total
	stats["win_trades"] = r.Wins
	stats["total_pnl"] = r.TotalPnL
	stats["total_fee"] = r.TotalFee
	if r.Total > 0 {
		stats["win_rate"] = float64(r.Wins) / float64(r.Total) * 100
	} else {
		stats["win_rate"] = 0.0
	}

	return stats, nil
}

// CountConsecutiveLossesSince returns the number of consecutive LOSING closed
// trades at the TAIL of this trader's closed-trade history since sinceMs (the CME
// session-day start, Unix ms UTC). A winning or break-even close (realized_pnl >= 0)
// ends the streak; reconcile_flat orphans (unknown P&L) are excluded. Used by the
// D1 consecutive-loss halt. Scoped by trader_id (each trader's closes are recorded
// under its own id).
func (s *PositionStore) CountConsecutiveLossesSince(traderID string, sinceMs int64) (int, error) {
	var rows []TraderPosition
	err := s.db.
		// A-2 (2026-08-28): exclude legacy unverified rows from the loss
		// streak too (reconcile_flat + class-27 unresolved + e7 test-seam).
		Where("trader_id = ? AND status = ? AND close_reason <> ? AND exit_time >= ?",
			traderID, "CLOSED", CloseReasonTestSeam, sinceMs).
		Order("exit_time DESC").
		Find(&rows).Error
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		// D2 (2026-09-09) — AN UNKNOWN CLOSE ENDS THE RUN; IT DOES NOT BRIDGE IT.
		//
		// The WHERE used to drop unresolved and legacy-unverified rows, with a
		// comment saying they were "never counted either way". Excluding a row
		// from the scan does not make it neutral — it makes it TRANSPARENT: a
		// run of 3 losses, an unresolvable close, then 3 more was counted as
		// SIX, because the unknown row was not there to break it.
		//
		// Transparent is the DESTRUCTIVE direction. Blocking new entries is
		// this counter's harmful branch, and bridging lengthens the run, so an
		// unknown close made a halt MORE likely — exactly what A24 forbids.
		// "We do not know whether that trade won" honestly ends a run of KNOWN
		// losses. The e7 test-seam stays excluded in the WHERE: a synthetic row
		// is not a trade that happened, so it neither counts nor breaks.
		if r.CloseReason == CloseReasonReconcileFlat || r.CloseReason == CloseReasonUnresolved {
			break
		}
		pnl, resolved := r.CorrectedPnL()
		if !resolved {
			break
		}
		if pnl < 0 {
			n++
		} else {
			break // a win / break-even ends the losing streak
		}
	}
	return n, nil
}

// GetSessionDayActivity returns the realized P&L (CLOSED trades, excluding
// reconcile-flat orphans whose P&L is UNKNOWN) and the entry count (positions
// OPENED) since sinceMs (Unix ms UTC), optionally scoped to one NT account.
// Used by the Strategy Studio daily guardrails: daily loss/profit on realized
// P&L, max-daily-trades on entries — both measured from the CME session-day
// start. Variadic account mirrors GetClosedPositions/GetFullStats.
func (s *PositionStore) GetSessionDayActivity(traderID string, sinceMs int64, account ...string) (realizedPnL float64, entries int, err error) {
	acct := ""
	if len(account) > 0 {
		acct = account[0]
	}

	var pnl struct{ Total float64 }
	pq := s.db.Model(&TraderPosition{}).
		Select("COALESCE(SUM(pnl_corrected), 0) as total"). /* P&L-TRUTH WAVE: strict corrected column; NULL rows are excluded by the WHERE and never coerced */
		// A-2 (2026-08-28): no silent NULLs — legacy unverified rows are
		// excluded from the guardrail P&L too.
		Where("trader_id = ? AND status = ? AND close_reason NOT IN (?, ?, ?) AND pnl_corrected IS NOT NULL AND exit_time >= ?",
			traderID, "CLOSED", CloseReasonReconcileFlat, CloseReasonUnresolved, CloseReasonTestSeam, sinceMs)
	if acct != "" {
		pq = pq.Where("account = ?", acct)
	}
	if err = pq.Scan(&pnl).Error; err != nil {
		return 0, 0, err
	}

	var cnt int64
	cq := s.db.Model(&TraderPosition{}).
		Where("trader_id = ? AND entry_time >= ?", traderID, sinceMs)
	if acct != "" {
		cq = cq.Where("account = ?", acct)
	}
	if err = cq.Count(&cnt).Error; err != nil {
		return 0, 0, err
	}

	return pnl.Total, int(cnt), nil
}

// MentorDayActivity is the B1 done-after-win seam read: one query over the
// trader's CLOSED trades since sinceMs (the CME session-day start, 17:00 CT),
// optionally scoped to one NT account (variadic, mirrors GetSessionDayActivity).
// DayNetPnl sums pnl_corrected ONLY (corrected-column law, A22); a NULL row
// increments Unresolved and is excluded from the sum — the caller fails CLOSED
// when Unresolved > 0. ClosedInProfit is "a trade closed in profit today".
type MentorDayActivity struct {
	DayNetPnl      float64
	ClosedInProfit bool
	ClosedInLoss   bool
	Unresolved     int
}

// MentorDayActivity returns the day's net P&L, whether any closed trade won,
// and the unresolved (NULL pnl_corrected) count since sinceMs. Strict
// corrected-column: the raw realized_pnl column is never read here.
func (s *PositionStore) MentorDayActivity(traderID string, sinceMs int64, account ...string) (MentorDayActivity, error) {
	acct := ""
	if len(account) > 0 {
		acct = account[0]
	}
	var out MentorDayActivity
	q := s.db.Where("trader_id = ? AND status = ? AND exit_time >= ? AND close_reason NOT IN (?, ?, ?)",
		traderID, "CLOSED", sinceMs, CloseReasonReconcileFlat, CloseReasonUnresolved, CloseReasonTestSeam)
	if acct != "" {
		q = q.Where("account = ?", acct)
	}
	var rows []TraderPosition
	if err := q.Find(&rows).Error; err != nil {
		return out, fmt.Errorf("mentor day activity: %w", err)
	}
	for _, r := range rows {
		pnl, resolved := r.CorrectedPnL()
		if !resolved {
			out.Unresolved++
			continue
		}
		out.DayNetPnl += pnl
		if pnl > 0 {
			out.ClosedInProfit = true
		}
		if pnl < 0 {
			out.ClosedInLoss = true
		}
	}
	return out, nil
}

// GetFullStats gets complete trading statistics, optionally scoped to one
// account (mirrors GetClosedPositions): account=="" → trader-global (crypto +
// legacy); account!="" → only that NT account's closed trades, excluding
// pre-migration rows (account=”). Variadic so existing callers are unchanged.
func (s *PositionStore) GetFullStats(traderID string, account ...string) (*TraderStats, error) {
	stats := &TraderStats{}
	acct := ""
	if len(account) > 0 {
		acct = account[0]
	}

	// Exclude unknown-P&L orphan closes: their realized P&L is UNKNOWN (exit
	// fill never captured), not a real $0 — counting them would skew win-rate /
	// total P&L. They still appear in the position LIST (rendered "—").
	// P&L-TRUTH WAVE (2026-09-01, corrected-column law): the unresolved class
	// (pnl_corrected NULL) is COUNTED here and EXCLUDED from every figure
	// below. Before this wave the rows were loaded and summed through
	// EffectivePnL(), which coerced NULL → raw realized_pnl: 115 such rows put
	// "Total PnL -203.68 (220 trades)" in front of the executor when the truth
	// was +304.32 over 105 resolved (row 526 alone: raw −1,458.00 vs corrected
	// −69.43). The model must never read a fabricated track record.
	var unresolved int64
	uq := s.db.Model(&TraderPosition{}).Where("trader_id = ? AND status = ? AND close_reason NOT IN (?, ?, ?) AND pnl_corrected IS NULL", traderID, "CLOSED", CloseReasonReconcileFlat, CloseReasonUnresolved, CloseReasonTestSeam)
	if acct != "" {
		uq = uq.Where("account = ?", acct)
	}
	if err := uq.Count(&unresolved).Error; err != nil {
		return nil, err
	}
	stats.UnresolvedExcluded = int(unresolved)

	var positions []TraderPosition
	pq := s.db.Where("trader_id = ? AND status = ? AND close_reason NOT IN (?, ?, ?) AND pnl_corrected IS NOT NULL", traderID, "CLOSED", CloseReasonReconcileFlat, CloseReasonUnresolved, CloseReasonTestSeam)
	if acct != "" {
		pq = pq.Where("account = ?", acct)
	}
	err := pq.Order("exit_time ASC").
		Find(&positions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query position statistics: %w", err)
	}
	if len(positions) == 0 {
		return stats, nil
	}

	var pnls []float64
	var totalWin, totalLoss float64

	for _, pos := range positions {
		pnl, resolved := pos.CorrectedPnL()
		if !resolved {
			stats.UnresolvedExcluded++ // defensive: the WHERE excludes these
			continue
		}
		stats.TotalTrades++
		stats.ResolvedTrades++
		stats.TotalPnL += pnl
		stats.TotalFee += pos.Fee
		pnls = append(pnls, pnl)

		if pnl > 0 {
			stats.WinTrades++
			totalWin += pnl
		} else if pnl < 0 {
			stats.LossTrades++
			totalLoss += -pnl
		}
	}

	if stats.TotalTrades > 0 {
		stats.WinRate = float64(stats.WinTrades) / float64(stats.TotalTrades) * 100
	}
	if totalLoss > 0 {
		stats.ProfitFactor = totalWin / totalLoss
	}
	if stats.WinTrades > 0 {
		stats.AvgWin = totalWin / float64(stats.WinTrades)
	}
	if stats.LossTrades > 0 {
		stats.AvgLoss = totalLoss / float64(stats.LossTrades)
	}
	if len(pnls) > 1 {
		stats.SharpeRatio = calculateSharpeRatioFromPnls(pnls)
	}
	if len(pnls) > 0 {
		stats.MaxDrawdownPct = calculateMaxDrawdownFromPnls(pnls)
	}

	return stats, nil
}

// RecentTrade recent trade record.
//
// P&L-TRUTH WAVE: Resolved=false marks a row whose P&L is UNKNOWN
// (pnl_corrected NULL). Such a row carries NO P&L and NO percentage — the old
// code rendered an unresolved short with exit 0 as "+0.00 (+100.00%)".
// RealizedPnL (json name kept for API compatibility) holds the CORRECTED
// value for resolved rows and 0 for unresolved ones.
type RecentTrade struct {
	ID           int64   `json:"id"`
	Resolved     bool    `json:"resolved"`
	Symbol       string  `json:"symbol"`
	Side         string  `json:"side"`
	EntryPrice   float64 `json:"entry_price"`
	ExitPrice    float64 `json:"exit_price"`
	RealizedPnL  float64 `json:"realized_pnl"`
	PnLPct       float64 `json:"pnl_pct"`
	EntryTime    int64   `json:"entry_time"`
	ExitTime     int64   `json:"exit_time"`
	HoldDuration string  `json:"hold_duration"`
}

// GetRecentTrades gets recent closed trades, optionally scoped to one account
// (mirrors GetClosedPositions): account=="" → trader-global (crypto + legacy);
// account!="" → only that NT account's trades, excluding pre-migration rows
// (account=”). Variadic so existing callers stay trader-global unchanged.
func (s *PositionStore) GetRecentTrades(traderID string, limit int, account ...string) ([]RecentTrade, error) {
	var positions []TraderPosition
	// P&L-TRUTH WAVE: test-seam rows are quarantined from every strategy-P&L
	// surface (0A-2) — including the model's recent-trade list. Unresolved /
	// reconcile_flat rows STAY listed, rendered as UNRESOLVED, never as $0.
	q := s.db.Where("trader_id = ? AND status = ? AND close_reason <> ?", traderID, "CLOSED", CloseReasonTestSeam)
	if len(account) > 0 && account[0] != "" {
		q = q.Where("account = ?", account[0])
	}
	err := q.Order("exit_time DESC").
		Limit(limit).
		Find(&positions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query recent trades: %w", err)
	}

	var trades []RecentTrade
	for _, pos := range positions {
		pnl, resolved := pos.CorrectedPnL()
		t := RecentTrade{
			ID:          pos.ID,
			Resolved:    resolved,
			Symbol:      pos.Symbol,
			Side:        strings.ToLower(pos.Side),
			EntryPrice:  pos.EntryPrice,
			ExitPrice:   pos.ExitPrice,
			RealizedPnL: pnl,                  // 0 when unresolved — and Resolved=false says so
			EntryTime:   pos.EntryTime / 1000, // Convert ms to seconds for API compatibility
		}

		if pos.ExitTime > 0 {
			t.ExitTime = pos.ExitTime / 1000 // Convert ms to seconds
			durationMs := pos.ExitTime - pos.EntryTime
			t.HoldDuration = formatDurationMs(durationMs)
		}

		// A percentage is only meaningful for a RESOLVED row with a real exit:
		// exit 0 on an unresolved short used to render as "+100.00%".
		if resolved && pos.EntryPrice > 0 && pos.ExitPrice > 0 {
			if t.Side == "long" {
				t.PnLPct = (pos.ExitPrice - pos.EntryPrice) / pos.EntryPrice * 100 * float64(pos.Leverage)
			} else {
				t.PnLPct = (pos.EntryPrice - pos.ExitPrice) / pos.EntryPrice * 100 * float64(pos.Leverage)
			}
		}

		trades = append(trades, t)
	}

	return trades, nil
}

// calculateSharpeRatioFromPnls calculates Sharpe ratio
func calculateSharpeRatioFromPnls(pnls []float64) float64 {
	if len(pnls) < 2 {
		return 0
	}

	var sum float64
	for _, pnl := range pnls {
		sum += pnl
	}
	mean := sum / float64(len(pnls))

	var variance float64
	for _, pnl := range pnls {
		variance += (pnl - mean) * (pnl - mean)
	}
	stdDev := math.Sqrt(variance / float64(len(pnls)-1))

	if stdDev == 0 {
		return 0
	}

	return mean / stdDev
}

// calculateMaxDrawdownFromPnls calculates maximum drawdown
func calculateMaxDrawdownFromPnls(pnls []float64) float64 {
	if len(pnls) == 0 {
		return 0
	}

	const startingEquity = 10000.0
	equity := startingEquity
	peak := startingEquity
	var maxDD float64

	for _, pnl := range pnls {
		equity += pnl
		if equity > peak {
			peak = equity
		}
		if peak > 0 {
			dd := (peak - equity) / peak * 100
			if dd > maxDD {
				maxDD = dd
			}
		}
	}

	return maxDD
}

// SymbolStats per-symbol trading statistics
type SymbolStats struct {
	Symbol             string  `json:"symbol"`
	TotalTrades        int     `json:"total_trades"`        // resolved rows only
	UnresolvedExcluded int     `json:"unresolved_excluded"` // P&L-TRUTH WAVE
	WinTrades          int     `json:"win_trades"`
	WinRate            float64 `json:"win_rate"`
	TotalPnL           float64 `json:"total_pnl"`
	AvgPnL             float64 `json:"avg_pnl"`
	AvgHoldMins        float64 `json:"avg_hold_mins"`
}

// GetSymbolStats gets per-symbol trading statistics
func (s *PositionStore) GetSymbolStats(traderID string, limit int) ([]SymbolStats, error) {
	var positions []TraderPosition
	// Exclude unknown-P&L orphan closes (reconcile_flat / class-27 unresolved / e7 test-seam).
	err := s.db.Where("trader_id = ? AND status = ? AND close_reason NOT IN (?, ?, ?)", traderID, "CLOSED", CloseReasonReconcileFlat, CloseReasonUnresolved, CloseReasonTestSeam).Find(&positions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query symbol stats: %w", err)
	}

	// Group by symbol
	symbolMap := make(map[string]*SymbolStats)
	symbolHoldMins := make(map[string][]float64)

	for _, pos := range positions {
		if _, ok := symbolMap[pos.Symbol]; !ok {
			symbolMap[pos.Symbol] = &SymbolStats{Symbol: pos.Symbol}
			symbolHoldMins[pos.Symbol] = []float64{}
		}
		s := symbolMap[pos.Symbol]
		pnl, resolved := pos.CorrectedPnL()
		if !resolved {
			s.UnresolvedExcluded++ // P&L-TRUTH WAVE: counted, never coerced
			continue
		}
		s.TotalTrades++
		s.TotalPnL += pnl
		if pnl > 0 {
			s.WinTrades++
		}

		if pos.ExitTime > 0 {
			holdMins := float64(pos.ExitTime-pos.EntryTime) / 60000.0 // ms to minutes
			symbolHoldMins[pos.Symbol] = append(symbolHoldMins[pos.Symbol], holdMins)
		}
	}

	var stats []SymbolStats
	for symbol, s := range symbolMap {
		if s.TotalTrades > 0 {
			s.WinRate = float64(s.WinTrades) / float64(s.TotalTrades) * 100
			s.AvgPnL = s.TotalPnL / float64(s.TotalTrades)
		}
		if len(symbolHoldMins[symbol]) > 0 {
			var totalMins float64
			for _, m := range symbolHoldMins[symbol] {
				totalMins += m
			}
			s.AvgHoldMins = totalMins / float64(len(symbolHoldMins[symbol]))
		}
		stats = append(stats, *s)
	}

	// Sort by TotalPnL descending and limit
	for i := 0; i < len(stats)-1; i++ {
		for j := i + 1; j < len(stats); j++ {
			if stats[j].TotalPnL > stats[i].TotalPnL {
				stats[i], stats[j] = stats[j], stats[i]
			}
		}
	}

	if limit > 0 && len(stats) > limit {
		stats = stats[:limit]
	}

	return stats, nil
}

// HoldingTimeStats holding duration analysis
type HoldingTimeStats struct {
	Range              string  `json:"range"`
	TradeCount         int     `json:"trade_count"`         // resolved rows only
	UnresolvedExcluded int     `json:"unresolved_excluded"` // P&L-TRUTH WAVE
	WinRate            float64 `json:"win_rate"`
	AvgPnL             float64 `json:"avg_pnl"`
}

// GetHoldingTimeStats analyzes performance by holding duration
func (s *PositionStore) GetHoldingTimeStats(traderID string) ([]HoldingTimeStats, error) {
	var positions []TraderPosition
	// P&L-TRUTH WAVE: same ledger exclusions as every other aggregator
	// (unknown-P&L / test-seam reasons out); unresolved rows counted below.
	err := s.db.Where("trader_id = ? AND status = ? AND exit_time > 0 AND close_reason NOT IN (?, ?, ?)", traderID, "CLOSED", CloseReasonReconcileFlat, CloseReasonUnresolved, CloseReasonTestSeam).Find(&positions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query holding time stats: %w", err)
	}

	rangeStats := map[string]*struct {
		count      int
		unresolved int
		wins       int
		totalPnL   float64
	}{
		"<1h":   {},
		"1-4h":  {},
		"4-24h": {},
		">24h":  {},
	}

	for _, pos := range positions {
		if pos.ExitTime == 0 {
			continue
		}
		holdHours := float64(pos.ExitTime-pos.EntryTime) / 3600000.0 // ms to hours

		var rangeKey string
		switch {
		case holdHours < 1:
			rangeKey = "<1h"
		case holdHours < 4:
			rangeKey = "1-4h"
		case holdHours < 24:
			rangeKey = "4-24h"
		default:
			rangeKey = ">24h"
		}

		r := rangeStats[rangeKey]
		pnl, resolved := pos.CorrectedPnL()
		if !resolved {
			r.unresolved++ // P&L-TRUTH WAVE: counted, never coerced
			continue
		}
		r.count++
		r.totalPnL += pnl
		if pnl > 0 {
			r.wins++
		}
	}

	var stats []HoldingTimeStats
	for _, rangeKey := range []string{"<1h", "1-4h", "4-24h", ">24h"} {
		r := rangeStats[rangeKey]
		if r.count > 0 {
			stats = append(stats, HoldingTimeStats{
				Range:              rangeKey,
				TradeCount:         r.count,
				UnresolvedExcluded: r.unresolved,
				WinRate:            float64(r.wins) / float64(r.count) * 100,
				AvgPnL:             r.totalPnL / float64(r.count),
			})
		}
	}

	return stats, nil
}

// DirectionStats long/short performance comparison
type DirectionStats struct {
	Side               string  `json:"side"`
	TradeCount         int     `json:"trade_count"`
	WinRate            float64 `json:"win_rate"`
	TotalPnL           float64 `json:"total_pnl"`
	AvgPnL             float64 `json:"avg_pnl"`
	UnresolvedExcluded int     `json:"unresolved_excluded"` // P&L-TRUTH WAVE: counted, never coerced
}

// GetDirectionStats analyzes long vs short performance
func (s *PositionStore) GetDirectionStats(traderID string) ([]DirectionStats, error) {
	var positions []TraderPosition
	// Exclude unknown-P&L orphan closes (reconcile_flat / class-27 unresolved / e7 test-seam).
	err := s.db.Where("trader_id = ? AND status = ? AND close_reason NOT IN (?, ?, ?)", traderID, "CLOSED", CloseReasonReconcileFlat, CloseReasonUnresolved, CloseReasonTestSeam).Find(&positions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query direction stats: %w", err)
	}

	sideStats := make(map[string]*DirectionStats)
	for _, pos := range positions {
		if _, ok := sideStats[pos.Side]; !ok {
			sideStats[pos.Side] = &DirectionStats{Side: pos.Side}
		}
		s := sideStats[pos.Side]
		pnl, resolved := pos.CorrectedPnL()
		if !resolved {
			s.UnresolvedExcluded++ // P&L-TRUTH WAVE: counted, never coerced
			continue
		}
		s.TradeCount++
		s.TotalPnL += pnl
		if pnl > 0 {
			s.WinRate++
		}
	}

	var stats []DirectionStats
	for _, s := range sideStats {
		if s.TradeCount > 0 {
			s.AvgPnL = s.TotalPnL / float64(s.TradeCount)
			s.WinRate = s.WinRate / float64(s.TradeCount) * 100
		}
		stats = append(stats, *s)
	}

	return stats, nil
}
