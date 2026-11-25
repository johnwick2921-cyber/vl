package trader

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"vl/internal/ownerpush"
	"vl/kernel"
	"vl/mcp"
	"vl/store"
)

// P5 — 402 / BALANCE ALERT (ledger-close dispatch 2026-08-19, K11 class / U2).
//
// The 2026-08-18 incident: DeepSeek 402 "Insufficient Balance" killed 139
// consecutive cycles over 4.3h with only per-cycle log lines — zero alert.
// The per-CALL behavior was already correct (402 is non-retryable at the mcp
// layer: absent from the retryable-substring list → one attempt), so P5 adds
// the OUTAGE layer:
//   - a typed decision_records error class ("ai_payment_402") — one-query
//     forensics instead of LIKE-matching free text
//   - ONE P0 banner per outage ("AI CREDIT EXHAUSTED"), latched at the first
//     402 and auto-ACKED by the first successful call (banner clears without
//     human ack; a NEW outage re-alerts with a new event id)
//   - an optional daily DeepSeek balance check (AI_BALANCE_WARN, default OFF)

// classifyAIError maps an AI-call error to the typed decision_records class.
// The substrings mirror the existing telemetry classifier — the mcp layer
// flattens HTTP status into text, and that text shape is load-bearing.
func classifyAIError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if strings.Contains(s, "402") || strings.Contains(s, "Insufficient Balance") {
		return "ai_payment_402"
	}
	return "ai_call_failed"
}

// ai402EventID names the CURRENT outage (latched start instant) — the alert
// bus dedupes on it, so cycle #2..#139 of one outage are silent no-ops.
func (at *AutoTrader) ai402EventID() string {
	return "ai402:" + time.UnixMilli(at.ai402OutageStartMs.Load()).UTC().Format("2006-01-02T15:04")
}

// ai402ClearAllOn is the F5 (REL17) kill-switch: default ON. ON = recovery
// acks EVERY open ai-payment alert for this trader (restart-safe) and the
// planner / watch call sites join the decision path in raising (402) and
// clearing (success) the outage. OFF restores the pre-REL17 behaviour
// (in-memory event-id ack only; decision path is the only caller).
func ai402ClearAllOn() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AI402_CLEAR_ALL"))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// on402Failure latches the outage and raises the one P0 banner.
func (at *AutoTrader) on402Failure(now time.Time, err error) {
	first := at.ai402OutageStartMs.Load() == 0
	if first {
		at.ai402OutageStartMs.Store(now.UnixMilli())
	}
	at.emitAlert("P0", "ai-payment", at.ai402EventID(),
		"AI CREDIT EXHAUSTED — no decisions until topped up",
		fmt.Sprintf("The AI provider returned HTTP 402 (Insufficient Balance) at %s. Every decision cycle fails until the account is topped up (api.deepseek.com). The banner clears automatically on the first successful call.",
			kernel.FormatCT(now)))
	if first {
		at.logErrorf("🚨 AI-402 OUTAGE START at %s CT — one P0 banner raised; repeats are deduped for this outage.", kernel.FormatCT(now))
	}
	// m469: while the outage lasts, the owner is reminded once an hour.
	at.aiStillDownReminder(now)
}

// ── m469 — the AI outage reaches the owner ──────────────────────────────────
//
// 11-03 20:10 CT -> 11-06 10:39 CT: one trader's DeepSeek account answered 402
// for 2.5 days (460 of 460 decision cycles failed on 11-05). The P0 above was
// written to the alert center only and the owner was never told. The ai-payment
// P0 is now pushed (owner_push.go), and these add the rest of the episode: an
// hourly STILL DOWN while it lasts, ONE "AI back" when it ends, and a P0 when
// AI calls keep failing for a reason that is not a 402.

// aiStillDownEvery is the STILL DOWN reminder cadence during a 402 outage (the
// same hour as feedStillDownEvery). One an hour per trader sits inside the
// "ai-still-down" kind's 3/hour budget for up to three traders at once.
const aiStillDownEvery = time.Hour

