package trader

import (
	"compress/gzip"
	"encoding/csv"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/store"
)

// TestLL5SeedNeverStitchesOntoAContractTheBotDoesNotTrade — levels audit LL-5
// follow-up (reviewer P2). ContractSpans orders contracts by their FIRST 1m
// row, so the NEXT contract's sparse pre-roll rows (imports, NT8 backfill,
// the 09-11 overnight 12-26 run) make it the "newest" span days before the
// bot trades it. LL-5 then measured a pre-dense gap and back-adjusted the whole
// history onto the next contract's scale (~+291 pts on 09-14) while the tick
// and LastNBarsCurrentContract still read MNQ 09-26 — every seeded key level,
// EMA and trigger line ~291 pts off the bars the evaluator ticks on, with no
// refusal.
//
// Fixture: the real MNQ 09-26 / 12-26 1m + native 1h rows as AllBarsOn reads
// them (kernel/mentor LL-5 fixture, exported read-only from data.db). Through
// the PRODUCTION seed read (mentorFullKeyLevelHistory):
//   - as of 09-14 10:00 CT the newest closed 1m bar is MNQ 09-26's (09:59) —
//     the seed's newest contract must be MNQ 09-26, no gap onto 12-26, and the
//     stitched tail must be that 09-26 bar on its own scale;
//   - as of 09-14 17:00 CT MNQ 12-26 IS the traded contract — LL-5's pre-dense
//     gap measurement must still stitch MNQ 09-26 onto it.
func TestLL5SeedNeverStitchesOntoAContractTheBotDoesNotTrade(t *testing.T) {
	ct, _ := time.LoadLocation("America/Chicago")
	f, err := os.Open("../kernel/mentor/testdata/ll5_mnq_sep_dec_1m1h_asof_20260914_1700ct.csv.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(zr).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	load := func(asOf int64) (*AutoTrader, *store.BarHistoryStore, store.BarHistoryDB) {
		at := seedStateTrader(t)
		if at.mentorEval == nil {
			at.mentorEval = mentor.New(at.mentorEvaluatorConfig())
		}
		bh := store.NewBarHistoryStore(at.store.GormDB())
		if err := bh.Migrate(); err != nil {
			t.Fatal(err)
		}
		var rows []store.BarHistoryDB
		var last1m store.BarHistoryDB
		for _, r := range recs {
			ms, _ := strconv.ParseInt(r[2], 10, 64)
			span := int64(60_000)
			if r[1] == "1h" {
				span = 3_600_000
			}
			if ms+span > asOf {
				continue // not closed by asOf
			}
			pf := func(i int) float64 { v, _ := strconv.ParseFloat(r[i], 64); return v }
			row := store.BarHistoryDB{Symbol: "MNQ", Contract: r[0], TF: r[1], OpenTimeMs: ms, O: pf(3), H: pf(4), L: pf(5), C: pf(6), Source: store.BarSourceHistorical}
			rows = append(rows, row)
			if r[1] == "1m" && ms > last1m.OpenTimeMs {
				last1m = row
			}
		}
		if err := bh.InsertBars(rows); err != nil {
			t.Fatal(err)
		}
		return at, bh, last1m
	}

	// 1) 09-14 10:00 CT: the bot trades MNQ 09-26; MNQ 12-26 already has rows.
	asOf := time.Date(2026, 9, 14, 10, 0, 0, 0, ct).UnixMilli()
	at, bh, last := load(asOf)
	cur, err := bh.LastNBarsCurrentContract("MNQ", "1m", 1)
	if err != nil || len(cur) != 1 || cur[0].Contract != "MNQ 09-26" {
		t.Fatalf("fixture: the traded contract at 10:00 CT must be MNQ 09-26 (got %+v err %v)", cur, err)
	}
	spans, _ := bh.ContractSpans("MNQ", "1m")
	if len(spans) != 2 || spans[len(spans)-1].Contract != "MNQ 12-26" {
		t.Fatalf("fixture: MNQ 12-26 must be the span with the latest FIRST row (the bug's trigger), got %+v", spans)
	}
	stitched, full1H, contracts, gaps, stopped, _ := at.mentorFullKeyLevelHistory(bh)
	t.Logf("10:00 CT: %s", mentor.KeyLevelHistoryLine(contracts, full1H, gaps, stopped))
	if len(contracts) == 0 || contracts[len(contracts)-1].Contract != "MNQ 09-26" {
		names := []string{}
		for _, c := range contracts {
			names = append(names, c.Contract)
		}
		t.Errorf("the seed's newest contract must be the TRADED MNQ 09-26, got %v", names)
	}
	for _, g := range gaps {
		if strings.HasSuffix(g.Pair, "→MNQ 12-26") {
			t.Errorf("no history may be back-adjusted onto MNQ 12-26 while the bot trades MNQ 09-26: gap %+v", g)
		}
	}
	if n := len(stitched); n == 0 || stitched[n-1].OpenTime != last.OpenTimeMs || stitched[n-1].Close != last.C {
		var got string
		if n > 0 {
			got = time.UnixMilli(stitched[n-1].OpenTime).In(ct).Format("15:04") + " C " + strconv.FormatFloat(stitched[n-1].Close, 'f', 2, 64)
		}
		t.Errorf("the stitched 1m tail must be the traded bar %s C %.2f on its own scale, got %q",
			time.UnixMilli(last.OpenTimeMs).In(ct).Format("15:04"), last.C, got)
	}

	// 2) 09-14 17:00 CT: MNQ 12-26 is now the traded contract (pre-dense) — the
	// LL-5 measurement must still stitch MNQ 09-26 onto it.
	asOf = time.Date(2026, 9, 14, 17, 0, 0, 0, ct).UnixMilli()
	at, bh, _ = load(asOf)
	cur, _ = bh.LastNBarsCurrentContract("MNQ", "1m", 1)
	if len(cur) != 1 || cur[0].Contract != "MNQ 12-26" {
		t.Fatalf("fixture: the traded contract at 17:00 CT must be MNQ 12-26 (got %+v)", cur)
	}
	_, full1H, contracts, gaps, stopped, _ = at.mentorFullKeyLevelHistory(bh)
	t.Logf("17:00 CT: %s", mentor.KeyLevelHistoryLine(contracts, full1H, gaps, stopped))
	if stopped != "" || len(gaps) != 1 || !gaps[0].PreDense || gaps[0].Pair != "MNQ 09-26→MNQ 12-26" {
		t.Errorf("LL-5: the pre-dense traded MNQ 12-26 must still stitch MNQ 09-26 (stopped %q gaps %+v)", stopped, gaps)
	}
}
