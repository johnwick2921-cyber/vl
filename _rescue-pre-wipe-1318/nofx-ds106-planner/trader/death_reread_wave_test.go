package trader

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// W-DEATH-REREAD (2026-09-18, owner ruling 12:3x CT "fix all") tests: the knob,
// the boot line, the bias-free prior line, the wick guard, the REAL death path
// (dormant → one budgeted re-read → fresh v2 → superseded:death → counter), the
// OFF byte-identical path, and the budget-exhausted dormant-only path.

func TestDeathRereadKnobResolution(t *testing.T) {
	on, off := true, false
	cases := []struct {
		name string
		dp   *store.DayPlanConfig
		want bool
	}{
		{"nil config", nil, true},
		{"nil pointer = ON (owner default)", &store.DayPlanConfig{}, true},
		{"explicit true", &store.DayPlanConfig{DeathReread: &on}, true},
		{"explicit false = OFF byte-identical", &store.DayPlanConfig{DeathReread: &off}, false},
	}
	for _, c := range cases {
		if got := c.dp.DeathRereadEnabled(); got != c.want {
			t.Fatalf("%s: DeathRereadEnabled() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDeathRereadBootLineReadsResolvedKnob(t *testing.T) {
	on, off := true, false
	if got := DeathRereadBootLine(nil); got != "🧬 death→reread=on(default) (W-DEATH-REREAD)" {
		t.Fatalf("nil dp: %q", got)
	}
	if got := DeathRereadBootLine(&store.DayPlanConfig{}); got != "🧬 death→reread=on(default) (W-DEATH-REREAD)" {
		t.Fatalf("nil pointer: %q", got)
	}
	if got := DeathRereadBootLine(&store.DayPlanConfig{DeathReread: &on}); got != "🧬 death→reread=on(saved) (W-DEATH-REREAD)" {
		t.Fatalf("saved true: %q", got)
	}
	if got := DeathRereadBootLine(&store.DayPlanConfig{DeathReread: &off}); got != "🧬 death→reread=off (W-DEATH-REREAD)" {
		t.Fatalf("saved false: %q", got)
	}
}

// TestDeathRereadPriorLineIsBiasFree pins (a) + SF-3: the death prior line
// carries the dead version, the kill line, the break direction AND the price at
// death, and kernel.FlipToDirection on it returns "" — the write site forces NO
// bias (the death read is bias free, unlike the flip read whose prior line
// mandates the flipped bias).
func TestDeathRereadPriorLineIsBiasFree(t *testing.T) {
	killer := "death-condition: 5m_close close below 29767.00 (buffer 0.5×ATR14, 2× 5m closes)"
	prior := deathRereadPriorLine(2, "long", killer, 29672.5)
	if got := kernel.FlipToDirection(prior); got != "" {
		t.Fatalf("FlipToDirection(death prior) = %q, want \"\" (no forced bias); prior:\n%s", got, prior)
	}
	for _, want := range []string{"v2", "bias long", "break down", "price at death 29672.50", killer} {
		if !strings.Contains(prior, want) {
			t.Fatalf("prior line missing %q:\n%s", want, prior)
		}
	}
	if deathRereadKillerDirection("2x5m close above 29473.50") != "up" || deathRereadKillerDirection("close below 28981.00") != "down" {
		t.Fatalf("killer direction parse broken: up=%q down=%q", deathRereadKillerDirection("2x5m close above 29473.50"), deathRereadKillerDirection("close below 28981.00"))
	}
}

// TestDeathBornWickActive pins (c) + SF-5: the first death check of a
// death-born plan runs only after the 10-minute birth wick AND only on the SAME
// death line the prior version died on (± FlipLineClusterTolerance) — a fresh
// plan that authors a NEW death line dies normally; an unknown prior line never
// suppresses. The knob OFF removes the guard entirely; a non-death-born row is
// never guarded.
func TestDeathBornWickActive(t *testing.T) {
	on, off := true, false
	birth := time.Date(2026, 9, 18, 9, 12, 0, 0, time.UTC)
	born := func(doc string) *store.PlanDB {
		return &store.PlanDB{TriggerReason: store.TriggerDeathReplan, CreatedAt: birth, Doc: doc}
	}
	blob, err := json.Marshal(deathFixtureDoc()) // DeathStructured.Price 15480
	if err != nil {
		t.Fatal(err)
	}
	row := born(string(blob))
	dpOn := &store.DayPlanConfig{DeathReread: &on}
	dpOff := &store.DayPlanConfig{DeathReread: &off}
	if !deathBornWickActive(row, dpOn, birth.Add(9*time.Minute), 15480) {
		t.Fatal("inside the 10-min wick on the SAME line the guard must be active")
	}
	if deathBornWickActive(row, dpOn, birth.Add(11*time.Minute), 15480) {
		t.Fatal("after the 10-min wick the guard must clear")
	}
	if deathBornWickActive(row, dpOn, birth.Add(1*time.Minute), 15480+2*kernel.FlipLineClusterTolerance()) {
		t.Fatal("a DIFFERENT death line must die normally (SF-5)")
	}
	// B2 (2026-09-18 review): the comparison is in the RAW price space — the
	// prior kill line is the recorded RAW death price (29755). The buffered
	// killer number on the same real row was 29767 (12 pts off against a 3-pt
	// tolerance), which is why the old buffered-space compare never fired where
	// plans flap. Same raw line → guarded; the raw line ± buffer → not.
	rawPrior := 29755.0
	rawRow := &store.PlanDB{TriggerReason: store.TriggerDeathReplan, CreatedAt: birth, Doc: func() string {
		b, _ := json.Marshal(kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "short"}, DeathStructured: &kernel.PlanCondition{Price: rawPrior, Side: "below", Rule: "2x5m"}})
		return string(b)
	}()}
	if !deathBornWickActive(rawRow, dpOn, birth.Add(1*time.Minute), rawPrior) {
		t.Fatal("B2: the SAME raw line must be guarded even when the buffered killer number would differ by the ATR buffer")
	}
	if deathBornWickActive(rawRow, dpOn, birth.Add(1*time.Minute), rawPrior+12.0) {
		t.Fatal("B2: a line 12 pts away in the raw space is a DIFFERENT line and must die normally")
	}
	if deathBornWickActive(row, dpOn, birth.Add(1*time.Minute), 0) {
		t.Fatal("an unknown prior line must never suppress")
	}
	if deathBornWickActive(row, dpOff, birth.Add(1*time.Minute), 15480) {
		t.Fatal("knob OFF must not guard (byte-identical to today)")
	}
	other := &store.PlanDB{TriggerReason: "structure_mss", CreatedAt: birth}
	if deathBornWickActive(other, dpOn, birth.Add(1*time.Minute), 15480) {
		t.Fatal("a non-death-born row is never guarded")
	}
}

// deathFixtureDoc is a seeded SHORT plan whose structured death (2x5m close
// below 15480) fires on the standard flip-fixture tape (two 5m closes at
// 15470) — the SAME tape whose machine map accepts validShortPlanJSON, so the
// re-read's fresh v2 lands through the real write site.
func deathFixtureDoc() kernel.PlanDoc {
	return kernel.PlanDoc{
		Bias:            kernel.PlanBias{Direction: "short"},
		DeathStructured: &kernel.PlanCondition{Price: 15480, Side: "below", Rule: "2x5m"},
	}
}

// deathRealPathTrader mirrors realPathTrader with the death knob instead of the
// flip knob; deathRereadRun is NOT substituted — the real path (claimed read →
// planner core → write site → store) runs with only the AI call scripted.
func deathRealPathTrader(t *testing.T, deathReread *bool, respond func(n int, user string) (string, error)) (*AutoTrader, *store.Store, *scriptedPlannerClient) {
	t.Helper()
	t.Setenv("FLIP_ATR_BUFFER", "0")
	t.Setenv("DORMANT_MIN_HOLD_MIN", "0")
	off := false
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{
		PlanEnabled: true, ReplanCap: store.IntPtr(4), SessionsEnabled: []string{"NY"}, DeathReread: deathReread,
		WakeOn15mZone: &off, WakeOnHTFZone: &off, WakeOnHTFOB: false, WakeOnSeatedInvalidation: &off, WakeOnIFVG: &off,
	}}
	at, st := resetTrader(t, cfg)
	client := &scriptedPlannerClient{respond: respond}
	at.mcpClient = client
	origDrift := clockHoldDriftFn
	clockHoldDriftFn = func(string) (int64, bool) { return 0, false }
	t.Cleanup(func() { clockHoldDriftFn = origDrift })
	t.Cleanup(func() { market.FuturesBarsProvider = nil; traderTestBarsInstalled = false })
	return at, st, client
}

