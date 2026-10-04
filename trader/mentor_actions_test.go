package trader

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── MENTOR INJECTOR P0 pins (CTO 1791040360324) ───────────────────────────────
//
// Every evaluator action reaches a real executor, at the call site. The
// counters (MentorCountSnapshot) are the refusal ledger: each test asserts
// the exact counter its branch must bump, so a branch that drops an action
// silently turns RED instead of passing.

func resetMentorCounters() {
	mentorCounterMu.Lock()
	mentorCounters = map[string]int{}
	mentorCounterMu.Unlock()
}

// mentorLoopback is a started loopback NT8 server whose far side proves the
// given build, mirroring every frame type into frames.
func mentorLoopback(t *testing.T, buildID string) (at *AutoTrader, st *store.Store, ledger *store.ArmedOrderStore, frames chan ntwire.FrameType) {
	t.Helper()
	t.Setenv("STOP_ENTRY_SEAM", "on")
	at, st = resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{
		MentorMode:          true,
		MentorWindowStart:   "00:00", // all-day window so the window gate passes at any test hour
		MentorWindowMinutes: 1440,
	}})
	withMaintenanceDir(t)
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
	frames = make(chan ntwire.FrameType, 32)
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
	// A fresh empty book: the slot + one-contract guards read it and an
	// unreadable book is a refusal (A24).
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, time.Now())
	ledger = st.ArmedOrders()
	return at, st, ledger, frames
}

// mentorWireSeams injects every placement seam the mentorSourcesMissing gate
// reads, so the entry path can reach the wire.
func mentorWireSeams(t *testing.T, at *AutoTrader, ledger *store.ArmedOrderStore) {
	t.Helper()
	mentorOpenStopSource = func() (float64, bool) { return 0, false }
	mentorOpenSideSource = func() string { return "" } // no open position: the never-add gate passes
	mentorLegProtectedSource = func(leg string) bool { return false }
	mentorLatestPriceSource = func() (float64, bool) { return 29590, true } // below the long trigger: the no-chase rule passes
	mentorConfluenceForIntent = func(in mentor.Intent) bool { return in.Confluence }
	mentorDayNetSource = func() float64 { return 0 }
	mentorClosedProfitSource = func() bool { return false }
	mentorSetArmExpiryWire = func(armID int64, expiryMs int64) error {
		return ledger.SetArmExpiry(armID, expiryMs)
	}
	mentorSourceDepthSource = func(name string) (int, bool) { return 1000, true }
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline {
		return []market.Kline{{Open: 1, High: 2, Low: 0.5, Close: 1.5, OpenTime: time.Now().UnixMilli()}}
	}
	t.Cleanup(func() {
		mentorOpenStopSource = nil
		mentorOpenSideSource = nil
		mentorLegProtectedSource = nil
		mentorLatestPriceSource = nil
		mentorConfluenceForIntent = nil
		mentorDayNetSource = nil
		mentorClosedProfitSource = nil
		mentorSetArmExpiryWire = nil
		mentorSourceDepthSource = nil
		market.FuturesBarsProvider = nil
		mentorLiveMu.Lock()
		mentorLiveArms = map[string]mentorLiveArm{}
		mentorLiveMu.Unlock()
	})
}

