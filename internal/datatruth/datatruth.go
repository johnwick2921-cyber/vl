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
//     A check that blocks AGAIN within ReopenMergeWindow of its own clear is the
//     SAME episode re-opening (a flapping check), not a new one: no second
//     alert, the original since is kept, and the flap is counted in `reopens`.
//   - Require(names...): the wiring declares which checks MUST exist. A required
//     check nobody registered is a stub that blocks (fail closed), so a wiring
//     omission pauses entries and says so - it can never read as "nothing wrong".
//   - Counters per check (Snapshot / HealthSnapshot) feed /api/health:
//     {check: {blocked, episodes, last_reason}}.
//
// This package imports neither trader/ nor kernel/: checks are registered from
// their own packages at wiring time (the trader registers closures).
package datatruth

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"vl/logger"
)

// CheckFn is one data-truth check. blocked=true pauses new entries for symbol;
// a non-nil err is treated as blocked (fail closed) and surfaces as the reason.
type CheckFn func(symbol string, now time.Time) (blocked bool, why string, err error)

// READERS (release #55 review, "hold durations come from the AI-path specs").
// A bad bar matters to a trader only through the indicators and frames THAT
// trader's decisions read. The AI / planner / Picture / agent paths read the
// 3m/5m/15m frames and the AI indicator set; the mentor evaluator reads the 1m
// ring (EMA 34/9, the levels) plus a native 5m for the strong-day test. A scoped
// check says which readers a block concerns; Evaluate carries it on the Reason
// and the consumer (trader/data_truth_gate.go) obeys only the reasons that name
// its own reader. A reason with NO readers concerns every reader (the default,
// and what Register gives every unscoped check): the scope can only NARROW a
// block for a reader the data provably does not reach, never invent one.
const (
	ReaderAI     = "ai"
	ReaderMentor = "mentor"
)

// ScopedCheckFn is a CheckFn that also says WHO its block is for. Two kinds of
// scope ride in the one returned []string, told apart by their shape (the
// integration of release #55 m450 "readers" and m451 "scopes"; Evaluate splits
// them onto Reason.Readers and Reason.Scopes):
//
//   - READERS (m450): the plain names ReaderAI / ReaderMentor name the entry
//     paths a block concerns (see above); none = every path.
//   - SCOPES (m451): ScopeTrader / ScopeAccount tokens name the traders or
//     accounts the block is FOR. The base contract is per symbol: a block stops
//     every trader on the symbol. That is right for a check about the symbol's
//     DATA (a bad print, a missing bar), and wrong for a check about ONE account
//     (the broker view, the fills recorded on one account): trader B's account
//     view is fine when trader A's disagrees, and pausing B is pausing on data
//     that is not lost. none = everyone on the symbol (the safe default, and what
//     every error or panic means).
//
// blocked=false returns no scopes.
type ScopedCheckFn func(symbol string, now time.Time) (blocked bool, why string, scopes []string, err error)

// ScopeTrader / ScopeAccount build the scope tokens a ScopedCheckFn returns and
// AppliesTo reads. Accounts are compared case-insensitively.
func ScopeTrader(id string) string { return "trader:" + strings.TrimSpace(id) }

func ScopeAccount(acct string) string { return "acct:" + strings.ToUpper(strings.TrimSpace(acct)) }

// splitTokens separates the reader names (no ':') from the trader/account scope
// tokens (contain ':') a check returned.
func splitTokens(tokens []string) (readers, scopes []string) {
	for _, t := range tokens {
		if strings.Contains(t, ":") {
			scopes = append(scopes, t)
		} else if t != "" {
			readers = append(readers, t)
		}
	}
	return readers, scopes
}

// Reason is one blocking check's verdict for a symbol.
type Reason struct {
	Check   string `json:"check"`
	Symbol  string `json:"symbol"`
	Detail  string `json:"detail"`
	SinceMs int64  `json:"since_ms"` // when this episode opened (ms UTC)
	// Readers names the entry paths this block concerns ("ai", "mentor"); empty =
	// every path. See ScopedCheckFn.
	Readers []string `json:"readers,omitempty"`
	// Scopes: who this block is for (see ScopedCheckFn). Empty = every trader on
	// the symbol.
	Scopes []string `json:"scopes,omitempty"`
}

