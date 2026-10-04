package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// limitsK builds a 1m candle.
func limitsK(o, h, l, c float64, minute int) market.Kline {
	ct := time.Date(2026, 10, 2, 9, minute, 0, 0, time.UTC).UnixMilli()
	return market.Kline{Open: o, High: h, Low: l, Close: c, OpenTime: ct, CloseTime: ct + 60_000}
}

// limitsNow is a CT-morning timestamp matching the candle series above.
func limitsNow(minute int) int64 {
	return time.Date(2026, 10, 2, 9, minute, 0, 0, time.UTC).UnixMilli()
}

var limitsLvls = []Level{{Key: "old_extreme:100", Kind: KindOldExtreme, Price: 100}}

// limitsLvlsG2 adds a KEY LEVEL at 97 (distinct from the old extreme 100) so
// the G2 tests prove the loss box keys on the setup's PLACE, not the extreme.
var limitsLvlsG2 = []Level{
	{Key: "key_level:97", Kind: KindKeyLevel, Price: 97},
	{Key: "old_extreme:100", Kind: KindOldExtreme, Price: 100},
}

func limitsCfg() Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	return cfg
}

// limitsCfgNoLeg is G2-only: the G1 leg budget is off.
func limitsCfgNoLeg() Config {
	cfg := limitsCfg()
	cfg.LegBudgetEnabled = false
	return cfg
}

// limitsPHL is a long PHL entry anchored at the old extreme 100.
func limitsPHL(entry, stop, target float64, expiry int64) Intent {
	return Intent{
		Action: PlaceStopEntry, Side: SideLong,
		Price: entry, Stop: stop, Target: target, ExpiryMs: expiry,
	}
}

// limitsPHLAt is a long PHL entry at a named place (the touch level).
func limitsPHLAt(entry, stop, target float64, expiry int64, place string, anchor float64) Intent {
	return Intent{
		Action: PlaceStopEntry, Side: SideLong,
		Price: entry, Stop: stop, Target: target, ExpiryMs: expiry,
		Anchor: anchor, AnchorKey: place,
	}
}

// limitsISB is a long ISB entry.
func limitsISB(entry, stop, target float64, expiry int64) Intent {
	return Intent{
		Action: PlaceStopLimitEntry, Side: SideLong,
		Price: entry, Stop: stop, Target: target, ExpiryMs: expiry,
	}
}

func applyAt(l *Limits, in []Intent, prev, cur market.Kline, minute int) []Intent {
	return l.Apply(in, prev, cur, limitsNow(minute), limitsLvls, limitsCfg())
}

func applyAtCfg(l *Limits, in []Intent, prev, cur market.Kline, minute int, cfg Config) []Intent {
	return l.Apply(in, prev, cur, limitsNow(minute), limitsLvls, cfg)
}

func applyAtG2(l *Limits, in []Intent, prev, cur market.Kline, minute int, cfg Config) []Intent {
	return l.Apply(in, prev, cur, limitsNow(minute), limitsLvlsG2, cfg)
}

func applyLevels(l *Limits, in []Intent, prev, cur market.Kline, minute int, cfg Config, levels []Level) []Intent {
	return l.Apply(in, prev, cur, limitsNow(minute), levels, cfg)
}

// G1 (a): the third entry in one leg is refused — the PHL fills, one
// same-direction ISB fills, and the next ISB hits the budget.
// MUTANT: change the ISB budget check to `leg.Entries >= 3` → this test
// goes RED (the third entry is emitted).
func TestLimitsThirdEntryInLegRefused(t *testing.T) {
	var l Limits
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	out1 := applyAt(&l, []Intent{limitsPHL(90, 88, 99, exp)}, prev, limitsK(93, 94, 92, 93, 1), 1)
	if len(out1) != 1 {
		t.Fatalf("PHL placement: want 1 entry, got %d", len(out1))
	}
	out2 := applyAt(&l, []Intent{limitsISB(92, 89, 98, exp)}, limitsK(93, 94, 92, 93, 1), limitsK(89, 91, 88.5, 89, 2), 2)
	if len(out2) != 1 {
		t.Fatalf("first ISB: want 1 entry, got %d", len(out2))
	}
	if l.Long == nil || l.Long.Entries != 1 {
		t.Fatalf("leg after PHL fill: want entries=1, got %+v", l.Long)
	}
	out3 := applyAt(&l, []Intent{limitsISB(93, 90, 99, exp)}, limitsK(89, 91, 88.5, 89, 2), limitsK(91, 93, 90, 92, 3), 3)
	if len(out3) != 0 {
		t.Fatalf("second ISB: want 0 entries (budget full), got %d", len(out3))
	}
	if l.Long == nil || l.Long.Entries != 2 {
		t.Fatalf("leg after ISB fill: want entries=2, got %+v", l.Long)
	}
}

