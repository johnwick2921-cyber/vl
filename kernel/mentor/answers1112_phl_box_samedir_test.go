package mentor

import (
	"fmt"
	"testing"
	"time"
)

// answers-1112 Q9 — knob phl_5m_box_same_dir_allowed (default OFF).
//
// The course (re-read in the Oct Day 4.3 .srt and .check.txt) [A]: inside a 5m
// inside-bar box you trade only the box's direction — "muốn tray bên trong
// InshaBar 05 phút thì anh chị phải đánh cùng chiều với InshaBar 05 phút"
// [Oct D4.3 @01:08:24-01:08:39]; "Chứ không có 5 phút đang Inshabashort vô đây
// đánh lệnh lon nha" [@01:09:46-01:09:51]. A same-direction lower-high is allowed
// to a trained eye ("đây là kinh nghiệm cho anh chị nào đã nhìn quen rồi / Chứ
// những anh chị khóa mới chưa nhìn quen thì đừng" [@01:10:58-01:11:03]) because it
// "vẫn là đúng theo cái Insa Bar cho lệnh Short bên ngoài" [@01:11:20-01:11:24],
// as a scalp with no target of its own, out near the box's far edge ("Chỉ cần
// xuống gần cạnh dưới bóc là anh chị đã out rồi. Chỉ là scap thôi"
// [@01:11:40-01:11:45]).
//
// The worked trades are the mentor's own Oct D2.3 WE6 / WE7 rungs on the real
// stored bars. The 5m boxes are the ones the report read from data.db with the
// bot's IsISB / ISBDirection: WE6 = the 10-27 09:35 5m candle (H 29,673.50,
// L 29,611.25), candle 1 (09:30) green -> Dir long; WE7 = the 10-30 13:20 5m
// candle (H 28,333.00, L 28,288.75), candle 1 (13:15) red -> Dir short.

type q9Case struct {
	name         string
	file         string
	d            int
	side         Side
	fromH, fromM int
	rungH, rungM int
	boxAt        [2]int // the 5m ISB candle's open (hh, mm)
	boxHi, boxLo float64
	entry, tgt   float64
}

var q9WE6 = q9Case{"WE6 10-27 PHL in a long box", "phl1_mnq_1m_20261027_0730ct.csv", 27, SideLong, 9, 30, 9, 44, [2]int{9, 35}, 29673.50, 29611.25, 29645.00, 29668.50}
var q9WE7 = q9Case{"WE7 10-30 PLH in a short box", "phl1_mnq_1m_20261030_1120ct.csv", 30, SideShort, 13, 15, 13, 25, [2]int{13, 20}, 28333.00, 28288.75, 28316.25, 28293.75}

// q9Run runs the production rollingPHLTick over the example's bars with the
// 5m ISB box standing (dir = the box's direction) and returns the rung.
func q9Run(t *testing.T, c q9Case, dir Side, hi, lo float64, mut func(*Config)) (phl1Rung, *Evaluator) {
	t.Helper()
	bars := phl1Tape(t, c.file)
	from := phl1At(t, bars, 2026, time.October, c.d, c.fromH, c.fromM)
	rung := phl1At(t, bars, 2026, time.October, c.d, c.rungH, c.rungM)
	cfg := DefaultConfig()
	if mut != nil {
		mut(&cfg)
	}
	e := phl1Evaluator(cfg, c.side, bars[from].OpenTime, bars[rung].Close)
	boxAt := time.Date(2026, time.October, c.d, c.boxAt[0], c.boxAt[1], 0, 0, ctime()).UnixMilli()
	e.State.ISBBox = &ISBBox{High: hi, Low: lo, Dir: dir, AtTime: boxAt}
	if cl := bars[rung].Close; !(cl > lo && cl < hi) {
		t.Fatalf("%s: the rung candle's close %.2f must sit inside the 5m box %.2f-%.2f", c.name, cl, lo, hi)
	}
	steps := phl1RunRolling(e, bars, from, rung)
	return phl1Find(steps, fmt.Sprintf("%02d:%02d", c.rungH, c.rungM)), e
}

