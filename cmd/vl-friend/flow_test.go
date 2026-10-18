package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// flow_test.go — the install/update orchestration over fake deps. Proves the
// two safety invariants S9 demands: verify BEFORE write, and flat BEFORE swap.

type flowRecorder struct {
	execs []string
	lines []string
}

func flowFakeDeps(t *testing.T, home string) (deps, *flowRecorder) {
	t.Helper()
	rec := &flowRecorder{}
	d := deps{
		exec: func(_ context.Context, name string, args ...string) ([]byte, error) {
			rec.execs = append(rec.execs, name+" "+strings.Join(args, " "))
			return nil, nil
		},
		fetch: func(_ context.Context, url string) ([]byte, error) {
			return nil, fmt.Errorf("no fetch mapped for %s", url)
		},
		homeDir:   func() (string, error) { return home, nil },
		positions: func(_ context.Context) (float64, error) { return 0, nil },
		writeFile: os.WriteFile,
		mkdirAll:  os.MkdirAll,
		rename:    os.Rename,
		readFile:  os.ReadFile,
		readLine: func(p string) (string, error) {
			rec.lines = append(rec.lines, p)
			return "", nil
		},
	}
	return d, rec
}

// fetchMap builds a fetch func that serves a SIGNED manifest (with the real
// add-on sha filled in) plus the add-on artifact. It returns the throwaway
// signers so the caller can set d.allowedSigners.
func fetchMap(t *testing.T, base string, manifest, addon []byte) (func(context.Context, string) ([]byte, error), []byte) {
	t.Helper()
	sum := sha256.Sum256(addon)
	manifestWithSHA := strings.Replace(string(manifest), "REPLACE_ADDON_SHA", hex.EncodeToString(sum[:]), 1)
	_, sig, signers := signManifest(t, []byte(manifestWithSHA))
	table := map[string][]byte{
		base + "/api/friend/manifest":       []byte(manifestWithSHA),
		base + "/api/friend/manifest.sig":   sig,
		base + "/api/friend/artifact/addon": addon,
		base + "/api/friend/artifact/app":   []byte("fakeapp"),
	}
	fn := func(_ context.Context, url string) ([]byte, error) {
		b, ok := table[url]
		if !ok {
			return nil, fmt.Errorf("no fetch mapped for %s", url)
		}
		return b, nil
	}
	return fn, signers
}

func addonBody() []byte { return []byte("MZ-fake-dll") }

// manifestText is a manifest whose addon_sha256 is filled in by fetchMap.
func manifestText(addonBuild string) string {
	return `{"addon_build":"` + addonBuild + `","addon_sha256":"REPLACE_ADDON_SHA",
"addon_url":"/api/friend/artifact/addon","app_version":"1.0.0",
"app_sha256":"` + strings.Repeat("cd", 32) + `","app_url":"/api/friend/artifact/app"}` + "\n"
}

