package trader

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"vl/kernel"
	"vl/logger"
	"vl/store"
	"vl/telemetry"
)

// ── release #24 RISK-TRUTH — the daily-loss gate says what it does ──────────
//
// RT-1: whichever side latches first (the AI decision cycle's kernel guardrail
// or the 60 s beat's verdict), the trip is announced and counted EXACTLY ONCE
// per (trader, CME session-day).
// RT-2: a latch outlives switching the gate OFF (fail-closed default, owner
// decision pending), so every surface that describes the gate says LATCHED.

// rtLogHook captures the log lines that name one trader (logrus has no
// per-hook removal; the cleanup restores the previous hook set).
type rtLogHook struct {
	mu    sync.Mutex
	tag   string
	lines []string
}

func (h *rtLogHook) Levels() []logrus.Level { return logrus.AllLevels }
func (h *rtLogHook) Fire(e *logrus.Entry) error {
	if h.tag != "" && !strings.Contains(e.Message, h.tag) {
		return nil
	}
	h.mu.Lock()
	h.lines = append(h.lines, e.Message)
	h.mu.Unlock()
	return nil
}

func (h *rtLogHook) count(sub string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, l := range h.lines {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

func (h *rtLogHook) all() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.lines, "\n")
}

func rtCaptureLog(t *testing.T, tag string) *rtLogHook {
	t.Helper()
	h := &rtLogHook{tag: tag}
	prev := logrus.LevelHooks{}
	for lv, hs := range logger.Log.Hooks {
		prev[lv] = append([]logrus.Hook(nil), hs...)
	}
	logger.Log.AddHook(h)
	t.Cleanup(func() { logger.Log.ReplaceHooks(prev) })
	return h
}

// RT-1 — THE KERNEL LATCHES FIRST, THE TRIP IS STILL ANNOUNCED ONCE.
//
// The decision path, from its production call sites: attachTradeContext fills
// the loss leg from the one P&L reader, kernel.GetFullDecisionWithStrategy runs
// the Strategy Studio guardrail (engine_analysis.go) and publishes
// SetDailyForceFlat BEFORE any 60 s beat has evaluated the gate. Then the beat
// runs twice. Exactly one "🔴 DAILY LOSS LIMIT HIT" line and one
// daily_loss_tripped count; the second beat adds nothing.
//
// The kernel path reads the WALL clock (MaybeResetDaily / SetDailyForceFlat),
// so the whole test runs on time.Now().
//
// MUTANT: drop the announce from the latch branch of dailyGuardVerdictAt →
// zero lines, zero counts → RED.
func TestDailyLossKernelFirstTripIsAnnouncedAndCountedOnce(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	cfg := store.StrategyConfig{RiskControl: store.RiskControlConfig{DailyLossLimitUSD: 100}}
	dlOn(&cfg)
	at, st := resetTrader(t, cfg)
	at.id = "rt1-kernel-first"
	at.name = "rt1"
	kernel.ClearDailyForceFlat(at.id)
	t.Cleanup(func() { kernel.ClearDailyForceFlat(at.id) })
	hook := rtCaptureLog(t, "")

	now := time.Now()
	dlSeedClosed(t, st, at.id, now.Add(-2*time.Second), -150, "")

	ctx := &kernel.Context{TraderID: at.id}
	at.attachTradeContext(ctx)
	if !ctx.DailyLossPnLKnown || ctx.DailyLossPnL != -150 {
		t.Fatalf("fixture: the decision path must judge the bot's -150, got known=%v pnl=%.2f", ctx.DailyLossPnLKnown, ctx.DailyLossPnL)
	}
	if _, err := kernel.GetFullDecisionWithStrategy(ctx, nil, kernel.NewStrategyEngine(&cfg), ""); err != nil {
		t.Fatalf("the guardrail HOLD returns no error, got %v", err)
	}
	if kernel.DailyForceFlatReason(at.id) == "" {
		t.Fatal("fixture: the AI decision cycle's guardrail must latch the kernel first")
	}
	if n := hook.count("DAILY LOSS LIMIT HIT"); n != 0 {
		t.Fatalf("fixture: nothing announced before the beat, got %d lines", n)
	}

	at.dailyLossBeatAt(time.Now())

	if n := hook.count("🔴 DAILY LOSS LIMIT HIT"); n != 1 {
		t.Fatalf("a kernel-first trip must be announced exactly once by the beat — got %d '🔴 DAILY LOSS LIMIT HIT' line(s):\n%s", n, hook.all())
	}
	if n := dlGateBlocks(at.id)["daily_loss_tripped"]; n != 1 {
		t.Fatalf("a kernel-first trip must be counted once under daily_loss_tripped, got %d (%v)", n, dlGateBlocks(at.id))
	}

	// A second beat, and an entry attempt, add nothing.
	at.dailyLossBeatAt(time.Now().Add(time.Second))
	if v := at.dailyGuardVerdictAt(time.Now().Add(2 * time.Second)); !v.Refuse || !v.Latched {
		t.Fatalf("the latch must still refuse, got %+v", v)
	}
	if n := hook.count("🔴 DAILY LOSS LIMIT HIT"); n != 1 {
		t.Fatalf("a second beat is not a new trip — want 1 line, got %d", n)
	}
	if n := dlGateBlocks(at.id)["daily_loss_tripped"]; n != 1 {
		t.Fatalf("a second beat is not a new trip — daily_loss_tripped must stay 1, got %d", n)
	}
	// The persisted copy is written, so a restart cannot lift it.
	if raw, _ := st.GetSystemConfig(dailyLossLatchKey(at.id)); !strings.HasPrefix(raw, kernel.CMESessionDayKey(time.Now())+"|") {
		t.Fatalf("the kernel-first latch must be persisted for today's session-day, got %q", raw)
	}
}

