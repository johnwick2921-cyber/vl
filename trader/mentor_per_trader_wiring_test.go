package trader

import (
	"testing"

	"vl/kernel/mentor"
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
