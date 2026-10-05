package trader

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// realFillAT builds a loopback trader with the FU-1 real-fill plumbing: a
// bounded fill channel and a RealFillOnly evaluator. Every FU-1 trader pin
// seeds a pend through the REAL Limits.Apply, then drives the REAL
// onArmedOrderUpdate fill callback and mentorEvalOnce drain — the production
// call sites.
func realFillAT(t *testing.T) (*AutoTrader, *store.Store, *store.ArmedOrderStore) {
	t.Helper()
	at, st, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	at.mentorFillCh = make(chan mentorFillReceipt, 8)
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	cfg.RealFillOnly = true
	at.mentorEval = mentor.New(cfg)
	// The fill callback reads the 1m provider for the wave seed; pin it to a
	// fixed bar so the test is deterministic.
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(_, _ string, _ int) []market.Kline {
		return []market.Kline{{Open: 29600, High: 29620, Low: 29580, Close: 29610}}
	}
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	return at, st, ledger
}

// seedRealFillPend registers a real-fill pend for armID through the REAL
// Limits.Apply (the production placement-time hook).
func seedRealFillPend(t *testing.T, at *AutoTrader, armID string) {
	t.Helper()
	now := time.Now().UnixMilli()
	levels := []mentor.Level{{Key: "old_extreme:29650", Kind: mentor.KindOldExtreme, Price: 29650}}
	prev := market.Kline{Open: 29500, High: 29590, Low: 29450, Close: 29580}
	cur := market.Kline{Open: 29580, High: 29590, Low: 29500, Close: 29550}
	seed := mentor.Intent{
		Action: mentor.PlaceStopEntry, ArmID: armID, Setup: "PHL",
		Side: mentor.SideLong, Price: 29600, Stop: 29590, Target: 29630,
		StopPts: 10, TargetPts: 30, ExpiryMs: now + 86400_000,
		Anchor: 29600, AnchorKey: "key_level:29600",
	}
	if out := at.mentorEval.State.Limits.Apply([]mentor.Intent{seed}, prev, cur, now, levels, at.mentorEval.Cfg); len(out) != 1 {
		t.Fatalf("seed pend: Apply kept %d, want 1", len(out))
	}
}

// fillMentorRow writes a filled mentor ledger row whose scenario resolves to
// armID, then drives the REAL fill callback.
func fillMentorRow(t *testing.T, at *AutoTrader, ledger *store.ArmedOrderStore, armID, signalID string) {
	t.Helper()
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: armID + "-123", Side: "long", EntryPx: 29600, StopPx: 29590,
		TargetPx: 29630, Kind: "stop_entry", Condition: "PHL",
		State: store.StateWorking, SignalID: signalID,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(2),
	}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{
		SignalID: signalID, State: "filled", Quantity: 2, FillPrice: 29600, Account: "Sim101",
	}, ledger)
}

// TestMentorRealFillRegistersOneLegThroughTheQueue is the FU-1 pin (b): a REAL
// fill, through the production fill callback and the queued drain inside
// mentorEvalOnce, registers EXACTLY one leg. MUTANT: drop the mentorDrainFills
// call from mentorEvalOnce → the leg never registers → RED.
func TestMentorRealFillRegistersOneLegThroughTheQueue(t *testing.T) {
	at, _, ledger := realFillAT(t)
	seedRealFillPend(t, at, "lvl-9")
	fillMentorRow(t, at, ledger, "lvl-9", "sig-fill-9")

	// mentorEvalOnce early-returns (the bar is already ticked) but the drain
	// runs first, under mentorEvalMu — this is the wiring the pin guards.
	bar := market.Kline{OpenTime: 1, CloseTime: 60_000 - 1, Open: 29600, High: 29620, Low: 29580, Close: 29610}
	at.mentorLastTickOpen = bar.OpenTime
	at.mentorEvalOnce([]market.Kline{bar})

	l := at.mentorEval.State.Limits.Long
	if l == nil || l.Entries != 1 || !l.PHLFilled {
		t.Fatalf("a real fill must register exactly one leg entry, got %+v", l)
	}
}

