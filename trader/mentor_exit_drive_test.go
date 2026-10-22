package trader

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── MENTOR EXIT DRIVE (DS-107) — pins + mutants ─────────────────────────────
//
// The pure rules (mentorRR11Stop / mentorLegStopB) are pinned directly; the
// loop (mentorExitDrivePos / mentorExitDrive) is pinned at the per-leg signal
// move seam so "which signal moved to which stop" is asserted, not inferred.
// dev's entry is ONE row → ONE position, so the live position is a SINGLE leg
// (Legs[0] = the whole position, Final = the runner).

type driveMove struct {
	signalID string
	side     string
	newStop  float64
}

// newDriveAT builds a minimal AutoTrader (mentor mode ON, a zero-value
// TCPTrader as the armed trader — the seam never touches it) and captures every
// signal-keyed move through mentorMoveStopForSignalWire.
//
// Release #22: these pins are the PRE-R3 rule, so the helper sets
// mentor_trail_mode=off explicitly (the default is now candle — pinned at the
// wire in mentor_r22_exit_test.go). The broker-judged never-widen (P2-3) reads
// a broker book these seam-level pins do not have; the helper mirrors a broker
// that applied every move (broker stop = the leg's belief), so the rule math
// they pin is unchanged. The real book is exercised in mentor_r22_exit_test.go.
func newDriveAT(t *testing.T) (*AutoTrader, func() []driveMove) {
	t.Helper()
	at := &AutoTrader{
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorTrailMode: "off"}}},
		trader: &nttrader.TCPTrader{},
	}
	oldBroker := mentorBrokerLegStopRead
	mentorBrokerLegStopRead = func(_ *AutoTrader, _ *nttrader.TCPTrader, _ string, leg *mentorLeg) (float64, int) {
		return leg.Stop, mentorBrokerStopLive // a broker that applied every move
	}
	t.Cleanup(func() { mentorBrokerLegStopRead = oldBroker })
	var mu sync.Mutex
	var moves []driveMove
	old := mentorMoveStopForSignalWire
	driveLegsMu.Lock()
	driveLegs = nil
	driveLegsMu.Unlock()
	mentorMoveStopForSignalWire = func(nt *nttrader.TCPTrader, signalID, side string, newStop float64, leg int) error {
		mu.Lock()
		moves = append(moves, driveMove{signalID, side, newStop})
		mu.Unlock()
		driveLegsMu.Lock()
		driveLegs = append(driveLegs, leg)
		driveLegsMu.Unlock()
		return nil
	}
	t.Cleanup(func() {
		mentorMoveStopForSignalWire = old
		at.resetMentorCountersForTest()
	})
	return at, func() []driveMove { mu.Lock(); defer mu.Unlock(); return append([]driveMove(nil), moves...) }
}

// bPos builds a SINGLE-leg live position: Legs[0] = the whole position (the
// runner, Final=true), Legs[1] empty — dev's one-row entry.
func bPos(mode, side string, entry, stop, target float64, qty int) *mentorLivePos {
	return &mentorLivePos{
		Pos: mentorPosition{
			Symbol:  "MNQ",
			Side:    side,
			Origin:  "PHL",
			Entry:   entry,
			Stop:    stop,
			Target:  target,
			R:       2,
			Mode:    mode,
			Leg1:    qty,
			Leg2:    0,
			Leg1TP:  target,
			ArmedBE: mode == "A-resonance",
		},
		Legs: [3]mentorLeg{
			{SignalID: "entry", Qty: qty, TP: target, Stop: stop, Final: true},
			{},
		},
	}
}

func assertMoves(t *testing.T, got []driveMove, want ...driveMove) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("moves = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("move[%d] = %+v, want %+v (all: %+v)", i, got[i], want[i], got)
		}
	}
}

// pin (a): 10/20 long — close 15 → 10 (BE), 18 → 16, 16 after → stays 16;
// short mirror. [D1.2 p1 @11:36–16:07]
func TestMentorRR11Stop(t *testing.T) {
	if got := mentorRR11Stop("long", 10, 15, 20); got != 10 {
		t.Fatalf("long close 15 → %v, want 10 (BE)", got)
	}
	if got := mentorRR11Stop("long", 10, 18, 20); got != 16 {
		t.Fatalf("long close 18 → %v, want 16", got)
	}
	if got := mentorRR11Stop("long", 16, 16, 20); got != 16 {
		t.Fatalf("long close 16 after 16 → %v, want 16 (never widen)", got)
	}
	if got := mentorRR11Stop("short", 20, 15, 10); got != 20 {
		t.Fatalf("short close 15 → %v, want 20 (BE)", got)
	}
	if got := mentorRR11Stop("short", 20, 12, 10); got != 14 {
		t.Fatalf("short close 12 → %v, want 14", got)
	}
	if got := mentorRR11Stop("short", 14, 16, 10); got != 14 {
		t.Fatalf("short close 16 after 14 → %v, want 14 (never widen)", got)
	}
}

// The runner takes the TIGHTER of the 1:1 rule and the candle trail.
func TestMentorLegStopB_RunnerTrailTighter(t *testing.T) {
	if got := mentorLegStopB("long", 10, 18, 20, 15, true); got != 16 {
		t.Fatalf("long 1:1+trail → %v, want 16", got)
	}
	if got := mentorLegStopB("long", 10, 18, 20, 17.5, true); got != 17.5 {
		t.Fatalf("long trail tighter → %v, want 17.5", got)
	}
	if got := mentorLegStopB("short", 20, 12, 10, 15, true); got != 14 {
		t.Fatalf("short 1:1+trail → %v, want 14", got)
	}
	if got := mentorLegStopB("long", 10, 18, 20, 17.5, false); got != 16 {
		t.Fatalf("long no-trail → %v, want 16", got)
	}
}

// item 8 (ISB partial, R1): leg 1's +1R TP always rests at NT8. The candle-3
// partial is ONLY the case "close in profit SHORT of +1R" (leg 1 did NOT fill
// inside the candle) — the close itself [D1.4 p1 @12:51–13:01 "bán bớt. Bắt
// buộc"]. A close PAST +1R means the +1R TP already filled inside the candle
// (the 1:1 rule) → 0. A losing close → 0 (the +1R TP keeps resting).
func TestMentorISBPartialTP(t *testing.T) {
	long := mentorPosition{Side: "long", Entry: 100, R: 10}
	if got := mentorISBPartialTP(long, 108, 111, 99); got != 0 {
		t.Fatalf("close past +1R → %v, want 0 (the +1R TP already filled inside the candle)", got)
	}
	if got := mentorISBPartialTP(long, 104, 105, 99); got != 104 {
		t.Fatalf("candle-3 close in profit short of +1R → %v, want 104 (the TP is moved to a marketable limit at it)", got)
	}
	if got := mentorISBPartialTP(long, 98, 105, 95); got != 0 {
		t.Fatalf("losing close → %v, want 0 (the +1R TP keeps resting)", got)
	}
	short := mentorPosition{Side: "short", Entry: 100, R: 10}
	if got := mentorISBPartialTP(short, 92, 99, 89); got != 0 {
		t.Fatalf("short close past −1R → %v, want 0 (the +1R TP already filled)", got)
	}
	if got := mentorISBPartialTP(short, 96, 99, 95); got != 96 {
		t.Fatalf("short candle-3 close in profit short of +1R → %v, want 96", got)
	}
	if got := mentorISBPartialTP(short, 102, 103, 95); got != 0 {
		t.Fatalf("short losing close → %v, want 0", got)
	}
}

