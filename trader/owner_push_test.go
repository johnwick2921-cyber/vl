package trader

import (
	"strings"
	"sync"
	"testing"
	"time"

	"vl/internal/ownerpush"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// m432 (release #52) — OWNER REACHABILITY pins. Every test drives the PRODUCTION
// seam: monitorTick (the 60 s beat) -> checkFeedDown / checkUnfilledHole ->
// emitAlert -> maybeOwnerPush -> the process-default ownerpush channel. The
// sender is a recorder standing in for telegram.NotifyOwner.

type pushRec struct {
	mu   sync.Mutex
	msgs []string
}

func (r *pushRec) send(s string) { r.mu.Lock(); r.msgs = append(r.msgs, s); r.mu.Unlock() }
func (r *pushRec) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.msgs...)
}

// installPush swaps in a recording owner channel and returns it plus a flush.
func installPush(t *testing.T) (*pushRec, func()) {
	t.Helper()
	// the CME-open leg judges at THIS instant, never the wall clock
	fixed := cmeOpenNow(t)
	oldClk := ownerPushClock
	ownerPushClock = func() time.Time { return fixed }
	t.Cleanup(func() { ownerPushClock = oldClk })
	rec := &pushRec{}
	p := ownerpush.New(rec.send)
	t.Cleanup(ownerpush.Use(p))
	return rec, p.Flush
}

func countContaining(msgs []string, sub string) int {
	n := 0
	for _, m := range msgs {
		if strings.Contains(m, sub) {
			n++
		}
	}
	return n
}

// MUTANT (shown RED): drop the at.maybeOwnerPush call in emitAlert -> no down
// message -> RED. MUTANT 2: drop the noteFeedBack call in checkFeedDown -> no
// "BACK" message -> RED.
func TestFeedDownPushesOwnerOncePerEpisodeAndTellsHimWhenItIsBack(t *testing.T) {
	rec, flush := installPush(t)
	at, st := feedTrader(t)
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 11*60_000 - 60_000}})

	at.monitorTick(now)
	at.monitorTick(now.Add(time.Minute))
	at.monitorTick(now.Add(2 * time.Minute)) // the outage goes on: still ONE message
	flush()
	if got := feedAlerts(t, st, "feed-down"); got != 1 {
		t.Fatalf("in-app alert rows = %d, want 1", got)
	}
	msgs := rec.all()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "NT8 bar feed DOWN") {
		t.Fatalf("one down message per episode, got %q", msgs)
	}
	if !strings.Contains(msgs[0], "connect NinjaTrader's data connection") {
		t.Fatalf("the message must say what to do, got %q", msgs[0])
	}

	// the feed returns: a bar 30 s old -> ONE "BACK" message, then silence.
	later := now.Add(3 * time.Minute)
	withProvider(t, []market.Kline{{OpenTime: later.UnixMilli() - 30_000 - 60_000}})
	at.monitorTick(later)
	at.monitorTick(later.Add(time.Minute))
	flush()
	msgs = rec.all()
	if len(msgs) != 2 || countContaining(msgs, "BACK") != 1 {
		t.Fatalf("want down + exactly one BACK, got %q", msgs)
	}

	// a NEW outage later is a NEW episode: down again.
	t2 := later.Add(30 * time.Minute)
	withProvider(t, []market.Kline{{OpenTime: t2.UnixMilli() - 15*60_000 - 60_000}})
	at.monitorTick(t2)
	flush()
	if got := countContaining(rec.all(), "NT8 bar feed DOWN"); got != 2 {
		t.Fatalf("a new episode must push again, down messages = %d: %q", got, rec.all())
	}
}

