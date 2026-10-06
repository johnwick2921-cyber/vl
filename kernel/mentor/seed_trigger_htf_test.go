package mentor

import (
	"testing"

	"vl/market"
)

// ── F2 (release #10): Seed warms State.Trigger (5m) and State.HTF (4h + 1h) ──
//
// Before F2, Seed never set State.Trigger / State.HTF — every boot rebuilt the
// 4h line from the 1500-bar tick window (~6 4h candles). These pins drive the
// CALL SITE (Seed + the first Tick) and the cold path.

// seedTriggerHTFBars builds all-hours 1m bars for ~7.4 days whose 4h buckets
// break HIGH exactly once (~3.7 days ago, bucket 19 of the 17:00 CT anchor) and
// then never break high or low again — so a 4h LONG trigger fires on bucket 19
// and stays LONG. A 25h window of the tail (the tick window) contains only the
// flat buckets → a COLD evaluator reads "no 4h trigger".
func seedTriggerHTFBars(t *testing.T) []market.Kline {
	t.Helper()
	bucketPrice := func(bucket int) (hi, lo float64) {
		switch {
		case bucket < 19:
			return 100, 99 // flat — no break
		case bucket == 19:
			return 101, 99.5 // breaks bucket 18's high 100 → LONG
		default:
			return 100.5, 99.5 // no new high, no low break
		}
	}
	start := ctMs(t, -7, 17, 0)
	end := ctMs(t, 0, 13, 0)
	var buckets []int64
	var out []market.Kline
	for ms := start; ms < end; ms += 60_000 {
		b := fourHBucketStart(ms, ctime())
		idx := -1
		for i, bs := range buckets {
			if bs == b {
				idx = i
				break
			}
		}
		if idx < 0 {
			idx = len(buckets)
			buckets = append(buckets, b)
		}
		hi, lo := bucketPrice(idx)
		out = append(out, market.Kline{
			Open: lo, High: hi, Low: lo, Close: lo,
			OpenTime: ms, CloseTime: ms + 59_999, Final: true,
		})
	}
	return out
}

func enabledCfg() Config {
	c := DefaultConfig()
	c.Enabled = true
	return c
}

// TestSeedTriggerHTFSeedsOld4HDirection — the named RED: a 4h trigger set ~3.7
// days ago must be present on the FIRST tick (a cold 25h window reads "no 4h
// trigger yet"). Dropping the seed advance makes this fail.
func TestSeedTriggerHTFSeedsOld4HDirection(t *testing.T) {
	bars := seedTriggerHTFBars(t)
	now := ctMs(t, 0, 13, 5)

	e := New(enabledCfg())
	Seed(e, bars, now)

	if e.State.HTF.FourH.Dir != SideLong {
		t.Fatalf("seed must warm the 4h trigger (set ~3.7 days ago), got dir=%q", e.State.HTF.FourH.Dir)
	}

	// The first tick (the tail window, as mentorEvalOnce passes ~1500 bars) must
	// NOT reset the seeded 4h direction.
	tail := bars[len(bars)-1500:]
	e.Tick(tail, now)
	if e.State.HTF.FourH.Dir != SideLong {
		t.Fatalf("the first tick must keep the seeded 4h direction, got dir=%q", e.State.HTF.FourH.Dir)
	}
}

// TestSeedTriggerHTFFirstTickDoesNotReAdvance — watermark pin: the first tick
// over the tail window (a subset of the seeded bars) must not re-advance the
// committed buckets.
func TestSeedTriggerHTFFirstTickDoesNotReAdvance(t *testing.T) {
	bars := seedTriggerHTFBars(t)
	now := ctMs(t, 0, 13, 5)

	e := New(enabledCfg())
	Seed(e, bars, now)
	beforeTrigger := e.State.Trigger.LastBucket
	before4h := e.State.HTF.FourH.LastBucket

	tail := bars[len(bars)-1500:]
	e.Tick(tail, now)

	if e.State.Trigger.LastBucket != beforeTrigger {
		t.Fatalf("the first tick must not re-advance the seeded 5m buckets: %d -> %d", beforeTrigger, e.State.Trigger.LastBucket)
	}
	if e.State.HTF.FourH.LastBucket != before4h {
		t.Fatalf("the first tick must not re-advance the seeded 4h buckets: %d -> %d", before4h, e.State.HTF.FourH.LastBucket)
	}
}

// TestSeedTriggerHTFColdEvaluatorMatchesSeed — a cold evaluator (New + Tick, no
// Seed) over the SAME bars must produce byte-identical trigger/HTF state as the
// seeded path, proving Seed uses the exact same functions and aggregation.
func TestSeedTriggerHTFColdEvaluatorMatchesSeed(t *testing.T) {
	bars := seedTriggerHTFBars(t)
	now := ctMs(t, 0, 13, 5)

	seeded := New(enabledCfg())
	Seed(seeded, bars, now)

	cold := New(enabledCfg())
	cold.Tick(bars, now)

	if seeded.State.Trigger != cold.State.Trigger {
		t.Fatalf("cold Tick trigger differs from seeded: seeded=%+v cold=%+v", seeded.State.Trigger, cold.State.Trigger)
	}
	if seeded.State.HTF.FourH != cold.State.HTF.FourH || seeded.State.HTF.OneH != cold.State.HTF.OneH {
		t.Fatalf("cold Tick HTF differs from seeded: seeded4h=%+v cold4h=%+v seeded1h=%+v cold1h=%+v",
			seeded.State.HTF.FourH, cold.State.HTF.FourH, seeded.State.HTF.OneH, cold.State.HTF.OneH)
	}
}