// TestMentorISBMarketableTP — the CTO's exact marketable formula: long → close −
// 8 ticks (2.00 pts), short → close + 8 ticks, rounded to the tick grid. Worked
// example from the ruling: long close 30108 → 30106.00.
func TestMentorISBMarketableTP(t *testing.T) {
	if got := mentorISBMarketableTP("long", 30108, 0.25); got != 30106.00 {
		t.Fatalf("long close 30108 → %v, want 30106.00 (close − 8 ticks)", got)
	}
	if got := mentorISBMarketableTP("short", 30737.50, 0.25); got != 30739.50 {
		t.Fatalf("short close 30737.50 → %v, want 30739.50 (close + 8 ticks)", got)
	}
	if got := mentorISBMarketableTP("SHORT", 30108, 0.25); got != 30110.00 {
		t.Fatalf("SHORT close 30108 → %v, want 30110.00", got)
	}
	// off-grid close rounds onto the grid.
	if got := mentorISBMarketableTP("long", 30108.13, 0.25); got != 30106.25 {
		t.Fatalf("long close 30108.13 → %v, want 30106.25 (rounded)", got)
	}
}

// TestMentorISBMarketableReModify — R1: a pending marketable leg-1 TP is
// re-priced to the NEW close ∓ 8 ticks once per later candle while the close
// stays in profit short of +1R; never back to "none"; the leg keeps its stop.
func TestMentorISBMarketableReModify(t *testing.T) {
	at := newDriveATForTest(t)
	old := modifyBracketWire
	t.Cleanup(func() { modifyBracketWire = old })
	var sent []float64
	modifyBracketWire = func(_ *nttrader.TCPTrader, signalID, _ string, wireLeg int, newTP float64) error {
		if signalID != "sig-1" || wireLeg != 1 {
			t.Fatalf("re-modify must address leg 1 of sig-1, got %q leg %d", signalID, wireLeg)
		}
		sent = append(sent, newTP)
		return nil
	}
	// long entry 30100, R 8 → +1R = 30108. First marketable TP 30106 already sent.
	p := bPos("B", "long", 30100, 30098, 30120, 4)
	p.Pos.Origin = "ISB"
	p.Pos.R = 8
	p.Legs[0].Wire = 1
	p.Legs[0].SignalID = "sig-1"
	p.TPModWant = 30106.0
	// In band (30100 < 30104 < 30108): re-priced to the new close − 8 ticks.
	at.mentorISBMarketableReModify(&nttrader.TCPTrader{}, p, 30104)
	if len(sent) != 1 || sent[0] != 30102.00 {
		t.Fatalf("in-band re-modify must send ONE modify to 30102.00; sent=%v", sent)
	}
	// Same close again → the resting TP is already there → no frame.
	at.mentorISBMarketableReModify(&nttrader.TCPTrader{}, p, 30104)
	if len(sent) != 1 {
		t.Fatalf("an unchanged close must not re-send; sent=%v", sent)
	}
	// Out of band (past +1R) → the native TP / +1R owns the exit → no frame.
	p.TPModWant = 30102.00
	at.mentorISBMarketableReModify(&nttrader.TCPTrader{}, p, 30112)
	if len(sent) != 1 {
		t.Fatalf("a past-+1R close must not re-modify; sent=%v", sent)
	}
	// A losing close → no frame.
	at.mentorISBMarketableReModify(&nttrader.TCPTrader{}, p, 30095)
	if len(sent) != 1 {
		t.Fatalf("a losing close must not re-modify; sent=%v", sent)
	}
	// No pending first modify → no frame (never re-sends a never-sent modify).
	p.TPModWant = 0
	at.mentorISBMarketableReModify(&nttrader.TCPTrader{}, p, 30105)
	if len(sent) != 1 {
		t.Fatalf("TPModWant 0 means the first modify never went out — nothing to re-modify; sent=%v", sent)
	}
}

func newDriveATForTest(t *testing.T) *AutoTrader {
	at, _ := newDriveAT(t)
	return at
}

// item 8 live-loop pin (R1): the ISB partial resolves ONCE at the fill candle's
// close. A close past +1R does NOTHING at the close — the +1R TP already filled
// inside the candle. bPos is ONE bracket (Wire 0); the market sell of a leg the
// wire does not carry is refused + counted. The split-wire market close is
// pinned in mentor_r22_exit_test.go.
// Rule 4 (release #31): the ISB candle-3 partial reads the frame the entry was
// read on. A 2m entry's fill candle IS the 2m candle — the partial reads ITS
// high, not the 1m's. Here the 2m high 105 crosses +1R (entry 100 + R 2) inside
// the fill candle, so the +1R TP already filled and NOTHING is sold at the close
// (#559: high >= +1R → 0 → isb_partial_no_sale). The 1m candle the same pass
// drives closes 101 IN PROFIT short of +1R — a 1m read would send a marketable
// modify. MUTANT: read the 1m c/h/l in the partial (drop trailC/trailH/trailL)
// → a modify is sent (modify_bracket_sent=1) → RED.
func TestMentorExitDrivePosISBPartialReadsThe2mFillCandle(t *testing.T) {
	at, _ := newDriveAT(t)
	var sent []float64
	old := modifyBracketWire
	modifyBracketWire = func(_ *nttrader.TCPTrader, _ string, _ string, _ int, tp float64) error {
		sent = append(sent, tp)
		return nil
	}
	t.Cleanup(func() { modifyBracketWire = old })

	p := bPos("B", "long", 100, 99, 104, 5) // R 2 (bPos), +1R = 102
	p.Pos.Origin = "ISB"
	p.Pos.ArmedBE = true
	p.BarsSinceFill = 1
	p.TrailTFMin = 2
	// split: leg 1 on its own wire + the runner (the marketable modify needs it).
	p.Pos.Leg1, p.Pos.Leg2, p.Pos.Leg1TP = 3, 2, 102
	p.Legs[0] = mentorLeg{SignalID: "entry", Qty: 3, TP: 102, Stop: 99, Final: false, Wire: 1}
	p.Legs[1] = mentorLeg{SignalID: "entry", Qty: 2, TP: 104, Stop: 99, Final: true, Wire: 2}
	// the 2m candle: high 105 crosses +1R (102) — the +1R already filled inside it.
	p.trailBar = market.Kline{OpenTime: 0, High: 105, Low: 99, Close: 104}
	p.trailBarOK = true
	at.mentorExitDrivePos(nil, p, 101, 101, 100, 0) // the 1m candle is in profit short of +1R
	if len(sent) != 0 {
		t.Fatalf("the 2m high crossed +1R inside the fill candle — nothing is sold at the close; sent=%v", sent)
	}
	if got := at.mentorCountSnapshot()["isb_partial_no_sale"]; got != 1 {
		t.Fatalf("isb_partial_no_sale = %d, want 1 (the 2m high crossed +1R)", got)
	}
	if got := at.mentorCountSnapshot()["modify_bracket_sent"]; got != 0 {
		t.Fatalf("modify_bracket_sent = %d, want 0 (no marketable sale when the +1R already filled)", got)
	}
}

