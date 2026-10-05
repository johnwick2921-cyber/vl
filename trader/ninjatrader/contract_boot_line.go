package ninjatrader

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"vl/kernel"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ContractBootLine is the ROLL WAVE's boot line (D6). Every field is READ
// (A11): the contract from the AddOn's ACK with its receipt time, the census
// from the store, the filtered-out count from the rehydrate that just ran, the
// roll from the server's record. A field the process cannot know prints n/a.
//
// Pure so a fixture can pin the wording without a server.
func ContractBootLine(symbol string, fact ntwire.ContractFact, haveFact bool, census map[string]int64,
	readerBarsKept, readerBarsFiltered int, ringReseeded bool) string {
	cur := "n/a (no subscription ACK received; nothing stamped in the store)"
	if haveFact {
		when := "boot"
		if !fact.ReceivedAt.IsZero() {
			when = kernel.ClockCTSeconds(fact.ReceivedAt)
		}
		cur = fmt.Sprintf("%s (source=%s@%s)", fact.Contract, fact.Source, when)
	}
	// Census, stable order: named contracts sorted, then spans-roll, then null.
	var names []string
	for k := range census {
		if k != "" && k != store.ContractMixed {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names)+2)
	for _, k := range names {
		parts = append(parts, fmt.Sprintf("%s=%d", strings.TrimPrefix(k, symbol+" "), census[k]))
	}
	parts = append(parts, fmt.Sprintf("spans-roll=%d", census[store.ContractMixed]))
	parts = append(parts, fmt.Sprintf("null=%d", census[""]))
	lastRoll := "none"
	if haveFact && fact.Previous != "" {
		lastRoll = fmt.Sprintf("%s→%s @%s", fact.Previous, fact.Contract, kernel.ClockCTSeconds(fact.RolledAt))
	}
	reseeded := "no"
	if ringReseeded {
		reseeded = "yes"
	}
	return fmt.Sprintf("📜 contract: current=%s · bars by contract: %s · readers filtered=%d/%d (kept/read) · ring reseeded=%s · last roll=%s",
		cur, strings.Join(parts, " "), readerBarsKept, readerBarsKept+readerBarsFiltered, reseeded, lastRoll)
}

// SourceBootLine is the BAR-SOURCE WAVE's boot line: which feed wrote the
// store's rows, and whether this process has caught the replay on a different
// scale from live. Every field read; the threshold is stated as [I].
func SourceBootLine(symbol string, census map[string]int64, mismatches []ntwire.ScaleMismatch, pct float64, hold string) string {
	mm := "none"
	if len(mismatches) > 0 {
		parts := make([]string, 0, len(mismatches))
		for _, m := range mismatches {
			if m.Symbol != symbol {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s@%s Δ=%.2f (replay %.2f vs live %.2f, %d dropped)",
				m.Timeframe, kernel.ClockCTSeconds(m.At), m.DeltaPts, m.LastHistoricalC, m.FirstLiveC, m.HistoricalDropped))
		}
		if len(parts) > 0 {
			mm = strings.Join(parts, "; ")
		}
	}
	if hold == "" {
		hold = "replay-hold: n/a"
	}
	return fmt.Sprintf("📼 bar source: %s live=%d historical=%d mixed=%d off-scale=%d null=%d · replay-never-overwrites-live=on · unverified-replay-held=on · %s · scale-mismatch threshold=%.2f%%[env:%s] AND %.0fx[env:%s] median body [I] · mismatches this process: %s",
		symbol, census[store.BarSourceLive], census[store.BarSourceHistorical], census[store.BarSourceMixed], census[store.BarSourceOffScale], census[""], hold, pct*100, ntwire.ScaleMismatchPctSource(), ntwire.ScaleMismatchRangeMult, ntwire.ScaleMismatchRangeMultSource(), mm)
}

// contractBootLineFor assembles the line from live sources. Called after the
// boot rehydrate and again after a roll reseed.
func contractBootLineFor(bh *store.BarHistoryStore, server *ntwire.TCPServer, symbol string, kept, filtered int, reseeded bool) string {
	var fact ntwire.ContractFact
	have := false
	if server != nil {
		fact, have = server.CurrentContract(symbol)
	}
	if !have && bh != nil {
		if c, ok := bh.LatestContract(symbol); ok {
			fact, have = ntwire.ContractFact{Contract: c, Source: "store-fallback"}, true
		}
	}
	census := map[string]int64{}
	if bh != nil {
		if c, err := bh.ContractCensus(symbol); err == nil {
			census = c
		}
	}
	_ = time.Now
	return ContractBootLine(symbol, fact, have, census, kept, filtered, reseeded)
}
