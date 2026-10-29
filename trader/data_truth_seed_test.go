package trader

import (
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	ntTrader "vl/trader/ninjatrader"
)

// ── #55 m449 — THE DATA-TRUTH CHECKS WAIT FOR THE BOOT SEED ─────────────────
//
// Review #55 (reproduced): the checks are registered (ntTrader.DataTruthReady) a
// few seconds after the wire server exists - BEFORE the AddOn's bars_historical
// replay and the store rehydrate - so the trader asked Evaluate on a ring that
// had not arrived and four checks opened episodes and pushed the owner on every
// restart ("113 of 113 slots missing"). The data was not lost, it was not there
// yet. Now an entry waits - BLOCKED, quietly - until the seed has landed, then
// the checks judge.
//
// The pins use the REAL edge (ntTrader.DataTruthJudgeable, the production
// default of dataTruthReadyFn), not an override, and drive it with the same two
// edges production flips (registered, then seeded).
//
// MUTANTS (shown RED in the report):
//   S1  DataTruthJudgeable returns DataTruthReady() alone (the pre-fix edge)
//                                  -> TestDataTruthWaitsForTheSeedQuietly RED
//   S2  dataTruthStarting ignores the seed edge's grace (always false)
//                                  -> TestDataTruthWaitsForTheSeedQuietly RED
//   S3  rehydrateRingFromStore never marks seeded (ntTrader pin)
//                                  -> TestBootRehydrateOpensTheSeedEdge RED

func armUnseededGate(t *testing.T) (*truthFake, *AutoTrader, *[]string) {
	t.Helper()
	f := armDataTruthGate(t, checks.CheckBadPrint)
	dataTruthReadyFn = ntTrader.DataTruthJudgeable // the production default, un-overridden
	// the registration edge has fired; the seed has NOT landed
	t.Cleanup(ntTrader.SetDataTruthReadyForTest(true))
	t.Cleanup(ntTrader.SetDataTruthSeededForTest(false))
	f.blocked.Store(true) // what an empty, not-yet-arrived ring looks like to a check
	var pushed []string
	datatruth.SetNotifier(func(kind, text string) { pushed = append(pushed, kind) })
	at, _ := pauseTrader(t)
	at.id = "datatruth-seed-1"
	return f, at, &pushed
}

func TestDataTruthWaitsForTheSeedQuietly(t *testing.T) {
	f, at, pushed := armUnseededGate(t)
	wired := time.UnixMilli(dataTruthWiredAt.Load())

	v := at.dataIntegrityVerdictAt(ntTrader.IntegrityDeepOpenMin, wired.Add(30*time.Second), false)
	if !v.Blocked || v.Class != integrityClassDataTruth || !strings.Contains(v.Reason, "still starting") {
		t.Fatalf("registered but not seeded: entries must be BLOCKED as 'starting', got %+v", v)
	}
	at.dataTruthBeat(wired.Add(time.Minute))
	if f.calls.Load() != 0 || len(*pushed) != 0 || datatruth.Snapshot()[checks.CheckBadPrint].Episodes != 0 {
		t.Fatalf("nothing is judged, alerted or episoded before the seed lands: calls=%d pushed=%v", f.calls.Load(), *pushed)
	}

	// the seed lands -> the checks judge, and the owner hears once
	restore := ntTrader.SetDataTruthSeededForTest(true)
	defer restore()
	v = at.dataIntegrityVerdictAt(ntTrader.IntegrityDeepOpenMin, wired.Add(2*time.Minute), false)
	if !v.Blocked || !strings.Contains(v.Reason, "bad_print") {
		t.Fatalf("once seeded the check judges and its reason is named: %+v", v)
	}
	if f.calls.Load() == 0 || len(*pushed) != 1 {
		t.Fatalf("once seeded: judged and ONE episode alert expected, calls=%d pushed=%v", f.calls.Load(), *pushed)
	}
}

// A replay that never lands is not waited on forever: past the grace the checks
// speak (loud, named), so the owner is told rather than left on a silent wait.
func TestDataTruthUnseededPastTheGraceSpeaks(t *testing.T) {
	f, at, pushed := armUnseededGate(t)
	wired := time.UnixMilli(dataTruthWiredAt.Load())
	v := at.dataIntegrityVerdictAt(ntTrader.IntegrityDeepOpenMin, wired.Add(dataTruthBootGrace+time.Minute), false)
	if !v.Blocked || strings.Contains(v.Reason, "still starting") || !strings.Contains(v.Reason, "bad_print") {
		t.Fatalf("past the grace an unseeded ring is judged and named, got %+v", v)
	}
	if f.calls.Load() == 0 || len(*pushed) != 1 {
		t.Fatalf("past the grace: judged + one alert, calls=%d pushed=%v", f.calls.Load(), *pushed)
	}
}
