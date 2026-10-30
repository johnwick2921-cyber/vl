package checks

import (
	"fmt"
	"sort"
	"strings"
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
//
// NOTHING LOST IS NOTHING OWED. A dropped message that carried no real bar (an
// empty bars_historical frame, a placeholder-only batch) lost no minute, so it
// opens no debt: the first draft ledgered it as an "unknown" loss that had no
// clearing path and paused MNQ until the process restarted (review round 1, D1).
// The only debt that is not tied to a minute is the ledger-cap OVERFLOW, and it
// is recorded as a (symbol, frame, from, to) RANGE that the whole-range verifier
// clears once every slot of it is present in the ring (or aged out of it).

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
	mu       sync.Mutex
	pending  map[lossKey]lossEntry
	intrabar []int64 // ms of recent intrabar drops (for the rate)
	cap      int
	overflow map[ovKey]*ovRange // bars lost beyond the ledger cap, kept as ranges so they can be verified wholesale
	whole    WholeFn
}

type ovKey struct{ symbol, tf string }

// ovRange is the span of open times dropped beyond the cap for one frame.
type ovRange struct {
	kind     string
	from, to int64
	n        int64
}

// WholeFn reports whether every open-market slot of [from, to] of symbol/tf is
// present (and, for kind final, closed) in the series the decision reads, or has
// aged out of it. An error means "cannot tell" and leaves the debt standing.
type WholeFn func(symbol, tf string, from, to int64, kind string) (whole bool, err error)

// NewLedger builds a ledger holding at most capBars pending bars.
func NewLedger(capBars int) *Ledger {
	if capBars <= 0 {
		capBars = 20000
	}
	return &Ledger{pending: map[lossKey]lossEntry{}, cap: capBars, overflow: map[ovKey]*ovRange{}}
}

// SetWhole installs the range verifier that clears cap-overflow debt. Without
// one the overflow debt can only block (fail closed), never clear.
func (l *Ledger) SetWhole(fn WholeFn) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.whole = fn
	l.mu.Unlock()
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
			ok := ovKey{symbol, tf}
			r := l.overflow[ok]
			if r == nil {
				r = &ovRange{kind: kind, from: t, to: t}
				l.overflow[ok] = r
			}
			if t < r.from {
				r.from = t
			}
			if t > r.to {
				r.to = t
			}
			if kind == LossFinal {
				r.kind = LossFinal // the stricter proof
			}
			r.n++
			continue
		}
		l.pending[k] = lossEntry{kind: kind, atMs: now.UnixMilli()}
	}
	// len(ts) == 0 records NOTHING: a message with no real bar lost no minute.
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
	// Overflow debt: verified wholesale by range, or it stays.
	var ovMsg string
	for k, r := range l.overflow {
		if k.symbol != symbol {
			continue
		}
		if l.whole == nil {
			ovMsg += fmt.Sprintf("; %d bar(s) dropped beyond the ledger cap (%s %s..%s CT) and no range verifier is wired", r.n, k.tf, time.UnixMilli(r.from).In(ctLoc()).Format("01-02 15:04"), time.UnixMilli(r.to).In(ctLoc()).Format("15:04"))
			continue
		}
		ok, err := l.whole(symbol, k.tf, r.from, r.to, r.kind)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if ok && err == nil {
			delete(l.overflow, k)
			continue
		}
		ovMsg += fmt.Sprintf("; %d bar(s) dropped beyond the ledger cap (%s %s..%s CT) not yet verified whole", r.n, k.tf, time.UnixMilli(r.from).In(ctLoc()).Format("01-02 15:04"), time.UnixMilli(r.to).In(ctLoc()).Format("15:04"))
	}
	if len(left) == 0 && ovMsg == "" {
		return false, ""
	}
	sort.Slice(left, func(i, j int) bool {
		if left[i].t != left[j].t {
			return left[i].t < left[j].t
		}
		return left[i].tf < left[j].tf
	})
	msg := ""
	if len(left) > 0 {
		k := left[0]
		msg = fmt.Sprintf("%d lost bar(s) not yet verified present (first %s %s %s CT, %s) - %s", len(left), symbol, k.tf, time.UnixMilli(k.t).In(ctLoc()).Format("01-02 15:04"), l.pending[k].kind, ingestLossRemedy(k.tf))
	}
	if msg == "" {
		ovMsg = strings.TrimPrefix(ovMsg, "; ")
	}
	msg += ovMsg
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

// LedgerRingBars is the per-frame bar ring a lost bar ages out of (the NT8 bar
// cache, provider/ninjatrader.DefaultBarCacheMaxBars - a parity test in that
// package keeps the two equal). Used only to word the pause's duration.
const LedgerRingBars = 2500

// ctLoc is the owner's clock (America/Chicago); UTC only if the zone data is
// missing, never silently a different zone name.
func ctLoc() *time.Location {
	if l, err := time.LoadLocation("America/Chicago"); err == nil {
		return l
	}
	return time.UTC
}

// ingestLossRemedy is the owner text of an ingest_loss pause (release #55 round 3,
// review 4 D4): what the bot does by itself, what happens when that is refused
// (budget spent, feed down) - the pause then lasts until the bar ages out of the
// ring, about LedgerRingBars frames of open market - and the one thing the owner
// can do (a restart clears this in-memory loss list and re-seeds the ring from
// the store and NT8's replay).
func ingestLossRemedy(tf string) string {
	mins := 0
	var n int
	var u byte
	if _, err := fmt.Sscanf(tf, "%d%c", &n, &u); err == nil && n > 0 {
		switch u {
		case 'm':
			mins = n
		case 'h':
			mins = n * 60
		}
	}
	span := "an unknown time"
	if mins > 0 {
		h := LedgerRingBars * mins / 60
		span = fmt.Sprintf("about %d hours of open market", h)
		if h >= 48 {
			span = fmt.Sprintf("about %d hours of open market (roughly %d trading days)", h, (h+22)/23)
		}
	}
	return "the bot re-requests them from NT8 itself and the pause lifts when they are back; if that is refused (budget spent or feed down) it lasts until they age out of the " + fmt.Sprint(LedgerRingBars) + "-bar " + tf + " ring (" + span + "), or restart the bot in a flat/safe window (clears this in-memory list, re-seeds from the store and NT8)"
}
