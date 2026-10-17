package trader

import (
	"nofx/store"
)

// FlipRereadBootLine (W-FLIP-REREAD, 2026-09-17) renders the resolved knob at
// trader load — the moment the strategy config exists. READ from the resolver,
// never from a file default: off unless the strategy saved flip_reread=true.
func FlipRereadBootLine(dp *store.DayPlanConfig) string {
	if dp != nil && dp.FlipRereadEnabled() {
		return "🧬 flip→reread=on(saved) (W-FLIP-REREAD)"
	}
	return "🧬 flip→reread=off(default) (W-FLIP-REREAD)"
}
