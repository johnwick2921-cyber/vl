package mentor

import (
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// m462 — release #59: the 4h/1h trigger gate applies ALL SESSION by default.
//
// D5.1 (the final review) overrides the D4.4 "news first" caveat:
//   - D5.1-R9 [D5.1 p1 @08:46–09:13, @11:27]: "Lúc nào khi anh chị open ra,
//     không cần phải là news… nếu là news thì 1h trước news và 4h trước news;
//     còn nếu là open cũng vậy."
//   - D5.1-R11 [D5.1 p1 @08:29–09:52, @11:27–11:50]: "Trigger 4h, trigger 1h nằm
//     ngược nhau… cứng ngắt nằm giữa cái range luôn, mình không làm được gì hết
//     trơn." — on 15 Sep the conflict held until 11–12 h, far past 09:30.
//   - D5.1-R20 [D5.1 p1 @19:11–20:10] decision matrix (agree → trade with them).
//   - D4.4-R11/R12/R13 [D4.4 p1 @14:08–15:34]: case 1 agree → that side only;
//     case 2 4h triggered, 1h nothing → follow the 4h; case 3 opposite → sit out
//     until the 1h flips to the 4h.
//
// The bot traded through conflicts because HTFGateNewsOnly (default ON) only
// applied the gate in the print window and the 08:30–09:30 open: trades 669,
// 677, 698, 705 (case 3) and 716 (case 1 short, long entry) — tradecheck
// chunk11/13/15/16/19. SWING4H (715) is NOT gated: its direction is the 4h
// EMA34 (D5.2-R7 [D5.2 p3 @15:04–18:25] "4 giờ đụng resistance thì mình đánh
// xuống").

// TestM462DefaultGateIsAllDay — the DEFAULT applies the 4h/1h gate at every
// minute of the session; the stored news-only value stays reachable.
// RED before m462: DefaultConfig().HTFGateNewsOnly was true and the gate was
// off at 02:38 / 13:30 / 00:36 CT.
func TestM462DefaultGateIsAllDay(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HTFGateNewsOnly = false // D2-G7 oct round 2: the default is now true (TestD2G7HTFGateOctoberFiveMinOnly); this pins the knob's all-session value
	for _, at := range []struct {
		name string
		ms   int64
	}{
		{"669 arm 10-21 02:37 CT", auditMs(2026, 10, 21, 2, 37, 56)},
		{"677 arm 10-21 14:21 CT", auditMs(2026, 10, 21, 14, 21, 59)},
		{"698 arm 10-23 13:29 CT", auditMs(2026, 10, 23, 13, 29, 59)},
		{"705 arm 10-28 00:35 CT", auditMs(2026, 10, 28, 0, 35, 58)},
		{"716 arm 10-29 01:51 CT", auditMs(2026, 10, 29, 1, 51, 59)},
		{"evening 18:00 CT", auditMs(2026, 10, 27, 18, 0, 0)},
	} {
		if !HTFGateActive(at.ms, cfg) {
			t.Fatalf("%s: the default gate must be ACTIVE (all day)", at.name)
		}
	}
	// Stored values are sacred: an explicit stored news-only (true) keeps the
	// old windows — off at 13:30 CT, on at 08:45 CT.
	cfg.HTFGateNewsOnly = true
	if HTFGateActive(auditMs(2026, 10, 23, 13, 30, 0), cfg) {
		t.Fatal("an explicit stored news-only must keep the gate OFF at 13:30 CT")
	}
	if !HTFGateActive(auditMs(2026, 10, 23, 8, 45, 0), cfg) {
		t.Fatal("an explicit stored news-only must keep the gate ON in the 08:30–09:30 open")
	}
}

// TestM462HTFEntryCheckCases — the ONE shared check, every case of the
// mentor's screen [D4.4 p1 @14:08–15:34] and its named kind.
func TestM462HTFEntryCheckCases(t *testing.T) {
	long4 := TriggerLine{Dir: SideLong, Price: 100, MovedAt: 1000}
	cases := []struct {
		name string
		h    HTF
		side Side
		want HTFRefusal
	}{
		{"no 4h trigger", HTF{}, SideLong, HTFRefuseNo4h},
		{"case 1 agree, with side", HTF{FourH: long4, OneH: TriggerLine{Dir: SideLong, MovedAt: 2000}}, SideLong, ""},
		{"case 1 agree, against side", HTF{FourH: long4, OneH: TriggerLine{Dir: SideLong, MovedAt: 2000}}, SideShort, HTFRefuseSide},
		{"case 2 1h silent (none), with side", HTF{FourH: long4}, SideLong, ""},
		{"case 2 1h silent (older), with side", HTF{FourH: long4, OneH: TriggerLine{Dir: SideShort, MovedAt: 500}}, SideLong, ""},
		{"case 2 against side", HTF{FourH: long4}, SideShort, HTFRefuseSide},
		{"case 3 conflict, long", HTF{FourH: long4, OneH: TriggerLine{Dir: SideShort, MovedAt: 2000}}, SideLong, HTFRefuseConflict},
		{"case 3 conflict, short", HTF{FourH: long4, OneH: TriggerLine{Dir: SideShort, MovedAt: 2000}}, SideShort, HTFRefuseConflict},
		{"case 3 conflict, no side (setup-wide)", HTF{FourH: long4, OneH: TriggerLine{Dir: SideShort, MovedAt: 2000}}, "", HTFRefuseConflict},
		{"gate off (stored news-only, outside the windows)", HTF{FourH: long4, OneH: TriggerLine{Dir: SideShort, MovedAt: 2000}, GateOff: true}, SideShort, ""},
	}
	for _, c := range cases {
		got, why := HTFEntryCheck(c.h, c.side)
		if got != c.want {
			t.Fatalf("%s: HTFEntryCheck = %q (%s), want %q", c.name, got, why, c.want)
		}
		if got != "" && why == "" {
			t.Fatalf("%s: a refusal must carry its course reason", c.name)
		}
	}
}

// TestM462RealTradesRefusedUnderDefault pins the five real trades the video
// check flagged (VL-MENTOR-VIDEO-CHECK.md item 3; tradecheck chunk11 #669,
// chunk13 #677, chunk15 #698, chunk16 #705, chunk19 #716). The 4h/1h state
// is the LIVE one at the arm minute: armed_orders.mentor_snapshot_json
// (rows 444, 467, 486 — "conflict" means !oneHSilent, so the 1h moved at or
// after the 4h) and the bot's own trigger-move log lines for rows 396/408
// (10-21, before the snapshot column existed). Under the DEFAULT config the
// gate is active at that minute and the entry is refused by NAME.
// RED before m462: GateOff=true at every one of these minutes → "" (allowed).
func TestM462RealTradesRefusedUnderDefault(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HTFGateNewsOnly = false // D2-G7 oct round 2: these pins exercise the September all-session gate (knob false)
	ct := func(y int, mo time.Month, d, hh, mm int) int64 { return auditMs(y, mo, d, hh, mm, 0) }
	cases := []struct {
		pos   int
		arm   int64
		side  Side
		htf   HTF
		want  HTFRefusal
		setup string
	}{
		// #669 ISB LONG @29900, arm 10-21 02:37:56 CT: 4h short @29845.25 since
		// 10-20 21:00 CT; 1h moved short→LONG @29870.25 at 02:10 CT (log 02:10:56).
		{669, auditMs(2026, 10, 21, 2, 37, 56), SideLong, HTF{
			FourH: TriggerLine{Dir: SideShort, Price: 29845.25, MovedAt: ct(2026, 10, 20, 21, 0)},
			OneH:  TriggerLine{Dir: SideLong, Price: 29870.25, MovedAt: ct(2026, 10, 21, 2, 10)}}, HTFRefuseConflict, "isb"},
		// #677 ISB LONG @29991, arm 10-21 14:21:59 CT: 4h LONG @29918 since
		// 09:02 CT; 1h SHORT @30046.75 since 13:00 CT.
		{677, auditMs(2026, 10, 21, 14, 21, 59), SideLong, HTF{
			FourH: TriggerLine{Dir: SideLong, Price: 29918, MovedAt: ct(2026, 10, 21, 9, 2)},
			OneH:  TriggerLine{Dir: SideShort, Price: 30046.75, MovedAt: ct(2026, 10, 21, 13, 0)}}, HTFRefuseConflict, "isb"},
		// #698 ISB LONG @29189.5, arm 10-23 13:29:59 CT: snapshot row 444
		// trigger_4h long @29175.75, trigger_1h short @29216.25, htf_verdict conflict.
		{698, auditMs(2026, 10, 23, 13, 29, 59), SideLong, HTF{
			FourH: TriggerLine{Dir: SideLong, Price: 29175.75, MovedAt: ct(2026, 10, 23, 9, 0)},
			OneH:  TriggerLine{Dir: SideShort, Price: 29216.25, MovedAt: ct(2026, 10, 23, 11, 0)}}, HTFRefuseConflict, "isb"},
		// #705 PLH SHORT @29917.25, arm 10-28 00:35:58 CT: snapshot row 467
		// trigger_4h long @29687.75, trigger_1h short @29888.50 (moved 00:00 CT,
		// log 10-28 00:00:58), htf_verdict conflict.
		{705, auditMs(2026, 10, 28, 0, 35, 58), SideShort, HTF{
			FourH: TriggerLine{Dir: SideLong, Price: 29687.75, MovedAt: ct(2026, 10, 27, 17, 0)},
			OneH:  TriggerLine{Dir: SideShort, Price: 29888.50, MovedAt: ct(2026, 10, 28, 0, 0)}}, HTFRefuseConflict, "isbrev"},
		// #716 ISB LONG @29655.75, arm 10-29 01:51:59 CT: snapshot row 486
		// trigger_4h short @29784, trigger_1h short @29632.75, htf_verdict
		// agree_short — case 1 SHORT, so the LONG is against the 4h.
		{716, auditMs(2026, 10, 29, 1, 51, 59), SideLong, HTF{
			FourH: TriggerLine{Dir: SideShort, Price: 29784, MovedAt: ct(2026, 10, 28, 21, 0)},
			OneH:  TriggerLine{Dir: SideShort, Price: 29632.75, MovedAt: ct(2026, 10, 29, 1, 0)}}, HTFRefuseSide, "isb"},
	}
	for _, c := range cases {
		h := c.htf
		h.GateOff = !HTFGateActive(c.arm, cfg) // exactly as Tick sets it
		got, why := HTFEntryCheck(h, c.side)
		if got != c.want {
			t.Fatalf("#%d: HTFEntryCheck = %q (%s), want %q — the default gate must refuse it", c.pos, got, why, c.want)
		}
		if name := c.setup + "_" + string(got); !strings.Contains(name, "_htf_") {
			t.Fatalf("#%d: refusal name %q", c.pos, name)
		}
		// The stored news-only value (the pre-m462 default) let it through.
		old := cfg
		old.HTFGateNewsOnly = true
		h.GateOff = !HTFGateActive(c.arm, old)
		if got, _ := HTFEntryCheck(h, c.side); got != "" {
			t.Fatalf("#%d: control — under news-only the gate was off at this minute (that is the bug); got %q", c.pos, got)
		}
	}
}

// shiftTo moves a bar slice so its first bar opens at the given CT clock on
// the same calendar day (2026-09-15, a Tuesday with no print window).
func m462ShiftTo(bars []market.Kline, hh, mm int) []market.Kline {
	t0 := time.Date(2026, time.September, 15, hh, mm, 0, 0, ctime()).UnixMilli()
	shift := t0 - bars[0].OpenTime
	out := make([]market.Kline, len(bars))
	for i, b := range bars {
		b.OpenTime += shift
		b.CloseTime += shift
		out[i] = b
	}
	return out
}

func m462Conflict(four, one Side) HTF {
	return HTF{
		FourH: TriggerLine{Dir: four, Price: 1, MovedAt: auditMs(2026, 9, 15, 1, 0, 0)},
		OneH:  TriggerLine{Dir: one, Price: 1, MovedAt: auditMs(2026, 9, 15, 6, 0, 0)},
	}
}

func m462Entries(ins []Intent, setup string, side Side) int {
	n := 0
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == setup && in.Side == side {
			n++
		}
	}
	return n
}

