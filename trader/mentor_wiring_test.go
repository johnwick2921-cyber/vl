package trader

import (
	"errors"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
	"vl/trader/types"
)

// fakeMentorPositionReader is a minimal types.Trader slice: the bound
// account's position snapshot + the working (bracket) orders. The embedded
// nil interface satisfies the rest; only the two wiring methods are used.
type fakeMentorPositionReader struct {
	types.Trader
	positions []map[string]interface{}
	orders    []types.OpenOrder
	err       error
}

func (f *fakeMentorPositionReader) GetPositions() ([]map[string]interface{}, error) {
	return f.positions, f.err
}

func (f *fakeMentorPositionReader) GetOpenOrders(symbol string) ([]types.OpenOrder, error) {
	return f.orders, f.err
}

// TestMentorProductionWiring builds the PRODUCTION wiring (mentorWireProductionSeams,
// not test injection) and asserts every seam is non-nil and returns a value from
// the fake NT8 snapshot. Mutant: drop ONE binding → the seam stays nil and this
// test goes RED.
func TestMentorProductionWiring(t *testing.T) {
	st := mentorSeedStore(t)
	fake := &fakeMentorPositionReader{
		positions: []map[string]interface{}{
			{"symbol": "MNQ", "side": "SHORT", "quantity": 1.0},
		},
		orders: []types.OpenOrder{
			{OrderID: "b-1", Symbol: "MNQ", Type: "STOP_MARKET", StopPrice: 105.5, Quantity: 1},
		},
	}
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline {
		return []market.Kline{{OpenTime: 1, Close: 100.25, CloseTime: 60000}}
	}
	t.Cleanup(func() { market.FuturesBarsProvider = nil })

	at := &AutoTrader{id: "t-wire", trader: fake, store: st}
	at.mentorWireProductionSeams()
	t.Cleanup(func() {
		mentorOpenStopSource = nil
		mentorOpenSideSource = nil
		mentorLegProtectedSource = nil
		mentorLatestPriceSource = nil
		mentorNowSource = nil
		mentorConfluenceForIntent = nil
		mentorSetArmExpiryWire = nil
	})

	// every seam bound and answering from the fake snapshot.
	if missing := mentorSeamMissing(); len(missing) != 0 {
		t.Fatalf("no seam may be missing after production wiring: %v", missing)
	}
	px, ok := mentorOpenStopSource()
	if !ok || px != 105.5 {
		t.Fatalf("open stop = %.2f/%v, want 105.5/true", px, ok)
	}
	if side := mentorOpenSideSource(); side != "short" {
		t.Fatalf("open side = %q, want short", side)
	}
	if !mentorLegProtectedSource("leg2") {
		t.Fatal("leg protection must report the working bracket stop")
	}
	if px, ok := mentorLatestPriceSource(); !ok || px != 100.25 {
		t.Fatalf("latest price = %.2f/%v, want 100.25/true", px, ok)
	}
	before := time.Now()
	now := mentorNowSource()
	if now.Before(before) || now.After(time.Now().Add(2*time.Second)) {
		t.Fatalf("now seam not the wall clock: %v", now)
	}
	if !mentorConfluenceForIntent(mentor.Intent{Confluence: true}) ||
		mentorConfluenceForIntent(mentor.Intent{}) {
		t.Fatal("confluence seam must read the intent's Confluence flag")
	}

	// arm expiry: a real armed row gets the stamp (store.SetArmExpiry).
	if err := st.ArmedOrders().Migrate(); err != nil {
		t.Fatalf("armed_orders migrate: %v", err)
	}
	arm := &store.ArmedOrderDB{
		TraderID: "t-wire", PlanID: "wire:plan", Version: 1, Session: "RTH",
		Scenario: "S1", Side: "long", EntryPx: 100, StopPx: 99, TargetPx: 102,
		State: store.StateArmed, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatalf("upsert arm: %v", err)
	}
	if err := mentorSetArmExpiryWire(arm.ID, 1_000_000); err != nil {
		t.Fatalf("SetArmExpiry on the armed row: %v", err)
	}
	if err := mentorSetArmExpiryWire(arm.ID+9999, 1_000_000); err == nil {
		t.Fatal("SetArmExpiry on a missing row must error, never silent")
	}
	if err := mentorSetArmExpiryWire(arm.ID, 0); err == nil {
		t.Fatal("a non-positive expiry must be refused")
	}

	// boot line: everything wired.
	if line := mentorSeamBootLine(); !textHas(line, "open_stop=wired") || textHas(line, "=missing") {
		t.Fatalf("boot line must show every seam wired: %q", line)
	}

	// a failing snapshot must answer not-ok, never a fabricated value.
	fake.err = errors.New("snapshot down")
	if _, ok := mentorOpenStopSource(); ok {
		t.Fatal("a snapshot error must answer not-ok")
	}
}
