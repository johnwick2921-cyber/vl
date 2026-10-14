package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// ── r22 RISK-GATES — THE DAILY-LOSS GATE AT ITS PRODUCTION CALL SITES ────────
//
// Every test drives a production entry point: at.monitorTick(now) (the 60s
// wall-clock beat that latches, withdraws and flattens), the agent door's
// admission (AdmitManualEntryBracketAt → admitEntry), the arm path's
// admitEntry, the mentor's authoring step (mentorPlaceIntent → the recorder
// seam), attachTradeContext (the decision path's kernel input) and
// kernel.MaybeForceFlat (the Emergency Flat's reset). The wire is the real
// in-process NT8 link of the withdraw fixture, so a cancel and a flatten are
// FRAMES the fake AddOn received, not flags.

// dlSeedClosed writes one CLOSED row with a corrected P&L. origin "" is a
// bot row (the column default); store.OriginManual is a manual row — the ONE
// manual rule (r22 pnl-truth, store/manual_origin.go), not the plan_id
// sentinel, decides.
func dlSeedClosed(t *testing.T, st *store.Store, traderID string, exit time.Time, pnl float64, origin string) {
	t.Helper()
	p := &store.TraderPosition{TraderID: traderID, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryQuantity: 1, EntryPrice: 100, ExitPrice: 100 + pnl/2, RealizedPnL: pnl, PnlCorrected: &pnl,
		Status: "CLOSED", CloseReason: "sync", Origin: origin,
		EntryTime: exit.Add(-5 * time.Minute).UnixMilli(), ExitTime: exit.UnixMilli(),
		CreatedAt: exit.UnixMilli(), UpdatedAt: exit.UnixMilli()}
	if err := st.GormDB().Create(p).Error; err != nil {
		t.Fatal(err)
	}
}

// dlSeedOpen writes one OPEN bot LONG 1 @ entry.
func dlSeedOpen(t *testing.T, st *store.Store, traderID string, at time.Time, entry float64) *store.TraderPosition {
	t.Helper()
	p := &store.TraderPosition{TraderID: traderID, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryQuantity: 1, EntryPrice: entry, Status: "OPEN",
		EntryTime: at.UnixMilli(), CreatedAt: at.UnixMilli(), UpdatedAt: at.UnixMilli()}
	if err := st.GormDB().Create(p).Error; err != nil {
		t.Fatal(err)
	}
	return p
}

// dlOn switches BOTH guardrail toggles on, the way a Studio save stores them.
// Since r22 an ABSENT toggle reads OFF (owner R4), so every fixture that
// means "the owner turned the gate on" must say so.
func dlOn(c *store.StrategyConfig) {
	on := true
	c.RiskControl.GuardrailsEnabled = &on
	c.RiskControl.DailyLossEnabled = &on
}

func dlMark(t *testing.T, px float64) {
	t.Helper()
	dailyLossMarkSource = func(string) (float64, bool) { return px, true }
	t.Cleanup(func() { dailyLossMarkSource = nil })
}

func dlGateBlocks(traderID string) map[string]int {
	_, table := telemetry.GateBlockSnapshot()
	if table[traderID] == nil {
		return map[string]int{}
	}
	return table[traderID]
}

func dlClosePositionFrames(w *wdWire) int {
	n := 0
	for _, f := range w.all() {
		if f.Type == ntwire.FrameClosePosition {
			n++
		}
	}
	return n
}

