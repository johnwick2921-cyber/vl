package ninjatrader

import (
	"math"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
)

// W1b E12(a) — THE WIRE NEVER ROUNDS A STOP TOWARD THE ENTRY.
//
// RoundToTick is math.Round: side-blind nearest-tick. A stop composed exactly
// on the min-SL floor (entry − 1.5×ATR5m, off the tick grid) rounded to the
// nearest tick lands up to half a tick INSIDE the floor the gate approved:
// long entry 29600.00, stop 29575.90 (distance 24.10) went out as 29576.00
// (distance 24.00). The stop must round AWAY from the entry (long: down,
// short: up); the target keeps nearest-tick rounding.
//
// Production call sites: the three entry functions, each over a real loopback
// frame (stopEntryServer), never a re-built payload.

type wireStopCase struct {
	name     string
	side     string
	entry    float64
	sl, tp   float64
	wantSL   float64
	wantTP   float64
	judgedSL float64 // the distance the gate approved (entry − sl, mirrored for a short)
}

var wireStopCases = []wireStopCase{
	// long: 29575.90 is 0.6 of a tick above 29575.75 — nearest goes UP (toward entry).
	{name: "long-floor-stop", side: "long", entry: 29600.00, sl: 29575.90, tp: 29648.30, wantSL: 29575.75, wantTP: 29648.25, judgedSL: 24.10},
	// short: 29624.10 is 0.4 of a tick above 29624.00 — nearest goes DOWN (toward entry).
	{name: "short-floor-stop", side: "short", entry: 29600.00, sl: 29624.10, tp: 29551.70, wantSL: 29624.25, wantTP: 29551.75, judgedSL: 24.10},
	// on-grid stops are untouched (byte-identical to the old nearest rounding).
	{name: "long-on-grid", side: "long", entry: 29600.00, sl: 29575.75, tp: 29650.00, wantSL: 29575.75, wantTP: 29650.00, judgedSL: 24.25},
	{name: "short-on-grid", side: "short", entry: 29600.00, sl: 29624.25, tp: 29550.00, wantSL: 29624.25, wantTP: 29550.00, judgedSL: 24.25},
}

func awaitFrame(t *testing.T, frames chan ntwire.SignalPayload, what string) ntwire.SignalPayload {
	t.Helper()
	select {
	case p := <-frames:
		return p
	case <-time.After(3 * time.Second):
		t.Fatalf("%s: no signal frame arrived on the loopback", what)
	}
	return ntwire.SignalPayload{}
}

func assertWireStop(t *testing.T, what string, c wireStopCase, p ntwire.SignalPayload) {
	t.Helper()
	if p.StopLoss != c.wantSL {
		t.Errorf("%s: wire stop_loss=%.2f, want %.2f (rounded AWAY from the entry)", what, p.StopLoss, c.wantSL)
	}
	if p.TakeProfit != c.wantTP {
		t.Errorf("%s: wire take_profit=%.2f, want %.2f (target keeps nearest-tick rounding)", what, p.TakeProfit, c.wantTP)
	}
	onGrid := math.Abs(p.StopLoss/0.25-math.Round(p.StopLoss/0.25)) < 1e-9
	if !onGrid {
		t.Errorf("%s: wire stop %.4f is off the 0.25 grid", what, p.StopLoss)
	}
	dist := c.entry - p.StopLoss
	if c.side == "short" {
		dist = p.StopLoss - c.entry
	}
	if dist+1e-9 < c.judgedSL {
		t.Errorf("%s: broker stop distance %.2f is INSIDE the distance the gate approved (%.2f)", what, dist, c.judgedSL)
	}
}

func TestWireStopRoundsAwayFromEntryLimit(t *testing.T) {
	for _, c := range wireStopCases {
		t.Run(c.name, func(t *testing.T) {
			s, _, _, frames := stopEntryServer(t)
			tr := NewTCPTrader(s, "MNQ", "Sim101")
			var err error
			for i := 0; i < 50; i++ {
				_, err = tr.PlaceLimitEntry("MNQ", c.side, 1, c.entry, c.sl, c.tp)
				if err == nil || !strings.Contains(err.Error(), "no NT client connected") {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err != nil {
				t.Fatalf("PlaceLimitEntry: %v", err)
			}
			assertWireStop(t, "limit "+c.name, c, awaitFrame(t, frames, c.name))
		})
	}
}

func TestWireStopRoundsAwayFromEntryStopEntry(t *testing.T) {
	for _, c := range wireStopCases {
		t.Run(c.name, func(t *testing.T) {
			s, _, conn, frames := stopEntryServer(t)
			if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: ntwire.MinAddonBuildStopSlot}); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 100 && !ntwire.FarSideProven(s.FarSideBuildID(), ntwire.MinAddonBuildStopSlot); i++ {
				time.Sleep(10 * time.Millisecond)
			}
			tr := NewTCPTrader(s, "MNQ", "Sim101")
			if _, err := tr.PlaceStopEntry("MNQ", c.side, 1, c.entry, c.sl, c.tp, 0, 0); err != nil {
				t.Fatalf("PlaceStopEntry: %v", err)
			}
			assertWireStop(t, "stop-entry "+c.name, c, awaitFrame(t, frames, c.name))
		})
	}
}

