package trader

import (
	"testing"
	"time"

	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── EX-01 / EX-02 (full re-read 11-16, fix/mentor-reread-1116) ──────────────
//
// Both pins run the PRODUCTION call sites on the real loopback bridge
// (r22Fixture): the fill-time registration (registerMentorLivePos), the hook
// close-sync calls on the broker's leg-1 TP receipt (dispatchPositionLegClosed),
// the event pass that receipt wakes (mentorEventPassAt → mentorEvalOnce) and the
// once-per-closed-candle exit drive (mentorExitDrive). They assert the frames the
// fake AddOn READS off the socket.
//
// Numbers are the live 11-16 trades (MNQ 12-26, CT; read-only data.db):
//   EX-01: armed_orders 608/609 LONG 5 ISB, entry 27364.25 (filled 27364.05),
//          stop 27345.25, target 27383.25, leg 1 = 3 @ TP 27383.25, runner 2.
//          trader_fills 738/739: leg 1 SELL 3 @ 27366.50 at 19:15:00;
//          trader_fills 740/741: runner SELL 2 @ 27345.125 at 19:15:40 (−$75.50
//          each) — on its ORIGINAL stop: nothing moved it between the two.
//   EX-02: armed_orders 610/611 LONG 1 PHL @ 27374.75, stop 27338.50, target
//          27411.00, A-resonance; 19:19:00 "leg 0 → 27375.75 is through the
//          market (price 27375.25) — closing the leg at market instead (= the
//          BE exit)"; trader_fills 742/743 SELL 1 @ 27374.75, pnl 0.

const (
	ex01Sig    = "d3470df0-7619-4f5b-a24b-63556fb1fbb9"
	ex01Fill   = 27364.05
	ex01Stop   = 27345.25
	ex01Target = 27383.25
	ex01L1Px   = 27366.50 // trader_fills 738: leg 1 sold 3 @ 27366.50
	ex01RR11   = 27349.75 // max(27345.25, 2·27366.50 − 27383.25) [D1.2 @26:40-26:57]

	ex02Sig    = "d26cee2b-578c-4377-a54f-5223a0ad7e13"
	ex02Entry  = 27374.75
	ex02Stop   = 27338.50
	ex02Target = 27411.00
)

// ex01Off is the EX-01 revert (mentor_runner_1to1_at_fill=false) for the R3
// mechanics pins that assert the move on the closed candle AFTER the receipt.
func ex01Off() *store.MentorTuning {
	return &store.MentorTuning{MentorRunnerRR11AtFill: cxBool(false)}
}

// ex0102Price pins the freshest-1m-close seam the wrong-side guard reads.
func ex0102Price(t *testing.T, px float64) {
	t.Helper()
	prev := mentorLatestPriceSeam
	mentorLatestPriceSeam = func(*AutoTrader) (float64, bool) { return px, true }
	t.Cleanup(func() { mentorLatestPriceSeam = prev })
}

// ex01Book is the 11-16 19:14 broker book: leg 1 (-sl/-tp, 3) and the runner
// (-sl2/-tp2, 2), both stops at the original 27345.25.
func ex01Book(runnerStop float64) []ntwire.NT8Order {
	return []ntwire.NT8Order{
		r22StopOrder(ex01Sig+"-sl", ex01Stop, 3), r22StopOrder(ex01Sig+"-sl2", runnerStop, 2),
		r22TP(ex01Sig+"-tp", ex01Target, 3), r22TP(ex01Sig+"-tp2", ex01Target, 2),
	}
}

// ex01Setup registers the 11-16 19:14 ISB (split 3 + 2) through the production
// builder and closes the 19:14 fill candle through the production drive.
func ex01Setup(t *testing.T, rc store.RiskControlConfig) *r22Bridge {
	t.Helper()
	b := r22Fixture(t, rc)
	b.positionsPx("long", 5, ex01Fill)
	b.book(ex01Book(ex01Stop)...)
	b.registerSplit(ex01Sig, "long", "ISB", 5, 3, ex01Fill, ex01Stop, ex01Target, ex01Target)
	ex0102Price(t, 27365.50)
	// 19:14 (the fill candle): O 27358.75 H 27372.50 L 27355.75 C 27365.50 — the
	// candle-3 partial moves leg 1's TP to a marketable limit; the runner stays.
	b.candle(r22Minute(0), 27358.75, 27372.50, 27355.75, 27365.50)
	if got := b.waitMoves(0); len(got) != 0 {
		t.Fatalf("the fill candle must move no stop (stage 0, close short of half-way); got %+v", got)
	}
	return b
}

// ex01EventPass runs the event pass the receipt's poke wakes: the newest
// closed bar is already evaluated (the watermark), so mentorEvalOnce runs its
// drains and returns — exactly the pass that runs between two candle closes.
func ex01EventPass(t *testing.T, b *r22Bridge) {
	t.Helper()
	open := r22Minute(0).UnixMilli()
	bar := market.Kline{OpenTime: open, CloseTime: open + 59_999, Open: 27358.75, High: 27372.50, Low: 27355.75, Close: 27365.50, Final: true}
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return []market.Kline{bar} }
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	b.at.mentorLastTickOpen = open
	b.at.mentorEventPassAt(time.Now())
}

