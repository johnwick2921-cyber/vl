package ninjatrader

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
)

// ── PERSISTENT BAR-GAP AUDIT (m444) — PINS ──────────────────────────────────
//
// The real 2026-10-26 shape comes from data/data.db, exported READ-ONLY into
// testdata/mnq_20261026_1m_gap.json (minute offsets of the stored MNQ 1m/3m/5m
// bars on MNQ 12-26, 14:00 CT Mon .. 10:00 CT Tue): 1m is missing 17:00-21:56
// CT while 3m and 5m are whole. Every tracker-level pin runs the PRODUCTION
// path: newGHFixture (started TCPServer, fake AddOn, SQLite, the real tracker
// and persister) + the tracker's own auditPass — the function runAudit calls.

type realGapFixture struct {
	BaseMs int64   `json:"base_ms"`
	EndMs  int64   `json:"end_ms"`
	One    []int64 `json:"tf_1m"`
	Three  []int64 `json:"tf_3m"`
	Five   []int64 `json:"tf_5m"`
}

func loadRealGap(t *testing.T) (one, three, five []int64, f realGapFixture) {
	t.Helper()
	raw, err := os.ReadFile("testdata/mnq_20261026_1m_gap.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	abs := func(offs []int64) []int64 {
		out := make([]int64, len(offs))
		for i, o := range offs {
			out[i] = f.BaseMs + o*60_000
		}
		return out
	}
	return abs(f.One), abs(f.Three), abs(f.Five), f
}

func evidOf(three, five []int64) map[int64]struct{} {
	m := map[int64]struct{}{}
	evidenceMinutes(3*time.Minute, three, m)
	evidenceMinutes(5*time.Minute, five, m)
	return m
}

// the 10-26 hole: 17:00-21:56 CT = 297 open minutes, closed by the 21:57 bar.
var (
	real1026From = ct(2026, 10, 26, 17, 0).UnixMilli()
	real1026To   = ct(2026, 10, 26, 21, 57).UnixMilli()
)

// A real-data pin: the audit's detector finds exactly the 10-26 hole, with the
// right missing count, in the rows the store actually held.
// MUTANT (shown RED): findBarHoles(...) -> nil at the top of auditSpans -> no span -> RED.
func TestAuditSpansFindTheReal1026MNQHole(t *testing.T) {
	one, three, five, f := loadRealGap(t)
	spans := auditSpans(one, evidOf(three, five), f.BaseMs, f.EndMs, 3, barHoleExpected)
	if len(spans) != 1 {
		t.Fatalf("want exactly the one 10-26 hole in the real rows, got %+v", spans)
	}
	sp := spans[0]
	if sp.FromMs != real1026From || sp.ToMs != real1026To || sp.OpenMissing != 297 {
		t.Fatalf("hole = %s (%d open min), want 10-26 17:00–21:57 CT (297)", holeWindowCT(sp.FromMs, sp.ToMs), sp.OpenMissing)
	}
}

// CROSS-FRAME on real rows: with the 1m bars from 21:57 on removed (so no 1m bar
// stands on the far side of the hole), the between-bars rule is blind — but 3m/5m
// prove the market traded. The audit must still call it a hole.
// MUTANT (shown RED): drop the two edge(...) calls in auditSpans -> no span -> RED.
func TestAuditSpansCrossFrameCatchesAHoleWithNoLaterOneMinuteBar(t *testing.T) {
	one, three, five, f := loadRealGap(t)
	var head []int64
	for _, m := range one {
		if m < real1026From {
			head = append(head, m)
		}
	}
	if got := findBarHoles(head, 3, barHoleExpected); len(got) != 0 {
		t.Fatalf("precondition: the between-bars rule must see nothing here: %+v", got)
	}
	spans := auditSpans(head, evidOf(three, five), f.BaseMs, f.EndMs, 3, barHoleExpected)
	if len(spans) != 1 || spans[0].FromMs != real1026From {
		t.Fatalf("cross-frame must find the hole from 17:00: %+v", spans)
	}
	// 17:00 Mon -> 09:59 Tue inclusive = 17h = 1020 minutes of 5m/3m evidence
	if spans[0].OpenMissing != 1020 {
		t.Fatalf("missing = %d, want 1020 (17:00 Mon .. 09:59 Tue)", spans[0].OpenMissing)
	}
}