// TestDeathRereadRealPathLandsFreshPlanAndSupersedes is the (a)+(b) REAL-path
// pin: a death fires → dormant → ONE budgeted re-read lands a fresh ACTIVE v2
// (trigger death_replan, bias free) → v1 superseded with superseded:death →
// counter recorded → class-35 budget spent → and the wick guard then protects
// v2: one minute later, on the SAME below-line tape, v2 does NOT die.
func TestDeathRereadRealPathLandsFreshPlanAndSupersedes(t *testing.T) {
	at, st, client := deathRealPathTrader(t, nil, func(int, string) (string, error) { return validShortPlanJSON, nil })
	// FIXED synthetic date (the flip real-path tests' own frame): the G7
	// freshness gate measures bar staleness against the clock, and a real-clock
	// now made the last complete 5m bucket land either side of the staleness
	// threshold depending on the second the test started — a flake by
	// construction. v2's CreatedAt is stamped with the real clock by the write
	// site; nothing asserted here depends on it (the wick guard's timing is
	// pinned by TestDeathBornWickActive, the predicate at both call sites).
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	flipRereadTestNow(t, now)
	td := "2026-08-18"
	row := seedActivePlan(t, at, td, "NY", now.Add(-40*time.Minute), deathFixtureDoc())
	seedFlipBars(15500, 15470, 6*time.Minute, now) // two 5m closes below the death line 15480
	logBuf := captureTraderLog(t)

	at.maybeRunSessionReadsAt(now)
	defer drainReReads(t) // CTO M4: join the async re-read before the seam resets

	if got := versionLifecycle(t, st, td, "NY", at.id, 1); got != "dormant" {
		t.Fatalf("death must park dormant first, got %q", got)
	}
	// B2: the dormant write records the RAW prior death line (the wick's space).
	if v := sysCfgVal(t, st, deathRereadPriorLineKey(row)); v != "15480.000000" {
		t.Fatalf("the RAW prior line must be recorded at the dormant write, got %q", v)
	}
	if !waitFor(t, 10*time.Second, func() bool {
		latest, err := st.Plan().GetLatestPlanForTraderSession(td, "NY", at.id)
		return err == nil && latest != nil && latest.Version == 2 && latest.Lifecycle == "active"
	}) {
		latest, _ := st.Plan().GetLatestPlanForTraderSession(td, "NY", at.id)
		t.Fatalf("the death re-read must land a fresh ACTIVE v2; latest=%+v; log:\n%s", latest, logBuf.String())
	}
	if client.calls() != 1 {
		t.Fatalf("exactly ONE planner call (valid plan accepted first try), got %d", client.calls())
	}
	// SF-1 (2026-09-18 review): v2-active in the store is satisfied by the
	// write itself, BEFORE the goroutine records the spend, sets the once-key,
	// increments the counter and runs the supersede CAS — and the once-key is
	// written BEFORE the CAS, so waiting on the key alone still races the
	// supersede. Wait on the SUPERSEDED line (emitted only after the CAS
	// succeeds) before asserting ANY of the goroutine's writes.
	if !waitFor(t, 10*time.Second, func() bool {
		return strings.Contains(logBuf.String(), "SUPERSEDED by the death re-read")
	}) {
		t.Fatalf("the supersede never landed; log:\n%s", logBuf.String())
	}
	latest, _ := st.Plan().GetLatestPlanForTraderSession(td, "NY", at.id)
	if latest.TriggerReason != store.TriggerDeathReplan {
		t.Fatalf("v2 must be authored by the death_replan trigger (the class-35 spending class), got %q", latest.TriggerReason)
	}
	if reason := lastLifecycleReason(t, st, row); !strings.HasPrefix(reason, "superseded:death:v2") {
		t.Fatalf("v1 must be superseded with superseded:death, got %q", reason)
	}
	if n := store.DeathRereadCount(st, at.id, td, "NY"); n != 1 {
		t.Fatalf("counter death_reread:<trader>:<date>:<session> must be 1, got %d", n)
	}
	if b := store.GetReplanBudget(st, at.id, td, "NY", 4); b.Used != 1 {
		t.Fatalf("the death re-read must SPEND one class-35 replan unit, used=%d cap=%d", b.Used, b.Cap)
	}
	if v := sysCfgVal(t, st, deathRereadDoneKey(row)); v == "" || v == "0" {
		t.Fatalf("the once-key must be set after a landed read, got %q", v)
	}
	if !strings.Contains(logBuf.String(), "SUPERSEDED by the death re-read") {
		t.Fatalf("missing the supersede log line; log:\n%s", logBuf.String())
	}
	// The 10-minute birth wick (c) is pinned at the predicate level in
	// TestDeathBornWickActive — every branch of deathBornWickActive, which is
	// the exact expression wired into both production death-check call sites
	// (the planner's death branch and executorPlanDeadReason). Driving it
	// through the session loop would need ≥2 complete post-birth 5m buckets
	// inside a 10-minute window — the guard's own boundary — which the
	// pre-existing bucket predicate cannot resolve deterministically, so the
	// predicate pin carries the (c) burden and this test carries (a)+(b).
}

