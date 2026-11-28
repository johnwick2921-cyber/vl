package checks

import (
	"sort"
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
)

type fillFixture struct {
	Obs []struct {
		ID     string  `json:"id"`
		Table  string  `json:"table"`
		Row    int64   `json:"row"`
		Kind   string  `json:"kind"`
		Symbol string  `json:"symbol"`
		Side   string  `json:"side"`
		Price  float64 `json:"price"`
		AtMs   int64   `json:"at_ms"`
		Source string  `json:"source"`
	} `json:"obs"`
	Bars []struct {
		S string  `json:"s"`
		T int64   `json:"t"`
		O float64 `json:"o"`
		H float64 `json:"h"`
		L float64 `json:"l"`
		C float64 `json:"c"`
		V float64 `json:"v"`
	} `json:"bars"`
}

func loadFills(t *testing.T) (obs []FillObs, ring map[string][]Bar, fx fillFixture) {
	t.Helper()
	loadJSON(t, "fills_30d.json", &fx)
	ring = map[string][]Bar{}
	for _, b := range fx.Bars {
		ring[b.S] = append(ring[b.S], Bar{T: b.T, O: b.O, H: b.H, L: b.L, C: b.C, V: b.V, Final: true})
	}
	for _, o := range fx.Obs {
		obs = append(obs, FillObs{ID: o.ID, Symbol: o.Symbol, Kind: o.Kind, Side: o.Side, Price: o.Price, AtMs: o.AtMs, Tick: 0.25})
	}
	for s := range ring {
		b := ring[s]
		sort.Slice(b, func(i, j int) bool { return b[i].T < b[j].T })
	}
	return obs, ring, fx
}

// THE REAL-DATA PROOF. Every fill and entry of the last 30 days (data.db,
// trader_fills 151 exits + trader_positions 95 entries, ids in the fixture; the RECORDED
// close prices are a third source, see the test below) is
// judged against the stored 1m bars of its own minute: NONE is far. A real
// market move cannot fire this check, because the fill printed inside the bars.
func TestFillPriceRealThirtyDaysNeverFires(t *testing.T) {
	obs, ring, _ := loadFills(t)
	if len(obs) != 246 {
		t.Fatalf("fixture: %d obs", len(obs))
	}
	p := DefaultFillParams()
	var far, unjudged []string
	for _, o := range obs {
		v := JudgeFill(o, ring[o.Symbol], p)
		switch {
		case !v.Judged:
			unjudged = append(unjudged, o.ID+": "+v.Detail)
		case v.Far:
			far = append(far, o.ID+": "+v.Detail)
		}
	}
	if len(far) != 0 || len(unjudged) != 0 {
		t.Fatalf("real fills must all be judged and none far: far=%v unjudged=%v", far, unjudged)
	}
	t.Logf("30 days: %d fills judged, 0 far, 0 unjudged (tolerance %v ticks, window -%d/+%d min)", len(obs), p.TolTicks, p.BackMin, p.FwdMin)
}

// The same fills with a corrupted price (a mis-parsed frame, a stale quote, the
// wrong contract) ARE far: the check has teeth. A 300-point error is caught for
// every one; 50 points is caught unless the minute itself was a 50-point range
// (the volatile minutes legitimately contain such a print).
func TestFillPriceCorruptedRealFillsAreFar(t *testing.T) {
	obs, ring, _ := loadFills(t)
	for _, c := range []struct {
		delta float64
		min   int
	}{{300, len(obs)}, {-300, len(obs)}, {50, len(obs) * 90 / 100}, {-50, len(obs) * 90 / 100}} {
		n := 0
		for _, o := range obs {
			o.Price += c.delta
			if v := JudgeFill(o, ring[o.Symbol], DefaultFillParams()); v.Judged && v.Far {
				n++
			}
		}
		t.Logf("price %+.0f points off: %d of %d flagged", c.delta, n, len(obs))
		if n < c.min {
			t.Fatalf("delta %+.0f: only %d of %d corrupted fills flagged (need %d)", c.delta, n, len(obs), c.min)
		}
	}
}