func q9Refused(r phl1Rung, name string) bool {
	for _, x := range r.refuse {
		if x == name {
			return true
		}
	}
	return false
}

func TestAnswers1112PHLBoxSameDirDefaultOff(t *testing.T) {
	if DefaultConfig().PHL5mBoxSameDirAllowed {
		t.Fatal("PHL5mBoxSameDirAllowed must default OFF — \"experienced only\" [Oct D4.3 @01:10:58-01:11:03] is the owner's call")
	}
}

// TestAnswers1112PHLBoxSameDirWorkedTrades — WE6 (long in a long box) and WE7
// (short in a short box): OFF = today (rolling_phl_5m_box_ban, nothing armed);
// ON = the mentor's rung arms at his entry, the target already inside the box
// (WE6 old high 29,673.50 - 5 = 29,668.50 under the box top 29,673.50; WE7 old
// low 28,288.75 + 5 = 28,293.75 above the box bottom 28,288.75) so the cap
// leaves it alone.
func TestAnswers1112PHLBoxSameDirWorkedTrades(t *testing.T) {
	for _, c := range []q9Case{q9WE6, q9WE7} {
		t.Run(c.name, func(t *testing.T) {
			off, _ := q9Run(t, c, c.side, c.boxHi, c.boxLo, nil)
			if len(off.places) != 0 || !q9Refused(off, "rolling_phl_5m_box_ban") {
				t.Fatalf("%s OFF (default) must stay today: rolling_phl_5m_box_ban, nothing armed; got %+v refused %v", c.name, off.places, off.refuse)
			}
			on, e := q9Run(t, c, c.side, c.boxHi, c.boxLo, func(cf *Config) { cf.PHL5mBoxSameDirAllowed = true })
			if len(on.places) != 1 {
				t.Fatalf("%s ON: the same-direction rung must arm; got %+v refused %v ledger %v", c.name, on.places, on.refuse, e.State.Refusals)
			}
			in := on.places[0]
			if in.Side != c.side || !m464Approx(in.Price, c.entry) || !m464Approx(in.Target, c.tgt) {
				t.Fatalf("%s ON: armed %s %.2f target %.2f, want %s %.2f target %.2f", c.name, in.Side, in.Price, in.Target, c.side, c.entry, c.tgt)
			}
		})
	}
}

// TestAnswers1112PHLBoxOppositeDirStaysBanned — "Chứ không có 5 phút đang
// Inshabashort vô đây đánh lệnh lon nha" [Oct D4.3 @01:09:46-01:09:51]: the WE6
// long inside the same box read SHORT, and the WE7 short inside the same box
// read LONG, stay refused phl_5m_box_ban with the knob ON. A box with no
// direction (doji candle 1 never makes a box, but a hand-built one) fails closed.
func TestAnswers1112PHLBoxOppositeDirStaysBanned(t *testing.T) {
	for _, c := range []q9Case{q9WE6, q9WE7} {
		opp := SideShort
		if c.side == SideShort {
			opp = SideLong
		}
		for _, dir := range []Side{opp, ""} {
			on, _ := q9Run(t, c, dir, c.boxHi, c.boxLo, func(cf *Config) { cf.PHL5mBoxSameDirAllowed = true })
			if len(on.places) != 0 || !q9Refused(on, "rolling_phl_5m_box_ban") {
				t.Fatalf("%s in a %q box, knob ON: must stay rolling_phl_5m_box_ban; got %+v refused %v", c.name, dir, on.places, on.refuse)
			}
		}
	}
}

