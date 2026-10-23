# Corrected vlauto PR #12 body (draft for the owner/CTO to apply)

> Drafted 2026-09-26 by lane DS-103 (VERIFY-0926, dispatch 1790398893574).
> Do not edit vlauto directly — this text is for the owner/CTO to apply to PR #12.
> Changes vs the published body: the "only in nofx" row of the match table and the
> build-commit note, per the independent cross-check (docs/superpowers/reports/
> 2026-09-26-verify-partner.md). Everything else is the published body, verbatim.

---

# DO NOT MERGE / DO NOT DEPLOY — base for the post-boot-2 sync — Partner sync: nofx 0960a6ac → 9c106d0b

Owner ruling (08:2x CT 2026-09-25): the partner machines update only after boot 2 is live with one clean session, in the weekend window. This PR is the verified BASE for the post-boot-2 sync; the boot-2 range (9c106d0b..\<boot-2 release\>) will be added on top with the file-by-file match redone. Do not merge, do not deploy.

Shared source changes from nofx `0960a6ac` through `9c106d0bc1` (the LIVE release booted 06:47 CT 2026-09-25, marker b46adeb1) applied to `vlautoagenttraderv1` via **format-patch → am** (topo-order, 676 patches, 171 skips = empty claim commits + merge duplicates), then the same **tree-level reconciliation commit** the 09-22 sync used for merge traffic. Partner commit history only — no merge with or reset to nofx history.

## nofx range

- `0960a6ac..9c106d0b` — 776 commits / 676 non-merge patches / 754 files touched.
- Everything since the 09-22 sync: W-ONE-BUTTON M2/M2.1/M3/M4 (maintenance hold), Picture HTF send consolidation, entry-policy rewrite (1a-plan P3), WAVE PLANNER A1–A6 + the planner legality table, fresh-tape, entry mechanics, roll/census hardening, and the wave's boot fixes.

## Match table (every nofx@9c106d0b file vs the partner branch)

