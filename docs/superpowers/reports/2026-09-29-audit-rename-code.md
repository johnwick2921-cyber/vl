# AUDIT — VL rename plan, code half — NARROWED SLICE (DS-101): updater + activation readers, pass-on-empty tests, D0/D1 wording

- **Auditor:** DS-101 (Copilot lane), 2026-09-30. Narrowed per CTO mail `1790730545618-436-000001` (sent 2026-09-30T01:09:05Z): DS-101 keeps **UPDATER + ACTIVATION READERS (R1a item 1 + 3: log globs, binary names, unit/MainPID, cgroup refusal, lock-script path, forbidden-import pass-on-empty in `internal/updaterworker` + `internal/activation` + `cmd/*`)** and **D0/D1 wording**. All other slices (module rewrite, web/UI, migration script, env/config, partner+GitHub+release, NT8+wire+CI, everything outside the repo) are out of my scope and not duplicated here.
- **Plan under audit:** `/home/hoang/rename-vl-plan/2026-09-29-rename-nofx-to-vl-plan.md` — md5 `b16759336fe0e264d11c1bc885ce5192` (matches `b1675933`), read in full.
- **Base:** `origin/dev` @ `9d52f5dc6` — `RELEASE 4d5382069643a491403ac0a8575f2bbea3648676 — boot 7: #266 (updater installs with NT8 CLOSED…) + #265 (release build outside tree) booted 01:00:43 CT 2026-09-28` (`git log -1` quoted; fetched ~01:10Z). Also read: `origin/fix/updater-flat-live-ok` @ `21a8b6cd3` (PR #267, D0).
- **Disposition:** DONE_WITH_CONCERNS — no P0 in this slice. The reader lists are nearly complete; 4 missed sites (2 break loudly in R1b, 2 silently drift), one wrong-file citation, and a stale-fixture class D2 must handle.

Evidence tiers on every claim: [A] read the exact line this session · [B] inferred · [C] speculation.

---

## CHECK A — complete reader inventory in `internal/updaterworker` + `internal/activation` + `cmd/nofx-updater` + `cmd/nofx-activate`

Method [A]: `grep -rn "nofx"` over the four dirs, non-test files, comments excluded → 81 hits; imports (`"nofx/internal/…"`, 20+) are R1b-item-1 mechanical rewrites, NOT readers. Every name-reader is listed below against the plan's citations.

### A1 — log globs (`nofx_*.log`)

| Site | Reads | Plan cites? | Verdict |
|---|---|---|---|
| `internal/updaterworker/runner.go:394` | builds `nofx_<date>.log` (date-keyed) | R1a item 1 ✓ | covered |
| `internal/updaterworker/recovery.go:133` | shell string greps `…/nofx_$(date +%F).log` | ✓ | covered |
| `internal/activation/system.go:121` | `filepath.Glob(nofx_*.log)` — **the actual glob the boot proof uses** | ✓ | covered |
| `internal/activation/system.go:126` | error text `"no nofx_*.log in %s"` | — | cosmetic; edit with :121 |
| `cmd/nofx-activate/main.go:37` | flag default text `data/nofx_*.log` | ✓ | covered |
| `internal/updaterworker/bootcheck.go` | **no glob of its own** — comments only; its log input arrives via `activation.Watch` (system.go:121). Plan lists bootcheck.go as a reader; the real edit site is system.go:121 (P3 wording, CHECK C) | ✗ imprecise | wording fix |

No other log-name reader exists in the slice. ✓

### A2 — binary name (`nofx-bin`)

| Site | Reads | Plan cites? | Verdict |
|---|---|---|---|
| `internal/updaterworker/release.go:109` | `binaryName = "nofx-bin"` | ✓ | covered |
| `internal/updaterworker/target.go:98` | `Binary: Join(InstallDir, "nofx-bin")` | ✓ | covered |
| `internal/updaterworker/runner.go:317` | `bin := Join(InstallDir, "nofx-bin")` | ✓ | covered |
| `internal/updaterworker/snapshot.go:24` | `Binary: Join(dir, "nofx-bin")` | ✓ | covered |
| `internal/activation/activation.go:95` | `Binary: Join(dir, "nofx-bin")` | ✓ | covered |
| `internal/activation/steps.go:491` | `Binary: Join(dest, "nofx-bin")` | in D1 text ✓ | covered |
| `internal/updaterworker/releasefixture/releasefixture.go:164,175` | **the archive fixture hardcodes `"nofx-bin"` as the stand-in ELF** (map key :164, chmod :175) | ✗ **missed** | **P2-1** |

P2-1: R1a item 1 adds tests of the form "an archive whose bot writes ONLY `vl-bin` activates" — those tests cannot be built on a fixture that only ever emits `nofx-bin`. R1a must parameterise the fixture's binary name, and the new tests then prove both names.

### A3 — unit / MainPID

| Site | Reads | Plan cites? | Verdict |
|---|---|---|---|
| `internal/activation/system.go:43` | `exec.Command("systemctl","show","-p","MainPID","--value","nofx")` | ✓ | covered |
| `internal/updaterworker/recovery.go:153` | `restartLine` shell const: `systemctl show -p MainPID --value nofx` (a string-built command in Go — R1a edits this string, not a glob) | ✓ | covered |
| `internal/activation/system.go:27-38` | reads `/proc/<pid>/stat` from the MainPID; comment warns "NEVER pgrep" (`pgrep -f nofx-bin` example) | — | covered by the :43 edit; comment cosmetic |
| `internal/updaterworker/bootcheck.go:90` | "the new MainPID %d cannot have written a boot line" — pid check, no name | — | no edit |

No `pgrep`, no `/proc/*/comm`, no argv0 parsing anywhere in the slice [A] (`grep pgrep|argv0|/proc` → only the two comment sites above).

### A4 — cgroup refusal

| Site | Reads | Plan cites? | Verdict |
|---|---|---|---|
| `internal/updaterworker/host_os.go:34` | `ErrBotCgroup` text | ✓ | covered |
| `internal/updaterworker/host_os.go:45` | `bytes.Contains(b, []byte("/nofx.service"))` | ✓ | covered |
| `internal/updaterworker/host_os.go:23` | `readCgroup` reads `/proc/self/cgroup` | — | the reader the refusal uses; no edit |

Plan's claim that "today it would silently stop protecting after a rename" is TRUE [A]: the check is a `bytes.Contains` against the literal — if the unit becomes `vl.service`, the contains misses and the refusal passes vacuously.

### A5 — lock-script path

| Site | Reads | Plan cites? | Verdict |
|---|---|---|---|
| `cmd/nofx-updater/main.go:167` | `LockScript: Join(InstallDir, "deploy", "nofx-lock.sh")` — the ONLY construction site | ✓ | covered |
| `internal/updaterworker/host_os.go:56,78-86` | abs/stat/`check`-verb execution | ✓ | covered |
| `internal/updaterworker/deps.go:234-236` | `MainTreeLockHeld` interface field (comment names nofx-lock.sh) | — | no edit (path flows from main.go:167) |
| `internal/updaterworker/steps.go:145` | calls `w.host.MainTreeLockHeld()` | — | no edit |

Complete. ✓

### A6 — CLI prose strings (cosmetic, mechanically renamed with the cmd dirs in R1b item 1)

`host_os.go:30` ("run nofx-updater"), `recovery.go:147`, `reverifier.go:79,83`, `runner.go:435`, `steps.go:552` ("nofx-updater resume"), `cmd/nofx-updater/main_test.go:811` (argv0 assert), `cmd/nofx-activate/main_test.go:16,122`. D1/D2 should say "rename CLI prose in the same commit as the dirs; the census guard's fallback bucket covers them".

---

## CHECK B — pass-on-empty / stale-name test inventory in the slice dirs

The plan's D1 item 4 names `release_test.go:997`, `reverifier_test.go`, `sshsig_test.go` (+ `branding/scope_test.go:48`, outside my slice). Full inventory of literal `"nofx…"` strings in `_test.go` files under the slice dirs [A]:

### B1 — would pass vacuously / silently drift after the rename

- **P2-2 `internal/updaterworker/bootcheck_parity_test.go:48`** — substitutes the synthetic caller `"nofx/main.go:322"` into a line the REAL formatter produced, then matches against the matcher. After R1b the production caller is `vl/main.go:322`; this test keeps passing while its fixture no longer represents what production emits — canon-53 drift (a parity test that stops exercising the production shape). Fix for D2: build the caller from `censuswalk.ModulePath` (same pattern as `hold_census_test.go:175`).

### B2 — would break LOUDLY in R1b (safe, but D2 must list them; the mechanical import rewrite will not touch them)

- **P2-3 `internal/updaterworker/containment_census_test.go:28`** — walks `filepath.Join("..","..","cmd","nofx-updater")`; after R1b item 1 renames the dir, the walk errors and the test `t.Fatal`s at :45-47 [A]. Not vacuous — it fails red — but item 1 must include "update the containment-census walk path" or CI is red on the rename commit.
- **P2-4 `cmd/nofx-updater/main_test.go:951`** — `exec.Command("go","list","-deps","nofx/cmd/nofx-updater")` — DATA string with BOTH the module path and the dir name. Breaks loudly after both change; D2 item 1 must name it (derive module from `censuswalk.ModulePath`, dir from the new name).

### B3 — stale-name fixtures (tests keep passing on the OLD name; D2 must switch them or they pin the retired name)

`harness_test.go:163,172,180,375,432,443,563,619` (harness builds installs/logs/releases with `nofx-bin` + `nofx_<date>.log`), `activate_reprove_test.go:30`, `internal/activation/stage_test.go:65`, `cmd/nofx-updater/main_test.go:148,220` (cgroup text assert). D2 item 1 should say: "after the writer switch, every harness/fixture in `internal/updaterworker` + `internal/activation` that fabricates `nofx-bin`/`nofx_<date>.log` switches to the vl names (or parameterises), so the tests exercise the production names."

### B4 — synthetic-tree tests (correct as-is, one caveat)

- `internal/updaterworker/hold_census_test.go:157-160` — synthetic files import `"nofx/internal/updaterworker"` while the sought package is `module + "/internal/updaterworker"` (ModulePath-derived, :175). After the rename the synthetic imports become wrong → offenders=0 while the test expects them → RED, not vacuous. D2 item 1 must rewrite the synthetic strings. This is the file the plan cites as `store/hold_census_test.go ~175` — **wrong path**, see P3-1.
- `release_test.go:997` map keys (`"nofx/internal/updateauth"`, `"nofx/internal/activation"`) — DATA, not imports; goimports will NOT rewrite them → the forbidden check looks for imports that no longer exist → **vacuous pass**. The plan's item 4 concern is REAL for this file; ModulePath derivation is the right fix. ✓ plan correct.
- `reverifier_test.go:15-16`, `sshsig_test.go:18` — the `"nofx/…"` literals here are IMPORT statements; item 1's goimports rewrites them. The plan's item 4 lists these files as needing ModulePath derivation — unnecessary (P3-3, wording).
- `driver_guard_test.go` and `hold_census_test.go` already derive via `censuswalk.ModulePath` ✓ (the pattern exists at `internal/updaterworker/hold_census_test.go:175`, not `store/`).

---

## CHECK C — D0/D1 wording (my wording scope)

- **D0 — precise, verified [A].** On `21a8b6cd3` (`origin/fix/updater-flat-live-ok`), `trader/installation_gate.go:357` appends `" — %d connected non-SIM connection(s), all accounts flat — allowed (owner ruling 2026-09-28)"` to `detail` before the refusal `why` list is returned — a refusal for an open position reads "all accounts flat — allowed". D0's fix (append only when `positions == 0 && working == 0`) is exactly right, and its new assertion ("the refusal detail for a live open position does NOT contain 'all accounts flat'") is the correct call-site test. No wording change needed. Note: the pass path also requires `nonSim==0 && unsettled==0`, so the suffix on pass always reads "0 connected non-SIM connection(s)" — fine.
- **D1 item 1 wording — three fixes.**
  - P3-1: `store/hold_census_test.go ~175` → `internal/updaterworker/hold_census_test.go:175`.
  - P3-2: "bootcheck.go" is listed as a log-glob reader; bootcheck.go has NO glob (its only name-adjacent line is the MainPID error at :90; its log input arrives via `activation.Watch` → `system.go:121`). Reword: "the boot-proof glob lives in `internal/activation/system.go:121`; `bootcheck.go` needs no log-name edit".
  - P3-3: item 4 lists `reverifier_test.go` and `sshsig_test.go` as forbidden-import files needing ModulePath derivation; their `"nofx/…"` literals are imports (mechanically rewritten). Restrict the derivation to DATA literals: `release_test.go:997` (map keys), `branding/scope_test.go:48` (prefix compare), plus the two loud-break paths (B2) and the parity caller (B1) for D2.
- **D1 item 3 wording** — "agent/tools.go (~1249, log tool)": line 1249 is the log tool's glob `data/nofx_*.log` ✓ exists [A]. It sits in `agent/`, outside my slice dirs — routing note: the plan should state explicitly who edits it so it is not orphaned between slices.
- **D1 item 5 "byte-identical" claim** — says "Byte-identical runtime behaviour today"; the env helper spec says "+ one WARN". The WARN is the only runtime delta of R1a and it is safe (the boot-line matcher anchors at line start on the boot line itself, `bootcheck.go:28-30` [A]) — add the qualifier so the claim is literally true: "byte-identical except one WARN line per fallback env name".
- **D1 test list ("Tests (call sites)")** — precise and buildable: a fake install writing ONLY `vl_<date>.log` reaches `booted`; archive with only `vl-bin` activates; MainPID via `vl` when `nofx` absent; cgroup refusal inside `/vl.service`; env helper precedence; pruner deletes an old `vl_` and an old `nofx_` log. Each maps to a real production call site in CHECK A. One addition: "the archive test is built on a releasefixture parameterised by binary name (releasefixture.go:164,175)".

---

## Findings ranked (for the CTO's fold into the plan)

| # | Sev | Where | Why it matters | Exact fix to the plan text |
|---|---|---|---|---|
| 1 | P2 | `releasefixture/releasefixture.go:164,175` | R1a's own dual-archive test cannot be built on a fixture that only emits `nofx-bin` | R1a item 1: "parameterise the fixture's stand-in binary name; new tests prove `vl-bin`-only archives activate" |
| 2 | P2 | `bootcheck_parity_test.go:48` | Synthetic caller `nofx/main.go:322` keeps passing while production prints `vl/main.go:322` — canon-53 drift | D2 item 1: "derive the parity caller from `censuswalk.ModulePath`" |
| 3 | P2 | `containment_census_test.go:28` | Walk path `cmd/nofx-updater` breaks loudly (t.Fatal) on the dir rename | D2 item 1: "update the containment-census walk path with the dir rename" |
| 4 | P2 | `cmd/nofx-updater/main_test.go:951` | `go list -deps nofx/cmd/nofx-updater` — data string with module + dir name; breaks loudly | D2 item 1: same commit as the module + dir rename |
| 5 | P3 | plan cites `store/hold_census_test.go ~175` | Wrong path in both R1a and D1 | → `internal/updaterworker/hold_census_test.go:175` |
| 6 | P3 | D1 item 1 "bootcheck.go" as log-glob reader | bootcheck.go has no glob; the glob is `activation/system.go:121` | reword as in CHECK C |
| 7 | P3 | D1 item 4 lists reverifier/sshsig for ModulePath derivation | their nofx literals are imports, rewritten mechanically | restrict derivation to data literals (release_test.go:997, scope_test.go:48) |
| 8 | P3 | D1 item 5 "byte-identical" | one WARN per fallback env name IS a runtime delta (safe) | add "except one WARN line per fallback env name" |
| 9 | P3 | stale-name harness/fixtures (B3 list) | after R1b they pin the retired name | D2 item 1: "switch harness/fixtures in the slice dirs to the vl names" |
| 10 | P3 | `agent/tools.go:1249` reader unowned between slices | could be orphaned | D1 item 3: name the owning lane for the agent/ reader |

## Out-of-slice handoff (already reported to CTO; listed once, not duplicated)

- P1-1 `deploy/leveltruth-cutover.sh:37,49` (unit `nofx` reader; live cutover path) → route to DS-104/DS-106.
- P1-2 `deploy/cutover.sh:69-70` (stages `nofx-bin`) → route to DS-102/DS-104.
- P2 `~/nofx-backups` writers + `NOFX_LOCK_DIR` env + census allow-list width + storage-key/env/i18n/header/NT8 items → their slice owners.

## What I did NOT do (L-AUDIT)
No code change, no worktree edit outside this report, no deploy, no stop/start, no DB write, no lock acquire. Only `git fetch` and read-only grep/sed in the audit worktree. No race run (nothing built).
