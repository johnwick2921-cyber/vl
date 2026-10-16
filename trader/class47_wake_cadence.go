package trader

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"vl/kernel"
	"vl/safe"
)

// ── CLASS 47 (2026-09-02) — WAKE CADENCE ────────────────────────────────────
//
// Measured on the 7-day tape and today's session (a5a53bec cadence audit,
// re-measured in this wave):
//
//   · 60 level_event wake re-plans in 7 days; 33 arm rows written, 23 ever
//     placed, 9 ever working/filled. Most wakes buy a plan version nobody
//     trades.
//   · Today's wakes fire at 08:42:30 · 09:12:30 · 09:42:30 · 10:14:30 ·
//     10:44:30 · 11:15 · 11:45 · 12:16:30 · 12:48:29 · 13:18:29 · 13:48:29 ·
//     14:20:29 — a clean ~30-minute drumbeat. The wake CONDITION is
//     continuously true; the existing wake_min_interval_min throttle is the
//     only thing pacing them. NY produced 12 plan versions on that pattern.
//   · A wake at 14:20:29 sits 10 minutes from the NY last-entry cutoff (14:30)
//     and 25 from the flat: a max-reasoning read whose plan can never be
//     entered.
//
// PROMOTED TO ENFORCE (owner ruling 2026-09-03). The two cutoffs shipped
// WARN-first, recording what a suppression WOULD have skipped so the ruling
// could rest on counts. One morning of live observation supplied them:
//
//   ⏱ wake would_skip: 24 min to flat (cutoff 25m) — 1h S/D zone Demand
//     [29101.75–29187.25] on LONDON. WARN-first: the wake PROCEEDS.
//   ⏱ wake would_skip: cooldown 21 min since the last wake-authored version
//     (cooldown 30m) — seated Supply·1h invalidated
//
// The first wrote LONDON v2 at 08:15:44 for a session that flattens at 08:30 —
// a ~500s max-reasoning read whose plan could never be entered. Both cutoffs
// now SKIP the wake. The counters are unchanged, so the same keys keep counting
// (they now count skips rather than would-be skips, which the log line says).
//
// SCOPE: wakes only. A scheduled read, a death re-plan and an owner reset are
// untouched — WakeCadenceGoverns is the single place that decides, and a test
// pins the list both ways.

// WakeCutoffMinDefault — a wake this close to the session flat cannot produce a
// tradeable plan: the last-entry cutoff is flat−15, and the p90 planner call is
// ~9.3 min, so a read starting inside ~25 min of the flat lands after the gate
// has already closed. Env WAKE_CUTOFF_MIN; 0 disables the check.
const WakeCutoffMinDefault = 25

// WakeCooldownMinDefault — minutes since the last wake-AUTHORED plan version.
// Deliberately distinct from wake_min_interval_min (which paces wake ATTEMPTS
// from the last attempt): this measures from the last version a wake actually
// WROTE, so a wake whose read failed or kept the active plan does not start the
// clock. Env WAKE_COOLDOWN_MIN; 0 disables.
const WakeCooldownMinDefault = 30

func wakeCutoffMinutes() int   { return envPosInt("WAKE_CUTOFF_MIN", WakeCutoffMinDefault) }
func wakeCooldownMinutes() int { return envPosInt("WAKE_COOLDOWN_MIN", WakeCooldownMinDefault) }

