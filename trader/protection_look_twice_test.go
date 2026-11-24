package trader

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── LOOK TWICE (spec 2026-11-24, incident 11-19 06:47:30, row 1194 −1207.00) ─
//
// The RED suite of spec section 9. Everything here goes in as REAL FRAMES on a
// dialed AddOn socket (read loop → receipt clocks → order book → ordered worker
// → ApplyNT8Exit) and every assertion is on the frames the server WROTE OUT.
//
// TIME. The fixture's clock is wall time SHIFTED to the incident's date, so a
// test runs at real pace but never depends on whether the CME is open today.
// ONE clock: it stamps the received frames (TCPServer.SetReceiptClockForTest)
// and it is the `now` of every pass — the background re-look must read it too
// (TCPServer.ReceiptNow), or its now and the frame stamps disagree by days.
// jump() moves it forward; the 3 s background re-look itself is real time.
//
// The trader is marked running the way Run leaves it (stop monitor, running
// flag, close-sync, ordered worker, post-exit registry), so a re-look that
// refuses to run on a stopped trader still runs here.

const lookTwiceEnv = "PROTECTION_CONFIRM_MS"

type lookWire struct {
	*execWire
	t        *testing.T
	clkMu    sync.Mutex // guards base + start (read by the server's read loop)
	base     time.Time
	start    time.Time
	wmu      sync.Mutex // one writer at a time on the AddOn socket
	stopAck  chan struct{}
	stopOnce sync.Once
	ackWG    sync.WaitGroup
	lastPos  []ntwire.OpenPosition
	lastBook []ntwire.NT8Order
}

func (w *lookWire) now() time.Time {
	w.clkMu.Lock()
	defer w.clkMu.Unlock()
	return w.base.Add(time.Since(w.start))
}

// setNow puts the fixture clock at t; it runs on at real pace from there.
func (w *lookWire) setNow(t time.Time) {
	w.clkMu.Lock()
	defer w.clkMu.Unlock()
	w.base, w.start = t, time.Now()
}

// jump moves the fixture clock forward without waiting.
func (w *lookWire) jump(d time.Duration) { w.setNow(w.now().Add(d)) }

// until waits (real pace) for the fixture clock to reach 06:47:30 + d.
func (w *lookWire) until(d time.Duration) {
	if wait := at1193(w.t, d).Sub(w.now()); wait > 0 {
		time.Sleep(wait)
	}
}

// newLookWire starts the fixture with its clock at base.
func newLookWire(t *testing.T, traderID string, base time.Time) *lookWire {
	t.Helper()
	w := &lookWire{execWire: newExecWire(t, traderID), t: t, base: base, start: time.Now(), stopAck: make(chan struct{})}
	w.s.SetReceiptClockForTest(w.now)
	nttrader.ResetPricedCloseCacheForTest()

	// the running state Run leaves behind, without Run's loops.
	w.at.setupStopMonitorForTest()
	w.at.isRunningMutex.Lock()
	w.at.isRunning = true
	w.at.isRunningMutex.Unlock()
	w.at.kickCh = make(chan string, 4)
	t.Setenv("POST_EXIT_RESCAN", "0")
	registerPostExitDispatch(w.at)
	w.nt.StartCloseSync(w.at.id, "ex", "ninjatrader", w.st)
	if err := w.at.installNTOrderedExecutions(w.nt); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.at.Stop() })
	// The verdict memory is process-global by trader id: an alert-only verdict is
	// raised on a CHANGE only, so a -count=2 run would inherit the first run's.
	t.Cleanup(func() { w.at.sweepStaleProtectionState(nil) })

	// The AddOn's answers: every Go heartbeat is acked on the same ordered stream
	// (VLTraderTCPClient.cs HandleMessage "heartbeat" → SendAck).
	w.ackWG.Add(1)
	go func() {
		defer w.ackWG.Done()
		acked := 0
		for {
			select {
			case <-w.stopAck:
				return
			case <-time.After(2 * time.Millisecond):
			}
			w.mu.Lock()
			beats := 0
			for _, e := range w.frames {
				if e.Type == ntwire.FrameHeartbeat {
					beats++
				}
			}
			w.mu.Unlock()
			for ; acked < beats; acked++ {
				w.wmu.Lock()
				_ = ntwire.WriteFrame(w.conn, ntwire.FrameAck, ntwire.AckPayload{Acks: "heartbeat"})
				w.wmu.Unlock()
			}
		}
	}()
	t.Cleanup(func() { w.stopOnce.Do(func() { close(w.stopAck) }); w.ackWG.Wait() })
	return w
}

// send writes one AddOn frame.
func (w *lookWire) send(ft ntwire.FrameType, p any) {
	w.t.Helper()
	w.wmu.Lock()
	defer w.wmu.Unlock()
	if err := ntwire.WriteFrame(w.conn, ft, p); err != nil {
		w.t.Fatalf("fixture: write %s: %v", ft, err)
	}
}

func (w *lookWire) count(ft ntwire.FrameType) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, e := range w.frames {
		if e.Type == ft {
			n++
		}
	}
	return n
}

// sync returns once the read loop has processed every frame sent so far (the
// server acks an AddOn heartbeat from that same loop).
func (w *lookWire) sync() {
	w.t.Helper()
	before := w.count(ntwire.FrameAck)
	w.send(ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: ntwire.MinAddonBuildPictureHtf})
	for deadline := time.Now().Add(5 * time.Second); w.count(ntwire.FrameAck) == before; {
		if time.Now().After(deadline) {
			w.t.Fatal("fixture: the read loop did not answer a heartbeat in 5 s")
		}
		time.Sleep(time.Millisecond)
	}
}

func (w *lookWire) positions(ps ...ntwire.OpenPosition) {
	if ps == nil {
		ps = []ntwire.OpenPosition{}
	}
	w.lastPos = ps
	w.send(ntwire.FramePositions, ntwire.PositionsPayload{Account: "Sim101", Positions: ps})
}