// The tolerance is a margin over measured reality, not decoration: with NO
// tolerance and NO window (fill-minute bar only) real fills do fire - the three
// the report names - so the margin is what keeps real prints legal.
func TestFillPriceToleranceIsLoadBearing(t *testing.T) {
	obs, ring, _ := loadFills(t)
	strict := FillParams{TolTicks: 0.0001, BackMin: 0, FwdMin: 0, SlipTicks: 40, Strikes: 2, PendingMax: time.Minute}
	var ids []string
	for _, o := range obs {
		if v := JudgeFill(o, ring[o.Symbol], strict); v.Judged && v.Far {
			ids = append(ids, o.ID)
		}
	}
	if len(ids) == 0 {
		t.Fatal("expected real fills outside their own minute's bar with zero tolerance (488, 491, 584 exits)")
	}
	t.Logf("zero tolerance, fill-minute bar only: %d real fills would fire: %v", len(ids), ids)
}

func bar(t int64, lo, hi float64, final bool) Bar {
	return Bar{T: t, O: lo, H: hi, L: lo, C: hi, V: 1, Final: final}
}

func TestJudgeFillWaitsForAClosedMinuteThenJudges(t *testing.T) {
	m := int64(1_793_000_000_000) / minuteMs * minuteMs
	o := FillObs{ID: "x", Symbol: "MNQ", Kind: "exit", Price: 29010, AtMs: m + 30_000, Tick: 0.25}
	// forming bar only: not judgeable (its range so far may not hold the print).
	if v := JudgeFill(o, []Bar{bar(m, 29000, 29005, false)}, DefaultFillParams()); v.Judged {
		t.Fatalf("a forming bar must not judge: %+v", v)
	}
	// the minute closed (final): 29010 is 5 above the high -> far (tol 3).
	v := JudgeFill(o, []Bar{bar(m, 29000, 29005, true)}, DefaultFillParams())
	if !v.Judged || !v.Far || !strings.Contains(v.Detail, "above the highest high 29005.00") {
		t.Fatalf("want far: %+v", v)
	}
	// 29007.5 is inside high+3.
	o.Price = 29007.5
	if v := JudgeFill(o, []Bar{bar(m, 29000, 29005, true)}, DefaultFillParams()); !v.Judged || v.Far {
		t.Fatalf("inside tolerance: %+v", v)
	}
}

func TestJudgeFillHoleFallsBackToTheOrderPrice(t *testing.T) {
	m := int64(1_793_000_000_000) / minuteMs * minuteMs
	bars := []Bar{bar(m-minuteMs, 29000, 29005, true), bar(m+minuteMs, 29000, 29005, true)} // minute m missing
	o := FillObs{ID: "h", Symbol: "MNQ", Kind: "entry", Price: 29100, AtMs: m + 10_000, Tick: 0.25, OrderPx: 29000}
	if v := JudgeFill(o, bars, DefaultFillParams()); !v.Judged || !v.Far || !strings.Contains(v.Detail, "from its order price") {
		t.Fatalf("100 pts from the order with no bar to confirm: %+v", v)
	}
	o.Price = 29003
	if v := JudgeFill(o, bars, DefaultFillParams()); !v.Judged || v.Far {
		t.Fatalf("near the order price: %+v", v)
	}
	o.OrderPx, o.Price = 0, 29100
	if v := JudgeFill(o, bars, DefaultFillParams()); v.Judged {
		t.Fatalf("no bar and no order price cannot be judged: %+v", v)
	}
	o.HasSlip, o.SlipTicks = true, 90
	if v := JudgeFill(o, bars, DefaultFillParams()); !v.Judged || !v.Far {
		t.Fatalf("90 ticks of reported slippage with no bar: %+v", v)
	}
}

