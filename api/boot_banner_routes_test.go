package api

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestBootBannerMatchesTheRouteRegistrations — OPS NEW-4 (10-14). The boot
// banner is a hand-written list; it said "GET /api/equity-history … (no auth
// required)" and "GET /api/equity-history-batch" for 2 days after R22 moved
// both behind JWT and made the batch a POST (live vl_2026-10-14.log
// 13101-13102). Boot lines are READ, never literal: every banner line must
// name a route registered with the SAME method, and a line that says
// "no auth required" must be registered on the public group (api) while
// every other line is on the JWT group (protected). routeRegistry does not
// record the group (config_resolved_test.go), so this reads the registration
// site, like the other wiring pins.
func TestBootBannerMatchesTheRouteRegistrations(t *testing.T) {
	b, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	reg := map[string]string{} // "METHOD /api/path" -> group
	for _, m := range regexp.MustCompile(`s\.route(?:WithSchema)?\((\w+), "(GET|POST|PUT|DELETE)", "([^"]+)"`).FindAllStringSubmatch(src, -1) {
		reg[m[2]+" /api"+m[3]] = m[1]
	}
	// direct group registrations (api.Any("/health", …) and the like)
	for _, m := range regexp.MustCompile(`\b(api|protected)\.(GET|POST|PUT|DELETE|Any)\("([^"]+)"`).FindAllStringSubmatch(src, -1) {
		methods := []string{m[2]}
		if m[2] == "Any" {
			methods = []string{"GET", "POST", "PUT", "DELETE"}
		}
		for _, meth := range methods {
			reg[meth+" /api"+m[3]] = m[1]
		}
	}
	if len(reg) < 50 {
		t.Fatalf("fixture: found only %d registrations — the pin is going vacuous", len(reg))
	}
	lines := regexp.MustCompile(`logger\.Infof\("  • (GET|POST|PUT|DELETE)\s+(/api/[^ ?]+)[^"]*"\)`).FindAllStringSubmatch(src, -1)
	if len(lines) < 5 {
		t.Fatalf("fixture: found only %d banner lines", len(lines))
	}
	for _, l := range lines {
		key := l[1] + " " + l[2]
		group, ok := reg[key]
		if !ok {
			t.Errorf("boot banner names %q but no route is registered with that method and path: %s", key, l[0])
			continue
		}
		public := strings.Contains(l[0], "no auth required")
		if public && group != "api" {
			t.Errorf("boot banner calls %q public (no auth required) but it is registered on the %q group: %s", key, group, l[0])
		}
		if !public && group == "api" {
			t.Errorf("boot banner omits that %q is public (registered on the api group): %s", key, l[0])
		}
	}
}