// sawMentorFrame drains frames until deadline and reports whether want arrived.
func sawMentorFrame(t *testing.T, frames chan ntwire.FrameType, want ntwire.FrameType, within time.Duration) bool {
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

// TestMentorDispatchHandlesEveryAction — the switch pin: every evaluator
// action has a named executor; an unknown action REFUSES with a counter,
// never silent. Each case asserts its exact counter.
func TestMentorDispatchHandlesEveryAction(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	t.Setenv("MENTOR_PLACE", "")
	resetMentorCounters()
	cases := []struct {
		name string
		in   mentor.Intent
		want string
	}{
		{"place_stop_limit_entry_held", mentor.Intent{Action: mentor.PlaceStopLimitEntry, Setup: "ISB", Side: mentor.SideLong,
			Price: 29600, Stop: 29595, Target: 29610, StopPts: 5, TargetPts: 10, ExpiryMs: 100_000}, "placement_held"},
		{"extend_unknown_arm", mentor.Intent{Action: mentor.ExtendArm, ArmID: "nope", ExpiryMs: 100_000}, "extend_refused_unknown_arm"},
		{"cancel_unknown_arm", mentor.Intent{Action: mentor.CancelArm, ArmID: "nope"}, "cancel_refused_unknown_arm"},
		{"level_invalid_recorded", mentor.Intent{Action: mentor.LevelInvalid, LevelKey: "k:1@x"}, "intent_level_invalid"},
		{"move_be_unknown_arm", mentor.Intent{Action: mentor.ActionMoveStopBE, ArmID: "nope"}, "move_be_refused_unknown_arm"},
		{"close_unknown_arm", mentor.Intent{Action: mentor.ActionClosePosition, ArmID: "nope"}, "close_refused_unknown_arm"},
		{"unknown_action_refused", mentor.Intent{Action: "frobnicate", Reason: "what is this"}, "unknown_action_frobnicate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetMentorCounters()
			at.mentorDispatchIntent(tc.in, mentorTierInputs{}, 1000, 1100)
			if got := MentorCountSnapshot()[tc.want]; got != 1 {
				t.Fatalf("counter %q = %d, want 1 (the action must reach its named executor)", tc.want, got)
			}
		})
	}
}

// TestMentorEntryIsAlwaysStopLimit — the ONE mentor entry path (P0-b) at
// the call site: a PlaceStopLimitEntry intent (the ISB order type) authors an
// ARMED LEDGER row (kind stop_entry, the intent's expiry, state armed) and the
// ARMED EXECUTOR places it — a stop_limit frame on the wire, no direct wire
// hop in the injector. Mutant: a direct broker call in the injector leaves no
// row behind, and this turns RED.
func TestMentorEntryIsAlwaysStopLimit(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	t.Setenv("MENTOR_STOP_LIMIT", "") // ALWAYS stop-limit: the env is not read, an unset env must still route the limit variant
	at, _, ledger, frames := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	resetMentorCounters()

	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-1", Setup: "ISB", Side: mentor.SideLong,
		Price: 29600, Stop: 29595, Target: 29610, StopPts: 5, TargetPts: 10, ExpiryMs: rthInstant().UnixMilli() + 60_000}
	at.mentorDispatchIntent(in, mentorTierInputs{}, 1000, 1100)

	// The injector authored the armed row; nothing reached the wire yet.
	if sawMentorFrame(t, frames, ntwire.FrameSignal, 300*time.Millisecond) {
		t.Fatal("the injector must NOT place directly — the armed executor places")
	}
	var row store.ArmedOrderDB
	if err := ledger.DB().Where("scenario = ?", "isb-1").First(&row).Error; err != nil {
		t.Fatalf("the mentor arm row must exist: %v", err)
	}
	if row.Kind != "stop_entry" || row.ExpiryMs != in.ExpiryMs || row.State != store.StateArmed {
		t.Fatalf("mentor arm kind=%q expiry=%d state=%q, want stop_entry / %d / armed", row.Kind, row.ExpiryMs, row.State, in.ExpiryMs)
	}
	if !isMentorArmOrigin(row) {
		t.Fatalf("mentor injector row origin=%q, want %q", row.Origin, store.ArmOriginMentor)
	}
	arm, ok := mentorLiveArmFor("isb-1")
	if !ok || arm.RowID != row.ID {
		t.Fatalf("the ArmID registry must resolve isb-1 -> row %d, got %+v ok=%v", row.ID, arm, ok)
	}
	armed := 0
	for k, v := range MentorCountSnapshot() {
		if strings.HasPrefix(k, "armed_") {
			armed += v
		}
	}
	if armed != 1 {
		t.Fatalf("exactly one armed tier must be counted, got %d", armed)
	}

	// The armed executor places it: stop_limit frame on the wire. The clock
	// is pinned to an RTH instant (class 110): the admission chain asks the
	// calendar on the clock it is GIVEN.
	now := rthInstant()
	s := at.armedTrader().GetServer()
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	at.runArmedPlacementAt([]market.Kline{{Close: 29599}}, now.Add(-time.Hour).UnixMilli(), now, nil)
	if !sawMentorFrame(t, frames, ntwire.FrameSignal, 2*time.Second) {
		t.Fatal("the armed pass must place the mentor arm")
	}
	if err := ledger.DB().First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != store.StatePlacePending || row.SignalID == "" {
		t.Fatalf("after the armed pass the row must be place_pending with a signal id, got %q/%q", row.State, row.SignalID)
	}

	// The same production send-point admission refuses a planner row on this
	// mentor-mode trader, even when its planner authoring pass admitted it.
	plannerRow := store.ArmedOrderDB{PlanID: "planner", Scenario: "S1", LegIndex: 0}
	plannerAdmission := armAdmission{armAdmitKey(plannerRow.PlanID, plannerRow.Scenario, plannerRow.LegIndex): true}
	if admitted, refusal := at.armAdmitted(plannerRow, "long", 29600, now, plannerAdmission); admitted ||
		!strings.Contains(refusal, "mentor_mode: AI entries are OFF") {
		t.Fatalf("planner row must be refused by mentor-mode admission, admitted=%v refusal=%q", admitted, refusal)
	}
}

