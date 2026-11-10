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
	// KindHTFTrigger is the 4h trigger line as a TARGET-LADDER level only
	// (D4.4-15; release #52 B5: the 1h line is no longer a target — the course
	// names only the 4h line [D4.4 p2 @05:04–05:44]): it joins the level set so nextLevelBeyond can pick it, but it
	// is never touched, never located and never a PHL/PLH place.
	KindHTFTrigger LevelKind = "htf_trigger"
	// KindWickMicroscalp is the D4.3 wick-microscalp target (D4.3-02/-07): the
	// rejecting 5m candles' far wick, target-only — it joins the level set so
	// nextLevelBeyond can pick it, but it is never touched and never a
	// PHL/PLH location.
	KindWickMicroscalp LevelKind = "wick_microscalp"
	// KindWickZone is the D4.3 wick zone's NEAR edge as an entry LOCATION
	// (m465 P5, knob wick_microscalp): the 5m wick-run box drawn under the
	// lower wicks (long) or over the upper wicks (short), extended right until
	// a 1m close beyond its far edge. A 1m PHL/PLH whose reference candle
	// touches it arms through the ordinary touch loop — "Vẫn được: potential
	// high low khung giờ nhỏ hơn, hay là insert bar cho lệnh long khung giờ nhỏ
	// hơn" [D4.3 @06:13–06:29]. Keys "wick_zone:<side>:<5m open ms>:<top|bottom>".
	KindWickZone LevelKind = "wick_zone"
	// KindTrendlineTarget is a valid trendline's price as a TARGET-LADDER
	// level (CE-11/B12, release #25, knob trendline_is_target, default ON):
	// "Target là về những level kế tiếp… về trendline, anything… những cái mà
	// nó ngán đường anh chị trên đường đi" [D3.3 p1 @05:18–05:35]. It is never
	// touched and never a location (the trendline LOCATION stays KindTrendline);
	// like a key level it is a real obstacle for the room rule.
	KindTrendlineTarget LevelKind = "trendline_target"
	// KindGapEdge is the FAR edge of an unfilled session-open gap (m466 P2,
	// G11 D3.1-R20): the prior session's last close. "Cái cạnh dưới của cái
	// gap, lúc nào nó cũng sẽ là resistance… Level là resistance" [D3.1 p1
	// @09:41–09:51]. A LOCATION, a target and an obstacle like a key level; it
	// stands until a 1m candle trades through it (the gap filled — "có thể
	// đợi vài ngày" R16). No gap-fill entry exists ("em không đánh theo gap",
	// R23). Keys "gap_edge:<price>@<session open ms>".
	KindGapEdge LevelKind = "gap_edge"
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

	// FormedMs / TF (m421b) are set on FTGH/FTGL box-edge locations only: the
	// box's formation-bar open (Box.FormedAtMs) and the frame it was drawn on
	// (Box.TF; "" = the base frame). Display-only — the chart draws the box
	// as a rectangle from FormedMs. A box that escapes is dropped from the
	// set, so no end time is ever known: the panel omits ended_ms.
	FormedMs int64  `json:"formed_ms,omitempty"`
	TF       string `json:"tf,omitempty"`
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
	// a BOX entry whose 5m trigger agrees AT THE REAL ENTRY PRICE (not the box
	// edge, C52 W1 A1; no key-level condition, B3), or an ISB/PHL/PLH entry with
	// the 15m ISB, 5m ISB and entry side all aligned (MTFConfluence, D4.2 p1
	// @12:16-12:47; no trigger conjunct, C52 W1 B9) — the exit-C branch reads it.
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
	// KeyLevelDeleteOn1HClose — levels audit LL-1. "off" (default, October):
	// a key level SURVIVES a 1H RTH candle whose body closes through it — the
	// mentor draws a line at every colour change, "vẽ bao nhiêu tùy thích… Key
	// Level nó là quá khứ" [OctD5.2-R13..R16 @04:10–07:12, R15 @04:44], and
	// trades the 11-03 19:08 retest of 27,997.75, a level a 1H body crossed on
	// 07-29 [OctD5.2-R19/R20 @07:20–07:42, WE3 @09:12–10:05]. "on": the deck /
	// September rule, "Khi nến 1H đóng cửa XUYÊN level đó, thì lúc đó sẽ xóa
	// level đó đi" (slide 31 step 4) — levelDeletedBy1HBody. The 20-pt
	// keep-the-newer prune applies either way. Mentor question (LL-1).
	// Studio JSON: risk_control.mentor_tuning.key_level_delete_on_1h_close.
	KeyLevelDeleteOn1HClose string
	// KeyLevelHistorySource — levels audit LL-4. "own" (default): every 1H RTH
	// key-level candle the CURRENT contract's native bars fully cover (1m, else
	// 15m, else 30m) is built from its own prints — the mentor draws on
	// MNQZ2026's own chart back to late July [OctD5.2-R16 @06:18]; the rest
	// keeps the back-adjusted older contract. "stitched": every pre-roll candle
	// from the older contract + the one measured roll gap (pre-LL-4).
	// Applied by the trader's seed read (mentorFullKeyLevelHistory).
	// Studio JSON: risk_control.mentor_tuning.key_level_history_source.
	KeyLevelHistorySource string

	EMAPeriod34  int // default 34
	EMAPeriod9   int // default 9
	EMATFMinutes int // TF the EMA lines are computed on; default 1

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
	// 20–25 pts is the reduced tier — the ceiling lives in CourseExits.ISBMaxStopPts.
	// m466 G23 (D4.1-R12 "hai mươi mấy điểm… đừng vô" [D4.1 p1 @05:41–06:08]):
	// default 20 — the ISB 20–25 reduced band is unreachable at default.

	// ISBReverseEMA9Enabled turns on R7, the reverse-ISB-at-EMA9 setup
	// (RULES-FIX-v3, its own knob per the dispatch). Default ON — OWNER RULING
	// 2026-10-04 R-C "do all as mentor": it is the mentor's own reading [D5.4 p1 @01:26–01:30].
	ISBReverseEMA9Enabled bool
	// WickMicroscalpEnabled turns on the D4.3 wick microscalp (knob
	// wick_microscalp; advanced — "đừng có tập khúc này đầu liền" [D4.3
	// @04:55–05:17]). m465 P5: the WHOLE setup — a run of ≥2 same-way 5m wicks
	// WITH the 5m trend (wick_zone.go) draws a zone box, extended right; a 1m
	// ISB/PHL at the zone arms through the ordinary paths; the target is the
	// nearby 5m wicks (wick_microscalp.go). Default OFF.
	WickMicroscalpEnabled bool
	// WickSafePlay (m465 P5, sub-knob wick_safe_play, default OFF) is the
	// D4.3-R20 safe-play confirmation: a setup at a wick zone counts only once
	// its reference candle CLOSED beyond the zone's near edge (under the box
	// for a short, over it for a long) — "mọi người đợi nó nằm ở dưới cái
	// box. Mọi người vẫn có thể trade bình thường, là thêm một sự confirm"
	// [D4.3 @14:07–14:27]. Read only while WickMicroscalpEnabled is ON.
	WickSafePlay bool

	// PHL/PLH (PLAN v1 §3).
	PHLMinCandlesFromExtreme int     // entry at least N candles from the old extreme; default 3 [D4.1 p1 @ 09:40 written]
	PHLTargetShyPts          float64 // target this far short of the old extreme; default 5 (Q2 knob; 'near the old extreme, not AT it' [D2.2 p1 @06:11, 07:33] — the 5 is ours, not the mentor's)
	// PHLEntryBufferPts — entry buffer beyond the reference candle's extreme
	// (outward). Default 0.50 (release #59 fix round, mentor-exact): the
	// committed entry RAY is 29,396.25 over the signal high 29,395.75 (+2
	// ticks) [D2.2-R10 p1 @04:14–05:43, screen]. The old 1.00 read the HIGH as
	// 29,396.25 and the long-tool's refitted 29,397.25 as the entry [D2.2 p1
	// @06:45]; the real NT8 tape of the replay day (MNQ 09-26, Fri 11 Sep 2026
	// 10:14 CT: O 29,390.50 H 29,395.75 L 29,386.50 C 29,394.00) confirms the
	// high (testdata/m464_mnq_1m_20260911.csv).
	PHLEntryBufferPts float64
	// PHLStopBufferPts — the stop sits this far BEYOND the broken candle's other
	// extreme (outward). Default 0.50: the stop RAY is 29,386.00 under the signal
	// low 29,386.50 (−2 ticks) [D2.2-R10 p1 @04:14–05:43, screen]. Not applied
	// when the D3.3 EMA 34 nuance puts the stop AT the EMA ("no extra room").
	// Entry and stop move together, so the RISK is unchanged from the old
	// 1.00 / 0 pair (range + 1.00).
	PHLStopBufferPts float64
	// PHLOldExtreme1mK — PHL-1 (phl audit 2026-11-10): the rolling PHL/PLH's
	// old high / old low is the 1m swing the pullback started from (a bar
	// beating its k left neighbours, unbroken, confirmed by k bars to its
	// right) — "gần đỉnh cũ" [OctD2.3-R30 @ 26:50, R42 @ 41:23, R53 @ 54:00];
	// every worked example's old extreme is that 1m swing (WE6 10-27 09:36,
	// WE7 10-30 13:21, WE9 10-29 20:53, WE10 10-29 19:48). The course states
	// no width: the default 4 is the only k that reproduces all four
	// (phl_tape_extreme.go). <= 0 = the pre-PHL-1 source (the 5m/15m pivots
	// of the level set). Knob risk_control.mentor_tuning.phl_old_extreme_1m_k.
	PHLOldExtreme1mK int
	// RollingPHL (m216/m225, DS-102) turns on the D2.2 pullback-continuation
	// rolling PHL/PLH stop entry: during a pullback in a trend (no drawn-level
	// touch) the buy/sell stop is re-armed to EACH closed candle's extreme
	// until it fills. Level touches stay first-touch-fixed (touch.go). Default
	// ON since c52 W6 (owner order 2026-10-26 "align everything to the course"),
	// behind the pullback-colour precondition and the 5m trigger-side gate in
	// rolling_phl.go [D2.2 p1 @01:52–03:34, 10:47–11:36; X8 @06:25, 08:37].
	RollingPHL bool

	// Filters (PLAN v1 §4).
	StopCeilingPts float64 // hard stop ceiling; default 25 [D3.3 p1 @ 02:04]
	RoomMultiple   float64 // room rule: first level >= RoomMultiple x leg-1 distance (leg 1 = 1R -> 2R; confluence leg 1 = 2R -> 4R) [D5.3 p1 @09:16–09:52]; default 2
	RangeGapPts    float64 // mid-range: levels bracketing price within this gap both sides; default 0 = disabled

	// HTFGateNewsOnly — m462 (release #59): false (DEFAULT) = the 4h/1h
	// direction gate applies ALL SESSION to every intraday setup. The final
	// review D5.1 overrides the D4.4 "news first" caveat: D5.1-R9 [D5.1 p1
	// @08:46–09:13, @11:27] "Lúc nào khi anh chị open ra, không cần phải là
	// news… nếu là news thì 1h trước news và 4h trước news; còn nếu là open
	// cũng vậy"; D5.1-R11 [D5.1 p1 @08:29–09:52, @11:27–11:50] "Trigger 4h,
	// trigger 1h nằm ngược nhau… cứng ngắt nằm giữa cái range luôn, mình không
	// làm được gì hết trơn" (15 Sep: the conflict held until 11–12 h); the
	// matrix D5.1-R20 [@19:11–20:10]. The bot traded through conflicts under
	// the old default (trades 669, 677, 698, 705, 716).
	// true = the OLD D4.4-11 news-only windows [D4.4 p1 @13:44–14:06, @24:48]:
	// a print day from the print window's start (07:20 CT) to the 08:30 CT
	// open, plus the 08:30–09:30 CT open window (the 60 minutes are OURS, B28).
	// Kept reachable for anyone who STORED true explicitly — stored values are
	// sacred; on 2026-11-01 no strategy stores the key (all read nil → the new
	// all-day default at the next trader load).
	// SWING4H is never gated by the 4h/1h triggers (D5.2-R7: its direction is
	// the 4h EMA34).
	// Studio switch: risk_control.mentor_tuning.htf_gate_news_only.
	HTFGateNewsOnly bool

	// NearBoxRoomMultiple — Day-3 row 24 [D3.2 p1 @ 21:53–23:08]: a setup
	// whose nearest box edge IN the trade direction is closer than this × its
	// own risk is refused ("sát box"); 0 disables. Default 2 — the same 2R
	// room the target rule demands. Between two boxes (row 25) is exempt.
	NearBoxRoomMultiple float64
	// NearBoxRoomPHL — release #52 W11 B11 (knob near_box_room_phl, default ON
	// = unchanged): the near-box refusal also applies to PHL/PLH entries (risk
	// = the reference bar's range). The course lesson shows the "sát box" skip
	// on an ISB only [Ngày 3.2 FTGH FTGL part1 @00:22:44–00:23:10]; the PHL/PLH
	// leg is OUR extension, so this switch lets the owner scope it back to the
	// ISB. The distance itself (NearBoxRoomMultiple, 2 × risk) is ours too —
	// the mentor gave no number.
	NearBoxRoomPHL bool

	// m465 P3 (release #60) structure-fidelity knobs. Each default is the
	// course; the other value is the pre-#60 behaviour, kept for the owner.
	//
	// TrendlineEntryTouch — D2.3-R7: "thà nó về một lần, rồi nó test, rồi nó
	// hold rồi, là lần thứ ba đó, thì lần thứ tư mới dám tự tin mình vô" [D2.3
	// p1 @02:34–02:47, 03:03–03:16]; D3.2-R34 "test lần thứ 3 thì mới valid".
	// m472 A2: the DEFAULT is now "third" — October D3.2 [Oct D3.2 @ 31:00-31:17]
	// "Em chỉ cần 2 touch là được mà" and [@ 41:20-41:27] (two bottoms, price
	// returns to the line, you trade it) supersede the September 4th-touch read.
	// "fourth" (opt-in, the Sept D2.3-R7 behaviour): an intraday line is NOT a location on the candle of
	// its 3rd touch (the test) — refused trendline_third_touch_is_test — and
	// becomes one from the next candle, so the entry comes on a later touch.
	// "third" (default since m472): the line arms on its 3rd-touch candle.
	TrendlineEntryTouch string
	// NearBoxBetweenExempt — D3.2-R29/R30: the mentor refused the short ISB
	// sitting on the FTGL box (E6) although an FTGH box stood above it, and
	// allowed the ISB "giữa 2 box… nó đâu có làm gì đâu" because nothing was
	// near it [D3.2 p1 @22:12–24:41]. false (default): no blanket exemption —
	// the middle case passes on its own room; true: the pre-#60 exemption
	// (one box wholly above AND one wholly below → never near).
	NearBoxBetweenExempt bool
	// PHLFlatLow — D2.2-R64/R65: a PHL needs a low HIGHER than the previous
	// low; a flat low ("ngang phè") is a FAILURE TO GO LOWER, not a PHL [D2.2
	// p3 @07:44–10:15] (mirror: a flat high is an FTGH, not a PLH [@14:03–14:22]).
	// "flat_near" (default): a low within the m428 R2 "same price" distance
	// (flatNearPts) of the prior low is refused phl_flat_low_is_ftgl
	// (plh_flat_high_is_ftgh); "strict": the pre-#60 strict comparison only.
	PHLFlatLow string
	// ISBReverseTouch — D5.4-R7: "nó không cần đụng, nó cứ đụng vòng vòng ngay
	// EMA 9 là được … không quan trọng là nó đụng ngay tại EMA 9 hay là nó lố
	// EMA 9" [D5.4 p1 @03:35–03:46]. "pair" (default): the EMA 9 is touched
	// when it sits inside the mother + ISB candle group's range; "isb_candle":
	// the pre-#60 read (inside the ISB candle's own range only).
	ISBReverseTouch string

	// ISB size-cut definitions — release #52 W11 B27. The course says reduce
	// size for an ISB "ngay đỉnh/đáy cũ" and "in range" and gives NO distance or
	// definition [Ngày 4.1 Go over ngày 3 part1 @00:04:31–00:05:19]. Both
	// numbers are OURS; the knobs make that visible and tunable, defaults
	// unchanged.
	//   ISBOldExtremePts — "at the old high/low" = the ISB candle's range reaches
	//   an old extreme within ± this many points (default 2).
	//   ISBInRangeGapPts — "in range" = price sits between two KEY levels closer
	//   than this (default 0 = inherit PingPongMinGapPts, i.e. 50, borrowed
	//   from X4/D4.2 — so an owner who already tuned ping_pong_min_gap_pts keeps
	//   the same in-range reading).
	ISBOldExtremePts float64
	ISBInRangeGapPts float64

	// ISB stacking counts on the 2m — release #52 W11 B8. The course states the
	// stacking arithmetic in 1m candles (2x1m = one 2m, 3x1m = one 3m, a 4th
	// 1m would be a 4m that does not exist so it jumps to the 5m) [Ngày 4.2
	// Multitime frame 15_ part2 @00:08:53–00:09:03, @00:15:24–00:16:08]; it never
	// states how many 2m candles an arm survives. m406 reuses the 1m count on
	// the arm's own frame, so a 2m arm is held through 3 stacked 2m candles and
	// cancelled on the 4th. ISBStackCancelCandle2m is that candle number on the
	// 2m (default 4 = the 1m course count, OURS); the 1m (and 5m) count stays
	// the course 4.
	ISBStackCancelCandle2m int
	// ISBStackTinyStopPts — the tiny-stop exception [Ngày 5.1 Go over lại tất
	// cả part2 @02:50–03:40, @04:10–04:58: "stop loss của mình ở thời điểm đó
	// rất nhỏ. Có 12 điểm… Trade nó là take risk"]: an arm whose ISB stop
	// (range + 2 × ISBBufferPts) is at or below this many points is NOT
	// cancelled at the 4th candle (it still cancels the moment a body leaves
	// I1). m465 (release #60, D5.1-R26 — the full video read): the worked
	// example pins the number — "stop loss của mình lúc đó rất nhỏ, có 12
	// điểm… cancel làm chi trời… Anh chị có một cái quyền: thấy stop loss
	// nhỏ… cứ việc đặt lệnh, mặc dù sau 3 cây" [D5.1 p2 @02:24–04:41], the
	// on-screen stop 29,285.75 → 29,273.25 = 12.50 pts (50 ticks). Default
	// 12.5 (was 0 = OFF); the owner may still store any (0, 25].
	ISBStackTinyStopPts float64

	// Evening "slow" read — release #52 W11 B10. X11 says trade the 5m only in
	// a night the US market barely moved and a 100–200-pt night is fine on the
	// 2m [extra "Go over lớp học" part1 @17:40–18:26]; owner answer Q6
	// (2026-10-25): slow = candles not running much, NO number fixed. All three
	// numbers are therefore OURS and unchanged by default:
	//   EveningSlowPts       — the night is slow when its high-low is under this (100).
	//   EveningSlowFromHour  — the CT hour the range starts at: 15 = the RTH close
	//                          (default), 17 = the Globex reopen ("buổi tối").
	//   EveningSlowWindowMin — 0 = cumulative since the start hour (default);
	//                          N > 0 = only the last N minutes of bars count.
	EveningSlowPts       float64
	EveningSlowFromHour  int
	EveningSlowWindowMin int

	// §7 day gate knobs (fold item 4, DS-106): the method defaults until the
	// routed Config integration lands.
	DayGateSpentPts     float64 // run >= this before the open = spent; default 265 (m465 D5.1-R18: the measured −265.00 day "nó chạy gần hết cây daily luôn rồi" [D5.1 p1 @17:56–18:14]; the 300–400 rule [@16:05-16:11])
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

	// Exec2mAfter30m — X5-10 knob (default OFF since m472 A1 [Oct D4.2 @ 22:56-23:46]
	// "Trade khung 1 phut, chu khong la khung 2 phut"; was ON since release #31): the exec-frame
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
	// slow → 5m" is implemented by eveningSlow (eval.go; the 100-pt cutoff is
	// ours and the range is measured since the 15:00 CT RTH close — how "slow" is
	// measured is an open mentor question). The MNQ strategy sets no mentor_tuning, so this default
	// is the live behaviour (now OFF = the 1m all day); an explicit ON restores the
	// September 2m-after-09:00 schedule.
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
	// Early15mRead — m465 P1 (release #60, D4.2-R2 / D4.2-R11 / D4.3-R21):
	// read the FORMING 15m candle from its first two closed 5m candles — "3
	// cây 5 phút mà nó cứ xào quần xào quần ngay chỗ đó mà nó không đi đâu
	// hết... ngay lập tức mình biết khung 15 phút đang tạo inside bar" [D4.2
	// p1 @00:08–00:39]; "2, 3 cây 5 phút anh chị đã có được 1 cây 15 phút"
	// [@07:40]; "2 cái nến 5 phút, 3 cái nến 5 phút, nó đang làm cái nến 15
	// phút rồi đó" [D4.3 @12:20–12:32]. ON (default; defensive — it only ever
	// refuses, "né được rất nhiều đoạn ranging" [D4.2 p1 @11:40–11:42]) boxes
	// that forming 15m as a PROVISIONAL ISBBox15m (forming15mISB) when no 15m
	// box stands; every 15m-box gate then refuses under a "_forming" name.
	// Studio: risk_control.mentor_tuning.15m_early_read.
	Early15mRead bool
	// MTFConfluenceSource — m465 P1 (D4.2-R17): where mode C reads "the 15m
	// and the 5m both say so" [D4.2 p1 @14:57–15:37]. "standing_boxes"
	// (default) = the STANDING 15m box + the standing (or same-way escaped) 5m
	// box (MTFConfluenceStanding); "last_pair" = the pre-#60 read of the latest
	// closed 5m and 15m candle pairs (MTFAligned), which lost the confluence
	// one 5m candle after the 5m ISB. Studio:
	// risk_control.mentor_tuning.mtf_confluence_source.
	MTFConfluenceSource string
	// TriggerEscalation — m465 P1 (D3.4-R43 [D3.4 p2 @17:07–17:49]): when the
	// 5m inside bars cross back and forth the 5m trigger is not valid — read
	// the 15m trigger; if the 15m crosses too, the 30m (the 1h/4h already gate
	// through the HTF check). ON (default) = every gating read of the trigger
	// uses governingTrigger; OFF = the 5m line always (pre-#60). Studio:
	// risk_control.mentor_tuning.trigger_escalation.
	TriggerEscalation bool
	// CrossingISBLookback / CrossingISBMin — isbTF-F6: the crossing read that
	// escalates the trigger (crossingISBsCfg): the last CrossingISBLookback
	// CLOSED buckets hold at least CrossingISBMin inside bars of BOTH
	// directions. The course states no count or window ("inside bars cross
	// back and forth", D3.4-R43 [D3.4 p2 @17:07–17:49]); 3 / 2 is today's
	// [B] reading, mentor question Q10. Studio JSON:
	// risk_control.mentor_tuning.crossing_isb_lookback / crossing_isb_min.
	CrossingISBLookback int
	CrossingISBMin      int
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
	// ISBBoxEscapeMode — CE-5: the 5m/15m/30m ISB rest-box escape (and the
	// #60 provisional forming-15m box). "close5m" (default since m473 B3) = a 1m
	// close outside gives trade permission but the box is dropped only when a
	// candle of the box's own timeframe closes outside [Oct D4.3 @ 65:00-66:13,
	// OctD4.3-R67]. September "close" (default since m466 G20/G49,
	// release #61) = a 1m candle CLOSES beyond the edge — the course's stated
	// minimum, "Break là sao? Là một cây nến một phút anh chị trade, nó đóng
	// bên ngoài cái box là at least" [D4.1 p1 @20:36–21:03]; "nó đóng một cái
	// nến một phút ở ngoài" [D3.4 p2 @05:37–06:01] — the same close the PHL/PLH
	// 5m-box ban reads. "body" = the #25–#60 default: OPEN AND CLOSE beyond
	// the edge (a stored isb_box_escape=body still wins; a stored close is an
	// EXPLICIT opt-out of the October default close5m: it deletes the box on the 1m close). FTGH/FTGL boxes keep
	// their own body escape (D3.2-R16, box.go) whatever this says.
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
	// OrbEscapeCompletely — release #52 B19 (knob orb_escape_completely,
	// default OFF): ON = the ORB opens only when a 1m candle lies COMPLETELY
	// outside the 2-minute range, wicks included (low above the high / high
	// below the low); OFF = today's body test (open AND close beyond the
	// edge). The ORB source: "Completely nằm ở ngoài… Còn không nếu mà nó vẫn
	// còn vòng vòng trong ORB 2 phút mình không trade" [extra "Routine trước
	// khi trade" part1 @06:47–07:16]. The wick reading is the X7 ping-pong
	// definition carried to the ORB [X7 @09:21–11:07] — an analogy [B], so the
	// default flip is the owner's call.
	OrbEscapeCompletely bool
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
	// ISBLossBox — release #60 P2 (D4.2-R27/R28/R30, knob
	// mentor_isb_loss_box): what an ISB stop-out boxes. "range" (default, the
	// course): the whole 5m range that held the trade — every closed 5m bucket
	// from the fill candle's through the stop-out candle's — unioned with the
	// entry-to-stop band; overlapping ISB losses union into one place and two
	// turn it off for the day [D4.2 p2 @02:41–03:17, @03:43–04:07, @04:13–05:03].
	// "band": the pre-#60 entry-to-stop band only. "" reads as range
	// (limits.go isbLossRange).
	ISBLossBox string

	// m466 P2 (release #61) — levels and touches. Every value below has a
	// knob back to the pre-#61 behaviour (mentor_tuning.*).
	//
	// UnoKeyLevels — G2 D1.3-R21: "Hỗ trợ bị phá → sẽ thành kháng cự khi
	// backtest" [D1.3 p1 @12:31–12:58]. "on" (default): a KEY level that HELD
	// today (≥1 reject visit) and then breaks FLIPS its role once
	// (level_broken_uno) instead of turning ISB-only; the backtest reject from
	// the other side trades through the normal gates. A second wrong-way close
	// after the flip, or a wrong-way FIRST visit with KeyLevelBreakRetest off
	// (LL-2: on, the default, flips that break too), is ISB-only as before
	// [D5.2 p1 @19:44–20:25]. "off": the pre-#61 ISB-only on any break.
	UnoKeyLevels string
	// KeyLevelBreakRetest — levels audit LL-2. "on" (default, October): a KEY
	// level whose FIRST touch closes through it is not made ISB-only; the
	// break cancels the arm, flips the level's role and starts a new visit
	// (counter level_broken_retest), so the first retest candle that touches
	// from the new side and closes back on it is the PHL/PLH reference — the
	// stop order goes beyond that candle. OctD5.2-R20 [@07:42] "nến nằm ở dưới
	// đường line level này, nó breakdown… Level này đang là kháng cự"; R24
	// [@13:35] the 28,154.50 worked example "nó quay ngược lên, nó bắt test…
	// vẫn là nến chạm level… Thứ 2 nữa là nến đóng dưới"; R25 [@14:16] sell
	// stop on the FIRST retest candle that touches; R22/R23 [@12:16, 12:29]
	// touch, then where it closes. One flip per level per session day (shared
	// with Uno, UnoFlipped): a second close through after the flip is
	// ISB-only. How many retests count after one break is not in the course —
	// mentor question (LL-2). "off": the September "first touch closed through
	// → invalid, ISB-only" [D5.2 p1 @19:51–20:25].
	// Studio JSON: risk_control.mentor_tuning.key_level_break_retest.
	KeyLevelBreakRetest string
	// SessionOpenLevels — G10 D3.1-R26 + G11 D3.1-R20: "cái râu này nó sẽ là
	// cái đỉnh" [D3.1 p1 @11:58–12:22] (the session's first 5m candle wick is
	// an old high/low with no left-neighbour test) and "Cái cạnh dưới của cái
	// gap, lúc nào nó cũng sẽ là resistance… Level là resistance" [@09:41–09:51]
	// (the gap's far edge = the prior session's last close is a level while
	// unfilled). "on" (default) | "off" (neither).
	SessionOpenLevels string
	// LevelArmLineCancel — G17 D2.3-R34: "Mình làm gì? Mình cancel"
	// [D2.3 p1 @23:35–24:03] — a resting order on a MOVING line (trendline,
	// EMA 34) is cancelled by a close through the line's CURRENT price, and
	// a trendline that left the set cancels its order. "current_line"
	// (default) | "placement_price" (the pre-#61 frozen placement price).
	LevelArmLineCancel string
	// PHLPullbackLow — G41 D2.2-R59: "đang đi kiếm một cái lệnh có đáy cao hơn
	// đáy trước đó" [D2.2 p3 @08:25–09:25]: "pullback" (default) = the lowest
	// low of the WHOLE pullback (old high → signal candle) must stay above the
	// prior low (phl_pullback_lower_low; mirror plh_pullback_higher_high);
	// "signal" = the pre-#61 signal-candle-only comparison.
	PHLPullbackLow string
	// ── m466 P1 (release #61): boxes, breaks, ranges ──────────────────────
	// FiveMinBoxSource — G28 D4.1-R37 [D4.1 p1 @22:31–22:49 "cái nến inside
	// bar nào gần với thời điểm đó nhất, bọc lại cho em"] + G32 D4.2-R10:
	// "merge" (default, ISB5-F1 OctD4.3-R59 [Oct D4.3 @ 58:00] "bọc từ head
	// to toe"; October wins) = every 5m candle that CLOSES inside the standing
	// box widens it to its high/low (isb_5m_box_merged); the box keeps the
	// first inside bar's time and direction (OctD4.3-R54) and is deleted only
	// on a 5m close outside (OctD4.3-R67); crossing 5m pairs inside it do not
	// lift the R70 same-direction gate (ISB5-F2). No growth cap: the course
	// states none. "newest" = the #61–#66 read: a NEWER closed 5m inside bar
	// replaces it (isb_5m_box_replaced, _opposite when the direction flips —
	// the 15m/5m conflict then reads the new box); "first" = the pre-#61 read
	// (the first box stands until it escapes). Studio:
	// risk_control.mentor_tuning.mentor_5m_box_source.
	FiveMinBoxSource string
	// BreakDirectionLock — G21 D4.1-R31/R33 [D4.1 p1 @18:29–20:33] + G29
	// D4.1-R55 [D4.1 p1 @07:47–08:07]: after a box BREAK (an FTGH/FTGL body
	// escape, a 5m/15m/30m ISB rest-box 1m-close escape) only setups in the
	// break direction trade (break_counter_setup) until a 1m CLOSE back inside
	// the broken box (the failed break, D4.1-R30), an opposite break, or the
	// 17:00 CT roll (break_lock.go). Default ON. Studio:
	// risk_control.mentor_tuning.mentor_break_direction_lock.
	BreakDirectionLock bool
	// SmallRangeGuard — G33 D4.2-R33 [D4.2 p2 @06:16–06:30 "bên trong một cái
	// range nó quá nhỏ đi, đừng bao giờ đụng tới"]: the last 15 closed 1m
	// candles span <= 61.50 pts with two candles >= 30.00 pts → no entry priced
	// inside that range (range_too_small_big_candles) until a 1m close
	// outside it (range_small.go; the numbers are the video's measurement).
	// Default OFF (release #61 final): the course gives no count of 30–40-pt
	// candles and no breakout margin — only the one worked example — and three
	// review rounds kept finding mis-latches (the breakout candle, a churn
	// close just outside the prior 14 wicks with no margin). It stays behind
	// the knob and is on the mentor list; a stored true still wins.
	// Studio: risk_control.mentor_tuning.mentor_small_range_guard.
	SmallRangeGuard bool
	// MidRangeRejectSide — G43 D2.2-R71 [D2.2 p3 @14:28–15:58 "Nó mới reject
	// thì đánh theo hướng nó reject"]: between an FTGL floor and an FTGH
	// ceiling, after a 1m candle rejects the ceiling (High >= its bottom, Close
	// below) a LONG entry is refused mid_range_against_rejection until a
	// candle touches the floor top; mirror for a floor reject (reject_side.go).
	// Default ON. Studio: risk_control.mentor_tuning.mentor_mid_range_reject_side.
	MidRangeRejectSide bool

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

		KeyLevelTFMinutes:       60,
		KeyLevelRTHOnly:         true,
		KeyLevelPrunePts:        20,
		KeyLevelDeleteOn1HClose: KeyLevelDeleteOn1HCloseOff, // LL-1: October keeps a level a 1H body crossed [OctD5.2-R13..R20 @04:10–07:42]
		KeyLevelHistorySource:   KeyLevelHistoryOwn,         // LL-4: the current contract's own candles [OctD5.2-R16 @06:18]

		EMAPeriod34:  34,
		EMAPeriod9:   9,
		EMATFMinutes: 1,

		EMALocationTFMinutes: 1, // R3: intraday EMA 34 lives on the 1m chart

		TouchBandPts: 0, // §3: wait for the LITERAL touch [D3.3 p1 @ 00:13]; D5.2 p2 @18:36–18:45 "gần gần gần đụng … Còn như cái này thì bắt buộc là phải đụng nha"

		ISBBufferPts:  1.5,
		ISBStopMinPts: 5,

		ISBReverseEMA9Enabled: true,

		WickMicroscalpEnabled: false, // D4.3 advanced wick read — OFF by default
		WickSafePlay:          false, // D4.3-R20 safe play — an optional extra confirm, OFF by default

		PHLMinCandlesFromExtreme: 3,
		PHLTargetShyPts:          5,
		PHLEntryBufferPts:        1.0,                     // [Oct D2.3 @ 46:20-47:20] OctD2.3-R46 entry 1 pt beyond the candle extreme (supersedes Sept D2.2-R10 +0.50 [p1 @04:14–05:43])
		PHLStopBufferPts:         1.0,                     // [Oct D2.3 @ 46:20-47:20] OctD2.3-R46 stop 1 pt beyond the other extreme, 2 pts total (supersedes Sept D2.2-R10 −0.50 [p1 @04:14–05:43])
		PHLOldExtreme1mK:         PHLOldExtreme1mKDefault, // PHL-1: the 1m swing the pullback started from (k = 4 reproduces WE6/7/9/10; the course states no width — knob)
		RollingPHL:               true,                    // D2.2 pullback-continuation rolling stop entry — ON (c52 W6) with the pullback-colour + 5m-trigger gates [@01:52–03:34, 10:47–11:36]

		StopCeilingPts:         25,
		RoomMultiple:           2,
		HTFGateNewsOnly:        false,               // m462: the 4h/1h gate applies ALL SESSION (D5.1-R9/R11); true = the old D4.4-11 news + open windows
		NearBoxRoomMultiple:    2,                   // Day-3 row 24: near-box room = 2 × risk (the 2 is OURS — B11); between two boxes exempt (row 25)
		NearBoxRoomPHL:         true,                // B11: the near-box skip also gates PHL/PLH (our extension of the ISB lesson)
		NearBoxBetweenExempt:   false,               // m465 D3.2-R29/R30: no blanket two-box exemption (E6 refused) — the middle case passes on its own room
		TrendlineEntryTouch:    TrendlineEntryThird, // m472 A2 [Oct D3.2 @ 31:00-31:17] "chỉ cần 2 touch là được", [Oct D3.2 @ 41:20-41:27] two bottoms then the return is traded; supersedes m465 D2.3-R7 (Sept) "lần thứ tư mới dám vô"; "fourth" stays as the stricter knob value
		PHLFlatLow:             PHLFlatLowNear,      // m465 D2.2-R64: a flat low (within flatNearPts) is an FTGL, not a PHL
		ISBReverseTouch:        ISBReverseTouchPair, // m465 D5.4-R7: EMA 9 inside the mother + ISB group counts
		ISBOldExtremePts:       2,                   // B27 [ours]: "at the old extreme" = ± 2 pts
		ISBInRangeGapPts:       0,                   // B27 [ours]: 0 = inherit PingPongMinGapPts (50)
		ISBStackCancelCandle2m: 4,                   // B8 [ours]: the 1m course count (cancel on candle 4) reused on the 2m
		ISBStackTinyStopPts:    12.5,                // m465 D5.1-R26: the worked example stop, measured 12.50 pts (29,285.75→29,273.25) [D5.1 p2 @02:24–04:41]
		EveningSlowPts:         100,                 // B10 [ours]: X11 gives no cutoff; owner Q6 fixed none
		EveningSlowFromHour:    15,                  // B10 [ours]: the RTH close
		EveningSlowWindowMin:   0,                   // B10 [ours]: cumulative since the start hour
		LossDeparturePts:       0,                   // B22: structural departure; the numeric fallback is OFF
		LocTriggerFilter:       true,
		TriggerSchool:          1,  // B20: school 1 — level/box entries without 5m agreement
		PingPongMinGapPts:      50, // B21: gap > 50 AND candles <= 20 over the last 30
		PingPongCandleMaxPts:   20, // B21: "nến tầm mười mấy điểm" (D4.2 p2 @05:31–05:37; the 100-pt range = candles ~20)
		PingPongCandleLookback: 30, // B21 [C: ours — the 30-bar window; the 20-pt cap itself matches D4.2 p2 @05:31–05:37]: keeps the on-camera rejection @06:21 (box ping-pong only; the key-level pair reads the current candle, MENTOR-NY item 2)
		LevelMaxVisits:         3,  // B23: first 3 visits/day trade, the 4th refuses
		EmaMaxCross30m:         0,  // E4 (item 16): REMOVED default (#45 — the mentor gives no count; "xien len xien xuong" is qualitative). The EMA-off-while-not-respected rule is E2 (loss → blocked until departure). An explicit 2-4 re-enables the numeric gate.
		RangeGapPts:            0,

		DayGateSpentPts:     265, // m465 D5.1-R18: the worked example's -265.00 run was called spent [D5.1 p1 @17:56–18:14]
		DayGateTargetCapPts: 15,
		DayOffRecheck:       false, // course-defaults-30: the course shuts the whole day [D5.1 p1 @19:22–19:40]

		Swing: DefaultSwingCfg(),

		Box: DefaultBoxCfg(),

		OrbGateEnabled: true,

		// m472 A1: the ISB-exec schedule defaults OFF — the 1m all day.
		// "Trade khung 1 phut, chu khong la khung 2 phut. Khung 2 phut no quet
		// banh xac" [Oct D4.2 @ 22:56-23:46], repeated [Oct D5.4 @ 03:09-03:24],
		// [Oct D5.5 @ 09:16-09:20]; EMA 34 on the 1m only [Oct D5.3 @ 02:00]. This
		// supersedes Release #31 rule 4 (X5-10; Sept "2m after the first 30 min").
		Exec2mAfter30m: false,
		// Release #31 rule 5: the ORB-edge ISB candle must be ≥ 80% outside.
		OrbISBFractionOutside: 0.80,

		LegBudgetEnabled: true,
		LegResetOn:       "close",

		CourseExits: DefaultCourseExitsCfg(),
		// m465 P1 (release #60): the early 15m read, mode C from the standing
		// boxes, the trigger escalation 5m → 15m → 30m.
		Early15mRead:        true,                       // D4.2-R2/R11, D4.3-R21 (defensive: refusals only)
		MTFConfluenceSource: MTFConfluenceStandingBoxes, // D4.2-R17
		TriggerEscalation:   true,                       // D3.4-R43
		CrossingISBLookback: 3,                          // isbTF-F6: [B] reading, mentor Q10 (no course number)
		CrossingISBMin:      2,                          // isbTF-F6: [B] reading, mentor Q10 (no course number)
		// Release #25 course-entry knobs (COMMON-25 knob rule).
		FifteenMinIBRule:      FifteenMinRequire5mISB, // CE-1 (a)
		TFConflictEnd:         TFConflictBothBoxes,    // CE-2 — course-defaults-30: "đợi nó thoát cả luôn" [D4.2 p1 @07:54–08:02]
		NewsWaitBothLines:     true,                   // CE-3 — course-defaults-30 [D4.4 p1 @21:55–22:12]
		NewsBarsExcluded:      true,                   // CE-4 (a, release #29 rework: freeze the pre-print read, candles whole)
		ISBBoxEscapeMode:      ISBBoxEscapeCloseBoxTF, // m473 B3 OctD4.3-R67: a 1m close = permission, the box is dropped on a close of its OWN TF [Oct D4.3 @ 65:00-66:13]; was CE-5 -> m466 G20 D4.1-R34 / G49 D3.4-R34: a 1m CLOSE outside the box [D4.1 p1 @20:36–21:03; D3.4 p2 @05:37–06:01]
		EmaLossRelock:         EmaRelockUntilZoneExit, // CE-6 (a)
		D43TrendSource:        D43TrendStructure,      // CE-8 — course-defaults-30: 5m swing structure [D4.3 @12:43–12:54]
		TrendlineIsTarget:     true,                   // CE-11/B12 (a)
		TrendlineBetweenBoxes: true,                   // CE-11/B13 — course-defaults-30 [D3.2 p2 @10:56–11:04]
		FlagWallTarget:        true,                   // CE-11 — course-defaults-30 [X9 @05:59–06:12]
		LegResetOnWaveBreak:   true,                   // CE-12 (a)
		BoxNewZoneAfterEscape: true,                   // B52 (a) — new zone after a range escape
		ISBLossBox:            ISBLossBoxRange,        // #60 P2 — D4.2-R27: box the whole 5m range
		// m466 P2 (release #61): levels and touches.
		UnoKeyLevels:        UnoKeyLevelsOn,            // G2 D1.3-R21: a held-then-broken key level flips (Uno Reverse)
		KeyLevelBreakRetest: KeyLevelBreakRetestOn,     // LL-2 OctD5.2-R20/R24/R25 [@07:42–14:16]: a first-touch break flips the level; the first retest arms
		SessionOpenLevels:   SessionOpenLevelsOn,       // G10/G11 D3.1-R26/R20: the open wick + the gap far edge are levels
		LevelArmLineCancel:  LevelArmLineCancelCurrent, // G17 D2.3-R34: close through the line's CURRENT price cancels
		PHLPullbackLow:      PHLPullbackLowPullback,    // G41 D2.2-R59: the whole pullback's low must hold
		// m466 P1 (release #61): boxes, breaks, ranges.
		FiveMinBoxSource:   FiveMinBoxMerge, // ISB5-F1 OctD4.3-R59 (October wins over G28 D4.1-R37 / G32 D4.2-R10)
		BreakDirectionLock: true,            // G21 D4.1-R31/R33, G29 D4.1-R55
		SmallRangeGuard:    false,           // G33 D4.2-R33 — OFF until the mentor answers (count / margin unstated)
		MidRangeRejectSide: true,            // G43 D2.2-R71
	}
}

