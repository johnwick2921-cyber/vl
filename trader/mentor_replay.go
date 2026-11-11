package trader

import (
	"sync"
	"time"

	"vl/calendar"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

// MentorSeedBarSource is the exported name of the seed's bar read surface
// (mentorSeedBarSource): *store.BarHistoryStore in production, an as-of view of
// the same rows in the offline replay.
type MentorSeedBarSource = mentorSeedBarSource

// MentorReplay drives the PRODUCTION mentor path offline (cmd/mentor-replay).
//
// It is a minimal AutoTrader — no store, no broker, no NT8 — whose evaluator is
// seeded by the SAME seed body the boot runs (mentorSeedFrom, the body of
// mentorSeedAtStart) and ticked by the SAME per-bar entry the live event loop
// and the 2-minute scan run (mentorEvalOnce). Nothing here re-implements a
// detector: every box / trigger / level / trendline / intent / refusal the
// replay reports is what the live trader's evaluator produced on those bars.
//
// HONEST LIMITS. With no store and no broker the trader-side dispatch after the
// evaluator (ledger write, sizing, placement) has nowhere to write: an entry
// intent is observed through the intent tap BEFORE anything acts on it, and the
// dispatch's own lines are trader-side, not kernel facts. Fills are therefore
// never real, so G1/G2 (cfg.RealFillOnly) stay as a fill-less day leaves them.
// The process-wide seams it installs (the mentor clock, the calendar reads)
// make it single-use per process: one replay at a time.
type MentorReplay struct {
	at *AutoTrader

	mu     sync.Mutex
	now    time.Time
	tapped []mentor.Intent
}

// NewMentorReplay builds the replay trader for a strategy config. MentorMode is
// forced ON (the replay exists to run the mentor path); everything else in the
// config is the strategy's own, read exactly as the manager hands it to a live
// trader (store.Strategy.ParseConfig).
func NewMentorReplay(id string, sc *store.StrategyConfig) *MentorReplay {
	if sc == nil {
		sc = &store.StrategyConfig{}
	}
	cp := *sc
	cp.RiskControl.MentorMode = true
	at := &AutoTrader{
		id:   id,
		name: id,
		config: AutoTraderConfig{
			ID:             id,
			Name:           id,
			StrategyConfig: &cp,
		},
	}
	r := &MentorReplay{at: at}
	at.mentorIntentTap = func(in mentor.Intent) {
		r.tapped = append(r.tapped, in)
	}
	setMentorNowSource(func() time.Time {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.now
	})
	return r
}

// SetNow pins the mentor clock (every mentor time gate reads mentorClockNow).
func (r *MentorReplay) SetNow(t time.Time) {
	r.mu.Lock()
	r.now = t
	r.mu.Unlock()
}

// SetCalendar installs the stored calendar read for the replay: the tick's
// red-folder print windows (mentorDayEvents, by the clock's trade date) and the
// seed's per-history-day windows (mentorDayEventsOn). nil = no calendar (every
// day reads as no slice, exactly like a live bot with an empty calendar table).
func (r *MentorReplay) SetCalendar(fn func(date string) ([]calendar.Event, bool)) {
	if fn == nil {
		mentorDayEventsOnForTest = func(string) ([]calendar.Event, bool) { return nil, false }
		mentorDayEventsForTest = func() ([]calendar.Event, bool) { return nil, false }
		return
	}
	mentorDayEventsOnForTest = fn
	mentorDayEventsForTest = func() ([]calendar.Event, bool) {
		return fn(plannerTradeDateCT(mentorClockNow()))
	}
}

// Seed runs the production seed body over src at the pinned clock.
func (r *MentorReplay) Seed(src MentorSeedBarSource) {
	r.tapped = nil
	r.at.mentorSeedFrom(src)
}

// Reseed runs the production history-refill re-seed body (mentorReseedFrom,
// the body of mentorReseedOnRefill) at nowMs. The live trader runs it inside
// mentorEvalOnce, before the tick, when NT8 released a history replay to the
// store; the replay has no NT8, so a caller that knows when the live bot did it
// (the live log line) injects it before the same bar's tick.
func (r *MentorReplay) Reseed(src MentorSeedBarSource, nowMs int64, why string) {
	if r.at.mentorEval == nil {
		return
	}
	r.at.mentorReseedFrom(src, nowMs, why)
}

// EvalOnce is one production tick (mentorEvalOnce) over the closed 1m window,
// returning every intent the evaluator emitted for it (catch-up ticks
// included), observed before the dispatch acts on them. ran=false when the
// newest bar was already ticked (the once-per-bar watermark).
func (r *MentorReplay) EvalOnce(closed1m []market.Kline) (intents []mentor.Intent, ran bool) {
	if len(closed1m) == 0 {
		return nil, false
	}
	r.tapped = nil
	ran = r.at.mentorEvalOnce(closed1m)
	out := r.tapped
	r.tapped = nil
	return out, ran
}

// EventPass is the LIVE event-loop entry (mentorEventPassAt): it reads the 1m
// window from market.FuturesBarsProvider, drops a forming tail bar, and ticks.
// The parity test drives this against EvalOnce.
func (r *MentorReplay) EventPass(now time.Time) (intents []mentor.Intent, ran bool) {
	r.tapped = nil
	ran = r.at.mentorEventPassAt(now)
	out := r.tapped
	r.tapped = nil
	return out, ran
}

// Evaluator exposes the evaluator for read-only state inspection.
func (r *MentorReplay) Evaluator() *mentor.Evaluator { return r.at.mentorEval }

// Counts returns a copy of the trader-side mentor counters.
func (r *MentorReplay) Counts() map[string]int { return r.at.mentorCountSnapshot() }

// LogTag is the "[trader_id=…]" prefix this replay's log lines carry.
func (r *MentorReplay) LogTag() string { return r.at.logTag() }
