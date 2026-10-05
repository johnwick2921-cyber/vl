package mentor

import (
	"testing"
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
