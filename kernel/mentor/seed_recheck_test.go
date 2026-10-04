package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// The live MNQ store had 94 of the 102 closed 4h buckets at boot: Seed set
// e.missing ONCE and nothing ever re-checked it, so the evaluator refused
// every entry until a restart. These pins drive the production shape — Seed
// once, then Tick over the ~2-day live slice — and require the gate to clear
// by itself when the 102nd bucket closes.

// seedRecheckBase finds the first 4h boundary (+5 min) at which the tape holds
// exactly `want` closed 4h candles.
func seedRecheckBase(t *testing.T, tape []market.Kline, want int) int {
	t.Helper()
	for j := 13 * 24 * 60; j < len(tape)-1; j++ {
		ct := time.UnixMilli(tape[j].OpenTime).In(ctime())
		if ct.Minute() != 5 || (ct.Hour()-17+24)%4 != 0 {
			continue
		}
		if n := len(fourHClosedBuckets(barsTF(tape[:j], 60), tape[j].OpenTime)); n == want {
			return j
		}
	}
	t.Fatalf("no seed point with exactly %d closed 4h candles on the tape", want)
	return 0
}

func TestSeedDepthRecheckClearsWithoutRestart(t *testing.T) {
	tape := minuteTape(t, 30)
	j0 := seedRecheckBase(t, tape, 94)
	seedNow := tape[j0].OpenTime

	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	missing := Seed(e, tape[:j0], seedNow)
	if len(missing) != 1 || !strings.Contains(missing[0], "4h EMA34 warm-up (94/102 4h candles)") {
		t.Fatalf("seed at 94 closed 4h candles: missing = %v, want exactly the 4h warm-up (94/102)", missing)
	}

	var clearedAt int64
	met := ""
	// Tick like production: the last ~2 days of 1m bars, one tick per 5 minutes.
	for j := j0 + 1; j < len(tape) && clearedAt == 0; j++ {
		if j%5 != 0 {
			continue
		}
		lo := j - 2*24*60
		e.Tick(tape[lo:j], tape[j-1].OpenTime)
		if line := e.TakeDepthMet(); line != "" {
			met = line
			clearedAt = tape[j-1].OpenTime
		}
		if clearedAt == 0 && len(e.SourcesMissing()) == 0 {
			t.Fatalf("missing cleared without the depth-met line at j=%d", j)
		}
	}
	if clearedAt == 0 {
		t.Fatalf("94 → 102 closed 4h candles never cleared the gate (still missing: %v, depths %v)", e.SourcesMissing(), e.Depths())
	}
	if met != depthMetLine {
		t.Fatalf("depth-met line = %q, want %q", met, depthMetLine)
	}
	if m := e.SourcesMissing(); len(m) != 0 {
		t.Fatalf("after the flip the evaluator still reports missing sources: %v", m)
	}
	if d := e.Depths()["4h EMA34"]; d != FourHEMA34Warmup {
		t.Fatalf("4h depth at the flip = %d, want exactly the %d warm-up (never lowered, never skipped)", d, FourHEMA34Warmup)
	}
	// exactly 8 more closed 4h candles: 8 × 4h after the seed point, to the bucket.
	if got := clearedAt - seedNow; got < 7*4*3600_000 || got > 9*4*3600_000 {
		t.Fatalf("cleared %.1fh after the seed, want ~8 4h candles (28–36h)", float64(got)/3.6e6)
	}
	if again := e.TakeDepthMet(); again != "" {
		t.Fatalf("the depth-met line fired twice: %q", again)
	}
}

// The floor is never lowered: a seed at 93 candles is still refusing after 8
// rollovers (93 + 8 = 101 < 102).
func TestSeedDepthRecheckNeverLowersTheWarmup(t *testing.T) {
	tape := minuteTape(t, 30)
	j0 := seedRecheckBase(t, tape, 93)
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	if m := Seed(e, tape[:j0], tape[j0].OpenTime); len(m) != 1 {
		t.Fatalf("seed at 93: missing = %v", m)
	}
	for j := j0 + 1; j < len(tape) && j <= j0+8*4*60+90; j++ {
		if j%5 != 0 {
			continue
		}
		e.Tick(tape[j-2*24*60:j], tape[j-1].OpenTime)
	}
	if got := e.Depths()["4h EMA34"]; got >= FourHEMA34Warmup {
		t.Fatalf("depth %d reached the warm-up %d inside the window — fixture invalid", got, FourHEMA34Warmup)
	}
	if len(e.SourcesMissing()) == 0 || e.TakeDepthMet() != "" {
		t.Fatalf("the gate cleared below the warm-up: missing %v, depths %v", e.SourcesMissing(), e.Depths())
	}
}