// TestMentorRealFillDedupeOnReplay is the FU-1 mutant kill ("double-record on
// replay"): two fill receipts with the SAME signal id (a partial-then-full, or
// a retransmit) register once and count no spurious refusal. MUTANT: remove
// the receipt dedupe in RecordFill → the replay counts a no-pend refusal → RED.
func TestMentorRealFillDedupeOnReplay(t *testing.T) {
	at, _, ledger := realFillAT(t)
	seedRealFillPend(t, at, "lvl-10")
	fillMentorRow(t, at, ledger, "lvl-10", "sig-fill-10")
	// A retransmit of the SAME receipt must not double-register or count a
	// refusal — it is deduped by receipt id in RecordFill.
	at.mentorFillCh <- mentorFillReceipt{receiptID: "sig-fill-10", armID: "lvl-10", lo: 29580, hi: 29620}
	at.mentorDrainFills()
	l := at.mentorEval.State.Limits.Long
	if l == nil || l.Entries != 1 {
		t.Fatalf("a retransmitted receipt must register once, got %+v", l)
	}
	if n := at.mentorEval.State.Limits.Refusals["record_fill_no_pend"]; n != 0 {
		t.Fatalf("a deduped receipt must not count a no-pend refusal, got %d", n)
	}
}

// TestMentorTransientHoldThenFillStillOneLeg is the FU-1 pin (c): a transient
// hold (a no-op for the pend — the armed-pass refusal does NOT drop it) then a
// real placement + fill registers EXACTLY one leg. The opposite bug (dropping
// the pend on the hold) would leave the fill uncounted.
func TestMentorTransientHoldThenFillStillOneLeg(t *testing.T) {
	at, _, ledger := realFillAT(t)
	seedRealFillPend(t, at, "lvl-11")
	// The transient hold: nothing touches the pend (a far_side_unproven /
	// maintenance_hold refusal is a no-op for Limits). A candle touching the
	// entry must NOT fill it (real-fill-only), and the pend must survive.
	at.mentorEval.State.Limits.Apply(nil,
		market.Kline{Open: 29580, High: 29590, Low: 29500, Close: 29550},
		market.Kline{Open: 29590, High: 29620, Low: 29540, Close: 29610},
		time.Now().UnixMilli()+60_000, nil, at.mentorEval.Cfg)
	if at.mentorEval.State.Limits.Long != nil {
		t.Fatalf("real-fill-only: the held pend must not fill on a candle touch")
	}
	// The hold clears, the arm places and fills for real → exactly one leg.
	fillMentorRow(t, at, ledger, "lvl-11", "sig-fill-11")
	at.mentorDrainFills()
	if l := at.mentorEval.State.Limits.Long; l == nil || l.Entries != 1 {
		t.Fatalf("a transient-hold-then-fill must register exactly one leg, got %+v", l)
	}
}

// TestMentorRealFillConcurrentEnqueueDrain is the FU-1 pin (d): the fill
// callback (executor goroutine) enqueues while the drain consumes — no
// cross-goroutine evaluator mutation. Run under -race this proves the queue is
// the only handoff and the Limits mutation stays single-goroutine.
func TestMentorRealFillConcurrentEnqueueDrain(t *testing.T) {
	at, _, _ := realFillAT(t)
	const n = 8
	// Seed n pends with the leg budget OFF so every one can fill (the budget
	// caps at 2; the fill registration itself does not).
	cfg := at.mentorEval.Cfg
	cfg.LegBudgetEnabled = false
	at.mentorEval.Cfg = cfg
	for i := 0; i < n; i++ {
		seedRealFillPend(t, at, "lvl-race-"+strconv.Itoa(i))
	}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			at.mentorFillCh <- mentorFillReceipt{receiptID: "sig-race-" + strconv.Itoa(i), armID: "lvl-race-" + strconv.Itoa(i), lo: 29580, hi: 29620}
		}()
	}
	wg.Wait()
	at.mentorDrainFills()
	if l := at.mentorEval.State.Limits.Long; l == nil || l.Entries != n {
		t.Fatalf("concurrent enqueue + drain must register %d legs, got %+v", n, l)
	}
}

