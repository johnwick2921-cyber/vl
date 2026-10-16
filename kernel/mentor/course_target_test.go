package mentor

import "testing"

// Release #30 (D3.3 p1 @05:15–07:38): the EMA 9 / EMA 34 lines are PARTIAL
// points along the way, not the trade target — "Ta ghẹt tôi đầu tiên phải là
// về gần đỉnh cũ trước đó nè … Tiếp tục là email 34 nè … những cái chỗ này là
// những cái chỗ anh chị bán bớt". So an EMA must not by itself kill the trade
// (the 1:1 floor / room rule read the NEXT non-EMA level). These pins anchor
// nextLevelBeyondRoom's EMA skip + fallback.

func TestNextLevelBeyondRoomSkipsEMAAsTarget(t *testing.T) {
	levels := []Level{
		{Key: "ema9", Kind: KindEMA9, Price: 102},                              // nearest — a partial point
		{Key: "ema34", Kind: KindEMA34, Price: 106},                            // a partial point
		{Key: "old-high:120", Kind: KindOldExtreme, Role: OldHigh, Price: 120}, // the real target
	}
	if got := nextLevelBeyondRoom(levels, 100, 98, SideLong, 2); got != 120 {
		t.Fatalf("an EMA must not be the target when an old high stands beyond: got %.2f, want 120", got)
	}
	shortLevels := []Level{
		{Key: "ema9", Kind: KindEMA9, Price: 98},
		{Key: "ema34", Kind: KindEMA34, Price: 94},
		{Key: "old-low:80", Kind: KindOldExtreme, Role: OldLow, Price: 80},
	}
	if got := nextLevelBeyondRoom(shortLevels, 100, 102, SideShort, 2); got != 80 {
		t.Fatalf("short: an EMA must not be the target when an old low stands beyond: got %.2f, want 80", got)
	}
}

func TestNextLevelBeyondRoomEMAFallbackWhenNothingElse(t *testing.T) {
	onlyEMA := []Level{{Key: "ema34", Kind: KindEMA34, Price: 106}}
	if got := nextLevelBeyondRoom(onlyEMA, 100, 98, SideLong, 2); got != 106 {
		t.Fatalf("no other level → the EMA is the fallback target: got %.2f, want 106", got)
	}
	onlyEMA9 := []Level{{Key: "ema9", Kind: KindEMA9, Price: 96}}
	if got := nextLevelBeyondRoom(onlyEMA9, 100, 102, SideShort, 2); got != 96 {
		t.Fatalf("short, no other level → the EMA is the fallback target: got %.2f, want 96", got)
	}
}

func TestIsEMAPartialKind(t *testing.T) {
	for _, k := range []LevelKind{KindEMA9, KindEMA34, KindEMA34HTF} {
		if !isEMAPartialKind(k) {
			t.Fatalf("isEMAPartialKind(%q) = false, want true", k)
		}
	}
	for _, k := range []LevelKind{KindOldExtreme, KindKeyLevel, KindTrendlineTarget, KindHTFTrigger, KindTriggerRetest} {
		if isEMAPartialKind(k) {
			t.Fatalf("isEMAPartialKind(%q) = true, want false", k)
		}
	}
}

// TestISBEMALineDoesNotKillTheTrade — the production ISB call site: entry 104,
// stop 99 (risk 5, 2R = 10). An EMA 9 at 106 (2 pts away, below the 1:1 floor)
// is a PARTIAL point, not the target — the ISB must emit with the old high 130
// as its target, not be refused isb_target_below_floor / room. Pre-#30 the EMA
// became the target and the ISB was refused. Mutant: drop the EMA skip in
// nextLevelBeyondRoom → RED (isb_target_below_floor).
func TestISBEMALineDoesNotKillTheTrade(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	cfg.ISBReverseEMA9Enabled = false
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	e, bars, now := rel5ISBEval(t, cfg,
		[]Level{
			{Key: "ema9", Kind: KindEMA9, Price: 106},
			{Key: "old-high:130", Kind: KindOldExtreme, Role: OldHigh, Price: 130},
		},
		106,
	)

	ins := e.Tick(bars, now)
	var isb *Intent
	for i := range ins {
		if (ins[i].Action == PlaceStopEntry || ins[i].Action == PlaceStopLimitEntry) && ins[i].Setup == "ISB" {
			isb = &ins[i]
			break
		}
	}
	if isb == nil {
		t.Fatalf("an EMA line must not kill the trade — the ISB must emit; intents %+v refusals %v", ins, e.State.Refusals)
	}
	if isb.Target != 130 {
		t.Fatalf("ISB target = %.2f, want 130 (the old high, never the EMA 9); refusals=%v", isb.Target, e.State.Refusals)
	}
	if e.State.Refusals["isb_target_below_floor"] != 0 || e.State.Refusals["room"] != 0 {
		t.Fatalf("the EMA line must not trigger the floor/room refusal; refusals=%v", e.State.Refusals)
	}
}
