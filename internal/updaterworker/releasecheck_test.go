package updaterworker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/updatersource"
	"vl/internal/updaterwire"
)

// stubHost implements Host for the check tests: a fixed build revision and
// no waits.
type stubHost struct {
	rev string
}

func (stubHost) Now() time.Time                             { return time.Now().UTC() }
func (stubHost) Sleep(context.Context, time.Duration) error { return nil }

func (stubHost) LockAcquire(string, string, int) (bool, string, error) {
	return false, "", nil
}
func (stubHost) LockHolder() (string, error)                { return "", nil }
func (stubHost) LockRelease(string) error                   { return nil }
func (s stubHost) BuildInfo(string) (string, string, error) { return s.rev, "false", nil }
func (stubHost) ExeOf(int) (string, error)                  { return "", fmt.Errorf("unused") }
func (stubHost) AddonRestore(string) error                  { return nil }

// checkWorker builds a Worker with just enough target for handleCheck.
func checkWorker(t *testing.T) *Worker {
	t.Helper()
	root := t.TempDir()
	inst := filepath.Join(root, "install")
	data := filepath.Join(root, "data")
	for _, d := range []string{inst, data} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return &Worker{
		cfg: Config{
			Target: Target{InstallDir: inst, DataDir: data},
			Logf:   func(string, ...any) {},
		},
		host: stubHost{rev: "aaaa" + strings.Repeat("bb", 18)},
	}
}

// checkSourceAt points the seam at a test source. t.Cleanup restores it.
func checkSourceAt(t *testing.T, src *updatersource.Source) {
	t.Helper()
	prev := checkSource
	checkSource = func() *updatersource.Source { return src }
	t.Cleanup(func() { checkSource = prev })
}

func apiJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

// commitShaAnswer answers commits/<tag> as GitHub does for the sha media type
// the updater sends (OPS NEW-1): the bare sha as text. Any other Accept gets
// the commit JSON, so a regression to the JSON read still finds a sha here
// and only the updatersource pin catches it.
func commitShaAnswer(w http.ResponseWriter, r *http.Request, sha string) {
	if r.Header.Get("Accept") == "application/vnd.github.sha" {
		w.Header().Set("Content-Type", "application/vnd.github.sha; charset=utf-8")
		fmt.Fprint(w, sha)
		return
	}
	apiJSON(w, 200, `{"sha":"`+sha+`"}`)
}

func TestReleaseSourceKnobDefaultOff(t *testing.T) {
	t.Setenv("VL_RELEASE_SOURCE", "")
	t.Setenv("NO"+"FX_RELEASE_SOURCE", "")
	w := checkWorker(t)
	if w.Handle(updaterwire.NewCheck()).State != "off" {
		t.Fatal("unset knob did not answer off")
	}
	t.Setenv("VL_RELEASE_SOURCE", "yes-please")
	if w.Handle(updaterwire.NewCheck()).State != "off" {
		t.Fatal("a non-github knob value did not answer off (fail-closed)")
	}
}

func TestDecideCheckTable(t *testing.T) {
	rev := "a" + strings.Repeat("b", 39)
	other := "c" + strings.Repeat("d", 39)
	cases := []struct {
		name          string
		latest        updatersource.Latest
		running       string
		verdictShas   map[string]bool
		verdictForTag bool
		wantState     string
	}{
		{"missing tag", updatersource.Latest{Tag: "", TargetCommitish: rev}, rev, nil, false, "error"},
		{"missing commitish", updatersource.Latest{Tag: "v1", TargetCommitish: ""}, rev, nil, false, "error"},
		{"verdict for tag exists", updatersource.Latest{Tag: "v9", TargetCommitish: other}, rev, nil, true, "verified_ready"},
		{"the RUNNING release keeps its verdict: up_to_date first (live 18:13 fix)", updatersource.Latest{Tag: "v9", TargetCommitish: rev}, rev, nil, true, "up_to_date"},
		{"same commit as running", updatersource.Latest{Tag: "v9", TargetCommitish: rev}, rev, nil, false, "up_to_date"},
		{"commit already in a verdict", updatersource.Latest{Tag: "v9", TargetCommitish: other}, rev, map[string]bool{other: true}, false, "up_to_date"},
		{"newer", updatersource.Latest{Tag: "v9", TargetCommitish: other}, rev, nil, false, "available"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state, detail := decideCheck(c.latest, c.running, c.verdictShas, c.verdictForTag)
			if state != c.wantState {
				t.Fatalf("state = %q, want %q (detail %+v)", state, c.wantState, detail)
			}
		})
	}
}

