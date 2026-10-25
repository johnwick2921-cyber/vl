package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"vl/manager"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/trader"
	ntTrader "vl/trader/ninjatrader"
)

// m420b/B6 — /api/health cme_open + feed_stale, and a status that degrades only
// while CME is open. All four states (+ the open-span clamp), through handleHealth.

func healthAt(t *testing.T, now time.Time, newestBarOpen time.Time, deadMan bool) map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	st, err := store.New(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	oldNow, oldProv := healthNow, market.FuturesBarsProvider
	healthNow = func() time.Time { return now }
	market.FuturesBarsProvider = func(string, string, int) []market.Kline {
		if newestBarOpen.IsZero() { // m426: no bar at all
			return nil
		}
		return []market.Kline{{OpenTime: newestBarOpen.UnixMilli()}}
	}
	t.Cleanup(func() { healthNow, market.FuturesBarsProvider = oldNow, oldProv })

	tcpT := ntTrader.NewTCPTrader(ntwire.NewTCPServer(nil), "MNQ", "Sim101")
	at := trader.NewAutoTraderOnBrokerForTest("t1", "ninjatrader", tcpT, true)
	if deadMan {
		at.SetDeadManBlockedForTest(42_000, 3)
	}
	tm := manager.NewTraderManager()
	tm.SetTraderForTest("t1", at)
	s := &Server{store: st, traderManager: tm}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	s.handleHealth(c)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func ctInstant(t *testing.T, date, hhmm string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	ts, err := time.ParseInLocation("2006-01-02 15:04", date+" "+hhmm, loc)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestM420bHealthStatusFollowsTheCMEClock(t *testing.T) {
	mon := ctInstant(t, "2026-08-17", "12:00") // open
	sat := ctInstant(t, "2026-08-22", "12:00") // weekend, closed

	check := func(name string, b map[string]any, status string, open, stale bool) {
		t.Helper()
		if b["status"] != status || b["cme_open"] != open || b["feed_stale"] != stale {
			t.Fatalf("%s: status=%v cme_open=%v feed_stale=%v, want %s/%v/%v", name, b["status"], b["cme_open"], b["feed_stale"], status, open, stale)
		}
	}
	// 1. open + fresh bar -> ok.
	check("open+fresh", healthAt(t, mon, mon.Add(-2*time.Minute), false), "ok", true, false)
	// 2. open + a 2 h old bar -> degraded, feed_stale.
	check("open+stale", healthAt(t, mon, mon.Add(-2*time.Hour), false), "degraded", true, true)
	if b := healthAt(t, mon, mon.Add(-2*time.Hour), false); b["feed_stale_secs"] == nil || b["feed_stale_secs"].(float64) < 7000 {
		t.Fatalf("feed_stale_secs must be present and ~2h when stale: %v", b["feed_stale_secs"])
	} else if _, has := b["cme_reopens_at"]; has {
		t.Fatal("cme_reopens_at must be absent while open")
	}
	if b := healthAt(t, mon, mon.Add(-2*time.Minute), false); b["feed_stale_secs"] != nil {
		t.Fatal("feed_stale_secs must be absent when not stale")
	}
	// 3. open + fresh bar + dead-man block -> degraded.
	check("open+deadman", healthAt(t, mon, mon.Add(-2*time.Minute), true), "degraded", true, false)
	// 4. closed weekend, 30 h old bar, link not up -> NOT degraded.
	check("weekend", healthAt(t, sat, sat.Add(-30*time.Hour), false), "ok", false, false)
	if b := healthAt(t, sat, sat.Add(-30*time.Hour), false); b["cme_reopens_at"] != ctInstant(t, "2026-08-23", "17:00").UTC().Format(time.RFC3339) {
		t.Fatalf("cme_reopens_at must be Sun 17:00 CT from the calendar, got %v", b["cme_reopens_at"])
	}
	// 4b. closed weekend + a dead-man block -> still ok (degrades only while open).
	check("weekend+deadman", healthAt(t, sat, sat.Add(-30*time.Hour), true), "ok", false, false)
	// 5. 5 min after the 17:00 reopen, newest bar from 15:59: closed time is not counted.
	reopen := ctInstant(t, "2026-08-17", "17:05")
	check("reopen clamp", healthAt(t, reopen, ctInstant(t, "2026-08-17", "15:59"), false), "ok", true, false)
}

// m426 (review of m420b): CME open + NT8 link not up + NO bar (a boot with NT8
// closed) must read degraded/feed_stale; a closed weekend in the same state
// stays ok. Mutant: drop the `else if cmeOpen && Link != "up"` arm -> RED.
func TestM426HealthOpenMarketNoBarLinkDownIsDegraded(t *testing.T) {
	mon := ctInstant(t, "2026-08-17", "12:00")
	sat := ctInstant(t, "2026-08-22", "12:00")
	b := healthAt(t, mon, time.Time{}, false)
	if b["status"] != "degraded" || b["cme_open"] != true || b["feed_stale"] != true {
		t.Fatalf("open+no bar+link down: status=%v cme_open=%v feed_stale=%v, want degraded/true/true", b["status"], b["cme_open"], b["feed_stale"])
	}
	b = healthAt(t, sat, time.Time{}, false)
	if b["status"] != "ok" || b["feed_stale"] != false {
		t.Fatalf("closed weekend+no bar+link down must stay ok: status=%v feed_stale=%v", b["status"], b["feed_stale"])
	}
}
