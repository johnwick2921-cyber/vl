package trader

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// Release #54 (m441) — swing exit state, call-site pins. Every test enters
// through mentorDispatchIntent (the production switch the eval loop calls) or
// mentorSwingReconcile (the production hook at the top of mentorEvalOnce).

// swingOwnedFixture seeds what the live system holds for a FILLED swing arm: an
// OPEN positions row carrying the row's signal id, and a fresh broker book with
// the arm's working "<signal>-sl". It returns a func that closes the position
// row (the stop filled) and drops the bracket from the book.
func swingOwnedFixture(t *testing.T, at *AutoTrader, row store.ArmedOrderDB) (stopOut func()) {
	t.Helper()
	side := "LONG"
	if row.Side == "short" {
		side = "SHORT"
	}
	qty := float64(row.FillQuantity)
	if qty < 1 {
		qty = 1
	}
	pos := &store.TraderPosition{
		TraderID: at.id, ExchangeID: "NT8", ExchangeType: "ninjatrader", Symbol: "MNQ", Side: side,
		EntryQuantity: qty, Quantity: qty, EntryPrice: row.EntryPx, EntryOrderID: row.SignalID,
		EntryTime: time.Now().UnixMilli(), Status: "OPEN", Source: "system",
	}
	if err := at.store.Position().CreateOpenPosition(pos); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	bracket := true
	prev := mentorStopBook
	mentorStopBook = func(*nttrader.TCPTrader) (ntwire.OrderSnapshotPayload, time.Time, bool) {
		mu.Lock()
		defer mu.Unlock()
		if !bracket || row.StopPx <= 0 {
			return ntwire.OrderSnapshotPayload{}, time.Now(), true
		}
		return ntwire.OrderSnapshotPayload{Orders: []ntwire.NT8Order{
			{Name: row.SignalID + "-sl", State: "working", StopPrice: row.StopPx, Quantity: int(qty), Symbol: "MNQ", Action: "sell"},
		}}, time.Now(), true
	}
	t.Cleanup(func() { mentorStopBook = prev })
	return func() {
		mu.Lock()
		bracket = false
		mu.Unlock()
		if ok, err := at.store.Position().ClosePosition(pos.ID, row.StopPx, "stop", -200, 0, "stop"); err != nil || !ok {
			t.Fatalf("fixture: close the position row: ok=%v err=%v", ok, err)
		}
	}
}

// swingSignalMoves captures the signal-scoped stop moves (the wire the intraday
// legs use).
type swingSignalMoves struct {
	mu    sync.Mutex
	calls []swingSignalMove
	err   error
}
type swingSignalMove struct {
	signalID, side string
	stop           float64
	leg            int
}

func (m *swingSignalMoves) install(t *testing.T) {
	t.Helper()
	old := mentorMoveStopForSignalWire
	mentorMoveStopForSignalWire = func(_ *nttrader.TCPTrader, signalID, side string, newStop float64, leg int) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.calls = append(m.calls, swingSignalMove{signalID, side, newStop, leg})
		return m.err
	}
	t.Cleanup(func() { mentorMoveStopForSignalWire = old })
}
func (m *swingSignalMoves) n() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.calls) }
func (m *swingSignalMoves) setErr(e error) {
	m.mu.Lock()
	m.err = e
	m.mu.Unlock()
}

// swingCloseSeams captures the close wires: the X8 marketable limit and the
// market flatten.
type swingCloseSeams struct {
	mu      sync.Mutex
	limits  []float64
	markets int
	limErr  error
	price   float64
	fresh   bool
}

func (c *swingCloseSeams) install(t *testing.T) {
	t.Helper()
	oldL, oldM, oldP := mentorX8LimitWire, mentorCloseLegAtMarketWire, mentorLatestPriceSeam
	mentorX8LimitWire = func(_ *AutoTrader, _ *nttrader.TCPTrader, _ string, limit float64) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.limErr != nil {
			return c.limErr
		}
		c.limits = append(c.limits, limit)
		return nil
	}
	mentorCloseLegAtMarketWire = func(*AutoTrader, *nttrader.TCPTrader, string, int) error {
		c.mu.Lock()
		c.markets++
		c.mu.Unlock()
		return nil
	}
	mentorLatestPriceSeam = func(*AutoTrader) (float64, bool) { return c.price, c.fresh }
	t.Cleanup(func() { mentorX8LimitWire, mentorCloseLegAtMarketWire, mentorLatestPriceSeam = oldL, oldM, oldP })
}
func (c *swingCloseSeams) counts() (limits, markets int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.limits), c.markets
}

