package kernel

import "time"

// IsCMETradingExpected reports whether the bot EXPECTS a 1m bar to open at t: the
// instant is inside the weekly CME schedule (IsCMEOpen) AND the trading date it
// belongs to is not a calendar-closed day.
//
// session_calendar.json rows are keyed by TRADING date (CMETradingDateKey: the
// date a session closes on), so the evening session that would belong to a closed
// day - Wednesday 17:00+ before Thanksgiving, Thursday 17:00+ before Christmas /
// New Year's Day, Thursday before Good Friday - reads open by the instant alone
// while the calendar calls its trading date closed.
//
// #55 m449: ONE calendar for every data gate. The m445 hole tracker already used
// this combined test (trader/ninjatrader.barHoleExpected, which now delegates
// here) while the m446 data-truth checks used IsCMEOpen alone, so on those three
// evenings m446 demanded bars the calendar says are not expected, paused entries
// (with an owner push) for the whole evening, and kept pausing after the reopen
// while the phantom open-but-barless slots filled its open-aware lookback
// windows. A minute the calendar does not expect is never "missing".
//
// m471 L1-2: the SAME calendar for the trading gates too. IsCMEOpen now
// resolves every instant by its trading date (SessionStateAt), so those
// evenings read CLOSED there as well and IsCMEOpen(t) == IsCMETradingExpected(t)
// for every minute (pinned over 2026-11..2027-12). The trading-date check below
// is kept as belt-and-braces: it can no longer disagree, and if a later change
// re-splits the two the parity pin goes red instead of the bot trading a
// session its data gates ignore.
func IsCMETradingExpected(t time.Time) bool {
	if !IsCMEOpen(t) {
		return false
	}
	day, err := time.ParseInLocation("2006-01-02", CMETradingDateKey(t), CTLocation())
	if err != nil {
		return true // the instant said open; an unreadable key never invents a closure
	}
	return SessionStateAt(day.Add(12*time.Hour)).Class != SessionClosed
}
