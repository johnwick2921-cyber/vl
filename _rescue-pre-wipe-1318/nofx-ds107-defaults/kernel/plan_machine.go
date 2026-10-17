package kernel

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"nofx/store"
)

// W-EXEC-TRUTH W5 — MACHINE-AUTHORED SCENARIOS (the Picture HTF source).
//
// A Picture opportunity becomes a RECORDED Day Plan scenario before any order
// exists (D27: no trade behind "No plan"). It reaches a plan by exactly two
// doors, both append-only (CTO ruling 1790191033566, design O′):
//
//   - an ACTIVE plan gets one plan_overlays row, origin
//     MachineOverlayOriginPicture, whose patch is a single
//     `add /scenarios/-` of the machine scenario;
//   - no plan row at all gets a machine plan v1 (store.AppendPlanIfAbsent).
//
// It never becomes a new plan VERSION of an AI chain: UpsertArm's re-arm pin
// holds only within a version, so a version bump would let a filled zone row
// re-arm (a second trade).
//
// ResolvePlanFinal is the ONE fold every reader uses (the executor, the card,
// the re-alignment path): user overlays fold and re-validate at the hard caps
// exactly as before — a failure falls back to the base — and only THEN are
// machine scenarios appended, each validated on its own and never counted
// against the caps. A machine scenario therefore cannot knock the owner's
// overlays out of plan_final, and no user or AI patch can reach one: user
// patches are applied first, and applyPlanOverlay refuses any patch that
// changes a machine scenario (MachineScenariosPreserved).

const (
	// ScenarioSourcePicture is the only machine source today.
	ScenarioSourcePicture = "picture"
	// MachineOverlayOriginPicture is the plan_overlays.origin of a Picture
	// scenario. Every machine origin carries the "machine:" prefix.
	MachineOverlayOriginPicture = "machine:picture_htf"
	// MachinePlanTriggerPicture is plans.trigger_reason of a machine plan (the
	// no-plan door); plans.model_id is MachinePlanModelID.
	MachinePlanTriggerPicture = "machine:picture_htf"
	MachinePlanModelID        = "machine"
	// MachineRulePictureH1CloseBreak names the Picture rule a scenario records.
	MachineRulePictureH1CloseBreak = "h1_close_break"
)

// PlanMachineSource is a machine scenario's own record: which rule produced
// it, the opportunity it stands for (Ref — the idempotency key), the window it
// may be placed in, and the evidence frozen at hand-off. Evidence is opaque to
// the kernel: the trader's adapter (picture_evidence.go) owns its shape, so W4
// can change the evaluator's fields without touching the plan grammar.
type PlanMachineSource struct {
	Rule            string          `json:"rule"`
	RuleVer         int             `json:"rule_ver"`
	Ref             string          `json:"ref"`
	EligibleFromMs  int64           `json:"eligible_from_ms"`
	EligibleUntilMs int64           `json:"eligible_until_ms"`
	RunEpoch        int64           `json:"run_epoch,omitempty"`
	Evidence        json.RawMessage `json:"evidence,omitempty"`
}

// machineScenarioIDRe — machine scenarios live in their own P namespace so an
// AI S-id and a Picture id can never collide.
var machineScenarioIDRe = regexp.MustCompile(`^P\d{1,2}$`)

// IsMachineScenario reports whether a scenario was written by the machine.
func IsMachineScenario(s PlanScenario) bool { return s.Source != "" || s.Machine != nil }

// machineScenarioIdentity is the id/source part of the validator for a
// machine scenario.
func machineScenarioIdentity(s PlanScenario) error {
	if s.Source != ScenarioSourcePicture {
		return fmt.Errorf("source %q invalid (the only machine source is %q)", s.Source, ScenarioSourcePicture)
	}
	if s.Machine == nil {
		return fmt.Errorf("source %q requires its machine record", s.Source)
	}
	if !machineScenarioIDRe.MatchString(strings.TrimSpace(s.ID)) {
		return fmt.Errorf("id %q invalid for a machine scenario (format: P1..P99)", s.ID)
	}
	if strings.TrimSpace(s.Machine.Ref) == "" || strings.TrimSpace(s.Machine.Rule) == "" {
		return fmt.Errorf("machine record requires rule and ref")
	}
	if s.Machine.EligibleUntilMs <= 0 || s.Machine.EligibleUntilMs < s.Machine.EligibleFromMs {
		return fmt.Errorf("machine eligibility window [%d, %d] invalid", s.Machine.EligibleFromMs, s.Machine.EligibleUntilMs)
	}
	return nil
}

