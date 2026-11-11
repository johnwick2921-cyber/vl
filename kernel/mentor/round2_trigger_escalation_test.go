package mentor

import "testing"

// G3 (oct round 2): OctD4.3-R9 [Oct D4.3 @ 07:28] "khung 5 phút only. Ở thời điểm
// hiện tại mình chỉ sử dụng khung 5 phút" - the 15m/30m trigger escalation (Sept
// D3.4-R43, m465) is not repeated anywhere in October. Default OFF: the 5m line
// governs even while the 5m inside bars cross; the 15m box (OctD4.3-R60) is unchanged.
// Production call site: the m60 crossing tape through Evaluator.Tick. Knob
// trigger_escalation=true restores the 15m governing read.
func TestG3TriggerEscalationOffByDefault(t *testing.T) {
	if DefaultConfig().TriggerEscalation {
		t.Fatal("G3: TriggerEscalation must default OFF (October: the 5m is the only big frame)")
	}
	bars := m60p1Crossing()
	e := m60p1CrossingEval(t, 28_950)
	e.Cfg.TriggerEscalation = DefaultConfig().TriggerEscalation
	m60p1Run(e, bars)
	if e.State.Counters["trigger_escalated_15m"] != 0 || e.State.Refusals["isb_trigger_side_mismatch_15m"] != 0 {
		t.Fatalf("default: no escalation to the 15m line; counters %v refusals %v", e.State.Counters, e.State.Refusals)
	}
	e = m60p1CrossingEval(t, 28_950)
	e.Cfg.TriggerEscalation = true
	m60p1Run(e, bars)
	if e.State.Counters["trigger_escalated_15m"] == 0 {
		t.Fatalf("knob ON (September): escalation counted; counters %v", e.State.Counters)
	}
}