// testGuardNow is the instant the fixtures live at (2026-10-27 15:00 UTC) and the
// clock newTestGuard installs: the guard's 96 h state age cutoff reads it instead of
// the wall clock, so a state file written by a fixture does not age out as real time
// passes (the #57 time bomb; see TestFillStateAgeCutoffUsesTheGuardClock).
var testGuardNow = time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)

func newTestGuard(session func(time.Time) string) (*FillGuard, *[]string) {
	var alerts []string
	g := NewFillGuard(DefaultFillParams(), session)
	g.SetClock(func() time.Time { return testGuardNow })
	g.SetAlert(func(kind, text string) { alerts = append(alerts, kind) })
	return g, &alerts
}

func ringOf(bars ...Bar) RingFn { return func(string) ([]Bar, error) { return bars, nil } }

// One far fill: ONE alert, the counter, NO pause. The second far fill in the
// same session pauses entries through the datatruth verdict; Ack lifts it; the
// next session lifts it by itself; a replay of the same fill id counts once.
func TestFillGuardOneAlertThenPauseOnTheSecondThenAckOrNextSession(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	sessionOf := func(ts time.Time) string { return ts.UTC().Format("2006-01-02") }
	g, alerts := newTestGuard(sessionOf)
	RegisterFillPrice(g)
	day := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := day.UnixMilli() / minuteMs * minuteMs
	ring := ringOf(bar(m, 29000, 29005, true), bar(m+minuteMs, 29000, 29005, true))
	far := func(id string, atMs int64) FillObs {
		return FillObs{ID: id, Symbol: "MNQ", Kind: "exit", Side: "SELL", Price: 29100, AtMs: atMs, Tick: 0.25}
	}

	g.Record(far("f1", m+5_000), ring, day)
	g.Record(far("f1", m+5_000), ring, day) // replay: same fill
	if len(*alerts) != 1 || g.Stats().Far != 1 {
		t.Fatalf("one far fill = one alert and one count: alerts=%v stats=%+v", *alerts, g.Stats())
	}
	if b, rs := datatruth.Evaluate("MNQ", day); b {
		t.Fatalf("a single far fill must NOT pause entries: %+v", rs)
	}

	g.Record(far("f2", m+20_000), ring, day)
	if len(*alerts) != 2 || g.Stats().Far != 2 {
		t.Fatalf("second far fill alerts too: %v %+v", *alerts, g.Stats())
	}
	b, rs := datatruth.Evaluate("MNQ", day)
	if !b || len(rs) != 1 || rs[0].Check != CheckFillPrice || !strings.Contains(rs[0].Detail, "f1,f2") || !strings.Contains(rs[0].Detail, "/api/risk/ack-fill-price") {
		t.Fatalf("two far fills in a session must pause entries naming both ids: %v %+v", b, rs)
	}
	if b, _ := datatruth.Evaluate("ES", day); b {
		t.Fatal("the pause is per symbol")
	}

	if !g.Ack("MNQ") {
		t.Fatal("Ack must report a standing pause")
	}
	if b, rs := datatruth.Evaluate("MNQ", day.Add(time.Second)); b {
		t.Fatalf("acknowledged: entries resume: %+v", rs)
	}
	if g.Ack("MNQ") {
		t.Fatal("a second Ack has nothing to lift")
	}

	// next session without any ack: a fresh pair of strikes was needed; the old
	// strikes are spent.
	g2, _ := newTestGuard(sessionOf)
	datatruth.Reset()
	RegisterFillPrice(g2)
	g2.Record(far("a1", m+5_000), ring, day)
	g2.Record(far("a2", m+6_000), ring, day)
	if b, _ := datatruth.Evaluate("MNQ", day); !b {
		t.Fatal("paused in session")
	}
	if b, rs := datatruth.Evaluate("MNQ", day.Add(24*time.Hour)); b {
		t.Fatalf("the next session lifts the pause: %+v", rs)
	}
}

