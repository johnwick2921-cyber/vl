package trader

import "vl/store"

// ── ENTRY GROUP — the group admission for the mentor split legs ─────────────
//
// One mentor intent of n contracts splits AT PLACEMENT into leg 1 (ceil(n/2))
// and leg 2 (the runner): TWO arm rows that share ONE EntryGroup (the mentor
// intent's ArmID) plus LegIndex 0/1 and LegCount 2. The one-entry guards
// (latch, one-contract adjudication, sibling-cancel) treat rows sharing a
// non-empty EntryGroup as ONE entry: a sibling leg is admitted, every OTHER
// entry stays refused exactly as today (CTO ruling 2026-10-04 18:21).
//
// A row with EntryGroup == '' is not part of a split group — every predicate
// below then answers "no sibling", which is byte-identical to today's
// behaviour for every non-mentor / legacy row.

// sameEntryGroup reports whether a and b are DISTINCT rows of the SAME split
// entry — both carry the same non-empty EntryGroup.
func sameEntryGroup(a, b store.ArmedOrderDB) bool {
	return a.EntryGroup != "" && a.EntryGroup == b.EntryGroup
}

// entryGroupSiblings returns the OTHER rows in r's entry group. nil when r
// carries no EntryGroup (a legacy / non-split arm) or has no siblings.
func entryGroupSiblings(rows []store.ArmedOrderDB, r store.ArmedOrderDB) []store.ArmedOrderDB {
	if r.EntryGroup == "" {
		return nil
	}
	var out []store.ArmedOrderDB
	for _, rr := range rows {
		if rr.ID != r.ID && rr.EntryGroup == r.EntryGroup {
			out = append(out, rr)
		}
	}
	return out
}

// entryGroupSiblingSignalIDs returns the signal ids of r's siblings that
// already carry one (already placed / working at the broker). nil when none.
// The one-contract book exemption keys on these: a working order whose name is
// a sibling's signal id is the group's OWN entry, not a competing one.
func entryGroupSiblingSignalIDs(rows []store.ArmedOrderDB, r store.ArmedOrderDB) map[string]bool {
	var out map[string]bool
	for _, s := range entryGroupSiblings(rows, r) {
		if s.SignalID == "" {
			continue
		}
		if out == nil {
			out = map[string]bool{}
		}
		out[s.SignalID] = true
	}
	return out
}

// entryGroupFilledSiblingCount counts r's siblings that have already FILLED —
// each owns a share of the group's position, so the one-contract open-position
// count must not count them against the group.
func entryGroupFilledSiblingCount(rows []store.ArmedOrderDB, r store.ArmedOrderDB) int {
	n := 0
	for _, s := range entryGroupSiblings(rows, r) {
		if s.State == store.StateFilled || s.FillQuantity > 0 {
			n++
		}
	}
	return n
}