// TestEX01_1116_1914RunnerTo1to1AtTheLeg1Fill — the 19:14 ISB: the moment the
// broker's leg-1 TP receipt lands (19:15:00, 3 @ 27366.50) the runner's stop
// goes to the live 1:1 max(stop, 2·price − T) = 27349.75 — on the pass that
// receipt wakes, NOT on the next closed candle (19:16:00, after the runner was
// already stopped at 19:15:40 on its original 27345.25). "1 là mình bán bớt 2
// là mình kéo stop lên giữ cái mức reward 1 1" [Oct D1.2 @26:40-26:57]; keep
// 1:1 "khi anh chị đã vô lệnh rồi và nến nó đang chạy" [D1.2 @17:45-18:14].
func TestEX01_1116_1914RunnerTo1to1AtTheLeg1Fill(t *testing.T) {
	b := ex01Setup(t, store.RiskControlConfig{})
	b.receiptLeg1(ex01Sig, "long", ex01L1Px, r22Minute(1))
	b.positionsPx("long", 2, ex01Fill)
	b.book(ex01Book(ex01Stop)[1:2]...) // the OCO cancelled leg 1's stop; the runner's -sl2 rests
	ex01EventPass(t, b)
	got := b.waitMoves(1)
	if len(got) != 1 || got[0].SignalID != ex01Sig || got[0].Leg != 2 || got[0].NewStopLoss != ex01RR11 {
		t.Fatalf("EX-01: on the leg-1 fill receipt the RUNNER (leg 2) must go at once to the live 1:1 %.2f = max(%.2f, 2·%.2f − %.2f) [D1.2 @26:40-26:57]; got %+v",
			ex01RR11, ex01Stop, ex01L1Px, ex01Target, got)
	}
	if n := b.at.mentorCountSnapshot()["runner_rr11_at_partial_fill"]; n != 1 {
		t.Fatalf("the at-fill move must be counted, runner_rr11_at_partial_fill=%d", n)
	}
}