func (w *lookWire) book(reason string, orders ...ntwire.NT8Order) {
	if orders == nil {
		orders = []ntwire.NT8Order{}
	}
	w.lastBook = orders
	w.send(ntwire.FrameOrderSnapshot, ntwire.OrderSnapshotPayload{
		Account: "Sim101", EmittedMs: w.now().UnixMilli(), Reason: reason, Orders: orders})
}

// beat30 is the AddOn's 30 s heartbeat resend: positions and the book, unchanged.
func (w *lookWire) beat30() {
	w.positions(w.lastPos...)
	w.book("periodic", w.lastBook...)
}

// orderUpdate sends the state change and, as SendOrderUpdateFrame does, the book
// that follows it.
func (w *lookWire) orderUpdate(name, state string, px float64, qty int, book ...ntwire.NT8Order) {
	w.send(ntwire.FrameOrderUpdate, ntwire.OrderUpdatePayload{SignalID: legBase(name), OrderName: name,
		State: state, FillPrice: px, Quantity: qty, Symbol: "MNQ", Account: "Sim101"})
	w.book("state_change", book...)
}

// legBase strips the leg suffix the way the AddOn does.
func legBase(name string) string {
	for _, s := range []string{"-sl3", "-tp3", "-sl2", "-tp2", "-sl", "-tp", "-lx"} {
		if strings.HasSuffix(name, s) {
			return strings.TrimSuffix(name, s)
		}
	}
	return name
}

func (w *lookWire) closeFrame(signal, side, reason string, px float64, qty, leg int, exit time.Time) ntwire.PositionClosePayload {
	p := ntwire.PositionClosePayload{SignalID: signal, Symbol: "MNQ", PositionSide: side, ExitPrice: px,
		Quantity: qty, ExitReason: reason, ExitTime: exit.UTC().Format(time.RFC3339Nano), Leg: leg, Account: "Sim101"}
	w.send(ntwire.FramePositionClose, p)
	return p
}

// price serves a fresh 1m close to the through-check.
func (w *lookWire) price(px float64) {
	w.t.Helper()
	orig := market.FuturesBarsProvider
	w.t.Cleanup(func() { market.FuturesBarsProvider = orig })
	market.FuturesBarsProvider = func(_, _ string, _ int) []market.Kline {
		return []market.Kline{{OpenTime: w.now().Truncate(time.Minute).UnixMilli(), Close: px}}
	}
}

// mentorStop registers the mentor driver's live position for one side.
func (w *lookWire) mentorStop(sid, side string, qty1, qty2 int, stop float64) {
	lp := &mentorLivePos{Pos: mentorPosition{Symbol: "MNQ", Side: side, Contracts: qty1 + qty2}}
	lp.Legs[0] = mentorLeg{SignalID: sid, Qty: qty1, Stop: stop, Wire: 1, Final: qty2 == 0}
	if qty2 > 0 {
		lp.Legs[1] = mentorLeg{SignalID: sid, Qty: qty2, Stop: stop, Wire: 2, Final: true}
	}
	w.at.mentorExitMu.Lock()
	if w.at.mentorLivePos == nil {
		w.at.mentorLivePos = map[string]*mentorLivePos{}
	}
	w.at.mentorLivePos[sid] = lp
	w.at.mentorExitMu.Unlock()
}

func (w *lookWire) row(sid, side string, qty, entry float64, entered time.Time) *store.TraderPosition {
	w.t.Helper()
	p := &store.TraderPosition{TraderID: w.at.id, ExchangeType: "ninjatrader", Symbol: "MNQ", Side: side,
		Quantity: qty, EntryQuantity: qty, EntryPrice: entry, EntryOrderID: sid, EntryTime: entered.UnixMilli(),
		Leverage: 1, Status: "OPEN", Source: "armed_entry", Account: "Sim101"}
	if err := w.st.Position().Create(p); err != nil {
		w.t.Fatal(err)
	}
	return p
}

// receipts waits until the ordered worker has stored n exit receipts for the
// bound account with this applied flag.
func (w *lookWire) receipts(n int, applied bool) {
	w.t.Helper()
	var got int64
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
		w.st.GormDB().Model(&store.NT8ExitReceipt{}).Where("account = ? AND applied = ?", "Sim101", applied).Count(&got)
		if got >= int64(n) {
			return
		}
	}
	w.t.Fatalf("fixture: %d exit receipt(s) applied=%v stored, want %d", got, applied, n)
}

// ordersOut is every order-affecting frame the bot has written, as JSON.
func (w *lookWire) ordersOut() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, e := range w.frames {
		switch e.Type {
		case ntwire.FrameClosePosition, ntwire.FramePlaceProtectiveStop, ntwire.FrameSignal, ntwire.FrameMoveStop:
			out = append(out, fmt.Sprintf("%s %s", e.Type, string(e.Payload)))
		}
	}
	return out
}

func (w *lookWire) p0s() []*store.AlertDB {
	w.t.Helper()
	all, err := w.st.Alert().List(w.at.id, 200)
	if err != nil {
		w.t.Fatal(err)
	}
	var out []*store.AlertDB
	for _, a := range all {
		if a.Level == "P0" {
			out = append(out, a)
		}
	}
	return out
}

func (w *lookWire) mustBeSilent(when string) {
	w.t.Helper()
	if out := w.ordersOut(); len(out) != 0 {
		w.t.Fatalf("%s: the bot sent %d order frame(s), want 0:\n  %s", when, len(out), strings.Join(out, "\n  "))
	}
}

func (w *lookWire) mustHaveNoP0(when string) {
	w.t.Helper()
	if ps := w.p0s(); len(ps) != 0 {
		w.t.Fatalf("%s: %d P0 alert(s), want 0 — first: kind=%s title=%q body=%q", when, len(ps), ps[0].Kind, ps[0].Title, ps[0].Body)
	}
}

