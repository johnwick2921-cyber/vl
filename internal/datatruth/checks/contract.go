package checks

import (
	"fmt"
	"sort"
	"strings"
)

// ── CHECK 5: CONTRACT ROLL GUARD ─────────────────────────────────────────────
//
// MNQ rolls 12-26 -> 03-27 in December. The 2026-09-10 roll left ~2,000
// September bars in the ring with December ones appended: a ~292 pt step that
// every reader took for a move. The ring is now purged on a roll
// (TCPServer.observeContract), so the guard here is the net under that purge:
//
//   - two different contracts in the bars of one decision window -> block;
//   - the contract the AddOn named (live) differs from the contract of the
//     history the evaluator was seeded from (stored) -> block;
//   - either fact unknown while the other is known -> block (fail closed): a
//     value that cannot be read is not "the same".
//
// It clears the moment the window is single-contract and live == history.

// ContractFacts are the observations the wiring gathers.
type ContractFacts struct {
	Live    string   // contract named by the AddOn's latest subscribed ACK ("" = none)
	History string   // contract of the stored history the evaluator seeded from ("" = unknown)
	Window  []string // distinct contracts seen on bars inside the decision window
}

func norm(s string) string { return strings.ToUpper(strings.Join(strings.Fields(s), " ")) }

// JudgeContract returns the verdict for one symbol.
func JudgeContract(f ContractFacts) (blocked bool, why string) {
	seen := map[string]bool{}
	for _, c := range f.Window {
		if c = norm(c); c != "" {
			seen[c] = true
		}
	}
	if len(seen) > 1 {
		var cs []string
		for c := range seen {
			cs = append(cs, c)
		}
		sort.Strings(cs)
		return true, fmt.Sprintf("bars from %d contracts inside one decision window (%s)", len(cs), strings.Join(cs, ", "))
	}
	live, hist := norm(f.Live), norm(f.History)
	switch {
	case live == "" && hist == "":
		return true, "neither the live contract nor the history contract is known"
	case live == "":
		return true, fmt.Sprintf("live contract unknown (no subscribed ACK) while history is %s", hist)
	case hist == "":
		return true, fmt.Sprintf("history contract unknown while live is %s", live)
	case live != hist:
		return true, fmt.Sprintf("live contract %s differs from the stored history contract %s", live, hist)
	}
	if len(seen) == 1 {
		for c := range seen {
			if c != live {
				return true, fmt.Sprintf("bars in the decision window are %s but the live contract is %s", c, live)
			}
		}
	}
	return false, ""
}
