# AUDIT-RENAME-OPS — DS-106 (operations half, READ-ONLY) — NARROWED EDITION

Scope narrowed by the CTO 20:15 CT 09-29 (8-lane split): DS-106 owns **everything outside the repo + the lock hand-over + D-FREEZE wording**. Migration-script (D3/rollback/sudo), env/config, web/UI, partner/GitHub/release findings from the earlier edition of this report are REMOVED here and owned by DS-104/DS-105/DS-103/DS-107 respectively — not duplicated.

Plan audited: `/home/hoang/rename-vl-plan/2026-09-29-rename-nofx-to-vl-plan.md`, **md5 `b16759336fe0e264d11c1bc885ce5192`** (matches the dispatch's `b1675933`). Code-citation base: `origin/dev` @ `9d52f5dc6`. Every observation is read-only (`systemctl cat`, `systemctl --user list-units`, `ls`, `stat`, `readlink`, `ps`, `strings`, `grep` — nothing stopped, started, or edited).

## Summary

For the outside-the-repo slice the plan is mostly sound: the bridge/hook machinery, VS Code, and the worktrees all survive through the symlink because they are keyed by path STRINGS. Two P1s stand: the live web unit `nofx-web.service` is missing from R2 entirely, and the tracked canon-law texts (not just CLAUDE.md) carry the main-tree path and worktree convention that the owner-steps don't cover. The lock hand-over has an unlocked window (P2) with a clean fix. D-FREEZE has two wording gaps (P2/P3) that could cost a lane a wasted merge.

---

## P1-1 — `nofx-web.service` exists, is ACTIVE and ENABLED, and is missing from R2/D3 entirely

- [A] `/etc/systemd/system/nofx-web.service`: `Description=NOFX frontend (vite dev server :3000)`, `WorkingDirectory=/home/hoang/nofx/web`, `ExecStart=/usr/bin/npm run dev`. `systemctl is-active nofx-web` → `active`; `is-enabled` → `enabled`.
- [A] The live listener is that server: `ss -ltnp` shows `127.0.0.1:3000` owned by node pid 427; `/proc/427/cwd` → `/home/hoang/nofx/web`. `nginx` is `inactive` — the web UI on this box IS the vite dev server under `nofx-web.service`.
- The plan's R2 handles only `nofx.service`. R1b creates `deploy/vl-web.service`, but nothing installs it or removes the old unit. After the rename the box would run `vl.service` + a still-enabled `nofx-web.service` — the web keeps working through the symlink, so nothing breaks, but the unit is a permanent `nofx` name the rename was meant to remove, and it is covered by no rollback either.
- **Fix to plan text (R2, add step 3b):** "stop and disable `nofx-web`, install `/etc/systemd/system/vl-web.service` from `deploy/vl-web.service` (same placeholders as `install-autostart.sh` fills), `systemctl enable --now vl-web`." And in `--rollback`: "stop+disable `vl-web`, re-enable `nofx-web`." (The rollback half belongs to DS-104's script — this line is for them to fold.)

## P1-2 — the tracked canon-law texts carry the main-tree path and the worktree convention; the owner-steps only cover CLAUDE.md

- [A] `docs/superpowers/CLAUDE-canon.md` holds 17 `nofx` mentions, including the WORKTREE LAW example `W=/home/hoang/nofx-<task>` at `:105`; `docs/superpowers/AUDIT-CHECKLIST.md:106` holds `git worktree add ../nofx-<task>` + `git worktree lock`.
- The plan's Owner steps say "CTO drafts the text, owner applies: CLAUDE.md name/path references; optional `.env` edit." That misses the TRACKED canon: after the rename, the law that "the main tree `~/nofx` is deploy-only" and every `nofx-<lane>` worktree example names a path that is now a symlink. Functionally the old wording still RESOLVES (the symlink), so lanes do not break — but a law whose example names the pre-rename path invites lanes to keep creating `~/nofx-<newlane>` worktrees whose `.git` files point through a symlinked main tree, and the R1b census allow-list must then decide whether those examples are allow-listed history.
- **Fix to plan text (Owner steps):** add — "CTO also drafts the same pass for the TRACKED canon: `docs/superpowers/CLAUDE-canon.md` (17 refs), the repo-root `AGENTS.md` mirror, `docs/superpowers/AUDIT-CHECKLIST.md` (worktree examples) — one commit on the rename branch, so the shipped law text names `~/vl` and the worktree convention `git worktree add ../vl-<task>`."

## P2-1 — the lock hand-over has an unlocked window a third session could enter

- Plan step 5: "release the OLD lock (old script), acquire+release once with the new script to prove `~/vl-main.lock.d`". Between release-old and acquire-new, NO lock exists: another session can `deploy/nofx-lock.sh acquire` the old home (`$HOME/nofx-main.lock.d` [A, `deploy/nofx-lock.sh:62`]; the dir does NOT currently exist [A]) while the migration session holds nothing — two sessions can each believe they own the main tree, the exact class-70 shape.
- The R1b rule ("new script REFUSES while the old lock dir exists") makes acquire-new-before-release-old impossible as written, so the window is inherent to the plan's ordering. Mitigated — not closed — by the D-FREEZE and the owner's presence.
- **Fix to plan text (R1b lock item + D3 step 5):** the `vl-lock.sh` refusal exempts the migration session — "refuse while `~/nofx-main.lock.d` exists UNLESS its `meta` names the same `--session` now calling acquire; then the new lock is acquired FIRST and the migration releases the old lock immediately after." D3 step 5 becomes: "acquire the NEW lock with the same `--session` (exempted by the old-lock holder match), then release the old lock — a lock home is never empty while the bot is stopped."

## P2-2 — D-FREEZE wording: two gaps a lane can misread

- "open no PR against dev, push nothing to a branch you intend to merge, start no new wave." Ambiguity 1: a lane whose PR is ALREADY open and mid-review (e.g. DS-103's #243 DRAFT at dispatch time) may read "open no PR" as "only new PRs are forbidden" and push a fix commit or request a merge — which is exactly the concurrent merge the freeze exists to prevent. Ambiguity 2: "start no new wave" vs the plan text's own "Draft/read-only work in your own worktree is fine" — the plan text says nothing about who may be dispatched DURING the freeze (the plan itself schedules D3/D4 in parallel with D2's window), so a lane may treat a CTO message arriving mid-freeze as outside the freeze.
- **Fix:** "From this message until 'RENAME MERGED <sha>': no lane opens a PR with base dev, pushes any commit to any branch it intends to merge (including already-open PRs — no new commits, no merge requests, no `gh pr edit`), and no lane starts work whose deliverable touches files DS-107 is rewriting; the CTO's own D3/D4 dispatches are the only exceptions and name their files. 'RENAME MERGED' is posted only by the CTO."

## P3-1 — the bridge, lane hooks and VS Code are keyed by path strings outside `~/nofx` — they survive; do not "fix" them

- [A] `~/.agent-bridge/copilot-hooks.json` registers `hook-ping.sh` / `copilot-stop-wait.sh` with absolute `/home/hoang/.agent-bridge/...` paths — the agent-bridge tree is NOT under `~/nofx`, so no hook breaks. The lane Stop hook and the bridge MCP registration live in VS Code user data (`~/.vscode-server/data/User/mcp.json`), also outside `~/nofx` [A]. VS Code workspace storage is keyed by the folder URI string `/home/hoang/nofx`, which the symlink keeps resolving [B].
- `~/.claude/projects/` holds 40+ `-home-hoang-nofx*` memory dirs keyed by the path string [A]: EXISTING memory keeps working through the symlink, but a session opened at `~/vl` starts a NEW empty `-home-hoang-vl` memory dir — the plan's "unaffected thanks to the symlink" is true for continuity and false for "no change".
- **Fix to plan text (Owner steps):** append — "keep opening the main tree as `~/nofx` (the symlink) in VS Code/Claude for as long as the old memory matters; `~/vl` is a new path with empty memory. No hook or MCP file needs editing — none lives under `~/nofx`."

## P3-2 — `~/nofx-main.lock.d` does not exist today; step 5's hand-over must not assume it predates the migration

- [A] `ls -d ~/nofx-main.lock.d` → absent. The migration acquires it in step 0 (correct); D3 step 5 says "release the OLD lock (old script)" — wording should say "(held since step 0 of this run)" so a reader does not assume a pre-existing lock.

## P3-3 — the installed updater unit bakes the old names; the new unit's ExecStart must be named explicitly

- [A] `systemctl --user cat nofx-updater.service`: `ExecStart=%h/bin/nofx-updater --install-dir %h/nofx serve`; the binary carries 1,299 `nofx` strings including `nofx-backups`, `nofx-lock.sh`, `nofx-updater` (`strings ~/bin/nofx-updater`). Step 1 stops it before the move ✓ and step 4 installs `vl-updater` ✓ — but the new unit line must be written out: `ExecStart=%h/bin/vl-updater --install-dir %h/vl serve` (through the symlink `%h/nofx` also resolves, but the installed unit should carry the real name). DS-104 owns the script; this line is the outside-the-repo fact they fold in.

---

## What I did NOT audit (by the narrowed split)

Migration-script internals, rollback mechanics, sudo structure (DS-104). Env/config readers and the release-dir guard interplay (DS-105). Web/UI, storage keys, guide (DS-103). Partner sync, GitHub repo/env/PR consequences (DS-107). NT8/AddOn/wire/CI (DS-108). Updater code readers R1a item 1+3 (DS-101). No tests run (L-AUDIT; report-only deliverable).
