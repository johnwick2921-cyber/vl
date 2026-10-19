# Docker tag-metadata fix — evidence (2026-10-19)

## Failure (triage, read-only)

Run `39290092554`, job `118189404117` ("Create multi-arch manifests (backend)"),
step "Create and push multi-arch manifest":

```
ERROR: ghcr.io/johnwick2921-cyber/vl/vl-backend:v2026.10.19.1-arm64: not found
##[error]Process completed with exit code 1.
```

The per-arch images were never pushed under `v2026.10.19.1-<arch>` because
`docker/metadata-action` emitted no tags for the calendar tag: `v2026.10.19.1`
is not valid semver (four numeric parts), and the workflow had no
`type=ref,event=tag` rule.

## Fix (workflow-only, one rule)

`.github/workflows/docker-build.yml` tags list gains:

```
type=ref,event=tag,suffix=-${{ matrix.arch_tag }}
```

This emits `<tag>-amd64` / `<tag>-arm64` for ANY tag, matching the
`create-manifest` job's `${REF_NAME}-<arch>` references (REF_NAME is
`github.ref_name` with `/` -> `-`). Existing branch / semver / pr / sha rules are
preserved.

## Offline tag-coverage fixture (docker/metadata-action documented semantics)

| Event                | ref_name         | manifest runs? | tags (per arch)                          | covered |
|----------------------|------------------|----------------|------------------------------------------|---------|
| tag (calendar)       | `v2026.10.19.1`  | yes            | `v2026.10.19.1-<arch>` (ref,event=tag)   | yes     |
| tag (true semver)    | `v1.2.3`         | yes            | `v1.2.3-<arch>`, `v1.2-<arch>`, `v1-<arch>` (semver + ref,event=tag) | yes |
| branch               | `dev`            | yes            | `dev-<arch>` (ref,event=branch)          | yes     |
| pull_request         | `<n>/merge`      | no (skipped)   | `pr-<n>-<arch>`, `sha-<sha>-<arch>`      | n/a     |

The Go fixtures in `deploy/docker_tag_metadata_test.go` pin the exact rules and
assert the manifest references are covered; a mutant removing the
`type=ref,event=tag` rule leaves `v2026.10.19.1-<arch>` uncovered (RED).

## Validation (retained logs)

- `go test -run TestDockerBuild -v ./deploy/` → PASS (exit 0). Log: `fleet-1017/out/docker-tag-fix/test-*.log`
- `go vet ./deploy/` → exit 0.
- `gofmt -l` → clean.
- PyYAML `safe_load` of `docker-build.yml` → OK (exit 0). Log: `fleet-1017/out/docker-tag-fix/yamlcheck.log`
- `actionlint`: not installed on this box (noted).
- Raw triage error retained: `fleet-1017/out/docker-tag-fix/triage-error.txt`
