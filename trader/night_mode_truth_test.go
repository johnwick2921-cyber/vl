package trader

import (
	"testing"
	"time"

	"vl/store"
)

// TestNightModeAtHonorsPerStrategyOverrides is the S2 (release #32) anchor: the
// "🌙 NIGHT MODE — no reads, no entries" log line used registry.IsNightMode,
// which reads only the registry's Enabled flag and therefore called a strategy
// "night" at 17:00 / 02:00 while an explicit sessions[].enable override kept
// ASIA/LONDON runnable. NightModeAt must judge night with the SAME resolver the
// session gates use (sessionRunnable per registry session).
//
// MUTANT: point observeNightEdge back at at.sessionRegistry(now).IsNightMode(now)
// (registry-only) → 17:00 and 02:00 read NIGHT here → RED.
func TestNightModeAtHonorsPerStrategyOverrides(t *testing.T) {
	on := true
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{
		SessionsEnabled: []string{"NY"}, // the owner saved NY-only…
		Sessions: []store.DayPlanSessionOverride{
			{Session: "ASIA", Enable: &on},   // …but an explicit override runs ASIA
			{Session: "LONDON", Enable: &on}, // …and LONDON
		},
	}}
	at, _ := resetTrader(t, cfg)

	// 14:45 CT — NY has just closed and ASIA opens at 17:00: no runnable window
	// contains now → NIGHT.
	if at.NightModeAt(time.Date(2026, 8, 18, 14, 45, 0, 0, chicagoLoc())) != true {
		t.Error("14:45 CT must be NIGHT (no runnable window active)")
	}
	// 17:00 CT — ASIA's window opened and the override runs it → DAY.
	if at.NightModeAt(time.Date(2026, 8, 18, 17, 0, 0, 0, chicagoLoc())) {
		t.Error("17:00 CT must be DAY (ASIA runnable via override)")
	}
	// 02:00 CT — LONDON's window opened and the override runs it → DAY.
	if at.NightModeAt(time.Date(2026, 8, 18, 2, 0, 0, 0, chicagoLoc())) {
		t.Error("02:00 CT must be DAY (LONDON runnable via override)")
	}
}

// TestNightModeAtInheritsRegistryOff: with NO per-session override, a session
// disabled in the registry stays night even inside its window (the subset
// sessions_enabled=["NY"] also excludes ASIA). 17:00 (ASIA) must be NIGHT.
func TestNightModeAtInheritsRegistryOff(t *testing.T) {
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{
		SessionsEnabled: []string{"NY"},
	}}
	at, _ := resetTrader(t, cfg)
	if !at.NightModeAt(time.Date(2026, 8, 18, 17, 0, 0, 0, chicagoLoc())) {
		t.Error("17:00 CT must be NIGHT when ASIA has no override and is disabled in the registry")
	}
	if at.NightModeAt(time.Date(2026, 8, 18, 10, 0, 0, 0, chicagoLoc())) {
		t.Error("10:00 CT must be DAY (NY runnable)")
	}
}
