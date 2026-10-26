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
// control: the flag-wall pass must refuse the ISB (flag_wall_room) AND leave no
// isb arm behind. Production path: Evaluator.Tick (the deferred leave passes).
// Mutant (drop the exit pass `e.unregisterDroppedArms` from Tick's defer) →
// the isb arm of 12:22 stays registered for two ticks → RED.
func TestM431NoArmWithoutAnEmittedEntryOnTheRealTape(t *testing.T) {
	e, all, n0 := m431Day10_23(t)
	known := e.heldArmIDs() // armed before the day began: lifecycle is theirs
	flagWallAt := ""
	for i := n0 + 1; i <= len(all); i++ {
		before := e.State.Refusals["flag_wall_room"]
		out := m431Tick(e, all, i)
		ct := time.UnixMilli(all[i-1].CloseTime + 1).In(ctime()).Format("15:04")
		for id := range entryArmIDs(out) {
			known[id] = true
		}
		if e.State.Refusals["flag_wall_room"] != before && flagWallAt == "" {
			flagWallAt = ct
		}
		for id := range e.heldArmIDs() {
			if !known[id] {
				t.Fatalf("tick %s CT: arm %q is registered but no entry carrying it ever left Tick (phantom arm; flag_wall_room first seen %q)", ct, id, flagWallAt)
			}
		}
	}
	if flagWallAt != "12:22" {
		t.Fatalf("control: the flag-wall pass must refuse the 12:22 CT ISB on this tape (first flag_wall_room at %q)", flagWallAt)
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
