package updaterbootstrap

// P-E E2/E3 + P-G G2 — the worker credential and the last-authz file.
//
// The cutover-worker token is minted ONLY here, by the attended CLI, from the
// installation's own JWT secret (its .env), under the worker_token_epoch the
// bot DB holds. It is written to ~/.config/vl-updater/env (the same file the
// installer and the worker read) with the VL_CUTOVER_TOKEN line replaced —
// every other line preserved byte-identically. `enroll --replace` revokes the
// PREVIOUS worker token through the bot DB's revoked_tokens table before
// writing the new one; `revoke-worker --all` bumps worker_token_epoch, which
// invalidates every worker token minted before the bump.
//
// authorize additionally writes the last-authz file (G2) so the owner can
// `cat` the line instead of re-copying it from a wrapped terminal.

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"vl/auth"
	"vl/config"
)

// workerEnvPath is the env file the worker and the installer read
// (deploy/install-updater-worker.sh ENV_FILE — kept in one place).
func workerEnvPath(home string) string {
	return filepath.Join(home, ".config", "vl-updater", "env")
}

// lastAuthzPath is the G2 file for one release authorization.
func lastAuthzPath(home, releaseID string) string {
	return filepath.Join(home, ".config", "vl-updater", "last-authz-"+releaseID+".json")
}

// openReadWrite opens the bot DB for the ONE revocation/epoch write this CLI
// is entitled to (the owner's attended tool). The same path-character
// refusal as openReadOnly; no pragmas beyond busy_timeout.
func openReadWrite(dbFile string) (*sql.DB, error) {
	if strings.ContainsAny(dbFile, "?#%") {
		return nil, fmt.Errorf("database path %q has a character the read-only URI cannot carry", dbFile)
	}
	db, err := sql.Open("sqlite", "file:"+dbFile+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open database for the worker-credential write: %w", err)
	}
	return db, nil
}

// tableExists reports whether the bot DB has the named table (read-only).
func tableExists(db *sql.DB, name string) (bool, error) {
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// workerEpoch reads the current worker_token_epoch; a DB without the table
// yet reads as epoch 0 (the epoch the first mint starts from).
func workerEpoch(dbFile string) (int64, error) {
	db, err := openReadOnly(dbFile)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	ok, err := tableExists(db, "worker_epoch")
	if err != nil {
		return 0, fmt.Errorf("read worker_epoch: %w", err)
	}
	if !ok {
		return 0, nil
	}
	var epoch int64
	if err := db.QueryRow("SELECT COALESCE(MAX(epoch),0) FROM worker_epoch").Scan(&epoch); err != nil {
		return 0, fmt.Errorf("read worker_epoch: %w", err)
	}
	return epoch, nil
}

// mintWorkerToken mints the cutover-worker credential the same way the server
// would validate it: the installation's .env supplies the JWT secret (the
// same resolution gate-jwt documents), the DB supplies the epoch. The .env is
// READ, never Loaded into the process environment — godotenv.Load would leak
// DB_PATH and friends into this process (a one-shot CLI, harmless) and into
// the shared test process (not harmless).
func mintWorkerToken(tgt target, userID, email string) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	if m, err := godotenv.Read(filepath.Join(tgt.installDir, ".env")); err == nil {
		if v, ok := m["JWT_SECRET"]; ok && v != "" {
			secret = v
		}
	}
	config.Init()
	if secret == "" {
		secret = config.Get().JWTSecret
	}
	auth.SetJWTSecret(secret)

	epoch, err := workerEpoch(tgt.dbFile)
	if err != nil {
		return "", err
	}
	return auth.GenerateWorkerJWT(userID, email, epoch)
}

// readWorkerTokenLine returns the current VL_CUTOVER_TOKEN (or the pre-rename
// twin) from the env file, and whether the file exists at all.
func readWorkerTokenLine(home string) (string, bool, error) {
	b, err := os.ReadFile(workerEnvPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			if strings.TrimSpace(k) == "VL_CUTOVER_TOKEN" || strings.TrimSpace(k) == "NOFX_CUTOVER_TOKEN" {
				if t := strings.TrimSpace(v); t != "" {
					return t, true, nil
				}
			}
		}
	}
	return "", true, nil
}