// ... and with NO 1m bar in the window at all the whole evidenced stretch is a hole.
func TestAuditSpansCrossFrameWithNoOneMinuteAtAll(t *testing.T) {
	_, three, five, f := loadRealGap(t)
	spans := auditSpans(nil, evidOf(three, five), f.BaseMs, f.EndMs, 3, barHoleExpected)
	if len(spans) != 1 || spans[0].FromMs != f.BaseMs {
		t.Fatalf("want one hole from the first evidenced minute: %+v", spans)
	}
	// no evidence, no 1m: nothing to judge (that is FEED DOWN's story)
	if got := auditSpans(nil, nil, f.BaseMs, f.EndMs, 3, barHoleExpected); len(got) != 0 {
		t.Fatalf("no 1m and no 3m/5m evidence must not invent a hole: %+v", got)
	}
}

// the 16:00-17:00 break and a closed weekend are never holes, however many
// minutes lie between the bars.
func TestAuditSpansClosedMarketIsNotAHole(t *testing.T) {
	var one []int64
	for m := ct(2026, 10, 9, 15, 0); m.Before(ct(2026, 10, 9, 16, 0)); m = m.Add(time.Minute) {
		one = append(one, m.UnixMilli())
	}
	for m := ct(2026, 10, 11, 17, 0); m.Before(ct(2026, 10, 11, 17, 30)); m = m.Add(time.Minute) {
		one = append(one, m.UnixMilli())
	}
	if got := auditSpans(one, nil, one[0], one[len(one)-1]+60_000, 3, barHoleExpected); len(got) != 0 {
		t.Fatalf("Fri 16:00 -> Sun 17:00 is closed: %+v", got)
	}
}

func TestAuditWindowStartCountsTradingSessions(t *testing.T) {
	// Wed 2026-10-28 10:00 CT, 7 sessions: the one opened Tue 10-27 17:00, Mon
	// 10-26 17:00, Sun 10-25 17:00, Thu 10-22, Wed 10-21, Tue 10-20, Mon 10-19
	// 17:00 CT (Fri and Sat evenings do not trade).
	got := auditWindowStart(ct(2026, 10, 28, 10, 0), 7, barHoleExpected)
	if want := ct(2026, 10, 19, 17, 0); !got.Equal(want) {
		t.Fatalf("window start = %s, want %s", kernel.TableTimeCT(got), kernel.TableTimeCT(want))
	}
	if got := auditWindowStart(ct(2026, 10, 28, 10, 0), 1, barHoleExpected); !got.Equal(ct(2026, 10, 27, 17, 0)) {
		t.Fatalf("one session = the current one: %s", kernel.TableTimeCT(got))
	}
}

func TestAuditKnobs(t *testing.T) {
	t.Setenv("BARS_GAP_AUDIT_DAYS", "")
	if n, src := barGapAuditDays(); n != 7 || src != "default" {
		t.Fatalf("default days = %d %q", n, src)
	}
	t.Setenv("BARS_GAP_AUDIT_DAYS", "0")
	if n, _ := barGapAuditDays(); n != 0 {
		t.Fatalf("0 must switch the audit off, got %d", n)
	}
	t.Setenv("BARS_GAP_AUDIT_DAYS", "999")
	if n, src := barGapAuditDays(); n != barGapAuditDaysMax || !strings.Contains(src, "capped") {
		t.Fatalf("cap = %d %q", n, src)
	}
	t.Setenv("BARS_GAP_AUDIT_DAYS", "x")
	if n, src := barGapAuditDays(); n != 7 || !strings.Contains(src, "unreadable") {
		t.Fatalf("garbage = %d %q", n, src)
	}
	t.Setenv("BARS_GAP_AUDIT_EVERY_MIN", "3")
	if d, src := barGapAuditEvery(); d != 3*time.Minute || src != "env" {
		t.Fatalf("every = %v %q", d, src)
	}
}

