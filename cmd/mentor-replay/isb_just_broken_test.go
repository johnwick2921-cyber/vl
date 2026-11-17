package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
)

// jbReplay replays the session of replay day d (17:00 CT the evening before)
// through the PRODUCTION trader replay (trader.MentorReplay seed + per-bar tick)
// up to endCT on the given calendar date, and returns the entries armed and the
// named refusals per bar-close minute.
func jbReplay(t *testing.T, bs *barSet, d string, end time.Time, sc *store.StrategyConfig) (arms []octArm, refusals map[string]int, logs []octLine) {
	t.Helper()
	seedAt, err := seedInstant("session", d)
	if err != nil {
		t.Fatal(err)
	}
	endMs := end.UnixMilli()
	refusals = map[string]int{}
	var cur []Event
	testCap.take()
	rp := newReplayer(bs, sc, map[string][]calendar.Event{}, testCap, func(ev Event) { cur = append(cur, ev) })
	rp.seed(seedAt)
	for _, r := range bs.byTF["1m"][fixtureContract] {
		if r.T+60_000 <= seedAt.UnixMilli() || r.T+60_000 > endMs {
			continue
		}
		cur = nil
		b := klines([]row{r}, 60_000)[0]
		if rp.step(b, seedAt.UnixMilli()) {
			t.Fatalf("panic on the %s bar: %v", ct(b.OpenTime), cur)
		}
		for _, ev := range cur {
			ts, _ := ev["t"].(string)
			if ts == "" {
				continue
			}
			at := octParseCT(ts)
			switch fmt.Sprint(ev["kind"]) {
			case "intent":
				a := fmt.Sprint(ev["action"])
				if a == string(mentor.PlaceStopEntry) || a == string(mentor.PlaceStopLimitEntry) {
					px, _ := ev["entry"].(float64)
					arms = append(arms, octArm{At: at, Side: fmt.Sprint(ev["side"]), Setup: fmt.Sprint(ev["setup"]), Entry: px, Reason: fmt.Sprint(ev["reason"])})
				}
			case "refusal":
				n := 1
				if f, ok := ev["n"].(float64); ok {
					n = int(f)
				}
				refusals[at.Format("15:04")+" "+fmt.Sprint(ev["reason"])] += n
			case "log":
				logs = append(logs, octLine{at, fmt.Sprint(ev["msg"])})
			}
		}
	}
	return arms, refusals, logs
}

// jbFixture1116 is the full Sept-Nov tape plus the 11-03 -> 11-16 19:40 CT tape
// (extracted read-only from data.db bars, contract MNQ 12-26).
func jbFixture1116(t *testing.T) *barSet {
	t.Helper()
	a := loadFixtureFile(t, "mnq_12-26_1m_20260907-20261103_full.csv.gz")
	b := loadFixtureFile(t, "mnq_12-26_1m_20261103-20261116_1940.csv.gz")
	rows := a.byTF["1m"][fixtureContract]
	last := rows[len(rows)-1].T
	for _, r := range b.byTF["1m"][fixtureContract] {
		if r.T > last {
			rows = append(rows, r)
		}
	}
	a.byTF["1m"][fixtureContract] = rows
	return a
}

// jbScope builds a strategy whose mentor tuning pins the just-broken scope.
func jbScope(scope string) *store.StrategyConfig {
	sc := &store.StrategyConfig{}
	sc.RiskControl.MentorTuning = &store.MentorTuning{MentorISBJustBrokenScope: scope}
	return sc
}

func jbArmAt(arms []octArm, hhmm, side, setup string, px float64) bool {
	for _, a := range arms {
		if a.At.Format("15:04") == hhmm && a.Side == side && a.Setup == setup && (px == 0 || a.Entry == px) {
			return true
		}
	}
	return false
}