// Rule 4 (release #31): the partial WAITS for the 2m fill candle to close — at
// BarsSinceFill 1 with the 2m still forming (trailBarOK false) it must not
// resolve on the 1m close; the next pass, once the 2m candle closed, it does.
// MUTANT: gate on BarsSinceFill==1 instead of trailBarOK → the first pass
// resolves on the 1m close (isb_partial_no_sale) → RED.
func TestMentorExitDrivePosISBPartialWaitsForThe2mClose(t *testing.T) {
	at, _ := newDriveAT(t)
	p := bPos("B", "long", 100, 99, 104, 5)
	p.Pos.Origin = "ISB"
	p.Pos.ArmedBE = true
	p.BarsSinceFill = 1
	p.TrailTFMin = 2
	p.trailBar = market.Kline{OpenTime: 0, High: 105, Low: 99, Close: 104}
	p.trailBarOK = false // the 2m fill candle has not closed yet
	at.mentorExitDrivePos(nil, p, 98, 99, 95, 0)
	if p.ISBPartialDone {
		t.Fatal("the partial must NOT resolve before the 2m fill candle closes")
	}
	if got := at.mentorCountSnapshot()["isb_partial_no_sale"]; got != 0 {
		t.Fatalf("isb_partial_no_sale = %d, want 0 (still waiting for the 2m close)", got)
	}
	// The next pass: the 2m candle has closed.
	p.BarsSinceFill = 2
	p.trailBarOK = true
	at.mentorExitDrivePos(nil, p, 98, 99, 95, 0)
	if !p.ISBPartialDone {
		t.Fatal("ISBPartialDone must be set once the 2m candle closes")
	}
	if got := at.mentorCountSnapshot()["isb_partial_no_sale"]; got != 1 {
		t.Fatalf("isb_partial_no_sale = %d, want 1 (the 2m high crossed +1R: already filled, nothing sold)", got)
	}
}

// TestMentorExitDrivePosISBPartial1mNotOnALaterCandle — D3 (fold #565): dev's
// BarsSinceFill==1 gate stays for a 1m entry. When the first drive pass is at
// BarsSinceFill==2 (bar 1 skipped by the flat-read continue), the partial is
// LATCHED done but never resolved on the candle AFTER the fill candle — no
// marketable modify is sent. MUTANT M565-4 (resolve at any BarsSinceFill for a
// 1m entry) → the later candle's close (101, in profit short of +1R) sends a
// modify (modify_bracket_sent=1) → RED.
func TestMentorExitDrivePosISBPartial1mNotOnALaterCandle(t *testing.T) {
	at, _ := newDriveAT(t)
	var sent []float64
	old := modifyBracketWire
	modifyBracketWire = func(_ *nttrader.TCPTrader, _ string, _ string, _ int, tp float64) error {
		sent = append(sent, tp)
		return nil
	}
	t.Cleanup(func() { modifyBracketWire = old })

	p := bPos("B", "long", 100, 99, 104, 5) // R 2 (bPos), +1R = 102
	p.Pos.Origin = "ISB"
	p.Pos.ArmedBE = true
	p.BarsSinceFill = 2 // bar 1 was skipped (flat read) — NOT the fill candle
	// split: leg 1 on its own wire + the runner (the marketable modify needs it).
	p.Pos.Leg1, p.Pos.Leg2, p.Pos.Leg1TP = 3, 2, 102
	p.Legs[0] = mentorLeg{SignalID: "entry", Qty: 3, TP: 102, Stop: 99, Final: false, Wire: 1}
	p.Legs[1] = mentorLeg{SignalID: "entry", Qty: 2, TP: 104, Stop: 99, Final: true, Wire: 2}
	// a candle whose close is in profit short of +1R (high 101 < +1R 102) — but it
	// is the candle AFTER the fill candle, so a late resolve would sell.
	at.mentorExitDrivePos(nil, p, 101, 101, 100, 0)
	if !p.ISBPartialDone {
		t.Fatal("dev latches ISBPartialDone on a skipped fill candle")
	}
	if len(sent) != 0 {
		t.Fatalf("a 1m entry must NOT resolve the partial on a later candle; sent=%v", sent)
	}
	if got := at.mentorCountSnapshot()["modify_bracket_sent"]; got != 0 {
		t.Fatalf("modify_bracket_sent = %d, want 0 (no late partial)", got)
	}
	if p.Legs[0].TP != 102 {
		t.Fatalf("leg1 TP must stay 102 (the resting +1R; no late partial); got %.2f", p.Legs[0].TP)
	}
}

// TestMentorExitDrivePosISBPartialReadsThe2mClose — M565-5: the 2m partial
// reads the 2m CLOSE, not the 1m's. A 2m entry whose 2m high stays under +1R
// but whose 2m CLOSE is in profit short of +1R must modify leg 1's TP to the 2m
// close (clamped to entry + 1 tick — the #559 no-loss floor). Dropping trailC
// (reading the 1m close 98, a losing close) → nothing sent → RED.
func TestMentorExitDrivePosISBPartialReadsThe2mClose(t *testing.T) {
	at, _ := newDriveAT(t)
	var sent []float64
	old := modifyBracketWire
	modifyBracketWire = func(_ *nttrader.TCPTrader, _ string, _ string, _ int, tp float64) error {
		sent = append(sent, tp)
		return nil
	}
	t.Cleanup(func() { modifyBracketWire = old })

	p := bPos("B", "long", 100, 99, 104, 5) // R 2 (bPos), +1R = 102
	p.Pos.Origin = "ISB"
	p.Pos.ArmedBE = true
	p.BarsSinceFill = 1
	p.TrailTFMin = 2
	// split: leg 1 on its own wire + the runner (the marketable modify needs it).
	p.Pos.Leg1, p.Pos.Leg2, p.Pos.Leg1TP = 3, 2, 102
	p.Legs[0] = mentorLeg{SignalID: "entry", Qty: 3, TP: 102, Stop: 99, Final: false, Wire: 1}
	p.Legs[1] = mentorLeg{SignalID: "entry", Qty: 2, TP: 104, Stop: 99, Final: true, Wire: 2}
	// the 2m candle: high 101 (< +1R 102), close 100.8 in profit short of +1R.
	p.trailBar = market.Kline{OpenTime: 0, High: 101, Low: 99.5, Close: 100.8}
	p.trailBarOK = true
	at.mentorExitDrivePos(nil, p, 98, 99, 95, 0) // the 1m candle is a LOSING close
	if len(sent) != 1 || sent[0] != 100.25 {
		t.Fatalf("the partial must modify to the 2m close (clamped to entry+1 tick 100.25), not the 1m losing close; sent=%v", sent)
	}
	if got := at.mentorCountSnapshot()["modify_bracket_sent"]; got != 1 {
		t.Fatalf("modify_bracket_sent = %d, want 1", got)
	}
}