// A fill whose minute has not closed WAITS and is judged by Sweep when the bar
// arrives (the forming bar is not evidence); one whose bar never arrives is
// counted unjudged, never silently dropped and never paused on.
func TestFillGuardPendingIsJudgedWhenTheBarClosesAndUnjudgedWhenItNever_Does(t *testing.T) {
	g, alerts := newTestGuard(nil)
	day := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := day.UnixMilli() / minuteMs * minuteMs
	forming := ringOf(bar(m, 29000, 29005, false))
	o := FillObs{ID: "p1", Symbol: "MNQ", Kind: "entry", Price: 29100, AtMs: m + 5_000, Tick: 0.25}
	g.Record(o, forming, day)
	if g.Stats().Pending != 1 || len(*alerts) != 0 {
		t.Fatalf("waiting: %+v %v", g.Stats(), *alerts)
	}
	closed := ringOf(bar(m, 29000, 29005, true))
	g.pending[0].ring = closed // the ring now holds the closed bar
	g.Sweep(day.Add(time.Minute))
	if g.Stats().Pending != 0 || g.Stats().Far != 1 || len(*alerts) != 1 {
		t.Fatalf("judged on sweep: %+v %v", g.Stats(), *alerts)
	}

	o2 := FillObs{ID: "p2", Symbol: "MNQ", Kind: "entry", Price: 29100, AtMs: m + 5_000, Tick: 0.25}
	g.Record(o2, ringOf(), day)
	g.Sweep(day.Add(11 * time.Minute))
	if st := g.Stats(); st.Unjudged != 1 || st.Pending != 0 || st.Far != 1 {
		t.Fatalf("a fill that never gets a bar is counted unjudged: %+v", st)
	}
}

// m447 review defect 2 (mutant: apply() never clears acked). Ack spends the
// strikes seen so far; the NEXT far fill pauses again.
func TestFillGuardAckIsSpentByTheNextFarFill(t *testing.T) {
	day := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := day.UnixMilli() / 60_000 * 60_000
	ring := func(string) ([]Bar, error) {
		return []Bar{{T: m, O: 29000, H: 29005, L: 29000, C: 29005, V: 1, Final: true}, {T: m + 60_000, O: 29000, H: 29005, L: 29000, C: 29005, V: 1, Final: true}}, nil
	}
	g := NewFillGuard(DefaultFillParams(), nil)
	rec := func(id string) {
		g.Record(FillObs{ID: id, Symbol: "MNQ", Kind: "exit", Price: 29100, AtMs: m + 5_000, Tick: 0.25}, ring, day)
	}
	rec("a")
	rec("b")
	if b, _, _ := g.Check()("MNQ", day); !b {
		t.Fatal("2 far fills pause")
	}
	if !g.Ack("MNQ") {
		t.Fatal("ack")
	}
	if b, _, _ := g.Check()("MNQ", day); b {
		t.Fatal("acked: clear")
	}
	if g.Ack("MNQ") {
		t.Fatal("nothing standing: a second ack is a no-op")
	}
	rec("c")
	if b, _, _ := g.Check()("MNQ", day); !b {
		t.Fatal("the next far fill after the ack must pause again")
	}
	if !g.Ack("MNQ") {
		t.Fatal("and it can be acknowledged again")
	}
}

