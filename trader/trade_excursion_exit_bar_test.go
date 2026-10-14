package trader

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
)

// RT-8 (2026-10-14) — trade_excursions.mae_pts left out the EXIT bar.
//
// The bars table holds CLOSED bars only, and the close hook runs seconds after
// the fill — inside the exit minute, before that bar has closed. The row is
// never recomputed after exit_ts is set, so the leg that carried the trade to
// its stop was lost for good: live #653 stored MAE 68.00 against an 89.00-pt
// stop-out, #652 stored 36.75 against 47.25.
//
// The exit minute is bounded by the FILL: the position did not exist after
// it. LONG → entry − max(exitBarLow, exitPx); SHORT mirrored; MFE the same way
// for a target exit. Driven through the production writers (excursionOnClose,
// BackfillExcursions), on a temp store — never the live DB.

// rt8Min is one CT minute on 2026-10-14 (CDT), as epoch ms. The fixtures are
// stated in CT and pinned to the live rows' epoch values below, so the test
// reads the same on a Chicago box and under TZ=UTC.
func rt8Min(h, m int) int64 {
	return time.Date(2026, 10, 14, h, m, 0, 0, kernel.CTLocation()).UnixMilli()
}

type rt8Bar struct {
	h, m int
	hi   float64
	lo   float64
}

// rt8Tape builds 1m bars from (h0:m0) to (h1:m1) inclusive, each neutral
// (hi/lo given), then applies the overrides.
func rt8Tape(h0, m0, h1, m1 int, hi, lo float64, over ...rt8Bar) []store.BarHistoryDB {
	byOpen := map[int64]rt8Bar{}
	for _, o := range over {
		byOpen[rt8Min(o.h, o.m)] = o
	}
	var out []store.BarHistoryDB
	for ms := rt8Min(h0, m0); ms <= rt8Min(h1, m1); ms += 60_000 {
		h, l := hi, lo
		if o, ok := byOpen[ms]; ok {
			h, l = o.hi, o.lo
		}
		out = append(out, store.BarHistoryDB{
			Symbol: "MNQ", TF: "1m", OpenTimeMs: ms, Contract: "MNQ 12-26", Source: store.BarSourceLive,
			O: (h + l) / 2, H: h, L: l, C: (h + l) / 2,
		})
	}
	return out
}

func rt8Store(t *testing.T, bars []store.BarHistoryDB) *store.Store {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.BarHistory().Migrate(); err != nil {
		t.Fatalf("migrate bars: %v", err)
	}
	if err := st.BarHistory().InsertBars(bars); err != nil {
		t.Fatalf("insert bars: %v", err)
	}
	return st
}

type rt8Case struct {
	name                    string
	side                    string
	entryPx, stopPx, tgtPx  float64
	exitPx                  float64
	entryMs, exitMs         int64
	reason                  string
	bars                    []store.BarHistoryDB
	wantMAE, wantMFE        float64
	wantMAETs, wantMFETs    int64   // 0 = not asserted
	wantBarsHeld, wantAmbig int     // -1 = not asserted
	qty                     float64 // 0 = a 1-lot
	maeFloor                bool    // wantMAE is a FLOOR (the last fill), not an exact value
}

// #653 — LONG 1 MNQ filled 30516.25 at 08:57:18.803 CT, stopped 1@30427.25 at
// 09:52:55.036 CT. Tape: the entry bar's high gives the live MFE 26.25, the
// 09:50 low 30448.25 is the stored (wrong) MAE 68.00, and the 09:52 exit bar
// went to 30423.00 — past the fill, so it is capped at 30427.25 → 89.00.
func rt8Pos653() (entryMs, exitMs int64) {
	entryMs = time.Date(2026, 10, 14, 8, 57, 18, 803_000_000, kernel.CTLocation()).UnixMilli()
	exitMs = time.Date(2026, 10, 14, 9, 52, 55, 36_000_000, kernel.CTLocation()).UnixMilli()
	return
}

// #652 — SHORT 1 MNQ filled 30802.00 at 02:26:35.375 CT, stopped 1@30849.25
// at 03:10:47.597 CT. Stored MAE 36.75 (the 03:08 high); the 03:10 exit bar
// high 30852.25 is past the fill → capped at 30849.25 → 47.25.
func rt8Pos652() (entryMs, exitMs int64) {
	entryMs = time.Date(2026, 10, 14, 2, 26, 35, 375_000_000, kernel.CTLocation()).UnixMilli()
	exitMs = time.Date(2026, 10, 14, 3, 10, 47, 597_000_000, kernel.CTLocation()).UnixMilli()
	return
}