// TestMentorExitDriveCandleBEWaitsForThe2mFrame — D8 (fold #565): a 2m entry's
// break-even reads its own frame. While the 2m candle is still forming
// (frameOK=false) a 1m close past break-even must NOT arm BE; once the 2m candle
// closes past break-even it does. MUTANT (drop the frameOK gate / read the 1m
// close) → BE arms on the half-formed 2m candle → RED.
func TestMentorExitDriveCandleBEWaitsForThe2mFrame(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 100, 98, 104, 2)
	p.Pos.Leg1, p.Pos.Leg2, p.Pos.Leg1TP = 1, 1, 101
	p.Legs[0] = mentorLeg{SignalID: "entry", Qty: 1, TP: 101, Stop: 98, Final: false, Wire: 1}
	p.Legs[1] = mentorLeg{SignalID: "entry", Qty: 1, TP: 104, Stop: 98, Final: true, Wire: 2}
	p.TrailTFMin = 2

	// The 2m candle is still forming (frameOK=false): a 1m close 101 past BE
	// must NOT arm BE.
	at.mentorExitDriveCandle(nil, p, 101, 102, 100, true, false, true, false, [3]bool{})
	if p.Pos.ArmedBE {
		t.Fatal("a 2m entry's BE must not arm on a 1m close while its 2m candle is still forming")
	}
	if got := moves(); len(got) != 0 {
		t.Fatalf("no BE move while the 2m candle forms; moves=%+v", got)
	}

	// The 2m candle closes past break-even (close 101): BE arms.
	at.mentorExitDriveCandle(nil, p, 101, 102, 100, true, false, true, true, [3]bool{})
	if !p.Pos.ArmedBE {
		t.Fatal("BE must arm once the 2m candle closes past break-even")
	}
	assertMoves(t, moves(), driveMove{"entry", "long", 100})
}

// TestMentorBEBeforePartialWaitsForThe2mFrameShort — live 10-20 20:42 CT
// (cf95ee3d, ISB short 29938, 2m frame): stage 0's BE-before-partial read the
// still-forming 2m trail bar's 0 close, "past half-way" for any short, and sent
// a BE stop NT8 refused below the market. A 2m short with its 2m candle still
// forming must move nothing, and a 0 close must never arm BE; once the 2m
// candle closes past half-way, BE arms. MUTANTS: drop the stage-0 frameOK gate
// → the first assertion RED; drop the c<=0 guard → the second RED.
func TestMentorBEBeforePartialWaitsForThe2mFrameShort(t *testing.T) {
	at, moves := newDriveAT(t)
	// short entry 100, stop 102 (R 2), half-way to +1R = 99.
	p := bPos("ISB", "short", 100, 102, 96, 2)
	p.Pos.Leg1, p.Pos.Leg2, p.Pos.Leg1TP = 1, 1, 98
	p.Legs[0] = mentorLeg{SignalID: "entry", Qty: 1, TP: 98, Stop: 102, Final: false, Wire: 1}
	p.Legs[1] = mentorLeg{SignalID: "entry", Qty: 1, TP: 96, Stop: 102, Final: true, Wire: 2}
	p.TrailTFMin = 2

	// The 2m candle is still forming (frameOK=false): even a close past
	// half-way (a stale/half-formed value) must not arm BE.
	at.mentorExitDriveCandle(nil, p, 98.75, 99.5, 98.5, false, false, true, false, [3]bool{})
	if p.Pos.ArmedBE {
		t.Fatal("a 2m short must not arm BE while its 2m candle is still forming")
	}
	if got := moves(); len(got) != 0 {
		t.Fatalf("no BE move while the 2m candle forms; moves=%+v", got)
	}

	// A 0 close with frameOK set must still never arm BE.
	at.mentorExitDriveCandle(nil, p, 0, 0, 0, false, false, true, true, [3]bool{})
	if p.Pos.ArmedBE {
		t.Fatal("a 0 close is not a close: it must never arm BE")
	}
	if got := moves(); len(got) != 0 {
		t.Fatalf("no BE move on a 0 close; moves=%+v", got)
	}

	// The 2m candle closes past half-way (98.75 <= 99): BE arms, both legs.
	at.mentorExitDriveCandle(nil, p, 98.75, 99.5, 98.5, false, false, true, true, [3]bool{})
	if !p.Pos.ArmedBE {
		t.Fatal("BE must arm once the 2m candle closes past half-way")
	}
	assertMoves(t, moves(), driveMove{"entry", "short", 100}, driveMove{"entry", "short", 100})
}

// item 8 live-loop pin: the ISB partial is resolved ONCE at the fill candle's
// close (BarsSinceFill == 1), logged once, and never re-fires on later candles.
// item 8 live-loop pin: the ISB partial is resolved ONCE at the fill candle's
// close (BarsSinceFill == 1), logged once, and never re-fires on later candles.
func TestMentorExitDrivePosISB_PartialResolvedOnce(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 10, 13, 5) // stop already at BE
	p.Pos.Origin = "ISB"
	p.Pos.ArmedBE = true
	p.BarsSinceFill = 1 // the fill candle just closed
	// fill candle high 12 = +1R (entry 10 + R 2): the +1R TP already filled
	// inside the candle — nothing to do at the close.
	at.mentorExitDrivePos(nil, p, 11.0, 12.0, 10.8, 0)
	if got := at.mentorCountSnapshot()["isb_partial_no_sale"]; got != 1 {
		t.Fatalf("isb_partial_no_sale = %d, want 1 (a past-+1R close sells nothing at the close)", got)
	}
	if got := at.mentorCountSnapshot()["modify_bracket_sent"]; got != 0 {
		t.Fatalf("a past-+1R close must send no modify, modify_bracket_sent = %d", got)
	}
	assertMoves(t, moves())
	// a later candle must NOT re-fire the ISB partial.
	p.BarsSinceFill = 2
	at.mentorExitDrivePos(nil, p, 12.5, 13, 12, 0)
	if got := mentorISBPartialResolutions(at); got != 1 {
		t.Fatalf("ISB partial resolutions = %d after a later candle, want 1 (no re-fire)", got)
	}
}

// B BE on the single leg: the stop moves to entry once price covers half the
// distance to the trade target.
func TestMentorExitDrivePosB_ArmsBE(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 9, 13, 5)               // half = (13−10)/2 = 1.5 → BE at 11.5
	at.mentorExitDrivePos(nil, p, 11.0, 11.8, 10.8, 0) // high 11.8 arms it
	assertMoves(t, moves(), driveMove{"entry", "long", 10})
	if !p.Pos.ArmedBE {
		t.Fatalf("ArmedBE = false, want true")
	}
}

// The single leg trails only AFTER the 1:1 point (entry + R) printed on a
// PRIOR candle (CTO gate, release #4: the course trails the runner after the
// 1:1 point), then takes the TIGHTER of the live 1:1 and the candle trail, the
// trail sitting one tick BEYOND the candle's low (m283 P2 — the same rule as
// mentorCandleTrailStop [X8 part1 @16:18–16:31; D2.4 p1 @08:11–08:24]).
// Mutants: trail straight after BE (candle 1 moves to 11.75) → RED; trail AT
// the low (13.5) → RED.
func TestMentorExitDrivePosB_SingleLegTrails(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 10, 16, 5) // R = 2 → the 1:1 point is 12
	p.Pos.ArmedBE = true
	// Candle 1 prints the 1:1 point (high 13 ≥ 12): no trail yet; the live
	// 1:1 (2·12.5−16 = 9) is below the BE stop → no move.
	at.mentorExitDrivePos(nil, p, 12.5, 13, 12, 0)
	assertMoves(t, moves())
	if !p.Pos.Scaled {
		t.Fatal("the 1:1 point printed — Scaled must be set")
	}
	// Candle 2: 1:1 = 2·14−16 = 12; trail = low 13.5 − 1 tick = 13.25.
	at.mentorExitDrivePos(nil, p, 14, 14.5, 13.5, 0)
	assertMoves(t, moves(), driveMove{"entry", "long", 13.25})
}

