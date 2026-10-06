package ninjatrader

import (
	"errors"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// Production command composition, real loopback frame, and real inbound reject
// routing: a fresh row alone would not detect the bar-derived timestamp defect.
func TestEntryCommandClockAndReceivedRejection(t *testing.T) {
	for _, kind := range []string{"limit", "stop_entry"} {
		for _, reason := range []string{"", "  stale signal age=715.3s (max 60s)  "} {
			t.Run(kind+"/"+reason, func(t *testing.T) {
				s, st, conn, frames := stopEntryServer(t)
				if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: ntwire.MinAddonBuildStopSlot}); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(time.Second)
				for s.FarSideBuildID() == "" && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				tr := NewTCPTrader(s, "MNQ", "Sim101")
				tr.StartCloseSync("clock-trader", "fixture", "ninjatrader", st)
				bar := ntwire.Bar{T: time.Now().Add(-31 * time.Minute).UnixMilli(), O: 29600, H: 29602, L: 29598, C: 29601, V: 10}
				s.BarCache().SeedHistorical("MNQ", "1m", []ntwire.Bar{bar})
				if time.Since(tr.feedNowUTC("MNQ")) < 29*time.Minute {
					t.Fatal("fixture lacks an old market clock")
				}
				ledger := st.ArmedOrders()
				row := &store.ArmedOrderDB{TraderID: "clock-trader", PlanID: "clock-plan", Scenario: "S1", State: "armed", Version: 1, EntryPx: bar.C, StopPx: 29590, TargetPx: 29630}
				if err := ledger.UpsertArm(row); err != nil {
					t.Fatal(err)
				}
				register := func(sid string) error { return ledger.BeginPlacement(row.ID, sid) }
				before := time.Now().UTC().Truncate(time.Millisecond)
				var sid string
				var err error
				if kind == "stop_entry" {
					sid, err = tr.PlaceStopEntry("MNQ", "long", 1, bar.C, row.StopPx, row.TargetPx, 0, 0, register)
				} else {
					sid, err = tr.PlaceLimitEntry("MNQ", "long", 1, bar.C, row.StopPx, row.TargetPx, register)
				}
				if err != nil {
					t.Fatal(err)
				}
				var p ntwire.SignalPayload
				select {
				case p = <-frames:
				case <-time.After(time.Second):
					t.Fatal("no entry frame")
				}
				received := time.Now().UTC()
				stamp, err := time.Parse(time.RFC3339Nano, p.Timestamp)
				if err != nil || stamp.Before(before) || stamp.After(received) {
					t.Fatalf("command clock %q outside [%s,%s], err=%v", p.Timestamp, before, received, err)
				}
				if received.Sub(stamp) >= time.Second {
					t.Fatalf("payload age=%s, want milliseconds", received.Sub(stamp))
				}
				t.Logf("old market bar age=%s; received payload age=%.3f ms", received.Sub(time.UnixMilli(bar.T)), float64(received.Sub(stamp))/float64(time.Millisecond))
				var got store.ArmedOrderDB
				if err := ledger.DB().First(&got, row.ID).Error; err != nil {
					t.Fatal(err)
				}
				if got.State != store.StatePlacePending || got.SignalID != sid {
					t.Fatalf("send claimed acceptance or lost identity: %+v", got)
				}
				// h1's rejection is a fill frame, not an order_update. Exercise that consumer.
				if err := ntwire.WriteFrame(conn, ntwire.FrameFill, ntwire.FillPayload{SignalID: sid, Symbol: p.Symbol, Account: p.Account, TraderID: p.TraderID, Seq: p.Seq, Status: "rejected", Reason: reason}); err != nil {
					t.Fatal(err)
				}
				deadline = time.Now().Add(time.Second)
				for time.Now().Before(deadline) {
					if err := ledger.DB().First(&got, row.ID).Error; err != nil {
						t.Fatal(err)
					}
					if got.State == store.StateRejected {
						break
					}
					time.Sleep(time.Millisecond)
				}
				if got.State != store.StateRejected {
					t.Fatalf("received reject did not settle row: %+v", got)
				}
				if reason != "" && got.StateReason != reason {
					t.Fatalf("reason altered: %q", got.StateReason)
				}
				if reason == "" && !strings.Contains(got.StateReason, "reason unavailable") {
					t.Fatalf("missing reason hidden: %q", got.StateReason)
				}
			})
		}
	}
}

func TestEntryRegistrationFailureNeverSends(t *testing.T) {
	s, _, _, frames := stopEntryServer(t)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	_, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29600, 29590, 29630, func(string) error { return errors.New("fixture ledger unavailable") })
	if err == nil {
		t.Fatal("registration failure ignored")
	}
	select {
	case <-frames:
		t.Fatal("unregistered entry sent")
	case <-time.After(30 * time.Millisecond):
	}
}
