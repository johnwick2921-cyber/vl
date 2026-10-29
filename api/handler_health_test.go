package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"vl/kernel"
	"vl/manager"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/trader"
	ntTrader "vl/trader/ninjatrader"
)

// TestHealthReportsRealState pins P2-6 at the production call site
// (handleHealth): time must be a real RFC3339 timestamp (never null), db must
// be probed, and a dead database must answer 503 — not 200.
func TestHealthReportsRealState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// No store: degraded (the DB leg is unavailable) but still 200 with a real
	// time and the revision cutover scripts read.
	srv := &Server{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	srv.handleHealth(c)

	if w.Code != http.StatusOK {
		t.Fatalf("no-store health = %d, want 200 (degraded)", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("health body not JSON: %v", err)
	}
	if ts, ok := body["time"].(string); !ok || ts == "" {
		t.Fatalf("health time must be a real timestamp, got %v", body["time"])
	} else if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Fatalf("health time not RFC3339: %q", ts)
	}
	if body["revision"] != kernel.RunningRevision() {
		t.Fatalf("revision must stay %q for cutover scripts, got %v", kernel.RunningRevision(), body["revision"])
	}
	if body["db"] != "unavailable" || body["status"] != "degraded" {
		t.Fatalf("no-store health must read db=unavailable status=degraded, got %+v", body)
	}
	if tr, ok := body["traders_running"].(float64); !ok || tr != 0 {
		t.Fatalf("traders_running must be 0, got %v", body["traders_running"])
	}
	// K1/K7 — the connect-key refusal counter is exposed under the protocol
	// doc's name, and reads 0 when no wire server is running (records, never
	// infers).
	if kr, ok := body["nt_connect_key_refused"].(float64); !ok || kr != 0 {
		t.Fatalf("nt_connect_key_refused must be present and 0 with no wire server, got %v", body["nt_connect_key_refused"])
	}

	// A CLOSED database must flip the endpoint to 503 — the old handler
	// answered 200 to a dead DB.
	st, err := store.New(filepath.Join(t.TempDir(), "dead.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	st.Close()
	srv2 := &Server{store: st}
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	srv2.handleHealth(c2)
	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("dead-DB health = %d, want 503", w2.Code)
	}
}

// TestHealthReportsDeadManBlock — W26 CTO gate F1 supplement: the /api/health
// `nt8.<id>.dead_man` field must carry the live block at the production call
// site (handleHealth reads at.HealthDeadMan()). A running trader with a blocked
// dead-man episode must surface {blocked, since_ms, probes}; a running trader
// that is NOT blocked must omit the field.
func TestHealthReportsDeadManBlock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.New(filepath.Join(t.TempDir(), "dm.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	srv := ntwire.NewTCPServer(nil)
	tcpT := ntTrader.NewTCPTrader(srv, "MNQ", "Sim101")
	blocked := trader.NewAutoTraderOnBrokerForTest("t1", "ninjatrader", tcpT, true)
	blocked.SetDeadManBlockedForTest(42_000, 3)

	clear := trader.NewAutoTraderOnBrokerForTest("t2", "ninjatrader", tcpT, true)

	tm := manager.NewTraderManager()
	tm.SetTraderForTest("t1", blocked)
	tm.SetTraderForTest("t2", clear)
	s := &Server{store: st, traderManager: tm}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	s.handleHealth(c)

	if w.Code != http.StatusOK {
		t.Fatalf("health = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("health body not JSON: %v", err)
	}
	nt8, ok := body["nt8"].(map[string]any)
	if !ok {
		t.Fatalf("health nt8 must be a map, got %T %v", body["nt8"], body["nt8"])
	}
	// Blocked trader: the dead_man field is present with the episode's READ values.
	t1, ok := nt8["t1"].(map[string]any)
	if !ok {
		t.Fatalf("nt8.t1 missing, got %+v", nt8)
	}
	dm, ok := t1["dead_man"].(map[string]any)
	if !ok {
		t.Fatalf("nt8.t1.dead_man must be present when blocked, got %+v", t1)
	}
	if dm["blocked"] != true {
		t.Fatalf("dead_man.blocked must be true, got %v", dm["blocked"])
	}
	if dm["since_ms"] != float64(42_000) {
		t.Fatalf("dead_man.since_ms must be 42000, got %v", dm["since_ms"])
	}
	if dm["probes"] != float64(3) {
		t.Fatalf("dead_man.probes must be 3, got %v", dm["probes"])
	}
	// Not blocked: omitempty — the field must be absent, never a fabricated false.
	t2, ok := nt8["t2"].(map[string]any)
	if !ok {
		t.Fatalf("nt8.t2 missing, got %+v", nt8)
	}
	if _, present := t2["dead_man"]; present {
		t.Fatalf("nt8.t2.dead_man must be ABSENT when not blocked, got %+v", t2)
	}
}

// m445 — /api/health trading_paused: the data-integrity gate's READ surface.
// ABSENT without the NT8 bridge (nothing claimed); with it and a detector that
// has not judged any symbol yet, paused:true with the unknown reason while CME
// is open (and not paused while CME is closed — nothing can open then).
func TestHealthTradingPaused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	get := func(at time.Time) map[string]any {
		t.Helper()
		prev := healthNow
		healthNow = func() time.Time { return at }
		t.Cleanup(func() { healthNow = prev })
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
		(&Server{}).handleHealth(c)
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("health body not JSON: %v", err)
		}
		return body
	}
	open := time.Date(2026, 10, 28, 10, 0, 0, 0, kernel.CTLocation())  // Wed, CME open
	shut := time.Date(2026, 10, 28, 16, 30, 0, 0, kernel.CTLocation()) // the daily break

	t.Cleanup(ntTrader.SetIntegrityArmedForTest(false))
	if _, present := get(open)["trading_paused"]; present {
		t.Fatal("no NT8 bridge: trading_paused must be ABSENT, never a fabricated 'not paused'")
	}

	ntTrader.SetIntegrityArmedForTest(true)
	restore := ntTrader.SetBarHolesWiredForTest() // a tracker that has judged nothing
	t.Cleanup(restore)
	tp, ok := get(open)["trading_paused"].(map[string]any)
	if !ok {
		t.Fatal("armed: trading_paused must be present")
	}
	reasons, _ := tp["reasons"].([]any)
	if tp["paused"] != true || len(reasons) != 1 || reasons[0] != ntTrader.IntegrityClassUnknown {
		t.Fatalf("an unjudged detector with CME open is PAUSED with the unknown reason: %+v", tp)
	}
	if _, ok := tp["counts"].(map[string]any); !ok {
		t.Fatalf("counts must be present: %+v", tp)
	}
	if tp2 := get(shut)["trading_paused"].(map[string]any); tp2["paused"] != false {
		t.Fatalf("CME closed: an unknown detector is moot, not paused: %+v", tp2)
	}
}

// m445 round 2 (reviewer A, D1) — the mentor's SEEDED-series leg is on the
// health surface, per mentor trader, named by id: a pause the owner cannot see
// is a pause the owner discovers from a silent day. A non-mentor trader on the
// same process is not listed (it never reads the seeded key levels).
func TestHealthTradingPausedNamesTheMentorSeedLeg(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(ntTrader.SetIntegrityArmedForTest(true))
	t.Cleanup(ntTrader.SetBarHolesWiredForTest())

	now := time.Date(2026, 10, 28, 10, 0, 0, 0, kernel.CTLocation()) // Wed, CME open
	var ts []int64
	for m := time.Date(2026, 10, 20, 17, 0, 0, 0, kernel.CTLocation()); m.Before(now); m = m.Add(time.Minute) {
		if kernel.IsCMEOpen(m) {
			// a 40-minute RTH hole on 10-22 10:00–10:40 CT
			if h := time.Date(2026, 10, 22, 10, 0, 0, 0, kernel.CTLocation()); !m.Before(h) && m.Before(h.Add(40*time.Minute)) {
				continue
			}
			ts = append(ts, m.UnixMilli())
		}
	}
	srv := ntwire.NewTCPServer(nil)
	tcpT := ntTrader.NewTCPTrader(srv, "MNQ", "Sim101")
	mentorT := trader.NewAutoTraderOnBrokerForTest("tm", "ninjatrader", tcpT, true)
	mentorT.NoteSeedSeriesForTest(true, ts, now)
	plainT := trader.NewAutoTraderOnBrokerForTest("tp", "ninjatrader", tcpT, true)
	plainT.NoteSeedSeriesForTest(false, ts, now)
	tm := manager.NewTraderManager()
	tm.SetTraderForTest("tm", mentorT)
	tm.SetTraderForTest("tp", plainT)

	prev := healthNow
	healthNow = func() time.Time { return now }
	t.Cleanup(func() { healthNow = prev })
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	(&Server{traderManager: tm}).handleHealth(c)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("health body not JSON: %v", err)
	}
	tp, ok := body["trading_paused"].(map[string]any)
	if !ok {
		t.Fatal("armed: trading_paused must be present")
	}
	ms, _ := tp["mentor_seed"].([]any)
	if tp["paused"] != true || len(ms) != 1 {
		t.Fatalf("one mentor trader with a seeded RTH hole must be listed (and only it): %+v", tp)
	}
	row := ms[0].(map[string]any)
	wins, _ := row["windows"].([]any)
	if row["trader"] != "tm" || row["scope"] != "mentor_entries" || len(wins) != 1 || wins[0] != "10-22 10:00–10:40 CT" || row["action"] != ntTrader.IntegrityActionSeeded {
		t.Fatalf("the row names the trader, the scope, the CT window and the action: %+v", row)
	}
	allWins, _ := tp["windows"].([]any)
	found := false
	for _, w := range allWins {
		found = found || w == "10-22 10:00–10:40 CT"
	}
	if !found {
		t.Fatalf("the top-level windows (the banner's source) must carry the seeded hole: %+v", tp["windows"])
	}
}

