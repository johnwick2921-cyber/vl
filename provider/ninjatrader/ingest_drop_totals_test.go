package ninjatrader

import "testing"

// m448: the per-minute summary SWAPS the ingest drop counters to zero, so before
// IngestDropTotals nothing could say how many final frames were lost in a
// session. The totals must (a) survive the summary's swap and (b) include drops
// that have not been summarised yet.
func TestIngestDropTotalsSurviveTheMinuteSwap(t *testing.T) {
	// Isolate from other tests in the package.
	ingestTotMu.Lock()
	ingestFinalDropTotal, ingestHistDropTotal = 0, 0
	ingestTotMu.Unlock()
	ingestFinalDropped.Store(0)
	ingestDropHist.Store(0)
	defer func() {
		ingestTotMu.Lock()
		ingestFinalDropTotal, ingestHistDropTotal = 0, 0
		ingestTotMu.Unlock()
		ingestFinalDropped.Store(0)
		ingestDropHist.Store(0)
	}()

	ingestFinalDropped.Add(2)
	ingestDropHist.Add(5)
	if f, h := IngestDropTotals(); f != 2 || h != 5 {
		t.Fatalf("before any summary: final=%d hist=%d, want 2/5", f, h)
	}

	// The 1-line/minute summary runs: it swaps both counters to zero.
	ingestLastSum.Store(0)
	ingestDropSummary()
	if ingestFinalDropped.Load() != 0 || ingestDropHist.Load() != 0 {
		t.Fatal("test premise: the summary must have swapped the live counters")
	}
	if f, h := IngestDropTotals(); f != 2 || h != 5 {
		t.Fatalf("after the swap: final=%d hist=%d — the summary ate the count, want 2/5", f, h)
	}

	// More drops after the summary add on top.
	ingestFinalDropped.Add(1)
	if f, h := IngestDropTotals(); f != 3 || h != 5 {
		t.Fatalf("swapped + unsummarised: final=%d hist=%d, want 3/5", f, h)
	}
}
