package trader

import (
	"fmt"
	"time"

	"vl/kernel"
	"vl/telemetry"
)

// P3.1 — SESSION GATES. Wire the P0 registry to live entry gating: entries are
// allowed ONLY inside an ENABLED session's window (NY-only by default), which
// closes the interim/overnight window, plus the spec's no-trade sub-windows
// (first 5m of the session, lunch 12:00–13:30 CT). GATED on day_plan → dormant
// by default. Session-flat at the enabled session's boundary is covered by P2.3's
// eod_flat (the NY flat 14:45 CT sits inside the NY window); multi-session flats
// arrive when ASIA/LONDON earn enablement.
//
// NOTE: the registry's `Killzones` currently hold high-probability ACTIVE windows
// (ny_am/ny_pm), so they are NOT used to block here — the dispatch's "no-trade
// sub-windows" are the spec-card-#5 windows below. Reconciling the killzone
// semantics is a P4 admin decision.

// sessionEntryBlocked reports (reason, blocked): whether NEW entries are blocked
// because we are outside an enabled session window or inside a no-trade
// sub-window. Gated on day_plan → dormant by default.
// sessionEntryBlocked is the entry point and owns the clock (A28/class 60); the
// judgement lives in sessionEntryBlockedAt so the ARM path — which already
// carries its own `now` — can consult the SAME band without a second clock read
// and without a second copy of the rule.
func (at *AutoTrader) sessionEntryBlocked() (string, bool) {
	return at.sessionEntryBlockedAt(time.Now())
}

func (at *AutoTrader) sessionEntryBlockedAt(now time.Time) (string, bool) {
	why, blocked, _ := at.sessionEntryBlockedT1At(now)
	return why, blocked
}

// sessionEntryBlockedT1At is sessionEntryBlockedAt that also hands back the
// T1 windows it read — nil when it returned before reading them (never an
// empty list standing in for "not read") — so a caller that goes on to
// forceFlatWindowAt reads the calendar ONCE (W1b E13 repair: the second read
// doubled the "📅 calendar FAIL-CLOSED" warning on every scan and event pass).
func (at *AutoTrader) sessionEntryBlockedT1At(now time.Time) (string, bool, *[]kernel.CTWindow) {
	if !at.dayPlanEnabled() {
		return "", false, nil
	}
	reg := at.sessionRegistry(now)
	// m308 — read the T1 windows FIRST and refuse the red-news blackout BEFORE
	// any session-list band reason. The mentor waives the session-list band
	// (session off, trade cap, outside-window, lunch, first-5m) but NEVER the
	// T1 blackout — so the blackout must win over every waivable reason. The
	// windows are read once here and handed back to forceFlatWindowAt.
	windows := at.currentT1Windows(now)
	if label, blocked := kernel.InT1Blackout(ctMinutesNow(now), windows); blocked {
		return "🔴 red-news blackout: " + label, true, &windows
	}
	// W9 + PART A — the strategy's session enable gates ENTRIES too (not just reads),
	// through the SAME resolver the read scheduler uses: an explicit per-session
	// toggle wins, otherwise registry + sessions_enabled. A session the owner
	// switched off admits no entries even in advisory mode.
	if sess, ok := reg.ActiveSession(now); ok {
		if runnable, why := at.sessionRunnable(sess); !runnable {
			return why, true, &windows
		}
		// W15.B — per-session trade cap (the override was persisted + rendered but
		// enforced by NOTHING). Absent → no cap, shipped behavior unchanged.
		if why, blocked := at.sessionTradeCapBlocked(sess, now); blocked {
			return why, true, &windows
		}
	}
	why, blocked := sessionGateDecision(reg, now, windows, at.sessionRunnable)
	return why, blocked, &windows
}

// sessionWindowStart returns the wall-clock start of the CURRENTLY-RUNNING
// instance of a session's window. Wrap-aware: at 01:00 CT inside ASIA's
// 17:00→02:00 window, the instance started at 17:00 YESTERDAY, so the naive
// "today at 17:00" would be in the future and would count zero trades.
func sessionWindowStart(sess *kernel.SessionDef, now time.Time) (time.Time, bool) {
	start, ok := hhmmToMin(sess.WindowStartCT)
	if !ok {
		return time.Time{}, false
	}
	loc := kernel.CTLocation()
	ct := now.In(loc)
	t := time.Date(ct.Year(), ct.Month(), ct.Day(), start/60, start%60, 0, 0, loc)
	if t.After(ct) {
		t = t.AddDate(0, 0, -1) // window opened yesterday and wrapped midnight
	}
	return t, true
}

