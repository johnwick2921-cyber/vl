package deploy

import (
	"regexp"
	"strings"
	"testing"
)

// W-DOCKER-TAG-METADATA (2026-10-19): the Docker workflow's create-manifest job
// references per-arch images as ${REF_NAME}-amd64 / ${REF_NAME}-arm64, where
// REF_NAME is github.ref_name with '/' -> '-'. The build-and-push job must emit
// those tags. A tag like v2026.10.19.1 is NOT valid semver (four numeric parts),
// so the type=semver rules emit nothing and the manifest step failed with
// "not found". The fix is a type=ref,event=tag rule, which emits the raw tag
// name for ANY tag. These are OFFLINE fixtures that encode the documented
// docker/metadata-action semantics — they are NOT a run of the real action.

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

var semverRE = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// metadataRefTags encodes the documented type=ref tag names (docker/metadata-action):
// branch -> branch name, tag -> tag name, pr -> pr-<number>. Returns the set for one arch.
func metadataRefTags(event, refName, arch string) map[string]bool {
	base := ""
	switch event {
	case "branch", "tag":
		base = refName
	case "pr":
		base = "pr-" + refName // ref_name is "<number>/merge"; number is the leading digits
	}
	return map[string]bool{base + "-" + arch: true}
}

// metadataSemverTags encodes type=semver tags for a valid semver ref only.
func metadataSemverTags(refName, arch string) map[string]bool {
	v := strings.TrimPrefix(refName, "v")
	if !semverRE.MatchString(refName) {
		return map[string]bool{}
	}
	parts := strings.Split(v, ".")
	return map[string]bool{
		refName + "-" + arch:                         true, // {{version}}
		"v" + parts[0] + "." + parts[1] + "-" + arch: true, // {{major}}.{{minor}}
		"v" + parts[0] + "-" + arch:                  true, // {{major}}
	}
}

// TestDockerBuildTagCoverageForTagEvents is the regression: for a calendar tag
// (invalid semver), the type=ref,event=tag rule must produce the exact
// REF_NAME-<arch> references the manifest builds from.
func TestDockerBuildTagCoverageForTagEvents(t *testing.T) {
	cases := []struct {
		name        string
		refName     string
		validSemver bool
	}{
		{"calendar tag v2026.10.19.1", "v2026.10.19.1", false},
		{"true semver tag v1.2.3", "v1.2.3", true},
		{"branch dev", "dev", false},
	}
	for _, c := range cases {
		for _, arch := range []string{"amd64", "arm64"} {
			tags := map[string]bool{}
			for k := range metadataRefTags("tag", c.refName, arch) {
				tags[k] = true
			}
			for k := range metadataSemverTags(c.refName, arch) {
				tags[k] = true
			}
			// The manifest references REF_NAME-<arch> (REF_NAME == ref_name for tags,
			// and the branch rule supplies branch names the same way).
			refName := strings.ReplaceAll(c.refName, "/", "-")
			want := refName + "-" + arch
			if !tags[want] {
				t.Fatalf("%s %s: tag %q missing from produced set %v", c.name, arch, want, tags)
			}
		}
	}
}

// TestDockerBuildTagCoverageCalendarTagWithoutTagRule is the mutant guard: if the
// type=ref,event=tag rule is removed, a calendar tag (invalid semver) emits NO
// v2026.10.19.1-<arch> tag and the manifest reference is uncovered.
func TestDockerBuildTagCoverageCalendarTagWithoutTagRule(t *testing.T) {
	// This fixture mirrors the pre-fix state: only semver rules for a tag event.
	refName := "v2026.10.19.1"
	for _, arch := range []string{"amd64", "arm64"} {
		tags := metadataSemverTags(refName, arch) // semver-only (no ref,event=tag rule)
		want := refName + "-" + arch
		if tags[want] {
			t.Fatalf("pre-fix semver-only rules must NOT produce %q (that was the bug)", want)
		}
	}
}
