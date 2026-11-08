package mentor

import (
	"compress/gzip"
	"io"
	"os"
	"strings"
	"testing"

	"vl/market"
)

// Release #61 m466 P4 — SWING4H fidelity (G63 D5.2-R26, G66 D5.2-R25), pinned
// RED-first from the course's own trades.
//
// The bars are driven through stepSwingBar with the level already INVALID
// (FirstTouch.Through latched by an earlier 5m close beyond the line), at the
// course's exact prices, so the numbers in the assertions are the numbers on
// the mentor's screen.

// m61InvalidLevel returns a swing state whose 4h level is INVALID (a 5m close
// went through it earlier in this 4h candle), the window opened from approach.
func m61InvalidLevel(t *testing.T, line float64, approach Side, ref market.Kline) *SwingState {
	t.Helper()
	return &SwingState{
		Line:        line,
		BucketStart: fourHBucketStart(ref.OpenTime, nil),
		FirstTouch:  &swingTouch{RefBar: ref, Approach: approach, Through: true, Touches: 1},
	}
}

// TestM61SwingISBWithoutCloseBack — G63 D5.2-R26 [p3 @04:49–05:09, @07:42–08:36].
// Example G / E8 (Fri 11 Sep, 4h line 29,405.25, support → long): the 5m dips
// under the line 11:15–11:50; the student entered at the inside bar BEFORE any
// close back — "Chị vô đúng mà… thế là đúng… còn nếu chị đợi đóng ngược lại
// cũng được, không thành vấn đề, nhưng chị đợi hơi lâu"; entry 29,424.25, stop
// AT the line 29,405.25 (R29), +39.5 / +42.25 to illustrate the waiting cost.
// Candle 1 closes 29,393.75 and the inside bar 29,397.25 — both still BELOW the
// line. The inside-bar high (the buy stop) 29,424.25 sits ABOVE the line, on
// the trade side.
//
// not_required (default): a LONG buy stop 29,424.25, stop 29,405.25, the
// inside-bar order (ISB marker) — and ISBFired latches as before.
// required (pre-#61): no entry; the bar is a reject shape at an invalid level
// → named swing_level_invalid.
// Mutant: keep the closedBack(prev) && closedBack(b) clause under
// not_required → no entry → RED.
func TestM61SwingISBWithoutCloseBack(t *testing.T) {
	const line = 29405.25
	bars := []market.Kline{
		mk5m(t, 11, 11, 20, 29430, 29440, 29380, 29393.75),    // candle 1: closes BELOW the line
		mk5m(t, 11, 11, 25, 29395, 29424.25, 29388, 29397.25), // inside bar (body in candle 1's range), still below
	}
	now := bars[1].OpenTime + 5*60_000

	cfg := DefaultSwingCfg()
	if cfg.ISBCloseBackRequired {
		t.Fatal("the default must be not_required (D5.2-R26)")
	}
	s := m61InvalidLevel(t, line, SideLong, bars[0])
	es := m463Entries(s.stepSwingBar(bars, 1, line, cfg, now, swingBarLive))
	if len(es) != 1 {
		t.Fatalf("not_required: the inside bar alone is the setup — want ONE long entry, got %+v (refused %+v)", es, s.refused)
	}
	in := es[0]
	if in.Side != SideLong || in.Price != 29424.25 || in.Stop != line {
		t.Fatalf("entry = %+v, want LONG buy stop 29,424.25 with the stop AT the line 29,405.25 (R29)", in)
	}
	if s.Pending == nil || !s.Pending.ISB || s.Pending.Entry != 29424.25 {
		t.Fatalf("the resting order must be the inside-bar order (ISB marker): %+v", s.Pending)
	}
	if !s.FirstTouch.ISBFired || !s.FirstTouch.Through {
		t.Fatalf("ISBFired latches, Through stays: %+v", s.FirstTouch)
	}

	// required: the pre-#61 rule — no entry, named swing_level_invalid.
	req := DefaultSwingCfg()
	req.ISBCloseBackRequired = true
	s2 := m61InvalidLevel(t, line, SideLong, bars[0])
	if es := m463Entries(s2.stepSwingBar(bars, 1, line, req, now, swingBarLive)); len(es) != 0 {
		t.Fatalf("required: neither candle closed back — no entry, got %+v", es)
	}
	if n := m463Refused(s2, SwingRefusedLevelInvalid); n != 1 {
		t.Fatalf("required: the bar is NAMED %s once, got %d (%+v)", SwingRefusedLevelInvalid, n, s2.refused)
	}
}

