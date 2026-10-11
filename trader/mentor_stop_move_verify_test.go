package trader

import (
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
// Named RED mutants each test catches:
//   - drop the re-send          → TestStopMoveRefusalReSendsOnce fails (no re-send captured)
//   - drop the belief correction → TestStopMoveRefusalCorrectsBelief / TestStopMoveTighterBrokerKept fail
//   - accept any price          → TestStopMoveRefusalReSendsOnce fails (would CONFIRM, no re-send)

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

func f1RecordPending(at *AutoTrader, signalID string, wireLeg int, want float64, sendAt time.Time) {
	at.mentorRecordPendingStopMove(signalID, "long", wireLeg, want)
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

	f1RecordPending(at, "sig1", 1, 100.0, time.Now().Add(-3*time.Second))

	at.mentorVerifyPendingStopMoves(nil)

	if len(at.mentorPendingStopMoves) != 0 {
		t.Fatalf("pending entry must be deleted on CONFIRMED, still %d present", len(at.mentorPendingStopMoves))
	}
	if len(*moves) != 0 {
		t.Fatalf("no re-send on a confirmed move, got %d", len(*moves))
	}
}

// TestStopMoveRefusalReSendsOnce: a broker stop at another price renders NOT
// APPLIED and re-sends the SAME wanted price exactly once.
func TestStopMoveRefusalReSendsOnce(t *testing.T) {
	at := f1StopVerifyTrader(t)
	moves, restore := f1CaptureMoves()
	defer restore()
	restoreBook := f1SetStopBook(f1Book(101.0), time.Now(), true)
	defer restoreBook()

	f1RecordPending(at, "sig1", 1, 100.0, time.Now().Add(-3*time.Second))

	at.mentorVerifyPendingStopMoves(nil)

	if len(*moves) != 1 {
		t.Fatalf("first refusal must re-send ONCE, got %d re-sends", len(*moves))
	}
	if (*moves)[0].price != 100.0 {
		t.Fatalf("re-send must be the SAME wanted price 100.0, got %.2f", (*moves)[0].price)
	}
	// The pending entry stays — the re-send needs a verdict too.
	if len(at.mentorPendingStopMoves) != 1 {
		t.Fatalf("pending entry must survive the first refusal, got %d", len(at.mentorPendingStopMoves))
	}
}

// TestStopMoveRefusalCorrectsBelief: the SECOND disagreement adopts the broker's
// price as the driver belief and stops managing the move (no third send).
func TestStopMoveRefusalCorrectsBelief(t *testing.T) {
	at := f1StopVerifyTrader(t)
	moves, restore := f1CaptureMoves()
	defer restore()
	restoreBook := f1SetStopBook(f1Book(101.0), time.Now(), true)
	defer restoreBook()

	f1RecordPending(at, "sig1", 1, 100.0, time.Now().Add(-3*time.Second))
	at.mentorVerifyPendingStopMoves(nil) // first miss → re-send

	// Rewind again: the re-send set sendAt = now, so open the gate for a fresh
	// snapshot that STILL shows the broker at 101.
	at.mentorExitMu.Lock()
	at.mentorPendingStopMoves[mentorStopMoveKey("sig1", 1)].sendAt = time.Now().Add(-3 * time.Second)
	at.mentorExitMu.Unlock()
	at.mentorVerifyPendingStopMoves(nil) // second miss → adopt broker truth

	if len(*moves) != 1 {
		t.Fatalf("exactly ONE re-send total, got %d", len(*moves))
	}
	if len(at.mentorPendingStopMoves) != 0 {
		t.Fatalf("pending entry must be deleted after the second miss, got %d", len(at.mentorPendingStopMoves))
	}
	leg := at.mentorLivePos["sig1"].Legs[0]
	if leg.Stop != 101.0 {
		t.Fatalf("belief must be corrected to the broker stop 101.0, got %.2f", leg.Stop)
	}
}

// TestStopMoveTighterBrokerKept: when the broker sits TIGHTER than the wanted
// price through two misses, the belief adopts the broker's price — never widened
// back to the wanted level.
func TestStopMoveTighterBrokerKept(t *testing.T) {
	at := f1StopVerifyTrader(t)
	moves, restore := f1CaptureMoves()
	defer restore()
	restoreBook := f1SetStopBook(f1Book(99.0), time.Now(), true)
	defer restoreBook()

	f1RecordPending(at, "sig1", 1, 100.0, time.Now().Add(-3*time.Second))
	at.mentorVerifyPendingStopMoves(nil) // first miss → re-send
	at.mentorExitMu.Lock()
	at.mentorPendingStopMoves[mentorStopMoveKey("sig1", 1)].sendAt = time.Now().Add(-3 * time.Second)
	at.mentorExitMu.Unlock()
	at.mentorVerifyPendingStopMoves(nil) // second miss → adopt

	leg := at.mentorLivePos["sig1"].Legs[0]
	if leg.Stop != 99.0 {
		t.Fatalf("tighter broker stop must be KEPT at 99.0, got %.2f", leg.Stop)
	}
	if len(*moves) != 1 {
		t.Fatalf("exactly ONE re-send total, got %d", len(*moves))
	}
}

// TestStopMoveStaleBookNoVerdict: a book older than snapshotMaxAge (2N) renders
// no verdict — the pending entry stays, nothing re-sends.
func TestStopMoveStaleBookNoVerdict(t *testing.T) {
	at := f1StopVerifyTrader(t)
	moves, restore := f1CaptureMoves()
	defer restore()
	// Snapshot received 61s ago (older than 2×30s) — but still AFTER send+2s.
	restoreBook := f1SetStopBook(f1Book(101.0), time.Now().Add(-61*time.Second), true)
	defer restoreBook()

	f1RecordPending(at, "sig1", 1, 100.0, time.Now().Add(-70*time.Second))
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
