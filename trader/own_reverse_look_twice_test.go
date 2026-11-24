package trader

import (
	"fmt"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── OWN REVERSE (spec look-twice 2026-11-24, 3.C and 9.13) ───────────────────
//
// An exit receipt that stays UNAPPLIED (no row to close) while NT8 now holds the
// OPPOSITE side, at the receipt's price, in exactly the quantity the receipts
// over-closed: our own close opened a reverse. About 3 s after the receipt the
// owner gets a P0 that says "reverse"; with a send of ours on record the row the
// reconcile materializes is origin=bot, so the daily-loss gate counts it.
//
// CONTRACT the build must keep: the P0's title or body contains "reverse", and
// its kind passes isOpsAlertKind (mentor traders run with no day plan).

const ownReverseEnv = "OWN_REVERSE"

// fastReverse shortens the detector's wait for tests that pin what it decides, not
// that it waits 3 s (the 11-19 pair and TestOwnReverseSetting keep the 3 s).
func fastReverse(t *testing.T) {
	t.Helper()
	ownReverseDelay = time.Second
	t.Cleanup(func() { ownReverseDelay = ownReverseDelayDefault })
}

func (w *lookWire) reverseP0s() []*store.AlertDB {
	w.t.Helper()
	var out []*store.AlertDB
	for _, a := range w.p0s() {
		if strings.Contains(strings.ToLower(a.Title+" "+a.Body), "reverse") {
			out = append(out, a)
		}
	}
	return out
}

func (w *lookWire) mustFlagReverse(d time.Duration) {
	w.t.Helper()
	for deadline := time.Now().Add(d); ; time.Sleep(10 * time.Millisecond) {
		if got := w.reverseP0s(); len(got) > 0 {
			if len(got) != 1 {
				w.t.Fatalf("one reverse = one P0, got %d", len(got))
			}
			if !isOpsAlertKind(got[0].Kind) {
				w.t.Fatalf("the reverse P0 kind %q is not an ops kind: a mentor trader with no day plan would never see it", got[0].Kind)
			}
			return
		}
		if time.Now().After(deadline) {
			w.t.Fatalf("no P0 saying the bot's own close opened a reverse within %s of the unapplied receipt (P0s: %d)", d, len(w.p0s()))
		}
	}
}

func (w *lookWire) mustNotFlagReverse(d time.Duration) {
	w.t.Helper()
	time.Sleep(d)
	if got := w.reverseP0s(); len(got) != 0 {
		w.t.Fatalf("false reverse: %q / %q", got[0].Title, got[0].Body)
	}
}

// materialize drives the reconcile's 60 s untracked grace and returns the row.
func (w *lookWire) materialize(side string) *store.TraderPosition {
	w.t.Helper()
	w.nt.ReconcilePassForTest(w.at.id, "ex", "ninjatrader", w.st)
	w.jump(61 * time.Second)
	w.beat30()
	w.sync()
	w.nt.ReconcilePassForTest(w.at.id, "ex", "ninjatrader", w.st)
	row, err := w.st.Position().GetOpenPositionByAccountSymbol("Sim101", "MNQ", side)
	if err != nil || row == nil {
		w.t.Fatalf("fixture: the reconcile must materialize the %s row (row=%v err=%v)", side, row, err)
	}
	return row
}

func (w *lookWire) mustBeBotRow(side string) {
	w.t.Helper()
	row := w.materialize(side)
	for deadline := time.Now().Add(2 * time.Second); store.IsManualOrigin(row.Origin); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			w.t.Fatalf("row %d (%s, the reverse our own close opened) is origin=%q — the daily-loss gate and the bot record would not see it", row.ID, side, row.Origin)
		}
		if r, err := w.st.Position().GetOpenPositionByAccountSymbol("Sim101", "MNQ", side); err == nil && r != nil {
			row = r
		}
	}
}

func (w *lookWire) closesOut() []string {
	var out []string
	for _, f := range w.ordersOut() {
		if strings.HasPrefix(f, string(ntwire.FrameClosePosition)) {
			out = append(out, f)
		}
	}
	return out
}

