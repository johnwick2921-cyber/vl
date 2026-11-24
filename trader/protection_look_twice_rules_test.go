package trader

import (
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── LOOK TWICE, rule by rule (spec 2026-11-24 section 3 B) ───────────────────
//
// protection_look_twice_test.go replays incidents; this file takes one rule at a
// time, on the same real-frames fixture. Each test names the mutant it was shown
// RED against.

func (w *lookWire) protState(fn func()) {
	w.at.protMu.Lock()
	defer w.at.protMu.Unlock()
	fn()
}

func (w *lookWire) holds() (n int) {
	w.protState(func() { n = len(w.at.protHolds) })
	return n
}

func (w *lookWire) relookAlive() (alive bool) {
	w.protState(func() { alive = w.at.protRelookRun != nil })
	return alive
}

// noAcks: the AddOn stops answering heartbeats (the stream is stuck).
func (w *lookWire) noAcks() {
	w.stopOnce.Do(func() { close(w.stopAck) })
	w.ackWG.Wait()
}

// actsAfterRestart: the look just taken must be a FIRST one — nothing goes out
// now — and the position must still be protected by the re-look.
func (w *lookWire) actsAfterRestart(when string) {
	w.t.Helper()
	w.mustBeSilent(when)
	if out, took := w.waitOrders(1, 12*time.Second); len(out) != 1 {
		w.t.Fatalf("%s: the position must still be protected by the next second look, got %d order(s) after %s", when, len(out), took)
	}
}

func TestLookTwiceConfirmSetting(t *testing.T) {
	for v, want := range map[string]time.Duration{
		"": 3 * time.Second, "3000": 3 * time.Second, " 5000 ": 5 * time.Second, "0": 0,
		"1000": time.Second, "30000": 30 * time.Second,
		"abc": 3 * time.Second, "-1": 3 * time.Second, "3": 3 * time.Second, "999": 3 * time.Second,
		"30001": 3 * time.Second, "3s": 3 * time.Second, "2.5": 3 * time.Second,
	} {
		t.Setenv(lookTwiceEnv, v)
		if got := protectionConfirm(); got != want {
			t.Errorf("%s=%q = %s, want %s", lookTwiceEnv, v, got, want)
		}
	}
	t.Setenv(lookTwiceEnv, "0")
	if l := ProtectionConfirmBootLine(); !strings.Contains(l, "OFF") || !strings.Contains(l, lookTwiceEnv+"=0") {
		t.Errorf("boot line must READ the revert: %q", l)
	}
	t.Setenv(lookTwiceEnv, "7")
	if l := ProtectionConfirmBootLine(); !strings.Contains(l, "3000ms") || !strings.Contains(l, lookTwiceEnv+"=7") {
		t.Errorf("boot line must show the value in force AND the raw setting: %q", l)
	}
}

// Rule 1 — a changed average price is another position.
func TestLookTwiceRule1_AverageChangeRestarts(t *testing.T) {
	w, sid := nakedShort(t, "lt-r1-avg", 27740)
	w.cancelStop(sid)
	w.look()
	w.jump(3100 * time.Millisecond)
	w.positions(short(1, 27733.75))
	w.sync()
	w.look()
	time.Sleep(300 * time.Millisecond)
	w.actsAfterRestart("same quantity, another average price")
}

// Rule 1 — more than 70 s since the last look.
func TestLookTwiceRule1_GapRestarts(t *testing.T) {
	w, sid := nakedShort(t, "lt-r1-gap", 27740)
	w.cancelStop(sid)
	w.look()
	w.jump(71 * time.Second)
	w.beat30()
	w.sync()
	w.look()
	time.Sleep(300 * time.Millisecond)
	w.actsAfterRestart("71 s after the last look")
}

// Rule 1 — two consecutive 60 s beats DO count as two looks.
func TestLookTwiceRule1_TwoBeatsCount(t *testing.T) {
	w, sid := nakedShort(t, "lt-r1-beats", 27740)
	w.cancelStop(sid)
	w.look()
	w.jump(60 * time.Second)
	w.beat30()
	w.sync()
	w.look()
	if out, took := w.waitOrders(1, time.Second); len(out) != 1 {
		t.Fatalf("the second 60 s beat must act at once (with its link check), got %d order(s) after %s", len(out), took)
	}
}

// Rule 1 — positions that cannot be read are not a look.
func TestLookTwiceRule1_ReadErrorRestarts(t *testing.T) {
	w, sid := nakedShort(t, "lt-r1-err", 27740)
	w.cancelStop(sid)
	w.look()
	w.at.trader = nttrader.NewTCPTrader(w.s, "MNQ", "Sim999") // no positions frame, no fill: UNKNOWN
	w.look()
	w.at.trader = w.nt
	w.jump(3100 * time.Millisecond)
	w.look()
	time.Sleep(300 * time.Millisecond)
	w.actsAfterRestart("after an unreadable positions read")
}

// redial drops the AddOn connection and dials a new one.
func (w *lookWire) redial() {
	t := w.t
	t.Helper()
	_ = w.conn.Close()
	for deadline := time.Now().Add(5 * time.Second); w.s.IsConnected(); time.Sleep(2 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("fixture: the server did not see the disconnect")
		}
	}
	conn, err := net.Dial("tcp", w.s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	waitAddonRegistered(t, w.s)
	w.wmu.Lock()
	w.conn = conn
	w.wmu.Unlock()
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			w.mu.Lock()
			w.frames = append(w.frames, env)
			w.mu.Unlock()
		}
	}()
}

