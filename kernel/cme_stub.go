package kernel

import "time"

// ReanchorCMEStub corrects the NT8-native coarse-TF session-close stub bar,
// which NT8 right-aligns to the 16:00 CT RTH close and stamps ONE HOUR EARLY
// (bug-hunt #34, m220 DS-104 — extends m134/m169 DS-108's 2h/4h correction to
// the whole 2h/4h/6h/8h/12h family with the SAME condition).
//
// Observed on the live store [A]: MNQ 4h anchors are 17, 21, 01, 05, 09, 12,
// 17 — the 13:00 bucket (13:00–16:00, the RTH-close stub) is stored at 12:00.
// NT8 builds these minute-period bars so the last one of the session ends
// exactly at the 16:00 close, i.e. openMs + step == 16:00 CT — one hour
// before the 17:00-CT-anchored boundary the rest of the bot uses. The 23h CME
// session does not divide by 2h/4h/6h/8h/12h, so EVERY one of those TFs has a
// short stub bar with the same +1h stamp error (6h: 10:00→11:00, 8h:
// 08:00→09:00, 12h: 04:00→05:00, 2h: 14:00→15:00, 4h: 12:00→13:00).
//
// This is a STAMP correction only: the bar that ends at 16:00 CT is re-anchored
// +1h to the session boundary. Every other bar is untouched (its open + step
// is not 16:00). DST-safe via America/Chicago. tf other than 2h/4h/6h/8h/12h
// is untouched.
func ReanchorCMEStub(tf string, openMs int64) int64 {
	var step time.Duration
	switch tf {
	case "2h":
		step = 2 * time.Hour
	case "4h":
		step = 4 * time.Hour
	case "6h":
		step = 6 * time.Hour
	case "8h":
		step = 8 * time.Hour
	case "12h":
		step = 12 * time.Hour
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
