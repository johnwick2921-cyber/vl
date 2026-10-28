package trader

import (
	"fmt"
	"strings"
	"time"

	"vl/internal/ownerpush"
	"vl/kernel"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// m432 (release #52) — OWNER PUSH for the ops alerts that matter while the bot
// cannot trade. The 2026-10-25 17:00 -> 2026-10-26 03:40 CT blind spell (NT8
// linked, its data connection off) raised FEED DOWN to the alert center and the
// log only; the owner heard about it from the CTO ~10 hours later. The same
// alerts now also go out as ONE outbound Telegram message per episode (see
// internal/ownerpush for the dedupe / rate limit / async / friend guarantees).
//
// Advisory only: the alert center row stays the authoritative record, and a
// push that is dropped (rate limit, queue full, no chat bound, friend
// instance) changes nothing about trading.

// alertKindBarHoleUnfilled — the P1 "NT8 has no history for this window" alert
// (item 2). An ops kind: it must reach the owner whatever the day-plan state.
const alertKindBarHoleUnfilled = "bar-hole-unfilled"

// ownerPushClock is the instant the feed-down CME-open leg is judged at; a var
// so the pins are not hostage to the wall clock (the CME daily break / weekend
// would otherwise make a green test red).
var ownerPushClock = time.Now

// ownerPushKind reports the alert kinds pushed to the owner. Anything else keeps
// the in-app surface only (no external-push for plan-content alerts).
func ownerPushKind(level, kind string) bool {
	switch kind {
	case "feed-down",
		"trader_down_no_evaluator",
		"dead_man_reconcile_stuck",
		"unprotected_position",
		"protection_partial",
		"protection_pending_stuck",
		"protection_over_covered":
		return level == "P0"
	case alertKindBarHoleUnfilled:
		return true
	}
	return false
}

// ownerPushKey is the dedupe key. Feed-down and the bar-hole notice describe
// the MARKET DATA, not a trader: every trader on the box raises its own row for
// the same episode, but the owner is told once. The rest are per-trader facts.
func ownerPushKey(a *store.AlertDB) string {
	switch a.Kind {
	case "feed-down", alertKindBarHoleUnfilled:
		return a.Kind + "|" + a.EventID
	}
	return a.Kind + "|" + a.TraderID + "|" + a.EventID
}

// maybeOwnerPush is called by emitAlert for a NEWLY WRITTEN alert row (a
// store-deduped repeat never reaches it). Non-blocking by construction.
func (at *AutoTrader) maybeOwnerPush(a *store.AlertDB, now time.Time) {
	if !ownerPushKind(a.Level, a.Kind) {
		return
	}
	// Feed-down is only news while CME is open: the weekend / daily break is
	// not an outage (checkFeedDown already gates on it; this is the second leg).
	if a.Kind == "feed-down" && !kernel.IsCMEOpen(now) {
		return
	}
	text := fmt.Sprintf("🚨 vl %s — %s", a.Level, strings.TrimSpace(a.Title))
	if b := strings.TrimSpace(a.Body); b != "" {
		text += "\n" + b
	}
	if a.Kind != "feed-down" && a.Kind != alertKindBarHoleUnfilled && a.TraderID != "" {
		text += "\n(trader " + a.TraderID + ")"
	}
	ownerpush.Push(ownerPushKey(a), text)
}

// ownerPushFeedBack tells the owner the feed returned, once per episode.
func ownerPushFeedBack(episodeID string, newestBarAge time.Duration) {
	ownerpush.Push("feed-back|"+episodeID,
		fmt.Sprintf("✅ vl — NT8 bar feed is BACK (newest bar %s old). Bars are flowing again; trading can resume.",
			newestBarAge.Round(time.Second)))
}

// unfilledHolesFn is the tracker read (ntTrader.UnfilledHolesFeeding); a var so
// the trader-side pin can feed it without a socket fixture. The read's own
// pins (feeding live bars, calendar, symbol, budget spent) live in
// trader/ninjatrader/bar_holes_unfilled_owner_test.go.
var unfilledHolesFn = ntTrader.UnfilledHolesFeeding

// unfilledHoleBody is the owner's instruction, in plain words.
func unfilledHoleBody(h ntTrader.UnfilledHole) string {
	if h.ReplayDisagreed > 0 {
		// A replay that DISAGREED with live minutes was held out of the store as
		// another price scale: reloading history is the wrong first move.
		return fmt.Sprintf("The %s 1-minute bars for %s are still missing after %d re-requests, and %d NT8 replay(s) were rejected as another price scale. Read the bot log's scale lines before reloading NinjaTrader history.",
			h.Symbol, h.Window, h.Attempts, h.ReplayDisagreed)
	}
	return fmt.Sprintf("NT8 has no %s history for %s. In NinjaTrader: open an %s 1-minute chart, right-click, Reload All Historical Data; the bot picks it up on its next re-request.",
		h.Symbol, h.Window, h.Symbol)
}

// checkUnfilledHole (m432 item 2) raises ONE P1 alert (+ owner push) per hole
// the gap-backfill tracker gave up on while NT8 is linked and feeding live
// bars — the data provider is up, NT8 simply has no history for the span. The
// tracker keeps re-requesting on its own schedule, so the owner's reload
// self-heals it; this only tells him. Dedupe: one event id per hole window.
func (at *AutoTrader) checkUnfilledHole(now time.Time) {
	if at.config.Exchange != "ninjatrader" {
		return
	}
	sym := at.futuresSymbol()
	if sym == "" {
		return
	}
	for _, h := range unfilledHolesFn(sym, now) {
		at.emitAlert("P1", alertKindBarHoleUnfilled,
			fmt.Sprintf("barhole-unfilled:%s:%d", strings.ToUpper(h.Symbol), h.FromMs),
			fmt.Sprintf("NT8 has no %s history — reload it", h.Symbol),
			unfilledHoleBody(h))
	}
}
