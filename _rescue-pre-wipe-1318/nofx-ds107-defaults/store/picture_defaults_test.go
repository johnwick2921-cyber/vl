package store

import "testing"

// DEFAULTS-SANE (2026-09-24): the shipped Picture defaults must be reachable by
// the real pipeline (2-minute trader cadence, hour-boundary frame storms). The
// resolver is the single canonicalizer — the live path and the boot line both
// read it, so these pins sit at the production call site (canon 53), not at a
// rebuilt copy of the values.
func TestPictureHtfResolvedDefaultsAreSane(t *testing.T) {
	cases := map[string]*PictureHtfConfig{
		"nil config":  nil,
		"zero struct": {},
		"enabled only": {Enabled: true},
	}
	for name, c := range cases {
		out := PictureHtfResolved(c)
		if out.EntryWindowSec != PictureHtfDefaultEntryWindowSec {
			t.Fatalf("%s: entry window default = %d, want %d", name, out.EntryWindowSec, PictureHtfDefaultEntryWindowSec)
		}
		if out.FreshnessSec != PictureHtfDefaultFreshnessSec {
			t.Fatalf("%s: freshness default = %d, want %d", name, out.FreshnessSec, PictureHtfDefaultFreshnessSec)
		}
	}
}

// An explicitly saved value is the owner's value — defaults never override it.
func TestPictureHtfResolvedHonorsExplicitValues(t *testing.T) {
	out := PictureHtfResolved(&PictureHtfConfig{Enabled: true, EntryWindowSec: 10, FreshnessSec: 2})
	if out.EntryWindowSec != 10 || out.FreshnessSec != 2 {
		t.Fatalf("explicit knob values must survive the resolver, got %d/%d", out.EntryWindowSec, out.FreshnessSec)
	}
}
