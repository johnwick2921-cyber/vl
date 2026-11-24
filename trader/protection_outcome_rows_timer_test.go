package trader

import (
	"fmt"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── LOOK TWICE, review of 92f14deac, findings 2-4 ────────────────────────────

// Finding 2 — the owner was told a stop "could NOT be sent — set one by hand".
// When the bot's retry then sends it, he must hear THAT (a second stop beside it
// can double-exit). Once; the repeats of one outcome stay one P0.
// MUTANTS: drop the ":sent" P0; emit it on every sent repeat.
func TestProtectionPlaceRetryThatSendsTellsTheOwner(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{})
	tr := &placeTrader{err: fmt.Errorf("no build")}
	at.id, at.exchange, at.trader = "pl-retry", "ninjatrader", tr
	t.Cleanup(func() { at.sweepStaleProtectionState(nil) })
	v := protectionVerdict{Action: protectionPlace, StopPx: 31163, NeedQty: 1, Source: "plan", Why: "naked."}
	t0, pass := time.Now(), 0
	act := func() {
		at.actOnProtection(v, "MNQ", "LONG", 1, "sig", "t", 0, t0.Add(time.Duration(pass)*time.Minute))
		pass++
	}
	titles := func() (out []string) {
		all, err := st.Alert().List(at.id, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range all {
			out = append(out, a.Title)
		}
		return out
	}
	act()
	act()
	if got := titles(); len(got) != 1 || !strings.Contains(got[0], "could NOT be sent") {
		t.Fatalf("two refusals = one P0, got %q", got)
	}
	tr.err = nil
	act()
	act()
	got := titles()
	if len(got) != 2 || !strings.Contains(strings.Join(got, "|"), "now SENT") || !strings.Contains(strings.Join(got, "|"), "do not add another") {
		t.Fatalf("the retry was sent = ONE second P0 that says so, got %q", got)
	}
	// a NEW episode that is sent at once has no 'now SENT' of its own
	at.actOnProtection(protectionVerdict{Action: protectionOK}, "MNQ", "LONG", 1, "sig", "t", 0, t0.Add(10*time.Minute))
	pass = 11
	act()
	act()
	if got = titles(); len(got) != 3 {
		t.Fatalf("a new episode sent at once = one P0, got %q", got)
	}
	// … not even when the episode before it ended on a refusal
	ok := func() {
		at.actOnProtection(protectionVerdict{Action: protectionOK}, "MNQ", "LONG", 1, "sig", "t", 0, t0.Add(20*time.Minute))
	}
	ok()
	tr.err = fmt.Errorf("no build")
	pass = 21
	act()
	ok()
	tr.err = nil
	act()
	if got = titles(); len(got) != 5 {
		t.Fatalf("refused, recovered, then sent in a NEW episode = two P0s and no 'now SENT', got %q", got)
	}
}

// Finding 2, the flatten half.
func TestProtectionFlattenRetryThatSendsTellsTheOwner(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{})
	tr := &closeFailTrader{err: fmt.Errorf("no NT client connected")}
	at.id, at.exchange, at.trader = "fl-retry", "ninjatrader", tr
	t.Cleanup(func() { at.sweepStaleProtectionState(nil) })
	v := protectionVerdict{Action: protectionFlatten, StopPx: 31163, NeedQty: 1, Why: "through."}
	t0 := time.Now()
	for i := 0; i < 5; i++ { // refused, refused, sent, in flight, in flight
		if i == 2 {
			tr.err = nil
		}
		at.actOnProtection(v, "MNQ", "LONG", 1, "sig", "t", 0, t0.Add(time.Duration(i)*time.Second))
	}
	all, err := st.Alert().List(at.id, 50)
	if err != nil {
		t.Fatal(err)
	}
	failed, sent := 0, 0
	for _, a := range all {
		if strings.Contains(a.Title, "could NOT be sent") {
			failed++
		}
		if strings.Contains(a.Title, "now SENT") && strings.Contains(a.Title, "do not close it again") {
			sent++
		}
	}
	if len(all) != 2 || failed != 1 || sent != 1 {
		t.Fatalf("want one 'could NOT be sent' and one 'now SENT', got %+v", all)
	}
}

