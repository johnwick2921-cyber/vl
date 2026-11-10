package mentor

import (
	"encoding/csv"
	"os"
	"strconv"
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
