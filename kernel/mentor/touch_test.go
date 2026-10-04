package mentor

import (
	"testing"

	"vl/market"
)

// The touch rules follow the CTO-corrected reading (2026-10-02):
// REJECT = the first touching candle closes BACK on the side price came FROM.
// Resistance (came from below): close BELOW → sell stop under the candle.
// Support (came from above): close ABOVE → buy stop over the candle.
// A close THROUGH the line = wrong side → CANCEL + level INVALID.

// TestTouchRejectAtResistanceCloseBelow — source [D5.2 p3 @ 21:30]:
// "ĐỤNG, ĐÓNG DƯỚI — ĐẶT LỆNH SELL STOP… đang đụng RESISTANCE".
func TestTouchRejectAtResistanceCloseBelow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	lvl := Level{Key: "k1", Kind: KindKeyLevel, Price: 100}
	tr := &Touch{LevelKey: "k1"}
	// confirmation rule (c): the outcome is judged on the CLOSE of the 1m
	// reference candle (§3) — the intrabar High pokes THROUGH the resistance
	// (102 > 100) but the close comes back below: reject, not a through.
	// previous close BELOW the level: resistance, approached from below
	intents := TouchTick(tr, lvl, 99, market.Kline{High: 102.0, Low: 98.0, Close: 98.5}, cfg)
	if tr.Outcome != TouchReject {
		t.Fatalf("outcome = %q, want reject (intrabar through, closed back below resistance)", tr.Outcome)
	}
	if len(intents) != 0 {
		t.Fatalf("reject emits no intents here (order is the setup layer's): %+v", intents)
	}
	side, price, ok := RejectEntry(*tr, cfg)
	if !ok || side != SideShort || price != 98.0-1.5 {
		t.Fatalf("RejectEntry = %q/%v/%v, want short/96.5/true", side, price, ok)
	}
}

// TestTouchWrongWayAtResistanceCloseAbove — source [D5.2 p3 @ 21:30]:
// "ĐỤNG, ĐÓNG LÊN TRÊN — CANCEL LỆNH…" — a close THROUGH the resistance.
func TestTouchWrongWayAtResistanceCloseAbove(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	lvl := Level{Key: "k1", Kind: KindKeyLevel, Price: 100}
	tr := &Touch{LevelKey: "k1"}
	intents := TouchTick(tr, lvl, 99, market.Kline{High: 102.0, Low: 99.5, Close: 101.0}, cfg)
	if tr.Outcome != TouchWrongWay {
		t.Fatalf("outcome = %q, want wrong_way (closed through the resistance)", tr.Outcome)
	}
	if len(intents) != 2 || intents[0].Action != CancelArm || intents[1].Action != LevelInvalid {
		t.Fatalf("intents = %+v, want cancel + level_invalid", intents)
	}
	if intents[1].LevelKey != "k1" {
		t.Fatalf("level_invalid names %q, want k1", intents[1].LevelKey)
	}
}

// TestTouchRejectAtSupportCloseAbove — the mirror of the source quote at a
// support: close ABOVE → buy stop over the candle.
func TestTouchRejectAtSupportCloseAbove(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	lvl := Level{Key: "k1", Kind: KindKeyLevel, Price: 100}
	tr := &Touch{LevelKey: "k1"}
	// previous close ABOVE the level: support, approached from above
	intents := TouchTick(tr, lvl, 101, market.Kline{High: 100.5, Low: 98.0, Close: 100.2}, cfg)
	if tr.Outcome != TouchReject {
		t.Fatalf("outcome = %q, want reject (closed back above support)", tr.Outcome)
	}
	if len(intents) != 0 {
		t.Fatalf("reject emits no intents here: %+v", intents)
	}
	side, price, ok := RejectEntry(*tr, cfg)
	if !ok || side != SideLong || price != 100.5+1.5 {
		t.Fatalf("RejectEntry = %q/%v/%v, want long/102/true", side, price, ok)
	}
}

// TestTouchWrongWayAtSupportCloseBelow — a close THROUGH the support → the
// level is INVALID, only ISBs may trade there after [D5.2 p1 @ 19:51].
func TestTouchWrongWayAtSupportCloseBelow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	lvl := Level{Key: "k1", Kind: KindKeyLevel, Price: 100}
	tr := &Touch{LevelKey: "k1"}
	intents := TouchTick(tr, lvl, 101, market.Kline{High: 100.5, Low: 98.0, Close: 98.5}, cfg)
	if tr.Outcome != TouchWrongWay {
		t.Fatalf("outcome = %q, want wrong_way (closed through the support)", tr.Outcome)
	}
	if len(intents) != 2 || intents[1].Action != LevelInvalid {
		t.Fatalf("intents = %+v, want cancel + level_invalid", intents)
	}
}

// TestTouchOnlyFirstCandleCounts — §3 step 2 [D3.3 p1 @ 00:31; D5.3 p1
// @ 20:40]: the FIRST touching candle is the reference, "never the second".
// A later candle never reclassifies.
func TestTouchOnlyFirstCandleCounts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	lvl := Level{Key: "k1", Kind: KindKeyLevel, Price: 100}
	tr := &Touch{LevelKey: "k1"}
	first := market.Kline{High: 100.5, Low: 98.0, Close: 100.2}
	TouchTick(tr, lvl, 101, first, cfg)
	if tr.Outcome != TouchReject {
		t.Fatalf("first touch = %q, want reject", tr.Outcome)
	}
	// a second candle crosses and closes through — ignored.
	if got := TouchTick(tr, lvl, 99, market.Kline{High: 101, Low: 99, Close: 100.2}, cfg); len(got) != 0 {
		t.Fatalf("second candle reclassified the first touch: %+v", got)
	}
	if tr.Outcome != TouchReject || tr.RefBar != first {
		t.Fatalf("reference changed: %+v", tr)
	}
}

