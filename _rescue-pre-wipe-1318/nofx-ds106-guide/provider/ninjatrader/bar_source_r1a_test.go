package ninjatrader

import (
	"testing"

	"vl/internal/envcompat"
)

// R1a: the scale-mismatch knobs carry the env SOURCE of the value in force
// onto the bar-source boot line.
func TestEnvScaleKnobSource(t *testing.T) {
	for name, c := range map[string]struct {
		vl, nofx string
		want     float64
		wantSrc  envcompat.Source
	}{
		"VL wins":               {"0.7", "0.3", 0.7, envcompat.SourceVL},
		"NOFX fallback":         {"", "0.4", 0.4, envcompat.SourceNOFX},
		"both empty":            {"", "", 0, envcompat.SourceDefault},
		"garbage rejected":      {"garbage", "0.4", 0, envcompat.SourceDefault},
		"non-positive rejected": {"-1", "0.4", 0, envcompat.SourceDefault},
	} {
		t.Setenv("VL_TEST_KNOB", c.vl)
		t.Setenv("NOFX_TEST_KNOB", c.nofx)
		got, src := envScaleKnob(envcompat.Env("TEST_KNOB"))
		if got != c.want || src != c.wantSrc {
			t.Errorf("%s: envScaleKnob = %v/%s, want %v/%s", name, got, src, c.want, c.wantSrc)
		}
	}
}
