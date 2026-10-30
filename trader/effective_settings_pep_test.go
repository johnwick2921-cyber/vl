package trader

import (
	"strings"
	"testing"

	"vl/store"
)

// Every pep leaf the schema walk reaches has a resolver; none reads
// "n/a — no resolver registered".
func TestEffectivePepLeavesAllResolve(t *testing.T) {
	rows := effRowsFor(t, `{"strategy_type":"ai_trading"}`, "ninjatrader", "")
	n := 0
	for path, r := range rows {
		if !strings.HasPrefix(path, "pep.") {
			continue
		}
		n++
		if !r.Resolved || r.Effective == EffectiveNoResolver {
			t.Errorf("%s: unresolved %+v", path, r)
		}
		if r.Origin != store.SourceShippedDefault {
			t.Errorf("%s: unset leaf origin %q, want shipped default", path, r.Origin)
		}
	}
	if n != 22 {
		t.Fatalf("pep rows = %d, want 22", n)
	}
	if rows["pep.pep_enabled"].Effective != false || rows["pep.pep_contracts"].Effective != 6 {
		t.Fatalf("shipped: enabled %v contracts %v", rows["pep.pep_enabled"].Effective, rows["pep.pep_contracts"].Effective)
	}
	// OWNER DEFAULTS 2026-10-30 ("keep default E 6"), literals on purpose: the
	// effective rows of an unsaved strategy read style E, 6 contracts, an E box of
	// 2 minutes and an A box of 5 minutes.
	// Mutant: store.PepStyleDefault "A" / PepContractsDefault 3 / PepEBoxMinutesDefault 5 -> RED.
	if rows["pep.pep_style"].Effective != "E" || rows["pep.pep_contracts"].Effective != 6 ||
		rows["pep.style_e.pep_e_box_minutes"].Effective != 2 || rows["pep.style_a.pep_a_box_minutes"].Effective != 5 {
		t.Fatalf("shipped E/6/2/5: style %v contracts %v E box %v A box %v", rows["pep.pep_style"].Effective,
			rows["pep.pep_contracts"].Effective, rows["pep.style_e.pep_e_box_minutes"].Effective, rows["pep.style_a.pep_a_box_minutes"].Effective)
	}
}

// The row IS the production resolver's answer for the same stored config, and a
// clamp names itself.
func TestEffectivePepRowsAreStoreResolvePep(t *testing.T) {
	raw := `{"strategy_type":"ai_trading","pep":{"pep_enabled":true,"pep_style":"e","pep_contracts":15,
	  "pep_fee_per_contract_usd":0,"style_e":{"pep_e_ema_period":12,"pep_e_ema_tf_min":7},"style_a":{"pep_a_box_minutes":6}}}`
	rows := effRowsFor(t, raw, "ninjatrader", "")
	cfg, err := (&store.Strategy{Config: raw}).ParseConfig()
	if err != nil {
		t.Fatal(err)
	}
	want := store.ResolvePep(cfg)
	if got := rows["pep.pep_contracts"]; got.Effective != want.Contracts || want.Contracts != 10 ||
		!strings.HasPrefix(got.Origin, "clamp (store.ResolvePep)") {
		t.Fatalf("contracts: %+v want %d", got, want.Contracts)
	}
	if got := rows["pep.pep_style"]; got.Effective != "E" || got.Origin != store.SourceSaved {
		t.Fatalf("style (saved lower-case, canonicalised): %+v", got)
	}
	if got := rows["pep.pep_fee_per_contract_usd"]; got.Effective != 0.0 || got.Origin != store.SourceSaved {
		t.Fatalf("an explicit legal 0 must read saved value: %+v", got)
	}
	if got := rows["pep.style_e.pep_e_ema_tf_min"]; got.Effective != 2 || !strings.Contains(got.Origin, "clamp") {
		t.Fatalf("tf 7 must read the clamp: %+v", got)
	}
	if got := rows["pep.style_e.pep_e_ema_period"]; got.Effective != 12 || got.Origin != store.SourceSaved {
		t.Fatalf("ema period: %+v", got)
	}
	if got := rows["pep.style_a.pep_a_box_minutes"]; got.Effective != 6 || got.Origin != store.SourceSaved {
		t.Fatalf("style A box: %+v", got)
	}
	if got := rows["pep.pep_enabled"]; got.Effective != true || got.Origin != store.SourceSaved {
		t.Fatalf("enabled: %+v", got)
	}
	// style E's numbers never move style A's rows
	if got := rows["pep.style_a.pep_a_sl_fraction"]; got.Effective != 1.0 || got.Origin != store.SourceShippedDefault {
		t.Fatalf("A sl fraction leaked: %+v", got)
	}
}

