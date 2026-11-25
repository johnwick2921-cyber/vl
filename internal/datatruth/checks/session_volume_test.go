package checks

import (
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
)

var chicago = func() *time.Location {
	l, err := time.LoadLocation("America/Chicago")
	if err != nil {
		panic(err)
	}
	return l
}()

// cmeOpenForTest is the weekly CME equity-index schedule (no holidays): closed
// Fri 16:00 CT -> Sun 17:00 CT and 16:00-17:00 CT every day.
func cmeOpenForTest(ms int64) bool {
	d := time.UnixMilli(ms).In(chicago)
	m := d.Hour()*60 + d.Minute()
	switch d.Weekday() {
	case time.Saturday:
		return false
	case time.Friday:
		if m >= 16*60 {
			return false
		}
	case time.Sunday:
		if m < 17*60 {
			return false
		}
	}
	return !(m >= 16*60 && m < 17*60)
}

type sessionFixture struct {
	Start, End int64
	M1, M5     [][]any
}

func loadSessions(t *testing.T) map[string]sessionFixture {
	var raw map[string]struct {
		Start int64   `json:"start"`
		End   int64   `json:"end"`
		M1    [][]any `json:"m1"`
		M5    [][]any `json:"m5"`
	}
	loadJSON(t, "sessions_vol.json", &raw)
	out := map[string]sessionFixture{}
	for k, v := range raw {
		out[k] = sessionFixture{v.Start, v.End, v.M1, v.M5}
	}
	return out
}

func (s sessionFixture) bars() (m1, m5 []Bar) { return toBars(s.M1), toBars(s.M5) }

func ctTime(y int, mo time.Month, d, h, m int) time.Time {
	return time.Date(y, mo, d, h, m, 5, 0, chicago)
}

// TestSessionVolumeHole1026Pauses: the 10-26 17:02-22:37 CT 1m hole (63 bars and
// 13% of the volume at 6 h). The session VWAP read from the 1m is 71 pt off the
// 5m at 6 h; the check pauses from inside the hole until the cumulative
// shortfall stops mattering, and it NAMES the VWAP.
func TestSessionVolumeHole1026Pauses(t *testing.T) {
	s := loadSessions(t)["2026-10-26"]
	m1, m5 := s.bars()
	p := DefaultSessionVol()
	// 6 h into the session (23:00 CT), 12 h (05:00 CT)
	for _, now := range []time.Time{ctTime(2026, 10, 26, 23, 0), ctTime(2026, 10, 27, 5, 0)} {
		b, why := JudgeSessionLevels(m1, m5, now, chicago, cmeOpenForTest, p, 2.0)
		if !b {
			t.Fatalf("%s: the session VWAP is wrong by tens of points and must pause", now.Format("15:04"))
		}
		t.Logf("%s -> %s", now.Format("01-02 15:04"), why)
	}
	v := JudgeSessionVolume("session VWAP", m1, m5, s.Start, s.End, ctTime(2026, 10, 26, 23, 0), cmeOpenForTest, p, true, false)
	if !v.Blocked || v.VWAP-v.VWAPRestored > -40 {
		t.Fatalf("6 h in: vwap(1m)=%.2f restored=%.2f, want the 1m about 70 pt BELOW the restored value", v.VWAP, v.VWAPRestored)
	}
	if v.MissShare < 0.08 {
		t.Fatalf("missing share %.3f, want the ~13%% the hole removes", v.MissShare)
	}
}

// TestSessionVolumeProfilePausesAsPriorSession: the same session read the next
// day as the PRIOR session (pdVWAP, POC/VAH/VAL, nPOC) stays wrong for the whole
// day, because the profile and pdVWAP are immutable once cached.
func TestSessionVolumeProfilePausesAsPriorSession(t *testing.T) {
	s := loadSessions(t)["2026-10-26"]
	m1, m5 := s.bars()
	now := ctTime(2026, 10, 27, 17, 30) // the next session's first half hour
	v := JudgeSessionVolume("prior", m1, m5, s.Start, s.End, now, cmeOpenForTest, DefaultSessionVol(), false, true)
	if !v.Blocked {
		t.Fatalf("prior-session profile from a 1m with a 4.5 h hole must pause: %+v", v)
	}
	if v.POC == 0 || v.POCRestored == 0 {
		t.Fatalf("the profile was not recomputed: %+v", v)
	}
	t.Logf("pd 10-26: vwap %.2f vs %.2f, POC %.2f vs %.2f, VAH %.2f vs %.2f, VAL %.2f vs %.2f (miss %.1f%%)",
		v.VWAP, v.VWAPRestored, v.POC, v.POCRestored, v.VAH, v.VAHRestored, v.VAL, v.VALRestored, 100*v.MissShare)
}

