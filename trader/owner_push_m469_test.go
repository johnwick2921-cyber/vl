package trader

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/discipline"
	"vl/internal/ownerpush"
	"vl/kernel"
	"vl/safe"
	"vl/store"
)

// m469 — the P0s that never reached the owner. Every pin drives the production
// seam (on402Failure / onAISuccess / noteAICallFailure / recoverPanic /
// goNetted / reconcileProtectionAt -> emitAlert -> maybeOwnerPush -> the
// process-default ownerpush channel). The sender is switchable, so "the box
// cannot reach Telegram" is a sender that returns an error, and the channel's
// own clock is driven by hand.

// m469Net is a Telegram stand-in that can be cut off and restored.
type m469Net struct {
	mu   sync.Mutex
	down bool
	got  []string
	n    int
}

func (s *m469Net) send(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	if s.down {
		return errors.New(`Post "https://api.telegram.org/bot[redacted]/sendMessage": dial tcp: lookup api.telegram.org: no such host`)
	}
	s.got = append(s.got, text)
	return nil
}
func (s *m469Net) setDown(d bool) { s.mu.Lock(); s.down = d; s.mu.Unlock() }
func (s *m469Net) attempts() int  { s.mu.Lock(); defer s.mu.Unlock(); return s.n }
func (s *m469Net) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.got...)
}

type m469Clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *m469Clock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *m469Clock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// m469Push installs a switchable channel whose clock starts at start.
func m469Push(t *testing.T, start time.Time) (*m469Net, *ownerpush.Pusher, *m469Clock) {
	t.Helper()
	net := &m469Net{}
	clk := &m469Clock{t: start}
	p := ownerpush.NewE(net.send)
	p.SetClockForTest(clk.now)
	t.Cleanup(ownerpush.Use(p))
	return net, p, clk
}

// m469Trader is a trader WITHOUT a day plan (every mentor trader): a pushed P0
// must still write its row, or it is never pushed (the ops-kind gate).
func m469Trader(t *testing.T, id string) (*AutoTrader, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "m469.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	at := &AutoTrader{id: id, name: "hoang", store: st, exchange: "ninjatrader"}
	at.config.Exchange = "ninjatrader"
	at.config.StrategyConfig = &store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: false}}
	t.Cleanup(func() { aiOutages.Delete(id); protectionUnverifiedSince.Delete(id) })
	return at, st
}

