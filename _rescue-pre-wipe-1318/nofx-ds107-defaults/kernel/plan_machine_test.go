package kernel

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"nofx/store"
)

// W5 foundation — machine scenarios and the ONE fold (ResolvePlanFinal).

func pictureScenarioFixture(id, ref string) PlanScenario {
	return PlanScenario{
		ID: id, Trigger: "H1 close 21530 beyond the 4H body 21520 (h1_close_break v1)",
		Condition: "acceptance", Direction: "long", Quality: "B",
		TargetChain: []float64{21560}, Invalid: "back inside the 4H body 21500-21520, or the window closed",
		Arm:       &PlanArmSpec{Enabled: true, Entry: 21530, Stop: 21510, Target: 21560, Policy: EntryPolicyMarketInZone},
		Economics: &ScenarioEconomics{EntryZone: []float64{21528, 21530}},
		Source:    ScenarioSourcePicture,
		Machine: &PlanMachineSource{Rule: MachineRulePictureH1CloseBreak, RuleVer: 1, Ref: ref,
			EligibleFromMs: 1_000, EligibleUntilMs: 11_000},
	}
}

func planJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func machineOverlay(t *testing.T, version int, sc PlanScenario) OverlayRef {
	t.Helper()
	p, err := MachineOverlayPatch(sc)
	if err != nil {
		t.Fatal(err)
	}
	return OverlayRef{Version: version, Origin: MachineOverlayOriginPicture, Patch: p}
}

// legacyFold is the fold the three call sites ran before W5, verbatim in
// shape: no overlays → the base as parsed; otherwise apply every patch (a bad
// one skipped), re-validate at the hard caps, fall back to the base.
func legacyFold(base []byte, patches []string) (PlanDoc, []error) {
	var doc PlanDoc
	_ = json.Unmarshal(base, &doc)
	if len(patches) == 0 {
		return doc, nil
	}
	final, errs := ApplyOverlayPatches(base, patches)
	var merged PlanDoc
	if json.Unmarshal(final, &merged) == nil && ValidatePlanDocWithCaps(&merged, PlanHardMaxLevels, PlanHardMaxScenarios) == nil {
		return merged, errs
	}
	return doc, errs
}

func TestResolvePlanFinalMatchesTheLegacyFoldWithoutMachineOverlays(t *testing.T) {
	base := planJSON(t, selfCheckPlanDoc())
	cases := map[string][]string{
		"no overlays":                  nil,
		"one good overlay":             {`[{"op":"replace","path":"/bias/direction","value":"short"}]`},
		"a bad patch skipped":          {`[{"op":"replace","path":"/nope/0","value":1}]`, `[{"op":"replace","path":"/day_type","value":"trend"}]`},
		"fold fails validation → base": {`[{"op":"replace","path":"/bias/direction","value":"sideways"}]`},
	}
	for name, patches := range cases {
		want, wantErrs := legacyFold(base, patches)
		refs := make([]OverlayRef, 0, len(patches))
		for i, p := range patches {
			refs = append(refs, OverlayRef{Version: i + 1, Origin: "owner", Patch: p})
		}
		got, err := ResolvePlanFinal(base, refs)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if a, b := planJSON(t, want), planJSON(t, got.Doc); string(a) != string(b) {
			t.Errorf("%s: plan_final drifted from the legacy fold\nwant %s\n got %s", name, a, b)
		}
		if fmt.Sprint(wantErrs) != fmt.Sprint(got.OverlayErrs) {
			t.Errorf("%s: overlay errors drifted: want %v got %v", name, wantErrs, got.OverlayErrs)
		}
		if len(got.MachineApplied) != 0 || len(got.MachineSkipped) != 0 {
			t.Errorf("%s: no machine overlay, yet machine results %+v", name, got)
		}
	}
}

