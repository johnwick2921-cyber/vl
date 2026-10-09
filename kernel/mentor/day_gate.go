package mentor

import (
	"sync"
	"time"

	"vl/market"
)

// §7 — the pre-session routine, under 5 minutes [D5.1 p1 @ 18:36]:
//  1. turn on the key levels;
//  2. switch to the DAILY and measure how far the current daily candle has
//     already run (Asia high to low, through pre-market);
//  3. check the 4-HOUR trigger, then the 1-HOUR trigger.
//
// Table verbatim [D5.1 p1]:
//   normal run      + agree    → TRADE, following them
//   300–400+ pts    + conflict → "TẮT MÁY NGHỈ LUÔN CHO EM" (shut it off)
//   300–400+ pts    + agree    → trade, but DON'T target big —
//                                "15 điểm bán, 10 điểm bán"
//
// NOTE (CTO review E1): a NORMAL day with a 4h/1h conflict is NOT off — that
// is §5.4 case 3, a per-tick sit-out until the 1h flips. DayOff is ONLY
// spent AND conflict, read AT the open (08:30 CT) and LATCHED for the rest
// of that trading day: even if the 1h flips later, the machine stays off.
//
// The Globex session runs 17:00 CT → 08:30 CT. Before 08:30 the run is
// measured on the CURRENT session, 17:00 CT to NOW; from 08:30 on the
// 17:00→08:30 value is frozen (CTO review E2).

const (
	globexOpenMin  = 17 * 60   // 17:00 CT
	globexCloseMin = 8*60 + 30 // 08:30 CT
	// globexCoverToleranceMs (A6): the feed's earliest in-window bar may
	// open this far after the session open and still count as full
	// coverage — bar-alignment slack only. A 240-bar feed at the 08:30
	// latch starts ~04:30, hours past the tolerance, and is refused.
	globexCoverToleranceMs = 30 * 60_000
)

// DayVerdict is one row of the §7 table.
type DayVerdict int

const (
	// DayTrade: normal run, HTF agrees (or 1h silent) — trade, following them.
	DayTrade DayVerdict = iota
	// DaySpent: the day already ran SpentPts+ and the HTFs agree — trade but
	// cap the target ("15 điểm bán, 10 điểm bán").
	DaySpent
	// DayOff: spent AND conflict at the open — shut the machine off for the
	// day ("TẮT MÁY NGHỈ LUÔN CHO EM"). Latched once read.
	DayOff
	// DayNotMeasured: fewer than 2 bars cover the window — fail closed, no
	// mentor entries ("any trade you are vague about — don't" [§12]).
	DayNotMeasured
)

// DayGate holds the §7 knobs (the Config integration diff is routed via the
// CTO; until it lands, DefaultDayGate supplies the method defaults).
type DayGate struct {
	// SpentPts: a run at/above this before the open means "already run"
	// — 300 [D5.1 p1 @ 15:57 "Already run 300–400 before the open → 80–90%
	// it ranges"].
	SpentPts float64
	// TargetCapPts: on a spent day, the target distance cap — 15
	// ("15 điểm bán, 10 điểm bán" [D5.1 p1]).
	TargetCapPts float64
}

// DefaultDayGate returns the §7 defaults (300 / 15).
func DefaultDayGate() DayGate {
	return DayGate{SpentPts: 300, TargetCapPts: 15}
}

// DayLatch is the frozen §7 verdict for a TRADING DAY (CTO reviews E1, L2).
// Once frozen, nothing re-reads it — a DayOff survives a later 1h flip.
type DayLatch struct {
	// Key is the trading day this verdict belongs to: the calendar date of
	// the RTH session it names. A trading day runs 17:00 CT → 16:00 CT the
	// NEXT calendar day, so from 17:00 on, the key is tomorrow's date (and
	// a Sunday 17:00 open belongs to Monday). "" = nothing latched.
	Key     string
	Verdict DayVerdict
}