func m469Rows(t *testing.T, st *store.Store, traderID, kind string) []*store.AlertDB {
	t.Helper()
	alerts, err := st.Alert().List(traderID, 200)
	if err != nil {
		t.Fatal(err)
	}
	var out []*store.AlertDB
	for _, a := range alerts {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}

// drive advances the channel's clock in 30 s steps, re-offering held messages
// each step the way the production 30 s retry loop does.
func m469Drive(p *ownerpush.Pusher, c *m469Clock, d time.Duration) {
	for el := time.Duration(0); el < d; el += 30 * time.Second {
		c.add(30 * time.Second)
		p.Retry()
		p.Flush()
	}
}

var m469Err402 = errors.New(`API returned error (status 402): {"error":{"message":"Insufficient Balance"}}`)

// THE 11-03..11-06 SHAPE: the 402 P0 is written, the push fails (the box cannot
// reach Telegram), the message is HELD, and it is delivered once the network
// returns - for a trader with no day plan.
// MUTANT (shown RED): remove "ai-payment" from ownerPushKind -> nothing is ever
// sent -> RED. MUTANT 2: remove "ai-payment" from isOpsAlertKind -> no row for
// a day-plan-off trader, nothing pushed -> RED.
func TestAI402IsHeldThroughANetworkOutageAndDeliveredAfter(t *testing.T) {
	t0 := time.Date(2026, 11, 4, 2, 10, 0, 0, time.UTC) // 11-03 20:10 CT
	net, p, clk := m469Push(t, t0)
	at, st := m469Trader(t, "m469-held")
	net.setDown(true)

	at.on402Failure(t0, m469Err402)
	p.Flush()
	if rows := m469Rows(t, st, at.id, "ai-payment"); len(rows) != 1 {
		t.Fatalf("a day-plan-off trader must write the ai-payment P0 row, rows = %d", len(rows))
	}
	if net.attempts() == 0 {
		t.Fatal("the ai-payment P0 was never offered to Telegram")
	}
	m469Drive(p, clk, 3*time.Hour) // still offline: the P0 stays held, never stale
	if st := p.Stats(); st.Held == 0 || st.Stale != 0 || len(net.sent()) != 0 {
		t.Fatalf("an offline P0 must stay held (never stale, nothing delivered): %+v sent=%q", st, net.sent())
	}
	net.setDown(false)
	m469Drive(p, clk, 6*time.Minute)
	got := strings.Join(net.sent(), "\n---\n")
	if !strings.Contains(got, "AI CREDIT EXHAUSTED") || !strings.Contains(got, "(trader hoang)") {
		t.Fatalf("the held 402 P0 was not delivered after the network returned:\n%s", got)
	}
	if strings.Contains(got, "CT CT") {
		t.Fatalf("a doubled zone label reached the owner:\n%s", got)
	}
}

// The hourly STILL DOWN: not before an hour, once per full hour, and a newer
// reminder withdraws an older one still held.
// MUTANT (shown RED): drop the at.aiStillDownReminder call in on402Failure ->
// no STILL DOWN -> RED.
func TestAIStillDownReminderEveryHourOfA402Outage(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	net, p, _ := m469Push(t, t0)
	at, _ := m469Trader(t, "m469-remind")
	for m := 0; m <= 59; m += 3 { // the decision cycles of the first hour
		at.on402Failure(t0.Add(time.Duration(m)*time.Minute), m469Err402)
	}
	p.Flush()
	if n := countContaining(net.sent(), "STILL DOWN"); n != 0 {
		t.Fatalf("a STILL DOWN before a full hour: %q", net.sent())
	}
	at.on402Failure(t0.Add(61*time.Minute), m469Err402)
	at.on402Failure(t0.Add(64*time.Minute), m469Err402) // same hour: no second reminder
	p.Flush()
	if n := countContaining(net.sent(), "AI STILL DOWN since"); n != 1 {
		t.Fatalf("want exactly one STILL DOWN after the first hour, got %d: %q", n, net.sent())
	}
	at.on402Failure(t0.Add(121*time.Minute), m469Err402)
	p.Flush()
	if n := countContaining(net.sent(), "AI STILL DOWN since"); n != 2 {
		t.Fatalf("want a second STILL DOWN after the second hour, got %d: %q", n, net.sent())
	}
	if !strings.Contains(net.sent()[len(net.sent())-1], "(2h01m)") {
		t.Fatalf("the reminder must say how long: %q", net.sent())
	}
}

// "AI back": the first successful call after an outage withdraws the held
// CREDIT EXHAUSTED and STILL DOWN (stale news) and sends ONE "AI is BACK".
// MUTANT (shown RED): drop the ownerpush.Cancel of the ai-payment key in
// endAIOwnerEpisode -> the stale CREDIT EXHAUSTED is delivered after the BACK -> RED.
func TestAIBackCancelsTheHeldOutageMessages(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	net, p, clk := m469Push(t, t0)
	at, st := m469Trader(t, "m469-back")
	net.setDown(true)
	at.on402Failure(t0, m469Err402)
	at.on402Failure(t0.Add(61*time.Minute), m469Err402)
	p.Flush()
	m469Drive(p, clk, 10*time.Minute)

	at.onAISuccess(t0.Add(2 * time.Hour)) // topped up
	p.Flush()
	if rows := m469Rows(t, st, at.id, "ai-payment"); len(rows) != 1 || !rows[0].Acked {
		t.Fatalf("the success must still auto-ack the banner: %+v", rows)
	}
	net.setDown(false)
	m469Drive(p, clk, 6*time.Minute)
	sent := net.sent()
	if n := countContaining(sent, "AI is BACK"); n != 1 {
		t.Fatalf("want one AI is BACK, got %d: %q", n, sent)
	}
	if !strings.Contains(strings.Join(sent, "\n"), "after 2h00m (402 Insufficient Balance since 11-05 08:00 CT, back 10:00 CT)") {
		t.Fatalf("the BACK must say how long the AI was down, in CT: %q", sent)
	}
	for _, stale := range []string{"AI CREDIT EXHAUSTED", "AI STILL DOWN"} {
		if countContaining(sent, stale) != 0 {
			t.Fatalf("%q was delivered after the outage ended (it was held, it must be withdrawn): %q", stale, sent)
		}
	}
	// a later success says nothing more
	at.onAISuccess(t0.Add(3 * time.Hour))
	p.Flush()
	if n := countContaining(net.sent(), "AI is BACK"); n != 1 {
		t.Fatalf("a success with no outage open must not push: %q", net.sent())
	}
}

// Non-402 failures: ONE P0 at aiFailStreakAlert in a row, nothing before it,
// nothing more in the same streak; a success ends it with ONE "AI is BACK";
// a 402 is not counted (it has its own outage).
// MUTANT (shown RED): change `n >= aiFailStreakAlert` to `n > aiFailStreakAlert`
// -> no row after exactly 5 -> RED.
func TestAIFailingStreakRaisesOneP0(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	net, p, _ := m469Push(t, t0)
	at, st := m469Trader(t, "m469-streak")
	timeout := errors.New("context deadline exceeded")
	for i := 0; i < aiFailStreakAlert-1; i++ {
		at.noteAICallFailure(t0.Add(time.Duration(i)*3*time.Minute), timeout)
		at.noteAICallFailure(t0.Add(time.Duration(i)*3*time.Minute+time.Second), m469Err402) // never counted
	}
	if rows := m469Rows(t, st, at.id, "ai-failing"); len(rows) != 0 {
		t.Fatalf("%d failures in a row must not page yet, rows = %d", aiFailStreakAlert-1, len(rows))
	}
	for i := aiFailStreakAlert - 1; i < 12; i++ {
		at.noteAICallFailure(t0.Add(time.Duration(i)*3*time.Minute), timeout)
	}
	p.Flush()
	if rows := m469Rows(t, st, at.id, "ai-failing"); len(rows) != 1 {
		t.Fatalf("one streak = one P0 row, got %d", len(rows))
	}
	if n := countContaining(net.sent(), "AI calls keep failing"); n != 1 {
		t.Fatalf("one streak = one push, got %d: %q", n, net.sent())
	}
	at.onAISuccess(t0.Add(40 * time.Minute))
	p.Flush()
	if n := countContaining(net.sent(), "AI is BACK"); n != 1 {
		t.Fatalf("the streak's end must push one AI is BACK: %q", net.sent())
	}
	// a success resets the count: a new streak needs aiFailStreakAlert again, and is a new episode
	for i := 0; i < aiFailStreakAlert; i++ {
		at.noteAICallFailure(t0.Add(time.Hour+time.Duration(i)*3*time.Minute), timeout)
	}
	if rows := m469Rows(t, st, at.id, "ai-failing"); len(rows) != 2 {
		t.Fatalf("a new streak after a success must raise a new P0, rows = %d", len(rows))
	}
}

// trader_panic: one P0 and one push per trader per CME session day. The old id
// ("panic:"+id) was deduped by the store forever - one push per trader EVER.
// MUTANT (shown RED): put "panic:"+at.id back -> the event id carries no day -> RED.
func TestTraderPanicEventIDCarriesTheSessionDay(t *testing.T) {
	discipline.ResetFreezeForTest() // a panic freezes the trader (process-wide state)
	t.Cleanup(discipline.ResetFreezeForTest)
	_, p, _ := m469Push(t, time.Now())
	at, st := m469Trader(t, "m469-panic")
	before := kernel.CMESessionDayKey(time.Now())
	at.runBeatSafely("m469-beat", func() { panic("boom") })
	at.runBeatSafely("m469-beat", func() { panic("boom again") }) // the next beat re-panics: same episode
	after := kernel.CMESessionDayKey(time.Now())
	p.Flush()
	rows := m469Rows(t, st, at.id, "trader_panic")
	if len(rows) != 1 {
		t.Fatalf("two panics in one session day = one row, got %d", len(rows))
	}
	if ev := rows[0].EventID; ev != "panic:"+at.id+":"+before && ev != "panic:"+at.id+":"+after {
		t.Fatalf("trader_panic event id %q must end with the CME session day %q", ev, before)
	}
	// a stored row from an EARLIER day (the old process) must not silence today's
	if _, err := st.Alert().Emit(&store.AlertDB{TraderID: "m469-panic-2", Level: "P0", Kind: "trader_panic",
		EventID: "panic:m469-panic-2:2026-01-02", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	at.id = "m469-panic-2"
	t.Cleanup(func() { aiOutages.Delete("m469-panic-2") })
	at.runBeatSafely("m469-beat", func() { panic("boom") })
	if rows := m469Rows(t, st, at.id, "trader_panic"); len(rows) != 2 {
		t.Fatalf("an earlier day's panic row must not dedupe today's, rows = %d", len(rows))
	}
}

// goroutine_panic: same day key, through the real safe.GoNet net.
// MUTANT (shown RED): put "panic:"+at.id+":"+name back -> no day in the id -> RED.
func TestGoroutinePanicEventIDCarriesTheSessionDay(t *testing.T) {
	discipline.ResetFreezeForTest()
	t.Cleanup(discipline.ResetFreezeForTest)
	at, st := m469Trader(t, "m469-gonet")
	safe.TestPanicNextGoNet.Store(true)
	t.Cleanup(func() { safe.TestPanicNextGoNet.Store(false) })
	before := kernel.CMESessionDayKey(time.Now())
	at.goNetted("m469-g", func() {})
	deadline := time.Now().Add(10 * time.Second)
	var rows []*store.AlertDB
	for time.Now().Before(deadline) {
		if rows = m469Rows(t, st, at.id, "goroutine_panic"); len(rows) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(rows) != 1 {
		t.Fatalf("want one goroutine_panic row, got %d", len(rows))
	}
	after := kernel.CMESessionDayKey(time.Now())
	prefix := "panic:" + at.id + ":m469-g:"
	if ev := rows[0].EventID; ev != prefix+before && ev != prefix+after {
		t.Fatalf("goroutine_panic event id %q must be %q + the CME session day %q", ev, prefix, before)
	}
}

// errPositionsTrader: NinjaTrader's positions cannot be read.
type errPositionsTrader struct{ MockTrader }

func (e *errPositionsTrader) GetPositions() ([]map[string]interface{}, error) {
	return nil, errors.New("NT8 account positions unknown: no account snapshot or connection")
}

// UNVERIFIED for longer than protectionUnverifiedAlertAfter with a position open
// = ONE P0 per spell, pushed. Shorter spells, and spells with nothing open, are
// silent; a pass that reads the book flat ends the spell.
// MUTANT (shown RED): drop the noteProtectionUnverified call on the
// positions-unreadable path -> no row after 10 min -> RED.
func TestProtectionUnverifiedTooLongWithAPositionOpenIsAP0(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC)
	net, p, _ := m469Push(t, t0)
	at, st := m469Trader(t, "m469-unverified")
	at.trader = &errPositionsTrader{}

	// nothing open locally: hours of UNVERIFIED (NT8 closed overnight) stay silent
	for m := 0; m <= 60; m++ {
		at.reconcileProtectionAt(t0.Add(time.Duration(m)*time.Minute), "monitor")
	}
	if rows := m469Rows(t, st, at.id, alertKindProtectionUnverified); len(rows) != 0 {
		t.Fatalf("UNVERIFIED with no position open must not page, rows = %d", len(rows))
	}
	// the book reads flat once: the spell ends
	at.trader = &MockTrader{positions: []map[string]interface{}{}}
	at.reconcileProtectionAt(t0.Add(61*time.Minute), "monitor")

	if err := st.Position().Create(&store.TraderPosition{TraderID: at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryPrice: 21500, Status: "OPEN", EntryTime: 1, CreatedAt: 1, UpdatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	at.trader = &errPositionsTrader{}
	t1 := t0.Add(62 * time.Minute)
	for m := 0; m < 10; m++ { // 9 minutes: a reconnect-length spell
		at.reconcileProtectionAt(t1.Add(time.Duration(m)*time.Minute), "monitor")
	}
	if rows := m469Rows(t, st, at.id, alertKindProtectionUnverified); len(rows) != 0 {
		t.Fatalf("a spell shorter than %s must not page, rows = %d", protectionUnverifiedAlertAfter, len(rows))
	}
	for m := 10; m < 20; m++ {
		at.reconcileProtectionAt(t1.Add(time.Duration(m)*time.Minute), "monitor")
	}
	p.Flush()
	if rows := m469Rows(t, st, at.id, alertKindProtectionUnverified); len(rows) != 1 {
		t.Fatalf("one spell past %s with a position open = one P0 row, got %d", protectionUnverifiedAlertAfter, len(rows))
	}
	if n := countContaining(net.sent(), "its stop cannot be verified"); n != 1 {
		t.Fatalf("one spell = one push, got %d: %q", n, net.sent())
	}
	// the broker reports the position but there is no book: still UNVERIFIED,
	// still the same spell (no second row)
	at.trader = &MockTrader{positions: []map[string]interface{}{{"symbol": "MNQ", "side": "long", "quantity": 1.0}}}
	at.reconcileProtectionAt(t1.Add(21*time.Minute), "monitor")
	if rows := m469Rows(t, st, at.id, alertKindProtectionUnverified); len(rows) != 1 {
		t.Fatalf("the spell goes on with the broker read but no book: rows = %d, want 1", len(rows))
	}
	if _, open := protectionUnverifiedSince.Load(at.id); !open {
		t.Fatal("a pass with no broker book must keep the UNVERIFIED spell open")
	}
}

// The BROKER-READ path: NinjaTrader reports the position, but its order book is
// absent (the reconnect edge fires before any fresh order_snapshot), so the
// stop cannot be verified. A spell that starts and stays on this path pages
// once it passes protectionUnverifiedAlertAfter - not before.
// MUTANT (shown RED): replace the broker-path noteProtectionUnverified call
// with `_ = unverified` -> no row after 11 minutes -> RED.
func TestProtectionUnverifiedOnTheBrokerReadPathIsAP0(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC)
	net, p, _ := m469Push(t, t0)
	at, st := m469Trader(t, "m469-unverified-broker")
	at.trader = &MockTrader{positions: []map[string]interface{}{{"symbol": "MNQ", "side": "long", "quantity": 1.0}}}
	for m := 0; m < 10; m++ {
		at.reconcileProtectionAt(t0.Add(time.Duration(m)*time.Minute), "monitor")
	}
	if rows := m469Rows(t, st, at.id, alertKindProtectionUnverified); len(rows) != 0 {
		t.Fatalf("a broker-read spell shorter than %s must not page, rows = %d", protectionUnverifiedAlertAfter, len(rows))
	}
	for m := 10; m <= 12; m++ {
		at.reconcileProtectionAt(t0.Add(time.Duration(m)*time.Minute), "monitor")
	}
	p.Flush()
	rows := m469Rows(t, st, at.id, alertKindProtectionUnverified)
	if len(rows) != 1 {
		t.Fatalf("a broker-read spell past %s = one P0 row, got %d", protectionUnverifiedAlertAfter, len(rows))
	}
	if !strings.Contains(rows[0].Body, "long MNQ ×1") {
		t.Fatalf("the P0 must name the position the broker reports: %q", rows[0].Body)
	}
	if n := countContaining(net.sent(), "its stop cannot be verified"); n != 1 {
		t.Fatalf("one spell = one push, got %d: %q", n, net.sent())
	}
}

// A pass that VERIFIES every open position (a fresh book with the working
// stop) ends the spell. Without that, the next reconnect - the book not yet
// re-sent, a 2-minute UNVERIFIED spell - would inherit an hours-old start and
// page on its first pass: a false P0 on every reconnect.
// MUTANT (shown RED): delete `protectionUnverifiedSince.Delete(at.id) // every
// open position verified` -> the 2-minute spell pages -> RED.
func TestProtectionVerifiedPassEndsTheUnverifiedSpell(t *testing.T) {
	noFreshPrice(t)
	w := newExecWire(t, "m469-unverified-reset")
	t.Cleanup(func() { protectionUnverifiedSince.Delete(w.at.id); aiOutages.Delete(w.at.id) })
	t0 := time.Now()
	net, p, _ := m469Push(t, t0)
	sid := w.open654(t)

	for m := 0; m < 9; m++ { // no book yet: UNVERIFIED for 8 minutes
		w.at.reconcileProtectionAt(t0.Add(time.Duration(m)*time.Minute), "monitor")
	}
	if _, open := protectionUnverifiedSince.Load(w.at.id); !open {
		t.Fatal("fixture: a pass with no book must open the UNVERIFIED spell")
	}
	// the book arrives with the -sl working at the plan stop: verified
	w.bookWithStop(sid, 30551.25, t0.Add(9*time.Minute+30*time.Second))
	w.at.reconcileProtectionAt(t0.Add(10*time.Minute), "monitor")
	if _, open := protectionUnverifiedSince.Load(w.at.id); open {
		t.Fatal("a pass that verified every open position must end the spell")
	}
	// a reconnect: the book is old again, 2 minutes of UNVERIFIED
	stale := snapshotMaxAge() + time.Minute
	w.at.reconcileProtectionAt(t0.Add(10*time.Minute+stale), "reconnect")
	w.at.reconcileProtectionAt(t0.Add(12*time.Minute+stale), "monitor")
	p.Flush()
	if rows := m469Rows(t, w.st, w.at.id, alertKindProtectionUnverified); len(rows) != 0 {
		t.Fatalf("a 2-minute spell after a verified pass must not page (it is a new spell), rows = %d: %+v", len(rows), rows[0])
	}
	if n := countContaining(net.sent(), "its stop cannot be verified"); n != 0 {
		t.Fatalf("no push for a short spell after a verified pass: %q", net.sent())
	}
}
