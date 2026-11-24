package trader

import (
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
)

// ── LOOK TWICE, the late positions frame (review of 92f14deac, finding 1) ────
//
// The AddOn's 30 s resend reads the account on one thread and writes on the same
// lock as every other frame: a "Short 1" built BEFORE the exit filled can be
// written AFTER the Flat frame. It is then the newest frame, newer than every
// fill, and it repeats the dead position for up to 30 s — two looks, the fences
// and the link check all agree with it. What contradicts it is the smaller frame
// received before it, with no entry fill since.

// mustStaySilent runs a look and a second look and waits out a background
// re-look, all on the frames already received.
func (w *lookWire) mustStaySilent(when string) {
	w.t.Helper()
	w.look()
	w.secondLook()
	relookOver()
	w.mustBeSilent(when)
}

// (a) the target fills, NT8 says flat, a late "Short 1" follows; no position_close
// was applied. A stop placed now rests alone on a flat account.
func TestLookTwiceLateFrame_PlaceOnAFlatAccount(t *testing.T) {
	w, sid := nakedShort(t, "lt-late-place", 27740)
	w.orderUpdate(sid+"-tp", "filled", 27700, 1)
	w.positions()
	w.positions(short(1, 27733.50)) // the late one
	w.sync()
	w.mustStaySilent("a late Short 1 after the Flat frame")
	w.mustHaveNoP0("before the next 30 s frame")
	w.jump(20 * time.Second)
	w.positions() // the next 30 s resend: flat
	w.sync()
	w.look()
	w.mustBeSilent("flat")
	w.mustHaveNoP0("the next frame settled it")
	if w.holds() != 0 {
		t.Fatalf("flat ends the hold, holds %d", w.holds())
	}
}

// (b) a Close fills, NT8 says flat, a late "Short 1" follows and the price is
// through the stop: close_position on a flat account OPENS a reverse (11-19).
func TestLookTwiceLateFrame_FlattenOnAFlatAccount(t *testing.T) {
	fastConfirm(t)
	for name, fill := range map[string]func(w *lookWire){
		"order_update": func(w *lookWire) { w.orderUpdate("Close", "filled", 27755, 1) },
		"position_close": func(w *lookWire) {
			w.closeFrame("Close", "short", "manual", 27755, 1, 0, w.now())
			w.book("state_change")
		},
		"no exit frame at all": func(w *lookWire) { w.book("state_change") },
	} {
		t.Run(name, func(t *testing.T) {
			w, _ := nakedShort(t, "lt-late-flat-"+strings.ReplaceAll(name, " ", ""), 27755)
			fill(w)
			w.positions()
			w.positions(short(1, 27733.50)) // the late one
			w.sync()
			w.mustStaySilent("a late Short 1 after the Close and the Flat frame")
		})
	}
}

// (c) SHORT 3: leg 1 stops 2, the runner's stop is cancelled, "Short 1", then a
// late "Short 3". The stop must be for 1, once a frame says 1 again.
func TestLookTwiceLateFrame_RunnerGetsAStopOfItsOwnSize(t *testing.T) {
	fastConfirm(t)
	w := open1193(t, "lt-late-runner", 27740)
	sl2, tp2 := sl(sid1193+"-sl2", stop1193, 1, "Working"), tp(sid1193+"-tp2", 27133.50, 1, "Working")
	w.orderUpdate(sid1193+"-sl", "filled", stop1193, 2, sl2, tp2)
	w.closeFrame(sid1193, "short", "sl", stop1193, 2, 1, w.now())
	w.orderUpdate(sid1193+"-sl2", "cancelled", 0, 0, tp2)
	w.positions(short(1, entry1193))
	w.positions(short(3, entry1193)) // the late one
	w.sync()
	w.receipts(1, true)
	w.mustStaySilent("a late Short 3 after Short 1")
	w.jump(20 * time.Second)
	w.positions(short(1, entry1193)) // the next 30 s resend
	w.book("periodic", tp2)
	w.sync()
	w.look()
	out, took := w.waitOrders(1, 12*time.Second)
	if len(out) != 1 || !strings.HasPrefix(out[0], string(ntwire.FramePlaceProtectiveStop)) || !strings.Contains(out[0], `"quantity":1,`) {
		t.Fatalf("the runner of 1 must get a stop of 1, got %v after %s", out, took)
	}
}

// Two late frames in their own order: flat, then "Short 3", then "Short 1". The
// smaller of the two is still more than flat. MUTANT: end the doubt on any
// smaller frame.
func TestLookTwiceLateFrame_TwoLateFrames(t *testing.T) {
	fastConfirm(t)
	w := open1193(t, "lt-late-two", 27740)
	w.stops1193(exits1193{noReceipt: true})
	w.positions()
	w.positions(short(3, entry1193))
	w.positions(short(1, entry1193))
	w.sync()
	w.mustStaySilent("flat, then two late frames")
}

// A late frame of one side while the OTHER side was really entered: the entry
// fill says nothing about the dead short. MUTANT: compare EntryOpened with the
// exit instead of the last smaller frame.
func TestLookTwiceLateFrame_AfterTheOtherSideEntered(t *testing.T) {
	fastConfirm(t)
	w, sid := nakedShort(t, "lt-late-other", 27755)
	w.orderUpdate(sid+"-tp", "filled", 27700, 1)
	w.positions()
	stop := sl("new-long-sl", 27740, 1, "Working")
	stop.Action = "sell"
	w.orderUpdate("new-long", "filled", 27755, 1, stop)
	w.positions(long(1, 27755))
	w.positions(short(1, 27733.50)) // the late one
	w.sync()
	w.mustStaySilent("a late Short 1 after Long 1")
}