| class | count | disposition |
|---|---|---|
| shared files, blob-identical | all others | match byte-for-byte |
| `deploy/RELEASE` | 1 | partner stamp (below) — never a nofx sha |
| `web/src/test/brand-scope-baseline.json` | 1 | partner authority file — 16 pins re-pinned (sha256) to post-sync content; 5 overridden pins named |
| only in partner | 5 | kept 2 docs (runbook + verification report); **removed 3 stale source files** (`trader/picture_htf_send.go`, `trader/picture_htf_send_test.go`, `trader/ninjatrader/market_entry_wire_test.go`) — nofx deleted all three in `dd17416c` (W5 step 2, retire Picture's own send path — a safety change; that path bypassed the shared admission gate). Proven zero callers of `pictureHtfSend`/`MarketEntryWithProtection` in the partner tree; one comment in `picture_htf_evaluator.go:754` names the retired file as context (shared nofx content) |
| only in nofx | 2113 | `.audit/**` (10) excluded — nofx-lane audit notes. **CORRECTED (DS-103 cross-check): the published body said "the other 2103 copied (docs 2044, …)". The head tree refutes that: 1632 of those files were copied and then removed by the cleanup commits 8b36b1f2 ("remove internal lane reports/audit notes", 1604 files) + 4f4397aa ("drop the 8 standalone research-harness dirs", 28 files), and 317 were never copied (316 `docs/superpowers/**` + `web/CLAUDE.md`). Net: the partner carries 165 of nofx's 2092 `docs/superpowers/` files — docs only; every trading/code file is present and blob-identical.** The copied-then-removed set was deliberate (partner machines do not need nofx lane reports); the table below is the accurate final state. Several of the retained files were build-required (`windowSweepPace`, `armScenarioLegs`, `respecWorkingArm`, `armedCancelPace`, `firstPace`, `kernel.PlannerRejectItemClass`) |

Baseline pre-check before the am: 754 touched files, 296 pre-exist on partner — all matched nofx@0960a6ac except `deploy/RELEASE` and the `GUIDE_BUILT_REV` literal in `web/src/guide/types.ts` (per-repo stamps, named and reported to the CTO before applying).

## Build commit vs branch tip (DS-103 cross-check addition)

The branch tip `3ab8652` is the RELEASE-stamp commit, a CHILD of the build commit `66a0111c` that deploy/RELEASE names. A machine that builds at the tip produces a binary with `vcs.revision=3ab8652…`, which fails the boot-integrity prefix match (`kernel/boot_integrity.go:144-147`) and the bot REFUSES trading. The machine runbook (docs/superpowers/runbooks/2026-09-22-vl-partner-update.md, corrected 2026-09-26) now pins the build to the RELEASE-named commit and adds a one-line pre-cutover equality check; future syncs align with nofx: write RELEASE at cutover, push the stamp commit only after the boot.

## AddOn (must match byte-for-byte)

All 8 `ninjascript/*` paths match nofx@9c106d0b (the published body said 7; the cross-check counts 8 paths — 5 .cs + 3 md, all blob-identical); `VLTraderTCPClient.cs` md5 `20b6a4f187e5a9a29e02300fb749b84e` = the build running on our NT8 (build_id 2026-09-23-m21). Partner-machine update must F5 + full NT8 restart and verify a frame carrying `2026-09-23-m21`.

## Build + tests (clean clone named `nofx`, partner branch)

- Binary built at **`9b91bfc`** — `go version -m`: `vcs.revision=9b91bfc086f721dcae8b8b8db17d4bf860054132`, `vcs.modified=false`; md5 `7995d96d8fb00ed2f9c7037997a25869`.
- `go vet ./...` clean.
- web: `npm ci` ok · `tsc --noEmit` clean · **full vitest 95 files / 654 passed / 1 skipped (655) / 0 failed** · production build green with `VITE_GUIDE_BUILT_REV=9b91bfc…` (gate passed, 5.1s).
- The 1 skipped test: `src/brand-scope.test.ts > preserves every existing TypeScript import target in changed files` — skipped by the test's own guard with `[base commit 954f11b1 is not in this repository (mirror clone) — import-target pin not evaluable here]`. The partner branch is a format-patch→am mirror without nofx history (runbook: never a merge or reset to nofx history), so the import-target pin that compares against nofx's `954f11b1` cannot be evaluated and the test self-disables; it PASSES on nofx where the base exists. This is the legitimately-differing partner-authority case — inventing a partner base would make the pin assert the wrong thing, so the skip stands (named here per the CTO's blocker mail).
- `go test ./...` (no -race): **GREEN — 47 `ok` packages, 0 failures** · `ok nofx/trader 1184.624s` · `ok nofx/kernel 5.492s`.
- `deploy/RELEASE` = `9b91bfc…` (the partner binary sha, committed `5bcfa52`).
- Secret-scan of the full diff: clean (0 hits on real patterns; only prose false-positives and nofx's own scan-tooling files).

## Per-machine update steps (runbook 2026-09-22-vl-partner-update.md)

1. Record running Go revision / AddOn build / strategy / positions; SQLite backup; preserve binary, frontend, RELEASE, AddOn sources.
2. Fresh flat gate: no working entries, no orphan protective orders, pending arms retired.
3. Fetch the approved branch, update without touching `.env`, local data, credentials, bindings, saved strategies.
4. NT8: back up old AddOn sources under their build ID; copy the repo C# files; compare hashes; F5; **full restart**; verify a received frame carrying `2026-09-23-m21`.
5. **Build at the BUILD commit named by deploy/RELEASE, never the branch tip** (clean clone named `nofx`, `vcs.modified=false`; one-line `vcs.revision == RELEASE` check before cutover — corrected 2026-09-26); RELEASE + GUIDE_BUILT_REV from the partner binary; then the frontend.
6. Owner's attended safe window install + service restart (v6 cutover path); verify boot, health revision, UI bundle, AddOn receipt; restore on failure.
7. Picture HTF defaults OFF; activation only via Strategy Studio after readiness (≥120 completed native 4H bars, counted bars not levels).
8. Return actual receipts (Go revision, AddOn build, data readiness, saved/runtime mode, first natural frames).

Neither partner machine was touched by this branch; SIM-only.
