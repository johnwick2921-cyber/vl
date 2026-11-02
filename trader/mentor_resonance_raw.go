package trader

import (
	"strings"

	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── m465 P4 (release #60): the RAW-inside-bar resonance + the 2m size knob ──
//
// D2.4-R2 — the resonance ("setup gồng lời") is an INSIDE BAR in the SAME
// direction within 3 candles after a PHL/PLH fill: "sau khi nó khớp lệnh của
// mình, trong vòng 3 cây nến sau đó xuất hiện một setup Inside bar cùng chiều,
// thì đó là một setup cộng hưởng" [D2.4 p1 @01:37, @04:54]. Direction must
// match: "inside bar cho lệnh long, cùng chiều nha anh chị, chứ không phải
// inside bar lệnh short" [D2.4-R3 p1 @03:56, @04:42–04:44].
//
// Before this file the bot armed resonance ONLY from an ISB entry intent that
// passed every placement gate except never-add (m151, trader/mentor_tick.go
// → mentorArmResonanceOnISB) — so an inside bar the evaluator never emitted, or
// whose entry a gate refused (stop above mentor_isb_max_pts, room, location…),
// never armed it. The course names no gate on that inside bar: it is a HOLD
// signal on the open trade, not a new entry. mentorResonanceRawTick reads the
// raw candle pair on every CLOSED entry-frame candle and arms once.
//
// Nothing is placed and no stop moves at the arm: the mode-A break-even is the
// exit drive's (mentorExitDriveResonance, after the 1:1 partial or once in
// profit — m134), exactly as the m151 path. Knob mentor_resonance_raw_isb
// (nil → ON, the course; false → only the m151 gated path arms).

// mentorResonanceRawISBOn resolves mentor_resonance_raw_isb: nil → ON.
func mentorResonanceRawISBOn(rc *store.RiskControlConfig) bool {
	if rc == nil || rc.MentorResonanceRawISB == nil {
		return true
	}
	return *rc.MentorResonanceRawISB
}

// mentor2mReduceSizeOn resolves mentor_2m_reduce_size: nil → ON (D3.1-R30
// "Mình phải giảm size" [D3.1 p1 @14:09–14:35]).
func mentor2mReduceSizeOn(rc *store.RiskControlConfig) bool {
	if rc == nil || rc.Mentor2mReduceSize == nil {
		return true
	}
	return *rc.Mentor2mReduceSize
}

// mentorResonanceRawPair returns the last two CLOSED candles of the tf frame
// (prev = candle 1, cur = the candidate inside bar) that the newest 1m bar
// completed. tf <= 1: the last two 1m bars. tf >= 2: the bucket the newest 1m
// bar closed (mentor.ClosedCandleTF — the SAME aggregation the exec-2m ISB read
// and the 2m trail use) and the closed bucket before it. ok=false while the
// newest bar does not close a tf candle, or there is no earlier closed candle.
func mentorResonanceRawPair(bars []market.Kline, tf int) (prev, cur market.Kline, ok bool) {
	n := len(bars)
	if n < 2 {
		return prev, cur, false
	}
	if tf <= 1 {
		prev, cur = bars[n-2], bars[n-1]
		return prev, cur, prev.Final && cur.Final
	}
	cur, ok = mentor.ClosedCandleTF(bars, tf)
	if !ok {
		return prev, cur, false
	}
	i := n
	for i > 0 && bars[i-1].OpenTime >= cur.OpenTime {
		i--
	}
	if i == 0 {
		return prev, cur, false
	}
	prev, ok = mentor.ClosedCandleTF(bars[:i], tf)
	return prev, cur, ok
}

// mentorResonanceRawWhy decides the raw arm for ONE candle pair (pure): ""
// when the pair arms, otherwise the reason it does not.
func mentorResonanceRawWhy(pos mentorPosition, prev, cur market.Kline, candlesAfterFill int) string {
	switch {
	case pos.Mode == "A-resonance":
		return "already armed"
	case pos.Origin != "PHL" && pos.Origin != "PLH":
		return "not a PHL/PLH fill"
	case candlesAfterFill < mentorResonanceMinCandles || candlesAfterFill > mentorResonanceMaxCandles:
		return "outside the 3-candle window"
	case !mentor.IsISB(prev, cur):
		return "no inside bar"
	case !strings.EqualFold(string(mentor.ISBDirection(prev)), pos.Side):
		return "inside bar for the other side"
	}
	return ""
}

// mentorResonanceRawTick is the D2.4-R2 hook: called by mentorExitDrive on each
// CLOSED entry-frame candle while a PHL/PLH position is open. prev/cur are the
// last two closed candles of the position's entry frame. When cur is an inside
// bar of prev in the position's direction and cur is candle 1..3 after the fill
// (mentorResonanceMinCandles..Max, the window the m151 path uses), resonance is
// armed ONCE (idempotent: mode A never re-arms), persisted, and the runner's TP
// handled exactly as the m151 arm (mentorArmResonanceFor). Counted
// resonance_armed_raw_isb. It places nothing. Returns true when it armed.
func (at *AutoTrader) mentorResonanceRawTick(nt *ntTrader.TCPTrader, p *mentorLivePos, prev, cur market.Kline) bool {
	if at == nil || p == nil || !mentorResonanceRawISBOn(at.mentorRiskControl()) {
		return false
	}
	k := p.candlesAfterFill()
	if why := mentorResonanceRawWhy(p.Pos, prev, cur, k); why != "" {
		return false
	}
	if !at.mentorArmResonanceFor(nt, p, p.Pos.Side) {
		return false
	}
	at.mentorCount("resonance_armed_raw_isb")
	at.logInfof("🧑‍🏫 mentor resonance ARMED on a RAW inside bar: %s %s — candle %d after the fill is an inside bar of a %s candle (%.2f–%.2f), no entry needed — \"trong vòng 3 cây nến sau đó xuất hiện một setup Inside bar cùng chiều\" [D2.4-R2 p1 @01:37, @04:54; mentor_resonance_raw_isb]",
		p.Pos.Symbol, p.Pos.Side, k, mentor.ISBDirection(prev), prev.Low, prev.High)
	return true
}

// mentorResonanceRawDrive runs the raw hook for one live position on the
// closed bar the exit drive is processing. A 2m/5m entry acts only on the 1m
// bar that closes an entry-frame candle (trailBarOK, the same gate the
// FramesAfterFill clock uses); a 1m entry acts on every closed bar. SWING and
// a position whose X8 close is in flight are left alone.
func (at *AutoTrader) mentorResonanceRawDrive(nt *ntTrader.TCPTrader, p *mentorLivePos, bars []market.Kline) {
	if p == nil || p.Pos.Mode == "A-resonance" || (p.Pos.Origin != "PHL" && p.Pos.Origin != "PLH") {
		return
	}
	if p.TrailTFMin > 1 && !p.trailBarOK {
		return
	}
	at.mentorExitMu.Lock()
	x8 := p.X8Exited
	at.mentorExitMu.Unlock()
	if x8 {
		return
	}
	prev, cur, ok := mentorResonanceRawPair(bars, p.TrailTFMin)
	if !ok {
		return
	}
	at.mentorResonanceRawTick(nt, p, prev, cur)
}
