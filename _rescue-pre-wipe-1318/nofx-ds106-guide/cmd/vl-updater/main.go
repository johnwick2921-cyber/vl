// Command vl-updater is the M4 updater worker (wave 3b-B) and its attended
// operator verbs. It is the ONLY place a resume request is built
// (internal/updaterwire/resume_census_test.go admits exactly this directory):
//
//	vl-updater [--install-dir d] serve              run the worker (one job at a time)
//	vl-updater [--install-dir d] fetch <release_id> materialize + verify a local release (attended)
//	vl-updater [--install-dir d] status [<job>]     the worker's state, or one job's file
//	vl-updater [--install-dir d] resume <job>       attended resume of a job parked at nt8_updated
//	vl-updater [--install-dir d] recovery <job>     the manual steps for a recovery_needed job
//
// It never runs as root, never touches the hold (only the worker's census-
// admitted hold.go does), never mints a token (serve reads the operator's
// NOFX_CUTOVER_TOKEN from its environment) and never runs a step itself.
//
// fetch is wired (U4N item B): it only verifies a LOCAL archive into the
// release root and writes its verdict — it installs nothing.
//
// L4: serve REFUSES unless both production adapters are wired — the release
// re-proof (U4N) and the activation library (NewActivationLibrary) — and, even
// then, only an operator who starts `serve` with its token, a home for the
// backups and the worker lock runs anything (TestServeRefusesUnlessBothAdaptersAreWired).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"vl/internal/envcompat"
	"vl/internal/updaterjob"
	"vl/internal/updaterwire"
	"vl/internal/updaterwire/wireserver"
	"vl/internal/updaterworker"
	"vl/safe"
)

// Seams (tests only).
var (
	geteuid      = os.Geteuid
	isTerminal   = stdinIsTerminal
	checkProcess = updaterworker.CheckProcess
	getwd        = os.Getwd
	// serveContext ends serve (SIGINT/SIGTERM in production).
	serveContext = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	}
)

// The two production adapters. serve refuses unless BOTH are wired (L4).
//
//   - newReverifier: the release re-proof — updaterworker.NewReleaseReverifier
//     over U3's updaterjob.ReadVerdict / RehashRelease / ReverifyRelease
//     against <install>/deploy/release_allowed_signers (U4N item A). WIRED.
//   - newLibrary: the activation library adapter —
//     updaterworker.NewActivationLibrary, one-line delegations to
//     vl/internal/activation. WIRED. It needs 103's D4 fix (activation
//     registers the ONE sqlite driver, store/sqlitedriver's): without it this
//     binary panics at init with "sql: Register called twice for driver
//     sqlite" (TestUpdaterBinaryInitsWithoutPanic,
//     TestNoBinaryLinkingTheWorkerSetRegistersADuplicateSQLDriver).
var (
	newLibrary    = updaterworker.NewActivationLibrary
	newReverifier = updaterworker.NewReleaseReverifier
)

// releaseInboxEnv names the local inbox the owner fills with release assets
// (`gh release download`); fetch reads <inbox>/<release_id>.tar.gz. There is
// no network source in v1 (brief C9).
const releaseInboxEnv = "NOFX_RELEASE_INBOX"

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

const usage = "usage: vl-updater [--install-dir d] serve | fetch <release_id> | status [<job>] | resume <job> | recovery <job>"

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// The worker's three env fallbacks (cutover token, inbox, service dir)
	// WARN through envcompat; the updater never inits the logger, so its
	// sink is stderr. // R5 removes with envcompat.
	envcompat.SetWarnSink(func(m string) { fmt.Fprintln(stderr, "vl-updater:", m) })
	top := flag.NewFlagSet("vl-updater", flag.ContinueOnError)
	top.SetOutput(stderr)
	wd, _ := getwd()
	installDir := top.String("install-dir", wd, "the bot's WorkingDirectory (its .env and DB_PATH decide the data dir)")
	if err := top.Parse(args); err != nil {
		return 2
	}
	rest := top.Args()
	if len(rest) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if geteuid() == 0 {
		fmt.Fprintln(stderr, "vl-updater: refusing to run as root: run it as the bot's own user (a root-owned data/updater would lock the bot into a hold it cannot read)")
		return 2
	}
	verb, operands := rest[0], rest[1:]
	want := map[string][2]int{"serve": {0, 0}, "fetch": {1, 1}, "status": {0, 1}, "resume": {1, 1}, "recovery": {1, 1}}
	n, ok := want[verb]
	if !ok {
		fmt.Fprintf(stderr, "vl-updater: unknown subcommand %q\n%s\n", verb, usage)
		return 2
	}
	if len(operands) < n[0] || len(operands) > n[1] {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	t, err := updaterworker.ResolveTarget(*installDir)
	if err != nil {
		fmt.Fprintln(stderr, "vl-updater:", err)
		return 2
	}
	switch verb {
	case "serve":
		return serve(t, stderr)
	case "fetch":
		return fetch(t, operands[0], stdout, stderr)
	case "status":
		if len(operands) == 0 {
			return statusWorker(t, stdout, stderr)
		}
		return statusJob(t, operands[0], stdout, stderr)
	case "resume":
		return resume(t, operands[0], stdin, stdout, stderr)
	case "recovery":
		return recovery(t, operands[0], stdout, stderr)
	}
	return 2
}

