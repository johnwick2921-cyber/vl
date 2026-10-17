package updaterjob

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// UPDATE_GOLDEN=1 rewrites testdata/*.golden.json from the production writer;
// a reviewer reads the diff. Never set in CI.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("golden %s: %v", p, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("job file differs from %s\n--- got ---\n%s\n--- want ---\n%s", p, got, want)
	}
}

// fullJob walks the production API down the longest path — the AddOn park,
// the attended resume, activation, a failed boot, a rollback whose Watch
// fails — and sets every field the schema has, so the golden shows every key.
func fullJob(t *testing.T, dd string) Job {
	t.Helper()
	now := t0
	id := "0123456789abcdef0123456789abcdef"
	sha := "0123456789abcdef0123456789abcdef01234567"
	old := "89abcdef0123456789abcdef0123456789abcdef"
	j := mustNew(t, id, "v1.2.0", now)
	mustWrite(t, dd, j)
	for _, s := range []State{StateDownloaded, StateVerified} {
		step(t, dd, &j, &now, s)
	}
	j.SourceSHA = sha
	j.Release = &Release{Dir: "/home/u/nofx-releases/" + sha, SHA: sha, Binary: "/home/u/nofx-releases/" + sha + "/nofx-bin",
		Dist: "/home/u/nofx-releases/" + sha + "/web/dist", ReleaseFile: "/home/u/nofx-releases/" + sha + "/RELEASE",
		ManifestPath: "/home/u/nofx-releases/" + sha + "/manifest.json"}
	j.Install = &Release{Dir: "/home/u/nofx", SHA: old, Binary: "/home/u/nofx/nofx-bin", Dist: "/home/u/nofx/web/dist", ReleaseFile: "/home/u/nofx/deploy/RELEASE"}
	for _, s := range []State{StatePreflightOK, StateMaintenanceHeld} {
		step(t, dd, &j, &now, s)
	}
	// UPDATER-NT8-CLOSED: the drain path + the server's disconnect stamp (the
	// maximal-key fixture carries every schema key, not one real run's story).
	j.DrainPath = "nt8_absent"
	j.LinkDownSince = "2026-09-24T18:00:00.000000000Z"
	for _, s := range []State{StateDrainedAcked, StateGateOK} {
		step(t, dd, &j, &now, s)
	}
	j.BackupPath = "/home/u/nofx-backups/updater/" + id + "/data.db"
	j.Snapshot = &Release{Dir: "/home/u/nofx-backups/updater/" + id + "/install", SHA: old, Binary: "/home/u/nofx-backups/updater/" + id + "/install/nofx-bin",
		Dist: "/home/u/nofx-backups/updater/" + id + "/install/web/dist", ReleaseFile: "/home/u/nofx-backups/updater/" + id + "/install/deploy/RELEASE"}
	step(t, dd, &j, &now, StateBackupDone)
	cs := false
	j.NT8 = &NT8Decision{Decision: NT8Updated, Reason: "ninjascript/*.cs changed", ManifestBuildID: "2026-09-24-m4", AckedBuildID: "2026-09-23-m21", AckedAt: "2026-09-24T18:02:30Z", AckAcceptSeq: 7, CSUnchanged: &cs, Absent: true, F5Owed: true}
	step(t, dd, &j, &now, StateNT8Updated)
	j.Blocker = "attended AddOn F5 required"
	mustWrite(t, dd, j)
	now = now.Add(time.Minute)
	resumed := now
	j.ResumedAt = &resumed
	if err := j.AddReceipt(Receipt{Step: "resume", StartedAt: now, EndedAt: now, OK: true, Evidence: map[string]string{"acked_build_id": "2026-09-24-m4"}}, now); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dd, j)
	j.IdentityBefore = &Identity{PID: 172, StartTicks: 23987}
	off := int64(81920)
	since := now.Add(time.Second)
	j.LogPath, j.LogOffset, j.WatchSince = "/home/u/nofx/data/nofx_2026-09-24.log", &off, &since
	step(t, dd, &j, &now, StateActivated)
	j.IdentityAfter = &Identity{PID: 9120, StartTicks: 24410}
	mustWrite(t, dd, j)
	now = now.Add(time.Second)
	if err := j.Enter(StateBooted, now); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dd, j)
	now = now.Add(90 * time.Second)
	if err := j.AddReceipt(Receipt{Step: "watch", StartedAt: now.Add(-90 * time.Second), EndedAt: now, OK: false, Err: "no boot line for 0123456789ab within 1m30s"}, now); err != nil {
		t.Fatal(err)
	}
	j.Error = "watch: no boot line for 0123456789ab within 1m30s"
	j.IdentityRollback = &Identity{PID: 9120, StartTicks: 24410}
	roff := int64(90112)
	rsince := now
	j.RollbackLogPath, j.RollbackLogOffset, j.RollbackWatchSince = "/home/u/nofx/data/nofx_2026-09-24.log", &roff, &rsince
	if err := j.Enter(StateRollingBack, now); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dd, j)
	restored := now.Add(5 * time.Second)
	if err := j.AddReceipt(Receipt{Step: "rollback", StartedAt: now, EndedAt: restored, OK: true, Evidence: map[string]string{"restored": "binary,dist,release"}}, restored); err != nil {
		t.Fatal(err)
	}
	now = restored.Add(90 * time.Second)
	if err := j.AddReceipt(Receipt{Step: "watch", StartedAt: restored, EndedAt: now, OK: false, Err: "no boot line for 89abcdef0123 within 1m30s"}, now); err != nil {
		t.Fatal(err)
	}
	good := len(j.Receipts) - 2
	j.LastGoodReceipt = &good
	j.RecoveryReason = "rollback watch RED: the old build did not prove itself"
	j.Error = "watch: no boot line for 89abcdef0123 within 1m30s"
	if err := j.Enter(StateRecoveryNeeded, now); err != nil {
		t.Fatal(err)
	}
	j.Blocker = "attended recovery: nofx-updater recovery " + id
	mustWrite(t, dd, j)
	got, err := Read(dd, id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// schemaKeys walks a type's JSON keys, dotted by path.
func schemaKeys(rt reflect.Type, prefix string, out map[string]bool) {
	for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct || rt == reflect.TypeOf(time.Time{}) {
		return
	}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			name = f.Name
		}
		out[prefix+name] = true
		schemaKeys(f.Type, prefix+name+".", out)
	}
}