func rt8Cases(t *testing.T) []rt8Case {
	t.Helper()
	e653, x653 := rt8Pos653()
	e652, x652 := rt8Pos652()
	// The fixtures ARE the live rows: pin them to trade_excursions.entry_ts /
	// exit_ts of rows 632 (#653) and 631 (#652).
	if e653 != 1791986238803 || x653 != 1791989575036 || e652 != 1791962795375 || x652 != 1791965447597 {
		t.Fatalf("CT fixtures drifted from the live epoch values: 653 %d/%d 652 %d/%d", e653, x653, e652, x652)
	}

	// #653 tape, 08:57 .. 09:51 — 55 closed bars, the exit minute NOT yet in.
	tape653 := func() []store.BarHistoryDB {
		return rt8Tape(8, 57, 9, 51, 30510, 30500,
			rt8Bar{8, 57, 30542.50, 30500}, // entry bar: MFE 26.25
			rt8Bar{9, 50, 30465.75, 30448.25},
			rt8Bar{9, 51, 30469.00, 30450.25})
	}
	tape653Late := func() []store.BarHistoryDB {
		return append(tape653(),
			rt8Tape(9, 52, 9, 53, 0, 0,
				rt8Bar{9, 52, 30453.50, 30423.00}, // the exit bar — low past the fill
				rt8Bar{9, 53, 30433.00, 30417.50})...)
	}
	tape652 := func() []store.BarHistoryDB {
		return rt8Tape(2, 26, 3, 9, 30810, 30795,
			rt8Bar{2, 26, 30805, 30779.75}, // entry bar: MFE 22.25
			rt8Bar{3, 8, 30838.75, 30826.00})
	}
	tape652Late := func() []store.BarHistoryDB {
		return append(tape652(),
			rt8Tape(3, 10, 3, 11, 0, 0,
				rt8Bar{3, 10, 30852.25, 30832.25}, // the exit bar — high past the fill
				rt8Bar{3, 11, 30855.00, 30844.75})...)
	}
	// A LONG target exit on #653's entry: target 30560.00 filled at 09:00:20.
	// The exit bar is what reached the target, so MFE must include it — capped
	// at the fill, not the bar's post-exit high.
	tgtExit := rt8Min(9, 0) + 20_000
	tapeTgt := func() []store.BarHistoryDB {
		return rt8Tape(8, 57, 8, 59, 30530, 30505, rt8Bar{8, 58, 30550, 30520})
	}
	tapeTgtLate := func() []store.BarHistoryDB {
		return append(tapeTgt(), rt8Tape(9, 0, 9, 0, 30571.00, 30515.00)...)
	}
	// C4 pin: an exit bar reaching BOTH the stop and the target stays counted
	// in ambiguous_bars after it is capped at the fill.
	tapeAmbig := func() []store.BarHistoryDB {
		return append(tapeTgt(), rt8Tape(9, 0, 9, 0, 30571.00, 30420.00)...)
	}

	return []rt8Case{
		{
			name: "653 LONG stop — exit bar not yet closed (the live close hook)", side: "LONG",
			entryPx: 30516.25, stopPx: 30427.25, tgtPx: 30720.25, exitPx: 30427.25,
			entryMs: e653, exitMs: x653, reason: "stop", bars: tape653(),
			wantMAE: 89.00, wantMFE: 26.25, wantMAETs: rt8Min(9, 52), wantMFETs: rt8Min(8, 57),
			wantBarsHeld: 55, wantAmbig: 0,
		},
		{
			name: "653 LONG stop — exit bar on the tape (late close): capped at the fill", side: "LONG",
			entryPx: 30516.25, stopPx: 30427.25, tgtPx: 30720.25, exitPx: 30427.25,
			entryMs: e653, exitMs: x653, reason: "stop", bars: tape653Late(),
			wantMAE: 89.00, wantMFE: 26.25, wantMAETs: rt8Min(9, 52), wantMFETs: rt8Min(8, 57),
			wantBarsHeld: 56, wantAmbig: 0,
		},
		{
			name: "652 SHORT stop — exit bar not yet closed", side: "SHORT",
			entryPx: 30802.00, stopPx: 30849.25, tgtPx: 30705.75, exitPx: 30849.25,
			entryMs: e652, exitMs: x652, reason: "stop", bars: tape652(),
			wantMAE: 47.25, wantMFE: 22.25, wantMAETs: rt8Min(3, 10), wantMFETs: rt8Min(2, 26),
			wantBarsHeld: 44, wantAmbig: 0,
		},
		{
			name: "652 SHORT stop — exit bar on the tape: capped at the fill", side: "SHORT",
			entryPx: 30802.00, stopPx: 30849.25, tgtPx: 30705.75, exitPx: 30849.25,
			entryMs: e652, exitMs: x652, reason: "stop", bars: tape652Late(),
			wantMAE: 47.25, wantMFE: 22.25, wantMAETs: rt8Min(3, 10), wantMFETs: rt8Min(2, 26),
			wantBarsHeld: 45, wantAmbig: 0,
		},
		{
			name: "LONG target — exit bar not yet closed: MFE reaches the target", side: "LONG",
			entryPx: 30516.25, stopPx: 30427.25, tgtPx: 30560.00, exitPx: 30560.00,
			entryMs: e653, exitMs: tgtExit, reason: "target", bars: tapeTgt(),
			wantMAE: 11.25, wantMFE: 43.75, wantMFETs: rt8Min(9, 0),
			wantBarsHeld: 3, wantAmbig: 0,
		},
		{
			name: "LONG target — exit bar on the tape: MFE capped at the fill", side: "LONG",
			entryPx: 30516.25, stopPx: 30427.25, tgtPx: 30560.00, exitPx: 30560.00,
			entryMs: e653, exitMs: tgtExit, reason: "target", bars: tapeTgtLate(),
			wantMAE: 11.25, wantMFE: 43.75, wantMFETs: rt8Min(9, 0),
			wantBarsHeld: 4, wantAmbig: 0,
		},
		{
			// A 3-lot all out at the stop: the average IS the fill, and the
			// absent exit minute still gets it folded in.
			name: "653-shaped 3-lot all out at the stop — exit bar not yet closed", side: "LONG",
			entryPx: 30516.25, stopPx: 30427.25, tgtPx: 30720.25, exitPx: 30427.25, qty: 3,
			entryMs: e653, exitMs: x653, reason: "stop", bars: tape653(),
			wantMAE: 89.00, wantMFE: 26.25, wantMAETs: rt8Min(9, 52),
			wantBarsHeld: 55, wantAmbig: 0,
		},
		{
			// #635's shape on #653's tape: 2 out at +23.75, the last 1 stopped at
			// 30427.25 (−89.00). exit_price is the AVERAGE 30502.4167. A stored
			// exit bar must not be cut to that average (MAE 68.00 — below where
			// the last contract went); it keeps the whole bar (93.25, the known
			// post-fill tail) and never reads under the last fill's 89.00.
			name: "3-lot scale-out then stop (averaged exit) — exit bar stored is NOT cut to the average", side: "LONG",
			entryPx: 30516.25, stopPx: 30427.25, tgtPx: 30720.25, exitPx: (2*30540.00 + 30427.25) / 3, qty: 3,
			entryMs: e653, exitMs: x653, reason: "stop", bars: tape653Late(),
			wantMAE: 89.00, maeFloor: true, wantMFE: 26.25,
			wantBarsHeld: 56, wantAmbig: 0,
		},
		{
			name: "exit bar spanning stop AND target keeps its C4 ambiguity count", side: "LONG",
			entryPx: 30516.25, stopPx: 30427.25, tgtPx: 30560.00, exitPx: 30560.00,
			entryMs: e653, exitMs: tgtExit, reason: "target", bars: tapeAmbig(),
			wantMAE: 11.25, wantMFE: 43.75,
			wantBarsHeld: 4, wantAmbig: 1,
		},
	}
}

