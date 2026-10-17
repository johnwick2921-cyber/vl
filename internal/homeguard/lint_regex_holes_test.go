package homeguard

import "testing"

// HOLE: the recursive-delete lint (R2/R4) matches only the literal
// `os.RemoveAll(` spelling and a shell-out to rm/rmdir/shred. Every one of
// these evasive forms deletes files but is invisible to the current regexes.
// This test asserts the lint WOULD flag them; it FAILS today on each miss so
// the CTO can see exactly which spellings slip through.
func TestLintRegexesCatchEvasiveRecursiveDeletes(t *testing.T) {
	cases := []struct {
		name    string
		snippet string
	}{
		{"aliased os import", `xos.RemoveAll(path)`},
		{"method value (no call parens)", `f := os.RemoveAll; f(path)`},
		{"dot-import os", `RemoveAll(path)`},
		{"x/sys/unix Unlinkat", `unix.Unlinkat(dirfd, path, 0)`},
		{"syscall Unlink", `syscall.Unlink(path)`},
	}
	for _, c := range cases {
		if !removeRe.MatchString(c.snippet) && !rmExecRe.MatchString(c.snippet) {
			t.Errorf("HOLE %s: the lint regexes miss %q", c.name, c.snippet)
		}
	}
}
