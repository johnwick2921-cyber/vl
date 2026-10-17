package trader

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/levelidentity"
	"nofx/market"
	"nofx/store"
)

// W-GEOMETRY-REFUSAL (2026-09-18) — executor-side tests. (a) the WARN, (b1)
// stable reference ids, (b2) the empty-tf wildcard, (b4) OFF byte-identical.

// TestGeometryRefIDsOnhRejectPlayAdmitted is the CTO's merge question
// (2026-09-18 09:2x CT): a plan authored TODAY at ONH carrying the ref| id must
// be ADMITTED — the stop composed from the structural stop rule (line − buffer,
// long), the target from the first distinct complete zone. The frozen map
// stores a reference LINE as lo/hi NULL + incomplete_width (measured on the
// owner's 09-18 LONDON v1 [A]); the knob synthesizes the zero-width band.
// OFF refuses byte-identical.
func TestGeometryRefIDsOnhRejectPlayAdmitted(t *testing.T) {
	p := func(v float64) *float64 { return &v }
	// The ONH identity line AS THE MAP EMITS IT for a reference kind:
	// lo=hi=price, tf filled from the base (1m), no formation close, ref| id.
	id := kernel.ReferenceLevelID("MNQ", "ONH", 29897, 29897, "2026-09-17", "1m")
	identity := kernel.PlanLevel{Symbol: refStr("MNQ"), Kind: refStr("ONH"), Lo: p(29897), Hi: p(29897), OriginDate: refStr("2026-09-17"), TF: refStr("1m"), Label: "ONH", Price: 29897, ID: id, Names: []string{"ONH"}}
	// The frozen map: ONH as a NULL-WIDTH line (lo/hi null, incomplete_width),
	// source tf "" — and one complete target zone above.
	doc := &kernel.PlanDoc{
		IdentityLevels: []kernel.PlanLevel{identity},
		Zones: &kernel.LevelZoneMap{Zones: []kernel.LevelZone{
			{Anchor: 29897, Incomplete: true, Sources: []kernel.ZoneSource{{Kind: "ONH", Price: 29897, Label: "ONH", TF: ""}}},
			{Anchor: 29950, Lo: p(29950), Hi: p(29954), Sources: []kernel.ZoneSource{{Kind: "SUPPLY", Price: 29952, Label: "Supply·1h", TF: "1h"}}},
		}},
	}
	sc := kernel.PlanScenario{ID: "S1", LevelID: id, Condition: "reject", Direction: "long"}

	// OFF (legacy): the line is unusable → refused.
	if idx, why := ArmGeometryVerdict(doc, sc, false); why == "" {
		t.Fatalf("knob OFF must refuse the null-width line (today's behaviour), got idx=%d", idx)
	}
	// ON: admitted at zone 0.
	idx, why := ArmGeometryVerdict(doc, sc, true)
	if why != "" || idx != 0 {
		t.Fatalf("a NEW ONH reject play must be ADMITTED with the knob ON: idx=%d why=%s", idx, why)
	}
	// The stop must be composed from the structural stop rule: line − buffer,
	// and the target = first distinct complete zone above.
	policy := store.StructuralStopPolicy{BufferPoints: 5, BufferKnown: true, CostPoints: 2, CostKnown: true, MinRR: 2}
	r := ComposeLevelFadeGeometryWith(doc, sc, kernel.PlanArmLeg{Entry: 29897}, policy, 20, 0.25, 2, true)
	if r.Reason != "" {
		t.Fatalf("the admitted ONH play must compose, got refuse: %s (%s)", r.Reason, r.Detail)
	}
	if r.Stop == nil || *r.Stop != 29892 {
		t.Fatalf("stop must be ONH − buffer (29897−5=29892), got %v", r.Stop)
	}
	if r.Target == nil || *r.Target != 29950 {
		t.Fatalf("target must be the first distinct complete zone (29950), got %v", r.Target)
	}
	if r.StopSource != "zone_edge" {
		t.Fatalf("stop source must be zone_edge (the structural rule), got %s", r.StopSource)
	}
}

