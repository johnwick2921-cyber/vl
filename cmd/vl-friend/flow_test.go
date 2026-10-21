package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// flow_test.go — the install/update orchestration over fake deps. Proves the
// two safety invariants S9 demands: verify BEFORE write, and flat BEFORE swap.

type flowRecorder struct {
	execs    []string
	lines    []string
	prints   []string
	opened   []string
	order    []string   // "print:", "prompt:", "open:" — for F8 ordering pins
	messages []string   // MessageBox texts (--open update dialog)
	rawExecs [][]string // name + args exactly as passed to d.exec (encoded commands NOT decoded)
}

func flowFakeDeps(t *testing.T, home string) (deps, *flowRecorder) {
	t.Helper()
	rec := &flowRecorder{}
	upCalled := false
	_ = os.WriteFile(filepath.Join(home, "vl-friend.exe"), []byte("MZ-self"), 0o755)
	d := deps{
		exec: func(_ context.Context, name string, args ...string) ([]byte, error) {
			rec.rawExecs = append(rec.rawExecs, append([]string{name}, args...))
			// P1: scripts ride -EncodedCommand; record the DECODED script so tests
			// can still assert on its text.
			shown := append([]string(nil), args...)
			for i := 0; i+1 < len(shown); i++ {
				if shown[i] == "-EncodedCommand" {
					shown[i+1] = decodePSCommand(shown[i+1])
				}
			}
			rec.execs = append(rec.execs, name+" "+strings.Join(shown, " "))
			// S10: tailscaleExec tries the absolute install path FIRST; route it
			// to the same state machine as the bare name.
			if name == tailscaleExeFallback {
				name = "tailscale"
			}
			// Tailscale state machine: NeedsLogin until "up", then Running — so a
			// "already installed" happy path proceeds straight to the join.
			if name == "tailscale" && len(args) > 0 && args[0] == "status" {
				st := "NeedsLogin"
				if upCalled {
					st = "Running"
				}
				return []byte(`{"BackendState":"` + st + `"}`), nil
			}
			if name == "tailscale" && len(args) > 0 && args[0] == "up" {
				upCalled = true
				return nil, nil
			}
			return nil, nil
		},
		fetch: func(_ context.Context, url string) ([]byte, error) {
			return nil, fmt.Errorf("no fetch mapped for %s", url)
		},
		homeDir:   func() (string, error) { return home, nil },
		docsDir:   func() (string, error) { return home, nil },
		dirExists: func(string) bool { return true }, // NT8 has run once (bin\Custom exists)
		positions: func(_ context.Context) (float64, error) { return 0, nil },
		writeFile: os.WriteFile,
		appendFile: func(path string, data []byte, perm fs.FileMode) error {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, perm)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = f.Write(data)
			return err
		},
		mkdirAll: os.MkdirAll,
		rename:   os.Rename,
		readFile: os.ReadFile,
		readLine: func(p string) (string, error) {
			rec.lines = append(rec.lines, p)
			rec.order = append(rec.order, "prompt:"+p)
			return "", nil
		},
		readSecret: func(p string) (string, error) {
			rec.lines = append(rec.lines, p)
			rec.order = append(rec.order, "secret:"+p)
			return "", nil
		},
		print: func(l string) {
			rec.prints = append(rec.prints, l)
			rec.order = append(rec.order, "print:"+l)
		},
		tempDir: func() (string, error) { return t.TempDir(), nil },
		verifyMsiSigner: func(string) (msiSignature, error) {
			return msiSignature{Status: "Valid", Subject: "CN=Tailscale Inc., O=Tailscale Inc."}, nil
		},
		openURL: func(_ context.Context, url string) error {
			rec.opened = append(rec.opened, url)
			rec.order = append(rec.order, "open:"+url)
			return nil
		},
		ntRunning:       func(context.Context) (bool, error) { return false, nil },
		localAppDataDir: func() string { return filepath.Join(home, "AppData", "Local") },
		selfExe:         func() (string, error) { return filepath.Join(home, "vl-friend.exe"), nil },
		messageBox: func(text string) bool {
			rec.messages = append(rec.messages, text)
			return false
		},
		errorBox: func(text string) {
			rec.messages = append(rec.messages, text)
		},
		infoBox: func(text string) {
			rec.messages = append(rec.messages, text)
		},
		singleInstance: func() bool { return true },
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
		// H1: the flat probe is flat and KNOWN by default (a bot with a live NT8).
		base + "/api/friend/flat": []byte(`{"flat":true,"traders":1,"at":1,"known":true,"last_known_flat_ms":1}`),
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
	// P0 pin (a): the FS starts EMPTY (home=t.TempDir) and installFirstRun must
	// write vl-app.json — a real PC never runs a test to pre-seed it. MUTANT:
	// drop the writeAppState call in installFirstRun → this stat fails.
	if _, err := os.Stat(appStatePath(home)); err != nil {
		t.Fatalf("vl-app.json not written by installFirstRun: %v", err)
	}
	if _, err := os.Stat(installedJSONPath(home)); err != nil {
		t.Fatalf("installed.json not written: %v", err)
	}
	shortcut := false
	for _, e := range rec.execs {
		if strings.HasPrefix(e, "powershell.exe ") {
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
	// m6: the old add-on is MOVED aside with a timestamped name (never deleted).
	oldDir := filepath.Join(ntVLdir(home), "old")
	entries, err := os.ReadDir(oldDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one timestamped backup in VL\\old\\: %v %v", entries, err)
	}
	if b, err := os.ReadFile(filepath.Join(oldDir, entries[0].Name())); err != nil || string(b) != "MZ-old" {
		t.Fatalf("old add-on was not MOVED aside (never deleted): %v %q", err, b)
	}
}

// TestRunUpdateCheckSkipsWhenDllMatchesManifest — S2: the swap is gated on the
// ON-DISK sha256, not installed.json alone. If the DLL already matches the
// manifest, nothing is rewritten. MUTANT: drop the addonNeedsUpdate gate → RED.
func TestRunUpdateCheckSkipsWhenDllMatchesManifest(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	// installed.json says 36, but the ON-DISK DLL is already the 37 add-on.
	if err := writeInstalledState(d, home, installedState{AddonBuild: "36", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(addonDstPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(addonDstPath(home), addonBody(), 0o644); err != nil {
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
	if dec.Applied {
		t.Fatal("the DLL already matched the manifest — nothing may be rewritten")
	}
}

// TestInstallFirstRunSwapsExistingAddon — S16: if a VL.AddOn.dll is already
// there, the first run uses the swap path (move old aside, never truncate).
func TestInstallFirstRunSwapsExistingAddon(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	ic := goodCode()
	// A pre-existing add-on DLL (e.g. a previous manual install).
	if err := os.MkdirAll(filepath.Dir(addonDstPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(addonDstPath(home), []byte("MZ-preexisting"), 0o644); err != nil {
		t.Fatal(err)
	}
	fetch, signers := fetchMap(t, ic.BaseURL(), []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	if err := installFirstRun(context.Background(), d, encodeInstallCode(ic)); err != nil {
		t.Fatalf("install refused: %v", err)
	}
	if b, err := os.ReadFile(addonDstPath(home)); err != nil || string(b) != string(addonBody()) {
		t.Fatalf("new add-on not in place: %v %q", err, b)
	}
	oldDir := filepath.Join(ntVLdir(home), "old")
	entries, err := os.ReadDir(oldDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("the pre-existing add-on was not MOVED aside: %v %v", entries, err)
	}
	if b, err := os.ReadFile(filepath.Join(oldDir, entries[0].Name())); err != nil || string(b) != "MZ-preexisting" {
		t.Fatalf("the pre-existing add-on was not preserved: %v %q", err, b)
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

// TestRunUpdateCheckUnreachableFlatProbeDefers — CTO ruling 1: a flat probe
// ERROR (endpoint unreachable / malformed) is NOT flat → the update defers and
// nothing is swapped. Same fail-closed path as an open position.
func TestRunUpdateCheckUnreachableFlatProbeDefers(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	if err := writeInstalledState(d, home, installedState{AddonBuild: "36", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	// The flat endpoint is unreachable: the probe returns an error every poll.
	d.positions = func(_ context.Context) (float64, error) {
		return 0, fmt.Errorf("GET /api/friend/flat: connection refused")
	}
	base := "http://100.64.0.2:18081"
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	dec, err := RunUpdateCheck(ctx, d, base, true)
	if err == nil {
		t.Fatal("an unreachable flat probe did not defer the update")
	}
	if dec.Applied {
		t.Fatal("the add-on was swapped despite an unreachable flat probe")
	}
	if _, statErr := os.Stat(addonDstPath(home)); statErr == nil {
		t.Fatal("an add-on appeared despite an unreachable flat probe")
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

// TestRunFirstRunPrintsSixStepsInOrder — CTO "instruction fix": the setup
// window must tell the (non-technical) owner what to do at every step, in plain
// English, numbered [1/6]…[6/6], in order. MUTANT: drop or reorder a step
// header → this test RED.
func TestRunFirstRunPrintsSixStepsInOrder(t *testing.T) {
	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
	ic := goodCode()
	fetch, signers := fetchMap(t, ic.BaseURL(), []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers
	// G7: the install code is read by readSecret (echo-off); the final
	// "Press Enter to close" pause stays readLine.
	d.readSecret = func(p string) (string, error) {
		rec.lines = append(rec.lines, p)
		return encodeInstallCode(ic), nil
	}

	if err := runFirstRun(context.Background(), d); err != nil {
		t.Fatalf("runFirstRun failed: %v", err)
	}

	var headers []string
	for _, l := range rec.prints {
		if len(l) >= 5 && l[0] == '[' && l[2] == '/' && l[4] == ']' {
			headers = append(headers, l)
		}
	}
	want := []string{"[1/6]", "[2/6]", "[3/6]", "[4/6]", "[5/6]", "[6/6]"}
	if len(headers) != len(want) {
		t.Fatalf("expected %d step headers, got %d: %q", len(want), len(headers), rec.prints)
	}
	for i, h := range headers {
		if !strings.HasPrefix(h, want[i]) {
			t.Fatalf("step headers out of order: got %q, want %q first", headers, want[i])
		}
	}
	joined := strings.Join(rec.prints, "\n")
	for _, needle := range []string{
		"Tailscale is already installed ✓",
		"Approved ✓",
		"Add-on verified ✓",
		"Done ✓. If NinjaTrader is open, close it and open it again.",
		"Your VL dashboard:  " + fmt.Sprintf("http://%s:%d", ic.Host, ic.WebPort),
		"A VL icon is on your desktop and in the Start Menu.",
		"Press Enter to close",
	} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("missing %q in step output:\n%s", needle, joined)
		}
	}
}

// TestPauseOnErrorPrintsPhotoLine — CTO error screen: "Something went wrong:
// <reason>… Send a photo of this window to the owner." + "Press Enter to
// close". MUTANT: drop the "Send a photo" line → this test RED.
func TestPauseOnErrorPrintsPhotoLine(t *testing.T) {
	var prints []string
	d := deps{
		print:    func(l string) { prints = append(prints, l) },
		readLine: func(p string) (string, error) { return "", nil },
	}
	pauseOnError(d, errors.New("boom"))
	joined := strings.Join(prints, "\n")
	if !strings.Contains(joined, "Something went wrong: boom") {
		t.Fatalf("error screen missing the reason: %q", joined)
	}
	if !strings.Contains(joined, "Nothing was changed on NinjaTrader") {
		t.Fatalf("error screen missing the 'Nothing was changed' line: %q", joined)
	}
	if !strings.Contains(joined, "Send a photo of this window to the owner") {
		t.Fatalf("error screen missing the 'Send a photo' line: %q", joined)
	}
	if !strings.Contains(joined, "Press Enter to close") {
		t.Fatalf("error screen missing 'Press Enter to close': %q", joined)
	}
}

// TestShortcutScriptTargetsRealDesktopAndEdge — B8 pins: the shortcut script
// must resolve the REAL Desktop + Start Menu via [Environment]::GetFolderPath
// (OneDrive Known-Folder safe, NOT $env:USERPROFILE\Desktop), launch the LOCAL
// copy of vl-friend.exe with --open, and fall back to a .url Internet Shortcut
// when the exe copy is missing. MUTANT: GetFolderPath('Desktop') back to
// $env:USERPROFILE\Desktop → this test RED.
func TestShortcutScriptTargetsRealDesktopAndEdge(t *testing.T) {
	ps := shortcutScript()
	for _, want := range []string{
		"[Environment]::GetFolderPath('Desktop')",
		"[Environment]::GetFolderPath('Programs')",
		`$env:LOCALAPPDATA\VL\vl-friend.exe`,
		"--open",
		"VL.lnk",
		"VL.url",
		"[InternetShortcut]",
		"URL=$env:VL_ARG0",
		"VL_RESULT=ok",
	} {
		if !strings.Contains(ps, want) {
			t.Fatalf("shortcut script missing %q:\n%s", want, ps)
		}
	}
	if strings.Contains(ps, "$env:USERPROFILE") {
		t.Fatalf("shortcut script still hardcodes $env:USERPROFILE (OneDrive-unsafe):\n%s", ps)
	}
	// P1: the URL rides $env:VL_ARG0 — never $args, never spliced into the text.
	if strings.Contains(ps, "$args") || !strings.Contains(ps, "$env:VL_ARG0") || strings.Contains(ps, "100.66.89.106") {
		t.Fatalf("shortcut script must read $env:VL_ARG0 and carry no $args / literal URL:\n%s", ps)
	}
	if !strings.HasPrefix(ps, psPreamble) {
		t.Fatalf("shortcut script must start with psPreamble:\n%s", ps)
	}
	// B1: the if/else must be ONE statement — PowerShell rejects "};else{".
	if strings.Contains(ps, "};else{") {
		t.Fatalf("shortcut script has an invalid '};else{' split:\n%s", ps)
	}
	if !strings.Contains(ps, "}else{") {
		t.Fatalf("shortcut script is missing the contiguous }else{:\n%s", ps)
	}
}

// TestRepairIconRecreatesShortcut — B8 live bug: the owner's OneDrive-redirected
// desktop had no icon. repairIcon (--repair-icon) must re-create it on an
// already-installed PC; the PowerShell argv must carry GetFolderPath('Desktop').
func TestRepairIconRecreatesShortcut(t *testing.T) {
	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
	ic := goodCode()
	if err := writeConnectJSON(d, home, ic); err != nil {
		t.Fatal(err)
	}
	if err := writeAppState(d, home, appState{Name: "friend1", WebPort: 18081}); err != nil {
		t.Fatal(err)
	}
	if err := repairIcon(context.Background(), d); err != nil {
		t.Fatalf("repairIcon refused: %v", err)
	}
	shortcut := false
	for _, e := range rec.execs {
		if strings.HasPrefix(e, "powershell.exe ") && strings.Contains(e, "[Environment]::GetFolderPath('Desktop')") {
			shortcut = true
		}
	}
	if !shortcut {
		t.Fatalf("repairIcon did not run the shortcut script with GetFolderPath('Desktop'): %v", rec.execs)
	}
	joined := strings.Join(rec.prints, "\n")
	if !strings.Contains(joined, "Done ✓ — the VL icon is back") {
		t.Fatalf("repairIcon did not confirm: %q", rec.prints)
	}
}

// TestRepairIconNotInstalledRefuses — repairIcon on a PC with no connect.json
// must refuse with a clear message, never guess a URL.
func TestRepairIconNotInstalledRefuses(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	if err := repairIcon(context.Background(), d); err == nil {
		t.Fatal("repairIcon on a not-installed PC was accepted")
	}
}

// TestInstallFirstRunOpensDashboardAndWritesREADME — CTO 23:45: at the end of
// setup the user must SEE where VL is three ways: the [6/6] screen prints the
// URL big and plain, setup OPENS the dashboard automatically, and README.txt is
// written. MUTANT: drop the d.openURL call → the opened check RED.
func TestInstallFirstRunOpensDashboardAndWritesREADME(t *testing.T) {
	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
	ic := goodCode()
	fetch, signers := fetchMap(t, ic.BaseURL(), []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	if err := installFirstRun(context.Background(), d, encodeInstallCode(ic)); err != nil {
		t.Fatalf("install refused: %v", err)
	}
	appURL := ic.BaseURL()
	opened := false
	for _, u := range rec.opened {
		if u == appURL {
			opened = true
		}
	}
	if !opened {
		t.Fatalf("setup did not auto-open the dashboard; opened=%v", rec.opened)
	}
	b, err := os.ReadFile(filepath.Join(ntVLdir(home), "README.txt"))
	if err != nil {
		t.Fatalf("README.txt not written: %v", err)
	}
	if !strings.Contains(string(b), "Your VL dashboard: "+appURL) {
		t.Fatalf("README.txt missing the URL: %q", b)
	}
	joined := strings.Join(rec.prints, "\n")
	if !strings.Contains(joined, "Your VL dashboard:  "+appURL) {
		t.Fatalf("[6/6] screen missing the dashboard URL: %q", rec.prints)
	}
	if !strings.Contains(joined, "A VL icon is on your desktop and in the Start Menu.") {
		t.Fatalf("[6/6] screen missing the icon line: %q", rec.prints)
	}
}

// TestRunInstalledPrintsBothLinesAndOpens — CTO 23:47: an installed PC, plain
// double-click, no args: announce, update check, ensure icons, print + open the
// dashboard, "Press Enter to close". NO install-code prompt. MUTANT: skip the
// openURL on the installed path → the opened check RED; skip ensureShortcut →
// the powershell GetFolderPath check RED.
func TestRunInstalledPrintsBothLinesAndOpens(t *testing.T) {
	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
	ic := goodCode()
	if err := writeConnectJSON(d, home, ic); err != nil {
		t.Fatal(err)
	}
	if err := writeAppState(d, home, appState{Name: "friend1", WebPort: 18081}); err != nil {
		t.Fatal(err)
	}
	// No update: manifest build == installed build, so the check does not prompt.
	if err := writeInstalledState(d, home, installedState{AddonBuild: "37", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://%s:%d", ic.Host, 18081)
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	if err := runInstalled(context.Background(), d, home); err != nil {
		t.Fatalf("runInstalled refused: %v", err)
	}

	joined := strings.Join(rec.prints, "\n")
	if !strings.Contains(joined, "VL is already set up on this PC ✓") {
		t.Fatalf("missing 'already set up' line: %q", rec.prints)
	}
	if !strings.Contains(joined, "Your VL dashboard: "+base) {
		t.Fatalf("missing dashboard URL line: %q", rec.prints)
	}

	opened := false
	for _, u := range rec.opened {
		if u == base {
			opened = true
		}
	}
	if !opened {
		t.Fatalf("the dashboard was not opened; opened=%v", rec.opened)
	}

	shortcut := false
	for _, e := range rec.execs {
		if strings.HasPrefix(e, "powershell.exe ") && strings.Contains(e, "[Environment]::GetFolderPath('Desktop')") {
			shortcut = true
		}
	}
	if !shortcut {
		t.Fatalf("installed run did not ensure the shortcut: %v", rec.execs)
	}

	// No FORCED install-code prompt (the S6 gate "Have a new install code?" is
	// optional and skipped here); readLine must never ask the user to PASTE a
	// code on the installed path.
	for _, p := range rec.lines {
		if strings.Contains(p, "Paste the install code") || strings.Contains(p, "Paste the new install code") {
			t.Fatalf("runInstalled prompted for an install code: %q", rec.lines)
		}
	}
}

// TestRunInstalledRepairsMissingAppState — P0 pin (b): the owner's laptop is
// installed with 1.1.2 and has NO vl-app.json (connect.json only). An installed
// run must REPAIR it (derive web_port = nt_port − 18900, verify the manifest),
// write vl-app.json, and open the dashboard. MUTANT: drop the repair
// (readAppState hard-fails) → this test RED.
func TestRunInstalledRepairsMissingAppState(t *testing.T) {
	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
	ic := goodCode()
	// connect.json only — deliberately NO vl-app.json (the 1.1.x laptop state).
	if err := writeConnectJSON(d, home, ic); err != nil {
		t.Fatal(err)
	}
	if err := writeInstalledState(d, home, installedState{AddonBuild: "37", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	// Candidate port = 36981 − 18900 = 18081 = ic.WebPort; serve the manifest there.
	base := fmt.Sprintf("http://%s:%d", ic.Host, ic.WebPort)
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	if err := runInstalled(context.Background(), d, home); err != nil {
		t.Fatalf("runInstalled refused on a laptop with no vl-app.json: %v", err)
	}
	if _, err := os.Stat(appStatePath(home)); err != nil {
		t.Fatalf("vl-app.json not written after repair: %v", err)
	}
	opened := false
	for _, u := range rec.opened {
		if u == base {
			opened = true
		}
	}
	if !opened {
		t.Fatalf("dashboard not opened after repair; opened=%v", rec.opened)
	}
}

// TestRunInstalledRepairVerificationFails — P0 pin (c): when the candidate port
// does NOT answer with a signature-valid manifest, the run must fail with the
// clear error and write NOTHING (never guess silently).
func TestRunInstalledRepairVerificationFails(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	ic := goodCode()
	if err := writeConnectJSON(d, home, ic); err != nil {
		t.Fatal(err)
	}
	if err := writeInstalledState(d, home, installedState{AddonBuild: "37", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	// No fetch mapped → the candidate-port manifest verification fails.
	err := runInstalled(context.Background(), d, home)
	if err == nil || !strings.Contains(err.Error(), "cannot find your VL dashboard port") {
		t.Fatalf("expected the clear 'cannot find' error, got %v", err)
	}
	if _, statErr := os.Stat(appStatePath(home)); statErr == nil {
		t.Fatal("vl-app.json must NOT be written when verification fails")
	}
}

// TestInstallFirstRunWritesUnderMyDocuments — F2: with MyDocuments !=
// <USERPROFILE>\Documents (OneDrive redirection), every NT path must land under
// MyDocuments. MUTANT: path helpers back to <home>\Documents → RED.
func TestInstallFirstRunWritesUnderMyDocuments(t *testing.T) {
	home := t.TempDir()
	docs := filepath.Join(home, "OneDrive", "Documents")
	d, _ := flowFakeDeps(t, home)
	d.docsDir = func() (string, error) { return docs, nil }
	ic := goodCode()
	fetch, signers := fetchMap(t, ic.BaseURL(), []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	if err := installFirstRun(context.Background(), d, encodeInstallCode(ic)); err != nil {
		t.Fatalf("install refused: %v", err)
	}
	for _, p := range []string{
		connectJSONPath(docs),
		installedJSONPath(docs),
		appStatePath(docs),
		addonDstPath(docs),
		filepath.Join(ntVLdir(docs), "README.txt"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing under MyDocuments: %s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "Documents", "NinjaTrader 8")); err == nil {
		t.Fatal("files were written under <home>\\Documents instead of MyDocuments")
	}
}

// TestInstallFirstRunRefusesMissingNTFolder — F2 exactness (owner 00:05): before
// ANY write, <MyDocuments>\NinjaTrader 8\bin\Custom must exist (NT8 creates it
// on first start). Missing → clear error, NOTHING written. MUTANT: drop the
// dirExists check → this test RED.
func TestInstallFirstRunRefusesMissingNTFolder(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	d.dirExists = func(string) bool { return false } // NT8 has never run
	ic := goodCode()
	fetch, signers := fetchMap(t, ic.BaseURL(), []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	err := installFirstRun(context.Background(), d, encodeInstallCode(ic))
	if err == nil || !strings.Contains(err.Error(), "Open NinjaTrader 8 once") {
		t.Fatalf("expected the 'Open NinjaTrader 8 once' error, got %v", err)
	}
	for _, p := range []string{
		connectJSONPath(home),
		appStatePath(home),
		addonDstPath(home),
		installedJSONPath(home),
	} {
		if _, statErr := os.Stat(p); statErr == nil {
			t.Fatalf("nothing may be written when bin\\Custom is missing, but %s exists", p)
		}
	}
}

// TestMigrateDocsMovesLegacyFiles — F2 MIGRATION: files under
// <USERPROFILE>\Documents move into MyDocuments (never deleted) when the two
// differ (the owner's laptop layout).
func TestMigrateDocsMovesLegacyFiles(t *testing.T) {
	home := t.TempDir()
	docs := filepath.Join(home, "OneDrive", "Documents")
	d, _ := flowFakeDeps(t, home)

	oldVL := filepath.Join(home, "Documents", "NinjaTrader 8", "VL")
	if err := os.MkdirAll(oldVL, 0o755); err != nil {
		t.Fatal(err)
	}
	oldConnect := filepath.Join(oldVL, "connect.json")
	if err := os.WriteFile(oldConnect, []byte(`{"host":"100.64.0.2","port":36981,"connect_key":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	oldDLL := filepath.Join(home, "Documents", "NinjaTrader 8", "bin", "Custom", "VL.AddOn.dll")
	if err := os.MkdirAll(filepath.Dir(oldDLL), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldDLL, []byte("MZ-old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := migrateDocsIfNeeded(d, home, docs); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if _, err := os.Stat(connectJSONPath(docs)); err != nil {
		t.Fatalf("connect.json not moved: %v", err)
	}
	if _, err := os.Stat(addonDstPath(docs)); err != nil {
		t.Fatalf("addon not moved: %v", err)
	}
	if _, err := os.Stat(oldConnect); err == nil {
		t.Fatal("legacy connect.json must be moved away (not left in place)")
	}
}

// TestProbeFlatZeroTradersSafe — F5/R11: flat==true OR traders==0 → safe; a
// missing traders field → today's rule (not flat).
func TestProbeFlatZeroTradersSafe(t *testing.T) {
	cases := []struct {
		body string
		want float64
	}{
		{`{"flat":true,"traders":3,"at":1}`, 0},
		{`{"flat":false,"traders":0,"at":1}`, 0}, // zero traders → safe
		{`{"flat":false,"traders":5,"at":1}`, 1},
		{`{"flat":false,"at":1}`, 1}, // missing traders → not flat
	}
	for _, c := range cases {
		d := deps{fetch: func(_ context.Context, u string) ([]byte, error) { return []byte(c.body), nil }}
		got, err := probeFlat(context.Background(), d, "http://x")
		if err != nil {
			t.Fatalf("probeFlat(%s): %v", c.body, err)
		}
		if got != c.want {
			t.Fatalf("probeFlat(%s) = %v, want %v", c.body, got, c.want)
		}
	}
}

// TestRunUpdateCheckWaitsForNinjaTraderClose — F4: never write a locked DLL. If
// NinjaTrader.exe is running, prompt to close it (or defer) and re-check.
func TestRunUpdateCheckWaitsForNinjaTraderClose(t *testing.T) {
	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
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
	running := true
	d.ntRunning = func(context.Context) (bool, error) { return running, nil }
	calls := 0
	d.readLine = func(p string) (string, error) {
		rec.lines = append(rec.lines, p)
		calls++
		if calls == 1 {
			running = false // the user closed NinjaTrader
			return "", nil
		}
		return "", nil
	}
	dec, err := RunUpdateCheck(context.Background(), d, base, true)
	if err != nil {
		t.Fatalf("update refused: %v", err)
	}
	if !dec.Applied {
		t.Fatal("update was not applied after NinjaTrader closed")
	}
	if !strings.Contains(strings.Join(rec.lines, "\n"), "Close NinjaTrader") {
		t.Fatalf("the close-NinjaTrader prompt was not shown: %q", rec.lines)
	}
}

// TestRunInstalledAnnouncesAppUpdateHonestly — F6: there is no self-update; an
// app-only update is announced honestly, never silently claimed as applied.
func TestRunInstalledAnnouncesAppUpdateHonestly(t *testing.T) {
	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
	ic := goodCode()
	if err := writeConnectJSON(d, home, ic); err != nil {
		t.Fatal(err)
	}
	if err := writeAppState(d, home, appState{Name: "friend1", WebPort: 18081}); err != nil {
		t.Fatal(err)
	}
	if err := writeInstalledState(d, home, installedState{AddonBuild: "37", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://%s:%d", ic.Host, 18081)
	man := strings.Replace(manifestText("37"), `"app_version":"1.0.0"`, `"app_version":"1.1.0"`, 1)
	fetch, signers := fetchMap(t, base, []byte(man), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	if err := runInstalled(context.Background(), d, home); err != nil {
		t.Fatalf("runInstalled refused: %v", err)
	}
	joined := strings.Join(rec.prints, "\n")
	if !strings.Contains(joined, "A new VL-Setup is available — ask the owner for it.") {
		t.Fatalf("missing the honest app-update line: %q", rec.prints)
	}
}

// TestRunInstalledOpensDashboardBeforeUpdatePrompt — F8: the dashboard opens
// BEFORE any update y/N question.
func TestRunInstalledOpensDashboardBeforeUpdatePrompt(t *testing.T) {
	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
	ic := goodCode()
	if err := writeConnectJSON(d, home, ic); err != nil {
		t.Fatal(err)
	}
	if err := writeAppState(d, home, appState{Name: "friend1", WebPort: 18081}); err != nil {
		t.Fatal(err)
	}
	if err := writeInstalledState(d, home, installedState{AddonBuild: "36", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	// An older add-on DLL is present (so S4 does not treat this as a partial
	// install) — the update check below must offer the update.
	if err := os.MkdirAll(filepath.Dir(addonDstPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(addonDstPath(home), []byte("MZ-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://%s:%d", ic.Host, 18081)
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	if err := runInstalled(context.Background(), d, home); err != nil {
		t.Fatalf("runInstalled refused: %v", err)
	}
	openIdx, promptIdx := -1, -1
	for i, e := range rec.order {
		if strings.HasPrefix(e, "open:"+base) {
			openIdx = i
		}
		if strings.Contains(e, "Update now?") {
			promptIdx = i
		}
	}
	if openIdx == -1 || promptIdx == -1 {
		t.Fatalf("missing events: open=%d prompt=%d order=%v", openIdx, promptIdx, rec.order)
	}
	if openIdx > promptIdx {
		t.Fatalf("the update prompt came BEFORE the dashboard opened (F8): order=%v", rec.order)
	}
}

// TestLogSetupErrorAppends — F7: setup.log APPENDS; both errors survive.
func TestLogSetupErrorAppends(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	logSetupError(d, "first")
	logSetupError(d, "second")
	b, err := os.ReadFile(setupLogPath(d))
	if err != nil {
		t.Fatalf("setup.log not written: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "first") || !strings.Contains(s, "second") {
		t.Fatalf("setup.log must APPEND both errors, got: %q", s)
	}
	if strings.Count(s, "second") != 1 {
		t.Fatalf("setup.log should carry one line per error: %q", s)
	}
}

// TestLogSetupErrorUsesAppendFile — S17: logSetupError must go through the
// O_APPEND seam (never read+write), and S10: the tskey regex redacts centrally.
func TestLogSetupErrorUsesAppendFile(t *testing.T) {
	var appended []byte
	called := false
	d := deps{
		localAppDataDir: func() string { return t.TempDir() },
		mkdirAll:        func(string, fs.FileMode) error { return nil },
		appendFile: func(_ string, data []byte, _ fs.FileMode) error {
			called = true
			appended = append(appended, data...)
			return nil
		},
	}
	logSetupError(d, "boom tskey-auth-SECRET123")
	if !called {
		t.Fatal("logSetupError must use appendFile, not read+write")
	}
	if strings.Contains(string(appended), "tskey-auth-SECRET123") {
		t.Fatalf("the tskey must be redacted in the log: %q", appended)
	}
}

// TestRunOpenOpensDashboardNoUpdate — the desktop icon (--open): no update →
// dashboard opens, no dialog.
func TestRunOpenOpensDashboardNoUpdate(t *testing.T) {
	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
	ic := goodCode()
	if err := writeConnectJSON(d, home, ic); err != nil {
		t.Fatal(err)
	}
	if err := writeAppState(d, home, appState{Name: "friend1", WebPort: 18081}); err != nil {
		t.Fatal(err)
	}
	if err := writeInstalledState(d, home, installedState{AddonBuild: "37", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://%s:%d", ic.Host, 18081)
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	if err := runOpen(context.Background(), d); err != nil {
		t.Fatalf("runOpen refused: %v", err)
	}
	if !containsStr(rec.opened, base) {
		t.Fatalf("dashboard not opened; opened=%v", rec.opened)
	}
	if len(rec.messages) != 0 {
		t.Fatalf("no dialog expected when there is no update: %v", rec.messages)
	}
}

// TestRunOpenUpdateDialogYes — an update exists, the user clicks Yes → the
// verified swap runs (dec.Applied). MUTANT: skip the messageBox/apply → RED.
func TestRunOpenUpdateDialogYes(t *testing.T) {
	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
	ic := goodCode()
	if err := writeConnectJSON(d, home, ic); err != nil {
		t.Fatal(err)
	}
	if err := writeAppState(d, home, appState{Name: "friend1", WebPort: 18081}); err != nil {
		t.Fatal(err)
	}
	if err := writeInstalledState(d, home, installedState{AddonBuild: "36", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(addonDstPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(addonDstPath(home), []byte("MZ-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://%s:%d", ic.Host, 18081)
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	// B2: runOpen wires d.positions = probeFlat, so the flat endpoint must answer.
	d.fetch = func(ctx context.Context, u string) ([]byte, error) {
		if u == base+"/api/friend/flat" {
			return []byte(`{"flat":true,"traders":1,"at":1}`), nil
		}
		return fetch(ctx, u)
	}
	d.allowedSigners = signers
	d.messageBox = func(text string) bool { rec.messages = append(rec.messages, text); return true }

	if err := runOpen(context.Background(), d); err != nil {
		t.Fatalf("runOpen refused: %v", err)
	}
	if b, err := os.ReadFile(addonDstPath(home)); err != nil || string(b) != string(addonBody()) {
		t.Fatalf("the update was not applied: %v %q", err, b)
	}
	// M4: the dialog names the builds; W8b: an applied update is announced.
	if len(rec.messages) != 2 {
		t.Fatalf("expected the update dialog + the applied message, got %v", rec.messages)
	}
	if !strings.Contains(rec.messages[0], "add-on 36 → 37") {
		t.Fatalf("the dialog must name the build transition: %v", rec.messages)
	}
	if !strings.Contains(rec.messages[1], "Updated ✓") {
		t.Fatalf("the applied message must tell the mentor to restart: %v", rec.messages)
	}
}

// TestRunOpenNotInstalledShowsError — I3: --open on a fresh PC (no connect.json)
// must never fail silently; the mentor sees "VL could not start".
func TestRunOpenNotInstalledShowsError(t *testing.T) {
	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
	if err := runOpen(context.Background(), d); err == nil {
		t.Fatal("runOpen on a not-installed PC must return an error")
	}
	if len(rec.messages) != 1 || !strings.Contains(rec.messages[0], "VL could not start") {
		t.Fatalf("the mentor must see 'VL could not start': %v", rec.messages)
	}
}

// TestRunOpenOpenFailureShowsMessage — I3: if the dashboard cannot be opened,
// the mentor sees a dialog (never silent).
func TestRunOpenOpenFailureShowsMessage(t *testing.T) {
	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
	ic := goodCode()
	if err := writeConnectJSON(d, home, ic); err != nil {
		t.Fatal(err)
	}
	if err := writeAppState(d, home, appState{Name: "friend1", WebPort: 18081}); err != nil {
		t.Fatal(err)
	}
	if err := writeInstalledState(d, home, installedState{AddonBuild: "37", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://%s:%d", ic.Host, 18081)
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers
	d.openURL = func(context.Context, string) error { return fmt.Errorf("browser failed") }

	if err := runOpen(context.Background(), d); err == nil {
		t.Fatal("runOpen must return the open error")
	}
	found := false
	for _, m := range rec.messages {
		if strings.Contains(m, "VL could not start") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the mentor must see 'VL could not start': %v", rec.messages)
	}
}

// TestRunInstalledEnsuresLocalExe — I1: an installed run with NO local exe must
// install it (so the icon can launch --open), not fall back to the .url.
func TestRunInstalledEnsuresLocalExe(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	ic := goodCode()
	if err := writeConnectJSON(d, home, ic); err != nil {
		t.Fatal(err)
	}
	if err := writeAppState(d, home, appState{Name: "friend1", WebPort: 18081}); err != nil {
		t.Fatal(err)
	}
	if err := writeInstalledState(d, home, installedState{AddonBuild: "37", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://%s:%d", ic.Host, 18081)
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	if err := runInstalled(context.Background(), d, home); err != nil {
		t.Fatalf("runInstalled refused: %v", err)
	}
	if _, err := os.Stat(localExePath(d)); err != nil {
		t.Fatalf("the local exe was not installed by the installed run: %v", err)
	}
}

// TestEnsureLocalExeCopiesSelf — the installer copies itself to
// %LOCALAPPDATA%\VL\vl-friend.exe so the icon can launch --open.
func TestEnsureLocalExeCopiesSelf(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	if err := ensureLocalExe(d); err != nil {
		t.Fatalf("ensureLocalExe failed: %v", err)
	}
	if _, err := os.Stat(localExePath(d)); err != nil {
		t.Fatalf("vl-friend.exe not copied: %v", err)
	}
	// Idempotent: a second call must not error.
	if err := ensureLocalExe(d); err != nil {
		t.Fatalf("ensureLocalExe not idempotent: %v", err)
	}
}

// TestEnsureLocalExeMovesOldCopy — on update, the OLD copy is MOVED to VL\old\
// (never deleted) and replaced by the new one.
func TestEnsureLocalExeMovesOldCopy(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	// Pre-seed an OLD copy.
	if err := os.MkdirAll(filepath.Dir(localExePath(d)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localExePath(d), []byte("MZ-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The running exe is a NEWER build (flowFakeDeps writes "MZ-self").
	if err := os.WriteFile(filepath.Join(home, "vl-friend.exe"), []byte("MZ-new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureLocalExe(d); err != nil {
		t.Fatalf("ensureLocalExe failed: %v", err)
	}
	b, err := os.ReadFile(localExePath(d))
	if err != nil || string(b) != "MZ-new" {
		t.Fatalf("the new exe was not installed: %v %q", err, b)
	}
	// The old copy is MOVED to VL\old\ with a timestamped name (never deleted,
	// never overwritten).
	oldDir := filepath.Join(d.localAppDataDir(), "VL", "old")
	entries, err := os.ReadDir(oldDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one timestamped backup in VL\\old\\: %v %v", entries, err)
	}
	ob, err := os.ReadFile(filepath.Join(oldDir, entries[0].Name()))
	if err != nil || string(ob) != "MZ-old" {
		t.Fatalf("the old exe was not MOVED to VL\\old\\: %v %q", err, ob)
	}
}

// TestApplyNewCodeMovesOldAndSkipsTailscale — S6: on an installed PC, a fresh
// install code swaps connect.json + vl-app.json and skips `tailscale up` when
// already Running on our tailnet. The old connect.json is MOVED to
// VL\old\connect-<ts>.json, never overwritten or deleted.
func TestApplyNewCodeMovesOldAndSkipsTailscale(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	old := goodCode()
	if err := writeConnectJSON(d, home, old); err != nil {
		t.Fatal(err)
	}
	if err := writeAppState(d, home, appState{Name: old.Name, WebPort: old.WebPort}); err != nil {
		t.Fatal(err)
	}
	f := &tailscaleFake{installed: true, beforeState: "Running", afterUpState: "Running",
		statusJSON: statusWithPeer(old.Host)}
	d.exec = f.exec

	newIC := goodCode()
	newIC.Name = "tom2"
	newIC.ConnectKey = strings.Repeat("cd", 32)

	if err := applyNewCode(context.Background(), d, home, encodeInstallCode(newIC)); err != nil {
		t.Fatalf("applyNewCode refused: %v", err)
	}
	if f.upCalled {
		t.Fatal("tailscale up must NOT run when already Running on our tailnet")
	}

	cfg, err := readConnectConfig(d, home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnectKey != newIC.ConnectKey || cfg.Host != newIC.Host || cfg.Port != newIC.NTPort {
		t.Fatalf("connect.json not swapped to the new code: %+v", cfg)
	}

	entries, err := os.ReadDir(filepath.Join(ntVLdir(home), "old"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one old connect.json backup in VL\\old\\: %v %v", entries, err)
	}
	if !strings.HasPrefix(entries[0].Name(), "connect-") || !strings.HasSuffix(entries[0].Name(), ".json") {
		t.Fatalf("backup name unexpected: %s", entries[0].Name())
	}
	ob, err := os.ReadFile(filepath.Join(ntVLdir(home), "old", entries[0].Name()))
	if err != nil || !strings.Contains(string(ob), old.ConnectKey) {
		t.Fatalf("the old connect.json was not moved intact: %v %q", err, ob)
	}
}

// TestApplyNewCodeRefusesOtherTailnet — S6: a new code whose bot host is NOT on
// the local tailnet refuses, and connect.json stays exactly as it was.
func TestApplyNewCodeRefusesOtherTailnet(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	old := goodCode()
	if err := writeConnectJSON(d, home, old); err != nil {
		t.Fatal(err)
	}
	f := &tailscaleFake{installed: true, beforeState: "Running", afterUpState: "Running"}
	d.exec = f.exec

	newIC := goodCode()
	newIC.ConnectKey = strings.Repeat("cd", 32)
	err := applyNewCode(context.Background(), d, home, encodeInstallCode(newIC))
	if err == nil || !strings.Contains(err.Error(), "already logged in") {
		t.Fatalf("expected 'already logged in' refusal, got %v", err)
	}

	cfg, rerr := readConnectConfig(d, home)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if cfg.ConnectKey != old.ConnectKey {
		t.Fatalf("connect.json must be unchanged after a refused new code: %+v", cfg)
	}
}

// TestRunInstalledTypeNAppliesNewCode — S6: the installed screen's "Type N" gate
// applies a pasted new code end-to-end.
func TestRunInstalledTypeNAppliesNewCode(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	old := goodCode()
	if err := writeConnectJSON(d, home, old); err != nil {
		t.Fatal(err)
	}
	if err := writeAppState(d, home, appState{Name: old.Name, WebPort: old.WebPort}); err != nil {
		t.Fatal(err)
	}
	if err := writeInstalledState(d, home, installedState{AddonBuild: "37", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	f := &tailscaleFake{installed: true, beforeState: "Running", afterUpState: "Running",
		statusJSON: statusWithPeer(old.Host)}
	d.exec = f.exec
	base := fmt.Sprintf("http://%s:%d", old.Host, old.WebPort)
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	newIC := goodCode()
	newIC.Name = "tom2"
	newIC.ConnectKey = strings.Repeat("cd", 32)
	// G7: "Type N" is readLine (echo-on); the code is readSecret (echo-off).
	d.readLine = func(string) (string, error) { return "N", nil }
	d.readSecret = func(string) (string, error) { return encodeInstallCode(newIC), nil }

	if err := runInstalled(context.Background(), d, home); err != nil {
		t.Fatalf("runInstalled with Type N failed: %v", err)
	}
	cfg, err := readConnectConfig(d, home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnectKey != newIC.ConnectKey {
		t.Fatalf("the new code was not applied: %+v", cfg)
	}
}

// TestWaitNinjaTraderClosedGUINoStdin — G1: GUI mode (--open) prompts via
// messageBox, never stdin.
func TestWaitNinjaTraderClosedGUINoStdin(t *testing.T) {
	running := true
	boxes := 0
	reads := 0
	d := deps{
		ntRunning:  func(context.Context) (bool, error) { r := running; running = false; return r, nil },
		messageBox: func(string) bool { boxes++; return true },
		readLine:   func(string) (string, error) { reads++; return "", nil },
	}
	if _, err := waitNinjaTraderClosed(context.Background(), d, true); err != nil {
		t.Fatal(err)
	}
	if boxes != 1 {
		t.Fatalf("GUI mode must prompt via messageBox, got %d boxes", boxes)
	}
	if reads != 0 {
		t.Fatalf("GUI mode must not read stdin, got %d reads", reads)
	}
}

// TestWaitNinjaTraderClosedReadErrorDefers — G1: a stdin read error (after
// FreeConsole) DEFERS, never fails the update.
func TestWaitNinjaTraderClosedReadErrorDefers(t *testing.T) {
	d := deps{
		ntRunning: func(context.Context) (bool, error) { return true, nil },
		readLine:  func(string) (string, error) { return "", fmt.Errorf("stdin closed") },
	}
	deferred, err := waitNinjaTraderClosed(context.Background(), d, false)
	if err != nil {
		t.Fatalf("a read error must defer, not fail: %v", err)
	}
	if !deferred {
		t.Fatal("a read error must defer")
	}
}

// TestRunUpdateCheckAcceptsYesCaseInsensitive — G10: y/yes, any case.
func TestRunUpdateCheckAcceptsYesCaseInsensitive(t *testing.T) {
	for _, ans := range []string{"y", "Y", "yes", "YES", "Yes"} {
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
		d.positions = func(context.Context) (float64, error) { return 0, nil }
		d.readLine = func(string) (string, error) { return ans, nil }

		dec, err := RunUpdateCheck(context.Background(), d, base, false)
		if err != nil {
			t.Fatalf("answer %q errored: %v", ans, err)
		}
		if !dec.Applied {
			t.Fatalf("answer %q did not apply", ans)
		}
	}
}

// TestRunUpdateCheckAppliesWhenKnownGoesUnknown — H1: after the user closes
// NinjaTrader the bot goes known→unknown (it can no longer see NT8), but the
// last-known-flat instant did not move backwards → the update still applies.
func TestRunUpdateCheckAppliesWhenKnownGoesUnknown(t *testing.T) {
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
	flatCalls := 0
	orig := fetch
	d.fetch = func(ctx context.Context, u string) ([]byte, error) {
		if u == base+"/api/friend/flat" {
			flatCalls++
			if flatCalls <= 1 {
				return []byte(`{"flat":true,"traders":1,"at":1,"known":true,"last_known_flat_ms":100}`), nil
			}
			return []byte(`{"flat":false,"traders":1,"at":2,"known":false,"last_known_flat_ms":100}`), nil
		}
		return orig(ctx, u)
	}
	d.fetch = fetch
	d.allowedSigners = signers
	d.positions = func(context.Context) (float64, error) { return 0, nil }

	dec, err := RunUpdateCheck(context.Background(), d, base, true)
	if err != nil {
		t.Fatalf("update errored: %v", err)
	}
	if !dec.Applied {
		t.Fatal("the update must apply when the bot goes known→unknown with no change while closing")
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// withIdentity wraps fetchMap's fetch to ALSO serve GET /api/friend/identity
// with the given key_sha256 (S14).
func withIdentity(t *testing.T, base string, manifest, addon []byte, keySHA string) (func(context.Context, string) ([]byte, error), []byte) {
	t.Helper()
	fetch, signers := fetchMap(t, base, manifest, addon)
	orig := fetch
	return func(ctx context.Context, u string) ([]byte, error) {
		if u == base+"/api/friend/identity" {
			return []byte(`{"key_sha256":"` + keySHA + `"}`), nil
		}
		return orig(ctx, u)
	}, signers
}

// TestRepairAppStateRefusesWrongBot — S14: when the bot serves an identity
// endpoint with a DIFFERENT key, the repair refuses (never silently trusts a
// different friend's bot).
func TestRepairAppStateRefusesWrongBot(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	ic := goodCode()
	if err := writeConnectJSON(d, home, ic); err != nil {
		t.Fatal(err)
	}
	if err := writeInstalledState(d, home, installedState{AddonBuild: "37", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://%s:%d", ic.Host, ic.WebPort)
	wrong := sha256.Sum256([]byte(strings.Repeat("ff", 32)))
	fetch, signers := withIdentity(t, base, []byte(manifestText("37")), addonBody(), hex.EncodeToString(wrong[:]))
	d.fetch = fetch
	d.allowedSigners = signers

	err := runInstalled(context.Background(), d, home)
	if err == nil || !strings.Contains(err.Error(), "identity key mismatch") {
		t.Fatalf("expected the identity-mismatch error, got %v", err)
	}
}

// TestApplyNewCodeRefusesWrongBot — S14: applying a new code whose bot's
// identity key does not match refuses, and connect.json stays unchanged.
func TestApplyNewCodeRefusesWrongBot(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	old := goodCode()
	if err := writeConnectJSON(d, home, old); err != nil {
		t.Fatal(err)
	}
	f := &tailscaleFake{installed: true, beforeState: "Running", afterUpState: "Running",
		statusJSON: statusWithPeer(old.Host)}
	d.exec = f.exec

	newIC := goodCode()
	newIC.Name = "tom2"
	newIC.ConnectKey = strings.Repeat("cd", 32)
	base := newIC.BaseURL()
	wrong := sha256.Sum256([]byte(strings.Repeat("00", 32)))
	d.fetch = func(_ context.Context, u string) ([]byte, error) {
		if u == base+"/api/friend/identity" {
			return []byte(`{"key_sha256":"` + hex.EncodeToString(wrong[:]) + `"}`), nil
		}
		return nil, fmt.Errorf("no fetch mapped for %s", u)
	}

	err := applyNewCode(context.Background(), d, home, encodeInstallCode(newIC))
	if err == nil || !strings.Contains(err.Error(), "identity key mismatch") {
		t.Fatalf("expected identity-mismatch refusal, got %v", err)
	}
	cfg, rerr := readConnectConfig(d, home)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if cfg.ConnectKey != old.ConnectKey {
		t.Fatalf("connect.json must be unchanged after a refused new code: %+v", cfg)
	}
}

// TestApplyNewCodeAcceptsMatchingIdentity — S14: a MATCHING identity key (the
// bot is MY bot) applies the new code.
func TestApplyNewCodeAcceptsMatchingIdentity(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	old := goodCode()
	if err := writeConnectJSON(d, home, old); err != nil {
		t.Fatal(err)
	}
	f := &tailscaleFake{installed: true, beforeState: "Running", afterUpState: "Running",
		statusJSON: statusWithPeer(old.Host)}
	d.exec = f.exec

	newIC := goodCode()
	newIC.Name = "tom2"
	newIC.ConnectKey = strings.Repeat("cd", 32)
	base := newIC.BaseURL()
	want := sha256.Sum256([]byte(newIC.ConnectKey))
	d.fetch = func(_ context.Context, u string) ([]byte, error) {
		if u == base+"/api/friend/identity" {
			return []byte(`{"key_sha256":"` + hex.EncodeToString(want[:]) + `"}`), nil
		}
		return nil, fmt.Errorf("no fetch mapped for %s", u)
	}

	if err := applyNewCode(context.Background(), d, home, encodeInstallCode(newIC)); err != nil {
		t.Fatalf("matching identity must accept: %v", err)
	}
	cfg, err := readConnectConfig(d, home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnectKey != newIC.ConnectKey {
		t.Fatalf("the new code was not applied: %+v", cfg)
	}
}

// TestMigrateDocsRenameFailureNonFatal — S23: a migration move that fails warns
// and continues (never aborts the run).
func TestMigrateDocsRenameFailureNonFatal(t *testing.T) {
	home := t.TempDir()
	docs := filepath.Join(home, "OneDrive", "Documents")
	d, rec := flowFakeDeps(t, home)
	oldVL := filepath.Join(home, "Documents", "NinjaTrader 8", "VL")
	if err := os.MkdirAll(oldVL, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldVL, "connect.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.rename = func(string, string) error { return fmt.Errorf("boom") }
	if err := migrateDocsIfNeeded(d, home, docs); err != nil {
		t.Fatalf("a migration failure must be non-fatal: %v", err)
	}
	joined := strings.Join(rec.prints, "\n")
	if !strings.Contains(joined, "warning: could not move") {
		t.Fatalf("expected a warning print, got: %q", rec.prints)
	}
}

// TestEnsureLocalExeSkipsWhenCopyNewer — S23 / DS-104 I2: an OLDER running exe
// must not downgrade the local copy the icon launches.
func TestEnsureLocalExeSkipsWhenCopyNewer(t *testing.T) {
	prev := appVersion
	appVersion = "1.0.0"
	defer func() { appVersion = prev }()

	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	if err := os.MkdirAll(filepath.Dir(localExePath(d)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localExePath(d), []byte("MZ-newer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localExeVersionPath(d), []byte("1.1.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureLocalExe(d); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(localExePath(d))
	if string(b) != "MZ-newer" {
		t.Fatalf("the newer copy must NOT be downgraded: %q", b)
	}
}

// TestEnsureLocalExeReplacesWhenRunningNewer — S23: a NEWER running exe replaces
// the older copy (and records its version).
func TestEnsureLocalExeReplacesWhenRunningNewer(t *testing.T) {
	prev := appVersion
	appVersion = "1.2.0"
	defer func() { appVersion = prev }()

	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	if err := os.MkdirAll(filepath.Dir(localExePath(d)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localExePath(d), []byte("MZ-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localExeVersionPath(d), []byte("1.1.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureLocalExe(d); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(localExePath(d))
	if string(b) != "MZ-self" {
		t.Fatalf("the running exe (newer) must replace the copy: %q", b)
	}
	vb, _ := os.ReadFile(localExeVersionPath(d))
	if strings.TrimSpace(string(vb)) != "1.2.0" {
		t.Fatalf("the copy version was not recorded: %q", vb)
	}
}

// TestWriteFileAtomicSuccess — S19: temp-then-rename replaces the file and
// leaves no .tmp behind.
func TestWriteFileAtomicSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vl-app.json")
	if err := os.WriteFile(path, []byte("OLD"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{writeFile: os.WriteFile, rename: os.Rename}
	if err := writeFileAtomic(d, path, []byte("NEW"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "NEW" {
		t.Fatalf("file not atomically replaced: %q", b)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Fatal("the .tmp file must be gone after a successful rename")
	}
}

// TestWriteFileAtomicRenameFailureKeepsOld — S19: a failed rename must leave the
// OLD file intact (never a half-written state file).
func TestWriteFileAtomicRenameFailureKeepsOld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vl-app.json")
	if err := os.WriteFile(path, []byte("OLD"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{
		writeFile: os.WriteFile,
		rename:    func(string, string) error { return fmt.Errorf("rename failed") },
	}
	if err := writeFileAtomic(d, path, []byte("NEW"), 0o600); err == nil {
		t.Fatal("expected the rename error to propagate")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "OLD" {
		t.Fatalf("the old file must survive a failed rename: %q", b)
	}
}

// TestRunInstalledRepairsOutOfRangeWebPort — S19: a vl-app.json with an
// out-of-range web_port is corrupt; it is repaired (derive + verify), never used.
func TestRunInstalledRepairsOutOfRangeWebPort(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	ic := goodCode()
	if err := writeConnectJSON(d, home, ic); err != nil {
		t.Fatal(err)
	}
	if err := writeAppState(d, home, appState{Name: "friend1", WebPort: 0}); err != nil {
		t.Fatal(err)
	}
	if err := writeInstalledState(d, home, installedState{AddonBuild: "37", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://%s:%d", ic.Host, ic.WebPort)
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	if err := runInstalled(context.Background(), d, home); err != nil {
		t.Fatalf("runInstalled refused on an out-of-range web_port: %v", err)
	}
	b, _ := os.ReadFile(appStatePath(home))
	if !strings.Contains(string(b), fmt.Sprintf("%d", ic.WebPort)) {
		t.Fatalf("vl-app.json not repaired to the derived port: %q", b)
	}
}

// TestRunIncompleteOffersNewCode — S19: an existing-but-unreadable connect.json
// is an INCOMPLETE install; "Type N" applies a fresh code.
func TestRunIncompleteOffersNewCode(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	if err := os.MkdirAll(ntVLdir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(connectJSONPath(home), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &tailscaleFake{installed: true, beforeState: "Running", afterUpState: "Running",
		statusJSON: statusWithPeer("100.64.0.2")}
	d.exec = f.exec

	newIC := goodCode()
	newIC.Name = "tom2"
	newIC.ConnectKey = strings.Repeat("cd", 32)
	// G7: "Type N" is readLine (echo-on); the code is readSecret (echo-off).
	d.readLine = func(string) (string, error) { return "N", nil }
	d.readSecret = func(string) (string, error) { return encodeInstallCode(newIC), nil }

	if err := runIncomplete(context.Background(), d, home); err != nil {
		t.Fatalf("runIncomplete with Type N failed: %v", err)
	}
	cfg, err := readConnectConfig(d, home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnectKey != newIC.ConnectKey {
		t.Fatalf("the new code was not applied: %+v", cfg)
	}
}

// TestRunIncompleteNoCodeReturnsError — S19: skipping the new-code offer on an
// incomplete install is a clear error, not a silent first-run.
func TestRunIncompleteNoCodeReturnsError(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	if err := os.MkdirAll(ntVLdir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(connectJSONPath(home), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runIncomplete(context.Background(), d, home)
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected the incomplete error, got %v", err)
	}
}

// TestTailscaleExecAbsolutePathFirst — S10: the ABSOLUTE install path is tried
// first; when it succeeds, the bare name is never touched.
func TestTailscaleExecAbsolutePathFirst(t *testing.T) {
	var order []string
	d := deps{exec: func(_ context.Context, name string, args ...string) ([]byte, error) {
		order = append(order, name)
		if name == tailscaleExeFallback {
			return []byte(`{"BackendState":"Running"}`), nil
		}
		return nil, fmt.Errorf("the bare name must not be reached when the absolute path succeeds")
	}}
	if _, err := tailscaleExec(context.Background(), d, "status", "--json"); err != nil {
		t.Fatalf("tailscaleExec failed: %v", err)
	}
	if len(order) != 1 || order[0] != tailscaleExeFallback {
		t.Fatalf("absolute path must be tried first and alone; got %v", order)
	}
}

// TestTailscaleExecNoFallbackOnOtherError — S10: a NON-not-found error (daemon
// down) must NOT fall back to the bare name — retrying a different name cannot
// fix a daemon error and could run a different binary.
func TestTailscaleExecNoFallbackOnOtherError(t *testing.T) {
	calls := 0
	d := deps{exec: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls++
		return nil, fmt.Errorf("tailscaled: daemon not running")
	}}
	if _, err := tailscaleExec(context.Background(), d, "status", "--json"); err == nil {
		t.Fatal("expected the daemon error to propagate")
	}
	if calls != 1 {
		t.Fatalf("a non-notfound error must NOT fall back to the bare name; got %d calls", calls)
	}
}

// TestTailscaleExecFallsBackOnNotFound — S10: when the absolute path does not
// EXIST, the bare name is the fallback.
func TestTailscaleExecFallsBackOnNotFound(t *testing.T) {
	var order []string
	d := deps{exec: func(_ context.Context, name string, args ...string) ([]byte, error) {
		order = append(order, name)
		if name == tailscaleExeFallback {
			return nil, fs.ErrNotExist
		}
		return []byte(`{"BackendState":"Running"}`), nil
	}}
	if _, err := tailscaleExec(context.Background(), d, "status", "--json"); err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}
	if len(order) != 2 || order[0] != tailscaleExeFallback || order[1] != "tailscale" {
		t.Fatalf("expected absolute then bare fallback; got %v", order)
	}
}

// TestRedactTSKeys — S10: the tskey regex redacts a Tailscale auth key even when
// no explicit secret is passed.
func TestRedactTSKeys(t *testing.T) {
	got := redactSecrets("tailscale up failed: --authkey=tskey-auth-ABC123xyz-456")
	if strings.Contains(got, "tskey-auth-ABC123xyz-456") {
		t.Fatalf("tskey not redacted: %q", got)
	}
}

// TestEnsureLocalExeRecopiesWhenExeMissing — A3: the recorded .version is
// current but the exe itself is gone (Defender quarantine). It must be copied
// again, or the desktop icon is dead forever.
func TestEnsureLocalExeRecopiesWhenExeMissing(t *testing.T) {
	prev := appVersion
	appVersion = "1.2.0"
	defer func() { appVersion = prev }()

	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	if err := os.MkdirAll(filepath.Dir(localExePath(d)), 0o755); err != nil {
		t.Fatal(err)
	}
	// .version says current, the exe is NOT there.
	if err := os.WriteFile(localExeVersionPath(d), []byte("1.2.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureLocalExe(d); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(localExePath(d))
	if err != nil || string(b) != "MZ-self" {
		t.Fatalf("a missing exe must be re-copied even when .version is current: %v %q", err, b)
	}
	if _, err := os.Stat(localExePath(d) + ".tmp"); err == nil {
		t.Fatal("the atomic temp file was left behind")
	}
}

// TestEnsureLocalExeUntouchedWhenSameVersion — A3: exe present + same version
// is not touched (no copy, no backup).
func TestEnsureLocalExeUntouchedWhenSameVersion(t *testing.T) {
	prev := appVersion
	appVersion = "1.2.0"
	defer func() { appVersion = prev }()

	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	if err := os.MkdirAll(filepath.Dir(localExePath(d)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localExePath(d), []byte("MZ-existing"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localExeVersionPath(d), []byte("1.2.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureLocalExe(d); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(localExePath(d))
	if string(b) != "MZ-existing" {
		t.Fatalf("an up-to-date exe must not be touched: %q", b)
	}
	if _, err := os.Stat(filepath.Join(d.localAppDataDir(), "VL", "old")); err == nil {
		t.Fatal("no backup should be made when nothing is replaced")
	}
}

// TestEnsureLocalExeOlderCopyReplacedAndMoved — A3: exe present + older version
// is replaced, and the old one is MOVED to VL\old\ (never deleted).
func TestEnsureLocalExeOlderCopyReplacedAndMoved(t *testing.T) {
	prev := appVersion
	appVersion = "1.2.0"
	defer func() { appVersion = prev }()

	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	if err := os.MkdirAll(filepath.Dir(localExePath(d)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localExePath(d), []byte("MZ-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localExeVersionPath(d), []byte("1.1.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureLocalExe(d); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(localExePath(d))
	if string(b) != "MZ-self" {
		t.Fatalf("the older copy was not replaced: %q", b)
	}
	entries, err := os.ReadDir(filepath.Join(d.localAppDataDir(), "VL", "old"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected the old exe moved to VL\\old\\: %v %v", entries, err)
	}
	ob, _ := os.ReadFile(filepath.Join(d.localAppDataDir(), "VL", "old", entries[0].Name()))
	if string(ob) != "MZ-old" {
		t.Fatalf("the moved backup is wrong: %q", ob)
	}
}

// TestShortcutScriptNeverSplicesURL — P1 pin: shortcutScript takes NO URL (the
// URL rides $env:VL_ARG0), starts with psPreamble, never uses $args, and ends
// with an existence check (exit 1 / VL_RESULT=ok). The URL is also absent from
// the script a real createDesktopShortcut call encodes (sentinel URL).
// MUTANT: splice the URL back into the .url body (e.g. "URL=http://x") → RED.
func TestShortcutScriptNeverSplicesURL(t *testing.T) {
	ps := shortcutScript()
	if strings.Contains(ps, "http") {
		t.Fatalf("the script must carry no literal URL:\n%s", ps)
	}
	if strings.Contains(ps, "$args") || !strings.Contains(ps, "$env:VL_ARG0") {
		t.Fatalf("shortcutScript must read $env:VL_ARG0 and never use $args:\n%s", ps)
	}
	if !strings.HasPrefix(ps, psPreamble) {
		t.Fatalf("shortcutScript must start with psPreamble:\n%s", ps)
	}
	if !strings.Contains(ps, "exit 1") || !strings.HasSuffix(ps, "'VL_RESULT=ok'") {
		t.Fatalf("shortcutScript must end with an existence check (exit 1 / VL_RESULT=ok):\n%s", ps)
	}
	if !strings.Contains(ps, "IconLocation=$exe") {
		t.Fatalf("shortcutScript lost the IconLocation line:\n%s", ps)
	}
}

// TestCreateDesktopShortcutPassesURLAsTrailingArg — P1 pin at the production
// call site: createDesktopShortcut sends the script via -EncodedCommand with the
// URL as the TRAILING arg (the exec seam turns it into $env:VL_ARG0), and the
// URL is not inside the encoded script. MUTANT: back to -Command with the URL
// spliced → RED.
func TestCreateDesktopShortcutPassesURLAsTrailingArg(t *testing.T) {
	d, rec := flowFakeDeps(t, t.TempDir())
	const url = "http://SENTINEL-HOST-7391:18081"
	if err := createDesktopShortcut(context.Background(), d, url); err != nil {
		t.Fatalf("createDesktopShortcut: %v", err)
	}
	var got []string
	for _, e := range rec.rawExecs {
		if e[0] == "powershell.exe" {
			got = e
		}
	}
	if got == nil {
		t.Fatalf("no powershell.exe exec recorded: %v", rec.rawExecs)
	}
	if len(got) != 6 || got[1] != "-NoProfile" || got[3] != "-EncodedCommand" || got[5] != url {
		t.Fatalf("want powershell.exe -NoProfile -NonInteractive -EncodedCommand <script> %s; got %q", url, got)
	}
	for _, a := range got {
		if a == "-Command" {
			t.Fatalf("must not use -Command: %q", got)
		}
	}
	script := decodePSCommand(got[4])
	if strings.Contains(script, "SENTINEL-HOST-7391") || !strings.Contains(script, "$env:VL_ARG0") {
		t.Fatalf("the encoded script must read $env:VL_ARG0, not carry the URL:\n%s", script)
	}
}

// TestCreateDesktopShortcutErrorIsReturned — the helper surfaces an exec error
// (callers decide it is non-fatal: warn + continue).
func TestCreateDesktopShortcutErrorIsReturned(t *testing.T) {
	d, _ := flowFakeDeps(t, t.TempDir())
	d.exec = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("exit status 1") }
	if err := createDesktopShortcut(context.Background(), d, "http://x"); err == nil {
		t.Fatal("an exec failure must be returned to the (non-fatal) caller")
	}
}
