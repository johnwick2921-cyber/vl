package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// m436 — release #53. The rolling PHL/PLH (default ON since c52 W6) used to skip
// every PHL/PLH LOCATION ban the level touch loop applies, so it armed inside a
// standing 5m ISB box, between boxes, next to a box and mid-range — exactly the
// zones the course bans for every PHL/PLH ("No PHL, no PLH, no opposite ISB"
// [D3.4 p2 @07:27–10:35; D3.2 p2 @08:51]; between two boxes [D3.2 p2
// @08:34–08:55]; near a box [D3.2 p1 @21:53–23:08]; mid-range [D2.2 p3
// @14:03–16:03]; X4 key-level rank [@06:31]). These pins drive the REAL
// Evaluator.Tick on rollingFixture() (the production call site of
// rollingPHLTick).

func m436Armed(ins []Intent) int { return len(rollingPlaceEntries(ins)) }

func m436Count(e *Evaluator, name string) int { return e.State.Refusals[name] }

// m436LatchBoxes seeds the standing FTGL / FTGH boxes the way Tick's latch
// reads them (same trading day, no formation anchor in the tape → the refresh
// keeps the values).
func m436LatchBoxes(e *Evaluator, now int64, boxes ...Box) {
	e.State.LatchedBoxesDay = boxLatchDayKey(now, e.Cfg)
	e.State.LatchedBoxes = map[string]Box{}
	for _, b := range boxes {
		e.State.LatchedBoxes[string(b.Kind)] = b
	}
}

// TestM436RollingRefusedInsideStandingISBBox — the P1: a rolling long inside a
// standing 5m / 15m / 30m ISB box is refused with the box ban's name (prefixed
// rolling_) and counted once; the control without the box arms. A resting roll
// is cancelled when the box appears.
// Mutant: delete the phlLocationBan call in rollingPHLTick → armed=1 → RED.
func TestM436RollingRefusedInsideStandingISBBox(t *testing.T) {
	cases := []struct {
		name  string
		set   func(e *Evaluator)
		count string
	}{
		{"5m", func(e *Evaluator) { e.State.ISBBox = &ISBBox{High: 125, Low: 105} }, "rolling_phl_5m_box_ban"},
		{"15m", func(e *Evaluator) { e.State.ISBBox15m = &ISBBox{High: 125, Low: 105} }, "rolling_phl_15m_box_ban"},
		{"30m", func(e *Evaluator) { e.State.ISBBox30m = &ISBBox{High: 125, Low: 105} }, "rolling_phl_30m_box_ban"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, bars, now := rollingFixture()
			tc.set(e)
			if n := m436Armed(e.Tick(bars, now)); n != 0 {
				t.Fatalf("a rolling PHL armed inside a standing %s ISB box (newest close 113 in 105–125): armed=%d refusals=%v", tc.name, n, e.State.Refusals)
			}
			if got := m436Count(e, tc.count); got != 1 {
				t.Fatalf("want %s counted once, got %d; ledger=%v", tc.count, got, e.State.Refusals)
			}
			// the same candle re-read (a 1m tick on the same closed exec candle) is not recounted
			_ = e.Tick(bars, now)
			if got := m436Count(e, tc.count); got != 1 {
				t.Fatalf("%s recounted on the same candle: %d", tc.count, got)
			}
			// control: no box → arms
			e2, bars2, now2 := rollingFixture()
			if n := m436Armed(e2.Tick(bars2, now2)); n != 1 {
				t.Fatalf("control: the clean pullback must arm, got %d", n)
			}
		})
	}
}

