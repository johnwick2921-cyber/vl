package trader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/kernel"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── MODE EXCLUSIVITY ────────────────────────────────────────────────────────

func modeTrader(mentor, pepOn, dayPlan bool) *AutoTrader {
	c := pepCfgOf("A", 3)
	if !pepOn {
		f := false
		c.Enabled = &f
	}
	cfg := &store.StrategyConfig{Pep: &c}
	cfg.RiskControl.MentorMode = mentor
	if dayPlan {
		cfg.DayPlan = &store.DayPlanConfig{PlanEnabled: true}
	}
	at := &AutoTrader{id: "m", name: "M", exchange: "ninjatrader", userID: "u"}
	at.config.StrategyConfig = cfg
	return at
}

// A trader is exactly one of {AI/planner, mentor, pep}. MUTANT: pepEnabled()
// returning pepSaved() (ignoring the conflict) makes the conflict rows RED.
func TestPepModeExclusivity(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		at                            *AutoTrader
		wantPep, wantMentor, wantOwns bool
		wantMode                      string
	}{
		{"ai", modeTrader(false, false, false), false, false, false, "ai"},
		{"mentor", modeTrader(true, false, false), false, true, true, "mentor"},
		{"pep", modeTrader(false, true, false), true, false, true, "pep"},
		{"pep+mentor is a conflict, pep does not act", modeTrader(true, true, false), false, true, true, "conflict"},
		{"pep+dayplan is a conflict, pep does not act", modeTrader(false, true, true), false, false, false, "conflict"},
	} {
		if got := tc.at.pepEnabled(); got != tc.wantPep {
			t.Errorf("%s: pepEnabled = %v, want %v", tc.name, got, tc.wantPep)
		}
		if got := tc.at.mentorEnabled(); got != tc.wantMentor {
			t.Errorf("%s: mentorEnabled = %v, want %v", tc.name, got, tc.wantMentor)
		}
		if got := tc.at.modeOwnsExits(); got != tc.wantOwns {
			t.Errorf("%s: modeOwnsExits = %v, want %v", tc.name, got, tc.wantOwns)
		}
		if got := tc.at.config.StrategyConfig.TradingMode(); got != tc.wantMode {
			t.Errorf("%s: TradingMode = %q, want %q", tc.name, got, tc.wantMode)
		}
	}
}

// BOTH ON is REFUSED at load: Run returns the named error, counts it, tells the
// owner with ONE alert (even across repeated start attempts), and leaves the
// trader not running. MUTANT: delete the pepStartRefusal call in Run -> Run
// proceeds (here it would block/err differently, and no alert exists) -> RED.
func TestPepBothOnIsRefusedAtStartWithOneAlert(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, tc := range []struct {
		name string
		at   *AutoTrader
		code string
	}{
		{"mentor", modeTrader(true, true, false), store.PepConflictMentor},
		{"day plan", modeTrader(false, true, true), store.PepConflictDayPlan},
	} {
		at := tc.at
		at.id = "conflict-" + strings.ReplaceAll(tc.name, " ", "-")
		at.store = st
		before := gateBlocks(at.id, tc.code)
		for i := 0; i < 2; i++ {
			err := at.Run()
			if err == nil || !strings.Contains(err.Error(), tc.code) {
				t.Fatalf("%s: Run #%d = %v, want the named %s refusal", tc.name, i, err, tc.code)
			}
			if at.runningNow() {
				t.Fatalf("%s: the refused trader must not read as running", tc.name)
			}
		}
		if got := gateBlocks(at.id, tc.code); got != before+2 {
			t.Fatalf("%s: every refusal is counted: %d -> %d", tc.name, before, got)
		}
		rows, err := st.Alert().List(at.id, 10)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, r := range rows {
			if r.Kind == "pep_mode_conflict" {
				n++
				if r.Level != "P0" || !strings.Contains(r.Body, "NOT started") {
					t.Fatalf("alert %+v", r)
				}
			}
		}
		if n != 1 {
			t.Fatalf("%s: want ONE pep_mode_conflict alert, got %d (rows %d)", tc.name, n, len(rows))
		}
	}
	// control: pep alone starts the pre-flight (the refusal is silent here)
	ok := modeTrader(false, true, false)
	if err := ok.pepStartRefusal(); err != nil {
		t.Fatalf("pep alone must not be refused: %v", err)
	}
	if err := modeTrader(true, false, false).pepStartRefusal(); err != nil {
		t.Fatalf("mentor alone must not be refused: %v", err)
	}
}