// putOpen stores one row per CME-open minute of [from, to) for (MNQ, tf), on the
// fixture's contract — the shape the persister leaves behind.
func (f *ghFixture) putOpen(tf string, from, to time.Time, src string) {
	f.t.Helper()
	step := map[string]time.Duration{"1m": time.Minute, "3m": 3 * time.Minute, "5m": 5 * time.Minute}[tf]
	var rows []store.BarHistoryDB
	for m := from; m.Before(to); m = m.Add(step) {
		if !barHoleExpected(m) {
			continue
		}
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: tf, OpenTimeMs: m.UnixMilli(), O: 30700, H: 30701, L: 30699, C: 30700, V: 1, Contract: ghContract, Source: src})
	}
	if err := f.bh.InsertBars(rows); err != nil {
		f.t.Fatal(err)
	}
}

func openMinutes(from, to time.Time) int {
	n := 0
	for m := from; m.Before(to); m = m.Add(time.Minute) {
		if barHoleExpected(m) {
			n++
		}
	}
	return n
}

// THE BUG: a hole older than the current session, with no live 1m bar arriving
// to judge it, was never seen. Here the tracker has judged NOTHING live
// (checked is empty — the boot scan never ran), the hole is Tue 10:00-11:59 CT
// the day before the session the boot scan reaches, and the audit finds it from
// the store alone, naming the symbol through the real wire.
// MUTANT (shown RED): return at the top of auditPass -> no hole -> RED.
func TestAuditFindsAnOldHoleWithNoLiveArrival(t *testing.T) {
	f := newGHFixture(t)
	f.putOpen("1m", ct(2026, 10, 13, 8, 0), ct(2026, 10, 13, 10, 0), store.BarSourceHistorical)
	f.putOpen("1m", ct(2026, 10, 13, 12, 0), ct(2026, 10, 14, 7, 46), store.BarSourceHistorical)
	if len(f.tr.checked) != 0 {
		t.Fatalf("precondition: no live arrival has been judged: %v", f.tr.checked)
	}
	f.tr.auditPass(f.clk.Now())
	hs := f.holes()
	if len(hs) != 1 {
		t.Fatalf("the audit must open the 10-13 10:00–12:00 hole, got %+v\n%s", hs, f.sink.dump())
	}
	h := hs[0]
	if h.Source != holeSourceAudit || h.State != holeStateRefilling || h.Window != "10-13 10:00–12:00 CT" || h.MissingMin != 120 || h.Symbol != "MNQ" {
		t.Fatalf("hole facts wrong: %+v", h)
	}
	if f.sink.count("bars HOLE found by the AUDIT: MNQ 1m 10-13 10:00–12:00 CT on "+ghContract+" — 120 open minute(s)") != 1 ||
		f.sink.count(BarsMissingAction) != 1 {
		t.Fatalf("one line naming the window, count and the owner's action:\n%s", f.sink.dump())
	}
	if d := f.delta(); d.Detected != 1 {
		t.Fatalf("detected +%d, want 1 (the existing counter)", d.Detected)
	}
	a := f.tr.snapshotAudit()
	if !a.Enabled || a.Passes != 1 || a.FoundByAudit != 1 || a.OpenHoles != 1 || a.Audited["MNQ"] != ghContract || a.LastPass == nil {
		t.Fatalf("audit block wrong: %+v", a)
	}
}

