package api

import (
	"testing"
	"time"
)

// R22 PNL-TRUTH — ONE "today". LEDGER_DAY used the CT CALENDAR day while every
// risk gate counts from the CME session day (17:00 CT), so from 17:00 to 24:00
// the header's "day" disagreed with the gates' (RISK-PNL P2-1, UI-GUIDE-TRUTH
// T26: +105.50 LEDGER_DAY vs the gates' day at 01:48 CT 10-14).
func TestLedgerDayWindowIsTheCMESessionDay(t *testing.T) {
	ct, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		now, start, end time.Time
	}{
		// 18:30 CT Tue → the session day that opened 17:00 Tue
		{time.Date(2026, 10, 13, 18, 30, 0, 0, ct), time.Date(2026, 10, 13, 17, 0, 0, 0, ct), time.Date(2026, 10, 14, 17, 0, 0, 0, ct)},
		// 01:48 CT Wed (the review instant) → still the Tue 17:00 session day
		{time.Date(2026, 10, 14, 1, 48, 0, 0, ct), time.Date(2026, 10, 13, 17, 0, 0, 0, ct), time.Date(2026, 10, 14, 17, 0, 0, 0, ct)},
		// 16:59 CT Wed → same day; 17:00 sharp → the next one
		{time.Date(2026, 10, 14, 16, 59, 0, 0, ct), time.Date(2026, 10, 13, 17, 0, 0, 0, ct), time.Date(2026, 10, 14, 17, 0, 0, 0, ct)},
		{time.Date(2026, 10, 14, 17, 0, 0, 0, ct), time.Date(2026, 10, 14, 17, 0, 0, 0, ct), time.Date(2026, 10, 15, 17, 0, 0, 0, ct)},
	} {
		s, e := ledgerDayWindow(tc.now)
		if !s.Equal(tc.start) || !e.Equal(tc.end) {
			t.Errorf("now %s → [%s, %s), want [%s, %s)", tc.now, s, e, tc.start, tc.end)
		}
	}
}
