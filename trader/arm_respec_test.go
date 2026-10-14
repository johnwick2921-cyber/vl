package trader

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── W1b E1 + E2 — a working arm under a NEW plan version ────────────────────
//
// E1: the churn guard compared the new bracket with itself (the row was built
// from the new leg and only its id was copied from the ledger), so a working
// order kept the stop and target it was placed with through every re-spec.
// E2: nothing compared a working market_in_zone limit with the CURRENT
// version's zone, so a moved zone left the old limit resting until the
// 30-minute rest cap. Both now go through ONE function (arm_respec.go) that
// asks for at most one cancel per row, through the filled-arm guard; the
// normal authoring then re-arms under the new version once the broker's book
// confirms the cancel. Every test below drives maybeManageArmedOrdersAt over
// the real TCP wire (canon 53).

// respecTape is zoneTape with a chosen bar half-range: a wider range raises
// ATR5m, which moves the composed market_in_zone stop (near − 1.5×ATR5m)
// WITHOUT any plan change — the drift the version gate exists to ignore.
func respecTape(last float64, now time.Time, half float64) []market.Kline {
	out := zoneTape(last, now, 0)
	for i := range out {
		out[i].High = out[i].Close + half
		out[i].Low = out[i].Close - half
	}
	return out
}

// restingBook is the AddOn's snapshot with the entry `sid` resting at px.
func (r *zoneRig) restingBook(at time.Time, sid string, px float64) {
	r.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{
		{OrderID: "o-" + sid, Name: sid, Symbol: "MNQ", Action: "buy", Type: "limit", LimitPrice: px, Quantity: 1, State: "Working"},
	}}, at)
}

// persistFlat is the PERSISTED empty snapshot the settlement pass reads as
// evidence that the cancelled order is gone.
func (r *zoneRig) persistFlat(at time.Time) {
	r.t.Helper()
	if err := r.st.NT8OrderSnapshots().Insert(&store.NT8OrderSnapshot{Account: "Sim101", OrdersJSON: "[]", ReceivedMs: at.UnixMilli()}); err != nil {
		r.t.Fatal(err)
	}
}

// placeWorking runs the first pass, returns the placed signal, and moves the
// row to working through the production order_update handler.
func (r *zoneRig) placeWorking(wantLimit float64) ntwire.SignalPayload {
	r.t.Helper()
	r.setTape(zoneTape(101.95, r.now, 0)) // beyond the zone: rests at the far edge
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	sigs, _ := r.drain()
	if len(sigs) != 1 || sigs[0].LimitPrice != wantLimit {
		r.t.Fatalf("fixture: want one limit at %.2f, got %+v", wantLimit, sigs)
	}
	r.at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: sigs[0].SignalID, State: "Working", Account: "Sim101"}, r.st.ArmedOrders())
	if row := r.row("S1"); row.State != store.StateWorking || row.Version != 1 {
		r.t.Fatalf("fixture: the placed row must be working under v1: %+v", row)
	}
	return sigs[0]
}

// newVersion appends the next plan version (the provider clock stays fixed).
func (r *zoneRig) newVersion(scs ...kernel.PlanScenario) {
	r.t.Helper()
	blob, _ := json.Marshal(zoneDoc(scs...))
	shadowPlanAtTime(r.t, r.at, r.st, string(blob), r.now)
}

// settleAndReArm confirms the requested cancel from a fresh persisted book at
// t2, then runs the re-arm pass at t3 on a flat book and returns its frames.
func (r *zoneRig) settleAndReArm() ([]ntwire.SignalPayload, []ntwire.CancelOrderPayload) {
	r.t.Helper()
	t2 := r.now.Add(2 * time.Minute)
	r.persistFlat(t2)
	r.flatBook(t2)
	r.setTape(zoneTape(101.95, t2, 0))
	r.at.maybeManageArmedOrdersAt(nil, t2)
	if sigs, cancels := r.drain(); len(sigs) != 0 || len(cancels) != 0 {
		r.t.Fatalf("the settlement pass must send nothing: sigs=%+v cancels=%+v", sigs, cancels)
	}
	var first store.ArmedOrderDB
	for _, x := range r.rows() {
		if x.Scenario == "S1" && x.PlacementSeq == 0 {
			first = x
		}
	}
	if first.State != store.StateCancelled {
		r.t.Fatalf("a fresh flat persisted book must settle the re-spec cancel: %+v", first)
	}
	// The adapter's B3 duplicate guard runs on the WALL clock; the fixture
	// clock says minutes passed, so the adapter is re-made to stand for that.
	r.at.trader = ntTrader.NewTCPTrader(r.srv, "MNQ", "Sim101")
	t3 := r.now.Add(3 * time.Minute)
	r.flatBook(t3)
	r.setTape(zoneTape(101.95, t3, 0))
	r.at.maybeManageArmedOrdersAt(nil, t3)
	return r.drain()
}

func onGridWithin(got, want, tick float64) bool {
	steps := got / tick
	return math.Abs(steps-math.Round(steps)) < 1e-6 && math.Abs(got-want) <= tick+1e-9
}