// TestEX02_1116_1917OneLotResonanceNoMarketExitAtEntry — the 19:17 PHL long,
// ONE contract (nothing to sell), flipped to mode A by the 19:18 raw inside bar.
// The 19:18 close 27375.00 is 0.25 past the entry; the resonance BE 27375.75
// (entry + 1 pt fee, D4.2 @13:00-13:25) sits above the market (27375.25), and
// the live code turned that BE into a MARKET CLOSE at the entry. October: sell
// part at +15/+20 FIRST, then BE ("anh chỉ có quyền bán ở giá 15 điểm hoặc là
// bán 20 điểm … xong sau đó anh chị để stop loss ở break even" [D4.2
// @12:14-12:27]); no October line closes a trade at market at its entry.
func TestEX02_1116_1917OneLotResonanceNoMarketExitAtEntry(t *testing.T) {
	octBEDefault(t)   // BE = entry + 4 ticks = 27375.75, the live value
	octLeg1Default(t) // +15 pts, the live mentor_leg1_fixed_pts
	b := r22Fixture(t, store.RiskControlConfig{})
	var closes []string
	prevClose := mentorCloseLegAtMarketWire
	mentorCloseLegAtMarketWire = func(_ *AutoTrader, _ *nttrader.TCPTrader, side string, _ int) error {
		closes = append(closes, side)
		return nil
	}
	t.Cleanup(func() { mentorCloseLegAtMarketWire = prevClose })
	b.positionsPx("long", 1, ex02Entry)
	b.book(r22StopOrder(ex02Sig+"-sl", ex02Stop, 1), r22TP(ex02Sig+"-tp", ex02Target, 1))
	row := store.ArmedOrderDB{TraderID: b.at.id, PlanID: "mentor", Scenario: "phl-ex02", Side: "LONG",
		EntryPx: ex02Entry, StopPx: ex02Stop, TargetPx: ex02Target, Condition: "PHL", State: store.StateFilled,
		SignalID: ex02Sig, Origin: store.ArmOriginMentor, Contracts: store.IntPtr(1), FillQuantity: 1, FillPrice: ex02Entry}
	b.at.registerMentorLivePos(row, ntwire.OrderUpdatePayload{SignalID: ex02Sig, State: "filled", Quantity: 1, FillPrice: ex02Entry, Account: "Sim101"})
	lp := b.at.mentorLivePos[ex02Sig]
	if lp == nil || lp.Legs[0].Wire != 0 {
		t.Fatalf("fixture: the one-bracket position did not register (%+v)", lp)
	}
	// 19:17 (the fill candle) in mode B: H 27385.00 < the BE-half 27392.88 — nothing.
	ex0102Price(t, 27382.75)
	b.candle(r22Minute(0), 27366.75, 27385.00, 27366.25, 27382.75)
	// 19:19:00 — the 19:18 raw inside bar flips it to mode A (mentorResonanceRawDrive,
	// "resonance ARMED on a RAW inside bar"); the arm itself is not under test.
	b.at.mentorExitMu.Lock()
	lp.Pos.Mode = "A-resonance"
	b.at.mentorExitMu.Unlock()
	ex0102Price(t, 27375.25) // the log's "price 27375.25"
	b.candle(r22Minute(1), 27383.00, 27395.00, 27358.75, 27375.00)
	if len(closes) != 0 {
		t.Fatalf("EX-02: the resonance BE on a 1-lot must NEVER become a market close at the entry (19:19:00 live: SELL 1 @ 27374.75, pnl 0); got %d close(s)", len(closes))
	}
	if got := b.waitMoves(0); len(got) != 0 {
		t.Fatalf("EX-02: BE 27375.75 is above the 19:18 close 27375.00 — it waits; got %+v", got)
	}
	// 19:19 closes 27378.00 (past BE + 1 tick); the 19:18 high 27395.00 already
	// reached +15 (27389.75) → BE + fee goes out as a STOP, the trade holds.
	ex0102Price(t, 27378.00)
	b.candle(r22Minute(2), 27375.25, 27385.00, 27371.25, 27378.00)
	got := b.waitMoves(1)
	if len(got) != 1 || got[0].SignalID != ex02Sig || got[0].NewStopLoss != 27375.75 || len(closes) != 0 {
		t.Fatalf("EX-02: after +15 was reached, the first close past BE sends BE + fee 27375.75 as a stop (no close); got moves %+v closes %d", got, len(closes))
	}
}

// TestEX01_1116_1914RevertWaitsForTheNextCandle — mentor_runner_1to1_at_fill =
// false is the live 11-16 behaviour exactly: the receipt only latches Scaled,
// the event pass it wakes moves nothing, and the runner keeps 27345.25 until
// the next closed candle (where the R3 stage waits: the 19:15 close 27344.75
// is back through the entry).
func TestEX01_1116_1914RevertWaitsForTheNextCandle(t *testing.T) {
	b := ex01Setup(t, store.RiskControlConfig{MentorTuning: ex01Off()})
	b.receiptLeg1(ex01Sig, "long", ex01L1Px, r22Minute(1))
	b.positionsPx("long", 2, ex01Fill)
	b.book(ex01Book(ex01Stop)[1:2]...)
	ex01EventPass(t, b)
	if got := b.waitMoves(0); len(got) != 0 {
		t.Fatalf("revert: the receipt's event pass must move nothing (the runner waits for the next closed candle); got %+v", got)
	}
	ex0102Price(t, 27344.75)
	b.candle(r22Minute(1), 27365.75, 27369.25, 27337.75, 27344.75)
	if got := b.waitMoves(0); len(got) != 0 {
		t.Fatalf("revert: the 19:15 close 27344.75 is back through the entry — BE waits, the runner keeps 27345.25; got %+v", got)
	}
	if n := b.at.mentorCountSnapshot()["runner_rr11_at_partial_fill_off"]; n != 1 {
		t.Fatalf("the owed move skipped by the revert must be counted, runner_rr11_at_partial_fill_off=%d", n)
	}
}

