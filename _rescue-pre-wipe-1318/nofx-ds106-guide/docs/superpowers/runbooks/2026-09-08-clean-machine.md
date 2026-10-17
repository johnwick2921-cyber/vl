names rewritten to vl on 2026-09-30 (VL rename)
# CLEAN-MACHINE READINESS RUNBOOK — second machine, vl

**Dispatch:** CLEAN-MACHINE READINESS · owner hoang · 2026-09-08 · read-only on
the running system; this document and its checklist are the whole deliverable.

**Purpose.** Establish, by measurement, everything a SECOND machine needs to run
this system against NinjaTrader SIM in parallel with the primary, and name what
will silently fail on it. Nothing here was installed, copied, changed, or booted
on either machine. It is not a migration and it moves nothing.

**Measured basis (all three legs agree on machine A, 2026-09-08 ~16:00 CT):**

| source | measured value |
|---|---|
| `GET /api/health` (curl, loopback) | `{"revision":"f8bc7044cc44","status":"ok"}` |
| `/proc/3566770/exe` | symlink → `/home/hoang/vl/vl-bin` |
| `go version -m` of that binary | `mod vl v0.0.0-20260908195007-f8bc7044cc44` |
| `journalctl -u vl` boot line | `🔐 BOOT INTEGRITY OK — rev f8bc7044cc44 · built 2026-09-08T19:50:07Z · expected f8bc7044 · goldens PASS` |
| `deploy/RELEASE` (main tree) | `f8bc7044` |
| live far-side proof | `🔌 nt8 addon: build_id=2026-09-07-h1 expected=2026-09-07-h1 match=yes` (`trader/auto_trader.go:44`) |

Worktree for this wave: `~/vl-cleanmachine`, cut from `origin/dev` =
`59af58fd54a74f1422296ac651a0c687c2072d33` ("docs(scenario-economics)…",
2026-09-08 15:41:24 -0500), locked, removed at the end. Branch
`docs/clean-machine-readiness`, claimed
`cleanmachine-fc70e2d7/root[unlisted]`, ls-remote sha `d5a686cbe4c82bb67955a87a86704c9b71194738`.

**Verified by running, on this machine, in the worktree:** `go build ./...`
(go1.25.3 linux/amd64, clean in ~7 s). Every other command below is quoted from
a repo file or from output quoted in this document; commands I could not execute
are marked **UNTESTED** and say so.

The live rev f8bc7044 is NOT the dev tip (59af58fd): `deploy/RELEASE` was updated
by the 15:06:00 CT marker commit `cd5b9a6b` after the running binary was built.
This is normal: `deploy/RELEASE` names the rev of the last DEPLOYED binary, and
the boot gate compares that STRING against the binary's embedded `vcs.revision`
by prefix (`kernel/boot_integrity.go:84-95`) — `f8bc7044` prefixes `f8bc7044cc44`,
so it matches. Git ancestry is not consulted. A second machine must pin to a
state where its binary rev prefixes its checkout's `deploy/RELEASE` (see C7).

---

## C1 — THE HOST: what the Go/JS side needs

| # | requirement | evidence (file:line, quoted) |
|---|---|---|
| 1 | **Go toolchain ≥ 1.25.3** — `go.mod:3` reads `go 1.25.3`. Machine A builds with `go1.25.3 linux/amd64` (measured, `go version`). | `go.mod:3` |
| 2 | CI pins DISAGREE and are all older: `pr-checks.yml:182` + `pr-checks-run.yml:32` = go `1.21`, `test.yml:22` = `1.23`, `pr-go-test-coverage.yml:32` = `1.25`, `security.yml:25-29` = `go-version-file: go.mod` + `1.25.3`. `go.mod` is what the local build actually obeys. A 1.21 toolchain WILL refuse the module. | `.github/workflows/*.yml` quoted lines |
| 3 | **CGO + a C compiler (gcc) are required.** The GORM store opens SQLite via `gorm.io/driver/sqlite` (`store/gorm.go:11`), which links `github.com/mattn/go-sqlite3` (CGO). The running binary is **dynamically linked** (`file /home/hoang/vl/vl-bin` → "ELF … dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2"). `modernc.org/sqlite v1.40.0` (`go.mod:31`, pure-Go) is registered only as the legacy `database/sql` driver name in `store/driver.go:16` (`_ "modernc.org/sqlite"`) for the deprecated `DBDriver` path — it is NOT what the live GORM path uses. No `CGO_ENABLED` override exists anywhere in the repo (grep = 0 hits). On Ubuntu/WSL2: `sudo apt install build-essential` supplies gcc. | `store/gorm.go:11`; `store/driver.go:16-17`; `file` output quoted |
| 4 | **OS/WSL assumptions.** WSL2 with **systemd enabled** — `docs/AUTOSTART.md` §0 requires `/etc/wsl.conf` `[boot] systemd=true` and `systemctl is-system-running` = running/degraded. **Mirrored networking mode** is the documented working shape for the Windows browser to reach `127.0.0.1` services (comment in `web/vite.config.ts:8-13`; AGENTS.md gotchas). Host timezone America/Chicago is ASSUMED by the two user timers ("The host runs in America/Chicago, so these calendar times ARE CT", `deploy/systemd-user/vl-backup.timer:6-7`). **`powershell.exe` interop** (WSL → Windows) is invoked by the clock guard for its Windows-clock cross-check — optional; the guard skips it when absent (`deploy/vl-clock-guard.sh`, header + win-drift block). The NT8 path shape `/mnt/c/Users/<u>/...` is referenced in `trader/ninjatrader/trader.go:26` (legacy CSV) and in the AGENTS.md HARD RULE. | `docs/AUTOSTART.md`; `web/vite.config.ts:8-13`; `deploy/systemd-user/vl-backup.timer:6-7` |
| 5 | **node + npm** — needed for two things: (a) the `vl-web.service` Vite dev server (`ExecStart=__NODE_DIR__/npm run dev`, `deploy/vl-web.service`; installer refuses to install without node, `deploy/install-autostart.sh:84-91`); (b) the production bundle `npm run build` → `web/dist`, which the **Go binary itself serves** from the working directory (`api/ui_serving.go:34`, `const UIDistDir = "web/dist"`). **No engines pin exists** in `web/package.json` (grep for `"engines"` = 0 hits). CI node pins disagree: 18 (`pr-checks.yml:229`, `pr-checks-run.yml:110`), 20 (`test.yml:46`), 22 (`security.yml:40`). Machine A runs node v22.22.1 / npm 10.9.4 (measured, informational). UNKNOWN which node version is authoritative; 22 is what the live machine uses. | `web/package.json`; CI quoted lines; `deploy/vl-web.service`; `api/ui_serving.go:34` |
| 6 | **Tools the deploy scripts invoke:** `git` (clone, everything), `sudo` (`install-autostart.sh:21-23`, `install-journald.sh`), `systemctl` (all installers), `python3` stdlib `sqlite3` module + `gzip` (`deploy/vl-db-backup.sh` backup block), `openssl` (key generation, `.env.example`), `curl` (verification), **`jq` only in `start.sh:284`** (the docker-management helper, which is NOT part of the native WSL path). `sqlite3` CLI is used by `deploy/cutover.sh` (the live cutover; the retired one-shot `leveltruth-cutover.sh` was superseded by it and the updater, Z8) — not by the normal run path. | quoted lines |
| 7 | **Docker is NOT required.** `./vl-bin` runs locally, SQLite at `data/data.db` (AGENTS.md Build & test). The Dockerfiles exist for Railway/other hosts and are out of scope for a WSL2 second machine. | AGENTS.md; `README.md:222` (`go build -o vl && ./vl`) |

The canonical manual build/run lines: `go build -o vl-bin .` (`deploy/install-autostart.sh:45-47`), `README.md:222`, `cd web && npm install && npm run dev` (`README.md:223`).

## C2 — THE WINDOWS SIDE: NinjaTrader 8 AddOn

**AddOn files** (repo `ninjascript/`, all shipped together):
`VLTraderTCPClient.cs` (the AddOn, 157 KB), `VLBarsSubscriptionManager.cs` (bars
subscription isolation), `VLContractResolver.cs` (expiry → instrument), plus
spec docs `vltrader_tcp_PROTOCOL.md`, `VLContractResolver_VERIFY.md`,
`vltrader_tcp_README.md`.

