package ninjatrader

import (
	"sync"
	"sync/atomic"
	"time"

	"nofx/logger"
)

// LIVE BAR SINK (W-PICTURE-HTF, 2026-09-20) — event fan-out for deterministic
// evaluators (the two-picture mode). The picture evaluator runs from NATIVE
// LIVE bar events, never historical replays: a Sept-17 replay must not mint a
// real opportunity, so drainBarIngest fans out ONLY non-historical frames.
//
// One process-wide sink (one TCP server). Registration is idempotent (last
// writer wins — exactly one evaluator dispatcher installs it). The sink call
// is dispatched through a small worker channel so the cache drain and the
// socket read loop are never stalled by evaluator work (DB writes only on
// qualified setups, but the drain-loop non-blocking invariant is absolute).

// liveSinkQueueCap must absorb a normal hour-open burst (one closed frame
// per subscribed TF plus intrabar frames) even while the sink worker briefly
// stalls on evaluator work. Measured 09-24 22:00 CT: 101 frames dropped with
// the old 256 cap (DEFAULTS-SANE).
const liveSinkQueueCap = 2048

var (
	liveBarSink   atomic.Value // func(symbol, tf, contract string, bars []Bar, receivedAt time.Time)
	liveSinkCh    = make(chan liveSinkMsg, liveSinkQueueCap)
	liveSinkOnce  sync.Once
	liveSinkDrops atomic.Int64
	// W4/D24: frames refused as LIVE ENTRY EVENTS because they reached us long
	// after they were emitted, and frames whose age could not be judged at all.
	// Both are READ; neither is inferred.
	staleLiveFrames  atomic.Int64
	unagedLiveFrames atomic.Int64
)

// liveFrameMaxAgeMs bounds how old a bar_update frame may be and still count
// as a LIVE entry event. It is deliberately far ABOVE the 10s entry window —
// an ordinary late emission (NT8 emits a closed bar on the first tick of the
// next bar) must never be refused here — and far BELOW one 5m candle period,
// so a frame belonging to an earlier interval cannot present itself as news.
// The frame still reaches the cache; only the entry fan-out is refused.
const liveFrameMaxAgeMs = 30_000

// StaleLiveFrames reports frames refused as live entry events for age.
func StaleLiveFrames() int64 { return staleLiveFrames.Load() }

// UnagedLiveFrames reports frames whose emitted_at was absent, so their age
// could not be judged. They are NOT refused — an AddOn older than the
// 2026-09-20 stamp would otherwise go dark — but they are never silent.
func UnagedLiveFrames() int64 { return unagedLiveFrames.Load() }

// liveFrameTooOld reports whether the frame's own emission stamp puts it
// outside liveFrameMaxAgeMs. Reads nothing but the frame: this is the wire
// boundary and it must not depend on any evaluator.
func liveFrameTooOld(bars []Bar, nowMs int64) bool {
	newest := int64(0)
	for _, b := range bars {
		if b.EmittedAt > newest {
			newest = b.EmittedAt
		}
	}
	if newest == 0 {
		unagedLiveFrames.Add(1)
		return false
	}
	return nowMs-newest > liveFrameMaxAgeMs
}

type liveSinkMsg struct {
	symbol string
	tf     string
	// contract is the front month the AddOn named on this frame; "" is
	// UNKNOWN. It travels WITH the frame because identity is a property of
	// the frame, not of the cache key it lands in.
	contract   string
	bars       []Bar
	receivedAt time.Time
}

// SetLiveBarSink installs the process-wide live-bar consumer. nil uninstalls.
func SetLiveBarSink(fn func(symbol, tf, contract string, bars []Bar, receivedAt time.Time)) {
	liveBarSink.Store(fn)
	startLiveSinkWorker()
}

func startLiveSinkWorker() {
	liveSinkOnce.Do(func() {
		go liveSinkLoop()
	})
}

func liveSinkLoop() {
	for msg := range liveSinkCh {
		fn, _ := liveBarSink.Load().(func(string, string, string, []Bar, time.Time))
		if fn == nil {
			continue
		}
		// The evaluator dispatcher never panics on purpose, but a panic in a
		// sink must never take the worker (and the frames behind it) down.
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("live bar sink panic recovered: %v", r)
				}
			}()
			fn(msg.symbol, msg.tf, msg.contract, msg.bars, msg.receivedAt)
		}()
	}
}

// sinkDropBurst tracks one drop episode so a full-queue storm is WARNed at
// its START (never silent, even mid-burst) and once at its END with the total
// count. fanOutLiveBars runs on the TCP read goroutine, but the mutex keeps
// the tracker safe regardless of caller.
type sinkDropBurst struct {
	mu      sync.Mutex
	count   int64
	started time.Time
}

// recordDrop opens or extends the burst. The FIRST drop WARNs immediately.
func (b *sinkDropBurst) recordDrop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.count == 0 {
		b.started = time.Now()
		logger.Warnf("picture-htf: live sink queue full — drop burst started (self-heals: evaluators rebuild from cache)")
	}
	b.count++
}

// closeBurst ends the burst, returning its count (0 when none). Called on a
// SUCCESSFUL enqueue — the queue has room again, so the episode is over.
func (b *sinkDropBurst) closeBurst() (int64, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, d := b.count, time.Since(b.started)
	b.count = 0
	return n, d
}

var liveSinkBurstTracker sinkDropBurst

// fanOutLiveBars enqueues LIVE frames for the sink. Non-blocking; on a full
// queue the pending frame is DROPPED, counted, and WARNed once per burst —
// evaluators rebuild their state from the cache on every event, so a dropped
// frame only delays an evaluation to the next one (the wall-clock fallback
// covers the rest).
func fanOutLiveBars(symbol, tf, contract string, bars []Bar) {
	if len(bars) == 0 {
		return
	}
	if _, ok := liveBarSink.Load().(func(string, string, string, []Bar, time.Time)); !ok {
		return
	}
	select {
	case liveSinkCh <- liveSinkMsg{symbol: symbol, tf: tf, contract: contract, bars: bars, receivedAt: time.Now()}:
		if n, d := liveSinkBurstTracker.closeBurst(); n > 0 {
			logger.Warnf("picture-htf: live sink queue drained — %d frame(s) dropped during the burst (%.0fs; self-heals: evaluators rebuild from cache)", n, d.Seconds())
		}
	default:
		liveSinkDrops.Add(1)
		liveSinkBurstTracker.recordDrop()
	}
}

// LiveSinkDrops exposes the dropped-frame counter (boot lines / dashboards).
func LiveSinkDrops() int64 { return liveSinkDrops.Load() }