// mentorLoopbackRaw is mentorLoopback plus the RAW signal payload bytes (the
// wire-flag pin needs the bytes: a decoded bool cannot tell absent from false).
func mentorLoopbackRaw(t *testing.T, buildID string) (at *AutoTrader, ledger *store.ArmedOrderStore, raw chan []byte) {
	t.Helper()
	t.Setenv("STOP_ENTRY_SEAM", "on")
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{
		MentorMode:          true,
		MentorWindowStart:   "00:00",
		MentorWindowMinutes: 1440,
	}})
	withMaintenanceDir(t)
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
	raw = make(chan []byte, 8)
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			if env.Type == ntwire.FrameSignal {
				raw <- append([]byte(nil), env.Payload...)
			}
		}
	}()
	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: buildID}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); !ntwire.FarSideProven(s.FarSideBuildID(), buildID) && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if !ntwire.FarSideProven(s.FarSideBuildID(), buildID) {
		t.Fatal("fixture: far-side build never proven")
	}
	broker := nttrader.NewTCPTrader(s, "MNQ", "Sim101")
	s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
	broker.StartCloseSync(at.id, "fixture", "ninjatrader", st)
	at.trader = broker
	at.config.NinjaTraderSymbol = "MNQ"
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, time.Now())
	return at, st.ArmedOrders(), raw
}

// TestMentorEntryFrameCarriesStopLimitTrue — the raw frame pin through
// the armed path: the mentor entry's stop_limit frame carries
// stop_limit:true and order_type stop_entry. (Knob-independence of the knob
// arrives with the bundle's origin routing; p3's routing is knob-gated.)
func TestMentorEntryFrameCarriesStopLimitTrue(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	t.Setenv("MENTOR_STOP_LIMIT", "") // the env is not read (always stop-limit)
	at, ledger, raw := mentorLoopbackRaw(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-raw", Setup: "ISB", Side: mentor.SideLong,
		Price: 29600, Stop: 29595, Target: 29610, StopPts: 5, TargetPts: 10, ExpiryMs: rthInstant().UnixMilli() + 60_000}, mentorTierInputs{}, 1000, 1100)
	now := rthInstant()
	s := at.armedTrader().GetServer()
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	at.runArmedPlacementAt([]market.Kline{{Close: 29599}}, now.Add(-time.Hour).UnixMilli(), now, nil)
	deadline := time.After(2 * time.Second)
	var payload []byte
	select {
	case payload = <-raw:
	case <-deadline:
		t.Fatal("no signal frame reached the wire")
	}
	if !bytes.Contains(payload, []byte(`"stop_limit":true`)) {
		t.Fatalf("the mentor frame must carry stop_limit:true, got %s", payload)
	}
	var p ntwire.SignalPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.OrderType != "stop_entry" || !p.StopLimit {
		t.Fatalf("order_type=%q stop_limit=%v, want stop_entry/true", p.OrderType, p.StopLimit)
	}
}

