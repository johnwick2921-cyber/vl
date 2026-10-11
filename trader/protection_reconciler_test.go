package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	nt "vl/provider/ninjatrader"
)

func px(v float64) *float64 { return &v }

// E5 — the reconciler's whole contract, one table.
//
// The 09-06 position ran 8h19m with no stop and nothing in this process was
// looking. Every mechanism watched orders it believed in; none asked "is there
// a position, and does the broker hold a stop for it".
func TestProtectionAdjudication(t *testing.T) {
	const sym, side = "MNQ", "LONG"
	live := nt.NT8Order{Symbol: sym, Name: "sig-sl", Action: "sell", Type: "stop",
		State: "Accepted", StopPrice: 29554, Quantity: 1}

	cases := []struct {
		name       string
		book       []nt.NT8Order
		haveBook   bool
		posQty     int
		accepted   *float64
		planStop   float64
		wantAction protectionAction
		wantSource string
		wantPx     float64
	}{
		{"protected by an Accepted stop", []nt.NT8Order{live}, true, 1, px(29554), 29500, protectionOK, "", 0},
		{"flat needs nothing", nil, true, 0, nil, 0, protectionOK, "", 0},

		{"no book is not permission", nil, false, 1, px(29554), 29500, protectionUnknown, "", 0},
		{"an unreadable protective state is not absence",
			[]nt.NT8Order{{Symbol: sym, Name: "sig-sl", Action: "sell", Type: "stop",
				State: "SomethingNT8NeverSends", Quantity: 1}},
			true, 1, px(29554), 29500, protectionUnknown, "", 0},
		{"a live stop with no quantity leaves coverage unknown",
			[]nt.NT8Order{{Symbol: sym, Name: "sig-sl", Action: "sell", Type: "stop",
				State: "Working", Quantity: 0}},
			true, 1, px(29554), 29500, protectionUnknown, "", 0},

		// THE ACTED-ON CASE
		{"nothing protective → place the ACCEPTED stop",
			[]nt.NT8Order{{Symbol: sym, Name: "sig", Action: "buy", Type: "limit", State: "Working", Quantity: 1}},
			true, 1, px(29554), 29500, protectionPlace, "accepted_risk", 29554},
		{"nothing protective and no accepted record → the plan's stop",
			nil, true, 1, nil, 29500, protectionPlace, "plan", 29500},
		{"nothing protective and no price at all → raised, never invented",
			nil, true, 1, nil, 0, protectionAlertOnly, "", 0},

		// A DYING OR LOCAL STOP IS NOT PROTECTION
		{"a CancelPending stop is not protection",
			[]nt.NT8Order{{Symbol: sym, Name: "sig-sl", Action: "sell", Type: "stop",
				State: "CancelPending", Quantity: 1}},
			true, 1, px(29554), 0, protectionPlace, "accepted_risk", 29554},
		{"a TriggerPending stop is held on this PC, not at the exchange",
			[]nt.NT8Order{{Symbol: sym, Name: "sig-sl", Action: "sell", Type: "stop",
				State: "TriggerPending", Quantity: 1}},
			true, 1, px(29554), 0, protectionPlace, "accepted_risk", 29554},

		// PARTIAL COVER IS REPORTED, NOT PATCHED
		{"partially covered is raised, not doubled",
			[]nt.NT8Order{live}, true, 2, px(29554), 29500, protectionAlertOnly, "", 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := adjudicateProtection(sym, side, c.posQty, c.book, c.haveBook, c.accepted, c.planStop, nil, nil)
			if v.Action != c.wantAction {
				t.Fatalf("action = %s, want %s (why: %s)", v.Action, c.wantAction, v.Why)
			}
			if c.wantSource != "" && v.Source != c.wantSource {
				t.Fatalf("source = %q, want %q", v.Source, c.wantSource)
			}
			if c.wantPx != 0 && v.StopPx != c.wantPx {
				t.Fatalf("stop = %.2f, want %.2f", v.StopPx, c.wantPx)
			}
			if v.Why == "" {
				t.Fatal("every verdict must say why — this one is silent")
			}
		})
	}
}

