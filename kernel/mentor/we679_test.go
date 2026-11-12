package mentor

import (
	"testing"
	"time"
	"vl/market"
)

// FIX-1112 item B — the mentor's worked trades WE6 / WE7 on the real stored
// 1m bars, with the REAL levels the replay had live at the rung (replay of the
// strategy a5b7662e, 2026-11-12): the rung the mentor drew was refused by two
// gates the course never taught for a PHL/PLH pullback leg.
//
//	WE6 10-27 09:44: rolling_level_touch_owns — a 1H key level 29,668.50 that
//	  price touched and rejected at 08:40 (an hour BEFORE the old high 09:36
//	  that the leg targets) still "owned" the entry. The mentor's WE6 ladder is
//	  the pullback AFTER 09:36 [OctD2.3 @ 13:37-28:33].
//	WE7 10-30 13:25: keypair_candle_too_big — the old low 28,288.75 is a 1m tape
//	  swing, paired with the 1H key level 28,283.00 5.75 pts below it, the 13.5-pt
//	  13:25 candle "is as big as the rank" (X4). X4 is a Sept/extra lesson about
//	  two KEY levels; the mentor sold this exact rung [OctD2.3 @ 35:46-39:09].

func we679Levels6() []Level {
	return []Level{{Key: "key_level:29668.50:1792524600000", Kind: KindKeyLevel, Price: 29668.50}}
}

func we679Levels7() []Level {
	return []Level{{Key: "key_level:28283.00:1792000000000", Kind: KindKeyLevel, Price: 28283.00}}
}

// we679Run ticks rollingPHLTick over the real bars with real levels. touchAt
// (optional) latches a TouchReject on the level at that CT clock.
func we679Run(t *testing.T, e *Evaluator, file string, mo time.Month, d int, fromH, fromM, toH, toM int, levels []Level, touchKey string, touchH, touchM int, touchSide Side) []phl1Rung {
	bars := phl1Tape(t, file)
	from := phl1At(t, bars, 2026, mo, d, fromH, fromM)
	to := phl1At(t, bars, 2026, mo, d, toH, toM)
	if touchKey != "" {
		ti := phl1At(t, bars, 2026, mo, d, touchH, touchM)
		e.State.Touches = map[string]Touch{touchKey: {LevelKey: touchKey, Outcome: TouchReject, RefBar: bars[ti], ApproachedFrom: touchSide}}
	}
	var out []phl1Rung
	for i := from; i <= to; i++ {
		before := map[string]int{}
		for k, v := range e.State.Refusals {
			before[k] = v
		}
		w := bars[:i+1]
		olds := oldExtremeIndexes(Levels(w, e.Cfg, BarCloseInstant(bars[i])), w)
		ins := e.rollingPHLTick(w, bars[i], bars[i], 1, olds, levels, nil, DayGate{})
		r := phl1Rung{at: time.UnixMilli(bars[i].OpenTime).In(ctime()).Format("15:04"), places: m464Rolling(ins)}
		for k, v := range e.State.Refusals {
			if v > before[k] {
				r.refuse = append(r.refuse, k)
			}
		}
		out = append(out, r)
	}
	return out
}

func we679Cfg() Config {
	cfg := DefaultConfig()
	cfg.RoomMultiple = 1
	cfg.PHLRoomMultiple = 1 // as TestPHL1CourseWorkedExamples: sets the D5.3 room rule aside
	return cfg
}

func TestWE6StaleLevelTouchDoesNotOwnThePullbackLeg(t *testing.T) {
	bars := phl1Tape(t, "phl1_mnq_1m_20261027_0730ct.csv")
	from := phl1At(t, bars, 2026, time.October, 27, 9, 30)
	px := bars[phl1At(t, bars, 2026, time.October, 27, 9, 44)].Close
	cfg := we679Cfg()
	cfg.RollingStaleTouchReleases = true
	e := phl1Evaluator(cfg, SideLong, bars[from].OpenTime, px)
	steps := we679Run(t, e, "phl1_mnq_1m_20261027_0730ct.csv", time.October, 27, 9, 30, 9, 44, we679Levels6(), "key_level:29668.50:1792524600000", 8, 40, SideLong)
	got := phl1Find(steps, "09:44")
	if len(got.places) != 1 {
		t.Fatalf("WE6 09:44 must arm the mentor's buy stop; ladder:%s\nrefusals=%v", phl1Dump(steps), e.State.Refusals)
	}
	in := got.places[0]
	if in.Side != SideLong || !m464Approx(in.Price, 29645.00) || !m464Approx(in.Stop, 29632.25) {
		t.Fatalf("WE6 rung = %s %.2f stop %.2f, want long 29,645.00 / 29,632.25", in.Side, in.Price, in.Stop)
	}
}