// 1 — A BREACH (owner's limit ON): the 60s beat LATCHES, CANCELS the resting
// entry, FLATTENS the bot's position; every new placement is REFUSED; the latch
// holds after the P&L recovers, until 17:00 CT, and lifts after.
//
// The loss is realized −60 + open −60 (LONG 1 @100 marked 70, $2/pt): the
// realized half alone is inside the $100 limit, so the OPEN half is what trips.
//
// MUTANTS (L4): drop dailyLossFlattenAt's flatten → no close_position frame →
// RED; drop the admitChain daily-guardrail step ("check only mentor
// placements") → the agent door is not refused on daily_loss → RED.
func TestDailyLossBreachRefusesCancelsFlattensAndLatchesUntil1700(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	f := withdrawFixture(t, "dl-breach", func(c *store.StrategyConfig) { dlOn(c); c.RiskControl.DailyLossLimitUSD = 100 })
	row, sid := f.placeResting(t)
	t1 := f.now.Add(time.Minute)

	dlSeedClosed(t, f.st, f.at.id, f.now.Add(-30*time.Minute), -60, "")
	dlSeedOpen(t, f.st, f.at.id, f.now.Add(-10*time.Minute), 100)
	f.w.srv.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "long", Quantity: 1, AvgPrice: 100}})
	f.at.peakPnLCache = map[string]float64{} // NewAutoTrader's map: the beat's drawdown check reads the held position
	dlMark(t, 70)
	f.setLiveBook(t1, wdEntryOrder(sid))
	if kernel.DailyForceFlatReasonAt(f.at.id, t1) != "" {
		t.Fatal("fixture: latched before the beat")
	}

	f.at.monitorTick(t1)

	// LATCHED.
	why := kernel.DailyForceFlatReasonAt(f.at.id, t1)
	if why == "" {
		t.Fatal("a bot loss of -120 against a $100 limit must LATCH the daily-loss gate on the 60s beat — nothing tripped")
	}
	if !strings.Contains(why, "realized -60.00") || !strings.Contains(why, "open -60.00") {
		t.Fatalf("the latch must name the bot P&L it judged (realized + open), got %q", why)
	}
	// CANCELLED: the resting ENTRY got a cancel_order and sits cancel_pending.
	f.w.waitFor(t, "the resting entry's cancel_order", isCancelFor(sid))
	got := f.rowByID(t, row.ID)
	if got.State != store.StateCancelPending || !strings.Contains(got.StateReason, WithdrawReasonPrefix+"daily_force_flat") {
		t.Fatalf("the resting entry must be withdrawn (cancel_pending, %sdaily_force_flat), got state=%q reason=%q",
			WithdrawReasonPrefix, got.State, got.StateReason)
	}
	// FLATTENED: a close_position frame reached the AddOn.
	f.w.waitFor(t, "the daily-loss flatten (close_position)", func(fr wdFrame) bool { return fr.Type == ntwire.FrameClosePosition })
	// COUNTED: one trip.
	if n := dlGateBlocks(f.at.id)["daily_loss_tripped"]; n != 1 {
		t.Fatalf("the trip must be counted once (daily_loss_tripped), got %d", n)
	}
	// A second beat inside the resend window does not re-send the flatten.
	f.w.flush(t)
	before := dlClosePositionFrames(f.w)
	f.at.monitorTick(t1.Add(30 * time.Second))
	f.w.flush(t)
	if after := dlClosePositionFrames(f.w); after != before {
		t.Fatalf("a flatten must not be re-sent inside %s (had %d close frames, now %d)", dailyLossFlattenResend, before, after)
	}
	if n := dlGateBlocks(f.at.id)["daily_loss_tripped"]; n != 1 {
		t.Fatalf("a latched beat is not a new trip — daily_loss_tripped must stay 1, got %d", n)
	}

	// REFUSED: the agent door (the decision chain) …
	refusal, refused := f.at.AdmitManualEntryBracketAt("MNQ", "open_long", 90, 130, t1)
	if !refused || !strings.Contains(refusal, "daily_loss") {
		t.Fatalf("a new entry after the breach must be REFUSED by the daily-loss gate, got refused=%v %q", refused, refusal)
	}
	if n := dlGateBlocks(f.at.id)["daily_loss"]; n < 1 {
		t.Fatalf("the refusal must be counted under daily_loss, got %v", dlGateBlocks(f.at.id))
	}
	// … the arm path …
	if r, ok := f.at.admitEntry(admitIntent{Path: admitArm, Symbol: "MNQ", Action: "open_long", Now: t1, Key: "dl-arm"}); !ok || !strings.Contains(r, "daily_loss") {
		t.Fatalf("an arm placement after the breach must be REFUSED by the daily-loss gate, got refused=%v %q", ok, r)
	}
	// … and the mentor's authoring gate.
	if refuse, why := f.at.mentorDailyLossGate(t1); !refuse || !strings.Contains(why, "daily_loss") {
		t.Fatalf("a mentor placement after the breach must be REFUSED, got refuse=%v %q", refuse, why)
	}

	// LATCH: the position is flattened at a BETTER price — the day's bot P&L
	// is now -70, inside the limit. The gate still refuses until 17:00 CT.
	if err := f.st.GormDB().Model(&store.TraderPosition{}).Where("trader_id = ? AND status = ?", f.at.id, "OPEN").
		Updates(map[string]any{"status": "CLOSED", "exit_price": 95.0, "realized_pnl": -10.0, "pnl_corrected": -10.0,
			"close_reason": "sync", "exit_time": t1.Add(time.Minute).UnixMilli()}).Error; err != nil {
		t.Fatal(err)
	}
	ct := kernel.CTLocation()
	t1659 := time.Date(2026, time.September, 11, 16, 59, 0, 0, ct)
	if b, err := f.at.botDayPnLAt(t1659); err != nil || b.Total() <= -100 {
		t.Fatalf("fixture: the recovered day must sit inside the limit (bot P&L %.2f, err %v)", b.Total(), err)
	}
	if v := f.at.dailyGuardVerdictAt(t1659); !v.Refuse || !v.Latched {
		t.Fatalf("the latch must HOLD until 17:00 CT even after the P&L recovers, got %+v", v)
	}
	// RELEASED at 17:00 CT (the next CME session-day).
	t1700 := time.Date(2026, time.September, 11, 17, 0, 0, 0, ct)
	if r := kernel.DailyForceFlatReasonAt(f.at.id, t1700); r != "" {
		t.Fatalf("the latch must lift at 17:00 CT, still %q", r)
	}
	if v := f.at.dailyGuardVerdictAt(t1700); v.Refuse {
		t.Fatalf("after the 17:00 CT roll the gate must admit (a new session-day), got %+v", v)
	}
}