// RefuseModelAuthoredMachineFields — the planner write path (new authoring)
// refuses any scenario carrying source/machine: only the machine writes them.
func RefuseModelAuthoredMachineFields(d *PlanDoc) error {
	if d == nil {
		return nil
	}
	for i, s := range d.Scenarios {
		if IsMachineScenario(s) {
			return fmt.Errorf("scenario[%d] carries machine fields (source/machine) — only the machine writes them; omit source and machine", i)
		}
	}
	return nil
}

// IsMachineOverlayOrigin reports whether an overlay row was written by the
// machine (never folded as a user patch, never carried as an owner edit).
func IsMachineOverlayOrigin(origin string) bool {
	return strings.HasPrefix(strings.TrimSpace(origin), "machine:")
}

// OverlayRef is one stored overlay row as the fold reads it.
type OverlayRef struct {
	Version int // plan_overlays.overlay_version
	Origin  string
	Patch   string
}

// OverlayRefsFrom adapts stored plan_overlays rows (ListOverlays order) for
// ResolvePlanFinal.
func OverlayRefsFrom(rows []*store.PlanOverlayDB) []OverlayRef {
	out := make([]OverlayRef, 0, len(rows))
	for _, r := range rows {
		if r != nil {
			out = append(out, OverlayRef{Version: r.OverlayVersion, Origin: r.Origin, Patch: r.Patch})
		}
	}
	return out
}

// MachineApplied names one machine scenario the fold appended.
type MachineApplied struct {
	OverlayVersion int
	ScenarioID     string
	Ref            string
}

// PlanFinal is the resolved plan plus the record of what composed it (CTO
// requirement: the fold records which overlay rows made the final doc).
type PlanFinal struct {
	Doc PlanDoc
	// OverlayErrs are the user patches that failed to apply and were skipped,
	// worded exactly as ApplyOverlayPatches words them ("overlay[i]: …", i
	// counting user patches).
	OverlayErrs []error
	// FoldErr is set when the folded user overlays failed re-validation at the
	// hard caps; Doc then carries the BASE (plus any machine scenarios).
	FoldErr error
	// UserApplied lists the overlay versions whose patches are IN Doc (empty
	// when FoldErr fell back to the base).
	UserApplied    []int
	MachineApplied []MachineApplied
	// MachineSkipped are machine overlays that did not fold (bad patch, a
	// scenario that fails ValidateMachineScenario, a duplicate ref or id).
	MachineSkipped []error
}

// ResolvePlanFinal folds a plan's overlays into plan_final. With no machine
// overlay the result is exactly what the three fold sites computed before W5:
// no overlays → the base as parsed; user overlays → applied in order (a bad
// patch skipped), re-validated at PlanHardMaxLevels/PlanHardMaxScenarios, and
// the base on failure. Machine scenarios are appended after that, each
// validated alone. The error is non-nil only when the base itself does not
// parse (callers keep their own handling of that case).
func ResolvePlanFinal(base []byte, overlays []OverlayRef) (PlanFinal, error) {
	var out PlanFinal
	if err := json.Unmarshal(base, &out.Doc); err != nil {
		return PlanFinal{}, err
	}
	var user, machine []OverlayRef
	for _, o := range overlays {
		if IsMachineOverlayOrigin(o.Origin) {
			machine = append(machine, o)
		} else {
			user = append(user, o)
		}
	}
	if len(user) > 0 {
		cur := base
		var applied []int
		for i, o := range user {
			next, err := ApplyPatchStrict(cur, o.Patch)
			if err != nil {
				out.OverlayErrs = append(out.OverlayErrs, fmt.Errorf("overlay[%d]: %w", i, err))
				continue
			}
			cur = next
			applied = append(applied, o.Version)
		}
		var merged PlanDoc
		if err := json.Unmarshal(cur, &merged); err != nil {
			out.FoldErr = err
		} else if err := ValidatePlanDocWithCaps(&merged, PlanHardMaxLevels, PlanHardMaxScenarios); err != nil {
			out.FoldErr = err
		} else {
			out.Doc = merged
			out.UserApplied = applied
		}
	}
	for _, o := range machine {
		sc, err := MachineScenarioFromPatch(o.Patch)
		if err == nil {
			err = ValidateMachineScenario(out.Doc, sc)
		}
		if err != nil {
			out.MachineSkipped = append(out.MachineSkipped, fmt.Errorf("machine overlay v%d (%s): %w", o.Version, o.Origin, err))
			continue
		}
		out.Doc.Scenarios = append(out.Doc.Scenarios, sc)
		out.MachineApplied = append(out.MachineApplied, MachineApplied{OverlayVersion: o.Version, ScenarioID: sc.ID, Ref: sc.Machine.Ref})
	}
	return out, nil
}