// m445 round 4 (D3) — the pause surface says WHICH scope is paused and only for
// symbols a running trader trades. Round 3: the banner read "TRADING PAUSED" when
// only SWING4H was (intraday kept trading), and an unfilled hole on a subscribed
// but untraded symbol (ES) painted it for an MNQ trader.
func TestHealthTradingPausedCarriesTheScopeAndIgnoresUntradedSymbols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(ntTrader.SetIntegrityArmedForTest(true))
	now := time.Date(2026, 10, 28, 10, 0, 0, 0, kernel.CTLocation()) // Wed, CME open
	ct := func(mo time.Month, d, h, m int) int64 {
		return time.Date(2026, mo, d, h, m, 0, 0, kernel.CTLocation()).UnixMilli()
	}
	srv := ntwire.NewTCPServer(nil)
	tcpT := ntTrader.NewTCPTrader(srv, "MNQ", "Sim101")
	mentorT := trader.NewAutoTraderOnBrokerForTest("tm", "ninjatrader", tcpT, true)
	mentorT.NoteSeedSeriesForTest(true, nil, now)
	tm := manager.NewTraderManager()
	tm.SetTraderForTest("tm", mentorT)

	get := func() map[string]any {
		t.Helper()
		prev := healthNow
		healthNow = func() time.Time { return now }
		t.Cleanup(func() { healthNow = prev })
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
		(&Server{traderManager: tm}).handleHealth(c)
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("health body not JSON: %v", err)
		}
		tp, ok := body["trading_paused"].(map[string]any)
		if !ok {
			t.Fatal("armed: trading_paused must be present")
		}
		return tp
	}
	judgedFrom, judgedTo := ct(time.October, 1, 17, 0), now.UnixMilli()

	// MNQ: a 10-14 overnight hole — outside the 12000-minute AI/planner window and
	// the 1500-minute intraday one, inside SWING4H's 24720. ES (nobody trades it):
	// a hole 08:00–08:30 today, inside the intraday window.
	restore := ntTrader.SetBarHolesWithHolesForTest(judgedFrom, judgedTo, []ntTrader.TestHole{
		{Symbol: "MNQ", Contract: "MNQ 12-26", FromMs: ct(time.October, 14, 3, 0), ToMs: ct(time.October, 14, 3, 30)},
		{Symbol: "ES", Contract: "ES 12-26", FromMs: ct(time.October, 28, 8, 0), ToMs: ct(time.October, 28, 8, 30)},
	})
	t.Cleanup(restore)
	tp := get()
	if tp["paused"] != true || tp["scope"] != "swing_entries" {
		t.Fatalf("only SWING4H is paused: the surface must say so, got paused=%v scope=%v (%+v)", tp["paused"], tp["scope"], tp)
	}
	scopes, _ := tp["scopes"].([]any)
	wins, _ := tp["windows"].([]any)
	if len(scopes) != 1 || scopes[0] != "swing_entries" || len(wins) != 1 || wins[0] != "10-14 03:00–03:30 CT" {
		t.Fatalf("the untraded ES hole must not appear in scopes/windows: scopes=%v windows=%v", scopes, wins)
	}
	for _, v := range tp["symbols"].([]any) {
		if v.(map[string]any)["symbol"] == "ES" {
			t.Fatalf("a symbol no running trader trades must not be listed: %+v", tp["symbols"])
		}
	}

	// nothing but the untraded symbol is holed: NOT paused at all
	restore()
	restore2 := ntTrader.SetBarHolesWithHolesForTest(judgedFrom, judgedTo, []ntTrader.TestHole{
		{Symbol: "MNQ", Contract: "MNQ 12-26"},
		{Symbol: "ES", Contract: "ES 12-26", FromMs: ct(time.October, 28, 8, 0), ToMs: ct(time.October, 28, 8, 30)},
	})
	t.Cleanup(restore2)
	// (paused may still be true for the stale-feed leg of this bare test trader —
	// what must be absent is any DATA reason and any scope)
	tp = get()
	rs, _ := tp["reasons"].([]any)
	for _, r := range rs {
		if r == ntTrader.IntegrityClassHole || r == ntTrader.IntegrityClassUnknown {
			t.Fatalf("a hole on a symbol no running trader trades pauses nothing: %+v", tp)
		}
	}
	if tp["scope"] != "" {
		t.Fatalf("no data pause, so no scope: %+v", tp)
	}

	// a fresh MNQ hole inside the intraday window is ALL entries, the widest scope
	restore2()
	restore3 := ntTrader.SetBarHolesWithHolesForTest(judgedFrom, judgedTo, []ntTrader.TestHole{
		{Symbol: "MNQ", Contract: "MNQ 12-26", FromMs: ct(time.October, 14, 3, 0), ToMs: ct(time.October, 14, 3, 30)},
		{Symbol: "MNQ", Contract: "MNQ 12-26", FromMs: ct(time.October, 28, 8, 0), ToMs: ct(time.October, 28, 8, 30)},
	})
	t.Cleanup(restore3)
	if tp := get(); tp["scope"] != "all_entries" {
		t.Fatalf("a hole inside the intraday window is all_entries: %+v", tp)
	}
}

