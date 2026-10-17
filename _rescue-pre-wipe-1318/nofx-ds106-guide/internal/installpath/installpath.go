// Package installpath is the ONE resolver for where an installation keeps its
// mutable state (W-ONE-BUTTON M2, MUST-2). The running bot (main.go via
// config.Init) and the operator CLI (cmd/maintenance-hold) both resolve the
// data directory here, so a hold the CLI writes is the hold the bot reads — a
// CLI that holds a file the bot never reads is a hold that does not exist.
//
// It is a leaf package on purpose: config imports logger/telemetry/mcp and
// store imports config, so neither can host a resolver the CLI can use
// without their side effects.
package installpath

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// DefaultDBPath is the database path when DB_PATH is unset (config.Init's
// historical default).
const DefaultDBPath = "data/data.db"

// DBPath resolves the database path exactly as config.Init does: DB_PATH if
// set and non-empty, else DefaultDBPath.
func DBPath(getenv func(string) string) string {
	if v := getenv("DB_PATH"); v != "" {
		return v
	}
	return DefaultDBPath
}

// DataDir is the directory of dbPath, anchored on workDir when dbPath is
// relative (the bot resolves relative paths against its WorkingDirectory),
// always absolute.
// DBFile is the absolute database file path, anchored on workDir when dbPath
// is relative (the bot resolves relative paths against its WorkingDirectory).
func DBFile(workDir, dbPath string) string {
	p := dbPath
	if !filepath.IsAbs(p) {
		p = filepath.Join(workDir, p)
	}
	return filepath.Clean(p)
}

func DataDir(workDir, dbPath string) string {
	return filepath.Dir(DBFile(workDir, dbPath))
}

// DotEnvGetenv returns a getenv that answers the way the bot's environment
// looks after main.go's godotenv.Load("<installDir>/.env"): a variable already
// in the process environment wins; <installDir>/.env fills the gaps; an
// absent or unreadable .env means the process environment only.
func DotEnvGetenv(installDir string) func(string) string {
	vals, err := godotenv.Read(filepath.Join(installDir, ".env"))
	if err != nil {
		vals = nil
	}
	return func(k string) string {
		if v, ok := os.LookupEnv(k); ok {
			return v
		}
		return vals[k]
	}
}

// What happened to <installDir>/.env when DBPathOrigin read it.
const (
	DotEnvRead       = "read"       // read and parsed
	DotEnvAbsent     = "absent"     // no such file (the bot then logs ".env absent")
	DotEnvDenied     = "denied"     // exists, permission denied to THIS process
	DotEnvUnreadable = "unreadable" // exists, not readable or not parseable (the bot then logs "⚠️ .env NOT loaded")
)

// DBPathOrigin is where DB_PATH comes from for one installation, with the
// two places kept APART (PR #200 fold F3). It is beside DotEnvGetenv, not a
// change to it: DotEnvGetenv stays the bot's and cmd/maintenance-hold's
// resolver, and Effective() is exactly DBPath(DotEnvGetenv(installDir)) (a
// test pins the two together).
//
// An operator CLI needs the split because the process environment it runs
// in is the OPERATOR's, not the bot's: a bot started by the shipped systemd
// unit (WorkingDirectory=<install>, no DB_PATH in the unit) resolves
// FromInstallation(), whatever the operator's shell exports.
type DBPathOrigin struct {
	DotEnvFile   string // <installDir>/.env
	DotEnvState  string // DotEnvRead, DotEnvAbsent, DotEnvDenied or DotEnvUnreadable
	InDotEnv     bool   // the .env was read and defines DB_PATH (possibly empty)
	DotEnvValue  string // meaningful only when InDotEnv
	InProcess    bool   // the process environment defines DB_PATH (possibly empty)
	ProcessValue string // meaningful only when InProcess
}

// ReadDBPathOrigin reads <installDir>/.env with the same parser DotEnvGetenv
// (and the bot's godotenv.Load) uses, and the process environment. It never
// keeps a parse error's text: godotenv quotes the file's remainder in it.
func ReadDBPathOrigin(installDir string) DBPathOrigin {
	o := DBPathOrigin{DotEnvFile: filepath.Join(installDir, ".env")}
	vals, err := godotenv.Read(o.DotEnvFile)
	switch {
	case err == nil:
		o.DotEnvState = DotEnvRead
		o.DotEnvValue, o.InDotEnv = vals["DB_PATH"]
	case errors.Is(err, fs.ErrNotExist):
		o.DotEnvState = DotEnvAbsent
	case errors.Is(err, fs.ErrPermission):
		o.DotEnvState = DotEnvDenied
	default:
		o.DotEnvState = DotEnvUnreadable
	}
	o.ProcessValue, o.InProcess = os.LookupEnv("DB_PATH")
	return o
}

// Effective is the DB_PATH this process resolves through DotEnvGetenv: the
// process environment wins (even when it sets DB_PATH empty — then the
// default), else the .env, else DefaultDBPath.
func (o DBPathOrigin) Effective() string {
	if o.InProcess {
		return DBPath(func(string) string { return o.ProcessValue })
	}
	return o.FromInstallation()
}

// FromInstallation is the DB_PATH a bot started from installDir with NO
// DB_PATH in its own environment resolves: the .env's value, else
// DefaultDBPath (an absent or unreadable .env loads nothing).
func (o DBPathOrigin) FromInstallation() string {
	return DBPath(func(string) string { return o.DotEnvValue })
}
