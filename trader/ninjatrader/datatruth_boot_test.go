package ninjatrader

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// Release #55 (integrator): the boot wiring of the m446 checks. The pins drive
// the PRODUCTION function (WireDataTruthChecks, the one bar_persist_wire.go calls
// the moment the wire server exists) and the production store reader.
//
// MUTANTS (RED in the report):
//   B1  WireDataTruthChecks never calls ntwire.WireDataTruth       -> registration pin RED
//   B2  storeSwingInputs reads the wrong timeframe                  -> store-reader pin RED

func dtStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "dt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestWireDataTruthChecksRegistersAllEightAndSignalsReady(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	t.Cleanup(SetDataTruthReadyForTest(false))
	dataTruthWiring.Store(false)
	t.Cleanup(func() { dataTruthWiring.Store(false) })

	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	srv := ntwire.NewTCPServer(nil)

	// nil inputs register nothing and do NOT signal ready (the trader-side stubs keep blocking)
	WireDataTruthChecks(nil, srv)
	WireDataTruthChecks(bh, nil)
	if DataTruthReady() || len(datatruth.Snapshot()) != 0 {
		t.Fatalf("a missing input must register nothing and stay not-ready: ready=%v checks=%v", DataTruthReady(), datatruth.Snapshot())
	}

	WireDataTruthChecks(bh, srv)
	if !DataTruthReady() {
		t.Fatal("the wiring must signal ready once the checks are registered")
	}
	snap := datatruth.Snapshot()
	for _, n := range []string{checks.CheckBadPrint, checks.CheckCrossFrame, checks.CheckLookback, checks.CheckLookbackSwing4h,
		checks.CheckLookbackLevels, checks.CheckLookbackSession, checks.CheckIngestLoss, checks.CheckContractRoll} {
		if _, ok := snap[n]; !ok {
			t.Fatalf("check %q is not registered after WireDataTruthChecks (have %v)", n, snap)
		}
	}
	// every check is a REAL one: none still says "required ... not wired"
	_, reasons := datatruth.Evaluate("MNQ", time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC))
	for _, r := range reasons {
		if strings.Contains(r.Detail, "is not wired") {
			t.Fatalf("%s is still a not-wired stub after the wiring: %s", r.Check, r.Detail)
		}
	}
	// a second call is a no-op (once-guard)
	WireDataTruthChecks(bh, srv)
}

func TestStoreSwingInputsReadsTheCurrentContractOneAndFiveMinute(t *testing.T) {
	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC).UnixMilli()
	var rows []store.BarHistoryDB
	for i := 0; i < 10; i++ {
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: "1m", Contract: "MNQ 12-26", Source: store.BarSourceHistorical,
			OpenTimeMs: base + int64(i)*60_000, O: 100, H: 101, L: 99, C: 100.5 + float64(i), V: 7})
	}
	for i := 0; i < 2; i++ {
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: "5m", Contract: "MNQ 12-26", Source: store.BarSourceHistorical,
			OpenTimeMs: base + int64(i)*300_000, O: 100, H: 101, L: 99, C: 200 + float64(i), V: 35})
	}
	if err := bh.InsertBars(rows); err != nil {
		t.Fatal(err)
	}
	m1, m5, line, err := storeSwingInputs(bh)("MNQ", time.UnixMilli(base))
	if err != nil {
		t.Fatal(err)
	}
	if len(m1) != 10 || len(m5) != 2 || line != 0 {
		t.Fatalf("m1=%d m5=%d line=%v, want 10 / 2 / 0 (the evaluator line is deliberately not coupled)", len(m1), len(m5), line)
	}
	if m1[0].T != base || m1[9].C != 109.5 || m5[1].C != 201 || !m1[0].Final {
		t.Fatalf("the store rows must map to checks.Bar in ascending order: %+v %+v", m1[0], m5[1])
	}
	if _, _, _, err := storeSwingInputs(nil)("MNQ", time.Now()); err == nil {
		t.Fatal("no store must be an ERROR (a check that cannot judge fails closed), not an empty read")
	}
}

