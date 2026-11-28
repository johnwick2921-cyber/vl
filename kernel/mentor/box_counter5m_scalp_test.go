package mentor

import "testing"

// Reviewer P2 (oct-exact): the BOX counter-5m scalp (mentor_box_counter_5m_scalp,
// Oct D3.3-E9 + D5.4-R20 @07:51) is only valid when the counter_5m stamp is
// actually applied; with counter_5m_reduce_size=off the old refusal must stay,
// else a box return against the 5m would be armed full-size.
func boxCounterIntent() Intent {
	return Intent{Action: PlaceStopEntry, Setup: "BOX", Side: SideLong, Price: 100, Stop: 90, Target: 140}
}

func TestBoxCounter5mScalpStampsReducedAndShortTarget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BoxCounter5mScalp = true
	cfg.Counter5mReduceSize = Counter5mReduceOn
	e := New(cfg)
	if !e.boxCounterScalpActive() {
		t.Fatal("scalp must be active with reduce on")
	}
	out := e.stampCounter5m([]Intent{boxCounterIntent()}, SideShort)
	if !HasFlag(out[0].Flag, FlagCounter5m) {
		t.Fatalf("BOX against the 5m must carry %q, got %q", FlagCounter5m, out[0].Flag)
	}
	if out[0].Target >= 140 {
		t.Fatalf("scalp target must be shortened, got %v", out[0].Target)
	}
}

func TestBoxCounter5mScalpRevertAndReduceOffAreNotStamped(t *testing.T) {
	for name, mut := range map[string]func(*Config){
		"revert false":    func(c *Config) { c.BoxCounter5mScalp = false },
		"reduce size off": func(c *Config) { c.BoxCounter5mScalp = true; c.Counter5mReduceSize = Counter5mReduceOff },
	} {
		cfg := DefaultConfig()
		mut(&cfg)
		e := New(cfg)
		if e.boxCounterScalpActive() {
			t.Fatalf("%s: scalp must be inactive (the refusal stays)", name)
		}
		out := e.stampCounter5m([]Intent{boxCounterIntent()}, SideShort)
		if HasFlag(out[0].Flag, FlagCounter5m) || out[0].Target != 140 {
			t.Fatalf("%s: BOX must be untouched, got %+v", name, out[0])
		}
	}
}