// TestEX01_AtFill1to1PastBreakEvenIsTheBreakEven — leg 1 sold by the candle-3
// marketable limit well past half-way (long 100, stop 90, T 120, leg 1 at 112):
// the at-fill 1:1 is 2·112 − 120 = 104, already PAST break-even 100. The next
// closed candle's R3 stage counts that as BE (armed, nothing sent) instead of
// asking for a wider BE that never-widen would refuse every candle.
func TestEX01_AtFill1to1PastBreakEvenIsTheBreakEven(t *testing.T) {
	const sig = "sig-ex01-past-be"
	b := r22Fixture(t, store.RiskControlConfig{MentorBEBeforePartial: cxBool(false)})
	b.positionsPx("long", 5, 30100)
	b.book(r22StopOrder(sig+"-sl", 30090, 3), r22StopOrder(sig+"-sl2", 30090, 2), r22TP(sig+"-tp", 30110, 3), r22TP(sig+"-tp2", 30120, 2))
	b.registerSplit(sig, "long", "PHL", 5, 3, 30100, 30090, 30110, 30120)
	ex0102Price(t, 30112)
	b.receiptLeg1(sig, "long", 30112, r22Minute(0).Add(20*time.Second))
	b.positionsPx("long", 2, 30100)
	b.book(r22StopOrder(sig+"-sl2", 30090, 2), r22TP(sig+"-tp2", 30120, 2))
	b.candle(r22Minute(0), 30100, 30113, 30099, 30111)
	got := b.waitMoves(1)
	if len(got) != 1 || got[0].Leg != 2 || got[0].NewStopLoss != 30104 {
		t.Fatalf("the at-fill 1:1 must send the runner to 30104 = 2·30112 − 30120, and the R3 stage must send NO wider BE; got %+v", got)
	}
	lp := b.at.mentorLivePos[sig]
	if lp == nil || !lp.Pos.ArmedBE || lp.Legs[1].Stop != 30104 {
		t.Fatalf("the runner at 30104 (past BE 30100) must count as break-even armed; got %+v", lp)
	}
	if n := b.at.mentorCountSnapshot()["widen_refused"]; n != 0 {
		t.Fatalf("no wider BE may even be asked for, widen_refused=%d", n)
	}
}

// TestEX02_1116_1917RevertClosesAtMarketAtEntry — mentor_resonance_1lot_be_guard
// = false is the live 11-16 19:19:00 behaviour exactly: the 19:18 close 27375.00
// is past the entry (mentor_resonance_be_min_rr 0), BE 27375.75 is through the
// market 27375.25, and the leg is closed at market at its entry.
func TestEX02_1116_1917RevertClosesAtMarketAtEntry(t *testing.T) {
	octBEDefault(t)
	octLeg1Default(t)
	off := false
	b := r22Fixture(t, store.RiskControlConfig{MentorTuning: &store.MentorTuning{MentorResonance1LotBEGuard: &off}})
	closes := 0
	prevClose := mentorCloseLegAtMarketWire
	mentorCloseLegAtMarketWire = func(*AutoTrader, *nttrader.TCPTrader, string, int) error { closes++; return nil }
	t.Cleanup(func() { mentorCloseLegAtMarketWire = prevClose })
	b.positionsPx("long", 1, ex02Entry)
	b.book(r22StopOrder(ex02Sig+"-sl", ex02Stop, 1), r22TP(ex02Sig+"-tp", ex02Target, 1))
	row := store.ArmedOrderDB{TraderID: b.at.id, PlanID: "mentor", Scenario: "phl-ex02", Side: "LONG",
		EntryPx: ex02Entry, StopPx: ex02Stop, TargetPx: ex02Target, Condition: "PHL", State: store.StateFilled,
		SignalID: ex02Sig, Origin: store.ArmOriginMentor, Contracts: store.IntPtr(1), FillQuantity: 1, FillPrice: ex02Entry}
	b.at.registerMentorLivePos(row, ntwire.OrderUpdatePayload{SignalID: ex02Sig, State: "filled", Quantity: 1, FillPrice: ex02Entry, Account: "Sim101"})
	lp := b.at.mentorLivePos[ex02Sig]
	b.at.mentorExitMu.Lock()
	lp.Pos.Mode = "A-resonance"
	b.at.mentorExitMu.Unlock()
	ex0102Price(t, 27375.25)
	b.candle(r22Minute(1), 27383.00, 27395.00, 27358.75, 27375.00)
	if closes != 1 {
		t.Fatalf("revert: the 19:19:00 BE exit (a market close at the entry) must come back exactly once; got %d", closes)
	}
}