// Concerns reports whether the reason applies to reader (an empty Readers list
// concerns everyone). A reader token this build does not recognise (a typo, a
// future name, a wrong case) concerns EVERYONE: Evaluate has already announced
// the block, so a token the consumer cannot match must never turn it into a
// block for nobody (m55r3 defect: an unrecognised token silently dropped an
// announced block - the owner was told entries were paused while every path
// admitted). A scope can only narrow to a RECOGNISED reader.
func (r Reason) Concerns(reader string) bool {
	if len(r.Readers) == 0 {
		return true
	}
	for _, x := range r.Readers {
		if x == reader || !knownReader(x) {
			return true
		}
	}
	return false
}

// knownReader: the reader names Concerns can match.
func knownReader(x string) bool { return x == ReaderAI || x == ReaderMentor }

// knownScope: a scope token AppliesTo can match - exactly "trader:<id>" or
// "acct:<ACCOUNT>" with a non-empty tail.
func knownScope(s string) bool {
	for _, pre := range []string{"trader:", "acct:"} {
		if strings.HasPrefix(s, pre) {
			return strings.TrimSpace(s[len(pre):]) != ""
		}
	}
	return false
}

// AppliesTo reports whether the block is for the asking trader: it is, when the
// reason carries no scope (a whole-symbol block), or names this trader id or
// this trader's bound account. A trader with no known account only matches by id.
// A scope token this build does not recognise ("trader:" with no id, "acct:" with
// no account, any other prefix) applies to EVERYONE - fail closed, the same rule
// as Concerns: it can only narrow to a recognised trader or account, never to
// nobody.
func (r Reason) AppliesTo(traderID, account string) bool {
	if len(r.Scopes) == 0 {
		return true
	}
	tok := ScopeTrader(traderID)
	acc := ""
	if strings.TrimSpace(account) != "" {
		acc = ScopeAccount(account)
	}
	for _, s := range r.Scopes {
		if !knownScope(s) || s == tok || (acc != "" && s == acc) {
			return true
		}
	}
	return false
}

// CheckStat is the per-check counter block exported on /api/health.
type CheckStat struct {
	Blocked    bool   `json:"blocked"`     // any symbol currently blocked by this check
	Episodes   int64  `json:"episodes"`    // episodes opened since boot (all symbols)
	Reopens    int64  `json:"reopens"`     // re-blocks merged into an earlier episode (flaps)
	LastReason string `json:"last_reason"` // detail of the most recent block
	// Counted is the number of distinct records the check noted WITHOUT pausing
	// (Count): real but immaterial disagreements, kept visible instead of lost.
	Counted int64 `json:"counted"`
	// Scopes: who the standing blocks are FOR (ScopeTrader / ScopeAccount tokens).
	// Empty with Blocked=true = the whole symbol (m451).
	Scopes []string `json:"scopes,omitempty"`
}

// ReopenMergeWindow: a check that blocks again within this long of its own
// clear re-opens the previous episode instead of opening (and alerting) a new
// one. Measured on the corpus: cross_frame opened 29 episodes in 10 days, in
// bursts at the HTF cadence (10-20 05:45, 06:00, 06:15, 06:30, 06:45).
var ReopenMergeWindow = 30 * time.Minute

type episode struct {
	open      bool
	sinceMs   int64
	clearedMs int64 // when the episode last measured clear (0 = never)
	detail    string
	scopes    []string // who the latest block was for (empty = the whole symbol)
}

type check struct {
	name     string
	fn       ScopedCheckFn
	counted  int64
	seen     map[string]struct{}
	eps      map[string]*episode // per symbol
	episodes int64
	reopens  int64
	last     string
	stub     bool // registered by Require, never replaced by a real check
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
	RegisterScoped(name, func(symbol string, now time.Time) (bool, string, []string, error) {
		b, why, err := fn(symbol, now)
		return b, why, nil, err
	})
}