// G1 (b): a stop-out inside the leg closes it — no more entries until a
// break beyond the extreme.
// MUTANT: delete the `leg.Stopped = true` in loss() → this test goes RED
// (the post-stopout ISB is emitted).
func TestLimitsStopOutClosesLeg(t *testing.T) {
	var l Limits
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	if out := applyAt(&l, []Intent{limitsPHL(90, 88, 99, exp)}, prev, limitsK(93, 94, 92, 93, 1), 1); len(out) != 1 {
		t.Fatalf("PHL placement: want 1 entry, got %d", len(out))
	}
	// The fill candle trades through the entry AND the stop: filled, then
	// stopped out — a loss that closes the leg.
	out2 := applyAt(&l, []Intent{limitsISB(92, 89, 98, exp)}, limitsK(93, 94, 92, 93, 1), limitsK(89, 91, 87, 88, 2), 2)
	if len(out2) != 0 {
		t.Fatalf("ISB after stopout: want 0 entries (leg closed), got %d", len(out2))
	}
	if l.Long == nil || !l.Long.Stopped {
		t.Fatalf("leg: want Stopped=true, got %+v", l.Long)
	}
}

// G1 (c): a 1m CLOSE beyond the prior high opens a NEW leg — after the
// stop-out, a close above the extreme resets and the next PHL is allowed.
// MUTANT: delete the long-leg reset line in resetBreaks → this test goes RED
// (the PHL after the break is refused).
func TestLimitsCloseBeyondExtremeOpensNewLeg(t *testing.T) {
	var l Limits
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	applyAt(&l, []Intent{limitsPHL(90, 88, 99, exp)}, prev, limitsK(93, 94, 92, 93, 1), 1)
	applyAt(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(89, 91, 87, 88, 2), 2) // fill + stop-out
	if l.Long == nil || !l.Long.Stopped {
		t.Fatalf("leg: want Stopped=true, got %+v", l.Long)
	}
	// prev closes at 101 — strictly beyond the prior high 100 — the new leg
	// (limitsPHL carries no anchor, so no loss box applies here).
	out3 := applyAt(&l, []Intent{limitsPHL(90, 88, 99, exp)}, limitsK(100, 101.5, 99, 101, 3), limitsK(94, 95, 93, 94, 4), 4)
	if len(out3) != 1 {
		t.Fatalf("PHL after the break: want 1 entry (new leg), got %d", len(out3))
	}
	// The leg is created at FILL, not placement — the fresh placement has
	// not filled yet.
	if l.Long != nil {
		t.Fatalf("leg: want nil before the fill, got %+v", l.Long)
	}
}

// G2 (d) B22 (FOMO extra @04:47-06:22; D4.2 p2 @02:41-04:06; X7 @10:11-11:05):
// the loss area is STRUCTURE — a long loss at a level is left only on a 1m
// CLOSE above the prior swing high (the nearest old extreme on the target
// side) or below the wave low (the lowest low from the entry through the
// stop-out candle). The 20-pt distance rule is GONE (the knob is an OFF
// fallback). G1 is off.
// PIN 1: a close 27 pts from the loss price but still INSIDE the wave keeps
// the place blocked. PIN 2: a close beyond the prior swing high frees it.
// MUTANT (pin 1): restore the 20-pt close rule in place of the structural
// rule → RED. MUTANT (pin 2): drop the swing-break path → RED.
func TestLimitsLossBlocksUntilDeparture(t *testing.T) {
	var l Limits
	cfg := limitsCfgNoLeg() // G2 only — the leg budget must not shadow it
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	applyAtG2(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "key_level:97", 97)}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg)
	applyAtG2(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(89, 91, 88.5, 89, 2), 2, cfg) // fill
	applyAtG2(&l, nil, limitsK(89, 91, 88.5, 89, 2), limitsK(70, 72, 59, 70, 3), 3, cfg) // stop-out → loss; wave low = 59

	// PIN 1: close 70 = 27 pts from the loss price 97, still INSIDE the
	// wave (70 > 59) and below the swing (100) → stays blocked.
	out4 := applyAtG2(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "key_level:97", 97)}, limitsK(70, 72, 59, 70, 3), limitsK(70, 72, 69, 70, 4), 4, cfg)
	if len(out4) != 0 {
		t.Fatalf("27-pt close inside the wave: want 0 entries, got %d", len(out4))
	}
	// PIN 2: a close 8 pts beyond the prior swing high frees the place
	// regardless of the distance to the loss price.
	out5 := applyAtG2(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "key_level:97", 97)}, limitsK(70, 72, 69, 70, 4), limitsK(107, 108.5, 106, 108, 5), 5, cfg)
	if len(out5) != 1 {
		t.Fatalf("swing break: want 1 entry, got %d", len(out5))
	}
}