// 2 — GATE OFF (the owner's live values: guardrails_enabled=false,
// daily_loss_enabled=false, $450): a loss far past $450 changes NOTHING — no
// latch, no cancel, no flatten, no daily refusal.
func TestDailyLossGateOffChangesNothing(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	off := false
	f := withdrawFixture(t, "dl-off", func(c *store.StrategyConfig) {
		c.RiskControl.GuardrailsEnabled = &off
		c.RiskControl.DailyLossEnabled = &off
		c.RiskControl.DailyLossLimitUSD = 450
	})
	row, sid := f.placeResting(t)
	t1 := f.now.Add(time.Minute)
	dlSeedClosed(t, f.st, f.at.id, f.now.Add(-30*time.Minute), -2000, "")
	dlSeedOpen(t, f.st, f.at.id, f.now.Add(-10*time.Minute), 100)
	f.w.srv.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "long", Quantity: 1, AvgPrice: 100}})
	f.at.peakPnLCache = map[string]float64{}
	dlMark(t, 10)
	f.setLiveBook(t1, wdEntryOrder(sid))

	f.at.monitorTick(t1)
	f.w.flush(t)

	if r := kernel.DailyForceFlatReasonAt(f.at.id, t1); r != "" {
		t.Fatalf("gate OFF must never latch, got %q", r)
	}
	if n := f.w.count(ntwire.FrameCancelOrder, sid); n != 0 {
		t.Fatalf("gate OFF must not cancel the resting entry (%d cancel frames)", n)
	}
	if n := dlClosePositionFrames(f.w); n != 0 {
		t.Fatalf("gate OFF must not flatten (%d close_position frames)", n)
	}
	if got := f.rowByID(t, row.ID); got.State != store.StateWorking {
		t.Fatalf("gate OFF must leave the resting entry working, got %q", got.State)
	}
	if r, refused := f.at.AdmitManualEntryBracketAt("MNQ", "open_long", 90, 130, t1); refused && strings.Contains(r, "daily_") {
		t.Fatalf("gate OFF must not refuse on a daily guardrail, got %q", r)
	}
	if v := f.at.dailyGuardVerdictAt(t1); v.Refuse {
		t.Fatalf("gate OFF: the verdict must be inert, got %+v", v)
	}
	for _, g := range []string{"daily_loss", "daily_loss_tripped", "daily_guardrail_unverified"} {
		if n := dlGateBlocks(f.at.id)[g]; n != 0 {
			t.Fatalf("gate OFF must count nothing under %s, got %d", g, n)
		}
	}
	if refuse, why := f.at.mentorDailyLossGate(t1); refuse {
		t.Fatalf("gate OFF: the mentor gate must allow, got %q", why)
	}
}