// healthTP serves /api/health for `tm` at `now` and returns trading_paused.
func healthTP(t *testing.T, tm *manager.TraderManager, now time.Time) map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	prev := healthNow
	healthNow = func() time.Time { return now }
	t.Cleanup(func() { healthNow = prev })
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	(&Server{traderManager: tm}).handleHealth(c)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("health body not JSON: %v", err)
	}
	tp, ok := body["trading_paused"].(map[string]any)
	if !ok {
		t.Fatal("armed: trading_paused must be present")
	}
	return tp
}

// m445 round 5 (DEF-B) — the health scope the banner reads says what the gate
// refuses. An RTH hole older than 1500 open minutes and inside 12000 pauses the
// AI/planner path AND the mentor's key-level leg: the old precedence reported
// swing_and_deep_entries, and the banner then claimed "intraday mentor setups
// trade normally" while the mentor gate refused them until the 90-day reach.
func TestHealthScopeCarriesEveryBlockedLegAndNeverHidesTheMentorPause(t *testing.T) {
	t.Cleanup(ntTrader.SetIntegrityArmedForTest(true))
	ct := func(mo time.Month, d, h, m int) int64 {
		return time.Date(2026, mo, d, h, m, 0, 0, kernel.CTLocation()).UnixMilli()
	}
	srv := ntwire.NewTCPServer(nil)
	tcpT := ntTrader.NewTCPTrader(srv, "MNQ", "Sim101")
	for _, now := range []time.Time{
		time.Date(2026, 10, 29, 12, 0, 0, 0, kernel.CTLocation()),
		time.Date(2026, 10, 31, 12, 0, 0, 0, kernel.CTLocation()),
	} {
		mentorT := trader.NewAutoTraderOnBrokerForTest("tm", "ninjatrader", tcpT, true)
		mentorT.NoteSeedSeriesForTest(true, nil, now)
		tm := manager.NewTraderManager()
		tm.SetTraderForTest("tm", mentorT)
		restore := ntTrader.SetBarHolesWithHolesForTest(ct(time.October, 1, 17, 0), now.UnixMilli(), []ntTrader.TestHole{
			{Symbol: "MNQ", Contract: "MNQ 12-26", FromMs: ct(time.October, 27, 9, 0), ToMs: ct(time.October, 27, 9, 30)},
		})
		tp := healthTP(t, tm, now)
		restore()
		if tp["scope"] != "all_entries" {
			t.Fatalf("%s: mentor + AI/planner legs are both paused, so no path trades: scope = %v (%+v)", now, tp["scope"], tp)
		}
		scopes, _ := tp["scopes"].([]any)
		has := map[any]bool{}
		for _, s := range scopes {
			has[s] = true
		}
		if !has["mentor_entries"] || !has["swing_and_deep_entries"] {
			t.Fatalf("%s: scopes must name every blocked leg: %v", now, scopes)
		}
	}
}

