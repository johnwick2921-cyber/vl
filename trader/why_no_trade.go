package trader

import (
	"vl/internal/datareport"
)

// ── m469 WHY NO TRADE (read-only) ──────────────────────────────────────────
//
// The nightly report opens with one line per trader answering "why no trade
// today". For a mentor-mode trader the answer is its funnel: how many setups
// the evaluator emitted, how many orders went out and filled, and which
// refusals stopped the rest. Those counters live only in memory, so this file
// hands a COPY of them to the report. It never changes any trading behaviour.

// WhyNoTradeInput returns this trader's why-no-trade inputs for the nightly
// report. A trader with mentor mode off has no funnel (Mentor=false): the
// report answers for it from its AI decision rows instead.
//
// The refusal figures are THIS session's only: the ledgers count from the
// trader's start, so they are diffed against the copy taken at the 17:00
// roll. When the trader started inside the session there is no such copy and
// Since says when the counting began (the report labels it "since boot").
func (at *AutoTrader) WhyNoTradeInput() datareport.TraderFunnel {
	out := datareport.TraderFunnel{ID: at.GetID(), Name: at.GetName()}
	if !at.mentorEnabled() {
		return out
	}
	out.Mentor = true
	f := &at.mentorFunnel
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dayKey == "" {
		return out // no bar evaluated yet: nothing is known
	}
	out.Have = true
	out.DayKey = f.dayKey
	out.Since = f.since
	out.Setups, out.Authored, out.Placed, out.Filled = f.entries, f.authored, f.placed, f.filled
	var kRestart, tRestart bool
	out.KernelRefusals, kRestart = sessionCounts(f.lastKernel, f.baseKernel)
	out.TraderRefusals, tRestart = sessionCounts(f.lastTrader, f.baseTrader)
	out.CountersRestarted = kRestart || tRestart
	return out
}

// sessionCounts is cur minus the session-start baseline, zero entries dropped.
// If any reason counts LOWER than its baseline the ledger was rebuilt after
// the roll (the evaluator was recreated), so everything it now holds was
// counted this session: cur is returned whole and restarted=true (the figure
// is partial: what came before the rebuild is gone).
func sessionCounts(cur, base map[string]int) (out map[string]int, restarted bool) {
	for k, b := range base {
		if cur[k] < b {
			return copyCounts(cur), true
		}
	}
	out = map[string]int{}
	for k, v := range cur {
		if d := v - base[k]; d > 0 {
			out[k] = d
		}
	}
	return out, false
}

// copyCounts copies a reason -> count map (nil stays nil).
func copyCounts(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