func TestWireStopRoundsAwayFromEntryMarket(t *testing.T) {
	for _, c := range wireStopCases {
		t.Run(c.name, func(t *testing.T) {
			s, _, _, frames := stopEntryServer(t)
			tr := NewTCPTrader(s, "MNQ", "Sim101")
			_ = tr.SetStopLoss("MNQ", c.side, 1, c.sl)
			_ = tr.SetTakeProfit("MNQ", c.side, 1, c.tp)
			open := tr.OpenLong
			if c.side == "short" {
				open = tr.OpenShort
			}
			if _, err := open("MNQ", 1, 1); err != nil {
				t.Fatalf("open %s: %v", c.side, err)
			}
			p := awaitFrame(t, frames, c.name)
			// The market frame's entry is a midpoint reference, not the fill; the
			// stop/target rounding is what the broker brackets with.
			if p.StopLoss != c.wantSL || p.TakeProfit != c.wantTP {
				t.Errorf("market %s: wire SL %.2f TP %.2f, want SL %.2f TP %.2f", c.name, p.StopLoss, p.TakeProfit, c.wantSL, c.wantTP)
			}
		})
	}
}

// TestWireStopTable pins the helper the wire and the gate share: away-from-entry
// for off-grid stops, and byte-identity with RoundToTick for on-grid ones —
// including a 0.10-tick instrument whose on-grid values are not exact in binary
// (2000.3/0.1 = 20002.999999999996; a bare Floor would move it a whole tick).
func TestWireStopTable(t *testing.T) {
	cases := []struct {
		side             string
		stop, tick, want float64
	}{
		{"long", 29575.90, 0.25, 29575.75},
		{"LONG", 29575.99, 0.25, 29575.75},
		{" long ", 29575.76, 0.25, 29575.75},
		{"short", 29624.10, 0.25, 29624.25},
		{"SHORT", 29624.01, 0.25, 29624.25},
		{"Short", 29624.24, 0.25, 29624.25},
		{"long", 29575.75, 0.25, RoundToTick(29575.75, 0.25)},
		{"short", 29624.25, 0.25, RoundToTick(29624.25, 0.25)},
		{"long", 2000.3, 0.10, RoundToTick(2000.3, 0.10)},
		{"short", 2000.3, 0.10, RoundToTick(2000.3, 0.10)},
		{"long", 2000.35, 0.10, RoundToTick(2000.3, 0.10)},
		{"short", 2000.35, 0.10, RoundToTick(2000.4, 0.10)},
		{"long", 41250.4, 1.0, 41250},
		{"short", 41250.4, 1.0, 41251},
		{"long", 21503.17, 0, 21503.17}, // tick <= 0 → unchanged, like RoundToTick
	}
	for _, c := range cases {
		got, err := WireStop(c.side, c.stop, c.tick)
		if err != nil || got != c.want {
			t.Errorf("WireStop(%q, %v, %v) = %v, %v; want %v", c.side, c.stop, c.tick, got, err, c.want)
		}
	}
	for _, side := range []string{"", "flat", "close_long", "buy", "sell"} {
		if _, err := WireStop(side, 29575.90, 0.25); err == nil {
			t.Errorf("WireStop(%q) must refuse an unknown side, got nil error", side)
		}
		if _, _, _, err := WireBracket(side, 29600, 29575.90, 29650, 0.25); err == nil {
			t.Errorf("WireBracket(%q) must refuse an unknown side", side)
		}
	}
	e, s, tp, err := WireBracket("long", 29600.12, 29575.90, 29648.30, 0.25)
	if err != nil || e != 29600.00 || s != 29575.75 || tp != 29648.25 {
		t.Fatalf("WireBracket long = %v %v %v %v; want 29600 29575.75 29648.25 (entry/target nearest, stop away)", e, s, tp, err)
	}
}

// TestWireRefusesUnknownSideBeforeAnySend — the fail-closed half at the
// production call site: a side the rounding cannot orient is refused, and no
// frame leaves.
func TestWireRefusesUnknownSideBeforeAnySend(t *testing.T) {
	s, _, _, frames := stopEntryServer(t)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	if _, err := tr.PlaceLimitEntry("MNQ", "flat", 1, 29600, 29575.90, 29650); err == nil || !strings.Contains(err.Error(), "long/short side") {
		t.Fatalf("unknown side must be refused by the wire rounding; got %v", err)
	}
	select {
	case p := <-frames:
		t.Fatalf("a frame left for an unorientable side: %+v", p)
	case <-time.After(50 * time.Millisecond):
	}
}
