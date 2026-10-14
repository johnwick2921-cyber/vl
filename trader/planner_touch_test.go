package trader

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// F-1 / F-6 (planner-touch-fills, 2026-10-14) at the PRODUCTION call sites, on
// the REAL stored tape (bars MNQ 12-26 1m, read-only DB extract 10-12 17:00 →
// 10-14 10:30 CT) and the REAL plan docs (plans NY 10-14 v2, ASIA 10-13 v3,
// zone_map dropped):
//
//	#653 (armed_orders 366) NY v2 S1 hold, time_hold 3 above OR-L 30506.25,
//	     zone 30509–30517, wait_confirm — live: MET 08:52:36 from closes 40–140
//	     pts above the level, placed 08:56, filled 08:57:18 in a −70 pt dump.
//	#650 (armed_orders 355) ASIA v3 S1 reject at VWAP 30798.92, confirm touch,
//	     NO wait_confirm — live: placed 00:08:36 "beyond", filled 00:09:23 on a
//	     1m candle that closed BELOW the level (30785.75).

type touchPlanFixture struct {
	PlanVersion int             `json:"plan_version"`
	Session     string          `json:"session"`
	TradeDate   string          `json:"trade_date"`
	CreatedAt   string          `json:"created_at"`
	Doc         json.RawMessage `json:"doc"`
}

func touchFixtures(t *testing.T) map[string]touchPlanFixture {
	t.Helper()
	raw, err := os.ReadFile("../kernel/testdata/planner_touch_plans_1013_1014.json")
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]touchPlanFixture
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// touchTape1m loads the stored 1m tape (column 7 = 1 for the NT8 historical
// gap-fill rows 10-14 03:44–07:47; they were in the live cache after the 08:22
// reconnect seed — the persisted decision_records structure proves the 1h read).
func touchTape1m(t *testing.T) []market.Kline {
	t.Helper()
	raw, err := os.ReadFile("../kernel/testdata/mnq1226_1m_20261012_1700_to_20261014_1030ct.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows [][7]float64
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	out := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		o := int64(r[0])
		out = append(out, market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: r[1], High: r[2], Low: r[3], Close: r[4], Volume: r[5]})
	}
	return out
}

// touchNative4h is the NT8 4h series (17:00 CT session grid) rebuilt from the
// stored NATIVE 1h bars (bars tf=1h, MNQ 12-26). The grid is evidenced [A]:
// the NY v2 doc's own structure map (built from the cache's native 4h series)
// carries its 4h last swing low 30591.25 at time_ms 1791900000000 — the close
// instant of this grid's 10-13 05:00–09:00 bar.
func touchNative4h(t *testing.T) []market.Kline {
	t.Helper()
	raw, err := os.ReadFile("../kernel/testdata/mnq1226_4h_nt8grid_from_1h_through_20261014_1030ct.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows [][6]float64
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	out := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		o := int64(r[0])
		out = append(out, market.Kline{OpenTime: o, CloseTime: o + 4*3_600_000 - 1, Open: r[1], High: r[2], Low: r[3], Close: r[4], Volume: r[5]})
	}
	return out
}

func touchCT(t *testing.T, y int, mo time.Month, d, h, mi, s, ms int) time.Time {
	t.Helper()
	return time.Date(y, mo, d, h, mi, s, ms*1_000_000, chicagoLoc())
}