// TestM436RollingBoxBanCancelsRestingRoll — a roll rests, then a standing ISB
// box forms around price: the next candle's roll is refused AND the resting
// stop is cancelled (the leg's location is dead).
// Mutant: return nil instead of rollingCancel in the ban block → the stale buy
// stop survives → RED.
func TestM436RollingBoxBanCancelsRestingRoll(t *testing.T) {
	e, bars, now := rollingFixture()
	if m436Armed(e.Tick(bars, now)) != 1 {
		t.Fatal("setup: the roll must arm")
	}
	first := e.State.RollingPHL.ArmID
	e.State.ISBBox = &ISBBox{High: 125, Low: 105}
	next := append(append([]market.Kline{}, bars...), rollingKline(bars[0].OpenTime, 6, 113, 114, 108, 109))
	ins := e.Tick(next, next[6].OpenTime+59_999)
	var cancelled bool
	for _, in := range ins {
		if in.Action == CancelArm && in.ArmID == first {
			cancelled = true
		}
		if in.Action == PlaceStopEntry && in.Setup == "PHL" {
			t.Fatalf("a roll armed inside the standing box: %+v", in)
		}
	}
	if !cancelled || e.State.RollingPHL.ArmID != "" {
		t.Fatalf("the resting roll must be cancelled when the box bans its location: cancelled=%v state=%+v", cancelled, e.State.RollingPHL)
	}
}

// TestM436RollingRefusedMidRange — price sits between a key level above and one
// below within RangeGapPts: no PHL/PLH, only the inside bar [D2.2 p3
// @14:03–16:03]. The level path names it phl_mid_range.
// Mutant: delete the phlLocationBan call → armed=1 → RED.
func TestM436RollingRefusedMidRange(t *testing.T) {
	e, bars, now := rollingFixture()
	e.Cfg.RangeGapPts = 20
	e.State.SeedLevels = append(e.State.SeedLevels, Level{Key: "kl-100", Kind: KindKeyLevel, Price: 100})
	if n := m436Armed(e.Tick(bars, now)); n != 0 {
		t.Fatalf("a rolling PHL armed mid-range: armed=%d refusals=%v", n, e.State.Refusals)
	}
	if got := m436Count(e, "rolling_phl_mid_range"); got != 1 {
		t.Fatalf("want rolling_phl_mid_range counted once, ledger=%v", e.State.Refusals)
	}
	// control: the lower level further than the gap → not mid-range → arms
	e2, bars2, now2 := rollingFixture()
	e2.Cfg.RangeGapPts = 5
	e2.State.SeedLevels = append(e2.State.SeedLevels, Level{Key: "kl-100", Kind: KindKeyLevel, Price: 100})
	if n := m436Armed(e2.Tick(bars2, now2)); n != 1 {
		t.Fatalf("control: not mid-range, must arm, got %d (refusals=%v)", n, e2.State.Refusals)
	}
}

// TestM436RollingRefusedBetweenBoxes — an FTGL below and an FTGH above the
// entry: no PHL/PLH regardless of width [D3.2 p2 @08:34–08:55].
// Mutant: delete the midRangeBoxedRefuses clause in phlLocationBan → armed=1 → RED.
func TestM436RollingRefusedBetweenBoxes(t *testing.T) {
	e, bars, now := rollingFixture()
	m436LatchBoxes(e, now,
		Box{Kind: FTGL, Bottom: 90, Top: 95, Key: "ftgl:95:90"},
		Box{Kind: FTGH, Bottom: 140, Top: 145, Key: "ftgh:145:140"},
	)
	if n := m436Armed(e.Tick(bars, now)); n != 0 {
		t.Fatalf("a rolling PHL armed between an FTGL and an FTGH: armed=%d refusals=%v", n, e.State.Refusals)
	}
	if got := m436Count(e, "rolling_phl_mid_range_boxed"); got != 1 {
		t.Fatalf("want rolling_phl_mid_range_boxed counted once, ledger=%v", e.State.Refusals)
	}
}

