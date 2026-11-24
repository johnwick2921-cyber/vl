package trader

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── LOOK TWICE, third review (2026-11-24), findings 1-3 ──────────────────────

// Finding 1 — a re-look that cannot look (UNKNOWN cover, unreadable positions, a
// contained panic) leaves the hold due, and the loop then ran every 10 ms, with
// its database reads, until the hold was 70 s old. It must wait a whole confirm.
// Counted over 5 s at the 1 s confirm: the receive clock is read three times a
// pass. MUTANT: drop lastTry from the wait / stamp it nowhere.
func TestLookTwiceRelookThatCannotLookDoesNotSpin(t *testing.T) {
	for name, spoil := range map[string]func(w *lookWire, sid string){
		"unknown": func(w *lookWire, sid string) {
			w.book("state_change", append([]ntwire.NT8Order{sl(sid+"-sl", stop1193, 1, "Martian")}, w.lastBook...)...)
			w.sync()
		},
		"unreadable positions": func(w *lookWire, _ string) {
			w.protState(func() { w.at.trader = nttrader.NewTCPTrader(w.s, "MNQ", "Sim999") })
		},
		"contained panic": func(w *lookWire, _ string) {
			w.protState(func() {
				market.FuturesBarsProvider = func(string, string, int) []market.Kline { panic("look-twice fixture") }
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			fastConfirm(t)
			w, sid := nakedShort(t, "lt-spin-"+strings.ReplaceAll(name, " ", ""), 27740)
			w.cancelStop(sid)
			w.look()
			spoil(w, sid)
			var reads atomic.Int64
			w.s.SetReceiptClockForTest(func() time.Time { reads.Add(1); return w.now() })
			time.Sleep(5 * time.Second)
			n := reads.Load()
			if n < 9 {
				t.Fatalf("fixture: %d clock reads in 5 s, the re-look must have kept trying", n)
			}
			if n > 30 {
				t.Fatalf("%d clock reads in 5 s (3 a pass): the re-look spins", n)
			}
			w.mustBeSilent(name)
		})
	}
}

// The same when the pass that panics was the one woken by the link check's ack.
// MUTANT: leave h.probe set on a pass that read its ack.
func TestLookTwicePanicAfterTheAckDoesNotSpin(t *testing.T) {
	fastConfirm(t)
	w, sid := nakedShort(t, "lt-spin-ack", 27740)
	w.noAcks()
	w.cancelStop(sid)
	w.look()
	w.secondLook() // the link check is out
	if w.at.outstandingProtectionProbe() == nil {
		t.Fatal("fixture: no link check was sent")
	}
	w.protState(func() {
		market.FuturesBarsProvider = func(string, string, int) []market.Kline { panic("look-twice fixture") }
	})
	var reads atomic.Int64
	w.s.SetReceiptClockForTest(func() time.Time { reads.Add(1); return w.now() })
	w.mu.Lock()
	beats := 0
	for _, e := range w.frames {
		if e.Type == ntwire.FrameHeartbeat {
			beats++
		}
	}
	w.mu.Unlock()
	for i := 0; i < beats; i++ {
		w.send(ntwire.FrameAck, ntwire.AckPayload{Acks: "heartbeat"})
	}
	w.sync()
	time.Sleep(4 * time.Second)
	if n := reads.Load(); n < 6 || n > 40 {
		t.Fatalf("%d clock reads in 4 s (3 a pass), want a pass a second", n)
	}
}

// The stamp is put on the holds that are DUE only: a pass made for one hold must
// not push back another (finding 4 of the second review: 6 s instead of 3).
// MUTANT: stamp every hold.
func TestLookTwiceLastTryIsForDueHoldsOnly(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	s := ntwire.NewTCPServer(nil)
	s.SetReceiptClockForTest(func() time.Time { return now })
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	due := &protectionHold{firstLook: ago(3 * time.Second), lastLook: ago(3 * time.Second)}
	soon := &protectionHold{firstLook: ago(1500 * time.Millisecond), lastLook: ago(1500 * time.Millisecond)}
	at := &AutoTrader{}
	at.protHolds = map[string]*protectionHold{"due": due, "soon": soon}
	at.beginProtectionLookLocked(s, 3*time.Second)
	if !due.lastTry.Equal(now) || !soon.lastTry.IsZero() {
		t.Fatalf("lastTry: due %s, soon %s", due.lastTry, soon.lastTry)
	}
	if got := at.protectionRelookWaitLocked(now, 3*time.Second); got != 1500*time.Millisecond {
		t.Fatalf("the next look is due in 1.5 s, got %s", got)
	}
}

// Finding 2 — "the stop that could NOT be sent is now SENT" is only true when the
// owner was told a stop could not be sent. One episode has ONE first P0 (its event
// id is the episode's), so a refusal after a send is silent - and the next send
// must be silent too. MUTANTS: note every refusal; do not clear it.
func TestProtectionNowSentOnlyAfterATellingRefusal(t *testing.T) {
	titles := func(st *store.Store, id string) (out []string) {
		all, err := st.Alert().List(id, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range all {
			out = append(out, a.Title)
		}
		return out
	}
	no := fmt.Errorf("refused")
	v := protectionVerdict{Action: protectionPlace, StopPx: 31163, NeedQty: 1, Source: "plan", Why: "naked."}
	for name, c := range map[string]struct {
		outcomes []error
		want     []string // one substring per P0, oldest first
	}{
		"sent, refused, sent":                   {[]error{nil, no, nil}, []string{"was sent"}},
		"refused, sent, refused, sent":          {[]error{no, nil, no, nil}, []string{"a protective stop could NOT be sent", "now SENT"}},
		"refused, refused, sent, sent":          {[]error{no, no, nil, nil}, []string{"a protective stop could NOT be sent", "now SENT"}},
		"sent, sent, refused, refused, sent, s": {[]error{nil, nil, no, no, nil, nil}, []string{"was sent"}},
	} {
		at, st := resetTrader(t, store.StrategyConfig{})
		tr := &placeTrader{}
		at.id, at.exchange, at.trader = "now-sent-"+name, "ninjatrader", tr
		t.Cleanup(func() { at.sweepStaleProtectionState(nil) })
		t0 := time.Now()
		for i, err := range c.outcomes {
			tr.err = err
			at.actOnProtection(v, "MNQ", "LONG", 1, "sig", "t", 0, t0.Add(time.Duration(i)*time.Minute))
		}
		got := titles(st, at.id)
		for _, want := range c.want {
			n := 0
			for _, g := range got {
				if strings.Contains(g, want) {
					n++
				}
			}
			if n != 1 {
				t.Errorf("%s: want one %q P0, got %q", name, want, got)
			}
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: want %d P0s, got %q", name, len(c.want), got)
		}
	}
}

// Finding 2, the flatten half: sent, refused (silent), sent = the first P0 only.
func TestProtectionFlattenNowSentOnlyAfterATellingRefusal(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{})
	tr := &closeFailTrader{}
	at.id, at.exchange, at.trader = "fl-now-sent", "ninjatrader", tr
	t.Cleanup(func() { at.sweepStaleProtectionState(nil) })
	v := protectionVerdict{Action: protectionFlatten, StopPx: 31163, NeedQty: 1, Why: "through."}
	t0 := time.Now()
	for i, err := range []error{nil, fmt.Errorf("refused"), nil} {
		tr.err = err
		// past the in-flight hold each time, so every pass really sends
		at.actOnProtection(v, "MNQ", "LONG", 1, "sig", "t", 0, t0.Add(time.Duration(i)*2*exitInFlightHold))
	}
	all, err := st.Alert().List(at.id, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || !strings.Contains(all[0].Title, "a market close was sent") {
		t.Fatalf("want the first P0 only, got %d: %+v", len(all), all)
	}
}

// Finding 2 — the one way a refusal is told AFTER a send of the same episode: the
// first P0's row could not be written. Both P0s must then say a stop is already
// out there. MUTANT: drop the sentence.
func TestProtectionRefusalAfterASendSaysAStopWasSent(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{})
	tr := &placeTrader{}
	at.id, at.exchange, at.trader = "now-sent-earlier", "ninjatrader", tr
	t.Cleanup(func() { at.sweepStaleProtectionState(nil) })
	v := protectionVerdict{Action: protectionPlace, StopPx: 31163, NeedQty: 1, Source: "plan", Why: "naked."}
	var deny atomic.Bool
	if err := st.GormDB().Callback().Create().Before("gorm:create").Register("deny-alerts", func(tx *gorm.DB) {
		if deny.Load() && strings.Contains(tx.Statement.Table, "alert") {
			_ = tx.AddError(errors.New("database is locked"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	act := func(i int, err error) {
		tr.err = err
		at.actOnProtection(v, "MNQ", "LONG", 1, "sig", "t", 0, t0.Add(time.Duration(i)*time.Minute))
	}
	deny.Store(true)
	act(0, nil) // sent; its P0 row is refused
	deny.Store(false)
	if all, _ := st.Alert().List(at.id, 10); len(all) != 0 {
		t.Fatalf("fixture: the alert row must have been refused, got %+v", all)
	}
	act(1, fmt.Errorf("refused"))
	act(2, nil)
	all, err := st.Alert().List(at.id, 10)
	if err != nil || len(all) != 2 {
		t.Fatalf("want the refusal and the 'now SENT', got %d (%v): %+v", len(all), err, all)
	}
	for _, a := range all {
		if !strings.Contains(a.Body, "earlier stop of this episode was already sent") {
			t.Errorf("%q must say a stop is already out there: %q", a.Title, a.Body)
		}
	}
}

// ── Finding 3 — an order book written late ───────────────────────────────────

// bookAt is a book the AddOn stamped at emitted (0 = no stamp).
func (w *lookWire) bookAt(emitted time.Time, orders ...ntwire.NT8Order) {
	ms := int64(0)
	if !emitted.IsZero() {
		ms = emitted.UnixMilli()
	}
	if orders == nil {
		orders = []ntwire.NT8Order{}
	}
	w.send(ntwire.FrameOrderSnapshot, ntwire.OrderSnapshotPayload{Account: "Sim101", EmittedMs: ms, Reason: "state_change", Orders: orders})
}

// The stop is on the newest book; a book built before it, without the stop, is
// written after it. Nothing may be placed or closed beside the working stop.
// MUTANT: take the late book as evidence.
func TestLookTwiceLateBook_IsNotEvidence(t *testing.T) {
	fastConfirm(t)
	for name, px := range map[string]float64{"place": 27740, "flatten": 27755} {
		t.Run(name, func(t *testing.T) {
			w, sid := nakedShort(t, "lt-book-"+name, px)
			covered := w.lastBook
			emitted := w.now()
			w.bookAt(emitted, covered...)
			w.bookAt(emitted.Add(-time.Millisecond), tp(sid+"-tp", 27700, 1, "Working")) // the late one
			w.sync()
			w.mustStaySilent("a late book without the stop")
			w.mustHaveNoP0("before the next book")
			w.jump(10 * time.Second)
			w.book("periodic", covered...)
			w.sync()
			w.look()
			w.mustBeSilent("covered")
			w.mustHaveNoP0("the next book settled it")
			if w.holds() != 0 {
				t.Fatalf("covered ends the hold, holds %d", w.holds())
			}
		})
	}
}

// A look on a late book is not a look: the two looks start on the newer one.
// MUTANT: do not restart the hold.
func TestLookTwiceLateBook_IsNotALook(t *testing.T) {
	fastConfirm(t)
	w, sid := nakedShort(t, "lt-book-look", 27740)
	emitted := w.now()
	w.bookAt(emitted, w.lastBook...)
	w.bookAt(emitted.Add(-time.Millisecond), tp(sid+"-tp", 27700, 1, "Working"))
	w.sync()
	w.look()
	w.jump(5 * time.Second)
	w.book("state_change", tp(sid+"-tp", 27700, 1, "Working"))
	w.sync()
	w.look()
	time.Sleep(300 * time.Millisecond)
	w.actsAfterRestart("a late book, then a newer one")
}

// Really naked: the first newer book lets the bot act.
func TestLookTwiceLateBook_ANewerBookActs(t *testing.T) {
	fastConfirm(t)
	w, sid := nakedShort(t, "lt-book-newer", 27740)
	emitted := w.now()
	w.bookAt(emitted, w.lastBook...)
	w.bookAt(emitted.Add(-40*time.Millisecond), tp(sid+"-tp", 27700, 1, "Working"))
	w.sync()
	w.mustStaySilent("late")
	w.book("state_change", tp(sid+"-tp", 27700, 1, "Working"))
	w.sync()
	if out, took := w.waitOrders(1, 8*time.Second); len(out) != 1 || !strings.HasPrefix(out[0], string(ntwire.FramePlaceProtectiveStop)) {
		t.Fatalf("a newer book shows it naked: it must be protected, got %v after %s", out, took)
	}
}

// No newer book at all: one P0 at 35 s that says why, nothing sent.
func TestLookTwiceLateBook_P0At35s(t *testing.T) {
	fastConfirm(t)
	w, sid := nakedShort(t, "lt-book-p0", 27740)
	emitted := w.now()
	w.bookAt(emitted, w.lastBook...)
	w.bookAt(emitted.Add(-40*time.Millisecond), tp(sid+"-tp", 27700, 1, "Working"))
	w.sync()
	w.look()
	w.jump(34 * time.Second)
	w.look()
	w.mustHaveNoP0("before 35 s")
	w.jump(2 * time.Second)
	w.look()
	if ps := w.p0s(); len(ps) != 1 || ps[0].Kind != alertKindProtectionHeld || !strings.Contains(ps[0].Body, "order book") {
		t.Fatalf("holding past 35 s = one P0 that says why, got %d: %+v", len(ps), ps)
	}
	w.mustBeSilent("no newer book")
}

// Windows set its clock BACK an hour: every book is older than the one before the
// step. Held, then protected by the next 30 s book, before any P0. MUTANT: compare
// with the newest stamp for ever.
func TestLookTwiceLateBook_ClockSetBackRecovers(t *testing.T) {
	for name, back := range map[string]time.Duration{"an hour": time.Hour, "two seconds": 2 * time.Second} {
		t.Run(name, func(t *testing.T) {
			fastConfirm(t)
			w, sid := nakedShort(t, "lt-book-back-"+strings.ReplaceAll(name, " ", ""), 27740)
			naked := tp(sid+"-tp", 27700, 1, "Working")
			w.bookAt(w.now(), w.lastBook...)
			w.bookAt(w.now().Add(-back), naked)
			w.sync()
			w.mustStaySilent("the first book after the step")
			w.jump(25 * time.Second) // the next periodic book
			w.bookAt(w.now().Add(-back), naked)
			w.sync()
			out, took := w.waitOrders(1, 8*time.Second)
			if len(out) != 1 || !strings.HasPrefix(out[0], string(ntwire.FramePlaceProtectiveStop)) {
				t.Fatalf("a clock set back must not blind protection, got %v after %s", out, took)
			}
			for _, a := range w.p0s() {
				if a.Kind == alertKindProtectionHeld {
					t.Fatalf("recovered before 35 s: no hold P0, got %q", a.Body)
				}
			}
		})
	}
}

// Long after the step, with no hold open, nothing is different: a naked position
// is protected at once.
func TestLookTwiceLateBook_ClockSetBackLongAgo(t *testing.T) {
	fastConfirm(t)
	w, sid := nakedShort(t, "lt-book-back-old", 27740)
	covered := w.lastBook
	for i := 0; i < 3; i++ {
		w.bookAt(w.now().Add(-time.Hour), covered...)
		w.sync()
		w.jump(30 * time.Second)
	}
	w.positions(w.lastPos...)
	w.bookAt(w.now().Add(-time.Hour), tp(sid+"-tp", 27700, 1, "Working"))
	w.sync()
	w.actsOnce(0)
}

// No stamp (0) is never late, and a new connection starts over. MUTANTS: read 0 as
// older; compare across connections.
func TestLookTwiceLateBook_NoStampAndReconnectAreEvidence(t *testing.T) {
	fastConfirm(t)
	t.Run("no stamp", func(t *testing.T) {
		w, sid := nakedShort(t, "lt-book-zero", 27740)
		w.bookAt(time.Time{}, tp(sid+"-tp", 27700, 1, "Working"))
		w.sync()
		w.actsOnce(0)
	})
	t.Run("reconnect", func(t *testing.T) {
		w, sid := nakedShort(t, "lt-book-redial", 27740)
		w.redial()
		w.positions(w.lastPos...)
		w.bookAt(w.now().Add(-time.Hour), tp(sid+"-tp", 27700, 1, "Working"))
		w.sync()
		w.actsOnce(0)
	})
}

// A book written late that SHOWS a stop covers nothing either: the hold, and its
// 35 s P0, stay. MUTANT: drop the hold on it.
func TestLookTwiceLateBook_ALateStopDoesNotEndTheHold(t *testing.T) {
	fastConfirm(t)
	w, sid := nakedShort(t, "lt-book-late-stop", 27740)
	covered := w.lastBook
	w.cancelStop(sid)
	w.look()
	w.bookAt(w.now().Add(-5*time.Second), covered...) // the one from before the cancel
	w.sync()
	w.look()
	if w.holds() != 1 {
		t.Fatalf("a late book ended the hold, holds %d", w.holds())
	}
	w.jump(5 * time.Second)
	w.book("periodic", tp(sid+"-tp", 27700, 1, "Working"))
	w.sync()
	w.look()
	time.Sleep(300 * time.Millisecond)
	w.actsAfterRestart("naked, a late covered book, naked")
}

// The guard of the send reads the order too. MUTANT: drop bookLate from check.
func TestLookTwiceLateBook_AtTheSend(t *testing.T) {
	w, sid := nakedShort(t, "lt-book-send", 27740)
	w.cancelStop(sid)
	p := w.proofFor(protectionVerdict{Action: protectionPlace, StopPx: stop1193})
	if err := p.check(); err != nil {
		t.Fatalf("fixture: nothing changed, got %v", err)
	}
	at := p.bookAt
	w.s.SetReceiptClockForTest(func() time.Time { return at }) // the same instant
	w.bookAt(at.Add(-time.Hour), w.lastBook...)
	w.sync()
	if err := p.check(); err == nil || !strings.Contains(err.Error(), "order book") {
		t.Fatalf("a late book since the second look withholds the send, got %v", err)
	}
}

// Every other reader is unchanged: the last book received, dated at its receipt.
func TestLookTwiceLateBook_AllReadersStillSeeIt(t *testing.T) {
	w, sid := nakedShort(t, "lt-book-readers", 27740)
	late := w.now().Add(-time.Hour)
	w.bookAt(late, tp(sid+"-tp", 27700, 1, "Working"))
	w.sync()
	snap, at, ok := w.s.OrderSnapshots().LatestReceived("Sim101")
	if !ok || snap.EmittedMs != late.UnixMilli() || len(snap.Orders) != 1 || w.now().Sub(at) > 5*time.Second {
		t.Fatalf("every other reader keeps the last book received: ok=%v %+v received %s", ok, snap, at)
	}
}
