// Package mentor is the mentor-mode evaluator (PLAN.md v1, P2): a pure,
// deterministic rule engine that emits trade intents from recorded bars.
//
// Every rule cites /home/hoang/mm-course/notes-local/00-METHOD.md as
// [D<lesson> p<part> @ mm:ss]. The package NEVER places orders — DS-102's
// injector (P3) converts intents into synthetic kernel.Decisions fed through
// the existing executeDecisionWithRecord pipeline. All behaviour sits behind
// Config knobs; Config.Enabled (mentor_mode) defaults to false, and with it
// off nothing in this package is consulted (L4: byte-identical bot).
package mentor

import (
	"strings"

	"vl/market"
)

// Action is what the evaluator asks the outside world to do. It is an intent,
// not an execution: no order, wire frame or ledger write happens here.
type Action string

const (
	// PlaceStopEntry asks the injector to rest a stop-entry order at Price
	// (side, stop and target as computed by the rules).
	PlaceStopEntry Action = "place_stop_entry"
	// PlaceStopLimitEntry is the R1 ISB order type (RULES-FIX-v3): a
	// stop-limit whose limit equals the stop (trigger) price — the order
	// simply does not fill past the limit [D1.4 p1 @ 24:41–24:55].
	PlaceStopLimitEntry Action = "place_stop_limit_entry"
	// ExtendArm asks the injector to push a resting order's expiry_ms forward
	// (N12 correction, CTO 1791007969871): the evaluator extends a stacked ISB
	// arm while the candles stay inside the mother candle.
	ExtendArm Action = "extend_arm"
	// CancelArm asks the injector to cancel a previously emitted arm
	// (ArmID names it). Reason says which rule demands the cancel.
	CancelArm Action = "cancel_arm"
	// LevelInvalid marks a level invalid per §3 [D5.2 p1 @ 19:51]:
	// only ISBs may ever trade there after.
	LevelInvalid Action = "level_invalid"
	// ConfluenceUpgrade (B20, 10-03 evening, OPEN-NUMBERS Q2, CTO-verified
	// D3.4 p3 @09:17–12:59): a school-1 entry was taken WITHOUT the 5m trigger
	// agreeing; the trigger has now flipped to the trade's side — the open
	// position upgrades to confluence (hold >= 1:2, exit C). The trader half
	// (DS-102) matches this ONE definition — no local re-declaration.
	ConfluenceUpgrade Action = "confluence_upgrade"
)

// Side is the trade direction.
type Side string

const (
	SideLong  Side = "long"
	SideShort Side = "short"
)

// LevelKind is the mentor-level taxonomy (local enum; the bot's kernel.LevelKind
// is a different, wider set — see DS-106 §1 for the mapping).
type LevelKind string

const (
	// KindKeyLevel is a colour-change OPEN line on RTH bars (§4.3).
	KindKeyLevel LevelKind = "key_level"
	// KindOldExtreme is an old high/low — the bot's swing levels reused
	// (kernel/levels_swing.go SwingPointLevels).
	KindOldExtreme LevelKind = "old_extreme"
	// KindEMA34 / KindEMA9 — EMA values promoted to levels (§8, §10, §11).
	KindEMA34 LevelKind = "ema34"
	KindEMA9  LevelKind = "ema9"
	// KindEMA34HTF is the location-gate EMA 34 line (knob ema34_tf — R3:
	// default 1m, the trading chart; the 4h EMA 34 is the swing's only).
	KindEMA34HTF LevelKind = "ema34_htf"
	// KindTriggerRetest is the 5m trigger-line retest location (§5.1
	// [D3.4 p2 @ 20:51]: the retest is a level confirm).
	KindTriggerRetest LevelKind = "trigger_retest"
	// KindTrendline is an intraday trendline location (X9 slide 27, D2.3,
	// DAY-3 row 27): two same-role swings joined, valid as a location only
	// after the 3rd touch; discarded by a 5m close through. Location only,
	// never a target.
	KindTrendline LevelKind = "trendline"
	// KindHTFTrigger is the 4h/1h trigger line as a TARGET-LADDER level only
	// (D4.4-15): it joins the level set so nextLevelBeyond can pick it, but it
	// is never touched, never located and never a PHL/PLH place.
	KindHTFTrigger LevelKind = "htf_trigger"
	// KindWickMicroscalp is the D4.3 wick-microscalp target (D4.3-02/-07): the
	// rejecting 5m candles' far wick, target-only — it joins the level set so
	// nextLevelBeyond can pick it, but it is never touched and never a
	// PHL/PLH location.
	KindWickMicroscalp LevelKind = "wick_microscalp"
)

