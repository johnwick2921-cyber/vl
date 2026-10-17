package trader

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// W-FLIP-OWNS-THE-BREACH (2026-09-17) — tests at the PRODUCTION call sites:
// maybeWakePlannerOnLevelEventsAt / maybeWakePlannerOnMSSAt (the R1/R3 gate),
// describeActivePlanDeath (the R2 window) and maybeRunSessionReadsAt (the
// flip → dormant → structure_flip read, and the death path the gate must not
// touch). Harness: flipHoldTrader (CLASS 139) — NY, 09:35:30 CT, ATR buffer 0.

func resetFlipOnceKeys() {
	flipWindowNoted.Range(func(k, _ any) bool { flipWindowNoted.Delete(k); return true })
	flipWakeDeferNote.Range(func(k, _ any) bool { flipWakeDeferNote.Delete(k); return true })
}

// breachPlanDoc: bias short, flip {flip above 2x5m → long}, optional death
// {death above 5m_close}, and ONE seated Supply level at 98 so a close at 104
// is a seated-invalidation wake candidate every cycle (the wake that must be
// deferred while the flip line is breached).
func breachPlanDoc(t *testing.T, flip, death float64) string {
	t.Helper()
	doc := kernel.PlanDoc{
		Bias:           kernel.PlanBias{Direction: "short", FlipCondition: "flips long on 2x5m above the line"},
		FlipStructured: &kernel.PlanCondition{Price: flip, Side: "above", Rule: "2x5m", FlipTo: "long"},
		Levels:         []kernel.PlanLevel{{Price: 98, Label: "Supply·1h"}},
	}
	if death > 0 {
		doc.DeathStructured = &kernel.PlanCondition{Price: death, Side: "above", Rule: "5m_close"}
	}
	blob, _ := json.Marshal(doc)
	return string(blob)
}

// tapeBackInside: flipHoldTape with the LAST `back` minutes closing at 100
// again (price closed back inside the flip line).
func tapeBackInside(now time.Time, minutes, breakMinutesBeforeNow, back int) []market.Kline {
	bars := flipHoldTape(now, minutes, breakMinutesBeforeNow)
	for i := len(bars) - back; i < len(bars); i++ {
		if i >= 0 {
			bars[i].Open, bars[i].High, bars[i].Low, bars[i].Close = 100, 100, 100, 100
		}
	}
	return bars
}

