package updaterworker

// ONE-BUTTON P-A (fold A1–A5): the worker's release check, reached ONLY over
// the unix socket verb `check` (the bot API relays POST /api/updates/check).
// This file owns the orchestration; updatersource owns the network client.
//
// A2 comparison: the latest release is OFFERED only when its target commit
// differs from the running binary's vcs.revision AND from every stored
// verdict's source_sha. A tag whose verdict already exists is reported
// "verified, ready" — FetchRelease refuses a second fetch of the same tag
// (release.go ErrVerdictExists), so the check never re-fetches one.
// A4: the downloaded tarball goes to <inbox>/<tag>.tar.gz and through the
// SAME updaterworker.FetchRelease the attended CLI runs (cmd/vl-updater
// main.go fetch) — no second unpack/hash/verify path.
// A5: knob VL_RELEASE_SOURCE (envcompat VL_ prefix + legacy fallback), default OFF.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vl/internal/envcompat"
	"vl/internal/updaterjob"
	"vl/internal/updatersource"
	"vl/internal/updaterwire"
)

// CheckDetail is the JSON the check answer carries in Response.Detail (fold
// A1: the wire keeps its {v,ok,state,error} shape; detail holds the typed
// result the page renders).
type CheckDetail struct {
	Available       bool   `json:"available"`
	Ready           bool   `json:"ready"`
	Tag             string `json:"tag,omitempty"`
	TargetCommitish string `json:"target_commitish,omitempty"`
	SourceSHA       string `json:"source_sha,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

func detailJSON(d CheckDetail) string {
	b, err := json.Marshal(d)
	if err != nil {
		return `{"available":false,"ready":false,"reason":"detail encode failed"}`
	}
	return string(b)
}

// decideCheck is the pure A2 comparison (unit-tested directly).
// verdictForTag: a valid verdict exists for latest.Tag.
// verdictShas: every stored verdict's source_sha.
func decideCheck(latest updatersource.Latest, runningRevision string, verdictShas map[string]bool, verdictForTag bool) (state string, detail CheckDetail) {
	if latest.Tag == "" || latest.TargetCommitish == "" {
		return "error", CheckDetail{Reason: "release is missing tag or target commit"}
	}
	// running-first (live fix 2026-10-02 18:13 CT): the RUNNING release answers
	// up_to_date regardless of any verdict — the installed release keeps its
	// verdict file, and offering "verified, ready" for it ends in the install
	// refusal "already this release" (job e141a9c2cab2).
	if latest.TargetCommitish == runningRevision {
		return "up_to_date", CheckDetail{Available: false, Ready: false, Tag: latest.Tag, TargetCommitish: latest.TargetCommitish}
	}
	if verdictForTag {
		return "verified_ready", CheckDetail{Available: true, Ready: true, Tag: latest.Tag, TargetCommitish: latest.TargetCommitish}
	}
	if verdictShas[latest.TargetCommitish] {
		return "up_to_date", CheckDetail{Available: false, Ready: false, Tag: latest.Tag, TargetCommitish: latest.TargetCommitish}
	}
	return "available", CheckDetail{Available: true, Ready: false, Tag: latest.Tag, TargetCommitish: latest.TargetCommitish}
}

// checkSource builds the release source (fold A3/A5). Tests swap it for an
// httptest-backed source; production uses the real GitHub bounds. The knob
// is checked separately (releaseSourceOn), so a swapped source is still OFF
// unless the test sets VL_RELEASE_SOURCE=github.
var checkSource = func() *updatersource.Source {
	return updatersource.New(updatersource.Config{})
}

// handleCheck answers the check verb. It never holds the job mutex: the
// download+fetch can take minutes, and FetchRelease serializes itself with
// its own release-root lock. The API-side timeout (workerProbeTimeout for a
// probe; the page's fetch for check) bounds the caller.
func (w *Worker) handleCheck() updaterwire.Response {
	if !releaseSourceOn() {
		return ok("off")
	}
	src := checkSource()
	ctx, cancel := context.WithTimeout(context.Background(), updatersource.DefaultTimeout)
	defer cancel()

	latest, err := src.Latest(ctx)
	if err != nil {
		switch {
		case errors.Is(err, updatersource.ErrRateLimited):
			return okWithDetail("rate_limited", detailJSON(CheckDetail{Reason: "rate limited, try later"}))
		case errors.Is(err, updatersource.ErrInvalidTag):
			return okWithDetail("error", detailJSON(CheckDetail{Reason: "release tag not valid"}))
		case errors.Is(err, updatersource.ErrReleaseCommitUnknown):
			return okWithDetail("error", detailJSON(CheckDetail{Reason: "release commit unknown"}))
		default:
			w.logf("updater: check API: %v", err)
			return okWithDetail("error", detailJSON(CheckDetail{Reason: "release API unavailable"}))
		}
	}

	running, modified, err := w.host.BuildInfo(w.cfg.Target.InstallBinaryPath())
	if err != nil || modified != "false" {
		w.logf("updater: check build info: rev=%q modified=%q err=%v", running, modified, err)
		return okWithDetail("error", detailJSON(CheckDetail{Reason: "running binary build info unavailable"}))
	}

	shas, forTag := w.verdictIndex(latest.Tag)
	state, detail := decideCheck(latest, running, shas, forTag)
	switch state {
	case "verified_ready", "up_to_date", "error":
		if state == "verified_ready" {
			if v, err := updaterjob.ReadVerdict(w.dataDir(), latest.Tag); err == nil {
				detail.SourceSHA = v.SourceSHA
			}
		}
		return okWithDetail(state, detailJSON(detail))
	}

	// available: download into the inbox, then the SAME FetchRelease as the
	// attended CLI (A4). On ANY failure nothing of ours remains: the partial
	// file is removed by updatersource, and FetchRelease writes nothing on a
	// refusal.
	inbox, err := releaseInbox()
	if err != nil {
		w.logf("updater: check inbox: %v", err)
		return okWithDetail("error", detailJSON(CheckDetail{Reason: "release inbox unconfigured"}))
	}
	if _, err := src.Download(ctx, latest.Tag, inbox); err != nil {
		w.logf("updater: check download %s: %v", latest.Tag, err)
		reason := "download failed"
		if err == updatersource.ErrSizeCap {
			reason = "release exceeds the size bound"
		}
		return okWithDetail("error", detailJSON(CheckDetail{Reason: reason}))
	}
	root, err := ReleaseRoot(w.cfg.Target.InstallDir)
	if err != nil {
		w.logf("updater: check release root: %v", err)
		return okWithDetail("error", detailJSON(CheckDetail{Reason: "release dir unconfigured"}))
	}
	v, err := FetchRelease(FetchConfig{
		Archive:        filepath.Join(inbox, latest.Tag+".tar.gz"),
		ReleaseID:      latest.Tag,
		ReleaseRoot:    root,
		AllowedSigners: ReleaseAllowedSignersPath(w.cfg.Target.InstallDir),
		DataDir:        w.dataDir(),
	})
	if err != nil {
		w.logf("updater: check fetch %s: %v", latest.Tag, err)
		return okWithDetail("error", detailJSON(CheckDetail{Reason: "verification failed"}))
	}
	detail.Ready = true
	detail.SourceSHA = v.SourceSHA
	return okWithDetail("verified_ready", detailJSON(detail))
}

// releaseSourceOn is the A5 knob: VL_RELEASE_SOURCE exactly "github" turns
// the source on; anything else (unset, "off", anything) is OFF. Fail-closed.
func releaseSourceOn() bool {
	v, _ := envcompat.Env("RELEASE_SOURCE")
	return strings.TrimSpace(v) == "github"
}

// releaseInbox returns the inbox dir (envcompat RELEASE_INBOX, same var the
// CLI reads) when it is an existing absolute directory.
func releaseInbox() (string, error) {
	inbox, _ := envcompat.Env("RELEASE_INBOX")
	inbox = strings.TrimSpace(inbox)
	if inbox == "" {
		return "", fmt.Errorf("RELEASE_INBOX is not set")
	}
	if !filepath.IsAbs(inbox) {
		return "", fmt.Errorf("RELEASE_INBOX %q must be an absolute path", inbox)
	}
	if fi, err := os.Stat(inbox); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("RELEASE_INBOX %q is not a directory", inbox)
	}
	return inbox, nil
}

// verdictIndex scans <data>/updater/verdicts: the set of source shas and
// whether a verdict exists for tag. Unreadable verdict files are logged and
// skipped (they cannot vouch for anything; the comparison just doesn't use
// them).
func (w *Worker) verdictIndex(tag string) (map[string]bool, bool) {
	shas := map[string]bool{}
	forTag := false
	dir := filepath.Join(w.dataDir(), updaterwire.UpdaterDirName, "verdicts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return shas, forTag
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if !updaterwire.ValidReleaseID(id) {
			continue
		}
		v, err := updaterjob.ReadVerdict(w.dataDir(), id)
		if err != nil {
			w.logf("updater: check: verdict %s unreadable: %v", id, err)
			continue
		}
		shas[v.SourceSHA] = true
		if id == tag {
			forTag = true
		}
	}
	return shas, forTag
}

func okWithDetail(state, detail string) updaterwire.Response {
	return updaterwire.Response{OK: true, State: state, Detail: detail}
}
