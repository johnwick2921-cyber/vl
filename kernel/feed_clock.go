package kernel

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"vl/logger"
	"vl/safe"
)

// ── STALE GATE ON THE MONOTONIC CLOCK (release #22 OPS-SAFETY) ──────────────
//
// THE DEFECT. B4's age contract (barIsStale) compares the newest bar's open,
// stamped by NT8 on the Windows clock, with THIS process's WALL clock. On
// 10-14 the WSL wall clock stepped back 125.0 s at 00:23:00 (then +124.4 s
// 4.9 s later) and took 124 smaller backward steps in one night. A backward
// step of S seconds makes every bar read S seconds fresher, so a dead feed
// read as live: the gate failed OPEN (verified at base: the same 200 s-dead
// feed is refused on the true clock and placed after a −125 s step).
//
// THE FIX, in two legs that only ever ADD refusals:
//
//  1. RECEIPT AGE (monotonic). The bar cache stamps every accepted live frame
//     with time.Now(), which carries Go's monotonic reading. time.Since on it
//     is immune to wall steps. A 1m feed whose last live frame is older than
//     2×interval + grace (135 s with the default 15 s grace — the most the
//     wall rule itself can ever allow) is stale, whatever the wall clock says.
//
//  2. CLOCK-STEP HOLD. A sampler compares wall-clock elapsed with monotonic
//     elapsed between two reads. Any difference at or above CLOCK_STEP_FREEZE_MS
//     (default 6000 ms — m134, was 2000) is a STEP: it is logged and counted, and NEW entries
//     are refused for CLOCK_STEP_HOLD_S (default 120 s) of MONOTONIC time after
//     the last such step. A step of 2–6 s is logged and counted only — EXCEPT
//     inside the CME open/close boundary (15:59–16:01 and 16:59–17:01 CT),
//     where any step >= 2 s still holds, because it can flip IsCMEOpen. A
//     smaller step (>= CLOCK_STEP_DETECT_MS, default 100 ms) is logged and
//     counted only: it cannot open the gate, because the receipt leg does not
//     read the wall clock at all.
//
// Exits and protection never consult the hold; protection reads only the
// FEED legs (FeedStaleAt), so a price is judged on whether frames arrive, not
// on whether the wall clock just moved.

// FeedLastLiveReceipt is wired once in production (trader/ninjatrader
// bars_market_bridge.go) to the NT8 bar cache's LastLiveReceipt. nil (tests,
// sandbox, no NT8) → the receipt leg is skipped, exactly as before.
var FeedLastLiveReceipt func(symbol, timeframe string) (time.Time, bool)

// receiptMaxAgeMs is the receipt-age limit for a timeframe: two intervals plus
// the delivery grace. The wall rule (barIsStale) trips at the latest when the
// newest bar opened 2 intervals before the current one, i.e. after at most
// 2×interval of silence — so this leg never refuses a feed the wall rule
// would pass on a correct clock; it only stops a stepped clock from hiding one.
func receiptMaxAgeMs(intervalMs int64) int64 {
	return 2*intervalMs + staleBarGraceMs()
}

// feedReceiptStale reports the receipt leg for (symbol, tf) at the monotonic
// instant at: (stale, ageMs, known). known=false when the provider is not
// wired or `at` is the zero time (no monotonic instant to measure from).
// A wired provider with NO live frame since boot is stale (fail-closed: a
// historical seed is a replay, not proof the feed is alive).
func feedReceiptStale(symbol, tf string, at time.Time) (stale bool, ageMs int64, known bool) {
	fn := FeedLastLiveReceipt
	iv := tfIntervalMs(tf)
	if fn == nil || at.IsZero() || iv <= 0 {
		return false, 0, false
	}
	recv, ok := fn(symbol, tf)
	if !ok {
		return true, -1, true
	}
	age := at.Sub(recv).Milliseconds() // monotonic when both carry it
	return age > receiptMaxAgeMs(iv), age, true
}

