package ninjatrader

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nofx/logger"
)

// Bar persistence hook (2026-08-26) — the unblock for replay/calibration.
//
// BAR-TRUTH WAVE (2026-08-28, S-1/A-1): the old fan-out spawned a goroutine
// PER FRAME — at ~500 frames/s that was millions of goroutines, GC churn,
// and GORM single-connection pile-up (the drain fell behind and the ingest
// channel dropped frames: the backpressure WARN flood). Now ONE worker
// consumes a bounded queue, batches in time, and drops are counted + logged
// 1-line/min instead of per drop. A dropped CLOSE self-heals: the next live
// frame re-derives the closed tail from the cache, and the boot backfill
// covers anything older.

const barPersistQueueCap = 4096

// persistWatchdogSeconds (PRE-REOPEN F2, 2026-08-28) — the bar-persist silence
// alarm. The Friday 09:12–11:20 GORM stall went ~2h with only per-event 2s/4s
// WARNs and one drop — a silent stall can now never outlive this window.
func persistWatchdogSeconds() int64 {
	if v := os.Getenv("PERSIST_STALL_WATCHDOG_S"); v != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n >= 10 {
			return n
		}
	}
	return 60
}

var (
	barPersister         atomic.Value // func(historical bool, symbol, tf string, bars []Bar)
	barPersistCh         chan persistMsg
	persistOnce          sync.Once
	persistDropped       atomic.Int64 // queue-full drops (self-healing via cache tail)
	persistDroppedCloses atomic.Int64 // CLOSED bars lost on queue-full drops (must stay 0)
	persistFlushed       atomic.Int64 // closed bars handed to the persister
	persistLastSum       atomic.Int64 // unix seconds of the last drop summary
	// Reported-baselines: the counter values as of the last PUBLISHED summary.
	// The interval is measured against these, so the summary never has to zero
	// a counter in order to say what changed (class 35).
	persistDroppedReported       atomic.Int64
	persistDroppedClosesReported atomic.Int64
	persistLastFlushAt           atomic.Int64 // unix seconds of the last successful flush (F2 watchdog)
	persistLastFrameAt           atomic.Int64 // unix seconds of the last LIVE bar frame (W1 quiet-wire awareness)
	persistAlarmAt               atomic.Int64 // unix seconds of the last watchdog ERROR (dedup)
	ingestDropOld                atomic.Int64 // ingest channel drop-oldest events
	ingestDropCur                atomic.Int64 // ingest channel drop-current events
	ingestDropHist               atomic.Int64 // historical batch drops
	ingestLastSum                atomic.Int64
)

type persistMsg struct {
	historical bool
	symbol     string
	tf         string
	bars       []Bar
}

// SetBarPersister installs the persistence callback (nil to detach).
func SetBarPersister(fn func(historical bool, symbol, tf string, bars []Bar)) {
	if fn == nil {
		barPersister.Store((func(bool, string, string, []Bar))(nil))
		return
	}
	barPersister.Store(fn)
}

// startPersistWorker launches the single queue-consuming worker (once).
func startPersistWorker() {
	persistOnce.Do(func() {
		barPersistCh = make(chan persistMsg, barPersistQueueCap)
		go func() {
			ticker := time.NewTicker(300 * time.Millisecond)
			defer ticker.Stop()
			// S1 WIRE-UP (2026-08-29) — the F2 persist-silence watchdog, NOW
			// actually wired: a 30s check against the last successful flush
			// stamp. The pre-reopen wave shipped the atomics + knob resolver
			// but never wrote or read them (declared-but-dead code, class 19).
			watchdog := time.NewTicker(30 * time.Second)
			defer watchdog.Stop()
			var batch []persistMsg
			flush := func() {
				if len(batch) == 0 {
					return
				}
				fn, ok := barPersister.Load().(func(bool, string, string, []Bar))
				if ok && fn != nil {
					for _, m := range batch {
						func() {
							defer func() {
								if r := recover(); r != nil {
									// never take the worker down
								}
							}()
							fn(m.historical, m.symbol, m.tf, m.bars)
						}()
						persistFlushed.Add(int64(len(m.bars)))
					}
					// S1 WIRE-UP — stamp EVERY successful flush (this one
					// closure serves both the 256-batch and the 300ms ticker
					// paths) so the silence watchdog has a heartbeat.
					persistLastFlushAt.Store(time.Now().Unix())
				}
				batch = batch[:0]
			}
			for {
				select {
				case m := <-barPersistCh:
					batch = append(batch, m)
					if len(batch) >= 256 {
						flush()
					}
				case <-ticker.C:
					flush()
				case <-watchdog.C:
					if msg := persistWatchdogAlarmAt(time.Now().Unix()); msg != "" {
						logger.Errorf("%s", msg)
					}
				}
			}
		}()
	})
}