// K1 (CTO 13:15:27Z): the EMA's place key is the CONSTANT line key — a loss
// at the EMA still blocks the EMA after the line MOVED. The B22 structural
// departure applies: the swing is the old extreme beyond the entry.
// MUTANT: in normalizePlace, key the EMA case by price instead of the
// constant line key → this test goes RED (the moved EMA is not blocked).
func TestLimitsEMALossBlocksAfterMove(t *testing.T) {
	var l Limits
	cfg := limitsCfgNoLeg()
	levels := []Level{
		{Key: "ema34", Kind: KindEMA34, Price: 100},
		{Key: "old_extreme:100", Kind: KindOldExtreme, Price: 100},
	}
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	applyLevels(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "ema34", 100)}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg, levels)
	applyLevels(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(89, 91, 88.5, 89, 2), 2, cfg, levels) // fill
	applyLevels(&l, nil, limitsK(89, 91, 88.5, 89, 2), limitsK(70, 72, 59, 70, 3), 3, cfg, levels) // stop → loss at the EMA

	// The line moved to 105; a close inside the wave keeps the EMA blocked
	// under its CONSTANT key even though the price changed.
	out4 := applyLevels(&l, []Intent{limitsPHLAt(94, 92, 99, exp, "ema34", 105)}, limitsK(70, 72, 59, 70, 3), limitsK(70, 72, 69, 70, 4), 4, cfg, levels)
	if len(out4) != 0 {
		t.Fatalf("moved-EMA re-entry inside the wave: want 0 entries, got %d", len(out4))
	}
	// The swing break frees the moved EMA.
	out5 := applyLevels(&l, []Intent{limitsPHLAt(94, 92, 99, exp, "ema34", 105)}, limitsK(70, 72, 69, 70, 4), limitsK(107, 108.5, 106, 108, 5), 5, cfg, levels)
	if len(out5) != 1 {
		t.Fatalf("moved-EMA re-entry after the swing break: want 1 entry, got %d", len(out5))
	}
}

// K2 (CTO 13:15:27Z): an old-extreme entry keys on the COINCIDENT KEY LEVEL
// within ±2 pts — a loss at the old extreme blocks the key level that makes
// it a location.
// MUTANT: in normalizePlace, return the raw old-extreme key → this test goes
// RED (the key-level re-entry is not blocked).
func TestLimitsOldExtremeLossBlocksCoincidentKeyLevel(t *testing.T) {
	var l Limits
	cfg := limitsCfgNoLeg()
	levels := []Level{
		{Key: "old_extreme:100.00", Kind: KindOldExtreme, Price: 100},
		{Key: "key_level:99.5:1", Kind: KindKeyLevel, Price: 99.5},
	}
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	applyLevels(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "old_extreme:100.00", 100)}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg, levels)
	applyLevels(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(89, 91, 88.5, 89, 2), 2, cfg, levels) // fill
	applyLevels(&l, nil, limitsK(89, 91, 88.5, 89, 2), limitsK(70, 72, 59, 70, 3), 3, cfg, levels) // stop → loss at the coincident key level
	if l.Places == nil || l.Places["key_level:99.5:1"] == nil {
		t.Fatalf("loss: want it under the coincident key level, got %+v", l.Places)
	}

	// Re-entry AT the key level with a close inside the wave → blocked.
	out4 := applyLevels(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "key_level:99.5:1", 99.5)}, limitsK(70, 72, 59, 70, 3), limitsK(70, 72, 69, 70, 4), 4, cfg, levels)
	if len(out4) != 0 {
		t.Fatalf("key-level re-entry at the blocked place: want 0 entries, got %d", len(out4))
	}
	// The swing break clears the block.
	out5 := applyLevels(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "key_level:99.5:1", 99.5)}, limitsK(70, 72, 69, 70, 4), limitsK(107, 108.5, 106, 108, 5), 5, cfg, levels)
	if len(out5) != 1 {
		t.Fatalf("key-level re-entry after the swing break: want 1 entry, got %d", len(out5))
	}
}

