package checks

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/kernel"
)

type fakeSrc struct{}

func (fakeSrc) Bars(string, int) ([]Bar, error) { return nil, nil }
func (fakeSrc) Partner(string) string           { return "" }
func (fakeSrc) Open() OpenFn                    { return nil }
func (fakeSrc) Contract(string) (ContractFacts, error) {
	return ContractFacts{Live: "MNQ 12-26", History: "MNQ 12-26", Window: []string{"MNQ 12-26"}}, nil
}

// D6: the swing and levels checks both call SwingInputs - a full store read of
// ~200 four-hour buckets of 1m/5m - on every Evaluate. They share ONE read per
// symbol-minute now; a failed read is never cached.
func TestSwingInputsAreReadOncePerSymbolMinute(t *testing.T) {
	datatruth.Reset()
	defer datatruth.Reset()
	calls := 0
	fail := false
	RegisterAll(Config{Src: fakeSrc{},
		Unrequired: []string{CheckIngestLoss},
		SwingInputs: func(string, time.Time) ([]Bar, []Bar, float64, error) {
			calls++
			if fail {
				return nil, nil, 0, errors.New("store down")
			}
			return nil, nil, 0, nil
		}})
	t0 := time.Date(2026, 10, 27, 14, 3, 5, 0, time.UTC)
	for i := 0; i < 6; i++ { // six Evaluates inside one minute, both checks each
		datatruth.Evaluate("MNQ", t0.Add(time.Duration(i)*time.Second))
	}
	if calls != 1 {
		t.Fatalf("SwingInputs called %d times in one minute for two checks x six evaluations, want 1", calls)
	}
	datatruth.Evaluate("MNQ", t0.Add(time.Minute))
	if calls != 2 {
		t.Fatalf("a new minute reads again: calls=%d", calls)
	}
	fail = true
	datatruth.Evaluate("MNQ", t0.Add(2*time.Minute))
	before := calls
	datatruth.Evaluate("MNQ", t0.Add(2*time.Minute+time.Second))
	if calls == before {
		t.Fatal("a failed read must be retried, not cached")
	}
	b, rs := datatruth.Evaluate("MNQ", t0.Add(2*time.Minute+2*time.Second))
	if !b {
		t.Fatalf("an unreadable store blocks (fail closed): %+v", rs)
	}
}

// D5: a registration whose input is missing is a stub that blocks, not a gap.
func TestRegisterAllWithoutInputsLeavesRequiredChecksBlocking(t *testing.T) {
	datatruth.Reset()
	defer datatruth.Reset()
	RegisterAll(Config{Src: fakeSrc{}}) // no ledger, no verifier, no swing reader
	_, rs := datatruth.Evaluate("MNQ", time.Date(2026, 10, 27, 14, 3, 0, 0, time.UTC))
	got := map[string]bool{}
	for _, r := range rs {
		got[r.Check] = true
	}
	for _, n := range []string{CheckIngestLoss, CheckLookbackSwing4h, CheckLookbackLevels, CheckLookbackSession} {
		if !got[n] {
			t.Errorf("%s must block when its inputs were never wired: %+v", n, rs)
		}
	}
	datatruth.Reset()
	RegisterAll(Config{Src: fakeSrc{}, Unrequired: []string{CheckIngestLoss, CheckLookbackSwing4h, CheckLookbackLevels, CheckLookbackSession}})
	_, rs = datatruth.Evaluate("MNQ", time.Date(2026, 10, 27, 14, 3, 0, 0, time.UTC))
	for _, r := range rs {
		if r.Check == CheckIngestLoss || r.Check == CheckLookbackSwing4h || r.Check == CheckLookbackLevels || r.Check == CheckLookbackSession {
			t.Errorf("explicitly unrequired %s must not block a replay: %+v", r.Check, r)
		}
	}
}

// calSrc is fakeSrc with the real CME calendar.
type calSrc struct{ fakeSrc }

func (calSrc) Open() OpenFn { return func(ms int64) bool { return kernel.IsCMEOpen(time.UnixMilli(ms)) } }

// Release #55 review (swing cache): the swing verdict was cached per 4h bucket,
// so an owner's repair (POST /api/admin/bars/import: "read back within a
// minute") did not lift a blocking verdict for up to four hours. The cache is now
// keyed by a fingerprint of the inputs: a changed store is judged on the next
// read. Production call site: RegisterAll -> datatruth.Evaluate.
//
// MUTANT S1: swingCheck's cache key drops the fingerprint (sig) -> RED.
func TestSwing4hRepairLiftsThePauseWithinTheSameBucket(t *testing.T) {
	datatruth.Reset()
	defer datatruth.Reset()
	loc := kernel.CTLocation()
	now0 := time.Date(2026, 10, 14, 10, 0, 30, 0, loc)
	from := time.Date(2026, 9, 14, 17, 0, 0, 0, loc)
	good1 := dstPerfectBars(from, now0, 1)
	good5 := dstPerfectBars(from, now0, 5)
	// a 4h stretch lost in BOTH frames inside the warm-up (the unrepairable shape)
	holeFrom := time.Date(2026, 10, 7, 13, 0, 0, 0, loc).UnixMilli()
	holeTo := holeFrom + 4*3600_000
	cut := func(bars []Bar) []Bar {
		var o []Bar
		for _, b := range bars {
			if b.T < holeFrom || b.T >= holeTo {
				o = append(o, b)
			}
		}
		return o
	}
	cur1, cur5 := cut(good1), cut(good5)
	RegisterAll(Config{Src: calSrc{}, Unrequired: []string{CheckIngestLoss},
		SwingInputs: func(string, time.Time) ([]Bar, []Bar, float64, error) { return cur1, cur5, 0, nil }})
	swing := func(now time.Time) (bool, string) {
		_, rs := datatruth.Evaluate("MNQ", now)
		for _, r := range rs {
			if r.Check == CheckLookbackSwing4h {
				return true, r.Detail
			}
		}
		return false, ""
	}
	if b, why := swing(now0); !b {
		t.Fatal("fixture: a 4h bucket lost in both frames inside the warm-up must pause SWING4H")
	} else if !strings.Contains(why, "exist in no frame") {
		t.Fatalf("paused for the wrong reason: %s", why)
	}
	// the owner repairs the store; two minutes later, SAME 4h bucket
	cur1, cur5 = good1, good5
	if b, why := swing(now0.Add(2 * time.Minute)); b {
		t.Fatalf("the repair must lift the SWING4H pause on the next read, not at the next 4h bucket: %s", why)
	}
}