// A touch INSIDE the pullback (after the old high formed) still owns the entry
// (m216 first-touch-fixed) — the release is only for a touch of an earlier leg.
func TestWE6TouchInsideThePullbackStillOwns(t *testing.T) {
	bars := phl1Tape(t, "phl1_mnq_1m_20261027_0730ct.csv")
	from := phl1At(t, bars, 2026, time.October, 27, 9, 30)
	px := bars[phl1At(t, bars, 2026, time.October, 27, 9, 44)].Close
	cfg := we679Cfg()
	cfg.RollingStaleTouchReleases = true
	e := phl1Evaluator(cfg, SideLong, bars[from].OpenTime, px)
	steps := we679Run(t, e, "phl1_mnq_1m_20261027_0730ct.csv", time.October, 27, 9, 30, 9, 44, we679Levels6(), "key_level:29668.50:1792524600000", 9, 41, SideLong)
	if got := phl1Find(steps, "09:44"); len(got.places) != 0 || e.State.Refusals["rolling_level_touch_owns"] == 0 {
		t.Fatalf("a touch inside the pullback must still own the entry: %s\n%v", phl1Dump(steps), e.State.Refusals)
	}
}

// Knob OFF reproduces today exactly: the stale touch owns.
func TestWE6KnobOffReproducesToday(t *testing.T) {
	bars := phl1Tape(t, "phl1_mnq_1m_20261027_0730ct.csv")
	from := phl1At(t, bars, 2026, time.October, 27, 9, 30)
	px := bars[phl1At(t, bars, 2026, time.October, 27, 9, 44)].Close
	cfg := we679Cfg()
	cfg.RollingStaleTouchReleases = false
	e := phl1Evaluator(cfg, SideLong, bars[from].OpenTime, px)
	steps := we679Run(t, e, "phl1_mnq_1m_20261027_0730ct.csv", time.October, 27, 9, 30, 9, 44, we679Levels6(), "key_level:29668.50:1792524600000", 8, 40, SideLong)
	if got := phl1Find(steps, "09:44"); len(got.places) != 0 || e.State.Refusals["rolling_level_touch_owns"] == 0 {
		t.Fatalf("knob OFF must keep today's rolling_level_touch_owns: %s\n%v", phl1Dump(steps), e.State.Refusals)
	}
}

func TestWE7TapeOldExtremeIsNotAKeypairLevel(t *testing.T) {
	bars := phl1Tape(t, "phl1_mnq_1m_20261030_1120ct.csv")
	from := phl1At(t, bars, 2026, time.October, 30, 13, 15)
	px := bars[phl1At(t, bars, 2026, time.October, 30, 13, 25)].Close
	cfg := we679Cfg()
	cfg.RollingKeypairTapeExempt = true
	e := phl1Evaluator(cfg, SideShort, bars[from].OpenTime, px)
	steps := we679Run(t, e, "phl1_mnq_1m_20261030_1120ct.csv", time.October, 30, 13, 15, 13, 25, we679Levels7(), "", 0, 0, "")
	got := phl1Find(steps, "13:25")
	if len(got.places) != 1 {
		t.Fatalf("WE7 13:25 must arm the mentor's sell stop; ladder:%s\nrefusals=%v", phl1Dump(steps), e.State.Refusals)
	}
	in := got.places[0]
	if in.Side != SideShort || !m464Approx(in.Price, 28316.25) || !m464Approx(in.Stop, 28331.75) {
		t.Fatalf("WE7 rung = %s %.2f stop %.2f, want short 28,316.25 / 28,331.75", in.Side, in.Price, in.Stop)
	}
}