// Finding 3 — two rows of one symbol and side: ONE P0 a session day that names
// them, and a stop never larger than the bot's own open row (row 1 of 27733.50).
// MUTANTS: drop the P0; drop the cap.
func TestLookTwiceTwoRows_P0AndNoLargerStopThanOurRow(t *testing.T) {
	fastConfirm(t)
	w, sid := nakedShort(t, "lt-two-cap", 27740)
	w.mentorStop(sid, "short", 1, 0, stop1193)
	// the other month comes FIRST in the frame, so it is acted on first
	w.positions(short(2, 27500), short(1, 27733.50))
	w.sync()
	w.jump(10 * time.Second)
	w.beat30() // it is really there
	w.sync()
	w.cancelStop(sid)
	for i := 0; i < 2; i++ {
		w.look()
	}
	if out, took := w.waitOrders(1, 8*time.Second); len(out) != 1 || !strings.Contains(out[0], `"quantity":1,`) {
		t.Fatalf("two rows: want one stop of 1 (our own row), got %v after %s", out, took)
	}
	n := 0
	for _, a := range w.p0s() {
		if a.Kind == alertKindProtectionTwoRows {
			n++
			if !strings.Contains(a.Body, "×1") || !strings.Contains(a.Body, "×2") || !strings.Contains(a.Title, "MNQ") {
				t.Errorf("the P0 must name both rows: %q %q", a.Title, a.Body)
			}
		}
	}
	if n != 1 {
		t.Fatalf("two rows = one P0, got %d", n)
	}
}

// One row only: no P0 and no cap (a hand add is protected whole, as today).
func TestLookTwiceOneRow_NoTwoRowsP0(t *testing.T) {
	w, _ := nakedShort(t, "lt-one-row", 27740)
	w.positions(short(1, 27733.50), ntwire.OpenPosition{Symbol: "MNQ", Side: "long", Quantity: 1, AvgPrice: 1}, ntwire.OpenPosition{Symbol: "MES", Side: "short", Quantity: 1, AvgPrice: 2})
	w.sync()
	w.look()
	for _, a := range w.p0s() {
		if a.Kind == alertKindProtectionTwoRows {
			t.Fatalf("no two rows here: %q", a.Body)
		}
	}
}

// The tie itself.
func TestProtectionOwnStopQty(t *testing.T) {
	row := func(qty int, avg float64) protRow { return protRow{qty: qty, avg: avg} }
	for name, c := range map[string]struct {
		qty    int
		rows   []protRow
		own    float64
		ownAvg float64
		want   int
	}{
		"tied, larger row":       {2, []protRow{row(1, 100), row(2, 90)}, 1, 100, 1},
		"tied within a tick":     {2, []protRow{row(1, 100.25), row(2, 90)}, 1, 100, 1},
		"tied, our own row":      {1, []protRow{row(1, 100), row(2, 90)}, 1, 100, 1},
		"tied, smaller row":      {1, []protRow{row(1, 90), row(2, 100)}, 2, 100, 1},
		"average off":            {2, []protRow{row(1, 101), row(2, 90)}, 1, 100, 2},
		"quantity off":           {3, []protRow{row(3, 100), row(2, 90)}, 1, 100, 3},
		"no row of ours":         {2, []protRow{row(1, 100), row(2, 90)}, 0, 0, 2},
		"a part contract of own": {2, []protRow{row(1, 100), row(2, 90)}, 0.5, 100, 2},
	} {
		if got := protectionOwnStopQty(c.qty, c.rows, c.own, c.ownAvg, 0.25); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}
}

