package trader

import (
	"strings"
	"testing"
	"time"

	"vl/market"
	"vl/store"
)

// R-F (owner ruling 2026-10-04 "do all as mentor", METHOD §6 Mode B: the
// mentor charts the CONTRACT itself). These pins drive the production call
// sites: mentorSeedAtStart (the seed read) and mentorOnContractRoll →
// mentorEvalOnce (the roll rebuild).

func mentorRollSeedAT(t *testing.T, st *store.Store, id string) *AutoTrader {
	t.Helper()
	return &AutoTrader{
		id: id,
		config: AutoTraderConfig{
			StrategyConfig: &store.StrategyConfig{
				RiskControl: store.RiskControlConfig{MentorMode: true},
			},
		},
		store: st,
	}
}

// TestMentorSeedDropsIsolatedImportSnapshots: the one-bar-per-day wave-101
// snapshots on a contract's back-month days are not seed bars. Mutant: make
// mentorSeedBarsFromRows return the unfiltered series → the 1m depth is 3 too
// high → RED.
func TestMentorSeedDropsIsolatedImportSnapshots(t *testing.T) {
	st := mentorSeedStore(t)
	now := time.Now().UnixMilli()
	bh := store.NewBarHistoryStore(st.GormDB())
	rows := mentorSeedBars1m(now)
	if err := bh.InsertBars(rows); err != nil {
		t.Fatalf("InsertBars: %v", err)
	}
	closed := 0
	for _, r := range rows {
		if r.OpenTimeMs+60_000 <= now {
			closed++
		}
	}
	// three lonely import snapshots, a day apart, 20+ days before the dense tape
	first := rows[0].OpenTimeMs - 20*24*3600_000
	var snaps []store.BarHistoryDB
	for i := int64(0); i < 3; i++ {
		snaps = append(snaps, store.BarHistoryDB{
			Symbol: "MNQ", TF: "1m", Contract: "MNQ 12-26", Source: store.BarSourceHistoricalImport,
			OpenTimeMs: first + i*24*3600_000, O: 100, H: 101, L: 99, C: 100, V: 1,
		})
	}
	if _, _, err := bh.ImportBars(snaps); err != nil {
		t.Fatalf("ImportBars: %v", err)
	}
	at := mentorRollSeedAT(t, st, "t-seed-snapshots")
	wireMentorPlacementSeams(t)
	at.mentorSeedAtStart()

	if d, ok := mentorSourceDepth("1m EMA34"); !ok || d != closed {
		t.Fatalf("1m depth %d/%v, want %d (the 3 isolated import snapshots must not count)", d, ok, closed)
	}
	if !at.mentorRollWired.Load() {
		t.Fatalf("mentorSeedAtStart must register the roll listener (R-F)")
	}
}

// TestMentorSeedKeepsDenseImportFill: a DENSE import fill (adjacent minutes)
// is real history and stays.
func TestMentorSeedKeepsDenseImportFill(t *testing.T) {
	var rows []store.BarHistoryDB
	base := int64(1_700_000_000_000)
	for i := int64(0); i < 10; i++ {
		rows = append(rows, store.BarHistoryDB{OpenTimeMs: base + i*60_000, Source: store.BarSourceHistoricalImport, Contract: "MNQ 12-26"})
	}
	rows = append(rows, store.BarHistoryDB{OpenTimeMs: base + 5*24*3600_000, Source: store.BarSourceHistoricalImport, Contract: "MNQ 12-26"})
	got := mentorSeedBarsFromRows(rows)
	if len(got) != 10 {
		t.Fatalf("kept %d bars, want the 10 dense ones (the lone snapshot dropped)", len(got))
	}
}