func TestWE7KnobOffReproducesToday(t *testing.T) {
	bars := phl1Tape(t, "phl1_mnq_1m_20261030_1120ct.csv")
	from := phl1At(t, bars, 2026, time.October, 30, 13, 15)
	px := bars[phl1At(t, bars, 2026, time.October, 30, 13, 25)].Close
	cfg := we679Cfg()
	cfg.RollingKeypairTapeExempt = false
	e := phl1Evaluator(cfg, SideShort, bars[from].OpenTime, px)
	steps := we679Run(t, e, "phl1_mnq_1m_20261030_1120ct.csv", time.October, 30, 13, 15, 13, 25, we679Levels7(), "", 0, 0, "")
	if got := phl1Find(steps, "13:25"); len(got.places) != 0 || e.State.Refusals["rolling_keypair_candle_too_big"] == 0 {
		t.Fatalf("knob OFF must keep today's keypair refusal: %s\n%v", phl1Dump(steps), e.State.Refusals)
	}
}

// Both knobs ship OFF (today's behaviour): the worked examples do not
// reproduce end to end (5m box ban / mid-range ban, 2R PHL room come next).
func TestWE679KnobsDefaultOff(t *testing.T) {
	c := DefaultConfig()
	if c.RollingStaleTouchReleases || c.RollingKeypairTapeExempt {
		t.Fatalf("defaults must be OFF: %+v %+v", c.RollingStaleTouchReleases, c.RollingKeypairTapeExempt)
	}
}

// Review P2 (FIX-1112 B): with rolling_stale_touch_releases ON, a stale touch
// whose LEVEL order still RESTS must keep owning the entry — otherwise the
// level order and the rolling rung would both be live (double size, m216
// "never both").
func TestWE6StaleTouchWithRestingLevelArmStillOwns(t *testing.T) {
	bars := phl1Tape(t, "phl1_mnq_1m_20261027_0730ct.csv")
	from := phl1At(t, bars, 2026, time.October, 27, 9, 30)
	px := bars[phl1At(t, bars, 2026, time.October, 27, 9, 44)].Close
	cfg := we679Cfg()
	cfg.RollingStaleTouchReleases = true
	e := phl1Evaluator(cfg, SideLong, bars[from].OpenTime, px)
	key := "key_level:29668.50:1792524600000"
	e.State.LevelArms = map[string]LevelArm{key: {}}
	steps := we679Run(t, e, "phl1_mnq_1m_20261027_0730ct.csv", time.October, 27, 9, 30, 9, 44, we679Levels6(), key, 8, 40, SideLong)
	if got := phl1Find(steps, "09:44"); len(got.places) != 0 || e.State.Refusals["rolling_level_touch_owns"] == 0 {
		t.Fatalf("a resting level order must still own the entry: %s\n%v", phl1Dump(steps), e.State.Refusals)
	}
}

// Review P2/P3: rolling_keypair_tape_exempt is ROLLING-path only; the level
// path's phlLocationBan keeps the X4 sit-out for an old-extreme anchor.
func TestKeypairTapeExemptDoesNotApplyOnLevelPath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RollingKeypairTapeExempt = true
	cfg.PingPongCandleMaxPts = 100
	e := New(cfg)
	lvl := Level{Key: "oe", Kind: KindOldExtreme, Price: 28288.75}
	levels := []Level{lvl, {Key: "kl", Kind: KindKeyLevel, Price: 28283.00}}
	cur := market.Kline{High: 28320, Low: 28306.5}
	if name, _ := e.phlLocationBan(lvl, SideShort, 28316.25, cur, levels, nil); name != "keypair_candle_too_big" {
		t.Fatalf("level path must keep X4 for an old-extreme anchor, got %q", name)
	}
	if name, _ := e.phlLocationBanRolling(lvl, SideShort, 28316.25, cur, levels, nil); name == "keypair_candle_too_big" {
		t.Fatalf("rolling path with the knob ON must exempt the tape old extreme")
	}
}
