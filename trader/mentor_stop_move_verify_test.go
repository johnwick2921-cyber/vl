package trader

import (
	"errors"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	ntTrader "vl/trader/ninjatrader"
	"vl/store"
)

// F1 STOP-MOVE TRUTH — call-site tests. The seam under test is
// mentorVerifyPendingStopMoves: it renders a verdict on a SENT mentor move_stop
// against the broker's own order book, because the AddOn replies with NO ack on
// a refused move and a nil wire return only proves the frame was SENT.
//
// Direction reference (long: stop below entry moves UP toward BE; short: stop
// above entry moves DOWN):
//   - TIGHTER broker stop = closer to the position than wanted (long: above X;
//     short: below X) → adopted at once, NEVER re-sent (a re-send would WIDEN).
//   - WIDER broker stop = further from the position (long: below X; short: above
//     X) → the refusing-NT8 path: re-send once, then adopt + latch.
//
// Named RED mutants each test catches:
//   - drop the tighter check          → TestStopMoveTighterBrokerKeptLong/Short fail
//   - drop the miss-count persistence → TestStopMoveWiderCorrectsBeliefAfterTwoMisses fails
//   - drop the latch                  → TestStopMoveLatchBlocksFurtherMoves fails

func f1StopVerifyTrader(t *testing.T) *AutoTrader {
	t.Helper()
	at := &AutoTrader{
		id:       "t-stop-verify",
		exchange: "ninjatrader",
		config: AutoTraderConfig{
			NinjaTraderSymbol: "MNQ",
			StrategyConfig: &store.StrategyConfig{
				RiskControl: store.RiskControlConfig{MentorMode: true},
			},
		},
	}
	// One single-bracket live position (Legs[0] = the whole position, Wire 1).
	at.mentorLivePos = map[string]*mentorLivePos{
		"sig1": {
			Pos:  mentorPosition{Symbol: "MNQ", Side: "long", Entry: 95, Stop: 90},
			Legs: [2]mentorLeg{{SignalID: "sig1", Qty: 1, Stop: 90, Final: true, Wire: 1}},
		},
	}
	return at
}

// f1SetStopBook substitutes the broker-book seam and returns a restore func.
func f1SetStopBook(book ntwire.OrderSnapshotPayload, recvAt time.Time, ok bool) func() {
	orig := mentorStopBook
	mentorStopBook = func(*ntTrader.TCPTrader) (ntwire.OrderSnapshotPayload, time.Time, bool) {
		return book, recvAt, ok
	}
	return func() { mentorStopBook = orig }
}

// f1CaptureMoves substitutes the wire seam and returns a pointer to the captured
// (signalID, wireLeg, price) re-sends.
func f1CaptureMoves() (*[]moveCapture, func()) {
	var moves []moveCapture
	orig := mentorMoveStopForSignalWire
	mentorMoveStopForSignalWire = func(_ *ntTrader.TCPTrader, signalID, side string, newStop float64, leg int) error {
		moves = append(moves, moveCapture{signalID, side, newStop, leg})
		return nil
	}
	return &moves, func() { mentorMoveStopForSignalWire = orig }
}

type moveCapture struct {
	signalID string
	side     string
	price    float64
	leg      int
}

func f1RecordPending(at *AutoTrader, signalID, side string, wireLeg int, want float64, sendAt time.Time) {
	at.mentorRecordPendingStopMove(signalID, side, wireLeg, want)
	// Rewind the send time so the "first snapshot NEWER than send+2s" gate opens.
	at.mentorExitMu.Lock()
	at.mentorPendingStopMoves[mentorStopMoveKey(signalID, wireLeg)].sendAt = sendAt
	at.mentorExitMu.Unlock()
}

func f1Book(price float64) ntwire.OrderSnapshotPayload {
	return ntwire.OrderSnapshotPayload{Orders: []ntwire.NT8Order{{Name: "sig1-sl", StopPrice: price}}}
}

// TestStopMoveAppliedConfirms: a broker stop within half a tick of the wanted
// price renders CONFIRMED — the pending entry is deleted and nothing re-sends.
func TestStopMoveAppliedConfirms(t *testing.T) {
	at := f1StopVerifyTrader(t)
	moves, restore := f1CaptureMoves()
	defer restore()
	restoreBook := f1SetStopBook(f1Book(100.0), time.Now(), true)
	defer restoreBook()

	f1RecordPending(at, "sig1", "long", 1, 100.0, time.Now().Add(-3*time.Second))

	at.mentorVerifyPendingStopMoves(nil)

	if len(at.mentorPendingStopMoves) != 0 {
		t.Fatalf("pending entry must be deleted on CONFIRMED, still %d present", len(at.mentorPendingStopMoves))
	}
	if len(*moves) != 0 {
		t.Fatalf("no re-send on a confirmed move, got %d", len(*moves))
	}
}