// 3 — MANUAL TRADES DO NOT COUNT. The 10-08 shape: a manual 20-lot −1,127.50
// (reconcile-adopted, origin='manual') and a bot loss −31.50 against
// a $450 limit. The kernel's realized input sees −1,159.00; the daily-loss leg
// (decision path AND every other path) sees the bot's −31.50 and does not
// trip. MUTANT: count the manual rows again (add BotRealizedPnL's excluded
// manual P&L back into the realized half) → the beat latches → RED.
func TestDailyLossManualTradesDoNotCount(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	f := withdrawFixture(t, "dl-manual", func(c *store.StrategyConfig) { dlOn(c); c.RiskControl.DailyLossLimitUSD = 450 })
	t1 := f.now.Add(time.Minute)
	dlSeedClosed(t, f.st, f.at.id, f.now.Add(-40*time.Minute), -1127.50, store.OriginManual)
	dlSeedClosed(t, f.st, f.at.id, f.now.Add(-30*time.Minute), -31.50, "")

	f.at.monitorTick(t1)
	f.w.flush(t)
	if r := kernel.DailyForceFlatReasonAt(f.at.id, t1); r != "" {
		t.Fatalf("a manual loss must not trip the bot's daily-loss gate, latched: %q", r)
	}
	if n := dlClosePositionFrames(f.w); n != 0 {
		t.Fatalf("no flatten on a manual loss (%d close frames)", n)
	}
	if v := f.at.dailyGuardVerdictAt(t1); v.Refuse {
		t.Fatalf("the verdict must admit (bot P&L -31.50 vs -450), got %+v", v)
	}
	b, err := f.at.botDayPnLAt(t1)
	if err != nil || b.Total() != -31.50 || b.ManualExcluded != 1 {
		t.Fatalf("the one reader must sum the bot's -31.50 and COUNT the manual row it excluded, got %+v err %v", b, err)
	}

	// The decision path's kernel input, from its production call site
	// (attachTradeContext reads the WALL clock, so the same two trades are
	// seeded again on today's real session-day).
	wall := time.Now().Add(-time.Second)
	dlSeedClosed(t, f.st, f.at.id, wall, -1127.50, store.OriginManual)
	dlSeedClosed(t, f.st, f.at.id, wall, -31.50, "")
	ctx := &kernel.Context{TraderID: f.at.id}
	f.at.attachTradeContext(ctx)
	if !ctx.DailyLossPnLKnown || ctx.DailyLossPnL != -31.50 {
		t.Fatalf("the decision path's loss leg must judge the bot's own -31.50, got known=%v pnl=%.2f", ctx.DailyLossPnLKnown, ctx.DailyLossPnL)
	}
	g := kernel.DailyGuardrails{MasterEnabled: true, DailyRealizedPnL: ctx.DailyRealizedPnL,
		DailyLossEnabled: true, DailyLossLimitUSD: 450, DailyLossPnL: ctx.DailyLossPnL, DailyLossPnLKnown: ctx.DailyLossPnLKnown}
	if d, err := g.Check(); err != nil {
		t.Fatalf("the kernel loss leg tripped on a manual loss (decision %v): %v", d, err)
	}
}

