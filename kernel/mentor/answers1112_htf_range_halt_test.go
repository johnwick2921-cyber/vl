package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// answers-1112 Q1 extra rule — knob htf_opposed_range_halt_pts (DEFAULT 0 =
// OFF). "Còn nếu như 2 khung giờ đang ngược nhau / Mà khung daily nó đã chạy
// được 300-400 điểm rồi / Tắt máy nghỉ luôn cho em" [Sept D5.1 p1
// @19:22-19:26]: 4h and 1h opposed + the daily candle already 300-400 pts →
// stop for the day. The daily candle is measured from the Asia open through
// pre-market [@16:55-17:00, @17:27-17:50].
//
// The video's own day is Tue 15 Sep 2026 (MNQZ2026): frame part1 s_01038
// (@17:18) measures 265.50 / 1,062 ticks from 29,495.50 (Mon 14 Sep 18:40) to
// 29,230.25 (Tue 02:50), and frame @18:10 measures −286.75 / −1,147 for the
// whole daily candle by the evening; his frames read "Khung 4h Trigger Sale /
// Khung 1h Trigger Buy" [@18:17-18:22]. The recorded tapes of that day
// (testdata mnq_5m_2026-09-15_globex + mnq_1m_2026-09-15_rth) read 264.5 at
// 08:30 (29,495.75 − 29,231.25) and 287.00 at the 14:52 low (29,208.75).

func q1HisDay(t *testing.T) []market.Kline {
	t.Helper()
	g := loadFixture(t, "mnq_5m_2026-09-15_globex", "5m")
	r := loadFixture(t, "mnq_1m_2026-09-15_rth", "1m")
	return append(append([]market.Kline{}, g...), r...)
}

func q1At(h, m int) int64 { return auditMs(2026, 9, 15, h, m, 0) }

// q1Conflict is his read: 4h trigger SELL, 1h trigger BUY drawn after it.
func q1Conflict() HTF {
	return HTF{
		FourH: TriggerLine{Dir: SideShort, Price: 29480, MovedAt: auditMs(2026, 9, 14, 21, 0, 0)},
		OneH:  TriggerLine{Dir: SideLong, Price: 29300, MovedAt: auditMs(2026, 9, 15, 5, 0, 0)},
	}
}

func TestAnswers1112HTFRangeHaltDefaultOff(t *testing.T) {
	if v := DefaultConfig().HTFOpposedRangeHaltPts; v != 0 {
		t.Fatalf("HTFOpposedRangeHaltPts default = %v, want 0 (OFF): the citation gives a range 300-400, not one number, and it is September [Sept D5.1 p1 @19:22-19:26]", v)
	}
}

// The range is the Globex DAY (17:00 CT → now), not frozen at 08:30 the way
// the §7 GlobexMeasure is, and not RTH-only.
func TestAnswers1112HTFRangeHaltGlobexDayOnHisTape(t *testing.T) {
	bars := q1HisDay(t)
	for _, c := range []struct {
		h, m          int
		day, frozen   float64
		wantHi, wantL float64
	}{
		{8, 30, 264.5, 264.5, 29495.75, 29231.25},
		{9, 52, 269.0, 264.5, 29495.75, 29226.75}, // the 09:51 bar's low 29,226.75
		{15, 0, 287.0, 264.5, 29495.75, 29208.75}, // the 14:52 low — frame @18:10 reads −286.75
	} {
		now := q1At(c.h, c.m)
		d := GlobexDayMeasure(bars, now, ctime())
		if !d.OK || math.Abs(d.Run-c.day) > 1e-9 || d.Hi != c.wantHi || d.Lo != c.wantL {
			t.Fatalf("%02d:%02d Globex day: %+v, want run %.2f hi %.2f lo %.2f", c.h, c.m, d, c.day, c.wantHi, c.wantL)
		}
		if f := GlobexMeasure(bars, now, ctime()); !f.OK || math.Abs(f.Run-c.frozen) > 1e-9 {
			t.Fatalf("%02d:%02d §7 GlobexMeasure must stay frozen at the 08:30 value %.2f, got %+v", c.h, c.m, c.frozen, f)
		}
	}
	// RTH-only would be 0 at the 08:30 read — the rule could never fire then.
	var rth []market.Kline
	for _, b := range bars {
		if b.OpenTime >= q1At(8, 30) {
			rth = append(rth, b)
		}
	}
	if d := GlobexDayMeasure(rth, q1At(9, 52), ctime()); d.OK {
		t.Fatalf("a feed that starts at 08:30 does not cover the daily candle and must read not-measured (A6), got %+v", d)
	}
}

