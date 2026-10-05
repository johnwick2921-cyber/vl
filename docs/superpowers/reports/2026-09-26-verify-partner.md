# VERIFY-0926 PARTNER SYNC — independent adversarial cross-check of vlauto PR #12

- Lane: DS-103. Branch: `verify/0926-partner` (claim 88c0a201, base 04ae1c2f).
- Base: `git log -1` = 04ae1c2f "RELEASE 6cac1b89 — boot 3 … booted 19:33:22 CT 2026-09-25" (origin/dev tip at accept; spec = CTO dispatch 1790398893574 + file:line facts re-read at this base).
- Subject: vlautoagenttraderv1 PR #12 head 3ab8652 (branch `sync/nofx-9c106d0b-20260925`, base fedccc8e), nofx range 9c106d0b..6cac1b89.
- Method: blob-level comparison `nofx@6cac1b89` vs `vlauto@3ab8652`; full-tree absent accounting; RELEASE/GUIDE_BUILT_REV provenance; gitleaks + pattern grep; clean-clone build+vet. READ-ONLY — zero code changes, nothing pushed to vlauto.

## 1. Match table (task 1) — CONFIRMED

Every path in `git diff --name-only 9c106d0b 6cac1b89` (173 files) classified:

| class | count | disposition |
|---|---|---|
| blob-IDENTICAL | 171 | nofx@6cac1b89 blob == vlauto@3ab8652 blob [A] |
| `deploy/RELEASE` | 1 | PARTNER-SPECIFIC — partner stamp `66a0111c…` (partner build commit), never a nofx sha |
| `docs/superpowers/reports/2026-09-25-restart1-boot-watch.md` | 1 | EXCLUDED — named in the net-diff commit b25c239b's message: "Excluded (same rules as 4f4397a): internal report … (no code/test refs)" [A] |