// lockScriptFor is the deploy lock script for this install: vl-lock.sh when
// present, else vl-lock.sh. Only `check` is ever run. // R5 removes the
// vl branch.
func lockScriptFor(installDir string) string {
	if _, err := os.Stat(filepath.Join(installDir, "deploy", "vl-lock.sh")); err == nil {
		return filepath.Join(installDir, "deploy", "vl-lock.sh")
	}
	return filepath.Join(installDir, "deploy", "nofx-lock.sh")
}

// serve refuses, in order: the process checks (root, the bot's cgroup, TZ),
// a missing cutover token, an adapter that has not landed, no home for the
// backups, the worker lock (a second serve writes nothing), and the start
// sweep (more than one unfinished job). Only then does it print its start line (READ; n/a when absent) and serve until
// SIGINT/SIGTERM.
func serve(t updaterworker.Target, stderr io.Writer) int {
	if err := checkProcess(); err != nil {
		fmt.Fprintln(stderr, "vl-updater serve:", err)
		return 2
	}
	app, err := updaterworker.NewHTTPApp(t.BaseURL())
	if err != nil {
		fmt.Fprintln(stderr, "vl-updater serve:", err)
		return 2
	}
	lib, lerr := newLibrary()
	rel, rerr := newReverifier(t)
	if lerr != nil || rerr != nil || lib == nil || rel == nil {
		fmt.Fprintf(stderr, "vl-updater serve: %v — activation library adapter: %s · release re-proof adapter: %s; refusing to start (install dir %s, data dir %s); nothing was written\n",
			updaterworker.ErrNotWired, adapterState(lib != nil, lerr), adapterState(rel != nil, rerr), t.InstallDir, t.DataDir)
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		fmt.Fprintln(stderr, "vl-updater serve: no home directory for ~/vl-backups/updater")
		return 2
	}
	logf := func(format string, a ...any) { fmt.Fprintf(stderr, format+"\n", a...) }
	// The lock script is deploy/vl-lock.sh when present, else nofx-lock.sh
	// (the rename lands in R5; until then either may be shipped). Only its
	// `check` verb is ever run.
	lockScript := lockScriptFor(t.InstallDir)
	w, err := updaterworker.New(updaterworker.Config{
		Target:     t,
		BackupRoot: filepath.Join(envcompat.BackupRoot(), "updater"),
		Budgets:    updaterworker.DefaultBudgets(),
		Logf:       logf,
	}, updaterworker.Deps{Lib: lib, App: app, Rel: rel, Host: updaterworker.OSHost{LockScript: lockScript}})
	if err != nil {
		fmt.Fprintln(stderr, "vl-updater serve:", err)
		return 2
	}
	path, err := updaterwire.SocketPath(t.DataDir)
	if err != nil {
		fmt.Fprintln(stderr, "vl-updater serve:", err)
		return 2
	}
	// The worker lock FIRST (verifier D1): Listen takes the single-worker
	// flock, and only its holder may run the start sweep or the runner — a
	// second serve that is refused here has written nothing to any job.
	ln, err := wireserver.Listen(path, logf)
	if err != nil {
		fmt.Fprintln(stderr, "vl-updater serve:", err)
		return 2
	}
	defer ln.Close()
	ctx, stop := serveContext()
	defer stop()
	rep, err := w.Start(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "vl-updater serve:", err)
		return 2
	}
	warnSelfUpdateRestart(stderr)
	fmt.Fprintf(stderr, "🔧 vl-updater: serving %s · install %s · data %s · active=%s · recovery_needed=%s · stale_at_start=%s\n",
		path, t.InstallDir, t.DataDir, na(rep.Active), na(strings.Join(rep.Recovery, ",")), na(strings.Join(rep.StaleAtStart, ",")))
	done := make(chan error, 1)
	safe.GoNamed("updater-http-serve", func() { done <- ln.Serve(w.Handle) })
	select {
	case <-ctx.Done():
		ln.Close()
		<-done
		return 0
	case <-w.SwapDone():
		// worker-self-update: the worker swapped its own binary; exit so
		// systemd restarts the unit on the new one (the unit's Restart=
		// policy decides — see the PR body; today it is Restart=no).
		fmt.Fprintln(stderr, "vl-updater serve: worker self-update swapped the binary; exiting for the unit restart")
		ln.Close()
		<-done
		return 0
	case err := <-done:
		fmt.Fprintln(stderr, "vl-updater serve:", err)
		return 1
	}
}

