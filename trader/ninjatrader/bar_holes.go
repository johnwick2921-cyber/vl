package ninjatrader

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/kernel"
	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// ── GAP-BACKFILL (owner 08:23 CT 2026-10-14: "check how it come back, fix this
// bug") — a bar HOLE is refilled by the bot itself ─────────────────────────
//
// What happened [A]: 03:44:04 CT NinjaTrader lost its Tradovate connections and
// its Historical Data Server; no bar reached the bot until 07:48 ("FEED DOWN"
// every minute 03:54→07:49). At 07:49:55 the AddOn rebuilt its BarsRequests and
// NT8 answered bars=0/1 for every timeframe — its DB did not have the gap yet.
// Live bars resumed and the store kept a 244-minute 1m HOLE (03:44–07:48): the
// owner's chart jumped 200 points across it and every 5m/15m/1h candle spanning
// it was wrong. At 08:16 the owner reloaded NT8's history; NOTHING on the bot
// asked again. The hole filled at 08:22 only because the release #22 boot
// re-sent bars_subscribe — an unrelated restart.
//
// What this does:
//  1. DETECT. Every live 1m bar the persister writes is checked against the
//     store: a run of ≥ N OPEN-MARKET minutes with no stored bar between two
//     stored bars is a hole [from, to) — `from` is the first missing minute,
//     `to` the bar that closed it. "Open" is the one CME calendar
//     (barHoleExpected: kernel.IsCMEOpen + the session calendar by trading
//     date), so the 16:00–17:00 break, weekends and holiday closes are never
//     holes. N = BARS_HOLE_MIN_GAP_MIN (default 3). Logged once, counted.
//  2. REFILL. Each hole gets re-requests at +1/+3/+10/+30 min after it was seen,
//     through the wire's gap re-request (RequestGapBackfillAt — the
//     bars_subscribe resend that healed the hole at 08:22, on a budget separate
//     from the scale-break one). Never while the link or feed is down or CME is
//     closed: a BarsRequest run in an outage returns bars=0.
//  3. RE-MEASURE from the store after each attempt. Filled → logged + counted
//     with its attempt number, schedule stops. Still open after the fourth →
//     ONE loud line naming the window and the manual fix (an NT8 history
//     reload), counted; then one slow retry every 15 min while CME is open,
//     at most barHoleSlowCapTotal IN TOTAL per hole (#23 review P2-3: the old
//     per-session-day cap polled an unfillable hole forever), so the owner's
//     reload is picked up by itself.
//  4. RETIRE what no re-request can reach (#23 review P2-3): a re-request
//     replays the last bars_back 1m bars, so a hole whose first missing minute
//     lies more than bars_back open minutes back is outside NT8's replay
//     window — one line, counted, never asked for again.
//  5. SEND TIMING (#23 review P2-1): a re-request makes the AddOn rebuild its
//     BarsRequests, which freezes the symbol's forming bars for 20–35 s; a
//     freeze that crosses a minute boundary stores that 1m candle truncated,
//     as LIVE. Sends go out only in the first barHoleSendWindow of a CT minute,
//     so the freeze ends before the boundary.
//  6. SHOW. /api/health serves the open holes as read values.

// WHY 1m ONLY (m424 P3, decided not extended). A gap re-request is the AddOn's
// bars_subscribe resend: it rebuilds EVERY timeframe's BarsRequest and replays
// ≈bars_back bars per tf, so the refill a 1m hole triggers already heals the
// 2m/5m bars spanning it. A separate 2m/5m detector would send the same single
// request on the same budget, and would need its own reach arithmetic (the
// retire rule counts 1m open minutes) and a per-tf minute width in findBarHoles
// — new code paths for no additional healing. A 5m-only hole (1m intact) is not
// a real shape: the 5m bar is built by NT8 from the same ticks.
const (
	barHoleTF            = "1m"
	barHoleMinGapDefault = 3
	barHoleSlowEvery     = 15 * time.Minute
	barHoleSlowCapTotal  = 8               // slow re-requests per hole, in TOTAL (#23 review P2-3)
	barHoleSettle        = 2 * time.Minute // replay lands + the hold releases on the next live frame
	barHoleTickEvery     = 15 * time.Second
	// barHoleSendWindow — sends only while now.In(CT).Second() is below this
	// (#23 review P2-1). It equals the tick period ON PURPOSE: with 15 s ticks
	// exactly one tick per minute lands in [0, 15 s), whatever the ticker's
	// phase. A 10 s window would be missed by EVERY tick for a third of the
	// phases (ticks at :10/:25/:40/:55) and the hole would never be asked for.
	// Worst case: a send at :14.9 + a 35 s freeze ends at :50, before the
	// boundary (the review's limit was "sends after about :25").
	barHoleSendWindow = barHoleTickEvery
	barHoleSpacing    = 60 * time.Second // one gap re-request per symbol per minute, at most
	barHoleMaxOpen    = 32
	// barHoleMaxRetired bounds the RETIRED holes the tracker keeps LISTED (m444
	// review round 2): a retired hole is not being worked on, so it must not eat
	// one of the barHoleMaxOpen slots that new holes need — but the list is not
	// unbounded either.
	barHoleMaxRetired = 128

	// A hole older than the subscription's own replay (bars_back = 2000) is not
	// given up on (m444 review round 2): the bot asks NT8 for it with a DEEP
	// 1m re-request (provider RequestDeepGapBackfillAt, up to
	// DeepGapBackfillMaxBarsBack bars). A deep replay is ~4x a standard one, so
	// it gets a SMALLER budget: barHoleDeepScheduled scheduled asks, then
	// UNFILLED, then barHoleDeepSlowCap slow ones.
	barHoleDeepScheduled = 2
	barHoleDeepSlowCap   = 2
	// barHoleDeepMargin — bars added to the open minutes between the hole's
	// first missing minute and now, so the replay reaches a little past it.
	barHoleDeepMargin = 30

	holeStateRefilling = "refilling"
	holeStateUnfilled  = "unfilled"
	// holeStateRetired (m444): the hole's first missing minute lies beyond NT8's
	// replay window, so no re-request can reach it. It used to be DROPPED from
	// the tracker (and so from /api/health) the moment it was retired; the
	// persistent gap audit would then re-find it every pass. It now stays
	// LISTED — unfillable by the bot, still missing, still owed to the owner —
	// and is never scheduled again.
	holeStateRetired = "retired"

	holeSourceLive  = "live"  // found by observeLive (a live 1m arrival / the boot scan)
	holeSourceAudit = "audit" // found by the persistent gap audit (bar_gap_audit.go)
)

// barHoleSchedule — the scheduled attempts, as offsets from the moment the hole
// was seen. After the last one the hole is either filled or declared unfilled.
var barHoleSchedule = []time.Duration{1 * time.Minute, 3 * time.Minute, 10 * time.Minute, 30 * time.Minute}

// barHoleMinGap resolves the knob and names its source for the boot line (A11).
func barHoleMinGap() (int, string) {
	v := strings.TrimSpace(os.Getenv("BARS_HOLE_MIN_GAP_MIN"))
	if v == "" {
		return barHoleMinGapDefault, "default"
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return barHoleMinGapDefault, fmt.Sprintf("default — env %q unreadable", v)
	}
	return n, "env"
}

// holeStore is the store read the tracker needs (*store.BarHistoryStore).
type holeStore interface {
	PrevOpenTimeOn(symbol, tf, contract string, beforeMs int64) (int64, bool, error)
	BarsBetweenOn(symbol, tf, contract string, fromMs, toMs int64) ([]store.BarHistoryDB, error)
}

