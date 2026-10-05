# 2026-09-29 — AUDIT R2 — VL rename plan v2, slice: updater + activation readers (R1a item 1,3) + D0/D1 wording + folds F11–F13

- **Auditor:** DS-108 (round 2, rotated slice). Plan v2: `/home/hoang/rename-vl-plan/2026-09-29-rename-nofx-to-vl-plan-v2.md`, md5 `7b8dc9a9a196e5554203db7a3b71aa7b` (matches dispatch).
- **Round-1 baseline:** PR #274 (DS-101) read in full before this audit; its findings are NOT repeated here.
- **Branch:** `audit2/rename-readers` @ `70eb197c1e03d56f70410ee2a0543d5b5664b1be` · base `origin/dev` = `9d52f5dc611ed001fc057bca613685071d77efab` (RELEASE boot 7, 2026-09-28T01:01:07-05:00).
- **Mode:** L-AUDIT — read-only; only this report file written. No lock, no DB, no live-box action.

## Verdict

F11 and F12's listed sites verify EXACT against origin/dev [A]. F13 is **incomplete** (three
confirmed round-1 P2 findings were dropped from the fold) — P1. One NEW reader that BOTH rounds
missed: `deploy/bars-key-rollback.sh` — P1. D1 dispatch text still contradicts folds F9/F10/F12/F13
in four places — P2. No P0.

## NEW P0=0 P1=2

### P1-1 (NEW) — `deploy/bars-key-rollback.sh` is a missed reader (round 1 missed it too)
- **Where:** `deploy/bars-key-rollback.sh:55-56` `pgrep -f nofx-bin` refusal guard; `:61` `BARS_KEY_BACKUP_DIR:-$HOME/nofx-backups`; `:21` `NOFX_DB` [A, read this session].
- **Why it matters:** same failure class as the cgroup check: after R1b renames the binary, `pgrep -f nofx-bin` stops matching the running `vl-bin` and the REFUSED guard silently stops protecting — a bars-key rollback could run against a LIVE bot. `BARS_KEY_BACKUP_DIR` is also absent from F10's env list (only `NOFX_BACKUP_DIR` is there — a different variable).
- **Exact replacement text for the plan (append to F12 and R1a item 3):** "`deploy/bars-key-rollback.sh`: the liveness refusal becomes `pgrep -f 'vl-bin|nofx-bin'`; `BARS_KEY_BACKUP_DIR` default `${BARS_KEY_BACKUP_DIR:-$HOME/vl-backups:-$HOME/nofx-backups}` (add `BARS_KEY_BACKUP_DIR` to the F10 env list)."

### P1-2 (fold incomplete) — F13 dropped three confirmed round-1 P2s
- **Where:** v2 folds section F13 covers round-1 findings #1, #2, #5, #6, #7, #8 — but NOT #3, #4, #9, all in this slice and all confirmed this session [A]:
  - `internal/updaterworker/containment_census_test.go:27-28` walks `../../cmd/nofx-updater` → `t.Fatal` after the dir rename (loud break).
  - `cmd/nofx-updater/main_test.go:950-951` `go list -deps nofx/cmd/nofx-updater` — data string with BOTH the module path and the dir name (loud break).
  - `internal/updaterworker/harness_test.go:172,180,375,432` (+ neighbors) build fixtures with `nofx-bin`/`nofx_<date>.log` — after R1b they silently pin the retired name.
- **Why it matters:** the round-2 contract is that every confirmed round-1 finding is folded; DS-107 will hit these unplanned in D2 item 1 and either stop to ask (fine) or ship stale-name fixtures (silent drift).
- **Exact replacement text (append to F13):** "…; D2 item 1 additionally: `containment_census_test.go:27-28` walks `cmd/vl-updater`; `main_test.go:950-951` reads `vl/cmd/vl-updater` (derive or update with the rename); the `harness_test.go` fixtures switch to `vl-bin`/`vl_<date>.log` in the same commit."

## P2

