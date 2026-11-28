package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"vl/manager"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// REL18-C FOLD — drive the O2 refusal through handleStartTrader: the ordered
// owner is pre-registered on the wire (not a running trader, so the O3b
// pre-check does NOT fire). Run's ordered install is refused, and the handler
// must answer 409 (not "Trader started") and leave the DB bit false.
//
// This is the production call site: UpdateStatus(true) is written BEFORE Run is
// spawned, so Run's O2 self-mark (UpdateStatus(false)) lands after and wins.
// With the OLD order (true after spawn) this test is RED: the DB bit ends true.
func TestStartTraderOrderedOwnerConflictReturns409AndNotRunning(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// A wire with a live ordered owner for (MNQ, Sim101), held by a trader that
	// is NOT marked running — so O3b passes and only the install refuses.
	t.Setenv(ntTrader.TransportEnvVar, "tcp")
	srv := ntwire.NewTCPServer(nil)
	if _, err := srv.RegisterOrderedExecutionsFor("MNQ", "Sim101", ntwire.OrderedExecutionHandlers{TraderID: "other-owner"}); err != nil {
		t.Fatalf("pre-register ordered owner: %v", err)
	}
	ntTrader.SetTCPServerForTest(srv)

	st, err := store.New(filepath.Join(t.TempDir(), "fold-start.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeStoreAndWaitForConns(t, st) })

	if err := st.GormDB().Create(&store.AIModel{ID: "m", UserID: "u", Name: "deepseek", Provider: "deepseek", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.GormDB().Create(&store.Exchange{
		ID: "ex", ExchangeType: "ninjatrader", AccountName: "NT8", UserID: "u",
		Name: "NinjaTrader", Type: "futures", Enabled: true,
		NTDataDir: "/tmp/nt", NTInstrumentName: "MNQ", NTDefaultContractQty: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.Strategy().Create(&store.Strategy{ID: "s", UserID: "u", Name: "s", Config: "{}"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Trader().Create(&store.Trader{
		ID: "t2", UserID: "u", Name: "second", AIModelID: "m", ExchangeID: "ex",
		StrategyID: "s", Account: "Sim101", InitialBalance: 50000, IsRunning: false,
	}); err != nil {
		t.Fatal(err)
	}

	s := &Server{store: st, traderManager: manager.NewTraderManager()}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "u")
	c.Params = gin.Params{{Key: "id", Value: "t2"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/traders/t2/start", nil)
	s.handleStartTrader(c)

	if w.Code == http.StatusOK {
		t.Fatalf("start must NOT be 200 when the ordered install is refused; got %d (body: %s)", w.Code, w.Body.String())
	}
	row, err := st.Trader().GetByID("t2")
	if err != nil || row == nil || row.IsRunning {
		t.Fatalf("refused start must leave is_running=false, got row=%+v err=%v", row, err)
	}
}

// closeStoreAndWaitForConns closes the store and then waits until every
// SQLite connection to it has really closed, so the TempDir that holds the
// database is removed only once nothing touches it any more.
//
// m468 (flake, CI run 41773933846, 11-03): loading the trader starts the
// NT8 background workers (level stats, bar repair, data-truth warmer, picture
// rows), and they keep querying this store after the test body returns.
// Store.Close does not wait for a connection that is in use or still being
// opened; that connection closes a moment later, and SQLite then deletes or
// creates its -wal/-shm files. When that lands while TempDir's RemoveAll is
// running, the cleanup fails with "unlinkat …/001: directory not empty".
// After Close no new query can start, so OpenConnections only goes down;
// at zero the files are settled and the directory can be removed safely.
func closeStoreAndWaitForConns(t *testing.T, st *store.Store) {
	t.Helper()
	_ = st.Close()
	db := st.DB()
	if db == nil {
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	for db.Stats().OpenConnections > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := db.Stats().OpenConnections; n > 0 {
		t.Errorf("%d SQLite connection(s) still open 10s after Store.Close — a background worker is holding the test store, and removing its TempDir would race it", n)
	}
}
