package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// OCT-EXACT build step D (CTO-decisions.md section D, owner order 2026-11-14
// "mentor mode = the October course exactly"). D1 and D3 live in the kernel;
// D2 (the leg-1 fixed partial) is trader-side (trader/oct_d_test.go).

// octDRevert applies the D reverts (D1 off, D3 off). Mechanics tests of the
// Sept behaviour build from preOctCfg, which carries it.
func octDRevert(c Config) Config {
	c.ISBJustBrokenLevel = false
	c.FTGLBodyEdgePartner = false
	return c
}

func TestOctD_Defaults(t *testing.T) {
	d := DefaultConfig()
	if !d.ISBJustBrokenLevel {
		t.Error("ISBJustBrokenLevel default = false, want true (D1 OctD3.3-R47 @38:10)")
	}
	if !d.FTGLBodyEdgePartner {
		t.Error("FTGLBodyEdgePartner default = false, want true (D3 OctD3.3-R54 @43:14)")
	}
	if !d.withBoxFlagsForTest().FTGLBodyEdgePartner {
		t.Error("the box cfg does not carry FTGLBodyEdgePartner")
	}
}

// withBoxFlagsForTest builds the box cfg the evaluator builds.
func (c Config) withBoxFlagsForTest() BoxCfg {
	e := &Evaluator{Cfg: c}
	return e.baseBoxCfg()
}

// D3, the video's own example: 13 Sep 2026 FTGL (Oct D3.3-R54 @43:14 is the
// mirror rule; the drawing is the D3.2 FTGL example). Bottom 1 wick 28,980.75
// (28,981 on the tape), the box drawn up to ~29,014 (frame f_000338 area
// 29,012-29,015, crosshair 29,014). The wick pairing took the ADJACENT 17:09 low
// (body edge 29,003.75) as the partner: top 29,003.75, 10.25 pts under the video.
// The body-edge pairing reads the second bottom whose lower body edge is level
// with bottom 1's own (29,015): the 18:51 low, body edge 29,015.00 (18:56:
// 29,014.75).
func TestOctD3_FTGLPartnerByBodyEdge13Sep(t *testing.T) {
	withFlatNear(t) // production levelness (flatNearScale 1); the package TestMain pins the legacy 0
	bars := loadFixture(t, "mnq_1m_2026-09-13_boxframe", "1m")
	trimmed := bars[3:] // the same frame TestBoxesGolden13SepFrame reads
	now := time.UnixMilli(trimmed[len(trimmed)-1].OpenTime + 60_000)
	find := func(cfg BoxCfg) *Box {
		for _, b := range BoxesBuild(trimmed, cfg, now) {
			if b.Kind == FTGL {
				bb := b
				return &bb
			}
		}
		return nil
	}

	on := DefaultBoxCfg()
	on.FTGLBodyEdgePartner = true
	b := find(on)
	if b == nil {
		t.Fatal("no FTGL box on the 13 Sep frame with the body-edge partner ON")
	}
	if math.Abs(b.Top-29014) > 1.0 || math.Abs(b.Bottom-28981) > 1.0 {
		t.Errorf("FTGL = [%.2f, %.2f], want ~[28981, 29014] (video: top 29,014, bottom 28,980.75; tolerance 1.0)", b.Bottom, b.Top)
	}

	off := DefaultBoxCfg()
	o := find(off)
	if o == nil || math.Abs(o.Top-29003.75) > 0.01 {
		t.Errorf("knob OFF: FTGL = %+v, want the pre-October top 29,003.75 (TestBoxesGolden13SepFrame)", o)
	}
}

// D3: the Config default reaches the evaluator's box cfg.
func TestOctD3_ConfigReachesBoxCfg(t *testing.T) {
	if !DefaultConfig().withBoxFlagsForTest().FTGLBodyEdgePartner {
		t.Error("default Config does not turn the body-edge partner on in the box cfg")
	}
	if octDRevert(DefaultConfig()).withBoxFlagsForTest().FTGLBodyEdgePartner {
		t.Error("the revert does not turn it off")
	}
}

func octDBar(o, h, l, c float64, minute int) market.Kline {
	base := int64(1793368800000) // arbitrary 1m grid
	t := base + int64(minute)*60_000
	return market.Kline{OpenTime: t, CloseTime: t + 59_999, Open: o, High: h, Low: l, Close: c, Final: true}
}