// RT-1 (other order) — the beat's own trip announces once, and the kernel
// guardrail tripping afterwards on the same session-day adds nothing.
func TestDailyLossBeatFirstThenKernelAnnouncesOnce(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	cfg := store.StrategyConfig{RiskControl: store.RiskControlConfig{DailyLossLimitUSD: 100}}
	dlOn(&cfg)
	at, st := resetTrader(t, cfg)
	at.id = "rt1-beat-first"
	kernel.ClearDailyForceFlat(at.id)
	t.Cleanup(func() { kernel.ClearDailyForceFlat(at.id) })
	hook := rtCaptureLog(t, "")
	now := time.Now()
	dlSeedClosed(t, st, at.id, now.Add(-2*time.Second), -150, "")

	at.dailyLossBeatAt(now)
	ctx := &kernel.Context{TraderID: at.id}
	at.attachTradeContext(ctx)
	if _, err := kernel.GetFullDecisionWithStrategy(ctx, nil, kernel.NewStrategyEngine(&cfg), ""); err != nil {
		t.Fatal(err)
	}
	at.dailyLossBeatAt(time.Now().Add(time.Second))
	if n := hook.count("🔴 DAILY LOSS LIMIT HIT"); n != 1 {
		t.Fatalf("want exactly one announcement, got %d:\n%s", n, hook.all())
	}
	if n := dlGateBlocks(at.id)["daily_loss_tripped"]; n != 1 {
		t.Fatalf("want daily_loss_tripped 1, got %d", n)
	}
}

