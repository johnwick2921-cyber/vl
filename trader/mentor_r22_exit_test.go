package trader

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── RELEASE #22 MENTOR EXIT — production call sites on a REAL loopback bridge ─
//
// Every test here drives the production entry points (mentorExitDrive — the
// once-per-closed-candle drive; dispatchPositionLegClosed — the hook close-sync
// calls on the broker's position_close receipt; registerMentorLivePos — the
// fill-time registration; the restart rebuild inside mentorExitDrive) against
// a real TCPServer + TCPTrader, and asserts the frames the fake AddOn READS off
// the socket (move_stop / modify_bracket). The broker book is the server's own
// order-snapshot cache, which the fake AddOn "applies" by putting a new book.
//
// Numbers are row 362 / position #651 (10-14 01:03 CT, SHORT 3 @ 30742.25,
// stop 30760.25, R 18, leg-1 TP 30724.25, trade target 30703.00) unless a test
// says otherwise.
//
// Named mutants (each anchored to ONE site, run RED then restored):
//   M1 allow widen — mentor_trail_r22.go mentorJudgeAgainstBroker
//      `if mentorStopTighter(side, price, newStop) {` → `if false {`
//      → TestR22NeverWidenJudgedAgainstTheBroker RED.
//   M2 BE on belief — mentor_trail_r22.go mentorExitDriveCandle
//      `if !scaledNow {` → BE once the candle crosses leg 1's TP
//      → TestR22BEAfterConfirmedPartialThenCandleTrail RED.
//   M3 unassign the seam — mentor_mode.go `var modifyBracketWire = func(...)`
//      → a nil declaration → TestR22Candle3PartialIsARealModify RED.

const (
	r22Sig    = "2a0597c0-8d16-4160-a8eb-df8b548f427a"
	r22Entry  = 30742.25
	r22Stop   = 30760.25
	r22L1TP   = 30724.25
	r22Target = 30703.00
)

type r22Bridge struct {
	t     *testing.T
	at    *AutoTrader
	st    *store.Store
	s     *ntwire.TCPServer
	nt    *nttrader.TCPTrader
	mu    sync.Mutex
	moves []ntwire.MoveStopPayload
	mods  []ntwire.ModifyBracketPayload
	closes []ntwire.ClosePositionPayload
}

// r22Fixture stands up the real TCPServer + loopback "AddOn" + TCPTrader +
// store, with mentor mode ON and the given risk-control knobs.
func r22Fixture(t *testing.T, rc store.RiskControlConfig) *r22Bridge {
	t.Helper()
	rc.MentorMode = true
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: rc})
	withMaintenanceDir(t)
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })
	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	waitAddonRegistered(t, s)
	b := &r22Bridge{t: t, at: at, st: st, s: s}
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			switch env.Type {
			case ntwire.FrameMoveStop:
				var p ntwire.MoveStopPayload
				if json.Unmarshal(env.Payload, &p) == nil {
					b.mu.Lock()
					b.moves = append(b.moves, p)
					b.mu.Unlock()
				}
			case ntwire.FrameModifyBracket:
				var p ntwire.ModifyBracketPayload
				if json.Unmarshal(env.Payload, &p) == nil {
					b.mu.Lock()
					b.mods = append(b.mods, p)
					b.mu.Unlock()
				}
			case ntwire.FrameClosePosition:
				var p ntwire.ClosePositionPayload
				if json.Unmarshal(env.Payload, &p) == nil {
					b.mu.Lock()
					b.closes = append(b.closes, p)
					b.mu.Unlock()
				}
			}
		}
	}()
	b.nt = nttrader.NewTCPTrader(s, "MNQ", "Sim101")
	at.trader = b.nt
	at.config.NinjaTraderSymbol = "MNQ"
	at.resetMentorCountersForTest()
	return b
}

// positions seeds the broker's open-position snapshot for Sim101.
func (b *r22Bridge) positions(side string, qty int) {
	if qty <= 0 {
		b.s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
		return
	}
	b.s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: side, Quantity: qty, AvgPrice: r22Entry}})
}

// book puts the broker's order book, received 3 s from now — newer than any
// send so far + 2 s, so the next F1 verdict reads THIS book.
func (b *r22Bridge) book(orders ...ntwire.NT8Order) {
	b.s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: orders}, time.Now().Add(3*time.Second))
}

func r22StopOrder(name string, px float64, qty int) ntwire.NT8Order {
	return ntwire.NT8Order{Name: name, State: "Accepted", Type: "stop", StopPrice: px, Quantity: qty, Symbol: "MNQ"}
}

func r22TP(name string, px float64, qty int) ntwire.NT8Order {
	return ntwire.NT8Order{Name: name, State: "Working", Type: "limit", LimitPrice: px, Quantity: qty, Symbol: "MNQ"}
}