// Rule 1 — a NEW connection: the looks of the old one do not count, and the
// positions frame cached from the old one is not evidence.
func TestLookTwiceRule1_ReconnectRestarts(t *testing.T) {
	w, sid := nakedShort(t, "lt-r1-conn", 27740)
	w.cancelStop(sid)
	w.look()

	w.redial()
	w.book("connect", w.lastBook...) // the book arrives, the positions frame does not yet
	w.sync()
	w.jump(3100 * time.Millisecond)
	w.look()
	w.mustBeSilent("the first look on the new connection")
	w.secondLook()
	w.mustBeSilent("two looks, but no positions frame on the new connection")
	w.positions(w.lastPos...)
	w.sync()
	if out, took := w.waitOrders(1, 12*time.Second); len(out) != 1 {
		t.Fatalf("once the new connection reported the position it must be protected, got %d after %s", len(out), took)
	}
}

// Rule 1 — a NEW connection that reports the position at once: still a first look.
func TestLookTwiceRule1_ReconnectIsAFirstLook(t *testing.T) {
	w, sid := nakedShort(t, "lt-r1-conn2", 27740)
	w.cancelStop(sid)
	w.look()
	w.redial()
	w.beat30()
	w.sync()
	w.jump(3100 * time.Millisecond)
	w.look()
	time.Sleep(300 * time.Millisecond)
	w.actsAfterRestart("the first look on the new connection")
}

// Rule 1 — two looks 1 s apart are not two looks.
func TestLookTwiceRule1_TooSoon(t *testing.T) {
	w, sid := nakedShort(t, "lt-r1-soon", 27740)
	w.cancelStop(sid)
	w.look()
	w.jump(time.Second)
	w.look()
	time.Sleep(300 * time.Millisecond)
	w.actsAfterRestart("1 s after the first look")
}

// Rule 1 — a fill after the first look: the same side, quantity and price again,
// 3.2 s after ITS fill, is that position's FIRST look.
func TestLookTwiceRule1_FillSinceTheFirstLookRestarts(t *testing.T) {
	w := open1193(t, "lt-r1-fill", 27740)
	w.stops1193(exits1193{})
	w.until(ms1193(249))
	w.look()
	w.jump(time.Second)
	const b = "reentry-b"
	w.row(b, "SHORT", 1, entry1193, w.now())
	w.mentorStop(b, "short", 1, 0, stop1193)
	w.orderUpdate(b, "filled", entry1193, 1)
	w.positions(short(1, entry1193))
	w.sync()
	w.jump(3200 * time.Millisecond)
	w.look()
	time.Sleep(300 * time.Millisecond)
	w.actsAfterRestart("B's first look")
}