// e2e pin (CTO 2026-10-04): ONE filled row registers the single-leg live
// position, and the next closed candle at half the distance to the trade target
// arms BE. The wiring is the point: fill callback → registerMentorLivePos →
// mentorLivePositions → exit-drive loop.
func TestMentorExitDriveE2E_SingleRowFillRegistersAndDrives(t *testing.T) {
	at, moves := newDriveAT(t)
	at.mentorExitModes = map[string]string{"long": "B"}
	row := store.ArmedOrderDB{ID: 1, SignalID: "sig-1", Side: "long", StopPx: 9, TargetPx: 13, FillQuantity: 5, Condition: "PHL"}
	u := ntwire.OrderUpdatePayload{State: "filled", SignalID: "sig-1", Account: "Sim101", FillPrice: 10, Quantity: 5}
	at.registerMentorLivePos(row, u)

	lp, ok := at.mentorLivePos["sig-1"]
	if !ok || lp == nil {
		t.Fatalf("live position not registered under the entry signal id")
	}
	if lp.Legs[0].Qty != 5 || !lp.Legs[0].Final || lp.Legs[1].SignalID != "" {
		t.Fatalf("single leg = whole position (Final runner, Legs[1] empty): %+v", lp.Legs)
	}
	at.mentorExitDrivePos(nil, lp, 11.0, 11.8, 10.8, 0)
	assertMoves(t, moves(), driveMove{"sig-1", "long", 10})
	if !lp.Pos.ArmedBE {
		t.Fatalf("ArmedBE = false, want true")
	}
}

// Mode C (confluence): the stop NEVER moves [D4.2 p1 @14:57].
func TestMentorExitDrivePosC_NeverMoves(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("C", "long", 10, 9, 16, 5)
	at.mentorExitDrivePos(nil, p, 15, 16, 14, 0)
	assertMoves(t, moves())
}

// Mode A (resonance): no trail, no 1:1 tightening [D2.4 p1 @02:17–02:29].
func TestMentorExitDrivePosA_NoMoves(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("A-resonance", "long", 10, 10, 16, 5)
	p.Pos.ArmedBE = true
	at.mentorExitDrivePos(nil, p, 15, 16, 14, 0)
	assertMoves(t, moves())
}

// SWING: no moves — the swing rules own it.
func TestMentorExitDrivePosSwing_NoMoves(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("swing", "long", 10, 9, 0, 5)
	at.mentorExitDrivePos(nil, p, 15, 16, 14, 0)
	assertMoves(t, moves())
}

// The loop never acts on a FORMING bar.
func TestMentorExitDriveRefusesFormingBar(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 9, 13, 5)
	p.FillBarOpen = 0
	at.mentorRegisterLivePos("sig-1", p)
	bars := []market.Kline{
		{OpenTime: 1, Close: 10, High: 10.5, Low: 9.5, Final: true},
		{OpenTime: 2, Close: 12, High: 12.5, Low: 11, Final: false}, // forming: would arm BE if driven
	}
	at.mentorExitDrive(bars)
	assertMoves(t, moves())
}

// The never-widen guard refuses a widening leg move before the wire.
func TestMentorMoveLegStop_NeverWidens(t *testing.T) {
	at, moves := newDriveAT(t)
	err := at.mentorMoveLegStop(nil, "long", &mentorLeg{SignalID: "sig-1", Stop: 12}, 10)
	if err == nil || !strings.Contains(err.Error(), "widen") {
		t.Fatalf("widen move not refused: %v", err)
	}
	assertMoves(t, moves())
}

// A same-side ISB within 3 candles of a PHL/PLH fill arms mode A, but m134 (b)
// /(c): arming does NOT move any stop and does NOT write the ArmedBE belief —
// the runner's break-even is deferred to the closed-1m exit drive. Release #25
// (CX-3): the runner-TP move is SENT on the runner's own wire leg
// (TestCX3ResonanceRunnerTPIsSentOnTheRunnerLeg); a SINGLE bracket has no
// separate runner, so nothing is sent and it is counted (never a modify of the
// whole position past the 1:1 partial).
func TestMentorArmResonanceOnISB(t *testing.T) {
	at, moves := newDriveAT(t)
	// m186 W-MENTOR-EXACT: the runner TP default is OFF (the mentor never places
	// a runner TP); pin the not_split branch by enabling it explicitly.
	at.config.StrategyConfig.RiskControl.MentorResonanceRunnerTPOldHigh = cxBool(true)
	p := bPos("B", "long", 10, 9, 16, 5)
	p.Pos.Origin = "PHL"
	p.BarsSinceFill = 1
	p.RunnerTarget = 20
	at.mentorRegisterLivePos("sig-1", p)
	at.mentorArmResonanceOnISB("long")
	if p.Pos.Mode != "A-resonance" || p.Pos.ArmedBE || p.Pos.Stop != 9 {
		t.Fatalf("resonance arm: mode=%s armed=%v stop=%.2f — want A-resonance, NOT armed, stop still 9", p.Pos.Mode, p.Pos.ArmedBE, p.Pos.Stop)
	}
	assertMoves(t, moves())
	snap := at.mentorCountSnapshot()
	if snap["resonance_armed"] != 1 || snap["resonance_runner_tp_not_split"] != 1 {
		t.Fatalf("resonance counters: armed=%d runner_tp_not_split=%d, want 1/1", snap["resonance_armed"], snap["resonance_runner_tp_not_split"])
	}
}

// m134 (b)/(d): the runner's break-even goes out on the CLOSED 1m candle once
// price is on the profit side of break-even — even before the 1:1 partial is
// confirmed. A candle NOT on the profit side (and no confirmed partial) moves
// nothing.
func TestMentorResonanceBEDeferredToClosedCandle(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 9, 16, 5)
	p.Pos.Origin = "PHL"
	p.Pos.Mode = "A-resonance"
	p.BarsSinceFill = 1
	at.mentorRegisterLivePos("sig-1", p)
	// Not on the profit side, partial not confirmed → nothing moves (wait).
	at.mentorExitDrivePos(nil, p, 10.0, 10.5, 9.5, 0)
	assertMoves(t, moves())
	// On the profit side (close ≥ BE + tick = 10.25) → the runner's stop → BE.
	at.mentorExitDrivePos(nil, p, 10.5, 11.0, 10.0, 0)
	assertMoves(t, moves(), driveMove{"entry", "long", 10})
	if !p.Pos.ArmedBE || p.Pos.Stop != 10 {
		t.Fatalf("BE after the profit-side close: armed=%v stop=%.2f — want armed, stop 10", p.Pos.ArmedBE, p.Pos.Stop)
	}
	snap := at.mentorCountSnapshot()
	if snap["resonance_be_after_partial_or_profit"] != 1 {
		t.Fatalf("resonance_be_after_partial_or_profit = %d, want 1", snap["resonance_be_after_partial_or_profit"])
	}
}

