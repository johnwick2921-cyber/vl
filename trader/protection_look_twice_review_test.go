package trader

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/trader/types"
)

// ── LOOK TWICE, the review of 2026-11-24 ─────────────────────────────────────
//
// What stands between the second look and the order frame, what a hold's identity
// is, and what ends a hold. Same real-frames fixture. Each test names its mutant.

func (w *lookWire) lastAction() string { return w.at.lastProtectionAction("MNQ", "SHORT") }

func (w *lookWire) unprotectedP0s() (out []*store.AlertDB) {
	for _, a := range w.p0s() {
		if a.Kind == "unprotected_position" {
			out = append(out, a)
		}
	}
	return out
}

// onceOnTable runs fn once, on the goroutine of the first database statement that
// matches (before a create, after a query).
func (w *lookWire) onceOnTable(op, match string, fn func()) {
	w.t.Helper()
	var once sync.Once
	cb := w.st.GormDB().Callback()
	var err error
	switch op {
	case "create":
		err = cb.Create().Before("gorm:create").Register("lt:stall", func(db *gorm.DB) {
			if strings.Contains(db.Statement.Table, match) {
				once.Do(fn)
			}
		})
	case "query":
		err = cb.Query().After("gorm:query").Register("lt:stall", func(db *gorm.DB) {
			if strings.Contains(db.Statement.SQL.String(), match) {
				once.Do(fn)
			}
		})
	}
	if err != nil {
		w.t.Fatal(err)
	}
}

// goesFlat: the target fills and NT8 reports flat, all of it read by Go.
func (w *lookWire) goesFlat(sid string) {
	w.orderUpdate(sid+"-tp", "filled", 27700, 1)
	w.closeFrame(sid, "short", "tp", 27700, 1, 0, w.now())
	w.positions()
	w.sync()
}

// The P0's database write stalls (SQLite busy timeout 5 s) and the position goes
// flat meanwhile. The stop must already be out: nothing may follow the flat.
// MUTANT: emit the PLACE alert before the send.
func TestLookTwiceReview_PlaceIsSentBeforeTheAlertWrite(t *testing.T) {
	w, sid := nakedShort(t, "lt-rv-stall", 27740)
	w.cancelStop(sid)
	atInsert, ran := -1, make(chan struct{})
	w.onceOnTable("create", "alert", func() {
		time.Sleep(50 * time.Millisecond) // a frame already written reaches the fixture
		atInsert = w.count(ntwire.FramePlaceProtectiveStop)
		w.goesFlat(sid)
		close(ran)
	})
	w.look()
	select {
	case <-ran:
	case <-time.After(12 * time.Second):
		t.Fatal("fixture: no alert was written")
	}
	time.Sleep(700 * time.Millisecond)
	if n := w.count(ntwire.FramePlaceProtectiveStop); atInsert != 1 || n != 1 {
		t.Fatalf("stops on the wire at the alert insert %d, in the end %d — want 1 and 1 (a stop sent after the flat rests on a flat account)", atInsert, n)
	}
}

// The own-death ledger read stalls and the position goes flat meanwhile: the
// evidence must be read after it. The flat carries no applied receipt, so only
// the evidence can hold. MUTANT: read the evidence before OwnDeathFor.
func TestLookTwiceReview_FlatDuringTheLedgerRead(t *testing.T) {
	for name, px := range map[string]float64{"place": 27740, "flatten": 27755} {
		t.Run(name, func(t *testing.T) {
			w, sid := nakedShort(t, "lt-rv-ledger-"+name, px)
			w.cancelStop(sid)
			ran := make(chan struct{})
			w.onceOnTable("query", "UPPER(side)", func() {
				w.positions() // the book is unchanged: only the positions frame tells
				w.sync()
				close(ran)
			})
			w.look()
			select {
			case <-ran:
			case <-time.After(12 * time.Second):
				t.Fatal("fixture: the ledger was not read")
			}
			time.Sleep(150 * time.Millisecond)
			w.protState(func() {
				if h := w.at.protHolds["MNQ|SHORT"]; h != nil && strings.HasPrefix(h.lastWhy, "withheld") {
					t.Errorf("the look itself must see it, not only the guard of the send: %q", h.lastWhy)
				}
			})
			time.Sleep(3500 * time.Millisecond)
			w.mustBeSilent("flat during the own-death read")
		})
	}
}