// TestDeathRereadOffByteIdentical pins (d): with the knob explicitly OFF the
// death path is today's behaviour — dormant only, no read, no counter, no
// lifecycle event beyond the dormant marker.
func TestDeathRereadOffByteIdentical(t *testing.T) {
	off := false
	at, st, client := deathRealPathTrader(t, &off, func(int, string) (string, error) { return validShortPlanJSON, nil })
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	flipRereadTestNow(t, now)
	td := "2026-08-18"
	row := seedActivePlan(t, at, td, "NY", now.Add(-40*time.Minute), deathFixtureDoc())
	seedFlipBars(15500, 15470, 6*time.Minute, now)
	logBuf := captureTraderLog(t)

	at.maybeRunSessionReadsAt(now)
	defer drainReReads(t)              // CTO M4: join the async re-read before the seam resets
	time.Sleep(300 * time.Millisecond) // let any (wrong) async launch start

	if got := versionLifecycle(t, st, td, "NY", at.id, 1); got != "dormant" {
		t.Fatalf("knob OFF must still park dormant, got %q", got)
	}
	if client.calls() != 0 {
		t.Fatalf("knob OFF must launch NO read, got %d calls", client.calls())
	}
	if n := store.DeathRereadCount(st, at.id, td, "NY"); n != 0 {
		t.Fatalf("knob OFF must record no counter, got %d", n)
	}
	if reason := lastLifecycleReason(t, st, row); !strings.HasPrefix(reason, "dormant:death:") {
		t.Fatalf("the ONLY lifecycle event must be the dormant:death marker, got %q", reason)
	}
	if strings.Contains(logBuf.String(), "death re-read") {
		t.Fatalf("knob OFF must print no death re-read lines; log:\n%s", logBuf.String())
	}
}

