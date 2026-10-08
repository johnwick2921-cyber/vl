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

// TestRollSafeClockDriftMs is the CLOCK-HEALTH-ROLL decision pin: the roll at
// boundary+5s must read ~5s (no WARN) even when the newest bar is the in-flight
// FORMING one, a 10-min-old bar must still be CRITICAL, and a mid-minute feed
// whose freshest closed bar is 40s old must still fire EARLY-WARNING.
//
// Named RED (CLOCK-HEALTH-FORMING-BAR): with newestOpen == expectedOpen, the
// #456 version measured now − boundary (which grows 0..60 s across the minute,
// so the 17:00:58 roll read 58314 ms and WARNed). This version measures against
// the forming bar's EmittedAt, so a fresh forming bar at boundary+58s reads ~1s
// → no WARN.
func TestRollSafeClockDriftMs(t *testing.T) {
	const boundary = int64(1_700_000_040_000) // minute-aligned (× 60_000)
	const warn, tol = int64(30_000), int64(60_000)

	cases := []struct {
		name       string
		nowMs      int64
		newestOpen int64
		emittedAt  int64
		wantDrift  int64
		wantRef    string
		wantClass  string
	}{
		{
			"roll at boundary+5s, just-closed bar newest",
			boundary + 5_000, boundary - 60_000, 0,
			+5_000, "closed_close", "",
		},
		{
			"roll at boundary+58s, forming bar with fresh EmittedAt (the #456 false alarm)",
			boundary + 58_000, boundary, boundary + 57_000,
			+1_000, "forming.EmittedAt", "",
		},
		{
			"forming bar with no EmittedAt → 0, no tick time",
			boundary + 58_000, boundary, 0,
			0, "forming.no_tick_time", "",
		},
		{
			"10-min-old newest bar still fires CRITICAL",
			boundary + 40_000, boundary - 600_000, 0,
			boundary + 40_000 - (boundary - 600_000 + 60_000), "closed_close", "critical",
		},
		{
			"40s-late feed mid-minute still fires EARLY-WARNING",
			boundary + 40_000, boundary - 60_000, 0,
			+40_000, "closed_close", "warn",
		},
		{
			// A local clock BEHIND the feed (newest bar open in our future) must
			// alarm, never be clamped away.
			"local clock ~3min behind the feed still fires CRITICAL",
			boundary + 20_000, boundary + 180_000, 0,
			-160_000, "future_open", "critical",
		},
		{
			"local clock 40s behind the feed still fires EARLY-WARNING",
			boundary + 20_000, boundary + 60_000, 0,
			-40_000, "future_open", "warn",
		},
	}
	for _, c := range cases {
		got, ref := rollSafeClockDriftMs(c.nowMs, c.newestOpen, c.emittedAt)
		if got != c.wantDrift {
			t.Errorf("%s: drift = %d, want %d", c.name, got, c.wantDrift)
		}
		if ref != c.wantRef {
			t.Errorf("%s: ref = %q, want %q", c.name, ref, c.wantRef)
		}
		if cls := classifyClockDrift(absI64(got), warn, tol); cls != c.wantClass {
			t.Errorf("%s: class = %q, want %q (drift %d)", c.name, cls, c.wantClass, got)
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

	// The roll-race case this wave fixes: the newest bar is the FORMING one
	// (open == the current minute boundary) with a fresh EmittedAt — must not
	// panic and must not be misclassified as a false WARN (the pure pin above
	// asserts the value).
	market.FuturesBarsProvider = func(string, string, int) []market.Kline {
		nowMs := time.Now().UnixMilli()
		return []market.Kline{{OpenTime: (nowMs / 60_000) * 60_000, EmittedAt: nowMs - 1_000}}
	}
	LogClockHealth("test-roll-forming", "MNQ")
}

// P1.4 — the boot block must read the guard's state JSON and never error when
// it is missing (bot boots identically without the guard installed).
func TestLogClockGuardBootReadsState(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "clock-guard-state.json")
	t.Setenv("VL_CLOCK_STATE", state)

	LogClockGuardBoot() // missing file → timer=inactive-or-not-installed, no panic

	if err := os.WriteFile(state, []byte(`{"last_run_utc":"2026-08-19T13:50:34Z","last_run_unix":`+
		i64str(time.Now().Unix()-60)+`,"status":"OK","rtc_vs_wsl_s":"-1","ntp_offset":"+644ms"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	LogClockGuardBoot() // fresh file → timer=active path
}