// judge runs the gate alone, as one pass would with this evidence.
func (w *lookWire) judge(qty int, bookAt time.Time, prep func(*protectionHold)) (act bool, why string) {
	w.protState(func() {
		start := w.at.beginProtectionLookLocked(w.s)
		if h := w.at.protHolds["MNQ|SHORT"]; h != nil && prep != nil {
			prep(h)
		}
		act = w.at.protectionMayActLocked(protectionVerdict{Action: protectionPlace, StopPx: stop1193}, "MNQ|SHORT", "MNQ", "SHORT",
			qty, 27733.50, w.s, "Sim101", bookAt, start, 3*time.Second, "t") != nil
		if h := w.at.protHolds["MNQ|SHORT"]; h != nil {
			why = h.lastWhy
		}
	})
	return act, why
}

// A frame that lands while a pass is reading (its database reads can take
// seconds) must not be judged with the older half of the evidence, and a Stop
// that lands there must not send.
func TestLookTwiceGate_EvidenceChangedDuringThePass(t *testing.T) {
	bookAt := func(w *lookWire) time.Time {
		_, at, _ := w.s.OrderSnapshots().LatestReceived("Sim101")
		return at
	}
	proven := func(h *protectionHold) { h.proven = true }
	t.Run("positions", func(t *testing.T) {
		w, _ := nakedShort(t, "lt-g-pos", 27740)
		w.judge(2, bookAt(w), nil) // NT8 says 1
		w.jump(3100 * time.Millisecond)
		if act, why := w.judge(2, bookAt(w), proven); act || !strings.Contains(why, "position changed") {
			t.Fatalf("the newest frame shows another position: act=%v why=%q", act, why)
		}
	})
	t.Run("book", func(t *testing.T) {
		w, _ := nakedShort(t, "lt-g-book", 27740)
		old := bookAt(w)
		w.judge(1, old, nil)
		w.jump(3100 * time.Millisecond)
		w.beat30()
		w.sync()
		if act, why := w.judge(1, old, proven); act || !strings.Contains(why, "newer order book") {
			t.Fatalf("a newer book arrived: act=%v why=%q", act, why)
		}
		if act, _ := w.judge(1, bookAt(w), proven); !act {
			t.Fatal("control: with the newest book the same evidence acts")
		}
	})
	t.Run("stop", func(t *testing.T) {
		w, _ := nakedShort(t, "lt-g-stop", 27740)
		w.judge(1, bookAt(w), nil)
		w.jump(3100 * time.Millisecond)
		w.at.cancelStopMonitor()
		if act, _ := w.judge(1, bookAt(w), proven); act {
			t.Fatal("the Run ended while the pass was reading: nothing may be sent")
		}
	})
}

// Rule 1 — a Stop/Start in between: the new Run looks for itself.
func TestLookTwiceRule1_NewRunRestarts(t *testing.T) {
	w, sid := nakedShort(t, "lt-r1-run", 27740)
	w.cancelStop(sid)
	w.look()
	w.at.Stop()
	w.jump(3100 * time.Millisecond)
	w.at.reconcileProtectionAt(w.now(), "monitor") // a late beat on a stopped trader
	time.Sleep(300 * time.Millisecond)
	w.mustBeSilent("a stopped trader sends nothing")
	w.at.setupStopMonitorForTest()
	w.at.isRunningMutex.Lock()
	w.at.isRunning = true
	w.at.isRunningMutex.Unlock()
	w.look()
	time.Sleep(300 * time.Millisecond)
	w.actsAfterRestart("the new Run's first look")
}

