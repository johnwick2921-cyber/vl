package trader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/store"
	ntTrader "nofx/trader/ninjatrader"
)

// ── 0B (2026-09-02) — EXIT SANITY ────────────────────────────────────────────
// C1: MIN_SL_ATR_MULT = 1.0×ATR5m was [C] code-canon with no citation. Round-7
// research tests the day-trade range at 1.5–2.5×ATR; below 1.0× stop-out rates
// exceed 60% on noise alone. Own tape: 6 of 8 losers had MAE beyond the stop.
// Owner ruling: the floor moves to 1.5×ATR5m.

// zeroBArmTrader builds a trader whose gate chain reaches the min-SL leg:
// plan_mode advisory (no bias gate), no quality floor, R:R comfortably ≥ 2.
func zeroBArmTrader(t *testing.T) *AutoTrader {
	t.Helper()
	return &AutoTrader{
		id: "t1", exchange: "ninjatrader",
		config: AutoTraderConfig{NinjaTraderSymbol: "MNQ", StrategyConfig: &store.StrategyConfig{
			DayPlan: &store.DayPlanConfig{PlanEnabled: true},
		}},
	}
}

// zeroBScenario is a long sweep_reclaim arm at `entry` with `stop` and a target
// far enough away that the R:R gate (ARM_MIN_RR 2.0) always passes.
func zeroBScenario(entry, stop float64) (kernel.PlanScenario, kernel.PlanArmLeg) {
	risk := entry - stop
	leg := kernel.PlanArmLeg{Entry: entry, Stop: stop, Target: entry + 4*risk}
	sc := kernel.PlanScenario{
		ID: "S1", Condition: "sweep_reclaim", Direction: "long", Quality: "A",
		Confirm: &kernel.PlanConfirm{Rule: "touch", RefPrice: entry, Side: "below"},
		Arm:     &kernel.PlanArmSpec{Enabled: true, Entry: entry, Stop: stop, Target: leg.Target, WaitConfirm: true},
	}
	return sc, leg
}

// E1 — THE PIN. ATR5m = A = 20.0. A stop 1.2×A from entry passed the arm gate
// under the 1.0× floor and must be REFUSED under 1.5×; a stop at 1.6×A passes
// either way. MUST FAIL on the pre-0B tree (1.2×A returns "" = allowed).
func TestZeroBPinStopFloor(t *testing.T) {
	const atr5m = 20.0
	const entry = 29000.0
	at := zeroBArmTrader(t)

	if got := kernel.MinSLATRMult(); got != 1.5 {
		t.Fatalf("0B: the resolved min-SL floor is %.2f×ATR5m, want 1.5 (C1 owner ruling; env MIN_SL_ATR_MULT unset)", got)
	}

	// 1.2×ATR — inside the new floor, outside the old one.
	scTight, legTight := zeroBScenario(entry, entry-1.2*atr5m)
	verdict := at.armGateVerdictFor(scTight, legTight, "", nil, atr5m, "", at.config.StrategyConfig, "NY")
	if verdict == "" {
		t.Fatalf("0B: a stop 1.2×ATR5m (%.1f pts) from entry must be REFUSED by the 1.5× floor — the gate allowed it (old 1.0× canon)", 1.2*atr5m)
	}
	if !strings.Contains(verdict, "too close") {
		t.Fatalf("the refusal must name the min-SL leg, got %q", verdict)
	}

	// 1.6×ATR — passes both floors (regression pin: the floor must not refuse a sane stop).
	scWide, legWide := zeroBScenario(entry, entry-1.6*atr5m)
	if v := at.armGateVerdictFor(scWide, legWide, "", nil, atr5m, "", at.config.StrategyConfig, "NY"); v != "" {
		t.Fatalf("a stop 1.6×ATR5m from entry must pass the gate, got %q", v)
	}
}

// ── E2 — STOP ANCHORED TO SEATED STRUCTURE ──────────────────────────────────

