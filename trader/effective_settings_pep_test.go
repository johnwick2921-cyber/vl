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
	if rows["pep.pep_enabled"].Effective != false || rows["pep.pep_contracts"].Effective != 3 {
		t.Fatalf("shipped: enabled %v contracts %v", rows["pep.pep_enabled"].Effective, rows["pep.pep_contracts"].Effective)
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