// Level is one mentor level line. Lo/Hi are equal (a line); AtTime is the bar
// open time that produced it (recency and the ≥3-candle PHL/PLH distance use it).
type Level struct {
	Key    string // stable id "<kind>:<price>@<session>" — state joins on this
	Kind   LevelKind
	Price  float64
	AtTime int64
}

// Intent is one evaluator output.
type Intent struct {
	Action Action
	Reason string // cites the rule, never empty for emits
	Side   Side   // for PlaceStopEntry
	Symbol string // "MNQ" — the mentor instrument (00-METHOD header)

	// PlaceStopEntry / PlaceStopLimitEntry fields.
	Price  float64 // stop-entry trigger price
	Limit  float64 // PlaceStopLimitEntry: the limit price (== Price)
	Stop   float64 // stop-loss
	Target float64 // take-profit level
	Flag   string  // sizing/routing flags for the injector, joined with "|" (FlagISBAtOldExtreme, FlagISBInRange)

	// ExpiryMs is the per-order expiry the injector arms on placement (N12
	// correction): cancel when now >= expiry_ms and the order is unfilled.
	// 0 = no expiry — the order is never auto-cancelled by the expiry sweep
	// (the setup carries its own explicit CancelArm instead).
	ExpiryMs int64

	// RefBarMs (N10) is the reference candle's CLOSE time (ms) — the bar the
	// entry geometry was measured from. The injector refuses any entry whose
	// reference candle is not the newest closed bar (a reload replay of stale
	// box/ISB/PHL returns must never become live orders). 0 = not stamped
	// (swing — gated by its own 5m watermark instead).
	RefBarMs int64

	// Confluence is the R2 flag [00-METHOD Risk-reward, D3.4 p3 @ 07:38]:
	// box edge + a key level inside the box or within 2 pts of its edge +
	// the 5m trigger agrees — DS-102's exit-C / size-10 branch reads it.
	Confluence bool
	// Anchor / AnchorKey name the G2 place the setup was taken at (CTO R-b
	// 2026-10-03): the level price, the box edge, or the EMA — NOT the old
	// extreme. The PHL/PLH emit site sets both from the touch level; a plain
	// ISB sets neither (no anchor = not loss-boxed). Limits keys the loss
	// box on AnchorKey when set, else the quarter-tick Anchor.
	Anchor    float64
	AnchorKey string

	// CancelArm / LevelInvalid fields.
	ArmID    string
	LevelKey string

	// P3 size-tier inputs (the trader's mentorContractsFor reads these).
	Setup     string  // "ISB", "PHL", "PLH", "BOX" (SWING4H when it lands)
	StopPts   float64 // |entry - stop|
	TargetPts float64 // |target - entry|
	// SpentDay is the A5 flag (CTO 1791041016051): the §7 verdict for the
	// trading day is DaySpent ("already run 300-400+ before the open"). The
	// evaluator stamps it on EVERY intent; the injector's size table then
	// holds 1-2 (spent_day tier) and the R9 15-pt stop cap applies.
	SpentDay bool
	// HTFAgree: the 4h AND 1h triggers BOTH stand and point this entry's side
	// (D4.4 p1 @16:00 case 1: "4h trigger buy, 1h trigger buy" → trade that
	// side). The evaluator stamps it on every ENTRY where intents leave Tick;
	// the trader's size table needs it for the 20-contract tier ("size 20 only
	// when 4h AND 1h agree and room ≥ 30 pts"). A silent 1h (case 2: follow the
	// 4h) is NOT agreement.
	HTFAgree bool
	// RunnerTarget is the mode-A runner target BEYOND the old high (D2-49 /
	// D2.4 p1 @01:56: "resonance breaks the old high 70–80%"). The PHL/PLH
	// emit site stamps the next level beyond the old extreme (nextLevelBeyond);
	// 0 = none — in mode A the runner's native TP is removed and the exit is
	// the BE stop or EOD flat. The trader's resonance arming consumes it.
	RunnerTarget float64
}