// checkOwnedFile asserts the file (if it exists) is owned by the user and
// mode 0600 — the same conditions install-updater-worker.sh enforces.
func checkOwnedFile(path string) error {
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode().Perm() != 0o600 {
		return fmt.Errorf("%s must be mode 0600 (got %04o)", path, fi.Mode().Perm())
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("%s is not owned by you", path)
	}
	return nil
}

// writeWorkerTokenLine writes the worker token into the env file, replacing
// only the CUTOVER_TOKEN line; every other line is preserved byte-identically
// (the VL_RELEASE_DIR line the installer checks survives untouched). A
// missing file is created 0600 in a 0700 config dir.
func writeWorkerTokenLine(home, token string) error {
	path := workerEnvPath(home)
	if err := checkOwnedFile(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			k, _, ok := strings.Cut(line, "=")
			if ok && (strings.TrimSpace(k) == "VL_CUTOVER_TOKEN" || strings.TrimSpace(k) == "NOFX_CUTOVER_TOKEN") {
				continue
			}
			lines = append(lines, line)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	lines = append(lines, "VL_CUTOVER_TOKEN="+token, "")
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600)
}

// revokeWorkerToken writes the previous worker token's fingerprint into the
// bot DB's revoked_tokens table so the server refuses it immediately — the
// same row auth.BlacklistToken persists to (P-E E3).
func revokeWorkerToken(home, dbFile, oldToken string) error {
	if oldToken == "" {
		return nil
	}
	db, err := openReadWrite(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS revoked_tokens (
		id TEXT PRIMARY KEY, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL)`); err != nil {
		return fmt.Errorf("ensure revoked_tokens: %w", err)
	}
	until := time.Now().UTC().Add(auth.WorkerTokenTTL + auth.ClockLeeway)
	fp := auth.TokenFingerprint(oldToken)
	if _, err := db.Exec(`INSERT INTO revoked_tokens(id, expires_at, created_at)
		VALUES(?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET expires_at=excluded.expires_at`,
		fp, until.Format("2006-01-02 15:04:05"), time.Now().UTC().Format("2006-01-02 15:04:05")); err != nil {
		return fmt.Errorf("revoke previous worker token: %w", err)
	}
	return nil
}

// bumpWorkerEpochDB increments worker_token_epoch and returns the new value.
// Every cutover-worker token minted before the bump is refused by the API
// (its WTE is below the current epoch).
func bumpWorkerEpochDB(dbFile string) (int64, error) {
	db, err := openReadWrite(dbFile)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS worker_epoch (
		id INTEGER PRIMARY KEY, epoch INTEGER NOT NULL)`); err != nil {
		return 0, fmt.Errorf("ensure worker_epoch: %w", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO worker_epoch(id, epoch) VALUES(1, 0)`); err != nil {
		return 0, fmt.Errorf("seed worker_epoch: %w", err)
	}
	if _, err := db.Exec(`UPDATE worker_epoch SET epoch = epoch + 1 WHERE id = 1`); err != nil {
		return 0, fmt.Errorf("bump worker_epoch: %w", err)
	}
	var epoch int64
	if err := db.QueryRow(`SELECT epoch FROM worker_epoch WHERE id = 1`).Scan(&epoch); err != nil {
		return 0, fmt.Errorf("read worker_epoch after bump: %w", err)
	}
	return epoch, nil
}

// writeLastAuthz implements G2: the authorization line is ALSO written to
// ~/.config/vl-updater/last-authz-<release_id>.json, mode 0600. It REFUSES
// to overwrite an existing file, and refuses when the config dir is missing,
// not 0700, or not owned by the user.
func writeLastAuthz(home, releaseID string, payload []byte) error {
	dir := filepath.Join(home, ".config", "vl-updater")
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("refusing: %s does not exist (the config dir must be there to receive the file)", dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("refusing: %s is not a directory", dir)
	}
	if fi.Mode().Perm() != 0o700 {
		return fmt.Errorf("refusing: %s must be mode 0700 (got %04o)", dir, fi.Mode().Perm())
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("refusing: %s is not owned by you", dir)
	}
	path := lastAuthzPath(home, releaseID)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("refusing: %s already exists — a fresh authorization goes into a fresh file", path)
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
