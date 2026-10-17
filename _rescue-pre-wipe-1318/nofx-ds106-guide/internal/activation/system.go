package activation

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// system is the seam between this package and the machine. It exists so the
// refusals can be TESTED — a recycled pid, a process that never comes back, a
// boot line that predates the kill — none of which can be produced reliably by
// signalling real processes in a test.
//
// It is a package-level var rather than a parameter because §2's signatures are
// the contract the worker (3b-B) imports; the seam must not leak into them.
type system struct {
	// ReadStat returns the raw /proc/<pid>/stat line.
	ReadStat func(pid int) (string, error)
	// Kill sends SIGKILL. SIGTERM exits 0 and systemd's Restart=on-failure
	// does NOT relaunch, so the process would simply stay down.
	Kill func(pid int) error
	// MainPID reads systemd's notion of the unit's main process. NEVER pgrep:
	// `pgrep -f vl-bin` also matches `go version -m vl-bin`, and a pattern
	// can match the very shell that runs it (CLASS 242). The unit is vl,
	// falling back to vl while the rename is in flight (R5 removes the
	// vl unit and the fallback).
	MainPID func() (int, error)
	Now     func() time.Time
	Sleep   func(time.Duration)
}

// mainPIDOf runs `systemctl show -p MainPID --value <unit>` and returns the
// pid, or the error. // R5 removes the unit list with the rename.
func mainPIDOf(unit string) (int, error) {
	out, err := exec.Command("systemctl", "show", "-p", "MainPID", "--value", unit).Output()
	if err != nil {
		return 0, fmt.Errorf("systemctl show MainPID (%s): %w", unit, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("MainPID (%s) %q is not a pid: %w", unit, strings.TrimSpace(string(out)), err)
	}
	return pid, nil
}

// readMainPID walks the unit list through the reader: vl first (R5 removes
// the vl fallback); a unit that is absent or stopped reports 0 or errors,
// and then the next unit answers.
func readMainPID(mainPIDOf func(string) (int, error)) (int, error) {
	for _, unit := range []string{"vl", "nofx"} {
		pid, err := mainPIDOf(unit)
		if err != nil {
			continue
		}
		if pid <= 1 {
			continue
		}
		return pid, nil
	}
	return 0, fmt.Errorf("MainPID is 0 — the unit is not running")
}

func defaultSystem() *system {
	return &system{
		ReadStat: func(pid int) (string, error) {
			b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
			return string(b), err
		},
		Kill:    func(pid int) error { return syscall.Kill(pid, syscall.SIGKILL) },
		MainPID: func() (int, error) { return readMainPID(mainPIDOf) },
		Now:     time.Now,
		Sleep:   time.Sleep,
	}
}

var sys = defaultSystem()

// CurrentIdentity reads the running unit's identity: its MainPID together with
// the start time that distinguishes it from any later process reusing that pid.
func CurrentIdentity() (Identity, error) {
	pid, err := sys.MainPID()
	if err != nil {
		return Identity{}, err
	}
	line, err := sys.ReadStat(pid)
	if err != nil {
		return Identity{}, fmt.Errorf("cannot read /proc/%d/stat: %w", pid, err)
	}
	ticks, err := statField22(line)
	if err != nil {
		return Identity{}, fmt.Errorf("pid %d: %w", pid, err)
	}
	return Identity{PID: pid, StartTicks: ticks}, nil
}

// stillAlive reports whether the pid is STILL the process this Identity names.
// A pid that has been recycled is a DIFFERENT process wearing the same number,
// and signalling it would kill an innocent bystander.
func (id Identity) stillAlive() (bool, error) {
	line, err := sys.ReadStat(id.PID)
	if err != nil {
		return false, nil // gone
	}
	ticks, err := statField22(line)
	if err != nil {
		return false, err
	}
	return ticks == id.StartTicks, nil
}

// IdentityOf reads the identity of a pid the caller named. A pid on its own is
// not an identity: the start-ticks are read here too, so a number that has been
// recycled since the caller looked it up is refused by the same guard.
func IdentityOf(pid int) (Identity, error) {
	line, err := sys.ReadStat(pid)
	if err != nil {
		return Identity{}, fmt.Errorf("cannot read /proc/%d/stat: %w", pid, err)
	}
	ticks, err := statField22(line)
	if err != nil {
		return Identity{}, fmt.Errorf("pid %d: %w", pid, err)
	}
	return Identity{PID: pid, StartTicks: ticks}, nil
}

// NewestLogPath returns the log the running process is actually writing.
//
// LOGS ARE NAMED BY BOOT DATE, NOT CALENDAR DATE. On the live box at 08:04 on
// 2026-09-24 the active file was data/vl_2026-09-23.log, because the process
// booted the previous evening. Anything that builds the path as
// vl_$(date +%F).log — as the v6 script did — points at a file that may not
// exist, and then a Watch fails for a reason that has nothing to do with the
// activation.
func NewestLogPath(dir string) (string, error) {
	// Both prefixes: a vl-boot names its log vl_, a nofx-boot nofx_; the
	// newest of either is what the running process is writing. // R5 removes
	// the vl glob.
	var hits []string
	for _, pat := range []string{"vl_*.log", "nofx_*.log"} {
		h, err := filepath.Glob(filepath.Join(dir, pat))
		if err != nil {
			return "", err
		}
		hits = append(hits, h...)
	}
	if len(hits) == 0 {
		return "", fmt.Errorf("no vl_*.log or nofx_*.log in %s", dir)
	}
	newest, newestAt := "", time.Time{}
	for _, h := range hits {
		st, err := os.Stat(h)
		if err != nil {
			continue
		}
		if st.ModTime().After(newestAt) {
			newest, newestAt = h, st.ModTime()
		}
	}
	if newest == "" {
		return "", fmt.Errorf("no readable vl_*.log or nofx_*.log in %s", dir)
	}
	return newest, nil
}
