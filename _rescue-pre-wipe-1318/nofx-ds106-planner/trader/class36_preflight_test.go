package trader

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
)

// CLASS 36 (2026-09-01) — a preflight that refuses SCHEDULED work because the
// market is closed, when the scheduled work exists precisely to run while the
// market is closed. Fixtures: the scheduled reads bypass the freshness check
// ONLY in a halt/weekend; every other trigger class keeps it; every refusal is
// a ⛔ line; the executor's halt block is untouched.

// warnPlusCapture attaches a WARN+ sink (the repo's only log hook; INFO stays
// journal-only by design) and returns a getter over the captured messages.
func warnPlusCapture(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	logger.AttachDBSink(func(_ int64, level, _, _, message, _ string) {
		mu.Lock()
		lines = append(lines, level+" "+message)
		mu.Unlock()
	})
	t.Cleanup(func() { logger.AttachDBSink(func(int64, string, string, string, string, string) {}) })
	return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), lines...) }
}

func hasLine(lines []string, subs ...string) bool {
	for _, l := range lines {
		ok := true
		for _, s := range subs {
			if !strings.Contains(l, s) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, d time.Duration, pred func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if pred() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return pred()
}

// ── D5 line builders + class table ──────────────────────────────────────────

func TestClass36LineBuilders(t *testing.T) {
	b := preflightBypassLine("ASIA", "2026-09-01", "", "2026-09-01 15:59 CT", 31)
	for _, want := range []string{"🗓 preflight bypass (class 36)", "scheduled session_read read ASIA 2026-09-01", "halt/weekend", "freshness check skipped", "newest 1m 2026-09-01 15:59 CT", "age 31m"} {
		if !strings.Contains(b, want) {
			t.Errorf("bypass line %q missing %q", b, want)
		}
	}
	r := preflightRefusalLine("ASIA", "2026-09-01", "death_replan", "stale_bars_1800s")
	if !strings.HasPrefix(r, "⛔ planner preflight refused death_replan: stale_bars_1800s") {
		t.Errorf("refusal line must lead with the class and reason, got %q", r)
	}
	if !strings.Contains(preflightRefusalLine("ASIA", "d", "", "no_bars"), "refused session_read: no_bars") {
		t.Error("the scheduled read's class must be named session_read")
	}
	if !strings.Contains(PreflightBootLine(), "class 36") || !strings.Contains(PreflightBootLine(), "executor halt-block unchanged") {
		t.Errorf("boot line %q", PreflightBootLine())
	}
}

func TestClass36TriggerClassTable(t *testing.T) {
	halt := ctTime(t, 2026, 9, 1, 16, 30)   // Tuesday halt
	sunday := ctTime(t, 2026, 9, 6, 16, 30) // weekend
	open := ctTime(t, 2026, 9, 1, 10, 0)    // live tape
	if kernel.IsCMEOpen(halt) || kernel.IsCMEOpen(sunday) || !kernel.IsCMEOpen(open) {
		t.Fatal("fixture: calendar")
	}
	for _, tc := range []struct {
		trigger   string
		scheduled bool
	}{
		{"", true}, {"ASIA_scheduled_read", true}, {"weekly", true}, {"sunday_weekly_read", true},
		{"level_event", false}, {"structure_mss", false}, {"death_replan", false}, {"owner_reread", false}, {"owner_reset", false},
	} {
		if got := preflightIsScheduled(tc.trigger); got != tc.scheduled {
			t.Errorf("preflightIsScheduled(%q) = %v, want %v", tc.trigger, got, tc.scheduled)
		}
		// bypass = scheduled AND closed; NEVER on a live tape
		if got := preflightScheduledBypass(tc.trigger, halt); got != tc.scheduled {
			t.Errorf("halt: bypass(%q) = %v, want %v", tc.trigger, got, tc.scheduled)
		}
		if got := preflightScheduledBypass(tc.trigger, sunday); got != tc.scheduled {
			t.Errorf("sunday: bypass(%q) = %v, want %v", tc.trigger, got, tc.scheduled)
		}
		if preflightScheduledBypass(tc.trigger, open) {
			t.Errorf("open tape: bypass(%q) must be false — a silent feed during open hours is the 08-19 outage class", tc.trigger)
		}
	}
}

// ── E6: every refusal, for any reason, emits the ⛔ line ─────────────────────

func TestClass36EveryRefusalEmitsTheLine(t *testing.T) {
	get := warnPlusCapture(t)
	at, _ := feedTrader(t)
	now := ctTime(t, 2026, 9, 1, 16, 30)
	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = nil })

	withProvider(t, nil) // wired, empty
	if at.plannerPreflight("ASIA", "2026-09-01", "level_event") {
		t.Fatal("empty cache must refuse")
	}
	if !hasLine(get(), "⛔ planner preflight refused level_event: no_bars") {
		t.Fatalf("no ⛔ line for no_bars, got %v", get())
	}
	// stale + non-scheduled class in the halt → refused, named
	withProvider(t, class36HaltBars(ctTime(t, 2026, 9, 1, 15, 59))(("MNQ"), "1m", 0))
	if at.plannerPreflight("ASIA", "2026-09-01", "death_replan") {
		t.Fatal("death_replan must keep the freshness check")
	}
	if !hasLine(get(), "⛔ planner preflight refused death_replan: stale_bars_") {
		t.Fatalf("no ⛔ line for the death_replan refusal, got %v", get())
	}
	// scheduled class with a STALE tape while the market is OPEN → still refused
	openNow := ctTime(t, 2026, 9, 1, 10, 0)
	testNow = func() time.Time { return openNow }
	withProvider(t, class36HaltBars(ctTime(t, 2026, 9, 1, 9, 0))("MNQ", "1m", 0)) // newest 09:00 → 60m stale at 10:00
	if at.plannerPreflight("NY", "2026-09-01", "") {
		t.Fatal("a scheduled read into a silent OPEN tape must refuse (feed-down class)")
	}
	if !hasLine(get(), "⛔ planner preflight refused session_read: stale_bars_") {
		t.Fatalf("no ⛔ line for the open-tape refusal, got %v", get())
	}
	// scheduled class, stale, market CLOSED → bypass, loud
	testNow = func() time.Time { return now }
	withProvider(t, class36HaltBars(ctTime(t, 2026, 9, 1, 15, 59))("MNQ", "1m", 0))
	if !at.plannerPreflight("ASIA", "2026-09-01", "") {
		t.Fatal("the scheduled read must pass in the halt")
	}
	if !hasLine(get(), "🗓 preflight bypass (class 36): scheduled session_read read ASIA 2026-09-01") {
		t.Fatalf("no bypass line, got %v", get())
	}
}

