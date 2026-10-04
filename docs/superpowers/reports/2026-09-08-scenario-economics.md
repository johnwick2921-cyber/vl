> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# Scenario economics — new authoring contract, legacy UNKNOWN preserved

**Latest cutover state — BOOTED 18:11:54 CT at `6f677b55`:** economics, confirmation and liveness are live; legacy UNKNOWN is by design. The authorized W7 clock correction passed. Stage A capture is DISABLED by a reproduced relative-path archive defect, so combined-wave proof is incomplete. [Actual boot marker and evidence](2026-09-08-scenario-economics-cutover.md).


2026-09-08 · branch `fix/scenario-economics` · session `scenario-economics-83f741b2/root[unlisted]`.

**Implemented and candidate built; not deployed.** New authoring must provide a complete economics declaration. The existing write parser refuses missing required fields and the three approved numeric contradictions. Stored legacy reads keep missing economics UNKNOWN; role differences and sub-1R obstacles remain WARN plus recorded counters. No target policy, arm authorization, trading gate, floor, confirmation or cadence change is introduced.

The owner approved the corrected premises after the initial measurement STOP at `59af58fd54a74f1422296ac651a0c687c2072d33`: new-authoring-only completeness, unchanged legacy acceptance, role differences WARN, corrected C5 and **C6 NOT ESTABLISHED — dropped**. That pinned earlier report preserves the original correction record. Its STOP is resolved by the owner's subsequent ruling. This report describes the approved implementation and remaining deployment proof.

## Runtime, scope, and fresh evidence

[A] Accept base `72bb9de08f7fee83f2b45a5219ec14bd17a30340`, current `origin/dev` at accept. The isolated, locked worktree was `/tmp/vl-scenario-economics`. Required claim helper created `fix/scenario-economics`; the subsequent **ls-remote SHA** was `7262e50253cdb4f45d529b0463ccd914b37cb7a2`.

[A] At 15:36:55 CT, `/api/health` returned `revision=f8bc7044cc44`, `status=ok`; `/proc/3566770/exe` reported full revision `f8bc7044cc44d58e84904a0a7761e78b420404af`, `vcs.modified=false`. Only nonsensitive floor overrides were inspected: neither `MIN_SL_ATR_MULT` nor `ARM_STOP_ANCHOR_MAX_ATR` was set. The code defaults are 1.5 and 3.0. [Runtime receipt](2026-09-08-scenario-economics-data/runtime.json).

[A] The independent `/api/plan/today` read at **15:35:59 CT** returned HTTP 200, `found=false`, `is_active=false`, `active_session=""`, `session=""`, scenario IDs `[]`. London v2 is a retained historical document at this observation, not the active plan. Its `plans.rowid=270`, creation **2026-09-08 02:08:48 CT**. Calling it “live London v2” now would be wrong.

[A] `measure.py` used a single read transaction through SQLite `mode=ro`. It read authored JSON and the immutable indicator block, independently computed Decimal arithmetic, and did not call production geometry/validation functions. No trades or P&L rates were computed. The date floor follows `store.DayPlanEraStart`'s **2026-08-15 CT** date; no pre-era rows were included. `pnl_corrected`/NULL/exclusion rules remain applicable to any later outcome study, but no outcome columns were queried here.

The retained database had **276 plan rows, 799 scenarios**. Two weekly documents (rows **223, 257**) have no scenarios, hence 274 scenario-bearing plans. The frozen CSV's 280 members map to 100 scenario-bearing plans, plus those two weekly plans gives the cited 102 versions. All 280 logical scenario identities matched the current database; all 111 complete frozen entry/stop/target triples matched their raw CSV fields exactly. Membership, row IDs, versions and scenario indexes are saved for every row; repeated versions are not independent trades.

- [Reproduction script](2026-09-08-scenario-economics-data/measure.py)
- [Census and complete plan-row denominator](2026-09-08-scenario-economics-data/census.json)
- [Every scenario and derived geometry](2026-09-08-scenario-economics-data/scenarios.json)
- [Every literal role diagnostic](2026-09-08-scenario-economics-data/role-diagnostics.json)

## Pinned research and source freshness

Both requested documents are evidence at their specified immutable revisions. Neither path exists on dev in the requested form. They were not substituted with a branch URL or treated as a new dispatch.

