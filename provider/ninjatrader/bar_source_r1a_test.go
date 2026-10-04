package ninjatrader

import (
	"os"
	"testing"
)

// R5: the scale-mismatch knobs carry the env SOURCE of the value in force
// onto the bar-source boot line (VL / default only).
func TestEnvScaleKnobSource(t *testing.T) {
	for name, c := range map[string]struct {
		vl      string
		want    float64
		wantSrc string
	}{
		"VL accepted":           {"0.7", 0.7, "VL"},
		"unset":                 {"", 0, "default"},
		"garbage rejected":      {"garbage", 0, "default"},
		"non-positive rejected": {"-1", 0, "default"},
	} {
		t.Setenv("VL_TEST_KNOB", c.vl)
		got, src := envScaleKnob(os.Getenv("VL_TEST_KNOB"))
		if got != c.want || src != c.wantSrc {
			t.Errorf("%s: envScaleKnob = %v/%s, want %v/%s", name, got, src, c.want, c.wantSrc)
		}
	}
}
