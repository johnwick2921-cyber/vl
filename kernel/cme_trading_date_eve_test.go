package kernel

// m471 L1-2 / L1-3 — the 17:00-23:59 CT evening is governed by the calendar row
// of its TRADING date (CMETradingDateKey), in IsCMEOpen, CMEClosedReason,
// NextCMEOpen and SessionStateAt, so the trading gates agree with the data
// gates (IsCMETradingExpected) for every minute.

import (
	"strings"
	"testing"
	"time"
)

func evCT(y, mo, d, h, mi int) time.Time {
	return time.Date(y, time.Month(mo), d, h, mi, 0, 0, CTLocation())
}

func TestEveningFollowsTradingDateCalendarRow(t *testing.T) {
	cases := []struct {
		name       string
		at         time.Time
		open       bool
		reasonHas  string // when closed
		nextOpenAt time.Time
	}{
		// Wed 11-25 evening → trading date 11-26 Thanksgiving, a FULL CLOSURE.
		{"Wed 11-25 17:00 (Thanksgiving eve)", evCT(2026, 11, 25, 17, 0), false, "Thanksgiving Day", evCT(2026, 11, 26, 17, 0)},
		{"Wed 11-25 23:59 (Thanksgiving eve)", evCT(2026, 11, 25, 23, 59), false, "Thanksgiving Day", evCT(2026, 11, 26, 17, 0)},
		// Thanksgiving day itself, then the 17:00 reopen for the shortened 11-27.
		{"Thu 11-26 10:00 (Thanksgiving)", evCT(2026, 11, 26, 10, 0), false, "holiday", evCT(2026, 11, 26, 17, 0)},
		{"Thu 11-26 17:00 (11-27 session)", evCT(2026, 11, 26, 17, 0), true, "", time.Time{}},
		{"Thu 11-26 23:00 (11-27 session)", evCT(2026, 11, 26, 23, 0), true, "", time.Time{}},
		// Christmas Eve: FINAL 12:15 close, no Thursday evening session.
		{"Thu 12-24 13:00 (after the early close)", evCT(2026, 12, 24, 13, 0), false, "early close 12:15", evCT(2026, 12, 27, 17, 0)},
		{"Thu 12-24 17:15 (Christmas eve evening)", evCT(2026, 12, 24, 17, 15), false, "Christmas Day", evCT(2026, 12, 27, 17, 0)},
		// New Year's Eve: a normal day, closed evening into 2027-01-01.
		{"Thu 12-31 13:00", evCT(2026, 12, 31, 13, 0), true, "", time.Time{}},
		{"Thu 12-31 17:15", evCT(2026, 12, 31, 17, 15), false, "New Year's Day", evCT(2027, 1, 3, 17, 0)},
		// Sunday evenings before the 2027 Monday holidays.
		{"Sun 2027-01-17 19:00 (MLK)", evCT(2027, 1, 17, 19, 0), false, "MLK Day", evCT(2027, 1, 18, 17, 0)},
		{"Sun 2027-02-14 19:00 (Presidents)", evCT(2027, 2, 14, 19, 0), false, "Presidents Day", evCT(2027, 2, 15, 17, 0)},
		{"Sun 2027-05-30 19:00 (Memorial)", evCT(2027, 5, 30, 19, 0), false, "Memorial Day", evCT(2027, 5, 31, 17, 0)},
		{"Sun 2027-07-04 19:00 (Jul-4 observed)", evCT(2027, 7, 4, 19, 0), false, "Independence Day", evCT(2027, 7, 5, 17, 0)},
		{"Sun 2027-09-05 19:00 (Labor)", evCT(2027, 9, 5, 19, 0), false, "Labor Day", evCT(2027, 9, 6, 17, 0)},
		// Ordinary controls stay open.
		{"Wed 11-18 20:00 (ordinary)", evCT(2026, 11, 18, 20, 0), true, "", time.Time{}},
		{"Sun 11-29 17:00 (ordinary reopen)", evCT(2026, 11, 29, 17, 0), true, "", time.Time{}},
		{"Sun 2027-01-10 19:00 (ordinary)", evCT(2027, 1, 10, 19, 0), true, "", time.Time{}},
	}
	for _, c := range cases {
		open := IsCMEOpen(c.at)
		closed, reason := CMEClosedReason(c.at)
		if open != c.open {
			t.Errorf("%s: IsCMEOpen=%v, want %v (reason %q, trading date %s)", c.name, open, c.open, reason, CMETradingDateKey(c.at))
		}
		if closed != !open {
			t.Errorf("%s: CMEClosedReason closed=%v disagrees with IsCMEOpen=%v", c.name, closed, open)
		}
		if exp := IsCMETradingExpected(c.at); exp != open {
			t.Errorf("%s: IsCMETradingExpected=%v disagrees with IsCMEOpen=%v", c.name, exp, open)
		}
		if !c.open {
			if !strings.Contains(reason, c.reasonHas) {
				t.Errorf("%s: reason %q must name %q", c.name, reason, c.reasonHas)
			}
			if got := NextCMEOpen(c.at); !got.Equal(c.nextOpenAt) {
				t.Errorf("%s: NextCMEOpen=%s, want %s", c.name, got.In(CTLocation()).Format("Mon 2006-01-02 15:04"), c.nextOpenAt.Format("Mon 2006-01-02 15:04"))
			}
		}
	}

	// SessionStateAt resolves the evening by its trading date.
	if st := SessionStateAt(evCT(2026, 11, 26, 18, 0)); st.Date != "2026-11-27" || st.Class != SessionShortened {
		t.Errorf("SessionStateAt(Thu 11-26 18:00) = %s %s, want 2026-11-27 shortened (the session it belongs to)", st.Date, st.Class)
	}
	if st := SessionStateAt(evCT(2026, 11, 26, 10, 0)); st.Date != "2026-11-26" || st.Class != SessionClosed {
		t.Errorf("SessionStateAt(Thu 11-26 10:00) = %s %s, want 2026-11-26 closed (daytime unchanged)", st.Date, st.Class)
	}
}

// The parity pin: for every sampled evening (and daytime) minute from
// 2026-11-01 through 2027-12-31, the trading gate and the data gate agree, and
// CMEClosedReason mirrors IsCMEOpen.
func TestIsCMEOpenMatchesTradingExpectedEveryDay(t *testing.T) {
	samples := [][2]int{{0, 0}, {9, 0}, {12, 14}, {12, 15}, {16, 30}, {17, 0}, {17, 15}, {20, 0}, {23, 59}}
	n := 0
	for d := evCT(2026, 11, 1, 0, 0); !d.After(evCT(2027, 12, 31, 0, 0)); d = d.AddDate(0, 0, 1) {
		for _, s := range samples {
			tm := time.Date(d.Year(), d.Month(), d.Day(), s[0], s[1], 0, 0, CTLocation())
			open := IsCMEOpen(tm)
			if exp := IsCMETradingExpected(tm); exp != open {
				t.Errorf("%s CT (trading date %s): IsCMEOpen=%v IsCMETradingExpected=%v", tm.Format("Mon 2006-01-02 15:04"), CMETradingDateKey(tm), open, exp)
			}
			if closed, _ := CMEClosedReason(tm); closed != !open {
				t.Errorf("%s CT: CMEClosedReason closed=%v vs IsCMEOpen=%v", tm.Format("Mon 2006-01-02 15:04"), closed, open)
			}
			n++
		}
	}
	if n < 400*len(samples) {
		t.Fatalf("sampled only %d instants", n)
	}
}