// MUTANTS: count a manual row; count another account's; drop the weighting.
func TestProtectionOwnOpenRow(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{})
	at.id = "own-row"
	mk := func(sid, side, account, origin string, qty, px float64) {
		p := &store.TraderPosition{TraderID: at.id, ExchangeType: "ninjatrader", Symbol: "MNQ", Side: side, Quantity: qty, EntryQuantity: qty,
			EntryPrice: px, EntryOrderID: sid, EntryTime: 1, Leverage: 1, Status: "OPEN", Source: "t", Account: account, Origin: origin}
		if err := st.Position().Create(p); err != nil {
			t.Fatal(err)
		}
	}
	if got := at.ownOpenRow("Sim101", "MNQ", "SHORT"); got != (protRowSum{}) {
		t.Fatalf("no row: %+v", got)
	}
	mk("a", "SHORT", "Sim101", store.OriginBot, 1, 100)
	mk("b", "SHORT", "", store.OriginBot, 3, 104)
	mk("c", "SHORT", "Sim102", store.OriginBot, 5, 1)
	mk("d", "LONG", "Sim101", store.OriginBot, 5, 1)
	mk("e", "SHORT", "Sim101", store.OriginManual, 5, 1)
	if got := at.ownOpenRow("Sim101", "MNQ", "short"); got.qty != 4 || got.avg != 103 {
		t.Fatalf("our own open position there is 4 @ 103, got %+v", got)
	}
}

// Finding 4 — the re-look runs when the EARLIEST hold is due. A hold opened 1.5 s
// into another one's wait used to be looked at 4.5 s after its first look (at
// the other's next 3 s tick). MUTANT: always wait protectionConfirm().
func TestLookTwiceRelookWaitsForTheEarliestHold(t *testing.T) {
	w, sid := nakedShort(t, "lt-earliest", 27740)
	w.cancelStop(sid)
	w.look() // MNQ
	time.Sleep(1200 * time.Millisecond)
	w.send(ntwire.FrameOrderUpdate, ntwire.OrderUpdatePayload{SignalID: "mes", OrderName: "mes", State: "filled",
		FillPrice: 6000, Quantity: 1, Symbol: "MES", Account: "Sim101"})
	w.positions(short(1, 27733.50), ntwire.OpenPosition{Symbol: "MES", Side: "long", Quantity: 1, AvgPrice: 6000})
	w.sync()
	began := time.Now()
	w.at.reconcileProtectionAt(w.now(), "monitor") // MES, first look
	for {
		mes := false
		for _, a := range w.unprotectedP0s() {
			mes = mes || strings.Contains(a.Body, "MES")
		}
		took := time.Since(began)
		if mes {
			if took < protectionConfirm() || took > protectionConfirm()+900*time.Millisecond {
				t.Fatalf("MES was acted on %s after its first look, want %s plus the link check", took, protectionConfirm())
			}
			return
		}
		if took > 8*time.Second {
			t.Fatal("MES was never acted on")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestProtectionRelookWait(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	const c = 3 * time.Second
	for name, x := range map[string]struct {
		holds []*protectionHold
		want  time.Duration
	}{
		"none":                       {nil, c},
		"one":                        {[]*protectionHold{{firstLook: ago(time.Second), lastLook: ago(time.Second)}}, 2 * time.Second},
		"the earliest of two":        {[]*protectionHold{{firstLook: ago(time.Second), lastLook: ago(0)}, {firstLook: ago(2 * time.Second), lastLook: ago(0)}}, time.Second},
		"due now":                    {[]*protectionHold{{firstLook: ago(4 * time.Second), lastLook: ago(4 * time.Second)}}, protectionRelookFloor},
		"looked at since it was due": {[]*protectionHold{{firstLook: ago(9 * time.Second), lastLook: ago(time.Second)}}, 2 * time.Second},
		"restarted":                  {[]*protectionHold{{lastLook: ago(time.Second)}}, 2 * time.Second},
		"restarted long ago":         {[]*protectionHold{{lastLook: ago(20 * time.Second)}}, protectionRelookFloor},
		"a clock that stepped":       {[]*protectionHold{{firstLook: now.Add(time.Hour), lastLook: now.Add(time.Hour)}}, c},
	} {
		at := &AutoTrader{}
		at.protHolds = map[string]*protectionHold{}
		for i, h := range x.holds {
			at.protHolds[fmt.Sprint(i)] = h
		}
		if got := at.protectionRelookWait(now, c); got != x.want {
			t.Errorf("%s: %s, want %s", name, got, x.want)
		}
	}
}