func rt8Qty(c rt8Case) float64 {
	if c.qty > 0 {
		return c.qty
	}
	return 1
}

func rt8Ts(p *int64) string {
	if p == nil {
		return "NULL"
	}
	return fmt.Sprintf("%d", *p)
}

func rt8Int(p *int) string {
	if p == nil {
		return "NULL"
	}
	return fmt.Sprintf("%d", *p)
}

func rt8Check(t *testing.T, c rt8Case, row *store.TradeExcursion) {
	t.Helper()
	if row == nil {
		t.Fatal("no excursion row")
	}
	if row.MAEPts == nil || row.MFEPts == nil {
		t.Fatalf("mae/mfe NULL (resolution=%q) — the hold is covered", row.Resolution)
	}
	if c.maeFloor {
		if *row.MAEPts < c.wantMAE-1e-9 {
			t.Errorf("mae_pts = %.2f, want >= %.2f (the last fill) — a stored exit bar was cut to the averaged exit %.4f", *row.MAEPts, c.wantMAE, c.exitPx)
		}
	} else if math.Abs(*row.MAEPts-c.wantMAE) > 1e-9 {
		t.Errorf("mae_pts = %.2f, want %.2f (exit bar counted up to the fill %.2f)", *row.MAEPts, c.wantMAE, c.exitPx)
	}
	if math.Abs(*row.MFEPts-c.wantMFE) > 1e-9 {
		t.Errorf("mfe_pts = %.2f, want %.2f", *row.MFEPts, c.wantMFE)
	}
	if c.wantMAETs != 0 && (row.MAETs == nil || *row.MAETs != c.wantMAETs) {
		t.Errorf("mae_ts = %s, want %d", rt8Ts(row.MAETs), c.wantMAETs)
	}
	if c.wantMFETs != 0 && (row.MFETs == nil || *row.MFETs != c.wantMFETs) {
		t.Errorf("mfe_ts = %s, want %d", rt8Ts(row.MFETs), c.wantMFETs)
	}
	if c.wantBarsHeld >= 0 && (row.BarsHeld == nil || *row.BarsHeld != c.wantBarsHeld) {
		t.Errorf("bars_held = %s, want %d (STORED bars only — the fill is a price, not a bar)", rt8Int(row.BarsHeld), c.wantBarsHeld)
	}
	if c.wantAmbig >= 0 && (row.AmbiguousBars == nil || *row.AmbiguousBars != c.wantAmbig) {
		t.Errorf("ambiguous_bars = %s, want %d", rt8Int(row.AmbiguousBars), c.wantAmbig)
	}
}

