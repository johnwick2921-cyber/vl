package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
)

// ── REL18-B: mentor per-trader wiring pins ─────────────────────────────────
//
// These pins drive the PRODUCTION call sites (mentorDoneAfterWinGate,
// mentorStopAfterLossGate, mentorAddGate, mentorOpenPositionActive,
// mentorCount/mentorCountSnapshot, mentorSeamBootLine) with TWO AutoTraders
// (A and B, distinct ids) and assert that each trader reads ITS OWN seam and
// ITS OWN counters. The pre-fix bug: the seams were package-level vars that
// captured the LAST-CONSTRUCTED trader, so A read B's day net / open side /
// loss. A mutant that re-globals any of these fields makes the corresponding
// pin RED.

// twoTraderA builds trader A (id "t-a") as the flat, no-win, no-loss trader.
func twoTraderA(t *testing.T) *AutoTrader {
	t.Helper()
	at := funnelAT(t)
	at.id = "t-a"
	return at
}

// twoTraderB builds trader B (id "t-b") as the trader whose state must NOT
// leak into A.
func twoTraderB(t *testing.T) *AutoTrader {
	t.Helper()
	at := funnelAT(t)
	at.id = "t-b"
	return at
}

// TestMentorDoneAfterWinGateReadsOwnDayNet: B closes a winner and is net
// positive; A closed nothing. A must keep trading (not refused); B must be
// refused. A mutant that re-globals the day-net / closed-profit seams makes A
// read B's win and refuse.
func TestMentorDoneAfterWinGateReadsOwnDayNet(t *testing.T) {
	// B first, so a re-globaled seam would capture B's winning read.
	b := twoTraderB(t)
	b.mentorDayNetSource = func() (float64, bool) { return 120, true }
	b.mentorClosedProfitSource = func() (bool, bool) { return true, true }

	a := twoTraderA(t)
	a.mentorDayNetSource = func() (float64, bool) { return 0, true }
	a.mentorClosedProfitSource = func() (bool, bool) { return false, true }

	if refused, why := a.mentorDoneAfterWinGate(); refused {
		t.Fatalf("A must read A's own (no-win, flat) day, got refused: %s", why)
	}
	if refused, _ := b.mentorDoneAfterWinGate(); !refused {
		t.Fatal("B with a closed win and a net-positive day must be refused")
	}
}

// TestMentorStopAfterLossGateReadsOwnLossAndKnob: B closed a loss; A closed
// nothing. Both have the knob ON. A must not refuse (it reads A's own no-loss
// read), B must refuse. Then A closes a loss itself and must refuse.
func TestMentorStopAfterLossGateReadsOwnLossAndKnob(t *testing.T) {
	on := true

	b := twoTraderB(t)
	b.config.StrategyConfig.RiskControl.MentorStopAfterLoss = &on
	b.mentorClosedLossSource = func() (bool, bool) { return true, true } // B lost

	a := twoTraderA(t)
	a.config.StrategyConfig.RiskControl.MentorStopAfterLoss = &on
	a.mentorClosedLossSource = func() (bool, bool) { return false, true } // A no loss

	if refused, why := a.mentorStopAfterLossGate(); refused {
		t.Fatalf("A must read A's own no-loss day (B's loss is B's), got refused: %s", why)
	}
	if refused, _ := b.mentorStopAfterLossGate(); !refused {
		t.Fatal("B with its own closed loss must be refused")
	}

	// A's OWN loss trips A.
	a.mentorClosedLossSource = func() (bool, bool) { return true, true }
	if refused, _ := a.mentorStopAfterLossGate(); !refused {
		t.Fatal("A with its own closed loss must be refused")
	}
}

// TestMentorAddGateReadsOwnOpenSide: B holds a long position; A is flat. A's
// long entry must NOT be refused by B's open long. Once A itself holds long, A
// IS refused. A mutant that re-globals the open-side seam makes A refuse on
// B's position.
func TestMentorAddGateReadsOwnOpenSide(t *testing.T) {
	b := twoTraderB(t)
	b.mentorOpenSideSource = func() string { return "long" } // B holds long

	a := twoTraderA(t)
	a.mentorOpenSideSource = func() string { return "" } // A flat

	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, Side: mentor.SideLong}
	if refused, why := a.mentorAddGate(in); refused {
		t.Fatalf("A must not refuse on B's open long, got refused: %s", why)
	}
	// B still holds its own long: a same-side add on B must be refused — this
	// is the mutant detector (a re-globaled seam would read A's flat side and
	// let B add).
	if refused, _ := b.mentorAddGate(in); !refused {
		t.Fatal("B with its own open long must refuse a same-side add")
	}

	// A itself holds long → a same-side add is refused.
	a.mentorOpenSideSource = func() string { return "long" }
	if refused, _ := a.mentorAddGate(in); !refused {
		t.Fatal("A with its own open long must refuse a same-side add")
	}
}