// TestM436RollingRefusedNearBox — the nearest box edge in the trade direction is
// closer than NearBoxRoomMultiple x the reference candle's range: sát box, skip
// [D3.2 p1 @21:53–23:08]. Reference range 6 (118-112), x2 = 12; the FTGH edge is
// 6 above the 118 high. A far edge (50 pts) passes.
// Mutant: delete the phlNearBoxRefuses block in rollingPHLTick → armed=1 → RED.
func TestM436RollingRefusedNearBox(t *testing.T) {
	e, bars, now := rollingFixture()
	m436LatchBoxes(e, now, Box{Kind: FTGH, Bottom: 124, Top: 126, Key: "ftgh:126:124"})
	if n := m436Armed(e.Tick(bars, now)); n != 0 {
		t.Fatalf("a rolling PHL armed 6 pts under an FTGH with a 6-pt risk: armed=%d refusals=%v", n, e.State.Refusals)
	}
	if got := m436Count(e, "rolling_near_box"); got != 1 {
		t.Fatalf("want rolling_near_box counted once, ledger=%v", e.State.Refusals)
	}
	// control: the box far away (edge 60 pts above) → arms.
	e2, bars2, now2 := rollingFixture()
	m436LatchBoxes(e2, now2, Box{Kind: FTGH, Bottom: 180, Top: 185, Key: "ftgh:185:180"})
	if n := m436Armed(e2.Tick(bars2, now2)); n != 1 {
		t.Fatalf("control: a far box must not block the roll, got %d (refusals=%v)", n, e2.State.Refusals)
	}
}

// TestM436RollingRefusedKeypairCandleTooBig — X4: the candle is as big as the
// rank between the leg's anchor (the old high 130) and the next KEY level (135):
// sit out ("một cái nến nó bằng cái range của 2 cái level rồi thì ngồi im" [X4
// @06:31]). Candle range 6 >= 5.
// Mutant: delete the phlLocationBan call → armed=1 → RED.
func TestM436RollingRefusedKeypairCandleTooBig(t *testing.T) {
	e, bars, now := rollingFixture()
	e.State.SeedLevels = append(e.State.SeedLevels, Level{Key: "kl-135", Kind: KindKeyLevel, Price: 135})
	if n := m436Armed(e.Tick(bars, now)); n != 0 {
		t.Fatalf("a rolling PHL armed although the candle spans the key-level rank: armed=%d refusals=%v", n, e.State.Refusals)
	}
	if got := m436Count(e, "rolling_keypair_candle_too_big"); got != 1 {
		t.Fatalf("want rolling_keypair_candle_too_big counted once, ledger=%v", e.State.Refusals)
	}
	// control: the next key level 40 pts beyond → the rank dwarfs the candle → arms.
	e2, bars2, now2 := rollingFixture()
	e2.State.SeedLevels = append(e2.State.SeedLevels, Level{Key: "kl-170", Kind: KindKeyLevel, Price: 170})
	if n := m436Armed(e2.Tick(bars2, now2)); n != 1 {
		t.Fatalf("control: a wide key-level rank must not block, got %d (refusals=%v)", n, e2.State.Refusals)
	}
}

// TestM436LevelPathBansUnchanged — the extraction must not move the level path:
// the same standing 5m box still refuses a LEVEL PHL with phl_5m_box_ban (the
// existing r22 replay pins cover the full tape; this is the direct tick pin).
func TestM436LevelPathStillCountsBoxBan(t *testing.T) {
	e, bars, now := rollingFixture()
	e.Cfg.RollingPHL = false
	e.State.SeedLevels = append(e.State.SeedLevels, Level{Key: "support", Kind: KindKeyLevel, Price: 112})
	e.State.ISBBox = &ISBBox{High: 125, Low: 105}
	_ = e.Tick(bars, now)
	if got := m436Count(e, "phl_5m_box_ban"); got < 1 {
		t.Fatalf("the level path must still count phl_5m_box_ban (touch of 112 inside the 5m box); ledger=%v", e.State.Refusals)
	}
	if got := m436Count(e, "rolling_phl_5m_box_ban"); got != 0 {
		t.Fatalf("the level path must keep the unprefixed name, ledger=%v", e.State.Refusals)
	}
}

