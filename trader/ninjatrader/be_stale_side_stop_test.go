// m440 (system-check 2026-10-29, N1) — a stale side-keyed stop refused every
// later break-even on that side.
//
// LIVE: pos 712 (10-28 22:26 long @29522) wanted BE 29522, but t.stopLoss
// ["MNQ:LONG"] still held 29700 from an EARLIER long's BE, so every BE was
// refused as a "widen" for ~85 min (log_events 342096-342140; 95 such rows
// since 10-23, e.g. 316921/316925 on pos 696). The armed (limit / stop) entry
// paths never wrote the side key and nothing cleared it on close.
//
// These pins drive PRODUCTION entry points end to end (PlaceLimitEntry /
// PlaceStopEntry -> MoveStopToBreakeven -> the move_stop frame on the wire,
// and a position_close frame through the real close-sync goroutine). The
// real-data replay (the 10-23 and 10-28 sequences, numbers from log_events)
// is TestN1RealDayReplay below.
package ninjatrader

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

type n1Wire struct {
	s     *ntwire.TCPServer
	st    *store.Store
	conn  net.Conn
	sig   chan ntwire.SignalPayload
	moves chan ntwire.MoveStopPayload
}

func n1Fixture(t *testing.T) *n1Wire {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "n1.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel(); _ = st.Close() })
	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	w := &n1Wire{s: s, st: st, conn: conn,
		sig: make(chan ntwire.SignalPayload, 16), moves: make(chan ntwire.MoveStopPayload, 16)}
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			switch env.Type {
			case ntwire.FrameSignal:
				var p ntwire.SignalPayload
				if json.Unmarshal(env.Payload, &p) == nil {
					w.sig <- p
				}
			case ntwire.FrameMoveStop:
				var p ntwire.MoveStopPayload
				if json.Unmarshal(env.Payload, &p) == nil {
					w.moves <- p
				}
			}
		}
	}()
	top := ntwire.MinAddonBuildPictureHtf
	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: top}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400 && !ntwire.FarSideProven(s.FarSideBuildID(), top); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !ntwire.FarSideProven(s.FarSideBuildID(), top) {
		t.Fatal("fixture: far-side build never proven")
	}
	return w
}

func (w *n1Wire) wantMove(t *testing.T, sid string, stop float64) {
	t.Helper()
	select {
	case m := <-w.moves:
		if m.SignalID != sid || m.NewStopLoss != stop {
			t.Fatalf("move_stop frame = sig %q stop %.2f, want sig %q stop %.2f", m.SignalID, m.NewStopLoss, sid, stop)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no move_stop frame (want sig %q stop %.2f) — the BE was refused or never sent", sid, stop)
	}
}

func (w *n1Wire) noMove(t *testing.T) {
	t.Helper()
	select {
	case m := <-w.moves:
		t.Fatalf("a move_stop frame reached the wire when it must have been refused: %+v", m)
	case <-time.After(150 * time.Millisecond):
	}
}

func n1Sent(t *testing.T, w *n1Wire) string {
	t.Helper()
	select {
	case p := <-w.sig:
		return p.SignalID
	case <-time.After(2 * time.Second):
		t.Fatal("entry signal never reached the wire")
		return ""
	}
}

// THE INCIDENT, end to end through production call sites. Long #1 is moved to
// BE high (29700) and (adversarially) its position is NEVER reported closed;
// long #2 arrives via a different armed placer and wants a LOWER BE (29522):
// it must be SENT. Mutant (BE reads the bare side key again): RED.
func TestN1_SecondLongBEBelowFirstLongsStopIsSent(t *testing.T) {
	w := n1Fixture(t)
	tr := NewTCPTrader(w.s, "MNQ", "Sim101")

	if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29600, 29300, 29900); err != nil {
		t.Fatalf("entry 1: %v", err)
	}
	sid1 := n1Sent(t, w)
	if err := tr.MoveStopToBreakeven("long", 29700); err != nil {
		t.Fatalf("BE 1: %v", err)
	}
	w.wantMove(t, sid1, 29700)

	// Entry 2 (stop-entry placer: different B3 key), same side, same trader.
	if _, err := tr.PlaceStopEntry("MNQ", "long", 1, 29522, 29480, 29800, 0, 0, 0, 0); err != nil {
		t.Fatalf("entry 2: %v", err)
	}
	sid2 := n1Sent(t, w)
	if sid2 == sid1 {
		t.Fatal("fixture: the two entries share a signal id")
	}
	if err := tr.MoveStopToBreakeven("long", 29522); err != nil {
		t.Fatalf("pos 712 shape: BE 29522 for the second long was refused (stale side-keyed stop 29700): %v", err)
	}
	w.wantMove(t, sid2, 29522)
}