// TestGeometryRefAdmissionDoesNotMutateSharedDoc is the CTO's F8 BLOCKER pin
// (re-check 2026-09-18): the zero-width admission must compose from a LOCAL
// copy of the zone. doc.Zones is a POINTER into the shared plan, and the old
// in-place edge write made a short reject's composed target depend on whether
// the ONH long had been composed first — Demand 29805 before, the
// now-"complete" ONH line 29897 after. Two pins: (1) the frozen map is
// byte-identical before and after a verdict; (2) composing the reject →
// admitting the ONH → composing the reject again yields the IDENTICAL stop and
// target.
func TestGeometryRefAdmissionDoesNotMutateSharedDoc(t *testing.T) {
	p := func(v float64) *float64 { return &v }
	nowMs := time.Now().UnixMilli()
	onhID := kernel.ReferenceLevelID("MNQ", "ONH", 29897, 29897, "2026-09-17", "1m")
	onh := kernel.PlanLevel{Symbol: refStr("MNQ"), Kind: refStr("ONH"), Lo: p(29897), Hi: p(29897), OriginDate: refStr("2026-09-17"), TF: refStr("1m"), Label: "ONH", Price: 29897, ID: onhID, Names: []string{"ONH"}}
	supID, reason := levelidentity.ID(kernel.IdentityInputs(kernel.PlanLevel{
		Symbol: refStr("MNQ"), Kind: refStr("SUPPLY"), Lo: p(29950), Hi: p(29954),
		OriginDate: refStr("2026-09-17"), TF: refStr("1h"), FormedCloseMs: &nowMs,
	}))
	if supID == nil {
		t.Fatalf("supply identity refused: %s", reason)
	}
	sup := kernel.PlanLevel{Symbol: refStr("MNQ"), Kind: refStr("SUPPLY"), Lo: p(29950), Hi: p(29954), OriginDate: refStr("2026-09-17"), TF: refStr("1h"), Label: "Supply·1h", Price: 29952, ID: supID, FormedCloseMs: &nowMs, Names: []string{"Supply·1h"}}
	doc := &kernel.PlanDoc{
		IdentityLevels: []kernel.PlanLevel{onh, sup},
		Zones: &kernel.LevelZoneMap{Zones: []kernel.LevelZone{
			{Anchor: 29897, Incomplete: true, Sources: []kernel.ZoneSource{{Kind: "ONH", Price: 29897, Label: "ONH", TF: ""}}},
			{Anchor: 29950, Lo: p(29950), Hi: p(29954), Sources: []kernel.ZoneSource{{Kind: "SUPPLY", Price: 29952, Label: "Supply·1h", TF: "1h"}}},
			{Anchor: 29805, Lo: p(29801), Hi: p(29805), Sources: []kernel.ZoneSource{{Kind: "DEMAND", Price: 29803, Label: "Demand·1h", TF: "1h"}}},
		}},
	}
	policy := store.StructuralStopPolicy{BufferPoints: 5, BufferKnown: true, CostPoints: 2, CostKnown: true, MinRR: 2}
	onhSc := kernel.PlanScenario{ID: "S1", LevelID: onhID, Direction: "long"}
	supSc := kernel.PlanScenario{ID: "S2", LevelID: supID, Direction: "short"}
	reject := func() store.StructuralGeometryRecord {
		return ComposeLevelFadeGeometryWith(doc, supSc, kernel.PlanArmLeg{Entry: 29950}, policy, 20, 0.25, 2, true)
	}

	before, err := json.Marshal(doc.Zones)
	if err != nil {
		t.Fatal(err)
	}
	// The short reject FIRST — targets the Demand zone at 29805.
	r0 := reject()
	if r0.Reason != "" {
		t.Fatalf("short reject must compose, got refuse: %s (%s)", r0.Reason, r0.Detail)
	}
	if r0.Target == nil || *r0.Target != 29805 {
		t.Fatalf("short reject must target the Demand zone (29805), got %v", r0.Target)
	}
	// The ONH long admission (zero-width band) — may NOT mutate the shared map.
	radm := ComposeLevelFadeGeometryWith(doc, onhSc, kernel.PlanArmLeg{Entry: 29897}, policy, 20, 0.25, 2, true)
	if radm.Reason != "" {
		t.Fatalf("ONH long must admit and compose, got refuse: %s (%s)", radm.Reason, radm.Detail)
	}
	if radm.Stop == nil || *radm.Stop != 29892 {
		t.Fatalf("ONH stop must be 29897−5=29892, got %v", radm.Stop)
	}
	// Pin 1: the frozen map is byte-identical after the admission verdict.
	after, err := json.Marshal(doc.Zones)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("admission mutated the shared zone map:\nbefore=%s\nafter =%s", before, after)
	}
	// Pin 2: the SAME reject composed again gets the SAME stop and target —
	// scenario order must never change composed geometry.
	r1 := reject()
	if r1.Reason != "" || r1.Stop == nil || r0.Stop == nil || *r1.Stop != *r0.Stop || r1.Target == nil || *r1.Target != *r0.Target {
		t.Fatalf("order dependence: reject after admission stop=%v target=%v, reject before stop=%v target=%v", r1.Stop, r1.Target, r0.Stop, r0.Target)
	}
}

