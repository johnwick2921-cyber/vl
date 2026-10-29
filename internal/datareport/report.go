// Package datareport is the NIGHTLY DATA REPORT (release #54, m448): once per
// CME session day, after the 16:00 CT close, ONE short message to the owner's
// Telegram saying what the bot's data and books looked like that session.
//
// Why it exists. Owner principle 2026-10-29: "this is a financial product, not
// a game - nothing related to data loss or incorrect data can continue to
// trade". The gates stop trading on bad data; this is the other half: a daily
// receipt the owner can read in thirty seconds, so a quiet loss (a hole that
// never filled, a final frame dropped, a clock that stepped, a backup that
// would not restore) is a line on his phone the same evening instead of a
// forensic finding days later.
//
// Law for every number in it:
//
//   - READ, never literal. A figure comes from the store or from a counter the
//     system already keeps; nothing is computed from an assumption.
//   - n/a when unknown. A source that is not wired, a query that failed, a
//     counter that belongs to another session day: the line says "n/a", it
//     never prints a zero it did not measure (canon: no fabricated values).
//   - Counters record, never infer. Since-boot counters are labelled "since
//     boot <time>" because that is exactly what they are.
//
// It is advisory and outbound-only: it reads, it sends through the owner-push
// public API (internal/ownerpush: friend instances refused, scrubbed, rate
// limited, never blocks), and it changes nothing about trading.
package datareport

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"vl/kernel"
	"vl/store"
)

// Num is a counter reading: OK=false means "not measured" and prints n/a.
type Num struct {
	V  int64
	OK bool
}

// N builds a measured reading.
func N(v int64) Num { return Num{V: v, OK: true} }

func (n Num) String() string {
	if !n.OK {
		return "n/a"
	}
	return fmt.Sprintf("%d", n.V)
}

// HoleCounts are the bar-hole tracker's since-boot counters.
type HoleCounts struct {
	Detected, Filled, Unfilled int64
}

// Names of the optional counters other lanes add. A name that nobody has
// registered prints n/a. The integrator registers them (see Register).
const (
	CounterBadPrints        = "bad_prints"
	CounterCrossFrame       = "cross_frame_mismatches"
	CounterPausedMinutes    = "paused_minutes"
	restoreResultSuffix     = ".result"
	restoreOverdueAfter     = 8 * 24 * time.Hour
	refusalMinRepeats       = 3 // "repeated > 3"
	refusalTopN             = 5
	defaultStoredLookback   = 7 * 24 * time.Hour
	nonRefusalGateClockSkew = "clock_skew_observed"
)

var (
	regMu    sync.RWMutex
	registry = map[string]func() (int64, bool){}
)

// Register installs a named counter reader (CounterBadPrints, CounterCrossFrame,
// CounterPausedMinutes). fn returns (value, true) when measured. Registering a
// nil fn removes the name. Safe from any goroutine.
func Register(name string, fn func() (int64, bool)) {
	regMu.Lock()
	defer regMu.Unlock()
	if fn == nil {
		delete(registry, name)
		return
	}
	registry[name] = fn
}

func lookup(name string) Num {
	regMu.RLock()
	fn := registry[name]
	regMu.RUnlock()
	if fn == nil {
		return Num{}
	}
	if v, ok := fn(); ok {
		return N(v)
	}
	return Num{}
}

// Sources is everything the report reads. Every func field is optional: nil
// means "not wired", which the report prints as n/a.
type Sources struct {
	DB *gorm.DB

	// Expected reports whether the CME trades the 1m bar opening at m. Nil =
	// DefaultExpected.
	Expected func(m time.Time) bool
	// FeedDownAfter is the FEED DOWN threshold (the ONE feed policy). Nil = the
	// kernel feed policy's flat threshold.
	FeedDownAfter func() time.Duration
	// BootAt is when this process started (since-boot counters are labelled
	// with it). Zero = unknown.
	BootAt time.Time

	HoleCounts  func() (HoleCounts, bool)
	IngestDrops func() (finalDropped, historicalDropped int64, ok bool)
	ClockSteps  func() (back, fwd int64, ok bool)
	// Gates is the per-trader gate-block table and the session day (ms of the
	// 17:00 CT start) it belongs to.
	Gates func() (sessionDayMs int64, table map[string]map[string]int, ok bool)
	// Counter resolves a named counter. Nil = the package registry.
	Counter func(name string) Num

	// RestoreDir holds the weekly backup-restore-test result files. "" = not
	// configured (n/a).
	RestoreDir string
}

