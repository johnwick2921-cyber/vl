// Command vl-activate drives the activation library from a terminal.
//
// It exists so the ATTENDED boot and the unattended worker (3b-B) run the SAME
// code. deploy/cutover.sh v6 was a second implementation of these steps in
// bash; two implementations of a safety procedure drift, and the one that
// drifts is the one nobody runs until an incident.
//
// Every subcommand prints ONE receipt as JSON — on success and on refusal
// alike, because the evidence is most valuable exactly when the step failed —
// and exits non-zero on any refusal.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vl/internal/activation"
	"vl/internal/envcompat"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	var (
		relDir    = fs.String("release", "", "release directory (VL_RELEASE_DIR/NOFX_RELEASE_DIR/<sha>)")
		prevDir   = fs.String("prev", "", "previous release directory, for activate/rollback")
		install   = fs.String("install", "", "install directory the running process reads from")
		dbPath    = fs.String("db", "data/data.db", "sqlite database to back up")
		dest      = fs.String("dest", "", "backup destination file")
		logPath   = fs.String("log", "", "the bot's log file; empty = the NEWEST data/vl_*.log (nofx_* while the rename is in flight; logs are named by BOOT date, not today's date)")
		healthURL = fs.String("health", "http://127.0.0.1:8080/api/health", "health endpoint")
		within    = fs.Duration("within", 90*time.Second, "how long watch waits")
		pid       = fs.Int("pid", 0, "pid to replace; 0 = read it from systemd")
	)
	_ = fs.Parse(os.Args[2:])

	rc, err := run(cmd, opts{
		relDir: *relDir, prevDir: *prevDir, install: *install,
		dbPath: *dbPath, dest: *dest, logPath: *logPath,
		healthURL: *healthURL, within: *within, pid: *pid,
	})
	emit(rc)
	if err != nil {
		// The message goes to stderr so the receipt on stdout stays parseable
		// by a caller that is piping it.
		fmt.Fprintf(os.Stderr, "nofx-activate %s: %v\n", cmd, err)
		os.Exit(1)
	}
}

type opts struct {
	relDir, prevDir, install string
	dbPath, dest             string
	logPath, healthURL       string
	within                   time.Duration
	pid                      int
}

func run(cmd string, o opts) (activation.Receipt, error) {
	switch cmd {
	case "verify", "stage":
		rel, err := activation.Resolve(o.relDir)
		if err != nil {
			return activation.Receipt{Step: cmd, OK: false, Err: err.Error()}, err
		}
		if cmd == "verify" {
			// verify is stage WITHOUT the promise to install: same proofs, and
			// it is the one subcommand that is safe to run against a live box.
			return activation.Stage(rel)
		}
		return activation.Stage(rel)

	case "backup":
		dest := o.dest
		if dest == "" {
			dest = filepath.Join(envcompat.BackupRoot(), "updater", // R5 removes: vl-backups wins, nofx-backups only while ~/nofx exists
				time.Now().Format("20060102-150405"), "data.db")
		}
		return activation.Backup(o.dbPath, dest)

	case "watch":
		rel, err := activation.Resolve(o.relDir)
		if err != nil {
			return activation.Receipt{Step: cmd, OK: false, Err: err.Error()}, err
		}
		id, err := identity(o.pid)
		if err != nil {
			return activation.Receipt{Step: cmd, OK: false, Err: err.Error()}, err
		}
		logPath := o.logPath
		if logPath == "" {
			// Logs are named by BOOT date, not calendar date: on the live box
			// at 08:04 on 09-24 the active file was vl_2026-09-23.log. A
			// date-built path points at a file that may not exist, and then
			// Watch fails for a reason unrelated to the activation.
			p, lerr := activation.NewestLogPath("data")
			if lerr != nil {
				return activation.Receipt{Step: cmd, OK: false, Err: lerr.Error()}, lerr
			}
			logPath = p
		}
		// Since is left zero here: the attended CLI watches a restart it just
		// performed, so "now" is the correct anchor. A worker resuming from
		// persisted state passes its recorded kill instant instead.
		return activation.Watch(rel, id, activation.WatchOpts{
			LogPath: logPath, HealthURL: o.healthURL, Within: o.within,
		})

	case "activate":
		rel, err := activation.Resolve(o.relDir)
		if err != nil {
			return activation.Receipt{Step: cmd, OK: false, Err: err.Error()}, err
		}
		prev, err := activation.Resolve(o.prevDir)
		if err != nil {
			return activation.Receipt{Step: cmd, OK: false, Err: err.Error()}, err
		}
		id, err := identity(o.pid)
		if err != nil {
			return activation.Receipt{Step: cmd, OK: false, Err: err.Error()}, err
		}
		_, rc, err := activation.Activate(rel, prev, id)
		return rc, err

	case "rollback":
		prev, err := activation.Resolve(o.prevDir)
		if err != nil {
			return activation.Receipt{Step: cmd, OK: false, Err: err.Error()}, err
		}
		id, err := identity(o.pid)
		if err != nil {
			return activation.Receipt{Step: cmd, OK: false, Err: err.Error()}, err
		}
		if o.install != "" {
			inst, err := activation.Resolve(o.install)
			if err != nil {
				return activation.Receipt{Step: cmd, OK: false, Err: err.Error()}, err
			}
			_, rc, err := activation.RollbackTo(prev, inst, id)
			return rc, err
		}
		_, rc, err := activation.Rollback(prev, id)
		return rc, err

	default:
		usage()
		return activation.Receipt{Step: cmd, OK: false, Err: "unknown subcommand"},
			fmt.Errorf("unknown subcommand %q", cmd)
	}
}

// identity reads the running unit's identity unless the caller named a pid.
// A pid given on the command line still gets its start-ticks read, so the
// recycled-pid guard applies to it too — a number is not an identity.
func identity(pid int) (activation.Identity, error) {
	if pid == 0 {
		return activation.CurrentIdentity()
	}
	return activation.IdentityOf(pid)
}

func emit(rc activation.Receipt) {
	b, err := json.MarshalIndent(rc, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot render receipt: %v\n", err)
		return
	}
	fmt.Println(string(b))
}

func usage() {
	fmt.Fprint(os.Stderr, `nofx-activate <verify|stage|backup|watch|activate|rollback> [flags]

  verify   -release DIR                 prove a release's binary; safe on a live box
  stage    -release DIR                 same proofs, as the install step
  backup   -db PATH [-dest FILE]        online copy + integrity_check
  watch    -release DIR -log FILE       boot line (post-restart) AND health must agree
  activate -release DIR -prev DIR       install three halves, guarded kill, new identity
  rollback -prev DIR [-install DIR]     restore the previous release, guarded kill

Every subcommand prints one JSON receipt and exits non-zero on a refusal.
`)
}
