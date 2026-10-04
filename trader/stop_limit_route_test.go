package trader

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// Routing pin (PR B, 2026-10-03; REVIEW-313 F3) at the production call site:
// the discriminator is the arm's EXPLICIT origin, never the expiry as a proxy.
//   mentor + knob ON + expiry > 0  -> the limit variant.
//   mentor + knob ON + no expiry -> REFUSED: NOT_SENT, zero wire calls, the
//     arm untouched (no stop-market fallback for an expiry-less mentor arm).
//   non-mentor -> today's path whatever the expiry.
//   knob OFF -> today's path for everyone.
// Mutants: dropping the origin gate (M-a), dropping the no-expiry refusal
// (M-b), or gating on expiry instead of origin (M-c) each turn a case RED.
func TestStopLimitKnobRoutesStopEntriesThroughTheLimitVariant(t *testing.T) {
	for _, tc := range []struct {
		name        string
		env         string
		origin      string
		expiryMs    int64
		wantOutcome stopPlaceOutcome
		wantLimit   int
		wantMarket  int
	}{
		{"knob-off-planner", "", "", 0, stopPlaceCommitted, 0, 1},
		{"knob-off-mentor-with-expiry", "", store.ArmOriginMentor, 90_000, stopPlaceCommitted, 0, 1},
		{"knob-on-planner-no-expiry", "1", "", 0, stopPlaceCommitted, 0, 1},
		{"knob-on-planner-with-expiry", "1", "", 90_000, stopPlaceCommitted, 0, 1}, // non-mentor: today's path, whatever the expiry
		{"knob-on-mentor-with-expiry", "1", store.ArmOriginMentor, 90_000, stopPlaceCommitted, 1, 0},
		{"knob-on-mentor-no-expiry-refused", "1", store.ArmOriginMentor, 0, stopPlaceNotSent, 0, 0},
		{"odd-cased-mentor-with-expiry", "1", " Mentor ", 90_000, stopPlaceCommitted, 1, 0}, // the predicate case-folds
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MENTOR_STOP_LIMIT", tc.env)
			at, _ := resetTrader(t, store.StrategyConfig{})
			at.id = "route-" + tc.name
			r := store.ArmedOrderDB{
				ID: 8, TraderID: at.id, PlanID: "2026-09-23:NY", Version: 1,
				Session: "TEST-R", Scenario: "TEST-R", Side: "long",
				EntryPx: 29650, StopPx: 29640, TargetPx: 29670,
				Origin: tc.origin, ExpiryMs: tc.expiryMs,
			}
			d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
			if d.Action != stopEntryPlace {
				t.Fatalf("fixture must be an otherwise-placeable arm, got %q", d.Action)
			}
			pl := &fakePlacer{}
			if got := at.placeOneStopEntry(pl, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != tc.wantOutcome {
				t.Fatalf("outcome = %d, want %d", got, tc.wantOutcome)
			}
			if pl.stopLimitCalls != tc.wantLimit || len(pl.calls)-pl.stopLimitCalls != tc.wantMarket {
				t.Fatalf("routed %d limit + %d market calls, want %d + %d",
					pl.stopLimitCalls, len(pl.calls)-pl.stopLimitCalls, tc.wantLimit, tc.wantMarket)
			}
		})
	}
}

