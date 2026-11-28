package ninjatrader

import (
	"testing"
	"time"

	"vl/kernel"
)

// TestSampleIngestDepthResetsOncePerSessionRoll pins #36 (m220): the ingest
// peak must reset ONCE at the 17:00 CT session roll, not on every enqueue for
// the whole 17:00 hour. The old pattern CompareAndSwap(ingestPeakDay.Load(),
// day) fed its own fresh Load back as the "old" value, so it succeeded on every
// call and zeroed the high-water mark exactly during the reopen burst.
func TestSampleIngestDepthResetsOncePerSessionRoll(t *testing.T) {
	ingestPeakDepth.Store(0)
	ingestPeakDay.Store(0)
	t.Cleanup(func() { ingestPeakDepth.Store(0); ingestPeakDay.Store(0) })

	roll := time.Date(2026, 10, 21, 17, 0, 0, 0, kernel.CTLocation())
	// First call at the roll resets (day changes), then records the peak.
	sampleIngestDepthAt(5, roll)
	if got := ingestPeakDepth.Load(); got != 5 {
		t.Fatalf("peak after first sample = %d, want 5", got)
	}
	// A smaller depth LATER IN THE SAME 17:00 hour must NOT zero the peak.
	sampleIngestDepthAt(3, roll.Add(30*time.Minute))
	if got := ingestPeakDepth.Load(); got != 5 {
		t.Fatalf("peak reset mid-hour: %d, want 5 (the 17:00-hour high-water mark must survive)", got)
	}
	// The NEXT session's roll (17:00 the next day) resets exactly once.
	sampleIngestDepthAt(2, roll.Add(24*time.Hour))
	if got := ingestPeakDepth.Load(); got != 2 {
		t.Fatalf("peak after next-session roll = %d, want 2", got)
	}
}