// TestMentorOpenPositionActiveReadsOwnSide: the day-stop sweep's never-add
// cancel reads mentorOpenPositionActive (per-trader open side + per-trader
// position count). B's open long must NOT make A look open — so B's position
// can never cancel A's resting arms.
func TestMentorOpenPositionActiveReadsOwnSide(t *testing.T) {
	b := twoTraderB(t)
	b.mentorOpenSideSource = func() string { return "long" }

	a := twoTraderA(t)
	a.mentorOpenSideSource = func() string { return "" }

	if a.mentorOpenPositionActive() {
		t.Fatal("A must read flat even though B holds long (B's position must never cancel A's arms)")
	}
	if !b.mentorOpenPositionActive() {
		t.Fatal("B with its own open long must be active")
	}
}

// TestMentorCountersArePerTrader: A's counter bumps never appear in B's
// snapshot and vice versa. A mutant that re-globals mentorCounters sums them
// (the live bug: 'mentor positions rebuilt: N' printed the SUM).
func TestMentorCountersArePerTrader(t *testing.T) {
	a := twoTraderA(t)
	b := twoTraderB(t)

	a.mentorCount("refused_no_max")
	a.mentorCount("refused_no_max")
	b.mentorCount("placement_held")

	as := a.mentorCountSnapshot()
	bs := b.mentorCountSnapshot()
	if as["refused_no_max"] != 2 {
		t.Fatalf("A refused_no_max = %d, want 2", as["refused_no_max"])
	}
	if as["placement_held"] != 0 {
		t.Fatalf("A placement_held = %d, want 0 (B's counter leaked into A)", as["placement_held"])
	}
	if bs["placement_held"] != 1 {
		t.Fatalf("B placement_held = %d, want 1", bs["placement_held"])
	}
	if bs["refused_no_max"] != 0 {
		t.Fatalf("B refused_no_max = %d, want 0 (A's counter leaked into B)", bs["refused_no_max"])
	}
}

// TestMentorSeamBootLineReadsOwnSeams: A wires day_net + closed_profit, B does
// not. A's boot line must show them wired and B's must show them missing — the
// per-trader seams are read from THIS trader, not the last one constructed.
func TestMentorSeamBootLineReadsOwnSeams(t *testing.T) {
	b := twoTraderB(t) // unwired day_net / closed_profit
	a := twoTraderA(t)
	a.mentorDayNetSource = func() (float64, bool) { return 0, true }
	a.mentorClosedProfitSource = func() (bool, bool) { return false, true }

	al := a.mentorSeamBootLine()
	bl := b.mentorSeamBootLine()
	if !textHas(al, "day_net=wired") || !textHas(al, "closed_profit=wired") {
		t.Fatalf("A boot line must show its own day_net/closed_profit wired: %q", al)
	}
	if !textHas(bl, "day_net=missing") || !textHas(bl, "closed_profit=missing") {
		t.Fatalf("B boot line must show its own day_net/closed_profit missing (A's wiring must not leak): %q", bl)
	}
}

// seedClosedWire inserts one CLOSED row with a resolved corrected P&L.
func seedClosedWire(t *testing.T, st *store.Store, traderID string, exitMs int64, corrected float64) {
	t.Helper()
	row := &store.TraderPosition{
		TraderID: traderID, Account: "", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryPrice: 100, ExitPrice: 100 + corrected, RealizedPnL: corrected,
		PnlCorrected: &corrected, Status: "CLOSED", CloseReason: "sync", Source: "sync",
		EntryTime: exitMs - 60_000, ExitTime: exitMs, CreatedAt: exitMs, UpdatedAt: exitMs,
	}
	if err := st.GormDB().Create(row).Error; err != nil {
		t.Fatalf("seed closed row for %s: %v", traderID, err)
	}
}

