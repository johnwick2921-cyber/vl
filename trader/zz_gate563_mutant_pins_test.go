package trader

// release #31 (leg-3 bracket) — mutant pins for the surviving gate #563
// mutants. Each test drives a PRODUCTION call site so the anchored mutant goes
// RED when re-applied (mutants.py).

import (
	"testing"

	ntwire "vl/provider/ninjatrader"
	nttrader "vl/trader/ninjatrader"
)

// M16 (mentorRunnerLeg scans only legs 0-1) + M19 (resonance TP refuses wire 3):
// the runner of a 3-pair plan is wire leg 3, so the runner lookup AND the
// resonance TP modify must address wire 3. A mutant that scans only legs 0-1,
// or one that only accepts wire 2, leaves the wire-3 runner untouched.
func TestG563ResonanceTPIsSentOnWire3(t *testing.T) {
	at, _ := newDriveAT(t)
	lp := g563Register(t, at, "sig-m16-m19") // long 5: leg1 3, leg2 1 (line 14), leg3 1 (target 16)
	runner := mentorRunnerLeg(lp)
	if runner == nil || runner.Wire != 3 || !runner.Final {
		t.Fatalf("mentorRunnerLeg must return the wire-3 runner of a 3-pair plan, got %+v", runner)
	}
	var gotWire int
	var gotTP float64
	old := modifyBracketWire
	modifyBracketWire = func(_ *nttrader.TCPTrader, _ string, _ string, wireLeg int, newTP float64) error {
		gotWire, gotTP = wireLeg, newTP
		return nil
	}
	t.Cleanup(func() { modifyBracketWire = old })
	at.mentorResonanceRunnerTP(nil, lp, 18) // beyond the runner's resting TP 16
	if gotWire != 3 || gotTP != 18 {
		t.Fatalf("resonance runner TP must address wire 3: got leg %d TP %.2f", gotWire, gotTP)
	}
}

// M20 (broker stop read for wire 3 reads -sl2): the wire-3 stop lookup must
// read -sl3, never -sl2 (a leg-2 stop is the runner PARTIAL, not the runner).
func TestG563BrokerStopWire3ReadsSl3(t *testing.T) {
	book := ntwire.OrderSnapshotPayload{Orders: []ntwire.NT8Order{
		{Name: "sig-sl2", StopPrice: 100},
		{Name: "sig-sl3", StopPrice: 105},
	}}
	got := mentorBrokerStopOrders(book, "sig", 3)
	if len(got) != 1 || got[0].Name != "sig-sl3" || got[0].StopPrice != 105 {
		t.Fatalf("wire-3 broker stop lookup = %+v, want exactly sig-sl3 @105", got)
	}
}