// 4 — MAX DAILY TRADES ON THE MENTOR PATH (R5 bug fix: the owner's switch was
// never evaluated for a mentor trader). Two entries today against max 2 →
// the mentor's placement is refused, counted, and filed in the gate-block
// table; the send-time admission of a mentor arm refuses too. One entry →
// the placement proceeds. MUTANT: drop the max-trades refusal in the verdict
// → the recorder fires → RED.
func TestMaxDailyTradesEnforcedOnMentorPath(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	withMaintenanceDir(t)
	on := true
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{
		MentorMode: true, GuardrailsEnabled: &on, MaxDailyTradesEnabled: &on, MaxDailyTrades: 2}})
	at.id = "dl-mentor-max"
	kernel.ClearDailyForceFlat(at.id)
	t.Cleanup(func() { kernel.ClearDailyForceFlat(at.id) })
	wireMentorPlacementSeams(t, at)
	ct := kernel.CTLocation()
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, ct)
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })
	var placed int
	mentorPlaceRecorderForTest = func(mentor.Intent, int) { placed++ }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })
	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	choice := mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}

	dlSeedClosed(t, st, at.id, now.Add(-40*time.Minute), 20, "")
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 1 {
		t.Fatalf("one entry today against max 2 must proceed, placed=%d", placed)
	}
	dlSeedClosed(t, st, at.id, now.Add(-20*time.Minute), 20, "")
	at.mentorPlaceIntent(in, choice, 1000, 1100)
	if placed != 1 {
		t.Fatalf("two entries today against max 2 must REFUSE the mentor placement, placed=%d", placed)
	}
	if n := at.mentorCountSnapshot()["max_daily_trades_refused"]; n != 1 {
		t.Fatalf("the refusal must be counted max_daily_trades_refused once, got %d", n)
	}
	if n := dlGateBlocks(at.id)["mentor_max_daily_trades"]; n != 1 {
		t.Fatalf("the refusal must reach the gate-block table as mentor_max_daily_trades, got %v", dlGateBlocks(at.id))
	}
	// The send point: a mentor arm reaching the admission chain.
	r, refused := at.admitEntry(admitIntent{Path: admitArm, MentorArm: true, Symbol: "MNQ", Action: "open_long", Now: now, Key: "m1"})
	if !refused || !strings.Contains(r, "max_daily_trades") {
		t.Fatalf("a mentor arm at the send point must be refused by max daily trades, got refused=%v %q", refused, r)
	}
}

// 5 — MENTOR REFUSALS REACH THE GATE-BLOCK TABLE (P2-3). A window refusal at
// the placement call site is filed as mentor_window; before r22 only
// mentor_ai_entries_off/mentor_ai_closes_off ever reached the table.
func TestMentorPlacementRefusalReachesGateBlocks(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.id = "dl-mentor-window"
	wireMentorPlacementSeams(t, at)
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 10, 0, 0, 0, kernel.CTLocation()) }
	t.Cleanup(func() { mentorNowSource = nil })
	mentorPlaceRecorderForTest = func(mentor.Intent, int) {}
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })
	in := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	at.mentorPlaceIntent(in, mentorSizeChoice{Contracts: 5, Tier: "base"}, 1000, 1100)
	if n := dlGateBlocks(at.id)["mentor_window"]; n != 1 {
		t.Fatalf("a mentor window refusal must be filed once as mentor_window, got %v", dlGateBlocks(at.id))
	}
}

