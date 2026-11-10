package main

import (
	"strings"
	"testing"
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