// The pure latch on his tape with his 4h SELL / 1h BUY read, minute by minute
// from 08:00 to 16:59.
func TestAnswers1112HTFRangeHaltLatchOnHisDay(t *testing.T) {
	bars := q1HisDay(t)
	first := func(h HTF, pts float64) (HTFRangeHalt, int64) {
		var l HTFRangeHalt
		for now := q1At(8, 0); now <= q1At(16, 59); now += 60_000 {
			var fired bool
			if l, fired = htfRangeHaltLatch(l, h, bars, now, pts); fired {
				return l, now
			}
		}
		return l, 0
	}
	// OFF (default 0): never.
	if _, at := first(q1Conflict(), 0); at != 0 {
		t.Fatal("knob 0 (default) must never halt — today's behaviour")
	}
	// 300, the lower edge of his "300-400": his own example day never gets
	// there (287.00 max) — the halt never fires on it.
	if _, at := first(q1Conflict(), 300); at != 0 {
		t.Fatal("knob 300: his 15 Sep daily candle ran 287.00 at most — no halt")
	}
	// 265: not at 08:30 (264.5), but at the 09:51 bar's close, when the RTH
	// low 29,226.75 takes the daily candle to 269.00.
	l, at := first(q1Conflict(), 265)
	if at != q1At(9, 52) {
		t.Fatalf("knob 265: latched at %s, want 09:52 (the 09:51 bar's close)", time.UnixMilli(at).In(ctime()).Format("15:04"))
	}
	if l.Key != "2026-09-15" || l.RunPts != 269.0 || l.FourHDir != SideShort || l.OneHDir != SideLong {
		t.Fatalf("latch %+v, want key 2026-09-15 run 269.00 4h short / 1h long", l)
	}
	// 250: the 08:30 read itself (264.5 ≥ 250). Not before — pre-open the
	// §7 live verdict governs.
	if _, at := first(q1Conflict(), 250); at != q1At(8, 30) {
		t.Fatalf("knob 250: latched at %s, want 08:30", time.UnixMilli(at).In(ctime()).Format("15:04"))
	}
	// Frames AGREE (both sell): never — "Nếu như 2 khung trigger cùng với
	// nhau Ok Mình đánh theo khung 4 giờ" [@19:11-19:17].
	agree := q1Conflict()
	agree.OneH.Dir = SideShort
	if _, at := first(agree, 250); at != 0 {
		t.Fatal("4h and 1h agree: the halt never fires")
	}
	// 1h older than the 4h (an earlier 1h trigger is ignored — "silent"):
	// not a conflict, never.
	silent := q1Conflict()
	silent.OneH.MovedAt = silent.FourH.MovedAt - 60_000
	if _, at := first(silent, 250); at != 0 {
		t.Fatal("a 1h trigger older than the 4h is silent, not opposed: no halt")
	}
}

// ── production Tick: the halt refuses new entries for the REST of the day ──
//
// Fixture scaffolding [C]: the s_02392 short ISB (q3Run geometry, Oct D2.2
// @39:00-39:59 — entry 28,709.50, stop 28,735.75) at 09:01-09:02 CT on the
// video's day, plus a 17:00 CT Globex-open bar whose high sets the daily
// candle at exactly 300.00 (his "300" — 29,000.00 down to the mother's
// 28,700.00 low). Tick A reads the 4h SELL / 1h BUY conflict; then the 1h
// stops opposing (the case-2 / agree read) and Tick B sees the ISB.
func q1HaltRun(t *testing.T, pts float64, conflict bool) (*Evaluator, []Intent) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.HTFGateNewsOnly = true
	cfg.HTFOpposedRangeHaltPts = pts
	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideShort, Price: 28800}
	open := auditMs(2026, 9, 14, 17, 0, 0)
	globexOpen := market.Kline{OpenTime: open, CloseTime: open + 59_999, Open: 28995, High: 29000, Low: 28990, Close: 28992}
	mother := rthBars(1, 28748, 28750, 28700, 28740)
	isb := rthBars(2, 28730, q3Stop-1.5, q3Entry+1.5, 28725)
	nowA := mother.CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(nowA), High: 28900, Low: 28850, Drawn: true, Escaped: SideShort}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(nowA).In(ctime())), Verdict: DayTrade}
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA9 = 0
	// Tick A: the mother bar closes; the 4h and 1h are opposed (or not).
	// LastBucket pins each line to the tape's own bucket, so the two bars
	// never move the injected read (as dayoffRecheckEval does).
	fourH := TriggerLine{Dir: SideShort, Price: 28800, MovedAt: open, LastBucket: fourHBucketStart(nowA, ctime())}
	e.State.HTF = HTF{FourH: fourH}
	if conflict {
		e.State.HTF.OneH = TriggerLine{Dir: SideLong, Price: 28600, MovedAt: open + 3_600_000, LastBucket: (nowA / 3_600_000) * 3_600_000}
	}
	e.Tick([]market.Kline{globexOpen, mother}, nowA)
	if conflict && !HTFConflict(e.State.HTF) {
		t.Fatalf("fixture: Tick A must still read the 4h/1h conflict, got %+v", e.State.HTF)
	}
	// The 1h stops opposing: 4h SELL, 1h silent (case 2) — today trading resumes.
	e.State.HTF = HTF{FourH: fourH}
	ins := e.Tick([]market.Kline{globexOpen, mother, isb}, isb.CloseTime+1)
	return e, ins
}