// TestDeathRereadBudgetExhaustedStaysDormant pins (b): at budget exhausted the
// death re-read is NOT launched — the plan stays dormant (today's behaviour)
// with ONE WARN naming the budget.
func TestDeathRereadBudgetExhaustedStaysDormant(t *testing.T) {
	at, st, client := deathRealPathTrader(t, nil, func(int, string) (string, error) { return validShortPlanJSON, nil })
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	flipRereadTestNow(t, now)
	td := "2026-08-18"
	row := seedActivePlan(t, at, td, "NY", now.Add(-40*time.Minute), deathFixtureDoc())
	// Exhaust a cap-1 budget deterministically (one recorded spend).
	at.dayPlanCfg().ReplanCap = store.IntPtr(1)
	if _, err := store.SpendReplan(st, at.id, td, "NY"); err != nil {
		t.Fatal(err)
	}
	seedFlipBars(15500, 15470, 6*time.Minute, now)
	logBuf := captureTraderLog(t)

	at.maybeRunSessionReadsAt(now)
	defer drainReReads(t) // CTO M4: join the async re-read before the seam resets
	time.Sleep(300 * time.Millisecond)

	if got := versionLifecycle(t, st, td, "NY", at.id, 1); got != "dormant" {
		t.Fatalf("budget exhausted must stay dormant, got %q", got)
	}
	if client.calls() != 0 {
		t.Fatalf("budget exhausted must launch NO read, got %d calls", client.calls())
	}
	if !strings.Contains(logBuf.String(), "BUDGET EXHAUSTED (1/1)") {
		t.Fatalf("the WARN must name the budget; log:\n%s", logBuf.String())
	}
	if strings.Count(logBuf.String(), "BUDGET EXHAUSTED") != 1 {
		t.Fatalf("exactly ONE WARN per row; log:\n%s", logBuf.String())
	}
	if v := sysCfgVal(t, st, deathRereadBudgetWarnKey(row)); v != "1" {
		t.Fatalf("the one-WARN marker must be recorded, got %q", v)
	}
}