// BOX (CTO 13:20:08Z + B22): a box is ONE place — a loss at either edge
// blocks the WHOLE box until a 1m candle lies COMPLETELY outside it, wicks
// included. The place key is the box key WITHOUT the ":top"/":bottom"
// edge suffix.
// PIN 3: a candle whose close leaves the box but whose WICK still touches
// keeps the box blocked.
// MUTANT (pin 3): depart on close-outside instead of full-candle-outside →
// this test goes RED. MUTANT: keep the edge suffix → the top-edge re-entry
// is not blocked after a bottom-edge loss → RED.
func TestLimitsBoxLossBlocksWholeBox(t *testing.T) {
	var l Limits
	cfg := limitsCfgNoLeg()
	levels := []Level{
		{Key: "ftgh:120:80:top", Kind: KindFTGHEdge, Price: 120},
		{Key: "ftgh:120:80:bottom", Kind: KindFTGLEdge, Price: 80},
	}
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	// Loss at the BOTTOM edge.
	applyLevels(&l, []Intent{limitsPHLAt(70, 68, 99, exp, "ftgh:120:80:bottom", 80)}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg, levels)
	applyLevels(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(69, 71, 67, 68, 2), 2, cfg, levels) // fill + stop → loss at the box
	if l.Places == nil || l.Places["ftgh:120:80"] == nil {
		t.Fatalf("loss: want it under the box key, got %+v", l.Places)
	}
	// PIN 3: the close (78) leaves the box but the WICK (high 119) still
	// touches it → the whole box stays blocked.
	out3 := applyLevels(&l, []Intent{limitsPHLAt(118, 116, 99, exp, "ftgh:120:80:top", 120)}, limitsK(69, 71, 67, 68, 2), limitsK(74, 119, 73, 78, 3), 3, cfg, levels)
	if len(out3) != 0 {
		t.Fatalf("wick-touching re-entry: want 0 entries, got %d", len(out3))
	}
	// A candle COMPLETELY outside the box (wicks included) frees it.
	out4 := applyLevels(&l, []Intent{limitsPHLAt(118, 116, 99, exp, "ftgh:120:80:top", 120)}, limitsK(74, 119, 73, 78, 3), limitsK(74, 79, 73, 78, 4), 4, cfg, levels)
	if len(out4) != 1 {
		t.Fatalf("top-edge re-entry after leaving the box: want 1 entry, got %d", len(out4))
	}
}