// aiFailStreakAlert is how many AI calls in a row must fail for a non-402
// reason before the owner gets a P0. Evidence (decision_records, read-only,
// all history to 11-07): of 31 non-402 failures the runs were 10 (09-02, 16 min
// of watch cycles), 5, 4 and 3 (one degraded night, 10-15 01:12-04:49 UTC),
// then single misses; no ordinary blip ever reached 5. So 5 fires on the two
// real degraded spells and on nothing else. Safe mode (3 in a row) is a
// different thing: it stops new entries, it does not tell anyone.
const aiFailStreakAlert = 5

// aiOutageState is one trader's AI-outage bookkeeping. It lives beside the
// trader (keyed by its id) rather than on the AutoTrader struct, like the
// protection state maps: the planner, watcher and decision paths all report
// into it from their own goroutines, so it carries its own lock.
type aiOutageState struct {
	mu         sync.Mutex
	remindedN  int    // STILL DOWN reminders sent for the current 402 outage
	remindKey  string // the newest reminder's push key ("" = none)
	failStreak int    // non-402 AI failures in a row
	failSince  time.Time
	failEvent  string // the ai-failing P0's event id ("" = not raised this streak)
}

var aiOutages sync.Map // trader id -> *aiOutageState

func (at *AutoTrader) aiOutage() *aiOutageState {
	v, _ := aiOutages.LoadOrStore(at.id, &aiOutageState{})
	return v.(*aiOutageState)
}

// aiStillDownReminder pushes ONE "AI STILL DOWN" per full hour of a latched 402
// outage, counted from its first 402 (copying feedStillDownReminder). A newer
// reminder withdraws an older one that is still held, so a box that is also
// offline delivers only the latest.
func (at *AutoTrader) aiStillDownReminder(now time.Time) {
	startMs := at.ai402OutageStartMs.Load()
	if startMs == 0 {
		return
	}
	since := time.UnixMilli(startMs)
	n := int(now.Sub(since) / aiStillDownEvery)
	s := at.aiOutage()
	s.mu.Lock()
	if n < 1 || n <= s.remindedN {
		s.mu.Unlock()
		return
	}
	prev := s.remindKey
	s.remindedN = n
	key := fmt.Sprintf("ai-still-down|%s|%d|%d", at.id, since.Unix(), n)
	s.remindKey = key
	s.mu.Unlock()
	at.logErrorf("🚨 AI STILL DOWN: the AI provider has answered 402 (Insufficient Balance) since %s CT (%s) — reminder #%d to the owner; no AI decision can be made until the account is topped up", kernel.TableTimeCT(since), spanHM(now.Sub(since)), n)
	if prev != "" {
		ownerpush.Cancel(prev)
	}
	ownerpush.Push(key, fmt.Sprintf("🚨 vl — AI STILL DOWN since %s CT (%s) for trader %s: the AI provider answers 402 (Insufficient Balance), so every AI decision fails and no new trade can be planned. Top up the DeepSeek account (api.deepseek.com); the bot picks it up on its next call.",
		kernel.TableTimeCT(since), spanHM(now.Sub(since)), at.ownerPushTraderLabel()))
}

// noteAICallFailure counts a failed AI call that is NOT a 402 (a 402 has its
// own outage above) and raises ONE P0 when aiFailStreakAlert of them come in
// a row. Any successful AI call (onAISuccess) ends the streak.
// Called from every AI failure branch: the decision loop (after
// consecutiveAIFailures++), the planner call and the watch call
// (classifyWatchCallError). TestAIFailureBranchesFeedTheStreak pins the three.
func (at *AutoTrader) noteAICallFailure(now time.Time, err error) {
	if err == nil || classifyAIError(err) == "ai_payment_402" {
		return
	}
	s := at.aiOutage()
	s.mu.Lock()
	s.failStreak++
	if s.failStreak == 1 {
		s.failSince = now
	}
	n, since := s.failStreak, s.failSince
	raise := n >= aiFailStreakAlert && s.failEvent == ""
	if raise {
		s.failEvent = "ai-failing:" + since.UTC().Format("2006-01-02T15:04:05")
	}
	ev := s.failEvent
	s.mu.Unlock()
	if !raise {
		return
	}
	msg := err.Error()
	if r := []rune(msg); len(r) > 160 {
		msg = string(r[:160]) + "…"
	}
	at.logErrorf("🚨 AI FAILING: %d AI calls in a row failed since %s CT (not a payment error) — P0 raised; last error: %s", n, kernel.TableTimeCT(since), msg)
	at.emitAlert("P0", "ai-failing", ev,
		"AI calls keep failing — no AI decisions",
		fmt.Sprintf("The last %d AI calls failed in a row since %s CT (not a payment error). No new AI decision can be made while this lasts. Last error: %s. You get one message when the AI answers again.",
			n, kernel.TableTimeCT(since), msg))
}