// TestMentorEntryRefusesBelowC2 — an AddOn below the c2 floor REFUSES the
// mentor entry in the armed pass: the row stays armed (no frame reaches the
// wire) — never a stop-market fallback.
func TestMentorEntryRefusesBelowC2(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	t.Setenv("MENTOR_STOP_LIMIT", "")                                            // the env is not read (always stop-limit)
	at, _, ledger, frames := mentorLoopback(t, ntwire.MinAddonBuildCancelReport) // c1 < c2
	mentorWireSeams(t, at, ledger)
	resetMentorCounters()
	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-c1", Setup: "ISB", Side: mentor.SideLong,
		Price: 29600, Stop: 29595, Target: 29610, StopPts: 5, TargetPts: 10, ExpiryMs: rthInstant().UnixMilli() + 60_000}
	at.mentorDispatchIntent(in, mentorTierInputs{}, 1000, 1100)
	now := rthInstant()
	srv := at.armedTrader().GetServer()
	srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	at.runArmedPlacementAt([]market.Kline{{Close: 29599}}, now.Add(-time.Hour).UnixMilli(), now, nil)
	if sawMentorFrame(t, frames, ntwire.FrameSignal, 500*time.Millisecond) {
		t.Fatal("an AddOn below c2 must never receive a mentor entry frame")
	}
	var row store.ArmedOrderDB
	if err := ledger.DB().Where("scenario = ?", "isb-c1").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != store.StateArmed {
		t.Fatalf("the refused mentor arm stays armed (the pass retries; never a stop-market), got %q", row.State)
	}
}

// TestMentorExtendArmStampsTheRestingRow — ExtendArm reaches SetArmExpiry for
// the registered row (working + unfilled = the resting stop-limit ISB
// stacking extends). Mutant: dropping the case leaves the counter unset.
func TestMentorExtendArmStampsTheRestingRow(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "isb-ext",
		Side: "long", EntryPx: 29600, StopPx: 29595, TargetPx: 29610, Kind: "stop_entry", Condition: "ISB", ExpiryMs: time.Now().UnixMilli() + 60_000}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	if err := ledger.SetState(row.ID, store.StateWorking, "test receipt"); err != nil {
		t.Fatal(err)
	}
	mentorRegisterLiveArm("isb-ext", row.ID, "long", 29600)
	resetMentorCounters()
	newExpiry := time.Now().UnixMilli() + 120_000
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.ExtendArm, ArmID: "isb-ext", ExpiryMs: newExpiry}, mentorTierInputs{}, 1000, 1100)
	var got store.ArmedOrderDB
	if err := ledger.DB().First(&got, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ExpiryMs != newExpiry {
		t.Fatalf("the resting arm's expiry was not extended: got %d, want %d", got.ExpiryMs, newExpiry)
	}
	if c := MentorCountSnapshot()["extend_ok"]; c != 1 {
		t.Fatalf("extend_ok = %d, want 1", c)
	}
}

// TestMentorCancelArmReachesTheBroker — CancelArm sends the cancel_order on
// the same pass (the F1 shape) and requests the ledger cancel.
func TestMentorCancelArmReachesTheBroker(t *testing.T) {
	at, _, ledger, frames := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	now := time.Now()
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "isb-can",
		Side: "long", EntryPx: 29600, StopPx: 29595, TargetPx: 29610, Kind: "stop_entry", Condition: "ISB",
		State: store.StateWorking, SignalID: "isb-can-sig", ExpiryMs: now.UnixMilli() + 60_000}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	s := at.armedTrader().GetServer()
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{
		{Name: "isb-can-sig", State: "Working", Type: "stop_limit"}}}, now)
	mentorRegisterLiveArm("isb-can", row.ID, "long", 29600)
	resetMentorCounters()
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.CancelArm, ArmID: "isb-can", Reason: "ISB escaped the mother candle"}, mentorTierInputs{}, 1000, 1100)
	if !sawMentorFrame(t, frames, ntwire.FrameCancelOrder, 1*time.Second) {
		t.Fatal("CancelArm must send cancel_order on the same pass")
	}
	var got store.ArmedOrderDB
	if err := ledger.DB().First(&got, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.State != store.StateCancelPending {
		t.Fatalf("the cancelled row must be cancel_pending, got %q", got.State)
	}
	if c := MentorCountSnapshot()["cancel_requested"]; c != 1 {
		t.Fatalf("cancel_requested = %d, want 1", c)
	}
}