// Two traders on one box raise two alert rows for the SAME episode; the owner is
// told once (down) and once (back).
func TestFeedDownPushIsOncePerEpisodeAcrossTraders(t *testing.T) {
	rec, flush := installPush(t)
	a, _ := feedTrader(t)
	b, _ := feedTrader(t)
	b.id = "t2"
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 11*60_000 - 60_000}})
	a.monitorTick(now)
	b.monitorTick(now)
	flush()
	if got := countContaining(rec.all(), "NT8 bar feed DOWN"); got != 1 {
		t.Fatalf("two traders, one episode: %d down messages: %q", got, rec.all())
	}
	later := now.Add(4 * time.Minute)
	withProvider(t, []market.Kline{{OpenTime: later.UnixMilli() - 60_000 - 20_000}})
	a.monitorTick(later)
	b.monitorTick(later)
	flush()
	if got := countContaining(rec.all(), "BACK"); got != 1 {
		t.Fatalf("two traders, one recovery: %d BACK messages: %q", got, rec.all())
	}
}

// The weekend / daily break is not an outage: no alert, no push, no BACK.
func TestNoOwnerPushWhileCMEIsClosed(t *testing.T) {
	rec, flush := installPush(t)
	at, _ := feedTrader(t)
	saturday := ctTimeAt(t, "2026-08-15", "12:00")
	withProvider(t, []market.Kline{{OpenTime: saturday.UnixMilli() - 3*3_600_000}})
	at.monitorTick(saturday)
	flush()
	if len(rec.all()) != 0 {
		t.Fatalf("CME closed must never push: %q", rec.all())
	}
	// and the second leg: even a feed-down row emitted directly is not pushed closed.
	ownerPushClock = func() time.Time { return saturday }
	at.emitAlert("P0", "feed-down", "feeddown:closed", "NT8 bar feed DOWN", "x")
	flush()
	if len(rec.all()) != 0 {
		t.Fatalf("a feed-down row at a closed market must not push: %q", rec.all())
	}
}

// The kinds the owner must hear about while the bot cannot trade are pushed;
// plan-content and P1/P2 ops chatter are not.
func TestOwnerPushKindsAreExactlyTheOpsP0Set(t *testing.T) {
	rec, flush := installPush(t)
	at, _ := feedTrader(t)
	pushed := []string{"trader_down_no_evaluator", "dead_man_reconcile_stuck", "unprotected_position",
		"protection_partial", "protection_pending_stuck", "protection_over_covered"}
	for _, k := range pushed {
		at.emitAlert("P0", k, "ev-"+k, "title "+k, "body")
	}
	at.emitAlert("P0", "plan-died", "ev-plan", "plan died", "body")   // plan content: in-app only
	at.emitAlert("P1", "unprotected_position", "ev-p1", "p1", "body") // not P0
	at.emitAlert("P2", "fill", "ev-fill", "fill", "body")
	flush()
	msgs := rec.all()
	if len(msgs) != len(pushed) {
		t.Fatalf("want %d pushes, got %d: %q", len(pushed), len(msgs), msgs)
	}
	for _, k := range pushed {
		if countContaining(msgs, "title "+k) != 1 {
			t.Fatalf("kind %s not pushed exactly once: %q", k, msgs)
		}
	}
	if countContaining(msgs, "(trader t1)") != len(pushed) {
		t.Fatalf("trader-scoped kinds must name the trader: %q", msgs)
	}
}

// A friend instance never posts (it has no Telegram); the push is refused at the
// channel even if something wired a sender.
func TestOwnerPushNeverFiresOnAFriendInstance(t *testing.T) {
	t.Setenv("VL_FRIEND_INSTANCE", "1")
	rec, flush := installPush(t)
	at, st := feedTrader(t)
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 11*60_000 - 60_000}})
	at.monitorTick(now)
	flush()
	if len(rec.all()) != 0 {
		t.Fatalf("friend instance pushed: %q", rec.all())
	}
	if got := feedAlerts(t, st, "feed-down"); got != 1 {
		t.Fatalf("the in-app alert must still be raised on a friend instance, got %d", got)
	}
}

// No sender wired (no Telegram chat bound / not started) is a quiet no-op: the
// alert row is still written and nothing panics.
func TestNoSenderIsANoOp(t *testing.T) {
	t.Cleanup(ownerpush.Use(nil))
	at, st := feedTrader(t)
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 11*60_000 - 60_000}})
	at.monitorTick(now)
	if got := feedAlerts(t, st, "feed-down"); got != 1 {
		t.Fatalf("alert row = %d, want 1", got)
	}
}