// Session is one CME session day.
type Session struct {
	Key        string    // YYYY-MM-DD of the 17:00 CT start (kernel.CMESessionDayKey)
	Start, End time.Time // End is the 16:00 CT close, or "now" when earlier
}

// SessionFor returns the session whose 16:00 CT close is the most recent one at
// or before now: the session the nightly report is about. On a weekend this is
// the Friday session again (the scheduler's marker keeps it from re-sending).
func SessionFor(now time.Time) Session {
	ct := now.In(kernel.CTLocation())
	close := time.Date(ct.Year(), ct.Month(), ct.Day(), 16, 0, 0, 0, kernel.CTLocation())
	if ct.Before(close) {
		close = time.Date(ct.Year(), ct.Month(), ct.Day()-1, 16, 0, 0, 0, kernel.CTLocation())
	}
	start := time.Date(close.Year(), close.Month(), close.Day()-1, 17, 0, 0, 0, kernel.CTLocation())
	end := close
	if now.Before(end) {
		end = now
	}
	return Session{Key: kernel.CMESessionDayKey(start.Add(time.Hour)), Start: start, End: end}
}

// DefaultExpected is the bar-hole tracker's calendar, read the way its rows are
// keyed: a minute is expected only when BOTH the instant (kernel.IsCMEOpen) and
// the trading-date row of the session calendar say it trades. It mirrors
// trader/ninjatrader barHoleExpected (that one is unexported); the Good Friday
// pin in report_test.go is what keeps the two from drifting apart.
func DefaultExpected(m time.Time) bool {
	if !kernel.IsCMEOpen(m) {
		return false
	}
	day, err := time.ParseInLocation("2006-01-02", kernel.CMETradingDateKey(m), kernel.CTLocation())
	if err != nil {
		return true // the instant said open; an unreadable key never invents a closure
	}
	return kernel.SessionStateAt(day.Add(12*time.Hour)).Class != kernel.SessionClosed
}

// SymbolRow is one symbol's 1m bar accounting for the session.
type SymbolRow struct {
	Symbol   string
	Expected int // minutes the calendar says trade
	Stored   int // of those, minutes with a stored bar
	Runs     int // runs of consecutive missing expected minutes
	Longest  int // minutes in the longest run
	// FeedDownMin is the minutes inside runs LONGER than the FEED DOWN threshold
	// (a run that long is, by the bot's own definition, a feed outage).
	FeedDownMin int
	Err         string
}

// Missing is the expected minutes with no stored bar.
func (r SymbolRow) Missing() int { return r.Expected - r.Stored }

// SetupAgg is one setup's closed trades.
type SetupAgg struct {
	N   int
	Pnl float64 // resolved rows only
}

// TraderRow is one trader's closed trades for the session.
type TraderRow struct {
	Trader     string
	N          int
	Pnl        float64 // pnl_corrected of resolved rows only
	Unresolved int     // closed rows with no verified P&L: excluded from Pnl, counted here
	Setups     map[string]*SetupAgg
}

// Refusal is one refusal class repeated more than refusalMinRepeats times.
type Refusal struct {
	Gate string
	N    int
}

// RestoreResult is the last weekly backup-restore-test outcome.
type RestoreResult struct {
	Have    bool
	Status  string // OK | FAIL
	At      time.Time
	Src     string
	Overdue bool
	Raw     string
}

// Report is everything the message says.
type Report struct {
	Session Session
	BootAt  time.Time

	Symbols    []SymbolRow
	SymbolsErr string

	FeedDownAlerts Num
	Holes          *HoleCounts // nil = n/a

	BadPrints, CrossFrame, PausedMin Num
	IngestFinal, IngestHist          Num
	ClockBack, ClockFwd              Num

	Trades    []TraderRow
	TradesErr string

	Refusals   []Refusal
	RefusalsOK bool // false = the gate table was not readable for this session day

	Restore RestoreResult
}

