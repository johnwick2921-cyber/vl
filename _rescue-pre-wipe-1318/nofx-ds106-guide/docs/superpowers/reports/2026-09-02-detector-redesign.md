names rewritten to vl on 2026-09-30 (VL rename)
# Touch/Bounce Detector — Why It Is Biased, and a Symmetric Replacement

**Lane 4 · READ-ONLY · no lock · no engine code · offline analysis**
Owner: hoang · Live rev `0d093c3b` · Report opened **2026-09-02 07:44:10 CDT**
Scripts: `~/vl-analysis/detector-redesign/` (never under `~/vl`, per A1)

---

## 0. PRE-REGISTRATION (A7) — written **2026-09-02 07:44:10 CDT**, BEFORE any shuffle was run

Fixed in advance, before observing any E1–E5 output:

**Instrument under test (D1).** An episode BEGINS on the first 1m bar whose range
comes within `k·Δ` of level L from one side; `entry_side` = the side of L on which
the *previous* bar's close sat (never the touching bar's own close — that is the
live detector's tautology). The episode ENDS on the first bar whose range clears
the band entirely through one boundary. `HOLD` = exited through the boundary it
entered from. `BREAK` = exited through the far boundary. `AMBIGUOUS` = a single bar
spans both boundaries, or the max-bars horizon (`H = 12`, matching the live
`TOUCH_EPISODE_MAX_BARS`) elapses with price still inside. Same horizon, same
threshold, both sides. No wick special-casing in the base variant.

**Rate definition.** `p(hold) = HOLD / (HOLD + BREAK)`. AMBIGUOUS episodes are
recorded and counted but EXCLUDED from the denominator. Every rate is reported
with n and a Wilson 95% interval.

**E1 calibration criterion (the pass/fail rule).** Rebuild an IID-shuffled price
path from the real 1m close increments (sampling without replacement, same
multiset of returns, same start price). Run D1 for
`k ∈ {0.5, 1, 1.5, 2, 3, 4, 6}` against the real level map.
→ **PASS iff at least one k yields p(hold) whose point estimate lies within
0.50 ± 0.03** (i.e. 0.47 ≤ p̂ ≤ 0.53). The full curve is reported regardless.
A detector that cannot be driven to 0.50 on memoryless data at ANY k is
declared structurally biased and rejected.

**Chosen-k rule.** If several k pass, the chosen k is the one whose p̂ is
closest to 0.500; ties broken toward the larger k (more episodes per level).

**E3 edge rule.** Real tape "shows an edge" ONLY if its p(hold) Wilson interval
at the chosen k lies strictly ABOVE the E2 stationary-bootstrap p(hold) point
estimate. Overlap ⇒ no demonstrated edge.

**Ranking floor.** No per-kind verdict is stated below n = 200 episodes; kinds
with 30 ≤ n < 200 are reported as descriptive only and explicitly labelled
"too small to rank", with Cohen's h for 0.60 vs 0.50 quoted as the yardstick.

**Nothing below this line was known when the criterion above was written.**

---

## 1. C1 — The live predicate, quoted

Two DIFFERENT biased instruments are in production. Both were quoted as "reaction rates".

### [A] `kernel/touch_telemetry.go` — episode shapes (`rejection`/`acceptance`)

| Element | Code | Value (RESOLVED, A4) |
|---|---|---|
| Touch band | `minBarDist(last, l.Price) <= TouchBandPoints()` — `touch_telemetry.go:162,164,214` | `band=16t(4.0pt)` — boot line `kernel/levels_volume_boot.go:15`, no `TOUCH_BAND_TICKS` in `.env`, `systemctl show vl -p Environment` empty ⇒ default |
| Touch geometry | `minBarDist` = 0 if `Low<=L<=High`, else gap from High/Low — **wick-based** | ±4.00 pt |
| Approach side | `ep.approachFrom = approachSide(last.Close, l.Price)` — `:171,225` | `"below"` iff `close < L` |
| Bounce verdict | `ep.Close1m = closeSide(last.Close, l.Price, ep.approachFrom)` — `:194,265` | from below: `close <= L` ⇒ `reject` |
| Episode end | `if dist > band \|\| ep.BarsIn >= maxBars` — `:198` | `max_bars=12` |
| Shape | `classifyShape` — `:358`: `accept && BodyPen>0 ⇒ acceptance`; `reject ⇒ rejection`; else `chop` | binary in practice (`chop` n=0 live) |