// look is one protection pass at the fixture's now, followed by a short wait for
// anything it (or a background re-look) puts on the wire.
func (w *lookWire) look() {
	w.at.reconcileProtectionAt(w.now(), "monitor")
	time.Sleep(120 * time.Millisecond)
}

// secondLook is a pass 3.1 s later with the link alive, plus time for an
// asynchronous heartbeat-ack link check to finish.
func (w *lookWire) secondLook() {
	w.jump(3100 * time.Millisecond)
	w.at.reconcileProtectionAt(w.now(), "monitor")
	time.Sleep(400 * time.Millisecond)
}

// waitOrders waits up to d for n order frames and returns them with the wait.
func (w *lookWire) waitOrders(n int, d time.Duration) ([]string, time.Duration) {
	began := time.Now()
	for {
		out := w.ordersOut()
		if len(out) >= n || time.Since(began) > d {
			return out, time.Since(began)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// actsOnce: the naked position of the fixture must be acted on EXACTLY once by
// the background re-look — no second beat is run. notBefore > 0 also pins that
// nothing goes out earlier than that after the first look.
func (w *lookWire) actsOnce(notBefore time.Duration) string {
	w.t.Helper()
	began := time.Now()
	w.at.reconcileProtectionAt(w.now(), "monitor")
	out, _ := w.waitOrders(1, 12*time.Second)
	took := time.Since(began)
	if len(out) == 0 {
		w.t.Fatalf("a truly naked position was NOT protected within %s", took.Round(time.Millisecond))
	}
	if took < notBefore {
		w.t.Fatalf("acted %s after the first look, want no earlier than %s (one look must never act): %s",
			took.Round(time.Millisecond), notBefore, out[0])
	}
	time.Sleep(700 * time.Millisecond)
	if out = w.ordersOut(); len(out) != 1 {
		w.t.Fatalf("want exactly ONE order frame, got %d:\n  %s", len(out), strings.Join(out, "\n  "))
	}
	return out[0]
}

func ctInstant(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02 15:04:05.000", s, chicagoLoc())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func sl(name string, stop float64, qty int, state string) ntwire.NT8Order {
	return ntwire.NT8Order{OrderID: "o-" + name, Symbol: "MNQ", Name: name, Action: "buy", Type: "stop",
		StopPrice: stop, Quantity: qty, State: state, OCO: legBase(name) + "-exit", TimeInForce: "Gtc"}
}

func tp(name string, limit float64, qty int, state string) ntwire.NT8Order {
	return ntwire.NT8Order{OrderID: "o-" + name, Symbol: "MNQ", Name: name, Action: "buy", Type: "limit",
		LimitPrice: limit, Quantity: qty, State: state, OCO: legBase(name) + "-exit", TimeInForce: "Gtc"}
}

func short(qty int, avg float64) ntwire.OpenPosition {
	return ntwire.OpenPosition{Symbol: "MNQ", Side: "short", Quantity: qty, AvgPrice: avg}
}

func long(qty int, avg float64) ntwire.OpenPosition {
	return ntwire.OpenPosition{Symbol: "MNQ", Side: "long", Quantity: qty, AvgPrice: avg}
}

// ── the 11-19 06:47:30 timeline ──────────────────────────────────────────────

const (
	sid1193   = "3daa05cc-967e-4276-98ef-abff1ca15697"
	entry1193 = 27733.50
	stop1193  = 27751.25
)

// ms1193 is 06:47:30.<ms> CT.
func ms1193(ms int) time.Duration { return time.Duration(ms) * time.Millisecond }

// at1193 is 06:47:30.000 CT on 11-19, plus d.
func at1193(t *testing.T, d time.Duration) time.Time {
	t.Helper()
	return ctInstant(t, "2026-11-19 06:47:30.000").Add(d)
}

// exits1193 drops or repeats evidence of the two stop legs.
type exits1193 struct{ noOrderUpdate, noReceipt, duplicate bool }

// open1193 opens row 1193 on the wire: SHORT 3 @27733.50, split bracket 2+1, the
// book of snapshot 502905 and the positions frame, 13 minutes before the stop.
func open1193(t *testing.T, traderID string, px float64) *lookWire {
	t.Helper()
	w := newLookWire(t, traderID, ctInstant(t, "2026-11-19 06:34:37.052"))
	w.price(px)
	w.row(sid1193, "SHORT", 3, entry1193, w.now())
	w.mentorStop(sid1193, "short", 2, 1, stop1193)
	full := w.full1193()
	w.orderUpdate(sid1193, "filled", entry1193, 3, full...)
	w.positions(short(3, entry1193))
	w.sync()
	w.at.reconcileProtectionAt(w.now(), "monitor") // protected: a quiet pass
	w.mustBeSilent("fixture: the covered position")

	// 06:47:30.095: the stops go Working (snapshot 502905).
	w.setNow(at1193(t, 0))
	w.until(ms1193(60))
	w.positions(short(3, entry1193)) // the 30 s heartbeat resend
	w.until(ms1193(95))
	w.orderUpdate(sid1193+"-sl", "working", 0, 0, full...)
	w.orderUpdate(sid1193+"-sl2", "working", 0, 0, full...)
	return w
}

func (w *lookWire) full1193() []ntwire.NT8Order {
	return []ntwire.NT8Order{
		sl(sid1193+"-sl", stop1193, 2, "Working"), tp(sid1193+"-tp", 27715.75, 2, "Working"),
		sl(sid1193+"-sl2", stop1193, 1, "Working"), tp(sid1193+"-tp2", 27133.50, 1, "Working"),
	}
}

// stops1193 plays .157 → .230: leg 1 fills 2, the positions frame "Short 1" of
// .186, leg 2 fills 1 and the stop-less books 502916-502918, receipt LAST.
func (w *lookWire) stops1193(x exits1193) {
	w.t.Helper()
	sl2 := sl(sid1193+"-sl2", stop1193, 1, "Working")
	tp2 := func(s string) ntwire.NT8Order { return tp(sid1193+"-tp2", 27133.50, 1, s) }
	tp1 := func(s string) ntwire.NT8Order { return tp(sid1193+"-tp", 27715.75, 2, s) }
	exitOU := func(name string, qty int, book ...ntwire.NT8Order) {
		if x.noOrderUpdate {
			w.book("state_change", book...)
			return
		}
		w.orderUpdate(name, "filled", stop1193, qty, book...)
		if x.duplicate {
			w.orderUpdate(name, "filled", stop1193, qty, book...)
		}
	}
	receipt := func(qty, leg, ms int) {
		if x.noReceipt {
			return
		}
		exit := at1193(w.t, ms1193(ms))
		w.closeFrame(sid1193, "short", "sl", stop1193, qty, leg, exit)
		if x.duplicate {
			w.closeFrame(sid1193, "short", "sl", stop1193, qty, leg, exit)
		}
	}

	w.until(ms1193(157))
	exitOU(sid1193+"-sl", 2, tp1("Working"), sl2, tp2("Working")) // 502909
	receipt(2, 1, 157)
	w.orderUpdate(sid1193+"-tp", "cancelpending", 0, 0, tp1("CancelPending"), sl2, tp2("Working"))     // 502910
	w.orderUpdate(sid1193+"-tp", "cancelsubmitted", 0, 0, tp1("CancelSubmitted"), sl2, tp2("Working")) // 502911
	w.until(ms1193(186))
	w.positions(short(1, entry1193))
	w.until(ms1193(225))
	exitOU(sid1193+"-sl2", 1, tp1("CancelSubmitted"), tp2("Working"))                                      // 502916
	w.orderUpdate(sid1193+"-tp2", "cancelpending", 0, 0, tp1("CancelSubmitted"), tp2("CancelPending"))     // 502917
	w.orderUpdate(sid1193+"-tp2", "cancelsubmitted", 0, 0, tp1("CancelSubmitted"), tp2("CancelSubmitted")) // 502918
	w.until(ms1193(230))
	receipt(1, 2, 226)
	w.sync()
	if !x.noReceipt {
		w.receipts(2, true) // log 390443: row 1193 closed at .232, BEFORE the pass
		if rows, err := w.st.Position().GetOpenPositions(w.at.id); err != nil || len(rows) != 0 {
			w.t.Fatalf("fixture: row 1193 must be CLOSED by its own receipts before .249, open=%d err=%v", len(rows), err)
		}
	}
}

// flat1193 is the .254 positions frame (count=0) and the dying target leaving.
func (w *lookWire) flat1193() {
	w.positions()
	w.orderUpdate(sid1193+"-tp", "cancelled", 0, 0, tp(sid1193+"-tp2", 27133.50, 1, "CancelSubmitted")) // 502921
	w.orderUpdate(sid1193+"-tp2", "cancelled", 0, 0)
	w.sync()
}

// TestLookTwice01_Replay0647 — spec 9.1. The real ms timeline of 11-19 06:47:30:
// the pass at .249 sees "Short 1" (positions frame of .186), no stop in the book
// and the price AT the stop, 24 ms after the broker filled that stop. Today it
// sends close_position; NT8 is flat by then and the flatten OPENS LONG 1 (row
// 1194, −1207.00). Want: 0 closes, 0 stops, 0 P0, ever.
func TestLookTwice01_Replay0647(t *testing.T) {
	w := open1193(t, "lt-0647", stop1193) // the fresh 1m price EQUALS the stop (27751.25)
	w.stops1193(exits1193{})
	w.until(ms1193(249))
	w.look() // log 390444: "price already through the intended stop — flattened"
	w.mustBeSilent("06:47:30.249 first look")

	w.flat1193() // .254
	w.secondLook()
	w.mustBeSilent("06:47:33 second look")
	time.Sleep(3500 * time.Millisecond) // the real-time re-look, if one is armed
	w.jump(57 * time.Second)
	w.beat30()
	w.sync()
	w.look() // the next 60 s beat
	w.mustBeSilent("06:48:30 next beat")
	w.mustHaveNoP0("the whole replay")
}

// TestLookTwice02_Replay0647SafeSide — spec 9.2: the same timeline with the price
// on the protective side. Today the verdict is PLACE: a GTC stop with no OCO on
// an account that is about to be flat.
func TestLookTwice02_Replay0647SafeSide(t *testing.T) {
	w := open1193(t, "lt-0647-safe", 27750.00)
	w.stops1193(exits1193{})
	w.until(ms1193(249))
	w.look()
	w.mustBeSilent("06:47:30.249 first look")
	w.flat1193()
	w.secondLook()
	time.Sleep(3500 * time.Millisecond)
	w.mustBeSilent("after the second look")
	w.mustHaveNoP0("the whole replay")
}

// TestLookTwice03_EvidenceVariants — spec 9.3. Either exit frame alone must hold
// the fence: with the flat positions frame LOST, the second look still sees
// "Short 1" from .186 and must not act on a frame older than the exit fill. With
// both exit frames lost NT8's own Flat frame is what saves it.
func TestLookTwice03_EvidenceVariants(t *testing.T) {
	for _, c := range []struct {
		name      string
		x         exits1193
		flatArriv bool
	}{
		{"receipt missing", exits1193{noReceipt: true}, false},
		{"order_update missing", exits1193{noOrderUpdate: true}, false},
		{"both missing, NT8 says flat", exits1193{noOrderUpdate: true, noReceipt: true}, true},
		{"duplicates", exits1193{duplicate: true}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := open1193(t, "lt-ev-"+strings.ReplaceAll(c.name, " ", "-"), stop1193)
			w.stops1193(c.x)
			w.until(ms1193(249))
			w.look()
			w.mustBeSilent("first look")
			if c.flatArriv {
				w.flat1193()
			}
			w.secondLook()
			w.mustBeSilent("second look")
		})
	}
}

// TestLookTwice03_ReusedSLName — spec 9.3: the reconciler's own re-placed stop is
// named "<signal>-sl" again. Leg 1's "-sl" filled 2 earlier, so this fill's
// cumulative 1 is SMALLER. It is still a new fill and must move the fence.
func TestLookTwice03_ReusedSLName(t *testing.T) {
	w := open1193(t, "lt-reused-sl", stop1193)
	sl2 := sl(sid1193+"-sl2", stop1193, 1, "Working")
	w.orderUpdate(sid1193+"-sl", "filled", stop1193, 2, sl2)
	w.closeFrame(sid1193, "short", "sl", stop1193, 2, 1, w.now())
	w.positions(short(1, entry1193))
	w.sync()
	w.receipts(1, true)
	w.jump(5 * time.Minute) // leg 2's stop was lost and re-placed as "<signal>-sl"
	w.book("state_change", sl(sid1193+"-sl", stop1193, 1, "Working"))
	w.positions(short(1, entry1193))
	w.sync()
	w.jump(20 * time.Second) // now it fills; the positions frame is older than the fill
	w.orderUpdate(sid1193+"-sl", "filled", stop1193, 1)
	w.closeFrame(sid1193, "short", "sl", stop1193, 1, 0, w.now())
	w.sync()
	w.receipts(2, true)
	w.look()
	w.mustBeSilent("first look after the reused -sl filled")
	w.secondLook()
	w.mustBeSilent("second look: the only positions frame is older than the reused -sl fill")
}

// TestLookTwice03_HoldP0At35s — rule B's ceiling: an exit fill was seen but the
// positions frame that follows it is LOST. The hold sends nothing, and after 35 s
// (one 30 s AddOn heartbeat + 5) the owner is told.
func TestLookTwice03_HoldP0At35s(t *testing.T) {
	w := open1193(t, "lt-hold-35", stop1193)
	w.stops1193(exits1193{noReceipt: true})
	w.until(ms1193(249))
	w.look()
	for i := 0; i < 3; i++ { // +10 … +30 s: books arrive, positions frames do not
		w.jump(10 * time.Second)
		w.book("periodic", w.lastBook...)
		w.sync()
		w.at.reconcileProtectionAt(w.now(), "monitor")
		time.Sleep(200 * time.Millisecond)
	}
	w.mustBeSilent("holding on a positions frame older than the exit fill")
	w.mustHaveNoP0("before 35 s")
	w.jump(8 * time.Second)
	w.book("periodic", w.lastBook...)
	w.sync()
	w.at.reconcileProtectionAt(w.now(), "monitor")
	time.Sleep(400 * time.Millisecond)
	w.mustBeSilent("at 38 s")
	if len(w.p0s()) == 0 {
		t.Fatal("still holding past 35 s must raise a P0")
	}
}

// TestLookTwice04_HeartbeatShort1 — spec 9.4: a 30 s heartbeat positions frame
// emitted between the fill and NT8's own position update is received AFTER the
// exit evidence and still says Short 1. "A frame newer than the fill" alone is
// not proof; the second look is.
func TestLookTwice04_HeartbeatShort1(t *testing.T) {
	w := open1193(t, "lt-hb", stop1193)
	w.stops1193(exits1193{})
	w.until(ms1193(240))
	w.positions(short(1, entry1193)) // the heartbeat, newer than every exit frame
	w.sync()
	w.until(ms1193(249))
	w.look()
	w.mustBeSilent("first look on a heartbeat frame newer than the fill")
	w.flat1193()
	w.secondLook()
	time.Sleep(3500 * time.Millisecond)
	w.mustBeSilent("after NT8 said flat")
	w.mustHaveNoP0("the whole replay")
}

// nakedShort is one SHORT contract with a working bracket; the test then takes
// its stop away.
func nakedShort(t *testing.T, id string, px float64) (*lookWire, string) {
	t.Helper()
	const sid = "naked-sig"
	w := newLookWire(t, id, ctInstant(t, "2026-11-18 10:00:05.000"))
	w.price(px)
	w.row(sid, "SHORT", 1, 27733.50, w.now())
	w.mentorStop(sid, "short", 1, 0, stop1193)
	b := []ntwire.NT8Order{sl(sid+"-sl", stop1193, 1, "Working"), tp(sid+"-tp", 27700, 1, "Working")}
	w.orderUpdate(sid, "filled", 27733.50, 1, b...)
	w.positions(short(1, 27733.50))
	w.sync()
	w.look()
	w.mustBeSilent("fixture: the covered position")
	w.jump(10 * time.Minute)
	w.beat30()
	w.sync()
	return w, sid
}

// cancelStop: the stop leaves the book by CANCEL, no fill. Truly naked.
func (w *lookWire) cancelStop(sid string) {
	w.orderUpdate(sid+"-sl", "cancelled", 0, 0, tp(sid+"-tp", 27700, 1, "Working"))
	w.sync()
}

// TestLookTwice05_FrozenReadLoop — spec 9.5 (H1): the owner presses Close. The
// stop is cancelled, the stop-less book is cached, then the read loop freezes in
// the order-snapshot sink (main.go: a synchronous DB insert, busy 5000 ms) while
// the Close fill and the Flat frame wait in the socket. Time alone must not act;
// the heartbeat ack, behind the same frames, is what proves the link caught up.
func TestLookTwice05_FrozenReadLoop(t *testing.T) {
	w, sid := nakedShort(t, "lt-frozen", 27740)
	release := make(chan struct{})
	frozen := make(chan struct{})
	var once sync.Once
	var free sync.Once
	unfreeze := func() { free.Do(func() { close(release) }) }
	t.Cleanup(unfreeze)
	w.s.SetOrderSnapshotSink(func(ntwire.OrderSnapshotPayload) {
		once.Do(func() { close(frozen) })
		<-release
	})
	w.orderUpdate(sid+"-sl", "cancelled", 0, 0)
	select {
	case <-frozen:
	case <-time.After(5 * time.Second):
		t.Fatal("fixture: the sink never ran")
	}
	// waiting in the socket behind the frozen loop:
	w.orderUpdate("Close", "filled", 27740, 1)
	w.closeFrame("Close", "short", "manual", 27740, 1, 0, w.now())
	w.positions()

	w.look()
	w.mustBeSilent("first look")
	time.Sleep(5500 * time.Millisecond) // the 3 s re-look and its 5 s ack wait pass
	w.at.reconcileProtectionAt(w.now(), "monitor")
	w.mustBeSilent("5.5 s later, read loop still frozen: no ack, no action")
	unfreeze()
	w.sync()
	w.look()
	time.Sleep(3500 * time.Millisecond)
	w.mustBeSilent("after the loop caught up (NT8 is flat)")
}

// TestLookTwice06_SameSideReentry — spec 9.6 (H2): A stops out and is held. 2.9 s
// later a same-side re-entry B fills at the SAME quantity and price, its bracket
// not in the book yet. The look at +3 s is B's FIRST look, whatever A's was.
func TestLookTwice06_SameSideReentry(t *testing.T) {
	w := open1193(t, "lt-reentry", 27740) // safe side: a wrong act would PLACE
	w.stops1193(exits1193{})
	w.until(ms1193(249))
	w.look()
	w.mustBeSilent("A's first look")
	w.positions() // A is flat; no pass runs while it is
	w.sync()
	w.jump(2800 * time.Millisecond) // past 3 s from A's first look once B's 150 ms are added
	const b = "reentry-b"
	w.row(b, "SHORT", 1, entry1193, w.now())
	w.mentorStop(b, "short", 1, 0, stop1193)
	w.orderUpdate(b, "filled", entry1193, 1) // the book before the bracket lands
	w.positions(short(1, entry1193))
	w.sync()
	w.jump(150 * time.Millisecond)
	w.look()
	w.mustBeSilent("B's first look, 3 s after A's")
	w.orderUpdate(b+"-sl", "working", 0, 0, sl(b+"-sl", stop1193, 1, "Working"))
	w.sync()
	w.secondLook()
	time.Sleep(3500 * time.Millisecond)
	w.mustBeSilent("B's bracket is in the book: a stop beside it would double-exit")
}

// TestLookTwice07_ActsAtThreeSeconds — spec 9.7, the timing half: a truly naked
// position is acted on by the SECOND look (3 s, no 60 s wait), never the first.
func TestLookTwice07_ActsAtThreeSeconds(t *testing.T) {
	for _, c := range lookTwiceNakedCases() {
		t.Run(c.name, func(t *testing.T) {
			w := c.open(t, "lt-act-"+c.id)
			got := w.actsOnce(2900 * time.Millisecond)
			c.check(t, got)
		})
	}
}

// TestLookTwicePin07_NakedIsStillProtected — the half of spec 9.7 that must NOT
// change: those same positions get exactly one order. Green before and after.
func TestLookTwicePin07_NakedIsStillProtected(t *testing.T) {
	for _, c := range lookTwiceNakedCases() {
		t.Run(c.name, func(t *testing.T) {
			w := c.open(t, "lt-pin-"+c.id)
			c.check(t, w.actsOnce(0))
		})
	}
}

type nakedCase struct {
	name, id string
	open     func(*testing.T, string) *lookWire
	check    func(*testing.T, string)
}

func lookTwiceNakedCases() []nakedCase {
	isPlace := func(qty int, stop float64) func(*testing.T, string) {
		return func(t *testing.T, got string) {
			t.Helper()
			var p ntwire.PlaceProtectiveStopPayload
			body, ok := strings.CutPrefix(got, string(ntwire.FramePlaceProtectiveStop)+" ")
			if !ok || json.Unmarshal([]byte(body), &p) != nil {
				t.Fatalf("want a place_protective_stop frame, got %s", got)
			}
			if p.PositionSide != "SHORT" || p.Quantity != qty || p.StopPrice != stop {
				t.Fatalf("stop frame = %+v, want SHORT ×%d @ %.2f", p, qty, stop)
			}
		}
	}
	isClose := func(t *testing.T, got string) {
		t.Helper()
		var p ntwire.ClosePositionPayload
		body, ok := strings.CutPrefix(got, string(ntwire.FrameClosePosition)+" ")
		if !ok || json.Unmarshal([]byte(body), &p) != nil {
			t.Fatalf("want a close_position frame, got %s", got)
		}
		if p.Side != "short" || p.LimitPrice != 0 || p.Account != "Sim101" {
			t.Fatalf("close frame = %+v, want a market close of the short on Sim101", p)
		}
	}
	return []nakedCase{
		{"stop cancelled without a fill: PLACE", "place", func(t *testing.T, id string) *lookWire {
			w, sid := nakedShort(t, id, 27740)
			w.cancelStop(sid)
			return w
		}, isPlace(1, stop1193)},
		{"stop cancelled and price through it: FLATTEN", "flatten", func(t *testing.T, id string) *lookWire {
			w, sid := nakedShort(t, id, 27755)
			w.cancelStop(sid)
			return w
		}, isClose},
		// Leg 1's target filled 2 and leg 2's stop was CANCELLED: the runner is
		// naked for real. The AddOn repeats leg 1's exit frames 2.5 s after the
		// first look; a replay is not a new fill and must not push the act away.
		{"runner naked after leg 1, exit frames repeated during the hold: PLACE", "runner", func(t *testing.T, id string) *lookWire {
			w := newLookWire(t, id, ctInstant(t, "2026-11-18 10:00:05.000"))
			w.price(27740)
			w.row(sid1193, "SHORT", 3, entry1193, w.now())
			w.mentorStop(sid1193, "short", 2, 1, stop1193)
			w.orderUpdate(sid1193, "filled", entry1193, 3, w.full1193()...)
			w.positions(short(3, entry1193))
			w.sync()
			w.look()
			w.mustBeSilent("fixture: the covered position")
			w.jump(5 * time.Minute)
			exit := w.now()
			ou := ntwire.OrderUpdatePayload{SignalID: sid1193, OrderName: sid1193 + "-tp", State: "filled",
				FillPrice: 27715.75, Quantity: 2, Symbol: "MNQ", Account: "Sim101"}
			left := tp(sid1193+"-tp2", 27133.50, 1, "Working")
			w.orderUpdate(ou.OrderName, ou.State, ou.FillPrice, ou.Quantity, left)
			pc := w.closeFrame(sid1193, "short", "tp", 27715.75, 2, 1, exit)
			w.orderUpdate(sid1193+"-sl2", "cancelled", 0, 0, left)
			w.positions(short(1, entry1193))
			w.sync()
			w.receipts(1, true)
			w.jump(10 * time.Second) // long settled: only a NEW fill may hold it now
			w.beat30()
			w.sync()
			dup := time.AfterFunc(2500*time.Millisecond, func() {
				w.wmu.Lock()
				defer w.wmu.Unlock()
				_ = ntwire.WriteFrame(w.conn, ntwire.FrameOrderUpdate, ou)
				_ = ntwire.WriteFrame(w.conn, ntwire.FrameOrderSnapshot, ntwire.OrderSnapshotPayload{
					Account: "Sim101", Reason: "state_change", Orders: []ntwire.NT8Order{left}})
				_ = ntwire.WriteFrame(w.conn, ntwire.FramePositionClose, pc)
			})
			t.Cleanup(func() { dup.Stop() })
			return w
		}, isPlace(1, stop1193)},
		// spec 9.8 — 11-20 13:30:26, row 1447: the entry filled 13:30:00.015 while
		// its cancel was in flight and the AddOn dropped the bracket note, so no
		// -sl was ever sent. This net placed 25370.25. It must keep doing so.
		{"11-20 13:30:26 row 1447: PLACE 25370.25", "1447", func(t *testing.T, id string) *lookWire {
			const sid = "518638c6-120f-4625-97e6-800428998b14"
			w := newLookWire(t, id, ctInstant(t, "2026-11-20 13:30:00.015"))
			w.price(25321.25)
			w.row(sid, "SHORT", 1, 25322.50, w.now())
			w.mentorStop(sid, "short", 1, 0, 25370.25)
			w.orderUpdate(sid, "filled", 25322.50, 1)
			w.positions(short(1, 25322.50))
			w.sync()
			w.setNow(ctInstant(t, "2026-11-20 13:30:24.000"))
			w.beat30() // the AddOn's 30 s resend
			w.sync()
			w.setNow(ctInstant(t, "2026-11-20 13:30:26.721"))
			return w
		}, isPlace(1, 25370.25)},
		// spec 9.9 — an open the ledger does not know yet (an AI fill whose record
		// lags): a MISSING row is not a veto (rule 5).
		{"no row yet, the driver's stop known: PLACE", "norow", func(t *testing.T, id string) *lookWire {
			w := newLookWire(t, id, ctInstant(t, "2026-11-18 10:00:05.000"))
			w.price(27740)
			w.mentorStop("ai-sig", "short", 1, 0, stop1193)
			w.orderUpdate("ai-sig", "filled", 27733.50, 1)
			w.positions(short(1, 27733.50))
			w.sync()
			w.jump(10 * time.Second)
			w.beat30()
			w.sync()
			return w
		}, isPlace(1, stop1193)},
	}
}

// TestLookTwicePin09_NoRowNoPriceStillAlerts — spec 9.9's other half: with no row
// and no price there is nothing to place, and the P0 must still reach the owner.
func TestLookTwicePin09_NoRowNoPriceStillAlerts(t *testing.T) {
	w := newLookWire(t, "lt-norow-noprice", ctInstant(t, "2026-11-18 10:00:05.000"))
	w.orderUpdate("ai-sig", "filled", 27733.50, 1)
	w.positions(short(1, 27733.50))
	w.sync()
	w.jump(10 * time.Second)
	w.beat30()
	w.sync()
	w.at.reconcileProtectionAt(w.now(), "monitor")
	for deadline := time.Now().Add(12 * time.Second); len(w.p0s()) == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("a naked position with no price to place raised no P0")
		}
	}
	w.mustBeSilent("no price: nothing is invented")
}

