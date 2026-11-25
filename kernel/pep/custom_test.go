package pep

import (
	"reflect"
	"testing"
)

// m461: Custom with entry type breakout and A's numbers trades exactly like
// style A (same intent, only the style label differs); Custom with an edited box
// length reads THAT length. Mutant: make IsBreakout() ignore Custom.Entry -> RED.
func TestCustomBreakoutTradesLikeA(t *testing.T) {
	bars := aDay(t, dayCST, map[string]ohlc{"08:37": {25030, 25041.25, 25029, 25040}})
	now := nowAfter(t, dayCST, "08:37")
	a := mustOne(t, Evaluate(bars, now, cfgA(), State{}))

	c := DefaultConfig()
	c.Style = StyleCustom
	c.Custom = ParamsCustom{Entry: EntryBreakout, BoxMinutes: 5, SLFraction: 1, TPFraction: 1, BothEdges: BothEdgesNoTrade, EMAPeriod: 9, EMATFMin: 2, Source: SourceEMA}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	cu := mustOne(t, Evaluate(bars, now, c, State{}))
	if cu.Style != StyleCustom {
		t.Fatalf("intent style %s", cu.Style)
	}
	a.Style, cu.Style = "", ""
	if !reflect.DeepEqual(a, cu) {
		t.Fatalf("Custom breakout != A:\n%+v\n%+v", a, cu)
	}

	// edited numbers are used: a 0.5 x box stop and a 2 x box target
	c.Custom.SLFraction, c.Custom.TPFraction = 0.5, 2
	e := mustOne(t, Evaluate(bars, now, c, State{}))
	near(t, "stopDist", e.StopDist, 20)
	near(t, "targetDist", e.TargetDist, 80)
}

// The max-box cap applies to Custom (set) - and the A / E rules carry none: the
// resolver zeroes it, so a stored cap never refuses an A or E box.
func TestCustomValidateRefuses(t *testing.T) {
	mut := func(f func(*Config)) Config { c := DefaultConfig(); c.Style = StyleCustom; f(&c); return c }
	for name, c := range map[string]Config{
		"entry":  mut(func(c *Config) { c.Custom.Entry = "zig" }),
		"box":    mut(func(c *Config) { c.Custom.BoxMinutes = 0 }),
		"slfrac": mut(func(c *Config) { c.Custom.SLFraction = 0 }),
		"tf":     mut(func(c *Config) { c.Custom.EMATFMin = 7 }),
		"period": mut(func(c *Config) { c.Custom.EMAPeriod = 1 }),
		"source": mut(func(c *Config) { c.Custom.Source = "x" }),
		"both":   mut(func(c *Config) { c.Custom.Entry = EntryBreakout; c.Custom.BothEdges = "x" }),
	} {
		if c.Validate() == nil {
			t.Errorf("%s: want a validation error", name)
		}
	}
	if err := mut(func(c *Config) {}).Validate(); err != nil {
		t.Fatal(err)
	}
}