// TestM61SwingISBEntryMustBeOnTheTradeSide — G63 guard: "cái điểm entry ở
// inside bar, basically nó vẫn nằm trên line" [D5.2 p3 @07:42–08:36]. An inside
// bar wholly below a support line has its buy stop BELOW the line, where a stop
// AT the line cannot sit (R29). Refused, NAMED swing_isb_entry_beyond_line —
// never the hard-invariant alarm — and the level is NOT spent: a later inside
// bar whose high clears the line may still enter (R25, no time limit).
func TestM61SwingISBEntryMustBeOnTheTradeSide(t *testing.T) {
	const line = 29405.25
	bars := []market.Kline{
		mk5m(t, 11, 11, 20, 29400, 29402, 29370, 29380), // candle 1, wholly below the line
		mk5m(t, 11, 11, 25, 29382, 29398, 29375, 29390), // inside bar, high 29,398 < the line
	}
	now := bars[1].OpenTime + 5*60_000
	cfg := DefaultSwingCfg()
	before := SwingInvalidStops()
	s := m61InvalidLevel(t, line, SideLong, bars[0])
	if es := m463Entries(s.stepSwingBar(bars, 1, line, cfg, now, swingBarLive)); len(es) != 0 {
		t.Fatalf("a buy stop under the support line cannot enter, got %+v", es)
	}
	if n := m463Refused(s, SwingRefusedISBEntryBeyondLine); n != 1 {
		t.Fatalf("the inside bar must be NAMED %s once, got %d (%+v)", SwingRefusedISBEntryBeyondLine, n, s.refused)
	}
	if SwingInvalidStops() != before {
		t.Fatal("a geometric non-setup must not trip the hard-invariant alarm (swing_invalid_stop)")
	}
	if s.FirstTouch.ISBFired || s.Pending != nil {
		t.Fatalf("the level is not spent by a non-entry: %+v pending %+v", s.FirstTouch, s.Pending)
	}
}

// TestM61SwingISBSpentUnchanged — G63: swing_isb_spent is unchanged under
// not_required. Once the level's inside-bar order FILLED and that leg ended, a
// later inside bar in the same 4h candle is refused swing_isb_spent [D5.2-R19/R23 p1 @13:23–13:40, p3 @19:15–21:56].
func TestM61SwingISBSpentUnchanged(t *testing.T) {
	const line = 29405.25
	bars := []market.Kline{
		mk5m(t, 11, 11, 40, 29430, 29440, 29380, 29393.75),
		mk5m(t, 11, 11, 45, 29395, 29424.25, 29388, 29397.25),
	}
	s := m61InvalidLevel(t, line, SideLong, bars[0])
	s.FirstTouch.ISBFired = true // the level's inside-bar order filled; the leg ended (Pos nil)
	if es := m463Entries(s.stepSwingBar(bars, 1, line, DefaultSwingCfg(), bars[1].OpenTime+5*60_000, swingBarLive)); len(es) != 0 {
		t.Fatalf("the level gave its trade, got %+v", es)
	}
	if n := m463Refused(s, SwingRefusedISBSpent); n != 1 {
		t.Fatalf("NAMED %s once, got %d (%+v)", SwingRefusedISBSpent, n, s.refused)
	}
}