// THE SAME HOLE IS ANNOUNCED ONCE. A hole older than NT8's replay window is
// RETIRED at the next tick — and it must STAY listed, not be dropped (a dropped
// hole is re-found by the next audit pass and announced again every 10 minutes).
// MUTANT (shown RED): in collectDue's replay branch put removeLocked(h) back
// instead of h.State = holeStateRetired -> the second pass re-adds it: two
// "found by the AUDIT" lines, two RETIRED lines -> RED.
func TestAuditAnnouncesAnOldHoleOnceAndKeepsItListed(t *testing.T) {
	f := newGHFixture(t)
	f.putOpen("1m", ct(2026, 10, 6, 8, 0), ct(2026, 10, 6, 9, 0), store.BarSourceHistorical)
	f.putOpen("1m", ct(2026, 10, 13, 12, 0), ct(2026, 10, 14, 7, 46), store.BarSourceHistorical)
	want := openMinutes(ct(2026, 10, 6, 9, 0), ct(2026, 10, 13, 12, 0))
	now := f.clk.Now()
	f.tr.auditPass(now)
	f.tr.tick(now.Add(20 * time.Second)) // the retire check runs before every gate
	for i := 1; i <= 3; i++ {
		f.tr.auditPass(now.Add(time.Duration(i) * 10 * time.Minute))
		f.tr.tick(now.Add(time.Duration(i)*10*time.Minute + 20*time.Second))
	}
	hs := f.holes()
	if len(hs) != 1 || hs[0].State != holeStateRetired || hs[0].MissingMin != want || hs[0].Source != holeSourceAudit {
		t.Fatalf("the retired hole must stay listed once (want %d min): %+v\n%s", want, hs, f.sink.dump())
	}
	if n := f.sink.count("bars HOLE found by the AUDIT"); n != 1 {
		t.Fatalf("announced %d times, want once:\n%s", n, f.sink.dump())
	}
	if n := f.sink.count("bars hole RETIRED"); n != 1 {
		t.Fatalf("retired %d times, want once:\n%s", n, f.sink.dump())
	}
	if d := f.delta(); d.Detected != 1 || d.Unfilled != 0 || d.Rerequests != 0 {
		t.Fatalf("detected +%d unfilled +%d sent +%d, want 1/0/0", d.Detected, d.Unfilled, d.Rerequests)
	}
	a := f.tr.snapshotAudit()
	if a.UnfilledHoles7d != 1 || a.UnfilledMissing != want || a.Passes != 4 {
		t.Fatalf("the 7-day figure is re-derived, not per-boot: %+v", a)
	}
}

// A hole the store now holds is closed by the audit itself.
// MUTANT (shown RED): delete the reconcile loop in applyAuditLocked -> the hole
// stays listed after the bars arrived -> RED.
func TestAuditClosesAHoleTheStoreNowHolds(t *testing.T) {
	f := newGHFixture(t)
	f.putOpen("1m", ct(2026, 10, 13, 8, 0), ct(2026, 10, 13, 10, 0), store.BarSourceHistorical)
	f.putOpen("1m", ct(2026, 10, 13, 12, 0), ct(2026, 10, 14, 7, 46), store.BarSourceHistorical)
	now := f.clk.Now()
	f.tr.auditPass(now)
	if len(f.holes()) != 1 {
		t.Fatalf("precondition: one hole:\n%s", f.sink.dump())
	}
	f.putOpen("1m", ct(2026, 10, 13, 10, 0), ct(2026, 10, 13, 12, 0), store.BarSourceHistorical)
	f.tr.auditPass(now.Add(10 * time.Minute))
	if hs := f.holes(); len(hs) != 0 {
		t.Fatalf("the store holds the bars: the hole must close, got %+v", hs)
	}
	if f.sink.count("bars hole FILLED: MNQ 1m 10-13 10:00–12:00 CT") != 1 {
		t.Fatalf("one FILLED line:\n%s", f.sink.dump())
	}
	if d := f.delta(); d.Filled != 1 {
		t.Fatalf("filled +%d, want 1", d.Filled)
	}
}

