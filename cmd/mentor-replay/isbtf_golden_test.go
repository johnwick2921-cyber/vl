package main

import (
	"strings"
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// refusalsAt returns the kernel refusal names one production tick added.
func refusalsAt(ts tickState) map[string]int {
	out := map[string]int{}
	for _, ev := range ts.evs {
		if ev["kind"] == "refusal" {
			r, _ := ev["reason"].(string)
			n, _ := ev["n"].(int)
			out[r] += n
		}
	}
	return out
}

// intentsAt returns the setup intents one production tick emitted.
func intentsAt(ts tickState, setup string) []Event {
	var out []Event
	for _, ev := range ts.evs {
		if ev["kind"] == "intent" && ev["setup"] == setup {
			out = append(out, ev)
		}
	}
	return out
}

// TestISBTFF1TriggerLineGovernsInsideThe5mBox — isbTF-F1, the course's own
// chart (Mon 2026-11-02, MNQ 12-26 1m from data.db, production seed at 17:00
// CT 11-01 then every closed 1m bar through mentorEvalOnce, DEFAULT config).
//
// The 09:30 5m candle (O 28,886.5 H 28,890 L 28,829.5 C 28,834.25) breaks the
// 09:25 low 28,834.25 — the 5m trigger turns SELL @28,834.25 (the mentor
// re-draws it at 28,834.00 from about 09:35: OctD4.3-R26 [Oct D4.3 @ 20:36],
// frame "Ngày 4.3 Multi Time Frame/s_01364.jpg"). Its body sits inside 09:25,
// so it is also a LONG 5m inside bar (box 28,829.5–28,890).
//
// OctD4.3-R28 [Oct D4.3 @ 22:00]: above the sell line you may NOT go long
// while the 5m still has a SELL trigger — "Tuyệt đối không được. Tại vì sao?
// Tại vì khung 5 phút vẫn đang có sell trigger". OctD4.3-R29 [@ 23:20]: a long
// only on a 5m BUY trigger. OctD4.3-R69 [@ 67:26]: a trade inside a 5m ISB
// box needs BOTH the trigger and the box. So the long 1m ISBs read on the
// 09:34 and 09:36 bars (live: entries 28,845.75 and 28,847.75, the first one
// filled and stopped for −14.75) must be refused by the trigger side. Today
// the standing box hid the trigger and the pair went on to the target read
// (isb_missing_target).
func TestISBTFF1TriggerLineGovernsInsideThe5mBox(t *testing.T) {
	got := replayFixtureDay(t, "mnq_12-26_1m_20261030-20261102_1000.csv.gz", "2026-11-02", "09:40", nil)
	for _, k := range []string{"09:34", "09:36"} {
		ts := got[k]
		if b := ts.box; b == nil || b.Dir != "long" || b.Low != 28_829.5 || b.High != 28_890 || b.Escaped != "" {
			t.Fatalf("%s tick: precondition — the LONG 09:30 5m box 28,829.5–28,890 stands, got %s", k, boxStr(b))
		}
		r := refusalsAt(ts)
		trig := 0
		for name, n := range r {
			if strings.HasPrefix(name, "isb_trigger_side") {
				trig += n
			}
		}
		if trig == 0 {
			t.Errorf("%s tick: OctD4.3-R28 — a long 1m ISB above the 5m SELL line 28,834.25 must be refused isb_trigger_side*, got refusals %v", k, r)
		}
		if r["isb_missing_target"] != 0 {
			t.Errorf("%s tick: the long ISB passed the trigger gate to the target read (isb_missing_target) — the box hid the 5m sell trigger", k)
		}
		for _, in := range intentsAt(ts, "ISB") {
			if in["side"] == "long" {
				t.Errorf("%s tick: OctD4.3-R28 — no long ISB while the 5m has a SELL trigger, got %v", k, in)
			}
		}
	}
}

// TestISBTFF2EscalationDoesNotLiftTheBoxGate — isbTF-F2 on 2026-10-07 (MNQ
// 12-26 1m from data.db, production seed + mentorEvalOnce). Pinned in the mode
// the audit found it in (mentor_5m_box_source=newest, through the production
// resolver): 08:40 is an inside bar of 08:35 (red mother, SHORT), 08:45 an
// inside bar of 08:40 (green mother, LONG) — crossing 5m ISBs. The trigger
// escalates to the 15m at the 08:49 bar (D3.4-R43), and the 08:45 pair stands
// as the LONG box 31,144.25–31,207.25. On the 08:52 bar the 1m SHORT ISB (entry
// 31,188.5, wholly inside the unbroken long box) must be refused
// isb_box_blocked: "anh chị chỉ có quyền được đánh Inside bar cùng chiều với
// khung 5 phút ... Chứ không có 5 phút đang Inside bar short vô đây đánh lệnh
// long" [OctD4.3-R70, Oct D4.3 @ 68:24]. D3.4-R43 escalates the TRIGGER only.
//
// The default (merge) keeps the 08:40 SHORT box (merged through 08:45), so the
// same short ISB is a same-direction trade there — the escalation ladder is not
// what decides it.
func TestISBTFF2EscalationDoesNotLiftTheBoxGate(t *testing.T) {
	newest := &store.StrategyConfig{}
	openOff := false // G4 (oct round 2): the golden was written reading the trigger over the whole history
	escOn := true // G3 (oct round 2): escalation is OFF by default; this test pins that it still never lifts a box gate
	newest.RiskControl.MentorTuning = &store.MentorTuning{TriggerEscalation: &escOn, TriggerFromOpen: &openOff, Mentor5mBoxSource: mentor.FiveMinBoxNewest}
	got := replayFixtureDay(t, "mnq_12-26_1m_20261005-20261007.csv.gz", "2026-10-07", "08:53", newest)
	esc := false
	for _, ev := range got["08:49"].evs {
		if ev["kind"] == "trigger_escalation" && ev["to_tf"] == 15 {
			esc = true
		}
	}
	if !esc {
		t.Fatalf("precondition: the crossing 08:40/08:45 5m ISBs escalate the trigger to the 15m on the 08:49 bar, got %v", got["08:49"].evs)
	}
	ts := got["08:52"]
	if b := ts.box; b == nil || b.Dir != mentor.SideLong || b.Low != 31_144.25 || b.High != 31_207.25 || b.Escaped != "" {
		t.Fatalf("08:52 tick: precondition — the LONG 08:45 5m box 31,144.25–31,207.25 stands unbroken, got %s", boxStr(b))
	}
	for _, in := range intentsAt(ts, "ISB") {
		if in["side"] == "short" {
			t.Errorf("08:52 tick: OctD4.3-R70 — a SHORT 1m ISB inside the standing LONG 5m box must be refused, got %v", in)
		}
	}
	if r := refusalsAt(ts); r["isb_box_blocked"] == 0 {
		t.Errorf("08:52 tick: want isb_box_blocked, got refusals %v", r)
	}
}
