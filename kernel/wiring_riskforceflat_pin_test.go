package kernel

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// PIN (wiring wave 2026-09-05) — RiskForceFlat must be USED by its caller.
//
// DailyGuardrails.Check() always returned RiskForceFlat on a daily-loss trip
// and the only production caller threw the value away:
//
//	} else if _, gErr := g.Check(); gErr != nil {
//
// so the limit skipped a decision cycle and a resting arm filled straight
// through it. These pins fail if that regression is reintroduced.

// The trip state round-trips and fails open when nothing has tripped.
func TestDailyForceFlatStateRoundTrip(t *testing.T) {
	const id = "pin-trader-1"
	ClearDailyForceFlat(id)
	if got := DailyForceFlatReason(id); got != "" {
		t.Fatalf("untripped trader must fail open, got %q", got)
	}
	SetDailyForceFlat(id, "daily loss limit hit (realized today=-492.00, limit=-450.00)")
	if got := DailyForceFlatReason(id); !strings.Contains(got, "daily loss limit hit") {
		t.Fatalf("trip not recorded, got %q", got)
	}
	if other := DailyForceFlatReason("pin-trader-2"); other != "" {
		t.Fatalf("trip leaked across traders, got %q", other)
	}
	ClearDailyForceFlat(id)
	if got := DailyForceFlatReason(id); got != "" {
		t.Fatalf("clear did not lift the trip, got %q", got)
	}
}

// The daily window reset lifts every trip — a trip lasts the session-day, not
// forever.
func TestDailyResetClearsForceFlat(t *testing.T) {
	const id = "pin-trader-reset"
	SetDailyForceFlat(id, "daily loss limit hit")
	ResetDailyPnLAt(time.Now())
	if got := DailyForceFlatReason(id); got != "" {
		t.Fatalf("ResetDailyPnLAt must clear the trip; still %q", got)
	}
}

// THE PIN THAT FAILS WHEN THE CALL IS REMOVED. The guardrail branch in
// engine_analysis.go must bind Check()'s decision and publish RiskForceFlat.
// Source-level because that branch needs a full engine cycle to reach
// behaviourally; the assertion is precise about what must be present.
func TestEngineAnalysisPublishesRiskForceFlat(t *testing.T) {
	src, err := os.ReadFile("engine_analysis.go")
	if err != nil {
		t.Fatalf("read engine_analysis.go: %v", err)
	}
	s := string(src)

	if regexp.MustCompile(`_,\s*gErr\s*:=\s*g\.Check\(\)`).MatchString(s) {
		t.Error("REGRESSION: engine_analysis.go discards DailyGuardrails.Check()'s decision again " +
			"(`_, gErr := g.Check()`). A daily-loss trip must publish RiskForceFlat, or a resting " +
			"arm fills straight through the limit.")
	}
	if !strings.Contains(s, "gDecision, gErr := g.Check()") {
		t.Error("engine_analysis.go must bind the decision from g.Check() (gDecision, gErr := g.Check())")
	}
	if !strings.Contains(s, "gDecision == RiskForceFlat") {
		t.Error("engine_analysis.go must test the decision against RiskForceFlat")
	}
	if !strings.Contains(s, "SetDailyForceFlat(ctx.TraderID") {
		t.Error("engine_analysis.go must publish the trip via SetDailyForceFlat — without this call " +
			"the entry gate's daily leg can never fire and the limit is decorative")
	}
}