// N12 expiry pin (PR B, 2026-10-03): an unfilled order with a stored expiry is
// due at now >= expiry_ms; nothing else is. A missing expiry (0) is never
// swept — no intent expired it. Remove the state guard and a filled/terminal
// row gets re-cancelled.
func TestArmExpired(t *testing.T) {
	nowMs := int64(1760000000000)
	row := store.ArmedOrderDB{State: store.StateArmed}
	if armExpired(row, nowMs) {
		t.Fatalf("no stored expiry → never due (a blanket sweep is exactly what the CTO vetoed)")
	}
	row.ExpiryMs = nowMs + 60_000
	if armExpired(row, nowMs) {
		t.Fatalf("an expiry in the future must be kept")
	}
	row.ExpiryMs = nowMs
	if !armExpired(row, nowMs) {
		t.Fatalf("an unfilled order at its expiry is due")
	}
	row.ExpiryMs = nowMs - 1
	if !armExpired(row, nowMs) {
		t.Fatalf("a lapsed unfilled order is due")
	}
	row.State = store.StatePlacePending
	if !armExpired(row, nowMs) {
		t.Fatalf("a place_pending order at its expiry is due")
	}
	row.State = store.StateWorking
	row.FillQuantity = 0
	if !armExpired(row, nowMs) {
		t.Fatalf("a working, UNFILLED order is due — a stop-limit resting at NT8 IS the order the expiry exists to cancel")
	}
	row.FillQuantity = 1
	if armExpired(row, nowMs) {
		t.Fatalf("a working order with a partial fill is a trade in progress — never due")
	}
	row.FillQuantity = 0
	row.State = store.StateCancelPending
	if armExpired(row, nowMs) {
		t.Fatalf("a cancel_pending row is owned by the settlement pass, never this sweep")
	}
	row.State = store.StateCancelled
	if armExpired(row, nowMs) {
		t.Fatalf("a terminal row is never swept by its expiry")
	}
	// REVIEW-313 F5 probe rows: an unknown or odd-cased state is NEVER due —
	// the fall-through at 6beb984a7 made "WORKING" partial fills, odd-cased
	// cancel_pending, empty and bogus states all due.
	row.State = "WORKING"
	row.FillQuantity = 1
	if armExpired(row, nowMs) {
		t.Fatalf("an odd-cased WORKING partial fill must not be cancelled by expiry")
	}
	row.State = " working "
	if armExpired(row, nowMs) {
		t.Fatalf("a padded working partial fill must not be cancelled by expiry")
	}
	row.FillQuantity = 0
	if !armExpired(row, nowMs) {
		t.Fatalf("an odd-cased, UNFILLED working order is due — the canonical predicate case-folds")
	}
	row.State = "CANCEL_PENDING"
	if armExpired(row, nowMs) {
		t.Fatalf("an odd-cased cancel_pending row is never due")
	}
	row.State = "bogus_state"
	if armExpired(row, nowMs) {
		t.Fatalf("an UNKNOWN state is never due — lifecycle actions retain their refusal of unknown states")
	}
	row.State = ""
	if armExpired(row, nowMs) {
		t.Fatalf("an empty state is never due")
	}
}

// expiryFixture stands up the real TCPServer + TCPTrader + store fixture the
// expiry sweep pins run on — the same harness the stop-entry latch test uses.
// The far side proves the given build; every frame the server writes is
// mirrored into frames.
func expiryFixture(t *testing.T, buildID string) (at *AutoTrader, st *store.Store, ledger *store.ArmedOrderStore, frames chan ntwire.FrameType, conn net.Conn) {
	t.Helper()
	t.Setenv("STOP_ENTRY_SEAM", "on")
	withMaintenanceDir(t)
	at, st = resetTrader(t, store.StrategyConfig{})
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })
	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	waitAddonRegistered(t, s)
	frames = make(chan ntwire.FrameType, 16)
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			frames <- env.Type
		}
	}()
	if buildID != "" {
		if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: buildID}); err != nil {
			t.Fatal(err)
		}
		for deadline := time.Now().Add(2 * time.Second); !ntwire.FarSideProven(s.FarSideBuildID(), buildID) && time.Now().Before(deadline); {
			time.Sleep(time.Millisecond)
		}
		if !ntwire.FarSideProven(s.FarSideBuildID(), buildID) {
			t.Fatal("fixture: far-side build never proven")
		}
	}
	broker := nttrader.NewTCPTrader(s, "MNQ", "Sim101")
	s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
	broker.StartCloseSync(at.id, "fixture", "ninjatrader", st)
	at.trader = broker
	at.config.NinjaTraderSymbol = "MNQ"
	ledger = st.ArmedOrders()
	return at, st, ledger, frames, conn
}

