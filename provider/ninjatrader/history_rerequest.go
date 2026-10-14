// ── 101 D1'(3a) [O "I want full data", 2026-09-16] — ask NT8 again ──────────
//
// After a CONFIRMED scale break the ring has dropped its replay seed for that
// key. The store rehydrate (3b) can only give back what the store holds on the
// current contract, and for higher timeframes on a young contract that is
// little (MNQ 12-26 at this boot: 5m 585, 15m 205, 1h 59, 4h 20). NT8 holds
// the full window locally (db\minute\MNQ 12-26: 83 day-files) and the AddOn's
// N4 path DISPOSES + RECREATES its BarsRequest on a repeat bars_subscribe — so
// a Go-side re-send of the subscribe frame IS the re-request, and it comes
// back as bars_historical with ≈bars_back per tf. No AddOn change.
//
// Two conditions the 09-16 morning taught: never while the feed is down (a
// BarsRequest run then returns bars=0 — five times that morning), and never
// more than once per window per symbol (a flapping feed must not storm NT8).
package ninjatrader

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// historyReplayMaxPerBoot bounds the AUTOMATIC re-request to ONE per symbol
// per process (vl-93's objection 2, 2026-09-16). A time floor turned a TRUE
// break into a loop: mismatch → drop (which under guard (ii) includes the
// store-backed rows) → post-drop rehydrate restores them as historical →
// re-request → NT8's replay lands on the wrong scale again and, historical
// over historical, mergeSeedKeepingLive lets the INCOMING bar win → the ring
// is off-scale until the next live bar of that tf judges it (a full bar
// duration on a slow ring) → drop → again. The 09-16 false-positive shape
// needs exactly one re-request; a SECOND break on the same symbol after one
// means the replay is on another contract, and the AddOn restart is the fix.
// Bounded and loud beats bounded and looping.
const historyReplayMaxPerBoot = 1

type historyReplayState struct {
	mu   sync.Mutex
	last map[string]time.Time // symbol -> last re-request sent
	sent map[string]int       // symbol -> count since boot (READ by the summary line)
}

// ErrHistoryReplaySpent is returned when the per-boot budget is used: the
// caller's P0 line names it, because a second break after a re-request is a
// diagnosis, not a retry.
var ErrHistoryReplaySpent = fmt.Errorf("history replay budget spent for this boot (%d/%d): a second scale break after a re-request means the replay is on ANOTHER CONTRACT than the platform — the AddOn restart is the fix, not another request", historyReplayMaxPerBoot, historyReplayMaxPerBoot)

// RequestHistoryReplayAt re-sends the bars_subscribe frame for symbol —
// trading or extra — so the AddOn rebuilds its BarsRequest and replays the
// full bars_back window. `now` is the caller's clock (A28).
func (s *TCPServer) RequestHistoryReplayAt(symbol string, now time.Time) error {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return fmt.Errorf("tcp_server: history replay: empty symbol")
	}
	if !s.IsFeedConnected() {
		return fmt.Errorf("tcp_server: history replay %s: feed not connected — a BarsRequest run now returns bars=0 (09-16 shape); deferred", symbol)
	}
	s.histReplay.mu.Lock()
	if s.histReplay.last == nil {
		s.histReplay.last = map[string]time.Time{}
		s.histReplay.sent = map[string]int{}
	}
	if s.histReplay.sent[symbol] >= historyReplayMaxPerBoot {
		s.histReplay.mu.Unlock()
		return fmt.Errorf("tcp_server: history replay %s: %w", symbol, ErrHistoryReplaySpent)
	}
	s.histReplay.last[symbol] = now
	s.histReplay.sent[symbol]++
	s.histReplay.mu.Unlock()

	payload, err := s.resendBarsSubscribe(symbol)
	if err != nil {
		return fmt.Errorf("tcp_server: history replay %s: %w", symbol, err)
	}
	if s.logger != nil {
		s.logger.Info("tcp_server: history replay re-requested after a confirmed scale break", "symbol", symbol,
			"timeframes", payload.Timeframes, "bars_back", payload.BarsBack, "sent_since_boot", s.histReplay.sent[symbol])
	}
	return nil
}

// resendBarsSubscribe writes the symbol's bars_subscribe frame — the
// subscription's own timeframes and bars_back — on the live connection. It is
// THE re-request: the AddOn's N4 path disposes + recreates every BarsRequest
// for the symbol with its emit cursor at 0, so NT8 replays ≈bars_back per tf
// from its local DB. Shared by the scale-break re-request above and the
// gap-backfill re-request (history_gap_backfill.go); each keeps its own budget.
func (s *TCPServer) resendBarsSubscribe(symbol string) (BarsSubscribePayload, error) {
	s.barsSubMu.RLock()
	payload := BarsSubscribePayload{
		Symbol:     symbol,
		Timeframes: append([]string(nil), s.barsSubscribe.Timeframes...),
		BarsBack:   s.barsSubscribe.BarsBack,
	}
	s.barsSubMu.RUnlock()

	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c == nil {
		return payload, fmt.Errorf("not connected")
	}
	s.writeMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := WriteFrame(c, FrameBarsSubscribe, payload)
	s.writeMu.Unlock()
	if err != nil {
		return payload, fmt.Errorf("send bars_subscribe: %w", err)
	}
	return payload, nil
}

// HistoryReplaysSent is READ by the summary line (A11).
func (s *TCPServer) HistoryReplaysSent(symbol string) int {
	s.histReplay.mu.Lock()
	defer s.histReplay.mu.Unlock()
	return s.histReplay.sent[symbol]
}

// markFeedConnectedForTest sets the feed status a fixture needs.
func (s *TCPServer) markFeedConnectedForTest(up bool) {
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	if up {
		s.feedStatus = "Connected"
	} else {
		s.feedStatus = "ConnectionLost"
	}
}
