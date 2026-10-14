package trader

import (
	"os"
	"strings"
	"testing"
)

// Honest-logs source contract (final-bundle Phase 1, 2026-08-19).
//
// journald's frame-flood suppression drops INFO lines by the thousands per
// window; the log_events DB sink captures WARN+ only. Owner-visible trade
// truth must therefore be emitted at WARN or louder, or it disappears exactly
// when the owner goes looking for it (the phantom-position and "breakeven not
// moving" false alarms). This test pins the level of each promoted line so a
// refactor can't quietly demote them back to INFO.
func TestOwnerVisibleLinesAreWarnOrLouder(t *testing.T) {
	cases := []struct {
		file   string
		marker string // unique fragment of the log line's format string
		want   string // the call that must carry it
	}{
		// N-G (release #22 rescan): at.logWarnf — still WARN (it calls
		// logger.Warnf), now carrying the trader tag the 10:08:27 line lacked.
		{"auto_trader.go", "auto-breakeven: %s %s +%.1f pts in profit", "at.logWarnf"},
		{"ninjatrader/close_sync.go", "NT position closed: %s %s qty=", "logger.Warnf"},
		{"auto_trader_decision.go", "Position OPENED [%s] %s %s qty=", "logger.Warnf"},
		{"position_desync.go", "position_state_desync: store shows", "at.logErrorf"},
		{"auto_trader_feedwatch.go", "FEED DOWN: no NT8 bar", "at.logErrorf"},
	}
	for _, c := range cases {
		src, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatalf("read %s: %v", c.file, err)
		}
		idx := strings.Index(string(src), c.marker)
		if idx < 0 {
			t.Fatalf("%s: marker %q not found — the owner-visible line was removed or reworded; update the contract deliberately", c.file, c.marker)
		}
		// The call site is the last logger invocation before the marker.
		head := string(src[:idx])
		callPos := strings.LastIndex(head, c.want)
		infoPos := lastAnyIndex(head, "logger.Infof", "at.logInfof", "logger.Info(")
		if callPos < 0 || infoPos > callPos {
			t.Errorf("%s: line %q is not emitted via %s (an INFO call sits closer) — owner-visible lines must be WARN+ to reach log_events", c.file, c.marker, c.want)
		}
	}
}

func lastAnyIndex(s string, subs ...string) int {
	best := -1
	for _, sub := range subs {
		if i := strings.LastIndex(s, sub); i > best {
			best = i
		}
	}
	return best
}
