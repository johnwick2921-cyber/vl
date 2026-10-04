package trader

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"vl/kernel"
)

// ── N12 MENTOR FUNNEL VISIBILITY (read-only) ───────────────────────────────
//
// The kernel's B-rules refusal ledger (e.State.Refusals + e.State.Limits.Refusals)
// and the trader's mentorCount refusal counters were never surfaced, so a day
// where every intraday intent is dropped as orb_not_drawn is invisible. The
// funnel collects the per-session-day numbers and emits ONE INFO line every
// 15 minutes plus on change (the refusals/stages change, not the bar count)
// while mentor mode is ON. It never changes any trading behaviour.

const mentorFunnelEmitInterval = 15 * time.Minute

// mentorFunnel is the per-trader funnel counter (session-day scoped, 17:00 CT).
type mentorFunnel struct {
	mu       sync.Mutex
	dayKey   string
	bars     int
	intents  int
	authored int
	placed   int
	filled   int
	lastEmit time.Time
	lastFp   string
}

func (f *mentorFunnel) reset(dayKey string) {
	f.dayKey = dayKey
	f.bars, f.intents, f.authored, f.placed, f.filled = 0, 0, 0, 0, 0
}

// bumpAuthored/Placed/Filled are called from the entry/fill goroutines; they
// are mutex-guarded so the funnel line read never races a live fill.
func (f *mentorFunnel) bumpAuthored() {
	f.mu.Lock()
	f.authored++
	f.mu.Unlock()
}

func (f *mentorFunnel) bumpPlaced() {
	f.mu.Lock()
	f.placed++
	f.mu.Unlock()
}

func (f *mentorFunnel) bumpFilled() {
	f.mu.Lock()
	f.filled++
	f.mu.Unlock()
}

// mentorFunnelTick records one evaluator tick (one closed bar, intents emitted)
// and emits the line when due. Called once per closed bar from mentorEvalOnce.
func (at *AutoTrader) mentorFunnelTick(bars, intents int) {
	if at == nil || !at.mentorEnabled() {
		return
	}
	now := time.Now()
	dayKey := kernel.CMESessionDayKey(now)
	kernelRefusals := mentorKernelRefusals(at)
	traderRefusals := mentorTraderRefusals()

	f := &at.mentorFunnel
	f.mu.Lock()
	if f.dayKey != dayKey {
		f.reset(dayKey)
	}
	f.bars += bars
	f.intents += intents
	// The fingerprint is the MEANINGFUL part — refusals + stages, never the bar
	// count (bars change every tick; emitting on that would flood the log).
	fp := mentorFunnelFingerprint(kernelRefusals, traderRefusals, f.authored, f.placed, f.filled)
	emit := f.lastFp == "" || now.Sub(f.lastEmit) >= mentorFunnelEmitInterval || fp != f.lastFp
	if !emit {
		f.mu.Unlock()
		return
	}
	f.lastEmit = now
	f.lastFp = fp
	line := mentorFunnelLine(f.bars, f.intents, kernelRefusals, traderRefusals, f.authored, f.placed, f.filled)
	f.mu.Unlock()

	at.logInfof("%s", line)
}

// mentorFunnelLine renders the funnel line (pure — the pins assert its content).
func mentorFunnelLine(bars, intents int, kref, tref map[string]int, authored, placed, filled int) string {
	return fmt.Sprintf("🧑‍🏫 mentor funnel [15m]: bars %d · intents %d · kernel refusals %s · trader refusals %s · authored %d · placed %d · filled %d",
		bars, intents, mentorRefusalString(kref), mentorRefusalString(tref), authored, placed, filled)
}

// mentorKernelRefusals merges the evaluator's two B-rules refusal ledgers into
// one map (reason → count).
func mentorKernelRefusals(at *AutoTrader) map[string]int {
	out := map[string]int{}
	if at == nil || at.mentorEval == nil {
		return out
	}
	for k, v := range at.mentorEval.State.Refusals {
		out[k] += v
	}
	for k, v := range at.mentorEval.State.Limits.Refusals {
		out[k] += v
	}
	return out
}

// mentorTraderRefusals returns the TRADER-side refusals from the mentorCount
// snapshot — every counted drop the injector made, not the stage/tier/OK
// counters.
func mentorTraderRefusals() map[string]int {
	out := map[string]int{}
	for k, v := range MentorCountSnapshot() {
		if mentorTraderRefusalKey(k) {
			out[k] = v
		}
	}
	return out
}

// mentorTraderRefusalKey reports whether a mentorCount key is a trader-side
// refusal (a drop) as opposed to a stage (armed_*, authored), an action OK
// (sent/ok/requested), or a tier. The keyword list covers the refusal family:
// refused/suppressed/missing/no_source/no_data/no_calendar/held/no_chase/
// short/bad_value/failed/unwired/error/hold.
func mentorTraderRefusalKey(key string) bool {
	for _, kw := range []string{
		"refused", "suppressed", "missing", "no_source", "no_data", "no_calendar",
		"held", "no_chase", "short", "bad_value", "failed", "unwired", "error", "hold",
	} {
		if strings.Contains(key, kw) {
			return true
		}
	}
	return false
}

// mentorFunnelFingerprint is the stable string the on-change emit compares.
func mentorFunnelFingerprint(kref, tref map[string]int, authored, placed, filled int) string {
	return fmt.Sprintf("%s|%s|a=%d|p=%d|f=%d", mentorRefusalString(kref), mentorRefusalString(tref), authored, placed, filled)
}

// mentorRefusalString renders a refusal map deterministically: {} or
// {reason:n reason:n …} with sorted keys.
func mentorRefusalString(m map[string]int) string {
	if len(m) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "%s:%d", k, m[k])
	}
	b.WriteString("}")
	return b.String()
}