// aiCallAnswered reports whether a decision cycle that returned no error really
// got an answer from the AI. A pre-call guardrail HOLD (kernel holdCycle) is a
// FullDecision with only SkipReason set and no AI response: no call was made,
// so it proves nothing about the AI and must not end a 402 outage. A post-call
// skip (schema_parse_failed, executor_plan_gate) carries the RawResponse: the
// AI did answer.
func aiCallAnswered(d *kernel.FullDecision) bool {
	return d != nil && (d.SkipReason == "" || d.RawResponse != "")
}

// onAISuccess clears a latched 402 outage: recovery log + banner auto-ack.
// F5 (REL17, 2026-10-11): with the kill-switch ON it is RESTART-SAFE — it acks
// EVERY open (acked=0) ai-payment alert for this trader, not only the
// in-memory event id, so a fresh process (latch == 0) still clears a banner
// the previous process raised. OFF keeps the pre-REL17 behaviour byte-identical.
// m469: it also ends the owner-push episode — the held ai-payment / reminder /
// ai-failing messages are withdrawn (stale news) and ONE "AI back" goes out.
func (at *AutoTrader) onAISuccess(now time.Time) {
	latched := at.ai402OutageStartMs.Load() != 0
	var dur time.Duration
	var eventID string
	var since time.Time
	if latched {
		since = time.UnixMilli(at.ai402OutageStartMs.Load())
		dur = now.Sub(since).Round(time.Minute)
		eventID = at.ai402EventID()
		at.ai402OutageStartMs.Store(0)
	}
	at.endAIOwnerEpisode(now, latched, eventID, since)
	if at.store == nil {
		return
	}
	var n int64
	var err error
	if ai402ClearAllOn() {
		n, err = at.store.Alert().AckAllByKind(at.id, "ai-payment")
	} else {
		if !latched {
			return
		}
		n, err = at.store.Alert().AckByEvent(at.id, eventID)
	}
	if err != nil {
		at.logWarnf("⚠️ ai-402 recovery: banner auto-ack failed: %v", err)
	} else if n > 0 {
		if latched {
			at.logInfof("✅ AI PAYMENT RECOVERED after %s — %d 402 banner(s) cleared automatically.", dur, n)
		} else {
			at.logInfof("✅ AI PAYMENT RECOVERED (restart-safe) — %d stale 402 banner(s) cleared automatically.", n)
			at.endRestartedAIOwnerEpisode()
		}
	}
}