// deepHoleWire is the optional deep re-request (*ntwire.TCPServer has it). A
// wire without it (a fake) leaves a hole beyond the standard replay RETIRED.
type deepHoleWire interface {
	RequestDeepGapBackfillAt(symbol string, barsBack int, now time.Time) error
}

// holeWire is the wire the tracker needs (*ntwire.TCPServer).
type holeWire interface {
	IsConnected() bool
	IsFeedConnected() bool
	RequestGapBackfillAt(symbol string, now time.Time) error
	BarCache() *ntwire.BarCache
	CurrentContract(symbol string) (ntwire.ContractFact, bool)
	// BarsBackAsked is the bars_back the subscribe frame carried — how far a
	// re-request's replay reaches (0 = no subscribe yet → reach unknown).
	BarsBackAsked() int
}

// barHoleInSendWindow — the send-timing rule (item 5 of the header).
func barHoleInSendWindow(now time.Time) bool {
	in := now.In(kernel.CTLocation())
	return time.Duration(in.Second())*time.Second+time.Duration(in.Nanosecond()) < barHoleSendWindow
}

// barHoleExpected reports whether CME trades the 1m bar opening at m — the
// ONE calendar, read the way its rows are keyed. kernel.IsCMEOpen answers for
// the instant (the 16:00–17:00 break, weekends, a closed calendar date); the
// session_calendar.json rows are keyed by TRADING date (CMETradingDateKey: the
// date a session closes on), so the evening session that would belong to a
// closed day — Thursday 17:00+ before Good Friday, Sunday 17:00+ before a
// closed Monday — reads open by the instant alone. Found by this wave's own
// holiday pin: Good Friday 2026-04-03 came out as a 420-minute "hole"
// (Thu 04-02 17:00–24:00). A minute is expected only when BOTH say it trades.
func barHoleExpected(m time.Time) bool { return kernel.IsCMETradingExpected(m) }

// barHoleSpan is one stretch with no stored bar between two stored bars.
type barHoleSpan struct {
	FromMs, ToMs int64 // [first missing minute, the bar that closed it)
	OpenMissing  int   // missing minutes the CME calendar calls OPEN
}

// findBarHoles walks ascending 1m open times and returns every stretch whose
// OPEN-market missing minutes number at least minGap. A minute the calendar
// calls closed (the daily break, a weekend, a holiday close) is never missing;
// production passes barHoleExpected.
func findBarHoles(times []int64, minGap int, isOpen func(time.Time) bool) []barHoleSpan {
	const minute = int64(60_000)
	var out []barHoleSpan
	for i := 1; i < len(times); i++ {
		a, b := times[i-1], times[i]
		if b-a <= minute {
			continue
		}
		missing := 0
		for m := a + minute; m < b; m += minute {
			if isOpen(time.UnixMilli(m)) {
				missing++
			}
		}
		if missing >= minGap {
			out = append(out, barHoleSpan{FromMs: a + minute, ToMs: b, OpenMissing: missing})
		}
	}
	return out
}

// barHole is one tracked hole.
type barHole struct {
	Symbol, Contract string
	FromMs, ToMs     int64
	MissingAtDetect  int
	Missing          int // open minutes still missing at the last measure
	DetectedAt       time.Time
	Attempts         int
	State            string
	Source           string // holeSourceLive | holeSourceAudit

	// deep (m444 review round 2): the hole is older than the subscription's
	// standard replay but inside DeepGapBackfillMaxBarsBack, so its re-requests
	// are DEEP ones for deepBarsBack 1m bars (recomputed at every send).
	deep         bool
	deepBarsBack int

	nextAttemptAt time.Time // zero = nothing scheduled until the pending measure
	measureAt     time.Time // zero = no measure pending
	saidDeferred  int       // attempt index (1-based) whose deferral was already said
	saidRefused   int       // attempt index (1-based) whose refusal was already said
	slowCount     int       // slow re-requests SENT, in total (cap barHoleSlowCapTotal)
	saidSlowCap   bool      // the "slow retries spent" line was said

	// disagreedAtDetect is the cache's disagreeing-replay count FOR THIS SYMBOL when this hole
	// was detected (m438): the owner push reports only the disagreements that
	// happened SINCE, not the process-lifetime total.
	disagreedAtDetect int64

	reachCountedMs int64 // replay-reach walk: next minute to count (0 = from FromMs)
	reachOpen      int   // open minutes counted in [FromMs, reachCountedMs)
}

// schedLen is how many scheduled attempts the hole gets before it is declared
// unfilled; slowCap is how many slow re-requests it gets after that.
func (h *barHole) schedLen() int {
	if h.deep {
		return barHoleDeepScheduled
	}
	return len(barHoleSchedule)
}

// spentFix is what the "slow retries spent" line tells the log reader to do.
func (h *barHole) spentFix() string {
	if h.deep {
		return "an NT8 history reload, then POST /api/nt/bar-arbiter {action: backfill, timeframe: 1m, bars_back: N} (the deep ask by hand) — a bot restart re-reads only the last 2000 1m bars and does not reach this hole"
	}
	return "an NT8 history reload plus a bot restart (which re-subscribes) is the fix while it is inside NT8's replay window"
}

func (h *barHole) slowCap() int {
	if h.deep {
		return barHoleDeepSlowCap
	}
	return barHoleSlowCapTotal
}

type barHoleTracker struct {
	mu        sync.Mutex
	st        holeStore
	wire      func() holeWire
	clock     func() time.Time
	isOpen    func(time.Time) bool
	say       func(level, msg string)
	minGap    int
	minGapSrc string
	// receiptClock is the instant the live-bar receipt age is measured against.
	// The receipts carry the monotonic clock, so this is time.Now in production
	// and never the tracker's (possibly fixed) schedule clock.
	receiptClock func() time.Time

	// the persistent gap audit (bar_gap_audit.go) — same tracker, same holes
	auditDays     int // CME session days read per pass; 0 = audit off
	auditDaysSrc  string
	auditEvery    time.Duration
	auditEverySrc string
	audit         barGapAuditState

	holes    []*barHole
	checked  map[string]int64     // symbol|contract -> newest live 1m open time judged
	lastFire map[string]time.Time // symbol -> last gap re-request this tracker SENT
	saidErr  map[string]bool      // one WARN per store-read failure kind

	// DATA-INTEGRITY GATE (m445, bar_integrity.go): the verdict every entry path
	// asks. The tracker keeps what it needs to answer — all additive:
	retired       []*barHole         // holes no re-request can reach (kept: a retirement is NOT a fill)
	judgeErr      map[string]string  // symbol|contract -> the store-read failure that left a minute unjudged (cleared by the next good judgement)
	coveredFrom   map[string]int64   // symbol|contract -> oldest minute any judgement read back to
	integ         integrityMemo      // the window-start walk, memoised per wall minute
	clearsMemo    map[[2]int64]int64 // (last missing open minute, window) -> instant it leaves the window; under t.mu
	lastRemeasure time.Time          // the retired holes' last store re-read
	// retiredEvicted — the retained list overflowed and its oldest entries were
	// dropped: the verdict is UNKNOWN (blocked) from then on, until a restart.
	retiredEvicted bool
}

// barHoles is the process tracker: the persister reports to it, the health
// surface reads it. nil = not wired (no store) — the health field is ABSENT.
var barHoles atomic.Pointer[barHoleTracker]

