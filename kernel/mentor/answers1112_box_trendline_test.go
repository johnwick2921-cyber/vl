package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// answers-1112 Q12 (knob box_trendline_partial_not_room, default OFF since the
// 2026-11-12 review; ON is the owner's opt-in): for a
// BOX trade a trendline in the path is a place to sell part, not the level the
// room rule measures.
//
// The video [Sept D3.2 FTGH FTGL p2 @11:06-11:39, frame f_000338]: Sun 13 Sep
// 2026, MNQ 09-26 1m. A green FTGL box sits under a falling yellow trendline
// from the 18:30 top; the FTGH box is above. "Đứng dưới cái box nè, đụng cái
// box, cái target anh chị muốn đánh lên đâu, có phải là đánh lên trendline
// không. Mình sẽ có 1 cái biên bông khá là nhỏ … nếu mà thoát ra khỏi
// trendline thì biên bông nó sẽ bự hơn … mình canh mình đánh lên tới trên
// đây". With [Sept D3.3 p1 @05:15-07:46] "Về trendline / Anything / Những cái
// mà nó ngán đường anh chị … Nó là những target … Là những cái chỗ anh chị bán
// bớt" — the line in the way is where you sell part, the trade goes on.
//
// The tape is testdata/mnq_1m_2026-09-13_ftgl_trendline.json (data.db read
// only; its 17:00-19:00 rows equal mnq_1m_2026-09-13_boxframe.json, and the
// frame's header bar 19:48 O 29,035.25 H 29,042.50 L 29,033.75 C 29,035.00 is
// this tape's 19:48 row [A]). What the bot's own Tick draws on it by 20:24
// [A, read from Tick on this tape]: FTGL [28,981.00, 29,003.75], FTGH
// [29,096.75, 29,105.00], the falling trendline 18:30 29,102.00 → 19:55
// 29,047.25 (valid), worth 29,028.57 at 20:24. The 20:24 candle (O 29,001.00
// H 29,013.50 L 29,000.25 C 29,008.25) touches the FTGL and closes above it:
// the box long is a buy stop 29,013.50, stop 29,000.25 (13.25 pts). The line
// is 15.07 pts away = 1.14R; the FTGH bottom 29,096.75 = 6.30R. On the tape
// price went on to 29,078.00 by 21:01 [A].

const (
	q12Entry   = 29013.50
	q12Stop    = 29000.25
	q12BotLine = 29028.57 // the bot's line at 20:24: 29,102 + (29,047.25-29,102)·114/85
	q12FTGHBot = 29096.75
	// The mentor's own drawn line (frame f_000338: from the 18:30 top through
	// the 19:08 top 29,075.00) is worth 29,102 - 27·114/38 = 29,021.00 at 20:24
	// [B: anchors read from the frame against the tape] — 7.5 pts = 0.57R, inside
	// the 1:1 leg-1 exit.
	q12MentorLine = 29021.00
)

func q12Fixture(t *testing.T) (ref market.Kline, window []market.Kline, boxes []Box) {
	t.Helper()
	bars := loadFixture(t, "mnq_1m_2026-09-13_ftgl_trendline", "1m")
	ct, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	idx := -1
	for i, b := range bars {
		if time.UnixMilli(b.OpenTime).In(ct).Format("15:04") == "20:24" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("no 20:24 bar on the 13 Sep tape")
	}
	ref = bars[idx]
	if ref.High != q12Entry || ref.Low != q12Stop || ref.Close != 29008.25 {
		t.Fatalf("20:24 bar = %+v, want H 29,013.50 L 29,000.25 C 29,008.25 (the tape)", ref)
	}
	boxes = []Box{
		{Kind: FTGL, Top: 29003.75, Bottom: 28981.00, Key: "ftgl:29003.75:28981.00"},
		{Kind: FTGH, Top: 29105.00, Bottom: 29096.75, Key: "ftgh:29105.00:29096.75"},
	}
	return ref, bars[:idx+1], boxes
}