// Rule 3 — our own close went out 0.2 s ago: no stop beside it.
func TestLookTwiceRule3_OwnCloseInFlight(t *testing.T) {
	w, sid := nakedShort(t, "lt-r3-close", 27740)
	w.cancelStop(sid)
	w.look()
	w.jump(2900 * time.Millisecond)
	if _, err := w.nt.CloseShort("MNQ", 0); err != nil {
		t.Fatal(err)
	}
	w.jump(200 * time.Millisecond)
	w.look()
	time.Sleep(400 * time.Millisecond)
	if n := w.count(ntwire.FramePlaceProtectiveStop); n != 0 {
		t.Fatalf("a close of ours is 0.2 s old: no stop may be placed beside it, got %d", n)
	}
}

// Rule 4 — no ack: hold for ever (no order frame), tell the owner at 35 s, and
// Stop() must not wait for the ack.
func TestLookTwiceRule4_NoAckHoldsAndStopIsPrompt(t *testing.T) {
	w, sid := nakedShort(t, "lt-r4", 27755) // would FLATTEN
	w.noAcks()
	w.cancelStop(sid)
	w.look()
	before := w.count(ntwire.FrameHeartbeat)
	w.secondLook()
	if n := w.count(ntwire.FrameHeartbeat); n != before+1 {
		t.Fatalf("the second look must send exactly ONE heartbeat as the link check, sent %d", n-before)
	}
	w.mustBeSilent("no ack yet")
	w.jump(6 * time.Second) // past the 5 s ack wait
	w.look()
	w.secondLook()
	w.mustBeSilent("no ack in 5 s means keep holding")
	w.mustHaveNoP0("before 35 s")
	w.jump(25 * time.Second)
	w.beat30()
	w.sync()
	w.look()
	if ps := w.p0s(); len(ps) != 1 || ps[0].Kind != alertKindProtectionHeld || !strings.Contains(ps[0].Body, "link check") {
		t.Fatalf("holding past 35 s = ONE %s P0 that says why, got %d: %+v", alertKindProtectionHeld, len(ps), ps)
	}
	w.jump(10 * time.Second)
	w.beat30()
	w.sync()
	w.look()
	if ps := w.p0s(); len(ps) != 1 {
		t.Fatalf("one hold = one P0, got %d", len(ps))
	}
	began := time.Now()
	w.at.Stop() // the re-look goroutine is waiting for an ack that never comes
	if took := time.Since(began); took > 2*time.Second {
		t.Fatalf("Stop() took %s: it must not wait for the ack", took)
	}
	if w.relookAlive() {
		t.Fatal("the re-look goroutine outlived Stop()")
	}
	w.mustBeSilent("after Stop")
}

// Rule 4 — an ack that answers an EARLIER heartbeat is not this one's.
func TestLookTwiceRule4_OlderAckDoesNotCount(t *testing.T) {
	w, sid := nakedShort(t, "lt-r4-old", 27740)
	w.noAcks()
	if _, err := w.s.SendLinkProbe(); err != nil { // an earlier heartbeat, still unanswered
		t.Fatal(err)
	}
	w.cancelStop(sid)
	w.look()
	w.secondLook()
	w.send(ntwire.FrameAck, ntwire.AckPayload{Acks: "heartbeat"}) // answers the earlier one
	w.sync()
	time.Sleep(400 * time.Millisecond)
	w.mustBeSilent("the ack of an earlier heartbeat")
	w.send(ntwire.FrameAck, ntwire.AckPayload{Acks: "heartbeat"}) // answers ours
	if out, took := w.waitOrders(1, 3*time.Second); len(out) != 1 {
		t.Fatalf("its own ack must release the hold at once, got %d after %s", len(out), took)
	}
}