// RegisterScoped adds (or replaces) a check whose blocks name the readers they
// concern (m450) or the traders/accounts they are for (m451) rather than the
// whole symbol. Everything else is Register. See ScopedCheckFn.
func RegisterScoped(name string, fn ScopedCheckFn) {
	if name == "" || fn == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if c, ok := checks[name]; ok {
		c.fn, c.stub = fn, false
		return
	}
	checks[name] = &check{name: name, fn: fn, eps: map[string]*episode{}}
}

// Require declares checks that MUST be registered. A required name with no real
// check yet gets a stub that BLOCKS with "required check not wired" (fail
// closed): a wiring omission - a nil input the registration skipped - pauses
// entries and names what is missing instead of failing open. A later Register
// of the same name replaces the stub.
func Require(names ...string) {
	mu.Lock()
	defer mu.Unlock()
	for _, n := range names {
		if n == "" {
			continue
		}
		if _, ok := checks[n]; ok {
			continue
		}
		name := n
		checks[n] = &check{name: n, eps: map[string]*episode{}, stub: true,
			fn: func(string, time.Time) (bool, string, []string, error) {
				return true, "required data-truth check " + name + " is not wired (its inputs were never supplied) - fail closed", nil, nil
			}}
	}
}

// SetNotifier installs the owner-alert sender. kind is a ready-made owner-push
// key "data_truth_<check>|<symbol>|<sinceMs>": its prefix before "|" is the
// rate-limit kind - one bucket PER CHECK, so a flapping check can never starve
// another check's alert (m438 TestOneKindCannotStarveAnother) - and the whole
// string is the per-episode dedupe key, so every episode pushes once and a later
// episode is never swallowed by the 24h dedupe. nil detaches (log only).
func SetNotifier(fn func(kind, text string)) {
	mu.Lock()
	notifier = fn
	mu.Unlock()
}

