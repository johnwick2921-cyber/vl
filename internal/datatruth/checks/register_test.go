package checks

import (
	"errors"
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
	for _, n := range []string{CheckIngestLoss, CheckLookbackSwing4h, CheckLookbackLevels} {
		if !got[n] {
			t.Errorf("%s must block when its inputs were never wired: %+v", n, rs)
		}
	}
	datatruth.Reset()
	RegisterAll(Config{Src: fakeSrc{}, Unrequired: []string{CheckIngestLoss, CheckLookbackSwing4h, CheckLookbackLevels}})
	_, rs = datatruth.Evaluate("MNQ", time.Date(2026, 10, 27, 14, 3, 0, 0, time.UTC))
	for _, r := range rs {
		if r.Check == CheckIngestLoss || r.Check == CheckLookbackSwing4h || r.Check == CheckLookbackLevels {
			t.Errorf("explicitly unrequired %s must not block a replay: %+v", r.Check, r)
		}
	}
}