// waitMoves waits (bounded) until at least n move_stop frames arrived, then a
// short settle, and returns every move_stop frame received so far.
func (b *r22Bridge) waitMoves(n int) []ntwire.MoveStopPayload {
	b.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		b.mu.Lock()
		got := len(b.moves)
		b.mu.Unlock()
		if got >= n || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond) // any EXTRA frame lands too
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]ntwire.MoveStopPayload(nil), b.moves...)
}

func (b *r22Bridge) waitMods(n int) []ntwire.ModifyBracketPayload {
	b.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		b.mu.Lock()
		got := len(b.mods)
		b.mu.Unlock()
		if got >= n || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]ntwire.ModifyBracketPayload(nil), b.mods...)
}

func (b *r22Bridge) waitCloses(n int) []ntwire.ClosePositionPayload {
	b.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		b.mu.Lock()
		got := len(b.closes)
		b.mu.Unlock()
		if got >= n || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]ntwire.ClosePositionPayload(nil), b.closes...)
}

// registerSplit registers the filled position through the PRODUCTION fill-time
// builder (registerMentorLivePos), with the split the entry frame carried.
func (b *r22Bridge) registerSplit(sig, side, condition string, n, leg1 int, entry, stop, leg1TP, target float64) {
	b.t.Helper()
	prev := mentorSentSplit
	mentorSentSplit = func(_ *AutoTrader, s string) (nttrader.SentSplit, bool) {
		if s == sig {
			return nttrader.SentSplit{Leg1Qty: leg1, Leg1TP: leg1TP}, true
		}
		return prev(b.at, s)
	}
	b.t.Cleanup(func() { mentorSentSplit = prev })
	row := store.ArmedOrderDB{TraderID: b.at.id, PlanID: "mentor", Scenario: "isb-r22", Side: side,
		EntryPx: entry, StopPx: stop, TargetPx: target, Condition: condition, State: store.StateFilled,
		SignalID: sig, Origin: store.ArmOriginMentor, Contracts: store.IntPtr(n), FillQuantity: n, FillPrice: entry}
	b.at.registerMentorLivePos(row, ntwire.OrderUpdatePayload{SignalID: sig, State: "filled", Quantity: n, FillPrice: entry, Account: "Sim101"})
	if lp := b.at.mentorLivePos[sig]; lp == nil || lp.Legs[1].Wire != 2 {
		b.t.Fatalf("fixture: the split position did not register (%+v)", lp)
	}
}

// receiptLeg1 delivers the broker's leg-1 TP receipt through the hook
// close-sync calls (dispatchPositionLegClosed → mentorMarkLeg1Scaled).
func (b *r22Bridge) receiptLeg1(sig, side string, px float64, at time.Time) {
	registerPostExitDispatch(b.at)
	b.t.Cleanup(func() { unregisterPostExitDispatch(b.at) })
	dispatchPositionLegClosed(b.at.id, ntwire.PositionClosePayload{SignalID: sig, Symbol: "MNQ", PositionSide: side,
		ExitPrice: px, Quantity: 2, ExitReason: "tp", ExitTime: at.UTC().Format(time.RFC3339), Leg: 1, Account: "Sim101"})
}

// candle drives ONE closed 1m candle through the production drive.
func (b *r22Bridge) candle(openAt time.Time, o, h, l, c float64) {
	ms := openAt.UnixMilli()
	b.at.mentorExitDrive([]market.Kline{{OpenTime: ms, CloseTime: ms + 59_999, Open: o, High: h, Low: l, Close: c, Final: true}})
}

func r22Minute(i int) time.Time {
	return time.Now().Add(-30 * time.Minute).Truncate(time.Minute).Add(time.Duration(i) * time.Minute)
}

func r22FullBook(runnerStop float64) []ntwire.NT8Order {
	return []ntwire.NT8Order{
		r22StopOrder(r22Sig+"-sl", r22Stop, 2), r22StopOrder(r22Sig+"-sl2", runnerStop, 1),
		r22TP(r22Sig+"-tp", r22L1TP, 2), r22TP(r22Sig+"-tp2", r22Target, 1),
	}
}

func r22RunnerBook(runnerStop float64) []ntwire.NT8Order {
	return []ntwire.NT8Order{r22StopOrder(r22Sig+"-sl2", runnerStop, 1), r22TP(r22Sig+"-tp2", r22Target, 1)}
}

