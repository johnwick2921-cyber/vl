package kernel

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"vl/logger"
	"vl/market"
)

// PHASE 3.5 (timegate audit 2026-08-18) — CLOCK HEALTH, log-only.
//
// The C2 clock-drift guard already protects signals (feed-stamped since the
// WSL↔NT8 skew finding; thresholds untouched here by owner decision). What was
// missing is OBSERVABILITY: at boot and at each session roll, one line stating
// what the Go clock, the NT8 feed clock, and the OS time-sync layer each
// believe — so the next skew is a grep, not a forensic hunt. On tolerance
// breach it logs CRITICAL and nothing else: no new trade-blocking gate
// (guardrails-master decision), the existing C2 behavior is unchanged.

// C2ToleranceMs exposes the EXISTING C2 tolerance for log-only consumers.
// Additive accessor; the guard's own threshold is untouched.
func C2ToleranceMs() int64 { return clockDriftToleranceMs }

// ClockWarnMs exposes the P1.3 early-warning threshold (F6 consumers need the
// same band the clock-health logger uses).
func ClockWarnMs() int64 { return clockWarnMs() }

// clockWarnMs is the P1.3 early-warning threshold (ledger-close 2026-08-19):
// CRITICAL fires at HALF the C2 tolerance so the operator hears about drift
// BEFORE staleness verdicts and clock-health truth degrade. Env CLOCK_WARN_MS,
// default 30000 (50% of C2's 60s) — same env pattern as STALE_BAR_GRACE_S.
func clockWarnMs() int64 {
	if v := os.Getenv("CLOCK_WARN_MS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 30_000
}

// classifyClockDrift is the pure decision (test hook per dispatch P1: injected
// fake drift, never the real clock): "" below warn, "warn" in [warn, tolerance],
// "critical" above tolerance.
func classifyClockDrift(absDriftMs, warnMs, toleranceMs int64) string {
	switch {
	case absDriftMs > toleranceMs:
		return "critical"
	case absDriftMs >= warnMs:
		return "warn"
	default:
		return ""
	}
}

// timesyncStatus reports systemd-timesyncd's view, best-effort with a hard
// 2s ceiling ("unknown" on any failure — WSL2 setups vary).
func timesyncStatus() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "timedatectl", "show",
		"-p", "NTPSynchronized", "-p", "NTP").Output()
	if err != nil {
		return "unknown"
	}
	return strings.ReplaceAll(strings.TrimSpace(string(out)), "\n", " ")
}

// LogClockHealth emits the clock-health line for one symbol's feed. tag names
// the trigger ("boot" | "session-roll:<name>").
func LogClockHealth(tag, symbol string) {
	now := time.Now()
	// One labelled dual read, via the sanctioned tz helpers (the TZ guard test
	// rejects bare layouts — deservedly).
	line := "🕰 clock-health [" + tag + "] go=" + ClockCTAndUTC(now)

	drift := int64(0)
	haveBar := false
	if market.FuturesBarsProvider != nil {
		if bars := market.FuturesBarsProvider(symbol, "1m", 3); len(bars) > 0 {
			last := bars[len(bars)-1]
			// Roll-safe: a FORMING bar measures against its EmittedAt (freshest
			// evidence), never the boundary which grows 0..60 s across the minute.
			var ref string
			drift, ref = rollSafeClockDriftMs(now.UnixMilli(), last.OpenTime, last.EmittedAt)
			nt8Ms := now.UnixMilli() - drift // the reference the drift was measured against
			haveBar = true
			line += " nt8_last_bar=" + ClockCTAndUTC(time.UnixMilli(nt8Ms)) +
				" drift_ms=" + i64str(drift) + " ref=" + ref
		}
	}
	if !haveBar {
		line += " nt8_last_bar=none drift_ms=n/a"
	}
	// F6 (2026-08-30) — persist this measurement so the authoring gate and the
	// news-window widening read the freshest clock truth without re-deriving it.
	RecordClockDrift(drift, haveBar)
	line += " timesync{" + timesyncStatus() + "} tolerance_ms=" + i64str(C2ToleranceMs())
	logger.Infof("%s", line)

	// A stale-but-live feed inflates "drift" honestly (the bar IS old); the
	// CRITICAL only means "Go clock vs newest feed evidence disagree beyond C2
	// tolerance" — exactly what the operator must look at either way.
	// P1.3 (ledger-close 2026-08-19): an EARLY-WARNING tier fires at
	// CLOCK_WARN_MS (default 30s = 50% of tolerance) so drift is heard BEFORE
	// truth degrades. Both tiers stay log-only (no trading gate).
	if haveBar {
		switch classifyClockDrift(absI64(drift), clockWarnMs(), C2ToleranceMs()) {
		case "critical":
			logger.Errorf("🚨 CLOCK CRITICAL [%s]: |drift| %dms exceeds C2 tolerance %dms — check WSL2 time-sync (systemd-timesyncd) and the NT8 feed. Log-only: no trading gate added.",
				tag, absI64(drift), C2ToleranceMs())
		case "warn":
			logger.Errorf("🚨 CLOCK EARLY-WARNING [%s]: |drift| %dms exceeds CLOCK_WARN_MS %dms (tolerance %dms not yet breached) — fix WSL2 time-sync NOW, before staleness verdicts degrade. Log-only.",
				tag, absI64(drift), clockWarnMs(), C2ToleranceMs())
		}
	}
}

