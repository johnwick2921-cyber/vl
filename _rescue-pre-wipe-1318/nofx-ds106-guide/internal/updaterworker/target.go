package updaterworker

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/joho/godotenv"

	"vl/internal/installpath"
)

// Target is the installation one worker acts on. It is resolved exactly as
// the bot (main.go: loadDotEnv → config.Init → resolveMaintenanceDataDir), the
// hold CLI (internal/holdcli.DataDirFor) and the attended bootstrap
// (updaterbootstrap.resolveTarget, PR #200 fold F3) resolve it — a worker that
// held a file the bot never reads would hold nothing
// (TestDataDirMatchesHoldCLIAndBot).
type Target struct {
	InstallDir string // the bot's WorkingDirectory (absolute)
	DBFile     string // the bot's database
	DataDir    string // its directory: <data>/updater/{jobs,worker.sock,...}
	LogDir     string // <install>/data — the bot's logger writes vl_<boot date>.log relative to its cwd
	Port       int    // the bot's API port (API_SERVER_PORT in <install>/.env, else 8080)
	Source     string // where DB_PATH came from (operator text)
}

// ResolveTarget resolves the installation at installDir (absolute). It refuses,
// as the attended bootstrap does: a relative install dir; an <install>/.env that
// exists but cannot be read or parsed (the worker could not see the DB_PATH the
// bot uses); and a process DB_PATH that names a different file than the
// installation's own (the shipped unit sets none, so the bot uses the .env's).
func ResolveTarget(installDir string) (Target, error) {
	if installDir == "" || !filepath.IsAbs(installDir) {
		return Target{}, fmt.Errorf("updaterworker: --install-dir must be an absolute path (got %q)", installDir)
	}
	installDir = filepath.Clean(installDir)
	o := installpath.ReadDBPathOrigin(installDir)
	switch o.DotEnvState {
	case installpath.DotEnvDenied:
		return Target{}, fmt.Errorf("updaterworker: %s exists but this user cannot read it — run the worker as the bot's own user", o.DotEnvFile)
	case installpath.DotEnvUnreadable:
		// never the parse error's text: godotenv quotes the file in it
		return Target{}, fmt.Errorf("updaterworker: %s exists but could not be read or parsed — the worker cannot see the DB_PATH the bot uses", o.DotEnvFile)
	}
	own := o.FromInstallation()
	t := Target{
		InstallDir: installDir,
		DBFile:     installpath.DBFile(installDir, own),
		DataDir:    installpath.DataDir(installDir, own),
		LogDir:     filepath.Join(installDir, "data"),
		Port:       8080,
		Source:     "the installation's DB_PATH (" + own + ")",
	}
	if o.InProcess {
		if procFile := installpath.DBFile(installDir, o.Effective()); procFile != t.DBFile {
			return Target{}, fmt.Errorf("updaterworker: DB_PATH differs between this process (%s) and the installation (%s); "+
				"a bot started from %s uses the installation's, so this worker would hold a file the bot never reads — unset DB_PATH and re-run",
				procFile, t.DBFile, installDir)
		}
	}
	// The port the BOT listens on: the unit sets no environment, so it is the
	// .env's API_SERVER_PORT (config.go), else 8080 — never this process's env.
	if o.DotEnvState == installpath.DotEnvRead {
		vals, err := godotenv.Read(o.DotEnvFile)
		if err != nil {
			return Target{}, fmt.Errorf("updaterworker: %s could not be re-read", o.DotEnvFile)
		}
		if v := vals["API_SERVER_PORT"]; v != "" {
			p, err := strconv.Atoi(v)
			if err != nil || p <= 0 || p > 65535 {
				return Target{}, fmt.Errorf("updaterworker: API_SERVER_PORT in %s is not a port", o.DotEnvFile)
			}
			t.Port = p
		}
	}
	return t, nil
}

// HealthURL is the bot's loopback health endpoint (no auth).
func (t Target) HealthURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/api/health", t.Port)
}

// BaseURL is the bot's loopback API base.
func (t Target) BaseURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", t.Port)
}

// InstallRelease is the running install's three halves as the activation
// library addresses them (brief C4: built directly, never Resolved — the
// install is the TARGET of Activate, not a release dir). sha is the running
// binary's vcs.revision.
func (t Target) InstallRelease(sha string) Release {
	return Release{
		Dir:         t.InstallDir,
		SHA:         sha,
		Binary:      t.InstallBinaryPath(),
		Dist:        filepath.Join(t.InstallDir, "web", "dist"),
		ReleaseFile: filepath.Join(t.InstallDir, "deploy", "RELEASE"),
	}
}

// InstallBinaryPath is the install's binary: vl-bin when present, else
// nofx-bin — on the INSTALL side vl wins when both exist (a rename in
// flight may leave both). // R5 removes the vl branch.
func (t Target) InstallBinaryPath() string {
	if st, err := os.Stat(filepath.Join(t.InstallDir, "vl-bin")); err == nil && st.Mode().IsRegular() {
		return filepath.Join(t.InstallDir, "vl-bin")
	}
	return filepath.Join(t.InstallDir, "nofx-bin")
}
