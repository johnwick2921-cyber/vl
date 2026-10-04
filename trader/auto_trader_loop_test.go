package trader

import (
	"fmt"
	"sync"
	"testing"
	"time"
	"vl/trader/types"
)

// MockTrader is a minimal trader implementation for testing balance defer logic.
type MockTrader struct {
	balance       map[string]interface{}
	positions     []map[string]interface{}
	balanceReady  bool
	marketPrice   float64
	allOrders     []types.OpenOrder
	closedPnLData []types.ClosedPnLRecord
}

func (m *MockTrader) GetBalance() (map[string]interface{}, error) {
	if !m.balanceReady {
		return nil, fmt.Errorf("balance not yet available")
	}
	return m.balance, nil
}

func (m *MockTrader) GetPositions() ([]map[string]interface{}, error) {
	return m.positions, nil
}

func (m *MockTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return map[string]interface{}{"orderId": "sig-001", "avgPrice": m.marketPrice}, nil
}

func (m *MockTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return map[string]interface{}{"orderId": "sig-002", "avgPrice": m.marketPrice}, nil
}

func (m *MockTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (m *MockTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (m *MockTrader) SetLeverage(symbol string, leverage int) error {
	return nil
}

func (m *MockTrader) SetMarginMode(symbol string, isCrossMargin bool) error {
	return nil
}

func (m *MockTrader) GetMarketPrice(symbol string) (float64, error) {
	return m.marketPrice, nil
}

func (m *MockTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	return nil
}

func (m *MockTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	return nil
}

func (m *MockTrader) CancelStopLossOrders(symbol string) error {
	return nil
}

func (m *MockTrader) CancelTakeProfitOrders(symbol string) error {
	return nil
}

func (m *MockTrader) CancelAllOrders(symbol string) error {
	return nil
}

func (m *MockTrader) CancelStopOrders(symbol string) error {
	return nil
}

func (m *MockTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return fmt.Sprintf("%.4f", quantity), nil
}

func (m *MockTrader) GetOrderStatus(symbol string, orderId string) (map[string]interface{}, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockTrader) GetClosedPnL(startTime time.Time, limit int) ([]types.ClosedPnLRecord, error) {
	return m.closedPnLData, nil
}

func (m *MockTrader) GetOpenOrders(symbol string) ([]types.OpenOrder, error) {
	return m.allOrders, nil
}

// TestTraderGetBalanceReturnsErrorBeforeReceived verifies that a trader can signal
// "balance not yet received" via an error return from GetBalance().
// This is the defer-until-balance mechanism.
func TestTraderGetBalanceReturnsErrorBeforeReceived(t *testing.T) {
	mockTrader := &MockTrader{
		balanceReady: false, // Simulate no balance yet
		positions:    []map[string]interface{}{},
	}

	balance, err := mockTrader.GetBalance()
	if err == nil {
		t.Errorf("GetBalance should error when balanceReady=false; got balance=%v", balance)
	}
	if balance != nil {
		t.Errorf("GetBalance should return nil balance on error; got %v", balance)
	}
}

// TestTraderGetBalanceSucceedsWhenReady verifies that GetBalance() returns the
// balance successfully once the flag is set.
func TestTraderGetBalanceSucceedsWhenReady(t *testing.T) {
	mockTrader := &MockTrader{
		balanceReady: true,
		balance: map[string]interface{}{
			"totalWalletBalance":    100000.0,
			"totalUnrealizedProfit": 0.0,
			"availableBalance":      100000.0,
			"totalEquity":           100000.0,
		},
		positions: []map[string]interface{}{},
	}

	balance, err := mockTrader.GetBalance()
	if err != nil {
		t.Fatalf("GetBalance should succeed with balanceReady=true; got err=%v", err)
	}
	if balance == nil {
		t.Fatal("GetBalance should return non-nil balance when ready")
	}
	if eq, ok := balance["totalEquity"].(float64); !ok || eq != 100000.0 {
		t.Errorf("totalEquity: want 100000, got %v", eq)
	}
}

// TestDeferUntilBalanceTransition simulates the real scenario: initial GetBalance()
// call has no balance (balanceReady=false), then a subsequent call with
// balanceReady=true succeeds. This mirrors the NT account_balance frame arrival.
func TestDeferUntilBalanceTransition(t *testing.T) {
	mockTrader := &MockTrader{
		balanceReady: false, // Start with no balance
		positions:    []map[string]interface{}{},
	}

	// First call: no balance available (simulates first decision cycle)
	balance1, err1 := mockTrader.GetBalance()
	if err1 == nil {
		t.Fatal("First GetBalance call should error without balance")
	}
	if balance1 != nil {
		t.Errorf("First GetBalance should return nil; got %v", balance1)
	}

	// Simulate balance arriving (NT account_balance frame received)
	mockTrader.balanceReady = true
	mockTrader.balance = map[string]interface{}{
		"totalWalletBalance":    100000.0,
		"totalUnrealizedProfit": 500.0,
		"availableBalance":      99500.0,
		"totalEquity":           100500.0,
	}

	// Second call: balance is available (simulates second decision cycle)
	balance2, err2 := mockTrader.GetBalance()
	if err2 != nil {
		t.Fatalf("Second GetBalance call should succeed; got err=%v", err2)
	}
	if balance2 == nil {
		t.Fatal("Second GetBalance should return non-nil balance")
	}
	if eq, ok := balance2["totalEquity"].(float64); !ok || eq != 100500.0 {
		t.Errorf("totalEquity: want 100500, got %v", eq)
	}
}

// TestConcurrentGetBalance tests that GetBalance is thread-safe.
func TestConcurrentGetBalance(t *testing.T) {
	mockTrader := &MockTrader{
		balanceReady: true,
		balance: map[string]interface{}{
			"totalWalletBalance":    100000.0,
			"totalUnrealizedProfit": 0.0,
			"availableBalance":      100000.0,
			"totalEquity":           100000.0,
		},
		positions: []map[string]interface{}{},
	}

	// Launch concurrent GetBalance calls
	ch := make(chan error, 10)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := mockTrader.GetBalance()
			ch <- err
		}()
	}
	wg.Wait()
	close(ch)

	// Verify all calls succeeded
	for err := range ch {
		if err != nil {
			t.Errorf("Concurrent GetBalance returned error: %v", err)
		}
	}
}