// Collect reads every source for the session. It never returns an error: a
// failing source becomes an n/a line of its own and the rest of the report
// still goes out.
func Collect(src Sources, sess Session) Report {
	r := Report{Session: sess, BootAt: src.BootAt}
	expected := src.Expected
	if expected == nil {
		expected = DefaultExpected
	}
	feedAfter := src.FeedDownAfter
	if feedAfter == nil {
		feedAfter = func() time.Duration {
			return time.Duration(kernel.LoadFeedPolicy().FlatAlertMs) * time.Millisecond
		}
	}

	if src.DB == nil {
		r.SymbolsErr, r.TradesErr = "no database", "no database"
	} else {
		r.Symbols, r.SymbolsErr = collectSymbols(src.DB, sess, expected, feedAfter())
		r.FeedDownAlerts = feedDownAlerts(src.DB, sess)
		r.Trades, r.TradesErr = collectTrades(src.DB, sess)
	}

	if src.HoleCounts != nil {
		if hc, ok := src.HoleCounts(); ok {
			r.Holes = &hc
		}
	}
	counter := src.Counter
	if counter == nil {
		counter = lookup
	}
	r.BadPrints = counter(CounterBadPrints)
	r.CrossFrame = counter(CounterCrossFrame)
	r.PausedMin = counter(CounterPausedMinutes)
	if src.IngestDrops != nil {
		if f, h, ok := src.IngestDrops(); ok {
			r.IngestFinal, r.IngestHist = N(f), N(h)
		}
	}
	if src.ClockSteps != nil {
		if b, f, ok := src.ClockSteps(); ok {
			r.ClockBack, r.ClockFwd = N(b), N(f)
		}
	}
	if src.Gates != nil {
		if dayMs, table, ok := src.Gates(); ok && dayMs == sess.Start.UnixMilli() {
			r.Refusals, r.RefusalsOK = topRefusals(table), true
		}
	}
	if src.RestoreDir != "" {
		r.Restore = ReadRestoreResult(src.RestoreDir, sess.End)
	}
	return r
}

// collectSymbols accounts the 1m bars of every symbol that has traded in the
// week before the session ended.
func collectSymbols(db *gorm.DB, sess Session, expected func(time.Time) bool, feedAfter time.Duration) ([]SymbolRow, string) {
	var syms []string
	if err := db.Raw(`SELECT DISTINCT symbol FROM bars WHERE tf = '1m' AND open_time_ms >= ? AND open_time_ms < ? ORDER BY symbol`,
		sess.End.Add(-defaultStoredLookback).UnixMilli(), sess.End.UnixMilli()).Scan(&syms).Error; err != nil {
		return nil, "symbol list unreadable: " + err.Error()
	}
	var out []SymbolRow
	for _, sym := range syms {
		out = append(out, accountBars(db, sym, sess, expected, feedAfter))
	}
	return out, ""
}

// accountBars walks every expected minute of the session once.
func accountBars(db *gorm.DB, sym string, sess Session, expected func(time.Time) bool, feedAfter time.Duration) SymbolRow {
	row := SymbolRow{Symbol: sym}
	var opens []int64
	// The same exclusions BarsBetween makes: a mixed / off-scale row is not a
	// bar anyone may use, so it does not count as stored.
	if err := db.Raw(`SELECT DISTINCT open_time_ms FROM bars
		WHERE symbol = ? AND tf = '1m' AND open_time_ms >= ? AND open_time_ms < ?
		AND COALESCE(source, '') NOT IN (?, ?) AND COALESCE(contract, '') <> ?`,
		sym, sess.Start.UnixMilli(), sess.End.UnixMilli(),
		store.BarSourceMixed, store.BarSourceOffScale, store.ContractMixed).Scan(&opens).Error; err != nil {
		row.Err = err.Error()
		return row
	}
	have := make(map[int64]bool, len(opens))
	for _, o := range opens {
		have[o] = true
	}
	thresholdMin := int(feedAfter / time.Minute)
	run := 0
	closeRun := func() {
		if run == 0 {
			return
		}
		row.Runs++
		if run > row.Longest {
			row.Longest = run
		}
		if run > thresholdMin {
			row.FeedDownMin += run
		}
		run = 0
	}
	const minute = int64(60_000)
	for ms := sess.Start.UnixMilli(); ms+minute <= sess.End.UnixMilli(); ms += minute {
		if !expected(time.UnixMilli(ms)) {
			closeRun()
			continue
		}
		row.Expected++
		if have[ms] {
			row.Stored++
			closeRun()
		} else {
			run++
		}
	}
	closeRun()
	return row
}

