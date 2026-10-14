package store

import (
	"fmt"
	"strings"
)

// PrevOpenTimeOn returns the open time of the newest stored bar for
// (symbol, tf, contract) strictly BEFORE beforeMs, with the same reader filter
// as BarsBetweenOn (mixed and replay:off-scale rows are never read). ok=false
// when the store holds no such bar — an absent neighbour is not a zero.
//
// GAP-BACKFILL (2026-10-14): the bar-hole detector asks this once per
// symbol×contract per process — "what was the last minute the store held
// before this live bar?" — so a hole that opened before a restart is still
// seen. One indexed MAX over the bars key; it never scans the table.
func (s *BarHistoryStore) PrevOpenTimeOn(symbol, tf, contract string, beforeMs int64) (int64, bool, error) {
	if s == nil || s.db == nil {
		return 0, false, fmt.Errorf("store required")
	}
	var out struct{ M *int64 }
	q := s.db.Model(&BarHistoryDB{}).Select("MAX(open_time_ms) AS m").
		Where("symbol = ? AND tf = ? AND open_time_ms < ? AND COALESCE(source, '') NOT IN (?, ?)",
			symbol, tf, beforeMs, BarSourceMixed, BarSourceOffScale)
	if c := strings.TrimSpace(contract); c != "" {
		q = q.Where("contract = ?", c)
	}
	if err := q.Scan(&out).Error; err != nil {
		return 0, false, err
	}
	if out.M == nil {
		return 0, false, nil
	}
	return *out.M, true, nil
}
