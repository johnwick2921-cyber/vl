package ninjatrader

import "testing"

// m416 B — the ingest queue's eviction must never discard a frame that carries
// the AddOn's one-shot final bar while any non-final frame is queued.

func finalFrame(t int64) []Bar { return []Bar{{T: t, O: 1, H: 2, L: 0.5, C: 1.5, V: 10, Final: true}} }
func formingFrame(t int64) []Bar {
	return []Bar{{T: t, O: 1, H: 2, L: 0.5, C: 1.2, V: 5}}
}

func m416DrainQueue(s *TCPServer) (finals, forming int) {
	for {
		select {
		case m := <-s.barIngestCh:
			if barsCarryFinal(m.bars) {
				finals++
			} else {
				forming++
			}
		default:
			return
		}
	}
}

func TestM416FullQueueNeverDropsAFinal(t *testing.T) {
	t.Setenv("INGEST_QUEUE_CAP", "4")
	resetIngestCounters()
	ingestFinalKept.Store(0)
	ingestFinalDropped.Store(0)
	s := NewTCPServer(nil)
	// Oldest frame is the FINAL; the rest are forming. Pre-fix drop-oldest
	// discarded exactly this one.
	s.enqueueBarUpdate("MNQ", "1m", "MNQ 12-26", finalFrame(60_000))
	for i := 1; i < 4; i++ {
		s.enqueueBarUpdate("MNQ", "1m", "MNQ 12-26", formingFrame(int64(i+1)*60_000))
	}
	// Overload: 6 more forming frames.
	for i := 0; i < 6; i++ {
		s.enqueueBarUpdate("MNQ", "1m", "MNQ 12-26", formingFrame(int64(10+i)*60_000))
	}
	if got := ingestFinalDropped.Load(); got != 0 {
		t.Fatalf("final_dropped = %d, want 0", got)
	}
	if ingestFinalKept.Load() == 0 {
		t.Fatalf("final_kept = 0 — the eviction never had to spare the final, fixture is not exercising B")
	}
	if len(s.barIngestCh) != 4 {
		t.Fatalf("queue depth %d, want the unchanged cap 4", len(s.barIngestCh))
	}
	finals, _ := m416DrainQueue(s)
	if finals != 1 {
		t.Fatalf("queued finals = %d, want 1 — the only final was evicted", finals)
	}
}

// A queue holding ONLY finals: an incoming forming frame is the one lost; an
// incoming final displaces the oldest final and that loss is counted.
func TestM416QueueOfOnlyFinals(t *testing.T) {
	t.Setenv("INGEST_QUEUE_CAP", "3")
	resetIngestCounters()
	ingestFinalKept.Store(0)
	ingestFinalDropped.Store(0)
	s := NewTCPServer(nil)
	for i := 0; i < 3; i++ {
		s.enqueueBarUpdate("MNQ", "1m", "c", finalFrame(int64(i+1)*60_000))
	}
	s.enqueueBarUpdate("MNQ", "1m", "c", formingFrame(9*60_000))
	if ingestFinalDropped.Load() != 0 || ingestDropCur.Load() != 1 {
		t.Fatalf("forming vs all-finals: final_dropped=%d drop_current=%d, want 0/1", ingestFinalDropped.Load(), ingestDropCur.Load())
	}
	s.enqueueBarUpdate("MNQ", "1m", "c", finalFrame(8*60_000))
	if ingestFinalDropped.Load() != 1 {
		t.Fatalf("final_dropped = %d, want 1 (an incoming final displaced the oldest final)", ingestFinalDropped.Load())
	}
	if f, n := m416DrainQueue(s); f != 3 || n != 0 {
		t.Fatalf("queue = %d finals / %d forming, want 3/0", f, n)
	}
}

// m416 D — a FINAL for a minute the ring already holds replaces the forming
// snapshot, volume included, both for the newest bar and for an older one.
func TestM416RingTakesTheFinal(t *testing.T) {
	c := NewBarCache(100)
	const tf = "1m"
	t1, t2 := int64(1_789_000_020_000), int64(1_789_000_080_000)
	c.Upsert("MNQ", tf, []Bar{{T: t1, O: 100, H: 105, L: 99, C: 104, V: 2907}})
	// final arrives while t1 is the NEWEST bar (equal-T branch)
	c.Upsert("MNQ", tf, []Bar{{T: t1, O: 100, H: 106, L: 98, C: 101, V: 12000, Final: true}})
	got := c.Get("MNQ", tf)
	if len(got) != 1 || !got[0].Final || got[0].C != 101 || got[0].V != 12000 || got[0].H != 106 {
		t.Fatalf("final did not replace the newest-bar snapshot: %+v", got)
	}
	// older-bar branch: t2 forming, then boundary frame with t3 forming + t2 final
	t3 := t2 + 60_000
	c.Upsert("MNQ", tf, []Bar{{T: t2, O: 101, H: 103, L: 100, C: 102, V: 500}})
	c.Upsert("MNQ", tf, []Bar{{T: t3, O: 102, H: 102, L: 102, C: 102, V: 1}, {T: t2, O: 101, H: 110, L: 100, C: 109, V: 9000, Final: true}})
	got = c.Get("MNQ", tf)
	if len(got) != 3 || !got[1].Final || got[1].C != 109 || got[1].V != 9000 {
		t.Fatalf("final did not replace an older ring entry: %+v", got)
	}
}