// barHolesServer is the wire the production tracker sends on, bound once by
// the boot goroutine that resolves it (nil until then: no wire, every attempt
// is DEFERRED, never a call into the singleton accessor from the ticker).
var barHolesServer atomic.Pointer[ntwire.TCPServer]

func newBarHoleTracker(st holeStore, server func() *ntwire.TCPServer) *barHoleTracker {
	n, src := barHoleMinGap()
	days, daysSrc := barGapAuditDays()
	every, everySrc := barGapAuditEvery()
	return &barHoleTracker{
		auditDays: days, auditDaysSrc: daysSrc, auditEvery: every, auditEverySrc: everySrc,
		st: st,
		wire: func() holeWire {
			s := server()
			if s == nil {
				return nil // never a non-nil interface around a nil server
			}
			return s
		},
		clock:        time.Now,
		receiptClock: time.Now,
		isOpen:       barHoleExpected,
		say:          sayToLogger,
		minGap:       n,
		minGapSrc:    src,
		checked:      map[string]int64{},
		lastFire:     map[string]time.Time{},
		saidErr:      map[string]bool{},
		judgeErr:     map[string]string{},
		coveredFrom:  map[string]int64{},
	}
}

func sayToLogger(level, msg string) {
	switch level {
	case "ERROR":
		logger.Errorf("%s", msg)
	case "WARN":
		logger.Warnf("%s", msg)
	default:
		logger.Infof("%s", msg)
	}
}

// bootLine prints the armed configuration, every value read (A11).
func (t *barHoleTracker) bootLine() string {
	return fmt.Sprintf("🕳 bar-hole backfill: armed on %s · hole = ≥%d open minute(s) with no stored bar [%s, env BARS_HOLE_MIN_GAP_MIN] · re-request at %s then every %s while CME is open (≤%d slow re-requests per hole in total) · sent only in the first %s of a CT minute · a hole outside NT8's replay window (the subscription's bars_back of 1m bars) is retired · wire budget %d/day per symbol, SEPARATE from the scale-break re-request (1/boot) · never while the link or feed is down (gap-backfill 2026-10-14)",
		barHoleTF, t.minGap, t.minGapSrc, scheduleText(), strings.TrimSuffix(barHoleSlowEvery.String(), "0s"),
		barHoleSlowCapTotal, barHoleSendWindow, ntwire.GapBackfillDayCap())
}

// holeWindowCT renders [from, to) for a log line, in CT.
// Layouts route through kernel/tz.go (the TZ guard refuses a bare one here).
func holeWindowCT(fromMs, toMs int64) string {
	f := kernel.TableTimeCT(time.UnixMilli(fromMs)) // "01-02 15:04", CT
	e := kernel.TableTimeCT(time.UnixMilli(toMs))
	if f[:5] == e[:5] {
		return fmt.Sprintf("%s–%s CT", f, kernel.CloseHHMMCT(time.UnixMilli(toMs)))
	}
	return fmt.Sprintf("%s–%s CT", f, e)
}

func holeKey(symbol, contract string) string { return symbol + "|" + contract }

// observeLive is THE DETECTOR, called by the persister after it has written a
// live frame's closed rows. Only 1m on a named contract is judged; each new
// newest minute is judged once, against what the store held since the last
// judged minute. The FIRST judgement per (symbol, contract) this process is
// the BOOT SCAN (CTO fold 2, 10-14): it reads back over the current CME
// session day from its 17:00 CT open — or over the replay window when that is
// shorter (bootScanFromLocked) — and never less far than the stored bar just
// before the first live minute (the pre-fold reach). Release #23 booted 11:22
// with ES 1m missing 03:44–07:48 (one stored bar in that span) and health read
// bars_holes [] — a hole older than the boot was never judged. Holes the scan
// finds are tracked exactly like a live one; one line says what it covered.
func (t *barHoleTracker) observeLive(symbol, tf, contract string, rows []store.BarHistoryDB) {
	if t == nil || tf != barHoleTF || strings.TrimSpace(contract) == "" || len(rows) == 0 {
		return
	}
	oldest, newest := rows[0].OpenTimeMs, rows[0].OpenTimeMs
	for _, r := range rows {
		if r.OpenTimeMs < oldest {
			oldest = r.OpenTimeMs
		}
		if r.OpenTimeMs > newest {
			newest = r.OpenTimeMs
		}
	}
	key := holeKey(symbol, contract)
	t.mu.Lock()
	defer t.mu.Unlock()
	last, seen := t.checked[key]
	if seen && newest <= last {
		return
	}
	from := oldest
	bootScan, bootBound := false, ""
	if seen {
		from = last
	} else {
		prev, ok, err := t.st.PrevOpenTimeOn(symbol, barHoleTF, contract, oldest)
		if err != nil {
			t.noteJudgeErrLocked(key, err)
			t.sayErrOnce("prev", fmt.Sprintf("🕳 bar-hole detector: store read failed for %s %s on %s (%v) — this minute is not judged; the next one is", symbol, barHoleTF, contract, err))
			return
		}
		if ok {
			from = prev
		}
		// the boot scan: as far back as the session open / replay window,
		// never less far than before the fold
		scanFrom, bound := t.bootScanFromLocked(newest)
		if scanFrom < from {
			from = scanFrom
		}
		bootScan, bootBound = true, bound
	}
	// m445: the BOOT SCAN's first read starts at the session open, with no bar
	// before it to measure the first gap against — so a hole that STARTS at the
	// open (the 17:00 reopen lag; 2026-10-26 17:00–21:57 CT) was invisible to a
	// restart: the scan saw bars from 21:57 and no gap. Anchor it on the stored
	// bar just before the scan's first minute (one bar back; `from` stays the
	// scan's reach for the boot line and the coverage).
	readFrom := from
	if bootScan {
		if p, ok, perr := t.st.PrevOpenTimeOn(symbol, barHoleTF, contract, from); perr != nil {
			t.noteJudgeErrLocked(key, perr)
			t.sayErrOnce("prev", fmt.Sprintf("🕳 bar-hole detector: store read failed for %s %s on %s (%v) — this minute is not judged; the next one is", symbol, barHoleTF, contract, perr))
			return
		} else if ok {
			readFrom = p
		}
	}
	got, err := t.st.BarsBetweenOn(symbol, barHoleTF, contract, readFrom, newest+1)
	if err != nil {
		t.noteJudgeErrLocked(key, err)
		t.sayErrOnce("scan", fmt.Sprintf("🕳 bar-hole detector: store read failed for %s %s on %s (%v) — this minute is not judged; the next one is", symbol, barHoleTF, contract, err))
		return
	}
	t.checked[key] = newest
	t.noteJudgedLocked(key, from)
	if bootScan {
		got = trimPreRecording(got)
	}
	times := make([]int64, 0, len(got))
	for _, r := range got {
		times = append(times, r.OpenTimeMs)
	}
	now := t.clock()
	spans := findBarHoles(times, t.minGap, t.isOpen)
	if bootScan {
		t.say(bootScanLevel(spans), bootScanLine(symbol, contract, from, newest, bootBound, spans))
	}
	for _, sp := range spans {
		if t.trackedLocked(symbol, contract, sp) {
			continue
		}
		if t.atCapLocked() {
			telemetry.IncBarsHoleOverflow()
			t.retainLocked(symbol, contract, sp.FromMs, sp.ToMs, sp.OpenMissing, "untracked") // m445: not tracked is NOT filled — still a hole the gate must see
			t.say("WARN", fmt.Sprintf("🕳 bars hole NOT tracked: %s %s %s on %s (%d open minute(s) missing) — %d holes already open, the cap; it will be seen again once one closes",
				symbol, barHoleTF, holeWindowCT(sp.FromMs, sp.ToMs), contract, sp.OpenMissing, barHoleMaxOpen))
			continue
		}
		h := &barHole{
			Symbol: symbol, Contract: contract, FromMs: sp.FromMs, ToMs: sp.ToMs,
			MissingAtDetect: sp.OpenMissing, Missing: sp.OpenMissing,
			DetectedAt: now, State: holeStateRefilling, Source: holeSourceLive,
			nextAttemptAt:     now.Add(barHoleSchedule[0]),
			disagreedAtDetect: t.disagreedNowLocked(symbol),
		}
		t.holes = append(t.holes, h)
		telemetry.IncBarsHoleDetected()
		t.say("WARN", fmt.Sprintf("🕳 bars HOLE detected: %s %s %s on %s — %d open minute(s) with no stored bar (threshold %d, %s); NT8 history re-request scheduled at %s after now, never while the feed is down (gap-backfill)",
			symbol, barHoleTF, holeWindowCT(sp.FromMs, sp.ToMs), contract, sp.OpenMissing, t.minGap, t.minGapSrc, scheduleText()))
	}
}