func TestAnswers1112HTFRangeHaltRefusesRestOfDay(t *testing.T) {
	// OFF (default): the conflict sat earlier; the ISB trades once it is gone.
	e, ins := q1HaltRun(t, 0, true)
	if in := d2g3Entry(ins); in == nil || in.Price != q3Entry || in.Stop != q3Stop {
		t.Fatalf("knob 0 (today): the ISB is traded after the conflict ends; intents %+v refusals %v", ins, e.State.Refusals)
	}
	if e.State.HTFRangeHaltLatch().Key != "" || e.State.Refusals["isb_htf_opposed_range_halt"] != 0 {
		t.Fatalf("knob 0 must not latch: %+v %v", e.State.HTFRangeHaltLatch(), e.State.Refusals)
	}
	// 300 (his lower edge), daily candle 300.00, 4h/1h opposed at 09:02 →
	// halted; the ISB at 09:03 is refused by name even though the 1h no
	// longer opposes.
	e, ins = q1HaltRun(t, 300, true)
	if in := d2g3Entry(ins); in != nil {
		t.Fatalf("knob 300: opposed + 300.00 run → no new entries for the rest of the day; got %+v", *in)
	}
	if n := e.State.Refusals["isb_htf_opposed_range_halt"]; n != 1 {
		t.Fatalf("want isb_htf_opposed_range_halt=1, ledger %v", e.State.Refusals)
	}
	h := e.State.HTFRangeHaltLatch()
	if h.Key != "2026-09-15" || h.RunPts != 300 || h.At != q1At(9, 2) || h.FourHDir != SideShort || h.OneHDir != SideLong {
		t.Fatalf("latch %+v, want 2026-09-15 @09:02 run 300.00 short/long", h)
	}
	if e.State.Counters["htf_opposed_range_halt_latched"] != 1 {
		t.Fatalf("the latch is counted once: %v", e.State.Counters)
	}
	// 300.25: the 300.00 candle is below it → traded.
	if e, ins = q1HaltRun(t, 300.25, true); d2g3Entry(ins) == nil {
		t.Fatalf("knob 300.25 > run 300.00: traded; refusals %v", e.State.Refusals)
	}
	// No conflict at all: traded.
	if e, ins = q1HaltRun(t, 300, false); d2g3Entry(ins) == nil || e.State.HTFRangeHaltLatch().Key != "" {
		t.Fatalf("frames never opposed: no halt; latch %+v refusals %v", e.State.HTFRangeHaltLatch(), e.State.Refusals)
	}
	// The halt is for THIS trading day only: from the 17:00 CT roll it is off.
	e, _ = q1HaltRun(t, 300, true)
	if !e.htfRangeHaltActive(q1At(16, 59)) || e.htfRangeHaltActive(q1At(17, 0)) {
		t.Fatal("the halt holds until 16:59 CT and ends at the 17:00 CT day roll")
	}
}

// The swing is exempt, as it is from the §7 DayOff; non-entry actions pass.
func TestAnswers1112HTFRangeHaltLeaveScope(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HTFOpposedRangeHaltPts = 300
	e := New(cfg)
	now := q1At(10, 0)
	e.State.HTFRangeHalt = &HTFRangeHalt{Key: "2026-09-15"}
	out := e.htfRangeHaltLeave([]Intent{
		{Action: PlaceStopEntry, Setup: "PHL"},
		{Action: PlaceStopLimitEntry, Setup: "ISB"},
		{Action: PlaceStopEntry, Setup: "BOX"},
		{Action: PlaceStopEntry, Setup: "SWING4H"},
		{Action: CancelArm, Setup: "ISB"},
	}, now)
	if len(out) != 2 || out[0].Setup != "SWING4H" || out[1].Action != CancelArm {
		t.Fatalf("kept %+v, want the swing entry and the cancel only", out)
	}
	for _, k := range []string{"phl_htf_opposed_range_halt", "isb_htf_opposed_range_halt", "box_htf_opposed_range_halt"} {
		if e.State.Refusals[k] != 1 {
			t.Fatalf("want %s=1, ledger %v", k, e.State.Refusals)
		}
	}
	// Knob back to 0 with a stale latch: today exactly (nothing dropped).
	e.Cfg.HTFOpposedRangeHaltPts = 0
	if out := e.htfRangeHaltLeave([]Intent{{Action: PlaceStopEntry, Setup: "PHL"}}, now); len(out) != 1 {
		t.Fatal("knob 0: the leave filter drops nothing")
	}
}

