package mentor

import (
	"fmt"
	"strings"
	"time"

	"vl/kernel"
	"vl/market"
)

// ── D4.3 wick microscalp — the ZONE (m465 P5; knob wick_microscalp, default OFF)
//
// "Follow trend 5, kiếm lệnh 1" [Ngày 4.3]. The mentor calls it ADVANCED and
// asks people who still get muddled trading ranges not to practise it first
// ("làm ơn đừng… đừng có tập khúc này đầu liền, từ từ rập vô sau" [D4.3
// @04:55–05:17]) — so the whole setup sits behind a knob, default OFF.
//
// The procedure, in his order [D4.3 @14:43–15:09]:
//
//  1. Read the 5m TREND left to right — not the 1m one (R6/R7). Here: the
//     d43_trend_source knob (default "structure" = structureTrend5m, the
//     confirmed 5m swing structure [D4.3 @12:43–12:54]; "trigger_line" = the
//     5m trigger direction). A ranging read ("") lets the run's own side
//     decide — "cho dù là khung 5 phút nó đang ranging" [@04:44–04:55] (R13).
//  2. Find 2–3 closed 5m candles pulling wicks the SAME way and WITH the
//     trend (R1/R2/R3/R8): lower wicks in an uptrend (long), upper wicks in a
//     downtrend (short). "từ 2 cây 5 phút là đẹp… at least là phải 2 cây, tới 3
//     cây thì 2 cây là ok" [@06:38–06:42]; "là tới cây nến 3 thôi" [@13:29];
//     "Phải rút cùng chiều… rút ngược chiều nhau nó khó lắm" [@03:12–03:18];
//     "đang xu hướng giảm mà rút dưới là tôi không biết đường tôi đánh"
//     [@13:32–13:44].
//  3. Draw the zone box at the wick cluster (R5/R10/R12) and EXTEND it right:
//     "Thì có phải là mọi người kéo dài nó qua, nó vẫn có setup để anh chị vô
//     lệnh mà" [@06:03–06:07]. It lives until a 1m close beyond its FAR edge
//     (wick_zone_broken) or the trading-day end; one per side; a new run at a
//     NEW place replaces it.
//  4. Never enter at market — wait for price to come back and for a 1m setup
//     (R13/R14/R15): "Vô market là nó đâm cho lủng bụng" [@05:26–05:46];
//     "potential high low khung giờ nhỏ hơn, hay là insert bar" [@06:13–06:29].
//     The near edge is a LOCATION (KindWickZone, levelIsLocation) so a 1m
//     PHL/PLH touching it arms through the ordinary touch loop; an ISB at the
//     zone is the ordinary ISB (never location-gated) — the zone adds the
//     5m-wick target ladder (wick_microscalp.go) and the optional safe play.
//  5. Safe play (R20, sub-knob wick_safe_play, default OFF): "mọi người đợi
//     nó nằm ở dưới cái box… là thêm một sự confirm" [@14:07–14:27].
//
// Every refusal is named and counted: wick_run_too_short,
// wick_mixed_direction, wick_against_trend, wick_zone_broken,
// wick_zone_no_setup, wick_safe_play_wait, wick_target_no_room,
// wick_zone_wrong_side.

const (
	// wickRunWindow5m — the run is read on the last 3 closed 5m candles:
	// "là tới cây nến 3 thôi, cho tới cây nến 3 thôi" [D4.3 @13:29].
	wickRunWindow5m = 3
	// wickRunMin — "at least là phải 2 cây" [D4.3 @06:38–06:42].
	wickRunMin = 2
	// wickZoneMinPts / wickZoneMaxPts — the box heights he drew: Ex.1 5.00
	// (28,880.50–28,885.50) and Ex.4 14.25 (29,261.00–29,275.25) [D4.3 screen
	// @03:35–03:46, @15:25–15:27]. A wick cluster tighter than 5 pts is widened
	// to 5 from its FAR edge (the extreme wick); a looser one is capped at 14.25
	// from the far edge. Pinned from the drawings; no number is spoken.
	wickZoneMinPts = 5.0
	wickZoneMaxPts = 14.25
	// wickAtZoneBars — "price is at the zone" reads the last four closed 1m
	// bars (an exec-2m ISB pair spans four 1m bars). Ours.
	wickAtZoneBars = 4
)

