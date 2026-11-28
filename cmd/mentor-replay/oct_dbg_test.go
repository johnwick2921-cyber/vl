package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"vl/calendar"
	"vl/kernel"
)

// TestOctDebugWindow dumps every replay event of one session inside a CT
// window (diagnostic helper for the scorecard misses; skipped unless
// OCT_DBG_DAY is set). OCT_DBG_DAY=2026-11-02 OCT_DBG_FROM=17:00 OCT_DBG_TO=17:08.
func TestOctDebugWindow(t *testing.T) {
	d := os.Getenv("OCT_DBG_DAY")
	if d == "" {
		t.Skip("OCT_DBG_DAY unset")
	}
	from := octParseCT(os.Getenv("OCT_DBG_DATE") + " " + os.Getenv("OCT_DBG_FROM"))
	to := octParseCT(os.Getenv("OCT_DBG_DATE") + " " + os.Getenv("OCT_DBG_TO"))
	bs := loadFixtureFile(t, "mnq_12-26_1m_20260907-20261103_full.csv.gz")
	seedAt, err := seedInstant("session", d)
	if err != nil {
		t.Fatal(err)
	}
	_ = kernel.CTLocation()
	loc := kernel.CTLocation()
	_ = loc
	var cur []Event
	testCap.take()
	rp := newReplayer(bs, nil, map[string][]calendar.Event{}, testCap, func(ev Event) { cur = append(cur, ev) })
	rp.seed(seedAt)
	var sb strings.Builder
	if os.Getenv("OCT_DBG_BOXES") != "" {
		fmt.Fprintf(&sb, "SEED carried=%+v latched=%+v post=%+v\n", rp.r.Evaluator().State.CarriedBoxes, rp.r.Evaluator().State.LatchedBoxes, rp.r.Evaluator().State.PostEscapeBoxes)
	}
	for _, r := range bs.byTF["1m"][fixtureContract] {
		if r.T+60_000 <= seedAt.UnixMilli() {
			continue
		}
		if r.T+60_000 > to.Add(2*60_000).UnixMilli() {
			break
		}
		cur = nil
		b := klines([]row{r}, 60_000)[0]
		rp.step(b, seedAt.UnixMilli())
		for _, ev := range cur {
			ts, _ := ev["t"].(string)
			if ts == "" {
				continue
			}
			at := octParseCT(ts)
			if at.Before(from) || at.After(to) {
				continue
			}
			fmt.Fprintf(&sb, "%v\n", ev)
		}
	}
	if os.Getenv("OCT_DBG_LEVELS") != "" {
		for _, l := range rp.r.Evaluator().State.ChartLevels {
			fmt.Fprintf(&sb, "LVL %s %.2f kind=%s role=%s\n", l.Key, l.Price, l.Kind, l.Role)
		}
	}
	if os.Getenv("OCT_DBG_BOXES") != "" {
		fmt.Fprintf(&sb, "END carried=%+v\n latched=%+v\n post=%+v\n", rp.r.Evaluator().State.CarriedBoxes, rp.r.Evaluator().State.LatchedBoxes, rp.r.Evaluator().State.PostEscapeBoxes)
	}
	t.Log("\n" + sb.String())
}

// TestOctDebugSeedAt prints the boxes a seed at OCT_DBG_AT (CT "2006-01-02 15:04") rebuilds.
func TestOctDebugSeedAt(t *testing.T) {
	at := os.Getenv("OCT_DBG_AT")
	if at == "" {
		t.Skip("OCT_DBG_AT unset")
	}
	bs := loadFixtureFile(t, "mnq_12-26_1m_20260907-20261103_full.csv.gz")
	testCap.take()
	rp := newReplayer(bs, nil, map[string][]calendar.Event{}, testCap, func(ev Event) {})
	rp.seed(octParseCT(at))
	st := rp.r.Evaluator().State
	t.Logf("latched=%+v\n post=%+v\n carried=%+v", st.LatchedBoxes, st.PostEscapeBoxes, st.CarriedBoxes)
}