// DayRecheck is the one-way DayOff recheck bookkeeping (owner ruling
// 2026-10-09). A DayOff latch freezes at the 08:30 CT read; from then on the
// recheck re-reads the 4h/1h directions on every CLOSED 1h bar and clears the
// latch the moment they agree. Once cleared, the day never re-latches. The
// whole struct is derived deterministically from bars + config, so it rebuilds
// under Replay.
type DayRecheck struct {
	// Key is the trading day this recheck state belongs to ("" = none).
	Key string `json:"key,omitempty"`
	// ClearedDir is the direction the 4h/1h agreed on when the clear fired.
	ClearedDir Side `json:"cleared_dir,omitempty"`
	// ClearedAt is the close BOUNDARY (CloseTime + 1) of the closed 1h bar the
	// clear fired on — the same convention as BarCloseInstant, so the WARN line
	// prints "11:00" for the 10:00 bar's close.
	ClearedAt int64 `json:"cleared_at,omitempty"`
	// Clears is the per-day clear count (always 0 or 1 — one-way).
	Clears int `json:"clears,omitempty"`
	// Last1HClose is the CloseTime of the newest closed 1h bar the recheck has
	// already evaluated — the recheck runs once per NEW closed 1h bar.
	Last1HClose int64 `json:"last_1h_close,omitempty"`
}

// DayGateReport is the once-per-day record of the §7 day-gate decision
// (owner ruling 2026-10-09): captured the moment the 08:30 latch freezes so
// the per-day INFO line reads the gate's OWN inputs — not a later re-read.
type DayGateReport struct {
	Key      string     `json:"key,omitempty"`
	Verdict  DayVerdict `json:"verdict"`
	RunPts   float64    `json:"run_pts,omitempty"`
	HaveRun  bool       `json:"have_run"`
	HiPx     float64    `json:"hi_px,omitempty"`
	HiAt     int64      `json:"hi_at,omitempty"`
	LoPx     float64    `json:"lo_px,omitempty"`
	LoAt     int64      `json:"lo_at,omitempty"`
	FourHDir Side       `json:"fourh_dir,omitempty"`
	OneHDir  Side       `json:"oneh_dir,omitempty"`
	Conflict bool       `json:"conflict,omitempty"`
	SpentPts float64    `json:"spent_pts,omitempty"`
}

// recheckDayOff (owner ruling 2026-10-09) runs the one-way DayOff recheck:
// after a DayOff latches at the open, every NEW closed 1h bar re-reads the
// 4h/1h directions; the moment they AGREE the latch clears for the rest of the
// trading day — never re-latched. The cleared day lands on DaySpent, not
// DayTrade: the run is still spent (that is why it latched), only the conflict
// is gone, so the §7 "trade, but don't target big" cap still applies.
func recheckDayOff(l DayLatch, rc *DayRecheck, h HTF, bars1h []market.Kline, now int64, loc *time.Location) DayLatch {
	if loc == nil {
		loc = ctime()
	}
	key := tradingDayKey(time.UnixMilli(now).In(loc))
	// The recheck only touches a latched DayOff for TODAY.
	if l.Key != key || l.Verdict != DayOff {
		if rc.Key != key {
			*rc = DayRecheck{Key: key}
		}
		return l
	}
	if rc.Key != key {
		*rc = DayRecheck{Key: key}
	}
	var newest int64
	for _, b := range bars1h {
		if b.CloseTime > newest {
			newest = b.CloseTime
		}
	}
	if newest == 0 || newest <= rc.Last1HClose {
		return l // no new closed 1h bar since the last recheck
	}
	rc.Last1HClose = newest
	if ok, dir := HTFAgreement(h); ok {
		rc.ClearedDir = dir
		rc.ClearedAt = newest + 1 // the close boundary, like BarCloseInstant
		rc.Clears++
		return DayLatch{Key: key, Verdict: DaySpent}
	}
	return l
}

// ctime loads America/Chicago (the mentor quotes all times in US Central
// [D4.4 p1 @ 01:45 "em tính giờ Texas"]). Falls back to a fixed −6h zone if
// tzdata is unavailable.
var chicagoOnce sync.Once
var chicagoLoc *time.Location
var chicagoErr error