// REL17-F4 — a registered mentor live position places at the driver's CURRENT
// stop (the moved BE/1:1/trail stop), never the original, and a stop already
// through the last price flattens instead of placing.
func TestProtectionUsesMentorDriverStop(t *testing.T) {
	const sym, side = "MNQ", "LONG"
	// A long entered at 31163: the driver has moved the BE stop to entry, while
	// the accepted-risk record and the plan still carry the ORIGINAL 31188 stop.
	original, entry := 31188.0, 31163.0

	// naked (no protective order), last price still above the BE stop → place at
	// the driver's CURRENT stop (entry), not the original.
	if v := adjudicateProtection(sym, side, 1, nil, true, px(original), original, px(entry), px(31180)); v.Action != protectionPlace || v.StopPx != entry || v.Source != "mentor_driver" {
		t.Fatalf("naked mentor after BE: got %s stop=%.2f source=%q — %s", v.Action, v.StopPx, v.Source, v.Why)
	}

	// last price through the BE stop → flatten, never place a stop behind the
	// market.
	if v := adjudicateProtection(sym, side, 1, nil, true, px(original), original, px(entry), px(31162)); v.Action != protectionFlatten {
		t.Fatalf("price through the BE stop: got %s, want flatten — %s", v.Action, v.Why)
	}

	// short: tightest = the LOWEST stop; last price below it flattens.
	if v := adjudicateProtection(sym, "SHORT", 1, nil, true, px(original), original, px(31100), px(31110)); v.Action != protectionFlatten {
		t.Fatalf("short through its stop: got %s, want flatten — %s", v.Action, v.Why)
	}

	// no mentor stop → today's behaviour (accepted-risk stop) unchanged.
	if v := adjudicateProtection(sym, side, 1, nil, true, px(original), original, nil, px(31180)); v.Action != protectionPlace || v.StopPx != original || v.Source != "accepted_risk" {
		t.Fatalf("non-mentor: got %s stop=%.2f source=%q — %s", v.Action, v.StopPx, v.Source, v.Why)
	}

	// no last price (no mark): do NOT flatten, still place at the driver's stop
	// (never fall back to the wider original), and the Why names that the side
	// could not be checked.
	if v := adjudicateProtection(sym, side, 1, nil, true, px(original), original, px(entry), nil); v.Action != protectionPlace || v.StopPx != entry || v.Source != "mentor_driver" {
		t.Fatalf("no fresh price: got %s stop=%.2f source=%q — %s", v.Action, v.StopPx, v.Source, v.Why)
	}
	if v := adjudicateProtection(sym, side, 1, nil, true, px(original), original, px(entry), nil); !strings.Contains(v.Why, "could NOT be checked") {
		t.Fatalf("no last price must say the side could not be checked: %s", v.Why)
	}
}

// REL17-F4 P0 — the through-check may only use a FRESH 1m close. No bars, or a
// stale bar, means NO fresh price and therefore NO flatten; a fresh bar through
// the stop flattens. (The false-flatten regression: a BE stop == entry, and the
// old markPrice fell back to entry — so a naked healthy LONG read "through".)
func TestProtectionFreshPriceNoFlattenOnStaleOrMissing(t *testing.T) {
	t.Setenv("STALE_BAR_GRACE_S", "15")
	const sym, side = "MNQ", "LONG"
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, kernel.CTLocation()) // Tuesday, CME open
	if !kernel.IsCMEOpen(now) {
		t.Fatalf("test premise: %v should be CME-open", now)
	}
	openMs := now.UnixMilli()           // the current 1m bar opens on this minute boundary
	original, entry := 31188.0, 31163.0 // the driver's BE stop == entry

	orig := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = orig })

	adjudicate := func() (protectionVerdict, bool) {
		fp, ok := protectionFreshPrice(sym, now)
		var fpp *float64
		if ok {
			fpp = &fp
		}
		return adjudicateProtection(sym, side, 1, nil, true, px(original), original, px(entry), fpp), ok
	}

	// (a) no bars + BE stop == entry → PLACE at entry, no flatten, no fresh price.
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline { return nil }
	v, ok := adjudicate()
	if ok || v.Action != protectionPlace || v.StopPx != entry || v.Source != "mentor_driver" {
		t.Fatalf("(a) no bars: got ok=%v %s stop=%.2f source=%q — %s", ok, v.Action, v.StopPx, v.Source, v.Why)
	}
	if !strings.Contains(v.Why, "could NOT be checked") {
		t.Fatalf("(a) must name that the stop side could not be checked: %s", v.Why)
	}

	// (b) a STALE bar whose close is through the stop → no fresh price → no flatten.
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		return []market.Kline{{OpenTime: openMs - 5*60_000, Close: entry - 10}} // 5 min old, through the BE stop
	}
	v, ok = adjudicate()
	if ok || v.Action != protectionPlace {
		t.Fatalf("(b) stale bar: got ok=%v %s, want place (no flatten) — %s", ok, v.Action, v.Why)
	}

	// (c) a FRESH bar whose close is through the stop → flatten.
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		return []market.Kline{{OpenTime: openMs, Close: entry - 10}}
	}
	v, ok = adjudicate()
	if !ok || v.Action != protectionFlatten {
		t.Fatalf("(c) fresh bar through stop: got ok=%v %s, want flatten — %s", ok, v.Action, v.Why)
	}
}

