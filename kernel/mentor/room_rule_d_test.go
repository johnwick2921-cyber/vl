package mentor

import (
	"strings"
	"testing"

	"vl/market"
)

// ROOM-RULE-D (release #14): the room rule reads the leg-1 take-profit distance
// — Leg1RiskMultiple(confluence) — so the room to the first available level is
// ≥ 2R normally (leg 1 = 1R, "bán bớt ở 1:1" [D2.2 p3 @12:13]) and ≥ 4R for a
// confluence entry (leg 1 = 2R, mode C [D3.4 p3 @07:52–08:07]). Non-confluence
// behaviour is byte-identical to the pre-D code; the ONLY change is confluence
// (2R → 4R).

// TestRoomRefusalNonConfluenceIs2R — the unchanged 2R pins, exact arithmetic.
func TestRoomRefusalNonConfluenceIs2R(t *testing.T) {
	if refuse, _ := roomRefusal(100, 90, 119, false, 2); !refuse {
		t.Fatal("stop 10 / first level 19 (19 < 2R 20) must refuse")
	}
	if refuse, _ := roomRefusal(100, 90, 120, false, 2); refuse {
		t.Fatal("stop 10 / first level 20 (20 = 2R) must admit")
	}
}

// TestRoomRefusalConfluenceIs4R — the NEW gate: a confluence entry's leg 1 is
// 2R, so its room is 4R.
func TestRoomRefusalConfluenceIs4R(t *testing.T) {
	if refuse, _ := roomRefusal(100, 90, 130, true, 2); !refuse {
		t.Fatal("confluence stop 10 / first level 30 (30 < 4R 40) must refuse")
	}
	if refuse, _ := roomRefusal(100, 90, 140, true, 2); refuse {
		t.Fatal("confluence stop 10 / first level 40 (40 = 4R) must admit")
	}
}

// TestRoomRuleReadsTheLeg1Definition — the ONE definition proof: the room
// check's confluence threshold and the exit drive's C leg-1 TP both read
// Leg1RiskMultiple. Change Leg1RiskMultiple(true) to 3 and BOTH move together.
func TestRoomRuleReadsTheLeg1Definition(t *testing.T) {
	if Leg1RiskMultiple(false) != 1 || Leg1RiskMultiple(true) != 2 {
		t.Fatalf("Leg1RiskMultiple = (%v, %v), want (1, 2)", Leg1RiskMultiple(false), Leg1RiskMultiple(true))
	}
	// The confluence refusal names the leg-1 distance it measured against.
	if _, why := roomRefusal(100, 90, 130, true, 2); !strings.Contains(why, "leg-1 2R") {
		t.Fatalf("confluence room refusal must name the 2R leg-1 distance: %q", why)
	}
}

// TestPHLRoomRefusalCountsRoomKey — the B-rules ledger names a PHL room drop
// "room" (the shared room counter the ISB / reverse-ISB / box paths use), NOT
// "phl_refused". The room-rule-d reason-text fold ("room rule" → "room: …")
// broke the phlRefusalKey prefix, so every PHL room refusal fell through to the
// default "phl_refused" (DS-104 flag 1). MUTANT: drop/rename the room case in
// phlRefusalKey → this test goes RED.
func TestPHLRoomRefusalCountsRoomKey(t *testing.T) {
	cfg := workedCfg()
	levels := []Level{
		{Key: "key_level:29410", Kind: KindKeyLevel, Price: 29_410}, // reward 14.25 < 2×8.25
	}
	_, ok, reason := PHLPLHR2Levels(workedTouch(), Level{Kind: KindOldExtreme, Price: 29_431.75}, 0, 3, 29_380, levels, cfg)
	if ok {
		t.Fatal("room to the obstacle must refuse; got ok")
	}
	if !strings.HasPrefix(reason, "room") {
		t.Fatalf("the PHL room refusal must keep the new room reason text; got %q", reason)
	}
	// The evaluator's call site: e.refuse(phlRefusalKey(reason)).
	e := New(cfg)
	e.refuse(phlRefusalKey(reason))
	if e.State.Refusals["room"] != 1 {
		t.Fatalf("PHL room refusal must count under the shared \"room\" key; refusals=%v", e.State.Refusals)
	}
	if e.State.Refusals["phl_refused"] != 0 {
		t.Fatalf("PHL room refusal must NOT count under phl_refused; refusals=%v", e.State.Refusals)
	}
}

// TestBoxConfluenceRoomIs4RAtTheCallSite — the box path is the natural
// confluence carrier (box + trigger agree). stop 10, first level 30 (2R < 30 <
// 4R) → REFUSED (named RED: the old 2R check ADMITTED it); first level 40 →
// ADMITTED.
func TestBoxConfluenceRoomIs4RAtTheCallSite(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.RoomMultiple = 2
	b := Box{Kind: FTGL, Top: 100, Bottom: 90}
	ref := market.Kline{High: 101, Low: 91, Close: 102} // reject from above → LONG
	trig := TriggerLine{Dir: SideLong, Price: 95}       // buy trigger agrees → confluence

	// first level 131 → room 30 < 4R 40 → refused.
	if out := boxEntryIntent(ref, b, []Box{b}, []Level{{Kind: KindKeyLevel, Price: 131}}, trig, nil, cfg); len(out) != 0 {
		t.Fatalf("confluence box room 30 (2R < 30 < 4R) must refuse; got %+v", out)
	}
	// first level 141 → room 40 = 4R → admitted, Confluence stamped.
	out := boxEntryIntent(ref, b, []Box{b}, []Level{{Kind: KindKeyLevel, Price: 141}}, trig, nil, cfg)
	if len(out) != 1 || !out[0].Confluence {
		t.Fatalf("confluence box room 40 = 4R must emit with Confluence; got %+v", out)
	}
}
