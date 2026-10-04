package mentor

import (
	"testing"

	"vl/market"
)

// TestEMAStateKeysStayStable — the EMA 34/9 lines are ONE line each even
// though their value moves every bar. Keying by value would fragment the
// touch/invalid state (a new level per bar, the wrong-way-close rule would
// never stick). After a full recorded day, the state must hold at most two
// EMA touch keys and at most two EMA ISB-only keys.
func TestEMAStateKeysStayStable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	bars := loadFixture(t, "mnq_1m_2026-09-15_rth", "1m")
	for i := 2; i <= len(bars); i++ {
		e.Tick(bars[:i], bars[i-1].OpenTime+59_999)
	}
	emaTouches := 0
	emaInvalid := 0
	for k := range e.State.Touches {
		if k == string(KindEMA9) || k == string(KindEMA34) {
			emaTouches++
		}
	}
	for k := range e.State.ISBOnly {
		if k == string(KindEMA9) || k == string(KindEMA34) {
			emaInvalid++
		}
	}
	if emaTouches > 2 || emaInvalid > 2 {
		t.Fatalf("EMA state fragmented: %d touch keys, %d invalid keys (want <= 2 each)", emaTouches, emaInvalid)
	}
}

// TestEMATouchResetsWhenLineDrifts — a touch against the EMA at one price is
// not a touch of the same line after it moved away: freshTouch resets the
// classification when the line drifts more than a tick from where it was
// touched.
func TestEMATouchResetsWhenLineDrifts(t *testing.T) {
	tr := Touch{LevelKey: string(KindEMA34), Outcome: TouchWrongWay, RefBar: market.Kline{High: 100}, PriceAtTouch: 100}
	// same price → keep the classification
	kept := freshTouch(tr, Level{Key: string(KindEMA34), Kind: KindEMA34, Price: 100})
	if kept.Outcome != TouchWrongWay {
		t.Fatalf("same-price tick reset the touch: %+v", kept)
	}
	// drifted 6 pts → reset to an untouched state keyed to the level
	got := freshTouch(tr, Level{Key: string(KindEMA34), Kind: KindEMA34, Price: 106})
	if got.Outcome != TouchNone || got.LevelKey != string(KindEMA34) {
		t.Fatalf("drifted line kept the stale classification: %+v", got)
	}
	// static levels (key levels) never drift: price equality always keeps
	static := freshTouch(tr, Level{Key: "k1", Kind: KindKeyLevel, Price: 100})
	if static.Outcome != TouchWrongWay {
		t.Fatalf("static level touch was reset: %+v", static)
	}
}