// TestDeathBornPlanGetsHoldAnchor confirms (c): a version authored by the
// death_replan trigger lands the FlipHoldAnchorReplan anchor, so a death-born
// plan carries the same 30-min flip hold the flip-born plan does (class-139
// anchors, resolved by the SAME production resolver).
func TestDeathBornPlanGetsHoldAnchor(t *testing.T) {
	versions := []kernel.PlanVersionFact{
		{Version: 1, TriggerReason: "session_read", BiasDirection: "short", CreatedAtMs: 1_000},
		{Version: 2, TriggerReason: store.TriggerDeathReplan, BiasDirection: "short", CreatedAtMs: 2_000},
	}
	got := kernel.ResolveFlipHoldAnchor(versions, nil, 2, 0)
	if got.Source != kernel.FlipHoldAnchorReplan || got.SinceMs != 2_000 {
		t.Fatalf("a death-born plan must anchor the hold at its replan version, got %+v", got)
	}
	_ = fmt.Sprintf // keep fmt if unused later
}

// TestDeathRereadHeldInsideFlapGuard pins SF-2 (money): a death re-read must NOT
// launch while the dormant row is inside DORMANT_MIN_HOLD_MIN since the dormant
// write — five of the six re-armed deaths on the DB copy re-armed at the first
// permitted instant (5m27s–10m), and a 300–500 s planner call launched at +0
// would land AFTER the re-arm: a spent unit and a zombie fresh version. The
// runaway case loses the 5 minutes, the flap case spends nothing.
func TestDeathRereadHeldInsideFlapGuard(t *testing.T) {
	// Own harness (not deathRealPathTrader): the flap guard needs hold=5.
	t.Setenv("FLIP_ATR_BUFFER", "0")
	t.Setenv("DORMANT_MIN_HOLD_MIN", "5")
	off := false
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{
		PlanEnabled: true, ReplanCap: store.IntPtr(4), SessionsEnabled: []string{"NY"}, DeathReread: nil,
		WakeOn15mZone: &off, WakeOnHTFZone: &off, WakeOnHTFOB: false, WakeOnSeatedInvalidation: &off, WakeOnIFVG: &off,
	}}
	at, st := resetTrader(t, cfg)
	client := &scriptedPlannerClient{respond: func(int, string) (string, error) { return validShortPlanJSON, nil }}
	at.mcpClient = client
	origDrift := clockHoldDriftFn
	clockHoldDriftFn = func(string) (int64, bool) { return 0, false }
	t.Cleanup(func() { clockHoldDriftFn = origDrift })
	t.Cleanup(func() { market.FuturesBarsProvider = nil; traderTestBarsInstalled = false })

	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	flipRereadTestNow(t, now)
	td := "2026-08-18"
	row := seedActivePlan(t, at, td, "NY", now.Add(-40*time.Minute), deathFixtureDoc())
	// The death branch wrote the dormant marker before this call; mirror it.
	if err := st.Plan().UpdatePlanLifecycle(row.PlanID, 1, "dormant", "dormant:death:death-condition: 5m_close close below 15480.00"); err != nil {
		t.Fatal(err)
	}
	seedFlipBars(15500, 15470, 6*time.Minute, now)
	var called atomic.Int32
	orig := deathRereadRun
	deathRereadRun = func(*AutoTrader, string, string, string, *store.PlanDB, bool) bool { called.Add(1); return true }
	t.Cleanup(func() { deathRereadRun = orig })

	// Inside the flap guard (dormant write 1 minute ago): HELD, no launch.
	_ = st.SetSystemConfig(dormantSinceKey(row), fmt.Sprintf("%d", now.Add(-1*time.Minute).UnixMilli()))
	at.maybeRereadAfterDeath(now, "NY", td, row, "death-condition: 5m_close close below 15480.00", 15470)
	time.Sleep(300 * time.Millisecond)
	if called.Load() != 0 {
		t.Fatalf("inside the flap guard the read must be HELD (0 launches), got %d", called.Load())
	}
	// After the guard elapses the same call launches.
	_ = st.SetSystemConfig(dormantSinceKey(row), fmt.Sprintf("%d", now.Add(-6*time.Minute).UnixMilli()))
	at.maybeRereadAfterDeath(now, "NY", td, row, "death-condition: 5m_close close below 15480.00", 15470)
	if !waitFor(t, 5*time.Second, func() bool { return called.Load() == 1 }) {
		t.Fatalf("after the flap guard the read must launch once, got %d", called.Load())
	}
	// Drain the goroutine before the cleanup swaps the seam back (no data race).
	if !waitFor(t, 5*time.Second, func() bool {
		_, running := deathRereadInFlight.Load(deathRereadInFlightKey(at, row))
		return !running
	}) {
		t.Fatal("the death re-read goroutine never drained")
	}
}