// TestAnswers1112PHLBoxSameDirTargetCappedAtBoxEdge — the scalp exits near the
// box's far edge ("Chỉ cần xuống gần cạnh dưới bóc là anh chị đã out rồi"
// [@01:11:40-01:11:45]). On the WE6 rung (entry 29,645.00, stop 29,632.25, risk
// 12.75) with the box top moved inside the old-high target [B: the box edges
// here are hypothetical, the real WE6 box top sits past the target]:
//   - top 29,662.00 -> target capped 29,662.00 (reward 17.00 = 1.33R) -> arms;
//   - top 29,655.00 -> target 29,655.00 (reward 10.00 < risk 12.75) -> refused
//     by the 1:1 floor (rolling_phl_target_below_floor), never armed past the box.
func TestAnswers1112PHLBoxSameDirTargetCappedAtBoxEdge(t *testing.T) {
	c := q9WE6
	on := func(cf *Config) { cf.PHL5mBoxSameDirAllowed = true }
	r, _ := q9Run(t, c, SideLong, 29662.00, c.boxLo, on)
	if len(r.places) != 1 || !m464Approx(r.places[0].Target, 29662.00) {
		t.Fatalf("box top 29,662: the target must be capped at the box edge 29,662.00; got %+v refused %v", r.places, r.refuse)
	}
	if rt := r.places[0].RunnerTarget; rt != 0 && rt > 29662.00 {
		t.Fatalf("box top 29,662: the runner must not ride past the box edge (scalp), RunnerTarget %.2f", rt)
	}
	r2, _ := q9Run(t, c, SideLong, 29655.00, c.boxLo, on)
	if len(r2.places) != 0 || !q9Refused(r2, "rolling_phl_target_below_floor") {
		t.Fatalf("box top 29,655 (reward 10 < risk 12.75): must be refused rolling_phl_target_below_floor; got %+v refused %v", r2.places, r2.refuse)
	}
}

