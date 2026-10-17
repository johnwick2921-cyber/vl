package updaterworker

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"vl/internal/updaterjob"
)

// atBackupDone drives a rig to backup_done/done (held, drained, gated, backed
// up) — the moment the runner makes the AddOn decision.
func atBackupDone(t *testing.T, opts ...rigOpt) (*rig, updaterjob.Job) {
	t.Helper()
	r := newRig(t, opts...)
	r.w.crash = func(q string) {
		if q == "backup_done/done" {
			panic(crashPanic{q})
		}
	}
	if !r.runCrashing(t) {
		t.Fatal("never reached backup_done/done")
	}
	r.w.crash = nil
	j := r.job()
	if j.State != updaterjob.StateBackupDone || j.Phase != updaterjob.PhaseDone {
		t.Fatalf("at %s/%s", j.State, j.Phase)
	}
	return r, j
}

// PIN (dispatch §4(8), C12 as ruled): the AddOn decision is READ — a fresh,
// held ack for THIS job, the ack's build equal to the signed manifest's (never
// "" or n/a), and the release's ninjascript/*.cs equal by CONTENT to the
// install's. Anything else parks (nt8_updated). Every file's mtime moved far
// into the past or future changes nothing: no file date is ever read.
func TestNT8DecisionReadsTheAckNeverAFileDate(t *testing.T) {
	for _, c := range []struct {
		name   string
		opts   []rigOpt
		mutate func(r *rig)
		want   string
		reason string
	}{
		{name: "everything equal", want: updaterjob.NT8Skipped},
		{name: "no ack (NT8 closed)", mutate: func(r *rig) { r.addonConnected = false }, want: updaterjob.NT8Updated, reason: "no AddOn maintenance_ack"},
		{name: "a stale ack", mutate: func(r *rig) { r.ackStale = true }, want: updaterjob.NT8Updated, reason: "20000 ms old"},
		{name: "an ack for another job", mutate: func(r *rig) { r.ackJob = "job-u4-7777abcd" }, want: updaterjob.NT8Updated, reason: "acks job"},
		{name: "the AddOn runs another build", mutate: func(r *rig) { r.addonBuild = "2026-09-20-m19" }, want: updaterjob.NT8Updated, reason: "runs build"},
		{name: "the manifest names n/a", mutate: func(r *rig) { r.manifestBuild, r.addonBuild = "n/a", "n/a" }, want: updaterjob.NT8Updated, reason: "no addon build_id"},
		{name: "the manifest names no build", mutate: func(r *rig) { r.manifestBuild, r.addonBuild = "", "" }, want: updaterjob.NT8Updated, reason: "no addon build_id"},
		{name: "the C# changed", mutate: func(r *rig) {
			writeFile(r.t, filepath.Join(r.relDir, "ninjascript", "VLTrader.cs"), "// C# v2\n")
		}, want: updaterjob.NT8Updated, reason: "differs from the install's (VLTrader.cs)"},
		{name: "a C# file added", mutate: func(r *rig) {
			writeFile(r.t, filepath.Join(r.inst, "ninjascript", "Extra.cs"), "// old only\n")
		}, want: updaterjob.NT8Updated, reason: "(Extra.cs)"},
	} {
		for _, dates := range []string{"mtimes as written", "every mtime moved"} {
			t.Run(c.name+"/"+dates, func(t *testing.T) {
				r, j := atBackupDone(t, c.opts...)
				if c.mutate != nil {
					c.mutate(r)
				}
				if dates == "every mtime moved" {
					i := 0
					for _, root := range []string{r.inst, r.relDir} {
						filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
							if err == nil && !fi.IsDir() {
								i++
								when := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
								if i%2 == 0 {
									when = time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
								}
								os.Chtimes(p, when, when)
							}
							return nil
						})
					}
				}
				d := r.w.decideNT8(t.Context(), j)
				if d.Decision != c.want || !strings.Contains(d.Reason, c.reason) {
					t.Fatalf("decision %q (reason %q), want %q (reason containing %q)", d.Decision, d.Reason, c.want, c.reason)
				}
				if c.want == updaterjob.NT8Skipped && (d.Reason != "" || d.CSUnchanged == nil || !*d.CSUnchanged || d.AckedBuildID != boxOldBuild || d.ManifestBuildID != boxOldBuild) {
					t.Fatalf("skipped without every value READ: %+v", d)
				}
			})
		}
	}
	// at the production call site: the runner persists the decision with the
	// state it names (U1 fold: nt8_skipped ⇔ skipped, nt8_updated ⇔ updated)
	for _, c := range []struct {
		opts  []rigOpt
		state updaterjob.State
		dec   string
	}{{nil, updaterjob.StateNT8Skipped, updaterjob.NT8Skipped}, {[]rigOpt{withCSChanged()}, updaterjob.StateNT8Updated, updaterjob.NT8Updated}} {
		r := newRig(t, c.opts...)
		r.install()
		r.w.crash = func(q string) {
			if q == string(c.state)+"/done" {
				panic(crashPanic{q})
			}
		}
		r.runCrashing(t)
		j := r.job()
		if j.State != c.state || j.NT8 == nil || j.NT8.Decision != c.dec {
			t.Fatalf("persisted %s with decision %+v, want %s/%s", j.State, j.NT8, c.state, c.dec)
		}
		if c.state == updaterjob.StateNT8Updated && !strings.Contains(j.Blocker, "vl-updater resume "+boxJobID) {
			t.Fatalf("the park names no attended resume: blocker %q", j.Blocker)
		}
	}
}

