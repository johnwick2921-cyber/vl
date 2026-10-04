> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# 1H WAVE + R2 GRADING + R4 QUALITY KNOB — IMPLEMENTATION REPORT (2026-08-25/26)

PR: https://github.com/johnwick2921-cyber/vl/pull/75 · Branch: `feat/1h-wave-grading`
Deployed rev: **57b60b60** · PID 1991583 · PR #75 OPEN (base `dev`)

---

## 0 — TRIAGE VERDICT (Phase 0)

**v9 fail-closed cause:** DeepSeek **300s client timeout** — not no-JSON, not a new
class. Log proof: `ai_call model=deepseek-v4-pro duration_ms=300001 deadline_s=300
err="context deadline exceeded"` at 21:27:26 and 21:36:44 CT → `PLANNER FAIL-CLOSED
2026-08-25 ASIA` → v9 `no_trade/planner_fail_closed` (21:36:44 CT). The owner reset
at 21:22:25 re-armed the budget correctly (C7); the fresh read itself timed out.

**Session state:** ASIA was DARK from 21:36 CT. **REVIVED** before the wave:
- 21:57 CT — `AI_HTTP_TIMEOUT_SECONDS=600` + `AI_MAX_RETRIES=2` appended to `.env`,
  bot restarted (boot line 21:57:44 quotes `timeout=600s retries=2`).
- ~23:55 CT — owner reset → **v10 `owner_reset` ACTIVE** (plan written 04:55 UTC).
- 00:04 CT — **v11 `level_event` ACTIVE** (a W6 wake fired and produced a fresh
  plan — wakes budget-free + non-fatal working live).
- 00:20:56 CT — post-deploy wake on the NEW binary: "level wake seated Supply·1h
  invalidated: close 29239.00 above 29209.25 … waking the planner (W6, 5th wake-up)".

**R6 verdict:** the 600s timeout is applied and effective (env proof quoted above);
the planner-only longer client was NOT built — stays queued hardening.

**Baseline:** deployed rev at dispatch time was 93412ce8 (not 004d11ce); running
PID 1921663, boot `BOOT INTEGRITY OK — rev 93412ce8188a · goldens PASS`.

---

## 1 — PHASE 1: 1H WAVE (R1) — per the research edit map

Citation: `docs/superpowers/reports/2026-08-25-1h-timeframe-research-wave.md`
synthesis §4 items 1–5 + edit-site map §5 (R13–R17).

- **Evidence table** `kernel/levels_score.go` (`zoneEvidenceByKind`): 1h OB
  0.60→**0.70**; 1h Supply/Demand/FVG/iFVG 0.55→**0.65**. 15m + 4h untouched.
- **Floor/cap switch** (`levels_score.go`): split the shared `"15m","1h"` case —
  `15m` stays floor B cap B; `1h` → floor B **cap A**; `4h` unchanged.
- **1h seat guarantee**: new `kernel.Seat1HZone` post-pass + `is1HSDZone` — reserves
  one of `maxHTFSeats=2` for an in-band 1h S/D zone when one exists (TF survives
  via `DetectedLevel.TF`; demotes the weakest non-priority, non-HTF head entry).
- **Planner mandate**: `plannerOutputContract` gains `has1HSDZone` — the 1h
  MUST-include rule is emitted ONLY when a 1h S/D row actually renders (same
  conditional pattern as the G2.2 HTF mandate fix).
- **HTFZones cap-4 survival**: `trader/auto_trader_planner.go` applies
  `Seat1HZone(zs, 4)` on the zone section, gated by the knob.
- **Knob** `seat_1h_zone` pointer-bool **DEFAULT ON** (R16 7-touchpoint pattern):
  Go struct + default + `Seat1HZoneEnabled()` accessor; TS `strategy.ts` type;
  `DayPlanEditor.tsx` DEFAULT + toggle; `plan-translations.ts` i18n; engine KEY
  LEVELS gate. 15m stays B (R20).
- **R3 (document only)**: TFmult double-count documented in the v3 comment block
  — effective 4h:1m ≈ **2.3×** (0.72/0.40 × 1.3/1.0). `zoneTFMult` NOT removed.
