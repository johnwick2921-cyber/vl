<h1 align="center">VL Trader</h1>

<p align="center">
  <strong>An AI day-trading bot for CME micro futures.</strong><br/>
  <strong>NinjaTrader SIM only — never live.</strong>
</p>

<p align="center">
  <a href="https://golang.org/"><img src="https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go" alt="Go"></a>
  <a href="https://reactjs.org/"><img src="https://img.shields.io/badge/React-18+-61DAFB?style=flat&logo=react" alt="React"></a>
</p>

> **Operator's manual + full UI reference:** [docs/README-VL-SYSTEM.md](docs/README-VL-SYSTEM.md).
> That manual was last refreshed to running rev **`717acd34` (2026-08-27)** and is **stale** against the current
> rev — treat its file/line cites as pointers, not truth; the code wins where they disagree.

---

## What it is

VL Trader is a single-instrument, level-anchored **AI day-trading bot for CME micro futures**.
It trades **MNQ** (the ES bar feed also streams) end-to-end on a **NinjaTrader 8 SIM account** —
there is no live path: `isAccountTradeable` hard-blocks every non-SIM account.

Data **and** execution ride the same channel: NinjaTrader 8 streams real-time bars, account,
positions and orders to the Go bot through the bundled **VL AddOn** (`ninjascript/*.cs`) over TCP.
The bot never calls a crypto exchange and never routes an order outside NinjaTrader SIM.

## The pipeline, in plain words

1. **NT8 bars in** — the AddOn pushes real-time MNQ bars (1m up to 6h) over TCP into the Go **BarCache**.
2. **AI planner** — the bot asks the model (DeepSeek) for one **day plan per session**
   (**ASIA / LONDON / NY**): scenarios, levels, direction, budgets.
3. **Executor + armed entries** — plans become **resting stop-entry (stop-limit) orders** at levels;
   nothing is placed at market.
4. **Mentor mode** — the owner's course rules (a Studio switch) decide *which* setups place:
   ISB, PHL/PLH, box returns, SWING4H — with their own filters and sizes.
5. **Risk gates** — daily-loss limit (checked at placement, fail-closed on an unresolved close),
   the 07:30 CT news window, the per-session gate, and the R:R floor all run before an order leaves.
6. **OCO brackets on NT8 SIM** — a filled entry carries one OCO pair (or, on 2+ contracts, the
   **1:1 split**: leg 1 takes +1R, the runner holds to the target).
7. **Exits** — break-even moves both stops, the runner trails behind each closed candle, and the
   swing holds its own 4h path.

## Control surfaces (web UI)

| Page | What it does |
| --- | --- |
| **Dashboard** | Live positions, P/L, AI decision logs, per-trader view |
| **Strategy Studio** | Build the strategy: indicators, risk controls, mentor mode |
| **Settings** | Accounts, models, guardrails, per-trader configuration |
| **Chat** | Conversational AI assistant for the bot |
| **Updates** | Signed releases, one-button install |
| **Guide** | The full rulebook — every knob, its default, and where it lives in code |

The bot serves the API **and** the built web UI on **http://127.0.0.1:8080**; `npm run dev` serves a live-reload dev UI on **:3000**.

## Run it

```bash
# Prerequisites: Go 1.25+, Node.js 18+

go build -o vl-bin . && ./vl-bin     # backend (SQLite at data/data.db)
cd web && npm install && npm run dev   # live-reload dev UI (new terminal)
```

**NinjaTrader AddOn** (required — it is the data source and the execution path):

1. Copy `ninjascript/*.cs` to `C:\Users\<you>\Documents\NinjaTrader 8\bin\Custom\AddOns\`
2. F5-compile inside NinjaTrader's NinjaScript editor
3. **Fully restart NT8** (AddOns do not hot-reload)

**Releases** install with the **Update** button on the Updates page (signed, one click).

## Docs

| Doc | What it covers |
| --- | --- |
| [Operator's manual](docs/README-VL-SYSTEM.md) | The full system + UI reference (note the stale-rev warning above) |
| [Pipeline map](docs/PIPELINE-MAP.md) | End-to-end data/decision/execution flow |
| [Decision anatomy](docs/DECISION-ANATOMY.md) | What the AI is asked and what it answers |

## Contributing

See [Contributing Guide](CONTRIBUTING.md) — the operational contract for shipping changes to `vl`.

---

> **Risk Warning**: AI auto-trading carries significant risk. This bot is SIM-only by design;
> it is built for learning, research, and simulation.

## License

[AGPL-3.0](LICENSE)