// sessionTradeCapBlocked reports (reason, blocked): whether NEW entries are
// blocked because this session's own max_trades cap is reached. The count is
// entries since THIS session instance opened — not since the session-day start,
// which would make a late session inherit the earlier ones' trades. Scoped to the
// active NT account, mirroring the daily guardrail. No cap configured → never
// blocks; a cap of 0 means "no entries this session" and is honored as written.
func (at *AutoTrader) sessionTradeCapBlocked(sess *kernel.SessionDef, now time.Time) (string, bool) {
	if sess == nil || at.store == nil {
		return "", false
	}
	capN, ok := at.dayPlanCfg().MaxTradesFor(sess.Name)
	if !ok {
		return "", false
	}
	start, ok := sessionWindowStart(sess, now)
	if !ok {
		return "", false
	}
	_, entries, err := at.store.Position().GetSessionDayActivity(at.id, start.UnixMilli(), at.currentAccountName())
	if err != nil {
		at.logWarnf("⚠️ session trade cap: entry count failed (not blocking): %v", err)
		return "", false
	}
	if entries >= capN {
		return fmt.Sprintf("%s trade cap reached (%d/%d this session)", sess.Name, entries, capN), true
	}
	return "", false
}

// sessionGateDecision is the pure session-gate logic: entries only inside an
// ENABLED session window and outside the no-trade sub-windows (first-5m, lunch,
// and W3 red-news T1 blackouts).
func sessionGateDecision(reg kernel.SessionRegistry, now time.Time, t1Windows []kernel.CTWindow, runnable func(*kernel.SessionDef) (bool, string)) (string, bool) {
	sess, ok := reg.ActiveSession(now)
	if !ok {
		return "outside all session windows (overnight/interim)", true
	}
	// H8 — enablement is resolved by sessionRunnable (explicit per-session toggle
	// wins over the registry). The registry's Enabled flag is only the DEFAULT the
	// resolver consults; it must never VETO a session the resolver already said
	// runs. When no resolver is supplied (pure callers/tests), the registry flag
	// is the answer.
	if runnable != nil {
		if ok, why := runnable(sess); !ok {
			return why, true
		}
	} else if !sess.Enabled {
		return fmt.Sprintf("%s session not enabled", sess.Name), true
	}
	// W3 — HARD red-news blackout: no entry within ±T1BlackoutMinutes of a T1
	// event. Checked BEFORE the lunch and first-5m sub-windows: the FOMC /
	// Fed-minutes 13:00 CT print sits inside the 12:00–13:30 lunch window, and
	// a mentor arm waives ONLY lunch/first-5m (#9) — the T1 blackout must never
	// be masked by the lunch reason.
	if label, blocked := kernel.InT1Blackout(ctMinutesNow(now), t1Windows); blocked {
		return "🔴 red-news blackout: " + label, true
	}
	if inSessionFirst5m(sess, now) {
		return fmt.Sprintf("%s first-5m no-trade window", sess.Name), true
	}
	if kernel.InLunchNoTrade(now) {
		ls, le := kernel.LunchWindowCT()
		return fmt.Sprintf("lunch no-trade window (%s–%s CT)", ls, le), true
	}
	return "", false
}

// inSessionFirst5m reports whether now falls in the first 5 minutes of the
// session window (a no-trade sub-window).
func inSessionFirst5m(sess *kernel.SessionDef, now time.Time) bool {
	start, ok := hhmmToMin(sess.WindowStartCT)
	if !ok {
		return false
	}
	_ = start
	return kernel.InFirstNoTradeMinutes(sess.WindowStartCT, now)
}

// ---- P3.6-D — night mode ------------------------------------------------------

// nightEdgeDecision reports whether a night/day TRANSITION event should fire:
// only when the prior state is known (prev != nil) and differs from now. A nil
// prev (fresh (re)start) never emits — the restart resumes the current state
// cleanly with no spurious edge.
func nightEdgeDecision(prev *bool, night bool) bool {
	return prev != nil && *prev != night
}

// observeNightEdge tracks the night/day state and logs an event on transitions.
// Restart during night resumes cleanly (nil prev → no event). GATED on day_plan.
func (at *AutoTrader) observeNightEdge() {
	if !at.dayPlanEnabled() {
		return
	}
	now := time.Now()
	// S2 (release #32): judge night with the SAME per-strategy resolver the
	// session gates use (NightModeAt → sessionRunnable), not the registry-only
	// IsNightMode — which called this strategy "night" while an explicit
	// sessions[].enable override kept ASIA/LONDON runnable.
	night := at.NightModeAt(now)
	if nightEdgeDecision(at.nightPrev, night) {
		if night {
			at.logInfof("🌙 NIGHT MODE — outside every session window this strategy runs; no reads, no entries until the next runnable window.")
		} else {
			at.logInfof("🌅 DAY MODE — a runnable session window opened.")
		}
		// P2-3a (release #26): a night/day transition is an EVENT, not a
		// refusal — it must not count as a gate-block.
		telemetry.IncTraderEvent(at.id, "night_transition")
	}
	at.nightPrev = &night
}
