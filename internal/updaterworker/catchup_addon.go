package updaterworker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vl/internal/addonbuild"
	"vl/internal/updaterjob"
	"vl/internal/updaterwire"
)

// ── m355 — the catch-up must compile from the VERIFIED installed release ─────
//
// The 10-21 18:31 incident (m339): the catch-up job compiled the INSTALLED
// release's ninjascript (which still declared VL_BUILD_ID "2026-10-18-a") and
// expected oldInstallBuildID() from the SAME tree — self-consistent, so a
// stale install source silently re-placed a stale AddOn over the correct
// "2026-10-21-c" that release #45 had just placed. The catch-up now:
//
//  1. compiles ONLY from the verified installed-release artifact —
//     <VL_RELEASE_DIR>/<running revision>/ninjascript — located by the running
//     binary's vcs.revision and re-proved through its verdict + SSHSIG. When
//     that artifact is not available the AddOn step is SKIPPED with a logged
//     reason; InstallDir/ninjascript is NEVER a fallback (it is not a proof).
//  2. expects the artifact's SIGNED manifest addon.build_id — never
//     oldInstallBuildID().
//  3. never downgrades: a build that is not strictly newer than what the box
//     runs (the maintenance ack) or loads (the placed DLL) is refused and the
//     AddOn is left alone.

// buildIDParts splits a build id into its numeric date and suffix. Build ids
// are YYYY-MM-DD(-suffix) with suffix [A-Za-z0-9]+; ok is false unless the id
// parses exactly (two-padded month/day).
func buildIDParts(id string) (y, m, d int, suffix string, ok bool) {
	id = strings.TrimSpace(id)
	if len(id) < 10 || id[4] != '-' || id[7] != '-' {
		return 0, 0, 0, "", false
	}
	for i, c := range id[:10] {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return 0, 0, 0, "", false
		}
	}
	y = int(id[0]-'0')*1000 + int(id[1]-'0')*100 + int(id[2]-'0')*10 + int(id[3]-'0')
	m = int(id[5]-'0')*10 + int(id[6]-'0')
	d = int(id[8]-'0')*10 + int(id[9]-'0')
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return 0, 0, 0, "", false
	}
	rest := id[10:]
	if rest == "" {
		return y, m, d, "", true
	}
	if rest[0] != '-' {
		return 0, 0, 0, "", false
	}
	suffix = rest[1:]
	if suffix == "" {
		return 0, 0, 0, "", false
	}
	for _, c := range suffix {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return 0, 0, 0, "", false
		}
	}
	return y, m, d, suffix, true
}

// compareBuildID orders two build ids: -1 (a older), 0 (equal), +1 (a newer).
// ok is false when either does not parse — an order cannot be proven, and the
// caller treats that as "not provably newer" (the safe side).
func compareBuildID(a, b string) (int, bool) {
	ay, am, ad, as, aok := buildIDParts(a)
	by, bm, bd, bs, bok := buildIDParts(b)
	if !aok || !bok {
		return 0, false
	}
	if ay != by {
		return sign1(ay - by), true
	}
	if am != bm {
		return sign1(am - bm), true
	}
	if ad != bd {
		return sign1(ad - bd), true
	}
	// Same date: the suffix breaks the tie (absent < present; then by length,
	// then lexical — so "-aa" follows "-z").
	if as == bs {
		return 0, true
	}
	if as == "" {
		return -1, true
	}
	if bs == "" {
		return 1, true
	}
	if len(as) != len(bs) {
		return sign1(len(as) - len(bs)), true
	}
	if as < bs {
		return -1, true
	}
	return 1, true
}

func sign1(n int) int {
	if n < 0 {
		return -1
	}
	return 1
}

// buildIDNewer reports whether a names a strictly LATER build than b.
func buildIDNewer(a, b string) bool {
	cmp, ok := compareBuildID(a, b)
	return ok && cmp > 0
}

// runningReleaseDir resolves the VERIFIED installed-release directory: the
// release dir whose signature the install verified, located by the running
// binary's vcs.revision (<VL_RELEASE_DIR>/<revision>). An error means no
// verified artifact can be named — the caller skips, never falls back.
func (w *Worker) runningReleaseDir() (string, error) {
	rev, mod, err := w.host.BuildInfo(w.cfg.Target.InstallBinaryPath())
	if err != nil {
		return "", fmt.Errorf("the running binary's revision is unreadable: %w", err)
	}
	if mod != "false" || !isSHA40(rev) {
		return "", fmt.Errorf("the running binary is modified or unnamed (rev %q, modified %q)", rev, mod)
	}
	root, err := ReleaseRoot(w.cfg.Target.InstallDir)
	if err != nil {
		return "", fmt.Errorf("release root: %w", err)
	}
	dir := filepath.Join(root, rev)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("no verified release dir %s for the running build %s", dir, rev)
	}
	return dir, nil
}