// rollFixture inserts a contract roll the way the real 09-10 12-26 roll left the
// store: the OLD contract holds dense, perfect 1m + 5m up to its last stored bar
// (the roll day's morning), the NEW contract holds sparse one-row-a-day import
// snapshots before the roll and dense replay after it, on a price scale 100 pt
// higher. Only the first few sessions of the new contract are dense.
func rollFixture(t *testing.T, bh *store.BarHistoryStore, loc *time.Location, hole func(tm time.Time) bool) (now time.Time) {
	t.Helper()
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	open := func(tm time.Time) bool { return kernel.IsCMEOpen(tm) }
	var rows []store.BarHistoryDB
	add := func(contract, tf string, tm time.Time, px float64) {
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: tf, Contract: contract, Source: store.BarSourceHistorical,
			OpenTimeMs: tm.UnixMilli(), O: px, H: px + 1, L: px - 1, C: px, V: 10})
	}
	cut := at("2026-09-10 08:00")
	px := 30000.0
	for tm := at("2026-08-12 17:00"); !tm.After(cut); tm = tm.Add(time.Minute) {
		if !open(tm) {
			continue
		}
		px += 0.25
		if hole != nil && hole(tm) {
			continue // lost in BOTH frames
		}
		add("MNQ 09-26", "1m", tm, px)
		if tm.Minute()%5 == 0 {
			add("MNQ 09-26", "5m", tm, px)
		}
	}
	// the new contract: sparse snapshots (one 1m + one 5m row a day, 12:00 CT) before the roll ...
	for d := at("2026-08-12 12:00"); d.Before(cut); d = d.AddDate(0, 0, 1) {
		if open(d) {
			add("MNQ 12-26", "1m", d, px+100)
			add("MNQ 12-26", "5m", d, px+100)
		}
	}
	// ... dense replay after the roll
	now = at("2026-09-14 12:30")
	for tm := cut.Add(time.Minute); tm.Before(now); tm = tm.Add(time.Minute) {
		if !open(tm) {
			continue
		}
		px += 0.25
		add("MNQ 12-26", "1m", tm, px+100)
		if tm.Minute()%5 == 0 {
			add("MNQ 12-26", "5m", tm, px+100)
		}
	}
	if err := bh.InsertBars(rows); err != nil {
		t.Fatal(err)
	}
	return now
}

// TestSwing4hJudgesTheStitchedSeriesAcrossARoll (release #55 review, P2 + P3) is
// the production-path pin: WireDataTruthChecks + datatruth.Evaluate over a store
// that just rolled. The series the evaluator reads (old contract up to its last
// bar + new contract after it) is complete and perfect, so the SWING4H check must
// NOT pause. Before the fix the check judged the new contract's rows alone ("only
// N closed 4h buckets" / "buckets inside the warm-up exist in no frame") for days.
//
// MUTANT R1: stitchedBarRows returns the current contract only -> RED.
func TestSwing4hJudgesTheStitchedSeriesAcrossARoll(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	t.Cleanup(SetDataTruthReadyForTest(false))
	dataTruthWiring.Store(false)
	t.Cleanup(func() { dataTruthWiring.Store(false) })
	loc := kernel.CTLocation()

	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := rollFixture(t, bh, loc, nil)
	WireDataTruthChecks(bh, ntwire.NewTCPServer(nil))
	_, reasons := datatruth.Evaluate("MNQ", now)
	for _, r := range reasons {
		if r.Check == checks.CheckLookbackSwing4h {
			t.Fatalf("SWING4H paused on a complete stitched series after a roll: %s", r.Detail)
		}
	}
	// the stitch is the only thing standing between this fixture and a pause: the
	// same store judged on the new contract alone must block (the pre-fix reading).
	m1, m5, _, err := storeSwingInputs(bh)("MNQ", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(m1) < 20000 {
		t.Fatalf("stitched 1m has %d rows - the older contract was not read", len(m1))
	}
	for i := 1; i < len(m1); i++ {
		if m1[i].T <= m1[i-1].T {
			t.Fatalf("stitched 1m not strictly ascending at %d", i)
		}
	}
	// one price scale across the seam: no 100 pt step anywhere in the stitched closes
	for i := 1; i < len(m1); i++ {
		if d := m1[i].C - m1[i-1].C; d > 5 || d < -5 {
			t.Fatalf("seam step %.2f pt at %s: the older contract was not shifted onto the newer scale", d, time.UnixMilli(m1[i].T).UTC())
		}
	}
	_ = m5
}

// TestSwing4hStillPausesOnARealHoleAcrossARoll keeps the stitch honest: a 4h
// stretch of the OLDER contract lost in BOTH frames, inside the warm-up, is a
// real unknowable bucket and must still pause SWING4H (fail closed), stitched or
// not.
func TestSwing4hStillPausesOnARealHoleAcrossARoll(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	t.Cleanup(SetDataTruthReadyForTest(false))
	dataTruthWiring.Store(false)
	t.Cleanup(func() { dataTruthWiring.Store(false) })
	loc := kernel.CTLocation()

	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 8, 31, 17, 0, 0, 0, loc)
	now := rollFixture(t, bh, loc, func(tm time.Time) bool { return !tm.Before(from) && tm.Before(from.Add(4*time.Hour)) })
	WireDataTruthChecks(bh, ntwire.NewTCPServer(nil))
	_, reasons := datatruth.Evaluate("MNQ", now)
	for _, r := range reasons {
		if r.Check == checks.CheckLookbackSwing4h {
			if !strings.Contains(r.Detail, "exist in no frame") {
				t.Fatalf("paused, but for the wrong reason: %s", r.Detail)
			}
			return
		}
	}
	t.Fatal("a 4h bucket lost in both frames inside the warm-up must still pause SWING4H")
}

