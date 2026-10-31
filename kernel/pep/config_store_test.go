package pep

import (
	"encoding/json"
	"reflect"
	"testing"

	"vl/store"
)

// ONE source of defaults: the evaluator's DefaultConfig equals what the stored
// settings resolve to when nothing is saved. A drift in either table fails here.
func TestDefaultConfigEqualsResolvedStoreDefaults(t *testing.T) {
	got := ConfigOf(nil)
	want := DefaultConfig()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("store defaults and kernel/pep defaults drifted:\n store %+v\n pep   %+v", got, want)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

// A saved block flows through the production path: JSON → StrategyConfig
// (UnmarshalJSON) → ResolvePep → FromSettings → a Config Evaluate accepts, and a
// clamped number arrives clamped. Custom is the editable style (m461).
func TestSavedPepBlockReachesTheEvaluatorConfig(t *testing.T) {
	raw := `{"strategy_type":"ai_trading","pep":{"pep_enabled":true,"pep_style":"Custom","pep_contracts":6,
	  "pep_flat_time_ct":"14:30","style_custom":{"pep_c_box_minutes":3,"pep_c_signal_source":"close","pep_c_entry_type":"ema_cross"}}}`
	var sc store.StrategyConfig
	if err := json.Unmarshal([]byte(raw), &sc); err != nil {
		t.Fatal(err)
	}
	c := ConfigOf(&sc)
	if c.Style != StyleCustom || c.Contracts != 6 || c.FlatMin != 14*60+30 || c.Custom.BoxMinutes != 3 || c.Custom.Source != SourceClose || c.BoxMinutes() != 3 || c.EMA().Source != SourceClose {
		t.Fatalf("%+v", c)
	}
	if c.Custom.EMAPeriod != 9 || c.Custom.SLFraction != 0.5 || c.IsBreakout() {
		t.Fatalf("unsaved leaves must keep the shipped values: %+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	sc.Pep.Contracts = store.IntPtr(99)
	if got := ConfigOf(&sc).Contracts; got != 10 {
		t.Fatalf("a 99-contract save must run as the 10 ceiling, got %d", got)
	}
}

// m461 PIN (production path JSON -> ConfigOf): style A and E ignore edited stored
// numbers; Custom uses the same edits. Mutant: delete the Locked override block
// in store.ResolvePep -> the A and E legs go RED.
func TestAAndEIgnoreEditedStoredNumbersCustomUsesThem(t *testing.T) {
	const body = `"pep_flat_time_ct":"14:30","pep_box_start_ct":"09:00","pep_max_trades_per_day":3,"pep_max_box_points":50,"pep_sl_rounding":"nearest","pep_contracts":3,
	  "style_a":{"pep_a_box_minutes":9,"pep_a_sl_fraction":0.4,"pep_a_tp_fraction":3,"pep_a_both_edges":"nearest_open"},
	  "style_e":{"pep_e_box_minutes":6,"pep_e_sl_fraction":0.9,"pep_e_tp_fraction":2,"pep_e_ema_period":20,"pep_e_ema_tf_min":5,"pep_e_signal_source":"close","pep_e_warmup_bars":7},
	  "style_custom":{"pep_c_entry_type":"breakout","pep_c_box_minutes":9,"pep_c_sl_fraction":0.4,"pep_c_tp_fraction":3,"pep_c_both_edges":"nearest_open"}`
	cfgFor := func(style string) Config {
		var sc store.StrategyConfig
		if err := json.Unmarshal([]byte(`{"strategy_type":"ai_trading","pep":{"pep_style":"`+style+`",`+body+`}}`), &sc); err != nil {
			t.Fatal(err)
		}
		return ConfigOf(&sc)
	}
	pdf := DefaultConfig()
	for _, style := range []string{"A", "E"} {
		c := cfgFor(style)
		want := pdf
		want.Style = Style(style)
		want.Contracts = 3 // contracts stay editable
		// Custom's own block is a stored value too: A/E never read it, but it is resolved.
		want.Custom = c.Custom
		if !reflect.DeepEqual(c, want) {
			t.Fatalf("style %s must run the PDF rules:\n got  %+v\n want %+v", style, c, want)
		}
	}
	c := cfgFor("Custom")
	if c.Style != StyleCustom || !c.IsBreakout() || c.BoxStartMin != 9*60 || c.FlatMin != 14*60+30 || c.MaxTradesPerDay != 3 || c.MaxBoxPoints != 50 ||
		c.SLRounding != RoundNearest || c.BoxMinutes() != 9 || c.SLFraction() != 0.4 || c.TPFraction() != 3 || c.BothEdgesPolicy() != BothEdgesNearestOpen || c.Contracts != 3 {
		t.Fatalf("Custom must use the stored numbers: %+v", c)
	}
}

// m461 PIN: a fresh Custom trades like E - same defaults AND the same intent on
// the same tape. Mutant: change any DefaultConfig().Custom / store Custom default
// -> RED.
func TestCustomDefaultsEqualEAndTradeLikeE(t *testing.T) {
	d := DefaultConfig()
	if d.Custom.BoxMinutes != d.E.BoxMinutes || d.Custom.SLFraction != d.E.SLFraction || d.Custom.TPFraction != d.E.TPFraction ||
		d.Custom.EMAPeriod != d.E.EMAPeriod || d.Custom.EMATFMin != d.E.EMATFMin || d.Custom.Source != d.E.Source || d.Custom.Entry != EntryEMACross {
		t.Fatalf("Custom defaults must equal style E: %+v vs %+v", d.Custom, d.E)
	}
	var sc store.StrategyConfig
	if err := json.Unmarshal([]byte(`{"pep":{"pep_style":"Custom"}}`), &sc); err != nil {
		t.Fatal(err)
	}
	cc := ConfigOf(&sc)
	want := d
	want.Style = StyleCustom
	if !reflect.DeepEqual(cc, want) {
		t.Fatalf("a fresh Custom != the shipped config:\n%+v\n%+v", cc, want)
	}
	bars := eDay(t, dayCST, map[string]ohlc{"08:30": {25000, 25020, 24983.25, 25000}, "08:31": {25000, 25010, 24990, 25000}})
	now := nowAfter(t, dayCST, "08:35")
	e := Evaluate(bars, now, cfgE(), State{})
	cu := Evaluate(bars, now, cc, State{})
	if len(e.Intents) != len(cu.Intents) || len(e.Refusals) != len(cu.Refusals) {
		t.Fatalf("E %+v vs Custom %+v", e, cu)
	}
	for i := range e.Intents {
		a, b := e.Intents[i], cu.Intents[i]
		a.Style, b.Style = "", ""
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("intent %d differs:\n%+v\n%+v", i, a, b)
		}
	}
}

// The SL rounding setting reaches the stop: outward vs nearest on a box whose
// half is not on the grid.
func TestSLRoundingSetting(t *testing.T) {
	bars := eDay(t, dayCST, map[string]ohlc{"08:30": {25000, 25020, 24983.25, 25000}, "08:31": {25000, 25010, 24990, 25000}})
	now := nowAfter(t, dayCST, "08:35")
	out := mustOne(t, Evaluate(bars, now, cfgE(), State{}))
	near(t, "outward 36.75/2 -> 18.5", out.StopDist, 18.5)
	c := cfgE()
	c.SLRounding = RoundNearest
	nr := mustOne(t, Evaluate(bars, now, c, State{}))
	near(t, "nearest 18.375 -> 18.5 (half away) ", nr.StopDist, 18.5)
	// a case where they differ: box 36.25 x 0.4987 = 18.0804 -> outward 18.25, nearest 18.0
	bars = eDay(t, dayCST, map[string]ohlc{"08:30": {25000, 25020, 24983.75, 25000}, "08:31": {25000, 25010, 24990, 25000}})
	frac := cfgE()
	frac.E.SLFraction = 0.4987
	out = mustOne(t, Evaluate(bars, now, frac, State{}))
	frac.SLRounding = RoundNearest
	nr = mustOne(t, Evaluate(bars, now, frac, State{}))
	near(t, "outward -> 18.25", out.StopDist, 18.25)
	near(t, "nearest -> 18.0", nr.StopDist, 18.0)
}