// E1 — a new version that moves the AUTHORED bracket ≥ 2 ticks (SL 98 → 96,
// TP 110 → 112 in the plan doc) cancels the working
// order (the AddOn cannot modify a resting entry's bracket) and the scenario
// re-arms under the new version with the NEW stop and target.
func TestWorkingArmBracketRespecByNewVersionReplacesTheOrder(t *testing.T) {
	r := newZoneRig(t, "w1b-respec-e1", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	first := r.placeWorking(100.5)
	sid := first.SignalID
	if first.StopLoss <= 96.5 {
		t.Fatalf("fixture: the v1 stop must sit above the v2 stop by ≥ 2 ticks, got %.2f", first.StopLoss)
	}
	before, _ := store.SystemCounter(r.st, "arm:respec_cancel")

	// v2: the same zone, the bracket re-spec'd to SL 96 / TP 112.
	sc := zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)
	sc.Arm.Stop, sc.Arm.Target = 96, 112
	sc.TargetChain = []float64{112}
	r.newVersion(sc)
	t1 := r.now.Add(time.Minute)
	r.restingBook(t1, sid, 100.5)
	r.setTape(zoneTape(101.95, t1, 0))
	r.at.maybeManageArmedOrdersAt(nil, t1)
	sigs, cancels := r.drain()
	if len(cancels) != 1 || cancels[0].SignalID != sid || len(sigs) != 0 {
		t.Fatalf("a re-spec'd bracket must cancel the working order %s exactly once and place nothing: sigs=%+v cancels=%+v", sid, sigs, cancels)
	}
	row := r.row("S1")
	if row.State != store.StateCancelPending || !strings.Contains(row.StateReason, "bracket re-spec by v2: authored SL 98.00→96.00 TP 110.00→112.00") {
		t.Fatalf("the working row must be cancel_pending naming the AUTHORED change 'bracket re-spec by v2: authored SL 98.00→96.00 TP 110.00→112.00': %+v", row)
	}
	if n, _ := store.SystemCounter(r.st, "arm:respec_cancel"); n != before+1 {
		t.Fatalf("the re-spec cancel must be counted once (%d → %d)", before, n)
	}

	sigs, cancels = r.settleAndReArm()
	if len(cancels) != 0 || len(sigs) != 1 {
		t.Fatalf("the settled scenario must re-arm and place once under v2: sigs=%+v cancels=%+v", sigs, cancels)
	}
	if !onGridWithin(sigs[0].StopLoss, 96, 0.25) || sigs[0].StopLoss > 96 || !onGridWithin(sigs[0].TakeProfit, 112, 0.25) {
		t.Fatalf("the re-placed order must carry the v2 bracket (SL ≤ 96 on grid, TP 112): %+v", sigs[0])
	}
	if sigs[0].SignalID == sid {
		t.Fatalf("the re-placement must be a NEW signal, got the old %s", sid)
	}
	rows := r.rows()
	if len(rows) != 2 {
		t.Fatalf("the re-arm mints ONE successor row: %+v", rows)
	}
	nw := r.row("S1")
	if nw.PlacementSeq != 1 || nw.Version != 2 || nw.SignalID != sigs[0].SignalID {
		t.Fatalf("the successor must be placement_seq 1 under v2 carrying the new signal: %+v", nw)
	}
}