// Rule 4 — an ack that comes after the 5 s answers nothing: a NEW check is sent.
func TestLookTwiceRule4_LateAckDoesNotCount(t *testing.T) {
	w, sid := nakedShort(t, "lt-r4-late", 27740)
	w.noAcks()
	w.cancelStop(sid)
	w.look()
	w.secondLook()
	w.jump(5100 * time.Millisecond)
	before := w.count(ntwire.FrameHeartbeat)
	w.send(ntwire.FrameAck, ntwire.AckPayload{Acks: "heartbeat"})
	w.sync()
	w.look()
	w.mustBeSilent("an ack 5.1 s after the check")
	if n := w.count(ntwire.FrameHeartbeat); n != before+1 {
		t.Fatalf("a new link check must replace the expired one, sent %d", n-before)
	}
}

// Rule 5 — the owner is told as soon as the veto is what holds it, not at 35 s.
func TestLookTwiceRule5_VetoTellsTheOwnerAtOnce(t *testing.T) {
	w := deadByReceipts(t, "lt-r5-p0")
	w.look()
	w.mustHaveNoP0("a first look")
	w.jump(5 * time.Second)
	w.beat30()
	w.sync()
	w.look()
	time.Sleep(300 * time.Millisecond)
	if ps := w.p0s(); len(ps) != 1 || ps[0].Kind != alertKindProtectionHeld || !strings.Contains(ps[0].Title, "already closed") {
		t.Fatalf("want ONE own-death P0, got %d: %+v", len(ps), ps)
	}
	w.mustBeSilent("P0 only")
}

// Rule 5 — a row closed by receipts that do NOT sum to its size is not a death:
// no veto (the runner is still there).
func TestLookTwiceRule5_PartialReceiptsAreNotADeath(t *testing.T) {
	w := deadByReceipts(t, "lt-r5-part")
	if err := w.st.GormDB().Model(&store.TraderPosition{}).Where("trader_id = ?", w.at.id).Update("entry_quantity", 4).Error; err != nil {
		t.Fatal(err)
	}
	w.look()
	w.jump(10 * time.Second)
	w.beat30()
	w.sync()
	w.look()
	if out, took := w.waitOrders(1, 2*time.Second); len(out) != 1 {
		t.Fatalf("receipts of 3 against an entry of 4: the rest must be protected, got %d after %s", len(out), took)
	}
}

// Rule 5 — a clock that stamps the exit in the future cannot stretch the veto:
// 60 s of hold is the ceiling.
func TestLookTwiceRule5_FutureExitTimeIsBounded(t *testing.T) {
	w := deadByReceipts(t, "lt-r5-skew")
	if err := w.st.GormDB().Model(&store.TraderPosition{}).Where("trader_id = ?", w.at.id).
		Update("exit_time", w.now().Add(2*time.Hour).UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	w.look()
	for i := 0; i < 2; i++ {
		w.jump(28 * time.Second)
		w.beat30()
		w.sync()
		w.look()
		time.Sleep(200 * time.Millisecond)
	}
	w.mustBeSilent("inside the 60 s veto")
	w.jump(8 * time.Second)
	w.beat30()
	w.sync()
	w.look()
	if out, took := w.waitOrders(1, 12*time.Second); len(out) != 1 {
		t.Fatalf("the veto must end at 60 s whatever the exit stamp says, got %d after %s", len(out), took)
	}
}

// Rule 5 — an OPEN row with a blank account is ours: nothing died, no veto.
func TestLookTwiceRule5_BlankAccountOpenRowIsOurs(t *testing.T) {
	w := deadByReceipts(t, "lt-r5-blank")
	p := w.row("other-sig", "SHORT", 1, entry1193, w.now())
	if err := w.st.GormDB().Model(&store.TraderPosition{}).Where("id = ?", p.ID).Update("account", "").Error; err != nil {
		t.Fatal(err)
	}
	w.look()
	w.jump(10 * time.Second)
	w.beat30()
	w.sync()
	w.look()
	if out, took := w.waitOrders(1, 2*time.Second); len(out) != 1 {
		t.Fatalf("an open row (blank account) is there: not our death, got %d after %s", len(out), took)
	}
}

// Rule 6 — the same book cannot justify a second stop, however old the first
// send is; a book received AFTER the send that still shows none does.
func TestLookTwiceRule6_RePlaceNeedsANewBook(t *testing.T) {
	w, sid := nakedShort(t, "lt-r6", 27740)
	w.cancelStop(sid)
	w.actsOnce(0)
	w.jump(10 * time.Second)
	w.look()
	w.secondLook()
	if n := w.count(ntwire.FramePlaceProtectiveStop); n != 1 {
		t.Fatalf("no book since the send: want 1 stop, got %d", n)
	}
	w.book("periodic", w.lastBook...)
	w.sync()
	w.jump(3100 * time.Millisecond)
	w.look()
	for deadline := time.Now().Add(8 * time.Second); w.count(ntwire.FramePlaceProtectiveStop) < 2; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("a book received after the send shows no stop: the position must be protected again")
		}
	}
}