// FLATTEN waits for limitFlattenMu behind another exit; NT8 reports flat meanwhile.
// MUTANT: call flattenPositionAtMarket with no guard, or ask it before the lock.
func TestLookTwiceReview_FlatDuringTheFlattenLockWait(t *testing.T) {
	w, sid := nakedShort(t, "lt-rv-flatlock", 27755)
	w.cancelStop(sid)
	w.at.limitFlattenMu.Lock()
	w.look()
	time.Sleep(4 * time.Second) // the re-look confirms and waits for the lock
	// (the pass holds protMu now: nothing here may take it)
	w.positions()
	w.sync()
	w.at.limitFlattenMu.Unlock()
	time.Sleep(700 * time.Millisecond)
	w.mustBeSilent("flat while the close waited for limitFlattenMu")
	w.mustHaveNoP0("nothing was sent, nothing to report")
	if a := w.lastAction(); a != "ok" { // the fixture's covered pass, or nothing once swept flat
		if a != "" {
			t.Fatalf("a withheld send must not be recorded as acted on, last=%q", a)
		}
	}
}

// proofFor drives the gate to its confirmation.
func (w *lookWire) proofFor(v protectionVerdict) *protectionProof {
	w.t.Helper()
	var p *protectionProof
	step := func(proven bool) {
		w.protState(func() {
			_, bookAt, _ := w.s.OrderSnapshots().LatestReceived("Sim101")
			start := w.at.beginProtectionLookLocked(w.s)
			if h := w.at.protHolds["MNQ|SHORT"]; h != nil {
				h.proven = proven
			}
			p = w.at.protectionMayActLocked(v, "MNQ|SHORT", "MNQ", "SHORT", 1, 27733.50, w.s, "Sim101", bookAt, start, 3*time.Second, "t")
		})
	}
	step(false)
	w.jump(3100 * time.Millisecond)
	step(true)
	if p == nil {
		w.t.Fatal("fixture: the second look must confirm")
	}
	return p
}

// The last recheck, one change at a time. MUTANTS: drop any case of check().
func TestLookTwiceReview_TheGuardIsTheLastStepBeforeTheFrame(t *testing.T) {
	changes := map[string]func(w *lookWire, sid string){
		"flat":    func(w *lookWire, sid string) { w.positions(); w.sync() },
		"smaller": func(w *lookWire, sid string) { w.positions(short(2, 27733.50)); w.sync() },
		"exit fill": func(w *lookWire, sid string) {
			w.closeFrame(sid, "short", "tp", 27700, 1, 0, w.now())
			w.sync()
			w.beatPositions()
		},
		"entry fill": func(w *lookWire, sid string) {
			w.send(ntwire.FrameOrderUpdate, ntwire.OrderUpdatePayload{SignalID: "new", OrderName: "new", State: "filled",
				FillPrice: 27733.50, Quantity: 1, Symbol: "MNQ", Account: "Sim101"})
			w.sync()
			w.beatPositions()
		},
		"own close": func(w *lookWire, sid string) {
			_ = w.s.SendClosePosition(ntwire.ClosePositionPayload{Symbol: "MNQ", Side: "short", SignalID: "c", Account: "Sim101"})
		},
		"own stop": func(w *lookWire, sid string) {
			_ = w.s.SendPlaceProtectiveStop(ntwire.PlaceProtectiveStopPayload{Symbol: "MNQ", SignalID: "p", PositionSide: "SHORT", Quantity: 1, StopPrice: 27760, Account: "Sim101"})
		},
		"new book":  func(w *lookWire, sid string) { w.book("periodic", w.lastBook...); w.sync() },
		"reconnect": func(w *lookWire, sid string) { w.redial(); w.beatPositions() },
		"link down": func(w *lookWire, sid string) { w.dropLink() },
		"stopping":  func(w *lookWire, sid string) { w.at.cancelStopMonitor() },
		"old ack":   func(w *lookWire, sid string) { w.jump(protectionProofMaxAge + 100*time.Millisecond) },
		"nothing":   nil,
	}
	for _, act := range []protectionAction{protectionPlace, protectionFlatten} {
		for name, change := range changes {
			if act == protectionPlace && name == "link down" {
				continue // PlaceProtectiveStop's own far-side-build gate refuses first, and the P0 says so
			}
			t.Run(act.String()+"/"+name, func(t *testing.T) {
				w, sid := nakedShort(t, "lt-rv-guard-"+act.String()+strings.ReplaceAll(name, " ", ""), 27740)
				w.cancelStop(sid)
				v := protectionVerdict{Action: act, StopPx: stop1193, Source: "t", Why: "naked."}
				p := w.proofFor(v)
				if change != nil {
					change(w, sid)
					time.Sleep(50 * time.Millisecond)
				}
				before, last := len(w.ordersOut()), w.lastAction()
				w.protState(func() {
					w.at.actOnConfirmed(v, "MNQ", "SHORT", 1, sid, "t", 0, w.now(), p.check)
					w.at.finishProtectionActLocked("MNQ|SHORT", v, p)
				})
				time.Sleep(100 * time.Millisecond)
				sent := len(w.ordersOut()) - before
				if change == nil {
					if sent != 1 || p.withheld != "" || w.holds() != 0 || len(w.unprotectedP0s()) != 1 {
						t.Fatalf("control: want 1 order, the hold ended and one P0 — sent %d, withheld %q, holds %d, P0s %d", sent, p.withheld, w.holds(), len(w.unprotectedP0s()))
					}
					return
				}
				if sent != 0 || p.withheld == "" {
					t.Fatalf("%s after the second look: want nothing written, got %d frame(s), withheld %q", name, sent, p.withheld)
				}
				if len(w.unprotectedP0s()) != 0 || w.lastAction() != last {
					t.Fatalf("a withheld send must leave no P0 and no record: P0s %d, last %q -> %q", len(w.unprotectedP0s()), last, w.lastAction())
				}
				w.protState(func() {
					if h := w.at.protHolds["MNQ|SHORT"]; h == nil || !h.firstLook.IsZero() || h.proven {
						t.Errorf("a withheld hold stays, as a first look: %+v", h)
					}
				})
			})
		}
	}
}

