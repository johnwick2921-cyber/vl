# AUDIT2-RENAME-MIGRATE — round 2: migration script (R2/D3) + folds F1–F8

- **Auditor:** DS-101 (Copilot lane), 2026-09-30, ~01:22–01:50Z. Round-2 dispatch (owner: "audit until perfect"), slice: **MIGRATION SCRIPT (R2/D3) + folds F1–F8**; round-1 sources read in full: draft PR #268 (`audit/rename-migrate`, DS-104) and the ops parts of draft PR #272 (`audit/rename-ops`, DS-106).
- **Plan audited:** `/home/hoang/rename-vl-plan/2026-09-29-rename-nofx-to-vl-plan-v2.md` — `md5sum` = `7b8dc9a9a196e5554203db7a3b71aa7b` (dispatch's expected prefix `7b8dc9a9…` ✓), 246 lines, read in full.
- **Base:** `origin/dev` @ `9d52f5dc6` — `git log -1` = `9d52f5dc6 RELEASE 4d5382069643a491403ac0a8575f2bbea3648676 — boot 7: #266 … booted 01:00:43 CT 2026-09-28`.
- **Disposition:** DONE_WITH_CONCERNS — **NEW P0=0 P1=1**. F1/F2/F4/F5 are correct and essentially complete; F3/F6/F7/F8 each have completeness gaps, and the R2/D3 phase+dispatch text below the folds is still the v1 text that contradicts six of the eight folds (the binding clause exists but a lane that copies D3 "as written" builds the v1 script).

Evidence tiers on every claim: [A] read the exact line this session · [B] inferred · [C] speculation.

---

## CHECK (a) — are folds F1–F8 CORRECT and COMPLETE?

### F1 — CORRECT [A]; two completeness gaps
- **Correct:** `internal/updaterworker/releaseroot.go:46-55` is exactly as cited: `EvalSymlinks(root)`, `realRoot != root → "… is not its own resolved path … name the real directory, with no symlink in it"`, then `PathWithin(realRoot, realInstall)` refuses inside the install. A symlinked `~/nofx-releases` would break every fetch [A]. Moving to a REAL `~/vl-releases` + rewriting the env file to the real path is the right fix.
- **Gap 1 (P3):** the env file holds MORE than `NOFX_RELEASE_DIR` — `~/.config/nofx-updater/env` feeds `EnvironmentFile=%h/.config/nofx-updater/env` [A: `deploy/systemd-user/nofx-updater.service`], and `NOFX_RELEASE_INBOX` is read from it (`cmd/nofx-updater/main.go:77` const, `:209`). F1 names only `VL_RELEASE_DIR`. Fix text: "REWRITES the updater env file so every path-carrying var points at the real vl path (at minimum `VL_RELEASE_DIR=/home/hoang/vl-releases` AND `VL_RELEASE_INBOX=/home/hoang/vl-inbox`)".
- **Gap 2 (P3):** `--release-dir` given at step 0 may live under `~/nofx-releases/<sha>`; after step 2 that dir has moved. The script must re-resolve `--release-dir` through the new real path before the guard re-check. Fix: add to D3 step 2: "re-resolve `--release-dir` to its post-move real path and re-run the outside-the-install guard against it".

### F2 — CORRECT and complete [A]
`trader/ninjatrader/transport.go:28` "the TCP server is a process-singleton: one bot → one listener on 127.0.0.1:36974" and `VLTraderTCPClient.cs:38` confirm 36974 cannot be shared; 8080 is the API. Never running both is mandatory; the verify-before-disable order removes the round-1 P1-2 box-down window. ✓

### F3 — correct direction, INCOMPLETE (one P2, two P3)
- **P2-F3a:** F8 renames `~/bin/nofx-updater` → `.old` at R2; F3's rollback says "re-enable nofx + the old user units" but does NOT restore the updater binary. After a rollback the re-enabled `nofx-updater` unit has `ExecStart=%h/bin/nofx-updater` [A] pointing at a renamed-away file → the updater unit fails on every start, and the next button update is unwatched. Fix: add to F3: "restore `~/bin/nofx-updater` from its `.old` (and the env file), then re-enable `nofx-updater`".
- **P3-F3b:** "restore both env files" — which two? Name them: `~/.config/vl-updater/env → ~/.config/nofx-updater/env` and (only if the owner ran the optional sed) the `.env`.
- **P3-F3c:** after `rm /etc/systemd/system/vl.service` the rollback must run `systemctl daemon-reload` (F3 says remove + disable, not the reload).

### F4 — CORRECT [A]; one wording anchor
`internal/updaterworker/app_http.go:172` requires key `"nt8_absent"` in the ready payload — the NT8-closed state is a real, first-class drain path (#266), so requiring an NT8 hello in the verify would auto-rollback a good boot. F4's rule is right. Wording gap (P3): the round-1 fix said "match `postboot-check.sh`'s framing" — but `deploy/postboot-check.sh` has NO NT8/hello line at all [A: grep]. Anchor the leg where the state actually lives: "NT8 hello line **or** the ready payload's `nt8_absent` leg (app_http.go:172)". Also fold the rest of round-1 P3-5: the 90 s auto-rollback fires on BOOT INTEGRITY + health-revision legs only, and is waived only by the owner's explicit word at the boot (NO UNATTENDED DEPLOYS).

### F5 — CORRECT and complete [A]
`deploy/nofx-web.service` exists in the repo with the vite-dev shape; DS-106 proved the live unit is active/enabled (`nofx-web.service`, WorkingDirectory `/home/hoang/nofx/web`, listener on :3000). Stop/disable + install `vl-web.service` + verify :3000 is exactly right, and F3 covers the rollback half. ✓

### F6 — correct items, one DANGEROUS word (P2), one omission (P3)
- **P2-F6a:** "skip-with-log for any dir that does not exist" conflicts with round-1 #268 P3-1, which is NOT folded. Applied to `~/nofx` itself, a missing main dir gets skipped and the migration proceeds over nothing. Fix: "skip-with-log for any OPTIONAL dir that does not exist; `~/nofx` missing/not-a-dir is a hard refusal, `~/vl` existing — even as a symlink — is a hard refusal, and the old lock held by a DIFFERENT session is a hard refusal quoting `nofx-lock.sh status`".
- **P3-F6b:** the new `vl-updater` unit needs BOTH `ExecStart=%h/bin/vl-updater --install-dir %h/vl serve` AND `EnvironmentFile=%h/.config/vl-updater/env` — F6 names only `--install-dir`.

### F7 — correct direction [A], INCOMPLETE (one P2) + one contradiction (P2)
- The old lock's `meta` is `field=value` lines with a session field (`_meta`/`_field` in `deploy/nofx-lock.sh:75-76`), so the same-session exemption is implementable: `vl-lock.sh acquire` reads the OLD dir's session and allows the same name. Direction correct.
- **P2-F7a:** acquiring the vl lock then RELEASING it "to prove" (the v1 step-5 shape F7 doesn't replace) leaves the main tree UNLOCKED for the rest of the boot (step 6 verify, postboot checks). Fix: "step 5: `vl-lock.sh acquire <session>` (same-session exemption) → release the OLD lock → KEEP the vl lock until the script exits (trap release), proving `~/vl-main.lock.d` with `status`".
- **P2-F7b (contradiction):** the Verification section still says "let the new lock script run while the old lock dir exists (must refuse)" — under F7, same-session must NOT refuse. Fix: "…while the old lock dir is held by a DIFFERENT session (must refuse); held by the SAME session (must proceed)".

### F8 — correct, two gaps
- **P3-F8a:** the old unit FILE `~/.config/systemd/user/nofx-updater.service` stays after disable (needed for rollback) — state it: "the old unit file stays disabled for one release (the rollback needs it); remove it in the release after the first post-rename release".
- **P3-F8b:** F8's rename-to-.old must happen INSIDE `migrate-to-vl.sh` (D3 step 4b), not as a side note — and F3's restore (P2-F3a) is its rollback counterpart.

---

## CHECK (b) — NEW findings beyond round 1

- **N1 P2 = P2-F6a** (above): the skip-with-log wording can skip the main dir; round-1 P3-1 unfilled.
- **N2 P2 = P2-F7a** (above): unlocked window after the hand-over.
- **N3 P2 = P2-F3a** (above): rollback restores a unit whose binary was renamed away — the safety net is broken by the F3/F8 combination.
- **N4 P3:** the updater worker's backup root (`cmd/nofx-updater/main.go:158,164` → `~/nofx-backups/updater`) is covered cross-slice by F12's `backupRoot()` helper, but the MIGRATION must ensure `~/vl-backups/updater` exists (the mv creates `~/vl-backups`; the subdir must exist or the first vl-updater backup fails) — one line in D3 step 2: "mkdir -p ~/vl-backups/updater".
- **N5 P3:** `~/nofx-release-key.pub` and `~/nofx-release-*` clones (round-1 #268 P3-3, not folded): add the out-of-scope line to D3 so a lane doesn't try to migrate build caches.
- **N6 P3:** gate-jwt naming (round-1 #268 P3-6, not folded): D3 step 0 must name it — "read `VL_CUTOVER_TOKEN` (R1a fallback `NOFX_CUTOVER_TOKEN`) from a file, sent exactly as `cutover.sh:127-134` sends it, never logged".
- **N7 P3:** `deploy/nofx-clock-guard.sh:25` hardcodes `STATE="${NOFX_CLOCK_STATE:-/home/hoang/nofx/data/clock-guard-state.json}"` — the default path carries BOTH the old name and an absolute home. R1b item 7 renames the script but no fold says to change this default. It works through the symlink today and breaks the day the symlink retires. Fix: D2/R1b item 7 add "vl-clock-guard.sh STATE default → `$HOME/vl/data/clock-guard-state.json`".

---

## CHECK (c) — CONSISTENCY: the R2/D3 text still contradicts the folds

The folds clause says "BINDING; they override any phase/dispatch text below that disagrees" — but D3 is to be "sent as written (updated with the audit folds)" and the text below the folds is byte-for-byte the v1 text. A lane that builds from the D3 text alone builds the v1 script. This is the P1. Quoted contradictions and exact replacements:

1. **F1 vs R2/D3 step 2.** v2 text: "same for `~/nofx-releases`, `~/nofx-inbox`, `~/.config/nofx-updater` → `vl` names + symlinks; then verify the release-dir 'outside the install' guard still holds with the symlinks in place". Replacement: "`mv ~/nofx-releases ~/vl-releases` and leave `~/nofx-releases → ~/vl-releases` **for humans only**; same pattern for `~/nofx-inbox` and `~/.config/nofx-updater`; REWRITE `~/.config/vl-updater/env` so `VL_RELEASE_DIR` and `VL_RELEASE_INBOX` name the real paths; re-resolve `--release-dir` and re-run the outside-the-install guard against the REAL path".
2. **F2 vs R2/D3 step 3.** v2 text: "… `systemctl disable nofx`, `enable --now vl`" (park `nofx-bin` first, disable before verify). Replacement: "(a) install `vl-bin` into `~/vl`; (b) install `/etc/systemd/system/vl.service`; (c) `enable --now vl`; (d) verify (step 6); (e) only then `mv nofx-bin nofx-bin.old.<oldsha>` and `systemctl disable nofx`. At every prefix the box is runnable."
3. **F3 vs R2/D3 step 7.** v2 rollback omits: `rm /etc/systemd/system/vl.service` + `systemctl daemon-reload`, removal of vl user units + `~/bin/vl-updater`, restoring `~/bin/nofx-updater` from `.old`, restoring the updater env file, re-enabling `nofx-web`. Replacement: "stop vl; rm the symlinks; mv back; restore `nofx-bin`, `~/bin/nofx-updater` and `~/.config/nofx-updater/env`; disable+remove `vl.service`, `vl-web.service`, the vl user units and `~/bin/vl-updater`; `daemon-reload` (system + user); re-enable `nofx`, `nofx-web`, `nofx-backup`, `nofx-clock-guard`, `nofx-updater`; start; verify the old boot line. Accepted residue, stated: `vl_*` logs written, `~/vl-backups` content, verdicts carrying `~/vl` paths (re-fetched on the next install)."
4. **F4 vs R2/D3 step 6.** v2 text: "NT8 hello line" (required). Replacement: "NT8 hello line **or** the ready payload's `nt8_absent` leg (app_http.go:172); the 90 s auto-rollback fires on BOOT INTEGRITY + health-revision legs only, and is waived only by the owner's explicit word at the boot".
5. **F5 vs R2/D3 steps.** v2 text never mentions `nofx-web`. Add step 3b: "stop and disable `nofx-web`; install `/etc/systemd/system/vl-web.service` from `deploy/vl-web.service` (placeholders filled like `install-autostart.sh`); `enable --now vl-web`; verify :3000."
6. **F7 vs R2/D3 step 5.** v2 text: "release the OLD lock (old script), acquire+release once with the new script to prove `~/vl-main.lock.d`". Replacement per P2-F7a: "`vl-lock.sh acquire <session>` (same-session exemption over the old dir) → release the OLD lock with the old script → KEEP the vl lock until the script exits (trap release); prove with `status`".
7. **F8 vs R2/D3 step 4.** v2 text: "disable `nofx-updater`". Replacement: "disable `nofx-updater`, `mv ~/bin/nofx-updater ~/bin/nofx-updater.old.<sha>`; the old unit file stays disabled for one release (rollback needs it); then install + enable `vl-updater` (`--install-dir %h/vl`, `EnvironmentFile=%h/.config/vl-updater/env`)".
8. **F6 vs R2/D3 step 0.** v2 precondition list omits: `sudo -v` once up front (+ `sudo -k` at exit), `cd` out of `~/nofx` before the `mv`, hard refusals for `~/nofx` missing / `~/vl` existing / foreign lock holder, `mkdir -p ~/vl-backups/updater`, `git worktree list | wc -l` before/after (294 today). Add them.
9. **Verification section vs F7.** "let the new lock script run while the old lock dir exists (must refuse)" → per P2-F7b.

---

## Findings ranked (NEW P0=0 P1=1)

| # | Sev | Where | Why | Exact fix |
|---|---|---|---|---|
| 1 | P1 | R2/D3 text below the folds = v1 text | a lane building from D3 "as written" builds the v1 script (wrong order, symlinked releases, locked hand-over, no web unit) | replace the R2/D3 text with the eight replacements in CHECK (c) |
| 2 | P2 | F6 "skip-with-log for any dir" | a missing `~/nofx` gets skipped, migration proceeds over nothing | scope the skip to optional dirs; hard-refuse `~/nofx` missing, `~/vl` existing, foreign holder |
| 3 | P2 | F7 leaves the tree unlocked after prove-and-release | another lane can take the main tree mid-boot | keep the vl lock until script exit (trap release) |
| 4 | P2 | F3 + F8: rollback re-enables `nofx-updater` whose binary was renamed `.old` | the safety net restores a broken updater | F3 add "restore `~/bin/nofx-updater` from `.old`" |
| 5 | P2 | Verification mutant text vs F7 | "must refuse while old lock dir exists" contradicts the same-session exemption | reword per P2-F7b |
| 6 | P3 | F1 rewrites only `VL_RELEASE_DIR` | inbox env + `--release-dir` re-resolution left dangling | add `VL_RELEASE_INBOX` + re-resolve line |
| 7 | P3 | F3 "restore both env files" unnamed; missing `daemon-reload` | ambiguous rollback | name the files; add the reloads |
| 8 | P3 | F4 anchor "postboot-check.sh framing" | that script has no NT8 line | anchor to `nt8_absent` ready-payload leg |
| 9 | P3 | F6 names only `--install-dir` for the vl-updater unit | env file path unchanged | add `EnvironmentFile=%h/.config/vl-updater/env` |
| 10 | P3 | `~/vl-backups/updater` subdir | first vl-updater backup fails if absent | `mkdir -p` in step 2 |
| 11 | P3 | release clones + `~/nofx-release-key.pub` unmentioned | lane may try to migrate build caches | out-of-scope line (round-1 P3-3) |
| 12 | P3 | gate-jwt file unnamed | lane invents a convention | `VL_CUTOVER_TOKEN` file + cutover.sh header shape, never logged |
| 13 | P3 | `nofx-clock-guard.sh:25` STATE default `/home/hoang/nofx/data/…` | breaks at symlink retirement | R1b item 7: default → `$HOME/vl/data/clock-guard-state.json` |
| 14 | P3 | F8 old unit file fate unnamed | lingering name | "stays disabled one release, then removed" |

## What I did NOT do (L-AUDIT)
No code change, no worktree edit outside this report, no deploy, no stop/start, no DB write, no lock acquire. Read-only: `git fetch`, `git show`, grep/sed/cat at `origin/dev` in the audit worktree. No race run (nothing built).
