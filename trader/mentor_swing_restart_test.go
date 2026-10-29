package trader

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// FIX-SWING-RESTART — call-site tests. The production site is
// mentorRebuildLivePosAtBoot → mentorRebuildSwingArm, reached through the
// mentorRebuildBook seam so a test can pin the broker book without a live link.
// A restarted AutoTrader is a FRESH one (empty mentorLiveArms + empty
// evaluator) over the durable store — exactly what a restart leaves behind.

func strPtr(s string) *string { return &s }

// swingRestartArm authors a FILLED SWING4H armed row the way the mentor
// injector does: the ledger scenario is "<armID>-<epoch>".
func swingRestartArm(traderID, signalID, armID string, contracts int, entry, stop, target float64) *store.ArmedOrderDB {
	return &store.ArmedOrderDB{
		TraderID: traderID, PlanID: "mentor:" + traderID, Scenario: armID + "-123456",
		Side: "long", State: store.StateFilled, SignalID: signalID,
		EntryPx: entry, StopPx: stop, TargetPx: target,
		Origin: store.ArmOriginMentor, Condition: "SWING4H",
		Contracts: store.IntPtr(contracts), FillPrice: entry, FillQuantity: contracts,
		ExitMode: strPtr("swing"),
	}
}

// swingRestartPos authors the open position the swing fill left behind.
func swingRestartPos(traderID, signalID string, qty int, entry float64, entryTime int64) *store.TraderPosition {
	return &store.TraderPosition{
		TraderID: traderID, ExchangeID: "NT8", ExchangeType: "ninjatrader",
		Symbol: "MNQ", Side: "LONG", EntryQuantity: float64(qty),
		Quantity: float64(qty), EntryPrice: entry, EntryOrderID: signalID,
		EntryTime: entryTime, Status: "OPEN", Source: "system",
	}
}

const swingRestartOpenTime = int64(1791900000000) // a positive 5m bar open

func TestMentorRebuildSwingRegistersArmAndEvalPos(t *testing.T) {
	at, st := f2Trader(t)
	at.id = "swing-reg"
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	sig := "swing-reg-sig"
	armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
	arm := swingRestartArm(at.id, sig, armID, 3, 31020, 31000, 31080)
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(swingRestartPos(at.id, sig, 3, 31020, swingRestartOpenTime)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{f2StopOrder(sig+"-sl", 31000, 3)}, true, 0)

	rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now())
	if !ran || rebuilt != 1 || refused != 0 {
		t.Fatalf("rebuilt=%d refused=%d ran=%v want 1/0/true", rebuilt, refused, ran)
	}
	// the trader registry entry
	a, ok := at.mentorLiveArmFor(armID)
	if !ok {
		t.Fatal("swing arm not in the trader registry after rebuild")
	}
	if a.RowID != arm.ID || a.Side != "long" || a.Entry != 31020 || a.Scenario != arm.Scenario {
		t.Fatalf("registry entry wrong: %+v", a)
	}
	// the evaluator's swing position
	p := at.mentorEval.State.Swing.Pos
	if p == nil {
		t.Fatal("evaluator swing Pos not rebuilt")
	}
	if p.Side != mentor.SideLong || p.Entry != 31020 || p.Risk != 20 || p.ArmID != armID {
		t.Fatalf("evaluator swing Pos wrong: %+v", p)
	}
	if p.MovedBE {
		t.Fatal("MovedBE must be false (the broker stop is still at the original 31000)")
	}
	if c := at.mentorCountSnapshot()["swing_rebuild_ok"]; c != 1 {
		t.Fatalf("swing_rebuild_ok = %d, want 1", c)
	}
}

