package trader

import (
	"strings"
	"testing"
	"time"

	nt "nofx/provider/ninjatrader"
	"nofx/store"
)

// Exercise the production ledger renderer before the production broker gate.
func TestArmStateGateSeparatesAuthorizationFromPlacement(t *testing.T) {
	now := time.Date(2026, 9, 9, 19, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		states  []string
		signals []string
		broker  []nt.NT8Order
		pass    bool
		text    string
	}{
		{"two authorized, empty book", []string{store.StateArmed, store.StateArmed}, []string{"", ""}, nil, true, "2 armed"},
		{"placement unconfirmed", []string{store.StatePlacePending}, []string{"sent-entry"}, nil, false, "unconfirmed"},
		{"broker-only working order", nil, nil, []nt.NT8Order{{OrderID: "broker-entry", Name: "missing-ledger", Symbol: "MNQ", State: "Working"}}, false, "broker 1 vs ledger 0"},
		{"pending identity absent", []string{store.StatePlacePending}, []string{""}, nil, false, "unconfirmed"},
		{"unknown is possible exposure", []string{"future_state"}, []string{""}, nil, false, "unconfirmed"},
		{"finished placement", []string{"superseded"}, []string{"old-signal"}, nil, true, "0 armed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := class33Trader(t)
			for i, state := range tc.states {
				class33Seed(t, at, string(rune('A'+i)), tc.signals[i], "", state)
			}
			orders, err := at.ledgerOpenOrders("MNQ")
			if err != nil {
				t.Fatal(err)
			}
			book := nt.NewOrderSnapshotCache()
			book.PutAt(nt.OrderSnapshotPayload{Account: "SimFixture", BuildID: "fixture", Orders: tc.broker}, now)
			leg := Leg4FromBrokerAt(book, "SimFixture", "MNQ", 30*time.Second, orders, now)
			if leg.Pass != tc.pass || !strings.Contains(leg.Detail, tc.text) {
				t.Fatalf("pass=%v want=%v, detail must contain %q: %+v", leg.Pass, tc.pass, tc.text, leg)
			}
			if leg.ArmedUnplaced == nil {
				t.Fatal("successful ledger read must expose authorization count")
			}
			// Labels may change without changing the ledger's classification.
			for i := range orders {
				orders[i].Status = "a translated display label"
			}
			relabelled := Leg4FromBrokerAt(book, "SimFixture", "MNQ", 30*time.Second, orders, now)
			if relabelled.Pass != leg.Pass || relabelled.Detail != leg.Detail {
				t.Fatalf("display label changed gate: before=%+v after=%+v", leg, relabelled)
			}
			if tc.name == "two authorized, empty book" {
				if *leg.ArmedUnplaced != 2 {
					t.Fatalf("authorization count=%d", *leg.ArmedUnplaced)
				}
				// Pin the API assembly too: add(4, ...) used to drop extra fields.
				apiLeg := at.CutoverGateStatus().Legs[3]
				if apiLeg.ArmedUnplaced == nil || *apiLeg.ArmedUnplaced != 2 {
					t.Fatalf("API lost informational count: %+v", apiLeg)
				}
				if line := at.store.ArmedOrders().PlacementCensusLine(); !strings.Contains(line, "0 working/unconfirmed · 2 armed") {
					t.Fatalf("boot census: %s", line)
				}
			}
		})
	}
}

func TestArmStateReadersAgreeOnFinishedRows(t *testing.T) {
	for _, state := range store.ArmStateNames() {
		if !store.IsTerminalArmState(state) {
			continue
		}
		if armedActually(123, state) {
			t.Errorf("terminal state %q must not be reported armed", state)
		}
	}
}

func TestArmStateCancelKeepsGoNormalization(t *testing.T) {
	book := []nt.NT8Order{{Name: "entry", Symbol: "MNQ", State: "Working"}}
	// EqualFold differs from ToLower for dotted capital I. Preserve the
	// original cancel predicate's normalization when using the shared table.
	if got := adjudicateArmCancel("cancel_pendİng", "entry", book, true); got.Allow {
		t.Fatalf("normalized cancellation in flight must stay refused: %+v", got)
	}
}
