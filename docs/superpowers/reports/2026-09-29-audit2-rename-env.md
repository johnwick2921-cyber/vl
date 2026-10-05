# AUDIT2-RENAME-ENV — READ-ONLY round-2 audit of the VL rename plan v2 (ENV + CONFIG + shell slice)

DS-103 · 2026-09-29 · branch `audit2/rename-env` · base origin/dev @ accept:

```
9d52f5dc6 RELEASE 4d5382069… — boot 7: #266 … + #265 … booted 01:00:43 CT 2026-09-28
```

Plan v2: `/home/hoang/rename-vl-plan/2026-09-29-rename-nofx-to-vl-plan-v2.md`, md5 `7b8dc9a9a196e5554203db7a3b71aa7b` (matches the dispatch's 7b8dc9a9; full text read). Slice: ENV + CONFIG + every shell script (R1a item 2, R1b items 2/4) + folds F9, F10. Round-1 report for this slice: draft PR #269 (DS-105) — read in full; its P0/P1/P2/P3 map onto F9 (init-time readers), F10 (complete env list), F19 (CI/compose), F29 (script unit names).

Evidence tiers: **[A]** read/ran the exact line at this base · **[B]** inferred · **[C]** speculation. No code changed; the branch adds only this report.

---

## (a) Fold verification — F9, F10

**F9 (init-time env readers) — CORRECT and COMPLETE.** Re-read at base: `api/handler_klines.go:572` is a package var (`var chartAcrossRoll = … os.Getenv("NOFX_CHART_ACROSS_ROLL")` — evaluated at package init [A]); `provider/ninjatrader/bar_source.go:44-53` is `init()` reading `NOFX_BAR_SCALE_MISMATCH_PCT/_MULT` [A]. These are the ONLY init-time readers: the rest of the census is function-lazy (`kernel/boot_integrity.go:88` `expectedRevision()`; `kernel/clock_health.go:146` inside a call path; `trader/auto_trader_calendar.go:117` `calendarStaticLoader()` [A]). F9's fix — helper works pre-logger, records the source, WARN deferred to first log emit, boot lines state the source — addresses exactly the P0-1 mechanism, and the boot-line hooks exist (`ChartAcrossRollResolved` is READ onto the boot line, `handler_klines.go:574` comment [A]; the bar-source boot line already prints the resolved values [A]). **No finding.**

**F10 (complete env list) — CORRECT on the NAMES, two consistency gaps in the dispatches (see (c)).** I re-ran the full census at the base (`grep -rhoE 'NOFX_[A-Z0-9_]+'` over `*.go *.sh *.yml *.yaml *.md *.env* Makefile`, 9d52f5dc6) and diffed every token against F10 + the original R1a list. Every production and test-only name is covered [A]:

- cutover `NOFX_UNIT/INSTALL/ACTIVATE_BIN/HEALTH_URL/GATE_URL` ✓; lock `NOFX_LOCK_DIR/LEGACY_LOCK/STALE_SECONDS/BEAT_SECONDS/INCOMPLETE_SECONDS` ✓; backup `NOFX_DB/DB_RESEARCH/BACKUP_DIR/BACKUP_RESEARCH/BACKUP_MIN_FREE_GB/KEEP_DAILY/KEEP_WEEKLY/KEEP_RESEARCH_DAILY/KEEP_RESEARCH_WEEKLY` ✓; updater/ops `NOFX_UPDATER_BUILD_REPO/UPDATER_INSTALL_DIR/REPO/DATA/BIN/ENV/SESSION` ✓; test-only `NOFX_LIVE_TESTS, NOFX_DEMO_*, NOFX_REHEARSAL*` (incl. `_BASE/_DB/_OUT/_TRADER`), `NOFX_M3_OPEN_FINDINGS, NOFX_TEST_*` (incl. `TEST_DOTENV_GOOD/VALID`, `TEST_REFUSAL_SERIES_CHILD`) ✓. The names `NOFX_CUTOVER_TOKEN`/`NOFX_RELEASE_DIR`/`NOFX_CLOCK_STATE`/`NOFX_EXPECTED_REVISION`/`NOFX_RELEASE_INBOX`/`NOFX_CALENDAR_STATIC`/`NOFX_CHART_ACROSS_ROLL`/`NOFX_BAR_SCALE_*`/`NOFX_DB_PATH`/`NOFX_TRADER_ID`/`NOFX_UPDATER` were already in the original list ✓.

**Not covered by F10 or any fold, found in this round's census [A]:**
- `NOFX_ADMIN_PASSWORD` — `docs/getting-started/README.md:84` only; ZERO code readers (grep over go/sh/yml). A docs-only env name that has never existed in code. → **P3-A**.
- `NOFX_TIMEZONE` — dead (round-1 knew it, no fold named it). → **P3-B**.
- `NOFX_HALF_DAYS` — comment-only mention (`trader/auto_trader_halfdays.go:27`), no Getenv (round-1 knew it, no fold named it). → **P3-C**.
- Docker-path port names in `start.sh:178-188` (`read_env_vars()` greps `.env` for `NOFX_FRONTEND_PORT`/`NOFX_BACKEND_PORT` and feeds the compose branch), `install.sh:105-106`, `install-stable.sh:67-68` — **F19 folds the compose files + CI but NOT these three scripts**; R1b item 2's generic "scripts use VL_*" is the only cover. → **P2-A** (partial fold).

---

## (b) New findings beyond round 1

**P2-A — F19 is partial: `start.sh`/`install.sh`/`install-stable.sh` port reads must flip in the same R1b commit.** [A] `start.sh:176-188` `read_env_vars()` greps `.env` for the `NOFX_*_PORT` keys; after R1b item 2 rewrites `.env.example` to `VL_*`, the grep finds nothing and docker mode silently falls back to the hard-coded 3000/8080 — a behavior regression masked as "docker-only" by round 1. Round 1 called these docker-only and F19 was folded narrowly. Why it matters: docker users' port overrides stop working with no error. EXACT FIX to F19: *"…docker-compose `${VL_*_PORT}`, and in the SAME commit: `start.sh` `read_env_vars()` (grep the `.env` file for `VL_*_PORT`, falling back to `NOFX_*_PORT` for one release), `install.sh:105-106`, `install-stable.sh:67-68`, `.env.example` port keys, `.github/workflows/pr-docker-compose-healthcheck.yml:38-40`."*

**P3-A — `NOFX_ADMIN_PASSWORD` is docs-only.** [A] `docs/getting-started/README.md:84` documents an env that no code reads (grep: zero readers). It is a CURRENT doc (not a dated history report), so F23's "dated history docs" bucket does not cover it. EXACT FIX to F23: add the pair `docs/getting-started/README.md: NOFX_ADMIN_PASSWORD` to the allow-list with the note "docs-only, zero readers — deleted or renamed in the R1b docs pass", or add an R1b item: "delete the `NOFX_ADMIN_PASSWORD` line from docs/getting-started/README.md (it has no reader)".

**P3-B — `NOFX_TIMEZONE` has no fold.** [A] zero readers since 2026-09-26 (round-1 §2). It still sits in `.env.example` and the owner's `.env`. EXACT FIX to R1b item 4: add "drop `NOFX_TIMEZONE` from `.env.example` (dead); leave the owner's `.env` untouched".

**P3-C — `NOFX_HALF_DAYS` comment-only trap is still open.** [A] `trader/auto_trader_halfdays.go:27` mentions it in a comment with no Getenv. The F23 token/pattern allow-list can absorb it, but the fold should say so. EXACT FIX to F23: add `trader/auto_trader_halfdays.go: NOFX_HALF_DAYS (comment-only, no reader)`.

**No other new env/config readers exist.** `config/config.go` reads zero `NOFX_` names [A] (round-1 verified; re-verified). `store/knob_registry.go` zero [A]. `.github/workflows/*` carries only the docker-port trio + compose names (covered by P2-A + F19).

---

## (c) Consistency — phase/dispatch text that still contradicts a binding fold

The v2 header says the folds "override any phase/dispatch text below that disagrees", so these are not ambiguous in LAW — but they are ambiguous in EXECUTION: the dispatch text is what a lane reads line by line. Each quote needs the fold's text spliced in.

1. **D1 item 2 (env helper) contradicts F9.** Plan v2 D1 item 2 still says: *"WARN once per name when only the NOFX_ form is set. Route every production `os.Getenv("NOFX_…")` through it"* — F9 overrides exactly this for the two init-time readers (their WARN must be lazy, pre-logger). **[A]** both texts are in the file. EXACT REPLACEMENT for D1 item 2's first sentences: *"add ONE helper `config.Env(name)`: `VL_<name>` if set, else `NOFX_<name>`, else \"\"; it records which name was used and emits the WARN lazily — once, at the first log emit (F9). Route every production `os.Getenv("NOFX_…")` through it EXCEPT the two package-init readers (`api/handler_klines.go:572`, `provider/ninjatrader/bar_source.go` `init()`), whose boot lines state the resolved value AND its source (VL_ / NOFX_ / default) (F9)."*
2. **D1 item 2's "at minimum" list survives.** The dispatch still enumerates an 11-name at-minimum list while F10 (binding) holds the complete list. A lane diffing them spends time reconciling; a lane trusting only the dispatch ships the short list. EXACT REPLACEMENT: delete the at-minimum sentence and write: *"The complete name list is F10 (binding). The PR must quote a census diff (`grep -rhoE 'NOFX_[A-Z0-9_]+'` before/after) — a missed reader is a red diff, not a judgment call."*
3. **D2 item 4 (env docs) omits the writer and the dead names.** It says only *".env.example, runbooks, guide"*. EXACT REPLACEMENT: *"Env names in docs/examples/scripts → `VL_*` (`.env.example`, runbooks, guide, `install-updater-worker.sh` — which WRITES the env file and writes `VL_*` names from R1b on (F10); drop the dead `NOFX_TIMEZONE`; the test-only envs are renamed with no fallback (F10)). The owner's .env stays valid through R1a's fallback — do NOT touch it."*

*(For completeness — out of this slice but same class: D2 item 3's header sentence and D2 item 5's storage sentence still carry the pre-F22/F21 text in the dispatch; the same splice-in is needed there.)*

---

## Ranked round-2 findings

| # | Rank | Finding | Where | Evidence |
|---|---|---|---|---|
| 1 | P2 | F19 partial: start.sh/install.sh/install-stable.sh port env reads not folded (docker port overrides silently revert to defaults) | `start.sh:176-188`, `install.sh:105-106`, `install-stable.sh:67-68` | [A] |
| 2 | P2 | D1 item 2 contradicts F9 (WARN-once wording) and carries the superseded at-minimum list | plan v2 D1 item 2 | [A] |
| 3 | P3 | `NOFX_ADMIN_PASSWORD` docs-only, zero readers, not allow-listed | `docs/getting-started/README.md:84` | [A] |
| 4 | P3 | `NOFX_TIMEZONE` dead but unfolded | `.env.example` (zero readers) | [A] |
| 5 | P3 | `NOFX_HALF_DAYS` comment-only trap still open | `trader/auto_trader_halfdays.go:27` | [A] |
| 6 | P3 | D2 item 4 omits install-updater-worker.sh (the env-file writer) and the dead names | plan v2 D2 item 4 | [A] |

F9: CORRECT and COMPLETE. F10: CORRECT on names. No new P1 found in the slice beyond the above — the round-1 P0s (init-time readers, cutover envs) are properly folded and verified.

## What I did NOT do
No code change; the branch adds only this report. No worktree edit outside this file, no deploy, no unit/DB action, no lock, no .env read beyond key names, no new race (L16: the round-1 audit's race at the same base was still running; `go build ./...` + `go vet ./...` clean at the claim head — tail below).