// catchupAddonSource is the verified ninjascript a catch-up compiles from:
// <VL_RELEASE_DIR>/<running revision>/ninjascript, resolved and existing. An
// error means the catch-up must skip/refuse — InstallDir/ninjascript is never
// used.
func (w *Worker) catchupAddonSource() (string, error) {
	dir, err := w.runningReleaseDir()
	if err != nil {
		return "", err
	}
	src := filepath.Join(dir, "ninjascript")
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("no ninjascript under the verified release dir %s", dir)
	}
	return src, nil
}

// catchupAddonFacts re-proves the verified installed release for a catch-up:
// the verdict whose source_sha is the running revision, re-verified NOW
// (SSHSIG) so the signed manifest's addon.build_id and the ninjascript
// artifacts are the PROVEN ones — never the install tree's.
func (w *Worker) catchupAddonFacts() (ReleaseFacts, error) {
	dir, err := w.runningReleaseDir()
	if err != nil {
		return ReleaseFacts{}, err
	}
	rev := filepath.Base(dir)
	v, ok, err := verdictForSourceSHA(w.dataDir(), rev)
	if err != nil {
		return ReleaseFacts{}, fmt.Errorf("verdicts: %w", err)
	}
	if !ok {
		return ReleaseFacts{}, fmt.Errorf("no verdict names the running build %s", rev)
	}
	if filepath.Clean(v.ReleaseDir) != dir {
		return ReleaseFacts{}, fmt.Errorf("the verdict for the running build %s names %s, not %s", rev, v.ReleaseDir, dir)
	}
	f, err := w.rel.Reverify(mirrorVerdict(v))
	if err != nil {
		return ReleaseFacts{}, fmt.Errorf("re-verify the running build %s: %w", rev, err)
	}
	if f.SourceSHA != rev {
		return ReleaseFacts{}, fmt.Errorf("the re-verified release is %s, not the running build %s", f.SourceSHA, rev)
	}
	if len(csFromArtifacts(f.Artifacts)) == 0 {
		return ReleaseFacts{}, fmt.Errorf("the verified release lists no %s — the AddOn source cannot be proven", ninjascriptGlob)
	}
	return f, nil
}

// verdictForSourceSHA returns the verdict whose source_sha is sourceSHA (the
// release that produced the RUNNING binary). ok is false when none matches.
// An unreadable verdict is skipped — it cannot vouch for a release, and the
// catch-up's skip is the safe side.
func verdictForSourceSHA(dataDir, sourceSHA string) (updaterjob.Verdict, bool, error) {
	dir := filepath.Join(dataDir, updaterwire.UpdaterDirName, "verdicts")
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return updaterjob.Verdict{}, false, nil
		}
		return updaterjob.Verdict{}, false, err
	}
	var names []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		releaseID := strings.TrimSuffix(n, ".json")
		v, err := updaterjob.ReadVerdict(dataDir, releaseID)
		if err != nil {
			continue
		}
		if v.SourceSHA == sourceSHA {
			return v, true, nil
		}
	}
	return updaterjob.Verdict{}, false, nil
}

// catchupNeverDowngrade (m355 rule 3) refuses a catch-up placement that is not
// a STRICT upgrade over what the box runs or loads now: the running AddOn's
// ack build when NT8 is open, and the currently placed DLL's embedded build id
// (what the next NT8 start loads) always. A build that cannot be proven newer
// than an existing one is refused — the AddOn is left alone.
func (w *Worker) catchupNeverDowngrade(ctx context.Context, docsDir, wantBuild string) error {
	if !validBuildID(wantBuild) {
		return nil // nothing to rank; verify-on-place + the sha still guard
	}
	var refs []string
	if m, merr := w.app.Maintenance(ctx); merr == nil && m.AddonAck != nil {
		refs = append(refs, m.AddonAck.BuildID)
	}
	if got, err := addonbuild.BuildIDFromDLL(filepath.Join(docsDir, "bin", "Custom", addonDllName)); err == nil && got != "" {
		refs = append(refs, got)
	}
	for _, ref := range refs {
		if !validBuildID(ref) {
			continue
		}
		if cmp, ok := compareBuildID(wantBuild, ref); !ok || cmp < 0 {
			return fmt.Errorf("refusing to place build %q — it is not newer than the build already on the box (%q)", wantBuild, ref)
		}
	}
	return nil
}
