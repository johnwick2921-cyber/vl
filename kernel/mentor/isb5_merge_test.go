package mentor

import "testing"

// TestISB5F2CrossingDoesNotLiftTheMergedBoxGate — ISB5-F2. OctD4.3-R70 [Oct
// D4.3 @ 1:08:24] "anh chị chỉ có quyền được đánh Insha bar cùng chiều với
// khung 5 phút ... Chứ không có 5 phút đang Insha bar short vô đây đánh lệnh
// long"; OctD4.3-R59 [@ 58:00] the candles that stay inside merge into ONE
// inside bar, so the box has ONE direction; OctD4.3-R9 [@ 07:28] the 5m is the
// only big frame.
//
// Tape (m60p1Crossing): 09:30 green mother, 09:35 inside → the LONG box
// 28,980–29,028; 09:40 inside 09:35 (a SHORT pair) closes 29,005 inside the
// box → merged, the box stays LONG. The last three closed 5m candles hold
// crossing ISBs. A 1m SHORT ISB inside the standing long box must be refused
// isb_box_blocked — the crossing read must not lift the R70 gate while the
// merged box stands. Trigger escalation is OFF so the box gate is the only
// refusal in play (the m60 "knob OFF" verdict: the short ISB traded).
//
// Mutant: mentor_5m_box_source=newest → the 09:40 short pair replaces the box
// (today's read) and the short ISB trades.
func TestISB5F2CrossingDoesNotLiftTheMergedBoxGate(t *testing.T) {
	bars := m60p1Crossing()
	e := m60p1CrossingEval(t, 28_950)
	e.Cfg.FiveMinBoxSource = FiveMinBoxMerge
	e.Cfg.TriggerEscalation = false
	ins := m60p1Run(e, bars)
	if !crossingISBs(closedBuckets(bars, BarCloseInstant(bars[len(bars)-1]), e.Cfg), 3) {
		t.Fatal("precondition: the last three closed 5m candles must hold crossing ISBs")
	}
	if b := e.State.ISBBox; b == nil || b.Dir != SideLong || b.AtTime != m60p1At(9, 35) || b.Low != 28_980 || b.High != 29_028 || b.Escaped != "" {
		t.Fatalf("OctD4.3-R59: want the merged LONG 09:35 box 28,980–29,028 standing, got %+v", b)
	}
	if in := m60p1ISB(ins, SideShort); in != nil {
		t.Fatalf("OctD4.3-R70: a short 1m ISB inside the standing LONG 5m box must be refused, got %+v", *in)
	}
	if m60p1LastDelta["isb_box_blocked"] == 0 {
		t.Fatalf("want isb_box_blocked on the last tick, got %v", m60p1LastDelta)
	}

	// Mutant: newest → the box turns short with the 09:40 pair; the short ISB trades.
	e = m60p1CrossingEval(t, 28_950)
	e.Cfg.FiveMinBoxSource = FiveMinBoxNewest
	e.Cfg.TriggerEscalation = false
	ins = m60p1Run(e, bars)
	if in := m60p1ISB(ins, SideShort); in == nil {
		t.Fatalf("mentor_5m_box_source=newest keeps today's verdict (the short ISB trades); last-tick refusals %v", m60p1LastDelta)
	}
}
