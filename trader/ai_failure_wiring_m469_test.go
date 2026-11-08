package trader

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
)

// m469 review P1s: (a) "AI calls keep failing" had no production caller, so it
// could never fire; (b) a pre-call guardrail HOLD was read as an AI success, so
// a single market_data_unavailable cycle mid-outage pushed a false "AI is BACK".

// The watch call's production failure seam feeds the streak: five non-402
// failures through classifyWatchCallError = one ai-failing P0 row.
// MUTANT (shown RED): drop the noteAICallFailure line from
// classifyWatchCallError -> 0 rows -> RED.
func TestAIFailureBranchesFeedTheStreak(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	m469Push(t, t0)
	at, st := m469Trader(t, "m469-wired")
	timeout := errors.New("context deadline exceeded")
	for i := 0; i < aiFailStreakAlert; i++ {
		at.classifyWatchCallError(&store.DecisionRecord{}, timeout)
	}
	if rows := m469Rows(t, st, at.id, "ai-failing"); len(rows) != 1 {
		t.Fatalf("%d watch-call failures in a row must raise one ai-failing P0, rows = %d", aiFailStreakAlert, len(rows))
	}

	// The decision loop and the planner have no test seam short of a full
	// cycle, so pin the call inside their failure branches by source.
	loop := readTraderSource(t, "auto_trader_loop.go")
	inc := strings.Index(loop, "at.consecutiveAIFailures++")
	call := strings.Index(loop, "at.noteAICallFailure(time.Now(), err)")
	safe := strings.Index(loop, "// Activate safe mode after 3 consecutive failures")
	if inc < 0 || call < 0 || safe < 0 || !(inc < call && call < safe) {
		t.Fatalf("auto_trader_loop.go: the decision-failure branch must call at.noteAICallFailure(time.Now(), err) after consecutiveAIFailures++ (inc=%d call=%d safe=%d)", inc, call, safe)
	}
	planner := readTraderSource(t, "auto_trader_planner.go")
	fail := strings.Index(planner, "🛰 planner call FAILED")
	pcall := strings.Index(planner, "at.noteAICallFailure(time.Now(), err)")
	succ := strings.Index(planner, "a successful planner read clears any latched 402")
	if fail < 0 || pcall < 0 || succ < 0 || !(fail < pcall && pcall < succ) {
		t.Fatalf("auto_trader_planner.go: the planner err branch must call at.noteAICallFailure(time.Now(), err) (fail=%d call=%d success=%d)", fail, pcall, succ)
	}
}

// A HOLD made before any AI call (kernel holdCycle: only SkipReason set) is
// not an AI answer; a post-call skip that carries the AI's response is.
func TestAICallAnsweredOnlyWhenTheAIAnswered(t *testing.T) {
	cases := []struct {
		name string
		d    *kernel.FullDecision
		want bool
	}{
		{"nil", nil, false},
		{"pre-call hold (holdCycle shape)", &kernel.FullDecision{SkipReason: "market_data_unavailable"}, false},
		{"blackout hold", &kernel.FullDecision{SkipReason: "blackout_window"}, false},
		{"post-call schema skip", &kernel.FullDecision{SkipReason: "schema_parse_failed", RawResponse: "{...}"}, true},
		{"normal decision", &kernel.FullDecision{RawResponse: "{}"}, true},
		{"normal decision, empty raw", &kernel.FullDecision{}, true},
	}
	for _, c := range cases {
		if got := aiCallAnswered(c.d); got != c.want {
			t.Errorf("%s: aiCallAnswered = %v, want %v", c.name, got, c.want)
		}
	}
}

// THE REVIEW SCENARIO: a latched 402 outage, then one market_data_unavailable
// HOLD cycle. The loop's success gate must keep the outage latched and push
// nothing. MUTANT (shown RED): make aiCallAnswered return true for any non-nil
// decision -> the HOLD pushes "AI is BACK" and clears the latch -> RED.
func TestAIHoldCycleDoesNotEndA402Outage(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	net, p, _ := m469Push(t, t0)
	at, _ := m469Trader(t, "m469-hold")
	at.on402Failure(t0, m469Err402)
	p.Flush()

	hold := &kernel.FullDecision{SkipReason: "market_data_unavailable"}
	if aiCallAnswered(hold) { // the exact gate auto_trader_loop.go uses
		at.onAISuccess(t0.Add(5 * time.Minute))
	}
	p.Flush()
	if at.ai402OutageStartMs.Load() == 0 {
		t.Fatal("a pre-call HOLD ended the 402 outage latch")
	}
	if n := countContaining(net.sent(), "AI is BACK"); n != 0 {
		t.Fatalf("a pre-call HOLD pushed AI is BACK: %q", net.sent())
	}

	loop := readTraderSource(t, "auto_trader_loop.go")
	if !strings.Contains(loop, "if aiCallAnswered(aiDecision) {\n\t\tat.onAISuccess(time.Now())") {
		t.Fatal("auto_trader_loop.go must call onAISuccess only under aiCallAnswered(aiDecision)")
	}
}

func readTraderSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
