# FIX-LEVEL-GRADE — confluence-window measurement (2026-10-14, DS-108)

## Decision

- The default grade direction looks inverted (A breaks clean 41.2% vs C 33.8%) but is **not significant**: every window's A-vs-C broke-clean 95% CI crosses zero on fit AND hold-out.
- ±3pt is the only window that keeps the right sign, but n is too small to call (hold-out A 13 / C 25 touched).
- **No knob ships** in release #21. The `day_plan.confluence_band_pts` change stays on the parked branch.
- Re-measure when the hold-out split has **≥ 60 touched per grade cell** — at the current ±3pt A-cell touch rate (13 touched / 11 hold-out session days ≈ 1.2/day), that is ≈ 40 session days from 2026-10-12, i.e. **~2026-11-20**.
- Branch `fix/level-grade-window` @ `34f2f0a2f` is **parked**, not merged; this docs-only branch carries the measurement so the evidence survives independent of the parked code.

The A/B/C level grade does not separate levels that hold from levels that break.
This report measures whether a tighter confluence band fixes that, and whether
any window's separation survives held-out days.

## Method

- **Re-score** the persisted `candidate_pool` (12,336 rows — the FULL per-read
  candidate set the constructor produced, seated + cut, 2026-09-04..10-14) with
  fixed confluence bands, holding every non-confluence score component fixed:
  `base = stored_score / stored_confluence_multiplier`, then
  `new_score = base × (1 + 0.20 × min(conf, 3))`, `grade = gradeFromScore(new_score)`
  with the zone TF floors/caps and the Tier-1 proximity cap re-applied. This is
  the production formula (`kernel/levels_score.go: scoreLevelsPool`) with one
  variable — the cluster half-width — moved.
- **Join** SEATED candidates to `level_stats` outcomes
  (`touched` / `reacted` / `broke_clean`, `kernel.EvaluateLevelOutcome`).
- **Reference** "0.10×dATR" = the STORED grade (the production band), not a
  re-score.
- **Session days**: `level_stats` spans 2026-08-24..10-12 (880 levels). The band
  sweep joins **497** levels — `candidate_pool` only covers from 2026-09-04, so
  the 9 earlier days (08-24..09-03) have no pool rows. Stated, not hidden.

### Fit / hold-out split (fixed BEFORE looking at hold-out results)

Chronological by CME session day, boundary `2026-09-28`:

- **FIT (24 days)**: 2026-08-24, 08-25, 08-26, 08-27, 08-30, 08-31, 09-01,
  09-02, 09-03, 09-06, 09-07, 09-08, 09-09, 09-10, 09-14, 09-15, 09-16, 09-17,
  09-20, 09-21, 09-22, 09-23, 09-24, 09-27
- **HOLD-OUT (11 days)**: 2026-09-28, 09-29, 09-30, 10-01, 10-04, 10-05, 10-06,
  10-07, 10-08, 10-11, 10-12

The split rule (day `< 2026-09-28` → fit) was fixed in the harness before any
hold-out result was read.

## 1. Baseline — stored grade (0.10×dATR), all 880 levels

| grade | n | share of A | touched | reacted % (n) | broke-clean % (n) |
|---|---|---|---|---|---|
| A | 702 | 79.8% | 512 | 75.6% (387) | 41.2% (211) |
| B | 57 | 6.5% | 39 | 61.5% (24) | 33.3% (13) |
| C | 121 | 13.8% | 74 | 77.0% (57) | 33.8% (25) |

**A breaks clean MORE than C (41.2% vs 33.8%) — the grade is inverted.** C reacts
as often as A (77.0% vs 75.6%). The legacy `reacted` field is the D4-retired
predicate (≥8pt in 3 bars, either side — a blast-through scores as a rejection);
`broke-clean` is the honest "holds" metric used for the A-vs-C comparison.

## 2. Per window × split (seated levels; n · touched · reacted n/% · broke n/% · share-of-A)

`Δbroke` = A broke-clean% − C broke-clean% (negative = A holds better). CI is the
Newcombe (Wilson score) 95% interval on that difference. A cell with <30 touched
is flagged **TOO SMALL TO CALL**.

