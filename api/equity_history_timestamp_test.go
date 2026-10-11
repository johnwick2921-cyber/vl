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
	"vl/store"
)

// F6 equity-chart "Invalid Date": the single-equity-history endpoint used to
// send ONLY kernel.FormatCT(snap.Timestamp) — "2026-09-17 14:12 CT" — which the
// web parsed with new Date() and rendered "Invalid Date" on every axis tick.
// The fix adds timestamp_ms (epoch ms) beside the display string, so the chart
// can format a real instant and never parse the display string.
//
// RED (today's code): TimestampMs is absent from the JSON → this test fails.
func TestEquityHistoryCarriesTimestampMs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.New(filepath.Join(t.TempDir(), "eq.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	const tid = "trader-1"
	if err := st.Trader().Create(&store.Trader{
		ID: tid, UserID: "u1", Name: "t1", AIModelID: "m1", ExchangeID: "e1", InitialBalance: 100000,
	}); err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 9, 17, 14, 12, 0, 0, kernel.CTLocation())
	if err := st.Equity().Save(&store.EquitySnapshot{
		TraderID: tid, Timestamp: ts, TotalEquity: 100000, Balance: 100000,
		UnrealizedPnL: 250, PositionCount: 1, MarginUsedPct: 12.5,
	}); err != nil {
		t.Fatal(err)
	}

	s := &Server{store: st}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/equity-history?trader_id="+tid, nil)
	s.handleEquityHistory(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var pts []struct {
		Timestamp   string  `json:"timestamp"`
		TimestampMs int64   `json:"timestamp_ms"`
		TotalEquity float64 `json:"total_equity"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pts); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(pts) != 1 {
		t.Fatalf("points = %d, want 1", len(pts))
	}
	if pts[0].TimestampMs != ts.UnixMilli() {
		t.Errorf("timestamp_ms = %d, want %d (snapshot epoch ms)", pts[0].TimestampMs, ts.UnixMilli())
	}
	if pts[0].Timestamp != kernel.FormatCT(ts) {
		t.Errorf("timestamp = %q, want the unchanged display string %q", pts[0].Timestamp, kernel.FormatCT(ts))
	}
}