// Config is every knob. Enabled is mentor_mode and defaults to false (L4):
// while false the evaluator is never consulted and the bot is byte-identical.
type Config struct {
	Enabled bool

	// Levels (PLAN v1 §1).
	KeyLevelTFMinutes int     // TF of the colour-change walk; default 60 = the 1H RTH series anchored at 08:30 CT (KEY-LEVEL RULING final, slide 31: "Khung 1H ONLY… Bắt đầu từ lúc market open"). Other TFs walk barsTF+RTH for experimentation only.
	KeyLevelRTHOnly   bool    // walk RTH candles only; default true
	KeyLevelPrunePts  float64 // prune pairs closer than this, keep the more recent; default 20 [D5.3 p1 @ 16:08–16:44]
	EMAPeriod34       int     // default 34
	EMAPeriod9        int     // default 9
	EMATFMinutes      int     // TF the EMA lines are computed on; default 1

	// EMALocationTFMinutes is the LOCATION-GATE EMA 34 timeframe. R3 (RULES
	// FIX v3, verified): intraday setups use the EMA 34 of the TRADING chart
	// — the 1m [D5.4 @ 01:00–01:27, 05:21–05:37] — so the default is 1. The
	// 4h EMA 34 is ONLY for the §8 swing, which carries its own knobs
	// (SwingCfg) and never reads this [D5.2 p1 @ 05:30–05:43]. Other TFs
	// (5/15/60/240) remain allowed for experimentation.
	EMALocationTFMinutes int

	// Touch / close-side (PLAN v1 §2).
	TouchBandPts float64 // literal-touch band around a level; default 4.0 (the bot's 16-tick touch band)
	// LvlRevisitMinPts — L1 knob (CTO 12:19:20Z): the extra departure distance
	// (from the level close) a closed non-touching candle needs to END a visit.
	// Default 0: any closed candle that did not touch ends the visit ("he never
	// states one"). Key-level touch references are per VISIT, not per day.
	LvlRevisitMinPts float64
	// LossDeparturePts — B22 fallback knob (default OFF): the structural
	// departure (swing break / wave break / box exit) is primary. Only for a
	// swing-less place (no old extreme beyond the entry) does this numeric
	// fallback apply when > 0: a closed candle |close - loss price| >= knob
	// frees the place.
	LossDeparturePts float64
	// LocTriggerFilter — mirror of the replay row v5_loc_notrig (CTO
	// 13:20:22Z): true (default) keeps the 5m-trigger filter on LEVEL and BOX
	// rejects; false switches it off for those two only.
	LocTriggerFilter bool
	// TriggerSchool — B20 (10-03 evening, OPEN-NUMBERS Q2, CTO-verified
	// D3.4 p3 @09:17–12:59): 1 (default, his own) = a key-level reject and an
	// FTGL/FTGH box return are taken WITHOUT waiting for the 5m trigger to
	// agree ("em vô trước" @09:22–09:36) — a later flip to the trade's side
	// upgrades the position to confluence. 2 = wait for the flip (school 2,
	// "chọn 1 trong 2" @12:38). Still binding in both schools: the FTGL+BUY-line
	// no-trade zone and the trigger side for every ISB and non-level entry.
	TriggerSchool int
	// PingPong knobs — B21 (10-03 evening, OPEN-NUMBERS Q5, CTO-verified
	// D4.2 p2 @05:17–06:37): the gap between an FTGL floor and an FTGH ceiling
	// must exceed PingPongMinGapPts AND the largest 1m candle of the last
	// PingPongCandleLookback closed bars must stay at or below
	// PingPongCandleMaxPts ("nến tầm mười mấy điểm"). The same candle cap and
	// lookback gate the key-level pair rule (X4 @06:31–08:52: "một cái nến nó
	// bằng cái rank của 2 cái level rồi thì ngồi im").
	PingPongMinGapPts      float64
	PingPongCandleMaxPts   float64
	PingPongCandleLookback int
	// LevelMaxVisits — B23 (10-03 evening, OPEN-NUMBERS Q3, CTO-verified
	// D1.3 p1 @10:32–11:43 "knock knock"): a level trades its first
	// LevelMaxVisits visits of the day; later visits refuse (0 = cap off).
	LevelMaxVisits int
	// EmaMaxCross30m — E4 knob (CTO 12:27:25Z): refuse the EMA34 setup when the
	// close crossed the line this many times over the last 30 closed 1m candles
	// ("xien len xien xuong", D4.2 p1 @ 22:27). Default ON (item 16, CTO
	// 23:49Z): 2 — the most conservative of the replay sensitivity rows
	// v5_ema_cross2 / v5_ema_cross4. MENTOR QUESTION OPEN: the course states no
	// count and no window — at the exact second he says "xiên lên xiên xuống"
	// the frame D4.2 p1 @22:28 shows the indicator already OFF the chart, so
	// 2 is our reading, not his number (reread OPEN-NUMBERS Q4). 0 = OFF.
	EmaMaxCross30m int

	// ISB (PLAN v1 §3).
	ISBBufferPts   float64 // order buffer beyond the wick extremes, BOTH sides; default 1.5 [D1.4 p1 @ 22:22–22:30]
	ISBStopMinPts  float64 // RETIRED for ISB by RULES-FIX-v3 (no fixed stop size); kept for the other setups
	ISBTwentiesPts float64 // a stop at/above this ("in the twenties") must not be taken; default 20 [D4.1 p1 @ 05:41]

	// ISBReverseEMA9Enabled turns on R7, the reverse-ISB-at-EMA9 setup
	// (RULES-FIX-v3, its own knob per the dispatch). Default ON — OWNER RULING
	// 2026-10-04 R-C "do all as mentor": it is the mentor's own reading [D5.4].
	ISBReverseEMA9Enabled bool
	// WickMicroscalpEnabled turns on the D4.3 wick microscalp (advanced —
	// "đừng có tập khúc này đầu tiên" [D4.3 @04:55]): 2+ consecutive CLOSED 5m
	// candles rejecting with wicks the SAME way (lower wicks in an uptrend,
	// upper wicks in a downtrend) put a bounded target at the rejecting
	// candles' far wick. Default OFF (advanced, behind a knob per D4.3-02).
	WickMicroscalpEnabled bool

	// PHL/PLH (PLAN v1 §3).
	PHLMinCandlesFromExtreme int     // entry at least N candles from the old extreme; default 3 [D4.1 p1 @ 09:40 written]
	PHLTargetShyPts          float64 // target this far short of the old extreme; default 5 (Q2 knob; worked example 5–15 [D2.2 p1 @ 06:50])
	PHLEntryBufferPts        float64 // entry buffer beyond the candle extreme (outward); default 1.0 [D2.2 p1 @ 06:50 drawn: high 29,396.25 → entry 29,397.25]

	// Filters (PLAN v1 §4).
	StopCeilingPts float64 // hard stop ceiling; default 25 [D3.3 p1 @ 02:04]
	RoomMultiple   float64 // room rule: reward >= RoomMultiple x risk; default 2 [D5.3 p1 @ 09:16]
	RangeGapPts    float64 // mid-range: levels bracketing price within this gap both sides; default 0 = disabled

	// NearBoxRoomMultiple — Day-3 row 24 [D3.2 p1 @ 21:53–23:08]: a setup
	// whose nearest box edge IN the trade direction is closer than this × its
	// own risk is refused ("sát box"); 0 disables. Default 2 — the same 2R
	// room the target rule demands. Between two boxes (row 25) is exempt.
	NearBoxRoomMultiple float64

	// §7 day gate knobs (fold item 4, DS-106): the method defaults until the
	// routed Config integration lands.
	DayGateSpentPts     float64 // run >= this before the open = spent; default 300 [D5.1 p1 @ 15:57]
	DayGateTargetCapPts float64 // spent-day target cap; default 15 ("15 điểm bán, 10 điểm bán")

	// §8 SWING4H knobs (DS-106): the method defaults. The 5m-zone gate
	// lives in SwingCfg.Respects5mZone (default false, [C]) and is wired
	// at the runSwing call site (CTO swing ruling 2026-10-03).
	Swing SwingCfg

	// §4.1 FTGH/FTGL boxes (DS-106, BOX RULING part 2): 1m regular by
	// default. The evaluator builds the boxes per tick and wires their
	// edges into the location gate and InsideAnyBox into the bans.
	Box BoxCfg

	// OrbGateEnabled turns on the §7 step 0 ORB gate (default ON): the high and
	// low of the FIRST 2-minute candle of the regular session gate every
	// intraday entry — nothing inside, no reversal at the edges, only the
	// escape side after a 1m body close outside. The §8 swing is exempt.
	OrbGateEnabled bool

	// LegBudgetEnabled — G1 (R12, DS-107, CTO 2026-10-03): at most 2 entries
	// per leg (the PHL/PLH + a same-direction ISB); a stop-out inside the leg
	// closes it. Default ON.
	LegBudgetEnabled bool
	// LegResetOn — G1 parity knob: a NEW leg starts only on a break beyond
	// the prior extreme. "close" (default): the previous candle's CLOSE
	// strictly beyond the extreme; "touch": this candle's wick reaching it.
	LegResetOn string
}

