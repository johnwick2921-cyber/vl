package telemetry

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// B6 — per-trader, per-CME-session-day GATE-BLOCK counters.
//
// The global Prometheus RiskGateTrips counter (metrics.go) answers "how often has
// gate X tripped, ever, process-wide". B6 answers a different operational question:
// "which gates blocked THIS trader TODAY, and how many times each" — a small,
// queryable table exposed over the REST API plus a one-line daily journal summary.
//
// It is intentionally in-memory and ephemeral (a diagnostics tally, not a ledger):
// the table resets at the 17:00 CT CME session-day rollover. Every gate site that
// blocks an entry/cycle calls IncGateBlock alongside its existing Prometheus Inc.
//
// The session-day stamp is passed IN by callers (kernel.CMESessionDayStart(...))
// so this package never imports kernel (kernel imports telemetry — the reverse
// would be an import cycle).

type gateBlockKey struct {
	trader string
	gate   string
}

var (
	gateBlockMu   sync.Mutex
	gateBlockDay  int64 // active session-day (ms at the 17:00 CT rollover); 0 = unset
	gateBlockCnts = map[gateBlockKey]int{}
)

// IncGateBlock records one block of the named gate for the given trader on the
// current session-day.
func IncGateBlock(trader, gate string) {
	if gate == "" {
		return
	}
	gateBlockMu.Lock()
	gateBlockCnts[gateBlockKey{trader: trader, gate: gate}]++
	gateBlockMu.Unlock()
}

// IncTraderEvent records one per-trader EVENT that is NOT a gate refusal (an
// arm authored, a night/day transition, a close whose P&L was unresolved).
// It feeds the Prometheus TraderEventsTotal counter, NEVER the per-session
// gate-block table — a non-block must not read as a block (P2-3a, release #26;
// canon 35: counters record, never infer).
func IncTraderEvent(trader, event string) {
	if event == "" {
		return
	}
	TraderEventsTotal.WithLabelValues(trader, event).Inc()
}

// IncClockSkewObserved records one observed local-vs-feed clock skew event that
// did NOT block an entry (signals are feed-stamped since 2026-08-18). It feeds
// the same per-trader session table under the gate name "clock_skew_observed".
func IncClockSkewObserved(trader string) {
	IncGateBlock(trader, "clock_skew_observed")
}

// RolloverGateBlocks sets the active session-day to sessionDayMs. On the FIRST call
// it just adopts the day (no summary). On a later call whose day DIFFERS from the
// tracked one, it returns a one-line journal summary of the ENDING day's table and
// clears it; same-day calls return "". Call once per cycle from the trader loop:
// the first trader across the midnight/rollover boundary receives the summary to
// log, the rest receive "".
func RolloverGateBlocks(sessionDayMs int64) string {
	gateBlockMu.Lock()
	defer gateBlockMu.Unlock()
	if gateBlockDay == 0 {
		gateBlockDay = sessionDayMs
		return ""
	}
	if sessionDayMs == gateBlockDay {
		return ""
	}
	summary := summarizeGateBlocksLocked()
	gateBlockDay = sessionDayMs
	gateBlockCnts = map[gateBlockKey]int{}
	return summary
}

// GateBlockSnapshot returns the active session-day and the current per-trader/gate
// table (trader -> gate -> count). Safe to call from the API handler goroutine.
func GateBlockSnapshot() (sessionDayMs int64, table map[string]map[string]int) {
	gateBlockMu.Lock()
	defer gateBlockMu.Unlock()
	out := make(map[string]map[string]int, len(gateBlockCnts))
	for k, v := range gateBlockCnts {
		if out[k.trader] == nil {
			out[k.trader] = map[string]int{}
		}
		out[k.trader][k.gate] = v
	}
	return gateBlockDay, out
}

// GateBlockDailySummary renders the current table as a single deterministic line
// (gates sorted, highest count first) — used both by the rollover summary and the
// API. Returns "no gate blocks recorded" when the table is empty.
func GateBlockDailySummary() string {
	gateBlockMu.Lock()
	defer gateBlockMu.Unlock()
	return summarizeGateBlocksLocked()
}

// summarizeGateBlocksLocked builds the one-line summary. Caller holds gateBlockMu.
//
// P2-3b (release #26): the summary is grouped PER TRADER so the line names which
// trader each gate blocked — the old aggregate (gate=n across all traders) lost
// the trader dimension and read as one trader's tally. The empty-string trader
// (the process-wide bucket) renders as "(process)".
func summarizeGateBlocksLocked() string {
	if len(gateBlockCnts) == 0 {
		return "gate-block summary: no gate blocks recorded"
	}
	total := 0
	perTrader := map[string]map[string]int{} // trader → gate → count
	for k, v := range gateBlockCnts {
		if perTrader[k.trader] == nil {
			perTrader[k.trader] = map[string]int{}
		}
		perTrader[k.trader][k.gate] += v
		total += v
	}
	traderNames := make([]string, 0, len(perTrader))
	for t := range perTrader {
		traderNames = append(traderNames, t)
	}
	sort.Strings(traderNames)
	parts := make([]string, 0, len(traderNames))
	for _, t := range traderNames {
		label := t
		if t == "" {
			label = "(process)"
		}
		gates := perTrader[t]
		gateNames := make([]string, 0, len(gates))
		for g := range gates {
			gateNames = append(gateNames, g)
		}
		sort.Slice(gateNames, func(i, j int) bool {
			if gates[gateNames[i]] != gates[gateNames[j]] {
				return gates[gateNames[i]] > gates[gateNames[j]]
			}
			return gateNames[i] < gateNames[j]
		})
		gs := make([]string, 0, len(gateNames))
		for _, g := range gateNames {
			gs = append(gs, fmt.Sprintf("%s=%d", g, gates[g]))
		}
		parts = append(parts, fmt.Sprintf("%s{%s}", label, strings.Join(gs, " ")))
	}
	return fmt.Sprintf("gate-block summary (%d total): %s", total, strings.Join(parts, " "))
}

// resetGateBlocksForTest clears all state (test-only helper).
func resetGateBlocksForTest() {
	gateBlockMu.Lock()
	gateBlockDay = 0
	gateBlockCnts = map[gateBlockKey]int{}
	gateBlockMu.Unlock()
}

// ResetGateBlocksForTest clears all gate-block state for a test (test-only —
// never called from production). The package-global table is otherwise never
// reset between tests, so a test that asserts an absolute count fails at
// -count=2+ once a prior iteration (or another test) incremented the same
// (trader, gate) key.
func ResetGateBlocksForTest() {
	resetGateBlocksForTest()
}