// clockGuardState mirrors the JSON written by deploy/vl-clock-guard.sh.
type clockGuardState struct {
	LastRunUTC  string `json:"last_run_utc"`
	LastRunUnix int64  `json:"last_run_unix"`
	Status      string `json:"status"`
	RTCvsWSLs   string `json:"rtc_vs_wsl_s"`
	NTPOffset   string `json:"ntp_offset"`
}

// LogClockGuardBoot is the P1.4 boot integrity extension: one line stating the
// live host-RTC drift, whether the clock-guard timer is running, and its last
// check. Everything is best-effort — a missing guard reads as timer=inactive,
// never an error (the bot must boot identically without it).
func LogClockGuardBoot() {
	// Live drift, same channel as the guard script: host RTC vs Go clock.
	rtcDrift := "n/a"
	if raw, err := os.ReadFile("/sys/class/rtc/rtc0/since_epoch"); err == nil {
		if rtcS, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64); err == nil {
			rtcDrift = i64str(time.Now().Unix()-rtcS) + "s"
		}
	}

	// Guard timer state, judged by state-file freshness (the bot runs as a
	// SYSTEM service and cannot reliably reach the user systemd manager, so the
	// file the 15-min timer writes is the honest signal: fresh = active).
	statePath := os.Getenv("VL_CLOCK_STATE") // R5: VL_ only
	if statePath == "" {
		statePath = "data/clock-guard-state.json"
	}
	timer, lastCheck, lastStatus := "inactive-or-not-installed", "never", ""
	if raw, err := os.ReadFile(statePath); err == nil {
		var st clockGuardState
		if json.Unmarshal(raw, &st) == nil && st.LastRunUnix > 0 {
			age := time.Since(time.Unix(st.LastRunUnix, 0))
			lastCheck = st.LastRunUTC + " (" + age.Round(time.Second).String() + " ago)"
			lastStatus = " last_status=" + st.Status + " rtc_vs_wsl_s=" + st.RTCvsWSLs + " ntp_offset=" + st.NTPOffset
			if age <= 20*time.Minute { // 15-min cadence + 5-min slack
				timer = "active"
			} else {
				timer = "stale"
			}
		}
	}
	logger.Infof("🛡 clock-guard [boot] rtc_vs_go=%s timer=%s last_check=%s%s warn_ms=%d tolerance_ms=%d resync=unavailable-no-root (timesyncd slews; owner root unit is the escalation path)",
		rtcDrift, timer, lastCheck, lastStatus, clockWarnMs(), C2ToleranceMs())
}

func i64str(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
