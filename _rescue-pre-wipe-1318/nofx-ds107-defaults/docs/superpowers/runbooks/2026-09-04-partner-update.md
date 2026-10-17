# PARTNER UPDATE RUNBOOK — Binnie's machine, 2026-09-04

**Author** session `nofx-c6` · branch `docs/partner-sync-0904` · **read-only on hoang's box**:
nothing here restarted, swapped, or changed the running bot.

> ## ⛔ READ THIS FIRST — `git pull` WILL NOT WORK. YOU MUST RE-CLONE.
>
> The partner repo's history and ours have **NO COMMON ANCESTOR**. This is not
> "your branch is behind" — `git merge-base` returns nothing at all. Any
> `git pull`, `git merge`, or `git rebase` will either refuse outright or produce
> a corrupt tree. **Section 3 is a fresh clone, not a pull.** This was measured,
> not assumed — see §0.

---

## 0. WHY — the push was STOPPED, and nothing was pushed to your repo

Per **PARTNER REPO LAW** (never force-push to `vlautoagenttraderv1`; fast-forward only;
if it cannot fast-forward, STOP and report — the owner rules), **nothing was pushed.**

```bash
# run from hoang's repo root
git fetch https://github.com/johnwick2921-cyber/vlautoagenttraderv1.git main
git rev-parse FETCH_HEAD          # f6ae7597fb3bc9caeaaedb25ce8c3c48bca72247
git rev-parse origin/dev          # b2d3826e0f9db87f93d7a059ed0bf8af0ce04ef9
git merge-base --is-ancestor FETCH_HEAD origin/dev ; echo $?   # 1 = NOT an ancestor
git merge-base FETCH_HEAD origin/dev                            # (no output — unrelated)
git rev-list --left-right --count FETCH_HEAD...origin/dev       # 96   1622
```

| | value |
|---|---|
| our `origin/dev` | `b2d3826e` (boot-10 marker, RELEASE=`36648655`) |
| partner `origin/main` | `f6ae7597` — newest commit **2026-08-23 23:53:20 CT**, 12 days stale |
| merge-base | **NONE — unrelated histories** |
| partner-only commits | **96** |
| our-only commits | **1,622** |
| content difference | **1,440 files · 418,289 insertions · 2,115 deletions** |
| our root commits | `81f5be33` `6df9e8b6` `85794a72` |
| partner root commit | `75bcb576` — **in neither of ours** |

**Cause, and it was predicted.** The **2026-08-29 origin history rewrite** (the binary purge)
rewrote every sha on our side. Your clone is from 08-23, *before* it. `CLAUDE.md` says exactly
this: *"After any `origin` history rewrite (e.g. the 2026-08-29 binary purge): the partner repo and
every other clone MUST re-clone fresh — `format-patch → am` against a rewritten remote is corrupt
by construction."*

**The 96 commits on your side are listed in**
`docs/superpowers/reports/2026-09-04-partner-sync-data/partner-only-commits.txt`. Newest five:

| sha | date | subject |
|---|---|---|
| `f6ae7597` | 08-23 23:53 | sync(nofx): P0 AI-params config + clock-drift feedNowUTC + close_sync parity |
| `dfa52364` | 08-23 23:43 | feat(ui): sync DecisionCard to latest nofx (703-line card) |
| `e49ca145` | 08-23 23:37 | fix(mirror): split glued brace line in sendCloseAt |
| `31a486ce` | 08-23 23:36 | feat(mirror): 4.3 limit-close wire + C# fixes from nofx |
| `f9a6f001` | 08-22 19:03 | feat(wave4): per-model thinking knobs + exit-fill persistence |

**Owner decision needed before §3:** these 96 commits are *your* fork's work. A fresh clone of
`nofx` **discards them**. If anything in them is not already in our dev, it must be re-applied by
hand afterwards. The owner rules on this; this runbook does not.

**Secrets check (A25) — clean.** `.env` is untracked (`git ls-files | grep -c '^\.env$'` → 0).
The three `git ls-files | grep -i env` hits are `.env.example` (a template, placeholder values
only), `web/src/components/common/WebCryptoEnvironmentCheck.tsx` and `web/src/vite-env.d.ts` —
filenames containing "env", no secrets. No JWT secret, no API key, no NT8 account name appears in
this runbook or in anything that would have been pushed.

---

## 1. PRECHECK on your box