// PIN (C13 as ruled): Watch is GREEN on the REFUSED boot line's rev token, so
// boot_verified reads the log itself from the offset recorded before the
// kill: a REFUSED boot rolls back, whatever Watch said.
func TestBootVerifyRefusesARefusedBootLine(t *testing.T) {
	r := newRig(t)
	r.refuseBoot[boxNew] = true
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRolledBack {
		t.Fatalf("a REFUSED boot ended %s, want rolled_back\n%v", j.State, states(j))
	}
	steps := strings.Join(receiptSteps(j), ",")
	if !strings.Contains(steps, "activate,watch,boot_verify(fail),rollback,watch,boot_verify,release_hold,main_tree_lock") {
		t.Fatalf("receipts %s", steps)
	}
	if !strings.Contains(j.Error, "REFUSED boot line") {
		t.Fatalf("error %q", j.Error)
	}

	// the scan itself, over a real log file
	sha := boxNew
	const bootPID = 4242 // the NEW MainPID the worker read after the kill
	ok := "09-24 10:00:06 [INFO] main/main.go:322 🔐 BOOT INTEGRITY OK — rev " + sha[:12] + " · pid " + strconv.Itoa(bootPID) + " · built x · expected " + sha[:12] + " · goldens PASS\n"
	refused := "09-24 10:00:08 [ERRO] main/main.go:317 🔐 BOOT INTEGRITY REFUSED — rev " + sha[:12] + " · pid " + strconv.Itoa(bootPID) + " · built x · expected " + sha[:12] + " · goldens FAIL\n"
	// The CTO's two negative shapes (#206 fold verdict on 88944226): log lines
	// that ECHO client text carrying the pieces of a boot line. Neither may be
	// read as the app's boot line.
	spoofRefusedWarn := `09-24 10:00:07 [WARN] api/handler_other.go:77 request note="x [ERRO] clone-build/main.go:1 🔐 BOOT INTEGRITY REFUSED"` + "\n"
	spoofRefusedErro := `09-24 10:00:07 [ERRO] api/handler_other.go:77 echo: /main.go: 🔐 BOOT INTEGRITY REFUSED` + "\n"
	// The same shapes the substring matcher accepted before the anchor fold.
	spoofOK := `09-24 10:00:07 [WARN] api/handler_updates.go:298 🔒 [updates] refused GET "/api/updates/jobs/BOOT INTEGRITY OK — rev ` + sha[:12] + ` ·": update header missing or wrong — first update_header refusal on /api/updates/jobs/:id this process; repeats log at DEBUG, all count in vl_updates_refused_total` + "\n"
	for _, c := range []struct {
		name, before, after string
		wantErr             string
	}{
		{name: "OK after the offset", after: ok},
		{name: "REFUSED after the offset", after: refused, wantErr: "REFUSED boot line"},
		{name: "OK then a REFUSED for another rev", after: ok + strings.Replace(refused, sha[:12], boxOld[:12], 1), wantErr: "REFUSED boot line"},
		{name: "OK only BEFORE the offset (the previous boot)", before: ok, wantErr: "no \"BOOT INTEGRITY OK — rev " + sha[:12] + "\""},
		{name: "OK for another sha", after: strings.ReplaceAll(ok, sha[:12], boxOld[:12]), wantErr: "no \"BOOT INTEGRITY OK"},
		{name: "a +dirty binary", after: strings.Replace(ok, sha[:12]+" ·", sha[:12]+" +dirty ·", 1), wantErr: "no \"BOOT INTEGRITY OK"},
		// The RELEASE half (#206 review fold): an OK line whose "expected"
		// names another rev is not this install's boot line — boot integrity
		// only passes when the binary rev equals the RELEASE marker's.
		{name: "an OK line whose expected names another rev", after: strings.Replace(ok, " expected "+sha[:12], " expected "+boxOld[:12], 1), wantErr: "no \"BOOT INTEGRITY OK"},
		// The #206 anchor negatives (the CTO's two lines first): the spoofed
		// lines are not the app's boot line — a refused spoof must not fail
		// the scan, and a forged OK must not satisfy it.
		{name: "the CTO's WARN line echoing a refused boot shape", after: ok + spoofRefusedWarn},
		{name: "the CTO's ERRO line echoing a refused boot shape", after: ok + spoofRefusedErro},
		{name: "a spoofed REFUSED at INFO is not the boot line", after: ok + strings.Replace(refused, "[ERRO]", "[INFO]", 1)},
		{name: "a REFUSED line for another pid is not ours", after: ok + strings.Replace(refused, " pid "+strconv.Itoa(bootPID)+" ", " pid 9999 ", 1)},
		{name: "a forged OK inside a client path does not satisfy", after: spoofOK, wantErr: "no \"BOOT INTEGRITY OK"},
		{name: "a forged OK from a non-main caller does not satisfy", after: strings.Replace(ok, "main/main.go:322", "api/handler_updates.go:298", 1), wantErr: "no \"BOOT INTEGRITY OK"},
		{name: "an OK line for another pid does not satisfy", after: strings.Replace(ok, " pid "+strconv.Itoa(bootPID)+" ", " pid 9999 ", 1), wantErr: "no \"BOOT INTEGRITY OK"},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "nofx_2026-09-24.log")
			writeFile(t, p, c.before)
			off := int64(len(c.before))
			f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
			f.WriteString(c.after)
			f.Close()
			err := verifyBootLine(p, off, sha, bootPID, map[string]string{})
			if (err == nil) != (c.wantErr == "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("verifyBootLine = %v, want %q", err, c.wantErr)
			}
		})
	}
	p := filepath.Join(t.TempDir(), "short.log")
	writeFile(t, p, ok)
	if err := verifyBootLine(p, 1<<20, sha, bootPID, map[string]string{}); err == nil || !strings.Contains(err.Error(), "shorter than the offset") {
		t.Fatalf("a log shorter than the recorded offset: %v", err)
	}
}