// TestM462ISBConflictRefusedAllDay — the ISB path, DEFAULT config, 13:30 CT
// (outside the old news/open windows): a 4h/1h conflict refuses the ISB
// "isb_htf_conflict" (the 698 shape); case 1 agree trades.
func TestM462ISBConflictRefusedAllDay(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HTFGateNewsOnly = false // D2-G7 oct round 2: these pins exercise the September all-session gate (knob false)
	cfg.Enabled = true
	cfg.Exec2mAfter30m = false // the 1m isbFixture read
	run := func(h HTF) (*Evaluator, []Intent) {
		e := newISBEval(cfg)
		e.State.HTF = h
		bars := m462ShiftTo(isbFixture(), 13, 30)
		now := bars[len(bars)-1].CloseTime + 1
		e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
		return e, e.Tick(bars, now)
	}
	e, ins := run(m462Conflict(SideLong, SideShort))
	if m462Entries(ins, "ISB", SideLong) != 0 {
		t.Fatalf("13:30 CT 4h long / 1h short: the ISB must sit out (D5.1-R11); intents %+v", ins)
	}
	if e.State.Refusals["isb_htf_conflict"] == 0 {
		t.Fatalf("the conflict refusal must be NAMED isb_htf_conflict; ledger %v", e.State.Refusals)
	}
	// case 1: agree long → trades.
	e, ins = run(m462Conflict(SideLong, SideLong))
	if m462Entries(ins, "ISB", SideLong) != 1 {
		t.Fatalf("13:30 CT case 1 (4h long + 1h long) must still trade the long ISB; intents %+v refusals %v", ins, e.State.Refusals)
	}
	// case 1 short vs a long ISB (the 716 shape) → side mismatch, named.
	e, ins = run(m462Conflict(SideShort, SideShort))
	if m462Entries(ins, "ISB", SideLong) != 0 || e.State.Refusals["isb_htf_side_mismatch"] == 0 {
		t.Fatalf("13:30 CT case 1 SHORT: a long ISB must be refused isb_htf_side_mismatch; intents %+v ledger %v", ins, e.State.Refusals)
	}
	// stored news-only (explicit true): the old behaviour — the conflict ISB trades.
	cfg2 := cfg
	cfg2.HTFGateNewsOnly = true
	e = newISBEval(cfg2)
	e.State.HTF = m462Conflict(SideLong, SideShort)
	bars := m462ShiftTo(isbFixture(), 13, 30)
	now := bars[len(bars)-1].CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	if ins := e.Tick(bars, now); m462Entries(ins, "ISB", SideLong) != 1 {
		t.Fatalf("explicit stored news-only must keep the old off-window behaviour; intents %+v ledger %v", ins, e.State.Refusals)
	}
}