// TestStopMoveTighterBrokerKeptLong: a LONG broker stop ABOVE the wanted price
// (tighter) is adopted at once — zero re-sends, no P0, no widen.
func TestStopMoveTighterBrokerKeptLong(t *testing.T) {
	at := f1StopVerifyTrader(t)
	moves, restore := f1CaptureMoves()
	defer restore()
	restoreBook := f1SetStopBook(f1Book(101.0), time.Now(), true)
	defer restoreBook()

	f1RecordPending(at, "sig1", "long", 1, 100.0, time.Now().Add(-3*time.Second))
	at.mentorVerifyPendingStopMoves(nil)

	if len(*moves) != 0 {
		t.Fatalf("a tighter broker stop must NOT re-send (a re-send would widen), got %d", len(*moves))
	}
	if at.mentorLivePos["sig1"].Legs[0].Stop != 101.0 {
		t.Fatalf("tighter broker stop must be adopted at 101.0, got %.2f", at.mentorLivePos["sig1"].Legs[0].Stop)
	}
	if len(at.mentorPendingStopMoves) != 0 {
		t.Fatalf("pending entry must be deleted on a tighter adopt, got %d", len(at.mentorPendingStopMoves))
	}
}

// TestStopMoveTighterBrokerKeptShort: a SHORT broker stop BELOW the wanted price
// (tighter) is adopted at once — zero re-sends.
func TestStopMoveTighterBrokerKeptShort(t *testing.T) {
	at := f1StopVerifyTrader(t)
	moves, restore := f1CaptureMoves()
	defer restore()
	restoreBook := f1SetStopBook(f1Book(99.0), time.Now(), true)
	defer restoreBook()

	f1RecordPending(at, "sig1", "short", 1, 100.0, time.Now().Add(-3*time.Second))
	at.mentorVerifyPendingStopMoves(nil)

	if len(*moves) != 0 {
		t.Fatalf("a tighter (short) broker stop must NOT re-send, got %d", len(*moves))
	}
	if at.mentorLivePos["sig1"].Legs[0].Stop != 99.0 {
		t.Fatalf("tighter (short) broker stop must be adopted at 99.0, got %.2f", at.mentorLivePos["sig1"].Legs[0].Stop)
	}
}

// TestStopMoveWiderRefusalReSendsOnce: a LONG broker stop BELOW the wanted price
// (wider) renders NOT APPLIED and re-sends the SAME wanted price exactly once.
func TestStopMoveWiderRefusalReSendsOnce(t *testing.T) {
	at := f1StopVerifyTrader(t)
	moves, restore := f1CaptureMoves()
	defer restore()
	restoreBook := f1SetStopBook(f1Book(99.0), time.Now(), true)
	defer restoreBook()

	f1RecordPending(at, "sig1", "long", 1, 100.0, time.Now().Add(-3*time.Second))
	at.mentorVerifyPendingStopMoves(nil)

	if len(*moves) != 1 {
		t.Fatalf("first wider refusal must re-send ONCE, got %d re-sends", len(*moves))
	}
	if (*moves)[0].price != 100.0 {
		t.Fatalf("re-send must be the SAME wanted price 100.0, got %.2f", (*moves)[0].price)
	}
	if len(at.mentorPendingStopMoves) != 1 {
		t.Fatalf("pending entry must survive the first wider refusal, got %d", len(at.mentorPendingStopMoves))
	}
}

// TestStopMoveWiderShortRefusalReSendsOnce: a SHORT broker stop ABOVE the wanted
// price (wider) re-sends once.
func TestStopMoveWiderShortRefusalReSendsOnce(t *testing.T) {
	at := f1StopVerifyTrader(t)
	moves, restore := f1CaptureMoves()
	defer restore()
	restoreBook := f1SetStopBook(f1Book(101.0), time.Now(), true)
	defer restoreBook()

	f1RecordPending(at, "sig1", "short", 1, 100.0, time.Now().Add(-3*time.Second))
	at.mentorVerifyPendingStopMoves(nil)

	if len(*moves) != 1 {
		t.Fatalf("first wider (short) refusal must re-send ONCE, got %d", len(*moves))
	}
}