// TestGeometryRefAmbiguousRefusesNeverPicks (a): when the wildcard makes a
// second zone match, the verdict is entry_zone_ambiguous — a REFUSAL, never a
// pick of either zone (fail-closed).
func TestGeometryRefAmbiguousRefusesNeverPicks(t *testing.T) {
	p := func(v float64) *float64 { return &v }
	id := kernel.ReferenceLevelID("MNQ", "ONH", 29897, 29897, "2026-09-17", "1m")
	identity := kernel.PlanLevel{Symbol: refStr("MNQ"), Kind: refStr("ONH"), Lo: p(29897), Hi: p(29897), OriginDate: refStr("2026-09-17"), TF: refStr("1m"), Label: "ONH", Price: 29897, ID: id, Names: []string{"ONH"}}
	// Two zones BOTH match once the empty tf is a wildcard: zone 0 carries the
	// empty-tf source (wildcard), zone 1 carries a source whose tf equals the
	// identity tf (exact).
	doc := &kernel.PlanDoc{
		IdentityLevels: []kernel.PlanLevel{identity},
		Zones: &kernel.LevelZoneMap{Zones: []kernel.LevelZone{
			{Anchor: 29897, Incomplete: true, Sources: []kernel.ZoneSource{{Kind: "ONH", Price: 29897, Label: "ONH", TF: ""}}},
			{Anchor: 29897, Incomplete: true, Sources: []kernel.ZoneSource{{Kind: "ONH", Price: 29897, Label: "ONH", TF: "1m"}}},
		}},
	}
	sc := kernel.PlanScenario{ID: "S1", LevelID: id}
	if idx, why := ArmGeometryVerdict(doc, sc, true); idx != -1 || why != "entry_zone_ambiguous" {
		t.Fatalf("two matching zones must REFUSE as ambiguous, never pick: idx=%d why=%s", idx, why)
	}
}

func refPtr(v float64) *float64 { return &v }
func refStr(v string) *string   { return &v }

