package ninjatrader

import (
	"fmt"
	"testing"
	"time"
)

// look-twice C — what WE sent, per account+symbol, bounded, and forgotten when
// the write failed.
func TestOwnCloseLog(t *testing.T) {
	s := &TCPServer{}
	at := time.Date(2026, 11, 19, 12, 47, 30, 0, time.UTC)
	s.SetReceiptClockForTest(func() time.Time { return at })
	p := ClosePositionPayload{Symbol: "MNQ", Account: "Sim101", SignalID: "m"}
	s.noteOwnClose(p)
	p.SignalID, p.LimitPrice = "l", 27750
	forget := s.noteOwnClose(p)
	got := s.OwnCloses("Sim101", "MNQ")
	if len(got) != 2 || !got[0].Market || got[0].SignalID != "m" || !got[0].SentAt.Equal(at) || got[1].Market {
		t.Fatalf("log = %+v", got)
	}
	if o := s.OwnCloses("Sim102", "MNQ"); len(o) != 0 {
		t.Fatalf("another account: %+v", o)
	}
	forget()
	if got = s.OwnCloses("Sim101", "MNQ"); len(got) != 1 || got[0].SignalID != "m" {
		t.Fatalf("a failed write must be forgotten: %+v", got)
	}
	s.noteOwnClose(ClosePositionPayload{Symbol: "MNQ", SignalID: "blank"}) // no account: cannot be attributed
	for i := 0; i < ownCloseLogCap+5; i++ {
		s.noteOwnClose(ClosePositionPayload{Symbol: "MNQ", Account: "Sim101", SignalID: fmt.Sprint(i)})
	}
	if got = s.OwnCloses("Sim101", "MNQ"); len(got) != ownCloseLogCap || got[len(got)-1].SignalID != fmt.Sprint(ownCloseLogCap+4) {
		t.Fatalf("bounded log: %d %+v", len(got), got[len(got)-1])
	}
}

// A close that could not be sent (no connection) leaves no record.
func TestOwnCloseLogNoClient(t *testing.T) {
	s := &TCPServer{}
	if err := s.SendClosePosition(ClosePositionPayload{Symbol: "MNQ", Account: "Sim101", SignalID: "m"}); err == nil {
		t.Fatal("want an error")
	}
	if got := s.OwnCloses("Sim101", "MNQ"); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}