### [B] `kernel/level_stats_calc.go` — the `reacted` flag that produced 84% / 70.3%

```go
// :69-74
for i := first + 1; i <= first+LevelReactBars && i < len(bars); i++ {
    if math.Abs(bars[i].Close-levelPrice) >= reactPts {   // ← DIRECTION-BLIND
        o.Reacted = true
```
`LevelTouchTolPoints=4.0 · LevelReactPoints=8.0 · LevelReactBars=3 · LevelBreakLookback=12` (`:19-25`, **hardcoded consts — NOT wired to `TOUCH_BAND_TICKS`**).

## 2. C2 — Why it returns ~0.75 on memoryless data

**Named cause (A): the approach side and the verdict are the same sign test on the same
quantity, evaluated at two times.** `approachFrom = sign(close_open − L)`;
`Close1m = sign(close_exit − L)` compared back against it. `reject` therefore means
*"the close is still on the side it was already on"* — which for a driftless walk started
at any non-zero offset d is `Φ(d/σ) > 0.5` by construction, rising to 1 as d grows. It
cannot return 0.50 on noise at any band width. Measured, real level map, IID-shuffled tape,
R=40: **p(rejection) = 0.7357 [0.7329, 0.7386], n=92,845.**

| offset of opening close from L | P(a later close still on that side) |
|---|---|
| 0.25σ | 0.5987 |
| 0.50σ | 0.6915 |
| 1.00σ | 0.8413 |
| 1.50σ | 0.9332 |

σ(1m Δclose) = **7.97 pt**; the band is 4.00 pt = **0.50σ** — *narrower than one bar's move*,
so the opening close typically sits ~0.5σ or more from L: ≈0.69 before any market
behaviour is involved.

**Named cause (B): `math.Abs` — the break IS counted as a bounce.** `reacted` fires on an
8-point move away from the level **in either direction** within 3 bars. Price blasting
straight through the level by 8 points is recorded as a "reaction". With σ=7.97 pt/bar:
P(|move| ≥ 8) = 0.316 after 1 bar, 0.478 after 2, 0.562 after 3 — cumulatively the
~0.75–0.85 observed. Measured on shuffled data: **p(reacted|touched) = 0.7593 [0.7463, 0.7719], n=4,313.**

**Named cause (C): asymmetric horizons.** `reacted` = 3 bars, either side, no persistence
requirement. `broke_clean` = a close ≥8 pt through **and never returning within ±4 pt for
12 bars** (`:80-96`). Bounce is cheap; break must survive four times longer.

**Named cause (D): the tie goes to the bounce.** `closeSide` uses `<=` from below and `>=`
from above (`:265-276`) — `close == L` is `reject` on both sides.

## 3. C3 — The three coexisting touch definitions (D15)

| # | Definition | Site | Geometry |
|---|---|---|---|
| 1 | `minBarDist(b,L) <= TouchBandPoints()` | `kernel/touch_telemetry.go:162,214` | ±4.00 pt band, **env-tunable** |
| 2 | `b.Low <= L+tol && b.High >= L-tol` | `kernel/level_stats_calc.go:50-52` | ±4.00 pt band, **hardcoded const** |
| 3 | `b.Low <= L && b.High >= L` | `kernel/plan_lifecycle.go:190` + `kernel/levels_role.go:134` | **zero-tolerance straddle** |

(1) and (2) are algebraically equivalent *today* but not linked: retuning `TOUCH_BAND_TICKS`
moves the telemetry band and leaves `level_stats` at 4.0. **`level_stats` uses definition (2).**

