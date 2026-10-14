package trader

import (
	"fmt"
	"strings"
	"time"

	"vl/store"
)

// ── N-I (release #22 rescan) — the arm path honours the session clock ────────
//
// Row 365 was AUTHORED at 08:24:00 ("⚔️ armed LONDON S1 … limit 30555.00")
// after LONDON's 08:15 last-entry cutoff — the same second the admission
// refused it. Its zone verdict then counted five WAITING short_of_zone
// refusals (08:29:04–08:29:44) for a row that could never place, and at
// 08:30:00 it was cancelled as "NY first-5m no-trade window" when the true
// cause was that LONDON had closed.

// planSessionPastLastEntry reports whether the plan's OWN session is the active
// one and its last-entry cutoff has passed (the reason names session + time).
// A plan for a session that is not the active one is not judged here.
func (at *AutoTrader) planSessionPastLastEntry(planSession string, now time.Time) (string, bool) {
	sess, ok := at.sessionRegistry(now).ActiveSession(now)
	if !ok || !strings.EqualFold(sess.Name, strings.TrimSpace(planSession)) {
		return "", false
	}
	return at.entryBlockedByLastEntryAt(now)
}

// sessionClosedAt reports whether the session window named has ENDED (its
// most recent end is nearer in the past than its next start) and is not open
// now. Unknown / malformed windows are never "closed" (no invented cause).
func (at *AutoTrader) sessionClosedAt(name string, now time.Time) bool {
	def, ok := at.sessionRegistry(now).SessionByName(name)
	if !ok || def == nil || def.InWindow(now) {
		return false
	}
	startMin, okS := hhmmToMin(def.WindowStartCT)
	endMin, okE := hhmmToMin(def.WindowEndCT)
	if !okS || !okE {
		return false
	}
	nowMin := ctMinutesNow(now)
	sinceEnd := ((nowMin-endMin)%1440 + 1440) % 1440
	untilStart := ((startMin-nowMin)%1440 + 1440) % 1440
	return sinceEnd < untilStart
}

// cancelClosedSessionArms cancels every non-terminal, non-mentor arm row of a
// session that has CLOSED, with that as the reason — before a band cancel
// would sweep it under the band's name. Same wire-cancel seam and pace.
func (at *AutoTrader) cancelClosedSessionArms(ledger *store.ArmedOrderStore, now time.Time, pace *armedCancelPace) {
	if ledger == nil {
		return
	}
	rows, err := ledger.ListNonTerminal(at.id)
	if err != nil {
		return
	}
	active := "none"
	if sess, ok := at.sessionRegistry(now).ActiveSession(now); ok {
		active = sess.Name
	}
	seen := map[string]bool{}
	for _, r := range rows {
		name := strings.TrimSpace(r.Session)
		if r.TraderID != at.id || name == "" || isMentorArmOrigin(r) || seen[strings.ToUpper(name)] {
			continue
		}
		seen[strings.ToUpper(name)] = true
		if strings.EqualFold(name, active) || !at.sessionClosedAt(name, now) {
			continue
		}
		reason := fmt.Sprintf("session ended (%s closed; %s is active)", name, active)
		skip := func(x store.ArmedOrderDB) bool {
			return isMentorArmOrigin(x) || !strings.EqualFold(strings.TrimSpace(x.Session), name)
		}
		if n, unacked := at.cancelArmedOrdersSyncFiltered(reason, skip, pace); n > 0 || unacked > 0 {
			at.logWarnf("🔒 armed cancel: %s — %d order(s) disarmed, %d unacked", reason, n, unacked)
		}
	}
}
