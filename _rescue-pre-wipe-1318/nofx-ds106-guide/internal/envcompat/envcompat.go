// Package envcompat (RENAME-R1a) is the ONE place production code reads
// environment variables that carry a rename: Env returns VL_<name> when
// non-empty, else NOFX_<name> when non-empty, else "". One WARN per name is
// emitted when only the NOFX_ form is set; before a sink is installed the
// warnings queue and the first sink drains them (main.go registers the
// logger's sink right after logger.Init; cmd/vl-updater registers a stderr
// sink, since it never inits the logger). R5 removes this package and every
// fallback (each caller carries an `// R5 removes` comment).
package envcompat

import (
	"os"
	"path/filepath"
	"sync"
)

// Source names which prefix a read used.
type Source string

// The three sources a boot line may print.
const (
	SourceVL      Source = "VL"
	SourceNOFX    Source = "NOFX"
	SourceDefault Source = "default"
)

var (
	mu     sync.Mutex
	sink   func(string)
	queued []string
	warned = map[string]bool{}
)

// SetWarnSink installs the writer for fallback warnings. Warnings recorded
// before the first sink are drained into it, so a read at package init is
// never dropped. A nil or second sink is ignored (one sink, ever).
func SetWarnSink(f func(string)) {
	mu.Lock()
	defer mu.Unlock()
	if f == nil || sink != nil {
		return
	}
	sink = f
	for _, q := range queued {
		f(q)
	}
	queued = nil
}

// ResetForTest clears the warn bookkeeping and the sink (TESTS ONLY — no
// production binary calls it; it exists so a test can exercise the updater's
// stderr sink on a fresh process state).
func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	sink, queued, warned = nil, nil, map[string]bool{}
}

// Env returns VL_<name> when non-empty, else NOFX_<name> when non-empty,
// else "" — the shell twin's `:-` semantics. The source names the prefix
// used (SourceDefault when both are empty). // R5 removes.
func Env(name string) (string, Source) {
	if v := os.Getenv("VL_" + name); v != "" {
		return v, SourceVL
	}
	if v := os.Getenv("NOFX_" + name); v != "" {
		warnFallback(name)
		return v, SourceNOFX
	}
	return "", SourceDefault
}

// EnvValue is Env without the source, for callers whose boot line does not
// name it. // R5 removes.
func EnvValue(name string) string {
	v, _ := Env(name)
	return v
}

func warnFallback(name string) {
	mu.Lock()
	defer mu.Unlock()
	if warned[name] {
		return
	}
	warned[name] = true
	msg := "env " + name + " read through the NOFX_ fallback (RENAME-R1a; R5 removes it)"
	f := sink
	if f == nil {
		queued = append(queued, msg)
	} else {
		f(msg)
	}
}

// BackupRoot is the one backup-directory default: ~/vl-backups when it
// exists; else ~/nofx-backups when ~/nofx exists; else ~/vl-backups — a
// fresh machine never gets an old-name dir. Callers: store backups, the
// activate and updater CLIs. // R5 removes the middle branch.
func BackupRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	if st, err := os.Stat(filepath.Join(home, "vl-backups")); err == nil && st.IsDir() {
		return filepath.Join(home, "vl-backups")
	}
	if st, err := os.Stat(filepath.Join(home, "nofx")); err == nil && st.IsDir() {
		return filepath.Join(home, "nofx-backups")
	}
	return filepath.Join(home, "vl-backups")
}
