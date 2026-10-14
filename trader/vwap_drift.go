package trader

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// R22 P2 (PLANNER.md F4) — VWAP IS FROZEN AT PLAN WRITE. A market_in_zone arm
// whose zone came from a session-anchored moving line (VWAP, its σ bands,
// eVWAP) rests at the price that line had when the plan was WRITTEN: #650 (row
// 355, provenance frozen_line:VWAP[30798.92,…], written 21:11 CT) filled at
// 00:09 while the session VWAP sat ~20 pts higher. The owner's ruling for this
// wave is OBSERVABILITY ONLY: at placement, log the live-vs-plan drift and count
// it. The entry is never changed here. pdVWAP is a fixed prior-day value and
// cannot drift, so it is not measured.

// vwapNameRE finds a VWAP-family name inside the provenance's "+"-joined names.
var vwapNameRE = regexp.MustCompile(`(?:^|\+)(pdVWAP|eVWAP|VWAP(?:[+−]\dσ)?)(?:$|\+)`)

// vwapProvenance reads a row's zone_provenance ("<kind>:<names>[lo,hi]"). ok
// = a moving VWAP-family line is among the names. planPx is known only when
// that line is the zone's ONLY source (a frozen line: lo == hi == the line);
// a zone that mixes sources has no single plan price for the line (n/a).
func vwapProvenance(prov string) (label string, planPx float64, planKnown, ok bool) {
	j := strings.LastIndex(prov, "[")
	if j < 0 {
		return "", 0, false, false
	}
	head := prov[:j]
	names := head[strings.LastIndex(head, ":")+1:]
	for _, m := range vwapNameRE.FindAllStringSubmatch(names, -1) {
		if m[1] != "pdVWAP" {
			label, ok = m[1], true
			break
		}
	}
	if !ok {
		return "", 0, false, false
	}
	if names != label {
		return label, 0, false, true
	}
	parts := strings.Split(strings.TrimSuffix(prov[j+1:], "]"), ",")
	if len(parts) != 2 {
		return label, 0, false, true
	}
	lo, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	hi, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if e1 != nil || e2 != nil || lo <= 0 || hi <= 0 {
		return label, 0, false, true
	}
	return label, (lo + hi) / 2, true, true
}

// liveVWAPLine recomputes the named line from the pass's closed bars at now —
// the same kernel functions the level map uses. ok=false = unknown (no
// session volume yet), printed n/a, never a guessed price.
func liveVWAPLine(label string, bars []market.Kline, now time.Time) (float64, bool) {
	var lv []kernel.DetectedLevel
	if label == "eVWAP" {
		lv = kernel.EVWAPLevels(bars, now)
	} else {
		lv = kernel.SessionVWAPLevels(bars, now)
	}
	for _, l := range lv {
		if l.Label == label && l.Price > 0 {
			return l.Price, true
		}
	}
	return 0, false
}

// noteVWAPDriftAtPlacement logs + counts the drift for a placed zone row whose
// provenance is a VWAP-family line. Observability only — r.EntryPx is printed,
// never modified. Counter: vwap_drift_at_placement:<session>:<measured |
// live_unknown | mixed_source>.
func (at *AutoTrader) noteVWAPDriftAtPlacement(r store.ArmedOrderDB, bars []market.Kline, now time.Time) {
	label, planPx, planKnown, ok := vwapProvenance(r.ZoneProvenance)
	if !ok {
		return
	}
	live, liveOK := liveVWAPLine(label, bars, now)
	plan, liveTxt, drift, outcome := "n/a (zone mixes sources)", "n/a (no session volume yet)", "n/a", "live_unknown"
	if planKnown {
		plan = fmt.Sprintf("%.2f", planPx)
	}
	if liveOK {
		liveTxt = fmt.Sprintf("%.2f", live)
	}
	switch {
	case liveOK && planKnown:
		d := live - planPx
		drift, outcome = fmt.Sprintf("%+.2f pts (%.2f pts from the resting entry)", d, math.Abs(live-r.EntryPx)), "measured"
	case liveOK:
		outcome = "mixed_source"
	}
	at.logInfof("📐 VWAP drift at placement: %s %s leg %d %s frozen %s (plan v%d write) · live %s · drift %s — observability only, entry %.2f unchanged",
		r.Session, r.Scenario, r.LegIndex+1, label, plan, r.Version, liveTxt, drift, r.EntryPx)
	if at.store == nil {
		return
	}
	if _, err := store.IncSystemCounter(at.store, "vwap_drift_at_placement:"+r.Session+":"+outcome); err != nil {
		at.logWarnf("📐 vwap-drift counter write failed: %v", err)
	}
}