// ── restart: the seed restores the halt the live tick latched ──────────────
//
// Synthetic Globex day [C] in the shape of his: 4h SELL from 21:00 (the
// 21:00-01:00 bucket breaks the 17:00 bucket's low), lows to 29,200.00 by
// 04:00 (daily candle 295.75 from the 29,495.75 open — his tape's high), and
// at 09:00 the 1h breaks the 08:00 hour's high (1h BUY) while the 4h holds —
// opposed from the 09:00 bar on. The production Tick runs every minute; the
// seed, given only the bars, must rebuild the same latch.
func q1HourTape() []market.Kline {
	type hr struct{ h, l, c float64 }
	spec := []hr{
		{29495.75, 29450, 29460}, {29480, 29452, 29455}, {29470, 29455, 29460}, {29465, 29456, 29458}, // 17-20
		{29460, 29400, 29410}, {29440, 29390, 29400}, {29430, 29380, 29390}, {29420, 29370, 29380}, // 21-00 (4h SELL)
		{29390, 29300, 29310}, {29350, 29260, 29270}, {29300, 29200, 29220}, {29290, 29205, 29240}, // 01-04 (low 29,200)
		{29260, 29210, 29240}, {29255, 29215, 29240}, {29250, 29220, 29240}, {29245, 29225, 29240}, // 05-08
		{29250, 29230, 29248}, {29249, 29231, 29240}, // 09 (1h BUY, 4h holds), 10
	}
	t0 := auditMs(2026, 9, 14, 17, 0, 0)
	var bars []market.Kline
	for i, s := range spec {
		for m := 0; m < 60; m++ {
			ot := t0 + int64(i*60+m)*60_000
			bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: s.c, High: s.h, Low: s.l, Close: s.c})
		}
	}
	return bars
}

func TestAnswers1112HTFRangeHaltSeedRestoresLiveLatch(t *testing.T) {
	bars := q1HourTape()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.HTFOpposedRangeHaltPts = 290
	live := New(cfg)
	stop := q1At(10, 30)
	var n int
	for i := range bars {
		if bars[i].CloseTime+1 > stop {
			break
		}
		live.Tick(bars[:i+1], bars[i].CloseTime+1)
		n = i + 1
	}
	got := live.State.HTFRangeHaltLatch()
	if got.Key != "2026-09-15" || got.At != q1At(9, 1) || got.RunPts != 295.75 || got.FourHDir != SideShort || got.OneHDir != SideLong {
		t.Fatalf("live latch %+v (4h %+v / 1h %+v), want 2026-09-15 @09:01 run 295.75 short/long", got, live.State.HTF.FourH, live.State.HTF.OneH)
	}
	// Restart at 10:30: the seed alone rebuilds it.
	seeded := New(cfg)
	SeedHTFRangeHalt(seeded, bars[:n], stop)
	s := seeded.State.HTFRangeHaltLatch()
	if s.Key != got.Key || s.At != got.At || s.RunPts != got.RunPts || !s.Restored {
		t.Fatalf("seed %+v, want the live latch %+v (Restored)", s, got)
	}
	// Above the run (300): neither path latches.
	cfg.HTFOpposedRangeHaltPts = 300
	off := New(cfg)
	SeedHTFRangeHalt(off, bars[:n], stop)
	if off.State.HTFRangeHaltLatch().Key != "" {
		t.Fatalf("knob 300 > run 295.75: no restore, got %+v", off.State.HTFRangeHaltLatch())
	}
	// Knob 0: the seed does nothing.
	cfg.HTFOpposedRangeHaltPts = 0
	zero := New(cfg)
	SeedHTFRangeHalt(zero, bars[:n], stop)
	if zero.State.HTFRangeHaltLatch().Key != "" {
		t.Fatal("knob 0: the seed restores nothing")
	}
}
