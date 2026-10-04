package trader

import (
	"fmt"
	"time"
	"vl/kernel"

	"vl/mcp"
	"vl/store"
)

// ── WATCHDOG FIRE RECORDER (owner ruling 2026-09-02) ────────────────────────
// The watchdog fired for the first time at 20:50:44 CT on 2026-09-02 — 376.1 s
// into a call that had produced 60,034 reasoning chars and then went silent for
// its full limit. One fire is an anecdote. This records every fire with its
// call age, its bytes, and what the identical resend then did, so a week can be
// read as a table. The resend outcome is the load-bearing column: a watchdog
// that kills calls the resend cannot recover is worse than one that waits.

// installWatchdogFireRecorder wires the mcp hook to this trader's store. Called
// once per trader at start; the hook is process-wide, so the LAST trader to
// register owns it — acceptable because the row carries the trader id and the
// single-trader case is the live one. Stated rather than hidden.
func (at *AutoTrader) installWatchdogFireRecorder() {
	if at.store == nil || at.store.WatchdogFires() == nil {
		return
	}
	id := at.id
	st := at.store
	log := at.logWarnf
	mcp.SetWatchdogFireHook(func(f mcp.WatchdogFire) {
		if _, err := st.WatchdogFires().Record(store.WatchdogFireDB{
			TraderID: id, At: time.Now().UTC(), Kind: f.Kind, Mode: f.Mode,
			GapMs: f.GapMs, LimitMs: f.LimitMs, CallAgeMs: f.CallAgeMs, Bytes: f.Bytes,
			IdleBeforeMs: f.IdleBeforeMs, Reused: f.Reused, ClosedBy: f.ClosedBy,
		}); err != nil {
			log("⏱ %s record write failed: %v (measurement only — the stream still closed)", f.Kind, err)
		}
	})
}

// recordWatchdogResend attaches the identical resend's outcome to the newest
// unresolved fire. Called by the planner loop after a provider-failure resend
// completes. An unresolved row stays visibly unresolved rather than becoming a
// false success.
func (at *AutoTrader) recordWatchdogResend(ok bool, took time.Duration, note string) {
	if at.store == nil || at.store.WatchdogFires() == nil {
		return
	}
	attached, err := at.store.WatchdogFires().ResolveLatest(at.id, ok, took.Milliseconds(), note)
	if err != nil {
		at.logWarnf("⏱ watchdog resend outcome write failed: %v", err)
		return
	}
	if attached {
		at.logInfof("⏱ watchdog resend outcome: ok=%t in %.1fs (%s) — attached to the open fire row", ok, took.Seconds(), note)
	}
}

// WatchdogFireTable renders the week's table (pure, so the report and any
// future endpoint share one rendering).
func WatchdogFireTable(rows []store.WatchdogFireDB) string {
	if len(rows) == 0 {
		return "⏱ watchdog fires: none recorded"
	}
	out := fmt.Sprintf("⏱ watchdog fires: %d recorded\n  %-19s %-5s %8s %8s %10s  %s\n",
		len(rows), "at (UTC)", "mode", "gap_s", "age_s", "bytes", "resend")
	fired, recovered := 0, 0
	for _, r := range rows {
		fired++
		resend := "UNRESOLVED"
		if r.Resolved {
			if r.ResendOK {
				recovered++
				resend = fmt.Sprintf("ok in %.1fs", float64(r.ResendMs)/1000)
			} else {
				resend = fmt.Sprintf("FAILED after %.1fs (%s)", float64(r.ResendMs)/1000, r.ResendNote)
			}
		}
		out += fmt.Sprintf("  %-19s %-5s %8.1f %8.1f %10d  %s\n",
			// TZ GUARD (merged-HEAD integration, 2026-09-03): a bare layout here
			// broke TestTZGuardSingleTimeSource once the tz-guard lane merged.
			// One time source — kernel.FormatCT — never a local layout string.
			kernel.FormatCT(r.At), r.Mode,
			float64(r.GapMs)/1000, float64(r.CallAgeMs)/1000, r.Bytes, resend)
	}
	out += fmt.Sprintf("  → %d fire(s), %d recovered by the identical resend", fired, recovered)
	return out
}
