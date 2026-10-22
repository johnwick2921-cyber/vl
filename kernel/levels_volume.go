package kernel

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"vl/logger"
	"vl/market"
)

// Pack B — VOLUME FAMILY detectors (owner override 2026-08-26, research
// authority). The backward Phase-0 replay is WAIVED (no historical bars
// existed when the volume wave was first scoped) — forward validation via the
// B4 level_stats table replaces it.
//
// Phase 0(a) re-confirmed on live frames 2026-08-26: NT8 bar_update/bars_historical
// frames carry real per-minute V (1m MNQ: V=460 / 728 / 631 observed), and the
// bars table persists them. The volume substrate is REAL, not simulated.
// Phase 0(b): session VWAP / POC profile need only 1m OHLCV — computable from
// the bars table + BarCache, no tick data required.
//
// All detectors here are pure and deterministic on the closed 1m slice the
// kernel already holds. VWAP is anchored at the CME session-day boundary
// (17:00 CT, kernel/cme_calendar.go — the same roll the rest of the system
// uses), so it re-emits dynamically every cycle as bars close.

// ── session VWAP + σ bands ─────────────────────────────────────────────────

// SessionVWAPLevels (B1) — session-anchored VWAP (17:00 CT roll) plus the
// ±1σ fair-value bands, emitted as lines. Dynamic re-emit: recomputed from
// the closed bars on EVERY cycle, so the level moves with the auction.
// VWAP bands are the institutional mean-reversion envelope (research: the
// bulk of session volume trades inside ±1σ; rejection there is the classic
// fade signal). Emits nothing until ≥2 closed bars exist in the session.
func SessionVWAPLevels(bars []market.Kline, now time.Time) []DetectedLevel {
	cb := closedBars(bars, now)
	if len(cb) == 0 {
		return nil
	}
	sessStart := CMESessionDayStart(now).UnixMilli()
	session := make([]market.Kline, 0, len(cb))
	for _, b := range cb {
		if b.OpenTime >= sessStart {
			session = append(session, b)
		}
	}
	vwap, sd := vwapAndStdev(session)
	if vwap <= 0 || len(session) < 2 {
		return nil
	}
	day := time.UnixMilli(sessStart).In(CTLocation()).Format("2006-01-02")
	anchor := int64(0)
	if session[0].OpenTime == sessStart {
		anchor = sessStart
	}
	return captureFormationGroup([]DetectedLevel{
		lineLevel(KindVWAP, vwap, "VWAP", day, false),
		lineLevel(KindVWAP, vwap+sd, "VWAP+1σ", day, false),
		lineLevel(KindVWAP, vwap-sd, "VWAP−1σ", day, false),
		// Level-truth wave (2026-08-27) — ±2σ bands EMITTED at 0.85 per owner
		// ruling (the dispatched spec listed them; the emission was missing).
		lineLevel(KindVWAP2S, vwap+2*sd, "VWAP+2σ", day, false),
		lineLevel(KindVWAP2S, vwap-2*sd, "VWAP−2σ", day, false),
	}, anchor, len(session), "session_anchor", now)
}

// vwapAndStdev computes the session VWAP (typical price = (H+L+C)/3, weighted
// by volume) and the volume-weighted standard deviation of typical prices.
func vwapAndStdev(bars []market.Kline) (vwap, sd float64) {
	var pv, v float64
	for _, b := range bars {
		tp := (b.High + b.Low + b.Close) / 3
		pv += tp * b.Volume
		v += b.Volume
	}
	if v <= 0 {
		return 0, 0
	}
	vwap = pv / v
	var acc float64
	for _, b := range bars {
		tp := (b.High + b.Low + b.Close) / 3
		d := tp - vwap
		acc += b.Volume * d * d
	}
	sd = math.Sqrt(acc / v)
	return vwap, sd
}

// ── extended VWAP (since-cash-close anchor) ────────────────────────────────