// TestMentorRebuildSwingResumesBE — the core pin: after the rebuild the swing's
// BE intent reaches the signal-scoped move wire (the registry lookup succeeds). A mutant that
// skips the swing rebuild leaves the registry empty → the move wire never fires
// and this turns RED.
func TestMentorRebuildSwingResumesBE(t *testing.T) {
	at, st, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	sig := "swing-be-sig"
	armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
	arm := swingRestartArm(at.id, sig, armID, 3, 31020, 31000, 31080)
	if err := ledger.UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(swingRestartPos(at.id, sig, 3, 31020, swingRestartOpenTime)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{f2StopOrder(sig+"-sl", 31000, 3)}, true, 0)
	if rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now()); !ran || rebuilt != 1 || refused != 0 {
		t.Fatalf("rebuilt=%d refused=%d ran=%v want 1/0/true", rebuilt, refused, ran)
	}

	moves := &swingSignalMoves{}
	moves.install(t)
	// the rebuilt swing's bracket is in the broker book (the never-widen judge
	// reads it; the position row was seeded above).
	prevBook := mentorStopBook
	mentorStopBook = func(*nttrader.TCPTrader) (ntwire.OrderSnapshotPayload, time.Time, bool) {
		return ntwire.OrderSnapshotPayload{Orders: []ntwire.NT8Order{f2StopOrder(sig+"-sl", 31000, 3)}}, time.Now(), true
	}
	t.Cleanup(func() { mentorStopBook = prevBook })

	resetMentorCounters(at)
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionMoveStopBE, ArmID: armID, Reason: "swing +1R"}, mentorTierInputs{}, 1000, 1100)
	if moves.n() != 1 || moves.calls[0].stop != 31020 || moves.calls[0].signalID != sig {
		t.Fatalf("the signal-scoped move must be called once with the entry price, got %+v", moves.calls)
	}
	if c := at.mentorCountSnapshot()["move_be_sent"]; c != 1 {
		t.Fatalf("move_be_sent = %d, want 1", c)
	}
}

// TestMentorRebuildSwingResumesHold — the hold-close intent reaches the broker
// after the rebuild. Mutant: skip the swing rebuild → close_refused_unknown_arm.
func TestMentorRebuildSwingResumesHold(t *testing.T) {
	at, st, ledger, frames := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	sig := "swing-hold-sig"
	armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
	arm := swingRestartArm(at.id, sig, armID, 3, 31020, 31000, 31080)
	if err := ledger.UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(swingRestartPos(at.id, sig, 3, 31020, swingRestartOpenTime)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{f2StopOrder(sig+"-sl", 31000, 3)}, true, 0)
	if rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now()); !ran || rebuilt != 1 || refused != 0 {
		t.Fatalf("rebuilt=%d refused=%d ran=%v want 1/0/true", rebuilt, refused, ran)
	}

	resetMentorCounters(at)
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionClosePosition, ArmID: armID, Reason: "hold to the 2nd 4h close"}, mentorTierInputs{}, 1000, 1100)
	if !sawMentorFrame(t, frames, ntwire.FrameClosePosition, 1*time.Second) {
		t.Fatal("ClosePosition must reach the broker after the rebuild")
	}
	if c := at.mentorCountSnapshot()["close_sent"]; c != 1 {
		t.Fatalf("close_sent = %d, want 1", c)
	}
}

// TestMentorRebuildSwingNoLiveStopRefused — no live protective stop in the book
// refuses the WHOLE position (the existing rebuild's fail-closed path) and the
// swing is never registered: its stop stays exactly where NT8 has it.
func TestMentorRebuildSwingNoLiveStopRefused(t *testing.T) {
	at, st := f2Trader(t)
	at.id = "swing-nostop"
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	sig := "swing-nostop-sig"
	armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
	arm := swingRestartArm(at.id, sig, armID, 3, 31020, 31000, 31080)
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(swingRestartPos(at.id, sig, 3, 31020, swingRestartOpenTime)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{f2TPOrder(sig+"-tp", 31080)}, true, 0) // no -sl

	rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now())
	if !ran || rebuilt != 0 || refused != 1 {
		t.Fatalf("rebuilt=%d refused=%d ran=%v want 0/1/true", rebuilt, refused, ran)
	}
	if _, ok := at.mentorLiveArmFor(armID); ok {
		t.Fatal("swing arm must NOT be registered when the live stop is missing")
	}
	if at.mentorEval.State.Swing.Pos != nil {
		t.Fatal("evaluator swing Pos must NOT be rebuilt when the live stop is missing")
	}
	alerts, _ := st.Alert().List(at.id, 100)
	for _, a := range alerts {
		if a.Kind == "mentor-restart-rebuild" {
			return // the existing fail-closed alert fired
		}
	}
	t.Fatal("no P0 mentor-restart-rebuild alert raised")
}

