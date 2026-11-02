package mentor

// D2-17 (item 17, D4.2-13) — G2 loss box by PRICE BAND, not only the level key.
//
// D4.2 p2 @01:56–02:12: "mình bị QUÉT một lệnh — ĐỪNG TRADE Ở KHU VỰC ĐÓ THÊM
// MỘT LẦN NÀO NỮA"; @03:48–04:07: "TÔI BỌC CÁI KHU VỰC NÀY LẠI. TÔI SẼ KHÔNG
// TRADE BÊN TRONG KHU VỰC NÀY". An ISB stop-out must box its entry-to-stop
// band, and a blocked place refuses by PRICE BAND (entry inside the loss's
// [wave, swing] area), not by key equality — a different level a few points
// away inside the same area is also refused, both sides.

import "testing"

// TestLimitsISBLossBoxesTheBand pins D4.2-13 at Limits.Apply: an ISB that fills
// and stops out boxes its [stop, entry] band; a re-entry inside the band is
// refused, a re-entry outside is allowed, and a candle fully outside the band
// frees it.
// MUTANT: drop the ISB's `box/lo/hi` on the pend → the ISB loss boxes nothing
// and the in-band re-entry is emitted → RED.
//
// Release #60 P2: the band is now the knob mentor_isb_loss_box=band; the
// default (range) boxes the whole 5m range — TestLimitsISBLossRangeCoversBand
// below and m60_loss_range_test.go.
func TestLimitsISBLossBoxesTheBand(t *testing.T) {
	var l Limits
	cfg := limitsCfgNoLeg() // G2 only — the G1 leg must not shadow it
	cfg.ISBLossBox = ISBLossBoxBand
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	// ISB placed (entry 92, stop 89).
	if out := applyAtG2(&l, []Intent{limitsISB(92, 89, 98, exp)}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg); len(out) != 1 {
		t.Fatalf("ISB placement: want 1, got %d", len(out))
	}
	// Candle 2 fills (high 93 >= 92) and stops (low 88 <= 89) — a loss that
	// boxes the [89, 92] band.
	applyAtG2(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(92, 93, 88, 89, 2), 2, cfg)
	if l.Places == nil || l.Places[isbBandKey(92, 89)] == nil || !l.Places[isbBandKey(92, 89)].Blocked {
		t.Fatalf("ISB loss must box the [89,92] band, places=%+v", l.Places)
	}
	// Re-entry INSIDE the band (entry 91) is refused.
	if out := applyAtG2(&l, []Intent{limitsISB(91, 88, 97, exp)}, limitsK(92, 93, 88, 89, 2), limitsK(90, 91.5, 89.5, 91, 3), 3, cfg); len(out) != 0 {
		t.Fatalf("in-band ISB re-entry: want 0, got %d (refusals=%v)", len(out), l.Refusals)
	}
	// A re-entry OUTSIDE the band (entry 94) is allowed even while the band
	// is still boxed — the box blocks only its own area.
	if out := applyAtG2(&l, []Intent{limitsISB(94, 91, 99, exp)}, limitsK(90, 91.5, 89.5, 91, 3), limitsK(93, 95, 92.5, 94, 4), 4, cfg); len(out) != 1 {
		t.Fatalf("out-of-band ISB re-entry: want 1, got %d (refusals=%v)", len(out), l.Refusals)
	}
}

// TestLimitsBandBlocksDifferentLevel pins the price-band match at Limits.Apply:
// a level loss at 97 (swing 100, wave 59) boxes [59,100] — a DIFFERENT level at
// 99 inside the area is refused; a level at 105 outside is allowed.
// MUTANT: drop the band loop in blockedPlace → the 99-level re-entry is emitted
// (only the exact 97 key is blocked) → RED.
func TestLimitsBandBlocksDifferentLevel(t *testing.T) {
	var l Limits
	cfg := limitsCfgNoLeg()
	levels := limitsLvlsG2 // key_level:97 + old_extreme:100
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	// Loss at key_level:97 (anchor 97): fill, then a stop-out down to wave 59.
	applyLevels(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "key_level:97", 97)}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg, levels)
	applyLevels(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(89, 91, 88.5, 89, 2), 2, cfg, levels) // fill
	applyLevels(&l, nil, limitsK(89, 91, 88.5, 89, 2), limitsK(70, 72, 59, 70, 3), 3, cfg, levels) // stop → loss, swing 100 wave 59
	if l.Places == nil || l.Places["key_level:97"] == nil || !l.Places["key_level:97"].Blocked {
		t.Fatalf("level loss must be blocked, places=%+v", l.Places)
	}
	// A DIFFERENT level at 99 (entry inside [59,100]) is refused.
	if out := applyLevels(&l, []Intent{limitsPHLAt(99, 97, 105, exp, "key_level:99", 99)}, limitsK(70, 72, 59, 70, 3), limitsK(70, 72, 69, 70, 4), 4, cfg, levels); len(out) != 0 {
		t.Fatalf("different level inside the band: want 0, got %d (refusals=%v)", len(out), l.Refusals)
	}
	// A level at 105 (outside the band) is allowed.
	if out := applyLevels(&l, []Intent{limitsPHLAt(105, 103, 110, exp, "key_level:105", 105)}, limitsK(70, 72, 69, 70, 4), limitsK(70, 72, 69, 70, 5), 5, cfg, levels); len(out) != 1 {
		t.Fatalf("level outside the band: want 1, got %d (refusals=%v)", len(out), l.Refusals)
	}
}

// TestLimitsISBLossRangeCoversBand — release #60 P2 (D4.2-R27 "em đợi cho cái
// điểm entry nằm ở vòng vòng bên ngoài" [D4.2 p2 @02:41–03:17]): the same
// D2-17 sequence under the default range box. The 09:00 5m bucket that held
// the trade ran 88–94 (candles 1..2), so the 94 re-entry that the band let through is now
// INSIDE the range and refused loss_range_blocked; the in-band 91 too.
// MUTANT: fall back to the band in loss() → the 94 re-entry is emitted → RED.
func TestLimitsISBLossRangeCoversBand(t *testing.T) {
	var l Limits
	cfg := limitsCfgNoLeg()
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	applyAtG2(&l, []Intent{limitsISB(92, 89, 98, exp)}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg)
	applyAtG2(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(92, 93, 88, 89, 2), 2, cfg) // fill + stop
	p := l.Places[isbBandKey(92, 89)]
	if p == nil || !p.Range || p.Lo != 88 || p.Hi != 94 {
		t.Fatalf("range box: want the 09:00 5m range 88–94 (candles 1..2), got %+v", l.Places)
	}
	if out := applyAtG2(&l, []Intent{limitsISB(91, 88, 97, exp)}, limitsK(92, 93, 88, 89, 2), limitsK(90, 91.5, 89.5, 91, 3), 3, cfg); len(out) != 0 {
		t.Fatalf("in-band re-entry: want 0, got %d", len(out))
	}
	if out := applyAtG2(&l, []Intent{limitsISB(94, 91, 99, exp)}, limitsK(90, 91.5, 89.5, 91, 3), limitsK(93, 95, 92.5, 94, 4), 4, cfg); len(out) != 0 {
		t.Fatalf("94 re-entry inside the 5m range: want 0, got %d", len(out))
	}
	if l.Refusals["loss_range_blocked"] != 2 {
		t.Fatalf("want loss_range_blocked=2, refusals=%v", l.Refusals)
	}
}
