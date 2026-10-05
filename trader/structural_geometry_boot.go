package trader

import (
	"fmt"
	"strings"
	"time"
	"vl/kernel"
	"vl/store"
)

func StructuralGeometryBootLine(st *store.Store, now time.Time, traderIDs ...string) string {
	cfg, _, _, _, ok := bootRiskFacts(st)
	if !ok {
		return "🎯 stop/target: bound strategy unreadable · buffer=n/a · refused today=n/a · never-widened=asserted"
	}
	p := store.ResolveStructuralStop(cfg, "MNQ")
	p.MinRR = resolvedMinRR(cfg)
	buffer, percentile := "n/a", "n/a"
	if p.BufferKnown {
		buffer = fmt.Sprintf("%.2f[I]", p.BufferPoints)
	}
	if p.Percentile > 0 {
		percentile = fmt.Sprintf("p%d", p.Percentile)
	} else {
		percentile = "owner override; percentile n/a"
	}

	counts, err := st.StructuralGeometryCounts(traderIDs, kernel.CMESessionDayKey(now))
	readable := err == nil && len(traderIDs) > 0
	fallback, refused := counts["atr_fallback"], 0
	for reason, n := range counts {
		if reason != "atr_fallback" {
			refused += n
		}
	}
	counter := "atr-fallback=n/a · refused today=n/a (records unreadable)"
	if readable {
		counter = fmt.Sprintf("atr-fallback=%d · refused today=%d (no_target=%d net<=0=%d rr<%.2f=%d no_provenance=%d other=%d)", fallback, refused, counts["no_target"], counts["net_nonpositive"], p.MinRR, counts["rr"], counts["no_provenance"], counts["invalid_geometry"]+counts["entry_gate"]+counts["one_setup"]+counts["risk_cap"]+counts["risk_cap_missing"])
	}
	return fmt.Sprintf("🎯 stop/target: stop=zone-edge+buffer buffer=%s (%s of measured overshoot; resolver=%s; calibration=%s; sweep=%v points[I]) · %s · target=first-distinct-eligible-zone · never-widened=asserted · research-candidate", buffer, percentile, strings.TrimSpace(p.BufferSource), p.Calibration, store.StructuralBufferSweepPoints(), counter)
}
