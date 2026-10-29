package ninjatrader

import (
	"strings"
	"sync"
	"testing"

	"vl/internal/ownerpush"
	ntwire "vl/provider/ninjatrader"
)

// m442 item 6, the stop-widen REFUSED site (the 95 refused break-evens since
// 10-23 were this refusal, logged one line at a time for six days). Driven
// through the production entry points (MoveStopToBreakeven, MoveStopForSignalLeg).
// MUTANT (shown RED): drop the ownerpush.NoteRefusal line at either site -> the
// third refusal pushes nothing -> RED.

type stormSink struct {
	mu   sync.Mutex
	msgs []string
}

func (s *stormSink) send(text string) { s.mu.Lock(); s.msgs = append(s.msgs, text); s.mu.Unlock() }
func (s *stormSink) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.msgs...)
}

func TestM442StopWidenRefusedThreeTimesPushesOneStorm(t *testing.T) {
	sink := &stormSink{}
	p := ownerpush.New(sink.send)
	t.Cleanup(ownerpush.Use(p))

	s := ntwire.NewTCPServer(nil)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.mu.Lock()
	tr.traderID = "8d5c8af5_396db319-d3fe-4d63-9b97-516ff0008f53_deepseek_0751c0b6_1792604566" // a real-shaped 65+ char id
	tr.lastEntrySignalID = "sig-1"
	tr.mu.Unlock()
	_ = tr.SetStopLoss("MNQ", "long", 1, 29900)

	for i := 0; i < 2; i++ {
		if err := tr.MoveStopToBreakeven("long", 29850); err == nil {
			t.Fatal("a widening move must be refused")
		}
	}
	p.Flush()
	if got := sink.all(); len(got) != 0 {
		t.Fatalf("two refusals are below the storm threshold, got %q", got)
	}
	if err := tr.MoveStopToBreakeven("long", 29850); err == nil {
		t.Fatal("a widening move must be refused")
	}
	p.Flush()
	got := sink.all()
	if len(got) != 1 {
		t.Fatalf("the third refusal must push exactly ONE storm message, got %q", got)
	}
	for _, want := range []string{"stop-widen REFUSED", "3 times in 10 min", "MNQ long", "29850.00", "29900.00"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("storm text %q lacks %q", got[0], want)
		}
	}
	if strings.Contains(got[0], "[redacted]") {
		t.Errorf("the 65-char trader id was printed as [redacted]: %q", got[0])
	}
	// a 4th refusal in the same episode: still ONE message
	_ = tr.MoveStopToBreakeven("long", 29850)
	p.Flush()
	if n := len(sink.all()); n != 1 {
		t.Fatalf("one storm push per episode, got %d", n)
	}
}

func TestM442StopWidenPerSignalSiteAlsoCounts(t *testing.T) {
	sink := &stormSink{}
	p := ownerpush.New(sink.send)
	t.Cleanup(ownerpush.Use(p))

	s := ntwire.NewTCPServer(nil)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	tr.mu.Lock()
	tr.traderID = "trader-x"
	if tr.stopBySignal == nil {
		tr.stopBySignal = map[string]float64{}
	}
	tr.stopBySignal["sig-77"] = 29900
	tr.mu.Unlock()

	for i := 0; i < 3; i++ {
		if err := tr.MoveStopForSignal("sig-77", "long", 29850); err == nil || !strings.Contains(err.Error(), "stop-widen ban") {
			t.Fatalf("a widening per-signal move must be refused with the ban, got %v", err)
		}
	}
	p.Flush()
	got := sink.all()
	if len(got) != 1 || !strings.Contains(got[0], "sig-77") {
		t.Fatalf("want ONE storm push naming the signal, got %q", got)
	}
}
