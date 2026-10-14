package testnet

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestGuardRefusesAndRecordsALiveBotDial(t *testing.T) {
	restore := Install()
	defer restore()
	for _, u := range []string{"http://127.0.0.1:8080/api/models", "http://localhost:8080/api/health", "http://[::1]:8080/x"} {
		resp, err := http.Get(u)
		if err == nil {
			resp.Body.Close()
			t.Fatalf("%s: the guard must refuse a dial to the live bot port", u)
		}
		if !strings.Contains(err.Error(), "LIVE bot API") {
			t.Fatalf("%s: the refusal must name itself, got %v", u, err)
		}
	}
	if n := len(Attempts()); n != 3 {
		t.Fatalf("every refused dial must be recorded, got %d", n)
	}
	for _, a := range []string{"127.0.0.1:8081", "10.0.0.5:8080", "example.test:8080"} {
		if IsLiveBotAddr(a) {
			t.Fatalf("%s is not the live bot", a)
		}
	}
}

// liveLiteral matches a test that NAMES the live bot as something to dial: an
// http(s) URL on a loopback host at :8080, or the Telegram agent built on 8080.
var liveLiteral = regexp.MustCompile(`https?://(127\.0\.0\.1|localhost|\[::1\]):8080|\bagent\.New\(\s*8080\b|\bNew\(\s*8080\s*,`)

// liveLiteralAllow names the files that may carry such a literal, and why.
// Each must not DIAL it during an ordinary `go test`.
var liveLiteralAllow = map[string]string{
	"trader/acceptance_rehearsal_test.go": "the Part C dress rehearsal: skipped unless VL_REHEARSAL=1 (a paid, deliberately live harness)",
	"api/handler_updates_test.go":         "Origin HEADER values for the same-origin check; nothing is dialed",
	"internal/updaterworker/preflight_test.go": "app bases NewHTTPApp must REFUSE (scheme / path / host checks); refused before any dial",
}

// TestNoTestNamesTheLiveBotPort is the repo census half of the guard.
func TestNoTestNamesTheLiveBotPort(t *testing.T) {
	root := filepath.Join("..", "..")
	var offenders []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "web", "node_modules", ".claude", ".understand-anything", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == "internal/testnet/testnet_test.go" {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil || !liveLiteral.Match(b) {
			return nil
		}
		if _, ok := liveLiteralAllow[rel]; !ok {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if len(offenders) > 0 {
		t.Fatalf("these tests name the LIVE bot port 8080 as a dial target; use an httptest server (or add a reasoned allowlist entry if nothing is dialed): %v", offenders)
	}
}