// TestR22BEAfterConfirmedPartialThenCandleTrail — R3 end to end at the wire
// (SHORT, #651's numbers): a candle that CROSSES leg 1's +1R with no broker
// receipt moves NOTHING; the receipt arrives; the next close sends the runner
// (leg 2 only — never the gone leg 1) to break-even 30742.25; the following
// closes trail 1 tick above each closed candle's HIGH, only when tighter.
// Mutant M2 (BE on the price crossing) sends BE on candle 1 → RED.
func TestR22BEAfterConfirmedPartialThenCandleTrail(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{})
	b.positions("short", 3)
	b.book(r22FullBook(r22Stop)...)
	b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)

	// candle 0 — the fill candle closes; nothing reached.
	b.candle(r22Minute(0), 30742.25, 30744.00, 30738.00, 30739.00)
	// candle 1 — the LOW crosses leg 1's TP (30724.25) but NO receipt yet.
	b.candle(r22Minute(1), 30739.00, 30740.00, 30724.00, 30726.00)
	if got := b.waitMoves(0); len(got) != 0 {
		t.Fatalf("a price crossing with no NT8 receipt must move NOTHING (R3: the fill, never belief); got %+v", got)
	}

	// The broker's leg-1 TP receipt lands mid-candle 2.
	b.receiptLeg1(r22Sig, "short", r22L1TP, r22Minute(2).Add(36*time.Second))
	b.positions("short", 1)
	b.book(r22RunnerBook(r22Stop)...) // the OCO cancelled leg 1's stop

	// candle 2 closes → BE for the RUNNER only. Its low 30723.00 is past +1R
	// but NOT past the pre-R3 half-distance trigger 30722.62 — the row-313
	// shape (10-12 03:22: leg 1 banked +1R, the runner kept its ORIGINAL stop
	// and was stopped −1R), so the base rule sends nothing here.
	b.candle(r22Minute(2), 30726.00, 30729.00, 30723.00, 30725.00)
	got := b.waitMoves(1)
	if len(got) != 1 || got[0].SignalID != r22Sig || got[0].Leg != 2 || got[0].NewStopLoss != r22Entry {
		t.Fatalf("after the confirmed partial exactly ONE move_stop for leg 2 → BE %.2f was expected; got %+v", r22Entry, got)
	}
	b.book(r22RunnerBook(r22Entry)...) // NT8 applied it

	// candle 3 (opened after the receipt) — high 30730.00 → trail to 30730.25.
	b.candle(r22Minute(3), 30725.00, 30730.00, 30720.00, 30722.00)
	got = b.waitMoves(2)
	if len(got) != 2 || got[1].Leg != 2 || got[1].NewStopLoss != 30730.25 {
		t.Fatalf("the trail must move leg 2 to 1 tick above the closed candle's high (30730.25); got %+v", got)
	}
	if n := b.at.mentorCountSnapshot()["stop_move_confirmed"]; n != 1 {
		t.Fatalf("the BE move must be CONFIRMED from the broker book, stop_move_confirmed=%d", n)
	}
	b.book(r22RunnerBook(30730.25)...)

	// candle 4 — a HIGHER high (30735.00): never a widen, nothing sent.
	b.candle(r22Minute(4), 30722.00, 30735.00, 30721.00, 30733.00)
	if got = b.waitMoves(2); len(got) != 2 {
		t.Fatalf("a higher high must move nothing for a short; got %+v", got)
	}
	// candle 5 — a lower high (30726.50) → 30726.75.
	b.candle(r22Minute(5), 30733.00, 30726.50, 30715.00, 30716.00)
	got = b.waitMoves(3)
	if len(got) != 3 || got[2].NewStopLoss != 30726.75 {
		t.Fatalf("a lower high must trail the stop down to 30726.75; got %+v", got)
	}
	for _, m := range got {
		if m.Leg != 2 {
			t.Fatalf("a move was sent for leg %d — only the runner (leg 2) may move after the partial (F-6): %+v", m.Leg, got)
		}
	}
	if b.at.mentorCountSnapshot()["widen_refused"] != 0 {
		t.Fatal("the candle trail must never even ASK for a wider stop")
	}
}

// TestR22BEWaitsWhilePriceIsBackThroughTheEntry — the partial filled (the
// candle-3 TP at a small profit) but the next close is back ABOVE a short's
// entry: a buy stop at the entry would sit below the market (NT8 refuses it),
// so BE waits — nothing sent, counted — and goes out on the first close past
// break-even. Mutant: drop the wait → a move at 30742.25 on the first candle → RED.
func TestR22BEWaitsWhilePriceIsBackThroughTheEntry(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{})
	b.positions("short", 1)
	b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
	b.book(r22RunnerBook(r22Stop)...)
	b.receiptLeg1(r22Sig, "short", 30737.50, r22Minute(0).Add(10*time.Second))
	b.candle(r22Minute(0), 30738.00, 30746.00, 30737.00, 30745.00) // closes ABOVE the entry
	if got := b.waitMoves(0); len(got) != 0 {
		t.Fatalf("BE must wait while the close is back through the entry; got %+v", got)
	}
	if n := b.at.mentorCountSnapshot()["be_after_partial_waiting_price"]; n != 1 {
		t.Fatalf("the wait must be counted, be_after_partial_waiting_price=%d", n)
	}
	b.candle(r22Minute(1), 30745.00, 30746.00, 30733.00, 30735.00) // closes past BE
	if got := b.waitMoves(1); len(got) != 1 || got[0].Leg != 2 || got[0].NewStopLoss != r22Entry {
		t.Fatalf("BE must go out on the first close past it; got %+v", got)
	}
}

