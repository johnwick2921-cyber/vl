> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# 2026-08-26 · FVG Entry Model — 5th scenario condition (pure-math play)

**PR:** [#79](https://github.com/johnwick2921-cyber/vl/pull/79) · **Branch:** `feat/fvg-entry-model` (base: merged dev `024cf6b0` ⊂ `b22f5c02`)
**Deployed:** `rev 657e813b9da5 · goldens PASS` (flat cutover, release==running verified first).

## Purpose

FVG was a LEVEL TYPE only. Now it is a first-class PLAY: displacement off a Tier-1 anchor
leaves a fresh gap → enter the retrace INTO the gap → invalid on close through the DISTAL
edge → target the next liquidity. Every element machine-computable. **Advisory law: zero new
hard gates — the executor AI remains the judge.**

## Per-commit ledger

| Commit | Change |
|---|---|
| `9b3ec57b` | `kernel/fvg_entry.go` + tests + schema: `PlanFvgEntry{lo,hi,ce,entry_mode,displacement_atr,origin_level,direction}` on `PlanScenario.fvg`; `scenarioConds` + `"fvg_entry"`; write-time re-verification; live confirm states; `AggregateBars` 5m ATR leg |
| `b42a1a6e` | Planner contract: ≤6-line cited playbook + `fvg{}` JSON line; `ScenarioAnchor` returns the DISTAL edge for fvg scenarios (min-SL clearance leg); `RoleMismatches` sweep-reclaim WARN for liquidity-break origins; executor `RenderFvgEntryLines`; boot line |
| `657e813b` | Write-site validation from stored bars (origin set = seated table ∪ HTF section) in the planner retry loop; API `fvg_states` payload; FE band chips (`IN ZONE/ABOVE/BELOW/FILLED_INVALID · touch #n`) |

## Thresholds (env, citation-commented — zero literals)

- `FVG_ENTRY_MIN_DISP_ATR=1.5` — displacement body floor in 5m Wilder ATR(14) (MSS research: <1.5× is noise).
- `FVG_CE_WIDTH_PTS=20` — CE entry mode for gaps wider than this (NQ sweet spot 20–80 pts; 1h+ fill ~70–80%).
- `FVG_ENTRY_LOOKBACK_BARS=40` — a gap older than this is stale (freshness ladder applies to the gap as a zone).
- Gap floor reuses the existing `fvgMinGapPoints` (max(2×tick, noise floor)).

## Validator (write-time, retry-loop consuming)

Re-verifies from stored bars: 3-candle relation (dispatch convention 0=newest — bullish `low[0] > high[2]`, bearish mirrored) · gap size ≥ floor · impulse body ≥ 1.5×ATR5m · origin_level ∈ seated table ∪ HTF section · entry_mode=ce only for wide gaps · CE recomputed (a lying ce fails). Fake/stale/weak → fail → planner retry.

## Interactions (each tested)

- **min-SL:** `ScenarioAnchor` = distal edge → SL beyond distal +2 ticks clears the level leg naturally; the ATR leg still refuses narrow-gap setups (`TestFvgMinSLInteraction`).
- **Role grammar:** fvg_entry on a liquidity_break origin → sweep-reclaim caution WARN (`TestFvgRoleWarn`).
- **Freshness:** touch number counted per retrace into the gap (1st/2nd/3rd+), rendered in the confirm line + card chip.
- **level_stats/expectancy:** scenario conditions persist in the plan doc — the condition-type win-rate analysis picks up `fvg_entry` from day one with zero query changes (no hardcoded condition lists exist outside the schema enum).

## Tests

`TestFvgValidateGolden` (both directions) · `TestFvgValidateRejects` (fake gap / weak displacement / missing origin / ce-on-narrow / lying ce) · `TestFvgConfirmStates` (ABOVE/IN_ZONE/FILLED_INVALID + ce MET rules + touch numbering) · `TestFvgMinSLInteraction` · `TestFvgPromptRender` (playbook + live line) · `TestFvgRoleWarn`. `go test ./...` EXIT=0 · tsc clean · vitest 263 PASS · goldens untouched.

## Proofs

- **Boot:** `📐 fvg_entry: on min_disp=1.5×ATR ce_width=20pt lookback=40 bars — advisory, zero gates`
- **E1** — no plan authored fvg_entry in the first post-deploy cycles: **today has zero qualifying displacements** (live 5m ATR ≈ 32 pts → 1.5× floor ≈ 48-pt 1m body; the largest fresh gap body was 12.25). The validator's REJECT on a real gap is the proof the gate works:
  `REJECT: scenario S1 fvg: displacement body 5.5 < 1.5×ATR5m (35.4) — weak impulse, not a displacement`
  The ACCEPT path is locked by the golden fixtures (both directions). Card band chips covered by the component surface.
- **E2** — live confirm line on REAL bars (real gap [29261.25, 29267.00], state machine computed end-to-end):
  `S1 fvg_entry: gap 29261.25–29267.00 (CE 29264.12, mode edge) — price FILLED_INVALID · touch #12 (closed through distal 29261.25 on the decision TF)`
  card state: `{S1 Lo:29261.25 Hi:29267 CE:29264.125 Mode:edge State:FILLED_INVALID Touch:12 Met:false}`
- **E3** — `go test ./kernel/ -run Golden` PASS; no golden file touched (diff outside new files is additive-only).

## Diff reconciliation

Additive-only: new files `kernel/fvg_entry.go` / `kernel/fvg_entry_test.go`; schema enum + one struct field; prompt contract paragraph; one validator call site; one API payload key; one FE prop. No gate changes in `validateDecision`.

## Owner queue

- **`FVG_ENTRY_MIN_DISP_ATR=1.5` is strict on high-ATR days** (today's 32-pt 5m ATR → 48-pt floor; nothing qualified). Env-tunable — consider 1.0 if the model never authors fvg_entry across a week of normal days. The validator will tell you.
- CE-mode band (10% of width) is a design choice, citation-commented — revisit with the first real ce-mode trades.