// mentorDriverCurrentStop picks the tightest live leg stop in the protective
// direction: the HIGHEST stop for a long, the LOWEST for a short.
func TestMentorDriverCurrentStopTightest(t *testing.T) {
	at := &AutoTrader{id: "t"}
	at.mentorLivePos = map[string]*mentorLivePos{
		"sig": {
			Pos: mentorPosition{Symbol: "MNQ", Side: "long", Contracts: 2},
			Legs: [2]mentorLeg{
				{SignalID: "sig", Qty: 1, Stop: 31163, Final: false, Wire: 1}, // BE at entry
				{SignalID: "sig", Qty: 1, Stop: 31155, Final: true, Wire: 2},  // a trail below
			},
		},
	}
	stop, ok := at.mentorDriverCurrentStop("MNQ", "LONG")
	if !ok || stop != 31163 {
		t.Fatalf("long tightest = 31163, got ok=%v stop=%.2f", ok, stop)
	}
	// short reads the same registry via a case-insensitive side match; the
	// tightest short stop is the LOWEST of the two.
	at.mentorLivePos["sig"].Pos.Side = "short"
	at.mentorLivePos["sig"].Legs[0].Stop = 31170
	at.mentorLivePos["sig"].Legs[1].Stop = 31165
	stop, ok = at.mentorDriverCurrentStop("MNQ", "SHORT")
	if !ok || stop != 31165 {
		t.Fatalf("short tightest = 31165, got ok=%v stop=%.2f", ok, stop)
	}
}

// A short position is protected by a BUY stop; the side logic must not be
// long-only. (The 592 incident was long, which is exactly how a long-only bug
// would have survived review.)
func TestShortPositionProtectionIsRecognised(t *testing.T) {
	book := []nt.NT8Order{{Symbol: "MNQ", Name: "whatever", Action: "BuyToCover", Type: "StopMarket",
		State: "Accepted", StopPrice: 29700, Quantity: 1}}
	if v := adjudicateProtection("MNQ", "SHORT", 1, book, true, nil, 0, nil, nil); v.Action != protectionOK {
		t.Fatalf("a buy stop above a SHORT is its protection; got %s — %s", v.Action, v.Why)
	}
	// the same order does NOT protect a long
	if v := adjudicateProtection("MNQ", "LONG", 1, book, true, nil, 29500, nil, nil); v.Action != protectionPlace {
		t.Fatalf("a BUY stop was counted as protection for a LONG; got %s", v.Action)
	}
}

// D7 — the boot line says what it KNOWS and n/a for what it cannot.
func TestBracketsBootLineReadsRatherThanAsserts(t *testing.T) {
	// At process start there is no book and no far-side build id.
	cold := BracketsBootLine(nil, false, "", 0)
	for _, want := range []string{"entry-oco=n/a", "bracket-oco=n/a", "state-source=none", "protective-tif=n/a"} {
		if !strings.Contains(cold, want) {
			t.Errorf("cold boot line is missing %q — a field this process cannot know must print n/a, "+
				"never a constant dressed as a measurement.\n  got: %s", want, cold)
		}
	}
	if strings.Contains(cold, "can-place-stop=yes") {
		t.Errorf("an unknown AddOn build was reported as capable:\n  %s", cold)
	}

	// With a book carrying the shape this wave creates.
	book := []nt.NT8Order{
		{Name: "aa07e583", OCO: "", State: "Working", TimeInForce: "Day"},
		{Name: "aa07e583-sl", OCO: "aa07e583-exit", State: "Accepted", TimeInForce: "Gtc"},
		{Name: "aa07e583-tp", OCO: "aa07e583-exit", State: "Working", TimeInForce: "Gtc"},
	}
	warm := BracketsBootLine(book, true, "2026-09-07-h1", 3)
	for _, want := range []string{
		"entry-oco=own(none)", "bracket-oco=on-fill(shared)", "state-source=broker",
		"protective-tif=Gtc", "can-place-stop=yes", "unprotected-found=3",
	} {
		if !strings.Contains(warm, want) {
			t.Errorf("warm boot line is missing %q\n  got: %s", want, warm)
		}
	}

	// THE REGRESSION THIS LINE EXISTS TO SHOW: an entry back in the bracket's
	// group must be visible on sight, not inferred from an incident report.
	bad := []nt.NT8Order{
		{Name: "aa07e583", OCO: "aa07e583-exit", State: "Working"},
		{Name: "aa07e583-sl", OCO: "aa07e583-exit", State: "Accepted", TimeInForce: "Gtc"},
	}
	if got := BracketsBootLine(bad, true, "2026-09-07-h1", 0); !strings.Contains(got, "entry-oco=SHARED(") {
		t.Errorf("an entry sharing its bracket's OCO group did not show as SHARED:\n  %s", got)
	}
}