// B22 follow-up (CTO 20:39:46Z): the MAIN box path carries the UNSUFFIXED box
// key (boxEntryIntent sets AnchorKey = b.Key, box_trade.go) — it must be a box
// place too. Pin at the PRODUCTION call site: an intent BUILT BY
// boxEntryIntent, filled and stopped, then a wick still touching the box stays
// blocked and a candle fully outside frees it.
// MUTANT: drop the ftgh:/ftgl: prefix case in normalizePlace → the
// fully-outside candle stays blocked → RED.
func TestLimitsBoxReturnLossBlocksWholeBox(t *testing.T) {
	var l Limits
	cfg := limitsCfgNoLeg()
	cfg.LocTriggerFilter = false // isolate the box gates
	levels := []Level{{Key: "key_level:140", Kind: KindKeyLevel, Price: 140}}
	b := Box{Key: "ftgl:120:80", Kind: FTGL, Top: 120, Bottom: 80}
	// The return candle touches the low edge and closes above the top.
	ref := market.Kline{Open: 75, High: 78, Low: 74, Close: 121}
	prev := limitsK(94, 96, 94, 95, 0)

	intent := boxEntryIntent(ref, b, []Box{b}, levels, TriggerLine{}, cfg)
	if len(intent) != 1 {
		t.Fatalf("boxEntryIntent: want 1 intent, got %d", len(intent))
	}
	if out := applyLevels(&l, intent, prev, limitsK(93, 94, 92, 93, 1), 1, cfg, levels); len(out) != 1 {
		t.Fatalf("box return placement: want 1 entry, got %d", len(out))
	}
	// Fill + stop-out on the next candle → loss at the box.
	applyLevels(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(77, 79, 73, 75, 2), 2, cfg, levels)
	if l.Places == nil || l.Places["ftgl:120:80"] == nil || !l.Places["ftgl:120:80"].Box {
		t.Fatalf("loss: want a box place under the unsuffixed key, got %+v", l.Places)
	}
	// A wick still touching the box (high 119 inside [80,120]) → blocked.
	out3 := applyLevels(&l, boxEntryIntent(ref, b, []Box{b}, levels, TriggerLine{}, cfg), limitsK(77, 79, 73, 75, 2), limitsK(74, 119, 73, 78, 3), 3, cfg, levels)
	if len(out3) != 0 {
		t.Fatalf("wick-touching re-entry: want 0 entries, got %d", len(out3))
	}
	// Fully outside, wicks included → freed.
	out4 := applyLevels(&l, boxEntryIntent(ref, b, []Box{b}, levels, TriggerLine{}, cfg), limitsK(74, 119, 73, 78, 3), limitsK(74, 79, 73, 78, 4), 4, cfg, levels)
	if len(out4) != 1 {
		t.Fatalf("fully-outside re-entry: want 1 entry, got %d", len(out4))
	}
}

// G2 (e): two losses at one place -> that place is OFF FOR THE DAY — even a
// departure does not clear it; the next trading day does. G1 is off.
// MUTANT: change the off-for-day threshold to `p.Losses > 2` → this test
// goes RED (the third re-entry is emitted).
func TestLimitsTwoLossesOffForDay(t *testing.T) {
	var l Limits
	cfg := limitsCfgNoLeg() // G2 only — the leg budget must not shadow it
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	// Loss 1 at the key level 97 (the loss candle never counts as the
	// departure).
	applyAtG2(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "key_level:97", 97)}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg)
	applyAtG2(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(89, 91, 88.5, 89, 2), 2, cfg) // fill
	applyAtG2(&l, nil, limitsK(89, 91, 88.5, 89, 2), limitsK(70, 72, 59, 70, 3), 3, cfg) // stop → loss 1
	// The swing break frees the place; re-enter → loss 2.
	if out := applyAtG2(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "key_level:97", 97)}, limitsK(70, 72, 59, 70, 3), limitsK(107, 108.5, 106, 108, 4), 4, cfg); len(out) != 1 {
		t.Fatalf("re-entry after the swing break: want 1 entry, got %d", len(out))
	}
	applyAtG2(&l, nil, limitsK(107, 108.5, 106, 108, 4), limitsK(89, 91, 88.5, 89, 5), 5, cfg) // fill
	applyAtG2(&l, nil, limitsK(89, 91, 88.5, 89, 5), limitsK(70, 72, 59, 70, 6), 6, cfg)       // stop → loss 2
	if l.Places == nil || l.Places["key_level:97"] == nil || !l.Places["key_level:97"].OffDay {
		t.Fatalf("place after two losses: want OffDay=true, got %+v", l.Places)
	}
	// A structural departure does NOT clear an off-for-day place.
	out7 := applyAtG2(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "key_level:97", 97)}, limitsK(70, 72, 59, 70, 6), limitsK(107, 108.5, 106, 108, 7), 7, cfg)
	if len(out7) != 0 {
		t.Fatalf("same-day re-entry after two losses: want 0 entries, got %d", len(out7))
	}
	// The next trading day clears the registry.
	next := time.Date(2026, 10, 3, 9, 8, 0, 0, time.UTC).UnixMilli()
	out8 := l.Apply([]Intent{limitsPHLAt(90, 88, 99, next+86400_000, "key_level:97", 97)}, limitsK(107, 108.5, 106, 108, 7), limitsK(93, 94, 92, 93, 8), next, limitsLvlsG2, cfg)
	if len(out8) != 1 {
		t.Fatalf("next-day re-entry: want 1 entry, got %d", len(out8))
	}
}