// Named wick_* refusals (m465 P5).
const (
	wickRefuseRunTooShort  = "wick_run_too_short"
	wickRefuseMixed        = "wick_mixed_direction"
	wickRefuseAgainstTrend = "wick_against_trend"
	wickRefuseZoneBroken   = "wick_zone_broken"
	wickRefuseNoSetup      = "wick_zone_no_setup"
	wickRefuseSafePlayWait = "wick_safe_play_wait"
	wickRefuseTargetNoRoom = "wick_target_no_room"
	wickRefuseWrongSide    = "wick_zone_wrong_side"
)

// WickZone is one D4.3 wick-run box.
type WickZone struct {
	Key    string  `json:"key"` // "wick_zone:<side>:<FormedMs>"
	Side   Side    `json:"side"`
	Top    float64 `json:"top"`
	Bottom float64 `json:"bottom"`
	// FormedMs is the open time of the run's first wick candle (the box's left
	// edge on the chart); FormedAt is the close time of the 5m candle that
	// completed the run — only LATER 1m bars visit or break the zone.
	FormedMs int64  `json:"formed_ms"`
	FormedAt int64  `json:"formed_at"`
	Day      string `json:"day"` // trading day (17:00 CT roll) the zone belongs to
	// InVisit / VisitSetup track the current visit of price to the zone, so a
	// visit that produced no setup is named once (wick_zone_no_setup).
	InVisit    bool `json:"in_visit,omitempty"`
	VisitSetup bool `json:"visit_setup,omitempty"`
}

// WickState is the evaluator's D4.3 state. nil while the knob is OFF, so the
// State JSON is byte-identical to the pre-m465 build.
type WickState struct {
	Long  *WickZone `json:"long,omitempty"`
	Short *WickZone `json:"short,omitempty"`
	// Last5mClose is the close time of the last 5m candle the run detector
	// read — the run is read once per closed 5m candle, never per 1m tick.
	Last5mClose int64 `json:"last_5m_close,omitempty"`
}

// zones returns the standing zones in a fixed order (long, short).
func (s *WickState) zones() []*WickZone {
	var out []*WickZone
	if s == nil {
		return nil
	}
	if s.Long != nil {
		out = append(out, s.Long)
	}
	if s.Short != nil {
		out = append(out, s.Short)
	}
	return out
}

func (s *WickState) slot(side Side) **WickZone {
	if side == SideShort {
		return &s.Short
	}
	return &s.Long
}

// byKey finds the zone a level key ("wick_zone:<side>:<ms>:<edge>") belongs to.
func (s *WickState) byKey(levelKey string) *WickZone {
	for _, z := range s.zones() {
		if strings.HasPrefix(levelKey, z.Key+":") {
			return z
		}
	}
	return nil
}

func (z WickZone) nearEdge() (float64, string) {
	if z.Side == SideShort {
		return z.Bottom, "bottom"
	}
	return z.Top, "top"
}

func (z WickZone) farEdge() (float64, string) {
	if z.Side == SideShort {
		return z.Top, "top"
	}
	return z.Bottom, "bottom"
}

// overlapsBar: the candle's range reaches into the zone.
func (z WickZone) overlapsBar(c market.Kline) bool {
	return c.High >= z.Bottom && c.Low <= z.Top
}

func (z WickZone) overlaps(o WickZone) bool {
	return z.Bottom <= o.Top && o.Bottom <= z.Top
}

// closedBeyondFar — the zone is broken by a 1m CLOSE beyond its far edge
// (under the bottom of a demand box, over the top of a supply box).
func (z WickZone) closedBeyondFar(c market.Kline) bool {
	if z.Side == SideShort {
		return c.Close > z.Top
	}
	return c.Close < z.Bottom
}

// wickRunRead is one side's reading of the run window.
type wickRunRead struct {
	with       int     // candles that pull a with-side wick at all (a lower wick for a long)
	sumWith    float64 // summed with-side wick length over the window
	sumAgainst float64 // summed against-side wick length over the window
}