// m447 review defect 6 - the accuracy of "0 far in 30 days". trader_fills exits
// and trader_positions ENTRIES are never far (TestFillPriceRealThirtyDaysNeverFires),
// but the price the bot RECORDED on a closed position (trader_positions.exit_price,
// what P&L reads) is a third source, and exactly two of its 95 rows in the last 30
// days are far from the market of their minute:
//
//	pos:635 (2026-10-07 11:03 CT) exit_price 31299.17 vs stored 1m bars 31258-31273;
//	        the real prints are trader_fills 485/486/487 = 31269.4-31269.5
//	pos:639 (2026-10-08 06:38 CT) exit_price 31297.75; trader_fills 492 = 31272.75
//
// Both recorded prices are wrong (their pnl_corrected was later set from the NT8
// execution receipts) - true positives, not market moves. They are in different
// sessions, so each would have raised ONE alert and NO pause. Every other recorded
// exit is inside its band: a real market move never fires.
func TestFillPriceRecordedExitPricesOfTheLast30DaysFireOnlyTheTwoWrongOnes(t *testing.T) {
	var fx fillFixture
	loadJSON(t, "exits_pos_30d.json", &fx)
	ring := map[string][]Bar{}
	for _, b := range fx.Bars {
		ring[b.S] = append(ring[b.S], Bar{T: b.T, O: b.O, H: b.H, L: b.L, C: b.C, V: b.V, Final: true})
	}
	for s := range ring {
		b := ring[s]
		sort.Slice(b, func(i, j int) bool { return b[i].T < b[j].T })
	}
	if len(fx.Obs) != 95 {
		t.Fatalf("fixture: %d rows", len(fx.Obs))
	}
	var far, unjudged []string
	for _, o := range fx.Obs {
		v := JudgeFill(FillObs{ID: o.ID, Symbol: o.Symbol, Kind: o.Kind, Side: o.Side, Price: o.Price, AtMs: o.AtMs, Tick: 0.25}, ring[o.Symbol], DefaultFillParams())
		switch {
		case !v.Judged:
			unjudged = append(unjudged, o.ID)
		case v.Far:
			far = append(far, o.ID)
		}
	}
	sort.Strings(far)
	t.Logf("recorded exit prices, 95 closed positions: far=%v unjudged=%v", far, unjudged)
	if len(far) != 2 || far[0] != "pos:635" || far[1] != "pos:639" {
		t.Fatalf("exactly the two wrong recorded exits (pos:635, pos:639) are far, got %v", far)
	}
	if len(unjudged) != 0 {
		t.Fatalf("unjudged: %v", unjudged)
	}
}

// m447 review N2: the strike map has ONE canonical key. Fill frames that spell
// the symbol "mnq" or "MNQ 12-26" strike the same entry the gate reads as "MNQ",
// and the owner's acknowledgement (any spelling) lifts it. Mutant: the raw symbol
// as the map key -> the two far fills never meet, no pause (RED).
func TestFillGuardSymbolSpellingsShareOneStrikeCount(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	g, _ := newTestGuard(func(ts time.Time) string { return ts.UTC().Format("2006-01-02") })
	RegisterFillPrice(g)
	day := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := day.UnixMilli() / minuteMs * minuteMs
	ring := ringOf(bar(m, 29000, 29005, true), bar(m+minuteMs, 29000, 29005, true))
	g.Record(FillObs{ID: "s1", Symbol: "mnq", Kind: "exit", Side: "SELL", Price: 29100, AtMs: m + 5_000, Tick: 0.25}, ring, day)
	g.Record(FillObs{ID: "s2", Symbol: "MNQ 12-26", Kind: "entry", Side: "BUY", Price: 29100, AtMs: m + 6_000, Tick: 0.25}, ring, day)
	if b, rs := datatruth.Evaluate("MNQ", day); !b || len(rs) != 1 || rs[0].Check != CheckFillPrice {
		t.Fatalf("two far fills, spelled differently, are one symbol's two strikes: %v %+v", b, rs)
	}
	if !g.Ack(" mnq ") {
		t.Fatal("the acknowledgement canonicalizes its symbol too")
	}
	if b, rs := datatruth.Evaluate("MNQ", day.Add(time.Second)); b {
		t.Fatalf("acknowledged: %+v", rs)
	}
}

