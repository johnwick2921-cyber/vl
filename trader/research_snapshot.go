package trader

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
	"vl/kernel"
	"vl/levelidentity"
	"vl/researchsnapshot"
	"vl/store"
)

// Caller transfers the completed local detector slices; no worker reads a live
// BarCache, trader configuration pointer, or mutable plan document.
func recordResearchCandidates(id, symbol string, raw []kernel.DetectedLevel, seated []kernel.ScoredLevel, observed time.Time) {
	recordResearchCandidatesAt(id, symbol, raw, seated, observed, time.Now())
}
func recordResearchCandidatesAt(id, symbol string, raw []kernel.DetectedLevel, seated []kernel.ScoredLevel, observed, received time.Time) {
	defer researchsnapshot.Contain("recordResearchCandidatesAt recording")
	researchsnapshot.Record("candidate:planner_read", func() []researchsnapshot.Fact {
		ranks := map[string]int{}
		key := func(l kernel.DetectedLevel) string { return fmt.Sprintf("%s|%g|%s", l.Kind, l.Price, l.Label) }
		for i, l := range seated {
			ranks[key(l.DetectedLevel)] = i + 1
		}
		out := make([]researchsnapshot.Fact, 0, len(raw))
		for _, l := range raw {
			f := researchsnapshot.NewFact("candidate", "planner_read", researchsnapshot.Value(id), researchsnapshot.Clocks{ObservationMS: researchsnapshot.Value(observed.UnixMilli()), ReceiptMS: researchsnapshot.Value(received.UnixMilli())})
			recordResearchCandidateIdentity(&f, symbol, l)
			f.Set("identity_basis", "symbol/kind/bounds/origin date/timeframe/separate formation CLOSE; missing input means NULL id")
			f.Set("root_symbol", symbol)
			f.Set("raw_origin", l)
			f.Set("price", l.Price)
			f.Set("lo", l.Lo)
			f.Set("hi", l.Hi)
			if l.FormedAtMs > 0 {
				f.Set("formation_ms", l.FormedAtMs)
			}
			f.Set("availability_ms", received.UnixMilli())
			if l.TF != "" {
				f.Set("timeframe", l.TF)
			}
			if l.ZonePattern != "" {
				f.Set("zone_pattern", l.ZonePattern)
			}
			if c := l.Research; c != nil {
				f.Set("family", c.Family)
				f.Set("role", c.Role)
				f.Set("freshness_at_read", c.Freshness)
				f.Set("confluence_raw", c.ConfluenceRaw)
				f.Set("confluence_capped", c.ConfluenceCapped)
				if c.Score != nil {
					f.Set("raw_score_components", c)
					f.Set("capped_score_components", map[string]any{"confluence": c.ConfluenceCapped, "grade": c.Grade})
					f.Set("final_score", c.Score)
					f.Set("grade", c.Grade)
				}
				f.Set("grade", c.Grade)
				f.Set("overrides", c.Overrides)
				f.Set("exclusion_reason", c.Exclusion)
			}
			if rank, ok := ranks[key(l)]; ok {
				f.Set("rank", rank)
				f.Set("selection_outcome", "seated")
				f.Unknown("exclusion_reason", "selected; no exclusion")
			} else {
				f.Set("selection_outcome", "cut")
				if f.Fields["exclusion_reason"] == nil {
					f.Unknown("exclusion_reason", "later selection override did not emit a cause")
				}
			}
			if f.Fields["role"] == nil {
				f.Unknown("role", "not computed before exclusion; exclusion does not invalidate this map reference")
			}
			out = append(out, f)
		}
		return out
	})
}

