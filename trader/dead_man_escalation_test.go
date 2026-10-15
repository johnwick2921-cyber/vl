package trader

import (
	"net"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	nttrader "vl/trader/ninjatrader"

	"vl/store"
)

// W26 deadman-escalation (bug fix, ships ON). The dead-man reconcile block had
// no upper bound and no owner-facing signal: if the AddOn stopped sending order
// snapshots, a trader stayed entry-blocked forever with only a once-per-state
// WARN. Now one P0 `dead_man_reconcile_stuck` fires PER blocked episode (after
// deadManStuckProbeMin consecutive failed probes OR deadManStuckAgeMs blocked),
// entries stay BLOCKED (the alert is an operator signal, never an auto-unblock),
// and /api/health surfaces the live block.

// The two anchored mutants these tests kill:
//   - DROP THE ALERT (remove the emitAlert call) → TestDeadManEscalateFiresOncePerEpisode
//     asserts count==1 after the threshold and count==2 after the second episode.
//   - CONSTANT EPISODE ID (episode := a fixed string) → the second episode's P0
//     dedupes against the first (same trader_id+event_id) and the count==2
//     assertion fails.

func deadManEscalationTrader(t *testing.T) (*AutoTrader, *store.Store) {
	t.Helper()
	// day_plan enabled so emitAlert writes to the store (the alert center is
	// gated on it), matching production's futures wiring.
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	return at, st
}

func stuckAlertCount(t *testing.T, st *store.Store, id string) int {
	t.Helper()
	alerts, _ := st.Alert().List(id, 200)
	n := 0
	for _, a := range alerts {
		if a.Kind == "dead_man_reconcile_stuck" {
			n++
		}
	}
	return n
}

// TestDeadManEscalateFiresOncePerEpisode — the probe-count path at the
// escalation call site: a reconnect whose order snapshots never arrive fails
// deadManStuckProbeMin probes, fires exactly ONE P0, stays silent until a clean
// reconcile ends the episode, then a second episode alerts AGAIN (a distinct
// episode id, so it is not deduped).
func TestDeadManEscalateFiresOncePerEpisode(t *testing.T) {
	at, st := deadManEscalationTrader(t)

	// Episode 1: drop → reconnect → reconcile probe fails forever.
	if ev := at.deadMan.step(false, 1_000, nil); ev != wdWentDown {
		t.Fatalf("precondition: drop must emit wentDown, got %d", ev)
	}
	if ev := at.deadMan.step(true, 2_000, func() bool { return false }); ev != wdReconnected {
		t.Fatalf("precondition: reconnect must emit reconnected, got %d", ev)
	}
	for i := int64(0); i < deadManStuckProbeMin; i++ {
		at.deadMan.step(true, 3_000+i*1_000, func() bool { return false })
	}
	if n := stuckAlertCount(t, st, at.id); n != 0 {
		t.Fatalf("precondition: nothing may alert before the escalation runs, got %d", n)
	}

	// Call site: the trader escalates once the threshold is crossed.
	at.deadManEscalate(time.UnixMilli(3_000 + deadManStuckProbeMin*1_000))
	if n := stuckAlertCount(t, st, at.id); n != 1 {
		t.Fatalf("one stuck episode must fire exactly ONE P0, got %d", n)
	}

	// More failed probes + more escalations within the SAME episode: silent.
	for i := int64(0); i < 5; i++ {
		nowMs := 10_000 + i*1_000
		at.deadMan.step(true, nowMs, func() bool { return false })
		at.deadManEscalate(time.UnixMilli(nowMs))
	}
	if n := stuckAlertCount(t, st, at.id); n != 1 {
		t.Fatalf("the alert must not repeat within one episode, got %d", n)
	}

	// A clean reconcile ends the episode (and clears the latch).
	if ev := at.deadMan.step(true, 99_000, func() bool { return true }); ev != wdResumed {
		t.Fatalf("clean reconcile must resume, got %d", ev)
	}

	// Episode 2: a NEW block alerts again — a distinct episode id, so the store
	// must hold a SECOND P0.
	if ev := at.deadMan.step(false, 200_000, nil); ev != wdWentDown {
		t.Fatalf("second drop must emit wentDown, got %d", ev)
	}
	at.deadMan.step(true, 201_000, func() bool { return false })
	for i := int64(0); i < deadManStuckProbeMin; i++ {
		at.deadMan.step(true, 202_000+i*1_000, func() bool { return false })
	}
	at.deadManEscalate(time.UnixMilli(202_000 + deadManStuckProbeMin*1_000))
	if n := stuckAlertCount(t, st, at.id); n != 2 {
		t.Fatalf("a second episode must fire a second P0 (distinct episode id), got %d", n)
	}
}

