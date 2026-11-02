package mentor

import (
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"vl/market"
)

// ── m431 (release #52) — an intent dropped ANYWHERE after its arm registered
// leaves no arm. The #51 gate audit found it on the real 2026-10-23 tape:
// ISBArm isb-13 (isb-17 in this fixture's numbering) registered at 12:22 CT,
// never emitted, sat in State.ISBArms two ticks, and a CancelArm for a
// never-authored order went out at 12:24. Cause: the deferred flag-wall
// target pass (applyFlagWallTarget, refusal flag_wall_room) dropped the entry
// AFTER the m425a S1 mid-Tick un-register pass had already run.
//
// Fixtures (kernel/mentor/testdata), dumped from the PRODUCTION replay path:
//   - m431_state_20261023_0830.json.gz: the evaluator State exactly as it stood
//     before the 08:30 CT tick of the real-day replay of 10-23;
//   - m431_window1500_1m_20261023_0829.csv.gz: the 1500-bar 1m window the
//     production tick passes (10-22 10:42 → 10-23 08:29 CT);
//   - the rest of the day is testdata/m423_mnq_1m_20261023.csv (the real
//     10-23 tape; bars after the window).

func m431LoadWindow(t *testing.T) []market.Kline {
	t.Helper()
	f, err := os.Open("testdata/m431_window1500_1m_20261023_0829.csv.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := csv.NewReader(zr).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	var out []market.Kline
	for _, r := range rs {
		ms, _ := strconv.ParseInt(r[0], 10, 64)
		pf := func(i int) float64 { v, _ := strconv.ParseFloat(r[i], 64); return v }
		out = append(out, market.Kline{OpenTime: ms, Open: pf(1), High: pf(2), Low: pf(3), Close: pf(4), Volume: pf(5), CloseTime: ms + 59_999, Final: true})
	}
	return out
}

// m431Day10_23 returns the evaluator at the 08:30 CT tick of 10-23 (production
// state, flat-near ON like production) and the full bar series: the 1500-bar
// window then the rest of the real tape.
func m431Day10_23(t *testing.T) (*Evaluator, []market.Kline, int) {
	t.Helper()
	withFlatNear(t)
	cfg := DefaultConfig()
	cfg.Enabled = true
	// m462: this pins the phantom-arm mechanics on the 10-23 tape as it ran
	// live (news-only gate). Under the m462 all-day default the 12:22 CT ISB
	// (isb-17, 4h long vs 1h short that afternoon — trade 698's conflict) is
	// refused by the 4h/1h gate before the flag wall can see it.
	cfg.HTFGateNewsOnly = true
	// m465 (#60, D5.1-R18): 10-23's pre-open run sits between 265 and 300, so
	// the new 265 line makes it a SPENT day and the 12:22 ISB is refused
	// isb_target_below_floor (the 15-pt cap). This pins the phantom-arm
	// mechanics as the day ran live: the pre-#60 300 line.
	cfg.DayGateSpentPts = 300
	e := New(cfg)
	f, err := os.Open("testdata/m431_state_20261023_0830.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(zr).Decode(&e.State); err != nil {
		t.Fatal(err)
	}
	e.seeded = true
	win := m431LoadWindow(t)
	all := append([]market.Kline(nil), win...)
	for _, b := range m423Bars(t) {
		if b.OpenTime > win[len(win)-1].OpenTime {
			b.Final = true
			all = append(all, b)
		}
	}
	return e, all, len(win)
}

// m431Tick feeds the evaluator the production window (the last 1500 bars) at
// the bar's close instant.
func m431Tick(e *Evaluator, all []market.Kline, i int) []Intent {
	lo := 0
	if i > 1500 {
		lo = i - 1500
	}
	return e.Tick(all[lo:i], BarCloseInstant(all[i-1]))
}

// TestM431NoArmWithoutAnEmittedEntryOnTheRealTape — the named RED. At every
// tick 08:30–15:58 CT of the real 10-23 tape, every arm in ISBArms / LevelArms /
// RollingPHL / the swing Pending / the EMA34 pending was either held before the
// day began or belongs to an entry that LEFT Tick. The 12:22 CT tick is the
// control: before c52 W9 the flag-wall pass refused that ISB (flag_wall_room)
// and had to leave no isb arm behind; W9 B22 now exempts ISB, so the 12:22 ISB
// leaves Tick and the control asserts exactly that. Production path: Evaluator.Tick (the deferred leave passes).
// Mutant (drop the exit pass `e.unregisterDroppedArms` from Tick's defer) →
// the isb arm dropped at 11:28 CT stays registered → RED (shown in #52 after
// W9 removed the 12:22 flag-wall drop).
//
// m466 (release #61): G43 D2.2-R71 [D2.2 p3 @14:28–15:58] now refuses that very
// 12:22 long ISB mid_range_against_rejection — the FTGH 29,258.00–29,325.75 was
// rejected at 11:50 CT and no candle has reached the floor since. The loop runs
// twice: knob ON (production) — no phantom arm, and the 12:22 drop is named and
// its arm un-registered by the same exit pass; knob OFF — the #52 control as
// before.
func TestM431NoArmWithoutAnEmittedEntryOnTheRealTape(t *testing.T) {
	for _, on := range []bool{true, false} {
		e, all, n0 := m431Day10_23(t)
		e.Cfg.MidRangeRejectSide = on
		known := e.heldArmIDs() // armed before the day began: lifecycle is theirs
		flagWallAt := ""
		isb1222Left := false
		rejected1222 := false
		for i := n0 + 1; i <= len(all); i++ {
			before := e.State.Refusals["flag_wall_room"]
			beforeRej := e.State.Refusals["mid_range_against_rejection"]
			out := m431Tick(e, all, i)
			ct := time.UnixMilli(all[i-1].CloseTime + 1).In(ctime()).Format("15:04")
			for id := range entryArmIDs(out) {
				known[id] = true
				if ct == "12:22" && id == "isb-17" {
					isb1222Left = true
				}
			}
			if ct == "12:22" && e.State.Refusals["mid_range_against_rejection"] > beforeRej {
				rejected1222 = true
			}
			if e.State.Refusals["flag_wall_room"] != before && flagWallAt == "" {
				flagWallAt = ct
			}
			for id := range e.heldArmIDs() {
				if !known[id] {
					t.Fatalf("knob %v, tick %s CT: arm %q is registered but no entry carrying it ever left Tick (phantom arm; flag_wall_room first seen %q)", on, ct, id, flagWallAt)
				}
			}
		}
		if on {
			if !rejected1222 || flagWallAt != "" {
				t.Fatalf("m466 G43: the 12:22 CT long ISB must be refused mid_range_against_rejection (and never by the flag wall): rejected=%v, first flag_wall_room at %q", rejected1222, flagWallAt)
			}
			continue
		}
		// #52 integration: c52 W9 (B22) exempts ISB from the flag-wall room refusal
		// (PHL/PLH entries only), so the 12:22 ISB now LEAVES Tick with its arm and
		// flag_wall_room no longer fires on this tape. The control follows the new
		// semantics: the 12:22 entry is emitted (its arm is legitimately held) and
		// the flag-wall pass does not refuse it. The un-register pass keeps its
		// teeth on this tape through another drop (the exit-pass mutant is RED at
		// 11:28 CT on arm isb-15) and in TestM431DroppedEntryUnregistersEveryArmKind.
		if !isb1222Left || flagWallAt != "" {
			t.Fatalf("control: the 12:22 CT ISB (isb-17) must leave Tick un-refused by the flag wall (W9 B22 exempts ISB): left=%v, first flag_wall_room at %q", isb1222Left, flagWallAt)
		}
	}
}

// TestM431DroppedEntryUnregistersEveryArmKind — the exit pass handles each arm
// kind through the one helper: an arm registered during the Tick whose entry is
// not among the surviving intents is un-registered; an arm held before the Tick
// began, or one whose entry survives, is untouched.
func TestM431DroppedEntryUnregistersEveryArmKind(t *testing.T) {
	e := New(DefaultConfig())
	e.State.ISBArms = map[string]ISBArm{"isb-old": {}, "isb-new-dropped": {}, "isb-new-kept": {}}
	e.State.LevelArms = map[string]LevelArm{"lvl-a": {ArmID: "lvl-new-dropped"}, "lvl-b": {ArmID: "lvl-old"}}
	e.State.RollingPHL = RollingPHL{ArmID: "roll-new-dropped"}
	e.State.EmaPendingArmID = "ema-new-dropped"
	e.State.EmaPendingSide = SideLong
	held := map[string]bool{"isb-old": true, "lvl-old": true}
	e.unregisterDroppedArms(held, []Intent{{Action: PlaceStopEntry, ArmID: "isb-new-kept"}})
	if _, ok := e.State.ISBArms["isb-new-dropped"]; ok {
		t.Errorf("dropped ISB arm survived: %v", e.State.ISBArms)
	}
	if _, ok := e.State.ISBArms["isb-old"]; !ok {
		t.Errorf("an arm held before the Tick must be left alone")
	}
	if _, ok := e.State.ISBArms["isb-new-kept"]; !ok {
		t.Errorf("an arm whose entry survives must stay registered")
	}
	if _, ok := e.State.LevelArms["lvl-a"]; ok {
		t.Errorf("dropped level arm survived: %v", e.State.LevelArms)
	}
	if _, ok := e.State.LevelArms["lvl-b"]; !ok {
		t.Errorf("a held level arm must be left alone")
	}
	if e.State.RollingPHL.ArmID != "" {
		t.Errorf("dropped rolling PHL survived: %+v", e.State.RollingPHL)
	}
	if e.State.EmaPendingArmID != "" || e.State.EmaPendingSide != "" {
		t.Errorf("dropped EMA34 pending survived: %q %q", e.State.EmaPendingArmID, e.State.EmaPendingSide)
	}
}