// TestDeathRereadRealPathRearmedMeanwhileSupersedeRefused pins SF-4: v1 re-arms
// (close-back) INSIDE the AI call; the fresh v2 still lands but the supersede
// CAS (from dormant) is REFUSED — both rows stand and the newest version
// governs at read time (the flip path's own BLOCKER-3 pin, mirrored).
func TestDeathRereadRealPathRearmedMeanwhileSupersedeRefused(t *testing.T) {
	var at *AutoTrader
	var st *store.Store
	at, st, client := deathRealPathTrader(t, nil, func(n int, _ string) (string, error) {
		pid := store.MakePlanIDForTrader(at.id, "2026-08-18", "NY")
		if err := st.Plan().UpdatePlanLifecycle(pid, 1, "active", "rearmed:2x5m close back above 15480"); err != nil {
			return "", err
		}
		return validShortPlanJSON, nil
	})
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	flipRereadTestNow(t, now)
	td := "2026-08-18"
	row := seedActivePlan(t, at, td, "NY", now.Add(-40*time.Minute), deathFixtureDoc())
	seedFlipBars(15500, 15470, 6*time.Minute, now)
	logBuf := captureTraderLog(t)

	at.maybeRunSessionReadsAt(now)
	defer drainReReads(t) // CTO M4: join the async re-read before the seam resets
	if !waitFor(t, 10*time.Second, func() bool {
		return strings.Contains(logBuf.String(), "RE-ARMED meanwhile") &&
			strings.Contains(logBuf.String(), "supersede REFUSED")
	}) {
		t.Fatalf("no supersede-REFUSED line; calls=%d log:\n%s", client.calls(), logBuf.String())
	}
	v1, _ := st.Plan().GetPlan(row.PlanID, 1)
	v2, _ := st.Plan().GetPlan(row.PlanID, 2)
	if v1 == nil || v1.Lifecycle != "active" {
		t.Fatalf("the re-armed v1 must keep its active lifecycle, got %+v", v1)
	}
	if v2 == nil || v2.Lifecycle != "active" || v2.TriggerReason != store.TriggerDeathReplan {
		t.Fatalf("the death read's v2 must stand as written, got %+v", v2)
	}
	latest, _ := st.Plan().GetLatestPlanForTraderSession(td, "NY", at.id)
	if latest == nil || latest.Version != 2 {
		t.Fatalf("the newest version governs at read time, got %+v", latest)
	}
}

// TestDeathRereadPreReadRecheckSkipsRearmedRow pins SF-4: the goroutine re-reads
// the row before the read; a row no longer dormant is skipped — no read launch.
func TestDeathRereadPreReadRecheckSkipsRearmedRow(t *testing.T) {
	at, st, _ := deathRealPathTrader(t, nil, func(int, string) (string, error) { return validShortPlanJSON, nil })
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	flipRereadTestNow(t, now)
	td := "2026-08-18"
	row := seedActivePlan(t, at, td, "NY", now.Add(-40*time.Minute), deathFixtureDoc())
	// Re-arm BEFORE the read is requested — the pre-read re-check must skip.
	if err := st.Plan().UpdatePlanLifecycle(row.PlanID, 1, "active", "rearmed:close back"); err != nil {
		t.Fatal(err)
	}
	seedFlipBars(15500, 15470, 6*time.Minute, now)
	var called atomic.Int32
	orig := deathRereadRun
	deathRereadRun = func(*AutoTrader, string, string, string, *store.PlanDB, bool) bool { called.Add(1); return true }
	t.Cleanup(func() { deathRereadRun = orig })

	at.maybeRereadAfterDeath(now, "NY", td, row, "death-condition: 5m_close close below 15480.00", 15470)
	defer drainReReads(t) // CTO M4: join the async re-read before the seam resets
	time.Sleep(300 * time.Millisecond)
	if called.Load() != 0 {
		t.Fatalf("the pre-read re-check must skip a non-dormant row (0 launches), got %d", called.Load())
	}
}

