names rewritten to vl on 2026-09-30 (VL rename)
# Plan liveness — corrected premises and implementation

**Current handoff:** combined boot with CONFIRMATION-TRUTH verified at
2026-09-08 15:17:31 CT, revision `f8bc7044cc44d58e84904a0a7761e78b420404af`. See
[the combined cutover record](2026-09-08-plan-liveness-confirmation-cutover.md).
Versioned death evidence and authored-condition validation are implemented;
exhaustion is warning-only. Both boot lines, integrity/goldens and five references passed. No active plan
exists in the observed window, so active-version N/M and organic event proofs
remain outstanding. Earlier STOP/build sections below are historical evidence.

The original STOP report below is retained as historical evidence. The owner
subsequently authorized the version/anchor repair and the revised conditional
scope. See the implementation update at the end. The STOP report was merged to
dev at `63d902ac9c345e6e51cfd237b035d4062a59acf0`; its commit-pinned raw URL returned
HTTP 200 / 11,183 bytes, byte-identical to the git blob.

## Original measurement STOP (historical)

Owner dispatch: PLAN LIVENESS, 2026-09-08. Branch: `fix/plan-liveness`.
Session: `plan-liveness-c22ee052/root[unlisted]`.

The dispatch remains authoritative for scope. The owner's follow-up explicitly
requires a STOP and correction if a Section C premise fails at the running rev.
**C1's purported pre-publication death timestamp is cross-version contamination;
C3's assertion that S2 has no timestamp is false. No fixtures, implementation,
build, migration, or deployment were performed.**

## Source and runtime provenance

[A] Initial dev tip: `171bebee5a1d29d7493ce4ac58b96dddf2f40c95`.
Claim verified through `ls-remote`:
`a66a2fe02cc6194aed14d7afb56a3126a2a79258 refs/heads/fix/plan-liveness`.
On resumption, origin/dev had advanced to
`9c2ab980986f5b9d07d7a7c21e833a9659ebe296`; it was merged into the isolated
worktree, preserving the claim history, at
`d2538a1e6eddcb7a415ff0d8aae90eeb4a177879`. The inherited placement changes are
not this dispatch's implementation. The main checkout was not modified.

[A] At 2026-09-08 00:03–00:06 CT, `/api/health` returned
`{"revision":"317388e7ab50","status":"ok","time":null}`. Service PID was
`3201079`; `/proc/3201079/exe` resolved to `/home/hoang/vl/vl-bin`.
`go version -m /proc/3201079/exe` returned:

```
vcs.revision=317388e7ab50d9ccb4ee4c8a92036b2ed30959da
vcs.modified=false
```