// MachineOverlayPatch is the ONE patch shape a machine overlay may carry.
func MachineOverlayPatch(sc PlanScenario) (string, error) {
	v, err := json.Marshal(sc)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal([]PatchOp{{Op: "add", Path: "/scenarios/-", Value: v}})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// MachineScenarioFromPatch reads a machine overlay back: exactly one
// `add /scenarios/-` op whose value is a machine scenario. Anything else is
// refused — a machine overlay is structural, never an index-based edit.
func MachineScenarioFromPatch(patch string) (PlanScenario, error) {
	var ops []PatchOp
	if err := json.Unmarshal([]byte(patch), &ops); err != nil {
		return PlanScenario{}, fmt.Errorf("patch is not a JSON op array: %w", err)
	}
	if len(ops) != 1 || ops[0].Op != "add" || ops[0].Path != "/scenarios/-" {
		return PlanScenario{}, fmt.Errorf("a machine overlay is exactly one add /scenarios/- op")
	}
	var sc PlanScenario
	if err := json.Unmarshal(ops[0].Value, &sc); err != nil {
		return PlanScenario{}, fmt.Errorf("machine scenario: %w", err)
	}
	return sc, nil
}

// ValidateMachineScenario judges one machine scenario against the doc it will
// join: its identity (source, record, P id), uniqueness of id and ref in the
// doc, and every scenario law the plan validator applies — run on a doc that
// holds only this scenario beside the plan's levels, so the machine scenario
// never counts against (or trips) the plan's scenario cap.
func ValidateMachineScenario(doc PlanDoc, sc PlanScenario) error {
	if err := machineScenarioIdentity(sc); err != nil {
		return err
	}
	for _, s := range doc.Scenarios {
		if strings.TrimSpace(s.ID) == strings.TrimSpace(sc.ID) {
			return fmt.Errorf("scenario id %s already in the plan", sc.ID)
		}
		if s.Machine != nil && s.Machine.Ref == sc.Machine.Ref {
			return fmt.Errorf("machine ref %s already in the plan as %s", store.RedactPictureOppKey(sc.Machine.Ref), s.ID)
		}
	}
	probe := PlanDoc{
		Reasoning:      "machine scenario probe",
		Bias:           PlanBias{Direction: "neutral"},
		DeathCondition: "machine scenario probe",
		Levels:         doc.Levels,
		Scenarios:      []PlanScenario{sc},
	}
	return ValidatePlanDocWithCaps(&probe, PlanHardMaxLevels, 1)
}

// MachineScenariosPreserved refuses a candidate (a user or AI edit) that
// adds, removes or alters a machine scenario. applyPlanOverlay folds user
// overlays only, so before and after both come from the user fold; a machine
// scenario appearing there means a patch tried to author one.
func MachineScenariosPreserved(before, after PlanDoc) error {
	prev := map[string]string{}
	for _, s := range before.Scenarios {
		if IsMachineScenario(s) {
			b, _ := json.Marshal(s)
			prev[s.ID] = string(b)
		}
	}
	seen := map[string]bool{}
	for _, s := range after.Scenarios {
		if !IsMachineScenario(s) {
			continue
		}
		b, _ := json.Marshal(s)
		was, ok := prev[s.ID]
		if !ok {
			return fmt.Errorf("an edit may not add a machine scenario (%s) — only the machine records one", s.ID)
		}
		if was != string(b) {
			return fmt.Errorf("an edit may not alter machine scenario %s — its evidence is the machine's record", s.ID)
		}
		seen[s.ID] = true
	}
	for id := range prev {
		if !seen[id] {
			return fmt.Errorf("an edit may not remove machine scenario %s", id)
		}
	}
	return nil
}

// MachineEligibleAt reports whether a machine scenario may still be placed at
// nowMs (inclusive window). A planner scenario is always "eligible" here — the
// question does not apply to it.
func MachineEligibleAt(sc PlanScenario, nowMs int64) bool {
	if sc.Machine == nil {
		return true
	}
	return nowMs >= sc.Machine.EligibleFromMs && nowMs <= sc.Machine.EligibleUntilMs
}

// MachineScenarioByRef finds the machine scenario recorded for an opportunity.
func MachineScenarioByRef(doc PlanDoc, ref string) (PlanScenario, bool) {
	for _, s := range doc.Scenarios {
		if s.Machine != nil && s.Machine.Ref == ref {
			return s, true
		}
	}
	return PlanScenario{}, false
}

// NextMachineScenarioID mints P<n>: one more than the highest P id the doc
// already holds (P1 on a doc with none).
func NextMachineScenarioID(doc PlanDoc) string {
	hi := 0
	for _, s := range doc.Scenarios {
		id := strings.TrimSpace(s.ID)
		if machineScenarioIDRe.MatchString(id) {
			if n, err := strconv.Atoi(id[1:]); err == nil && n > hi {
				hi = n
			}
		}
	}
	return "P" + strconv.Itoa(hi+1)
}
