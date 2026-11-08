package datareport

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"vl/kernel"
)

// m469 WHY NO TRADE: the first thing the nightly report says, one line per
// trader, so the owner can read "why no trade today" without a dozen queries.
//
//   - A mentor-mode trader answers from its funnel (setups, placed, filled and
//     the refusals that stopped the rest). The funnel lives in memory: its
//     refusal figures are diffed against the 17:00 roll by the trader, and
//     when the trader started inside the session the line says "since boot".
//   - An AI-planner trader answers from decision_records (durable, survives a
//     restart): how many of the session's cycles failed and the main reason.
//     Mentor-mode traders are left out of this count: their rows are written
//     with success=1 meaning "skipped", which says nothing about the AI.
//     A guardrail skip (execution_status 'guardrail_skip': a cycle a gate held
//     before or after a good AI answer - superseded_wait, stale_bar_discarded,
//     market_data_unavailable ...) is also success=0 with no error class, but
//     the AI did not fail: it is counted apart as "skipped by a guardrail".

const whyTopN = 3 // refusals shown per kind

// TraderFunnel is one trader's why-no-trade inputs, read from the running
// trader (trader.AutoTrader.WhyNoTradeInput).
type TraderFunnel struct {
	ID, Name string
	// Mentor is true for a mentor-mode trader. False = an AI-planner trader:
	// its line is read from decision_records and the fields below are unused.
	Mentor bool
	// Have is false when no bar has been evaluated since the trader started.
	Have bool
	// DayKey is the session day (kernel.CMESessionDayKey) the counts belong to.
	DayKey string
	// Since is when this session's counting began: the 17:00 roll, or the
	// trader's start when it started inside the session.
	Since time.Time
	// CountersRestarted: a refusal ledger was rebuilt inside the session, so
	// the refusal figures are partial.
	CountersRestarted bool

	Setups, Authored, Placed, Filled int
	KernelRefusals, TraderRefusals   map[string]int
}

// AICycles is an AI-planner trader's decision cycles in the session.
type AICycles struct {
	// Total is every decision cycle in the session; Failed the ones that
	// failed; Skipped the guardrail skips (not failures, not in Failed).
	Total, Failed, Skipped int
	TopClass      string // the most frequent failure reason ("402", ...)
	Err           string // the query failed: the line prints n/a
}

// TraderWhy is one trader's why-no-trade line inputs.
type TraderWhy struct {
	TraderFunnel
	AI *AICycles // set for AI-planner traders only
}

// collectWhy reads the AI cycles of every AI-planner trader; mentor traders
// carry their funnel as handed in.
func collectWhy(db *gorm.DB, sess Session, traders []TraderFunnel) []TraderWhy {
	out := make([]TraderWhy, 0, len(traders))
	for _, t := range traders {
		w := TraderWhy{TraderFunnel: t}
		if !t.Mentor {
			ai := aiCycles(db, t.ID, sess)
			w.AI = &ai
		}
		out = append(out, w)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Mentor != out[j].Mentor {
			return out[i].Mentor // mentor traders first
		}
		return whyName(out[i].TraderFunnel) < whyName(out[j].TraderFunnel)
	})
	return out
}

type aiRow struct {
	Success         bool
	ExecutionStatus string
	ErrorClass      string
	ErrorMessage    string
}

// isGuardrailSkip: the row a gate wrote for a held cycle (stampGuardrailSkip in
// trader/auto_trader_loop.go: execution_status 'guardrail_skip', error_message
// 'guardrail_skip: <reason>'). Either mark is enough.
func isGuardrailSkip(r aiRow) bool {
	return r.ExecutionStatus == "guardrail_skip" || strings.HasPrefix(r.ErrorMessage, "guardrail_skip:")
}

