names rewritten to vl on 2026-09-30 (VL rename)
# Onboarding — vl NQ Futures Bot

Welcome. This document gets a new engineer from "I just cloned this repo" to "I can ship a change with confidence." Read it once, then bookmark the cross-referenced docs.

If you only have five minutes, read this section and the data-flow diagram in §3.

---

## 1. Welcome

`vl` is an AI-driven trading bot. The repo started life as a crypto perpetuals bot (Binance / Bybit / Hyperliquid) and was repurposed for **CME NQ/MNQ index futures** using Databento for market data and NinjaTrader 8 (NT8) as the execution venue via a CSV file bridge.

The crypto path is GONE — the crypto brokers, providers and wallet surfaces were removed in the crypto-removal wave (plan v10). The build is **futures-only**: `TRADING_MODE` is accepted only as `futures` (or unset); any other value is ignored with one boot warning. The single supported instrument is MNQ through the NT8 TCP bridge.

The user (the operator) runs the bot on a WSL2 (Linux) host that shares files with a Windows host running NT8. The bot writes signals to a CSV file that NT8 polls; NT8 writes fill records that the bot tails. End to end.

The architectural decisions that shaped this system are documented as ADRs:

- **ADR-001** — Why CSV file bridge instead of TCP.
- **ADR-002** — Why ICT/SMC concepts with deterministic confluence scoring.
- **ADR-003** — Why all monetary math is int64-ticks, not float64.
- **ADR-004** — Why `ForceFlatSignaler` is an interface in `kernel/`, not a direct import.
- **ADR-005** — Why we dispatch parallel `general-purpose` subagents for multi-task plans.
- **ADR-006** — Why CME calendar gating runs BEFORE any AI inference.
- **ADR-007** — Why Plan 1 critical files must remain byte-identical across PRs.

Read ADR-001 → ADR-007 in order before your first non-trivial change. Each is ~150 lines.

---

## 2. System architecture (10,000-foot view)

Four control surfaces on the web UI:

1. **Config / Settings** — AI models + exchanges + per-trader config.
2. **Dashboard / Trader** — positions, P&L, AI decisions.
3. **Strategy / Studio** — prompt + indicators + risk controls.
4. **AgentBeta / Chat** — conversational AI assistant (VLi).

Backend layers:

- **`api/`** — Go HTTP handlers. Single port `:8080`. Frontend dev server on port `:3000` proxies `/api` to here.
- **`kernel/`** — Strategy engine. Builds prompts, validates AI decisions, enforces risk + session gates.
- **`market/`** — OHLCV pipeline, technical indicators, decimal-safe arithmetic (ADR-003).
- **`provider/`** — External data sources (Databento for CME bars; legacy crypto providers for the historical path).
- **`trader/`** — Broker execution layer. The 19-method `types.Trader` interface; `trader/ninjatrader/` is the live target.
- **`store/`** — SQLite via GORM. Decisions, positions, traders, users, audit trail.
- **`config/`** — `.env` loading + the JWT secret gate (`config/config.go:67-69` defaults are dangerous; see CLAUDE.md).

Frontend layer:

- **`web/`** — React 18 + TypeScript + Vite + Tailwind + SWR. See `web/CLAUDE.md`.

---

## 3. Data flow

The full hot path from market data to broker fill:

```
Databento (Historical + Live)
    ↓
market/databento_adapter.go              (Bars → market.Kline)
    ↓
kernel/engine_analysis.go                (4-gate decision pipeline:
                                            L50  T18 CME session         — ADR-006
                                            L55  T19 contract roll
                                            L78  T21 risk limits         — ADR-004
                                            L105 T22 stale / drift)
    ↓
AI inference (Claude / OpenAI / DeepSeek via mcp)
    ↓
Decision (stored in store/decision.go)   — shape locked, see ADR-002
    ↓
trader/ninjatrader/trader.go             (19-method Trader interface impl)
    ↓
provider/ninjatrader/csv_writer.go       (atomic write — temp + rename)
    ↓
trade_signals.csv                        (WSL2 path: /mnt/c/Users/<u>/VLTrader/data)
    ↓
NinjaTrader 8 + vltrader.cs              (Windows, polls every 2s)     — ADR-001
    ↓
trades_taken.csv                         (fill record)
    ↓
provider/ninjatrader/csv_tailer.go       (tail + parse)
    ↓
back to kernel for next cycle
```

The 4 gates in `engine_analysis.go` all run BEFORE the AI inference call so they capture cost savings. See ADR-006 for the rationale on session gating specifically.

---

## 4. Getting started

Prerequisites:

- **Go 1.21+** (`go version` to verify)
- **Node 18+** (`node --version`)
- **TA-Lib** — on Ubuntu: `sudo apt-get install libta-lib0-dev`; on macOS: `brew install ta-lib`
- **WSL2 mirrored networking** — required only if you intend to connect to a Windows-side NT8 instance. See `docs/operations/STARTUP.md` §1.

Clone + build:

```bash
git clone <your-repo-url>
cd vl

# Backend
go build -o vl-bin .
go test ./...

# Frontend
cd web && npm install && npm run build && cd ..
```

Run locally (no Databento, no NT8 — just the backend + UI for inspection):

```bash
./vl-bin > /tmp/vl.log 2>&1 &
cd web && npm run dev   # opens http://localhost:3000
```

The web UI proxies `/api` calls to `localhost:8080`. SQLite is at `data/data.db`. Stop with `pkill -TERM -f vl-bin`.

For the **full live SIM round-trip**, you need a Windows host running NT8 + VLTrader + a Databento API key. The runbook is `docs/operations/STARTUP.md` — start there, not here.

Environment variables in `.env` at repo root:

```bash
JWT_SECRET=...                            # openssl rand -base64 64
DATABENTO_API_KEY=...                     # CME Globex historical/backfill context
DATABENTO_DATASET=GLBX.MDP3               # CME Globex (default)
NINJATRADER_DATA_DIR=/mnt/c/Users/<u>/VLTrader/data
RISK_MAX_DAILY_LOSS_USD=500
RISK_MAX_CONCURRENT_TRADES=2
RISK_MAX_NOTIONAL_USD=50000
RISK_MAX_CONTRACTS_PER_ORDER=5
```

---

## 5. Key code paths

When tracing a behavior, these are the files to open first:

| Concern | File | Notes |
|---|---|---|
| Decision pipeline (the hot loop) | `kernel/engine_analysis.go` | The 4 gates live here. See ADR-006. |
| Futures prompt template | `kernel/engine_prompt_futures.go` | ICT/SMC vocabulary. See ADR-002. |
| CME session calendar | `kernel/cme_calendar.go` | `IsCMEOpen`, holidays. ADR-006. |
| Risk limits / force-flat | `kernel/risk_limits.go` | `ForceFlatSignaler` interface. ADR-004. |
| Decimal-safe arithmetic | `market/decimal_safe.go` | int64 ticks. ADR-003. |
| Data freshness | `market/data_freshness.go` | `MaxAgeRTH` (90s) / `MaxAgeETH` (5min) / drift threshold (5% / 60s). |
| Databento HTTP client | `provider/databento/client.go` | Basic auth: key as username, EMPTY password. |
| Databento bar adapter | `market/databento_adapter.go` | Float OHLCV at the boundary, then int64 ticks. |
| NT CSV writer | `provider/ninjatrader/csv_writer.go` | Atomic temp + rename. ADR-001. |
| NT CSV tailer | `provider/ninjatrader/csv_tailer.go` | Tails `trades_taken.csv`. |
| Trader interface impl | `trader/ninjatrader/trader.go` | 19 methods. See `trader/CLAUDE.md`. |
| Tick rounding | `trader/ninjatrader/tick_rounding.go` | 0.25 for NQ/MNQ. ADR-003. |
| End-to-end smoke | `cmd/nq_smoke/main.go` | Sub-commands: databento / resolver / prompt / roundtrip / all. |
| Decision JSON shape (do NOT break) | `kernel/engine_position.go` + DB schema | Locked. See CLAUDE.md "High-cascade types." |

Subsystem CLAUDE.md files (read before editing the relevant area):

- `kernel/CLAUDE.md` — strategy engine + prompts
- `market/CLAUDE.md` — OHLCV + indicators + the `Normalize()` case-preservation bug
- `provider/CLAUDE.md` — data providers
- `trader/CLAUDE.md` — broker implementations (19-method interface)
- `trader/ninjatrader/CLAUDE.md` — NT-specific quirks
- `web/CLAUDE.md` — React conventions + i18n hotspots

---

## 6. Tests

```bash
go test ./...                          # all Go tests
go test -run TestRisk ./kernel         # one package
go test -race ./...                    # race detector
cd web && npm run lint && npm run build
```

Notable test suites:

- `market/decimal_safe_test.go` — drift regression test (ADR-003). Catches anyone who tries to "simplify" by going back to floats.
- `kernel/engine_prompt_golden_test.go` — prompt structure freeze. Diff-friendly snapshots.
- `kernel/cme_calendar_test.go` — session boundaries, holiday table, DST handling.
- `kernel/risk_limits_test.go` — daily-loss / concurrent / notional / per-order gates + force-flat classification.
- `provider/databento/historical_test.go` + `mock_server_test.go` — Databento contract + mock harness (Plan 5).
- `provider/ninjatrader/csv_writer_test.go` + `csv_tailer_test.go` + `mock_nt_test.go` — CSV protocol + mock NT (Plan 5).
- `trader/ninjatrader/tick_rounding_test.go` — banker's rounding to 0.25 for NQ/MNQ.

