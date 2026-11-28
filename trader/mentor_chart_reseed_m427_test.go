package trader

import (
	"os"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// m427/2 — the replay hold is released on a CLOSED market (vl relaunched in the
// break / over a weekend): the refill generation bumps, but the display seed was
// built from the pre-release store and no Tick will ever run. The periodic
// closed-market path must rebuild it. Production call site:
// mentorRefreshChartSeedOnClosed (called from cmeSessionClosedSkip) over a real
// store, after the real mentorSeedAtStart.
// Mutant: drop the body of the rebuild (or the generation compare) -> RED.
func TestM427ClosedMarketChartReseedAfterRefill(t *testing.T) {
	bars := loadSeedStateBars(t)
	at := seedStateTrader(t)
	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = nil
	defer func() { market.FuturesBarsProvider = old }()
	bh := store.NewBarHistoryStore(at.store.GormDB())
	storeLast := seedStateCT(8, 15, 0).UnixMilli()
	if err := bh.InsertBars(seedStateRows(bars, func(b seedStateBar) bool { return !b.refill && b.k.OpenTime <= storeLast })); err != nil {
		t.Fatal(err)
	}
	setMentorNowSource(func() time.Time { return seedStateCT(8, 22, 40) })
	at.mentorSeedAtStart()
	before := at.mentorEval.State.ChartSeededAt
	if before == 0 {
		t.Fatal("control: the boot display seed must have run")
	}
	closed := time.Date(2026, 10, 24, 10, 0, 0, 0, kernel.CTLocation()) // Saturday
	open := seedStateCT(8, 23, 30)
	if kernel.IsCMEOpen(closed) || !kernel.IsCMEOpen(open) {
		t.Fatal("premise: Saturday closed, Wednesday 08:23 open")
	}

	// No refill yet: nothing to rebuild.
	at.mentorRefreshChartSeedOnClosed(closed)
	if got := at.mentorEval.State.ChartSeededAt; got != before {
		t.Fatalf("no refill -> no rebuild, ChartSeededAt %d -> %d", before, got)
	}

	// The release lands rows the boot read missed, and announces it.
	tick := seedStateCT(8, 23, 0).UnixMilli()
	if err := bh.InsertBars(seedStateRows(bars, func(b seedStateBar) bool {
		return b.refill || b.k.OpenTime > storeLast && b.k.OpenTime <= tick
	})); err != nil {
		t.Fatal(err)
	}
	ntTrader.NoteHistoryRefill("MNQ", "1m", 244, "replay released 244 MNQ 1m row(s) to the store")

	// OPEN market: the Tick path owns the re-seed; the closed hook stays out.
	at.mentorRefreshChartSeedOnClosed(open)
	if got := at.mentorEval.State.ChartSeededAt; got != before {
		t.Fatalf("open market must not rebuild here, ChartSeededAt %d -> %d", before, got)
	}
	stateGen := at.mentorSeedHistGen

	at.mentorRefreshChartSeedOnClosed(closed)
	after := at.mentorEval.State.ChartSeededAt
	if after <= before {
		t.Fatalf("closed-market refill must rebuild the display seed: ChartSeededAt %d -> %d", before, after)
	}
	if at.mentorSeedHistGen != stateGen {
		t.Fatal("the display rebuild must NOT claim the generation the first Tick's state re-seed waits on")
	}
	if len(at.mentorEval.State.Levels) != 0 {
		t.Fatalf("display only: State.Levels must stay empty, got %d", len(at.mentorEval.State.Levels))
	}
	if c := at.mentorCountSnapshot(); c["chart_reseed_closed"] != 1 {
		t.Fatalf("rebuild counted once: %v", c)
	}
	// Same generation again: idempotent.
	at.mentorRefreshChartSeedOnClosed(closed)
	if c := at.mentorCountSnapshot(); c["chart_reseed_closed"] != 1 {
		t.Fatalf("no new refill -> no second rebuild: %v", c)
	}
	// After a real Tick the marker clears and the hook never touches it again.
	win := make([]market.Kline, 0)
	for _, b := range bars {
		if b.k.OpenTime <= tick {
			win = append(win, b.k)
		}
	}
	at.mentorEval.Tick(win[len(win)-mentorBars1mDepth:], mentor.BarCloseInstant(win[len(win)-1]))
	ntTrader.NoteHistoryRefill("MNQ", "1m", 1, "another")
	at.mentorRefreshChartSeedOnClosed(closed)
	if at.mentorEval.State.ChartSeededAt != 0 {
		t.Fatal("after a real Tick the closed hook must not re-seed")
	}
}

// The closed-market path must actually call the rebuild (wiring pin; the real
// cmeSessionClosedSkip sleeps a 3-minute backoff, so it is a source check on the
// production function, like TestMentorSeedAtStartCallsTheChartDisplaySeed).
func TestM427ClosedSkipCallsTheChartReseed(t *testing.T) {
	src, err := os.ReadFile("auto_trader_loop.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	i := strings.Index(s, "func (at *AutoTrader) cmeSessionClosedSkip()")
	j := strings.Index(s[i:], "\n}\n")
	if i < 0 || j < 0 || !strings.Contains(s[i:i+j], "at.mentorRefreshChartSeedOnClosed(now)") {
		t.Fatal("cmeSessionClosedSkip no longer calls mentorRefreshChartSeedOnClosed")
	}
}
