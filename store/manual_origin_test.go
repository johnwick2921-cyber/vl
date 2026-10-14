// R22 PNL-TRUTH — owner ruling R1: the four manual NT8 shorts are removed from
// hoang's BOT record by EXCLUSION, never by deletion, and shown as their own
// counted line. Every test here drives the PRODUCTION reader (canon 53).
package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const r1Trader = "8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265"

func newR1Store(t *testing.T) (*Store, *PositionStore) {
	t.Helper()
	st, err := New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("New store: %v", err)
	}
	return st, st.Position()
}

type r1Row struct {
	id       int64
	trader   string
	account  string
	pnl      *float64
	exitMs   int64
	source   string
	planID   string
	entryOID string
	reason   string
	qty      float64
}

func pf(v float64) *float64 { return &v }

func seedR1(t *testing.T, ps *PositionStore, rows []r1Row) {
	t.Helper()
	for _, r := range rows {
		reason := r.reason
		if reason == "" {
			reason = CloseReasonManual
		}
		qty := r.qty
		if qty == 0 {
			qty = 1
		}
		acct := r.account
		if acct == "" {
			acct = "A1"
		}
		p := &TraderPosition{
			ID: r.id, TraderID: r.trader, Account: acct, Symbol: "MNQ", Side: "SHORT",
			Quantity: qty, EntryQuantity: qty, EntryPrice: 30000, ExitPrice: 30000,
			Status: "CLOSED", CloseReason: reason, Source: r.source, PlanID: r.planID,
			EntryOrderID: r.entryOID, PnlCorrected: r.pnl, RealizedPnL: -99999, // raw never read (A22)
			EntryTime: r.exitMs - 500, ExitTime: r.exitMs, CreatedAt: r.exitMs, UpdatedAt: r.exitMs,
		}
		if err := ps.db.Create(p).Error; err != nil {
			t.Fatalf("seed #%d: %v", r.id, err)
		}
	}
}

// the R1 fixture: the four ruled rows with their real P&L, two hoang bot
// trades, one hoang reconcile adoption that is NOT on the owner's list (a
// 1-lot UNRESOLVABLE row like #566 — never marked), and another trader's row.
func r1Fixture() []r1Row {
	return []r1Row{
		{id: 600, trader: r1Trader, pnl: pf(-61.50), exitMs: 1_000, source: "armed_entry", planID: "mentor", entryOID: "sig-600", reason: "stop"},
		{id: 601, trader: r1Trader, pnl: pf(+98.50), exitMs: 2_000, source: "armed_entry", planID: "mentor", entryOID: "sig-601", reason: "target"},
		{id: 566, trader: r1Trader, pnl: pf(+97.00), exitMs: 3_000, source: "reconcile", planID: PlanUnresolvable},
		{id: 608, trader: r1Trader, pnl: pf(119.50), exitMs: 4_000, source: "reconcile", planID: PlanUnresolvable, qty: 40},
		{id: 609, trader: r1Trader, pnl: pf(-2435.00), exitMs: 5_000, source: "reconcile", planID: PlanUnresolvable, qty: 40},
		{id: 637, trader: r1Trader, pnl: pf(241.00), exitMs: 6_000, source: "reconcile", planID: PlanUnresolvable, qty: 20},
		{id: 640, trader: r1Trader, pnl: pf(-1127.50), exitMs: 7_000, source: "reconcile", planID: PlanUnresolvable, qty: 20},
		{id: 700, trader: "other-trader", pnl: pf(-5.00), exitMs: 8_000, source: "reconcile", planID: PlanUnresolvable},
	}
}

func originOf(t *testing.T, ps *PositionStore, id int64) string {
	t.Helper()
	var p TraderPosition
	if err := ps.db.Where("id = ?", id).First(&p).Error; err != nil {
		t.Fatalf("read #%d: %v", id, err)
	}
	return p.Origin
}