// TestDeadManEscalateFiresByAgeWhileLinkDown — the age path: a link that stays
// DOWN produces zero failed probes (no reconcile is attempted while down), but
// a block that outlives deadManStuckAgeMs must still alert. A block younger
// than the threshold must not.
func TestDeadManEscalateFiresByAgeWhileLinkDown(t *testing.T) {
	at, st := deadManEscalationTrader(t)
	at.deadMan.step(false, 1_000, nil)
	// Exactly at the age threshold: fires (probes 0 < min, but age >= min age).
	at.deadManEscalate(time.UnixMilli(1_000 + deadManStuckAgeMs))
	if n := stuckAlertCount(t, st, at.id); n != 1 {
		t.Fatalf("a block at the age threshold must fire the P0 even with 0 probes, got %d", n)
	}

	// One millisecond under: no alert.
	at2, st2 := deadManEscalationTrader(t)
	at2.deadMan.step(false, 1_000, nil)
	at2.deadManEscalate(time.UnixMilli(1_000 + deadManStuckAgeMs - 1))
	if n := stuckAlertCount(t, st2, at2.id); n != 0 {
		t.Fatalf("a block younger than the age threshold must NOT alert, got %d", n)
	}
}

// TestDeadManEscalateFlapWithinEpisodeDoesNotRestart — CTO gate F1: a
// down↔awaiting flap INSIDE one blocked episode must NOT restart the episode.
// drop → reconnect → 2 failed probes → drop (flap) → reconnect → 1 failed probe
// is 3 probes across ONE episode: it must fire, and the episode id must be the
// FIRST blockedSinceMs. The mutant (restart the episode on every flap) resets
// the probe/age counters and the alert never fires — this test goes RED.
func TestDeadManEscalateFlapWithinEpisodeDoesNotRestart(t *testing.T) {
	at, st := deadManEscalationTrader(t)

	// Episode begins at 1000 on the first drop.
	at.deadMan.step(false, 1_000, nil)
	at.deadMan.step(true, 2_000, func() bool { return false }) // reconnect (no probe)
	at.deadMan.step(true, 3_000, func() bool { return false }) // probe 1 fail
	at.deadMan.step(true, 4_000, func() bool { return false }) // probe 2 fail
	// Flap: drop again mid-episode, then reconnect — SAME continuous block.
	at.deadMan.step(false, 5_000, nil)
	at.deadMan.step(true, 6_000, func() bool { return false }) // reconnect (no probe)
	at.deadMan.step(true, 7_000, func() bool { return false }) // probe 3 fail (total)

	// The health snapshot must still report the ORIGINAL episode, not the flap.
	if blocked, since, probes := at.HealthDeadMan(); !blocked || since != 1_000 || probes != 3 {
		t.Fatalf("a flap must not restart the episode: got blocked=%v since=%d probes=%d, want blocked since=1000 probes=3", blocked, since, probes)
	}

	at.deadManEscalate(time.UnixMilli(7_000))
	if n := stuckAlertCount(t, st, at.id); n != 1 {
		t.Fatalf("3 probes across one episode (with a flap) must fire exactly one P0, got %d", n)
	}
	// The episode id must be the FIRST blockedSinceMs (1000), not the flap's.
	alerts, _ := st.Alert().List(at.id, 10)
	found := 0
	for _, a := range alerts {
		if a.Kind != "dead_man_reconcile_stuck" {
			continue
		}
		found++
		if a.EventID != "deadman-stuck-trader-1-1000" {
			t.Fatalf("episode id must be the first blockedSinceMs, got %q", a.EventID)
		}
	}
	if found != 1 {
		t.Fatalf("expected exactly one stuck alert, found %d", found)
	}
}

// TestDeadManHealthSnapshot — /api/health `dead_man` (READ values, never
// literal): blocked, the episode start, and the failed-probe count.
func TestDeadManHealthSnapshot(t *testing.T) {
	at, _ := deadManEscalationTrader(t)
	if blocked, since, probes := at.HealthDeadMan(); blocked || since != 0 || probes != 0 {
		t.Fatalf("a live watchdog must report clear health, got blocked=%v since=%d probes=%d", blocked, since, probes)
	}
	at.deadMan.step(false, 42_000, nil)
	at.deadMan.step(true, 43_000, func() bool { return false }) // reconnect, no probe
	at.deadMan.step(true, 44_000, func() bool { return false }) // failed probe 1
	if blocked, since, probes := at.HealthDeadMan(); !blocked || since != 42_000 || probes != 1 {
		t.Fatalf("blocked health must report the episode, got blocked=%v since=%d probes=%d", blocked, since, probes)
	}
}