// ── E5: death_replan and owner_reread halt behavior — pinned UNCHANGED ──────

func TestClass36DeathReplanAndOwnerRereadInHaltUnchanged(t *testing.T) {
	get := warnPlusCapture(t)
	at, st := class35Trader(t, 4)
	prev := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	now := ctTime(t, 2026, 9, 1, 16, 30)
	market.FuturesBarsProvider = class36HaltBars(ctTime(t, 2026, 9, 1, 15, 59))
	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = nil })
	seedV1(t, st, "2026-09-01", "ASIA")
	row := latestRow(t, st, "2026-09-01", "ASIA")

	// death_replan: today it is refused by the freshness check in a halt (no
	// row, no spend) and retried next cycle — kept exactly.
	at.runDeathReplan("ASIA", "2026-09-01", row, "all levels consumed")
	if fresh := latestRow(t, st, "2026-09-01", "ASIA"); fresh.Version != 1 {
		t.Fatalf("death_replan in a halt must write nothing (unchanged), got %+v", fresh)
	}
	if !hasLine(get(), "⛔ planner preflight refused death_replan: stale_bars_") {
		t.Fatalf("death_replan refusal must be the ⛔ line, got %v", get())
	}
	if b := store.GetReplanBudget(st, "trader-1", "2026-09-01", "ASIA", 4); b.Used != 0 {
		t.Fatalf("a refused death re-plan must not spend: %+v", b)
	}
	// owner_reread: refused BEFORE the preflight — at 16:30 no session is
	// active yet (ASIA opens 17:00), so CanForceReread refuses on "no session
	// is active"; the market-closed gate (auto_trader_reread.go:52,
	// !kernel.IsCMEOpen) is the next layer. Kept exactly: a re-read never
	// authors in a halt.
	gate := at.CanForceReread(now)
	if gate.Allowed || !strings.Contains(gate.Reason, "no session is active") {
		t.Fatalf("owner re-read in a halt must be refused (unchanged), got %+v", gate)
	}
}

// ── E4: level_event (and fast-market, which rides it) cannot fire in a halt ──

func TestClass36LevelEventDoesNotFireInHalt(t *testing.T) {
	at, st := asiaClockTrader(t)
	prev := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	now := ctTime(t, 2026, 9, 1, 16, 30)
	market.FuturesBarsProvider = class36HaltBars(ctTime(t, 2026, 9, 1, 15, 59)) // frozen: no bar after the plan's birth
	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = nil })
	if _, err := st.Plan().AppendPlan(&store.PlanDB{
		PlanID: store.MakePlanID("2026-09-01", "ASIA"), StrategyID: "t1", TradeDate: "2026-09-01", Session: "ASIA",
		TriggerReason: "ASIA_scheduled_read", Lifecycle: "active", Doc: validTraderPlanJSON,
	}); err != nil {
		t.Fatal(err)
	}
	row, _ := st.Plan().GetLatestPlanForTraderSession("2026-09-01", "ASIA", "t1")
	at.maybeWakePlannerOnLevelEvents("ASIA", "2026-09-01", row)
	time.Sleep(200 * time.Millisecond)
	if !at.lastPlannerWakeAt.IsZero() {
		t.Fatal("a frozen halt tape has no new level events — no wake may fire")
	}
	if fresh, _ := st.Plan().GetLatestPlanForTraderSession("2026-09-01", "ASIA", "t1"); fresh.Version != 1 {
		t.Fatalf("no wake read may land in a halt, got %+v", fresh)
	}
}

