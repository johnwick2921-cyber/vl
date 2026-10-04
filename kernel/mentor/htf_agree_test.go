package mentor

import "testing"

// HTFAgree gates the 20-contract tier ("size 20 only when 4h AND 1h agree and
// room ≥ 30 pts"). It was never set, so the tier was unreachable. The
// evaluator now stamps it on every ENTRY as it leaves Tick, from the 4h/1h
// trigger state: both lines stand and point the entry's side — case 1 of the
// mentor's screen [D4.4 p1 @16:00]. A silent 1h (case 2: follow the 4h) still
// trades but is NOT agreement.

func htfAgreeISB(t *testing.T, h HTF) Intent {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := newISBEval(cfg)
	e.State.HTF = h
	bars := isbFixture()
	for _, in := range e.Tick(bars, bars[len(bars)-1].CloseTime+1) {
		if in.Action == PlaceStopLimitEntry {
			return in
		}
	}
	t.Fatalf("the long ISB did not emit with HTF %+v: %v", h, e.State.Refusals)
	return Intent{}
}

func TestHTFAgreeStampedFromTheFourHAndOneHTriggers(t *testing.T) {
	long := TriggerLine{Dir: SideLong, Price: 90}
	if in := htfAgreeISB(t, HTF{FourH: long, OneH: long}); !in.HTFAgree {
		t.Fatalf("4h LONG + 1h LONG, entry LONG: HTFAgree must be stamped true: %+v", in)
	}
	// case 2: 1h silent — follow the 4h, the entry trades, but it is not agreement.
	if in := htfAgreeISB(t, HTF{FourH: long}); in.HTFAgree {
		t.Fatalf("4h LONG + 1h silent: the entry trades but HTFAgree must be false: %+v", in)
	}
}

func TestHTFAgreesTable(t *testing.T) {
	long := TriggerLine{Dir: SideLong, Price: 1}
	short := TriggerLine{Dir: SideShort, Price: 1}
	cases := []struct {
		name string
		h    HTF
		side Side
		want bool
	}{
		{"both long, long entry", HTF{FourH: long, OneH: long}, SideLong, true},
		{"both short, short entry", HTF{FourH: short, OneH: short}, SideShort, true},
		{"both long, short entry", HTF{FourH: long, OneH: long}, SideShort, false},
		{"1h silent", HTF{FourH: long}, SideLong, false},
		{"4h silent", HTF{OneH: long}, SideLong, false},
		{"opposite", HTF{FourH: long, OneH: short}, SideLong, false},
		{"no side", HTF{FourH: long, OneH: long}, "", false},
	}
	for _, c := range cases {
		if got := HTFAgrees(c.h, c.side); got != c.want {
			t.Fatalf("%s: HTFAgrees = %v, want %v", c.name, got, c.want)
		}
	}
}

// Only entries carry the flag; cancels and other actions pass untouched.
func TestStampHTFAgreeEntriesOnly(t *testing.T) {
	long := TriggerLine{Dir: SideLong, Price: 1}
	out := []Intent{
		{Action: PlaceStopEntry, Side: SideLong, Setup: "PHL"},
		{Action: PlaceStopLimitEntry, Side: SideLong, Setup: "ISB"},
		{Action: CancelArm, Side: SideLong},
	}
	stampHTFAgree(out, HTF{FourH: long, OneH: long})
	if !out[0].HTFAgree || !out[1].HTFAgree {
		t.Fatalf("entries not stamped: %+v", out)
	}
	if out[2].HTFAgree {
		t.Fatalf("a cancel must not carry HTFAgree: %+v", out[2])
	}
}