// m134 (b): once the 1:1 partial is CONFIRMED (Scaled), the runner's break-even
// goes out on the next closed candle even when the close is only just past BE.
func TestMentorResonanceBEAfterPartialConfirmed(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 9, 16, 5)
	p.Pos.Origin = "PHL"
	p.Pos.Mode = "A-resonance"
	p.Pos.Scaled = true // leg 1's +1R partial confirmed filled at NT8
	p.BarsSinceFill = 1
	at.mentorRegisterLivePos("sig-1", p)
	at.mentorExitDrivePos(nil, p, 10.0, 10.5, 9.5, 0)
	assertMoves(t, moves(), driveMove{"entry", "long", 10})
	if !p.Pos.ArmedBE || p.Pos.Stop != 10 {
		t.Fatalf("BE after the confirmed partial: armed=%v stop=%.2f — want armed, stop 10", p.Pos.ArmedBE, p.Pos.Stop)
	}
}

// m134 (c): a FAILED BE move never writes the ArmedBE/Stop belief (the leg
// belief stays too) — BE is re-tried on the next closed candle.
func TestMentorResonanceBEFailedMoveDoesNotWriteBelief(t *testing.T) {
	at, _ := newDriveAT(t)
	p := bPos("B", "long", 10, 9, 16, 5)
	p.Pos.Origin = "PHL"
	p.Pos.Mode = "A-resonance"
	p.BarsSinceFill = 1
	at.mentorRegisterLivePos("sig-1", p)
	old := mentorMoveStopForSignalWire
	mentorMoveStopForSignalWire = func(_ *nttrader.TCPTrader, _, _ string, _ float64, _ int) error {
		return fmt.Errorf("broker refused")
	}
	t.Cleanup(func() { mentorMoveStopForSignalWire = old })
	// on the profit side → the BE move is attempted but NOT applied.
	at.mentorExitDrivePos(nil, p, 10.5, 11.0, 10.0, 0)
	if p.Pos.ArmedBE || p.Pos.Stop != 9 || p.Legs[0].Stop != 9 {
		t.Fatalf("a failed BE move must not write the belief: armed=%v stop=%.2f legStop=%.2f", p.Pos.ArmedBE, p.Pos.Stop, p.Legs[0].Stop)
	}
}

// m151: the resonance arm lives at the never-add gate in mentorPlaceIntent, NOT
// at the ledger-accept point (mentorArmIntent). A direct accept (the m134 shape)
// must no longer flip the open position — the arm moved.
func TestMentorArmResonanceFromAcceptedISBOnly(t *testing.T) {
	at, st, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)
	defer st.Close()
	// an open PHL long position filled one closed candle ago.
	at.mentorRegisterLivePos("sig-phl", &mentorLivePos{
		Pos:           mentorPosition{Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 100, Stop: 90, Target: 130, R: 10, Mode: "B"},
		Legs:          [3]mentorLeg{{SignalID: "sig-phl", Qty: 1, Stop: 90, Final: true}},
		BarsSinceFill: 1,
	})
	// the ledger-accept point no longer arms resonance.
	at.mentorArmIntent(
		mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-res", Side: mentor.SideLong,
			Price: 101, Stop: 100, Target: 112, Setup: "ISB", ExpiryMs: 1_800_000_000_000},
		mentorSizeChoice{Contracts: 1, Tier: "base"}, 1_800_000_000_000, 1_800_000_000_000, "B", 0)
	lp := at.mentorLivePos["sig-phl"]
	if lp == nil || lp.Pos.Mode != "B" {
		t.Fatalf("the accept point must NOT arm resonance (it moved to the add gate): %+v", lp)
	}
}

// m134 (a) NEGATIVE pin at the production call site (mentorEvalOnce): a raw ISB
// entry intent the ledger REFUSES must NOT arm resonance and must NOT request a
// stop move. The mutant (DS-102 re-adding the raw-intent loop in mentorEvalOnce)
// arms resonance from the raw intent BEFORE the dispatch refuses it → Mode flips
// to A → this pin goes RED.
func TestMentorEvalOnceRawRefusedISBDoesNotArmResonance(t *testing.T) {
	at, st, _, _ := mentorB3Rig(t)
	defer st.Close()
	oldBarsProvider := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(_, _ string, _ int) []market.Kline { return nil }
	t.Cleanup(func() { market.FuturesBarsProvider = oldBarsProvider })
	// an open PHL long position filled one closed candle ago (would resonate).
	at.mentorRegisterLivePos("sig-phl", &mentorLivePos{
		Pos:           mentorPosition{Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 100, Stop: 90, Target: 130, R: 10, Mode: "B"},
		Legs:          [3]mentorLeg{{SignalID: "sig-phl", Qty: 1, Stop: 90, Final: true}},
		BarsSinceFill: 1,
	})
	// capture any stop move the production tick requests.
	oldBroker := mentorBrokerLegStopRead
	mentorBrokerLegStopRead = func(_ *AutoTrader, _ *nttrader.TCPTrader, _ string, leg *mentorLeg) (float64, int) {
		return leg.Stop, mentorBrokerStopLive
	}
	t.Cleanup(func() { mentorBrokerLegStopRead = oldBroker })
	var mu sync.Mutex
	moves := 0
	oldMove := mentorMoveStopForSignalWire
	mentorMoveStopForSignalWire = func(_ *nttrader.TCPTrader, _, _ string, _ float64, _ int) error {
		mu.Lock()
		moves++
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { mentorMoveStopForSignalWire = oldMove })
	// a raw ISB entry intent the ledger REFUSES: long with the target BELOW the
	// entry → mentorRuleGate refuses "bad geometry" before any placement — it
	// never reaches the accept point in mentorArmIntent.
	at.mentorEvalIntentsSeam = func([]market.Kline) []mentor.Intent {
		return []mentor.Intent{{Action: mentor.PlaceStopLimitEntry, Setup: "ISB", Side: mentor.SideLong,
			Price: 101, Stop: 100, Target: 99}}
	}
	t.Cleanup(func() { at.mentorEvalIntentsSeam = nil })
	bars := []market.Kline{
		{OpenTime: 1_000_000, CloseTime: 1_000_059, Open: 100, High: 101, Low: 99, Close: 100.5},
		{OpenTime: 1_000_060, CloseTime: 1_000_119, Open: 100.5, High: 102, Low: 100, Close: 101},
	}
	at.mentorEvalOnce(bars)
	lp := at.mentorLivePos["sig-phl"]
	if lp == nil || lp.Pos.Mode != "B" {
		t.Fatalf("a REFUSED raw ISB must NOT arm resonance: mode=%s", func() string {
			if lp == nil {
				return "<nil>"
			}
			return lp.Pos.Mode
		}())
	}
	mu.Lock()
	n := moves
	mu.Unlock()
	if n != 0 {
		t.Fatalf("a REFUSED raw ISB must not request a stop move: %d", n)
	}
}

// The resonance trigger is an ISB entry intent (not a cancel/close action).
func TestIsISBEntryIntent(t *testing.T) {
	if !isISBEntryIntent(mentor.Intent{Setup: "ISB", Action: mentor.PlaceStopEntry}) {
		t.Fatal("ISB stop-entry is a resonance trigger")
	}
	if !isISBEntryIntent(mentor.Intent{Setup: "ISB", Action: mentor.PlaceStopLimitEntry}) {
		t.Fatal("ISB stop-limit-entry is a resonance trigger")
	}
	if isISBEntryIntent(mentor.Intent{Setup: "PHL", Action: mentor.PlaceStopEntry}) {
		t.Fatal("a PHL entry is not the ISB trigger")
	}
	if isISBEntryIntent(mentor.Intent{Setup: "ISB", Action: mentor.CancelArm}) {
		t.Fatal("a cancel is not the ISB trigger")
	}
}

