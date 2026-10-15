package mentor

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// ── release #23 MENTOR-SEED-STATE — pins on the REAL 10-14 tape ──────────────
//
// testdata/mnq_1m_seed_state_2026-10-14.json.gz (extract_seed_state_fixture.py,
// read-only) holds MNQ 12-26 1m from the Sunday 10-11 17:00 open to 10-14 11:00
// CT. refill=1 marks the 244 'historical' rows of the 03:44–07:47 hole that the
// AddOn's replay released to the store at 08:23:00 — 20 s AFTER the release #22
// boot's 08:22:40 seed read the store (whose newest row was 08:15).
//
// Every pin drives the production evaluator entry points (SeedFull / Tick /
// ReseedState) on the windows the trader hands them (the last 1500 closed 1m
// bars at BarCloseInstant), and the independent Python oracles of the rescan
// (MATH-LEVELS-TIME.md: trig.py / livesim.py / boxes.py / swing.py) supply the
// numbers the live log printed.

var (
	ssSeedNow   = int64(1791984160000)                                        // 2026-10-14 08:22:40 CT — the release #22 boot seed
	ssStoreLast = int64(1791983700000)                                        // 2026-10-14 08:15 CT — the store's newest 1m row at that seed
	ssRefillBar = int64(1791984180000)                                        // 2026-10-14 08:23 CT — the bar the first tick after the 08:23:00 release closes
	ssRoll      = int64(1791928800000)                                        // 2026-10-13 17:00 CT — the session roll
	ssCPI       = []PrintWindow{{FromMs: 1791980400000, ToMs: 1791981300000}} // 10-14 CPI 07:30: 07:20–07:35 CT
)

type ssBar struct {
	k      market.Kline
	refill bool
}

