package trader

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/internal/ownerpush"
	"vl/market"
	"vl/store"
)

// m471 L3-3 — a P0 held across a restart that belongs to an episode the
// PREVIOUS process was tracking (here FEED DOWN) is not delivered as if
// current. The new process rebuilds the feed episode empty, so noteFeedBackAt
// returns early (no episode to end) and no BACK ever follows: before the fix
// the restored FEED DOWN went out after the restart and was the owner's last
// word while the bot traded. Money P0s (daily-loss) are still delivered. A feed
// that is STILL down after the restart is raised again by the new process.
//
// Production seams: process A = checkFeedDown / emitAlert -> maybeOwnerPush ->
// a pusher whose sender fails (the box cannot reach Telegram), persisted by
// EnableState; process B = ownerpush.RestoreAtBoot (what Start runs at boot)
// + BootMessage + the new trader's checkFeedDown.
//
// MUTANT (shown RED): supersededLabel returns "" for every key (RestoreAtBoot
// withdraws nothing) -> "the restored FEED DOWN was delivered after the
// restart" -> RED. MUTANT 2: drop the firstSight re-offer in checkFeedDown ->
// "a feed still down after the restart must reach the owner" -> RED.

type m471Rec struct {
	mu  sync.Mutex
	got []string
}

func (r *m471Rec) send(s string) error {
	r.mu.Lock()
	r.got = append(r.got, s)
	r.mu.Unlock()
	return nil
}
func (r *m471Rec) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.got...)
}

// m471ProcessA runs the previous process: the feed goes down and the daily loss
// trips while Telegram is unreachable; both P0s are held and persisted.
func m471ProcessA(t *testing.T, statePath string, st *store.Store, now time.Time) {
	t.Helper()
	a := ownerpush.NewE(func(string) error { return errors.New("dial tcp: lookup api.telegram.org: no such host") })
	a.SetClockForTest(func() time.Time { return now })
	a.EnableState(statePath)
	restore := ownerpush.Use(a)
	defer restore()
	at := &AutoTrader{id: "t1", name: "hoang", store: st, exchange: "ninjatrader", startTime: now.Add(-time.Hour)}
	at.config.Exchange = "ninjatrader"
	at.config.StrategyConfig = &store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: false}}
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 11*60_000 - 60_000}})
	at.checkFeedDown(now)
	at.emitAlert("P0", "daily-loss", "daily-loss:2026-08-17", "🔴 Daily loss limit hit", "loss -500.00 <= limit -500.00")
	a.Flush()
	if a.Held() != 2 {
		t.Fatalf("process A should hold FEED DOWN + daily-loss, held=%d", a.Held())
	}
	a.CloseState()
}

func m471ProcessB(t *testing.T, statePath string, st *store.Store, now time.Time) (*AutoTrader, *ownerpush.Pusher, *m471Rec, ownerpush.BootInfo) {
	t.Helper()
	rec := &m471Rec{}
	b := ownerpush.NewE(rec.send)
	b.SetClockForTest(func() time.Time { return now })
	info := b.RestoreAtBoot(statePath)
	t.Cleanup(ownerpush.Use(b))
	t.Cleanup(b.CloseState)
	at := &AutoTrader{id: "t1", name: "hoang", store: st, exchange: "ninjatrader", startTime: now.Add(-time.Hour)}
	at.config.Exchange = "ninjatrader"
	at.config.StrategyConfig = &store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: false}}
	return at, b, rec, info
}

func m471FeedStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "m471.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestRestoredFeedDownIsSupersededAtBootNotDeliveredAsCurrent(t *testing.T) {
	now := cmeOpenNow(t)
	oldClk := ownerPushClock
	ownerPushClock = func() time.Time { return now }
	t.Cleanup(func() { ownerPushClock = oldClk })
	statePath := filepath.Join(t.TempDir(), "data.db.ownerpush.json")
	st := m471FeedStore(t)
	m471ProcessA(t, statePath, st, now)

	// restart 5 minutes later: Telegram reachable, the feed is back
	later := now.Add(5 * time.Minute)
	at, b, rec, info := m471ProcessB(t, statePath, st, later)
	if info.Restored != 1 || len(info.Superseded) != 1 || !strings.HasPrefix(info.Superseded[0], "FEED DOWN") {
		t.Errorf("boot info: want 1 restored (daily-loss) + FEED DOWN superseded, got %+v", info)
	}
	bootKey, bootText := ownerpush.BootMessage("abc123", info, false, later)
	if !strings.Contains(bootText, "FEED DOWN") || !strings.Contains(bootText, "superseded by this restart") {
		t.Errorf("the boot message must name the superseded FEED DOWN: %q", bootText)
	}
	b.Push(bootKey, bootText)
	withProvider(t, []market.Kline{{OpenTime: later.UnixMilli() - 30_000}})
	at.checkFeedDown(later)
	b.Flush()
	b.Retry()
	b.Flush()

	got := rec.all()
	if c := countContaining(got, "NT8 bar feed DOWN"); c != 0 {
		t.Errorf("the restored FEED DOWN was delivered after the restart and no BACK follows - the owner's last word says the bot is blind: %q", got)
	}
	if c := countContaining(got, "Daily loss limit hit"); c != 1 {
		t.Errorf("a money P0 (daily-loss) held across the restart must still be delivered once, got %d: %q", c, got)
	}
}

func TestFeedStillDownAfterRestartIsRaisedAgain(t *testing.T) {
	now := cmeOpenNow(t)
	oldClk := ownerPushClock
	ownerPushClock = func() time.Time { return now }
	t.Cleanup(func() { ownerPushClock = oldClk })
	statePath := filepath.Join(t.TempDir(), "data.db.ownerpush.json")
	st := m471FeedStore(t)
	m471ProcessA(t, statePath, st, now)

	// restart 1 minute later: Telegram reachable, the feed is STILL down (the
	// newest bar is the same one, so the episode id and its alert row are the same)
	later := now.Add(time.Minute)
	at, b, rec, _ := m471ProcessB(t, statePath, st, later)
	at.checkFeedDown(later)
	at.checkFeedDown(later.Add(time.Minute))
	b.Flush()
	b.Retry()
	b.Flush()
	if c := countContaining(rec.all(), "NT8 bar feed DOWN"); c != 1 {
		t.Fatalf("a feed still down after the restart must reach the owner exactly once, got %d: %q", c, rec.all())
	}
}
