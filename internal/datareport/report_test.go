package datareport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"vl/internal/ownerpush"
	"vl/kernel"
	"vl/store"
)

// ct builds a Chicago wall-clock instant.
func ct(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, kernel.CTLocation())
}

// A normal Wed session: Tue 2026-10-27 17:00 CT -> Wed 10-28 16:00 CT.
var (
	normalNow = ct(2026, 10, 28, 16, 10)
	feed10m   = func() time.Duration { return 10 * time.Minute }
)

func newDB(t *testing.T) (*store.Store, *gorm.DB) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.BarHistory().Migrate(); err != nil {
		t.Fatalf("bars migrate: %v", err)
	}
	return st, st.GormDB()
}

// putBars stores a live 1m bar for every minute of sess the calendar expects,
// except the minutes skip(i) names (i = index among the expected minutes).
func putBars(t *testing.T, st *store.Store, sym string, sess Session, skip func(i int) bool) (expected int) {
	t.Helper()
	var rows []store.BarHistoryDB
	i := 0
	for ms := sess.Start.UnixMilli(); ms+60_000 <= sess.End.UnixMilli(); ms += 60_000 {
		if !DefaultExpected(time.UnixMilli(ms)) {
			continue
		}
		expected++
		if !skip(i) {
			rows = append(rows, store.BarHistoryDB{Symbol: sym, TF: "1m", OpenTimeMs: ms, O: 1, H: 2, L: 1, C: 2, V: 1,
				Contract: "MNQ 12-26", Source: store.BarSourceLive})
		}
		i++
	}
	if err := st.BarHistory().InsertBars(rows); err != nil {
		t.Fatalf("InsertBars: %v", err)
	}
	return expected
}

func TestSessionForIsThe16ClosePrecedingNow(t *testing.T) {
	s := SessionFor(normalNow)
	if got, want := s.Start, ct(2026, 10, 27, 17, 0); !got.Equal(want) {
		t.Fatalf("start %v want %v", got, want)
	}
	if got, want := s.End, ct(2026, 10, 28, 16, 0); !got.Equal(want) {
		t.Fatalf("end %v want %v (the 16:00 close, not now)", got, want)
	}
	if s.Key != "2026-10-27" {
		t.Fatalf("key %q want the 17:00 start date 2026-10-27", s.Key)
	}
	// Monday's close belongs to the SUNDAY 17:00 session, not Friday's.
	m := SessionFor(ct(2026, 10, 26, 16, 10))
	if !m.Start.Equal(ct(2026, 10, 25, 17, 0)) || m.Key != "2026-10-25" {
		t.Fatalf("Monday session = %v key %s, want Sun 10-25 17:00", m.Start, m.Key)
	}
	// Before the close: the session so far, ending at now.
	mid := ct(2026, 10, 28, 9, 0)
	e := SessionFor(mid)
	if !e.End.Equal(ct(2026, 10, 27, 16, 0)) {
		t.Fatalf("09:00 still reports the previous closed session; end = %v", e.End)
	}
}

func TestCollectCountsExpectedVsStoredAndTheFeedDownRuns(t *testing.T) {
	st, db := newDB(t)
	sess := SessionFor(normalNow)
	// MNQ: a 15-minute hole (longer than the 10 min FEED DOWN threshold) and a
	// 3-minute one (shorter). ES complete.
	exp := putBars(t, st, "MNQ", sess, func(i int) bool { return (i >= 100 && i < 115) || (i >= 500 && i < 503) })
	putBars(t, st, "ES", sess, func(int) bool { return false })
	if exp != 1380 {
		t.Fatalf("a normal session has 1380 expected minutes, calendar says %d", exp)
	}

	rep := Collect(Sources{DB: db, Expected: DefaultExpected, FeedDownAfter: feed10m}, sess)
	if rep.SymbolsErr != "" || len(rep.Symbols) != 2 {
		t.Fatalf("symbols: %+v err=%q", rep.Symbols, rep.SymbolsErr)
	}
	es, mnq := rep.Symbols[0], rep.Symbols[1]
	if es.Symbol != "ES" || mnq.Symbol != "MNQ" {
		t.Fatalf("order %s %s", es.Symbol, mnq.Symbol)
	}
	if es.Expected != 1380 || es.Stored != 1380 || es.Runs != 0 || es.FeedDownMin != 0 {
		t.Fatalf("ES must be whole: %+v", es)
	}
	if mnq.Expected != 1380 || mnq.Stored != 1380-18 || mnq.Runs != 2 || mnq.Longest != 15 {
		t.Fatalf("MNQ accounting wrong: %+v", mnq)
	}
	// Only the 15-minute run exceeds the threshold: 15 FEED DOWN minutes, not 18.
	if mnq.FeedDownMin != 15 {
		t.Fatalf("FEED DOWN minutes = %d, want 15 (only the run longer than the threshold)", mnq.FeedDownMin)
	}
}