// DefaultConfig returns the mentor defaults per PLAN v1 (knob values start from
// DS-108's replay per the dispatch; Enabled stays false).
func DefaultConfig() Config {
	return Config{
		Enabled: false,

		KeyLevelTFMinutes: 60,
		KeyLevelRTHOnly:   true,
		KeyLevelPrunePts:  20,
		EMAPeriod34:       34,
		EMAPeriod9:        9,
		EMATFMinutes:      1,

		EMALocationTFMinutes: 1, // R3: intraday EMA 34 lives on the 1m chart

		TouchBandPts: 0, // §3: wait for the LITERAL touch [D3.3 p1 @ 00:13]; §8's "KHÔNG ĐƯỢC GẦN ĐỤNG" is the same strictness

		ISBBufferPts:   1.5,
		ISBStopMinPts:  5,
		ISBTwentiesPts: 20,

		ISBReverseEMA9Enabled: true,

		WickMicroscalpEnabled: false, // D4.3 advanced wick read — OFF by default

		PHLMinCandlesFromExtreme: 3,
		PHLTargetShyPts:          5,
		PHLEntryBufferPts:        1.0,

		StopCeilingPts:         25,
		RoomMultiple:           2,
		NearBoxRoomMultiple:    2, // Day-3 row 24: near-box room = 2 × risk; between two boxes exempt (row 25)
		LossDeparturePts:       0, // B22: structural departure; the numeric fallback is OFF
		LocTriggerFilter:       true,
		TriggerSchool:          1,  // B20: school 1 — level/box entries without 5m agreement
		PingPongMinGapPts:      50, // B21: gap > 50 AND candles <= 20 over the last 30
		PingPongCandleMaxPts:   20, // B21: "nến tầm mười mấy điểm" (D4.2 p2 @05:17–06:37)
		PingPongCandleLookback: 30, // B21 [C: ours]: keeps the on-camera rejection @06:21
		LevelMaxVisits:         3,  // B23: first 3 visits/day trade, the 4th refuses
		EmaMaxCross30m:         2,  // E4 (item 16): default ON, most conservative — mentor question open (no count in the course)
		RangeGapPts:            0,

		DayGateSpentPts:     300,
		DayGateTargetCapPts: 15,

		Swing: DefaultSwingCfg(),

		Box: DefaultBoxCfg(),

		OrbGateEnabled: true,

		LegBudgetEnabled: true,
		LegResetOn:       "close",
	}
}

