package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/logger"
	"vl/market"
	"vl/store"
	"vl/trader"
)

const fixtureContract = "MNQ 12-26"

// loadFixture reads the stored-day fixture (exported READ-ONLY from data.db:
// MNQ 12-26 1m, 2026-11-05 17:00 CT → 2026-11-10 10:29 CT; columns
// open_time_ms,o,h,l,c,v,source,convention) into the tool's in-memory bar set.
func loadFixture(t *testing.T) *barSet {
	t.Helper()
	f, err := os.Open("testdata/mnq_12-26_1m_20261106-20261110.csv.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var rows []row
	sc := bufio.NewScanner(gz)
	for sc.Scan() {
		p := strings.Split(sc.Text(), ",")
		if len(p) != 8 {
			t.Fatalf("bad fixture line %q", sc.Text())
		}
		var r row
		r.T, _ = strconv.ParseInt(p[0], 10, 64)
		r.O, _ = strconv.ParseFloat(p[1], 64)
		r.H, _ = strconv.ParseFloat(p[2], 64)
		r.L, _ = strconv.ParseFloat(p[3], 64)
		r.C, _ = strconv.ParseFloat(p[4], 64)
		r.V, _ = strconv.ParseFloat(p[5], 64)
		r.Src, r.Conv = p[6], p[7]
		rows = append(rows, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) < 3000 {
		t.Fatalf("fixture too short: %d rows", len(rows))
	}
	return &barSet{byTF: map[string]map[string][]row{"1m": {fixtureContract: rows}}}
}

var testCap = func() *capture {
	c := &capture{}
	logger.Log.SetOutput(io.Discard)
	logger.Log.AddHook(c)
	return c
}()

// comparable drops what legitimately differs between the two drivers: the
// wall-clock latency the trader prints in a few lines ("[N ms]").
//
// Within ONE tick the logged lines are compared as a set: the evaluator emits
// some notes from a map walk (kernel/mentor/box_validity.go:123, the carried-box
// expiry loop over State.CarriedBoxes), so their order is not stable from run
// to run in production either — only their content is.
func comparable(evs []Event) []string {
	out := make([]string, 0, len(evs))
	start := 0
	flush := func(end int) {
		sort.Strings(out[start:end])
		start = end
	}
	prevTick := ""
	for _, ev := range evs {
		if m, ok := ev["msg"].(string); ok {
			ev["msg"] = msRe.ReplaceAllString(m, "")
		}
		tick := fmt.Sprint(ev["t"], "|", ev["bar"], "|", ev["kind"] == "log")
		if tick != prevTick {
			flush(len(out))
			prevTick = tick
		}
		js, _ := json.Marshal(ev)
		out = append(out, string(js))
	}
	flush(len(out))
	return out
}

// TestReplayMatchesLivePath — PARITY. The tool's replay of a stored day (the
// as-of 1m window handed straight to the production tick, clock pinned to the
// bar close) must produce the SAME detector events, intents, refusals and
// logged lines as the LIVE entry: the event-loop pass (mentorEventPassAt),
// which reads the bar cache through market.FuturesBarsProvider — closed bars
// PLUS the forming bar NT8 is still building — drops the forming tail
// (mentorClosedBars) and runs with the wall clock a little after the close.
// Both are seeded by the production seed body at the live boot instant of
// 2026-11-10 (08:15:25 CT) and run to 10:29 CT, through two 5m-ISB-box
// replacements and their break locks (the live log's lines for that window).
//
// MUTANT (run by hand 2026-11-10): the replay ticks a newest bar whose close is
// its open (a half-built candle, the defect mentorClosedBars exists for) → event
// 6 differs (isb_tf_conflict vs level_invalid_first_touch on the 08:15 bar) → RED.
func TestReplayMatchesLivePath(t *testing.T) {
	bs := loadFixture(t)
	loc := kernel.CTLocation()
	seedAt := time.Date(2026, 11, 10, 8, 15, 25, 0, loc)
	endAt := time.Date(2026, 11, 10, 10, 30, 0, 0, loc).UnixMilli()
	rows := bs.byTF["1m"][fixtureContract]
	var span []market.Kline
	for _, r := range rows {
		if r.T+60_000 > seedAt.UnixMilli() && r.T+60_000 <= endAt {
			span = append(span, klines([]row{r}, 60_000)[0])
		}
	}
	if len(span) < 120 {
		t.Fatalf("fixture span too short: %d bars", len(span))
	}
	noCal := map[string][]calendar.Event{}
	// The live traders ran the #61–#66 box source (newest) on 11-10; the ISB5
	// audit made merge the default. The parity compares the live log, so it
	// runs the config the live log was written under.
	live := &store.StrategyConfig{}
	live.RiskControl.MentorTuning = &store.MentorTuning{Mentor5mBoxSource: mentor.FiveMinBoxNewest}
	oldProv := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = oldProv })

	// A — the tool's own stepping (what `mentor-replay` writes to <day>.jsonl).
	var a []Event
	testCap.take()
	rpA := newReplayer(bs, live, noCal, testCap, func(ev Event) { a = append(a, ev) })
	rpA.seed(seedAt)
	a = nil // the seed lines are the same call in both; compare the ticks
	for _, b := range span {
		rpA.step(b, seedAt.UnixMilli())
	}

	// B — the live event-loop entry over an NT8-like bar cache.
	var b []Event
	testCap.take()
	rpB := newReplayer(bs, live, noCal, testCap, func(ev Event) { b = append(b, ev) })
	rpB.seed(seedAt)
	b = nil
	src := asOfSource{bs: bs, symbol: "MNQ"}
	for i, bar := range span {
		closeAt := mentor.BarCloseInstant(bar)
		src.AsOf = closeAt
		closed := src.window("1m", window)
		cache := append([]market.Kline(nil), closed...)
		if i+1 < len(span) {
			nx := span[i+1]
			// The bar NT8 has just opened: first print only, not final.
			cache = append(cache, market.Kline{OpenTime: nx.OpenTime, CloseTime: nx.CloseTime,
				Open: nx.Open, High: nx.Open, Low: nx.Open, Close: nx.Open, Final: false})
		}
		market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline {
			if symbol != "MNQ" || tf != "1m" {
				return nil
			}
			if len(cache) > n {
				return cache[len(cache)-n:]
			}
			return cache
		}
		wall := time.UnixMilli(closeAt + 400) // the FINAL frame lands a little after the close
		rpB.r.SetNow(wall)
		rpB.src.AsOf = closeAt
		rpB.observe(bar, closed, func() []mentor.Intent {
			ins, ran := rpB.r.EventPass(wall)
			if !ran {
				t.Errorf("live event pass did not run for the %s bar", ct(bar.OpenTime))
			}
			return ins
		})
	}

	ca, cb := comparable(a), comparable(b)
	if len(ca) == 0 {
		t.Fatal("the replay produced no events — nothing was compared")
	}
	n := len(ca)
	if len(cb) < n {
		n = len(cb)
	}
	for i := 0; i < n; i++ {
		if ca[i] != cb[i] {
			t.Fatalf("event %d differs:\n replay:    %s\n live path: %s", i, ca[i], cb[i])
		}
	}
	if len(ca) != len(cb) {
		t.Fatalf("event count differs: replay %d, live path %d", len(ca), len(cb))
	}

	// The window holds the 5m ISB box replacements the live log recorded
	// (vl_2026-11-10.log, trader hoang) — the parity must not be vacuous.
	want := []string{
		"5m ISB box replaced by the newest 5m inside bar at 11-10 08:15 CT: long 27722.50–27748.75 → long 27718.25–27761.75",
		"break lock short set: the isb_5m box 27722.50–27748.75 broke on the 1m close 27719.25 at 11-10 08:18 CT",
	}
	all := strings.Join(ca, "\n")
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("the replay is missing the live line %q", w)
		}
	}
	var boxEv int
	for _, ev := range a {
		if k, _ := ev["kind"].(string); strings.HasPrefix(k, "isb_box_5m_") {
			boxEv++
		}
	}
	if boxEv < 3 {
		t.Errorf("only %d 5m ISB box events in the window — the fixture no longer exercises the box", boxEv)
	}
	_ = trader.MentorSeedBarSource(asOfSource{})
}
