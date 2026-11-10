package mentor

import (
	"compress/gzip"
	"encoding/csv"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// levels audit (LL-*) — fixtures exported READ-ONLY from data.db:
//
//   - testdata/ll_mnq_1hrth_live_20260720_to_20261103.csv: the 08:30-anchored
//     1H RTH candles the LIVE seed walked as of 2026-11-03 17:00 CT (the 11-04
//     seed), from 07-20 on — the production stitch (mentor.StitchKeyLevelHistory
//     over every contract, MNQ 09-26 back-adjusted +291.0) of the rows exactly
//     as store.AllBarsOn returned them before LL-3 (close-stamped imports raw).
//     Columns: open_ms, o, h, l, c, close_ms. The full-history seed line from
//     the same read is "key levels 28 live · 68 deleted" (the finding's count).
//   - testdata/ll1_mnq_1m_20261029_1230ct.csv: MNQ 12-26 1m, 10-29 12:30–15:14 CT.
func llLoad1H(t *testing.T, p string) []market.Kline {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	out := make([]market.Kline, 0, len(rs))
	for _, r := range rs {
		pi := func(i int) int64 { v, _ := strconv.ParseInt(r[i], 10, 64); return v }
		pf := func(i int) float64 { v, _ := strconv.ParseFloat(r[i], 64); return v }
		out = append(out, market.Kline{OpenTime: pi(0), Open: pf(1), High: pf(2), Low: pf(3), Close: pf(4), CloseTime: pi(5), Final: true})
	}
	return out
}

func llCT(y int, mo time.Month, d, h, mi int) int64 {
	return time.Date(y, mo, d, h, mi, 0, 0, ctime()).UnixMilli()
}

// llMentorLines are the mentor's October lines (frame s_00333, 20:38 CT 11-03
// [OctD5.2-R13/R14/R16 @04:10–07:12]) the finding matched to their candle, plus
// the WE3 quiz level (07-28 09:30) [OctD5.2-R19/R20 @07:20, 07:42; WE3 @09:12].
// Every one of them was DELETED by the deck rule before the 11-04 seed.
var llMentorLines = []struct {
	name string
	at   int64
}{
	{"28,302.00 (10-30 13:30)", llCT(2026, 10, 30, 13, 30)},
	{"28,703.00 (10-30 10:30)", llCT(2026, 10, 30, 10, 30)},
	{"29,466.50 (10-29 13:30)", llCT(2026, 10, 29, 13, 30)},
	{"29,549.50 (10-26 10:30)", llCT(2026, 10, 26, 10, 30)},
	{"29,259.00 (10-23 10:30)", llCT(2026, 10, 23, 10, 30)},
	{"29,339.75 (10-22 11:30)", llCT(2026, 10, 22, 11, 30)},
	{"29,116.00 (10-22 13:30)", llCT(2026, 10, 22, 13, 30)},
	{"29,187.50 (10-23 13:30)", llCT(2026, 10, 23, 13, 30)},
	{"29,667.25 (10-20 14:30)", llCT(2026, 10, 20, 14, 30)},
	{"27,997.75 WE3 (07-28 09:30)", llCT(2026, 7, 28, 9, 30)},
}

func llLevelAt(ls []Level, at int64) (Level, bool) {
	for _, l := range ls {
		if l.Kind == KindKeyLevel && l.AtTime == at {
			return l, true
		}
	}
	return Level{}, false
}

// TestLL1OctoberKeepsKeyLevelsThroughA1HBodyClose — levels audit LL-1 (P1).
// October: the mentor walks the 1h RTH chart and draws a line at every colour
// change, "vẽ bao nhiêu tùy thích… Key Level nó là quá khứ" [OctD5.2-R13..R16
// @04:10–07:12, R15 @04:44] and trades the 11-03 19:08 retest of 27,997.75, a
// level a 1H body crossed on 07-29 [WE3 @09:12–10:05]. The deck's "nến 1H đóng
// cửa XUYÊN level đó … xóa level đó đi" (slide 31 step 4) is September.
// Production SeedFull over the live 11-04 seed candles: by default every line
// above is LIVE; the knob key_level_delete_on_1h_close=on restores the deck.
func TestLL1OctoberKeepsKeyLevelsThroughA1HBodyClose(t *testing.T) {
	candles := llLoad1H(t, "testdata/ll_mnq_1hrth_live_20260720_to_20261103.csv")
	now := llCT(2026, 11, 3, 17, 0)
	for _, mode := range []string{"", KeyLevelDeleteOn1HCloseOff, KeyLevelDeleteOn1HCloseOn} {
		cfg := DefaultConfig()
		cfg.Enabled = true
		if mode != "" {
			cfg.KeyLevelDeleteOn1HClose = mode
		}
		e := New(cfg)
		SeedFull(e, nil, candles, now)
		for _, ml := range llMentorLines {
			l, ok := llLevelAt(e.State.SeedLevels, ml.at)
			if !ok {
				t.Fatalf("mode %q: no seeded level at %s — the walk must draw it", mode, ml.name)
			}
			deleted := e.State.DeletedLevels[l.Key]
			wantDeleted := mode == KeyLevelDeleteOn1HCloseOn
			if deleted != wantDeleted {
				t.Errorf("mode %q: %s (bot %.2f) deleted=%v, want %v", mode, ml.name, l.Price, deleted, wantDeleted)
			}
		}
		live, del := e.keyLevelCounts()
		t.Logf("mode %q: key levels %d live · %d deleted", mode, live, del)
	}
}

// TestLL1TickKeepsTheLevelThe1430BodyCrossed — the per-tick path
// (advanceKeyLevelDeletion via the production Tick): seed at 10-29 14:30 CT with
// the candles to the 13:30 one, then tick the real 1m bars 14:30–15:14. The
// 14:30 candle (O 29,494.25 C 29,182.25) closes through the 13:30 level
// (29,471.50, the mentor's 29,466.50). Default: the level is still in the set
// after the 15:15 close; knob on: it is deleted and leaves the set.
func TestLL1TickKeepsTheLevelThe1430BodyCrossed(t *testing.T) {
	all := llLoad1H(t, "testdata/ll_mnq_1hrth_live_20260720_to_20261103.csv")
	seedAt := llCT(2026, 10, 29, 14, 30)
	var candles []market.Kline
	for _, c := range all {
		if c.OpenTime < seedAt {
			candles = append(candles, c)
		}
	}
	bars := m413Load(t, "testdata/ll1_mnq_1m_20261029_1230ct.csv")
	lvlAt := llCT(2026, 10, 29, 13, 30)
	for _, mode := range []string{KeyLevelDeleteOn1HCloseOff, KeyLevelDeleteOn1HCloseOn} {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.KeyLevelDeleteOn1HClose = mode
		e := New(cfg)
		var pre []market.Kline
		for _, b := range bars {
			if b.OpenTime < seedAt {
				pre = append(pre, b)
			}
		}
		SeedFull(e, pre, candles, seedAt)
		l, ok := llLevelAt(e.State.SeedLevels, lvlAt)
		if !ok || e.State.DeletedLevels[l.Key] {
			t.Fatalf("mode %s: the 13:30 level must be seeded live (ok=%v)", mode, ok)
		}
		for i := range bars {
			if bars[i].OpenTime < seedAt {
				continue
			}
			e.Tick(bars[:i+1], bars[i].CloseTime+1)
		}
		if got := bars[len(bars)-1]; got.CloseTime+1 != llCT(2026, 10, 29, 15, 15) {
			t.Fatalf("fixture must end on the 15:14 bar, ends %d", got.OpenTime)
		}
		_, inSet := llLevelAt(e.State.Levels, lvlAt)
		deleted := e.State.DeletedLevels[l.Key]
		if mode == KeyLevelDeleteOn1HCloseOff && (deleted || !inSet) {
			t.Errorf("default (October): the 13:30 level %.2f must survive the 14:30 body close (deleted=%v inSet=%v)", l.Price, deleted, inSet)
		}
		if mode == KeyLevelDeleteOn1HCloseOn && (!deleted || inSet) {
			t.Errorf("deck knob on: the 14:30 body close must delete the 13:30 level (deleted=%v inSet=%v)", deleted, inSet)
		}
	}
}

// TestLL6EMA34AliasIsInvalidatedOnce — levels audit LL-6 (P3). The EMA 34 of an
// intraday trade is ONE line, the 1m EMA 34 [OctD5.2-R9 @03:37]; with the
// default EMALocationTFMinutes = EMATFMinutes = 1 the location-gate line
// ema34_htf IS the ema34 line (emaLocationLevel returns State.EMA34). Production
// Tick over the real 11-01 evening (MNQ 12-26 1m, seeded at 17:00 CT from
// 10-30): every wrong-way first touch of that one line must be ONE LevelInvalid
// intent (on ema34_htf, the location-gate key) and ONE
// level_invalid_first_touch refusal — never an ema34 twin of the same candle.
func TestLL6EMA34AliasIsInvalidatedOnce(t *testing.T) {
	bars := m413Load(t, "testdata/ll6_mnq_1m_20261030_0800ct.csv")
	seedAt := llCT(2026, 11, 1, 17, 0)
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	var pre []market.Kline
	for _, b := range bars {
		if b.OpenTime < seedAt {
			pre = append(pre, b)
		}
	}
	Seed(e, pre, seedAt)
	inv := map[string][]string{}
	refused, invalids := 0, 0
	for i := range bars {
		if bars[i].OpenTime < seedAt {
			continue
		}
		before := e.State.Refusals["level_invalid_first_touch"]
		for _, in := range e.Tick(bars[:i+1], bars[i].CloseTime+1) {
			if in.Action != LevelInvalid {
				continue
			}
			invalids++
			inv[in.LevelKey] = append(inv[in.LevelKey], time.UnixMilli(bars[i].OpenTime).In(ctime()).Format("15:04"))
		}
		refused += e.State.Refusals["level_invalid_first_touch"] - before
	}
	htf := inv[string(KindEMA34HTF)]
	if len(htf) < 2 || htf[0] != "17:28" {
		t.Fatalf("fixture: want the 17:28 and 18:30 ema34_htf invalidations, got %v", htf)
	}
	if n := len(inv[string(KindEMA34)]); n != 0 {
		t.Errorf("ema34 is the SAME line as ema34_htf (EMA TF 1 = location TF 1): %d duplicate LevelInvalid intents at %v (ema34_htf at %v)", n, inv[string(KindEMA34)], htf)
	}
	if refused != invalids {
		t.Errorf("level_invalid_first_touch counted %d for %d emitted LevelInvalid intents — one invalidation, one count", refused, invalids)
	}
	t.Logf("LevelInvalid by key: %v (refusals %d)", inv, refused)
}

// TestLL2BreakThenFirstRetestArmsTheRoleReversal — levels audit LL-2 (P1).
// OctD5.2-R24 [Oct D5.2 @13:35], the worked example at 28,154.50 on 11-03: the
// red 18:26 candle (O 28,176.50 H 28,178.00 L 28,133.00 C 28,135.50) closes
// BELOW the support, then "nó quay ngược lên, nó bắt test… vẫn là nến chạm
// level… Thứ 2 nữa là nến đóng dưới" — the level is now resistance
// [R20 @07:42]; the sell stop goes on the FIRST retest candle that touches
// [R25 @14:16]; the only checks are touch, then where it closes [R22/R23
// @12:16, 12:29]. Production SeedFull (the live 11-04 seed candles: the bot's
// copy of the level is 28,155.25, the 07-30 08:30 open) + Tick over the real
// MNQ 12-26 1m of 11-03 17:00–18:40 CT. Default: the 18:26 break flips the
// level (counter level_broken_retest, no ISB-only), and the 18:29 retest
// (H 28,161.25 C 28,149.75) is the first touch from below — a reject, so the
// level path reads it as the PLH reference candle. Knob off: ISB-only all day
// (the September "first touch closed through → invalid" [D5.2 p1 @19:51]).
func TestLL2BreakThenFirstRetestArmsTheRoleReversal(t *testing.T) {
	candles := llLoad1H(t, "testdata/ll_mnq_1hrth_live_20260720_to_20261103.csv")
	bars := m413Load(t, "testdata/ll2_mnq_1m_20261102_0800ct.csv")
	seedAt := llCT(2026, 11, 3, 17, 0)
	lvlAt := llCT(2026, 7, 30, 8, 30)
	phl1829 := map[string]int{}
	run := func(mode string) (e *Evaluator, key string, invalidAt []string, refAt string, refFrom Side, outcome TouchOutcome, ins []Intent) {
		cfg := DefaultConfig()
		cfg.Enabled = true
		if mode != "" {
			cfg.KeyLevelBreakRetest = mode
		}
		e = New(cfg)
		var pre []market.Kline
		for _, b := range bars {
			if b.OpenTime < seedAt {
				pre = append(pre, b)
			}
		}
		SeedFull(e, pre, candles, seedAt)
		l, ok := llLevelAt(e.State.SeedLevels, lvlAt)
		if !ok || l.Price != 28155.25 {
			t.Fatalf("fixture: the 07-30 08:30 level must be seeded at 28155.25, got %+v ok=%v", l, ok)
		}
		key = l.Key
		for i := range bars {
			if bars[i].OpenTime < seedAt {
				continue
			}
			hm := time.UnixMilli(bars[i].OpenTime).In(ctime()).Format("15:04")
			before := map[string]int{}
			for k, v := range e.State.Refusals {
				before[k] = v
			}
			tickOut := e.Tick(bars[:i+1], bars[i].CloseTime+1)
			if hm == "18:29" || hm == "18:30" {
				for k, v := range e.State.Refusals {
					if d := v - before[k]; d != 0 {
						t.Logf("mode %q %s refusal %s +%d", mode, hm, k, d)
						if hm == "18:29" && strings.HasPrefix(k, "phl_") {
							phl1829[mode] += d
						}
					}
				}
			}
			for _, in := range tickOut {
				if in.LevelKey != key {
					continue
				}
				if in.Action == LevelInvalid {
					invalidAt = append(invalidAt, hm)
				}
				ins = append(ins, in)
			}
			if tr := e.State.Touches[key]; tr.Outcome == TouchReject && refAt == "" {
				refAt = time.UnixMilli(tr.RefBar.OpenTime).In(ctime()).Format("15:04")
				refFrom, outcome = tr.ApproachedFrom, tr.Outcome
			}
		}
		return
	}
	e, key, invalid, refAt, refFrom, _, ins := run("")
	if e.State.ISBOnly[key] {
		t.Errorf("default (October): the 18:26 break must flip 28155.25, not make it ISB-only; LevelInvalid at %v", invalid)
	}
	if got := e.State.Refusals["level_broken_retest"] + e.State.Counters["level_broken_retest"]; got < 1 {
		t.Errorf("default: the break must be counted level_broken_retest (got %d)", got)
	}
	if refAt != "18:29" || refFrom != SideShort {
		t.Errorf("default: the first retest touch must be the 18:29 candle from below (resistance) as a reject, got ref %q from %q (intents %+v)", refAt, refFrom, ins)
	}
	// The retest now reaches the PLH gates: the 18:29 reference candle is 40
	// pts tall (H 28,161.25 L 28,121.25), so the PLH is named by the ordinary
	// stop ceiling — a gate, not the silent ISB-only of the September read.
	if phl1829[""] < 1 {
		t.Errorf("default: the 18:29 retest must reach the PLH gates (a named phl_* verdict), got none; intents %+v", ins)
	}
	e, key, invalid, refAt, _, _, _ = run(KeyLevelBreakRetestOff)
	if !e.State.ISBOnly[key] || len(invalid) != 1 || invalid[0] != "18:26" || refAt != "" {
		t.Errorf("knob off (September): the 18:26 first touch closing through must make the level ISB-only, no retest read; ISBOnly=%v invalid=%v ref=%q", e.State.ISBOnly[key], invalid, refAt)
	}
	if phl1829[KeyLevelBreakRetestOff] != 0 {
		t.Errorf("knob off: no PLH read at the ISB-only level (got %d phl_* verdicts at 18:29)", phl1829[KeyLevelBreakRetestOff])
	}
}

// TestLL5SeedBeforeTheFirstDenseSessionStillStitches — levels audit LL-5 (P2).
// Draw levels going back months; there is no reset at a contract roll
// [OctD5.2-R13/R14/R16 @04:10–06:40]. As of 09-14 17:00 CT (the 09-15 seed)
// MNQ 12-26 has no session day with >= 1000 1m bars yet, so NewRollStitcher
// stopped at once ("stopped at MNQ 12-26: no measured gap", 6 1H RTH candles,
// "key levels 1 live" in the replay). Fixture: the real MNQ 09-26 / 12-26 1m +
// native 1h rows from 09-08 to 09-14 17:00 CT as AllBarsOn reads them (imports
// open-stamped), exported read-only. Production StitchKeyLevelHistory must
// measure the Sep→Dec gap on the last overlapping native-1h evening and stitch
// the Sep RTH candles.
func TestLL5SeedBeforeTheFirstDenseSessionStillStitches(t *testing.T) {
	f, err := os.Open("testdata/ll5_mnq_sep_dec_1m1h_asof_20260914_1700ct.csv.gz")
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
	cs := map[string]*Contract1M{"MNQ 09-26": {Contract: "MNQ 09-26"}, "MNQ 12-26": {Contract: "MNQ 12-26"}}
	for _, r := range recs {
		ms, _ := strconv.ParseInt(r[2], 10, 64)
		pf := func(i int) float64 { v, _ := strconv.ParseFloat(r[i], 64); return v }
		span := int64(60_000)
		if r[1] == "1h" {
			span = 3_600_000
		}
		k := market.Kline{OpenTime: ms, Open: pf(3), High: pf(4), Low: pf(5), Close: pf(6), CloseTime: ms + span - 1}
		if r[1] == "1h" {
			cs[r[0]].Bars1H = append(cs[r[0]].Bars1H, k)
		} else {
			cs[r[0]].Bars = append(cs[r[0]].Bars, k)
		}
	}
	_, b1h, gaps, stopped := StitchKeyLevelHistory([]Contract1M{*cs["MNQ 09-26"], *cs["MNQ 12-26"]})
	t.Logf("%s", KeyLevelHistoryLine([]Contract1M{*cs["MNQ 09-26"], *cs["MNQ 12-26"]}, b1h, gaps, stopped))
	if stopped != "" || len(gaps) != 1 {
		t.Fatalf("the Sep→Dec roll must be stitched before Dec's first dense session: stopped=%q gaps=%+v (%d 1H RTH candles)", stopped, gaps, len(b1h))
	}
	if g := gaps[0].Gap; g < 280 || g > 300 {
		t.Errorf("the measured Sep→Dec gap %.2f is not the ~291 roll basis (09-11 evening 291.0)", g)
	}
	if len(b1h) < 30 {
		t.Errorf("want the 09-08..09-14 RTH candles stitched (>= 30), got %d", len(b1h))
	}
}