// nilLevelIDOverride returns the one-setup fixture doc with S1's level_id set
// to nil — the exact live shape of a reject play authored at ONH/ONL (LONDON
// v1 S1 2026-09-18 [A]).
func nilLevelIDOverride(t *testing.T) string {
	t.Helper()
	var d kernel.PlanDoc
	if err := json.Unmarshal([]byte(oneSetupFixtureDoc()), &d); err != nil {
		t.Fatal(err)
	}
	for i := range d.Scenarios {
		if d.Scenarios[i].ID == "S1" {
			d.Scenarios[i].LevelID = nil
		}
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestGeometryRefusalWarnOncePerKeyChange (a): a reject scenario with LevelID
// nil produces exactly ONE ⚔️ arm REFUSED WARN across TWO cycles (de-duped by
// (geometry key, reason) exactly like the other arm refusals). RED on dev
// b70fc6ca: 0 WARN lines (INFO-only). Driven on a SYNTHETIC in-session clock so
// the test never depends on the wall clock (the shared drive harness skips
// outside a live session window).
func TestGeometryRefusalWarnOncePerKeyChange(t *testing.T) {
	oneSetupFixtureDocOverride = nilLevelIDOverride(t)
	entry := 29010.0
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	cfg.RiskControl.MinRiskRewardRatio = 2
	structuralTestPolicy(&cfg, 5)
	oneSetupOff(&cfg) // isolate geometry from the independently tested selection checks
	at, st := resetTrader(t, cfg)
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC) // 09:00 CT, inside NY
	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = nil })
	sess, ok := at.sessionRegistry(now).ActiveSession(now)
	if !ok {
		t.Fatal("no active session on the synthetic clock")
	}
	cfg.DayPlan.SessionsEnabled = []string{sess.Name}
	trueV := true
	cfg.DayPlan.Sessions = []store.DayPlanSessionOverride{{Session: sess.Name, Enable: &trueV}}
	td, _ := kernel.PlanChainTradeDate(sess, now)
	pid := store.MakePlanIDForTrader(at.id, td, sess.Name)
	if _, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: td, Session: sess.Name, StrategyID: at.id, Lifecycle: "active", Doc: oneSetupFixtureDocOverride, CreatedAt: now.Add(-30 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	installActivePlanProviderAt(at, st, func() time.Time { return now })
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(symbol string, tf string, n int) []market.Kline { return oneSetupFixtureTape(now) }
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	at.oneSetupFactsForTest = func(now time.Time) oneSetupTestFacts {
		return oneSetupTestFacts{Price: entry, BandPts: 100, Candidates: []kernel.MapCandidate{{Price: entry, Names: []string{"PDL"}, Grade: "A"}}, Permission: map[string]kernel.FadeVerdict{"S1": {Evaluated: true, Permitted: true}}}
	}
	buf := captureTraderLog(t)
	at.maybeManageArmedOrdersAt(nil, now)
	at.maybeManageArmedOrdersAt(nil, now) // second cycle — same key/reason → no second WARN

	log := buf.String()
	if !strings.Contains(log, "geometry_no_provenance (scenario_level_id_missing)") {
		t.Fatalf("the geometry refusal must WARN with its reason; got:\n%s", log)
	}
	if n := strings.Count(log, "⚔️ arm REFUSED "); n != 1 {
		t.Fatalf("exactly ONE WARN across two cycles (de-duped), got %d:\n%s", n, log)
	}
}

// TestEnsureReferenceLevelIDsStableAndScoped (b1): anchor kinds with no
// formation close get a STABLE ref| id; non-anchor kinds stay NULL; strict ids
// are never overwritten.
func TestEnsureReferenceLevelIDsStableAndScoped(t *testing.T) {
	mk := func(kind string) kernel.MapCandidate {
		return kernel.MapCandidate{Identity: kernel.PlanLevel{
			Symbol: refStr("MNQ"), Kind: refStr(kind), Lo: refPtr(29400), Hi: refPtr(29410),
			OriginDate: refStr("2026-09-17"), TF: refStr("1m"), Label: kind, Price: 29405,
		}}
	}
	cs := []kernel.MapCandidate{mk("ONH"), mk("FVG")}
	kernel.EnsureReferenceLevelIDs(cs)
	if cs[0].ID == nil {
		t.Fatal("ONH (anchor kind, no formation close) must get a stable id")
	}
	if !strings.HasPrefix(*cs[0].ID, "ref|") {
		t.Fatalf("the fallback id must carry the ref| prefix: %s", *cs[0].ID)
	}
	if cs[1].ID != nil {
		t.Fatal("a non-anchor kind with NULL id must stay NULL")
	}
	again := []kernel.MapCandidate{mk("ONH")}
	kernel.EnsureReferenceLevelIDs(again)
	if *cs[0].ID != *again[0].ID {
		t.Fatalf("same inputs must yield the SAME id: %s vs %s", *cs[0].ID, *again[0].ID)
	}
	full := mk("ONH")
	nowMs := time.Now().UnixMilli()
	full.Identity.FormedCloseMs = &nowMs
	strict, _ := levelidentity.ID(kernel.IdentityInputs(full.Identity))
	full.ID = strict
	full.Identity.ID = strict
	kernel.EnsureReferenceLevelIDs([]kernel.MapCandidate{full})
	if *full.ID != *strict {
		t.Fatal("a strict id must never be overwritten by the reference id")
	}
}