// TestM436OrphanTouchDoesNotBlockRolling — the P2: a TouchReject entry whose
// level LEFT the level set (swept old extreme, replaced S/R, deleted key level)
// owns nothing. Mutant: drop the live-set filter in levelTouchRejectActive →
// the orphan blocks the long roll forever → RED. Control: the same touch on a
// level that IS in the set still owns the entry (the m216 guard).
func TestM436OrphanTouchDoesNotBlockRolling(t *testing.T) {
	e, bars, now := rollingFixture()
	e.State.Touches["old_extreme:112.00"] = Touch{
		LevelKey: "old_extreme:112.00", Outcome: TouchReject, ApproachedFrom: SideLong,
		RefBar: bars[4], PriceAtTouch: 112,
	}
	if n := m436Armed(e.Tick(bars, now)); n != 1 {
		t.Fatalf("an orphan touch (level no longer in the set) blocked the roll: armed=%d state=%+v", n, e.State.RollingPHL)
	}
	// the helper itself: live level owns, orphan does not.
	st := &State{Touches: map[string]Touch{"k": {Outcome: TouchReject, ApproachedFrom: SideLong}}}
	if levelTouchRejectActive(st, SideLong, nil) {
		t.Fatal("an orphan touch owns the entry")
	}
	if !levelTouchRejectActive(st, SideLong, []Level{{Key: "k"}}) {
		t.Fatal("a live level's reject must still own the entry (m216)")
	}
}

// m436At shifts the whole rolling fixture to a wall-clock CT time (the tape,
// the day latch and the ORB all follow).
func m436At(hour, min int) (*Evaluator, []market.Kline, int64) {
	e, bars, _ := rollingFixture()
	t0 := time.Date(2026, time.September, 15, hour, min, 0, 0, ctime()).UnixMilli()
	shift := t0 - bars[0].OpenTime
	out := make([]market.Kline, len(bars))
	for i, b := range bars {
		b.OpenTime += shift
		b.CloseTime += shift
		out[i] = b
	}
	now := out[len(out)-1].OpenTime + 59_999
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e, out, now
}

// TestM436RollingTradesOutsideTheHTFGateWindow — the P2 (lens l6): with
// HTFGateNewsOnly ON (the default) the 4h/1h gate is OFF outside 08:30–09:30 CT
// (and the print window) and HTFVerdict returns an EMPTY side, which cancelled
// every roll at 09:30 and started none after. The rolling leg follows the 4h
// direction alone when the gate is off. At 10:00 CT a 4h-long pullback arms; a
// resting roll survives the 09:30 window close; with NO 4h trigger there is
// still no direction (no roll); a 4h-short tape does not arm the long.
// Mutant: restore `_, side, _ := HTFVerdict(e.State.HTF)` in rollingPHLTick →
// 10:00 arms 0 → RED.
func TestM436RollingTradesOutsideTheHTFGateWindow(t *testing.T) {
	// inside the window (control, = the existing fixture)
	e0, bars0, now0 := m436At(9, 0)
	if m436Armed(e0.Tick(bars0, now0)) != 1 {
		t.Fatal("control: 09:00 CT (gate window) must arm")
	}
	// outside the window
	e, bars, now := m436At(10, 0)
	if m436Armed(e.Tick(bars, now)) != 1 {
		t.Fatalf("10:00 CT (gate off) with a 4h-long trend must arm; gateOff=%v refusals=%v", e.State.HTF.GateOff, e.State.Refusals)
	}
	if !e.State.HTF.GateOff {
		t.Fatal("setup: the HTF gate must be OFF at 10:00 CT")
	}
	// no 4h trigger → no direction → no roll
	e2, bars2, now2 := m436At(10, 0)
	e2.State.HTF = HTF{}
	if m436Armed(e2.Tick(bars2, now2)) != 0 {
		t.Fatal("no 4h trigger at all: there is no trend to continue")
	}
	// a resting roll survives the window close (09:29 → 09:31)
	e3, bars3, now3 := m436At(9, 24) // newest bar 09:29
	if m436Armed(e3.Tick(bars3, now3)) != 1 {
		t.Fatal("setup: 09:29 roll must arm")
	}
	first := e3.State.RollingPHL.ArmID
	next := append(append([]market.Kline{}, bars3...), rollingKline(bars3[0].OpenTime, 6, 113, 114, 108, 109)) // 09:30 bar
	next = append(next, rollingKline(bars3[0].OpenTime, 7, 109, 110, 104, 105))                                // 09:31 bar
	ins := e3.Tick(next, next[7].OpenTime+59_999)
	for _, in := range ins {
		if in.Action == CancelArm && in.ArmID == first && strings.Contains(in.Reason, "no HTF trend direction") {
			t.Fatalf("the window close cancelled the resting roll: %+v", in)
		}
	}
}
