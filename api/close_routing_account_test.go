package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"vl/logger"
	"vl/manager"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/trader"
	ntTrader "vl/trader/ninjatrader"
)

// ── r24 AC-5 / AC-4 — THE API CLOSE PATHS STAMP THE TRADER'S BOUND ACCOUNT ───
//
// Two MNQ traders on two SIM accounts share ONE AddOn link (the live 10-14
// topology: t1 → Sim101, t2 → SimAccount1). From AddOn build 2026-10-14-a the
// AddOn flattens only the account a close_position frame names, so the frame
// IS the routing. Each test calls the production handler (handleForceFlat =
// POST /api/risk/force-flat, handleClosePosition = Positions → Close) for t2
// and reads the frame the fake AddOn received.
//
// Scenario (a) of rescan N-1: t1 holds MNQ, t2 is flat, the owner presses
// Emergency Flat on t2. The handler sends a close even with 0 positions —
// that frame must name SimAccount1, never Sim101.
//
// MUTANT (L4, quoted in the PR): drop `Account: t.boundAccount` from
// trader/ninjatrader/tcp_trader.go sendCloseAt → RED here and in
// trader/close_routing_account_test.go.

type crAPIWire struct {
	srv    *ntwire.TCPServer
	conn   net.Conn
	mu     sync.Mutex
	closes []ntwire.ClosePositionPayload
}

func (w *crAPIWire) read() {
	for {
		env, err := ntwire.ReadFrame(w.conn)
		if err != nil {
			return
		}
		if env.Type != ntwire.FrameClosePosition {
			continue
		}
		var p ntwire.ClosePositionPayload
		if json.Unmarshal(env.Payload, &p) == nil {
			w.mu.Lock()
			w.closes = append(w.closes, p)
			w.mu.Unlock()
		}
	}
}

func (w *crAPIWire) waitCloses(t *testing.T, n int) []ntwire.ClosePositionPayload {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		w.mu.Lock()
		got := append([]ntwire.ClosePositionPayload(nil), w.closes...)
		w.mu.Unlock()
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d close_position frame(s); got %+v", n, got)
		}
		time.Sleep(time.Millisecond)
	}
}

// crAPIRig: one store with t1 (Sim101) and t2 (SimAccount1) owned by user u,
// one TCP server with BOTH accounts SIM, one fake AddOn, both traders running
// in the manager's roster. t1 holds MNQ; t2 is flat (scenario a).
func crAPIRig(t *testing.T) (*Server, *crAPIWire) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	st, err := store.New(filepath.Join(t.TempDir(), "close-routing-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rel18Seed(t, st, "u", "t1", "hoang", "Sim101")
	rel18Seed(t, st, "u", "t2", "12345", "SimAccount1")

	srv := ntwire.NewTCPServer(nil)
	srv.SetAddrForTest("127.0.0.1:0")
	srv.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}, {Name: "SimAccount1", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop(); cancel() })
	conn, err := net.Dial("tcp", srv.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	for deadline := time.Now().Add(5 * time.Second); !srv.IsConnected(); {
		if time.Now().After(deadline) {
			t.Fatal("fixture: the server never registered the fake AddOn")
		}
		time.Sleep(time.Millisecond)
	}
	w := &crAPIWire{srv: srv, conn: conn}
	go w.read()
	srv.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "long", Quantity: 1, AvgPrice: 30000}})
	srv.SeedPositionsForTest("SimAccount1", []ntwire.OpenPosition{})

	tm := manager.NewTraderManager()
	for _, b := range []struct{ id, acct string }{{"t1", "Sim101"}, {"t2", "SimAccount1"}} {
		tcpT := ntTrader.NewTCPTrader(srv, "MNQ", b.acct)
		tcpT.StartCloseSync(b.id, "ex-"+b.id, "ninjatrader", st) // stamps trader_id on outbound frames
		tm.SetTraderForTest(b.id, trader.NewAutoTraderOnBrokerForTest(b.id, "ninjatrader", tcpT, true))
	}
	return &Server{store: st, traderManager: tm}, w
}

func crAPIMustOnly(t *testing.T, path string, got []ntwire.ClosePositionPayload) {
	t.Helper()
	for _, p := range got {
		if p.Account != "SimAccount1" || p.TraderID != "t2" {
			t.Fatalf("%s: close_position for t2 carries account=%q trader_id=%q — want SimAccount1/t2; the AddOn would flatten t1's Sim101 position (rescan N-1): %+v", path, p.Account, p.TraderID, p)
		}
	}
}

// crWarnLines captures every WARN+ line logged while the test runs.
type crWarnLines struct {
	mu    sync.Mutex
	lines []string
}

func (h *crWarnLines) Levels() []logrus.Level {
	return []logrus.Level{logrus.WarnLevel, logrus.ErrorLevel}
}

func (h *crWarnLines) Fire(e *logrus.Entry) error {
	h.mu.Lock()
	h.lines = append(h.lines, e.Message)
	h.mu.Unlock()
	return nil
}

func (h *crWarnLines) all() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.lines...)
}

func captureAPIWarns(t *testing.T) *crWarnLines {
	t.Helper()
	prev := logrus.LevelHooks{}
	for lvl, hs := range logger.Log.Hooks {
		prev[lvl] = append([]logrus.Hook(nil), hs...)
	}
	h := &crWarnLines{}
	logger.Log.AddHook(h)
	t.Cleanup(func() { logger.Log.ReplaceHooks(prev) })
	return h
}

// EMERGENCY FLAT (POST /api/risk/force-flat → handleForceFlat). Also AC-4 at
// the call site: the TCP path logs the SEND it made, naming the bound account,
// and never the CSV path's false "operator must manually flatten on NT chart".
// MUTANT (AC-4): put forceFlatSignalAdapter{writer:nil} back at the call →
// RED on the log assertions.
func TestEmergencyFlatSendsTheCloseForTheBoundAccountOnly(t *testing.T) {
	s, w := crAPIRig(t)
	logs := captureAPIWarns(t)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("user_id", "u")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/risk/force-flat?trader_id=t2", nil)
	s.handleForceFlat(c)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"triggered":true`) {
		t.Fatalf("force-flat on t2 must trigger, got %d %s", rec.Code, rec.Body.String())
	}
	got := w.waitCloses(t, 1)
	crAPIMustOnly(t, "Emergency Flat", got)

	var sent, falseLine bool
	for _, l := range logs.all() {
		if strings.Contains(l, "trader=t2 close sent to NT8 for account SimAccount1") &&
			strings.Contains(l, "confirmed by the position_close frame") {
			sent = true
		}
		if strings.Contains(l, "operator must manually flatten") {
			falseLine = true
		}
	}
	if !sent {
		t.Errorf("the TCP Emergency Flat must log the send it made for the bound account (AC-4); WARN lines: %q", logs.all())
	}
	if falseLine {
		t.Errorf("the TCP Emergency Flat still logs \"operator must manually flatten on NT chart\" after a real send (rescan D-4 / N7)")
	}
}

// POSITIONS → CLOSE (POST /api/traders/:id/close-position → handleClosePosition).
func TestPositionsCloseSendsTheCloseForTheBoundAccountOnly(t *testing.T) {
	s, w := crAPIRig(t)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("user_id", "u")
	c.Params = gin.Params{{Key: "id", Value: "t2"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/traders/t2/close-position", strings.NewReader(`{"symbol":"MNQ","side":"LONG"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	s.handleClosePosition(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("Positions → Close on t2 must send, got %d %s", rec.Code, rec.Body.String())
	}
	crAPIMustOnly(t, "Positions → Close", w.waitCloses(t, 1))
}
