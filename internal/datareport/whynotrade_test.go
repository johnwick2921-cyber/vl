package datareport

import (
	"strings"
	"testing"
	"time"

	"vl/store"
)

// m469 why-no-trade pins at the report level. The end-to-end fixture with the
// real 11-05 / 11-06 funnel lives in trader/why_no_trade_test.go (it drives
// the production funnel); these pin the report's own guards.

// The 11-05 session (11-04 17:00 -> 11-05 16:00 CT).
var whySess = SessionFor(ct(2026, 11, 5, 16, 10))

func logAI(t *testing.T, st *store.Store, traderID string, start time.Time, n int, ok bool, class, msg string) {
	t.Helper()
	logAIStatus(t, st, traderID, start, n, ok, "", class, msg)
}

func logAIStatus(t *testing.T, st *store.Store, traderID string, start time.Time, n int, ok bool, status, class, msg string) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := st.Decision().LogDecision(&store.DecisionRecord{TraderID: traderID, CycleNumber: i + 1,
			Timestamp: start.Add(time.Duration(i) * time.Minute), Success: ok, ExecutionStatus: status, ErrorClass: class, ErrorMessage: msg}); err != nil {
			t.Fatalf("LogDecision: %v", err)
		}
	}
}

func whyLinesFor(src Sources, sess Session) []string {
	return Collect(src, sess).whyLines()
}

func funnelOf(ts ...TraderFunnel) func() []TraderFunnel {
	return func() []TraderFunnel { return ts }
}

// AI-planner cycles: only the session window counts, the typed 402 class is
// named, and a mentor-mode trader's rows (success=1 means "skipped") never
// become an AI count.
func TestWhyAICyclesSessionWindowAndMentorExcluded(t *testing.T) {
	st, _ := newDB(t)
	const msg402 = `Failed to get AI decision: AI API call failed: API returned error (status 402): {"error":{"message":"Insufficient Balance"}}`
	const msgCtx = `Failed to build trading context: failed to get account balance: ninjatrader/tcp: balance unavailable for bound account "SimAccount1" — no snapshot`
	logAI(t, st, "ai", whySess.Start, 439, false, "ai_payment_402", msg402)
	logAI(t, st, "ai", whySess.Start.Add(12*time.Hour), 21, false, "", msgCtx)
	logAI(t, st, "ai", whySess.Start.Add(-10*time.Minute), 10, true, "", "") // the session before
	logAI(t, st, "ai", whySess.End, 10, true, "", "")                        // the 16:00 close onwards
	logAI(t, st, "mentor", whySess.Start.Add(time.Hour), 30, true, "", "")
	logAI(t, st, "mentor", whySess.Start.Add(2*time.Hour), 5, false, "", msgCtx)

	got := whyLinesFor(Sources{DB: st.GormDB(), Funnel: funnelOf(
		TraderFunnel{ID: "ai", Name: "12345"},
		TraderFunnel{ID: "mentor", Name: "hoang", Mentor: true, Have: true, DayKey: whySess.Key, Since: whySess.Start, Setups: 1},
	)}, whySess)
	want := []string{"hoang: setups 1, placed 0, filled 0", "12345: AI failed 460/460 (402)"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A guardrail skip (success=0, no error class - exactly what stampGuardrailSkip
// in trader/auto_trader_loop.go writes) is NOT an AI failure: on 10-28 the only
// success=0 rows of the AI trader were 3 guardrail skips, and the line read
// "AI failed 3/461 (other)" while the AI never failed. Each reason the review
// named gets a row; the mark is either the status or the message prefix.
// MUTANT (shown RED): drop the isGuardrailSkip check -> "AI failed 5/12 (other)".
func TestWhyGuardrailSkipsAreNotAIFailures(t *testing.T) {
	st, _ := newDB(t)
	at := whySess.Start.Add(time.Hour)
	logAI(t, st, "planner", at, 7, true, "", "")
	for i, reason := range []string{"superseded_wait", "stale_bar_discarded", "stale_reeval_refused", "schema_parse_failed", "market_data_unavailable"} {
		status := "guardrail_skip"
		if i == 4 {
			status = "" // an older row: only the message carries the mark
		}
		logAIStatus(t, st, "planner", at.Add(time.Duration(10+i)*time.Minute), 1, false, status, "", "guardrail_skip: "+reason)
	}
	logAI(t, st, "mixed", at, 3, true, "", "")
	logAIStatus(t, st, "mixed", at.Add(10*time.Minute), 2, false, "guardrail_skip", "", "guardrail_skip: superseded_wait")
	logAI(t, st, "mixed", at.Add(20*time.Minute), 1, false, "ai_payment_402", "Failed to get AI decision: API returned error (status 402)")
	got := whyLinesFor(Sources{DB: st.GormDB(), Funnel: funnelOf(
		TraderFunnel{ID: "planner", Name: "12345"}, TraderFunnel{ID: "mixed", Name: "zulu"},
	)}, whySess)
	want := "12345: AI cycles 12, none failed, 5 skipped by a guardrail\nzulu: AI failed 1/6 (402), 2 skipped by a guardrail"
	if strings.Join(got, "\n") != want {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), want)
	}
}

// An AI trader with healthy cycles, and one with none, say so in words.
func TestWhyAICyclesHealthyAndIdle(t *testing.T) {
	st, _ := newDB(t)
	logAI(t, st, "ok", whySess.Start.Add(time.Hour), 7, true, "", "")
	got := whyLinesFor(Sources{DB: st.GormDB(), Funnel: funnelOf(
		TraderFunnel{ID: "ok", Name: "alpha"}, TraderFunnel{ID: "idle", Name: "beta"},
	)}, whySess)
	want := "alpha: AI cycles 7, none failed\nbeta: no AI cycles this session"
	if strings.Join(got, "\n") != want {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), want)
	}
}

