# 2026-09-29 — AUDIT2-RENAME-NT8-CI (DS-105, READ-ONLY, round 2)

**Slice:** NT8 AddOn + wire + CI (R3, R1b items 2/8) + folds F17–F19, F27.
**Plan audited:** /home/hoang/rename-vl-plan/2026-09-29-rename-nofx-to-vl-plan-v2.md, md5 `7b8dc9a9a196e5554203db7a3b71aa7b` (matches dispatch).
**Branch:** `audit2/rename-nt8-ci` · base `origin/dev` = `9d52f5dc6` (RELEASE boot 7, `git log -1` quoted).
**Round-1 report read first:** PR #275 (DS-108), report `docs/superpowers/reports/2026-09-29-audit-rename-nt8-ci.md` — its findings are the P1-1/P1-2/P1-3/P2-1/P2-2/P3-1/P3-2/P3-3 cited below.
**Mode:** L-AUDIT — the only file written is this report. No lock, no DB, no live-box action.

## Verdict: NEW P0=0 P1=4 P2=2 P3=1

## (a) Fold verification — F17, F18, F19, F27 against origin/dev 9d52f5dc6

- **F17 CORRECT + COMPLETE** [A]. `provider/ninjatrader/order_snapshot.go:234` `ExpectedAddonBuild = "2026-09-23-m21"` equals `ninjascript/VLTraderTCPClient.cs:62` `VL_BUILD_ID = "2026-09-23-m21"`; the lockstep test and bytewise floors are as round-1 P1-3 stated. The fold names the right files and the ISO-prefix rule.
- **F18 PARTIAL** — see NEW P1-1 below.
- **F19 PARTIAL** — see NEW P1-2 and NEW P2-1 below. The GHCR lines it cites are real: `.github/workflows/docker-build.yml:95-96,164,192` [A].
- **F27 CORRECT** on the repo side [A]: `docs/superpowers/runbooks/2026-09-27-one-button-update.md:78,80` already documents the `nt8_updated` park ("parks at nt8_updated" → "F5 in NT8 when the job says nt8_updated"). The D2 PR-body note half is dispatch text (nothing to verify pre-send).

Non-finding, verified: JWT issuer `auth/auth.go:230` `"nofxAI"` has NO parse-side issuer check (grep [A]) — the plan's "no logout" claim holds.

## (b) NEW findings (deeper than round 1)

### NEW P1-1 — F18 leaves the EMPTY-file shadow: a copied empty VLTrader file silently re-selects Sim101
- **Where:** F18 ("the AddOn copies it to VLTrader itself on first run") + current code `VLTraderTCPClient.cs:440-443`: `File.ReadAllText(path).Trim()`; `if (preferred.Length == 0) preferred = null` → falls through to "Sim101 (SIM default)".
- **Why it matters:** the copy-on-first-run can copy an EMPTY `NofxTrader\account.txt` (or one the operator clears later). From then on the VL file exists but is empty → `.cs:442` maps it to ABSENT → default Sim101 — while the operator believes the AddOn is trading the account they set. That is exactly the "silent account switch" F18 says must never happen, and round-1 P1-1(c) flagged this shape; F18 dropped the empty-file clause when the CTO folded it.
- **Exact fix to the plan text:** append to F18: "An EMPTY VLTrader file counts as absent (fall through to NofxTrader), matching `VLTraderTCPClient.cs:442`; the first-run copy runs only when the source is non-empty."

### NEW P1-2 — F19 renames GHCR/DockerHub image paths in R1b, which breaks the publish job until R4 (and DockerHub needs an owner click no fold mentions)
- **Where:** F19 "`.github/workflows/docker-build.yml` GHCR image paths … renamed in R1b"; actual paths `docker-build.yml:95-96` (`ghcr.io/…/nofx-$suffix`), `:164`, `:192` (`DOCKERHUB_USERNAME/nofx-$suffix`) [A].
- **Why it matters:** round-1 P1-2 itself said "GHCR registry paths do NOT follow repo renames" — so renaming the paths in R1b makes every docker-build push target a `…/vl-*` package that does not exist until R4 renames the GitHub repo → CI publish red from R1b to R4. F16 already moved the `RELEASE_REPO` flip to R4 for exactly this timing reason; F19 contradicts that precedent. DockerHub additionally requires the OWNER to create the `vl-*` repos (no fold names that click).
- **Exact fix:** F19 becomes: "`.github/workflows/docker-build.yml` image paths stay `nofx-*` through R1b; the GHCR/DockerHub path flip moves to R4 in the SAME commit as the GitHub repo rename (and the owner creates the DockerHub `vl-*` repos — add to Owner steps). The compose `${NOFX_*_PORT}` env names rename in R1b as folded."