// TestEX02_OneLotShortWaitsForTheFirstTargetAndNeverClosesAtMarket — the short
// mirror on the production drive: a 1-lot short resonance (entry 30100, stop
// 30136, BE + fee 30099) whose candles stay inside +15 moves nothing (no BE,
// no close); once a candle's LOW reaches 30085 (−15) a close back above BE
// waits (no close), and the first close below 30098.75 sends BE + fee.
func TestEX02_OneLotShortWaitsForTheFirstTargetAndNeverClosesAtMarket(t *testing.T) {
	octBEDefault(t)
	octLeg1Default(t)
	const sig = "sig-ex02-short"
	b := r22Fixture(t, store.RiskControlConfig{})
	closes := 0
	prevClose := mentorCloseLegAtMarketWire
	mentorCloseLegAtMarketWire = func(*AutoTrader, *nttrader.TCPTrader, string, int) error { closes++; return nil }
	t.Cleanup(func() { mentorCloseLegAtMarketWire = prevClose })
	b.positionsPx("short", 1, 30100)
	b.book(r22StopOrder(sig+"-sl", 30136, 1), r22TP(sig+"-tp", 30064, 1))
	row := store.ArmedOrderDB{TraderID: b.at.id, PlanID: "mentor", Scenario: "plh-ex02", Side: "SHORT",
		EntryPx: 30100, StopPx: 30136, TargetPx: 30064, Condition: "PLH", State: store.StateFilled,
		SignalID: sig, Origin: store.ArmOriginMentor, Contracts: store.IntPtr(1), FillQuantity: 1, FillPrice: 30100}
	b.at.registerMentorLivePos(row, ntwire.OrderUpdatePayload{SignalID: sig, State: "filled", Quantity: 1, FillPrice: 30100, Account: "Sim101"})
	lp := b.at.mentorLivePos[sig]
	b.at.mentorExitMu.Lock()
	lp.Pos.Mode = "A-resonance"
	b.at.mentorExitMu.Unlock()
	// low 30090 (−10): the first target −15 is not reached; close 30099.50 sits
	// above BE 30099 — the revert would close this at market.
	ex0102Price(t, 30099.50)
	b.candle(r22Minute(0), 30100, 30104, 30090, 30099.50)
	if got := b.waitMoves(0); len(got) != 0 || closes != 0 {
		t.Fatalf("short: before −15 nothing moves and nothing closes; got moves %+v closes %d", got, closes)
	}
	// low 30085 reaches −15; the close 30099.25 is still above BE − tick → wait, no close.
	ex0102Price(t, 30099.25)
	b.candle(r22Minute(1), 30099.50, 30100, 30085, 30099.25)
	if got := b.waitMoves(0); len(got) != 0 || closes != 0 {
		t.Fatalf("short: −15 reached but the close is not past BE — wait, never a market close; got moves %+v closes %d", got, closes)
	}
	// the first close past BE − tick (30098.50) sends BE + fee 30099 as a stop.
	ex0102Price(t, 30098.50)
	b.candle(r22Minute(2), 30099.25, 30100, 30096, 30098.50)
	got := b.waitMoves(1)
	if len(got) != 1 || got[0].NewStopLoss != 30099 || closes != 0 {
		t.Fatalf("short: BE + fee 30099 must go out as a stop on the first close past it; got moves %+v closes %d", got, closes)
	}
	snap := b.at.mentorCountSnapshot()
	if snap["resonance_be_waiting_first_target"] != 1 || snap["resonance_be_waiting_price"] != 1 || snap["resonance_be_first_target_reached"] != 1 {
		t.Fatalf("counters: waiting_first_target=%d waiting_price=%d reached=%d, want 1/1/1",
			snap["resonance_be_waiting_first_target"], snap["resonance_be_waiting_price"], snap["resonance_be_first_target_reached"])
	}
}

// TestEX01_Leg1SoldAtTheRunnerTargetSendsNothing — an ISB whose trade target is
// the +1R (11-16: leg 1 and the runner both at 27383.25): when leg 1 sells AT
// that target the runner's take-profit is filling with it, so the "1:1" would
// be the market itself — the at-fill move sends nothing and closes nothing.
func TestEX01_Leg1SoldAtTheRunnerTargetSendsNothing(t *testing.T) {
	b := ex01Setup(t, store.RiskControlConfig{MentorBEBeforePartial: cxBool(false)})
	closes := 0
	prevClose := mentorCloseLegAtMarketWire
	mentorCloseLegAtMarketWire = func(*AutoTrader, *nttrader.TCPTrader, string, int) error { closes++; return nil }
	t.Cleanup(func() { mentorCloseLegAtMarketWire = prevClose })
	ex0102Price(t, 27383.00)
	b.receiptLeg1(ex01Sig, "long", ex01Target, r22Minute(1))
	ex01EventPass(t, b)
	if got := b.waitMoves(0); len(got) != 0 || closes != 0 {
		t.Fatalf("leg 1 sold at the runner's own target: no at-fill move and no market close; got moves %+v closes %d", got, closes)
	}
	if n := b.at.mentorCountSnapshot()["runner_rr11_at_partial_fill_target_reached"]; n != 1 {
		t.Fatalf("runner_rr11_at_partial_fill_target_reached=%d, want 1", n)
	}
}