// touchArmTrader installs ONE real plan (fixture key) on a fresh trader whose
// bar provider serves the stored tape AS OF *clock (closed bars only — the
// forming minute's final OHLC is the future).
func touchArmTrader(t *testing.T, key string) (*AutoTrader, *store.Store, *kernel.ActivePlan, *time.Time) {
	t.Helper()
	fx, ok := touchFixtures(t)[key]
	if !ok {
		t.Fatalf("fixture %s missing", key)
	}
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true,
		ConditionStatus: map[string]string{"hold": "live", "reject": "live", "acceptance": "live"}},
		RiskControl: store.RiskControlConfig{MinRiskRewardRatio: 2}}
	// The live "mnq 2" strategy runs ASIA/LONDON enabled (REPORT.md header).
	trueV := true
	cfg.DayPlan.SessionsEnabled = []string{"ASIA", "LONDON", "NY"}
	cfg.DayPlan.Sessions = []store.DayPlanSessionOverride{{Session: "ASIA", Enable: &trueV}, {Session: "LONDON", Enable: &trueV}}
	oneSetupOff(&cfg)
	at, st := resetTrader(t, cfg)
	at.config.NinjaTraderSymbol = "MNQ"
	var doc kernel.PlanDoc
	if err := json.Unmarshal(fx.Doc, &doc); err != nil {
		t.Fatal(err)
	}
	birth, err := time.Parse("2006-01-02 15:04:05.999999999-07:00", fx.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	pid := store.MakePlanIDForTrader(at.id, fx.TradeDate, fx.Session)
	blob, _ := json.Marshal(doc)
	ver, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: fx.TradeDate, Session: fx.Session, StrategyID: at.id, Lifecycle: "active", Doc: string(blob), CreatedAt: birth})
	if err != nil {
		t.Fatal(err)
	}
	plan := &kernel.ActivePlan{Doc: doc, PlanID: pid, Session: fx.Session, Version: ver, BirthMs: birth.UnixMilli()}
	tape := touchTape1m(t)
	clock := new(time.Time)
	old := market.FuturesBarsProvider
	h4 := touchNative4h(t)
	market.FuturesBarsProvider = func(_ string, tf string, n int) []market.Kline {
		if tf == "4h" { // the bar cache's NATIVE 4h series (forming bar included, as live)
			var out []market.Kline
			for _, b := range h4 {
				if b.OpenTime <= clock.UnixMilli() {
					out = append(out, b)
				}
			}
			return out
		}
		if tf != "1m" {
			return nil
		}
		var out []market.Kline
		for _, b := range tape {
			if b.CloseTime < clock.UnixMilli() {
				out = append(out, b)
			}
		}
		if n > 0 && len(out) > n {
			out = out[len(out)-n:]
		}
		return out
	}
	kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{ActivePlan: func(string) *kernel.ActivePlan { return plan }})
	t.Cleanup(func() {
		market.FuturesBarsProvider = old
		kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{})
	})
	return at, st, plan, clock
}