// ── E3: the executor never trades in a halt — regression pin, untouched ──────

func TestClass36ExecutorHaltBlockUnchanged(t *testing.T) {
	for _, tm := range []time.Time{ctTime(t, 2026, 9, 1, 16, 30), ctTime(t, 2026, 9, 6, 16, 30), ctTime(t, 2026, 9, 5, 12, 0)} {
		if kernel.IsCMEOpen(tm) {
			t.Errorf("IsCMEOpen(%s) must be false — cmeSessionClosedSkip idles the whole cycle on it", tm)
		}
	}
	if !kernel.IsCMEOpen(ctTime(t, 2026, 9, 6, 17, 0)) {
		t.Error("Sunday 17:00 CT is the open")
	}
	reg := kernel.DefaultSessionRegistry()
	if reason, blocked := sessionGateDecision(reg, ctAt(t, 16, 30), nil, nil); !blocked {
		t.Errorf("16:30 must be blocked for entries (reason %q)", reason)
	}
}

// ── E7: idempotence — no new bars for 20 minutes → the read fires ONCE ───────

func TestClass36ScheduledReadFiresOnceInHalt(t *testing.T) {
	at, st := asiaClockTrader(t)
	prev := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	market.FuturesBarsProvider = class36HaltBars(ctTime(t, 2026, 9, 1, 15, 59))
	cur := ctTime(t, 2026, 9, 1, 16, 30)
	testNow = func() time.Time { return cur }
	t.Cleanup(func() { testNow = nil })

	at.evaluateWallClockSessionReads()
	defer drainReReads(t) // CTO M4: join the async re-read before the seam resets
	if row := waitPlan(t, st, "2026-09-01", "ASIA", "t1"); row == nil {
		t.Fatal("first evaluation must land the plan")
	}
	drainReReads(t) // the loop below moves `cur`, which the read's clock seam reads
	for _, mm := range []int{32, 40, 50} {
		cur = ctTime(t, 2026, 9, 1, 16, mm)
		if fired := at.maybeRunSessionReadsAt(cur); len(fired) != 0 {
			t.Fatalf("16:%02d: the read must not fire again (plan-store dedupe), fired=%+v", mm, fired)
		}
	}
	time.Sleep(200 * time.Millisecond)
	if row, _ := st.Plan().GetLatestPlanForTraderSession("2026-09-01", "ASIA", "t1"); row.Version != 1 {
		t.Fatalf("exactly one version expected, got v%d", row.Version)
	}
}

// ── E8: LONDON 01:30 and NY 08:00 unchanged with live bars ───────────────────

func TestClass36LondonAndNYUnchangedWithLiveBars(t *testing.T) {
	get := warnPlusCapture(t)
	for _, tc := range []struct {
		session string
		hh, mm  int
	}{{"LONDON", 1, 30}, {"NY", 8, 0}} {
		st, err := store.New(t.TempDir() + "/" + tc.session + ".db")
		if err != nil {
			t.Fatal(err)
		}
		enabled := true
		at := &AutoTrader{id: "t1", exchange: "ninjatrader", store: st, config: AutoTraderConfig{NinjaTraderSymbol: "MNQ", StrategyConfig: &store.StrategyConfig{
			DayPlan: &store.DayPlanConfig{PlanEnabled: true, Sessions: []store.DayPlanSessionOverride{{Session: tc.session, Enable: &enabled}}},
		}}}
		at.mcpClient = &planClient{}
		now := ctTime(t, 2026, 9, 1, tc.hh, tc.mm)
		if !kernel.IsCMEOpen(now) {
			t.Fatalf("fixture: %s read time must be a live tape", tc.session)
		}
		prev := market.FuturesBarsProvider
		market.FuturesBarsProvider = class36HaltBars(now.Add(-time.Minute)) // newest bar 1m old = fresh
		testNow = func() time.Time { return now }
		fired := at.maybeRunSessionReadsAt(now)
		if len(fired) != 1 || fired[0].Session != tc.session {
			t.Fatalf("%s: fired=%+v", tc.session, fired)
		}
		if row := waitPlan(t, st, "2026-09-01", tc.session, "t1"); row == nil || row.TriggerReason != tc.session+"_scheduled_read" {
			t.Fatalf("%s: read must land as before, got %+v", tc.session, row)
		}
		drainReReads(t) // CTO M4: join the async read before the seams below reset
		market.FuturesBarsProvider = prev
		testNow = nil
		_ = st.Close()
	}
	if hasLine(get(), "preflight bypass (class 36)") {
		t.Fatal("live-tape reads must not take the halt bypass")
	}
}