// rereview m461 P2-1 (UI + SETTINGS TRUTH): with pep ON the AI-era Risk Control
// knobs are NOT what the bot runs, and the effective rows must say so the way
// they say it for mentor: break-even and trailing "not applied", the AI
// confidence floor and the order cap "not used", the planner stamp size "not
// used". The sizing row names pep_contracts, the number resolveMaxContracts
// returns for a pep trader. Production path: EffectiveSettings over the stored
// JSON (the same rows the Studio reads).
//
// Mutant: delete the pep branch of exitMech / min_confidence /
// max_contracts_per_order / planner_contracts in effective_settings.go -> RED.
func TestEffectiveRiskControlRowsSayPepIgnoresThem(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0")
	const raw = `{"ai_config":{"risk_control":{"max_contracts_per_order":2,"breakeven_enabled":true,"trailing_enabled":true,"min_confidence":60,"min_risk_reward_ratio":2,"planner_contracts":3}},"pep":{"pep_enabled":true,"pep_contracts":6}}`
	rows := effRowsFor(t, raw, "ninjatrader", "")
	for _, leaf := range []string{"breakeven_enabled", "trailing_enabled"} {
		r := rows["ai_config.risk_control."+leaf]
		v, _ := r.Effective.(string)
		if !strings.HasPrefix(v, "not applied while pep mode is ON") || !strings.Contains(r.Origin, "pep_enabled on") || !strings.Contains(r.Origin, "not used") {
			t.Errorf("%s under pep: %+v", leaf, r)
		}
	}
	mc := rows["ai_config.risk_control.min_confidence"]
	if v, _ := mc.Effective.(string); !strings.HasPrefix(v, "not used in pep mode") || !strings.Contains(mc.Origin, "pep_enabled on") {
		t.Errorf("min_confidence under pep: %+v", mc)
	}
	sz := rows["ai_config.risk_control.max_contracts_per_order"]
	if v, _ := sz.Effective.(string); !strings.Contains(v, "pep_contracts = 6") || !strings.Contains(sz.Origin, "pep_enabled on") {
		t.Errorf("max_contracts_per_order under pep must name pep_contracts = 6: %+v", sz)
	}
	pc := rows["ai_config.risk_control.planner_contracts"]
	if v, _ := pc.Effective.(string); !strings.HasPrefix(v, "not used in pep mode") {
		t.Errorf("planner_contracts under pep: %+v", pc)
	}
	// pep OFF: the rows are the AI-mode numbers, byte-identical to before.
	off := effRowsFor(t, `{"ai_config":{"risk_control":{"breakeven_enabled":true,"min_confidence":60}}}`, "ninjatrader", "")
	if be := off["ai_config.risk_control.breakeven_enabled"]; be.Effective != true || strings.Contains(be.Origin, "pep") {
		t.Errorf("pep OFF breakeven row changed: %+v", be)
	}
	if mc := off["ai_config.risk_control.min_confidence"]; strings.Contains(mc.Origin, "pep") {
		t.Errorf("pep OFF min_confidence row changed: %+v", mc)
	}
}

// rereview m461 P2-2: a rejected box-start / flat-time pair is blamed on BOTH
// rows, so the box-start row cannot read "saved value 16:00" while the bot
// trades the 08:30 box. Production path: EffectiveSettings.
//
// Mutant: drop the pep_box_start_ct clamp in store.ResolvePep -> RED.
func TestEffectiveRejectedBoxFlatPairIsBlamedOnBothRows(t *testing.T) {
	rows := effRowsFor(t, `{"pep":{"pep_enabled":true,"pep_box_start_ct":"16:00","pep_flat_time_ct":"15:00"}}`, "ninjatrader", "")
	bs, ft := rows["pep.pep_box_start_ct"], rows["pep.pep_flat_time_ct"]
	if bs.Effective != "08:30" || !strings.HasPrefix(bs.Origin, "clamp (store.ResolvePep)") {
		t.Errorf("box start row must read the clamp and the 08:30 that runs: %+v", bs)
	}
	if ft.Effective != "15:00" || !strings.HasPrefix(ft.Origin, "clamp (store.ResolvePep)") {
		t.Errorf("flat time row: %+v", ft)
	}
	if !strings.Contains(bs.Origin, "16:00") {
		t.Errorf("the box-start row must name the rejected 16:00: %+v", bs)
	}
}
