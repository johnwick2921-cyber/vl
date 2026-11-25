package mentor

import (
	"fmt"
	"time"

	"vl/kernel"
	"vl/market"
)

// ── HTF-OPPOSED RANGE HALT (answers-1112 Q1 extra rule, knob
// htf_opposed_range_halt_pts, DEFAULT 0 = OFF) ─────────────────────────────
//
// The rule [Sept D5.1 p1 @19:03-19:26, .srt re-read]: "Khung 4 giờ nó đang
// trigger gì / Khung 1 giờ … Nếu như 2 khung trigger cùng với nhau Ok Mình đánh
// theo khung 4 giờ … Còn nếu như 2 khung giờ đang ngược nhau / Mà khung daily nó
// đã chạy được 300-400 điểm rồi / Tắt máy nghỉ luôn cho em" (if the 4h and the 1h
// are opposed and the daily candle has already run 300-400 points, shut the
// machine off and rest). "Ngày hôm đó không đáng để trade" [@19:38-19:40].
//
// What the existing §7 day gate already does [A, read in code — day_gate.go]:
// the SAME rule, read ONCE at the 08:30 CT open with DayGateSpentPts (265,
// m465 D5.1-R18), latched as DayOff for the day. This knob is the part the
// 08:30 read cannot see: a 4h/1h conflict and/or a daily run that reach the
// rule's condition AFTER the open. From the 08:30 CT open to the 17:00 CT day
// roll, at every tick, when the 4h and 1h are opposed (HTFConflict — the same
// definition the §7 gate uses) and the daily run is at/above the knob, every
// new intraday entry is refused for the REST OF THE TRADING DAY, named
// <setup>_htf_opposed_range_halt — even if the 1h later flips back.
//
// The range is the GLOBEX DAY, 17:00 CT → now — not RTH high-low: he measures
// the current DAILY candle ("Anh chị bật qua khung daily / Anh chị đo cái nến
// hiện tại" [@18:56-18:58]) and that candle starts at the Asia open ("Khi mà
// anh chị đo cái daily / Nó cứ nghĩ là nó phải chạy từ Asia / Nó chạy qua
// pre-market" [@16:55-17:00]; "Ngày hôm qua từ lúc … thị trường châu Á mở cửa
// đó em đo … em không hề đo bên ngoài đây" [@17:27-17:50]). An RTH-only range
// is 0 at the 08:30 read, when the rule is meant to be taken. Unlike the §7
// GlobexMeasure, the window is NOT frozen at 08:30: the daily candle keeps
// growing through RTH (on his own 15 Sep tape: 264.5 at 08:30, 287.00 by the
// 14:52 low — frame part1 @18:10 measures −286.75).
//
// Why DEFAULT 0 (OFF): the citation gives a range (300-400), not one number,
// and it is September; October teaches no 4h/1h gate (Oct D4.3 @07:26-07:38
// "khung 5 phút only"). An uncited number ships as a knob that defaults OFF.
//
// Pre-open the knob does nothing: before 08:30 the §7 verdict is already LIVE
// every tick (spent + conflict refuses that minute, day_off), and his routine
// is a read taken when he opens his eyes for the session ("Buổi sáng mở mắt
// ra" [@18:39]) [B: the 08:30 start is ours, the same instant the §7 latch
// uses]. The 4h swing is exempt, as it is from the §7 DayOff (htfExempt; the
// swing reads the 4h EMA34, not the trigger lines).

// HTFRangeHalt is the per-trading-day latch of the halt. Key "" = not latched.
type HTFRangeHalt struct {
	// Key is the trading day (tradingDayKey) the halt belongs to.
	Key string `json:"key,omitempty"`
	// At is the evaluator clock of the tick that latched it.
	At int64 `json:"at,omitempty"`
	// RunPts / Hi / Lo are the Globex-day measurement that fired it.
	RunPts float64 `json:"run_pts,omitempty"`
	Hi     float64 `json:"hi,omitempty"`
	Lo     float64 `json:"lo,omitempty"`
	// FourHDir / OneHDir are the opposed directions read at that tick.
	FourHDir Side `json:"fourh_dir,omitempty"`
	OneHDir  Side `json:"oneh_dir,omitempty"`
	// Restored = the seed rebuilt it after a restart (not a live latch).
	Restored bool `json:"restored,omitempty"`
}