// endAIOwnerEpisode closes the in-memory owner-push side of an AI outage on the
// first successful call: withdraws the held messages and says the AI is back,
// once. Nothing open = nothing sent (a success is the normal case).
func (at *AutoTrader) endAIOwnerEpisode(now time.Time, latched402 bool, eventID402 string, since402 time.Time) {
	s := at.aiOutage()
	s.mu.Lock()
	remindKey := s.remindKey
	failEvent, failSince, failN := s.failEvent, s.failSince, s.failStreak
	s.remindedN, s.remindKey = 0, ""
	s.failStreak, s.failSince, s.failEvent = 0, time.Time{}, ""
	s.mu.Unlock()
	if remindKey != "" {
		ownerpush.Cancel(remindKey)
	}
	label := at.ownerPushTraderLabel()
	switch {
	case latched402:
		ownerpush.Cancel(ownerPushKey(&store.AlertDB{Kind: "ai-payment", TraderID: at.id, EventID: eventID402}))
		if failEvent != "" {
			ownerpush.Cancel(ownerPushKey(&store.AlertDB{Kind: "ai-failing", TraderID: at.id, EventID: failEvent}))
		}
		ownerpush.Push("ai-back|"+at.id+"|"+eventID402, fmt.Sprintf("✅ vl — AI is BACK for trader %s after %s (402 Insufficient Balance since %s CT, back %s). While it was down no AI decision could be made; it answers again now.",
			label, spanHM(now.Sub(since402)), kernel.TableTimeCT(since402), kernel.ClockCT(now)))
	case failEvent != "":
		ownerpush.Cancel(ownerPushKey(&store.AlertDB{Kind: "ai-failing", TraderID: at.id, EventID: failEvent}))
		ownerpush.Push("ai-back|"+at.id+"|"+failEvent, fmt.Sprintf("✅ vl — AI is BACK for trader %s after %s (%d+ calls failed since %s CT, back %s). It answers again now.",
			label, spanHM(now.Sub(failSince)), failN, kernel.TableTimeCT(failSince), kernel.ClockCT(now)))
	}
}

// endRestartedAIOwnerEpisode is the restart-safe half: this process never
// latched the outage, but it just cleared ai-payment banners a previous process
// raised. Their pushes may still be held in the owner-push state file (it
// survives a restart), so they are withdrawn by their own keys and ONE "AI back"
// is sent instead of a stale "CREDIT EXHAUSTED".
func (at *AutoTrader) endRestartedAIOwnerEpisode() {
	rows, err := at.store.Alert().List(at.id, 50)
	if err != nil {
		return
	}
	first := ""
	for _, a := range rows {
		if a.Kind != "ai-payment" {
			continue
		}
		ownerpush.Cancel(ownerPushKey(a))
		if first == "" {
			first = a.EventID
		}
	}
	if first == "" {
		return
	}
	ownerpush.Push("ai-back|"+at.id+"|"+first, fmt.Sprintf("✅ vl — AI is BACK for trader %s: the AI provider answers again after a 402 (Insufficient Balance) outage raised before the last restart.",
		at.ownerPushTraderLabel()))
}

// aiBalanceWarnThreshold reads AI_BALANCE_WARN (USD). Unset/invalid = OFF —
// the poller never fires (dispatch 5.4 default).
func aiBalanceWarnThreshold() (float64, bool) {
	v := os.Getenv("AI_BALANCE_WARN")
	if v == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	return f, true
}

// maybeCheckAIBalance polls the DeepSeek balance endpoint once per CME
// session-day when AI_BALANCE_WARN is set. DeepSeek exposes GET /user/balance
// (Bearer auth) — mcp.DeepSeekBalance wraps it defensively; any error is a
// WARN-and-move-on (never blocks trading).
func (at *AutoTrader) maybeCheckAIBalance(now time.Time) {
	threshold, on := aiBalanceWarnThreshold()
	if !on || at.config.CustomAPIKey == "" {
		return
	}
	day := kernel.CMESessionDayKey(now)
	if at.lastAIBalanceDay == day {
		return
	}
	at.lastAIBalanceDay = day

	bal, currency, err := mcp.DeepSeekBalance(at.config.CustomAPIKey)
	if err != nil {
		at.logWarnf("⚠️ AI balance check failed (poll continues tomorrow): %v", err)
		return
	}
	at.logInfof("💳 AI balance: %.2f %s (warn threshold %.2f)", bal, currency, threshold)
	if bal < threshold {
		at.emitAlert("P1", "ai-balance",
			"aibal:"+day,
			fmt.Sprintf("AI balance low: %.2f %s", bal, currency),
			fmt.Sprintf("The DeepSeek account balance %.2f %s is below AI_BALANCE_WARN=%.2f. Top up before it hits 402 and decisions stop.", bal, currency, threshold))
		at.logWarnf("⚠️ AI BALANCE LOW: %.2f %s < %.2f — top up before decisions stop.", bal, currency, threshold)
	}
}