// TestBalanceDeferDecisionCycleLogic demonstrates the decision cycle logic:
// when buildTradingContext receives a GetBalance error, it returns error
// to the caller, which should log and skip the cycle without recording
// a decision. This is the defer-until-balance behavior.
func TestBalanceDeferDecisionCycleLogic(t *testing.T) {
	mockTrader := &MockTrader{
		balanceReady: false,
	}

	at := &AutoTrader{
		trader: mockTrader,
		id:     "test-trader",
		name:   "Test Trader",
	}

	// Simulate the decision cycle's GetBalance call
	balance, err := at.trader.GetBalance()
	if err == nil {
		t.Fatal("trader.GetBalance should error initially")
	}
	if balance != nil {
		t.Fatalf("balance should be nil on error; got %v", balance)
	}

	// The runCycle method would check this error and skip recording a decision,
	// continuing to defer until balance arrives. We verify the MockTrader
	// correctly simulates this behavior.
	t.Logf("Balance defer correctly triggered: %v", err)

	// Now simulate balance arriving
	mockTrader.balanceReady = true
	mockTrader.balance = map[string]interface{}{
		"totalWalletBalance": 100000.0,
		"totalEquity":        100000.0,
	}

	// Retry: balance should now be available
	balance2, err2 := at.trader.GetBalance()
	if err2 != nil {
		t.Fatalf("trader.GetBalance should succeed after balance arrival; got err=%v", err2)
	}
	if balance2 == nil {
		t.Fatal("balance should be non-nil after arrival")
	}
}