// EVWAPLevels (B1) — extended VWAP anchored at the most recent 15:00 CT CASH
// CLOSE (RTH close), spanning the close hour + the whole overnight + today's
// session. Overnight-positioned funds mark eVWAP; the distance between session
// VWAP and eVWAP measures overnight inventory.
//
// A2 (mega-research 2026-08-26) — the old 16:00 CT anchor was a DEGENERATE
// DUPLICATE of session VWAP: no bars exist 16:00–17:00 CT (maintenance halt),
// so both windows were byte-identical and eVWAP burned a seat for nothing.
// 15:00 CT makes it a real, distinct object ("since-cash-close" VWAP).
func EVWAPLevels(bars []market.Kline, now time.Time) []DetectedLevel {
	cb := closedBars(bars, now)
	if len(cb) == 0 {
		return nil
	}
	ct := now.In(CTLocation())
	anchorCT := time.Date(ct.Year(), ct.Month(), ct.Day(), 15, 0, 0, 0, CTLocation())
	if ct.Hour() < 15 {
		anchorCT = anchorCT.AddDate(0, 0, -1)
	}
	anchorCT = lastCashCloseWithBars(cb, anchorCT)
	anchorMs := anchorCT.UnixMilli()
	win := make([]market.Kline, 0, len(cb))
	for _, b := range cb {
		if b.OpenTime >= anchorMs {
			win = append(win, b)
		}
	}
	ev, _ := vwapAndStdev(win)
	if ev <= 0 || len(win) < 2 {
		return nil
	}
	anchor := int64(0)
	if win[0].OpenTime == anchorMs {
		anchor = anchorMs
	}
	return captureFormationGroup([]DetectedLevel{lineLevel(KindEVWAP, ev, "eVWAP", anchorCT.Format("2006-01-02"), false)}, anchor, len(win), "cash_close_anchor", now)
}

// ── prior-day volume profile (pdPOC / VAH / VAL) ───────────────────────────

// pdProfileCache memoizes a COMPLETED prior session-day's profile (cached at
// roll — spec B1). A prior session-day's bars are immutable once the current
// day's 17:00 CT roll passed, so the cache is safe forever.
var pdProfileCache sync.Map // dayKey → []DetectedLevel

// PriorDayProfileLevels (B1) — the prior CME session-day's point of control
// (max-volume price) and the 70% value area (VAH/VAL). Cached per day at roll.
// POC/VAH/VAL are the auction's accepted-value references: the open inside/
// outside value area is the single most robust day-type classifier.
func PriorDayProfileLevels(bars []market.Kline, now time.Time) []DetectedLevel {
	return priorDayProfileLevelsFor("", bars, now)
}

func priorDayProfileLevelsFor(symbol string, bars []market.Kline, now time.Time) []DetectedLevel {
	cb := closedBars(bars, now)
	if len(cb) == 0 {
		return nil
	}
	// N4 (release #23): the cache is keyed by symbol AND session — the old
	// day-only key let a second instrument read the first one's profile.
	if _, dayKey := priorSessionBars(cb, CMESessionDayStart(now)); dayKey != "" {
		if v, ok := pdProfileCache.Load(symbol + "|" + dayKey); ok {
			return v.([]DetectedLevel)
		}
	}
	prior, dayKey := priorSessionFor(symbol, cb, now)
	if len(prior) == 0 {
		return nil
	}
	out := profileLevels(prior, dayKey, "")
	out = captureFormationGroup(out, prior[len(prior)-1].CloseTime, len(prior), "prior_profile_last_source_close", now)
	if len(out) > 0 {
		pdProfileCache.Store(symbol+"|"+dayKey, out)
	}
	return out
}

// profileLevels computes POC + 70% value area from a closed 1m series, using
// the SAME SVP definition as the chart (m239): 1.25-pt rows with each bar's
// volume spread across the rows its range overlaps — one POC, never a second
// close-binned value 7+ pts away.
// prefix labels the rows ("pd" for the prior-day profile, "" for session).
func profileLevels(bars []market.Kline, dayKey, prefix string) []DetectedLevel {
	if len(bars) == 0 {
		return nil
	}
	hist := make(map[int]float64, 128)
	for _, b := range bars {
		svpAddBar(hist, b.Low, b.High, b.Volume)
	}
	poc, vaLow, vaHigh, _, total := svpValueArea(hist, SVPValueAreaPercent)
	if total <= 0 {
		return nil
	}
	pocPx := (float64(poc) + 0.5) * SVPRowHeight // row midpoint (the SVP POC)
	vah := float64(vaHigh+1) * SVPRowHeight      // top edge of the top VA row
	val := float64(vaLow) * SVPRowHeight         // bottom edge of the bottom VA row
	return []DetectedLevel{
		lineLevel(KindPOC, pocPx, prefix+"POC", dayKey, false),
		lineLevel(KindVAH, vah, prefix+"VAH", dayKey, false),
		lineLevel(KindVAL, val, prefix+"VAL", dayKey, false),
	}
}