```bash
# 1.1 what is running right now
curl -s http://127.0.0.1:8080/api/health
#   expect: {"revision":"<12-hex>","status":"ok","time":null}
#   write the revision down — it is your rollback target.

# 1.2 NT8 AddOn build — read it from the bot's own log, not from the .cs file
grep -h "🔌 nt8 addon" data/nofx_$(date +%F).log | tail -1
#   ours reads: 🔌 nt8 addon: build_id=2026-09-03-f12 expected=2026-09-03-f12 match=yes
#   if yours says match=no, §7 is mandatory.

# 1.3 units — NOTE THE TWO SCOPES, they are not the same
systemctl is-active nofx                         # SYSTEM unit  → expect: active
#   (FragmentPath on ours: /etc/systemd/system/nofx.service)
systemctl --user is-active nofx-backup.timer     # USER unit    → expect: active
systemctl --user is-active nofx-clock-guard.timer # USER unit   → expect: active
```

**There is no `nofx` *user* unit** — `systemctl --user is-active nofx` returns `inactive` on ours
and that is correct. The dispatch listed all three as user units; only the two timers are.

### 1.4 Required `.env` keys — NAMES ONLY, never values

Twenty keys, from `.env.example`:

```
DATABENTO_API_KEY   DATABENTO_DATASET   DATA_ENCRYPTION_KEY   DB_HOST   DB_NAME
DB_PASSWORD         DB_PATH             DB_PORT               DB_SSLMODE DB_TYPE
DB_USER             JWT_SECRET          NINJATRADER_DATA_DIR  NOFX_BACKEND_PORT
NOFX_FRONTEND_PORT  NOFX_TIMEZONE       NT_TRANSPORT          RSA_PRIVATE_KEY
TRADING_MODE        TRANSPORT_ENCRYPTION
```

```bash
# compare names only — this prints NO values
comm -13 <(grep -oE '^[A-Z_][A-Z0-9_]*' .env | sort -u) \
         <(grep -oE '^[A-Z_][A-Z0-9_]*' .env.example | sort -u)
#   any line printed = a key you are missing
```

**New required keys since your rev: NONE that this audit can prove.** Your rev `f6ae7597` shares no
history with ours, so a `git diff` of `.env.example` between the two revisions is not a meaningful
diff — it is a comparison of two unrelated files. Compare the *name sets* with the command above
and treat any difference as new. `DATABENTO_API_KEY` is **not** required for futures trading
(Databento was dropped as a live source 2026-05-28).

---

## 2. STOP CONDITIONS — do not proceed unless the gate says ready

```bash
cd ~/nofx                      # MUST be the repo root: godotenv reads .env from $PWD
TOKEN=$(go run ./cmd/gate-jwt <your-email> data/data.db)
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/api/cutover-gate
```

Five legs, all must pass (`class 33`, emitted at boot as
`🛡 cutover safety (class 33): flat gate legs=5`):

| leg | name | passes when |
|---|---|---|
| 1 | `db_open_positions` | no OPEN row in `trader_positions` |
| 2 | `api_positions` | the broker API reports flat |
| 3 | `nt8_positions_snapshot` | NT8's own snapshot reports flat |
| 4 | `working_orders` | no working arm — **reads the `armed_orders` LEDGER** (it was a stub that passed vacuously at cutovers 35→41) |
| 5 | `planner_in_flight` | no planner read in flight |

**Proceed only on `ready:true`.** If any leg is false, wait — do not cancel anything to force it.

> ⚠️ Running `go run ./cmd/gate-jwt` from a worktree or any other directory silently falls back to
> the default JWT secret and mints a token the server answers 401 to. Repo root, always.

---

## 3. RE-CLONE + BUILD (not a pull)

```bash
# 3.1 preserve what is yours. NOTHING below touches these.
cp ~/nofx/.env            ~/nofx-keep.env
cp -r ~/nofx/data         ~/nofx-keep-data
mv ~/nofx                 ~/nofx-old-$(date +%Y%m%d-%H%M)

# 3.2 fresh clone — the directory MUST be named nofx
git clone https://github.com/johnwick2921-cyber/nofx.git ~/nofx
cd ~/nofx
git checkout dev
git rev-parse HEAD                      # expect b2d3826e… or later

# 3.3 restore your own state
cp ~/nofx-keep.env       ~/nofx/.env
cp -r ~/nofx-keep-data/. ~/nofx/data/

# 3.4 build. vcs.modified MUST be false — a dirty tree stamps modified=true
#     and BOOT INTEGRITY will refuse the binary.
git status --porcelain                  # must be EMPTY before building
go build -o nofx-bin.next .
go version -m ./nofx-bin.next | grep -E 'vcs.revision|vcs.modified'
#   expect: vcs.revision=<dev sha>   vcs.modified=false
```