func fileKeys(v any, prefix string, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if prefix == "receipts.evidence." { // free-form, the library's
				continue
			}
			out[prefix+k] = true
			fileKeys(e, prefix+k+".", out)
		}
	case []any:
		for _, e := range x {
			fileKeys(e, prefix, out)
		}
	}
}

// TestJobFileSchemaGolden: the exact bytes the production writer puts on
// disk — for a job at creation (only the always-present keys) and for the
// longest path with every field set — and the full golden carries every key
// the schema has, so a key added, renamed or dropped is a reviewed diff.
func TestJobFileSchemaGolden(t *testing.T) {
	restoreSeams(t)
	dd := t.TempDir()
	j := mustNew(t, "0123456789abcdef0123456789abcdef", "v1.2.0", t0)
	mustWrite(t, dd, j)
	p, _ := Path(dd, j.JobID)
	b, _ := os.ReadFile(p)
	golden(t, "job_requested.golden.json", b)

	d2 := t.TempDir()
	full := fullJob(t, d2)
	p, _ = Path(d2, full.JobID)
	b, _ = os.ReadFile(p)
	golden(t, "job_full.golden.json", b)

	want := map[string]bool{}
	schemaKeys(reflect.TypeOf(Job{}), "", want)
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	fileKeys(raw, "", got)
	// install and snapshot are the Release type too (with manifest_path
	// absent by design): a Release key is exercised when any of the three
	// carries it.
	fold := func(m map[string]bool) map[string]bool {
		out := map[string]bool{}
		for k := range m {
			for _, p := range []string{"install.", "snapshot."} {
				if strings.HasPrefix(k, p) {
					k = "release." + strings.TrimPrefix(k, p)
				}
			}
			out[k] = true
		}
		return out
	}
	want, got = fold(want), fold(got)
	var missing, extra []string
	for k := range want {
		if !got[k] {
			missing = append(missing, k)
		}
	}
	for k := range got {
		if !want[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing)+len(extra) > 0 {
		t.Errorf("the full golden does not exercise the schema: missing %v, extra %v", missing, extra)
	}
	// the minimal golden: exactly the always-present keys (absent ≠ empty)
	var minimal map[string]json.RawMessage
	b0, _ := os.ReadFile(filepath.Join("testdata", "job_requested.golden.json"))
	_ = json.Unmarshal(b0, &minimal)
	var top []string
	for k := range minimal {
		top = append(top, k)
	}
	sort.Strings(top)
	if w := []string{"attempts", "created_at", "job_id", "phase", "receipts", "release_id", "schema", "state", "transitions", "updated_at"}; !reflect.DeepEqual(top, w) {
		t.Errorf("a new job carries keys %v, want exactly %v", top, w)
	}
}

// activationReceipt is activation.Receipt COPIED from
// origin/feat/one-button-m4-activation at 79be322d
// (internal/activation/activation.go:52-59, last changed by 9d410b41 "3b-A(2):
// Backup, Activate and Watch"). The package is not on dev, so U1 may not import
// it; U4 replaces this copy with a reflect parity test against the real type.
type activationReceipt struct {
	Step      string            `json:"step"`
	StartedAt time.Time         `json:"started_at"`
	EndedAt   time.Time         `json:"ended_at"`
	OK        bool              `json:"ok"`
	Evidence  map[string]string `json:"evidence,omitempty"`
	Err       string            `json:"err,omitempty"`
}

// activationRelease / activationIdentity: activation.go:31-38 and :44-47 at
// the same sha (no JSON tags there).
type activationRelease struct {
	Dir          string
	SHA          string
	Binary       string
	Dist         string
	ReleaseFile  string
	ManifestPath string
}

type activationIdentity struct {
	PID        int
	StartTicks uint64
}

// These conversions compile only while the mirrors have the library's field
// names, types and order (Go ignores tags in a struct conversion) — the form
// the worker uses to persist a library receipt without re-mapping it.
var (
	_ = Receipt(activationReceipt{})
	_ = Release(activationRelease{})
	_ = Identity(activationIdentity{})
)

// TestReceiptTagsMatchActivationGolden: Receipt carries activation.Receipt's
// exact fields and JSON tags, so a library receipt lands in the job file
// field for field (its times normalized to UTC — the same instant, not
// necessarily the same rendering); Release and Identity carry the library's
// fields in its order.
func TestReceiptTagsMatchActivationGolden(t *testing.T) {
	type field struct{ name, typ, tag string }
	fields := func(v any, withTags bool) []field {
		rt := reflect.TypeOf(v)
		var out []field
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			tag := ""
			if withTags {
				tag = string(f.Tag)
			}
			out = append(out, field{f.Name, f.Type.String(), tag})
		}
		return out
	}
	// the golden, spelled out (79be322d activation.go:52-59)
	want := []field{
		{"Step", "string", `json:"step"`},
		{"StartedAt", "time.Time", `json:"started_at"`},
		{"EndedAt", "time.Time", `json:"ended_at"`},
		{"OK", "bool", `json:"ok"`},
		{"Evidence", "map[string]string", `json:"evidence,omitempty"`},
		{"Err", "string", `json:"err,omitempty"`},
	}
	if got := fields(Receipt{}, true); !reflect.DeepEqual(got, want) {
		t.Errorf("Receipt = %v\nwant activation.Receipt's %v", got, want)
	}
	if got := fields(activationReceipt{}, true); !reflect.DeepEqual(got, want) {
		t.Errorf("the activation copy drifted from its golden: %v", got)
	}
	strip := func(fs []field) []field {
		for i := range fs {
			fs[i].tag = ""
		}
		return fs
	}
	if got, w := fields(Release{}, false), strip(fields(activationRelease{}, false)); !reflect.DeepEqual(got, w) {
		t.Errorf("Release fields %v, want activation.Release's %v", got, w)
	}
	if got, w := fields(Identity{}, false), strip(fields(activationIdentity{}, false)); !reflect.DeepEqual(got, w) {
		t.Errorf("Identity fields %v, want activation.Identity's %v", got, w)
	}
	// byte golden: a library receipt and ours encode identically
	at := time.Date(2026, 9, 24, 18, 5, 0, 0, time.UTC)
	lib := activationReceipt{Step: "stage", StartedAt: at, EndedAt: at.Add(time.Second), OK: true, Evidence: map[string]string{"vcs.revision": "0123456789abcdef0123456789abcdef01234567", "vcs.modified": "false"}}
	a, _ := json.Marshal(lib)
	b, _ := json.Marshal(Receipt(lib))
	const wantJSON = `{"step":"stage","started_at":"2026-09-24T18:05:00Z","ended_at":"2026-09-24T18:05:01Z","ok":true,"evidence":{"vcs.modified":"false","vcs.revision":"0123456789abcdef0123456789abcdef01234567"}}`
	if string(a) != wantJSON || string(b) != wantJSON {
		t.Errorf("receipt JSON:\nlibrary %s\nours    %s\nwant    %s", a, b, wantJSON)
	}
	failed, _ := json.Marshal(Receipt{Step: "watch", StartedAt: at, EndedAt: at, Err: "x"})
	if string(failed) != `{"step":"watch","started_at":"2026-09-24T18:05:00Z","ended_at":"2026-09-24T18:05:00Z","ok":false,"err":"x"}` {
		t.Errorf("failed receipt JSON %s", failed)
	}
}

