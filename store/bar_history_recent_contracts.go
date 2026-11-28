package store

import "fmt"

// RecentContracts lists the contracts that hold (symbol, tf) rows at or after
// sinceMs, OLDEST first by their LAST stored bar (the order contracts stopped
// being recorded - the roll order, even when a newer contract carries a sparse
// pre-roll import snapshot that would put it first by FIRST bar). It is the
// time-bounded twin of ContractSpans: an index range scan over the last weeks of
// rows instead of an aggregate over the symbol's whole history, so a per-minute
// caller (the data-truth swing check, release #55) can afford it. FirstMs / N
// cover only the rows at or after sinceMs. Read-only.
func (s *BarHistoryStore) RecentContracts(symbol, tf string, sinceMs int64) ([]ContractSpan, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store required")
	}
	var rows []ContractSpan
	err := s.db.Raw(`SELECT contract, MIN(open_time_ms) AS first_ms, MAX(open_time_ms) AS last_ms, COUNT(*) AS n
		FROM bars WHERE symbol = ? AND tf = ? AND open_time_ms >= ? AND contract != '' AND contract != ? AND COALESCE(source,'') NOT IN (?, ?)
		GROUP BY contract ORDER BY MAX(open_time_ms)`, symbol, tf, sinceMs, ContractMixed, BarSourceMixed, BarSourceOffScale).Scan(&rows).Error
	return rows, err
}
