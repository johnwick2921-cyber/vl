# vl DB backup & restore (C1)

Automatic SQLite backups of `data/data.db`, taken twice daily by a **user** systemd
timer (no root, survives logout via linger).

## What runs

- **Timer:** `~/.config/systemd/user/vl-backup.timer` (before the R2 boot: `<old>-backup.timer`) → fires **05:00 & 17:30 CT**
  (host is `America/Chicago`, so those calendar times are already CT). `Persistent=true`
  runs a missed backup after a sleep/off window.
- **Service:** `vl-backup.service` (oneshot) → runs `deploy/vl-db-backup.sh` (before the R2 boot: `<old>-backup.service` → `deploy/<old>-db-backup.sh`).
- **Method:** SQLite **online backup API** via `python3` stdlib (no `sqlite3` CLI
  required; consistent even while the bot is writing), integrity-checked, then gzipped.
- **Location & retention** (under `~/vl-backups/auto/`; before the R2 boot: `~/<old>-backups/auto/`):
  - `daily/vl-YYYY-MM-DD_HHMMSS.db.gz` — every run; newest **14** kept.
  - `weekly/vl-...W##.db.gz` — one per ISO week; newest **8** kept.
- **Research ledger (data/data.db.research.db, 200+ GB live) is OPT-IN**
  (`VL_BACKUP_RESEARCH=1` (the old-name envs still read until R5)). Default run = main DB only. When opted in:
  `daily/research-*.db.gz` newest **1** kept, `weekly/research-*.db.gz` newest
  **1** kept (short on purpose — a 213 GB snapshot is a disk, not a record).
- **Disk precheck on EVERY source (main DB too):** the run REFUSES — loud
  stderr, non-zero exit, nothing written — when free space on the backup volume
  is < 2.5 × the source size, or when free-after-backup would drop below
  `VL_BACKUP_MIN_FREE_GB` (default **50**). A full disk breaks the live bot;
  a refused backup is the correct outcome. Knobs: `VL_BACKUP_RESEARCH`,
  `VL_BACKUP_MIN_FREE_GB`, `VL_KEEP_RESEARCH_DAILY` (1),
  `VL_KEEP_RESEARCH_WEEKLY` (1).

## Install / manage (all no-sudo)

```bash
bash ~/vl/deploy/install-db-backup.sh          # before the R2 boot: ~/<old>/deploy/install-db-backup.sh          # install + enable + show next run
systemctl --user start   vl-backup.service      # back up right now
systemctl --user list-timers vl-backup.timer    # when does it next run?
journalctl --user -u vl-backup.service -n 50     # last run's log
systemctl --user disable --now vl-backup.timer   # stop auto-backups
```

## Weekly proof that the backups restore (m448, release #54)

A backup nobody has restored is a hope. `deploy/backup-restore-test.sh` takes the
**newest** DB backup (by file time, across `~/vl-backups/auto/{daily,weekly}` and the
retired-prefix `auto/{daily,weekly}` dirs if they still exist), decompresses it into its
own `mktemp -d` dir, opens the copy **read-only**, and requires:

- `PRAGMA integrity_check` to return exactly `ok`;
- `traders` >= 1 and `trader_positions` >= 1 rows;
- the newest 1m bar to be no older than 120 h measured against the **backup's own time**
  (a holiday weekend is ~4 days of closed market; `VL_RESTORE_TEST_MAX_BARS_AGE_H` overrides).

It prints ONE line and exits non-zero on any failure:

```
RESTORE-TEST OK ts=1793279004 src=vl-2026-10-29_050015.db.gz integrity=ok traders=3 positions=714 bars=2013877 bars_newest=2026-10-29T10:34:00Z bars_newest_age_h=0.4 secs=214
RESTORE-TEST FAIL ts=... src=... integrity=ok traders=0 ... reason=no-traders secs=...
```

