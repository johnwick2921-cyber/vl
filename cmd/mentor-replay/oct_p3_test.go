package main

import (
	"fmt"
	"strings"
	"testing"

	"vl/calendar"
	"vl/kernel"
)

// P3-missesA (oct-exact, D3.3-E9): the Friday FTGH box still works on the Sunday
// chart [Oct D3.3 @17:00 OctD3.3-R27, @41:28 R51; E9: Sun 1 Nov ~21:53 the retest of
// the Friday FTGH 28,812.50-28,818.25, "cũng được 17 điểm", target 28,790.75]. Three
// pieces, each pinned here on the video's own tape:
//   1. the seed replay latches the SAME Friday RTH box the live tick latched (the
//      08:30 box replaces the 08:23 pre-open box in the seed too);
//   2. the box carried past the 17:00 roll is that RTH box, not a later post-escape zone;
//   3. a box return against the 5m trigger is the D5.4-R20 counter-5m scalp (reduced
//      size tier), not box_against_5m_trend.

func octP3Intents(t *testing.T, bs *barSet, d string, fromHM, toHM string) []Event {
	t.Helper()
	seedAt, err := seedInstant("session", d)
	if err != nil {
		t.Fatal(err)
	}
	var cur, out []Event
	testCap.take()
	rp := newReplayer(bs, nil, map[string][]calendar.Event{}, testCap, func(ev Event) { cur = append(cur, ev) })
	rp.seed(seedAt)
	_ = kernel.CTLocation()
	for _, r := range bs.byTF["1m"][fixtureContract] {
		if r.T+60_000 <= seedAt.UnixMilli() {
			continue
		}
		cur = nil
		b := klines([]row{r}, 60_000)[0]
		if rp.step(b, seedAt.UnixMilli()) {
			t.Fatalf("panic on the %s bar: %v", ct(b.OpenTime), cur)
		}
		for _, ev := range cur {
			ts, _ := ev["t"].(string)
			if fmt.Sprint(ev["kind"]) != "intent" || len(ts) < 16 {
				continue
			}
			if hm := ts[11:16]; hm >= fromHM && hm <= toHM {
				out = append(out, ev)
			}
		}
		if len(ct(b.OpenTime)) >= 16 && ct(b.OpenTime)[11:16] > toHM && strings.HasPrefix(ct(b.OpenTime), "2026-11-01") {
			break
		}
	}
	return out
}

func TestOctP3E9SeedLatchesTheFridayRTHBox(t *testing.T) {
	bs := loadFixtureFile(t, "mnq_12-26_1m_20260907-20261103_full.csv.gz")
	testCap.take()
	rp := newReplayer(bs, nil, map[string][]calendar.Event{}, testCap, func(ev Event) {})
	rp.seed(octParseCT("2026-10-30 15:30"))
	b, ok := rp.r.Evaluator().State.LatchedBoxes["ftgh"]
	if !ok {
		t.Fatal("no FTGH latched by the seed at Fri 10-30 15:30")
	}
	// Live (tick by tick) latches 28,812.50-28,818.25 at the 08:30 open; the seed used to
	// keep the 08:23 pre-open box 28,793.50-28,794.00.
	if b.Bottom != 28812.5 || b.Top != 28818.25 {
		t.Fatalf("seed latched FTGH %.2f-%.2f, the live tick latched 28,812.50-28,818.25 (Oct D3.3 E3/E9)", b.Bottom, b.Top)
	}
}

func TestOctP3E9FridayFTGHIsShortedOnSundayAsACounter5mScalp(t *testing.T) {
	bs := loadFixtureFile(t, "mnq_12-26_1m_20260907-20261103_full.csv.gz")
	var hit Event
	for _, ev := range octP3Intents(t, bs, "2026-11-02", "21:50", "22:28") {
		a := fmt.Sprint(ev["action"])
		if (a != "place_stop_entry" && a != "place_stop_limit_entry") || fmt.Sprint(ev["setup"]) != "BOX" || fmt.Sprint(ev["side"]) != "short" {
			continue
		}
		if px, _ := ev["entry"].(float64); px > 28808.25-2 && px < 28808.25+2 {
			hit = ev
			break
		}
	}
	if hit == nil {
		t.Fatal("no short BOX entry near 28,808.25 between 21:50 and 22:28 on Sun 1 Nov: the Friday FTGH 28,812.50-28,818.25 retest (D3.3 E9) is not armed")
	}
	if !strings.Contains(fmt.Sprint(hit["flag"]), "counter_5m") {
		t.Fatalf("the box return is against the 5m trigger: it must carry the counter_5m scalp tier (Oct D5.4-R20), got flag %q", hit["flag"])
	}
}
