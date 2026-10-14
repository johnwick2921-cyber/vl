package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
)

// RT-2 — LATCHED + GATE OFF. The owner switches the gate OFF after a trip
// (master and daily-loss both saved off). The latch is KEPT (fail-closed
// default; lifting it is an owner decision), so the boot line the trader
// prints at load (a Studio save reloads it; so does a restart) must read
// LATCHED — never "OFF … nothing refused, cancelled or flattened" — and
// GET /api/risk/status must say latched with the same lift rule. Both read
// the latch (kernel, else the persisted copy a restart restores); neither is
// a literal.
//
// MUTANT: have DailyLossGateBootLine ignore the latch (return the posture
// line alone) → the line reads "OFF" → RED.
func TestDailyLossLatchedGateOffBootLineAndStatusReadLatched(t *testing.T) {
	off := false
	cfg := store.StrategyConfig{RiskControl: store.RiskControlConfig{GuardrailsEnabled: &off, DailyLossEnabled: &off, DailyLossLimitUSD: 450}}
	at, st := resetTrader(t, cfg)
	at.id = "rt2-latched-off"
	kernel.ClearDailyForceFlat(at.id)
	t.Cleanup(func() { kernel.ClearDailyForceFlat(at.id) })
	now := dailyLossNow()
	day := kernel.CMESessionDayKey(now)

	// Not latched: the OFF posture line, unchanged.
	if l := DailyLossGateBootLine(st, at.id, &cfg, now); !strings.HasPrefix(l, "daily-loss gate: OFF (master off (saved)") {
		t.Fatalf("an unlatched OFF gate keeps its OFF line, got %q", l)
	}

	// (a) After a RESTART: only the persisted copy holds the latch (the
	// kernel map died with the process); the boot line prints before the
	// first beat restores it.
	if err := st.SetSystemConfig(dailyLossLatchKey(at.id), day+"|daily loss limit hit (bot today=-512.00, limit=-450.00)"); err != nil {
		t.Fatal(err)
	}
	line := DailyLossGateBootLine(st, at.id, &cfg, now)
	for _, want := range []string{
		"daily-loss gate: LATCHED for session-day " + day,
		"switching the gate OFF does not lift it",
		"Emergency Flat lifts it",
		"it ends at 17:00 CT",
		"bot today=-512.00",
		"master off (saved)",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("latched + gate OFF: the boot line must read %q, got:\n%s", want, line)
		}
	}
	if strings.Contains(line, "nothing refused, cancelled or flattened") {
		t.Fatalf("a latched gate must never claim nothing is refused:\n%s", line)
	}
	s := at.DailyLossGateStatusAt(now)
	if !s.Latched || s.LatchDay != day || !strings.Contains(s.LatchLifts, "switching the gate OFF does not lift it") || s.Enforced {
		t.Fatalf("/api/risk/status must say latched (day %s, the lift rule) with the switch OFF, got %+v", day, s)
	}
	// The verdict agrees with what the surfaces say: entries are refused.
	if v := at.dailyGuardVerdictAt(now); !v.Refuse || !v.Latched {
		t.Fatalf("the latch refuses with the gate OFF (kept, fail-closed), got %+v", v)
	}

	// (b) A STRATEGY SAVE (same process): the kernel latch is live.
	line = DailyLossGateBootLine(st, at.id, &cfg, now.Add(time.Minute))
	if !strings.Contains(line, "daily-loss gate: LATCHED for session-day "+day) {
		t.Fatalf("after a save the live kernel latch must read LATCHED, got:\n%s", line)
	}

	// (c) The next CME session-day: the latch has lifted; the line is OFF again.
	next := time.Date(2026, 10, 2, 17, 5, 0, 0, kernel.CTLocation())
	if l := DailyLossGateBootLine(st, at.id, &cfg, next); !strings.HasPrefix(l, "daily-loss gate: OFF") {
		t.Fatalf("after 17:00 CT the latch has lifted, got %q", l)
	}
	if s := at.DailyLossGateStatusAt(next); s.Latched || s.LatchDay != "" || s.LatchLifts != "" {
		t.Fatalf("after 17:00 CT the status must not say latched, got %+v", s)
	}
}
