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
		text += "\n(trader " + at.ownerPushTraderLabel() + ")"
	}
	// m438: the trader ids on this box are 65-74 characters, which Scrub (rightly)
	// redacts as token-shaped; a body that quotes the id would read "[redacted]".
	// Say WHICH trader by its display name instead — before Scrub runs.
	if a.TraderID != "" {
		text = strings.ReplaceAll(text, a.TraderID, at.ownerPushTraderLabel())
	}
	ownerpush.Push(ownerPushKey(a), text)
}

// ownerPushTraderLabel names this trader for a phone message: its display NAME
// (never a secret), else the id's short form. The raw id is 65-74 characters and
// is redacted by the push scrubber.
func (at *AutoTrader) ownerPushTraderLabel() string {
	if n := strings.TrimSpace(at.name); n != "" {
		return n
	}
	return shortID(at.id)
}

// ownerPushFeedBack tells the owner the feed returned, once per episode.
func ownerPushFeedBack(episodeID string, newestBarAge time.Duration) {
	// m438: a FEED DOWN message still held by the budget is no longer news once
	// the feed is back — withdraw it so it is never delivered after the BACK.
	ownerpush.Cancel("feed-down|" + episodeID)
	ownerpush.Push("feed-back|"+episodeID,
		fmt.Sprintf("✅ vl — NT8 bar feed is BACK (newest bar %s old). Bars are flowing again; trading can resume.",
			newestBarAge.Round(time.Second)))
}

// unfilledHolesFn is the tracker read (ntTrader.UnfilledHolesFeeding); a var so
// the trader-side pin can feed it without a socket fixture. The read's own
// pins (feeding live bars, calendar, symbol, budget spent) live in
// trader/ninjatrader/bar_holes_unfilled_owner_test.go.
var unfilledHolesFn = ntTrader.UnfilledHolesFeeding

// reaskText renders the tracker's slow re-request horizon ("about 2 hours").
func reaskText(d time.Duration) string {
	if d <= 0 {
		return "for a while"
	}
	if d%time.Hour == 0 {
		h := int(d / time.Hour)
		if h == 1 {
			return "for about 1 hour"
		}
		return fmt.Sprintf("for about %d hours", h)
	}
	return fmt.Sprintf("for about %d minutes", int(d.Round(time.Minute)/time.Minute))
}

// unfilledHoleBody is the owner's instruction, in plain words. It must stay TRUE
// for as long as the alert is the owner's latest word (m438): the tracker's slow
// re-requests are capped IN TOTAL (UnfilledHole.ReaskHorizon after the alert), so
// the reload instruction names that horizon and the restart that is needed after it.
func unfilledHoleBody(h ntTrader.UnfilledHole) string {
	if h.ReplayDisagreed > 0 {
		// A replay that DISAGREED with live minutes was held out of the store as
		// another price scale: reloading history is the wrong first move.
		return fmt.Sprintf("The %s 1-minute bars for %s are still missing after %d re-requests, and %d NT8 replay(s) were rejected as another price scale. Read the bot log's scale lines before reloading NinjaTrader history.",
			h.Symbol, h.Window, h.Attempts, h.ReplayDisagreed)
	}
	return fmt.Sprintf("NT8 has no %s history for %s. In NinjaTrader: open an %s 1-minute chart, right-click, Reload All Historical Data. The bot re-asks NT8 about every %d minutes %s and picks the reload up on its next re-request; if you reload later than that, restart the bot afterwards (it re-subscribes on start) — you will get a second message when it stops re-asking.",
		h.Symbol, h.Window, h.Symbol, int(h.ReaskEvery/time.Minute), reaskText(h.ReaskHorizon))
}

// unfilledHoleSpentBody is the follow-up when the tracker has spent its slow
// re-requests: the first message promised a pick-up that no longer happens by
// itself, so the owner is told the one step that still works.
func unfilledHoleSpentBody(h ntTrader.UnfilledHole) string {
	if h.ReplayDisagreed > 0 {
		return fmt.Sprintf("The %s 1-minute bars for %s are still missing and the bot has stopped re-asking NT8. %d NT8 replay(s) were rejected as another price scale: read the bot log's scale lines first.",
			h.Symbol, h.Window, h.ReplayDisagreed)
	}
	return fmt.Sprintf("The bot has STOPPED re-asking NT8 for the %s 1-minute bars of %s (re-requests spent). A history reload alone will not be picked up now: in NinjaTrader open an %s 1-minute chart, right-click, Reload All Historical Data, THEN restart the bot so it asks again.",
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
		if h.SlowSpent {
			at.emitAlert("P1", alertKindBarHoleUnfilled,
				fmt.Sprintf("barhole-spent:%s:%d", strings.ToUpper(h.Symbol), h.FromMs),
				fmt.Sprintf("NT8 %s history still missing — reload it, then restart the bot", h.Symbol),
				unfilledHoleSpentBody(h))
		}
	}
}