### 3.5 The Guide rev — stamp AFTER the binary, BEFORE `npm run build`

`web/scripts/stamp-guide-rev.sh` reads the revision from a **running** binary via `/api/health`,
because the banner compares against `kernel.RunningRevision()` — a **12-char** short rev. A
hand-typed 40-char sha can never match, which is how the drift banner once got stuck on.

```bash
web/scripts/stamp-guide-rev.sh http://127.0.0.1:8080/api/health   # default URL, arg optional
cd web && npm ci && npm run build && cd ..
```

**Ordering matters and is the usual mistake:** binary first → stamp from the *old* running bot only
if it is already at the target rev; otherwise boot the new binary first, then stamp, then build
`dist`, then restart. The boot-5 lesson is that `GUIDE_BUILT_REV` + `dist` are built **before** the
final boot, not inside the marker commit.

---

## 4. UNITS + SCRIPTS

```bash
# 4.1 the ops scripts ship with the repo — nothing to install, but make them executable
chmod +x deploy/nofx-lock.sh deploy/nofx-claim.sh deploy/nofx-clock-guard.sh deploy/nofx-db-backup.sh

# 4.2 user timers (backup + clock-guard)
bash deploy/install-db-backup.sh
bash deploy/install-clock-guard.sh
systemctl --user daemon-reload
systemctl --user is-active nofx-backup.timer nofx-clock-guard.timer   # both: active

# 4.3 the OLD pid-style lock file — remove it if present.
#     The lock is now an atomic mkdir directory with a heartbeat; there is no pid field.
ls -d ~/nofx-main.lock.d 2>/dev/null      # the NEW form (a directory) — leave it alone
rm -f ~/nofx-main.lock                    # the OLD form (a flat file) — remove
deploy/nofx-lock.sh status                # rc 0 free · 1 held · 2 stale
```

**`nofx.service` is a SYSTEM unit** (`/etc/systemd/system/nofx.service`) and needs sudo to install
or restart. If you do not have sudo, run the binary directly and skip the systemd path — the
cutover in §6 uses `kill` + `Restart=on-failure`, which requires the system unit.

> **The tree-guard is NOT shipped.** The dispatch asks for a tree-guard unit + timer (§4) and four
> `--once` PASS lines (§8). Neither exists: `docs/superpowers/plans/2026-09-02-tree-guard-spec.md`
> is a **spec**, there is no `cmd/tree-guard`, no unit in `deploy/systemd-user/`, and `--once` is
> not implemented anywhere. **Skip both steps.** They are written here as absent on purpose so you
> do not go looking.

---

## 5. DATABASE

**Migrations run automatically at boot.** Two are flag-guarded and **must stay OFF**:

| flag | default | turn on only if |
|---|---|---|
| `ADHERENCE_REGRADE` | **OFF** | you have the same 2026-09-02 history — it **rewrites published grades** |
| `E8_BACKFILL` | **OFF** | same condition — it rewrites the E8 price-space columns |

**You almost certainly do NOT have that history** — your fork's last sync was 08-23, before the
day-plan era rows those migrations target. **Leave both unset.** If either is already in your
`.env`, remove it before booting.

```bash
# backup BEFORE the boot — online, safe while the bot runs
bash deploy/nofx-db-backup.sh
#   or manually:
mkdir -p ~/nofx-backups/manual-$(date +%F)
sqlite3 data/data.db ".backup '$HOME/nofx-backups/manual-$(date +%F)/data.db'"
ls -la ~/nofx-backups/manual-$(date +%F)/data.db     # must be non-zero
```

---

## 6. CUTOVER

**Order is not negotiable — RELEASE before the kill, marker after the boot, same tree.**

```bash
# 6.1 RELEASE first (BOOT INTEGRITY compares the binary's stamp against this file)
git rev-parse --short=8 HEAD > deploy/RELEASE
cat deploy/RELEASE

# 6.2 swap with mv, NEVER cp (cp into a running binary's inode corrupts it)
mv nofx-bin nofx-bin.prev-$(cat deploy/RELEASE)     # name the rollback by the rev it HOLDS
mv nofx-bin.next nofx-bin

# 6.3 kill — SIGKILL, so systemd's Restart=on-failure relaunches.
#     SIGTERM exits 0 and does NOT relaunch.
sudo kill -9 $(systemctl show nofx --property=MainPID --value)
```

### Within 90 seconds you must see all eight lines

```bash
tail -f data/nofx_$(date +%F).log
```