// m462PHL drives the level PHL fixture of TestHTFGateNewsOnlyTick at 10:05 CT
// (outside the old windows) under the DEFAULT config with the given HTF.
func m462PHL(h HTF) (*Evaluator, []Intent) {
	return m462PHLCfg(h, func(c *Config) { c.HTFGateNewsOnly = false }) // D2-G7: the all-session gate
}

// m462PHLCfg is m462PHL with a config mutator (round 2 D2-G7: the knob both ways).
func m462PHLCfg(h HTF, mut func(*Config)) (*Evaluator, []Intent) {
	cfg := DefaultConfig()
	if mut != nil {
		mut(&cfg)
	}
	cfg.Enabled = true
	cfg.PHLTargetShyPts = 6
	cfg.EMALocationTFMinutes = 0
	cfg.Exec2mAfter30m = false
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = []Level{
		{Key: "L", Kind: KindKeyLevel, Price: 29386},
		{Key: "old-low:29335", Kind: KindOldExtreme, Role: OldLow, Price: 29335},
		{Key: "old-high:29431.75", Kind: KindOldExtreme, Role: OldHigh, Price: 29431.75},
	}
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 29300}
	for _, l := range []*TriggerLine{&h.FourH, &h.OneH} {
		if l.Dir != "" {
			l.Price = 29300 // far below the tape: the tick cannot move the line
			if l.Dir == SideShort {
				l.Price = 29600
			}
		}
	}
	e.State.HTF = h
	b := func(i int, o, hi, l, c float64) market.Kline {
		ot := auditMs(2026, 9, 15, 10, 0, 0) + int64(i)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: hi, Low: l, Close: c}
	}
	bars := []market.Kline{
		b(0, 29400, 29410, 29335, 29405),
		b(1, 29420, 29430, 29415, 29425),
		b(2, 29425, 29431.75, 29420, 29428),
		b(3, 29410, 29415, 29400, 29408),
		b(4, 29400, 29405, 29395, 29398),
		b(5, 29395, 29397, 29386, 29392),
	}
	now := bars[5].CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(now), High: 29300, Low: 29200, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e, e.Tick(bars, now)
}

