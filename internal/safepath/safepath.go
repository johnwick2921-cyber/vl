// Package safepath is the single gate every recursive delete in vl goes
// through. Owner order 2026-10-17 (THE ONE RULE, after the 2026-10-16
// 19:05–19:14 CDT wipe of /home/hoang): no os.RemoveAll may run on a path
// that is empty, "/", a home folder, an ancestor of a home folder, or outside
// the subfolder the caller expects. A bare os.RemoveAll with an empty or
// unexpanded variable is exactly the shape that can take the whole machine;
// this package refuses it before any byte is touched.
//
// The lint test internal/homeguard/lint_test.go fails the build on any
// os.RemoveAll outside this package (allowlist: test code removing its own
// t.TempDir / os.MkdirTemp results).
package safepath

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// ErrRefused wraps every refusal so callers can errors.Is it.
var ErrRefused = errors.New("safepath: refused")

// homes returns every home folder this process can name: $HOME, the
// os.UserHomeDir answer and the passwd entry (user.Current) — the passwd
// home is the REAL one even when a test has pointed $HOME elsewhere.
func homes() []string {
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		p = filepath.Clean(p)
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		out = append(out, p)
	}
	add(os.Getenv("HOME"))
	if h, err := os.UserHomeDir(); err == nil {
		add(h)
	}
	if u, err := user.Current(); err == nil {
		add(u.HomeDir)
	}
	return out
}

// realHome is the passwd home (falls back to $HOME only when the passwd
// lookup fails) — the folder the 2026-10-16 wipe took.
func realHome() string {
	h := ""
	if u, err := user.Current(); err == nil {
		h = u.HomeDir
	}
	if h == "" {
		h = os.Getenv("HOME")
	}
	if h == "" {
		return ""
	}
	return resolve(h)
}

// isAncestorOrSelf reports whether a is b or a parent directory of b.
func isAncestorOrSelf(a, b string) bool {
	if a == b {
		return true
	}
	rel, err := filepath.Rel(a, b)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

// resolve cleans p and resolves symlinks on the longest existing prefix, so
// a symlinked parent cannot smuggle the target outside root.
func resolve(p string) string {
	p = filepath.Clean(p)
	cur, rest := p, ""
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			if rest == "" {
				return r
			}
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// Check returns nil only when target may be removed recursively as a child
// of root: both absolute and non-empty, target strictly INSIDE root, neither
// one "/" nor a home folder nor an ancestor of a home folder.
func Check(root, target string) error {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(target) == "" {
		return fmt.Errorf("%w: empty path (root=%q target=%q)", ErrRefused, root, target)
	}
	if !filepath.IsAbs(root) || !filepath.IsAbs(target) {
		return fmt.Errorf("%w: relative path (root=%q target=%q)", ErrRefused, root, target)
	}
	r, t := resolve(root), resolve(target)
	if r == "/" || t == "/" {
		return fmt.Errorf("%w: filesystem root (root=%q target=%q)", ErrRefused, r, t)
	}
	for _, h := range homes() {
		if isAncestorOrSelf(t, h) {
			return fmt.Errorf("%w: %q is a home folder or contains one (%q)", ErrRefused, t, h)
		}
	}
	// The ROOT is checked against the REAL home only: a test that points
	// $HOME at a t.TempDir() must still be able to use os.TempDir() as root.
	if h := realHome(); h != "" && isAncestorOrSelf(r, h) {
		return fmt.Errorf("%w: root %q is the real home folder or contains it (%q)", ErrRefused, r, h)
	}
	if t == r || !isAncestorOrSelf(r, t) {
		return fmt.Errorf("%w: %q is not strictly inside %q", ErrRefused, t, r)
	}
	return nil
}

// RemoveAllUnder is os.RemoveAll(target) behind Check(root, target).
func RemoveAllUnder(root, target string) error {
	if err := Check(root, target); err != nil {
		return err
	}
	return os.RemoveAll(target) //homeguard:allow — the one gated call site
}

// RemoveAllTemp removes a directory this process created with os.MkdirTemp
// (or t.TempDir) under the system temp folder. It refuses anything that is
// not strictly inside os.TempDir().
func RemoveAllTemp(target string) error {
	return RemoveAllUnder(os.TempDir(), target)
}
