package updaterworker

import (
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"vl/internal/activation"
)

// impossiblePID can never name a process: Linux caps pid_max at 2^22
// (4194304), so /proc/<impossiblePID>/stat never exists. activation's guard
// (steps.go killAndAwait → Identity.stillAlive) reads that stat file FIRST and
// refuses "no longer the process this step measured" when it is absent — so
// Activate and RollbackTo run their whole file half on temp fixtures and stop
// at the guard, before any signal. Nothing here kills, signals, runs systemctl
// or dials :8080.
const impossiblePID = 1 << 30

// PIN (the production Library IS internal/activation): each adapter method,
// called through the production constructor, reaches activation's function
// with the converted arguments and returns its converted result. Every
// receipt the library writes records the arguments it was handed (db/dest,
// from/dest, release/prev/old_pid, expect_sha/pid/since, restore/kill_pid), so
// a dropped, swapped or defaulted argument shows in the evidence.
//
// CurrentIdentity is the one method no test may drive: activation reads the
// unit's MainPID with `systemctl show` through an UNEXPORTED seam (system.go
// `var sys`), so it is proven at the source level instead — its body, like
// every other method's, must be exactly the delegation
// (TestAdapterMethodsAreExactlyTheDelegation).
func TestAdapterDelegatesToActivation(t *testing.T) {
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", impossiblePID)); err == nil {
		t.Fatalf("/proc/%d exists — the refusal fixture would name a real process; refusing to run", impossiblePID)
	}
	lib, err := NewActivationLibrary()
	if err != nil || lib == nil {
		t.Fatalf("NewActivationLibrary = %v, %v", lib, err)
	}
	const newSHA, oldSHA = "1111111111111111111111111111111111111111", "0000000000000000000000000000000000000000"

	// a release dir in activation's layout, and an install with its three halves
	relDir := t.TempDir()
	writeTree(t, relDir, map[string]string{
		"manifest.json":       `{"source_sha":"` + newSHA + `","signature_verdict":"sshsig:release:SHA256:fixture"}`,
		"nofx-bin":            "new binary (not a Go binary)\n",
		"RELEASE":             newSHA + "\n",
		"web/dist/index.html": "<html>new</html>",
	})
	newInstall := func(t *testing.T) Release {
		d := t.TempDir()
		writeTree(t, d, map[string]string{
			"nofx-bin":            "old binary\n",
			"deploy/RELEASE":      oldSHA + "\n",
			"web/dist/index.html": "<html>old</html>",
		})
		return Release{Dir: d, SHA: oldSHA, Binary: filepath.Join(d, "nofx-bin"), Dist: filepath.Join(d, "web", "dist"), ReleaseFile: filepath.Join(d, "deploy", "RELEASE")}
	}

	var rel Release
	t.Run("Resolve", func(t *testing.T) {
		got, err := lib.Resolve(relDir)
		want := Release{Dir: relDir, SHA: newSHA, Binary: filepath.Join(relDir, "nofx-bin"), Dist: filepath.Join(relDir, "web", "dist"),
			ReleaseFile: filepath.Join(relDir, "RELEASE"), ManifestPath: filepath.Join(relDir, "manifest.json")}
		if err != nil || got != want {
			t.Fatalf("Resolve = %+v, %v\nwant %+v", got, err, want)
		}
		direct, derr := activation.Resolve(relDir)
		if Release(direct) != got || derr != nil {
			t.Fatalf("adapter %+v ≠ activation.Resolve %+v (%v)", got, direct, derr)
		}
		rel = got
		// a refusal is the library's, verbatim
		empty := t.TempDir()
		_, aerr := lib.Resolve(empty)
		_, lerr := activation.Resolve(empty)
		if aerr == nil || lerr == nil || aerr.Error() != lerr.Error() {
			t.Fatalf("refusal: adapter %v, activation %v", aerr, lerr)
		}
	})

	t.Run("Stage", func(t *testing.T) {
		rc, err := lib.Stage(rel)
		direct, derr := activation.Stage(toRelease(rel))
		if err == nil || rc.OK || rc.Step != "stage" || !strings.Contains(rc.Err, "cannot read build info from "+rel.Binary) {
			t.Fatalf("Stage(a non-Go binary) = %+v, %v; want activation's refusal naming %s", rc, err, rel.Binary)
		}
		if derr == nil || rc.Err != direct.Err || err.Error() != derr.Error() {
			t.Fatalf("adapter %q ≠ activation.Stage %q", rc.Err, direct.Err)
		}
	})

	t.Run("Backup", func(t *testing.T) {
		dir := t.TempDir()
		db, dest := filepath.Join(dir, "data.db"), filepath.Join(dir, "backup", "data.db")
		h, err := sql.Open("sqlite", db) // store/sqlitedriver's registration, linked via vl/store
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.Exec("create table t(a int); insert into t values (1),(2)"); err != nil {
			t.Fatal(err)
		}
		h.Close()
		rc, err := lib.Backup(db, dest)
		if err != nil || !rc.OK || rc.Step != "backup" || rc.Evidence["db"] != db || rc.Evidence["dest"] != dest || rc.Evidence["integrity_check"] != "ok" || rc.Evidence["already"] != "" {
			t.Fatalf("Backup = %+v, %v", rc, err)
		}
		if st, err := os.Stat(dest); err != nil || st.Size() == 0 {
			t.Fatalf("no backup at %s: %v", dest, err)
		}
		// idempotent at its step — the library's own "already"
		if rc, err := lib.Backup(db, dest); err != nil || !rc.OK || rc.Evidence["already"] != "true" {
			t.Fatalf("second Backup = %+v, %v; want already=true", rc, err)
		}
	})

	t.Run("Snapshot", func(t *testing.T) {
		install := newInstall(t)
		dest := filepath.Join(t.TempDir(), "install")
		rc, err := lib.Snapshot(install, dest)
		if err != nil || !rc.OK || rc.Step != "snapshot" || rc.Evidence["from"] != install.Dir || rc.Evidence["dest"] != dest || rc.Evidence["release_marker"] != oldSHA {
			t.Fatalf("Snapshot = %+v, %v", rc, err)
		}
		for rel, want := range map[string]string{"nofx-bin": "old binary\n", "RELEASE": oldSHA + "\n", "web/dist/index.html": "<html>old</html>"} {
			if b, err := os.ReadFile(filepath.Join(dest, rel)); err != nil || string(b) != want {
				t.Fatalf("snapshot %s = %q, %v; want %q", rel, b, err, want)
			}
		}
	})

	t.Run("Activate", func(t *testing.T) {
		install := newInstall(t)
		id := Identity{PID: impossiblePID, StartTicks: 7}
		next, rc, err := lib.Activate(rel, install, id)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("pid %d is no longer the process", impossiblePID)) {
			t.Fatalf("Activate against an impossible pid = %v; want activation's identity refusal", err)
		}
		if next != (Identity{}) || rc.Step != "activate" || rc.OK || rc.Err != err.Error() ||
			rc.Evidence["release"] != newSHA || rc.Evidence["prev"] != oldSHA || rc.Evidence["old_pid"] != fmt.Sprint(impossiblePID) ||
			rc.Evidence["installed"] != "binary,dist,RELEASE" || rc.Evidence["killed"] != "" {
			t.Fatalf("Activate = %+v, %+v, %v", next, rc, err)
		}
		// rel's halves went INTO the install's paths (rel first, prev = the install)
		for path, want := range map[string]string{install.Binary: "new binary (not a Go binary)\n", install.ReleaseFile: newSHA + "\n", filepath.Join(install.Dist, "index.html"): "<html>new</html>"} {
			if b, err := os.ReadFile(path); err != nil || string(b) != want {
				t.Fatalf("%s = %q, %v; want %q", path, b, err, want)
			}
		}
	})

	t.Run("RollbackTo", func(t *testing.T) {
		install := newInstall(t)
		snap := filepath.Join(t.TempDir(), "snap")
		if _, err := lib.Snapshot(install, snap); err != nil {
			t.Fatal(err)
		}
		prev := Release{Dir: snap, SHA: oldSHA, Binary: filepath.Join(snap, "nofx-bin"), Dist: filepath.Join(snap, "web", "dist"), ReleaseFile: filepath.Join(snap, "RELEASE")}
		writeTree(t, install.Dir, map[string]string{"nofx-bin": "new binary\n", "deploy/RELEASE": newSHA + "\n", "web/dist/index.html": "<html>new</html>"})
		next, rc, err := lib.RollbackTo(prev, install, Identity{PID: impossiblePID, StartTicks: 9})
		if err == nil || !strings.HasPrefix(err.Error(), "ROLLBACK FAILED — ") || !strings.Contains(err.Error(), fmt.Sprintf("pid %d is no longer the process", impossiblePID)) {
			t.Fatalf("RollbackTo against an impossible pid = %v; want activation's ROLLBACK FAILED refusal", err)
		}
		if next != (Identity{}) || rc.Step != "rollback" || rc.OK || rc.Evidence["restore"] != oldSHA || rc.Evidence["kill_pid"] != fmt.Sprint(impossiblePID) || rc.Evidence["restored"] != "binary,dist,RELEASE" {
			t.Fatalf("RollbackTo = %+v, %+v, %v", next, rc, err)
		}
		// prev's halves went back INTO the install (prev first, install second)
		for path, want := range map[string]string{install.Binary: "old binary\n", install.ReleaseFile: oldSHA + "\n", filepath.Join(install.Dist, "index.html"): "<html>old</html>"} {
			if b, err := os.ReadFile(path); err != nil || string(b) != want {
				t.Fatalf("%s = %q, %v; want %q", path, b, err, want)
			}
		}
	})

	t.Run("Watch", func(t *testing.T) {
		sha12 := newSHA[:12]
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `{"status":"ok","revision":%q}`, sha12)
		}))
		defer srv.Close()
		now := time.Now().Truncate(time.Second)
		logPath := filepath.Join(t.TempDir(), "nofx_boot.log")
		line := now.Format("01-02 15:04:05") + " [INFO] 🔐 BOOT INTEGRITY OK — rev " + sha12 + " · built x · goldens PASS\n"
		if err := os.WriteFile(logPath, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
		id := Identity{PID: 4242, StartTicks: 1}
		since := now.Add(-time.Minute)
		rc, err := lib.Watch(rel, id, WatchOpts{LogPath: logPath, HealthURL: srv.URL + "/api/health", Since: since, Within: 20 * time.Second})
		if err != nil || !rc.OK || rc.Step != "watch" || rc.Evidence["expect_sha"] != newSHA || rc.Evidence["pid"] != "4242" ||
			rc.Evidence["since"] != since.Format(time.RFC3339) || rc.Evidence["health_sha"] != sha12 || rc.Evidence["boot_line"] == "" {
			t.Fatalf("Watch (boot line after Since, health agrees) = %+v, %v", rc, err)
		}
		// Since and Within reach the library: a kill instant AFTER the line
		// refuses it, and the refusal names the bound and the log path
		later := now.Add(time.Hour)
		rc, err = lib.Watch(rel, id, WatchOpts{LogPath: logPath, HealthURL: srv.URL + "/api/health", Since: later, Within: 1200 * time.Millisecond})
		if err == nil || rc.OK || !strings.Contains(rc.Err, "not proven within 1.2s") || !strings.Contains(rc.Err, "no boot line for "+newSHA+" in "+logPath) ||
			rc.Evidence["since"] != later.Format(time.RFC3339) {
			t.Fatalf("Watch (boot line before Since) = %+v, %v; want activation's refusal", rc, err)
		}
	})
}