// The live writer: excursionOnClose, exactly as recordClosedTradeAnalyticsAt
// calls it.
func TestRT8ExcursionCloseCountsTheExitBarUpToTheFill(t *testing.T) {
	for _, c := range rt8Cases(t) {
		c := c
		t.Run(c.name, func(t *testing.T) {
			st := rt8Store(t, c.bars)
			at := mkTrader("ninjatrader", boolp(true), "5m")
			at.store, at.id = st, "t1"
			const posID = 653
			if _, err := st.TradeExcursions().Open(store.TradeExcursion{
				PositionID: posID, Side: c.side, EntryPx: c.entryPx, EntryTs: c.entryMs,
				StopPxInitial: c.stopPx, TargetPx: c.tgtPx, Size: 1, Source: "live",
			}); err != nil {
				t.Fatalf("open excursion: %v", err)
			}
			at.excursionOnClose(&store.TraderPosition{
				ID: posID, TraderID: "t1", Symbol: "MNQ", Side: c.side, Quantity: rt8Qty(c), EntryQuantity: rt8Qty(c),
				EntryPrice: c.entryPx, EntryTime: c.entryMs,
				ExitPrice: c.exitPx, ExitTime: c.exitMs, CloseReason: c.reason, Status: "CLOSED",
			})
			row, err := st.TradeExcursions().GetByPosition(posID)
			if err != nil {
				t.Fatalf("read row: %v", err)
			}
			rt8Check(t, c, row)
		})
	}
}

// The backfill writer: BackfillExcursions reads the SAME tape after the fact,
// when the exit bar IS stored — it must be capped at the fill the same way, so
// a live row and a backfilled row agree.
func TestRT8ExcursionBackfillCapsTheExitBarAtTheFill(t *testing.T) {
	for _, c := range rt8Cases(t) {
		c := c
		if c.wantAmbig > 0 {
			continue // the backfill resolves levels from decisions; none exist here
		}
		t.Run(c.name, func(t *testing.T) {
			st := rt8Store(t, c.bars)
			ps := st.Position()
			p := &store.TraderPosition{
				TraderID: "t1", Symbol: "MNQ", Side: c.side, Quantity: rt8Qty(c), EntryQuantity: rt8Qty(c),
				EntryPrice: c.entryPx, EntryTime: c.entryMs,
				CreatedAt: c.entryMs, UpdatedAt: c.entryMs,
			}
			if err := ps.Create(p); err != nil {
				t.Fatalf("create pos: %v", err)
			}
			if _, err := ps.ClosePosition(p.ID, c.exitPx, "", 0, 0, c.reason); err != nil {
				t.Fatalf("close pos: %v", err)
			}
			// ClosePosition stamps exit_time = now; the fixture's exit is the fill time.
			if err := st.GormDB().Exec("UPDATE trader_positions SET exit_time = ? WHERE id = ?", c.exitMs, p.ID).Error; err != nil {
				t.Fatalf("stamp exit_time: %v", err)
			}
			if _, err := BackfillExcursions(st, "MNQ", "t1",
				time.UnixMilli(c.entryMs-60_000), time.UnixMilli(c.entryMs+60_000)); err != nil {
				t.Fatalf("backfill: %v", err)
			}
			row, err := st.TradeExcursions().GetByPosition(p.ID)
			if err != nil {
				t.Fatalf("read row: %v", err)
			}
			c.wantAmbig = -1 // levels are unknown to the backfill here
			rt8Check(t, c, row)
		})
	}
}