- **Tests**: hard breaker `levels_htf_test.go` 1h **B→A** flipped (+15m B and 1m C
  pinned); new `TestSeat1HZonePromotesInBandSD`; prompt mandate tests
  (`TestPlannerPrompt1HMandateConditional`).

## 2 — PHASE 2: GRADING FIXES (R2)

Citation: `docs/superpowers/reports/2026-08-24-level-grading-full-audit.md` §4.

- **4.4 FVG**: detection floor is now `max(2×tick, 2.0 pts)` (`FVGNoiseFloorPoints`
  + `fvgMinGapPoints`) — the 1×ATR gap requirement is gone; size weighting already
  applied at scoring via `zoneSizeMult`. Tests: `TestFVGMinGapNoiseFloor`.
- **4.5 OB lookback**: bounded scan, env `OB_LOOKBACK_BARS` default **8**
  (`OBLookbackBarsDefault` + accessor). Test: `TestOBLookbackBounded`.
- **4.6 seatBothSides + 4.7 minGrade**: new `ScoreLevelsMinGrade` /
  `AssembleScoredLevelsMinGrade` / `BuildKeyLevelsBlockOpts` — the scorer runs on a
  **2× pool**, filters by minGrade, then re-seats with `seatBothSides` so the cut
  REFILLS seats (a min_grade cut can no longer leave a one-sided table). minGrade
  now applies to: planner table (already), **executor KEY LEVELS**, **PLAN STATUS**
  (`RenderPlanStatusMinGrade`), **level-state writers**, **fail-closed maps**
  (`noTradeLevelMap(session)`). `DayPlanConfig.MinGradeFor(session)` is the ONE
  resolution seam. Test: `TestScoreLevelsMinGradeRefillsSides`.
- **§5 live gap**: VERIFIED ALREADY FIXED in `c1cf4fdb` — HTF-zones rows ARE merged
  into the `machineGrades`/`machineLabels` maps at the write site (Supply·4h-class
  rows get stamped). No new code needed.
- **4.8**: prompt quality string `"A+|A|B|C"` + a Rules line documenting C =
  machine-demoted (G5). Test asserts both.

## 3 — PHASE 3: QUALITY-C KNOB (R4)

- `DayPlanConfig.MinScenarioQuality` (base, default **C**) + per-session override
  `MinScenarioQuality` in `DayPlanSessionOverride` + `MinScenarioQualityFor(session)`.
- Kernel gate: `MinScenarioQualityVerdict` — after the C6 dead-plan gate; blocks
  only entries citing a scenario graded below the floor; **fail-open** on
  off-plan/unknown citations and management actions; floor C = dormant
  (byte-identical today). `ctx.MinScenarioQuality` + `ctx.PlanScenarioQuality`
  populated each cycle in `auto_trader_loop.go`. Test: `TestMinScenarioQualityVerdict`.
- FE: `DayPlanEditor` segmented A/B/C (strategy-level + per-session override row);
  TS types; i18n keys. Boot observability line added:
  `🗺️ day-plan knobs: seat_1h_zone=true min_scenario_quality=C ob_lookback_bars=8`.

## 4 — PHASE 4: DOCS REFRESH (docs-only commit)

`docs/PIPELINE-MAP.md` + `docs/regime-wave/gate-order.md`: G6 refs purged, planner
wake-ups now list the W6 level-event wakes (unlimited/budget-free/non-fatal,
30-min throttle), HTF zones + 1h seat + conditional mandate, R4 gate added to the
gate chain, knobs ledger updated, **"as-built at the 1h-wave cutover rev"** header.
`docs/VL-VERIFICATION-CHECKLIST.md`: G6 row struck (removal noted).

## 5 — PHASE 5: CUTOVER + BOOT PROOF

Flat window: 00:18 CT — zero open positions, ASIA v11 active. Order:
build → RELEASE marker → restart.

```
🔐 BOOT INTEGRITY OK — rev 57b60b60d652 +dirty · built 2026-08-26T05:17:18Z · expected 57b60b60 · goldens PASS
🗺️ day-plan knobs: seat_1h_zone=true min_scenario_quality=C ob_lookback_bars=8
```

