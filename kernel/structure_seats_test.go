package kernel

import (
	"reflect"
	"testing"
)

// S3 (2026-09-16, CTO ruling 2026-09-17 02:35Z) — day_plan.htf_seats drives
// seatHTF: nil = the LEGACY path (byte-identical to the pre-S3 table, including
// the class-NN restore-sort nullification); a saved value = the EFFECTIVE path
// (promotion survives because head and tail are sorted separately).

// htfsSeatFixture is PRE-SORTED like a real scorer output (score desc), and the
// HTF candidates score BELOW the head fillers — the case that matters: without
// an effective promotion they can never seat.
func htfsSeatFixture() []ScoredLevel {
	scored := make([]ScoredLevel, 0, 13)
	for i := 0; i < 8; i++ { // stronger round numbers fill the head
		scored = append(scored, ScoredLevel{
			DetectedLevel: DetectedLevel{Kind: KindRound, Price: 990 + float64(i), Label: "RN"},
			Score:         0.9, Grade: "B", Fresh: "fresh", Distance: 990 + float64(i) - 1000,
		})
	}
	for i := 0; i < 5; i++ { // HTF reversal zones lost the cut
		scored = append(scored, ScoredLevel{
			DetectedLevel: DetectedLevel{Kind: KindSupply, Price: 1100 + float64(i), Lo: 1098 + float64(i), Hi: 1102 + float64(i), Label: "Supply·4h", HTF: true, TF: "4h", ZonePattern: "reversal"},
			Score:         0.4, Grade: "C", Fresh: "fresh", Distance: 100 + float64(i),
		})
	}
	return scored
}

func htfCountInHead(out []ScoredLevel, maxLevels int) int {
	n := 0
	for _, l := range out[:maxLevels] {
		if isHTFSeatEligible(l) {
			n++
		}
	}
	return n
}

// the dispatch's seat test: htf_seats=4 seats four; 2 seats two; 0 seats none;
// 6 seats all five candidates available.
func TestSeatHTFSeatsKnob(t *testing.T) {
	for seats, want := range map[int]int{4: 4, 2: 2, 0: 0, 6: 5} {
		v := seats
		out := seatHTF(htfsSeatFixture(), 8, &v)
		if got := htfCountInHead(out, 8); got != want {
			t.Errorf("seats=%d: %d HTF in head, want %d", seats, got, want)
		}
	}
}

// the CTO's rewrite rule: with the knob UNSET (nil) the HTF candidates that
// score BELOW the head fillers do NOT seat (legacy restore sort — class NN),
// and with the knob SAVED they DO. Seating must be knob-gated, not score-gated.
func TestSeatHTFPromotesSwingLevels(t *testing.T) {
	fx := htfsSeatFixture()
	if got := htfCountInHead(seatHTF(fx, 8, nil), 8); got != 0 {
		t.Fatalf("knob unset (legacy path): %d HTF in head, want 0 — the legacy table must not change", got)
	}
	two := 2
	if got := htfCountInHead(seatHTF(fx, 8, &two), 8); got != 2 {
		t.Fatalf("knob saved=2 (effective path): %d HTF in head, want 2 — promotion must survive its own sort", got)
	}
	// today-priority entries must never be demoted, on either path.
	pri := append([]ScoredLevel{
		{DetectedLevel: DetectedLevel{Kind: KindPDH, Price: 1200, Label: "PDH", HTF: true}, Score: 1.4, Grade: "A", Fresh: "fresh", Distance: 200},
	}, fx...)
	out := seatHTF(pri, 8, &two)
	priSeated := false
	for _, l := range out[:8] {
		if l.Kind == KindPDH {
			priSeated = true
		}
	}
	if !priSeated {
		t.Fatal("today-priority PDH must stay seated")
	}
}

// seats ≤ 0 must return the input untouched (a legal knob value, not an error).
func TestSeatHTFZeroSeatsNoOp(t *testing.T) {
	in := htfsSeatFixture()
	zero := 0
	out := seatHTF(in, 8, &zero)
	if !reflect.DeepEqual(out, in) {
		t.Fatal("seats=0 must be a no-op — the table must not move")
	}
}