// THE MIGRATION marks exactly the four ruled rows, deletes nothing, changes no
// P&L, and a second run marks nothing.
func TestR1MigrationMarksExactlyTheFourRowsAndIsIdempotent(t *testing.T) {
	_, ps := newR1Store(t)
	seedR1(t, ps, r1Fixture())

	pending, err := ps.PendingManualOriginWork()
	if err != nil || pending.Pending != 4 || pending.Already != 0 || pending.Skipped != 0 {
		t.Fatalf("pending before = %+v err=%v, want 4 pending", pending, err)
	}
	c, err := ps.RunManualOriginMigration()
	if err != nil {
		t.Fatalf("migration: %v", err)
	}
	if c.Marked != 4 || len(c.MarkedIDs) != 4 || c.MarkedIDs[0] != 608 || c.MarkedIDs[3] != 640 {
		t.Fatalf("first run = %+v, want exactly 608 609 637 640 marked", c)
	}
	for _, id := range []int64{608, 609, 637, 640} {
		if o := originOf(t, ps, id); o != OriginManual {
			t.Errorf("#%d origin = %q, want manual", id, o)
		}
	}
	for _, id := range []int64{600, 601, 566, 700} {
		if o := originOf(t, ps, id); o != OriginBot {
			t.Errorf("#%d origin = %q, want bot (not on the owner's list)", id, o)
		}
	}
	var n int64
	ps.db.Model(&TraderPosition{}).Count(&n)
	if n != 8 {
		t.Fatalf("rows = %d, want 8 — the migration must never delete", n)
	}
	var p609 TraderPosition
	ps.db.Where("id = ?", 609).First(&p609)
	if p609.PnlCorrected == nil || *p609.PnlCorrected != -2435.00 || p609.CloseReason != CloseReasonManual || p609.PlanID != PlanUnresolvable {
		t.Fatalf("#609 P&L/reason/plan changed: %+v", p609)
	}

	again, err := ps.RunManualOriginMigration()
	if err != nil || again.Marked != 0 || again.Already != 4 || again.Pending != 0 {
		t.Fatalf("second run = %+v err=%v, want 0 marked / 4 already (idempotent)", again, err)
	}
}

// A target id that belongs to ANOTHER trader, or that lost its adoption
// fingerprint, is skipped — never marked (fail-closed).
func TestR1MigrationSkipsWrongTraderAndNonAdoptionRows(t *testing.T) {
	_, ps := newR1Store(t)
	seedR1(t, ps, []r1Row{
		{id: 608, trader: "someone-else", pnl: pf(10), exitMs: 1_000, source: "reconcile", planID: PlanUnresolvable},
		{id: 609, trader: r1Trader, pnl: pf(-20), exitMs: 2_000, source: "armed_entry", planID: "mentor", entryOID: "sig-609"},
	})
	c, err := ps.RunManualOriginMigration()
	if err != nil {
		t.Fatalf("migration: %v", err)
	}
	if c.Marked != 0 || c.Skipped != 4 {
		t.Fatalf("got %+v, want 0 marked / 4 skipped (608 other trader, 609 a bot fill, 637/640 absent)", c)
	}
	if originOf(t, ps, 608) != OriginBot || originOf(t, ps, 609) != OriginBot {
		t.Fatal("a non-target row was marked manual")
	}
}

