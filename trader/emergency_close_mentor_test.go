package trader

import (
	"strings"
	"testing"

	"vl/store"
)

// FIX-EMERGENCY-CLOSE-OFF-IN-MENTOR (owner ruling 2026-10-09): the 60s-monitor
// drawdown emergency close (profit > 5% AND drawdown ≥ 40% from the peak) is an
// AI-era close. With mentor mode ON it must NOT close a mentor-owned position.

// drawdownRig builds a trader with ONE LONG position at profit 6% (leverage 10 →
// 6% P&L) whose peak cache is seeded at 10% → drawdown 40% → the condition fires.
func drawdownRig(mentorOn bool) (*AutoTrader, *recordingTrader) {
	rt := &recordingTrader{MockTrader: &MockTrader{positions: []map[string]interface{}{
		{"symbol": "MNQ", "side": "LONG", "entryPrice": 100.0, "markPrice": 100.6, "positionAmt": 1.0, "leverage": 10.0},
	}}}
	at := &AutoTrader{
		id:     "dd-mentor",
		trader: rt,
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{
			RiskControl: store.RiskControlConfig{MentorMode: mentorOn},
		}},
		peakPnLCache: map[string]float64{"MNQ_LONG": 10.0},
	}
	return at, rt
}

// TestDrawdownEmergencyCloseBlockedInMentorMode — mentor ON: no close, one WARN,
// counter 1, second tick logs nothing new. RED (named): delete the mentorEnabled
// check in checkPositionDrawdown and the position closes.
func TestDrawdownEmergencyCloseBlockedInMentorMode(t *testing.T) {
	at, rt := drawdownRig(true)
	resetDrawdownMentorBlockedCountForTest()
	get := warnPlusCapture(t)

	at.checkPositionDrawdown()
	if rt.closedLong || rt.closedShort {
		t.Fatal("mentor mode ON: the drawdown emergency close must NOT close the position")
	}
	if c := DrawdownMentorBlockedCount(); c != 1 {
		t.Fatalf("drawdown mentor-blocked counter = %d, want 1", c)
	}
	if !hasLine(get(), "drawdown emergency close: not applied — mentor mode ON") {
		t.Fatalf("expected the one 'not applied' WARN line, got %v", get())
	}

	// 2nd tick on the SAME position: no new log line, no re-count.
	at.checkPositionDrawdown()
	if c := DrawdownMentorBlockedCount(); c != 1 {
		t.Fatalf("2nd tick re-counted: %d, want 1", c)
	}
	n := 0
	for _, l := range get() {
		if strings.Contains(l, "drawdown emergency close: not applied — mentor mode ON") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("2nd tick logged a new line: %d, want 1 (%v)", n, get())
	}
}

// TestDrawdownEmergencyCloseFiresWhenMentorModeOff — mentor OFF keeps the close
// byte-identical: the SAME rig closes the LONG position (proves the blocked test
// above cannot pass on an empty close path).
func TestDrawdownEmergencyCloseFiresWhenMentorModeOff(t *testing.T) {
	at, rt := drawdownRig(false)
	resetDrawdownMentorBlockedCountForTest()

	at.checkPositionDrawdown()
	if !rt.closedLong {
		t.Fatal("mentor OFF: the drawdown emergency close must close the LONG position")
	}
	if c := DrawdownMentorBlockedCount(); c != 0 {
		t.Fatalf("mentor OFF: the mentor-blocked counter must stay 0, got %d", c)
	}
}