// reverse1119 plays 11-19 from the broker's side with the close already sent by
// us at .250: flat .254, the Close BuyToCover fill .388, and NT8 long.
func reverse1119(t *testing.T, id string, avg float64, book ...ntwire.NT8Order) *lookWire {
	t.Helper()
	w := open1193(t, id, stop1193)
	w.stops1193(exits1193{})
	w.until(ms1193(250))
	if _, err := w.nt.CloseShort("MNQ", 0); err != nil { // our own send, on record
		t.Fatal(err)
	}
	w.until(ms1193(254))
	w.flat1193()
	w.until(ms1193(388))
	w.orderUpdate("Close", "filled", 27753.25, 1, book...)
	w.closeFrame("Close", "short", "manual", 27753.25, 1, 0, at1193(t, ms1193(388)))
	w.positions(long(1, avg))
	w.sync()
	w.receipts(1, false) // log 390446: PENDING, position_id 0
	return w
}

func TestLookTwice13_OwnReverse1119(t *testing.T) {
	w := reverse1119(t, "or-1119", 27753.25)
	w.mustFlagReverse(8 * time.Second)
	if got := w.closesOut(); len(got) != 1 {
		t.Fatalf("OWN_REVERSE defaults to alert: no close beyond our first one, got %d:\n  %s", len(got), strings.Join(got, "\n  "))
	}
}

// Row 1194's origin. Today the reconcile materializes it origin=manual, so the
// −1207.00 was outside the bot record and the daily-loss gate.
func TestLookTwice13_OwnReverseRowIsBot1119(t *testing.T) {
	w := reverse1119(t, "or-1119-row", 27753.25)
	time.Sleep(ownReverseDelay + time.Second) // the detector's 3 s
	w.mustBeBotRow("LONG")
}

// 10-30 14:45:21: two close_position frames 4 ms apart rested two "-lx" limits;
// both filled at 28429.75 and the second opened LONG 1 (row 724, origin manual).
func TestLookTwice13_OwnReverse1030(t *testing.T) {
	fastReverse(t)
	const sid = "a07a4475-1d73-41c1-acb1-78decf310acf"
	w := newLookWire(t, "or-1030", ctInstant(t, "2026-10-30 14:40:00.000"))
	w.row(sid, "SHORT", 1, 28600.25, w.now())
	w.orderUpdate(sid, "filled", 28600.25, 1)
	w.positions(short(1, 28600.25))
	w.sync()
	w.setNow(ctInstant(t, "2026-10-30 14:45:21.193"))
	var cids []string
	for i := 0; i < 2; i++ {
		r, err := w.nt.CloseWithLimit("MNQ", "short", 1, 28429.75)
		if err != nil {
			t.Fatal(err)
		}
		cids = append(cids, fmt.Sprint(r["signal_id"]))
	}
	for i, ms := range []string{"14:45:25.553", "14:45:25.568"} {
		w.setNow(ctInstant(t, "2026-10-30 "+ms))
		w.orderUpdate(cids[i]+"-lx", "filled", 28429.75, 1)
		w.closeFrame(cids[i], "short", "limit", 28429.75, 1, 0, w.now())
	}
	w.positions(long(1, 28429.75))
	w.sync()
	w.receipts(1, true)
	w.receipts(1, false)
	w.mustFlagReverse(8 * time.Second)
}

// 10-16 02:13:26: two stops for one contract both filled at 30186.25. Receipts
// only: no order_update frame, no close of ours on record. Flagged (alert).
func TestLookTwice13_OwnReverse1016FromReceiptsOnly(t *testing.T) {
	fastReverse(t)
	const sid = "6714b4ad-9863-4010-b106-449714de2e5b"
	w := newLookWire(t, "or-1016", ctInstant(t, "2026-10-16 02:13:00.000"))
	w.row(sid, "SHORT", 1, 30186.25, w.now())
	w.orderUpdate(sid, "filled", 30186.25, 1)
	w.positions(short(1, 30186.25))
	w.sync()
	w.setNow(ctInstant(t, "2026-10-16 02:13:26.490"))
	w.closeFrame(sid, "short", "sl", 30186.25, 1, 0, ctInstant(t, "2026-10-16 02:13:26.515"))
	w.closeFrame(sid, "short", "sl", 30186.25, 1, 0, ctInstant(t, "2026-10-16 02:13:26.522"))
	w.positions(long(1, 30186.25))
	w.sync()
	w.receipts(1, true)
	w.receipts(1, false)
	w.mustFlagReverse(8 * time.Second)
	w.mustBeSilent("an alert, nothing else")
}