// ── naked POC (10-session retire-on-touch) ────────────────────────────────

// nPOCRetireTick is the retire-on-touch tolerance (register A4: the spec's
// ±1-tick rule). A bar retires a naked POC only when it brackets BEYOND
// ±1 tick — a 0.01-pt graze no longer consumes the magnet. MNQ tick = 0.25.
const nPOCRetireTick = 0.25

// NakedPOCLevels (B1) — prior session-days' POC that price has NOT traded
// back through since birth = naked POC (unfinished business; price revisits).
// RETIRE-ON-TOUCH: any later session bar whose range covers the POC ±1 tick
// retires it (it has been revisited → no longer naked). Older than 10 sessions
// → retired regardless (spec). The live cache covers ~2 sessions; the
// 10-session window becomes fully exercisable as the 90-day bars table warms
// forward (validation, B4).
func NakedPOCLevels(bars []market.Kline, now time.Time) []DetectedLevel {
	cb := closedBars(bars, now)
	if len(cb) == 0 {
		return nil
	}
	curDay := CMESessionDayStart(now)
	type dayPOC struct {
		day   string
		poc   float64
		birth int64 // first bar open of the day (touch comparisons start after)
		bars  int   // recording-only source window size
	}
	var days []dayPOC
	for d := 1; d <= 10; d++ {
		dayStart := curDay.AddDate(0, 0, -d)
		startMs, endMs := dayStart.UnixMilli(), dayStart.AddDate(0, 0, 1).UnixMilli()
		var dayBars []market.Kline
		for _, b := range cb {
			if b.OpenTime >= startMs && b.OpenTime < endMs {
				dayBars = append(dayBars, b)
			}
		}
		if len(dayBars) == 0 {
			continue
		}
		// N4 guard (bug-hunt #24): the 1m ring holds ~1.45 sessions, so the
		// prior day is a front-truncated tail for most of the NY session. A POC
		// built from that tail is NOT the session POC ("nPOC" at the wrong
		// price) — skip it; the stored-profile provider supplies the
		// whole-session nPOC for that day.
		if sessionFrontTruncated(dayBars[0].OpenTime) {
			continue
		}
		prof := profileLevels(dayBars, "", "")
		if len(prof) == 0 {
			continue
		}
		days = append(days, dayPOC{
			day:   dayStart.In(CTLocation()).Format("2006-01-02"),
			poc:   prof[0].Price,
			birth: dayBars[len(dayBars)-1].CloseTime,
			bars:  len(dayBars),
		})
	}
	var out []DetectedLevel
	for _, d := range days {
		touched := false
		for _, b := range cb {
			if b.OpenTime <= d.birth {
				continue
			}
			// A4/S4 — ±1-tick tolerance: a graze inside the tick band does NOT
			// retire the POC; the bar must bracket beyond ±1 tick.
			if b.Low <= d.poc-nPOCRetireTick && b.High >= d.poc+nPOCRetireTick {
				touched = true
				break
			}
		}
		if touched {
			continue // retire-on-touch
		}
		out = append(out, WithFormationClose(lineLevel(KindNPOC, d.poc, "nPOC·"+d.day, d.day, false), d.birth, d.bars, "prior_profile_last_source_close", now))
	}
	return out
}

// ── pdVWAP · SETT · MID-O ──────────────────────────────────────────────────