// fetch is the ATTENDED pre-fetch (brief C9): it verifies the local archive
// <$NOFX_RELEASE_INBOX>/<release_id>.tar.gz against the INSTALL's
// deploy/release_allowed_signers (C6/C7: absent ⇒ refused), materializes it
// under $NOFX_RELEASE_DIR/<source_sha> (the one release-dir resolver,
// installpath.ReleaseDir; never inside the install) and writes the verdict
// into the installation's data dir — U3's FetchRelease does all of it, in
// its order. No network code; nothing is written on a refusal. It prints the
// verdict's release id and source sha, never a key.
func fetch(t updaterworker.Target, releaseID string, stdout, stderr io.Writer) int {
	refuse := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "vl-updater fetch: "+format+"; nothing was written\n", a...)
		return 2
	}
	if !updaterwire.ValidReleaseID(releaseID) {
		return refuse("invalid release id %q", releaseID)
	}
	inbox, _ := envcompat.Env("RELEASE_INBOX") // R5 removes: VL_/NOFX_ prefix is envcompat's business
	inbox = strings.TrimSpace(inbox)
	if inbox == "" {
		return refuse("%s is not set (the local inbox holding %s.tar.gz — there is no network fetch)", releaseInboxEnv, releaseID)
	}
	if !filepath.IsAbs(inbox) {
		return refuse("%s=%q must be an absolute path", releaseInboxEnv, inbox)
	}
	// the ONE release-root check (the re-proof adapter makes the same one):
	// unset, relative, not its own resolved path, or inside the RESOLVED
	// install — compared by path elements — refuses
	root, err := updaterworker.ReleaseRoot(t.InstallDir)
	if err != nil {
		return refuse("%v", err)
	}
	v, err := updaterworker.FetchRelease(updaterworker.FetchConfig{
		Archive:        filepath.Join(inbox, releaseID+".tar.gz"),
		ReleaseID:      releaseID,
		ReleaseRoot:    root,
		AllowedSigners: updaterworker.ReleaseAllowedSignersPath(t.InstallDir),
		DataDir:        t.DataDir,
	})
	if err != nil {
		fmt.Fprintf(stderr, "vl-updater fetch %s: refused: %v\n", releaseID, err)
		return 1
	}
	fmt.Fprintf(stdout, "release %s verified: source %s · release dir %s · verdict written to %s\n",
		v.ReleaseID, v.SourceSHA, v.ReleaseDir, filepath.Join(t.DataDir, updaterwire.UpdaterDirName, "verdicts"))
	return 0
}

// statusWorker asks the running worker (status with no job id).
func statusWorker(t updaterworker.Target, stdout, stderr io.Writer) int {
	c, err := updaterwire.DialWorker(t.DataDir)
	if err != nil {
		fmt.Fprintf(stderr, "vl-updater status: no worker answers (%v)\n", err)
		return 1
	}
	defer c.Close()
	resp, err := c.Do(updaterwire.NewStatus(""))
	if err != nil {
		fmt.Fprintln(stderr, "vl-updater status:", err)
		return 1
	}
	return printResponse(resp, stdout, stderr)
}

// statusJob reads one job's file (no worker needed): the API view plus the
// fields an operator needs, READ; absent ones print n/a.
func statusJob(t updaterworker.Target, jobID string, stdout, stderr io.Writer) int {
	j, err := updaterjob.Read(t.DataDir, jobID)
	if err != nil {
		fmt.Fprintf(stderr, "vl-updater status %s: %v\n", jobID, err)
		return 1
	}
	v := updaterjob.View(j)
	out := map[string]any{"job_id": v.JobID, "release_id": j.ReleaseID, "state": v.State, "phase": j.Phase, "attempts": j.Attempts,
		"receipts": len(j.Receipts), "blocker": na(j.Blocker), "error": na(j.Error), "recovery_reason": na(j.RecoveryReason)}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Fprintln(stdout, string(b))
	return 0
}

