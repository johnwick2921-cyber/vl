// Package datatruth is the DATA-TRUTH CONTRACT (release #54, m446).
//
// OWNER PRINCIPLE (2026-10-29): "this is a financial product, not a game -
// nothing related to data loss or incorrect data can continue to trade."
//
// A data-truth check answers ONE question for ONE symbol: can the bars the
// decision is about to read be trusted right now? When the answer is no, NEW
// ENTRIES for that symbol pause. Nothing else: a check never exits, never moves
// a stop, never touches BE/trail, closes, protection or flatten - those paths
// never consult this package.
//
// The contract (the integrator wires each entry path to Evaluate):
//
//   - Register(name, fn): a check returns (blocked, why, err). An error is a
//     BLOCK - a check that cannot judge fails CLOSED, never open. A panic in a
//     check is recovered and is also a block.
//   - Evaluate(symbol, now): runs every registered check and returns whether
//     the symbol is blocked plus one Reason per blocking check.
//   - Episodes: per (check, symbol). The first blocked evaluation opens an
//     episode (one WARN line and one owner push through the injectable
//     notifier); repeats inside the episode are silent. The episode closes - and
//     entries resume - ONLY when the check is measured clear (blocked=false,
//     err=nil). Nothing times out; a clear is never inferred from silence.
//   - Counters per check (Snapshot / HealthSnapshot) feed /api/health:
//     {check: {blocked, episodes, last_reason}}.
//
// This package imports neither trader/ nor kernel/: checks are registered from
// their own packages at wiring time (the trader registers closures).
package datatruth

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"vl/logger"
)

// CheckFn is one data-truth check. blocked=true pauses new entries for symbol;
// a non-nil err is treated as blocked (fail closed) and surfaces as the reason.
type CheckFn func(symbol string, now time.Time) (blocked bool, why string, err error)

// Reason is one blocking check's verdict for a symbol.
type Reason struct {
	Check   string `json:"check"`
	Symbol  string `json:"symbol"`
	Detail  string `json:"detail"`
	SinceMs int64  `json:"since_ms"` // when this episode opened (ms UTC)
}

// CheckStat is the per-check counter block exported on /api/health.
type CheckStat struct {
	Blocked    bool   `json:"blocked"`     // any symbol currently blocked by this check
	Episodes   int64  `json:"episodes"`    // episodes opened since boot (all symbols)
	LastReason string `json:"last_reason"` // detail of the most recent block
}

type episode struct {
	open    bool
	sinceMs int64
	detail  string
}

type check struct {
	name     string
	fn       CheckFn
	eps      map[string]*episode // per symbol
	episodes int64
	last     string
}

var (
	mu       sync.Mutex
	checks   = map[string]*check{}
	notifier func(kind, text string)
)

// Register adds (or replaces) a check. Re-registering a name keeps its episode
// state, so a re-wire cannot reopen an episode that is already open.
func Register(name string, fn CheckFn) {
	if name == "" || fn == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if c, ok := checks[name]; ok {
		c.fn = fn
		return
	}
	checks[name] = &check{name: name, fn: fn, eps: map[string]*episode{}}
}

// SetNotifier installs the owner-alert sender. kind is a ready-made owner-push
// key "data_truth|<check>|<symbol>|<sinceMs>": its prefix before "|" is the
// rate-limit kind and the whole string is the per-episode dedupe key, so every
// episode pushes once and a later episode is never swallowed by the 24h dedupe.
// nil detaches (log only).
func SetNotifier(fn func(kind, text string)) {
	mu.Lock()
	notifier = fn
	mu.Unlock()
}

// Evaluate runs every registered check for symbol. blocked is true when at
// least one check blocks; reasons names each blocking check (sorted by check
// name). With no check registered nothing is blocked - the wiring, not this
// package, decides which checks exist.
func Evaluate(symbol string, now time.Time) (blocked bool, reasons []Reason) {
	mu.Lock()
	names := make([]string, 0, len(checks))
	for n := range checks {
		names = append(names, n)
	}
	sort.Strings(names)
	fns := make([]CheckFn, len(names))
	for i, n := range names {
		fns[i] = checks[n].fn
	}
	mu.Unlock()

	type verdict struct {
		name, why string
		blocked   bool
	}
	vs := make([]verdict, len(names))
	for i, n := range names {
		b, why := runCheck(fns[i], symbol, now)
		vs[i] = verdict{name: n, why: why, blocked: b}
	}

	type alert struct{ kind, text string }
	var alerts []alert
	mu.Lock()
	notify := notifier
	for _, v := range vs {
		c := checks[v.name]
		ep := c.eps[symbol]
		if ep == nil {
			ep = &episode{}
			c.eps[symbol] = ep
		}
		if v.blocked {
			blocked = true
			if !ep.open {
				ep.open, ep.sinceMs, ep.detail = true, now.UnixMilli(), v.why
				c.episodes++
				alerts = append(alerts, alert{
					kind: fmt.Sprintf("data_truth|%s|%s|%d", v.name, symbol, ep.sinceMs),
					text: fmt.Sprintf("DATA TRUTH: new entries PAUSED for %s - %s: %s (exits, stops and protection are unaffected; entries resume automatically when the data measures clear)", symbol, v.name, v.why),
				})
			}
			c.last = v.why
			ep.detail = v.why
			reasons = append(reasons, Reason{Check: v.name, Symbol: symbol, Detail: v.why, SinceMs: ep.sinceMs})
		} else if ep.open {
			ep.open = false
			logger.Infof("datatruth: %s CLEAR for %s after %dms (was: %s) - new entries resume", v.name, symbol, now.UnixMilli()-ep.sinceMs, ep.detail)
		}
	}
	mu.Unlock()

	for _, a := range alerts {
		logger.Warnf("datatruth: %s", a.text)
		if notify != nil {
			notify(a.kind, a.text)
		}
	}
	return blocked, reasons
}

// runCheck calls one check, converting an error or a panic into a block.
func runCheck(fn CheckFn, symbol string, now time.Time) (blocked bool, why string) {
	defer func() {
		if r := recover(); r != nil {
			blocked, why = true, fmt.Sprintf("check panicked (fail closed): %v", r)
		}
	}()
	b, w, err := fn(symbol, now)
	if err != nil {
		return true, fmt.Sprintf("cannot judge (fail closed): %v", err)
	}
	return b, w
}

// Snapshot returns the per-check counters (sorted map by name when encoded).
func Snapshot() map[string]CheckStat {
	mu.Lock()
	defer mu.Unlock()
	out := make(map[string]CheckStat, len(checks))
	for n, c := range checks {
		st := CheckStat{Episodes: c.episodes, LastReason: c.last}
		for _, ep := range c.eps {
			if ep.open {
				st.Blocked = true
			}
		}
		out[n] = st
	}
	return out
}

// HealthSnapshot is Snapshot shaped for the /api/health "data_truth" key.
func HealthSnapshot() map[string]any {
	snap := Snapshot()
	out := make(map[string]any, len(snap))
	for n, s := range snap {
		out[n] = map[string]any{"blocked": s.Blocked, "episodes": s.Episodes, "last_reason": s.LastReason}
	}
	return out
}

// Reset drops every registered check, episode and the notifier. TEST SEAM:
// production never calls it (checks live for the process).
func Reset() {
	mu.Lock()
	checks = map[string]*check{}
	notifier = nil
	mu.Unlock()
}