// TestAnswers1112PHLBoxSameDirTickLevelAndRolling — the production Tick on
// rollingFixture (rolling entry 119, stop 112, old high 130, newest close 113):
//   - rolling path, long box 105-128 ON: arms, target capped at 128;
//   - rolling path, long box 105-125 ON: capped target 125 is 6 < risk 7 ->
//     rolling_phl_target_below_floor (no arm past the box);
//   - rolling path, long box 105-119 ON: the buy stop 119 is at the box top ->
//     rolling_phl_5m_box_scalp_no_room;
//   - level path (touch of 112): OFF = phl_5m_box_ban; ON with a long box the
//     ban is lifted and the target capped (125 -> phl_target_below_floor,
//     128 -> arms at 128); a short box stays phl_5m_box_ban;
//   - the 15m box ban is untouched by the knob.
func TestAnswers1112PHLBoxSameDirTickLevelAndRolling(t *testing.T) {
	// The box's 5m candle is the newest one (09:05): no closed 5m candle after
	// it, so Tick's merge / escape step leaves its edges as set (a box at
	// AtTime 0 would merge the 09:00 5m candle, H 130).
	e, bars, now := rollingFixture()
	at := bars[len(bars)-1].OpenTime
	e.Cfg.PHL5mBoxSameDirAllowed = true
	e.State.ISBBox = &ISBBox{High: 128, Low: 105, Dir: SideLong, AtTime: at}
	got := rollingPlaceEntries(e.Tick(bars, now))
	if len(got) != 1 || got[0].Target != 128 {
		t.Fatalf("rolling, long box 105-128, ON: want one arm with target capped 128; got %+v ledger %v", got, e.State.Refusals)
	}

	e, bars, now = rollingFixture()
	e.Cfg.PHL5mBoxSameDirAllowed = true
	e.State.ISBBox = &ISBBox{High: 125, Low: 105, Dir: SideLong, AtTime: at}
	if n := m436Armed(e.Tick(bars, now)); n != 0 || m436Count(e, "rolling_phl_target_below_floor") != 1 {
		t.Fatalf("rolling, long box 105-125, ON: capped target 125 (6 < risk 7) must be rolling_phl_target_below_floor; armed %d ledger %v", n, e.State.Refusals)
	}

	// ON, long box 105-119: the rung's buy stop 119 sits AT the box top — no
	// scalp left inside the box: rolling_phl_5m_box_scalp_no_room.
	e, bars, now = rollingFixture()
	e.Cfg.PHL5mBoxSameDirAllowed = true
	e.State.ISBBox = &ISBBox{High: 119, Low: 105, Dir: SideLong, AtTime: at}
	if n := m436Armed(e.Tick(bars, now)); n != 0 || m436Count(e, "rolling_phl_5m_box_scalp_no_room") != 1 {
		t.Fatalf("rolling, long box 105-119, ON: entry 119 at the box top must be rolling_phl_5m_box_scalp_no_room; armed %d ledger %v", n, e.State.Refusals)
	}

	e, bars, now = rollingFixture()
	e.State.ISBBox = &ISBBox{High: 125, Low: 105, Dir: SideLong, AtTime: at}
	if n := m436Armed(e.Tick(bars, now)); n != 0 || m436Count(e, "rolling_phl_5m_box_ban") != 1 {
		t.Fatalf("rolling, long box, OFF: today = rolling_phl_5m_box_ban; armed %d ledger %v", n, e.State.Refusals)
	}

	level := func(on bool, dir Side, hi float64) (*Evaluator, []Intent) {
		e, bars, now := rollingFixture()
		e.Cfg.RollingPHL = false
		e.Cfg.PHL5mBoxSameDirAllowed = on
		e.State.SeedLevels = append(e.State.SeedLevels, Level{Key: "support", Kind: KindKeyLevel, Price: 112})
		e.State.ISBBox = &ISBBox{High: hi, Low: 105, Dir: dir, AtTime: bars[len(bars)-1].OpenTime}
		return e, rollingPlaceEntries(e.Tick(bars, now))
	}
	if e, _ := level(false, SideLong, 125); m436Count(e, "phl_5m_box_ban") < 1 {
		t.Fatalf("level path OFF: today = phl_5m_box_ban; ledger %v", e.State.Refusals)
	}
	// ON, long box 105-125: the ban is lifted and the capped target 125 is
	// under the 1:1 floor — named phl_target_below_floor, nothing armed.
	if e, ins := level(true, SideLong, 125); m436Count(e, "phl_5m_box_ban") != 0 || len(ins) != 0 || m436Count(e, "phl_target_below_floor") != 1 {
		t.Fatalf("level path ON, long box 105-125: want the ban lifted and phl_target_below_floor (capped 125); armed %+v ledger %v", ins, e.State.Refusals)
	}
	// ON, long box 105-128: the level PHL arms with its target capped at 128.
	if e, ins := level(true, SideLong, 128); len(ins) != 1 || ins[0].Target != 128 || ins[0].RunnerTarget != 128 {
		t.Fatalf("level path ON, long box 105-128: want one arm, target and runner capped 128; got %+v ledger %v", ins, e.State.Refusals)
	}
	if e, ins := level(true, SideShort, 128); m436Count(e, "phl_5m_box_ban") < 1 || len(ins) != 0 {
		t.Fatalf("level path ON, long PHL in a SHORT box: must stay phl_5m_box_ban; armed %+v ledger %v", ins, e.State.Refusals)
	}

	e, bars, now = rollingFixture()
	e.Cfg.PHL5mBoxSameDirAllowed = true
	e.State.ISBBox15m = &ISBBox{High: 125, Low: 105, Dir: SideLong, AtTime: at}
	if n := m436Armed(e.Tick(bars, now)); n != 0 || m436Count(e, "rolling_phl_15m_box_ban") != 1 {
		t.Fatalf("the 15m box ban is not covered by the knob: want rolling_phl_15m_box_ban; armed %d ledger %v", n, e.State.Refusals)
	}
}