// swingEvalWithPos builds the evaluator's side of a FILLED swing for a row.
func swingEvalWithPos(t *testing.T, at *AutoTrader, armID string, row store.ArmedOrderDB) {
	t.Helper()
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	var open int64
	if _, err := fmt.Sscanf(armID, "swing-%d", &open); err != nil || open <= 0 {
		open = swingRestartOpenTime
	}
	side := mentor.SideLong
	if row.Side == "short" {
		side = mentor.SideShort
	}
	if !at.mentorEval.State.Swing.RebuildSwingPos(mentor.SwingRebuild{
		Side: side, Entry: row.EntryPx, EntryStop: row.StopPx, LiveStop: row.StopPx, Target: row.TargetPx,
		EntryBucket: open, ArmID: armID,
	}) {
		t.Fatal("fixture: evaluator swing Pos not built")
	}
}

func swingRow(at *AutoTrader, scenario, side string, entry, stop, target float64, qty int, state string) store.ArmedOrderDB {
	return store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: scenario,
		Side: side, EntryPx: entry, StopPx: stop, TargetPx: target, Kind: "stop_entry", Condition: "SWING4H",
		State: state, SignalID: scenario + "-sig", FillPrice: entry, FillQuantity: qty}
}

// (a) N2 — production call site mentorDispatchIntent. The swing's stop filled
// (the positions row is CLOSED and the bracket is gone); hours later the
// evaluator still sends the hold-close and a BE. Both must be REFUSED, NAMED,
// send nothing on any wire (the AddOn runs close_position as a Flatten of the
// whole instrument and move_stop would move another trade's stop), and clear the
// evaluator's swing state.
// Mutant: gate on the ledger row alone (the pre-m441 mentorSwingFill) -> the
// stopped-out swing closes/moves again -> RED.
func TestMentorSwingStoppedOutNeverCloseOrMovesLater(t *testing.T) {
	at, _, ledger, frames := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	moves := &swingSignalMoves{}
	moves.install(t)
	closes := &swingCloseSeams{price: 29600, fresh: true}
	closes.install(t)
	armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
	row := swingRow(at, armID, "long", 29600, 29570, 29660, 2, store.StateFilled)
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	stopOut := swingOwnedFixture(t, at, row)
	mentorRegisterLiveArmForTest(armID, at.id, row.ID, "long", 29600)
	swingEvalWithPos(t, at, armID, row)
	stopOut() // the stop filled: position CLOSED, bracket gone
	resetMentorCounters(at)

	at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionMoveStopBE, ArmID: armID, Reason: "stale BE"}, mentorTierInputs{}, 1000, 1100)
	if moves.n() != 0 {
		t.Fatalf("a stopped-out swing must not move any stop (it would move ANOTHER trade's): %+v", moves.calls)
	}
	if c := at.mentorCountSnapshot()["move_be_refused_position_gone"]; c != 1 {
		t.Fatalf("move_be_refused_position_gone = %d, want 1; counters=%v", c, at.mentorCountSnapshot())
	}
	if at.mentorEval.State.Swing.Pos != nil {
		t.Fatal("the refusal must clear the evaluator's swing state")
	}

	// The close: re-arm the evaluator state (a second stale intent) and dispatch.
	swingEvalWithPos(t, at, armID, row)
	mentorRegisterLiveArmForTest(armID, at.id, row.ID, "long", 29600)
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionClosePosition, ArmID: armID, Reason: "stale hold-close"}, mentorTierInputs{}, 1000, 1100)
	if l, m := closes.counts(); l != 0 || m != 0 {
		t.Fatalf("a stopped-out swing must not send ANY close (limit %d, market %d)", l, m)
	}
	if sawMentorFrame(t, frames, ntwire.FrameClosePosition, 300*time.Millisecond) {
		t.Fatal("a stopped-out swing must not put a close_position frame on the wire")
	}
	if c := at.mentorCountSnapshot()["close_refused_position_gone"]; c != 1 {
		t.Fatalf("close_refused_position_gone = %d, want 1; counters=%v", c, at.mentorCountSnapshot())
	}
	if at.mentorEval.State.Swing.Pos != nil {
		t.Fatal("the refusal must clear the evaluator's swing state")
	}
}

