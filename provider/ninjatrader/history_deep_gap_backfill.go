package ninjatrader

import (
	"fmt"
	"strings"
	"time"
)

// ── DEEP GAP BACKFILL (m444 review round 2) ─────────────────────────────────
//
// The standard gap re-request resends the subscription's own bars_subscribe:
// NT8 replays the last bars_back (2000) 1m bars. A hole older than that was
// RETIRED by the bar-hole tracker and the owner was told "the bot cannot refill
// it" — false: the bot already has a deeper path (RequestDeepBarsBackfill, the
// bar-truth arbiter's), and the 10-26 MNQ hole sat only ~2800 open minutes back.
// 17:00–21:56 may well exist in NT8 beyond the 2000-bar window every subscribe
// uses (NT8 served MNQ 1m from 21:57 later; the 21:57 start is exactly the
// bars_back=2000 edge of the 10-28 08:17 replay).
//
// This is that deep ask, for the hole tracker: ONE timeframe (1m), an enlarged
// bars_back sized to the hole, on the SAME admission rule and budget as every
// gap re-request (link + feed up, 60 s spacing, 40/day/symbol, overlap verdict
// armed before the send). The AddOn disposes + recreates only the named
// timeframe's BarsRequest (3m/5m keep streaming); the next reconnect restores
// the standard depth. Unlike the arbiter's call it does NOT open a bar-truth
// capture — that state belongs to the owner's diff action.

// DeepGapBackfillMaxBarsBack bounds a deep ask: 4800 1m bars, about 3.5 CME
// sessions. NOT the arbiter's 8640: every replay row passes the replay-hold
// (trader/ninjatrader/replay_hold.go), which keeps only the NEWEST
// 2*DefaultBarCacheMaxBars (5000) rows per key — so an 8640-bar replay would
// lose its OLDEST 3640 rows (exactly the part a deep hole sits in), the measure
// would find the hole still open, and the owner would be told "NT8 has no
// history" for rows the bot itself threw away. A compile-time guard in
// trader/ninjatrader keeps this below the hold's cap. A hole needing more is
// beyond what the bot asks NT8 for and stays listed as retired.
const DeepGapBackfillMaxBarsBack = 4800

// RequestDeepGapBackfillAt asks NT8 for `barsBack` 1m bars of `symbol` (clamped
// to [1, DeepGapBackfillMaxBarsBack]). `now` is the caller's clock (A28).
func (s *TCPServer) RequestDeepGapBackfillAt(symbol string, barsBack int, now time.Time) error {
	symbol = strings.TrimSpace(symbol)
	if barsBack <= 0 {
		return fmt.Errorf("tcp_server: deep gap backfill %s: bars_back %d", symbol, barsBack)
	}
	if barsBack > DeepGapBackfillMaxBarsBack {
		barsBack = DeepGapBackfillMaxBarsBack
	}
	sentToday, err := s.admitGapBackfill(symbol, now)
	if err != nil {
		return err
	}
	payload := BarsSubscribePayload{Symbol: symbol, Timeframes: []string{"1m"}, BarsBack: barsBack}
	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c == nil {
		return fmt.Errorf("tcp_server: deep gap backfill %s: not connected", symbol)
	}
	s.writeMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err = WriteFrame(c, FrameBarsSubscribe, payload)
	s.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("tcp_server: deep gap backfill %s: send bars_subscribe: %w", symbol, err)
	}
	s.barCache.NoteReconnect(time.Now()) // W26: the AddOn rebuilds the 1m BarsRequest
	if s.logger != nil {
		s.logger.Info("tcp_server: DEEP gap backfill re-requested 1m history (bars_subscribe for 1m only, enlarged bars_back)",
			"symbol", symbol, "timeframes", payload.Timeframes, "bars_back", payload.BarsBack,
			"sent_this_session_day", sentToday, "day_cap", gapBackfillMaxPerDay)
	}
	return nil
}