// THE STATS CALL (GetFullStats — the dashboard stats and the AI track record)
// excludes the manual rows and carries the separate line with the right sum.
func TestGetFullStatsExcludesManualAndShowsSeparateLine(t *testing.T) {
	_, ps := newR1Store(t)
	seedR1(t, ps, r1Fixture())
	if _, err := ps.RunManualOriginMigration(); err != nil {
		t.Fatalf("migration: %v", err)
	}
	stats, err := ps.GetFullStats(r1Trader, "A1")
	if err != nil {
		t.Fatalf("GetFullStats: %v", err)
	}
	// bot = 600 (−61.50) + 601 (+98.50) + 566 (+97.00) = +134.00 over 3
	if stats.TotalTrades != 3 || stats.TotalPnL != 134.00 {
		t.Fatalf("bot record = %+.2f over %d, want +134.00 over 3 (manual rows must be excluded)", stats.TotalPnL, stats.TotalTrades)
	}
	if stats.ManualExcludedTrades != 4 || stats.ManualExcludedPnL != -3202.00 {
		t.Fatalf("manual line = %d trades %+.2f, want 4 trades −3202.00", stats.ManualExcludedTrades, stats.ManualExcludedPnL)
	}
	if stats.ManualExcludedLine != "Manual (excluded): 4 trades, −$3,202.00" {
		t.Fatalf("line = %q", stats.ManualExcludedLine)
	}
}

// Every other bot aggregator applies the same rule.
func TestEveryBotAggregatorExcludesManual(t *testing.T) {
	_, ps := newR1Store(t)
	seedR1(t, ps, r1Fixture())
	if _, err := ps.RunManualOriginMigration(); err != nil {
		t.Fatalf("migration: %v", err)
	}
	if pnl, entries, _ := ps.GetSessionDayActivity(r1Trader, 0, "A1"); pnl != 134.00 || entries != 3 {
		t.Errorf("GetSessionDayActivity = %+.2f / %d entries, want +134.00 / 3", pnl, entries)
	}
	if act, _ := ps.MentorDayActivity(r1Trader, 0, "A1"); act.DayNetPnl != 134.00 {
		t.Errorf("MentorDayActivity net = %+.2f, want +134.00", act.DayNetPnl)
	}
	if day, _ := ps.GetLedgerDayTotal(r1Trader, "A1", 0, 1<<62); day.Total != 134.00 || day.Resolved != 3 || day.ManualTrades != 4 || day.ManualPnL != -3202.00 {
		t.Errorf("GetLedgerDayTotal = %+v, want bot +134.00/3 and manual 4/−3202.00", day)
	}
	if b, err := ps.BotRealizedPnL(r1Trader, 0, "A1"); err != nil || b.Total != 134.00 || b.Resolved != 3 || b.Manual.Trades != 4 || b.Manual.PnL != -3202.00 {
		t.Errorf("BotRealizedPnL = %+v err=%v, want +134.00/3, manual 4/−3202.00", b, err)
	}
	stats, _ := ps.GetPositionStats(r1Trader)
	if stats["total_trades"].(int) != 3 {
		t.Errorf("GetPositionStats total_trades = %v, want 3", stats["total_trades"])
	}
	recent, _ := ps.GetRecentTrades(r1Trader, 50)
	for _, r := range recent {
		if r.ID == 608 || r.ID == 609 || r.ID == 637 || r.ID == 640 {
			t.Errorf("GetRecentTrades (the AI's recent-trade list) carries manual row #%d", r.ID)
		}
	}
	dir, _ := ps.GetDirectionStats(r1Trader)
	for _, d := range dir {
		if d.Side == "SHORT" && d.TradeCount != 3 {
			t.Errorf("GetDirectionStats SHORT = %d trades, want 3", d.TradeCount)
		}
	}
	sym, _ := ps.GetSymbolStats(r1Trader, 10)
	if len(sym) != 1 || sym[0].TotalTrades != 3 {
		t.Errorf("GetSymbolStats = %+v, want 3 MNQ trades", sym)
	}
	hs, _ := ps.GetHistorySummary(r1Trader)
	if hs == nil || hs.TotalTrades != 3 || hs.TotalPnL != 134.00 {
		t.Errorf("GetHistorySummary = %+v, want 3 / +134.00", hs)
	}
}