// TestM462PHLCasesUnderDefault — D4.4-R11/R12/R13 + D5.1-R20(a) on the level
// PHL at 10:05 CT, DEFAULT config: case 1 (agree) and case 2 (1h silent —
// its last move older than the 4h's) still trade; case 3 is refused
// phl_htf_conflict; no 4h trigger is refused phl_htf_no_4h.
func TestM462PHLCasesUnderDefault(t *testing.T) {
	four := TriggerLine{Dir: SideLong, MovedAt: auditMs(2026, 9, 15, 5, 0, 0)}
	e, ins := m462PHL(HTF{FourH: four, OneH: TriggerLine{Dir: SideLong, MovedAt: auditMs(2026, 9, 15, 7, 0, 0)}})
	if m462Entries(ins, "PHL", SideLong) != 1 {
		t.Fatalf("case 1 (4h + 1h long): the long PHL must trade; intents %+v ledger %v", ins, e.State.Refusals)
	}
	e, ins = m462PHL(HTF{FourH: four, OneH: TriggerLine{Dir: SideShort, MovedAt: auditMs(2026, 9, 15, 3, 0, 0)}})
	if m462Entries(ins, "PHL", SideLong) != 1 {
		t.Fatalf("case 2 (1h short fired BEFORE the 4h = silent): follow the 4h, the long PHL trades; intents %+v ledger %v", ins, e.State.Refusals)
	}
	e, ins = m462PHL(HTF{FourH: four, OneH: TriggerLine{Dir: SideShort, MovedAt: auditMs(2026, 9, 15, 7, 0, 0)}})
	if m462Entries(ins, "PHL", SideLong) != 0 || e.State.Refusals["phl_htf_conflict"] == 0 {
		t.Fatalf("case 3 at 10:05 CT: refused phl_htf_conflict; intents %+v ledger %v", ins, e.State.Refusals)
	}
	e, ins = m462PHL(HTF{})
	if m462Entries(ins, "PHL", SideLong) != 0 || e.State.Refusals["phl_htf_no_4h"] == 0 {
		t.Fatalf("no 4h trigger: refused phl_htf_no_4h; intents %+v ledger %v", ins, e.State.Refusals)
	}
}