### NEW P2-1 — F19 dropped the workflow sites round-1 P1-2 explicitly listed
- **Where:** round-1 P1-2's exact fix named `pr-docker-check.yml` image tags AND `docker-compose*.yml`/healthcheck env; F19 names only `docker-build.yml` + "docker-compose ${NOFX_*_PORT}". `pr-docker-check.yml:55,73,92,130` (`tags: nofx-…:pr-test`), `pr-checks.yml:214` (`go build -v -o nofx`) are unnamed [A].
- **Why it matters:** the census guard (R1b item 10) will flag every one of them; a lane that reads only F19 leaves them and then fights the guard mid-rename.
- **Exact fix:** extend F19's list: "`pr-docker-check.yml:55,73,92,130` image tags → `vl-*:pr-test`; `pr-checks.yml:214` `-o nofx` → `-o vl` (same commit)."

### NEW P2-2 — the round-1 P2-2 fold is MISSING from v2 (wire wording + literal pin sites)
- **Where:** round-1 P2-2's exact fix does not appear in ROUND-1 AUDIT FOLDS; R1b item 2 still reads "NT8 hello `Source: "vl-go"` (the AddOn ignores this field — verified at VLTraderTCPClient.cs:748-765 — so no AddOn change, no protocol bump)" — byte-identical to v1.
- **Why it matters:** "no protocol bump" can be misread as "touch nothing protocol-ish"; the pinned-byte sites (`provider/ninjatrader/tcp_server.go:2027`, `tcp_framing.go:118` comment, `vltrader_tcp_PROTOCOL.md:150` + `:574`, `TestGoHelloReplyIsByteIdentical`) will fail if not updated in lockstep, and the AddOn's OWN `source` `"vltrader-addon"` must not change. A lane reading only v2 has none of this.
- **Exact fix:** insert as F30: "R1b item 2 wire sentence becomes: NT8 hello reply `Source: "nofx-go"` → `"vl-go"` — NO `ProtocolVersion` change; literal sites `provider/ninjatrader/tcp_server.go:2027`, the comment at `provider/ninjatrader/tcp_framing.go:118`, `vltrader_tcp_PROTOCOL.md:150` and the pinned bytes at `:574`, `TestGoHelloReplyIsByteIdentical`; do NOT change the AddOn's own `source` (`"vltrader-addon"`)."

## (c) CONSISTENCY — phase/dispatch text that still contradicts the folds

### C-P1-1 — the R3 section still carries v1's account.txt text, contradicting F18
- **Where:** plan R3: "read `VLTrader\account.txt`, else `NofxTrader\account.txt` (and copy it across once)" — no both-exist/differ rule, no copy actor.
- **Fix:** replace R3's account sentence with F18 verbatim (including the NEW P1-1 empty-file clause).

### C-P1-2 — D2 item 8 still says only "bump VL_BUILD_ID … re-pin the .cs hash", contradicting F17 and F18
- **Where:** D2 item 8 text. It names neither `order_snapshot.go:234` `ExpectedAddonBuild` (F17) nor the both-exist WARN (F18).
- **Fix:** D2 item 8 becomes F17's exact text ("same commit: `ExpectedAddonBuild` moves to the new id; keep the ISO-date prefix…") + F18's account paragraph.

### C-P1-3 — D2 item 3 header wording is weaker than F22 and contradicts it
- **Where:** D2 item 3 "server accepts EXACTLY ONE of X-VL-Update / X-NOFX-Update = '1'" vs F22 "count VALUES across both names — exactly one value equal to '1' passes; two values (same or different name) → 403".
- **Fix:** D2 item 3 uses F22 verbatim (a pair VL=1 + NOFX=0 must 403 under F22, but passes under D2's reading).

### C-P3-1 — D2 item 8 "Do NOT copy anything into the NT8 folder" vs F18 "the AddOn copies it to VLTrader itself"
- **Where:** D2 item 8's DO-NOT line. Not a true contradiction (VLTrader is not the NT8 folder), but a lane could misread it as banning the AddOn's own copy.
- **Fix:** append to the DO-NOT line: "…(the AddOn's own account.txt copy into %USERPROFILE%\VLTrader is NOT the NT8 folder and is covered by F18)."

## Folds verdict table
| fold | verdict |
|---|---|
| F17 | CORRECT + COMPLETE [A] |
| F18 | PARTIAL — empty-file shadow (NEW P1-1) |
| F19 | PARTIAL — R1b-vs-R4 timing for registry paths (NEW P1-2); dropped pr-docker-check/pr-checks sites (NEW P2-1) |
| F27 | CORRECT (runbook already carries it) [A] |

## What this audit did NOT do
- No code change anywhere; only this report file. No race run (docs-only commit; round-1 already noted the shared box), no DB read, no live-box action, no lock, no deploy.
- Round-1 P1-1/P1-3/P2-1/P3-1/P3-2/P3-3 were folded (F18/F17/F27/F29) and were re-verified as folded, not re-raised.