// m447 round 3 D2 (mutant MP, fail-OPEN): a far fill that belongs to an OLDER
// session - a late or retried exit receipt, a replay after a reconnect or a
// restart - replaced the whole strikeState (it was keyed by the fill's session),
// and Check then read the new, empty state as "next session: clear". Two far fills
// had paused entries; one late third from yesterday lifted the pause. Through the
// production call site: datatruth.Evaluate over the registered check.
func TestFillGuardLateFillFromAnOlderSessionDoesNotLiftAStandingPause(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	sessionOf := func(ts time.Time) string { return ts.UTC().Format("2006-01-02") }
	g, alerts := newTestGuard(sessionOf)
	RegisterFillPrice(g)
	day := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := day.UnixMilli() / minuteMs * minuteMs
	ringToday := ringOf(bar(m, 29000, 29005, true), bar(m+minuteMs, 29000, 29005, true))
	yesterday := day.Add(-24 * time.Hour)
	my := yesterday.UnixMilli() / minuteMs * minuteMs
	ringYesterday := ringOf(bar(my, 28000, 28005, true), bar(my+minuteMs, 28000, 28005, true))
	far := func(id string, atMs int64, px float64) FillObs {
		return FillObs{ID: id, Symbol: "MNQ", Kind: "exit", Side: "SELL", Price: px, AtMs: atMs, Tick: 0.25}
	}

	g.Record(far("t1", m+5_000, 29100), ringToday, day)
	g.Record(far("t2", m+20_000, 29100), ringToday, day)
	if b, _ := datatruth.Evaluate("MNQ", day); !b {
		t.Fatal("two far fills this session must pause entries")
	}

	// The late receipt: a far fill that printed YESTERDAY, judged now.
	g.Record(far("old1", my+5_000, 28100), ringYesterday, day.Add(time.Minute))
	b, rs := datatruth.Evaluate("MNQ", day.Add(time.Minute))
	if !b || len(rs) != 1 || !strings.Contains(rs[0].Detail, "t1,t2") || strings.Contains(rs[0].Detail, "old1") {
		t.Fatalf("FAIL-OPEN: a late far fill from the previous session must not touch the standing pause (and must not join it): blocked=%v %+v", b, rs)
	}
	if st := g.Stats(); st.Far != 3 || len(*alerts) != 3 {
		t.Fatalf("the late far print is still counted and alerted exactly once: %+v alerts=%v", st, *alerts)
	}
	if last := (*alerts)[2]; last != "data_truth_fill_price_far|MNQ|old1" {
		t.Fatalf("the late fill alerts under its own id: %q", last)
	}

	// An acknowledged pause stays acknowledged: a late old fill is not a new strike.
	if !g.Ack("MNQ") {
		t.Fatal("Ack must lift the standing pause")
	}
	g.Record(far("old2", my+10_000, 28100), ringYesterday, day.Add(2*time.Minute))
	if b, rs := datatruth.Evaluate("MNQ", day.Add(2*time.Minute)); b {
		t.Fatalf("a late old-session fill must not re-pause an acknowledged session: %+v", rs)
	}

	// A fill of the NEXT session still starts fresh strikes (the order is by the
	// fill's time, so the fix does not freeze the state on the first session).
	tomorrow := day.Add(24 * time.Hour)
	mt := tomorrow.UnixMilli() / minuteMs * minuteMs
	ringTomorrow := ringOf(bar(mt, 30000, 30005, true), bar(mt+minuteMs, 30000, 30005, true))
	g.Record(far("n1", mt+5_000, 30100), ringTomorrow, tomorrow)
	if b, _ := datatruth.Evaluate("MNQ", tomorrow); b {
		t.Fatal("one far fill in the new session is a single strike, not a pause")
	}
	g.Record(far("n2", mt+6_000, 30100), ringTomorrow, tomorrow)
	if b, _ := datatruth.Evaluate("MNQ", tomorrow); !b {
		t.Fatal("two far fills in the new session pause it")
	}
}