// TestM462RollingConflictNamed — the rolling PHL/PLH at 10:00 CT, DEFAULT
// config: a 4h/1h conflict stops the roll with a NAMED, counted refusal
// (rolling_htf_conflict) instead of the silent "no side" cancel; case 1
// still rolls.
func TestM462RollingConflictNamed(t *testing.T) {
	e, bars, now := m436At(10, 0)
	e.Cfg.HTFGateNewsOnly = false // D2-G7: the all-session value
	e.State.HTF = m462Conflict(SideLong, SideShort)
	if n := m436Armed(e.Tick(bars, now)); n != 0 {
		t.Fatalf("10:00 CT conflict: the rolling long must not arm; armed=%d", n)
	}
	if e.State.Refusals["rolling_htf_conflict"] != 1 {
		t.Fatalf("the rolling conflict sit-out must be named + counted once; ledger %v", e.State.Refusals)
	}
	e2, bars2, now2 := m436At(10, 0)
	e2.Cfg.HTFGateNewsOnly = false // D2-G7: the all-session value
	e2.State.HTF = m462Conflict(SideLong, SideLong)
	if n := m436Armed(e2.Tick(bars2, now2)); n != 1 {
		t.Fatalf("10:00 CT case 1 long: the rolling long must arm; armed=%d ledger %v", n, e2.State.Refusals)
	}
}