func TestResolvePlanFinalRecordsWhatComposedIt(t *testing.T) {
	base := planJSON(t, selfCheckPlanDoc())
	got, err := ResolvePlanFinal(base, []OverlayRef{
		{Version: 1, Origin: "owner", Patch: `[{"op":"replace","path":"/day_type","value":"trend"}]`},
		{Version: 2, Origin: "owner", Patch: `[{"op":"replace","path":"/nope/0","value":1}]`},
		machineOverlay(t, 3, pictureScenarioFixture("P1", "opp-a")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got.UserApplied) != "[1]" {
		t.Errorf("user overlays in plan_final = %v, want [1] (v2 failed to apply)", got.UserApplied)
	}
	if len(got.MachineApplied) != 1 || got.MachineApplied[0] != (MachineApplied{OverlayVersion: 3, ScenarioID: "P1", Ref: "opp-a"}) {
		t.Errorf("machine overlays in plan_final = %+v", got.MachineApplied)
	}
	if got.Doc.DayType != "trend" || got.Doc.Scenarios[len(got.Doc.Scenarios)-1].ID != "P1" {
		t.Errorf("plan_final = day_type %q, last scenario %q", got.Doc.DayType, got.Doc.Scenarios[len(got.Doc.Scenarios)-1].ID)
	}
}

// F2 pin: a machine scenario never counts against the caps, so it cannot push
// a full plan's user fold over the hard cap and knock the owner's overlays out.
func TestMachineScenarioNeverKnocksOwnerOverlaysOut(t *testing.T) {
	doc := selfCheckPlanDoc()
	for len(doc.Scenarios) < PlanHardMaxScenarios {
		s := doc.Scenarios[0]
		s.ID = fmt.Sprintf("S%d", len(doc.Scenarios)+1)
		doc.Scenarios = append(doc.Scenarios, s)
	}
	base := planJSON(t, doc)
	got, err := ResolvePlanFinal(base, []OverlayRef{
		{Version: 1, Origin: "owner", Patch: `[{"op":"replace","path":"/day_type","value":"trend"}]`},
		machineOverlay(t, 2, pictureScenarioFixture("P1", "opp-a")),
		machineOverlay(t, 3, pictureScenarioFixture("P2", "opp-b")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.FoldErr != nil || got.Doc.DayType != "trend" {
		t.Fatalf("the owner overlay must stay active on a full plan: fold err %v, day_type %q", got.FoldErr, got.Doc.DayType)
	}
	if n := len(got.Doc.Scenarios); n != PlanHardMaxScenarios+2 {
		t.Fatalf("%d planner scenarios + 2 machine scenarios expected, got %d", PlanHardMaxScenarios, n)
	}
}

func TestMachineOverlaySkippedWhenNotAMachineScenario(t *testing.T) {
	base := planJSON(t, selfCheckPlanDoc())
	planner := selfCheckPlanDoc().Scenarios[0]
	planner.ID = "S7"
	vPlanner, _ := json.Marshal(planner)
	dup := pictureScenarioFixture("P2", "opp-a")
	cases := []struct {
		name  string
		patch string
		want  string
	}{
		{"index edit", `[{"op":"replace","path":"/scenarios/0/quality","value":"A"}]`, "exactly one add /scenarios/- op"},
		{"two ops", `[{"op":"add","path":"/scenarios/-","value":{}},{"op":"add","path":"/scenarios/-","value":{}}]`, "exactly one add"},
		{"a planner scenario", `[{"op":"add","path":"/scenarios/-","value":` + string(vPlanner) + `}]`, "source"},
	}
	for _, c := range cases {
		got, err := ResolvePlanFinal(base, []OverlayRef{{Version: 1, Origin: MachineOverlayOriginPicture, Patch: c.patch}})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.MachineSkipped) != 1 || !strings.Contains(got.MachineSkipped[0].Error(), c.want) {
			t.Errorf("%s: skipped = %v, want one containing %q", c.name, got.MachineSkipped, c.want)
		}
		if len(got.Doc.Scenarios) != len(selfCheckPlanDoc().Scenarios) {
			t.Errorf("%s: a skipped machine overlay must add nothing", c.name)
		}
	}
	got, _ := ResolvePlanFinal(base, []OverlayRef{
		machineOverlay(t, 1, pictureScenarioFixture("P1", "opp-a")),
		machineOverlay(t, 2, dup),
	})
	if len(got.MachineApplied) != 1 || len(got.MachineSkipped) != 1 || !strings.Contains(got.MachineSkipped[0].Error(), "already in the plan") {
		t.Errorf("a second overlay for the same opportunity must be skipped: %+v", got)
	}
}

func TestValidatorMachineScenarioIdentity(t *testing.T) {
	ok := selfCheckPlanDoc()
	ok.Scenarios = append(ok.Scenarios, pictureScenarioFixture("P1", "opp-a"))
	if err := ValidatePlanDocWithCaps(&ok, PlanHardMaxLevels, PlanHardMaxScenarios); err != nil {
		t.Fatalf("a well-formed machine scenario must validate: %v", err)
	}
	mut := func(f func(*PlanScenario)) PlanDoc {
		d := selfCheckPlanDoc()
		s := pictureScenarioFixture("P1", "opp-a")
		f(&s)
		d.Scenarios = append(d.Scenarios, s)
		return d
	}
	for name, d := range map[string]PlanDoc{
		"planner id on a machine scenario": mut(func(s *PlanScenario) { s.ID = "S9" }),
		"unknown source":                   mut(func(s *PlanScenario) { s.Source = "robot" }),
		"source without its record":        mut(func(s *PlanScenario) { s.Machine = nil }),
		"record without a source":          mut(func(s *PlanScenario) { s.Source = "" }),
		"no ref":                           mut(func(s *PlanScenario) { s.Machine.Ref = "" }),
		"inverted window":                  mut(func(s *PlanScenario) { s.Machine.EligibleUntilMs = 500 }),
	} {
		if err := ValidatePlanDocWithCaps(&d, PlanHardMaxLevels, PlanHardMaxScenarios); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}
	p := selfCheckPlanDoc()
	p.Scenarios[0].ID = "P1"
	err := ValidatePlanDocWithCaps(&p, PlanHardMaxLevels, PlanHardMaxScenarios)
	if err == nil || !strings.Contains(err.Error(), "format: S1..S99") {
		t.Fatalf("a P id on a planner scenario keeps the legacy refusal, got %v", err)
	}
}

func TestPlannerCannotAuthorMachineFields(t *testing.T) {
	d := selfCheckPlanDoc()
	d.Scenarios = append(d.Scenarios, pictureScenarioFixture("P1", "opp-a"))
	raw := string(planJSON(t, d))
	if _, err := ParsePlanDocForAuthoring(raw, PlanHardMaxLevels, PlanHardMaxScenarios, AuthoringOpts{}); err == nil ||
		!strings.Contains(err.Error(), "only the machine writes them") {
		t.Fatalf("new authoring must refuse model-authored machine fields, got %v", err)
	}
	// A stored reader is not the write path: the machine plan it reads parses.
	if _, err := ParsePlanDoc(raw); err != nil {
		t.Fatalf("a stored doc carrying a machine scenario must parse: %v", err)
	}
}

func TestPlannerScenarioMarshalsWithoutMachineKeys(t *testing.T) {
	b := planJSON(t, selfCheckPlanDoc().Scenarios[0])
	if strings.Contains(string(b), `"source"`) || strings.Contains(string(b), `"machine"`) {
		t.Fatalf("a planner scenario must re-marshal byte-identically (no source/machine keys): %s", b)
	}
}

func TestMachineScenariosPreserved(t *testing.T) {
	before := selfCheckPlanDoc()
	before.Scenarios = append(before.Scenarios, pictureScenarioFixture("P1", "opp-a"))
	if err := MachineScenariosPreserved(before, before); err != nil {
		t.Fatalf("an edit that leaves the machine scenario alone is fine: %v", err)
	}
	added := selfCheckPlanDoc()
	added.Scenarios = append(added.Scenarios, pictureScenarioFixture("P1", "opp-a"))
	if err := MachineScenariosPreserved(selfCheckPlanDoc(), added); err == nil {
		t.Error("an edit that ADDS a machine scenario must be refused")
	}
	altered := selfCheckPlanDoc()
	s := pictureScenarioFixture("P1", "opp-a")
	s.Arm.Stop = 21500
	altered.Scenarios = append(altered.Scenarios, s)
	if err := MachineScenariosPreserved(before, altered); err == nil {
		t.Error("an edit that ALTERS a machine scenario must be refused")
	}
	if err := MachineScenariosPreserved(before, selfCheckPlanDoc()); err == nil {
		t.Error("an edit that REMOVES a machine scenario must be refused")
	}
}

func TestMachineHelpers(t *testing.T) {
	d := selfCheckPlanDoc()
	if id := NextMachineScenarioID(d); id != "P1" {
		t.Fatalf("first machine id = %s", id)
	}
	d.Scenarios = append(d.Scenarios, pictureScenarioFixture("P1", "opp-a"), pictureScenarioFixture("P7", "opp-b"))
	if id := NextMachineScenarioID(d); id != "P8" {
		t.Fatalf("next machine id = %s, want P8", id)
	}
	if s, ok := MachineScenarioByRef(d, "opp-b"); !ok || s.ID != "P7" {
		t.Fatalf("by ref = %+v %v", s, ok)
	}
	sc := pictureScenarioFixture("P1", "opp-a")
	for ms, want := range map[int64]bool{999: false, 1_000: true, 11_000: true, 11_001: false} {
		if MachineEligibleAt(sc, ms) != want {
			t.Errorf("eligible at %d must be %v", ms, want)
		}
	}
	if !MachineEligibleAt(selfCheckPlanDoc().Scenarios[0], 0) {
		t.Error("a planner scenario is never window-bound")
	}
}

// store cannot import kernel, so it mirrors three names; they must agree.
func TestMachineNamesMirroredInStore(t *testing.T) {
	if ScenarioSourcePicture != store.ArmSourcePicture {
		t.Fatalf("kernel %q vs store %q", ScenarioSourcePicture, store.ArmSourcePicture)
	}
	if !strings.HasPrefix(MachinePlanTriggerPicture, store.MachinePlanTriggerPrefix) ||
		!store.IsMachinePlan(&store.PlanDB{TriggerReason: MachinePlanTriggerPicture}) {
		t.Fatalf("the machine plan trigger %q must carry the store prefix %q", MachinePlanTriggerPicture, store.MachinePlanTriggerPrefix)
	}
	if !IsMachineOverlayOrigin(MachineOverlayOriginPicture) || IsMachineOverlayOrigin("owner") || IsMachineOverlayOrigin("planner-revised") {
		t.Fatal("machine overlay origin classification drifted")
	}
}

func TestOverlayRefsFrom(t *testing.T) {
	got := OverlayRefsFrom([]*store.PlanOverlayDB{
		{OverlayVersion: 1, Origin: "owner", Patch: `[]`}, nil,
		{OverlayVersion: 2, Origin: MachineOverlayOriginPicture, Patch: `[{}]`},
	})
	if len(got) != 2 || got[0] != (OverlayRef{Version: 1, Origin: "owner", Patch: `[]`}) || got[1].Version != 2 || got[1].Origin != MachineOverlayOriginPicture {
		t.Fatalf("refs = %+v", got)
	}
}