// A genuine widen on the SAME position is still refused (B1 stays): after the
// second long's BE to 29522, 29500 widens it; and a BE below the entry's own
// wire stop (29480) widens too, before any BE was sent.
func TestN1_RealWidenOnTheSamePositionStillRefused(t *testing.T) {
	w := n1Fixture(t)
	tr := NewTCPTrader(w.s, "MNQ", "Sim101")
	if _, err := tr.PlaceStopEntry("MNQ", "long", 1, 29522, 29480, 29800, 0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	sid := n1Sent(t, w)

	// (a) below the entry's own stop, no BE yet.
	if err := tr.MoveStopToBreakeven("long", 29470); err == nil {
		t.Fatal("a stop below the entry's own 29480 stop must be refused as a widen")
	}
	w.noMove(t)
	// (b) a tighten, then a widen from it.
	if err := tr.MoveStopToBreakeven("long", 29522); err != nil {
		t.Fatalf("BE: %v", err)
	}
	w.wantMove(t, sid, 29522)
	if err := tr.MoveStopToBreakeven("long", 29500); err == nil {
		t.Fatal("29500 widens the position's own 29522 stop — must be refused")
	}
	w.noMove(t)
}

// SHORT twin (mirror image of the incident).
func TestN1_SecondShortBEAboveFirstShortsStopIsSent(t *testing.T) {
	w := n1Fixture(t)
	tr := NewTCPTrader(w.s, "MNQ", "Sim101")
	if _, err := tr.PlaceLimitEntry("MNQ", "short", 1, 29400, 29700, 29000); err != nil {
		t.Fatal(err)
	}
	sid1 := n1Sent(t, w)
	if err := tr.MoveStopToBreakeven("short", 29400); err != nil {
		t.Fatal(err)
	}
	w.wantMove(t, sid1, 29400)
	if _, err := tr.PlaceStopEntry("MNQ", "short", 1, 29500, 29550, 29100, 0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	sid2 := n1Sent(t, w)
	if err := tr.MoveStopToBreakeven("short", 29500); err != nil {
		t.Fatalf("second short BE 29500 refused against the first short's 29400: %v", err)
	}
	w.wantMove(t, sid2, 29500)
}

// The close half of the fix, through the REAL close-sync goroutine: a whole
// position_close (leg 0) for the owning signal drops the side key; a split-leg
// close does not (a runner is alive); a close naming ANOTHER signal does not.
func TestN1_PositionCloseFrameClearsTheOwnersSideStopOnly(t *testing.T) {
	w := n1Fixture(t)
	tr := NewTCPTrader(w.s, "MNQ", "Sim101")
	tr.StartCloseSync("n1-trader", "fixture", "ninjatrader", w.st)

	if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29600, 29300, 29900); err != nil {
		t.Fatal(err)
	}
	sid := n1Sent(t, w)
	if err := tr.MoveStopToBreakeven("long", 29700); err != nil {
		t.Fatal(err)
	}
	w.wantMove(t, sid, 29700)

	sideStop := func() (float64, bool) {
		stops, _ := tr.EntryBracketMapsForTest()
		v, ok := stops["MNQ:LONG"]
		return v, ok
	}
	send := func(p ntwire.PositionClosePayload) {
		p.Symbol, p.PositionSide, p.Account = "MNQ", "long", "Sim101"
		p.Quantity, p.ExitPrice, p.ExitReason = 1, 29700, "sl"
		p.ExitTime = time.Now().UTC().Format(time.RFC3339)
		if err := ntwire.WriteFrame(w.conn, ntwire.FramePositionClose, p); err != nil {
			t.Fatal(err)
		}
	}
	settle := func() { time.Sleep(200 * time.Millisecond) }

	send(ntwire.PositionClosePayload{SignalID: sid, Leg: 1}) // a leg: runner alive
	settle()
	if v, ok := sideStop(); !ok || v != 29700 {
		t.Fatalf("a leg-1 close must NOT clear the side stop (runner alive); got %v ok=%v", v, ok)
	}
	send(ntwire.PositionClosePayload{SignalID: "some-other-signal"}) // not the owner
	settle()
	if v, ok := sideStop(); !ok || v != 29700 {
		t.Fatalf("another signal's close must NOT clear this side stop; got %v ok=%v", v, ok)
	}
	send(ntwire.PositionClosePayload{SignalID: sid}) // whole close, the owner
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := sideStop(); !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the owning signal's whole position_close must clear the side-keyed stop")
}

// The legacy shape stays judged by the side key: SetStopLoss -> OpenWithBracket
// binds the stop to the entry, and a BE for it is checked against it.
func TestN1_LegacyBracketEntryStopStillBansAWiden(t *testing.T) {
	w := n1Fixture(t)
	tr := NewTCPTrader(w.s, "MNQ", "Sim101")
	if _, err := tr.OpenWithBracket("MNQ", "long", 1, 29480, 29800); err != nil {
		t.Fatalf("entry: %v", err)
	}
	sid := n1Sent(t, w)
	if err := tr.MoveStopToBreakeven("long", 29470); err == nil {
		t.Fatal("below the market entry's own 29480 stop must be refused as a widen")
	}
	if err := tr.MoveStopToBreakeven("long", 29522); err != nil {
		t.Fatalf("a tighten must be sent: %v", err)
	}
	w.wantMove(t, sid, 29522)
}

// REAL-DATA REPLAY. The two refusal clusters in log_events (read-only DB):
//
//	10-23 13:42:15 id 316921: "MNQ LONG new stop 29032.00 would WIDEN from 29181.00"
//	10-29 04:49:17 id 342096: "MNQ LONG new stop 29522.00 widens from 29700.00"
//
// Real sequence for 10-28/29 (log_events): row 706 long 🎯 auto-breakeven to
// 29700.00 SENT (id 341258, 00:42:36 UTC) -> row 706 closed sl at 29700.00 as
// a whole close (id 341281, 01:00:38) -> pos 712 long refused 29522 vs 29700
// from 03:38:27 on (ids 341830-341833, ...342140). The entry-side stops used
// below are synthesized (the log keeps the BE numbers, not the bracket).
// Each case is (the older long's BE, the next long's BE). Replayed through the
// production placers + MoveStopToBreakeven; the 76 + 19 = 95 logged refusals
// are exactly the retries of these two second-long BEs.
func TestN1RealDayReplay(t *testing.T) {
	for _, c := range []struct {
		name         string
		firstBE, be2 float64
		entry2Stop   float64
	}{
		{"10-28 pos 712 (log 342096-342140)", 29700, 29522, 29480},
		{"10-23 pos 696 (log 316921-316925)", 29181, 29032, 28980},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := n1Fixture(t)
			tr := NewTCPTrader(w.s, "MNQ", "Sim101")
			if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, c.firstBE-100, c.firstBE-400, c.firstBE+300); err != nil {
				t.Fatal(err)
			}
			sid1 := n1Sent(t, w)
			if err := tr.MoveStopToBreakeven("long", c.firstBE); err != nil {
				t.Fatal(err)
			}
			w.wantMove(t, sid1, c.firstBE)
			if _, err := tr.PlaceStopEntry("MNQ", "long", 1, c.be2, c.entry2Stop, c.be2+300, 0, 0, 0, 0); err != nil {
				t.Fatal(err)
			}
			sid2 := n1Sent(t, w)
			for i := 0; i < 3; i++ { // the minute-by-minute retries seen live
				if err := tr.MoveStopToBreakeven("long", c.be2); err != nil {
					t.Fatalf("retry %d refused: %v", i, err)
				}
				w.wantMove(t, sid2, c.be2)
			}
		})
	}
}