- The net-diff commit b25c239b "boot2+#227 range 9c106d0b..6cac1b89 as one net-diff commit" touched exactly 170 files = 173 − RELEASE − boot-watch − brand-scope-baseline. The range is merge-heavy (no-ff merges #218 #224 #225 #206 #226 #223 + #227), so a net-diff instead of an am replay is the right method — no refutation.
- `web/src/test/brand-scope-baseline.json` and `web/src/brand-scope.test.ts` are both blob-IDENTICAL to nofx@6cac1b89 [A]; the baseline was applied by the partner's own re-pin commit 66a0111c rather than the net diff (named "Partner-specific, applied separately" in b25c239b) — the end state is identical, so CONFIRMED either way.
- `ninjascript/` is NOT in the range (0 paths) and all 8 files are blob-IDENTICAL between nofx@6cac1b89 and vlauto@3ab8652 [A] — the claim "AddOn needs no copy/F5" holds.

## 2. Absent files (task 2) — PARTIAL (accounting claim, zero code impact)

Full-tree: files present in nofx@6cac1b89 but absent in vlauto@3ab8652 = **1960**. Accounting closes exactly:

| bucket | count | rule |
|---|---|---|
| `.audit/**` | 10 | excluded by PR body rule ".audit/** excluded — nofx-lane audit notes" |
| `restart1-boot-watch.md` | 1 | excluded, named in b25c239b |
| removed after copy | 1632 | 8b36b1f2 "remove internal lane reports/audit notes" (1604) + 4f4397aa "drop the 8 standalone research-harness dirs" (28) — all 1632 still absent at tip (removed ∩ absent = 1632, nothing re-added) |
| never copied | 317 | 316 `docs/superpowers/**` + 1 `web/CLAUDE.md` |

- W5 deletions did NOT reappear: `trader/picture_htf_send.go`, `trader/picture_htf_send_test.go`, `trader/ninjatrader/market_entry_wire_test.go` all absent at 3ab8652 [A].
- **Zero non-doc files absent**: the entire 1960-set is docs (1948 `docs/**` at the previous-sync baseline, the new-range delta is exactly the 1 named exclusion). No .go/.cs/.ts code file is missing from the partner.
- **Finding PARTIAL (severity NOTE, tier [A])**: the PR #12 body's match table states "only in nofx | 2113 | … the other 2103 copied (docs 2044, …)". At head, only ~154 of those remain present (partner carries 165 `docs/superpowers/` files vs nofx's 2092); 1632 were copied then removed and **317 were never copied** (e.g. `web/CLAUDE.md`, and 316 docs/superpowers files). The removals are a defensible policy ("internal lane reports" do not belong on partner machines) but the table describes them as "copied", which the head tree refutes. Refute attempt: re-derived the table four ways (ls-tree comm, removed-commit union, delta vs previous sync at 4f4397aa = exactly 1, re-count) — all agree [A]. No trading/safety impact; the partner's docs are a deliberate subset.

## 3. RELEASE + GUIDE_BUILT_REV (task 3) — CONFIRMED, with one PARTIAL on the build procedure

- `deploy/RELEASE` @3ab8652 = `66a0111c6baf77b533b3bd7d56d71b6533b4793a` — the PARTNER's build commit (commit 3ab86527 "RELEASE stamp = the partner built commit 66a0111"). Resolves in vlauto, not in nofx [A]. CONFIRMED.
- `GUIDE_BUILT_REV`: at 3ab8652 `web/src/guide/types.ts` uses `resolveGuideBuiltRev()` — supplied AT BUILD TIME from `VITE_GUIDE_BUILT_REV` (40-hex validated), degrading to `'unknown'`/`'dev'`, never a hardcoded sha (types.ts:24-37) [A]. All guide content modules stamp `asBuiltRev: GUIDE_BUILT_REV`. The only hardcoded 40-hex literals under `web/src` are test fixtures: `954f11b1` in `brand-scope.test.ts:223` (partner-authority base pin, named in the PR body) and `662c79bd` in `guide/guide-built-rev.test.ts:25` (byte-identical to nofx@6cac1b89, shared test content). No nofx sha rides the runtime guide path. CONFIRMED.
- **PARTIAL (severity P2, tier [A]) — build-at-tip vs RELEASE**: the PR head 3ab8652 is the RELEASE-stamp commit, a CHILD of the build commit 66a0111c. A partner machine that follows runbook step 3 ("fetch the approved branch") and builds at the branch TIP produces a binary with `vcs.revision=3ab8652…` (I built exactly this — see §5) while `deploy/RELEASE` = 66a0111c. On boot, `kernel/boot_integrity.go:144-147` prefix-matches the binary's vcs.revision against RELEASE and REFUSES trading on mismatch ("binary is revision X but the intended release is Y — a stale binary is running"). Step 5 says "build at the exact approved commit" — that must name 66a0111c explicitly, not the branch tip. This is the same shape as nofx's marker commits, where the deploy lane builds at the release sha, but the partner runbook is the only thing between a machine and a refused boot. Refute attempt: verified vcs.revision of a tip build (3ab8652) and the refusal path (boot_integrity.go:144-147) [A].

## 4. Secret scan (task 4) — CONFIRMED clean

- `gitleaks detect --no-git` on the 3ab8652 tree: **22 findings in 18 files** — 19 generic-api-key, 1 private-key, 1 curl-auth-header, 1 curl-auth-user. **All 18 files are byte-IDENTICAL to nofx@6cac1b89** [A] — i.e. shared test fixtures (e.g. `main_dotenv_test.go`, `api/utils_test.go`) and doc examples; nothing partner-specific.
- Grep (`api[_-]?key|secret|token|passwd|password|BEGIN .*PRIVATE KEY`): 518 files contain pattern words; the only files NOT byte-identical to nofx are the 2 partner-only docs (`2026-09-22-vl-partner-verification.md`, runbook) + `deploy/RELEASE`. The verification doc's two matches are prose describing the scan's limits (lines 21, 23) [A]; RELEASE holds only the partner sha. No real secrets, no credentials, nothing new vs nofx.

## 5. Clean-clone build (task 5) — CONFIRMED

Fresh clone of vlautoagenttraderv1, detached at 3ab8652, `GOMAXPROCS=4 nice -n 19 ionice -c 3`:

- `go build -o nofx-bin .` — OK (rc 0). `go vet ./...` — clean (rc 0).
- `go version -m nofx-bin`: `vcs.revision=3ab865271b0141831767fe714e8f5f46ed345b1b`, `vcs.modified=false`, `vcs.time=2026-09-26T04:07:46Z`; md5 `e124e6a8e1450376028b9587a6289d6d`.
- Note: building at the tip yields vcs.revision=3ab8652, NOT the RELEASE-named 66a0111c — this is the §3 PARTIAL restated with the artifact in hand. No full suite run (DS-104 ran it; dispatch exempted me).

## 6. Claims ledger

| claim checked | verdict | evidence |
|---|---|---|
| 171/173 range files blob-identical | CONFIRMED | §1 table [A] |
| net-diff method appropriate (merge-heavy range) | CONFIRMED | b25c239b message; 170-file coverage closes [A] |
| boot-watch exclusion | CONFIRMED | named in b25c239b [A] |
| brand-scope baseline re-pin ends identical | CONFIRMED | blob compare [A] |
| W5 deletions gone, not reappeared | CONFIRMED | 3 files absent at head [A] |
| AddOn unchanged + identical | CONFIRMED | 0 range paths, 8/8 blobs [A] |
| RELEASE carries partner's own sha | CONFIRMED | 66a0111c resolves only in vlauto [A] |
| GUIDE_BUILT_REV carries no nofx sha at runtime | CONFIRMED | resolveGuideBuiltRev() + test-only literals [A] |
| PR body "the other 2103 copied" | **REFUTED (PARTIAL)** | 317 never copied + 1632 removed; final docs 165/2092 [A] |
| RELEASE/build commit vs branch tip (runbook hazard) | **PARTIAL (NEW finding, P2)** | boot_integrity.go:144-147 refusal on mismatch; tip build vcs.revision=3ab8652 [A] |
| secret scan clean | CONFIRMED | gitleaks 22 in 18 identical files; grep prose-only on partner-only files [A] |
| clean build + vet green, modified=false | CONFIRMED | §5 [A] |

Counts: CONFIRMED 9 · REFUTED/PARTIAL 1 (body accounting, NOTE) · NEW 1 (P2 runbook/build-tip hazard) · no P0/P1.

## 7. What I did NOT do

- No go test suite (exempted; DS-104's run stands), no -race (L16 slot protocol).
- No web build (no web source differs from nofx; nofx-side builds green at boot 3).
- No checklist edit (L17 read-only), no fixes, nothing merged, nothing pushed to vlauto.
- No live-box reads beyond git/github; DB untouched.

## DONE_WITH_CONCERNS

- Branch: `verify/0926-partner` @ HEAD (claim 88c0a201 + this addendum commit).
- Base: `git log -1` = 04ae1c2f.
- The partner sync is sound for all trading code: every code file matches byte-for-byte. The two findings are documentation/build-procedure accuracy, both recoverable and neither touching trading logic.