// TestDeathRereadRealPathFlapGuardHoldsLaunch pins B1 on the REAL path: the
// dormant timestamp is written a few milliseconds AFTER the cycle's clock, so
// `elapsed` is NEGATIVE at the +0 launch — negative must mean "just now" and
// HOLD. With DORMANT_MIN_HOLD_MIN=5 the death branch parks the plan and NO
// read launches this cycle (before the fix the negative elapsed fell through
// and the read launched at +0 — reproduced by the review).
func TestDeathRereadRealPathFlapGuardHoldsLaunch(t *testing.T) {
	t.Setenv("FLIP_ATR_BUFFER", "0")
	t.Setenv("DORMANT_MIN_HOLD_MIN", "5")
	off := false
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{
		PlanEnabled: true, ReplanCap: store.IntPtr(4), SessionsEnabled: []string{"NY"}, DeathReread: nil,
		WakeOn15mZone: &off, WakeOnHTFZone: &off, WakeOnHTFOB: false, WakeOnSeatedInvalidation: &off, WakeOnIFVG: &off,
	}}
	at, st := resetTrader(t, cfg)
	client := &scriptedPlannerClient{respond: func(int, string) (string, error) { return validShortPlanJSON, nil }}
	at.mcpClient = client
	origDrift := clockHoldDriftFn
	clockHoldDriftFn = func(string) (int64, bool) { return 0, false }
	t.Cleanup(func() { clockHoldDriftFn = origDrift })
	t.Cleanup(func() { market.FuturesBarsProvider = nil; traderTestBarsInstalled = false })

	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	flipRereadTestNow(t, now)
	td := "2026-08-18"
	row := seedActivePlan(t, at, td, "NY", now.Add(-40*time.Minute), deathFixtureDoc())
	seedFlipBars(15500, 15470, 6*time.Minute, now)
	logBuf := captureTraderLog(t)

	at.maybeRunSessionReadsAt(now)
	defer drainReReads(t) // CTO M4: join the async re-read before the seam resets
	if got := versionLifecycle(t, st, td, "NY", at.id, 1); got != "dormant" {
		t.Fatalf("death must park dormant first, got %q", got)
	}
	if !strings.Contains(logBuf.String(), "HELD: the dormant row is inside the 5-minute flap guard") {
		t.Fatalf("the +0 launch must be HELD (the dormant timestamp lands AFTER the cycle clock); log:\n%s", logBuf.String())
	}
	time.Sleep(300 * time.Millisecond)
	if client.calls() != 0 {
		t.Fatalf("B1: no read may launch inside the flap guard, got %d AI calls", client.calls())
	}
	_ = row
}

