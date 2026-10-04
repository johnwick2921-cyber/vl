package trader

import (
	"fmt"
	"time"

	"strings"
	"vl/kernel"
	"vl/market"
)

// U1 (stale-bar dispatch 2026-08-19) — FEED PROBLEMS BECOME LOUD.
//
// The 08-19 00:53 VM reboot took the NT8 wire down for ~50 minutes and the only
// symptoms were mystery artifacts: planner fail-closed rows ("scenarios count 0
// invalid") from LLM calls made with NO market data, and an empty chart. The
// forensics filed it as U1: feed loss must announce itself, and the planner
// must refuse to spend an API call on an empty window.

// feedDownAfter() — B2 (T6): the flat-state threshold now comes from the ONE
// feed policy (kernel.LoadFeedPolicy, env FEED_ALERT_S, default 600s — the
// same 10 minutes as before, env-tunable and shared with the B4 fallback).
func feedDownAfter() time.Duration {
	return time.Duration(kernel.LoadFeedPolicy().FlatAlertMs) * time.Millisecond
}

// intradeFeedAlertDefault is the TIGHTENED threshold while a position is open
// (in-position silence fix 2026-08-19). Fail-open posture does NOT apply to
// position management: with money on the line, missing data must get LOUDER,
// never quieter. Override with INTRADE_FEED_ALERT_S.
const intradeFeedAlertDefault = 120 * time.Second

// intradeFeedAlertAfter — B2 (T6): same env, resolved through the ONE policy.
func intradeFeedAlertAfter() time.Duration {
	return time.Duration(kernel.LoadFeedPolicy().InPositionAlertMs) * time.Millisecond
}

// holdingOpenPosition reports whether this trader has any OPEN position row.
// Deliberately NOT gated on day_plan (unlike skipWhileOpen): the tightened
// in-position feed alert protects every held futures trade.
func (at *AutoTrader) holdingOpenPosition() bool {
	if at.store == nil {
		return false
	}
	positions, err := at.store.Position().GetOpenPositions(at.id)
	return err == nil && len(positions) > 0
}

// feedNewestBarAge returns the age of the newest 1m bar (open-stamp + interval
// = NT8's own stamp), ok=false when no bars exist at all.
func (at *AutoTrader) feedNewestBarAge(now time.Time) (time.Duration, bool) {
	if market.FuturesBarsProvider == nil {
		return 0, false
	}
	bars := market.FuturesBarsProvider(at.futuresSymbol(), "1m", 2)
	if len(bars) == 0 {
		return 0, false
	}
	newestStamp := bars[len(bars)-1].OpenTime + 60_000
	return time.Duration(now.UnixMilli()-newestStamp) * time.Millisecond, true
}

// checkFeedDown (3.1) fires a P0 alert + CRITICAL log when no bar has arrived
// for >10 min while the CME session is open. P0 renders as the persistent
// dashboard banner (in-app only — the owner's no-external-push rule). Deduped
// per gap: the event id carries the gap's start bucket, so one outage = one
// alert, and a NEW outage alerts again.
func (at *AutoTrader) checkFeedDown(now time.Time) {
	if at.config.Exchange != "ninjatrader" || !kernel.IsCMEOpen(now) {
		return
	}
	// IN-POSITION CONTRACT: liveness gets STRICTER while holding — the normal
	// 10-minute threshold drops to INTRADE_FEED_ALERT_S (default 120s).
	threshold := feedDownAfter()
	holding := at.holdingOpenPosition()
	if holding {
		threshold = intradeFeedAlertAfter()
	}
	age, haveBars := at.feedNewestBarAge(now)
	if haveBars && age <= threshold {
		return
	}
	gapStart := now.Add(-age)
	if !haveBars {
		gapStart = at.startTime // never had a bar this process — gap since boot
		if now.Sub(gapStart) <= threshold {
			return // give a fresh boot the same grace as a gap
		}
	}
	title := "NT8 bar feed DOWN"
	body := fmt.Sprintf("No bar frames for %s while the market is open. Nothing can trade and the planner will refuse to run until bars return. Start/check NinjaTrader on Windows.",
		now.Sub(gapStart).Round(time.Minute))
	if holding {
		title = "⚠ NO DATA WHILE POSITION OPEN — check NT8"
		body = fmt.Sprintf("A position is OPEN and no bar frame has arrived for %s (in-position threshold %s). The NT8-side OCO bracket still protects the trade, but the bot is price-blind: check NinjaTrader on Windows now.",
			now.Sub(gapStart).Round(time.Second), threshold)
	}
	at.logErrorf("🚨 FEED DOWN: no NT8 bar for %s while CME is OPEN (newest-bar stamp age; threshold %s; holding=%v). Charts, detectors and the planner are all blind. Check NT8 on the Windows side — the AddOn reconnects on its own.",
		now.Sub(gapStart).Round(time.Second), threshold, holding)
	at.emitAlert("P0", "feed-down",
		fmt.Sprintf("feeddown:%s", gapStart.UTC().Format("2006-01-02T15:04")),
		title, body)
}

