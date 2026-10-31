package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func boolp(b bool) *bool      { return &b }
func f64p(f float64) *float64 { return &f }

// pepLeafPaths are the schema paths the pep block marshals to.
func pepLeafPaths(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, p := range EnumerateSchemaKnobs() {
		if strings.HasPrefix(p, "pep.") {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		t.Fatal("the schema walk found no pep.* path: the block is not in MarshalJSON")
	}
	return out
}

// Every pep leaf has a registry entry by EXACT path (not the leaf fallback), is
// live with a named consumer, and the leaf name is unique across the schema.
func TestPepLeavesRegisteredExactlyAndUnique(t *testing.T) {
	paths := pepLeafPaths(t)
	if len(paths) != 31 {
		t.Fatalf("pep leaves = %d, want 31 (11 shared + 4 style A + 7 style E + 9 style Custom): %v", len(paths), paths)
	}
	leaf := func(p string) string { return p[strings.LastIndex(p, ".")+1:] }
	count := map[string]int{}
	for _, p := range EnumerateSchemaKnobs() {
		count[leaf(p)]++
	}
	for _, p := range paths {
		e, ok, how := lookupKnob(p)
		if !ok || how != knobExact {
			t.Errorf("%s: want an EXACT registry entry, got ok=%v match=%v", p, ok, how)
			continue
		}
		if e.Status != KnobLive || len(e.Consumers) == 0 {
			t.Errorf("%s: status %s consumers %v", p, e.Status, e.Consumers)
		}
		if !strings.HasPrefix(leaf(p), "pep_") {
			t.Errorf("%s: leaf %q is not pep_-prefixed", p, leaf(p))
		}
		if count[leaf(p)] != 1 {
			t.Errorf("%s: leaf name %q is shared by %d schema paths (the registry falls back on it)", p, leaf(p), count[leaf(p)])
		}
	}
}

// MarshalJSON AND UnmarshalJSON both carry the block: a populated pep block
// survives a full marshal → unmarshal → marshal round trip byte for byte.
func TestPepBlockRoundTrips(t *testing.T) {
	in := StrategyConfig{StrategyType: "ai_trading", Pep: &PepConfig{
		Enabled: boolp(true), Style: "E", Contracts: IntPtr(6), BoxStartCT: "08:30", FlatTimeCT: "15:00",
		TickSize: f64p(0.25), SLRounding: "outward", MaxTradesPerDay: IntPtr(1), FeePerContractRT: f64p(0),
		MaxBoxPoints: f64p(0), SignalMaxAgeMin: IntPtr(2),
		StyleA: &PepStyleAConfig{BoxMinutes: IntPtr(5), SLFraction: f64p(1), TPFraction: f64p(1), BothEdges: "no_trade"},
		StyleE: &PepStyleEConfig{BoxMinutes: IntPtr(2), SLFraction: f64p(0.5), TPFraction: f64p(1), EMAPeriod: IntPtr(9),
			EMATFMin: IntPtr(2), SignalSource: "ema", WarmupBars: IntPtr(0)},
	}}
	b1, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var back StrategyConfig
	if err := json.Unmarshal(b1, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in.Pep, back.Pep) {
		t.Fatalf("unmarshal dropped or changed the pep block:\n in  %+v\n out %+v", in.Pep, back.Pep)
	}
	b2, _ := json.Marshal(back)
	if string(b1) != string(b2) {
		t.Fatalf("round trip is not byte-stable:\n%s\n%s", b1, b2)
	}
	// explicit zeros survive (pointer fields): fee 0, max box 0, warmup 0
	if !strings.Contains(string(b1), `"pep_fee_per_contract_usd":0`) || !strings.Contains(string(b1), `"pep_e_warmup_bars":0`) {
		t.Fatalf("an explicit 0 was erased by omitempty: %s", b1)
	}
	// grid strategies keep the block too (ROOT placement)
	grid := StrategyConfig{StrategyType: "grid_trading", Pep: in.Pep}
	gb, _ := json.Marshal(grid)
	if !strings.Contains(string(gb), `"pep"`) {
		t.Fatal("a grid switch dropped the root pep block")
	}
}

// No pep block, no pep key: every existing strategy stays byte-identical.
func TestNoPepBlockEmitsNoKey(t *testing.T) {
	b, _ := json.Marshal(StrategyConfig{StrategyType: "ai_trading"})
	if strings.Contains(string(b), "pep") {
		t.Fatalf("a strategy without pep emitted a pep key: %s", b)
	}
	if (*StrategyConfig)(nil).PepEnabled() || (&StrategyConfig{}).PepEnabled() || (&StrategyConfig{Pep: &PepConfig{}}).PepEnabled() {
		t.Fatal("pep must default OFF")
	}
}

func TestResolvePepShippedDefaults(t *testing.T) {
	r := ResolvePep(nil)
	if r.Enabled || r.Style != "E" || r.Contracts != 6 || r.BoxStartCT != "08:30" || r.FlatTimeCT != "15:00" || r.BoxStartMin != 510 || r.FlatMin != 900 ||
		r.TickSize != 0.25 || r.SLRounding != "outward" || r.MaxTradesPerDay != 1 || r.FeePerContractRT != 1 || r.MaxBoxPoints != 0 || r.SignalMaxAgeMin != 2 {
		t.Fatalf("shared defaults: %+v", r)
	}
	if r.A != (PepResolvedA{5, 1, 1, "no_trade"}) {
		t.Fatalf("style A defaults: %+v", r.A)
	}
	if r.E != (PepResolvedE{2, 0.5, 1, 9, 2, "ema", 0}) {
		t.Fatalf("style E defaults: %+v", r.E)
	}
	if len(r.Clamps) != 0 {
		t.Fatalf("defaults clamped something: %v", r.Clamps)
	}
	if !r.Locked {
		t.Fatal("the shipped style E must be locked to the PDF")
	}
	// m461 pin: Custom's shipped defaults ARE style E's PDF values.
	if r.Custom != (PepResolvedCustom{"ema_cross", r.E.BoxMinutes, r.E.SLFraction, r.E.TPFraction, "no_trade", r.E.EMAPeriod, r.E.EMATFMin, r.E.SignalSource, r.E.WarmupBars}) {
		t.Fatalf("Custom defaults must equal style E: %+v vs %+v", r.Custom, r.E)
	}
}

// A saved value that is legal takes effect AS SAVED, including a legal 0 - for
// the CUSTOM style, the only editable one (m461).
func TestResolvePepSavedValuesTakeEffect(t *testing.T) {
	c := &StrategyConfig{Pep: &PepConfig{
		Enabled: boolp(true), Style: " custom ", Contracts: IntPtr(6), BoxStartCT: "9:05", FlatTimeCT: "14:30",
		FeePerContractRT: f64p(0), MaxBoxPoints: f64p(150), SLRounding: "Nearest", MaxTradesPerDay: IntPtr(3),
		StyleCustom: &PepStyleCustomConfig{EntryType: "Breakout", BoxMinutes: IntPtr(7), SLFraction: f64p(0.6), TPFraction: f64p(1.4), BothEdges: "Nearest_Open",
			SignalSource: "CLOSE", EMAPeriod: IntPtr(12), EMATFMin: IntPtr(3), WarmupBars: IntPtr(20)},
	}}
	r := ResolvePep(c)
	if !r.Enabled || r.Style != "Custom" || r.Locked || r.Contracts != 6 || r.BoxStartCT != "09:05" || r.BoxStartMin != 545 || r.FlatMin != 870 ||
		r.FeePerContractRT != 0 || r.MaxBoxPoints != 150 || r.SLRounding != "nearest" || r.MaxTradesPerDay != 3 {
		t.Fatalf("%+v", r)
	}
	want := PepResolvedCustom{"breakout", 7, 0.6, 1.4, "nearest_open", 12, 3, "close", 20}
	if r.Custom != want {
		t.Fatalf("Custom %+v want %+v", r.Custom, want)
	}
	if len(r.Clamps) != 0 {
		t.Fatalf("legal values were clamped: %v", r.Clamps)
	}
}

// m461 PIN: A and E run the PDF whatever is stored - edited stored root rule
// numbers, style_a and style_e blocks are ignored, and no clamp note is raised
// for a number that never ran. Mutant: skip the `if r.Style != PepStyleCustom`
// override block in ResolvePep, or read sa/se again -> RED.
func TestResolvePepAAndEIgnoreStoredNumbers(t *testing.T) {
	for _, style := range []string{"A", "E", "a", ""} {
		c := &StrategyConfig{Pep: &PepConfig{
			Enabled: boolp(true), Style: style, Contracts: IntPtr(4), BoxStartCT: "09:05", FlatTimeCT: "14:30",
			MaxBoxPoints: f64p(150), SLRounding: "nearest", MaxTradesPerDay: IntPtr(3), TickSize: f64p(0.5), SignalMaxAgeMin: IntPtr(5),
			StyleE: &PepStyleEConfig{SignalSource: "close", EMAPeriod: IntPtr(12), EMATFMin: IntPtr(3), SLFraction: f64p(0.6), WarmupBars: IntPtr(20), BoxMinutes: IntPtr(9)},
			StyleA: &PepStyleAConfig{BothEdges: "nearest_open", SLFraction: f64p(0.8), TPFraction: f64p(2), BoxMinutes: IntPtr(15)},
		}}
		r := ResolvePep(c)
		if !r.Locked || r.BoxStartCT != "08:30" || r.BoxStartMin != 510 || r.FlatTimeCT != "15:00" || r.FlatMin != 900 ||
			r.MaxBoxPoints != 0 || r.SLRounding != "outward" || r.MaxTradesPerDay != 1 {
			t.Fatalf("style %q: rule leaves must be the PDF's: %+v", style, r)
		}
		if r.A != (PepResolvedA{5, 1, 1, "no_trade"}) || r.E != (PepResolvedE{2, 0.5, 1, 9, 2, "ema", 0}) {
			t.Fatalf("style %q: A %+v E %+v", style, r.A, r.E)
		}
		// contracts are the one editable number; the tick and the stale-signal age
		// are rules (reviewer P2): the PDF's 0.25 / 2 run whatever is stored.
		if r.Contracts != 4 || r.TickSize != 0.25 || r.SignalMaxAgeMin != 2 {
			t.Fatalf("style %q: contracts editable, tick/age locked: %+v", style, r)
		}
		if len(r.Clamps) != 0 {
			t.Fatalf("style %q: a clamp note for a number that never ran: %v", style, r.Clamps)
		}
		// the stored numbers are untouched (sacred).
		if *c.Pep.StyleE.EMAPeriod != 12 || c.Pep.BoxStartCT != "09:05" {
			t.Fatal("ResolvePep mutated the stored block")
		}
	}
}

// Contracts are editable in EVERY style (shared; ceiling 10).
func TestResolvePepContractsEditableInEveryStyle(t *testing.T) {
	for _, style := range []string{"A", "E", "Custom"} {
		for n, want := range map[int]int{1: 1, 8: 8, 10: 10, 99: 10} {
			r := ResolvePep(&StrategyConfig{Pep: &PepConfig{Style: style, Contracts: IntPtr(n)}})
			if r.Contracts != want {
				t.Errorf("style %s contracts %d -> %d, want %d", style, n, r.Contracts, want)
			}
		}
	}
}

// OUT OF RANGE is clamped AND named: never silent.
func TestResolvePepClampsAreNamed(t *testing.T) {
	c := &StrategyConfig{Pep: &PepConfig{
		Style: "Custom", Contracts: IntPtr(15), TickSize: f64p(-1), MaxTradesPerDay: IntPtr(0), SignalMaxAgeMin: IntPtr(99),
		BoxStartCT: "25:00", SLRounding: "inward", FeePerContractRT: f64p(-3), MaxBoxPoints: f64p(-5),
		StyleCustom: &PepStyleCustomConfig{EntryType: "zig", BoxMinutes: IntPtr(500), SLFraction: f64p(0), BothEdges: "both",
			EMAPeriod: IntPtr(1), EMATFMin: IntPtr(7), SignalSource: "x", WarmupBars: IntPtr(-1), TPFraction: f64p(99)},
	}}
	r := ResolvePep(c)
	if r.Style != "Custom" || r.Contracts != 10 || r.TickSize != 0.25 || r.MaxTradesPerDay != 1 || r.SignalMaxAgeMin != 30 || r.BoxStartCT != "08:30" ||
		r.SLRounding != "outward" || r.FeePerContractRT != 0 || r.MaxBoxPoints != 0 {
		t.Fatalf("shared: %+v", r)
	}
	if cu := r.Custom; cu.EntryType != "ema_cross" || cu.BoxMinutes != 120 || cu.SLFraction != 0.5 || cu.BothEdges != "no_trade" ||
		cu.EMAPeriod != 2 || cu.EMATFMin != 2 || cu.SignalSource != "ema" || cu.WarmupBars != 0 || cu.TPFraction != 10 {
		t.Fatalf("Custom: %+v", cu)
	}
	if z := ResolvePep(&StrategyConfig{Pep: &PepConfig{Style: "Z"}}); z.Style != "E" || z.Clamps["pep_style"] == "" {
		t.Fatalf("an unknown style must run E and say so: %+v", z)
	}
	want := []string{"pep_contracts", "pep_tick_size", "pep_max_trades_per_day", "pep_signal_max_age_min", "pep_box_start_ct",
		"pep_sl_rounding", "pep_fee_per_contract_usd", "pep_max_box_points", "pep_c_entry_type", "pep_c_box_minutes", "pep_c_sl_fraction", "pep_c_both_edges",
		"pep_c_ema_period", "pep_c_ema_tf_min", "pep_c_signal_source", "pep_c_warmup_bars", "pep_c_tp_fraction"}
	var missing []string
	for _, k := range want {
		if r.Clamps[k] == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("clamped leaves with no clamp note: %v (notes %v)", missing, r.Clamps)
	}
	notes := r.PepClampNotes()
	if !sort.StringsAreSorted(notes) || !strings.HasPrefix(notes[0], "pep.") {
		t.Fatalf("notes: %v", notes)
	}
	if got := LogPepClamps("test", c); len(got) != len(notes) {
		t.Fatalf("LogPepClamps returned %d notes, want %d", len(got), len(notes))
	}
}

func TestResolvePepFlatBeforeBoxFallsBack(t *testing.T) {
	r := ResolvePep(&StrategyConfig{Pep: &PepConfig{Style: "Custom", BoxStartCT: "10:00", FlatTimeCT: "09:00"}})
	if r.BoxStartCT != "08:30" || r.FlatTimeCT != "15:00" || r.Clamps["pep_flat_time_ct"] == "" {
		t.Fatalf("%+v", r)
	}
}

// A trader is exactly one of {AI/planner, mentor, pep}.
func TestTradingModeConflict(t *testing.T) {
	on := &PepConfig{Enabled: boolp(true)}
	m := StrategyConfig{Pep: on}
	m.RiskControl.MentorMode = true
	if e := m.TradingModeConflict(); e == nil || e.Code != PepConflictMentor || !strings.Contains(e.Message, "mentor") {
		t.Fatalf("pep+mentor: %v", e)
	}
	d := StrategyConfig{Pep: on, DayPlan: &DayPlanConfig{PlanEnabled: true}}
	if e := d.TradingModeConflict(); e == nil || e.Code != PepConflictDayPlan {
		t.Fatalf("pep+day plan: %v", e)
	}
	// pep OFF with mentor ON is legal, as is pep ON alone
	off := StrategyConfig{Pep: &PepConfig{Enabled: boolp(false)}}
	off.RiskControl.MentorMode = true
	if off.TradingModeConflict() != nil {
		t.Fatal("pep off + mentor on must be legal")
	}
	if (&StrategyConfig{Pep: on}).TradingModeConflict() != nil {
		t.Fatal("pep alone must be legal")
	}
}

func TestGuardStrategyModesOnTheSavedJSON(t *testing.T) {
	both := StrategyConfig{StrategyType: "ai_trading", Pep: &PepConfig{Enabled: boolp(true)}}
	both.RiskControl.MentorMode = true
	b, _ := json.Marshal(both)
	err := guardStrategyModes(string(b))
	var mc *ModeConflictError
	if !errors.As(err, &mc) || mc.Code != PepConflictMentor {
		t.Fatalf("guard: %v", err)
	}
	if guardStrategyModes(`{"strategy_type":"ai_trading"}`) != nil || guardStrategyModes(`not json pep_enabled`) != nil {
		t.Fatal("the guard must not fire without a pep block or on junk")
	}
}

// THE PRODUCTION WRITERS refuse pep + mentor / pep + day plan with the named
// error, leave the stored row untouched, and still accept a legal pep save.
func TestStrategyStoreWritersRefuseModeConflict(t *testing.T) {
	st := seedTruthStore(t)
	s := st.Strategy()
	const id = "syn-breaker-0-nested-confirmed"
	before, err := s.Get("u1", id)
	if err != nil {
		t.Fatal(err)
	}

	bad := StrategyConfig{StrategyType: "ai_trading", Pep: &PepConfig{Enabled: boolp(true)}}
	bad.RiskControl.MentorMode = true
	badJSON, _ := json.Marshal(bad)
	badDP := StrategyConfig{StrategyType: "ai_trading", Pep: &PepConfig{Enabled: boolp(true)}, DayPlan: &DayPlanConfig{PlanEnabled: true}}
	badDPJSON, _ := json.Marshal(badDP)

	var mc *ModeConflictError
	for name, run := range map[string]func() error{
		"Update": func() error { return s.Update(&Strategy{ID: id, UserID: "u1", Name: "x", Config: string(badJSON)}) },
		"UpdateWithExplicitZeros": func() error {
			return s.UpdateWithExplicitZeros(&Strategy{ID: id, UserID: "u1", Name: "x", Config: string(badJSON)}, &bad)
		},
		"Create": func() error {
			return s.Create(&Strategy{ID: "pep-bad-create", UserID: "u1", Name: "x", Config: string(badJSON)})
		},
		"CreateWithExplicitZeros": func() error {
			return s.CreateWithExplicitZeros(&Strategy{ID: "pep-bad-create2", UserID: "u1", Name: "x", Config: string(badJSON)}, &bad)
		},
		"UpdateDayPlan": func() error { return s.Update(&Strategy{ID: id, UserID: "u1", Name: "x", Config: string(badDPJSON)}) },
	} {
		err := run()
		if !errors.As(err, &mc) {
			t.Errorf("%s: want *ModeConflictError, got %v", name, err)
		}
	}
	after, _ := s.Get("u1", id)
	if after.Config != before.Config || after.Name != before.Name {
		t.Fatal("a refused save changed the stored row")
	}
	if _, err := s.Get("u1", "pep-bad-create"); err == nil {
		t.Fatal("a refused create left a row")
	}

	ok := StrategyConfig{StrategyType: "ai_trading", Pep: &PepConfig{Enabled: boolp(true), Style: "A"}}
	okJSON, _ := json.Marshal(ok)
	if err := s.UpdateWithExplicitZeros(&Strategy{ID: id, UserID: "u1", Name: "x", Config: string(okJSON)}, &ok); err != nil {
		t.Fatalf("a legal pep save was refused: %v", err)
	}
}

// rereview m461 P2-2: the pair is rejected as a pair, so BOTH leaves carry the
// clamp, and the save warning prints it once, naming both leaves.
// Mutant: drop r.Clamps["pep_box_start_ct"] in ResolvePep -> RED.
func TestResolvePepRejectedPairClampsBothLeavesAndWarnsOnce(t *testing.T) {
	c := &StrategyConfig{Pep: &PepConfig{Style: "Custom", BoxStartCT: "16:00", FlatTimeCT: "15:00"}}
	r := ResolvePep(c)
	if r.BoxStartCT != "08:30" || r.FlatTimeCT != "15:00" || r.Clamps["pep_box_start_ct"] == "" || r.Clamps["pep_flat_time_ct"] == "" {
		t.Fatalf("%+v", r)
	}
	notes := r.PepClampNotes()
	if len(notes) != 1 || !strings.Contains(notes[0], "pep.pep_box_start_ct") || !strings.Contains(notes[0], "pep.pep_flat_time_ct") {
		t.Fatalf("one warning naming both leaves, got %q", notes)
	}
}

// rereview m461 P3: the store guard's fast path is case-insensitive like Go's
// JSON decoder, so a hand-written upper-case key cannot skip it.
// Mutant: strings.Contains(configJSON, "pep_enabled") (case-sensitive) -> RED.
func TestGuardStrategyModesIsCaseInsensitiveOnTheKey(t *testing.T) {
	raw := `{"strategy_type":"ai_trading","ai_config":{"risk_control":{"mentor_mode":true}},"PEP":{"PEP_ENABLED":true}}`
	var me *ModeConflictError
	if err := guardStrategyModes(raw); !errors.As(err, &me) || me.Code != PepConflictMentor {
		t.Fatalf("an upper-case PEP_ENABLED key with mentor mode must be refused by name, got %v", err)
	}
}
