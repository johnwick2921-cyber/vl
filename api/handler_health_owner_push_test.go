package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"vl/internal/ownerpush"
)

// m442 N4: the owner-push delivery counters are served by /api/health, READ from
// the pusher - and ABSENT (never a fabricated zero) when no pusher runs.
// MUTANT (shown RED): drop the owner_push block in handleHealth -> RED.
func TestHealthServesOwnerPushCountersOnlyWhenAPusherRuns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	get := func() map[string]json.RawMessage {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
		(&Server{}).handleHealth(c)
		var body map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("health body not JSON: %v", err)
		}
		return body
	}

	t.Cleanup(ownerpush.Use(nil))
	if _, ok := get()["owner_push"]; ok {
		t.Fatal("no pusher running (friend instance): owner_push must be absent, not a fabricated zero")
	}

	p := ownerpush.NewE(func(string) error { return errors.New("telegram send: 502 SECRET-LOOKING-DETAIL") })
	t.Cleanup(ownerpush.Use(p))
	p.Push("feed-down|e1", "DOWN")
	p.Flush()
	raw, ok := get()["owner_push"]
	if !ok {
		t.Fatal("/api/health must serve owner_push while a pusher runs")
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["failed"] != float64(1) || m["sent"] != float64(0) || m["held"] != float64(1) {
		t.Fatalf("want failed=1 sent=0 held=1, got %v", m)
	}
	if strings.Contains(string(raw), "SECRET-LOOKING-DETAIL") {
		t.Fatalf("the unauthenticated health page must not carry the send error text: %s", raw)
	}
}
