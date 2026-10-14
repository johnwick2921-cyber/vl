package trader

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── W-EXEC-TRUTH W3 D14 — the live-bar armed pass, through the REAL sink ────
//
// Frames enter at pictureHtfLiveBars (the process-wide live-bar sink the TCP
// server's worker calls), the trader's event loop runs on its own goroutine
// (started as Run starts it), and the pass it runs is the FULL
// maybeManageArmedOrdersAt pass. The loop's clock is the fixture clock through
// the nil-in-production seam armedEventNowForTest.

// eventRig makes the zone rig a RUNNING trader with a chosen account, its
// event loop started and its sink registration in place.
func eventRig(t *testing.T, id string, doc kernel.PlanDoc) *zoneRig {
	t.Helper()
	r := newZoneRig(t, id, doc)
	if err := r.st.Trader().Create(&store.Trader{ID: id, Name: id, Account: "Sim101"}); err != nil {
		t.Fatal(err)
	}
	r.at.isRunningMutex.Lock()
	r.at.isRunning = true
	r.at.isRunningMutex.Unlock()
	r.at.armedEventNowForTest = func() time.Time { return r.now }
	r.at.registerPictureHtf()
	r.at.startArmedEventLoop()
	t.Cleanup(func() {
		r.at.stopArmedEventLoop()
		pictureHtfTraders.Delete(id)
	})
	return r
}

// finalFrame pushes one FINAL 1m bar for the trader's instrument through the
// production sink.
func (r *zoneRig) finalFrame(close float64) {
	b := ntwire.Bar{T: r.now.Add(-time.Minute).Truncate(time.Minute).UnixMilli(), O: close, H: close + 0.25, L: close - 0.25, C: close, Final: true}
	pictureHtfLiveBars("MNQ 12-26", "1m", "MNQ 12-26", []ntwire.Bar{b}, time.Now())
}

func (r *zoneRig) passes() int64 {
	if l := r.at.armedEvent.Load(); l != nil {
		return l.passes.Load()
	}
	return -1
}

