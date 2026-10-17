## fix(security): Go 1.26.9 + x/net v0.60.0 — clears GO-2026-6617/6613/6612

Base: `origin/dev 192ef87d9` (merge #486 rel19-brand-pin). Head: `d989837ed` on `fix/go-vuln-1026`.

govulncheck has been FAILING every PR (#483–#486) on three findings: GO-2026-6617 (`golang.org/x/net@v0.57.0`, fixed v0.60.0), GO-2026-6613 (`net/http@go1.25.13`, fixed go1.26.9), GO-2026-6612 (`golang.org/x/net@v0.57.0`, fixed v0.60.0). `net/http` only lists a 1.26 fix — the 1.25 line gets no backport — so the toolchain must move to 1.26.9.

## What changed

- `go.mod`: `go 1.25.13` → `go 1.26.9`; `golang.org/x/net v0.57.0` → `v0.60.0` via `go get golang.org/x/net@v0.60.0 && go mod tidy`. **Transitive bumps pulled by x/net v0.60.0**: `x/crypto v0.55.0→v0.57.0`, `x/sync v0.22.0→v0.23.0`, `x/sys v0.47.0→v0.48.0`, `x/text v0.41.0→v0.42.0`. Tidy also prunes dead `testify`, `gomonkey`, `yaml.v3` (CTO-confirmed: 0 imports on origin/dev).
- **No `toolchain` line added**: the `go` directive is the minimum, and `GOTOOLCHAIN=auto` resolves to exactly `go1.26.9` (proven on this box: `go version` in the worktree = `go1.26.9 linux/amd64`). The release workflow's `go-version-file: go.mod` picks up 1.26.9 from the directive.
- Stale CI pins moved off the hardcoded `go-version: '1.21'` to follow the module: `.github/workflows/pr-checks.yml:182`, `.github/workflows/pr-checks-run.yml:32` → `go-version-file: go.mod` (the other workflows already use `go-version-file: go.mod`).
- `docker/Dockerfile.backend:9` `GO_VERSION` `1.25-alpine` → `1.26-alpine`.
- `README.md` prereq `Go 1.25+` → `Go 1.26+`; `INSTALL.md:12` `Go ≥ 1.25` → `≥ 1.26.9`.
- Brand-scope re-pin: `web/src/test/brand-scope-baseline.json` `go.mod` hash → `58e9a9bc…` + dated line in `web/src/brand-scope.test.ts`.
- **Dependabot #116 (postcss-selector-parser)**: `web/package.json` adds `overrides: { "postcss-selector-parser": "^7.1.6" }` — resolves GHSA-rj75-hqrm-r3gf (PostCSS quadratic selector-parse CPU exhaustion) without the breaking `tailwindcss@4` jump. `npm install` re-resolved `postcss-selector-parser 6.1.4 → 7.1.6` (deduped under `tailwindcss@3.4.19 → postcss-nested@6.2.0`, marked `overridden`) and regenerated `web/package-lock.json`. `npm audit` drops the 2 moderate findings → 0 moderate (5 high `braces` remain — a separate advisory out of this fold's scope).

## govulncheck (head)

`go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...` → **EXIT 0**: "No vulnerabilities found. Your code is affected by 0 vulnerabilities. This scan also found 0 vulnerabilities in packages you import and 1 vulnerability in modules you require, but your code doesn't appear to call these vulnerabilities." (The three CI findings are gone.) The one remaining module-level finding does NOT fail CI: GO-2026-5932 in `golang.org/x/crypto@v0.57.0`. **GO-2026-5932 has NO fixed version** — pkg.go.dev reports `golang.org/x/crypto/openpgp` "all versions, no known fixed" (the package is unsafe by design and unmaintained; ProtonMail/go-crypto is the drop-in replacement). `x/crypto v0.57.0` is the latest — there is nothing to bump.

## Go 1.26 behaviour census (release notes read at go.dev/doc/go1.26)

- **net/http (:8080)**: `ServeMux` trailing-slash redirects 301 → 307; `net/url.Parse` rejects malformed colon-hosts. Neither affects the REST API's normal paths. [B]
- **TCP to NT8 (:36974)**: `net.Dialer` gained DialIP/DialTCP/…; no change to existing dial. [B]
- **sqlite / `-tags cgofree`**: no build-tag change; cgo overhead −30% and heap-base randomization (GOEXPERIMENT opt-outs only). `go build -tags cgofree ./...` clean. [A]
- **time/tzdata**: `asynctimerchan` GODEBUG still present in 1.26 (removed in 1.27); no change. [B]
- **New Green Tea GC enabled by default**: perf only; no correctness change expected. [B]
- **crypto**: `rsa.PrivateKey.D` consistency check + `ecdsa` big.Int deprecation — the JWT path parses keys rather than hand-constructing them; no impact. [B]

## RULES COMPLIANCE (A1–A14)

1. SIM-only ✓ · 2. reasoning=max ✓ (no AI path touched) · 3. data.db read-only ✓ · 4. worktree-only ✓ (`~/vl-ds108-govuln`) · 5. push-empty claim ✓ (`6fc2711f3`) · 6. spec freshness ✓ (re-read every cited file:line at base; dev advanced to `192ef87d9` with #486 — merged in) · 7. production call sites — toolchain bump has no unit-testable RED/GREEN; the RED is govulncheck on the base (quoted above) and GREEN at head · 8. one -race at a time ✓ · 9. guide content law — no user-visible guide names the Go version (verified `grep` across `web/src/guide/`), no guide edit · 10. no CLAUDE.md/settings/hooks ✓ · 11. no Google Drive tools ✓ · 12. no destructive knob (toolchain/dep bump only) ✓ · 13. questions to CTO ✓ · 14. commit early + push every commit ✓.

## ERROR TRACEABILITY (B1–B8)

No new error paths — a dependency/toolchain bump. `grep -n '_ ='` on the changed files shows no new hits (the workflow/docs/brand-scope edits are declarative).

## DEAD/CONFLICT LEDGER (D1–D5)

| Item | Fix | Evidence |
|---|---|---|
| stale `go-version: '1.21'` pins (pr-checks.yml / pr-checks-run.yml) | RESOLVED → `go-version-file: go.mod` (one source = go.mod) | workflow YAML |
| `GO_VERSION=1.25-alpine` docker arg | WIRED → 1.26-alpine | Dockerfile |
| brand-scope `go.mod` hash | RE-PINNED → 58e9a9bc + dated line | brand-scope.test.ts / baseline |

## Gates (head `d989837ed`)

- `go build ./...` + `go vet ./...` — clean.
- `go build -tags cgofree ./...` — clean.
- Full non-race `go test ./... -count=1` — green (48 pkgs; `vl/trader 167.7s`, `vl/provider/ninjatrader 35.4s`).
- Full `go test -race -p 4 -timeout 30m ./...` — **EXIT 0, 48 packages ok, 0 FAIL, 0 DATA RACE lines** (slowest ok: `vl/trader` 2362 pass, `vl/kernel` 1107, `vl/internal/updaterworker` 534, `vl/store` 562).
- Web: `npx tsc --noEmit` clean · `npx vitest run` 1032/1032 · build byte-identical to base (`diff -r` base vs head `dist/` = IDENTICAL at the same `VITE_GUIDE_BUILT_REV`).

## PRE-EXISTING (not introduced here): `-count=10` test-isolation failure

`go test ./provider/ninjatrader/ -count=10` fails deterministically at BOTH base `192ef87d9` and head `d989837ed`:

- `TestAttemptedEntryNoFreshSnapshotDropsAfterWait` — `attempted_replay_test.go:142` `refusal must be counted (attempted_entry_unverified), got 2` (then 3…10)
- `TestReviewAttemptedFrameDecidesFromItsOwnAccountBook` — `attempted_replay_test.go:263` `refusal must be counted (attempted_entry_unverified), got 2` (then 3…10)

Root cause: `telemetry.gateBlockCnts` (package-global map) is never reset between `-count` iterations; the tests assert an exact `== 1` on `GateBlockCountForTest`. `-count=1` (what CI runs) passes. This is a test-hygiene bug, NOT a production bug, and it is identical at base — out of scope for this security bump; flagged to the CTO for a separate fix PR if wanted.

## USABLE BY OWNER

After the next release install (the shipped binary is built with Go 1.26.9); partners need Go 1.26.9 or `GOTOOLCHAIN=auto`.
