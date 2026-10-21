package trader

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"vl/market"
	"vl/store"
)

// ── LOG-NOISE-2 (2026-10-08, owner order "check whole scan system") ─────────
//
// Two log floods, fixed with selection/log changes only — NO trading behaviour
// change. (a) a closed TEST-SEAM position (row 572) was re-processed every
// decision cycle because a seam close never receives an adherence grade (the
// grade flag is the idempotency marker), so the loop poll re-ran the excursion
// path and logged three lines per cycle. (b) picture-htf noteSilent keyed on
// stage+"|"+reason with a varying millisecond number in the reason, so every
// frame was a "first occurrence" WARN.

// TestClosedTradeAnalyticsSkipsTestSeamRow — (a): a closed test-seam position is
// skipped BEFORE the excursion path, so its excursion row (entry half) is never
// closed. Named RED: drop the early seam guard → the excursion row gets its exit
// half written (re-processed).
func TestClosedTradeAnalyticsSkipsTestSeamRow(t *testing.T) {
	at := mkTrader("ninjatrader", boolp(true), "5m")
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	at.store = st
	at.id = "t1"

	pos := &store.TraderPosition{
		TraderID: "t1", Symbol: "MNQ", Side: "LONG",
		EntryPrice: 30000, EntryQuantity: 1,
		EntryTime: time.Now().Add(-40 * time.Minute).UnixMilli(),
		Source:    store.CloseReasonTestSeam,
	}
	if err := st.Position().Create(pos); err != nil {
		t.Fatalf("create pos: %v", err)
	}
	if _, err := st.Position().ClosePosition(pos.ID, 30010, "", 1, 0, store.CloseReasonTestSeam); err != nil {
		t.Fatalf("close pos: %v", err)
	}
	// An excursion row with only the entry half (no exit half yet).
	if _, err := st.TradeExcursions().Open(store.TradeExcursion{
		PositionID: pos.ID, EntryPx: pos.EntryPrice, EntryTs: pos.EntryTime,
		Side: "LONG", Source: "live",
	}); err != nil {
		t.Fatalf("open excursion: %v", err)
	}

	prev := market.FuturesBarsProvider
	defer func() { market.FuturesBarsProvider = prev }()
	market.FuturesBarsProvider = func(string, string, int) []market.Kline {
		var bars []market.Kline
		for i := 0; i < 40; i++ {
			ct := time.Now().Add(-time.Duration(40-i) * time.Minute)
			bars = append(bars, market.Kline{OpenTime: ct.UnixMilli(), CloseTime: ct.Add(time.Minute).UnixMilli(), Open: 30000, High: 30015, Low: 29990, Close: 30010})
		}
		return bars
	}

	at.recordClosedTradeAnalyticsAt(time.Now(), pos)

	row, err := st.TradeExcursions().GetByPosition(pos.ID)
	if err != nil {
		t.Fatalf("get excursion: %v", err)
	}
	if row == nil {
		t.Fatal("excursion row must exist (the entry half)")
	}
	if row.ExitTs != nil {
		t.Fatalf("a test-seam position must NOT be re-processed — excursion exit half must stay unset, got exit_ts=%d", *row.ExitTs)
	}
}