// A funnel whose counts belong to another session day (the trader already
// rolled, or stopped days ago) is n/a, never printed as this session's.
func TestWhyFunnelOfAnotherSessionIsNA(t *testing.T) {
	got := whyLinesFor(Sources{Funnel: funnelOf(
		TraderFunnel{Name: "hoang", Mentor: true, Have: true, DayKey: "2026-11-03", Since: ct(2026, 11, 3, 17, 0), Setups: 4, Placed: 1},
		TraderFunnel{Name: "zed", Mentor: true},
	)}, whySess)
	want := "hoang: funnel n/a (its counts belong to session 2026-11-03)\nzed: funnel n/a (no bar evaluated since the trader started)"
	if strings.Join(got, "\n") != want {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), want)
	}
}

// The since-boot label: present when the counting began after the 17:00
// start, absent when it began at the roll.
func TestWhySinceBootLabel(t *testing.T) {
	mk := func(since time.Time) TraderFunnel {
		return TraderFunnel{Name: "hoang", Mentor: true, Have: true, DayKey: whySess.Key, Since: since,
			KernelRefusals: map[string]int{"isb_stop_over_max": 82, "phl_not_higher_low": 45}}
	}
	got := whyLinesFor(Sources{Funnel: funnelOf(mk(ct(2026, 11, 5, 9, 41)))}, whySess)
	if want := "hoang: setups 0, placed 0, filled 0 · top refusals isb_stop_over_max 82, phl_not_higher_low 45 (since boot 09:41 CT)"; got[0] != want {
		t.Fatalf("mid-session boot:\n got %s\nwant %s", got[0], want)
	}
	got = whyLinesFor(Sources{Funnel: funnelOf(mk(whySess.Start))}, whySess)
	if strings.Contains(got[0], "since boot") {
		t.Fatalf("a funnel counted from the 17:00 roll must not say since boot: %s", got[0])
	}
}

// The why-no-trade lines sit straight under the header, so a report cut from
// the end can never cut them; unwired is n/a, not silence.
func TestWhyLinesAreAtTheTop(t *testing.T) {
	r := Collect(Sources{Funnel: funnelOf(TraderFunnel{ID: "ai", Name: "12345"})}, whySess)
	lines := r.Lines()
	if !strings.HasPrefix(lines[1], "12345: ") {
		t.Fatalf("line 1 = %q, want the why-no-trade line", lines[1])
	}
	if p := Parts(lines); !strings.Contains(p[0], "12345: ") {
		t.Fatalf("first message lacks the why-no-trade line: %q", p[0])
	}
	if got := Collect(Sources{}, whySess).Lines()[1]; got != "Why no trade: n/a (traders not wired)" {
		t.Fatalf("unwired line = %q", got)
	}
}
