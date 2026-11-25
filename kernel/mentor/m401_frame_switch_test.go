package mentor

import (
	"strings"
	"testing"
)

// m401Warm2m forces a warmed 2m EMA a small step away from the 1m one, the way
// DS-103's pin does, so the schedule can select it right after a switch.
func m401Warm2m(e *Evaluator) {
	e.State.EMA2mWarmed = true
	e.State.EMA34x2 = e.State.EMA34 + 1
	e.State.EMA9x2 = e.State.EMA9 + 1
}

// m401Sentinel marks a planted (stale) touch: a fresh visit after the reset may
// legitimately record its own touch, but never with this price.
const m401Sentinel = -12345.0

// m401SwitchRun seeds at 08:00 CT, ticks to `plantAt`, plants the stale
// wrong-way state on the EMA keys AND on a control key, ticks to `until`.
func m401SwitchRun(t *testing.T, plantAtH, plantAtM, untilH, untilM int) *Evaluator {
	t.Helper()
	cfg := m401Cfg(true)
	cfg.TouchBandPts = 5
	tape := m401Tape()
	e := New(cfg)
	first := 5 * 60
	Seed(e, tape[:first], tape[first-1].CloseTime+1)
	m401Warm2m(e)
	plant := barIndexAt(tape, auditMs(2026, 10, 21, plantAtH, plantAtM, 0))
	for n := first; n <= plant; n++ {
		e.Tick(tape[:n], BarCloseInstant(tape[n-1]))
	}
	for _, k := range []string{string(KindEMA34), string(KindEMA9), string(KindEMA34HTF), "control_level"} {
		e.State.Touches[k] = Touch{Outcome: TouchWrongWay, ApproachedFrom: SideShort, PriceAtTouch: m401Sentinel}
		e.State.ISBOnly[k] = true
		e.State.VisitCapRefused[k] = true
	}
	until := barIndexAt(tape, auditMs(2026, 10, 21, untilH, untilM, 0))
	for n := plant + 1; n <= until; n++ {
		e.Tick(tape[:n], BarCloseInstant(tape[n-1]))
	}
	return e
}

// Both switches (08:59 -> 09:00 and 08:29 -> 08:30) start a FRESH visit for the
// three EMA keys only; no other level is touched. MUTANT: skip the reset in
// resetEMAVisitsOnFrameSwitch -> RED.
func TestM401FrameSwitchStartsFreshEMAVisit(t *testing.T) {
	for _, c := range []struct {
		name           string
		ph, pm, uh, um int
	}{
		{"08:59->09:00 (1m to 2m)", 8, 59, 9, 1},
		{"08:29->08:30 (2m to 1m)", 8, 29, 8, 31},
	} {
		e := m401SwitchRun(t, c.ph, c.pm, c.uh, c.um)
		for _, k := range []string{string(KindEMA34), string(KindEMA9), string(KindEMA34HTF)} {
			if e.State.Touches[k].PriceAtTouch == m401Sentinel {
				t.Errorf("%s: %s kept the planted stale touch across the frame switch", c.name, k)
			}
			if e.State.VisitCapRefused[k] {
				t.Errorf("%s: %s kept the visit-cap mark across the frame switch", c.name, k)
			}
			// ISB-only may be re-set only by a FRESH wrong-way touch of the new line.
			if e.State.ISBOnly[k] && e.State.Touches[k].Outcome != TouchWrongWay {
				t.Errorf("%s: %s kept ISB-only without a fresh wrong-way touch", c.name, k)
			}
		}
		if !e.State.ISBOnly["control_level"] || !e.State.VisitCapRefused["control_level"] ||
			e.State.Touches["control_level"].Outcome != TouchWrongWay {
			t.Errorf("%s: a non-EMA level's state must be left alone", c.name)
		}
	}
}

// The owner-facing snapshots print the EMA actually used, labelled by frame.
// MUTANT: read the bare 1m State.EMA9/EMA34 -> RED.
func TestM401SnapshotsShowTheEMAActuallyUsed(t *testing.T) {
	// 10:00 CT: 2m.
	e, tape, upto := m401Run(t, m401Cfg(true), true, 10, 0)
	_ = tape
	_ = upto
	if e.State.EMAFrame != 2 || !e.State.EMA2mWarmed {
		t.Fatalf("fixture: frame=%d warmed=%v, want 2/true", e.State.EMAFrame, e.State.EMA2mWarmed)
	}
	arm := e.State.ArmSnapshot(0)
	if arm.EMA34 != e.State.EMA34x2 || arm.EMA9 != e.State.EMA9x2 {
		t.Errorf("arm snapshot at 10:00 = %.4f/%.4f, want the 2m EMA %.4f/%.4f", arm.EMA34, arm.EMA9, e.State.EMA34x2, e.State.EMA9x2)
	}
	snap := e.reseedSnapshot()
	if snap.EMAFrame != "2m" || snap.EMA34 != e.State.EMA34x2 {
		t.Errorf("reseed snapshot at 10:00 = %+v, want frame 2m and the 2m EMA34", snap)
	}
	line := ReseedReport{Before: snap, After: snap}.Line()
	if !strings.Contains(line, "2m EMA34") || !strings.Contains(line, "2m EMA9") || strings.Contains(line, "1m EMA34") {
		t.Errorf("re-seed line at 10:00 must label the 2m EMA: %s", line)
	}
	// 08:45 CT: 1m.
	e, _, _ = m401Run(t, m401Cfg(true), true, 8, 45)
	arm = e.State.ArmSnapshot(0)
	if arm.EMA34 != e.State.EMA34 || arm.EMA9 != e.State.EMA9 {
		t.Errorf("arm snapshot at 08:45 must be the 1m EMA")
	}
	snap = e.reseedSnapshot()
	line = ReseedReport{Before: snap, After: snap}.Line()
	if snap.EMAFrame != "1m" || !strings.Contains(line, "1m EMA34") {
		t.Errorf("re-seed line at 08:45 must label the 1m EMA: %s", line)
	}
}