func q12Levels(line float64, extra ...Level) []Level {
	lv := []Level{
		{Key: "ftgl:29003.75:28981.00:top", Kind: KindFTGHEdge, Price: 29003.75},
		{Key: "ftgl:29003.75:28981.00:bottom", Kind: KindFTGLEdge, Price: 28981.00},
		{Key: "ftgh:29105.00:29096.75:top", Kind: KindFTGHEdge, Price: 29105.00},
		{Key: "ftgh:29105.00:29096.75:bottom", Kind: KindFTGLEdge, Price: q12FTGHBot},
		{Key: "ema9", Kind: KindEMA9, Price: 29006.72},
		{Key: "ema34", Kind: KindEMA34, Price: 29016.41},
	}
	if line != 0 {
		lv = append(lv, Level{Key: "target:trendline:short:1789342200000:1789347300000", Kind: KindTrendlineTarget, Price: line})
	}
	return append(lv, extra...)
}

func q12Cfg(on bool) Config {
	cfg := DefaultConfig()
	cfg.LocTriggerFilter = false // the 5m trigger is not under test here
	cfg.BoxTrendlinePartialNotRoom = on
	return cfg
}

// TestAnswers1112BoxTrendlineDefaultOff — review 2026-11-12 (P1): the knob
// ships OFF. The report lists the Q12 build under "Builds, all default OFF";
// the video makes the trendline the TARGET and aims past it only after the
// line breaks ("nếu mà thoát ra khỏi trendline thì biên bông nó sẽ bự hơn"
// [Sept D3.2 p2 @11:11-11:39]); and on the 90-day replay the returned box
// trades include lines 0.07R / 0.25R from entry. OFF = today's box_room.
// MUTANT: BoxTrendlinePartialNotRoom: true in DefaultConfig → RED.
func TestAnswers1112BoxTrendlineDefaultOff(t *testing.T) {
	if DefaultConfig().BoxTrendlinePartialNotRoom {
		t.Fatal("box_trendline_partial_not_room must default OFF (review P1: report \"Builds, all default OFF\"; the line is the target until it breaks [Sept D3.2 p2 @11:11-11:39])")
	}
	// The default refuses the video's 20:24 box long box_room, as today.
	ref, window, boxes := q12Fixture(t)
	cfg := DefaultConfig()
	cfg.LocTriggerFilter = false
	if out, why := boxEntryIntentWhy(ref, boxes[0], boxes, q12Levels(q12BotLine), TriggerLine{}, window, cfg); len(out) != 0 || why != "box_room" {
		t.Fatalf("default: got %d intents, %q — want box_room (today)", len(out), why)
	}
}

// The video's case: the line at 1.14R is the first partial, the FTGH is the
// target; OFF = today's box_room refusal exactly.
func TestAnswers1112BoxTrendlineFromTheVideo(t *testing.T) {
	ref, window, boxes := q12Fixture(t)
	levels := q12Levels(q12BotLine)

	out, why := boxEntryIntentWhy(ref, boxes[0], boxes, levels, TriggerLine{}, window, q12Cfg(true))
	if why != "" || len(out) != 1 {
		t.Fatalf("ON: box long refused %q (%d intents) — the trendline at 1.14R must not cap the room [Sept D3.3 p1 @05:15-07:46]", why, len(out))
	}
	in := out[0]
	if in.Side != SideLong || in.Price != q12Entry || in.Stop != q12Stop {
		t.Fatalf("ON: intent %s %.2f stop %.2f, want long 29,013.50 stop 29,000.25", in.Side, in.Price, in.Stop)
	}
	if in.Target != q12FTGHBot {
		t.Fatalf("ON: target %.2f, want the FTGH bottom 29,096.75 (\"mình canh mình đánh lên tới trên đây\")", in.Target)
	}
	if !strings.Contains(in.Reason, "trendline 29028.57 in the path is a partial") {
		t.Fatalf("ON: the intent must name the trendline it passed, reason = %q", in.Reason)
	}
	if refuse, r := roomRefusal(in.Price, in.Stop, in.Target, in.Confluence, targetRoomMultiple(levels, in.Target, q12Cfg(true).RoomMultiple)); refuse {
		t.Fatalf("ON: the evaluator's room check refuses the emitted target: %s", r)
	}
	// The trendline is the runner's FIRST partial line (Tick stamps it with
	// this function): it sits past the 1:1 leg-1 exit 29,026.75.
	if p := runnerPartialLine(levels, in.Price, in.Stop, in.Target, in.Side); p != q12BotLine {
		t.Fatalf("ON: first partial past 1:1 = %.2f, want the trendline 29,028.57 (\"những cái chỗ anh chị bán bớt\")", p)
	}

	// Revert: today exactly.
	off, offWhy := boxEntryIntentWhy(ref, boxes[0], boxes, levels, TriggerLine{}, window, q12Cfg(false))
	if len(off) != 0 || offWhy != "box_room" {
		t.Fatalf("OFF: got %d intents, %q — want box_room (the trendline at 1.14R < 2R room, today)", len(off), offWhy)
	}
}