// TestM61SwingISBOrderSurvivesCloseThrough — G66 D5.2-R25 [p2 @10:23–10:51]:
// Example F / E7 (Tue 11 Aug, level 29,553.50, long): "Dù nó có xuống 1 cây
// nữa, nó trồi được 5 cây nữa. Để cái cây inside bar này cho em." A pending
// INSIDE-BAR long (buy stop 29,577.75, stop AT the line 29,553.50) is NOT
// cancelled by a later 5m close at 29,540 under the line; it keeps resting until
// it fills or the 4h candle carrying the line closes (R27 — the order's expiry).
// A pending REJECT order still dies on the same close (R22). mentor_swing_isb_keep
// OFF restores the pre-#61 cancel for the inside-bar order too.
// Mutant: drop the ISB exemption from the stepSwingBar cancel → a CancelArm → RED.
func TestM61SwingISBOrderSurvivesCloseThrough(t *testing.T) {
	const line = 29553.50
	bars := []market.Kline{
		mk5m(t, 11, 10, 25, 29560, 29580, 29550, 29570),
		mk5m(t, 11, 10, 30, 29565, 29570, 29535, 29540), // no fill (high < 29,577.75); closes THROUGH the line
		mk5m(t, 11, 10, 35, 29540, 29579, 29538, 29575), // trades through 29,577.75 → fills
	}
	ref := bars[0]
	isbOrder := func(expiry int64) *swingPosition {
		return &swingPosition{Side: SideLong, Entry: 29577.75, Stop: line, Target: 29613.5, Risk: 24.25,
			ArmID: "swing-isb", ExpiryMs: expiry, ISB: true}
	}
	expiry := fourHBucketStart(ref.OpenTime, nil) + 4*3600_000 - 1
	cancels := func(out []Intent) int {
		n := 0
		for _, in := range out {
			if in.Action == CancelArm {
				n++
			}
		}
		return n
	}

	// keep ON (default): the inside-bar order rests, then fills.
	cfg := DefaultSwingCfg()
	s := m61InvalidLevel(t, line, SideLong, ref)
	s.FirstTouch.ISBFired = true
	s.Pending = isbOrder(expiry)
	if n := cancels(s.stepSwingBar(bars, 1, line, cfg, bars[1].OpenTime+5*60_000, swingBarLive)); n != 0 {
		t.Fatalf("keep ON: a later close through the line must NOT cancel the inside-bar order, got %d CancelArm", n)
	}
	if s.Pending == nil || s.Pending.ArmID != "swing-isb" {
		t.Fatalf("keep ON: the inside-bar order still rests, got %+v", s.Pending)
	}
	_ = s.stepSwingBar(bars, 2, line, cfg, bars[2].OpenTime+5*60_000, swingBarLive)
	if s.Pos == nil || s.Pos.ArmID != "swing-isb" || s.Pending != nil {
		t.Fatalf("the kept order fills on the bar that trades through 29,577.75: pos %+v pending %+v", s.Pos, s.Pending)
	}

	// R27: the 4h candle carrying the line closes → the kept order is over.
	s3 := m61InvalidLevel(t, line, SideLong, ref)
	s3.FirstTouch.ISBFired = true
	s3.Pending = isbOrder(expiry)
	_ = s3.stepSwingBar(bars, 1, line, cfg, bars[1].OpenTime+5*60_000, swingBarLive)
	next := mk5m(t, 11, 13, 0, 29540, 29545, 29530, 29535) // the NEXT 4h candle's first bar
	s3.stepSwingBar([]market.Kline{bars[1], next}, 1, line, cfg, next.OpenTime+5*60_000, swingBarLive)
	if s3.Pending != nil {
		t.Fatalf("R27: the inside-bar order expires with the 4h candle that carried the line: %+v", s3.Pending)
	}

	// A pending REJECT order still dies on the close through (R22).
	s2 := &SwingState{Line: line, BucketStart: fourHBucketStart(ref.OpenTime, nil),
		FirstTouch: &swingTouch{RefBar: ref, Approach: SideLong, Touches: 1}}
	s2.Pending = &swingPosition{Side: SideLong, Entry: 29580, Stop: line - 30, ArmID: "swing-reject", ExpiryMs: expiry}
	if n := cancels(s2.stepSwingBar(bars, 1, line, cfg, bars[1].OpenTime+5*60_000, swingBarLive)); n != 1 {
		t.Fatalf("a pending REJECT order is still cancelled by the close through (R22), got %d CancelArm", n)
	}
	if s2.Pending != nil {
		t.Fatalf("the reject order is gone: %+v", s2.Pending)
	}

	// keep OFF: the pre-#61 cancel for the inside-bar order too.
	off := DefaultSwingCfg()
	off.ISBCancelOnCloseThrough = true
	s4 := m61InvalidLevel(t, line, SideLong, ref)
	s4.FirstTouch.ISBFired = true
	s4.Pending = isbOrder(expiry)
	if n := cancels(s4.stepSwingBar(bars, 1, line, off, bars[1].OpenTime+5*60_000, swingBarLive)); n != 1 {
		t.Fatalf("keep OFF: the inside-bar order is cancelled by the close through, got %d CancelArm", n)
	}
	if s4.Pending != nil || s4.FirstTouch.ISBFired {
		t.Fatalf("keep OFF: cancelled unfilled, the level is not spent (rv59): pending %+v touch %+v", s4.Pending, s4.FirstTouch)
	}
}