// E2 — a new version whose zone no longer contains the working limit cancels
// it ("zone moved by v2") and re-arms at the NEW zone's far edge.
func TestZoneMovedByNewVersionCancelsTheWorkingLimitAndReArms(t *testing.T) {
	r := newZoneRig(t, "w1b-respec-e2", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	// F-1 freshness (planner-touch-fills): the rig's plans are born at now−30m
	// and its rising tape rejects the new v2 band from the first post-birth bar,
	// so at the t3 re-arm that trigger is 32 min old > the 30-min default
	// (zone_rest_max_min) and would be held STALE. This test is the zone-move
	// re-arm, not freshness: the bound is widened to 60 for it.
	sixty := 60
	r.at.config.StrategyConfig.DayPlan.ZoneRestMaxMin = &sixty
	sid := r.placeWorking(100.5).SignalID
	before, _ := store.SystemCounter(r.st, "market_in_zone:zone_moved")

	r.newVersion(zoneScenario("S1", kernel.EntryPolicyMarketInZone, []float64{99.0, 100.0}, false))
	t1 := r.now.Add(time.Minute)
	r.restingBook(t1, sid, 100.5)
	r.setTape(zoneTape(101.95, t1, 0))
	r.at.maybeManageArmedOrdersAt(nil, t1)
	sigs, cancels := r.drain()
	if len(cancels) != 1 || cancels[0].SignalID != sid || len(sigs) != 0 {
		t.Fatalf("a moved zone must cancel the working limit %s exactly once and place nothing: sigs=%+v cancels=%+v", sid, sigs, cancels)
	}
	row := r.row("S1")
	if row.State != store.StateCancelPending || !strings.Contains(row.StateReason, "zone moved by v2") {
		t.Fatalf("the working row must be cancel_pending 'zone moved by v2': %+v", row)
	}
	if n, _ := store.SystemCounter(r.st, "market_in_zone:zone_moved"); n != before+1 {
		t.Fatalf("the zone-moved cancel must be counted once (%d → %d)", before, n)
	}

	sigs, cancels = r.settleAndReArm()
	if len(cancels) != 0 || len(sigs) != 1 || sigs[0].LimitPrice != 100.0 {
		t.Fatalf("the settled scenario must re-arm at the NEW far edge 100.00: sigs=%+v cancels=%+v", sigs, cancels)
	}
	rows := r.rows()
	if len(rows) != 2 {
		t.Fatalf("the re-arm mints ONE successor row: %+v", rows)
	}
	nw := r.row("S1")
	if nw.PlacementSeq != 1 || nw.Version != 2 || nw.State != store.StatePlacePending || nw.EntryPx != 100.0 {
		t.Fatalf("the successor must be placement_seq 1, v2, place_pending at 100.00: %+v", nw)
	}
}

// The version gate: live ATR moves the composed stop by ≥ 2 ticks inside ONE
// plan version — that is not a re-spec, and nothing is sent.
func TestRespecIgnoresATRDriftWithinOneVersion(t *testing.T) {
	r := newZoneRig(t, "w1b-respec-atr", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	first := r.placeWorking(100.5)
	t1 := r.now.Add(time.Minute)
	r.restingBook(t1, first.SignalID, 100.5)
	r.setTape(respecTape(101.95, t1, 0.8)) // ATR5m up → composed stop ≥ 2 ticks lower
	r.at.maybeManageArmedOrdersAt(nil, t1)
	if sigs, cancels := r.drain(); len(sigs) != 0 || len(cancels) != 0 {
		t.Fatalf("ATR drift inside one version must send nothing: sigs=%+v cancels=%+v", sigs, cancels)
	}
	if row := r.row("S1"); row.State != store.StateWorking || row.Version != 1 {
		t.Fatalf("the working row must stay working under v1: %+v", row)
	}
}

// A new version whose zone still contains the limit and whose bracket is
// unchanged leaves the working order alone.
func TestRespecLeavesAContainedUnchangedOrderAlone(t *testing.T) {
	r := newZoneRig(t, "w1b-respec-same", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	sid := r.placeWorking(100.5).SignalID
	r.newVersion(zoneScenario("S1", kernel.EntryPolicyMarketInZone, []float64{99.5, 100.75}, false))
	t1 := r.now.Add(time.Minute)
	r.restingBook(t1, sid, 100.5)
	r.setTape(zoneTape(101.95, t1, 0))
	r.at.maybeManageArmedOrdersAt(nil, t1)
	if sigs, cancels := r.drain(); len(sigs) != 0 || len(cancels) != 0 {
		t.Fatalf("a contained limit with an unchanged bracket must send nothing: sigs=%+v cancels=%+v", sigs, cancels)
	}
	if row := r.row("S1"); row.State != store.StateWorking {
		t.Fatalf("the working row must stay working: %+v", row)
	}
}

// The filled-arm guard decides: a book that shows only the signal's bracket
// children (the entry filled) REFUSES the cancel — nothing is sent, the row
// stays working — and the next pass, on a book with the entry resting, sends it.
func TestRespecCancelRefusedByTheFilledArmGuardRetriesNextPass(t *testing.T) {
	r := newZoneRig(t, "w1b-respec-guard", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	sid := r.placeWorking(100.5).SignalID
	r.newVersion(zoneScenario("S1", kernel.EntryPolicyMarketInZone, []float64{99.0, 100.0}, false))
	t1 := r.now.Add(time.Minute)
	r.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{
		{OrderID: "sl", Name: sid + "-SL", Symbol: "MNQ", Action: "sell", Type: "stop", StopPrice: 97.5, Quantity: 1, State: "Working"},
	}}, t1)
	r.setTape(zoneTape(101.95, t1, 0))
	r.at.maybeManageArmedOrdersAt(nil, t1)
	if _, cancels := r.drain(); len(cancels) != 0 {
		t.Fatalf("the filled-arm guard must refuse a cancel that would reach the protections: %+v", cancels)
	}
	if row := r.row("S1"); row.State != store.StateWorking {
		t.Fatalf("a refused cancel leaves the row working: %+v", row)
	}
	t2 := t1.Add(30 * time.Second)
	r.restingBook(t2, sid, 100.5)
	r.setTape(zoneTape(101.95, t2, 0))
	r.at.maybeManageArmedOrdersAt(nil, t2)
	if _, cancels := r.drain(); len(cancels) != 1 || cancels[0].SignalID != sid {
		t.Fatalf("the next pass, entry resting, must send the cancel once: %+v", cancels)
	}
}

// The refresh-write skip (critic correction to E1 step 6): a non-armed row's
// refresh write is skipped ONLY when it carries the same opportunity. A
// WORKING row holding opportunity A under a key a later version gives to B
// still reaches UpsertArm, so W5 R13(a)'s typed refusal is named (WARN +
// counter, once) and B's admit is withdrawn — nothing is placed for B, and
// A's working row is neither rewritten nor cancelled (it is a Picture row).
func TestRespecSkipKeepsTheTypedSourceRefusalForAWorkingRow(t *testing.T) {
	r, epoch := newPicRig(t, "w1b-respec-r13", nil)
	scA := picScenario("P1", "opp-respec-a", r.now, epoch, picDefault)
	picPlan(r, scA)
	seed := &store.ArmedOrderDB{TraderID: r.at.id, PlanID: r.pid, Version: 1, Session: "TEST", Scenario: "P1", Side: "long",
		EntryPx: 100.5, StopPx: 97, TargetPx: 110, State: store.StateArmed, EntryClass: "armed_fill", Kind: "limit",
		Policy: store.ArmPolicyMarketInZone, CreatedAt: r.now, UpdatedAt: r.now}
	stampPictureSource(seed, scA)
	ledger := r.st.ArmedOrders()
	if err := ledger.UpsertArm(seed); err != nil {
		t.Fatal(err)
	}
	a := r.rows()[0]
	if err := ledger.BeginPlacement(a.ID, "sig-respec-a"); err != nil {
		t.Fatal(err)
	}
	if err := ledger.ApplyPlacementReceipt(r.at.id, "sig-respec-a", store.StateWorking, "fixture"); err != nil {
		t.Fatal(err)
	}
	if row := r.rows()[0]; row.State != store.StateWorking || row.SourceRef != "opp-respec-a" {
		t.Fatalf("fixture: A must be working under its own opportunity: %+v", row)
	}
	g := picDefault
	g.stop = 96.5
	picPlan(r, picScenario("P1", "opp-respec-b", r.now, epoch, g)) // v2: B under A's id
	logs := captureTraderLog(t)
	workingBook(r, r.now.Add(5*time.Second), "sig-respec-a", 100.5)
	picPass(r, 5*time.Second, 100.25)
	workingBook(r, r.now.Add(10*time.Second), "sig-respec-a", 100.5)
	picPass(r, 10*time.Second, 100.25)
	if sigs, cancels := r.drain(); len(sigs) != 0 || len(cancels) != 0 {
		t.Fatalf("nothing may be placed or cancelled on B's admission of A's working row: sigs=%+v cancels=%+v", sigs, cancels)
	}
	if after := r.rows(); len(after) != 1 || after[0].State != store.StateWorking || after[0].SourceRef != "opp-respec-a" || after[0].StopPx != 97 {
		t.Fatalf("A's working row must be untouched: %+v", after)
	}
	if n := strings.Count(logs.String(), "NOT authored — arm_source_mismatch"); n != 1 {
		t.Fatalf("the typed refusal must still be named once for a WORKING row, got %d:\n%s", n, logs.String())
	}
	if n := store.ArmRefusalCount(r.st, r.at.id, "2026-09-11", "TEST", "arm_source_mismatch"); n != 1 {
		t.Fatalf("the typed refusal is counted once: %d", n)
	}
}

// The predicate's edges (pure): only a WORKING planner row with a signal is
// ever judged, and a version bump that both moves the zone and re-specs the
// bracket yields ONE decision (E2 first) — at most one cancel per row. E1
// compares AUTHORED brackets (the prior version's, read lazily, vs this
// version's); the composed prices never decide, and an absent prior is named,
// never read as a change.
func TestArmRespecForEdges(t *testing.T) {
	zl := zoneLeg{on: true, v: kernel.ZoneVerdict{Lo: 99, Hi: 100, Far: 100, Near: 99}}
	authored := kernel.PlanArmLeg{Entry: 100, Stop: 96, Target: 112} // v2's plan doc
	composed := kernel.PlanArmLeg{Entry: 100, Stop: 95.5, Target: 112}
	wasLeg := kernel.PlanArmLeg{Entry: 100, Stop: 98, Target: 110} // v1's plan doc
	reads := 0
	was := func(l kernel.PlanArmLeg, why string) func() (kernel.PlanArmLeg, string) {
		return func() (kernel.PlanArmLeg, string) { reads++; return l, why }
	}
	base := store.ArmedOrderDB{State: store.StateWorking, SignalID: "sig", Version: 1, Policy: kernel.EntryPolicyMarketInZone,
		EntryPx: 100.5, StopPx: 97.7, TargetPx: 110}
	if c, _, ok := armRespecFor(2, base, authored, composed, zl, 0.25, was(wasLeg, "")); !ok || !strings.HasPrefix(c.reason, "zone moved by v2") || c.counter != "market_in_zone:zone_moved" || reads != 0 {
		t.Fatalf("zone moved AND bracket re-spec'd → ONE decision, the zone's, with no history read: %+v %v reads=%d", c, ok, reads)
	}
	for name, mut := range map[string]func(*store.ArmedOrderDB){
		"armed":          func(r *store.ArmedOrderDB) { r.State = store.StateArmed },
		"place_pending":  func(r *store.ArmedOrderDB) { r.State = store.StatePlacePending },
		"cancel_pending": func(r *store.ArmedOrderDB) { r.State = store.StateCancelPending },
		"no signal":      func(r *store.ArmedOrderDB) { r.SignalID = " " },
		"picture row":    func(r *store.ArmedOrderDB) { r.Source = "picture" },
		"picture row ws": func(r *store.ArmedOrderDB) { r.Source = " picture " },
	} {
		p := base
		mut(&p)
		if c, _, ok := armRespecFor(2, p, authored, composed, zl, 0.25, was(wasLeg, "")); ok {
			t.Fatalf("%s: must never be judged, got %+v", name, c)
		}
	}
	in := base
	in.EntryPx = 100
	if c, _, ok := armRespecFor(2, in, authored, composed, zl, 0.25, was(wasLeg, "")); !ok ||
		!strings.HasPrefix(c.reason, "bracket re-spec by v2: authored SL 98.00→96.00 TP 110.00→112.00 (v1→v2; resting SL 97.70 TP 110.00, composed now SL 95.50 TP 112.00)") {
		t.Fatalf("a contained limit whose AUTHORED bracket moved under a newer version is E1: %+v %v", c, ok)
	}
	reads = 0
	if c, _, ok := armRespecFor(1, in, authored, composed, zl, 0.25, was(wasLeg, "")); ok || reads != 0 {
		t.Fatalf("the same version never re-specs a bracket and never reads history: %+v reads=%d", c, reads)
	}
	legacy := in
	legacy.Policy = ""
	if c, _, ok := armRespecFor(2, legacy, authored, composed, zoneLeg{}, 0.25, was(wasLeg, "")); !ok || c.counter != "arm:respec_cancel" {
		t.Fatalf("a legacy working row whose authored bracket a newer version moved is E1 too: %+v %v", c, ok)
	}
	// THE REPAIR: the authored bracket did not move (< 2 ticks), the COMPOSED one
	// moved a full point from the resting order — ATR drift, not a re-spec.
	same := kernel.PlanArmLeg{Entry: 100, Stop: 97.75, Target: 110.25}
	drift := kernel.PlanArmLeg{Entry: 100, Stop: 96.7, Target: 110}
	if c, _, ok := armRespecFor(2, in, same, drift, zl, 0.25, was(wasLeg, "")); ok {
		t.Fatalf("a < 2-tick AUTHORED move is not a re-spec, however far the composed stop drifted: %+v", c)
	}
	// Absent prior → no decision, the reason is handed back (absent ≠ changed).
	if c, absent, ok := armRespecFor(2, in, authored, composed, zl, 0.25, was(kernel.PlanArmLeg{}, "v1 carried no scenario S1")); ok || absent != "v1 carried no scenario S1" {
		t.Fatalf("an absent prior authored leg must not fire and must be named: %+v %v %q", c, ok, absent)
	}
	if c, _, ok := armRespecFor(2, in, authored, composed, zl, 0.25, nil); ok {
		t.Fatalf("no history reader → no E1: %+v", c)
	}
}

// THE VERIFIER'S BLOCKING PROBE, pinned (W1b E1 repair): an IDENTICAL scenario
// re-published as v2 plus a live-ATR tape wide enough to move the COMPOSED
// stop ≥ 2 ticks is NOT a re-spec — the plan's authored bracket did not move.
// Nothing is sent, the row stays working under v1, and arm:respec_cancel is
// not incremented (class 35: counters record, never infer).
func TestArmRespecIgnoresATRDriftAcrossVersions(t *testing.T) {
	r := newZoneRig(t, "w1b-respec-atr-v2", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	first := r.placeWorking(100.5)
	before, _ := store.SystemCounter(r.st, "arm:respec_cancel")
	r.newVersion(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)) // v2: byte-identical scenario
	t1 := r.now.Add(time.Minute)
	r.restingBook(t1, first.SignalID, 100.5)
	r.setTape(respecTape(101.95, t1, 0.8)) // ATR5m up → composed stop ≥ 2 ticks lower
	r.at.maybeManageArmedOrdersAt(nil, t1)
	if sigs, cancels := r.drain(); len(sigs) != 0 || len(cancels) != 0 {
		t.Fatalf("ATR drift across versions with an unchanged authored bracket must send nothing: sigs=%d cancels=%d %+v", len(sigs), len(cancels), cancels)
	}
	if row := r.row("S1"); row.State != store.StateWorking || row.Version != 1 {
		t.Fatalf("the working row must stay working under v1: %+v", row)
	}
	if n, _ := store.SystemCounter(r.st, "arm:respec_cancel"); n != before {
		t.Fatalf("arm:respec_cancel must record only a re-spec that happened (%d → %d)", before, n)
	}
}

// E1 on a LEGACY (policy "") row at the production call site (verifier item
// 2): v2 moves the AUTHORED stop 98 → 96 (≥ 2 ticks) → the working limit is
// cancelled "bracket re-spec by v2: authored SL 98.00→96.00", counted once;
// a fresh PERSISTED flat book (st.NT8OrderSnapshots) settles it, and the next
// pass mints placement_seq 1 under v2 and places a NEW signal at the legacy
// limit 100.00 (no D15 pin on a legacy row; the tape sits in the tick band).
func TestLegacyWorkingArmAuthoredRespecByNewVersionReplacesTheOrder(t *testing.T) {
	r := newZoneRig(t, "w1b-respec-legacy", zoneDoc(zoneScenario("S1", "", zone, false)))
	first := r.placeWorking(100)
	sid := first.SignalID
	if row := r.row("S1"); row.Policy != "" {
		t.Fatalf("fixture: the row must be legacy (no policy): %+v", row)
	}
	before, _ := store.SystemCounter(r.st, "arm:respec_cancel")
	sc := zoneScenario("S1", "", zone, false)
	sc.Arm.Stop = 96
	r.newVersion(sc)
	t1 := r.now.Add(time.Minute)
	r.restingBook(t1, sid, 100)
	r.setTape(zoneTape(101.95, t1, 0))
	r.at.maybeManageArmedOrdersAt(nil, t1)
	sigs, cancels := r.drain()
	if len(cancels) != 1 || cancels[0].SignalID != sid || len(sigs) != 0 {
		t.Fatalf("an authored re-spec must cancel the legacy working order %s exactly once and place nothing: sigs=%+v cancels=%+v", sid, sigs, cancels)
	}
	row := r.row("S1")
	if row.State != store.StateCancelPending || !strings.Contains(row.StateReason, "bracket re-spec by v2: authored SL 98.00→96.00 TP 110.00→110.00") {
		t.Fatalf("the legacy row must be cancel_pending naming the AUTHORED change: %+v", row)
	}
	if n, _ := store.SystemCounter(r.st, "arm:respec_cancel"); n != before+1 {
		t.Fatalf("the re-spec cancel must be counted once (%d → %d)", before, n)
	}
	// Verifier item 3: the leg whose order this pass pulled is not recorded
	// "admitted" under v2 (the structural record stops at pending_gates).
	geo, err := r.st.StructuralGeometryFor(r.at.id, r.pid, 2)
	if err != nil || len(geo) != 1 || geo[0].Scenario != "S1" || geo[0].Reason == "admitted" {
		t.Fatalf("a re-spec-cancelled leg must not be recorded admitted: %+v %v", geo, err)
	}
	// NOTE [A]: on this legacy reject the structural composition takes the stop
	// from the PDH zone (98.00) whatever the authored stop says, so the v2
	// order re-places at the same composed bracket — E1 judges the AUTHORED
	// change by contract, not what composition later makes of it.
	sigs, cancels = r.settleAndReArm()
	if len(cancels) != 0 || len(sigs) != 1 || sigs[0].SignalID == sid || sigs[0].LimitPrice != 100 {
		t.Fatalf("the settled legacy scenario must re-arm and place ONE new signal at 100.00 under v2: sigs=%+v cancels=%+v", sigs, cancels)
	}
	nw := r.row("S1")
	if len(r.rows()) != 2 || nw.PlacementSeq != 1 || nw.Version != 2 || nw.Policy != "" || nw.SignalID != sigs[0].SignalID {
		t.Fatalf("the successor must be ONE legacy row, placement_seq 1 under v2, carrying the new signal: %+v", r.rows())
	}
}

// Absent ≠ changed: a working row whose placing version cannot supply the
// authored leg (here v1 never carried scenario S2) is NOT re-spec'd — nothing
// is sent, no counter moves, the row stays working — and ONE WARN names why,
// however many passes ask.
func TestArmRespecAbsentPriorAuthoredLegNeverCancels(t *testing.T) {
	r := newZoneRig(t, "w1b-respec-absent", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.newVersion(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false), zoneScenario("S2", kernel.EntryPolicyMarketInZone, zone, false))
	ledger := r.st.ArmedOrders()
	seed := &store.ArmedOrderDB{TraderID: r.at.id, PlanID: r.pid, Version: 1, Session: "TEST", Scenario: "S2", Side: "long",
		EntryPx: 100.5, StopPx: 90, TargetPx: 120, State: store.StateArmed, EntryClass: "armed_fill", Kind: "limit",
		Policy: store.ArmPolicyMarketInZone, CreatedAt: r.now, UpdatedAt: r.now}
	if err := ledger.UpsertArm(seed); err != nil {
		t.Fatal(err)
	}
	if err := ledger.BeginPlacement(seed.ID, "sig-respec-s2"); err != nil {
		t.Fatal(err)
	}
	if err := ledger.ApplyPlacementReceipt(r.at.id, "sig-respec-s2", store.StateWorking, "fixture"); err != nil {
		t.Fatal(err)
	}
	before, _ := store.SystemCounter(r.st, "arm:respec_cancel")
	logs := captureTraderLog(t)
	for i := 1; i <= 2; i++ {
		ti := r.now.Add(time.Duration(i) * time.Minute)
		r.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{
			{OrderID: "o-s2", Name: "sig-respec-s2", Symbol: "MNQ", Action: "buy", Type: "limit", LimitPrice: 100.5, Quantity: 1, State: "Working"},
		}}, ti)
		r.setTape(zoneTape(101.95, ti, 0))
		r.at.maybeManageArmedOrdersAt(nil, ti)
		for _, c := range func() []ntwire.CancelOrderPayload { _, c := r.drain(); return c }() {
			if c.SignalID == "sig-respec-s2" {
				t.Fatalf("pass %d: an absent prior authored leg must never cancel the working order: %+v", i, c)
			}
		}
	}
	if row := r.row("S2"); row.State != store.StateWorking || row.Version != 1 {
		t.Fatalf("the working row must stay working under v1: %+v", row)
	}
	if n, _ := store.SystemCounter(r.st, "arm:respec_cancel"); n != before {
		t.Fatalf("nothing happened, so nothing is recorded (%d → %d)", before, n)
	}
	if n := strings.Count(logs.String(), "armed re-spec NOT judged: TEST S2 leg 1"); n != 1 {
		t.Fatalf("the absent prior must be named exactly once across two passes, got %d:\n%s", n, logs.String())
	}
	if !strings.Contains(logs.String(), "v1 carried no scenario S2") {
		t.Fatalf("the WARN must say why the prior is absent:\n%s", logs.String())
	}
}