// (a) N2 — the receipt path. mentorSwingReconcile runs at the top of every tick:
// a swing whose position row is CLOSED is cleared from the evaluator BEFORE it
// can emit anything; a swing whose position is still open is left alone.
// Mutant: no-op the reconcile -> Pos survives the stop-out -> RED.
func TestMentorSwingReconcileClearsStoppedOutSwing(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
	row := swingRow(at, armID, "short", 29600, 29630, 29540, 1, store.StateFilled)
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	stopOut := swingOwnedFixture(t, at, row)
	mentorRegisterLiveArmForTest(armID, at.id, row.ID, "short", 29600)
	swingEvalWithPos(t, at, armID, row)
	resetMentorCounters(at)

	at.mentorSwingReconcile()
	if at.mentorEval.State.Swing.Pos == nil {
		t.Fatal("an OPEN swing must be left alone by the reconcile")
	}
	stopOut()
	at.mentorSwingReconcile()
	if at.mentorEval.State.Swing.Pos != nil {
		t.Fatal("a stopped-out swing must be cleared by the reconcile")
	}
	if c := at.mentorCountSnapshot()["swing_state_ended"]; c != 1 {
		t.Fatalf("swing_state_ended = %d, want 1", c)
	}
	if _, ok := at.mentorLiveArmFor(armID); ok {
		t.Fatal("the finished swing's registry entry must be pruned")
	}
}

// (b) N3 — a refused BE stays owed. The move wire refuses (no receipt reaches
// the evaluator: MovedBE stays false so it re-emits); the next attempt succeeds
// and the receipt lands (MovedBE true). The move goes through the SAME
// signal-scoped path as the intraday legs (never the side-keyed moveStopWire).
// Mutant: report the receipt before the move (or set it unconditionally) ->
// MovedBE true after the refusal -> RED.
func TestMentorSwingBERefusedThenAppliedReportsReceipt(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	moves := &swingSignalMoves{}
	moves.install(t)
	armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
	row := swingRow(at, armID, "long", 29600, 29570, 29660, 2, store.StateFilled)
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	swingOwnedFixture(t, at, row)
	mentorRegisterLiveArmForTest(armID, at.id, row.ID, "long", 29600)
	swingEvalWithPos(t, at, armID, row)
	resetMentorCounters(at)

	moves.setErr(errors.New("NT8 refused: no live bracket"))
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionMoveStopBE, ArmID: armID, Reason: "swing +1R"}, mentorTierInputs{}, 1000, 1100)
	if moves.n() != 1 {
		t.Fatalf("the BE must be attempted once, got %d", moves.n())
	}
	if at.mentorEval.State.Swing.Pos == nil || at.mentorEval.State.Swing.Pos.MovedBE {
		t.Fatalf("a REFUSED BE must leave the swing owed (MovedBE false): %+v", at.mentorEval.State.Swing.Pos)
	}
	if c := at.mentorCountSnapshot()["move_be_refused"]; c != 1 {
		t.Fatalf("move_be_refused = %d, want 1", c)
	}

	moves.setErr(nil)
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionMoveStopBE, ArmID: armID, Reason: "swing +1R (retry)"}, mentorTierInputs{}, 1000, 1100)
	if moves.n() != 2 {
		t.Fatalf("the retry must send the move again, got %d calls", moves.n())
	}
	last := moves.calls[1]
	if last.signalID != row.SignalID || last.stop != 29600 || last.side != "long" {
		t.Fatalf("the BE must be signal-scoped to the arm's own signal at its entry: %+v", last)
	}
	if !at.mentorEval.State.Swing.Pos.MovedBE {
		t.Fatal("the applied BE must report the receipt to the evaluator")
	}
	if c := at.mentorCountSnapshot()["move_be_sent"]; c != 1 {
		t.Fatalf("move_be_sent = %d, want 1", c)
	}
}

