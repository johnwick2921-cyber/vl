package trader

import (
	"math"
	"sort"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── m456 (release #56) defect 2: a roll seam is not a hole in the 4h EMA34 ──
//
// The REAL 09-26 -> 12-26 shape: the stitch cuts the older contract at its last
// stored bar and the head after it, so a 4h bucket the older never recorded but
// the head DID store (09-11 01:00-05:00 CT on the real roll, 230 minutes) is
// absent from the stitched series. holedFourHBuckets read that as a data hole:
// SWING4H was refused for ~68 folds (~11 trading days) after each quarterly
// roll, and a refill could not cure it (the refilled minutes are the same
// seam). The stitcher already holds those bars (SeamBars); a minute is present
// when ANY contract stores it, and the line is built from all of them.
//
// Built into a real bar store and run through the production call sites:
// mentorSeedAtStart and mentorReseedOnRefill (both hand the evaluator the
// stitch's SeamBars via SetSeamBars: the line FOLDS the seam bucket).

// m456SeamPrice is what the head stored in the seam bucket: far from the 110
// the rest of the tape sits at, so a fold of it moves the line measurably.
const m456SeamPrice = 140

// m456Store fills a store with the two-contract roll. The older (09-26) lacks
// the whole 01:00-05:00 CT bucket of 09-10 and ends at its last stored bar on
// 09-14 10:33 (the cut); the head (12-26) stores the bucket iff headHas.
func m456Store(t *testing.T, at *AutoTrader, headHas bool) (holeFrom, holeTo time.Time) {
	t.Helper()
	bh := store.NewBarHistoryStore(at.store.GormDB())
	holeFrom, holeTo = r4CT(time.September, 10, 1, 0), r4CT(time.September, 10, 5, 0)
	inHole := func(m time.Time) bool { return !m.Before(holeFrom) && m.Before(holeTo) }
	end := r4CT(time.September, 16, 9, 59)
	var rows []store.BarHistoryDB
	rows = append(rows, r4Rows("MNQ 09-26", store.BarSourceLive, 100, r4CT(time.September, 7, 17, 0), r4CT(time.September, 14, 10, 34), inHole)...)
	rows = append(rows, r4Rows("MNQ 12-26", store.BarSourceLive, 110, holeTo, end, nil)...)
	if headHas {
		rows = append(rows, r4Rows("MNQ 12-26", store.BarSourceHistorical, m456SeamPrice, holeFrom, holeTo, nil)...)
	}
	rows = append(rows, r4Rows1H("MNQ 09-26", 100, r4CT(time.September, 7, 17, 0), r4CT(time.September, 14, 10, 0))...)
	rows = append(rows, r4Rows1H("MNQ 12-26", 110, r4CT(time.September, 7, 17, 0), end)...)
	if err := bh.InsertBars(rows); err != nil {
		t.Fatal(err)
	}
	return holeFrom, holeTo
}

func m456Seed(t *testing.T, headHas bool) (*AutoTrader, time.Time, time.Time, time.Time) {
	t.Helper()
	at := seedStateTrader(t)
	at.exchange = "ninjatrader"
	f := &intFake{blockedFor: func(int) bool { return false }} // only the swing-line leg is under test
	armIntegrityGate(t, f)
	hf, ht := m456Store(t, at, headHas)
	now := r4CT(time.September, 16, 10, 0)
	setMentorNowSource(func() time.Time { return now })
	at.mentorSeedAtStart()
	return at, now, hf, ht
}

// A bucket the HEAD stores is present: the production seed's line is not stale.
func TestMentorSeedAtStart_RollSeamBucket_SwingLineNotStale(t *testing.T) {
	at, _, _, _ := m456Seed(t, true)
	sw := at.mentorEval.State.Swing
	if sw.EmaCount < 10 {
		t.Fatalf("setup: the seeded 4h line has only %d closes", sw.EmaCount)
	}
	if sw.LineStale || len(sw.HoledBuckets) > 0 {
		t.Fatalf("MNQ 12-26 stores 09-10 01:00-05:00 CT: the roll cut must not make the EMA34 line stale: stale=%v holed=%v", sw.LineStale, sw.HoledBuckets)
	}
}

// The negative control: the SAME bucket stored by NO contract is a real hole —
// still stale, and it names the bucket. Then the refill puts the minutes in the
// head's store; the production re-seed (mentorReseedOnRefill) clears it.
func TestMentorReseedOnRefill_RollSeamBucket_ClearsTheStaleLine(t *testing.T) {
	at, now, hf, ht := m456Seed(t, false)
	sw := at.mentorEval.State.Swing
	if !sw.LineStale || len(sw.HoledBuckets) != 1 || sw.HoledBuckets[0] != hf.UnixMilli() {
		t.Fatalf("control: a bucket NO contract stores is a hole — stale, naming %d; got stale=%v holed=%v", hf.UnixMilli(), sw.LineStale, sw.HoledBuckets)
	}

	bh := store.NewBarHistoryStore(at.store.GormDB())
	if err := bh.InsertBars(r4Rows("MNQ 12-26", store.BarSourceHistorical, m456SeamPrice, hf, ht, nil)); err != nil {
		t.Fatal(err)
	}
	ntTrader.NoteHistoryRefill("MNQ", "1m", 240, "m456 refill of the roll-seam bucket")
	at.mentorEvalMu.Lock()
	at.mentorReseedOnRefill(now.UnixMilli())
	at.mentorEvalMu.Unlock()
	sw = at.mentorEval.State.Swing
	if sw.LineStale || len(sw.HoledBuckets) > 0 {
		t.Fatalf("the refilled bucket is stored by the head: the re-seed must clear the stale mark, got stale=%v holed=%v", sw.LineStale, sw.HoledBuckets)
	}
}

// m456UnionLine is the line a full recompute gives on every minute any contract
// stores: the production stitch (mentorFullKeyLevelHistory) plus its seam bars.
func m456UnionLine(t *testing.T, at *AutoTrader, now time.Time) (line float64, count int, bare float64) {
	t.Helper()
	bh := store.NewBarHistoryStore(at.store.GormDB())
	stitched, full1h, _, _, _, seam := at.mentorFullKeyLevelHistory(bh)
	if len(seam) == 0 {
		t.Fatal("setup: the stitch has no seam bars")
	}
	union := append(append([]market.Kline(nil), stitched...), seam...)
	sort.Slice(union, func(i, j int) bool { return union[i].OpenTime < union[j].OpenTime })
	ref := mentor.New(at.mentorEval.Cfg)
	mentor.SeedFull(ref, union, full1h, now.UnixMilli())
	b := mentor.New(at.mentorEval.Cfg)
	mentor.SeedFull(b, stitched, full1h, now.UnixMilli())
	return ref.State.Swing.Line, ref.State.Swing.EmaCount, b.State.Swing.Line
}

// The value half (reviewer B, round 1): not stale is not enough — the seam
// bucket's close must be FOLDED, so the production seed's line equals a full
// recompute on the union of stored minutes.
func TestMentorSeedAtStart_RollSeamBucket_LineEqualsUnionRecompute(t *testing.T) {
	at, now, _, _ := m456Seed(t, true)
	want, wantN, bare := m456UnionLine(t, at, now)
	if math.Abs(want-bare) < 0.1 {
		t.Fatalf("setup: the seam bucket must move the line (union %.4f vs stitched-only %.4f)", want, bare)
	}
	sw := at.mentorEval.State.Swing
	if sw.EmaCount != wantN || math.Abs(sw.Line-want) > 1e-9 {
		t.Fatalf("seeded line %.6f (n=%d) != union recompute %.6f (n=%d): the seam bucket's fold is missing", sw.Line, sw.EmaCount, want, wantN)
	}
}

// ...and the same through the re-seed after a refill.
func TestMentorReseedOnRefill_RollSeamBucket_LineEqualsUnionRecompute(t *testing.T) {
	at, now, hf, ht := m456Seed(t, false)
	bh := store.NewBarHistoryStore(at.store.GormDB())
	if err := bh.InsertBars(r4Rows("MNQ 12-26", store.BarSourceHistorical, m456SeamPrice, hf, ht, nil)); err != nil {
		t.Fatal(err)
	}
	ntTrader.NoteHistoryRefill("MNQ", "1m", 240, "m456 refill of the roll-seam bucket")
	at.mentorEvalMu.Lock()
	at.mentorReseedOnRefill(now.UnixMilli())
	at.mentorEvalMu.Unlock()
	want, wantN, bare := m456UnionLine(t, at, now)
	if math.Abs(want-bare) < 0.1 {
		t.Fatalf("setup: the seam bucket must move the line (union %.4f vs stitched-only %.4f)", want, bare)
	}
	sw := at.mentorEval.State.Swing
	if sw.LineStale || sw.EmaCount != wantN || math.Abs(sw.Line-want) > 1e-9 {
		t.Fatalf("re-seeded line %.6f (n=%d, stale=%v) != union recompute %.6f (n=%d)", sw.Line, sw.EmaCount, sw.LineStale, want, wantN)
	}
}