// priorSessionBars returns the closed bars of the PRIOR CME session — the
// last session-day before curDay that HAS bars — and its session key.
//
// R22 F-1: every "prior day" reader used curDay − 1 calendar day. For the
// Monday session (Sunday 17:00 open) that is Saturday 17:00 → Sunday 17:00,
// where no bars exist, so pdVWAP, SETT and the prior-day profile were absent
// for the WHOLE Monday session every week (and after every holiday). The
// prior session is Friday's.
func priorSessionBars(cb []market.Kline, curDay time.Time) ([]market.Kline, string) {
	curMs := curDay.UnixMilli()
	key := ""
	for i := len(cb) - 1; i >= 0; i-- {
		if cb[i].OpenTime < curMs {
			key = CMESessionDayKey(time.UnixMilli(cb[i].OpenTime))
			break
		}
	}
	if key == "" {
		return nil, ""
	}
	var out []market.Kline
	for _, b := range cb {
		if b.OpenTime < curMs && CMESessionDayKey(time.UnixMilli(b.OpenTime)) == key {
			out = append(out, b)
		}
	}
	return out, key
}

// ── N4 (release #23): the prior session is read WHOLE, or not at all ─────────
//
// priorSessionBars takes whatever part of the prior session falls inside the
// caller's slice — the 1m ring of ~2,000 bars. The full 1,380-bar session fits
// only until ~03:20 CT, so from then on pdVWAP and the prior-day profile ran on
// a FRONT-TRUNCATED session every day (10-13: pdVWAP −16.23 at 08:30, −29.57 at
// 11:00, −54.21 at 14:30 against the whole session; pdProfileCache then kept the
// first, truncated profile for the whole day). The same class #514 fixed for
// dATR (sessionFrontTruncated, datr_sessions.go).
//
// The trader installs PriorSessionBarsProvider (the store's NT8 1m rows of the
// current contract). A truncated slice session is replaced by the stored one
// when the store holds it whole; otherwise the session is REFUSED — no pdVWAP,
// no prior-day profile — logged once per symbol and session and counted.
// SETT reads only the session's LAST bar and keeps the slice read.

// PriorSessionBarsProvider returns the stored closed 1m bars of symbol in
// [fromMs, toMs), oldest first; ok=false when the store cannot answer.
var PriorSessionBarsProvider func(symbol string, fromMs, toMs int64) ([]market.Kline, bool)

var (
	priorSessionFromStore     atomic.Int64
	priorSessionRefused       atomic.Int64
	priorSessionRefusedLogged sync.Map // symbol|sessionKey → true
	// priorSessionWhole keeps the last WHOLE stored session per symbol: a
	// prior session is immutable once the roll passed, so the store is read
	// once per symbol per session, not once per assembly.
	priorSessionWhole sync.Map // symbol → priorSessionCached
)

type priorSessionCached struct {
	key  string
	bars []market.Kline
}

// PriorSessionReadCounts reports the N4 counters (never reset): how often a
// truncated slice session was replaced by the stored whole session, and how
// often it was refused.
func PriorSessionReadCounts() map[string]int64 {
	return map[string]int64{
		"prior_session_from_store":        priorSessionFromStore.Load(),
		"prior_session_refused_truncated": priorSessionRefused.Load(),
	}
}

// priorSessionFor returns the WHOLE prior CME session for the volume readers,
// or nothing.
func priorSessionFor(symbol string, cb []market.Kline, now time.Time) ([]market.Kline, string) {
	cur := CMESessionDayStart(now)
	prior, key := priorSessionBars(cb, cur)
	if len(prior) == 0 || !sessionFrontTruncated(prior[0].OpenTime) {
		return prior, key
	}
	start := CMESessionDayStart(time.UnixMilli(prior[0].OpenTime))
	if v, ok := priorSessionWhole.Load(symbol); ok && symbol != "" {
		if c := v.(priorSessionCached); c.key == key {
			priorSessionFromStore.Add(1)
			return c.bars, key
		}
	}
	if PriorSessionBarsProvider != nil && symbol != "" {
		if stored, ok := PriorSessionBarsProvider(symbol, start.UnixMilli(), cur.UnixMilli()); ok {
			var sess []market.Kline
			for _, b := range stored {
				if b.CloseTime < now.UnixMilli() && CMESessionDayKey(time.UnixMilli(b.OpenTime)) == key {
					sess = append(sess, b)
				}
			}
			if len(sess) > 0 && !sessionFrontTruncated(sess[0].OpenTime) && len(sess) >= len(prior) {
				priorSessionFromStore.Add(1)
				priorSessionWhole.Store(symbol, priorSessionCached{key: key, bars: sess})
				return sess, key
			}
		}
	}
	priorSessionRefused.Add(1)
	if _, said := priorSessionRefusedLogged.LoadOrStore(symbol+"|"+key, true); !said {
		sym := symbol
		if sym == "" {
			sym = "n/a"
		}
		logger.Warnf("📉 prior session %s (%s) REFUSED for pdVWAP and the prior-day profile: the 1m slice starts it at %s, not at its %s open, and the store did not supply it whole — no truncated value is drawn (N4; counted prior_session_refused_truncated)",
			key, sym, ClockCT(time.UnixMilli(prior[0].OpenTime)), ClockCT(start))
	}
	return nil, key
}

