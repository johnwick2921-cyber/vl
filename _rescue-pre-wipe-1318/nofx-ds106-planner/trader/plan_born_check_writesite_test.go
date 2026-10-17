package trader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// W-EXEC-TRUTH W2 A1 + A2 + D5 at the PRODUCTION call site
// (runPlannerReadCoreWithFactsGradesClock → runPlannerReadCoreObserved →
// validateAuthoredScenariosAt). Fixture: plans rowids 455 / 452 and MNQ 1m bars
// open_time_ms 1790144400000..1790146500000, exported read-only
// (kernel/testdata/w2_born_check/, provenance inside). Read clock 01:30:27 CT
// (1790145027000), publish clock 01:51:47 CT (1790146307000).

type w2WriteFixture struct {
	ReadClockMs    int64 `json:"read_clock_ms"`
	PublishClockMs int64 `json:"publish_clock_ms"`
	Rows           map[string]struct {
		Scenarios []struct {
			ID      string `json:"id"`
			Invalid string `json:"invalid"`
		} `json:"scenarios"`
	} `json:"rows"`
}

func loadW2WriteFixture(t *testing.T) (w2WriteFixture, []market.Kline, time.Time, time.Time) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "kernel", "testdata", "w2_born_check", "row455_row452_tape_20260923.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f w2WriteFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	var ohlc struct {
		Bars []struct {
			OpenTimeMs int64   `json:"open_time_ms"`
			O          float64 `json:"o"`
			H          float64 `json:"h"`
			L          float64 `json:"l"`
			C          float64 `json:"c"`
		} `json:"bars_mnq_1m"`
	}
	if err := json.Unmarshal(raw, &ohlc); err != nil {
		t.Fatal(err)
	}
	var bars []market.Kline
	for _, b := range ohlc.Bars {
		if b.OpenTimeMs >= f.PublishClockMs { // the provider at publish holds nothing later
			continue
		}
		bars = append(bars, market.Kline{OpenTime: b.OpenTimeMs, CloseTime: b.OpenTimeMs + 59_999, Open: b.O, High: b.H, Low: b.L, Close: b.C})
	}
	return f, bars, time.UnixMilli(f.ReadClockMs), time.UnixMilli(f.PublishClockMs)
}

// w2Trader: plannerTestTrader + MNQ + ScenarioCap 4 (row 455 has four) + the
// exported tape behind market.FuturesBarsProvider.
func w2Trader(t *testing.T, bars []market.Kline) *AutoTrader {
	t.Helper()
	at := plannerTestTrader(t)
	at.config.NinjaTraderSymbol = "MNQ"
	at.config.StrategyConfig.DayPlan.ScenarioCap = 4
	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return bars }
	t.Cleanup(func() { market.FuturesBarsProvider = old })
	return at
}