### 0.10×dATR (stored grade)

| split | grade | n | share | touched | reacted n (%) | broke n (%) | Δbroke [95% CI] |
|---|---|---|---|---|---|---|---|
| FIT (n=270) | A | 226 | 83.7% | 158 | 109 (69.0%) | 63 (39.9%) | −13.5pp [−36.5, +11.0] **TOO SMALL (C=15)** |
| | B | 25 | 9.3% | 20 | 12 (60.0%) | 8 (40.0%) | |
| | C | 19 | 7.0% | 15 | 11 (73.3%) | 8 (53.3%) | |
| HOLD-OUT (n=227) | A | 197 | 86.8% | 145 | 114 (78.6%) | 57 (39.3%) | −17.8pp [−45.9, +15.3] **TOO SMALL (C=7)** |
| | B | 17 | 7.5% | 12 | 7 (58.3%) | 3 (25.0%) | |
| | C | 13 | 5.7% | 7 | 7 (100.0%) | 4 (57.1%) | |

### ±3 pt

| split | grade | n | share | touched | reacted n (%) | broke n (%) | Δbroke [95% CI] |
|---|---|---|---|---|---|---|---|
| FIT (n=270) | A | 65 | 24.1% | 49 | 33 (67.3%) | 20 (40.8%) | −9.2pp [−28.1, +10.7] |
| | B | 138 | 51.1% | 100 | 73 (73.0%) | 37 (37.0%) | |
| | C | 67 | 24.8% | 44 | 26 (59.1%) | 22 (50.0%) | |
| HOLD-OUT (n=227) | A | 27 | 11.9% | 13 | 13 (100.0%) | 2 (15.4%) | −12.6pp [−35.1, +17.5] **TOO SMALL (A=13, C=25)** |
| | B | 159 | 70.0% | 126 | 95 (75.4%) | 55 (43.7%) | |
| | C | 41 | 18.1% | 25 | 20 (80.0%) | 7 (28.0%) | |

### ±5 pt

| split | grade | n | share | touched | reacted n (%) | broke n (%) | Δbroke [95% CI] |
|---|---|---|---|---|---|---|---|
| FIT (n=270) | A | 107 | 39.6% | 77 | 49 (63.6%) | 28 (36.4%) | −11.3pp [−28.9, +6.8] |
| | B | 98 | 36.3% | 74 | 58 (78.4%) | 31 (41.9%) | |
| | C | 65 | 24.1% | 42 | 25 (59.5%) | 20 (47.6%) | |
| HOLD-OUT (n=227) | A | 76 | 33.5% | 52 | 42 (80.8%) | 19 (36.5%) | +7.4pp [−15.8, +27.1] **TOO SMALL (C=24)** |
| | B | 111 | 48.9% | 88 | 67 (76.1%) | 38 (43.2%) | |
| | C | 40 | 17.6% | 24 | 19 (79.2%) | 7 (29.2%) | |

### ±10 pt

| split | grade | n | share | touched | reacted n (%) | broke n (%) | Δbroke [95% CI] |
|---|---|---|---|---|---|---|---|
| FIT (n=270) | A | 147 | 54.4% | 107 | 74 (69.2%) | 41 (38.3%) | −10.4pp [−27.8, +7.2] |
| | B | 63 | 23.3% | 47 | 36 (76.6%) | 19 (40.4%) | |
| | C | 60 | 22.2% | 39 | 22 (56.4%) | 19 (48.7%) | |
| HOLD-OUT (n=227) | A | 130 | 57.3% | 98 | 78 (79.6%) | 37 (37.8%) | +9.2pp [−14.0, +26.9] **TOO SMALL (C=21)** |
| | B | 61 | 26.9% | 45 | 33 (73.3%) | 21 (46.7%) | |
| | C | 36 | 15.9% | 21 | 17 (81.0%) | 6 (28.6%) | |

### ±20 pt