// TestSwing4hKeepsTheOlderFiveMinuteWhenTheNewFiveMinuteRingIsFull (release #55
// review round 1, finding 1) is the production-path pin for the state
// rollFixture never reaches: the NEW contract's 5m ring already holds the full
// dataTruthStore5mN rows while its 1m is still short. The older contract lost a 4h
// stretch of 1m inside the warm-up but its 5m holds it; the newer contract's 5m
// holds it too, but the head5 cut (strictly after the older's last bar) drops it,
// so the ONLY frame that can cover the bucket is the older contract's 5m. Reading
// it with N-len(r5) = 0 rows left the bucket in no frame -> SWING4H paused on data
// that exists. Real instance: MNQ 09-26 / 12-26 at 2026-10-07 14:10 CT.
//
// MUTANT R2: the older 5m is fetched with dataTruthStore5mN-len(r5) -> RED.
func TestSwing4hKeepsTheOlderFiveMinuteWhenTheNewFiveMinuteRingIsFull(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	t.Cleanup(SetDataTruthReadyForTest(false))
	dataTruthWiring.Store(false)
	t.Cleanup(func() { dataTruthWiring.Store(false) })
	loc := kernel.CTLocation()
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	var rows []store.BarHistoryDB
	add := func(contract, tf string, tm time.Time, px float64) {
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: tf, Contract: contract, Source: store.BarSourceHistorical,
			OpenTimeMs: tm.UnixMilli(), O: px, H: px + 1, L: px - 1, C: px, V: 10})
	}
	cut := at("2026-09-10 08:00")
	holeFrom := at("2026-09-02 17:00") // 1m of the OLD contract lost here (5m kept)
	holeTo := holeFrom.Add(4 * time.Hour)
	now := at("2026-09-14 12:30")
	// a quiet, flat tape (a bounded wiggle, no trend): the 1m-built line and the
	// all-frames line then differ only by the bucket the 5m restores, well inside the
	// tolerance - the real 10-07 case was an overnight bucket on a flat tape.
	px := 30000.0
	k := 0
	for tm := at("2026-08-12 17:00"); tm.Before(now); tm = tm.Add(time.Minute) {
		if !kernel.IsCMEOpen(tm) {
			continue
		}
		k++
		px = 30000 + 0.25*float64(k%8)
		if !tm.After(cut) {
			if tm.Before(holeFrom) || !tm.Before(holeTo) {
				add("MNQ 09-26", "1m", tm, px)
			}
			if tm.Minute()%5 == 0 {
				add("MNQ 09-26", "5m", tm, px)
			}
		} else {
			add("MNQ 12-26", "1m", tm, px+100)
		}
		// the NEW contract's 5m is dense for the whole span (a full ring), the 1m only after the cut
		if tm.Minute()%5 == 0 {
			add("MNQ 12-26", "5m", tm, px+100)
		}
	}
	if err := bh.InsertBars(rows); err != nil {
		t.Fatal(err)
	}
	n5, err := bh.LastNBarsCurrentContract("MNQ", "5m", dataTruthStore5mN)
	if err != nil {
		t.Fatal(err)
	}
	n1, err := bh.LastNBarsCurrentContract("MNQ", "1m", dataTruthStore1mN)
	if err != nil {
		t.Fatal(err)
	}
	if len(n5) < dataTruthStore5mN || len(n1) == 0 || len(n1) >= dataTruthStore1mN {
		t.Fatalf("fixture is not in the target state (new 5m ring full, new 1m short): 5m=%d 1m=%d", len(n5), len(n1))
	}
	WireDataTruthChecks(bh, ntwire.NewTCPServer(nil))
	_, reasons := datatruth.Evaluate("MNQ", now)
	for _, r := range reasons {
		if r.Check == checks.CheckLookbackSwing4h {
			t.Fatalf("SWING4H paused although the older contract's 5m holds the bucket: %s", r.Detail)
		}
	}
	_, m5, _, err := storeSwingInputs(bh)("MNQ", now)
	if err != nil {
		t.Fatal(err)
	}
	var covered bool
	for _, b := range m5 {
		if b.T >= holeFrom.UnixMilli() && b.T < holeTo.UnixMilli() {
			covered = true
			break
		}
	}
	if !covered {
		t.Fatal("the stitched 5m lost the stretch only the older contract's 5m held")
	}
}