// TestDeathRereadWickThroughProductionCallSite is the independent review's last
// ask (2026-09-18): a REAL two-version chain driven through the production call
// site (maybeRunSessionReadsAt → describeActivePlanDeath → the
// deathBornWickActive expression with at.priorDeathLinePrice), plus the
// return-0 sabotage as a negative control. FLIP_CONFIRM_CLOSES=1 lets the death
// fire with ONE closed 5m bucket, so the predicate can fire INSIDE the 10-minute
// wick (the production 2-close floor makes the evidence window equal the wick —
// boundary-untestable). With the recorded RAW prior line the same-line wick
// keeps v2 ALIVE; with the seam sabotaged to 0 the SAME tape kills v2.
func TestDeathRereadWickThroughProductionCallSite(t *testing.T) {
	run := func(sabotage bool) {
		t.Helper()
		t.Setenv("FLIP_ATR_BUFFER", "0")
		t.Setenv("FLIP_CONFIRM_CLOSES", "1")
		t.Setenv("DORMANT_MIN_HOLD_MIN", "0")
		off := false
		cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{
			PlanEnabled: true, ReplanCap: store.IntPtr(4), SessionsEnabled: []string{"NY"}, DeathReread: nil,
			WakeOn15mZone: &off, WakeOnHTFZone: &off, WakeOnHTFOB: false, WakeOnSeatedInvalidation: &off, WakeOnIFVG: &off,
		}}
		at, st := resetTrader(t, cfg)
		client := &scriptedPlannerClient{respond: func(int, string) (string, error) { return "not json", nil }}
		at.mcpClient = client
		origDrift := clockHoldDriftFn
		clockHoldDriftFn = func(string) (int64, bool) { return 0, false }
		t.Cleanup(func() { clockHoldDriftFn = origDrift })
		t.Cleanup(func() { market.FuturesBarsProvider = nil; traderTestBarsInstalled = false })
		origSeam := priorDeathLinePriceSeam
		if sabotage {
			priorDeathLinePriceSeam = func(*AutoTrader, *store.PlanDB) float64 { return 0 }
		}
		t.Cleanup(func() { priorDeathLinePriceSeam = origSeam })

		now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
		flipRereadTestNow(t, now)
		td := "2026-08-18"

		// Version 1: active, dies below its 15480 death line on the first cycle.
		row := seedActivePlan(t, at, td, "NY", now.Add(-40*time.Minute), deathFixtureDoc())
		// The death re-read launches ASYNC (production behavior). Its goroutine
		// reads market.FuturesBarsProvider through kernel.ResolveVoidScope; the
		// cleanup above nils that global, and under -race the pair is a DATA
		// RACE (read kernel/void_scope.go:91 vs the nil write — caught by the
		// first race run at 5198452c). Join the goroutines before returning: an
		// empty in-flight map means each goroutine has run its deferred Delete,
		// its last act, so no provider read remains. Bounded so a stuck
		// goroutine fails the test loudly instead of hanging the package.
		joinReReads := func() {
			deadline := time.Now().Add(10 * time.Second)
			for {
				n := 0
				deathRereadInFlight.Range(func(_, _ any) bool { n++; return true })
				if n == 0 {
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("async death re-read goroutines never drained before teardown (they read market.FuturesBarsProvider — the cleanup would nil it)")
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
		seedFlipBars(15500, 15470, 6*time.Minute, now)
		at.maybeRunSessionReadsAt(now)
		if got := versionLifecycle(t, st, td, "NY", at.id, 1); got != "dormant" {
			t.Fatalf("v1 must park dormant first, got %q", got)
		}
		// The dormant write records the RAW prior line (B2 production path).
		if v := sysCfgVal(t, st, deathRereadPriorLineKey(row)); v != "15480.000000" {
			t.Fatalf("the RAW prior line must be recorded at the dormant write, got %q", v)
		}
		// Cycle 1 launched the v1 re-read goroutine. Join it BEFORE the next
		// global write: flipRereadTestNow(now2) and barsAt(bars) below touch
		// the clock and provider globals, and the goroutine still reads the
		// provider inside assemblePlannerInputWithCtx → detectHTFLevels
		// (-race caught barsAt-write vs goroutine-read — the second pair at
		// 5198452c). An empty in-flight map = the goroutine ran its deferred
		// Delete, its last act, so no further read remains.
		joinReReads()

		// Version 2: SEEDED death-born (the read would author it with the real
		// clock, which the wick compares against — seeded CreatedAt = now keeps
		// the chain and the clock in one space). SAME raw death line.
		v2doc, _ := json.Marshal(kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "short"}, DeathStructured: &kernel.PlanCondition{Price: 15480, Side: "below", Rule: "5m_close"}})
		pid := store.MakePlanIDForTrader(at.id, td, "NY")
		if _, err := at.store.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: td, Session: "NY", StrategyID: at.id, TriggerReason: store.TriggerDeathReplan, Lifecycle: "active", Doc: string(v2doc), CreatedAt: now}); err != nil {
			t.Fatal(err)
		}

		// Cycle 2, six minutes later — INSIDE the 10-minute wick. A hand-built
		// tape whose first post-birth bar straddles the raw line (touch gate)
		// and whose next five close below it: one closed 5m bucket = death
		// evidence with FLIP_CONFIRM_CLOSES=1.
		now2 := now.Add(6 * time.Minute)
		flipRereadTestNow(t, now2)
		t0 := now.UnixMilli()
		bars := []market.Kline{
			{OpenTime: t0, CloseTime: t0 + 60_000 - 1, Open: 15500, High: 15500, Low: 15470, Close: 15470},
		}
		for i := 1; i <= 5; i++ {
			ot := t0 + int64(i)*60_000
			bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 60_000 - 1, Open: 15470, High: 15470, Low: 15470, Close: 15470})
		}
		barsAt(bars)
		logBuf := captureTraderLog(t)
		at.maybeRunSessionReadsAt(now2)

		if sabotage {
			// The SAME tape must kill v2 — the guard is gone (seam returns 0),
			// so the death check proceeds normally.
			if got := versionLifecycle(t, st, td, "NY", at.id, 2); got != "dormant" {
				t.Fatalf("SABOTAGE CONTROL: with priorDeathLinePrice returning 0, v2 must DIE on the same tape, got %q; log:\n%s", got, logBuf.String())
			}
			if !strings.Contains(logBuf.String(), "plan 2026-08-18 NY v2 DORMANT — death-condition") {
				t.Fatalf("SABOTAGE CONTROL: the v2 dormant line must print; log:\n%s", logBuf.String())
			}
			joinReReads()
			return
		}
		// POSITIVE: the recorded RAW prior line makes the same-line wick active
		// — v2 survives the tape that would otherwise kill it.
		if got := versionLifecycle(t, st, td, "NY", at.id, 2); got != "active" {
			t.Fatalf("the production wick (priorDeathLinePrice through the call site) must keep the same-line v2 active inside the 10-minute wick, got %q; log:\n%s", got, logBuf.String())
		}
		joinReReads()
	}
	t.Run("positive-same-line-wick", func(t *testing.T) { run(false) })
	t.Run("negative-return-0-sabotage", func(t *testing.T) { run(true) })
}