// The re-look leaves the per-beat bookkeeping alone: the m217 pending-SL count
// is consecutive BEATS.
func TestLookTwiceRelookDoesNotCountAsABeat(t *testing.T) {
	const sid = "pend-sig"
	w := newLookWire(t, "lt-m217", ctInstant(t, "2026-11-18 10:00:05.000"))
	t.Cleanup(func() { clearPendingSLSeen("lt-m217", sid+"-sl") })
	w.price(27740)
	w.row(sid, "SHORT", 2, 27733.50, w.now())
	w.orderUpdate(sid, "filled", 27733.50, 2, sl(sid+"-sl", stop1193, 1, "Submitted")) // partial cover
	w.positions(short(2, 27733.50))
	w.sync()
	w.jump(10 * time.Second)
	w.beat30()
	w.sync()
	w.at.reconcileProtectionAt(w.now(), "monitor")
	w.mustHaveNoP0("first look at partial cover")
	for deadline := time.Now().Add(8 * time.Second); len(w.p0s()) == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("partial cover confirmed by the second look must raise its P0")
		}
	}
	if ps := w.p0s(); ps[0].Kind != "protection_partial" {
		t.Fatalf("kind = %s, want protection_partial", ps[0].Kind)
	}
	v, ok := pendingSLSeenMap.Load("lt-m217|" + sid + "-sl")
	if !ok {
		t.Fatal("fixture: the beat must have counted the pending -sl once")
	}
	if n := v.(*pendingSLSeen).Count; n != 1 {
		t.Fatalf("one beat and its re-looks = count %d, want 1", n)
	}
	w.mustBeSilent("partial cover is alert-only")
}

// A cleared hold ends the goroutine; a later hold starts a new one.
func TestLookTwiceRelookEndsWithTheHold(t *testing.T) {
	w, sid := nakedShort(t, "lt-life", 27740)
	for i := 0; i < 2; i++ {
		w.cancelStop(sid)
		w.at.reconcileProtectionAt(w.now(), "monitor")
		if w.holds() != 1 || !w.relookAlive() {
			t.Fatalf("round %d: one hold (%d) and a live re-look (%v)", i, w.holds(), w.relookAlive())
		}
		w.orderUpdate(sid+"-sl", "working", 0, 0, sl(sid+"-sl", stop1193, 1, "Working"), tp(sid+"-tp", 27700, 1, "Working"))
		w.sync()
		for deadline := time.Now().Add(6 * time.Second); w.relookAlive(); time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("round %d: the re-look goroutine outlived its hold", i)
			}
		}
		if w.holds() != 0 {
			t.Fatalf("round %d: the hold outlived the cover", i)
		}
	}
	w.cancelStop(sid)
	w.at.reconcileProtectionAt(w.now(), "monitor")
	w.positions() // flat: the hold ends with the position, not 70 s later
	w.sync()
	for deadline := time.Now().Add(6 * time.Second); w.relookAlive() || w.holds() != 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("flat: holds %d, goroutine %v", w.holds(), w.relookAlive())
		}
	}
	w.mustBeSilent("the stop came back twice, then flat")
}

