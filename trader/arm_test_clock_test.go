package trader

import (
	"testing"
	"time"

	"vl/kernel"
)

// armTestClock returns a moment that is inside an ENABLED session window and
// outside EVERY no-trade sub-window, so an arm-path test asserts the arm path
// rather than the hour the suite happened to run at.
//
// WHY THIS EXISTS. On 2026-09-10 dev's Go suite was 31 ok / 0 fail at 11:26 and
// 11:58 and RED at 12:11 — eight trader/ tests, one machine, one commit, one
// install, nothing touched. The clock had crossed 12:00 and entered the lunch
// no-trade band the session-risk wave added:
//
//	🛑 arm REFUSED (session risk): no_trade_band: lunch no-trade window (12:00–13:30 CT)
//
// It would have gone green again at 13:30 with nobody doing anything, which is
// worse than staying red: a cutover in that 90-minute window reads a red suite
// and blames its own branch.
//
// THE SEAM WAS NOT MISSING — the tests simply did not use it. A28 is already
// honoured in production: maybeManageArmedOrders() takes time.Now() at the entry
// and delegates to maybeManageArmedOrdersAt(snap, now), and every rule beneath
// takes the clock as an argument. The tests called the WALL-CLOCK entry point and
// handed a correctly-seamed rule the real hour of the day. clock-seams.list's own
// header describes exactly this ("the entry owns the clock; the rule takes it as
// an argument", class 60), and trader/clock_seam_lint_test.go asserts the seam
// EXISTS but never that the tests USE it. A seam only the production path honours
// is half a seam.
//
// The search is deliberate rather than a fixed constant: session windows and the
// no-trade bands are configuration and move. Scanning asks the registry what is
// true instead of hard-coding an hour that a later config change would silently
// invalidate — which would be this same defect with a longer fuse.
//
// FOLD #535-extra (W26 extra, test-only P2): the search used to start from
// time.Now(), so the arm fixtures still depended on the hour the suite ran. At
// 08:19 CT (LONDON last-entry) the driver picked LONDON from time.Now() while
// this search returned a moment in the NEXT session, and the arm was refused by
// the last-entry gate. The base is now FIXED — the search stays (windows/bands
// move), but the answer is the same instant every run, never the wall clock.
func armTestClock(t *testing.T, at *AutoTrader) time.Time {
	t.Helper()
	return armTestClockFrom(t, at, armTestClockBase)
}

// armTestClockBase is a FIXED, calendar-covered instant: 10:00 CT 2026-09-10 —
// inside NY (08:30–14:45), outside the lunch band and the T1 print windows. The
// search from this constant resolves to the same armable moment every run.
var armTestClockBase = time.Date(2026, 9, 10, 10, 0, 0, 0, kernel.CTLocation())

// armTestClockFrom is the same search from a CALLER-CHOSEN base.
//
// Pass a FIXED base when the test's fixtures are sensitive to the absolute
// value of the clock rather than merely to being inside a session — bar bucket
// boundaries are the case that bit: a tape whose confirm depends on the last
// five 1m bars forming a complete 5m bucket means the tape's meaning depends on
// `now` MODULO 5 MINUTES, and searching from time.Now() moves that modulus
// through the day. Fixed base, fixed modulus, same answer every run.
func armTestClockFrom(t *testing.T, at *AutoTrader, base time.Time) time.Time {
	t.Helper()
	// Search the surrounding 24h in 5-minute steps, nearest-first in both
	// directions, so the chosen moment stays on the session day the fixtures
	// were built for wherever possible.
	for i := 0; i < 288; i++ {
		for _, delta := range []time.Duration{
			time.Duration(i) * 5 * time.Minute,
			-time.Duration(i) * 5 * time.Minute,
		} {
			cand := base.Add(delta)
			if _, ok := at.sessionRegistry(cand).ActiveSession(cand); !ok {
				continue
			}
			if _, blocked := at.sessionEntryBlockedAt(cand); blocked {
				continue
			}
			return cand
		}
	}
	t.Skip("no armable moment in the surrounding 24h for this registry — every window is blocked")
	return time.Time{}
}