// ── E2: Sunday 16:30 — weekly on the wall-clock path, then ASIA follows ──────

// sundayClient answers the weekly read with a validator-legal doc and every
// other read with the canned valid plan.
type sundayClient struct {
	fakeDecisionClient
	weeklyJSON string
}

func (c *sundayClient) CallWithMessages(sys, user string) (string, error) {
	if sys == weeklySystemPrompt {
		return c.weeklyJSON, nil
	}
	return mapCompliantPlanJSON(user), nil // W2 A4: a compliant model reads the MAP block
}

func TestClass36PinSundayWeekly(t *testing.T) {
	get := warnPlusCapture(t)
	at, st := asiaClockTrader(t)
	prev := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	// A Sunday in the real PAST (the F6 clock-hold treats bars stamped in the
	// future as a broken host clock and defers authoring — correct, and not
	// what this fixture tests).
	now := ctTime(t, 2026, 8, 30, 16, 30) // Sunday 16:30 CT — market closed
	if kernel.IsCMEOpen(now) {
		t.Fatal("fixture: Sunday 16:30 must be closed")
	}
	lastBar := ctTime(t, 2026, 8, 28, 15, 59) // Friday's last bar → ~2 days old on Sunday
	market.FuturesBarsProvider = class36HaltBars(lastBar)
	// The weekly read authors from STORED 1m bars: seed two sessions' worth.
	var rows []store.BarHistoryDB
	for i := 0; i < 2*390; i++ {
		o := lastBar.Add(-time.Duration(2*390-1-i) * time.Minute).UnixMilli()
		rows = append(rows, store.BarHistoryDB{Contract: "MNQ 09-26", Source: store.BarSourceLive, Symbol: "MNQ", TF: "1m", OpenTimeMs: o, O: 15600 + float64(i%10), H: 15650 + float64(i%10), L: 15550 + float64(i%10), C: 15600 + float64(i%10), V: 100})
	}
	if err := st.BarHistory().Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := st.BarHistory().InsertBars(rows); err != nil {
		t.Fatal(err)
	}
	bars1m := at.weeklyBars1m(now)
	if len(bars1m) == 0 {
		t.Fatal("fixture: stored bars must load")
	}
	price := bars1m[len(bars1m)-1].Close
	facts := kernel.ComputeWeeklyFacts(bars1m, now, price)
	refs := kernel.WeeklyRefSet(facts)
	if len(refs) == 0 {
		t.Fatal("fixture: weekly refs")
	}
	wj, _ := json.Marshal(kernel.WeeklyDoc{WeeklyLevels: []kernel.WeeklyLevel{{Name: "PWL", Px: refs[0]}}, Narrative: "range"})
	at.mcpClient = &sundayClient{weeklyJSON: string(wj)}
	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = nil })
	monday := kernel.WeekGoverningMonday(now).Format("2006-01-02")

	// One tick at 16:30: weekly first, then the session reads — ASIA defers.
	at.evaluateWallClockWeeklyRead()
	at.evaluateWallClockSessionReads()
	if !waitFor(t, 5*time.Second, func() bool {
		r, _ := st.Plan().GetLatestPlanForTraderSession(monday, "WEEKLY", "t1")
		return r != nil
	}) {
		t.Fatalf("the weekly read must land on the wall-clock path; log: %v", get())
	}
	reg := at.sessionRegistry(now)
	var asia *kernel.SessionDef
	for i := range reg.Sessions {
		if reg.Sessions[i].Name == "ASIA" {
			asia = &reg.Sessions[i]
		}
	}
	asiaDate := sessionChainDate(asia, now)
	if r, _ := st.Plan().GetLatestPlanForTraderSession(asiaDate, "ASIA", "t1"); r != nil {
		t.Fatalf("ASIA must DEFER until the weekly doc exists, got %+v", r)
	}
	// Next tick: the weekly doc exists → ASIA fires and authors from stored bars (weekend bypass).
	at.evaluateWallClockSessionReads()
	defer drainReReads(t) // CTO M4: join the async re-read before the seam resets
	if row := waitPlan(t, st, asiaDate, "ASIA", "t1"); row == nil || row.Lifecycle != "active" {
		t.Fatalf("ASIA read must follow the weekly doc; log: %v", get())
	}
	if !hasLine(get(), "🗓 preflight bypass (class 36): scheduled session_read read ASIA") {
		t.Fatalf("the Sunday ASIA read must take the loud weekend bypass, got %v", get())
	}
}
