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
// clamped number arrives clamped.
func TestSavedPepBlockReachesTheEvaluatorConfig(t *testing.T) {
	raw := `{"strategy_type":"ai_trading","pep":{"pep_enabled":true,"pep_style":"E","pep_contracts":6,
	  "pep_flat_time_ct":"14:30","style_e":{"pep_e_box_minutes":3,"pep_e_signal_source":"close"},
	  "style_a":{"pep_a_sl_fraction":0.9}}}`
	var sc store.StrategyConfig
	if err := json.Unmarshal([]byte(raw), &sc); err != nil {
		t.Fatal(err)
	}
	c := ConfigOf(&sc)
	if c.Style != StyleE || c.Contracts != 6 || c.FlatMin != 14*60+30 || c.E.BoxMinutes != 3 || c.E.Source != SourceClose || c.A.SLFraction != 0.9 {
		t.Fatalf("%+v", c)
	}
	if c.A.BoxMinutes != 5 || c.E.EMAPeriod != 9 {
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