// persistWatchdogAlarmAt (S1 WIRE-UP, 2026-08-29) — the F2 persist-silence
// watchdog, NOW WIRED: returns the loud ERROR text when no successful flush
// has landed for persistWatchdogSeconds() (default 60s, min 10), deduped to
// ONE alarm per watchdog window via persistAlarmAt. Returns "" when healthy.
// A zero stamp (no flush yet — boot backfill in flight) never alarms cold.
//
// SUNDAY-SHIELD W1 (2026-08-29) — FRAME-AWARE: the alarm fires ONLY when live
// frames are FLOWING (persistLastFrameAt fresh) while flushes aren't. An idle
// wire — weekend, the daily 16:00–17:00 break, NT8 closed — has no frames and
// must stay SILENT: without this the alarm cried wolf 1/min for the whole
// weekend (373 fires on the 2026-08-29 boot alone) and a real GORM stall
// became indistinguishable from market-closed noise.
func persistWatchdogAlarmAt(now int64) string {
	last := persistLastFlushAt.Load()
	if last == 0 {
		return ""
	}
	if now-last <= persistWatchdogSeconds() {
		return ""
	}
	if now-persistLastFrameAt.Load() > persistWatchdogSeconds() {
		return "" // idle wire — no live frames within the window, nothing to stall
	}
	if now-persistAlarmAt.Load() < persistWatchdogSeconds() {
		return ""
	}
	persistAlarmAt.Store(now)
	return fmt.Sprintf("🔕 PERSIST WATCHDOG: no successful bar flush for %ds (queue_drops=%d) — the persist writer may be stalled (the 2026-08-28 GORM-stall class)", now-last, persistDropped.Load())
}

// barPersistSummary logs the 1-line/minute drop summary (no per-drop WARNs).
// Clock seam (class 60): the 60s rate limit here is a TIME-DEPENDENT RULE, and
// the body is destructive — it Swap(0)s both counters. A caller that increments
// and then reads them can have its increment erased by a summary that happened
// to fall due, which is what made TestFanOutClosesLastResortIsHonest fail ~1 run
// in 6 with no load involved at all.
func barPersistSummary() {
	barPersistSummaryAt(time.Now())
}

func barPersistSummaryAt(nowT time.Time) {
	now := nowT.Unix()
	if now-persistLastSum.Load() < 60 {
		return
	}
	if persistLastSum.CompareAndSwap(persistLastSum.Load(), now) {
		// Read the interval, THEN advance the baseline. The counters are not
		// touched: a reader racing this line sees the same value before and
		// after. The reported numbers are identical to what Swap(0) produced.
		dropped, closes := persistIntervalDelta()
		persistDroppedReported.Store(persistDropped.Load())
		persistDroppedClosesReported.Store(persistDroppedCloses.Load())
		logger.Warnf("bars: persist queue summary: queue_drops=%d closes_dropped=%d flushed=%d (closes_dropped must be 0 — queue-full drops self-heal via the cache tail)",
			dropped, closes, persistFlushed.Load())
	}
}

// persistIntervalDelta reports what has accumulated since the last published
// summary, without disturbing the counters.
func persistIntervalDelta() (dropped, closes int64) {
	d := persistDropped.Load() - persistDroppedReported.Load()
	c := persistDroppedCloses.Load() - persistDroppedClosesReported.Load()
	// A baseline ahead of its counter can only mean an unsynchronised rollover.
	// Report zero rather than a negative count, which would read as a fix.
	if d < 0 {
		d = 0
	}
	if c < 0 {
		c = 0
	}
	return d, c
}

// rollPersistCounters is the ONLY reset. Keeping it a separate, explicit verb is
// the whole fix: reporting must never perform one as a side effect. The previous
// destructive read spent weeks being misfiled as a load flake.
func rollPersistCounters() {
	persistDropped.Store(0)
	persistDroppedCloses.Store(0)
	persistDroppedReported.Store(0)
	persistDroppedClosesReported.Store(0)
}

// ClosedBarsOnly keeps the bars whose CLOSE time (T + tf duration) has passed
// at nowMs — forming bars are never persisted (the spec: zero writes on
// forming bars). T is the bar's OPEN time per the canonical cache contract.
func ClosedBarsOnly(bars []Bar, tf string, nowMs int64) []Bar {
	dur := timeframeMs(tf)
	if dur <= 0 {
		dur = 60_000
	}
	out := make([]Bar, 0, len(bars))
	for _, b := range bars {
		if b.T+dur <= nowMs {
			out = append(out, b)
		}
	}
	return out
}

// ClosedCacheTail returns up to `window` of the most-recent CLOSED bars from
// the cache (sorted ascending by T, oldest first). Live bar_update frames
// only ever carry the FORMING bar — NT8 does not re-emit the just-closed bar
// at the minute boundary — so the live persistence path reads the final
// closed bars from the cache itself (which always holds them) instead of the
// frame. A window of 8 covers multi-minute gaps between frames.
func ClosedCacheTail(get func(symbol, tf string) []Bar, symbol, tf string, nowMs int64, window int) []Bar {
	dur := timeframeMs(tf)
	if dur <= 0 {
		dur = 60_000
	}
	all := get(symbol, tf)
	closed := make([]Bar, 0, window)
	for i := len(all) - 1; i >= 0 && len(closed) < window; i-- {
		if all[i].T+dur <= nowMs {
			closed = append(closed, all[i])
		}
	}
	// Reverse to restore ascending order (InsertBars expects any order, but
	// ascending keeps logs sane).
	for l, r := 0, len(closed)-1; l < r; l, r = l+1, r-1 {
		closed[l], closed[r] = closed[r], closed[l]
	}
	return closed
}