## 4. E1 — the pre-registered calibration: **FAILED**

D1 exactly as pre-registered (episode opens at BAND ENTRY). IID shuffle of bar shapes
relative to the previous close (preserves gaps + intrabar geometry, destroys serial
dependence), real level map, 6 session-days × 40 reps.

| k | band (pt) | hold | break | amb | n | p(hold) | Wilson 95% | amb % |
|---|---|---|---|---|---|---|---|---|
| 0.5 | 5.38 | 47120 | 20764 | 20556 | 67884 | 0.6941 | [0.6906, 0.6976] | 23.2% |
| 1 | 10.75 | 67063 | 18605 | 8140 | 85668 | 0.7828 | [0.7801, 0.7856] | 8.7% |
| 1.5 | 16.13 | 71013 | 13632 | 3360 | 84645 | 0.8390 | [0.8365, 0.8414] | 3.8% |
| 2 | 21.51 | 76421 | 11463 | 1776 | 87884 | 0.8696 | [0.8673, 0.8718] | 2.0% |
| 3 | 32.26 | 81014 | 7760 | 2082 | 88774 | 0.9126 | [0.9107, 0.9144] | 2.3% |
| 4 | 43.02 | 81985 | 4629 | 3830 | 86614 | 0.9466 | [0.9450, 0.9480] | 4.2% |
| 6 | 64.52 | 78424 | 1345 | 6322 | 79769 | 0.9831 | [0.9822, 0.9840] | 7.3% |

