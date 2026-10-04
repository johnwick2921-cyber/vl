package trader

import (
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/store"
)

// A seeded 1H candle must close at open + 1h − 1ms like a live one. With
// open + 60_000 on the 1m rows the candle BEFORE a level's own candle "closed"
// exactly at the level's AtTime, so the 1H body-cross deletion tested it too
// and deleted the level whenever the level opened inside that body.
//
// Store-shaped fixture (one RTH day, 1m rows): a red 08:30 candle 15100→15080,
// then a green 09:30 candle that GAPS UP and opens at 15090 — inside the red
// body — so the colour-change level 15090 sits strictly inside the previous
// candle's body. Every later candle stays above it, so nothing legitimately
// deletes the level. Mutant: restore +60_000 in storeBarsToKlines → RED.
func TestMentorSeedBarCloseTimeDoesNotDeleteLevelByPriorCandle(t *testing.T) {
	ct, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	day := func(h, m int) int64 { return time.Date(2026, time.September, 16, h, m, 0, 0, ct).UnixMilli() }
	hours := []struct {
		h, m        int
		open, close float64
	}{
		{8, 30, 15100, 15080}, // red
		{9, 30, 15090, 15110}, // green, opens inside the red body → level 15090
		{10, 30, 15110, 15120},
		{11, 30, 15120, 15130},
		{12, 30, 15130, 15140},
		{13, 30, 15140, 15150},
		{14, 30, 15150, 15160}, // clipped at 15:00 CT
	}
	var rows []store.BarHistoryDB
	for _, hr := range hours {
		span := 60
		if hr.h == 14 {
			span = 30
		}
		for i := 0; i < span; i++ {
			o := hr.open + (hr.close-hr.open)*float64(i)/60
			c := hr.open + (hr.close-hr.open)*float64(i+1)/60
			hi, lo := o, c
			if c > o {
				hi, lo = c, o
			}
			rows = append(rows, store.BarHistoryDB{
				Symbol: "MNQ", TF: "1m", Contract: "MNQ 12-26", Source: store.BarSourceLive,
				OpenTimeMs: day(hr.h, hr.m) + int64(i)*60_000, O: o, H: hi, L: lo, C: c, V: 1,
			})
		}
	}

	st := mentorSeedStore(t)
	if err := store.NewBarHistoryStore(st.GormDB()).InsertBars(rows); err != nil {
		t.Fatalf("InsertBars: %v", err)
	}
	at := &AutoTrader{
		id: "t-seed-closetime",
		config: AutoTraderConfig{
			StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}},
		},
		store: st,
	}
	wireMentorPlacementSeams(t)
	at.mentorSeedAtStart()

	var lvl *mentor.Level
	for i := range at.mentorEval.State.SeedLevels {
		if l := &at.mentorEval.State.SeedLevels[i]; l.Price == 15090 {
			lvl = l
		}
	}
	if lvl == nil {
		t.Fatalf("seeded level set has no 15090 level: %+v", at.mentorEval.State.SeedLevels)
	}

	bars := storeBarsToKlines(rows, 60_000)
	at.mentorEval.Tick(bars, day(15, 0))
	if at.mentorEval.State.DeletedLevels[lvl.Key] {
		t.Fatalf("level %s (15090) was deleted by the 1H candle BEFORE its own: seeded candle CloseTime is open+tf, not open+tf−1", lvl.Key)
	}
	if d := bars[0].CloseTime - bars[0].OpenTime; d != 60_000-1 {
		t.Fatalf("1m CloseTime = open+%d, want open+59999", d)
	}
}