// A close of 3 while only 1 was left: the receipts over-closed by 2 and NT8 is
// long 2. Arithmetic, not "the same quantity as the receipt".
func TestLookTwice13_PartialOverClose(t *testing.T) {
	fastReverse(t)
	w := open1193(t, "or-partial", stop1193)
	w.until(ms1193(157))
	left := sl(sid1193+"-sl2", stop1193, 1, "Working")
	w.orderUpdate(sid1193+"-sl", "filled", stop1193, 2, left)
	w.closeFrame(sid1193, "short", "sl", stop1193, 2, 1, w.now())
	w.positions(short(1, entry1193))
	w.sync()
	w.receipts(1, true)
	if _, err := w.nt.CloseShort("MNQ", 0); err != nil {
		t.Fatal(err)
	}
	w.orderUpdate("Close", "filled", 27753.25, 3)
	w.closeFrame("Close", "short", "manual", 27753.25, 3, 0, w.now())
	w.positions(long(2, 27753.25))
	w.sync()
	w.receipts(1, false)
	w.mustFlagReverse(8 * time.Second)
}

// Pins: what must NOT be called a reverse. Green today (no detector) and after.
func TestLookTwicePin13_NotAReverse(t *testing.T) {
	fastReverse(t)
	t.Run("09-28 hand Close of 40: no row, flat after", func(t *testing.T) {
		w := newLookWire(t, "or-0928", ctInstant(t, "2026-09-28 01:16:00.000"))
		w.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "long", Quantity: 40, AvgPrice: 30600})
		w.sync()
		w.setNow(ctInstant(t, "2026-09-28 01:16:30.098"))
		w.orderUpdate("Close", "filled", 30634.275, 40)
		w.closeFrame("Close", "long", "manual", 30634.275, 40, 0, w.now())
		w.positions()
		w.sync()
		w.receipts(1, false)
		w.mustNotFlagReverse(ownReverseDelay + time.Second)
	})
	t.Run("one tick off the receipt price", func(t *testing.T) {
		w := reverse1119(t, "or-tick", 27753.50)
		w.mustNotFlagReverse(ownReverseDelay + time.Second)
	})
	t.Run("a hand reverse: no send of ours, so the row stays manual", func(t *testing.T) {
		w := newLookWire(t, "or-hand", ctInstant(t, "2026-11-18 10:00:05.000"))
		w.positions(long(1, 27700))
		w.sync()
		if row := w.materialize("LONG"); !store.IsManualOrigin(row.Origin) {
			t.Fatalf("a hand-opened position must stay origin=manual, got %q", row.Origin)
		}
	})
	t.Run("OWN_REVERSE=off", func(t *testing.T) {
		t.Setenv(ownReverseEnv, "off")
		w := reverse1119(t, "or-off", 27753.25)
		w.mustNotFlagReverse(ownReverseDelay + time.Second)
	})
}

// OWN_REVERSE=close is REFUSED in Release 1 and reads as alert (step 4 ruling:
// "refused unless every section 3 C condition is implemented" — the send record
// is in memory only, so the "saved record of our own send" condition is not).
// Whatever is on the book, it alerts and never sends an order.
func TestLookTwice13_CloseMode(t *testing.T) {
	fastReverse(t)
	for name, book := range map[string][]ntwire.NT8Order{
		"ours alone": nil,
		"the owner added an order": {{OrderID: "own-1", Symbol: "MNQ",
			Name: "Stop1", Action: "sell", Type: "stop", StopPrice: 27700, Quantity: 1, State: "Working"}},
	} {
		t.Run(name+": alert, no close", func(t *testing.T) {
			t.Setenv(ownReverseEnv, "close")
			w := reverse1119(t, "or-close-"+name[:3], 27753.25, book...)
			w.mustFlagReverse(8 * time.Second)
			time.Sleep(ownReverseDelay + time.Second)
			if got := w.closesOut(); len(got) != 1 {
				t.Fatalf("close is refused in this release: no close beyond our first one, got %d:\n  %s", len(got), strings.Join(got, "\n  "))
			}
			p := w.reverseP0s()[0]
			if !strings.Contains(p.Body, "Nothing was sent") {
				t.Fatalf("the P0 must say nothing was sent: %q", p.Body)
			}
			if wantStop := len(book) == 0; strings.Contains(p.Title, "no stop") != wantStop {
				t.Fatalf("the P0 must say what the book shows (no stop = %v): %q", wantStop, p.Title)
			}
		})
	}
}