// sawFrame drains frames until deadline and reports whether want ever arrived.
func sawFrame(t *testing.T, frames chan ntwire.FrameType, want ntwire.FrameType, within time.Duration) bool {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case f := <-frames:
			if f == want {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// REVIEW-313 M10 + M11 + F1 pin, behavioural at the production call site: an
// expired WORKING row (a stop-limit resting at NT8) must become cancel_pending
// AND emit a cancel_order frame on the SAME pass. Disabling the sweep (M10) or
// the cancel inside it (M11) kept the old source-grep test green — no grep can
// see this.
func TestExpirySweepCancelsARestingOrderOnTheWire(t *testing.T) {
	at, _, ledger, frames, _ := expiryFixture(t, ntwire.MinAddonBuildStopLimit)
	now := rthInstant()
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "expwire", Scenario: "S1", Version: 1, State: "armed",
		Side: "long", EntryPx: 29600, StopPx: 29590, TargetPx: 29630, Kind: "stop_entry", Condition: "reclaim"}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	s := at.armedTrader().GetServer()
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	at.runArmedPlacementAt([]market.Kline{{Close: 29599}}, now.Add(-time.Hour).UnixMilli(), now,
		armAdmission{armAdmitKey("expwire", "S1", 0): true})
	if !sawFrame(t, frames, ntwire.FrameSignal, 300*time.Millisecond) {
		t.Fatal("fixture: the placement never reached the wire")
	}
	var got store.ArmedOrderDB
	if err := ledger.DB().First(&got, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.State != store.StatePlacePending || got.SignalID == "" {
		t.Fatalf("fixture: row must be place_pending with a signal id after placement, got state=%q signal=%q", got.State, got.SignalID)
	}
	// The evaluator stamps the expiry, and the broker receipt makes it working.
	if err := ledger.SetArmExpiry(got.ID, now.UnixMilli()+60_000); err != nil {
		t.Fatal(err)
	}
	if err := ledger.ApplyPlacementReceipt(at.id, got.SignalID, store.StateWorking, "test order_update"); err != nil {
		t.Fatal(err)
	}
	now2 := now.Add(61 * time.Second)
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{
		{Name: got.SignalID, State: "Working", Type: "stop_limit"}}}, now2)
	at.runArmedPlacementAt([]market.Kline{{Close: 29599}}, now.Add(-time.Hour).UnixMilli(), now2,
		armAdmission{armAdmitKey("expwire", "S1", 0): true})
	if !sawFrame(t, frames, ntwire.FrameCancelOrder, 500*time.Millisecond) {
		t.Fatal("the expiry sweep must send cancel_order on the same pass the order lapses")
	}
	if err := ledger.DB().First(&got, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.State != store.StateCancelPending || !strings.Contains(got.StateReason, "stop-limit expiry elapsed") {
		t.Fatalf("the lapsed row must be cancel_pending with the expiry reason, got state=%q reason=%q", got.State, got.StateReason)
	}
}

// REVIEW-313 F2 pin, behavioural: an expired arm that was NEVER SENT (armed,
// no signal id) ends terminal 'expired' in the ledger — no broker cancel, no
// stranded cancel_pending with an empty signal id that no regime can settle.
func TestExpiredUnsentArmEndsTerminal(t *testing.T) {
	at, _, ledger, frames, _ := expiryFixture(t, ntwire.MinAddonBuildStopLimit)
	now := rthInstant()
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "expunsent", Scenario: "S1", Version: 1, State: "armed",
		Side: "long", EntryPx: 29600, StopPx: 29590, TargetPx: 29630, Kind: "stop_entry", Condition: "reclaim"}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	if err := ledger.SetArmExpiry(row.ID, now.UnixMilli()-1); err != nil {
		t.Fatal(err)
	}
	at.runArmedPlacementAt([]market.Kline{{Close: 29599}}, now.Add(-time.Hour).UnixMilli(), now, nil)
	var got store.ArmedOrderDB
	if err := ledger.DB().First(&got, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.State != "expired" || !strings.Contains(got.StateReason, "never placed") {
		t.Fatalf("an expired never-sent arm must end terminal expired, got state=%q reason=%q", got.State, got.StateReason)
	}
	if sawFrame(t, frames, ntwire.FrameCancelOrder, 300*time.Millisecond) {
		t.Fatal("an arm that never reached the wire must not emit a broker cancel")
	}
}