// ok is the pinned R1/R2/R3 verdict: ≥2 candles pull the with-side wick (so
// with 3 candles at most ONE odd candle that pulls none) AND the summed
// with-side wick beats the summed against-side wick.
//
// A candle with BOTH wicks still pulls from the with side — "Mặc dù là cây
// này nó rút râu ở trên xuống, nhưng nhìn… technically nhìn ra nó vẫn có sự rút
// râu từ dưới rút lên" [D4.3 @01:55–02:02]; his own Ex.1 08:30 candle (O28,906
// H28,933.75 L28,880.50 C28,905.50 [screen @07:13]) has a 27.75 upper wick
// over a 25.00 lower one and he reads it "rút ngược râu lên" [@07:13–07:28].
// So the per-candle test is "has the wick", and the DIRECTION is the sum: an
// alternating run whose wicks balance out — "cây này nó rút ở dưới lên, cây
// này nó rút trên xuống, cây này nó rút dưới lên thì không được"
// [@01:27–01:49] — is mixed.
func (r wickRunRead) ok() bool {
	return r.with >= wickRunMin && r.sumWith > r.sumAgainst
}

func readWickRun(window []market.Kline, side Side) wickRunRead {
	var r wickRunRead
	for _, c := range window {
		lower, upper := wickParts(c)
		w, a := lower, upper
		if side == SideShort {
			w, a = upper, lower
		}
		if w > 0 {
			r.with++
		}
		r.sumWith += w
		r.sumAgainst += a
	}
	return r
}

func oppositeSide(s Side) Side {
	switch s {
	case SideLong:
		return SideShort
	case SideShort:
		return SideLong
	}
	return ""
}

// detectWickRun reads the last ≤3 CLOSED 5m candles against the 5m trend and
// returns the zone (Side != "") or the refusal name ("" = no candidate at all:
// not one with-side wick candle, or fewer than two closed 5m candles).
//
//	trend long/short: the run must be WITH it; a qualifying run on the other
//	side is wick_against_trend (R8); two or more wick candles whose wicks do
//	not out-pull the other side are wick_mixed_direction (R2/R3); a single
//	with-side wick candle is wick_run_too_short (R1).
//	trend "" (ranging): the run's own side decides (R13).
func detectWickRun(closed5m []market.Kline, trend Side) (WickZone, string) {
	if len(closed5m) < wickRunMin {
		return WickZone{}, ""
	}
	from := len(closed5m) - wickRunWindow5m
	if from < 0 {
		from = 0
	}
	window := closed5m[from:]
	long, short := readWickRun(window, SideLong), readWickRun(window, SideShort)
	read := func(s Side) wickRunRead {
		if s == SideShort {
			return short
		}
		return long
	}
	switch trend {
	case SideLong, SideShort:
		rt, ro := read(trend), read(oppositeSide(trend))
		switch {
		case rt.ok():
			return wickZoneFrom(window, trend), ""
		case ro.ok():
			return WickZone{}, wickRefuseAgainstTrend
		case rt.with >= wickRunMin, ro.with >= wickRunMin:
			return WickZone{}, wickRefuseMixed
		case rt.with == 1:
			return WickZone{}, wickRefuseRunTooShort
		}
		return WickZone{}, ""
	default:
		switch {
		case long.ok():
			return wickZoneFrom(window, SideLong), ""
		case short.ok():
			return wickZoneFrom(window, SideShort), ""
		case long.with >= wickRunMin || short.with >= wickRunMin:
			return WickZone{}, wickRefuseMixed
		case long.with == 1 || short.with == 1:
			return WickZone{}, wickRefuseRunTooShort
		}
		return WickZone{}, ""
	}
}