// TestSessionVolumeCleanSessionsNeverPause walks 10-21 and 10-22 (clean in the
// store) every 5 minutes and also reads each as the prior session: not one pause.
func TestSessionVolumeCleanSessionsNeverPause(t *testing.T) {
	sess := loadSessions(t)
	for _, k := range []string{"2026-10-21", "2026-10-22"} {
		s := sess[k]
		m1, m5 := s.bars()
		n := 0
		for now := time.UnixMilli(s.Start).Add(10 * time.Minute); now.UnixMilli() < s.End; now = now.Add(5 * time.Minute) {
			if !cmeOpenForTest(now.UnixMilli()) {
				continue
			}
			n++
			if b, why := JudgeSessionLevels(m1, m5, now, chicago, cmeOpenForTest, DefaultSessionVol(), 2.0); b {
				t.Fatalf("%s at %s: clean session paused: %s", k, now.In(chicago).Format("01-02 15:04"), why)
			}
		}
		if n < 200 {
			t.Fatalf("%s: only %d checkpoints walked", k, n)
		}
		v := JudgeSessionVolume("prior", m1, m5, s.Start, s.End, time.UnixMilli(s.End).Add(time.Hour), cmeOpenForTest, DefaultSessionVol(), false, true)
		if v.Blocked {
			t.Fatalf("%s as the prior session: %+v", k, v)
		}
	}
}

// TestSessionVolume1019VolumeTruncation: row-level proof that a SINGLE
// volume-truncated stored bar (no hole at all) moves the session VWAP past
// tolerance; the session of 10-19 17:00 CT carries one around 21:15 CT.
func TestSessionVolume1019Truncation(t *testing.T) {
	s := loadSessions(t)["2026-10-19"]
	m1, m5 := s.bars()
	p := DefaultSessionVol()
	early := JudgeSessionVolume("session VWAP", m1, m5, s.Start, s.End, ctTime(2026, 10, 19, 20, 0), cmeOpenForTest, p, true, false)
	if early.Blocked {
		t.Fatalf("before the defect the session reads clean: %+v", early)
	}
	late := JudgeSessionVolume("session VWAP", m1, m5, s.Start, s.End, ctTime(2026, 10, 19, 21, 30), cmeOpenForTest, p, true, false)
	if !late.Blocked {
		t.Fatalf("21:30 CT: a stored 1m with truncated volume moved the VWAP %.2f pt and must pause (%+v)", late.VWAP-late.VWAPRestored, late)
	}
	t.Logf("10-19 21:30 CT: %s", late.Why)
}

// TestSessionVolumeFailsClosed: slots in neither frame. In the middle of a
// session they are priced from the median slot at the last known price (and the
// level still pauses when that matters); at the FRONT, with no price known yet,
// they cannot be priced at all and block outright.
func TestSessionVolumeFailsClosed(t *testing.T) {
	s := loadSessions(t)["2026-10-22"]
	m1, m5 := s.bars()
	drop := func(from, to int64) (a, b []Bar) {
		f := func(in []Bar) (out []Bar) {
			for _, x := range in {
				if x.T < from || x.T >= to {
					out = append(out, x)
				}
			}
			return
		}
		return f(m1), f(m5)
	}
	now := ctTime(2026, 10, 22, 22, 0)
	a, b := drop(s.Start, s.Start+2*3600_000)
	v := JudgeSessionVolume("session VWAP", a, b, s.Start, s.End, now, cmeOpenForTest, DefaultSessionVol(), true, false)
	if !v.Blocked || v.Unpriced == 0 {
		t.Fatalf("the first two hours missing from BOTH frames cannot be priced and must block: %+v", v)
	}
	a, b = drop(s.Start+2*3600_000, s.Start+4*3600_000)
	v = JudgeSessionVolume("session VWAP", a, b, s.Start, s.End, now, cmeOpenForTest, DefaultSessionVol(), true, false)
	if v.Unpriced != 0 || v.MissShare == 0 {
		t.Fatalf("a mid-session gap is estimated, not unpriced: %+v", v)
	}
}

