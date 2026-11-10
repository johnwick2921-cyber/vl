package trader

import (
	"encoding/csv"
	"os"
	"strconv"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/store"
)

// TestLL4PreRollKeyLevelCandlesReadTheCurrentContractsOwnBars — levels audit
// LL-4 (P2). The mentor draws on MNQZ2026's own chart back to late July
// [OctD5.2-R16 @06:18] (27,817.75 among his lines). Fixture (exported
// read-only, as stored): MNQ 09-26 1m import rows of 07-29 RTH + both
// contracts' native 1h of the 07-29 session (the roll-gap measurement), MNQ
// 12-26's native 30m of 07-29 RTH and a few of its own 1m (09-11). Through the
// PRODUCTION seed read (mentorFullKeyLevelHistory) the 07-29 10:30 1H RTH
// candle must be MNQ 12-26's own: O 27,825.50 C 27,831.25 — GREEN, a key level
// (the mentor's 27,817.75). Stitched (Sep + one gap) it is RED: no level.
// Knob key_level_history_source=stitched keeps the pre-LL-4 candle.
func TestLL4PreRollKeyLevelCandlesReadTheCurrentContractsOwnBars(t *testing.T) {
	ct, _ := time.LoadLocation("America/Chicago")
	at1030 := time.Date(2026, 7, 29, 10, 30, 0, 0, ct).UnixMilli()
	run := func(src string) (o, c float64, ok bool) {
		at := seedStateTrader(t)
		if at.mentorEval == nil {
			at.mentorEval = mentor.New(at.mentorEvaluatorConfig())
		}
		at.mentorEval.Cfg.KeyLevelHistorySource = src
		bh := store.NewBarHistoryStore(at.store.GormDB())
		if err := bh.Migrate(); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open("testdata/ll4_mnq_sep_dec_20260729.csv")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		recs, err := csv.NewReader(f).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		var imp, other []store.BarHistoryDB
		for _, r := range recs {
			ms, _ := strconv.ParseInt(r[2], 10, 64)
			pf := func(i int) float64 { v, _ := strconv.ParseFloat(r[i], 64); return v }
			row := store.BarHistoryDB{Symbol: "MNQ", Contract: r[0], TF: r[1], OpenTimeMs: ms, O: pf(3), H: pf(4), L: pf(5), C: pf(6), V: pf(7), Source: r[8], Convention: r[9]}
			if row.Source == store.BarSourceHistoricalImport {
				imp = append(imp, row)
			} else {
				other = append(other, row)
			}
		}
		if _, _, err := bh.ImportBars(imp); err != nil {
			t.Fatal(err)
		}
		if err := bh.InsertBars(other); err != nil {
			t.Fatal(err)
		}
		_, full1H, _, gaps, stopped, _ := at.mentorFullKeyLevelHistory(bh)
		if len(gaps) != 1 {
			t.Fatalf("fixture: the Sep→Dec gap must be measured (gaps %+v stopped %q)", gaps, stopped)
		}
		for _, k := range full1H {
			if k.OpenTime == at1030 {
				return k.Open, k.Close, true
			}
		}
		return 0, 0, false
	}
	o, c, ok := run("") // default: own
	if !ok || o != 27825.50 || c != 27831.25 {
		t.Errorf("own (default): the 07-29 10:30 candle must be MNQ 12-26's own O 27825.50 C 27831.25 (green, the mentor's 27,817.75), got O %.2f C %.2f ok=%v", o, c, ok)
	}
	o, c, ok = run(mentor.KeyLevelHistoryStitched)
	if !ok || c >= o {
		t.Errorf("stitched: the 07-29 10:30 candle is the back-adjusted MNQ 09-26 one (red), got O %.2f C %.2f ok=%v", o, c, ok)
	}
	t.Logf("stitched 07-29 10:30: O %.2f C %.2f", o, c)
}
