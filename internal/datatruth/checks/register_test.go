package checks

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vl/internal/datatruth"
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

// m452 (release #55) - SINGLE-FLIGHT. The first Evaluate of each minute pays the
// ~100 ms store read, and N concurrent first callers (three traders on the minute
// edge; the armed pass holding armedPassMu through armAdmitted) each repeated it.
// Driven through RegisterAll + datatruth.Evaluate (the production call path, both
// checks asking per Evaluate).
// MUTANT: cachedInputs without the inflight map -> N reads -> RED.
func TestSwingInputsReadIsSingleFlightAcrossConcurrentCallers(t *testing.T) {
	datatruth.Reset()
	defer datatruth.Reset()
	var calls atomic.Int64
	release := make(chan struct{})
	fail := atomic.Bool{}
	RegisterAll(Config{Src: fakeSrc{},
		Unrequired: []string{CheckIngestLoss},
		SwingInputs: func(string, time.Time) ([]Bar, []Bar, float64, error) {
			calls.Add(1)
			<-release // the slow store read
			if fail.Load() {
				return nil, nil, 0, errors.New("store busy")
			}
			return nil, nil, 0, nil
		}})
	t0 := time.Date(2026, 10, 27, 14, 3, 5, 0, time.UTC)
	const callers = 8
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			datatruth.Evaluate("MNQ", t0.Add(time.Duration(i)*time.Millisecond))
		}(i)
	}
	time.Sleep(150 * time.Millisecond) // everyone is parked behind the one read
	close(release)
	wg.Wait()
	if c := calls.Load(); c != 1 {
		t.Fatalf("%d concurrent first callers of one symbol-minute read the store %d times, want 1 (single-flight)", callers, c)
	}

	// a failed read is shared by the burst's waiters but never cached: the next
	// minute's first caller retries
	fail.Store(true)
	before := calls.Load()
	b, _ := datatruth.Evaluate("MNQ", t0.Add(time.Minute))
	if !b || calls.Load() < before+1 {
		t.Fatalf("an unreadable store still blocks (fail closed) and is read: blocked=%v reads=%d", b, calls.Load()-before)
	}
	mid := calls.Load()
	datatruth.Evaluate("MNQ", t0.Add(time.Minute+time.Second))
	if calls.Load() <= mid {
		t.Fatal("a failed read must not be cached - the next Evaluate retries")
	}
}

// The warmer hook reads the current minute for every symbol already seen, so the
// entry paths find it hot. A symbol never asked about is not read.
// MUTANT: WarmSwingInputs a no-op -> the post-warm Evaluate pays a read -> RED.
func TestWarmSwingInputsMakesTheNextMinuteAHit(t *testing.T) {
	datatruth.Reset()
	defer datatruth.Reset()
	var calls atomic.Int64
	RegisterAll(Config{Src: fakeSrc{},
		Unrequired: []string{CheckIngestLoss},
		SwingInputs: func(string, time.Time) ([]Bar, []Bar, float64, error) {
			calls.Add(1)
			return nil, nil, 0, nil
		}})
	t0 := time.Date(2026, 10, 27, 14, 3, 5, 0, time.UTC)
	datatruth.Evaluate("MNQ", t0)
	if calls.Load() != 1 {
		t.Fatalf("fixture: first Evaluate reads once, got %d", calls.Load())
	}
	WarmSwingInputs(t0.Add(time.Minute))
	if calls.Load() != 2 {
		t.Fatalf("the warmer must read the new minute for the seen symbol: reads=%d", calls.Load())
	}
	datatruth.Evaluate("MNQ", t0.Add(time.Minute+10*time.Second))
	if calls.Load() != 2 {
		t.Fatalf("after the warm the entry path's Evaluate must be a cache hit: reads=%d", calls.Load())
	}
}
