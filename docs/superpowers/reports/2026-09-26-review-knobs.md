# Independent adversarial review — FIX-KNOBS (DS-105)

- **Reviewer:** DS-101 · READ-ONLY · 90-min timebox · reviewed head `8717e3b88` (branch `fix/knobs-truth`, merge-base `35a53d29`).
- **Method:** full-diff source review + mutations via `go test -overlay` in an isolated worktree; DB COPY read-only cross-checks. No file on DS-105's branch was touched.

## Verdict: no P0/P1. The wave is materially sound; findings are P2/P3 below.

## Vector 1 — the 4 risk controls

| knob | status | evidence |
|---|---|---|
| `max_contracts_enabled` | REMOVED (toggle) | field deleted; clamps always-on by construction; `ResolveMaxContracts` value knob + Stage-A ceiling pinned by `TestSizeCapsAlwaysOnAfterToggleRemoval` (see P3-1). |
| `max_margin_usage` | REMOVED | field deleted; was crypto-prompt advisory only — no gate changed. Agent rejections tell the truth (`agent/tools.go`, `agent/skill_execution_handlers.go`). |
| `min_position_size` | REMOVED | field + clamp deleted; admission floor is the hardcoded 12 in `trader/auto_trader_risk.go enforceMinPositionSize` (mirrored in `agent/trade.go:472`). **DB copy check: all 9 stored strategies carry `min_position_size: 12` (the default == the floor) → no live sizing change.** Old rows load (unknown JSON keys ignored; pinned `TestDeadRiskKnobsOldRowsStillLoad`). |
| `notional_cap_enabled` | REMOVED (toggle) | same as contracts; `ResolveNotionalLeverage` value knob pinned (10 vs 20 distinguishes — meaningful). |

## Vector 2 — sessions_enabled vs per-session enable

- The stored `sessions_enabled` list is PARSE-ONLY now; `sessionEnabledForStrategy` = per-session override → runtime-registry fallback; the read scheduler/entry gate use `sessionRunnable` with the RUNTIME registry (`at.sessionRegistry(now)` → `loadStoredRegistry`). No non-display reader of the old list remains (grepped: effective-settings display only).
- **Live config verified (DB copy):** stored list `["NY"]`, per-session `enable:true` for ASIA+LONDON → all three run under the new code ✓. Stored admin registry has ASIA/LONDON `enabled:false` (== compile-time default) ✓.
- Mutation M-C (derived list consults the stored list again) → `TestB1PerSessionEnableIsTheOnlyTruth` **FAILs** ✓ non-hollow.

## Vector 3 — behaviour changes the wave introduces (named)

1. `FAST_MARKET_REASONING` code default `fast`→`max` — the owner-ordered A2 flip; fast-market reads now run at max instead of fast. **Named in commit `1a86b2f07`; a real behaviour change, deliberately ordered.**
2. `sessions_enabled` no longer gates — for a stored row whose ASIA/LONDON enablement came ONLY from the list, those sessions stop. **DB-verified: no live row has a non-NY list** → no live impact; no folded-knob WARN exists for future rows (P3-3).
3. `min_position_size` removed — a stored floor > 12 silently drops to 12. **DB-verified: all live rows store 12** → no live impact (P3-3).
4. `nofx-activate activate/watch/rollback` now REFUSE the live flat layout loudly (P1-C) — an ops-behaviour change, safe direction, named in commit `eaa033a26`.

## Vector 4 — FAST_MARKET_REASONING env high at boot

- `fastMarketReasoningWireWithSource()` reads `os.Getenv` at boot, default `max`, source named on the boot line; duplicate `.env` keys WARN with the last-wins value (the owner's 4×). Pinned by `TestFastMarketReasoningBootSource` (env source + unset default + wire never downgrades) and `TestEnvDupKeyWarning`.
- The owner's actual `high` value flows through the same env-read path (the pin uses `max`; the mechanism is value-agnostic — no gap). Mutation M-D (default back to `fast`) → `TestFastMarketReasoningBootSource` **FAILs** ✓ non-hollow.

## Vector 5 — removed dead fields, leftover readers

- `ExternalDataSource` struct + registry rows removed; `TestExternalDataSourcesAbsentFromStruct` + `OldRowsStillLoad` pin it ✓.
- Leftover readers found:
  - **P3-2** `api/server.go:392-393`: a user-facing help/config text still documents `risk_control.max_margin_usage: 0.5-0.95 …` and `risk_control.min_position_size: minimum USDT …` as configurable — a doc lie about removed knobs. Update or remove.
  - `web/src/types/strategy.ts:318` — comment only ✓. `DayPlanEditor.tsx` — comments + default `['NY']` (parse-only, harmless) ✓. `agent/*` — truthful rejections ✓.

## Findings

- **P2-1** `derivedSessionsEnabled` derives from `kernel.DefaultSessionRegistry()` (compile-time) while the scheduler uses the RUNTIME registry (`at.sessionRegistry`). Display-only today, but when an admin enables a session in the runtime registry without per-strategy overrides, the effective-settings UI lies (D3/D4 shape). Fix: derive from the runtime registry.
- **P2-2** the replacement 12-USDT floor in `enforceMinPositionSize` has NO admission-site test — the knob removal left the floor unpinned. Add one pin (admission refuses below-12).
- **P3-1** `TestSizeCapsAlwaysOnAfterToggleRemoval`'s contracts-value assertion is vacuous: stored 1 vs default 2 both clamp to 1 under the Stage-A ceiling, so a dead value-knob passes (mutation M-A proved it). Pin `ResolveMaxContractsWithSource`'s source (`SourceSaved`) instead.
- **P3-2** the api/server.go help text above.
- **P3-3** no folded-knob boot WARN for stored rows carrying the removed knobs at non-default values (verified no live impact; future rows change silently).

## Mutations run (go test -overlay)

| # | Mutation | Pin | Result |
|---|---|---|---|
| M-A | contracts value-knob dead | `TestSizeCapsAlwaysOnAfterToggleRemoval` | **passes** — vacuous pin (P3-1) |
| M-C | derived sessions consult the stored list again | `TestB1PerSessionEnableIsTheOnlyTruth` | **FAILs** ✓ non-hollow |
| M-D | fast-market default back to `fast` | `TestFastMarketReasoningBootSource` | **FAILs** ✓ non-hollow |

Baseline: `go build ./...` ✓ · store+trader knob pins all green.