// TestLookTwice09_NoPriceAlertWaitsForTheSecondLook — the "no price" alert is a
// verdict like the others: not on the first look.
func TestLookTwice09_NoPriceAlertWaitsForTheSecondLook(t *testing.T) {
	w := newLookWire(t, "lt-noprice-first", ctInstant(t, "2026-11-18 10:00:05.000"))
	w.orderUpdate("ai-sig", "filled", 27733.50, 1)
	w.positions(short(1, 27733.50))
	w.sync()
	w.jump(10 * time.Second)
	w.beat30()
	w.sync()
	w.at.reconcileProtectionAt(w.now(), "monitor")
	w.mustHaveNoP0("first look")
}

// deadByReceipts closes row 1193 by its own receipts, yet NT8 keeps reporting
// Short 1 on NEW positions frames (a double-counted receipt, or NT8 truth we
// cannot explain).
func deadByReceipts(t *testing.T, id string) *lookWire {
	t.Helper()
	w := open1193(t, id, 27740)
	w.stops1193(exits1193{})
	w.until(ms1193(249))
	w.positions(short(1, entry1193))
	w.sync()
	return w
}

// TestLookTwice10_OwnDeathVeto — spec 9.10: our last row there closed seconds ago
// by receipts that sum to its entry quantity and no row is open → P0 only. The
// veto lives 60 s; after it, a position that is STILL there is protected.
func TestLookTwice10_OwnDeathVeto(t *testing.T) {
	w := deadByReceipts(t, "lt-owndeath")
	w.look()
	for i := 0; i < 5; i++ { // 10 s … 50 s after the close
		w.jump(10 * time.Second)
		w.beat30()
		w.sync()
		w.at.reconcileProtectionAt(w.now(), "monitor")
		time.Sleep(200 * time.Millisecond)
	}
	w.mustBeSilent("within 60 s of our own death by receipts")
	if len(w.p0s()) == 0 {
		t.Fatal("the own-death veto must tell the owner (P0) while it holds")
	}
	w.jump(15 * time.Second) // > 60 s
	w.beat30()
	w.sync()
	w.at.reconcileProtectionAt(w.now(), "monitor")
	if out, took := w.waitOrders(1, 12*time.Second); len(out) != 1 {
		t.Fatalf("the veto must EXPIRE at 60 s and the position be protected, got %d order(s) after %s", len(out), took)
	}
}

