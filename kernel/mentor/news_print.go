package mentor

import (
	"vl/market"
)

// ── NEWS 07:30 CT — the print candle must not move the HTF trigger lines ────
// [D4.4 p1 @18:13, @22:15]. The course's "kệ nó" (ignore it) is about the
// PRINT candle on a RED-FOLDER news day — a news spike is not a real break. The
// evaluator skips HTF breaks from any 1m bar whose OPEN falls inside one of the
// day's print windows. The windows are PLUMBED IN from the trader (which owns
// the calendar): an empty list means no print today, and the HTF feed is
// byte-identical — a normal day's 07:20–07:35 candles move the lines as usual.

// PrintWindow is one 07:30 CT red-folder print window (FromMs inclusive,
// ToMs exclusive) — the print minute with its −10m/+5m skirt.
type PrintWindow struct {
	FromMs int64 `json:"from_ms,omitempty"`
	ToMs   int64 `json:"to_ms,omitempty"`
}

// inPrintWindows reports whether openMs falls inside any listed window.
func inPrintWindows(openMs int64, windows []PrintWindow) bool {
	for _, w := range windows {
		if openMs >= w.FromMs && openMs < w.ToMs {
			return true
		}
	}
	return false
}

// htfFeedBars returns the 1m feed for the HTF trigger lines: every bar EXCEPT
// the print-window bars (item 18 part 1). It allocates only when a print bar
// is actually present — a non-print-day tape (no windows, or no bar inside a
// window) returns the input slice untouched, byte-identical.
func htfFeedBars(bars []market.Kline, windows []PrintWindow) []market.Kline {
	if len(windows) == 0 {
		return bars
	}
	drop := false
	for i := range bars {
		if inPrintWindows(bars[i].OpenTime, windows) {
			drop = true
			break
		}
	}
	if !drop {
		return bars
	}
	out := make([]market.Kline, 0, len(bars))
	for i := range bars {
		if !inPrintWindows(bars[i].OpenTime, windows) {
			out = append(out, bars[i])
		}
	}
	return out
}
