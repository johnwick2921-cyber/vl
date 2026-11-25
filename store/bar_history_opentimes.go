package store

import (
	"fmt"
	"strings"
)

// OpenTimesBetweenOn returns ONLY the open times (epoch ms, ascending) of the
// stored bars for (symbol, tf, contract) in [fromMs, toMs), with the same
// reader filter as BarsBetweenOn (mixed and replay:off-scale rows are never
// read). An empty contract means unfiltered.
//
// GAP AUDIT (m444, 2026-10-29): the persistent bar-gap audit reads a rolling
// 7-session window for every live symbol every ~10 minutes. It needs the
// minute stamps, not the OHLCV rows, so this is one indexed range read that
// selects a single column — about 10k int64s per symbol for 7 sessions of 1m —
// instead of materialising 10k structs through gorm.
func (s *BarHistoryStore) OpenTimesBetweenOn(symbol, tf, contract string, fromMs, toMs int64) ([]int64, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store required")
	}
	var out []int64
	q := s.db.Model(&BarHistoryDB{}).
		Where("symbol = ? AND tf = ? AND open_time_ms >= ? AND open_time_ms < ? AND COALESCE(source, '') NOT IN (?, ?)",
			symbol, tf, fromMs, toMs, BarSourceMixed, BarSourceOffScale)
	if c := strings.TrimSpace(contract); c != "" {
		q = q.Where("contract = ?", c)
	}
	if err := q.Order("open_time_ms").Pluck("open_time_ms", &out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