// TestMentorProductionWiringReadsOwnTrader drives the PRODUCTION wiring on TWO
// AutoTraders with DISTINCT fake traders and stores (A flat / no win / a CME-day
// loss; B long / +120 win / no loss), B wired LAST. It asserts through the
// production gates that each trader reads ITS OWN positions and ITS OWN session
// P&L. This is the FOLD the CTO required: the six direct-injection pins never
// ran the production wiring, so MB1 (a closure capturing the last-wired trader)
// survived.
func TestMentorProductionWiringReadsOwnTrader(t *testing.T) {
	ct := kernel.CTLocation()
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, ct) // 09:00 CT

	// A: flat, no NY-day win, but one CME-session-day loss (18:00 CT the prior
	// day — after the 17:00 CT session roll, before today's 08:30 NY boundary).
	aSt := mentorSeedStore(t)
	if err := store.NewPositionStore(aSt.GormDB()).InitTables(); err != nil {
		t.Fatalf("A positions migrate: %v", err)
	}
	seedClosedWire(t, aSt, "t-a", time.Date(2026, 10, 12, 18, 0, 0, 0, ct).UnixMilli(), -40)

	// B: long, one NY-day win (08:45 CT today) and no loss.
	bSt := mentorSeedStore(t)
	if err := store.NewPositionStore(bSt.GormDB()).InitTables(); err != nil {
		t.Fatalf("B positions migrate: %v", err)
	}
	seedClosedWire(t, bSt, "t-b", time.Date(2026, 10, 13, 8, 45, 0, 0, ct).UnixMilli(), 120)

	on := true
	a := &AutoTrader{
		id:     "t-a",
		trader: &fakeMentorPositionReader{positions: []map[string]interface{}{}}, // flat
		store:  aSt,
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{
			MentorMode: true, MentorStopAfterLoss: &on,
		}}},
	}
	b := &AutoTrader{
		id:     "t-b",
		trader: &fakeMentorPositionReader{positions: []map[string]interface{}{{"side": "long"}}}, // long
		store:  bSt,
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{
			MentorMode: true, MentorStopAfterLoss: &on,
		}}},
	}

	// PRODUCTION wiring, B wired LAST — the exact bug shape MB1 exploits.
	a.mentorWireProductionSeams()
	b.mentorWireProductionSeams()
	// The wiring re-pins mentorNowSource to the wall clock; re-pin the fixed
	// clock so the day-boundary reads below are deterministic (the closures
	// read mentorClockNow() lazily at call time).
	setMentorNowSource(func() time.Time { return now })
	t.Cleanup(func() { setMentorNowSource(nil) })

	// (audit #4) done-after-win: A has no NY-day win → keeps trading; B has a
	// +120 win → refused. A mutant that captures the last-wired trader's day
	// read makes A read B's +120 and refuse here.
	if refused, why := a.mentorDoneAfterWinGate(); refused {
		t.Fatalf("A must read A's own NY day (net 0, no win), got refused: %s", why)
	}
	if refused, _ := b.mentorDoneAfterWinGate(); !refused {
		t.Fatal("B with its own +120 win must be refused")
	}

	// (audit #4) stop-after-loss: A closed a CME-session-day loss → refused;
	// B closed none → keeps trading. A mutant that captures the last-wired
	// trader's loss read makes A read B's no-loss and pass here.
	if refused, why := a.mentorStopAfterLossGate(); !refused {
		t.Fatalf("A must read A's own CME-day loss and refuse, got allow (%q)", why)
	}
	if refused, _ := b.mentorStopAfterLossGate(); refused {
		t.Fatal("B with no CME-day loss must keep trading")
	}

	// (audit #5) never-add / open-position-active: A flat → not refused and
	// not active; B long → refused and active. A mutant that captures the
	// last-wired trader's positions makes A read B's long and refuse.
	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, Side: mentor.SideLong}
	if refused, why := a.mentorAddGate(in); refused {
		t.Fatalf("A must read A's own (flat) positions, got refused: %s", why)
	}
	if refused, _ := b.mentorAddGate(in); !refused {
		t.Fatal("B with its own long must refuse a same-side add")
	}
	if a.mentorOpenPositionActive() {
		t.Fatal("A must read flat (B's long must never make A look open)")
	}
	if !b.mentorOpenPositionActive() {
		t.Fatal("B with its own long must be active")
	}
}