func TestHandleCheckUpToDateByCommit(t *testing.T) {
	t.Setenv("VL_RELEASE_SOURCE", "github")
	rev := "aaaa" + strings.Repeat("bb", 18)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/"+updatersource.ReleaseRepo+"/commits/v9.9.9" {
			commitShaAnswer(w, r, rev)
			return
		}
		apiJSON(w, 200, `{"tag_name":"v9.9.9","target_commitish":"dev"}`)
	}))
	defer srv.Close()
	checkSourceAt(t, testCheckSource(srv))
	w := checkWorker(t)
	w.host = stubHost{rev: rev}
	resp := w.Handle(updaterwire.NewCheck())
	if !resp.OK || resp.State != "up_to_date" {
		t.Fatalf("resp = %+v, want ok/up_to_date", resp)
	}
}

func TestHandleCheckRateLimited(t *testing.T) {
	t.Setenv("VL_RELEASE_SOURCE", "github")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiJSON(w, 429, `{"message":"rate limit"}`)
	}))
	defer srv.Close()
	checkSourceAt(t, testCheckSource(srv))
	w := checkWorker(t)
	resp := w.Handle(updaterwire.NewCheck())
	if !resp.OK || resp.State != "rate_limited" {
		t.Fatalf("resp = %+v, want ok/rate_limited", resp)
	}
	var d CheckDetail
	if err := json.Unmarshal([]byte(resp.Detail), &d); err != nil || d.Reason == "" {
		t.Fatalf("detail = %q (%v), want a reason", resp.Detail, err)
	}
}

// TestHandleCheckVerifiedReadyReusesFetchRelease is the A2+A4 integration:
// the worker downloads the asset and runs the SAME FetchRelease the CLI uses
// (write-once verdict), and a second check of the same tag answers
// "verified, ready" WITHOUT a second download (FetchRelease refuses
// verdict-exists — the check must skip, never error).
func TestHandleCheckVerifiedReadyReusesFetchRelease(t *testing.T) {
	t.Setenv("VL_RELEASE_SOURCE", "github")
	rel := buildRelease(t, releaseOpts{})
	var assetHits int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assetHits++
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			apiJSON(w, 200, `{"tag_name":"`+testReleaseID+`","target_commitish":"dev"}`)
			return
		case strings.HasSuffix(r.URL.Path, "/commits/"+testReleaseID):
			commitShaAnswer(w, r, testSHA)
			return
		}
		b, err := os.ReadFile(rel.archive)
		if err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	base := t.TempDir()
	inbox := filepath.Join(base, "inbox")
	relDir := filepath.Join(base, "releases")
	data := filepath.Join(base, "data")
	for _, d := range []string{inbox, relDir, data} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("VL_RELEASE_INBOX", inbox)
	t.Setenv("VL_RELEASE_DIR", relDir)

	w := checkWorker(t)
	w.cfg.Target.DataDir = data
	// The worker verifies against the INSTALL's deploy/release_allowed_signers
	// (the same file FetchRelease reads for the CLI): give the fixture install
	// the fixture signer.
	signers := filepath.Join(w.cfg.Target.InstallDir, "deploy", "release_allowed_signers")
	if err := os.MkdirAll(filepath.Dir(signers), 0o700); err != nil {
		t.Fatal(err)
	}
	signerBytes, err := os.ReadFile(rel.signers)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(signers, signerBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	w.host = stubHost{rev: "ffff" + strings.Repeat("ee", 18)} // NOT testSHA
	checkSourceAt(t, testCheckSource(srv))

	first := w.Handle(updaterwire.NewCheck())
	if !first.OK || first.State != "verified_ready" {
		t.Fatalf("first = %+v, want ok/verified_ready (detail %q)", first, first.Detail)
	}
	var d CheckDetail
	if err := json.Unmarshal([]byte(first.Detail), &d); err != nil || !d.Ready || d.SourceSHA != testSHA {
		t.Fatalf("first detail = %+v (%v), want ready with source_sha %s", d, err, testSHA)
	}
	before := assetHits
	second := w.Handle(updaterwire.NewCheck())
	if !second.OK || second.State != "verified_ready" {
		t.Fatalf("second = %+v, want ok/verified_ready", second)
	}
	if assetHits != before {
		t.Fatalf("second check downloaded again (%d new hits)", assetHits-before)
	}
	if _, err := os.Stat(filepath.Join(inbox, testReleaseID+".tar.gz")); err != nil {
		t.Fatalf("inbox archive missing after the check: %v", err)
	}
}

