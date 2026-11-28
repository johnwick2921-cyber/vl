package ninjatrader

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// ── GAP-BACKFILL (2026-10-14) — PRODUCTION CALL SITES ──────────────────────
//
// Every test below runs the real path: a started TCPServer, a fake AddOn on a
// real socket streaming bar_update / bars_historical frames, the server's read
// loop → ingest drain → BarCache → persist worker → THE persister closure
// WireBarPersistence installs (newBarPersister) → a real SQLite store → the
// bar-hole tracker the closure reports to. Only the tracker's clock is fixed,
// so the schedule (+1/+3/+10/+30 min) is driven without waiting for it, and
// every bar sits in the past so nothing depends on when the suite runs (the
// CME-break wall-clock trap).

const ghContract = "MNQ 12-26"

func ct(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, kernel.CTLocation())
}

// ── the fake AddOn ─────────────────────────────────────────────────────────

type fakeAddOn struct {
	t    *testing.T
	conn net.Conn
	wmu  sync.Mutex

	mu       sync.Mutex
	subs     []ntwire.BarsSubscribePayload
	baseline int
	// answer, when set, runs on every RE-request (n = 1, 2, … after baseline)
	answer func(n int, p ntwire.BarsSubscribePayload)
}

// send writes one frame. A write error is not reported through t: answers run
// on the AddOn's read goroutine and may outlive the test by a frame; the
// assertions that matter read what the SERVER received.
func (a *fakeAddOn) send(ft ntwire.FrameType, p any) {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	_ = ntwire.WriteFrame(a.conn, ft, p)
}

func (a *fakeAddOn) loop() {
	for {
		_ = a.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		env, err := ntwire.ReadFrame(a.conn)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			return
		}
		switch env.Type {
		case ntwire.FrameHeartbeat:
			a.wmu.Lock()
			_ = ntwire.WriteFrame(a.conn, ntwire.FrameAck, ntwire.AckPayload{Acks: "heartbeat"})
			a.wmu.Unlock()
		case ntwire.FrameBarsSubscribe:
			var p ntwire.BarsSubscribePayload
			_ = json.Unmarshal(env.Payload, &p)
			a.mu.Lock()
			a.subs = append(a.subs, p)
			n := len(a.subs) - a.baseline
			fn := a.answer
			a.mu.Unlock()
			if n >= 1 && fn != nil {
				fn(n, p)
			}
		}
	}
}

// reRequests is how many bars_subscribe frames arrived after the boot ones.
func (a *fakeAddOn) reRequests() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.subs) - a.baseline
}

func (a *fakeAddOn) lastSub() ntwire.BarsSubscribePayload {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.subs[len(a.subs)-1]
}

func (a *fakeAddOn) setAnswer(fn func(n int, p ntwire.BarsSubscribePayload)) {
	a.mu.Lock()
	a.answer = fn
	a.mu.Unlock()
}

// live sends one 1m bar as a bar_update (WIRE stamp = open + 1m).
func (a *fakeAddOn) live(open time.Time, c float64) {
	a.send(ntwire.FrameBarUpdate, ntwire.BarUpdatePayload{Symbol: "MNQ", Timeframe: "1m", Contract: ghContract,
		Bars: []ntwire.Bar{wireBar(open, c)}})
}

func wireBar(open time.Time, c float64) ntwire.Bar {
	return ntwire.Bar{T: open.Add(time.Minute).UnixMilli(), O: c, H: c + 1, L: c - 1, C: c, V: 10}
}

// ── the fixture ────────────────────────────────────────────────────────────

func (h *replayHold) clearForTest() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending = map[string][]store.BarHistoryDB{}
	h.written = map[string]int{}
	h.discarded = map[string]int{}
	h.overflow = map[string]int{}
}

type lineSink struct {
	mu    sync.Mutex
	lines []string
}

func (s *lineSink) say(level, msg string) {
	s.mu.Lock()
	s.lines = append(s.lines, level+" "+msg)
	s.mu.Unlock()
}