// fanOutBarPersist enqueues closed bars for the persist worker. Empty batch
// or no persister installed → no-op (zero writes on forming bars BY
// CONSTRUCTION — intra-bar updates stay in-memory). Queue-full drops are
// counted + summarized 1-line/min; they self-heal because the next live
// frame re-derives the closed cache tail.
//
// F2 (LONDON-FORENSICS 2026-08-28) — CLOSES ARE SACRED: the 06:09 event
// dropped 8 CLOSED bars (queue full while the GORM writer was stalled).
// Queue-full now blocks-with-timeout (backpressure to the ingest drainer,
// which the socket read loop never sees) instead of dropping; only after the
// deadline does a close-carrying batch drop, and then it shouts ERROR — never
// a silent counted drop.
func fanOutBarPersist(warn func(msg string, kv ...interface{}), historical bool, symbol, tf string, bars []Bar) {
	if len(bars) == 0 {
		return
	}
	fn, ok := barPersister.Load().(func(bool, string, string, []Bar))
	if !ok || fn == nil {
		return
	}
	// W1 — stamp LIVE frame flow (never historical/backfill) so the silence
	// watchdog can separate "stalled while bars flow" from "idle wire". A
	// queue-full drop is still stamped: frames flowed, persistence didn't.
	if !historical {
		persistLastFrameAt.Store(time.Now().Unix())
	}
	startPersistWorker()
	msg := persistMsg{historical: historical, symbol: symbol, tf: tf, bars: bars}
	select {
	case barPersistCh <- msg:
		return
	default:
	}
	// Queue full. Closes (and historical batches) are sacred: bounded-blocking
	// retry — backpressure, never a drop — then a LOUD last resort.
	if historical || hasClosedBar(bars, tf) {
		for attempt := 1; attempt <= 3; attempt++ {
			select {
			case barPersistCh <- msg:
				return
			case <-time.After(2 * time.Second):
				logger.Warnf("bars: persist queue stalled %ds (attempt %d/3) — close-carrying batch waiting (closes are never dropped on this path)", attempt*2, attempt)
			}
		}
		persistDropped.Add(1)
		persistDroppedCloses.Add(int64(len(closedBarsOnly(bars, tf))))
		logger.Errorf("bars: persist queue stalled 6s+ — %d CLOSED bar(s) dropped (closes_dropped must be 0; cache-tail re-derive + boot backfill cover the gap)", len(closedBarsOnly(bars, tf)))
		barPersistSummary()
		return
	}
	persistDropped.Add(1)
	barPersistSummary()
}

// hasClosedBar reports whether the batch carries at least one CLOSED bar (open
// time + tf duration ≤ now).
func hasClosedBar(bars []Bar, tf string) bool {
	return len(closedBarsOnly(bars, tf)) > 0
}

// closedBarsOnly filters to the bars whose CLOSE time has passed (same
// contract as ClosedBarsOnly, exported for the drop accounting).
func closedBarsOnly(bars []Bar, tf string) []Bar {
	dur := timeframeMs(tf)
	if dur <= 0 {
		dur = 60_000
	}
	nowMs := time.Now().UnixMilli()
	out := make([]Bar, 0, len(bars))
	for _, b := range bars {
		if b.T+dur <= nowMs {
			out = append(out, b)
		}
	}
	return out
}

// ingestDropSummary logs the 1-line/minute ingest-drop summary in place of
// the old per-drop WARN flood (A-1, 2026-08-28). FORENSICS HYGIENE: the
// counters are now labeled honestly — the ingest channel carries FORMING
// intra-bar updates only (closed bars are re-derived from the cache tail
// after the drain, so a dropped CLOSE is impossible here BY CONSTRUCTION),
// and the persist queue reports queue_drops and closes_dropped separately.
func ingestDropSummary() {
	now := time.Now().Unix()
	if now-ingestLastSum.Load() < 60 {
		return
	}
	if ingestLastSum.CompareAndSwap(ingestLastSum.Load(), now) {
		logger.Warnf("bars: ingest drop summary: intrabar_dropped=%d current_dropped=%d historical_dropped=%d peak_depth=%d/%d (1-line/min; intra-bar drops self-heal on the next tick — closed bars are NEVER dropped on this path)",
			ingestDropOld.Swap(0), ingestDropCur.Swap(0), ingestDropHist.Swap(0),
			ingestPeakDepth.Load(), ingestQueueCap())
	}
}
