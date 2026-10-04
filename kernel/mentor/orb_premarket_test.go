package mentor

import "testing"

// ORB pre-market fix (release #3b, after I1): the ORB gate used to drop EVERY
// intraday entry until 08:32 CT, because the ORB resets at CT midnight and the
// gate only read `orb.Drawn`. The course says "No ORB for pre-market" [X5
// @05:42], and the owner's window is any hour — so before the 08:30 CT RTH open
// (Globex/overnight) the ORB must not block.
//
// These pins exercise orbGateFilter — the production gate call site
// (eval.go:1061, Tick).

func orbEntry(side Side, price float64) Intent {
	return Intent{Action: PlaceStopEntry, Side: side, Price: price, Reason: "ISB"}
}

// TestORBNotGatedBeforeRTHOpen — pin 1: at 03:00 CT (pre-market) an intraday
// entry is NOT ORB-blocked, even though the ORB is not drawn yet.
func TestORBNotGatedBeforeRTHOpen(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	preMarket := auditMs(2026, 9, 15, 3, 0, 0) // 03:00 CT
	got, refused := orbGateFilter([]Intent{orbEntry(SideLong, 25000)},
		ORB{Day: dayStartCT(preMarket), Drawn: false}, preMarket, cfg)
	if len(refused) != 0 {
		t.Fatalf("pre-market entries must not be ORB-gated; refused = %v", refused)
	}
	if len(got) != 1 {
		t.Fatalf("pre-market entry must pass through; got %d intents", len(got))
	}
}

// TestORBBlocksAfterRTHOpenUntilEscape — pin 2: from the 08:30 CT RTH open the
// gate blocks until the ORB is drawn and escaped (unchanged behaviour).
func TestORBBlocksAfterRTHOpenUntilEscape(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true

	// 08:31 CT — the 08:30–08:32 two-minute ORB candle has not completed.
	atRTHOpen := auditMs(2026, 9, 15, 8, 31, 0)
	_, refused := orbGateFilter([]Intent{orbEntry(SideLong, 25000)},
		ORB{Day: dayStartCT(atRTHOpen), Drawn: false}, atRTHOpen, cfg)
	if len(refused) != 1 || refused[0] != "orb_not_drawn" {
		t.Fatalf("08:31 entry must be ORB-blocked (not drawn); refused = %v", refused)
	}

	// Drawn + escaped long at 09:00 → a long outside the range is allowed.
	escaped := ORB{Day: dayStartCT(atRTHOpen), High: 24844, Low: 24814, Drawn: true, Escaped: SideLong}
	postOpen := auditMs(2026, 9, 15, 9, 0, 0)
	got, refused := orbGateFilter([]Intent{orbEntry(SideLong, 24850)}, escaped, postOpen, cfg)
	if len(refused) != 0 || len(got) != 1 {
		t.Fatalf("escaped long outside the ORB must pass; got %d, refused %v", len(got), refused)
	}
}

// TestORBNotGatedAfterRTHClose — pin 3: after the 15:00 CT RTH close (the
// evening Globex session) the ORB gate does not filter, whatever the morning
// escape side latched [EXTRAS P2].
func TestORBNotGatedAfterRTHClose(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	// The morning ORB escaped LONG; an evening SHORT would be orb_wrong_side if
	// the gate still applied after the RTH close.
	orb := ORB{Day: dayStartCT(auditMs(2026, 9, 15, 9, 0, 0)), High: 24844, Low: 24814, Drawn: true, Escaped: SideLong}
	for _, hhmm := range []struct{ hh, mm int }{{15, 30}, {19, 0}} {
		at := auditMs(2026, 9, 15, hhmm.hh, hhmm.mm, 0)
		got, refused := orbGateFilter([]Intent{orbEntry(SideShort, 24810)}, orb, at, cfg)
		if len(refused) != 0 || len(got) != 1 {
			t.Fatalf("%02d:%02d CT entry must not be ORB-gated (morning escape is long); got %d, refused %v",
				hhmm.hh, hhmm.mm, len(got), refused)
		}
	}
}