func TestMixedAndOffScaleRowsAreNotStoredBars(t *testing.T) {
	st, db := newDB(t)
	sess := SessionFor(normalNow)
	putBars(t, st, "MNQ", sess, func(i int) bool { return i == 7 })
	// The skipped minute exists only as a MIXED row: no reader may use it, so it
	// must not count as stored.
	ms := sess.Start.UnixMilli()
	i := 0
	for m := ms; ; m += 60_000 {
		if DefaultExpected(time.UnixMilli(m)) {
			if i == 7 {
				if err := db.Exec(`INSERT INTO bars(symbol, tf, open_time_ms, o, h, l, c, v, convention, contract, source) VALUES ('MNQ','1m',?,1,2,1,2,1,'epoch_floor','MNQ 12-26',?)`, m, store.BarSourceMixed).Error; err != nil {
					t.Fatal(err)
				}
				break
			}
			i++
		}
	}
	rep := Collect(Sources{DB: db, Expected: DefaultExpected, FeedDownAfter: feed10m}, sess)
	if m := rep.Symbols[0]; m.Missing() != 1 {
		t.Fatalf("a mixed-source row counted as stored: %+v", m)
	}
}

// GOOD FRIDAY 2026-04-03: the evening session that belongs to a closed trading
// day (Thu 04-02 17:00+) reads open by the instant alone. The hole tracker found
// this and its barHoleExpected carries the fix; this pin keeps DefaultExpected
// (its mirror) from drifting back to the instant-only reading.
func TestDefaultExpectedMirrorsTheHoleTrackerOnGoodFriday(t *testing.T) {
	if DefaultExpected(ct(2026, 4, 2, 20, 0)) {
		t.Fatal("Thu 04-02 20:00 CT belongs to the closed Good Friday trading date: not expected")
	}
	if !DefaultExpected(ct(2026, 4, 1, 20, 0)) {
		t.Fatal("an ordinary Wednesday evening minute must be expected")
	}
	sess := SessionFor(ct(2026, 4, 3, 16, 10))
	st, db := newDB(t)
	_ = st
	rep := Collect(Sources{DB: db, FeedDownAfter: feed10m}, sess)
	if hasSession(rep) {
		t.Fatalf("Good Friday has nothing to report: %+v", rep.Symbols)
	}
}