// aiCycles counts one trader's decision_records rows in the session window.
// timestamp is stored in UTC, so the bounds are bound in UTC (the store's own
// range queries do the same).
func aiCycles(db *gorm.DB, traderID string, sess Session) AICycles {
	if db == nil {
		return AICycles{Err: "no database"}
	}
	var rows []aiRow
	if err := db.Raw(`SELECT success, COALESCE(execution_status, '') AS execution_status,
			COALESCE(error_class, '') AS error_class,
			substr(COALESCE(error_message, ''), 1, 200) AS error_message
		FROM decision_records WHERE trader_id = ? AND timestamp >= ? AND timestamp < ?`,
		traderID, sess.Start.UTC(), sess.End.UTC()).Scan(&rows).Error; err != nil {
		return AICycles{Err: "decision rows unreadable: " + err.Error()}
	}
	out := AICycles{Total: len(rows)}
	classes := map[string]int{}
	for _, r := range rows {
		if r.Success {
			continue
		}
		if isGuardrailSkip(r) {
			out.Skipped++
			continue
		}
		out.Failed++
		classes[aiFailureClass(r.ErrorClass, r.ErrorMessage)]++
	}
	if top := topCounts(classes, 1); len(top) > 0 {
		out.TopClass = top[0].Gate
	}
	return out
}

// aiFailureClass names a failed cycle's reason in a few words. The typed
// error_class wins; older rows (and the context-build failures, which carry
// no class) are named from the message.
func aiFailureClass(class, msg string) string {
	switch {
	case class == "ai_payment_402" || strings.Contains(msg, "status 402"):
		return "402"
	case class == "ai_call_failed" || strings.Contains(msg, "AI API call failed"):
		return "AI call failed"
	case strings.Contains(msg, "Failed to build trading context"):
		return "no trading context"
	case class != "":
		return class
	}
	return "other"
}

// topCounts returns the n largest entries, largest first, ties by name.
func topCounts(m map[string]int, n int) []Refusal {
	var out []Refusal
	for k, v := range m {
		if v > 0 {
			out = append(out, Refusal{Gate: k, N: v})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].N != out[j].N {
			return out[i].N > out[j].N
		}
		return out[i].Gate < out[j].Gate
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func joinCounts(rs []Refusal) string {
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		parts = append(parts, fmt.Sprintf("%s %d", r.Gate, r.N))
	}
	return strings.Join(parts, ", ")
}

func whyName(t TraderFunnel) string {
	if n := strings.TrimSpace(t.Name); n != "" {
		return n
	}
	return t.ID
}

// whyLines renders the why-no-trade lines, one per trader.
func (r Report) whyLines() []string {
	if !r.WhyWired {
		return []string{"Why no trade: n/a (traders not wired)"}
	}
	if len(r.Why) == 0 {
		return []string{"Why no trade: no traders loaded"}
	}
	out := make([]string, 0, len(r.Why))
	for _, w := range r.Why {
		out = append(out, shortName(whyName(w.TraderFunnel))+": "+w.line(r.Session))
	}
	return out
}

func (w TraderWhy) line(sess Session) string {
	if w.AI != nil {
		a := w.AI
		switch {
		case a.Err != "":
			return "AI cycles n/a (" + a.Err + ")"
		case a.Total == 0:
			return "no AI cycles this session"
		}
		skipped := ""
		if a.Skipped > 0 {
			skipped = fmt.Sprintf(", %d skipped by a guardrail", a.Skipped)
		}
		if a.Failed == 0 {
			return fmt.Sprintf("AI cycles %d, none failed%s", a.Total, skipped)
		}
		return fmt.Sprintf("AI failed %d/%d (%s)%s", a.Failed, a.Total, a.TopClass, skipped)
	}
	if !w.Have {
		return "funnel n/a (no bar evaluated since the trader started)"
	}
	if w.DayKey != sess.Key {
		return "funnel n/a (its counts belong to session " + w.DayKey + ")"
	}
	l := fmt.Sprintf("setups %d, placed %d, filled %d", w.Setups, w.Placed, w.Filled)
	if tr := topCounts(w.TraderRefusals, whyTopN); len(tr) > 0 {
		l += " · trader refusals " + joinCounts(tr)
	}
	if kr := topCounts(w.KernelRefusals, whyTopN); len(kr) > 0 {
		l += " · top refusals " + joinCounts(kr)
	}
	if w.Since.IsZero() || w.Since.After(sess.Start) {
		l += " (" + bootLabelClock(w.Since) + ")"
	}
	if w.CountersRestarted {
		l += " (refusal counters restarted mid-session: partial)"
	}
	return l
}

// bootLabelClock is bootLabel at clock resolution: inside one session (17:00
// to 16:00) a clock time is never ambiguous.
func bootLabelClock(t time.Time) string {
	if t.IsZero() {
		return "since boot (boot time n/a)"
	}
	return "since boot " + kernel.ClockCT(t)
}