func armRowsAt(t *testing.T, at *AutoTrader, st *store.Store, clock *time.Time, now time.Time) int {
	t.Helper()
	*clock = now
	at.maybeManageArmedOrdersAt(nil, now)
	rows, err := st.ArmedOrders().ListNonTerminal(at.id)
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

// #653: the hold confirm counted closes printed 40–140 pts above the level. At
// every live instant before the fill — MET 08:52:36, placed 08:56:00, filled
// 08:57:18 — the arm must authorize NOTHING (no beyond limit can rest): price
// had not reached the zone (the first closed 1m bar to touch 30506.25–30517 is
// 08:57, closing 08:58:00).
func TestPlannerTouch653HoldWaitsForTheLevel(t *testing.T) {
	at, st, _, clock := touchArmTrader(t, "ny_v2")
	logBuf := captureTraderLog(t)
	for _, now := range []time.Time{touchCT(t, 2026, 10, 14, 8, 52, 36, 81), touchCT(t, 2026, 10, 14, 8, 56, 0, 2), touchCT(t, 2026, 10, 14, 8, 57, 18, 803)} {
		if n := armRowsAt(t, at, st, clock, now); n != 0 {
			t.Fatalf("%s: a hold whose closes printed away from the level must authorize NO arm (live #653 armed 08:52:36, filled 08:57:18): rows=%d\nlog:\n%s",
				now.Format("15:04:05"), n, logBuf.String())
		}
	}
	if !strings.Contains(logBuf.String(), "HELD — a level play waits for its reaction AT the level") ||
		!strings.Contains(logBuf.String(), "waiting for the touch") {
		t.Fatalf("the hold and its reason must be logged:\n%s", logBuf.String())
	}
	raw, _ := st.GetSystemConfig("confirm_waits_for_touch:hold:" + at.id + ":2026-10-14:NY")
	if strings.TrimSpace(raw) != "1" {
		t.Fatalf("the touch hold is counted once per scenario-version: counter=%q", raw)
	}
	if raw, _ := st.GetSystemConfig("sweep_waits_for_trigger:" + at.id + ":2026-10-14:NY"); strings.TrimSpace(raw) != "" {
		t.Fatalf("a hold play is not a sweep setup — the #515 sweep counter must not move: %q", raw)
	}
	// The gate is a wait, not a block: after the 08:57 touch the hold FAILED
	// (closes 08:57–09:01 below 30506.25), then 09:02–09:04 closed back above —
	// three post-touch closes, MET at 09:05:00 by the scenario's own time_hold
	// rule, and the same call site arms. (What stops this long on the 10-14 tape
	// is the HTF veto — TestPlannerTouchHTFVeto653 — and, from 09:10, the
	// existing invalidation leg.)
	if n := armRowsAt(t, at, st, clock, touchCT(t, 2026, 10, 14, 9, 5, 5, 0)); n != 1 ||
		!strings.Contains(logBuf.String(), "wait_confirm MET (time_hold 3min [stored])") {
		t.Fatalf("three closes above the level AFTER the touch are the hold — the gate releases: rows=%d\nlog:\n%s", n, logBuf.String())
	}
}

// #650: a reject with a touch confirm and NO authored wait_confirm. Before F-1
// the gate never looked at its confirm — the market_in_zone limit rested beyond
// the zone at 00:08:36 and filled on the first touch (a candle that CLOSED BELOW
// the level). Now nothing is authorized until a touch candle closes back above
// 30798.92; the first one is the 00:12 bar (close 30815.50), MET at 00:13:00.
func TestPlannerTouch650RejectWaitsForTheRejectingClose(t *testing.T) {
	at, st, _, clock := touchArmTrader(t, "asia_v3")
	logBuf := captureTraderLog(t)
	for _, now := range []time.Time{touchCT(t, 2026, 10, 14, 0, 8, 36, 697), touchCT(t, 2026, 10, 14, 0, 9, 23, 608), touchCT(t, 2026, 10, 14, 0, 12, 30, 0)} {
		if n := armRowsAt(t, at, st, clock, now); n != 0 {
			t.Fatalf("%s: a reject must not rest a beyond limit before a touch candle rejects the level (live #650 rested from 00:08:36, filled 00:09:23): rows=%d\nlog:\n%s",
				now.Format("15:04:05"), n, logBuf.String())
		}
	}
	if !strings.Contains(logBuf.String(), "HELD — a level play waits for its reaction AT the level") {
		t.Fatalf("the hold must be logged:\n%s", logBuf.String())
	}
	// 00:12 is the first touch candle that closes back above (30815.50): the
	// touch gate releases at 00:13:00. On this tape the EXISTING invalidation
	// leg then refuses the arm — the 00:09–00:11 closes below VWAP accepted
	// through it — so #650 is no trade at all, and the reason is named.
	n := armRowsAt(t, at, st, clock, touchCT(t, 2026, 10, 14, 0, 13, 5, 0))
	if !strings.Contains(logBuf.String(), "⚔️ arm S1 leg 1 wait_confirm MET (touch") {
		t.Fatalf("the rejecting touch candle must release the touch gate:\n%s", logBuf.String())
	}
	if n != 0 || !strings.Contains(logBuf.String(), "entry_gate: scenario S1 invalidated") {
		t.Fatalf("on the 10-14 tape the released arm meets the existing invalidation leg: rows=%d\nlog:\n%s", n, logBuf.String())
	}
}

// F-6 — the adherence grade at its PRODUCTION call site
// (recordClosedTradeAnalyticsAt) reads the LOCAL trigger: the one after the
// touch, never the stored verdict from away from the level.
func gradeTouchFill(t *testing.T, key string, entry, exit time.Time, side string, entryPx, exitPx float64) (grade, logText string, at *AutoTrader) {
	t.Helper()
	at, st, plan, clock := touchArmTrader(t, key)
	logBuf := captureTraderLog(t)
	*clock = exit
	pos := &store.TraderPosition{TraderID: at.id, Symbol: "MNQ", Side: side, EntryPrice: entryPx, EntryQuantity: 1,
		EntryTime: entry.UnixMilli(), Source: "armed_entry",
		PlanID: plan.PlanID, PlanVersion: plan.Version, CitedScenarioID: "S1", PlanMatched: true}
	if err := st.Position().Create(pos); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Position().ClosePosition(pos.ID, exitPx, "", (exitPx-entryPx)*2, 0, "sl"); err != nil {
		t.Fatal(err)
	}
	rows, err := st.Position().GetClosedPositions(at.id, 10)
	if err != nil || len(rows) == 0 {
		t.Fatalf("closed position not found: %v", err)
	}
	at.recordClosedTradeAnalyticsAt(exit, rows[0])
	rows, _ = st.Position().GetClosedPositions(at.id, 10)
	return rows[0].AdherenceGrade, logBuf.String(), at
}

func TestPlannerTouch653GradeIsC(t *testing.T) {
	got, log, _ := gradeTouchFill(t, "ny_v2", touchCT(t, 2026, 10, 14, 8, 57, 18, 803), touchCT(t, 2026, 10, 14, 9, 52, 55, 0), "LONG", 30516.25, 30427.25)
	if got != "C" || !strings.Contains(log, "trigger=missed") {
		t.Fatalf("#653 entered before any bar reached its level — the hold had not happened; live graded A trigger=met, must grade C trigger=missed: got %q\nlog:\n%s", got, log)
	}
}

func TestPlannerTouch650GradeTriggerMissed(t *testing.T) {
	entry := touchCT(t, 2026, 10, 14, 0, 9, 23, 608)
	got, log, at := gradeTouchFill(t, "asia_v3", entry, touchCT(t, 2026, 10, 14, 0, 55, 0, 0), "LONG", 30798.75, 30760.5)
	// The base for a missed trigger is C; the killzone step-down that made the
	// live grade B (A − outside a killzone) still applies on top of it.
	inKZ, inNoTrade := kernel.SessionWindowFacts(at.sessionRegistry(entry), entry)
	want, _ := kernel.GradeAdherence(kernel.AdherenceInput{Trigger: kernel.TriggerMissed, Cited: true, Matched: true, InNoTrade: inNoTrade, InKillzone: inKZ})
	if got != want || !strings.Contains(log, "trigger=missed") {
		t.Fatalf("#650 filled on a touch candle that closed THROUGH the level — no rejection yet; live graded B, must grade %q trigger=missed: got %q\nlog:\n%s", want, got, log)
	}
}

// F-4 — the HTF veto at its PRODUCTION call site: the arm pass fed by the
// production snapshot builder (kernel.StructureSnapshotForVeto — the loop, the
// event pass, the strict nudge and the mentor pass all call it) under the LIVE
// mode HTF_VETO_MODE=cross (boot line 10-14 08:22:40 "htf veto: mode=cross").
// Before F-4 the snapshot held 5m/15m/1h only, so cross never found "4h" and
// passed every entry SILENTLY — zero "htf_veto:" refusals in any log since the
// 08-28 switch to cross. On the 10-14 tape the 1h read TRENDING_DOWN from 06:00
// (persisted decision_records 54873–54883) and the native 4h TRENDING_DOWN once
// the 05:00–09:00 bar closed; the NY v2 long's leg (row 366) is released by the
// touch gate at 09:05:00 (post-touch hold) — and must meet the veto there.
func touchVetoPass(t *testing.T, at *AutoTrader, st *store.Store, clock *time.Time, now time.Time) int {
	t.Helper()
	*clock = now
	snap := kernel.StructureSnapshotForVeto(at.futuresSymbol(), market.FuturesBarsProvider(at.futuresSymbol(), kernel.AISVPBarInterval, kernel.AISVPBarCount), now.UnixMilli())
	at.maybeManageArmedOrdersAt(snap, now)
	rows, err := st.ArmedOrders().ListNonTerminal(at.id)
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

func TestPlannerTouchHTFVeto653(t *testing.T) {
	t.Setenv("HTF_VETO_MODE", "cross")
	at, st, _, clock := touchArmTrader(t, "ny_v2")
	logBuf := captureTraderLog(t)
	now := touchCT(t, 2026, 10, 14, 9, 5, 5, 0)
	if n := touchVetoPass(t, at, st, clock, now); n != 0 {
		t.Fatalf("the NY v2 long at 30517 must be VETOED against 1h+4h TRENDING_DOWN (cross): rows=%d\nlog:\n%s", n, logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "HTF veto: htf_veto: long vs 1h TRENDING_DOWN + 4h TRENDING_DOWN (cross-check)") {
		t.Fatalf("the veto must be the logged refusal:\n%s", logBuf.String())
	}
}

// F-1 + F-4 together on the whole NY v2 window: one pass a minute from the
// live MET instant to the plan's DORMANT (09:19:31) — no arm is ever
// authorized, so #653 (−$178) is no trade.
func TestPlannerTouch653NoTradeOnTheTape(t *testing.T) {
	t.Setenv("HTF_VETO_MODE", "cross")
	at, st, _, clock := touchArmTrader(t, "ny_v2")
	logBuf := captureTraderLog(t)
	for now := touchCT(t, 2026, 10, 14, 8, 52, 36, 81); now.Before(touchCT(t, 2026, 10, 14, 9, 19, 31, 0)); now = now.Add(time.Minute) {
		if n := touchVetoPass(t, at, st, clock, now); n != 0 {
			t.Fatalf("%s: #653's arm must never be authorized on the 10-14 tape: rows=%d\nlog:\n%s", now.Format("15:04:05"), n, logBuf.String())
		}
	}
}
