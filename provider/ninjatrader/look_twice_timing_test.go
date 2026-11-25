package ninjatrader

import (
	"net"
	"testing"
	"time"
)

// ── LOOK TWICE, the VALUES of its timing constants (wire half) ───────────────
//
// Every number here is absolute on purpose. A test that computes its boundary from
// the constant it tests follows a changed constant and stays green.

// positionRegrowProof is 12 s. MUTANTS: 10 s, 15 s.
func TestLookTwiceTiming_PositionRegrowProof(t *testing.T) {
	short := []OpenPosition{{Symbol: "MNQ", Side: "short", Quantity: 1, AvgPrice: 100}}
	for after, want := range map[time.Duration]bool{11900 * time.Millisecond: true, 12100 * time.Millisecond: false} {
		s := NewTCPServer(nil)
		now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
		s.SetReceiptClockForTest(func() time.Time { return now })
		put := func(d time.Duration, ps []OpenPosition) {
			now = now.Add(d)
			s.SeedPositionsForTestAt("Sim101", ps, now)
		}
		put(0, short)
		put(time.Second, []OpenPosition{})
		put(time.Millisecond, short) // grown with no entry fill
		put(after, short)
		if got := s.ProtectionEvidenceFor("Sim101", "MNQ").Unproven("MNQ", "SHORT"); got != want {
			t.Errorf("the same size repeated %s later: Unproven = %v, want %v", after, got, want)
		}
	}
}

// The late-book step rule is 12 s of Go time AND 10 s of stamps; the cap is 90 s of
// three books. MUTANTS: positionRegrowProof 10 s / 15 s, lateBookStepProof 8 s / 11 s,
// lateBookCap 75 s / 110 s, lateBookCapCount 2 / 4.
func TestLookTwiceTiming_LateBook(t *testing.T) {
	type put struct {
		afterMs, emitted int64
	}
	for name, c := range map[string]struct {
		puts []put
		late bool
	}{
		"step, 11.9 s of Go time":       {[]put{{0, 1e9}, {0, 50}, {11_900, 20_050}}, true},
		"step, 12.1 s of Go time":       {[]put{{0, 1e9}, {0, 50}, {12_100, 20_050}}, false},
		"step, 9.9 s of stamps":         {[]put{{0, 1e9}, {0, 50}, {30_000, 9_950}}, true},
		"step, 10.1 s of stamps":        {[]put{{0, 1e9}, {0, 50}, {30_000, 10_150}}, false},
		"three late books, 89.9 s":      {[]put{{0, 1e9}, {0, 9e6}, {45_000, 8e6}, {44_900, 7e6}}, true},
		"three late books, 90.1 s":      {[]put{{0, 1e9}, {0, 9e6}, {45_000, 8e6}, {45_100, 7e6}}, false},
		"two late books, 90.1 s":        {[]put{{0, 1e9}, {0, 9e6}, {90_100, 8e6}}, true},
		"four late books in 89.9 s":     {[]put{{0, 1e9}, {0, 9e6}, {1000, 8e6}, {1000, 7e6}, {87_900, 6e6}}, true},
		"three late books, 90.1 s, 1 s": {[]put{{0, 1e9}, {0, 9e6}, {1000, 8e6}, {89_100, 7e6}}, false},
	} {
		cache, link := NewOrderSnapshotCache(), &net.TCPConn{}
		now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
		for _, p := range c.puts {
			now = now.Add(time.Duration(p.afterMs) * time.Millisecond)
			cache.putFrom(OrderSnapshotPayload{Account: "Sim101", EmittedMs: p.emitted}, now, link)
		}
		if _, _, late, ok := cache.LatestOrder("Sim101"); !ok || late != c.late {
			t.Errorf("%s: late = %v (ok %v), want %v", name, late, ok, c.late)
		}
	}
}

// exitPairWindow is 10 s: the close of a fill its order_update already stamped is the
// same fill up to there. MUTANTS: 7 s, 15 s.
func TestLookTwiceTiming_ExitPairWindow(t *testing.T) {
	r := newExitFenceRig(t)
	r.ou(1000, "a-sl", "filled", 2)
	r.pc(1000+9900, "a", "sl", 0, 2, "t1")
	r.want(1000, "9.9 s after its order_update the close is the same fill")
	r.ou(20_000, "b-sl", "filled", 2)
	r.pc(20_000+10_100, "b", "sl", 0, 2, "t2")
	r.want(30_100, "10.1 s after it the close stamps again")
}

// deadlineConn records how far ahead the write deadline was set. before is taken by
// the test before the send, so ahead is never less than what the code added.
type deadlineConn struct {
	net.Conn
	got []time.Time
}

func (d *deadlineConn) SetWriteDeadline(at time.Time) error { d.got = append(d.got, at); return nil }
func (d *deadlineConn) Write(b []byte) (int, error)         { return len(b), nil }
func (d *deadlineConn) Close() error                        { return nil }

// The link check and both guarded order frames may hold the socket's writer for 5 s.
// MUTANTS: 2 s, 10 s, at each of the three.
func TestLookTwiceTiming_WriteDeadlines(t *testing.T) {
	for name, send := range map[string]func(*TCPServer) error{
		"link check": func(s *TCPServer) error { _, err := s.SendLinkProbe(); return err },
		"close": func(s *TCPServer) error {
			return s.SendClosePositionIf(ClosePositionPayload{Symbol: "MNQ", Side: "short", SignalID: "c", Account: "Sim101"}, nil)
		},
		"stop": func(s *TCPServer) error {
			return s.SendPlaceProtectiveStopIf(PlaceProtectiveStopPayload{Symbol: "MNQ", SignalID: "p", PositionSide: "SHORT", Quantity: 1, StopPrice: 1, Account: "Sim101"}, nil)
		},
	} {
		s, c := NewTCPServer(nil), &deadlineConn{}
		s.connMu.Lock()
		s.conn, s.hbLink = c, newHeartbeatLink()
		s.connMu.Unlock()
		before := time.Now()
		err := send(s)
		after := time.Now()
		if err != nil || len(c.got) != 1 {
			t.Fatalf("%s: err %v, %d deadlines", name, err, len(c.got))
		}
		if lo, hi := c.got[0].Sub(before), c.got[0].Sub(after); lo < 5*time.Second || hi > 5*time.Second {
			t.Errorf("%s: the write deadline is %s..%s ahead, want 5 s", name, hi, lo)
		}
	}
}
