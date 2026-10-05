package store

import (
	"os"
	"strings"
	"testing"
)

// TestSplitUpsertArmReauthCarriesSplitLegs pins P2-5: BOTH re-authorization
// update maps in UpsertArm carry contracts/leg1_qty/leg1_tp, so a re-authorized
// armed row does not silently revert its split to a single bracket. Mutant: drop
// the three keys from either map → RED.
func TestSplitUpsertArmReauthCarriesSplitLegs(t *testing.T) {
	b, err := os.ReadFile("armed_orders.go")
	if err != nil {
		t.Fatalf("cannot read armed_orders.go: %v", err)
	}
	src := string(b)
	want := `"contracts": row.Contracts, "leg1_qty": row.Leg1Qty, "leg1_tp": row.Leg1TP`
	if n := strings.Count(src, want); n != 2 {
		t.Fatalf("UpsertArm must carry the split legs in BOTH re-auth maps (%s), found %d", want, n)
	}
}
