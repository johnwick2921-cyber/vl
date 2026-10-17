package kernel

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"vl/market"
)

// P1.3 (ledger-close 2026-08-19) — the early-warning tier is decided by a pure
// classifier so tests inject FAKE drift (the dispatch's test hook), never touch
// the real clock.
func TestClassifyClockDrift(t *testing.T) {
	const warn, tol = 30_000, 60_000
	cases := []struct {
		driftMs int64
		want    string
	}{
		{0, ""},
		{29_999, ""},          // just under warn → silent
		{30_000, "warn"},      // exactly warn → early warning
		{45_000, "warn"},      // between warn and tolerance
		{60_000, "warn"},      // exactly tolerance → still the early tier (C2 uses >)
		{60_001, "critical"},  // past tolerance → the existing CRITICAL
		{116_000, "critical"}, // the −116s WSL incident class
	}
	for _, c := range cases {
		if got := classifyClockDrift(c.driftMs, warn, tol); got != c.want {
			t.Errorf("classifyClockDrift(%d) = %q, want %q", c.driftMs, got, c.want)
		}
	}
}

func TestClockWarnMsEnvOverride(t *testing.T) {
	if got := clockWarnMs(); got != 30_000 {
		t.Fatalf("default CLOCK_WARN_MS must be 30000 (50%% of C2 tolerance), got %d", got)
	}
	t.Setenv("CLOCK_WARN_MS", "10000")
	if got := clockWarnMs(); got != 10_000 {
		t.Fatalf("CLOCK_WARN_MS override not honored, got %d", got)
	}
	t.Setenv("CLOCK_WARN_MS", "garbage")
	if got := clockWarnMs(); got != 30_000 {
		t.Fatalf("malformed CLOCK_WARN_MS must fall back to default, got %d", got)
	}
}

// A drifted feed clock must flow through LogClockHealth without panicking in
// both tiers (line content is journald-verified live; this pins the code path).
func TestLogClockHealthWithInjectedDriftDoesNotPanic(t *testing.T) {
	old := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = old })

	for _, ageMs := range []int64{40_000, 120_000} { // warn tier, critical tier
		market.FuturesBarsProvider = func(string, string, int) []market.Kline {
			return []market.Kline{{OpenTime: time.Now().UnixMilli() - ageMs - 60_000}}
		}
		LogClockHealth("test-fake-drift", "MNQ")
	}
}

// P1.4 — the boot block must read the guard's state JSON and never error when
// it is missing (bot boots identically without the guard installed).
func TestLogClockGuardBootReadsState(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "clock-guard-state.json")
	t.Setenv("NOFX_CLOCK_STATE", state)

	LogClockGuardBoot() // missing file → timer=inactive-or-not-installed, no panic

	if err := os.WriteFile(state, []byte(`{"last_run_utc":"2026-08-19T13:50:34Z","last_run_unix":`+
		i64str(time.Now().Unix()-60)+`,"status":"OK","rtc_vs_wsl_s":"-1","ntp_offset":"+644ms"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	LogClockGuardBoot() // fresh file → timer=active path
}
