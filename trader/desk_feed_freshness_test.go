package trader

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"vl/market"
	ntwire "vl/provider/ninjatrader"
	ntTrader "vl/trader/ninjatrader"
)

// TestDeskFeedSurfacesBrokerMode pins the production deskFeed call site over the
// REAL wire path: a feed_status frame carrying mode=delayed drives HealthFeedMode
// → deskFeed, so the FEED row goes stale with the mode named; an omitted mode
// stays UNKNOWN (never realtime), and a stale bar under an UNKNOWN mode still
// renders actionable market-data guidance that names the NT8 data connection.
func TestDeskFeedSurfacesBrokerMode(t *testing.T) {
	srv := ntwire.NewTCPServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.SetAddrForTest("127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("tcp server Start: %v", err)
	}
	defer srv.Stop()

	client, err := net.Dial("tcp", srv.ListenAddrForTest().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	waitAddonRegistered(t, srv)

	at := NewAutoTraderOnBrokerForTest("t-feed", "ninjatrader", ntTrader.NewTCPTrader(srv, "MNQ", "Sim101"), true)

	prevBars := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = prevBars })
	barTime := time.Now().UnixMilli() - 3_000
	market.FuturesBarsProvider = func(symbol, timeframe string, count int) []market.Kline {
		return []market.Kline{{OpenTime: barTime}}
	}

	sendStatus := func(mode string) {
		t.Helper()
		if err := ntwire.WriteFrame(client, ntwire.FrameFeedStatus, ntwire.FeedStatusPayload{
			PriceStatus: "Connected",
			Time:        time.Now().UTC().Format(time.RFC3339),
			Mode:        mode,
		}); err != nil {
			t.Fatal(err)
		}
		waitFeedModeAt(t, at, mode)
	}

	t.Run("delayed", func(t *testing.T) {
		barTime = time.Now().UnixMilli() - 3_000 // fresh bar: the mode leg alone must fail it
		sendStatus("delayed")
		line := at.deskFeed(time.Now())
		if line.State != "stale" {
			t.Fatalf("delayed mode: state = %q, want stale (reason %q)", line.State, line.Reason)
		}
		if !strings.Contains(line.Text, "mode DELAYED") {
			t.Fatalf("delayed mode: Text %q must name mode DELAYED", line.Text)
		}
		if !strings.Contains(line.Reason, "broker feed mode is DELAYED") {
			t.Fatalf("delayed mode: Reason %q must name the delayed broker mode", line.Reason)
		}
	})

	t.Run("unknown fresh", func(t *testing.T) {
		barTime = time.Now().UnixMilli() - 3_000
		sendStatus("") // omitted mode → UNKNOWN, never fabricated as realtime
		line := at.deskFeed(time.Now())
		if line.State != "ok" {
			t.Fatalf("unknown mode + fresh bar: state = %q, want ok (reason %q)", line.State, line.Reason)
		}
		if !strings.Contains(line.Text, "mode unknown") {
			t.Fatalf("unknown mode + fresh bar: Text %q must name mode unknown", line.Text)
		}
	})

	t.Run("unknown stale", func(t *testing.T) {
		barTime = time.Now().UnixMilli() - 130_000 // >2m: stale even with mode unknown
		sendStatus("")
		line := at.deskFeed(time.Now())
		if line.State != "stale" {
			t.Fatalf("unknown mode + stale bar: state = %q, want stale", line.State)
		}
		if !strings.Contains(line.Reason, "the newest 1m bar is older than 2 minutes — check the NT8 data connection") {
			t.Fatalf("unknown mode + stale bar: Reason %q must carry actionable NT8 data-connection guidance, not just the age", line.Reason)
		}
	})
}

func waitFeedModeAt(t *testing.T, at *AutoTrader, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if at.HealthFeedMode() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("HealthFeedMode = %q, want %q", at.HealthFeedMode(), want)
}