// beatPositions resends the last positions frame (newer than any fill).
func (w *lookWire) beatPositions() { w.positions(w.lastPos...); w.sync() }

func (w *lookWire) dropLink() {
	_ = w.conn.Close()
	for deadline := time.Now().Add(5 * time.Second); w.s.IsConnected(); time.Sleep(2 * time.Millisecond) {
		if time.Now().After(deadline) {
			w.t.Fatal("fixture: the server did not see the disconnect")
		}
	}
}

// Two rows of one root symbol and side (two MNQ expiries): the naked position is
// still acted on at about 3 s, with ONE stop. MUTANT: key the hold by symbol|side.
func TestLookTwiceReview_TwoRowsOfOneSymbolStillAct(t *testing.T) {
	w, sid := nakedShort(t, "lt-rv-tworows", 27740)
	w.positions(short(1, 27733.50), short(2, 27990))
	w.cancelStop(sid)
	began := time.Now()
	w.look()
	w.positions(short(2, 27990), short(1, 27733.50)) // the rows swap places
	w.sync()
	out, took := w.waitOrders(1, 8*time.Second)
	if len(out) != 1 || time.Since(began) < 2900*time.Millisecond {
		t.Fatalf("two rows: want one order at about 3 s, got %d after %s", len(out), took)
	}
	time.Sleep(4 * time.Second)
	if out = w.ordersOut(); len(out) != 1 {
		t.Fatalf("no book since the first stop: the second row must not get another, got %d:\n  %s", len(out), strings.Join(out, "\n  "))
	}
	w.at.Stop() // the second row's hold is still open
}

func TestProtectionHoldKeys(t *testing.T) {
	row := func(sym, side string, qty, avg float64) map[string]interface{} {
		return map[string]interface{}{"symbol": sym, "side": side, "quantity": qty, "entryPrice": avg}
	}
	a, b, c := row("MNQ", "SHORT", 1, 100), row("MNQ", "SHORT", 2, 90), row("MNQ", "LONG", 2, 90)
	for _, ps := range [][]map[string]interface{}{{a, b, c}, {c, b, a}} {
		got := protectionHoldKeys(ps)
		for i, p := range ps {
			want := map[float64]string{100: "MNQ|SHORT#2", 90: "MNQ|SHORT"}[p["entryPrice"].(float64)]
			if p["side"] == "LONG" {
				want = "MNQ|LONG"
			}
			if got[i] != want {
				t.Errorf("row %d of %v: key %q, want %q", i, got, got[i], want)
			}
		}
	}
	if got := protectionHoldKeys([]map[string]interface{}{a, a}); got[0] == got[1] {
		t.Errorf("two equal rows need two keys: %v", got)
	}
}