// TestMentorMoveStopBEReachesMoveStopWire — the swing's BE intent moves the
// stop to the registered ENTRY price through mentorMoveStop (never-widen
// guarded). Mutant: dropping the dispatch case leaves the spy uncalled.
func TestMentorMoveStopBEReachesMoveStopWire(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorOpenStopSource = func() (float64, bool) { return 29595, true } // current stop below entry: BE is not a widen
	t.Cleanup(func() { mentorOpenStopSource = nil })
	var mu sync.Mutex
	var moved []float64
	oldWire := moveStopWire
	moveStopWire = func(nt *nttrader.TCPTrader, side string, newStop float64) error {
		mu.Lock()
		moved = append(moved, newStop)
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { moveStopWire = oldWire })
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "swing-1",
		Side: "long", EntryPx: 29600, StopPx: 29595, TargetPx: 29660, Kind: "stop_entry", Condition: "SWING4H",
		State: store.StateFilled, SignalID: "swing-1-sig", FillPrice: 29600, FillQuantity: 1}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	mentorRegisterLiveArm("swing-1", row.ID, "long", 29600)
	resetMentorCounters()
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionMoveStopBE, ArmID: "swing-1", Reason: "swing +1R"}, mentorTierInputs{}, 1000, 1100)
	mu.Lock()
	defer mu.Unlock()
	if len(moved) != 1 || moved[0] != 29600 {
		t.Fatalf("moveStopWire must be called once with the entry price, got %v", moved)
	}
	if c := MentorCountSnapshot()["move_be_sent"]; c != 1 {
		t.Fatalf("move_be_sent = %d, want 1", c)
	}
}

// TestMentorClosePositionReachesTheBroker — the swing's hold-close intent
// closes the registered side at the broker.
func TestMentorClosePositionReachesTheBroker(t *testing.T) {
	at, _, ledger, frames := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "swing-1",
		Side: "short", EntryPx: 29600, StopPx: 29630, TargetPx: 29540, Kind: "stop_entry", Condition: "SWING4H",
		State: store.StateFilled, SignalID: "swing-1-sig", FillPrice: 29600, FillQuantity: 1}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	mentorRegisterLiveArm("swing-1", row.ID, "short", 29600)
	resetMentorCounters()
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionClosePosition, ArmID: "swing-1", Reason: "hold to the 2nd 4h close"}, mentorTierInputs{}, 1000, 1100)
	if !sawMentorFrame(t, frames, ntwire.FrameClosePosition, 1*time.Second) {
		t.Fatal("ClosePosition must reach the broker")
	}
	if c := MentorCountSnapshot()["close_sent"]; c != 1 {
		t.Fatalf("close_sent = %d, want 1", c)
	}
}

// TestMentorIntentYieldsExactlyOneArmedRow — DS-102's merge check: one
// mentor intent authors exactly one ledger row, and the row carries the
// intent's expiry as the ledger write itself (no double stamping seam).
func TestMentorIntentYieldsExactlyOneArmedRow(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	expiry := time.Now().UnixMilli() + 60_000
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-one", Setup: "ISB", Side: mentor.SideLong,
		Price: 29600, Stop: 29595, Target: 29610, StopPts: 5, TargetPts: 10, ExpiryMs: expiry}, mentorTierInputs{}, 1000, 1100)
	var rows []store.ArmedOrderDB
	if err := ledger.DB().Where("scenario = ?", "isb-one").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("one mentor intent must yield exactly one armed row, got %d", len(rows))
	}
	if rows[0].ExpiryMs != expiry || rows[0].State != store.StateArmed {
		t.Fatalf("the row must carry the intent's expiry as the ledger write: expiry=%d state=%q", rows[0].ExpiryMs, rows[0].State)
	}
}

