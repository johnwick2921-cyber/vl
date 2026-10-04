package mentor

import (
	"strings"
	"time"
)

// LevelArmExpiry is THE lifetime of a level ("lvl-") arm (D2-44, item 11): the
// first-touch order RESTS until the next 15:00 CT — the RTH window end — or an
// earlier close-through cancel [D2.3 p1 @17:42–19:12]. One definition, read by
// the trader's injector (mentorIntentExpiry) and by the leg budget's pend
// simulation (Limits), so the budget never stops watching an order the broker
// still holds.
func LevelArmExpiry(nowMs int64) int64 {
	now := time.UnixMilli(nowMs).In(ctime())
	end := time.Date(now.Year(), now.Month(), now.Day(), 15, 0, 0, 0, ctime())
	if now.After(end) {
		end = end.AddDate(0, 0, 1)
	}
	return end.UnixMilli()
}

// isLevelArmID reports whether an ArmID belongs to a level arm.
func isLevelArmID(armID string) bool {
	return strings.HasPrefix(strings.TrimSpace(armID), "lvl-")
}

// pendExpiry is the expiry the leg budget simulates for an intent: its own
// expiry when it carries one; a level arm's LevelArmExpiry otherwise; 0 (the
// one-candle default handled in simulate) for every other zero-expiry intent.
func pendExpiry(in Intent, nowMs int64) int64 {
	if in.ExpiryMs != 0 {
		return in.ExpiryMs
	}
	if isLevelArmID(in.ArmID) {
		return LevelArmExpiry(nowMs)
	}
	return 0
}
