package trader

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── MULTI-DAY REPLAY (m186 DS-108, m257) ─────────────────────────────────────
//
// The production mentor path (seed → tick → dispatch → fake AddOn) replayed
// bar-by-bar over the recorded MNQ 10-13 → 10-21 tape, with the broker's
// price-touch fills simulated: a resting entry fills when a bar's extreme
// crosses its trigger, and an open position's bracket stop/target fills when a
// bar's extreme crosses it. Run with MULTI_DAY_REPLAY=1; control vs #45 head is
// two runs of the SAME test at two commits, diffed.
//
// EXIT MODEL (documented approximation): the fake AddOn models ONE bracket per
// position (the harness's single -sl/-tp). The mentor's split legs (leg-1 1:1
// TP + runner trail) are NOT individually modelled — a position closes in full
// at the first stop OR target touch. Entries are EXACT (the ledger); P&L is
// therefore approximate. Every added/dropped trade is still attributed to a
// named reason (the entry or exit rule that changed).

const (
	mdrFixturePath  = "testdata/mnq_1m_2026-10-21_multiday.json.gz"
	mdrCalendarPath = "testdata/multiday_calendar_2026-10-13_10-21.json"
	mdrSeedMs       = int64(1791928800000) // 10-13 17:00 CT
	mdrWindowEndMs  = int64(1792616400000) // 10-21 16:00 CT RTH close
	mdrAddonBuild   = "2026-10-21-c"       // the #43 live AddOn build
	mdrAccount      = "SimReplayM"
	mdrTraderID     = "fdr-md"
	mdrPointValue   = 2.0 // MNQ = $2 / pt
)

// mdrCalendar loads the real calendar slices (10-13 → 10-21) into the replay
// store so the news gates and the 07:30 print windows run on the live schedule.
func (w *fdrWorld) mdrStoreCalendar() {
	w.t.Helper()
	raw, err := os.ReadFile(mdrCalendarPath)
	if err != nil {
		w.t.Fatalf("read calendar fixture: %v", err)
	}
	var slices []struct {
		TradeDate  string `json:"trade_date"`
		EventsJSON string `json:"events_json"`
	}
	if err := json.Unmarshal(raw, &slices); err != nil {
		w.t.Fatalf("parse calendar fixture: %v", err)
	}
	for _, s := range slices {
		row := store.CalendarSliceDB{TradeDate: s.TradeDate, Source: "multiday-replay", EventsJSON: s.EventsJSON}
		if _, err := w.st.Calendar().SaveSliceIfAbsent(&row); err != nil {
			w.t.Fatalf("calendar %s: %v", s.TradeDate, err)
		}
	}
}

// mdrTouchFill plays the broker's price-touch fills on the current bar: resting
// entries fill first (a position filled this bar does not exit this bar), then
// open positions' bracket stop/target fills. Called once per closed 1m bar.
func (a *fdrAddon) mdrTouchFill() {
	b := a.w.bars[a.w.cur.Load()]
	// 1. entry fills — a working entry (not a bracket child) whose trigger the
	// bar's extreme crossed. LONG stop fills on the high; SHORT on the low.
	for acct, book := range a.books {
		for _, o := range book {
			if !o.IsWorking() || isBracketChild(o.Name) || o.StopPrice <= 0 {
				continue
			}
			p, ok := a.signals[o.Name]
			if !ok {
				continue
			}
			long := strings.EqualFold(p.Side, "long")
			if (long && b.High >= o.StopPrice) || (!long && b.Low <= o.StopPrice) {
				a.fill(acct, o.Name, o.StopPrice)
			}
		}
	}
	// 2. bracket exits — the position's -sl stop / -tp limit when the bar's
	// extreme crossed it.
	for acct, pos := range a.positions {
		for _, o := range a.books[acct] {
			if !o.IsWorking() || !strings.HasPrefix(o.Name, pos.signalID) {
				continue
			}
			long := pos.side == "long"
			if strings.HasSuffix(o.Name, "-sl") && o.StopPrice > 0 {
				if (long && b.Low <= o.StopPrice) || (!long && b.High >= o.StopPrice) {
					a.mdrExit(acct, pos, o.StopPrice, "sl")
					break
				}
			}
			if strings.HasSuffix(o.Name, "-tp") && o.LimitPrice > 0 {
				if (long && b.High >= o.LimitPrice) || (!long && b.Low <= o.LimitPrice) {
					a.mdrExit(acct, pos, o.LimitPrice, "tp")
					break
				}
			}
		}
	}
}

// mdrExit closes the whole position at price with reason, the way the real
// AddOn reports a bracket fill (position_close), so close-sync records it.
func (a *fdrAddon) mdrExit(acct string, pos fdrAddonPos, price float64, reason string) {
	kept := a.books[acct][:0]
	for _, o := range a.books[acct] {
		if !strings.HasPrefix(o.Name, pos.signalID) {
			kept = append(kept, o)
		}
	}
	a.books[acct] = kept
	a.w.srv.SeedPositionsForTest(acct, []ntwire.OpenPosition{})
	delete(a.positions, acct)
	traderID := ""
	if p, ok := a.signals[pos.signalID]; ok {
		traderID = p.TraderID
	}
	a.write(ntwire.FramePositionClose, ntwire.PositionClosePayload{
		SignalID: pos.signalID, Symbol: "MNQ", PositionSide: pos.side,
		ExitPrice: price, Quantity: pos.qty, ExitReason: reason,
		ExitTime: time.UnixMilli(a.w.nowMs.Load()).UTC().Format(time.RFC3339),
		Account:  acct, TraderID: traderID, Seq: 0,
	})
	a.awaitClosed(pos.signalID)
}

