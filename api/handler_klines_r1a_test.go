package api

import (
	"testing"
)

// R5: the chart-across-roll boot-line word names the env SOURCE (VL/
// default), not a guess.
func TestChartAcrossRollResolvedNamesTheSource(t *testing.T) {
	for _, c := range []struct {
		on   bool
		src  string
		want string
	}{
		{true, "default", "on[O]"},
		{false, "VL", "off[env:VL]"},
		{false, "default", "off[env:default]"},
	} {
		if got := chartAcrossRollResolved(c.on, c.src); got != c.want {
			t.Errorf("chartAcrossRollResolved(%v, %s) = %s, want %s", c.on, c.src, got, c.want)
		}
	}
}
