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

// jphl1Minute is the production evaluator's 5m line and rolling ladder after
// one replayed 1m bar.
type jphl1Minute struct {
	HM       string // the bar's open, CT
	FourH    mentor.Side
	FiveM    mentor.Side
	Ladder   bool
	Side     mentor.Side
	RungPx   float64
	Arms     []octArm
	Refusals []string
}

// jphl1Replay replays the 11-16 evening (seeded 17:00 CT) through the
// PRODUCTION trader replay up to 19:30 CT and reads the evaluator's own state
// after every bar.
func jphl1Replay(t *testing.T, bs *barSet, sc *store.StrategyConfig) []jphl1Minute {
	t.Helper()
	seedAt, err := seedInstant("session", "2026-11-17")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 11, 16, 19, 0, 0, 0, kernel.CTLocation()).UnixMilli()
	endMs := time.Date(2026, 11, 16, 19, 30, 0, 0, kernel.CTLocation()).UnixMilli()
	var cur []Event
	testCap.take()
	rp := newReplayer(bs, sc, map[string][]calendar.Event{}, testCap, func(ev Event) { cur = append(cur, ev) })
	rp.seed(seedAt)
	var out []jphl1Minute
	for _, r := range bs.byTF["1m"][fixtureContract] {
		if r.T+60_000 <= seedAt.UnixMilli() || r.T+60_000 > endMs {
			continue
		}
		cur = nil
		b := klines([]row{r}, 60_000)[0]
		if rp.step(b, seedAt.UnixMilli()) {
			t.Fatalf("panic on the %s bar: %v", ct(b.OpenTime), cur)
		}
		if b.OpenTime < from {
			continue
		}
		s := rp.r.Evaluator().State
		m := jphl1Minute{HM: time.UnixMilli(b.OpenTime).In(kernel.CTLocation()).Format("15:04"),
			FourH: s.HTF.FourH.Dir, FiveM: s.Trigger.Dir, Ladder: s.RollingPHL.Ladder, Side: s.RollingPHL.Side, RungPx: s.RollingPHL.RungPx}
		for _, ev := range cur {
			switch fmt.Sprint(ev["kind"]) {
			case "intent":
				a := fmt.Sprint(ev["action"])
				if a == string(mentor.PlaceStopEntry) || a == string(mentor.PlaceStopLimitEntry) {
					px, _ := ev["entry"].(float64)
					m.Arms = append(m.Arms, octArm{Side: fmt.Sprint(ev["side"]), Setup: fmt.Sprint(ev["setup"]), Entry: px, Reason: fmt.Sprint(ev["reason"])})
				}
			case "refusal":
				m.Refusals = append(m.Refusals, fmt.Sprint(ev["reason"]))
			}
		}
		out = append(out, m)
	}
	return out
}

func jphl1HTFGate(on bool) *store.StrategyConfig {
	sc := &store.StrategyConfig{}
	sc.RiskControl.MentorTuning = &store.MentorTuning{MentorHTFGateEnabled: &on}
	return sc
}

// J-PHL-1 (P0, the 11-16 19:12-19:20 rally): the 4h line was short (27,854.00
// since 11-15 21:48), the 5m line long (27,328.50). The rolling ladder took
// the 4h's side, so through the whole run-up it could only ladder SHORTS (each
// rung then refused) and never a PHL buy stop. October: the 5m is the only big
// frame [Oct D4.3 @07:26-07:35; @10:36-11:33]. Default: no short ladder while
// the 5m line is long, and the 19:13 red pullback candle of the 5m up leg
// starts the LONG ladder. Revert (mentor_htf_gate_enabled=true; 19:13 is
// outside the open/news windows, so the gate is off and the 4h alone is the
// side): the live short ladder comes back exactly.
func TestJPHL1_1116_RollingSideFollowsThe5m(t *testing.T) {
	bs := jbFixture1116(t)
	mins := jphl1Replay(t, bs, nil)
	sawLong := false
	for _, m := range mins {
		if m.FiveM == mentor.SideLong && m.Ladder && m.Side == mentor.SideShort {
			t.Errorf("default: %s a SHORT rolling ladder runs (rung %.2f) under a 5m BUY line (4h %s) - the side must be the 5m's [Oct D4.3 @07:26-07:35]", m.HM, m.RungPx, m.FourH)
		}
		for _, a := range m.Arms {
			if a.Side == "short" && m.FiveM == mentor.SideLong && strings.Contains(a.Reason, "rolling pullback") {
				t.Errorf("default: %s a short rolling rung armed at %.2f under a 5m BUY line", m.HM, a.Entry)
			}
		}
		if m.HM >= "19:12" && m.HM <= "19:20" && m.Ladder && m.Side == mentor.SideLong {
			sawLong = true
		}
	}
	if !sawLong {
		for _, m := range mins {
			if m.HM >= "19:10" && m.HM <= "19:21" {
				t.Logf("%s 4h %s 5m %s ladder %v %s rung %.2f refusals %v", m.HM, m.FourH, m.FiveM, m.Ladder, m.Side, m.RungPx, m.Refusals)
			}
		}
		t.Errorf("default: no LONG rolling ladder anywhere in the 19:12-19:20 rally although the 5m line is long and 19:13 is a red pullback candle")
	}

	rev := jphl1Replay(t, bs, jphl1HTFGate(true))
	for _, m := range rev {
		if m.HM == "19:13" || m.HM == "19:14" {
			if !(m.Ladder && m.Side == mentor.SideShort && m.FourH == mentor.SideShort) {
				t.Errorf("revert (mentor_htf_gate_enabled=true): %s want the live short ladder of the 4h side, got ladder=%v side=%q 4h=%q", m.HM, m.Ladder, m.Side, m.FourH)
			}
		}
	}
}

// Review P1 (prod-path, 11-17): the exact pre-J-PHL-1 live state — HTF gate
// OFF, rolling side from the 4h — through mentor_rolling_side_source=htf alone.
// On the 11-16 tape the live short ladder of the 4h side comes back at 19:13 and
// 19:14 (the resolver keeps the 4h/1h gate OFF: trader
// TestJPHL1RollingSideSourceReachesTheEvaluator; on this window the gate would
// add no htf_* refusal either way, so the check below is a guard, not the proof).
func TestJPHL1_1116_RollingSideSourceHTFRevertsExactly(t *testing.T) {
	bs := jbFixture1116(t)
	sc := &store.StrategyConfig{}
	sc.RiskControl.MentorTuning = &store.MentorTuning{MentorRollingSideSource: "htf"}
	rev := jphl1Replay(t, bs, sc)
	for _, m := range rev {
		if m.HM == "19:13" || m.HM == "19:14" {
			if !(m.Ladder && m.Side == mentor.SideShort && m.FourH == mentor.SideShort) {
				t.Errorf("mentor_rolling_side_source=htf: %s want the live short ladder of the 4h side, got ladder=%v side=%q 4h=%q", m.HM, m.Ladder, m.Side, m.FourH)
			}
		}
		for _, r := range m.Refusals {
			if strings.HasPrefix(r, "htf_") || strings.Contains(r, "_htf_") {
				t.Errorf("mentor_rolling_side_source=htf: %s refusal %q - the 4h/1h entry gate must stay OFF", m.HM, r)
			}
		}
	}
}
