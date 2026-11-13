package mentor

import (
	"strings"
	"testing"

	"vl/market"
)

// answers-1112 Q2 (REPORT-mentor-answers-1112.md, verdict confirmed, high):
// the PHL/PLH room to the next level is 1:1, not 2R.
//
//   - [Oct D2.3 @49:37-49:56] "khi em đặt stop-loss 40 điểm em cũng phải kiếm
//     cái target 40 điểm ... Risk reward 1-1 ... at least là cũng 35 điểm"
//     (a 40-pt stop needs a 40-pt target, RR 1:1).
//   - WE10 (Thu 10-29 ~19:53, PLH short, entry 28,980): frame s_04120 first
//     draws Target 29.00 / R:R 3.31, then he drags the target in to the old low;
//     frame s_04126 reads Stop 8.75 / Target 15.00 / R:R 1.71 / Closed PnL 15.
//   - WE7 (Fri 10-30 ~13:25, PLH short demo): entry 28,316.25, stop 28,332.00,
//     red target line 28,298.25 (old low 28,288.00) = 18 / 15.75 = 1.14R.
//
// Default phl_room_multiple = 1 takes both; the revert value 2 refuses both
// (today before this change); a target inside 1R is refused at either value
// (the D1.2 floor, "Đừng bao giờ nằm dưới 1-1", Sept D2.1 p1 @00:59-01:34).

// we10Touch is the WE10 PLH reference candle: entry ray = Low − 1 = 28,980,
// stop ray = High + 1 = 28,988.75 (risk 8.75, frame s_04126) [Oct D2.3 @46:20-47:20 rays].
func we10Touch() Touch {
	return Touch{
		LevelKey: "old-high", Outcome: TouchReject, ApproachedFrom: SideShort,
		RefBar: market.Kline{High: 28_987.75, Low: 28_981.00, Close: 28_982.00},
	}
}

// we7Touch is the WE7 PLH reference candle: entry 28,316.25, stop 28,332.00
// (risk 15.75; frames s_02716/s_02912).
func we7Touch() Touch {
	return Touch{
		LevelKey: "old-high", Outcome: TouchReject, ApproachedFrom: SideShort,
		RefBar: market.Kline{High: 28_331.00, Low: 28_317.25, Close: 28_318.00},
	}
}

func answersCfg() Config {
	c := DefaultConfig()
	c.Enabled = true
	return c
}

type phlRoomCase struct {
	name                string
	touch               Touch
	oldLow              float64 // the old extreme the ladder aims at (target = old + shy when nothing is in the way)
	obstacle            float64 // the level the mentor put the target on
	entry, stop, target float64
}

func answers1112Cases() []phlRoomCase {
	return []phlRoomCase{
		// WE10: the 3.31R target (28,951 = 28,946 + shy 5) is first drawn; the
		// old low at 28,965 stands in the way and becomes the target (1.71R).
		{"WE10 10-29 1.71R", we10Touch(), 28_946, 28_965, 28_980, 28_988.75, 28_965},
		// WE7: old low 28,288 (shy target 28,293); the red line 28,298.25 is in
		// the way and is the target (1.14R).
		{"WE7 10-30 1.14R", we7Touch(), 28_288, 28_298.25, 28_316.25, 28_332, 28_298.25},
	}
}

func phlRoomRun(c phlRoomCase, cfg Config, obstacle float64) (Intent, bool, string) {
	levels := []Level{{Key: "old-low-line", Kind: KindKeyLevel, Price: obstacle}}
	return PHLPLHR2Levels(c.touch, Level{Kind: KindOldExtreme, Role: OldLow, Price: c.oldLow}, 0, 3, 0, levels, cfg)
}

// TestAnswers1112PHLRoomDefaultIsOneToOne — the default is October's 1:1.
// MUTANT: PHLRoomMultiple: 2 in DefaultConfig → RED.
func TestAnswers1112PHLRoomDefaultIsOneToOne(t *testing.T) {
	if got := DefaultConfig().PHLRoomMultiple; got != 1 {
		t.Fatalf("PHLRoomMultiple default = %v, want 1 [Oct D2.3 @49:37-49:56 \"Risk reward 1-1\"; WE10 s_04126 R:R 1.71 taken]", got)
	}
}