**PASS set: NONE.** Monotone increasing in k; never approaches 0.50. **Per §0 the
band-entry design is declared structurally biased and rejected — the same disease as the
live detector, milder.** Diagnosis: opening the episode at the band EDGE places price
adjacent to the entry-side barrier, so re-crossing it is far likelier than traversing the
full band (gambler's ruin from a non-central start).

### AMENDMENT (post-hoc, written 2026-09-02 after E1 failed — declared, not hidden)

**D1′:** anchor the barriers to the LEVEL and start the episode when price actually
TOUCHES the level (`bar.Low <= L <= bar.High`, previous bar not touching). Barriers `L ± k·Δ`
are then equidistant from the start, so a driftless walk is a coin flip by construction.
`entry_side` from the previous (wholly one-sided) bar. Same criterion as §0.

| k | ±m (pt) | n | p(hold) | Wilson 95% | amb % | | k | n | p(hold) | Wilson 95% | amb % |
|---|---|---|---|---|---|---|---|---|---|---|---|
| **exit_on = close** ||||||| **exit_on = range** |||||
| 0.5 | 2.69 | 86219 | 0.5163 | [0.5130, 0.5197] | 0.1% | | 0.5 | 66817 | 0.5256 | [0.5218, 0.5294] | 24.7% |
| 1 | 5.38 | 83003 | 0.5138 | [0.5104, 0.5172] | 0.3% | | 1 | 83728 | 0.5175 | [0.5141, 0.5209] | 9.8% |
| 1.5 | 8.07 | 66731 | 0.5101 | [0.5063, 0.5139] | 1.6% | | 1.5 | 77470 | 0.5163 | [0.5127, 0.5198] | 4.9% |
| 2 | 10.75 | 58219 | 0.5124 | [0.5084, 0.5165] | 5.1% | | 2 | 73169 | 0.5156 | [0.5120, 0.5192] | 3.1% |
| **3** | **16.13** | **42608** | **0.5067** | **[0.5019, 0.5114]** | **17.8%** | | 3 | 56922 | 0.5096 | [0.5055, 0.5137] | 6.9% |
| 4 | 21.51 | 31133 | 0.5147 | [0.5091, 0.5202] | 34.9% | | 4 | 42433 | 0.5150 | [0.5103, 0.5198] | 19.7% |
| 6 | 32.26 | 15809 | 0.5077 | [0.4999, 0.5155] | 63.4% | | 6 | 21948 | 0.5081 | [0.5015, 0.5147] | 50.6% |

**PASS: every k, both variants.** Chosen by the §0 rule (closest to 0.500, ties to larger k):
**k = 3, ±16.13 pt, exit_on=close, p_null = 0.5067.** A small residual +0.005…+0.016 bias
persists at every k (the entry bar's close sits marginally on the entry side); it is
carried into E2/E3 as part of the null, not corrected away.

## 5. E2–E5 — results at the chosen k

**E2 — stationary bootstrap (autocorrelation baseline).** 1m return autocorrelation:
lag1 ρ = **−0.0361** (2σ band ±0.0168 — significant, mild microstructure mean-reversion),
lag2 = +0.0077 (inside band). Geometric blocks, mean length 10 (sensitivity 5/20 shown).

| mean block | p(hold) | Wilson 95% | n | amb % |
|---|---|---|---|---|
| 5 | 0.5187 | [0.5132, 0.5242] | 31770 | 20.8% |
| **10** | **0.5189** | **[0.5133, 0.5244]** | 31641 | 20.6% |
| 20 | 0.5121 | [0.5066, 0.5177] | 31363 | 21.3% |

→ **BASELINE = 0.5189.** Real levels must beat this to show anything beyond 1m autocorrelation.

**E3 — real tape, all seated kinds pooled.** Query: `level_stats` rows (145 level-days,
6 session days 2026-08-24 … 2026-08-31, trader `8d5c8af5_8ef641a7…`) against MNQ 1m bars.

> **p(hold) = 0.5177, 95% Wilson [0.4889, 0.5463], n = 1159** (hold 600 / break 559);
> 340 ambiguous of 1499 episodes = **22.7 % ambiguous share (D2)**.
> vs baseline 0.5189 → **DOES NOT CLEAR.** The CI contains, and is centred essentially on,
> the autocorrelation baseline.

Per KIND (n ≥ 30; the §0 floor forbids a verdict under n = 200 — **every kind is below it**):

| kind | n | hold | break | p(hold) | Wilson 95% | label |
|---|---|---|---|---|---|---|
| VWAP | 147 | 68 | 79 | 0.4626 | [0.3840, 0.5431] | too small to rank |
| OB | 131 | 72 | 59 | 0.5496 | [0.4642, 0.6322] | too small to rank |
| EQL | 99 | 51 | 48 | 0.5152 | [0.4180, 0.6112] | too small to rank |
| EQH | 98 | 48 | 50 | 0.4898 | [0.3931, 0.5873] | too small to rank |
| PDC | 96 | 50 | 46 | 0.5208 | [0.4220, 0.6180] | too small to rank |
| ONL | 68 | 35 | 33 | 0.5147 | [0.3983, 0.6295] | too small to rank |
| OR-L | 54 | 29 | 25 | 0.5370 | [0.4061, 0.6631] | too small to rank |
| DEMAND | 53 | 26 | 27 | 0.4906 | [0.3612, 0.6212] | too small to rank |
| SWG-H | 53 | 30 | 23 | 0.5660 | [0.4327, 0.6905] | too small to rank |
| SUPPLY | 48 | 22 | 26 | 0.4583 | [0.3258, 0.5971] | too small to rank |
| POC | 47 | 24 | 23 | 0.5106 | [0.3724, 0.6472] | too small to rank |
| OR-H | 39 | 16 | 23 | 0.4103 | [0.2708, 0.5658] | too small to rank |
| SWG-L | 36 | 18 | 18 | 0.5000 | [0.3447, 0.6553] | too small to rank |
| RTH-H | 34 | 17 | 17 | 0.5000 | [0.3407, 0.6593] | too small to rank |
| PDL | 33 | 23 | 10 | 0.6970 | [0.5266, 0.8262] | too small to rank |

Cohen's h(0.60 vs 0.50) = **0.2014** ⇒ **n ≈ 193 per group** for 80% power. PDL's 0.6970
is the only kind whose interval excludes 0.50, at n=33 with 15 kinds tested — expected by
multiplicity alone. **No kind is rankable.**

**E4 — Osler random-level bootstrap.** B = 1000 random level sets, same per-day count,
prices drawn uniformly from each session-day's own bar range.

> random-level p(hold): mean 0.5106, sd 0.0140, p05 0.4880 / median 0.5103 / p95 0.5351.
> The real map's 0.5177 sits at the **70.1st percentile** — inside the bulk, far from the
> 95th. **Real levels are not distinguishable from randomly-placed prices.**

**E5 — the LIVE definitions on the SAME tape and SAME levels (side by side).**

| instrument | statistic | value | Wilson 95% | n |
|---|---|---|---|---|
| **New D1′ (k=3)** | p(hold) | **0.5177** | [0.4889, 0.5463] | 1159 |
| Live `touch_telemetry` | p(rejection) | **0.7076** | [0.6896, 0.7250] | 2538 |
| Live `level_stats` | p(reacted \| touched) | **0.8190** | [0.7390, 0.8784] | 116 of 133 level-days |
| *(live, on shuffled tape)* | p(rejection) | *0.7357* | [0.7329, 0.7386] | 92845 |
| *(live, on shuffled tape)* | p(reacted \| touched) | *0.7593* | [0.7463, 0.7719] | 4313 |

The live instruments report ≈0.71 / 0.82 on the real tape and ≈0.74 / 0.76 on **pure noise**.
Their signal is construction bias; the market contribution is within noise of zero.

**D3 — touch ordinal recomputed from the tape** (the live ordinal lives in an in-memory
`touchRegistry` — `touch_telemetry.go:111` — and resets on restart, so it was not used):

| ordinal | p(hold) | Wilson 95% | n |
|---|---|---|---|
| 1st | 0.4767 | [0.3745, 0.5810] | 86 |
| 2nd | 0.4789 | [0.3668, 0.5931] | 71 |
| 3rd | 0.5132 | [0.4029, 0.6222] | 76 |
| 4th+ | 0.5248 | [0.4926, 0.5568] | 926 |

No monotone "levels weaken with retests" pattern; all intervals cover 0.50.
Episode shape: bars_to_exit median 4 (mean 4.42, max 12 = horizon); MFE median +18.50 pt,
MAE median −19.00 pt inside the band.

## 6. Verdict

**The instrument is now calibrated; the tape still shows nothing.** D1′ returns 0.5067 on
memoryless data (was: 0.69–0.98 for the pre-registered band-entry design, and 0.74–0.76 for
both live detectors) — it is the first level detector in this system that can be driven to a
coin flip on noise, which is the minimum bar for any rate it reports to mean anything. Run on
the real tape at the chosen k it returns **p(hold) = 0.5177 [0.4889, 0.5463], n = 1159**,
against a stationary-bootstrap baseline of **0.5189** — the real interval straddles the
baseline, so **at pooled n the MNQ tape shows no level edge beyond 1m autocorrelation**, and
the Osler random-level test agrees (70.1st percentile, not extreme). This corroborates 1C on
a rebuilt instrument rather than the biased one, so the conclusion no longer depends on the
detector under suspicion. Every per-kind cell is far below the pre-registered n=200 floor
(largest: VWAP n=147), so **no level-kind ranking is stated, and none should be quoted from
this table** — 15 kinds at n≈30–150 will always produce one or two apparently-significant
cells (here PDL 0.6970, n=33). The honest summary: **every reaction rate this system has
published — 84% anchors, 70.3% seated, the 75.1% `rejection` share in `touch_episodes` — is
an artefact of the predicate, not a property of the market**; and the corrected instrument,
on the 6 session-days of level data that exist, cannot yet distinguish the seated map from
randomly-placed prices. That is a statement about statistical power as much as about
markets: 145 level-days is not enough to rank anything, and the fix for that is more stored
level-days, not a different threshold.

## 7. What a 1B implementation would change

**Files (none touched by this lane):**
- `kernel/level_stats_calc.go` — replace `EvaluateLevelOutcome`'s direction-blind `reacted`
  (`math.Abs`, :70) with the D1′ episode walk; keep `broke_clean`/`chopped` as separate
  descriptive fields, never as the bounce complement.
- `kernel/touch_telemetry.go` — `approachFrom` must come from the bar BEFORE the touching
  bar (:171), not from `last.Close`; `classifyShape` (:358) becomes hold/break/ambiguous.
- Unify the three touch geometries (C3) on one resolver-backed helper so
  `TOUCH_BAND_TICKS` moves all of them or none.
- Persist the ordinal (`touch_episodes.touch_number` already exists) instead of the
  in-memory `touchRegistry` counter that resets on restart.

**Touch-outcome record schema (new table `touch_outcomes`):**

| column | type | note |
|---|---|---|
| `level_key`, `trader_id`, `session_day`, `symbol` | text | join keys |
| `kind`, `grade`, `family` | text | from the seated level |
| `level_price` | real | |
| `ordinal` | int | recomputed per level per session from the tape |
| `k`, `band_pts`, `horizon_bars` | real/int | the resolved parameters this row was judged under |
| `entry_side` | text | `below` \| `above` (from the pre-touch bar) |
| `exit_side` | text | `below` \| `above` \| NULL |
| `outcome` | text | `hold` \| `break` \| `ambiguous_span` \| `ambiguous_horizon` |
| `bars_to_exit` | int | |
| `mfe_pts`, `mae_pts` | real | excursion inside the band |
| `opened_at_ms`, `closed_at_ms` | int | |

Rates are then computed as `hold/(hold+break)` with ambiguous **counted and excluded** —
never silently folded into either side.

**Parked:** the 1C band recommendation stays PARKED until this instrument replaces the old
one. No knob was written, no engine file touched, no restart performed by this lane.

## 8. Scripts and reproduction

All under `~/vl-analysis/detector-redesign/` (outside the repo, per A1):
`detectors.py` (library: D1, D1′, both live replications, Wilson, shuffle, stationary
bootstrap) · `run_e1.py` (pre-registered E1) · `run_e1b.py` (amended E1′) ·
`run_e2345.py` (E2/E3/E5) · `run_e4.py` (Osler + D3) · `run_c2.py` (C2 mechanism) ·
inputs `tape_mnq_1m.json` (MNQ 1m, n=13,666, 2026-08-19 15:00 → 2026-09-02 12:42 UTC,
0 duplicate timestamps), `levels_real.json` (145 level-days), `tape_stats.json`
(Δ=5.3771 pt = 21.51 ticks over n=13,665 returns; σ=7.9722 pt; autocorrelation table).
Seeds are fixed in each script (20260902 / 777 / 4242 / 31337).

## 9. Closeout — A8 SATISFIED (HTTP 200)

Owner standing instruction "always push" (2026-09-02): rebased onto `origin/dev` and
pushed (`15b01369..1dc958da dev -> dev`); branch in sync. The A8 commit-ref raw URL then
returned **HTTP 200**:

```
https://raw.githubusercontent.com/johnwick2921-cyber/nofx/<report-commit>/docs/superpowers/reports/2026-09-02-detector-redesign.md
HTTP 200
```

Correction of record: the pre-push 404 in the first closeout was attributed to *two*
causes — unpushed commit AND a private repo. Only the first was real. `origin` serves
anonymous raw content, so the single blocker was that the commit had not left this
machine. A8 is met.

**Surprises (A9 — included, acted on in none):**
1. The pre-registered D1 failed its own calibration (§4) — the band-entry design carries a
   milder form of the same bias it was meant to replace.
2. `touch_episodes` shows `chop` n=0 across 943 live rows: `classifyShape` is binary in
   practice, so the schema's third bucket is dead.
3. `level_stats` covers only **6 session-days / 145 level-days** (2026-08-24…08-31) —
   the binding constraint on every per-kind question is stored sample size, not method.