// TestM462BoxConflictNamed — box returns obey the same all-day gate:
// 13:00 CT conflict → box_htf_conflict; no 4h → box_htf_no_4h.
func TestM462BoxConflictNamed(t *testing.T) {
	fixture := func(h HTF) (*Evaluator, []market.Kline, int64) {
		cfg := DefaultConfig()
		cfg.HTFGateNewsOnly = false // D2-G7 oct round 2: these pins exercise the September all-session gate (knob false)
		cfg.Enabled = true
		cfg.Exec2mAfter30m = false
		cfg.KeyLevelTFMinutes = 1
		cfg.EMAPeriod34 = 0
		cfg.EMAPeriod9 = 0
		cfg.RoomMultiple = 0.05
		cfg.PHLRoomMultiple = cfg.RoomMultiple // review: the PHL/PLH room follows the fixture's room setting (the decoupled knob defaults to 2)
		t0 := time.Date(2026, time.September, 15, 13, 0, 0, 0, ctime()).UnixMilli()
		mk := func(i int, o, hi, l, c float64) market.Kline {
			return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: hi, Low: l, Close: c}
		}
		bars := []market.Kline{
			mk(0, 100, 101, 99, 100),
			mk(1, 100, 102, 99, 101),
			mk(2, 101, 103, 95, 96),
			mk(3, 96.5, 97, 96, 96.8),
			mk(4, 96.8, 97, 96.2, 96.8),
			mk(5, 96.8, 97, 95.5, 96),
			mk(6, 96.2, 97, 95.8, 96.8),
			mk(7, 96.5, 97.1, 95.9, 97),
		}
		e := New(cfg)
		now := bars[7].OpenTime + 59_999
		e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
		e.State.HTF = h
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
		return e, bars, now
	}
	h := m462Conflict(SideLong, SideShort)
	h.FourH.Price, h.OneH.Price = 93, 120
	e, bars, now := fixture(h)
	if n := m462Entries(e.Tick(bars, now), "BOX", SideLong); n != 0 {
		t.Fatalf("13:00 CT conflict: the box long must sit out; got %d", n)
	}
	if e.State.Refusals["box_htf_conflict"] == 0 {
		t.Fatalf("want box_htf_conflict counted; ledger %v", e.State.Refusals)
	}
	e, bars, now = fixture(HTF{})
	e.Tick(bars, now)
	if e.State.Refusals["box_htf_no_4h"] == 0 {
		t.Fatalf("want box_htf_no_4h counted; ledger %v", e.State.Refusals)
	}
	h1 := m462Conflict(SideLong, SideLong)
	h1.FourH.Price, h1.OneH.Price = 93, 93
	e, bars, now = fixture(h1)
	if n := m462Entries(e.Tick(bars, now), "BOX", SideLong); n != 1 {
		t.Fatalf("13:00 CT case 1 long: the box long must trade; got %d ledger %v", n, e.State.Refusals)
	}
}

// TestM462ReverseISBConflictNamed — the reverse ISB (the 705 shape: retagged
// PHL/PLH by CX-6) at 13:30 CT under the default: conflict → isbrev_htf_conflict.
func TestM462ReverseISBConflictNamed(t *testing.T) {
	e, bars, _ := isbReverseFixture(t)
	e.Cfg.HTFGateNewsOnly = false // D2-G7: the all-session value
	bars = m462ShiftTo(bars, 13, 0)
	now := bars[len(bars)-1].CloseTime
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	h := m462Conflict(SideLong, SideShort)
	h.FourH.Price, h.OneH.Price = 90, 130
	e.State.HTF = h
	ins := e.Tick(bars, now)
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup != "SWING4H" {
			t.Fatalf("13:00 CT conflict: no intraday entry may leave; intents %+v", ins)
		}
	}
	if e.State.Refusals["isbrev_htf_conflict"] == 0 && e.State.Refusals["isb_htf_conflict"] == 0 {
		t.Fatalf("the reverse/normal ISB conflict must be named; ledger %v", e.State.Refusals)
	}
}