func TestInstallFirstRunPlacesAddonAfterVerify(t *testing.T) {
	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
	ic := goodCode()
	fetch, signers := fetchMap(t, ic.BaseURL(), []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	if err := installFirstRun(context.Background(), d, encodeInstallCode(ic)); err != nil {
		t.Fatalf("install refused: %v", err)
	}
	got, err := os.ReadFile(addonDstPath(home))
	if err != nil {
		t.Fatalf("addon not placed: %v", err)
	}
	if string(got) != string(addonBody()) {
		t.Fatalf("addon bytes wrong: %q", got)
	}
	if _, err := os.Stat(connectJSONPath(home)); err != nil {
		t.Fatalf("connect.json not written: %v", err)
	}
	if _, err := os.Stat(installedJSONPath(home)); err != nil {
		t.Fatalf("installed.json not written: %v", err)
	}
	shortcut := false
	for _, e := range rec.execs {
		if strings.HasPrefix(e, "powershell ") {
			shortcut = true
		}
	}
	if !shortcut {
		t.Fatalf("desktop shortcut not created; execs=%v", rec.execs)
	}
}

// TestInstallFirstRunRefusesTamperedSignature — S9 mutant target. A manifest
// whose SIGNATURE is garbage (but whose JSON is well-formed) must refuse, and
// nothing may be written. MUTANT: drop the verifySSHSIG call in
// VerifyFriendManifest → this test RED (the add-on gets written).
func TestInstallFirstRunRefusesTamperedSignature(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	ic := goodCode()
	base := ic.BaseURL()

	sum := sha256.Sum256(addonBody())
	man := strings.Replace(manifestText("37"), "REPLACE_ADDON_SHA", hex.EncodeToString(sum[:]), 1)
	_, _, signers := signManifest(t, []byte(man))
	garbageSig := []byte("-----BEGIN SSH SIGNATURE-----\nAAAA\n-----END SSH SIGNATURE-----\n")
	d.allowedSigners = signers
	d.fetch = func(_ context.Context, url string) ([]byte, error) {
		switch url {
		case base + "/api/friend/manifest":
			return []byte(man), nil
		case base + "/api/friend/manifest.sig":
			return garbageSig, nil
		case base + "/api/friend/artifact/addon":
			return addonBody(), nil
		}
		return nil, fmt.Errorf("no fetch mapped for %s", url)
	}

	if err := installFirstRun(context.Background(), d, encodeInstallCode(ic)); err == nil {
		t.Fatal("a tampered signature was accepted")
	}
	if _, statErr := os.Stat(addonDstPath(home)); statErr == nil {
		t.Fatal("the add-on was written despite a tampered signature")
	}
}

// TestInstallFirstRunRefusesBadAddonHash — a good signature but a download
// whose bytes don't match addon_sha256 must refuse and write nothing.
func TestInstallFirstRunRefusesBadAddonHash(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	ic := goodCode()
	base := ic.BaseURL()

	man := strings.Replace(manifestText("37"), "REPLACE_ADDON_SHA", strings.Repeat("00", 32), 1)
	_, sig, signers := signManifest(t, []byte(man))
	d.allowedSigners = signers
	d.fetch = func(_ context.Context, url string) ([]byte, error) {
		switch url {
		case base + "/api/friend/manifest":
			return []byte(man), nil
		case base + "/api/friend/manifest.sig":
			return sig, nil
		case base + "/api/friend/artifact/addon":
			return addonBody(), nil // sha256(addon) != 00..00
		}
		return nil, fmt.Errorf("no fetch mapped for %s", url)
	}

	if err := installFirstRun(context.Background(), d, encodeInstallCode(ic)); err == nil {
		t.Fatal("a bad add-on hash was accepted")
	}
	if _, statErr := os.Stat(addonDstPath(home)); statErr == nil {
		t.Fatal("the add-on was written despite a bad hash")
	}
}

func TestRunUpdateCheckNoUpdate(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	if err := writeInstalledState(d, home, installedState{AddonBuild: "37", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	base := "http://100.64.0.2:18081"
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	dec, err := RunUpdateCheck(context.Background(), d, base, true)
	if err != nil {
		t.Fatalf("no-update check errored: %v", err)
	}
	if dec.AddonUpdateAvailable || dec.AppUpdateAvailable || dec.Applied {
		t.Fatalf("expected no update, got %+v", dec)
	}
}

func TestRunUpdateCheckSwapsAddonWhenFlat(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	if err := writeInstalledState(d, home, installedState{AddonBuild: "36", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(addonDstPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(addonDstPath(home), []byte("MZ-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := "http://100.64.0.2:18081"
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	dec, err := RunUpdateCheck(context.Background(), d, base, true)
	if err != nil {
		t.Fatalf("update errored: %v", err)
	}
	if !dec.Applied {
		t.Fatal("update was not applied")
	}
	if b, err := os.ReadFile(addonDstPath(home)); err != nil || string(b) != string(addonBody()) {
		t.Fatalf("new add-on not in place: %v %q", err, b)
	}
	if b, err := os.ReadFile(oldAddonPath(home)); err != nil || string(b) != "MZ-old" {
		t.Fatalf("old add-on was not MOVED aside (never deleted): %v %q", err, b)
	}
}

// TestRunUpdateCheckRefusesWhenNotFlat — the swap must never happen while the
// friend has an open position (flat-wait deferral).
func TestRunUpdateCheckRefusesWhenNotFlat(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	if err := writeInstalledState(d, home, installedState{AddonBuild: "36", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	d.positions = func(_ context.Context) (float64, error) { return 2, nil } // never flat
	base := "http://100.64.0.2:18081"
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	dec, err := RunUpdateCheck(ctx, d, base, true)
	if err == nil {
		t.Fatal("an open position did not defer the update")
	}
	if dec.Applied {
		t.Fatal("the add-on was swapped mid-position")
	}
	if _, statErr := os.Stat(addonDstPath(home)); statErr == nil {
		t.Fatal("an add-on appeared despite a non-flat refusal")
	}
}

// TestRunUpdateCheckLaterDoesNothing — answering the pop-up with "later" leaves
// everything untouched.
func TestRunUpdateCheckLaterDoesNothing(t *testing.T) {
	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
	if err := writeInstalledState(d, home, installedState{AddonBuild: "36", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	d.readLine = func(p string) (string, error) {
		rec.lines = append(rec.lines, p)
		return "n", nil
	}
	base := "http://100.64.0.2:18081"
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	dec, err := RunUpdateCheck(context.Background(), d, base, false)
	if err != nil {
		t.Fatalf("later path errored: %v", err)
	}
	if dec.Applied {
		t.Fatal("a 'later' answer still applied the update")
	}
	if _, statErr := os.Stat(addonDstPath(home)); statErr == nil {
		t.Fatal("an add-on appeared despite 'later'")
	}
	if len(rec.lines) == 0 {
		t.Fatal("the pop-up was not shown")
	}
}

func TestVersionCompare(t *testing.T) {
	for _, c := range []struct {
		remote, current string
		want            bool
	}{
		{"37", "36", true},
		{"36", "37", false},
		{"37", "37", false},
		{"37", "", true}, // never installed
	} {
		if got := addonBuildNewer(c.remote, c.current); got != c.want {
			t.Errorf("addonBuildNewer(%q,%q)=%v want %v", c.remote, c.current, got, c.want)
		}
	}
	for _, c := range []struct {
		remote, current string
		want            bool
	}{
		{"1.2.0", "1.1.0", true},
		{"1.1.0", "1.2.0", false},
		{"1.0.0", "1.0.0", false},
	} {
		if got := appVersionNewer(c.remote, c.current); got != c.want {
			t.Errorf("appVersionNewer(%q,%q)=%v want %v", c.remote, c.current, got, c.want)
		}
	}
}

func TestProbeFlat(t *testing.T) {
	base := "http://100.64.0.2:18081"
	cases := []struct {
		name    string
		body    string
		fetchOK bool
		wantQ   float64
		wantErr bool
	}{
		{"flat", `{"flat":true,"at":1720000000000}`, true, 0, false},
		{"open", `{"flat":false,"at":1720000000000}`, true, 1, false},
		{"probe error fails closed", "", false, 0, true},
		{"malformed json fails closed", `{"flat":`, true, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := deps{fetch: func(_ context.Context, url string) ([]byte, error) {
				if url != base+"/api/friend/flat" {
					t.Fatalf("probed %q, want %q", url, base+"/api/friend/flat")
				}
				if !c.fetchOK {
					return nil, fmt.Errorf("connection refused")
				}
				return []byte(c.body), nil
			}}
			q, err := probeFlat(context.Background(), d, base)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if q != c.wantQ {
				t.Fatalf("q=%v want %v", q, c.wantQ)
			}
		})
	}
}