// CTO gate (release #4): the PRODUCTION loop drops a position once the account
// reads flat on its side for two consecutive closed candles after the fill —
// nothing else ever unregistered it. A read error keeps it; an open side keeps
// it. Mutant: never unregister → RED.
func TestMentorExitDriveDropsAFlatPosition(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 9, 13, 1)
	at.mentorRegisterLivePos("entry", p)
	sides := map[string]bool{"long": true}
	readOK := true
	old := mentorDriveOpenSides
	mentorDriveOpenSides = func(*AutoTrader) (map[string]bool, bool) { return sides, readOK }
	t.Cleanup(func() { mentorDriveOpenSides = old })
	// The I7 qty seam must be substituted too — the zero-value TCPTrader has no
	// server, so the production reader would panic on GetPositions.
	oldQty := mentorDriveOpenQty
	mentorDriveOpenQty = func(*AutoTrader) (map[string]float64, bool) { return map[string]float64{"long": 5}, readOK }
	t.Cleanup(func() { mentorDriveOpenQty = oldQty })
	bar := func(i int) []market.Kline {
		return []market.Kline{{OpenTime: int64(i) * 60_000, Close: 10.2, High: 10.3, Low: 10.1, Final: true}}
	}
	at.mentorExitDrive(bar(1)) // open → kept
	if len(at.mentorLivePosList()) != 1 {
		t.Fatal("an open position must stay in the loop")
	}
	sides = map[string]bool{}
	readOK = false
	at.mentorExitDrive(bar(2)) // read error proves nothing → kept
	at.mentorExitDrive(bar(3))
	if len(at.mentorLivePosList()) != 1 {
		t.Fatal("a failed position read must never drop the position")
	}
	readOK = true
	at.mentorExitDrive(bar(4)) // flat read 1
	if len(at.mentorLivePosList()) != 1 {
		t.Fatal("one flat read is not enough (the fill may not be in the snapshot yet)")
	}
	at.mentorExitDrive(bar(5)) // flat read 2 → dropped
	if n := len(at.mentorLivePosList()); n != 0 {
		t.Fatalf("a position flat for two candles must leave the loop; %d left", n)
	}
	_ = moves
}

// CTO gate (release #4, canon 53): a FULL fill of a mentor-origin row, through
// the PRODUCTION order_update handler onArmedOrderUpdate, registers the live
// position the exit drive manages. Every other exit-drive pin registers by
// hand; without this one, dropping the fill-callback registration survived.
func TestMentorFullFillRegistersTheLivePositionAtTheFillHandler(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "lvl-3-0",
		Side: "long", EntryPx: 29600, StopPx: 29590, TargetPx: 29630, Kind: "stop_entry", Condition: "PHL",
		State: store.StateWorking, SignalID: "sig-fill-1", Origin: store.ArmOriginMentor, Contracts: store.IntPtr(2)}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: "sig-fill-1", State: "filled", Quantity: 2, FillPrice: 29600, Account: "Sim101"}, ledger)
	got := at.mentorLivePosList()
	if len(got) != 1 {
		t.Fatalf("a full mentor fill must register ONE live position, got %d", len(got))
	}
	if got[0].Legs[0].SignalID != "sig-fill-1" || got[0].Legs[0].Qty != 2 || got[0].Pos.Entry != 29600 {
		t.Fatalf("registered leg = %+v entry %.2f, want sig-fill-1 ×2 @29600", got[0].Legs[0], got[0].Pos.Entry)
	}
}

// CTO pin (#390 gate): the ISB partial resolves on the FILL candle's close
// ONLY (candle 3). Later candles never re-fire it, even when leg 1 has not
// scaled. Mutant: drop the BarsSinceFill == 1 gate → a second log → RED.
func TestMentorExitDrivePosISB_PartialOnlyOnTheFillCandle(t *testing.T) {
	at, _ := newDriveAT(t)
	p := bPos("B", "long", 10, 8, 20, 4) // R = 2 (bPos fixes R at 2): +1R = 12
	p.Pos.Origin = "ISB"
	p.BarsSinceFill = 1
	at.mentorExitDrivePos(nil, p, 11.0, 11.5, 10.5, 0) // fill candle closes in profit below +1R
	if got := mentorISBPartialResolutions(at); got != 1 {
		t.Fatalf("the fill-candle close must resolve the partial once, got %d", got)
	}
	// bPos is ONE bracket (Wire 0): a TP modify would close the WHOLE position,
	// so the candle-3 partial is refused + counted, never sent (release #22).
	if got := at.mentorCountSnapshot()["isb_partial_not_split"]; got != 1 {
		t.Fatalf("a single-bracket ISB partial must be refused as not split, got %d", got)
	}
	p.BarsSinceFill = 2
	at.mentorExitDrivePos(nil, p, 11.4, 11.8, 11.1, 0) // still below +1R, still in profit
	if got := mentorISBPartialResolutions(at); got != 1 {
		t.Fatalf("a later candle must not re-fire the ISB partial, got %d resolutions", got)
	}
}

// mentorISBPartialResolutions sums every ISB candle-3 resolution counter — one
// resolution per position, whichever branch it took.
func mentorISBPartialResolutions(at *AutoTrader) int {
	snap := at.mentorCountSnapshot()
	n := 0
	for _, k := range []string{"isb_partial_no_sale", "isb_partial_wait_1r", "isb_partial_not_split",
		"isb_partial_not_in_profit", "modify_bracket_unwired", "modify_bracket_failed",
		"modify_bracket_sent"} {
		n += int(snap[k])
	}
	return n
}

// UR-FIX U3: splitBySignal + stopBySignal must be dropped on the FULL close
// (both legs flat) — the exit-drive flat-unregister is the call site — and must
// SURVIVE a leg-1-only close (the runner still needs SplitSentFor and
// MoveStopForSignalLeg). Mutant: delete on the leg-1 scale → the runner's split
// record vanishes mid-trade → RED; mutant: never delete → both maps never clear
// → RED.
func TestMentorExitDriveForgetsSignalMapsOnFullCloseOnly(t *testing.T) {
	at, _ := newDriveAT(t)
	nt := at.trader.(*nttrader.TCPTrader)
	sid := "entry"
	nt.SeedSignalMapsForTest(sid, nttrader.SentSplit{Leg1Qty: 2, Leg1TP: 11},
		map[string]float64{sid: 9, sid + "#leg1": 9, sid + "#leg2": 9})

	// A SPLIT long: leg 1 TP at 11 (1:1), the runner to 14. Both legs carry the
	// one entry signal id.
	p := &mentorLivePos{
		Pos: mentorPosition{
			Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 10, Stop: 9,
			Target: 14, R: 1, Mode: "B", Leg1: 2, Leg2: 2, Leg1TP: 11,
		},
		Legs: [3]mentorLeg{
			{SignalID: sid, Qty: 2, TP: 11, Stop: 9, Final: false, Wire: 1},
			{SignalID: sid, Qty: 2, TP: 14, Stop: 9, Final: true, Wire: 2},
		},
	}
	at.mentorRegisterLivePos(sid, p)

	// Leg-1-only close: since B2/I6 a SPLIT position's leg 1 is scaled only by
	// the broker's leg-1 TP confirmation (never a candle guess) — latch it the
	// way the receipt does; the runner stays open.
	if !at.mentorLatchLeg1Scaled(sid, 1) {
		t.Fatal("the leg-1 TP receipt must scale leg 1")
	}
	if !p.Pos.Scaled {
		t.Fatal("the leg-1 TP receipt must scale leg 1")
	}
	if _, ok := nt.SplitSentFor(sid); !ok {
		t.Fatal("a leg-1-only close must NOT drop the split record (the runner still needs it)")
	}

	// Full close: the flat drive unregisters after two consecutive flat reads.
	sides := map[string]bool{}
	old := mentorDriveOpenSides
	mentorDriveOpenSides = func(*AutoTrader) (map[string]bool, bool) { return sides, true }
	t.Cleanup(func() { mentorDriveOpenSides = old })
	bar := func(i int) []market.Kline {
		return []market.Kline{{OpenTime: int64(i) * 60_000, Close: 10.2, High: 10.3, Low: 10.1, Final: true}}
	}
	p.BarsSinceFill = 2
	at.mentorExitDrive(bar(1)) // flat read 1 — still held
	if n := len(at.mentorLivePosList()); n != 1 {
		t.Fatalf("one flat read must not drop the position; %d left", n)
	}
	at.mentorExitDrive(bar(2)) // flat read 2 → dropped + maps forgotten
	if n := len(at.mentorLivePosList()); n != 0 {
		t.Fatalf("a flat position must leave the loop; %d left", n)
	}
	if _, ok := nt.SplitSentFor(sid); ok {
		t.Fatal("the full close must drop the split record")
	}
	splits, stops := nt.SignalMapsForTest()
	if _, ok := splits[sid]; ok {
		t.Fatal("the full close must clear splitBySignal")
	}
	for _, k := range []string{sid, sid + "#leg1", sid + "#leg2"} {
		if _, ok := stops[k]; ok {
			t.Fatalf("the full close must clear stopBySignal[%s]", k)
		}
	}
}

