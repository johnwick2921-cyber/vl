package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"vl/manager"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/trader"
	ntTrader "vl/trader/ninjatrader"
)

// rel18Seed seeds one NinjaTrader exchange (symbol MNQ), one AI model and one
// trader bound to `account`, all owned by userID. Returns the exchange id.
func rel18Seed(t *testing.T, st *store.Store, userID, traderID, name, account string) string {
	t.Helper()
	if err := st.GormDB().Create(&store.AIModel{ID: "m-" + traderID, UserID: userID, Name: "deepseek", Provider: "deepseek", Enabled: true}).Error; err != nil {
		t.Fatalf("seed ai model: %v", err)
	}
	exID := "ex-" + traderID
	if err := st.GormDB().Create(&store.Exchange{
		ID: exID, ExchangeType: "ninjatrader", AccountName: "NT8", UserID: userID,
		Name: "NinjaTrader", Type: "futures", Enabled: true,
		NTDataDir: "/tmp/nt", NTInstrumentName: "MNQ", NTDefaultContractQty: 1,
	}).Error; err != nil {
		t.Fatalf("seed exchange: %v", err)
	}
	if err := st.Trader().Create(&store.Trader{
		ID: traderID, UserID: userID, Name: name, AIModelID: "m-" + traderID,
		ExchangeID: exID, StrategyID: "s", Account: account,
	}); err != nil {
		t.Fatalf("seed trader: %v", err)
	}
	return exID
}

func rel18Server(st *store.Store, tm *manager.TraderManager) *Server {
	return &Server{store: st, traderManager: tm}
}

func rel18SelectRequest(c *gin.Context, traderID, body string) {
	c.Request = httptest.NewRequest(http.MethodPost, "/api/account/select?trader_id="+traderID, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
}

// REL18-C O1 — selecting an account on a RUNNING trader is refused with 409 and
// changes NOTHING: the DB binding and the live bound account stay put. This is
// the 00:43 live incident (UI showed the new account, the trader kept trading
// the old one).
func TestSelectAccountWhileRunningRefused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.New(filepath.Join(t.TempDir(), "o1.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rel18Seed(t, st, "u", "t1", "hoang", "Sim101")

	srv := ntwire.NewTCPServer(nil)
	tcpT := ntTrader.NewTCPTrader(srv, "MNQ", "Sim101")
	at := trader.NewAutoTraderOnBrokerForTest("t1", "ninjatrader", tcpT, true)
	tm := manager.NewTraderManager()
	tm.SetTraderForTest("t1", at)
	s := rel18Server(st, tm)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "u")
	rel18SelectRequest(c, "t1", `{"account":"SimAccount1"}`)
	s.handleSelectAccount(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("select while running = %d, want 409 (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Stop the trader first") {
		t.Fatalf("409 must carry the owner-readable stop-first message, got: %s", w.Body.String())
	}
	// Nothing persisted, no binding change.
	row, err := st.Trader().GetByID("t1")
	if err != nil || row == nil || row.Account != "Sim101" {
		t.Fatalf("DB binding must be unchanged (Sim101), got row=%+v err=%v", row, err)
	}
	if got := tcpT.BoundAccount(); got != "Sim101" {
		t.Fatalf("live bound account must be unchanged (Sim101), got %q", got)
	}
}

// REL18-C O3a — selecting an account already bound to ANOTHER trader on the same
// symbol is refused with 409 naming that trader (running or not).
func TestSelectAccountBoundToOtherTraderRefused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.New(filepath.Join(t.TempDir(), "o3a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rel18Seed(t, st, "u", "t1", "hoang", "Sim101")
	rel18Seed(t, st, "u", "t2", "12345", "SimAccount1")

	srv := ntwire.NewTCPServer(nil)
	srv.SetAccountsList([]ntwire.AccountInfo{{Name: "SimAccount1", IsSim: true}, {Name: "Sim101", IsSim: true}}, "Sim101")
	tcpT := ntTrader.NewTCPTrader(srv, "MNQ", "Sim101")
	at := trader.NewAutoTraderOnBrokerForTest("t1", "ninjatrader", tcpT, false) // stopped
	tm := manager.NewTraderManager()
	tm.SetTraderForTest("t1", at)
	s := rel18Server(st, tm)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "u")
	rel18SelectRequest(c, "t1", `{"account":"SimAccount1"}`)
	s.handleSelectAccount(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("select bound-to-other = %d, want 409 (body: %s)", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if !strings.Contains(body["error"].(string), "12345") {
		t.Fatalf("409 must name the other trader %q, got: %v", "12345", body["error"])
	}
	// The binding did not change.
	row, _ := st.Trader().GetByID("t1")
	if row == nil || row.Account != "Sim101" {
		t.Fatalf("DB binding must be unchanged, got %+v", row)
	}
}

// REL18-C O3b — starting a trader while ANOTHER RUNNING trader holds the same
// (symbol, account) is refused with 409 naming it.
func TestStartTraderDuplicateSymbolAccountRefused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.New(filepath.Join(t.TempDir(), "o3b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rel18Seed(t, st, "u", "t1", "hoang", "Sim101")
	rel18Seed(t, st, "u", "t2", "12345", "Sim101")

	at := trader.NewAutoTraderOnBrokerForTest("t1", "ninjatrader", nil, true) // running
	tm := manager.NewTraderManager()
	tm.SetTraderForTest("t1", at)
	s := rel18Server(st, tm)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "u")
	c.Params = gin.Params{{Key: "id", Value: "t2"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/traders/t2/start", nil)
	s.handleStartTrader(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("start duplicate = %d, want 409 (body: %s)", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if !strings.Contains(body["error"].(string), "t1") {
		t.Fatalf("409 must name the running trader %q, got: %v", "t1", body["error"])
	}
	// The duplicate was never marked running.
	row, _ := st.Trader().GetByID("t2")
	if row == nil || row.IsRunning {
		t.Fatalf("t2 must stay not-running, got %+v", row)
	}
}

// REL18-C — a STOPPED trader can still change its account (O1/O3a must not
// fire) and the pick persists.
func TestStoppedTraderCanChangeAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.New(filepath.Join(t.TempDir(), "stopped.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rel18Seed(t, st, "u", "t1", "hoang", "Sim101")

	srv := ntwire.NewTCPServer(nil)
	srv.SetAccountsList([]ntwire.AccountInfo{{Name: "SimAccount1", IsSim: true}, {Name: "Sim101", IsSim: true}}, "Sim101")
	tcpT := ntTrader.NewTCPTrader(srv, "MNQ", "Sim101")
	at := trader.NewAutoTraderOnBrokerForTest("t1", "ninjatrader", tcpT, false) // stopped
	tm := manager.NewTraderManager()
	tm.SetTraderForTest("t1", at)
	s := rel18Server(st, tm)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "u")
	rel18SelectRequest(c, "t1", `{"account":"SimAccount1"}`)
	s.handleSelectAccount(c)

	if w.Code != http.StatusOK {
		t.Fatalf("stopped select = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	row, _ := st.Trader().GetByID("t1")
	if row == nil || row.Account != "SimAccount1" {
		t.Fatalf("stopped select must persist the new account, got %+v", row)
	}
}