// plannerPreflight (3.2) refuses to CALL the LLM when the bar window is empty
// or feed-down stale. It saves the API spend and kills the confusing 0-scenario
// fail-closed stubs the 08-19 outage produced. NOT writing a plan row is
// deliberate: the read window retries next cycle, and no re-plan budget is
// consumed by a refusal.
func (at *AutoTrader) plannerPreflight(session, tradeDate, trigger string) (ok bool) {
	// No provider wired at all = crypto or a test harness — can't assess, so
	// fail OPEN (mirrors B4's convention). The incident this guards against had
	// the provider WIRED and the cache EMPTY (wire down): that still refuses.
	if market.FuturesBarsProvider == nil {
		return true
	}
	now := traderNow() // class 32: wall-clock readers must agree on the trader clock (test seam; == time.Now() in prod)
	age, haveBars := at.feedNewestBarAge(now)
	if haveBars && age <= feedDownAfter() {
		return true
	}
	// CLASS 36 (owner ruling 2026-08-31, shipped 2026-09-01): a SCHEDULED read
	// (the session's open−30 read, the Sunday weekly) exists precisely to run
	// while the market is closed — the 16:00-17:00 halt and the weekend. The
	// freshness check is UNSATISFIABLE there by definition (2026-09-01: fifteen
	// `stale_bars_1865s…3545s` refusals 16:31→16:59, read launched 17:01:05,
	// fail-closed 17:23:14). Scheduled classes author from the last STORED bars
	// when the market is closed; staleness at trade time belongs to the
	// executor guards, never to this preflight. Non-scheduled classes
	// (death_replan, owner_reread, level_event, structure_mss) keep the check.
	if haveBars && preflightScheduledBypass(trigger, now) {
		newestCT, ageMin, _ := newestStoredBarInfo(at.futuresSymbol(), "1m", now)
		at.logWarnf("%s", preflightBypassLine(session, tradeDate, trigger, newestCT, ageMin))
		return true
	}
	reason := "no_bars"
	if haveBars {
		reason = fmt.Sprintf("stale_bars_%ds", int(age.Seconds()))
	}
	// D5 — a refusal is NEVER silent: one ⛔ line naming the trigger class.
	at.logErrorf("%s", preflightRefusalLine(session, tradeDate, trigger, reason))
	at.emitAlert("P1", "planner-preflight",
		fmt.Sprintf("preflight:%s:%s", tradeDate, session),
		fmt.Sprintf("%s planner read held — no bar data", session),
		"The bar feed is empty/stale, so the planner call was refused rather than producing a garbage plan. It retries automatically when bars return.")
	return false
}

// preflightTriggerClass names the trigger class for log lines: the scheduled
// session read passes "" (the write path labels it "<S>_scheduled_read").
func preflightTriggerClass(trigger string) string {
	t := strings.TrimSpace(trigger)
	if t == "" {
		return "session_read"
	}
	return t
}

// preflightIsScheduled reports whether a trigger class is SCHEDULED work — the
// session's open−30 read ("" / "<S>_scheduled_read") or the Sunday weekly read.
// Everything else is event-driven or owner-driven and keeps the freshness check.
func preflightIsScheduled(trigger string) bool {
	t := strings.TrimSpace(trigger)
	return t == "" || strings.HasSuffix(t, "_scheduled_read") || t == "weekly" || t == "sunday_weekly_read" || t == "weekly_boot_backfill"
}

// preflightScheduledBypass (CLASS 36) — the freshness check is skipped ONLY for
// a scheduled class AND only while the market is closed (halt / weekend /
// holiday). A scheduled read that runs into a live-but-silent tape (feed down
// during open hours) still refuses — that is the 08-19 outage class.
func preflightScheduledBypass(trigger string, now time.Time) bool {
	return preflightIsScheduled(trigger) && !kernel.IsCMEOpen(now)
}

// preflightBypassLine is the loud positive outcome (D5) — fixture-tested.
func preflightBypassLine(session, tradeDate, trigger, newestCT string, ageMin int) string {
	return fmt.Sprintf("🗓 preflight bypass (class 36): scheduled %s read %s %s during halt/weekend — freshness check skipped, authoring from last stored bars (newest 1m %s, age %dm)",
		preflightTriggerClass(trigger), session, tradeDate, newestCT, ageMin)
}

// preflightRefusalLine is the loud negative outcome (D5) — fixture-tested.
func preflightRefusalLine(session, tradeDate, trigger, reason string) string {
	return fmt.Sprintf("⛔ planner preflight refused %s: %s (session=%s trade_date=%s) — refusing to call the LLM with no market data (the 0-scenario fail-closed stub class); the read window retries next cycle",
		preflightTriggerClass(trigger), reason, session, tradeDate)
}

// PreflightBootLine (D7) names the class-36 accounting in the boot block.
func PreflightBootLine() string {
	return "preflight: scheduled reads bypass freshness in halt/weekend (class 36); executor halt-block unchanged (cmeSessionClosedSkip / IsCMEOpen)"
}