// TestM61SwingISBKnobsOnTheTape — the two knobs reach the SwingTick walk (not
// only the unit seam): on the swingTape harness (line 10,168.16, support) a
// close through, then candle 1 + its inside bar BOTH still below the line, the
// inside bar's high above it → a long under not_required; nothing under required.
func TestM61SwingISBKnobsOnTheTape(t *testing.T) {
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10200, 10210, 10195, 10200),  // prev: above → support
		mk5m(t, 15, 5, 5, 10190, 10192, 10160, 10150),  // touch, closes THROUGH → invalid
		mk5m(t, 15, 5, 10, 10150, 10185, 10140, 10155), // candle 1, closes below
		mk5m(t, 15, 5, 15, 10152, 10175, 10148, 10160), // inside bar, closes below; high 10,175 > the line
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	es := m463Entries(m463Walk(t, s, bars, 4, DefaultSwingCfg()))
	if len(es) != 1 || es[0].Side != SideLong || es[0].Price != 10175 || abs(es[0].Stop-swingLineGolden) > 0.01 {
		t.Fatalf("not_required on the tape: want LONG 10,175 stop AT the line %.2f, got %+v (refused %+v)", swingLineGolden, es, s.refused)
	}
	req := DefaultSwingCfg()
	req.ISBCloseBackRequired = true
	s2 := &SwingState{}
	if es := m463Entries(m463Walk(t, s2, bars, 4, req)); len(es) != 0 {
		t.Fatalf("required on the tape: no entry, got %+v", es)
	}
}

// TestM61NormalizeSwingISBCloseBack — canon 28: one canonicalizer, "" → the
// course default, unknown → fails closed to not_required with ok=false.
func TestM61NormalizeSwingISBCloseBack(t *testing.T) {
	for in, want := range map[string]struct {
		mode string
		ok   bool
	}{
		"": {SwingISBCloseBackNotRequired, true}, " Required ": {SwingISBCloseBackRequired, true},
		"not_required": {SwingISBCloseBackNotRequired, true}, "maybe": {SwingISBCloseBackNotRequired, false},
	} {
		if m, ok := NormalizeSwingISBCloseBack(in); m != want.mode || ok != want.ok {
			t.Fatalf("NormalizeSwingISBCloseBack(%q) = %q %v, want %q %v", in, m, ok, want.mode, want.ok)
		}
	}
}

// m61PreP4Swing puts the two m466 P4 swing knobs at their pre-#61 values —
// mentor_swing_isb_close_back=required, mentor_swing_isb_keep OFF — for the
// tests and real-tape pins written against the strict R24 read. The #61
// defaults (not_required, keep ON) are pinned in this file.
func m61PreP4Swing(c *SwingCfg) {
	c.ISBCloseBackRequired = true
	c.ISBCancelOnCloseThrough = true
}

// m61PreP4 is m61PreP4Swing on the evaluator config (replay cfgMut).
func m61PreP4(c *Config) { m61PreP4Swing(&c.Swing) }