Every run also writes that line to a **new** file `~/vl-backups/restore-test/<UTC stamp>.result`
(append-only, never overwritten, never removed). The nightly data report (16:10 CT, Telegram)
reads the newest of those files and prints it as its last line: `Backup restore test: OK <time>
(<backup>)`, or `FAIL`, or `OVERDUE, weekly run missed` when the newest result is older than
8 days, or `n/a` when no result file exists (timer not installed / never ran).

Run it by hand any time (no sudo, does not touch the live DB or stop the bot):

```bash
nice -n 15 ionice -c3 bash ~/vl/deploy/backup-restore-test.sh; echo "rc=$?"
```

Disk: it refuses (a FAIL with `reason=disk-space(...)`) when the work volume has less than
8x the compressed backup or `VL_RESTORE_TEST_MIN_FREE_GB` (default 20) free. The decompressed
copy of a 580 MB backup is ~3 GB and exists only for the minutes of the run; the script removes
exactly its own `restore-test.*` dir and nothing else.

### Install the weekly timer (OWNER step - not installed by the build)

The unit files are in the repo; nothing installs them until you do (no sudo; same as the
backup timer, linger is already on):

```bash
mkdir -p ~/.config/systemd/user
install -m 0644 ~/vl/deploy/systemd-user/vl-restore-test.service ~/.config/systemd/user/
install -m 0644 ~/vl/deploy/systemd-user/vl-restore-test.timer   ~/.config/systemd/user/
chmod +x ~/vl/deploy/backup-restore-test.sh
systemctl --user daemon-reload
systemctl --user enable --now vl-restore-test.timer
systemctl --user list-timers vl-restore-test.timer --all --no-pager    # next run: Sun 12:00 CT
systemctl --user start vl-restore-test.service                         # run it right now (optional)
journalctl --user -u vl-restore-test.service -n 20 --no-pager          # the result line
systemctl --user disable --now vl-restore-test.timer                   # to stop it
```

A FAILED unit (`systemctl --user --failed`) or a `FAIL` / `OVERDUE` line in the nightly report
means the newest backup cannot be trusted: take a fresh one (`systemctl --user start
vl-backup.service`) and re-run the test before relying on any restore.

## Restore the database (rewritten release #22 — the old steps were unsafe)

**Why the old steps were wrong** (OPS review 10-14 P1-3; each reproduced on
scratch files before this rewrite):

- **The step-1 command failed.** `ls …/{vl,<old>}-*.db.gz`: bash reads `<old>`
  as a redirect, prints `old: No such file or directory`, and `BK` comes back
  EMPTY — the next step then decompresses nothing.
- **`kill -9` raced the copy.** `vl.service` has `Restart=on-failure`,
  `RestartSec=5`. A SIGKILL is a failure, so systemd relaunched the bot 5 s
  later — in the middle of a 2.5 GB `mv`/`cp`.
- **The WAL was left behind.** The live DB runs in WAL mode
  (`store/gorm.go`). After a SIGKILL its `data.db-wal` is HOT (60 MB on
  10-14). SQLite pairs files by NAME, so the old DB's frames are replayed into
  the restored file. On a scratch copy the "restored" DB read **the old rows,
  not the backup's**, and `PRAGMA integrity_check` still said `ok` — the doc's
  own check could not see it.

The procedure below stops the bot through systemd (no relaunch), moves
`data.db`, `data.db-wal` and `data.db-shm` aside **together**, and writes the
restored file with SQLite's own `.backup` (never `cp` onto a name a WAL could
pair with). Every command was run against scratch files on 2026-10-14
(`sqlite3` 3.x is installed at `/usr/bin/sqlite3`); the `systemctl` steps need
the owner's sudo.

**Before you start:** no update job running (Settings → Updates shows no job,
and `~/vl/deploy/vl-lock.sh status` shows the lock free), ideally a flat window
(the CME halt 16:00–17:00 CT). Protective orders stay at NinjaTrader while the
bot is down; the bot sees nothing until it is back.

