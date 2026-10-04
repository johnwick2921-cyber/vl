> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# F1 — Dependency Vulnerability Scan (build & prove, no deploy)

**Branch:** `fix/security-hygiene` · **Base:** `f08a300a` · **Deploy:** rides Monday's cutover with news-hygiene (one boot for everything).
**Commit-ref:** `https://github.com/johnwick2921-cyber/vl/blob/eab22ecc6879ede4f3d242f9965d1c078cf013fc/docs/superpowers/reports/2026-08-29-f1-dependency-vuln-scan.md`

## F1a — FINDINGS (pre-fix, all fresh this run)

### Go — govulncheck `./...` (symbol-level reachability)

| # | ID | Module | Found | Fixed | Reachable in our code? | Bump |
|---|---|---|---|---|---|---|
| 1 | GO-2026-5970 | x/text | v0.29.0 | v0.39.0 | YES (trace via core text paths) | minor ✓ shipped |
| 2 | GO-2026-5676 | quic-go | v0.54.0 | v0.59.1 | YES (trace via api.Server.ListenAndServe → http3) | minor ✓ shipped |
| 3 | GO-2026-5004 | jackc/pgx/v5 | v5.6.0 | v5.9.2 | YES (indirect; SQLite runtime never uses pgx) | minor ✓ shipped |
| 4 | GO-2026-4508 | go-ethereum | v1.16.7 | v1.17.0 | YES (trace via wallet/hyperliquid paths) | minor ✓ shipped — full regression green |
| 5 | GO-2026-4315 | go-ethereum | v1.16.7 | v1.16.8 | YES | patch ✓ shipped |
| 6 | GO-2026-4314 | go-ethereum | v1.16.7 | v1.16.8 | YES | patch ✓ shipped |
| 7 | GO-2025-4233 | quic-go | v0.54.0 | v0.57.0 | YES (HTTP/3 never enabled at runtime) | ✓ covered by #2 |
| 8 | GO-2025-3553 | golang-jwt/jwt/v5 | v5.2.0 | v5.2.2 | YES — auth path, executed on EVERY API request | patch ✓ shipped |

Plus 14 vulns in imported-but-uncalled packages + 21 in required-but-uncalled modules (no symbol-level trace → left as-is; Dependabot will surface the direct ones weekly).

### npm — `npm audit --production`

14 findings pre-fix: **lodash ≤4.17.23 HIGH** (`_.template` code injection, `_.unset/_.omit` prototype pollution) · **react-router 7.9.x ×5 HIGH** (RCE via turbo-stream, DoS ×2, CSRF, open redirect, XSS — client-SPA: no SSR/RSC endpoints served, but unpatched regardless) · **form-data CRLF** (transitive) + 6 more low/moderate.

## F1b — BUMPS APPLIED + REGRESSION

- Go: x/text v0.39.0 · quic-go v0.59.1 · pgx/v5 v5.9.2 · jwt/v5 v5.2.2 · go-ethereum v1.16.8 → **v1.17.0** (attempted-and-proven: build clean, full suite green — shipped, not listed).
- npm: `npm audit fix` → react-router 7.18.3 via lockfile, 0 remaining.
- **Gates:** `govulncheck` → **0 in-our-code vulns** · `npm audit --omit=dev` → **0** · `go test ./...` **27/27** · vitest **277/277** · `npm run build` clean · gofmt clean.
- LISTED for owner (none major-required; zero risky upgrades): nothing remains — every reachable finding was patch/minor-fixable and shipped.

## F1c — AUTOMATION

- `.github/workflows/security.yml`: weekly cron (Mon 09:30 UTC) + on-PR → `golang/govulncheck-action` (`fail-on-vuln: true`) + `npm ci --omit=dev` + `npm audit --audit-level=high` (fails on HIGH/CRITICAL).
- `.github/dependabot.yml`: gomod + npm, weekly, security-labelled, **patch/minor only** (direct deps; majors are owner-ruled).

## F1d — F2a EXPOSURE ANSWER (read-only)

- **Bind:** `api/server.go:848-854` — default host **`127.0.0.1` (loopback-only)**; only `API_SERVER_HOST` env overrides it (`config.go:133-141`, loud WARN off-loopback). **Live `.env`: `API_SERVER_HOST` UNSET → loopback.** Port `8080` (`VL_BACKEND_PORT`).
- **Write routes:** 50 (POST/PUT/DELETE). **Public (no token): 5** — `/login`, `/register`, `/reset-password`, `/equity-history-batch` (public competition batch, documented as no-auth), `/strategies/estimate-tokens`. The other 45 sit behind Bearer JWT (`authMiddleware` + `planTraderOwnership` IDOR gate). Two env-gated debug seams inside the protected group: `/debug/nt-test-trade`, `/armed/test-arm` (SIM-only).
- **"Owner token":** no route requires a separate owner token — the JWT `user_id` claim IS the owner credential. Net exposure today: loopback-only, so the network can't reach any write route; F2's real subjects are JWT-secret strength + the public batch shape, not missing middleware. **F2 can ride post-NFP.**

## F1e — CANON

`AUDIT-CHECKLIST` class 22 (unprobed supply chain) appended; header → THE 22 BUG CLASSES (class 21 lands with news-hygiene; Monday's merge reconciles numbering).

## GATES — ALL GREEN (recap)

`govulncheck` 0 · `npm audit` 0 · `go test ./...` 27/27 (goldens PASS) · vitest 277/277 · web build ✓ · go build ✓.
