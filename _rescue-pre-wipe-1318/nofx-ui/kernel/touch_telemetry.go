package kernel

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nofx/market"
)

// TOUCH TELEMETRY (Pack B addendum, 2026-08-26) — machine read of level
// reactions. ADVISORY: zero gates, zero order authority. Every threshold is
// env-tunable. Citation semantics per order-flow research:
//   - REJECTION: price probes past a level but CLOSES back on the approach
//     side (sellers/buyers defended it — the "spring").
//   - ACCEPTANCE/ABSORPTION: closes accumulate THROUGH the level (the level is
//     being absorbed → breakout fuel).
//   - Volume spike + fast approach = initiative probes (liquidity run); slow
//     drift + no volume = passive test (weak signal).

// ── env thresholds ──────────────────────────────────────────────────────────

// TouchBandTicks is the touch-proximity band in MNQ ticks (16 = 4.00 pts).
func TouchBandTicks() int {
	if v := os.Getenv("TOUCH_BAND_TICKS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 16
}

// TouchEpisodeMaxBars closes an episode after this many 1m bars even if price
// never leaves the band (default 12).
func TouchEpisodeMaxBars() int {
	if v := os.Getenv("TOUCH_EPISODE_MAX_BARS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 12
}

// TouchVolLookback is the pre-episode volume-average window (default 20 bars).
func TouchVolLookback() int {
	if v := os.Getenv("TOUCH_VOL_LOOKBACK"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 20
}

// TouchApproachBars is the approach-speed window before the touch (default 5).
func TouchApproachBars() int {
	if v := os.Getenv("TOUCH_APPROACH_BARS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 5
}

// TouchBandPoints converts the tick band to points (MNQ tick 0.25).
func TouchBandPoints() float64 { return float64(TouchBandTicks()) * 0.25 }

// ── episode model ───────────────────────────────────────────────────────────

// TouchEpisode is one open-or-closed price-vs-level interaction.
type TouchEpisode struct {
	TraderID   string  `json:"trader_id"`
	Symbol     string  `json:"symbol"`
	Label      string  `json:"label"`
	LevelPrice float64 `json:"level_price"`
	Number     int     `json:"number"` // 1st / 2nd / 3rd+ touch of this level
	OpenedAtMs int64   `json:"opened_at_ms"`
	ClosedAtMs int64   `json:"closed_at_ms"` // 0 while open
	BarsIn     int     `json:"bars_in"`
	// Penetration: max pts THROUGH the level (approach-side aware).
	PenetrationPts float64 `json:"penetration_pts"`
	WickPenPts     float64 `json:"wick_pen_pts"` // through via high/low only
	BodyPenPts     float64 `json:"body_pen_pts"` // through via CLOSES
	Close1m        string  `json:"close_1m"`     // reject | accept | "" (open)
	Close5m        string  `json:"close_5m"`
	VolRatio       float64 `json:"vol_ratio"`    // episode vol ÷ pre-episode 20-bar avg
	ApproachATR    float64 `json:"approach_atr"` // pts in approach window ÷ ATR
	Shape          string  `json:"shape"`        // rejection | acceptance | chop | forming
	// approachFrom records which side price approached from.
	approachFrom string
}

// touchTracker is the per-(level) episode state machine.
type touchLevelState struct {
	opened   int // episodes ever opened this process (touch numbering)
	active   *TouchEpisode
	last     *TouchEpisode // last CLOSED episode (card chip state)
	ring     []market.Kline
}

// TouchRegistry is the process-wide telemetry state, keyed trader+symbol+level.
type TouchRegistry struct {
	mu     sync.Mutex
	states map[string]*touchLevelState
}

var touchRegistry = &TouchRegistry{states: map[string]*touchLevelState{}}

// TouchEpisodeSink is installed by the trader layer (once) to persist CLOSED
// episodes. Nil → telemetry stays in-memory only (tests).
var TouchEpisodeSink func(TouchEpisode)

// SetTouchEpisodeSink installs the persistence hook.
func SetTouchEpisodeSink(fn func(TouchEpisode)) { TouchEpisodeSink = fn }

func touchKey(traderID, symbol, label string, price float64) string {
	return traderID + "|" + symbol + "|" + label + "|" + strconv.FormatFloat(price, 'f', 2, 64)
}

// TouchUpdate feeds one cycle: bars (ascending 1m, closed only preferred), the
// seated levels, and the session ATR. Returns episodes CLOSED this cycle (the
// caller persists them via the sink).
func TouchUpdate(traderID, symbol string, bars []market.Kline, levels []ScoredLevel, atr float64, now time.Time) []TouchEpisode {
	nowMs := now.UnixMilli()
	cb := closedBars(bars, now)
	if len(cb) == 0 {
		return nil
	}
	last := cb[len(cb)-1]
	price := last.Close
	band := TouchBandPoints()
	maxBars := TouchEpisodeMaxBars()
	volWindow := TouchVolLookback()
	apprWindow := TouchApproachBars()

	var closed []TouchEpisode
	touchRegistry.mu.Lock()
	defer touchRegistry.mu.Unlock()
	for _, l := range levels {
		if l.Price <= 0 {
			continue
		}
		key := touchKey(traderID, symbol, l.Label, l.Price)
		st := touchRegistry.states[key]
		if st == nil {
			st = &touchLevelState{}
			touchRegistry.states[key] = st
		}
		// Roll the bar ring (dedup by OpenTime).
		for _, b := range cb {
			if len(st.ring) == 0 || b.OpenTime > st.ring[len(st.ring)-1].OpenTime {
				st.ring = append(st.ring, b)
			}
		}
		if maxRing := volWindow + apprWindow + maxBars + 8; len(st.ring) > maxRing {
			st.ring = st.ring[len(st.ring)-maxRing:]
		}
		dist := minBarDist(last, l.Price)
		if st.active == nil {
			if dist <= band {
				// OPEN episode.
				st.opened++
				ep := &TouchEpisode{
					TraderID: traderID, Symbol: symbol, Label: l.Label, LevelPrice: l.Price,
					Number: st.opened, OpenedAtMs: last.OpenTime,
				}
				ep.approachFrom = approachSide(last.Close, l.Price)
				st.active = ep
			}
			continue
		}
		// Episode active — accumulate.
		ep := st.active
		// BarsIn counts the RING bars inside the episode (deterministic under
		// any call cadence, not call-counting).
		ep.BarsIn = 0
		var epBars []market.Kline
		for _, b := range st.ring {
			if b.OpenTime >= ep.OpenedAtMs {
				ep.BarsIn++
				epBars = append(epBars, b)
			}
		}
		// Penetration is measured on EPISODE bars only — pre-episode ring bars
		// (kept for the vol-ratio/approach windows) must never count as
		// "through" the level.
		pen, wick, body := penetrationStats(epBars, l.Price, ep.approachFrom)
		ep.PenetrationPts, ep.WickPenPts, ep.BodyPenPts = pen, wick, body
		// Close side (1m): the current bar's close vs approach side.
		ep.Close1m = closeSide(last.Close, l.Price, ep.approachFrom)
		// Close side (5m): last completed 5m close from the ring.
		ep.Close5m = closeSide5m(st.ring, l.Price, ep.approachFrom, nowMs)
		// Close the episode?
		if dist > band || ep.BarsIn >= maxBars {
			ep.ClosedAtMs = last.CloseTime
			ep.VolRatio = volRatio(st.ring, ep.OpenedAtMs, volWindow)
			ep.ApproachATR = approachATR(st.ring, ep.OpenedAtMs, apprWindow, atr)
			ep.Shape = classifyShape(ep)
			st.active = nil
			st.last = ep
			closed = append(closed, *ep)
		}
	}
	_ = price
	return closed
}

// ── pure metric helpers ─────────────────────────────────────────────────────

func minBarDist(b market.Kline, level float64) float64 {
	if b.Low <= level && b.High >= level {
		return 0
	}
	if b.High < level {
		return level - b.High
	}
	return b.Low - level
}

// approachSide returns "below" when price approaches from below.
func approachSide(close, level float64) string {
	if close < level {
		return "below"
	}
	return "above"
}

// penetrationStats: max pts through the level. From below: through = high−level
// (wick) and close−level when close>level (body); from above mirrored.
func penetrationStats(ring []market.Kline, level float64, from string) (pen, wick, body float64) {
	for _, b := range ring {
		var w, bd float64
		if from == "below" {
			if b.High > level {
				w = b.High - level
			}
			if b.Close > level {
				bd = b.Close - level
			}
		} else {
			if b.Low < level {
				w = level - b.Low
			}
			if b.Close < level {
				bd = level - b.Close
			}
		}
		if w > wick {
			wick = w
		}
		if bd > body {
			body = bd
		}
	}
	pen = math.Max(wick, body)
	return
}

// closeSide classifies a close: reject = back on the approach side, accept =
// through the level.
func closeSide(close, level float64, from string) string {
	if from == "below" {
		if close <= level {
			return "reject"
		}
		return "accept"
	}
	if close >= level {
		return "reject"
	}
	return "accept"
}

// closeSide5m buckets the ring into 5m closes and classifies the last one.
func closeSide5m(ring []market.Kline, level float64, from string, nowMs int64) string {
	if len(ring) == 0 {
		return ""
	}
	var closes []float64
	for i, b := range ring {
		if b.CloseTime >= nowMs {
			continue
		}
		if i > 0 && b.OpenTime-ring[i-1].OpenTime < 5*60_000 {
			// Same 5m bucket as the previous bar — replace its close.
			closes[len(closes)-1] = b.Close
		} else {
			closes = append(closes, b.Close)
		}
	}
	if len(closes) == 0 {
		return ""
	}
	return closeSide(closes[len(closes)-1], level, from)
}

// volRatio: episode volume ÷ average volume of the bars before the episode.
func volRatio(ring []market.Kline, openedAtMs int64, lookback int) float64 {
	var ep float64
	var pre []float64
	opened := false
	for _, b := range ring {
		if !opened && b.OpenTime >= openedAtMs {
			opened = true
		}
		if opened {
			ep += b.Volume
		} else {
			pre = append(pre, b.Volume)
		}
	}
	// A14 (mega-research 2026-08-26) — the old branch
	// `if preN > float64(lookback) { _ = pre }` was DEAD CODE: the baseline
	// averaged ALL pre-episode bars (up to ~45). Fix: only the last `lookback`
	// pre-bars count (TOUCH_VOL_LOOKBACK, default 20 — as documented).
	if lookback > 0 && len(pre) > lookback {
		pre = pre[len(pre)-lookback:]
	}
	var preSum float64
	for _, v := range pre {
		preSum += v
	}
	if len(pre) == 0 || preSum <= 0 {
		return 0
	}
	return ep / (preSum / float64(len(pre)))
}

// approachATR: pts covered in the approach window ÷ ATR (0 = n/a).
func approachATR(ring []market.Kline, openedAtMs int64, window int, atr float64) float64 {
	if atr <= 0 {
		return 0
	}
	// Find the bar at episode open; measure back `window` bars.
	idx := -1
	for i := range ring {
		if ring[i].OpenTime >= openedAtMs {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return 0
	}
	from := idx - window
	if from < 0 {
		from = 0
	}
	move := math.Abs(ring[idx].Close - ring[from].Close)
	return move / atr
}

// classifyShape turns the close-side/penetration facts into the spec shape.
func classifyShape(ep *TouchEpisode) string {
	if ep.Close1m == "accept" && ep.BodyPenPts > 0 {
		return "acceptance"
	}
	if ep.Close1m == "reject" {
		return "rejection"
	}
	return "chop"
}

// ── rendering (T2/T3/T4) ────────────────────────────────────────────────────

// ActiveTouchEpisodes returns the OPEN episodes for a trader+symbol, nearest
// to `price` first.
func ActiveTouchEpisodes(traderID, symbol string, price float64) []TouchEpisode {
	touchRegistry.mu.Lock()
	defer touchRegistry.mu.Unlock()
	var out []TouchEpisode
	for k, st := range touchRegistry.states {
		if !strings.HasPrefix(k, traderID+"|"+symbol+"|") || st.active == nil {
			continue
		}
		out = append(out, *st.active)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return math.Abs(out[i].LevelPrice-price) < math.Abs(out[j].LevelPrice-price)
	})
	return out
}

// RenderTouchLines renders the T2 executor/watcher TOUCH lines (max 2 nearest,
// facts only).
func RenderTouchLines(traderID, symbol string, price float64, maxLines int) string {
	eps := ActiveTouchEpisodes(traderID, symbol, price)
	if len(eps) == 0 {
		return ""
	}
	if maxLines <= 0 {
		maxLines = 2
	}
	if len(eps) > maxLines {
		eps = eps[:maxLines]
	}
	var b strings.Builder
	for _, ep := range eps {
		shape := ep.Shape
		if shape == "" {
			shape = "forming"
		}
		ord := ordinal(ep.Number)
		through := "none"
		if ep.PenetrationPts > 0 {
			through = fmt.Sprintf("through %.0fpt", ep.PenetrationPts)
			if ep.WickPenPts > 0 && ep.BodyPenPts <= 0 {
				through = fmt.Sprintf("wick-through %.0fpt", ep.PenetrationPts)
			}
		}
		vol := "n/a"
		if ep.VolRatio > 0 {
			vol = fmt.Sprintf("vol %.1f×avg", ep.VolRatio)
		}
		speed := ""
		if ep.ApproachATR > 0 {
			kind := "drift"
			if ep.ApproachATR >= 1.0 {
				kind = "fast"
			}
			speed = fmt.Sprintf("%s approach %.1f×ATR", kind, ep.ApproachATR)
		}
		closeBit := ""
		if ep.Close1m != "" {
			closeBit = fmt.Sprintf(", 1m closed %s", sideWord(ep.Close1m, ep.approachFrom))
		}
		parts := []string{}
		for _, s := range []string{fmt.Sprintf("TOUCH: %s %.2f", ep.Label, ep.LevelPrice), ord + " touch", through + closeBit, vol, speed, "shape: " + shape} {
			if s != "" && !strings.HasSuffix(s, "· ") {
				parts = append(parts, s)
			}
		}
		b.WriteString(strings.Join(parts, " · ") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// TouchStateForCard returns the T4 card chip state for a seated level:
// touching | rejected | accepted | approaching | "" (none).
func TouchStateForCard(traderID, symbol, label string, levelPrice, price float64, nowMs int64) string {
	band := TouchBandPoints()
	touchRegistry.mu.Lock()
	st := touchRegistry.states[touchKey(traderID, symbol, label, levelPrice)]
	touchRegistry.mu.Unlock()
	if st == nil {
		if math.Abs(price-levelPrice) <= 2*band {
			return "approaching"
		}
		return ""
	}
	if st.active != nil {
		return "touching"
	}
	if st.last != nil && nowMs-st.last.ClosedAtMs < 30*60_000 {
		switch st.last.Shape {
		case "rejection":
			return "rejected"
		case "acceptance":
			return "accepted"
		}
	}
	if math.Abs(price-levelPrice) <= 2*band {
		return "approaching"
	}
	return ""
}

// RenderScenarioTouchTies (T3, 2026-08-26) — for each scenario whose
// confirm{} ref_price sits within 3 points of an OPEN touch episode's level,
// append the live shape to the advisory. No gates — the confirm machinery is
// unchanged; this line only surfaces the touch facts beside it.
func RenderScenarioTouchTies(traderID, symbol string, doc *PlanDoc, price float64) string {
	if doc == nil {
		return ""
	}
	eps := ActiveTouchEpisodes(traderID, symbol, price)
	if len(eps) == 0 {
		return ""
	}
	var b strings.Builder
	for _, sc := range doc.Scenarios {
		if sc.Confirm == nil || sc.Confirm.RefPrice <= 0 {
			continue
		}
		for _, ep := range eps {
			if math.Abs(ep.LevelPrice-sc.Confirm.RefPrice) > 3.0 {
				continue
			}
			shape := ep.Shape
			if shape == "" {
				shape = "forming"
			}
			fmt.Fprintf(&b, "confirm %s NOT MET — touch active at %s %.2f: %s shape forming\n", sc.ID, ep.Label, ep.LevelPrice, shape)
			break
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func ordinal(n int) string {
	switch n {
	case 1:
		return "1st"
	case 2:
		return "2nd"
	default:
		return fmt.Sprintf("%dth", n)
	}
}

func sideWord(side, from string) string {
	if side == "reject" {
		if from == "below" {
			return "back below"
		}
		return "back above"
	}
	if from == "below" {
		return "through above"
	}
	return "through below"
}
