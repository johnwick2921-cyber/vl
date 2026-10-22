package ninjatrader

import (
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── release #23 N1 — a history refill is announced to the mentor ────────────
//
// The mentor re-seeds its history-derived state when the MNQ 1m refill
// generation moves (trader/mentor_reseed.go). These pins drive the two
// PRODUCTION moments that put older 1m bars into the store — the replay hold's
// release (replayHold.resolve, on the persister's live frame) and the hole
// tracker's FILLED measure (fillLocked) — and read the generation they bump.
// Mutants: drop either NoteHistoryRefill call → RED.

func TestReplayReleaseAnnouncesTheHistoryRefill(t *testing.T) {
	f := newRHFixture(t)
	onScale := make([]ntwire.Bar, 0, 10)
	for i := int64(5); i < 15; i++ {
		onScale = append(onScale, ntwire.Bar{T: f.t0 + (i+1)*minuteMs, O: rhLiveScale, H: rhLiveScale + 2, L: rhLiveScale - 2, C: rhLiveScale - 1, V: 10})
	}
	f.server = ntwire.NewTCPServer(nil)
	f.server.BarCache().SeedHistorical("MNQ", "1m", onScale)
	backfillBars(f.bh, f.server)
	f.server.BarCache().Upsert("MNQ", "1m", f.liveBar(15, rhLiveScale))
	gen0, _ := HistoryRefillGeneration("MNQ")
	f.resolveAsThePersisterWould()
	if c := f.census(t); c[store.BarSourceHistorical] != 5 {
		t.Fatalf("control: the replay should release 5 historical rows, got %d", c[store.BarSourceHistorical])
	}
	gen1, why := HistoryRefillGeneration("MNQ")
	if gen1 <= gen0 {
		t.Fatalf("a released MNQ 1m replay must move the refill generation (%d → %d)", gen0, gen1)
	}
	if !strings.Contains(why, "replay released 10 MNQ 1m row(s) to the store") {
		t.Fatalf("the refill must name itself (READ values): %q", why)
	}
}

func TestOffScaleReplayAnnouncesNothing(t *testing.T) {
	f := newRHFixture(t)
	backfillBars(f.bh, f.server)
	f.server.BarCache().Upsert("MNQ", "1m", f.liveBar(15, rhLiveScale))
	gen0, _ := HistoryRefillGeneration("MNQ")
	f.resolveAsThePersisterWould()
	if gen1, _ := HistoryRefillGeneration("MNQ"); gen1 != gen0 {
		t.Fatalf("a DISCARDED replay wrote nothing — the generation must stay %d, got %d", gen0, gen1)
	}
}

func TestNoteHistoryRefillOnlyMovesOn1m(t *testing.T) {
	gen0, _ := HistoryRefillGeneration("MNQ")
	NoteHistoryRefill("MNQ", "5m", 10, "x")
	NoteHistoryRefill("MNQ", "1m", 0, "x")
	if g, _ := HistoryRefillGeneration("MNQ"); g != gen0 {
		t.Fatalf("only a non-empty 1m refill moves the generation: %d → %d", gen0, g)
	}
	NoteHistoryRefill("mnq", "1M", 3, "y")
	if g, why := HistoryRefillGeneration("MNQ"); g != gen0+1 || why != "y" {
		t.Fatalf("a 1m refill moves it once (symbol/tf case-insensitive): %d → %d %q", gen0, g, why)
	}
}

func TestHoleFilledAnnouncesTheHistoryRefill(t *testing.T) {
	f := newGHFixture(t)
	f.addon.setAnswer(func(n int, _ ntwire.BarsSubscribePayload) { f.answerFull() })
	det := f.openHole()
	f.tr.tick(det.Add(61 * time.Second))
	f.waitReRequests(1)
	waitFor(t, "NT8's replay to reach the store through the replay-hold", func() bool {
		return f.storedOpen(ct(2026, 10, 14, 3, 44), ct(2026, 10, 14, 7, 48)) == 244
	})
	gen0, _ := HistoryRefillGeneration("MNQ")
	f.tr.tick(det.Add(3*time.Minute + 5*time.Second))
	if hs := f.holes(); len(hs) != 0 {
		t.Fatalf("control: the hole must be closed once the store holds it: %+v\n%s", hs, f.sink.dump())
	}
	gen1, why := HistoryRefillGeneration("MNQ")
	if gen1 <= gen0 || !strings.Contains(why, "bars hole FILLED MNQ 1m 10-14 03:44–07:48 CT") {
		t.Fatalf("the FILLED measure must announce the refill: gen %d → %d, why %q", gen0, gen1, why)
	}
}

// TestRingRehydrateAnnouncesTheHistoryRefill is the m327 (a) pin: the boot RING
// rehydrate reads the STORE's 1m and deepens the ring — the store may hold 1m
// bars the mentor's boot seed could not see (a restart hole healed after the
// seed). It must announce the 1m refill so the mentor tick re-seeds its
// history-derived state (EMA9/34, trigger lines). MUTANT: drop the
// NoteHistoryRefill in rehydrateRingFromStoreWith → the generation stays put →
// RED.
func TestRingRehydrateAnnouncesTheHistoryRefill(t *testing.T) {
	f := newRHFixture(t)
	gen0, _ := HistoryRefillGeneration("MNQ")
	rehydrateRingFromStoreWith(f.bh, f.server, time.Now(), false)
	gen1, why := HistoryRefillGeneration("MNQ")
	if gen1 <= gen0 {
		t.Fatalf("the ring rehydrate must announce the MNQ 1m refill: gen %d → %d", gen0, gen1)
	}
	if !strings.Contains(why, "ring rehydrate") {
		t.Fatalf("the refill must name itself (READ values): %q", why)
	}
}