(`+dirty` is the untracked `.env.bak` — cosmetic; rev + goldens verified.)

**Post-deploy proof** (v11, the read that ran through the new grading):
two `Supply·1h` rows seated beside 4h A rows — the 1h guarantee live:

```
  29539.38  Supply·1h                B
  29209.25  Supply·1h                B
  29154.38  OB(bear)·4h              A
  29031.12  Demand·4h                A
  28711.25  OB(bear)·4h              A
```

HTF detection read: 328 levels incl. `Supply·1h:6 Demand·1h:7 iFVG·1h:4…`; the
conditional 1h mandate renders only when a 1h S/D row is present (unit-tested).

## 6 — REGRESSION

- `go test ./...` → **EXIT=0** (kernel/store/trader full).
- `go build ./...` clean.
- FE `npx tsc --noEmit` clean · `npx vitest run` → **32 files, 263 tests PASS**.
- Goldens: the ONE hard breaker (`levels_htf_test.go` 1h B→A) flipped deliberately;
  no other golden changed. Diff: 9 commits, 10 Go files + 3 FE files + 3 docs.

## 7 — PHASE 6: CALIBRATION (numbers only — nothing shipped)

### 6.5 min-conf 60 vs 65 (position truth, `entry_confidence`)

| Day | Positions | Excluded (conf<65) | PnL excluded | PnL kept |
|---|---|---|---|---|
| 08-21 | 11 | 5 | −314.0 | −123.5 |
| 08-24 | 5 | 5 | −142.5 | +0.0 |
| 08-25 | 11 | 9 | −241.0 | **+214.5** |
| 08-26 (CT) | 1 | 1 | −52.0 | +0.0 |
| 08-19 | 5 | 4 | +152.5 | +273.5 (exclusion would HURT) |
| 08-20 | 8 | 6 | +270.6 | −235.5 |

Recent days favor 65 strongly (08-24/25: every losing day was conf<65); 08-19/20
are mixed — not a slam-dunk over all history.

### 6.4 HTF_VETO_TF sweep (stored `structure_json`)

| Day | opens | veto @1h | veto @15m |
|---|---|---|---|
| 08-21 | 14 | 0 (no structure_json — regime wave predates) | — |
| 08-24 | 7 | 0 | 4 |
| 08-25 | 11 | 0 | 2 |

4h NOT stored in structure_json → 4h variant not evaluable without re-persisting.

### BLOCKED on stored data (honest)

- **6.1** 08-21 has 628 decision rows (replayable in part); **08-22 has ZERO rows**
  (bot dark that day). Full decision-row replay through the current pipeline needs
  the offline nq_smoke path wired to historical prompts — not done.
- **6.2** swing-k sweep (2 vs 10–20): no historical bars persisted (NT8 BarCache
  is in-memory; structure snapshots store only latest swing per TF).
- **6.3** MSS-FVG ON/OFF: same bar-persistence blocker.
- **6.6** trail mult 2.0 vs 1.5: no per-position trail config or bar data.
  Proxy evidence (MFE≥10 that closed red — givebacks a 1.5× trail would shrink):
  08-24 14:09 MFE+15→−139.5; 08-25 01:43 MFE+20→−84.5; 08-25 07:09 MFE+58→−62.0;
  08-20 08:26 MFE+48→−97.5; 08-21 08:54 MFE+42→−84.5.

## 7B — ADDENDUM: WIN-RATE DIAGNOSTIC PACK (DB-only, Aug 19 → now, pnl_corrected)

n=41 closed positions · 31 scenario-linked · Σ = **−197.4** (win% 29.3, avgW +136.8, avgL −63.4).

### A. Expectancy by scenario condition

| Session | reject | acceptance | sweep_reclaim | breakout_retest | off-plan | unknown |
|---|---|---|---|---|---|---|
| ASIA | 7 · 14.3% · −80.5 | 2 · 0% · −157.4 | 1 · 0% · −52.0 | 1 · 0% · −66.0 | 1 · 0% · −29.5 | — |
| LONDON | 3 · 33.3% · +40.5 | — | — | 1 · 100% · +168.0 | 3 · 33.3% · −160.0 | 4 · 50% · +152.0 |
| NY | 4 · 75% · **+665.5** | — | 2 · 0% · −140.0 | 1 · 0% · −54.5 | — | 2 · 50% · −58.5 |
| TOTAL | 14 · 35.7% · +625.5 | 2 · 0% · −157.4 | 3 · 0% · −192.0 | 3 · 33.3% · +47.5 | 4 · 25% · −189.5 | 6 · 50% · +93.5 |