// Layer isolation: NO armed entry is sent by this process after long #1 (a
// restart / reconcile-materialized position, or an NT8-side entry), so nothing
// re-seeds the side key. The materialized position's signal (cached by
// rememberEntryOrderID, lastEntrySignalID empty) wants BE 29522; the side key
// still holds long #1's 29700 owned by sid1. The reference must be
// "unknown" for the new signal, not 29700.
func TestN1_MaterializedPositionIsNotJudgedByAnEarlierSignalsStop(t *testing.T) {
	w := n1Fixture(t)
	tr := NewTCPTrader(w.s, "MNQ", "Sim101")
	if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29600, 29300, 29900); err != nil {
		t.Fatal(err)
	}
	sid1 := n1Sent(t, w)
	if err := tr.MoveStopToBreakeven("long", 29700); err != nil {
		t.Fatal(err)
	}
	w.wantMove(t, sid1, 29700)

	tr.mu.Lock()
	tr.lastEntrySignalID = "" // long #1 is gone; nothing in this process entered since
	tr.mu.Unlock()
	tr.rememberEntryOrderID("MNQ", "long", "sig-materialized")
	if err := tr.MoveStopToBreakeven("long", 29522); err != nil {
		t.Fatalf("BE for a materialized position refused against the earlier long's 29700: %v", err)
	}
	w.wantMove(t, "sig-materialized", 29522)
	// and it still bans a widen of ITS OWN stop afterwards
	if err := tr.MoveStopToBreakeven("long", 29500); err == nil {
		t.Fatal("a widen of the materialized position's own 29522 BE must be refused")
	}
}
