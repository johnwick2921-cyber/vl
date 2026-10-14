package ninjatrader

import (
	"strings"
	"testing"
	"time"

	"vl/store"
	"vl/telemetry"
)

// R22 PNL-TRUTH (owner ruling R1) — an NT8 position the reconcile loop ADOPTS
// with no originating bot order/arm is flagged origin=manual AT ADOPTION,
// logged and counted, so it never enters the bot record. Every other verdict
// (a tagged fill, a price match, an untagged/ambiguous answer) stays bot —
// fail-closed. Driven through the production reconcilePositions call site.
func TestReconcileFlagsOnlyNoEvidenceAdoptionsManual(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(t *testing.T, w *lateFillWire)
		manual bool
	}{
		{name: "no evidence at all", setup: func(t *testing.T, w *lateFillWire) {}, manual: true},
		{
			name: "only an arm older than the window",
			setup: func(t *testing.T, w *lateFillWire) {
				w.filledArm(t, "2026-09-22:NY", "S1", "sig-armed-yesterday", 29001, 24*time.Hour)
			},
			manual: true,
		},
		{
			name: "late armed fill",
			setup: func(t *testing.T, w *lateFillWire) {
				w.filledArm(t, "2026-09-23:NY", "S2", "sig-arm-mine", 29001, 90*time.Second)
				w.fill(t, "sig-arm-mine", "long", 29001)
			},
		},
		{
			name: "armed fill matched by price in the window",
			setup: func(t *testing.T, w *lateFillWire) {
				w.filledArm(t, "2026-09-23:NY", "S3", "sig-armed", 29001, 90*time.Second)
			},
		},
		{
			name: "ambiguous ring",
			setup: func(t *testing.T, w *lateFillWire) {
				w.aiOrderRow(t, lateFillTrader, "sig-ai-a", "open_long", "LONG", "BUY")
				w.aiOrderRow(t, lateFillTrader, "sig-ai-b", "open_long", "LONG", "BUY")
				w.fill(t, "sig-ai-a", "long", 29001)
				w.fill(t, "sig-ai-b", "long", 29001)
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			w := newLateFillWire(t)
			warns := captureLateWarns(t)
			tc.setup(t, w)
			flaggedBefore, _ := telemetry.ManualAdoptionCounts()
			row := w.materialize(t, 29001)
			flaggedAfter, _ := telemetry.ManualAdoptionCounts()
			want := store.OriginBot
			if tc.manual {
				want = store.OriginManual
			}
			if row.Origin != want {
				t.Fatalf("adopted row origin = %q, want %q", row.Origin, want)
			}
			if tc.manual && flaggedAfter != flaggedBefore+1 {
				t.Fatalf("manual adoption counter %d → %d, want +1", flaggedBefore, flaggedAfter)
			}
			if !tc.manual && flaggedAfter != flaggedBefore {
				t.Fatalf("a %s bumped the manual adoption counter %d → %d", tc.name, flaggedBefore, flaggedAfter)
			}
			if tc.manual {
				found := false
				warns.mu.Lock()
				for _, l := range warns.lines {
					if strings.Contains(l, "flagged origin=manual") {
						found = true
					}
				}
				warns.mu.Unlock()
				if !found {
					t.Fatal("the manual flag must be LOGGED (🧾 manual origin … flagged origin=manual)")
				}
			}
		})
	}
}