// wickZoneFrom draws the box at the wick cluster (R5 "rút râu ở đâu nhiều
// hơn" [@08:35, 11:29]; R10 the ~3-candle base [@09:17–09:26]): the
// [lowest, highest] wick extreme of the with-side candles — their LOWS for a
// demand (long) box, their HIGHS for a supply (short) box — of the candles
// that pull the with-side wick, widened to 5 pts /
// capped at 14.25 pts from the FAR edge (the extreme wick).
func wickZoneFrom(window []market.Kline, side Side) WickZone {
	var lo, hi float64
	var first int64
	n := 0
	for _, c := range window {
		lower, upper := wickParts(c)
		w, p := lower, c.Low
		if side == SideShort {
			w, p = upper, c.High
		}
		if w <= 0 {
			continue // the odd candle (R3) — no with-side wick — does not draw the box
		}
		if n == 0 || p < lo {
			lo = p
		}
		if n == 0 || p > hi {
			hi = p
		}
		if n == 0 {
			first = c.OpenTime
		}
		n++
	}
	z := WickZone{Side: side, Top: hi, Bottom: lo, FormedMs: first, FormedAt: window[len(window)-1].CloseTime}
	if side == SideShort {
		if z.Top-z.Bottom < wickZoneMinPts {
			z.Bottom = z.Top - wickZoneMinPts
		}
		if z.Top-z.Bottom > wickZoneMaxPts {
			z.Bottom = z.Top - wickZoneMaxPts
		}
	} else {
		if z.Top-z.Bottom < wickZoneMinPts {
			z.Top = z.Bottom + wickZoneMinPts
		}
		if z.Top-z.Bottom > wickZoneMaxPts {
			z.Top = z.Bottom + wickZoneMaxPts
		}
	}
	z.Key = fmt.Sprintf("%s:%s:%d", KindWickZone, side, z.FormedMs)
	return z
}

// wickTrend is the 5m trend the run must follow (R6/R8): the d43_trend_source
// knob — "structure" (default) reads the confirmed 5m swing structure left to
// right [D4.3 @12:43–12:54, @14:43–15:03]; "trigger_line" the 5m trigger
// direction. "" = ranging.
func (e *Evaluator) wickTrend(closed5m []market.Kline) Side {
	if e.Cfg.D43TrendSource == D43TrendTriggerLine {
		return e.State.Trigger.Dir
	}
	return structureTrend5m(closed5m)
}

// wickZoneLevels advances the D4.3 state on this tick and returns the levels
// it contributes: each standing zone's NEAR edge as a location
// (KindWickZone), and — while price is at the zone — the 5m-wick target
// ladder (KindWickMicroscalp, target-only). Knob OFF → nil and no state.
func (e *Evaluator) wickZoneLevels(bars []market.Kline, now int64) []Level {
	if !e.Cfg.WickMicroscalpEnabled || len(bars) == 0 {
		return nil
	}
	if e.State.Wick == nil {
		e.State.Wick = &WickState{}
	}
	st := e.State.Wick
	cur := bars[len(bars)-1]
	day := tradingDayKey(time.UnixMilli(cur.CloseTime).In(ctime()))

	// R12: a zone dies at the trading-day end or on a 1m close beyond its far edge.
	for _, side := range []Side{SideLong, SideShort} {
		slot := st.slot(side)
		z := *slot
		if z == nil {
			continue
		}
		if z.Day != day {
			e.note("D4.3 wick zone %s %.2f–%.2f ended at the trading-day roll [D4.3 @06:03–06:07]", z.Side, z.Bottom, z.Top)
			*slot = nil
			continue
		}
		if cur.CloseTime > z.FormedAt && z.closedBeyondFar(cur) {
			e.refuse(wickRefuseZoneBroken)
			far, _ := z.farEdge()
			e.note("D4.3 wick zone %s %.2f–%.2f broken: the 1m candle closing %s closed %.2f beyond its far edge %.2f", z.Side, z.Bottom, z.Top, kernel.ClockCT(time.UnixMilli(cur.CloseTime)), cur.Close, far)
			*slot = nil
		}
	}

	// R1/R2/R3/R5/R8/R10: read the run once per CLOSED 5m candle.
	cb := e.closedBucketsMemo(bars, now)
	if n := len(cb); n > 0 && cb[n-1].CloseTime > st.Last5mClose {
		st.Last5mClose = cb[n-1].CloseTime
		z, why := detectWickRun(cb, e.wickTrend(cb))
		if why != "" {
			e.refuse(why)
		}
		if z.Side != "" {
			z.Day = tradingDayKey(time.UnixMilli(z.FormedAt).In(ctime()))
			slot := st.slot(z.Side)
			// One per side. A run re-read over the SAME place keeps the drawn box
			// (he re-fits one box, he does not draw a second); a run at a new
			// place replaces it.
			if z.Day == day && (*slot == nil || !(*slot).overlaps(z)) {
				zz := z
				*slot = &zz
				e.note("D4.3 wick zone %s %.2f–%.2f drawn from the 5m wick run closing %s — extended right until a 1m close beyond %s [D4.3 @03:35–03:46, @06:03–06:07]", z.Side, z.Bottom, z.Top, kernel.ClockCT(time.UnixMilli(z.FormedAt)), func() string { _, n := z.farEdge(); return n }())
			}
		}
	}

	var out []Level
	for _, z := range st.zones() {
		// Visit tracking: a visit that ends with no setup at the zone is named
		// once (R13/R14 "phải đợi setup" — the zone alone is never an entry).
		at := e.wickAtZone(*z, bars)
		switch {
		case at && !z.InVisit:
			z.InVisit, z.VisitSetup = true, false
		case !at && z.InVisit:
			if !z.VisitSetup {
				e.refuse(wickRefuseNoSetup)
			}
			z.InVisit, z.VisitSetup = false, false
		}
		near, edge := z.nearEdge()
		out = append(out, Level{Key: z.Key + ":" + edge, Kind: KindWickZone, Price: near, AtTime: z.FormedMs, FormedMs: z.FormedMs, TF: "5m"})
		if at {
			out = append(out, wickTargetLevels(cb, *z)...)
		}
	}
	return out
}