func recordResearchInput(id string, input kernel.PlannerInput, systemPrompt, model string) {
	recordResearchInputAt(id, input, systemPrompt, model, time.Now())
}
func recordResearchInputAt(id string, input kernel.PlannerInput, systemPrompt, model string, received time.Time) {
	defer researchsnapshot.Contain("recordResearchInputAt recording")
	researchsnapshot.Record("plan:input", func() []researchsnapshot.Fact {
		f := researchsnapshot.NewFact("plan", "input", researchsnapshot.Value(id), researchsnapshot.Clocks{ObservationMS: researchsnapshot.Value(input.Now.UnixMilli()), ReceiptMS: researchsnapshot.Value(received.UnixMilli())})
		f.Set("input_snapshot_id", id)
		f.Set("input_snapshot", input)
		f.Set("model", model)
		f.Set("system_prompt", systemPrompt)
		f.Set("config_version", input.AIConfigHash)
		return []researchsnapshot.Fact{f}
	})
}

// The caller has already evaluated these verdicts and serialized its metadata.
// No confirmation evaluator is called here.
func recordResearchPermissions(planID string, version int, meta string, observed time.Time, evaluations []kernel.ScenarioEval) {
	recordResearchPermissionsAt(planID, version, meta, observed, evaluations, time.Now())
}
func recordResearchPermissionsAt(planID string, version int, meta string, observed time.Time, evaluations []kernel.ScenarioEval, received time.Time) {
	defer researchsnapshot.Contain("recordResearchPermissionsAt recording")
	researchsnapshot.Record("scenario:revalidation", func() []researchsnapshot.Fact {
		f := researchsnapshot.NewFact("scenario", "revalidation", nil, researchsnapshot.Clocks{ObservationMS: researchsnapshot.Value(observed.UnixMilli()), ReceiptMS: researchsnapshot.Value(received.UnixMilli()), PermissionMS: researchsnapshot.Value(observed.UnixMilli())})
		f.Set("plan_id", planID)
		f.Set("plan_version", version)
		f.Set("revalidation", json.RawMessage(meta))
		f.Unknown("permission_status", "scenario activation and confirmation verdicts; not an order authorization")
		out := []researchsnapshot.Fact{f}
		for _, e := range evaluations {
			sf := researchsnapshot.NewFact("scenario", "activation_verdict", nil, f.Clocks)
			sf.Set("plan_id", planID)
			sf.Set("plan_version", version)
			sf.Set("scenario_id", e.ID)
			sf.Set("reason", e.Reason)
			value := map[string]any{"status": e.Status, "basis": e.Basis, "has_anchor": e.HasAnchor, "anchor": nil, "facts": nil}
			if e.HasAnchor {
				value["anchor"] = e.Anchor
				value["facts"] = e.Facts
			}
			sf.Set("revalidation", value)
			sf.Unknown("permission_status", "activation verdict only; EntryGate supplies order permission separately")
			if e.Status == kernel.ScenarioExpired {
				sf.Set("expiry", map[string]any{"observed_ms": observed.UnixMilli(), "reason": e.Reason})
			}
			if e.Status == kernel.ScenarioInvalidated {
				sf.Set("invalidation", map[string]any{"observed_ms": observed.UnixMilli(), "reason": e.Reason, "basis": e.Basis})
			}
			out = append(out, sf)
		}
		return out
	})
}

func recordResearchGate(path, planID string, version int, scenario, reason string, refused bool) {
	recordResearchGateAt(path, planID, version, scenario, reason, refused, time.Now())
}
func recordResearchGateAt(path, planID string, version int, scenario, reason string, refused bool, now time.Time) {
	defer researchsnapshot.Contain("recordResearchGateAt recording")
	researchsnapshot.Record("scenario:entry_gate", func() []researchsnapshot.Fact {
		f := researchsnapshot.NewFact("scenario", "entry_gate:"+path, nil, researchsnapshot.Clocks{ObservationMS: researchsnapshot.Value(now.UnixMilli()), ReceiptMS: researchsnapshot.Value(now.UnixMilli()), PermissionMS: researchsnapshot.Value(now.UnixMilli())})
		if planID != "" {
			f.Set("plan_id", planID)
		}
		if version > 0 {
			f.Set("plan_version", version)
		}
		if scenario != "" {
			f.Set("scenario_id", scenario)
		}
		status := "pass"
		if refused {
			status = "refused"
		}
		f.Set("permission_status", status)
		f.Set("reason", reason)
		f.Set("revalidation", map[string]any{"path": path, "refused": refused, "basis": "returned EntryGate verdict; later placement checks still apply"})
		return []researchsnapshot.Fact{f}
	})
}

