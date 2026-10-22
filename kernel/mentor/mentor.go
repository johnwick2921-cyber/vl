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
	// KindOldExtreme is an old high/low: ONE nearest UNBROKEN old high and ONE
	// nearest UNBROKEN old low (r22 P1, course §6 "nearest old high" / §2.1
	// rule 2 / §2.2) — picked from the bot's swing pivots
	// (kernel/levels_swing.go SwingPointLevels) by swingOldExtremeLevels.
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
	// KindTrendlineTarget is a valid trendline's price as a TARGET-LADDER
	// level (CE-11/B12, release #25, knob trendline_is_target, default ON):
	// "Target là về những level kế tiếp… về trendline, anything… những cái mà
	// nó ngán đường anh chị trên đường đi" [D3.3 p1 @05:18–05:35]. It is never
	// touched and never a location (the trendline LOCATION stays KindTrendline);
	// like a key level it is a real obstacle for the room rule.
	KindTrendlineTarget LevelKind = "trendline_target"
)

// Level is one mentor level line. Lo/Hi are equal (a line); AtTime is the bar
// open time that produced it (recency and the ≥3-candle PHL/PLH distance use it).
type Level struct {
	Key    string // stable id "<kind>:<price>@<session>" — state joins on this
	Kind   LevelKind
	Price  float64
	AtTime int64

	// Role is an old extreme's EXPLICIT role — OldHigh or OldLow — taken from
	// the swing pivot that drew it (r22 P1-4). "" on every other kind. The
	// PHL/PLH target pick, the target ladder and the leg extreme read it; the
	// role is never re-inferred from the tape (a later bar revisiting the
	// price used to relabel an old HIGH as a "low").
	Role string `json:"role,omitempty"`

	// MergedFrom lists prices a DISPLAY line absorbed. Since r22 the chart
	// draws the trading old extremes one-for-one (DisplayLevels), so nothing
	// fills it; it stays on the panel wire as an always-absent field.
	MergedFrom []float64 `json:"merged_from,omitempty"`
}

// Old-extreme roles (Level.Role).
const (
	OldHigh = "high"
	OldLow  = "low"
)

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

	// ExecTFMin is the frame the entry was READ on: 2 when the X5-10 exec-2m
	// schedule (Exec2mAfter30m) read the setup on closed 2m candles — the ISB
	// pair AND PHL/PLH (F2: "Mình trade khung 2 phút … potential high low"
	// [X8 @15:52–15:57]); 0 = the 1m (every other setup, and every named setup
	// while the knob is OFF). The trader trails the runner on closed candles of
	// THIS frame — "nếu như mình trade khung nào thì mình gồng theo khung đó,
	// theo cây nến của khung đó" [X8 @18:02–18:14].
	ExecTFMin int

	// RefBarMs (N10) is the reference candle's CLOSE time (ms) — the bar the
	// entry geometry was measured from. The injector refuses any entry whose
	// reference candle is not the newest closed bar (a reload replay of stale
	// box/ISB/PHL returns must never become live orders). 0 = not stamped
	// (swing — gated by its own 5m watermark instead).
	RefBarMs int64

	// EntryCandleHigh / EntryCandleLow (release #31 rule 5) are the entry
	// candle's high and low, stamped ONLY on ISB intents (the candle the ORB
	// edge rule measures the fraction outside the range from). 0 on every
	// other intent — a pair with High <= Low is "not stamped" and the rule
	// does not apply.
	EntryCandleHigh float64
	EntryCandleLow  float64

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
	// the trader's size table needs it for the big tier (size stays 4 with
	// confluence + 4h AND 1h agree + room ≥ 2× + target ≥ 30 pts — owner order
	// 2026-10-16; the course default 5 MNQ, 1.1 p2 L1103, is superseded). A
	// silent 1h (case 2: follow the 4h) is NOT agreement.
	HTFAgree bool
	// RunnerTarget is the mode-A runner target BEYOND the old high (D2-49 /
	// D2.4 p1 @01:56: "resonance breaks the old high 70–80%"). The PHL/PLH
	// emit site stamps the next level beyond the old extreme (nextLevelBeyond);
	// 0 = none — in mode A the runner's native TP is removed and the exit is
	// the BE stop or EOD flat. The trader's resonance arming consumes it.
	RunnerTarget float64
	// RunnerPartialTP (release #31) is the runner's FIRST partial line — the
	// nearest target line (key level / EMA 34/9 / old high-low / HTF trigger)
	// strictly between the leg-1 exit (1:1) and the trade target, in the trade
	// direction. The runner of ≥ 2 contracts sells half (rounded down) there;
	// the rest trails as today. 0 = no line in between → no leg-3 split. The
	// Tick emit sites stamp it centrally (runnerPartialLine).
	RunnerPartialTP float64
}