// Rule 5 — a same-side re-entry that filled after the exit is a NEW position, row
// or no row. MUTANT: drop the EntryOpened line of protectionOwnDeath.
func TestLookTwiceReview_OwnDeathDoesNotHoldANewEntry(t *testing.T) {
	w := open1193(t, "lt-rv-newentry", 27740)
	w.stops1193(exits1193{})
	w.flat1193()
	w.jump(10 * time.Second)
	w.mentorStop("reentry", "short", 1, 0, 27752.25)
	w.orderUpdate("reentry", "filled", 27735, 1) // no row, no bracket
	w.positions(short(1, 27735))
	w.sync()
	w.jump(3200 * time.Millisecond)
	w.look()
	if out, took := w.waitOrders(1, 6*time.Second); len(out) != 1 || !strings.HasPrefix(out[0], string(ntwire.FramePlaceProtectiveStop)) {
		t.Fatalf("a live re-entry without a stop must be protected at about 3 s, got %v after %s", out, took)
	}
	for _, a := range w.p0s() {
		if a.Kind == alertKindProtectionHeld {
			t.Fatalf("no 'already closed' P0 for a new position: %q", a.Title)
		}
	}
}

// … but NT8's "Close" fill is not an entry (its order_update moves the ENTRY fence
// too). MUTANT: use ev.Entry instead of ev.EntryOpened.
func TestLookTwiceReview_ACloseFillIsNotANewEntry(t *testing.T) {
	w := deadByReceipts(t, "lt-rv-closefill")
	w.jump(2 * time.Second)
	w.orderUpdate("Close", "filled", 27752, 1)
	w.positions(short(1, entry1193))
	w.sync()
	w.jump(3200 * time.Millisecond)
	w.look()
	w.secondLook()
	w.mustBeSilent("dead by receipts, then a Close fill")
	if ps := w.p0s(); len(ps) != 1 || ps[0].Kind != alertKindProtectionHeld {
		t.Fatalf("the veto must still hold, P0s %d", len(ps))
	}
}

// An UNKNOWN cover between two naked looks is not a look, and the next is a first
// one. MUTANT: drop h.restart() in the protectionUnknown case.
func TestLookTwiceReview_UnknownRestartsTheLooks(t *testing.T) {
	w, sid := nakedShort(t, "lt-rv-unknown", 27740)
	w.cancelStop(sid)
	naked := w.lastBook
	w.look()
	w.jump(time.Second)
	w.book("state_change", append([]ntwire.NT8Order{sl(sid+"-sl", stop1193, 1, "Martian")}, naked...)...)
	w.sync()
	w.look() // the stop's state cannot be read: UNKNOWN
	w.mustBeSilent("unknown")
	if w.holds() != 1 {
		t.Fatalf("fixture: the hold must survive an UNKNOWN look, holds %d", w.holds())
	}
	w.jump(2500 * time.Millisecond)
	w.book("state_change", naked...)
	w.sync()
	w.look()
	time.Sleep(300 * time.Millisecond)
	w.actsAfterRestart("naked, unknown, naked")
}

// A stale book ends the hold after 70 s instead of keeping its goroutine (and its
// spent P0 and own-death bound) alive all night. MUTANT: h.lastLook = passStart in
// the protectionUnknown case.
func TestLookTwiceReview_UnknownDoesNotKeepTheHoldAlive(t *testing.T) {
	w, sid := nakedShort(t, "lt-rv-stale", 27740)
	w.cancelStop(sid)
	w.look()
	for i := 0; i < 3; i++ { // beats with NT8 silent: the book is older than its bound
		w.jump(30 * time.Second)
		w.positions(w.lastPos...)
		w.sync()
		w.jump(30*time.Second + 500*time.Millisecond)
		w.look()
	}
	for deadline := time.Now().Add(6 * time.Second); w.relookAlive() || w.holds() != 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("3 minutes of UNKNOWN: holds %d, goroutine %v", w.holds(), w.relookAlive())
		}
	}
	w.mustBeSilent("unknown")
}