// ISB-M1 (P0, the 11-16 19:14 live trade): the 19:12 GREEN mother (O 27,330.50
// C 27,368.00) closed up THROUGH key level 27,359; the 19:13 RED inside candle
// (L 27,346.75 H 27,362.75) sits ON it. The side is right (mother colour, Oct
// D2.2 @1:00:40-1:01:28), the PLACE is wrong: "Insider 3 qua gan ... o giua cai
// level ... dung vo" (Oct D3.3 @00:46-01:00), "dung bat INSABA ngay cho no vua
// break" (@02:40). Live armed 19:14:01 long 27,364.25 (rows 608/609) and was
// stopped 19:15. Default (scope all): no 19:14 long ISB, refused by name.
// Revert (scope box): the live arm comes back exactly.
func TestISBM1_1116_1914LongRefusedAtTheLevelItJustBroke(t *testing.T) {
	bs := jbFixture1116(t)
	end := time.Date(2026, 11, 16, 19, 16, 0, 0, kernel.CTLocation())
	arms, ref, _ := jbReplay(t, bs, "2026-11-17", end, nil)
	if jbArmAt(arms, "19:14", "long", "ISB", 27364.25) {
		t.Errorf("default (scope all): the 19:14 long ISB 27,364.25 still arms - the inside candle sits on key 27,359 the mother just broke [Oct D3.3 @00:46-01:00, @02:40]")
	}
	if ref["19:14 isb_just_broken_level"] == 0 {
		t.Errorf("default (scope all): no 19:14 isb_just_broken_level refusal; 19:14 refusals: %v", jbAt(ref, "19:14"))
	}
	armsBox, refBox, _ := jbReplay(t, bs, "2026-11-17", end, jbScope("box"))
	if !jbArmAt(armsBox, "19:14", "long", "ISB", 27364.25) {
		t.Errorf("revert (scope box): the live 19:14 long ISB 27,364.25 did not come back; arms %v", armsBox)
	}
	if refBox["19:14 isb_just_broken_level"] != 0 {
		t.Errorf("revert (scope box): 19:14 still refused isb_just_broken_level")
	}
}

// D3.3 @37:00-37:51 (frame 00:37:03, 10-30): the 08:59 green candle closes ABOVE
// the FTGH box (28,812.50-28,819.00); "dong tren level thi minh chang lam gi het
// thi mot la minh doi Insaba long" - wait for an ISB long. The 09:00 inside candle
// (L 28,827.25, wholly above the box) IS that ISB long: allowed. The old box-only
// rule refused it (09:01 isb_just_broken_level) because the 08:59 body crossed the
// top edge and the inside bar did not "retest" it. Default: not refused by this
// rule. Revert (scope box): the old refusal is back.
func TestISBM1_1030_0900LongAboveTheBoxNotRefused(t *testing.T) {
	bs := loadFixtureFile(t, "mnq_12-26_1m_20260907-20261103_full.csv.gz")
	end := time.Date(2026, 10, 30, 9, 10, 0, 0, kernel.CTLocation())
	_, ref, _ := jbReplay(t, bs, "2026-10-30", end, nil)
	if n := ref["09:01 isb_just_broken_level"]; n != 0 {
		t.Errorf("default (scope all): the 09:00 ISB long wholly above the box is refused isb_just_broken_level x%d [Oct D3.3 @37:00-37:51 allows it]", n)
	}
	_, refBox, _ := jbReplay(t, bs, "2026-10-30", end, jbScope("box"))
	if refBox["09:01 isb_just_broken_level"] == 0 {
		t.Errorf("revert (scope box): the old 09:01 isb_just_broken_level refusal did not come back")
	}
}

func jbAt(ref map[string]int, hhmm string) []string {
	var out []string
	for k, n := range ref {
		if len(k) > 5 && k[:5] == hhmm {
			out = append(out, fmt.Sprintf("%s x%d", k[6:], n))
		}
	}
	return out
}