// wickAtZone: one of the last wickAtZoneBars closed 1m bars (after the zone
// formed) reached into the zone.
func (e *Evaluator) wickAtZone(z WickZone, bars []market.Kline) bool {
	for i := len(bars) - 1; i >= 0 && i >= len(bars)-wickAtZoneBars; i-- {
		if bars[i].CloseTime > z.FormedAt && z.overlapsBar(bars[i]) {
			return true
		}
	}
	return false
}

// wickZoneDisplayLevels returns each standing zone's FAR edge for the chart
// only (ChartLevels): the near edge is already in the level set, so the panel
// pairs ":top"/":bottom" into one rectangle (chartOverlays.ts). Nothing that
// trades reads it. Knob OFF → nil.
func (e *Evaluator) wickZoneDisplayLevels() []Level {
	if !e.Cfg.WickMicroscalpEnabled || e.State.Wick == nil {
		return nil
	}
	var out []Level
	for _, z := range e.State.Wick.zones() {
		far, edge := z.farEdge()
		out = append(out, Level{Key: z.Key + ":" + edge, Kind: KindWickZone, Price: far, AtTime: z.FormedMs, FormedMs: z.FormedMs, TF: "5m"})
	}
	return out
}

// wickZoneLeave is the exit pass over the intents leaving Tick (run from
// Tick's defer, before the leave stamp; the m431 pass un-registers the arm of
// any entry dropped here). For every ISB/PHL/PLH entry AT a standing zone on
// the zone's side:
//   - a PHL/PLH anchored on the zone edge but on the OTHER side (a reject of
//     the box from inside it) is dropped — wick_zone_wrong_side (the zone
//     trades only its own side: "bắt ngược lên" from a demand box [@04:44–04:55],
//     "scalp ngược xuống" from a supply box [@15:38–15:47]);
//   - safe play ON: the reference candle must have CLOSED beyond the near edge,
//     else wick_safe_play_wait (R20);
//   - the 5m-wick target must leave ≥1R room, else wick_target_no_room is
//     named (the target falls through to the next level — the 1R room rule
//     itself lives in nextLevelBeyondRoom / targetRoomMultiple,
//     WickTargetRoomMultiple);
//   - the visit is marked as having produced a setup; the reason names the zone.
//
// Knob OFF → out unchanged.
func (e *Evaluator) wickZoneLeave(out []Intent, bars []market.Kline, now int64) []Intent {
	if !e.Cfg.WickMicroscalpEnabled || e.State.Wick == nil || len(e.State.Wick.zones()) == 0 {
		return out
	}
	st := e.State.Wick
	kept := make([]Intent, 0, len(out))
	for _, in := range out {
		if (in.Action != PlaceStopEntry && in.Action != PlaceStopLimitEntry) || (in.Setup != "ISB" && in.Setup != "PHL" && in.Setup != "PLH") {
			kept = append(kept, in)
			continue
		}
		var z *WickZone
		anchored := strings.HasPrefix(in.AnchorKey, string(KindWickZone)+":")
		if anchored {
			z = st.byKey(in.AnchorKey)
			if z != nil && in.Side != z.Side {
				e.refuse(wickRefuseWrongSide)
				e.spendLevelTouch(in.AnchorKey)
				continue
			}
		} else if zz := *st.slot(in.Side); zz != nil && in.Side != "" && wickRefTouches(*zz, in, bars) {
			z = zz
		}
		if z == nil {
			kept = append(kept, in)
			continue
		}
		if e.Cfg.WickSafePlay && !wickRefClosedBeyondNear(*z, in, bars) {
			e.refuse(wickRefuseSafePlayWait)
			if anchored {
				e.spendLevelTouch(in.AnchorKey)
			}
			continue
		}
		z.InVisit, z.VisitSetup = true, true
		risk := abs(in.Price - in.Stop)
		first := 0.0
		for _, p := range wickTargetLadder(e.closedBucketsMemo(bars, now), *z) {
			if in.Side == SideLong && p > in.Price || in.Side == SideShort && p < in.Price {
				first = p
				break
			}
		}
		if first == 0 || abs(first-in.Price) < risk {
			e.refuse(wickRefuseTargetNoRoom)
		}
		e.count("wick_zone_entry")
		in.Reason += fmt.Sprintf(" · at the D4.3 wick zone %s %.2f–%.2f [D4.3 @06:13–06:29]", z.Side, z.Bottom, z.Top)
		kept = append(kept, in)
	}
	return kept
}