### P2-1 (consistency) — the D1 dispatch text is sent as-written and contradicts the binding folds
- **Where:** v2 header claims "Every dispatch below is sent as written (updated with the audit folds)"; D1's JOB list was NOT updated:
  1. F12 (`backupRoot()` helper for the bot's own backup writers) has NO item in D1's JOB list at all — a lane building D1 from its dispatch text alone would never implement it.
  2. D1 item 2 still carries the short "at minimum NOFX_UPDATER, NOFX_RELEASE_DIR…" list (v2 line 158) — F9 (init-time readers) and F10 (complete list incl. NOFX_UNIT/NOFX_LOCK_DIR/BARS_KEY…/test-only names) are absent.
  3. D1 item 1 still names `bootcheck.go` as a log-glob site (v2 lines 51 and 153) — F13 says it has none.
  4. D1 item 1 does not include F11's dual readers (`leveltruth-cutover.sh`, `cutover.sh:69-70` staging).
- **Why it matters:** the folds say "binding; they override any dispatch text that disagrees" — but DS-103 receives only the D1 message. Either the D1 text must carry the folds inline, or the dispatch must say "the ROUND-1 FOLDS section of plan v2 is part of this dispatch".
- **Exact replacement text:** prepend to D1: "The plan v2 ROUND-1 AUDIT FOLDS section is PART OF THIS DISPATCH — implement F9–F13 exactly as written there; the JOB list below is a summary."

### P2-2 (fold nuance) — `vl-updater.service` EnvironmentFile path and `install-updater-worker.sh` ENV_FILE
- **Where:** `deploy/systemd-user/nofx-updater.service:21` `EnvironmentFile=%h/.config/nofx-updater/env`; `deploy/install-updater-worker.sh:48` `ENV_FILE="$HOME/.config/nofx-updater/env"` [A]. R2 step 2 moves `~/.config/nofx-updater` → vl names + symlink; R1b item 7 renames the unit but does not say the EnvironmentFile line moves.
- **Why it matters:** works either way through the symlink, but if DS-107 renames the path in the unit WITHOUT the symlink being present yet (unit ships in R1b, runs after R2 — fine) or leaves the old path and the symlink is later removed (R4 residue), the updater loses its env silently. Explicit is cheaper than forensic.
- **Exact replacement text:** D2 item 7 adds: "`vl-updater.service` EnvironmentFile becomes `%h/.config/vl-updater/env`; `install-updater-worker.sh` ENV_FILE likewise (the R2 symlink keeps the old path valid until then)."

## P3

- **P3-1 (consistency):** v2 line 51 (R1a) and 153 (D1) still cite `bootcheck.go` as a log-glob reader — F13 contradicts. Delete "bootcheck.go" from both lists (keep `activation/system.go:121`). [A]
- **P3-2 (fold wording):** F11 ends "or retires leveltruth-cutover.sh if obsolete — state which" — the plan still states neither. State it: the script is a live cutover path (journalctl + MainPID + binary moves); keep it and rename in R1b, or name the successor before the R2 boot. [A/B]
- **P3-3 (fold wording):** F12 lists "dayplan-level-repair" but `cmd/dayplan-level-repair/main.go` has NO `~/nofx-backups` writer — only the usage comment at `:9`. Either the real writer lives elsewhere (name it) or the entry is a comment and the list should say "comment-only; no writer". [A]

## Folds verified CORRECT this session [A]

- **F11:** `deploy/leveltruth-cutover.sh:8` `cd /home/hoang/nofx`, `:21,46` `journalctl -u nofx`, `:37` `MainPID nofx`, `:10,38,39` `nofx-bin` moves — exactly as folded. `deploy/cutover.sh` stages `$STAGE_DIR/nofx-bin` (cp + md5, lines ~69-71) — as folded (20 nofx refs total in the file; the rest are env names in F10 and UNIT/INSTALL defaults in R1b item 7).
- **F12:** `store/adherence_regrade.go:125`, `store/ab_confirm.go:448`, `store/wave_a_migration.go:53`, `store/bar_contract_key.go:225`, `cmd/nofx-activate/main.go:83`, `cmd/nofx-updater/main.go:162` (fold cites 158,164 — the `BackupRoot` Join is at 162) — all exact. `internal/updaterworker/worker.go:49` is a comment, not a writer; the updater's backup root flows from `main.go:162` ✓.
- **F13 (the parts that exist):** `releasefixture/releasefixture.go:164` (`"nofx-bin": "\x7fELF…"`) + `:175` (chmod 0o755) exact; `bootcheck_parity_test.go:48` `…+ "nofx/main.go:322" + rest[sp:]` exact; the production `bootShape` at `internal/updaterworker/bootcheck.go:50` is `\S*/main\.go:\d+` — matches ANY builddir, so production survives the rename (only the test pin needed deriving, as folded).
- **D0:** still valid — #267 is OPEN (not merged) and `all accounts flat` is absent from `trader/installation_gate.go` at dev `9d52f5dc6` (the defect lives on the #267 branch, as round 1 verified at `21a8b6cd3`).

## New-reader sweep (deeper than round 1) — negatives worth recording [A]

- No `pgrep`/`/proc/*/comm`/argv0 parsing in the slice (only comments; round 1 [A]).
- No Go reader of the lock DIR (`~/nofx-main.lock.d`) — path lives only in the shell scripts ✓.
- No Go reader of `~/.config/nofx-updater/env` (path lives in the unit + install script, P2-2) ✓.
- No `nofx-inbox`/`nofx-releases` Go readers; `internal/updaterjob` has zero non-import nofx strings ✓.
- `bootcheck.go:45-46` matchers are name-free prefixes ("BOOT INTEGRITY OK — rev ") ✓.

## What this audit did NOT do
- No code change; only this report. `go build ./...` RC=0, `go vet ./...` RC=0 at this head.
- Race NOT run — the single L16 slot was held by another lane at audit time (disclosed).
- No DB read, no deploy, no lock, no live-box action.

## Evidence base
`git log -1 --format='%H %cI' origin/dev` = `9d52f5dc611ed001fc057bca613685071d77efab 2026-09-28T01:01:07-05:00`; plan v2 md5 `7b8dc9a9a196e5554203db7a3b71aa7b`.