// ── W1b FOLD-1 — E1 judges a re-priced ENTRY (CTO ruling, P1) ──────────────
//
// E1 compared only the authored stop and target, so a v2 that moved a
// planned_order limit's ENTRY (SL/TP unchanged) left the v1 limit resting at
// the old price with v1's bracket until the 30-minute rest cap. The rule: a
// non-market_in_zone WORKING row whose AUTHORED entry moved ≥ 2 ticks (the
// churnNeedsModify threshold) under a newer version is cancelled through the
// same single-cancel function as E1/E2, reason "entry re-spec by vN: E a→b"
// (authored values); market_in_zone keeps E2's zone rule.

// entryVersion publishes the next plan version whose S1 is the planned_order
// reject with only its AUTHORED entry moved (stop 98 / target 110 unchanged).
// The doc is built at the unmoved entry first so the fixture links S1 to its
// level identity (structuralTestMap links on Arm.Entry == level price), then
// the entry alone is moved.
func (r *zoneRig) entryVersion(entry float64) {
	r.t.Helper()
	r.entryVersionFor(kernel.EntryPolicyPlannedOrder, entry)
}

// entryVersionFor is entryVersion for any policy ("" = legacy): the same doc,
// zone and authored bracket, only S1's AUTHORED entry moved.
func (r *zoneRig) entryVersionFor(policy string, entry float64) {
	r.t.Helper()
	d := zoneDoc(zoneScenario("S1", policy, zone, false))
	d.Scenarios[0].Arm.Entry = entry
	blob, _ := json.Marshal(d)
	shadowPlanAtTime(r.t, r.at, r.st, string(blob), r.now)
}

