# AUDIT2-RENAME-MODULE — round-2 audit: module rewrite + census guard + goldens (R1b 1/7/10 + F14/F15/F23/F29)

- **Lane:** DS-104. **Plan audited:** `/home/hoang/rename-vl-plan/2026-09-29-rename-nofx-to-vl-plan-v2.md`
  `md5sum` = `7b8dc9a9a196e5554203db7a3b71aa7b` ✓ (dispatch's `7b8dc9a9…` — not stale)
- **Base:** `origin/dev = 9d52f5dc6` — `git log -1` = `9d52f5dc6 RELEASE 4d5382069 … boot 7 (#266 + #265) booted 01:00:43 CT 2026-09-28`
- **Round-1 slice report read first:** PR #273 (DS-102) — its P1/P2s map 1:1 onto the folds in my slice (F13/F14/F15/F23/F29).
- **Method:** READ-ONLY greps/reads at the base. No file outside this report was written, no unit touched.

## Fold verification (a)

### F14 — CORRECT but not propagated → P1
- [A] `internal/updateauth/admin.go:62` `const passwordBindingDomain = "nofx-updater/password-binding/v1\x00"` — verified byte-for-byte, and the comment explains it is the MAC-domain separator for stored bindings. Renaming it invalidates every enrolled password binding (owner re-enrollment). The fold is right.
- **Gap:** the operative D2 text (item 10's allow-list) does **not** list this string. The fold header says folds override phase/dispatch text "that disagrees" — but an allow-list is additive, so D2 as written simply produces a guard that FAILS on `admin.go:62` after the rename. **Exact fix:** in D2 item 10's bucket list, add: `internal/updateauth/admin.go` passwordBindingDomain string (stored-data compatibility surface, comment required) — in F14's own words.

### F15 — CORRECT but INCOMPLETE → P1
- [A] `internal/updaterjob/testdata/job_full.golden.json` carries **17** nofx hits ✓ (as cited).
- **Gap 1:** `internal/updaterjob/golden_test.go:50-53` builds the golden INPUT with hard-coded `/home/u/nofx-releases/…/nofx-bin`, `/home/u/nofx/nofx-bin`, `/home/u/nofx/web/dist` — if only the `.json` is regenerated and the test's constructor is left nofx-pathed, the comparison fails (vl-pathed golden vs nofx-pathed input). The fold names only the `.json`.
- **Gap 2:** D2 (the operative dispatch) has **no golden step at all** — the fold was never written into it.
- **Exact fix:** extend F15: "regenerate `job_full.golden.json` AND rename the synthetic paths in `golden_test.go:50-53` in the same commit; diff the regen"; add the step as D2 item 10½ or fold it into item 1.

### F23 — RIGHT DIRECTION, PARTIAL, and not propagated → P1
- The fold correctly rejects a raw grep count, but it names only **two** buckets (synthetic `module nofx` fixtures, `.audit/*.md`). Round 1's full bucket set that the guard actually needs is not carried: provider/nofxos + NofxOS labels; broker ids; upstream NoFxAiOS links/users; the R1a fallbacks/wrappers; dated history docs; the Binance referral code; the F14 domain; `releasefixture.go`'s embedded `nofx-bin`; `/srv/nofx`; the three testdata `_provenance` hits; `verdict_test.go:29` allowedImports map.
- **And D2 item 10 is still the v1 text** ("`grep -rIi nofx` over the tree must equal an explicit allow-list: …") — the lane will implement the superseded guard.
- **Exact fix:** rewrite D2 item 10 to: "token/pattern guard: forbidden patterns = `"nofx/…` import, `module nofx`, `nofx-bin`, `nofx.service`, `NOFX_*`, `nofx_*.log`, `nofx-updater`, `X-NOFX-Update`, `nofxAI`, `nofx-go`; a `file:pattern` allow-list covers: [the full bucket list above, enumerated]".

### F29 — CORRECT for what it names, INCOMPLETE → P2
- [A] verified present: `docker/Dockerfile.backend:52` `-o nofx`; `deploy/release/manifest.sh`; `install-autostart.sh` / `install-clock-guard.sh` unit names; `strategy_management.json` nofxos key.
- **Missed by F29** (each verified [A]): `deploy/install-journald.sh:22` + `deploy/journald-nofx.conf` (2 hits); `deploy/install-db-backup.sh:12-13` (installs `nofx-backup.{service,timer}`); `deploy/bars-key-rollback.sh:21,55-56` (see P1-1); `.dockerignore:19-20`; `.gitignore:11,13,14`; `Dockerfile.railway:5,8,16`; `.github/workflows/pr-checks.yml:214`; `.github/workflows/pr-docker-check.yml:55,73,92,130,146`.
- **Exact fix:** extend F29's list with those files, split by action: RENAME (gitignore/dockerignore entries, COPY paths in Dockerfile.railway, `-o nofx`/image tags in the two workflows, journald unit+conf, db-backup installer) vs KEEP+allow-list (Dockerfile.railway's `FROM ghcr.io/nofxaios/nofx/*` — old-org images, third-party like upstream links; `.github/CODEOWNERS:24` upstream handles).

## NEW findings (b) — round 1 missed these

### NEW P1-1 — `deploy/bars-key-rollback.sh` `pgrep -f nofx-bin` is a READER R1a does not dual-ize
- [A] `deploy/bars-key-rollback.sh:55-56`: `pgrep -f nofx-bin` refuses (unless `--force`) while a bot binary is running. After R2's binary rename this pattern stops matching the live `vl-bin` → the refusal silently vanishes and a bars-key rollback runs against a LIVE bot, or the operator passes `--force` believing nothing is running.
- **Why round 1 missed it:** D-AUDIT's reader list focused on updater/activation/cutover scripts; this rollback script is outside that cluster.
- **Exact fix:** R1a item 3 add: "`deploy/bars-key-rollback.sh` pgrep matches `vl-bin\|nofx-bin` (and its `NOFX_DB` env read routes through the helper / `${VL_DB:-${NOFX_DB:-…}}`); R1b renames it in F29's list."

### NEW P1-2 — D2 item 10's v1 wording would make the census guard unpassable AND let the module rewrite land with no guard teeth
- The guard is the only mechanical proof that 25,767 mentions shrank to the allow-list. As written (raw grep + v1 allow-list) it cannot pass (round 1 P2-2 proved this — hence F23), and it omits F14's domain. A lane that reads only its D2 text builds the superseded guard.
- **Exact fix:** the F23 rewrite above, plus a line in D2's gates: "the guard's own `file:pattern` table is reviewed by the CTO at gate time".

### NEW P2-1 — `.gitignore` / `.dockerignore` entries not renamed
- [A] `.gitignore:11,13,14` (`nofx-auto`, `nofx`, `nofx_test`), `.dockerignore:19-20` (`nofx`, `nofx_test`). After the rename the new `vl-bin`/`vl` artifacts are UNIGNORED: git status noise and a real chance of committing the binary again (the 2026-08-29 binary purge is the precedent), and the docker build context balloons.
- **Exact fix:** D2 item 1 file list add: ".gitignore + .dockerignore entries → vl names, same commit as the binary-name change."

### NEW P2-2 — `Dockerfile.railway` COPY paths
- [A] lines 5/8 (`FROM ghcr.io/nofxaios/nofx/nofx-{backend,frontend}:latest`) are old-org images → KEEP + allow-list; line 16 `COPY --from=backend /app/nofx /app/nofx` is OUR binary → rename to `/app/vl`.
- **Exact fix:** add to D2 item 1 (rename the COPY pair) and to F19 (keep the FROM images, bucket as third-party).

### NEW P3-1 — CI workflow leftovers
- [A] `.github/workflows/pr-checks.yml:214` `go build -v -o nofx`; `.github/workflows/pr-docker-check.yml:55,73,92,130,146` image tags `nofx-*`. Mechanical; fold into F19's list.

### NEW P3-2 — journald + db-backup installers
- [A] `deploy/install-journald.sh:22` installs `journald-nofx.conf`→`nofx.conf`; `deploy/journald-nofx.conf` has 2 hits; `deploy/install-db-backup.sh:12-13` installs `nofx-backup.*`. Fold into F29 (rename) — item 7 renames the unit files, so the installers must be named or the box installs dead unit names.

## Consistency (c)

- D2 items 1/10 and the Phase R1b items 1/7 still read as v1 (verified above, quoted lines). The binding-fold header cannot substitute for the lane's operative text. **Exact fix:** regenerate the D2 text from the folds before dispatch (the round-1 loop's stated purpose), at minimum for items 1, 7, 10.
- One adjacent inconsistency spotted (outside my slice, flagging only): D2 item 3 "both present → 403" predates F22's value-count rule; and D2 item 7's lock-refusal predates F7's same-session exemption. Pass to their slice owners.

## DONE line

**NEW P0=0 P1=2 (P1-1 bars-key pgrep reader, P1-2 D2 item-10 contradiction/omission), plus 3 fold-completeness P1s under (a): F14-unpropagated, F15-incomplete, F23-partial. P2=2 new (+F29 P2), P3=2 new.**

## L14

- Branch `audit2/rename-module` (claim `66c26a727`), HEAD after the report commit below; base `git log -1` = `9d52f5dc6`.
- Files touched: this report only. No code, no units, no DB, no deploy, no lock.