// D1, the 10-30 09:02/09:03 pair from Oct D3.3 @38:10 (R47): the box top/bottom
// 28,819.00 / 28,812.50, the mother candle 09:02 opened 28,826.75 above the box
// and closed 28,800.75 under it, the 09:03 inside bar H 28,807.50 never got back
// to the box (close 28,802.75 - the number the note records).
func TestOctD1_JustBrokenLevelE7(t *testing.T) {
	prev := octDBar(28826.75, 28828.25, 28795.75, 28800.75, 0)
	cur := octDBar(28801.00, 28807.50, 28786.00, 28802.75, 1)
	boxes := []Box{{Kind: FTGH, Top: 28819, Bottom: 28812.5, Key: "ftgh:28819.00:28812.50"}}

	if got := justBrokenLevel(SideShort, prev, cur, boxes); got == "" {
		t.Fatal("E7: the short ISB under the just-broken FTGH was not named")
	}
	// the retest lifts it: the inside bar's wick reaches the bottom edge
	retest := cur
	retest.High = 28812.5
	if got := justBrokenLevel(SideShort, prev, retest, boxes); got != "" {
		t.Errorf("retested box still refused: %q", got)
	}
	// the other direction is not refused (D3.3-R46: after a close above, the ISB LONG is the way in)
	if got := justBrokenLevel(SideLong, prev, cur, boxes); got != "" {
		t.Errorf("a long ISB after a close BELOW the box was refused: %q", got)
	}
	// a mother body that stays on one side of the box crosses nothing
	calm := octDBar(28790, 28800, 28785, 28795, 0)
	if got := justBrokenLevel(SideShort, calm, cur, boxes); got != "" {
		t.Errorf("a mother that crossed nothing was refused: %q", got)
	}
	// the FTGL mirror: a candle closes ABOVE the box top, the long ISB right there
	floor := []Box{{Kind: FTGL, Top: 28720, Bottom: 28710}}
	up := octDBar(28705, 28735, 28700, 28730, 0)
	insideBar := octDBar(28730, 28734, 28722, 28733, 1)
	if got := justBrokenLevel(SideLong, up, insideBar, floor); got == "" {
		t.Error("the long ISB over a just-broken FTGL was not named")
	}
}

// D1 SCOPE pin: the mentor's own D2.2 E9 trade is an ISB short right after a
// candle closed through an ascending trendline (10-30 08:33: mother 08:32 O 28,747.25
// C 28,714.75 through the 28,743.9 line; the order sold 28,709.5, marked). The D1
// box read (scope box, the revert) reads boxes only. ISB-M1 (scope all) reads the
// trendline too and still allows E9 because the inside bar is clear of the line
// (TestISBM1_E9AllowedClearOfTheLine).
func TestOctD1_OnlyBoxEdgesAreRead(t *testing.T) {
	mother := octDBar(28747.25, 28751, 28710.75, 28714.75, 0)
	isb := octDBar(28714.5, 28735.75, 28711, 28726, 1)
	if got := justBrokenLevel(SideShort, mother, isb, nil); got != "" {
		t.Errorf("with no box in play the E9 pair was refused: %q", got)
	}
}

// The evaluator hook obeys the knob. ISB-M1: the default scope (all) refuses the
// E7 pair as the box BREAK-AND-REVERSE (the 09:01 candle closed above the box, the
// 09:02 mother came back down through it) - the inside bar is clear of the edge, so
// the at-the-level read alone would not; scope box (the D1 build) refuses it as
// "mother crossed, no wick retest". Both keep E7 refused; knob off reverts both.
func TestOctD1_KnobOffRevert(t *testing.T) {
	above := octDBar(28849.00, 28857.00, 28825.25, 28827.00, -1) // 10-30 09:01, closed over the box
	below := octDBar(28772.50, 28780.00, 28771.25, 28775.00, -2) // price came from under the box
	prev := octDBar(28826.75, 28828.25, 28795.75, 28800.75, 0)
	cur := octDBar(28801.00, 28807.50, 28786.00, 28802.75, 1)
	jb := justBrokenCtx{hist: []market.Kline{below, above, prev, cur}, boxes: []Box{{Kind: FTGH, Top: 28819, Bottom: 28812.5}}}
	e := &Evaluator{Cfg: DefaultConfig()}
	if got := e.isbJustBrokenRefusal(SideShort, jb); got != "isb_just_broken_level" {
		t.Errorf("default (scope all): got %q, want isb_just_broken_level", got)
	}
	box := DefaultConfig()
	box.ISBJustBrokenScope = JustBrokenScopeBox
	e = &Evaluator{Cfg: box}
	if got := e.isbJustBrokenRefusal(SideShort, jb); got != "isb_just_broken_level" {
		t.Errorf("scope box: got %q, want isb_just_broken_level", got)
	}
	e = &Evaluator{Cfg: octDRevert(DefaultConfig())}
	if got := e.isbJustBrokenRefusal(SideShort, jb); got != "" {
		t.Errorf("revert: got %q, want none", got)
	}
}