func TestZeroBStopAnchoredToSeatedStructure(t *testing.T) {
	const atr5m, tick = 20.0, 0.25
	const mult = 1.5
	clr := float64(kernel.MinSLTickClearance) * tick // 0.50
	levels := []kernel.PlanLevel{
		{Price: 28990, Label: "PDL"}, // 10 pts below a 29000 long entry — TIGHTER than the ATR floor
		{Price: 28950, Label: "ONL"}, // 50 pts below — farther
		{Price: 29060, Label: "PDH"}, // above (wrong side for a long)
	}

	// (a) structure TIGHTER than the floor → the ATR floor binds at 1.5×ATR.
	c := composeArmStop("long", 29000, 28995, atr5m, tick, levels, mult, kernel.MinSLTickClearance, 3.0)
	if c.Bound != "atr_floor" {
		t.Fatalf("a 10-pt anchor under a 30-pt floor must let the FLOOR bind, got %+v", c)
	}
	if got := 29000 - c.Stop; got+1e-9 < mult*atr5m {
		t.Fatalf("stop distance %.2f < %.2f = %.1f×ATR5m", got, mult*atr5m, mult)
	}
	if c.AnchorPrice != 28990 || c.AnchorLabel != "PDL" {
		t.Fatalf("the nearest risk-side level must still be reported: %+v", c)
	}

	// (b) structure WIDER than the floor → the anchor binds, beyond the level.
	wide := []kernel.PlanLevel{{Price: 28960, Label: "ONL"}} // 40 pts below > 30-pt floor
	c = composeArmStop("long", 29000, 28995, atr5m, tick, wide, mult, kernel.MinSLTickClearance, 3.0)
	if c.Bound != "anchor" || c.Stop != 28960-clr {
		t.Fatalf("the anchor must bind at %.2f (level 28960 − %.2f clearance), got %+v", 28960-clr, clr, c)
	}

	// (c) SHORT mirror: the risk side is ABOVE the entry.
	shortLevels := []kernel.PlanLevel{{Price: 29040, Label: "PDH"}, {Price: 28900, Label: "PDL"}}
	c = composeArmStop("short", 29000, 29005, atr5m, tick, shortLevels, mult, kernel.MinSLTickClearance, 3.0)
	if c.Bound != "anchor" || c.Stop != 29040+clr {
		t.Fatalf("short: the anchor must bind at %.2f, got %+v", 29040+clr, c)
	}

	// (d) DEAD ZONE: nothing within 3×ATR (60 pts) on the risk side → unanchored,
	// the ATR floor governs, and no level is invented.
	dead := []kernel.PlanLevel{{Price: 28800, Label: "far"}} // 200 pts away
	c = composeArmStop("long", 29000, 28995, atr5m, tick, dead, mult, kernel.MinSLTickClearance, 3.0)
	if !c.Unanchored || c.AnchorPrice != 0 || c.Bound != "atr_floor" {
		t.Fatalf("dead zone must be stop_unanchored on the ATR floor, got %+v", c)
	}
	if !strings.Contains(armStopCompositionLine("NY", "S1", 1, "long", c, atr5m, mult), "stop_unanchored") {
		t.Fatal("the dead-zone case must log stop_unanchored")
	}

	// (e) NEVER TIGHTENS: an authored stop wider than both legs survives.
	c = composeArmStop("long", 29000, 28900, atr5m, tick, levels, mult, kernel.MinSLTickClearance, 3.0)
	if c.Stop != 28900 || c.Bound != "authored" {
		t.Fatalf("composition must never tighten the authored stop, got %+v", c)
	}

	// (f) the log line carries stop · anchor · floor · binding side.
	c = composeArmStop("long", 29000, 28995, atr5m, tick, wide, mult, kernel.MinSLTickClearance, 3.0)
	line := armStopCompositionLine("NY", "S1", 1, "long", c, atr5m, mult)
	for _, want := range []string{"🛑 arm stop NY S1 leg 1 long", "stop 28959.50", "ONL 28960.00", "atr_floor 28970.00", "bound=anchor", "authored 28995.00 WIDENED"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q missing %q", line, want)
		}
	}
}

// ── E6 — the R:R gate still refuses < 2.0 WITH the wider stop ───────────────