// TestR22RefusedBENeverArmsInBelief — a stale broker book (older than
// snapshotMaxAge) refuses every stop move (nothing can be judged against the
// broker — P2-3); the refusal is counted and BE is NOT believed armed: the
// state line keeps the real stop and the next candle with a fresh book sends
// BE. Pinned in BOTH modes. Mutant: arm BE whether or not a move applied → the
// off-mode belief says armed with nothing sent → RED.
func TestR22RefusedBENeverArmsInBelief(t *testing.T) {
	for _, mode := range []string{"off", "candle"} {
		b := r22Fixture(t, store.RiskControlConfig{MentorTrailMode: mode})
		b.positions("short", 1)
		b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
		b.receiptLeg1(r22Sig, "short", r22L1TP, r22Minute(0).Add(30*time.Second))
		b.s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: r22RunnerBook(r22Stop)}, time.Now().Add(-2*time.Minute))
		b.candle(r22Minute(0), 30730.00, 30731.00, 30720.00, 30725.00) // past the half-distance too
		if got := b.waitMoves(0); len(got) != 0 {
			t.Fatalf("%s: a stale book must refuse every move; got %+v", mode, got)
		}
		lp := b.at.mentorLivePos[r22Sig]
		if lp.Pos.ArmedBE || lp.Pos.Stop != r22Stop {
			t.Fatalf("%s: a refused BE must not be believed: armedBE=%v stop=%.2f", mode, lp.Pos.ArmedBE, lp.Pos.Stop)
		}
		if n := b.at.mentorCountSnapshot()["stop_move_refused_book_unknown"]; n < 1 {
			t.Fatalf("%s: the stale-book refusal must be counted, got %d", mode, n)
		}
		b.book(r22RunnerBook(r22Stop)...) // a fresh book arrives
		b.candle(r22Minute(1), 30725.00, 30727.00, 30719.00, 30722.00)
		if got := b.waitMoves(1); len(got) != 1 || got[0].Leg != 2 || got[0].NewStopLoss != r22Entry {
			t.Fatalf("%s: BE must go out once a fresh book judges it; got %+v", mode, got)
		}
	}
}

// TestR22LongTrailHigherLowUpLowerLowNoMove — the long mirror at the wire, with
// an off-grid averaged entry: BE rounds TOWARD PROFIT (30820.10 → 30820.25);
// a higher low moves the stop up to 1 tick under it; a lower low moves nothing.
func TestR22LongTrailHigherLowUpLowerLowNoMove(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{})
	sig := "68be9062-a901-418f-b3de-59e1882f79c2"
	b.positions("long", 1)
	b.registerSplit(sig, "long", "PHL", 3, 2, 30820.10, 30814.75, 30825.25, 30839.00)
	b.book(r22StopOrder(sig+"-sl2", 30814.75, 1), r22TP(sig+"-tp2", 30839.00, 1))
	b.receiptLeg1(sig, "long", 30825.25, r22Minute(0).Add(20*time.Second))
	b.candle(r22Minute(0), 30821.00, 30826.00, 30820.50, 30825.00) // BE
	got := b.waitMoves(1)
	if len(got) != 1 || got[0].Leg != 2 || got[0].NewStopLoss != 30820.25 {
		t.Fatalf("BE on an off-grid long entry 30820.10 must round toward profit to 30820.25; got %+v", got)
	}
	b.book(r22StopOrder(sig+"-sl2", 30820.25, 1), r22TP(sig+"-tp2", 30839.00, 1))
	b.candle(r22Minute(1), 30825.00, 30828.00, 30823.00, 30827.00) // higher low 30823 → 30822.75
	got = b.waitMoves(2)
	if len(got) != 2 || got[1].NewStopLoss != 30822.75 {
		t.Fatalf("a higher low must move the stop UP to 30822.75; got %+v", got)
	}
	b.book(r22StopOrder(sig+"-sl2", 30822.75, 1), r22TP(sig+"-tp2", 30839.00, 1))
	b.candle(r22Minute(2), 30827.00, 30829.00, 30821.00, 30824.00) // lower low → no move
	if got = b.waitMoves(2); len(got) != 2 {
		t.Fatalf("a lower low must move nothing for a long; got %+v", got)
	}
}

