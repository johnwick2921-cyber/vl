package mentor

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCourseTagsAreMachineCheckable is the m186 tag-format PIN (DS-108): every
// bracket-delimited course tag in kernel/mentor and trader/mentor_* must carry a
// well-formed D-lesson (`Dx.y`) and at least one well-formed timestamp
// (`@mm:ss`), unless it is an explicit [derived: …] or [example: …] marker. A tag
// with `UNTAGGED`, `§` or `(no timestamp)` is malformed. This keeps the tags
// greppable and machine-checkable so the transcript re-check can re-verify them.
//
// MUTANT: put `UNTAGGED` (or drop the `@mm:ss`) on any tagged line → RED.
func TestCourseTagsAreMachineCheckable(t *testing.T) {
	var files []string
	me, _ := filepath.Glob("*.go")
	files = append(files, me...)
	trader, _ := filepath.Glob(filepath.Join("..", "..", "trader", "mentor_*.go"))
	files = append(files, trader...)
	if len(files) == 0 {
		t.Fatal("no mentor source files found — the tag pin cannot run")
	}

	// A course tag is any [...] run containing a D-lesson reference.
	tagRx := regexp.MustCompile(`\[[^\]]*D[0-9]+\.[0-9]+[^\]]*\]`)
	// Well-formed: carries @hh:mm somewhere inside (range or single), and none
	// of the banned markers. The space after @ is tolerated (both "@08:45" and
	// "@ 08:45" appear in the tree).
	tsRx := regexp.MustCompile(`@\s*[0-9]{2}:[0-9]{2}`)
	banned := []string{"UNTAGGED", "§", "(no timestamp)", "no timestamp"}

	malformed := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		lines := strings.Split(string(raw), "\n")
		for ln, line := range lines {
			for _, tag := range tagRx.FindAllString(line, -1) {
				// Explicit markers are exempt from the D-timestamp requirement.
				if strings.HasPrefix(tag, "[derived:") || strings.HasPrefix(tag, "[example:") {
					continue
				}
				bad := false
				for _, b := range banned {
					if strings.Contains(tag, b) {
						bad = true
					}
				}
				if !tsRx.MatchString(tag) {
					bad = true
				}
				if bad {
					malformed++
					t.Errorf("%s:%d malformed course tag %s", f, ln+1, tag)
				}
			}
		}
	}
	if malformed > 0 {
		t.Fatalf("%d malformed course tag(s) — every [Dx.y …] tag needs a @mm:ss and no UNTAGGED/§/(no timestamp)", malformed)
	}
}