// The breaker: a manual LOSS is not a bot loss — it neither counts nor breaks
// a run (the test-seam treatment: not a trade the bot made).
func TestConsecutiveLossesIgnoreManualRows(t *testing.T) {
	_, ps := newR1Store(t)
	seedR1(t, ps, []r1Row{
		{id: 1, trader: r1Trader, pnl: pf(-10), exitMs: 1_000, source: "armed_entry", planID: "mentor", entryOID: "s1", reason: "stop"},
		{id: 2, trader: r1Trader, pnl: pf(+500), exitMs: 2_000, source: "reconcile", planID: PlanUnresolvable},
		{id: 3, trader: r1Trader, pnl: pf(-12), exitMs: 3_000, source: "armed_entry", planID: "mentor", entryOID: "s3", reason: "stop"},
	})
	if ok, err := ps.MarkManualOrigin(2, r1Trader); err != nil || !ok {
		t.Fatalf("MarkManualOrigin: ok=%v err=%v", ok, err)
	}
	if n, _ := ps.CountConsecutiveLossesSince(r1Trader, 0); n != 2 {
		t.Fatalf("streak = %d, want 2 (the manual winner between two bot losses is not a bot trade)", n)
	}
	// idempotent, and WHERE-scoped by trader id
	if ok, _ := ps.MarkManualOrigin(2, r1Trader); ok {
		t.Fatal("second MarkManualOrigin reported a write")
	}
	if ok, _ := ps.MarkManualOrigin(3, "someone-else"); ok || originOf(t, ps, 3) != OriginBot {
		t.Fatal("MarkManualOrigin crossed a trader boundary")
	}
}

func TestManualExcludedLineWording(t *testing.T) {
	if got := (ManualExcluded{}).Line(); got != "Manual (excluded): none" {
		t.Fatalf("empty line = %q", got)
	}
	if got := (ManualExcluded{Trades: 1, PnL: 241}).Line(); got != "Manual (excluded): 1 trade, +$241.00" {
		t.Fatalf("one line = %q", got)
	}
	if got := (ManualExcluded{Trades: 2, PnL: -10, Unresolved: 1}).Line(); !strings.Contains(got, "1 unresolved, not summed") {
		t.Fatalf("unresolved line = %q", got)
	}
}

// THE ONE-RULE GUARD (canon 28): every store-side registered P&L surface
// applies the bot-origin predicate. A new aggregator that forgets it — or an
// edit that drops it — fails here, by name.
func TestEveryStorePnLSurfaceAppliesTheBotOriginRule(t *testing.T) {
	for _, sf := range PnLSurfaces() {
		if !strings.HasPrefix(sf.File, "store/") {
			continue // agent/trader surfaces delegate to these and are tested at their call sites
		}
		body := funcBody(t, filepath.Join("..", sf.File), sf.Name)
		if !strings.Contains(body, "BotOriginSQL") {
			t.Errorf("P&L surface %s (%s) does not apply BotOriginSQL — manual NT8 rows would leak into the bot record", sf.Name, sf.File)
		}
	}
}

// funcBody returns the source of one function (from its func line to the
// next top-level func).
func funcBody(t *testing.T, path, name string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	src := string(b)
	i := strings.Index(src, ") "+name+"(")
	if i < 0 {
		i = strings.Index(src, "func "+name+"(")
	}
	if i < 0 {
		t.Fatalf("%s not found in %s", name, path)
	}
	rest := src[i:]
	if j := strings.Index(rest[1:], "\nfunc "); j >= 0 {
		rest = rest[:j+1]
	}
	return rest
}

// The decision-stats opens/closes (GET /api/statistics) count the BOT's
// positions only.
func TestDecisionStatisticsCountBotPositionsOnly(t *testing.T) {
	st, ps := newR1Store(t)
	seedR1(t, ps, r1Fixture())
	if _, err := ps.RunManualOriginMigration(); err != nil {
		t.Fatal(err)
	}
	s, err := st.Decision().GetStatistics(r1Trader, "A1")
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalOpenPositions != 3 || s.TotalClosePositions != 3 {
		t.Fatalf("statistics opens/closes = %d/%d, want 3/3 (4 manual rows excluded)", s.TotalOpenPositions, s.TotalClosePositions)
	}
}
