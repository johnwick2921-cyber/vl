package updaterworker

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ── the real machine ────────────────────────────────────────────────────────

// Seams for the start refusals (tests play root, the bot's cgroup and a TZ
// without being any of them).
var (
	geteuid    = os.Geteuid
	readCgroup = func() ([]byte, error) { return os.ReadFile("/proc/self/cgroup") }
	lookupTZ   = func() (string, bool) { return os.LookupEnv("TZ") }
)

var (
	// ErrRoot: the worker never runs as root (a root-owned data/updater would
	// lock the bot into a hold it cannot read — the hold CLI's M2.1 N4 rule).
	ErrRoot = errors.New("updaterworker: refusing to run as root: run nofx-updater as the bot's own user")
	// ErrBotCgroup: the worker must never share the bot's control group — the
	// unit is KillMode=control-group, so the activation's kill would kill the
	// worker with the bot, mid-job. Both unit names until R5 removes vl.
	ErrBotCgroup = errors.New("updaterworker: refusing to run inside the bot's service control group (vl or nofx — R5 removes the nofx name)")
	// ErrTZ: the library parses log line times in time.Local; a worker whose
	// zone differs from the bot's would misjudge every boot line's time.
	ErrTZ = errors.New("updaterworker: refusing to run with TZ set: the worker must read log times in the bot's own local zone")
)

// CheckProcess is the worker's start refusals: root, the bot's cgroup, TZ.
func CheckProcess() error {
	if geteuid() == 0 {
		return ErrRoot
	}
	if b, err := readCgroup(); err == nil && (bytes.Contains(b, []byte("/nofx.service")) || bytes.Contains(b, []byte("/vl.service"))) { // R5 removes the vl name
		return ErrBotCgroup
	}
	if _, set := lookupTZ(); set {
		return ErrTZ
	}
	return nil
}

// OSHost is the production Host.
type OSHost struct {
	// LockScript is <install>/deploy/vl-lock.sh. The worker runs its
	// status/acquire/release/check verbs. It NEVER reclaims and NEVER
	// clear-incomplete (a stale or incomplete lock is never taken over).
	LockScript string
}

func (OSHost) Now() time.Time { return time.Now() }

func (OSHost) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// runLockVerb runs `bash <LockScript> <verb> <args...>` and returns the
// combined output and the exit code. Every worker lock decision reads ONLY
// the script's verbs — never the lock directory directly.
func (h OSHost) runLockVerb(verb string, args ...string) (string, int, error) {
	if h.LockScript == "" || !filepath.IsAbs(h.LockScript) {
		return "", 0, errors.New("no lock script configured")
	}
	if _, err := os.Stat(h.LockScript); err != nil {
		return "", 0, fmt.Errorf("lock script: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", append([]string{h.LockScript, verb}, args...)...)
	out, err := cmd.CombinedOutput()
	rc := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return "", 0, fmt.Errorf("lock %s: %w", verb, err)
		}
		rc = ee.ExitCode()
	}
	return string(out), rc, nil
}

// lockHolderRe parses the script's status line: `held by 'X' (task: ...)`
// and `STALE — held by 'X' (task: ...)` both name the holder.
var lockHolderRe = regexp.MustCompile(`held by '([^']*)'`)

// MainTreeLockHeld was removed (P3c, worker-lock-cancel): it had no callers
// left after WORKER-TAKES-THE-LOCK — LockHolder is the worker's status read.

// LockHolder reports the current lock holder from `bash <LockScript> status`.
// "" when free. Held and STALE both name their holder. INCOMPLETE and
// ABANDONED-INCOMPLETE are errors — the worker never proceeds on a lock it
// cannot positively attribute, and never clears either shape.
func (h OSHost) LockHolder() (string, error) {
	out, rc, err := h.runLockVerb("status")
	if err != nil {
		return "", fmt.Errorf("lock status: %w", err)
	}
	switch rc {
	case 0, 2:
		// free (rc 0) or STALE (rc 2) — the stale line still names the
		// holder; parse it and let the caller decide (the worker refuses
		// a lock held by anyone else, stale included).
		if m := lockHolderRe.FindStringSubmatch(out); m != nil {
			return m[1], nil
		}
		return "", nil
	default:
		return "", fmt.Errorf("lock status refused (rc %d): %s", rc, firstLine(out))
	}
}

// LockAcquire runs `bash <LockScript> acquire <session> <task> <minutes>`,
// the SAME atomic acquire humans use. rc 0 = acquired (ours). rc 1 = held:
// holder names the holder parsed from the refusal's status block; "" when
// unparseable. Anything else is an error.
func (h OSHost) LockAcquire(session, task string, minutes int) (bool, string, error) {
	out, rc, err := h.runLockVerb("acquire", session, task, strconv.Itoa(minutes))
	if err != nil {
		return false, "", fmt.Errorf("lock acquire: %w", err)
	}
	switch rc {
	case 0:
		return true, "", nil
	case 1:
		if m := lockHolderRe.FindStringSubmatch(out); m != nil {
			return false, m[1], nil
		}
		return false, "", nil
	default:
		return false, "", fmt.Errorf("lock acquire refused (rc %d): %s", rc, firstLine(out))
	}
}

// LockRelease runs `bash <LockScript> release <session>`. Only the holder
// may release; the script refuses a non-holder (rc 1). A no-lock release
// prints "no lock" with rc 0 — idempotent.
func (h OSHost) LockRelease(session string) error {
	out, rc, err := h.runLockVerb("release", session)
	if err != nil {
		return fmt.Errorf("lock release: %w", err)
	}
	if rc != 0 {
		return fmt.Errorf("lock release refused (rc %d): %s", rc, firstLine(out))
	}
	return nil
}

// firstLine clips a script output for error strings and receipts.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// BuildInfo reads the binary's vcs stamps with debug/buildinfo (never by
// parsing `go version -m` text — CLASS 241).
func (OSHost) BuildInfo(binary string) (string, string, error) {
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return "", "", err
	}
	var rev, mod string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			mod = s.Value
		}
	}
	return rev, mod, nil
}

// ExeOf is the target of /proc/<pid>/exe (" (deleted)" kept: a replaced
// binary is not the install's binary).
func (OSHost) ExeOf(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("pid %d", pid)
	}
	return os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
}

// firstMarkerLine is the first non-empty, non-comment line of a RELEASE
// marker (kernel/boot_integrity.go expectedRevision's rule).
func firstMarkerLine(b []byte) string {
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" && !strings.HasPrefix(ln, "#") {
			return ln
		}
	}
	return ""
}

// revisionsAgree: the REPORTED revision may abbreviate the EXPECTED one (≥7
// chars, never longer) — activation's rule (health reports 12 chars, a RELEASE
// marker may be short).
func revisionsAgree(reported, expected string) bool {
	if reported == "" || expected == "" {
		return false
	}
	if reported == expected {
		return true
	}
	if len(reported) < 7 || len(reported) >= len(expected) {
		return false
	}
	return strings.HasPrefix(expected, reported)
}
