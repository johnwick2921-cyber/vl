package trader

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vl/internal/ownerpush"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// m438 (release #53) — owner-push hardening. Every pin drives the production
// seam: actOnProtection / monitorTick -> emitAlert -> maybeOwnerPush -> the
// process-default ownerpush channel (a recorder stands in for telegram).

const m438LongID = "8d5c8af5_396db319-d3fe-4b2e-9a1c-0f7e6d5c4b3a_deepseek_mentor_1791870197"

func unprotectedRows(t *testing.T, st *store.Store, traderID string) int {
	t.Helper()
	alerts, err := st.Alert().List(traderID, 200)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range alerts {
		if a.Kind == "unprotected_position" {
			n++
		}
	}
	return n
}

// (1) The PLACE verdict repeats every 1-minute pass while a stop stays refused or
// unconfirmed. ONE episode = ONE in-app P0 + ONE owner push; leaving place and
// returning is a NEW episode.
//
// MUTANT (shown RED): put now.UnixMilli() back in the protectionPlace event id
// (the old per-pass id) -> 5 rows, and the owner is pushed on every pass -> RED.
func TestProtectionPlaceEpisodeRaisesOneAlertAndOnePush(t *testing.T) {
	rec, flush := installPush(t)
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	at.id = "m438-place-episode" // protection state maps are package-level
	at.exchange = "ninjatrader"
	at.trader = &MockTrader{}
	place := protectionVerdict{Action: protectionPlace, StopPx: 31163, NeedQty: 1, Source: "plan", Why: "no stop at the broker"}
	ok := protectionVerdict{Action: protectionOK, Why: "protected"}

	t0 := time.Now()
	for i := 0; i < 5; i++ { // five 1-minute passes, the stop never confirms
		at.actOnProtection(place, "MNQ", "LONG", 1, "sig", "t", 0, t0.Add(time.Duration(i)*time.Minute))
	}
	flush()
	if n := unprotectedRows(t, st, at.id); n != 1 {
		t.Fatalf("5 passes of one place episode wrote %d in-app P0 rows, want 1", n)
	}
	if n := countContaining(rec.all(), "Open position had NO stop"); n != 1 {
		t.Fatalf("5 passes of one place episode pushed the owner %d times, want 1: %q", n, rec.all())
	}

	// protected, then naked again: a NEW episode, a NEW alert, a NEW push.
	at.actOnProtection(ok, "MNQ", "LONG", 1, "sig", "t", 0, t0.Add(10*time.Minute))
	at.actOnProtection(place, "MNQ", "LONG", 1, "sig", "t", 0, t0.Add(11*time.Minute))
	flush()
	if n := unprotectedRows(t, st, at.id); n != 2 {
		t.Fatalf("a second place episode must raise a second P0, rows = %d", n)
	}
	if n := countContaining(rec.all(), "Open position had NO stop"); n != 2 {
		t.Fatalf("a second place episode must push again, pushes = %d", n)
	}
}

// (1b) A flood of one kind (any shape of repeating alert) must leave FEED DOWN and
// TRADER DOWN deliverable — through the real emitAlert path. MUTANT: see
// internal/ownerpush TestOneKindCannotStarveAnother (single shared bucket).
func TestFloodOfProtectionAlertsCannotSilenceFeedDown(t *testing.T) {
	rec, flush := installPush(t)
	at, _ := feedTrader(t)
	for i := 0; i < 40; i++ {
		at.emitAlert("P0", "unprotected_position", fmt.Sprintf("unprotected:MNQ:LONG:%d", i), "naked", "body")
	}
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 11*60_000 - 60_000}})
	at.monitorTick(now) // FEED DOWN
	at.emitAlert("P0", "trader_down_no_evaluator", "down-1", "Trader x is DOWN", "body")
	flush()
	msgs := rec.all()
	if countContaining(msgs, "NT8 bar feed DOWN") != 1 {
		t.Fatalf("FEED DOWN was starved by a protection flood: %q", msgs)
	}
	if countContaining(msgs, "Trader x is DOWN") != 1 {
		t.Fatalf("TRADER DOWN was starved by a protection flood: %q", msgs)
	}
	if n := countContaining(msgs, "naked"); n > 3 {
		t.Fatalf("the flooding kind pushed %d times, want at most its budget", n)
	}
}

