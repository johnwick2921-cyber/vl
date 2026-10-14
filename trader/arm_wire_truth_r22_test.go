package trader

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// RELEASE #22 CANCEL-SETTLE — F-7 (review-1014 EXECUTION.md F-7), re-verified
// against data.db (read-only) before the fix:
//
//   - row 350 stop_px 30882.7763003284 vs NT8 -sl 30883 (snapshot 109320);
//   - row 347 leg1_tp 30824.25 vs NT8 -tp 30825.25 (snapshot 108678);
//   - row 362 leg1_tp 30725.25 vs NT8 -tp 30724.25 (snapshot 110404).
//
// The pin (canon 53, production call sites): every placement path sends a
// REAL frame over a loopback AddOn, and the ledger row must then carry EXACTLY
// the frame's stop_loss / take_profit (and, on a mentor split, leg1_tp and
// leg1_qty). Both sides are read — the frame's bytes and the row — never
// rebuilt from the same inputs.

const r22Tick = 0.25

func r22OffGrid(px float64) bool {
	return nttrader.WireMoved(px, nttrader.RoundToTick(px, r22Tick), r22Tick)
}

func r22LedgerEqualsWire(t *testing.T, row store.ArmedOrderDB, p ntwire.SignalPayload) {
	t.Helper()
	if row.SignalID != p.SignalID {
		t.Fatalf("fixture: row signal %q vs frame %q", row.SignalID, p.SignalID)
	}
	if row.StopPx != p.StopLoss {
		t.Fatalf("ledger stop_px %v ≠ wire stop_loss %v — the ledger records a price NT8 never received (row 350: 30882.7763 vs 30883)", row.StopPx, p.StopLoss)
	}
	if row.TargetPx != p.TakeProfit {
		t.Fatalf("ledger target_px %v ≠ wire take_profit %v", row.TargetPx, p.TakeProfit)
	}
}

// The four placement paths TestFourPlacementPathsWaitForEntryReceipt drives
// (the plan limit pass, the stop-entry pass, and both E2 debug seams), with an
// OFF-TICK authored stop and target.
func TestR22LedgerRecordsTheWirePricesOnEveryPlacementPath(t *testing.T) {
	const entry, stop, target = 29600.0, 29589.87, 29630.1
	if !r22OffGrid(stop) || !r22OffGrid(target) {
		t.Fatal("fixture: the authored stop and target must be off the tick grid, or the pin is vacuous")
	}
	for _, path := range []string{"limit", "stop_entry", "debug_limit", "debug_stop"} {
		t.Run(path, func(t *testing.T) {
			t.Setenv("ARMED_TEST_SEAM", "on")
			t.Setenv("STOP_ENTRY_SEAM", "on")
			at, st := resetTrader(t, store.StrategyConfig{})
			s := ntwire.NewTCPServer(nil)
			s.SetAddrForTest("127.0.0.1:0")
			s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := s.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer s.Stop()
			conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
			if err != nil {
				t.Fatal(err)
			}
			waitAddonRegistered(t, s)
			defer conn.Close()
			frames := make(chan ntwire.SignalPayload, 4)
			go func() {
				for {
					env, err := ntwire.ReadFrame(conn)
					if err != nil {
						return
					}
					if env.Type == ntwire.FrameSignal {
						var p ntwire.SignalPayload
						if json.Unmarshal(env.Payload, &p) == nil {
							frames <- p
						}
					}
				}
			}()
			if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: ntwire.MinAddonBuildStopSlot}); err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(time.Second); s.FarSideBuildID() == "" && time.Now().Before(deadline); {
				time.Sleep(time.Millisecond)
			}
			broker := nttrader.NewTCPTrader(s, "MNQ", "Sim101")
			s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
			broker.StartCloseSync(at.id, "fixture", "ninjatrader", st)
			at.trader = broker
			at.config.NinjaTraderSymbol = "MNQ"
			s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, rthInstant())
			ledger := st.ArmedOrders()
			var row store.ArmedOrderDB
			switch path {
			case "debug_limit":
				row, err = at.TestArmPlace("long", entry, stop, target)
			case "debug_stop":
				row, err = at.TestArmPlaceStop("long", entry, stop, target)
			default:
				row = store.ArmedOrderDB{TraderID: at.id, PlanID: "placement", Scenario: "S1", Version: 1, State: "armed", Side: "long", EntryPx: entry, StopPx: stop, TargetPx: target, Kind: path, Condition: "reclaim"}
				if err = ledger.UpsertArm(&row); err != nil {
					t.Fatal(err)
				}
				price := 29601.0
				if path == "stop_entry" {
					price = 29599
				}
				at.runArmedPlacementAt([]market.Kline{{Close: price}}, rthInstant().Add(-time.Hour).UnixMilli(), rthInstant(),
					armAdmission{armAdmitKey("placement", "S1", 0): true})
			}
			if err != nil {
				t.Fatal(err)
			}
			var p ntwire.SignalPayload
			select {
			case p = <-frames:
			case <-time.After(2 * time.Second):
				t.Fatal("production path did not send")
			}
			if err := ledger.DB().First(&row, row.ID).Error; err != nil {
				t.Fatal(err)
			}
			r22LedgerEqualsWire(t, row, p)
		})
	}
}

