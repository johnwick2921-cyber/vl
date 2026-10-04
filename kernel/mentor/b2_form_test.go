package mentor

import (
	"testing"

	"vl/market"
)

// TestTriggerFormingBucketReevaluatedEachTick — the forming bucket is NEVER
// committed: it is re-evaluated every tick with its growing extremes, so a
// break fires at the MINUTE it happens (confirmation rule (a) — never from
// the bucket open, and never lost). Same-direction re-breaks are idempotent.
func TestTriggerFormingBucketReevaluatedEachTick(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	// bucket 17:00 recorded; bucket 17:05 partial (no break yet)
	first := TriggerTick(TriggerLine{}, []market.Kline{
		b5(0, 100, 96, 97),
		b5(5, 99, 97, 98), // high 99 does NOT break 100
	}, 5, cfg)
	if first.Dir != "" {
		t.Fatalf("no break should have fired yet: %+v", first)
	}
	// the SAME forming bucket grows a high that breaks → the line fires NOW.
	second := TriggerTick(first, []market.Kline{
		b5(0, 100, 96, 97),
		b5(5, 104, 97, 99), // grown: high 104 > 100 — the break minute arrived
	}, 5, cfg)
	if second.Dir != SideLong || second.Price != 100 {
		t.Fatalf("the grown forming bucket must fire at the break minute: %+v", second)
	}
	// a NEW bucket may then reverse (the stream continues normally)
	third := TriggerTick(second, []market.Kline{
		b5(0, 100, 96, 97),
		b5(5, 104, 97, 99),
		b5(10, 99, 95, 95), // breaks the LOW of the (now past) bucket → sell line at 97
	}, 5, cfg)
	if third.Dir != SideShort || third.Price != 97 {
		t.Fatalf("next bucket break = %+v, want short @ 97", third)
	}
}
