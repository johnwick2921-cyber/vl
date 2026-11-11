package mentor

import "testing"

// D2-G7 + D2-G15 + D5.1-RR2-01 (oct round 2): October's only big frame is the 5m
// [OctD4.3-R9 @ 07:28 "khung 5 phút only"]; no October lesson states the 4h/1h gate
// (the FINAL report's "Oct D5.1-R9" was September's, reread D5.1-RR2-01). So the
// DEFAULT is HTFGateNewsOnly=true: the 4h/1h read gates only the print period and the
// 08:30-09:30 open. htf_gate_news_only=false restores the all-session gate.
//
// Production call site: the level PHL fixture at 10:05 CT (outside both windows)
// with a 4h long / 1h short CONFLICT: default trades, the knob at false refuses it.
func TestD2G7HTFGateOctoberFiveMinOnly(t *testing.T) {
	if !DefaultConfig().HTFGateNewsOnly {
		t.Fatal("D2-G7: HTFGateNewsOnly must default ON (October: the 5m is the only big frame, no all-session 4h/1h gate)")
	}
	four := TriggerLine{Dir: SideLong, MovedAt: auditMs(2026, 9, 15, 5, 0, 0)}
	conflict := HTF{FourH: four, OneH: TriggerLine{Dir: SideShort, MovedAt: auditMs(2026, 9, 15, 7, 0, 0)}}
	e, ins := m462PHLCfg(conflict, nil)
	if m462Entries(ins, "PHL", SideLong) != 1 || e.State.Refusals["phl_htf_conflict"] != 0 {
		t.Fatalf("default (October): a 4h/1h conflict at 10:05 CT must not stop the long PHL; intents %+v ledger %v", ins, e.State.Refusals)
	}
	e, ins = m462PHLCfg(conflict, func(c *Config) { c.HTFGateNewsOnly = false })
	if m462Entries(ins, "PHL", SideLong) != 0 || e.State.Refusals["phl_htf_conflict"] == 0 {
		t.Fatalf("knob htf_gate_news_only=false (September): refused phl_htf_conflict; intents %+v ledger %v", ins, e.State.Refusals)
	}
	// the open window still gates (the stored-true behaviour).
	if !HTFGateActive(auditMs(2026, 10, 23, 8, 45, 0), DefaultConfig()) {
		t.Fatal("the 08:30-09:30 open window keeps the gate ON")
	}
}