// TestR22NeverWidenJudgedAgainstTheBroker — P2-3 at the wire: the owner moved
// the runner's stop BY HAND to 30728.00 (tighter than the bot's belief
// 30742.25). The next candle's trail candidate 30730.25 is tighter than the
// belief but WIDER than the broker: nothing is sent, the refusal is counted,
// and the belief takes the broker's stop. Mutant M1 (drop the broker-tighter
// refusal) sends 30730.25 → RED.
func TestR22NeverWidenJudgedAgainstTheBroker(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{})
	b.positions("short", 1)
	b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
	b.book(r22RunnerBook(r22Stop)...)
	b.receiptLeg1(r22Sig, "short", r22L1TP, r22Minute(0).Add(30*time.Second))
	b.candle(r22Minute(0), 30730.00, 30731.00, 30722.00, 30725.00) // BE sent
	if got := b.waitMoves(1); len(got) != 1 || got[0].NewStopLoss != r22Entry {
		t.Fatalf("fixture: BE first; got %+v", got)
	}
	b.book(r22RunnerBook(r22Entry)...) // NT8 applied BE
	// candle 1 — a high above BE: no trail candidate; the BE verdict CONFIRMS,
	// so no move is pending when the owner acts.
	b.candle(r22Minute(1), 30725.00, 30745.00, 30724.00, 30740.00)
	if n := b.at.mentorCountSnapshot()["stop_move_confirmed"]; n != 1 {
		t.Fatalf("fixture: BE confirmed before the manual move, stop_move_confirmed=%d", n)
	}
	b.book(r22RunnerBook(30728.00)...) // the owner tightened it BY HAND in NT8
	b.candle(r22Minute(2), 30725.00, 30730.00, 30720.00, 30722.00)
	if got := b.waitMoves(1); len(got) != 1 {
		t.Fatalf("a move WIDER than the broker's own stop must never be sent; got %+v", got)
	}
	if n := b.at.mentorCountSnapshot()["widen_refused_broker"]; n != 1 {
		t.Fatalf("the broker-judged refusal must be counted once, widen_refused_broker=%d", n)
	}
	if s := b.at.mentorLivePos[r22Sig].Legs[1].Stop; s != 30728.00 {
		t.Fatalf("the belief must take the broker's stop 30728.00, got %.2f", s)
	}
}

// TestR22LegClosedNoMoveSent — F-6 at the wire. (a) the runner's stop already
// FILLED (no live -sl2, the account flat): no move is sent, counted. (b) the
// pre-R3 rule (knob off) arming BE after leg 1's confirmed partial sends to
// leg 2 only — never to the gone leg 1. (c) a pending verdict for a closed leg
// is recorded and ignored — no WARN path, no re-send.
func TestR22LegClosedNoMoveSent(t *testing.T) {
	t.Run("runner stop gone at the broker", func(t *testing.T) {
		b := r22Fixture(t, store.RiskControlConfig{})
		b.positions("short", 1)
		b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
		b.book(r22RunnerBook(r22Stop)...)
		b.receiptLeg1(r22Sig, "short", r22L1TP, r22Minute(0).Add(30*time.Second))
		// The runner's stop FILLED: the fresh book has no live -sl2 while the
		// positions snapshot still lags (shows the runner open).
		b.book(r22TP(r22Sig+"-tp2", r22Target, 1))
		b.candle(r22Minute(0), 30730.00, 30731.00, 30722.00, 30725.00)
		if got := b.waitMoves(0); len(got) != 0 {
			t.Fatalf("a leg with no live broker stop must never be sent a move; got %+v", got)
		}
		if n := b.at.mentorCountSnapshot()["stop_move_skipped_no_broker_stop"]; n != 1 {
			t.Fatalf("the skipped move must be counted, stop_move_skipped_no_broker_stop=%d", n)
		}
		// A DYING stop (the OCO pulling it) is never moved either.
		dying := r22StopOrder(r22Sig+"-sl2", r22Stop, 1)
		dying.State = "CancelPending"
		b.book(dying)
		b.candle(r22Minute(1), 30725.00, 30726.00, 30720.00, 30721.00)
		if got := b.waitMoves(0); len(got) != 0 {
			t.Fatalf("a dying stop must never be moved; got %+v", got)
		}
	})
	t.Run("account flat — the drive sends nothing", func(t *testing.T) {
		b := r22Fixture(t, store.RiskControlConfig{})
		b.positions("short", 1)
		b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
		b.book(r22RunnerBook(r22Stop)...)
		b.receiptLeg1(r22Sig, "short", r22L1TP, r22Minute(0).Add(30*time.Second))
		b.positions("", 0)
		b.book()
		b.candle(r22Minute(0), 30730.00, 30731.00, 30722.00, 30725.00)
		b.candle(r22Minute(1), 30725.00, 30726.00, 30720.00, 30721.00)
		if got := b.waitMoves(0); len(got) != 0 {
			t.Fatalf("a flat position must never be sent a move; got %+v", got)
		}
	})
	t.Run("off mode BE after the partial skips leg 1", func(t *testing.T) {
		b := r22Fixture(t, store.RiskControlConfig{MentorTrailMode: "off"})
		b.positions("short", 1)
		b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
		// The book LAGS the receipt: leg 1's -sl still shows live (the OCO
		// cancel not yet in a snapshot) — #651 01:05:00. Only the Scaled skip
		// keeps the move off leg 1 here.
		b.book(r22FullBook(r22Stop)...)
		b.receiptLeg1(r22Sig, "short", r22L1TP, r22Minute(0).Add(30*time.Second))
		// the low reaches the off-mode half-distance BE trigger (30722.62).
		b.candle(r22Minute(0), 30730.00, 30731.00, 30722.00, 30725.00)
		got := b.waitMoves(1)
		if len(got) != 1 || got[0].Leg != 2 || got[0].NewStopLoss != r22Entry {
			t.Fatalf("off-mode BE after the confirmed partial must move leg 2 ONLY; got %+v", got)
		}
	})
	t.Run("late verdict for a closed leg is ignored", func(t *testing.T) {
		b := r22Fixture(t, store.RiskControlConfig{})
		b.positions("short", 1)
		b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
		b.receiptLeg1(r22Sig, "short", r22L1TP, r22Minute(0).Add(30*time.Second))
		b.at.mentorRecordPendingStopMove(r22Sig, "short", 1, r22Entry) // a move sent to leg 1 just before it filled
		b.at.mentorExitMu.Lock()
		b.at.mentorPendingStopMoves[mentorStopMoveKey(r22Sig, 1)].sendAt = time.Now().Add(-5 * time.Second)
		b.at.mentorExitMu.Unlock()
		b.book(r22RunnerBook(r22Stop)...)
		b.at.mentorVerifyPendingStopMoves(b.nt)
		snap := b.at.mentorCountSnapshot()
		if snap["stop_move_verify_leg_closed"] != 1 || snap["stop_move_no_order"] != 0 || snap["stop_move_not_applied"] != 0 {
			t.Fatalf("a verdict for the closed leg 1 must be recorded + ignored (no WARN, no P0): %v", snap)
		}
		if got := b.waitMoves(0); len(got) != 0 {
			t.Fatalf("a closed leg's late verdict must never re-send; got %+v", got)
		}
	})
}

