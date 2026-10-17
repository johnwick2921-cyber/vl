package updaterjob

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// m5JobViewKeys reads M5's UpdateJobView keys from the page's own client
// (web/src/lib/api/updates.ts), so a key M5 adds or drops is a failure here,
// not a silent mismatch.
func m5JobViewKeys(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "lib", "api", "updates.ts"))
	if err != nil {
		t.Fatalf("M5's client is where the view's keys are defined: %v", err)
	}
	src := string(b)
	i := strings.Index(src, "export interface UpdateJobView {")
	if i < 0 {
		t.Fatal("updates.ts has no UpdateJobView")
	}
	end := strings.Index(src[i:], "\n}")
	body := src[i : i+end]
	var keys []string
	for _, m := range regexp.MustCompile(`(?m)^\s+([a-z_]+)\??:`).FindAllStringSubmatch(body, -1) {
		keys = append(keys, m[1])
	}
	sort.Strings(keys)
	return keys
}

func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestAPIViewProjectsOnlyComputedFields: the projection's keys are exactly
// M5's UpdateJobView keys minus the client-side "status"; each optional key
// appears only once it is computed (absent ≠ empty — no "step":"" on a job
// that is between steps, no receipt_url before a receipt exists); and
// nothing else in the job file — paths, identities, backup location, log
// offsets, AddOn build ids — reaches the API. Built from the file the
// production Write wrote and the production Read returned.
func TestAPIViewProjectsOnlyComputedFields(t *testing.T) {
	restoreSeams(t)
	m5 := m5JobViewKeys(t)
	if want := []string{"blocker", "error", "job_id", "receipt_url", "state", "status", "step", "timestamps"}; !reflect.DeepEqual(m5, want) {
		t.Fatalf("M5 UpdateJobView keys = %v, want %v (M5 changed: re-derive the projection)", m5, want)
	}
	var server []string
	for _, k := range m5 {
		if k != "status" {
			server = append(server, k)
		}
	}
	var tags []string
	rt := reflect.TypeOf(APIView{})
	for i := 0; i < rt.NumField(); i++ {
		tags = append(tags, strings.Split(rt.Field(i).Tag.Get("json"), ",")[0])
	}
	sort.Strings(tags)
	if !reflect.DeepEqual(tags, server) {
		t.Fatalf("APIView keys = %v, want exactly M5's minus status %v", tags, server)
	}

	dd := t.TempDir()
	now := t0
	j := mustNew(t, "job-0060", "v1.2.0", now)
	mustWrite(t, dd, j)
	read := func() Job {
		t.Helper()
		got, err := Read(dd, "job-0060")
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	ts := func(x time.Time) string { return x.UTC().Format(time.RFC3339Nano) }

	// at creation: job_id, state, timestamps{requested} — nothing else
	v := View(read())
	if got := jsonKeys(t, v); !reflect.DeepEqual(got, []string{"job_id", "state", "timestamps"}) {
		t.Errorf("view at creation has keys %v, want [job_id state timestamps]", got)
	}
	if v.JobID != "job-0060" || v.State != StateRequested || !reflect.DeepEqual(v.Timestamps, map[State]string{StateRequested: ts(t0)}) {
		t.Errorf("view at creation = %+v", v)
	}
	rv := Receipts(read())
	if b, _ := json.Marshal(rv); string(b) != `{"job_id":"job-0060","state":"requested","receipts":[]}` {
		t.Errorf("receipts view at creation = %s (receipts must be [] — computed empty, not null or absent)", b)
	}

	// a step running: step names the effect; still no receipt_url
	now = now.Add(time.Second)
	if err := j.Enter(StateDownloaded, now); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dd, j)
	v = View(read())
	if got := jsonKeys(t, v); !reflect.DeepEqual(got, []string{"job_id", "state", "step", "timestamps"}) {
		t.Errorf("view while downloading has keys %v", got)
	}
	if v.Step != EffectDownload || len(v.Timestamps) != 1 {
		t.Errorf("view while downloading = %+v (a started state has no done time yet)", v)
	}

	// the step done: no step; receipt_url; the done time
	now = now.Add(time.Second)
	if err := j.AddReceipt(Receipt{Step: "download", StartedAt: now.Add(-time.Second), EndedAt: now, OK: true}, now); err != nil {
		t.Fatal(err)
	}
	if err := j.Finish(now); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dd, j)
	v = View(read())
	if got := jsonKeys(t, v); !reflect.DeepEqual(got, []string{"job_id", "receipt_url", "state", "timestamps"}) {
		t.Errorf("view after a receipt has keys %v", got)
	}
	if v.ReceiptURL != "/api/updates/jobs/job-0060/receipt" || v.ReceiptURL != ReceiptURL("job-0060") {
		t.Errorf("receipt_url = %q", v.ReceiptURL)
	}
	if !reflect.DeepEqual(v.Timestamps, map[State]string{StateRequested: ts(t0), StateDownloaded: ts(now)}) {
		t.Errorf("timestamps = %v", v.Timestamps)
	}
	rv = Receipts(read())
	if len(rv.Receipts) != 1 || rv.Receipts[0].Step != "download" || rv.State != StateDownloaded {
		t.Errorf("receipts view = %+v", rv)
	}

	// blocker and error: verbatim when recorded
	now = now.Add(time.Second)
	if err := j.Enter(StateVerified, now); err != nil {
		t.Fatal(err)
	}
	j.Blocker = "addon_ack: no ack for job-0060 within 15s"
	j.Error = "signature: principal mismatch"
	mustWrite(t, dd, j)
	v = View(read())
	if v.Blocker != j.Blocker || v.Error != j.Error {
		t.Errorf("blocker/error not verbatim: %+v", v)
	}

	// a job carrying everything the worker persists: none of it leaks
	sha := "0123456789abcdef0123456789abcdef01234567"
	old := "89abcdef0123456789abcdef0123456789abcdef"
	off := int64(4096)
	since := now
	cs := true
	f := j
	f.Blocker, f.Error = "", ""
	f.SourceSHA = sha
	f.Release = &Release{Dir: "/rel/" + sha, SHA: sha, Binary: "/rel/" + sha + "/nofx-bin", Dist: "/rel/" + sha + "/web/dist", ReleaseFile: "/rel/" + sha + "/RELEASE", ManifestPath: "/rel/" + sha + "/manifest.json"}
	f.Install = &Release{Dir: "/inst", SHA: old, Binary: "/inst/nofx-bin", Dist: "/inst/web/dist", ReleaseFile: "/inst/deploy/RELEASE"}
	f.Snapshot = &Release{Dir: "/bak/install", SHA: old, Binary: "/bak/install/nofx-bin", Dist: "/bak/install/web/dist", ReleaseFile: "/bak/install/deploy/RELEASE"}
	f.BackupPath = "/bak/data.db"
	f.IdentityBefore = &Identity{PID: 17231, StartTicks: 239871}
	f.LogPath, f.LogOffset, f.WatchSince = "/inst/data/nofx_2026-09-24.log", &off, &since
	f.NT8 = &NT8Decision{Decision: NT8Skipped, ManifestBuildID: "BUILD-M-77", AckedBuildID: "BUILD-M-77", AckedAt: "2026-09-24T18:09:40Z", CSUnchanged: &cs}
	b, _ := json.Marshal(View(f))
	for _, leak := range []string{"/rel/", "/inst", "/bak", "nofx-bin", "17231", "239871", "4096", "BUILD-M-77", sha, old, "nofx_2026"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("the API view leaks %q: %s", leak, b)
		}
	}
	if got := jsonKeys(t, View(f)); len(got) > len(server) {
		t.Errorf("view keys %v exceed M5's", got)
	}
}