// FOLD-1 RED at the production call site: v2 moves ONLY the authored entry of
// a working planned_order limit by exactly 2 ticks (100.00 → 99.50, the
// inclusive edge) → the v1 order is cancelled once, "entry re-spec by v2:
// E 100.00→99.50", counted under arm:respec_cancel; a fresh PERSISTED flat
// book (st.NT8OrderSnapshots().Insert) settles it and the scenario re-arms
// under v2 with a NEW signal at the NEW entry.
func TestPlannedOrderEntryRespecByNewVersionReplacesTheOrder(t *testing.T) {
	r := newZoneRig(t, "w1b-respec-entry", zoneDoc(zoneScenario("S1", kernel.EntryPolicyPlannedOrder, zone, false)))
	first := r.placeWorking(100)
	sid := first.SignalID
	if row := r.row("S1"); row.Policy == kernel.EntryPolicyMarketInZone || row.EntryPx != 100 {
		t.Fatalf("fixture: the row must be a non-market_in_zone limit at 100.00: %+v", row)
	}
	before, _ := store.SystemCounter(r.st, "arm:respec_cancel")

	r.entryVersion(99.5)
	t1 := r.now.Add(time.Minute)
	r.restingBook(t1, sid, 100)
	r.setTape(zoneTape(101.95, t1, 0))
	r.at.maybeManageArmedOrdersAt(nil, t1)
	sigs, cancels := r.drain()
	if len(cancels) != 1 || cancels[0].SignalID != sid || len(sigs) != 0 {
		t.Fatalf("a re-priced entry must cancel the working order %s exactly once and place nothing: sigs=%+v cancels=%+v", sid, sigs, cancels)
	}
	row := r.row("S1")
	if row.State != store.StateCancelPending || !strings.Contains(row.StateReason, "entry re-spec by v2: E 100.00→99.50") {
		t.Fatalf("the working row must be cancel_pending naming the AUTHORED entry change 'entry re-spec by v2: E 100.00→99.50': %+v", row)
	}
	if n, _ := store.SystemCounter(r.st, "arm:respec_cancel"); n != before+1 {
		t.Fatalf("the entry re-spec cancel must be counted once under arm:respec_cancel (%d → %d)", before, n)
	}

	sigs, cancels = r.settleAndReArm()
	if len(cancels) != 0 || len(sigs) != 1 || sigs[0].SignalID == sid || sigs[0].LimitPrice != 99.5 {
		t.Fatalf("the settled scenario must re-arm and place ONE new signal at the v2 entry 99.50: sigs=%+v cancels=%+v", sigs, cancels)
	}
	nw := r.row("S1")
	if len(r.rows()) != 2 || nw.PlacementSeq != 1 || nw.Version != 2 || nw.EntryPx != 99.5 || nw.SignalID != sigs[0].SignalID {
		t.Fatalf("the successor must be ONE row, placement_seq 1 under v2 at 99.50, carrying the new signal: %+v", r.rows())
	}
}