func ctime() *time.Location {
	// LoadLocation per call is the replay killer: bucketOpen/rthHourAnchor/
	// rthMinuteOf call ctime() PER BAR, and O(n) per tick over 12k ticks is
	// O(n^2) LoadLocation calls. Load once; the rest is a cached pointer.
	chicagoOnce.Do(func() { chicagoLoc, chicagoErr = time.LoadLocation("America/Chicago") })
	if chicagoErr == nil {
		return chicagoLoc
	}
	return time.FixedZone("CST6", -6*3600)
}

// tradingDayKey returns the calendar date of the trading day the moment
// belongs to (CTO review L2). Before 17:00 CT the trading day is the current
// calendar day (its RTH session opened at 17:00 yesterday); at/after 17:00
// the trading day is tomorrow (the session that just opened).
func tradingDayKey(t time.Time) string {
	if t.Hour() >= globexOpenMin/60 {
		return t.AddDate(0, 0, 1).Format("2006-01-02")
	}
	return t.Format("2006-01-02")
}

// sessionBounds returns the Globex session window for a moment in time:
// the 17:00 CT open and the 08:30 CT close of the session now belongs to.
func sessionBounds(now int64, loc *time.Location) (openMs, closeMs int64) {
	t := time.UnixMilli(now).In(loc)
	open := time.Date(t.Year(), t.Month(), t.Day(), globexOpenMin/60, globexOpenMin%60, 0, 0, loc)
	if open.After(t) {
		open = open.AddDate(0, 0, -1)
	}
	close := time.Date(open.Year(), open.Month(), open.Day()+1, globexCloseMin/60, globexCloseMin%60, 0, 0, loc)
	return open.UnixMilli(), close.UnixMilli()
}

// GlobexMeasurement is the §7 step-2 run WITH its own extremes (owner ruling
// 2026-10-09: the per-day day-gate log line reads the gate's own inputs — run,
// hi/lo and their bar times).
type GlobexMeasurement struct {
	Run  float64
	OK   bool
	Hi   float64
	HiAt int64 // OpenTime (ms) of the bar whose high set Hi
	Lo   float64
	LoAt int64 // OpenTime (ms) of the bar whose low set Lo
}

// GlobexMeasure is GlobexRun plus the extremes the run is made of. Before
// 08:30 CT the window is the CURRENT session from 17:00 CT to NOW; from 08:30
// CT on it is the frozen 17:00→08:30 high−low of the session that just ended
// (CTO review E2). OK=false when fewer than 2 bars cover the window (or the
// coverage guard trips) — the gate then reads DayNotMeasured.
func GlobexMeasure(bars []market.Kline, now int64, loc *time.Location) GlobexMeasurement {
	if loc == nil {
		loc = ctime()
	}
	var m GlobexMeasurement
	openMs, closeMs := sessionBounds(now, loc)
	hiMs := now
	if !time.UnixMilli(now).In(loc).Before(time.UnixMilli(closeMs).In(loc)) {
		hiMs = closeMs // session ended: freeze the 17:00→08:30 value
	}
	loMs := openMs

	n := 0
	var earliest int64
	for _, b := range bars {
		if b.OpenTime < loMs || b.OpenTime >= hiMs {
			continue
		}
		if n == 0 {
			m.Hi, m.HiAt = b.High, b.OpenTime
			m.Lo, m.LoAt = b.Low, b.OpenTime
			earliest = b.OpenTime
		} else {
			if b.High > m.Hi {
				m.Hi, m.HiAt = b.High, b.OpenTime
			}
			if b.Low < m.Lo {
				m.Lo, m.LoAt = b.Low, b.OpenTime
			}
		}
		n++
	}
	if n < 2 {
		return m
	}
	// A6 coverage guard [D5.1 p1 @17:44-17:56]: the run is the FULL
	// Globex session (Asia high -> pre-market low). A feed that starts
	// mid-window under-measures it and would fabricate a "normal" day —
	// the day gate must read not-measured instead (fail closed, §12).
	// The tolerance is bar-alignment slack only, not a measurement window.
	if earliest > loMs+globexCoverToleranceMs {
		return m
	}
	m.Run = m.Hi - m.Lo
	m.OK = true
	return m
}

// GlobexRun measures the §7 step-2 run [D5.1 p1 @ 18:36]. See GlobexMeasure.
func GlobexRun(bars []market.Kline, now int64, loc *time.Location) (run float64, ok bool) {
	m := GlobexMeasure(bars, now, loc)
	return m.Run, m.OK
}