// TestR22RefusedMovesAreCounted — NT8 refuses (the book keeps the old price):
// the BE stop move is logged + counted and re-sent once (F1), nothing silent.
func TestR22RefusedMovesAreCounted(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{})
	b.positions("short", 1)
	b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
	b.book(r22RunnerBook(r22Stop)...)
	b.receiptLeg1(r22Sig, "short", r22L1TP, r22Minute(0).Add(30*time.Second))
	b.candle(r22Minute(0), 30730.00, 30731.00, 30722.00, 30725.00) // BE sent
	if got := b.waitMoves(1); len(got) != 1 {
		t.Fatalf("fixture: BE first; got %+v", got)
	}
	b.book(r22RunnerBook(r22Stop)...) // NT8 did NOT apply it
	b.candle(r22Minute(1), 30725.00, 30745.00, 30724.00, 30744.00)
	snap := b.at.mentorCountSnapshot()
	if snap["stop_move_not_applied"] != 1 || snap["stop_move_resend_sent"] != 1 {
		t.Fatalf("a refused stop move must be counted and re-sent once: %v", snap)
	}
}

// TestR22Candle3PartialIsARealMarketSell — R1 (release #30 fold): the ISB fill
// candle closes in profit short of +1R (#651: close 30737.50, low 30735.00 >
// 30724.25) → ONE close_position frame, LEG 1, market (no limit), qty = leg 1's
// 2 contracts. Never a resting limit. Mutant M3 (unassign closeLegWire) → no
// frame → RED.
func TestR22Candle3PartialIsARealMarketSell(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{})
	b.positions("short", 3)
	b.book(r22FullBook(r22Stop)...)
	b.registerSplit(r22Sig, "short", "ISB", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
	b.candle(r22Minute(0), 30742.25, 30744.00, 30735.00, 30737.50)
	closes := b.waitCloses(1)
	if len(closes) != 1 || closes[0].SignalID != r22Sig || closes[0].Leg != 1 || closes[0].Quantity != 2 || closes[0].LimitPrice != 0 {
		t.Fatalf("the candle-3 partial must be ONE market close {leg 1, qty 2, no limit}; got %+v", closes)
	}
	if got := b.waitMoves(0); len(got) != 0 {
		t.Fatalf("the candle-3 market sell moves no stop; got %+v", got)
	}
	if mods := b.waitMods(0); len(mods) != 0 {
		t.Fatalf("the candle-3 market sell sends no modify_bracket; got %+v", mods)
	}
	if n := len(b.waitCloses(1)); n != 1 {
		t.Fatalf("the partial resolves ONCE — got %d close frames", n)
	}
}