// Leg1RiskMultiple is the leg-1 take-profit distance in units of R — the ONE
// definition the exit drive (trader/mentor_mode.go mentorLeg1TPForC) and the
// room check (eval.go roomRefusal) BOTH read. Normal = 1 (the 1:1 first
// partial, "bán bớt ở 1:1" [D2.2 p3 @12:13; D1.2 p1 @07:41–08:45]); confluence
// (mode C) = 2 (hold to 1:2 [D3.4 p3 @07:52–08:07]). The room rule reads this,
// so a future leg-1 change can never drift from the room check.
func Leg1RiskMultiple(confluence bool) float64 {
	if confluence {
		return 2
	}
	return 1
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
	// — the 1m [D5.4 @08:12–08:40] — so the default is 1. The
	// 4h EMA 34 is ONLY for the §8 swing, which carries its own knobs
	// (SwingCfg) and never reads this [D5.2 p1 @ 05:30–05:43]. Other TFs
	// (5/15/60/240) remain allowed for experimentation.
	EMALocationTFMinutes int

	// Touch / close-side (PLAN v1 §2).
	TouchBandPts float64 // literal-touch band around a level; default 0 = literal touch only (L2 CTO 12:19:20Z); key levels never widen beyond 0
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
	// D4.2 p2 @05:13–06:37 — "ping-pong needs a big range: ≥100 or above 50
	// pts; candles 'mười mấy điểm' (about 10–19)"): the gap between an FTGL
	// floor and an FTGH ceiling must exceed PingPongMinGapPts AND the largest
	// 1m candle of the last PingPongCandleLookback closed bars must stay at or
	// below PingPongCandleMaxPts ("nến tầm mười mấy điểm" @05:31–05:37; a
	// 100-pt range goes with candles ~20 — cap 20 is the fair reading). PingPongCandleMaxPts > 0
	// also switches on the key-level pair rule (X4 @06:31–08:52: "một cái nến
	// nó bằng cái rank của 2 cái level rồi thì ngồi im"), which since
	// MENTOR-NY item 2 reads the CURRENT closed candle only — the lookback
	// gates the box ping-pong alone.
	PingPongMinGapPts      float64
	PingPongCandleMaxPts   float64
	PingPongCandleLookback int
	// LevelMaxVisits — B23 (10-03 evening, OPEN-NUMBERS Q3, CTO-verified
	// D1.3 p1 @10:32–11:43 "knock knock"): a level trades its first
	// LevelMaxVisits visits of the day; later visits refuse (0 = cap off).
	LevelMaxVisits int
	// EmaMaxCross30m — E4 knob (CTO 12:27:25Z): refuse the EMA34 setup when the
	// close crossed the line this many times over the last 30 closed 1m candles
	// ("xien len xien xuong", D4.2 p1 @ 22:27). Default REMOVED (#45): 0 — the
	// mentor gives no count and no window; at the exact second he says "xiên lên
	// xiên xuống" the frame D4.2 p1 @22:28 shows the indicator already OFF, so
	// the number is ours, not his. The mentor's actual rule — "switched off only
	// while it is not respected" — is E2's loss relock, not a cross count. An
	// explicit 2-4 re-enables the numeric gate. 0 = OFF.
	EmaMaxCross30m int

	// ISB (PLAN v1 §3).
	ISBBufferPts  float64 // order buffer beyond the wick extremes, BOTH sides; default 1.5 [D1.4 p1 @ 22:22–22:30]
	ISBStopMinPts float64 // RETIRED for ISB by RULES-FIX-v3 (no fixed stop size); kept for the other setups
	// The "twenties" skip (ISBTwentiesPts) was REMOVED in course-defaults-30
	// (owner order "move to 30"); W-MENTOR-EXACT (owner order 2026-10-21) reverts
	// that: an ISB stop > 25 pts is skipped ("trên nữa thì thôi"), a stop of
	// 20–25 pts is the reduced tier — the ceiling lives in CourseExits.ISBMaxStopPts
	// (default 25).

	// ISBReverseEMA9Enabled turns on R7, the reverse-ISB-at-EMA9 setup
	// (RULES-FIX-v3, its own knob per the dispatch). Default ON — OWNER RULING
	// 2026-10-04 R-C "do all as mentor": it is the mentor's own reading [D5.4 p1 @01:26–01:30].
	ISBReverseEMA9Enabled bool
	// WickMicroscalpEnabled turns on the D4.3 wick microscalp (advanced —
	// "đừng có tập khúc này đầu tiên" [D4.3 @04:55]): 2+ consecutive CLOSED 5m
	// candles rejecting with wicks the SAME way (lower wicks in an uptrend,
	// upper wicks in a downtrend) put a bounded target at the rejecting
	// candles' far wick. Default OFF (advanced, behind a knob per D4.3-02).
	WickMicroscalpEnabled bool

	// PHL/PLH (PLAN v1 §3).
	PHLMinCandlesFromExtreme int     // entry at least N candles from the old extreme; default 3 [D4.1 p1 @ 09:40 written]
	PHLTargetShyPts          float64 // target this far short of the old extreme; default 5 (Q2 knob; 'near the old extreme, not AT it' [D2.2 p1 @06:11, 07:33] — the 5 is ours, not the mentor's)
	PHLEntryBufferPts        float64 // entry buffer beyond the candle extreme (outward); default 1.0 [D2.2 p1 @ 06:50 drawn: high 29,396.25 → entry 29,397.25]
	// RollingPHL (m216/m225, DS-102) turns on the D2.2 pullback-continuation
	// rolling PHL/PLH stop entry: during a pullback in a trend (no drawn-level
	// touch) the buy/sell stop is re-armed to EACH closed candle's extreme
	// until it fills. Level touches stay first-touch-fixed (touch.go). Default
	// OFF — it is a NEW #45 entry source; the multi-day replay re-baseline
	// (DS-108) flips it ON before the #45 merge [D2.2 p1 @02:44–03:34,
	// 11:05–11:36].
	RollingPHL bool

	// Filters (PLAN v1 §4).
	StopCeilingPts float64 // hard stop ceiling; default 25 [D3.3 p1 @ 02:04]
	RoomMultiple   float64 // room rule: first level >= RoomMultiple x leg-1 distance (leg 1 = 1R -> 2R; confluence leg 1 = 2R -> 4R) [D5.3 p1 @09:16–09:52]; default 2
	RangeGapPts    float64 // mid-range: levels bracketing price within this gap both sides; default 0 = disabled

	// HTFGateNewsOnly — D4.4-11 [D4.4 p1 @13:44–14:06, @24:48]: the 4h/1h
	// direction gate is for NEWS first, not ordinary trading yet
	// ("đánh news NÊN SỬ DỤNG CHO NEWS TRƯỚC ĐI. ĐỪNG SỬ DỤNG CHO [trade]
	// THƯỜNG"). true (default, the mentor's rule): the HTF gate applies inside
	// the 07:20–07:35 CT news window AND the 08:30–09:30 CT open window ("the
	// open is like news" [D4.4 p2 @00:35–00:46]; the open length is
	// [example: D1.2 p1 @23:52–24:59]); ordinary intraday entries outside those
	// windows are not HTF-gated. false (owner decision A 2026-10-21 m134 "fix
	// all", REVERTED by #45): the 4h/1h direction gate applies ALL DAY. Stored
	// strategies carry no htf_gate_news_only key, so the new default applies at
	// the next trader load; an explicit stored false stays all-day, and none
	// exists today.
	// Studio switch: risk_control.mentor_tuning.htf_gate_news_only.
	HTFGateNewsOnly bool

	// NearBoxRoomMultiple — Day-3 row 24 [D3.2 p1 @ 21:53–23:08]: a setup
	// whose nearest box edge IN the trade direction is closer than this × its
	// own risk is refused ("sát box"); 0 disables. Default 2 — the same 2R
	// room the target rule demands. Between two boxes (row 25) is exempt.
	NearBoxRoomMultiple float64

	// §7 day gate knobs (fold item 4, DS-106): the method defaults until the
	// routed Config integration lands.
	DayGateSpentPts     float64 // run >= this before the open = spent; default 300 [D5.1 p1 @16:05-16:11]
	DayGateTargetCapPts float64 // spent-day target cap; default 15 ("15 điểm bán, 10 điểm bán")

	// DayOffRecheck (course-defaults-30, owner "fix all", supersedes the
	// 2026-10-09 owner ruling): the course shuts the WHOLE day on a DayOff —
	// "Tắt máy nghỉ luôn cho em … Ngày hôm đó không đáng để trade" [D5.1 p1
	// @19:22–19:40], "tắt máy nghỉ 1 ngày cho lành" [D5.1 p2 @00:01]. Default
	// false = the whole-day latch (no intraday re-read clears it). true = the
	// pre-ruling behaviour (re-read 4h/1h on each closed 1h bar; the moment
	// they agree the DayOff clears one-way to DaySpent).
	DayOffRecheck bool

	// PrintWindows (item 18 part 1) lists the day's red-folder 07:30 print
	// windows (FromMs inclusive, ToMs exclusive). The evaluator skips HTF
	// breaks from 1m bars whose open falls inside any listed window — the
	// 07:30 print candle never moves the 1h/4h trigger lines [D4.4 p1 @18:13,
	// @22:15]. The trader plumbs it from the calendar before each Tick; empty
	// = no print today, and the HTF feed is byte-identical.
	PrintWindows []PrintWindow

	// §8 SWING4H knobs (DS-106): the method defaults. The 5m-zone gate
	// lives in SwingCfg.Respects5mZone (default false, [C]) and is wired
	// at the runSwing call site (CTO swing ruling 2026-10-03).
	Swing SwingCfg

	// §4.1 FTGH/FTGL boxes (DS-106, BOX RULING part 2): 1m regular by
	// default. The evaluator builds the boxes per tick and wires their
	// edges into the location gate and InsideAnyBox into the bans.
	Box BoxCfg

	// Exec2mAfter30m — X5-10 knob (default ON since release #31): the exec-frame
	// timeframe schedule (owner order 2026-10-13 ~23:00 CT "fix all"; sources
	// X5 @00:07–01:17, X11 @17:06–19:35 — ~/mm-course/extra/notes/05-EXTRA.md
	// :305-311; re-read 2026-10-16: extra "Routine trước khi trade" part1
	// @00:41–01:14 "Trong 30 phút đầu giờ em vẫn sẽ trade khung 1 phút… sau
	// 30 phút em sẽ chuyển qua khung 2 phút", Go over @17:27 "khung 2 phút ít
	// bị quét râu"): with the knob ON the NAMED setups — the ISB pair AND
	// PHL/PLH (F2, W-MENTOR-TRANSCRIPT-TRUTH; "Mình trade khung 2 phút …
	// potential high low" [X8 @15:52–15:57]) — read the 1m ONLY during the RTH
	// open +30 minutes (08:30 ≤ t < 09:00 CT), and the 2m at every other
	// minute — overnight and the Europe open (3am Texas) included. "Evening
	// slow → 5m" is an OPEN COURSE QUESTION (no number for "slow"); left
	// unimplemented. The MNQ strategy sets no mentor_tuning, so this default
	// is the live behaviour; an explicit OFF restores the 1m read all day.
	Exec2mAfter30m bool

	// OrbISBFractionOutside — rule 5 (release #31, default 0.80): an ISB at the
	// ORB edge trades only when ≥ this fraction of the ISB candle's range
	// (high − low) lies OUTSIDE the ORB on the trade side — "80% 90% là cái
	// nến nó nằm ở ngoài… anh chị có quyền take risk… điểm entry… nằm gần cái
	// resistance quá thì thôi bỏ đi" [extra "Routine trước khi trade" part1
	// @07:46–09:04]. The measure is the fraction of the candle, NOT a points
	// distance (that is the separate CE-7 orb_edge_veto_pts). ≤ 0 = the rule
	// is off.
	OrbISBFractionOutside float64

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

	// ── COURSE-ENTRY KNOBS (release #25, fix/mentor-course-entries) ────────
	// Each is its own strategy knob (risk_control.mentor_tuning.<key>); the
	// default follows the COMMON-25 knob rule: (a) the course says it and no
	// owner ruling / open question goes against it → ON; (b) otherwise OFF.

	// FifteenMinIBRule — CE-1 (a): inside a standing 15m inside-bar box the
	// 1m ISB needs a SAME-direction 5m ISB — "Nếu như khung 5 phút xuất hiện
	// Insaba cho lệnh short thì em trade. Còn không em không trade" [D4.2 p1
	// @17:09–17:13; @14:24–14:34]. "require_5m_isb" (default) | "no_trade_until_escape"
	// (option 1, @01:06–01:14) | "off" (the pre-#25 read: the 15m box gate only).
	FifteenMinIBRule string
	// TFConflictEnd — CE-2 (b, OQ-2): "Đừng trade. Đợi nó thoát cả luôn"
	// [D4.2 p1 @07:54–08:02]. "first_box" = the 15m/5m conflict ends
	// when either box escapes; "both_boxes" (default, course-defaults-30) =
	// it holds until the 5m AND the 15m box have both escaped.
	TFConflictEnd string
	// NewsWaitBothLines — CE-3 (b, OQ-6): after the print, an entry needs the
	// close beyond the 4h trigger line on its side — "Đợi canh nó nằm trên hai
	// cái đường line này… Hoặc… nằm ở trên cái đường line 4 giờ" [D4.4 p1
	// @21:55–22:12] — from the print window until the 08:30 CT open (the open
	// has its own rule, D4.4 p2 @03:51). Default ON (course-defaults-30).
	NewsWaitBothLines bool
	// NewsBarsExcluded — CE-4 (a, release #29 rework): "Còn cái khúc mà news
	// ra rồi thì không tính" [D4.4 p1 @18:18–18:25]. ON FREEZES the pre-print
	// 1h/4h trigger read from the print window start until the 08:30 CT
	// day-gate read — the bars still build their whole 1h/4h candles, so
	// nothing is ever deleted. OFF keeps the item-18 print-window drop
	// (07:20–07:35). Default ON.
	NewsBarsExcluded bool
	// ISBBoxEscapeMode — CE-5 (a): the 5m/15m/30m ISB rest-box escape. "body"
	// (default) = a 1m candle's OPEN AND CLOSE beyond the edge [D4.1 p1
	// @20:52–21:02 "cái thân nến… nằm bên ngoài… Cái đó là at least"];
	// "close" = the pre-#25 close-only test.
	ISBBoxEscapeMode string
	// EmaLossRelock — CE-6 (a): after one loss at the EMA 34 —
	// "until_zone_exit" (default) = back ON once a CLOSED 1m candle lies
	// completely outside the losing trade's entry-to-stop zone [D4.2 p2
	// @01:33–01:39 "Đợi xíu nữa nó thoát khỏi cái vùng đó đi. Rồi tôi bật EMA
	// 34 lên lại"]; "session" = off until the 17:00 CT roll (pre-#25).
	EmaLossRelock string
	// OrbEdgeVetoPts — CE-7 (b, OQ-3): an entry within this many points of the
	// ORB edge is skipped — "điểm entry… nằm gần cái resistance quá thì thôi
	// bỏ đi" [X5 @08:52–09:09]. No number in the source → 0 = off.
	OrbEdgeVetoPts float64
	// D43TrendSource — CE-8 (b, [B] reading): what "the 5m trend" means for
	// the D4.3 no-box-return-against-the-trend gate. "trigger_line" = the 5m
	// trigger line; "structure" (default, course-defaults-30) = 5m swing
	// structure (HH+HL up, LH+LL down) [D4.3 @12:43–12:54, @14:52–15:01].
	D43TrendSource string
	// LevelEntriesNeedTriggerSide — CE-9 (b, owner ruled school 1): under
	// TriggerSchool 1, ALSO bind the 5m trigger side on every level reject
	// (key level, EMA, trendline, old extreme); school 1 stays at the
	// FTGL/FTGH box only [D3.4 p1 @17:48–18:00 vs p3 @09:57–10:27]. Default OFF.
	LevelEntriesNeedTriggerSide bool
	// TrendlineIsTarget — CE-11/B12 (a): a valid trendline in the path is a
	// target [D3.3 p1 @05:18–05:35 "Target là về những level kế tiếp… về
	// trendline, anything"; @07:57–08:04]. Default ON; false = the pre-#25
	// "location only, never a target".
	TrendlineIsTarget bool
	// TrendlineBetweenBoxes — CE-11/B13 (b): a trendline reject between an
	// FTGL and an FTGH may trade [D3.2 p2 @10:56–11:20] although the same
	// lesson bans every PHL/PLH between two boxes [@08:34–08:55]. Default ON
	// (course-defaults-30: the course trades it).
	TrendlineBetweenBoxes bool
	// LegResetOnWaveBreak — CE-12 (a, extra where the course is silent): a
	// leg closed by a stop-out also resets on a close beyond the losing
	// trade's wave extreme — "đợi cho nó break được cái đỉnh, hoặc đợi cho nó
	// break được cái đáy của con sóng đó" [X-FOMO @06:10–06:22]. Default ON.
	LegResetOnWaveBreak bool
	// FlagWallTarget — CE-11 (b, ported from PR #416): when ON, a trade taken
	// INSIDE a live flag (both trendlines exist and are unbroken, the entry
	// strictly between them) gets the OPPOSITE flag wall as a target candidate
	// — the target is the NEAREST of {the existing target, the wall}, and the
	// wall can only bring it CLOSER [X9 @ 05:47–06:12: "target của anh chị là
	// chỉ là trên cái đỉnh của cái flag… vẫn scalping được, không sai"]. A wall
	// that leaves < 1:1 refuses the entry (flag_wall_room). Default ON
	// (course-defaults-30).
	FlagWallTarget bool
	// BoxNewZoneAfterEscape — B52 (release #29): when the standing FTGH/FTGL
	// box escapes (a body close outside its outer edge), the bot may latch ONE
	// new box on that side, built from failures that form AFTER the escape and
	// OUTSIDE the old range — "khi nào nó rớt xuống đây, nó bật ngược lên, nó
	// tạo cái failure to go lower dưới này nè… thì anh chị mới có được cái
	// range giữa cái này và cái này" [D3.4 p3 recovered2 @00:00–00:21];
	// "Đừng vẽ vùng thứ 3… Rối lắm" [D3.2 p2 @09:47–09:54] — never a zone
	// inside the standing range. Default ON (the course); false = today's
	// one-pair-per-session behaviour exactly.
	BoxNewZoneAfterEscape bool

	// RealFillOnly — FU-1 (CTO 2026-10-05): when true, the G1 leg budget and
	// the G2 loss box are fed ONLY from real broker fills (Limits.RecordFill,
	// delivered by the trader's fill callback), never from the simulated
	// candle-touch fill — a never-placed order can then never phantom-fill and
	// spend the budget. The trader sets this true for LIVE; false keeps the
	// simulated fill for the kernel's parity tests and any backtest. Default
	// false (zero value).
	RealFillOnly bool

	// CourseExits — the release #25 course-exits knobs (course_exits.go).
	CourseExits CourseExitsCfg
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

		TouchBandPts: 0, // §3: wait for the LITERAL touch [D3.3 p1 @ 00:13]; D5.2 p2 @18:36–18:45 "gần gần gần đụng … Còn như cái này thì bắt buộc là phải đụng nha"

		ISBBufferPts:  1.5,
		ISBStopMinPts: 5,

		ISBReverseEMA9Enabled: true,

		WickMicroscalpEnabled: false, // D4.3 advanced wick read — OFF by default

		PHLMinCandlesFromExtreme: 3,
		PHLTargetShyPts:          5,
		PHLEntryBufferPts:        1.0,
		RollingPHL:               false, // D2.2 pullback-continuation rolling stop entry — OFF until the #45 replay re-baseline flips it ON [@02:44–03:34, 11:05–11:36]

		StopCeilingPts:         25,
		RoomMultiple:           2,
		HTFGateNewsOnly:        true, // D4.4-11: gate NEWS + OPEN windows (the mentor's rule; #45 reverts owner decision A 2026-10-21 m134 all-day)
		NearBoxRoomMultiple:    2,    // Day-3 row 24: near-box room = 2 × risk; between two boxes exempt (row 25)
		LossDeparturePts:       0,    // B22: structural departure; the numeric fallback is OFF
		LocTriggerFilter:       true,
		TriggerSchool:          1,  // B20: school 1 — level/box entries without 5m agreement
		PingPongMinGapPts:      50, // B21: gap > 50 AND candles <= 20 over the last 30
		PingPongCandleMaxPts:   20, // B21: "nến tầm mười mấy điểm" (D4.2 p2 @05:31–05:37; the 100-pt range = candles ~20)
		PingPongCandleLookback: 30, // B21 [C: ours — the 30-bar window; the 20-pt cap itself matches D4.2 p2 @05:31–05:37]: keeps the on-camera rejection @06:21 (box ping-pong only; the key-level pair reads the current candle, MENTOR-NY item 2)
		LevelMaxVisits:         3,  // B23: first 3 visits/day trade, the 4th refuses
		EmaMaxCross30m:         0,  // E4 (item 16): REMOVED default (#45 — the mentor gives no count; "xien len xien xuong" is qualitative). The EMA-off-while-not-respected rule is E2 (loss → blocked until departure). An explicit 2-4 re-enables the numeric gate.
		RangeGapPts:            0,

		DayGateSpentPts:     300,
		DayGateTargetCapPts: 15,
		DayOffRecheck:       false, // course-defaults-30: the course shuts the whole day [D5.1 p1 @19:22–19:40]

		Swing: DefaultSwingCfg(),

		Box: DefaultBoxCfg(),

		OrbGateEnabled: true,

		// Release #31 rule 4 (X5-10): the ISB-exec schedule defaults ON — 1m
		// for the first 30 min of RTH, then the 2m.
		Exec2mAfter30m: true,
		// Release #31 rule 5: the ORB-edge ISB candle must be ≥ 80% outside.
		OrbISBFractionOutside: 0.80,

		LegBudgetEnabled: true,
		LegResetOn:       "close",

		CourseExits: DefaultCourseExitsCfg(),
		// Release #25 course-entry knobs (COMMON-25 knob rule).
		FifteenMinIBRule:      FifteenMinRequire5mISB, // CE-1 (a)
		TFConflictEnd:         TFConflictBothBoxes,    // CE-2 — course-defaults-30: "đợi nó thoát cả luôn" [D4.2 p1 @07:54–08:02]
		NewsWaitBothLines:     true,                   // CE-3 — course-defaults-30 [D4.4 p1 @21:55–22:12]
		NewsBarsExcluded:      true,                   // CE-4 (a, release #29 rework: freeze the pre-print read, candles whole)
		ISBBoxEscapeMode:      ISBBoxEscapeBody,       // CE-5 (a)
		EmaLossRelock:         EmaRelockUntilZoneExit, // CE-6 (a)
		D43TrendSource:        D43TrendStructure,      // CE-8 — course-defaults-30: 5m swing structure [D4.3 @12:43–12:54]
		TrendlineIsTarget:     true,                   // CE-11/B12 (a)
		TrendlineBetweenBoxes: true,                   // CE-11/B13 — course-defaults-30 [D3.2 p2 @10:56–11:04]
		FlagWallTarget:        true,                   // CE-11 — course-defaults-30 [X9 @05:59–06:12]
		LegResetOnWaveBreak:   true,                   // CE-12 (a)
		BoxNewZoneAfterEscape: true,                   // B52 (a) — new zone after a range escape
	}
}

