names rewritten to vl on 2026-09-30 (VL rename)
# External Architecture Report — End-to-end UI, Backend, NT8 Data Pipeline (2026-05-28)

> **Source:** External agent / reviewer, delivered 2026-05-28.
> **Status:** Captured verbatim for the audit trail. See companion file
> `2026-05-28-comparison-vs-canonical-plan.md` for what matches reality,
> what's drifted, and what's missing relative to
> `docs/superpowers/plans/2026-05-22-nq-databento-ninjatrader.md`.
> The author probed the public `dev` branch of
> `github.com/johnwick2921-cyber/nofx` and explicitly flagged NT8-specific
> claims as INFERRED. The actual NT8 work lives on `main` (default branch
> for the operator) and the comparison doc reconciles the two views.

---

# vl (johnwick2921-cyber fork) — End-to-End Architecture Report: UI, Backend, and NT8 Data Pipeline

## TL;DR

- **The futures/NT8 adaptation described in the brief is not visible on the public `dev` branch of `johnwick2921-cyber/nofx` as of 2026-05-28.** A direct probe of the repo found a 1,113-commit fork whose visible `dev` HEAD is the upstream crypto VL (CHANGELOG last-updated 2025-11-01, latest version 3.0.0, no v1.5.x entries, no `ninjascript/` folder, no `CLAUDE.md`, no `provider/ninjatrader` surfaced at the root listing, and zero C# in the language breakdown shown by GitHub). The NT8 work in this report is therefore documented from the owner's detailed architecture brief and external authoritative references, and is explicitly flagged as **INFERRED / UNVERIFIED-FROM-PUBLIC** where it cannot be corroborated against the visible source.
- **Architecturally, the system is a three-tier loop**: NT8 C# AddOn (`VLTraderTCPClient.cs` + `VLBarsSubscriptionManager.cs`) ⇄ Go backend (`provider/ninjatrader/tcp_server.go` + `tcp_framing.go`, `kernel/engine_*.go` decision layer, `store/` GORM persistence, Gin `api/` HTTP layer, SSE chart relay) ⇄ React frontend (Vite + TypeScript + TradingView Lightweight Charts v5). A single TCP socket carries both control-plane messages (`signal`, `fill`, `heartbeat`, `ack`) and the data-plane bar feed (`bars_subscribe`, `bars_historical`, `bar_update`, `bars_unsubscribe`) using a 4-byte big-endian length prefix + JSON envelope `{type, payload}`, 1 MB max frame.
- **Three concrete operational risks dominate**: (1) the **N11 trader-starvation bug** — Balanced Strategy pointing at a dead crypto-coin signal source returns an error and starves the NQ trader; fix is `coin_source=static` + `["NQ.c.0"]`; (2) the **EventSource/JWT-in-query constraint** for the chart SSE stream — the WHATWG html issue #2177, opened by GitHub user chicoxyzzy on Dec 14, 2016, asks "Seems like there is no way to add Authorization header or any other headers for EventSource. Is there any reason it shouldn't be possible?" — so the JWT must travel as a URL query parameter and the chart relay endpoint must accept it there; (3) the **ADR-007 "Plan 1 critical files" byte-identical contract** — `tcp_server.go`, `tcp_framing.go`, and the C# `VLTraderTCPClient.cs` must remain wire-compatible across versions or the AddOn silently desyncs from the Go side.

## Key Findings

### 1. Repository state as actually observed
- The public fork `https://github.com/johnwick2921-cyber/nofx` (forked from `upstream github link (removed in the VL rename)`, owner login `johnwick2921-cyber`, default branch `dev`, 1,113 commits, 27 tags, homepage `vergex.trade`) **shows only the upstream crypto codebase on `dev`**. CHANGELOG.md last-updated 2025-11-01 has six entries — `[Unreleased]`, `[3.0.0] 2025-10-30`, `[2.0.2] 2025-10-29`, `[2.0.1] 2025-10-29`, `[2.0.0] 2025-10-28`, `[1.0.0] 2025-10-27` — none of which mention NT8, Databento, v1.5.x TCP fixes, Plan 4.4, ADR-007, or N11.
- `agents.md` at the root is a 922-line Chinese-language VLi crypto-agent spec ("VLi 交易智能助手规范"); it lists agent tools (`manage_trader`, `manage_exchange_config`, `manage_model_config`, `manage_strategy`, `execute_trade`, `get_positions`, `get_balance`, `search_stock`) but contains no futures/NQ/NT8 vocabulary.
- No `ninjascript/` folder, no `CLAUDE.md` at root, and no C# language fraction is visible (GitHub language stats: Go 67.6%, TypeScript 31.2%, Shell 0.6%, CSS 0.4%, JavaScript 0.1%, Makefile 0.1%). The futures pivot work therefore **either lives on a non-`dev` branch, in a separate private fork, or in an unpushed working tree**. All NT8 architectural detail below is described as in the brief and should be treated as the intended/local design rather than confirmed-from-public source.

### 2. Pipeline at a glance (end-to-end)
```
[CME matching engine] → Tradovate → NT8 client
        │
        ▼ (NT8 BarsRequest, OnBarUpdate)
[C# AddOn] VLBarsSubscriptionManager → VLTraderTCPClient
        │  TCP socket (4-byte BE length prefix + JSON envelope, ≤1 MB frame)
        ▼
[Go] provider/ninjatrader/tcp_server.go ← single-client gate, frame dispatch
        │
        ├──► market/data.go  (bar cache, keyed by symbol+timeframe)
        │       └──► market/data_indicators.go  (EMA/MACD/RSI/ATR/Bollinger via TA-Lib)
        │
        ├──► kernel/engine_analysis.go → engine_prompt_futures.go → AI provider (mcp/)
        │       └──► engine_position.go → decision parsing → trader/ninjatrader/tcp_trader.go
        │              └──► sends signal frame back to NT8 → NT8 places order → fill frame
        │                     └──► store/position.go, store/decision.go (GORM/SQLite)
        │
        └──► chart relay: Go SSE endpoint → React FuturesChart (Lightweight Charts v5)
```
Round-trip latency is bounded by AI inference (decision cycle defaults documented at 3 minutes for upstream VL), not by the wire layer.

### 3. Backend (Go) — package responsibilities
- `provider/` — market-data ingress. `ninjatrader/tcp_server.go` is the TCP listener and frame dispatcher; `ninjatrader/tcp_framing.go` is the encode/decode for the 4-byte BE length prefix + JSON envelope. `databento/` is now historical-only (the documented tier split — Databento's product pages distinguish "APIs and client libraries for receiving historical data older than 24 hours" (Historical) from "real-time and intraday history from the last 24 hours" (Live) — made the Historical tier unusable for live decisions when only Historical was licensed).
- `trader/` — execution egress. `Trader` interface in `trader/types`, with two NinjaTrader impls: legacy CSV trader (`trader/ninjatrader/trader.go`, file-watcher protocol) and the current TCP trader (`trader/ninjatrader/tcp_trader.go`). The `NT_TRANSPORT` env var routes between them.
- `kernel/` — decision/AI/risk core. `engine.go` (cycle loop), `engine_analysis.go` (indicator → context assembly), `engine_prompt.go` (crypto prompt), `engine_prompt_futures.go` (futures prompt variant — NQ/MNQ language, USD throughout, no funding-rate / OI), `engine_position.go` (open/close/update_stop_loss/update_take_profit/partial_close enforcement).
- `market/` — `data.go` (OHLCV bar cache), `data_indicators.go` (TA-Lib indicators: EMA, MACD, RSI, ATR, Bollinger Bands).
- `store/` — GORM models: `exchange.go`, `strategy.go`, `position.go`, `decision.go`. SQLite is the persistence target.
- `manager/trader_manager.go` — multi-trader lifecycle (start/stop, isolation, account-level segregation).
- `api/` — Gin HTTP handlers: `handler_exchange.go`, `handler_trader.go`, `handler_klines.go`, `handler_symbols.go`, `strategy.go`, and a JWT-secured SSE endpoint for the chart.
- `agent/` — chat agent + tools surface exposed in the AgentBeta page.
- `config/` — loader for `.env`, `config.json`, and runtime feature flags.
- `cmd/nq_smoke/` — a CLI smoke test for the NQ pipeline (subscribe a bar series, dump a few `bar_update` frames, exit).
- Upstream `docs/architecture/README.md` confirms the broader package layout — *"vl/ ├── main.go # Entry point ├── api/ # HTTP API (Gin framework) ├── trader/ # Trading execution layer ├── strategy/ # Strategy engine ├── market/ # Market data service ├── mcp/ # AI model clients ├── store/ # Database operations ├── auth/ # JWT authentication ├── manager/ # Multi-trader management └── web/ # React frontend"*.

### 4. The TCP wire protocol (Plan 1.5)
- **Framing**: 4-byte big-endian unsigned length prefix followed by the JSON envelope `{type: string, payload: object}`. Maximum frame size is 1 MB; oversized frames are rejected and logged.
- **Control-plane message types**: `signal` (Go→NT8: side, qty, contract, stop/target), `fill` (NT8→Go: fill price, qty, ts, account), `heartbeat` (bidirectional, used as a liveness check), `ack` (correlation-ID echo).
- **Data-plane message types** (added by Plan 4.4): `bars_subscribe` (Go→NT8: symbol, timeframe, history depth), `bars_historical` (NT8→Go: one-shot batch of backfill bars), `bar_update` (NT8→Go: incremental closed bar or in-progress tick-update for the live bar), `bars_unsubscribe`.
- **Connection model**: single-client. The Go server holds one active NT8 connection at a time and refuses (or supersedes) duplicates. On (re)connect, the Go side **auto-resubscribes** all symbols+timeframes currently registered with the manager.
- **v1.5.6 write fix**: writes are serialized through a per-connection write mutex and each write has its own `SetWriteDeadline` — required because concurrent goroutines (heartbeat, signal, bars_subscribe) were corrupting frames on the wire under load.
- **v1.5.7 read-deadline-desync fix**: the previous design called `SetReadDeadline` on the read loop to enforce idle-timeout, but a long `bars_historical` batch (10k+ bars) could legitimately span more than the deadline, causing spurious resets. The fix removes the read deadline on the read loop entirely and instead uses a `context.AfterFunc(ctx, func(){ conn.SetReadDeadline(time.Now()) })` watcher that fires only on shutdown — this is the canonical Go pattern for cancellable Reads. The Go `context` package docs spell out the contract: *"func AfterFunc(ctx Context, f func()) (stop func() bool) AfterFunc arranges to call f in its own goroutine after ctx is canceled."*
- **Unknown frames**: dispatch logs a warning and continues rather than tearing down the connection — important for forward-compatibility when one side ships a new message type before the other.

### 5. Bar feed and chart relay (Plan 4.4 stages)
- **Stage 1 — C# AddOn**: `VLBarsSubscriptionManager.cs` owns one `BarsRequest` per (symbol, timeframe) pair. The NinjaTrader 8 NinjaScript help guide is explicit: *"BarsRequest can be used to request Bars data and subscribe to real-time Bars data events."* The standard pattern (visible in NinjaTrader support-forum samples) is to attach `barsRequest.Update += OnBarUpdate` for the live event and use the `Request(callback)` overload for the historical batch. `VLTraderTCPClient.cs` is the long-lived TCP client that handles connect/reconnect, heartbeats, and frame serialization back to the Go server.
- **Stage 2 — Go bar handling**: `tcp_server.go` dispatches `bars_historical` and `bar_update` frames into `market.Data` (the bar cache, keyed by `symbol|timeframe`). The cache is bounded and uses a coalescing channel: if the indicator/decision consumer is slow, in-progress bar updates for the same bucket overwrite older pending updates rather than queuing, so the consumer always sees the latest tick.
- **Stage 3 — Kernel wiring**: every decision cycle, `engine_analysis.go` pulls the current bar window for each `SelectedTimeframes` entry, recomputes indicators via `data_indicators.go`, and assembles the prompt context. Two strategies are supported: **native multi-timeframe** (one `BarsRequest`/`bars_subscribe` per timeframe, which is the NT8-idiomatic choice — NT8 docs note "BarsRequests are asynchronous operations") and **Go-side aggregation** (subscribe at the finest timeframe and roll up in Go for historical bandwidth savings).
- **Stage 4 — Chart**: a Go SSE endpoint (`/api/klines/stream?token=...`) re-broadcasts `bar_update` frames to the React `FuturesChart` over EventSource. **The JWT must be a query parameter** because the EventSource constructor has no header API — see WHATWG html issue #2177 quoted in the TL;DR. The standard mitigation is either cookie-based auth or token-in-URL.
- **Lightweight Charts v5 migration**: the official TradingView "From v4 to v5" migration guide lists the headline change as *"Unified series creation API using single `addSeries` function"*. The example replaces `chart.addCandlestickSeries({...})` with `chart.addSeries(CandlestickSeries, {...})`. The chart must `setData(initialBatch)` once after the `bars_historical` arrives, then call `series.update(bar)` for each incremental `bar_update` to avoid full re-renders.

### 6. UI (React) — 4 control surfaces
- **Config / SettingsPage** — AI model keys, exchange/broker credentials, JWT settings, NT_TRANSPORT toggle. Tab structure carried from upstream; for futures, crypto unit labels are remapped to USD and leverage/liquidation columns are hidden.
- **TraderDashboardPage** — live positions, P/L, decision audit. `DecisionCard.tsx` and `DecisionAudit.tsx` render the AI chain-of-thought; `ExchangeConfigModal.tsx` is reused for the NT8 connection settings.
- **StrategyStudioPage** — visual strategy builder: `CoinSourceEditor.tsx` (here used as the symbol/contract selector; for NQ the correct value is `coin_source=static` with the list `["NQ.c.0"]`), `IndicatorEditor.tsx` (EMA/MACD/RSI/ATR/Bollinger — funding-rate and open-interest indicators are hidden for futures), `RiskControlEditor.tsx` (margin, max-positions, R:R floor).
- **AgentChatPage (AgentBeta)** — chat surface over the `agent/tools.go` toolset (`manage_trader`, `manage_exchange_config`, `manage_model_config`, `manage_strategy`, `execute_trade`, `get_positions`, `get_balance`, `get_market_price`, `get_trade_history`). `MarketTicker.tsx` is futures-aware and surfaces MNQ tickers alongside NQ.
- **Shared**: `ChartTabs.tsx` carries a futures pill that switches the chart between NQ and MNQ; `HeaderBar.tsx` is the cross-page nav; `router/AppRoutes.tsx` + `router/paths.ts` define the route table; `i18n/translations.ts` carries en/zh strings (the upstream repo's Chinese README and the Chinese-language `agents.md` confirm zh is a first-class locale).

### 7. ADR-007 "Plan 1 critical files"
The byte-identical contract pins the wire-protocol files so the C# AddOn and Go server stay in lockstep across versions:
- `provider/ninjatrader/tcp_server.go`
- `provider/ninjatrader/tcp_framing.go`
- `ninjascript/VLTraderTCPClient.cs`

Any change must be **simultaneous on both sides** and bumped through a coordinated release; otherwise the AddOn either silently drops new frame types (warn-and-continue path) or, worse, deserializes envelope fields into the wrong shape. The "warn-and-continue on unknown frame type" rule is the safety valve that makes a coordinated rollout merely degraded rather than fatal.

### 8. The N11 trader-starvation bug
Symptom: the NQ trader's decision cycle never produces a signal; the log shows the Balanced Strategy hitting an HTTP 402 response and skipping the cycle. Root cause: the Balanced Strategy preset still references a dead crypto coin-source endpoint, which is a paywalled crypto signal API — it returns an error in the absence of payment, the strategy treats that as "no candidates", and the trader is starved. **Fix**: set `coin_source=static` and `coins=["NQ.c.0"]` in the strategy config; this bypasses the crypto signal pool entirely. (The Balanced Strategy is upstream-default; nothing about it is futures-aware until the static override is set.)

## Details

### 9. Decision cycle (futures variant) — concrete walkthrough
1. **Tick**: scheduler fires every N minutes (upstream default 3; for NQ this is typically tightened).
2. **Candidates**: `coin_source=static` returns `["NQ.c.0"]`.
3. **Context**: `engine_analysis.go` pulls the latest 200-bar window from `market.Data` for each `SelectedTimeframes` entry, calls `data_indicators.go` for EMA/MACD/RSI/ATR/Bollinger, attaches account state from the NT8 fill history in `store/`.
4. **Prompt**: `engine_prompt_futures.go` produces the system+user prompt — the futures variant strips funding-rate/OI/altcoin language and substitutes NQ point/tick math, RTH session awareness, and USD margining. The output contract is the same `<reasoning>...</reasoning><decision>[{symbol, action, ...}]</decision>` envelope upstream uses, with the action set extended (per upstream issue #982, the validated actions are `open_long, open_short, close_long, close_short, update_stop_loss, update_take_profit, partial_close, hold, wait`).
5. **AI call**: routed through `mcp/` to whichever provider is configured (DeepSeek / Claude / GPT / etc. via API keys, (the upstream crypto-payment path has been removed)).
6. **Parse**: response goes through `parseFullDecisionResponse` — CoT extracted, JSON validated, risk parameters re-checked against `engine_position.go` floors (R:R, max positions, margin usage).
7. **Send**: a `signal` frame is built and written to the NT8 TCP connection through `tcp_trader.go` (write-mutex serialized, write-deadline guarded).
8. **Fill**: NT8 reports back via a `fill` frame; `store/position.go` and `store/decision.go` are updated; `DecisionAudit.tsx` picks it up on the next dashboard refresh.

### 10. Backpressure and timing
- **Bounded channel + coalesce**: in-progress live-bar ticks for the same bucket overwrite rather than queue. Closed-bar `bar_update` frames cannot be dropped — they enqueue and the consumer is expected to keep up; if it can't, the cycle skips and logs.
- **Single-client TCP**: prevents multi-AddOn races (e.g. two NT8 instances both pushing to the same Go server). Reconnects auto-resubscribe.
- **Read loop has no deadline** (post-v1.5.7) — long historical batches are legitimate; shutdown is driven by ctx-cancel + AfterFunc, which is the documented Go pattern for cancellable blocking reads.
- **Writes are deadlined** — a stuck NT8 client cannot block the Go server's signal path.

### 11. Frontend chart specifics
- **Initial load**: on mount, `FuturesChart` issues a REST call to `/api/klines?symbol=NQ.c.0&timeframe=1m`, which the Go server services from the bar cache (originally populated by a `bars_historical` response); the chart calls `series.setData(...)` exactly once.
- **Live updates**: an EventSource is opened to `/api/klines/stream?symbol=NQ.c.0&timeframe=1m&token=<JWT>`. Each SSE event is a serialized `bar_update`; the chart calls `series.update(bar)` on each. The Lightweight Charts v5 docs are explicit: *"a series cannot be transferred from one type to another one, since different series types require different data and options types"* — so the candlestick series is constructed once and never replaced.
- **JWT-in-query rationale**: see Key Findings §5 (WHATWG html#2177). The Go SSE handler must therefore parse the token from `r.URL.Query().Get("token")` instead of the `Authorization` header, and the JWT should be short-lived. The upstream VL SlowMist disclosure (Medium, Nov 2025) explicitly called out the residual hardcoded-secret risk: *"config.json.example:1–27 and… In main.go:198–226, admin_mode=true and the default jwt_secret are still hardcoded."* Any deployment must override the default secret before going live.

### 12. What was confirmed vs inferred
- **Confirmed from the public repo as observed (2026-05-28)**: fork lineage, branch `dev` at 1,113 commits, upstream package layout (`api/`, `trader/`, `market/`, `mcp/`, `store/`, `auth/`, `manager/`, `web/`), upstream crypto CHANGELOG through v3.0.0, upstream `agents.md` Chinese spec, upstream `STRATEGY_MODULE.md` decision-cycle flow, upstream prompt structure (`<reasoning>`/`<decision>` XML+JSON), upstream issue #982 listing the nine valid actions, the SlowMist JWT-default disclosure.
- **Confirmed from external authoritative sources**: NinjaScript `BarsRequest` API (NinjaTrader 8 help docs: "BarsRequest can be used to request Bars data and subscribe to real-time Bars data events"), Lightweight Charts v5 `addSeries(CandlestickSeries)` migration (TradingView "From v4 to v5" guide: "Unified series creation API using single `addSeries` function"), EventSource header limitation (WHATWG html#2177 opened by chicoxyzzy on Dec 14, 2016), Go cancellable-read pattern via `context.AfterFunc` + `SetReadDeadline` (pkg.go.dev/context), Databento Historical-vs-Live tier split (Databento product pages: "older than 24 hours" vs "the last 24 hours").
- **Inferred from the owner's brief, not visible on the public `dev`**: `provider/ninjatrader/tcp_server.go` and `tcp_framing.go`; `trader/ninjatrader/tcp_trader.go`; `kernel/engine_prompt_futures.go`; `ninjascript/VLTraderTCPClient.cs` and `VLBarsSubscriptionManager.cs`; CHANGELOG entries for v1.5.6/v1.5.7/Plan 4.4; ADR-007; the N11 / dead-coin-source / HTTP 402 narrative; `web/src/components/charts/FuturesChart.tsx`; the `NT_TRANSPORT` env var router.

## Recommendations

**Stage 1 — Verify ground truth (do this first, before any code change)**
- Confirm which branch of `johnwick2921-cyber/nofx` contains the NT8 adaptation. The public `dev` does not, as of 2026-05-28. Likely candidates: a `futures/*` branch, a `nt8/*` branch, or a private fork. Once identified, point CI and documentation at it explicitly.
- If the work is unpushed, push it to a non-`dev` feature branch and tag the most recent stable revision so v1.5.6/v1.5.7 fixes are recoverable. Trigger to escalate: if the working tree exceeds 30 days without a remote backup.

**Stage 2 — Lock down the wire contract**
- Enforce ADR-007 with a CI check: hash `tcp_server.go`, `tcp_framing.go`, and `VLTraderTCPClient.cs` and fail any PR that changes one without a matching change in the other two. Threshold to relax: only when a coordinated minor-version bump is being released.
- Add a `protocol_version` field to the JSON envelope (or to the `heartbeat` payload). On connect, the side with the older version logs a warning and the newer side falls back; this keeps the "warn-and-continue on unknown frame type" rule from masking real drift.

**Stage 3 — Operational fixes (immediate, high-value)**
- **Apply the N11 fix on every futures trader**: set `coin_source=static` and `coins=["NQ.c.0"]`; verify no Balanced Strategy preset is still pointing at a dead coin-source endpoint. Trigger to revisit: any new strategy preset added by upstream.
- **Rotate the JWT secret away from the default** in `config.json.example` (the SlowMist disclosure is explicit about this being the residual vector even after the admin-mode patch). Make startup refuse to run if `jwt_secret` matches the example string.
- **Short-lived JWTs for the chart SSE**: since the token is in the URL and therefore in server logs/browser history, the chart token must be a separate short-TTL token, not the long-lived API JWT. Issue it from a `/api/chart-token` endpoint that requires the full JWT.

**Stage 4 — Robustness on the bar feed**
- Add a `bars_resync` frame type: on reconnect, the Go side asks NT8 for the last N bars per subscription rather than relying on memory. This protects against the case where Go restarts while NT8 keeps streaming.
- Cap the historical batch at, e.g., 5,000 bars per `bars_historical` and paginate. Even with the 1 MB frame ceiling, a single batch that exhausts the read pipeline holds up other frames; chunking restores fairness.
- Instrument the coalescing channel with a Prometheus counter for dropped in-progress ticks; if it grows above ~1% of the stream rate, the indicator path is too slow and needs profiling.

**Stage 5 — UI hardening**
- Replace EventSource with `fetch`-based SSE (the `fetch-event-source` polyfill / `event-source-polyfill` library) so the JWT can ride in the `Authorization` header. This eliminates the URL-token risk entirely. Trigger to skip: if cookie-based session auth is added instead.
- Audit every component for residual crypto-only labels: crypto unit labels→USD, leverage column hidden for futures, liquidation column hidden for futures, funding-rate / OI indicators hidden in `IndicatorEditor.tsx`. The futures pill in `ChartTabs.tsx` should be the single source of truth for the futures-mode toggle.

## Caveats

- **Public-repo discrepancy**: the `dev` branch of `johnwick2921-cyber/nofx` as publicly visible on 2026-05-28 does not contain the NT8/futures adaptation. Treat every NT8-specific code path described above as the **intended design from the owner's brief**, not as code confirmed against the public source. Code quotes, line numbers, and CHANGELOG version numbers for v1.5.6 / v1.5.7 / Plan 4.4 / ADR-007 / N11 should be re-verified against the actual branch that holds the work.
- **Single-client TCP** is a deliberate simplification; it precludes running two NT8 AddOns (e.g. one for sim, one for live) against the same Go process. Workaround if needed: run two Go processes on different ports.
- **EventSource-in-query JWT** is a known compromise. URLs leak into logs and browser history; the chart token must be short-lived and scoped (read-only, single symbol+timeframe).
- **Databento historical lag** (the cited ~8h availability gap) is the explicit reason it was dropped from the live decision path. It remains useful for backtest and offline indicator tuning; the lag is intrinsic to the Historical-vs-Live tier boundary documented on Databento's product pages.
- **The 1 MB max frame** is generous for `bar_update` (a single bar is ~200 bytes JSON) but tight for `bars_historical` if backfilling weeks of 1-minute bars in one shot — Stage 4 of the recommendations chunks this.
- **Upstream security history**: SlowMist disclosed multiple critical authentication flaws in November 2025 (admin-mode bypass, hardcoded JWT secret); any deployment derived from that codebase must be patched to the post-`be768d9` revision with a rotated `jwt_secret`.