// m445 round 5 (M2) — the mentor_seed rows are for traders that are RUNNING: a
// stopped mentor trader holding a seed hole pauses nothing (it enters nothing),
// so it must neither list a row nor paint the banner.
func TestHealthMentorSeedRowsSkipAStoppedTrader(t *testing.T) {
	t.Cleanup(ntTrader.SetIntegrityArmedForTest(true))
	t.Cleanup(ntTrader.SetBarHolesWiredForTest())
	now := time.Date(2026, 10, 28, 10, 0, 0, 0, kernel.CTLocation())
	var ts []int64
	for m := time.Date(2026, 10, 20, 17, 0, 0, 0, kernel.CTLocation()); m.Before(now); m = m.Add(time.Minute) {
		if kernel.IsCMEOpen(m) {
			if h := time.Date(2026, 10, 22, 10, 0, 0, 0, kernel.CTLocation()); !m.Before(h) && m.Before(h.Add(40*time.Minute)) {
				continue
			}
			ts = append(ts, m.UnixMilli())
		}
	}
	srv := ntwire.NewTCPServer(nil)
	tcpT := ntTrader.NewTCPTrader(srv, "MNQ", "Sim101")
	stopped := trader.NewAutoTraderOnBrokerForTest("tm", "ninjatrader", tcpT, false)
	stopped.NoteSeedSeriesForTest(true, ts, now)
	if len(stopped.HealthSeedSeriesPause(now)) == 0 {
		t.Fatal("setup: the stopped mentor trader must hold a seed hole (else this pins nothing)")
	}
	tm := manager.NewTraderManager()
	tm.SetTraderForTest("tm", stopped)
	tp := healthTP(t, tm, now)
	if ms, _ := tp["mentor_seed"].([]any); len(ms) != 0 {
		t.Fatalf("a stopped mentor trader must not list a mentor_seed row: %+v", ms)
	}
	for _, w := range tp["windows"].([]any) {
		if w == "10-22 10:00–10:40 CT" {
			t.Fatalf("a stopped trader's seed hole must not reach the banner windows: %+v", tp)
		}
	}
	if tp["scope"] == "mentor_entries" {
		t.Fatalf("a stopped trader's seed hole must not set the scope: %+v", tp)
	}
}