**Where NT8 loads them from — NOT the repo.** NT8 compiles NinjaScript ONLY from
`C:\Users\<windows-user>\Documents\NinjaTrader 8\bin\Custom\AddOns\` (AGENTS.md
HARD RULE — "a lane lost a night to exactly that"). The AddOn's own header names
it: `VLTraderTCPClient.cs:30` `/// Documents\NinjaTrader 8\bin\Custom\AddOns\.
Lifecycle is driven by…`. The WSL view of that folder is
`/mnt/c/Users/<u>/Documents/NinjaTrader 8/bin/Custom/AddOns/`.

**Deploy procedure for every C# change** (AGENTS.md HARD RULE, quoted):
1. `cp` repo `ninjascript/*.cs` → the Documents `AddOns` folder;
2. F5 compile inside NT8;
3. **full NT8 restart** — AddOns do NOT hot-reload.
Skipping any step = NT8 runs the old binary. This is the single biggest NT8
gotcha and it reproduces on the second machine.

**Build id.** `VL_BUILD_ID = "2026-09-07-h1"` (`ninjascript/VLTraderTCPClient.cs:55`).
It rides the `hello` handshake — "identifiable from the FIRST frame" — and every
`heartbeat` ("has carried it since E7") per `vltrader_tcp_PROTOCOL.md:383`;
the three payload sites in the AddOn are `VLTraderTCPClient.cs:1491,1638,2685`. Go receives it in `HeartbeatPayload.BuildID`
(`provider/ninjatrader/tcp_framing.go:115-119`) and stores the last one received
(`provider/ninjatrader/tcp_server.go:1733-1738`, exposed via `FarSideBuildID()`
at `:519-521`).

**How a build id is proven received** — by a frame, never from our own source:
`FarSideProven` returns false for `""` ("capability is proven by receipt, not
assumed", `provider/ninjatrader/tcp_framing.go:263-266`). The human line is
`nt8 addon: build_id=<received> expected=<floor> match=yes|NO` built at
`provider/ninjatrader/order_snapshot.go:217-228`. **Live on machine A right
now:** `🔌 nt8 addon: build_id=2026-09-07-h1 expected=2026-09-07-h1 match=yes`
(`trader/auto_trader.go:44`, journal 15:17:31).

**The floors the build id gates** (all in `provider/ninjatrader/tcp_framing.go`):
- `MinAddonBuildStopSlot = "2026-09-05-g2"` (:246) — gates `stop_entry` placement
  (`trader/ninjatrader/tcp_trader.go:509-511`); `FarSideBuildE7 = "2026-08-30-e7"`
  (:232) is retained as history and "gates nothing".
- `MinAddonBuildProtectiveStop = "2026-09-07-h1"` (:257) — gates
  `place_protective_stop` (`tcp_trader.go:802`).
- **The date prefix decides** — bytewise compare, suffixes not zero-padded
  (:239-244): every future minimum must advance the ISO date, never only the suffix.
A second machine that copies the repo `.cs` but skips F5/restart sends the OLD
build id and gets loud `match=NO` + capability refusals — see D4.

**AddOn constants** (`VLTraderTCPClient.cs:37-56`): `GO_SERVER_HOST = "127.0.0.1"`,
`GO_SERVER_PORT = 36974` (NOT NT's ATI 36973), `HEARTBEAT_INTERVAL_MS = 30000`,
`ORDER_SNAPSHOT_INTERVAL_MS = 30000`, `RECONNECT_INTERVAL_MS = 5000`,
`STALE_SIGNAL_AGE_SECONDS = 60`, `PROTOCOL_VERSION = 3`, `MAX_FRAME_BYTES = 1<<20`.
Go side binds `TCPListenAddr = "127.0.0.1:36974"` (`provider/ninjatrader/tcp_server.go:34`).
The bot is the TCP **server** — it can be up for hours before NT8 opens; the AddOn
connects within ~5 s (`docs/AUTOSTART.md` "NT8 closed is fine").

**What the AddOn's user-data trace names:** the AddOn logs through
NinjaTrader's `Log()` (NT8 Log tab + `Documents\NinjaTrader 8\trace\*.txt`).
The repo does not hardcode the trace path (grep for trace paths in
`ninjascript/*.cs` = 0 hits); the folder shape is established from the AGENTS.md
HARD RULE (AddOns path) and NT8 conventions, not from the AddOn source. Named in
D5 accordingly.

## C3 — CONFIGURATION: every environment key, by name only

`.env` is loaded by `godotenv.Load()` at `main.go:38` from the process working
directory — the unit's `WorkingDirectory` (`deploy/vl.service` comment:
"WorkingDirectory matters: .env (godotenv) + data/data.db resolve relative to
it"). Services never read `~/.bashrc`. On machine A, `.env` holds 27 keys (listed
below BY NAME ONLY, never valued). The code reads 162 distinct `os.Getenv` keys
(machine A `.env` sets only 27 of them).

**Machine A `.env` keys (names only):** `AI_HTTP_TIMEOUT_SECONDS AI_MAX_RETRIES
AI_MAX_TOKENS ARMED_TEST_SEAM
DATABENTO_API_KEY DATABENTO_DATASET DATA_ENCRYPTION_KEY
DB_PATH DB_TYPE EOD_FLAT_LIMIT_TICKS EOD_FLAT_MARKET_AFTER_SEC HTF_VETO_MODE
JWT_SECRET NINJATRADER_DATA_DIR VL_BACKEND_PORT VL_FRONTEND_PORT
VL_TIMEZONE NT_EXTRA_SYMBOLS NT_RUNTIME_SYMBOLS NT_TRANSPORT RSA_PRIVATE_KEY
STOP_ENTRY_SEAM TRADING_MODE TRANSPORT_ENCRYPTION`

### REQUIRED TO BOOT (process refuses to start without them)

| key | reads it | what happens if absent |
|---|---|---|
| `RSA_PRIVATE_KEY` | `crypto/crypto.go:30` (const) + `:77-81` | `NewCryptoService` returns error → `logger.Fatalf` at `main.go:50-54`. **Boot-fatal.** PEM, newlines as `\n` (`.env.example`); generate `openssl genrsa 2048`. |
| `DATA_ENCRYPTION_KEY` | `crypto/crypto.go:29` + `:91-94` | same — **boot-fatal**. Base64 32-byte AES key; generate `openssl rand -base64 32` (`.env.example`). It is the key that encrypts API keys/secrets in the DB (`store/ai_model.go:28`). |
| `JWT_SECRET` | `config/config.go:117-124` | **boots anyway** with `default-jwt-secret-change-in-production` + WARN. Required to be SET (not merely present) before any non-loopback exposure; `api/` auth is HS256 against it (`auth.SetJWTSecret`, `main.go:136`). Generate `openssl rand -base64 64` (AGENTS.md security gate). |
| `DB_PATH` | `config/config.go:200` | default `data/data.db` relative to cwd (`config/config.go:106-113`). No key needed if the default layout is used. |
| `DB_TYPE` | `config/config.go:194` | default `sqlite` (`:109`). `postgres` switches GORM to the postgres driver (`store/gorm.go:9`); then `DB_HOST/PORT/USER/PASSWORD/NAME/SSLMODE` (`config/config.go:203-220`) apply. The futures path is SQLite-only in practice. |

### REQUIRED TO TRADE

| key / DB row | reads it | what happens if absent |
|---|---|---|
| `NT_TRANSPORT=tcp` | `trader/ninjatrader/transport.go:25` (`TransportEnvVar`) + `:100-134` | **silent wrong path**: the transport falls back to the DEPRECATED CSV bridge (`transport.go:3-4`; `.env.example` NT_TRANSPORT block: "a value set only there silently falls back to CSV"). The installer appends `NT_TRANSPORT=tcp` if missing (`deploy/install-autostart.sh:76-80`) — a manual clone+run does not get this. |
| `TRADING_MODE=futures` | `config/config.go:172` | default `crypto` — the bot boots into the wrong universe; MNQ path never activates. |
| exchange row `nt_data_dir` | `trader/auto_trader.go:678-680` | `ninjatrader requires NinjaTraderDataDir …; the NINJATRADER_DATA_DIR env is not read by the live path` — the trader FAILS TO LOAD. Note: on the TCP transport the value is only handed to the legacy CSV writer (`trader/ninjatrader/trader.go:50-51`) — any non-empty path satisfies the gate on TCP. |
| exchange row `nt_instrument_name` | `manager/trader_manager.go:681` (`NinjaTraderSymbol = exchangeCfg.NTInstrumentName`) | symbol = `MNQ`; empty symbol means no real instrument. |
| trader row `account` | `store/trader.go:48-54` (bound account; empty = GATED), `manager/trader_manager.go:684`, enforced `trader/ninjatrader/tcp_trader.go:334-339` (unbound → refuse) | **trader gated — cannot place entries** until an account is selected and persisted (`handleSelectAccount` → `UpdateAccount`, `store/trader.go:202-209`). |
| `NT_ALLOWED_ACCOUNTS` | `config/config.go:167-171`; enforced `tcp_trader.go:312-320` and `api/handler_account.go:166` | unset = allow-list off (any SIM account passes the SIM check). With a list set, an account NOT on it is refused — including on machine B if the list names machine A's account. |
| `ai_models` row: provider `deepseek`, `api_key`, base URL + model | `store/ai_model.go:28` (encrypted api_key); `mcp/client.go:425-426` ("AI API key not set, please call SetAPIKey first"); defaults `DefaultDeepSeekBaseURL = "https://api.deepseek.com"`, `DefaultDeepSeekModel = "deepseek-v4-pro"` (`mcp/providers.go:19-20`) | no planner/executor decisions — the loop runs but every AI call fails. `DEEPSEEK_API_KEY` env is NOT the trading key: its only reader is `telegram/bot.go` (the Telegram bot's own AI). The trading key lives in the DB row, encrypted under `DATA_ENCRYPTION_KEY`. |

### OPTIONAL knobs (set explicitly on machine B where behavior matters)

AI tuning: `AI_MAX_TOKENS`, `AI_HTTP_TIMEOUT_SECONDS` (canonical timeout, default
300), `AI_MAX_RETRIES`, `AI_TEMPERATURE`, `AI_TOP_P`, `AI_PLAN_MAX_TOKENS`,
`AI_PLAN_STREAM_IDLE_SECS`, `AI_PLAN_STREAM_TOTAL_DEADLINE_SECS`,
`AI_EXEC_REASONING`/`AI_PLAN_REASONING` (latency routing, `config/config.go:183-184`),
`AI_RETRY_BACKOFF_SECONDS`, `AI_REPLANNER_MAX_TOKENS`,
`AI_TASKSTATE_*_MAX_TOKENS` — all read in `mcp/config.go`; the boot block prints
which are unset (`main.go:172-187`, live WARN quoted above). Gates and law:
`MIN_SL_ATR_MULT` (`kernel/min_sl.go`), `HTF_VETO_MODE` (`kernel/htf_veto.go`),
`HTF_VETO_TF`, `STOP_ENTRY_SEAM` (`kernel/entry_law.go` — gates stop entries;
only meaningful once the AddOn build ≥ `MinAddonBuildStopSlot`), `ARMED_TEST_SEAM`
(`trader/armed_executor.go`), `EOD_FLAT_LIMIT_TICKS` / `EOD_FLAT_MARKET_AFTER_SEC`
(dormant 0/0, `config/config.go:178-179`), `LEVEL_ROLE_MAP` (`main.go:318`),
`VL_CALENDAR_STATIC` (else repo-shipped `calendar_static_t1.json`,
`trader/auto_trader_calendar.go:112-119`), `VL_EXPECTED_REVISION` (boot-expectation
override, `kernel/boot_integrity.go:87`), `SANDBOX_MODE` (`config/config.go:197` —
synthetic bars + canned planner, no live trading; `main.go:530-534`),
`ALLOW_ACCOUNT_RESET` (`api/handler_user.go:230-240`), `TRANSPORT_ENCRYPTION`
(`config/config.go:146-149`), `API_SERVER_HOST`/`API_SERVER_PORT` (bind control;
default loopback 127.0.0.1:8080, `config/config.go:103-104,127-142`),
`NT_EXTRA_SYMBOLS`
(`trader/ninjatrader/transport.go:132-134`), `NT_RUNTIME_SYMBOLS`
(`api/handler_debug.go:14-18`), `EXPERIENCE_IMPROVEMENT` (`config/config.go:151-155`).

### LEGACY / DEAD (present in `.env` or example but with no live reader)

- `NINJATRADER_DATA_DIR` — "loads removed — zero live readers" (`config/config.go:163`); only `cmd/nq_smoke/main.go:59-61` reads it (the smoke harness).
- `DATABENTO_API_KEY`, `DATABENTO_DATASET` — loaded (`config/config.go:162`) but the live data source is NT8; only the optional `nq_smoke databento|resolver` sub-smokes use them.
- `VL_BACKEND_PORT`, `VL_FRONTEND_PORT`, `VL_TIMEZONE` — no Go reader; they appear only in UI help text (`web/src/i18n/translations.ts:802`) and `start.sh` (docker helper). The real port keys are `API_SERVER_PORT` and the vite config.
- `RISK_MAX_NOTIONAL_USD` — "loaded but enforced nowhere" (`config/config.go:186-189`); `RISK_MAX_CONTRACTS_PER_ORDER` — removed, zero readers (`config/config.go:190-192` comment).
- `DB_*` postgres keys — inert when `DB_TYPE=sqlite`.
- `.env.example` contains BOTH `DB_TYPE=postgres` and `DB_TYPE=sqlite` blocks; godotenv keeps the LAST occurrence, and in the shipped example sqlite is later (lines ~84-86) — copy-then-edit, don't uncomment both.

### Where a value resolves from when two sources disagree (A11)

- `.env` (godotenv, `main.go:38`) sets process env only; `config.Init()` then reads process env once (`config/config.go:98`). Nothing later re-reads `.env`.
- Contract count clamp: strategy row `max_contracts_per_order` (`store/strategy.go:1732`) wins; venue default is 2 (`trader/auto_trader_orders.go:25,49-60`); arms are hardcoded quantity 1 (`trader/armed_executor.go:1788-1789`).
- Boot expectation: `VL_EXPECTED_REVISION` env wins over the file `deploy/RELEASE` (`kernel/boot_integrity.go:84-95`).
- Account gate: trader row `account` is the binding; `NT_ALLOWED_ACCOUNTS` is an additional rail; the C#-reported account list + `Account.Simulation` flag is the ground truth (`tcp_trader.go:296-311`).

### Per-machine keys — MUST DIFFER on machine B (identity, not shared)

`NT_ALLOWED_ACCOUNTS` (machine B's own SIM account name), trader row `account`
(B's account), exchange row `nt_data_dir` (B's NT8 data path), `JWT_SECRET`
(separate user database ⇒ separate sessions), `DATA_ENCRYPTION_KEY` +
`RSA_PRIVATE_KEY` (fresh DB ⇒ generate fresh; see C4). Paths defaulted in
scripts that hardcode `/home/hoang/vl` (see C5/D3) must point at B's clone.
`DB_PATH` is per-clone by default (`data/data.db` relative).

## C4 — THE DATABASE

**What creates it:** `main.go:64-67` creates the `data/` dir; `store.InitGorm`
opens `sqlite.Open(dbPath)` — GORM creates the file on first boot
(`store/gorm.go:20-25`). Connection pragmas: WAL, `synchronous=FULL`,
`busy_timeout=5000`, `foreign_keys=ON`, pool `SetMaxOpenConns(4)`
(`store/gorm.go:40-60`).

**What migrates it:** `store.NewWithConfig` → `initTables()` (`store/store.go:143-247`)
AutoMigrates ~30 sub-stores (users, ai_models, exchanges, traders, decisions,
positions, strategies, equity, orders, grid, telegram, ai_charge, plans,
level_state, session_profiles, calendar_slices, digests, owner_levels, alerts,
watch, log_events, plan_qa, matched_random, armed_orders, ab_confirm_log,
plan_lifecycle_log, nt8_order_snapshots, trade_excursions …) plus raw-DDL
`system_config` (`store/store.go:147-153`).

**Does a fresh empty DB boot?** Yes, and it is the supported path for machine B:
- `initDefaultData()` seeds NOTHING — all three seeders are explicit no-ops
  (`store/ai_model.go:59-62`, `store/exchange.go:182-185`, `store/strategy.go:1796-1799`).
- With zero traders, `main.go:273-277` logs "(No trader configurations, please
  create via Web interface)" and the process runs.
- Registration is allowed only while the users table is empty — first-time setup
  (`api/handler_user.go:52-55`).
- The boot-time backfills in `main.go` (`BackfillEntryConfidence`,
  `CorrectHistoricalPnL`, `ConvergePlanLinkSentinel`, `StampSeamRowsExcluded`,
  `BackfillPnlCorrectedAll`, acceptance-rule migration, E8 backfill, adherence
  regrade, wave-A record migration) are all idempotent/WHERE-scoped repairs —
  each no-ops on an empty DB. (Verified by reading each call site in `main.go`;
  not executed against a fresh DB — a live fresh-boot test is UNTESTED and listed
  in D5.)
- `system_config` gets one row on first boot: `installation_id` (anonymous
  telemetry, `main.go:520-542`).
- Boot integrity still applies: a fresh machine must build a binary whose rev
  matches its checkout's `deploy/RELEASE`, or set `VL_EXPECTED_REVISION`, or
  trading is refused (see C7).

**What machine B must NOT copy from machine A — the entire `data/data.db`.**
Per-table reasons it would make B believe A's book is its own, or just break:

| table / row | why copying it is wrong |
|---|---|
| `trader_positions` | B's ledger would show A's OPEN/CLOSED positions and P&L — "empty book that reads flat" replaced by a phantom book (D4). |
| `armed_orders` | resting arms from A would look live to B's executor (`store/armed_orders.go`). |
| `accepted_risk`, `ab_confirm_log`, `trade_excursions` | A's risk acceptance and counterfactual rows become B's evidence (`store/accepted_risk.go:73`, `store/ab_confirm.go`). |
| `nt8_order_snapshots` | rows carry `Account` + `BuildID` (`main.go:244-250`; `store/nt8_order_snapshot.go`) — A's broker book would be read as B's (F12 ledger). |
| `system_config.installation_id` | anonymous telemetry identity (`main.go:520-542`) — not harmful, but it IS machine A's identity. |
| `users` | A's owner account + password hash would exist on B; registration is blocked when any user exists (`api/handler_user.go:52-55`), so copying the DB LOCKS OUT the first-user setup flow. |
| `ai_models` / `exchanges` | `api_key` is `crypto.EncryptedString` (`store/ai_model.go:28`, `store/exchange.go` fields) encrypted under A's `DATA_ENCRYPTION_KEY` (`crypto/crypto.go:91-94`) — under B's fresh key, decryption fails → broken/blank credentials that LOOK configured. |
| `traders` | `account` column (`store/trader.go:48-54`) names A's SIM account; `isAccountTradeable` refuses it on B's NT8 (`tcp_trader.go:296-311`) — fail-safe but only after you copy it. |
| `strategies`, `decisions`, `equity`, `log_events`, `calendar_slices`, `bars`, digests, alerts, level_state | A's history mis-presented as B's; calendar slices frozen past dates (`docs` note in 2026-08-24 calendar fix) plus A's events. |

**Fresh start is fully supported; there is no export/migration mechanism between
machines and none is needed.** Machine B provisions its own user, model, exchange,
strategy, and trader through the UI (D1 step 10).

## C5 — UNITS AND SCRIPTS: names, paths, assumptions

### systemd SYSTEM units (sudo; rendered at install time)

- `vl.service` — template with `__VL_USER__` / `__VL_DIR__` placeholders,
  `Type=simple`, `ExecStart=__VL_DIR__/vl-bin`, `Restart=on-failure`
  `RestartSec=5`, `StartLimitIntervalSec=0` (`deploy/vl.service`). Installed:
  `User=hoang WorkingDirectory=/home/hoang/vl ExecStart=/home/hoang/vl/vl-bin`
  (measured via `systemctl cat vl`). Rendered by `deploy/install-autostart.sh:96-101`
  (sudo required, `:21-23`), which detects the target user from `SUDO_USER`
  (`:29-37`) and node via login shell → nvm → system PATH (`:47-85`).
- `vl-web.service` — vite dev server :3000, `After=vl.service`,
  `ExecStart=__NODE_DIR__/npm run dev`, `WorkingDirectory=__VL_DIR__/web`
  (`deploy/vl-web.service`). NOTE: the running system's production UI is served
  by the Go binary from `web/dist` (`api/ui_serving.go:34`); `vl-web` is the
  DEV surface. A second machine needs node only if it wants `vl-web` (or to
  run `npm run build` once).

### systemd USER units (no sudo; hardcoded paths inside)

- `vl-backup.service` — oneshot, **hardcoded** `ExecStart=/home/hoang/vl/deploy/vl-db-backup.sh` (`deploy/systemd-user/vl-backup.service:7`, Documentation `:3`); timer 05:00 + 17:30 CT, `Persistent=true` (`vl-backup.timer`).
- `vl-clock-guard.service` — oneshot, **hardcoded** `ExecStart=/home/hoang/vl/deploy/vl-clock-guard.sh` (`vl-clock-guard.service:9`); timer every 15 min.
- Installers `deploy/install-db-backup.sh:12-13` and `deploy/install-clock-guard.sh:16-17` copy these units VERBATIM into `$HOME/.config/systemd/user` — the hardcoded `/home/hoang/vl` paths are NOT re-rendered. On a machine whose user home or clone path differs, these two units point at nonexistent files and die silently (oneshot, journal only). This is a D3 divergence row.
- Both scripts (`deploy/vl-db-backup.sh:17` `VL_DB` default, `deploy/vl-clock-guard.sh:25` `VL_CLOCK_STATE` default) also default to `/home/hoang/vl/...` but honor env overrides.

### Deploy scripts (each with its machine assumptions)

| script | assumptions / hardcoded paths |
|---|---|
| `deploy/install-autostart.sh` | sudo; detects everything; appends `NT_TRANSPORT=tcp` to `.env` if absent (`:76-80`); warns when `.env` or `vl-bin` missing (`:41-47, 89-96`). |
| `deploy/install-clock-guard.sh` | no sudo; **verbatim copy with `/home/hoang` inside** (`:16-17`); needs `systemctl --user` + linger. |
| `deploy/install-db-backup.sh` | no sudo; same verbatim-copy issue (`:12-13`). |
| `deploy/install-journald.sh` | sudo; installs `deploy/journald-vl.conf` dropin + `mkdir /var/log/journal` + restarts journald (`:21-27`). |
| `deploy/vl-db-backup.sh` | `VL_DB` default `/home/hoang/vl/data/data.db` (`:17`); python3 sqlite backup API + `PRAGMA quick_check` + gzip; retention 14 daily / 8 weekly under `~/vl-backups/auto`. |
| `deploy/vl-clock-guard.sh` | `VL_CLOCK_STATE` default `/home/hoang/vl/data/clock-guard-state.json` (`:25`); reads `/sys/class/rtc/rtc0/since_epoch`, `timedatectl timesync-status`, optional `powershell.exe`; writes state JSON for the Go P1.4 boot block. |
| `deploy/fix-wsl2-clock.sh` | owner/sudo path — WSL2 has NO root-free clock resync (`deploy/vl-clock-guard.sh` header: hwclock absent, timesyncd slews only). |
| `deploy/cutover.sh` / the update button | the live cutover path (the retired one-shot `leveltruth-cutover.sh` was superseded by `cutover.sh` + the updater, Z8). |
| `deploy/RESTORE.md` | restore runbook: `kill -9` the bot, swap `~/vl/data/data.db`, verify `quick_check` → `ok`, systemd relaunches. Paths are the machine's own. |
| `deploy/vl-lock.sh` / `deploy/vl-claim.sh` | see C6/C7. |

### Unit-name collision surface

Unit names `vl`, `vl-web`, `vl-backup`, `vl-clock-guard` are generic.
They only collide if both machines shared one systemd namespace or one checkout
— under this wave's assumptions (separate machines, separate clones) they do
not. The things that WOULD collide across machines: the origin-wide claim
branch namespace (refused on collision, `deploy/vl-claim.sh`), and the NT8
SIM account (NOT refused anywhere — C7).

## C6 — THE SINGLE-INSTANCE ASSUMPTIONS (the heart)

For each: does it break, degrade, or silently corrupt when two machines run at once?

1. **Main-tree lock** (`deploy/vl-lock.sh:43` `LOCK_DIR="${VL_LOCK_DIR:-$HOME/vl-main.lock.d}"`; atomic `mkdir` acquire `:71-73`; heartbeat meta; expiry written `:80` but **never enforced** — liveness is the heartbeat, corroboration mandatory). Scope is ONE machine's `$HOME`. Two machines → two lock dirs → **no cross-machine conflict at all**. It breaks only if both machines share a home (not assumed). A lane on B would still need its own acquire/discipline for B's checkout.
2. **Claim protocol** (`deploy/vl-claim.sh`: `cmd_new` refuses when `git ls-remote --heads origin "$br"` already returns the branch — "ANOTHER LANE HAS THIS WAVE"). Scope is **origin (GitHub), which IS shared**. Two machines working the same repo MUST use different branch names; collision is refused loudly, never corrupted. Does not prevent both pushing to `dev` — normal git non-fast-forward handling applies.
3. **Boot marker `deploy/RELEASE`** (tracked file; read at boot from the CWD, `kernel/boot_integrity.go:84-95`; mismatch ⇒ `TRADING REFUSED`, `main.go:279-287`). Two machines with independent clones each read THEIR OWN copy. If both build the same commit, both boot fine. If the branch's `deploy/RELEASE` was moved by one machine's cutover and the other machine builds/restarts a different rev, that machine **refuses trading loudly** — degrade to "read-only dashboard", never silent wrong trading. Escape hatch per machine: `VL_EXPECTED_REVISION`.
4. **GUIDE_BUILT_REV stamp** (`web/src/guide/types.ts:6` = `f8bc7044cc44…`). Compiled into the frontend bundle; the drift banner compares it to `/api/health` revision. Two machines at different revs → the older machine's guide shows a drift banner. Degrades to a warning; does not corrupt.
5. **Broker snapshot table** `nt8_order_snapshots` — rows keyed by `Account`/`BuildID` (`main.go:244-250`). Per-DB ⇒ fine with separate DBs. **Silently corrupts if the DB is copied** (B reads A's broker book as its own) — see C4/D4.
6. **One-contract-per-account invariant.** Not a single-machine assumption, a per-order rule: arms hardcode `Quantity: 1` (`trader/armed_executor.go:1788-1789`); decision-path sizing is notional→contracts clamped by `resolveMaxContracts` (strategy `max_contracts_per_order`, venue default 2, `trader/auto_trader_orders.go:25,31-60`). Two machines = two books; the REAL invariant at risk is "one BOT per NT8 account" — see C7. There is no machine-identity key anywhere in the order path.
7. **Tree guard** — **SPEC ONLY, NOT BUILT** (`docs/superpowers/plans/2026-09-02-tree-guard-spec.md` status line: "SPEC ONLY — not built, not installed"). The AGENTS.md/CLAUDE.md header sentence "the tree guard checks its md5 (check 5)" describes an artifact that does not exist as a running check anywhere in this tree (grep across `deploy/`, `scripts/`, `.github/` = 0 hits). Nothing runs on either machine; nothing to collide. Named in D5 so nobody relies on it.
8. **Clock guard** — per-machine state file under its own `data/` (`deploy/vl-clock-guard.sh:25`); the Go boot block reads the local file. Independent per machine. No collision.
9. **Counters keyed by trader** — `telemetry/gate_blocks.go:36-85`: in-memory per-process map `trader → gate → count`, session-day rollover; surfaces at `/api/risk/gate-blocks`. Independent per machine. Only wrong if the DB were shared/copied (it is not and must not be).
10. **TCP listener** `127.0.0.1:36974` (`provider/ninjatrader/tcp_server.go:34`) — loopback, per host. Two machines don't collide; each host's NT8 talks only to its own Go process.
11. **`installation_id`** in `system_config` (`main.go:520-542`) — per-DB telemetry identity; independent.
12. **`deploy/RELEASE` push discipline (the A19 class)** — nothing in code or CI enforces who may push a marker; `.github/workflows` contains NO check on `deploy/RELEASE` or `GUIDE_BUILT_REV` (grep = 0 hits). The only enforcement is the boot gate on the NEXT machine that restarts at a mismatched rev, and canon. See C7.

**Verdict of this section:** with separate clones, separate DBs, and separate NT8
accounts, every single-instance mechanism is either per-machine-local (lock,
clock state, counters, units, ports) or fails loudly (claim collision, RELEASE
mismatch, build-id mismatch). The two mechanisms that corrupt SILENTLY are
outside the mechanisms' scope: a copied DB, and a shared NT8 account.

## C7 — WHAT THE SECOND MACHINE MUST NOT DO, AND WHAT THE CODE ALLOWS

| forbidden action | does current code/tooling allow it? |
|---|---|
| **Deploy a boot marker for a rev it did not boot** (push `deploy/RELEASE` / marker commit) | **Allowed by tooling** — no CI check, no hook, no guard (grep across `.github/workflows` = 0 hits). Enforced only by canon (A19, RELEASE-ordering four halves) and by the consequence: the next machine that restarts against a marker matching the wrong rev refuses trading (`kernel/boot_integrity.go`). |
| **Push a boot marker for a rev it did not boot on the same branch the other machine builds from** | Same as above — and now the OTHER machine is the victim of a silent-looking mismatch at its next restart. Fail-safe is loud, not silent. |
| **Take the same lock name** | On separate machines the lock is `$HOME`-local (`deploy/vl-lock.sh:43`) — physically impossible to collide under this wave's assumptions. Only a shared home/checkout would collide; not assumed. |
| **Claim the same branch name** | **Refused** — `deploy/vl-claim.sh` checks `origin` before creating (`cmd_new` ls-remote gate). |
| **Trade the same NT8 SIM account** | **ALLOWED — nothing prevents it.** `isAccountTradeable` checks only `Account.Simulation` + the local `NT_ALLOWED_ACCOUNTS` list (`trader/ninjatrader/tcp_trader.go:296-320`); there is no machine-identity key anywhere. Two bots on one account would each place, fill, flatten, and reconcile against their own ledger — double fills and phantom positions. This is the one item whose enforcement is purely procedural: machine B MUST use a different Tradovate SIM account and name it in `NT_ALLOWED_ACCOUNTS` + the trader row. |
| **Copy the DB** | Allowed (nothing blocks it) and exactly what must not happen — C4. |
| **Run the old CSV transport** | Allowed silently — `NT_TRANSPORT` unset falls back to CSV (`trader/ninjatrader/transport.go:3-4`). The installer's `.env` guard (`deploy/install-autostart.sh:76-80`) is the only helper. |

---

# D1 — THE RUNBOOK (ordered, end to end)

Conventions: every step ends with **VERIFY**. "What if you don't see it" is the
repair path. Commands are quoted from repo files or from output quoted above;
anything I could not execute is marked **UNTESTED**.

## Step 0 — Windows prerequisites (on the NEW machine)

0.1 Install NinjaTrader 8, log in once, confirm the platform loads.
0.2 Create/open the NEW machine's own SIM account (different from the primary's).
0.3 Install WSL2 distro (Ubuntu), enable systemd: `/etc/wsl.conf` with
    `[boot]\nsystemd=true` (`docs/AUTOSTART.md` §0), then `wsl.exe --shutdown`
    and reopen.
0.4 Enable mirrored networking (Win11 22H2+: `.wslconfig` `networkingMode=mirrored`;
    the repo documents mirrored mode as the working shape, `web/vite.config.ts:8-13`).
0.5 Verify host timezone is America/Chicago (the user timers assume CT,
    `deploy/systemd-user/vl-backup.timer:6-7`).
- **VERIFY:** `systemctl is-system-running` → `running` or `degraded`;
  `timedatectl` shows `Time zone: America/Chicago`.
- **Not there:** fix `/etc/wsl.conf` (full `wsl.exe --shutdown`) and
  `sudo timedatectl set-timezone America/Chicago`.

## Step 1 — Toolchains (WSL)

1.1 `sudo apt update && sudo apt install -y build-essential git curl gzip python3 openssl`
    (gcc is required by CGO — C1.3; python3+gzip by `deploy/vl-db-backup.sh`;
    openssl for keygen per `.env.example`).
1.2 Install Go ≥ 1.25.3 (`go.mod:3`) — NOT the CI pins 1.21/1.23, which are older
    than the module (`C1.2`). Machine A runs `go1.25.3 linux/amd64`.
1.3 Install node + npm (no version pinned in `web/package.json`; machine A runs
    node v22.22.1; CI uses 18/20/22 — 22 recommended).
- **VERIFY:** `go version` prints ≥ 1.25.3; `node --version` and `npm --version`
  print; `gcc --version` prints.
- **Not there:** install the missing one; a Go < 1.25.3 refuses the module with a
  `go.mod requires go >= 1.25.3` error.

## Step 2 — Clone

2.1 `git clone git@github.com:johnwick2921-cyber/nofx.git ~/vl` (origin per AGENTS.md
    repo ownership; this is the user's own project).
2.2 Decide the pin: machine A's running rev is `f8bc7044` and dev tip is
    `59af58fd` (measured). For a parallel test machine, check out the SAME commit
    the primary runs — or accept any commit and set `VL_EXPECTED_REVISION`
    accordingly (C7). **If you pick a commit whose tree's `deploy/RELEASE` names
    another rev, the bot boots with TRADING REFUSED until you fix it — loud, not silent.**
- **VERIFY:** `git rev-parse HEAD` prints your chosen sha; `cat deploy/RELEASE`
  prints a rev that equals, or is an ancestor-prefix of, the sha you will build.
- **Not there:** `git checkout <sha>`; if the marker still mismatches, either
  build exactly the `deploy/RELEASE` rev or set `VL_EXPECTED_REVISION=<your build sha>`
  in `.env` (it wins, `kernel/boot_integrity.go:87`).

## Step 3 — Build the backend

3.1 `go build -o vl-bin .` (the canonical build, `deploy/install-autostart.sh:45-47`,
    `README.md:222`). Compilation of the whole tree at 59af58fd with go1.25.3 was
    verified green in this wave's worktree (`go build ./...`, ~7 s).
- **VERIFY:** `./vl-bin` exists and `go version -m ./vl-bin | grep vcs.revision`
  prints `vcs.revision=<your sha>` (build from the checkout, not a worktree —
  worktree builds lose vcs stamping and boot refuses, AGENTS.md deploy lesson).
- **Not there:** you built in a worktree or /tmp copy — build from the real clone.

## Step 4 — `.env` (keys BY NAME; values are the owner's)

4.1 `cp .env.example .env`.
4.2 Set, at minimum (C3 REQUIRED-TO-BOOT): `RSA_PRIVATE_KEY` (gen `openssl genrsa 2048`,
    newlines → `\n`), `DATA_ENCRYPTION_KEY` (gen `openssl rand -base64 32`),
    `JWT_SECRET` (gen `openssl rand -base64 64`), `DB_TYPE=sqlite`, `DB_PATH=data/data.db`.
4.3 Set REQUIRED-TO-TRADE: `TRADING_MODE=futures`, `NT_TRANSPORT=tcp`,
    `NT_ALLOWED_ACCOUNTS=<B's SIM account name>`, and optionally the AI tuning
    group (`AI_MAX_TOKENS`, `AI_HTTP_TIMEOUT_SECONDS`, …). The AI API key itself
    goes in the DB model row later (Step 10), NOT in `.env` (`C3`).
4.4 Do NOT set `SANDBOX_MODE=1` (synthetic feed, no live trading,
    `main.go:530-534`) unless that is deliberate. Do NOT carry over machine A's
    `VL_BACKEND_PORT/VL_FRONTEND_PORT/VL_TIMEZONE/NINJATRADER_DATA_DIR/
    DATABENTO_*` lines expecting them to do anything (they are dead on the live
    path — C3 legacy).
- **VERIFY:** `awk -F= '/^[A-Za-z_][A-Za-z0-9_]*=/{print $1}' .env` shows exactly
  the keys you chose. (Never print values — A25.)
- **Not there:** copy failed or wrong dir — `.env` must sit in the clone ROOT,
  the unit's `WorkingDirectory`.

## Step 5 — Frontend bundle (production path served by the Go binary)

5.1 `cd web && npm install && npm run build` (`README.md:223` for install/run;
    build script is `tsc && vite build`, `web/package.json`). **UNTESTED in this
    wave** (no installs allowed); the output contract is `web/dist/index.html`,
    which `MountUI` looks for (`api/ui_serving.go:34-41`) and the boot line
    reports as served or `served-by=none` (`main.go:299-307`).
- **VERIFY:** `ls web/dist/index.html` exists.
- **Not there:** read the tsc/vite errors — the bundle did not build; the API
  still works, the UI does not (boot line says `served-by=none`).
5.2 (Alternative dev surface) skip unless you want the vite dev server as a
    service: it comes with `install-autostart.sh` → `vl-web.service`.

## Step 6 — Database (fresh, never copied)

6.1 Do nothing. The DB is created and migrated on first boot (`C4`). **Do not
    copy `data/data.db` from machine A** — every reason is in C4.
- **VERIFY (after first boot):** `ls -la data/` shows `data.db` (+ WAL files);
  `python3 -c "import sqlite3;print(sqlite3.connect('file:data/data.db?mode=ro',uri=True).execute('select name from sqlite_master where type=\"table\"').fetchall())"`
  lists the ~30 tables (read-only open).
- **Not there:** the process never booted (journal) or `DB_PATH` points elsewhere.

## Step 7 — Units

7.1 `sudo bash deploy/install-autostart.sh` (renders + installs `vl.service`,
    `vl-web.service`, appends `NT_TRANSPORT=tcp` if missing).
7.2 `bash deploy/install-db-backup.sh` — then **fix the hardcoded path**:
    `deploy/systemd-user/vl-backup.service:7` says
    `/home/hoang/vl/deploy/vl-db-backup.sh`. If B's clone is not at that
    exact path, edit the installed unit
    `~/.config/systemd/user/vl-backup.service` and `systemctl --user daemon-reload`
    (the installer copies verbatim — C5). Same for
    `vl-clock-guard.service:9` after `bash deploy/install-clock-guard.sh`.
    Alternatively set `VL_DB` (`deploy/vl-db-backup.sh:17`) and
    `VL_CLOCK_STATE` (`deploy/vl-clock-guard.sh:25`) so the payload scripts
    point at B's clone.
7.3 Optional: `sudo bash deploy/install-journald.sh` (journal persistence,
    owner-gated on A).
- **VERIFY:** `systemctl status vl vl-web` → active; `systemctl --user
  list-timers` shows `vl-backup.timer` (05:00/17:30 CT) and
  `vl-clock-guard.timer` (every 15 min); run each user service once by hand:
  `systemctl --user start vl-backup.service vl-clock-guard.service`, then
  `journalctl --user -u vl-backup.service -n 5` shows "wrote … db.gz" and
  `journalctl --user -u vl-clock-guard.service -n 3` shows `clock-guard status=…`.
- **Not there:** a unit failed → `journalctl -u <unit> -n 50`; if a user unit
  exits instantly with a path error, the hardcoded `/home/hoang` fix above was
  missed. **Linger:** if timers don't fire while logged out,
  `loginctl enable-linger <user>`.

## Step 8 — NT8 AddOn

8.1 `cp ninjascript/*.cs "/mnt/c/Users/<B-windows-user>/Documents/NinjaTrader 8/bin/Custom/AddOns/"`
    (HARD RULE — C2; the destination is the Documents AddOns folder, NOT the repo).
8.2 In NT8: **F5 compile**.
8.3 **Full NT8 restart** (AddOns do not hot-reload).
- **VERIFY:** NT8 Log tab shows the AddOn's connect/hello lines; after the Go
  bot is up you will additionally see the build-id proof (Step 9.4).
- **Not there:** compile errors → read the NT8 compile output; the three files
  ship together and must all compile (`ninjascript/` listing). If NT8 silently
  keeps old behavior, the restart step was skipped — that is the #1 NT8 gotcha.

## Step 9 — First boot

9.1 `sudo systemctl enable --now vl` was already done by the installer; to
    boot by hand: `./vl-bin` from the clone root.
9.2 Read the boot block in `journalctl -u vl -n 200`.
- **VERIFY — see ALL of these:**
  - `✅ Encryption service initialized successfully` (RSA/DATA keys OK);
  - `🔐 BOOT INTEGRITY OK — rev <sha> · built … · expected … · goldens PASS`
    (if instead `REFUSED`: rev/RELEASE mismatch — Step 2);
  - `(No trader configurations, please create via Web interface)` (fresh DB);
  - `🖥` UI line says served from `web/dist` (not `served-by=none`) if Step 5 ran;
  - `🗓 session calendar …` line shows the calendar loaded (from
    `calendar_static_t1.json` or `VL_CALENDAR_STATIC`).
- **Not there:** journal shows the Fatal/error line — fix in order: keys → RELEASE
  → DB path.

## Step 10 — Provision the trading configuration (UI)

10.1 Open `http://localhost:3000` (or `:8080` for the Go-served bundle). Register
    the first user (allowed only on an empty DB, `api/handler_user.go:52-55`).
10.2 Settings → AI Models: add `deepseek` with B's API key (base URL
    `https://api.deepseek.com`, model `deepseek-v4-pro` defaults,
    `mcp/providers.go:19-20`). The key is stored encrypted under
    `DATA_ENCRYPTION_KEY` (`store/ai_model.go:28`).
10.3 Settings → Exchanges: add exchange type `ninjatrader`; set
    `nt_data_dir` to any non-empty path on B (required by the gate,
    `trader/auto_trader.go:678-680`), `nt_instrument_name` = `MNQ`
    (`manager/trader_manager.go:681`), `nt_default_contract_qty` = 1.
10.4 Strategy: create B's strategy (or accept defaults — nothing is pre-seeded,
    `store/strategy.go:1796-1799`). Keep `max_contracts_per_order` ≤ 2 (venue cap,
    `trader/auto_trader_orders.go:25,49-60`) for the intended one-contract posture.
10.5 Traders: create trader → bind the strategy + model + exchange, then
    **select B's SIM account** (persists, `store/trader.go:202-209`; empty =
    gated). Ensure the account name is in `NT_ALLOWED_ACCOUNTS` (Step 4.3).
    Do NOT start the trader yet.
- **VERIFY:** the trader card shows the bound account and model; `journalctl -u vl`
  shows the trader loaded (`📦 Loading trader …` / `✓ Trader '…' loaded to memory`,
  `manager/trader_manager.go:459,713`).
- **Not there:** the Settings pages refuse or 401 — the JWT/registration failed;
  check the boot log and re-login.

## Step 11 — End-to-end verification (the real proof)

11.1 With NT8 open and connected, watch the far-side proof:
    `journalctl -u vl | grep 'nt8 addon'` → must print
    `build_id=<AddOn's VL_BUILD_ID> expected=… match=yes` (the live line on
    machine A is quoted in C2). `VL_BUILD_ID` is in
    `ninjascript/VLTraderTCPClient.cs:55`.
- **NOT there / match=NO:** NT8 runs an old compile — redo Step 8 fully.
11.2 Bars: during market hours, `journalctl -u vl` shows bar updates and no
    "backpressure" floods; the dashboard chart moves.
11.3 Flat book: the positions page is empty and STAYS empty until a trade —
    an empty-but-real book (D4 tells you how an empty book can lie).
11.4 SIM rail: attempt nothing live — the guard is per-account (`C2/C7`); B's
    account name must be the one NT8 reports as `Simulation`.
11.5 Fire a planner read (owner action) and confirm a plan writes
    (`🗓️ PLAN written …` journal line) — proves the AI key + bars + calendar +
    strategy end to end.
- **VERIFY:** steps 11.1–11.5 all observed.
- **Not there:** treat each line's absence as a specific subsystem: build id →
  AddOn; bars → NT_TRANSPORT/tcp or NT8 feed; plan write → AI key or
  `TRADING_MODE=futures`; book → account binding.

## Step 12 — Crash-restart proof

`sudo kill -9 $(pgrep -x vl-bin); sleep 6; pgrep -x vl-bin && echo RESTARTED`
(`deploy/install-autostart.sh:114-115`). VERIFY the boot block re-prints with
`goldens PASS` and the same rev.

---

# D2 — READINESS CHECKLIST

Separate file, one line per item with command + expected output:
`docs/superpowers/runbooks/2026-09-08-clean-machine-checklist.md`.

# D3 — DIVERGENCE TABLE: what MUST differ between the machines

| setting | machine A (measured) | machine B must | what breaks if identical |
|---|---|---|---|
| NT8 SIM account (trader row `account`, `store/trader.go:48-54`) | A's account (never printed) | B's own SIM account | two bots interleave orders in one book — double fills, phantom positions; nothing in code prevents it (C7). |
| `NT_ALLOWED_ACCOUNTS` (`config/config.go:167-171`) | names A's account(s) | names B's account | if B's account is not listed and the list is set → every entry refused (`tcp_trader.go:312-320`); if A's name is listed on B → no effect (B's NT8 doesn't report it) but it invites the C7 mistake. |
| `data/data.db` | A's ledger (NEVER copy) | fresh, self-created | phantom book, A's identity, undecryptable credentials, first-user lockout (C4). |
| `JWT_SECRET` | A's value | fresh value | same secret ≠ break (separate DBs) but shared secret = shared session surface; SHOULD differ. |
| `DATA_ENCRYPTION_KEY` / `RSA_PRIVATE_KEY` | A's values | fresh values | with a FRESH DB identical keys are merely insecure-sharing; with a COPIED DB, different keys break decryption and identical keys make A's secrets readable on B — the copy is the defect either way. |
| `deploy/RELEASE` vs binary rev | `f8bc7044` (marker cd5b9a6b) | build the commit B runs AND make its checkout's `deploy/RELEASE` match (or `VL_EXPECTED_REVISION`) | TRADING REFUSED at boot (`kernel/boot_integrity.go`) — loud, but it blocks B until fixed. |
| hardcoded `/home/hoang/vl` in `vl-backup.service:7`, `vl-clock-guard.service:9`, `vl-db-backup.sh:17`, `vl-clock-guard.sh:25`, `scripts/leveltruth_missed_turns.py:20` | A's home | B's clone path (units re-rendered/edited; env overrides `VL_DB`, `VL_CLOCK_STATE`) | backups/clock-guard silently dead (oneshot, journal-only) if B's path differs. |
| exchange row `nt_data_dir` | A's NT8 data path | B's path (any non-empty path satisfies the TCP gate, `trader/auto_trader.go:678-680`) | empty → trader fails to load. |
| `installation_id` in `system_config` | A's id | auto-generated on B's first boot (`main.go:520-542`) | sharing it = copied DB (see row 3). |
| claim branch names | in use on origin | NEW branch name per lane | claim refused (`deploy/vl-claim.sh`) — loud, correct. |
| unit names `vl`, `vl-web`, `vl-backup`, `vl-clock-guard` | A's systemd | B's systemd (separate namespace — no change needed) | collide only if the machines ever share a namespace/checkout — not assumed. |

# D4 — "WILL LOOK FINE BUT IS WRONG" (A15)

1. **Stale AddOn build id.** NT8 boots, frames flow, the dashboard is alive —
   but the AddOn is an old compile. The bot trades the OLD wire behavior; stop
   entries and protective stops are refused or wrong (`tcp_framing.go:239-266`).
   Detect: `journalctl -u vl | grep 'nt8 addon'` → `match=NO` (or the desk
   line's `n/a`-until-frame fields never resolve). Look for the line, don't
   trust that NT8 "compiled fine".
2. **An empty broker book that reads flat.** After a fresh install B's ledger is
   empty — correct. But the SAME emptiness is what a broken account binding or a
   missed NT8 connect looks like. Detect: the `📸 order_snapshot` sink rows
   (`main.go:244-250`) and the journal snapshot lines showing B's account name
   and `count=0`; emptiness WITH B's account name is the healthy kind.
3. **A calendar with unknown dates.** The boot line prints "an unsourced date is
   counted rather than hidden" (`main.go` D6 block) — a machine whose
   `calendar_static_t1.json` is the repo template shows counts for unsourced
   dates. Detect: read the `🗓` boot line; if unsourced > 0, the calendar is not
   the checked one.
4. **Guardrails off while everything looks armed.** e.g. `ARMED_TEST_SEAM=on` or
   `STOP_ENTRY_SEAM=on` copied from A's `.env` (or unset when A had them on) —
   the boot block prints each seam's state (`main.go` boot lines); read them,
   don't assume the defaults are the deployed posture. Worst case:
   `SANDBOX_MODE=1` — a fully alive UI, synthetic bars, canned plans, zero real
   trading (`main.go:530-534`). Detect: the `🧪 SANDBOX MODE` boot WARN.
5. **A DB copied from the primary.** Everything renders — positions, history,
   settings — and is A's, not B's; decrypt fails silently-ish (blank credentials)
   under B's keys, and `users` being non-empty blocks the first-user registration
   flow (`api/handler_user.go:52-55`). Detect: `installation_id` in
   `system_config` equals A's, or positions exist on day one. Fix: delete the DB
   and boot fresh (it is B's decision; this wave writes nothing).
6. **A lock name shared.** On separate machines the lock is `$HOME`-local so a
   shared NAME is harmless — but if B ever mounts A's home or the machines share
   a checkout, both lanes would fight one lock dir (`deploy/vl-lock.sh:43`).
   Detect: `deploy/vl-lock.sh status` names the holder before any work.
7. **`NT_TRANSPORT` missing → CSV fallback.** The bot boots, logs, serves the UI
   — and silently uses the DEPRECATED CSV path (`transport.go:3-4`); no bars,
   no executions, nothing errors loudly at first. Detect: the installer's append
   log line or `grep -q '^NT_TRANSPORT=' .env` (the installer does this check,
   `install-autostart.sh:76-80`).
8. **RELEASE/binary mismatch with a perfectly healthy UI.** `TRADING REFUSED`
   prints once in the journal and the dashboard keeps working read-only
   (`kernel/boot_integrity.go` header, `main.go:279-287`). A person watching the
   UI and not the journal sees a healthy bot that can never open a position.
   Detect: `journalctl -u vl | grep 'BOOT INTEGRITY'`.
9. **The vite dev server serving a different bundle than the Go binary.** B can
   browse :3000 (dev server) while :8080 serves `web/dist`; if `npm run build`
   was never run, :8080 has NO UI while :3000 looks complete — or the two
   disagree after a change. Detect: the `🖥` boot line (`served-by=none` vs
   served) and `curl -sI http://localhost:8080/ | head -1`.

# D5 — UNKNOWNS (named, with what would establish them)

1. **Exact NT8 user-data/trace folder on the second machine.** The AddOns folder
   is established (`VLTraderTCPClient.cs:30`, AGENTS.md HARD RULE); the AddOn
   logs via NT8's own `Log()` and the repo hardcodes no trace path. Established
   by: opening NT8 on B and reading the Log tab / the `trace` folder under the
   user-data directory.
2. **Whether B's NinjaTrader edition supports compiling AddOns.** Not
   determinable from the repo. Established by: Step 8's F5 compile on B.
3. **gcc presence on a clean Ubuntu WSL2.** Inferred from the dynamically-linked
   binary and `mattn/go-sqlite3` (`C1.3`); not tested on a fresh machine.
   Established by: Step 1.1 on B.
4. **The authoritative node version.** No `engines` field; CI pins 18/20/22 and
   the live machine runs 22. Established by: `npm run build` succeeding on B
   with a chosen version.
5. **Which CI workflow actually gates PRs** (`pr-checks.yml` vs
   `pr-checks-run.yml` — two parallel definitions). Established by: the GitHub
   repo's branch-protection / checks configuration.
6. **A live fresh-DB boot** (registration → model/exchange/strategy/trader →
   first plan write). Everything here is established from code paths, and the
   empty-DB no-op behavior of each boot-time backfill was established by reading
   the call sites — but the full sequence has not been executed on a fresh
   database. Established by: Step 9–11 on B.
7. **The tree guard.** SPEC ONLY, not built
   (`docs/superpowers/plans/2026-09-02-tree-guard-spec.md` status). The md5
   check described in AGENTS.md/CLAUDE.md headers does not exist as a running
   artifact anywhere in this tree. Established by: the spec wave being built and
   installed (future).
8. **Machine A's current far-side ledger details** (snapshot row counts). The
   F12 snapshot store was read from code; its live rows were not enumerated in
   this read-only pass. Established by: a read-only `mode=ro` query on A.

# E1–E4 — VERIFICATION OF THIS DOCUMENT ITSELF

**E1 tested commands.** `go build ./...` — RUN, green at 59af58fd with
go1.25.3 (quoted output, ~7 s). `git log -1`, `git ls-remote`, `curl
/api/health`, `/proc/PID/exe`, `go version -m`, `file`, `systemctl cat`,
`journalctl` greps, `awk`/`grep` censuses, and the claim script `new`+`check` —
all RUN and quoted. `npm install && npm run build` — **UNTESTED** (no installs
allowed on either machine by this dispatch). The cutover/backup/clock-guard
scripts were READ, not executed. Every other command in D1 is quoted verbatim
from a repo file named in this document.

**E2 secret scan.** The grep below runs against this finished document and its
checklist; expected zero.

```
grep -nE 'sk-[A-Za-z0-9]{8,}|JWT_SECRET=..|DATA_ENCRYPTION_KEY=..|RSA_PRIVATE_KEY=..|-----BEGIN|api[_-]?key="[^"]+|account"?: ?"[A-Z][a-z]+[0-9]' \
  docs/superpowers/runbooks/2026-09-08-clean-machine.md \
  docs/superpowers/runbooks/2026-09-08-clean-machine-checklist.md
```

(Result recorded in the commit message / report: **zero matches**.)

**E3 every version/path/unit name traces to a repo file or quoted output.** The
File Evidence Index below carries `git log -1 --format=%h` for every file
cited; versions (1.25.3, 22.22.1, 2026-09-07-h1, 2026-09-05-g2, f8bc7044,
59af58fd) are quoted from `go.mod`, measured tool output, `VLTraderTCPClient.cs:55`,
`tcp_framing.go`, `/api/health`, and `git log`. No number is from memory.

**E4 second-reader test.** Ambiguities found and fixed: (a) "the UI at :3000" —
now says :3000 = vite dev surface AND :8080 = Go-served `web/dist`, with the
build step explicit; (b) "set the account" — now names the exact UI step
(Settings → Traders → select account, persisted via `UpdateAccount`) and the
gating consequence; (c) the `.env` copy step now lists the exact key names, not
"the usual keys"; (d) the hardcoded `/home/hoang` units now carry the concrete
edit path + `daemon-reload`; (e) every "should work" phrasing was replaced with
VERIFY + the observed failure mode.

---

# FILE EVIDENCE INDEX (`git log -1` per cited file, from this worktree @ origin/dev 59af58fd)

```
README.md                         d453bc2e 2026-08-26 19:50:46 -0500
docs/AUTOSTART.md                 b03debf4 2026-06-10 21:40:33 -0500
go.mod                            294d7a13 2026-08-29 22:47:49 -0500
.env.example                      ed26620a 2026-08-23 08:42:14 -0500
web/package.json                  6e541c67 2026-08-15 20:15:47 -0500
web/vite.config.ts                11235e13 2026-08-16 09:55:36 -0500
web/src/guide/types.ts            df515875 2026-09-08 15:02:18 -0500
.github/workflows/pr-checks.yml   85794a72 2026-03-15 11:50:08 +0800
config/config.go                  d7a1b1a6 2026-08-27 00:02:49 -0500
main.go                           e020885b 2026-09-08 14:21:34 -0500
store/gorm.go                     0cb06ae5 2026-08-28 18:40:39 -0500
store/driver.go                   85794a72 2026-03-15 11:50:08 +0800
store/store.go                    2730d688 2026-09-05 21:59:36 -0500
store/ai_model.go                 6677b5b0 2026-08-22 18:24:37 -0500
store/exchange.go                 8ea7d79c 2026-05-26 17:58:46 -0500
store/trader.go                   396b16e9 2026-08-25 10:40:45 -0500
store/strategy.go                 5710cb5d 2026-09-08 09:00:25 -0500
crypto/crypto.go                  85794a72 2026-03-15 11:50:08 +0800
kernel/boot_integrity.go          7b19e753 2026-09-03 23:00:08 -0500
mcp/providers.go                  9f82938e 2026-06-11 11:32:24 -0500
trader/auto_trader.go             6310eaf8 2026-09-07 23:51:51 -0500
trader/auto_trader_orders.go      95767c7c 2026-09-02 17:55:15 -0500
trader/armed_executor.go          e020885b 2026-09-08 14:21:34 -0500
trader/ninjatrader/transport.go   c84bd247 2026-09-03 20:04:23 -0500
trader/ninjatrader/tcp_trader.go  1fb3c21e 2026-09-08 01:30:26 -0500
trader/auto_trader_calendar.go    a7486c44 2026-09-02 22:46:11 -0500
provider/ninjatrader/tcp_server.go 7e0c5527 2026-09-07 23:39:11 -0500
provider/ninjatrader/tcp_framing.go 6262bf42 2026-09-07 23:34:58 -0500
ninjascript/VLTraderTCPClient.cs  b4195e6f 2026-09-07 10:53:37 -0500
deploy/vl.service               b03debf4 2026-06-10 21:40:33 -0500
deploy/vl-web.service           b03debf4 2026-06-10 21:40:33 -0500
deploy/systemd-user/vl-backup.service 1b29263c 2026-08-13 17:56:29 -0500
deploy/systemd-user/vl-clock-guard.service 27637c1e 2026-08-19 09:27:34 -0500
deploy/install-autostart.sh       b03debf4 2026-06-10 21:40:33 -0500
deploy/install-clock-guard.sh     27637c1e 2026-08-19 09:27:34 -0500
deploy/install-db-backup.sh       1b29263c 2026-08-13 17:56:29 -0500
deploy/vl-db-backup.sh          1b29263c 2026-08-13 17:56:29 -0500
deploy/vl-clock-guard.sh        5cac3a80 2026-09-02 20:49:24 -0500
deploy/fix-wsl2-clock.sh          1beef226 2026-08-30 23:55:41 -0500
deploy/RESTORE.md                 986a8fbe 2026-08-16 09:54:59 -0500
deploy/RELEASE                    cd5b9a6b 2026-09-08 15:06:00 -0500
deploy/vl-lock.sh               bd20be31 2026-09-03 22:04:13 -0500
deploy/vl-claim.sh              3f23d9bb 2026-09-07 19:30:13 -0500
docs/superpowers/SYSTEM-MAP.md    e020885b 2026-09-08 14:21:34 -0500
docs/superpowers/AUDIT-CHECKLIST.md 78eed09b 2026-09-08 14:32:14 -0500
docs/superpowers/runbooks/2026-09-04-partner-update.md b44ce31c 2026-09-04 13:31:41 -0500
docs/superpowers/plans/2026-09-02-tree-guard-spec.md f9b00935 2026-09-03 21:57:32 -0500
api/handler_user.go               5f412c0b 2026-08-15 23:25:46 -0500
api/ui_serving.go                 1560aeb2 2026-09-03 21:09:35 -0500
telemetry/gate_blocks.go          71f0c39f 2026-08-28 12:05:41 -0500
kernel/min_sl.go                  4657560b 2026-09-02 07:33:39 -0500
cmd/gate-jwt/main.go              1560aeb2 2026-09-03 21:09:35 -0500
calendar_static_t1.json           9fa92f25 2026-08-30 07:10:53 -0500
```

# FINAL STATEMENT

**Can this system currently run on two machines at once?** YES — with the
separations in D3 held (separate clones at a coherent rev, fresh DB per machine,
different NT8 SIM account with matching `NT_ALLOWED_ACCOUNTS` + trader row,
different `.env` keys, re-rendered units). Every single-instance mechanism is
either machine-local (lock, clock state, counters, units, loopback ports) or
fails loudly (claim collision, RELEASE mismatch, build-id mismatch, account
gate). The two silent killers are both outside the mechanisms: a copied
`data/data.db`, and sharing one NT8 SIM account — the latter is NOT prevented by
any code and is enforced only by this runbook. Nothing needs to change in the
code for a correct two-machine run; the hardening candidates, if wanted later:
a CI check on `deploy/RELEASE`/`GUIDE_BUILT_REV` (none exists), path-parameterized
user units (the `/home/hoang` literals), and building the spec-only tree guard.