// ── the clock-step guard ────────────────────────────────────────────────────

const (
	defaultClockStepDetectMs = 100
	// m134: 6000 ms (was 2000). The 10-14 NTP micro-corrections were 1.0–1.2 s,
	// but the 10-21 00:44–04:00 steps were 2–5 s — all below a real step, and
	// every one refused a mentor arm for the full 120 s hold. A sub-6 s step is
	// now logged and counted, never held: the bar-age wall rule has 1 interval +
	// grace of slack and the monotonic receipt leg already fails a dead feed
	// closed, so the freeze only needs to catch a REAL clock jump (the 10-14
	// −125 s / +124 s pair) and the IsCMEOpen boundary crossing. The hold (120 s)
	// and the receipt leg are unchanged.
	defaultClockStepFreezeMs = 6000
	defaultClockStepHoldS    = 120
	clockStepSamplePeriod    = time.Second

	// m149: a step of 2–6 s inside the CME open/close boundary (15:59–16:01 and
	// 16:59–17:01 CT) can flip IsCMEOpen across 16:00/17:00, so it still holds
	// even though it is below the 6 s freeze threshold.
	sessionBoundaryFreezeMs = 2000
)

func envInt(name string, def int64) int64 {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

// ClockStepConfig is the guard's resolved config (READ, never literal).
type ClockStepConfig struct {
	DetectMs int64 `json:"detect_ms"`
	FreezeMs int64 `json:"freeze_ms"`
	HoldS    int64 `json:"hold_s"`
}

// LoadClockStepConfig resolves CLOCK_STEP_DETECT_MS / CLOCK_STEP_FREEZE_MS /
// CLOCK_STEP_HOLD_S. A freeze threshold below the detect floor is raised to it.
func LoadClockStepConfig() ClockStepConfig {
	c := ClockStepConfig{
		DetectMs: envInt("CLOCK_STEP_DETECT_MS", defaultClockStepDetectMs),
		FreezeMs: envInt("CLOCK_STEP_FREEZE_MS", defaultClockStepFreezeMs),
		HoldS:    envInt("CLOCK_STEP_HOLD_S", defaultClockStepHoldS),
	}
	if c.DetectMs <= 0 {
		c.DetectMs = defaultClockStepDetectMs
	}
	if c.FreezeMs < c.DetectMs {
		c.FreezeMs = c.DetectMs
	}
	return c
}

// procStart anchors the monotonic clock for the guard.
var procStart = time.Now()

// clockSample is THE clock read of the guard: the wall reading (Unix ns) and
// the monotonic reading (elapsed since procStart) of ONE time.Now(). A test
// replaces it to inject a stepped wall clock; production never does.
var clockSampleFn atomic.Value // func() (int64, time.Duration); swapped only by ClockSampleForTest

func realClockSample() (wallNs int64, mono time.Duration) {
	n := time.Now()
	return n.UnixNano(), n.Sub(procStart)
}

func init() { clockSampleFn.Store(realClockSample) }

// clockSample reads the current clock function atomically, so a test swap
// can never race the 1 s sampler goroutine.
func clockSample() (wallNs int64, mono time.Duration) {
	return clockSampleFn.Load().(func() (int64, time.Duration))()
}

// monoOf maps a time.Time from time.Now() onto the guard's monotonic axis.
// Callers pass instants that carry a monotonic reading (ctx.SnapshotAt,
// time.Now()); the zero time maps to "now".
func monoOf(t time.Time) time.Duration {
	if t.IsZero() {
		_, m := clockSample()
		return m
	}
	return t.Sub(procStart)
}

// ClockStepView is the guard's state as /api/health serves it. Every value is
// a counter or a reading; n/a fields are absent (omitempty), never zero-filled.
type ClockStepView struct {
	Config          ClockStepConfig `json:"config"`
	Samples         int64           `json:"samples"`
	StepsBackward   int64           `json:"steps_backward"`
	StepsForward    int64           `json:"steps_forward"`
	FreezeSteps     int64           `json:"freeze_steps"`
	LargestBackMs   int64           `json:"largest_backward_ms"`
	LargestFwdMs    int64           `json:"largest_forward_ms"`
	LastStepMs      *int64          `json:"last_step_ms,omitempty"`
	LastStepWallUTC string          `json:"last_step_wall_utc,omitempty"`
	LastStepAgoS    *int64          `json:"last_step_ago_s,omitempty"`
	EntryHoldActive bool            `json:"entry_hold_active"`
	EntryHoldLeftS  *int64          `json:"entry_hold_left_s,omitempty"`
	SamplerRunning  bool            `json:"sampler_running"`
}

type clockStepGuard struct {
	mu                 sync.Mutex
	have               bool
	prevWallNs         int64
	prevMono           time.Duration
	samples            int64
	back, fwd          int64
	freezeSteps        int64
	largestBack        int64 // ns, negative
	largestFwd         int64 // ns
	haveLast           bool
	lastStepNs         int64
	lastStepWall       int64
	lastStepMono       time.Duration
	haveFreeze         bool
	lastFreezeMon      time.Duration
	lastFreezeBoundary bool
	samplerOn          bool
}

var clockSteps = &clockStepGuard{}

// resetClockStepsForTest clears the process-wide guard (tests only).
func resetClockStepsForTest() {
	g := clockSteps
	g.mu.Lock()
	defer g.mu.Unlock()
	g.have, g.prevWallNs, g.prevMono, g.samples = false, 0, 0, 0
	g.back, g.fwd, g.freezeSteps, g.largestBack, g.largestFwd = 0, 0, 0, 0, 0
	g.haveLast, g.lastStepNs, g.lastStepWall, g.lastStepMono = false, 0, 0, 0
	g.haveFreeze, g.lastFreezeMon, g.lastFreezeBoundary = false, 0, false
}

// observe takes ONE sample under the lock (so two goroutines can never fold
// samples out of order and mint a phantom step pair) and returns the step it
// saw (ns; 0 when below the detect floor) and whether it was a freeze step.
func (g *clockStepGuard) observe(cfg ClockStepConfig) (stepNs int64, freeze bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	wallNs, mono := clockSample()
	g.samples++
	if !g.have {
		g.have, g.prevWallNs, g.prevMono = true, wallNs, mono
		return 0, false
	}
	step := (wallNs - g.prevWallNs) - int64(mono-g.prevMono)
	g.prevWallNs, g.prevMono = wallNs, mono
	abs := step
	if abs < 0 {
		abs = -abs
	}
	if abs < cfg.DetectMs*int64(time.Millisecond) {
		return 0, false
	}
	if step < 0 {
		g.back++
		if step < g.largestBack {
			g.largestBack = step
		}
	} else {
		g.fwd++
		if step > g.largestFwd {
			g.largestFwd = step
		}
	}
	g.haveLast, g.lastStepNs, g.lastStepWall, g.lastStepMono = true, step, wallNs, mono
	freezeMs := cfg.FreezeMs * int64(time.Millisecond)
	boundaryMs := sessionBoundaryFreezeMs * int64(time.Millisecond)
	boundary := abs >= boundaryMs && abs < freezeMs && stepIsSessionBoundary(wallNs)
	if abs >= freezeMs || boundary {
		g.freezeSteps++
		g.haveFreeze, g.lastFreezeMon, g.lastFreezeBoundary = true, mono, boundary
		return step, true
	}
	return step, false
}

// stepIsSessionBoundary reports whether a wall instant sits inside a window
// where a 2–6 s wall step can flip IsCMEOpen: ±60 s around the daily break
// (16:00 close / 17:00 reopen CT) and, on a shortened session day, ±60 s
// around the session's early close (e.g. 12:15 CT on 11-27 / 12-24). The
// post-step wall reading is what we judge, so a step that landed inside the
// window is the one that could have crossed the boundary.
func stepIsSessionBoundary(wallNs int64) bool {
	ct := time.Unix(0, wallNs).In(CTLocation())
	sec := ct.Hour()*3600 + ct.Minute()*60 + ct.Second()
	in := func(a, b int) bool { return sec >= a && sec <= b }
	if in(15*3600+59*60, 16*3600+1*60) || in(16*3600+59*60, 17*3600+1*60) {
		return true
	}
	// A shortened session (early close) is the OTHER instant IsCMEOpen flips —
	// not just 16:00/17:00. The close is resolved through the session calendar
	// (SessionStateAt), never a hard-coded time.
	if st := SessionStateAt(ct); st.Class == SessionShortened && st.HasClose {
		close := st.Close.In(CTLocation())
		closeSec := close.Hour()*3600 + close.Minute()*60 + close.Second()
		if sec >= closeSec-60 && sec <= closeSec+60 {
			return true
		}
	}
	return false
}

// ObserveClock takes one sample and logs any step it sees (every step at or
// above the detect floor is logged and counted: WARN when it freezes entries,
// INFO otherwise). Safe to call from any goroutine; the gates call it on every
// evaluation so a step between two sampler ticks is still caught.
func ObserveClock() {
	cfg := LoadClockStepConfig()
	step, freeze := clockSteps.observe(cfg)
	if step == 0 {
		return
	}
	dir := "BACKWARD"
	if step > 0 {
		dir = "FORWARD"
	}
	back, fwd := clockSteps.counts()
	if freeze {
		logger.Warnf("🕰 clock step %s %+dms (wall vs monotonic) — NEW entries held for %ds of monotonic time; exits and protection unaffected (steps since boot: %d back / %d fwd)",
			dir, step/int64(time.Millisecond), cfg.HoldS, back, fwd)
		return
	}
	logger.Infof("🕰 clock step %s %+dms (wall vs monotonic) — below the %dms freeze threshold, counted only (steps since boot: %d back / %d fwd)",
		dir, step/int64(time.Millisecond), cfg.FreezeMs, back, fwd)
}

func (g *clockStepGuard) counts() (back, fwd int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.back, g.fwd
}

// clockStepHeldAt reports whether a FREEZE step was observed within the hold
// window before the monotonic instant at, or at any time after it (a step
// that landed while a slow AI call was in flight still holds that call's
// snapshot). Returns the refusal text.
func clockStepHeldAt(at time.Time) (bool, string) {
	ObserveClock()
	cfg := LoadClockStepConfig()
	atMono := monoOf(at)
	g := clockSteps
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.haveFreeze {
		return false, ""
	}
	hold := time.Duration(cfg.HoldS) * time.Second
	if g.lastFreezeMon+hold <= atMono {
		return false, ""
	}
	_, nowMono := clockSample()
	left := (g.lastFreezeMon + hold - nowMono).Round(time.Second)
	if left < 0 {
		left = 0
	}
	threshold := cfg.FreezeMs
	boundaryNote := ""
	if g.lastFreezeBoundary {
		threshold = sessionBoundaryFreezeMs
		boundaryNote = " (session boundary)"
	}
	return true, fmt.Sprintf("clock_step: wall clock stepped %+dms vs monotonic; entries held %s more (hold %ds after any step >= %dms%s)",
		g.lastStepNs/int64(time.Millisecond), left, cfg.HoldS, threshold, boundaryNote)
}

// ClockStepSnapshot is the guard's state for /api/health and the clock lines.
func ClockStepSnapshot() ClockStepView {
	ObserveClock()
	cfg := LoadClockStepConfig()
	_, nowMono := clockSample()
	g := clockSteps
	g.mu.Lock()
	defer g.mu.Unlock()
	v := ClockStepView{
		Config: cfg, Samples: g.samples, StepsBackward: g.back, StepsForward: g.fwd,
		FreezeSteps: g.freezeSteps, LargestBackMs: g.largestBack / int64(time.Millisecond),
		LargestFwdMs: g.largestFwd / int64(time.Millisecond), SamplerRunning: g.samplerOn,
	}
	if g.haveLast {
		ms := g.lastStepNs / int64(time.Millisecond)
		ago := int64((nowMono - g.lastStepMono) / time.Second)
		v.LastStepMs, v.LastStepAgoS = &ms, &ago
		v.LastStepWallUTC = time.Unix(0, g.lastStepWall).UTC().Format(time.RFC3339)
	}
	if g.haveFreeze {
		if left := g.lastFreezeMon + time.Duration(cfg.HoldS)*time.Second - nowMono; left > 0 {
			s := int64((left + time.Second - 1) / time.Second)
			v.EntryHoldActive, v.EntryHoldLeftS = true, &s
		}
	}
	return v
}

// ClockStepSummary is the one-line form for the clock-health log lines.
func ClockStepSummary() string {
	v := ClockStepSnapshot()
	last := "n/a"
	if v.LastStepMs != nil {
		last = fmt.Sprintf("%+dms@%s", *v.LastStepMs, v.LastStepWallUTC)
	}
	return fmt.Sprintf("steps{back=%d fwd=%d freeze=%d last=%s hold=%t}", v.StepsBackward, v.StepsForward, v.FreezeSteps, last, v.EntryHoldActive)
}

var clockSamplerOnce sync.Once

// StartClockStepSampler starts the 1 s background sampler once per process.
// It exists because a step and its correction can both land between two gate
// evaluations (10-14: −125.0 s then +124.4 s, 4.9 s apart); point samples a
// minute apart would see only the 0.6 s net.
func StartClockStepSampler() {
	clockSamplerOnce.Do(func() {
		cfg := LoadClockStepConfig()
		clockSteps.mu.Lock()
		clockSteps.samplerOn = true
		clockSteps.mu.Unlock()
		ObserveClock()
		// Under the panic net (deploy/panic_net_guard_test.go): a panic here
		// logs + counts and ends the sampler; the gates still observe the
		// clock on every evaluation, so a dead sampler only coarsens sampling.
		safe.GoNet("clock-step sampler", "", func() {
			t := time.NewTicker(clockStepSamplePeriod)
			defer t.Stop()
			for range t.C {
				ObserveClock()
			}
		})
		logger.Infof("🕰 clock-step guard: ON — wall vs monotonic every %s; detect >= %dms (log+count), freeze >= %dms holds NEW entries %ds (CLOCK_STEP_DETECT_MS / CLOCK_STEP_FREEZE_MS / CLOCK_STEP_HOLD_S); the stale gate also ages the 1m feed by monotonic receipt (limit %ds)",
			clockStepSamplePeriod, cfg.DetectMs, cfg.FreezeMs, cfg.HoldS, receiptMaxAgeMs(60_000)/1000)
	})
}

// ── the one verdict ──────────────────────────────────────────────────────────

// FeedStaleAt is the FEED question for the 1m series of symbol: is its price
// current? It is the wall rule (barIsStale, CME-open/halt aware, unknown bar →
// not stale) OR the monotonic receipt leg — which fails CLOSED when a wired
// provider reports no live frame since boot, even with an empty 1m cache. It
// never reads the clock-step hold, so protection can use it. now is the wall
// instant the caller evaluates at; a now from time.Now() also carries the
// monotonic reading the receipt leg uses.
func FeedStaleAt(symbol string, newestBarOpenMs int64, now time.Time) (bool, string) {
	if !IsCMEOpen(now) {
		return false, ""
	}
	if StaleEntryGateFeed(newestBarOpenMs, now) {
		return true, fmt.Sprintf("bar_age: newest 1m bar opened %ds ago on the wall clock (B4 rule)", (now.UnixMilli()-newestBarOpenMs)/1000)
	}
	// m149 P2: a wired receipt provider with NO live frame since boot fails
	// CLOSED even when the 1m cache is empty (newest bar = 0). The empty-cache
	// fail-open below must not run before this leg — the 2 s freeze hold used
	// to cover it by accident; at 6 s nothing does.
	if stale, age, known := feedReceiptStale(symbol, "1m", recvClock(now)); known && stale && age < 0 {
		return true, "receipt_age: no live 1m frame received since boot (monotonic)"
	}
	if newestBarOpenMs <= 0 {
		return false, "" // no 1m series at all, and no wired no-live-frame provider: the documented fail-open
	}
	if stale, age, known := feedReceiptStale(symbol, "1m", recvClock(now)); known && stale {
		return true, fmt.Sprintf("receipt_age: last live 1m frame received %ds ago on the monotonic clock (> %ds)", age/1000, receiptMaxAgeMs(60_000)/1000)
	}
	return false, ""
}

// EntryFeedStale is THE stale-data verdict for a NEW entry on the mentor and
// protection-free paths: a clock-step hold (checked FIRST — a step can also
// move IsCMEOpen across 16:00/17:00, so it must not sit behind that check)
// OR FeedStaleAt. Returns the refusal reason naming the leg that fired.
func EntryFeedStale(symbol string, newestBarOpenMs int64, now time.Time) (bool, string) {
	if held, why := clockStepHeldAt(time.Time{}); held {
		return true, why
	}
	// P0 feed freshness — explicit DELAYED broker mode blocks a NEW entry with
	// precedence over the timestamp legs (delayed > stale). UNKNOWN mode is not
	// a reject reason on its own; FeedStaleAt still refuses missing/stale
	// candles below. This is the ENTRY verdict only — FeedStaleAt stays pure
	// for protection.
	if mode := feedMode(); mode == "delayed" {
		return true, "broker feed mode is delayed"
	}
	return FeedStaleAt(symbol, newestBarOpenMs, now)
}

// recvClock picks the instant the receipt leg measures at. A caller's now
// that carries a monotonic reading (time.Now(), or derived from it) is used;
// a constructed wall time (tests, replays) has none, so the real monotonic
// now is used instead — the receipt leg must never be measured on a clock
// the caller may have stepped.
func recvClock(now time.Time) time.Time {
	if now.Round(0) == now { // no monotonic reading
		return time.Now()
	}
	return now
}

// ClockSampleForTest replaces the guard's clock read and resets the guard
// (TESTS ONLY — production never calls it). It returns the restore func,
// which also resets the guard so no injected step leaks into another test.
func ClockSampleForTest(fn func() (wallNs int64, mono time.Duration)) (restore func()) {
	prev := clockSampleFn.Load()
	clockSampleFn.Store(fn)
	resetClockStepsForTest()
	return func() {
		clockSampleFn.Store(prev)
		resetClockStepsForTest()
	}
}

// SteppedClock is a test clock: monotonic time advances only via Advance, and
// the wall reading is monotonic + Offset, so Step(d) is a pure wall step.
type SteppedClock struct {
	mu     sync.Mutex
	mono   time.Duration
	offset time.Duration
	base   int64
}

// NewSteppedClock starts a test clock whose wall reads wall0.
func NewSteppedClock(wall0 time.Time) *SteppedClock {
	return &SteppedClock{base: wall0.UnixNano()}
}

// Sample is the clockSample shape.
func (c *SteppedClock) Sample() (int64, time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.base + int64(c.mono) + int64(c.offset), c.mono
}

// Advance moves real (monotonic) time forward; the wall follows.
func (c *SteppedClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.mono += d
	c.mu.Unlock()
}

// Step moves ONLY the wall clock (negative = backward), like the 10-14 step.
func (c *SteppedClock) Step(d time.Duration) {
	c.mu.Lock()
	c.offset += d
	c.mu.Unlock()
}