### B. Expectancy by quality + trigger level

- quality **A**: n=15 · 40.0% · **+360.1** (avgW +165.8 / avgL −70.5)
- quality **B**: n=8 · 12.5% · **−292.0**
- quality **C**: n=8 · 12.5% · **−169.5**
- quality unknown (unlinked/off-plan): n=10 · 40.0% · −96.0
- Trigger level (kind+grade, linked): OR-L/A +229.1 (n=10, 40%) · PWL/A +169.5 ·
  ONH/A **−131.0** (n=14, 21.4%) · EQL/A −116.0 · PDC/A −78.5 · ONL/A −60.5 ·
  PDH/A −62.0 · PDC/B −52.0.

### C. MAE/MFE autopsy

- Losers: **15 STOPPED-TOO-TIGHT** (MFE ≥ 0.5×SL before the stop-out) vs **12
  WRONG-FROM-START** — more than half the losers printed a tradable MFE first
  (e.g. 08-25 02:09 MFE +58.0 → −62.0; 08-24 09:55 MFE +58.5 → −54.5).
- Winners survived real heat: 08-20 09:41 MAE −61.5 → +311.0; 08-20 05:26
  MAE −43.8 → +168.0; 08-25 04:19 MAE −22.5 → +168.0.

### D. Session table

| Session | n | win% | Σ | avg hold |
|---|---|---|---|---|
| ASIA | 15 | 13.3% | **−477.4** | 38.2 min |
| LONDON | 16 | 31.2% | **−250.5** | 48.5 min |
| NY | 10 | 50.0% | **+530.5** | 49.4 min |

### E. CT-hour heatmap

Positive hours: 11:00 +443 (WW) · 09:00 +156.5 · 21:00 +93 · 13:00 +81 ·
04:00 +168. Negative: **07:00 −171.5 (LLL)** · **10:00 −150 (LL)** · 08:00 −134.5 ·
18:00–23:00 ASIA evening mostly red (except 21:00).

### F. The 3 highest-EV rule changes (knob + retroactive projected Σ delta)

1. **`min_scenario_quality = A`** (shipped knob, currently C): retroactive →
   keep only A-quality entries: +360.1 kept, −461.5 avoided → **projected
   Δ ≈ +461.5** over the window (unknown-citation rows fail open and stay).
2. **Disable ASIA** (`sessions[ASIA].enable=false`): **Δ ≈ +477.4** — ASIA is
   13.3% win, −477.4 across 15 entries; NY alone carries +530.5.
3. **`min_confidence = 65`** (config): Δ over Aug 19–26 ≈ **+326.4**
   (08-21 +314 · 08-24 +142.5 · 08-25 +241.0 · 08-26 +52 · offset by 08-19
   −152.5 and 08-20 −270.6 — mixed early, strongly positive recently).

Bonus (unpriceable retroactively): 15/27 losers stopped-too-tight → SL sizing /
1.5× trail review; and condition-type guidance — NY `reject` setups are the
money spot (+665.5) while ASIA rejects lose (−80.5) — a prompt line steering
reject plays to NY hours.

## 8 — OWNER QUEUE

1. TFmult revisit (R3) — after 1h-wave live data.
2. Calibration rulings: min-conf 60 vs 65 (recent evidence favors 65); trail
   mult; swing-k + MSS-FVG + HTF_VETO_TF 4h need **bar persistence** (persist
   decision-time 15m/1h/4h kline snapshots, or NT8-side bar export) before they
   can be replayed.
3. Planner-only longer timeout client (queued hardening beyond the 600s env fix).
4. Merge PR #75 → dev.
5. Stale-doc leftovers: `master-audit-v2` / `research-conformance` (08-22) still
   pre-W6 (accepted as historical).
