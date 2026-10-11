package trader

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"vl/kernel"
	"vl/mcp"
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
		fmt.Sprintf("The AI provider returned HTTP 402 (Insufficient Balance) at %s CT. Every decision cycle fails until the account is topped up (api.deepseek.com). The banner clears automatically on the first successful call.",
			kernel.FormatCT(now)))
	if first {
		at.logErrorf("🚨 AI-402 OUTAGE START at %s CT — one P0 banner raised; repeats are deduped for this outage.", kernel.FormatCT(now))
	}
}

// onAISuccess clears a latched 402 outage: recovery log + banner auto-ack.
// F5 (REL17, 2026-10-11): with the kill-switch ON it is RESTART-SAFE — it acks
// EVERY open (acked=0) ai-payment alert for this trader, not only the
// in-memory event id, so a fresh process (latch == 0) still clears a banner
// the previous process raised. OFF keeps the pre-REL17 behaviour byte-identical.
func (at *AutoTrader) onAISuccess(now time.Time) {
	latched := at.ai402OutageStartMs.Load() != 0
	var dur time.Duration
	var eventID string
	if latched {
		dur = now.Sub(time.UnixMilli(at.ai402OutageStartMs.Load())).Round(time.Minute)
		eventID = at.ai402EventID()
		at.ai402OutageStartMs.Store(0)
	}
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
		}
	}
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