[A] The basis audit is **not yet on dev**. Its pinned source is
[planner-preparation audit, sections 4, 7 and 8](https://github.com/johnwick2921-cyber/nofx/blob/6095ca58fe5901ba398be374e4f9d3488d0bed6b/docs/superpowers/reports/2026-09-07-planner-preparation-audit/README.md).
The extracted git blob is 77,345 bytes. Its `git log -1 <sha> -- <path>` is:

```
6095ca58fe5901ba398be374e4f9d3488d0bed6b 2026-09-07T23:09:02-05:00 docs: record owner approval for public audit evidence publication
```

The checklist used is `docs/superpowers/AUDIT-CHECKLIST.md`; its latest commit
on the refreshed dev base is:

```
7e0c5527f42bec9beee164773c279e6ce077e964 2026-09-07T23:39:11-05:00 fix: install receipt routing before entries and preserve rejection evidence
```

All source line references below refer to the **running revision**, inspected
with `git show 317388e7ab50:<path>`. Freshness ledger, each from
`git log -1 317388e7ab50 --format='%H %cI %s' -- <path>`:

| File | Latest commit at running revision |
|---|---|
| `trader/invalidation_resolver.go` | `d280540835f2ddfc09a17497df45359084a8f95f 2026-09-03T10:54:48-05:00 feat(invalidation-wired): the system's own verdict refuses the arm; a position states the version it was armed under` |
| `trader/auto_trader_levelstate.go` | `d280540835f2ddfc09a17497df45359084a8f95f 2026-09-03T10:54:48-05:00 feat(invalidation-wired): the system's own verdict refuses the arm; a position states the version it was armed under` |
| `store/strategy.go` | `d3f711617d58176fb33b3bc3beda9b5bb897e19a 2026-09-03T22:33:00-05:00 feat(settings D6): saved → resolved · source, from the shipped resolvers` |
| `trader/entry_gate.go` | `01ce808839becd61120140e178bffa7cbc225d30 2026-09-05T12:12:00+00:00 fix(risk,planner): wire RiskForceFlat and BiasArmWarning — both shipped uncalled` |
| `kernel/scenario_state.go` | `2eaf7ab59ff1cf89ce88d0317d71a3f3390eff74 2026-08-26T15:21:56-05:00 FVG entry model — 5th scenario condition (pure-math play) (#79)` |
| `trader/auto_trader_wake_levels.go` | `fa86029e95fdd4ff82e093a7b721e1fb41c12372 2026-09-03T14:47:08-05:00 fix(wake): a clock seam — the enforcing cutoff made a fixed-fixture test time-of-day dependent` |
| `store/plan.go` | `4e901261778399f2e09e3f7f9b7e5f4168afae8b 2026-09-03T17:40:18-05:00 feat(plan): a lifecycle log, so trigger_reason stops answering the wrong question` |

## C1 — publication confirmed; claimed death time refuted

[A] Live SQLite was opened using `mode=ro`. The case is one version:
`plans.rowid=267`, logical ID `2026-09-07:ASIA:v4`, lifecycle `active`,
trigger `level_event`. Creation is `2026-09-08 04:40:33.408927189+00:00`,
or **2026-09-07 23:40:33.408927189 CT**. It contains exactly two scenarios:

| Scenario | Direction | Trigger anchor | Authored invalidation |
|---|---|---:|---|
| S1 | short | 29753.25 | `5m close above 29761.62 invalidates rejection` |
| S2 | long | 29761.62 | `5m close back below 29753.25 negates breakout` |

[A] The claimed refusal really appears at 23:45:18 CT:

```
entry-gate REFUSED arm ASIA: entry_gate: scenario S1 invalidated at 2026-09-07 20:51 CT (accepted through 29753.25) — price accepted through the level against the trade — it flipped roles
```

**That line does not establish acceptance through 29753.25 at 20:51.**
At 20:51:39 CT the historical log instead says:

```
scenario S1 → ≈invalidated @ 29687.50 (price accepted through the level against the trade — it flipped roles — display-only estimate, never execution-wired)
scenario S3 → ≈invalidated @ 29664.50 (price accepted through the level against the trade — it flipped roles — display-only estimate, never execution-wired)
```

[A] Those anchors match v1, `plans.rowid=264`, published 20:47:57 CT.
The v4 refusal combines an earlier stored timestamp with a different current
anchor. `store/strategy.go:1340–1341` keys the timestamp by trader, plan ID and
scenario ID, **omitting version**. `trader/auto_trader_levelstate.go:256–270`
stamps each evaluated invalidated scenario once and never overwrites a prior
nonempty stamp. `trader/invalidation_resolver.go:64–79` reads that stamp but
supplies the current evaluation's anchor and reason.

[A] `system_config.rowid=190` holds S1's `2026-09-07 20:51 CT` timestamp.
Row 191 holds S3's same timestamp even though v4 has no S3. This is additional
direct evidence that the namespace survives scenario reuse across versions.

[A] The evaluator selects trigger text before invalidation text
(`kernel/scenario_state.go:96–132`) and applies a generic dangerous-direction
acceptance verdict (`:188–210`). It does not parse S1's authored 29761.62 rule.
The resolver evaluates bars windowed since the current plan's birth
(`trader/invalidation_resolver.go:45–50`).

**Correction:** E1's proposed “accepted through 2h49m earlier” live fixture
cannot use this gate line as its evidence. Whether v4 was already invalid by
its *own written conditions* at publication remains **NOT ESTABLISHED**.
The complete write-time validation path and historical tape replay were not
completed after the required STOP.

## C2 — current all-dead premise not reproduced

[A] During the 00:05 CT measurement, one status row,
`system_config.rowid=188`, held:

```json
{"S1":"armed","S2":"invalidated"}
```

Companion metadata row 189 held v4's confirmation references 29753.25 and
29761.62. These are mutable, unversioned status records, not a durable history.
The measurement does not disprove an earlier both-invalidated observation,
but it **does not reproduce “both dead” now**. No tradeable count is inferred
from these heuristic labels. A complete search of all UI/count surfaces is
not completed.

## C3 — lifecycle count confirmed; absence of timestamps refuted

[A] `plan_lifecycle_log` contains **n=11**, IDs **1–11**, all `active` or
`dormant`. Latest is ID **11**, v3, dormant by flip at
`2026-09-07 23:33:19.779859054-05:00`.

[A] There are already persisted scenario invalidation timestamps outside that
table: S1 at row **190**, S3 at row **191**, and **S2 at row 197**, whose value
is **`2026-09-07 23:45 CT`**. These stamps lack the required version, condition,
price and cause record, but scenario death is not exclusively log text and
S2's stored timestamp is not absent.

[A] `kernel/scenario_state.go:235–265` evaluates all scenarios. The gate resolver
also calls that full evaluator before selecting the cited ID. The recorder
loops over **all** evaluations (`trader/auto_trader_levelstate.go:260–270`).
An entry refusal returns for its cited scenario; that does not prevent the
separate recorder from stamping S2. The claim that S2 has no time *because the
gate stops at S1* is therefore unsupported and contradicted by row 197.

The `display-only estimate, never execution-wired` log text is stale:
the arm gate actually consumes the same evaluator's verdict
(`trader/entry_gate.go:223–250`, `trader/invalidation_resolver.go:49–50`).

## C4 — four suppressions confirmed, prices and governing check corrected

[A] The narrowly selected journal interval 23:39–23:49 CT contains these
**n=4** level-wake suppressions, identified by their exact timestamps:

| Timestamp CT | Close | Seated level | Elapsed printed | Refusal |
|---|---:|---:|---:|---|
| 23:41:18 | 29746.25 | 29708.25 | 4m | `wake_min_interval_min (30m)` |
| 23:43:18 | 29746.25 | 29708.25 | 6m | `wake_min_interval_min (30m)` |
| 23:45:18 | 29758.50 | 29708.25 | 8m | `wake_min_interval_min (30m)` |
| 23:47:17 | 29758.50 | 29708.25 | 10m | `wake_min_interval_min (30m)` |

All identify seated `OB(bear)·4h (HTF)`. The first two do **not** print the
dispatch's claimed 29758.50 close. None of these four cites the cutoff.

[A] `trader/auto_trader_wake_levels.go:274–277` returns at this minimum-interval
check, before the class-47 cadence decision and fast-market measurement at
`:289–320`. The latter's bypass handling is at `:329–343`, followed by its
separate cooldown refusal. The two cooldown concepts must be distinguished
when specifying the exhaustion bypass. The claimed earlier 1.9×ATR event and
governing boot line were not verified before the STOP.

## C5, implementation and live proof status

The exhaustive running-revision wake inventory remains **UNVERIFIED**. No
conclusion is claimed from an incomplete search. The STOP was triggered by
the reproduced C1/C3 contradictions, not merely the missing unmerged audit.

No RED/GREEN pins, mutations, goldens, full Go suite, vitest, tsc, build,
checklist-number allocation, main-tree merge, RELEASE change, binary swap,
process signal, migration or live proof occurred. No new reject, wake trigger,
gate change or durable death recorder exists from this dispatch. The deployed
behavior is unchanged. There is no implementation rollback to perform.

**What the owner still sees wrong:** a gate refusal can attach an older
version's death time to a newly authored scenario; the “display-only” label
misstates an execution-wired heuristic; there is no new versioned death history
or tradeable-count surface from this work. No exhaustion wake or born-dead
refusal has been implemented or proven live.

**Required owner correction before resuming:** distinguish (1) versioned
recording of the existing evaluator's verdict from (2) D3 validation of the
scenario's own authored invalidation. Do not turn the contaminated 20:51 stamp
into a born-dead fixture or treat an anchor heuristic as the authored condition.
Confirm that exhaustion bypasses the minimum-interval throttle observed here
as well as the separate class-47 cooldown, while retaining cutoff and budget.

This report is preserved on the dispatch branch for review. It is not yet
merged to dev; the dispatch is paused under the owner's explicit STOP rule.

## Revised implementation (owner resumed the wave)

[A] On resumption the running health revision was `33672fdd2cd2`, PID 3260027.
The independent born-dead case is **one scenario**, ASIA v2 S1, `plans.rowid=265`,
published **22:03:44.933209 CT**. Its exact invalidation is:
`5m close below 29664.50 (SWG-H·15m) kills the setup`.
The immediately preceding completed five-minute candle is `bars.rowid=451050`,
open epoch-ms `1788836100000`, close **29661.50**, convention `epoch_floor`.
Its **n=5** constituent minute bars independently agree:

| Minute row ID | Open epoch-ms | Close |
|---|---:|---:|
| 451031 | 1788836100000 | 29668.75 |
| 451034 | 1788836160000 | 29668.50 |
| 451037 | 1788836220000 | 29668.50 |
| 451039 | 1788836280000 | 29665.25 |
| 451051 | 1788836340000 | 29661.50 |

This supports D3 independently of the invalid 20:51 claim. It does **not**
establish that every scenario of v4 was born dead. The full production retry
loop accepted the bad candidate on the baseline and retries it after the fix.

D1 remains **WARN-only**. The four level-event suppressions establish that the
minimum-interval throttle fired; they do not establish that an exhaustion
trigger caused a blocked action or missed fill. No exhaustion wake, exemption,
cutoff override or budget change was implemented. The existing recorder emits
one exhaustion warning/event per version when all current evaluator statuses
are invalidated/expired. Unknown and empty scenario sets do not exhaust.

D2 extends the **existing recorder**, without a new table or a migration.
`ScenarioInvalidatedAtKey` now uses a separate `scenario_death:` namespace
containing trader, plan ID, **version**, and scenario ID. The JSON record carries
plan/version/scenario, judged anchor, observed price, cause, evaluated condition,
basis and first-observed time. Atomic insert-on-conflict preserves the first
record. Readers reject mismatched anchors/identity instead of attaching an
older timestamp to a different verdict. Legacy unversioned records are retained
untouched and are never imported. Status and metadata keys are also versioned;
the plan API reads the displayed version. History remains history: the existing
scenario evaluator may subsequently return armed again. No gate verdict changed.

D3 validates every candidate after the model call, inside the existing retry
loop. It recognizes a deliberately narrow **complete** grammar: explicit one or
two five-minute closes above/below a numeric threshold, optional reference label
and simple invalidation wording. It requires every constituent minute of the
latest fully completed rule window and ignores the forming bucket. Duplicate,
missing, malformed and non-finite bars are UNKNOWN. Compound/sequential/MSS and
subjective wording are UNKNOWN. Each UNKNOWN is named, counted and accepted for
this check. Known invalidation refuses the **candidate**, preserving the authored
scenario set rather than silently deleting scenarios; the existing model repair
attempts re-author it. Existing retry counts, fail-closed behavior and replan
accounting remain in place. Authoring time is captured after the model returns;
the accepted row carries the instant at which this check ran.

D4: the active plan card and PLANNER desk line show `tradeable N/M` from the
current evaluator snapshot, with an EXHAUSTED warning at zero. This is scenario
liveness, not order eligibility. Missing/unevaluable/stale version snapshots
show UNKNOWN. Each available invalidation record shows its version, anchor,
cause and recorded first-observed timestamp. It never substitutes render time.

D5: `PlanLivenessBootLine` reads persisted event counts. Startup has no active
snapshot yet and prints `tradeable=n/a`. The exhaustion field is now
`exhausted-warnings`, not a fictitious wake count, reflecting the revised scope.
Unavailable counters print UNKNOWN; telemetry does not panic. The Guide and
SYSTEM-MAP are updated with the enforcement split and the remaining limitations.

### RED, GREEN and mutation evidence

[A] The fixed-clock production-recorder/gate pin failed before the key change:

```
v2 S1 must retain its own anchor 29753.25 and death time 2026-09-08 11:00 CT;
got {Invalidated:true AtCT:2026-09-08 10:00 CT Anchor:29753.25 ...}
```

[A] The production retry/write pin failed before the D3 call was wired:

```
born-dead candidate must retry before publication; unsupported repair accepted:
version=1 lifecycle=active calls=1 err=<nil>
```

[A] The production recorder exhaustion pin failed before observation was wired
at **both 08:00 CT and 23:45 CT** (26 minutes of minimum interval remaining):

```
exhaustion must record one warning per version despite repeated cycles and 26m
throttle remaining: counts={DeathsRecorded:1 ExhaustionWarnings:0 BornDeadRefusals:0 AuthoredUnknown:0}
```

[A] All three tests subsequently passed (`go test ./trader -run
'^TestPlanLiveness' -count=1`). The UI pin initially failed because
`data-testid="plan-liveness"` did not exist; it now renders tradeable 0/1,
EXHAUSTED, the exact stored timestamp and 29753.25 anchor.

[A] Deliberate mutations were applied one at a time and restored in `finally`:

| Mutation | Actual failure |
|---|---|
| Force all death keys to version 1 | `v2 S1 must retain its own anchor 29753.25 and death time ...11:00 CT; got {Invalidated:true AtCT: Anchor:29753.25 ...}` |
| Remove `validateAuthoredScenariosAt` from the candidate loop | `born-dead candidate must retry ... version=1 lifecycle=active calls=1 err=<nil>` |
| Remove `observePlanExhaustionAt` from the existing recorder | `exhaustion must record one warning per version ... ExhaustionWarnings:0` at both fixed clocks |

Supplemental pins cover concurrent first writes, retained versions, anchor
mismatch, versioned/stale/unevaluable count snapshots, both price directions,
equality, two-close runs, missing/duplicate/non-finite bars, forming candles,
unsupported/compound/sequential wording, and desk/boot reads. The clock-seam lint
passes for the new delegates; the clock callback supplies the actual authoring
instant after a potentially long model call.

[A] Initial frontend validation: **50 files / 366 tests passed**, `tsc --noEmit`
passed. Initial targeted backend packages kernel/store/trader passed. The full
Go suite and final merged-head checks are recorded in the closeout below once
completed. No build/deployment success is asserted by this intermediate update.

### Production call sites

The existing recorder calls `RecordScenarioDeath` and `observePlanExhaustionAt`;
the resolver calls `ScenarioDeathFor`. The candidate retry loop calls
`validateAuthoredScenariosAt`, which calls `EvaluateAuthoredInvalidationAt`.
`RecordPlanLivenessEvent` is called at the warning/refusal observations.
`PlanLivenessCounts` is read by the main boot line. `ScenarioLivenessFor` is read
by the API and desk. The production card renders `PlanLiveness` and passes death
records to the scenario list. New clock wrappers delegate to their clock-aware
implementations; the scope does not modify the signal-clock files.

### What remains wrong / proof not yet observed

No live cutover has occurred in this dispatch. No first live exhaustion warning,
new versioned death record, born-dead refusal, new boot line or live card count
has yet been observed. The old binary can still exhibit the cross-version label
until cutover. Unsupported authored invalidations remain accepted as UNKNOWN;
this is explicitly not a general natural-language invalidation engine. A first
observation is not the candle's original death time. Historical legacy stamps
remain unassignable to versions. The current heuristic can revive a scenario;
this wave records that history without making its verdict terminal.

Rollback: restore the verified previous binary and RELEASE under the owner's
cutover procedure. New system_config keys are additive; do not delete or rewrite
legacy evidence or reset the database. No live DB migration was required or run.

### Additional source freshness ledger

The following is `git log -1 --format='%H %cI %s' -- <path>` at the implementation
base, before these edits. NEW means this wave introduces the file.

- `api/handler_plan.go`: `ae9bd136d1d20df072d4a5d95c30e0beb08eb749 2026-09-07T22:11:20-05:00 fix(plan): select placement by version and expose three price sources`
- `cmd/sandbox-seed/main.go`: `54cbcceca328b27e298760c66dc2ee3c010d5fb4 2026-08-27T14:11:57-05:00 level-truth T4: MarkConsumed records the consuming touch (times_tested=1 + last_play_ms when born already-accepted) — consumed rows always carry ≥1 touch`
- `main.go`: `6310eaf8941f53194fa2c5e7368552eed1ed8d64 2026-09-07T23:51:51-05:00 merge: reconcile placement confirmation with pre-send identity and owner ruling`
- `store/strategy.go`: `d3f711617d58176fb33b3bc3beda9b5bb897e19a 2026-09-03T22:33:00-05:00 feat(settings D6): saved → resolved · source, from the shipped resolvers`
- `trader/auto_trader_levelstate.go`: `d280540835f2ddfc09a17497df45359084a8f95f 2026-09-03T10:54:48-05:00 feat(invalidation-wired): the system's own verdict refuses the arm; a position states the version it was armed under`
- `trader/auto_trader_planner.go`: `01ce808839becd61120140e178bffa7cbc225d30 2026-09-05T12:12:00+00:00 fix(risk,planner): wire RiskForceFlat and BiasArmWarning — both shipped uncalled`
- `trader/desk_facts.go`: `1064dca0e734d4caca8450533c64756b8331abbc 2026-09-07T21:52:21-05:00 fix(desk): date broker facts and distinguish scenario activation`
- `trader/invalidation_resolver.go`: `d280540835f2ddfc09a17497df45359084a8f95f 2026-09-03T10:54:48-05:00 feat(invalidation-wired): the system's own verdict refuses the arm; a position states the version it was armed under`
- `web/src/components/plan/ScenarioList.tsx`: `6262bf427276d3e00102de64247b122af743c1fe 2026-09-07T23:34:58-05:00 fix: stamp entry creation time and await received placement truth`
- `web/src/components/plan/SessionPlanCard.tsx`: `64b076d27f34041c7c82253dc63c891a707fbb7e 2026-09-06T14:38:14-05:00 feat(desk-strip): the component, the four truth fixes, the guide, the map and class 82`
- `web/src/guide/content/planCard.ts`: `6310eaf8941f53194fa2c5e7368552eed1ed8d64 2026-09-07T23:51:51-05:00 merge: reconcile placement confirmation with pre-send identity and owner ruling`
- `web/src/lib/api/plan.ts`: `ae9bd136d1d20df072d4a5d95c30e0beb08eb749 2026-09-07T22:11:20-05:00 fix(plan): select placement by version and expose three price sources`
- `kernel/plan_authored_invalidation.go`: `NEW in fix/plan-liveness`
- `store/plan_liveness.go`: `NEW in fix/plan-liveness`
- `trader/plan_liveness.go`: `NEW in fix/plan-liveness`
- `web/src/components/plan/PlanLiveness.tsx`: `NEW in fix/plan-liveness`

### Review follow-up

The first complete Go run failed only `TestP0AScenarioStatusKeyIsTraderScoped`:
its expected string still described the legacy key. The assertion now checks
the versioned shape as well as trader and version separation. Targeted tests
pass. Additional API reader coverage proves that an unevaluated displayed
version never borrows legacy or earlier-version status/metadata.

A review fixture caught ambiguous parenthetical wording: `5m close below 101
(only after breakout) kills the setup` initially parsed as an unconditional
rule. Its RED output was `Known:true Invalidated:true`. Annotation parsing now
accepts only recognized reference-label spellings; the fixture returns UNKNOWN.
Born-dead/UNKNOWN event records use distinct event identities, so three attempts
at the same fixed clock count as three; exhaustion alone deduplicates by version.

Partner propagation is not attempted against the existing sibling checkout:
`/home/hoang/vlautoagenttraderv1` is at `f6ae7597fb3bc9caeaaedb25ce8c3c48bca72247`
(2026-08-23), predating the documented 2026-08-29 history rewrite, and has three
pre-existing modified test files. Its root history differs from this repository.
The standing partner rule requires a fresh clone after that rewrite. No files,
refs or remotes in that checkout were changed. A transport patch can be handed
to the owner for application only after the required fresh-clone preparation.

### Merge census and scope

At the merge decision, the two-format census piped through `sort -n | uniq -c`
reported `1 89` as the highest occupied class and `2 75`, `2 76`, `2 77` as
pre-existing duplicates. This wave adds 90; no other entry was renumbered.
The implementation changes neither EntryGate's file/legs, the executor, stop
composition, reaper, level scoring/seating, signal-clock files, cadence values,
nor replan budget rules. The small companion edits update versioned record
consumers, boot output, API/UI, Guide and test fixtures.

### C5 inventory correction at the measured running source

At `33672fdd2cd2`, direct scheduled reads run through
`auto_trader_clock.go:98` → `auto_trader_planner.go:190/252`; death replans through
`auto_trader_planner.go:334/765`; MSS through `auto_trader_transition.go:156/206`;
and level-event reads through `auto_trader_wake_levels.go:250/376`.
There was no scenario-exhaustion authoring trigger in these paths.
The dispatch's other labels need qualification: `auto_trader_loop.go:80` is the
`fastMarketATR` threshold getter, not a planner trigger; the planner reads that
drift for its reasoning mode and the cadence code for its bypass. `discard_burn`
implements stale decision re-evaluation and delayed cycle kicks, not a distinct
scenario-exhaustion planner read. A kick reaches normal scheduled/read checks
at the top of the cycle (`auto_trader_clock.go:897–905`). Budget-related
"exhausted" text also exists in the planner, not only the MSS comment.

Freshness for these additional running-source citations:

- `trader/auto_trader_clock.go`: `3f23d9bb5404696c7eeabc43bb133c118d60a6f7 2026-09-07T19:30:13-05:00 feat(session-calendar): the fold — one calendar, one owner, and the sourced close times win`
- `trader/auto_trader_loop.go`: `0eddf90e95184c106d5a8c6f8fabaa8059d667f5 2026-09-07T19:30:13-05:00 feat(session-calendar): D5 the MODE line, D6 the boot line, the Guide, the map and class 84`
- `trader/discard_burn.go`: `e424ec41bf60bbf18ec05a12ee5b6945aa46876e 2026-08-20T00:53:41-05:00 fix(T3): ONE timeframe table (kernel/timeframes.go) — the three drifted private copies delegated (stale_data 1m/5m/15m-only, dodge, NT8 bridge with its silent unknown→60s CloseTime fabrication); unmapped primary TF is now a named BOOT FAIL; 3m/30m gain forming labels + correct interval math`
- `trader/auto_trader_transition.go`: `21b3e75e3243839bfddcf39e80f630363af5f03e 2026-09-03T22:50:35-05:00 fix(class 60/72): clock seams for every time-dependent rule + the flake was a clock`

### Merged validation and candidate build

[A] Code and report merged to dev at
`393712c1bcbc767de0318517d5f2823a1907d374`. Both remote refs were verified equal
to that SHA. At that merged head:

- `go test ./...`: PASS (including trader, store, API and all broker packages).
- `go test ./kernel -run 'Golden|SelfCheck' -count=1`: PASS.
- Vitest: **50 files / 366 tests PASS**.
- `tsc --noEmit`: PASS.
- All **14 new Go functions** have production callers; the three load-bearing
  mutation removals fail the intended tests. Frontend `PlanLiveness` is rendered
  by the real SessionPlanCard, and the card supplies the records to ScenarioList.

[A] After the merged-head suite passed, `go build -o vl-bin .` ran in the clean
clone `/tmp/vl-plan-liveness-build/vl` (leaf directory **vl**). Build metadata:

```
vcs.revision=393712c1bcbc767de0318517d5f2823a1907d374
vcs.time=2026-09-08T14:02:15Z
vcs.modified=false
SHA256=65ce253fe3dc602971ba88a558bb79437303d03e5c784ee3ae3fa85445711a96
```

`GUIDE_BUILT_REV` was set by reading this binary's `vcs.revision`, not by guessing
HEAD. The frontend is rebuilt after this guide-marker commit. This marker does
not claim a deployment: RELEASE, the running executable and the served dist are
unchanged. The final marker/report head is tested again before the frontend build.

The owner still needs to give explicit cutover GO. At preparation time (~09:00
CT), the routine A7 deploy window is closed; no mid-session exception was given.
There was no binary swap, process signal, live data.db change, RELEASE update or
boot marker. The fresh five-leg broker gate, in-flight check, backup, swap/verify,
owner kill, 90-second boot check and five-reference proof remain cutover work.
No historical log or fixture output is presented as a new live boot/death/refusal.

Operational note: the first shell-launched heartbeat keeper did not survive its
shell. The lock was still fresh and held by this session; it was renewed and a
keeper was restarted with `start_new_session=True`. It exits when the lock is
released. No foreign lock was cleared or reclaimed.

Production call-site census at the compiled code head:

```
EvaluateAuthoredInvalidationAt: trader/plan_liveness.go:22
PlanLivenessBootLine: main.go:341
PlanLivenessCounts: trader/plan_liveness.go:69
RecordPlanLivenessEvent: trader/plan_liveness.go:25, trader/plan_liveness.go:37, trader/plan_liveness.go:58
RecordScenarioDeath: trader/auto_trader_levelstate.go:271
ScenarioDeathFor: trader/invalidation_resolver.go:76, api/handler_plan.go:424
ScenarioLivenessFor: trader/desk_facts.go:675, api/handler_plan.go:429
observePlanExhaustionAt: trader/auto_trader_levelstate.go:283
planExhaustionPolicy: trader/plan_liveness.go:57, trader/plan_liveness.go:73
recordScenarioStateAt: trader/auto_trader_levelstate.go:183
runPlannerReadCoreWithFactsGradesClock: trader/auto_trader_planner.go:1467
scenarioInvalidationAt: trader/invalidation_resolver.go:34
scenarioInvalidationResolverClock: trader/invalidation_resolver.go:26
validateAuthoredScenariosAt: trader/auto_trader_planner.go:1787
```

### Final telemetry review and retained C4 lines

The initially built 393712c1 candidate is superseded by the telemetry review
fix below. It was not deployed. A fault-injection test of the event-ID source
made the panic-on-error UUID helper panic (`panic: injected entropy failure`).
The implementation now uses the error-returning UUID constructor and returns
that error to the existing WARN caller. The same fixture passes. Telemetry
failure cannot decide an entry, change a planner verdict, or panic the process.
The replacement candidate is rebuilt after the full merged-head suite.

[A] Fresh retained journal proof of the governing cadence configuration:

```
2026-09-08 01:33:46 CT: wakes: cutoff=25m(enforce) cooldown=30m(enforce, fast-market≥1.5×ATR exempt) cross-session=on stale-arm-expiry=on (class 47) — cutoffs govern LEVEL_EVENT/structure_mss wakes ONLY; scheduled reads, death re-plans and owner resets are untouched; the cutoff is NOT exempted by a fast market
```

[A] One retained bypass event, 2026-09-08 **03:20:00 CT**, names **1.8×ATR**,
25 minutes since the previous wake-authored version, 30-minute cooldown, and
seated Demand·1h invalidation (close 29512.75 below 29541.12). This verifies an
actual bypass of the separate cadence cooldown; the claimed earlier 1.9×ATR
line was not reproduced in this extraction. It does not change the reason for
the four 23:41–23:47 minimum-interval suppressions.

### Final replacement candidate

[A] Replacement code head: `94f0d7df8601eec585b38029ccafb90239bee90d`, merged to dev and verified on both
remote refs. `go test ./...` passed at this head. The kernel golden/self-check
coverage is included; the explicit golden run also passed at the earlier
identical kernel implementation. Vitest remains **50 files / 366 tests PASS**;
TypeScript passes. The fault-injection fixture now returns the entropy error
without panic.

The replacement was built in the clean `vl` clone **after** the complete
merged-head Go suite passed:

```
vcs.revision=94f0d7df8601eec585b38029ccafb90239bee90d
vcs.modified=false
SHA256=ce97e597ee86004002ce5c415c7f0d98aec0fe68251b59d6ec905696585f1981
```

`GUIDE_BUILT_REV` is read from this replacement binary. The earlier 393712c1
candidate and Guide stamp are superseded. Final marker-head checks and frontend
bundle verification are recorded in the local handoff manifest at
`/tmp/vl-plan-liveness-build/candidate.json` after they finish; no such manifest
is used as a substitute for live boot proof. Candidate binary and dist stay in
`/tmp/vl-plan-liveness-build/vl/`; the original dispatch worktree is removed
at handoff. The transfer patch is `/tmp/plan-liveness-transfer.patch` and has not
been applied to the stale partner checkout.

Cutover is not scheduled and no timer was created. The owner must be present and
give GO in the permitted flat window; all fresh broker/in-flight/window checks,
backup, RELEASE ordering, swap verification, owner kill, boot verification and
post-boot marker remain mandatory. This report's commit-pinned raw URL and byte
count are independently verified at publication and included in the handoff.

## Owner-authorized A7 cutover preparation — 2026-09-08

[A] Owner GO received before the next 14:45–16:30 CT window. The alternative
is after 17:10 CT, flat with no arms and no open position. No mid-session
override was given. Preparation does not authorize an early swap or boot;
the fresh five-leg gate, broker-snapshot leg 4 and in-flight check must pass
at cutover. The owner receives the exact kill command only after RELEASE →
`mv` → independent VERIFY, as required by dispatch A3/A7/A13. No timed deploy
is scheduled.

**The born-dead refusal ships on n=1 measured case:** plan row **265**,
2026-09-07 ASIA v2 S1, authored 22:03:44.933209 CT. Its authored invalidation
was a 5m close below **29664.50**. Stored 5m bar row **451050**, completed
22:00 CT, closed **29661.50**; the five constituent minute rows are
**451031, 451034, 451037, 451039, 451051**. The corrected v4 claim is not
a second case. The standing rule keeps an unmeasured REJECT at WARN; this
case was measured, so the owner authorizes the D3 refusal to ship. **n=1
does not establish representativeness.** The recorded born-dead-refusal
counter, together with its date/session/scenario/condition evidence, is
what will tell us whether the measured instance was representative. UNKNOWN
still accepts with a named warning. Exhaustion remains WARN + counter and
does not change wake policy.

[A] Refreshed origin/dev and fix/plan-liveness both resolve to
`b0f95bc6bdd133124061a68d20ae66328fd70903` at preparation. Existing claim
resumed in isolated locked worktree `/tmp/vl-plan-liveness-cutover`.
Source freshness read before changes:

```
b0f95bc6bdd133124061a68d20ae66328fd70903 2026-09-08T09:16:09-05:00 docs(plan-liveness): pin final verified candidate and pending cutover — report; web/src/guide/types.ts
393712c1bcbc767de0318517d5f2823a1907d374 2026-09-08T09:02:15-05:00 docs(plan-liveness): assign class 90 at integration — AUDIT-CHECKLIST.md
268ee6097b1aa2c7979552018f004b548592f182 2026-09-03T19:46:13-05:00 feat(F12): cutover leg 4 reads the broker; the override guard becomes a check — trader/class33_cutover_gate.go
```

The replacement candidate will be built from the merged report head after
the full suite, with the actual binary's vcs.revision used for GUIDE_BUILT_REV
before rebuilding dist. Earlier candidate manifests remain historical until
the replacement's metadata, hash, suite SHA and bundle are recorded.

### A7 candidate built after the merged-head suite

[A] `go test ./...` and the explicit kernel golden/self-check run passed at
merged dev HEAD `04a62a0e31868ac9618010e915215284574353da`. Vitest: **50 files /
366 tests PASS**; TypeScript passes. A fresh ordinary clone at
`/tmp/vl-plan-liveness-a7-build/vl` built the binary after those checks:

```
vcs.revision=04a62a0e31868ac9618010e915215284574353da
vcs.time=2026-09-08T18:59:19Z
vcs.modified=false
SHA256=7e6ad8884a0599b682d2cac5ef5f83dbbd472b1034ca55989590e4c28454928e
```

The only changes from the earlier verified Go implementation are report and
Guide metadata. `GUIDE_BUILT_REV` is set by parsing this binary's build
metadata; dist is built **after** this stamp. The Guide/report marker itself
does not claim a deployment. Final marker-head verification and the bundle
manifest are retained in `/tmp/plan-liveness-a7/`.

[A] The n=1 amendment at commit `04a62a0e31868ac9618010e915215284574353da`
returned pinned raw HTTP **200 / 35,336 bytes**, byte-identical to its git blob.

[A] Preparation gate at **14:00:57 CT**, n=1 running trader, HTTP 200:
DB open positions 0; API positions 0; NT8 positions snapshot 0; broker working
orders 0 with ledger agreement 0, source `broker — NT8 order_snapshot frame
(age 27s, build 2026-09-07-h1)`; no planner read claimed. All five legs passed.
The gate's legacy trailing note still claims no NT8 working-order frame;
the actual leg-4 source is the broker snapshot quoted above. This preparatory
read is not reused at swap time. Running process **3260027**, health and
`/proc` revision **33672fdd2cd2fee60a2c562a9693e06ab3b13551**, remains unchanged.
No process `VL_EXPECTED_REVISION` override is set; the RELEASE file governs.

The next permitted window begins **14:45 CT**. Until then: no RELEASE change,
no binary/dist swap, no kill, and no new live proof. The main lock is released
after preparation and reacquired with an independent heartbeat at cutover.

### Combined cutover owner ruling

The owner authorized both waves in one A7 boot and clarified that A31 scopes
what this lane authors, not legitimate changes inherited from the other lane.
PLAN-LIVENESS is the deploy owner, not the author of CONFIRMATION-TRUTH.
The combined record names both lanes' commits, the deploy owner's own suite,
build and gate, and the approved **23 + 1 = 24** historical validation
REJECT→PASS observations. Earlier A7 candidate/hold sections are historical.
