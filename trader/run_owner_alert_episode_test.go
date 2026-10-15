package trader

import (
	"errors"
	"testing"
	"time"

	"vl/store"
	"vl/telemetry"
)

// alertDownCount returns how many P0 trader_down_no_evaluator alerts exist for
// the trader, and their event ids.
func alertDownCount(t *testing.T, st *store.Store, id string) (int, []string) {
	t.Helper()
	alerts, err := st.Alert().List(id, 50)
	if err != nil {
		t.Fatalf("list alerts: %v", err)
	}
	n := 0
	var ids []string
	for _, a := range alerts {
		if a.Kind == "trader_down_no_evaluator" && a.Level == "P0" {
			n++
			ids = append(ids, a.EventID)
		}
	}
	return n, ids
}

// stuckSlot installs a run-owner slot whose done channel never closes (the old
// run is stuck), then registers a cleanup that releases it.
func stuckSlot(t *testing.T, h *runHarness, id string) *runOwnerSlot {
	t.Helper()
	stuck := h.mk(id)
	slot := &runOwnerSlot{at: stuck, seq: 1 << 40, done: make(chan struct{})}
	runOwners.Lock()
	runOwners.m[id] = slot
	runOwners.Unlock()
	t.Cleanup(func() { releaseRunOwner(id, slot) })
	return slot
}

func mkTraderRow(t *testing.T, h *runHarness, id string) {
	t.Helper()
	if err := h.st.Trader().Create(&store.Trader{ID: id, UserID: "u", Name: "hoang", AIModelID: "m", ExchangeID: "ex", StrategyID: "s", Account: "Sim101", IsRunning: true}); err != nil {
		t.Fatal(err)
	}
}

func waitGateBlock(t *testing.T, traderID, key string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, tbl := telemetry.GateBlockSnapshot(); tbl[traderID][key] == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, tbl := telemetry.GateBlockSnapshot()
	t.Fatalf("%s %s never reached %d (got %v)", traderID, key, want, tbl[traderID])
}

// TestOpsAlertKindsBypassTheDayPlanGate — W26 fold. Every OPS kind (down, link,
// dead-man) reaches the owner on a non-day-plan trader; a plan-content kind
// still needs the day plan. MUTANT: drop one ops kind from isOpsAlertKind →
// RED.
func TestOpsAlertKindsBypassTheDayPlanGate(t *testing.T) {
	h := newRunHarness(t)
	mkTraderRow(t, h, "tr")
	at := h.mk("tr")
	t.Cleanup(func() { waitRunRetryIdle(t, at) }) // stop the armed retry reading the seams before they are restored (race) // no day plan (empty StrategyConfig)

	opsKinds := []string{"trader_down_no_evaluator", "feed-down", "dead_man_reconcile_stuck"}
	for i, kind := range opsKinds {
		at.emitAlert("P0", kind, store.EpisodeEventID(kind, "tr", time.Now().UnixMilli()+int64(i)), "t", "b")
	}
	// plan-content alert: must NOT land without a day plan.
	at.emitAlert("P0", "plan-died", store.EpisodeEventID("plan-died", "tr", time.Now().UnixMilli()), "t", "b")

	alerts, err := h.st.Alert().List("tr", 20)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, a := range alerts {
		got[a.Kind]++
	}
	for _, kind := range opsKinds {
		if got[kind] != 1 {
			t.Fatalf("ops kind %q must bypass the day-plan gate, got %d rows (all: %v)", kind, got[kind], got)
		}
	}
	if got["plan-died"] != 0 {
		t.Fatalf("plan-content kind plan-died must keep the day-plan gate, got %d rows", got["plan-died"])
	}
}

// TestDownAlertFiresWithoutDayPlan — W26 N1. The run-owner P0 "trader is DOWN"
// alert must reach the owner even when the trader has NO day plan (the old
// emitAlert gate dropped it on dayPlanEnabled()==false). MUTANT: re-gate the
// down kind on dayPlanEnabled → RED.
func TestDownAlertFiresWithoutDayPlan(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	prevWait := runOwnerHandoffWait
	runOwnerHandoffWait = 200 * time.Millisecond
	t.Cleanup(func() { runOwnerHandoffWait = prevWait })
	prevRetry := runRetryMaxWait
	runRetryMaxWait = 100 * time.Millisecond
	t.Cleanup(func() { runRetryMaxWait = prevRetry })

	h := newRunHarness(t)
	mkTraderRow(t, h, "tr")
	stuckSlot(t, h, "tr")

	at := h.mk("tr")
	t.Cleanup(func() { waitRunRetryIdle(t, at) }) // stop the armed retry reading the seams before they are restored (race)
	// NO day plan: h.mk sets an empty StrategyConfig (DayPlan nil).
	err := at.Run()
	if !errors.Is(err, ErrTraderOldInstanceAlive) {
		t.Fatalf("a handoff timeout must refuse with ErrTraderOldInstanceAlive, got %v", err)
	}
	n, ids := alertDownCount(t, h.st, "tr")
	if n != 1 {
		t.Fatalf("a non-day-plan trader must still raise the trader-down P0, got %d alerts (ids %v)", n, ids)
	}
}

