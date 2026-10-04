> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# Structural stop and first-zone target

**Owner correction after this boot:** the owner meant the existing DAILY loss limit. The added mandatory per-trade cap is superseded by [the daily-loss correction](2026-09-13-structural-stop-daily-loss.md). The measurements and original boot receipts below remain historical evidence.

**E4: the corrected conservative replay remains negative; geometry does not establish profitability. C1: the ATR floor won 180/200 logged compositions (90%), a selected log cohort spanning 27 distinct plan/version/scenario/leg specifications—not 200 independent arms. No numeric owner risk cap is configured, so actual structural admission refuses every opportunity. Booted to SIM at 2026-09-13 00:21:42 CT; the final boot marker below supersedes the earlier holds. First live composition/refusal proof remains pending the market reopening.**

## C5 — the measured overshoot and the buffer decision

[A] The unchanged detector/read harness reproduced **11,302 shortlisted zone touches across 3,084 reads**, from **2022-04-11 through 2026-09-11**, contract-stamped MNQ. There were 2,888 reads with at least one touch. Original H12 close-based labels give **8,041 held, 2,936 broke and 325 unresolved at the horizon**. Complete denominators and IDs: [event index](2026-09-12-structural-stop/evidence/event-index.csv); every held-event ID, grouping and penetration value: [C5 evidence](2026-09-12-structural-stop/evidence/c5-overshoot.json).

Penetration is the maximum excursion beyond the entry zone's far edge, from the touch minute through the original close-based hold/break determination, with up to 12 subsequent one-minute bars. The touch minute is included. Its extreme may precede the touch, so this is an OHLC upper bound on penetration, not identified tick-level overshoot. Conditioning on eventual holds excludes failed reversions; these percentiles are **not stop-survival probabilities or win rates**.

| Held-event population | n | Median points | p75 | p90 | p95 |
|---|---:|---:|---:|---:|---:|
| All periods | 8,041 | 0.0000 | 0.0000 | 1.2500 | 4.7500 |
| Before 2025-09-12 | 6,181 | 0.0000 | 0.0000 | 1.2500 | 4.3676 |
| 2025-09-12 onward, previously exposed | 1,860 | 0.0000 | 0.0000 | 1.5000 | 6.5125 |

[I] **Default buffer: 4.50 points**, the training sample's p95 of 4.3675648248 points rounded outward to the 0.25-point MNQ tick. This percentile was chosen before the profit sweep to cover more of the observed held-event penetration tail. It is a tolerance hypothesis, not the profit-maximizing cell and not a validated universal buffer. The resolver records `C5-H12-IS-6181-p95-20260912` and the source on each composition.

[T] Registered grid: training p50 and p75 both become the one-tick floor **0.25**; p90 becomes **1.25**; p95 becomes **4.50**. Four percentile labels therefore produce **three distinct buffers**, each evaluated under three fill models. Explicit owner overrides are separate research settings; invalid explicit values refuse rather than silently reverting to the default.

The following are descriptive all-period splits, not separate fitted defaults. Each metric cell is **[median, p75, p90, p95]**. Width fractions divide by that event's own `hi−lo`; ATR fractions divide by its frozen ATR5m. “TF missing” is genuinely missing metadata on the **first source** in the merged zone, not a guessed timeframe. These groups do not enumerate every timeframe represented by every member. The one-observation four-family group cannot calibrate a tail.

| Group | n held | Penetration, points | Fraction of zone width | Fraction of ATR5m |
|---|---:|---|---|---|
| tf= (TF missing) | 7,235 | [0.0000, 0.0000, 1.2500, 4.5000] | [0.0000, 0.0000, 0.1345, 0.4419] | [0.0000, 0.0000, 0.1066, 0.3336] |
| tf=5m | 420 | [0.0000, 0.0000, 2.6310, 6.9084] | [0.0000, 0.0000, 0.3177, 0.7445] | [0.0000, 0.0000, 0.2460, 0.6009] |
| tf=15m | 386 | [0.0000, 0.0000, 0.9578, 3.3347] | [0.0000, 0.0000, 0.0933, 0.3653] | [0.0000, 0.0000, 0.0857, 0.3101] |
| families=1 | 2,387 | [0.0000, 0.0000, 1.0510, 3.5000] | [0.0000, 0.0000, 0.1429, 0.4625] | [0.0000, 0.0000, 0.1043, 0.3337] |
| families=2 | 4,181 | [0.0000, 0.0000, 1.2500, 4.5615] | [0.0000, 0.0000, 0.1340, 0.4408] | [0.0000, 0.0000, 0.1106, 0.3409] |
| families=3 | 1,472 | [0.0000, 0.0000, 1.7500, 7.0483] | [0.0000, 0.0000, 0.1477, 0.4467] | [0.0000, 0.0000, 0.1336, 0.3710] |
| families=4 | 1 | [0.0000, 0.0000, 0.0000, 0.0000] | [0.0000, 0.0000, 0.0000, 0.0000] | [0.0000, 0.0000, 0.0000, 0.0000] |
| session=LONDON | 2,892 | [0.0000, 0.0000, 0.5000, 1.8791] | [0.0000, 0.0000, 0.0689, 0.3027] | [0.0000, 0.0000, 0.0612, 0.2386] |
| session=NY | 2,310 | [0.0000, 0.0000, 6.0000, 10.0000] | [0.0000, 0.0000, 0.4960, 0.8310] | [0.0000, 0.0000, 0.3906, 0.6252] |
| session=ASIA | 2,839 | [0.0000, 0.0000, 0.0000, 0.7500] | [0.0000, 0.0000, 0.0000, 0.0591] | [0.0000, 0.0000, 0.0000, 0.0548] |

[B] Session heterogeneity matters: NY's all-period p95 is 10 points, compared with 0.75 in Asia. The pooled 4.50-point default must not be described as a validated NY noise allowance. No session-specific buffer was selected after inspecting profits in this wave. [C5 reproduction check](2026-09-12-structural-stop/evidence/logs/c5-parity.log) independently matches all stored held-event values and summary quantiles against the frozen event cache.

## Status, authority and source provenance

Binding Round 22 status, also included verbatim in the Guide:

> [I]/[T] a codeable research candidate, not a validated replacement. No external evidence fixes its buffer or proves that it will turn the losing book positive.

