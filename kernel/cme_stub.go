package kernel

import "time"

// ReanchorCMEStub corrects the NT8-native 2h/4h session-close stub bar, which
// NT8 right-aligns to the 16:00 CT RTH close and stamps ONE HOUR EARLY
// (m134 DS-108 (d), owner "fix all" 2026-10-21).
//
// Observed on the live store [A]: MNQ 4h anchors are 17, 21, 01, 05, 09, 12,
// 17 — the 13:00 bucket (13:00–16:00, the RTH-close stub) is stored at 12:00.
// MNQ 2h anchors end …11, 13, 14, 17 — the 15:00 stub (15:00–16:00) is stored
// at 14:00. NT8 builds these minute-period bars so the last one of the session
// ends exactly at the 16:00 close, i.e. openMs + step == 16:00 CT — one hour
// before the 17:00-CT-anchored boundary the rest of the bot uses.
//
// This is a STAMP correction only: the bar that ends at 16:00 CT is re-anchored
// +1h to the session boundary (12:00→13:00 for 4h, 14:00→15:00 for 2h). Every
// other bar is untouched (its open + step is not 16:00). DST-safe via
// America/Chicago. tf other than 2h/4h is untouched.
func ReanchorCMEStub(tf string, openMs int64) int64 {
	var step time.Duration
	switch tf {
	case "2h":
		step = 2 * time.Hour
	case "4h":
		step = 4 * time.Hour
	default:
		return openMs
	}
	t := time.UnixMilli(openMs).In(CTLocation())
	closeAt := t.Add(step)
	if closeAt.Hour() == 16 && closeAt.Minute() == 0 {
		return openMs + int64(time.Hour/time.Millisecond)
	}
	return openMs
}