// TestLookTwicePin10_ManualRowKeepsTodaysPath — an owner's own position (row
// origin=manual) is protected on the same path as today.
func TestLookTwicePin10_ManualRowKeepsTodaysPath(t *testing.T) {
	w, sid := nakedShort(t, "lt-manual", 27740)
	if err := w.st.GormDB().Model(&store.TraderPosition{}).Where("trader_id = ?", w.at.id).Update("origin", store.OriginManual).Error; err != nil {
		t.Fatal(err)
	}
	w.cancelStop(sid)
	w.actsOnce(0)
}

// TestLookTwice11_StopDuringHold — spec 9.11: Stop() lands inside the hold. The
// re-look must be joined (monitorWg) and nothing may go out afterwards.
func TestLookTwice11_StopDuringHold(t *testing.T) {
	w, sid := nakedShort(t, "lt-stop", 27755) // would FLATTEN
	w.cancelStop(sid)
	w.at.reconcileProtectionAt(w.now(), "monitor")
	w.at.Stop()
	time.Sleep(4500 * time.Millisecond)
	w.mustBeSilent("after Stop during the hold")
}

// TestLookTwice11_BeatAndRelookPlaceOneStop — spec 9.11: the 60 s beat and the
// background re-look fall together. ONE stop, until a book received AFTER the
// send says it is not there (rule 6).
func TestLookTwice11_BeatAndRelookPlaceOneStop(t *testing.T) {
	w, sid := nakedShort(t, "lt-onestop", 27740)
	w.cancelStop(sid)
	w.at.reconcileProtectionAt(w.now(), "monitor")
	time.Sleep(3 * time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); w.at.reconcileProtectionAt(w.now(), "monitor") }()
	}
	wg.Wait()
	w.waitOrders(1, 10*time.Second)
	time.Sleep(3500 * time.Millisecond) // another re-look, same book
	w.at.reconcileProtectionAt(w.now(), "monitor")
	time.Sleep(300 * time.Millisecond)
	if n := w.count(ntwire.FramePlaceProtectiveStop); n != 1 {
		t.Fatalf("beat + re-look placed %d stops on one book, want exactly 1:\n  %s", n, strings.Join(w.ordersOut(), "\n  "))
	}
}