// resume is the ATTENDED resume from nt8_updated: a terminal, the job id
// typed back, then the one resume frame the module ever builds.
func resume(t updaterworker.Target, jobID string, stdin io.Reader, stdout, stderr io.Writer) int {
	if !updaterwire.ValidJobID(jobID) {
		fmt.Fprintln(stderr, "vl-updater resume: invalid job id")
		return 2
	}
	if !isTerminal(stdin) {
		fmt.Fprintln(stderr, "vl-updater resume: refusing — a resume is attended: run it from a terminal and type the job id")
		return 2
	}
	j, err := updaterjob.Read(t.DataDir, jobID)
	if err != nil {
		fmt.Fprintf(stderr, "vl-updater resume %s: %v\n", jobID, err)
		return 1
	}
	fmt.Fprintf(stdout, "job %s (release %s) is %s/%s.\nblocker: %s\n", j.JobID, j.ReleaseID, j.State, j.Phase, na(j.Blocker))
	fmt.Fprint(stdout, "Only after the AddOn is compiled (copy → F5 → full NT8 restart): type the job id to resume: ")
	line, err := bufio.NewReader(io.LimitReader(stdin, 256)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(stderr, "vl-updater resume:", err)
		return 2
	}
	if strings.TrimSpace(line) != jobID {
		fmt.Fprintln(stderr, "vl-updater resume: the typed id does not match; nothing was sent")
		return 2
	}
	c, err := updaterwire.DialWorker(t.DataDir)
	if err != nil {
		fmt.Fprintf(stderr, "vl-updater resume: no worker answers (%v)\n", err)
		return 1
	}
	defer c.Close()
	resp, err := c.Do(updaterwire.NewResume(jobID))
	if err != nil {
		fmt.Fprintln(stderr, "vl-updater resume:", err)
		return 1
	}
	return printResponse(resp, stdout, stderr)
}

// recovery prints the job-specific manual steps (C16).
func recovery(t updaterworker.Target, jobID string, stdout, stderr io.Writer) int {
	j, err := updaterjob.Read(t.DataDir, jobID)
	if err != nil {
		fmt.Fprintf(stderr, "vl-updater recovery %s: %v\n", jobID, err)
		return 1
	}
	fmt.Fprint(stdout, updaterworker.RecoveryText(j, t))
	return 0
}

func printResponse(resp updaterwire.Response, stdout, stderr io.Writer) int {
	if resp.OK {
		fmt.Fprintln(stdout, resp.State)
		return 0
	}
	fmt.Fprintln(stderr, "refused:", resp.Error)
	return 1
}

// adapterState is one adapter's state for serve's refusal line (READ).
func adapterState(ok bool, err error) string {
	switch {
	case err != nil:
		return "missing (" + err.Error() + ")"
	case !ok:
		return "missing (nil)"
	}
	return "wired"
}

func na(s string) string {
	if s == "" {
		return "n/a"
	}
	return s
}

// warnSelfUpdateRestart (worker-self-update P1): when the self-update knob is
// ON, a swap ends in a worker exit that only systemd can turn into a restart —
// warn when the running unit's Restart policy won't. The line is READ, never
// literal: n/a when systemctl is unavailable or the unit is not loaded.
func warnSelfUpdateRestart(stderr io.Writer) {
	v, _ := envcompat.Env("UPDATER_SELF_UPDATE")
	if v != "1" {
		return
	}
	out, err := exec.Command("systemctl", "--user", "show", "vl-updater", "-p", "Restart").Output()
	if err != nil {
		fmt.Fprintln(stderr, "🔧 vl-updater serve: worker self-update ON but the unit Restart policy is n/a (systemctl unavailable) — a swap will leave the worker down until the owner restarts it")
		return
	}
	restart := strings.TrimSpace(strings.TrimPrefix(string(out), "Restart="))
	if restart != "always" && restart != "on-success" {
		fmt.Fprintf(stderr, "🔧 vl-updater serve: worker self-update ON but the unit Restart=%s — a swap will leave the worker down until the owner restarts it\n", restart)
	}
}