func TestZeroBRRGateRefusesWithTheWiderStop(t *testing.T) {
	const atr5m = 20.0
	at := zeroBArmTrader(t)
	// Authored: entry 29000, stop 28990 (10 pts), target 29030 → R:R 3.0 as
	// authored. Composition widens the stop to the 1.5×ATR floor (28970 = 30
	// pts), so the REAL R:R is 30/30 = 1.0 — the gate must refuse it.
	entry, target := 29000.0, 29030.0
	comp := composeArmStop("long", entry, 28990, atr5m, 0.25, nil, kernel.MinSLATRMult(), kernel.MinSLTickClearance, 3.0)
	if comp.Stop != entry-1.5*atr5m {
		t.Fatalf("fixture: the floor must bind, got %+v", comp)
	}
	sc, leg := zeroBScenario(entry, comp.Stop)
	leg.Target, sc.Arm.Target = target, target
	if v := at.armGateVerdictFor(sc, leg, "", nil, atr5m, "", at.config.StrategyConfig, "NY"); !strings.Contains(v, "below arm min") {
		t.Fatalf("R:R 1.0 with the widened stop must be refused by ARM_MIN_RR, got %q", v)
	}
	// D7 VERIFY-ONLY: the resolved floor is the owner's 2.0, unchanged by 0B.
	// MIGRATED 2026-09-03 (R1): the floor now comes from the Studio value via
	// ONE resolver rather than the ARM_MIN_RR env var, which is deleted. The
	// assertion's intent — 0B changes NO R:R value — is unchanged, and the
	// bound strategy still resolves 2.0.
	cfgRR := &store.StrategyConfig{}
	cfgRR.RiskControl.MinRiskRewardRatio = 2.0
	if got := at.armMinRRFor(cfgRR); got != 2.0 {
		t.Fatalf("the resolved floor is %.2f, want the owner-ruled 2.0 (0B changes NO R:R value)", got)
	}
}

// ── E3 — BE and the ATR trail are INERT: no move_stop reaches the wire ──────

func TestZeroBSuspendedMechanismsNeverReachTheWire(t *testing.T) {
	ResetExitMechSuspendNoticeForTest()
	sent := 0
	prev := moveStopWire
	moveStopWire = func(_ *ntTrader.TCPTrader, _ string, _ float64) error { sent++; return nil }
	t.Cleanup(func() { moveStopWire = prev })

	beOn := true
	at := &AutoTrader{id: "t1", exchange: "ninjatrader", config: AutoTraderConfig{
		NinjaTraderSymbol: "MNQ",
		StrategyConfig:    &store.StrategyConfig{RiskControl: store.RiskControlConfig{BreakevenEnabled: &beOn, BreakevenTriggerPoints: 40}},
	}}
	// The trigger DID fire: +60 pts on a 40-pt trigger.
	if fire, pts := breakevenTrigger(at.config.StrategyConfig.RiskControl, "long", 29000, 29060); !fire || pts != 60 {
		t.Fatalf("fixture: the BE trigger must fire (fire=%v pts=%.0f)", fire, pts)
	}
	at.maybeMoveStopToBreakeven("MNQ", "long", 29000, 29060)
	if sent != 0 {
		t.Fatalf("BE is SUSPENDED — %d move_stop frame(s) reached the wire", sent)
	}
	// The trail ratchet condition likewise computes a level and sends nothing.
	if emit, level := trailDecide("long", 29050, 0, 29000, true); !emit || level != 29050 {
		t.Fatalf("fixture: the trail ratchet must emit (emit=%v level=%.2f)", emit, level)
	}
	if at.exitMechSuspendedRefuse("atr-trail", "fixture ratchet") != true {
		t.Fatal("the trail guard must refuse while suspended")
	}
	if sent != 0 {
		t.Fatalf("trail is SUSPENDED — %d move_stop frame(s) reached the wire", sent)
	}
	// Un-suspending restores both (the knobs are retained, not deleted). The BE
	// path resolves the broker AFTER the guard, so the fixture supplies one.
	t.Setenv("EXIT_MECHS_SUSPENDED", "0")
	ResetExitMechSuspendNoticeForTest()
	at.trader = &ntTrader.TCPTrader{}
	at.breakevenDone = nil
	at.maybeMoveStopToBreakeven("MNQ", "long", 29000, 29060)
	if sent == 0 {
		t.Fatal("EXIT_MECHS_SUSPENDED=0 must restore the BE path (suspended, not deleted)")
	}
}