// TestAnswers1112PHLRoomWorkedTradesAcceptedAtDefault — WE10 (1.71R) and WE7
// (1.14R) arm at the default and are refused "room" at the revert value 2.
func TestAnswers1112PHLRoomWorkedTradesAcceptedAtDefault(t *testing.T) {
	for _, c := range answers1112Cases() {
		t.Run(c.name, func(t *testing.T) {
			in, ok, why := phlRoomRun(c, answersCfg(), c.obstacle)
			if !ok {
				t.Fatalf("default config must take the mentor's trade; refused: %s", why)
			}
			if !m464Approx(in.Price, c.entry) || !m464Approx(in.Stop, c.stop) || !m464Approx(in.Target, c.target) {
				t.Fatalf("geometry = entry %.2f stop %.2f target %.2f, want %.2f / %.2f / %.2f", in.Price, in.Stop, in.Target, c.entry, c.stop, c.target)
			}

			rev := answersCfg()
			rev.PHLRoomMultiple = 2 // revert knob: phl_room_multiple=2 = today before answers-1112
			if _, ok2, why2 := phlRoomRun(c, rev, c.obstacle); ok2 || !strings.HasPrefix(why2, "room:") {
				t.Fatalf("phl_room_multiple=2 must restore the 2R room refusal; ok=%v why=%q", ok2, why2)
			}
		})
	}
}

// TestAnswers1112PHLRoomBelowOneRStillRefused — a target inside 1R is refused
// at the default (the D1.2 floor) and at the revert value.
func TestAnswers1112PHLRoomBelowOneRStillRefused(t *testing.T) {
	c := answers1112Cases()[0]
	// WE10 entry 28,980 / risk 8.75; an obstacle at 28,972 leaves 8 pts = 0.91R.
	for _, room := range []float64{1, 2} {
		cfg := answersCfg()
		cfg.PHLRoomMultiple = room
		if _, ok, why := phlRoomRun(c, cfg, 28_972); ok || why != targetCloserThanStopReason {
			t.Fatalf("room %g: a 0.91R target must be refused by the 1:1 floor; ok=%v why=%q", room, ok, why)
		}
	}
}

// ── near_box_room_phl_multiple: today's value, owner question ──────────────

// TestAnswers1112NearBoxPHLMultipleDefaultFollows — the default follows
// near_box_room_multiple (today exactly): the B11 fixture still counts near_box,
// and near_box_room_multiple=0 still turns the PHL leg off.
func TestAnswers1112NearBoxPHLMultipleDefaultFollows(t *testing.T) {
	if got := DefaultConfig().NearBoxRoomPHLMultiple; got != NearBoxRoomPHLMultipleFollow {
		t.Fatalf("NearBoxRoomPHLMultiple default = %v, want NearBoxRoomPHLMultipleFollow (today)", got)
	}
	if got := w11NearBoxPHLTick(t, func(*Config) {}); got != 1 {
		t.Fatalf("default: near_box = %d, want 1 (today)", got)
	}
	if got := w11NearBoxPHLTick(t, func(c *Config) { c.NearBoxRoomMultiple = 0 }); got != 0 {
		t.Fatalf("follow + near_box_room_multiple=0: near_box = %d, want 0 (today)", got)
	}
}

// TestAnswers1112NearBoxPHLMultipleKnob — an explicit PHL multiple governs the
// PHL/PLH leg only: 0 turns it off (the ISB skip is untouched — see
// TestW11B11ISBNearBoxIgnoresThePHLScope), 1 lets a 1.5R box edge through that
// the 2x refuses.
// MUTANT: phlNearBoxRefuses keeps reading NearBoxRoomMultiple → RED.
func TestAnswers1112NearBoxPHLMultipleKnob(t *testing.T) {
	if got := w11NearBoxPHLTick(t, func(c *Config) { c.NearBoxRoomPHLMultiple = 0 }); got != 0 {
		t.Fatalf("near_box_room_phl_multiple=0: near_box = %d, want 0", got)
	}
	// Long PHL at 100, risk 10, FTGH box bottom at 115 → 1.5 × risk of room.
	boxes := []Box{{Kind: FTGH, Top: 120, Bottom: 115, Key: "ftgh:120:115"}}
	cfg := answersCfg()
	if name := phlNearBoxRefuses(cfg, boxes, 100, SideLong, 10); name != "near_box" {
		t.Fatalf("default (follow 2x): a 1.5R box edge must refuse near_box, got %q", name)
	}
	cfg.NearBoxRoomPHLMultiple = 1
	if name := phlNearBoxRefuses(cfg, boxes, 100, SideLong, 10); name != "" {
		t.Fatalf("near_box_room_phl_multiple=1: a 1.5R box edge must pass, got %q", name)
	}
	cfg.NearBoxRoomPHLMultiple = 2
	cfg.NearBoxRoomMultiple = 0 // the ISB multiple off must not switch an explicit PHL multiple off
	if name := phlNearBoxRefuses(cfg, boxes, 100, SideLong, 10); name != "near_box" {
		t.Fatalf("explicit near_box_room_phl_multiple=2: must refuse independently of near_box_room_multiple, got %q", name)
	}
}
