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

// OCT-EXACT D1 on the video's own tape: 10-30 ~09:04, the FTGH break-and-reverse
// ISB the mentor refuses ("hoan toan khong duoc", Oct D3.3 @38:10, R47). The box
// is 28,812.50-28,819.00; the 09:02 candle opened above it and closed under it
// (28,800.75), the 09:03 inside bar closes 28,802.75 and never gets back. The
// scorecard row D3.3-E7 passes today for any of several reasons (the trigger-side
// gate also refuses it); this pins that the NEW rule is what refuses it: the named
// refusal isb_just_broken_level is counted on that tick, and not with the knob off.
func octDReplayRefusals(t *testing.T, bs *barSet, d string, sc *store.StrategyConfig) (arms []octArm, refusals map[string]int) {
	t.Helper()
	seedAt, err := seedInstant("session", d)
	if err != nil {
		t.Fatal(err)
	}
	loc := kernel.CTLocation()
	day, _ := time.ParseInLocation("2006-01-02", d, loc)
	endMs := time.Date(day.Year(), day.Month(), day.Day(), 11, 0, 0, 0, loc).UnixMilli()
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
					arms = append(arms, octArm{At: at, Side: fmt.Sprint(ev["side"]), Setup: fmt.Sprint(ev["setup"]), Entry: px})
				}
			case "refusal":
				n := 1
				if f, ok := ev["n"].(float64); ok {
					n = int(f)
				}
				refusals[at.Format("15:04")+" "+fmt.Sprint(ev["reason"])] += n
			}
		}
	}
	return arms, refusals
}

func TestOctD1_E7RefusedByTheRule(t *testing.T) {
	bs := loadFixtureFile(t, "mnq_12-26_1m_20260907-20261103_full.csv.gz")
	armsOn, refOn := octDReplayRefusals(t, bs, "2026-10-30", nil)
	off := false
	sc := &store.StrategyConfig{}
	sc.RiskControl.MentorTuning = &store.MentorTuning{MentorISBJustBrokenLevel: &off}
	armsOff, refOff := octDReplayRefusals(t, bs, "2026-10-30", sc)

	// bar 09:03 closes at the 09:04 stamp (the tick evaluates the bar that opened a minute earlier)
	const key = "09:04 isb_just_broken_level"
	if refOn[key] == 0 {
		var got []string
		for k := range refOn {
			if strings.HasPrefix(k, "09:0") {
				got = append(got, k)
			}
		}
		t.Fatalf("knob ON: no %q refusal on the E7 tick; 09:0x refusals: %v", key, got)
	}
	if refOff[key] != 0 {
		t.Errorf("knob OFF: %q still fires (%d) - the revert does not revert", key, refOff[key])
	}
	// E7 stays refused (no short ISB/BOX arm 09:01-09:08) with the rule on
	for _, a := range armsOn {
		if a.Side == string(mentor.SideShort) && (a.Setup == "ISB" || a.Setup == "BOX") &&
			a.At.Format("15:04") >= "09:01" && a.At.Format("15:04") <= "09:08" {
			t.Errorf("knob ON: a short %s armed at %s @%.2f inside the E7 window", a.Setup, a.At.Format("15:04"), a.Entry)
		}
	}
	_ = armsOff
}