// trimPreRecording drops the LEADING rows of a boot-scan read that the bot did
// not record: the history-import snapshots (store.BarSourceHistoricalImport)
// that stand in the store before the contract's first recorded (live / replayed)
// bar. A contract that has just rolled in is stored sparsely before the bot
// subscribes to it — on the live store 12-26 holds one import row a day for
// 09-07..09-10 and then dense data from 09-14 — and those stretches are not
// minutes the bot lost: it was not on that contract yet, and the stitched
// history takes them from the older contract. Read as holes they pause the
// gate for the whole window after every roll (verified by replaying the real
// 12-26 rows). Only LEADING import rows go; an import row after the first
// recorded bar is data like any other, and a store with no recorded bar at all
// is judged whole. (Review round 2, window + false pauses.)
func trimPreRecording(rows []store.BarHistoryDB) []store.BarHistoryDB {
	for i, r := range rows {
		if r.Source != store.BarSourceHistoricalImport {
			return rows[i:]
		}
	}
	return rows
}

// bootScanFromLocked is the boot scan's first minute for a key whose newest
// live minute is newestMs. m445 (review round 1, D2): it reaches back as far as
// the DEEPEST data window the data-integrity gate judges (IntegritySwingOpenMin
// open minutes) — before, it stopped at the current CME session day's open, so a
// restart after the session roll judged only that day and every older hole was
// silently clear in the deep window (the 10-26 17:00–21:57 hole, booted at
// 10-28 06:00: blocked=false). One BarsBetweenOn read of at most ~25k rows
// (IntegritySwingOpenMin open minutes) per key, once per process. The shorter bounds (session open,
// bars_back replay window) are kept for the line and never reached less far.
// Returns the bound and its name for the boot line.
func (t *barHoleTracker) bootScanFromLocked(newestMs int64) (int64, string) {
	deep := t.windowFromLocked(IntegritySwingOpenMin, time.UnixMilli(newestMs))
	shallow, bound := t.bootScanShallowLocked(newestMs)
	if deep < shallow {
		return deep, fmt.Sprintf("the deepest integrity window (%d open min back)", IntegritySwingOpenMin)
	}
	return shallow, bound
}

func (t *barHoleTracker) bootScanShallowLocked(newestMs int64) (int64, string) {
	const minute = int64(60_000)
	start := kernel.CMESessionDayStart(time.UnixMilli(newestMs)).UnixMilli()
	bb := 0
	if w := t.wire(); w != nil {
		bb = w.BarsBackAsked()
	}
	if bb <= 0 {
		return start, "the session open (bars_back n/a)"
	}
	open := 0
	for m := newestMs; m >= start; m -= minute {
		if t.isOpen(time.UnixMilli(m)) {
			open++
			if open >= bb {
				return m, fmt.Sprintf("the replay window (bars_back %d, shorter than the session)", bb)
			}
		}
	}
	return start, fmt.Sprintf("the session open (inside the bars_back %d replay window)", bb)
}

// rthMissing counts the OPEN minutes in [fromMs, toMs) that lie inside the
// regular trading hours the key-level walk reads: 08:30 <= CT time of day < 15:00.
func (t *barHoleTracker) rthMissing(fromMs, toMs int64) int {
	const minute = int64(60_000)
	n := 0
	for m := fromMs; m < toMs; m += minute {
		if isRTHMinute(time.UnixMilli(m)) && t.isOpen(time.UnixMilli(m)) {
			n++
		}
	}
	return n
}

// isRTHMinute — the 1m bar opening at m belongs to a 1H RTH key-level candle
// (kernel/mentor keyLevel1HBars: anchored 08:30 CT, ends 15:00 CT).
func isRTHMinute(m time.Time) bool {
	in := m.In(kernel.CTLocation())
	if wd := in.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	tod := in.Hour()*60 + in.Minute()
	return tod >= 8*60+30 && tod < 15*60
}

func bootScanLevel(spans []barHoleSpan) string {
	if len(spans) > 0 {
		return "WARN"
	}
	return "INFO"
}

// bootScanLine is the ONE line per (symbol, contract) per process saying what
// the boot scan read and what it found — every value read.
func bootScanLine(symbol, contract string, fromMs, toMs int64, bound string, spans []barHoleSpan) string {
	found := fmt.Sprintf("%d hole(s)", len(spans))
	if len(spans) > 0 {
		parts := make([]string, 0, len(spans))
		for _, sp := range spans {
			parts = append(parts, fmt.Sprintf("%s, %d open min", holeWindowCT(sp.FromMs, sp.ToMs), sp.OpenMissing))
		}
		found += " (" + strings.Join(parts, "; ") + ") — tracked like a live hole"
	}
	return fmt.Sprintf("🕳 boot hole scan: %s %s on %s %s → %s · from %s · %s",
		symbol, barHoleTF, contract, kernel.TableTimeCT(time.UnixMilli(fromMs)), kernel.TableTimeCT(time.UnixMilli(toMs)), bound, found)
}

func scheduleText() string {
	parts := make([]string, 0, len(barHoleSchedule))
	for _, d := range barHoleSchedule {
		parts = append(parts, "+"+strings.TrimSuffix(d.String(), "0s"))
	}
	return strings.Join(parts, "/")
}

func (t *barHoleTracker) sayErrOnce(kind, msg string) {
	if t.saidErr[kind] {
		return
	}
	t.saidErr[kind] = true
	t.say("WARN", msg)
}

// atCapLocked reports whether a NEW hole cannot be tracked: barHoleMaxOpen
// holes being worked on, or the retired list full. A RETIRED hole is listed but
// not worked on, so it does not take one of the working slots.
func (t *barHoleTracker) atCapLocked() bool {
	retired := 0
	for _, h := range t.holes {
		if h.State == holeStateRetired {
			retired++
		}
	}
	return len(t.holes)-retired >= barHoleMaxOpen || retired >= barHoleMaxRetired
}

