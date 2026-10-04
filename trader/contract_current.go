package trader

import (
	"time"
	"vl/kernel"

	ntwire "vl/provider/ninjatrader"
)

// currentContract is the trader-side face of the ONE source: the AddOn's most
// recent `subscribed` ACK, via the TCP server; else the newest usable bar in
// the store (labelled a fallback); else "". Never a date rule (A24).
//
// It returns the source beside the value so every line that prints the
// contract can say where it came from (A11).
func (at *AutoTrader) currentContract(symbol string) (contract, source string) {
	if nt := at.armedTrader(); nt != nil {
		if srv := nt.GetServer(); srv != nil {
			if f, ok := srv.CurrentContract(symbol); ok {
				return f.Contract, "subscribed@" + kernel.ClockCTSeconds(f.ReceivedAt)
			}
		}
	}
	if at.store != nil && at.store.BarHistory() != nil {
		if c, ok := at.store.BarHistory().LatestContract(symbol); ok {
			return c, "store-fallback"
		}
	}
	return "", "none"
}

// contractFactFor is currentContract with the full provenance (roll history),
// for the boot line and the desk strip. ok=false → print n/a, never a literal.
func (at *AutoTrader) contractFactFor(symbol string) (ntwire.ContractFact, bool) {
	if nt := at.armedTrader(); nt != nil {
		if srv := nt.GetServer(); srv != nil {
			if f, ok := srv.CurrentContract(symbol); ok {
				return f, true
			}
		}
	}
	if at.store != nil && at.store.BarHistory() != nil {
		if c, ok := at.store.BarHistory().LatestContract(symbol); ok {
			return ntwire.ContractFact{Contract: c, Source: "store-fallback", ReceivedAt: time.Time{}}, true
		}
	}
	return ntwire.ContractFact{}, false
}