// PROTECTION_CONFIRM_MS=0 puts nothing new on the wire: no link check, no hold,
// no goroutine — place_protective_stop is the first frame after the look.
func TestLookTwiceConfirmZeroSendsNoLinkCheck(t *testing.T) {
	t.Setenv(lookTwiceEnv, "0")
	w, sid := nakedShort(t, "lt-zero-wire", 27740)
	w.cancelStop(sid)
	beats := w.count(ntwire.FrameHeartbeat)
	w.look()
	if n := w.count(ntwire.FramePlaceProtectiveStop); n != 1 {
		t.Fatalf("the old single look places at once, got %d", n)
	}
	if n := w.count(ntwire.FrameHeartbeat); n != beats || w.holds() != 0 || w.relookAlive() {
		t.Fatalf("revert must add nothing: heartbeats +%d, holds %d, goroutine %v", n-beats, w.holds(), w.relookAlive())
	}
}

// No deadlock: passes, the re-look, whole-position flattens, rejected-close
// reopens (both take limitFlattenMu) and AddOn frames (the read loop) at once,
// then Stop in the middle of it. Nothing may go out after Stop returned.
func TestLookTwiceNoDeadlockWithFlattensReadLoopAndStop(t *testing.T) {
	w, sid := nakedShort(t, "lt-locks", 27755)
	w.cancelStop(sid)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	spin := func(every time.Duration, fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				case <-time.After(every):
					fn()
				}
			}
		}()
	}
	for i := 0; i < 3; i++ {
		spin(time.Millisecond, func() { w.at.reconcileProtectionAt(w.now(), "monitor") })
	}
	spin(3*time.Millisecond, func() {
		w.at.flattenPositionAtMarket(&store.TraderPosition{TraderID: w.at.id, Symbol: "MNQ", Side: "SHORT"}, "t", w.now(), nil)
	})
	spin(2*time.Millisecond, func() {
		w.at.reopenRejectedExit(ntwire.PositionCloseRejectedPayload{Account: "Sim101", Symbol: "MNQ", PositionSide: "SHORT", Reason: "t"})
	})
	spin(2*time.Millisecond, func() {
		w.positions(short(1, 27733.50))
		w.book("periodic", tp(sid+"-tp", 27700, 1, "Working"))
	})
	time.Sleep(4 * time.Second)
	close(stop)
	done := make(chan struct{})
	go func() { wg.Wait(); w.at.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("deadlock: passes / flattens / Stop did not finish in 20 s")
	}
	time.Sleep(100 * time.Millisecond) // frames already on the wire reach the fixture
	n := len(w.ordersOut())
	time.Sleep(4 * time.Second)
	if got := len(w.ordersOut()); got != n {
		t.Fatalf("%d order frame(s) went out after Stop returned", got-n)
	}
}

// The 654 floor through the DEFAULT path: no mentor record, an accepted BE stop
// in the book history, a wider plan. The second look places the BE price.
func TestLookTwiceKnownBrokerStopIsPlacedBySecondLook(t *testing.T) {
	const sid = "known-sig"
	w := newLookWire(t, "lt-known", ctInstant(t, "2026-11-18 10:00:05.000"))
	w.price(27740)
	w.row(sid, "SHORT", 1, 27733.50, w.now())
	w.orderUpdate(sid, "filled", 27733.50, 1)
	w.positions(short(1, 27733.50))
	w.sync()
	b, _ := json.Marshal([]ntwire.NT8Order{sl(sid+"-sl", 27751.25, 1, "Accepted")})
	if err := w.st.NT8OrderSnapshots().Insert(&store.NT8OrderSnapshot{Account: "Sim101", OrdersJSON: string(b), OrderCount: 1,
		ReceivedMs: w.now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	w.jump(10 * time.Second)
	w.beat30()
	w.sync()
	got := w.actsOnce(2900 * time.Millisecond)
	if !strings.HasPrefix(got, string(ntwire.FramePlaceProtectiveStop)) || !strings.Contains(got, `"stop_price":27751.25`) {
		t.Fatalf("want the known broker stop 27751.25 placed, got %s", got)
	}
}