// refAnchorDoc builds the frozen-map doc for the tf-wildcard tests.
func refAnchorDoc(sourceTF string) (*kernel.PlanDoc, *string) {
	id := kernel.ReferenceLevelID("MNQ", "eVWAP", 29400, 29410, "2026-09-17", "1m")
	identity := kernel.PlanLevel{Symbol: refStr("MNQ"), Kind: refStr("eVWAP"), Lo: refPtr(29400), Hi: refPtr(29410), OriginDate: refStr("2026-09-17"), TF: refStr("1m"), Label: "eVWAP", Price: 29405, ID: id}
	doc := &kernel.PlanDoc{
		IdentityLevels: []kernel.PlanLevel{identity},
		Zones: &kernel.LevelZoneMap{Zones: []kernel.LevelZone{{
			Lo: refPtr(29400), Hi: refPtr(29410),
			Sources: []kernel.ZoneSource{{Price: 29405, Label: "eVWAP", TF: sourceTF}},
		}}},
	}
	return doc, id
}

// TestGeometryTFWildcardExecutorOnly (b2): an EMPTY source tf refuses under the
// legacy contract and with the knob OFF (parity), and resolves with the knob ON.
// A NON-empty mismatched tf still refuses even with the knob ON.
func TestGeometryTFWildcardExecutorOnly(t *testing.T) {
	doc, id := refAnchorDoc("")
	sc := kernel.PlanScenario{ID: "S1", LevelID: id}

	if idx, why := ResolveEntryGeometryZone(doc, sc); why == "" {
		t.Fatalf("legacy must refuse the empty-tf source (today's behaviour), got idx=%d", idx)
	}
	if idx, why := ArmGeometryVerdict(doc, sc, false); why == "" {
		t.Fatalf("knob OFF must refuse (byte-identical parity), got idx=%d", idx)
	}
	if idx, why := ArmGeometryVerdict(doc, sc, true); why != "" || idx != 0 {
		t.Fatalf("knob ON must treat the empty source tf as a wildcard: idx=%d why=%s", idx, why)
	}

	docMismatch, id2 := refAnchorDoc("5m")
	scMismatch := kernel.PlanScenario{ID: "S1", LevelID: id2}
	if idx, why := ArmGeometryVerdict(docMismatch, scMismatch, true); why == "" {
		t.Fatalf("a non-empty mismatched tf must still refuse with the knob ON, got idx=%d", idx)
	}
}

// TestGeometryReferenceLevelsKnobResolution (b4): nil config or nil pointer →
// ON (owner default); explicit false → OFF.
func TestGeometryReferenceLevelsKnobResolution(t *testing.T) {
	if !(*store.DayPlanConfig)(nil).GeometryRefIDsEnabled() {
		t.Fatal("nil config must resolve ON (owner default)")
	}
	if !(&store.DayPlanConfig{}).GeometryRefIDsEnabled() {
		t.Fatal("nil pointer must resolve ON (owner default)")
	}
	off := false
	if (&store.DayPlanConfig{GeometryReferenceLevels: &off}).GeometryRefIDsEnabled() {
		t.Fatal("explicit false must resolve OFF")
	}
	on := true
	if !(&store.DayPlanConfig{GeometryReferenceLevels: &on}).GeometryRefIDsEnabled() {
		t.Fatal("explicit true must resolve ON")
	}
}

// TestGeometryRefBootLineReadsResolvedKnob (c): the per-trader boot line reads
// the resolved knob, never a literal.
func TestGeometryRefBootLineReadsResolvedKnob(t *testing.T) {
	off := false
	line := GeometryRefBootLine(&store.DayPlanConfig{GeometryReferenceLevels: &off})
	if !strings.Contains(line, "geom_ref_ids=off") {
		t.Fatalf("boot line must read the resolved OFF: %s", line)
	}
	if strings.Contains(GeometryRefBootLine(nil), "geom_ref_ids=off") {
		t.Fatal("nil config must read on(default)")
	}
	on := true
	if line := GeometryRefBootLine(&store.DayPlanConfig{GeometryReferenceLevels: &on}); !strings.Contains(line, "on(saved)") {
		t.Fatalf("an explicit true is a SAVED value and must read on(saved): %s", line)
	}
}