// TestTouchNoNearTouch — §3 step 1: wait for the LITERAL touch (the candle's
// range must reach the level). A candle that stops short emits nothing.
func TestTouchNoNearTouch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	lvl := Level{Key: "k1", Kind: KindKeyLevel, Price: 100}
	tr := &Touch{LevelKey: "k1"}
	// range [100.4, 100.8] stops short of the level → no touch at all
	if got := TouchTick(tr, lvl, 101, market.Kline{High: 100.8, Low: 100.4, Close: 100.5}, cfg); len(got) != 0 {
		t.Fatalf("near-touch emitted: %+v", got)
	}
	if tr.Outcome != TouchNone {
		t.Fatalf("near-touch classified as %q", tr.Outcome)
	}
	// a genuine touch (range reaches the level) classifies: support, close
	// back above → reject.
	TouchTick(tr, lvl, 101, market.Kline{High: 100.5, Low: 99.0, Close: 100.2}, cfg)
	if tr.Outcome != TouchReject {
		t.Fatalf("literal touch = %q, want reject", tr.Outcome)
	}
}

// TestTouchOnRecordedTapeClassifiesOncePerLevel — canon 53: run the §3
// classifier across the recorded 2026-09-15 RTH day against that day's key
// levels; every level that gets touched is classified exactly once and the
// outcome never flips.
func TestTouchOnRecordedTapeClassifiesOncePerLevel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1 // rich 1m level set — this test is about touch classification, not the 1H source
	bars := loadFixture(t, "mnq_1m_2026-09-15_rth", "1m")
	levels := KeyLevels(bars, cfg)
	if len(levels) == 0 {
		t.Fatal("no key levels on the recorded day")
	}
	touches := map[string]*Touch{}
	for i := 1; i < len(bars); i++ {
		prevClose := bars[i-1].Close
		for _, lvl := range levels {
			tr := touches[lvl.Key]
			if tr == nil {
				tr = &Touch{LevelKey: lvl.Key}
				touches[lvl.Key] = tr
			}
			before := tr.Outcome
			intents := TouchTick(tr, lvl, prevClose, bars[i], cfg)
			if before != TouchNone && tr.Outcome != before {
				t.Fatalf("level %s reclassified from %q to %q at bar %d", lvl.Key, before, tr.Outcome, i)
			}
			for _, in := range intents {
				if in.Action == LevelInvalid && in.LevelKey != lvl.Key {
					t.Fatalf("level_invalid names %q, want %q", in.LevelKey, lvl.Key)
				}
			}
		}
	}
	touched := 0
	rejects := 0
	wrongWay := 0
	for _, tr := range touches {
		switch tr.Outcome {
		case TouchReject:
			rejects++
		case TouchWrongWay:
			wrongWay++
		}
		if tr.Outcome != TouchNone {
			touched++
		}
	}
	t.Logf("recorded day: %d levels, %d touched (%d reject, %d wrong-way)",
		len(levels), touched, rejects, wrongWay)
	if touched == 0 {
		t.Fatal("no level was touched on the recorded day — the tape never exercises §3 (fixture too thin)")
	}
}

// TestTouchTiePreviousCloseNoApproach — CTO review 2d3aad1ef: a previous close
// exactly ON the level is a tie — no approach side yet. The touch waits for a
// bar that closes off the level; nothing classifies on the tie bar.
func TestTouchTiePreviousCloseNoApproach(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	lvl := Level{Key: "k1", Kind: KindKeyLevel, Price: 100}
	tr := &Touch{LevelKey: "k1"}
	// prevClose == the level: the candle touches and closes above, but there
	// is no approach side → nothing classifies.
	if got := TouchTick(tr, lvl, 100, market.Kline{High: 101, Low: 99.5, Close: 100.2}, cfg); len(got) != 0 {
		t.Fatalf("tie bar emitted: %+v", got)
	}
	if tr.Outcome != TouchNone {
		t.Fatalf("tie bar classified as %q", tr.Outcome)
	}
	// the NEXT bar closes off the level (above) and its touch classifies as a
	// support reject.
	if got := TouchTick(tr, lvl, 100.2, market.Kline{High: 100.5, Low: 99.0, Close: 100.2}, cfg); len(got) != 0 || tr.Outcome != TouchReject {
		t.Fatalf("off-level bar: intents=%+v outcome=%q, want reject", got, tr.Outcome)
	}
}

// TestTouchDisabledIsNil — L4: mentor_mode OFF → nothing.
func TestTouchDisabledIsNil(t *testing.T) {
	lvl := Level{Key: "k1", Price: 100}
	tr := &Touch{LevelKey: "k1"}
	if got := TouchTick(tr, lvl, 101, market.Kline{High: 100.5, Low: 99.0, Close: 100.2}, Config{}); got != nil {
		t.Fatalf("disabled evaluator emitted: %+v", got)
	}
}