func insertTrader(t *testing.T, db *gorm.DB, id, name string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO traders(id, user_id, name, ai_model_id, exchange_id, initial_balance) VALUES (?, 'u', ?, 'm', 'e', 1000)`, id, name).Error; err != nil {
		t.Fatalf("insert trader: %v", err)
	}
}

func insertTrade(t *testing.T, db *gorm.DB, trader, scen, reason, origin string, pnl *float64, exit time.Time) {
	t.Helper()
	if err := db.Exec(`INSERT INTO trader_positions(trader_id, symbol, side, status, exit_time, close_reason, cited_scenario_id, origin, pnl_corrected, entry_time, quantity, entry_quantity, entry_price)
		VALUES (?, 'MNQ', 'LONG', 'CLOSED', ?, ?, ?, ?, ?, ?, 1, 1, 100)`,
		trader, exit.UnixMilli(), reason, scen, origin, pnl, exit.Add(-time.Hour).UnixMilli()).Error; err != nil {
		t.Fatalf("insert trade: %v", err)
	}
}

func fp(v float64) *float64 { return &v }

func TestTradesAndPnLByTraderAndSetupUseTheCorrectedColumnOnly(t *testing.T) {
	_, db := newDB(t)
	sess := SessionFor(normalNow)
	in := sess.Start.Add(3 * time.Hour)
	insertTrader(t, db, "t1", "hoang")
	insertTrader(t, db, "t2", "second")
	insertTrade(t, db, "t1", "swing-1793269800000-1", "stop", "bot", fp(150), in)
	insertTrade(t, db, "t1", "swing-1793269800000-2", "stop", "bot", fp(-50), in.Add(time.Minute))
	insertTrade(t, db, "t1", "isb-2-1793256600000-1", "target", "bot", fp(-30), in.Add(2*time.Minute))
	insertTrade(t, db, "t1", "isb-2-1793256600000-2", "sync", "bot", nil, in.Add(3*time.Minute))       // UNRESOLVED: NULL corrected
	insertTrade(t, db, "t1", "isb-2-1793256600000-3", store.CloseReasonUnresolved, "bot", fp(999), in) // unknown-P&L reason: unresolved even with a number
	insertTrade(t, db, "t1", "swing-1", "manual", "manual", fp(5000), in)                              // manual origin: not the bot's trade
	insertTrade(t, db, "t1", "swing-1", store.CloseReasonTestSeam, "bot", fp(7000), in)                // test seam: never a real trade
	insertTrade(t, db, "t1", "swing-1", "stop", "bot", fp(1234), sess.End.Add(time.Hour))              // outside the session
	insertTrade(t, db, "t2", "", "stop", "bot", fp(10), in)                                            // no cited scenario: setup n/a

	rep := Collect(Sources{DB: db, Expected: DefaultExpected, FeedDownAfter: feed10m}, sess)
	if rep.TradesErr != "" || len(rep.Trades) != 2 {
		t.Fatalf("trades: %+v err=%q", rep.Trades, rep.TradesErr)
	}
	h, s := rep.Trades[0], rep.Trades[1]
	if h.Trader != "hoang" || h.N != 5 || h.Pnl != 70 || h.Unresolved != 2 {
		t.Fatalf("hoang: %+v (want 5 closed, +70 = 150-50-30, 2 unresolved excluded from the sum)", h)
	}
	if sw := h.Setups["swing"]; sw == nil || sw.N != 2 || sw.Pnl != 100 {
		t.Fatalf("swing setup: %+v", h.Setups["swing"])
	}
	if is := h.Setups["isb"]; is == nil || is.N != 3 || is.Pnl != -30 {
		t.Fatalf("isb setup: %+v (the NULL and unknown-reason rows count as trades, never as P&L)", h.Setups["isb"])
	}
	if s.Trader != "second" || s.N != 1 || s.Pnl != 10 || s.Setups["n/a"] == nil {
		t.Fatalf("second: %+v", s)
	}
	for _, l := range rep.Lines() {
		if strings.Contains(l, "hoang:") {
			if !strings.Contains(l, "+$70.00") || !strings.Contains(l, "unresolved 2") {
				t.Fatalf("line %q must show +$70.00 and the unresolved count", l)
			}
		}
	}
}

func TestRefusalsRepeatedMoreThanThreeTopFiveNoClockSkew(t *testing.T) {
	table := map[string]map[string]int{
		"a": {"g1": 2, "g2": 3, "g3": 9, "clock_skew_observed": 400, "g4": 1},
		"b": {"g1": 2, "g2": 1, "g5": 5, "g6": 6, "g7": 7, "g8": 8, "g9": 4},
	}
	got := topRefusals(table)
	want := []Refusal{{"g3", 9}, {"g8", 8}, {"g7", 7}, {"g6", 6}, {"g5", 5}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("top refusals = %v, want %v (g1=4 sums across traders but is beaten by the top-5 cut; g2=4; clock_skew_observed is an observation, not a refusal)", got, want)
	}
	// Exactly 3 is NOT "repeated > 3".
	if r := topRefusals(map[string]map[string]int{"a": {"g": 3}}); len(r) != 0 {
		t.Fatalf("3 is not > 3: %v", r)
	}
	if r := topRefusals(map[string]map[string]int{"a": {"g": 2}, "b": {"g": 2}}); len(r) != 1 || r[0].N != 4 {
		t.Fatalf("a gate is summed across traders: %v", r)
	}
}

func TestRefusalsAreNAWhenTheTableIsForAnotherSessionDay(t *testing.T) {
	sess := SessionFor(normalNow)
	mk := func(day int64) Report {
		return Collect(Sources{Gates: func() (int64, map[string]map[string]int, bool) {
			return day, map[string]map[string]int{"a": {"g": 9}}, true
		}}, sess)
	}
	if r := mk(sess.Start.UnixMilli()); !r.RefusalsOK || len(r.Refusals) != 1 {
		t.Fatalf("same day must read: %+v", r.Refusals)
	}
	r := mk(sess.Start.AddDate(0, 0, 1).UnixMilli())
	if r.RefusalsOK || len(r.Refusals) != 0 {
		t.Fatalf("another session day's counters must not be printed as this one's: %+v", r)
	}
	if !strings.Contains(strings.Join(r.Lines(), "\n"), "Refusals: n/a") {
		t.Fatal("the line must say n/a")
	}
}

func TestNothingWiredPrintsNAEverywhereNeverAZero(t *testing.T) {
	sess := SessionFor(normalNow)
	r := Collect(Sources{}, sess)
	text := strings.Join(r.Lines(), "\n")
	for _, must := range []string{
		"1m bars: n/a", "FEED DOWN: alerts n/a", "minutes n/a", "Bar holes: n/a",
		"Bad prints n/a · cross-frame mismatches n/a · paused minutes n/a",
		"final n/a, historical n/a", "n/a back / n/a fwd", "Trades: n/a", "Refusals: n/a", "Backup restore test: n/a",
	} {
		if !strings.Contains(text, must) {
			t.Errorf("unwired report lacks %q:\n%s", must, text)
		}
	}
}

func TestRegisteredCountersFeedTheLineAndUnregisteredStayNA(t *testing.T) {
	defer Register(CounterCrossFrame, nil)
	Register(CounterCrossFrame, func() (int64, bool) { return 4, true })
	Register(CounterPausedMinutes, func() (int64, bool) { return 0, false }) // registered but not measured
	defer Register(CounterPausedMinutes, nil)
	r := Collect(Sources{}, SessionFor(normalNow))
	if !r.CrossFrame.OK || r.CrossFrame.V != 4 {
		t.Fatalf("cross-frame: %+v", r.CrossFrame)
	}
	if r.PausedMin.OK {
		t.Fatalf("an unmeasured counter must stay n/a, got %+v", r.PausedMin)
	}
	if r.BadPrints.OK {
		t.Fatalf("an unregistered counter must stay n/a, got %+v", r.BadPrints)
	}
}

func fullReport(t *testing.T) Report {
	st, db := newDB(t)
	sess := SessionFor(normalNow)
	putBars(t, st, "MNQ", sess, func(i int) bool { return i >= 100 && i < 130 })
	putBars(t, st, "ES", sess, func(int) bool { return false })
	insertTrader(t, db, "t1", "hoang")
	insertTrade(t, db, "t1", "swing-1", "stop", "bot", fp(150), sess.Start.Add(time.Hour))
	insertTrade(t, db, "t1", "isb-2-1", "stop", "bot", fp(-30), sess.Start.Add(2*time.Hour))
	if err := db.Exec(`INSERT INTO day_plan_alerts(trader_id, level, event_id, kind, title, body, created_at) VALUES ('t1','P0','feeddown:x','feed-down','t','b',?)`, sess.Start.Add(2*time.Hour).Unix()).Error; err != nil {
		t.Fatal(err)
	}
	return Collect(Sources{
		DB: db, Expected: DefaultExpected, FeedDownAfter: feed10m,
		BootAt:      sess.Start.Add(-time.Hour),
		HoleCounts:  func() (HoleCounts, bool) { return HoleCounts{3, 2, 1}, true },
		IngestDrops: func() (int64, int64, bool) { return 0, 2, true },
		ClockSteps:  func() (int64, int64, bool) { return 1, 0, true },
		Gates: func() (int64, map[string]map[string]int, bool) {
			return sess.Start.UnixMilli(), map[string]map[string]int{"a": {"mentor_done_after_win": 12, "session_gate": 7}}, true
		},
		Counter: func(n string) Num {
			if n == CounterBadPrints {
				return N(0)
			}
			return Num{}
		},
	}, sess)
}

func TestReportReadsEveryFigureAndFitsTheOwnerPushLimit(t *testing.T) {
	rep := fullReport(t)
	lines := rep.Lines()
	text := strings.Join(lines, "\n")
	for _, must := range []string{
		"MNQ 1m bars 1350/1380 stored — MISSING 30 in 1 run(s), longest 30m",
		"ES 1m bars 1380/1380 stored",
		"FEED DOWN: alerts 1 · minutes ES 0m, MNQ 30m",
		"Bar holes since boot 2026-10-27 16:00 CT: found 3, filled 2, unfilled 1",
		"Bad prints 0 · cross-frame mismatches n/a · paused minutes n/a",
		"final 0, historical 2 · clock steps 1 back / 0 fwd",
		"hoang: 2 closed, +$120.00 (isb 1 -$30.00, swing 1 +$150.00), unresolved 0",
		"Refusals repeated >3: mentor_done_after_win 12, session_gate 7",
		"Backup restore test: n/a",
	} {
		if !strings.Contains(text, must) {
			t.Errorf("report lacks %q\n%s", must, text)
		}
	}
	parts := Parts(lines)
	if len(parts) == 0 || len(parts) > maxParts {
		t.Fatalf("%d parts", len(parts))
	}
	for i, p := range parts {
		if n := len([]rune(p)); n > 600 {
			t.Fatalf("part %d is %d runes: ownerpush.Scrub would truncate it at 600", i+1, n)
		}
		// The owner-push scrubber must leave the report untouched: nothing in it
		// may look like a credential or a long token (it would print [redacted]).
		if s := ownerpush.Scrub(p); s != p {
			t.Fatalf("ownerpush.Scrub altered part %d:\n%s\n--- became ---\n%s", i+1, p, s)
		}
	}
}

func TestPartsNeverExceedTheBudgetAndSayWhatWasCut(t *testing.T) {
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, fmt.Sprintf("line %02d %s", i, strings.Repeat("x", 70)))
	}
	parts := Parts(lines)
	if len(parts) != maxParts {
		t.Fatalf("%d parts, want the cap %d", len(parts), maxParts)
	}
	for _, p := range parts {
		if n := len([]rune(p)); n > 600 {
			t.Fatalf("part of %d runes", n)
		}
	}
	if !strings.Contains(parts[len(parts)-1], "lines cut)") {
		t.Fatalf("a cut report must say so:\n%s", parts[len(parts)-1])
	}
	if one := Parts([]string{"a", "b"}); len(one) != 1 || one[0] != "a\nb" {
		t.Fatalf("a short report is one unprefixed message: %q", one)
	}
}

func TestRestoreResultReadsNewestAndFlagsFailureAndOverdue(t *testing.T) {
	dir := t.TempDir()
	now := ct(2026, 10, 29, 16, 10)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if r := ReadRestoreResult(dir, now); r.Have {
		t.Fatalf("empty dir must be n/a: %+v", r)
	}
	if r := ReadRestoreResult(filepath.Join(dir, "missing"), now); r.Have {
		t.Fatal("missing dir must be n/a")
	}
	old := now.Add(-3 * 24 * time.Hour)
	write("20261026T170000Z.result", fmt.Sprintf("RESTORE-TEST OK ts=%d src=vl-2026-10-26_050004.db.gz integrity=ok\n", old.Unix()))
	r := ReadRestoreResult(dir, now)
	if !r.Have || r.Status != "OK" || r.Src != "vl-2026-10-26_050004.db.gz" || r.Overdue {
		t.Fatalf("OK result: %+v", r)
	}
	// A newer FAIL wins, whatever the older one said.
	write("20261029T120000Z.result", fmt.Sprintf("RESTORE-TEST FAIL ts=%d src=vl-2026-10-29_050015.db.gz reason=integrity\n", now.Add(-4*time.Hour).Unix()))
	r = ReadRestoreResult(dir, now)
	if r.Status != "FAIL" || !strings.Contains(r.Line(), "FAIL") {
		t.Fatalf("newest result must win: %+v", r)
	}
	// Overdue: the newest is older than 8 days.
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "a.result"), []byte(fmt.Sprintf("RESTORE-TEST OK ts=%d src=x\n", now.Add(-9*24*time.Hour).Unix())), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := ReadRestoreResult(dir2, now); !r.Overdue || !strings.Contains(r.Line(), "OVERDUE") {
		t.Fatalf("a 9-day-old result must read OVERDUE: %+v", r)
	}
	// Garbage is n/a, never a made-up OK.
	dir3 := t.TempDir()
	for name, body := range map[string]string{"a.result": "all good\n", "b.result": "RESTORE-TEST OK src=x\n"} {
		if err := os.WriteFile(filepath.Join(dir3, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if r := ReadRestoreResult(dir3, now); r.Have {
		t.Fatalf("a result with no time must be n/a: %+v", r)
	}
}