// oneSetupOnhDoc builds the F3 fixture: S1 reject at ONH carrying the ref| id,
// identity map with the ONH line AS THE MAP EMITS IT (lo=hi=price, tf 1m, no
// formation close), frozen map with the null-width ONH line zone + one complete
// target. Entry 29897, authored stop/target below/above.
func oneSetupOnhDoc(t *testing.T) (string, *string) {
	t.Helper()
	p := func(v float64) *float64 { return &v }
	id := kernel.ReferenceLevelID("MNQ", "ONH", 29897, 29897, "2026-09-17", "1m")
	identity := kernel.PlanLevel{Symbol: refStr("MNQ"), Kind: refStr("ONH"), Lo: p(29897), Hi: p(29897), OriginDate: refStr("2026-09-17"), TF: refStr("1m"), Label: "ONH", Price: 29897, ID: id, Names: []string{"ONH"}}
	d := kernel.PlanDoc{
		Bias:           kernel.PlanBias{Direction: "long"},
		IdentityLevels: []kernel.PlanLevel{identity},
		// StampAuthoredIdentity copies the machine metadata (kind, lo/hi, id)
		// onto the model's levels — the fixture mirrors that shape, because
		// LevelByReferenceID refuses a ref id on a kind-less level entry.
		Levels: []kernel.PlanLevel{{Price: 29897, Label: "ONH", ID: id, Symbol: refStr("MNQ"), Kind: refStr("ONH"), Lo: p(29897), Hi: p(29897), OriginDate: refStr("2026-09-17"), TF: refStr("1m")}},
		Zones: &kernel.LevelZoneMap{Zones: []kernel.LevelZone{
			{Anchor: 29897, Incomplete: true, Sources: []kernel.ZoneSource{{Kind: "ONH", Price: 29897, Label: "ONH", TF: ""}}},
			{Anchor: 29950, Lo: p(29950), Hi: p(29954), Sources: []kernel.ZoneSource{{Kind: "SUPPLY", Price: 29952, Label: "Supply·1h", TF: "1h"}}},
		}},
		Scenarios: []kernel.PlanScenario{{ID: "S1", LevelID: id, Condition: "reject", Direction: "long", Quality: "A", Trigger: "overnight high rejection", Invalid: "below 29897", Confirm: &kernel.PlanConfirm{Rule: "touch", RefPrice: 29897, Side: "above"}, Arm: &kernel.PlanArmSpec{Enabled: true, Entry: 29897, Stop: 29860, Target: 29950}}},
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return string(b), id
}

// TestOneSetupRefIDRejectPlayArms is the CTO's F3: with one-setup ON (its
// shipped default) and the knob ON, the ref-id ONH reject play must pass the
// one-setup consult (not level_unresolved) and ARM with the zone-edge stop
// (29897 − buffer 5 = 29892). Knob OFF → refused as today. RED before the
// oneSetupLevelRef fallback: declined every cycle with level_unresolved.
func TestOneSetupRefIDRejectPlayArms(t *testing.T) {
	docJSON, id := oneSetupOnhDoc(t)
	drive := func(geomOff bool) armPathGolden {
		oneSetupFixtureDocOverride = docJSON
		mutate := func(c *store.StrategyConfig) {
			// one-setup stays ON (the F3 point) — unlike the geometry-isolation
			// tests, nothing calls oneSetupOff here.
			structuralTestPolicy(c, 5)
			if geomOff {
				off := false
				c.DayPlan.GeometryReferenceLevels = &off
			}
		}
		hook := func(at *AutoTrader, _ *store.Store, _ string) {
			at.oneSetupFactsForTest = func(now time.Time) oneSetupTestFacts {
				return oneSetupTestFacts{Price: 29897, BandPts: 100, Candidates: []kernel.MapCandidate{{ID: id, Identity: kernel.PlanLevel{Symbol: refStr("MNQ"), Kind: refStr("ONH"), Lo: refPtr(29897), Hi: refPtr(29897), OriginDate: refStr("2026-09-17"), TF: refStr("1m"), Label: "ONH", Price: 29897, ID: id, Names: []string{"ONH"}}, Price: 29897, Names: []string{"ONH"}, Grade: "A"}}, Permission: map[string]kernel.FadeVerdict{"S1": {Evaluated: true, Permitted: true}}}
			}
			// Same tape trick as structuralFixture: close AT the entry so the
			// fade's invalidated-through-the-level gate does not fire on a tape
			// written for the other fixture's entry price.
			originalTape := market.FuturesBarsProvider
			market.FuturesBarsProvider = func(_ string, _ string, _ int) []market.Kline {
				bars := originalTape("MNQ", "1m", 80)
				for i := range bars {
					bars[i].Open = 29897
					bars[i].Close = 29897
					bars[i].High = 29897 + 10
					bars[i].Low = 29897 - 10
				}
				return bars
			}
		}
		g, _, _, _ := driveOneSetupArmPath(t, mutate, hook)
		return g
	}
	g := drive(false)
	if len(g.Rows) != 1 || g.Rows[0]["stop"] != 29892.0 {
		t.Fatalf("one-setup ON + knob ON must ARM the ONH reject play with the zone-edge stop 29892 (29897−5); rows=%+v", g.Rows)
	}
	goff := drive(true)
	if len(goff.Rows) != 0 {
		t.Fatalf("knob OFF must refuse as today; rows=%+v", goff.Rows)
	}
}

// TestGeometryRefWildcardDoesNotFlagMergedNames is the 2026-09-13 ASIA v9 S1 /
// v14 S1 regression pin (CTO 10:4x CT): the identity's merged member names
// (SWG-H·5m that also wears EQL·1h/EQL·4h) live in their OWN zones at the same
// price — that is the identity itself under aliases, NOT a competing entry
// zone. The match is exact (tf matches), so no ambiguity may fire; the play
// ADMITS.
func TestGeometryRefWildcardDoesNotFlagMergedNames(t *testing.T) {
	p := func(v float64) *float64 { return &v }
	nowMs := time.Now().UnixMilli()
	id, reason := levelidentity.ID(kernel.IdentityInputs(kernel.PlanLevel{
		Symbol: refStr("MNQ"), Kind: refStr("SWG-H"), Lo: p(29038), Hi: p(29038),
		OriginDate: refStr("2026-09-13"), TF: refStr("5m"), FormedCloseMs: &nowMs,
	}))
	if id == nil {
		t.Fatalf("strict identity refused: %s", reason)
	}
	identity := kernel.PlanLevel{Symbol: refStr("MNQ"), Kind: refStr("SWG-H"), Lo: p(29038), Hi: p(29038), OriginDate: refStr("2026-09-13"), TF: refStr("5m"), Label: "SWG-H·5m", Price: 29038, ID: id, FormedCloseMs: &nowMs, Names: []string{"SWG-H·5m", "EQL", "EQL·4h", "EQL·1h"}}
	doc := &kernel.PlanDoc{
		IdentityLevels: []kernel.PlanLevel{identity},
		Zones: &kernel.LevelZoneMap{Zones: []kernel.LevelZone{
			{Anchor: 29038, Lo: p(29032.16), Hi: p(29055.5), Sources: []kernel.ZoneSource{{Kind: "SWG-H", Price: 29038, Label: "SWG-H·5m", TF: "5m"}}},
			{Anchor: 29038, Lo: p(29032.16), Hi: p(29055.5), Sources: []kernel.ZoneSource{{Kind: "EQL", Price: 29038, Label: "EQL·4h", TF: "4h"}}},
			{Anchor: 29038, Lo: p(29032.16), Hi: p(29055.5), Sources: []kernel.ZoneSource{{Kind: "EQL", Price: 29038, Label: "EQL·1h", TF: "1h"}}},
		}},
	}
	sc := kernel.PlanScenario{ID: "S1", LevelID: id}
	if idx, why := ArmGeometryVerdict(doc, sc, true); why != "" || idx != 0 {
		t.Fatalf("merged-name zones must NOT flag ambiguity (v9/v14 regression): idx=%d why=%s", idx, why)
	}
}
func TestGeometryRefWildcardAmbiguousAcrossTF(t *testing.T) {
	doc, id := refAnchorDoc("")
	doc.Zones.Zones = append(doc.Zones.Zones, kernel.LevelZone{
		Lo: refPtr(29400), Hi: refPtr(29410),
		Sources: []kernel.ZoneSource{{Price: 29405, Label: "eVWAP", TF: "1h"}},
	})
	sc := kernel.PlanScenario{ID: "S1", LevelID: id}
	if idx, why := ArmGeometryVerdict(doc, sc, true); idx != -1 || why != "entry_zone_ambiguous" {
		t.Fatalf("wildcard must treat a same-price+label source at another tf as ambiguous, got idx=%d why=%s", idx, why)
	}
	if idx, why := ResolveEntryGeometryZone(doc, sc); why == "" {
		t.Fatalf("legacy must not admit, got idx=%d", idx)
	}
}