// TestMentorX8TrapDodgeExit pins the X8 trap-dodge exit at the production hook
// (m186 DS-108, [X8 part1 @07:31–08:07]): a LONG with the 5m trigger on the
// SELL side and a CLOSED 2m candle ABOVE the EMA34 is a fake breakout → a market
// close is sent through mentorCloseLegAtMarketWire. MUTANT: drop the
// mentor.X8TrapDodge call in mentorX8TrapDodgeExit → no close → RED.
func TestMentorX8TrapDodgeExit(t *testing.T) {
	at, _ := newDriveAT(t)
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	e := mentor.New(cfg)
	e.State.EMA34 = 100
	e.State.Trigger = mentor.TriggerLine{Dir: mentor.SideShort, Price: 99}
	at.mentorEval = e

	p := bPos("B", "long", 100, 98, 104, 1)

	var mu sync.Mutex
	var closes []string
	old := mentorCloseLegAtMarketWire
	mentorCloseLegAtMarketWire = func(_ *AutoTrader, _ *nttrader.TCPTrader, side string, qty int) error {
		mu.Lock()
		closes = append(closes, side)
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { mentorCloseLegAtMarketWire = old })

	// Two 1m bars in ONE 2m bucket: the second bar closes the 2m candle at 101.5.
	base := int64(120_000_000_000) // 2m-aligned
	bars := []market.Kline{
		{OpenTime: base, CloseTime: base + 59_999, Open: 100, High: 102, Low: 99, Close: 101, Final: true},
		{OpenTime: base + 60_000, CloseTime: base + 119_999, Open: 101, High: 103, Low: 100, Close: 101.5, Final: true},
	}
	if !at.mentorX8TrapDodgeExit(nil, p, bars) {
		t.Fatal("a 2m close above the EMA34 against a 5m sell trigger must exit the long")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(closes) != 1 || closes[0] != "long" {
		t.Fatalf("close = %v, want exactly one 'long' market close", closes)
	}
}

// TestMentorX8TrapDodgeExemptsSwingAndNonTrap — the swing (held by the 4h) never
// fires the trap-dodge, and a same-side trigger is not a trap.
func TestMentorX8TrapDodgeExemptsSwingAndNonTrap(t *testing.T) {
	at, _ := newDriveAT(t)
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	e := mentor.New(cfg)
	e.State.EMA34 = 100
	e.State.Trigger = mentor.TriggerLine{Dir: mentor.SideShort, Price: 99}
	at.mentorEval = e

	closed := false
	old := mentorCloseLegAtMarketWire
	mentorCloseLegAtMarketWire = func(_ *AutoTrader, _ *nttrader.TCPTrader, _ string, _ int) error {
		closed = true
		return nil
	}
	t.Cleanup(func() { mentorCloseLegAtMarketWire = old })

	base := int64(120_000_000_000)
	bars := []market.Kline{
		{OpenTime: base, CloseTime: base + 59_999, Open: 100, High: 102, Low: 99, Close: 101, Final: true},
		{OpenTime: base + 60_000, CloseTime: base + 119_999, Open: 101, High: 103, Low: 100, Close: 101.5, Final: true},
	}
	// SWING is exempt.
	if at.mentorX8TrapDodgeExit(nil, bPos("swing", "long", 100, 98, 104, 1), bars) {
		t.Fatal("a SWING4H position must not fire the X8 trap-dodge")
	}
	// A SHORT with a SELL trigger (same side) is not a trap.
	if at.mentorX8TrapDodgeExit(nil, bPos("B", "short", 100, 102, 96, 1), bars) {
		t.Fatal("a short with a 5m sell trigger is not a trap")
	}
	if closed {
		t.Fatal("no close may be sent for a swing or a non-trap")
	}
}

// TestMentorX8TrapDodgeOneShot pins the m262 one-shot latch: two candles in a
// row that BOTH meet the X8 trap-dodge send exactly ONE market close — never a
// second close while the first fill is in flight. MUTANT: drop the p.X8Exited
// latch → the second candle sends a second close → RED.
func TestMentorX8TrapDodgeOneShot(t *testing.T) {
	at, _ := newDriveAT(t)
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	e := mentor.New(cfg)
	e.State.EMA34 = 100
	e.State.Trigger = mentor.TriggerLine{Dir: mentor.SideShort, Price: 99}
	at.mentorEval = e

	p := bPos("B", "long", 100, 98, 104, 1)

	var mu sync.Mutex
	closes := 0
	old := mentorCloseLegAtMarketWire
	mentorCloseLegAtMarketWire = func(_ *AutoTrader, _ *nttrader.TCPTrader, side string, qty int) error {
		mu.Lock()
		closes++
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { mentorCloseLegAtMarketWire = old })

	base := int64(120_000_000_000)
	bars := []market.Kline{
		{OpenTime: base, CloseTime: base + 59_999, Open: 100, High: 102, Low: 99, Close: 101, Final: true},
		{OpenTime: base + 60_000, CloseTime: base + 119_999, Open: 101, High: 103, Low: 100, Close: 101.5, Final: true},
	}
	// Two candles in a row that both meet X8.
	if !at.mentorX8TrapDodgeExit(nil, p, bars) {
		t.Fatal("the first trap candle must exit")
	}
	if at.mentorX8TrapDodgeExit(nil, p, bars) {
		t.Fatal("the second trap candle must NOT send a second close (latched)")
	}
	mu.Lock()
	defer mu.Unlock()
	if closes != 1 {
		t.Fatalf("closes = %d, want exactly 1 (the one-shot latch)", closes)
	}
}