// ── item 2: the unfillable hole ─────────────────────────────────────────────

// MUTANT (shown RED): drop the at.checkUnfilledHole call from monitorTick -> no
// alert, no push -> RED.
func TestUnfilledHoleRaisesOneP1AndOneOwnerMessage(t *testing.T) {
	rec, flush := installPush(t)
	at, st := feedTrader(t)
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 90_000}}) // live feed: no FEED DOWN
	hole := ntTrader.UnfilledHole{Symbol: "MNQ", Contract: "MNQ 12-26", FromMs: now.UnixMilli() - 640*60_000, ToMs: now.UnixMilli(),
		Missing: 640, Attempts: 4, Window: "10-25 17:00–10-26 03:40 CT",
		ReaskEvery: 15 * time.Minute, ReaskHorizon: 2 * time.Hour}
	old := unfilledHolesFn
	unfilledHolesFn = func(sym string, _ time.Time) []ntTrader.UnfilledHole {
		if sym != at.futuresSymbol() {
			t.Errorf("asked about %q, want the traded symbol %q", sym, at.futuresSymbol())
		}
		return []ntTrader.UnfilledHole{hole}
	}
	t.Cleanup(func() { unfilledHolesFn = old })

	at.monitorTick(now)
	at.monitorTick(now.Add(time.Minute)) // the hole persists: still ONE alert, ONE message
	flush()

	alerts, _ := st.Alert().List("t1", 20)
	var rows []*store.AlertDB
	for _, a := range alerts {
		if a.Kind == alertKindBarHoleUnfilled {
			rows = append(rows, a)
		}
	}
	if len(rows) != 1 || rows[0].Level != "P1" {
		t.Fatalf("want exactly one P1 bar-hole-unfilled row, got %+v", rows)
	}
	want := "NT8 has no MNQ history for 10-25 17:00–10-26 03:40 CT. In NinjaTrader: open an MNQ 1-minute chart, right-click, Reload All Historical Data. The bot re-asks NT8 about every 15 minutes for about 2 hours and picks the reload up on its next re-request; if you reload later than that, restart the bot afterwards (it re-subscribes on start) — you will get a second message when it stops re-asking."
	if rows[0].Body != want {
		t.Fatalf("body = %q\nwant   %q", rows[0].Body, want)
	}
	msgs := rec.all()
	if len(msgs) != 1 || !strings.Contains(msgs[0], want) {
		t.Fatalf("want one owner message carrying the instruction, got %q", msgs)
	}
	if !isOpsAlertKind(alertKindBarHoleUnfilled) {
		t.Fatal("bar-hole-unfilled must be an ops kind (reaches the owner without a day plan)")
	}
}

func TestNoUnfilledHoleNoAlert(t *testing.T) {
	rec, flush := installPush(t)
	at, st := feedTrader(t)
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 90_000}})
	old := unfilledHolesFn
	unfilledHolesFn = func(string, time.Time) []ntTrader.UnfilledHole { return nil }
	t.Cleanup(func() { unfilledHolesFn = old })
	at.monitorTick(now)
	flush()
	if got := feedAlerts(t, st, alertKindBarHoleUnfilled); got != 0 || len(rec.all()) != 0 {
		t.Fatalf("no unfilled hole -> no alert (%d) and no push (%q)", got, rec.all())
	}
}

// A hole whose refills were rejected as another price scale must NOT tell the
// owner to reload history first (the log's scale lines are the story).
func TestUnfilledHoleScaleStoryDoesNotSendTheOwnerToReload(t *testing.T) {
	body := unfilledHoleBody(ntTrader.UnfilledHole{Symbol: "MNQ", Window: "w CT", Attempts: 4, ReplayDisagreed: 2})
	if strings.Contains(body, "Reload All Historical Data") || !strings.Contains(body, "price scale") {
		t.Fatalf("scale-story body wrong: %q", body)
	}
}
