// ── GAP-BACKFILL (2026-10-14) — ask NT8 again after a feed gap ─────────────
//
// 03:44:04 CT 10-14 NinjaTrader lost Tradovate and its Historical Data Server;
// live bars resumed at 07:48 and the store held a 244-minute 1m HOLE. The
// AddOn's own reconnect rebuild answered bars=0/1 for every timeframe (NT8's
// DB did not have the gap yet), the owner's 08:16 "Reload All Historical Data"
// put it into NT8 — and NOTHING on the Go side asked again. The hole filled at
// 08:22 only because the release #22 boot re-sent bars_subscribe.
//
// This is that re-send, on demand, for the bar-hole tracker
// (trader/ninjatrader/bar_holes.go). It is the SAME frame and the SAME AddOn
// path as the scale-break re-request (RequestHistoryReplayAt → N4 dispose +
// recreate, emit cursor 0, ≈bars_back per tf) — the path that healed the hole
// at 08:22 — on a budget of its OWN: spending a gap re-request never touches
// the scale-break one-per-boot guard, and a scale-break re-request never eats
// the gap budget.
package ninjatrader

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"vl/kernel"
)

// gapBackfillMaxPerDay bounds the gap re-requests per symbol per CME
// session-day. A hole gets four scheduled attempts and then one slow attempt
// per 15 min; several holes on one symbol share each send. 40 covers a bad day
// and still cannot storm NT8 (each send is a full ≈bars_back replay per tf).
const gapBackfillMaxPerDay = 40

// gapBackfillMinSpacing is the floor between two gap re-requests for one
// symbol, whatever asks. A replay of every timeframe takes seconds to land;
// asking again inside a minute only stacks rebuilds on the AddOn.
const gapBackfillMinSpacing = 60 * time.Second

// Refusal reasons — each is a distinct, named error so the tracker's WARN and
// counter say WHICH guard refused (A9 / L7: never silent, never vague).
var (
	ErrGapBackfillFeedDown = errors.New("feed not connected — a BarsRequest run during a feed outage returns bars=0 (09-16 and 10-14 shape)")
	ErrGapBackfillTooSoon  = fmt.Errorf("a gap re-request for this symbol went out less than %s ago", gapBackfillMinSpacing)
	ErrGapBackfillSpent    = fmt.Errorf("gap re-request budget spent for this CME session-day (%d/day per symbol) — a hole still open now needs the owner's NT8 history reload; the budget resets at the 17:00 CT roll", gapBackfillMaxPerDay)
)

type gapBackfillState struct {
	mu    sync.Mutex
	last  map[string]time.Time // symbol -> last gap re-request sent
	day   map[string]string    // symbol -> CME session-day key of `count`
	count map[string]int       // symbol -> sends in that session-day
	total map[string]int       // symbol -> sends since boot (READ by health)
}

// RequestGapBackfillAt re-sends the bars_subscribe frame for symbol so the
// AddOn rebuilds its BarsRequests and replays ≈bars_back per tf — the gap
// backfill. `now` is the caller's clock (A28). Refused, with a named reason,
// while the link or the feed is down, inside the per-symbol spacing, or once
// the per-day budget is spent.
func (s *TCPServer) RequestGapBackfillAt(symbol string, now time.Time) error {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return fmt.Errorf("tcp_server: gap backfill: empty symbol")
	}
	if !s.IsConnected() {
		return fmt.Errorf("tcp_server: gap backfill %s: no NT8 client connected", symbol)
	}
	if !s.IsFeedConnected() {
		return fmt.Errorf("tcp_server: gap backfill %s: %w", symbol, ErrGapBackfillFeedDown)
	}
	day := kernel.CMESessionDayKey(now)
	s.histReplay.gap.mu.Lock()
	if s.histReplay.gap.last == nil {
		s.histReplay.gap.last = map[string]time.Time{}
		s.histReplay.gap.day = map[string]string{}
		s.histReplay.gap.count = map[string]int{}
		s.histReplay.gap.total = map[string]int{}
	}
	if last, ok := s.histReplay.gap.last[symbol]; ok && now.Sub(last) < gapBackfillMinSpacing {
		s.histReplay.gap.mu.Unlock()
		return fmt.Errorf("tcp_server: gap backfill %s: %w", symbol, ErrGapBackfillTooSoon)
	}
	if s.histReplay.gap.day[symbol] != day {
		s.histReplay.gap.day[symbol] = day
		s.histReplay.gap.count[symbol] = 0
	}
	if s.histReplay.gap.count[symbol] >= gapBackfillMaxPerDay {
		s.histReplay.gap.mu.Unlock()
		return fmt.Errorf("tcp_server: gap backfill %s: %w", symbol, ErrGapBackfillSpent)
	}
	s.histReplay.gap.last[symbol] = now
	s.histReplay.gap.count[symbol]++
	s.histReplay.gap.total[symbol]++
	sentToday := s.histReplay.gap.count[symbol]
	s.histReplay.gap.mu.Unlock()

	payload, err := s.resendBarsSubscribe(symbol)
	if err != nil {
		return fmt.Errorf("tcp_server: gap backfill %s: %w", symbol, err)
	}
	if s.logger != nil {
		s.logger.Info("tcp_server: gap backfill re-requested history (bars_subscribe resent; the AddOn rebuilds its BarsRequests)",
			"symbol", symbol, "timeframes", payload.Timeframes, "bars_back", payload.BarsBack,
			"sent_this_session_day", sentToday, "day_cap", gapBackfillMaxPerDay)
	}
	return nil
}

// GapBackfillsSent is READ by the health surface (A11): gap re-requests sent
// for symbol since boot. Separate from HistoryReplaysSent by construction.
func (s *TCPServer) GapBackfillsSent(symbol string) int {
	s.histReplay.gap.mu.Lock()
	defer s.histReplay.gap.mu.Unlock()
	return s.histReplay.gap.total[symbol]
}

// GapBackfillDayCap is READ by the boot line.
func GapBackfillDayCap() int { return gapBackfillMaxPerDay }

// AgeLiveFeedEvidenceForTest backdates the live-bar freshness stamp that lets
// IsFeedConnected override a stale feed_status, so a production-call-site test
// can hold the feed DOWN after it has streamed bars. TEST-ONLY — never called
// in production; mirrors SetListeningSinceForTest.
func (s *TCPServer) AgeLiveFeedEvidenceForTest(d time.Duration) {
	if last := s.lastBarNano.Load(); last > 0 {
		s.lastBarNano.Store(last - d.Nanoseconds())
	}
}