// (1c) A P0 the budget refused is not lost: the 60 s beat re-offers it once the
// budget reopens. MUTANT (shown RED): drop the ownerpush.Retry() call from
// monitorTick -> the held P0 is never delivered -> RED.
func TestDroppedOwnerPushIsRetriedByTheMonitorBeat(t *testing.T) {
	rec := &pushRec{}
	p := ownerpush.New(rec.send)
	clk := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	p.SetClockForTest(func() time.Time { return clk })
	t.Cleanup(ownerpush.Use(p))
	fixed := cmeOpenNow(t)
	oldClk := ownerPushClock
	ownerPushClock = func() time.Time { return fixed }
	t.Cleanup(func() { ownerPushClock = oldClk })

	at, _ := feedTrader(t)
	withProvider(t, []market.Kline{{OpenTime: fixed.UnixMilli() - 90_000}}) // live feed
	for i := 0; i < 4; i++ {                                                // 3 fit the kind's budget, the 4th is refused
		at.emitAlert("P0", "unprotected_position", fmt.Sprintf("unprotected:MNQ:LONG:%d", i), fmt.Sprintf("naked-%d", i), "body")
	}
	p.Flush()
	if got := len(rec.all()); got != 3 || p.Held() != 1 {
		t.Fatalf("setup: delivered %d held %d, want 3 / 1", got, p.Held())
	}
	clk = clk.Add(61 * time.Minute) // the hourly budget reopens
	at.monitorTick(fixed)           // the production beat
	p.Flush()
	if countContaining(rec.all(), "naked-3") != 1 {
		t.Fatalf("the refused P0 was never retried: %q", rec.all())
	}
}

// (3) The phone message says WHICH trader — by display name — although the real
// ids (65-74 chars) are redacted by the scrubber. The dead-man body quotes the id
// itself; it too must read as the name.
//
// MUTANT (shown RED): append at.id instead of ownerPushTraderLabel() and drop the
// ReplaceAll -> "(trader [redacted])" -> RED.
func TestOwnerPushNamesTheTraderNotTheRedactedID(t *testing.T) {
	rec, flush := installPush(t)
	at, _ := feedTrader(t)
	at.id = m438LongID
	at.name = "Mentor A"
	at.emitAlert("P0", "unprotected_position", "ev-1", "Open position is NOT protected", "Long MNQ ×1.")
	at.emitAlert("P0", "dead_man_reconcile_stuck", "ev-2", "Dead-man stuck",
		"trader "+m438LongID+": entries blocked while the reconcile is stuck")
	flush()
	msgs := rec.all()
	if len(msgs) != 2 {
		t.Fatalf("want 2 pushes, got %q", msgs)
	}
	for _, m := range msgs {
		if strings.Contains(m, "[redacted]") {
			t.Fatalf("the trader id was redacted instead of named: %q", m)
		}
		if !strings.Contains(m, "Mentor A") {
			t.Fatalf("the message does not say which trader: %q", m)
		}
	}
	// no name set: the short id, never the long one.
	at2, _ := feedTrader(t)
	at2.id = m438LongID
	at2.name = ""
	at2.emitAlert("P0", "protection_partial", "ev-3", "partial", "body")
	flush()
	last := rec.all()[len(rec.all())-1]
	if !strings.Contains(last, "(trader 8d5c8af5)") || strings.Contains(last, "[redacted]") {
		t.Fatalf("name-less trader should show the short id: %q", last)
	}
}

// (4) The unfilled-hole message stays TRUE after the slow retries are spent: the
// first message names the re-ask horizon and the restart that a late reload
// needs, and when the tracker reports SlowSpent a second, plain message says the
// reload now needs a bot restart.
//
// MUTANT (shown RED): drop the `if h.SlowSpent` follow-up -> no second push -> RED.
func TestUnfilledHoleOwnerMessageStaysTrueAfterTheCap(t *testing.T) {
	rec, flush := installPush(t)
	at, st := feedTrader(t)
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 90_000}})
	hole := ntTrader.UnfilledHole{Symbol: "MNQ", Contract: "MNQ 12-26", FromMs: now.UnixMilli() - 640*60_000, ToMs: now.UnixMilli(),
		Missing: 640, Attempts: 4, Window: "10-25 17:00–10-26 03:40 CT",
		ReaskEvery: 15 * time.Minute, ReaskHorizon: 2 * time.Hour}
	old := unfilledHolesFn
	t.Cleanup(func() { unfilledHolesFn = old })
	unfilledHolesFn = func(string, time.Time) []ntTrader.UnfilledHole { return []ntTrader.UnfilledHole{hole} }

	at.monitorTick(now)
	flush()
	if len(rec.all()) != 1 {
		t.Fatalf("before the cap: want exactly the first message, got %q", rec.all())
	}
	first := rec.all()[0]
	for _, want := range []string{"Reload All Historical Data", "about every 15 minutes", "for about 2 hours", "restart the bot"} {
		if !strings.Contains(first, want) {
			t.Fatalf("first message must say %q: %q", want, first)
		}
	}

	hole.SlowSpent = true // the tracker has stopped re-asking
	at.monitorTick(now.Add(time.Minute))
	at.monitorTick(now.Add(2 * time.Minute)) // repeats stay silent
	flush()
	msgs := rec.all()
	if len(msgs) != 2 {
		t.Fatalf("after the cap: want one follow-up, got %d: %q", len(msgs), msgs)
	}
	for _, want := range []string{"STOPPED re-asking", "THEN restart the bot"} {
		if !strings.Contains(msgs[1], want) {
			t.Fatalf("follow-up must say %q: %q", want, msgs[1])
		}
	}
	if got := feedAlerts(t, st, alertKindBarHoleUnfilled); got != 2 {
		t.Fatalf("alert rows = %d, want the first alert and the spent follow-up", got)
	}
}

