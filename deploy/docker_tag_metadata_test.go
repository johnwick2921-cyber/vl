package deploy

import (
	"strings"
	"testing"
)

// W-DOCKER-TAG-METADATA (2026-10-19): Docker workflow tag fix regression.
//
// The create-manifest job references per-arch images as ${REF_NAME}-<arch>,
// where REF_NAME is github.ref_name with '/' -> '-'. A calendar tag like
// v2026.10.19.1 is NOT valid semver (four numeric parts), so the type=semver
// rules emitted nothing and the manifest step failed "not found". The fix adds
// type=ref,event=tag, which emits the RAW tag name (no semver processing).
//
// These are OFFLINE fixtures that model ONLY the ref,event=tag rule and the
// manifest's REF_NAME computation. They do NOT model docker/metadata-action's
// semver/branch/pr tag semantics — those rules are asserted by source pin
// (presence), not simulated.

// TestDockerBuildTagRulesPinned asserts the exact tag rules are present and the
// pre-existing rules are preserved.
func TestDockerBuildTagRulesPinned(t *testing.T) {
	y := repoFile(t, ".github/workflows/docker-build.yml")
	for _, want := range []string{
		"type=ref,event=branch,suffix=-${{ matrix.arch_tag }}",
		"type=ref,event=tag,suffix=-${{ matrix.arch_tag }}",
		"type=semver,pattern={{version}},suffix=-${{ matrix.arch_tag }}",
		"type=semver,pattern={{major}}.{{minor}},suffix=-${{ matrix.arch_tag }}",
		"type=semver,pattern={{major}},suffix=-${{ matrix.arch_tag }}",
		"type=ref,event=pr,suffix=-${{ matrix.arch_tag }}",
		"type=sha,prefix=sha-,suffix=-${{ matrix.arch_tag }}",
	} {
		if !strings.Contains(y, want) {
			t.Fatalf("docker-build.yml tags must carry %q", want)
		}
	}
	// The manifest step must reference the same REF_NAME the tag rule emits.
	for _, want := range []string{
		`REF_NAME=$(echo "${{ github.ref_name }}" | sed 's/\//-/g')`,
		`docker buildx imagetools create -t "${GHCR_IMAGE}:${REF_NAME}"`,
	} {
		if !strings.Contains(y, want) {
			t.Fatalf("docker-build.yml create-manifest must carry %q", want)
		}
	}
}

// refEventTagName models type=ref,event=tag: the raw tag name, unchanged (the
// action applies NO semver stripping or v-prefix handling to a ref tag).
func refEventTagName(refName string) string { return refName }

// manifestRefName models the create-manifest REF_NAME: github.ref_name with
// '/' -> '-'.
func manifestRefName(refName string) string { return strings.ReplaceAll(refName, "/", "-") }

// TestDockerBuildCalendarTagCoverage is the regression: for a calendar tag
// (no slash), the ref,event=tag rule emits <tag>-<arch>, which is exactly the
// REF_NAME-<arch> reference the manifest builds from. Semver rules are not
// involved (the tag is not valid semver).
func TestDockerBuildCalendarTagCoverage(t *testing.T) {
	const refName = "v2026.10.19.1"
	for _, arch := range []string{"amd64", "arm64"} {
		produced := refEventTagName(refName) + "-" + arch
		referenced := manifestRefName(refName) + "-" + arch
		if produced != referenced {
			t.Fatalf("ref,event=tag produces %q but the manifest references %q", produced, referenced)
		}
	}
}

// TestDockerBuildCalendarTagIsNotSemver pins the premise of the bug: the
// calendar tag is not MAJOR.MINOR.PATCH, so the semver-only rules (pre-fix)
// produced no <tag>-<arch> image.
func TestDockerBuildCalendarTagIsNotSemver(t *testing.T) {
	const refName = "v2026.10.19.1"
	v := strings.TrimPrefix(refName, "v")
	if n := len(strings.Split(v, ".")); n == 3 {
		t.Fatalf("%q unexpectedly parses as valid semver (%d parts)", refName, n)
	}
}