| # | line | ours at boot 10 |
|---|---|---|
| 1 | `🔐 BOOT INTEGRITY OK` | `rev 36648655cfe0 · built 2026-09-04T18:22:30Z · expected 36648655 · goldens PASS` |
| 2 | `🎯 arms:` | `bias-coherent=warn · stop-entry=on(reclaim) · far-arm counter=on(3.0×ATR5m) · ledger append-only=on` |
| 3 | `🧮 replan budget:` | `recorded-counter (class 35) — spends: death_replan, owner_reread · free: <S>_scheduled_read, level_event, structure_mss …` |
| 4 | `🔬 detector:` | `D1′ k=3 Δ=resolved-per-read band=k×Δ H=12 exit_on=close · touch_outcomes=<n> · candidate_pool=<n>` |
| 5 | `🔌 nt8 addon:` | `build_id=2026-09-03-f12 expected=2026-09-03-f12 match=yes` |
| 6 | `🖥 ui:` | `served-by=go-static build=<ISO>` |
| 7 | `⚙ settings:` | `schema=57 classified=167 live=144 ineffective=7 candidate-unverified=16 suspended=0 advisory=0` |
| 8 | `⏱ wakes:` | `cutoff=25m(enforce) cooldown=30m(enforce, fast-market≥1.5×ATR exempt) cross-session=on stale-arm-expiry=on` |

**If line 1 does not appear within 90 s, roll back immediately.**

### Five-reference check — all five must be the same rev

```bash
cat deploy/RELEASE                                    # 1
go version -m ./nofx-bin | grep vcs.revision          # 2
curl -s localhost:8080/api/health                     # 3
grep "BOOT INTEGRITY" data/nofx_$(date +%F).log|tail -1  # 4
git rev-parse --short=8 HEAD                          # 5
```

### Rollback

```bash
mv nofx-bin nofx-bin.failed
mv nofx-bin.prev-<REV> nofx-bin      # the file is NAMED for the rev it holds — use that rev
echo <REV> > deploy/RELEASE          # RELEASE must match the binary you just restored
sudo kill -9 $(systemctl show nofx --property=MainPID --value)
```

---

## 7. NT8 SIDE — AddOn `2026-09-03-f12`

**NT8 compiles ONLY from the fixed Windows path.** Editing the repo `.cs` does nothing.

```powershell
# 7.1 back up BOTH, named by the build id you are replacing
cd "$env:USERPROFILE\Documents\NinjaTrader 8\bin\Custom\AddOns"
copy VLTraderTCPClient.cs  "VLTraderTCPClient.cs.bak-<old-build-id>"
copy "..\..\NinjaTrader.Custom.dll" "NinjaTrader.Custom.dll.bak-<old-build-id>"

# 7.2 copy the new AddOn in
copy "\\wsl$\Ubuntu\home\binnie\nofx\ninjascript\VLTraderTCPClient.cs" .
```

Then, **in a flat window** (no position, no working order):

1. **F5** in the NinjaScript editor to compile.
2. **Full NT8 restart** — AddOns do **not** hot-reload. Skipping this runs the old binary and is
   the single biggest NT8 gotcha.

**Proof it took:**

```bash
grep "🔌 nt8 addon" data/nofx_$(date +%F).log | tail -1
#   build_id=2026-09-03-f12 expected=2026-09-03-f12 match=yes
grep "order_snapshot" data/nofx_$(date +%F).log | tail -2
```

**Leg 4 source flips `ledger → broker` once snapshots arrive.** Before the first snapshot ours
prints `🔌 order_snapshot: last=none · leg4 source=ledger (no snapshot yet)` — that is normal at
boot, not a failure. Snapshots then arrive every ~30 s (`reason=periodic`).

> **Known wart, so it does not alarm you:** every `nt8_order_snapshots` row has a **blank `symbol`
> column** (1,442 of 1,442 on our box). Read the instrument from `orders_json`, not from `symbol`.

---

## 8. POST-UPDATE CHECKS

```bash
curl -s localhost:8080/api/health                       # revision == deploy/RELEASE
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/ # 200 — Studio is on :8080
```

- **Studio is on `:8080`, not `:3000`.** The Go binary serves `web/dist` itself
  (`🖥 ui: served-by=go-static`). `:3000` is the vite dev server and is **not** the shipped UI.
- **Plan card renders** — open `:8080`, confirm the card draws with levels and scenarios.
- **Resolved config answers** (needs the token from §2):
  ```bash
  curl -s -H "Authorization: Bearer $TOKEN" localhost:8080/api/config/resolved | head -c 200
  ```
  Unauthenticated it returns `{"error":"Missing Authorization header"}` — expected, not a fault.