// Evaluate runs every registered check for symbol. blocked is true when at
// least one check blocks; reasons names each blocking check (sorted by check
// name). With no check registered and none required nothing is blocked - the
// wiring declares what must exist with Require, so a skipped registration of a
// required check blocks instead of passing.
func Evaluate(symbol string, now time.Time) (blocked bool, reasons []Reason) {
	mu.Lock()
	names := make([]string, 0, len(checks))
	for n := range checks {
		names = append(names, n)
	}
	sort.Strings(names)
	fns := make([]ScopedCheckFn, len(names))
	for i, n := range names {
		fns[i] = checks[n].fn
	}
	mu.Unlock()

	type verdict struct {
		name, why string
		blocked   bool
		readers   []string
		scopes    []string
	}
	vs := make([]verdict, len(names))
	for i, n := range names {
		b, why, rd, sc := runCheck(fns[i], symbol, now)
		vs[i] = verdict{name: n, why: why, blocked: b, readers: rd, scopes: sc}
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
			if !ep.open && ep.clearedMs != 0 && now.UnixMilli()-ep.clearedMs < ReopenMergeWindow.Milliseconds() {
				// a flap: the same episode re-opening, not a new one
				ep.open = true
				c.reopens++
				logger.Warnf("datatruth: %s blocks %s AGAIN %dms after it cleared - merged into the open-since-%d episode, no new alert: %s",
					v.name, symbol, now.UnixMilli()-ep.clearedMs, ep.sinceMs, v.why)
			} else if !ep.open {
				ep.open, ep.sinceMs, ep.detail = true, now.UnixMilli(), v.why
				c.episodes++
				alerts = append(alerts, alert{
					kind: fmt.Sprintf("data_truth_%s|%s|%d", v.name, symbol, ep.sinceMs),
					text: fmt.Sprintf("DATA TRUTH: new entries PAUSED for %s%s - %s: %s (exits, stops and protection are unaffected; entries resume automatically when the data measures clear)", symbol, readersNote(v.readers), v.name, v.why),
				})
			}
			c.last = v.why
			ep.detail = v.why
			ep.scopes = v.scopes
			reasons = append(reasons, Reason{Check: v.name, Symbol: symbol, Detail: v.why, SinceMs: ep.sinceMs, Readers: v.readers, Scopes: v.scopes})
		} else if ep.open {
			ep.open, ep.clearedMs = false, now.UnixMilli()
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

// readersNote renders which entry paths a scoped block concerns for the alert.
func readersNote(readers []string) string {
	if len(readers) == 0 {
		return ""
	}
	names := make([]string, len(readers))
	for i, r := range readers {
		switch r {
		case ReaderMentor:
			names[i] = "mentor traders"
		case ReaderAI:
			names[i] = "AI/planner traders"
		default:
			names[i] = r
		}
	}
	return " (" + strings.Join(names, " + ") + ")"
}

// runCheck calls one check, converting an error or a panic into a block FOR
// EVERYONE on the symbol and EVERY reader (an error carries no scope: the check
// could not even say who it was about, or which path the data reaches). The
// returned tokens are split onto readers (m450) and scopes (m451).
func runCheck(fn ScopedCheckFn, symbol string, now time.Time) (blocked bool, why string, readers, scopes []string) {
	defer func() {
		if r := recover(); r != nil {
			blocked, why, readers, scopes = true, fmt.Sprintf("check panicked (fail closed): %v", r), nil, nil
		}
	}()
	b, w, tok, err := fn(symbol, now)
	if err != nil {
		return true, fmt.Sprintf("cannot judge (fail closed): %v", err), nil, nil
	}
	if !b {
		return false, w, nil, nil
	}
	rd, sc := splitTokens(tok)
	return true, w, rd, sc
}

// countSeenCap bounds the per-check distinct-record memory of Count.
const countSeenCap = 4096

// Count notes ONE distinct record (key) a check saw but deliberately did not
// pause for: a real yet immaterial disagreement. It is surfaced on /api/health
// (counted) and logged once per key, so an immaterial mismatch is visible and
// never silently dropped. Repeated calls with the same key are idempotent (the
// checks re-run every Evaluate over the same scan window). An unregistered check
// name is ignored.
func Count(checkName, key, text string) {
	mu.Lock()
	c, ok := checks[checkName]
	if !ok {
		mu.Unlock()
		return
	}
	if c.seen == nil {
		c.seen = map[string]struct{}{}
	}
	if _, dup := c.seen[key]; dup {
		mu.Unlock()
		return
	}
	if len(c.seen) >= countSeenCap {
		c.seen = map[string]struct{}{}
	}
	c.seen[key] = struct{}{}
	c.counted++
	mu.Unlock()
	logger.Warnf("datatruth: %s counted, entries NOT paused (immaterial to every indicator): %s", checkName, text)
}

// Wired reports whether a REAL check (not a Require stub) is registered under
// name.
func Wired(name string) bool {
	mu.Lock()
	defer mu.Unlock()
	c, ok := checks[name]
	return ok && !c.stub
}

// Snapshot returns the per-check counters (sorted map by name when encoded).
func Snapshot() map[string]CheckStat {
	mu.Lock()
	defer mu.Unlock()
	out := make(map[string]CheckStat, len(checks))
	for n, c := range checks {
		st := CheckStat{Episodes: c.episodes, Reopens: c.reopens, LastReason: c.last, Counted: c.counted}
		whole := false
		for _, ep := range c.eps {
			if ep.open {
				st.Blocked = true
				if len(ep.scopes) == 0 {
					whole = true
				}
				st.Scopes = append(st.Scopes, ep.scopes...)
			}
		}
		if whole {
			st.Scopes = nil // one whole-symbol block: the union is everyone
		} else {
			sort.Strings(st.Scopes)
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
		m := map[string]any{"blocked": s.Blocked, "episodes": s.Episodes, "reopens": s.Reopens, "last_reason": s.LastReason, "counted": s.Counted}
		if len(s.Scopes) > 0 {
			m["scopes"] = s.Scopes
		}
		out[n] = m
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