// TestMentorChainIntentToWireToExpiryCancel — the CTO's exact call-site chain:
// a mentor ISB intent -> an armed row -> a stop_limit frame on the wire -> at
// expiry -> cancel_order on the SAME pass.
func TestMentorChainIntentToWireToExpiryCancel(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	t.Setenv("MENTOR_STOP_LIMIT", "") // the env is not read (always stop-limit)
	at, _, ledger, frames := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	t0 := rthInstant()
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-chain", Setup: "ISB", Side: mentor.SideLong,
		Price: 29600, Stop: 29595, Target: 29610, StopPts: 5, TargetPts: 10, ExpiryMs: t0.UnixMilli() + 60_000}, mentorTierInputs{}, 1000, 1100)
	var row store.ArmedOrderDB
	if err := ledger.DB().Where("scenario = ?", "isb-chain").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	srv := at.armedTrader().GetServer()
	srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, t0)
	at.runArmedPlacementAt([]market.Kline{{Close: 29599}}, t0.Add(-time.Hour).UnixMilli(), t0, nil)
	if !sawMentorFrame(t, frames, ntwire.FrameSignal, 2*time.Second) {
		t.Fatal("step 2: the armed pass must send the stop_limit frame")
	}
	if err := ledger.DB().First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != store.StatePlacePending || row.SignalID == "" {
		t.Fatalf("step 2: row must be place_pending with a signal id, got %q/%q", row.State, row.SignalID)
	}
	// The evaluator's stacking extension moves the expiry; here the test
	// lapses it: at now >= expiry the pass requests the cancel on the SAME
	// pass. (The cancel_order FRAME on that same pass is the F1 fix on
	// fix/stop-limit-r2, which merges in the bundle; on p3 the sweep's
	// RequestCancel moves the row to cancel_pending on this pass.)
	if err := ledger.SetArmExpiry(row.ID, t0.UnixMilli()-1); err != nil {
		t.Fatal(err)
	}
	t1 := t0.Add(2 * time.Second)
	srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{
		{Name: row.SignalID, State: "Working", Type: "stop_limit"}}}, t1)
	at.runArmedPlacementAt([]market.Kline{{Close: 29599}}, t0.Add(-time.Hour).UnixMilli(), t1, nil)
	if err := ledger.DB().First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != store.StateCancelPending {
		t.Fatalf("step 3: the lapsed row must be cancel_pending on the SAME pass, got %q", row.State)
	}
}

// TestMentorClosePositionRefusesUnfilledArm (S1 pin, production call site
// mentorDispatchIntent): a swing ClosePosition for an arm that never FILLED
// must be refused NAMED and must NOT flatten an open position on the same
// side. Mutant: close on any registered arm (skip the fill check) → the
// close_sent counter fires and this turns RED.
func TestMentorClosePositionRefusesUnfilledArm(t *testing.T) {
	at, _, ledger, frames := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "swing-uf",
		Side: "short", EntryPx: 29600, StopPx: 29630, TargetPx: 29540, Kind: "stop_entry", Condition: "SWING4H",
		State: store.StateWorking, SignalID: "swing-uf-sig"} // resting, NOT filled
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	mentorRegisterLiveArm("swing-uf", row.ID, "short", 29600)
	resetMentorCounters()
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionClosePosition, ArmID: "swing-uf", Reason: "hold to the 2nd 4h close"}, mentorTierInputs{}, 1000, 1100)
	if sawMentorFrame(t, frames, ntwire.FrameClosePosition, 500*time.Millisecond) {
		t.Fatal("an unfilled swing must NOT send a close")
	}
	if c := MentorCountSnapshot()["close_refused_not_filled"]; c != 1 {
		t.Fatalf("close_refused_not_filled = %d, want 1", c)
	}
}