// RT-3 — A SYNTHETIC UNRESOLVED BOT CLOSE TODAY reaches the gate's existing
// unresolved handling (it used to be filtered out of BotRealizedPnL unseen):
// with the loss leg ON the verdict refuses fail-closed as Unverified (the
// mentor's authoring gate refuses on it; the admission chain does not — the
// handling since release 10-05-1, kept), and the gate's P&L read COUNTS it.
func TestDailyLossUnknownReasonCloseTodayIsUnresolved(t *testing.T) {
	cfg := store.StrategyConfig{RiskControl: store.RiskControlConfig{DailyLossLimitUSD: 450}}
	dlOn(&cfg)
	at, st := resetTrader(t, cfg)
	at.id = "rt3-unresolved"
	kernel.ClearDailyForceFlat(at.id)
	t.Cleanup(func() { kernel.ClearDailyForceFlat(at.id) })
	now := dailyLossNow()
	dlSeedClosed(t, st, at.id, now.Add(-30*time.Minute), -20, "")
	exit := now.Add(-10 * time.Minute)
	row := &store.TraderPosition{TraderID: at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryQuantity: 1, EntryPrice: 100, RealizedPnL: -500, Status: "CLOSED",
		CloseReason: store.CloseReasonUnresolved, // class 27: the exit fill is unknown
		EntryTime: exit.Add(-5 * time.Minute).UnixMilli(), ExitTime: exit.UnixMilli(),
		CreatedAt: exit.UnixMilli(), UpdatedAt: exit.UnixMilli()}
	if err := st.GormDB().Create(row).Error; err != nil {
		t.Fatal(err)
	}
	b, err := at.botDayPnLAt(now)
	if err != nil || b.Unresolved != 1 || b.Realized != -20 || b.Closed != 1 {
		t.Fatalf("the gate's P&L read must count the unresolved close (1) and sum only the known -20, got %+v err %v", b, err)
	}
	v := at.dailyGuardVerdictAt(now)
	if !v.Refuse || !v.Unverified || v.Class != "daily_guardrail_unverified" || v.Latched {
		t.Fatalf("an unresolved close today must refuse fail-closed as Unverified (never latch, never flatten), got %+v", v)
	}
	if refuse, why := at.mentorDailyLossGate(now); !refuse || !strings.Contains(why, "unresolved") {
		t.Fatalf("the mentor's authoring gate must refuse on the unresolved close, got refuse=%v %q", refuse, why)
	}
}

// RT-4 — THE AI TRACK RECORD CARRIES THE MANUAL LINE. attachTradeContext (the
// decision path's production call site) reads GetFullStats; the manual rows it
// excluded ride into the context and onto the 🧾 log line as their own counted
// line, READ from the store — and the executor prompt renders it. MUTANT: drop
// the ManualExcludedLine assignment → the context and prompt lack it → RED.
func TestTrackRecordCarriesTheManualLine(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{Language: "en"})
	at.id = "rt4-track"
	at.name = "rt4"
	hook := rtCaptureLog(t, "[rt4]")
	wall := time.Now().Add(-time.Minute)
	dlSeedClosed(t, st, at.id, wall, -1127.50, store.OriginManual)
	dlSeedClosed(t, st, at.id, wall, -31.50, "")

	ctx := &kernel.Context{TraderID: at.id}
	at.attachTradeContext(ctx)
	if ctx.TradingStats == nil {
		t.Fatal("fixture: no track record built")
	}
	const want = "Manual (excluded): 1 trade, −$1,127.50"
	if ctx.TradingStats.ManualExcludedLine != want || ctx.TradingStats.TotalPnL != -31.50 {
		t.Fatalf("the AI context must carry the bot's -31.50 AND the manual line %q, got total %.2f line %q",
			want, ctx.TradingStats.TotalPnL, ctx.TradingStats.ManualExcludedLine)
	}
	if n := hook.count("Track record: -31.50 over 1 resolved trades"); n != 1 || hook.count(want) != 1 {
		t.Fatalf("the 🧾 track-record log line must carry the manual line once, got:\n%s", hook.all())
	}
	prompt := kernel.NewStrategyEngine(&store.StrategyConfig{Language: "en"}).BuildUserPrompt(ctx)
	if !strings.Contains(prompt, "over 1 resolved trades") || !strings.Contains(prompt, want) {
		t.Fatalf("the executor prompt's track record must render the manual line %q", want)
	}
}