// The market_in_zone path (zone_placement.go placeZoneRow): its composed stop
// is ATR-derived (≈97.70 in zoneDoc), off the grid the way row 350's was.
func TestR22ZonePlacementLedgerRecordsTheWirePrices(t *testing.T) {
	r := newZoneRig(t, "r22-f7-zone", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	sig := r.placeWorking(100.5)
	row := r.row("S1")
	r22LedgerEqualsWire(t, row, sig)
}

// The mentor stop-limit split (rows 347 and 362): the leg-1 TP is re-based on
// the wire trigger and rounded by the sender, so the authored leg1_tp is NOT
// what NT8 received. The ledger must carry the frame's leg1_tp and leg1_qty.
func TestR22MentorSplitLedgerRecordsTheWireLeg1(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, ledger, raw := mentorLoopbackRaw(t, ntwire.MinAddonBuildSplitLegs)
	mentorWireSeams(t, at, ledger)
	mentorPinClockToRTH(t)
	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-r22-f7", Setup: "ISB", Side: mentor.SideLong,
		Price: 29600, Stop: 29594.13, Target: 29611.9, StopPts: 5.87, TargetPts: 11.9, ExpiryMs: rthInstant().UnixMilli() + 60_000}
	at.mentorDispatchIntent(in, mentorTierInputs{Setup: "ISB", StopPts: 5.87, TargetPts: 11.9}, 1000, 1100)
	now := rthInstant()
	s := at.armedTrader().GetServer()
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	at.runArmedPlacementAt([]market.Kline{{Close: 29599}}, now.Add(-time.Hour).UnixMilli(), now, nil)
	var payload []byte
	select {
	case payload = <-raw:
	case <-time.After(2 * time.Second):
		t.Fatal("no signal frame reached the wire")
	}
	var p ntwire.SignalPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Leg1Qty <= 0 || p.Leg1TP <= 0 {
		t.Fatalf("fixture: the frame must carry a split (leg1_qty=%d leg1_tp=%v) or the pin is vacuous", p.Leg1Qty, p.Leg1TP)
	}
	row, err := ledger.FindBySignal(at.id, p.SignalID)
	if err != nil || row == nil {
		t.Fatalf("row for the frame: %v", err)
	}
	t.Logf("wire: stop=%v target=%v leg1=%d@%v · ledger: stop=%v target=%v leg1=%s@%v", p.StopLoss, p.TakeProfit, p.Leg1Qty, p.Leg1TP, row.StopPx, row.TargetPx, intPtrText(row.Leg1Qty), row.Leg1TP)
	r22LedgerEqualsWire(t, *row, p)
	if row.Leg1TP != p.Leg1TP {
		t.Fatalf("ledger leg1_tp %v ≠ wire leg1_tp %v — row 347's drift (30824.25 vs NT8 30825.25)", row.Leg1TP, p.Leg1TP)
	}
	if row.Leg1Qty == nil || *row.Leg1Qty != p.Leg1Qty {
		t.Fatalf("ledger leg1_qty %v ≠ wire leg1_qty %d", row.Leg1Qty, p.Leg1Qty)
	}
}
