# DEAD-WIRE CENSUS — 2026-09-26 (DS-103, audit/0926-dead-wire)

Base: `git log -1` = 04ae1c2f ("RELEASE 6cac1b89 — boot 3 … 2026-09-25") — origin/dev tip at accept. Spec: CTO dispatch 1790399622127 (NO-COSMETIC D1–D5 + dead-wire census, 90-min timebox). READ-ONLY census; NO deletions in this PR (removals become a wave after the current fix waves merge).

## Method

- Tool: `golang.org/x/tools/cmd/deadcode@latest -test=false ./...` (x/tools v0.50.0, auto-switched to go1.26.8). Niced: `GOMAXPROCS=4 nice -n 19 ionice -c 3`. Output: 613 lines, 612 parsed rows, all non-test code.
- Classifier (automated, per row): repo-wide grep of the symbol; any non-test reference → `KEEP(wired)` (cite the first ref); test-only references → `KEEP(tests)`; zero references anywhere (non-test AND test) → `REMOVE-CANDIDATE`.
- Supplemental greps: dead branches behind constants (`if true/false {`) — none found; i18n `if L == "zh"` branches are live wiring, not dead.
- Ownership exclusions applied (per dispatch): telemetry/** counters (DS-104 P2-8), prune/cleanup symbols (DS-104 P2-1), settings/env readers (DS-105), goroutine-stop paths (DS-108). Those do not appear in this census's actionable set.

## Results

| class | count | meaning |
|---|---|---|
| REMOVE-CANDIDATE | 261 | zero references anywhere — prime removal-wave input |
| KEEP(wired) | 142 | deadcode false-positive: has a non-test caller (deadcode's transitive reachability flags the caller's path) |
| KEEP(tests) | 184 | test-support packages/functions (`trader/testutil`, `internal/updaterworker/releasefixture`, test-only helpers) |
| KEEP(harness) | 9 | standalone research harnesses under `docs/superpowers/research/` (own `main`) |
| EXCLUDED (DS-104) | 16 | telemetry counters (15) + prune/cleanup symbol (1) |

Full row-level data: `docs/superpowers/reports/2026-09-26-dead-wire-census.csv` (path, line, kind, symbol, class, why).

### REMOVE-CANDIDATE by package

| package | RC |
|---|---|
| agent | 119 |
| kernel | 40 |
| provider/coinank (+coinank_api) | 28 |
| trader | 15 |
| store | 13 |
| mcp (+mcp/provider, mcp/payment) | 14 |
| api | 6 |
| market | 4 |
| provider/nofxos | 4 |
| logger | 3 |
| manager | 3 |
| crypto | 2 |
| provider/hyperliquid | 2 |
| others (1 each) | 8 |

### Notable REMOVE-CANDIDATE samples (first per package)

- `agent/agent.go:832` `Agent.getTradersSummary` — zero refs [A]
- `agent/config_validation.go:434` `unmarshalStringList` — zero refs [A]
- `agent/entity_field_catalog.go:46` `fieldKeysByCapability` — zero refs [A] (its sibling `manual*EditableFieldKeys` are KEEP(wired) via `skill_semantic_gate.go:45`)
- `agent/history.go:91` `chatHistory.CleanOld` — zero refs [A]
- `kernel` — 40 (e.g. planner/strategy helpers with no caller)
- `provider/coinank` — 23 in the wrapper + 5 in `coinank_api` (legacy market-data path; futures live path reads NT8 BarCache)

### Caveats (for the removals wave)

1. Reflection/string-dispatch: a REMOVE-CANDIDATE with zero literal-name references can still be reached via constructed names (`MethodByName`, `"Set"+name`). The removals wave must re-prove with a reflection grep + a full `go test ./...` RED→GREEN per item.
2. deadcode's transitive deadness: `KEEP(wired)` items were flagged because their caller chain was flagged — they are NOT dead; do not remove on deadcode's word alone.
3. Exported symbols with zero in-repo refs may be package API for the partner repo — the removal wave should diff against vlauto before deleting.

## DEAD/CONFLICT LEDGER (D5)

Census-only PR: no behavior change, no wiring. The ledger for the REMOVAL wave will list each of the 261 candidates → REMOVED → test name (RED→GREEN), once the fix waves land.

## What I did NOT do

- No deletions, no code changes (dispatch: removals are a later wave).
- No go test runs (read-only census; load rule).
- No settings/env/goroutine/counter scans (DS-105/DS-108/DS-104 scopes, excluded per dispatch).
