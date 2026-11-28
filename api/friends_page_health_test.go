package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// m386 — the web reads friends_page from /api/health (an existing status
// endpoint), never a new public endpoint. The flag must be present and track
// VL_FRIENDS_PAGE.

func TestHealthIncludesFriendsPageFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)

	check := func(want bool) {
		srv := &Server{}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
		srv.handleHealth(c)

		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("health body not JSON: %v", err)
		}
		got, ok := body["friends_page"].(bool)
		if !ok {
			t.Fatalf("health must carry friends_page bool, got %v", body["friends_page"])
		}
		if got != want {
			t.Fatalf("health friends_page = %v, want %v", got, want)
		}
	}

	t.Setenv("VL_FRIENDS_PAGE", "")
	check(false)

	t.Setenv("VL_FRIENDS_PAGE", "1")
	check(true)
}