// TestExcursionOnCloseIsIdempotent — a position whose excursion row already
// carries its exit half is never re-closed (the "already-excursed" half of the
// fix). Named RED: drop the ExitTs guard → Close rewrites the exit half.
func TestExcursionOnCloseIsIdempotent(t *testing.T) {
	at := mkTrader("ninjatrader", boolp(true), "5m")
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	at.store = st
	at.id = "t1"

	pos := &store.TraderPosition{
		TraderID: "t1", Symbol: "MNQ", Side: "LONG",
		EntryPrice: 30000, EntryQuantity: 1,
		EntryTime: time.Now().Add(-40 * time.Minute).UnixMilli(),
	}
	if err := st.Position().Create(pos); err != nil {
		t.Fatalf("create pos: %v", err)
	}
	if _, err := st.Position().ClosePosition(pos.ID, 30010, "", 1, 0, "oco_target"); err != nil {
		t.Fatalf("close pos: %v", err)
	}
	id, err := st.TradeExcursions().Open(store.TradeExcursion{
		PositionID: pos.ID, EntryPx: pos.EntryPrice, EntryTs: pos.EntryTime,
		Side: "LONG", Source: "live",
	})
	if err != nil {
		t.Fatalf("open excursion: %v", err)
	}
	// Pre-close the excursion row (already excursed).
	if err := st.TradeExcursions().Close(id, store.TradeExcursionClose{
		ExitPx: 30010, ExitTs: time.Now().UnixMilli(), ExitReason: "oco_target",
		StopPxFinal: 29900,
	}); err != nil {
		t.Fatalf("pre-close excursion: %v", err)
	}
	before, _ := st.TradeExcursions().GetByPosition(pos.ID)
	beforeTs := *before.ExitTs

	at.excursionOnClose(pos)

	after, err := st.TradeExcursions().GetByPosition(pos.ID)
	if err != nil || after == nil {
		t.Fatalf("get excursion after: %v %v", after, err)
	}
	if after.ExitTs == nil || *after.ExitTs != beforeTs {
		t.Fatalf("an already-closed excursion row must not be re-closed: before=%d after=%v", beforeTs, after.ExitTs)
	}
}

// TestUngradedClosedPositionsExcludeSeam — the loop poll's SELECTION itself no
// longer returns test-seam rows (they are never gradable, so they would be
// returned forever).
func TestUngradedClosedPositionsExcludeSeam(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	seam := &store.TraderPosition{TraderID: "t1", Symbol: "MNQ", Side: "LONG", EntryPrice: 1, EntryQuantity: 1, Source: store.CloseReasonTestSeam}
	if err := st.Position().Create(seam); err != nil {
		t.Fatalf("create seam: %v", err)
	}
	if _, err := st.Position().ClosePosition(seam.ID, 2, "", 1, 0, store.CloseReasonTestSeam); err != nil {
		t.Fatalf("close seam: %v", err)
	}
	real := &store.TraderPosition{TraderID: "t1", Symbol: "MNQ", Side: "LONG", EntryPrice: 1, EntryQuantity: 1}
	if err := st.Position().Create(real); err != nil {
		t.Fatalf("create real: %v", err)
	}
	if _, err := st.Position().ClosePosition(real.ID, 2, "", 1, 0, "oco_target"); err != nil {
		t.Fatalf("close real: %v", err)
	}
	rows, err := st.Position().GetUngradedClosedPositions("t1", 0, 20)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != real.ID {
		t.Fatalf("selection must return ONLY the real close, got %d rows: %+v", len(rows), rows)
	}
}

// TestNoteSilentDedupesByReasonShape — (b): 50 expiries that differ only in the
// millisecond number collapse to ONE counter (one first-occurrence WARN); the
// counter still advances. Named RED: revert reasonShape → 50 distinct keys.
func TestNoteSilentDedupesByReasonShape(t *testing.T) {
	e := NewPictureHtfEvaluator(nil, store.PictureHtfConfig{})
	for i := 0; i < 50; i++ {
		e.noteSilent("expired", fmt.Sprintf("source age %dms exceeds the freshness limit (360s) — a late frame cannot enter", 60000+i*1000))
	}
	counts := e.WatchReasonCounts()
	if len(counts) != 1 {
		t.Fatalf("50 same-shape expiries must dedupe to ONE counter, got %d: %v", len(counts), counts)
	}
	for _, n := range counts {
		if n != 50 {
			t.Fatalf("the counter must still advance to 50, got %d", n)
		}
	}
	// A DIFFERENT reason shape still gets its own counter.
	e.noteSilent("refused", "no R:R floor resolvable (no strategy config) — fail-closed")
	if len(e.WatchReasonCounts()) != 2 {
		t.Fatalf("a distinct reason shape must keep its own counter, got %d", len(e.WatchReasonCounts()))
	}
}