// The frames end for good: the owner is told at 35 s, and a position that the
// next frame repeats is then protected. MUTANT: no settle at all.
func TestLookTwiceLateFrame_P0At35sThenTheRepeatedFrameActs(t *testing.T) {
	fastConfirm(t)
	w, sid := nakedShort(t, "lt-late-p0", 27740)
	w.orderUpdate(sid+"-sl", "cancelled", 0, 0, tp(sid+"-tp", 27700, 1, "Working"))
	w.positions()
	w.positions(short(1, 27733.50)) // not late after all: NT8 blinked flat
	w.sync()
	w.look()
	w.jump(34 * time.Second)
	w.book("periodic", w.lastBook...)
	w.sync()
	w.look()
	w.mustHaveNoP0("before 35 s")
	w.jump(2 * time.Second)
	w.look()
	if ps := w.p0s(); len(ps) != 1 || ps[0].Kind != alertKindProtectionHeld || !strings.Contains(ps[0].Body, "already reported smaller") {
		t.Fatalf("holding past 35 s = one P0 that says why, got %d: %+v", len(ps), ps)
	}
	w.mustBeSilent("no frame has repeated it")
	w.beat30()
	w.sync()
	w.look()
	if out, took := w.waitOrders(1, 12*time.Second); len(out) != 1 {
		t.Fatalf("a later frame repeats the position: it must be protected, got %v after %s", out, took)
	}
}

// A frame that repeats the late one at once proves nothing (two resends can both
// be late). MUTANT: settle on any repeated frame.
func TestLookTwiceLateFrame_ARepeatAtOnceIsNotProof(t *testing.T) {
	fastConfirm(t)
	w, sid := nakedShort(t, "lt-late-rep", 27740)
	w.orderUpdate(sid+"-tp", "filled", 27700, 1)
	w.positions()
	w.positions(short(1, 27733.50))
	w.sync()
	w.jump(4 * time.Second)
	w.positions(short(1, 27733.50))
	w.sync()
	w.mustStaySilent("two late frames 4 s apart")
}

// THE OTHER WAY ROUND: a genuine hand entry (NT8 names it nothing, so the AddOn
// sends no order_update and there is no entry fence) after a flat frame. Held
// until the next 30 s frame, then protected. Never for ever.
func TestLookTwiceLateFrame_AHandEntryIsProtectedByTheNextFrame(t *testing.T) {
	fastConfirm(t)
	w := newLookWire(t, "lt-late-hand", ctInstant(t, "2026-11-18 10:00:05.000"))
	w.price(27740)
	w.mentorStop("hand", "short", 1, 0, stop1193)
	w.positions()
	w.sync()
	w.jump(10 * time.Minute)
	w.beat30() // flat
	w.positions(short(1, 27733.50))
	w.sync()
	w.look()
	w.secondLook()
	w.mustBeSilent("one frame, no entry fill")
	w.jump(25 * time.Second)
	w.beat30()
	w.sync()
	w.mustHaveNoP0("settled before 35 s")
	out, took := w.waitOrders(1, 12*time.Second)
	if len(out) != 1 || !strings.HasPrefix(out[0], string(ntwire.FramePlaceProtectiveStop)) {
		t.Fatalf("the hand entry must be protected once the 30 s frame repeats it, got %v after %s", out, took)
	}
}

// … and a hand ADD beside a lost stop: the next frame that repeats Short 2 gets
// a stop of 2.
func TestLookTwiceLateFrame_AHandAddIsProtectedByTheNextFrame(t *testing.T) {
	fastConfirm(t)
	w, sid := nakedShort(t, "lt-late-add", 27740)
	w.cancelStop(sid)
	w.positions(short(2, 27735))
	w.sync()
	w.look()
	w.secondLook()
	w.mustBeSilent("one frame, no entry fill")
	w.jump(25 * time.Second)
	w.beat30()
	w.sync()
	out, took := w.waitOrders(1, 12*time.Second)
	if len(out) != 1 || !strings.Contains(out[0], `"quantity":2,`) {
		t.Fatalf("the added position must get a stop of 2, got %v after %s", out, took)
	}
}

// A real entry (its fill received after the flat frame) is not delayed at all.
// MUTANT: ignore EntryOpened.
func TestLookTwiceLateFrame_AnEntryFillExplainsTheGrowth(t *testing.T) {
	fastConfirm(t)
	for name, frameFirst := range map[string]bool{"fill first": false, "frame first": true} {
		t.Run(name, func(t *testing.T) {
			w := newLookWire(t, "lt-late-entry-"+strings.ReplaceAll(name, " ", ""), ctInstant(t, "2026-11-18 10:00:05.000"))
			w.price(27740)
			w.mentorStop("ai-sig", "short", 1, 0, stop1193)
			w.positions()
			w.sync()
			w.jump(time.Minute)
			if frameFirst {
				w.positions(short(1, 27733.50))
			}
			w.orderUpdate("ai-sig", "filled", 27733.50, 1)
			w.positions(short(1, 27733.50))
			w.sync()
			w.jump(5 * time.Second)
			w.actsOnce(protectionConfirm() - 100*time.Millisecond)
		})
	}
}
