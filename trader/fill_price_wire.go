package trader

import (
	"vl/internal/datatruth/checks"
	"vl/internal/ownerpush"
	"vl/kernel"
)

// RegisterFillPrice wires the m447 (7) fill-price guard: the CME session key the
// "2 far fills in a session" count rolls on, the owner-push sender for the one
// alert per far fill, and the datatruth check. The integrator calls it once at
// boot (with RegisterBrokerAgreement, or through RegisterBrokerTruthChecks).
func RegisterFillPrice() {
	checks.DefaultFills.SetSession(kernel.CMESessionDayKey)
	checks.DefaultFills.SetAlert(func(kind, text string) { ownerpush.Push(kind, text) })
	checks.RegisterFillPrice(checks.DefaultFills)
}

// RegisterBrokerTruthChecks registers both m447 checks (broker_agreement and
// fill_price). Exits, stops and protection never consult either.
func RegisterBrokerTruthChecks() {
	RegisterBrokerAgreement()
	RegisterFillPrice()
}