// retireLocked marks a hole beyond every reach RETIRED: listed, never asked
// again. With the audit OFF nothing would ever close or age it out, so it is
// dropped as before m444 (a retired hole kept forever would also eat the
// tracker's slots).
func (t *barHoleTracker) retireLocked(h *barHole) {
	h.nextAttemptAt, h.measureAt = time.Time{}, time.Time{}
	if t.auditDays <= 0 {
		t.retainRetiredLocked(h) // m445: dropped from the tracker is NOT filled — the data-integrity gate keeps it until it ages out of the window or is measured filled
		t.removeLocked(h)
		return
	}
	h.State = holeStateRetired
}

// placeBeyondReplayLocked decides what a hole older than the standard replay
// becomes: DEEP (the wire can ask for it and it is within
// DeepGapBackfillMaxBarsBack — the hole continues on its schedule) or RETIRED
// (listed, never asked). Returns true when it was retired.
func (t *barHoleTracker) placeBeyondReplayLocked(h *barHole, w holeWire, bb, open int, now time.Time) (retired bool) {
	if _, ok := w.(deepHoleWire); ok {
		if need := t.openMinutesSince(h.FromMs, now, ntwire.DeepGapBackfillMaxBarsBack) + barHoleDeepMargin; need <= ntwire.DeepGapBackfillMaxBarsBack {
			h.deep, h.deepBarsBack = true, need
			t.say("WARN", fmt.Sprintf("🕳 bars hole needs a DEEP re-request: %s %s %s on %s — older than NT8's standard replay (the last %d 1m bars; %d open minute(s) have passed since %s), so its re-requests ask NT8 for the last %d 1m bars instead (%d scheduled, then %d slow; %d open minute(s) missing)",
				h.Symbol, barHoleTF, holeWindowCT(h.FromMs, h.ToMs), h.Contract, bb, open-1, kernel.TableTimeCT(time.UnixMilli(h.FromMs)), need, barHoleDeepScheduled, barHoleDeepSlowCap, h.Missing))
			return false
		}
	}
	telemetry.IncBarsHoleRefused()
	t.say("WARN", fmt.Sprintf("🕳 bars hole RETIRED: %s %s %s on %s — older than the bot can ask NT8 for: the standard re-request replays the last %d 1m bars, a deep one at most %d, and more than %d open minute(s) have passed since %s, so this hole stays (%d open minute(s) missing; %d re-request(s) made). Nothing more is asked for it; it stays LISTED in /api/health bars_holes and on the dashboard until the store holds the bars or it leaves the audit window.",
		h.Symbol, barHoleTF, holeWindowCT(h.FromMs, h.ToMs), h.Contract, bb, ntwire.DeepGapBackfillMaxBarsBack, open-1, kernel.TableTimeCT(time.UnixMilli(h.FromMs)), h.Missing, h.Attempts))
	t.retireLocked(h)
	return true
}

// openMinutesSince counts the 1m open minutes in [fromMs, now), stopping once it
// passes limit (the caller only needs to know "more than limit").
func (t *barHoleTracker) openMinutesSince(fromMs int64, now time.Time, limit int) int {
	const minute = int64(60_000)
	end := now.UnixMilli() / minute * minute
	n := 0
	for m := fromMs; m < end && n <= limit; m += minute {
		if t.isOpen(time.UnixMilli(m)) {
			n++
		}
	}
	return n
}

// trackedLocked reports whether an open hole already covers any of sp.
func (t *barHoleTracker) trackedLocked(symbol, contract string, sp barHoleSpan) bool {
	for _, h := range t.holes {
		if h.Symbol == symbol && h.Contract == contract && sp.FromMs < h.ToMs && sp.ToMs > h.FromMs {
			return true
		}
	}
	return false
}

// measureLocked re-reads the hole's window from the STORE. Filled = no stretch
// of ≥ minGap open minutes is still missing inside [from, to); missing = the
// open minutes still missing in such stretches.
func (t *barHoleTracker) measureLocked(h *barHole) (filled bool, missing int, err error) {
	const minute = int64(60_000)
	rows, err := t.st.BarsBetweenOn(h.Symbol, barHoleTF, h.Contract, h.FromMs, h.ToMs)
	if err != nil {
		return false, h.Missing, err
	}
	// The two bars that bounded the hole at detection are its edges.
	times := make([]int64, 0, len(rows)+2)
	times = append(times, h.FromMs-minute)
	for _, r := range rows {
		times = append(times, r.OpenTimeMs)
	}
	times = append(times, h.ToMs)
	spans := findBarHoles(times, t.minGap, t.isOpen)
	for _, sp := range spans {
		missing += sp.OpenMissing
	}
	return len(spans) == 0, missing, nil
}

// fillLocked closes a hole: logged, counted with its attempt number, dropped.
func (t *barHoleTracker) fillLocked(h *barHole, now time.Time) {
	telemetry.IncBarsHoleFilled(h.Attempts)
	how := fmt.Sprintf("after re-request #%d", h.Attempts)
	if h.Attempts == 0 {
		how = "before any re-request (another replay carried it)"
	}
	t.say("INFO", fmt.Sprintf("✅ bars hole FILLED: %s %s %s on %s — %d open minute(s) were missing, the store now holds them, %s (seen %s, filled %s)",
		h.Symbol, barHoleTF, holeWindowCT(h.FromMs, h.ToMs), h.Contract, h.MissingAtDetect, how,
		kernel.ClockCTSeconds(h.DetectedAt), kernel.ClockCTSeconds(now)))
	// N1 (release #23): the refilled window is in the store — the mentor tick
	// re-seeds its history-derived state from it.
	NoteHistoryRefill(h.Symbol, barHoleTF, h.MissingAtDetect, fmt.Sprintf("bars hole FILLED %s %s %s", h.Symbol, barHoleTF, holeWindowCT(h.FromMs, h.ToMs)))
	t.removeLocked(h)
}

func (t *barHoleTracker) removeLocked(h *barHole) {
	out := t.holes[:0]
	for _, x := range t.holes {
		if x != h {
			out = append(out, x)
		}
	}
	t.holes = out
}

// declareUnfilledLocked is the ONE loud line per hole, after the last
// scheduled attempt was measured and the window is still empty.
func (t *barHoleTracker) declareUnfilledLocked(h *barHole, now time.Time) {
	h.State = holeStateUnfilled
	h.nextAttemptAt = now.Add(barHoleSlowEvery)
	telemetry.IncBarsHoleUnfilled()
	t.say("ERROR", fmt.Sprintf("🚨 bars hole UNFILLED after %d re-requests: NinjaTrader has no history for %s (%s %s on %s, %d open minute(s) still missing): on an %s chart right-click → Reload All Historical Data; the bot re-requests on its next attempt (every %s while CME is open, at most %d more), until the hole leaves NT8's replay window — then it is retired and stays%s",
		h.Attempts, holeWindowCT(h.FromMs, h.ToMs), h.Symbol, barHoleTF, h.Contract, h.Missing, h.Symbol,
		strings.TrimSuffix(barHoleSlowEvery.String(), "0s"), barHoleSlowCapTotal, t.overlapNoteLocked()))
}

// beyondReplayLocked reports whether the hole's first missing minute lies
// further back than a re-request can reach: more than barsBack OPEN minutes
// (the one calendar) between it and now — a replay of barsBack 1m bars ends
// before it. The walk is incremental per hole (each minute counted once),
// and stops as soon as the answer is yes.
func (t *barHoleTracker) beyondReplayLocked(h *barHole, barsBack int, now time.Time) (bool, int) {
	const minute = int64(60_000)
	if h.reachCountedMs == 0 {
		h.reachCountedMs = h.FromMs
	}
	end := now.UnixMilli() / minute * minute // the forming minute's open
	for m := h.reachCountedMs; m < end && h.reachOpen <= barsBack; m += minute {
		if t.isOpen(time.UnixMilli(m)) {
			h.reachOpen++
		}
		h.reachCountedMs = m + minute
	}
	return h.reachOpen > barsBack, h.reachOpen
}

