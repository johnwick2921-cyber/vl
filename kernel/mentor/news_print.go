package mentor

import (
	"strings"

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
	// PrintAtMs (m466 G58 D4.4-R6) is the print minute itself (07:30 CT),
	// filled by the trader's mentorNewsPrintWindowsFromEvents. It anchors the
	// 1h/4h read lead (CourseExits.NewsReadLeadMin): the CE-4 freeze starts
	// PrintAt − lead. 0 (a window built before #61, or a test fixture) → the
	// freeze starts at FromMs, byte-identical to the pre-#61 behaviour.
	PrintAtMs int64 `json:"print_at_ms,omitempty"`
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
// window) returns the input slice untouched, byte-identical. The windows are
// ABSOLUTE instants for today's prints only, so a historical bar at a
// different absolute time never matches — no day scoping is needed, and the
// seed and the live tick share this one definition.
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

// ── CE-3 / CE-4 (release #25; CE-4 reworked release #29): the news trade ────
//
// The course trades news from a read taken BEFORE the print and holds it:
// "Chứ tính cái lúc trước news, chứ không phải tính cái lúc news ra rồi nha…
// Còn cái khúc mà news ra rồi thì không tính" [D4.4 p1 @18:18–18:25]; then,
// after the print, "Đợi canh nó nằm trên hai cái đường line này, kiếm insert
// bar đu vô… Hoặc anh chị chỉ cần đợi nó nằm ở trên cái đường line 4 giờ"
// [@21:55–22:12]. The course names no END to that news period; the 08:30 CT
// RTH open is our [B] bound — the open is read as its own event ("open nó
// cũng giống news thôi" [D4.4 p2 @00:35]) where price need NOT be on the
// right side of the line [@03:51].
//
// CE-4 was reworked (release #29, owner "fix all"): it used to DELETE the
// 07:20–08:30 1m bars from the 1h/4h feed, which corrupted every later 1h/4h
// candle ALL DAY. The course reads the triggers from BEFORE the print and
// never says rewrite candles after the open — so CE-4 now FREEZES the pre-print
// trigger read until the 08:30 CT day-gate read (newsHTFFrozen), and no bar is
// ever deleted: the candles stay whole. news_bars_excluded now defaults ON
// (COMMON-25 case a: the course line, no ruling against); CE-3 (news_wait_both_lines)
// stays default ON (course-defaults-30; see mentor.go:593).

// newsPeriodEnd is the end of the news period that a print window opens: the
// 08:30 CT RTH open of the print's CT day (never before the window's own end).
func newsPeriodEnd(w PrintWindow) int64 {
	end := dayStartCT(w.FromMs) + int64(globexCloseMin)*60_000
	if end < w.ToMs {
		return w.ToMs
	}
	return end
}

// newsHTFWindows returns the CE-4 news-period windows (the freeze span):
// OFF → the print windows unchanged (07:20–07:35); ON → each window runs to
// the 08:30 CT open, so post-print follow-through cannot move the 1h/4h lines.
// It is the window basis for newsHTFFrozen / newsBarFrozen (release #29).
//
// m466 G58 D4.4-R6: ON also starts the freeze at the READ — "Một giờ thì đợi
// gần trước news, tầm 30 phút" / board "(trước news 30')" [D4.4 p1
// @05:28–05:36; board 14:33–15:36] — PrintAt − mentor_news_read_lead_min
// (default 30 → 07:00 for a 07:30 print), never later than the window's own
// start. The order hold (the window itself, −10m/+5m) is unchanged; the news
// PERIOD the CE-3 wait and the stored htf_gate_news_only read still starts at
// the window (newsPeriodEnd / PrintWindows), only the trigger READ moves.
func newsHTFWindows(windows []PrintWindow, cfg Config) []PrintWindow {
	if !cfg.NewsBarsExcluded || len(windows) == 0 {
		return windows
	}
	out := make([]PrintWindow, len(windows))
	for i, w := range windows {
		out[i] = PrintWindow{FromMs: newsReadStart(w, cfg), ToMs: newsPeriodEnd(w), PrintAtMs: w.PrintAtMs}
	}
	return out
}

// newsReadStart is the instant the pre-print 1h/4h read is taken (the freeze
// start): PrintAt − lead minutes (D4.4-R6), or the window start when the window
// carries no PrintAtMs (pre-#61) or the lead is 0 / shorter than the window's
// pre-skirt.
func newsReadStart(w PrintWindow, cfg Config) int64 {
	lead := cfg.CourseExits.NewsReadLeadMin
	if w.PrintAtMs <= 0 || lead <= 0 {
		return w.FromMs
	}
	if start := w.PrintAtMs - int64(lead)*60_000; start < w.FromMs {
		return start
	}
	return w.FromMs
}

// newsHTFFrozen (CE-4 reworked, release #29) reports whether the 1h/4h trigger
// read is FROZEN at the tick clock `now`: news_bars_excluded ON and `now`
// inside a print window's news period, extended through the 08:30 CT day-gate
// read (the 08:30-close tick). While frozen the trigger lines hold their
// pre-print values — no 1m bar is deleted, so the 1h/4h candles stay whole all
// day. The course reads the trigger from BEFORE the print and never says
// rewrite candles after the open [D4.4 p1 @18:13–18:25]; the freeze is the
// faithful reading, the old htfFeedBars deletion was not.
func newsHTFFrozen(cfg Config, now int64) bool {
	if !cfg.NewsBarsExcluded || len(cfg.PrintWindows) == 0 {
		return false
	}
	for _, w := range newsHTFWindows(cfg.PrintWindows, cfg) {
		if now >= w.FromMs && now < w.ToMs+60_000 {
			return true
		}
	}
	return false
}

// newsBarFrozen reports whether a 1m bar at openMs is inside the CE-4 freeze
// (the seed replay's per-bar form): openMs in [window start, 08:30 CT open).
// The bar still builds its 1h/4h candle (whole), but fires no trigger break.
func newsBarFrozen(openMs int64, cfg Config) bool {
	if !cfg.NewsBarsExcluded {
		return false
	}
	return inPrintWindows(openMs, newsHTFWindows(cfg.PrintWindows, cfg))
}

// newsWaitFilter is the CE-3 gate (knob news_wait_both_lines): inside a news
// period (from a print window's start to the 08:30 CT open) an intraday entry
// needs the last close beyond the 4h trigger line on its side — above it for a
// long, below it for a short. No 4h line → refused (fail-closed), counted
// news_wait_4h_line.
//
// m466 G56 D4.4-R22 (knob mentor_news_wait_mode, default "both"): the course's
// FIRST method is BOTH lines — "Đợi canh nó nằm trên hai cái đường line này,
// kiếm inside bar đu vô… Hoặc anh chị chỉ cần đợi nó nằm ở trên cái đường line
// 4 giờ" [D4.4 p1 @21:48–22:12] — so "both" also needs the close beyond the
// pre-print 1h line (the line the CE-4 freeze holds) on the trade side,
// counted news_wait_1h_line. With no 1h line, or a 1h line older than the 4h
// one (oneHSilent — "mình sẽ coi 4 giờ thôi, không cần coi 1 giờ", D4.4-R12
// [D4.4 p1 @04:42–05:04, @18:48–18:52]), the 4h alone. "4h_only" = the
// pre-#61 rule (the course's "Hoặc…" minimum). The §8 swing is exempt;
// cancels and other actions pass.
func newsWaitFilter(ints []Intent, h HTF, lastClose float64, now int64, cfg Config) (out []Intent, refusals []string) {
	if !cfg.NewsWaitBothLines || len(cfg.PrintWindows) == 0 {
		return ints, nil
	}
	inNews := false
	for _, w := range cfg.PrintWindows {
		if now >= w.FromMs && now < newsPeriodEnd(w) {
			inNews = true
			break
		}
	}
	if !inNews {
		return ints, nil
	}
	// m466 G56: "both" (default; "" reads the same) also reads the 1h line,
	// unless the 1h has nothing to say (no line, or older than the 4h —
	// D4.4-R12 case 2).
	needOneH := cfg.CourseExits.NewsWaitMode != NewsWait4hOnly && h.OneH.Price > 0 && !oneHSilent(h)
	out = make([]Intent, 0, len(ints))
	for _, in := range ints {
		if in.Action != PlaceStopEntry && in.Action != PlaceStopLimitEntry || strings.HasPrefix(in.Reason, "swing") {
			out = append(out, in)
			continue
		}
		if !beyondLine(in.Side, lastClose, h.FourH.Price) {
			refusals = append(refusals, "news_wait_4h_line")
			continue
		}
		if needOneH && !beyondLine(in.Side, lastClose, h.OneH.Price) {
			refusals = append(refusals, "news_wait_1h_line") // m466 G56 D4.4-R22
			continue
		}
		out = append(out, in)
	}
	return out, refusals
}

// beyondLine reports a close strictly beyond a drawn line on the trade side
// (above it for a long, below it for a short); an undrawn line (≤ 0) is never
// passed.
func beyondLine(side Side, close, line float64) bool {
	return line > 0 && (side == SideLong && close > line || side == SideShort && close < line)
}