// ── ADMISSION: the pep path in every branch ─────────────────────────────────

func pepDecision(side string) *kernel.Decision {
	return &kernel.Decision{Symbol: "MNQ", Action: side, StopLoss: 25001, TakeProfit: 25081, PepSourced: true, PepContracts: 3}
}

func pepIntent(at *AutoTrader, now time.Time) admitIntent {
	return admitIntent{Path: admitPep, Symbol: "MNQ", Action: "open_long", Now: now, Decision: pepDecision("open_long")}
}

// With pep ON only a pep decision enters; AI/agent/arm/picture entries are
// refused under pep_ai_entries_off, and a pep decision on a non-pep trader
// under pep_not_enabled. MUTANT: delete the pepAdmitRefusal call in admitChain
// -> the AI decision on a pep trader is admitted -> RED.
func TestPepAdmissionIsExclusive(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	now := ctTimeAt(t, pepDay, "09:00")
	for _, path := range []admitPath{admitDecision, admitAgent, admitArm, admitPicture} {
		before := gateBlocks(w.at.id, "pep_ai_entries_off")
		in := admitIntent{Path: path, Symbol: "MNQ", Action: "open_long", Now: now, Key: "k",
			Decision: &kernel.Decision{Symbol: "MNQ", Action: "open_long", StopLoss: 1, TakeProfit: 3}}
		refusal, refused := w.at.admitEntry(in)
		if !refused || !strings.HasPrefix(refusal, "pep_mode: AI entries are OFF") {
			t.Fatalf("path %s on a pep trader: refused=%v %q", path, refused, refusal)
		}
		if gateBlocks(w.at.id, "pep_ai_entries_off") != before+1 {
			t.Fatalf("path %s: refusal not counted under pep_ai_entries_off", path)
		}
	}
	// the pep decision on the pep path passes the whole chain
	if refusal, refused := w.at.admitEntry(pepIntent(w.at, now)); refused {
		t.Fatalf("a pep decision on a pep trader must be admitted at 09:00 CT: %q", refusal)
	}
	// a pep decision on an AI trader is refused
	ai := &AutoTrader{id: "ai-pep", exchange: "ninjatrader"}
	ai.config.StrategyConfig = &store.StrategyConfig{}
	before := gateBlocks(ai.id, "pep_not_enabled")
	if r, refused := ai.admitEntry(pepIntent(ai, now)); !refused || !strings.Contains(r, "not in pep mode") {
		t.Fatalf("pep decision on an AI trader: refused=%v %q", refused, r)
	}
	if gateBlocks(ai.id, "pep_not_enabled") != before+1 {
		t.Fatal("not counted under pep_not_enabled")
	}
	// mentor/AI traders are byte-identical: pepAdmitRefusal says nothing
	men := modeTrader(true, false, false)
	if c, _ := men.pepAdmitRefusal(admitIntent{Path: admitArm, MentorArm: true}); c != "" {
		t.Fatalf("a mentor arm must be untouched by pep: %q", c)
	}
	if c, _ := ai.pepAdmitRefusal(admitIntent{Path: admitDecision, Decision: &kernel.Decision{}}); c != "" {
		t.Fatalf("an AI decision must be untouched by pep: %q", c)
	}
}