// TestMentorMoveStopBERefusesUnfilledArm (S1 pin): a swing MoveStopBE for an
// arm that never FILLED must be refused and must NOT move another trade's stop.
func TestMentorMoveStopBERefusesUnfilledArm(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "swing-uf-be",
		Side: "long", EntryPx: 29600, StopPx: 29595, TargetPx: 29660, Kind: "stop_entry", Condition: "SWING4H",
		State: store.StateCancelled, SignalID: "swing-uf-be-sig"} // cancelled, never filled
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	mentorRegisterLiveArm("swing-uf-be", row.ID, "long", 29600)
	resetMentorCounters()
	oldWire := moveStopWire
	moved := false
	moveStopWire = func(nt *nttrader.TCPTrader, side string, newStop float64) error { moved = true; return nil }
	t.Cleanup(func() { moveStopWire = oldWire })
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionMoveStopBE, ArmID: "swing-uf-be", Reason: "swing +1R"}, mentorTierInputs{}, 1000, 1100)
	if moved {
		t.Fatal("an unfilled swing must NOT move a stop")
	}
	if c := MentorCountSnapshot()["move_be_refused_not_filled"]; c != 1 {
		t.Fatalf("move_be_refused_not_filled = %d, want 1", c)
	}
}

// TestMentorSwingFillQuantityResolution (S1 pin): the swing close is sized by
// FillQuantity when set, else by the B2 Contracts column, else REFUSED — never
// guessed as 1 (a guess would close 1 of a 3-lot swing and strand the rest).
func TestMentorSwingFillQuantityResolution(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	three := 3
	cases := []struct {
		name         string
		fillQty      int
		contracts    *int
		wantQty      float64
		wantOK       bool
		unknownCount int
	}{
		{"fill quantity wins", 4, &three, 4, true, 0},
		{"contracts column fallback", 0, &three, 3, true, 0},
		{"both absent refused", 0, nil, 0, false, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "swing-qty-" + tc.name,
				Side: "short", EntryPx: 29600, StopPx: 29630, TargetPx: 29540, Kind: "stop_entry", Condition: "SWING4H",
				State: store.StateFilled, SignalID: "sig-" + tc.name, FillPrice: 29600, FillQuantity: tc.fillQty, Contracts: tc.contracts}
			if err := ledger.DB().Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			resetMentorCounters()
			qty, ok := at.mentorSwingFill(mentorLiveArm{RowID: row.ID, Side: "short", Entry: 29600})
			if qty != tc.wantQty || ok != tc.wantOK {
				t.Fatalf("mentorSwingFill = (%v, %v), want (%v, %v)", qty, ok, tc.wantQty, tc.wantOK)
			}
			if got := MentorCountSnapshot()["swing_fill_unknown_qty"]; got != tc.unknownCount {
				t.Fatalf("swing_fill_unknown_qty = %d, want %d", got, tc.unknownCount)
			}
		})
	}
}

// TestMentorClosePositionRefusesUnknownQty (S1 pin): a FILLED swing arm whose
// contracts cannot be attributed (FillQuantity 0, Contracts absent) is refused
// — the close must never guess 1 and strand the rest of a multi-lot swing.
func TestMentorClosePositionRefusesUnknownQty(t *testing.T) {
	at, _, ledger, frames := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "swing-uq",
		Side: "short", EntryPx: 29600, StopPx: 29630, TargetPx: 29540, Kind: "stop_entry", Condition: "SWING4H",
		State: store.StateFilled, SignalID: "swing-uq-sig", FillPrice: 29600, FillQuantity: 0, Contracts: nil}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	mentorRegisterLiveArm("swing-uq", row.ID, "short", 29600)
	resetMentorCounters()
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionClosePosition, ArmID: "swing-uq", Reason: "hold to the 2nd 4h close"}, mentorTierInputs{}, 1000, 1100)
	if sawMentorFrame(t, frames, ntwire.FrameClosePosition, 500*time.Millisecond) {
		t.Fatal("a filled swing with no attributable contracts must NOT close")
	}
	if c := MentorCountSnapshot()["swing_fill_unknown_qty"]; c != 1 {
		t.Fatalf("swing_fill_unknown_qty = %d, want 1", c)
	}
	if c := MentorCountSnapshot()["close_refused_not_filled"]; c != 1 {
		t.Fatalf("close_refused_not_filled = %d, want 1", c)
	}
}