// reconnectNoOrderSnapshot closes the AddOn link and dials a new one, seeding a
// positions snapshot on the new link but NO order snapshot — the exact
// production failure W26 targets (the AddOn stopped sending order_snapshot).
func reconnectNoOrderSnapshot(t *testing.T, s *ntwire.TCPServer, broker *nttrader.TCPTrader, old net.Conn) net.Conn {
	t.Helper()
	rec0, _, _, _ := broker.MaintenanceView()
	_ = old.Close()
	for deadline := time.Now().Add(5 * time.Second); s.IsConnected(); {
		if time.Now().After(deadline) {
			t.Fatal("fixture: the server never saw the old link close")
		}
		time.Sleep(time.Millisecond)
	}
	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	waitAddonRegistered(t, s)
	for deadline := time.Now().Add(5 * time.Second); ; {
		if rec, _, _, _ := broker.MaintenanceView(); rec.AcceptSeq > rec0.AcceptSeq {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture: the new link never got a new accept sequence")
		}
		time.Sleep(time.Millisecond)
	}
	// Positions on the new link, but NO order snapshot — reconcile stays dirty.
	s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
	return conn
}

// TestDriveDeadManWatchdogEscalatesWhenOrderSnapshotsStop — the FULL call site:
// a real TCP link whose order snapshots stop arriving after a reconnect. The
// watchdog is driven exactly as runCycle drives it; after deadManStuckProbeMin
// failed reconcile probes the P0 fires once, a clean reconcile (both snapshots
// land) ends the episode, and a second episode alerts again.
func TestDriveDeadManWatchdogEscalatesWhenOrderSnapshotsStop(t *testing.T) {
	at, s, broker, conn := deadManLoopback(t)
	// emitAlert is gated on day_plan; the loopback fixture builds a plan-off
	// trader, so arm it for the live call-site run.
	at.config.StrategyConfig.DayPlan = &store.DayPlanConfig{PlanEnabled: true}
	at.driveDeadManWatchdog()
	if at.deadMan.entriesBlocked() {
		t.Fatal("precondition: a live, never-dropped link must not block entries")
	}

	// Episode 1: replace the link but send only a positions snapshot — the
	// order snapshot never arrives, so reconcile can never succeed.
	conn = reconnectNoOrderSnapshot(t, s, broker, conn)
	if ev := at.driveDeadManWatchdog(); ev != wdReconnected {
		t.Fatalf("the replaced link must read as a reconnect, got %d", ev)
	}
	for i := 0; i < deadManStuckProbeMin; i++ {
		at.driveDeadManWatchdog()
	}
	if n := stuckAlertCount(t, at.store, at.id); n != 1 {
		t.Fatalf("order snapshots stopped: the P0 must fire exactly once after %d failed probes, got %d", deadManStuckProbeMin, n)
	}
	// More cycles without a snapshot: still one alert.
	for i := 0; i < 3; i++ {
		at.driveDeadManWatchdog()
	}
	if n := stuckAlertCount(t, at.store, at.id); n != 1 {
		t.Fatalf("the alert must not repeat within one episode, got %d", n)
	}

	// Clean reconcile: the order snapshot finally lands.
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, time.Now())
	if ev := at.driveDeadManWatchdog(); ev != wdResumed || at.deadMan.entriesBlocked() {
		t.Fatalf("both snapshots must resume entries, got event %d blocked=%v", ev, at.deadMan.entriesBlocked())
	}

	// Episode 2: another order-snapshot-starved reconnect alerts again.
	conn = reconnectNoOrderSnapshot(t, s, broker, conn)
	if ev := at.driveDeadManWatchdog(); ev != wdReconnected {
		t.Fatalf("second replaced link must read as a reconnect, got %d", ev)
	}
	for i := 0; i < deadManStuckProbeMin; i++ {
		at.driveDeadManWatchdog()
	}
	if n := stuckAlertCount(t, at.store, at.id); n != 2 {
		t.Fatalf("a second episode must fire a second P0, got %d", n)
	}
}