// PIN (source level): every method of the production adapter is EXACTLY the
// delegation — one return statement, one result-shape helper, wrapping one
// call to the SAME-NAMED activation function whose arguments are the method's
// own parameters, in order, each passed bare or through its to* converter.
// This is the proof for CurrentIdentity (no test may drive systemctl) and a
// second net under the behavioural test for the other seven.
func TestAdapterMethodsAreExactlyTheDelegation(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "library_activation.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	outs := map[string]bool{"releaseOut": true, "receiptOut": true, "identityOut": true, "stepOut": true}
	conv := map[string]bool{"toRelease": true, "toIdentity": true, "toWatchOpts": true}
	seen := map[string]bool{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		if len(fn.Recv.List) != 1 || fmt.Sprint(fn.Recv.List[0].Type) != "activationLibrary" {
			t.Errorf("%s: a method on %v in the adapter file", fn.Name.Name, fn.Recv.List[0].Type)
			continue
		}
		name := fn.Name.Name
		seen[name] = true
		var params []string
		for _, p := range fn.Type.Params.List {
			for _, n := range p.Names {
				params = append(params, n.Name)
			}
		}
		bad := func(why string) { t.Errorf("%s: not exactly the delegation — %s", name, why) }
		if len(fn.Body.List) != 1 {
			bad(fmt.Sprintf("%d statements", len(fn.Body.List)))
			continue
		}
		ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			bad("the body is not one return of one expression")
			continue
		}
		out, ok := ret.Results[0].(*ast.CallExpr)
		if !ok || len(out.Args) != 1 || !outs[identName(out.Fun)] {
			bad("the result is not one result-shape helper of one call")
			continue
		}
		call, ok := out.Args[0].(*ast.CallExpr)
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !ok || !isSel || identName(sel.X) != "activation" || sel.Sel.Name != name {
			bad("the call is not activation." + name)
			continue
		}
		var args []string
		for _, a := range call.Args {
			switch a := a.(type) {
			case *ast.Ident:
				args = append(args, a.Name)
			case *ast.CallExpr:
				if len(a.Args) == 1 && conv[identName(a.Fun)] {
					args = append(args, identName(a.Args[0]))
					continue
				}
				args = append(args, "?")
			default:
				args = append(args, "?")
			}
		}
		if !reflect.DeepEqual(args, params) && !(len(args) == 0 && len(params) == 0) {
			bad(fmt.Sprintf("activation.%s gets %v, the method's parameters are %v", name, args, params))
		}
	}
	for _, m := range []string{"Resolve", "Stage", "Backup", "Snapshot", "Activate", "Watch", "RollbackTo", "CurrentIdentity"} {
		if !seen[m] {
			t.Errorf("the adapter file defines no %s", m)
		}
	}
}

func identName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