// w2Candidate is validTraderPlanJSON (the fixture the whole write chain is
// pinned on) with one S1 clone per invalid sentence and a death line that the
// 31000s tape does not meet unless mutate says otherwise.
func w2Candidate(t *testing.T, invalids []string, mutate func(map[string]any)) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(validTraderPlanJSON), &m); err != nil {
		t.Fatal(err)
	}
	base := m["scenarios"].([]any)[0].(map[string]any)
	var scs []any
	for i, inv := range invalids {
		c := map[string]any{}
		for k, v := range base {
			c[k] = v
		}
		c["id"] = "S" + string(rune('1'+i))
		c["invalid"] = inv
		scs = append(scs, c)
	}
	m["scenarios"] = scs
	m["death"] = map[string]any{"price": 15470.0, "side": "below", "rule": "2x5m"}
	m["death_condition"] = "2x5m close below 15470 ends the plan"
	if mutate != nil {
		mutate(m)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func w2Run(at *AutoTrader, session, date string, read, publish time.Time, replies ...string) (int, string, error, []string) {
	var prompts []string
	ver, lc, err := at.runPlannerReadCoreWithFactsGradesClock(func() time.Time { return publish }, session, date, "", "model", "hash", "", "", "", "FULLPROMPT", kernel.PlanFacts{ReadAt: read}, nil, nil, nil, true, func(p string) (string, error) {
		prompts = append(prompts, p)
		i := len(prompts) - 1
		if i >= len(replies) {
			i = len(replies) - 1
		}
		return replies[i], nil
	})
	return ver, lc, err, prompts
}

var w2Conformant = []string{"5m close above 31095.00", "2x5m close above 31100.00", "5m close below 31000.00", "2x5m close below 30990.00"}

// A1 — row 455's four sentences: ONE rejected attempt, four counted grammar
// refusals, an error quoting all four, attempt 2's repair prompt carrying the
// grammar law, and a conformant attempt 2 publishing with the born-check
// record (read/publish clocks + the four group ids) on the row.
func TestW2Row455GrammarRefusedAtWriteSiteThenConformantPublishes(t *testing.T) {
	f, bars, read, publish := loadW2WriteFixture(t)
	at := w2Trader(t, bars)
	var row455 []string
	for _, s := range f.Rows["455"].Scenarios {
		row455 = append(row455, s.Invalid)
	}
	ver, lc, err, prompts := w2Run(at, "LONDON", "2026-09-23", read, publish, w2Candidate(t, row455, nil), w2Candidate(t, w2Conformant, nil))
	if err != nil || ver != 1 || lc != "active" || len(prompts) != 2 {
		t.Fatalf("want one refused attempt then an active v1: ver=%d lc=%s calls=%d err=%v", ver, lc, len(prompts), err)
	}
	for i, inv := range row455 {
		q, _ := json.Marshal(inv) // %q of these sentences == their JSON string form
		if want := "S" + string(rune('1'+i)) + " invalid " + string(q); !strings.Contains(prompts[1], want) {
			t.Errorf("attempt 2 does not carry the quoted refusal %s", want)
		}
	}
	if !strings.Contains(prompts[1], kernel.AuthoredGrammarRefusalMarker) || !strings.Contains(prompts[1], kernel.RepairInvalidationGrammarLaw) {
		t.Fatalf("attempt 2 (repair) must carry the refusal and the grammar law:\n%s", prompts[1])
	}
	c, cerr := at.store.PlanLivenessCounts()
	if cerr != nil || c.GrammarRefusals != 4 || c.BornDeadRefusals != 0 {
		t.Fatalf("four counted grammar refusals, no born-dead: %+v %v", c, cerr)
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-09-23", "LONDON")
	if row == nil || row.ReadClockMs == nil || *row.ReadClockMs != 1790145027000 || row.PublishClockMs == nil || *row.PublishClockMs != 1790146307000 || row.BornCheck == nil {
		t.Fatalf("published row must carry read/publish clocks + born check: %+v", row)
	}
	var bc kernel.BornCheck
	if json.Unmarshal([]byte(*row.BornCheck), &bc) != nil || bc.Policy != kernel.AuthoredInvalidationPolicy() ||
		len(bc.Groups) != 4 || bc.Groups[0] != 1790145000000 || bc.Groups[3] != 1790145900000 {
		t.Fatalf("born_check groups: %s", *row.BornCheck)
	}
	for _, s := range row455 {
		if strings.Contains(row.Doc, s) {
			t.Fatalf("a refused sentence reached the published doc: %s", s)
		}
	}
}

// A1 — row 452: S2 ("1m close below …") refused and quoted; S1 is conformant
// and, with no tape, tape-UNKNOWN: accepted and counted, never refused.
func TestW2Row452GrammarRefusalAndTapeUnknownAccepted(t *testing.T) {
	f, _, _, _ := loadW2WriteFixture(t)
	at := w2Trader(t, nil) // no tape at all
	s1, s2 := f.Rows["452"].Scenarios[0].Invalid, f.Rows["452"].Scenarios[1].Invalid
	now, _ := time.Parse(time.RFC3339, "2026-09-22T16:36:51-05:00")
	ver, lc, err, prompts := w2Run(at, "ASIA", "2026-09-22", time.Time{}, now,
		w2Candidate(t, []string{s1, s2}, nil), w2Candidate(t, []string{s1, "5m close below 31000.00"}, nil))
	if err != nil || ver != 1 || lc != "active" || len(prompts) != 2 {
		t.Fatalf("ver=%d lc=%s calls=%d err=%v", ver, lc, len(prompts), err)
	}
	q, _ := json.Marshal(s2)
	if !strings.Contains(prompts[1], "S2 invalid "+string(q)) || strings.Contains(prompts[1], "S1 invalid ") {
		t.Fatalf("only S2 is refused, quoted: %s", prompts[1][:600])
	}
	c, _ := at.store.PlanLivenessCounts()
	if c.GrammarRefusals != 1 || c.AuthoredUnknown != 3 { // S1 tape-unknown on attempt 1; S1+S2 on attempt 2
		t.Fatalf("grammar refusal counted once, tape unknowns counted and accepted: %+v", c)
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-09-22", "ASIA")
	if row == nil || row.ReadClockMs != nil || row.PublishClockMs == nil || row.BornCheck == nil || !strings.Contains(*row.BornCheck, `"read_clock_ms":null`) {
		t.Fatalf("legacy facts path: read clock NULL (n/a), publish + record present: %+v", row)
	}
}

// A2 discriminating cases (D2) at the write site: the breach exists only
// BETWEEN read and publish, so a zero read clock (latest-window check)
// publishes attempt 1 and the real read clock refuses it — A2 did the refusing.
// A conformant, unbreached candidate publishes the same doc bytes either way.
func TestW2A2RefusesOnlyWithTheReadClock(t *testing.T) {
	_, bars, read, publish := loadW2WriteFixture(t)
	for _, inv := range []string{"5m close above 31085.00", "2x5m close above 31080.00"} {
		t.Run(inv, func(t *testing.T) {
			cand := w2Candidate(t, []string{inv}, nil)
			fix := w2Candidate(t, []string{"5m close above 31095.00"}, nil)

			at := w2Trader(t, bars)
			ver, _, err, prompts := w2Run(at, "LONDON", "2026-09-23", read, publish, cand, fix)
			if err != nil || ver != 1 || len(prompts) != 2 || !strings.Contains(prompts[1], "born-dead authored scenario: S1") {
				t.Fatalf("with the read clock %q must be refused born-dead first: ver=%d calls=%d err=%v", inv, ver, len(prompts), err)
			}
			if c, _ := at.store.PlanLivenessCounts(); c.BornDeadRefusals != 1 {
				t.Fatalf("born-dead refusal must be counted once: %+v", c)
			}

			legacy := w2Trader(t, bars)
			ver, _, err, prompts = w2Run(legacy, "LONDON", "2026-09-23", time.Time{}, publish, cand, fix)
			if err != nil || ver != 1 || len(prompts) != 1 {
				t.Fatalf("latest-window check (zero read) must accept %q: ver=%d calls=%d err=%v", inv, ver, len(prompts), err)
			}
		})
	}

	a, b := w2Trader(t, bars), w2Trader(t, bars)
	cand := w2Candidate(t, w2Conformant[:1], nil)
	w2Run(a, "LONDON", "2026-09-23", read, publish, cand)
	w2Run(b, "LONDON", "2026-09-23", time.Time{}, publish, cand)
	ra, _ := a.store.Plan().GetLatestPlanForSession("2026-09-23", "LONDON")
	rb, _ := b.store.Plan().GetLatestPlanForSession("2026-09-23", "LONDON")
	if ra == nil || rb == nil || ra.Doc != rb.Doc || !strings.Contains(ra.Doc, `"invalid":"5m close above 31095.00"`) {
		t.Fatalf("a conformant candidate's stored doc must be byte-identical with and without the read clock (the check writes columns, not the doc)")
	}
}

// D5 — row 455's death{2x5m above 31075.75} was met by 01:35 31081.00 + 01:40
// 31090.75 before the plan existed: born-dead refusal, counted with the
// record; and a flip that already fired refuses, naming the flip.
func TestW2DeathAndFlipMetBetweenReadAndPublishRefuse(t *testing.T) {
	_, bars, read, publish := loadW2WriteFixture(t)
	at := w2Trader(t, bars)
	death455 := w2Candidate(t, w2Conformant[:1], func(m map[string]any) {
		m["death"] = map[string]any{"price": 31075.75, "side": "above", "rule": "2x5m"}
		m["death_condition"] = "Two consecutive 5m closes above 31075.75 PDH kill the plan"
	})
	ver, _, err, prompts := w2Run(at, "LONDON", "2026-09-23", read, publish, death455, w2Candidate(t, w2Conformant[:1], nil))
	if err != nil || ver != 1 || len(prompts) != 2 || !strings.Contains(prompts[1], "death{above 31075.75 2x5m} met") || !strings.Contains(prompts[1], "31081.00 (01:35 CT), 31090.75 (01:40 CT)") {
		t.Fatalf("death met between read and publish must refuse attempt 1: ver=%d calls=%d err=%v", ver, len(prompts), err)
	}
	var detail string
	at.store.GormDB().Raw("SELECT value FROM system_config WHERE key LIKE ?", store.LivenessEventPrefix+store.LivenessBornDeadRefusal+":%").Scan(&detail)
	if !strings.Contains(detail, `"read_clock_ms":1790145027000`) || !strings.Contains(detail, `"groups":[1790145000000,1790145300000,1790145600000,1790145900000]`) {
		t.Fatalf("the refused attempt's event must carry the born-check record: %s", detail)
	}

	fl := w2Trader(t, bars)
	flip455 := w2Candidate(t, w2Conformant[:1], func(m map[string]any) {
		m["bias"] = map[string]any{"direction": "short", "conviction": "medium", "flip_condition": "5m close above 31066.32 flips long"}
		m["flip"] = map[string]any{"price": 31066.32, "side": "above", "rule": "5m_close", "flip_to": "long"}
	})
	ver, _, err, prompts = w2Run(fl, "LONDON", "2026-09-23", read, publish, flip455, w2Candidate(t, w2Conformant[:1], nil))
	if err != nil || ver != 1 || len(prompts) != 2 || !strings.Contains(prompts[1], "flip{above 31066.32 5m_close → long} met") || !strings.Contains(prompts[1], "re-author on the flipped side") {
		t.Fatalf("a flip fired between read and publish must refuse, naming the flip: ver=%d calls=%d err=%v", ver, len(prompts), err)
	}
}

// Every attempt refused → the existing fail-closed NO-TRADE row, which records
// no born check (NULL), never a fabricated one.
func TestW2AllAttemptsRefusedFailClosedRowHasNoBornCheck(t *testing.T) {
	f, bars, read, publish := loadW2WriteFixture(t)
	at := w2Trader(t, bars)
	var row455 []string
	for _, s := range f.Rows["455"].Scenarios {
		row455 = append(row455, s.Invalid)
	}
	ver, lc, err, prompts := w2Run(at, "LONDON", "2026-09-23", read, publish, w2Candidate(t, row455, nil))
	if err != nil || ver != 1 || lc != "no_trade" || len(prompts) != 3 {
		t.Fatalf("three refusals must fail closed: ver=%d lc=%s calls=%d err=%v", ver, lc, len(prompts), err)
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-09-23", "LONDON")
	if row == nil || row.ReadClockMs != nil || row.PublishClockMs != nil || row.BornCheck != nil {
		t.Fatalf("fail-closed row must carry NULL born-check columns: %+v", row)
	}

	// A candidate that PASSED the born check on attempt 1 and was then refused
	// by write-time feasibility, followed by two unparseable attempts, fails
	// closed: the NO-TRADE row must not inherit attempt 1's record.
	g := feasPlannerTrader(t, nil) // write-time feasibility ON (owner default)
	feasStubBars(t)
	clock := feasClock()
	// The same composed-R:R-below-minimum candidate TestWriteTimeFeasibilityHintRedToGreen uses.
	red := strings.ReplaceAll(infeasibleFeasPlanJSON, `"target":15620`, `"target":15560`)
	red = strings.Replace(red, `"target_chain": [15550, 15620]`, `"target_chain": [15550, 15560]`, 1)
	red = strings.Replace(red, `"r_to_arm_target":7.0`, `"r_to_arm_target":1.0`, 1)
	var calls int
	ver, lc, err = g.runPlannerReadCoreWithFactsGradesClock(clock, "NY", "2026-08-14", "", "model", "hash", "", "", "", "FULLPROMPT",
		kernel.PlanFacts{Price: 15550, DATR: 300, ReadAt: clock().Add(-time.Minute)}, nil, map[float64]string{15480: "PWL", 15620: "PDH"}, nil, true,
		func(string) (string, error) {
			calls++
			if calls == 1 {
				return red, nil
			}
			return "not json", nil
		})
	if err != nil || ver != 1 || lc != "no_trade" || calls != 3 {
		t.Fatalf("feasibility-refused then unparseable must fail closed: ver=%d lc=%s calls=%d err=%v", ver, lc, calls, err)
	}
	row, _ = g.store.Plan().GetLatestPlanForSession("2026-08-14", "NY")
	if row == nil || row.ReadClockMs != nil || row.PublishClockMs != nil || row.BornCheck != nil {
		t.Fatalf("a NO-TRADE row must not carry the refused candidate's born check: %+v", row)
	}
}

// The shadow A/B replays the live chain; without the born check its legal
// rate would overstate (map note, rootfix_shadow_ab.go).
func TestW2ShadowVerdictIncludesBornCheck(t *testing.T) {
	f, bars, read, _ := loadW2WriteFixture(t)
	at := w2Trader(t, bars)
	ok, reasons := at.shadowVerdictFor(w2Candidate(t, []string{f.Rows["455"].Scenarios[3].Invalid}, nil), 8, 4, kernel.PlanFacts{ReadAt: read}, nil, nil, "")
	if ok || len(reasons) == 0 || !strings.Contains(strings.Join(reasons, " "), kernel.AuthoredGrammarRefusalMarker) {
		t.Fatalf("shadow must see the grammar refusal: ok=%v %v", ok, reasons)
	}
}

// The LIVE read path (runPlannerReadWithTriggerClaimedCtx → the facts built
// from the assembled input) carries the read clock: the row's read_clock_ms is
// input.Now — set before the model call, never after publication.
func TestW2LiveReadStampsTheReadClock(t *testing.T) {
	at, st := class35Trader(t, 4)
	before := time.Now()
	if !at.runPlannerReadWithTriggerClaimedCtx("NY", "2026-09-01", "owner_reset", "", nil, true) {
		t.Fatal("read did not run")
	}
	after := time.Now()
	row := latestRow(t, st, "2026-09-01", "NY")
	if row.ReadClockMs == nil || row.PublishClockMs == nil || row.BornCheck == nil {
		t.Fatalf("a live read must record its read clock, publish clock and born check: %+v", row)
	}
	r, p := *row.ReadClockMs, *row.PublishClockMs
	if r < before.UnixMilli() || r > p || p > after.UnixMilli() {
		t.Fatalf("read clock %d must be the assembly clock: before %d ≤ read ≤ publish %d ≤ after %d", r, before.UnixMilli(), p, after.UnixMilli())
	}
}
