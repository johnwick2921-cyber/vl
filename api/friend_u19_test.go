package api

import (
	"net/http"
	"testing"
)

// U19 — friend mode: custom_api_url must be https (and, via security.ValidateURL,
// must not resolve to loopback/private/link-local). The owner's bot keeps the
// lenient http-allowed behavior so pre-existing local proxies do not break.

func TestU19FriendModeRequiresHTTPSOnModelEntry(t *testing.T) {
	e := newUpdEnv(t)

	cases := []struct {
		name     string
		friend   string
		url      string
		wantCode int
	}{
		{"friend http refused", "1", "http://api.deepseek.com", http.StatusBadRequest},
		{"friend https ok", "1", "https://api.deepseek.com", http.StatusOK},
		{"friend http loopback refused", "1", "http://127.0.0.1:9999", http.StatusBadRequest},
		{"owner http allowed (lenient)", "", "http://api.deepseek.com", http.StatusOK},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("VL_FRIEND_INSTANCE", c.friend)
			body := `{"provider":"deepseek","name":"` + c.name + `","custom_api_url":"` + c.url + `"}`
			w := friendCall(t, e, "POST", "/api/models/entry", body, e.tok)
			if w.Code != c.wantCode {
				t.Fatalf("custom_api_url=%q friend=%q: code=%d want=%d (%s)", c.url, c.friend, w.Code, c.wantCode, w.Body.String())
			}
		})
	}
}

func TestU19FriendModeRequiresHTTPSOnModelUpdate(t *testing.T) {
	e := newUpdEnv(t)
	t.Setenv("VL_FRIEND_INSTANCE", "1")

	// http refused on the PUT path
	body := `{"models":{"deepseek":{"custom_api_url":"http://api.deepseek.com"}}}`
	w := friendCall(t, e, "PUT", "/api/models", body, e.tok)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT http custom_api_url in friend mode: code=%d want=400 (%s)", w.Code, w.Body.String())
	}

	// https accepted (passes the validator; the row may or may not exist — a
	// non-400 proves the URL guard let it through)
	body = `{"models":{"deepseek":{"custom_api_url":"https://api.deepseek.com"}}}`
	w = friendCall(t, e, "PUT", "/api/models", body, e.tok)
	if w.Code == http.StatusBadRequest {
		t.Fatalf("PUT https custom_api_url in friend mode: code=%d, expected not 400 (%s)", w.Code, w.Body.String())
	}
}
