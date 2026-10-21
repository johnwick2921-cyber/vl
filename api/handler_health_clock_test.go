package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"vl/kernel"
)

// Release #22 OPS-SAFETY item 3 — the clock guard was blind: the only step
// signal was a 15-min script reading an INACTIVE timesyncd. The bot now
// counts its own wall-vs-monotonic steps and /api/health serves them. Pinned
// at the production handler with an injected stepped clock.
func TestHealthServesClockStepCounters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	clk := kernel.NewSteppedClock(time.Date(2026, 10, 14, 0, 23, 0, 0, time.UTC))
	t.Cleanup(kernel.ClockSampleForTest(clk.Sample))
	kernel.ObserveClock()
	clk.Advance(time.Second)
	clk.Step(-125 * time.Second)
	kernel.ObserveClock() // the 1 s sampler's tick inside the 4.9 s excursion
	clk.Advance(4900 * time.Millisecond)
	clk.Step(124400 * time.Millisecond)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	(&Server{}).handleHealth(c)

	var body struct {
		Clock *kernel.ClockStepView `json:"clock"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("health body not JSON: %v", err)
	}
	if body.Clock == nil {
		t.Fatalf("/api/health must serve the clock-step counters, body=%s", w.Body.String())
	}
	v := body.Clock
	if v.StepsBackward != 1 || v.StepsForward != 1 || v.FreezeSteps != 2 {
		t.Fatalf("want 1 backward + 1 forward (both freezing), got %+v", *v)
	}
	if !v.EntryHoldActive || v.LastStepMs == nil || *v.LastStepMs < 124000 {
		t.Fatalf("hold + last step must be served as read values, got %+v", *v)
	}
}