// TestStopMoveWiderCorrectsBeliefAfterTwoMisses: the SECOND wider miss adopts the
// broker's price and latches the leg (the miss count SURVIVES a superseding
// move, so a fresh want every candle still reaches manage-by-hand).
func TestStopMoveWiderCorrectsBeliefAfterTwoMisses(t *testing.T) {
	at := f1StopVerifyTrader(t)
	moves, restore := f1CaptureMoves()
	defer restore()
	restoreBook := f1SetStopBook(f1Book(99.0), time.Now(), true)
	defer restoreBook()

	f1RecordPending(at, "sig1", "long", 1, 100.0, time.Now().Add(-3*time.Second))
	at.mentorVerifyPendingStopMoves(nil) // first wider miss → re-send

	// A fresh drive move supersedes the pending (new want 101) but the episode
	// miss count SURVIVES — the second wider miss must still adopt + latch.
	f1RecordPending(at, "sig1", "long", 1, 101.0, time.Now().Add(-3*time.Second))
	at.mentorVerifyPendingStopMoves(nil) // second wider miss → adopt + latch

	if len(*moves) != 1 {
		t.Fatalf("exactly ONE re-send total, got %d", len(*moves))
	}
	if len(at.mentorPendingStopMoves) != 0 {
		t.Fatalf("pending entry must be deleted after the second miss, got %d", len(at.mentorPendingStopMoves))
	}
	if at.mentorLivePos["sig1"].Legs[0].Stop != 99.0 {
		t.Fatalf("belief must be corrected to the broker stop 99.0, got %.2f", at.mentorLivePos["sig1"].Legs[0].Stop)
	}
	if _, latched := at.mentorStopMoveManual[mentorStopMoveKey("sig1", 1)]; !latched {
		t.Fatal("the leg must be latched into manual management after two misses")
	}
}

// TestStopMoveLatchBlocksFurtherMoves: once latched, a further drive move sends
// NOTHING (mentorMoveLegStop returns errMentorStopManual) — no fourth send.
func TestStopMoveLatchBlocksFurtherMoves(t *testing.T) {
	at := f1StopVerifyTrader(t)
	moves, restore := f1CaptureMoves()
	defer restore()
	restoreBook := f1SetStopBook(f1Book(99.0), time.Now(), true)
	defer restoreBook()

	// Want 100 → first wider miss (re-send); want 101 → second wider miss (adopt+latch).
	f1RecordPending(at, "sig1", "long", 1, 100.0, time.Now().Add(-3*time.Second))
	at.mentorVerifyPendingStopMoves(nil)
	f1RecordPending(at, "sig1", "long", 1, 101.0, time.Now().Add(-3*time.Second))
	at.mentorVerifyPendingStopMoves(nil)

	if len(*moves) != 1 {
		t.Fatalf("exactly ONE re-send total, got %d", len(*moves))
	}

	// A third drive move (want 102) must be blocked by the latch — no send.
	leg := &mentorLeg{SignalID: "sig1", Qty: 1, Stop: 99, Final: true, Wire: 1}
	err := at.mentorMoveLegStop(nil, "long", leg, 102.0)
	if !errors.Is(err, errMentorStopManual) {
		t.Fatalf("a latched leg must return errMentorStopManual, got %v", err)
	}
	if len(*moves) != 1 {
		t.Fatalf("the latch must block further sends, got %d sends", len(*moves))
	}
	if at.mentorLivePos["sig1"].Legs[0].Stop != 99.0 {
		t.Fatalf("belief must stay at the broker stop 99.0, got %.2f", at.mentorLivePos["sig1"].Legs[0].Stop)
	}
}

// TestStopMoveStaleBookNoVerdict: a book older than snapshotMaxAge (2N) renders
// no verdict — the pending entry stays, nothing re-sends.
func TestStopMoveStaleBookNoVerdict(t *testing.T) {
	at := f1StopVerifyTrader(t)
	moves, restore := f1CaptureMoves()
	defer restore()
	// Snapshot received 61s ago (older than 2×30s) — but still AFTER send+2s.
	restoreBook := f1SetStopBook(f1Book(99.0), time.Now().Add(-61*time.Second), true)
	defer restoreBook()

	f1RecordPending(at, "sig1", "long", 1, 100.0, time.Now().Add(-70*time.Second))
	at.mentorVerifyPendingStopMoves(nil)

	if len(at.mentorPendingStopMoves) != 1 {
		t.Fatalf("stale book must leave the pending entry, got %d", len(at.mentorPendingStopMoves))
	}
	if len(*moves) != 0 {
		t.Fatalf("no re-send on a stale book, got %d", len(*moves))
	}
	if at.mentorLivePos["sig1"].Legs[0].Stop != 90.0 {
		t.Fatalf("belief must be unchanged on a stale book, got %.2f", at.mentorLivePos["sig1"].Legs[0].Stop)
	}
}