// (1) breach in progress, one close beyond → the level-event wake is DEFERRED
// with the line and nothing is read; the second close → the flip fires,
// the plan goes dormant and (knob ON) the structure_flip read is requested.
func TestFlipBreachDefersLevelWakeThenFlipFires(t *testing.T) {
	resetFlipOnceKeys()
	t.Setenv("DORMANT_MIN_HOLD_MIN", "0")
	at, st, now := flipHoldTrader(t)
	at.config.StrategyConfig.DayPlan.FlipReread = true
	rec := &flipRereadRecorder{}
	installFlipRecorder(t, rec)
	td := "2026-08-18"
	row := appendVersion(t, st, at, td, "NY_scheduled_read", breachPlanDoc(t, 100, 0), now.Add(-45*time.Minute))
	barsAt(flipHoldTape(now, 40, 5)) // 09:30 bucket closed at 104: one close beyond, touched
	buf := captureTraderLog(t)

	at.maybeWakePlannerOnLevelEventsAt(now, "NY", td, row)

	if !strings.Contains(buf.String(), "wake deferred: flip line breached (above 100.00, closes 1/2)") {
		t.Fatalf("want the deferral line:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "waking the planner") || at.lastLevelWakeKey != "" || !at.lastPlannerWakeAt.IsZero() {
		t.Fatalf("a deferred wake must not read or touch the wake clock:\n%s", buf.String())
	}
	// once per version: a second cycle logs nothing new
	buf.Reset()
	at.maybeWakePlannerOnLevelEventsAt(now, "NY", td, row)
	at.maybeWakePlannerOnMSSAt(now, "NY", td, row)
	if strings.Contains(buf.String(), "wake deferred") || strings.Contains(buf.String(), "waking the planner") {
		t.Fatalf("deferral must be logged once per version and keep deferring:\n%s", buf.String())
	}
	if got, _ := st.Plan().GetLatestPlanForTraderSession(td, "NY", at.id); got.Version != 1 || got.Lifecycle != "active" {
		t.Fatalf("no version may be written while deferred: v%d %s", got.Version, got.Lifecycle)
	}

	// second close beyond → the flip evaluator fires at the session-read site
	barsAt(flipHoldTape(now, 40, 10))
	buf.Reset()
	at.maybeRunSessionReadsAt(now)
	defer drainReReads(t) // CTO M4: join the async re-read before the seam resets
	got, _ := st.Plan().GetLatestPlanForTraderSession(td, "NY", at.id)
	if got.Lifecycle != "dormant" || !strings.HasPrefix(lastLifecycleReason(t, st, got), "dormant:flip:") && !strings.Contains(dormantFlipKiller(t, st, got), "flip-condition") {
		t.Fatalf("second close must fire the flip → dormant:flip, got %s %q\n%s", got.Lifecycle, lastLifecycleReason(t, st, got), buf.String())
	}
	// the read is async (W6-C pattern); the CLASS 141 harness waits on it.
	if !waitFor(t, 5*time.Second, func() bool { return rec.count() == 1 }) {
		t.Fatalf("knob ON: the fired flip must request ONE structure_flip read, got %d\n%s", rec.count(), buf.String())
	}
	if strings.Contains(buf.String(), "wake deferred") {
		t.Fatalf("a fired flip is handled by the evaluator, never by the wake gate:\n%s", buf.String())
	}
}

// (2) price closes back inside → wakes resume (the resume line, then the
// ordinary wake fires).
func TestFlipBreachClearsWakesResume(t *testing.T) {
	resetFlipOnceKeys()
	at, st, now := flipHoldTrader(t)
	td := "2026-08-18"
	row := appendVersion(t, st, at, td, "NY_scheduled_read", breachPlanDoc(t, 100, 0), now.Add(-45*time.Minute))
	barsAt(flipHoldTape(now, 40, 5))
	buf := captureTraderLog(t)
	at.maybeWakePlannerOnLevelEventsAt(now, "NY", td, row)
	if !strings.Contains(buf.String(), "wake deferred: flip line breached") {
		t.Fatalf("precondition: deferred\n%s", buf.String())
	}
	// 09:30 bucket now closes at 100 (back inside); the seated Supply at 98 is
	// still invalidated by the 100 close, so a wake candidate still exists.
	barsAt(tapeBackInside(now, 40, 10, 5))
	buf.Reset()
	at.maybeWakePlannerOnLevelEventsAt(now, "NY", td, row)
	if !strings.Contains(buf.String(), "wakes resume on NY v1: flip line above 100.00 no longer breached") {
		t.Fatalf("want the resume line:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "waking the planner") || at.lastLevelWakeKey == "" {
		t.Fatalf("once inside, the ordinary wake must fire:\n%s", buf.String())
	}
	// The wake's async read (no AI client → benign failure) must END before
	// the test returns, or it races the next harness's clock seam (-race).
	if !waitFor(t, 10*time.Second, func() bool {
		_, open := anyPlannerStreamOpen()
		return !open && strings.Contains(buf.String(), "wake re-read failed for")
	}) {
		t.Fatalf("the wake read did not finish:\n%s", buf.String())
	}
}

// (3) a same-bias wake authored v2 with the flip line within tolerance (100 →
// 101.5) 4 minutes ago → the flip fires on closes counted from v1's birth
// (the chain anchor). On the version window (pre-wave) it does not.
func TestFlipBreachSameBiasWakeKeepsTheWindow(t *testing.T) {
	resetFlipOnceKeys()
	at, st, now := flipHoldTrader(t)
	td := "2026-08-18"
	appendVersion(t, st, at, td, "NY_scheduled_read", breachPlanDoc(t, 100, 0), now.Add(-45*time.Minute))
	row := appendVersion(t, st, at, td, "level_event", breachPlanDoc(t, 101.5, 0), now.Add(-4*time.Minute))
	bars := flipHoldTape(now, 40, 10) // 09:25 + 09:30 buckets closed at 104
	barsAt(bars)
	// pre-wave semantics: window = v2's birth (4 min ago) → one bucket → no flip
	var doc kernel.PlanDoc
	_ = json.Unmarshal([]byte(row.Doc), &doc)
	if _, firedOld, _ := kernel.PlanDeathOrFlipSinceFreshHold(doc, bars, "2x5m", row.CreatedAt.UnixMilli(), now.UnixMilli(), kernel.FlipHoldAnchor{SinceMs: now.Add(-45 * time.Minute).UnixMilli(), Source: kernel.FlipHoldAnchorBirth}); firedOld {
		t.Fatalf("pre-wave window must reproduce the miss (closes restart at v2's birth)")
	}
	buf := captureTraderLog(t)
	detail, dead := at.describeActivePlanDeath(row)
	if !dead || !strings.Contains(detail.Killer, "flip-condition") {
		t.Fatalf("chain-windowed flip must fire: dead=%v killer=%q\n%s", dead, detail.Killer, buf.String())
	}
	if !strings.Contains(buf.String(), "flip window: ") || !strings.Contains(buf.String(), "keeps the chain's flip line (above 101.50, within 3.00 pt) — closes counted from v1's birth") {
		t.Fatalf("want the chain-window note:\n%s", buf.String())
	}
	if cw := at.flipConditionAnchor(row); cw.Source != kernel.FlipWindowChain || cw.AnchorVersion != 1 || cw.SinceMs != now.Add(-45*time.Minute).UnixMilli() {
		t.Fatalf("anchor must be v1's birth: %+v", cw)
	}
	if h := at.flipHoldAnchor(row); h.Source != kernel.FlipHoldAnchorBirth {
		t.Fatalf("the CLASS 139 hold anchor is unchanged: %+v", h)
	}
}

// (4) the wake MOVED the line (100 → 110) → window from v2's birth, logged as
// moved; the untouched new line is no breach, so wakes are not deferred.
func TestFlipBreachMovedLineWindowsFromBirth(t *testing.T) {
	resetFlipOnceKeys()
	at, st, now := flipHoldTrader(t)
	td := "2026-08-18"
	appendVersion(t, st, at, td, "NY_scheduled_read", breachPlanDoc(t, 100, 0), now.Add(-45*time.Minute))
	row := appendVersion(t, st, at, td, "level_event", breachPlanDoc(t, 110, 0), now.Add(-4*time.Minute))
	barsAt(flipHoldTape(now, 40, 10))
	buf := captureTraderLog(t)
	if _, dead := at.describeActivePlanDeath(row); dead {
		t.Fatalf("104 is under the moved line 110: must not fire\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "flip line MOVED on") || !strings.Contains(buf.String(), "100.00 → 110.00 (Δ10.00 > 3.00 pt tolerance) — the flip window opens at this version's birth") {
		t.Fatalf("want the moved line:\n%s", buf.String())
	}
	if cw := at.flipConditionAnchor(row); cw.Source != kernel.FlipWindowMoved || !cw.Moved || cw.SinceMs != row.CreatedAt.UnixMilli() {
		t.Fatalf("moved line must window from v2's birth: %+v", cw)
	}
	if at.wakeDeferredByFlip(now, "NY", row) {
		t.Fatalf("an untouched moved line is not a breach — wakes must not be deferred")
	}
}

// (5) stale bars → the flip evaluation is skipped (G7) and the wake is
// deferred for the same reason, with no read.
func TestFlipBreachStaleBarsDeferWakes(t *testing.T) {
	resetFlipOnceKeys()
	at, st, now := flipHoldTrader(t)
	td := "2026-08-18"
	row := appendVersion(t, st, at, td, "NY_scheduled_read", breachPlanDoc(t, 100, 0), now.Add(-45*time.Minute))
	barsAt(flipHoldTape(now.Add(-15*time.Minute), 40, 5)) // newest 5m close 15 min old > 5m + 90s
	buf := captureTraderLog(t)
	at.maybeWakePlannerOnLevelEventsAt(now, "NY", td, row)
	at.maybeWakePlannerOnMSSAt(now, "NY", td, row)
	if !strings.Contains(buf.String(), "wake deferred: flip evaluation skipped (stale_bars, age") || !strings.Contains(buf.String(), "a wake must not author on the tape the flip evaluator refused") {
		t.Fatalf("want the stale deferral:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "waking the planner") || at.lastLevelWakeKey != "" || at.lastMSSWakeKey != "" {
		t.Fatalf("stale tape must author nothing:\n%s", buf.String())
	}
}

// (6) OFF-nothing: a scheduled read with no row fires regardless of the tape,
// and a DEATH line breached alongside the flip line goes through the death
// path (dormant:death) — the wake gate never sees it.
func TestFlipBreachScheduledReadsAndDeathUntouched(t *testing.T) {
	resetFlipOnceKeys()
	t.Setenv("DORMANT_MIN_HOLD_MIN", "0")
	at, st, now := flipHoldTrader(t)
	td := "2026-08-18"
	// death 102 (5m_close, floored at FLIP_CONFIRM_CLOSES=2) and flip 100 both
	// have two closes at 104: death is evaluated first and wins → dormant:death.
	appendVersion(t, st, at, td, "NY_scheduled_read", breachPlanDoc(t, 100, 102), now.Add(-45*time.Minute))
	barsAt(flipHoldTape(now, 40, 10))
	buf := captureTraderLog(t)
	at.maybeRunSessionReadsAt(now)
	defer drainReReads(t) // CTO M4: join the async re-read before the seam resets
	got, _ := st.Plan().GetLatestPlanForTraderSession(td, "NY", at.id)
	if got.Lifecycle != "dormant" || !strings.HasPrefix(lastLifecycleReason(t, st, got), "dormant:death:") {
		t.Fatalf("death must win → dormant:death, got %s %q\n%s", got.Lifecycle, lastLifecycleReason(t, st, got), buf.String())
	}
	if strings.Contains(buf.String(), "wake deferred") {
		t.Fatalf("the death path must not pass the wake gate:\n%s", buf.String())
	}
	if at.wakeDeferredByFlip(now, "NY", got) {
		t.Fatalf("a dormant row is outside R1")
	}
	// A scheduled read with NO row fires regardless of the tape (the gate lives
	// only in the two ordinary-wake functions). Second harness so the async
	// read it launches cannot touch the assertions above.
	at2, _, _ := flipHoldTrader(t)
	barsAt(flipHoldTape(now, 40, 5))
	if fired := at2.maybeRunSessionReadsAt(now); len(fired) != 1 || fired[0].Session != "NY" {
		t.Fatalf("scheduled read must fire with no row on a breached tape: %+v", fired)
	}
	// Let the async read END (it fail-closes to a no_trade row) before the
	// test returns — see the note in TestFlipBreachClearsWakesResume.
	if !waitFor(t, 10*time.Second, func() bool {
		row, _ := at2.store.Plan().GetLatestPlanForTraderSession(td, "NY", at2.id)
		_, open := anyPlannerStreamOpen()
		return row != nil && !open
	}) {
		t.Fatalf("the scheduled read did not finish")
	}
}

// REPLAY 2026-09-17 ASIA from the live store (plan 2026-09-17:ASIA:…, tape
// trader/flip_breach_fixture_test.go). What the journal shows, pinned:
//
//	22:42:33 CT  flip_eval_skipped v1 flip=stale_bars (age 453s) AND, the same
//	             second, "structure MSS … waking the planner" — the wake that
//	             authored v2 at 22:52:04 on the tape the evaluator refused (R3).
//	             v1's line 29772.62 was NEVER breached: the highest 5m close
//	             before v2 was 29762.25 (22:45), highest high 29764.5 (22:50).
//	22:52:04     v2 short, flip MOVED 29772.62 → 29747.50 (Δ25.12 > 3.00) —
//	             born with the line BELOW price; no post-birth bar touched it.
//	23:10:46     v2 DORMANT — death-condition: 2x5m close above 29755.50.
func TestFlipBreachReplayASIA0917(t *testing.T) {
	resetFlipOnceKeys()
	t.Setenv("FLIP_MIN_HOLD_MIN", "")
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: store.IntPtr(4), SessionsEnabled: []string{"ASIA"}}}
	at, st := resetTrader(t, cfg)
	ct := kernel.CTLocation()
	t.Cleanup(func() { testNow = nil })
	t.Cleanup(func() { market.FuturesBarsProvider = nil; traderTestBarsInstalled = false })

	td := "2026-09-17"
	pid := store.MakePlanIDForTrader(at.id, td, "ASIA")
	v1Doc := kernel.PlanDoc{
		Bias:            kernel.PlanBias{Direction: "short", Conviction: "low", FlipCondition: "5m close above 29772.62 flips to long; 5m close above 29797.88 kills the plan"},
		FlipStructured:  &kernel.PlanCondition{Price: 29772.62, Side: "above", Rule: "5m_close", FlipTo: "long"},
		DeathStructured: &kernel.PlanCondition{Price: 29797.88, Side: "above", Rule: "5m_close"},
	}
	v2Doc := kernel.PlanDoc{
		Bias:            kernel.PlanBias{Direction: "short", Conviction: "medium", FlipCondition: "A 5m close above 29747.50 flips bias long."},
		FlipStructured:  &kernel.PlanCondition{Price: 29747.5, Side: "above", Rule: "5m_close", FlipTo: "long"},
		DeathStructured: &kernel.PlanCondition{Price: 29755.5, Side: "above", Rule: "2x5m"},
	}
	v1Blob, _ := json.Marshal(v1Doc)
	v2Blob, _ := json.Marshal(v2Doc)
	all := make([]market.Kline, 0, len(asia0917OneMinuteBars))
	for _, r := range asia0917OneMinuteBars {
		ot := int64(r[0])
		all = append(all, market.Kline{OpenTime: ot, CloseTime: ot + 60_000 - 1, Open: r[1], High: r[2], Low: r[3], Close: r[4], Volume: r[5]})
	}
	cutAt := func(before time.Time) []market.Kline {
		var out []market.Kline
		for _, b := range all {
			if b.OpenTime < before.UnixMilli() {
				out = append(out, b)
			}
		}
		return out
	}

	// ── v1 at 22:42:33 on the post-reboot cache (newest 1m bar 22:34) ──────
	if _, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: td, Session: "ASIA", StrategyID: at.id, TriggerReason: "ASIA_scheduled_read", Lifecycle: "active", Doc: string(v1Blob), CreatedAt: time.Date(2026, 9, 17, 16, 38, 46, 0, ct)}); err != nil {
		t.Fatal(err)
	}
	v1, _ := st.Plan().GetLatestPlanForTraderSession(td, "ASIA", at.id)
	now1 := time.Date(2026, 9, 17, 22, 42, 33, 0, ct)
	testNow = func() time.Time { return now1 }
	barsAt(cutAt(time.Date(2026, 9, 17, 22, 35, 0, 0, ct)))
	buf := captureTraderLog(t)
	if _, dead := at.describeActivePlanDeath(v1); dead || !strings.Contains(buf.String(), "flip=stale_bars (age 453s)") {
		t.Fatalf("22:42:33 must reproduce the journal's flip=stale_bars (age 453s):\n%s", buf.String())
	}
	buf.Reset()
	at.maybeWakePlannerOnMSSAt(now1, "ASIA", td, v1)
	if !strings.Contains(buf.String(), "wake deferred: flip evaluation skipped (stale_bars, age 453s) on ASIA v1") || strings.Contains(buf.String(), "waking the planner") {
		t.Fatalf("R3: the 22:42:33 MSS wake must be deferred on the stale tape:\n%s", buf.String())
	}
	// and with the full tape at 22:52 the line was still not breached: never touched.
	now2 := time.Date(2026, 9, 17, 22, 52, 0, 0, ct)
	testNow = func() time.Time { return now2 }
	barsAt(cutAt(now2))
	if b := kernel.FlipBreachState(*v1Doc.FlipStructured, cutAt(now2), v1.CreatedAt.UnixMilli(), now2.UnixMilli()); b.Breached || b.Touched || b.Stale {
		t.Fatalf("v1's 29772.62 was never reached before v2 (max high 29764.5): %+v", b)
	}
	if hi := maxHigh(cutAt(now2), time.Date(2026, 9, 17, 21, 30, 0, 0, ct)); hi != 29764.5 {
		t.Fatalf("fixture drift: max high before 22:52 is %.2f, journal/store say 29764.5", hi)
	}

	// ── v2 born 22:52:04 with a MOVED line below price; death wins 23:10:46 ─
	if _, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: td, Session: "ASIA", StrategyID: at.id, TriggerReason: "structure_mss", Lifecycle: "active", Doc: string(v2Blob), CreatedAt: time.Date(2026, 9, 17, 22, 52, 4, 0, ct)}); err != nil {
		t.Fatal(err)
	}
	v2, _ := st.Plan().GetLatestPlanForTraderSession(td, "ASIA", at.id)
	if v2.Version != 2 {
		t.Fatalf("v2 read back: %+v", v2)
	}
	now3 := time.Date(2026, 9, 17, 23, 10, 46, 0, ct)
	testNow = func() time.Time { return now3 }
	barsAt(all)
	if cw := at.flipConditionAnchor(v2); !cw.Moved || cw.PrevPrice != 29772.62 || cw.SinceMs != v2.CreatedAt.UnixMilli() {
		t.Fatalf("v2's line moved 25.12 pt: window from its birth, logged as moved: %+v", cw)
	}
	if at.wakeDeferredByFlip(now3, "ASIA", v2) {
		t.Fatalf("v2's line 29747.50 was never touched after birth — no breach, wakes not deferred (no deadlock behind a flip that cannot fire)")
	}
	buf.Reset()
	detail, dead := at.describeActivePlanDeath(v2)
	if !dead || !strings.HasPrefix(detail.Killer, "death-condition: 2x5m close above 29755.50") {
		t.Fatalf("23:10:46 must reproduce the journal's death-dormant: dead=%v killer=%q\n%s", dead, detail.Killer, buf.String())
	}
	if !strings.Contains(buf.String(), "flip line MOVED on") || !strings.Contains(buf.String(), "29772.62 → 29747.50") {
		t.Fatalf("want the moved line for v2:\n%s", buf.String())
	}
}

func maxHigh(bars []market.Kline, from time.Time) float64 {
	hi := 0.0
	for _, b := range bars {
		if b.OpenTime >= from.UnixMilli() && b.High > hi {
			hi = b.High
		}
	}
	return hi
}