// parity pins (canon 53): the legacy wrapper and the S3 seats path with nil
// produce IDENTICAL tables; knob-off byte-identity against the pre-S3 binary is
// pinned by the existing goldens that run the production Assemble path at nil
// (identity_output_parity_test, one_setup_map_pin_test, weekly_shadow_test).
func TestS3HtfSeatsParityWithLegacy(t *testing.T) {
	levels, price, dATR := htfsParityLevels()
	viaWrapper, _ := ScoreLevelsMinGradeFull(levels, price, dATR, nil, 8, 1.5, "")
	viaSeats, _ := ScoreLevelsMinGradeFullSeats(levels, price, dATR, nil, 8, 1.5, "", nil, HTFScoreMultiplier, 0)
	if !reflect.DeepEqual(viaWrapper, viaSeats) {
		t.Fatalf("ScoreLevelsMinGradeFull and …FullSeats(nil) must be byte-identical — wrapper=%d rows, seats=%d rows", len(viaWrapper), len(viaSeats))
	}
}

// htfsParityLevels builds a small mixed pool where both seat passes run.
func htfsParityLevels() ([]DetectedLevel, float64, float64) {
	price, dATR := 30000.0, 300.0
	out := make([]DetectedLevel, 0, 14)
	for i := 0; i < 10; i++ {
		out = append(out, DetectedLevel{Kind: KindRound, Price: price - 100 + float64(i)*10, Label: "RN"})
	}
	out = append(out,
		DetectedLevel{Kind: KindSupply, Price: price + 80, Lo: price + 75, Hi: price + 85, Label: "Supply·1h", HTF: true, TF: "1h"},
		DetectedLevel{Kind: KindDemand, Price: price - 80, Lo: price - 85, Hi: price - 75, Label: "Demand·4h", HTF: true, TF: "4h"},
		DetectedLevel{Kind: KindPDH, Price: price + 120, Label: "PDH"},
		DetectedLevel{Kind: KindPDL, Price: price - 120, Label: "PDL"},
	)
	return out, price, dATR
}

// TWIN PROBE (CTO ruling item d): seatBothSides swaps entries IN and OUT of the
// maxLevels list and returns only that list, so its swap changes MEMBERSHIP and
// its restore sort only reorders — the twin is NOT nullified. Gap-day fixture:
// every today-priority kind sits ABOVE price; below-side candidates exist but
// lose the comparator. The swap must seat them anyway.
func TestSeatBothSidesSwapSurvivesOwnSort(t *testing.T) {
	price := 1000.0
	mk := func(kind LevelKind, dist, score float64) ScoredLevel {
		return ScoredLevel{DetectedLevel: DetectedLevel{Kind: kind, Price: price + dist, Label: string(kind)}, Score: score, Grade: "B", Fresh: "fresh", Distance: dist}
	}
	scored := []ScoredLevel{
		// head: 8 today-priority kinds, all ABOVE price (gap-down day shape)
		mk(KindPDH, 80, 1.2), mk(KindRTHH, 90, 1.1),
		mk(KindORH, 100, 1.0), mk(KindONH, 110, 0.9),
		mk(KindPWH, 120, 0.8), mk(KindPMH, 130, 0.7),
		mk(KindPDC, 140, 0.6), mk(KindSETT, 150, 0.5),
		// rest: below-side candidates the comparator can never rank into head
		mk(KindRound, -60, 0.4), mk(KindRound, -70, 0.4), mk(KindRound, -80, 0.4),
	}
	out := seatBothSides(scored, 8)
	below := 0
	for _, l := range out {
		if l.Distance < 0 {
			below++
		}
	}
	if below < MinSideLevels {
		t.Fatalf("gap-day rebalance failed: %d below-side levels seated, want >= %d — the swap does not survive", below, MinSideLevels)
	}
	// the seatHTF effective path runs BEFORE seatBothSides; a promoted HTF seat
	// may be swapped out by the side rule (P0.1 wins) — asserted here as the
	// documented interaction, not as a defect.
	if len(out) != 8 {
		t.Fatalf("seatBothSides must return exactly the cap, got %d", len(out))
	}
}