func recordResearchOutcomeAt(p *store.TraderPosition, now time.Time) {
	defer researchsnapshot.Contain("recordResearchOutcomeAt recording")
	if p == nil {
		return
	}
	// Copy primitive fields now; never queue a pointer another synchronization
	// pass may mutate. No raw-P&L fallback and no default fee treated as measured.
	id, symbol, scenario, version := p.ID, p.Symbol, p.CitedScenarioID, p.PlanVersion
	planID := p.PlanID
	entry, exit, quantity, entryMS, exitMS, reason, source := p.EntryPrice, p.ExitPrice, p.Quantity, p.EntryTime, p.ExitTime, p.CloseReason, p.Source
	var corrected *float64
	if p.PnlCorrected != nil {
		corrected = researchsnapshot.Value(*p.PnlCorrected)
	}
	exclusion := ""
	if entryMS < store.DayPlanEraStart.UnixMilli() {
		exclusion = "before DayPlanEraStart"
	} else if reason == store.CloseReasonTestSeam || store.IsSeamSource(source) {
		exclusion = "test seam"
	} else if p.PlanID == store.PlanUnresolvable {
		exclusion = "UNRESOLVABLE"
	} else if corrected == nil {
		exclusion = "UNRESOLVED: pnl_corrected is NULL"
	}
	researchsnapshot.Record("exec:closed_outcome", func() []researchsnapshot.Fact {
		clocks := researchsnapshot.Clocks{ReceiptMS: researchsnapshot.Value(now.UnixMilli())}
		if exitMS > 0 {
			clocks.ObservationMS = researchsnapshot.Value(exitMS)
		}
		f := researchsnapshot.NewFact("exec", "closed_outcome", nil, clocks)
		f.Set("position_id", id)
		if planID != "" {
			f.Set("plan_id", planID)
		}
		f.Set("root_symbol", symbol)
		if scenario != "" {
			f.Set("scenario_id", scenario)
		}
		if version > 0 {
			f.Set("plan_version", version)
		}
		if entry > 0 {
			f.Set("attainable_entry", entry)
			f.Set("entry_basis", "stored position entry; see entry order linkage")
		}
		if exit > 0 {
			f.Set("exit_price", exit)
			f.Set("exit_basis", reason)
		}
		f.Set("size", quantity)
		f.Set("reason", reason)
		f.Set("pnl_corrected", corrected)
		if exclusion != "" {
			f.Set("outcome_exclusion", exclusion)
		} else {
			f.Unknown("outcome_exclusion", "eligible corrected outcome; no exclusion applied")
		}
		f.Unknown("costs", "position fee has a default zero and no measured/missing discriminator")
		f.Unknown("common_horizon", "no experiment horizon selected by Stage A")
		return []researchsnapshot.Fact{f}
	})
}

