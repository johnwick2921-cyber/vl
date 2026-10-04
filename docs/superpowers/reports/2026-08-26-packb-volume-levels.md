> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# 2026-08-26 · Pack B Volume Levels + Role/Bias Addendum (owner override)

**PR:** [#78](https://github.com/johnwick2921-cyber/vl/pull/78) · **Branch:** `feat/volume-levels`
**Step 0:** PR #77 merged → dev (`33368ef2`), regression green, flat redeploy, boot quote:
```
🔐 BOOT INTEGRITY OK — rev 33368ef27891 +dirty · expected 33368ef2 · goldens PASS
```
**Cutover:** `02f10942` (fixes landed after E1 verification: `7c48aff7` seat order, `2543f9dd` post-swap re-sort, `02f10942` B-grade-anchor displacement, `61814630` nPOC magnet role, `6be7a88b` dist zero-reference fix).

## OWNER OVERRIDE — Phase 0

Backward Phase-0 replay **waived** (no historical bars existed at volume-wave scope time);
forward validation (B4 level_stats) replaces it. Phase 0 reduced to:
- **(a) live volume real:** re-confirmed — NT8 frames carry real V (1m MNQ V=460/728/631, 2026-08-26), bars table persists them.
- **(b) computable:** session VWAP/profile need only 1m OHLCV — computed from bars table + cache (`vwapAndStdev`, `profileLevels`).

## B1 — detectors (`kernel/levels_volume.go`)

SessionVWAP + ±1σ bands (dynamic re-emit each cycle, 17:00 CT anchor) · eVWAP (16:00 CT prior-day anchor) ·
PriorDayProfile pdPOC/VAH/VAL (70% value area, **cached at roll**) · NakedPOC **10-session retire-on-touch** ·
pdVWAP · SETT (prior 16:00 CT close) · MID-O ((ONH+ONL)/2). New kinds: `VWAP/eVWAP/POC/VAH/VAL/pdVWAP/SETT/MID-O`.
typeEvidence per spec with citation comments (0.90/0.85/0.80/0.60 — provisional; B4 is the 2-week verdict).

## B2 — tiering

- seats 12→8, proximity 2.0→1.5 — **overwritten with note** (DB backup `~/vl-backups/pack-b/data.db.0800`, WHERE-scoped on strategy `a5b7662e`).
- `TIER1_PROXIMITY_TICKS=12` — a pattern grades above C only within 12 ticks of a Tier-1 anchor.
- min_grade **Tier-1 exception** (Tier-1 rows/labels survive any min_grade cut).
- HTF seats **Tier-1-or-displacement** (`isHTFSeatEligible`: Tier-1 kind OR reversal zone).
- Volume-family **seat guarantee** (E1): one seat reserved for an in-band volume level; displaces the weakest non-A-anchor, non-HTF, non-volume row.

## B3 — confluence + ladder + noise gate

- Confluence counts **DISTINCT FAMILIES** (vwap/profile/prior/anchor/overnight/liquidity/zone/round/gap), cap 3 (C14).
- Zone freshness ladder **1.0/0.6/0.3/0.15** (anchors keep the original ladder — spec: anchors no-decay).
- Planner prompt: **≤5-line noise-filter gate** rule.

## B4 — level_stats nightly (`store/level_stats.go` + `trader/ninjatrader/level_stats_wire.go`)

At 17:05 CT roll (+boot): evaluates EVERY level the previous session-day's plans seated against the
day's persisted 1m bars → TOUCHED(±4pts) / REACTED(≥8pt in 3 bars) / BROKE-CLEAN / CHOPPED
(`kernel.EvaluateLevelOutcome`, pure + tested). Aggregates by grade/family — the forward-validation
table; the 2-week verdict on the volume family's weights reads `AggregateByGrade/Family`.

## ADDENDUM (1) — role grammar (`kernel/levels_role.go`)

Machine-assigned `level_role` per kind, env-overridable (`LEVEL_ROLE_MAP`):
- `magnet_meanrevert`: VWAP/eVWAP/pdVWAP/POC/nPOC/GAP
- `liquidity_break`: ONH/ONL/EQH/EQL/IB/OR/AS/LDN edges
- `react_zone`: PDH/PDL/VAH/VAL/RTH/fresh zones/PW/PM
- `target_only`: consumed · far-HTF (state overrides)
- `pivot`: PDC/SETT/MID-O/RN

Rendered as a ROLE column in the planner ranked table + executor KEY LEVELS + 5-line playbook legend.
Validator **WARN** (never fail) on scenario-vs-role mismatch (`RoleMismatches` → `🧭 role mismatch:` journal lines).
Wakes/births (W6 + mss) untouched.

## ADDENDUM (2) — bias-context line (`kernel/levels_role.go`, `ComputeBiasContext`)

One facts-only line per cycle in BOTH prompts: price vs VWAP · vs PDC · inside/above/below value area ·
nearest magnet · nearest liquidity. AI judges direction.

## Boot line

```
🎛 volume wave: detectors=on · seats=8 · proximity=1.5 · family-confluence(cap=3) · zone-ladder=1.0/0.6/0.3/0.15 · roles=on(overrides=false) · bias_ctx=on
```

## Proofs

- **E1** — seated table (≤8 rows, ROLE column, volume family seated). Cycle 13:46:00 UTC (price 29288.00):
```
  29281.00  OR-H                 A  fresh·x76 liquidity_break   -7.0
  29310.00  ONH                  A  fresh·x23 liquidity_break  +22.0
  29228.50  PDC                  B  flipped·x68 target_only     -59.5
  29174.25  OR-L                 A  fresh·x29 liquidity_break -113.8
  29416.00  RTH-H                A  fresh·x4  react_zone      +128.0
  29420.00  PDH                  A  fresh·x14 react_zone      +132.0
  29095.50  PDL                  A  fresh·x19 react_zone      -192.5
  29553.12  nPOC·wk·2026-08-18   A  fresh·x3  magnet_meanrevert +265.1
```
- **E2** — dynamic VWAP re-emit, two consecutive cycles: VWAP `29251.89` (13:46:00) → `29252.36` (13:48:28);
  bias_ctx distance to VWAP moved `+36.1 → +59.1` as price repriced (29288.00 → 29311.50).
- **E3** — full prompt quote (13:48:28):
  `bias_ctx: price 29311.50 · 59.1 vs VWAP 29252.36 · 83.0 vs PDC 29228.50 · above value area (29195.55–29295.61) · nearest magnet nPOC·wk·2026-08-18 (+241.6) · nearest liquidity OR-H (+44.2)`
  + KEY LEVELS ROLE column + `role playbook` legend (magnet/liquidity/react/target/pivot).
  Planner side locked by `TestPlannerPromptCarriesRoleAndBias` (role column, playbook, bias_ctx, NOISE FILTER).
- **E4** — role-mismatch WARN: `TestRoleMismatchesWarn` (magnet+breakout_retest, liquidity+reject → 2 WARN lines);
  live journal emits `🧭 role mismatch:` per `RoleMismatches` at plan write (WARN-only, never a fail).
- **E5** — soak: 15 cycles in the first 48 min post-cutover, cycles flowing every ~2 min through 13:52, 0 entries
  (market in balance inside value area; every cycle an honest wait). No new suppression paths: the wave adds
  detectors/scoring/prompt text/statistics only — ZERO gate changes in validateDecision.

## Live regression caught by the owner during E1: card "dist wrong again"

The plan card rendered every level's distance as its NEGATIVE PRICE (PDL → −29095). Root cause: after a
re-plan, `birthMs ≈ now` → `BarsSince(bars, birthMs)` is EMPTY → `referenceClose` = 0 →
`DistancePoints = 0 − level`. The 2026-08-24 guard fixed the dir reference only. Fixed (`6be7a88b`): both the
card endpoint (`planLevelFacts`) and the executor PLAN STATUS compute distance against the zero-guarded
current price (`SignedDistancePoints(price, level)`); the sweep/acceptance fields keep the birth-scoped bars.
Verified live: the same rows now read `+189.0 / +110.2 / +72.0 / +3.5 / −25.5 / −131.5 / −254.9 / −268.6` vs price 29284.5.

## Tests

`TestSessionVWAPMoves`, `TestPriorDayProfileCached`, `TestNakedPOCRetireOnTouch`, `TestRoleAssignment`,
`TestBiasContextLine`, `TestSeatVolumeFamily`, `TestEvaluateLevelOutcome`, `TestHTFZoneGradesB` (B2 gate),
`TestRoleMismatchesWarn` (E4), `TestPlannerPromptCarriesRoleAndBias` (E3 planner side).
`go test ./...` EXIT=0 at every gate. Deploy sequence (flat each time): `eea1e024 → 54e641cf → 7c48aff7 → 2543f9dd → 02f10942 → 61814630 → 6be7a88b`.