| split | grade | n | share | touched | reacted n (%) | broke n (%) | Δbroke [95% CI] |
|---|---|---|---|---|---|---|---|
| FIT (n=270) | A | 182 | 67.4% | 133 | 95 (71.4%) | 51 (38.3%) | −11.7pp [−29.8, +6.8] |
| | B | 38 | 14.1% | 28 | 18 (64.3%) | 12 (42.9%) | |
| | C | 50 | 18.5% | 32 | 19 (59.4%) | 16 (50.0%) | |
| HOLD-OUT (n=227) | A | 164 | 72.2% | 124 | 96 (77.4%) | 52 (41.9%) | +6.6pp [−18.2, +26.7] **TOO SMALL (C=17)** |
| | B | 32 | 14.1% | 23 | 19 (82.6%) | 6 (26.1%) | |
| | C | 31 | 13.7% | 17 | 13 (76.5%) | 6 (35.3%) | |

## 3. By family (stored grade)

| family | n | touched | reacted % (n) | broke-clean % (n) |
|---|---|---|---|---|
| vwap | 185 | 144 | 73.6% (106) | 41.0% (59) |
| prior | 160 | 98 | 75.5% (74) | 30.6% (30) |
| zone | 159 | 105 | 74.3% (78) | 39.0% (41) |
| other | 156 | 125 | 70.4% (88) | 40.0% (50) |
| overnight | 136 | 96 | 79.2% (76) | 44.8% (43) |
| liquidity | 33 | 22 | 81.8% (18) | 45.5% (10) |
| profile | 25 | 17 | 82.4% (14) | 35.3% (6) |
| round | 25 | 17 | 76.5% (13) | 52.9% (9) |
| anchor | 1 | 1 | 100.0% (1) | 100.0% (1) |

`prior` holds best (30.6% broke); `round` worst (52.9%). Family spreads reaction
by 12pp (70.4%→82.4%) but `reacted` is the retired field; the honest `broke-clean`
family spread is 30.6%→52.9%.

## 4. By touch count (touch_episodes max touch_number)

| touches | n | touched | reacted % (n) |
|---|---|---|---|
| 0 | 781 | 526 | 74.7% (393) |
| 1 | 27 | 27 | 77.8% (21) |
| 2 | 24 | 24 | 75.0% (18) |
| 3+ | 48 | 48 | 75.0% (36) |

No separation.

## Conclusion — stated plainly

1. The default grade is INVERTED on the honest metric: A breaks clean MORE than
   C (41.2% vs 33.8% over 880 levels). A wider confluence band is not the only
   cause (A is 80% of the sample), but tightening it spreads the grades
   (A share 79.8% → 18.5% at ±3pt).
2. ±3pt is the only window whose A-vs-C broke-clean gap is in the
   hold-better direction on BOTH fit (−9.2pp) and hold-out (−12.6pp); every
   wider window inverts on hold-out.
3. **That ±3pt hold-out edge is TOO SMALL TO CALL**: it rests on A=13 and C=25
   touched levels, and every A-vs-C broke-clean difference — at every window,
   fit and hold-out — has a 95% Newcombe interval that crosses zero. There is no
   statistically significant separation on this sample.
4. Therefore: **the band fix is directional only, not proven.** The knob
   `day_plan.confluence_band_pts` is shipped DEFAULT-OFF (≤0 → 0.10×dATR,
   byte-identical), so it changes nothing until the owner sets it. Whether the
   owner turns it on is the owner's call after reading this — the evidence does
   not support a default change.

## Files

- Harness: `cmd/levelgrade-measure/main.go` (read-only; mirrors the scorer
  formula it re-scores).
- Knob: `day_plan.confluence_band_pts` — `store/strategy.go`,
  `trader/auto_trader_planconfig.go:confluenceBandPts`,
  `kernel/levels_score.go:scoreLevelsPool`, registry `KnobLive`,
  guide `web/src/guide/content/levels.ts`.
- Tests: `kernel/levels_score_test.go` `TestConfluenceBandDefaultByteIdentical`
  (default byte-identical) + `TestConfluenceBandKnobSpreadsGrades`.