---

## 7. Smoke matrix

`cmd/nq_smoke` is the end-to-end harness. Sub-commands (Plan 5 Task 29):

```bash
go run ./cmd/nq_smoke help              # list sub-commands
go run ./cmd/nq_smoke databento         # Databento client only (needs DATABENTO_API_KEY)
go run ./cmd/nq_smoke resolver          # symbol resolver only
go run ./cmd/nq_smoke prompt            # prompt build only (no AI call)
go run ./cmd/nq_smoke roundtrip         # CSV signal → NT mock → fill
go run ./cmd/nq_smoke all               # full live round-trip (needs NT running)
```

The `all` sub-command requires `DATABENTO_API_KEY` + `NINJATRADER_DATA_DIR` + NT8 running with VLTrader attached to an MNQ chart. See `docs/operations/STARTUP.md` §4.

---

## 8. How to ship a change

1. **Branch from main.** Naming: `feat/...`, `fix/...`, `docs/...`, `chore/...`. See `CONTRIBUTING.md`.
2. **Read the relevant CLAUDE.md** for the subsystem you're touching.
3. **Check the Plan 1 critical-files list** in `CONTRIBUTING.md` and ADR-007. If you must modify one, you need a spec-authorized reason.
4. **Implement.** For multi-task work, dispatch in parallel per ADR-005.
5. **Verify locally:**
   ```bash
   go build ./...
   go test ./...
   cd web && npm run build
   ```
6. **For UI changes**, run a live-DOM Playwright check — bundle scan alone is not sufficient. Plan 4.2 shipped because Plan 4's UI verification missed five downstream consumers of the `NinjaTrader` exchange type.
7. **Commit + push + PR.** PR title in conventional-commits form (`feat(scope): subject`). PR body lists what changed and any deliberate Plan 1 critical-file delta with justification.

---

## 9. Operations docs (read once before any live trading)

- `docs/operations/STARTUP.md` — Cold start, pre-flight, first-trade smoke.
- `docs/operations/MONITORING.md` — Logs to grep, alert thresholds, risk-limit log patterns.
- `docs/operations/TRADER_MODE.md` — Daily / weekly checklist, SIM ↔ live switch, emergency flat.
- `docs/operations/ROLLBACK.md` — Code / schema / NT-script / risk-state rollback.
- `docs/operations/DR.md` — DB corruption, NT crash, Databento outage, WSL2 reboot, lost JWT.

---

## 10. Release history

The repo has 8 release tags. In order:

| Tag | Summary |
|---|---|
| `v1.0-plan1` | CSV bridge SIM-validated — first live SIM fills on SIM101 via NT Playback. ADR-001. |
| `v1.0-plan2` | CME futures domain — tick rounding, session calendar, contract roll, decimal-safe int64 arithmetic. ADR-003, ADR-006. |
| `v1.0-plan3` | Risk limits + force-flat kill switch + stale-data drift detection. ADR-004. |
| `v1.0-plan4` | Observability — audit trail, retry/circuit-breaker, Prometheus metrics, Emergency-Flat API endpoint. |
| `v1.0-plan4-1` | UI gaps — NinjaTrader exchange UI + Decisions tab + VLTrader rebrand sweep. |
| `v1.0-plan4-2` | Integration bug fix-up — five downstream consumers of the `NinjaTrader` exchange type that Plan 4 missed. |
| `v1.0-plan5` | Testing matrix — Databento mock + NT mock + prompt golden tests + smoke sub-commands. |
| `v1.0-plan6` | Operational runbooks (STARTUP / MONITORING / TRADER_MODE / ROLLBACK / DR). |

Plan 7 (this onboarding doc + the 7 ADRs + CONTRIBUTING) is the final canonical plan; no new code ships with it.

---

## 11. Where to learn more

- **Implementation plan:** `docs/superpowers/plans/2026-05-22-nq-databento-ninjatrader.md` — the full 37-task spec.
- **Project-level instructions:** `CLAUDE.md` at repo root.
- **Architecture decision records:** `docs/adr/ADR-001..ADR-007`.
- **Upstream architecture docs (historical reference only):** `upstream github link (removed in the VL rename)` — useful for the strategy-engine design vocabulary. **Stale-path warning** in CLAUDE.md: upstream cites `decision/engine.go`, locally it's `kernel/engine.go`.
- **Agent persona (VLi assistant spec):** `agents.md` (Chinese).
- **Persistent memory across Claude Code sessions:** `~/.claude/projects/<project-slug>/memory/`.
