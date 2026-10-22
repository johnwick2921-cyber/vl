package mentor

import "testing"

// TestPhlISBPreferenceFilter — D3.3 p1 @08:12–08:56: a box-edge reject and an
// inside bar on the SAME closed bar, SAME side — the mentor takes the inside bar
// ("Nên về xuống InstaBar… em lại an toàn với InstaBar hơn") because its stop is
// smaller. MUTANT: drop the `isb <= boxStop` condition (always refuse the box)
// → the larger-ISB-stop case refuses the box → RED.
func TestPhlISBPreferenceFilter(t *testing.T) {
	box := func(stopDist float64) Intent {
		return Intent{Action: PlaceStopEntry, Setup: "BOX", Side: SideLong, Price: 100 + stopDist, Stop: 100, RefBarMs: 1000}
	}
	isb := func(stopDist float64) Intent {
		return Intent{Action: PlaceStopEntry, Setup: "ISB", Side: SideLong, Price: 100 + stopDist, Stop: 100, RefBarMs: 1000}
	}

	// ISB stop <= box stop → keep the ISB, refuse the box.
	got, refusals, _ := phlISBPreferenceFilter([]Intent{box(7), isb(5)})
	if len(got) != 1 || got[0].Setup != "ISB" {
		t.Fatalf("smaller ISB stop: kept %+v, want only the ISB", got)
	}
	if len(refusals) != 1 || refusals[0] != "phl_isb_preferred_at_box" {
		t.Fatalf("refusals = %v, want [phl_isb_preferred_at_box]", refusals)
	}

	// ISB stop > box stop → keep BOTH, note the case (never a silent drop).
	got, refusals, notes := phlISBPreferenceFilter([]Intent{box(7), isb(9)})
	if len(got) != 2 {
		t.Fatalf("larger ISB stop: kept %+v, want both", got)
	}
	if len(refusals) != 0 {
		t.Fatalf("larger ISB stop must not refuse, got %v", refusals)
	}
	if len(notes) == 0 {
		t.Fatal("larger ISB stop must be noted")
	}

	// Opposite sides → no precedence (both stay, no refusal).
	got, refusals, _ = phlISBPreferenceFilter([]Intent{
		{Action: PlaceStopEntry, Setup: "BOX", Side: SideLong, Price: 107, Stop: 100, RefBarMs: 1000},
		{Action: PlaceStopEntry, Setup: "ISB", Side: SideShort, Price: 95, Stop: 100, RefBarMs: 1000},
	})
	if len(got) != 2 || len(refusals) != 0 {
		t.Fatalf("opposite sides must not precedence: kept %+v refusals %v", got, refusals)
	}
}
