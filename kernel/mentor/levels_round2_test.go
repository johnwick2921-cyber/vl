package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestD3G15KeyLevelRearmsAfterDeparture — oct round 2 D3-G15 (OctD3.2-R78
// [@ 1:00:00]: wait for the run, the retest of the level and a new close on the
// right side, then enter — "changed" vs the September "once invalid, only inside
// bars" [D5.2 p2 @20:48]). Production SeedFull + Tick over the real MNQ 12-26
// 1m tape of 11-03 17:00–18:40 CT (the LL-2 fixture: the 18:26 candle breaks
// 28,155.25, the one flip of the day is spent, 18:29 is the reject). The tape
// stops at 18:40, so the SECOND wrong-way close and the run/retest that follow
// are the labelled SYNTHETIC continuation below (the course's own sketch, a
// trendline, is path 28,786 → ~28,774 → 28,795 — no key-level tape has it):
//
//	18:41 closes ABOVE the level (second wrong-way close → ISB-only, flip spent)
//	18:42-18:43 stay above / fall back, 18:44 never touches (the run away),
//	18:45 retests from below and closes back below (a reject).
//
// Default (off, today): the level stays ISB-only all day, the 18:45 retest is
// never read. key_level_rearm=on: the departure at 18:44 clears the invalidity
// and the 18:45 retest is a fresh first touch (a reject, ISB-only gone).
func TestD3G15KeyLevelRearmsAfterDeparture(t *testing.T) {
	candles := llLoad1H(t, "testdata/ll_mnq_1hrth_live_20260720_to_20261103.csv")
	bars := m413Load(t, "testdata/ll2_mnq_1m_20261102_0800ct.csv")
	seedAt := llCT(2026, 11, 3, 17, 0)
	lvlAt := llCT(2026, 7, 30, 8, 30)
	last := bars[len(bars)-1]
	synth := []struct{ o, h, l, c float64 }{
		{28082.25, 28170, 28080, 28165}, // 18:41 second wrong-way close (above)
		{28165, 28180, 28160, 28172},    // 18:42 still above
		{28172, 28175, 28100, 28105},    // 18:43 falls back (still touches: high >= level)
		{28105, 28120, 28085, 28100},    // 18:44 the run away: no touch
		{28100, 28160, 28095, 28120},    // 18:45 the retest: touches, closes back below
	}
	for i, s := range synth {
		ot := last.OpenTime + int64(i+1)*60_000
		bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: s.o, High: s.h, Low: s.l, Close: s.c, Volume: 1000})
	}
	run := func(mode string) (isbAt41 bool, isbAt45 bool, outcome45 TouchOutcome, ref45 string) {
		cfg := DefaultConfig()
		cfg.Enabled = true
		if mode != "" {
			cfg.KeyLevelRearm = mode
		}
		e := New(cfg)
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
		for i := range bars {
			if bars[i].OpenTime < seedAt {
				continue
			}
			hm := time.UnixMilli(bars[i].OpenTime).In(ctime()).Format("15:04")
			e.Tick(bars[:i+1], bars[i].CloseTime+1)
			switch hm {
			case "18:41":
				isbAt41 = e.State.ISBOnly[l.Key]
			case "18:45":
				isbAt45 = e.State.ISBOnly[l.Key]
				tr := e.State.Touches[l.Key]
				outcome45 = tr.Outcome
				if tr.RefBar.OpenTime != 0 {
					ref45 = time.UnixMilli(tr.RefBar.OpenTime).In(ctime()).Format("15:04")
				}
			}
		}
		return
	}
	for _, mode := range []string{"", KeyLevelRearmOff} {
		at41, at45, out45, _ := run(mode)
		if !at41 {
			t.Fatalf("precondition (mode %q): the 18:41 second wrong-way close must make the level ISB-only (the 18:26 flip is spent)", mode)
		}
		if !at45 || out45 == TouchReject {
			t.Errorf("default/off (today): the level stays ISB-only all day and the 18:45 retest is never read, got ISBOnly=%v outcome=%q", at45, out45)
		}
	}
	at41, at45, out45, ref := run(KeyLevelRearmOn)
	if !at41 {
		t.Fatalf("precondition (on): the 18:41 second wrong-way close must make the level ISB-only")
	}
	if at45 || out45 != TouchReject || ref != "18:45" {
		t.Errorf("key_level_rearm=on: the departure at 18:44 clears the invalidity and 18:45 is a fresh reject; got ISBOnly=%v outcome=%q ref=%q", at45, out45, ref)
	}
}