// disagreedNowLocked is the cache's disagreeing-replay count for ONE symbol right
// now (0 with no wire / no cache) — the per-hole baseline. Per SYMBOL, not the
// cache-wide total: another symbol's scale story is not this hole's (m438).
func (t *barHoleTracker) disagreedNowLocked(symbol string) int64 {
	w := t.wire()
	if w == nil || w.BarCache() == nil {
		return 0
	}
	return w.BarCache().OverlapDisagreedFor(symbol)
}

// overlapNoteLocked READS the ring's same-minute replay verdicts for the
// unfilled line: a replay that DISAGREED with the live minutes it overlapped
// was kept out of the store by the replay-hold, so the window can be empty for
// a scale reason, not a missing-history one — the line must not send the owner
// to reload history when the 🚨 scale lines are the story.
func (t *barHoleTracker) overlapNoteLocked() string {
	w := t.wire()
	if w == nil || w.BarCache() == nil {
		return " · replay overlap verdicts: n/a (no wire)"
	}
	agreed, disagreed := w.BarCache().OverlapVerdictCounts()
	note := fmt.Sprintf(" · replay overlap verdicts this process: agreed %d, disagreed %d", agreed, disagreed)
	if disagreed > 0 {
		note += " — a DISAGREEING replay is on another price scale and is held out of the store; read the 🚨 scale lines before reloading history"
	}
	return note
}

// reRequestGateWire is the slice of the wire the re-request admission reads.
type reRequestGateWire interface {
	IsConnected() bool
	IsFeedConnected() bool
}

// reRequestGateReason names why a gap re-request may not go out NOW ("" = it
// may): no wire, link down, feed down, or CME closed. It is THE gate every
// automatic re-request shares - the m444 tracker (gateLocked) and the m449
// ingest_loss hook (datatruth_rerequest.go) - so the two cannot drift. The
// other half of the shared admission is barHoleInSendWindow (send timing); the
// per-symbol spacing and the per-day budget live in the wire itself.
func reRequestGateReason(w reRequestGateWire, isOpen func(time.Time) bool, now time.Time) string {
	switch {
	case w == nil:
		return "no NT8 wire"
	case !w.IsConnected():
		return "NT8 link down"
	case !w.IsFeedConnected():
		return "NT8 feed down (a BarsRequest now returns bars=0)"
	case isOpen == nil || !isOpen(now):
		return "CME closed"
	}
	return ""
}

// gateLocked names why a re-request may not go out NOW ("" = it may).
func (t *barHoleTracker) gateLocked(w holeWire, now time.Time) string {
	if w == nil {
		return reRequestGateReason(nil, t.isOpen, now)
	}
	return reRequestGateReason(w, t.isOpen, now)
}