// feedDownAlerts counts the FEED DOWN episodes the alert center raised in the
// session (distinct event ids: every trader on the box raises its own row for
// the same episode). created_at is Unix seconds.
func feedDownAlerts(db *gorm.DB, sess Session) Num {
	var n int64
	if err := db.Raw(`SELECT COUNT(DISTINCT event_id) FROM day_plan_alerts WHERE kind = 'feed-down' AND created_at >= ? AND created_at < ?`,
		sess.Start.Unix(), sess.End.Unix()).Scan(&n).Error; err != nil {
		return Num{}
	}
	return N(n)
}

// setupOf names the setup of a closed trade: the head of the scenario id it
// cited ("swing-1793...", "isb-2-1793..." -> "swing", "isb"). No citation = n/a,
// never a guess.
func setupOf(scenarioID string) string {
	s := strings.TrimSpace(scenarioID)
	if s == "" {
		return "n/a"
	}
	if i := strings.IndexByte(s, '-'); i > 0 {
		s = s[:i]
	}
	if len(s) > 12 {
		s = s[:12]
	}
	return s
}

type tradeRaw struct {
	TraderID    string
	Name        string
	Scenario    string
	CloseReason string
	PnlCorrect  *float64
}

// collectTrades reads the session's CLOSED bot trades. P&L is the corrected
// column only: a NULL (or an unknown-P&L close reason) is UNRESOLVED, excluded
// from every sum, and its COUNT is shown (corrected-column law).
func collectTrades(db *gorm.DB, sess Session) ([]TraderRow, string) {
	var raws []tradeRaw
	if err := db.Raw(`SELECT p.trader_id AS trader_id, COALESCE(t.name, '') AS name,
			COALESCE(p.cited_scenario_id, '') AS scenario, COALESCE(p.close_reason, '') AS close_reason,
			p.pnl_corrected AS pnl_correct
		FROM trader_positions p LEFT JOIN traders t ON t.id = p.trader_id
		WHERE p.status = 'CLOSED' AND p.exit_time >= ? AND p.exit_time < ?
		AND `+store.BotOriginSQL+` AND COALESCE(p.close_reason, '') <> ?
		ORDER BY p.exit_time`,
		sess.Start.UnixMilli(), sess.End.UnixMilli(), store.CloseReasonTestSeam).Scan(&raws).Error; err != nil {
		return nil, "trades unreadable: " + err.Error()
	}
	byTrader := map[string]*TraderRow{}
	var order []string
	for _, x := range raws {
		name := strings.TrimSpace(x.Name)
		if name == "" {
			name = x.TraderID
		}
		tr := byTrader[x.TraderID]
		if tr == nil {
			tr = &TraderRow{Trader: name, Setups: map[string]*SetupAgg{}}
			byTrader[x.TraderID] = tr
			order = append(order, x.TraderID)
		}
		tr.N++
		sa := tr.Setups[setupOf(x.Scenario)]
		if sa == nil {
			sa = &SetupAgg{}
			tr.Setups[setupOf(x.Scenario)] = sa
		}
		sa.N++
		if x.PnlCorrect == nil || store.UnknownPnLReason(x.CloseReason) {
			tr.Unresolved++
			continue
		}
		tr.Pnl += *x.PnlCorrect
		sa.Pnl += *x.PnlCorrect
	}
	out := make([]TraderRow, 0, len(order))
	for _, id := range order {
		out = append(out, *byTrader[id])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Trader < out[j].Trader })
	return out, ""
}

// topRefusals sums each gate across traders and keeps the classes that fired
// more than refusalMinRepeats times, most frequent first, at most refusalTopN.
// clock_skew_observed is an OBSERVATION the table also carries (it never
// blocked anything: telemetry/gate_blocks.go says so), so it is not a refusal.
func topRefusals(table map[string]map[string]int) []Refusal {
	sum := map[string]int{}
	for _, gates := range table {
		for g, n := range gates {
			if g == nonRefusalGateClockSkew {
				continue
			}
			sum[g] += n
		}
	}
	var out []Refusal
	for g, n := range sum {
		if n > refusalMinRepeats {
			out = append(out, Refusal{Gate: g, N: n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].N != out[j].N {
			return out[i].N > out[j].N
		}
		return out[i].Gate < out[j].Gate
	})
	if len(out) > refusalTopN {
		out = out[:refusalTopN]
	}
	return out
}
