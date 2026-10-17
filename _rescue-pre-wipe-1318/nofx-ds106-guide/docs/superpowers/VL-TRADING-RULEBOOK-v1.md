names rewritten to vl on 2026-09-30 (VL rename)
# VL TRADING RULEBOOK v1 — corrected evidence and policy structure

**Documentation correction: 2026-09-10. This document describes and proposes no code changes.**

**Canonical authority.** The owner has designated this corrected rulebook as the
replacement for the supplied v1, effective on merge of PR #99. Its five substantive
sections are A–E below; the sources appendix is supporting material. Every wave
agrees with this file. A wave touching a trading rule updates **A. WHAT RUNS** in
that same commit, with code-line evidence and an explicit revision/proof boundary;
**B. WHAT IS WANTED** is never used to represent implemented behaviour. See the
[Master Plan, §6](VL-MASTER-PLAN-v5.md#6-verification--how-every-claim-in-this-plan-is-checked).

This revision separates **A. what runs**, **B. what is wanted**, and **C. what is still to build or prove**. Historical measurements and trader recommendations have their own sections. A roadmap item, an owner preference and a verified implementation are not interchangeable.

**Evidence boundary.** The health endpoint returned `status=ok`, revision `95f387ae3cfe`, at **2026-09-10 13:01:15 CT**. Implementation citations below are pinned to full commit **`95f387ae3cfe675919cd6f81a689010a85889059`**. The PR was accepted from dev **`5e27344271fb1d04550426fee9a3e2a96ee737d3`** and rebased onto **`a98a92c76b62d4b9533e12cc4a51f6c142d2ecb6`** before publication; dev and the observed running revision are deliberately distinguished. Reading code establishes the implementation at that revision, not that every configured route is enabled or every broker outcome has been demonstrated. Active strategy/environment settings and live broker state were not re-read for this documentation correction.

**Original-file provenance.** The supplied Desktop file is preserved byte-for-byte in commit `3b449ff04269dc623f7aa01f8848c0ce4571b6bc`: 280 lines, 14,769 bytes, SHA256 `62611a3bc06e535cce7657c2adbb892326bbb3fbc2e9596ae38b5ea473a6e621`. It was not previously tracked on dev.

**Labels.** `[R]` = research-supported statement with its market/sample limits; `[T]` = measured result from the explicitly named population, not necessarily a new measurement; `[I]` = unvalidated hypothesis or proposed analytical/documentary choice. `[O]` identifies a carried-forward owner rule, not empirical support or deployment proof. Code citations `[Cnn]` establish implementation; audit citations `[Ann]` establish what the archived audit reported.

## A. WHAT RUNS — implementation at the observed revision

### Level zones — implementation addition, awaiting cutover

| Surface | Representation and proof boundary |
|---|---|
| Full read-time map | `kernel/level_zones.go:BuildLevelZones` preserves every detected source, including seat losers. Native bounds stay intact; unknown point widths are NULL. Frozen `zone_map` reaches the model and card. This branch is not yet booted. |
| Compatible merge [I] | One nearest fixed-anchor cluster, anchors within 0.5×ATR5m, union width ≤1×ATR5m. Both values resolve from display-only settings. No transitive chaining. Broad native bands (>resolved threshold, default 1×ATR5m) and bands above the merge cap stay separate context. |
| Family and shortlist [I] | Five display families capped at 3; weights 1 each on log(1+known prior touches), round proximity, family count, and negative ATR distance. Legacy score confluence/HTF weighting and all trading anchors remain unchanged. Shortlist cap is the existing bound session value. |
| Missing evidence | No defining wick/ATR means NULL width; no complete post-formation tape means UNKNOWN touches, not zero. No historical plan backfill. Zones versus lines and multi-timeframe confluence remain untested. |


Every implementation statement in this section cites a code line. Defaults and conditional branches are described as such; no unresolved setting is silently called enabled.

### The tape is one contract — implementation addition, awaiting cutover

**Roll wave, 2026-09-10; recording only until its boot.** Every bar the store
holds carries the contract it was received on (`bars.contract`), stamped at
write from the AddOn's most recent `subscribed` ACK — the one frame that names
the instrument. The current contract is that ACK's value with its receipt time,
exposed as `CurrentContract(symbol)`; it is never derived from a date. When the
ACK names a different contract than it last did, the symbol's ring is purged on
every timeframe, reseeded from the store for the new contract only, and the
event is logged with both names and raised once as a P0. Every live bar reader
asks for a contract: the current one for the tape being traded, or the contract
a historical window was on; a window that spans a roll is
`unrecomputable:spans_roll` and is never read as one series. Retired bars are
filtered, never deleted. On roll the book re-seats: levels seated on the retired
scale are phantom on the new one and are replaced by the next planner read from
the purged ring.

### One contract, two sources — implementation addition, awaiting cutover

**Bar-source wave, 2026-09-10; recording only until its boot.** Every bar
names the feed that delivered it (`bars.source`): live is the minute as it
traded; historical is a replay the ring has judged to be on the live scale;
mixed is a minute with one side on each scale and is never read; off-scale is
a row measured to hold an unverified replay's values and is never read. A
replay never overwrites a live bar, in the ring or in the store. A replay is
held out of the store until the first live bar after it lets the ring compare
scales — agree and it is released into the minutes live never wrote; disagree
and it is discarded, the ring drops the seed and refills from the store's live
rows, and the event is raised once as a P0 with both closes. An empty minute
is a gap; a wrong-scale minute is a false event, and the book never sees one.
The boot line `📼 bar source:` states the census, the hold, both thresholds
and every mismatch of the process. The NT8 replay's scale is a finding for the
AddOn, filed with the two research facts that prove it.

What this corrects: on 2026-09-10 at 21:15 CT the subscription rolled MNQ 09-26
→ 12-26 on reconnect, the ring kept ~2,000 September bars under December ones,
and a ~292-point basis presented to every reader as a move. Plan v7 (21:29 CT)
seated 7 of 12 levels on the retired scale and one IFVG inside the gap itself.
The AddOn resolves bars at subscribe-time and orders at request-time from the
same date rule (`VLContractResolver.cs:80`), so for the 135 minutes between the
rule's flip (19:00 CT) and the reconnect they disagreed — order 152 was placed at
a September price on the December contract. That resolver is filed for the next
AddOn wave; this wave makes Go read one contract regardless.

Implementation: `store/bar_contract_roll.go`, `store/bar_history.go`
(`InsertBars` refuses an unstamped bar; `LastNBarsOn`, `BarsBetweenOn`,
`WindowContract`), `provider/ninjatrader/contract_roll.go`
(`CurrentContract`, `observeContract`, `OnContractRoll`),
`trader/ninjatrader/bar_persist_wire.go` (`contractFor`, the filtered
rehydrate, the roll listener), `trader/contract_current.go`,
`trader/bars_store_depth.go`, `trader/desk_facts.go` (MODE line).

### The scale check judges neighbours; the chart shows every contract — implementation addition, awaiting cutover

**Dispatch 101, 2026-09-16; recording and display only until its boot.** The
scale comparison above is made only between ADJACENT bars — the replay's last
bar and the live bar within two intervals of it. A reference older than that
is a time gap, not a scale gap: the check is skipped, said once with both ages,
the seed kept and the check left armed for a later adjacent pair. An empty
replay (a reconnect whose BarsRequest ran while the feed was down) re-arms
nothing. What this corrects: on 2026-09-16 at 09:22 CT a zero-bar reconnect
replay re-armed the check, the next live bar was judged against the 22:10 bar
from the previous evening, 146.75 points of overnight move read as a scale
break, and 1,999 five-minute and 1,832 one-minute bars of history NT8 had
delivered at 22:15 were dropped — the 5m horizon fell from 12 days to 11 hours
for the life of the process. After a confirmed break the P0 line now says what
the ring is for that timeframe, and asks NT8 for its full replay again — once
per symbol per boot. A second confirmed break in the same boot is not retried:
the line says the replay is on another contract and the AddOn restart is the
fix; the store refill still runs, NT8 is simply not asked again. (A timed retry would loop: the wrong-scale
replay overwrites the store's rows every cycle until a live bar judges it.)

Every timeframe's ring is refilled from the store at boot and after a drop
["i want fuull data", 2026-09-16, superseding the 1m-only condition of
2026-09-09], through one door: after a drop only live rows come back; every
row enters as replay-grade, never as a live bar; imported history is refused
at that door and counted on the boot line; the contract is the current one.

The planner's tape is NT8's own [CTO ruling under the owner's delegation,
2026-09-16]: every planner door — the 12,000-bar 1m candle tape, the weekly
reader's weeks and window, the POC-touch historical leg, the planner seam —
reads only bars NT8 produced (live, or replay verified on the live scale).
Imported history never reaches a decision; the chart keeps it, labelled.
Removing it moves what the regime baseline and the levels are fed, so the boot
line prints the tape and the baseline both ways, measured, and the rule itself
does not change (the same input both ways is a zero delta, pinned).

The chart is served across contract rolls [O]: the current contract's bars,
then prior contracts filling strictly before the current contract's first live
bar, every bar labelled with its contract, the roll a visible basis step and
never adjusted. **The book decides on the current contract only** — the
contract-scoped readers are unchanged and nothing on the level, plan or arm
path reads across the roll.

Implementation: `provider/ninjatrader/bar_source.go` (adjacency, skips),
`bar_cache.go` (empty replay), `history_at_subscribe.go` (the `🧯` line),
`store/bar_history_nt8_only.go` (`LastNBarsFromNT8On`, `BarsBetweenFromNT8On`, `ImportRowsOn`),
`trader/planner_tape_nt8_only.go` (the 🧮 accounting line), `history_rerequest.go` (once-per-boot re-request), `trader/ninjatrader/bar_persist_wire.go`
(`rehydrateRowsFor` — the door), `store/bar_history_across_roll.go` (display readers,
the current contract excluded from "prior"), `api/handler_klines.go`
(`klinesAcrossRoll`, `VL_CHART_ACROSS_ROLL`), `trader/ninjatrader/bar_horizon_warn.go`
(one WARN per condition per five minutes).

### Scenario identity — implementation addition, awaiting cutover

**105, recording only; not yet live.** A scenario names its level by candidate id.
The new-authoring schema carries `level_id`; the frozen map carries the primary
candidate's seven hash inputs, including a separate `formed_close_ms`.
`FormedAtMs` remains the pre-existing candle-open input to wakes and gates.
`LevelByID` resolves recording and display attribution; the trading evaluator
keeps its own anchor and decisions. Disagreements beyond the existing merge
width are recorded once per plan version/scenario, not corrected in execution.
Missing or unknown IDs are accepted with WARN and a recorded counter for the
first two boots; escalation requires the owner's ruling after at least five
plans. Legacy IDs remain NULL. Backfill never computes a partial hash.

Implementation: `kernel/scenario_level_identity.go` (`LevelByID`,
`StampAuthoredIdentity`, `ResolveScenarioIdentity`),
`trader/scenario_level_identity.go` (`recordPlanIdentity`,
`observeScenarioIdentity`), `store/level_identity.go` (`BackfillLevelIdentity`).
This paragraph is the identity lane's addition. The remainder of this corrected
rulebook is carried from `daeb654978b0c739592e8589a393c5e79171560d`, where it was
last changed; no section B policy is amended. Branch tests establish source
behavior; the boot report must establish the eventual running revision.

### One setup — implementation addition, awaiting cutover (dispatch 102, 2026-09-11)

**The book arms ONE play; the follow is recorded.** At the arm seam, after the
shared gate and before the arm row is composed, a scenario is authorized only
when three verdicts hold at that instant: its level is the top-ranked merged
candidate inside the reachability band (grade ≥ `one_setup_min_grade`, default
B, then distance; any timeframe, any kind; an unresolved `level_id` never
resolves by nearest), its condition is `reject`, and the fade-permission label
reads permitted (NULL never permits). The structural-stop candidate replaces the reject arm target with the first distinct eligible zone near edge; its frozen geometry is judged before the unchanged selection checks. Refusals record their exact reason and quantity zero. One arm at a time per plan:
the higher-quality allowed scenario arms first, the other reads
`second_setup_waiting` until it is terminal. A declined scenario is still
evaluated, confirmed and recorded — its episode row carries all three verdicts
— and is never armed. The predicate gates authorization, and (owner ruling
2026-09-11, after the first boot) an authorization that predates it whose
scenario is currently declined is retired by ledger state at placement time —
never placed, nothing at the broker under it; a broker order is never
cancelled by it. `one_setup_enabled` defaults ON `[O]`; OFF restores this section's
previous behaviour byte-identically (pinned against a golden generated before
the wave). Nothing the planner is shown changes.

Beside every fade-plan the follow-plan is RECORDED and never armed `[T]`: the
break (first closed 5-minute bucket beyond the level), the role reversal on
the episode row, the retest from the far side, the would-be passive-limit
entry (filled only if the bar traded through by a tick — a touch is not a
fill), MAE/MFE and net after 2-pt friction at 10 and 20 buckets, the retest's
own hold-vs-break verdict, and the bias the break would imply — written
beside the plan's frozen bias, never into it. Pre-registered null: role
reversal ≤ 50%, follow ≤ 0 net.

Implementation: `kernel/one_setup.go` (`OneSetupAllowsAt`, `OneSetupOrder`),
`trader/one_setup_wiring.go` (`oneSetupVerdictsAt`, `oneSetupConsult`),
`trader/armed_executor.go` (the seam), `kernel/follow_plan.go`
(`ComputeFollowPlan`), `trader/follow_plan_wiring.go` (`recordFollowPlans`,
`BackfillFollowPlans`), `store/one_setup.go`. Branch tests establish source
behaviour; the boot report (`reports/2026-09-11-one-setup.md`) must establish
the running revision.

### Part 1 — Entry families and routing

The entry-law table distinguishes reject, FVG, sweep-reclaim, reclaim, breakout-retest, acceptance/hold and continuation conditions. The name “level-fade book” is a description of the studied style, not a statement that these conditions share one execution rule. [C01] [C03]

When `plan_mode=strict` is selected, the shared gate refuses decision-path market entries and requires an arm-path entry to cite a scenario with a matching direction. This is a conditional code fact; the current strategy's resolved mode is not established by this document. [C06]

### Part 2 — The day: read time, activation, entry cutoff and flat are distinct

| Source-defined item | What the inspected implementation says | Code |
|---|---|---|
| Asia registry | Read 16:30; window 17:00–02:00 CT; flat 02:00; default enabled=false. | [C11] |
| London registry | Read 01:30; window 02:00–08:30 CT; flat 08:30; default enabled=false. | [C11] |
| NY registry | Read 08:00; window 08:30–14:45 CT; flat 14:45; default enabled=true. | [C11] |
| Opening no-entry band | First five minutes after the relevant session open. | [C12] |
| Lunch no-entry band | 12:00–13:30 CT. | [C12] |
| Last-entry offset | Default 15 minutes before session end; per-session overrides take precedence. Normal NY at that default gives 14:30, not an unconditional 14:20. | [C13] [C14] |
| Earlier holiday close | The last-entry calculation can pull in to the calendar's earlier close minus the resolved offset. | [C14] |
| Arm-path session risk | The arm path calls the shared session-risk adjudication, whose no-trade-band refusal precedes its loss-run check. | [C18] [C15] |
| 2026-12-31 | The inspected calendar classifies this date as NORMAL. That is the stored calendar classification, not independent proof of the future exchange schedule. | [C27] |

The table describes registry defaults and implementations, not a fresh determination of enabled sessions or account-specific overrides. In particular, a 01:30 read is not itself a 01:30 London activation, and a replan cutoff must not be substituted for a last-entry cutoff.

Exchange hours are a separate external fact: CME lists normal Micro E-mini Globex hours as 17:00–16:00 CT. The strategy's 14:45 flat and subsequent idle period should not be called the exchange's maintenance window. Holiday trading, settlement and floor hours must be distinguished. [R1] [R2]

### Part 3 — The map: supported construction, not an all-timeframe promise

| Component | What the inspected implementation supports | Code |
|---|---|---|
| Main level assembly | Calls multi-day, round-number, opening-range, gap, equal-high/low, supply/demand, FVG, order-block, volume and swing detectors, then includes extra supplied levels and returns raw, pool and seated outputs. | [C19] |
| HTF detection | Runs selected structure detectors on requested supported timeframes; its explicit whitelist is 15m, 30m, 1h, 2h, 4h, 6h, 8h and 12h. This is not an all-timeframe detector claim. | [C20] [C21] |
| Session VWAP | Uses the session-day window and volume-weighted typical price with deviation bands. | [C22] |
| Profile approximation | Accumulates each bar's volume into the bin containing that bar's close. It is a close-bin proxy, not observed volume at every traded price. | [C23] |

The exact intended definitions of prior-day, overnight, OR, IB, weekly references and merge/seat policy are retained in section B as a specification to reconcile with source and data. Their existence in a wish list is not proof that every reference was available to a particular historical plan.

### Part 4 — The trade

**Entry — reject arms on the TOUCH condition.** More precisely, an enabled `reject` scenario is a **touch-entry fade**: its legal confirmation rule is `touch`, with a limit entry at the level. It does not wait for a five-minute rejection close; the entry-law table marks close confirmation on a reject fade as illegal. This describes the rule, not a guarantee of arming, placement or fill when other gates refuse. [C01]

The touch evaluator marks a level touched when an observed bar spans its price; it can mark a forming touch MET and only assigns a known ordered event time when the minute is complete. It does not establish that the level subsequently held. Creation of an internal arm, its touch condition, broker submission and execution are distinct events. [C02]

**Touch-entry and confirmed-entry are two different trades.** They can produce different fills, stop distances, attainable targets, time remaining and expectancy. A confirmation observed later cannot justify booking the earlier touch price. E1 compares them side by side on the same initially observable opportunities, with their own executable entries and unchanged declared comparison assumptions; E1 does not presume either is better. This is the research design in section C, not a newly enabled entry condition. [I]

**Other confirmation rules.** Sweep-reclaim permits the table's touch and confirmation combinations; reclaim permits a five-minute close or one-minute structure confirmation. The close-rule evaluator reports completed rule-timeframe close evidence. These rules must not be generalised into “every setup waits for five minutes.” [C03] [C07]

**Stop-entry availability.** The stop-entry route requires `STOP_ENTRY_SEAM=on`; its default is off, and the arm path checks the switch before proceeding. This document does not assert its current environment value or that every reclaim became a broker order. [C04] [C05]

**Stop composition — structural-stop candidate, booted 2026-09-13.** For a `reject` level fade, read the uniquely identified entry zone from the frozen machine map. Long: `floor_tick(zone.lo - buffer)`; short: `ceil_tick(zone.hi + buffer)`. The measured buffer is resolved from strategy configuration; provisional MNQ default 4.50 points [I], training p95. Neither the authored stop nor 1.5×ATR may override available structural invalidation. Missing usable provenance records the unchanged ATR multiplier as fallback, but refuses admission. Other entry plays and post-entry exits are unchanged. Sources: `trader/structural_geometry.go:ComposeLevelFadeGeometry`, `trader/arm_stop_anchor.go:composeArmStop`, `store/structural_geometry.go:ResolveStructuralStop`.

**Target and admission — structural-stop candidate.** The target is the near edge of the first complete sourced zone strictly beyond the entry zone in the profit direction, from the map already merged under its non-transitive rule. Never skip it for a more attractive ratio. Normalize entry to the existing execution tick rule, freeze stop and target, compute gross/net gain and one-contract dollar loss including costs. Refuse missing provenance/buffer/cost, unresolved or nonpositive geometry, nonpositive net target gain, R:R below the unchanged bound strategy floor (owner 2.0). The owner clarified on 2026-09-13 that the configurable loss limit is DAILY. No additional per-trade cap is required; existing daily-loss and entry gates retain their configured value, switches and behavior. Quantity is zero on refusal; prices never move to pass. `ComposeLevelFadeGeometry` and the actual arm cycle record the decision. Research status: [I]/[T] a codeable research candidate, not a validated replacement. No external evidence fixes its buffer or proves that it will turn the losing book positive.

**R:R admission.** The inspected arm validator computes R from the arm leg's entry, stop and target before placement and compares it with the resolved arm threshold; its documented default is 2.0. This is **arm-time geometry**, not a guarantee of R≥2 at the eventual broker fill. For a structurally composed reject fade the ATR leg no longer overrides the stop; other paths retain their prior floor. [C10]

**Quantity at the inspected send sites.** The limit and stop-entry send calls shown here each pass quantity one. These two lines establish those call-site quantities, not proof of account-wide exposure, all broker paths or current broker inventory. [C25] [C26]

### Part 5 — Position, cancellation and flat: verification boundary

This correction does not certify that every cancel or flat attempt settled correctly. Broker-confirmed protection and full flatness are desired obligations in section B; current proof is an open item in section C. A healthy endpoint, a desired rule and a missing order are insufficient by themselves to prove a safe position state.

No blanket “only stop, target or 14:45 can exit” statement is made here. The session registry has distinct flat times, and this documentation change did not perform an exhaustive exit-path or broker-event audit. [C11]

### Part 6 — Risk: describe the resolver and its exceptions

| Item | Verified source behaviour | Code |
|---|---|---|
| Loss-run adjudication | The adjudicator refuses at a positive resolved halt count, warns at a positive warning count, and evaluates the no-trade band first. | [C15] |
| Read failure | If the loss-run query fails, the inspected gate logs that the breaker is not applied in that cycle and adjudicates with zero losses while retaining any band refusal. This is an explicit fail-open exception. | [C16] |
| Daily-limit description | The boot-line formatter marks the limit DECORATIVE when either the master or daily leg is off. The formatter is not proof of the current switches; those values were not read for this correction. | [C17] |

Owner thresholds and SIM restrictions are stated in section B as policy. They must not be mistaken for fully verified current enforcement.

## B. WHAT IS WANTED — owner policy and the intended trading contract

These are carried-forward owner choices from the supplied rulebook or explicitly marked analytical hypotheses. **Their presence here is not a deployment claim or an instruction to change code.** Exact runtime settings, exceptions and settlement evidence belong in section A only when verified.

### B1. Scope and style

- [O] SIM evaluation; one contract and one position per account, including working-entry exposure; no averaging down or fractional scaling.
- [O] Strict scenario attribution: the plan, candidate, version, direction and permitted action should remain identifiable.
- [O] Breakeven/trailing remain suspended under the carried-forward rule. Their current runtime status is not newly certified here.
- [O] The supplied policy names a $450 daily limit with its switches off by choice, and warning/halt counts of 5/8. Retain these as owner choices pending resolved-setting evidence; do not describe an unenforced figure as protection.
- [I] The economic hypothesis is a level-fade book. First-touch fade, confirmed rejection, sweep/reclaim and continuation are separate hypotheses; their names alone do not establish an edge.

### B2. Intended map and context

| Reference | Definition the trading specification must make explicit |
|---|---|
| PDH / PDL / PDC | Prior trading-session boundaries; RTH versus full-session scope; last trade versus settlement for “close”; holiday handling. |
| ONH / ONL | Exact overnight start/end, trading date, developing/final status. |
| OR / IB | Intended 5-minute OR and 60-minute IB after the cash open, with developing values distinguished from completed values. These durations are parameters, not proven optimal NQ choices. |
| VWAP and bands | Session anchor, typical-price input, volume source, variance convention and reset. |
| POC / VAH / VAL | Source session and price-volume method; a proxy must retain its proxy label. |
| Swings and zones | Source timeframe, zone edges, formation and confirmation times; the time the level first became knowable. |
| OB / FVG | Operational definition and source lineage; predictive value remains unestablished for this implementation. |
| Round and weekly references | Chosen grid, contract/price basis, completed-week boundaries and data sufficiency. |
| Projections | Clearly labelled hypothetical targets/obstacles; not historical traded structure or automatically eligible entries. |

[O] Preserve the full map and distinguish entry, target, obstacle and invalidation roles. Exclusion from a shortlist is not invalidation. The original three-point merge, 12-seat cap, Tier-1 preference, freshness/confluence factors and HTF multiplier are specified choices to document and evaluate, not calibrated probabilities. [I]

A reference's availability at the decision time matters more than its later appearance on a chart. Merged aliases must retain underlying prices/zone edges and shared provenance, so repeated constructions are not counted as independent evidence. [I]

### B3. Intended scenario and management contract

**Scope note (2026-09-11).** What runs (§A) arms one play — the fade — and
records the follow. The wider book this section describes, and a LIVE follow
side, are **what is wanted after the experiments**: round 17's cell criterion
(approach direction × level timeframe, ~385 episodes per cell, role reversal
with a 95% lower bound above 50% AND a net-positive follow after 2-pt friction)
is the gate between the record and the wire. Until the owner's own record
shows that cell, §B's follow is a specification, not a behaviour.


[O] Before a scenario is accepted as a trading plan, its meaning should be explicit: entry zone, trigger, confirmation, structural invalidation, protective stop, first obstacle and provenance, executable response there, broker target, R to obstacle and target, time horizon and expiry.

The intended contract must choose whether an intervening reference is the actual profit target or an obstacle to pass under a stated condition. “Next opposing level” and “whatever far target clears the gate” must not stand in for the same policy. With one contract, “reduce” cannot mean a fractional partial exit; it needs an executable whole-position interpretation or an unavailable label. [I]

Structural invalidation and the mechanical protective stop are distinct. A wider stop changes dollar risk and available R. Any claim about managing a failed premise before the protective stop, a time exit or a news event must say whether it is desired policy, a tested variant or established behaviour. [I]

[O] The intended bracket contract is a protective stop-market and target limit sharing their bracket OCO, separate from the unfilled entry's OCO. GTC duration does not remove the session-end cancellation obligation. These are retained owner requirements; this correction does not certify the AddOn's current bracket or OCO behaviour.

[O] A cancelled order should be resolved using fresh broker evidence and the resulting position; its absence alone does not distinguish cancellation from a fill. Flat should cover positions, working orders, pending placements and arms that can re-place, for the correct account/contract. Existing protection must remain distinguishable from unknown protection.

### B4. Intended clocks, validity and risk governance

[O] Keep read, publication, activation, entry cutoff, order expiry, session flat and exchange close separate. “No overnight” must say whether it means no carry across a strategy-session boundary or no carry across a CME trading-day boundary; it cannot silently contradict an Asia session spanning midnight.

[I] A plan's data age, publication age and last-validation age are different. Record what invalidates the plan, what merely requests a refresh, what happens during a replan cooldown, and how an existing position relates to a replacement version. The original 30-minute cooldown, 25-minute replan cutoff and 1.5×ATR fast-move trigger are carried-forward parameters, not established optimal lifetimes.

[O] Retain the original live-money gate as a governance proposal: at least 100 closed trades and 40 active CME days under one unchanged policy, documented costs, drawdown/day-loss tolerances, fee verification and protection/cancel/reconnect/kill drills. **The two “95% lower bounds” must name their metrics and interval methods before the gate is evaluable.** No present-tense pass/fail claim about all gate items is made from old snapshots.

## C. WHAT IS STILL TO BUILD OR PROVE — backlog and research, not current behaviour

These names are carried forward from Master Plan v5 to identify unfinished specification/proof obligations. They do not assert that a named branch is currently unimplemented, authorize a boot, assign another agent, or propose a code patch.

| Workstream | Completion evidence still required by the trading contract |
|---|---|
| W1 episode contract / identity | Stable candidate and scenario identity; first-known times; real event sequence; explicit heuristic links and missing-data reasons; unambiguous opportunity denominator. |
| Map / W-TF | An inventory showing actual detector coverage by timeframe and data availability. Added model-visible context belongs to a new policy cohort unless decision neutrality is established. |
| W2 fade permission | Causal context labels made from information available at the decision, with visible labels distinguished from truly decision-invisible collection. |
| Settlement / flat / working-versus-armed | Current broker-event receipts and complete state reconciliation, including fills during cancellation and in-flight placements. |
| W-LIVE / 13f | Eligible forming-bar observations, declared prediction horizon/null and unseen evaluation; no inference from unavailable formation times. |
| Plan drift / 15e | Event-based and time-based validity comparisons with their own delay, churn and missed-opportunity costs. |
| Documentation truth | Current settings and source pin; one status per claim; no roadmap sentence silently promoted to present tense. |

### Experiments retained, with their estimands made explicit

| Test | Fixed comparison basis | Compared alternatives and required accounting |
|---|---|---|
| **E1 — first-touch versus confirmation** | Same originally knowable opportunities and declared absolute stop/target or risk-normalised design. | Touch-entry, rejection confirmation, failed-break and reclaim evaluated side by side. Each gets its own attainable fill, resulting stop distance/R, delay, expiry and missed trades. Never-confirmed is a zero-trade opportunity where appropriate, not a filled flat trade. |
| **E2 — stop / target / horizon** | A declared entry policy and cohort. | Structural/ATR/other stop variants with structural/fixed-R/no fixed target and explicit remaining exit horizon. Use all eligible paths, not only historical winners; distinguish price touch from feasible fill. |
| **E3 — context permission** | Frozen downstream policy and causal context. | Predeclared exclusions against a declared comparator. Analytical comparisons do not suspend owner risk/news restrictions in the running bot. |
| **E4 — ranking** | Same full candidate universe and downstream policy. | Score/seat variants against a role-aware distance baseline; retained exclusions and score components; one-position opportunity conflicts. |
| **E5 — risk** | Explicit account/day/session units and cost assumptions. | Loss clustering, tail loss, time underwater, slippage and opportunity cost. Historical worst loss is not a maximum possible loss. |
| **14b — continuation / projections** | Its own knowable context and eligible population. | Test continuation separately from fade; projection reach is not itself a trade-profit result. |

[I] Approximately 20 sessions is a recording checkpoint, not a universal statistical sample size. Name whether a session is Asia/London/NY or a CME day; account for shared-day dependence and repeated plan versions. Predeclare effect size, uncertainty, test universe and untouched evaluation data. Test the combined selected policy once on a fresh holdout rather than combining independently selected winners and calling the combination validated.

## C5. Research-snapshot recorder volume — built 2026-09-16 (dispatch 103)

The research archive keeps every fact; only the narration changed. Before the
wave the recorder printed one INFO line per archived fact (measured 324,807
lines/hour = 88.8% of the log, ~16 GiB/day archive, no retention, drops narrated
at INFO). After: at most one rollup line per RESEARCH_LOG_EVERY_S (default 60s)
carrying rows-per-object, live drops, and queue depth; drop notices are
WARN-level, coalesced to one line per minute with the delta; RESEARCH_SNAPSHOT is
opt-out: explicitly 0/false leaves the recorder OFF ("research snapshot: OFF
(RESEARCH_SNAPSHOT=0)"); unset keeps it ON (today's behaviour).
RESEARCH_RETAIN_DAYS unset = never prune; when set, batched off-boot-path
prunes with no automatic VACUUM (a ~77 GB VACUUM on the trading DB's disk is the
owner's call). Rows/s unchanged — the archive path is untouched.

## D. HISTORICAL RECORD — each population stands on its own [T]

**These are the archived audits' own reported populations, not fresh account performance or proof about the policy at the current source pin.** Their membership, exclusions and assumptions must travel with the numbers. [A01] [A02] [A03] [A04]

### D1. Realised trades and authored geometry are different populations

| Population / metric | Audit result with n | Provenance and interpretation |
|---|---|---|
| Compliant realised cohort | **n=58 trades**, 12 CME days; 18 wins / 38 losses / 2 flats; corrected P&L **−$466.428572**; mean approximately **−$8.04/trade**. | Era entry cutoff 2026-08-15 00:00 CT; resolved plan, non-test, corrected non-NULL P&L. Exact exclusions are in the audit. Gross of commission; not a net expectancy estimate. [A02] [A04] [D01] |
| Win-rate denominator | **18/56 decided = 32.14%**; **18/58 including flats = 31.03%**. | Do not put a decided-only percentage beside n=58 without naming the denominator. Arithmetic from the population above. |
| Winner/loss payoff | Mean winner **$125.47** / mean losing-trade magnitude **$71.71** ≈ **1.75**, from **18 winners and 38 losers**, within the 58-trade cohort. | Ratio of average dollar outcomes, not the mean realised R of the same planned scenarios. [A01] [D01] |
| Broader authored-geometry census | **254 plan rows**, 253 scenario-bearing; **738 scenarios**, **132 complete arms**; planned-R median **2.36**, p25 2.10, p75 2.93. | Repeated versions are not independent trades. This is a plan/arm population, not the 58 realised trades. [A01] [D02] |
| Frozen scenario-economics cohort | **45/111 = 40.54%** of complete geometries have a positive directional first-listed target below 1R; **6/111** have arm-target R below 2. | 111 complete geometries within 280 scenario documents. A first-listed target is not automatically the first opposing obstacle. [A06] [A07] [D03] |
| Later retained scenario census | **69/177 = 38.98%** below 1R at the first-listed target; **17/177** below 2 at the arm target. | 177 complete geometries within 799 scenarios. This is a different census, not additional independent trades. [A06] [D03] |

The former paired “planned versus paid” headline is withdrawn. The table does not claim any of these separate populations is a paired estimate of realised target deterioration. A valid paired analysis needs the plan/arm identity, immutable initial risk, actual fill, fees and realised exit for the same trade.

### D2. Refusal replay — event rows and deduplicated opportunities differ

| Archived grouping | n and outcome counts | Reported counterfactual result |
|---|---|---|
| Raw refusal event rows | **n=61**: 42 STOP, 13 TARGET, 5 horizon-flat, 1 never-filled. | Session-flat **−$726.14**; CME-day horizon **−$902.02**. |
| Grouped by `cf_fill_ct` | **n=55**: 39 STOP, 10 TARGET, 5 horizon-flat, 1 never-filled. | Session-flat **−$862.14**; CME-day horizon **−$1,038.02**. |

Source: the audit's independent reconciliation of `refusals.csv`, including its tested grouping rules. [A03] These are counterfactuals under that replay's authored geometry, fill and horizon assumptions—not broker-realised P&L, and not a proof that any gate is optimal. The former unreconciled population/total pairing and the blanket conclusion that the gates cannot be the problem are withdrawn.

### D3. What the old record cannot establish

- [T] The corrected level audit separates contaminated touch history from its forward, first-recorded-read diagnostic; it states that formation-adjusted lifetime rates remain unmeasurable from that table. A historical reaction percentage is not a “coin flip” verdict or independent trade probability. [A05]
- [T] The old “no post-strict eligible trade” statement belongs to that audit's cutoff and exclusions. It is not a newly queried count for 10 September. [A04]
- [T] Winners-only excursion summaries describe selected winners; they cannot establish an optimal tighter stop or the opportunity cost of all rejected trades. The archived audit also records changing sample membership. [A01]
- [T] Style/plan inventories measure vocabulary and mix; they do not demonstrate profitability of the current policy. [A08]

## E. TRADER'S RECOMMENDATIONS — separate from implementation

Each item has one evidence label. These are recommendations for the **trading specification, analysis and reporting**. No code patch, parameter change, new entry gate, live-money action or deployment is proposed by this section.

1. **[I] State the economic premise for each playbook.** Separate first-touch fade, confirmed rejection, failed-break/reclaim and continuation. For each, write why a reaction is expected, what currently permits the trade, and what would refute it. Balance/directional/transition/unknown are proposed context labels, not hindsight declarations that a day was a trend day.

2. **[R] Price the actual entry, not the earlier signal.** Research on actual limit-order execution warns against substituting simple hypothetical price passage for executable fills. Use realistic fill assumptions and include confirmation delay, missed fills and deteriorated entry in E1. The cited evidence is from equities; it does not supply a calibrated MNQ fill probability. [R3]

3. **[I] Write one executable first-obstacle decision.** Before entry, specify the first obstacle, final target, time available and permitted whole-position action. With one contract, no fractional runner is available. A farther target's attractive R does not answer whether the nearer obstacle can be passed; conversely, an obstacle under 1R does not by itself prove the trade must be refused.

4. **[I] Separate invalidation, protective stop and risk budget in the plan.** Show the premise-defeating reference, stop components, final risk and remaining reward. Do not treat a nearby unrelated reference as the thesis merely because it is nearest. Evaluate tighter/wider stops on the full eligible population rather than conditioning on winners.

5. **[T] Keep the corrected population table attached to any conclusion.** The authored-arm census, compliant realised trades, raw refusal events and grouped refusal replay answer different questions. Preserve n, IDs/exclusions, snapshot, costs and method; do not compress them into one claimed edge or one gate verdict. [A01] [A03] [A04] [A06]

6. **[I] Define plan validity and version handover.** Record data cutoff, publication, last validation, effective session and expiry separately. List which market events invalidate a premise and which merely request a refresh. A cooldown is a scheduling choice, not evidence that an obsolete premise remains true.

7. **[I] Document what the level-ranking experiment predicts.** Reachability, reaction, feasible fill and net trade outcome are different targets. Preserve full-map roles, causal availability, zone edges, rejected candidates and correlated confluence components. A grade is not a calibrated probability.

8. **[R] Add a contract-month and price-basis note to preparation.** CME lists 14 September 2026 as the customary U.S. equity-index roll and 18 September as September expiry. A trader should identify the chart/data and execution contract and distinguish raw from adjusted continuous prices. The calendar fact does not prescribe a roll decision or validate transferring levels between contracts. [R4]

9. **[I] Specify event risk, competing scenarios and repeat attempts.** Document the intended treatment of news during the trade horizon, mutually exclusive long/short opportunities, re-entry at a previously failed level and end-of-session exposure. Evaluate the one-position portfolio, not a sum of trades that could not coexist.

10. **[R] Account for the full research search and keep a final holdout.** Sullivan, Timmermann and White report a nominal p-value 0.042 becoming 0.908 after a rule-universe adjustment in their historical S&P futures comparison. The applicable lesson is selection discipline, not a claim that all technical trading fails. A combined entry/target/ranking policy needs its own untouched evaluation. [R5]

11. **[I] Make the promotion gate interpretable.** Name both lower-bound metrics, cost convention, confidence-interval method, dependence treatment and unchanged-policy definition. The minimum trade/day counts do not automatically supply statistical power. Treat operational evidence separately from evidence of positive expectancy.

12. **[R] Narrow the adverse-selection citation.** Lalor and Swishchuk report **1,269/1,929 = 65.79%** adverse NQ fills in one specified TT simulation day, with “adverse” defined by the next relevant quote move. This supports realistic execution modelling; it does not establish a universal passive-fill loss rate, a cost in MNQ points, or a verdict against the bot's entry method. [R6]

## Sources and verification notes

All internal implementation/audit references below are pinned to the observed source revision. An archived audit citation verifies the report's population statement, not a new replay of its raw data. External sources retain their instrument, period and methodological limits.

The local verification for this correction checks the single-document diff, all internal source paths and one-based line anchors, the preserved original bytes, the corrected arithmetic and the separation of runtime facts from desired/backlog/recommendation sections. Trading tests and builds are not claimed for a Markdown-only change.

[C01]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/entry_law.go#L38
[C02]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/confirmation_evidence.go#L27
[C03]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/entry_law.go#L49
[C04]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/entry_law.go#L105
[C05]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/trader/armed_executor.go#L1071
[C06]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/trader/entry_gate.go#L184
[C07]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/confirmation_evidence.go#L91
[C08]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/scenario_economics.go#L137
[C09]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/trader/arm_stop_anchor.go#L71
[C10]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/trader/armed_executor.go#L1976
[C11]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/session_registry.go#L86
[C12]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/no_trade_band.go#L34
[C13]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/store/strategy.go#L1006
[C14]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/trader/auto_trader_clock.go#L395
[C15]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/trader/session_risk.go#L92
[C16]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/trader/session_risk.go#L120
[C17]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/trader/session_risk.go#L143
[C18]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/trader/armed_executor.go#L330
[C19]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/levels_assemble.go#L210
[C20]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/levels_assemble.go#L284
[C21]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/levels_assemble.go#L329
[C22]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/levels_volume.go#L35
[C23]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/levels_volume.go#L162
[C24]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/scenario_economics.go#L188
[C25]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/trader/armed_executor.go#L1125
[C26]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/trader/armed_executor.go#L1426
[C27]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/kernel/session_calendar.json#L119
[C28]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/trader/arm_stop_anchor.go#L138
[C29]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/trader/armed_executor.go#L470
[A01]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/docs/superpowers/reports/2026-09-05-vet-01-way-it-trades.md#L77
[A02]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/docs/superpowers/reports/2026-09-05-vet-01-way-it-trades.md#L48
[A03]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/docs/superpowers/reports/2026-09-05-veteran-part-d.md#L167
[A04]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/docs/superpowers/reports/2026-09-05-vet-02-levels-complete.md#L29
[A05]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/docs/superpowers/reports/2026-09-05-vet-02-levels-complete.md#L33
[A06]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/docs/superpowers/reports/2026-09-08-scenario-economics.md#L44
[A07]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/docs/superpowers/reports/2026-09-08-scenario-economics.md#L22
[A08]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/docs/superpowers/reports/2026-09-08-the-strategy.md#L1
[D01]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-data/revise/r01_headline.out#L1
[D02]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-data/q11_planned_rr.out#L1
[D03]: https://github.com/johnwick2921-cyber/nofx/blob/95f387ae3cfe675919cd6f81a689010a85889059/docs/superpowers/reports/2026-09-08-scenario-economics-data/scenarios.json#L1

[R1]: https://www.cmegroup.com/trading/equity-index/files/cme-micro-e-mini-futures-fact-card.pdf
[R2]: https://www.cmegroup.com/trading-hours.html
[R3]: https://web.mit.edu/Alo/www/Papers/limit10.html
[R4]: https://www.cmegroup.com/trading/equity-index/rolldates.html
[R5]: https://eprints.lse.ac.uk/119144/1/dp303.pdf
[R6]: https://arxiv.org/html/2409.12721v3