// envPosInt resolves a non-negative integer knob (0 = feature off).
func envPosInt(name string, def int) int {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

// minutesToSessionFlat returns whole minutes from now to this session's window
// end (the flat), wrap-aware for midnight-spanning sessions. ok=false outside
// the window or on malformed registry times — never invent a deadline.
func minutesToSessionFlat(now time.Time, sess *kernel.SessionDef) (int, bool) {
	if sess == nil {
		return 0, false
	}
	startMin, okS := hhmmToMin(sess.WindowStartCT)
	endMin, okE := hhmmToMin(sess.WindowEndCT)
	if !okS || !okE {
		return 0, false
	}
	sessLen := ((endMin-startMin)%1440 + 1440) % 1440
	nowOff := ((ctMinutesNow(now)-startMin)%1440 + 1440) % 1440
	if nowOff >= sessLen {
		return 0, false // outside the window
	}
	return sessLen - nowOff, true
}

// deferredWakeRefires holds wake re-fire closures registered while a planner
// stream was open, keyed by (trader, session) so at most ONE pending re-fire is
// queued per trader per session. releasePlannerRead drains them the moment the
// LAST claim clears, so a wake deferred by "stream open" re-fires when the
// stream closes instead of waiting for the next monitor cycle (W26 F-1: 10-14
// the wake sat parked behind another trader's 2h stream). W26-CTO-GATES F4: the
// map dedupes the ~60-closure herd that the old slice accumulated over a 2h
// stream.
var deferredWakeRefiresMu sync.Mutex
var deferredWakeRefires = map[string]func(){}

// wakeRefireTestHook is a test-only seam invoked after a deferred-wake re-fire's
// ASYNC re-read completes (nil in production). Tests set it to a channel close
// and WAIT on it instead of polling the wake-state fields, so the race detector
// never sees the re-fire goroutine's write race the test's read — and the test's
// FuturesBarsProvider restore never races the re-read that reads it.
var wakeRefireTestHook func()

// deferredWakeRefireKey builds the dedupe key: one pending re-fire per
// (trader, trade date, session) — the level-event wake is the only wake class
// that defers on stream-open, and a later cycle's re-registration for the same
// trader/session replaces nothing (the first pending closure already re-checks
// the reason on the re-run).
func deferredWakeRefireKey(traderID, tradeDate, session string) string {
	return traderID + "|" + tradeDate + "|" + session
}

// registerDeferredWakeRefire queues a wake re-fire for the next stream close.
// Same key → the existing closure stands (no herd).
func registerDeferredWakeRefire(key string, f func()) {
	deferredWakeRefiresMu.Lock()
	defer deferredWakeRefiresMu.Unlock()
	if _, exists := deferredWakeRefires[key]; exists {
		return
	}
	deferredWakeRefires[key] = f
}

// drainDeferredWakeRefires returns and clears the pending re-fires. The caller
// runs them; a re-fire that finds another stream open re-registers itself.
func drainDeferredWakeRefires() []func() {
	deferredWakeRefiresMu.Lock()
	defer deferredWakeRefiresMu.Unlock()
	out := make([]func(), 0, len(deferredWakeRefires))
	for _, f := range deferredWakeRefires {
		out = append(out, f)
	}
	deferredWakeRefires = map[string]func(){}
	return out
}

// fireDeferredWakeRefires runs every pending re-fire once the stream is closed.
// Called from releasePlannerRead; each closure is its own goroutine (a wake
// re-read is async, W6-C), and a closure that still sees an open stream simply
// re-registers and waits for the next close.
func fireDeferredWakeRefires() {
	if _, open := anyPlannerStreamOpen(); open {
		return
	}
	for _, f := range drainDeferredWakeRefires() {
		safe.GoNamed("planner-wake-refire", f)
	}
}

// anyPlannerStreamOpen reports whether ANY planner read is in flight, for any
// trading session and any trader in this process, and names one.
//
// F3: the claim key is per (trader, trade_date, session)
// (auto_trader_planner.go — `claimPlannerRead(store.MakePlanIDForTrader(...))`),
// so a LONDON read and an NY read hold different claims and stream
// concurrently. Today 08:01:06 opened a second max-reasoning stream while the
// 07:51:06 one was still running — the LONDON→NY handover overlap.
//
// A WAKE defers on this. A SCHEDULED read never does: the naive "defer on any
// stream" rule would have parked today's NY session read behind LONDON's, which
// is strictly worse than an overlap.
func anyPlannerStreamOpen() (string, bool) {
	held := ""
	plannerReadInFlight.Range(func(k, _ any) bool {
		if s, ok := k.(string); ok {
			held = s
			return false
		}
		return true
	})
	return held, held != ""
}

// WakeCadenceGoverns reports whether a trigger is a WAKE, and so subject to the
// cadence cutoffs. Everything else — scheduled reads, death re-plans, owner
// resets, the Sunday weekly read — is untouched by them.
func WakeCadenceGoverns(trigger string) bool {
	switch strings.TrimSpace(trigger) {
	case "level_event", "structure_mss":
		return true
	}
	return false
}

// WakeCadenceDecision is one wake's cadence verdict, pure so the two live cases
// can be pinned as fixtures rather than described in a comment.
//
// HaveFlat / HaveLastWakeVersion are separate from their numbers on purpose: an
// unreadable session window and a session with no prior wake-authored version
// must NEVER manufacture a skip out of a zero (A24 — a plausible zero is how a
// gate starts refusing things nobody decided to refuse).
type WakeCadenceDecision struct {
	Session string
	Desc    string

	MinutesToFlat int
	HaveFlat      bool
	CutoffMin     int

	SinceLastWakeVersionMin int
	HaveLastWakeVersion     bool
	CooldownMin             int

	// FAST-MARKET EXEMPTION (owner ruling 2026-09-03). FastMarketATR is the
	// measured drift since the last plan write, in ATR5m multiples; the
	// threshold is the resolved FAST_MARKET_ATR knob. At or above it the
	// COOLDOWN is bypassed — F3 downgrades reasoning for exactly these reads
	// because they matter, so refusing them on a cadence rule inverts the
	// intent. The last-window CUTOFF is not exempted: a re-plan with 20
	// minutes left is a re-plan with 20 minutes left, fast or not.
	//
	// A zero threshold means "unresolved", and must NOT make every wake a
	// fast-market wake — 0 >= 0 would silently disable the cooldown.
	FastMarketATR       float64
	FastMarketThreshold float64
}

// FastMarketBypass reports whether the drift is a fast market by the resolved
// threshold, and so exempt from the cooldown.
func (d WakeCadenceDecision) FastMarketBypass() bool {
	return d.FastMarketThreshold > 0 && d.FastMarketATR >= d.FastMarketThreshold
}

// BypassNote is the log fragment when the cooldown was waived, empty otherwise.
func (d WakeCadenceDecision) BypassNote() string {
	if !d.FastMarketBypass() {
		return ""
	}
	return fmt.Sprintf("cooldown bypassed: fast market %.1f×ATR", d.FastMarketATR)
}

// SkipForCutoff — the wake starts too close to the flat to produce a plan that
// can still be entered. The boundary belongs to the wake: the rule is < cutoff.
func (d WakeCadenceDecision) SkipForCutoff() bool {
	return d.CutoffMin > 0 && d.HaveFlat && d.MinutesToFlat < d.CutoffMin
}

// SkipForCooldown — a wake-authored version is younger than the cooldown.
func (d WakeCadenceDecision) SkipForCooldown() bool {
	if d.FastMarketBypass() {
		return false // owner ruling 2026-09-03 — see FastMarketATR
	}
	return d.CooldownMin > 0 && d.HaveLastWakeVersion && d.SinceLastWakeVersionMin < d.CooldownMin
}

// Reason renders whichever rule fired, in the enforcing wording.
func (d WakeCadenceDecision) Reason() string {
	switch {
	case d.SkipForCutoff():
		return fmt.Sprintf("%d min to flat (cutoff %dm) — SKIPPED", d.MinutesToFlat, d.CutoffMin)
	case d.SkipForCooldown():
		return fmt.Sprintf("cooldown %d min since the last wake-authored version (cooldown %dm) — SKIPPED", d.SinceLastWakeVersionMin, d.CooldownMin)
	}
	return ""
}

// wakeCutoffLine / wakeCooldownLine / wakeStreamDeferLine are the pure log-line
// builders (A9: loud, and fixture-tested for wording).
func wakeCutoffLine(session, desc string, minsToFlat, cutoff int, n int) string {
	return fmt.Sprintf("⏱ wake SKIPPED: %d min to flat (cutoff %dm) — %s on %s. A read starting here lands after the last-entry gate closes. Recorded n=%d (class 47, enforcing since 2026-09-03)",
		minsToFlat, cutoff, desc, session, n)
}

func wakeCooldownLine(session, desc string, sinceMin, cooldown int, n int) string {
	return fmt.Sprintf("⏱ wake SKIPPED: cooldown %d min since the last wake-authored version (cooldown %dm) — %s on %s. Recorded n=%d (class 47, enforcing since 2026-09-03)",
		sinceMin, cooldown, desc, session, n)
}

func wakeStreamDeferLine(session, desc, heldKey string) string {
	return fmt.Sprintf("⏱ wake DEFERRED: a planner stream is already open (%s) — %s on %s. Scheduled reads never defer; this is a wake (class 47)",
		heldKey, desc, session)
}

// WakeCadenceBootLine (F5) — every value READ from its resolver (A12/A24: no
// literals in a boot line).
func WakeCadenceBootLine() string {
	cutoff, cooldown := wakeCutoffMinutes(), wakeCooldownMinutes()
	return fmt.Sprintf("wakes: cutoff=%dm(%s) cooldown=%dm(%s, fast-market≥%.1f×ATR exempt) cross-session=%s stale-arm-expiry=%s (class 47) — cutoffs govern LEVEL_EVENT/structure_mss wakes ONLY; scheduled reads, death re-plans and owner resets are untouched; the cutoff is NOT exempted by a fast market",
		cutoff, enforceWord(cutoff), cooldown, enforceWord(cooldown), fastMarketATR(),
		onOffWord(true), onOffWord(true))
}

// enforceWord states whether a cutoff is live, READ from its own value: 0
// disables the check, so it cannot claim to enforce.
func enforceWord(minutes int) string {
	if minutes > 0 {
		return "enforce"
	}
	return "off"
}

func onOffWord(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
