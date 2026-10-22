package trader

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── N-I (release #22 rescan) — row 365 ──────────────────────────────────────
//
// The zone rig's TEST session runs 00:00–23:59 CT with its last-entry cutoff
// at 23:44 CT; the rig's plan provider is pinned to Thu 2026-09-11 10:00 CT.

func niCT(day time.Time, hh, mm int) time.Time {
	d := day.In(kernel.CTLocation())
	return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, kernel.CTLocation())
}

// TestArmNotAuthoredPastLastEntry — a pass after the session's last-entry
// cutoff writes NO new row (row 365 was written at 08:24:00 for an 08:15
// cutoff), and says why once.
func TestArmNotAuthoredPastLastEntry(t *testing.T) {
	r := newZoneRig(t, "ni-author", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	late := niCT(r.now, 23, 46)
	if why, past := r.at.entryBlockedByLastEntryAt(late); !past {
		t.Fatalf("fixture: %s must be past the TEST last-entry cutoff (%s)", late, why)
	}
	r.setTape(zoneTape(100.25, late, 0)) // inside the zone: the row WOULD be sendable
	r.flatBook(late)
	get := warnPlusCapture(t)
	r.at.maybeManageArmedOrdersAt(nil, late)
	r.at.maybeManageArmedOrdersAt(nil, late.Add(time.Second))
	if rows := r.rows(); len(rows) != 0 {
		t.Fatalf("N-I: past the last-entry cutoff no arm row may be written; got %+v", rows)
	}
	n := 0
	for _, l := range get() {
		if strings.Contains(l, "arm NOT authored") && strings.Contains(l, "last_entry") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("N-I: one 'arm NOT authored … last_entry' line (deduped), got %d: %v", n, get())
	}
	if c := r.armRefusals("last_entry"); c != 1 {
		t.Fatalf("N-I: the refusal is counted once, got %d", c)
	}
}

// TestZoneVerdictNotCountedPastLastEntry — a row authored before the cutoff
// sits short_of_zone; after the cutoff its verdict flips inside ↔ short twice.
// Row 365 counted five WAITING short_of_zone refusals for a row the cutoff had
// already refused: after the cutoff NOTHING more is counted as a zone verdict.
func TestZoneVerdictNotCountedPastLastEntry(t *testing.T) {
	r := newZoneRig(t, "ni-zone", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTapeReacted(99.0, r.now))   // #525 F-1: the leg waits for its reaction AT the level first
	r.at.maybeManageArmedOrdersAt(nil, r.now) // authored + WAITING short_of_zone (count 1)
	if n := r.armRefusals("market_in_zone:short_of_zone"); n != 1 {
		t.Fatalf("fixture: before the cutoff the wait is counted once, got %d", n)
	}
	late := niCT(r.now, 23, 46)
	for i, last := range []float64{100.25, 99.0, 100.25, 99.0} {
		at := late.Add(time.Duration(i) * time.Second)
		r.setTape(zoneTapeReacted(last, at))
		r.flatBook(at)
		r.at.maybeManageArmedOrdersAt(nil, at)
	}
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("past the cutoff nothing may place: %+v", sigs)
	}
	if n := r.armRefusals("market_in_zone:short_of_zone"); n != 1 {
		t.Fatalf("N-I: past the cutoff the zone verdict must not be counted again — short_of_zone count %d, want still 1", n)
	}
	if row := r.row("S1"); !strings.HasPrefix(row.LastVerdict, "refused: last_entry") {
		t.Fatalf("N-I: the row's verdict must name the cutoff, got %q", row.LastVerdict)
	}
}

// TestBandCancelNamesTheClosedSession — at 08:30:00 CT the LONDON row is
// cancelled as "session ended (LONDON closed …)", not under NY's first-5m
// band; a NY row is untouched by this step.
func TestBandCancelNamesTheClosedSession(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	at.id = "ni-close"
	reg := kernel.SessionRegistry{Sessions: []kernel.SessionDef{
		{Name: "LONDON", WindowStartCT: "02:00", WindowEndCT: "08:30", ReadCT: "01:45", FlatCT: "08:30", Enabled: true},
		{Name: "NY", WindowStartCT: "08:30", WindowEndCT: "15:00", ReadCT: "08:15", FlatCT: "15:00", Enabled: true},
	}}
	blob, _ := json.Marshal(reg)
	if err := st.SetSystemConfig(kernel.SessionRegistryConfigKey, string(blob)); err != nil {
		t.Fatal(err)
	}
	led := st.ArmedOrders()
	lon := &store.ArmedOrderDB{TraderID: at.id, PlanID: "2026-10-14:LONDON:" + at.id, Session: "LONDON", Scenario: "S1", Version: 1, State: "armed", Side: "LONG", EntryPx: 30555, StopPx: 30482.2057, TargetPx: 30700}
	ny := &store.ArmedOrderDB{TraderID: at.id, PlanID: "2026-10-14:NY:" + at.id, Session: "NY", Scenario: "S1", Version: 1, State: "armed", Side: "LONG", EntryPx: 30517, StopPx: 30427.25, TargetPx: 30720.25}
	for _, r := range []*store.ArmedOrderDB{lon, ny} {
		if err := led.UpsertArm(r); err != nil {
			t.Fatal(err)
		}
	}
	at.armedSyncSeam = &armedSyncSeam{Cancel: func(string) error { return nil },
		Stream: func() <-chan ntwire.OrderUpdatePayload { return make(chan ntwire.OrderUpdatePayload) }, Timeout: 10 * time.Millisecond}
	now := time.Date(2026, 10, 14, 8, 30, 0, 0, kernel.CTLocation())
	if !at.sessionClosedAt("LONDON", now) || at.sessionClosedAt("NY", now) {
		t.Fatalf("08:30:00: LONDON closed=%v (want true), NY closed=%v (want false)", at.sessionClosedAt("LONDON", now), at.sessionClosedAt("NY", now))
	}
	if at.sessionClosedAt("NY", time.Date(2026, 10, 14, 2, 0, 0, 0, kernel.CTLocation())) {
		t.Fatal("02:00 CT: NY is UPCOMING, never 'closed'")
	}
	at.cancelClosedSessionArms(led, now, at.windowSweepPace(now))
	rows, err := led.ListForPlan(lon.PlanID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows: %v %v", rows, err)
	}
	if rows[0].State != store.StateCancelled || !strings.Contains(rows[0].StateReason, "session ended (LONDON closed") {
		t.Fatalf("N-I: the LONDON row must be cancelled for LONDON's close, got state=%s reason=%q", rows[0].State, rows[0].StateReason)
	}
	nyRows, _ := led.ListForPlan(ny.PlanID)
	if len(nyRows) != 1 || nyRows[0].State != store.StateArmed {
		t.Fatalf("N-I: the NY row is not this step's business: %+v", nyRows)
	}
}