// Positions that stay unreadable end the hold after 70 s too. MUTANT: drop
// expireProtectionHoldsLocked.
func TestLookTwiceReview_UnreadablePositionsEndTheHold(t *testing.T) {
	w, sid := nakedShort(t, "lt-rv-unreadable", 27740)
	w.cancelStop(sid)
	w.look()
	// an entry fill with no positions frame after it: GetPositions knows nothing
	w.orderUpdate("new", "filled", 27733.50, 1)
	w.sync()
	if _, err := w.nt.GetPositions(); err == nil {
		t.Fatal("fixture: the positions must be unreadable")
	}
	w.jump(71 * time.Second)
	for deadline := time.Now().Add(7 * time.Second); w.relookAlive() || w.holds() != 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("holds %d, goroutine %v", w.holds(), w.relookAlive())
		}
	}
}

// A re-look serves the open holds only; another position's first look is the
// beat's. MUTANT: drop the relook filter.
func TestLookTwiceReview_RelookServesOpenHoldsOnly(t *testing.T) {
	w, sid := nakedShort(t, "lt-rv-only", 27740)
	w.noAcks() // the MNQ hold stays open
	w.cancelStop(sid)
	w.look()
	w.positions(short(1, 27733.50), ntwire.OpenPosition{Symbol: "MES", Side: "long", Quantity: 1, AvgPrice: 6000})
	w.sync()
	beats := w.count(ntwire.FrameHeartbeat)
	for deadline := time.Now().Add(6 * time.Second); w.count(ntwire.FrameHeartbeat) == beats; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("fixture: the re-look did not run (no link check)")
		}
	}
	time.Sleep(200 * time.Millisecond)
	if w.holds() != 1 {
		t.Fatalf("a re-look opened a hold for another position: holds %d", w.holds())
	}
}

// Recovery seen by a RE-LOOK ends the episode: the next emergency close raises its
// own P0. MUTANT: drop noteProtectionCovered.
func TestLookTwiceReview_RelookRecoveryEndsTheEpisode(t *testing.T) {
	w, sid := nakedShort(t, "lt-rv-episode", 27755) // would FLATTEN
	w.cancelStop(sid)
	w.actsOnce(0)
	if w.lastAction() != "flatten" || len(w.unprotectedP0s()) != 1 {
		t.Fatalf("fixture: last %q, P0s %d", w.lastAction(), len(w.unprotectedP0s()))
	}
	w.jump(exitInFlightHold + 5*time.Second) // NT8 refused it; the position is still there
	w.beat30()
	w.sync()
	w.look()
	if w.holds() != 1 {
		t.Fatalf("fixture: naked again, holds %d", w.holds())
	}
	w.orderUpdate(sid+"-sl", "working", 0, 0, sl(sid+"-sl", stop1193, 1, "Working"), tp(sid+"-tp", 27700, 1, "Working"))
	w.sync()
	for deadline := time.Now().Add(6 * time.Second); w.holds() != 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("fixture: the re-look did not see the cover")
		}
	}
	if a := w.lastAction(); a != "ok" {
		t.Fatalf("a covered re-look must end the flatten episode, last=%q", a)
	}
	w.cancelStop(sid)
	w.look()
	w.waitOrders(2, 12*time.Second)
	time.Sleep(300 * time.Millisecond)
	if n := len(w.unprotectedP0s()); n != 2 {
		t.Fatalf("a second naked episode needs its own P0, got %d", n)
	}
}