// FOLD-1 edge at the production call site: the authored entry moved ONE tick
// (100.00 → 99.75) under v2 — under the 2-tick threshold, so nothing is sent,
// the row stays working under v1 and nothing is counted.
func TestPlannedOrderEntryMovedUnderTwoTicksLeavesTheOrderAlone(t *testing.T) {
	r := newZoneRig(t, "w1b-respec-entry-1t", zoneDoc(zoneScenario("S1", kernel.EntryPolicyPlannedOrder, zone, false)))
	sid := r.placeWorking(100).SignalID
	before, _ := store.SystemCounter(r.st, "arm:respec_cancel")
	r.entryVersion(99.75)
	t1 := r.now.Add(time.Minute)
	r.restingBook(t1, sid, 100)
	r.setTape(zoneTape(101.95, t1, 0))
	r.at.maybeManageArmedOrdersAt(nil, t1)
	if sigs, cancels := r.drain(); len(sigs) != 0 || len(cancels) != 0 {
		t.Fatalf("an entry moved < 2 ticks must send nothing: sigs=%+v cancels=%+v", sigs, cancels)
	}
	if row := r.row("S1"); row.State != store.StateWorking || row.Version != 1 || row.SignalID != sid {
		t.Fatalf("the working row must stay working under v1: %+v", row)
	}
	if n, _ := store.SystemCounter(r.st, "arm:respec_cancel"); n != before {
		t.Fatalf("nothing happened, so nothing is recorded (%d → %d)", before, n)
	}
}