| Basis | Exact revision | HTTP / downloaded bytes = Git blob bytes | `git log -1 -- <file>` at that revision |
| --- | --- | --- | --- |
| [Trading-policy research](https://raw.githubusercontent.com/johnwick2921-cyber/vl/982091d4d908f4a5b8b65022cedf5b8c7c8202d5/docs/superpowers/research/2026-09-08-trading-policy/README.md) | `982091d4d908f4a5b8b65022cedf5b8c7c8202d5` | 200 / 67,959 = 67,959 | `0ec5bd2c 2026-09-08T08:30:15-05:00 docs(research): publish four-policy trading study with 28 cited sources` |
| [Planner-preparation audit](https://raw.githubusercontent.com/johnwick2921-cyber/vl/6095ca58fe5901ba398be374e4f9d3488d0bed6b/docs/superpowers/reports/2026-09-07-planner-preparation-audit/README.md) | `6095ca58fe5901ba398be374e4f9d3488d0bed6b` | 200 / 77,345 = 77,345 | `6095ca58 2026-09-07T23:09:02-05:00 docs: record owner approval for public audit evidence publication` |

Research §02 separates authored geometry from actual admission/execution and corrects the six under-2R records. §03 requires explicit obstacle response and explains why a favorable final R is not evidence of profitable behavior at a nearby obstacle. §07 connects causal preparation, permission, geometry and management, and prohibits inventing partial exits. §09 says **“Do not prescribe now: a mandatory 1R first target”**. Its priority two is consistency between obstacle, response, arm target and risk geometry. §05 preserves a level's possible target/obstacle/invalidation usefulness even when it is unsuitable for entry.

The dispatch's governing RESEARCH LAW is: **“MAY: require that a scenario state its first opposing obstacle, its planned response there, its arm target and the implied R”** and refuse contradictory numbers; it forbids selecting a target family or declaring a placement superior. [O] This report chooses **no structural, fixed-R, ATR, partial, trailing or mandatory-1R target policy**. Numerical illustrations below are arithmetic, not market beliefs or estimated VL rates. No new [R]/[I] profitability claim is introduced.

[Source-freshness receipt](2026-09-08-scenario-economics-data/source-freshness.txt) gives exact `git log -1` for the files cited in the **initial measurement** and verifies their pre-change versions were byte-identical to running `f8bc7044`. `SYSTEM-MAP.md`: `e020885b 2026-09-08T14:21:34-05:00`; `AUDIT-CHECKLIST.md`: `78eed09b 2026-09-08T14:32:14-05:00`. The audit follows its PART 2 R1–R10. No implementation spec was taken from a stale worktree base.

## C1–C3: independent recomputation

[T/A] These are authored geometries before execution rounding, costs or arm-time stop composition. “First listed target” does **not** mean that the author has identified it as the first opposing obstacle; that missing declaration is part of this wave's proposed contract.

| Measurement | Frozen membership | All retained scenario documents |
| --- | --- | --- |
| Scenarios | 280 | 799 |
| Complete positive entry/stop/arm target, nonzero risk | 111 | 177 |
| Positive directional first-listed target distance below 1R | 45/111 = 40.54% | 69/177 = 38.98% |
| Arm target R below 2 | 6/111 | 17/177 |
| Correct-side stop AND target | 111/111 | 177/177 |
| Arm target absent from path, half-tick tolerance 0.125 | 4/111 | 13/177 |
| Arm target absent from path, D2 one-tick tolerance 0.25 | 4/111 | 13/177 |

All contributing IDs and exclusions are explicitly enumerated in `scenarios.json`: `complete`, `sub1`, `arm_under2`, `absent_half_tick`, `absent_tick`, and `frozen`. The remaining **622/799** lack a complete stored arm geometry, including **169/280** frozen scenarios. Their geometry remains UNKNOWN; no entry/stop is supplied from prose. The displayed arm is optional (`kernel/plan_doc.go:53`, `:90`); D1 must not accidentally mandate an enabled arm or expand the armable set.

### C2 — all six dispatched rows reproduce

| Plan row / scenario | Date/session/version | Entry | Stop | Arm target | Arm R |
| --- | --- | --- | --- | --- | --- |
| 170/S1 | 2026-08-31:NY:v3 | 29351.47 | 29408.52 | 29280.88 | 1.237336 |
| 180/S1 | 2026-09-01:LONDON:v4 | 29182 | 29212.5 | 29154.38 | 0.905574 |
| 182/S1 | 2026-09-01:LONDON:v6 | 29123.25 | 29147.25 | 29085 | 1.593750 |
| 185/S2 | 2026-09-01:NY:v3 | 29085 | 29125 | 29062.75 | 0.556250 |
| 187/S1 | 2026-09-01:NY:v5 | 29100.5 | 29130 | 29082.75 | 0.601695 |
| 252/S1 | 2026-09-04:NY:v2 | 29611.25 | 29481.5 | 29720 | 0.838150 |

Minimum in the frozen group is **0.556250**, row **185/S2**. The formula uses unrounded stored prices: `abs(target-entry)/abs(entry-stop)`. These observations do not reconstruct the contemporaneous R:R setting or prove that an under-floor order was admitted. For completeness, the other eleven retained under-2R observations are:

| Plan row / scenario | Date/session/version | Arm R |
| --- | --- | --- |
| 141/S1 | 2026-08-27:ASIA:v8 | 1.340291 |
| 142/S3 | 2026-08-27:ASIA:v9 | 1.916667 |
| 144/S3 | 2026-08-27:ASIA:v11 | 1.600000 |
| 145/S2 | 2026-08-27:ASIA:v12 | 1.766181 |
| 145/S3 | 2026-08-27:ASIA:v12 | 1.417023 |
| 146/S1 | 2026-08-27:ASIA:v13 | 1.350814 |
| 147/S2 | 2026-08-28:LONDON:v1 | 0.931507 |
| 147/S3 | 2026-08-28:LONDON:v1 | 1.388889 |
| 149/S2 | 2026-08-28:LONDON:v3 | 1.934524 |
| 272/S1 | 2026-09-08:LONDON:v4 | 0.527696 |
| 272/S3 | 2026-09-08:LONDON:v4 | 0.205964 |

### C3 — all four dispatched mismatches reproduce

| Plan row / scenario | Date/session/version | Arm target | Path |
| --- | --- | --- | --- |
| 178/S1 | 2026-09-01:LONDON:v2 | 29418.62 | 29447.5, 29435.75, 29422.12 |
| 194/S3 | 2026-09-01:ASIA:v7 | 29131.66 | 29099.85, 29113.23 |
| 230/S4 | 2026-09-02:ASIA:v14 | 29214.5 | 29235, 29228.75, 29218.25, 29212.5, 29207.5 |
| 259/S1 | 2026-09-06:ASIA:v2 | 29575.48 | 29545, 29566.02, 29587.75 |

Each remains absent at one tick, so changing the tolerance from half a tick to D2's one tick does not remove these four. The running code validates positive chain prices (`kernel/plan_doc.go:646`), while the arm gate computes R from `leg.Target` (`trader/armed_executor.go:1903`). The plan card renders `target_chain` at `web/src/components/plan/ScenarioList.tsx:216`. The prompt calls the chain guidance at `kernel/planner_prompt.go:722`. These different meanings are confirmed, not a claim that all displayed target prices are executable orders.

## C4 — literal role/use evidence; no new role rejection

[T/A] Row **270**, London v2:

| Scenario | Map index (zero-based) / level | Map role | Scenario use |
| --- | --- | --- | --- |
| S2 | 9 / Supply·1h, 29657.38, grade C | confluence | arm target and second target-chain value |
| S3 | 10 / SWG-L·5m, 29675.75, grade A | target | explicit `5m close above 29675.75` invalidation |

The census intentionally tests only two narrow diagnostic families: a pure confluence instruction referenced as an arm/path target, and a pure target instruction referenced numerically as invalidation. It normalizes whitespace/underscores and explicitly lists recognized synonyms in the saved script. Price matching is within 0.25 point. It counts a level/scenario once even when both arm target and path reference it. It does not invent entry references from unrelated numbers or treat arbitrary free text as an enum.

**Frozen: 81 level/scenario diagnostics, affecting 73/280 scenarios. All retained: 172 diagnostics, affecting 150/799 scenarios.** Every row, instruction, price and use is in `role-diagnostics.json`. This is a measured lower bound for these two literal families, **not an exhaustive semantic role-disagreement rate**. Other uses/prose remain unevaluated, not silently compatible or incompatible. No historical structured exception field exists. It would require a policy decision to interpret every role as exclusive; the research explicitly permits several uses. The lexical n exceeds ten but is not a basis here for choosing a role-refusal policy. The owner has ruled WARN plus counter; no role rejection is implemented.

## C5 — corrected arithmetic and contract-size boundary

**Owner ruling for the master plan:** this C5 correction supersedes the master plan’s C5 figures/equation. Amend the master plan to the equation and reference table below; the superseded `p*b - (1-p)*c` must not be reused. This report records the correction, without claiming the master plan has already been edited.

[A] For gross outcomes +bR and −1R, and cost cR on the trade:

`E[net R] = p*b - (1-p) - c = p*(b+1) - 1 - c`

Setting this to zero gives `p = (1+c)/(1+b)`. The dispatched expression `p*b - (1-p)*c` instead gives `p=c/(b+c)` and cannot yield the provided table. Research §03 at `982091d4...` already uses the correct equation.

| b | Break-even, c=0 | Break-even, c=0.04 |
| --- | --- | --- |
| 0.5R | 66.67% | 69.33% |
| 1R | 50.00% | 52.00% |
| 2R | 33.33% | 34.67% |
| 3R | 25.00% | 26.00% |

`0.5*0.5R + 0.5*3R = 1.75R`. These four examples and the weighted example are arithmetic, **n=0 observed trade outcomes**, not estimates of rates or fees.

[A] The production limit-placement call uses quantity **1** (`trader/armed_executor.go:1067`, also the shared placement seam `:2161`). A single contract cannot be split into half a contract. However, “one contract” must not be misread as a universal configured one-leg ceiling: the one running strategy row **a5b7662e-7bf7-49bb-9f09-7efa48f95ac8** stores `max_contracts_per_order=2`, `max_contracts_enabled=false`, `plan_mode=strict`, `min_risk_reward_ratio=2`. `armLegCapacity`/`splitLegCapacity` (`:731`, `:741`) read the positive capacity directly; they do not consult that enabled switch. London v2 S1 itself authors two one-contract legs. This is not an observed two-contract position and is not authorization to add size for partial exits. [Whitelisted configuration evidence](2026-09-08-scenario-economics-data/risk-settings.json). No account name or credential is retained.

## C6 — DROPPED — UNESTABLISHED

The claimed frequency of ATR-floor-bound composed stops was not established. C6 is dropped from this wave and supplies no implementation premise, policy, test expectation or frequency claim. Historical exploratory measurements remain at the original correction revision; they are not evidence that the composed floor bound those stops. No stop-floor or composition code changes.

## E6 — zero new legacy refusals

[A] The same read-only probe ran against the pre-wave clean clone at `72bb9de0` (production source identical to running `f8bc7044`) and the new implementation. **All 276 plan-read results, covering 799 retained scenarios, are identical.** Plans containing **394 scenarios** already pass today's read schema and still pass. The remaining **405 scenarios** belong to plans with pre-existing parse/schema failures; their exact existing errors remain unchanged. No new economics refusal is introduced on either group: **0/799 new legacy refusals; rejected-by-this-wave IDs `[]`**.

[Before](2026-09-08-scenario-economics-data/legacy-read-before.json) and [after](2026-09-08-scenario-economics-data/legacy-read-after.json) enumerate every plan row, scenario count and pre-existing error. They compare equal as JSON. [Probe](2026-09-08-scenario-economics-data/legacy-read-probe.go.txt): copy to `/tmp/legacy-read-probe.go`, then run `go run /tmp/legacy-read-probe.go /tmp/result.json` from the source revision being checked. It opens the database with `mode=ro`; it does not backfill or migrate anything.

The initial projected **799/799 missing-obstacle refusals** belonged to the rejected interpretation that new completeness applied to unchanged legacy reads. The owner explicitly corrected that interpretation. New model output cannot opt into legacy treatment: `ParsePlanDocCapped` supplies the trusted new-authoring boundary, independently of the JSON version. The legacy pin verifies accepted stored reads, absent economics after round-trip, UNKNOWN R values and no authoring-counter increment.

Separately, the D2-only numeric comparison identifies these **13/799** legacy path differences (1.63% of all scenarios; **13/177=7.34%** of complete geometries). The original four are **4/280=1.43%** or **4/111=3.60%**. The chosen denominator must be stated; none licenses refusing UNKNOWN geometry. These legacy rows remain untouched; only a newly authored candidate must add its target to the path or declare its exception.

| Plan row / scenario | Date/session/version | Arm target | Path | Projected reason |
| --- | --- | --- | --- | --- |
| 142/S2 | 2026-08-27:ASIA:v9 | 29645 | 29677.86, 29652.97, 29643.5, 29591.5 | arm target absent within 0.25 |
| 142/S3 | 2026-08-27:ASIA:v9 | 29615 | 29623.48, 29619.5, 29591.5, 29576.5 | arm target absent within 0.25 |
| 143/S1 | 2026-08-27:ASIA:v10 | 29625 | 29624.65, 29619.5, 29591.5 | arm target absent within 0.25 |
| 143/S2 | 2026-08-27:ASIA:v10 | 29625 | 29624.65, 29591.5 | arm target absent within 0.25 |
| 143/S3 | 2026-08-27:ASIA:v10 | 29592 | 29591.5, 29581.88, 29576.5 | arm target absent within 0.25 |
| 144/S2 | 2026-08-27:ASIA:v11 | 29591 | 29591.5, 29576.5 | arm target absent within 0.25 |
| 144/S3 | 2026-08-27:ASIA:v11 | 29570 | 29576.5, 29432.25 | arm target absent within 0.25 |
| 152/S2 | 2026-08-28:LONDON:v6 | 29620 | 29644.38, 29577.75 | arm target absent within 0.25 |
| 156/S1 | 2026-08-28:NY:v4 | 29654 | 29707.5, 29657.39, 29642, 29577.75 | arm target absent within 0.25 |
| 178/S1 | 2026-09-01:LONDON:v2 | 29418.62 | 29447.5, 29435.75, 29422.12 | arm target absent within 0.25 |
| 194/S3 | 2026-09-01:ASIA:v7 | 29131.66 | 29099.85, 29113.23 | arm target absent within 0.25 |
| 230/S4 | 2026-09-02:ASIA:v14 | 29214.5 | 29235, 29228.75, 29218.25, 29212.5, 29207.5 | arm target absent within 0.25 |
| 259/S1 | 2026-09-06:ASIA:v2 | 29575.48 | 29545, 29566.02, 29587.75 | arm target absent within 0.25 |

The retained legacy sample has no first-obstacle/R declarations from which to count historical beyond-target or stated-R contradictions; those historical counts remain unevaluable. The owner explicitly approved all three D4 contradiction refusals for **new authoring**. The beyond-target and R pins are synthetic declared candidates, not claims of measured legacy events.

The E3 example independently reproduces at **row 265/S2**, 2026-09-07 ASIA v2: entry 29664.50, stop 29640.00, arm target 29721.25, risk 24.50, arm R **2.3163265306**, first listed target 29671.42, R **0.2824489796**. It is a prospective WARN fixture, not a declared historical obstacle or an executed exit.

## Implementation, RED/GREEN, and production wiring

[Implementation source freshness](2026-09-08-scenario-economics-data/implementation-source-freshness.txt) quotes `git log -1` and function line locations at the merged candidate, separately from the pre-change measurement. The first implementation commit is `d5e2414e` (contract + card + desk + Guide + SYSTEM-MAP together); follow-up changes complete compatibility fixtures, telemetry containment and verification. The definitive candidate ref and merged-head suite/build receipts are recorded at publication/cutover.

- `kernel/plan_doc.go`: optional `PlanScenario.Economics` travels through existing plan JSON. `ParsePlanDocCapped` enters `parsePlanDocument(..., true)` and the new-authoring check; stored `ValidatePlanDocWithCaps` never invokes it. `ParsePlanDoc` retains generic legacy parsing. The parser stamps the contract version after successful checking, so a model-supplied zero version cannot bypass it.
- `kernel/scenario_economics.go`: schema, independent geometry projection, D4 checks, known role diagnostics, process counters and logging. Target-path tolerance reads the MNQ tick from `market.FuturesTickSize`; stated-R tolerance is converted back to price distance. An arm remains the geometry authority; a scenario without an arm may declare hypothetical geometry without changing the armable set. Every economics check logs PASS/REFUSED, obstacle/provenance/response, target, both Rs, issues, warnings and cumulative counters. PASS means this economics check passed, not that all later validators accepted or a plan was persisted. Logging failure cannot panic the loop or turn refusal into acceptance.
- `kernel/planner_prompt.go`: only scenario-contract instructions change. New completeness, exact field names, required response, both R formulas, three contradiction refusals, role warnings and explicit exceptions are stated. No target family is selected.
- `kernel/levels_volume_boot.go`: one call to `ScenarioEconomicsBootLine`; `main.go` already calls this boot entry. Contract/obstacle switches and counts are read from enforcement. A coherent path means membership or a declared exception. Counters describe new-authoring checks **since process boot, including retries**, not independent scenarios, persisted plans or trades. Startup zero counters are actual initialized process counters, not a historical census.
- `web/src/components/plan/ScenarioEconomics.tsx` and `ScenarioList.tsx`: every scenario, including unevaluable activation, shows existing entry/stop, declared obstacle/provenance/response, arm objective, path, and both independently recomputed R values. Missing legacy economics remains UNKNOWN even with a known old arm. Broker-accepted prices remain a separate surface. `web/src/lib/api/plan.ts` mirrors the optional object.
- `trader/scenario_economics_desk.go` plus the display-only registration/count in `trader/desk_facts.go`: the SCENARIOS row carries each scenario's obstacle and arm R, version and plan creation time. Legacy/undated inputs remain UNKNOWN. No planner/liveness/confirmation function changes.
- `web/src/guide/content/planCard.ts` and `SYSTEM-MAP.md`: same behavior commit; explain semantics, counter scope, UNKNOWN, no target prescription, and independently checked C5 reference arithmetic. GUIDE_BUILT_REV must be set from the clean candidate binary before dist at build time.

No `store/plan.go` edit or migration was needed: the existing writer persists the additive JSON object. The full production write-path pin proves that the first off-path candidate is refused, the repaired second attempt is accepted, and the stamped economics object reaches the stored row.

### Pins and mutations

[Original kernel RED](2026-09-08-scenario-economics-data/kernel-red.txt): all five new-authoring refusal cases failed with `new authoring accepted contradiction/missing contract`. E3 failed with `accepted economics discarded; cannot render/count first obstacle`. The legacy acceptance pin passed before implementation, as it must.

[Original UI RED](2026-09-08-scenario-economics-data/ui-red.txt): all eight cases failed because the production scenario card had no economics surface. GREEN after implementation: legacy UNKNOWN, E3 sub-1R accepted/counted/marked, all six C2 arithmetic rows, and rendering even with unevaluable activation. The Guide pin computes its four break-even rows independently and checks the correct expectation equation.

Kernel GREEN covers C3 row 178, missing obstacle and whole contract, long/short beyond-target refusal, implied-R tolerance at and beyond one registry tick, explicit target-path exception, missing/zero-version bypass, hypothetical geometry without arm authorization, both London role warnings/counters, sub-1R counter and marker, boot fields, production parser/boot call sites, and telemetry failure containment. The production desk pin calls `DeskStripAt`; the production write pin calls the planner retry/write path. Existing mock new-authoring fixtures were updated with complete declarations; their cap/cadence/repair/liveness/no-trade assertions were preserved.

[Mutation receipts](2026-09-08-scenario-economics-data/mutations.json) quote each **exact changed line before/after**, nonzero test exit and assertion failures. Each replacement matched exactly once, failed an assertion (not compilation), and was restored:

1. D2 coherence: disable the off-path conditional → real C3 accepted, pin fails.
2. D4 refusal: replace the aggregate contradiction error with `return nil` → refusal pins fail.
3. Parser call: disable the new-authoring call site → missing/contradiction pins fail.
4. Sub-1R counter: increment by zero → accepted scenario's counter pin fails.
5. Role counter: increment by zero → real London role-warning counter pin fails.

[A29 call-site census](2026-09-08-scenario-economics-data/call-sites.json): every one of the **12 new Go functions** has a production caller; no `production call sites: 0`. UI component is rendered from `ScenarioList`, not merely exported. No new `time.Now` entry point or clock seam was introduced; desk tests supply their own clock.

Development validation: full `go test ./...` passed; all **53 Vitest files / 377 tests** passed; `tsc --noEmit` passed. At merged source **95e7b420df0960edc67a91ff2a719fe105f441ae**, the ordinary clean clone `/tmp/vl-scenario-economics-build/vl` passed fresh full Go (`-count=1`), explicit prompt goldens, 53 Vitest files / 377 tests, and tsc. The binary was built after these checks and reports `vcs.modified=false`; its embedded revision supplied GUIDE_BUILT_REV **before** dist was built. [Candidate build receipt](2026-09-08-scenario-economics-data/candidate-build.json) includes SHA-256 and asset hashes. The preparation read at **16:25:36 CT**, n=1 running trader, passed all five gate legs; leg 4 came from a broker order snapshot age 5s, build `2026-09-07-h1`, with zero working orders and matching ledger. This read is **not** permission for a later swap; repeat the gate and in-flight/window check at cutover.

### Stage A separation and checklist

At accept Stage A was `250546854b...`, claim `af1ded7e` by `stage-a-snapshot-96604090/root[unlisted]`. At the implementation refresh it was `896aeea5abb2faa2daabe247b7c551ab9f30ac9a`. Its new recorder diff owns scorer/record hooks and `main.go`; this wave touches none of those. The shared `kernel/planner_prompt.go` diff adds `ResearchSnapshotID` to `PlannerInput`; ours changes only scenario-contract text, in a different hunk. SYSTEM-MAP must preserve both additions when whichever lane lands second rebases. Ownership is established by named branch/claim and diff, not Git author identity; this is not a claim of direct lane acknowledgement.

The accept checklist census used both formats, numeric sort and **`uniq -c`**: highest **91**, duplicates **75/76/77 count 2 each**. [Census](2026-09-08-scenario-economics-data/checklist-census.txt). At the locked behavior merge, the fresh census still had highest 91 and the same duplicates; this wave was assigned **class 92**. No other entry was renumbered.

## Owner-visible limits, rollback, and publication

A15: until the new binary and dist are deployed, the owner still sees the existing card/desk, without this economics surface or counters. No first live scenario under the new contract, organic contradiction refusal, sub-1R WARN/counter, or live card proof is claimed. Test rendering is not live proof. Current running confirmation/liveness remains `f8bc7044`.

After deployment, old documents will still show UNKNOWN economics; hypothetical non-armed scenarios remain non-executable; declared obstacle responses do not implement partial exits or a new management policy. The displayed ratios are authored geometry, not net expectancy or the final composed/broker-accepted order geometry. Role diagnostics cover recognized instruction categories; unknown prose is not converted into a refusal. Counters restart with the process and are explicitly labeled as such.

Rollback: there is no database schema migration or legacy backfill. Retain the verified previous binary/dist/RELEASE for the normal rollback procedure. The additive JSON remains in newly written rows; an old reader can ignore it, but the economics enforcement and display would no longer be live. No account, trader binding, size, R:R floor, stop floor, arm/execution rule or cadence setting changes are part of rollback.

Deployment requires this wave's explicit owner GO, merged-head suite in a clean clone named `vl`, verified `vcs.modified=false`, GUIDE_BUILT_REV from that binary before dist, fresh five-leg gate and in-flight check, then RELEASE → atomic mv → VERIFY → exact owner kill, boot proof and five-reference check. The prior combined-wave GO is not a scenario-economics boot approval. The correction report is already on dev at the pinned initial revision; the implementation report is to merge with the behavior and be verified again by exact commit URL/byte equality. The final publication receipt supplies that immutable SHA and HTTP/byte result.


### Candidate handoff

Candidate source is `95e7b420df0960edc67a91ff2a719fe105f441ae`; subsequent Guide/report commits are preparation metadata, not a different Go binary. No RELEASE change, service-file replacement or kill occurred. Running health and RELEASE remain `f8bc7044`. The prepared binary is `/tmp/vl-scenario-economics-build/vl/vl-bin`; candidate dist is that clone's `web/dist`. Deployment remains owner-GO gated under A3, with A7's 14:45–16:30 CT window or after 17:10 flat/no arms/no position, never 16:45–17:10. If another lane merges before GO, rebuild and gate the resulting merged head rather than silently deploying this older candidate.


### Authorized cutover preparation — 2026-09-08

The owner has given GO after 17:10 CT, with no arms or open position and a fresh five-leg broker-backed gate. This authorizes preparation and cutover; it does not assert a boot has occurred. At 16:40 CT Stage A held a fresh main-tree lock, so this lane waited and prepared only in its isolated worktree. The final merged source and lane provenance will be recorded at cutover.

The economics boot line explicitly ends `legacy UNKNOWN by design`; its other fields read the enforcing policy and process counters. The wording pin first failed with `boot must explain intentional legacy UNKNOWN` against the prior line, then passed after the clarification. Legacy UNKNOWN is intentional missing historical information, not a new schema defect or evidence of a failed boot.


## Cutover STOP — existing W7 fixture crosses the CME day boundary

[A] The owner authorized cutover after 17:10 CT with no arms or open position and our own fresh gate, then explicitly instructed us to stand by until Stage A booted or released. Stage A released without booting. This lane acquired the free lock at 17:16:26 CT with an automatic heartbeat and merged its clarification `c40bb45a6e47c796810441cbf15069f2bee19e1e` onto dev `3c09651ca5abf3177c83be0d9470ac0f1e5e0ed7`, producing **`ed4567c7e17f3b5c9b5a4a447ace72565fb08c1d`**. The fresh ordinary clone is `/tmp/vl-scenario-economics-final/vl`.

[A] At that merged head, full Go exited **1**: `TestW7LevelBurnedStaysBurnedAcrossSessions`, `trader/w7_levelstate_test.go:119`, reported `touched-and-accepted-through level must be consumed, got consumed=false freshness=A` at 17:20:13 CT. Frontend **54 files / 378 tests**, TypeScript and explicit goldens passed. A23 stops cutover; A31 does not authorize this lane to change level-state behavior. No build followed the failed full suite.

[A] Reproduction: **3/3 isolated runs failed** at the merged source, and **3/3 failed** at retained clean clone `72bb9de08f7fee83f2b45a5219ec14bd17a30340`, whose Go production/test source is identical to running `f8bc7044`. The full-suite failure plus these six isolated failures are seven failed observations, not seven live trading events. [Merged failures](2026-09-08-scenario-economics-data/w7-reproduction.txt), [running-source failures](2026-09-08-scenario-economics-data/w7-running-baseline.txt). The W7 fixture itself has no diff between running and merged source. This evidence does not support attributing the failure to the economics contract or declaring a new Stage A regression.

[A] The fixture generates 20 minute bars from `time.Now()-22m`, with the sole initial touch and a final price 27 points beyond the level. `DailyRangeProxy` prefers completed CME-session-day ranges; `recordLevelState` filters levels using `ActivePlanLevels` before evaluating consumption. A fixed-clock probe of those production functions gives:

| Fixture clock CT | Range proxy | Activation band (1.5 × range) | Distance | Active level count |
| --- | --- | --- | --- | --- |
| 17:17:13 | 18 | 27 | 27 | 1 |
| 17:18:13 | 17 | 25.5 | 27 | 0 |
| 17:19:13 | 16 | 24 | 27 | 0 |
| 17:20:13 | 15 | 22.5 | 27 | 0 |
| 17:21:13 | 6 | 9 | 27 | 0 |
| 17:22:13 | 33 | 49.5 | 27 | 1 |
| 17:23:13 | 33 | 49.5 | 27 | 1 |

These are **n=7 synthetic clock points**, not observed market samples. [Probe output](2026-09-08-scenario-economics-data/w7-clock-probe.json), [probe source](2026-09-08-scenario-economics-data/w7-clock-probe.go.txt), and [pinned source freshness](2026-09-08-scenario-economics-data/w7-source-freshness.txt). [B] The failed assertion is explained by the clock-dependent fixture leaving the activation window before consumption can be evaluated. Waiting for a later clock to turn the suite green would not correct this defect; a deterministic fixture correction belongs in an explicitly scoped follow-up before this STOP is lifted.

[A] Our 17:10:17 CT preparation gate passed all five legs for n=1 running trader: no DB/API/NT8 positions, broker working orders 0 with ledger 0, order snapshot age 15s/build `2026-09-07-h1`, and no planner read claimed. That read is stale and is not cutover authorization. Backup at 17:18:30 CT: `/home/hoang/vl-backups/scenario-economics-20260908-171830`; database 751,734,784 bytes, `integrity_check=ok`; executable preserved as `vl-bin.old.f8bc7044cc44d58e84904a0a7761e78b420404af` after checking its embedded revision, SHA-256 `e2c2ce8602ca61e52d180309743593b3bf538337693e4f2c61ae83c21d457918`; prior RELEASE and served dist also preserved.

Lane provenance: economics contract `d5e2414e`, verification `0533c8d0`, class assignment `95e7b420`, Guide/receipt `c98ed6f2`, clarification `c40bb45a` belong to the named `fix/scenario-economics` branch/worktree. Stage A recorder changes `896aeea5`, `0babd090`, integration `13017618`, class/receipt `b167f597`, dev merge `2a96cf63`, and preparation receipt `3c09651c` come from `fix/stage-a-snapshot` and its claim `af1ded7e`; its recorder hooks, including those in shared files, were not authored by this lane. Clean-machine documentation `04c5f86c` came from `docs/clean-machine-readiness`, merged at `98d76e9b`. Provenance is taken from branch/claim records, not identical Git author identities; no direct peer acknowledgement is claimed. This lane assembled and gated the merged source but **did not build or boot it after the failed suite**. No passed boot marker exists for this attempted cutover.

C5 remains the master-plan correction: `E[net R]=p*b-(1-p)-c`, so break-even `p=(1+c)/(1+b)`; at b=0.5/1/2/3 the c=0 rates are 66.67/50/33.33/25%, and at c=0.04 they are 69.33/52/34.67/26%. The master plan must be amended to this reference; no amendment there is claimed here. **C6 is DROPPED — UNESTABLISHED**, not a retained implementation premise. Legacy economics remains UNKNOWN by design; the unbooted clarification says so explicitly.


## Owner-authorized class-60 correction

The owner explicitly authorized a test-only deterministic fixture correction and, if necessary, an `…At(now)` production seam with zero behavior change, then resumption of cutover. **The W7 failure was pre-existing, not introduced by scenario-economics or established as a Stage A regression.** The running-source 3/3 failures remain above.

`recordLevelState` now has one executable statement, `at.recordLevelStateAt(time.Now())`, and is registered in `clock-seams.list`. Its former body is `recordLevelStateAt(now)`: byte-for-byte identical after removing the old local `now := time.Now()` assignment. Thus clock capture is at entry and no gate, activation, consumption, scoring or persistence decision was rewritten. [Seam/body and golden parity receipt](2026-09-08-scenario-economics-data/w7-seam-parity.json): all **9 tracked golden paths unchanged**, explicit prompt goldens PASS, clock registry guard PASS. The production caller remains the existing entry point; no dead seam was added.

The W7 test now supplies **eight explicit CT clocks**: 11:00 and 17:17 through 17:23 on 2026-09-08. The unchanged old relative-window fixture, run through the seam, first failed at **17:18, 17:19, 17:20 and 17:21** with the same consumed=false assertion; [fixed-clock RED](2026-09-08-scenario-economics-data/w7-fixed-clock-red.txt). Corrected fixture: the acceptance bars occupy one completed hour, row birth is explicitly before that sequence, and an assertion checks the level remains inside the production activation window. Backdating, bars, re-arm checks and all three writer calls use the test's `now`; the target test/helper has no wall-clock read. The consumed-state, re-arm refusal and burned-retouch alert assertions are preserved. All eight cases and other W7/clock guards pass; [GREEN](2026-09-08-scenario-economics-data/w7-fixed-clock-green.txt), [goldens](2026-09-08-scenario-economics-data/w7-goldens.txt). No production activation window or range calculation changed.

This is the owner-authorized correction of the STOP, not a later-clock rerun used to conceal it. C5 still supersedes the master-plan arithmetic; C6 remains **DROPPED — UNESTABLISHED**. A new clean merged-head suite/build and fresh cutover gate must precede any RELEASE change or swap.


### Post-boot publication and override correction

The owner identified delayed remote publication; marker `1026263b` was pushed immediately from the same locked main tree and all five references were reverified at 18:20:54 CT. The temporary `/tmp` gate helper’s order-ID acceptance patch was reverted to the unchanged helper; it was never tracked as repository gate code. Encoding the owner override as a helper acceptance path was wrong. The gate remains independent, and the override is solely the owner’s chat instruction recorded in the [boot marker](2026-09-08-scenario-economics-cutover.md). The restored helper then passed all five legs without override. Stage A’s schema UNKNOWN means its half is **NOT LIVE**; its reproduced relative-path defect is recorded and untouched.
