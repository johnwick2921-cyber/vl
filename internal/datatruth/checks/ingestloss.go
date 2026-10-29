package checks

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// ── CHECK 4: INGEST DROPS ARE DATA LOSS ──────────────────────────────────────
//
// The bar ingest queue (provider/ninjatrader tcp_server.go evictForIngest /
// enqueueBarHistorical) can drop frames under a burst. Three kinds exist:
//
//	intrabar   a FORMING (non-final) frame: self-heals on the next tick of the
//	           same bar. NOT data loss - the rate is logged, nothing pauses.
//	final      a frame carrying a FINAL bar (the AddOn's one-shot close). The only
//	           proof the minute closed is gone; the cache's last forming values
//	           may lack the closing ticks. DATA LOSS.
//	historical a bars_historical batch (the restart seed / gap replay). DATA LOSS.
//
// The drop site records WHICH bars were lost (symbol, tf, open times) into a
// Ledger. The check blocks while any recorded bar is not VERIFIED - present, and
// for a lost final also CLOSED (carries Final, or a later bar of the frame
// exists) - in the source the decision reads. A re-request that lands the bars
// clears the entry; nothing else does. peak_depth at the cap is context for the
// reason text only: a full queue that dropped nothing lost nothing.

// Loss kinds.
const (
	LossFinal      = "final"
	LossHistorical = "historical"
	LossIntrabar   = "intrabar"
)

type lossKey struct {
	symbol, tf string
	t          int64
}

type lossEntry struct {
	kind string
	atMs int64
}

// Ledger is the bounded record of lost bars. Safe for concurrent use.
type Ledger struct {
	mu          sync.Mutex
	pending     map[lossKey]lossEntry
	intrabar    []int64 // ms of recent intrabar drops (for the rate)
	cap         int
	overflow    int64 // bars lost beyond the ledger cap: every symbol blocks until they are verified wholesale
	overflowSym map[string]int64
}

// NewLedger builds a ledger holding at most capBars pending bars.
func NewLedger(capBars int) *Ledger {
	if capBars <= 0 {
		capBars = 20000
	}
	return &Ledger{pending: map[lossKey]lossEntry{}, cap: capBars, overflowSym: map[string]int64{}}
}

// RecordLoss notes that bars (open times ts) of symbol/tf were dropped. kind is
// LossFinal or LossHistorical; LossIntrabar only feeds the rate.
func (l *Ledger) RecordLoss(kind, symbol, tf string, ts []int64, now time.Time) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if kind == LossIntrabar {
		l.intrabar = append(l.intrabar, now.UnixMilli())
		if len(l.intrabar) > 4096 {
			l.intrabar = l.intrabar[len(l.intrabar)-2048:]
		}
		return
	}
	for _, t := range ts {
		k := lossKey{symbol, tf, t}
		if _, ok := l.pending[k]; ok {
			continue
		}
		if len(l.pending) >= l.cap {
			l.overflowSym[symbol]++
			l.overflow++
			continue
		}
		l.pending[k] = lossEntry{kind: kind, atMs: now.UnixMilli()}
	}
	if len(ts) == 0 { // a drop whose bars are unknown is still a loss: block the symbol until the ledger is told otherwise
		l.overflowSym[symbol]++
		l.overflow++
	}
}

// IntrabarPerMin is the intrabar drop rate over the last 5 minutes.
func (l *Ledger) IntrabarPerMin(now time.Time) float64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.UnixMilli() - 5*minuteMs
	n := 0
	for _, t := range l.intrabar {
		if t >= cut {
			n++
		}
	}
	return float64(n) / 5
}

// ClearUnknown releases the "drop with unknown bars" debt for a symbol once the
// caller has independently verified the symbol's series whole (e.g. a clean
// re-seed). Bars recorded by time are cleared only by Judge's verification.
func (l *Ledger) ClearUnknown(symbol string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.overflow -= l.overflowSym[symbol]
	delete(l.overflowSym, symbol)
	l.mu.Unlock()
}

// VerifyFn reports whether the lost bar is now present and closed in the series
// the decision reads. An error means "cannot tell" and leaves the loss pending.
type VerifyFn func(symbol, tf string, t int64, kind string) (ok bool, err error)

// Judge verifies every pending loss of symbol and returns the verdict. Verified
// entries are removed; the rest keep the symbol blocked.
func (l *Ledger) Judge(symbol string, verify VerifyFn) (blocked bool, why string) {
	if l == nil {
		return false, ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var left []lossKey
	var firstErr error
	for k, e := range l.pending {
		if k.symbol != symbol {
			continue
		}
		ok, err := verify(k.symbol, k.tf, k.t, e.kind)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if ok && err == nil {
			delete(l.pending, k)
			continue
		}
		left = append(left, k)
	}
	unk := l.overflowSym[symbol]
	if len(left) == 0 && unk == 0 {
		return false, ""
	}
	sort.Slice(left, func(i, j int) bool {
		if left[i].t != left[j].t {
			return left[i].t < left[j].t
		}
		return left[i].tf < left[j].tf
	})
	msg := fmt.Sprintf("%d lost bar(s) not yet verified present", len(left))
	if len(left) > 0 {
		k := left[0]
		msg += fmt.Sprintf(" (first %s %s %s, %s)", symbol, k.tf, time.UnixMilli(k.t).UTC().Format("01-02 15:04Z"), l.pending[k].kind)
	}
	if unk > 0 {
		msg += fmt.Sprintf("; %d dropped bar(s) beyond the ledger/unknown", unk)
	}
	if firstErr != nil {
		msg += "; verify error: " + firstErr.Error()
	}
	return true, msg
}

// Pending returns the number of unverified lost bars (all symbols).
func (l *Ledger) Pending() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.pending)
}