// TestR22Candle3PartialPast1RDoesNothing — R1: when the fill candle runs PAST
// +1R, the native +1R TP already filled inside the candle (the 1:1 rule), so
// the candle-3 step sends NO market sell and NO modify at the close. Mutant:
// sell at the close anyway → a close frame → RED.
func TestR22Candle3PartialPast1RDoesNothing(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{})
	b.positions("short", 3)
	b.book(r22FullBook(r22Stop)...)
	b.registerSplit(r22Sig, "short", "ISB", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
	// fill candle: low 30720.00 runs PAST +1R (30724.25); close 30738.00.
	b.candle(r22Minute(0), 30742.25, 30744.00, 30720.00, 30738.00)
	if closes := b.waitCloses(0); len(closes) != 0 {
		t.Fatalf("a past-+1R close must send no market sell (the +1R TP filled inside the candle); got %+v", closes)
	}
	if mods := b.waitMods(0); len(mods) != 0 {
		t.Fatalf("a past-+1R close must send no modify; got %+v", mods)
	}
	if n := b.at.mentorCountSnapshot()["isb_partial_no_sale"]; n != 1 {
		t.Fatalf("isb_partial_no_sale = %d, want 1", n)
	}
}

// TestR22Candle3PartialLosingCloseDoesNothing — R1: a losing fill-candle close
// moves nothing; leg 1's +1R TP keeps resting at NT8 (fail-safe). Mutant: send
// a close on a losing close → a close frame → RED.
func TestR22Candle3PartialLosingCloseDoesNothing(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{})
	b.positions("short", 3)
	b.book(r22FullBook(r22Stop)...)
	b.registerSplit(r22Sig, "short", "ISB", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
	// fill candle closes at a LOSS (30750.00 > entry 30742.25 for a short).
	b.candle(r22Minute(0), 30742.25, 30752.00, 30740.00, 30750.00)
	if closes := b.waitCloses(0); len(closes) != 0 {
		t.Fatalf("a losing close must send no market sell (the +1R TP keeps resting); got %+v", closes)
	}
	if n := b.at.mentorCountSnapshot()["isb_partial_no_sale"]; n != 1 {
		t.Fatalf("isb_partial_no_sale = %d, want 1", n)
	}
}

// TestR22RestartMidTrailResumesTheStage — after a restart the stage is
// re-derived from the ledger + the broker (the rebuild inside the production
// drive): (a) leg 1 gone and the runner already trailed to 30730.25 → the next
// candle TRAILS (30726.75), no BE re-send; (b) leg 1 gone and the runner still
// at the original stop → the next candle sends BE.
func TestR22RestartMidTrailResumesTheStage(t *testing.T) {
	for _, tc := range []struct {
		name       string
		runnerStop float64
		want       float64
	}{
		{"trail stage", 30730.25, 30726.75},
		{"BE stage", r22Stop, r22Entry},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := r22Fixture(t, store.RiskControlConfig{})
			arm := &store.ArmedOrderDB{TraderID: b.at.id, PlanID: "mentor", Scenario: "isb-r22", Side: "short",
				State: store.StateFilled, SignalID: r22Sig, EntryPx: r22Entry, StopPx: r22Stop, TargetPx: r22Target,
				Origin: store.ArmOriginMentor, Condition: "ISB", Contracts: store.IntPtr(3), Leg1Qty: store.IntPtr(2),
				Leg1TP: r22L1TP, FillPrice: r22Entry, FillQuantity: 3, ExitMode: mentorExitModePtr("B")}
			if err := b.st.ArmedOrders().UpsertArm(arm); err != nil {
				t.Fatal(err)
			}
			if err := b.st.Position().CreateOpenPosition(&store.TraderPosition{TraderID: b.at.id, ExchangeID: "NT8",
				ExchangeType: "ninjatrader", Symbol: "MNQ", Side: "SHORT", EntryQuantity: 3, Quantity: 1,
				EntryPrice: r22Entry, EntryOrderID: r22Sig, Status: "OPEN", Source: "system"}); err != nil {
				t.Fatal(err)
			}
			b.positions("short", 1)
			b.book(r22RunnerBook(tc.runnerStop)...)
			// The first closed candle after the restart: the rebuild runs, then the drive.
			b.candle(r22Minute(0), 30725.00, 30726.50, 30715.00, 30716.00)
			if n := b.at.mentorCountSnapshot()["rebuild_ok"]; n != 1 {
				t.Fatalf("the restart rebuild must register the position, rebuild_ok=%d", n)
			}
			if b.at.mentorCountSnapshot()["modify_bracket_sent"] != 0 {
				t.Fatal("a rebuilt ISB must never resolve the candle-3 partial off a post-restart candle")
			}
			got := b.waitMoves(1)
			if len(got) != 1 || got[0].Leg != 2 || got[0].NewStopLoss != tc.want {
				t.Fatalf("%s: the rebuilt position must resume with ONE leg-2 move to %.2f; got %+v", tc.name, tc.want, got)
			}
		})
	}
}

