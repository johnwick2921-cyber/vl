package trader

import (
	"strings"
	"testing"
	"time"
	"vl/kernel"
	"vl/store"
)

func TestStructuralGeometryBootReadsPolicyAndDurableCounts(t *testing.T) {
	st := osBootStore(t, `{"structural_stop":{"buffer_points":2.25}}`)
	if err := st.GormDB().Exec(`UPDATE strategies SET config=? WHERE id=?`, `{"day_plan":{"structural_stop":{"buffer_points":2.25}},"ai_config":{"risk_control":{"min_risk_reward_ratio":2}}}`, "99-bound").Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, kernel.CTLocation())
	r := store.StructuralGeometryRecord{TraderID: "trader-1", PlanID: "P1", Version: 1, Scenario: "S1", Leg: 1, TradeDate: kernel.CMESessionDayKey(now), TimeMs: now.UnixMilli(), Reason: "rr", StopSource: "zone_edge"}
	if err := st.SaveStructuralGeometry(r); err != nil {
		t.Fatal(err)
	}
	line := StructuralGeometryBootLine(st, now, "trader-1")
	for _, s := range []string{"buffer=2.25[I]", "owner override; percentile n/a", "refused today=1", "rr<2.00=1"} {
		if !strings.Contains(line, s) {
			t.Fatalf("missing resolved %q in %s", s, line)
		}
	}
	if strings.Contains(line, "risk-cap") || strings.Contains(line, "UNSET (refuse)") {
		t.Fatalf("removed per-trade requirement leaked into boot: %s", line)
	}
	if line := StructuralGeometryBootLine(nil, now, "trader-1"); !strings.Contains(line, "n/a") {
		t.Fatal("unreadable store fabricated count")
	}
}