// TestSessionLevelsCheckThroughEvaluate: the registered check, called the way
// production calls it (datatruth.Evaluate over RegisterAll with a store reader),
// pauses on the 10-26 hole and not on the clean 10-22 session, and every
// Evaluate in a minute reads the store once.
func TestSessionLevelsCheckThroughEvaluate(t *testing.T) {
	sess := loadSessions(t)
	for _, c := range []struct {
		key     string
		now     time.Time
		blocked bool
	}{
		{"2026-10-26", ctTime(2026, 10, 26, 23, 0), true},
		{"2026-10-22", ctTime(2026, 10, 22, 23, 0), false},
	} {
		s := sess[c.key]
		m1, m5 := s.bars()
		reads := 0
		datatruth.Reset()
		RegisterAll(Config{Src: calendarSrc{}, Loc: chicago, Unrequired: []string{CheckIngestLoss, CheckLookbackSwing4h, CheckLookbackLevels},
			SwingInputs: func(string, time.Time) ([]Bar, []Bar, float64, error) { reads++; return m1, m5, 0, nil }})
		var got bool
		for i := 0; i < 4; i++ { // four Evaluates inside one minute
			_, rs := datatruth.Evaluate("MNQ", c.now.Add(time.Duration(i)*time.Second))
			got = false
			for _, r := range rs {
				if r.Check == CheckLookbackSession {
					got = true
					if i == 0 {
						t.Logf("%s: %s", c.key, r.Detail)
					}
				}
			}
		}
		if got != c.blocked {
			t.Errorf("%s at %s: indicator_lookback_session blocked=%v, want %v", c.key, c.now.Format("15:04"), got, c.blocked)
		}
		if reads != 1 {
			t.Errorf("%s: the store was read %d times in one minute, want 1", c.key, reads)
		}
	}
	datatruth.Reset()
}

// calendarSrc is fakeSrc with the real exchange calendar.
type calendarSrc struct{ fakeSrc }

func (calendarSrc) Open() OpenFn { return cmeOpenForTest }

// extendFlat appends flat synthetic bars (price = the last close, 100 per 1m, each
// 5m carrying exactly the sum of its five 1m) for every open minute after the
// last stored bar, up to the last minute that closed before `until`. The
// fixtures end at the session boundary; this stands in for the live session
// that follows, so a pin can read the PRIOR session at a later wall-clock time
// without that time looking like a silent store. The extension is consistent
// between the frames by construction: it can never be the cause of a pause.
func extendFlat(m1, m5 []Bar, until time.Time) (a, b []Bar) {
	a = append([]Bar(nil), m1...)
	b = append([]Bar(nil), m5...)
	px := m1[len(m1)-1].C
	lim := until.UnixMilli() / minuteMs * minuteMs // minutes strictly before lim are closed
	for t := m1[len(m1)-1].T + minuteMs; t < lim; t += minuteMs {
		if cmeOpenForTest(t) {
			a = append(a, Bar{T: t, O: px, H: px, L: px, C: px, V: 100})
		}
	}
	for t := m5[len(m5)-1].T + 5*minuteMs; t+5*minuteMs <= lim; t += 5 * minuteMs {
		open := true
		for k := int64(0); k < 5; k++ {
			open = open && cmeOpenForTest(t+k*minuteMs)
		}
		if open {
			b = append(b, Bar{T: t, O: px, H: px, L: px, C: px, V: 500})
		}
	}
	return a, b
}