// TestMentorRebuildSwingPerTrader — two traders' swings rebuild into their OWN
// registries (the key is traderID|armID), never into each other's.
func TestMentorRebuildSwingPerTrader(t *testing.T) {
	armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
	a, sta := f2Trader(t)
	a.id = "swing-trader-a"
	a.mentorEval = mentor.New(mentor.DefaultConfig())
	sigA := "swing-a-sig"
	armA := swingRestartArm(a.id, sigA, armID, 3, 31020, 31000, 31080)
	if err := sta.ArmedOrders().UpsertArm(armA); err != nil {
		t.Fatal(err)
	}
	if err := sta.Position().CreateOpenPosition(swingRestartPos(a.id, sigA, 3, 31020, swingRestartOpenTime)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{f2StopOrder(sigA+"-sl", 31000, 3)}, true, 0)
	if rebuilt, refused, ran := a.mentorRebuildLivePosAtBoot(time.Now()); !ran || rebuilt != 1 || refused != 0 {
		t.Fatalf("A rebuilt=%d refused=%d ran=%v want 1/0/true", rebuilt, refused, ran)
	}

	b, stb := f2Trader(t)
	b.id = "swing-trader-b"
	b.mentorEval = mentor.New(mentor.DefaultConfig())
	sigB := "swing-b-sig"
	armB := swingRestartArm(b.id, sigB, armID, 2, 31020, 31000, 31080)
	if err := stb.ArmedOrders().UpsertArm(armB); err != nil {
		t.Fatal(err)
	}
	if err := stb.Position().CreateOpenPosition(swingRestartPos(b.id, sigB, 2, 31020, swingRestartOpenTime)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{f2StopOrder(sigB+"-sl", 31000, 2)}, true, 0)
	if rebuilt, refused, ran := b.mentorRebuildLivePosAtBoot(time.Now()); !ran || rebuilt != 1 || refused != 0 {
		t.Fatalf("B rebuilt=%d refused=%d ran=%v want 1/0/true", rebuilt, refused, ran)
	}

	// A resolves its OWN arm; B resolves its OWN — the registry key is
	// traderID|armID, so a lookup can never cross traders.
	aa, ok := a.mentorLiveArmFor(armID)
	if !ok || aa.TraderID != a.id || aa.Entry != 31020 {
		t.Fatalf("A's swing not in A's registry: %+v ok=%v", aa, ok)
	}
	bb, ok := b.mentorLiveArmFor(armID)
	if !ok || bb.TraderID != b.id || bb.Entry != 31020 {
		t.Fatalf("B's swing not in B's registry: %+v ok=%v", bb, ok)
	}
	if aa.TraderID == bb.TraderID {
		t.Fatal("A and B must hold entries under DIFFERENT trader ids")
	}
}