// 6 — THE LATCH SURVIVES A RESTART. A trip is persisted; a NEW trader
// instance on the same store (the process restarted: the in-memory latch is
// gone) restores it for the rest of the session-day — even though the day's
// P&L, read again, would no longer trip — and ignores it on the next day.
func TestDailyLossLatchSurvivesRestartUntil1700(t *testing.T) {
	cfg := store.StrategyConfig{RiskControl: store.RiskControlConfig{DailyLossLimitUSD: 100}}
	dlOn(&cfg)
	at, st := resetTrader(t, cfg)
	at.id = "dl-restart"
	kernel.ClearDailyForceFlat(at.id)
	t.Cleanup(func() { kernel.ClearDailyForceFlat(at.id) })
	now := dailyLossNow()
	dlSeedClosed(t, st, at.id, now.Add(-5*time.Minute), -150, "")
	if v := at.dailyGuardVerdictAt(now); !v.Refuse || !v.Latched {
		t.Fatalf("fixture: -150 vs $100 must trip, got %+v", v)
	}
	// The loss row is corrected to inside the limit; then the process restarts.
	if err := st.GormDB().Model(&store.TraderPosition{}).Where("trader_id = ?", at.id).Update("pnl_corrected", -10.0).Error; err != nil {
		t.Fatal(err)
	}
	kernel.ClearDailyForceFlat(at.id) // the in-memory latch dies with the process
	restarted := &AutoTrader{id: at.id, store: st, exchange: "ninjatrader"}
	restarted.config.StrategyConfig = &cfg
	if v := restarted.dailyGuardVerdictAt(now.Add(time.Hour)); !v.Refuse || !v.Latched || !strings.Contains(v.Reason, "restored after restart") {
		t.Fatalf("a restart must not lift today's latch, got %+v", v)
	}
	// The next CME session-day: the persisted latch is from an earlier day.
	kernel.ClearDailyForceFlat(at.id)
	nextDay := &AutoTrader{id: at.id, store: st, exchange: "ninjatrader"}
	nextDay.config.StrategyConfig = &cfg
	if v := nextDay.dailyGuardVerdictAt(time.Date(2026, 10, 2, 17, 5, 0, 0, kernel.CTLocation())); v.Refuse {
		t.Fatalf("yesterday's latch must not restore on a new session-day, got %+v", v)
	}
}

// 7 — EMERGENCY FLAT ON TRADER A CLEARS ONLY A's LATCH (T4), at the trader
// method the handler calls. Trader B's latch (and its persisted copy) stand.
func TestClearDailyLossLatchIsScopedToOneTrader(t *testing.T) {
	cfg := store.StrategyConfig{RiskControl: store.RiskControlConfig{DailyLossLimitUSD: 100}}
	a, _ := resetTrader(t, cfg)
	a.id = "dl-scope-A"
	b, _ := resetTrader(t, cfg)
	b.id = "dl-scope-B"
	now := dailyLossNow()
	for _, x := range []*AutoTrader{a, b} {
		kernel.SetDailyForceFlatAt(x.id, "daily loss limit hit (test)", now)
		id := x.id
		t.Cleanup(func() { kernel.ClearDailyForceFlat(id) })
	}
	a.ClearDailyLossLatch("Emergency Flat (test)")
	if r := kernel.DailyForceFlatReasonAt(a.id, now); r != "" {
		t.Fatalf("A's latch must be cleared, still %q", r)
	}
	if r := kernel.DailyForceFlatReasonAt(b.id, now); r == "" {
		t.Fatal("an Emergency Flat on A must leave B's daily-loss latch intact")
	}
}

// 8 — THE OPEN HALF COUNTS A RUNNER'S CLOSED LEGS. The #651 shape: SHORT 3,
// leg 1 (2 contracts) closed by its TP while the runner (1) stays OPEN. The
// store keeps the residual in Quantity and the legs' average exit in
// ExitPrice; the reader re-derives the legs' P&L from those prices (never the
// raw realized_pnl column) and marks the runner. MUTANT: drop the closed-legs
// term → Open = the runner only → RED.
func TestBotDayPnLCountsClosedLegsOfAnOpenRunner(t *testing.T) {
	cfg := store.StrategyConfig{RiskControl: store.RiskControlConfig{DailyLossLimitUSD: 100}}
	at, st := resetTrader(t, cfg)
	at.id = "dl-runner"
	now := dailyLossNow()
	p := &store.TraderPosition{TraderID: at.id, Account: "Sim101", Symbol: "MNQ", Side: "SHORT",
		EntryQuantity: 3, Quantity: 1, EntryPrice: 30742.25, ExitPrice: 30724.00, Status: "OPEN",
		EntryTime: now.Add(-10 * time.Minute).UnixMilli(), CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli()}
	if err := st.GormDB().Create(p).Error; err != nil {
		t.Fatal(err)
	}
	dlMark(t, 30730.00)
	b, err := at.botDayPnLAt(now)
	if err != nil {
		t.Fatal(err)
	}
	// legs: -(30724.00-30742.25)×2×$2 = +73.00 ; runner: -(30730.00-30742.25)×1×$2 = +24.50
	if b.OpenPositions != 1 || b.Open != 97.50 {
		t.Fatalf("open P&L must be legs +73.00 + runner +24.50 = +97.50, got %+v", b)
	}
}