// HTFRangeHaltLatch is the halt latch (zero value = none).
func (s *State) HTFRangeHaltLatch() HTFRangeHalt {
	if s.HTFRangeHalt == nil {
		return HTFRangeHalt{}
	}
	return *s.HTFRangeHalt
}

// HTFRefuseOpposedRangeHalt names a refusal by the halt (per setup:
// isb_htf_opposed_range_halt, phl_htf_opposed_range_halt, box_…).
const HTFRefuseOpposedRangeHalt HTFRefusal = "htf_opposed_range_halt"

// GlobexDayMeasure measures the Globex trading day 17:00 CT → now (high − low
// of the bars that OPENED in that window), NOT frozen at 08:30. The same A6
// coverage guard as GlobexMeasure: a feed that starts more than 30 minutes
// after 17:00 under-measures the day and reads OK=false (no halt is claimed
// from it; the §7 gate is then DayNotMeasured on the same window and refuses
// every entry anyway).
func GlobexDayMeasure(bars []market.Kline, now int64, loc *time.Location) GlobexMeasurement {
	if loc == nil {
		loc = ctime()
	}
	openMs, _ := sessionBounds(now, loc)
	return measureWindow(bars, openMs, now)
}

// htfRangeHaltOpen is the 08:30 CT open of trading day key (0 if it does not
// parse).
func htfRangeHaltOpen(key string) int64 {
	return (DayGateReport{Key: key}).OpenAt()
}

// htfRangeHaltLatch is the pure halt check for one tick. It returns the latch
// (unchanged unless it fires) and whether it fired THIS tick.
func htfRangeHaltLatch(l HTFRangeHalt, h HTF, bars []market.Kline, now int64, haltPts float64) (HTFRangeHalt, bool) {
	if haltPts <= 0 {
		return l, false
	}
	key := tradingDayKey(time.UnixMilli(now).In(ctime()))
	if l.Key == key {
		return l, false // already halted for the rest of this trading day
	}
	open := htfRangeHaltOpen(key)
	if open <= 0 || now < open {
		return l, false // pre-open: the live §7 verdict governs
	}
	if !HTFConflict(h) {
		return l, false
	}
	m := GlobexDayMeasure(bars, now, ctime())
	if !m.OK || m.Run < haltPts {
		return l, false
	}
	return HTFRangeHalt{Key: key, At: now, RunPts: m.Run, Hi: m.Hi, Lo: m.Lo, FourHDir: h.FourH.Dir, OneHDir: h.OneH.Dir}, true
}

// htfRangeHaltActive reports whether the halt holds at now.
func (e *Evaluator) htfRangeHaltActive(now int64) bool {
	l := e.State.HTFRangeHaltLatch()
	return e.Cfg.HTFOpposedRangeHaltPts > 0 && l.Key != "" &&
		l.Key == tradingDayKey(time.UnixMilli(now).In(ctime()))
}

// advanceHTFRangeHalt runs the halt check on the live tick and notes/counts the
// latch once.
func (e *Evaluator) advanceHTFRangeHalt(bars []market.Kline, now int64) {
	l, fired := htfRangeHaltLatch(e.State.HTFRangeHaltLatch(), e.State.HTF, bars, now, e.Cfg.HTFOpposedRangeHaltPts)
	if !fired {
		return
	}
	e.State.HTFRangeHalt = &l
	e.count("htf_opposed_range_halt_latched")
	e.note("%s", htfRangeHaltNote(l, e.Cfg.HTFOpposedRangeHaltPts))
}