// TestDownTwiceTwoAlerts — W26 N2. A recurring down episode must not dedupe
// silently: two handoff timeouts produce two distinct alert rows (per-episode
// event ids). MUTANT: revert the event id to "trader-down-no-evaluator:"+id →
// RED (one row only).
func TestDownTwiceTwoAlerts(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	prevWait := runOwnerHandoffWait
	runOwnerHandoffWait = 200 * time.Millisecond
	t.Cleanup(func() { runOwnerHandoffWait = prevWait })
	prevRetry := runRetryMaxWait
	runRetryMaxWait = 100 * time.Millisecond
	t.Cleanup(func() { runRetryMaxWait = prevRetry })

	h := newRunHarness(t)
	mkTraderRow(t, h, "tr")
	stuckSlot(t, h, "tr")

	for i := 0; i < 2; i++ {
		at := h.mk("tr")
		t.Cleanup(func() { waitRunRetryIdle(t, at) }) // stop the armed retry reading the seams before they are restored (race)
		at.config.StrategyConfig = &store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
		if err := at.Run(); !errors.Is(err, ErrTraderOldInstanceAlive) {
			t.Fatalf("episode %d: a handoff timeout must refuse, got %v", i+1, err)
		}
		// Give each episode a distinct instant so its event id differs.
		time.Sleep(5 * time.Millisecond)
	}
	n, ids := alertDownCount(t, h.st, "tr")
	if n != 2 {
		t.Fatalf("two down episodes must be two alert rows, got %d (ids %v)", n, ids)
	}
	if len(ids) == 2 && ids[0] == ids[1] {
		t.Fatalf("two down episodes must have DISTINCT event ids, got %v", ids)
	}
}

// TestRunRetryGivesUpWhenOldRunNeverExits — W26 N3. The armed retry must not
// leak a goroutine forever: after runRetryMaxWait with the old run's claim
// still held, it gives up, logged and counted. MUTANT: drop the time.After
// branch → RED (no give-up, runRetryArmed stays true).
func TestRunRetryGivesUpWhenOldRunNeverExits(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	prevWait := runOwnerHandoffWait
	runOwnerHandoffWait = 200 * time.Millisecond
	t.Cleanup(func() { runOwnerHandoffWait = prevWait })
	prevRetry := runRetryMaxWait
	runRetryMaxWait = 100 * time.Millisecond
	t.Cleanup(func() { runRetryMaxWait = prevRetry })

	h := newRunHarness(t)
	mkTraderRow(t, h, "tr")
	stuckSlot(t, h, "tr")

	at := h.mk("tr")
	t.Cleanup(func() { waitRunRetryIdle(t, at) }) // stop the armed retry reading the seams before they are restored (race)
	if err := at.Run(); !errors.Is(err, ErrTraderOldInstanceAlive) {
		t.Fatalf("a handoff timeout must refuse, got %v", err)
	}
	waitGateBlock(t, "tr", "trader_run_retry_gave_up", 1)
	if _, tbl := telemetry.GateBlockSnapshot(); tbl["tr"]["trader_run_retried"] != 0 {
		t.Fatalf("a retry that gave up must not have run, got trader_run_retried=%v", tbl["tr"]["trader_run_retried"])
	}
	if at.runRetryArmed.Load() {
		t.Fatalf("the give-up must clear the armed flag (no leaked retry)")
	}
}

// TestStopDuringRunRetryCancelsRestart — W26 N4. A Stop asked for while the
// armed retry waits must cancel the restart: the refused instance (not
// running) must NOT resurrect when the old run finally exits. MUTANT: drop the
// stopGen check in armRunRetry → RED (the trader restarts).
func TestStopDuringRunRetryCancelsRestart(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	prevWait := runOwnerHandoffWait
	runOwnerHandoffWait = 200 * time.Millisecond
	t.Cleanup(func() { runOwnerHandoffWait = prevWait })
	prevRetry := runRetryMaxWait
	runRetryMaxWait = 10 * time.Second
	t.Cleanup(func() { runRetryMaxWait = prevRetry })

	h := newRunHarness(t)
	mkTraderRow(t, h, "tr")
	slot := stuckSlot(t, h, "tr")

	at := h.mk("tr")
	t.Cleanup(func() { waitRunRetryIdle(t, at) }) // stop the armed retry reading the seams before they are restored (race)
	if err := at.Run(); !errors.Is(err, ErrTraderOldInstanceAlive) {
		t.Fatalf("a handoff timeout must refuse, got %v", err)
	}

	// The operator stops the trader while the retry waits for the old run.
	at.Stop()
	// The old run finally exits.
	releaseRunOwner("tr", slot)

	waitGateBlock(t, "tr", "trader_run_retry_stopped", 1)
	time.Sleep(50 * time.Millisecond) // let a (wrong) retry attempt land, if any
	if liveLoop(at) || installed(at) {
		t.Fatalf("Stop during the wait must cancel the retry: loop=%v installed=%v", liveLoop(at), installed(at))
	}
	if _, tbl := telemetry.GateBlockSnapshot(); tbl["tr"]["trader_run_retried"] != 0 {
		t.Fatalf("a cancelled retry must not run, got trader_run_retried=%v", tbl["tr"]["trader_run_retried"])
	}
}

// waitRunRetryIdle waits (bounded) until the armed run retry goroutine has
// finished, so a test's seam-restore cleanup (runOwnerHandoffWait /
// runRetryMaxWait) never races the goroutine still reading them (-race).
func waitRunRetryIdle(t *testing.T, at *AutoTrader) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for at.runRetryArmed.Load() {
		if time.Now().After(deadline) {
			t.Errorf("the armed run retry did not finish within 5s")
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
