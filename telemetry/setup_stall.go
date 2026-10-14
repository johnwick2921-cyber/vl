package telemetry

import (
	"sort"
	"sync"
)

// ── STALLED-SETUP ALARM (release #22) ──────────────────────────────────────
//
// A mentor setup kind that the evaluator keeps EMITTING but that never reaches
// the ledger (armed_orders) is stalled — it is seen every tick yet never
// trades, and it can sit silent for days (the 10-08..10-13 level-order bug:
// 0 lvl- rows while the evaluator kept finding level touches). These counters
// record the emit-vs-placed pair per trader, per setup kind, per CME
// session-day, so the alert layer can fire "seen ≥ 3, placed = 0" once a day.
//
// Counters record, never infer (canon 35): the emit site and the ledger-author
// site each increment their own counter; nothing re-derives one from the other.

// SetupStallCount is one kind's seen-vs-placed tally for the current session day.
type SetupStallCount struct {
	Emitted     int    `json:"emitted"`
	Placed      int    `json:"placed"`
	LastRefusal string `json:"last_refusal,omitempty"` // "" = none recorded (silent drop)
}

// SetupStallAlert is one kind due for a stalled-setup alert, with the counts at
// the moment the alert was claimed (so the alert text never races a later
// increment).
type SetupStallAlert struct {
	Kind        string `json:"kind"`
	Emitted     int    `json:"emitted"`
	Placed      int    `json:"placed"`
	LastRefusal string `json:"last_refusal,omitempty"` // "" = none recorded (silent drop)
}

type setupStallKey struct {
	trader string
	kind   string
}

var (
	setupStallMu      sync.Mutex
	setupStallDay     int64 // active session-day (ms at the 17:00 CT rollover); 0 = unset
	setupStallCounts  = map[setupStallKey]*SetupStallCount{}
	setupStallAlerted = map[setupStallKey]bool{}
)

// IncSetupStallEmitted records one placement intent the evaluator emitted for a
// mentor setup kind (per trader, per session day). Call at the emit site.
func IncSetupStallEmitted(traderID, kind string) {
	if kind == "" {
		return
	}
	setupStallMu.Lock()
	k := setupStallKey{traderID, kind}
	setupStallCounts[k] = bumpSetupStall(setupStallCounts[k], true)
	setupStallMu.Unlock()
}

// IncSetupStallPlaced records one mentor placement intent that reached the
// ledger (an armed_orders row was authored). Call at the ledger-author site.
func IncSetupStallPlaced(traderID, kind string) {
	if kind == "" {
		return
	}
	setupStallMu.Lock()
	k := setupStallKey{traderID, kind}
	setupStallCounts[k] = bumpSetupStall(setupStallCounts[k], false)
	setupStallMu.Unlock()
}

// SetupStallRecordRefusal records the LAST refusal reason for a (trader, kind):
// the reason the most recently emitted intent of that kind did not reach the
// ledger. Call it where the refusal is logged/counted. A kind with no refusal
// ever recorded is a SILENT drop — the alert renders it as
// "last refusal: none recorded (silent drop)".
func SetupStallRecordRefusal(traderID, kind, reason string) {
	if kind == "" || reason == "" {
		return
	}
	setupStallMu.Lock()
	k := setupStallKey{traderID, kind}
	if setupStallCounts[k] == nil {
		setupStallCounts[k] = &SetupStallCount{}
	}
	setupStallCounts[k].LastRefusal = reason
	setupStallMu.Unlock()
}

// bumpSetupStall increments one counter field, allocating the count on first use.
func bumpSetupStall(c *SetupStallCount, emitted bool) *SetupStallCount {
	if c == nil {
		c = &SetupStallCount{}
	}
	if emitted {
		c.Emitted++
	} else {
		c.Placed++
	}
	return c
}

// SetupStallAlertDue returns the setup kinds due for a stalled-setup alert for
// traderID on the current session day — emitted >= 3 and placed == 0 and not
// yet alerted — marking each returned kind alerted (once per kind per day). The
// counts are those at claim time; a later increment never rewrites the alert.
func SetupStallAlertDue(traderID string) []SetupStallAlert {
	setupStallMu.Lock()
	defer setupStallMu.Unlock()
	var due []SetupStallAlert
	for k, c := range setupStallCounts {
		if k.trader != traderID || c == nil {
			continue
		}
		if c.Emitted >= 3 && c.Placed == 0 && !setupStallAlerted[k] {
			setupStallAlerted[k] = true
			due = append(due, SetupStallAlert{Kind: k.kind, Emitted: c.Emitted, Placed: c.Placed, LastRefusal: c.LastRefusal})
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].Kind < due[j].Kind })
	return due
}

// SetupStallSnapshot returns the active session-day and a COPY of the
// per-trader/per-kind tally table (safe for the API handler goroutine).
func SetupStallSnapshot() (sessionDayMs int64, table map[string]map[string]SetupStallCount) {
	setupStallMu.Lock()
	defer setupStallMu.Unlock()
	out := make(map[string]map[string]SetupStallCount, len(setupStallCounts))
	for k, c := range setupStallCounts {
		if c == nil {
			continue
		}
		if out[k.trader] == nil {
			out[k.trader] = map[string]SetupStallCount{}
		}
		out[k.trader][k.kind] = *c
	}
	return setupStallDay, out
}

// RolloverSetupStalls sets the active session-day. On the FIRST call it adopts
// the day (no reset); on a later call whose day DIFFERS it clears the table
// (the alert latch resets with it). Same-day calls are no-ops. Call once per
// cycle from the trader loop, next to RolloverGateBlocks.
func RolloverSetupStalls(sessionDayMs int64) {
	setupStallMu.Lock()
	defer setupStallMu.Unlock()
	if setupStallDay == 0 {
		setupStallDay = sessionDayMs
		return
	}
	if sessionDayMs == setupStallDay {
		return
	}
	setupStallDay = sessionDayMs
	setupStallCounts = map[setupStallKey]*SetupStallCount{}
	setupStallAlerted = map[setupStallKey]bool{}
}

// ResetSetupStallsForTest clears the per-process counters (tests only — the
// counters are global, and a stale tally from one test must never leak into
// the next).
func ResetSetupStallsForTest() {
	setupStallMu.Lock()
	setupStallDay = 0
	setupStallCounts = map[setupStallKey]*SetupStallCount{}
	setupStallAlerted = map[setupStallKey]bool{}
	setupStallMu.Unlock()
}
