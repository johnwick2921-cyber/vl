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
	// B1: migrate the positions table and seed closed rows so the day-net /
	// closed-profit seams read a deterministic session day. Rows close inside
	// the current CME session day (exit_time = now − a minute).
	if err := store.NewPositionStore(st.GormDB()).InitTables(); err != nil {
		t.Fatalf("positions migrate: %v", err)
	}
	nowMs := time.Now().UnixMilli()
	seedClosed := func(id int64, exitMs int64, realized float64, corrected *float64) {
		row := &store.TraderPosition{
			TraderID: "t-wire", Account: "Sim101", Symbol: "MNQ", Side: "LONG",
			Quantity: 1, EntryPrice: 100, ExitPrice: 100 + realized, RealizedPnL: realized,
			PnlCorrected: corrected, Status: "CLOSED", CloseReason: "sync", Source: "sync",
			EntryTime: exitMs - 60_000, ExitTime: exitMs, CreatedAt: exitMs, UpdatedAt: exitMs,
		}
		if err := st.GormDB().Create(row).Error; err != nil {
			t.Fatalf("create closed row %d: %v", id, err)
		}
	}
	plus30 := 30.0
	seedClosed(1, nowMs-60_000, 30, &plus30)
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
		mentorDayNetSource = nil
		mentorClosedProfitSource = nil
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

	// boot line: everything wired, including the B1 day-net / closed-profit
	// seams (they are in mentorSeamNames now).
	if line := mentorSeamBootLine(); !textHas(line, "open_stop=wired") ||
		!textHas(line, "day_net=wired") || !textHas(line, "closed_profit=wired") ||
		textHas(line, "=missing") {
		t.Fatalf("boot line must show every seam wired: %q", line)
	}

	// B1: day net / closed profit are bound to the strict-corrected read. With
	// one profitable close and no unresolved row, the day resolves to +30 / won.
	if net, ok := mentorDayNetSource(); !ok || net != 30 {
		t.Fatalf("day net seam = %.2f/%v, want 30/true", net, ok)
	}
	if closed, ok := mentorClosedProfitSource(); !ok || !closed {
		t.Fatalf("closed-profit seam = %v/%v, want true/true", closed, ok)
	}
	// A NULL pnl_corrected closed row today → UNRESOLVED → both seams answer
	// not-ok (fail closed), never a coerced value.
	seedClosed(2, nowMs-30_000, 100, nil)
	if net, ok := mentorDayNetSource(); ok || net != 0 {
		t.Fatalf("day net seam must fail closed on a NULL row: %.2f/%v", net, ok)
	}
	if closed, ok := mentorClosedProfitSource(); ok || closed {
		t.Fatalf("closed-profit seam must fail closed on a NULL row: %v/%v", closed, ok)
	}

	// a failing snapshot must answer not-ok, never a fabricated value.
	fake.err = errors.New("snapshot down")
	if _, ok := mentorOpenStopSource(); ok {
		t.Fatal("a snapshot error must answer not-ok")
	}
}