// ── #55 m449 — the seed edge (ready at WIRE time was before the replay landed) ──
//
// MUTANT S3: rehydrateRingFromStore (the boot entry bar_persist_wire.go calls;
// TestRingRehydrateIsWiredAtBoot census-pins that call site) without
// MarkDataTruthSeeded -> RED. The roll / scale-break reseeds use
// rehydrateRingFromStoreWith and must NOT open the edge.

func TestWireDataTruthChecksIsRegisteredButNotYetJudgeable(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	t.Cleanup(SetDataTruthReadyForTest(false))
	dataTruthWiring.Store(false)
	t.Cleanup(func() { dataTruthWiring.Store(false) })
	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	srv := ntwire.NewTCPServer(nil)
	WireDataTruthChecks(bh, srv)
	if !DataTruthReady() {
		t.Fatal("registered")
	}
	if DataTruthJudgeable() || DataTruthSeeded() {
		t.Fatal("registration alone must NOT make the checks judgeable: the replay has not landed")
	}
}

func TestBootRehydrateOpensTheSeedEdge(t *testing.T) {
	t.Cleanup(SetDataTruthReadyForTest(true))
	t.Cleanup(SetDataTruthSeededForTest(false))
	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	srv := ntwire.NewTCPServer(nil)
	if DataTruthJudgeable() {
		t.Fatal("precondition: not judgeable before the seed")
	}
	rehydrateRingFromStoreWith(bh, srv, time.Now(), true) // a roll reseed is not the boot seed
	if DataTruthSeeded() {
		t.Fatal("a roll-driven reseed must not open the boot seed edge")
	}
	rehydrateRingFromStore(bh, srv, time.Now())
	if !DataTruthSeeded() || !DataTruthJudgeable() {
		t.Fatal("the boot seed step must open the edge, however the rehydrate ended")
	}
}

// ── release #55 round 3 (review 3, P2): the reach invariant after a roll ─────

// replayRollFixture is the roll the way a LIVE roll leaves the store: the OLD
// contract is dense and perfect up to its last stored bar (cut), and the NEW
// contract holds its AddOn replay (bars_back, ~2000 1m bars - about 33 open hours
// that OVERLAP the older contract's tail) plus every live bar since, on a price
// scale basis points higher. The tape is a slow swing (a reversal every ~3
// sessions), because the seed residual of the 4h EMA recurrence is what a
// window mismatch turns into a "disagreement" - a quiet tape hides it.
func replayRollFixture(t *testing.T, bh *store.BarHistoryStore, loc *time.Location, sinceRollSessions int, periodDays, amp float64) (now time.Time) {
	t.Helper()
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	const basis = 250.0
	cut := at("2026-12-15 08:00")
	now = cut.Add(time.Duration(sinceRollSessions) * 24 * time.Hour).Add(4 * time.Hour)
	// the price of minute tm on the OLD scale: a ~3-session swing
	path := func(tm time.Time) float64 {
		return 30000 + amp*math.Sin(float64(tm.Unix())/(periodDays*24*3600)*2*math.Pi)
	}
	var rows []store.BarHistoryDB
	add := func(contract, tf string, tm time.Time, px float64) {
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: tf, Contract: contract, Source: store.BarSourceHistorical,
			OpenTimeMs: tm.UnixMilli(), O: px, H: px + 1, L: px - 1, C: px, V: 10})
	}
	replayFrom := cut.Add(-33 * time.Hour)
	for tm := at("2026-11-10 17:00"); tm.Before(now); tm = tm.Add(time.Minute) {
		if !kernel.IsCMEOpen(tm) {
			continue
		}
		px := path(tm)
		if !tm.After(cut) {
			add("MNQ 12-26", "1m", tm, px)
			if tm.Minute()%5 == 0 {
				add("MNQ 12-26", "5m", tm, px)
			}
		}
		if !tm.Before(replayFrom) {
			add("MNQ 03-27", "1m", tm, px+basis)
			if tm.Minute()%5 == 0 {
				add("MNQ 03-27", "5m", tm, px+basis)
			}
		}
	}
	if err := bh.InsertBars(rows); err != nil {
		t.Fatal(err)
	}
	return now
}