// ── review round 2 (m438) ───────────────────────────────────────────────────

// (1d) A FEED DOWN the budget is holding is withdrawn when the feed returns: the
// owner must never be told the bot is blind AFTER the BACK message. Production
// path: monitorTick -> checkFeedDown -> emitAlert -> push (held: the feed-down
// kind budget is spent) ... monitorTick sees live bars -> noteFeedBack ->
// ownerPushFeedBack -> Cancel; the budget reopens; the beat's Retry runs.
//
// MUTANT (shown RED): comment out ownerpush.Cancel("feed-down|"+episodeID) in
// ownerPushFeedBack -> Retry delivers the stale "feed DOWN" after the BACK -> RED.
func TestHeldFeedDownIsWithdrawnWhenTheFeedComesBack(t *testing.T) {
	rec := &pushRec{}
	p := ownerpush.New(rec.send)
	clk := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	p.SetClockForTest(func() time.Time { return clk })
	t.Cleanup(ownerpush.Use(p))
	fixed := cmeOpenNow(t)
	oldClk := ownerPushClock
	ownerPushClock = func() time.Time { return fixed }
	t.Cleanup(func() { ownerPushClock = oldClk })

	// spend the feed-down kind's hourly budget with earlier episodes.
	for i := 0; i < 3; i++ {
		if !ownerpush.Push(fmt.Sprintf("feed-down|earlier-%d", i), fmt.Sprintf("earlier outage %d", i)) {
			t.Fatalf("setup: earlier feed-down push %d refused", i)
		}
	}
	p.Flush()

	at, _ := feedTrader(t)
	withProvider(t, []market.Kline{{OpenTime: fixed.UnixMilli() - 11*60_000 - 60_000}})
	at.monitorTick(fixed) // FEED DOWN: refused by the budget, HELD
	p.Flush()
	if p.Held() != 1 || countContaining(rec.all(), "NT8 bar feed DOWN") != 0 {
		t.Fatalf("setup: the FEED DOWN must be held, not delivered (held %d): %q", p.Held(), rec.all())
	}

	later := fixed.Add(3 * time.Minute)
	withProvider(t, []market.Kline{{OpenTime: later.UnixMilli() - 30_000 - 60_000}})
	at.monitorTick(later) // the feed is back -> BACK push, FEED DOWN withdrawn
	p.Flush()
	if countContaining(rec.all(), "BACK") != 1 {
		t.Fatalf("setup: want exactly one BACK message: %q", rec.all())
	}
	if p.Held() != 0 {
		t.Fatalf("the held FEED DOWN must be withdrawn by the BACK, still held: %d", p.Held())
	}

	clk = clk.Add(61 * time.Minute) // the hourly budget reopens
	at.monitorTick(later)           // the production beat runs Retry
	p.Flush()
	if got := countContaining(rec.all(), "NT8 bar feed DOWN"); got != 0 {
		t.Fatalf("a stale FEED DOWN was delivered after the BACK: %q", rec.all())
	}
}

// (1e) The place-episode map is swept when the position goes away. Production
// path: actOnProtection(place) stamps the episode; the next reconcile pass reads
// NO positions (flat by hand / the stop filled) and sweeps; the next naked spell
// on the same symbol+side is a NEW episode -> a SECOND P0 row and a SECOND push.
//
// MUTANT (shown RED): comment out sweep(&protectionPlaceEpisode) in
// sweepStaleProtectionState -> the old episode id is reused, the store dedupes it,
// the second naked spell writes no P0 row and sends no push -> RED.
func TestPlaceEpisodeIsSweptWhenThePositionCloses(t *testing.T) {
	rec, flush := installPush(t)
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	at.id = "m438-place-sweep"
	at.exchange = "ninjatrader"
	at.trader = &MockTrader{positions: []map[string]interface{}{}}
	place := protectionVerdict{Action: protectionPlace, StopPx: 31163, NeedQty: 1, Source: "plan", Why: "no stop at the broker"}

	t0 := time.Now()
	at.actOnProtection(place, "MNQ", "LONG", 1, "sig", "t", 0, t0)
	// the position is gone; the monitor beat reads the empty book and sweeps.
	at.reconcileProtectionAt(t0.Add(time.Minute), "monitor")
	if _, ok := protectionPlaceEpisode.Load(at.id + "|MNQ|LONG"); ok {
		t.Fatal("the place episode survived a pass that read no position")
	}
	// a NEW naked position on the same symbol and side, a minute later.
	at.actOnProtection(place, "MNQ", "LONG", 1, "sig2", "t", 0, t0.Add(2*time.Minute))
	flush()
	if n := unprotectedRows(t, st, at.id); n != 2 {
		t.Fatalf("a new naked position after a closed one must raise a second P0 row, rows = %d", n)
	}
	if n := countContaining(rec.all(), "Open position had NO stop"); n != 2 {
		t.Fatalf("a new naked position after a closed one must push the owner again, pushes = %d: %q", n, rec.all())
	}
}