// TestM462SharedFilterBackstop — the ONE shared check also runs as the last
// entry filter of Tick: any intraday entry intent that reaches it against the
// 4h/1h read (a path that forgot its own check, now or later) is dropped with
// "<setup>_htf_<kind>". SWING4H passes untouched (D5.2-R7: its direction is
// the 4h EMA34, not the 4h/1h triggers). Cancels pass.
func TestM462SharedFilterBackstop(t *testing.T) {
	h := m462Conflict(SideLong, SideShort)
	ins := []Intent{
		{Action: PlaceStopEntry, Setup: "PHL", Side: SideLong, Reason: "PHL/PLH: x"},
		{Action: PlaceStopLimitEntry, Setup: "ISB", Side: SideShort, Reason: "ISB: x"},
		{Action: PlaceStopEntry, Setup: "BOX", Side: SideLong, Reason: "box edge return"},
		{Action: PlaceStopEntry, Setup: "SWING4H", Side: SideShort, Reason: "swing reject touch"},
		{Action: CancelArm, ArmID: "isb-1"},
	}
	kept, refused := htfEntryFilter(ins, h)
	if len(kept) != 2 || kept[0].Setup != "SWING4H" || kept[1].Action != CancelArm {
		t.Fatalf("kept %+v, want only the SWING4H entry + the cancel", kept)
	}
	want := map[string]bool{"phl_htf_conflict": true, "isb_htf_conflict": true, "box_htf_conflict": true}
	if len(refused) != 3 {
		t.Fatalf("refused %v, want 3 named refusals", refused)
	}
	for _, r := range refused {
		if !want[r] {
			t.Fatalf("unexpected refusal name %q (want %v)", r, want)
		}
	}
	// case 2 short (4h short, 1h silent): a long is a side mismatch, a short passes.
	h2 := HTF{FourH: TriggerLine{Dir: SideShort, MovedAt: 10}}
	kept, refused = htfEntryFilter([]Intent{
		{Action: PlaceStopEntry, Setup: "PLH", Side: SideShort},
		{Action: PlaceStopEntry, Setup: "PHL", Side: SideLong},
	}, h2)
	if len(kept) != 1 || kept[0].Side != SideShort || len(refused) != 1 || refused[0] != "phl_htf_side_mismatch" {
		t.Fatalf("case 2 short: kept %+v refused %v", kept, refused)
	}
	// gate off (stored news-only, outside the windows): nothing is dropped.
	h.GateOff = true
	if kept, refused := htfEntryFilter(ins, h); len(kept) != len(ins) || len(refused) != 0 {
		t.Fatalf("GateOff: kept %d/%d refused %v", len(kept), len(ins), refused)
	}
}

// TestM462SwingExemptUnderDefault — SWING4H is NOT gated by the 4h/1h
// triggers under the new default (trade 715 stays the swing's own call).
func TestM462SwingExemptUnderDefault(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HTFGateNewsOnly = false // D2-G7 oct round 2: these pins exercise the September all-session gate (knob false)
	cfg.Enabled = true
	bars, now := cx1SwingTape(t)
	e := New(cfg)
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 20000}, OneH: TriggerLine{Dir: SideShort, Price: 19000}}
	seededWith(e, fullDepth, now)
	ints := e.Tick(bars, now)
	if entriesOf(ints, "SWING4H") != 1 {
		t.Fatalf("SWING4H must emit under the all-day default despite the conflict (D5.2-R7); intents %+v refusals %v", ints, e.State.Refusals)
	}
	if e.State.HTF.GateOff {
		t.Fatal("setup: the default gate must be active on this tick")
	}
}