// sessionReasons runs the registered session check the way production does
// (datatruth.Evaluate over RegisterAll with a store reader) and returns its
// reason, if any.
func sessionReasons(t *testing.T, m1, m5 []Bar, now time.Time) (datatruth.Reason, bool) {
	t.Helper()
	datatruth.Reset()
	defer datatruth.Reset()
	RegisterAll(Config{Src: calendarSrc{}, Loc: chicago, Unrequired: []string{CheckIngestLoss, CheckLookbackSwing4h, CheckLookbackLevels},
		SwingInputs: func(string, time.Time) ([]Bar, []Bar, float64, error) { return m1, m5, 0, nil }})
	_, rs := datatruth.Evaluate("MNQ", now)
	for _, r := range rs {
		if r.Check == CheckLookbackSession {
			return r, true
		}
	}
	return datatruth.Reason{}, false
}

// TestSessionLevelsPriorSessionThroughEvaluate (review f7a round 3, D-A): the
// PRIOR-session path (JudgeSessionLevels step 3: pdVWAP, POC/VAH/VAL, nPOC)
// through the production call site, on the committed 10-26 fixture. Next
// session's first half hour and its 08:00 CT (the whole day the 10-26 profile
// is read as the prior one): the pause NAMES "pdVWAP/profile of session 10-26"
// and the profile. The counterpart reads the clean 10-21 session the same way
// and must not pause at all. Mutant: replace the step-3 add(...) with `_ = from`
// and the first half of this test goes red (no committed pin did before).
func TestSessionLevelsPriorSessionThroughEvaluate(t *testing.T) {
	sess := loadSessions(t)
	h1, h5 := sess["2026-10-26"].bars()
	c1, c5 := sess["2026-10-21"].bars()
	for _, now := range []time.Time{ctTime(2026, 10, 27, 17, 30), ctTime(2026, 10, 28, 8, 0)} {
		m1, m5 := extendFlat(h1, h5, now)
		r, got := sessionReasons(t, m1, m5, now)
		if !got {
			t.Fatalf("%s: the 10-26 session read as the prior session (1m hole 17:02-22:37 CT) did not pause", now.Format("01-02 15:04"))
		}
		t.Logf("%s: %s", now.Format("01-02 15:04"), r.Detail)
		for _, want := range []string{"pdVWAP/profile of session 10-26", "POC", "VAH", "VAL"} {
			if !strings.Contains(r.Detail, want) {
				t.Errorf("%s: the pause must name %q: %s", now.Format("01-02 15:04"), want, r.Detail)
			}
		}
	}
	for _, now := range []time.Time{ctTime(2026, 10, 22, 17, 30), ctTime(2026, 10, 23, 8, 0)} {
		m1, m5 := extendFlat(c1, c5, now)
		if r, got := sessionReasons(t, m1, m5, now); got {
			t.Errorf("%s: the clean 10-21 session read as the prior session paused: %s", now.Format("01-02 15:04"), r.Detail)
		}
	}
}

// TestSessionLevelsVerdictFollowsTheStore (review f7a round 3, D-B): the
// per-symbol-minute verdict cache must follow the store, in BOTH directions,
// minute by minute. Real 10-22 session; the "hole" store is the same session
// with the 1m of 17:02-22:37 CT removed (the 10-26 shape; the 5m keeps the
// span). clean -> hole -> clean -> hole on consecutive minutes: the verdict is
// F T F T, the store is read once per minute, not once ever. Mutant: key the
// cache on minute 0 and the first verdict is frozen (a clean one never pauses
// again = fail open; a blocked one never clears).
func TestSessionLevelsVerdictFollowsTheStore(t *testing.T) {
	s := loadSessions(t)["2026-10-22"]
	clean1, clean5 := s.bars()
	var hole1 []Bar
	for _, b := range clean1 {
		if b.T < s.Start+2*minuteMs || b.T >= s.Start+5*3600_000+37*minuteMs {
			hole1 = append(hole1, b)
		}
	}
	reads := 0
	useHole := false
	datatruth.Reset()
	defer datatruth.Reset()
	RegisterAll(Config{Src: calendarSrc{}, Loc: chicago, Unrequired: []string{CheckIngestLoss, CheckLookbackSwing4h, CheckLookbackLevels},
		SwingInputs: func(string, time.Time) ([]Bar, []Bar, float64, error) {
			reads++
			if useHole {
				return hole1, clean5, 0, nil
			}
			return clean1, clean5, 0, nil
		}})
	t0 := ctTime(2026, 10, 22, 23, 0)
	want := []bool{false, true, false, true}
	for i, w := range want {
		useHole = w
		got := false
		for k := 0; k < 3; k++ { // three Evaluates inside the minute
			_, rs := datatruth.Evaluate("MNQ", t0.Add(time.Duration(i)*time.Minute+time.Duration(k)*time.Second))
			got = false
			for _, r := range rs {
				if r.Check == CheckLookbackSession {
					got = true
				}
			}
		}
		if got != w {
			t.Fatalf("minute %d: blocked=%v, want %v (the cached verdict did not follow the store)", i, got, w)
		}
		if reads != i+1 {
			t.Fatalf("minute %d: %d store reads so far, want %d (one per minute)", i, reads, i+1)
		}
	}
}