// TestMentorRebuildSwingContractMismatchRefused — a position whose size does not
// match the row's filled contracts is refused (the swing's own close would size
// from a different trade).
func TestMentorRebuildSwingContractMismatchRefused(t *testing.T) {
	at, st := f2Trader(t)
	at.id = "swing-mismatch"
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	sig := "swing-mismatch-sig"
	armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
	arm := swingRestartArm(at.id, sig, armID, 3, 31020, 31000, 31080) // row says 3
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(swingRestartPos(at.id, sig, 2, 31020, swingRestartOpenTime)); err != nil { // position says 2
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{f2StopOrder(sig+"-sl", 31000, 2)}, true, 0)

	rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now())
	if !ran || rebuilt != 1 || refused != 0 {
		t.Fatalf("rebuilt=%d refused=%d ran=%v want 1/0/true (the 1m rebuild still succeeds)", rebuilt, refused, ran)
	}
	if _, ok := at.mentorLiveArmFor(armID); ok {
		t.Fatal("swing arm must NOT be registered on a contract mismatch")
	}
	if at.mentorEval.State.Swing.Pos != nil {
		t.Fatal("evaluator swing Pos must NOT be rebuilt on a contract mismatch")
	}
	if c := at.mentorCountSnapshot()["swing_rebuild_refused"]; c != 1 {
		t.Fatalf("swing_rebuild_refused = %d, want 1", c)
	}
	alerts, _ := st.Alert().List(at.id, 100)
	for _, a := range alerts {
		if a.Kind == "mentor-swing-restart" {
			return
		}
	}
	t.Fatal("no P0 mentor-swing-restart alert raised")
}

// swingOpenTimeCT is the real-UTC epoch ms of a CT wall-clock 5m bar open
// (America/Chicago — CDT -5 in September, the same anchor the kernel's
// fourHBucketStart uses).
func swingOpenTimeCT(day, hour, minute int) int64 {
	return time.Date(2026, 9, day, hour, minute, 0, 0, kernel.CTLocation()).UnixMilli()
}

// TestMentorRebuildSwingMovedBEReadsBroker — fold M2: a swing whose live NT8
// stop is ALREADY at entry rebuilds with MovedBE=true (read from the broker,
// never forced). A mutant that forces MovedBE false in RebuildSwingPos makes
// this RED.
func TestMentorRebuildSwingMovedBEReadsBroker(t *testing.T) {
	at, st := f2Trader(t)
	at.id = "swing-movedbe"
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	sig := "swing-movedbe-sig"
	armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
	arm := swingRestartArm(at.id, sig, armID, 3, 31020, 31000, 31080)
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(swingRestartPos(at.id, sig, 3, 31020, swingRestartOpenTime)); err != nil {
		t.Fatal(err)
	}
	// the broker stop is already AT entry (BE done before the restart)
	f2PinBook(t, []ntwire.NT8Order{f2StopOrder(sig+"-sl", 31020, 3)}, true, 0)
	if rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now()); !ran || rebuilt != 1 || refused != 0 {
		t.Fatalf("rebuilt=%d refused=%d ran=%v want 1/0/true", rebuilt, refused, ran)
	}
	p := at.mentorEval.State.Swing.Pos
	if p == nil {
		t.Fatal("evaluator swing Pos not rebuilt")
	}
	if !p.MovedBE {
		t.Fatal("MovedBE must be true — the live stop is already at entry")
	}
}

// TestMentorRebuildSwingEntryBucketIsArmBarBucket — fold M3: the evaluator's
// EntryBucket is the 4h bucket of the swing's own 5m bar (the ArmID
// "swing-<open>"), so a bar 3h55m into its bucket resolves to the BUCKET START,
// not the bar's round hour. A mutant that shifts EntryBucket one 4h candle late
// (+4h) makes this RED.
func TestMentorRebuildSwingEntryBucketIsArmBarBucket(t *testing.T) {
	at, st := f2Trader(t)
	at.id = "swing-bucket"
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	sig := "swing-bucket-sig"
	openTime := swingOpenTimeCT(15, 8, 55) // 08:55 = 3h55m into the 05:00 bucket
	armID := "swing-" + strconv.FormatInt(openTime, 10)
	arm := swingRestartArm(at.id, sig, armID, 3, 31020, 31000, 31080)
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(swingRestartPos(at.id, sig, 3, 31020, openTime)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{f2StopOrder(sig+"-sl", 31000, 3)}, true, 0)
	if rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now()); !ran || rebuilt != 1 || refused != 0 {
		t.Fatalf("rebuilt=%d refused=%d ran=%v want 1/0/true", rebuilt, refused, ran)
	}
	want := swingOpenTimeCT(15, 5, 0) // the 05:00 bucket start
	if got := at.mentorEval.State.Swing.Pos.EntryBucket; got != want {
		t.Fatalf("EntryBucket = %d, want %d (the 05:00 bucket, not 08:00 or +4h)", got, want)
	}
}