// mdrTrade is one filled-and-closed trade for the report.
type mdrTrade struct {
	id       int64
	ct       string
	symbol   string
	side     string
	qty      float64
	entry    float64
	exit     float64
	exitPx   float64
	pnl      float64
	reason   string
	scenario string
	status   string
}

// mdrTrades reads the replayed trades from the store (closed + open partials).
func (w *fdrWorld) mdrTrades() []mdrTrade {
	var rows []*store.TraderPosition
	if err := w.st.GormDB().Order("id ASC").Find(&rows).Error; err != nil {
		w.t.Fatalf("read positions: %v", err)
	}
	out := make([]mdrTrade, 0, len(rows))
	for _, p := range rows {
		if p.TraderID != mdrTraderID {
			continue
		}
		exit := p.ExitTime
		if exit == 0 {
			exit = p.UpdatedAt
		}
		out = append(out, mdrTrade{
			id: p.ID, ct: fdrCT(exit), symbol: p.Symbol, side: p.Side,
			qty: p.EntryQuantity, entry: p.EntryPrice, exitPx: p.ExitPrice,
			pnl: p.RealizedPnL, reason: p.CloseReason, scenario: p.CitedScenarioID,
			status: p.Status,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ct < out[j].ct })
	return out
}

// TestMultiDayReplay runs the 10-13 → 10-21 tape through the production mentor
// path and prints the per-day / per-trade table. MULTI_DAY_REPLAY=1 only.
func TestMultiDayReplay(t *testing.T) {
	if os.Getenv("MULTI_DAY_REPLAY") != "1" {
		t.Skip("the multi-day replay (10-13 → 10-21) runs with MULTI_DAY_REPLAY=1")
	}
	oldPath := fdrActiveFixturePath
	fdrActiveFixturePath = mdrFixturePath
	t.Cleanup(func() { fdrActiveFixturePath = oldPath })

	seedMs, endMs := mdrSeedMs, mdrWindowEndMs
	if day := os.Getenv("MDR_DAY"); day != "" {
		// One CME session-day: seed at the PREVIOUS calendar day 17:00 CT, run
		// to this day's 16:00 CT (the full-day replay's seed shape).
		d, err := time.ParseInLocation("2006-01-02", day, kernel.CTLocation())
		if err != nil {
			t.Fatalf("MDR_DAY %q: %v", day, err)
		}
		prev := d.AddDate(0, 0, -1)
		seedMs = time.Date(prev.Year(), prev.Month(), prev.Day(), 17, 0, 0, 0, kernel.CTLocation()).UnixMilli()
		endMs = time.Date(d.Year(), d.Month(), d.Day(), 16, 0, 0, 0, kernel.CTLocation()).UnixMilli()
	}

	w := newFdrWorld(t, seedMs, []string{mdrAccount}, map[string]bool{mdrAccount: true}, mdrAddonBuild)
	w.mdrStoreCalendar()
	cfg := loadReplayStrategy(t)

	start := w.barIndexAtOrAfter(seedMs)
	end := w.barIndexAtOrAfter(endMs)
	w.setNow(seedMs)
	w.cur.Store(int64(start - 1))
	ft := &fdrTrader{id: mdrTraderID, account: mdrAccount, levelPx: map[string]float64{}}
	w.traders = []*fdrTrader{ft}
	w.newFdrTrader(ft, cfg)
	l := &fdrLedger{rows: map[int64]*fdrRowLife{}}
	wall := time.Now()
	for i := start; i < end; i++ {
		w.step(i, []*fdrTrader{ft}, l)
		w.addon.mdrTouchFill()
		w.addon.settle()
	}
	t.Logf("MULTI-DAY replayed %d bars (%s → %s CT) in %s wall",
		end-start, fdrCT(w.bars[start].OpenTime), fdrCT(w.bars[end-1].CloseTime+1), time.Since(wall).Round(time.Second))

	trades := w.mdrTrades()
	t.Logf("TRADES %d:", len(trades))
	day := ""
	for _, tr := range trades {
		d := tr.ct[:5] // MM-DD
		if d != day {
			day = d
			t.Logf("  day %s:", day)
		}
		dir := "+"
		if tr.pnl < 0 {
			dir = ""
		}
		t.Logf("    #%d %s %s %s qty=%.0f entry=%.2f exit=%.2f pnl=%s%.2f reason=%s scenario=%s status=%s",
			tr.id, tr.ct, strings.ToUpper(tr.side), tr.symbol, tr.qty, tr.entry, tr.exitPx, dir, tr.pnl, tr.reason, tr.scenario, tr.status)
	}
	// X8 trap-dodge fires (the new m186 exit).
	snap := ft.at.mentorCountSnapshot()
	t.Logf("COUNTERS x8_trap_dodge_exit=%d x8_trap_dodge_close_sent=%d x8_trap_dodge_close_failed=%d",
		snap["x8_trap_dodge_exit"], snap["x8_trap_dodge_close_sent"], snap["x8_trap_dodge_close_failed"])
	t.Logf("LEDGER rows=%d", len(l.rows))
}