func researchCandidateID(symbol string, l kernel.DetectedLevel) *string {
	l.IdentitySymbol = symbol
	return kernel.CandidateIdentity(l).ID
}
func recordResearchCandidateIdentity(f *researchsnapshot.Fact, symbol string, l kernel.DetectedLevel) {
	l.IdentitySymbol = symbol
	record := kernel.CandidateIdentity(l)
	if id := researchCandidateID(symbol, l); id != nil {
		f.Set("stable_id", *id)
	} else {
		_, missing := levelidentity.ID(kernel.IdentityInputs(record))
		f.Unknown("stable_id", "missing "+missing)
	}
	f.Set("identity_inputs", kernel.IdentityInputs(record))
	if l.FormedCloseMs != nil {
		f.Set("formed_close_ms", *l.FormedCloseMs)
	} else {
		f.Unknown("formed_close_ms", "formation close not captured")
	}
	f.Set("formation_basis", l.FormationBasis)
	if l.FormationLookback > 0 {
		f.Set("formation_lookback", l.FormationLookback)
	} else {
		f.Unknown("formation_lookback", "not captured")
	}
}
func recordResearchEpisodes(id, symbol string, l kernel.DetectedLevel, episodes []kernel.TouchOutcome, k, delta float64, horizon int, exitOn string, observed time.Time) {
	recordResearchEpisodesAt(id, symbol, l, episodes, k, delta, horizon, exitOn, observed, time.Now())
}
func recordResearchEpisodesAt(id, symbol string, l kernel.DetectedLevel, episodes []kernel.TouchOutcome, k, delta float64, horizon int, exitOn string, observed, received time.Time) {
	defer researchsnapshot.Contain("episode recording")
	researchsnapshot.Record("candidate:prior_episodes", func() []researchsnapshot.Fact {
		f := researchsnapshot.NewFact("candidate", "prior_episodes", researchsnapshot.Value(id), researchsnapshot.Clocks{ObservationMS: researchsnapshot.Value(observed.UnixMilli()), ReceiptMS: researchsnapshot.Value(received.UnixMilli())})
		recordResearchCandidateIdentity(&f, symbol, l)
		f.Set("root_symbol", symbol)
		if episodes == nil {
			episodes = []kernel.TouchOutcome{}
		}
		f.Set("prior_episodes", map[string]any{"episodes": episodes, "k": k, "delta": delta, "horizon_bars": horizon, "exit_on": exitOn, "formation_verified": l.FormedAtMs > 0, "basis": "existing detector result over its available window; not a historical first-availability claim"})
		return []researchsnapshot.Fact{f}
	})
}

func recordResearchPlacement(row store.ArmedOrderDB, signal, kind string, entry, stop, target float64, err error) {
	recordResearchPlacementAt(row, signal, kind, entry, stop, target, err, time.Now())
}
func recordResearchPlacementAt(row store.ArmedOrderDB, signal, kind string, entry, stop, target float64, err error, now time.Time) {
	defer researchsnapshot.Contain("placement recording")
	planID, version, scenario, id, seq := row.PlanID, row.ArmedUnderVersion, row.Scenario, row.ID, row.PlacementSeq
	reason := "placement call returned; received broker frame is still required"
	if err != nil {
		reason = err.Error()
	}
	researchsnapshot.Record("exec:arm_placement", func() []researchsnapshot.Fact {
		clocks := researchsnapshot.Clocks{ObservationMS: researchsnapshot.Value(now.UnixMilli()), ReceiptMS: researchsnapshot.Value(now.UnixMilli())}
		f := researchsnapshot.NewFact("exec", "arm_placement", nil, clocks)
		f.Set("plan_id", planID)
		if version > 0 {
			f.Set("plan_version", version)
		}
		f.Set("scenario_id", scenario)
		if signal != "" {
			f.Set("signal_id", signal)
		}
		f.Set("order_type", kind)
		f.Set("composed_entry", entry)
		f.Set("reason", reason)
		f.Set("order_semantics", map[string]any{"arm_id": id, "placement_sequence": seq, "entry": entry, "stop": stop, "target": target, "basis": "arguments passed to existing placement method"})
		sf := researchsnapshot.NewFact("scenario", "placement_geometry", nil, clocks)
		sf.Set("plan_id", planID)
		if version > 0 {
			sf.Set("plan_version", version)
		}
		sf.Set("scenario_id", scenario)
		sf.Set("arm_id", id)
		sf.Set("initial_risk", math.Abs(entry-stop))
		sf.Set("risk_basis", "points between composed entry and stop; not filled-position risk")
		sf.Set("target_path", []float64{target})
		sf.Set("reason", reason)
		return []researchsnapshot.Fact{f, sf}
	})
}