func loadSeedStateFixture(t *testing.T) []ssBar {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "mnq_1m_seed_state_2026-10-14.json.gz"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	var doc struct {
		Bars [][]float64 `json:"bars"`
	}
	if err := json.NewDecoder(gz).Decode(&doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := make([]ssBar, 0, len(doc.Bars))
	for _, r := range doc.Bars {
		ot := int64(r[0])
		out = append(out, ssBar{k: market.Kline{
			OpenTime: ot, Open: r[1], High: r[2], Low: r[3], Close: r[4], Volume: r[5],
			CloseTime: ot + 59_999, Final: true,
		}, refill: r[6] == 1})
	}
	if len(out) < 3800 {
		t.Fatalf("fixture has %d bars — extraction broken", len(out))
	}
	return out
}

// ssTape returns the klines, optionally without the refill rows (the store as
// the 08:22:40 seed read it), keeping only bars opened at or before lastOpen.
func ssTape(all []ssBar, withRefill bool, lastOpen int64) []market.Kline {
	out := make([]market.Kline, 0, len(all))
	for _, b := range all {
		if b.k.OpenTime > lastOpen {
			break
		}
		if b.refill && !withRefill {
			continue
		}
		out = append(out, b.k)
	}
	return out
}

func ssIndexOf(bars []market.Kline, openMs int64) int {
	i := sort.Search(len(bars), func(i int) bool { return bars[i].OpenTime >= openMs })
	if i >= len(bars) || bars[i].OpenTime != openMs {
		return -1
	}
	return i
}

// ssSeed is what mentorSeedAtStart does: a fresh evaluator, the history's print
// windows set, SeedFull on every bar closed by now.
func ssSeed(bars []market.Kline, now int64, windows []PrintWindow) *Evaluator {
	hist := closedOnly(bars, now)
	e := New(lrProdCfg())
	e.Cfg.PrintWindows = windows
	SeedFull(e, hist, keyLevel1HBars(hist), now)
	return e
}

// ssSeedKnobOff seeds with the B52 box-new-zone knob OFF — the pre-B52 box
// world. B20 tests pin this so their scenario's historical returns survive
// into the trigger-zone refusal exactly as before release #29.
func ssSeedKnobOff(bars []market.Kline, now int64, windows []PrintWindow) *Evaluator {
	hist := closedOnly(bars, now)
	e := New(lrProdCfg())
	e.Cfg.BoxNewZoneAfterEscape = false
	e.Cfg.PrintWindows = windows
	SeedFull(e, hist, keyLevel1HBars(hist), now)
	return e
}

// ssLive is a continuous bot: booted at the 10-13 17:00 roll (seeded on the
// FULL tape), then ticked on every closed bar up to and including stopOpen, the
// day's print windows set per tick exactly as mentorEvalOnce does. each (may be
// nil) runs after every tick.
func ssLive(t *testing.T, full []market.Kline, stopOpen int64, each func(i int, e *Evaluator, out []Intent)) *Evaluator {
	t.Helper()
	var live *Evaluator
	for i, b := range full {
		if b.OpenTime < ssRoll {
			continue
		}
		if b.OpenTime > stopOpen {
			break
		}
		now := BarCloseInstant(b)
		if live == nil {
			live = ssSeed(full, BarCloseInstant(full[i-1]), ssCPI)
		}
		live.Cfg.PrintWindows = ssCPI
		out := live.Tick(lrWindow(full, i), now)
		if each != nil {
			each(i, live, out)
		}
	}
	if live == nil {
		t.Fatal("the fixture does not cover the 10-13 17:00 roll")
	}
	return live
}

func ssLine(t TriggerLine) string {
	return fmt.Sprintf("%s@%.2f since %s", t.Dir, t.Price, lrCT(time.UnixMilli(t.MovedAt)))
}

// TestReseedAfterHistoryRefill1014 — N1 (P1). The 08:22:40 seed read a store
// that lacked the 03:44–07:47 hole (refilled at 08:23:00) and ended at 08:15:
// it committed a 12-minute 1h 07:00 bucket and seeded "1h short @30790.75"
// (the live boot line, reproduced here). The re-seed the first tick after the
// refill runs must leave the evaluator EXACTLY where a fresh seed on the full
// tape leaves it — 1h short @30716.00 since 07:00 (trig.py truth) — and from
// there it must tick identically to a bot that never had the hole.
// Mutant: ReseedState continuing the old lines (seedHistoryState reset=false)
// → RED (the committed partial bucket survives).
func TestReseedAfterHistoryRefill1014(t *testing.T) {
	all := loadSeedStateFixture(t)
	holed := ssTape(all, false, ssStoreLast)
	full := ssTape(all, true, 1<<62)

	e := ssSeed(holed, ssSeedNow, ssCPI)
	// Control: the holed seed IS the live 08:22:40 boot line [A, vl_2026-10-14.log:12869].
	if got := ssLine(e.State.HTF.OneH); got != "short@30790.75 since 10-14 07:00" {
		t.Fatalf("control: the holed seed must reproduce the live boot's 1h line short@30790.75 since 10-14 07:00, got %s", got)
	}
	if got := ssLine(e.State.HTF.FourH); got != "short@30745.50 since 10-14 01:00" {
		t.Fatalf("control: the holed seed's 4h line, got %s (live boot: short @30745.50 since 01:00)", got)
	}
	if got := ssLine(e.State.Trigger); got != "long@30580.00 since 10-14 08:05" {
		t.Fatalf("control: the holed seed's 5m line, got %s (live boot: long @30580.00 since 08:05)", got)
	}
	holedEMA, holedSwing := e.State.EMA34, e.State.Swing.Line

	// The replay release landed at 08:23:00; the first tick (the 08:23 bar)
	// re-seeds from the refilled store, then ticks.
	ri := ssIndexOf(full, ssRefillBar)
	now := BarCloseInstant(full[ri])
	rep := ReseedState(e, closedOnly(full[:ri+1], now), keyLevel1HBars(closedOnly(full[:ri+1], now)), now)
	t.Logf("re-seed: %s", rep.Line())
	if !rep.Changed() || rep.Refused != "" {
		t.Fatalf("the refill must change the state: %s", rep.Line())
	}
	truth := ssSeed(full, now, ssCPI)
	if e.State.HTF.OneH != truth.State.HTF.OneH || e.State.HTF.FourH != truth.State.HTF.FourH || e.State.Trigger != truth.State.Trigger {
		t.Fatalf("re-seed ≠ full-tape seed: 1h %s vs %s · 4h %s vs %s · 5m %s vs %s",
			ssLine(e.State.HTF.OneH), ssLine(truth.State.HTF.OneH), ssLine(e.State.HTF.FourH), ssLine(truth.State.HTF.FourH), ssLine(e.State.Trigger), ssLine(truth.State.Trigger))
	}
	if got := ssLine(e.State.HTF.OneH); got != "short@30716.00 since 10-14 07:00" {
		t.Fatalf("the re-seeded 1h line must be the never-stopped truth short@30716.00 since 10-14 07:00 (trig.py), got %s", got)
	}
	if e.State.EMA34 != truth.State.EMA34 || e.State.EMA9 != truth.State.EMA9 || e.State.Swing.Line != truth.State.Swing.Line {
		t.Fatalf("re-seed EMA34/EMA9/swing %.4f/%.4f/%.4f ≠ full-tape %.4f/%.4f/%.4f",
			e.State.EMA34, e.State.EMA9, e.State.Swing.Line, truth.State.EMA34, truth.State.EMA9, truth.State.Swing.Line)
	}
	t.Logf("1m EMA34 holed %.2f → refilled %.2f · swing line %.4f → %.4f", holedEMA, e.State.EMA34, holedSwing, e.State.Swing.Line)

	// From here the re-seeded evaluator ticks exactly like a bot that never
	// had the hole — the 08:48 "1h long @30639.00" and 08:56 "short @30542.75"
	// false moves (livesim.py) never happen.
	live := ssLive(t, full, ssRefillBar-60_000, nil)
	stop := ri + 40 // through 09:03
	for i := ri; i <= stop; i++ {
		n := BarCloseInstant(full[i])
		e.Cfg.PrintWindows, live.Cfg.PrintWindows = ssCPI, ssCPI
		e.Tick(lrWindow(full, i), n)
		live.Tick(lrWindow(full, i), n)
		if e.State.HTF != live.State.HTF || e.State.Trigger != live.State.Trigger || e.State.EMA34 != live.State.EMA34 {
			t.Fatalf("%s CT: re-seeded ≠ never-holed bot — 1h %s vs %s · 4h %s vs %s · 5m %s vs %s · EMA34 %.4f vs %.4f",
				lrCT(time.UnixMilli(full[i].OpenTime)), ssLine(e.State.HTF.OneH), ssLine(live.State.HTF.OneH),
				ssLine(e.State.HTF.FourH), ssLine(live.State.HTF.FourH), ssLine(e.State.Trigger), ssLine(live.State.Trigger),
				e.State.EMA34, live.State.EMA34)
		}
	}
}

// TestReseedKeepsWhatTheBotActedOn — ReseedState rewrites ONLY the
// history-derived state: the box latch and its watermarks, the day latch, the
// ORB, visit counters, arms and legs stand.
func TestReseedKeepsWhatTheBotActedOn(t *testing.T) {
	all := loadSeedStateFixture(t)
	full := ssTape(all, true, 1<<62)
	ri := ssIndexOf(full, ssRefillBar)
	now := BarCloseInstant(full[ri])
	e := ssSeed(full, now, ssCPI)
	e.State.Visits = map[string]int{"k": 3}
	e.State.ISBArms["isb-9"] = ISBArm{}
	e.State.LevelArms = map[string]LevelArm{"lvl": {ArmID: "lvl-1"}}
	e.State.Day = DayLatch{Key: "2026-10-14", Verdict: DayTrade}
	e.State.ORB = ORB{Day: 1, High: 1, Low: 0, Drawn: true}
	e.State.Touches["k"] = Touch{}
	e.State.ArmSeq = 41
	boxes, refs := fmt.Sprint(e.State.LatchedBoxes), fmt.Sprint(e.State.BoxRefs)
	before, _ := MarshalState(e.State)
	ReseedState(e, closedOnly(full[:ri+1], now), nil, now)
	if fmt.Sprint(e.State.LatchedBoxes) != boxes || fmt.Sprint(e.State.BoxRefs) != refs {
		t.Fatal("a re-seed must keep the box latch and its watermarks")
	}
	if e.State.Visits["k"] != 3 || len(e.State.ISBArms) != 1 || len(e.State.LevelArms) != 1 ||
		e.State.Day.Verdict != DayTrade || !e.State.ORB.Drawn || e.State.ArmSeq != 41 || len(e.State.Touches) != 1 {
		t.Fatalf("a re-seed must keep visits/arms/day latch/ORB/touches: %+v", e.State)
	}
	after, _ := MarshalState(e.State)
	if string(before) != string(after) {
		t.Fatal("a re-seed on the SAME store must be a no-op on the state")
	}
	// An empty store read is refused — never a wipe.
	if rep := ReseedState(e, nil, nil, now); rep.Refused == "" || rep.Changed() {
		t.Fatalf("an empty read must be refused and change nothing: %s", rep.Line())
	}
}

// TestSeedHonoursPrintWindowsRestartParity1014 — N6 (P2). On the 10-14 CPI day a
// running bot dropped the 07:20–07:35 print candles from the 1h/4h feed; a
// restart must seed the SAME lines. The trader now hands the seed the windows
// of every replayed day; with them a restart at every instant from 07:20 to
// 10:00 reproduces the continuous 1h/4h lines (direction, price, since AND the
// committed bucket). Without them (the seed.go:256 nil read) the 1h fires at
// 07:30 instead of 07:35 and commits the 07:00 bucket at H 30860.00 instead of
// 30817.00 → RED.
func TestSeedHonoursPrintWindowsRestartParity1014(t *testing.T) {
	all := loadSeedStateFixture(t)
	full := ssTape(all, true, 1<<62)
	from, to := int64(1791980400000), int64(1791990000000) // 07:20 → 10:00 CT
	var noWin, samples int
	ssLive(t, full, to, func(i int, live *Evaluator, _ []Intent) {
		if full[i].OpenTime < from {
			return
		}
		samples++
		now := BarCloseInstant(full[i])
		re := ssSeed(full, now, ssCPI)
		if re.State.HTF.OneH != live.State.HTF.OneH || re.State.HTF.FourH != live.State.HTF.FourH {
			t.Fatalf("%s CT: restart with the print windows ≠ continuous — 1h %s (bar H %.2f) vs %s (bar H %.2f) · 4h %s vs %s",
				lrCT(time.UnixMilli(full[i].OpenTime)), ssLine(re.State.HTF.OneH), re.State.HTF.OneH.LastBar.High,
				ssLine(live.State.HTF.OneH), live.State.HTF.OneH.LastBar.High, ssLine(re.State.HTF.FourH), ssLine(live.State.HTF.FourH))
		}
		if bare := ssSeed(full, now, nil); bare.State.HTF.OneH != live.State.HTF.OneH || bare.State.HTF.FourH != live.State.HTF.FourH {
			noWin++
		}
	})
	t.Logf("%d restart samples 07:20–10:00 · a seed WITHOUT the windows differs in %d", samples, noWin)
	if noWin == 0 {
		t.Fatal("the scenario lost its teeth: a window-less seed must differ on the CPI day")
	}
}

// TestRestartReproducesBoxLatch1014 — N2 (P2). A continuous bot latched FTGH
// 30792.50–30805.75 (formed 10-13 17:05) and FTGL 30767.00–30801.00 (formed
// 17:28) [boxes.py]; the 08:22 restart redrew 30878.00–30891.50 and 30498.00–
// 30586.75. A restart at any instant of the session now reproduces the
// continuous latch. Mutant: no seedBoxLatch in seed() → RED.
func TestRestartReproducesBoxLatch1014(t *testing.T) {
	all := loadSeedStateFixture(t)
	full := ssTape(all, true, 1<<62)
	key := func(m map[string]Box) string {
		var parts []string
		for _, k := range []string{"ftgl", "ftgh"} {
			if b, ok := m[k]; ok {
				parts = append(parts, fmt.Sprintf("%s %.2f–%.2f formed %s", k, b.Bottom, b.Top, lrCT(time.UnixMilli(b.FormedAtMs))))
			}
		}
		return strings.Join(parts, " · ")
	}
	samples := 0
	var worst time.Duration
	live := ssLive(t, full, 1791992400000, func(i int, live *Evaluator, _ []Intent) { // → 10:40 CT
		if (i % 37) != 0 {
			return
		}
		samples++
		now := BarCloseInstant(full[i])
		t0 := time.Now()
		re := ssSeed(full, now, ssCPI)
		if d := time.Since(t0); d > worst {
			worst = d
		}
		if key(re.State.LatchedBoxes) != key(live.State.LatchedBoxes) {
			t.Fatalf("%s CT: restart latch [%s] ≠ continuous [%s]", lrCT(time.UnixMilli(full[i].OpenTime)), key(re.State.LatchedBoxes), key(live.State.LatchedBoxes))
		}
	})
	const want = "ftgl 30767.00–30801.00 formed 10-13 17:28 · ftgh 30792.50–30805.75 formed 10-13 17:05"
	if got := key(live.State.LatchedBoxes); got != want {
		t.Fatalf("continuous latch = %s, want boxes.py's %s", got, want)
	}
	re := ssSeed(full, ssSeedNow, ssCPI)
	if got := key(re.State.LatchedBoxes); got != want {
		t.Fatalf("the 08:22:40 restart latch = %s, want the continuous %s (it redrew 30878.00–30891.50 / 30498.00–30586.75)", got, want)
	}
	if line := re.SeedBoxLine(); !strings.Contains(line, "FTGH 30792.50–30805.75 (formed 10-13 17:05 CT)") || !strings.Contains(line, "FTGL 30767.00–30801.00 (formed 10-13 17:28 CT)") {
		t.Fatalf("seed box line must READ the replayed latch: %q", line)
	}
	t.Logf("%d restart samples; slowest full SeedFull %v", samples, worst)
}

// TestLatchedBoxIsOutputEveryTick1014 — N3 (P2). latchBoxes iterated only the
// fresh scan, so a latched role vanished whenever the scan lacked it (boxes.py:
// the continuous FTGL missing on 141 ticks). Every latched role is now output
// on every tick — its edges are locations and its interior bans entries. The
// mutant (iterate `fresh`) drops a latched role on N ticks → RED.
func TestLatchedBoxIsOutputEveryTick1014(t *testing.T) {
	all := loadSeedStateFixture(t)
	full := ssTape(all, true, 1<<62)
	ticks, freshMissing := 0, 0
	ssLive(t, full, 1791992400000, func(i int, live *Evaluator, _ []Intent) {
		if len(live.State.LatchedBoxes) == 0 {
			return
		}
		ticks++
		edges := map[string]bool{}
		for _, l := range live.State.ChartLevels {
			if l.Kind == KindFTGHEdge || l.Kind == KindFTGLEdge {
				edges[l.Key] = true
			}
		}
		fresh := map[string]bool{}
		for _, b := range BoxesBuild(lrWindow(full, i), live.Cfg.Box, time.UnixMilli(BarCloseInstant(full[i]))) {
			fresh[string(b.Kind)] = true
		}
		for role, b := range live.State.LatchedBoxes {
			if !fresh[role] {
				freshMissing++
			}
			if !edges[b.Key+":top"] || !edges[b.Key+":bottom"] {
				t.Fatalf("%s CT: latched %s %s is not in the tick's level set", lrCT(time.UnixMilli(full[i].OpenTime)), role, b.Key)
			}
		}
	})
	t.Logf("%d ticks with a latch; a latched role was absent from the fresh scan %d times (each one used to drop the box)", ticks, freshMissing)
	if freshMissing == 0 {
		t.Fatal("the scenario lost its teeth: the fresh scan never lacked a latched role")
	}
}

// TestRestartIssuesNoHistoricalBoxIntents1014 — P2-N2 (mentor rescan). New()
// leaves BoxRefs empty, so the first tick after a restart walked every return
// since each box formed (10-14 08:24: trigger_ftgh_sell_zone:6). The seed now
// watermarks every latched box at the newest closed bar: the restart's first
// tick judges exactly the returns a continuous bot judges on that tick — the
// same box refusals, the same box intents. Mutant: no BoxRefs watermark → RED.
func TestRestartIssuesNoHistoricalBoxIntents1014(t *testing.T) {
	all := loadSeedStateFixture(t)
	full := ssTape(all, true, 1<<62)
	ri := ssIndexOf(full, ssRefillBar)
	var liveRef map[string]int
	var liveOut []Intent
	ssLive(t, full, full[ri].OpenTime, func(i int, live *Evaluator, out []Intent) {
		if i == ri-1 {
			liveRef = copyCounts(live.State.Refusals)
		}
		if i == ri {
			liveOut = out
			liveRef = diffCounts(liveRef, live.State.Refusals)
		}
	})
	re := ssSeed(full, BarCloseInstant(full[ri-1]), ssCPI)
	before := copyCounts(re.State.Refusals)
	out := re.Tick(lrWindow(full, ri), BarCloseInstant(full[ri]))
	got := diffCounts(before, re.State.Refusals)
	boxOnly := func(m map[string]int) string {
		var ks []string
		for k, v := range m {
			if strings.HasPrefix(k, "box_") || strings.HasPrefix(k, "trigger_") || k == "isb_trigger_side" || k == "room" {
				ks = append(ks, fmt.Sprintf("%s:%d", k, v))
			}
		}
		sort.Strings(ks)
		return strings.Join(ks, ",")
	}
	if boxOnly(got) != boxOnly(liveRef) {
		t.Fatalf("restart first tick box refusals [%s] ≠ continuous [%s] — historical returns were walked", boxOnly(got), boxOnly(liveRef))
	}
	nBox := func(ins []Intent) int {
		n := 0
		for _, in := range ins {
			if in.Setup == "BOX" {
				n++
			}
		}
		return n
	}
	if nBox(out) != nBox(liveOut) {
		t.Fatalf("restart first tick emitted %d BOX intents, continuous %d", nBox(out), nBox(liveOut))
	}
	if re.State.SchoolOneSide != "" {
		t.Fatalf("restart first tick armed the B20 upgrade (%s) from history", re.State.SchoolOneSide)
	}
}

func copyCounts(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func diffCounts(before, after map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range after {
		if d := v - before[k]; d != 0 {
			out[k] = d
		}
	}
	return out
}

// TestSchoolOneArmedOnlyBySurvivingEntries1014 — P2-N2 (mentor rescan), the B20
// half. A school-1 box entry the trigger zone (or any later filter) refuses
// must NOT arm the flip upgrade: the 08:24 restart's refused historical FTGH
// returns armed the short side and the bot logged a spurious "confluence
// upgrade IGNORED". Driven with the box watermark removed, so the first tick
// walks the historical returns (4 of them die in triggerBoxZoneFilter).
// Mutant: arm SchoolOneSide at the box emit site again → RED.
func TestSchoolOneArmedOnlyBySurvivingEntries1014(t *testing.T) {
	all := loadSeedStateFixture(t)
	full := ssTape(all, true, 1<<62)
	ri := ssIndexOf(full, ssRefillBar)
	// B52 (release #29) ships ON and re-latches post-escape boxes on this
	// tape, changing the box set the first tick walks. This B20 test pins the
	// pre-B52 box world so its four historical FTGH returns still die in the
	// trigger zone exactly as the scenario was written.
	re := ssSeedKnobOff(full, BarCloseInstant(full[ri-1]), ssCPI)
	re.State.BoxRefs = map[string]int64{} // no watermark: walk every historical return
	before := copyCounts(re.State.Refusals)
	out := re.Tick(lrWindow(full, ri), BarCloseInstant(full[ri]))
	d := diffCounts(before, re.State.Refusals)
	if d["trigger_ftgh_sell_zone"] == 0 {
		t.Fatalf("the scenario lost its teeth: no school-1 box entry died in the trigger zone (%v)", d)
	}
	survivors := 0
	for _, in := range out {
		if in.Setup == "BOX" && (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) {
			survivors++
		}
	}
	if survivors == 0 && re.State.SchoolOneSide != "" {
		t.Fatalf("no box entry left Tick, yet the B20 upgrade was armed for %s (refused: %v)", re.State.SchoolOneSide, d)
	}
}
