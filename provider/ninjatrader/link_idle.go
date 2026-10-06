package ninjatrader

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"vl/kernel"
)

// linkIdleTimeoutSeconds is the B11 (L15) knob: the Go-side read-idle timeout.
// A half-open AddOn link — no inbound frame of ANY kind for this long while the
// CME session is open — is treated as down. Default 60s = 2× the AddOn heartbeat
// interval (TCPHeartbeatInterval 30s, spec L4410), the same margin the heartbeat
// ack timeout uses; the observed healthy open-session max inter-frame gap is the
// heartbeat interval (~30s) — quote: today's log shows the only long gaps are the
// link-down stretches (dead-man watchdog 00:52:37, "feed stale 7m" 01:00:39,
// flip_eval stale_bars age 455s), i.e. the failure this detector exists to catch,
// not a healthy cadence.
func linkIdleTimeoutSeconds() int64 {
	if v := os.Getenv("NT8_LINK_IDLE_SECONDS"); v != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n >= 10 {
			return n
		}
	}
	return 60
}

// Test seams (production defaults). linkIdleNow is the clock; linkIdleTick is
// the watcher cadence.
var (
	linkIdleNow  = time.Now
	linkIdleTick = 5 * time.Second
)

// linkIdleWatcher closes the AddOn connection when no inbound frame has arrived
// for linkIdleTimeoutSeconds() while the CME session is OPEN. It is the Go-side
// read-idle detector for a half-open link — a socket that is not closed, so the
// TCP close path never fires, but nothing flows. Never fires in the CME daily
// break (16:00–17:00 CT) or the weekend — kernel.IsCMEOpen reuses the session
// calendar. Not wg-tracked: exits with ctx, like livenessReporter.
func (s *TCPServer) linkIdleWatcher(ctx context.Context) {
	tick := time.NewTicker(linkIdleTick)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.checkLinkIdle(linkIdleNow())
		}
	}
}

// checkLinkIdle is the one-tick read-idle decision: a frame arrived within the
// timeout, or the CME session is closed → nothing. Otherwise log and close the
// connection — the same state the close path (closeConn) sets.
func (s *TCPServer) checkLinkIdle(now time.Time) {
	last := s.lastFrameUnixMs.Load()
	if last == 0 {
		return // no frame ever — pre-first-connect boot, nothing to judge
	}
	ageMs := now.UnixMilli() - last
	if ageMs <= linkIdleTimeoutSeconds()*1000 {
		return
	}
	if !kernel.IsCMEOpen(now) {
		return // daily break / weekend / holiday — never fire
	}
	s.logger.Warn("tcp_server: link idle — no inbound frame, closing",
		"age", time.Duration(ageMs)*time.Millisecond,
		"timeout_seconds", linkIdleTimeoutSeconds())
	s.closeConn()
}