// TestHandleCheckRejectsOffListHost proves the allow-list holds end to end:
// the API answer (an httptest URL) is only reachable because the test injects
// 127.0.0.1 — a redirect to localhost (off-list, and http) is refused.
func TestHandleCheckRejectsOffListHost(t *testing.T) {
	t.Setenv("VL_RELEASE_SOURCE", "github")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost:1/asset", http.StatusFound)
	}))
	defer srv.Close()
	checkSourceAt(t, testCheckSource(srv))
	w := checkWorker(t)
	resp := w.Handle(updaterwire.NewCheck())
	if !resp.OK || resp.State != "error" {
		t.Fatalf("resp = %+v, want ok/error", resp)
	}
	var d CheckDetail
	if err := json.Unmarshal([]byte(resp.Detail), &d); err != nil || d.Reason == "" {
		t.Fatalf("detail = %q (%v), want a reason", resp.Detail, err)
	}
}

// TestHandleCheckInvalidTagLeavesInboxEmpty: a refused tag (path traversal
// from the API) answers error with the exact reason and touches NOTHING.
func TestHandleCheckInvalidTagLeavesInboxEmpty(t *testing.T) {
	t.Setenv("VL_RELEASE_SOURCE", "github")
	inbox := t.TempDir()
	t.Setenv("VL_RELEASE_INBOX", inbox)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiJSON(w, 200, `{"tag_name":"../evil","target_commitish":"`+strings.Repeat("ab", 20)+`"}`)
	}))
	defer srv.Close()
	checkSourceAt(t, testCheckSource(srv))
	w := checkWorker(t)
	resp := w.Handle(updaterwire.NewCheck())
	if !resp.OK || resp.State != "error" {
		t.Fatalf("resp = %+v, want ok/error", resp)
	}
	var d CheckDetail
	if err := json.Unmarshal([]byte(resp.Detail), &d); err != nil || d.Reason != "release tag not valid" {
		t.Fatalf("detail = %q (%v), want 'release tag not valid'", resp.Detail, err)
	}
	if entries, _ := os.ReadDir(inbox); len(entries) != 0 {
		t.Fatalf("inbox not empty after a refused tag: %v", entries)
	}
}

// testCheckSource is a check-scope source whose API and asset live on srv.
func testCheckSource(srv *httptest.Server) *updatersource.Source {
	return updatersource.New(updatersource.Config{
		Hosts:        []string{"127.0.0.1"},
		MaxBytes:     1 << 20,
		Timeout:      5 * time.Second,
		CacheTTL:     15 * time.Minute,
		MaxRedirects: 3,
		APIBase:      srv.URL,
		DownloadBase: srv.URL,
		Transport:    srv.Client().Transport,
	})
}
