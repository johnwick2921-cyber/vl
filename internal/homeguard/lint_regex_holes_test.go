package homeguard

import "testing"

// DS-106 adversarial review (2026-10-17, branch test/home-guard-holes): the
// first lint matched only the literal `os.RemoveAll(` spelling. Each of these
// files deletes through another door; the AST lint must flag every one.
func TestLintCatchesEvasiveRecursiveDeletes(t *testing.T) {
	cases := map[string]string{
		"aliased os import":   "package p\nimport xos \"os\"\nfunc f(p string) { _ = xos.RemoveAll(p) }\n",
		"method value":        "package p\nimport \"os\"\nfunc f(p string) { g := os.RemoveAll; _ = g(p) }\n",
		"dot-import os":       "package p\nimport . \"os\"\nfunc f(p string) { _ = RemoveAll(p) }\n",
		"x/sys/unix Unlinkat": "package p\nimport \"golang.org/x/sys/unix\"\nfunc f(p string) { _ = unix.Unlinkat(0, p, 0) }\n",
		"syscall Unlink":      "package p\nimport \"syscall\"\nfunc f(p string) { _ = syscall.Unlink(p) }\n",
		"aliased exec rm":     "package p\nimport x \"os/exec\"\nfunc f(p string) { _ = x.Command(\"/bin/rm\", \"-rf\", p).Run() }\n",
		"sh -c rm":            "package p\nimport \"os/exec\"\nfunc f() { _ = exec.Command(\"sh\", \"-c\", \"cd /x && rm -rf y\").Run() }\n",
		"os/user":             "package p\nimport \"os/user\"\nfunc f() { _, _ = user.Current() }\n",
	}
	for name, src := range cases {
		lines, _, err := deleteFindings(name+".go", []byte(src))
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		if len(lines) == 0 {
			t.Errorf("HOLE %s: the lint misses it", name)
		}
	}
	// and a clean file stays clean (no false positive on os.Remove of one file)
	if lines, _, _ := deleteFindings("ok.go", []byte("package p\nimport \"os\"\nfunc f(p string) { _ = os.Remove(p) }\n")); len(lines) != 0 {
		t.Errorf("os.Remove of a single file must not be flagged: lines %v", lines)
	}
}
