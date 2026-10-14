package trader

import (
	"strings"
	"testing"

	"vl/kernel"
	"vl/store"
)

// R22 P2 (PLANNER.md F4) — VWAP frozen at plan write: at PLACEMENT the live
// line is recomputed and the drift logged + counted, the entry untouched.
// Production call site: the market_in_zone placement (placeZoneRow) reached
// through maybeManageArmedOrdersAt, with the scenario's zone a frozen VWAP line
// at 100.00 while the tape's session VWAP sits elsewhere.
func vwapZoneDoc() kernel.PlanDoc {
	d := zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false))
	d.Levels[0].Label = "VWAP"
	structuralTestMap(&d, structuralTestZone{100, 100, 100, "VWAP"}, structuralTestZone{110, 110, 111, "target"})
	return d
}

func TestVWAPDriftLoggedAtPlacementEntryUnchanged(t *testing.T) {
	r := newZoneRig(t, "r22-vwap-drift", vwapZoneDoc())
	logBuf := captureTraderLog(t)
	tape := zoneTape(101.95, r.now, 0) // beyond the zone: the limit rests at the far bound
	for i := range tape {
		tape[i].Volume = 100 // session volume → a live VWAP exists
	}
	r.setTape(tape)
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	sigs, _ := r.drain()
	if len(sigs) != 1 || sigs[0].LimitPrice != 100.5 {
		t.Fatalf("one limit at the far bound 100.50 (entry unchanged by the drift note): %+v", sigs)
	}
	row := r.row("S1")
	if !strings.Contains(row.ZoneProvenance, "VWAP[") {
		t.Fatalf("fixture must yield a VWAP provenance: %q", row.ZoneProvenance)
	}
	log := logBuf.String()
	if !strings.Contains(log, "📐 VWAP drift at placement: "+row.Session+" S1 leg 1 VWAP frozen 100.00") || !strings.Contains(log, "entry 100.50 unchanged") || strings.Contains(log, "live n/a") {
		t.Fatalf("the placement must log the frozen vs live VWAP:\n%s", log)
	}
	if n, _ := store.SystemCounter(r.st, "vwap_drift_at_placement:"+row.Session+":measured"); n != 1 {
		t.Fatalf("the measured drift must be counted once: %d", n)
	}
}

// No session volume → the live line is UNKNOWN: printed n/a, counted apart.
func TestVWAPDriftLiveUnknownSaysNA(t *testing.T) {
	r := newZoneRig(t, "r22-vwap-na", vwapZoneDoc())
	logBuf := captureTraderLog(t)
	r.setTape(zoneTape(101.95, r.now, 0)) // volume 0
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	r.drain()
	if !strings.Contains(logBuf.String(), "live n/a (no session volume yet) · drift n/a") {
		t.Fatalf("unknown live VWAP must print n/a:\n%s", logBuf.String())
	}
	if n, _ := store.SystemCounter(r.st, "vwap_drift_at_placement:"+r.row("S1").Session+":live_unknown"); n != 1 {
		t.Fatalf("live_unknown must be counted: %d", n)
	}
}

func TestVWAPProvenanceParse(t *testing.T) {
	for _, c := range []struct {
		prov, label string
		px          float64
		known, ok   bool
	}{
		{"frozen_line:VWAP[30798.92,30798.92]", "VWAP", 30798.92, true, true},
		{"frozen_line:VWAP−1σ[30832.71,30832.71]", "VWAP−1σ", 30832.71, true, true},
		{"frozen_line:eVWAP[30867.77,30867.77]", "eVWAP", 30867.77, true, true},
		{"authored:VWAP+2σ:VWAP+2σ[30900.00,30900.00]", "VWAP+2σ", 30900, true, true},
		{"frozen_overlap:PDH+VWAP+1σ[30800.00,30810.00]", "VWAP+1σ", 0, false, true},
		{"frozen_line:pdVWAP[30700.00,30700.00]", "", 0, false, false},
		{"frozen_subrange:PDH[98.50,100.00]", "", 0, false, false},
	} {
		l, px, known, ok := vwapProvenance(c.prov)
		if l != c.label || ok != c.ok || known != c.known || (known && px != c.px) {
			t.Errorf("%s → %q %.2f known=%v ok=%v", c.prov, l, px, known, ok)
		}
	}
}