// pep's own last-entry rule: no new entry at/after the flat time; one second
// before is fine. MUTANT: delete the admitPep branch's cutoff -> 15:00 is
// admitted -> RED.
func TestPepNoEntryAtOrAfterTheFlatTime(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	before := gateBlocks(w.at.id, "pep_after_flat_time")
	at1500 := ctTimeAt(t, pepDay, "15:00")
	if r, refused := w.at.admitEntry(pepIntent(w.at, at1500)); !refused || !strings.HasPrefix(r, "pep_after_flat_time:") {
		t.Fatalf("15:00:00 CT must refuse: refused=%v %q", refused, r)
	}
	if gateBlocks(w.at.id, "pep_after_flat_time") != before+1 {
		t.Fatal("not counted under pep_after_flat_time")
	}
	if r, refused := w.at.admitEntry(pepIntent(w.at, at1500.Add(-time.Second))); refused {
		t.Fatalf("14:59:59 CT must admit: %q", r)
	}
	// the flat time is a SETTING: 14:00 moves the cutoff
	c := pepCfgOf("A", 3)
	c.FlatTimeCT = "14:00"
	w.at.config.StrategyConfig = &store.StrategyConfig{Pep: &c}
	if _, refused := w.at.admitEntry(pepIntent(w.at, ctTimeAt(t, pepDay, "14:00"))); !refused {
		t.Fatal("a 14:00 flat setting must refuse an entry at 14:00")
	}
	if _, refused := w.at.admitEntry(pepIntent(w.at, ctTimeAt(t, pepDay, "13:59"))); refused {
		t.Fatal("13:59 must still admit under a 14:00 flat")
	}
}

// The safety legs are KEPT in the pep branch: each refuses a pep entry under its
// own named class. (feed down: needs the NT8 feed fixture; the rest are state.)
func TestPepKeepsTheSafetyLegsOfTheChain(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	now := ctTimeAt(t, pepDay, "09:00")

	// stop_until: the owner pause
	w.at.pauseUntilMs.Store(now.Add(time.Hour).UnixMilli())
	t.Cleanup(func() { w.at.pauseUntilMs.Store(0) })
	b := gateBlocks(w.at.id, "stop_until")
	if r, refused := w.at.admitEntry(pepIntent(w.at, now)); !refused || !strings.HasPrefix(r, "stop_until:") {
		t.Fatalf("owner pause must refuse a pep entry: %v %q", refused, r)
	}
	if gateBlocks(w.at.id, "stop_until") != b+1 {
		t.Fatal("stop_until not counted")
	}
	w.at.pauseUntilMs.Store(0)

	// CME closed (Saturday): pep has no runCycle skip in front of it
	sat := ctTimeAt(t, "2026-10-31", "09:00")
	bc := gateBlocks(w.at.id, "cme_closed")
	if r, refused := w.at.admitEntry(pepIntent(w.at, sat)); !refused || !strings.HasPrefix(r, "cme_closed:") {
		t.Fatalf("a closed CME must refuse a pep entry: %v %q", refused, r)
	}
	if gateBlocks(w.at.id, "cme_closed") != bc+1 {
		t.Fatal("cme_closed not counted")
	}
}

// ── DATA GATE: pep's OWN window and reader ──────────────────────────────────