func (r *zoneRig) waitPasses(n int64, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if r.passes() >= n {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return r.passes() >= n
}

// collect drains until at least one signal arrives or the deadline passes,
// then one more barrier so a second signal would be seen too.
func (r *zoneRig) collectSignals(within time.Duration) []ntwire.SignalPayload {
	var all []ntwire.SignalPayload
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		s, _ := r.drain()
		all = append(all, s...)
		if len(all) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s, _ := r.drain()
	return append(all, s...)
}

// armZoneRow runs one scan pass with the price short of the zone: the row is
// authored and armed, nothing is placed, and the pass caches the zone flag.
func (r *zoneRig) armZoneRow() {
	r.t.Helper()
	r.setTape(zoneTapeReacted(99.0, r.now))
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	if s, _ := r.drain(); len(s) != 0 {
		r.t.Fatalf("fixture: short of the zone must not place: %+v", s)
	}
	if !r.at.zoneArmActive.Load() {
		r.t.Fatal("fixture: the pass must cache the zone-arm flag")
	}
}

// A final 1m frame → the event pass places within a bounded wait. The
// frame's own close reads the SAME verdict the pass cached (short), so only
// the Final trigger can have woken the pass; the pass reads the tape.
func TestArmedEventPassPlacesOnAFinalFrame(t *testing.T) {
	r := eventRig(t, "w3-event-final", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.armZoneRow()
	r.setTape(zoneTape(100.0, r.now, 0))
	r.finalFrame(99.0)
	sigs := r.collectSignals(3 * time.Second)
	if len(sigs) != 1 || sigs[0].LimitPrice != 100.5 {
		t.Fatalf("a final frame must drive exactly one placement at 100.50: %+v", sigs)
	}
}

// A FORMING (non-final) frame whose price changes the cached zone verdict
// wakes the pass too — a bar entering the zone need not wait for its close.
// A forming frame with no verdict change wakes nothing.
func TestArmedEventPassWakesOnAZoneVerdictChange(t *testing.T) {
	r := eventRig(t, "w3-event-verdict", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.armZoneRow()
	forming := func(c float64) {
		b := ntwire.Bar{T: r.now.Truncate(time.Minute).UnixMilli(), O: c, H: c, L: c, C: c}
		pictureHtfLiveBars("MNQ 12-26", "1m", "MNQ 12-26", []ntwire.Bar{b}, time.Now())
	}
	forming(99.25) // still short of the zone: no change, no pass
	time.Sleep(200 * time.Millisecond)
	if p := r.passes(); p != 0 {
		t.Fatalf("a forming frame with an unchanged verdict must not wake the pass (passes=%d)", p)
	}
	r.setTape(zoneTape(100.0, r.now, 0))
	forming(100.0) // enters the zone
	sigs := r.collectSignals(3 * time.Second)
	if len(sigs) != 1 || sigs[0].LimitPrice != 100.5 {
		t.Fatalf("a verdict change must drive exactly one placement at 100.50: %+v", sigs)
	}
}

// A 20-frame burst → at most one pass per second, and the trailing edge is
// served (the burst is not lost to the limiter).
func TestArmedEventPassBurstIsRateLimited(t *testing.T) {
	r := eventRig(t, "w3-event-burst", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.armZoneRow() // price stays short of the zone: passes are observable, nothing places
	start := time.Now()
	for i := 0; i < 20; i++ {
		r.finalFrame(99.0)
		time.Sleep(10 * time.Millisecond)
	}
	if !r.waitPasses(1, 2*time.Second) {
		t.Fatal("the burst must run at least one pass")
	}
	if time.Since(start) < 900*time.Millisecond {
		if p := r.passes(); p > 1 {
			t.Fatalf("%d passes inside the first second — the limit is 1/s", p)
		}
	}
	if !r.waitPasses(2, 4*time.Second) {
		t.Fatalf("the trailing edge of the burst must run a pass (passes=%d)", r.passes())
	}
	time.Sleep(1200 * time.Millisecond)
	if p := r.passes(); p != 2 {
		t.Fatalf("a 20-frame burst must run exactly 2 passes (leading + trailing), got %d", p)
	}
}

// No pass for a stopped trader, a plan without a zone arm, or after Stop.
func TestArmedEventPassNeverRunsStoppedOrWithoutAZoneArm(t *testing.T) {
	t.Run("stopped trader", func(t *testing.T) {
		r := eventRig(t, "w3-event-stopped", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
		r.armZoneRow()
		r.at.isRunningMutex.Lock()
		r.at.isRunning = false
		r.at.isRunningMutex.Unlock()
		r.setTape(zoneTape(100.0, r.now, 0))
		r.finalFrame(100.0)
		time.Sleep(300 * time.Millisecond)
		if s, _ := r.drain(); len(s) != 0 || r.passes() != 0 {
			t.Fatalf("a stopped trader must run no pass (passes=%d sigs=%d)", r.passes(), len(s))
		}
	})
	t.Run("no zone arm", func(t *testing.T) {
		dir := withMaintenanceDir(t)
		r := eventRig(t, "w3-event-legacy", zoneDoc(zoneScenario("S1", "", zone, false)))
		setHold(t, dir, "job-legacy") // the scan pass authors, places nothing
		r.at.maybeManageArmedOrdersAt(nil, r.now)
		if err := store.ClearMaintenanceHold(dir, "job-legacy"); err != nil {
			t.Fatal(err)
		}
		if r.at.zoneArmActive.Load() {
			t.Fatal("a legacy plan must cache the flag OFF")
		}
		r.finalFrame(101.0)
		time.Sleep(300 * time.Millisecond)
		if r.passes() != 0 {
			t.Fatalf("a plan with no zone arm must never wake the event pass (passes=%d)", r.passes())
		}
	})
	t.Run("after Stop", func(t *testing.T) {
		r := eventRig(t, "w3-event-afterstop", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
		r.armZoneRow()
		r.at.stopArmedEventLoop()
		r.setTape(zoneTape(100.0, r.now, 0))
		r.finalFrame(100.0)
		time.Sleep(300 * time.Millisecond)
		if s, _ := r.drain(); len(s) != 0 {
			t.Fatalf("no pass may run after Stop: %+v", s)
		}
	})
}

// CTO D14 (1): the event pass is the FULL pass — an arm this pass's authoring
// gates exclude has no place in the admitted set and is NOT placed, though
// its armed row exists and the price is inside the zone.
func TestArmedEventPassHonoursTheAdmittedSet(t *testing.T) {
	dir := withMaintenanceDir(t)
	r := eventRig(t, "w3-event-g1", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTape(100.0, r.now, 0))
	setHold(t, dir, "job-g1")
	r.at.maybeManageArmedOrdersAt(nil, r.now) // authors the armed row under the hold
	if err := store.ClearMaintenanceHold(dir, "job-g1"); err != nil {
		t.Fatal(err)
	}
	if row := r.row("S1"); row.State != store.StateArmed || !r.at.zoneArmActive.Load() {
		t.Fatalf("fixture: an armed row and the flag: %+v", row)
	}
	// The authoring gates now exclude S1 (quality B < floor A).
	r.at.config.StrategyConfig.DayPlan.MinScenarioQuality = "A"
	r.finalFrame(100.0)
	if !r.waitPasses(1, 3*time.Second) {
		t.Fatal("the frame must run an event pass")
	}
	if s, _ := r.drain(); len(s) != 0 {
		t.Fatalf("an arm excluded by this pass's authoring gates must not be placed by the event pass: %+v", s)
	}
	if row := r.row("S1"); row.State != store.StateArmed || row.SignalID != "" {
		t.Fatalf("the excluded row stays armed: %+v", row)
	}
}

// CTO D14 (2): a nudge on a scenario the authoring gates exclude places
// nothing; the verdict names the gate.
func TestStrictNudgeOnAGateExcludedScenarioPlacesNothing(t *testing.T) {
	dir := withMaintenanceDir(t)
	r := strictZoneRig(t, "w3-nudge-excluded", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTape(100.0, r.now, 0))
	setHold(t, dir, "job-excl")
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	if err := store.ClearMaintenanceHold(dir, "job-excl"); err != nil {
		t.Fatal(err)
	}
	r.at.config.StrategyConfig.DayPlan.MinScenarioQuality = "A"
	rec, _ := nudge(r, "S1", r.now.Add(time.Second))
	if !strings.Contains(rec.Error, "🚦 refused: ") || !strings.Contains(rec.Error, "min_scenario_quality") {
		t.Fatalf("the nudge must report the authoring refusal, got %q", rec.Error)
	}
	if s, _ := r.drain(); len(s) != 0 {
		t.Fatalf("a gate-excluded scenario must not be placed by a nudge: %+v", s)
	}
}

// D14 SERIALIZATION: the event pass and the scan pass started together place
// exactly ONE order, and (run under -race) share no unguarded state. Removing
// armedPassMu makes this test RED under the race detector.
func TestArmedEventAndScanPassesAreSerialized(t *testing.T) {
	r := eventRig(t, "w3-event-race", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.armZoneRow()
	r.setTape(zoneTape(100.0, r.now, 0))
	// Hold each pass inside its critical section long enough for the other
	// to arrive: with the lock the second WAITS; without it both are inside.
	var inside, overlap atomic.Int32
	armedPassEnterForTest = func(id string) func() {
		if id != r.at.id {
			return func() {}
		}
		if inside.Add(1) > 1 {
			overlap.Store(1)
		}
		time.Sleep(150 * time.Millisecond)
		return func() { inside.Add(-1) }
	}
	t.Cleanup(func() { armedPassEnterForTest = nil })
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); r.finalFrame(100.0) }()
	go func() { defer wg.Done(); r.at.maybeManageArmedOrdersAt(nil, r.now) }()
	wg.Wait()
	if !r.waitPasses(1, 3*time.Second) {
		t.Fatal("the event pass must run")
	}
	// Let any in-flight pass finish before the barrier.
	r.at.armedPassMu.Lock()
	r.at.armedPassMu.Unlock()
	sigs := r.collectSignals(2 * time.Second)
	if len(sigs) != 1 {
		t.Fatalf("two concurrent passes must place exactly ONE order, got %d: %+v", len(sigs), sigs)
	}
	if overlap.Load() != 0 {
		t.Fatal("two armed passes of one trader were inside the pass at the same time — armedPassMu must serialize the scan and the event pass")
	}
}

func TestArmedPassEnterSeamIsNilInProduction(t *testing.T) {
	if armedPassEnterForTest != nil {
		t.Fatal("armedPassEnterForTest must be nil outside the test that sets it")
	}
}

// The clock seam is a test seam only.
func TestArmedEventClockSeamIsNilInProduction(t *testing.T) {
	at := &AutoTrader{}
	if at.armedEventNowForTest != nil {
		t.Fatal("armedEventNowForTest must be nil on a constructed trader")
	}
}