// 9 — OWNER RULING R4 + R5 (CTO fold 2026-10-14): A STRATEGY THAT NEVER SAVED
// THE SWITCHES IS OFF. No guardrail keys at all (the JSON a new strategy
// carries) and a loss far past the env $500 fallback: the 60s beat latches
// nothing, cancels nothing, flattens nothing, the verdict is inert, and the
// boot line says OFF (default). Before the fold the absent keys inherited
// master ON + daily-loss ON with the env $500 — with r22's flatten that would
// close positions on a strategy whose owner never turned anything on.
// MUTANT: restore the `true` default in kernel.ResolveStrategyGuardrails →
// the beat latches → RED.
func TestDailyLossGateAbsentKeysIsOffByDefault(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	f := withdrawFixture(t, "dl-absent", nil) // no guardrail keys at all
	rc := f.at.config.StrategyConfig.RiskControl
	if rc.GuardrailsEnabled != nil || rc.DailyLossEnabled != nil || rc.DailyLossLimitUSD != 0 {
		t.Fatalf("fixture: the strategy must carry NO guardrail keys, got %+v", rc)
	}
	row, sid := f.placeResting(t)
	t1 := f.now.Add(time.Minute)
	dlSeedClosed(t, f.st, f.at.id, f.now.Add(-30*time.Minute), -2000, "")
	dlSeedOpen(t, f.st, f.at.id, f.now.Add(-10*time.Minute), 100)
	f.w.srv.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "long", Quantity: 1, AvgPrice: 100}})
	f.at.peakPnLCache = map[string]float64{}
	dlMark(t, 10)
	f.setLiveBook(t1, wdEntryOrder(sid))

	f.at.monitorTick(t1)
	f.w.flush(t)

	if r := kernel.DailyForceFlatReasonAt(f.at.id, t1); r != "" {
		t.Fatalf("absent keys must read OFF: a -2000 day must not latch, got %q", r)
	}
	if n := f.w.count(ntwire.FrameCancelOrder, sid); n != 0 {
		t.Fatalf("absent keys must not cancel the resting entry (%d cancel frames)", n)
	}
	if n := dlClosePositionFrames(f.w); n != 0 {
		t.Fatalf("absent keys must not flatten (%d close_position frames)", n)
	}
	if got := f.rowByID(t, row.ID); got.State != store.StateWorking {
		t.Fatalf("absent keys must leave the resting entry working, got %q", got.State)
	}
	if v := f.at.dailyGuardVerdictAt(t1); v.Refuse {
		t.Fatalf("absent keys: the verdict must be inert, got %+v", v)
	}
	if _, _, enforced := f.at.deskGuardrail(); enforced {
		t.Fatal("absent keys: the desk strip must not call the limit ENFORCED")
	}
	line := DailyLossGateBootLineForStrategy(f.at.config.StrategyConfig)
	if !strings.Contains(line, "daily-loss gate: OFF (default)") || !strings.Contains(line, "master off (default)") {
		t.Fatalf("the boot line must say OFF (default) for a strategy that never saved the switches, got %q", line)
	}
	// The SAVED-off live shape reads as the owner's choice, not a default.
	off := false
	saved := store.StrategyConfig{RiskControl: store.RiskControlConfig{GuardrailsEnabled: &off, DailyLossEnabled: &off, DailyLossLimitUSD: 450}}
	if l := DailyLossGateBootLineForStrategy(&saved); !strings.Contains(l, "daily-loss gate: OFF (master off (saved), daily_loss_enabled off (saved), limit $450 (studio))") {
		t.Fatalf("the live strategies' saved OFF must read as saved, got %q", l)
	}
}