// A hole inside the box window refuses; a hole only the DEEP AI window can see
// does not pause pep (it never reads it). The window handed to the tracker is
// pep's derived one, not 12000. MUTANT: dataIntegrityAdmitRefusal without the
// admitPep branch (integrityWindowFor -> deep) -> the old-hole leg refuses -> RED.
func TestPepDataGateUsesItsOwnWindow(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	now := ctTimeAt(t, pepDay, "08:38")
	pepWin := w.at.pepIntegrityOpenMinAt(now)
	if pepWin >= ntTrader.IntegrityIntradayOpenMin || pepWin < 30 || pepWin%30 != 0 {
		t.Fatalf("pep window = %d open min: must be small, >= 30 and a multiple of 30", pepWin)
	}
	// 08:38 CT: box start 08:30 - 6 min margin = 08:24 -> 14 min back -> clamps to 30
	if pepWin != 30 {
		t.Fatalf("style A at 08:38: window %d, want 30", pepWin)
	}
	// style E adds the EMA warm-up (27 x 2m = 54 min): 08:38 - (08:30 - 54 - 6)
	e := pepCfgOf("E", 3)
	w.at.config.StrategyConfig = &store.StrategyConfig{Pep: &e}
	if got := w.at.pepIntegrityOpenMinAt(now); got != 90 { // 8+60 = 68 -> 90
		t.Fatalf("style E at 08:38: window %d, want 90", got)
	}
	w.at.config.StrategyConfig = &store.StrategyConfig{Pep: &store.PepConfig{Enabled: boolPtr(true), Style: "A"}}

	// 1) a hole the pep window reads: refused, class data_integrity_hole, counted
	fake := &intFake{blockedFor: func(openMin int) bool { return openMin <= pepWin }}
	armIntegrityGate(t, fake)
	before := gateBlocks(w.at.id, ntTrader.IntegrityClassHole)
	r, refused := w.at.admitEntry(pepIntent(w.at, now))
	if !refused || !strings.HasPrefix(r, "data_integrity: ") {
		t.Fatalf("a hole inside the box window must refuse a pep entry: %v %q", refused, r)
	}
	if gateBlocks(w.at.id, ntTrader.IntegrityClassHole) != before+1 {
		t.Fatal("the refusal must be counted under data_integrity_hole")
	}
	if len(fake.asked) == 0 || fake.asked[0] != pepWin {
		t.Fatalf("the tracker was asked windows %v, want pep's own %d first", fake.asked, pepWin)
	}
	for _, a := range fake.asked {
		if a == ntTrader.IntegrityDeepOpenMin {
			t.Fatalf("pep must never be judged on the deep AI window: asked %v", fake.asked)
		}
	}

	// 2) an old hole only the 12000-minute window sees: pep is NOT paused...
	old := &intFake{blockedFor: func(openMin int) bool { return openMin > 1500 }}
	armIntegrityGate(t, old)
	if r, refused := w.at.admitEntry(pepIntent(w.at, now)); refused {
		t.Fatalf("an old hole outside pep's window must not pause pep: %q", r)
	}
	// ...while the AI path on the same data IS (the control: the fake works)
	aiAt := &AutoTrader{id: "ai-old", exchange: "ninjatrader"}
	aiAt.config.StrategyConfig = &store.StrategyConfig{}
	aiAt.config.NinjaTraderSymbol = "MNQ"
	if r, refused := aiAt.admitEntry(admitIntent{Path: admitDecision, Symbol: "MNQ", Action: "open_long", Now: now,
		Decision: &kernel.Decision{Symbol: "MNQ", Action: "open_long", StopLoss: 25001, TakeProfit: 25081}}); !refused || !strings.HasPrefix(r, "data_integrity: ") {
		t.Fatalf("control: the AI path must still be paused by the deep-window hole: %v %q", refused, r)
	}
}


// The data-truth reader: pep is held to everything the mentor reader is held
// to (it reads the same raw 1m ring) and to nothing that only the AI reader
// reads. MUTANT: Concerns() without the pep/mentor leg -> RED.
func TestPepReaderConcerns(t *testing.T) {
	mk := func(readers ...string) datatruth.Reason { return datatruth.Reason{Check: "c", Readers: readers} }
	for _, tc := range []struct {
		name string
		r    datatruth.Reason
		want bool
	}{
		{"unscoped block concerns pep", mk(), true},
		{"a pep-scoped block concerns pep", mk(datatruth.ReaderPep), true},
		{"a mentor-scoped block concerns pep (same raw 1m ring)", mk(datatruth.ReaderMentor), true},
		{"an AI-only block does NOT concern pep", mk(datatruth.ReaderAI), false},
		{"an unknown token concerns everyone (fail closed)", mk("typo"), true},
	} {
		if got := tc.r.Concerns(datatruth.ReaderPep); got != tc.want {
			t.Errorf("%s: Concerns(pep) = %v, want %v", tc.name, got, tc.want)
		}
	}
	// the other readers are unchanged: an AI trader is not concerned by a pep-only block
	if mk(datatruth.ReaderPep).Concerns(datatruth.ReaderAI) || mk(datatruth.ReaderPep).Concerns(datatruth.ReaderMentor) {
		t.Fatal("a pep-scoped block must not pause the AI or mentor readers")
	}
}