// rollStore builds the replay-roll store and returns it with the judge instant.
func rollStore(t *testing.T, sessions int, periodDays, amp float64) (*store.BarHistoryStore, time.Time) {
	t.Helper()
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	t.Cleanup(SetDataTruthReadyForTest(false))
	dataTruthWiring.Store(false)
	t.Cleanup(func() { dataTruthWiring.Store(false) })
	st := dtStore(t)
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	return bh, replayRollFixture(t, bh, kernel.CTLocation(), sessions, periodDays, amp)
}

// TestStitchedFiveMinuteNeverReachesBeforeTheOneMinute pins the STRUCTURAL
// invariant the round-3 reviewer named (it does not depend on the price path):
// across a roll whose new contract carries an overlapping replay, the stitched 5m
// does not begin before the stitched 1m.
//
// MUTANT R3: the older 1m is read with dataTruthStore1mN-len(r1) and the rowsFrom
// trim is removed -> RED (the 5m begins ~3 sessions before the 1m).
func TestStitchedFiveMinuteNeverReachesBeforeTheOneMinute(t *testing.T) {
	loc := kernel.CTLocation()
	for _, sessions := range []int{1, 2, 3, 5, 8} {
		bh, now := rollStore(t, sessions, 5, 3000)
		m1, m5, _, err := storeSwingInputs(bh)("MNQ", now)
		if err != nil {
			t.Fatal(err)
		}
		if len(m1) == 0 || len(m5) == 0 {
			t.Fatalf("sessions=%d: empty stitched series 1m=%d 5m=%d", sessions, len(m1), len(m5))
		}
		if m5[0].T < m1[0].T {
			t.Fatalf("sessions=%d: the stitched 5m begins %s, before the stitched 1m %s - the 5m reaches back further than the series the check judges",
				sessions, time.UnixMilli(m5[0].T).In(loc).Format("01-02 15:04"), time.UnixMilli(m1[0].T).In(loc).Format("01-02 15:04"))
		}
	}
}

// TestSwing4hDoesNotPauseAfterARollOnAReversalTape is the PRODUCTION-path pin of
// the same defect: WireDataTruthChecks + datatruth.Evaluate over a store that
// rolled with an overlapping replay, on a strong swing (the 4h EMA seed residual
// of a window mismatch is what turns into a 1 pt "disagreement"; a quiet tape
// hides it, which is why the roll pins above never tripped). No data is lost, so
// SWING4H must never pause 1 / 2 / 3 / 5 / 8 sessions after the roll, at 8:00 or
// 12:00 CT. A REAL hole must still pause (TestSwing4hStillPausesOnARealHoleAcrossARoll).
//
// MUTANT R3 (same as above) -> RED at sessions=1 ("15 bucket(s) missing from the 1m").
func TestSwing4hDoesNotPauseAfterARollOnAReversalTape(t *testing.T) {
	for _, sessions := range []int{1, 2, 3, 5, 8} {
		for _, extraH := range []int{0, 8} {
			bh, now := rollStore(t, sessions, 5, 3000)
			now = now.Add(time.Duration(extraH) * time.Hour)
			WireDataTruthChecks(bh, ntwire.NewTCPServer(nil))
			_, reasons := datatruth.Evaluate("MNQ", now)
			for _, r := range reasons {
				if r.Check == checks.CheckLookbackSwing4h {
					t.Fatalf("sessions=%d +%dh: SWING4H paused after a roll with no data lost: %s", sessions, extraH, r.Detail)
				}
			}
		}
	}
}

// Round 4 (re-review 1 note): the trim (rowsFrom) alone makes the 5m never reach
// before the 1m, so the "full older 1m window" half of the roll fix was pinned only
// together with it. This pins the window on its own, through the production
// stitch: with an older contract that can supply them, the stitched 1m holds at
// least a FULL window (dataTruthStore1mN rows) however many rows of the NEW
// contract's replay overlap the older's tail. Read as "N minus what the newer
// contract already holds" it came out about len(replay) rows short, which shrinks
// the 1m the swing line is built from while the 5m keeps its whole reach.
//
// MUTANT W (shown RED): the older 1m is read with dataTruthStore1mN-len(r1).
func TestStitchedOneMinuteIsAFullWindowAfterARoll(t *testing.T) {
	for _, sessions := range []int{1, 3, 8} {
		bh, now := rollStore(t, sessions, 5, 3000)
		m1, _, _, err := storeSwingInputs(bh)("MNQ", now)
		if err != nil {
			t.Fatal(err)
		}
		if len(m1) < dataTruthStore1mN {
			t.Fatalf("sessions=%d: the stitched 1m holds %d rows, short of the %d-row window the older contract can fill - it was read as N minus the replay overlap",
				sessions, len(m1), dataTruthStore1mN)
		}
	}
}