// Release #25 course-entry knob values (the ONE spelling the kernel, the
// trader resolver, the boot line and the guide share).
const (
	FifteenMinOff          = "off"
	FifteenMinRequire5mISB = "require_5m_isb"
	FifteenMinUntilEscape  = "no_trade_until_escape"
	TFConflictFirstBox     = "first_box"
	TFConflictBothBoxes    = "both_boxes"
	ISBBoxEscapeClose      = "close"
	ISBBoxEscapeBody       = "body"
	EmaRelockSession       = "session"
	EmaRelockUntilZoneExit = "until_zone_exit"
	D43TrendTriggerLine    = "trigger_line"
	D43TrendStructure      = "structure"
)

// barsTF aggregates 1m bars into the given timeframe. Every TF except 240 is
// EPOCH-aligned (UTC) — identical to a 17:00 CT anchor for every TF used here
// (1/3/5/15/30/60 all divide 60 minutes, and 17:00 CT sits on a whole UTC
// hour). Only the 240 bucket is 17:00 CT session-anchored (fourHBucketStart:
// 17–21, 21–01, 01–05, 05–09, 09–13, 13–16), the way NT8 draws the 4h. Buckets
// never re-anchor when the window slides or a gap appears.
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

// BarsTFHoles reports the OPEN TIMES of aggregate buckets built from a 1m series
// that have an INTERIOR hole (a missing minute). It is a DIAGNOSTIC only — no
// production caller: the m148 coverage check keeps partial buckets out of the
// ring at the rehydrate call site, so a holey bucket never reaches the mentor
// reads as a full bar; BarsTFHoles exists to name a hole when one is seen
// elsewhere. The last (possibly forming) bucket is never reported; a short
// leading bucket (the series starts mid-bucket) is not a hole. PURE — it does
// not mutate the bars.
func BarsTFHoles(bars []market.Kline, tfMin int) map[int64]bool {
	if tfMin <= 1 || len(bars) < 2 {
		return nil
	}
	var holes map[int64]bool
	var curBucket int64
	var prevMin int64
	var gapSeen bool
	for i, b := range bars {
		bucket := bucketOpen(b.OpenTime, tfMin)
		if i == 0 {
			curBucket, prevMin, gapSeen = bucket, b.OpenTime, false
			continue
		}
		if bucket != curBucket {
			if gapSeen {
				if holes == nil {
					holes = map[int64]bool{}
				}
				holes[curBucket] = true
			}
			curBucket, prevMin, gapSeen = bucket, b.OpenTime, false
			continue
		}
		if b.OpenTime-prevMin > 60_000 {
			gapSeen = true
		}
		prevMin = b.OpenTime
	}
	return holes
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
	// FlagPullbackWarn is the X11 pullback-warning size cut: a 5m candle that
	// breaks the prior 5m candle's low in a 5m uptrend warns of a pullback, so
	// the entry sizes down [X11 @13:21–15:02, quote @14:50–15:00].
	FlagPullbackWarn = "pullback_warn"
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