```bash
VL=~/vl                      # the live install
DB=$VL/data/data.db
WORK=~/vl-backups/restore-$(date +%Y%m%d-%H%M%S)
mkdir -p "$WORK"

# 1. Pick a backup. The glob matches vl-<date> files only (never vl-research-*).
BK=$(ls -1 ~/vl-backups/auto/daily/vl-[0-9]*.db.gz | sort -r | head -1)
[ -n "$BK" ] || { echo "no backup found — stop"; exit 1; }
echo "restoring from $BK"

# 2. Decompress to the work dir and verify it FULLY before touching the live DB.
gunzip -c "$BK" > "$WORK/restore.db"
sqlite3 "$WORK/restore.db" "PRAGMA integrity_check;"     # → must print exactly: ok

# 3. Stop the bot through systemd. A stop is not a failure, so Restart=on-failure
#    does NOT relaunch it. Never kill -9 here: that IS a failure and relaunches in 5 s.
sudo systemctl stop vl
systemctl is-active vl                                  # → inactive
pgrep -x vl-bin && { echo "vl-bin still running — stop"; exit 1; } || echo "bot stopped"

# 4. Keep the CURRENT database, WAL included. .backup reads data.db + its -wal
#    as one consistent database, so nothing the bot wrote last is lost.
sqlite3 "$DB" ".backup '$WORK/pre-restore.db'"
sqlite3 "$WORK/pre-restore.db" "PRAGMA integrity_check;" # → ok

# 5. Move the live DB and its WAL/SHM aside TOGETHER, so no stale -wal can pair
#    with the restored file.
for f in "$DB" "$DB-wal" "$DB-shm"; do [ -e "$f" ] && mv "$f" "$WORK/"; done
ls "$DB" "$DB-wal" "$DB-shm" 2>/dev/null && { echo "files still in place — stop"; exit 1; } || echo "live files moved aside"

# 6. Write the restored database with SQLite's backup API, then check it.
sqlite3 "$WORK/restore.db" ".backup '$DB'"
sqlite3 "$DB" "PRAGMA integrity_check;"                  # → ok
ls -l "$DB"*                                            # → data.db only (no -wal / -shm yet)

# 7. Start the bot and confirm it booted on the restored DB.
sudo systemctl start vl
journalctl -u vl --since '2 min ago' --no-pager | grep -E 'Database initialized|BOOT INTEGRITY'
#   → "✅ Database initialized (GORM, SQLite)" and "BOOT INTEGRITY OK — rev <X> …"
curl -s http://127.0.0.1:8080/api/health                 # → "db":"ok"
```

**To undo the restore:** stop the bot again (step 3), move `data.db` (and any
`-wal`/`-shm`) aside, and `.backup` `$WORK/pre-restore.db` into place the same
way (step 6).

**Verification performed 2026-08-13** on `vl-2026-08-13_175507.db.gz` (the
backup itself, unchanged): decompressed, `PRAGMA quick_check = ok`, 19 tables
with a schema set **identical** to the live DB, and core tables read back
cleanly (`decision_records` 28042, `trader_positions` 516, `strategies` 9,
`exchanges` 1). The backup is fully restorable; it was the swap steps that were
unsafe, and they are the part rewritten above.

## Roll back the BINARY (and why you must re-arm `deploy/RELEASE`)

**If the boot log's `🗄 bars` key line says `migrated`, run `deploy/bars-key-rollback.sh`
BEFORE starting an older binary** (W-BARS-CONTRACT-KEY, 2026-09-18 — an older binary
cannot write the migrated `bars` table; the section below has the detail).

The DB restore above is only half a rollback. If you also go back to an earlier
binary, **`deploy/RELEASE` must be re-armed to the revision you are actually
running** — otherwise the boot assertion sees a mismatch and **REFUSES TRADING**
(`kernel/boot_integrity.go:135-138`: entries blocked, P0 alert, everything else
read-only). A rollback that skips this step comes up looking healthy and silently
takes no trades.

