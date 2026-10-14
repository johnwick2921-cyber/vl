package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// RT-9 (release #24, E-4) — THE BOOT API LIST MATCHES THE ROUTER. Every line
// apiBootDocs prints is sent through the real router (newUpdEnv → setupRoutes)
// without a token: a line labelled "no auth required" must not answer 401, a
// JWT line must, and every method+path must be a registered route. The old
// literal block called /api/equity-history public and the batch endpoint a
// GET (both JWT since R22; the batch is a POST) and listed an unregistered
// /api/performance. MUTANT: label equity-history Public again → RED.
func TestAPIBootDocsMatchTheRouter(t *testing.T) {
	e := newUpdEnv(t)
	registered := map[string]bool{}
	for _, r := range e.s.router.Routes() {
		registered[r.Method+" "+r.Path] = true
	}
	for _, d := range apiBootDocs {
		path := d.Path
		if i := strings.IndexByte(path, '?'); i >= 0 {
			path = path[:i]
		}
		if !registered[d.Method+" "+path] && !registered["GET "+path] {
			t.Errorf("boot doc %q lists %s %s, which the router does not register", d.line(), d.Method, path)
			continue
		}
		if !registered[d.Method+" "+path] && path != "/api/health" { // /health is api.Any
			t.Errorf("boot doc %q: %s %s is not registered with that method", d.line(), d.Method, path)
			continue
		}
		req := strings.ReplaceAll(path, ":id", "rt9-x")
		if strings.Contains(d.Path, "?") {
			req += d.Path[strings.IndexByte(d.Path, '?'):]
		}
		body := ""
		if d.Method == "POST" || d.Method == "PUT" {
			body = "{}"
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(d.Method, req, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		e.s.router.ServeHTTP(w, r)
		got401 := w.Code == http.StatusUnauthorized
		if d.Public && got401 {
			t.Errorf("boot doc %q says no auth required, but %s %s without a token = 401", d.line(), d.Method, req)
		}
		if !d.Public && !got401 {
			t.Errorf("boot doc %q says JWT required, but %s %s without a token = %d", d.line(), d.Method, req, w.Code)
		}
		if strings.Contains(d.line(), "no auth required") != d.Public {
			t.Errorf("boot doc label disagrees with its access field: %q", d.line())
		}
	}
}