// TestMentorRebuildSwingArmForeignTraderRefused — fold M5: a row whose TraderID
// is not the caller's is refused at the trader-id check (fail-closed, never a
// cross-trader registration). A mutant that drops the check returns true.
func TestMentorRebuildSwingArmForeignTraderRefused(t *testing.T) {
	at, _ := f2Trader(t)
	at.id = "swing-owner"
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	foreign := swingRestartArm("swing-foreign", "swing-f-sig", "swing-"+strconv.FormatInt(swingRestartOpenTime, 10), 3, 31020, 31000, 31080)
	pos := swingRestartPos("swing-foreign", "swing-f-sig", 3, 31020, swingRestartOpenTime)
	lp := &mentorLivePos{Pos: mentorPosition{Side: "long", Entry: 31020, Stop: 31000, Target: 31080, R: 20}}
	if ok, why := at.mentorRebuildSwingArm(*foreign, pos, lp); ok {
		t.Fatal("a foreign-trader row must be refused")
	} else if why == "" || !strings.Contains(why, "trader") {
		t.Fatalf("refusal must name the trader mismatch, got %q", why)
	}
	if _, ok := at.mentorLiveArmFor("swing-" + strconv.FormatInt(swingRestartOpenTime, 10)); ok {
		t.Fatal("a foreign-trader row must never register under the caller")
	}
}

// TestMentorRebuildSwingDegenerateRiskRefused — fold M6/M7: a degenerate swing
// (original stop == entry → risk 0) is refused at the call site: the arm is
// unregistered (never half-rebuilt), a P0 alert fires and swing_rebuild_refused
// is counted. A mutant that drops the unregister, or that accepts risk 0, makes
// this RED.
func TestMentorRebuildSwingDegenerateRiskRefused(t *testing.T) {
	at, st := f2Trader(t)
	at.id = "swing-degenerate"
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	sig := "swing-deg-sig"
	armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
	arm := swingRestartArm(at.id, sig, armID, 3, 31020, 31020, 31080) // stop == entry → risk 0
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(swingRestartPos(at.id, sig, 3, 31020, swingRestartOpenTime)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{f2StopOrder(sig+"-sl", 31020, 3)}, true, 0)
	rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now())
	if !ran || rebuilt != 1 || refused != 0 {
		t.Fatalf("rebuilt=%d refused=%d ran=%v want 1/0/true (the 1m rebuild still succeeds)", rebuilt, refused, ran)
	}
	if _, ok := at.mentorLiveArmFor(armID); ok {
		t.Fatal("a degenerate swing must NOT stay in mentorLiveArms")
	}
	if at.mentorEval.State.Swing.Pos != nil {
		t.Fatal("a degenerate swing must NOT rebuild the evaluator Pos")
	}
	if c := at.mentorCountSnapshot()["swing_rebuild_refused"]; c != 1 {
		t.Fatalf("swing_rebuild_refused = %d, want 1", c)
	}
	alerts, _ := st.Alert().List(at.id, 100)
	for _, a := range alerts {
		if a.Kind == "mentor-swing-restart" {
			return
		}
	}
	t.Fatal("no P0 mentor-swing-restart alert raised")
}
