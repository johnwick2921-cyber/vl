package kernel

// W1 (g) — the (value, source) twins must equal the value-only entry points the
// order path, the arm seam and the engine gate call, across the whole matrix.

import (
	"testing"

	"vl/store"
)

func TestResolveMaxContractsWithSourceParity(t *testing.T) {
	for _, env := range []string{"", "3", "1", "0", "x"} {
		t.Setenv("STAGE_A_CONTRACT_CAP", env)
		for _, per := range []int{-1, 0, 1, 2, 5} {
			for _, def := range []int{1, 2} {
				n, src := ResolveMaxContractsWithSource(per, def)
				if want := ResolveMaxContracts(per, def); n != want {
					t.Fatalf("env=%q per=%d def=%d: twin %d, ResolveMaxContracts %d", env, per, def, n, want)
				}
				if want := ClampStageAContracts(map[bool]int{true: per, false: def}[per > 0]); n != want {
					t.Fatalf("env=%q per=%d def=%d: twin %d, ClampStageAContracts %d", env, per, def, n, want)
				}
				if src == "" {
					t.Fatalf("empty source")
				}
			}
		}
	}
	t.Setenv("STAGE_A_CONTRACT_CAP", "3")
	if _, src := ResolveMaxContractsWithSource(5, 2); src != StageAContractCapEnvSource {
		t.Fatalf("env cap bound: %q", src)
	}
	if _, src := ResolveMaxContractsWithSource(2, 2); src != store.SourceSaved {
		t.Fatalf("saved under the cap: %q", src)
	}
	t.Setenv("STAGE_A_CONTRACT_CAP", "")
	if n, src := ResolveMaxContractsWithSource(0, 2); n != 1 || src != "clamp (kernel.ClampStageAContracts)" {
		t.Fatalf("default clamped by the Stage-A constant: %d %q", n, src)
	}
	if n, src := StageAContractCapWithSource(); n != StageAContractCap() || src == StageAContractCapEnvSource {
		t.Fatalf("stage-A twin: %d %q", n, src)
	}
}

func TestResolveNotionalLeverageWithSourceParity(t *testing.T) {
	for _, per := range []float64{-1, 0, 0.5, 20, 30} {
		v, src := ResolveNotionalLeverageWithSource(per, 20)
		if v != ResolveNotionalLeverage(per, 20) {
			t.Fatalf("per=%v: twin %v", per, v)
		}
		if (per > 0) != (src == store.SourceSaved) {
			t.Fatalf("per=%v: source %q", per, src)
		}
	}
}

func TestConditionStatusWithSourceParity(t *testing.T) {
	bases := []map[string]string{nil, {"reclaim": "shadow"}, {"fvg_entry": "live", "hold": "bogus"}}
	sessions := []map[string]string{nil, {"fvg_entry": "shadow"}, {"reclaim": "live"}}
	envs := []string{"", "breakout_retest=live", "hold", "hold=shadow,hold=live", "fvg_entry=shadow"}
	for _, b := range bases {
		for _, s := range sessions {
			for _, e := range envs {
				for _, c := range append(KnownConditions(), "", "  RECLAIM ") {
					st, src := ConditionStatusWithSource(c, b, s, e)
					if want := ConditionStatus(c, b, s, e); st != want {
						t.Fatalf("%q base=%v sess=%v env=%q: twin %q, ConditionStatus %q", c, b, s, e, st, want)
					}
					if src == "" {
						t.Fatalf("empty source")
					}
				}
			}
		}
	}
	for _, tc := range []struct {
		c    string
		base map[string]string
		sess map[string]string
		env  string
		src  string
	}{
		{"fvg_entry", map[string]string{"fvg_entry": "live"}, map[string]string{"fvg_entry": "shadow"}, "", store.SourceSessionOverride},
		{"fvg_entry", map[string]string{"fvg_entry": "live"}, nil, "fvg_entry=shadow", store.SourceStrategyValue},
		{"hold", nil, nil, "hold=shadow,hold=live", ConditionSourceEnvLive},
		{"hold", nil, nil, "hold", ConditionSourceEnvShadow},
		{"breakout_retest", nil, nil, "", store.SourceShippedDefault},
		{"reclaim", nil, nil, "", store.SourceShippedDefault},
	} {
		if _, src := ConditionStatusWithSource(tc.c, tc.base, tc.sess, tc.env); src != tc.src {
			t.Fatalf("%s: source %q, want %q", tc.c, src, tc.src)
		}
	}
}

// The posture the engine gate builds from: unset toggles take the gate's
// shipped defaults — since r22 (owner R4 + R5) EVERY toggle OFF, master and
// daily-loss included; the $ value keeps its env fallback (500 here) — saved
// values win.
func TestResolveStrategyGuardrailsPosture(t *testing.T) {
	p := ResolveStrategyGuardrails(store.RiskControlConfig{}, 500)
	if p.MasterEnabled || p.DailyLossEnabled || p.DailyLossLimitUSD != 500 ||
		p.DailyProfitEnabled || p.MaxDailyTradesEnabled || p.BlackoutEnabled || p.ConsistencyEnabled {
		t.Fatalf("unset posture: %+v", p)
	}
	off, on := false, true
	p = ResolveStrategyGuardrails(store.RiskControlConfig{
		GuardrailsEnabled: &off, DailyLossEnabled: &off, DailyLossLimitUSD: 450,
		DailyProfitEnabled: &on, DailyProfitTargetUSD: 900, MaxDailyTradesEnabled: &on, MaxDailyTrades: 4,
		BlackoutEnabled: &on, ConsistencyEnabled: &on,
	}, 500)
	if p.MasterEnabled || p.DailyLossEnabled || p.DailyLossLimitUSD != 450 || !p.DailyProfitEnabled ||
		p.DailyProfitTargetUSD != 900 || !p.MaxDailyTradesEnabled || p.MaxDailyTrades != 4 || !p.BlackoutEnabled || !p.ConsistencyEnabled {
		t.Fatalf("saved posture: %+v", p)
	}
}