// A real level still caps the room: the bot's 20:24 set also has the 20:10
// lower high 29,038.00 (old extreme, 1.85R). The knob only lifts the
// trendline's cap — the room is read to the old high and the trade is refused.
func TestAnswers1112BoxTrendlineRealLevelStillCaps(t *testing.T) {
	ref, window, boxes := q12Fixture(t)
	levels := q12Levels(q12BotLine, Level{Key: "old_extreme:29038.00", Kind: KindOldExtreme, Price: 29038.00, Role: "high"})
	for _, on := range []bool{true, false} {
		out, why := boxEntryIntentWhy(ref, boxes[0], boxes, levels, TriggerLine{}, window, q12Cfg(on))
		if len(out) != 0 || why != "box_room" {
			t.Fatalf("on=%v: %d intents, %q — want box_room (the old high 29,038 at 1.85R is a real level)", on, len(out), why)
		}
	}
}

// A trendline with no real level beyond stays the target (today), knob on or
// off: the knob never turns "no target" into a trade.
func TestAnswers1112BoxTrendlineAloneStaysTarget(t *testing.T) {
	ref, window, boxes := q12Fixture(t)
	var levels []Level
	for _, l := range q12Levels(q12BotLine) {
		if l.Price < 29090 { // drop the FTGH edges: nothing real beyond the line
			levels = append(levels, l)
		}
	}
	for _, on := range []bool{true, false} {
		out, why := boxEntryIntentWhy(ref, boxes[0], boxes, levels, TriggerLine{}, window, q12Cfg(on))
		if len(out) != 0 || why != "box_room" {
			t.Fatalf("on=%v: %d intents, %q — want box_room to the trendline (no level beyond it)", on, len(out), why)
		}
	}
}

// The mentor's own drawn line at 20:24 is 0.57R away — inside the 1:1 leg-1
// exit. The trade goes (the line is not the room cap) but the line is NOT a
// partial: he sells part only at >= 1:1 [Oct D1.2 @11:00-11:40 "Cái gain của
// anh chị cũng phải được at least là như vậy / Anh chị mới bắt đầu bán bớt"],
// so leg 1 at 29,026.75 is the first sale.
func TestAnswers1112BoxTrendlineInsideOneR(t *testing.T) {
	ref, window, boxes := q12Fixture(t)
	levels := q12Levels(q12MentorLine)
	out, why := boxEntryIntentWhy(ref, boxes[0], boxes, levels, TriggerLine{}, window, q12Cfg(true))
	if why != "" || len(out) != 1 || out[0].Target != q12FTGHBot {
		t.Fatalf("ON: %d intents, %q — want the box long targeting 29,096.75", len(out), why)
	}
	if p := runnerPartialLine(levels, out[0].Price, out[0].Stop, out[0].Target, out[0].Side); p == q12MentorLine {
		t.Fatalf("ON: a line inside 1R (%.2f) must not be the runner partial — leg 1 at 1:1 sells first [Oct D1.2 @11:00-11:40]", p)
	}
	off, offWhy := boxEntryIntentWhy(ref, boxes[0], boxes, levels, TriggerLine{}, window, q12Cfg(false))
	if len(off) != 0 || offWhy != "box_room" {
		t.Fatalf("OFF: %d intents, %q — want box_room (today)", len(off), offWhy)
	}
}

// Scope: only the BOX target reads the knob. The shared room helper the ISB,
// reverse ISB and PHL/PLH paths call still returns the trendline.
func TestAnswers1112BoxTrendlineScopeIsBoxOnly(t *testing.T) {
	levels := q12Levels(q12BotLine)
	if got := nextLevelBeyondRoom(levels, q12Entry, q12Stop, SideLong, DefaultConfig().RoomMultiple); got != q12BotLine {
		t.Fatalf("nextLevelBeyondRoom = %.2f, want the trendline 29,028.57 (ISB/PHL targets unchanged)", got)
	}
}