// TestM61SwingReplay1028Default — the #61 swing ISB defaults on the REAL
// 10-28 17:00 → 10-29 07:00 CT tape (the m463 replay: recorded MNQ 12-26 1m,
// production runSwing call site, emulated broker), on the record next to the
// strict-read pins in m463_swing_replay_test.go. Per contract, points:
//
//	pre-#61 (required, keep OFF):  19:00 ISB long 29,699.25 → target 29,752.97 (+53.72)
//	                               01:55 ISB long 29,654.25 → stop 29,644.48 (−9.77, trade 715)
//	                               18:20 ISB long 29,730.75 → cancelled 18:40 unfilled
//	#61 default (not_required, keep ON):
//	                               17:50 ISB long 29,660.00 → stop 29,645.53 (−14.47)  NEW
//	                               23:55 ISB short 29,638.00 → stop 29,642.04 (−4.04)   NEW (trades 713/714's bar)
//	                               01:45 ISB long 29,658.75 → stop 29,644.48 (−14.27)  NEW (replaces 715)
//	                               the 19:00 and 01:55 inside bars no longer arm (the level's ISB already filled)
//
// The reject arms (17:25, 17:30, 01:30, 05:30 bars) are unchanged. A mentor
// question is recorded for the candle-1 = the through candle itself case
// (AUDIT-CHECKLIST, m466 P4). This pin moves only with the course rule.
func TestM61SwingReplay1028Default(t *testing.T) {
	r := m463Replay(t, nil)
	type want struct {
		bar        int64
		side       Side
		entry, stp float64
	}
	for _, w := range []want{
		{1793227800000, SideLong, 29660.00, m463Line1700},
		{1793249700000, SideShort, 29638.00, m463Line2100},
		{1793256000000, SideLong, 29658.75, m463Line0100},
	} {
		in, ok := r.entryAt(w.bar)
		if !ok || in.Side != w.side || in.Price != w.entry || abs(in.Stop-w.stp) > 0.01 || !strings.Contains(in.Reason, "no close back needed") {
			t.Fatalf("default: the inside bar %d must arm %s %.2f stop AT the line %.2f, got %+v ok=%v\n%s", w.bar, w.side, w.entry, w.stp, in, ok, r.dump())
		}
	}
	for _, gone := range []int64{1793232000000, 1793256600000, 1793229600000} {
		if in, ok := r.entryAt(gone); ok {
			t.Fatalf("default: the pre-#61 inside bar %d no longer arms (the level's earlier ISB filled), got %+v\n%s", gone, in, r.dump())
		}
	}
	for _, rej := range []int64{1793226300000, 1793226600000, 1793255400000, 1793269800000} {
		if _, ok := r.entryAt(rej); !ok {
			t.Fatalf("default: the reject arm %d is unchanged\n%s", rej, r.dump())
		}
	}
	ends := strings.Join(r.Ends, "\n")
	for _, e := range []string{"swing-1793227800000 stop @29645.53", "swing-1793249700000 stop @29642.04", "swing-1793256000000 stop @29644.48"} {
		if !strings.Contains(ends, e) {
			t.Fatalf("default: want end %q\n%s", e, r.dump())
		}
	}
	// 17:40: the inside bar of the 17:35 through candle whose buy stop 29,644.25
	// would rest under the 29,645.53 support — NAMED, never armed (arm 475's bar).
	if !r.noteHas("17:40", SwingRefusedISBEntryBeyondLine) {
		t.Fatalf("default: the 17:40 inside bar must be NAMED %s\n%s", SwingRefusedISBEntryBeyondLine, r.dump())
	}
}

// m61PreP4GoldenPath is the m434 golden as it stood at the m466 P4 base
// (origin/fix/m465-video-gaps-all 45053f78a), gzip -n -9.
const m61PreP4GoldenPath = "testdata/m61_prep4_tick_identity.golden.gz"

// TestM61StrictReadIsThePreP4Golden — owner values are sacred: a strategy that
// STORES mentor_swing_isb_close_back=required and mentor_swing_isb_keep=false
// must get the pre-#61 swing exactly. Replays the four m434 tapes (5,216 ticks)
// under m61PreP4 and compares every tick — intents, State, notes, levels,
// trendlines — against the pre-P4 golden. Zero rows may differ.
// MUTANT: drop the ISBCloseBackRequired clause in walkSwingBar (or the
// required-mode reason text) → intents/notes diverge → RED.
func TestM61StrictReadIsThePreP4Golden(t *testing.T) {
	boxPeak1Level = false    // pre-m473 B2 construction: the frozen golden predates peak-1 / level pairing
	firstRTH5mNotISB = false // pre-m473 B4: the frozen golden predates the 08:30-candle-is-not-ISB rule [Oct D4.3 @ 57:20-57:35]
	t.Cleanup(func() { boxPeak1Level = true; firstRTH5mNotISB = true })
	raw, err := os.Open(m61PreP4GoldenPath)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	zr, err := gzip.NewReader(raw)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimSpace(string(body)), "\n")
	// Release #61 merged head: the P1/P2/P3 packages moved the same tapes, so
	// the strict read chains every package's pre-#61 values (+ the P1 G37
	// legacy hook) — m466Rel61PreValues; the golden is the #60 m434 golden.
	midRangeLegacyRole.Store(true)
	defer midRangeLegacyRole.Store(false)
	boxConflictLegacySkip.Store(true) // release #61 review round 2 (no knob): the pre-#61 conflict skip of the box-return walk
	defer boxConflictLegacySkip.Store(false)
	var got []string
	for _, c := range m434Cases() {
		inner := c.cfg
		c.cfg = func() Config { x := inner(); m466Rel61PreValues(&x); return x }
		got = append(got, m434Digest(t, c)...)
	}
	if len(got) != len(want) {
		t.Fatalf("ticks %d, pre-P4 golden %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("strict read diverges from the pre-#61 swing at row %d:\n got  %s\n want %s", i, got[i], want[i])
		}
	}
}
