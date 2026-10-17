package homeguard

import "testing"

// ROUND 3 (DS-106): the round-2 fix flags os.Remove by CONTEXT — inside a
// for/range body or a filepath.Walk/WalkDir FUNCTION LITERAL. A named callback
// (a FuncDecl, not a FuncLit) or a small helper called from the loop carries
// the os.Remove outside that context, so the same tree delete produces zero
// findings. This test asserts both indirections MUST be flagged; it FAILS today.
func TestLintCatchesRemoveBehindIndirection(t *testing.T) {
	cases := map[string]string{
		"named Walk callback": `package p
import "os"
import "path/filepath"
func rmCallback(p string, _ os.FileInfo, err error) error { return os.Remove(p) }
func wipe(root string) error { return filepath.Walk(root, rmCallback) }`,
		"helper called in a loop": `package p
import "os"
func removeOne(p string) error { return os.Remove(p) }
func wipe(paths []string) {
	for _, p := range paths {
		_ = removeOne(p)
	}
}`,
	}
	for name, src := range cases {
		lines, why, err := deleteFindings(name+".go", []byte(src))
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		if len(lines) == 0 {
			t.Errorf("HOLE %s: os.Remove behind a named function / helper is invisible to the loop-Walk detector", name)
		}
		_ = why
	}
}

// ROUND 3 (DS-106): the lint tracks the NAMED syscall entry points
// (syscall.Unlink / Unlinkat / Rmdir) but not a raw syscall.Syscall with a
// numeric number (SYS_UNLINKAT, SYS_RMDIR), nor x/sys/unix.Syscall. This test
// asserts the raw syscall form MUST be flagged; it FAILS today.
func TestLintCatchesRawSyscallUnlink(t *testing.T) {
	src := []byte(`package p
import "syscall"
func wipe(p string) error {
	_, _, e := syscall.Syscall(syscall.SYS_UNLINKAT, 0, uintptr(unsafe.Pointer(&p)), 0)
	return e
}
`)
	lines, _, err := deleteFindings("raw.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("HOLE: syscall.Syscall(SYS_UNLINKAT, …) is invisible to the lint")
	}
}
