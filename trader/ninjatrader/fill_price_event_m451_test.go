package ninjatrader

import (
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
)

// release #55 m451 - fill_price pins at the PRODUCTION fill handlers
// (handleFillInbound, recordCloseOrdered). Mutants (shown RED, then restored):
//   E1  apply() skips the event de-duplication      -> legs/partials pins RED
//   E2  noteFill/close_sync stop passing the event   -> legs pin RED
//   E3  noteFill drops price <= 0 again              -> malformed pin RED

func (f *fillGuardFx) atNano(offsetMs int64) string {
	return time.UnixMilli(f.m + 20_000 + offsetMs).UTC().Format(time.RFC3339Nano)
}

func (f *fillGuardFx) closeLeg(t *testing.T, sig, acct string, seq uint64, offsetMs int64, px float64) {
	t.Helper()
	f.tr.recordCloseOrdered("t1", "ex", "ninjatrader", f.st, ntwire.PositionClosePayload{
		SignalID: sig, Symbol: "MNQ", PositionSide: "long", Account: acct, Quantity: 1,
		ExitPrice: px, ExitReason: "sl", ExitTime: f.atNano(offsetMs), Seq: seq,
	})
}

// One far print arrives as several position_close frames (one per bracket leg,
// exit_time stamped a few ms apart, same signal and seq). It is ONE strike: the
// first far print never pauses on its own.
func TestM451SplitBracketLegsOfOneFarPrintAreOneStrike(t *testing.T) {
	f := newFillGuardFx(t)
	f.openRow(t, "sig-l1")
	f.closeLeg(t, "sig-l1", "Sim101", 7, 0, 29100)
	f.closeLeg(t, "sig-l1", "Sim101", 7, 3, 29100) // leg 2, 3 ms later
	f.closeLeg(t, "sig-l1", "Sim101", 7, 6, 29100) // leg 3
	s := f.g.Stats()
	if s.Far != 1 || s.FarRepeat != 2 || len(*f.alerts) != 1 {
		t.Fatalf("three frames of one print = one strike and one alert: %+v alerts=%d", s, len(*f.alerts))
	}
	if b, why, _ := f.g.Check()("MNQ", time.Now()); b {
		t.Fatalf("one far print must not pause entries: %q", why)
	}
	// a DIFFERENT exit (another seq) that is also far is the second strike
	f.closeLeg(t, "sig-l2", "Sim101", 9, 0, 28900)
	if b, why, _ := f.g.Check()("MNQ", time.Now()); !b || !strings.Contains(why, "2 far fill(s)") {
		t.Fatalf("two far EVENTS pause: blocked=%v %q", b, why)
	}
}

// The same print recorded on two mirrored accounts is one strike on each - and a
// strike on Sim101 is no strike on SimAccount12.
func TestM451MirroredAccountsKeepSeparateStrikes(t *testing.T) {
	f := newFillGuardFx(t)
	f.closeLeg(t, "sig-m1", "Sim101", 7, 0, 29100)
	f.closeLeg(t, "sig-m1", "SimAccount12", 7, 1, 29100) // the same print on the mirrored account
	if b, why, _ := f.g.Check()("MNQ", time.Now()); b {
		t.Fatalf("one far print on each of two accounts is a strike on each, a pause for neither: %q", why)
	}
	f.closeLeg(t, "sig-m2", "Sim101", 8, 0, 28900) // Sim101's second event
	b, why, scopes, _ := f.g.CheckScoped()("MNQ", time.Now())
	if !b || len(scopes) != 1 || scopes[0] != "acct:SIM101" || !strings.Contains(why, "account SIM101: 2 far fill(s)") {
		t.Fatalf("only Sim101 has two strikes: blocked=%v scopes=%v %q", b, scopes, why)
	}
}

// An entry sends a fill frame per partial; the cumulative average price and
// quantity differ between them, the signal does not.
func TestM451PartialEntryFillsOfOneFarPrintAreOneStrike(t *testing.T) {
	f := newFillGuardFx(t)
	fill := func(status string, qty int, px float64) ntwire.FillPayload {
		return ntwire.FillPayload{SignalID: "sig-pe", Symbol: "MNQ", Account: "Sim101", Status: status,
			FillPrice: px, FillTime: f.at(), Side: "buy", Quantity: qty}
	}
	f.tr.handleFillInbound(fill("partial", 1, 29500))
	f.tr.handleFillInbound(fill("filled", 2, 29525)) // cumulative: the avg carries the bad print
	if s := f.g.Stats(); s.Far != 1 || s.FarRepeat != 1 || len(*f.alerts) != 1 {
		t.Fatalf("two frames of one entry = one strike: %+v alerts=%d", s, len(*f.alerts))
	}
	if b, _, _ := f.g.Check()("MNQ", time.Now()); b {
		t.Fatal("one far entry must not pause")
	}
}

// A fill price <= 0 is a wrong recorded price the check exists to catch: it is
// counted as a far fill and logged, not silently dropped before the guard.
func TestM451ZeroOrNegativeFillPriceIsCountedNotDropped(t *testing.T) {
	f := newFillGuardFx(t)
	f.tr.handleFillInbound(ntwire.FillPayload{SignalID: "sig-z1", Symbol: "MNQ", Account: "Sim101", Status: "filled",
		FillPrice: 0, FillTime: f.at(), Side: "buy", Quantity: 1})
	f.closeLeg(t, "sig-z2", "Sim101", 3, 0, -5)
	s := f.g.Stats()
	if s.Malformed != 2 || s.Far != 2 || len(*f.alerts) != 2 {
		t.Fatalf("both malformed fills must be counted and alerted: %+v alerts=%d", s, len(*f.alerts))
	}
	if b, why, _ := f.g.Check()("MNQ", time.Now()); !b || !strings.Contains(why, "is not a price") {
		t.Fatalf("two unjudgeable prices pause (cannot judge = blocked): blocked=%v %q", b, why)
	}
}