// lastCashCloseWithBars walks the eVWAP anchor back to the most recent 15:00
// CT cash close that actually traded (a bar in its 15:00–16:00 hour), at most
// a week back. R22 F-1: from Sunday 17:00 to Monday 14:59 the anchor was
// SUNDAY 15:00 — no bars — so eVWAP collapsed onto session VWAP (the very
// duplicate the A2 re-anchor removed). The cash close it means is Friday's.
func lastCashCloseWithBars(cb []market.Kline, anchor time.Time) time.Time {
	for step := 0; step < 7; step++ {
		cand := anchor.AddDate(0, 0, -step)
		from, to := cand.UnixMilli(), cand.Add(time.Hour).UnixMilli()
		for _, b := range cb {
			if b.OpenTime >= from && b.OpenTime < to {
				return cand
			}
		}
	}
	return anchor // nothing traded at any 15:00 in reach — keep the old anchor
}

// PDVWAPLevels (B1) — the prior session-day's session VWAP (17:00-anchored).
// Prior-day VWAP is the inventory line overnight desks still mark the next
// morning.
func PDVWAPLevels(bars []market.Kline, now time.Time) []DetectedLevel {
	return pdVWAPLevelsFor("", bars, now)
}

func pdVWAPLevelsFor(symbol string, bars []market.Kline, now time.Time) []DetectedLevel {
	cb := closedBars(bars, now)
	if len(cb) == 0 {
		return nil
	}
	prior, day := priorSessionFor(symbol, cb, now)
	v, _ := vwapAndStdev(prior)
	if v <= 0 || len(prior) < 2 {
		return nil
	}
	return captureFormationGroup([]DetectedLevel{lineLevel(KindPDVWAP, v, "pdVWAP", day, false)}, prior[len(prior)-1].CloseTime, len(prior), "prior_vwap_last_source_close", now)
}

// nativeDailyCloseFor returns the native 1d close (the ~15:00 CT CME settle)
// and its source bar's CloseTime for a CME trading date ("YYYY-MM-DD"), or 0,0
// when there is no daily source (or no symbol, or no row). Shared by SETT and
// PDC (m239a: one source).
func nativeDailyCloseFor(symbol, tradingDate string) (float64, int64) {
	if symbol == "" {
		return 0, 0
	}
	for _, d := range DailyBarsFor(symbol, datrDailyFetch) {
		if NativeDailyTradingDate(d).Format("2006-01-02") == tradingDate && d.Close > 0 {
			return d.Close, d.CloseTime
		}
	}
	return 0, 0
}

// SETTLevels (B1) — prior settlement: the native 1d close (the CME settle,
// about 15:00 CT). With no native daily row the session's final 1m close is
// emitted as "SETT·last print", never presented as the settlement (m239/m239a).
func SETTLevels(bars []market.Kline, now time.Time) []DetectedLevel {
	return SETTLevelsFor("", bars, now)
}

