package branding

// Census guard (Z17, plan v7 FINAL item 10): every tracked file's count
// of the old brand token (assembled at runtime below) must EQUAL its
// allowed count in the transition table. This file and the table never
// contain the old name in any casing; wrapper paths use the {OLD}
// placeholder, expanded at runtime. Entries and the Ceiling may only
// go DOWN; the CTO rejects any PR that raises either. This table is
// PROVISIONAL (generated at the D2-DEAD+WEB merge head); the final
// re-pin lands after the ops + agent/addon parts merge.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type censusEntry struct {
	count  int
	phase  string
	reason string
}

var censusTable = map[string][]censusEntry{
	"ninjascript/VLTraderTCPClient.cs": {
		{count: 0, phase: "final", reason: "zero-name ruling 2026-10-03: the three pre-rename folder mentions reworded to the pre-rename folder (owner: zero old-name tokens, no exceptions)"},
	},
}

// Ceiling = sum of allowed counts at the R1b merge (1160) + 11 for the
// e1dcc173 legacy job fixture (2026-10-02 #307).
const censusCeiling = 10

func TestCensusGuard(t *testing.T) {
	tok := "no" + "fx" // runtime assembly — never the literal
	// expand() the table keys once: table keys hold the {OLD}
	// placeholder, real paths hold the literal.
	expandedTable := make(map[string][]censusEntry, len(censusTable))
	for k, es := range censusTable {
		expandedTable[expand(k)] = es
	}
	out, err := exec.Command("git", "-C", "..", "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files failed (never skip): %v", err)
	}
	files := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	scanned := 0
	var mismatches []string
	for _, f := range files {
		if f == "" {
			continue
		}
		scanned++
		b, err := os.ReadFile(filepath.Join("..", f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		actual := strings.Count(strings.ToLower(string(b)), tok) +
			strings.Count(strings.ToLower(f), tok)
		allowed := 0
		if es, ok := expandedTable[f]; ok {
			for _, e := range es {
				allowed += e.count
			}
		}
		if actual != allowed {
			mismatches = append(mismatches, fmt.Sprintf("%s: actual %d, allowed %d — context: %s", f, actual, allowed, context(b, tok)))
		}
	}
	if scanned != len(files) || scanned == 0 {
		t.Fatalf("scanned %d of %d files (must be equal and > 0)", scanned, len(files))
	}
	if len(mismatches) > 0 {
		t.Fatalf("census mismatches (%d):\n%s", len(mismatches), strings.Join(mismatches, "\n"))
	}
	sum := 0
	for _, es := range censusTable {
		for _, e := range es {
			sum += e.count
		}
	}
	if sum > censusCeiling {
		t.Fatalf("allowed sum %d exceeds Ceiling %d — entries only go DOWN", sum, censusCeiling)
	}
}

// expand resolves the {OLD} path placeholder at runtime.
func expand(path string) string {
	return strings.ReplaceAll(path, "{OLD}", "no"+"fx")
}

// context prints up to two matched lines from the file on a mismatch.
func context(b []byte, tok string) string {
	lt := strings.ToLower(tok)
	var found []string
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.Contains(strings.ToLower(ln), lt) {
			ln = strings.TrimSpace(ln)
			if len(ln) > 120 {
				ln = ln[:120] + "..."
			}
			found = append(found, ln)
			if len(found) == 2 {
				break
			}
		}
	}
	return strings.Join(found, " | ")
}
