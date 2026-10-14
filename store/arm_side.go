package store

import "strings"

// CanonicalArmSide is THE canonicalizer for armed_orders.side (class 28,
// "one canonicalizer per identifier, called where the value ENTERS"): UPPER,
// trimmed — "LONG" | "SHORT". UpsertArm calls it at the write; any reader of
// a row that may predate the write-side fix (data.db 10-14 still holds 18
// lowercase "short" and 19 "long" rows) calls it at the read.
func CanonicalArmSide(side string) string {
	return strings.ToUpper(strings.TrimSpace(side))
}