// htfRangeHaltNote is the one log line for a latch — READ values only.
func htfRangeHaltNote(l HTFRangeHalt, haltPts float64) string {
	how := "latched"
	if l.Restored {
		how = "RESTORED by the seed"
	}
	return fmt.Sprintf("HTF opposed range halt %s at %s CT: 4h %s vs 1h %s opposed, Globex day run %.2f (hi %.2f, lo %.2f) ≥ %.2f — no new mentor entries until the 17:00 CT day roll (knob htf_opposed_range_halt_pts) [Sept D5.1 p1 @19:22-19:26 \"Tắt máy nghỉ luôn cho em\"]",
		how, kernel.ClockHHMMCT(time.UnixMilli(l.At)), l.FourHDir, l.OneHDir, l.RunPts, l.Hi, l.Lo, haltPts)
}

// htfRangeHaltLeave drops every intraday ENTRY intent while the halt holds —
// the backstop where intents leave Tick, so no entry path (present or future)
// trades through it. Arm/cancel/exit actions pass. SWING4H is exempt.
func (e *Evaluator) htfRangeHaltLeave(out []Intent, now int64) []Intent {
	if !e.htfRangeHaltActive(now) {
		return out
	}
	kept := out[:0:0]
	for _, in := range out {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && !htfExempt(in) {
			e.refuse(htfRefusalName(in.Setup, HTFRefuseOpposedRangeHalt))
			continue
		}
		kept = append(kept, in)
	}
	return kept
}

// SeedHTFRangeHalt restores the halt after a restart: ONE pass over the closed
// 1m history, the 4h/1h lines replayed minute by minute from empty exactly as
// openHTFLines / TriggerReplay1m(Frozen) read them (same print-window drop or
// CE-4 freeze), and at every minute from the trading day's 08:30 open to now
// the same check the live tick runs. Knob OFF, before 08:30, or already
// latched → nothing. [B]: the replayed lines are the seed's TriggerReplay1m
// read, the one every other seed uses, not the live HTFAdvanceAt chain.
func SeedHTFRangeHalt(e *Evaluator, bars1m []market.Kline, now int64) {
	if e == nil || e.Cfg.HTFOpposedRangeHaltPts <= 0 || !e.Cfg.Enabled {
		return
	}
	key := tradingDayKey(time.UnixMilli(now).In(ctime()))
	if e.State.HTFRangeHaltLatch().Key == key {
		return
	}
	open := htfRangeHaltOpen(key)
	if open <= 0 || now < open {
		return
	}
	var frozen func(openMs int64) bool
	if e.Cfg.NewsBarsExcluded {
		frozen = func(openMs int64) bool { return newsBarFrozen(openMs, e.Cfg) }
	}
	r4 := newTriggerReplayer(TriggerLine{}, 240, e.Cfg, frozen)
	r1 := newTriggerReplayer(TriggerLine{}, 60, e.Cfg, frozen)
	closed := closedOnly(bars1m, now)
	for i, b := range closed {
		// The item-18 print-window drop (NewsBarsExcluded off): those bars
		// never reach the HTF feed; the minute is still checked.
		if e.Cfg.NewsBarsExcluded || !inPrintWindows(b.OpenTime, e.Cfg.PrintWindows) {
			r4.step(b)
			r1.step(b)
		}
		at := b.CloseTime + 1
		if at < open {
			continue
		}
		h := HTF{FourH: r4.next, OneH: r1.next}
		if !HTFConflict(h) {
			continue
		}
		if l, fired := htfRangeHaltLatch(HTFRangeHalt{}, h, closed[:i+1], at, e.Cfg.HTFOpposedRangeHaltPts); fired {
			l.Restored = true
			e.State.HTFRangeHalt = &l
			e.note("%s", htfRangeHaltNote(l, e.Cfg.HTFOpposedRangeHaltPts))
			return
		}
	}
}