func (s *lineSink) count(sub string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, l := range s.lines {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

func (s *lineSink) dump() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.lines, "\n")
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

type ghFixture struct {
	t     *testing.T
	bh    *store.BarHistoryStore
	srv   *ntwire.TCPServer
	addon *fakeAddOn
	tr    *barHoleTracker
	clk   *fakeClock
	sink  *lineSink
	base  telemetry.BarsHoleCounts
}

func newGHFixture(t *testing.T) *ghFixture {
	t.Helper()
	// Empty the process hold IN PLACE, under its own lock: earlier tests leave
	// MNQ|1m rows in it (stamped wall-clock −2h, which can sit inside this
	// fixture's window), and swapping the pointer would race a persist worker
	// still flushing the previous test's last frame.
	barReplayHold.clearForTest()
	st, err := store.New(filepath.Join(t.TempDir(), "gh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	srv := ntwire.NewTCPServer(nil)
	srv.SetAddrForTest(addr)
	ctx, cancel := context.WithCancel(context.Background())
	if err := srv.Start(ctx); err != nil {
		cancel()
		t.Fatalf("server start: %v", err)
	}
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		cancel()
		t.Fatalf("fake AddOn dial: %v", err)
	}
	addon := &fakeAddOn{t: t, conn: conn}
	go addon.loop()
	t.Cleanup(func() {
		_ = conn.Close()
		cancel()
		_ = srv.Stop()
	})
	waitFor(t, "the server to accept the fake AddOn", func() bool { return srv.IsConnected() })
	waitFor(t, "the boot bars_subscribe", func() bool { addon.mu.Lock(); defer addon.mu.Unlock(); return len(addon.subs) >= 1 })
	time.Sleep(200 * time.Millisecond) // every boot subscribe (primary + extras) is in
	addon.mu.Lock()
	addon.baseline = len(addon.subs)
	addon.mu.Unlock()
	addon.send(ntwire.FrameSubscribed, ntwire.SubscribedPayload{Symbol: "MNQ", ResolvedContract: ghContract})
	waitFor(t, "the subscription ACK", func() bool { _, ok := srv.CurrentContract("MNQ"); return ok })

	serverFn := func() *ntwire.TCPServer { return srv }
	ntwire.SetBarPersister(newBarPersister(bh, serverFn))
	t.Cleanup(func() {
		ntwire.SetBarPersister(nil)
		time.Sleep(400 * time.Millisecond) // a flush already inside the closure (300 ms ticker) finishes
	})
	// On a whole minute: the schedule's +1/+3/+10/+30 slots (and the 15 min
	// slow step) then land in the first seconds of a CT minute, inside the
	// send window, exactly as one of production's 15 s ticks does
	// (barHoleSendWindow, #23 review P2-1).
	clk := &fakeClock{t: ct(2026, 10, 14, 7, 49)}
	sink := &lineSink{}
	tr := newBarHoleTracker(bh, serverFn)
	tr.clock = clk.Now
	tr.say = sink.say
	prev := barHoles.Swap(tr)
	t.Cleanup(func() { barHoles.Store(prev) })
	return &ghFixture{t: t, bh: bh, srv: srv, addon: addon, tr: tr, clk: clk, sink: sink, base: telemetry.BarsHoleCountsSnapshot()}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (f *ghFixture) storedOpen(from, to time.Time) int {
	rows, err := f.bh.BarsBetweenOn("MNQ", "1m", ghContract, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		f.t.Fatal(err)
	}
	return len(rows)
}

func (f *ghFixture) holes() []BarHoleView { return f.tr.snapshot() }

func (f *ghFixture) delta() telemetry.BarsHoleCounts {
	now := telemetry.BarsHoleCountsSnapshot()
	d := telemetry.BarsHoleCounts{
		Detected: now.Detected - f.base.Detected, Filled: now.Filled - f.base.Filled,
		Unfilled: now.Unfilled - f.base.Unfilled, Rerequests: now.Rerequests - f.base.Rerequests,
		Deferred: now.Deferred - f.base.Deferred, Refused: now.Refused - f.base.Refused,
		FilledAt: make([]int64, len(now.FilledAt)),
	}
	for i := range now.FilledAt {
		d.FilledAt[i] = now.FilledAt[i] - f.base.FilledAt[i]
	}
	return d
}

// stream sends live 1m bars for every minute in [from, to] and waits until the
// store holds the last one.
func (f *ghFixture) stream(from, to time.Time) {
	f.t.Helper()
	for m := from; !m.After(to); m = m.Add(time.Minute) {
		f.addon.live(m, 30700)
	}
	waitFor(f.t, "the store to hold "+kernel.TableTimeCT(to), func() bool {
		return f.storedOpen(to, to.Add(time.Minute)) == 1
	})
}

// openHole is the 10-14 shape: live to 03:43, NT8 silent 03:44–07:47, live
// again from 07:48. Returns when the tracker holds the hole.
func (f *ghFixture) openHole() time.Time {
	f.t.Helper()
	f.stream(ct(2026, 10, 14, 3, 30), ct(2026, 10, 14, 3, 43))
	f.stream(ct(2026, 10, 14, 7, 48), ct(2026, 10, 14, 7, 49))
	waitFor(f.t, "the tracker to open the 03:44–07:48 hole", func() bool { return len(f.holes()) == 1 })
	return f.clk.Now()
}

// noNewReRequest asserts nothing more reaches the AddOn for a beat.
func (f *ghFixture) noNewReRequest(want int, why string) {
	f.t.Helper()
	time.Sleep(300 * time.Millisecond)
	if got := f.addon.reRequests(); got != want {
		f.t.Fatalf("%s: re-requests at the AddOn = %d, want %d\nlines:\n%s", why, got, want, f.sink.dump())
	}
}

func (f *ghFixture) waitReRequests(n int) {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f.addon.reRequests() >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("the AddOn received %d history re-request(s), want %d\nlines:\n%s", f.addon.reRequests(), n, f.sink.dump())
}

// answerFull makes the fake AddOn reply to a re-request as NT8 does once its DB
// has the window: a bars_historical 1m replay covering 03:30–07:49 on the live
// scale, then the next live minute (the frame that lets the hold release it).
func (f *ghFixture) answerFull() {
	f.t.Helper()
	bars := make([]ntwire.Bar, 0, 300)
	for m := ct(2026, 10, 14, 3, 30); !m.After(ct(2026, 10, 14, 7, 49)); m = m.Add(time.Minute) {
		bars = append(bars, wireBar(m, 30700))
	}
	f.addon.send(ntwire.FrameBarsHistorical, ntwire.BarsHistoricalPayload{Symbol: "MNQ", Timeframe: "1m", Contract: ghContract, Bars: bars})
	time.Sleep(100 * time.Millisecond)
	f.addon.live(ct(2026, 10, 14, 7, 50), 30700)
}

// answerEmpty is the 07:49:55 shape: NT8 answers with bars=0.
func (f *ghFixture) answerEmpty() {
	f.addon.send(ntwire.FrameBarsHistorical, ntwire.BarsHistoricalPayload{Symbol: "MNQ", Timeframe: "1m", Contract: ghContract, Bars: []ntwire.Bar{}})
}

// ── (a) a 244-minute hole during CME open is re-requested at +1 min ────────

func TestHoleDuringOpenIsReRequestedAtTheFirstSlot(t *testing.T) {
	f := newGHFixture(t)
	det := f.openHole()

	hs := f.holes()
	if hs[0].From != ct(2026, 10, 14, 3, 44).Format(time.RFC3339) || hs[0].To != ct(2026, 10, 14, 7, 48).Format(time.RFC3339) {
		t.Fatalf("hole window = [%s, %s), want [03:44, 07:48) CT", hs[0].From, hs[0].To)
	}
	if hs[0].MissingMin != 244 || hs[0].State != holeStateRefilling || hs[0].Attempts != 0 {
		t.Fatalf("hole = %+v, want 244 missing, refilling, 0 attempts", hs[0])
	}
	if d := f.delta(); d.Detected != 1 {
		t.Fatalf("bars_hole_detected +%d, want +1", d.Detected)
	}
	if f.sink.count("bars HOLE detected: MNQ 1m 10-14 03:44–07:48 CT") != 1 {
		t.Fatalf("the detection line must be said once:\n%s", f.sink.dump())
	}

	f.tr.tick(det.Add(30 * time.Second))
	f.noNewReRequest(0, "before the +1 min slot nothing is sent")

	f.tr.tick(det.Add(61 * time.Second))
	f.waitReRequests(1)
	p := f.addon.lastSub()
	if p.Symbol != "MNQ" || p.BarsBack != 2000 || len(p.Timeframes) == 0 {
		t.Fatalf("the re-request must be the subscription's own frame (MNQ, bars_back 2000, its tfs), got %+v", p)
	}
	if d := f.delta(); d.Rerequests != 1 {
		t.Fatalf("bars_hole_rerequests +%d, want +1", d.Rerequests)
	}
	if got := f.holes()[0].Attempts; got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

// ── (b) a bars=0 answer leads to a retry at the next slot ──────────────────

func TestEmptyAnswerIsRetriedAtTheNextSlot(t *testing.T) {
	f := newGHFixture(t)
	f.addon.setAnswer(func(int, ntwire.BarsSubscribePayload) { f.answerEmpty() })
	det := f.openHole()

	f.tr.tick(det.Add(61 * time.Second))
	f.waitReRequests(1)
	time.Sleep(200 * time.Millisecond) // the bars=0 replay lands
	f.tr.tick(det.Add(2 * time.Minute))
	f.noNewReRequest(1, "between slots nothing is sent")
	f.tr.tick(det.Add(3*time.Minute + 5*time.Second))
	f.waitReRequests(2)
	if hs := f.holes(); len(hs) != 1 || hs[0].Attempts != 2 || hs[0].MissingMin != 244 {
		t.Fatalf("after a bars=0 answer the hole stays open and is asked again: %+v", hs)
	}
}

// ── data arriving fills the hole: counted, and the schedule stops ─────────

func TestArrivingDataFillsTheHoleAndStopsTheSchedule(t *testing.T) {
	f := newGHFixture(t)
	f.addon.setAnswer(func(n int, _ ntwire.BarsSubscribePayload) {
		if n == 1 {
			f.answerEmpty()
			return
		}
		f.answerFull()
	})
	det := f.openHole()

	f.tr.tick(det.Add(61 * time.Second))
	f.waitReRequests(1)
	f.tr.tick(det.Add(3*time.Minute + 5*time.Second))
	f.waitReRequests(2)
	waitFor(t, "NT8's replay to reach the store through the replay-hold", func() bool {
		return f.storedOpen(ct(2026, 10, 14, 3, 44), ct(2026, 10, 14, 7, 48)) == 244
	})
	f.tr.tick(det.Add(5*time.Minute + 10*time.Second))
	if hs := f.holes(); len(hs) != 0 {
		t.Fatalf("the hole must be closed once the store holds it, still open: %+v\n%s", hs, f.sink.dump())
	}
	d := f.delta()
	if d.Filled != 1 || d.FilledAt[2] != 1 || d.Unfilled != 0 {
		t.Fatalf("filled +%d (by attempt %v) unfilled +%d — want one fill by re-request #2", d.Filled, d.FilledAt, d.Unfilled)
	}
	if f.sink.count("✅ bars hole FILLED: MNQ 1m 10-14 03:44–07:48 CT") != 1 || f.sink.count("after re-request #2") != 1 {
		t.Fatalf("the fill must be said once with its attempt:\n%s", f.sink.dump())
	}
	f.tr.tick(det.Add(10*time.Minute + 5*time.Second))
	f.tr.tick(det.Add(30*time.Minute + 5*time.Second))
	f.noNewReRequest(2, "a filled hole is never asked for again")
	if hs, ok := BarHolesSnapshot(); !ok || hs == nil || len(hs) != 0 {
		t.Fatalf("health must read [] (known, none open), got %v ok=%v", hs, ok)
	}
}

// ── nothing fires while the feed is down ───────────────────────────────────

func TestNothingFiresWhileTheFeedIsDown(t *testing.T) {
	f := newGHFixture(t)
	det := f.openHole()
	f.addon.send(ntwire.FrameFeedStatus, ntwire.FeedStatusPayload{PriceStatus: "ConnectionLost"})
	waitFor(t, "the feed status to read ConnectionLost", func() bool { return f.srv.FeedStatus() == "ConnectionLost" })
	f.srv.AgeLiveFeedEvidenceForTest(10 * time.Minute) // no live bar for 10 min: the outage
	if f.srv.IsFeedConnected() {
		t.Fatal("fixture: the feed must read DOWN")
	}

	f.tr.tick(det.Add(61 * time.Second))
	f.tr.tick(det.Add(76 * time.Second))
	f.noNewReRequest(0, "a BarsRequest during a feed outage returns bars=0 — nothing may be sent")
	d := f.delta()
	if d.Deferred != 1 || d.Refused != 0 || d.Rerequests != 0 {
		t.Fatalf("deferred +%d refused +%d sent +%d — the TRACKER must hold the attempt (said and counted once), not hand it to the wire", d.Deferred, d.Refused, d.Rerequests)
	}
	if f.sink.count("re-request #1 DEFERRED for MNQ 1m 10-14 03:44–07:48 CT: NT8 feed down") != 1 {
		t.Fatalf("the deferral must be said once, naming the feed:\n%s", f.sink.dump())
	}

	f.addon.send(ntwire.FrameFeedStatus, ntwire.FeedStatusPayload{PriceStatus: "Connected"})
	waitFor(t, "the feed status to read Connected", func() bool { return f.srv.FeedStatus() == "Connected" })
	f.tr.tick(det.Add(2 * time.Minute))
	f.waitReRequests(1)
}

// ── the scale-break budget is untouched ────────────────────────────────────

func TestGapReRequestsLeaveTheScaleBreakBudgetAlone(t *testing.T) {
	f := newGHFixture(t)
	det := f.openHole()
	f.tr.tick(det.Add(61 * time.Second))
	f.waitReRequests(1)
	if got := f.srv.HistoryReplaysSent("MNQ"); got != 0 {
		t.Fatalf("a gap re-request spent %d of the scale-break budget", got)
	}
	if err := f.srv.RequestHistoryReplayAt("MNQ", det.Add(70*time.Second)); err != nil {
		t.Fatalf("the scale-break re-request must still have its one-per-boot budget: %v", err)
	}
	f.waitReRequests(2)
	if f.srv.HistoryReplaysSent("MNQ") != 1 || f.srv.GapBackfillsSent("MNQ") != 1 {
		t.Fatalf("budgets: scale-break %d (want 1), gap %d (want 1)", f.srv.HistoryReplaysSent("MNQ"), f.srv.GapBackfillsSent("MNQ"))
	}
	// …and the scale-break guard still refuses its second, as before.
	if err := f.srv.RequestHistoryReplayAt("MNQ", det.Add(2*time.Hour)); !errors.Is(err, ntwire.ErrHistoryReplaySpent) {
		t.Fatalf("the scale-break one-per-boot guard must still refuse a second: %v", err)
	}
}

// ── after the last attempt, the unfilled line appears exactly once ─────────

func TestUnfilledLineIsSaidOnceThenSlowRetriesPickUpTheReload(t *testing.T) {
	f := newGHFixture(t)
	reloaded := false
	var amu sync.Mutex
	f.addon.setAnswer(func(int, ntwire.BarsSubscribePayload) {
		amu.Lock()
		r := reloaded
		amu.Unlock()
		if r {
			f.answerFull()
			return
		}
		f.answerEmpty()
	})
	det := f.openHole()
	steps := []struct {
		at   time.Duration
		want int
	}{
		{61 * time.Second, 1},
		{3*time.Minute + 5*time.Second, 2},
		{10*time.Minute + 5*time.Second, 3},
		{30*time.Minute + 5*time.Second, 4},
	}
	for _, s := range steps {
		f.tr.tick(det.Add(s.at))
		f.waitReRequests(s.want)
	}
	if f.sink.count("UNFILLED") != 0 {
		t.Fatalf("nothing is declared before the last attempt is measured:\n%s", f.sink.dump())
	}
	f.tr.tick(det.Add(32*time.Minute + 10*time.Second)) // the measure after #4
	want := "🚨 bars hole UNFILLED after 4 re-requests: NinjaTrader has no history for 10-14 03:44–07:48 CT (MNQ 1m on MNQ 12-26, 244 open minute(s) still missing): on an MNQ chart right-click → Reload All Historical Data; the bot re-requests on its next attempt (every 15m while CME is open, at most 8 more), until the hole leaves NT8's replay window — then it is retired and stays"
	if f.sink.count(want) != 1 {
		t.Fatalf("want the unfilled line exactly once:\n%s", f.sink.dump())
	}
	if f.sink.count("replay overlap verdicts this process: agreed 0, disagreed 0") != 1 {
		t.Fatalf("the unfilled line must READ the ring's overlap verdicts (no scale story here):\n%s", f.sink.dump())
	}
	if hs := f.holes(); len(hs) != 1 || hs[0].State != holeStateUnfilled {
		t.Fatalf("health must show the hole as unfilled: %+v", hs)
	}
	f.tr.tick(det.Add(40 * time.Minute))
	f.noNewReRequest(4, "the slow retry waits its 15 minutes")
	f.tr.tick(det.Add(47*time.Minute + 10*time.Second))
	f.waitReRequests(5)
	f.tr.tick(det.Add(49*time.Minute + 20*time.Second))
	if n := f.sink.count("UNFILLED"); n != 1 {
		t.Fatalf("the unfilled line is ONE line per hole, said %d times:\n%s", n, f.sink.dump())
	}
	if d := f.delta(); d.Unfilled != 1 {
		t.Fatalf("bars_hole_unfilled +%d, want +1", d.Unfilled)
	}

	// The owner reloads NT8's history; the next slow retry picks it up.
	amu.Lock()
	reloaded = true
	amu.Unlock()
	f.tr.tick(det.Add(62*time.Minute + 10*time.Second))
	f.waitReRequests(6)
	waitFor(t, "the reloaded window to reach the store", func() bool {
		return f.storedOpen(ct(2026, 10, 14, 3, 44), ct(2026, 10, 14, 7, 48)) == 244
	})
	f.tr.tick(det.Add(64*time.Minute + 30*time.Second))
	if hs := f.holes(); len(hs) != 0 {
		t.Fatalf("the reload must close the hole: %+v\n%s", hs, f.sink.dump())
	}
	if f.sink.count("after re-request #6") != 1 || f.sink.count("UNFILLED") != 1 {
		t.Fatalf("filled by the slow retry #6, unfilled still said once:\n%s", f.sink.dump())
	}
}

// ── the daily break, a weekend and a holiday are NOT holes ─────────────────

func TestClosedMarketIsNotAHole(t *testing.T) {
	cases := []struct {
		name               string
		lastFrom, lastTo   time.Time
		firstFrom, firstTo time.Time
	}{
		{"daily break Tue 16:00–17:00", ct(2026, 10, 13, 15, 55), ct(2026, 10, 13, 15, 59), ct(2026, 10, 13, 17, 0), ct(2026, 10, 13, 17, 2)},
		{"weekend Fri 16:00 → Sun 17:00", ct(2026, 10, 9, 15, 56), ct(2026, 10, 9, 15, 59), ct(2026, 10, 11, 17, 0), ct(2026, 10, 11, 17, 1)},
		{"Good Friday closed (Thu → Sun)", ct(2026, 4, 2, 15, 56), ct(2026, 4, 2, 15, 59), ct(2026, 4, 5, 17, 0), ct(2026, 4, 5, 17, 1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newGHFixture(t)
			f.stream(c.lastFrom, c.lastTo)
			f.stream(c.firstFrom, c.firstTo)
			time.Sleep(200 * time.Millisecond)
			if hs := f.holes(); len(hs) != 0 {
				t.Fatalf("%s is the calendar, not a hole: %+v\n%s", c.name, hs, f.sink.dump())
			}
			if d := f.delta(); d.Detected != 0 {
				t.Fatalf("bars_hole_detected +%d across %s", d.Detected, c.name)
			}
		})
	}
}

// ── findBarHoles at the threshold (unit) ───────────────────────────────────

func TestFindBarHolesThresholdAndCalendar(t *testing.T) {
	m := func(tm time.Time) int64 { return tm.UnixMilli() }
	open := barHoleExpected
	// 10:00 then 10:04 → 3 open minutes missing → a hole at N=3
	if got := findBarHoles([]int64{m(ct(2026, 10, 14, 10, 0)), m(ct(2026, 10, 14, 10, 4))}, 3, open); len(got) != 1 || got[0].OpenMissing != 3 {
		t.Fatalf("3 missing open minutes must be a hole at N=3: %+v", got)
	}
	// 10:00 then 10:03 → 2 missing → not a hole
	if got := findBarHoles([]int64{m(ct(2026, 10, 14, 10, 0)), m(ct(2026, 10, 14, 10, 3))}, 3, open); len(got) != 0 {
		t.Fatalf("2 missing minutes are not a hole at N=3: %+v", got)
	}
	// 15:58 then 17:01 → only 15:59 and 17:00 are open → 2 missing → not a hole
	if got := findBarHoles([]int64{m(ct(2026, 10, 14, 15, 58)), m(ct(2026, 10, 14, 17, 1))}, 3, open); len(got) != 0 {
		t.Fatalf("the daily break is not missing: %+v", got)
	}
	// 15:50 then 17:10 → 15:51–15:59 (9) + 17:00–17:09 (10) open → 19 missing
	if got := findBarHoles([]int64{m(ct(2026, 10, 14, 15, 50)), m(ct(2026, 10, 14, 17, 10))}, 3, open); len(got) != 1 || got[0].OpenMissing != 19 {
		t.Fatalf("a hole straddling the break counts only its open minutes: %+v", got)
	}
	// Thu 04-02 15:59 then Sun 04-05 17:00 — Good Friday is closed, and so is
	// the Thursday evening session that would have belonged to it
	if got := findBarHoles([]int64{m(ct(2026, 4, 2, 15, 59)), m(ct(2026, 4, 5, 17, 0))}, 3, open); len(got) != 0 {
		t.Fatalf("a holiday close (by trading date) is not missing: %+v", got)
	}
	// …while an ordinary Thursday evening IS expected: 10-15 17:00 → 17:30
	if got := findBarHoles([]int64{m(ct(2026, 10, 15, 17, 0)), m(ct(2026, 10, 15, 17, 30))}, 3, open); len(got) != 1 || got[0].OpenMissing != 29 {
		t.Fatalf("an ordinary evening session is expected: %+v", got)
	}
}

// ── VERIFY FIRST — the two 10-14 shapes, end to end, over an hour ──────────
//
// The tracker's clock walks an hour in 15-second ticks (the production tick)
// while the fake AddOn plays 10-14: (a) live to 03:43, silence, live again
// from 07:48; (b) the same, plus the 07:49:55 reconnect rebuild answering
// bars=0. Before the fix nothing on the Go side ever asked NT8 again: the hole
// waited for the 08:22 restart.

func (f *ghFixture) walkAnHour(from time.Time) {
	for i := 0; i <= 240; i++ {
		f.tr.tick(from.Add(time.Duration(i) * 15 * time.Second))
	}
	time.Sleep(300 * time.Millisecond)
}

func TestRepro1014A_HoleIsReRequestedWithoutARestart(t *testing.T) {
	f := newGHFixture(t)
	f.stream(ct(2026, 10, 14, 3, 30), ct(2026, 10, 14, 3, 43))
	f.stream(ct(2026, 10, 14, 7, 48), ct(2026, 10, 14, 7, 49))
	time.Sleep(200 * time.Millisecond)
	f.walkAnHour(f.clk.Now())
	if n := f.addon.reRequests(); n < 1 {
		t.Fatalf("10-14 (a): a 244-minute 1m hole (03:44–07:48 CT) during CME open — over 60 minutes the AddOn received %d history re-request(s); the hole waits for an unrelated restart", n)
	}
}

func TestRepro1014B_EmptyReplayAfterReconnectIsRetried(t *testing.T) {
	f := newGHFixture(t)
	f.addon.setAnswer(func(int, ntwire.BarsSubscribePayload) { f.answerEmpty() })
	f.stream(ct(2026, 10, 14, 3, 30), ct(2026, 10, 14, 3, 43))
	f.answerEmpty() // 07:49:55: the AddOn's reconnect rebuild — NT8 answers bars=0
	f.stream(ct(2026, 10, 14, 7, 48), ct(2026, 10, 14, 7, 49))
	time.Sleep(200 * time.Millisecond)
	f.walkAnHour(f.clk.Now())
	if n := f.addon.reRequests(); n < 2 {
		t.Fatalf("10-14 (b): NT8 answered bars=0 after the reconnect — over 60 minutes the AddOn received %d history re-request(s), want ≥ 2 (an ask and a retry)", n)
	}
}

// ── a hole on a contract the platform rolled away from is retired, loudly ──
// A re-request replays the subscription's front month only; asking four times
// for a retired contract's window would end in an UNFILLED line sending the
// owner to reload history that the subscription can never serve.
func TestHoleOnARolledContractIsRetiredNotReRequested(t *testing.T) {
	f := newGHFixture(t)
	det := f.openHole()
	f.addon.send(ntwire.FrameSubscribed, ntwire.SubscribedPayload{Symbol: "MNQ", ResolvedContract: "MNQ 03-27"})
	waitFor(t, "the roll ACK", func() bool { c, ok := f.srv.CurrentContract("MNQ"); return ok && c.Contract == "MNQ 03-27" })
	f.tr.tick(det.Add(61 * time.Second))
	f.noNewReRequest(0, "a retired contract's window is not re-requested")
	if hs := f.holes(); len(hs) != 0 {
		t.Fatalf("the hole must be retired: %+v", hs)
	}
	if f.sink.count("bars hole RETIRED: MNQ 1m 10-14 03:44–07:48 CT on MNQ 12-26 — the subscription now names MNQ 03-27") != 1 {
		t.Fatalf("the retirement must be said once, naming both contracts:\n%s", f.sink.dump())
	}
	if d := f.delta(); d.Refused != 1 || d.Unfilled != 0 {
		t.Fatalf("refused +%d unfilled +%d — a retirement is counted as a refusal, never as unfilled", d.Refused, d.Unfilled)
	}
}

// ── #23 review P2-1 — a re-request goes out only early in a CT minute ─────
// The AddOn's rebuild freezes the symbol's forming bars for 20–35 s; a send
// late in a minute lets that freeze cross the boundary and store the minute
// truncated as LIVE. A due hole waits for the next tick inside the window.

func TestGapReRequestWaitsForTheStartOfAMinute(t *testing.T) {
	f := newGHFixture(t)
	det := f.openHole()                   // 07:49:00 CT; the first slot is due at 07:50:00
	f.tr.tick(det.Add(100 * time.Second)) // 07:50:40 — due, but late in the minute
	f.noNewReRequest(0, "a send at :40 would let the rebuild's freeze cross the minute boundary")
	if hs := f.holes(); len(hs) != 1 || hs[0].Attempts != 0 {
		t.Fatalf("the hole stays due, untouched: %+v", hs)
	}
	if d := f.delta(); d.Deferred != 0 || d.Refused != 0 {
		t.Fatalf("waiting for the window is not a refusal: deferred +%d refused +%d", d.Deferred, d.Refused)
	}
	f.tr.tick(det.Add(2*time.Minute + 5*time.Second)) // 07:51:05 — inside the window
	f.waitReRequests(1)
}

// TestSendWindowCatchesEveryTickerPhase — the window equals the tick period,
// so whatever second the 15 s ticker started on, exactly one tick per minute
// is inside it. (A 10 s window is missed by every tick of a ticker that
// started on :10–:14 — the hole would never be asked for.)
func TestSendWindowCatchesEveryTickerPhase(t *testing.T) {
	minute := ct(2026, 10, 14, 9, 0)
	for phase := time.Duration(0); phase < barHoleTickEvery; phase += 250 * time.Millisecond {
		in := 0
		for k := time.Duration(0); k < 4; k++ {
			if barHoleInSendWindow(minute.Add(phase + k*barHoleTickEvery)) {
				in++
			}
		}
		if in != 1 {
			t.Fatalf("ticker phase %s: %d ticks per minute inside the send window, want exactly 1", phase, in)
		}
	}
	if barHoleInSendWindow(minute.Add(25 * time.Second)) {
		t.Fatal(":25 must be outside the window (the review's limit)")
	}
}

// ── #23 review P2-3 — a hole no re-request can reach is retired ───────────
// A re-request replays the last bars_back (2000) 1m bars. A hole that opened
// before that reach was asked for 4 + 24/day, forever, and the UNFILLED line
// sent the owner to reload history that no replay could deliver.

func TestHoleOutsideTheReplayWindowIsRetired(t *testing.T) {
	f := newGHFixture(t)
	// m444 review round 2: a hole older than the STANDARD replay is asked for
	// DEEPLY; only one older than the deepest ask (8640 1m bars, ~6 sessions) is
	// retired — Fri 09-25 → Wed 10-14 is ~13 sessions.
	f.stream(ct(2026, 9, 25, 10, 0), ct(2026, 9, 25, 10, 5))   // Fri 09-25
	f.stream(ct(2026, 10, 14, 7, 48), ct(2026, 10, 14, 7, 49)) // Wed 10-14
	waitFor(t, "the tracker to open the Fri→Wed hole", func() bool { return len(f.holes()) == 1 })
	det := f.clk.Now()
	f.tr.tick(det.Add(61 * time.Second))
	f.noNewReRequest(0, "a hole beyond even the deepest ask is never asked for")
	// m444: a retired hole STAYS LISTED (state "retired") — dropping it let the
	// persistent gap audit re-find and re-announce it on every pass.
	if hs := f.holes(); len(hs) != 1 || hs[0].State != holeStateRetired {
		t.Fatalf("the hole must be retired and stay listed: %+v\n%s", hs, f.sink.dump())
	}
	if f.sink.count("bars hole RETIRED: MNQ 1m 09-25 10:06–10-14 07:48 CT on MNQ 12-26 — older than the bot can ask NT8 for: the standard re-request replays the last 2000 1m bars, a deep one at most 4800") != 1 ||
		f.sink.count("so this hole stays") != 1 {
		t.Fatalf("the retirement must be said once, naming both reaches:\n%s", f.sink.dump())
	}
	if d := f.delta(); d.Refused != 1 || d.Unfilled != 0 || d.Rerequests != 0 {
		t.Fatalf("refused +%d unfilled +%d sent +%d — retired once, counted, never sent", d.Refused, d.Unfilled, d.Rerequests)
	}
	f.tr.tick(det.Add(3*time.Minute + 5*time.Second))
	f.noNewReRequest(0, "a retired hole stays retired")
}

// countingHoleStore counts the window reads of one hole (the measure).
type countingHoleStore struct {
	holeStore
	mu      sync.Mutex
	fromMs  int64
	windows int
}

func (c *countingHoleStore) BarsBetweenOn(symbol, tf, contract string, fromMs, toMs int64) ([]store.BarHistoryDB, error) {
	c.mu.Lock()
	if fromMs == c.fromMs {
		c.windows++
	}
	c.mu.Unlock()
	return c.holeStore.BarsBetweenOn(symbol, tf, contract, fromMs, toMs)
}

func (c *countingHoleStore) reads() int { c.mu.Lock(); defer c.mu.Unlock(); return c.windows }

// TestSlowRetriesAreCappedInTotal — after the four scheduled attempts, at most
// barHoleSlowCapTotal (8) slow re-requests per hole, IN TOTAL (the old cap was
// 24 per session-day, reset at every 17:00 roll — forever). The spent line is
// said once, and a spent hole is never RE-REQUESTED again (#23 review P3-5).
// m445: it is re-MEASURED from the store at most once a minute so the gate can
// resume when a history reload refills it (TestSpentHoleIsRemeasuredAndResumes).
func TestSlowRetriesAreCappedInTotal(t *testing.T) {
	f := newGHFixture(t)
	f.addon.setAnswer(func(int, ntwire.BarsSubscribePayload) { f.answerEmpty() })
	cs := &countingHoleStore{holeStore: f.bh, fromMs: ct(2026, 10, 14, 3, 44).UnixMilli()}
	f.tr.mu.Lock()
	f.tr.st = cs
	f.tr.mu.Unlock()
	det := f.openHole()
	for i, at := range []time.Duration{61 * time.Second, 3*time.Minute + 5*time.Second, 10*time.Minute + 5*time.Second, 30*time.Minute + 5*time.Second} {
		f.tr.tick(det.Add(at))
		f.waitReRequests(i + 1)
	}
	unfilledAt := det.Add(32*time.Minute + 10*time.Second)
	f.tr.tick(unfilledAt) // the measure after #4 → UNFILLED
	if f.sink.count("UNFILLED") != 1 {
		t.Fatalf("fixture: want the hole declared unfilled:\n%s", f.sink.dump())
	}
	sent := 4
	for k := 1; k <= 10; k++ {
		f.tr.tick(unfilledAt.Add(time.Duration(k) * 15 * time.Minute))
		if k <= 8 { // the spec'd cap, pinned as a number (not the constant)
			sent++
			f.waitReRequests(sent)
		}
	}
	f.noNewReRequest(4+8, "the slow retries are capped at 8 in total")
	if n := f.sink.count("bars hole slow retries spent: MNQ 1m 10-14 03:44–07:48 CT on MNQ 12-26"); n != 1 {
		t.Fatalf("the spent line must be said exactly once, said %d:\n%s", n, f.sink.dump())
	}
	hs := f.holes()
	if len(hs) != 1 || hs[0].State != holeStateUnfilled || hs[0].NextAttemptAt != nil {
		t.Fatalf("a spent hole stays listed as unfilled with no next attempt: %+v", hs)
	}
	// m445: never RE-REQUESTED again (the noNewReRequest above) — but the
	// data-integrity gate re-MEASURES it from the store, at most once a minute
	// (remeasureRetired), so a history reload that refills it lifts the pause.
	// The reads are rate-limited: four ticks inside ONE minute read once.
	reads := cs.reads()
	base := unfilledAt.Add(11 * 15 * time.Minute)
	for k := 0; k < 4; k++ {
		f.tr.tick(base.Add(time.Duration(k) * 10 * time.Second))
	}
	if got := cs.reads(); got != reads+1 {
		t.Fatalf("a spent hole is re-measured at most once a minute: %d window reads → %d (want +1)", reads, got)
	}
	f.noNewReRequest(4+8, "a spent hole is measured, never re-requested")
}

// ── CTO fold 2 (10-14) — a hole OLDER than the boot is found ──────────────
// Release #23 booted 11:22:42 CT with ES 1m missing 03:44–07:48 (one stored
// bar in that span); /api/health bars_holes read []. The first judgement per
// key looked back only to the stored bar just before the first live minute,
// so a hole further back in the session was never judged. The boot scan reads
// back to the session open (17:00 CT) and tracks what it finds like any hole.
// m445 (review D2): it now reaches the deep integrity window's start.

func TestBootScanFindsAHoleOlderThanTheBoot(t *testing.T) {
	f := newGHFixture(t)
	var rows []store.BarHistoryDB
	add := func(from, to time.Time) {
		for m := from; !m.After(to); m = m.Add(time.Minute) {
			rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: "1m", Contract: ghContract, OpenTimeMs: m.UnixMilli(),
				O: 30700, H: 30701, L: 30699, C: 30700, V: 10, Source: ntwire.BarSourceHistorical})
		}
	}
	// what the previous process and the boot replay left in the store:
	// the session up to 03:43, nothing 03:44–07:47, then 07:48–09:00
	add(ct(2026, 10, 13, 17, 0), ct(2026, 10, 14, 3, 43))
	add(ct(2026, 10, 14, 7, 48), ct(2026, 10, 14, 9, 0))
	if err := f.bh.InsertBars(rows); err != nil {
		t.Fatal(err)
	}
	f.stream(ct(2026, 10, 14, 9, 1), ct(2026, 10, 14, 9, 2)) // the first live minutes after the boot
	waitFor(t, "the boot scan to track the 03:44–07:48 hole", func() bool { return len(f.holes()) == 1 })
	hs := f.holes()
	if hs[0].From != ct(2026, 10, 14, 3, 44).Format(time.RFC3339) || hs[0].To != ct(2026, 10, 14, 7, 48).Format(time.RFC3339) || hs[0].MissingMin != 244 {
		t.Fatalf("boot scan hole = %+v, want [03:44, 07:48) CT, 244 open min", hs[0])
	}
	if f.sink.count("🕳 boot hole scan: MNQ 1m on MNQ 12-26 09-18 11:01 → 10-14 09:01 · from the deepest integrity window (24720 open min back) · 1 hole(s) (10-14 03:44–07:48 CT, 244 open min) — tracked like a live hole") != 1 {
		t.Fatalf("the boot scan must say once what it covered and found:\n%s", f.sink.dump())
	}
	if d := f.delta(); d.Detected != 1 {
		t.Fatalf("bars_hole_detected +%d, want +1", d.Detected)
	}
	// tracked exactly like a live hole: asked for at the first slot
	det := f.clk.Now()
	f.tr.tick(det.Add(61 * time.Second))
	f.waitReRequests(1)
}

// The scan is ONE judgement: later frames judge only their new minutes, and a
// clean session says "0 hole(s)" once.
func TestBootScanSaysOnceAndFindsNothingInACleanSession(t *testing.T) {
	f := newGHFixture(t)
	var rows []store.BarHistoryDB
	for m := ct(2026, 10, 13, 17, 0); !m.After(ct(2026, 10, 14, 9, 0)); m = m.Add(time.Minute) {
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: "1m", Contract: ghContract, OpenTimeMs: m.UnixMilli(),
			O: 30700, H: 30701, L: 30699, C: 30700, V: 10, Source: ntwire.BarSourceHistorical})
	}
	if err := f.bh.InsertBars(rows); err != nil {
		t.Fatal(err)
	}
	f.stream(ct(2026, 10, 14, 9, 1), ct(2026, 10, 14, 9, 4))
	time.Sleep(200 * time.Millisecond)
	if hs := f.holes(); len(hs) != 0 {
		t.Fatalf("a clean session has no hole: %+v", hs)
	}
	if n := f.sink.count("🕳 boot hole scan: MNQ 1m on MNQ 12-26"); n != 1 {
		t.Fatalf("the boot scan line is said once per key, said %d:\n%s", n, f.sink.dump())
	}
	if f.sink.count("· 0 hole(s)") != 1 {
		t.Fatalf("a clean scan says 0 hole(s):\n%s", f.sink.dump())
	}
}
