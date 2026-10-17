package store

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// armStates is the sole arm lifecycle classification table. Unknown states
// are non-terminal: an unfamiliar value must never hide possible exposure.
var armStates = map[string]bool{
	StateArmed:         false,
	StatePlacePending:  false,
	StateWorking:       false,
	StateCancelPending: false,
	StateFilled:        true,
	StateCancelled:     true,
	"canceled":         true,
	StateRejected:      true,
	"expired":          true,
	"superseded":       true,
	"shadowed":         true,
}

// IsTerminalArmState is the predicate formerly local to trader/one_contract.go.
// Both Go readers and the generated SQL use this classification.
func IsTerminalArmState(state string) bool {
	return armStates[strings.ToLower(strings.TrimSpace(state))]
}

// IsKnownArmState permits lifecycle actions to retain their refusal of unknown
// states while still using the canonical terminal predicate for exposure reads.
func IsKnownArmState(state string) bool {
	_, known := armStates[strings.ToLower(strings.TrimSpace(state))]
	return known
}

// ArmStateNames returns a copy for classification parity checks and source guards.
func ArmStateNames() []string {
	names := make([]string, 0, len(armStates))
	for name := range armStates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsUnplacedArm separates an authorization from possible broker exposure.
// A pending/working/unknown row with a missing identity is NOT an authorization.
func IsUnplacedArm(state, signalID string) bool {
	return !IsTerminalArmState(state) && strings.EqualFold(strings.TrimSpace(state), StateArmed) && strings.TrimSpace(signalID) == ""
}

var normalizedArmStateSQL = buildNormalizedArmStateSQL()
var terminalArmSQL = buildTerminalArmSQL()

func buildTerminalArmSQL() string {
	quoted := []string{}
	for _, state := range ArmStateNames() {
		if IsTerminalArmState(state) {
			quoted = append(quoted, "'"+strings.ReplaceAll(state, "'", "''")+"'")
		}
	}
	return normalizedArmStateSQL + " IN (" + strings.Join(quoted, ",") + ")"
}

func buildNormalizedArmStateSQL() string {
	// SQLite's default TRIM removes only ASCII spaces. Derive its trim set
	// from the same Unicode White_Space table used by Go's strings.TrimSpace.
	spaces := []string{}
	for _, span := range unicode.White_Space.R16 {
		for r := uint32(span.Lo); r <= uint32(span.Hi); r += uint32(span.Stride) {
			spaces = append(spaces, strconv.Itoa(int(r)))
		}
	}
	for _, span := range unicode.White_Space.R32 {
		for r := span.Lo; r <= span.Hi; r += span.Stride {
			spaces = append(spaces, strconv.Itoa(int(r)))
		}
	}
	// SQLite LOWER is ASCII-only. Derive the non-ASCII characters Go maps
	// into ASCII (e.g. dotted capital I), so they cannot disagree on a state.
	expr := "coalesce(state, '')"
	for _, span := range unicode.CaseRanges {
		for r := span.Lo; r <= span.Hi; r++ {
			lower := unicode.ToLower(rune(r))
			if r > unicode.MaxASCII && lower >= 'a' && lower <= 'z' {
				expr = fmt.Sprintf("replace(%s, char(%d), '%c')", expr, r, lower)
			}
		}
	}
	return "lower(trim(" + expr + ", char(" + strings.Join(spaces, ",") + ")))"
}

// TerminalArmStateSQL is a predicate for the armed_orders state column.
// NULL has the same conservative classification as an empty Go string.
func TerminalArmStateSQL() string { return terminalArmSQL }

// NonTerminalArmStateSQL negates the canonical predicate; it owns no state list.
func NonTerminalArmStateSQL() string { return "NOT (" + TerminalArmStateSQL() + ")" }

// SweepableArmStateSQL excludes cancellations already owned by the settlement
// pass. The boot sweep uses raw SetState; letting it select cancel_pending
// would bypass ConfirmCancel and its persisted snapshot evidence. Liveness and
// leg 4 still include those rows through NonTerminalArmStateSQL.
func SweepableArmStateSQL() string {
	return "(" + NonTerminalArmStateSQL() + ") AND " + normalizedArmStateSQL + " <> '" + StateCancelPending + "'"
}

// PlacementCensusLine reads the same terminal/authorization classification used
// by leg 4. A failed read reports UNKNOWN, never a fabricated zero.
func (s *ArmedOrderStore) PlacementCensusLine() string {
	var rows []ArmedOrderDB
	if s == nil || s.db == nil {
		return "arm placement census: UNKNOWN (ledger unavailable)"
	}
	if err := s.db.Where(NonTerminalArmStateSQL()).Find(&rows).Error; err != nil {
		return "arm placement census: UNKNOWN (ledger read failed)"
	}
	working, armed := 0, 0
	for _, row := range rows {
		if IsUnplacedArm(row.State, row.SignalID) {
			armed++
		} else {
			working++
		}
	}
	return fmt.Sprintf("arm placement census: %d working/unconfirmed · %d armed (authorized, no signal id; informational)", working, armed)
}