// DayGateVerdict applies the §7 table. conflict is HTFConflict(htf):
//
//	unmeasured run               → DayNotMeasured (fail closed, §12)
//	run >= SpentPts AND conflict → DayOff (spent + conflict only)
//	run >= SpentPts              → DaySpent (small targets)
//	otherwise (incl. a conflict  → DayTrade — §5.4 case 3 handles the
//	on a normal day)              per-tick sit-out until the flip
func DayGateVerdict(run float64, haveRun bool, conflict bool, g DayGate) (DayVerdict, string) {
	if g.SpentPts <= 0 || g.TargetCapPts <= 0 {
		g = DefaultDayGate()
	}
	if !haveRun {
		return DayNotMeasured, "day run not measured — no mentor entries ('any trade you are vague about — don't' [§12])"
	}
	if conflict && run >= g.SpentPts {
		return DayOff, "spent AND 4h/1h conflict at the pre-open read — 'TẮT MÁY NGHỈ LUÔN CHO EM', no trades today [D5.1 p1 @ 19:22]"
	}
	if run >= g.SpentPts {
		return DaySpent, "already run 300–400+ before the open — trade but don't target big, 15/10 pt sales [D5.1 p1 @ 15:57]"
	}
	return DayTrade, ""
}

// LatchDay is the call site the evaluator uses (CTO review E1 + L1 + L2):
// the §7 verdict is read AT the open and then LATCHES for the rest of that
// trading day. Before 08:30 CT the verdict stays LIVE — recomputed every
// tick (the overnight swing needs it; a pre-open spent+conflict does NOT
// latch, because §7 is a pre-session read taken at the open). The latch
// happens only at the first tick at/after 08:30 CT of the trading day, and
// the key is the trading day (17:00 CT → next calendar day), not the
// calendar date.
func LatchDay(l DayLatch, now int64, loc *time.Location, run float64, haveRun bool, conflict bool, g DayGate) DayLatch {
	if loc == nil {
		loc = ctime()
	}
	t := time.UnixMilli(now).In(loc)
	key := tradingDayKey(t)
	openDay, _ := time.Parse("2006-01-02", key)
	open := time.Date(openDay.Year(), openDay.Month(), openDay.Day(), globexCloseMin/60, globexCloseMin%60, 0, 0, loc)
	if l.Key == key && !t.Before(open) {
		// This trading day's pre-session read is done: frozen.
		return l
	}
	v, _ := DayGateVerdict(run, haveRun, conflict, g)
	if t.Before(open) {
		// Pre-open: live, never latched (L1). Key "" so nothing freezes
		// a stale pre-open value at 08:30.
		return DayLatch{Key: "", Verdict: v}
	}
	return DayLatch{Key: key, Verdict: v}
}

// CapTargetForDay applies the §7 spent-day cap: on a DaySpent day the target
// distance is capped at TargetCapPts ("15 điểm bán, 10 điểm bán" — he sells
// at 15 or 10, not at the full target). The cap moves the target CLOSER to
// entry, never further, and never flips the side.
func CapTargetForDay(in Intent, v DayVerdict, g DayGate) Intent {
	if v != DaySpent || in.Price == 0 || in.Target == 0 {
		return in
	}
	if g.TargetCapPts <= 0 {
		g = DefaultDayGate()
	}
	dist := in.Target - in.Price
	if in.Side == SideShort {
		dist = in.Price - in.Target
	}
	if dist <= g.TargetCapPts {
		return in
	}
	if in.Side == SideShort {
		in.Target = in.Price - g.TargetCapPts
	} else {
		in.Target = in.Price + g.TargetCapPts
	}
	return in
}

// dayGateRefusal — A10 (CTO 20:15:49Z): ONE day gate for every intraday
// setup. Returns the ledger reason for a verdict that forbids trading today,
// "" otherwise. Each emit site composes its own counter key from it
// ("isb_" + dayGateRefusal(...)) so the blocked setup is always named.
func dayGateRefusal(v DayVerdict) string {
	switch v {
	case DayOff:
		return "day_off"
	case DayNotMeasured:
		return "day_not_measured"
	}
	return ""
}