// spendLevelTouch ends the current touch of a level for re-emission (the same
// mark DropArm writes): the touch stays TouchReject, but the PHL loop does not
// re-emit it every minute; a NEW reference candle trades again.
func (e *Evaluator) spendLevelTouch(key string) {
	tr, ok := e.State.Touches[key]
	if !ok || tr.RefBar.CloseTime == 0 {
		return
	}
	if e.State.RefusedLevelTouch == nil {
		e.State.RefusedLevelTouch = map[string]int64{}
	}
	e.State.RefusedLevelTouch[key] = tr.RefBar.CloseTime
}

// wickRefTouches: one of the 1m bars of the setup's reference pair (the two
// exec candles ending at RefBarMs) reached into the zone, after it formed.
func wickRefTouches(z WickZone, in Intent, bars []market.Kline) bool {
	if in.RefBarMs == 0 {
		return false
	}
	tf := in.ExecTFMin
	if tf <= 0 {
		tf = 1
	}
	lo := in.RefBarMs - int64(2*tf)*60_000
	for i := len(bars) - 1; i >= 0; i-- {
		b := bars[i]
		if b.CloseTime <= lo {
			break
		}
		if b.CloseTime <= in.RefBarMs && b.CloseTime > z.FormedAt && z.overlapsBar(b) {
			return true
		}
	}
	return false
}

// wickRefClosedBeyondNear — R20 safe play: the reference candle closed beyond
// the near edge (above a demand box, under a supply box). No reference bar →
// false (wait).
func wickRefClosedBeyondNear(z WickZone, in Intent, bars []market.Kline) bool {
	for i := len(bars) - 1; i >= 0; i-- {
		b := bars[i]
		if b.CloseTime < in.RefBarMs {
			return false
		}
		if b.CloseTime == in.RefBarMs {
			if z.Side == SideShort {
				return b.Close < z.Bottom
			}
			return b.Close > z.Top
		}
	}
	return false
}