// OQ-4 and OQ-6 as ruled: after a GREEN Watch and an OK boot line, the AddOn
// must ack the NEW process with the manifest's build, and the served UI must
// hash to the signed index.html — else rollback.
func TestBootVerifyRollsBackWithoutTheAddOnOrTheReleaseUI(t *testing.T) {
	for name, c := range map[string]struct {
		afterKill func(r *rig)
		want      string
	}{
		"NT8 closed after the swap":       {func(r *rig) { r.addonConnected = false }, "no AddOn maintenance_ack"},
		"the AddOn reports another build": {func(r *rig) { r.addonBuild = "2026-09-20-m19" }, "runs build"},
		"the UI served is not the release's": {func(r *rig) {
			writeFile(r.t, filepath.Join(r.inst, "web", "dist", "index.html"), "<html>tampered</html>\n")
		}, "the served UI hashes to"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.w.crash = func(q string) {
				if q == "booted/done" {
					c.afterKill(r)
				}
			}
			j := r.runToEnd(t)
			if j.State != updaterjob.StateRolledBack || !strings.Contains(j.Error, c.want) {
				t.Fatalf("job %s (error %q), want rolled_back because %q", j.State, j.Error, c.want)
			}
		})
	}
}

// PIN (#206 review fold, runner.go:471): when the release's ninjascript/*.cs
// changed but the author forgot to bump VL_BUILD_ID (the manifest's
// addon.build_id equals what the OLD AddOn already acks), the build check
// cannot tell a restarted AddOn from the old one — a resume without any F5
// used to leave the park and activate a Go/C# mismatch. The proof of a
// restart is a NEW connection (accept_seq, assigned per connection): the
// resume must refuse while the AddOn still runs on the park's connection,
// and pass once an F5 reconnects it.
func TestResumeNeedsANewConnectionWhenTheCSharpChangedWithoutABuildBump(t *testing.T) {
	r := newRig(t)
	writeFile(r.t, filepath.Join(r.relDir, "ninjascript", "VLTrader.cs"), "// C# v2\n")
	// NO manifestBuild bump: the manifest names boxOldBuild, which the OLD
	// AddOn acks already.
	r.install()
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	if j := r.job(); j.State != updaterjob.StateNT8Updated || j.Phase != updaterjob.PhaseDone {
		t.Fatalf("no park: %s/%s", j.State, j.Phase)
	}
	// Resume with no F5: the AddOn is still on the park's connection.
	if resp := r.w.Handle(resumeRequest(boxJobID)); !resp.OK {
		t.Fatalf("resume verb: %+v", resp)
	}
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	j := r.job()
	if j.State != updaterjob.StateNT8Updated {
		t.Fatalf("a resume without an F5 left the park: %s (error %q)", j.State, j.Error)
	}
	if !strings.Contains(j.Error, "accept_seq") && !strings.Contains(j.Blocker, "accept_seq") {
		t.Fatalf("the refusal must name the unchanged connection: error %q blocker %q", j.Error, j.Blocker)
	}
	// F5 + NT8 restart reconnects the AddOn: the same build id, a NEW seq.
	r.f5()
	if resp := r.w.Handle(resumeRequest(boxJobID)); !resp.OK {
		t.Fatalf("resume verb after the F5: %+v", resp)
	}
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	if j := r.job(); j.State != updaterjob.StateComplete {
		t.Fatalf("a resume after the F5 ended %s (error %q)", j.State, j.Error)
	}
}