// The real 10-26 rows through the production tracker: the audit opens the
// 17:00-21:57 CT hole (297 min). The wall of the data is the store's, not a
// synthetic one.
func TestAuditOnTheReal1026RowsOpensTheHoleThroughTheTracker(t *testing.T) {
	one, three, five, _ := loadRealGap(t)
	f := newGHFixture(t)
	put := func(tf string, ms []int64) {
		rows := make([]store.BarHistoryDB, 0, len(ms))
		for _, m := range ms {
			rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: tf, OpenTimeMs: m, O: 30700, H: 30701, L: 30699, C: 30700, V: 1, Contract: ghContract, Source: store.BarSourceHistorical})
		}
		if err := f.bh.InsertBars(rows); err != nil {
			t.Fatal(err)
		}
	}
	put("1m", one)
	put("3m", three)
	put("5m", five)
	f.tr.auditPass(ct(2026, 10, 27, 10, 30))
	hs := f.holes()
	if len(hs) != 1 {
		t.Fatalf("want the one 10-26 hole, got %+v\n%s", hs, f.sink.dump())
	}
	if hs[0].Window != "10-26 17:00–21:57 CT" || hs[0].MissingMin != 297 || hs[0].Source != holeSourceAudit {
		t.Fatalf("real hole facts: %+v", hs[0])
	}
}

// What the owner is told. An audit-found hole beyond the replay is RETIRED; the
// monitor beat's read (UnfilledHolesFeeding) must carry it with OutsideReplay so
// the push promises no re-request, and only while NT8 is feeding live bars.
// MUTANT (shown RED): keep the `h.State != holeStateUnfilled` filter only -> the
// retired hole is never reported to the owner -> RED.
func TestAuditFoundRetiredHoleReachesTheOwnerRead(t *testing.T) {
	f := newGHFixture(t)
	f.putOpen("1m", ct(2026, 10, 6, 8, 0), ct(2026, 10, 6, 9, 0), store.BarSourceHistorical)
	f.putOpen("1m", ct(2026, 10, 13, 12, 0), ct(2026, 10, 14, 7, 46), store.BarSourceHistorical)
	now := f.clk.Now()
	f.tr.auditPass(now)
	f.tr.tick(now.Add(20 * time.Second))
	f.stream(ct(2026, 10, 14, 7, 46), ct(2026, 10, 14, 7, 48)) // live receipts
	got := f.tr.unfilledFeeding("MNQ", now)
	if len(got) != 1 || !got[0].OutsideReplay || got[0].Window == "" || got[0].Missing != openMinutes(ct(2026, 10, 6, 9, 0), ct(2026, 10, 13, 12, 0)) {
		t.Fatalf("the owner read must carry the retired hole: %+v\n%s", got, f.sink.dump())
	}
	// not while the feed is not live: that is FEED DOWN's story
	f.tr.receiptClock = func() time.Time { return time.Now().Add(liveBarFreshness + time.Minute) }
	if got := f.tr.unfilledFeeding("MNQ", now); len(got) != 0 {
		t.Fatalf("a stalled feed must not be reported as missing history: %+v", got)
	}
}

// BARS_GAP_AUDIT_DAYS=0 switches the pass off (the live detector is untouched).
func TestAuditOffReadsNothing(t *testing.T) {
	f := newGHFixture(t)
	f.putOpen("1m", ct(2026, 10, 13, 8, 0), ct(2026, 10, 13, 10, 0), store.BarSourceHistorical)
	f.putOpen("1m", ct(2026, 10, 13, 12, 0), ct(2026, 10, 14, 7, 46), store.BarSourceHistorical)
	f.tr.auditDays = 0
	f.tr.auditPass(f.clk.Now())
	if len(f.holes()) != 0 || f.tr.snapshotAudit().Enabled || f.tr.snapshotAudit().Passes != 0 {
		t.Fatalf("an audit switched off must do nothing")
	}
	if !strings.Contains(f.tr.auditBootLine(), "OFF") {
		t.Fatalf("boot line must say OFF: %s", f.tr.auditBootLine())
	}
}