[A] Branch **`fix/structural-stop`** was claimed from the accepted `origin/dev` tip **`e81602bb5c4bacb237ae2921e0188f8aa1d752bf`**. Remote claim verified by `ls-remote`: **`11e4475cd9875e0a7c6ac85f44a35ba9e8abfed5`**. Composite session: `structuralstop-22fba7ca/Codex[unlisted]`. The isolated worktree is `/tmp/vl-structural-stop-20260912`; the main checkout was not edited. A subsequent fetch still found the accepted dev tip and the original claim on this branch.

[A] Runtime evidence at acceptance: health revision `400ea26c12c8`, executable revision **`400ea26c12c8b6daa7069d14a88eddfe1c9297e5`**, `vcs.modified=false`, executable MD5 **`93e8bd17fe2d86a928e41a76768b1fae`**, systemd restart policy `on-failure`. No binary swap, restart, live config write or account mutation has occurred in this dispatch.

[O] After the missing-map census was reported, the owner authorized continuing with new-map fixtures and explicit missing-provenance refusal for legacy plans. The owner further ruled that the user sets the risk limit and it depends on the contract. **No numeric cap was supplied.** The code supports an instrument-keyed dollar limit; this wave remains MNQ, one contract. Planned loss uses the contract point value and the fixed one-contract quantity, including modeled round-trip costs. It is not a guarantee against gap loss.

Evidence labels: **[A] directly inspected or reproduced; [B] inference; [C] speculation; [R] researched; [T] proposed/local test; [I] doctrine or engineering inference; [O] owner constraint.** Local replay is system evidence, not peer-reviewed validation. No new literature search or claim to worldwide novelty is made here. The previous report's FX/equity transfer limitations remain binding.

Pinned bases:

- [Round 22 at e81602bb](https://github.com/johnwick2921-cyber/vl/blob/e81602bb5c4bacb237ae2921e0188f8aa1d752bf/docs/superpowers/research/2026-09-12-stop-target-geometry/README.md): 64,780 bytes. Last source-file commit at that base: `586d00ef 2026-09-12T11:30:05-05:00 docs: incorporate fresh structural-geometry backtest and qualify fill results`.
- [Backtest 1 at 6b3fddf7](https://github.com/johnwick2921-cyber/vl/blob/6b3fddf7/docs/superpowers/research/2026-09-12-backtest-zone-fade/README.md): 29,654 bytes; frozen control and original cohort, with the fill errors discussed below.
- Level-map basis `6c96683c`; scenario-economics basis `6f677b55`. Their detectors, zones, merge and economics writers are unchanged.
- [Source freshness manifest](2026-09-12-structural-stop/evidence/source-provenance.md) records `git log -1` at the accepted base for each cited/changed existing file. New candidate files are identified as new; their final content is pinned by this report's containing commit.

## C1 — what selected the old stop

[A] Original owner ruling, `trader/arm_stop_anchor.go` at `4657560b`, read before modification:

> stop = BEYOND the nearest seated level on the risk side + tick clearance, then floored at MIN_SL_ATR_MULT×ATR5m — WHICHEVER IS WIDER WINS. The authored stop is a third floor

For a long, the old composer finds the nearest seated risk-side level within the resolved dead-zone bound, subtracts tick clearance, calculates `entry − 1.5×ATR5m`, and retains whichever of anchor, ATR or authored candidates places the stop farthest away. The short case mirrors this. Missing ATR skips that old leg; missing nearby structure can leave the authored/ATR bound. **The stop was deterministic, not randomly chosen.** The construction mixed unrelated constraints.

[A] The reproducible C1 cohort is the last **200 changed/unanchored stop log lines**, matched to the latest preceding stored plan only when its authored stop also matches. All 200 matched; they represent **27 distinct plan/version/scenario/leg specifications**. ATR wins **180**, anchor **10**, authored **10**. These log lines predate the current running revision; the inspected pure composer is unchanged between those revisions. Existing logging suppresses unchanged anchored/authored outcomes, so **the requested denominator of 200 distinct composition attempts cannot be recovered** and the 90% figure is not an unbiased frequency over every arm attempt.

| Distance in MNQ points | Available n | Mean | Median | p75 | p90 | Min–max |
|---|---:|---:|---:|---:|---:|---:|
| Level-derived candidate | 133 | 15.6718 | 8.00 | 10.52 | 51.34 | 0.75–123.62 |
| ATR floor | 198 | 44.9128 | 34.50 | 58.8025 | 71.28 | 27.71–74.30 |
| Selected stop | 200 | 45.8328 | 34.81 | 61.55 | 71.28 | 28.13–123.62 |

Unavailable candidate distances are omitted, not zero-filled. Log price rounding limits precision. [C1 exact IDs, raw log lines, matches and distances](2026-09-12-structural-stop/evidence/c1-compositions.json); [reproducer](2026-09-12-structural-stop/harness/audit_compositions.py).

## C2 — what selected the old target

[A] The production arm composer substituted the scenario's recorded first obstacle when the existing one-setup predicate allowed it; otherwise it retained the authored target. The old historical simulator instead searched nearby zone anchors. Those are different populations and selection rules.

The backup contains **116 positive-price arm ledger records**, of which **90 explicitly carry `entry_class=armed_fill`** and form the primary composition cohort. The other **26 unclassified rows are excluded** from the primary table, with their IDs listed in the evidence. These are authorized/recorded arms, not 90 executions and not an inventory of all refused scenarios.

| Primary `armed_fill` records | n | Mean | Median | Min | Max |
|---|---:|---:|---:|---:|---:|
| Stop distance, points | 90 | 39.0008 | 34.19 | 17.25 | 67.8844 |
| Target distance, points | 90 | 90.56 | 84.06 | 36.16 | 166.25 |
| Gross target/risk | 90 | 2.3961 | 2.2092 | 2.0058 | 4.8771 |

[A] **The historical 4.35-point average target is not reproduced in this selected live ledger population.** The gate has already filtered it. This neither invalidates the raw-touch study nor establishes that current rejected opportunities have enough room. IDs and prices: [C2 ledger evidence](2026-09-12-structural-stop/evidence/c2-ledger.json). [Independent price arithmetic and census](2026-09-12-structural-stop/harness/audit_ledger.py) matches all 116 stored rows and the 90-row classification. No ledger P&L claim is made, so no legacy unresolved `pnl_corrected` row is silently included.

## C3/C4 and D — the implemented trade

[A] All **319 historical plan versions** in the backup lack `zone_map`; [plan IDs/version census](2026-09-12-structural-stop/evidence/c3-plans.csv). The latest NY v6 plan at 12:41:53 CT on September 11 predates the map writer implementation at 12:51 and its merge at 13:49. The current schema/writer supports a frozen `PlanDoc.Zones` snapshot; this census does not prove the writer is broken.

The old R:R failure was a named/logged refusal with an existing counter. It lacked the complete durable quantity-zero geometry record now added. New records are scoped by trader, plan, version, scenario and leg. The API exposes those stored decisions and the plan card displays their prices and reasons; it does not reconstruct a decision from a later map.

The changed path is the existing **`reject`** level-fade entry play, whether one-setup selection is on or off. Other conditions and explicit exit legs keep their legacy composition. The rule is:

1. Resolve the scenario's **canonical machine `level_id`** against the frozen identity list, then find the unique matching source in the frozen zone map. Require complete finite positive edges and source provenance. No nearest-price substitution, model-authored width or rebuilt historical zone stands in for missing identity.
2. Freeze entry using the existing adapter's tick normalization. Resolve the buffer and modeled costs. For a long, `S=floor_to_tick(zone.lo−buffer)`; for a short, `S=ceil_to_tick(zone.hi+buffer)`. The structural stop stands even if it is tighter than 1.5×ATR5m or the authored stop.
3. Read the **already merged** map. A distinct target interval lies strictly beyond the entry zone's profit-side edge. Every complete sourced interval is eligible; there is no extra grade, distance, source-count or R:R threshold for target eligibility. Choose the nearest such interval and its near edge, rounded toward entry. Overlapping/touching intervals are not treated as a distinct obstacle. The map's bounded non-transitive merge is not replaced by a second transitive merge.
4. Freeze S/T; compute risk `d`, gain `g`, net target gain `g−cost`, and one-contract planned loss `(d+cost)×point_value`. Apply the existing resolved R:R policy (owner floor 2.0 unchanged), then the configured instrument risk cap. Existing selection and other entry-gate checks still apply.
5. Refuse with quantity **0** and the exact first failure: `no_target`; `invalid_geometry` for nonpositive distances; `net_nonpositive`; `rr`; missing/exceeded owner cap; or `no_provenance` for absent/invalid source, buffer or cost data. Missing entry-zone provenance records the available ATR fallback price and `stop_source=atr_fallback`, then refuses. It never authorizes a zone-less trade.
6. Never resize, widen or tighten the stop, or move the target to rescue admission. The **23-for-4** case keeps those exact prices and refuses for R:R. With two points of costs, a four-point gross target still has two points of positive net gain; it is incorrect to say the nonpositive-net check alone catches that case.

[A] Only the ATR minimum-stop admission checks change role. The shared entry gate retains its resolved ATR multiplier for other consumers, including no-chase; a dedicated structural-validation flag bypasses only its minimum-stop leg. The detectors, merge, score, permission label, one-setup's three checks, cadence, R:R value, and post-entry exit management remain unchanged. The one-setup OFF fixture now keeps selection off while using the new geometry; its historical assertion of byte-identical reject targets is explicitly superseded.

[A] A geometry refusal retires a matching unplaced authorization. An existing broker entry is cancelled only through the existing cancellation safety predicate and broker-confirmed cancellation workflow. Protective orders are not blindly cancelled. The TCP loopback refusal pin proves zero new order/cancel messages for an unplaced refused authorization. If its decision cannot be persisted or its old authorization cannot be safely retired, the entire placement phase is withheld for that cycle; a database failure cannot turn a refusal back into an order.

Admission records mean **passed authoring gates**, not broker submission or fill. Transient pending checks remain quantity zero. Later gate refusals retain their recorded reason/detail. Counts are durable distinct **plan/version/scenario/leg/reason per CME day**; a pending-gates record cannot inflate a repeated refusal or erase an earlier one. ATR fallback is separately counted and not double-counted in total refusals.

Knobs: `day_plan.structural_stop.buffer_points`, `day_plan.structural_stop.round_trip_cost_points`, and `risk_control.max_trade_loss_usd` keyed by instrument. Missing owner cap refuses; no silent dollar default. The existing contract count remains one. The buffer and cap have owner controls in Studio, plus registry/type coverage. The boot line resolves the bound strategy, buffer source/percentile/calibration/grid, cap and recorded counters; unreadable values show `n/a`. Guide, arm map and Rulebook §A describe the same rule. The final deploy stamp will be set from the actual clean-build source after owner GO.

## C6 — corrections required before using the old profit numbers

[A] The frozen Backtest 1 and Round 22 identify four limitations:

| Old behavior | Correction in this replay | Effect that can honestly be quantified |
|---|---|---|
| Any zone-band intersection qualified fill A | Require actual anchor-price eligibility | Changes the filled population; the corrected A control has 3,815 fills, not all 11,302 touches |
| Fill C improved a long by −0.25 or a short by +0.25 | Add a genuinely adverse 0.25-point entry offset | Holding old paths fixed, old C −0.54 becomes approximately −1.04; this −0.50 adjustment is algebra, not the complete corrected backtest |
| Exit loop started after the touch minute | Include the fill minute and flag unresolved ordering | Changes paths and results; exact isolated attribution is not identifiable from the published aggregate |
| Independently simulated overlapping events | Report conditional event statistics separately from a chronological one-position diagnostic | Original episode cumulative P&L is not a deployable one-contract equity curve |

B additionally requires a one-tick trade-through for the target as well as entry. Stop gaps use the worse open. Stops and targets are tick-normalized. The first-touch minute remains the common entry-opportunity window; this does not evaluate a different multi-minute resting lifetime. A marketable-at-open proxy retains the frozen limit price conservatively rather than assuming price improvement.

On an ambiguous bar, the conservative proxy gives the stop priority and does not credit an unproven fill-bar target; the favorable bound credits a possible target. A close beyond the target proves post-fill traversal when no stop conflict exists. The reported interval is a **bar-model sensitivity range**, not a sharp identified execution interval: queue position, exact resting time and intrabar path remain unavailable, and pessimistic price assumptions need not describe an actual executable path. `FillBarAmbiguous` also flags later same-bar stop/target conflicts, despite its historical field name.

The old −0.79/−4.93/−0.54 means and 0/81 result are therefore historical reported evidence, **not the corrected benchmark**. This wave reruns the frozen baseline geometry under corrected proxies; it does not rerun every old detector-parameter cell. The full shift cannot be assigned independently to each correction because eligibility and exit paths interact. The old surface cannot establish that geometry alone caused the loss.

The read/data files are byte-identical to the pinned harness; [hash/parity proof](2026-09-12-structural-stop/evidence/harness-parity.json). The old pure stop function is frozen only as a control and matches `6b3fddf7`; [function parity](2026-09-12-structural-stop/evidence/legacy-port-parity.json). New geometry calls the same core as production through a frozen-zone-index wrapper; it does not invent planner identities for historical detector events.

## E1/E4 — before and after, with the limitations exposed

[A] All means below are **net MNQ points per filled conditional event**, including two points of round-trip friction. C adds another 0.25 adverse entry point; it is not charged twice. Original one-setup ranking, permission and every live entry gate are not reconstructed, so these are geometry comparisons, not claimed live policy returns.

A = actual anchor-touch proxy; B = through-tick proxy; C = A with adverse entry tick. Each cell is **mean (filled n)**. The training period has 8,639 opportunities; the previously exposed last year has 2,663. Complete per-fill IDs/prices/outcomes: [trade CSV](2026-09-12-structural-stop/evidence/e-trades.csv); every candidate opportunity and refusal: [geometry opportunities](2026-09-12-structural-stop/evidence/geometry-opportunities.csv).

| Period | Geometry | A | B | C |
|---|---|---:|---:|---:|
| all | legacy_corrected | -5.0105 (3,815) | -5.3643 (3,612) | -5.2605 (3,815) |
| all | p95 | -2.6039 (207) | -3.0817 (202) | -2.8539 (207) |
| in_sample | legacy_corrected | -4.4334 (2,892) | -4.7517 (2,726) | -4.6834 (2,892) |
| in_sample | p95 | -2.9293 (152) | -3.3666 (148) | -3.1793 (152) |
| held_out | legacy_corrected | -6.8188 (923) | -7.2489 (886) | -7.0688 (923) |
| held_out | p95 | -1.7045 (55) | -2.3009 (54) | -1.9545 (55) |

[B] The structural candidate loses less per selected fill in this comparison, but it selects a different set of fills. That does not establish a paired policy improvement or positive expectancy. **Default p95 remains negative in every primary fill model in training and the exposed last year.**

The OHLC uncertainty is material. For all-period p95 A, **126/207 fills** have unresolved ordering; conservative mean **−2.6039** versus favorable bar-model bound **+11.4903**. For B, **121/202** are ambiguous: **−3.0817 to +11.2698**. C has **126/207**: **−2.8539 to +11.2403**. These ranges prevent a claim that minute data identifies the strategy's true expectancy. Finer execution data or prospective frozen SIM evidence is needed.

## E2 — the entire buffer surface and multiple comparisons

[A] The table includes every registered candidate cell, not just the best. Mean net points (filled n):

| Period | Buffer points | A | B | C |
|---|---:|---:|---:|---:|
| all | .25 | -2.5709 (737) | -2.8827 (701) | -2.8209 (737) |
| all | 1.25 | -2.9636 (515) | -3.3432 (496) | -3.2136 (515) |
| all | 4.50 | -2.6039 (207) | -3.0817 (202) | -2.8539 (207) |
| in_sample | .25 | -2.5583 (583) | -2.8739 (553) | -2.8083 (583) |
| in_sample | 1.25 | -2.8741 (397) | -3.2434 (381) | -3.1241 (397) |
| in_sample | 4.50 | -2.9293 (152) | -3.3666 (148) | -3.1793 (152) |
| held_out | .25 | -2.6185 (154) | -2.9155 (148) | -2.8685 (154) |
| held_out | 1.25 | -3.2648 (118) | -3.6739 (115) | -3.5148 (118) |
| held_out | 4.50 | -1.7045 (55) | -2.3009 (54) | -1.9545 (55) |

Summary across the nine candidate cells in each period:

| Period | Positive candidate cells / 9 | Median mean | Best mean | Worst mean |
|---|---:|---:|---:|---:|
| all | 0/9 | -2.8827 | -2.5709 | -3.3432 |
| in_sample | 0/9 | -2.9293 | -2.5583 | -3.3666 |
| held_out | 0/9 | -2.8685 | -1.7045 | -3.6739 |

[T] Multiplicity treatment: common circular moving-block resamples of **five observed CME days**, **4,000 draws**, seed **20260912**. Basic centered-bootstrap **95% two-sided Bonferroni simultaneous bounds** use tail probability `0.025/9`, with the three buffers × three fill models as the candidate family. The legacy control is not a tenth selectable candidate. Period splits are descriptive and evaluated separately; this is not a simultaneous claim across all later research rounds. Dependence beyond five observed days and extreme tails can weaken this approximation. No individual-trade symmetry/sign-flipping assumption is used.

| All-period candidate | Fill | n / distinct days | Conservative mean | Simultaneous lower | Simultaneous upper | Favorable bar-model bound |
|---|---|---:|---:|---:|---:|---:|
| p50_p75_tick_floor | A_touch | 737 / 490 | -2.5709 | -3.4798 | -1.6738 | 7.4790 |
| p50_p75_tick_floor | B_through | 701 / 478 | -2.8827 | -3.7629 | -2.0005 | 7.4643 |
| p50_p75_tick_floor | C_adverse_tick | 737 / 490 | -2.8209 | -3.7298 | -1.9238 | 7.2290 |
| p90 | A_touch | 515 / 366 | -2.9636 | -4.2204 | -1.7658 | 8.4102 |
| p90 | B_through | 496 / 360 | -3.3432 | -4.5641 | -2.2125 | 8.3327 |
| p90 | C_adverse_tick | 515 / 366 | -3.2136 | -4.4704 | -2.0158 | 8.1602 |
| p95 | A_touch | 207 / 164 | -2.6039 | -5.5682 | 0.2896 | 11.4903 |
| p95 | B_through | 202 / 161 | -3.0817 | -5.9188 | -0.3747 | 11.2698 |
| p95 | C_adverse_tick | 207 / 164 | -2.8539 | -5.8182 | 0.0396 | 11.2403 |

These confidence limits concern sampling uncertainty **conditional on the conservative model**. They do not remove fill-model uncertainty. No candidate has a positive lower bound. The default p95 A/C upper bounds still cross zero; the full family therefore does not meet a strong statistical “all policies negative” kill criterion even before considering favorable path bounds. Full era/cell statistics, reasons, IDs and adjusted one-sided p-values: [complete surface](2026-09-12-structural-stop/evidence/e-complete-surface.json); [reproducer](2026-09-12-structural-stop/harness/summarize.py).

## E3 — refusals and attainable activity

| Buffer | Geometry eligible / 11,302 | Geometry refusal rate | A fills / all opportunities | B fills / all opportunities |
|---|---:|---:|---:|---:|
| .25 | 2,656 (23.50%) | 76.50% | 737 (6.52%) | 701 (6.20%) |
| 1.25 | 1,853 (16.40%) | 83.60% | 515 (4.56%) | 496 (4.39%) |
| 4.50 | 721 (6.38%) | 93.62% | 207 (1.83%) | 202 (1.79%) |

[A] At p95, **10,310** opportunities fail R:R and **271** fail nonpositive net target gain; **721** clear geometry. Of those, A/C fill **207** and leave **514** unfilled; B fills **202** and leaves **519** unfilled in the first-touch opportunity minute. These are exact reason partitions, not inferred zero counters. IDs are in the opportunity/trade CSVs.

[O] With the actual missing owner cap, those 721 otherwise eligible cases also refuse `risk_cap_missing`: **0/11,302 actual configured admissions**. The geometry-only diagnostic explicitly ignores that one missing configuration reason to measure the research candidate. It does not introduce an unlimited production cap. Sparse activity and the missing cap are both presented before boot, as the dispatch requires.

### One-position occupancy diagnostic

[T] A chronological diagnostic accepts only the first eligible opportunity when flat, reserving an unfilled opportunity's first-touch minute as well. Same-minute ties use the event ID. A selected fill occupies the book through its conservative exit minute. This avoids selecting only eventual fills, but still does not implement the live top-level selector, permissions, stale-data checks or queue. Drawdown is **closed-trade cumulative**, not marked intratrade drawdown. This is not a claimed attainable equity curve.

| Period, NY only, p95 | Fill | Fills | Mean net points | Closed-trade max drawdown points |
|---|---|---:|---:|---:|
| all | A_touch | 116 | -1.2586 | 228.0000 |
| all | B_through | 112 | -1.8527 | 259.0000 |
| all | C_adverse_tick | 116 | -1.5086 | 252.0000 |
| held_out | A_touch | 25 | 0.8500 | 63.5000 |
| held_out | B_through | 24 | -0.3854 | 63.5000 |
| held_out | C_adverse_tick | 25 | 0.6000 | 65.2500 |

The exposed-year NY subset contains only 24–25 fills; its positive A/C averages are not promotion evidence. Every selected occupancy ID is recorded in the complete surface. All-session diagnostics are provided there separately.

## F — behavioral verification, mutations and scope

**Valid RED came first.** Against the claimed baseline, with one-setup selection disabled only in the fixture to isolate geometry:

```text
F1/F2: structural stop 28995 must stand despite authored/ATR floors;
rows=[... entry:29010 ... stop:28960 target:29120]
--- FAIL: TestStructuralStopF1F2ProductionArmCycle
```

F3/F4/F5/F6 baseline fixtures failed because no durable structural/refusal record existed. [Complete corrected RED](2026-09-12-structural-stop/evidence/logs/red-corrected.log), [baseline fixture](2026-09-12-structural-stop/evidence/red-fixture.go.txt). An earlier fixture failed an unrelated identity-selection check; that is explicitly **not** counted as the F1 behavioral RED. No build failure is counted as a killed mutant.

**GREEN:** the production arm cycle preserves long stop **28995** for zone `[29000,29010]`, buffer 5; the mirror short stop is **29015**. Wider ATR/authored candidates cannot override them. Missing map produces the exact available ATR fallback **28980**, source `atr_fallback`, quantity zero and missing-provenance refusal. The 23-for-4 case preserves its stop/target, records `rr`, and creates no new order. A separate TCP loopback pin begins with an older unplaced authorization, retires it, and observes neither a place frame nor a cancel frame. Nonpositive net gain refuses even when gross R:R is adequate; when both fail, net reason comes first.

The owner-cap boundary pin verifies missing cap refusal, a $15 cap refusing $16 of modeled risk, equality at $16 preserving prices, and the contract point value changing modeled exposure to $160 when increased tenfold. All are one-contract calculations. Persistence pins verify trader/version scoping and counts surviving admission. A counter regression first reproduced **`map[rr:3]`** from one distinct refusal across pending-check cycles, then passed after durable per-day/spec/reason deduplication.

An additional fault-injection RED against candidate `540c9e8d` forced the cancellation-state database write to fail. The old authorization then reached the loopback broker with quantity 1 despite the recorded R:R refusal. The corrected path returns before placement when persistence or retirement fails; the same fixture is GREEN with no wire order. This was an isolated candidate defect, not a live incident. [Fault RED](2026-09-12-structural-stop/evidence/logs/retirement-write-red.log), [fault GREEN](2026-09-12-structural-stop/evidence/logs/retirement-write-green.log).

Eight required mutations plus the additional retirement-failure bypass mutation used **`scripts/mutate.sh`**. Each log proves that the edit landed, the mutant built, and a behavioral pin failed:

| Mutation | Actual changed expression | Verdict |
|---|---|---|
| D1 wrong edge | long stop uses `z.Hi` instead of `z.Lo` | KILLED |
| D2 wrong buffer side | long stop adds buffer instead of subtracting it | KILLED |
| D4 wrong target | choose farther eligible zone instead of nearest | KILLED |
| D5 admit/resize bad geometry | replace R:R refusal with `r.Quantity = 1; return r` | KILLED |
| D5 wrong refusal order | net rejection requires R:R to have passed first | KILLED |
| F5 widen | `stop -= tick` after composition | KILLED |
| F5 tighten | `stop += tick` after composition | KILLED |
| F5 move target | `target += 100` after target selection | KILLED |
| F4 retirement failure | bypass the persistence/retirement failure guard | KILLED |

Exact changed lines, build confirmation and failed test names are in [mutation logs](2026-09-12-structural-stop/evidence/logs/). All source mutations were restored. A sandbox VCS-status failure before the first mutant run was retried with appropriate build access; it is not counted as a mutation result.

[A] Verification completed on the isolated candidate: **full `go test ./...` including goldens passed**; the trader package took 155.482 seconds in that run. **Vitest: 63 files, 429 tests passed. TypeScript: `tsc --noEmit` passed.** The alleged pre-existing brand-scope failure did **not** reproduce: brand-scope and brand-visible checks passed with the required subprocess permissions. The initial sandbox EPERM in a Go subprocess is an environment limitation, not a brand behavior failure. Older one-setup/positive-placement fixtures were supplied valid frozen identities/zones and explicit test caps; their three selection checks were not weakened.

[Full Go output](2026-09-12-structural-stop/evidence/logs/go-full-final.log), [Vitest output](2026-09-12-structural-stop/evidence/logs/vitest-final.log), [TypeScript output](2026-09-12-structural-stop/evidence/logs/tsc-final.log), [exact fallback/cap and seam checks](2026-09-12-structural-stop/evidence/logs/contract-risk-green.log). New tests added after the full run were also run directly; a final publication-head run is recorded in the publication receipt. **Merged-head and clean-build checks remain required after owner GO.** Branch green is not claimed to mean merged green.

### A29 production claim list and A28 clock

The maintained wiring gate covers every new claimed production function: `ResolveEntryGeometryZone`, `FirstGeometryTarget`, `ComposeLevelFadeGeometry`, `composeGeometry`, `ResolveStructuralStop`, `SaveStructuralGeometry`, `StructuralGeometryFor`, `StructuralGeometryCounts`, `saveArmGeometry`, `retireGeometryRefusal`, `StructuralGeometryBootLine`, `planStructuralGeometry`. Each has a non-test production caller. The existing `composeArmStop` wrapper is reached from `maybeManageArmedOrdersAt`; the boot wrapper is called by `main.go`, and the read API wrapper by `handlePlanToday`. The report does not count a research-only call as production wiring. `ComposeFrozenLevelFadeGeometry` is explicitly the research wrapper and is not a production-wiring claim.

The arm seam supplies the same existing `now` for records, CME-day keys and cancellation checks. The pure geometry core reads no wall clock. Existing clock-seam and call-site gates pass. [Static scope and source facts](2026-09-12-structural-stop/evidence/source-provenance.md) make the baseline and changed files reviewable. No detector, kernel map/merge, provider or NinjaScript production file changed.

## Null, minimum sample, and the CTO decision

[T] The binding profitability null remains **`H0: μ_net ≤ 0`** for the complete executable one-contract policy. Beating a negative control is insufficient. The separate level-information question needs matched placebo entries preserving session, volatility, proximity, widths and geometry; this wave does not claim it has established that levels contain an exploitable signal.

[T] Round 22 proposes a reporting floor of **200 fills across 60 distinct CME days**, at least **20 losses** for even coarse loss-tail statements, and roughly **400 held/reclaimed events** for a p95 penetration tail with about 20 expected exceedances. These are research floors, not a published power guarantee. Actual power needs an owner-defined economically worthwhile effect, observed variability, day dependence and multiplicity. The exposed-year p95 sample of **55 A/C fills or 54 B fills across 44 days** falls below that reporting floor. The all-period p95 count barely exceeds 200 but includes calibration/exposed data and material fill uncertainty.

[I] **CTO judgment:** the geometry fix is coherent and the refusal behavior is useful, but it has not earned a profitability claim. A pooled measured buffer does not create a trading edge. The sparse eligibility, exposed small last-year sample and wide bar-model bounds argue for a frozen prospective SIM evaluation if the owner elects to boot it. Do not optimize more targets after seeing these results or remove the first-obstacle refusal merely to increase activity.

[T] Statistical abandonment of this defined family requires adequately powered simultaneous upper bounds at or below zero for every prespecified plausible policy, under an execution model relevant to the intended book. This wave does not satisfy that stronger conclusion for all cells, and minute-data uncertainty prevents universal claims about level fading. [O] The owner can still retire the book operationally if no candidate demonstrates an economically useful positive edge; proving every possible fade loses is not required. The unchanged 2R constraint, dollar cap, drawdown budget and decision to continue are owner choices.

## Backup, live truth and owner-gated cutover

[A] Before measurements, an SQLite online backup was taken from a **read-only source connection** at **2026-09-12 19:11:36 CT**. Size **1,236,602,880 bytes**, SHA-256 **`c264a1e0cde4b105a8b78c8b95fe94b9ab2d3baf86ace5b9436e87e13b2496ca`**; `PRAGMA integrity_check` returned `ok`. No live DB/config/account write was made.

A second byte-verified copy of the database, frozen event cache, complete sweep and captured input log cohort is preserved under **`/home/hoang/vl-backups/structural-stop-20260912/`**, outside the temporary worktree. [Backup verification manifest](2026-09-12-structural-stop/evidence/backup-verification.json). The database and large raw cache are private local backups, not committed research artifacts. The report, full cohort index, geometry decisions, trade outcomes, harness, verification logs and source manifest are versioned together.

**A15 status: candidate verified offline; NOT MERGED, NOT DEPLOYED, no new live-surface proof.** The first new-geometry live composition and first refusal are still awaited after any authorized boot. A suite is not substituted for those events. Until then, do not label the candidate shipped or claim that current live plans already contain the new map/record.

The dispatch explicitly requires **owner GO after E4** before merge and boot. No GO is assumed from the earlier request to finish and verify the work. After GO: refresh dev/spec freshness; census both checklist heading formats with `uniq -c` and allocate at merge, never reserve; scrutinize `git diff --stat origin/dev`; merge and run the full suite/goldens/Vitest/tsc at merged HEAD; build in a clean clone named `vl`, stamp Guide source then dist from the actual source revision and verify `vcs.modified=false`.

The deploy-only main tree then requires the helper-owned lock and fresh five-leg gate. Leg 4 must use `TerminalArmStateSQL()`; an unreadable leg fails. Confirm restart policy, flat position, no planner read in flight and the authorized session window. Quote any resting arm and the sweep outcome. Write RELEASE before swap; **move**, never copy, the old binary to `vl-bin.old.<actual-held-revision>` after reading its embedded revision; verify new built/running-file MD5 before the kill. Only then perform the authorized restart. Read boot integrity, verify five references and MD5, collect the first composition and refusal numbers, push the release marker, and release the lock.

Rollback remains a concrete gate-time procedure because the binary's then-held revision must be measured, not guessed from today's revision. Preserve both binaries and RELEASE metadata. If new boot/integrity fails, under the same lock and flat gate move the failed binary aside, restore the verified old image and matching release marker, restart via the verified service policy, and verify health/executable/MD5 against the preserved values. There is no destructive schema migration in this wave. Do not restore an old trading database over new fills merely to roll back code.

The isolated worktree remains locked while awaiting the owner decision; it will be removed at the completed dispatch's end after artifacts, source bundle and rollback/release evidence are preserved. There is no automatic deployment timer.

## Reproduction and artifact verification

Run the measurement harness only against the verified backup. Restore the private inputs into `data/structural-stop/` in an isolated checkout if needed:

```bash
GOCACHE=/tmp/round22-go-cache go run ./docs/superpowers/reports/2026-09-12-structural-stop/harness -db data/structural-stop/backtest.db
python3 docs/superpowers/reports/2026-09-12-structural-stop/harness/summarize_overshoot.py
GOCACHE=/tmp/round22-go-cache go run ./docs/superpowers/reports/2026-09-12-structural-stop/harness -sweep
python3 docs/superpowers/reports/2026-09-12-structural-stop/harness/summarize.py
python3 docs/superpowers/reports/2026-09-12-structural-stop/harness/audit_compositions.py
python3 docs/superpowers/reports/2026-09-12-structural-stop/harness/audit_ledger.py
```

`numpy` is needed for the Python summaries. The source fixture/pure-function parity checks are in the evidence. No network feed, broker order or live detector mutation is needed for this replay.

The publication receipt gives the raw report URL pinned to its complete commit SHA, HTTP status, downloaded byte count, `git ls-tree --long` size, branch `ls-remote` and tested SHA. Those values are generated after commit rather than embedding a circular self-commit identifier in this file. A branch URL, successful push alone or unverified local file size is not treated as publication proof.


## Owner GO and CI reconciliation — 2026-09-13

[A] The owner explicitly answered **“go”** after the negative E4 verdict and
pre-deployment status. This authorizes the documented SIM cutover after checks;
it supplies no numeric per-instrument risk cap. The cap remains owner-set.

[A] PR #115 at `b6c5e9430d9b4797fc6ff3477b97597a7e8646c1` had three failed
GitHub jobs despite the earlier local pass. The Test workflow used shallow
checkouts: both import-preservation guards require baseline
`954f11b15f2e7615678f7d2b708c47895faebf1e`, absent in that checkout. Its frontend
also denied `branding/product.txt?raw` outside `web`. The security scan reported
24 called standard-library vulnerabilities under Go 1.25.3. These are real failed
checks; the earlier local pass does not supersede them.

[A] The owner's GO to resolving these blockers adds a narrow build/test correction:
full history for the Test workflow, explicit test-only filesystem access to `web`
and `branding`, and Go 1.25.13 as the application patch toolchain. No module dependency
or trading rule changes. The protected `go.mod` hash is advanced solely for this
one-line patch-version change; all branding test assertions and protected imports
remain intact. The old pinned scanner failed package analysis after the patch;
`govulncheck v1.8.0`, built with isolated Go 1.26.7, successfully scans the application
under its own Go 1.25.13 toolchain and reports **0 affected vulnerabilities** (one
imported-package and four required-module findings are not on called paths).
[Scanner output](2026-09-12-structural-stop/evidence/logs/govulncheck-current.log).
No finding is suppressed. Official patch reference:
[Go vulnerability GO-2026-6218](https://pkg.go.dev/vuln/GO-2026-6218).

[A] Source freshness at this continuation: remote dev still
`e81602bb5c4bacb237ae2921e0188f8aa1d752bf`. Last-change records before correction:
`294d7a13 security(f1): dependency vuln scan + safe bumps + CI automation + class 22`
for go.mod; `85794a72 feat: add X-Client-ID header for claw402 monitoring` for
Test workflow; `ae9278c5 fix(E4): the pre-existing FE test pair` for Vitest config.
This is the environment-dependent green failure described by checklist class 110.

[A] At 2026-09-13 00:00:19 CT the five-leg API gate passed: DB open 0, API positions
0, NT8 positions 0, broker working 0 / ledger 0 / unplaced authorizations 0,
no planner read claimed. This observation is preliminary; a fresh gate is still
required immediately before cutover. Running revision remained `400ea26c12c8`.


## Merged and built; runtime cutover held by A7 — 2026-09-13

**[A] MERGED / VERIFIED / NOT DEPLOYED.** PR #115 merged at 00:12:58 CT as
`4127979f2fcc5615f4e8b17540f7aba74bb4ea92`. Its first parent is the verified
acceptance/current dev tip `e81602bb5c4bacb237ae2921e0188f8aa1d752bf`; second
parent is reviewed candidate `316f1e468294e27311114ac26420164d92531a67`.
The clean clone initially inherited a stale local dev ref from the source bare
repository; this was caught before remote publication, refreshed, and the merge
was recreated from GitHub's actual tip. The corrected merge tree is byte-identical
to the preparatory tree; the full checks were rerun at the corrected SHA.
Class 126 was assigned from the all-format census (previous maximum 125).

[A] GitHub candidate checks: **25 SUCCESS, 1 SKIPPED, 0 FAILURE**. The skipped
job is multi-architecture manifest publishing for the PR, not a test bypass.
[Exact CI receipt](2026-09-12-structural-stop/evidence/github-checks.json).
On the exact merged HEAD in clean clone `/tmp/structural-stop-release-build/vl`:
**full Go suite including goldens PASS; 63 frontend files / 429 tests PASS;
TypeScript PASS**.
[Go](2026-09-12-structural-stop/evidence/logs/go-merged-final.log),
[frontend](2026-09-12-structural-stop/evidence/logs/vitest-merged-final.log),
[TypeScript](2026-09-12-structural-stop/evidence/logs/tsc-merged-final.log).

[A] Clean binary: build revision `4127979f2fcc5615f4e8b17540f7aba74bb4ea92`,
Go 1.25.13, `vcs.modified=false`, **73,418,400 bytes**, MD5
`300dc70535f708be9f6278d252124ba4`, SHA256
`a826d045a5cf831def4f52af0762e9dc1a8505034cc9b0f2e20c4c2f10b800f9`.
[Build receipt](2026-09-12-structural-stop/evidence/build.json).
RELEASE and Guide SOURCE are stamped from that actual binary revision; their
presence on dev denotes the prepared release, not a running-image claim.

[A] Fresh online pre-cutover backup at 00:05:57 CT:
`/home/hoang/vl-backups/structural-stop-20260912/release-20260913/pre-cutover.db`,
1,236,660,224 bytes, SQLite integrity_check `ok`, SHA256
`f5904181390f857bbaaede8a33159a5f22e34cb6dbd5d5476dcc0b3900576f46`.
A verified source bundle and CI receipt are preserved beside it. Live database,
owner settings, account bindings and the running image were not changed.

[A] The owner GO remains valid, but work crossed midnight CT. Dispatch A7 permits
14:45–16:30 CT or after 17:10 flat, and requires an explicit owner exception
outside that window. Therefore the binary/dist swap and kill are held. No timer
or unattended deployment is scheduled. Before a resumed cutover, repeat the
five-leg gate and exact source/marker/MD5 checks; move the actual-held-revision
binary aside, then verify before kill. The initial midnight gate is not reusable
as a fresh deployment gate.

[A] Required live proof is **PENDING**, not passed: there is no composition or
refusal under this binary until it boots and a real scenario reaches the arm
composer. Current settings have no owner-set per-instrument risk cap, so this
release does not authorize trades through an invented budget. E4 remains
negative/unproven; neither this merge nor green checks establish profitable
best-trade selection.


[A] Prepared frontend build passed; the Guide stamp test passed (12 tests).
`web/dist/assets/index-CGuFCQsg.js` contains the exact binary revision
`4127979f2fcc5615f4e8b17540f7aba74bb4ea92`.
[Dist manifest](2026-09-12-structural-stop/evidence/dist-manifest.json),
[build log](2026-09-12-structural-stop/evidence/logs/frontend-build.log),
[Guide stamp test](2026-09-12-structural-stop/evidence/logs/guide-stamp-test.log).
The frontend build emits its existing large-chunk advisory; build exit is zero.
This staged dist has not replaced the running frontend.


## Boot marker — 2026-09-13 00:21:42 CT

**[A] DEPLOYED TO SIM; LIVE TRADE-CONSTRUCTION PROOF PENDING MARKET OPEN.**
The owner's explicit **“go market closed”** authorized the A7 time exception.
This supersedes the preceding A7 hold. No risk-cap value was supplied or invented,
and no owner configuration/account binding was changed.

[A] Lock `structuralstop-22fba7ca/Codex[unlisted]` acquired at 00:19:12 CT; its
bounded keeper supplied the heartbeat. Main was clean on dev and advanced only
by `--ff-only` to prepared release commit
`8f4790ca98e09187873ab4318d584dff1bea93e1`. The restart policy was verified as
`on-failure`. Fresh pre-kill gate at **00:21:10.266857 CT** passed all five legs:
DB open=0, API positions=0, NT8 snapshot count=0, broker working=0 / ledger=0 /
unplaced authorizations=0, and no planner read claimed. Broker snapshot age was
one second. Gate age at kill was **26.41804 seconds**. There were no resting
arms and no in-flight read to interrupt.
[Exact gate](2026-09-12-structural-stop/evidence/pre-kill-gate.json).

[A] RELEASE and its committed value preceded the swap. The old binary was
**moved**, not copied, to
`/home/hoang/vl-backups/structural-stop-20260912/release-20260913/vl-bin.old.400ea26c12c8b6daa7069d14a88eddfe1c9297e5`;
its embedded revision was read before naming the backup, and old MD5 was
`93e8bd17fe2d86a928e41a76768b1fae`. Old dist and release/Guide source are preserved
beside it. New binary and UI were moved into place at **00:20:55 CT**; revision,
MD5, Guide source and dist were verified **before** SIGKILL.
[Swap verification](2026-09-12-structural-stop/evidence/swap-verification.json).

[A] This agent sent SIGKILL to verified old PID **3671783** at
**00:21:36.789756 CT**. Systemd relaunched PID **4165029**, which logged integrity
success at **00:21:42 CT**, within the required 90 seconds. No timer, unattended
deployment or rollback was used.
[Restart receipt](2026-09-12-structural-stop/evidence/restart-request.json).

> 09-13 00:21:42 [INFO] vl/main.go:295 🔐 BOOT INTEGRITY OK — rev 4127979f2fcc · built 2026-09-13T05:10:17Z · expected 4127979f2fcc · goldens PASS

[A] The actual resolved stop/target boot line reads:

> stop=zone-edge+buffer buffer=4.50[I] (p95 of measured overshoot; resolver=ResolveStructuralStop:C5_MNQ_default[I]; calibration=C5-H12-IS-6181-p95-20260912; sweep=[0.25 1.25 4.5] points[I]) · atr-fallback=0 · refused today=0 (no_target=0 net<=0=0 rr<2.00=0 risk_cap=0 no_provenance=0 other=0) · target=first-distinct-eligible-zone · MNQ risk-cap=UNSET (refuse) · never-widened=asserted · research-candidate

[Captured boot lines](2026-09-12-structural-stop/evidence/logs/boot-lines.txt).
These zero counters are boot-time recorded values; they do not prove successful
trade construction or a profitable strategy.

| Reference | Verified value |
|---|---|
| Disk deploy/RELEASE | `4127979f2fcc5615f4e8b17540f7aba74bb4ea92` |
| HEAD:deploy/RELEASE | `4127979f2fcc5615f4e8b17540f7aba74bb4ea92` |
| Guide SOURCE | `4127979f2fcc5615f4e8b17540f7aba74bb4ea92` |
| /api/health | `4127979f2fcc`, status `ok` |
| /proc/4165029/exe | `4127979f2fcc5615f4e8b17540f7aba74bb4ea92`, modified=false |

[A] Built, on-disk and running executable MD5 all equal
**`300dc70535f708be9f6278d252124ba4`**. The served dist carries the same binary
revision; all **92** manifest files matched SHA256. The prepared source/marker
commit differs from the Go binary build commit by release metadata, Guide stamp
and report/evidence; this is deliberate, not an unstamped binary.
[Five-reference verification](2026-09-12-structural-stop/evidence/boot-verification.json).

[A] Post-boot at **00:22:46 CT**, the five-leg gate again passed: no positions,
no broker/ledger working orders, no unplaced authorizations, no planner read.
NT8 reconnected and the trader auto-started.
[Post-boot gate](2026-09-12-structural-stop/evidence/postboot-gate.json).

### What is still unproven from live operation

[A] At **00:24:38 CT**, the production store had **0** `structural_geometry:`
records (keys `[]`) and the post-boot journal had **0** composition lines. The
actual runtime says: `CME closed (weekend) — next open Sun 2026-09-13 17:00 CDT`.
`trader/auto_trader_loop.go:220` returns at the closed-session gate before its
arm-management call at line 432; the sweep is reached inside
`trader/armed_executor.go:210`. Consequently **no actual sweep-result line was
observed**, and this report does not invent “cancelled 0.” The lifetime sweep
counter is 13, from the store, and is **not this boot's sweep count**. The gate
proved that no resting order needed cancellation at this cutover.
[Exact proof status](2026-09-12-structural-stop/evidence/postboot-proof-status.json).

[A] The dispatch's first real structural composition (entry-zone edges, buffer,
stop, first eligible target and admission numbers), first refusal, and actual
sweep-result line remain pending an open-market cycle. The session gate was not
bypassed and no fixture was injected into the live book to manufacture proof.
The owner-set instrument risk cap remains UNSET; new otherwise-qualified fades
will be refused until the owner configures it. E4 remains negative/unproven.

Rollback assets retain the old binary, UI and release/Guide source alongside the
verified DB backup. A code rollback must restore matching release metadata and
old image under a fresh flat gate and lock; the historical DB must not be
restored over any subsequent fills merely to roll back code.