// tick runs the schedule at `now` (A28: the caller's clock). Production calls
// it every barHoleTickEvery from run(); tests call it directly. The wire send
// happens OUTSIDE the tracker lock: the persist worker reports to the same
// tracker and must never wait on a socket write.
func (t *barHoleTracker) tick(now time.Time) {
	if t == nil {
		return
	}
	t.remeasureRetired(now) // m445: a retired / untracked hole that NT8 has since filled lets the gate resume
	w, due := t.collectDue(now)
	if len(due) == 0 {
		return
	}
	syms := make([]string, 0, len(due))
	for s := range due {
		syms = append(syms, s)
	}
	sort.Strings(syms)
	sent := make(map[string]error, len(syms))
	deepBack := map[string]int{} // symbol -> bars_back of the DEEP ask (absent = the standard one)
	for _, sym := range syms {
		for _, h := range due[sym] {
			if h.deep && h.deepBarsBack > deepBack[sym] {
				deepBack[sym] = h.deepBarsBack
			}
		}
		// one send per symbol: a deep ask covers the standard window too
		if dw, ok := w.(deepHoleWire); ok && deepBack[sym] > 0 {
			sent[sym] = dw.RequestDeepGapBackfillAt(sym, deepBack[sym], now)
		} else {
			sent[sym] = w.RequestGapBackfillAt(sym, now)
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, sym := range syms {
		hs := due[sym]
		if err := sent[sym]; err != nil {
			for _, h := range hs {
				attempt := h.Attempts + 1
				if h.saidRefused != attempt {
					h.saidRefused = attempt
					telemetry.IncBarsHoleRefused()
					t.say("WARN", fmt.Sprintf("🕳 bars hole re-request #%d REFUSED by the wire for %s %s %s: %v",
						attempt, sym, barHoleTF, holeWindowCT(h.FromMs, h.ToMs), err))
				}
			}
			continue
		}
		t.lastFire[sym] = now
		telemetry.IncBarsHoleRerequest()
		for _, h := range hs {
			h.Attempts++
			h.measureAt = now.Add(barHoleSettle)
			switch {
			case h.State == holeStateUnfilled:
				h.slowCount++
				h.nextAttemptAt = now.Add(barHoleSlowEvery)
				if h.slowCount >= h.slowCap() {
					// the cap, in TOTAL: nothing further is scheduled — the
					// measure after this one says so once (collectDue, step 1)
					h.nextAttemptAt = time.Time{}
				}
			case h.Attempts < h.schedLen():
				// the slot, or — after an outage held this attempt back — the
				// schedule's own spacing from now, so a late start never
				// crowds four asks into the minutes after the feed returns
				step := barHoleSchedule[h.Attempts] - barHoleSchedule[h.Attempts-1]
				h.nextAttemptAt = laterOf(h.DetectedAt.Add(barHoleSchedule[h.Attempts]), now.Add(step))
			default:
				h.nextAttemptAt = time.Time{} // the measure after this one decides
			}
			how := "bars_subscribe resent, the AddOn rebuilds its BarsRequests and NT8 replays its window"
			if h.deep {
				how = fmt.Sprintf("DEEP bars_subscribe for 1m only, bars_back %d (older than the standard replay), the AddOn rebuilds the 1m BarsRequest", deepBack[sym])
			}
			t.say("INFO", fmt.Sprintf("🧯 bars hole re-request #%d SENT for %s %s %s on %s (%d open minute(s) missing) — %s; re-measured from the store at %s",
				h.Attempts, sym, barHoleTF, holeWindowCT(h.FromMs, h.ToMs), h.Contract, h.Missing, how, kernel.ClockCTSeconds(h.measureAt)))
		}
	}
}

// collectDue runs measures, gates and caps under the lock and returns the
// holes due a re-request, by symbol, with the wire to send on.
func (t *barHoleTracker) collectDue(now time.Time) (holeWire, map[string][]*barHole) {
	t.mu.Lock()
	defer t.mu.Unlock()
	w := t.wire()
	due := map[string][]*barHole{}
	for _, h := range append([]*barHole(nil), t.holes...) {
		// 0. a hole on a contract the platform has rolled away from cannot be
		//    re-requested: the subscription serves the front month only
		if w != nil {
			if f, ok := w.CurrentContract(h.Symbol); ok && f.Contract != "" && f.Contract != h.Contract {
				telemetry.IncBarsHoleRefused()
				t.say("WARN", fmt.Sprintf("🕳 bars hole RETIRED: %s %s %s on %s — the subscription now names %s; a re-request replays the front month only, so this window cannot be refilled from NT8 (%d open minute(s) stay missing)",
					h.Symbol, barHoleTF, holeWindowCT(h.FromMs, h.ToMs), h.Contract, f.Contract, h.Missing))
				t.removeLocked(h)
				continue
			}
			// 0b. a hole whose first minute is outside NT8's standard replay window
			//     cannot be refilled by the standard re-request (#23 review P2-3).
			//     m444 review round 2: it is not given up on — a DEEP 1m re-request
			//     reaches DeepGapBackfillMaxBarsBack bars back. Only a hole beyond
			//     THAT (or on a wire with no deep path) is retired.
			if h.State == holeStateRetired {
				continue // beyond every reach: listed, never scheduled again (m444)
			}
			if bb := w.BarsBackAsked(); bb > 0 && !h.deep {
				if beyond, open := t.beyondReplayLocked(h, bb, now); beyond && t.placeBeyondReplayLocked(h, w, bb, open, now) {
					continue
				}
			}
		}
		// 1. the measure that follows an attempt
		if !h.measureAt.IsZero() && !now.Before(h.measureAt) {
			h.measureAt = time.Time{}
			filled, missing, err := t.measureLocked(h)
			if err != nil {
				t.sayErrOnce("measure", fmt.Sprintf("🕳 bar-hole re-measure failed for %s %s (%v) — the hole stays open and is measured again before its next attempt", h.Symbol, holeWindowCT(h.FromMs, h.ToMs), err))
			}
			if filled {
				t.fillLocked(h, now)
				continue
			}
			h.Missing = missing
			if h.State == holeStateRefilling && h.Attempts >= h.schedLen() {
				t.declareUnfilledLocked(h, now)
			}
			// the slow cap is IN TOTAL (#23 review P2-3) — checked here, before
			// any later store read: a spent hole is never RE-REQUESTED again
			// (its nextAttemptAt is zero) and stays listed until it leaves the
			// replay window (step 0b) or the process ends. m445: the data-
			// integrity gate still RE-MEASURES it from the store, at most once a
			// minute and with no wire send (remeasureRetired), so a history
			// reload that refills it lifts the pause.
			if h.State == holeStateUnfilled && h.slowCount >= h.slowCap() && !h.saidSlowCap {
				h.saidSlowCap = true
				h.nextAttemptAt = time.Time{}
				telemetry.IncBarsHoleRefused()
				t.say("WARN", fmt.Sprintf("🕳 bars hole slow retries spent: %s %s %s on %s still has %d open minute(s) missing after %d re-requests (cap %d slow re-requests per hole, in total) — nothing more is asked; %s",
					h.Symbol, barHoleTF, holeWindowCT(h.FromMs, h.ToMs), h.Contract, h.Missing, h.Attempts, h.slowCap(), h.spentFix()))
			}
		}
		if h.nextAttemptAt.IsZero() || now.Before(h.nextAttemptAt) || !h.measureAt.IsZero() {
			continue
		}
		attempt := h.Attempts + 1
		// 2. never while the link/feed is down or CME is closed
		if reason := t.gateLocked(w, now); reason != "" {
			if h.saidDeferred != attempt {
				h.saidDeferred = attempt
				telemetry.IncBarsHoleDeferred()
				t.say("WARN", fmt.Sprintf("🕳 bars hole re-request #%d DEFERRED for %s %s %s: %s — it goes out once that clears",
					attempt, h.Symbol, barHoleTF, holeWindowCT(h.FromMs, h.ToMs), reason))
			}
			continue
		}
		// 3. only in the first barHoleSendWindow of a CT minute (#23 review
		//    P2-1): the hole stays due and the next tick in the window sends.
		//    Not a refusal — a wait of at most one minute — so not counted.
		if !barHoleInSendWindow(now) {
			continue
		}
		// 4. one send per symbol per minute (holes on one symbol share it)
		if last, ok := t.lastFire[h.Symbol]; ok && now.Sub(last) < barHoleSpacing {
			continue
		}
		// 4b. a DEEP ask is sized to the hole NOW (the minutes keep passing); a
		//     hole that has slid beyond the deepest reach is retired
		if h.deep {
			need := t.openMinutesSince(h.FromMs, now, ntwire.DeepGapBackfillMaxBarsBack) + barHoleDeepMargin
			if need > ntwire.DeepGapBackfillMaxBarsBack {
				telemetry.IncBarsHoleRefused()
				t.say("WARN", fmt.Sprintf("🕳 bars hole RETIRED: %s %s %s on %s — it has slid beyond the deepest re-request (%d 1m bars) after %d re-request(s); %d open minute(s) stay missing. Nothing more is asked for it; it stays LISTED until the store holds the bars or it leaves the audit window.",
					h.Symbol, barHoleTF, holeWindowCT(h.FromMs, h.ToMs), h.Contract, ntwire.DeepGapBackfillMaxBarsBack, h.Attempts, h.Missing))
				t.retireLocked(h)
				continue
			}
			h.deepBarsBack = need
		}
		// 5. measure before asking — another replay may have carried it (the
		//    store read comes AFTER every cheap check: #23 review P3-5)
		if filled, missing, err := t.measureLocked(h); err == nil {
			if filled {
				t.fillLocked(h, now)
				continue
			}
			h.Missing = missing
		}
		due[h.Symbol] = append(due[h.Symbol], h)
	}
	return w, due
}

func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// run is the production schedule: one tick every barHoleTickEvery, forever.
func (t *barHoleTracker) run() {
	tk := time.NewTicker(barHoleTickEvery)
	defer tk.Stop()
	for range tk.C {
		t.tick(t.clock())
	}
}

// BarHoleView is one open hole as /api/health serves it — every field READ.
type BarHoleView struct {
	Symbol        string  `json:"symbol"`
	TF            string  `json:"tf"`
	Contract      string  `json:"contract"`
	From          string  `json:"from"` // first missing minute, RFC3339 CT
	To            string  `json:"to"`   // the bar that closed it (exclusive), RFC3339 CT
	MissingMin    int     `json:"missing_min"`
	Attempts      int     `json:"attempts"`
	State         string  `json:"state"`  // refilling | unfilled | retired (m444: beyond NT8's replay window — listed, never re-asked)
	Source        string  `json:"source"` // live | audit (m444: found by the persistent gap audit)
	Window        string  `json:"window"` // "10-26 17:00–21:57 CT" (holeWindowCT) — the dashboard banner's text
	DetectedAt    string  `json:"detected_at"`
	NextAttemptAt *string `json:"next_attempt_at,omitempty"` // absent = waiting on the measure of the last attempt, or the slow retries are spent
}

func (t *barHoleTracker) snapshot() []BarHoleView {
	t.mu.Lock()
	defer t.mu.Unlock()
	loc := kernel.CTLocation()
	out := make([]BarHoleView, 0, len(t.holes))
	for _, h := range t.holes {
		v := BarHoleView{
			Symbol: h.Symbol, TF: barHoleTF, Contract: h.Contract,
			From:       time.UnixMilli(h.FromMs).In(loc).Format(time.RFC3339),
			To:         time.UnixMilli(h.ToMs).In(loc).Format(time.RFC3339),
			MissingMin: h.Missing, Attempts: h.Attempts, State: h.State,
			Source: h.Source, Window: holeWindowCT(h.FromMs, h.ToMs),
			DetectedAt: h.DetectedAt.In(loc).Format(time.RFC3339),
		}
		if !h.nextAttemptAt.IsZero() {
			s := h.nextAttemptAt.In(loc).Format(time.RFC3339)
			v.NextAttemptAt = &s
		}
		out = append(out, v)
	}
	return out
}

// BarHolesSnapshot is the health surface's read: the open holes ([] when none)
// and whether the tracker is wired at all (false → the field is ABSENT, A24).
func BarHolesSnapshot() ([]BarHoleView, bool) {
	t := barHoles.Load()
	if t == nil {
		return nil, false
	}
	return t.snapshot(), true
}

// SetBarHolesWiredForTest installs an EMPTY tracker (wired, no hole open) and
// returns the restore — so a handler test can tell [] from absent. TEST-ONLY;
// mirrors SetTCPServerForTest.
func SetBarHolesWiredForTest() (restore func()) {
	prev := barHoles.Swap(newBarHoleTracker(nil, func() *ntwire.TCPServer { return nil }))
	return func() { barHoles.Store(prev) }
}

// TestHole is one unfilled hole for SetBarHolesWithHolesForTest ([FromMs, ToMs)).
type TestHole struct {
	Symbol, Contract string
	FromMs, ToMs     int64
}

// SetBarHolesWithHolesForTest installs a tracker that has judged every symbol
// named in `holes` over [judgedFromMs, judgedToMs] and holds those holes as
// retired ones (nothing re-requests them), so a HANDLER test can see a scoped
// pause without a live wire. TEST-ONLY.
func SetBarHolesWithHolesForTest(judgedFromMs, judgedToMs int64, holes []TestHole) (restore func()) {
	t := newBarHoleTracker(nil, func() *ntwire.TCPServer { return nil })
	for _, h := range holes {
		k := holeKey(h.Symbol, h.Contract)
		t.checked[k] = judgedToMs
		t.coveredFrom[k] = judgedFromMs
		if h.ToMs <= h.FromMs {
			continue // judged only: the symbol is known to the detector, no hole
		}
		n := int((h.ToMs - h.FromMs) / 60_000)
		t.retired = append(t.retired, &barHole{Symbol: h.Symbol, Contract: h.Contract, FromMs: h.FromMs, ToMs: h.ToMs,
			MissingAtDetect: n, Missing: n, State: "retired"})
	}
	prev := barHoles.Swap(t)
	return func() { barHoles.Store(prev) }
}

// ── UNFILLABLE HOLE -> the owner (release #52, m432) ────────────────────────
//
// 2026-10-26: NT8 was linked and its Tradovate data connection came back at
// 03:40 CT, but the 640-minute hole (Sun 17:00 -> Mon 03:40) never refilled:
// NT8 itself had no history for that span. The tracker declared it UNFILLED
// after the fourth re-request and said so — in the LOG. The only fix is the
// owner's, in NinjaTrader (an MNQ 1-minute chart -> right-click -> Reload All
// Historical Data), and nothing told him. UnfilledHolesFeeding is the pull
// side: the trader's 60 s monitor beat reads it and raises ONE P1 alert (+ owner
// push) per hole. The tracker keeps re-requesting on its own schedule — but only
// barHoleSlowCapTotal slow re-requests IN TOTAL (about 2 hours after the hole is
// declared unfilled), so a reload heals the hole by itself only inside that span;
// after it, a reload needs a bot restart. SlowSpent tells the trader side which
// side of that line it is on (m438: the owner message used to promise a pick-up
// that no longer happened).

// liveBarFreshness — how recent the newest accepted LIVE 1m frame must be for
// the feed to count as "feeding live bars" (two bar intervals plus the usual
// delivery grace: a feed that is alive delivers a forming-bar update every few
// seconds; a minute-old one has stalled).
const liveBarFreshness = 3 * time.Minute

// UnfilledHole is one hole that stayed empty after the re-request budget while
// the data provider is demonstrably up. Every field is READ.
type UnfilledHole struct {
	Symbol, Contract string
	FromMs, ToMs     int64
	Missing          int    // open minutes still missing at the last measure
	Attempts         int    // re-requests made
	Window           string // "10-25 17:00–10-26 03:40 CT" (holeWindowCT)
	ReplayDisagreed  int    // disagreeing-replay verdicts SINCE THIS HOLE WAS DETECTED (m438: per hole, not the process lifetime): >0 = a scale story, not a missing-history one
	// SlowSpent — the slow re-requests are used up (barHoleSlowCapTotal) and the
	// hole is still open: the tracker asks NT8 for nothing more, so a history
	// reload is only picked up after a bot restart. ReaskEvery / ReaskHorizon are
	// the slow cadence and the span it covers after the hole is declared unfilled
	// (READ off the constants, so the owner message cannot drift from them).
	SlowSpent bool
	// OutsideReplay (m444) — the hole is RETIRED: its first missing minute is
	// beyond NT8's replay window, so the bot cannot re-ask for it at all and a
	// bot restart does not help. Audit-found old holes arrive in this state.
	OutsideReplay bool
	// Deep (m444 review round 2) — the hole is older than the standard replay,
	// so the bot's re-requests were DEEP ones (1m only, up to
	// DeepGapBackfillMaxBarsBack bars). A bot restart does NOT reach such a hole:
	// the owner's lever after a reload is the deep ask, not a restart.
	Deep         bool
	ReaskEvery   time.Duration
	ReaskHorizon time.Duration
}

// UnfilledHolesFeeding returns the tracked holes on `symbol` that are
// UNFILLED (budget spent) while NT8 is linked AND feeding live bars. Nil when
// the tracker is not wired.
func UnfilledHolesFeeding(symbol string, now time.Time) []UnfilledHole {
	return barHoles.Load().unfilledFeeding(symbol, now)
}

func (t *barHoleTracker) unfilledFeeding(symbol string, now time.Time) []UnfilledHole {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	w := t.wire()
	if w == nil || !w.IsConnected() || !w.IsFeedConnected() || !t.isOpen(now) {
		return nil
	}
	cache := w.BarCache()
	if cache == nil {
		return nil
	}
	recv, ok := cache.LastLiveReceipt(symbol, barHoleTF)
	rc := t.receiptClock
	if rc == nil {
		rc = time.Now
	}
	if !ok || rc().Sub(recv) > liveBarFreshness {
		return nil // NT8 is not delivering live bars: that is FEED DOWN's story, not a missing-history one
	}
	var out []UnfilledHole
	for _, h := range t.holes {
		if (h.State != holeStateUnfilled && h.State != holeStateRetired) || !strings.EqualFold(h.Symbol, symbol) {
			continue
		}
		out = append(out, UnfilledHole{
			Symbol: h.Symbol, Contract: h.Contract, FromMs: h.FromMs, ToMs: h.ToMs,
			Missing: h.Missing, Attempts: h.Attempts,
			Window:          holeWindowCT(h.FromMs, h.ToMs),
			ReplayDisagreed: int(max(cache.OverlapDisagreedFor(h.Symbol)-h.disagreedAtDetect, 0)),
			SlowSpent:       h.saidSlowCap,
			OutsideReplay:   h.State == holeStateRetired,
			Deep:            h.deep,
			ReaskEvery:      barHoleSlowEvery,
			ReaskHorizon:    barHoleSlowEvery * time.Duration(h.slowCap()),
		})
	}
	return out
}