// (a) N2 — the bracket is proof of life. A working "<signal>-sl" in the fresh
// broker book proves the position it protects is open EVEN WHEN the positions
// table has not caught up: (1) no row at all yet (the fill is newer than the
// reconcile), (2) a CLOSED row for the signal whose bracket is still working.
// Neither may be read as "no position" (unproven / gone) — the BE is owed and
// must go out. Production call site mentorDispatchIntent.
// Mutant: drop the `bookKnown && bracket` arm of mentorSwingOwnership ->
// (1) is refused as unproven, (2) as gone -> RED.
func TestMentorSwingWorkingBracketIsProofOfLife(t *testing.T) {
	for _, tc := range []struct {
		name      string
		closedRow bool
	}{{"no positions row yet", false}, {"closed row but bracket still working", true}} {
		t.Run(tc.name, func(t *testing.T) {
			at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
			moves := &swingSignalMoves{}
			moves.install(t)
			armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
			row := swingRow(at, armID, "long", 29600, 29570, 29660, 2, store.StateFilled)
			if err := ledger.DB().Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			if tc.closedRow {
				swingOwnedFixture(t, at, row) // open row + bracket ...
				opens, err := at.store.Position().GetOpenPositions(at.id)
				if err != nil || len(opens) != 1 {
					t.Fatalf("fixture: open rows=%d err=%v", len(opens), err)
				}
				// ... then only the ROW closes; the bracket stays working.
				if ok, err := at.store.Position().ClosePosition(opens[0].ID, row.StopPx, "stop", -200, 0, "stop"); err != nil || !ok {
					t.Fatalf("fixture: close the row: ok=%v err=%v", ok, err)
				}
			} else {
				prev := mentorStopBook
				mentorStopBook = func(*nttrader.TCPTrader) (ntwire.OrderSnapshotPayload, time.Time, bool) {
					return ntwire.OrderSnapshotPayload{Orders: []ntwire.NT8Order{
						{Name: row.SignalID + "-sl", State: "working", StopPrice: row.StopPx, Quantity: 2, Symbol: "MNQ", Action: "sell"},
					}}, time.Now(), true
				}
				t.Cleanup(func() { mentorStopBook = prev })
			}
			mentorRegisterLiveArmForTest(armID, at.id, row.ID, "long", 29600)
			swingEvalWithPos(t, at, armID, row)
			resetMentorCounters(at)

			at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionMoveStopBE, ArmID: armID, Reason: "swing +1R"}, mentorTierInputs{}, 1000, 1100)
			if moves.n() != 1 {
				t.Fatalf("a working bracket is proof the position is open: the BE must go out once, got %d (counters=%v)", moves.n(), at.mentorCountSnapshot())
			}
			if at.mentorEval.State.Swing.Pos == nil || !at.mentorEval.State.Swing.Pos.MovedBE {
				t.Fatalf("the applied BE must report its receipt: %+v", at.mentorEval.State.Swing.Pos)
			}
		})
	}
}

// (c) #15 — the hold-close is the X8 marketable limit, NOT a market flatten:
// one limit through the market (long: below the bid by the collar, on the tick
// grid), zero market closes; a second dispatch while the limit is in flight
// sends nothing; once the grace has passed with the position STILL open, ONE
// market close finishes it; a send failure of the limit goes straight to the
// market close.
// Mutant: send the market close first (the pre-m441 CloseLong) -> markets=1,
// limits=0 -> RED.
func TestMentorSwingHoldCloseUsesMarketableLimitThenFallback(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	closes := &swingCloseSeams{price: 29640.0, fresh: true}
	closes.install(t)
	armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
	row := swingRow(at, armID, "long", 29600, 29570, 29660, 2, store.StateFilled)
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	swingOwnedFixture(t, at, row)
	mentorRegisterLiveArmForTest(armID, at.id, row.ID, "long", 29600)
	swingEvalWithPos(t, at, armID, row)
	resetMentorCounters(at)
	in := mentor.Intent{Action: mentor.ActionClosePosition, ArmID: armID, Reason: "hold ONE 4h candle"}

	at.mentorDispatchIntent(in, mentorTierInputs{}, 1000, 1100)
	if l, m := closes.counts(); l != 1 || m != 0 {
		t.Fatalf("first attempt: want exactly ONE marketable limit and NO market flatten, got limit=%d market=%d", l, m)
	}
	// long exit sells through the bid: below the market, collared, on the grid.
	lim := closes.limits[0]
	if !(lim < 29640 && lim >= 29640-25) || lim != float64(int64(lim*4))/4 {
		t.Fatalf("the long exit limit must sit just below the market on the 0.25 grid, got %v", lim)
	}
	if at.mentorEval.State.Swing.Pos == nil {
		t.Fatal("the swing stays owed until the position is confirmed closed")
	}
	// Same bar / limit in flight: nothing more.
	at.mentorDispatchIntent(in, mentorTierInputs{}, 1000, 1100)
	if l, m := closes.counts(); l != 1 || m != 0 {
		t.Fatalf("a limit in flight must not be duplicated: limit=%d market=%d", l, m)
	}
	// Grace over, position still open: ONE market close.
	at.mentorSwingExitSentSet(armID, mentorSwingExitRecord{limitAt: time.Now().Add(-2 * mentorX8LimitFallbackAfter)})
	at.mentorDispatchIntent(in, mentorTierInputs{}, 1000, 1100)
	if l, m := closes.counts(); l != 1 || m != 1 {
		t.Fatalf("after the grace the market close finishes it once: limit=%d market=%d", l, m)
	}
	if c := at.mentorCountSnapshot()["swing_close_market_fallback"]; c != 1 {
		t.Fatalf("swing_close_market_fallback = %d, want 1", c)
	}

	// A limit that cannot be sent -> the market close at once (a close always
	// wins over preserving an arm).
	at2, _, ledger2, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	closes2 := &swingCloseSeams{price: 29640.0, fresh: true, limErr: errors.New("wire down")}
	closes2.install(t)
	row2 := swingRow(at2, armID, "long", 29600, 29570, 29660, 2, store.StateFilled)
	if err := ledger2.DB().Create(&row2).Error; err != nil {
		t.Fatal(err)
	}
	swingOwnedFixture(t, at2, row2)
	mentorRegisterLiveArmForTest(armID, at2.id, row2.ID, "long", 29600)
	swingEvalWithPos(t, at2, armID, row2)
	at2.mentorDispatchIntent(in, mentorTierInputs{}, 1000, 1100)
	if l, m := closes2.counts(); l != 0 || m != 1 {
		t.Fatalf("a failed limit must fall to the market close: limit=%d market=%d", l, m)
	}
}