// SETTLevelsFor is SETTLevels for a named instrument.
func SETTLevelsFor(symbol string, bars []market.Kline, now time.Time) []DetectedLevel {
	cb := closedBars(bars, now)
	if len(cb) == 0 {
		return nil
	}
	// The prior CME session closes on the current session's START date, so its
	// trading date is CMESessionDayKey(now). The native daily bar for that
	// trading date carries the ~15:00 CT settle close.
	tradingDate := CMESessionDayKey(now)
	sett, settleClose := nativeDailyCloseFor(symbol, tradingDate)
	label := "SETT"
	basis := "native_1d_settle"
	sourceClose := settleClose
	sourceCount := 0
	if sett > 0 {
		sourceCount = 1 // one native daily bar
	} else {
		// No native settle on record — fall back to the prior session's final
		// 1m close, but LABEL it a last print, never a settlement.
		prior, _ := priorSessionBars(cb, CMESessionDayStart(now))
		for _, b := range prior {
			sett = b.Close
			sourceClose = b.CloseTime
			sourceCount++
		}
		if sett <= 0 {
			return nil
		}
		label = "SETT·last print"
		basis = "last_print_source_close"
	}
	return captureFormationGroup([]DetectedLevel{lineLevel(KindSETT, sett, label, tradingDate, false)}, sourceClose, sourceCount, basis, now)
}

// MIDOLevels (B1) — the overnight range midpoint: (ONH+ONL)/2 for the CURRENT
// session-day's overnight window (17:00→08:30 CT). The auction's overnight
// balance point; price spends most of RTH revisiting it.
func MIDOLevels(bars []market.Kline, now time.Time) []DetectedLevel {
	cb := closedBars(bars, now)
	if len(cb) == 0 {
		return nil
	}
	// R22 P3-1: the cutover is the 08:30 CT that FOLLOWS this session's 17:00
	// open. It used to be "today 08:30", which from 17:00 to 23:59 lies BEFORE
	// the session start — an empty window, so MID-O vanished every evening.
	sessStart := CMESessionDayStart(now)
	ss := sessStart.In(CTLocation())
	cutover := time.Date(ss.Year(), ss.Month(), ss.Day()+1, 8, 30, 0, 0, CTLocation())
	if now.Before(cutover) {
		// Overnight window still open — use bars so far.
		cutover = now
	}
	hi, lo := math.Inf(-1), math.Inf(1)
	var sourceClose int64
	var sourceCount int
	for _, b := range cb {
		if b.OpenTime >= sessStart.UnixMilli() && b.OpenTime <= cutover.UnixMilli() {
			sourceCount++
			if b.CloseTime > sourceClose {
				sourceClose = b.CloseTime
			}
			hi = math.Max(hi, b.High)
			lo = math.Min(lo, b.Low)
		}
	}
	if math.IsInf(hi, 0) || math.IsInf(lo, 0) {
		return nil
	}
	mid := (hi + lo) / 2
	// The existing inclusive-open detector includes the 08:30 bar. Record its
	// actual later close; never stamp 08:30 while a contributing bar is future.
	if !cutover.Before(now) || sourceClose <= cutover.UnixMilli() {
		sourceClose = 0
	}
	return captureFormationGroup([]DetectedLevel{lineLevel(KindMIDO, mid, "MID-O", sessStart.In(CTLocation()).Format("2006-01-02"), false)}, sourceClose, sourceCount, "inclusive_overnight_last_source_close", now)
}

// VolumeLevels (B1) — the full volume family for one cycle (all detectors).
// No symbol: the prior-session readers use the slice alone (a front-truncated
// prior session is refused — N4).
func VolumeLevels(bars []market.Kline, now time.Time) []DetectedLevel {
	return VolumeLevelsFor("", bars, now)
}

// VolumeLevelsFor is VolumeLevels for a named instrument: the prior-day
// profile and pdVWAP read the WHOLE prior session from the store when the
// slice holds only its tail (N4, release #23).
func VolumeLevelsFor(symbol string, bars []market.Kline, now time.Time) []DetectedLevel {
	var out []DetectedLevel
	out = append(out, SessionVWAPLevels(bars, now)...)
	out = append(out, EVWAPLevels(bars, now)...)
	out = append(out, priorDayProfileLevelsFor(symbol, bars, now)...)
	out = append(out, NakedPOCLevels(bars, now)...)
	out = append(out, pdVWAPLevelsFor(symbol, bars, now)...)
	out = append(out, SETTLevelsFor(symbol, bars, now)...)
	out = append(out, MIDOLevels(bars, now)...)
	return out
}
