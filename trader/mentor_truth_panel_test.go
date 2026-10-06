package trader

import (
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/store"
)

// ── MENTOR-TRUTH PANEL (release #10) — call-site pins ───────────────────────

func truthAT(t *testing.T) *AutoTrader {
	t.Helper()
	at := &AutoTrader{
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}}},
	}
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	t.Cleanup(ResetMentorCountersForTest)
	return at
}

// The snapshot reflects the live evaluator state: the 4h/1h trigger directions,
// the HTF verdict, the 5m trigger, the key levels with today's visits, and the
// default window. Mutant: read the zero State (or skip the lock) → RED.
func TestMentorTruthSnapshotReflectsSeededEvaluator(t *testing.T) {
	at := truthAT(t)
	ev := at.mentorEval
	ev.State.HTF = mentor.HTF{
		FourH: mentor.TriggerLine{Dir: mentor.SideLong, MovedAt: 1000},
		OneH:  mentor.TriggerLine{Dir: mentor.SideLong, MovedAt: 2000},
	}
	ev.State.Trigger = mentor.TriggerLine{Dir: mentor.SideLong, Price: 30000, MovedAt: 3000}
	ev.State.Levels = []mentor.Level{{Key: "kl:30000@d", Kind: mentor.KindKeyLevel, Price: 30000, AtTime: 4000}}
	ev.State.Visits = map[string]int{"kl:30000@d": 2}

	now := time.Now()
	p, ok := at.MentorTruthSnapshot(now)
	if !ok || !p.Enabled {
		t.Fatalf("mentor-mode snapshot must be enabled, got ok=%v enabled=%v", ok, p.Enabled)
	}
	if p.AsOfMs != now.UnixMilli() {
		t.Fatalf("as_of_ms = %d, want the server stamp %d", p.AsOfMs, now.UnixMilli())
	}
	if p.HTF.FourHDir != "long" || p.HTF.FourHSince != 1000 {
		t.Fatalf("4h = %q@%d, want long@1000", p.HTF.FourHDir, p.HTF.FourHSince)
	}
	if p.HTF.OneHDir != "long" || p.HTF.OneHSince != 2000 {
		t.Fatalf("1h = %q@%d, want long@2000", p.HTF.OneHDir, p.HTF.OneHSince)
	}
	if p.HTF.Verdict != "follow" || p.HTF.VerdictSide != "long" {
		t.Fatalf("HTF verdict = %q/%q, want follow/long (4h and 1h agree)", p.HTF.Verdict, p.HTF.VerdictSide)
	}
	if p.Trigger5m.Dir != "long" || p.Trigger5m.Price != 30000 || p.Trigger5m.Since != 3000 {
		t.Fatalf("5m trigger = %+v, want long @30000 since 3000", p.Trigger5m)
	}
	if len(p.Levels) != 1 || p.Levels[0].Kind != "key_level" || p.Levels[0].Price != 30000 || p.Levels[0].VisitsToday != 2 {
		t.Fatalf("levels = %+v, want one key_level @30000 visited 2x today", p.Levels)
	}
	if p.Window.Start != "08:30" || p.Window.Minutes != 60 {
		t.Fatalf("window = %q/%d, want the default 08:30/60", p.Window.Start, p.Window.Minutes)
	}
	if p.DoneAfterWin || p.StopAfterLoss {
		t.Fatalf("unwired day-stop seams must read not-tripped, got daw=%v sal=%v", p.DoneAfterWin, p.StopAfterLoss)
	}
}

// Mentor mode OFF → the payload is Enabled=false so the frontend hides the card
// without a second call.
func TestMentorTruthSnapshotDisabledWhenMentorOff(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: false}}}}
	p, ok := at.MentorTruthSnapshot(time.Now())
	if ok || p.Enabled {
		t.Fatalf("mentor OFF must return ok=false enabled=false, got ok=%v %+v", ok, p)
	}
}