// barsTF aggregates 1m bars into the given timeframe on CLOCK-ALIGNED buckets
// to the CME session open 17:00 CT (17–21, 21–01, 01–05, 05–09, 09–13, 13–16)
// the way NT8 draws them. Buckets never re-anchor when the window slides or a
// gap appears.
func barsTF(bars []market.Kline, tfMin int) []market.Kline {
	if tfMin <= 1 || len(bars) == 0 {
		return bars
	}
	ms := int64(tfMin) * 60_000
	out := make([]market.Kline, 0, len(bars)/tfMin)
	var cur *market.Kline
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, b := range bars {
		bucket := bucketOpen(b.OpenTime, tfMin)
		if cur == nil || bucket != cur.OpenTime {
			flush()
			c := b
			c.OpenTime = bucket
			c.CloseTime = bucket + ms - 1
			cur = &c
			continue
		}
		if b.High > cur.High {
			cur.High = b.High
		}
		if b.Low < cur.Low {
			cur.Low = b.Low
		}
		cur.Close = b.Close
		cur.Volume += b.Volume
	}
	flush()
	return out
}

// bucketOpen floors an open time to its TF bucket. 1m/5m/15m/1h buckets are
// whole TF multiples of real-UTC epoch ms (whole-hour UTC offsets — DST
// safe). The 4h bucket is anchored at the CME session open 17:00 CT and is
// DST-aware via America/Chicago (EPOCH RULING 2026-10-03: bars carry real
// UTC ms; the CT read never uses raw division on the epoch).
func bucketOpen(openMs int64, tfMin int) int64 {
	if tfMin == 240 {
		return fourHBucketStart(openMs, ctime())
	}
	return (openMs / (int64(tfMin) * 60_000)) * (int64(tfMin) * 60_000)
}

// ISB size flags carried on Intent.Flag (written rules 2 and 3, D4.1 p1 @08:05 /
// @09:40). isbFlags produces them and the trader's size decision reads them, so
// both sides use these constants.
const (
	FlagISBAtOldExtreme = "isb_at_old_extreme"
	FlagISBInRange      = "isb_in_range"
)

// HasFlag reports whether the "|"-joined Intent.Flag contains the exact flag.
func HasFlag(flag, want string) bool {
	for _, f := range strings.Split(flag, "|") {
		if strings.TrimSpace(f) == want {
			return true
		}
	}
	return false
}
