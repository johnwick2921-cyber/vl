# AUDIT-RENAME-MIGRATE — D3 (migration script design) adversarial audit

- **Lane:** DS-104 (future owner of `deploy/migrate-to-vl.sh`)
- **Plan audited:** `/home/hoang/rename-vl-plan/2026-09-29-rename-nofx-to-vl-plan.md`
  `md5sum` = `b16759336fe0e264d11c1bc885ce5192` ✓ (matches the dispatch's `b1675933` — not stale)
- **Base:** `origin/dev = 9d52f5dc6` — `git log -1` = `9d52f5dc6 RELEASE 4d5382069 … boot 7 (#266 updater-NT8-closed + #265 release-outside-tree) booted 01:00:43 CT 2026-09-28`
- **Method:** READ-ONLY. Real-box walk: `systemctl cat nofx`, user units, `ls ~`, worktree metadata, a scratch-tree proof of the mv+symlink behavior. No unit was stopped/started, no file outside the report was written.
- **Verdict:** the D3 design is fundamentally sound (symlinks cover git metadata — proven below), but it has **3 P1s that would produce a box-down or a false auto-rollback**, plus wording gaps. Fixes are one-line to one-paragraph edits to the D3 text.

## Real-box facts the D3 text must build on ([A] = read today)

- System unit `nofx.service` bakes: `User=hoang` · `WorkingDirectory=/home/hoang/nofx` · `ExecStart=/home/hoang/nofx/nofx-bin` [A].
- User units: `nofx-backup.service` `ExecStart=/home/hoang/nofx/deploy/nofx-db-backup.sh`; `nofx-clock-guard.service` `ExecStart=/home/hoang/nofx/deploy/nofx-clock-guard.sh`; `nofx-updater.service` `ExecStart=%h/bin/nofx-updater --install-dir %h/nofx serve` (enabled; timers `nofx-backup.timer`, `nofx-clock-guard.timer` enabled) [A].
- All four step-2 dirs exist: `~/nofx-backups`, `~/nofx-releases`, `~/nofx-inbox`, `~/.config/nofx-updater` [A].
- `~/bin/nofx-updater` exists; `~/.config/nofx-updater/env` exists (values redacted; they are the NOFX_* env the R1a fallback covers) [A].
- Release artifacts: `~/nofx-release-key.pub` (file) + `~/nofx-release-1fa5c3f1` (clean-clone dir) [A].
- **294 git worktrees** on the main tree (`git -C ~/nofx worktree list | wc -l` = 294); each `<wt>/.git` holds `gitdir: /home/hoang/nofx/.git/worktrees/<name>` [A].
- `crontab -l` has no nofx entry [A]. `/api/installation-gate` exists (`api/handler_maintenance.go:29`, GET) [A]. `deploy/cutover.sh` uses `NOFX_CUTOVER_TOKEN` env, never argv [A] — D3's "gate-jwt from a file, never argv" matches that convention; name it exactly.

## P0

None found.

## P1 — findings that would break the boot or the rollback as written

### P1-1 — NT8-hello as a step-6 verify criterion auto-rollbacks a GOOD boot when NT8 is closed
- **Where:** D3 step 6 "verify within 90 s: … NT8 hello line …; on failure → automatic `--rollback`."
- **Why:** boot 7 (#266, merged in the audited base) makes **NT8-closed an explicitly supported install state** (nt8_absent drain path, `db_open_positions` + `sim_accounts` legs). With NT8 closed there is no hello line ever — the verify fails and the script rolls back a healthy rename.
- **Fix (exact):** step 6 verify = "`BOOT INTEGRITY OK` for the expected sha in `~/vl/data/vl_<date>.log`; NT8 hello line **OR** the nt8_absent drain marker (match `postboot-check.sh`'s framing)"; the auto-rollback fires on the BOOT INTEGRITY / health-revision leg, never on NT8 state.

### P1-2 — step-3 ordering has a box-down window
- **Where:** D3 step 3 "install `vl-bin` into ~/vl (**keep `nofx-bin` as `nofx-bin.old.<oldsha>`**), … `systemctl disable nofx`, `systemctl enable --now vl`."
- **Why:** the old unit's `ExecStart=/home/hoang/nofx/nofx-bin` [A] stays runnable only while `nofx-bin` exists. If the binary is parked as `.old` and `enable --now vl` then fails (bad unit file, daemon-reload hiccup), **neither unit can start** and the box is down mid-window. The script must never rename the only runnable binary before the successor proves itself.
- **Fix (exact):** reorder step 3: (a) install `vl-bin` into `~/vl`; (b) write `/etc/systemd/system/vl.service`; (c) `systemctl enable --now vl`; (d) verify the vl boot line (step 6); (e) only then `mv nofx-bin nofx-bin.old.<oldsha>` and `systemctl disable nofx`. At every prefix of this order the box is runnable (old or new).

### P1-3 — `--rollback` does not undo everything; updater state written with `~/vl` paths survives the rollback
- **Where:** D3 `--rollback`: "stop vl, rm the symlinks, mv ~/vl ~/nofx … restore nofx-bin, re-enable nofx + the old user units, start, verify the old boot line."
- **Why:** between the new boot and a rollback, the updater writes jobs/verdicts that carry **absolute `~/vl` paths** (the plan itself says "the first post-rename release is re-fetched so new verdicts carry ~/vl" — that works forward, but after `rm` of the `~/nofx` symlink those paths dangle, and the plan states no rollback counterpart). Also un-done by the rollback as written: `/etc/systemd/system/vl.service` stays installed (removed only if we say so), the installed vl user-unit files + `~/bin/vl-updater` stay, `~/vl-backups` entries stay, `vl_*` logs stay (fine, logs are append-only history — say so).
- **Fix (exact):** add to `--rollback`: (1) `systemctl disable --now vl` AND `rm /etc/systemd/system/vl.service` + `daemon-reload`; (2) disable + remove the vl user-unit files and `~/bin/vl-updater`; (3) clear/re-fetch `~/nofx/data/updater/jobs` whose content carries `~/vl` (or state that R1a makes the updater re-resolve `~/vl` → `~/nofx` when the symlink is absent — either way the dispatch must SAY which); (4) list the explicitly-not-undone items (`vl_*` logs already written, `~/vl-backups` content, old-style verdicts) as accepted residue.

## P2

### P2-1 — "ONE sudo inside" is not the real shape; a mid-run password prompt hangs a stopped bot
- **Why:** root-needing commands are: `systemctl stop/disable/enable nofx`, writing `/etc/systemd/system/vl.service`, `systemctl daemon-reload`, `enable --now vl` — several invocations, not one. User-unit operations need **no** sudo (`systemctl --user`). If the sudo timestamp expires mid-run (default 15 min), the script blocks on a password **after the bot is stopped**.
- **Fix (exact):** D3 text: "the script runs as the bot's user; it calls `sudo -v` before the first root command and refreshes before each subsequent one (or runs the root block as one `sudo bash -c`), and `sudo -k` on exit. User-unit commands use `systemctl --user` (no sudo). WSL note: systemd-in-WSL behaves normally here (verified: `systemctl cat nofx` works on this box); mirrored networking is irrelevant to this script — state that explicitly so a future reader stops looking."

### P2-2 — step 5's lock hand-over has an unavoidable unheld gap; it is only safe because of D-FREEZE
- **Why:** the new lock script REFUSES while `~/nofx-main.lock.d` exists, so the sequence is necessarily release-old → acquire-new, with a moment where NEITHER home is held. That gap is safe only because all lanes are frozen and the owner is present — but D3 says neither.
- **Fix (exact):** D3 step 5 add: "the release→acquire gap is inherent (the new script must refuse while the old home exists); it is safe ONLY under the D-FREEZE and owner presence. No other session may acquire in this window; the script prints the gap loudly before releasing."

### P2-3 — "re-run the updater's 'release dir outside the install' check" is not callable from bash
- **Why:** that check is Go logic inside the updater (host_os/runner realpath comparisons). A bash script cannot "re-run" it unless R1a exposes it.
- **Fix (exact):** either R1a adds `nofx-updater check-release-dir <release-dir> <install-dir>` (updaterworker already owns the code, add one subcommand) and D3 calls it, or D3 spells the invariant it replicates (`realpath <release-dir>` must not be inside `realpath ~/vl`, with both printed). Pick one; don't leave "re-run the check" dangling.

### P2-4 — the new updater user unit must be written with `--install-dir %h/vl`, and D3 never says so
- **Why:** `nofx-updater.service` bakes `ExecStart=%h/bin/nofx-updater --install-dir %h/nofx serve` [A]. Copying that unit and renaming it would leave the new updater serving `~/nofx` (works through the symlink, but then the rename is cosmetic for the updater's canonical path, and after any future symlink removal the unit breaks).
- **Fix (exact):** D3 step 4: "write the vl-updater unit with `ExecStart=%h/bin/vl-updater --install-dir %h/vl serve` (template like `deploy/install-updater-worker.sh`), never a copy of the old unit."

### P2-5 — the script must `cd` out of any `~/nofx` subtree before `mv`, and refuse if the release dir is inside the install
- **Why:** `mv` of a directory that is the shell's cwd leaves the session with a dangling cwd (and on some paths makes later relative operations land in the moved tree). The release dir used for the build must also be outside `~/nofx` or it moves under the bot.
- **Fix (exact):** D3 step 0 add: "script `cd $HOME` first; refuse if `$PWD` is under `~/nofx`; refuse if `realpath --release-dir` is inside `realpath ~/nofx`."

## P3

- **P3-1 Step 0 refusals:** also refuse when `~/nofx` is missing/not a directory, when `~/vl` exists even as a symlink, and when the old lock is HELD BY ANOTHER SESSION (named, with `nofx-lock.sh status` quoted) — the text only says "held by the session named in `--session`".
- **P3-2 Git worktree proof:** PROVEN [A] on a scratch repo: `mv a vl && ln -s vl a` → `git -C vl worktree list` still 2, `git -C <wt> status` clean; every `<wt>/.git` gitdir path resolves through the symlink. Add to D3 step 2 a cheap sanity: `git -C ~/vl worktree list | wc -l` equals the pre-count (294 today) and `<one-worktree> status` is clean.
- **P3-3 Release clones + `~/nofx-release-key.pub`:** D3 never mentions them. They are build caches; their remotes redirect after R4. State "out of scope, covered by GitHub redirects" so a lane doesn't try to migrate them.
- **P3-4 `.env` / updater env values:** `~/.config/nofx-updater/env` values embed old absolute paths — symlink-covered; the optional `sed` offer should extend to this file, owner's call.
- **P3-5 90 s verify window:** BOOT INTEGRITY prints early, but the plan should say the 90 s leg is only the BOOT INTEGRITY + health-revision read (never NT8 — see P1-1), and that the auto-rollback is waived only by the owner's explicit word at the boot (NO UNATTENDED DEPLOYS).
- **P3-6 D3 wording:** "installation gate READY (GET /api/installation-gate with a gate-jwt read from a file, never argv)" — name the file/env: `NOFX_CUTOVER_TOKEN` (R1a: `VL_CUTOVER_TOKEN` fallback), sent as the `X-Cutover-Token`-style header `cutover.sh` uses; say the script must NOT log it.

## What the audit verified as sound

- Symlink coverage of git worktree metadata (P3-2 proof) — the plan's core claim holds.
- R1a's dual-reader list covers every reader the box walk found (log glob, unit name, cgroup, lock path, env fallback, backup script names) [A].
- Step 1 before step 2 ordering is correct (stop → mv).
- Old unit remains runnable via symlink until step 3 — with the P1-2 reorder, at every prefix of the forward run the box is runnable.
- `crontab` has nothing to migrate; bridge/hook paths reference nothing under `~/nofx` that survives the check [A].

## L14

- Branch `audit/rename-migrate` (claim `2e9c6cf9f`, base `git log -1` = `9d52f5dc6`), HEAD after this report commit posted below.
- Files touched: this report only. No code, no units, no DB, no deploy, no lock, no worktree edits elsewhere.
- Read-only: every command above was `cat`/`grep`/`ls`/`git -C` on copies or a scratch dir in `/tmp`.