// TestSessionLevelsFailClosedOnSilentStore (review f7a round 3, D-C): an OPEN
// market with an empty or stale store read (err == nil) cannot be judged, so it
// blocks - this check alone, with the swing and levels checks not registered
// (the December roll: the store is keyed to a contract with no rows yet).
// Calendar silence is not data loss: the CME break, the weekend and the first
// ten minutes after a reopen never pause. Mutant: drop the storeSilent guard
// and the empty/stale cases read clean.
func TestSessionLevelsFailClosedOnSilentStore(t *testing.T) {
	wed := ctTime(2026, 10, 28, 10, 0)
	if b, why := JudgeSessionLevels(nil, nil, wed, chicago, cmeOpenForTest, DefaultSessionVol(), 2.0); !b || !strings.Contains(why, "cannot judge") {
		t.Fatalf("empty store at an open Wednesday 10:00 CT: blocked=%v why=%q, want a cannot-judge block", b, why)
	}
	if r, got := sessionReasons(t, nil, nil, wed); !got || !strings.Contains(r.Detail, "cannot judge") {
		t.Fatalf("through Evaluate: empty store must block the session check on its own: %+v", r)
	}
	// a stale store: the real 10-22 session, whose newest bar is 1 h old at the
	// Friday 12:00 CT read (control: the full store at the same instant is clean)
	s := loadSessions(t)["2026-10-22"]
	m1, m5 := s.bars()
	fri := ctTime(2026, 10, 23, 12, 0)
	if r, got := sessionReasons(t, m1, m5, fri); got {
		t.Fatalf("control: a fully stored Friday morning must not pause: %s", r.Detail)
	}
	cut := ctTime(2026, 10, 23, 11, 0).UnixMilli()
	var stale1, stale5 []Bar
	for _, b := range m1 {
		if b.T < cut {
			stale1 = append(stale1, b)
		}
	}
	for _, b := range m5 {
		if b.T < cut {
			stale5 = append(stale5, b)
		}
	}
	if r, got := sessionReasons(t, stale1, stale5, fri); !got || !strings.Contains(r.Detail, "cannot judge") {
		t.Fatalf("a store whose newest bar is 1 h old in an open market must block: %+v", r)
	}
	// calendar silence is not loss: closed Saturday, the Friday break, and the
	// first minutes after the Sunday reopen with an empty store
	for _, c := range []time.Time{
		time.Date(2026, 10, 24, 10, 0, 5, 0, chicago),  // Saturday
		time.Date(2026, 10, 23, 16, 30, 5, 0, chicago), // Friday after the close
		time.Date(2026, 10, 25, 17, 4, 5, 0, chicago),  // Sunday, 4 min after the reopen
		time.Date(2026, 10, 28, 16, 30, 5, 0, chicago), // the daily break
	} {
		if b, why := JudgeSessionLevels(nil, nil, c, chicago, cmeOpenForTest, DefaultSessionVol(), 2.0); b {
			t.Errorf("%s: a closed or just-reopened market with an empty store must not pause: %s", c.Format("Mon 01-02 15:04"), why)
		}
	}
}