// Release #25 course-entry knob values (the ONE spelling the kernel, the
// trader resolver, the boot line and the guide share).
const (
	FifteenMinOff          = "off"
	FifteenMinRequire5mISB = "require_5m_isb"
	FifteenMinUntilEscape  = "no_trade_until_escape"

	// m465 P1 (D4.2-R17): mode C's confluence source.
	MTFConfluenceStandingBoxes = "standing_boxes"
	MTFConfluenceLastPair      = "last_pair"

	TFConflictFirstBox  = "first_box"
	TFConflictBothBoxes = "both_boxes"
	ISBBoxEscapeClose   = "close"
	// ISBBoxEscapeCloseBoxTF (m473 B3, the default): a 1m CLOSE outside gives
	// trade permission, but the box is DELETED only when a candle of the box
	// timeframe (5m/15m/30m) closes outside [Oct D4.3 @ 65:00-66:13].
	ISBBoxEscapeCloseBoxTF = "close5m"
	ISBBoxEscapeBody       = "body"
	EmaRelockSession       = "session"
	EmaRelockUntilZoneExit = "until_zone_exit"
	D43TrendTriggerLine    = "trigger_line"
	D43TrendStructure      = "structure"

	// m465 P3 (release #60) knob values.
	TrendlineEntryFourth     = "fourth"
	TrendlineEntryThird      = "third"
	PHLFlatLowNear           = "flat_near"
	PHLFlatLowStrict         = "strict"
	ISBReverseTouchPair      = "pair"
	ISBReverseTouchISBCandle = "isb_candle"

	// m466 P1 (release #61, G28/G32): the 5m ISB rest box source.
	FiveMinBoxNewest = "newest"
	FiveMinBoxFirst  = "first"
	// ISB5-F1 OctD4.3-R59 [Oct D4.3 @ 58:00]: the 5m candles that stay inside
	// the standing box merge into ONE inside bar ("bọc từ head to toe").
	FiveMinBoxMerge = "merge"
)

// m466 P2 (release #61) knob values — the ONE spelling the kernel, the trader
// resolver and the guide share.
const (
	// levels audit LL-1: the 1H body-close deletion (deck) vs October's keep.
	KeyLevelDeleteOn1HCloseOn  = "on"
	KeyLevelDeleteOn1HCloseOff = "off"
	// levels audit LL-2: the October break-and-retest of a key level.
	KeyLevelBreakRetestOn       = "on"
	KeyLevelBreakRetestOff      = "off"
	UnoKeyLevelsOn              = "on"
	UnoKeyLevelsOff             = "off"
	SessionOpenLevelsOn         = "on"
	SessionOpenLevelsOff        = "off"
	LevelArmLineCancelCurrent   = "current_line"
	LevelArmLineCancelPlacement = "placement_price"
	PHLPullbackLowPullback      = "pullback"
	PHLPullbackLowSignal        = "signal"
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
	// breaks the prior 5m candle's low in a 5m uptrend (mirror: the high in a
	// downtrend) warns an entry WITH that trend, so it sizes down [Go over lớp
	// học @00:13:21–00:15:19]. Stamped per entry side (C52-W10 B20).
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