// TestLookTwice12_TargetFillCancelsTheStop — spec 9.12: the target fills, NT8
// cancels the stop (231 such windows, longest 424 ms) while the position still
// reads Short 1. Today: PLACE on an account that is about to be flat.
func TestLookTwice12_TargetFillCancelsTheStop(t *testing.T) {
	w, sid := nakedShort(t, "lt-tp", 27700) // price at the target: the safe side
	w.orderUpdate(sid+"-tp", "filled", 27700, 1, sl(sid+"-sl", stop1193, 1, "Working"))
	w.orderUpdate(sid+"-sl", "cancelsubmitted", 0, 0, sl(sid+"-sl", stop1193, 1, "CancelSubmitted"))
	w.closeFrame(sid, "short", "tp", 27700, 1, 0, w.now())
	w.sync()
	w.receipts(1, true)
	w.look()
	w.mustBeSilent("inside the cancel window")
	time.Sleep(300 * time.Millisecond) // Flat at 0.424 s
	w.positions()
	w.orderUpdate(sid+"-sl", "cancelled", 0, 0)
	w.sync()
	w.secondLook()
	time.Sleep(3500 * time.Millisecond)
	w.mustBeSilent("after flat")
	w.mustHaveNoP0("a normal target exit")
}

// TestLookTwicePin14_ConfirmZeroIsTheOldSingleLook — spec 9.14: 0 restores the old
// frame sequence: the first look acts. Green before and after.
func TestLookTwicePin14_ConfirmZeroIsTheOldSingleLook(t *testing.T) {
	t.Run("flatten on 06:47:30.249", func(t *testing.T) {
		t.Setenv(lookTwiceEnv, "0")
		w := open1193(t, "lt-zero-flat", stop1193)
		w.stops1193(exits1193{})
		w.until(ms1193(249))
		w.look()
		if out, _ := w.waitOrders(1, 2*time.Second); len(out) != 1 || !strings.HasPrefix(out[0], string(ntwire.FrameClosePosition)) {
			t.Fatalf("CONFIRM_MS=0 must send the old close on the first look, got %v", out)
		}
	})
	t.Run("place", func(t *testing.T) {
		t.Setenv(lookTwiceEnv, "0")
		w, sid := nakedShort(t, "lt-zero-place", 27740)
		w.cancelStop(sid)
		w.look()
		if out, _ := w.waitOrders(1, 2*time.Second); len(out) != 1 || !strings.HasPrefix(out[0], string(ntwire.FramePlaceProtectiveStop)) {
			t.Fatalf("CONFIRM_MS=0 must place on the first look, got %v", out)
		}
	})
}

// TestLookTwice14_BadValueMeans3000 — a typo is never the old single look.
func TestLookTwice14_BadValueMeans3000(t *testing.T) {
	for _, v := range []string{"abc", "-1", ""} {
		t.Run("value "+v, func(t *testing.T) {
			t.Setenv(lookTwiceEnv, v)
			w, sid := nakedShort(t, "lt-bad-"+v, 27740)
			w.cancelStop(sid)
			w.look()
			w.mustBeSilent("first look with a bad " + lookTwiceEnv)
		})
	}
}