// Release #55 review (P3): the prior-session levels check returned CLEAR when
// neither frame held a single bar in the five prior sessions - an unknown read
// as clear. It now blocks while the market is open.
//
// MUTANT L1: levelsCheck falls off the loop with `return false` again -> RED.
func TestLevelsCheckFailsClosedOnAnEmptyStore(t *testing.T) {
	datatruth.Reset()
	defer datatruth.Reset()
	loc := kernel.CTLocation()
	RegisterAll(Config{Src: calSrc{}, Unrequired: []string{CheckIngestLoss},
		SwingInputs: func(string, time.Time) ([]Bar, []Bar, float64, error) { return nil, nil, 0, nil }})
	open := time.Date(2026, 10, 14, 10, 0, 30, 0, loc)
	closed := time.Date(2026, 10, 17, 10, 0, 30, 0, loc) // Saturday
	_, rs := datatruth.Evaluate("MNQ", open)
	got := false
	for _, r := range rs {
		if r.Check == CheckLookbackLevels {
			got = true
			if !strings.Contains(r.Detail, "cannot be verified") {
				t.Errorf("reason: %s", r.Detail)
			}
		}
	}
	if !got {
		t.Fatalf("an empty store with the market open must block the levels check: %+v", rs)
	}
	_, rs = datatruth.Evaluate("MNQ", closed)
	for _, r := range rs {
		if r.Check == CheckLookbackLevels {
			t.Errorf("a closed market asks for no entry: the levels check must not raise on an empty store: %+v", r)
		}
	}
}

// framesSrc serves a fixed bar set per timeframe.
type framesSrc struct {
	calSrc
	bars map[int][]Bar
}

func (f framesSrc) Bars(_ string, tf int) ([]Bar, error) { return f.bars[tf], nil }

// Release #55 review (per-reader holds): the lookback check says WHICH readers a
// hole concerns. A hole in the 3m/5m rings only the AI path's indicators read is
// not a mentor trader's pause; a hole in the 1m ring both read concerns everyone.
//
// MUTANT R2: lookbackCheck returns nil readers for every blocked spec -> RED.
func TestLookbackNamesTheReadersOfTheHole(t *testing.T) {
	loc := kernel.CTLocation()
	now := time.Date(2026, 10, 14, 10, 0, 30, 0, loc)
	from := time.Date(2026, 10, 12, 17, 0, 0, 0, loc)
	full := map[int][]Bar{1: dstPerfectBars(from, now, 1), 3: dstPerfectBars(from, now, 3), 5: dstPerfectBars(from, now, 5)}
	for i := range full[1] { // a choppy tape: a missing minute can matter
		full[1][i].C = 30000 + 3*float64(i%2)
	}
	run := func(bars map[int][]Bar) (blocked bool, rd []string) {
		datatruth.Reset()
		defer datatruth.Reset()
		RegisterAll(Config{Src: framesSrc{bars: bars}, Unrequired: []string{CheckIngestLoss, CheckLookbackSwing4h, CheckLookbackLevels, CheckLookbackSession}})
		_, rs := datatruth.Evaluate("MNQ", now)
		for _, r := range rs {
			if r.Check == CheckLookback {
				return true, r.Readers
			}
		}
		return false, nil
	}
	if b, _ := run(full); b {
		t.Fatal("fixture: perfect frames must not block the lookback")
	}
	// the 3m/5m rings empty (1m intact): only the AI path reads them
	b, rd := run(map[int][]Bar{1: full[1]})
	if !b || len(rd) != 1 || rd[0] != datatruth.ReaderAI {
		t.Fatalf("a hole in the 3m/5m rings concerns the AI reader only: blocked=%v readers=%v", b, rd)
	}
	// a 20-minute hole in the 1m ring: both readers read the 1m EMAs
	var m1 []Bar
	cutFrom := now.Add(-30 * time.Minute).UnixMilli()
	for _, x := range full[1] {
		if x.T >= cutFrom && x.T < cutFrom+20*minuteMs {
			continue
		}
		m1 = append(m1, x)
	}
	b, rd = run(map[int][]Bar{1: m1, 3: full[3], 5: full[5]})
	if !b {
		t.Fatal("a 20-minute hole in the last 30 1m bars must block the 1m EMA9")
	}
	if len(rd) != 0 {
		t.Fatalf("a hole in the 1m ring concerns every reader (the AI path is held to the whole spec list): %v", rd)
	}
}