- **Snapshots gaining rows:**
  ```bash
  sqlite3 "file:data/data.db?mode=ro" "SELECT COUNT(*), datetime(MAX(received_at_ms)/1000,'unixepoch','-5 hours') FROM nt8_order_snapshots;"
  ```
  Run twice a minute apart — the count must grow.
- **First planner read writes the detector tables:**
  ```bash
  sqlite3 "file:data/data.db?mode=ro" "SELECT COUNT(*) FROM touch_outcomes; SELECT COUNT(*) FROM candidate_pool;"
  ```
  Both start at 0 on a fresh DB and fill from the **first read after the boot** — a scheduled read
  is at ASIA 16:30 / LONDON 01:30 / NY 08:00 CT. Zero before that is expected.
- **tree-guard `--once`: SKIP.** Not shipped (see §4).

---

## 9. WHAT YOU WILL STILL SEE WRONG AFTER THIS UPDATE

Honest list. None of these is fixed by this update.

1. **The rebrand is not done.** `nofx` appears everywhere — binary name, service name, log files,
   DB path, API routes, the repo itself. Rebrand phases 1+2 are **on hold**. Expect the old name in
   every surface.
2. **Row-38-class oddities persist** — mixed-timezone columns in sibling tables
   (`armed_orders.created_at` is CT-offset while `updated_at` is UTC; `plans.created_at` is UTC
   while `plan_lifecycle_log.at` is CT). Reading raw column text side by side gives wrong
   durations. Always normalise.
3. **`trade_excursions` is empty** and its backfill has no automatic trigger — it is reachable only
   from `cmd/excursions`. MAE/MFE live in `trader_positions.mae/mfe` instead.
4. **Your knobs are YOUR row's.** ⭐ **THE BOUND-ROW LESSON:** strategy config is resolved from the
   strategy row **bound to your trader**, not from the newest row, not from the row whose name
   looks right. Ours is `a5b7662e` ("MNQ") bound to trader `hoang`. Find yours:
   ```bash
   sqlite3 "file:data/data.db?mode=ro" "SELECT id,name,strategy_id FROM traders;"
   sqlite3 "file:data/data.db?mode=ro" "SELECT id,name FROM strategies;"
   ```
   Then set **on that row** — editing any other row changes nothing and looks like the knob is
   dead:
   - `day_plan.plan_mode` — ours is `strict`. **Under `strict` the decision path is closed and a
     resting arm is the only way into the market.**
   - `ai_config.risk_control.min_risk_reward_ratio` — ours is `2`. Note the path is
     **`ai_config.risk_control`**, *not* `risk_control`; reading the wrong path returns null and
     the value silently resolves to the shipped default `3.0`.
5. **Session enablement is per-strategy, not global.** ASIA / LONDON / NY each carry their own
   toggle on your strategy row. The session registry's `enabled:false` on ASIA and LONDON is only
   the *default the resolver consults* — your per-session toggle wins. If a session you expect is
   silent, check your row before checking the code.
6. **Guardrails ship OFF.** On ours the master `guardrails_enabled` is `false` **and** every limit's
   own `*_enabled` is `false` — daily loss $450, daily profit $900, `max_daily_trades` 3, contract
   clamp. They are inert twice over. If you want them, you must turn on both the master and the
   individual flag.
7. **Breakeven and trailing are OFF** despite the strategy row carrying `breakeven_enabled: true`
   and `trailing_enabled: true` — the 0B wave suspends both. Two boot lines disagree about
   trailing; the `🛑 exits: … BE=off · trail=off` line is the truthful one.
8. **The planner prompt still states a 1.0×ATR5m stop floor** while three gates enforce **1.5×**.
   Expect `⚔️ arm feasibility … min-SL gate will refuse it` warnings on plans the model authored in
   good faith. Known drift, fix not yet shipped.

---

## 10. PROVENANCE

| item | value |
|---|---|
| dev at time of writing | `b2d3826e` (boot-10 marker, RELEASE `36648655`) |
| partner main | `f6ae7597` (2026-08-23) — **not an ancestor; unrelated history** |
| pushed to partner | **NOTHING** (PARTNER REPO LAW) |
| tags pushed | **NONE** — a tag push implies a shared history that does not exist |
| written by | `nofx-c6`, branch `docs/partner-sync-0904` |
| evidence | `docs/superpowers/reports/2026-09-04-partner-sync-data/` |