// ── the exit-mover census: the four AI-era movers gate on modeOwnsExits ─────
//
// A source census (cheap, in the style of the repo's wiring pins): each AI-era
// stop mover / closer names modeOwnsExits(), and none of them has fallen back to
// a bare mentorEnabled() gate that would let a pep position through.
func TestAIEraMoversGateOnModeOwnsExits(t *testing.T) {
	for _, tc := range []struct{ file, fn string }{
		{"auto_trader.go", "maybeMoveStopToBreakeven"},
		{"auto_trader_trailing.go", "maybeTrailStop"},
		{"thesis_death_exit.go", "maybeExitOnThesisDeath"},
		{"auto_trader_risk.go", "checkPositionDrawdown"},
		{"breakeven_r.go", "breakevenTriggerFor"},
	} {
		body := funcBody(t, tc.file, tc.fn)
		if !strings.Contains(body, "modeOwnsExits()") {
			t.Errorf("%s:%s does not gate on modeOwnsExits(): an AI-era mover could act on a pep position", tc.file, tc.fn)
		}
		if strings.Contains(body, "mentorEnabled()") {
			t.Errorf("%s:%s still gates on a bare mentorEnabled()", tc.file, tc.fn)
		}
	}
}

// ── the loop wiring ─────────────────────────────────────────────────────────

func TestPepIsWiredIntoTheLoops(t *testing.T) {
	for _, tc := range []struct{ file, fn, want string }{
		{"armed_event_pass.go", "runArmedEventLoop", "pepEventPassAt("},
		{"auto_trader_loop.go", "runCycle", "pepTick()"},
		{"auto_trader_loop.go", "runCycle", "pepSkipsAIDecision()"},
		{"auto_trader_risk.go", "monitorTick", "pepFlatBeat("},
		{"auto_trader.go", "Run", "pepStartRefusal()"},
		{"picture_htf_live.go", "", "noteLiveBarsForPepPass("},
		{"auto_trader_orders.go", "executeDecisionWithRecordAt", "pepClaimBeforeSend("},
		{"auto_trader_orders.go", "openEntryWithRecord", "pepMarkSending("},
		{"entry_admission.go", "admitChain", "pepAdmitRefusal("},
	} {
		var body string
		if tc.fn == "" {
			body = fileText(t, tc.file)
		} else {
			body = funcBody(t, tc.file, tc.fn)
		}
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s %s does not call %s: pep is unwired there", tc.file, tc.fn, tc.want)
		}
	}
}

// pep never uses the mentor's entry route (Origin=mentor armed rows would attach
// the mentor exit drive, which moves stops) and rests no entry: its code does not
// touch the armed ledger or the mentor's ownership/exit-drive state.
func TestPepEntryPathNeverTouchesTheArmedLedgerOrMentorRoute(t *testing.T) {
	for _, f := range []string{"pep_tick.go", "pep_mode.go", "pep_admit.go", "pep_truth.go"} {
		for _, ln := range strings.Split(fileText(t, f), "\n") {
			code := strings.TrimSpace(ln)
			if strings.HasPrefix(code, "//") {
				continue
			}
			for _, banned := range []string{"ArmedOrders()", "isMentorArmOrigin", "MentorSourced", "mentorLivePos", "positionMentorOwned", "mentorExit"} {
				if strings.Contains(code, banned) {
					t.Errorf("%s uses %s outside a comment: %s", f, banned, code)
				}
			}
		}
	}
}

func fileText(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// funcBody is the text of the first top-level func in file whose declaration
// contains "<fn>(".
func funcBody(t *testing.T, file, fn string) string {
	t.Helper()
	lines, ok := bodyOf(fileText(t, file), fn)
	if !ok {
		t.Fatalf("%s: no func %s", file, fn)
	}
	return strings.Join(lines, "\n")
}

// ── the unreadable-time path: pepDayNow fails closed ───────────────────────

func TestPepDayNowIsTheChicagoCalendarDay(t *testing.T) {
	// 2026-11-01 05:30 UTC is 00:30 CDT on the 1st; 2026-11-02 05:30 UTC is 23:30 CST on the 1st
	if got := pepDayNow(time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)); got != "2026-11-01" {
		t.Fatalf("day = %q", got)
	}
	if got := pepDayNow(time.Date(2026, 11, 2, 5, 30, 0, 0, time.UTC)); got != "2026-11-01" {
		t.Fatalf("DST fall-back day = %q, want 2026-11-01", got)
	}
	if got := market.IsCMEFuturesSymbol("MNQ"); !got {
		t.Fatal("fixture")
	}
}