// TestMentorRollRebuildsEvaluatorFromNewContract: after a roll the tick HOLDS
// until the store settles, then the evaluator is rebuilt from the new
// contract's own rows. Mutants: (1) delete the mentorRebuildAfterRoll call in
// mentorRollApply → the retired-scale EMA survives → RED; (2) return true
// while not ready → the held tick steps the stale evaluator → RED.
func TestMentorRollRebuildsEvaluatorFromNewContract(t *testing.T) {
	st := mentorSeedStore(t)
	now := time.Now().UnixMilli()
	if err := store.NewBarHistoryStore(st.GormDB()).InsertBars(mentorSeedBars1m(now)); err != nil {
		t.Fatalf("InsertBars: %v", err)
	}
	at := mentorRollSeedAT(t, st, "t-roll-rebuild")
	wireMentorPlacementSeams(t)
	at.mentorSeedAtStart()

	const retired = 1.0
	at.mentorEval.State.EMA34 = retired // the retired contract's scale
	old := at.mentorEval

	prev := mentorRollSettle
	mentorRollSettle = 1500 * time.Millisecond
	t.Cleanup(func() { mentorRollSettle = prev })

	bars := []market.Kline{{OpenTime: now - 120_000, CloseTime: now - 60_001, Open: 29000, High: 29001, Low: 28999, Close: 29000}}
	at.mentorOnContractRoll("MNQ", "MNQ 09-26", "MNQ 12-26", time.Now())

	// HOLD: not settled yet — the tick must not step the stale evaluator.
	at.mentorEvalOnce(bars)
	if at.mentorEval != old || at.mentorEval.State.EMA34 != retired || at.mentorLastTickOpen != 0 {
		t.Fatalf("a tick between the roll and the settle must HOLD (eval swapped=%v ema=%v lastTick=%d)",
			at.mentorEval != old, at.mentorEval.State.EMA34, at.mentorLastTickOpen)
	}
	if at.mentorRollPending.Load() == nil {
		t.Fatalf("the roll must stay pending until the rebuild")
	}

	deadline := time.Now().Add(10 * time.Second)
	for at.mentorRollPending.Load() != nil && !at.mentorRollPending.Load().ready.Load() {
		if time.Now().After(deadline) {
			t.Fatal("roll never settled")
		}
		time.Sleep(20 * time.Millisecond)
	}
	at.mentorEvalOnce(bars)

	if at.mentorEval == old {
		t.Fatalf("the evaluator must be REBUILT after a roll")
	}
	if e := at.mentorEval.State.EMA34; e < 20000 {
		t.Fatalf("1m EMA34 %.2f is still the retired scale; the rebuild must seed from the new contract's rows", e)
	}
	if at.mentorRollPending.Load() != nil {
		t.Fatalf("the rebuild must clear the pending roll")
	}
	if at.mentorLastTickOpen != bars[0].OpenTime {
		t.Fatalf("after the rebuild the tick must proceed (lastTick=%d)", at.mentorLastTickOpen)
	}
}

// TestMentorRollIgnoresOtherSymbols: an ES roll must not rebuild the MNQ
// evaluator.
func TestMentorRollIgnoresOtherSymbols(t *testing.T) {
	at := mentorRollSeedAT(t, mentorSeedStore(t), "t-roll-es")
	at.mentorOnContractRoll("ES", "ES 09-26", "ES 12-26", time.Now())
	if at.mentorRollPending.Load() != nil {
		t.Fatalf("a non-MNQ roll must not hold or rebuild the mentor evaluator")
	}
}

// TestMentorRollRetiresRestingArms: every registered arm is sent to cancel at
// the rebuild — a level order priced on the retired contract rests with no
// expiry and the fresh evaluator has no state to cancel it.
func TestMentorRollRetiresRestingArms(t *testing.T) {
	st := mentorSeedStore(t)
	at := mentorRollSeedAT(t, st, "t-roll-arms")
	wireMentorPlacementSeams(t)
	at.mentorSeedAtStart()

	mentorRegisterLiveArm("lvl-roll-test", 4242, "long", 29000)
	t.Cleanup(func() {
		mentorLiveMu.Lock()
		delete(mentorLiveArms, "lvl-roll-test")
		mentorLiveMu.Unlock()
	})
	sum := func() int {
		m := MentorCountSnapshot()
		return m["cancel_refused_unavailable"] + m["cancel_refused_row_gone"] + m["cancel_requested"]
	}
	before := sum()
	at.mentorRebuildAfterRoll(&mentorRollEvent{from: "MNQ 09-26", to: "MNQ 12-26", at: time.Now()})
	after := sum()
	if after <= before {
		t.Fatalf("the rebuild must route every registered arm through mentorCancelArm (counters %d → %d)", before, after)
	}
	if !strings.Contains(strings.Join(mentorLiveArmIDs(), ","), "lvl-roll-test") {
		t.Fatalf("registry fixture lost")
	}
}