// ── E3b — the wire seam is the ONLY path to the broker (source pin) ─────────

func TestZeroBBothMechanismsGoThroughTheWireSeam(t *testing.T) {
	for _, f := range []string{"auto_trader.go", "auto_trader_trailing.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		if strings.Contains(src, "ntTCP.MoveStopToBreakeven(") {
			t.Errorf("%s calls the broker directly — both suspended mechanisms must go through moveStopWire so the fixture can prove zero frames", f)
		}
		if !strings.Contains(src, "moveStopWire(") {
			t.Errorf("%s must reach the broker through moveStopWire", f)
		}
		if !strings.Contains(src, "exitMechSuspendedRefuse(") {
			t.Errorf("%s must carry the 0B suspension guard", f)
		}
	}
}

// ── E4 — size resolves to 1 ─────────────────────────────────────────────────

func TestZeroBContractSizeResolvesToOne(t *testing.T) {
	// The live production contradiction: arm-leg capacity resolved 1 while
	// order sizing resolved 2 (unset max_contracts_per_order → maxFuturesContracts).
	if got := kernel.ResolveMaxContracts(0, 2); got != 1 {
		t.Fatalf("order sizing must resolve to 1 under the Stage-A cap, got %d", got)
	}
	if got := kernel.ResolveMaxContracts(5, 2); got != 1 {
		t.Fatalf("an explicit max_contracts_per_order must still be clamped to the Stage-A cap, got %d", got)
	}
	if got := splitLegCapacity(0); got != 1 {
		t.Fatalf("arm-leg capacity %d, want 1 (unchanged by 0B)", got)
	}
	// Stage B is a knob, not a code change.
	t.Setenv("STAGE_A_CONTRACT_CAP", "2")
	if got := kernel.ResolveMaxContracts(0, 2); got != 2 {
		t.Fatalf("STAGE_A_CONTRACT_CAP=2 must raise the ceiling, got %d", got)
	}
}

