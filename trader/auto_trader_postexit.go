package trader

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	ntwire "vl/provider/ninjatrader"
	ntTrader "vl/trader/ninjatrader"
)

// Phase 4 — POST-EXIT IMMEDIATE RESCAN (final-bundle 2026-08-19).
//
// On position close (TP, SL, EOD-flat, manual, reconcile-priced) ONE immediate
// full decision cycle fires after POST_EXIT_DELAY_MS (default 2000). The cycle
// is a perfectly NORMAL cycle: it flows through every gate (session, roll,
// stop_until, B7 cooldown, dead-man …) — immediate ≠ privileged — and its
// prompt is byte-identical to a timer cycle (no reference to the just-closed
// trade: revenge-bias by context is forbidden). Position truth is consumed only
// through the #50 reconciled path (the cycle reads broker/reconciled state like
// any other cycle; this file never reads raw store rows).
//
// Close events arrive from the ninjatrader package (close_sync + reconcile) via
// the OnPositionClosed hook; the per-position-ID dedup makes partial-fill
// sequences fire exactly once, on the final flat row close.

const defaultPostExitDelayMs = 2000

func postExitRescanEnabled() bool {
	switch strings.ToLower(os.Getenv("POST_EXIT_RESCAN")) {
	case "off", "0", "false", "disabled":
		return false
	}
	return true
}

func postExitDelayMs() int64 {
	if v := os.Getenv("POST_EXIT_DELAY_MS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			return n
		}
	}
	return defaultPostExitDelayMs
}

// postExitRegistry routes close notifications (which carry only a traderID)
// back to the live AutoTrader instances.
var (
	postExitRegistry    sync.Map // traderID -> *AutoTrader
	postExitHookOnce    sync.Once
	postExitLegHookOnce sync.Once
)

func registerPostExitDispatch(at *AutoTrader) {
	postExitRegistry.Store(at.id, at)
	// E7 self-grep fix: the package-var hook is written exactly ONCE (multiple
	// traders' Run() racing an idempotent assignment was still a data race by
	// the memory model; close-sync goroutines read it concurrently).
	postExitHookOnce.Do(func() { ntTrader.OnPositionClosed = dispatchPositionClosed })
	// B2 (BUILD-ALL): the leg-aware hook is likewise written exactly once.
	postExitLegHookOnce.Do(func() { ntTrader.OnPositionLegClosed = dispatchPositionLegClosed })
}

func unregisterPostExitDispatch(at *AutoTrader) {
	postExitRegistry.Delete(at.id)
}

// runningTrader returns the live AutoTrader for id. postExitRegistry is the
// process's RUNNING-trader registry: Run registers every trader before its
// loop starts and Stop removes it (auto_trader.go) — the one registry that
// covers traders without a Picture evaluator (W-EXEC-TRUTH W0 (c), CTO #1).
func runningTrader(id string) (*AutoTrader, bool) {
	v, ok := postExitRegistry.Load(id)
	if !ok {
		return nil, false
	}
	at, _ := v.(*AutoTrader)
	return at, at != nil
}

// runningTraderIDs lists every trader running in this process.
func runningTraderIDs() []string {
	var ids []string
	postExitRegistry.Range(func(k, _ any) bool {
		if id, _ := k.(string); id != "" {
			ids = append(ids, id)
		}
		return true
	})
	return ids
}

func dispatchPositionClosed(traderID string, positionID int64) {
	if v, ok := postExitRegistry.Load(traderID); ok {
		v.(*AutoTrader).notifyPositionClosed(positionID)
	}
}

// dispatchPositionLegClosed (B2, BUILD-ALL) routes the leg-aware position_close
// payload back to the owning trader so the exit drive marks leg 1 scaled only
// on the broker's TP receipt.
func dispatchPositionLegClosed(traderID string, p ntwire.PositionClosePayload) {
	if v, ok := postExitRegistry.Load(traderID); ok {
		v.(*AutoTrader).mentorMarkLeg1Scaled(p)
	}
}

// notifyPositionClosed schedules the one post-exit cycle. Called from the
// close-sync / reconcile goroutines — everything here is thread-safe.
func (at *AutoTrader) notifyPositionClosed(positionID int64) {
	if !postExitRescanEnabled() || at.exchange != "ninjatrader" {
		return
	}
	// Dedup per position id: partial-fill sequences and the close_sync+reconcile
	// double-report both collapse to ONE rescan.
	at.postExitMu.Lock()
	if at.postExitSeen == nil {
		at.postExitSeen = make(map[int64]bool)
	}
	if at.postExitSeen[positionID] {
		at.postExitMu.Unlock()
		return
	}
	if len(at.postExitSeen) > 4096 { // unbounded-growth backstop
		at.postExitSeen = make(map[int64]bool)
	}
	at.postExitSeen[positionID] = true
	at.postExitMu.Unlock()

	delay := time.Duration(postExitDelayMs()) * time.Millisecond
	at.logInfof("↻ post-exit rescan armed for position #%d — one full decision cycle in %v (all gates apply).", positionID, delay)
	time.AfterFunc(delay, func() {
		// The run loop is single-goroutine: if an AI call is in flight the kick
		// waits in the buffered channel and fires exactly once after it returns
		// (#45 no-concurrent-calls). If the loop is gone, the send drops.
		select {
		case at.kickCh <- "post_exit":
		default:
			at.logWarnf("↻ post-exit rescan for position #%d dropped — kick queue full/stopped.", positionID)
		}
	})
}