// FOLD-1 "market_in_zone keeps E2's zone rule", at the production call site
// (L8 — the pure edge below cannot see the arm loop): a working market_in_zone
// limit resting at the far edge 100.50, and v2 moves ONLY S1's authored entry
// 100.00 → 99.50 (2 ticks) with the SAME zone 99.50–100.50 and the same
// authored bracket. The zone still holds the limit (E2 silent) and the entry
// rule never judges a market_in_zone row, so nothing is sent, the row stays
// working under v1 and no counter moves.
func TestMarketInZoneEntryMovedInsideTheSameZoneLeavesTheOrderAlone(t *testing.T) {
	r := newZoneRig(t, "w1b-respec-entry-miz", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	sid := r.placeWorking(100.5).SignalID
	if row := r.row("S1"); row.Policy != kernel.EntryPolicyMarketInZone || row.EntryPx != 100.5 {
		t.Fatalf("fixture: the row must be a market_in_zone limit at the far edge 100.50: %+v", row)
	}
	respec, _ := store.SystemCounter(r.st, "arm:respec_cancel")
	moved, _ := store.SystemCounter(r.st, "market_in_zone:zone_moved")
	r.entryVersionFor(kernel.EntryPolicyMarketInZone, 99.5)
	t1 := r.now.Add(time.Minute)
	r.restingBook(t1, sid, 100.5)
	r.setTape(zoneTape(101.95, t1, 0))
	r.at.maybeManageArmedOrdersAt(nil, t1)
	if sigs, cancels := r.drain(); len(sigs) != 0 || len(cancels) != 0 {
		t.Fatalf("a market_in_zone entry moved inside the same zone is E2's to judge, and E2 is silent: sigs=%+v cancels=%+v", sigs, cancels)
	}
	if row := r.row("S1"); row.State != store.StateWorking || row.Version != 1 || row.SignalID != sid {
		t.Fatalf("the working market_in_zone row must stay working under v1: %+v", row)
	}
	if n, _ := store.SystemCounter(r.st, "arm:respec_cancel"); n != respec {
		t.Fatalf("arm:respec_cancel must not move for a market_in_zone entry (%d → %d)", respec, n)
	}
	if n, _ := store.SystemCounter(r.st, "market_in_zone:zone_moved"); n != moved {
		t.Fatalf("the zone did not move, so market_in_zone:zone_moved must not move (%d → %d)", moved, n)
	}
}

// FOLD-1 on a LEGACY (policy "") row at the production call site: the legacy
// limit rests AT its authored entry too, so v2 moving only that entry
// 100.00 → 99.50 cancels it once as "entry re-spec by v2: E 100.00→99.50",
// counted under arm:respec_cancel.
func TestLegacyWorkingArmEntryRespecByNewVersionCancelsTheOrder(t *testing.T) {
	r := newZoneRig(t, "w1b-respec-entry-legacy", zoneDoc(zoneScenario("S1", "", zone, false)))
	sid := r.placeWorking(100).SignalID
	if row := r.row("S1"); row.Policy != "" || row.EntryPx != 100 {
		t.Fatalf("fixture: the row must be a legacy limit at 100.00: %+v", row)
	}
	before, _ := store.SystemCounter(r.st, "arm:respec_cancel")
	r.entryVersionFor("", 99.5)
	t1 := r.now.Add(time.Minute)
	r.restingBook(t1, sid, 100)
	r.setTape(zoneTape(101.95, t1, 0))
	r.at.maybeManageArmedOrdersAt(nil, t1)
	sigs, cancels := r.drain()
	if len(cancels) != 1 || cancels[0].SignalID != sid || len(sigs) != 0 {
		t.Fatalf("a re-priced legacy entry must cancel the working order %s exactly once and place nothing: sigs=%+v cancels=%+v", sid, sigs, cancels)
	}
	if row := r.row("S1"); row.State != store.StateCancelPending || !strings.Contains(row.StateReason, "entry re-spec by v2: E 100.00→99.50") {
		t.Fatalf("the legacy row must be cancel_pending naming the AUTHORED entry change: %+v", row)
	}
	if n, _ := store.SystemCounter(r.st, "arm:respec_cancel"); n != before+1 {
		t.Fatalf("the legacy entry re-spec cancel must be counted once (%d → %d)", before, n)
	}
}

// FOLD-1 predicate edges (pure): the entry rule is for non-market_in_zone rows
// only (a market_in_zone row's resting price is the zone's far edge, judged by
// E2); the threshold is churnNeedsModify's (≥ 2 ticks); an entry move and a
// bracket move under one version bump are ONE decision (the entry's); an
// absent (zero) authored entry on either side is never a change (L7).
func TestArmRespecForEntryEdges(t *testing.T) {
	wasLeg := kernel.PlanArmLeg{Entry: 100, Stop: 98, Target: 110}
	was := func(l kernel.PlanArmLeg) func() (kernel.PlanArmLeg, string) {
		return func() (kernel.PlanArmLeg, string) { return l, "" }
	}
	base := store.ArmedOrderDB{State: store.StateWorking, SignalID: "sig", Version: 1, EntryPx: 100, StopPx: 98, TargetPx: 110}
	moved := kernel.PlanArmLeg{Entry: 99.5, Stop: 98, Target: 110}
	c, _, ok := armRespecFor(2, base, moved, moved, zoneLeg{}, 0.25, was(wasLeg))
	if !ok || c.counter != "arm:respec_cancel" || c.class != "entry re-spec" ||
		!strings.HasPrefix(c.reason, "entry re-spec by v2: E 100.00→99.50") {
		t.Fatalf("a 2-tick authored entry move on a planned_order/legacy row is E1: %+v %v", c, ok)
	}
	if c, _, ok := armRespecFor(2, base, kernel.PlanArmLeg{Entry: 99.75, Stop: 98, Target: 110}, moved, zoneLeg{}, 0.25, was(wasLeg)); ok {
		t.Fatalf("a 1-tick entry move is not a re-spec: %+v", c)
	}
	if c, _, ok := armRespecFor(1, base, moved, moved, zoneLeg{}, 0.25, was(wasLeg)); ok {
		t.Fatalf("the same version never re-specs an entry: %+v", c)
	}
	both := kernel.PlanArmLeg{Entry: 99.5, Stop: 96, Target: 112}
	if c, _, ok := armRespecFor(2, base, both, both, zoneLeg{}, 0.25, was(wasLeg)); !ok || !strings.HasPrefix(c.reason, "entry re-spec by v2: E 100.00→99.50") {
		t.Fatalf("entry AND bracket moved → ONE decision, the entry's: %+v %v", c, ok)
	}
	for name, pair := range map[string][2]float64{"absent prior entry": {0, 99.5}, "absent authored entry": {100, 0}} {
		w := wasLeg
		w.Entry = pair[0]
		a := moved
		a.Entry = pair[1]
		if c, _, ok := armRespecFor(2, base, a, a, zoneLeg{}, 0.25, was(w)); ok {
			t.Fatalf("%s: absent is never changed: %+v", name, c)
		}
	}
	// market_in_zone keeps E2's zone rule: its authored entry moving inside a
	// zone that still contains the resting limit is not an E1 entry re-spec.
	miz := base
	miz.Policy, miz.EntryPx = kernel.EntryPolicyMarketInZone, 100.5
	zl := zoneLeg{on: true, v: kernel.ZoneVerdict{Lo: 99.5, Hi: 100.5, Far: 100.5, Near: 99.5}}
	if c, _, ok := armRespecFor(2, miz, moved, moved, zl, 0.25, was(wasLeg)); ok {
		t.Fatalf("a market_in_zone row is never judged by the entry rule: %+v", c)
	}
}

// ── W1b FOLD-8 — the corrected comment's claim, pinned (CTO ruling, P2) ──────
//
// The comment above the respecWorkingArm call now says "No hole: a
// cancel_pending row is never placed". The cancelling pass's "place nothing"
// assertions do NOT pin that: on that pass the book still shows the entry
// resting, so the one-contract guard refuses too (a mutation that let the
// placement switch place cancel_pending rows left every E1/E2 test green).
// Here the pass AFTER the cancel sees a fresh FLAT live book (the one-contract
// guard admits) with no persisted book confirming the cancel, so the row is
// still cancel_pending. Two layers then keep it off the wire [A, probed]: the
// placement switch in runArmedPlacementAt (`case "armed":`) never enters the
// placement path for it, and the store CAS BeginPlacement (state = armed AND
// no signal) refuses it before the send. The send assertion needs both layers
// to fail (both mutated → the v1 order, limit 100.00 SL 98 TP 110, re-sent);
// the log assertion isolates the switch — with the switch alone mutated the
// CAS refuses every pass and "📌 armed place failed … no longer eligible for
// placement" is logged for a row nothing should have tried to place.
func TestRespecCancelPendingRowIsNeverPlacedOnAFlatLiveBook(t *testing.T) {
	r := newZoneRig(t, "w1b-respec-cp-unplaced", zoneDoc(zoneScenario("S1", kernel.EntryPolicyPlannedOrder, zone, false)))
	sid := r.placeWorking(100).SignalID
	r.entryVersion(99.5)
	t1 := r.now.Add(time.Minute)
	r.restingBook(t1, sid, 100)
	r.setTape(zoneTape(101.95, t1, 0))
	r.at.maybeManageArmedOrdersAt(nil, t1)
	if sigs, cancels := r.drain(); len(cancels) != 1 || len(sigs) != 0 {
		t.Fatalf("fixture: the entry re-spec must cancel once and place nothing: sigs=%+v cancels=%+v", sigs, cancels)
	}
	if row := r.row("S1"); row.State != store.StateCancelPending || row.SignalID != sid {
		t.Fatalf("fixture: the row must be cancel_pending on its own signal: %+v", row)
	}

	// The adapter's B3 duplicate guard runs on the WALL clock; re-made so it
	// cannot be what keeps the row off the wire (as settleAndReArm does).
	r.at.trader = ntTrader.NewTCPTrader(r.srv, "MNQ", "Sim101")
	logs := captureTraderLog(t)
	t2 := t1.Add(30 * time.Second)
	r.flatBook(t2)
	r.setTape(zoneTape(101.95, t2, 0))
	r.at.maybeManageArmedOrdersAt(nil, t2)
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("a cancel_pending row must never be placed, even on a flat live book: sigs=%+v", sigs)
	}
	if strings.Contains(logs.String(), "armed place failed") || strings.Contains(logs.String(), "placement requested") {
		t.Fatalf("a cancel_pending row must never enter the placement path at all:\n%s", logs.String())
	}
	if rows := r.rows(); len(rows) != 1 || rows[0].SignalID != sid {
		t.Fatalf("no successor may be minted before a persisted book confirms the cancel: %+v", rows)
	}
}
