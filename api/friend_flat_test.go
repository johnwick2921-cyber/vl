package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"vl/manager"
	"vl/store"
	trader "vl/trader"
)

// W-FRIEND-HOSTED (CTO ruling 2026-10-18 07:36): GET /api/friend/flat answers
// {flat, at} in friend mode only, fail-closed on a position-read error.

type fakeFlatTrader struct {
	trader.Trader // embedded nil interface: only GetPositions/GetOpenOrders/FriendStateKnown are exercised
	positions     []map[string]interface{}
	err           error
	orders        []trader.OpenOrder
	ordersErr     error
	known         bool // FriendStateKnown answer (R9c: connected + fresh)
}

func (f fakeFlatTrader) GetPositions() ([]map[string]interface{}, error) {
	return f.positions, f.err
}

func (f fakeFlatTrader) GetOpenOrders(string) ([]trader.OpenOrder, error) {
	return f.orders, f.ordersErr
}

func (f fakeFlatTrader) FriendStateKnown(time.Time, time.Duration) bool {
	return f.known
}

func newFriendFlatServer(t *testing.T, friend bool, brokers map[string]trader.Trader) *Server {
	t.Helper()
	prev := friendModeResolver
	t.Cleanup(func() { friendModeResolver = prev })
	friendModeResolver = func() bool { return friend }

	st, err := store.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	tm := manager.NewTraderManager()
	for id, b := range brokers {
		tm.SetTraderForTest(id, trader.NewAutoTraderOnBrokerForTest(id, "ninjatrader", b, true))
	}
	return NewServer(tm, st, nil, "127.0.0.1", 0)
}

func getFriendFlat(t *testing.T, s *Server) (int, map[string]interface{}) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/friend/flat", nil)
	s.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		return w.Code, nil // a 404 page is not JSON
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, w.Body.String())
	}
	return w.Code, body
}

func TestFriendFlatOwnerMode404(t *testing.T) {
	s := newFriendFlatServer(t, false, nil)
	code, _ := getFriendFlat(t, s)
	if code != http.StatusNotFound {
		t.Fatalf("owner mode /api/friend/flat = %d, want 404 (route not registered)", code)
	}
}

func TestFriendFlatEmptyTraders(t *testing.T) {
	s := newFriendFlatServer(t, true, nil)
	code, body := getFriendFlat(t, s)
	if code != http.StatusOK {
		t.Fatalf("friend mode empty = %d, want 200", code)
	}
	if body["flat"] != true {
		t.Fatalf("flat = %v, want true (no traders)", body["flat"])
	}
	if at, ok := body["at"].(float64); !ok || at <= 0 {
		t.Fatalf("at = %v, want a positive unix-ms number", body["at"])
	}
}

func TestFriendFlatNonFlatPosition(t *testing.T) {
	s := newFriendFlatServer(t, true, map[string]trader.Trader{
		// AutoTrader.GetPositions repackages from the broker's camelCase
		// "positionAmt" (the TCPTrader emits both shapes; this is the one it reads).
		"t1": fakeFlatTrader{positions: []map[string]interface{}{{"positionAmt": float64(2), "entryPrice": float64(30000)}}},
	})
	_, body := getFriendFlat(t, s)
	if body["flat"] != false {
		t.Fatalf("flat = %v, want false (open position)", body["flat"])
	}
}

func TestFriendFlatProbeErrorFailsClosed(t *testing.T) {
	s := newFriendFlatServer(t, true, map[string]trader.Trader{
		"t1": fakeFlatTrader{err: errors.New("probe down")},
	})
	_, body := getFriendFlat(t, s)
	if body["flat"] != false {
		t.Fatalf("flat = %v, want false (probe error must fail closed)", body["flat"])
	}
}

func TestFriendFlatWorkingOrderNotFlat(t *testing.T) {
	s := newFriendFlatServer(t, true, map[string]trader.Trader{
		"t1": fakeFlatTrader{known: true, orders: []trader.OpenOrder{{OrderID: "w1", Symbol: "MNQ", Type: "LIMIT"}}},
	})
	_, body := getFriendFlat(t, s)
	if body["flat"] != false {
		t.Fatalf("flat = %v, want false (a working/pending entry order must block restart)", body["flat"])
	}
}

func TestFriendFlatRestingBracketNotFlat(t *testing.T) {
	s := newFriendFlatServer(t, true, map[string]trader.Trader{
		// a resting protective bracket (stop) with no entry order: still blocks.
		"t1": fakeFlatTrader{known: true, orders: []trader.OpenOrder{{OrderID: "sl1", Symbol: "MNQ", Type: "STOP_MARKET"}}},
	})
	_, body := getFriendFlat(t, s)
	if body["flat"] != false {
		t.Fatalf("flat = %v, want false (a resting bracket order must block restart)", body["flat"])
	}
}

func TestFriendFlatOrdersErrorFailsClosed(t *testing.T) {
	s := newFriendFlatServer(t, true, map[string]trader.Trader{
		"t1": fakeFlatTrader{known: true, ordersErr: errors.New("order book down")},
	})
	_, body := getFriendFlat(t, s)
	if body["flat"] != false {
		t.Fatalf("flat = %v, want false (order-book error must fail closed)", body["flat"])
	}
}

func TestFriendFlatDisconnectedNotFlat(t *testing.T) {
	// FriendStateKnown=false == the NT8 link is down / snapshots absent.
	s := newFriendFlatServer(t, true, map[string]trader.Trader{
		"t1": fakeFlatTrader{known: false},
	})
	_, body := getFriendFlat(t, s)
	if body["flat"] != false {
		t.Fatalf("flat = %v, want false (NT disconnected must not read flat)", body["flat"])
	}
}

func TestFriendFlatStaleSnapshotNotFlat(t *testing.T) {
	// FriendStateKnown=false also covers a stale snapshot (the TCPTrader refuses
	// any snapshot older than friendSnapshotMaxAge).
	s := newFriendFlatServer(t, true, map[string]trader.Trader{
		"t1": fakeFlatTrader{known: false},
	})
	_, body := getFriendFlat(t, s)
	if body["flat"] != false {
		t.Fatalf("flat = %v, want false (stale snapshot must not read flat)", body["flat"])
	}
}

func TestFriendFlatAllClear(t *testing.T) {
	s := newFriendFlatServer(t, true, map[string]trader.Trader{
		"t1": fakeFlatTrader{known: true},
	})
	_, body := getFriendFlat(t, s)
	if body["flat"] != true {
		t.Fatalf("flat = %v, want true (no position, no order, state known + fresh)", body["flat"])
	}
}

func TestFlatFromPositions(t *testing.T) {
	cases := []struct {
		name      string
		positions []map[string]interface{}
		want      bool
	}{
		{"empty", nil, true},
		{"zero qty", []map[string]interface{}{{"quantity": float64(0)}}, true},
		{"missing qty key", []map[string]interface{}{{"symbol": "MNQ"}}, true},
		{"non-zero qty", []map[string]interface{}{{"quantity": float64(1)}}, false},
		{"one flat one open", []map[string]interface{}{{"quantity": float64(0)}, {"quantity": float64(3)}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := flatFromPositions(c.positions); got != c.want {
				t.Fatalf("flatFromPositions = %v, want %v", got, c.want)
			}
		})
	}
}
