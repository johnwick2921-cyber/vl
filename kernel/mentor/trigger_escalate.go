package mentor

import (
	"fmt"

	"vl/market"
)

// ── Trigger-line escalation 5m → 15m → 30m (m465 P1, release #60) ──────────
//
// D3.4-R43 [D3.4 p2 @17:07–17:49]: when the inside bars cross back and forth so
// the 5m buy/sell trigger is not valid, go to the 15m; if that fails, the 30m;
// then the 1h, the 4h. The whipsaw read is the PRODUCTION crossingISBs(…, 3)
// the ISB-box ladder already uses (2 opposite ISBs in the last 3 closed
// buckets — mentor question Q10 open, no new number). The 1h and 4h rungs
// already gate every intraday entry through the HTF check (HTFEntryCheck), so
// the ladder here stops at the 30m.
//
// Before #60 there was no 15m/30m trigger line at all, and while a 5m ISB box
// stood the crossing skipped the box gate AND the box hid the trigger read —
// so in exactly the chop the course escalates out of, nothing governed the side.

// governingTrigger returns the trigger line that governs this tick and its TF
// (5, 15 or 30): the 5m, or the 15m when the 5m ISBs cross, or the 30m when the
// 15m ISBs cross too. Knob mentor trigger_escalation OFF → the 5m always.
func governingTrigger(cfg Config, t5, t15, t30 TriggerLine, cb5, cb15 []market.Kline) (TriggerLine, int) {
	if !cfg.TriggerEscalation || !crossingISBs(cb5, 3) {
		return t5, 5
	}
	if !crossingISBs(cb15, 3) {
		return t15, 15
	}
	return t30, 30
}

// triggerTFSuffix names a trigger refusal by the TF that governed it: "" for the
// 5m (the pre-#60 names, byte-identical), "_15m" / "_30m" when escalated.
func triggerTFSuffix(tf int) string {
	switch tf {
	case 15:
		return "_15m"
	case 30:
		return "_30m"
	}
	return ""
}

// advanceEscalationTriggers moves the 15m and 30m trigger lines on the same 1m
// close the 5m line moves on (TriggerTick over the aggregated buckets — the
// forming bucket may fire a break once, never re-applied). Knob OFF → the
// lines are not kept (nil), so the OFF state is byte-identical to pre-#60.
func (e *Evaluator) advanceEscalationTriggers(bars []market.Kline) {
	if !e.Cfg.TriggerEscalation {
		return
	}
	t15 := TriggerTick(lineOrEmpty(e.State.Trigger15), e.barsTFMemo(bars, 15), 15, e.Cfg)
	t30 := TriggerTick(lineOrEmpty(e.State.Trigger30), e.barsTFMemo(bars, 30), 30, e.Cfg)
	e.State.Trigger15, e.State.Trigger30 = &t15, &t30
}

// seedEscalationTriggers seeds the 15m/30m lines from the closed 1m history by
// the SAME minute-by-minute replay the 5m line seeds with (TriggerReplay1m,
// LINES-RESTART), so a restart lands on the line a bot that never stopped
// holds. reset = a re-seed after a history refill: replay from an empty line.
func (e *Evaluator) seedEscalationTriggers(closed1m []market.Kline, reset bool) {
	if !e.Cfg.TriggerEscalation {
		return
	}
	t15, t30 := lineOrEmpty(e.State.Trigger15), lineOrEmpty(e.State.Trigger30)
	if reset {
		t15, t30 = TriggerLine{}, TriggerLine{}
	}
	t15 = TriggerReplay1m(t15, closed1m, 15, e.Cfg)
	t30 = TriggerReplay1m(t30, closed1m, 30, e.Cfg)
	e.State.Trigger15, e.State.Trigger30 = &t15, &t30
}

func lineOrEmpty(t *TriggerLine) TriggerLine {
	if t == nil {
		return TriggerLine{}
	}
	return *t
}

// governingTriggerTick picks this tick's governing trigger and, on a CHANGE of
// the governing TF, notes it and counts trigger_escalated_15m|30m (an
// escalation) or trigger_escalation_ended (back to the 5m) — once per change,
// never per tick.
func (e *Evaluator) governingTriggerTick(cb5, cb15 []market.Kline) (TriggerLine, int) {
	t, tf := governingTrigger(e.Cfg, e.State.Trigger, lineOrEmpty(e.State.Trigger15), lineOrEmpty(e.State.Trigger30), cb5, cb15)
	prev := e.State.TriggerGovTF
	if prev == 0 {
		prev = 5
	}
	if tf != prev {
		if tf == 5 {
			e.count("trigger_escalation_ended")
			e.note("trigger escalation ended: the 5m inside bars no longer cross — the 5m trigger governs again (%s) [D3.4-R43 p2 @17:07–17:49]", triggerDesc(e.State.Trigger))
		} else {
			e.count("trigger_escalated" + triggerTFSuffix(tf))
			e.note("trigger escalated to the %dm: crossing inside bars make the %dm trigger not valid — the %dm trigger governs (%s) [D3.4-R43 p2 @17:07–17:49]",
				tf, map[int]int{15: 5, 30: 15}[tf], tf, triggerDesc(t))
		}
	}
	if tf == 5 {
		e.State.TriggerGovTF = 0 // the pre-#60 state stays byte-identical while the 5m governs
	} else {
		e.State.TriggerGovTF = tf
	}
	return t, tf
}

func triggerDesc(t TriggerLine) string {
	if t.Dir == "" {
		return "no line yet"
	}
	return fmt.Sprintf("%s @%.2f", t.Dir, t.Price)
}