// (a) N2 — the hook itself. mentorEvalOnce (the production tick) runs the
// reconcile BEFORE the evaluator ticks, so a stopped-out swing is cleared even
// when it would have emitted nothing this bar. The intents seam returns none, so
// only the reconcile hook can clear the state.
// Mutant: delete the mentorSwingReconcile() call in mentorEvalOnce -> Pos
// survives the tick -> RED.
func TestMentorEvalOnceReconcilesStoppedOutSwing(t *testing.T) {
	at, st, ledger, _ := mentorB3Rig(t)
	defer st.Close()
	oldBars := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(_, _ string, _ int) []market.Kline { return nil }
	t.Cleanup(func() { market.FuturesBarsProvider = oldBars })
	armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
	row := swingRow(at, armID, "long", 29600, 29570, 29660, 2, store.StateFilled)
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	stopOut := swingOwnedFixture(t, at, row)
	mentorRegisterLiveArmForTest(armID, at.id, row.ID, "long", 29600)
	swingEvalWithPos(t, at, armID, row)
	stopOut()
	at.mentorEvalIntentsSeam = func([]market.Kline) []mentor.Intent { return nil }
	t.Cleanup(func() { at.mentorEvalIntentsSeam = nil })
	bars := []market.Kline{
		{OpenTime: 1_000_000, CloseTime: 1_000_059, Open: 100, High: 101, Low: 99, Close: 100.5},
		{OpenTime: 1_000_060, CloseTime: 1_000_119, Open: 100.5, High: 102, Low: 100, Close: 101},
	}
	at.mentorEvalOnce(bars)
	if at.mentorEval.State.Swing.Pos != nil {
		t.Fatal("the production tick must clear a stopped-out swing before the evaluator can act on it")
	}
}

// (b) N3 — the log must not promise a retry that does not exist. The old
// refusal said "retried next 4h read" while the evaluator had already cleared
// the swing (nothing was ever retried). The retry is real now (once per closed
// 5m bar - TestSwingHoldCloseRetriesUntilEnded in kernel/mentor) and the text
// says so.
func TestMentorSwingRefusalTextNamesTheRealRetry(t *testing.T) {
	for _, f := range []string{"mentor_course_exits.go", "mentor_actions.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "retried next 4h read") {
			t.Errorf("%s still promises \"retried next 4h read\"", f)
		}
	}
	b, _ := os.ReadFile("mentor_course_exits.go")
	if !strings.Contains(string(b), "re-sends it on the next closed 5m bar") {
		t.Error("mentorSwingCloseQty's refusal must name the real retry (next closed 5m bar)")
	}
}