// The mentor's own allowed ISBs on 10-30 stay armed under scope all, through the
// production replay: D2.2 E9 (08:34 short ISB 28,709.50, the inside bar clear of
// the 28,743.9 trendline its mother broke, D3.3 @01:40 "o xa level"), the 09:00
// ISB long above the box (09:01 buy 28,860.50 vs the mentor's 28,860.25, D3.3
// @37:00-51 / E6 entries) and the drop-away (09:16 short ISB 28,675.75, D3.3
// @40:07-40:20). E7 (09:04) stays refused by name (TestOctD1_E7RefusedByTheRule).
func TestISBM1_1030AllowedISBsStayArmed(t *testing.T) {
	bs := loadFixtureFile(t, "mnq_12-26_1m_20260907-20261103_full.csv.gz")
	end := time.Date(2026, 10, 30, 9, 20, 0, 0, kernel.CTLocation())
	arms, ref, logs := jbReplay(t, bs, "2026-10-30", end, nil)
	for _, w := range []struct {
		hhmm, side string
		px         float64
		why        string
	}{
		{"08:34", "short", 28709.50, "D2.2 E9"},
		{"09:01", "long", 28860.50, "D3.3 @37:00-51 ISB long above the box"},
		{"09:16", "short", 28675.75, "D3.3 @40:07-40:20 drop-away"},
	} {
		if !jbArmAt(arms, w.hhmm, w.side, "ISB", w.px) {
			t.Errorf("%s: no %s %s ISB %.2f; refusals then: %v", w.why, w.hhmm, w.side, w.px, jbAt(ref, w.hhmm))
		}
	}
	for _, l := range logs {
		if l.At.Format("15:04") == "09:04" && strings.Contains(l.Text, "refused: the inside bar sits at a level just broken") {
			if !strings.Contains(l.Text, "broke and reversed back through") {
				t.Errorf("E7 09:04 refused for another reason than the box break-and-reverse: %s", l.Text)
			}
		}
	}
}

// Oct D3.3 @00:16-01:00 (frame 00:50, Sun 01 Nov 21:18 CST): the inside bar on the
// trendline - "dung vo". The bot draws no 1m line there (the mentor's line is hand
// drawn), so on the production tape the 21:18 short ISB is refused by the box gate
// (isb_box_blocked at 21:19), in both scopes; the trendline read itself is pinned
// on the frame's reconstruction in kernel/mentor TestISBM1_D33_0101_2118*.
func TestISBM1_D33_0101_2118NoShortISB(t *testing.T) {
	bs := jbFixture1116(t)
	end := time.Date(2026, 11, 1, 21, 22, 0, 0, kernel.CTLocation())
	for _, sc := range []*store.StrategyConfig{nil, jbScope("box")} {
		arms, ref, _ := jbReplay(t, bs, "2026-11-02", end, sc)
		for _, a := range arms {
			if hm := a.At.Format("15:04"); a.Side == "short" && a.Setup == "ISB" && hm >= "21:19" && hm <= "21:20" {
				t.Errorf("a short ISB armed %s @%.2f on the 21:18 inside bar (refusals %v)", hm, a.Entry, jbAt(ref, "21:19"))
			}
		}
	}
}

// Review P1-1 (11-17): the 11-16 19:17 long PHL 27,374.75 (the reverse ISB at
// EMA 9 read as the with-trend PHL) must arm under the default. Its mother, the
// 19:15 RED candle (O 27,365.75 C 27,344.75), broke key 27,359 DOWN; the long
// trades AGAINST that break, and Oct D3.3 @00:16-02:47 refuses only the ISB in
// the break direction. REPORT-full-reread-1116.md:115 rules this trade correct
// (D5.3 @17:40-18:30 "Luc nao cung chon cai huong theo trend", "Nhung lenh no
// dep lam"). The 19:17 SHORT ISB on the same pair (the break direction) stays
// refused isb_just_broken_level.
func TestISBM1_1116_1917ReverseLongPHLNotRefused(t *testing.T) {
	bs := jbFixture1116(t)
	end := time.Date(2026, 11, 16, 19, 18, 0, 0, kernel.CTLocation())
	arms, ref, _ := jbReplay(t, bs, "2026-11-17", end, nil)
	if !jbArmAt(arms, "19:17", "long", "PHL", 27374.75) {
		t.Errorf("default: the 19:17 long PHL 27,374.75 (against the 19:15 down-break) did not arm; 19:17 refusals %v", jbAt(ref, "19:17"))
	}
	if ref["19:17 isb_just_broken_level_rev"] != 0 {
		t.Errorf("default: the 19:17 reverse ISB long was refused isb_just_broken_level_rev - the mother broke 27,359 DOWN, the long is against the break")
	}
	if ref["19:17 isb_just_broken_level"] == 0 {
		t.Errorf("default: the 19:17 short ISB in the break direction is no longer refused; 19:17 refusals %v", jbAt(ref, "19:17"))
	}
	if jbArmAt(arms, "19:14", "long", "ISB", 27364.25) {
		t.Errorf("default: the 19:14 long ISB came back (the up-break direction must stay refused)")
	}
}
