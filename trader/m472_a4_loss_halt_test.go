package trader

import (
	"fmt"
	"testing"
	"time"

	"vl/store"
)

// TestM472A4MentorLossHaltFour — the mentor's consecutive-loss day stop.
//
// [Oct D4.1 @ 14:40] OctD4.1-R28 / R19: "thua 3 lenh lien tuc, lenh thu 4 thu 1
// lenh nua, neu khong duoc thi tat may nghi" — 3 losses in a row, the 4th trade
// is one more try, and if that fails too, shut the machine off. So after FOUR
// consecutive losses the 5th entry is refused; after three the 4th is allowed.
// Frame: /home/hoang/mm-course/oct/scenes/Ngày 4.1 Go over ngày 3/s_00881.jpg
// (the 2 Nov 09:39 chart the rule is spoken over; the rule itself is voice).
//
// Both PRODUCTION call sites are exercised on a real store with losing closes
// seeded: consecutiveLossHaltedAt (decision/agent path; a MentorSourced
// decision is admitDecision) and sessionRiskGateAt (the mentor ARM path,
// admitEntry's else-branch). Resolution order: stored key > env > 4 for a
// mentor-mode strategy > 8.
func TestM472A4MentorLossHaltFour(t *testing.T) {
	now := time.Date(2026, 9, 22, 10, 0, 0, 0, chicagoLoc())
	cases := []struct {
		name       string
		mentor     bool
		halt       *int
		env        string
		losses     int
		wantRefuse bool
		wantN      int
		wantField  string
	}{
		{"mentor, 3 losses -> 4th entry allowed", true, nil, "", 3, false, 4, "4[I]"},
		{"mentor, 4 losses -> 5th entry refused", true, nil, "", 4, true, 4, "4[I]"},
		{"AI strategy, 7 losses -> allowed (still 8)", false, nil, "", 7, false, 8, "8[I]"},
		{"AI strategy, 4 losses -> allowed (still 8)", false, nil, "", 4, false, 8, "8[I]"},
		{"AI strategy, 8 losses -> refused", false, nil, "", 8, true, 8, "8[I]"},
		{"mentor + stored 6 beats default, 4 losses -> allowed", true, store.IntPtr(6), "", 4, false, 6, "6[O]"},
		{"mentor + stored 6, 6 losses -> refused", true, store.IntPtr(6), "", 6, true, 6, "6[O]"},
		{"mentor + stored 0 is OFF", true, store.IntPtr(0), "", 20, false, 0, "off[O]"},
		{"mentor + env 6 beats mentor default, 4 losses -> allowed", true, nil, "6", 4, false, 6, "6[E]"},
		{"mentor + env 6, 6 losses -> refused", true, nil, "6", 6, true, 6, "6[E]"},
		{"mentor + env 0 is OFF", true, nil, "0", 20, false, 0, "off[E]"},
		{"mentor + invalid env -> mentor 4", true, nil, "banana", 4, true, 4, "4[I]"},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("BREAKER_HALT_N", c.env)
			at := w1BreakerTrader(t, fmt.Sprintf("m472-a4-%d", i), c.halt, c.losses, now)
			at.config.StrategyConfig.RiskControl.MentorMode = c.mentor

			reason, halted := at.consecutiveLossHaltedAt(now)
			if halted != c.wantRefuse {
				t.Fatalf("decision path halted=%v want %v (%q)", halted, c.wantRefuse, reason)
			}
			v := at.sessionRiskGateAt(now) // the mentor ARM path
			if v.Refuse != c.wantRefuse {
				t.Fatalf("arm path refuse=%v want %v (%q)", v.Refuse, c.wantRefuse, v.Reason)
			}
			if v.N != c.wantN {
				t.Fatalf("arm path N=%d want %d", v.N, c.wantN)
			}
			if got, _ := breakerBootField(at.config.StrategyConfig); got != c.wantField {
				t.Fatalf("boot field %q want %q", got, c.wantField)
			}
		})
	}
}