// ── E5 — RE-ARM AFTER BOOT SWEEP ────────────────────────────────────────────
//
// C5: swept rows went to `cancelled`, and cancelled rows re-authorize only on a
// plan-version change (manual-cancel-wins) — so a restart during a live setup
// killed that setup until the next read. On 2026-09-02 00:16 that rule would
// have meant no position 587. The owner's cancels must still stick; the
// machine's own boot housekeeping must not.
func TestZeroBReArmAfterBootSweep(t *testing.T) {
	ResetBootSweepForTest()
	st, err := store.New(filepath.Join(t.TempDir(), "rearm.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ledger := st.ArmedOrders()
	const planID, version = "2026-09-02:ASIA:t1", 7
	now := time.Now()

	// Two pre-boot WORKING arms (a dead process's boot id) + one row the OWNER
	// cancelled in the same chain.
	seed := func(scenario, signal, state, reason, bootID string) int64 {
		row := &store.ArmedOrderDB{
			TraderID: "t1", PlanID: planID, Version: version, Session: "ASIA",
			Scenario: scenario, Side: "long", EntryPx: 29044, StopPx: 29014, TargetPx: 29104,
			State: state, StateReason: reason, EntryClass: "armed_fill", SignalID: signal,
			CreatedAt: now, UpdatedAt: now, LegIndex: 0, LegCount: 1, Kind: "limit", BootID: bootID,
		}
		if err := ledger.UpsertArm(row); err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	s1 := seed("S1", "sig-old-1", "working", "", "dead-boot")
	s3 := seed("S3", "sig-old-3", "working", "", "dead-boot")
	owner := seed("S9", "sig-owner", "cancelled", "owner cancelled in NT8", "dead-boot")

	// 1. THE SWEEP cancels the two pre-boot rows AT THE BROKER (wire recorder).
	var cancelled []string
	at := &AutoTrader{id: "t1", exchange: "ninjatrader", store: st,
		config: AutoTraderConfig{NinjaTraderSymbol: "MNQ", StrategyConfig: &store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}}}
	n := at.sweepPreBootArmsWith(ledger, func(signalID string) error { cancelled = append(cancelled, signalID); return nil })
	if n != 2 {
		t.Fatalf("the sweep must cancel the two pre-boot WORKING rows, swept %d", n)
	}
	if len(cancelled) != 2 || !strings.Contains(strings.Join(cancelled, ","), "sig-old-1") || !strings.Contains(strings.Join(cancelled, ","), "sig-old-3") {
		t.Fatalf("both old broker orders must be cancelled at the wire, got %v", cancelled)
	}
	for _, id := range []int64{s1, s3} {
		row := armedRowByID(t, ledger, id)
		if !store.IsTerminalArmState(row.State) {
			t.Fatalf("row %d must be terminal after the sweep, got %q", id, row.State)
		}
		if !store.IsBootSweepReason(row.StateReason) {
			t.Fatalf("row %d must carry the boot_sweep reason, got %q", id, row.StateReason)
		}
	}

	// 2. THE EXECUTOR re-authors the SAME scenarios at the SAME plan version.
	for _, sc := range []string{"S1", "S3", "S9"} {
		if err := ledger.UpsertArm(&store.ArmedOrderDB{
			TraderID: "t1", PlanID: planID, Version: version, Session: "ASIA",
			Scenario: sc, Side: "long", EntryPx: 29044, StopPx: 29014, TargetPx: 29104,
			State: "armed", EntryClass: "armed_fill", CreatedAt: now, UpdatedAt: now,
			LegIndex: 0, LegCount: 1, Kind: "limit",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// 3. The swept rows are ARMED again with a FRESH identity; the owner's is not.
	//
	// D5 (owner ruling 2026-09-04) — CHANGED HERE. A swept row REACHED the
	// broker, so it is no longer revived in place: it keeps its cancelled
	// record forever and the re-authorization lands as the next placement.
	// The subject of this test is unchanged — the swept scenario is armed
	// again with no dead broker identity — so it now follows the SCENARIO
	// rather than the row id.
	for _, id := range []int64{s1, s3} {
		swept := armedRowByID(t, ledger, id)
		row := latestArmedForScenario(t, ledger, swept.PlanID, swept.Scenario)
		if row.State != "armed" {
			t.Fatalf("swept row %d must re-arm under the same version, got state %q reason %q", id, row.State, row.StateReason)
		}
		if row.SignalID != "" {
			t.Fatalf("a re-armed row must drop the dead broker identity, still carries %q", row.SignalID)
		}
		if row.StateReason != "" {
			t.Fatalf("a re-armed row must clear the sweep reason, got %q", row.StateReason)
		}
	}
	if row := armedRowByID(t, ledger, owner); row.State != "cancelled" || row.SignalID != "sig-owner" {
		t.Fatalf("MANUAL-CANCEL-WINS: the owner's cancel must stay sticky at the same version, got %+v", row)
	}

	// 4. A NEW plan version still re-authorizes the owner's row (unchanged law).
	if err := ledger.UpsertArm(&store.ArmedOrderDB{
		TraderID: "t1", PlanID: planID, Version: version + 1, Session: "ASIA",
		Scenario: "S9", Side: "long", EntryPx: 29044, StopPx: 29014, TargetPx: 29104,
		State: "armed", EntryClass: "armed_fill", CreatedAt: now, UpdatedAt: now,
		LegIndex: 0, LegCount: 1, Kind: "limit",
	}); err != nil {
		t.Fatal(err)
	}
	// D5: the owner's cancelled row had a broker identity (sig-owner), so it
	// keeps its record and the new version's authorization is the NEXT
	// placement. The law under test — a new version DOES re-authorize — is
	// unchanged; only where the fresh row lives has moved.
	if prior := armedRowByID(t, ledger, owner); prior.State != "cancelled" || prior.SignalID != "sig-owner" {
		t.Fatalf("the owner's cancelled placement must keep its record: %+v", prior)
	}
	if row := latestArmedForScenario(t, ledger, planID, "S9"); row.State != "armed" || row.SignalID != "" {
		t.Fatalf("a new plan version must still re-authorize, as a fresh placement: %+v", row)
	}
}

func armedRowByID(t *testing.T, ledger *store.ArmedOrderStore, id int64) store.ArmedOrderDB {
	t.Helper()
	rows, err := ledger.ListForPlan("2026-09-02:ASIA:t1")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("row %d not found", id)
	return store.ArmedOrderDB{}
}

// ── OWNER RULINGS (2026-09-02) — the two recorded counters ──────────────────
//
//  1. ARM_STOP_ANCHOR_MAX_ATR 3.0 is PROVISIONAL [I]; reviewed at n≥30
//     stop_unanchored occurrences. 2. Arm refusals are the COST side of the
//     stop floor and are recorded per session-day and per class.
func TestZeroBRecordedCountersSurviveRestart(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "cnt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if got := store.CountFromSystemConfig(st, store.StopUnanchoredKey); got != 0 {
		t.Fatalf("fresh store must read 0, got %d", got)
	}
	for i := 1; i <= 3; i++ {
		n, err := store.IncStopUnanchored(st)
		if err != nil || n != i {
			t.Fatalf("stop_unanchored bump %d → %d (err %v)", i, n, err)
		}
	}
	if store.StopUnanchoredReviewN != 30 {
		t.Fatalf("the review trigger is n≥30 per the owner ruling, got %d", store.StopUnanchoredReviewN)
	}

	// Per-session, per-class refusal counters: R:R is separable from the rest,
	// and sessions do not bleed into each other.
	const tid, date = "t1", "2026-09-02"
	for i := 1; i <= 4; i++ {
		if n, err := store.IncArmRefusal(st, tid, date, "NY", "rr"); err != nil || n != i {
			t.Fatalf("NY rr bump %d → %d (err %v)", i, n, err)
		}
	}
	if _, err := store.IncArmRefusal(st, tid, date, "NY", "min_sl"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.IncArmRefusal(st, tid, date, "ASIA", "rr"); err != nil {
		t.Fatal(err)
	}
	if got := store.ArmRefusalCount(st, tid, date, "NY", "rr"); got != 4 {
		t.Fatalf("NY rr = %d, want 4", got)
	}
	if got := store.ArmRefusalCount(st, tid, date, "NY", "min_sl"); got != 1 {
		t.Fatalf("NY min_sl = %d, want 1 (classes must be separable)", got)
	}
	if got := store.ArmRefusalCount(st, tid, date, "ASIA", "rr"); got != 1 {
		t.Fatalf("ASIA rr = %d, want 1 (sessions must not bleed)", got)
	}
	if got := store.ArmRefusalCount(st, tid, "2026-09-03", "NY", "rr"); got != 0 {
		t.Fatalf("a different session-day must start at 0, got %d", got)
	}
	// A malformed row reads 0, never a fabricated figure.
	_ = st.SetSystemConfig(store.ArmRefusalKey(tid, date, "LONDON", "rr"), "not-a-number")
	if got := store.ArmRefusalCount(st, tid, date, "LONDON", "rr"); got != 0 {
		t.Fatalf("malformed counter must read 0, got %d", got)
	}
}

// latestArmedForScenario returns the newest row for a (plan, scenario) — the
// current placement, after D5 made a replacement its own row.
func latestArmedForScenario(t *testing.T, ledger *store.ArmedOrderStore, planID, scenario string) store.ArmedOrderDB {
	t.Helper()
	rows, err := ledger.ListNonTerminal("t1")
	if err != nil {
		t.Fatalf("list non-terminal: %v", err)
	}
	for i := range rows {
		if rows[i].PlanID == planID && rows[i].Scenario == scenario {
			return rows[i]
		}
	}
	t.Fatalf("no live row for %s/%s", planID, scenario)
	return store.ArmedOrderDB{}
}
