package trader

import (
	"strings"
	"testing"
	"time"

	"vl/store"
)

// P2-4 (release #26) — a mentor trader gets the soft guardrail audit on its own
// beat. The AI decision path runs CheckSoft in kernel/engine_analysis.go, which
// a mentor trader never enters; mentorSoftAudit closes that gap on the 60 s beat.
// Pure audit: the "would have tripped" line is logged and counted, never refused.
func TestMentorSoftAuditRunsWhenMasterOff(t *testing.T) {
	cfg := store.StrategyConfig{RiskControl: store.RiskControlConfig{
		DailyLossLimitUSD: 100,
		MentorMode:        true, // the mentor path; master OFF (GuardrailsEnabled nil)
	}}
	at, st := resetTrader(t, cfg)
	now := time.Date(2026, 8, 18, 10, 30, 0, 0, chicagoLoc())
	dlSeedClosed(t, st, at.id, now, -150, "") // a bot day deep enough to trip -100
	t.Cleanup(func() {
		dailyLossMu.Lock()
		dailyLossSoftAuditLogged = map[string]bool{}
		dailyLossMu.Unlock()
	})

	h := rtCaptureLog(t, "mentor guardrail WOULD have tripped")
	at.mentorSoftAudit(now)
	at.mentorSoftAudit(now) // the beat runs every 60 s — the line must be logged once

	if got := h.count("mentor guardrail WOULD have tripped"); got != 1 {
		t.Fatalf("mentor soft audit logged %d lines, want exactly 1 (dedup per trader/day/hit):\n%s", got, h.all())
	}
	if !strings.Contains(h.all(), "daily loss would trip") {
		t.Errorf("expected a daily-loss would-trip hit, got:\n%s", h.all())
	}
}

// MUTANT anchor: with the master ON the hard gate runs and the soft audit must
// stay silent (it is the master-OFF diagnostic, mirroring engine_analysis.go).
func TestMentorSoftAuditSilentWhenMasterOn(t *testing.T) {
	cfg := store.StrategyConfig{RiskControl: store.RiskControlConfig{
		DailyLossLimitUSD: 100,
		MentorMode:        true,
	}}
	dlOn(&cfg) // master + daily-loss ON
	at, st := resetTrader(t, cfg)
	now := time.Date(2026, 8, 18, 10, 30, 0, 0, chicagoLoc())
	dlSeedClosed(t, st, at.id, now, -150, "")

	h := rtCaptureLog(t, "mentor guardrail WOULD have tripped")
	at.mentorSoftAudit(now)
	if got := h.count("mentor guardrail WOULD have tripped"); got != 0 {
		t.Fatalf("soft audit must be silent when the master is ON, got %d lines:\n%s", got, h.all())
	}
}
