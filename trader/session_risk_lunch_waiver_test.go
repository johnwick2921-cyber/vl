package trader

import (
	"testing"

	"vl/kernel"
)

// #9 — a mentor arm waives ONLY the lunch and first-5m sub-windows. The red-news
// T1 blackout and the consecutive-loss breaker must still refuse it. Two masks
// are fixed: (1) sessionGateDecision returned lunch BEFORE the T1 blackout, so
// an FOMC/Fed-minutes 13:00 CT print inside the 12:00–13:30 lunch window read
// 'lunch' and was waived; (2) the band verdict returned before the breaker, so
// a tripped breaker was never evaluated during lunch/first-5m.
// MUTANT: move the T1 check back below lunch in sessionGateDecision → the 13:00
// FOMC minute reads 'lunch' → RED.
func TestSessionGateDecisionT1WinsOverLunch(t *testing.T) {
	reg := kernel.DefaultSessionRegistry() // NY enabled
	// A T1 window covering 13:00 CT (an FOMC print at 13:00, ±15m = 12:45–13:15).
	t1 := []kernel.CTWindow{{Start: 12*60 + 45, End: 13*60 + 15, Label: "FOMC 13:00 CT ±15m"}}
	reason, blocked := sessionGateDecision(reg, ctAt(t, 13, 0), t1, nil)
	if !blocked {
		t.Fatalf("13:00 CT inside a T1 window must block, got reason=%q", reason)
	}
	if contains(reason, "lunch") {
		t.Fatalf("the T1 blackout must not be masked by the lunch reason, got %q", reason)
	}
	if !contains(reason, "red-news blackout") {
		t.Fatalf("the T1 reason must be the red-news blackout, got %q", reason)
	}
	// The mentor waiver must NOT fire on it.
	if mentorWaivesSessionBand(sessionRiskVerdict{Refuse: true, Class: "no_trade_band", Reason: reason}) {
		t.Fatalf("a T1 blackout verdict must not be mentor-waivable, reason=%q", reason)
	}
}

// #9 (2) — the breaker trips through the lunch window: the verdict must be the
// consecutive-loss halt, not the (mentor-waivable) lunch band.
// MUTANT: drop the re-adjudication in adjudicateWithBandWaiver → the tripped
// breaker reads no_trade_band(lunch) → RED.
func TestSessionRiskBreakerNotMaskedByLunch(t *testing.T) {
	const N, M = 8, 5
	band := "no_trade_band: lunch no-trade window (12:00–13:30 CT)"
	v := adjudicateWithBandWaiver(9, N, M, band) // 9 losses >= 8
	if v.Class != "consecutive_loss" || !v.Refuse {
		t.Fatalf("the breaker must win through lunch, got class=%q refuse=%v", v.Class, v.Refuse)
	}
	if mentorWaivesSessionBand(v) {
		t.Fatal("a consecutive_loss verdict must never be mentor-waivable")
	}
	// With a clean record (0 losses) the lunch band IS waivable.
	clean := adjudicateWithBandWaiver(0, N, M, band)
	if clean.Class != "no_trade_band" || !mentorWaivesSessionBand(clean) {
		t.Fatalf("a clean-record lunch band must stay waivable, got class=%q", clean.Class)
	}
}

// isMentorWaivableBandReason: only lunch and first-5m; the T1 blackout and the
// breaker text are hard.
func TestMentorWaivableBandReasonClassification(t *testing.T) {
	cases := []struct {
		reason string
		want   bool
	}{
		{"no_trade_band: lunch no-trade window (12:00–13:30 CT)", true},
		{"no_trade_band: NY first-5m no-trade window", true},
		{"no_trade_band: 🔴 red-news blackout: FOMC 13:00 CT ±15m", false},
		{"force_flat_window: T1 lead flat", false},
		{"consecutive_loss_halt: 9 consecutive losing trades", false},
	}
	for _, c := range cases {
		if got := isMentorWaivableBandReason(c.reason); got != c.want {
			t.Errorf("isMentorWaivableBandReason(%q) = %v, want %v", c.reason, got, c.want)
		}
	}
}
