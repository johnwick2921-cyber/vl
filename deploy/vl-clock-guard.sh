#!/usr/bin/env bash
# vl-clock-guard.sh — P1 (ledger-close dispatch 2026-08-19): root-free clock
# drift DETECTOR, run every 15 min by the vl-clock-guard user timer.
#
# WHY DETECTOR-ONLY (dispatch 1.2 "use the best available resync and SAY SO"):
# in this WSL2 env hwclock does not exist (verified: not in /usr/sbin, rc=127),
# passwordless sudo is unavailable, and timedatectl set-ntp is polkit-gated —
# there is NO root-free resync. systemd-timesyncd is active and continuously
# slews the clock; the −116s incident class is therefore detected here and
# CORRECTED only by timesyncd/owner action. The owner-side root unit remains
# the escalation path (see the dispatch report).
#
# Measurements (all root-free, proven in recon):
#   rtc      — /sys/class/rtc/rtc0/since_epoch vs date +%s. rtc0 in WSL2 is
#              backed by the Windows host clock → host-vs-WSL drift, 1s grain.
#   ntp      — timedatectl timesync-status Offset (ms, WSL vs NTP server).
#   win      — powershell.exe interop cross-check (ms grain, ~±150ms process
#              launch noise); skipped when powershell.exe is unavailable.
#
# Output: one status line per run on stdout (lands in the user journal:
# journalctl --user -u vl-clock-guard.service) + an atomic state JSON the Go
# bot reads at boot for the P1.4 integrity block.
set -euo pipefail

# Install root is $HOME/vl — never a hardcoded /home/hoang (R5: VL_ names only).
DEFAULT_STATE="$HOME/vl/data/clock-guard-state.json"
STATE="${VL_CLOCK_STATE:-$DEFAULT_STATE}"
WARN_S="${CLOCK_GUARD_WARN_S:-30}"

now_s=$(date +%s)
now_iso=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# (1) host RTC vs WSL clock — the primary host-drift detector.
rtc_drift_s="n/a"
if [ -r /sys/class/rtc/rtc0/since_epoch ]; then
  rtc_s=$(cat /sys/class/rtc/rtc0/since_epoch)
  rtc_drift_s=$((now_s - rtc_s))
fi

# (2) NTP offset per systemd-timesyncd (best-effort).
ntp_offset="n/a"
if command -v timedatectl >/dev/null 2>&1; then
  # The `|| true` is NOT cosmetic: with `set -euo pipefail`, the command
  # substitution inherits the pipeline's exit status — and
  # `timedatectl timesync-status` exits 1 when timesyncd is absent (chrony
  # envs: "org.freedesktop.timesync1 was not provided"). The assignment then
  # fails, set -e kills the WHOLE guard before any output, and the service
  # dies in ~13 ms with no journal line (the 2026-09-02 incident).
  ntp_offset=$(timedatectl timesync-status 2>/dev/null | sed -n 's/^ *Offset: *//p' | head -1 || true)
  [ -n "$ntp_offset" ] || ntp_offset="n/a"
fi

# (3) Windows interop cross-check (ms). Bracket the call to bound launch noise.
win_drift_ms="n/a"
if command -v powershell.exe >/dev/null 2>&1; then
  t0=$(date +%s%3N)
  win_ms=$(powershell.exe -NoProfile -Command "[DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()" 2>/dev/null | tr -d '\r' || true)
  t1=$(date +%s%3N)
  if [[ "$win_ms" =~ ^[0-9]+$ ]]; then
    mid=$(((t0 + t1) / 2))
    win_drift_ms=$((mid - win_ms)) # positive = WSL ahead of Windows
  fi
fi

status="OK"
abs_rtc=${rtc_drift_s#-}
if [[ "$rtc_drift_s" != "n/a" ]] && [ "$abs_rtc" -ge "$WARN_S" ]; then
  status="CRITICAL"
fi

echo "clock-guard status=$status rtc_vs_wsl_s=$rtc_drift_s win_vs_wsl_ms=$win_drift_ms ntp_offset=$ntp_offset resync=unavailable-no-root timesyncd=$(systemctl is-active systemd-timesyncd 2>/dev/null | head -1 || true) warn_s=$WARN_S"
if [ "$status" = "CRITICAL" ]; then
  echo "clock-guard CRITICAL: WSL clock is ${rtc_drift_s}s from the Windows host RTC (|x| >= ${WARN_S}s) — the −116s skew class is BACK. No root-free resync exists here: restart Windows time sync / run the owner-side root resync unit."
fi

# Atomic state write for the Go boot integrity block (P1.4).
mkdir -p "$(dirname "$STATE")"
tmp="$STATE.partial"
cat > "$tmp" <<JSON
{"last_run_utc":"$now_iso","last_run_unix":$now_s,"status":"$status","rtc_vs_wsl_s":"$rtc_drift_s","win_vs_wsl_ms":"$win_drift_ms","ntp_offset":"$ntp_offset","resync":"unavailable-no-root","warn_s":$WARN_S}
JSON
mv "$tmp" "$STATE"