```bash
# 1. Pick the revision to go back to (e.g. the previous release).
cd ~/vl && git log --oneline -5   # before the R2 boot: ~/<old>
TARGET=<sha>

# 2. Build that revision. (Checkout only if you intend to move the working tree.
#    A build made in a linked git WORKTREE carries no vcs stamps and is refused —
#    see "If a cutover refuses the binary you just built" below: use a clean clone.)
git stash list && git status --porcelain      # know what you would disturb
git checkout "$TARGET" -- . 2>/dev/null || git checkout "$TARGET"
go build -o vl-bin . && echo BUILD OK

# 3. RE-ARM the expected release to MATCH the binary you just built.  ← never skip
git rev-parse HEAD > /tmp/rel
{ grep '^#' deploy/RELEASE; cat /tmp/rel; } > deploy/RELEASE.new && mv deploy/RELEASE.new deploy/RELEASE

# 4. Relaunch and CONFIRM the assertion passed.
kill -9 "$(pgrep -f vl-bin)"                # systemd Restart=on-failure relaunches
journalctl -u vl --since '2 min ago' | grep 'BOOT INTEGRITY'
#   → must read "BOOT INTEGRITY OK — rev <X> · expected <X> · goldens PASS"
#   → "TRADING REFUSED" means step 3 was missed or the goldens drifted
```

To disable the assertion deliberately (e.g. while bisecting), leave the value in
`deploy/RELEASE` **blank** — it then logs the revision and never refuses.

Rolling the binary back across a schema migration also needs the matching DB
snapshot from above; restore the DB **first**, then the binary, then re-arm.

## Roll back the bars CONTRACT-KEY migration (W-BARS-CONTRACT-KEY, 2026-09-18)

The first boot of a binary at/after this wave moves `bars` from
`PRIMARY KEY (symbol, tf, open_time_ms)` to `(symbol, tf, contract, open_time_ms)`.
It is a guarded write, done by code at boot (`store/bar_contract_key.go`):

1. a whole-database backup **before anything else** — `VACUUM INTO`
   `~/vl-backups/pre-bars-key-<YYYYMMDD-HHMMSS>.db` (before the R2 boot: `~/<old>-backups/…`), verified by `bars` row count;
   if it cannot be written the migration is **refused** and the bot runs on the old key
   (boot line `🗄 bars: migration FAILED — …; old table intact`);
2. `bars_v2` created on the new key, `INSERT … SELECT` of every row, then
   `bars` → `bars_pre_contract_key_<YYYY-MM-DD>` and `bars_v2` → `bars`, in **one
   transaction** (any error rolls back; the old table is never touched);
3. boot line `🗄 bars: key migrated to (symbol,tf,contract,open_time_ms) — rows=<n>
   backup=<path> old_table=<name>` (values read back). Later boots print
   `🗄 bars: key=(symbol,tf,contract,open_time_ms) (migrated <date>) rows=<n>` and do nothing.

Measured on a copy of the live DB (1,906,992 rows, 1.39 GB) 2026-09-18 01:00 CT:
migration 7.7 s including the backup; second boot 0.33 s.