// A standing alert-only state takes no look at all: no hold, no link check, no
// second P0. MUTANT: drop the standing-alert break.
func TestLookTwiceReview_StandingAlertOnlyIsSilent(t *testing.T) {
	w := newLookWire(t, "lt-rv-standing", ctInstant(t, "2026-11-18 10:00:05.000"))
	w.orderUpdate("ai-sig", "filled", 27733.50, 1)
	w.positions(short(1, 27733.50)) // no row, no price
	w.sync()
	w.jump(10 * time.Second)
	w.beat30()
	w.sync()
	w.look()
	for deadline := time.Now().Add(12 * time.Second); len(w.unprotectedP0s()) == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("fixture: the first P0")
		}
	}
	time.Sleep(200 * time.Millisecond)
	beats := w.count(ntwire.FrameHeartbeat)
	for i := 0; i < 3; i++ {
		w.jump(60 * time.Second)
		w.beat30()
		w.sync()
		w.look()
		if w.holds() != 0 || w.relookAlive() {
			t.Fatalf("beat %d: holds %d, goroutine %v", i, w.holds(), w.relookAlive())
		}
	}
	if n := w.count(ntwire.FrameHeartbeat) - beats; n != 0 || len(w.unprotectedP0s()) != 1 {
		t.Fatalf("link checks +%d, P0s %d", n, len(w.unprotectedP0s()))
	}
}

// One goroutine's exit never clears a newer one's registration. MUTANT: compare
// the Run instead of the wake channel.
func TestLookTwiceReview_RelookRegistrationIsPerGoroutine(t *testing.T) {
	at := &AutoTrader{}
	run := context.Background()
	old, newer := make(chan struct{}, 1), make(chan struct{}, 1)
	at.protRelookRun, at.protRelookWake = run, newer
	at.releaseProtectionRelookLocked(old)
	if at.protRelookRun != run || at.protRelookWake != newer {
		t.Fatal("an older goroutine cleared the newer one's registration")
	}
	at.releaseProtectionRelookLocked(newer)
	if at.protRelookRun != nil || at.protRelookWake != nil {
		t.Fatal("the owner must clear its own")
	}
}

type placeTrader struct {
	MockTrader
	err error
}

func (p *placeTrader) PlaceProtectiveStop(string, string, int, float64, string, string) error {
	return p.err
}

var _ types.Trader = (*placeTrader)(nil)

// The PLACE P0 says what went out. MUTANT: word it before the send.
func TestProtectionPlaceP0SaysWhatWasSent(t *testing.T) {
	for id, c := range map[string]struct {
		trader types.Trader
		want   string
	}{
		"sent":    {&placeTrader{}, "was sent"},
		"refused": {&placeTrader{err: fmt.Errorf("no build")}, "could NOT be sent"},
	} {
		at, st := resetTrader(t, store.StrategyConfig{})
		at.id, at.exchange, at.trader = "pl-"+id, "ninjatrader", c.trader
		t.Cleanup(func() { at.sweepStaleProtectionState(nil) })
		at.actOnProtection(protectionVerdict{Action: protectionPlace, StopPx: 31163, NeedQty: 1, Source: "plan", Why: "naked."}, "MNQ", "LONG", 1, "sig", "t", 0, time.Now())
		all, err := st.Alert().List(at.id, 10)
		if err != nil || len(all) != 1 || !strings.Contains(all[0].Title, c.want) || strings.Contains(all[0].Title+all[0].Body, "being placed") {
			t.Errorf("%s: err %v, alerts %+v", id, err, all)
		}
	}
}

// A resting limit exit is escalated and the market close cannot be written: the P0
// must not say it was sent. MUTANT: ignore the escalation's error.
func TestProtectionFlattenP0FailedEscalation(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{})
	at.id, at.exchange = "fl-esc", "ninjatrader"
	at.trader = &closeFailTrader{err: fmt.Errorf("no NT client connected")}
	t.Cleanup(func() { at.sweepStaleProtectionState(nil) })
	now := time.Now()
	pos := store.TraderPosition{TraderID: at.id, Symbol: "MNQ", Side: "LONG"}
	at.limitFlattenMu.Lock()
	at.registerLimitFlattenLocked(&pendingLimitFlatten{position: pos, key: flattenKeyOf(&pos), tag: "t", after: time.Hour,
		closeSignalID: "lx-1", sentAt: now, deadline: now.Add(time.Hour)}, false)
	at.limitFlattenMu.Unlock()
	at.actOnProtection(protectionVerdict{Action: protectionFlatten, StopPx: 31163, NeedQty: 1, Why: "through."}, "MNQ", "LONG", 1, "sig", "t", 0, now)
	all, err := st.Alert().List(at.id, 10)
	if err != nil || len(all) != 1 || !strings.Contains(all[0].Title, "could NOT be sent") {
		t.Fatalf("err %v, alerts %+v", err, all)
	}
}