// TestJobFileCarriesNoSecret: no key in the job schema (walked recursively)
// can carry a grant, a MAC, a key, a token, a password, an e-mail, a user or
// account — requested_by and grant are ABSENT (C14 as ruled) — the full
// golden spells none of them, and the reader refuses a file that adds one.
func TestJobFileCarriesNoSecret(t *testing.T) {
	restoreSeams(t)
	keys := map[string]bool{}
	schemaKeys(reflect.TypeOf(Job{}), "", keys)
	if len(keys) < 30 {
		t.Fatalf("positive control: walked only %d keys", len(keys))
	}
	forbidden := []string{"grant", "hmac", "token", "secret", "password", "passwd", "email", "requested_by", "user", "account", "expires", "key", "bearer", "jwt", "cookie", "auth"}
	for k := range keys {
		low := strings.ToLower(k)
		leaf := low[strings.LastIndex(low, ".")+1:]
		for _, f := range forbidden {
			if strings.Contains(low, f) {
				t.Errorf("job schema key %q contains %q", k, f)
			}
		}
		if leaf == "mac" || strings.HasPrefix(leaf, "mac_") || strings.HasSuffix(leaf, "_mac") {
			t.Errorf("job schema key %q is a MAC", k)
		}
	}
	b, err := os.ReadFile(filepath.Join("testdata", "job_full.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range forbidden {
		if bytes.Contains(bytes.ToLower(b), []byte(`"`+f)) {
			t.Errorf("the full job file spells a key starting %q", f)
		}
	}
	// the reader cannot be made to carry one either
	dd := t.TempDir()
	j := mustNew(t, "job-0070", "v1.2.0", t0)
	mustWrite(t, dd, j)
	p, _ := Path(dd, j.JobID)
	clean, _ := os.ReadFile(p)
	for _, k := range []string{"grant", "hmac", "requested_by", "email", "token"} {
		forged := bytes.Replace(clean, []byte(`"schema": 1,`), []byte(`"schema": 1,`+"\n  \""+k+`": "x",`), 1)
		_ = os.WriteFile(p, forged, 0o600)
		if _, err := Read(dd, j.JobID); !errors.Is(err, ErrCorrupt) {
			t.Errorf("a job file with %q read as %v, want ErrCorrupt", k, err)
		}
	}
}