// TestR22RestartNeverResolvesTheCandle3PartialLate — a restart-rebuilt ISB with
// leg 1 still open: the first post-restart candle is NOT the fill candle, so a
// close in profit short of +1R must send NO modify (it would sell leg 1 off the
// wrong candle). Mutant: drop ISBPartialDone on the rebuild → a modify → RED.
func TestR22RestartNeverResolvesTheCandle3PartialLate(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{})
	arm := &store.ArmedOrderDB{TraderID: b.at.id, PlanID: "mentor", Scenario: "isb-r22", Side: "short",
		State: store.StateFilled, SignalID: r22Sig, EntryPx: r22Entry, StopPx: r22Stop, TargetPx: r22Target,
		Origin: store.ArmOriginMentor, Condition: "ISB", Contracts: store.IntPtr(3), Leg1Qty: store.IntPtr(2),
		Leg1TP: r22L1TP, FillPrice: r22Entry, FillQuantity: 3, ExitMode: mentorExitModePtr("B")}
	if err := b.st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := b.st.Position().CreateOpenPosition(&store.TraderPosition{TraderID: b.at.id, ExchangeID: "NT8",
		ExchangeType: "ninjatrader", Symbol: "MNQ", Side: "SHORT", EntryQuantity: 3, Quantity: 3,
		EntryPrice: r22Entry, EntryOrderID: r22Sig, Status: "OPEN", Source: "system"}); err != nil {
		t.Fatal(err)
	}
	b.positions("short", 3)
	b.book(r22FullBook(r22Stop)...)
	b.candle(r22Minute(0), 30742.25, 30744.00, 30735.00, 30737.50)
	if n := b.at.mentorCountSnapshot()["rebuild_ok"]; n != 1 {
		t.Fatalf("fixture: the rebuild must register the position, rebuild_ok=%d", n)
	}
	if mods := b.waitMods(0); len(mods) != 0 {
		t.Fatalf("a rebuilt ISB must never send the candle-3 modify off a post-restart candle; got %+v", mods)
	}
}

// TestR22KnobOffKeepsThePreR3Rule — mentor_trail_mode=off: after the confirmed
// partial, a candle that does NOT reach half the distance to the trade target
// moves nothing (the old rule), while the default (candle) sends BE on the
// same candle.
func TestR22KnobOffKeepsThePreR3Rule(t *testing.T) {
	for _, tc := range []struct {
		mode  string
		moves int
	}{{"off", 0}, {"", 1}, {"candle", 1}} {
		b := r22Fixture(t, store.RiskControlConfig{MentorTrailMode: tc.mode})
		b.positions("short", 1)
		b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
		b.book(r22RunnerBook(r22Stop)...)
		b.receiptLeg1(r22Sig, "short", r22L1TP, r22Minute(0).Add(30*time.Second))
		// low 30723.00 is past +1R but NOT past the half-distance 30722.62.
		b.candle(r22Minute(0), 30730.00, 30731.00, 30723.00, 30725.00)
		if got := b.waitMoves(tc.moves); len(got) != tc.moves {
			t.Fatalf("mentor_trail_mode=%q: want %d move(s), got %+v", tc.mode, tc.moves, got)
		}
		if n := b.at.mentorCountSnapshot()["trail_mode_bad_value"]; n != 0 {
			t.Fatalf("mentor_trail_mode=%q is a VALID value (unset = the candle default), never a bad one: counted %d", tc.mode, n)
		}
	}
	// A bad value fails closed to candle (BE) and is counted.
	b := r22Fixture(t, store.RiskControlConfig{MentorTrailMode: "sideways"})
	if got := b.at.mentorTrailMode(); got != mentorTrailModeCandle || b.at.mentorCountSnapshot()["trail_mode_bad_value"] != 1 {
		t.Fatalf("a bad mentor_trail_mode must fail closed to candle and be counted: %q %v", got, b.at.mentorCountSnapshot())
	}
}

// TestR22ModifyBracketLegFrameIsLegScopedAndRounded — the TCPTrader half: the
// frame names the leg and never carries an off-grid price (MATH-EXECUTOR P2-2).
func TestR22ModifyBracketLegFrameIsLegScopedAndRounded(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{})
	if err := b.nt.ModifyBracketLeg(r22Sig, "short", 1, 30760.10, 30737.40); err != nil {
		t.Fatal(err)
	}
	mods := b.waitMods(1)
	if len(mods) != 1 || mods[0].Leg != 1 || mods[0].NewTakeProfit != 30737.50 || mods[0].NewStopLoss != 30760.25 {
		t.Fatalf("leg-scoped modify must round TP to the nearest tick and the stop AWAY from the entry: %+v", mods)
	}
	if err := b.nt.ModifyBracketLeg(r22Sig, "sideways", 1, 30760.10, 0); err == nil {
		t.Fatal("a stop with no long/short side must be refused, never guessed")
	}
	if err := b.nt.ModifyBracketLeg(r22Sig, "short", 1, 0, 0); err == nil {
		t.Fatal("nothing to change must be an error, never an empty frame")
	}
}