**A PRE-MIGRATION BINARY MUST NOT BOOT ON THE MIGRATED TABLE** (tested,
`store/bar_contract_key_test.go TestBarsKeyOldBinaryStatementsOnTheMigratedTable`):
its bar upsert names `ON CONFLICT(symbol, tf, open_time_ms)`, which is no longer a
unique constraint, and SQLite refuses every write (`ON CONFLICT clause does not
match any PRIMARY KEY or UNIQUE constraint`) — reads work, **persistence dies
silently except for a `bars: persist … failed` WARN per batch**. Its Migrate does
NOT run its destructive 2026-08-27 dedupe block (the name-only unique-index check
is satisfied by the renamed table's index, which the migration keeps on purpose).

### Option A — rename back (keeps the DB, loses bars written after the migration)

Scripted: `deploy/bars-key-rollback.sh [--force] [--db PATH]` (rc 0 done · rc 4 nothing
to do · rc 2 bot running · rc 3 unsafe state) — discovers the old table, refuses while
`vl-bin` runs unless `--force`, takes a `VACUUM INTO` backup, runs
`deploy/bars-key-rollback.sql` (the rename pair in one transaction, new-shape indexes
dropped from the parked copy so a later re-migration can recreate them), prints the counts.
The CTO's unattended cutover calls it on a binary rollback. By hand:

```bash
# 0. Stop the bot (nothing may write during the swap). NOT kill -9: that is a
#    failure, and Restart=on-failure relaunches the bot 5 s later, mid-swap.
sudo systemctl stop vl && systemctl is-active vl         # → inactive
#    … steps 1-4, then install the old binary, re-arm deploy/RELEASE, and
#    sudo systemctl start vl
# 1. Which tables exist?
sqlite3 ~/vl/data/data.db "SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'bars%';"
# 2. Swap: the migrated table aside, the pre-migration table back under its name.
#    The old table still carries idx_bars_sym_tf_time_unique / idx_bars_contract / idx_bars_source,
#    so the old binary's Migrate finds its unique index and is a no-op.
OLD=$(sqlite3 ~/vl/data/data.db "SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'bars_pre_contract_key_%' ORDER BY name DESC LIMIT 1")
sqlite3 ~/vl/data/data.db "BEGIN; ALTER TABLE bars RENAME TO bars_contract_key_v2; ALTER TABLE \"$OLD\" RENAME TO bars; COMMIT;"
# 3. (optional) carry bars written AFTER the migration back onto the old key.
#    On the old key a minute held by two contracts collapses to ONE row (first wins) — this is the
#    exact loss the wave removed; accept it or skip this step.
sqlite3 ~/vl/data/data.db "INSERT OR IGNORE INTO bars(symbol,tf,open_time_ms,o,h,l,c,v,convention,contract,source)
  SELECT symbol,tf,open_time_ms,o,h,l,c,v,convention,contract,source FROM bars_contract_key_v2;"
# 4. Verify, then boot the old binary (re-arm deploy/RELEASE — see the binary rollback above).
sqlite3 ~/vl/data/data.db "SELECT name FROM pragma_table_info('bars') WHERE pk>0 ORDER BY pk;"   # → symbol tf open_time_ms
sqlite3 ~/vl/data/data.db "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND tbl_name='bars' AND name='idx_bars_sym_tf_time_unique';"  # → 1
```

To re-migrate later, the new binary refuses while a `bars_pre_contract_key_<today>`
table exists (it never overwrites a backup table): rename or drop
`bars_contract_key_v2` and the stale `bars_pre_contract_key_*` first, deliberately.

### Option B — restore the whole-database backup (loses EVERYTHING written after it)

```bash
BK=$(ls -1 ~/vl-backups/pre-bars-key-*.db | sort -r | head -1)
sqlite3 "$BK" "PRAGMA integrity_check;"   # → ok
# Then follow "Restore the database" above from step 3, with restore.db = "$BK"
# (stop through systemd, keep the current DB with .backup, move data.db AND its
# -wal/-shm aside together, .backup "$BK" into place) — never kill -9 + cp.
# Then the binary rollback + RELEASE re-arm above, and start the bot.
```

Prefer A: it is bars-only. B rewinds decision_records, plans, positions — every table.

### Probes (also CLASS entry in docs/superpowers/AUDIT-CHECKLIST.md)

```sql
-- which key is live
SELECT name FROM pragma_table_info('bars') WHERE pk>0 ORDER BY pk;
-- roll overlaps: minutes held by two contracts (expected >0 after a roll on the new key; always 0 on the old key)
SELECT symbol,tf,open_time_ms,COUNT(DISTINCT contract) FROM bars GROUP BY 1,2,3 HAVING COUNT(*)>1;
-- the old-key loss, measured on the backup table: rows of the newer contract that the old key had NO slot for
SELECT COUNT(*) FROM bars b WHERE b.contract='MNQ 12-26' AND b.symbol='MNQ' AND b.tf='1m'
  AND EXISTS (SELECT 1 FROM bars a WHERE a.symbol=b.symbol AND a.tf=b.tf AND a.open_time_ms=b.open_time_ms AND a.contract='MNQ 09-26');
```

## Notes

- Backups are **gzip'd** (10-13: a 2.5 GB DB → 525 MB). Always `gunzip` before opening.
- The `.backup` API copies a transactionally-consistent snapshot, so a backup taken
  mid-trade is still valid — no torn writes.
- These automated backups are **separate** from the ad-hoc `~/vl-backups/<name>/`
  guarded-write snapshots; both live under `~/vl-backups/` (before the R2 boot: `~/<old>-backups/`).

## Manual boot after W-ONE-BUTTON M4 — the guide rev is a BUILD INPUT

**What changed and why the old preflight silently stops working.** Until this
wave `web/src/guide/types.ts` carried a literal:

```
export const GUIDE_BUILT_REV = '<40-hex sha>'
```

and the boot procedure grepped that line to check the guide matched the binary.
That literal **no longer exists**. The rev now arrives at build time, so the old
grep finds nothing — and "finds nothing" reads exactly like "nothing to check".
It is not a failure; it is a check that has quietly stopped checking.

**Every manual boot now does two things instead:**

```
# 1. BUILD with the rev of the commit you are booting
cd web && VITE_GUIDE_BUILT_REV=<40-hex merge sha> npm run build

# 2. VERIFY the served bundle actually carries it
grep -rq -- '<40-hex merge sha>' web/dist/assets/*.js \
  || { echo 'the dist does not carry the rev — do NOT boot'; exit 1; }
```

A production build with the variable missing or not 40-hex **fails at build
time** rather than shipping a guide whose claim about the running binary cannot
be checked. `deploy/cutover.sh` performs step 2 as a preflight and refuses the
cutover if it fails; `.github/workflows/release.yml` does exactly the same for a
tagged release, and additionally proves the negative — `VITE_GUIDE_BUILT_REV=
npm run build` must FAIL before the real build runs.

**Out-of-repo script:** the CTO's `~/vl-backups/cutover-auto-rollback-v3.sh`
still greps the old literal. It must be edited the same way before the next
boot, or it will report a green preflight for a check that no longer exists.
`deploy/cutover.sh` is its in-git successor and already does this.

## Rollback copies

`deploy/cutover.sh` keeps both halves of the previous release, named so they
cannot collide (R-o — the old script reused `vl-bin.old.<rev>`, so two
cutovers at the same rev overwrote the only way back):

```
vl-bin.old.<rev12>.<YYYYmmdd-HHMMSS>
web/dist.old.<rev12>.<YYYYmmdd-HHMMSS>
```

A rollback restores **both**, and restores `web/dist` atomically by moving
directories rather than copying into a live one — a half-copied dist serves a
mix of old and new assets, which reads as a UI bug rather than a failed cutover.

## If a cutover refuses the binary you just built

Two different refusals, two different cures — read WHICH one you got.

**"carries NO vcs stamps at all"** — the binary is fine; where it was BUILT is
not. Go does not stamp VCS information into a build made from a linked git
worktree, and every lane works in one. `-buildvcs=true` does not help: it exits
0 and stamps nothing. Build from a clean clone (or the main tree) at the same
commit and the stamps appear:

    git clone --no-local <repo> /tmp/build && cd /tmp/build
    git checkout <sha> && go build -o /tmp/vl-bin .
    go version -m /tmp/vl-bin | grep vcs.      # revision + modified=false

**"is stamped, but with a DIFFERENT revision"** — this really is the wrong
binary for the sha you named. Check the sha, not the build location.

Do not weaken either check to get past it. The identity proof is the only thing
standing between a cutover and installing a binary nobody can identify later.
