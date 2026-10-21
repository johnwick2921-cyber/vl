package deploy

// F2 (DS-101): a provisioned friend .env must carry the two encryption keys the
// boot demands and pass the SAME init function main.go calls. Before the fix,
// provision.sh wrote neither key, so a friend bot FATALed at boot
// ("Failed to initialize encryption service"). This test drives the production
// provision.sh into a scratch root, loads the .env the way main does
// (godotenv), and calls crypto.NewCryptoService — the same function main.go:52
// runs.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joho/godotenv"

	"vl/crypto"
)

// overrideEnv returns a child environment where each extra K=V REPLACES any
// existing K. exec passes the env through verbatim and getenv returns the FIRST
// duplicate, so extras go first and the originals are dropped.
func overrideEnv(extra ...string) []string {
	drop := make(map[string]bool, len(extra))
	out := make([]string, 0, len(os.Environ())+len(extra))
	for _, e := range extra {
		drop[strings.SplitN(e, "=", 2)[0]] = true
		out = append(out, e)
	}
	for _, e := range os.Environ() {
		if !drop[strings.SplitN(e, "=", 2)[0]] {
			out = append(out, e)
		}
	}
	return out
}

func TestFriendProvisionEnvBootsCrypto(t *testing.T) {
	provision := filepath.Join(repoRoot(t), "deploy", "friends", "provision.sh")

	// A stub `ss` so the port check is deterministic (provision.sh calls `ss -ltn`
	// by name via PATH).
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ss"), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	friendRoot := t.TempDir()
	home := t.TempDir()
	cmd := exec.Command("bash", provision, "carol", "--root", friendRoot)
	cmd.Env = overrideEnv(
		"HOME="+home,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TUNNEL_IP=100.64.0.7",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("provision.sh failed: %v\n%s", err, out)
	}

	// Parse the .env exactly the way the boot does (godotenv), then hand the two
	// keys to the real crypto init.
	env, err := godotenv.Read(filepath.Join(friendRoot, "carol", ".env"))
	if err != nil {
		t.Fatalf("godotenv read of provisioned .env: %v", err)
	}
	rsaKey, dataKey := env["RSA_PRIVATE_KEY"], env["DATA_ENCRYPTION_KEY"]
	if rsaKey == "" {
		t.Fatalf("RSA_PRIVATE_KEY missing from provisioned .env (keys: %v)", envKeys(env))
	}
	if dataKey == "" {
		t.Fatalf("DATA_ENCRYPTION_KEY missing from provisioned .env (keys: %v)", envKeys(env))
	}

	t.Setenv(crypto.EnvRSAPrivateKey, rsaKey)
	t.Setenv(crypto.EnvDataEncryptionKey, dataKey)
	if _, err := crypto.NewCryptoService(); err != nil {
		t.Fatalf("crypto init on a provisioned .env failed (the friend bot would FATAL here): %v", err)
	}
}

func envKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	return keys
}
