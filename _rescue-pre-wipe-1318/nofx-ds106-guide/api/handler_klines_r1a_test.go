package api

import (
	"testing"

	"vl/internal/envcompat"
)

// R1a: the chart-across-roll boot-line word names the env SOURCE (VL/NOFX/
// default), not a guess.
func TestChartAcrossRollResolvedNamesTheSource(t *testing.T) {
	for _, c := range []struct {
		on   bool
		src  envcompat.Source
		want string
	}{
		{true, envcompat.SourceDefault, "on[O]"},
		{false, envcompat.SourceVL, "off[env:VL]"},
		{false, envcompat.SourceNOFX, "off[env:NOFX]"},
		{false, envcompat.SourceDefault, "off[env:default]"},
	} {
		if got := chartAcrossRollResolved(c.on, c.src); got != c.want {
			t.Errorf("chartAcrossRollResolved(%v, %s) = %s, want %s", c.on, c.src, got, c.want)
		}
	}
}
