// ── 101 D2 — DISPLAY-ONLY readers for the chart across contract rolls ────────
//
// Owner ruling 2026-09-16: the chart MUST show stored history across rolls.
// These readers exist for the klines API and nothing else. The contract-scoped
// readers the kernel, the level map and the arm path use — LastNBarsOn,
// BarsBetweenOn — are untouched (A31: display truth changes; decision truth
// does not; trader/fade_no_refusal_test.go's cousin for bars is E4).
//
// RESEARCH LAW: nothing here adjusts a price. A prior contract's bars are that
// contract's bars, labelled with its name; the roll is a visible step in the
// series, never smoothed (Sep/Dec basis ≈292 pts on 2026-09-15).
package store

import (
	"sort"
	"strings"
)

// FirstLiveOn returns the open time of the first LIVE row this contract holds
// for (symbol, tf). That is the roll boundary the chart uses: sparse
// historical_import rows that precede a contract's live trading do not move it
// (MNQ 12-26 carries one daily import row from 09-07; its live series begins
// 09-14 15:34 CT).
func (s *BarHistoryStore) FirstLiveOn(symbol, tf, contract string) (openMs int64, ok bool, err error) {
	if s == nil || s.db == nil || strings.TrimSpace(contract) == "" {
		return 0, false, nil
	}
	var row BarHistoryDB
	res := s.db.Where("symbol = ? AND tf = ? AND contract = ? AND source = ?", symbol, tf, contract, BarSourceLive).
		Order("open_time_ms ASC").Limit(1).Find(&row)
	if res.Error != nil {
		return 0, false, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, false, nil
	}
	return row.OpenTimeMs, true, nil
}

// priorSourceRank orders rows at the SAME open time when more than one contract
// or source holds one: a live bar beats a replay beats an import. Among equal
// sources the newer contract wins (lexical on "MNQ 12-26" > "MNQ 09-26" within
// a year; across a year boundary the caller's boundary already bounds it).
func priorSourceRank(src string) int {
	switch src {
	case BarSourceLive:
		return 0
	case BarSourceHistorical:
		return 1
	case BarSourceHistoricalImport:
		return 2
	default:
		return 3
	}
}

// PriorContractBarsBefore returns up to n rows strictly older than beforeMs,
// from any contract EXCEPT current, ascending by time, ONE row per open time
// (best source, then newest contract). Display only. Every row keeps its own
// Contract and Source so the chart can label the step and a reader can tell
// replay from live from import.
//
// Why current is excluded: beforeMs is the current contract's first LIVE row,
// and anything the current contract holds before that is an import (the
// 09-07..09-14 12-26 file) at a time when the FRONT month was the prior
// contract. The bars PK has no contract, so such an import can only sit in a
// slot the prior series does not hold — a hole — and admitting it would draw
// one bar of the next contract's price space in the middle of the prior
// series (the fixture's first cut placed them on occupied slots, the PK
// skipped them, and this reader was never asked the question; nofx-93,
// 2026-09-16). A hole in the prior series stays a hole.
func (s *BarHistoryStore) PriorContractBarsBefore(symbol, tf, current string, beforeMs int64, n int) ([]BarHistoryDB, error) {
	if s == nil || s.db == nil || n <= 0 || beforeMs <= 0 {
		return nil, nil
	}
	// Over-fetch to survive per-timestamp dedupe, bounded.
	fetch := n * 3
	if fetch > 60_000 {
		fetch = 60_000
	}
	var rows []BarHistoryDB
	if err := s.db.Where("symbol = ? AND tf = ? AND open_time_ms < ? AND contract IS NOT NULL AND contract <> ? AND contract <> ?",
		symbol, tf, beforeMs, ContractMixed, current).
		Order("open_time_ms DESC").Limit(fetch).Find(&rows).Error; err != nil {
		return nil, err
	}
	best := map[int64]BarHistoryDB{}
	for _, r := range rows {
		cur, seen := best[r.OpenTimeMs]
		if !seen {
			best[r.OpenTimeMs] = r
			continue
		}
		ra, rb := priorSourceRank(r.Source), priorSourceRank(cur.Source)
		if ra < rb || (ra == rb && r.Contract > cur.Contract) {
			best[r.OpenTimeMs] = r
		}
	}
	out := make([]BarHistoryDB, 0, len(best))
	for _, r := range best {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenTimeMs < out[j].OpenTimeMs })
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}
