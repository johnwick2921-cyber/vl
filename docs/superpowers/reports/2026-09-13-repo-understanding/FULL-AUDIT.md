> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# Full repository audit — editable evidence report

Generated from preserved Markdown sources by `tools/build-reports.py`. This is an editable assembled report; make durable corrections in the linked originals and rebuild. Baseline source scope is **1,049 files / 250,582 lines at 63968be62e44db2fb07a92883e02127b9064b0be**, across 28 primary reviews plus two bounded independent reviews. Source review is not runtime verification, deployment approval, or profitability evidence.

**Final verification is recorded only in [CHECKPOINT.md](CHECKPOINT.md#final-verification).** Packaging is not an additional audit or test pass. Current disposition includes committed ordered-execution and entry-receipt lifetime repairs; original numbered appendices remain historical.

Relative links have been rebased to the original artifacts; fragment links point to the original document to avoid duplicate-heading ambiguity. Code fences and original source files are preserved. Machine-readable baseline consistency evidence remains in [coverage-validation.json](coverage-validation.json) and [publication-validation.json](publication-validation.json); the checkpoint describes its limits.

## Contents

- [PACKAGING-UPDATES.md](#section-1)
- [CTO-TRADING-LOGIC.md](#section-2)
- [REPAIR-STATUS.md](#section-3)
- [CORE-TRACE.md](#section-4)
- [CHECKPOINT.md](#section-5)
- [reviews/01/report.md](#section-6)
- [reviews/02/report.md](#section-7)
- [reviews/03/report.md](#section-8)
- [reviews/04/report.md](#section-9)
- [reviews/05/report.md](#section-10)
- [reviews/06/report.md](#section-11)
- [reviews/07/report.md](#section-12)
- [reviews/08/report.md](#section-13)
- [reviews/09/report.md](#section-14)
- [reviews/10/report.md](#section-15)
- [reviews/11/report.md](#section-16)
- [reviews/12/report.md](#section-17)
- [reviews/13/report.md](#section-18)
- [reviews/14/report.md](#section-19)
- [reviews/15/report.md](#section-20)
- [reviews/16/report.md](#section-21)
- [reviews/17/report.md](#section-22)
- [reviews/18/report.md](#section-23)
- [reviews/19/report.md](#section-24)
- [reviews/20/report.md](#section-25)
- [reviews/21/report.md](#section-26)
- [reviews/22/report.md](#section-27)
- [reviews/23/report.md](#section-28)
- [reviews/24/report.md](#section-29)
- [reviews/25/report.md](#section-30)
- [reviews/26/report.md](#section-31)
- [reviews/27/report.md](#section-32)
- [reviews/28/report.md](#section-33)
- [reviews/29/report.md](#section-34)
- [reviews/30/report.md](#section-35)

---

<a id="section-1"></a>

> Original: [PACKAGING-UPDATES.md](PACKAGING-UPDATES.md). Preserved source document; its own revision/checkpoint statements govern. Read current repair disposition before interpreting historical review findings.

## Report scope and repair candidate

This report joins a completed baseline source review with separately reviewed repairs. The baseline is `63968be62e44db2fb07a92883e02127b9064b0be`; 28 primary reviews cover 1,049 unique files / 250,582 lines, and reviews 29/30 examine bounded repairs at 99a06543 without adding primary coverage. The current repair candidate is `13882f01f72c313f4454bd29a309d5e31b6ec0cd`. It incorporates the required x/crypto and Go toolchain update after e333de41 passed its checks. The guide source stamp `40ed5d95e27857d13f639ceb4ff14f768396a2ac` identifies the backend source candidate, not the running binary. Its scope is not another full repository reading.

The summary documents now incorporate ordered execution (9b379c8c), missing first snapshots and pending excess exits (c20d0a82), observer replacement (d3e4638e), cancelled/rejected EXIT evidence (3f21431a), uncertain order display (dd11670b), positive rejected ENTRY evidence and stale-snapshot fencing (d7b70a90), and shared-server entry receipt lifetime across replacement (cd2978b7). These are committed repairs with bounded source/test evidence, not still-open implementation tasks. Their verification limits remain in the disposition table.

The 30 numbered reports remain historical originals. Their old findings are not silently rewritten; consult REPAIR-STATUS.md for the current disposition. Findings without a named repair retain their original reachability and evidence qualifications. [CHECKPOINT.md](CHECKPOINT.md#final-verification) is the only final verification ledger. Source review, document packaging and a passing offline suite are distinct from installed NT8 behavior, deployment approval, backup restoration and strategy profitability.

---

<a id="section-2"></a>

> Original: [CTO-TRADING-LOGIC.md](CTO-TRADING-LOGIC.md). Preserved source document; its own revision/checkpoint statements govern. Read current repair disposition before interpreting historical review findings.

## CTO assessment: does the system behave like a disciplined level trader?

Status: baseline source review complete; reviewed repair candidate `cd2978b77da54e2fceddfb19e1d3d148bd2bfb62`. Final verification is recorded only in [CHECKPOINT.md](CHECKPOINT.md#final-verification). Not a deployment or profitability approval.

Source references below are repository-relative. Strategy source was inspected at
`63968be62e44db2fb07a92883e02127b9064b0be`; repair-specific behavior is at
the repair commits named in [REPAIR-STATUS.md](REPAIR-STATUS.md). Named
function boundaries below avoid carrying baseline line numbers onto changed source. The repair branch is undeployed.
[A] means source inspected or an explicitly named test run; [B] means inference.
Owner-supplied backtest numbers are not independently recomputed here. This is a
repository engineering assessment, not a new literature review or evidence that
a discretionary trading doctrine is profitable.

### Executive judgment

The system has many useful controls, but their number does not establish a
coherent trading process. I cannot sign off on complete trading correctness.
All 30 scoped review reports are complete:28 source slices cover1,049 files and
250,582 lines; two cross-boundary reviews examined repair 99a06543. Subsequent
repairs receive separate focused review/tests. Final combined verification has its own ledger in CHECKPOINT.md. The number of reviews is not the number of simultaneous agents.

The core standard is consistency: the same setup identity, contract, account,
entry, stop, target, permission and lifecycle must survive from market data to
planner to risk admission to the broker and finally into performance records.
A convincing prompt cannot compensate for a different execution rule. A green
unit test cannot establish profitability. A successful socket write cannot
establish an accepted order, a fill, a cancellation or protection.

### 1. The job is to select a worthwhile trade, including selecting no trade

[B] “Find the best trade” must mean best among currently eligible candidates
under an explicit, validated objective. It cannot mean always produce an order,
choose the highest model confidence, or choose the largest displayed R multiple.
The repository review has not established that the ranking identifies the best
future outcome or a positive-expectancy subset.

A reviewable selection record needs: observation time; concrete contract and
account; session; frozen level identity; scenario/version; evidence for the
setup; entry condition; invalidation; opposing obstacles; composed geometry;
all admission results; and why the chosen candidate outranked alternatives.
This is an engineering acceptance requirement, not a new enabled trading rule.

[A] `trader/armed_executor.go` separates placement from authoring. Repairs
2dc94a19 and 456b38d4 exercise the actual cycle and loopback transport: missing
permission or current quality refusal cannot leave an inherited authorization
eligible. Placement consumes IDs admitted in the current cycle. Retirement
write failure prevents placement. These are enforcement tests, not merely
assertions about the model's prose.

### 2. Stop location: invalidate the identified setup, then report its exposure

[A] `ResolveEntryGeometryZone` in `trader/structural_geometry.go` resolves the entry zone from the scenario's
frozen identity and source provenance. It refuses missing or ambiguous identity.
This is stronger than attaching the stop to whatever unrelated level is nearest
when the order is submitted.

[A] For a valid reject-fade zone, `composeGeometry` in that file sets the long stop
below the lower zone edge by the configured buffer, rounding outward to the
contract tick. A short stop is above the upper edge. Invalid/missing buffer is a
refusal. The ATR fallback recorded when provenance is missing also returns
`no_provenance`: it is diagnostic geometry, not permission to trade.

[A] The production arm caller at `trader/armed_executor.go` (the `structuralFade` branch) selects this
structural branch for the reject play, excluding explicit exit legs. Other
plays retain their legacy stop construction. Therefore “ATR has been removed
from all stops” would be false.

Unresolved: a configured/calibrated buffer is not automatically a validated
noise allowance. Its sample must be causal, contract/session appropriate and
out of sample. An overshoot distribution conditioned only on levels that later
held excludes breakdowns; it cannot by itself establish total stop-out risk.
The research review records that limitation without claiming a new calibration.

### 3. The planner and execution contract now agree on reject-fade geometry

[A] Repair 710ea1c8 updates the actual planner prompt builder, legacy stop-floor
facts and feasibility warning consumer. Reject fades use frozen structural
geometry; the legacy ATR floor is explicitly scoped to other plays. The prompt
no longer suggests switching execution routes to escape a refused arm. Authored
reject stops are not evaluated as if they were the later composed trade. Focused
production builder/warning tests pass and the map golden remains unchanged.
This is a consistency repair; it does not validate the buffer or trading edge.

### 4. Target selection: the next level is precise, but not proved optimal

[A] `FirstGeometryTarget`, `trader/structural_geometry.go`, chooses the nearest
complete sourced zone strictly beyond the entry zone's profit-side edge. It does
not rank by source count, grade, timeframe, minimum reward distance or measured
quality. The long target uses the target zone's near lower edge; shorts use its
near upper edge, with conservative tick rounding.

This answers WHICH level the code selects. It does not establish that all zones
represent equally material obstacles. A very dense map can still leave little
room. Conversely, skipping a nearby real obstacle merely to display 2R does not
prove the farther target is reachable.

[B] Keep “first obstacle” and “trade objective” conceptually distinct in research.
If a different target policy is proposed, compare it prospectively with the
current nearest-zone rule using identical entry events and execution assumptions.
Do not silently reinterpret historical target results or install an untested
quality ranking. A refusal because there is insufficient room is a valid output;
persistent refusals are a diagnostic to investigate, not permission to weaken
the gate until trades appear.

### 5. Entry, stop and target must describe one trade

[A] `composeGeometry` freezes tick-normalized entry, stop and target before
admission. It requires positive directional risk/reward, known costs, positive
reward after those costs, and the configured gross reward/risk threshold. It
reports planned one-contract dollar loss, then leaves daily risk and other
admission checks to their existing gates. Quantity is not authorized by this
geometry function alone.

Important distinction: the current R gate uses gross gain divided by stop
 distance. Costs are checked separately; this is not a net-R gate and not an
expected-value calculation. A 2R objective can lose money if it is reached too
rarely. A lower-R trade can have positive expectation with a sufficiently high
realized win rate. Neither ratio establishes an edge by itself.

For owner-supplied average win W, average loss L, win probability p and constant
round-trip cost c, the simplified expectancy is pW-(1-p)L-c. With W=4.12,
L=24.57, p=0.899 and c=2 points, this is approximately -0.778 points/trade.
The break-even win rate is (L+c)/(W+L), approximately92.6%. Thus “no win rate
saves it” is too absolute for those averaged outcomes; the supplied observed
win rate does not save it. This arithmetic is not a fresh backtest and does not
explain all fill-assumption differences.

### 6. Daily loss is an owner control; it is not a stop-placement algorithm

[A] The owner clarified DAILY loss, not a mandatory additional per-trade cap.
The structural core explicitly retains that distinction at
`composeGeometry` in `trader/structural_geometry.go`. Do not restore the removed cap or silently
set a new value. Contract value converts distance to planned exposure; it does
not decide where the setup is invalidated.

[A] `bootRiskFacts` in `trader/session_risk.go` reads the guardrails master and
individual daily-loss enable switch for boot reporting. This is a reporting
boundary, not evidence by itself that admission enforced the configured amount.
`entryGateForArm` and `entryGateForDecision` supply `DailyForceFlatReason` to
`EntryGate`; its daily-force-flat leg refuses when that resolver reports a trip.
`SessionRiskBootLine` labels a configured but disabled limit decorative. A displayed dollar value alone is
not proof of enforcement. Earlier observed settings are historical snapshots;
this assessment did not read or change current settings.

Required verification: define and test the precise daily boundary, realized and
unrealized components, commissions, account/trader scope, loss-trigger response,
resting-entry cancellation, and protection/flattening behavior. Whether to reserve
remaining daily budget for a prospective stop is a separate explicit owner
policy, not an assumption to introduce during a repair.

### 7. The broker lifecycle matters as much as the setup

[A] Committed repairs now distinguish authorization, placement pending, working,
fill, cancel pending and confirmed cancellation. Tests reproduce and repair:
startup checks occurring after autostart; entry paths escaping the boot latch;
same-version terminal arms being reauthorized; cancellation send being called
settlement; pre-request empty snapshots being used as cancellation evidence;
and previous-process retries exhausting a new process's retry allowance.

[A] A production placement-loop test also reproduced a refused stop entry
cancelling other scenarios as if it had placed. Commit 7b2eb894 makes successful
pre-send registration the commitment boundary. A separate test retains that
commitment after an ambiguous send error. Repair b63747ea covers the analogous limit path through the actual TCP adapter
and registration callback. Both commit on durable registration; neither calls
an ambiguous send a fill or confirmed cancellation.

All these are offline fixture results. They do not prove the installed AddOn
accepted an order or that every asynchronous interleaving is correct.

### 8. NT8 protection and account routing: repaired source, bounded evidence

[A] C# source repairs 9140f6c9/f1b7cc10 resolve explicit accounts without fallback,
select the actual held expiry, refuse ambiguous bare roots, and preserve SIM,
connection and session-account restrictions. Entry cancellation retains deferred
protection until terminal broker evidence. Submit ambiguity retains bracket
identity; cumulative fills amend one pair. Actual leg quantities confirm an
amendment, and synchronous terminal receipts survive submission reentrancy.

[A] Independent review reproduced two gaps in the first repair, then verified
both fixes. All33 extracted-production-method harness assertions pass and all
five AddOn sources compile against installed NT8 assemblies. These checks do
not recreate NT8 scheduling, the broker's OCO implementation or real fills.
The source AddOn has not been copied, compiled in the live NT8 installation or
restarted. No runtime protection claim follows from the temporary DLL.

[A/report] Additional cross-boundary tests reproduced completed partial exits
being recorded as whole-position closes and positive cumulative ENTRY fills on
terminal cancellation being omitted. Repair a982cc74 now records actual exit
quantity, receipt identity, fill and residual cost basis atomically, and handles
cumulative entry growth without overwriting partial-exit accounting. Repair 3f21431a also emits valid positive cumulative EXIT evidence on terminal cancellation/rejection; the report records 89 extracted-method assertions and five-source reference compilation. This is distinct from terminal ENTRY materialization. Current exit wire also lacks commission data;
zero additional recorded fee is unreported commission, not measured zero cost.

#### Receive order, positive evidence and replacement lifetime

[A/source and reported offline tests] Repair 9b379c8c installs one exact account/symbol execution owner at successful trader construction. OrderUpdate, Fill and PositionClose are applied from TCP readLoop before advisory fanout. Internal handled flags prevent older consumers from reapplying events. Registration replacement and cleanup compare owner identity. Tests exercise the actual TCP path, both cumulative-entry/exit orders, raw and advisory replay, foreign accounts, cache resurrection and reentrant outbound progress.

For entry1@100 → cumulative entry2@105 → exit1@120, the expected residual is one contract at105 and realized MNQ P&L30USD. For entry1@100 → exit1@120 → cumulative entry2@105, it is one contract at110 with realized P&L40USD. Later cumulative growth of the same immutable order is observed exposure, not permission for another entry. Earlier exits and realized P&L are retained; corrected final P&L becomes unresolved while exposure continues. These fixtures establish their accounting cases, not exchange execution chronology.

Repair c20d0a82 retains a valid exit exceeding currently materialized entry quantity as pending rather than discarding it, and refuses absent/stale first position snapshots. Follow-up d7b70a90 preserves positive entry exposure even on rejection and fences a snapshot received before positive execution evidence. A rejection alarm cannot erase an actual partial fill.

Repair cd2978b7 moves the positive-entry receipt and cumulative deduplication state into the shared TCPServer, keyed by canonical symbol/account. Position data, receipt time and entry watermark are read together under one mutex. Replacing an adapter therefore neither forgets the prior entry nor lets an old duplicate renew its fence against a newer snapshot. The two replacement directions were reproduced before repair and covered by focused race tests; this summary reads their report rather than claiming another full source review.

The guarantee is receive order for an installed owner. There is no durable inbound journal or reconstruction of events received before ownership existed. Synchronous storage callbacks can backpressure transport and must not wait for broker replies. Failures still require later evidence/replay; committed database changes cannot guarantee subsequent process-local hooks across a crash. Same-order continuation does not retroactively repair all terminal analytics or excursion rows. These substantive limits remain after the named defects are fixed.

### 9. One contract and management rules must remain executable

One contract cannot be partially reduced. “Take some off and leave a runner” is
not an executable MNQ instruction for this account size. Every planned response
must map to hold, full exit, or another expressly supported action. Do not add
contracts to imitate a book's scaling example.

The historical task description says no stop movement, no re-entry and a session
cutoff. Those premises must be reconciled with current implemented stop repair,
management, same-version retirement and new-version authorization rules. A
protective-stop restoration is different from discretionary tightening, but the
UI and logs must identify which occurred. New plan versions must not become an
accidental loophole around the owner's intended re-entry policy.

Delayed flatten repair 94e08cf0 checks immutable position/entry lineage, invalidates
timers on Stop and preserves protection after close refusal. A broker-side atomic
position fence is still absent: a stale local row cannot prove that no unseen
replacement exists. Broker observers intentionally outlive ordinary Stop while
positions may remain. Repair d3e4638e retires a replaced reconciliation worker after its old close channel drains; an already executing pass can finish. Ordinary Stop still preserves protection observation. This bounded replacement handoff does not certify every shutdown path.

### 10. What establishes success, and the order of work

1. Close account-routing, protection, asynchronous settlement and stale
   authorization defects. Preserve SIM restrictions and real owner settings.
2. Align planner, geometry, admission, execution and displayed explanations.
   Every refusal must name the actual reason and retire incompatible permissions.
3. Baseline frontend/runtime-source and independent cross-boundary reviews are
   complete. Run the full suite on the final combined repaired source, relevant
   race tests, frontend verification and controlled broker lifecycle tests. Mark external/runtime
   checks unavailable until actually observed.
4. Validate the strategy separately: causal detector inputs and frozen levels;
   realistic limit fills, gaps and ambiguity; all costs; one-account chronological
   order/position constraints; actual session exits; and untouched evaluation data.
5. Compare policies with all candidates and refusals retained, not only executed
   winners. Report net expectancy, uncertainty with session dependence, tail loss,
   drawdown from the initial equity baseline, exposure and execution sensitivity.
   A hold rate alone is not trade win rate or expectancy.
6. If credible out-of-sample results remain nonpositive across realistic fills,
   do not label more code or a prettier R ratio an edge. Keep the strategy
   unapproved for promotion while recording the failed hypothesis honestly.

No precise buffer, optimum number of trades, universal target rule or profitable
regime classifier has been established by this source audit. The engineering job
is to make those hypotheses measurable and execution faithful. The trading job
is then to demonstrate that the selected opportunities pay after losses and costs.

### Evidence and completion limits

See README.md, CHECKPOINT.md, reviews/01 through reviews/30, coverage-validation.json
and [REPAIR-STATUS.md](REPAIR-STATUS.md), which separates repaired baseline
findings from concrete remaining source/runtime limitations. Frontend 28a6f32e
passed451 tests/build and was integrated as 6c4092bf. Full Go/build/focused race checks passed at 99a06543, before later changes. These are historical checkpoints; the single final verification ledger is in CHECKPOINT.md.
No deployment, owner-setting change, live database write or real order occurred.
Historical runtime snapshots from the earlier daily-loss dispatch are not new
observations. The earlier usage-blocked report is archived under interim/.

The final report must distinguish fixed/reproduced defects, static concerns,
intentional policies, dormant legacy paths and checks requiring real runtime.
Neither complete source coverage nor a green suite demonstrates profitable
trade selection. Owner-supplied backtest statistics remain supplied context,
not an independently repeated experiment in this engineering dispatch.

---

<a id="section-3"></a>

> Original: [REPAIR-STATUS.md](REPAIR-STATUS.md). Preserved source document; its own revision/checkpoint statements govern. Read current repair disposition before interpreting historical review findings.

## Repair disposition and publication status

This is the bridge between the **baseline review** and **later repairs**. It prevents a fixed baseline finding from being presented as still current, or a focused repair from being presented as runtime proof. Candidate source: `13882f01f72c313f4454bd29a309d5e31b6ec0cd` (security dependency follow-up 40ed5d95, build alignment d174ca95). Final verification appears only in [CHECKPOINT.md](CHECKPOINT.md#final-verification).

### How to read the evidence

- **Baseline source review complete:**30 reports/120 standard artifacts are indexed.28 primary slices account for1,049 unique assigned files/250,582 lines at 63968be. Reviews 29/30 examine bounded repairs at 99a06543 and add no primary files. Publication-validation.json independently finds no missing artifacts/index links, duplicate assignments, ledger hash/range gaps or errors in the supplied named-function validators. This is an artifact consistency check, not a second reading of every function.
- **Repair committed:** source was changed on a named repair branch. A stated focused test covers only its actual fixture/call site. Neither branch-green nor an extracted C# harness is final combined or live NT8 evidence.
- **Runtime unverified:** no new broker order, account/settings mutation, live trade-row inspection, deployment/restart, or profitability experiment was authorized or performed by this source audit. Historical receipts remain dated historical evidence.

### Disposition table

| Boundary / review evidence | Disposition and commit scope | Verification and remaining limit |
| --- | --- | --- |
| Ownership, boot entry latch, terminal arms, causal cancellation, cancel retry identity ([01](reviews/01/report.md), [20](reviews/20/report.md), [29](reviews/29/report.md)) | Repaired on control branch; consolidated Go checkpoint 99a06543 | Full Go suite/build and selected race checks at 99a06543. Later integration is separate. Not all-package race or broker runtime proof. |
| Structural planner contract, current-cycle admission, missing permission retirement ([08](reviews/08/report.md), [11](reviews/11/report.md), [30](reviews/30/report.md)) | Prompt 710ea1c8; admission 2dc94a19/456b38d4 and associated control repairs | Production builder/admission fixtures. No buffer calibration, ranking quality or expectancy validation. |
| Stop/limit registration ambiguity ([20](reviews/20/report.md), [29](reviews/29/report.md), [30](reviews/30/report.md)) | Stop 7b2eb894; limit b63747ea | Both commit admission on durable registration. Actual adapter/loop tests; no claim that ambiguous transmission is accepted/filled. The limit gap recorded at 99a06543 is subsequently repaired. |
| C# explicit account/expiry, entry cancel/protection and bracket amendments ([14](reviews/14/report.md), [29](reviews/29/report.md)) | Original 9140f6c9/f1b7cc10 integrated as e8d2243f/cc766e1c |33 extracted production-method assertions and five-source compilation against installed references reported. Live AddOn not installed/restarted; NT8 scheduling/OCO remains unverified. |
| Delayed flatten and Stop observer lifetime ([22](reviews/22/report.md), [23](reviews/23/report.md)) |94e08cf0 binds fallback to position lineage, invalidates timers on Stop, preserves protection on close refusal | Focused lifecycle/race checks. No broker-side atomic expected-position fence; replacement handoff is repaired by d3e4638e: the old close subscription drains before its periodic worker retires. Ordinary Stop retains protection observers; full shutdown is not certified. |
| Completed partial exits and cumulative **entry** terminal receipts | a982cc74 adds atomic receipt/fill/residual accounting and cumulative entry growth | Focused store/adapter/trader/race and C# reference/harness evidence reported. 3f21431a adds positive terminal cancelled/rejected **exit** receipts, with 89 extracted assertions and reference compilation reported; process-local hooks can be lost after commit/crash; missing replay/delivery and ambiguous multi-row attribution remain limits. |
| TCP execution ordering / cumulative continuation | 9b379c8c | Actual receive-order owner applies entry/order/exit before advisory fanout. TCP fixtures distinguish both interleavings, residual basis, duplicates, foreign accounts and outbound reentrancy. No exchange chronology reconstruction, durable inbound journal or retroactive terminal analytics repair. |
| First snapshot and exit-before-entry-growth | c20d0a82 | Missing/stale first account state refuses admission; currently excessive valid exit remains pending. Later replay/delivery still required. |
| Positive rejected entry / older flat snapshot | d7b70a90 | Positive execution survives rejection; older account snapshots cannot override newer entry evidence. Zero/legacy rejection remains distinct. Reported focused tests; final suite in CHECKPOINT.md. |
| Entry receipt across adapter replacement | cd2978b7 | Shared TCPServer account/symbol receipt and cumulative dedup survive replacement; position and receipt read atomically. Actual constructor/replacement regressions and targeted race checks reported. Memory lasts for server lifetime; no durable journal is introduced. |
| Agent HTTP identity / per-request model choice ([03](reviews/03/report.md), [04](reviews/04/report.md), [26](reviews/26/report.md)) | Control ownership fixes and c8323d09 request-local clients | Synthetic authenticated ownership/model tests; selected race checks. Not a claim that every agent trade-confirmation/background lifecycle is fully isolated. |
| Browser chat, SSE, local history and SWR cache ([25](reviews/25/report.md), [26](reviews/26/report.md)) | Frontend ed85a80a; integrated source 03090352. Data-truth follow-up 28a6f32e uses provider-local dashboard mutate and opaque cache key | Production stream/cache fixtures, arbitrary chunk splits, late-user/same-user completions. Browser abort is not backend tool cancellation. Unowned guest history retained separately, not assigned to later users. |
| Indexed plan edits ([24](reviews/24/report.md), [30](reviews/30/report.md)) | Control API expected-revision checks plus frontend opening-snapshot save/delete tuple in ed85a80a | API temporary-store conflicts and frontend draft/poll fixture. Historical Ask/Q&A proposals lack equivalent authored-version identity; separate JSON Patch test operations are not the same guarantee. |
| Corrected P&L UI and chart ownership ([26](reviews/26/report.md), [28](reviews/28/report.md), [30](reviews/30/report.md)) | Frontend 28a6f32e integrated as 6c4092bf | Missing/nonfinite corrections excluded and counted; server aggregate counts separated from loaded filtered counts. Late chart/history results and SVP toggle races tested. dd11670b repairs the empty-on-error wrapper with visibly UNKNOWN/stale snapshots and selected-account request invalidation. The endpoint remains trader-bound; backend selected-account completeness and forming-candle marker association remain open. |
| Modal drafts, NT edit defaults, config/errors, market selector and unsafe FAQ ([25](reviews/25/report.md), [28](reviews/28/report.md)) | Frontend ed85a80a / integration 03090352 | Core targeted fixtures and type/build checks. Not every wallet/model form path reproduced. Bulk model replay/extra-instance knobs, submit busy lifecycle remain open. dd11670b makes existing binding names read-only; backend account migration remains separate. |
| Breaker zero/default display ([27](reviews/27/report.md)) | Frontend 28a6f32e removes false Off in **RiskControlEditor**, not DayPlanEditor |0 means server threshold(default 8 unless overridden); env 0 can disable. No daily-loss policy, saved values, quantities or mandatory per-trade cap changed. |
| Swing wick provenance / aggregate volume / weekly daily-input reader ([10](reviews/10/report.md), [12](reviews/12/report.md)) |05a1775c and df4af389 | Synthetic detector/aggregate and actual weekly-reader regressions. Generic epoch aggregation elsewhere and daily-data limits on intraday gap timing remain open. Corrected input measurement, not proof of improved trading returns or recalibrated distributions. |
| Five dependency advisories | gnark-crypto0.19.2 in ffbcf3e4; four npm transitives in e26f63a8, integrated f5409132 | Targeted compatible versions; npm audit 0and web suite/build at private updated install. Advisory affected versions confirmed by paginated GitHub read. Default-branch alert closure awaits merge/scanning; exploitation/reachability not established. |
| Maps/guide discrepancies ([24](reviews/24/report.md), [30](reviews/30/report.md)) | Existing stale guide paragraphs corrected 6058d9fe; actual-fill guide 939e21db; current candidate uses matched execution-evidence source identity; final publication identity belongs to CHECKPOINT.md | Historical UA/CGC stay historical. Narrow map line guards do not verify all prose. Source-marker reference compilation is not installed-runtime identity verification. |
| Backup, runtime composition and strategy profitability ([17](reviews/17/report.md), [18](reviews/18/report.md), [19](reviews/19/report.md), [30](reviews/30/report.md)) | **Not certified by source review** | Prior receipt is not a restore rehearsal. Partial backup cleanup/weekly copy risks remain reported. First live composition/refusal, NT8 lifecycle and causal out-of-sample net expectancy remain unverified. |

The table tracks principal reviewed repair boundaries, not a declaration that every finding in all 30 reports was repaired. Findings without an explicit repair disposition retain their baseline status and reachability qualifications. In particular, crypto broker/client contracts ([06](reviews/06/report.md), [07](reviews/07/report.md), [13](reviews/13/report.md)), persistence/query concerns ([15](reviews/15/report.md), [16](reviews/16/report.md)), and operations/tooling concerns ([19](reviews/19/report.md)) were reviewed but not globally rewritten. They must not disappear behind the phrase “source review complete.”

### Verified checkpoints, not a manufactured final green

| Checkpoint | Evidence available | Does not establish |
| --- | --- | --- |
| Go 99a06543 | Full go test ./..., go build ./..., selected focused race tests | Later merged C#/Go/frontend correctness or every race |
| Frontend 28a6f32e |71 Vitest files/451 tests; TypeScript/Vite build; private dependency tree, npm audit 0 after compatible updates | Final control-branch suite, real browser E2E, broker execution or security completeness |
| C# lifecycle source pair |33 extracted-method harness assertions; five AddOn sources compile with NT8 references | Installed AddOn version, real callback timing, account/broker OCO or live fills |
| Coverage publication check |30 indexed reviews,120 artifacts,1,049 unique primary files,250,582 lines; errors[] | Every tracked test/document read, exact function correctness, runtime safety or edge |

For final candidate results and publication stamps, use the single ledger in [CHECKPOINT.md](CHECKPOINT.md#final-verification). Earlier checkpoints above retain their original scope.

### Open work that must survive publication

**Execution and accounting:** no broker-side atomic expected-position fence; receive order does not establish exchange chronology; events queued before an execution owner existed are not retroactively ordered. No durable inbound journal or crash-atomic process-local hook delivery exists. Storage failures and pending receipts require later evidence/replay, and synchronous callbacks can backpressure TCP. Ambiguous same-account/root/side multi-row attribution refuses. Same-order continuation does not retroactively rebuild terminal analytics/excursions. Installed NT8 callback/OCO, reconnect and controlled shutdown behavior remain unverified. These are limits of the repaired design, not claims that terminal EXIT, positive rejected ENTRY, replacement receipt lifetime or observer handoff are still unimplemented.

**Active UI/data truth:** backend account-qualified history/chart/orders completeness; loaded-window versus complete aggregate distinctions; forming-candle marker matching; bulk model payload replay and missing extra-model thinking knobs; submit request lifecycle; DayPlanEditor polled-draft reset/default/inheritance/translation issues. Binding migration remains separate; an existing account name is now read-only. The breaker display, corrected-PNL fallback, late-response identity and open-order error-to-empty defects have named repairs above.

**Research and historical analysis:** many scripts are archived exploratory tools rather than production admission. Flaws there do not by themselves prove the live measurements are wrong. Equally, an in-sample held-level overshoot distribution does not establish unconditional stop risk, trade win rate or profitable expectancy. Retain chronological/account constraints, all candidates and refusals, fill ambiguity, costs and untouched evaluation samples for any later strategy experiment.

**Dormant paths:** legacy crypto/CSV/competition/chart/reset-password prose or interfaces are not automatically active execution or reachable exploits. Preserve the reachability qualifications in individual reports. The current NT8 SIM source path and its active issues take priority.

### Report provenance

The original 30 reviews retain baseline and bounded cross-review findings; this table is their current principal disposition, not a declaration that every concern was repaired. Earlier usage-blocked snapshots remain under interim/. Selected function anchors in CORE-TRACE.md are regenerated at the candidate, while historical line coordinates remain in their original reports.

Detailed source/test receipts are included alongside this audit and on the control repair branch in `docs/superpowers/reports/2026-09-13-repository-repairs/README.md`, `2026-09-13-ordered-execution-repair.md`, `2026-09-13-nt8-entry-receipt-replacement.md`, and the NT8 partial/terminal lifecycle reports; frontend receipts remain in `2026-09-13-web-state-repairs/README.md`. Some chronological repair receipts describe work as pending before later follow-ups; this candidate disposition supersedes those named historical states. Packaging reads those receipts and does not invent an independent rerun.

### Final security follow-up

Dependency repair 40ed5d95 selects x/crypto 0.56.0 and only required graph updates, with Go 1.26.8. Build/CI alignment d174ca95 removes stale Go 1.21 setup pins and updates the Docker Go family. This resolves three version-fixable SSH advisories. Default Linux/amd64 production and test closures import neither SSH nor OpenPGP; govulncheck reports zero reachable or imported-package vulnerabilities and one module-only advisory, GO-2026-5932, with no fixed version. See [the primary-source advisory report](../2026-09-13-crypto-security-repair.md). This is not a claim that every platform/build tag or dependency is vulnerability-free.

---

<a id="section-4"></a>

> Original: [CORE-TRACE.md](CORE-TRACE.md). Preserved source document; its own revision/checkpoint statements govern. Read current repair disposition before interpreting historical review findings.

## Core trading source trace

Source snapshot: `13882f01f72c313f4454bd29a309d5e31b6ec0cd`. Each link names an exact committed declaration. This is a selected source locator, not evidence that every branch ran. Detailed baseline function notes and connections are in the 30 review folders. Execution-order and integration tests must be read beside these source links.

| Boundary | Exact source | Responsibility / transfer limit |
| --- | --- | --- |
| Transport | [readLoop](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/provider/ninjatrader/tcp_server.go#L1718) | Decodes broker frames; execution processing order must be verified separately. |
| Ordered execution owner | [RegisterOrderedExecutionsFor](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/provider/ninjatrader/ordered_execution.go#L17) | Exact account/symbol owner; preserves receive order before type fanout. |
| Ordered entry delivery | [dispatchOrderedOrder](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/provider/ninjatrader/ordered_execution.go#L37) | Applies received entry evidence before advisory consumers can reorder it. |
| Ordered exit delivery | [dispatchOrderedClose](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/provider/ninjatrader/ordered_execution.go#L54) | Applies received close evidence through the same serialized owner. |
| Shared entry receipt | [NoteEntryExecution](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/provider/ninjatrader/entry_receipt.go#L17) | Account/symbol cumulative evidence survives adapter replacement for shared server lifetime. |
| Atomic position evidence | [PositionsForExecutionReceipt](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/provider/ninjatrader/entry_receipt.go#L42) | Reads position snapshot and entry receipt under the same mutex; no guessed flat fallback. |
| Owner installation | [InstallOrderedExecutions](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/ninjatrader/ordered_execution.go#L12) | Installs observation at successful construction; ordinary Stop retains it. |
| Fill cache | [handleFill](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/ninjatrader/tcp_trader.go#L193) | Preserves actual exposure and refuses duplicate fully exited entry cache replay. |
| Market data | [GetWithTimeframes](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/market/data.go#L190) | Builds requested market context; futures provider path differs from legacy crypto. |
| Canonical symbol | [Normalize](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/market/data.go#L670) | Preserves the CME normalization boundary. |
| Level evidence | [AssembleResearchLevels](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/kernel/levels_assemble.go#L212) | Assembles raw, pool and seated candidates; heuristic scores are not probabilities. |
| Weekly evidence | [weeklyDailyBars](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/auto_trader_weekly.go#L138) | Preserves daily input for CME-week aggregation. |
| Weekly facts | [CompletedWeekCandles](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/kernel/weekly_bias.go#L93) | Groups observations into completed Monday-governed weeks. |
| Planner invocation | [runPlannerReadCoreWithFacts](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/auto_trader_planner.go#L1121) | Machine facts and model response enter planner persistence/validation. |
| Frozen setup identity | [ResolveEntryGeometryZone](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/structural_geometry.go#L30) | Rejects missing or ambiguous frozen source identity. |
| First structural obstacle | [FirstGeometryTarget](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/structural_geometry.go#L78) | Chooses nearest complete sourced zone beyond the entry zone. |
| Geometry | [ComposeLevelFadeGeometry](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/structural_geometry.go#L106) | Production wrapper freezes structural stop/target before admission. |
| Geometry arithmetic | [composeGeometry](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/structural_geometry.go#L123) | Zone-edge buffer, outward rounding, costs and gross-R refusal; no ranking proof. |
| Arm orchestration | [maybeManageArmedOrdersAt](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/armed_executor.go#L199) | Current-cycle authorization and gate results precede placement. |
| Placement | [runArmedPlacementAt](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/armed_executor.go#L1184) | Consumes currently eligible arm identities and broker/account evidence. |
| One-contract guard | [oneContractGuard](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/one_contract.go#L159) | Account exposure and entry-order admission boundary. |
| Session controls | [sessionRiskGateAt](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/session_risk.go#L120) | Session breaker/band verdict; does not alone establish daily-loss implementation. |
| Daily reporting | [bootRiskFacts](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/session_risk.go#L260) | Reporting facts only; do not cite as executable daily-loss gate. |
| Decision risk | [GetFullDecisionWithStrategy](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/kernel/engine_analysis.go#L57) | Strategy decision/control pipeline; distinct from resting-arm placement. |
| Position admission | [ntHeldPosition](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/auto_trader_orders.go#L377) | Broker position errors must remain unknown instead of flat. |
| Decision long entry | [executeOpenLongWithRecord](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/auto_trader_orders.go#L464) | Actual decision entry call site; admission failure must prevent wire submission. |
| Decision short entry | [executeOpenShortWithRecord](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/auto_trader_orders.go#L612) | Short counterpart requires the same ownership and exposure discipline. |
| Resting limit | [PlaceLimitEntry](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/ninjatrader/tcp_trader.go#L511) | Registers identity before transmission; transmission is not broker acceptance. |
| Stop entry | [PlaceStopEntry](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/ninjatrader/tcp_trader.go#L575) | Kind-specific stop entry adapter; distinct from protective stop placement. |
| Cumulative entry | [onArmedOrderUpdate](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/armed_executor.go#L1952) | Consumes actual entry state/quantity including positive terminal cancellations. |
| Entry accounting | [materializeArmedEntry](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/armed_executor.go#L2071) | Preserves cumulative entry quantity/notional and residual position accounting. |
| Broker exit | [recordClose](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/ninjatrader/close_sync.go#L88) | Builds actual exit receipt with account and broker-order identity. |
| Atomic exit | [ApplyNT8Exit](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/store/nt8_exit_receipt.go#L44) | Receipt, actual fill and residual/P&L update share one transaction. |
| Reconciliation | [reconcilePositions](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/ninjatrader/reconcile.go#L118) | Reconciles observations; a database row is not broker-flat proof. |
| Positions truth | [GetPositions](https://github.com/johnwick2921-cyber/vl/blob/13882f01f72c313f4454bd29a309d5e31b6ec0cd/trader/ninjatrader/tcp_trader.go#L986) | Selected bound-account position snapshot and freshness admission. |

Ordered execution and shared receipt lifetime are repaired at the source snapshot above; final verification is recorded in CHECKPOINT.md. Transport receipt, broker acceptance, execution, position reconciliation and permission to enter are separate facts. The root report records their verification status.

---

<a id="section-5"></a>

> Original: [CHECKPOINT.md](CHECKPOINT.md). Preserved source document; its own revision/checkpoint statements govern. Read current repair disposition before interpreting historical review findings.

## Audit and repair checkpoint

### Completed scope

All 30 scoped assignments are reported. At baseline 63968be62e44db2fb07a92883e02127b9064b0be, 28 primary reviews cover 1,049 unique first-party files / 250,582 lines. Two bounded independent reviews examine repair 99a06543 and add no primary files. Coverage and publication validators establish artifact/hash/range/function-note consistency, not a second semantic reading or runtime correctness. The 30 reports and their 120 standard artifacts are preserved.

Current candidate: `13882f01f72c313f4454bd29a309d5e31b6ec0cd` on `fix/repo-audit-control-boundaries-20260913`. The disposition and CTO assessment include the ordered-execution and positive-entry/replacement follow-ups through this revision. Those implementation tasks are committed; they are not awaiting an unspecified future repair. CORE-TRACE.md pins 35 declarations to frozen verification tree `13882f01f72c313f4454bd29a309d5e31b6ec0cd`.

Historical checkpoints remain revision-scoped: Go full suite/build and selected race checks at 99a06543; frontend 71 files/451 tests plus build at 28a6f32e; later chart follow-up 9 focused tests/build; final-marker C# reference compilation and 89 extracted assertions. No earlier green result is relabelled as a pass at the final candidate.

### Substantive remaining limits

Receive-order processing does not reconstruct exchange chronology or pre-owner queued events. No durable inbound journal or crash-atomic process-local hook delivery exists; storage failures still need later evidence/replay. Synchronous callbacks backpressure TCP; continuing cumulative exposure does not rebuild all already-emitted terminal analytics. Broker-side atomic expected-position fencing remains absent. Ambiguous multiple-row attribution refuses. Replacement handoff is repaired, while installed NT8 scheduling/OCO, reconnect and final shutdown still need controlled verification.

UI limits include backend selected-account completeness in history/chart/orders, forming-candle marker association, bulk model payload replay/extra-model thinking knobs, submit lifecycle and DayPlanEditor draft/default/inheritance/translation issues. Existing account names are now read-only; backend binding migration is separate. Corrected-PNL fallback, stale response identity and open-order error-to-empty have named repairs and are not wholly open.

The audit did not establish production database restoration, a first live structural composition/refusal, calibrated stop buffers, target optimality or causal out-of-sample net expectancy. Historical exploratory research is qualified separately from current measurement dependencies. Main runtime, owner settings, accounts and live records were not changed by this source audit; SIM and the owner's daily-loss policy remain intact.

### Final verification

**Source checks complete at `13882f01f72c313f4454bd29a309d5e31b6ec0cd`.** The full security-updated Go suite and race run were first verified at b45b3efd. The only subsequent change, 13882f01, updates the independently reviewed go.mod protected hash; all mutation assertions remain active. Full Go tests/build and the frontend suite were rerun on that clean commit. Every runner receipt records exact command, commit before/after, clean state, exit code, duration and log SHA-256. Historical failed runs are retained and never relabelled as passes.

| Check | Verified result and scope |
| --- | --- |
| Full Go suite | PASS: `go test ./...` at b45b3efd (314.242s), then at13882f01 (3.759s, cached package results). [Final receipt](verification/go-full-release.json). |
| Race detector | PASS: full store, provider/ninjatrader, trader/ninjatrader and trader packages, 322.822s at b45b3efd. Subsequent diff is one frontend hash string. [Receipt](verification/go-race-final.json). This is not an all-repository race run. |
| Backend build | PASS: `go build ./...` with explicit worktree Git environment and VCS metadata preserved, Go1.26.8. [Receipt](verification/go-build-release.json). |
| Frontend suite | PASS:71 files /454 tests at13882f01. The preceding run correctly caught the changed go.mod hash (453 passed,1 failed); the reviewed hash update resolved it without weakening the guard. [Receipt](verification/web-tests-release.json). |
| Frontend production build | PASS at b45b3efd; the subsequent hash-only test-data edit does not enter the product bundle. Existing large-chunk warning remains. [Receipt](verification/web-build-final.json). |
| Offline futures smokes | PASS: prompt and synthetic TCP roundtrip at13882f01. [Prompt](verification/smoke-prompt-release.json), [roundtrip](verification/smoke-roundtrip-release.json). No live broker order. |
| C# | Five files compile against installed NT8 references;89 extracted-production-method assertions pass. Receipts at e333de41; those C# sources are unchanged in the final candidate. [Compile](verification/csharp-reference-compile.json), [harness](verification/csharp-harness.json). Not installed-NT8 execution. |
| Dependency security | npm audit:0 findings in selected lockfile. govulncheck:0 reachable and0 imported-package vulnerabilities in default Linux/amd64 app/test closure; GO-2026-5932 remains module-only, no fixed version. [Advisory report](../2026-09-13-crypto-security-repair.md). Remote Trivy on b45b3efd cleared its previous version-fixable alerts; other remote jobs are separately reported and not universally declared green. |
| Review consistency |30 indexed reviews /120 standard artifacts; hash/range/named-function-note and publication checks pass.35 selected core declarations are pinned to the final source commit. These checks prove artifact consistency, not runtime semantics. |
| Source backup | Standalone full-history bundle: **78,752,411 bytes**, SHA-256 `6e29a3f7d09769fbffcbfc96e9f29e4defb07cbcb4e7059a957ebc79fd030265`. Fresh clone, full fsck, exact-revision checkout and clean-tree check passed. [Restore record](verification/source-backup.json). |
| Runtime / installed NT8 / profitability | Not verified or changed by this audit. No deployment, owner-account/settings changes, live database mutation or new profitability experiment. |

The source bundle records repair13882f01 and docs9988cd55; final publication Markdown and verification receipts are additionally present in the delivery ZIP. PDF exports and the ZIP receive separate byte/hash/integrity receipts outside Git, avoiding circular self-hashes. The source restore is **not** a production database restore. Earlier progress bundles and usage-blocked reports are historical.

Guide stamp40ed5d95 identifies the backend source candidate including the patched module graph; it does not identify the running binary. AddOn candidate ID remains `2026-09-13-execution-evidence`. A future deployment must follow the existing release procedure and coordinate Go/C# installation; this report grants no deployment approval.

The final report distinguishes repaired and reproduced defects from unresolved source/runtime limits. All baseline findings without a named repair retain their original qualifications. A later documentation-only merge must retain this source revision provenance and must not be described as an additional source audit.

---

<a id="section-6"></a>

> Original: [reviews/01/report.md](reviews/01/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 01 — API control surfaces and manager boundaries

Source pin: `63968be62e44db2fb07a92883e02127b9064b0be`. Isolated worktree `/tmp/vl-understanding-market-20260913` verified clean and at this HEAD before reading. Despite its directory name, assignment 1 is `api-auth-and-manager`: **24 assigned files, 6,649 lines, 144 named functions/methods**, all manually read in full. Seven anonymous callbacks are covered under their parents. `functions.json` includes every named declaration with line boundaries, semantics and syntactic call expressions; those expressions are not asserted as type-resolved graph edges.

This is a source understanding audit, not a live incident investigation. **[A]** means source directly read during this run; **[B]** means an inference explicitly bounded by prerequisites. No service calls, live database reads/writes, orders, runtime environment reads, source mutations or tests were executed. Findings below are static defects/risks; none is described as an observed attack or actual trade failure. The assigned pin is not independently certified as the running binary. Root owns reproduction and repairs.

### Rules and freshness

Read root AGENTS.md, tracked CLAUDE-canon in full, AUDIT-CHECKLIST classes 1–22 and pre-audit R1–R10/pre-cutover excerpts, SYSTEM-MAP settings/UI and relevant bar sections, and RULEBOOK authority/risk/owner-policy excerpts. Relevant classes: 8 (resolved knobs), 9 (binding rather than is_active), 24/53 (effective boundary and production-call-site parity), 49 (missing versus computed values), and R4/R6/R9 (line evidence, bounded verdict, isolation). Historical prose is not runtime proof.

Latest commits for reference documents, read at this pin:

- `07b53e65 cleanup batch 1 — B5 the mutation harness that cannot fake a verdict, B6 the worktree-add check` — CLAUDE-canon.
- `dfda15e1 test: isolate session clock fixtures from weekly backfill workers` — AUDIT-CHECKLIST.
- `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap` — SYSTEM-MAP and VL-TRADING-RULEBOOK-v1.

The September 13 correction is explicit: the owner loss control is **daily**, and no additional mandatory per-trade cap is implied here. No policy edits are proposed by this worker.

### End-to-end boundary map

[A] `NewServer` (`api/server.go:37`) constructs Gin with logging/recovery, CORS, CryptoHandler and exchange-state cache. `setupRoutes:82` builds public `/api` routes and protected group `authMiddleware` plus `planTraderOwnership`, then mounts UI fallback last (`:639`). `Start:894` binds configured interface or `127.0.0.1`; no read/header/idle timeout is set in its `http.Server` literal. CORS is wildcard, with OPTIONS terminated before handlers (`:66`). Health only reports process response/revision; it does not certify bars, account or execution safety (`:643`).

[A] `authMiddleware:851` validates exact `Bearer <token>`, blacklist, and JWT, then sets user_id/email. The ownership middleware's name can overstate its reach: `api/handler_plan.go:97–131` only checks routes whose full paths start `/api/plan/` or `/api/risk/`. All other protected handlers need their own owner resolution. `getTraderFromQuery` (`server.go:798–848`) provides that resolution to its callers: authenticated requests select only the user's own trader; public requests may explicitly name any existing trader. It invokes `LoadUserTradersFromStore` before resolving, so this helper is not a pure read of memory.

[A] Manager identity is global: `manager/trader_manager.go:56–65` indexes by trader ID with no user argument. Its `RemoveTrader:359–373` stops a running trader and removes the global entry. Scoped DB calls and global runtime calls therefore need explicit ordering/ownership validation at their seam.

[A] Trader creation (`handler_trader.go:339–582`) validates enabled owned model with credential, owned strategy, supported enabled exchange/credentials, probes broker equity, persists a stopped trader, and attempts runtime load. Persistence success returns201 even if runtime cannot initialize, with `startup_warning`. Update (`:585–764`) verifies the existing owner, merges defaults, persists, removes/reloads, and may launch Run asynchronously. Start (`:795–897`) verifies owned full config, refuses empty NT account binding, reloads fresh config, starts Run asynchronously, and warns rather than failing on status persistence. Stop (`:900–935`) verifies ownership before runtime Stop. An HTTP success at these asynchronous boundaries is not a broker or running-loop acknowledgment.

[A] Strategy editing (`strategy.go:174–440`) defaults omitted create config; clamps limits, validates indicator periods, merges nested update fields and preserves AI configuration across unconfirmed type changes. Saving logs resolved diffs and reloads all user traders bound through `Trader.StrategyID`. Legacy `activate` only changes `is_active` (`:461`); it is not binding. Prompt preview (`:567`) builds a system prompt using simulated equity and configured first symbol. Test-run (`:623`) fetches market/quant data and optionally calls the selected user's provider (`runRealAITest:806`), but has no execution call. A request called test-run may still make network/data/paid AI calls.

[A] NT chart flow (`handler_klines.go:23,345`) dispatches exchange=ninjatrader to the shared `market.FuturesBarsProvider`; unbound/nil/nonfutures returns `[]`, no crypto fallback. SSE (`handler_bars_sse.go:25`) consumes an opaque ticket, injects the user, resolves owned trader and reads that TCPTrader's BarCache. `stream_ticket.go` generates32 random bytes, retains user/30s expiry under mutex, deletes on first consume. The **ticket's admission** expires after30s; the already-open stream itself remains until disconnect/write error. Snapshot then 1s polling resends bars at/after lastT, with15s keepalive. Late corrections earlier than lastT are not emitted by this loop.

[A] NT account read (`handler_account.go:43`) returns account list, persisted selected binding and effective current account when a bound snapshot exists. Selection (`:109`) checks AddOn IsSim and optional allowed-account list; it resets only this TCPTrader cache and persists its binding, intentionally no shared account_select wire command. Manual position close (`handler_trader_status.go:124–317`) verifies ownership, then NT uses existing TCPTrader CloseLong/CloseShort(symbol,0); crypto instantiates a temporary adapter and records where appropriate. Debug test trade (`handler_debug.go:149`) resolves owned futures/TCP trader and calls DebugPlaceTestTrade directly, bypassing AI/risk by design. Armed seam (`handler_armed_seam.go:15`) explicitly checks ownership and delegates place/place_stop/cancel to AutoTrader test helpers; downstream env/SIM guards were not reread in this slice.

### Ranked static findings and repair boundaries

#### S1 — foreign trader deletion reaches unrelated equity and runtime state

**[A] PROVEN source path; runtime exploit UNVERIFIED.** Input: valid JWT for user B, `DELETE /api/traders/A` where A belongs to another user. Route is registered protected (`server.go:205`); JWT admits B, prefix ownership middleware passes the non-plan/non-risk path. `handleDeleteTrader` (`handler_trader.go:767–792`) does not verify owner. It calls `store.Trader().Delete(B,A)`. `store/trader.go:223–229` first deletes **all EquitySnapshot rows by trader_id=A**, without user scope, ignores that deletion's error, then deletes Trader where ID=A AND UserID=B and returns only `.Error`, not a matched-row result. With ordinary zero-row deletion success, the handler continues to global manager GetTrader(A), Stop and RemoveTrader(A). Manager lookup/removal are global (`manager/trader_manager.go:56,359`).

Consequences are conditional on a foreign trader ID, reachable authenticated endpoint and ordinary DB success; this audit did not test them. Repair should verify owned trader before any dependent write/runtime mutation, make store deletion scoped/transactional, and assert no foreign equity or runtime effect. Existing `handler_plan_idor_test.go` only covers plan/risk middleware and query-helper ownership, not this registered delete route. `handler_trader_test.go` only exercises validation helpers.

#### A1 — additional protected read/account endpoints miss ownership

**[A] PROVEN missing source checks; cross-user output/state effect UNVERIFIED at runtime.** Same valid-user premise and prefix middleware bypass:

| Route | Handler | Unscoped boundary |
|---|---|---|
| GET /config/resolved?trader_id=A | config_resolved.go:193 | Manager.GetTrader(A), exposes three resolved strategy values |
| GET /accounts?trader_id=A | handler_account.go:43 | Global trader/TCP list and GetByID persisted account |
| POST /account/select?trader_id=A | handler_account.go:109 | Global TCPTrader.ResetAccountState before scoped store write |
| GET /audit/decisions?trader_id=A | handler_decisions.go:28 | Global trader store audit records, optional account |
| GET /desk?trader_id=A | handler_desk.go:26 | Global DeskStripAt account/position read model |
| GET /traders/A/grid-risk | handler_trader_status.go:27 | Global grid risk model |

The account-binding **DB write is user-scoped** (`store/trader.go:206–210`), so this is not evidence that user B can persistently rebind A. The defect is earlier runtime reset and misleading success on zero-row write/failure. Do not turn this into an unsupported persistent-rebinding claim. `TestConfigResolvedIsRegisteredProtected` verifies only registration behind JWT; its bare handler tests do not supply a manager/foreign trader. Decision-audit tests (`handler_risk_test.go:152–179`) check missing ID/invalid since only. A broad ownership middleware or explicit owner checks need tests on actual registered handlers, not only a synthetic `/plan/today` callback.

#### A2 — validation can reject after configuration is already persisted

**[A] PROVEN source ordering; concrete overflowing fixtures not executed.** `handleUpdateStrategy` persists via Strategy.Update (`strategy.go:368`) and logs diff (`:378`) before token overflow check (`:380–397`). If all known limits are exceeded, response400 arrives after storage changed and before reload. A user believes save failed while a future reload uses the rejected config. `handleUpdateModelConfigs` likewise runs UpdateWithName (`handler_ai_model.go:217`) before ValidateThinkingKnobs (`:225`). Invalid thinking input returns400 after ordinary model fields changed; no reload follows. Map iteration can make a multi-model request partly applied before another entry fails. UpdateThinking failure is only WARN. Validate first; group intended DB changes transactionally and explicitly expose reload failure rather than implying active runtime parity.

#### A3 — trader-ID tests validate a different algorithm than production

**[A] PROVEN disagreement; collision occurrence UNVERIFIED.** Production `handleCreateTrader` (`handler_trader.go:414–419`) uses first8 exchange-ID bytes + full model-ID + `time.Now().Unix()`. Same exchange/model within one second generates identical ID. `api/traderid_test.go:45–47` instead defines its own `generateTraderID` using UUID; TestTraderIDUniqueness and TestTraderIDNoCollision never call production ID generation. Their green result cannot certify production uniqueness. Extract/use one production generator and drive actual create boundary with a controlled clock/ID source; preserve negative concurrent create coverage.

#### A4 — synthetic crypto close accounting is marked FILLED

**[A] PROVEN code; not an NT execution defect.** NT returns early through real TCP close, and most crypto brokers are skipped for OrderSync (`handler_trader_status.go:322–326`). The remaining path (notably kucoin in this switch) passes entryPrice into `recordClosePositionOrder` as exitPrice (`:309`). Missing price becomes `quantity*100` (`:361–367`), commission is assumed0.04%, and FILLED order plus fill is inserted without confirming broker fill. Missing orderId becomes `fmt.Sprintf("%v", nil)` ("<nil>") and is not excluded by the empty/zero check. This violates no-fabricated-values for the residual crypto path; do not report that NT fills use fabricated prices. Fix by using broker execution evidence or explicit unresolved status, and test each sync/non-sync branch.

#### B1 — account and lifecycle reporting can overstate success

[A] Account selection reports200 even when UpdateAccount fails (`handler_account.go:205–216`), after resetting state. `handleGetAccounts` builds per-item IsCurrent before overriding top-level Current to bound snapshot (`:70–98`), so the two current markers can disagree. Start reports started before asynchronous Run completes (`handler_trader.go:881–896`); config reload failures are mostly WARN-only. `handleSyncBalance` computes percentage dividing by oldBalance without a zero guard (`handler_trader_status.go:90`), so zero baseline can produce non-JSON finite values after the DB write. Its NT probe is `ntTrader.New` (`exchange_account_state.go:262–266`), the legacy config constructor rather than the running bound TCP instance; this boundary warrants NT balance validation by the execution worker.

#### B2 — public analytics remain independent of removed pages

[A] Public `/traders`, `/competition`, `/top-traders`, `/equity-history`, batch and public config remain mounted (`server.go:114–120`). Single equity history accepts any existing explicit ID when no JWT context (`getTraderFromQuery:814`); batch directly reads each requested ID (`handler_competition.go:328`). These are intentional public route registrations, not a claim of JWT bypass. Whether public competition respects visibility is a manager dependency not fully audited here. **Direct equity reads do not check ShowInCompetition in the inspected code.** Removing Competition/Strategy Market pages did not remove their APIs. Bound deployment exposure is unknown; listener defaults loopback.

#### B3 — onboarding has global credential side effects

[A] `handleBeginnerOnboarding` (`handler_onboarding.go:43–94`) returns raw wallet key deliberately, stores user model, sets process-wide CLAW402 env and rewrites shared .env. `resolveBeginnerWallet:155–207` may adopt orphan model. UpsertEnvFile (`:286`) rewrites nonatomically, only replaces first duplicated key, and mode0600 on WriteFile does not tighten an existing file's mode. These are source risks in a multi-user/control-plane design, not proof of leaked secrets. No actual credentials were inspected. Error responses may reveal env path/persistence reason by design. Concurrent preferences similarly use unguarded read/modify/write (`agent_preferences.go:34,67`).

#### B4 — route documentation disagrees with handlers and can instruct the agent incorrectly

[A] `GetAPIDocs` injects registered schema verbatim (`route_registry.go:50`). Examples: trader create/update schemas say minimum scan3 while handlers accept positive1 (`server.go:198,204`; `handler_trader.go:455,648`); update says only fields being changed while Name/AIModelID/ExchangeID are binding-required; close-position schema omits required Side; duplicate docs suggest automatic copied name while handler requires Name (`strategy.go:489`); active strategy description says currently in use although bindings determine use; account select return schema says current but handler sends current_account. Strategy schema embeds deprecated VLOS guidance. `routeRegistry` is global append-only; constructing multiple servers duplicates docs and concurrent construction has no synchronization here. These are concrete drift cases, not evidence every caller fails.

### Remaining file-specific semantics and limits

[A] `config_resolved.go:79–189` is a good no-fabrication pattern: saved unset differs from resolved default; resolver metadata exposes consumers but no secret values; uncomputed resolved list is absent, computed empty arrays are arrays. Resolution covers RR, plan mode and HTF veto only, not a full effective-config dump.

[A] `handler_expectancy.go:38–74` invokes global LoadAndBuildAt and publishes min_n, excluded/unresolved metadata, era filter and criterion. It is protected but not user-scoped in this handler. Model math/SQL is outside this slice. `handler_plan_geometry.go:5` returns nil/WARN on unavailable geometry; `openPositionProvenance` (`handler_plan_position_provenance.go:20`) selects first open position, distinguishes unrecorded version, and does not account-filter; multiple open rows or account switching need downstream contextual review.

[A] `handler_competition.go:128–188` single curve's percent is unrealized/base balance, whereas batch (`:328–448`) percent is (equity-initial)/initial. Both are computed but not equivalent metrics. Batch may append live point only if last snapshot older than30s; default history is90days despite500-record comment. Decision latest caps100 and reverses store order; broad decisions uses10,000 even though docs suggest limit parameter. Audit handler passes limit onward rather than capping itself; store maximum was not followed.

[A] External chart adapters use context.Background rather than request context; CoinAnk may fall back to Binance, TwelveData invalid parse leaves a zero element, Hyperliquid ignores numeric parse errors. These observations apply to those adapters, not live NT feed. `handleSymbols` lowercases switch selection but checks original exchange string inside, so mixed-case Hyperliquid may skip crypto mids.

[A] `errors.go` only scrubs errors routed through its helpers; arbitrary public messages are not automatically cleaned. `isValidPrivateKey` checks length/prefix not hex validity; onboarding's separate walletAddressFromPrivateKey performs actual ECDSA parsing. Utilities mask secret bytes but deliberately preserve URLs/addresses. Authenticated decrypt returns plaintext when transport encryption is enabled; no finer role/ownership check is visible in this handler. Agent chat grants authenticated sessions CanExecuteTrade=true and CanViewSensitiveSecrets=false (`agent_routes.go:13–36`); actual trade/secret policy enforcement is delegated to agent code and not certified here.

### Historical graph reconciliation

[A] Read historical Understand Anything file summaries for18 matching assigned files and first25 outgoing non-contains edges; selected115 nodes/640 outgoing edges exist but were **not all manually read**. July10@7a8adce0 is historical. Critical correction: handler_account summary says selection sends account_select; current `handler_account.go:183–202` explicitly removes that wire action. Handler_trader_status graph says protective-order cancellation/asynchronous polling across all supported exchanges; current inspected close handler has NT early return, skips OrderSync brokers and does not call pollAndUpdateOrderStatus. Graph imports connect a production API file to many store test files; these are coarse package expansion, not actual Go file imports/type-resolved calls. Server summary's350-line setupRoutes is now559 lines. Six current assigned files lack historical file nodes (config_resolved, armed_seam, desk, expectancy, plan_geometry, plan_position_provenance). No graph reindex/update was performed. Root's CGC evidence remains separately scoped; this worker did not query CGC.

### Verification plan and evidence limits

Read in full: config_resolved_test.go, handler_trader_test.go, traderid_test.go, handler_plan_idor_test.go; selected decision-audit tests. **Not run**: no broad or targeted tests, no reproducers, no network or live data. Highest-value follow-up is isolated test DB and actual protected route dispatch for S1/A1, asserting both DB and runtime remain untouched on foreign IDs. Then exercise rejected config writes, production ID collision source, NT balance source, and residual crypto accounting. Test both owner-success and foreign-refusal; superficial JWT registration is insufficient. No claim about current users/counts, effective switches, live broker inventory, P&L rows, deployment or main startup latch is made from this API slice.

---

<a id="section-7"></a>

> Original: [reviews/02/report.md](reviews/02/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 02 — API plan, account, auth and manager boundaries

**Source pin:** `63968be62e44db2fb07a92883e02127b9064b0be`, read-only worktree `/tmp/vl-understanding-execution-20260913`. Verified revision and empty porcelain before reading and at close. All **22 assigned files / 6,660 lines / 165 named functions** read completely; callbacks grouped under owning functions. `reads.json` records full assigned coverage separately from dependency/test excerpts. No source/runtime/account/config/DB changes, requests to the running service, trading, tests, or graph mutations were performed. This is source verification, not a runtime acceptance verdict.

[A] = directly read source; [B] = inferred consequence requiring runtime/test confirmation. Finding priority S/A/B/C is separate from evidence tier. None of the source concerns below is presented as a demonstrated exploit, broker incident, or observed loss. Pre-audit reference: `docs/superpowers/AUDIT-CHECKLIST.md` R1–R9, classes 7 (time), 9 (binding), 23/49/53 (read truth and absent evidence), 70/73 (dispatch/spec provenance), 124 (maintenance import). Root controls any follow-up tests and fixes.

Spec freshness lines read at this pin:

- SYSTEM-MAP.md: `565e8fbe 2026-09-13 00:39:02 -0500 fix: use owner daily-loss controls without requiring a per-trade cap`
- VL-TRADING-RULEBOOK-v1.md: `565e8fbe 2026-09-13 00:39:02 -0500 fix: use owner daily-loss controls without requiring a per-trade cap`

The current rulebook correction is DAILY loss control, with no newly mandatory per-trade cap. Nothing in this report proposes restoring the older interpretation. CLAUDE-canon was read fully; its keeper-based lock instructions supersede the untracked AGENTS hand-heartbeat prose. No worker lock operation was needed or performed in this parent-provisioned isolated tree.

### What this slice does

[A] Authentication begins with `handler_user.go:55,144`: registration only when user count is zero, bcrypt credentials, JWT issue. `auth/auth.go:85` issues 24-hour HS256 tokens; `api/server.go:851` checks blacklist then cryptographic/time validity and sets user/email context. `handler_user.go:25` revokes a logout token in memory. Reset-password is permanently 410; reset-account is protected at the router plus env and literal confirmation gates. Defaults seed three localized strategy variants transactionally; registration itself and default initialization are not one transaction.

[A] Protected routes use `api/server.go:146` → `planTraderOwnership` (`handler_plan.go:97`), which only applies to `/api/plan/` and `/api/risk/`. It probes query first, then restores POST/PUT JSON after inspecting body trader ID. Ownership is a list of the JWT user's trader rows, with failure treated as not found. Ordinary account/order reads use `getTraderFromQuery` (`server.go:798`): load owned traders, select only an owned explicit/default trader. SSE gets user identity from a consumed 30-second ticket, then uses the same resolver (`handler_bars_sse.go:25`). The ticket map is mutex-protected and removes a token on first redemption; the TTL governs redemption, not the lifetime of an established stream.

[A] Exchange settings (`handler_exchange.go`) project credential presence flags, accept plain/encrypted bodies according to config, retain blank secrets, validate effective values, and save configurations enabled. Updates remove affected manager instances and reload owned traders; persistence can succeed even when runtime reload fails. Account-state probing (`exchange_account_state.go`) clones maps across a 30-second per-user cache and launches one goroutine per exchange. Errors become credential/permission/unavailable statuses.

[A] `manager/trader_manager.go:575` resolves the actual bound strategy, exact model row or deterministic legacy provider fallback, enabled exchange, credentials, cadence/position mode and NT account into `AutoTraderConfig`, then calls `trader.NewAutoTrader`. Existing running DB rows auto-start in a goroutine. Registry maps are mutex-protected. Account fetching for comparison can happen under the registry read lock; loading/removing can do slow work under write lock. Competition results are cached for 30 seconds, ranked, and truncated to 50, with a top-five projection. Its ten-second timeout stops waiting, not the inner account call.

[A] Plan cards (`handler_plan.go:250`) resolve the effective registry, requested/current session, wrap-aware `PlanChainTradeDate`, newest or requested stored version, and that version's overlays. Replan budgets are recorded counters, not inferred from version numbers. A committed row plus ongoing read is `replan_in_flight`; only no-row plus in-flight is first writing. The response joins current level facts, owner provenance, recorded version-scoped scenario/death/one-setup/geometry data, current order-book evidence, open-position provenance, weekly refs, fade labels and uncarried edits. Historical plan documents are therefore combined with explicitly current facts; they are not an as-of historical replay.

[A] `handler_plan_order_truth.go:63` selects highest placement sequence then row ID per displayed version/scenario/leg, retaining `armed_under_version` separately. Intended plan quotes, composed ledger quotes and current accepted broker quotes are distinct. `acceptedOrderPrices:141` requires a dated, reason-free snapshot, unique signal-derived entry/SL/TP names, live order classification, and positive finite quoted prices. It never fills missing accepted quotes from intended/composed values. Snapshot account/contract/freshness validation is delegated to `OrderBookForDisplayAt`, not reimplemented here.

[A] Owner overlays (`handler_plan.go:910,1016`) have a coarse mutex around read/fold/strict patch/validate/price armor/append. Owner note/tag metadata is stripped from level replacements and written separately to sticky owner rows after success. Alerts support scoped acknowledgement, P0-aware dismissal and clearing only acknowledged rows. Reread/reset delegate eligibility and execution to AutoTrader. Approval keys are per trader/CME day; the registry is global stored configuration, with next-day execution adoption promised by the response.

[A] Ask-Planner (`handler_plan.go:1405`) resolves active/historical/no-plan context, calls the configured planner or canned sandbox, parses the anti-sycophancy reply, strips proposals outside active context, and persists Q&A. Apply reads the trader's proposal and appends its patch to the current chain; decline records a separate owner row. Re-align (`:2067`) similarly produces a proposal after current-plan/context checks and recorded debounce/cap lookup; failures log/emit a P1 alert. Manual re-align bypasses automation caps. Cost is an explicitly approximate byte-count estimate at fixed rates, not billing.

[A] Account/orders (`handler_order.go`) expose account snapshots, positions, close history, fills and open orders. The header's ledger day total is strict corrected PnL with unresolved count. Risk status (`handler_risk.go:183`) publishes compatibility env fields plus `not_enforced` explanations; the resolved desk is the preferred enforcement read model. Force-flat (`:78`) directly submits an NT TCP close; the CSV adapter records intent only. Bar-truth arbiter is an owned TCP maintenance surface, and named-contract history import is a separate env-gated seam. SVP uses the shared futures provider with the display timeframe and `AISVPBarCount`, never falls through to crypto. Stream-cut telemetry makes no automatic tuning changes. Telegram is singleton config with optional nonblocking reload; wallet handlers derive/generate EVM keys and only send public address to balance queries. Go static UI serving and sandbox setup are separate operational surfaces.

### Findings and unresolved paths

#### A priority — ownership and mutation boundaries

1. **[A source defect; B disclosure consequence] Order fills lose the owned trader constraint.** `handler_order.go:359–397` validates the selected trader but then calls `GetOrderFills(orderID)` at line 386. `store/order.go:263–271` filters only `order_id`, in the shared store. `/orders/:id/fills` is protected but outside plan/risk prefix middleware (`server.go:424`). Possession of any owned trader does not establish ownership of the requested numeric order. No cross-user request was run. A regression needs two users/traders/orders and the actual handler route.

2. **[A source defect; B disclosure consequence] AI costs are authenticated but not owned.** `handler_ai_cost.go:10` reads a caller-supplied trader ID directly, and `:33` returns a global summary. Routes are outside the ownership prefix (`server.go:239–240`). The handler does not obtain a user-scoped store filter. Single-user intent reduces normal exposure but is not authorization. Global risk counters, freezes and Telegram settings are intentionally/global-shaped too; those should be explicitly classified rather than silently assumed tenant-safe.

3. **[A source defect; B cross-trader/model context consequence] Historical Q&A fallback is global.** `resolveAskContext`, `handler_plan.go:1343`, uses `ListRecent(1)` rather than `ListRecentForTrader`; `store/plan.go:411` has no WHERE clause. An owned trader with no current plan can receive another trader's last plan as context, which `handlePlanAsk` sends to its planner and records under the requesting trader. The current plan-history endpoint uses the scoped helper correctly. Existing fallback tests only create one trader's plan.

4. **[A control-flow gap; B stale/repeated mutation] Ask Apply is chain-bound but not version-bound or atomic with its applied marker.** `handler_plan.go:1666` checks `Applied` before entering `applyPlanOverlay`'s mutex; `:1701` marks applied after append and ignores errors. Two overlapping calls can both pass the precheck; patches without test ops can both append. `store/plan_qa.go:19–50` stores plan ID/date/session but no plan version. A proposal for v1 can patch v2 of the same chain if its paths still validate; the helper does not require lifecycle `active` either. Decline uses a read-then-append idempotence check (`:1605`) and Apply never checks recorded decline. This is a static transaction/provenance concern, not proof that the owner applied a stale edit. Overlay mutex correctly serializes overlay writers but cannot cover checks performed outside it or independent planner version creation.

5. **[A reset lifecycle/error gaps; B residual authority] Account reset ignores trader and strategy Delete errors (`handler_user.go:259–270`), checks only user deletion, and never stops/removes in-memory traders.** Env/auth/confirm gating is real, and orphan credential adoption is removed. However a successful response does not certify all intended deletions or stopped runtime. `authMiddleware` checks no persisted user existence and password change/reset do not revoke preexisting JWTs; logout revocation itself is process-memory only. This is a remaining authenticated reset/session-lifetime concern, not the previously fixed public takeover chain.

6. **[A race shape; B extra registration] First-user registration uses separate Count then Create (`handler_user.go:55–112`).** `store/user.go:15–20` only shows unique email/primary ID, not a singleton constraint. Concurrent distinct-email registrations could both pass zero count; the existing closed-after-first test is sequential. No registration was attempted.

#### A/B priority — truth and crash boundaries

7. **[A nil dereference path] Q&A no-plan with available bars dereferences no row.** `resolveAskContext:1392` unconditionally evaluates `ctx.row.CreatedAt` once provider bars exist. No stored row leaves `ctx.row=nil`. `ask_context_test.go:30` exercises no-plan but supplies no bars, so it misses this branch. The advertised no-plan market-only path needs a provider-populated fixture. No running-service request or panic reproduction was performed.

8. **[A nil/time defects] Ask Apply constructs IDs using `sess.Name` before testing `!ok` (`handler_plan.go:1686–1688`).** `kernel/session_registry.go:193` explicitly returns `(nil,false)` outside windows. An existing proposal applied during the gap can panic before its intended 409. Apply also uses calendar date (`:1675`) while normal plan lookups use wrap-aware chain date (`:289–292,1041–1044`); realign (`:2112–2127`) uses calendar date too. Asia after midnight can fail to find/authorize the correct chain. These are source paths, not measured production occurrences.

9. **[A definite validation mismatch] Every enabled NT account-state probe omits required `nt_data_dir`.** `exchange_account_state.go:327–340` calls variadic `MissingRequiredExchangeCredentialFields` without `NTDataDir`; `store/visibility.go:34–41` treats absent arg as missing. Thus it returns `missing_credentials` even if the configuration holds a valid dir. The later NT constructor (`:260`) is a dormant legacy CSV path whose GetBalance is fixed $50,000 (`trader/ninjatrader/trader.go:160`) and default asset is USDT (`accountAssetForExchange:318`). Merely adding the omitted arg would expose false balance semantics; the full path needs the TCP-owned account truth. No config was changed.

10. **[A submission-versus-flat mismatch] Emergency flat is not proven flatness.** TCP handler calls `CloseLong("",0)` (`handler_risk.go:106`) and reports `positions_flattened:1` after successful send (`:122`). `TCPTrader.sendCloseAt` (`tcp_trader.go:753`) stamps account/trader but returns `close_submitted`, without waiting for broker position/order evidence. Handler does not cancel pending arms or stop entry re-placement itself. `MaybeForceFlat` resets the old global daily-PnL accumulator (`kernel/engine_analysis.go:1106`), not a persistent halt; CSV adapter increments an intent count and writes no sentinel despite its comment. Rulebook explicitly distinguishes send/cancel/flat proof. Downstream NT order settlement and arm behavior require the independent execution audit; no loss, flatten failure or re-entry is asserted here.

11. **[A response ordering] Bar arbiter writes HTTP 202 before sending the deep-backfill request (`handler_bar_truth.go:80–89`).** A send error then attempts another JSON response/status; client has already received acceptance. The existing source-lint test verifies merge-not-clear text, not this error path. `dbRows5` also collapses storage errors into nil, so diff can display zero counts/ATR instead of an unavailable state. Independent ATR comparison is window-common but not a coverage proof; `intersectByT` relies on DB ordering and missing minutes are not repaired.

12. **[A inconsistent finance labels] Risk status sets `daily_pnl_usd` from `total_pnl`, `last_reset_utc` from request time, and notional from price×quantity without futures point value (`handler_risk.go:226,237,260`).** Decorative-limit warnings do not clarify these three measurements. Account ledger day's end is midnight+24h (`handler_order.go:157`), a DST edge; close-history rows can be account-scoped while aggregate stats remain trader-wide (`:237–250`). Plan trade-review exposes `RealizedPnL`, not corrected PnL (`handler_plan.go:1732`); it is an adherence display, but must not be pooled as corrected expectancy. No DB rows or performance sample were queried.

#### B/C priority — important limitations

- [A] Manager binds persisted NT account only inside `NTInstrumentName != ""` (`manager/trader_manager.go:681–687`). Empty instrument plus nonempty account loses the explicit account assignment. Downstream default resolution was not fully read, so actual account-routing consequences remain unverified.
- [A] Manager's `RemoveTrader:359` calls Stop under write lock; `AutoTrader.Stop:1015` waits monitor goroutines but does not visibly join all planner/main-loop work. Concurrent replacement safety needs its own lifecycle audit. Competition inner calls survive timeout and its cached timestamp is read for logging after unlock (`:171`); caches are shallow for nested values. These were not race-tested.
- [A] Exchange batch writes are not all-or-nothing: an early account can be saved before a later validation/write fails, preventing final invalidation/reload. Safe template catalog lists alpaca/forex/metals while create validation rejects those types, and omits some accepted crypto types. Requested `Enabled` is ignored because persistence always enables; this is explicit code, potentially confusing callers.
- [A] Re-align debounce/cap read completed QA rows before an expensive call; no in-flight reservation protects concurrent requests (`handler_plan.go:2144–2161`). Two simultaneous calls may both proceed. Failed persistence of Q&A is ignored in Ask and re-align. Fixed text-size costs omit hidden reasoning/provider-specific prices; they are approximate by design.
- [A] `scenarioStatusForLifecycle:2290` creates expired entries for every scenario of a nonactive plan even when no status was recorded, contrary to its comment that absent statuses stay absent. The lifecycle projection may be intended, but it must not be described as observed evaluator facts.
- [A] Owner overlays are validated at hard storage caps, preserving larger historical documents. Price armor only checks level/target-chain prices and is explicitly fail-open without market data (test confirms). This does not establish every arm price is armored; kernel validation owns its own checks.
- [A] `MountUI` is GET/HEAD/API-safe and `http.Dir` bounds ordinary path traversal, but no symlink audit of deployed dist was performed. Its mtime-based age is not a content revision proof. Sandbox provider ignores requested symbol and uses current-clock offsets; isolation rests on startup gating outside this slice. Wallet generation deliberately returns a new secret to the authenticated caller; no external requests were made by this audit.

### Tests read and their limits

Fully read: `ask_context_test.go` (no-plan/dead fallback, no live bars), `handler_plan_idor_test.go` (query/body ownership, body restoration, force-flat prefix and no global default), `handler_plan_order_truth_test.go` (version/placement/split legs, stale/ambiguous/nonlive accepted quotes), `handler_plan_overlay_test.go` (required fields and pure price armor including no-feed fail-open), `w13_realign_test.go` (pure debounce, stored counters, failure alert, approximate cost), `handler_bar_truth_test.go` (source-lint merge invariant). Other security/exchange/UI/risk/SVP tests were located by test name only, not counted as full reads. No test results are claimed. Relevant missing fixtures are the exact malformed/no-feed/clock/concurrency/ownership branches listed above, ideally through production router/call sites per checklist parity law.

### Graph comparison

Historical Understand Anything graph was extracted by assigned file paths: **86 matched nodes, 440 incident edges**, only **12 of 22 current files represented**. Snapshot is July 10 at `7a8adce0`, not current. `handler_user.go` graph summary still says orphan-record adoption; current source explicitly deleted it. `handler_trader_config.go` summary does not reveal that prompt writes are compatibility-only. Ten current files including main plan handler, order-truth view, import and static UI have no represented file nodes. Historical package-import fanout includes edges to test files (e.g. exchange-account-state→store tests); these are not runtime call proofs. The AST census supplies exact declaration/range and syntactic callee expressions in `functions.json`; targets are explicitly not type-resolved. No new CGC query/reindex was performed here; root's historical CGC export should be compared with these source findings.

Artifacts: `report.md`, `reads.json`, `functions.json`, `graph.json`, supplemental `historic-graph-extract.json`, and reproducible artifact assembly script. Scope complete for assigned source; whole-repository, runtime settlement, performance, deployed assets and secret configuration remain outside this worker's evidence.

---

<a id="section-8"></a>

> Original: [reviews/03/report.md](reviews/03/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 03 — conversational agent entry, routing, planner and workflow

Source base: `63968be62e44db2fb07a92883e02127b9064b0be`. Isolated worktree: `/tmp/vl-understanding-surfaces-20260913`; `pwd`, revision and empty porcelain status verified at start. All **15 assigned files / 9,219 lines** were manually read in full with bounded output. `functions.json` records **301 named declarations and 9 local callbacks** from the root Go AST census, with manually supplied purposes and explicitly syntax-only call expressions. `reads.json` separates 20 additional dependency/test/doc files from assigned coverage. No production files, DB, account, runtime, graph, or settings changed. No tests or live requests executed; findings below are source observations and static risks, not reproduced trading incidents or proven exploits.

Rules read: root AGENTS instruction mirror; complete tracked `CLAUDE-canon.md`; `AUDIT-CHECKLIST.md` R1–R10 and class48 twin-path protection; relevant SYSTEM-MAP planner/cadence/settings and RULEBOOK risk/policy sections. Current canon says the lock keeper owns heartbeats, correcting the older root instruction mirror. No lock acquisition needed inside parent-provided read-only worktree. This work does not reinstate historical per-trade cap policy.

Spec provenance at this base (orientation only; no implementation built):

```
SYSTEM-MAP.md: 565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap
VL-TRADING-RULEBOOK-v1.md: 565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap
```

### End-to-end boundary

[A] `main.go:646` constructs **one** Agent shared by its HTTP handler and starts it. `api/agent_routes.go:11` authenticates both chat routes and supplies `WithStoreUserID` plus session policy. `agent/web.go:81` and `:123` decode chat input, supply a derived numeric session ID only when request `user_id == 0`, and set 55-second synchronous / 120-second stream request deadlines. The store identity and numeric conversation identity are separate values thereafter.

[A] `agent.go:428` / `:476` reload the model, parse a language prefix, log the input, intercept `/status`, `/clear`, trade confirmation and model-wallet questions, then call `thinkAndAct` / `thinkAndActStream` (`planner_runtime.go:823,854`). The latter serialize with a numeric-user flow mutex. Router, planner, active skill session, legacy skill session, workflow, execution state and suspended snapshots coexist; they are not interchangeable aliases. `/clear` explicitly clears many of these stores (`agent.go:514`), but pending trade removal is not in that clear list.

[A] With AI present, `tryLLMIntentRoute` (`llm_skill_router.go:24`) first asks for one decision over topic intent, business action, context mode, optional tasks and extracted fields. `executeUnifiedTurnDecision:259` gives strategy-create confirmation and active continuation priority. New management actions go to `driveActiveSession` outside this assignment; multiple tasks usually seed `WorkflowSession`. Market/account/tool-heavy requests go to `runPlannedAgentWithContextMode` (`planner_runtime.go:2552`). Without AI it tries state-priority/hard skills, then limited direct data/configuration fallback.

[A] The live chat planner path prepares persisted execution state (`:2646`), then `executePlan:3029` repeatedly asks `decideNextStep:2705` for an immediate batch when no pending step exists. It executes **tool steps sequentially**, calling `executePlanTool:3657` → `handleToolCall` (`tools.go:881`). Reason steps call the model; ask-user steps save waiting metadata and return; respond steps format deterministically or invoke final model synthesis. The loop counts both selection and execution iterations against 12. Per-stage budgets are create 36s, replan 24s, reasoning 30s, final36s and direct8s (`:23`); `withPlannerStageTimeout:245` never extends an earlier parent deadline. Tool string results and runtime bookkeeping may outlive a useful response budget where lower helpers do not accept context; stock HTTP has a separate10s timeout.

[A] `createExecutionPlan:2898`, `replanAfterStep:3388`, `shouldAttemptReplan:3524` and `executeReadFastPath:571` have **no non-test call sites found by exact-name search in agent**. Their tests and graph nodes do not establish live full-plan/replanner execution. `executePlan` discards `referencesChanged` rather than invoking the replan helper. They remain described in the function census as dormant helpers with that search scope, not globally proven unreachable symbols.

[A] Fallback `thinkAndActLegacyWithStore:3952` builds the legacy system prompt and data enrichment, deliberately omits previous conversation history, and runs up to five tool rounds before a final plain response. Non-timeout planner failures can enter this fallback even after earlier steps. AI-service-error summary fallback may instead report completed tool labels. Successful primary responses trigger asynchronous task-state/history maintenance (`:2623`) using a separate20s background context.

### File roles and invariants

- **agent.go (1–1166):** object lifecycle, model selection, entry points, bilingual prompt, context reads, direct status/account fallbacks. Candidate selection tries requested store user then `default`; enabled/key filtering follows ranking. Positive wallet balance for claw402/blockrun-base wins, then balance amount/newest model/ID (`:224`). Provider registry defaults preserve actual API model names instead of row IDs. Owner-scoped trader summaries and positions use `store.Trader().List(storeUserID)`; global status and scheduler do not.
- **history.go (1–117):** `Add`, `Get`, `Replace`, `Clear`, `CleanOld` protect numeric-user map with RWMutex. Reads/replacements copy message slices; cap is messages, despite maxTurns naming. `CleanOld` drops an entire session only when its last message is old. Lazy initialization and background coordination are outside this object's lock.
- **llm_skill_router.go (1–694):** normalized routing protocol, reliability threshold, continuation-first execution, management task dispatch, proposal handling, source-labelled references and trader list filters. Structured output is accepted as routing authority after syntax/shape normalization; not a separate cryptographic or user-consent boundary.
- **llm_flow_extractor.go (1–578):** prompt says extract only canonical explicit values. Parser permits code fences or surrounding prose but requires JSON string values for fields. Field filter is an exact allowlist. Missing-slot rules are separate handwritten logic from DAG/registry. Apply function uses first matching task, guards unmentioned model provider and sanitizes strategy config/type branch.
- **atomic_skill_executor.go (1–87):** dispatches four management and four diagnosis categories; create trader special case; optional running-only filter. Outcome variant marks diagnosis success/goal-achieved unconditionally; other outcome inference depends on text and saved state. Workflow driver calls plain dispatcher, not outcome variant.
- **workflow.go (1–959):** `agent_workflow_session_<int64>` system-config persistence, dependency readiness, recursive sequential advancement, optional LLM decomposition plus broad keyword fallback. Task statuses pending/running/completed/failed, not transaction state. Most persistence errors are ignored. Successful task inference is absence of active skill state.
- **planner_runtime.go (1–4118):** execution/observation/reference lifecycle, context/snapshot switching, model-generated step execution, summary/fallback formatting, routing heuristics and legacy loop. Store user is explicit for management tool reads; conversation state is numeric-keyed. UTC RFC3339 persists timestamps; response prompt clock uses CT through kernel in agent.go.
- **skill_dag.go (1–291):** declarative collect/branch/confirm/execute metadata. `normalizeSkillDAG:257` trims/deduplicates lists but does not check edges/cycles. Dependency `skill_dag_runtime.go:5` defaults to first step; `advanceSkillDAGStep:38` selects first successor, so branch execution requires handler logic.
- **skill_runner.go (1–244):** confirmation policy comes from separate `SkillDefinition` action metadata, not DAG node kind. Unknown action resolves false for confirmation. `beginConfirmationIfNeeded:225` enters wait phase; `awaitingConfirmationButNotApproved:236` tests `isYesReply`. Target provenance formatting is explanatory, not authorization.
- **strategy_field_catalog.go (1–224):** combined/manual-per-type/agent-updatable literal field keys, separate grid and AI sets. Includes legacy crypto fields and no claim of exhaustive current futures configuration coverage. `user_facing_prompt.go:3`, `prompt_persona.go:8` and `skill_catalog.go:3` are prose/presentation boundaries, not execution guards.
- **stock.go (1–485):** crypto-independent stock quote facility, static name/ticker map then Sina search; HTTPS, pooled10s client, bounded256KiB response before GBK decoding, positional A/HK/US parsers. No source-date freshness check. Numeric parse failures default zero. Extended-hours marker is positive ext price, not session clock.
- **scheduler.go (1–128):** minute ticker, daily report in host-local21:xx once12h elapsed, risk check after4h (initial zero timestamp means first tick), pending cleanup at minute0. `Stop` uses sync.Once; Start does not prevent duplicate starts. Report aggregates account unrealized P&L, not corrected closed-trade P&L; risk alerts use pnl/(entry*size). `notifyAll:1087` calls optional callback with0; main excerpt does not wire notification callback, so availability of actual delivery is unverified.

### Findings, highest priority first

#### A1 — store authentication does not bind numeric conversation identity

[A source; severity A; runtime UNVERIFIED] Auth middleware supplies an owned string ID, but chat handlers preserve any nonzero JSON `user_id` (`web.go:102,144`). `history.go:34,52`, `workflow.go:41,77`, `agent.go:514` and planner state operations use that numeric identity without store identity. A caller choosing another session ID can therefore address its in-memory/persisted conversation namespace, including `/clear` before the flow lock. The normal derived key is deterministic FNV (`preferences.go:40`), not secret. This is a concrete static isolation gap; no account/session enumeration, production request or proof of observed disclosure was performed. API auth still exists; this is not an unauthenticated endpoint claim.

#### A2 — trade-history tool gathers all manager traders

[A source; severity A; runtime UNVERIFIED] `handleToolCall:919` routes history without store identity. `toolGetTradeHistory:3359` loops `GetAllTraders()` and reads each trader's closed rows. Thus other management tools' owner filtering does not extend to this history tool. `/status` separately gives global counts (`agent.go:879`). Chat history access can reach this tool through ordinary planner/legacy paths, even though the direct fast-path executor is dormant. Whether the deployment has more than one owner/trader population was not read.

#### A3 — model selection is shared mutable state across users/turns

[A source, B concurrency consequence; severity A; runtime UNVERIFIED] `Agent.aiClient` is one field; `ensureAIClientForStoreUser:110` replaces/clears it for every message before `thinkAndAct`'s per-user mutex. Different users hold different mutexes, and even same-user requests can select a client before waiting for that mutex. Model calls throughout router/planner dereference the shared field again. Background maintenance also reads it. Possible consequences include another user's selected provider/credentials handling a turn or nil/client races. No race test run; no observed data egress claimed. The fallback to default user's model is deliberate code behavior but its tenancy policy remains unverified.

#### A4 — tool failure may be narrated as completion

[A source; severity A for integrity, runtime UNVERIFIED] `executePlan:3105–3120` sets tool step completed for every returned string, including `{"error":...}`. `deterministicCompletedPlanResponse:3218` and `formatCompletedPlanFallback:3861` use completed status/title only. `tryExecutionSummaryFallbackOnAIError:3885` can mark entire state completed after a provider failure when any tool step is completed. A failed management tool followed by completion-only response therefore has a static false-success path. No injected tool result/reproducer executed. Lower-level operation returning failure is preserved in raw observation but not consulted by deterministic completion. `RequiresConfirmation` also is not enforced at this executor seam; lower tools must guard each action. Read `toolStartTrader:2547` / `toolStopTrader:2588`: they enforce owned config but do not inspect confirmation. This identifies split confirmation surfaces, not a tested bypass of every skill handler.

#### A5 — blocked workflows are treated as finished; strategy-create can drop requested work

[A source; severity B; runtime UNVERIFIED] `normalizeWorkflowDecomposition:566` retains unchecked dependencies and duplicate IDs while dropping unsupported tasks. `nextRunnableWorkflowTask:129` returns false for cycles/dangling/failed dependencies as well as complete workflows. `maybeAdvanceWorkflow:297–312` then clears workflow and produces completion text. Separate path `executeUnifiedSkillTasks:371` selects **any first strategy-create** task before multi-task workflow creation, discarding the rest and its dependency order. Parser test `TestParseUnifiedTurnDecisionAcceptsSkillTaskList` proves the two tasks parse, not that both execute. No malformed workflow was submitted.

#### B1 — stale prose and schema branches disagree with actual code

[A] Legacy prompt `agent.go:550–734` says index futures have no data source and execution is crypto/US stocks. SYSTEM-MAP describes NT8 as live futures path; these are different surfaces and the chat prompt is misleading about product capability. Package header promises all messages through LLM/no pattern matching, but direct commands and broad keyword heuristics remain. Chinese skill catalog says enabled model needs custom URL/API key, while registry/environment default logic can resolve missing values; catalog also discusses live-money starts despite SIM-only owner law. None of this proves a live trade route.

[A] `allowedFieldSpecsForSkillSession:226–239` offers exchange wallet/Aster/Lighter fields, but `applyLLMExtractionToSkillSession:535–539` persists only exchange type/name/key/secret/passphrase/testnet/enabled/update_field. Allowed extraction can thus be silently lost at application. Missing-slot rules require model API key/exchange credentials even where DAG create metadata only requires provider/exchange type. Multiple vocabularies need boundary-specific tests; this report does not assert all are currently invoked in every flow.

[A] Registered model creation returns at `agent.go:188`; `mcp.ApplyThinking` is only below that return, duplicated at `:207–208`. `mcp/interface.go:45` establishes optional tuning contract. Per-row thinking customization is therefore not applied by this entry's registered-client branch, even though env/provider defaults may still work.

#### B2 — truth/freshness losses at formatting and fallback

[A] `queryPositionsDirect:997` skips every failed broker read and can return no-positions. `queryBalancesDirect:1047` can return only heading after all failures. `summarizeObservation:3825` slices1,000 bytes; final compact summary slices800 bytes (`:3751`), potentially splitting UTF-8/JSON and omitting result/refusal evidence. `buildRecentlyFetchedData:3267` treats error results as fetched and dedup filter suppresses identical tool calls for60s including mutations, with no explicit user-refresh exception.

[A] Strict history builder (`tools.go:3415`) keeps unresolved row P&L null and excludes it from summary; fixture IDs1–4 explicitly test that law. Dormant direct formatter `planner_runtime.go:813–815` passes per-row null through `toFloat` and displays0. This is a source formatter regression with **unproven production reachability**, not a confirmed UI regression. Builder summary aggregates per-trader limited input before global display limit, so summary population can exceed displayed list. No DB row evidence read.

[A] Stock local resolution uses substring matches over Go maps (`stock.go:85`): multiple names or overlap such as NIO aliases can select nondeterministically. Quotes are labelled real-time with no stale-time enforcement. Extended-hours detection and AM label are heuristic. Scheduler risk percent omits futures contract multiplier; it is only a notification, never a trading risk gate.

#### B3 — lifetime, persistence and sensitive context limits

[A] Entry logs complete user text (`agent.go:439,487`) before setup parsing; this can include credentials users submit. Router active-task prompt serializes collected fields (`llm_skill_router.go:433`) and other classifiers serialize session fields; secrets may be sent to configured model as context. No logs or actual credentials were inspected.

[A] Snapshot restore ignores persistence errors (`planner_runtime.go:2020`), and most workflow save/clear errors disappear (`workflow.go:92`). Lone confirmation-snapshot restore (`:2158`) does not remove snapshot, unlike ID/domain restoration; later repeat behavior untested. Async maintenance occurs outside flow lock, so a subsequent turn or `/clear` can overlap stale maintenance writes; code-level concern only. Router confidence zero is accepted (`llm_skill_router.go:104`), unlike positive values below.45; negative values normalize to zero and share that bypass.

Adjacent trade dependency (handed to parent for owning worker): pending creation authenticates session in `tools.go:2731`, but TradeAction creation and `trade.go:438` confirmation do not bind supplied numeric user or store owner in the inspected functions. Downstream `executeTrade`/resolver/SIM gate not fully read here, so no end-to-end trading authorization conclusion is claimed.

### Tests and verification limits

[A] Fully read `workflow_test.go` (lexical split/classification/simple chain), `skill_registry_test.go` (definition/slot/confirmation metadata), `skill_dag_runtime_test.go`, `unified_turn_router_test.go`, `planner_runtime_state_test.go`, `pnl_truth_tool_test.go`. Also read model-selection fixture and canonical extraction tests listed in reads ledger. No test command executed to avoid confusing source review with runtime verification; parent owns combined test plan.

[A] `skill_dag_runtime_test.go:17` skips advancement test as fork API mismatch. `planner_runtime_state_test.go` skips read-fast-path, dynamic config refresh, several routing/continuation scenarios, older conversation-memory assertion and timeout test. `TestShouldAttemptReplan:334` only tests helper predicate, while production executor does not call it. Active parser/continuation tests and corrected-P&L builder test are useful but do not cover session ownership, concurrent client selection, invalid workflow graph execution or error-as-completion path. Test names are not proof of execution or relevance.

### Historical graph comparison

All88 matching historical node summaries were read, plus first50 of285 incident edge objects; not all historical edges manually verified. `graph.json` states this coverage and lists corrections. Historical graph July10@7a8adce0 is **not current source**. It incorrectly describes per-user AI clients, Tencent quote/search endpoints, 08:00 report/hourly risk, and executePlan-triggered replanning. Current code says one shared client, Sina,21 local/>4h, and ReAct next-step execution. It also says history cleanup drops individual old messages and implies graph-transition validation/loose field types that code lacks. Package import expansion includes test files as import targets; those are not runtime file-level calls. Root CGC export pending at review time; no direct service query/reindex performed by this worker.

This slice explains the chat orchestration surface. It does not certify the futures day-plan engine, broker wire, live SIM state, entire management handler implementation, or all graph/runtime behavior.

---

<a id="section-9"></a>

> Original: [reviews/04/report.md](reviews/04/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 04 — conversational agent execution and tools

Base: `63968be62e44db2fb07a92883e02127b9064b0be`. Isolated worktree: `/tmp/vl-understanding-execution-20260913`. Initial `pwd`, HEAD and empty porcelain status verified. Read-only source review: **14/14 assigned files, 9,223 lines, 281 named functions/methods manually read**. `reads.json` records complete assigned-file hashes and additional excerpt boundaries; `functions.json` records every named declaration, semantic purpose, exact start/end lines and syntax-only calls. Anonymous ticker, range, sort and goroutine callbacks are covered with their enclosing functions. No source, runtime, database, account or environment changes; no tests or exploit requests executed. Findings below are **[A] directly verified static source facts**, with consequences distinguished from demonstrated execution. No incident is claimed.

Operating references read: root AGENTS instruction, tracked `docs/superpowers/CLAUDE-canon.md`, `AUDIT-CHECKLIST.md` classes and pre-audit R1–R10, relevant SYSTEM-MAP execution/EntryGate section and RULEBOOK risk section. Latest spec history at this base:

- AUDIT-CHECKLIST: `dfda15e1 test: isolate session clock fixtures from weekly backfill workers`
- SYSTEM-MAP: `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap`
- VL-TRADING-RULEBOOK-v1: `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap`

These are reference documents, not runtime verification. The owner's daily-loss correction is retained; this review does not recommend restoring a mandatory per-trade cap.

### End-to-end boundaries

**Chat identity and entry.** `api/agent_routes.go:11–35` authenticates chat/SSE, attaches store user and authenticated/trade-capable policy. `agent/web.go:81–120` derives numeric chat identity only if request `user_id` is zero, then forwards the store user separately. `agent/agent.go:429–460` and `:476–510` process confirmation text before the planner. Authentication therefore exists at HTTP entry, but store-user ownership and numeric memory identity are separate contracts. The broader consequence of request-selected chat identity belongs with assignment 03; active/session/reference storage in this slice relies on that numeric identity.

**Management route.** `tryMinimalBrain` (`central_brain.go:31–64`) builds routing context from saved active task, history, references and pending hint, calls AI with stage timeout, parses the four route enums, then enters `executeBrainDecision:213–267`. `driveActiveSession:269–440` plans a step, merges filtered fields, enforces strategy-specific readiness/confirmation and calls `executeActiveSkillSession:921–938`. The bridge temporarily saves legacy `skillSession`, invokes `dispatchBridgedSkillSession`, reads continuation state, clears temporary state and classifies the result. `handleCreateTraderSkill` (`skill_dispatcher.go:434–504`) hydrates owner-scoped binding options, validates those bindings, asks for required fields and saves an explicit create/start confirmation phase. `executeCreateTraderSkill:640–720` calls creation and optionally start tools and formats their actual responses.

**Tool boundary.** `buildAgentTools` (`tools.go:473–878`) defines schemas; `plannerToolNamesForDomain:84–112` limits advertised tool groups. `handleToolCall:881–930` dispatches actual names. `executePlanTool` (`planner_runtime.go:3657–3672` in inspected excerpt) and the legacy fallback `:4077` reach that dispatcher. The legacy fallback exposes the full catalog for general-domain messages (`:3986–3989`). This proves a callable path, not that an arbitrary user utterance reliably makes the LLM choose a particular tool. Advertised schema text is not an authorization gate.

**Mutation and runtime.** Owner-scoped model/exchange/strategy store access is generally present. `traderBindingValidator.Validate` (`config_validation.go:123–172`) requires an accessible enabled usable model, accessible enabled credential-complete exchange, and optionally accessible strategy. Create/update trader tools call it. Strategy writes use `MergeStrategyConfig`, `ClampLimits`, locked-key checks and explicit create `confirmed`; successful strategy updates only persist the strategy (`tools.go:2140`), without a bound runtime trader reload in this path. Trader update persists, removes the in-memory trader and best-effort reloads (`:2497–2503`). Start launches `Run` asynchronously then persists/returns started (`:2573–2584`), which is not evidence of a successful runtime boot.

**Direct trade.** `toolExecuteTrade` (`tools.go:2731–2837`) checks authenticated policy, trade permission and server flag, normalizes stock/crypto symbol, validates action/quantity, resolves a global running trader, validates estimated price/equity/notional and stores a pending pointer. `handleTradeConfirmation` (`trade.go:438–520`) retrieves that pointer and invokes `executeTrade:175–210`. Execution resolves again, validates again, then calls raw underlying broker OpenLong/OpenShort/CloseLong/CloseShort. It does not call AutoTrader's decision order path or EntryGate here. **Important refusals:** `isStockSymbol` treats normal `MNQ`/`NQ` as stock; resolution requires Alpaca for these symbols, so ordinary MNQ text does not establish an NT8 execution path. TCP entry independently refuses unbound/non-SIM accounts (`tcp_trader.go:324–339`), optionally deduplicates/rate-limits (`:341–359`), and requires cached nonzero SL/TP for requested symbol+side (`:366–375`). Do not convert this static alternate-path concern into a claim that normal MNQ trades or unprotected live orders were executed.

### Priority static findings

#### 1. High — foreign stopped-trader deletion has unscoped side effects

`toolDeleteTrader` (`tools.go:2512–2545`) rejects running in-memory/state rows, but ignores errors from owner-scoped `GetFullConfig` and does not require the target in the caller's list. It calls `TraderStore.Delete(user,id)`, whose first statement deletes `EquitySnapshot` rows **only by trader_id** (`store/trader.go:223–228`), before owner-scoped trader deletion. That scoped delete returns the database error, not a rows-affected ownership failure. Afterwards the tool calls global `RemoveTrader(id)` (`tools.go:2537`); `manager/trader_manager.go:359–373` stops if running and removes that global ID.

Static preconditions: dispatcher reaches this tool with a known foreign trader ID; target is not running at the initial checks. Consequences are foreign equity-history deletion and in-memory removal even though foreign trader-row deletion remains owner-scoped. Existing running-trader refusal is a real mitigating predicate and is preserved in this conclusion. A running-state race was not reproduced. This concerns a stopped foreign target, not proof that every foreign target can be deleted.

#### 2. High — direct trade confirmation is not owner-, policy-, expiry- or atomically bound

`TradeAction` (`trade.go:39–52`) has no owner; `resolveTradeExecutionContext:212–247` enumerates `GetAllTraders`, ignores even the existing TraderID field and picks first matching running broker. Manager's snapshot is an unordered Go map (`manager/trader_manager.go:68–77`), so preview/confirmation can choose different compatible traders. `handleTradeConfirmation:438` never uses its userID argument or session policy and does not repeat the tool's server trade flag check. `Get:70–74` and `Remove:76–80` are separately locked, allowing two callers to retrieve the same pointer before either removes it. No consumed/status check blocks duplicate execution at this layer.

Expiry wording says five minutes (`tools.go:2830`), but confirmation never checks CreatedAt. Cleanup occurs upon creating another intent and at the scheduler's hourly boundary (`scheduler.go:48–52`); absent a new creation an expired intent can remain until that sweep. Large-order confirmation checks the flag **before** revalidation; revalidation can set it after price/equity changes without a second large-confirmation check. Static concern requires an existing pending intent and a reachable confirmation request; no pending IDs or account state were inspected. NT8 refusals above substantially constrain the normal MNQ path and broker dedup may constrain duplicate entries. Those do not supply missing chat ownership.

#### 3. High — history gathers all users' loaded traders

`handleToolCall:920` passes no store user to `toolGetTradeHistory:3359–3401`. It iterates `GetAllTraders` (`:3384`) and retrieves each trader's closed positions. Payload includes trader names, truncated IDs, position IDs, quantities, entry/exit prices and P&L. Unlike balance and positions tools, there is no owner-scoped trader list. Static disclosure concern requires multiple users' traders loaded and history tool execution; no private rows were queried.

#### 4. High correctness — positions adapter discards real open positions

`toolGetPositions` (`tools.go:2856–2867`) calls **AutoTrader.GetPositions**, reads `p["size"]`, then drops rows where that becomes zero. Actual producer `trader/auto_trader_decision.go:234–289` returns `quantity`, `entry_price`, `mark_price`, `unrealized_pnl`, not `size` or the camelCase keys the tool also reads (`tools.go:2879–2881`). Thus every normal successful producer row is skipped, and the tool says `no open positions`. This is direct producer/consumer source evidence, not a mock shaped to the consumer. Independently, failed/unloaded traders are silently skipped, also collapsing unknown into empty.

`toolGetBalance:2930` similarly reads `used_margin`; its producer emits `margin_used` (`auto_trader_decision.go:229`). **Negative result:** `available_balance` and `total_equity` do match and are not flagged. Shared-account duplicate balance aggregation was not traced beyond this adapter.

#### 5. Medium — model edits clear unrelated thinking overrides and create overwrites provider primary

`toolManageModelConfig` parses thinking fields as plain strings (`tools.go:1758–1766`) and unconditionally calls `UpdateThinking` after every update (`:1922`). Omitted fields become empty. `store/ai_model.go:400–425` explicitly writes empties to mean inherit. Renaming or changing a key/model can silently reset an existing per-model override. `safeModelForTool:1146–1167` also fails to populate the declared ThinkingMode/ReasoningEffort output fields, reducing observability.

Create finds a provider primary (`:1823`), replaces the requested model ID with that existing ID (`:1828–1830`), then calls `UpdateWithName`; this is an upsert of existing credentials/name, not always a distinct creation. Owner scope exists; this is unintended-scope/behavior risk within that owner. No existing credentials were read or mutated.

#### 6. Medium — diagnostics have broader log scope and false empty tails

`toolGetBackendLogs` resolves an owned trader, but `readBackendLogEntries` trims the latest **global** matching lines before `filterBackendLogEntriesAny` applies trader ID/name (`tools.go:1377–1381`). An owned trader's recent errors can be excluded by other traders' newer errors. Name substring matching is not a unique ownership boundary. More directly, `backendLogDiagnosisExcerpt` (`skill_dispatcher.go:840–858`) has no owner parameter and falls back to generic `model`/`exchange` log text; model/exchange diagnosis call it. This is static potential disclosure of operational lines, not proof that logs contain secrets.

#### 7. Medium — history summary and returned-row population differ

`buildTradeHistory` accumulates wins/losses/P&L/unresolved across all input rows before sorting and limiting the displayed list (`tools.go:3420–3474`). With two source traders each fetching limit N, summary can describe up to 2N rows while response shows N. Output does not label that larger population. Corrected-column semantics are otherwise sound: unresolved rows carry null and are excluded from summary, test-seam rows are skipped, row IDs are present. Sorting formatted CT time strings can misorder across the repeated DST hour; not reproduced.

#### 8. Medium — execution/management state claims can outrun effects

`driveActiveSession:418–435` asks a task supervisor to choose complete/replan but returns a reply and clears active state even for `replan`. It does not actually replan here. `inferSkillOutcome` (`skill_outcome.go:83–108`) treats any nonempty no-session reply without `失败`/`failed`/`error` as success; deterministic management reply trust therefore depends on wording. `toolStartTrader` returns started before `Run` success and ignores status write failure; update ignores reload failure. These are outcome/reporting risks, not proof of currently broken traders.

#### Additional bounded observations

- Exchange tool parses `Enabled` but create/update hardcode true (`tools.go:1493,1588`); store Update also hardcodes true. Current tests contain a skipped lifecycle test that expects disabling (`config_tools_test.go:24–80`). This may reflect a broader product rule; the mismatch is between exposed schema and actual behavior, not an instruction to re-enable disablement.
- Exchange update writes settings before duplicate account-name validation (`tools.go:1658–1685`), allowing partial change on rename failure. NT8 empty update arguments **preserve** existing NT8 fields (`store/exchange.go:315–323`); the feared erase-by-empty is not present.
- Agent initial-balance NT8 probe constructs deprecated CSV trader (`tools.go:1109–1113`), while project live architecture is TCP. This is a creation/probe disagreement, not live source rewiring.
- Strategy tool update writes DB without live reload; stored success is not evidence of active strategy adoption. Delete may activate or create a fallback strategy (`tools.go:2159–2203`); active flag is legacy and must not be confused with trader binding.
- Active fields/history are inserted verbatim into prompts (`central_brain.go:129–131,802–806`); config workflows can collect credentials. Confirming actual credential retention/redaction in broader handlers is a follow-up, not proven here. Safe strategy output returns full parsed config and does not apply the small sensitive-key stripper.
- Snapshot stack has a five-task/24-hour bound; dynamic snapshots do not have a count bound. Active skill/proposal sessions have no TTL. Save errors are often ignored and read-modify-write stack operations are not atomic. Full request serialization was not established.
- `mergeExtractedData` replaces each map value, so a new config_patch replaces prior patch rather than recursively merging it; later legacy/config machinery may compensate, not traced completely.
- `findOptionByIDOrName` returns first duplicate exact name although separate ambiguity renderers exist; unique substring path does refuse multiple matches.
- AI/chat template deliberately lists daily_loss_limit_pct as non-field (`central_brain.go:1191–1197`, domain primer). This is a limited legacy AI editor schema, not authoritative futures daily-risk policy.

### File coverage and data/time semantics

All fourteen assigned files are accounted for in `reads.json`. The functions inventory covers: active task/proposal persistence and slot constraints; proactive news/brief workers; central LLM routing and strategy repair/confirmation; model/exchange/trader validators; execution state/log/reference normalization and snapshot stack; wallet fast path; prompt assembly; durable reference memory; legacy trader-create/diagnosis/target dispatch; bilingual skill-domain primers; outcome classification/review; strategy-name heuristics; full tool schemas/CRUD/market/account adapters; and pending direct-trade execution.

Persistence keys use numeric chat ID for sessions/references/preferences, while resource stores generally use authenticated string storeUserID. Timestamps are mostly UTC RFC3339 for stored chat state, Unix seconds for trade expiry, Unix milliseconds for position history. News/brief/scheduler trigger hours use machine-local `time.Now`, whereas displayed brief/history timestamps use kernel CT formatter. Normalization sometimes fills missing timestamps with now; this is structural fallback, not observed event provenance. Background market HTTP often uses its own background timeout rather than the incoming request's cancellation. No external HTTP requests were made for this review.

### Tests, historical graph and limits

Manually read the complete `tools_test.go`, `pnl_truth_tool_test.go`, and `planner_tools_test.go`, plus precise portions of `config_tools_test.go` and `trader_scope_test.go`. The stock table covers crypto and equities but no MNQ/NQ. P&L fixture IDs 1–4 prove corrected/null/test-seam shape in its source assertions; one source and nonbinding limit do not test ownership or summary truncation. Running-trader delete test only covers same-owner running refusal. Exchange lifecycle is explicitly skipped. No trade confirmation tests were located by symbol search; this is a search result, not proof none exist under indirect names. No tests were run or added.

Historical Understand Anything graph: July 10 at `7a8adce0`, not this source base. Inspected fourteen file nodes, counted 176 matching nodes and 328 outgoing non-contains edges, and read the first twenty outgoing edges. Sample imports from tools.go point to individual kernel/mcp test files: these are package-level historical associations, not resolved production calls. Historical statement that execution normalization bounds every persisted structure is too broad: DynamicSnapshots is not bounded. Historical pending-expiry prose does not show the missing confirmation-time TTL check. `graph.json` separates this sample and corrections from directly read current call boundaries. Root-provided AST calls are explicitly syntax-only and not a type-resolved graph; no CGC reindex/service mutation was attempted.

Unresolved: full downstream store/security/provider validation; all planner eligibility/confirmation branches; deployed binary/AddOn/account state; live database rows; per-user concurrency serialization; actual exploitability or broker effects; every historical function node/edge; full test execution. Source coverage is complete for this assignment only. Root owns authorized reproductions and repairs.

---

<a id="section-10"></a>

> Original: [reviews/05/report.md](reviews/05/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 05 — conversational agent management and HTTP boundaries

Base: `63968be62e44db2fb07a92883e02127b9064b0be`. Read-only isolated worktree: `/tmp/vl-understanding-market-20260913`. Assignment: 14 source files, **9,224 lines fully manually read**, **299 named functions/methods** inventoried. Anonymous callbacks are grouped under their enclosing function. No assigned source gaps. Additional dependency coverage is explicitly partial in `reads.json`; this is not a whole-agent or whole-repository certification.

Evidence notation: **[A]** directly read source/control flow; **[B]** consequence inferred from that source; **[C]** unresolved hypothesis. No test, exploit, trade execution, live API request, or DB mutation was performed. Findings below are static concerns, not reproduced runtime incidents. Source hashes were verified again while assembling artifacts.

### Rules and provenance

Read repository AGENTS instructions, tracked `docs/superpowers/CLAUDE-canon.md`, audit checklist excerpts (1–160, 2281–2310), SYSTEM-MAP 329–359 and VL-TRADING-RULEBOOK-v1 1–65. These are an audit reference, not an implementation spec. Recorded source-history lines from the reading:

- CLAUDE-canon: `07b53e65 cleanup batch 1 — B5 the mutation harness that cannot fake a verdict, B6 the worktree-add check`.
- AUDIT-CHECKLIST: `dfda15e1 test: isolate session clock fixtures from weekly backfill workers`.
- SYSTEM-MAP and VL-TRADING-RULEBOOK-v1: `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap`.

NT8 remains the single live futures data/execution route; SIM-only and immutable owner account/credential bindings remain governing instructions. Crypto-era USDT helpers in this slice are not justification to add mandatory per-trade caps to futures. No source changes are proposed or performed by this worker.

### Actual flow and boundaries

**HTTP identity and conversation state.** `api/agent_routes.go:11–42` authenticates chat routes and attaches the real store owner and trade policy. `web.go:82–188` parses messages, applies 55s/120s request deadlines and forwards the real owner plus a numeric conversation ID. Nonzero body `user_id` survives; only zero derives `SessionUserIDFromKey(owner)`. `agent.go:428–525` keeps these identities separate: store owner selects AI/store scope, numeric ID selects history, task state and skill/proposal/workflow state. `/clear` clears those numeric-keyed states. Raw incoming text is logged at `agent.go:439,487`, before the planner; catalog/onboarding prompts can solicit credentials, so logging policy/redaction needs a separate reviewed boundary.

**Skill routing and session management.** `skill_registry.go:59–177` loads embedded JSON; malformed JSON panics at initialization, duplicate skill names overwrite, and normalization is chiefly trimming/map preparation, not complete semantic validation. Registry context builders `:193–721` construct cached planner guidance, action contracts and field descriptions. This is not tool-side authorization. `skill_semantic_gate.go:10–246` renders fields/options and looks up owner-scoped strategy type; malformed config falls back toward AI. Option load failure and an actually empty list may both render no options. `skill_dag_runtime.go:5–50` resolves a missing/bad cursor to first step and follows only first successor; terminal advance leaves the cursor unchanged.

`skill_management_handlers.go:28–128` normalizes aliases and mirrors trader fields into old slots. Empty setters do nothing, so they cannot clear a stale value. `:203–226` refreshes target references by ID then same-name fallback; this is owner-scoped but not immutable identity binding. `:248–333` loads domain options and routes creation or simple management. `:2528–2638` resolves targets, supports bulk delete and dispatches to `skill_execution_handlers.go:1466–2473`. Lifecycle/delete paths generally require confirmation; update paths build/validate sparse payloads and delegate to owner-scoped tools. Owner checks in those tools are not fully audited here. Negative result: `loadEnabledModelOptions` (`skill_dispatcher.go:295–317`) actually includes disabled models, so a suspected inability to select a disabled model is **not supported**.

**Strategy creation/update.** Creation builds normalized draft/type/nested patch configuration (`skill_management_handlers.go:335–538`), strips protected risk fields, checks explicit required fields (`:567–766`) and presents summaries (`:768–1455`). AI/grid requirements differ; grid needs ten fields plus ATR or manual price bounds. Presence checks establish a path was supplied, not that its value is valid. Concrete creation occurs in `:2377–2457`, ultimately calling `toolManageStrategy` with `confirmed:true`. Config updates (`skill_execution_handlers.go:2529–2717`) load existing owner config, merge/clamp proposed patch and defer warnings for confirmation. Deferred state contains a **full config snapshot**; later persistence has no revision compare, so a concurrent config update may be overwritten [B]. Existing malformed config JSON is ignored in `loadStrategyConfigForUpdate:2631` rather than surfaced as a decoding error. Actual store merger/tool validation was not fully followed. The legacy `applyStrategyConfigPatch:830–1072` is a separate typed-field switch with parse checks and locked-field refusals, not the current nested merge validator.

**Persistent memory/preferences.** `memory.go:82–289` stores task state under numeric session ID, trims/deduplicates and heuristically filters open loops; it does not guarantee all model-generated facts are grounded or all secret-bearing fields are filtered. Missing timestamps are filled during normalization. Compression `:317–349` summarizes old history only beyond message/token thresholds, saves state first and then replaces history with recent messages. Failed summary/save retains old history. Incremental update `:350–483` uses a short window. Concurrent caller serialization around replace/read-modify-write was not established. Token estimation is rune/3 plus overhead, not model tokenizer output. Environment integer overrides accept negatives; cap snapshot can say “set” when invalid text caused fallback.

`preferences.go:21–166` validates additions (nonempty, ≤500 runes), prepends entries, caps count at 20, updates/deletes the **first** ID or case-insensitive substring match, and emits prompt bullets. Update does not repeat the 500-rune check [A]. JSON failures look like absent preferences. Read/modify/write has no local atomic transaction or mutex; caller serialization is unverified.

**Catalogs, onboarding and localization.** `entity_field_catalog.go:1–111` defines manual versus agent-editable fields/aliases; it is capability metadata, not validation. `i18n.go:80–90` and `onboard.go:599–607` provide localized templates. `model_provider_catalog.go:21–242` is a static eleven-provider catalog with defaults/custom URL/custom model flags, API-key/wallet guidance and a recommended provider. No remote provider claims were verified. Legacy wizard `onboard.go:108–358` uses numeric session state, seven crypto exchanges and independently maintained older AI defaults; direct Chinese conversation goes through planner rather than setup command recognition. Secret values are kept in memory while step metadata persists, so a restart can resume a step without its credential data. `needsSetup:37` checks global traders, not owner traders; current entrypoint reachability was not demonstrated. `saveSetupExchange:432–482` can update an existing same-owner/default-name exchange and force enabled/mainnet config; `saveSetupAIModel:484–508` updates owner/provider model. `createTraderFromSetupForStoreUser:364–430` reuses a matching binding or creates a stopped trader without strategy. No such writes were executed.

**Market display and background watcher.** `web.go:207–366` public market proxies always target Binance futures; symbol/interval shape, limits, body bounds and timeouts are present. Batch ticker is capped at 20, concurrently fetched, output order retained for successes while failed entries disappear. It does not merge stock data or read NT8. `HandleHealth:77` is static “ok,” not a bridge/data liveness probe. `stream_text.go:5–49` partitions output into callback chunks; SSE encoding uses JSON string escaping (`web.go:191–205`) and has no heartbeat producer.

`sentinel.go:54–223` is an independent Binance watcher, not live NT8 decision input. Start is not once-guarded; Stop closes once; Add/Remove compare exact symbol case; removed-symbol history remains. At least five observations compare current to `h[len(h)-5]`, spanning four sample intervals, while the alert says five minutes. Sequential scan/network delay further means no exact duration guarantee. Volume compares rolling 24h quote-volume samples, not per-minute traded volume. ParseFloat errors are ignored; no finite/positive input check is present. Alerts have no dedup/throttle. Funding alert enum exists but no funding alert is emitted here.

**Diagnosis.** `skill_execution_handlers.go:2789–3199` resolves an owned trader, collects safe entity metadata, runtime status/account/positions, five recent decisions and thirty logs, then asks AI under a bounded timeout or formats fallback. The reduced decision evidence loses row IDs, so diagnosis output cannot satisfy the sample-ID law from this representation alone. Fallback uses persisted TraderConfig.IsRunning rather than collected runtime flag and prioritizes text-pattern historical errors; a stale error may misdescribe a later waiting/healthy decision [B]. Account failures can become empty evidence; USDT regex and candidate-first symbol selection are crypto-oriented. These are static evidence-quality limitations, not an observed false diagnosis.

### Findings prioritized for root

#### R05-1 — high: authenticated owner and mutable numeric session identity diverge

**[A]** `web.go:102–104,144–146` only derives identity when body `user_id==0`. `api/agent_routes.go:13–35` adds owner context but never rewrites that body field. `agent.go:428–525` passes supplied numeric ID into conversation state and clear operations. `memory.go:82–123` and `preferences.go:53–146` use that numeric key for durable state.

**[B]** A caller able to address another numeric session key can cross its conversation state boundary, including clearing state via `/clear` and potentially reading its context through normal responses. This does **not** establish cross-owner CRUD access: actual management tool calls continue receiving authenticated `storeUserID`. The route requires authentication; this is not an unauthenticated route finding. No running server or other user's state was accessed. A safe root-owned regression would assert body IDs cannot change authenticated session scope in both HTTP variants.

#### R05-2 — high: direct trade confirmation is not bound to owner/policy/selected trader

Additional dependency trace, reported to coordinate with agent/03:

- `trade.go:40–55` TradeAction has TraderID but no owner; pending map `:57–91` is keyed only by trade ID.
- `handleTradeConfirmation:438–520` accepts text command ID, reads pending item, enforces large-order wording, removes it and calls execute. Its `userID` argument is unused; it does not recheck session policy/server flag or item age.
- `agent.go:452,499` calls this before planner routing.
- `executeTrade:175–210` revalidates trade numbers/risk and calls the broker. **Those safeguards are real and not bypassed by this finding.** `resolveTradeExecutionContext:212–248` chooses the first running manager trader matching stock/nonstock classification, without owner or TradeAction.TraderID binding.
- In contrast, proposal tool `tools.go:2731–2738` checks authenticated session and AllowTradeExecution. That protection is at proposal time, not confirmation time.

**[B]** Knowledge of a pending ID can suffice at this local handler to confirm someone else's intent; manager ordering may select a different eligible trader. Separate Get/Remove locks also leave concurrent confirmation check-and-consume non-atomic. No double execution or cross-owner execution was reproduced. Expiration cleanup exists (`CleanExpired`), so lack of age check is a stale-window concern; cleanup schedule is not established by these excerpts. Downstream NinjaTrader SIM/order guards remain outside this local trace and must not be weakened. Main trade implementation outside quoted ranges remains an explicit dependency gap.

#### R05-3 — medium/high: pending prompt flag substitutes for current consent

**[A]** `handleStrategyCreateSkill:2428–2450` only asks if **both** current text is not affirmative **and** awaiting-final-confirmation flag is false. Once the flag is true, execution writes with `confirmed:true` regardless of current text. Explicit cancellation is handled earlier and does refuse. Upstream active route `central_brain.go:362–395` consults `guardStrategyCreateBeforeFinalConfirmation`, but that guard returns no block when the flag and prior prompt exist (`:631–638`).

**[B]** If planner routes a nonconsenting follow-up to execute, handler does not independently require affirmative current-turn consent. This is a **conditional static path**, not a demonstrated natural-language exploit; normal planner routing can ask or cancel instead. Fix/test ownership stays with root.

#### R05-4 — medium: English creation instruction collides with trade command

**[A]** strategy summary `skill_management_handlers.go:1191` instructs `confirm create`; `strategyCreateConfirmationReply:540–553` accepts yes/ok/Chinese confirmations but not that phrase. More directly, `handleTradeConfirmation:441–465` parses any `CONFIRM <token>` first; with a nonnil pending store it treats “create” as trade ID and returns “Trade expired or not found.” `agent.go:452,499` intercepts before planner. Thus the recommended English phrase reaches the wrong handler under the nonnil-pending-store condition. No HTTP reproduction performed. A pure-agent regression can test recommended text against both interceptors without trading.

#### Other bounded concerns and negative results

- Generated prompt update confirmation (`executeStrategyPromptUpdate:2475–2527`) also falls through on unrecognized reply when its flag is set, but no production setter of `_requires_generated_confirmation` was found. Treat as dormant/helper issue until reachability is proven.
- Bulk deletion asks about current aggregate set and re-enumerates eligible stopped owned traders on execution (`:1801–1900`); it does not bind approval to immutable IDs. Not a claim that running traders are deleted.
- `normalizeCoinSymbol:1125` turns MNQ into MNQUSDT and uppercases continuous futures suffixes. This helper is crypto-specific; current production futures reachability was not established and it must not be cited as a proven live MNQ failure.
- `parseLooseTextValue:228` and `parseStandaloneTraderUpdateArgs:253` are empty stubs; don't document them as active extraction.
- Unsupported custom endpoint clearing calls `setField(..., "")`, which ignores empty values. A stale custom endpoint can remain in model creation state; downstream validation may reject it.
- Exchange creation initially demands API key and secret for every exchange (`:2127`), despite provider-specific credential alternatives. NT8 onboarding readiness is not established by this crypto-oriented path.
- `formatFieldKnowledgeLine:596` renders numeric min/max with `%.0f`; fractional constraints lose precision in planner guidance. Ordered field fallback uses map iteration, not stable sorting.

### Tests read, not run

- `preferences_test.go` (31 lines): constructor empty/trim cases; no update-length or ambiguous-match regression.
- `onboard_test.go` (27): direct setup command test skipped and contains stale Chinese expectations.
- `model_provider_catalog_test.go` (57): three provider guidance string tests.
- `skill_registry_test.go` (55): registry/action requirement loading.
- `memory_test.go` (134): skipped old compression expectations, active incremental-summary/temp-store and normalization tests. No live DB was opened.

No broad suite was run merely to imply verification. HTTP owner binding, pending confirmation policy/atomicity, current-turn strategy consent, and recommended English confirmation deserve focused root-owned tests. Existing tests outside these five files are not represented as reviewed or absent.

### Historical graph correction and explicit gaps

Historical Understand Anything graph is July10@7a8adce0, 3,121 nodes / 9,588 edges; its selected summaries were compared to current source. `graph.json` captures selected historical nodes and incident edges as **unverified historical material**, plus source-verified corrections. It is not current type-resolved call graph. Root AST syntactic calls are labeled syntax-only in every function record. CGC was not independently queried by this worker; root coordinates the historical export.

Corrections: preferences update has no creation length check, delete has no index fallback, add prepends and context is bullets; Sentinel Add/Remove return no bool and funding emission is absent; DAG terminal cursor is retained; registry fallback ordering is not always stable; applyStrategyConfigPatch does not clamp; generic action executors generally delegate creation elsewhere; web batch has no stock merge and SSE no heartbeat.

Explicit gaps: full planner/central-brain/tool authorization implementations, full trade.go (151–174 and 291–349 not read), store merger and transactional semantics, manager-to-broker selection ownership, runtime session serialization, current deployed binary behavior, frontend request construction, public proxy consumers, upstream provider availability, root-owned repair validation. Assigned source coverage is complete. No runtime incident is claimed from static gaps. Historical graph edge data exported here must not be mistaken for manually resolved current dependencies.

Graph label follow-up: all 226 selected non-containment incident edges were inspected. Historical `imports` fan out package membership into individual files, including test files; do not interpret those as production imports of tests. Current API route call edges match source. The 100 historical containment edges are provenance, superseded by the current 299-function census.

---

<a id="section-11"></a>

> Original: [reviews/06/report.md](reviews/06/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 06 — crypto broker adapters and history reconstruction

Source base **63968be62e44db2fb07a92883e02127b9064b0be**, read in `/tmp/vl-understanding-execution-20260913`, whose HEAD and clean porcelain were verified before reading. All **24 assigned files / 9,015 lines** were manually read in full. `functions.json` inventories **200 named functions/methods and 13 closures**, with exact boundaries, local call expressions, purpose and limitations. `reads.json` distinguishes assigned reads from additional dependencies/tests. This is a source review, not an exchange or production certification. No credentials, runtime settings, DB rows or broker accounts were read or changed; no orders, network calls, tests, deployments or source edits were performed.

[A] means a directly read source fact; [B] is its conditional implication. Every issue below is a **latent crypto-path concern**, not an observed MNQ trade failure. The owner describes current execution as NT8 SIM MNQ. The source constructor separately selects `ninjatrader` from crypto exchanges (`trader/auto_trader.go:632–694`), and each historical sync starter is separately exchange-gated (`:867–934`). Thus these files remain callable legacy support, not dead code, but their existence does not establish their use by the owner's current traders. No global claim that all code is SIM-only is justified: e.g. OKX explicitly sends `x-simulated-trading: 0` (`trader/okx/trader.go:232`), while Lighter's current constructor caller passes `false` for testnet (`auto_trader.go:674`). None of that demonstrates a configured live crypto account.

### Rules and freshness

The review used `docs/superpowers/AUDIT-CHECKLIST.md` classes 2/6/7/9/10 and pre-audit R1–R10: fresh source evidence, long/short mirror checks, explicit boundaries, account binding, isolation and no fabricated settlement. No PnL/statistical claims or row counts were made; the corrected-column rule therefore required no DB query. Severity below uses **A = substantial dormant execution/account risk**, **B = reconciliation/interface defect**, **C = robustness/documentation concern**; there is no S-grade current-runtime finding. Static facts are PROVEN by source; venue rejection/fill outcomes remain UNVERIFIED. No archived report is treated as current execution evidence.

Referenced document last-change lines at this base:

- `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap` — SYSTEM-MAP.md and VL-TRADING-RULEBOOK-v1.md.
- `dfda15e1 test: isolate session clock fixtures from weekly backfill workers` — AUDIT-CHECKLIST.md.

`CLAUDE-canon.md` was read fully: it corrects the stale supplied mirror's manual heartbeat advice; workers did not acquire or touch the main tree. `trader/AGENTS.md` is absent from this tracked worktree despite the root instruction index naming it. The latest daily-loss owner correction was retained; no per-trade cap was proposed.

### Data and control flow

[A] `types.Trader` has 19 methods (`trader/types/interface.go:45–106`). It normalizes many responses through `map[string]interface{}`, so conformance guarantees method signatures, not field types, units, casing, settlement status or completeness. `GetOpenOrders` promises stop, target and ordinary limits; selective cancel methods explicitly promise preservation of the other protective type. `GridTrader` adds placement, per-ID cancellation and depth. Concrete adapters frequently differ from those promises.

[A] Five assigned sync implementations—Aster, Binance, Bitget, Lighter and OKX—take **traderID + exchange account UUID + exchange type + Store**, fetch fills, sort oldest-first, insert `TraderOrder`, insert `TraderFill`, then call `PositionBuilder.ProcessTrade`. Their deduplication uses account UUID plus a **trade ID placed in the order-ID field**. Bitget/OKX fill rows also retain the actual venue order ID, making order-versus-trade identity intentionally uneven across records. `store/order.go:153–166,187–225` verifies existence by exchange UUID/ID, not symbol/trader ID. This review does not assume venue trade IDs collide across instruments; it records the identity boundary requiring confirmation before any repair.

[A] Times enter from venue milliseconds and become UTC Unix milliseconds; Gate closes use seconds (`gate/trader_account.go:155`). Aster/Bitget/Lighter/OKX periodically reread bounded recent pages, nominally last24h with 500/100/100/100 records respectively; Lighter ignores the supplied start time entirely. Binance recovers the DB cursor, adds1000ms, discovers symbols from commission, positions, recent DB fills and realized-PnL income, then uses per-symbol from-ID or time requests. None of these reviewed implementations drains paginated history to exhaustion. Tickers lack a stop channel/context. Binance starts its initial sync in one goroutine and the periodic loop in another, so overlap is possible.

[A] `store/position_builder.go:29–68` forwards crypto fills without an exchange exit reason; it dispatches open/close by action prefix. `:73–120` creates a new position or averages quantity/price into an existing row. The exchange-specific action classification therefore controls accounting state, rather than merely display text.

### File-by-file subsystem map

| Assigned files | Role and distinguishing semantics |
|---|---|
| `aster/trader.go`, `trader_account.go` | ECDSA/ABI/EIP-191 signing;30s client; microsecond nonce; cached tick/lot precision; public prices/depth; balance reconstructed from available balance plus leveraged mark notional; raw fallback on position error. |
| `aster/trader_orders.go`, `trader_sync.go` | One-way BOTH GTC aggressive limits; independent SL/TP orders; broad/selective cancel wrappers; PnL-based classification and last24h fill reconstruction. |
| `binance/futures.go`, `order_sync.go` | SDK/hook setup, server-time offset, constructor hedge-mode mutation; tagged random IDs; incremental history discovery and fill/order/position persistence. |
| `bitget/order_sync.go`, `trader_account.go` | max100 fill normalization and action mapping; cached USDT account equity; crossed/isolated and leverage requests; depth/ticker parsing. |
| `bybit/trader_account.go`, `trader_orders.go`, `trader_positions.go` | Unified-account balance; direct signed closed-PnL HTTP; one-way positionIdx0 market/limit/trigger requests; negative short positionAmt; cached snapshots. |
| `gate/trader.go`, `trader_account.go` | Authenticated SDK context, BTC_USDT conversion, contract multiplier cache; positive base-asset positionAmt; sparse close-history records. |
| `hyperliquid/trader.go`, `trader_orders.go`, `trader_sync.go` | Main/agent wallet checks; standard SDK plus explicit XYZ wire signing; nearest size and five-significant-figure prices; IOC market proxies, triggers and GTC grid orders; on-demand metadata helpers, not a background metadata loop. |
| `indodax/trader.go`, `trader_orders.go` | Spot-only HMAC-SHA512/form transport, monotonic per-instance nonce,5minute pair cache; BUY/SELL limits; explicit short/SL/TP refusal; leverage/margin and stop cancellation no-ops. |
| `kucoin/trader_orders.go` | Quantity-to-lots market entries; fresh-position capped exits; reduceOnly+closeOrder; mark-price triggers; client-ID substring stop classification; sparse/unconverted contract quantities in status/history. |
| `lighter/account.go`, `order_sync.go`, `trader.go` | Wallet→account selection, SDK signing-key comparison,7hour auth token, market-ID lookup; estimated margin and positive position-size maps; fill direction/position-flip reconstruction and periodic persistence. |
| `okx/order_sync.go`, `trader.go` | HMAC timestamp/path/body signing; account-mode detection/switch; lot formatting and instrument conversion; max100 fill history, hedge-mode action classifier and reconstruction. |

Paths in this table are under `trader/`. Individual methods and caller/callee expressions are enumerated in functions.json; those expressions are explicitly syntax-only where receiver type resolution was not independently verified.

### Findings, ordered by conditional consequence

#### A1 — Aster quantity/price formatter changes integer values

[A, static PROVEN] `aster/trader.go:199–208` calls `FormatFloat(...,'f',precision,64)` then always trims trailing `0`. With precision0, **100 becomes `1`; 0 becomes empty**. The same helper feeds open/close/SL/TP/grid requests (`aster/trader_orders.go:57–58,204–205,346–347,720–721`). This is a pure deterministic source defect; no venue response is needed to establish the wrong string. [B] A market with zero decimal quantity/price precision can receive a materially different order. Existing Aster mocks only exercise quantityPrecision3 and pricePrecision1/2, so they do not establish this edge case is protected. A focused pure helper regression is appropriate before repair.

#### A2 — Aster close and retry semantics lack settlement/idempotency boundaries

[A] `aster/trader_orders.go:159–321` submits GTC close limits without `reduceOnly`, parses acknowledgement, logs closed, and immediately cancels **all** symbol orders. No fill status/remaining exposure check exists between submission and cancellation. SL and TP submissions also omit reduceOnly (`:324–403`). [B] A still-resting close can be canceled by the subsequent sweep, and a stale oversized close/protection may increase reverse exposure under one-way execution semantics. These consequences are conditional; no venue action was observed.

[A] `aster/trader.go:329–369` retries any method—including order POST—on timeout/reset/EOF, copying and newly signing every attempt, up to3times. Assigned open/close order payloads do not provide a stable client key. [B] A response lost after venue acceptance can lead to duplicate submissions. No reproduction against a broker was attempted.

#### A3 — Hyperliquid reports fills/IDs not obtained from the exchange

[A] `hyperliquid/trader_orders.go:19–334` discards SDK order responses and returns `status=FILLED, orderId=0`. The close methods then cancel all symbol orders. XYZ handling checks the first inner error but returns only `error`; a resting response or even an empty status array can return success (`:584–742`). [B] Acknowledgement or partial IOC fill can be represented as complete settlement and remove remaining protection.

[A] GTC grid placement creates an ID from `time.Now().UnixNano()` (`:997–1055`), and `CancelOrder` parses the supplied ID and sends it to the venue (`:1059–1075`). That ID is not a venue receipt. This is a direct identity mismatch even without establishing a particular grid caller's runtime use. The actual SDK response contract was not independently audited; the defect here is throwing it away and substituting an unrelated ID.

#### A4 — Selective cancellation and open-order completeness differ from interface contracts

[A] Hyperliquid `CancelStopLossOrders` and `CancelTakeProfitOrders` both delegate to cancel-all-symbol behavior (`:337–424`), deleting ordinary entries and the other protective type. The source's comment claiming this is safe is not a verified invariant. Bybit `GetOpenOrders` reads only `orderFilter=StopOrder` (`bybit/trader_orders.go:527–585`), omitting ordinary GTC limits despite the interface promise. Its nonzero RetCode returns empty successful results. Bybit cancellation wrappers also discard business/individual errors (`:384–463`). Aster, Hyperliquid, Indodax, KuCoin bulk cancellation paths often return nil after individual failures. [B] Consumers can interpret incomplete cancellation/snapshots as complete state. This is not a finding about NT8's independent broker snapshot mechanism.

#### A5 — Constructor reads are not side-effect-free; OKX local mode can disagree after successful mutation

[A] Binance constructor requests hedge mode (`binance/futures.go:64–108`); OKX detects and may switch to long_short_mode (`okx/trader.go:112–196`). **`setPositionMode` never updates `positionMode`**. If detection returned net_mode and switching succeeds, local field remains net_mode; close payloads condition `posSide` on that cached field (`okx/trader_orders.go:241–244,352–355`). [B] Subsequent requests can use stale-mode fields. Existing OKX margin tests preseed long_short_mode and bypass the constructor, so they do not cover this sequence. This also explains why this review did not instantiate credentialed adapters merely to inspect them.

#### A6 — Lighter read-account fallback can break account consistency

[A] `lighter/account.go:15–82` tries stored accountIndex but falls back to the first account if absent; `lighter/trader.go:200–259` initializes from the first account, while TxClient was built for that chosen index (`:148–158`). Index0 is treated as unset. [B] A reordered/partial response lacking the bound nonzero index can make balance/positions describe a different account from the signing client. No account reorder was reproduced or owner binding inspected. A missing explicit target should be an error if this adapter is ever reactivated.

#### B1 — Order-first sync dedup prevents repair of partial persistence

[A] Aster `trader_sync.go:46–54,96–132`; Binance `order_sync.go:178–187,231–262`; Bitget `order_sync.go:188–195,232–263`; Lighter `order_sync.go:46–53,99–134`; OKX `order_sync.go:182–189,224–257`: existing order skips the whole fill/position path. Order creation occurs first; fill/position errors are logged and not rolled back. `store/order.go:153–166,187–199` supports independent order/fill idempotency, but the outer skip prevents invoking it for repair. [B] A failure after successful order insertion leaves a persistent missing fill or position update on later scans. Position averaging is not protected by a per-fill marker here, so removing the skip alone is not a safe fix. No DB failure injection was performed.

#### B2 — Action/PnL reconstruction loses valid closes

[A] Aster's classifier (`trader_sync.go:149–180`) relies on realizedPnL !=0 even for explicit LONG/SHORT. Binance (`order_sync.go:311–349`) also uses PnL as close evidence, and BOTH falls to opening regardless of PnL. Breakeven closure can therefore become an opening action. Bitget's buy_single/sell_single map always means open_long/close_long (`order_sync.go:111–116`), and OKX net mode always means open_long/open_short (`order_sync.go:116–124`); one-way short/close semantics are not fully represented.

[A] Lighter is internally stronger about direction: it derives action from signed position-before and splits a reversal into close/open IDs with proportional fee (`trader.go:568–688`), placing the close1ms earlier. Nevertheless **every emitted RealizedPnL is0**, while `GetClosedPnL` filters out all zero-PnL records (`:402–448`). Thus that interface method always returns no records on a successful current GetTrades path. This does not mean PositionBuilder can never derive a close; its separate OrderAction is preserved by sync.

#### B3 — Unknown data becomes successful zero/empty or unit defaults

[A] Aster GetTrades (`trader_account.go:217–232`) and Lighter GetTrades (`trader.go:493–523`) convert several API/parse failures to empty nil-error results. Lighter startTime is unused and newest limited rows alone are fetched. Gate failed contract lookup defaults multiplier1 (`gate/trader_account.go:86–99`); OKX fill conversion defaults lots as base quantity (`okx/order_sync.go:87–92`), and FormatQuantity metadata failure returns unconverted base size (`okx/trader.go:278–283`). [B] Missing metadata can become wrong-unit data rather than refusal. KuCoin GetOrderStatus exposes executedQty as int64 lots and GetClosedPnL/GetOpenOrders retain contracts; consumers expecting float64 base quantity must adapt explicitly. No blanket claim is made that all current callers assume one unit/type.

#### C1 — Concurrency, timeouts and protocol details requiring focused follow-up

[A] Aster getPrecision's final map read follows unlock (`trader.go:142–145`) and can race another cache-miss writer. Bitget GetBalance rereads cachedBalance after unlock (`trader_account.go:16–19`); several other caches return shared maps that callers must not mutate. Lighter reads authToken outside its mutex after token refresh, and Cleanup does not stop its ticker. Hyperliquid's price-significant-figure loop has no finite guard; positive infinity never exits (`trader.go:293–300`). These are static risks, not race-detector or fuzz reproductions.

[A] Hyperliquid XYZ metadata/open-order reads hard-code mainnet while sends honor testnet (`trader_sync.go:62`, `trader_orders.go:440,525–528`), and cancel wire field `a` is assigned the order ID (`:505`) rather than the asset index used in placement. Correct venue handling of that wire remains UNVERIFIED; do not treat the comment “asset index not needed” as proof. Bybit direct closed-PnL and depth use the default HTTP client without timeout (`trader_account.go:133`, `trader_orders.go:693`); this bypasses any SDK-specific transport settings. Bybit closed-PnL side/fee mapping requires venue semantic confirmation: the parser treats Sell as short, which cannot be resolved from this source alone. No web or broker request was made for that unresolved external fact.

### Tests, historical graph and negative evidence

[A] Fully read Aster mock tests, Binance sync diagnostic tests, Hyperliquid metadata race tests and OKX margin-mode recording tests. No tests were executed. Aster POST mocks JSON-unmarshal a **form-encoded production request**, then use defaults; their happy path does not verify precise transmitted fields. Hyperliquid tests establish intended metadata locking paths, not SDK order-response/XYZ behavior. OKX tests exercise actual request builders through a recording transport but initialize mode directly. Binance order-sync tests require live opt-in and are API diagnostics; they were not used as offline proof. Other test paths were inventoried only, not counted as full reads.

[A] Historical Understand Anything graph (July10@7a8adce0) has211 nodes for assigned paths and291 outbound-to-other-path edges. File summaries and representative import/test edges were compared with current source. Corrections: Aster “market open/close” is implemented as **GTC limits**, not market orders; Bybit “pagination parsing” overstates a single list parser with no cursor loop; Hyperliquid trader_sync.go is **on-demand metadata helpers**, not background sync; Lighter API-key generation/registration is **unimplemented**, and closed-PnL is empty by current dataflow. Historical tested_by edges mean a test file exists, not that it covers each risk. CGC was not queried/reindexed by this worker; root owns its exported evidence. No current dynamic call graph is claimed.

No repair was made because root owns the separate implementation lane. The next useful tests would be pure formatter cases, fake transports for rejected/partial acknowledgement and constructor-mode sequences, and isolated in-memory persistence failure tests with explicit per-fill idempotency. None requires a live broker or changing owner accounts. Legacy reactivation should first resolve those contracts; these findings do not justify modifying the owner's active MNQ risk policy or execution configuration.

---

<a id="section-12"></a>

> Original: [reviews/07/report.md](reviews/07/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Review 07 — retained crypto broker adapters

Source base: `63968be62e44db2fb07a92883e02127b9064b0be`. Isolated worktree `/tmp/vl-understanding-market-20260913`; initial `git status --porcelain` empty and revision verified. Review date: 2026-09-13 America/Chicago. All **25 assigned files, 9,071 lines**, manually read in bounded chunks; **184 named functions plus 9 callbacks** catalogued. Extra dependency/test reads are separately marked. No production/source edits, broker calls, credentials/environment reads, DB changes, test executions or restarts occurred.

[A] means directly read current source, [B] a consequence inferred from those source boundaries. The findings below are **static legacy-path defects/limitations**, not demonstrations of live failures, exploits, or the owner's MNQ behavior. No production rows were inspected, so there are no sample-ID/PnL population claims.

### Scope and authority

The parent's claimed documentation dispatch owns publication. This worker follows `/tmp/vl-understanding-review-instructions.md`, root AGENTS instructions, tracked `CLAUDE-canon.md`, and `AUDIT-CHECKLIST.md` pre-audit R1–R10. Applicable audit classes include wrong owner/identity, timestamp conventions, incomplete state presented as success, canonical identifiers, missing values, and tests exercising production call sites. No separate per-trade loss cap is proposed; the owner's daily-loss clarification remains authoritative.

Reference freshness at this source base:

- `git log -1 --oneline -- docs/superpowers/AUDIT-CHECKLIST.md`: `dfda15e1 test: isolate session clock fixtures from weekly backfill workers`
- `git log -1 --oneline -- docs/superpowers/SYSTEM-MAP.md`: `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap`
- `git log -1 --oneline -- docs/superpowers/VL-TRADING-RULEBOOK-v1.md`: `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap`

No implementation was built against those documents. Tracked `trader/AGENTS.md` is absent in this worktree; the parent-supplied root instructions still apply.

[A] `trader/auto_trader.go:632–688` chooses these broker constructors using `config.Exchange`; `ninjatrader` is a separate case. Startup invokes these crypto sync loops only under matching `at.exchange` branches (`:868–934`). Therefore these are retained selectable integrations, not dead source, but none of their broker-specific problems establish a defect in the owner's current NT8 SIM MNQ path. The shared `store.PositionBuilder` is also used outside crypto; consequences of its generic behavior require caller-specific evidence. This review did not recertify NT8 guards or the running process.

### End-to-end connections and state ownership

[A] The common `trader/types/interface.go:44` interface has 19 methods. It asks for selective SL/TP cancellation, normalized maps, and historical closure records. `GridTrader` extends it with limit placement, individual cancellation and book reads; `LimitOrderRequest` carries `PositionSide`, `ReduceOnly`, `PostOnly`, and `ClientID`. Implementing method names does not establish equivalent behavior across venues.

The flow for these integrations is configuration → `NewAutoTrader` broker choice → per-instance signed HTTP/SDK client → venue market/account/order API → shared balance/position/order maps. Fill-history sync then follows venue execution history → chronological sorting → order record → fill record → `PositionBuilder.ProcessTrade`. The account UUID passed as `exchangeID` is distinct from exchange type; history dedup commonly uses `(exchangeID, tradeID)`, while order records deliberately store a trade ID in `ExchangeOrderID`. Fill records often retain the real broker order ID separately. A consumer must not treat every such order row as one original exchange order.

[A] Balance consumers require `float64` values and either read explicit `totalEquity` or reconstruct wallet+unrealized (`auto_trader_decision.go:124–144`). Fill polling similarly reads `avgPrice` and `executedQty` as `float64` (`:363–374`). Numeric strings, synthesized zeros, and equity mislabeled as wallet have downstream consequences even though Go's map type compiles.

Account/position caches usually last 15 seconds. Binance/Bitget/KuCoin/OKX return cached fields after unlocking rather than capturing the field locally; returned maps/slices are also shared. [B] Concurrent refresh/invalidation can race with that field read, and external mutation can alter cached contents. This is a static synchronization concern; no race test or runtime race report was produced. Indodax and Lighter market-cache examples capture the cached reference while locked, though they still return shared data.

SDK calls commonly use `context.Background()` or a trader context; Bybit's direct `http.DefaultClient` and `http.Get` paths have no local timeout. Bitget, KuCoin and XYZ auxiliary requests explicitly use 30 seconds. KuCoin stores local-minus-server offset, adjusts signing timestamps, and refreshes on timestamp rejection without replaying the failed request. All four assigned sync loops own unbounded tickers/goroutines and expose no shutdown handle.

### Findings requiring attention before any crypto reuse

#### 1. Selective protection cancellation violates the shared contract

[A] `types/interface.go:81–85` explicitly says cancelling SL must not delete TP and vice versa. Gate `CancelStopLossOrders`/`CancelTakeProfitOrders` (`trader_orders.go:423,428`) both call `cancelTriggerOrders(:433)` whose `orderType` argument is unused: it cancels every trigger returned for the symbol. OKX `cancelAlgoOrders(:496)` similarly ignores `orderType` and cancels every conditional algo. Lighter `orders.go:159–200` sends both selective methods through `CancelStopOrders`, which cancels **all active orders**, including ordinary limits; this contradicts even its warning that says only stop orders.

[B] A caller adjusting one protective leg can remove the other; Lighter can additionally remove unrelated entries. These paths are not equivalent to NT8's separate order/bracket semantics. Remediation belongs in venue-specific filtering and acknowledgement handling with request-capture fixtures; do not globally weaken or reinterpret the interface.

#### 2. Failed or partial reads/cancels become successful empty/complete results

[A] Binance `CancelAllOrders(:387)` and `CancelStopOrders(:537)` always return nil despite API errors. Selective Binance cancel methods (`:235,311`) omit list errors and return an error only if cancellation errors exist and no cancellation succeeded. `GetOpenOrders(:596)` returns legacy orders successfully when Algo enumeration fails. Bitget `cancelPlanOrders(:340)` discards each POST error; bulk cancel discards ordinary/plan errors after listing. Gate bulk/trigger cancellation also logs and returns nil. Lighter cancellation loops count successes but return nil despite per-order failure.

[A] Bitget `GetOpenOrders(:494)` and OKX `GetOpenOrders(:657)` return nil error even when both ordinary and protective queries fail or JSON cannot decode. Gate `GetOpenOrders(:563)` drops the trigger portion on error. Hyperliquid `GetPositions(:11)` drops XYZ positions if the auxiliary query fails. None marks the successful result as partial.

[B] Callers cannot distinguish an empty book/position subset from an uncomputed one or confirm a cancel-all operation from its nil error. The problem is source-proven loss of evidence, not proof that a venue presently rejects these operations.

#### 3. Submission or absence is represented as FILLED

[A] Hyperliquid `trader_account.go:358` returns `FILLED` when `OpenOrders` fails and when the requested order is absent, with zero price/quantity/commission. An IOC cancellation is indistinguishable from a fill. Bitget market methods (`trader_orders.go:13,67,121,184`) decode only IDs and return `FILLED`; OKX market methods (`:13,92,171,282`) check acceptance `sCode` then return `FILLED` without an execution receipt. Gate (`:43,101,158,226`) ignores returned order status and always labels FILLED. Lighter `submitOrder(:352)` returns `submitted`, but `OpenLong/OpenShort/CloseLong/CloseShort(:20,61,102,146)` upgrade it to `FILLED`.

[A] Lighter's actual `GetOrderStatus(orders.go:95)` correctly returns query failure as an error, a useful negative result. However it returns `avgPrice` and `executedQty` as strings from its DTO, while the common polling site expects float64. [B] Were that polling path used for Lighter, it would retain fallback price/quantity rather than the fetched strings. Lighter has its own startup sync, so this is an interface incompatibility, not proof of this caller's runtime reachability.

#### 4. Lighter cancellation identity is selected by recency, not transaction identity

[A] `CreateOrder(trading.go:190)` invokes `pollForOrderIndex(:443)` after a limit submit. After 500 ms, the helper returns the highest active `OrderIndex`; its `txHash` argument is only logged. `PlaceLimitOrder(:929)` publishes that value to callers; `CancelOrder(orders.go:270)` treats numeric IDs as direct cancellation targets.

[B] Existing concurrent orders or delayed visibility can cause the new placement to be identified as another order, allowing a later cancel to target it. No concurrent venue incident was reproduced. The nearby `getOrderIndexByTxHash(orders.go:329)` does exact matching, but it does not repair this numeric misidentification because the numeric path bypasses it.

#### 5. History synchronization has incomplete commit/replay semantics

[A] Bybit `SyncOrdersFromBybit(:178)`, Hyperliquid `SyncOrdersFromHyperliquid(:17)`, and KuCoin `SyncOrdersFromKuCoin(:279)` first skip an existing order, then separately write order, fill and position. Failure after successful order insertion is logged; the next poll skips the whole trade, so it cannot repair the missing fill/position through this path. Success counters count inserted orders, not completed three-part processing. No transaction or durable processing stage appears in these functions.

[A] Gate `SyncOrdersFromGate(:153)` attempts a different repair: it reprocesses **every existing close** on every poll (`:195–207`). `PositionBuilder.ProcessTrade(:29)` routes to `handleClose(:124)`, which reads the currently open symbol/side position and reduces/closes it without checking that the supplied order ID has already been applied. [B] Repeated partial-close history can be applied twice to a still-open position; an old full close replayed while a newer same-symbol/side position exists can act on the newer row. The observed code path lacks a per-fill idempotency guard. This was not run against any database; root should own a local fixture if repair is authorized.

History coverage is bounded: Gate/KuCoin only fetch max 100 records without traversing pages; Bybit sends limit 1000 without a cursor despite parsing only one list; Binance income symbol discovery caps 1000; Bitget history max100; rolling 24h windows can omit earlier openings. This review makes no current exchange-limit compatibility claim. Gate comments acknowledge reversal fills but classify their entire size as one close; Bybit similarly treats any positive closedSize as wholly closing. KuCoin infers close from positive closing fee, leaving fee-free closure ambiguous. Hyperliquid falls back to nonzero PnL when Dir is unknown. These are incomplete reconstruction contracts.

#### 6. OKX response decoding and equity semantics disagree with their consumers

[A] `OKXTrader.doRequest(trader.go:207–256)` unmarshals `OKXResponse` and returns `okxResp.Data`. `GetClosedPnL(trader_account.go:168–214)` then unmarshals those bytes into **another** `{code,msg,data}` envelope. Normal array Data cannot unmarshal into that struct; even `null` leaves Code empty and fails the later `Code != "0"` check. This mismatch is independent of live credentials. If decoded successfully through a changed wrapper, its quantity would still be raw contracts rather than the base-asset quantities emitted by GetPositions/GetOrderStatus.

[A] `GetBalance(trader_account.go:14)` puts `totalEq` into `totalWalletBalance` and also returns USDT UPL, but no `totalEquity`. The shared account consumer adds wallet+UPL. [B] Nonzero UPL is counted again on that consumer path. The Hyperliquid/KuCoin implementations explicitly provide equity or subtract unrealized from wallet, demonstrating the intended distinction locally.

[A] OKX `doRequest` also permits outer Code `1` for partial success. Market placements check each item `sCode`; `SetStopLoss`, `SetTakeProfit`, `CancelOrder`, and algo cancellation do not. [B] Individual failures can be reported as successful protection/cancellation despite a decoded body describing failure.

#### 7. Quantity, side and symbol normalization are inconsistent

[A] Gate market/protective placement divides quantity by multiplier, truncates to int64, and forces all nonpositive results to one contract (`trader_orders.go:60–64`, mirrors). Thus zero/negative/tiny inputs can become an actual positive order request rather than refusal. The multiplier parse is unchecked. KuCoin `quantityToLots(trader.go:350)` rounds and caps, without lot-size alignment; missing position multiplier defaults to BTC's `.001` for every instrument (`trader_positions.go:64`), while history defaults non-BTC to `.01`. OKX `GetOpenOrders(:657)` leaves quantity in contracts while `GetPositions(:12)`/`GetOrderStatus(:584)` convert to base units; missing metadata silently leaves contracts under a base-quantity field.

[A] Binance retains signed short `positionAmt` and correctly negates it for CloseShort quantity=0; Aster/Bitget/Hyperliquid/KuCoin/OKX generally return positive magnitude with side. Do not report Binance's negate as a defect without this producer/consumer pairing. Indodax returns uppercase `LONG`, unlike other positions. OKX net-mode negative position gets absolute magnitude but remains `long` because only `posSide == "short"` sets short; its close methods contain net-mode handling, so constructor mode-change failure is an important boundary to verify before reuse. Net-mode close order bodies omit reduceOnly, so a venue inventory change between read and execution is not guarded by that flag.

[A] Lighter `normalizeSymbol(trading.go:471)` strips `USDT` before `/USDT`, so `BTC/USDT` becomes `BTC/`; lowercase suffixes are uppercased only after attempted stripping. Hyperliquid `GetOpenOrders(:528)` compares `order.Coin` directly with caller symbol instead of using the converter used by price/status/book methods, so canonical `BTCUSDT` cannot match venue `BTC`. KuCoin's symbol conversion is case-sensitive and appends its suffix even to an already converted string. Aster position decode checks only `positionAmt` type then directly asserts required price/leverage strings (`trader_positions.go:37–41`), permitting panic on malformed/missing fields.

[A] Binance grid placement (`futures_orders.go:418`) derives position side only from BUY/SELL and ignores ReduceOnly, PostOnly, requested PositionSide and ClientID. OKX grid similarly derives side and does not use requested PositionSide for the transmitted body, though it echoes that field in its result. These are contract differences, not assurance that current grid callers exercise every combination.

### Remaining per-file behavior and limits

| File group | Read behavior and ownership boundaries |
|---|---|
| Aster positions | `GetPositions:12`, `SetMarginMode:66`, `SetLeverage:113`; unsigned normalized magnitude, signed request helper outside slice; most margin errors are tolerated, unified/portfolio errors refused. |
| Binance account | `GetTrades:112` produces minimal income records with absent side/price/quantity/fee. `GetClosedPnL:55` wraps these as closures and defaults side to short for missing side; entry/exit/quantity remain zero. `GetTradesForSymbol:155` and `FromID:198` provide actual fill fields and are the better reconstruction boundary. |
| Binance position helpers | Leverage update `:120` consults cached positions then sleeps 5 s after success. `CalculatePositionSize:185` allocates leveraged balance, not stop-loss risk. `GetMinNotional:193` is fixed 10, not exchange metadata. Formatters round decimal precision and return fallback values on metadata failure. |
| Bitget core/positions | Constructor `trader.go:84` POSTs one-way account mode, so constructing for a read-only experiment is not harmless. `doRequest:138` signs exact path/body and unwraps Data, manually builds GET queries without escaping. `getContract:221` uses a shared refresh timestamp for per-symbol entries. `GetClosedPnL:96` parses history values but supplies no unique position/order ID. |
| Bybit core | Constructor `trader.go:43` configures MAINNET and a Referer header. `getQtyStep:86` is a public mainnet GET with default step 1; `FormatQuantity:144` floors to step, a stronger behavior than plain decimal rounding. `parseOrderResult:179` preserves NEW. |
| Gate order details | `SetMarginMode:34` is a logging no-op; `SetLeverage:16` returns nil on RISK_LIMIT_EXCEEDED. Protective requests set both nonzero Size and Close=true; SDK/venue compatibility not verified. `GetOrderStatus:502` sums Tkfr/Mkfr under commission; their external meaning was not independently checked, so this is an unresolved semantic concern, not a proven fee calculation. `GetOpenOrders:563` classifies trigger Rule alone, inverting the helper's intended labels for short-side protection. |
| Hyperliquid account/positions | Balance combines spot USDC, perp and XYZ compartments; available excludes XYZ, optionally adds spot in unified mode. XYZ balance/price URLs are hardcoded mainnet even if SDK is configured elsewhere. Missing XYZ becomes a partial success. `GetClosedPnL:405` excludes zero-PnL closures even though `GetTrades:456` parses explicit close Dir. `GetTrades` ignores requested limit. Margin mode is trader-wide mutable state. |
| Indodax account | `GetBalance:15` values IDR cash/holds only and publishes separate unvalued crypto balances. `GetPositions:87` synthesizes spot positions with mark as entry and zero unrealized, ignoring mark lookup failure. `GetClosedPnL:163` always fetches BTCIDR, emits buy rows too, and lacks quantity/PnL; decode failure is returned as nil success. It is not a generic futures closure ledger. |
| KuCoin core/account/positions | `NewKuCoinTrader:98` immediately reads server time. Timestamp resync changes later requests only. `GetBalance:11` exposes explicit equity. Position cache invalidation is explicit `:110`. `getContract:284` refreshes all contracts, unlike per-symbol shared-TTL refresh in Bitget/OKX. Fallback multipliers/leverage carry no unknown marker. |
| Lighter orders/trading/types | API key/account indices own signed requests. Some SDK signing calls provide explicit account/key and others only nonce; SDK default behavior was not inspected. Market IDs narrow uint16 to uint8 without range checking. Price/base fixed-point casts have no explicit bounds/finite guard; external SDK validation unresolved. Market and trigger execution bands are ±5%; limit expiry seven days, stops thirty days. `SetLeverage:625` always sends cross margin, so calling it after `SetMarginMode(false)` can change mode again. `SetMarginMode:687` silently uses 10x when position lookup fails. DTOs are not normalized float64 models. Checksum `types.go:118` validates length only, not hexadecimal content. |
| OKX metadata/orders | Shared per-instrument TTL can make older entries look recently refreshed. FormatSize in dependency uses decimal rounding, not general lot multiple enforcement. Close reads fresh positions and actual margin mode, a useful positive distinction; ack is still not fill. `_sl`/`_tp` IDs in open-order output are display identities, not accepted by ordinary `CancelOrder` unless a caller strips/routes them. |

### Tests and graph verification

[A] Fully read `trader/okx/trader_margin_mode_test.go` (247 lines): its recording transport drives the actual SetMarginMode/SetLeverage/OpenLong/OpenShort/SetStopLoss/SetTakeProfit/PlaceLimitOrder request sites, asserting requested tdMode/mgnMode. It does not establish execution, partial response handling, cancellation selectivity, or net-mode position sign correctness.

[A] Fully read `trader/lighter/orders_test.go` (421 lines): DTO parsing and duplicated conversion logic are tested. Its mock-server test uses direct `http.Get`, **not** `LighterTraderV2.GetActiveOrders`, so it does not prove production auth construction or index selection. These tests illustrate checklist call-site parity limits. Excerpts of KuCoin order-sync tests show credential-gated live HTTP construction, and Binance futures test setup shows a mock SDK suite. No tests were run: source review did not require network-capable constructors or broad suites, and root owns repairs/reproduction. Recommended focused future fixtures are failure/partial-read propagation, selective cancellation preserving the other leg, ack-vs-fill, Lighter exact ID attribution, Gate replay idempotency, OKX Data decoding/equity, and mixed-unit normalization.

[A] Historical Understand Anything graph was read from the supplied July-10@7a8adce0 stash, filtered to the assigned paths: **195 nodes** and **222 outgoing non-containment/export edges**. All 25 file summaries and the first 20 dependency/testing edges were manually inspected; this does not certify every historical edge. Source broadly confirms adapter roles, but corrects Bybit's summary: its RoundTripper adds Referer, not API credentials. Indodax's “closed PnL reconstruction” overstates its incomplete BTCIDR rows. A tested_by edge is merely a test association, not proof the real call site is exercised. `graph.json` records these corrections and explicit current boundaries. CGC export/service was not independently queried by this worker; root owns that evidence.

### Review conclusion

This slice is understood as a heterogeneous legacy integration layer with shared method signatures but divergent units, identity, acknowledgement, cancellation and history semantics. The most actionable source-proven issues are selective cancellation breadth, successful unknown/partial state, Lighter misattributed order identity, non-idempotent/incomplete sync, and OKX double-unwrapping. They warrant offline regression fixtures before any authorized crypto reuse. They do **not** justify changes to the owner's accounts, NT8 execution, daily-loss policy, or SIM restriction. All assigned source reading is complete; runtime compatibility and unexamined dependency paths remain explicitly unverified.

---

<a id="section-13"></a>

> Original: [reviews/08/report.md](reviews/08/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 08 — kernel engine, clocks, calendars and risk understanding

Base **63968be62e44db2fb07a92883e02127b9064b0be**. Read-only worktree `/tmp/vl-understanding-execution-20260913`; pwd/revision verified and initial porcelain status empty. 31 assigned source files, 7,721 lines, **all read manually in full**, 230 named functions catalogued. Inline callbacks are attached to their enclosing functions. Source hashes match the assignment manifest. No production/source/config/DB changes, tests, external calls, or runtime incident probes were performed. Artifacts alone were written under `/tmp/vl-review-08`.

Evidence: **[A]** means exact source read or local inventory observation; **[B]** is inferred consequence; **[C]** unresolved hypothesis. Static source behavior is not runtime deployment proof. Findings below are PROVEN only at the stated static boundary; operational outcomes remain UNVERIFIED.

Rules read: supplied/root AGENTS, tracked `CLAUDE-canon.md`, AUDIT-CHECKLIST pre-audit R1–R10 and horizon classes, relevant SYSTEM-MAP clock/risk/blackout paragraphs, corrected RULEBOOK authority and structural-stop daily-loss clarification. `kernel/AGENTS.md` is absent in this checkout despite root prose linking it. Parent owns branch claim/isolation. The tracked canon supersedes stale hand-heartbeat instructions. No mandatory per-trade loss cap is inferred: owner clarification means existing **daily** loss controls.

Spec freshness receipts from this base:

- SYSTEM-MAP: `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap`
- VL-TRADING-RULEBOOK-v1: `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap`
- AUDIT-CHECKLIST: `dfda15e1 test: isolate session clock fixtures from weekly backfill workers`

No implementation was built against a plan; these references orient the source review, and their historical measurements are not fresh measurements by this review.

### Main flow and authority boundaries

[A] `GetFullDecisionWithStrategy` (`kernel/engine_analysis.go:57–643`) is the current decision orchestrator. Its sequence is: nil context refusal; assert-only duplicate CME gate; futures expiry candidate filtering; concurrent position and daily guardrails; default engine if absent; clamped config and estimated token budget; captured snapshot instant; multi-timeframe market fetch; active symbol selection; bar/profile/levels/bias/weekly/plan facts; ownership assertion; current StrategyEngine system/user prompts; bounded model call and parse/validation; price sanity, stale data, drift observation and re-entry cooldown. This function does **not** place orders.

[A] Daily guardrails use `ctx.DailyRealizedPnL`, trades today, master/toggles, strategy values and env fallbacks (`:150–215`), not session-cumulative total PnL. `RiskForceFlat` result sets a per-trader latch (`:195–196`) here; no position close is called. Concurrent cap is applied before the model and can skip an entire decision cycle even with held positions. Broker/entry gate remains a separate execution authority. The nil-engine fallback is constructed at246, after the engine-dependent daily block at150; a direct nil-engine caller bypasses that block in this function. No normal production nil-engine case was established.

[A] Market fetch (`:778–865`) reads configured TFs/periods via `market.GetWithTimeframes`; it skips individual fetch failures, always returns nil, exempts CME futures from crypto OI filtering, and preserves held-symbol attempts. The top OI ranking fetch at314 is unconditional on futures mode when the context map is nil. [B] This leaves a crypto dependency reachable in futures orchestration, although the data routing inside the market layer belongs to another slice. Empty market fetch alone is not a cycle refusal here; downstream entry checks are essential.

[A] Per-trader ownership travels through `ctx.TraderID` into active plan, session registry, dATR context, touch updates and `assertPromptOwnership` (`:426–540`). A snapshot timestamp is captured once (`:286–287`), but market, 1m tape, HTF providers and plan providers are separate reads. [B] A common time label does not create an atomic market/plan snapshot; no race or contamination was reproduced. Mutable engine context fields are reset/set in this function, so per-engine concurrency ownership matters outside this slice.

[A] `engine_prompt.go:19–29` routes futures variants to `buildFuturesPrompt`; live orchestration calls `engine.BuildUserPrompt` at546. The compact standalone `BuildFuturesSystemPrompt`/`BuildFuturesUserPrompt` pair emits a different LONG/SHORT/NONE contract and has production-source callers in the smoke command, not the live call above. The legacy `PromptBuilder` emits yet another uppercase action contract. These APIs must not be treated as interchangeable.

[A] Current futures prompt (`engine_prompt_futures.go:59–288`) has fixed instrument/risk/output sections plus editable role/frequency/entry/process/custom text, optional stable plan block and dynamic map tail. It resolves confidence from the shared safe default but carries an unset RR fallback1.5 at69–72. It renders a single active symbol; unknown instrument roots silently become MNQ at324–339. The current MNQ-only operational scope limits this ambiguity; general multi-instrument safety is not established by this prompt fallback.

### Arms, confirmation and refusal semantics

[A] `ArmableCondition` recognizes FVG/reject/up/down continuation/reclaim; split sweep reclaim is a special case, with limit kind but not ordinary armability. `ArmKindFor` derives limit versus reclaim stop-entry; empty authored kind is allowed for composition, conflicting known kind refused. These functions classify **mechanics**, not live seam enablement or order admission. `BiasArmWarning` counts enabled same-side arms and warns only; it does not reject a plan or validate that an enabled arm is live/feasible.

[A] `BreakdownContinueState` (`breakdown_continue.go:141–192`) gets a confirming reference and scans closed5m buckets. Beyond closes contribute maximal excursion; any post-reference non-beyond close permanently marks reclaim, suppressing both legs. Later touch of the broken level plus beyond close satisfies pullback retest. Immediate mode takes leg2 at leg1. `ValidateBreakdownContinueScenarios` (`:200–279`) checks object, side, positive level, optional ATR-distance floor, mode, reclaim, confirmed pullback break, displacement and enabled-arm pullback/wait-confirm/confirm/min-stop requirements. Immediate displacement uses completed1m bars through `Evaluate1mDisplacement`, separately from closed5m void checks. Missing/nonpositive ATR skips ATR-based checks. `BreakLeg` declaration is read but not substituted for machine displacement; `Pullback` is declared schema data, not enforced by this state loop.

[A] `EvaluateBucketClose` (`confirmation_bucket.go:26–36`) aligns by epoch and compares explicit now against true end. `closedConfirmationBuckets` filters forming buckets. This is temporal closure, **not a completeness test** for every source minute. `evaluateConfirmAfter` (`confirmation_evidence.go:13–103`) accepts an observed forming touch without an event time, but a second leg needs a closed event reference. Time-hold counts completed beyond-side1m bars; acceptance/MSS calculations are delegated. `orderedScenarioConfirm` (`:127–158`) requires second event strictly after first; missing reference gives UNKNOWN and false, earlier second event is out_of_order. `recordConfirmationVerdict` counts per invocation, not deduplicated market episodes.

[A] `ComputeLevelDisplacements` evaluates the same waterfall state on deduplicated levels and both sides over `VoidScope`; rendering distinguishes no break, closed5m eligibility and immediate1m evidence. This is prompt/validator calculation parity, not independent empirical validation. `validator_hints.go` checks a **registered** vocabulary against condition status/default rule enums, including derived EntryLaw styles. It is not a global scan of every emitted hint; new unregistered prose can escape that registry.

### Time, calendar and horizon

[A] `TFDurationMs` is an exact case-sensitive table with explicit unknown false. `HorizonOf` measures count/span/age/gaps with -1 UNKNOWN, bounded calendar walk and bounded memo. It assumes ascending unique aligned cache bars; it does not verify every bar. The shortcut `span/step+1==served` at185 declares contiguous, and the memo uses only TF/oldest/newest/count. [B] Duplicate/off-grid or interior-distribution corruption could mask gaps; this is a trust-boundary limitation, not a demonstrated feed defect. `Why` warns EMPTY/SHORT/HOLED; unknown-only gaps do not count as HOLED.

[A] `IsCMEOpen` combines embedded session classification with weekly schedule. Shortened days halt until17CT and then reopen if weekly hours permit. `CMESessionDayStart` is most recent17CT using civil date arithmetic, not midnight and not necessarily an open session. `NextCMEOpen` walks future hour boundaries; existing reopen semantics are hourly, but its comment claiming **all state transitions** are hourly is stale because shortened closes can be minute-specific.

[A] `SessionStateAt` (`session_calendar.go:228–282`) consults covered-year data; absent date in covered year is normal, malformed class/close is closed. An uncovered year uses `isCMEHoliday`, otherwise normal: it does **not** refuse every uncovered-year date as the preceding comment at225–227 implies. `Unestablished` is copied as metadata; this function does not force class closed merely because that flag is true. Closure relies on embedded row class integrity. Calendar JSON data validation is outside assigned coverage. The direct key early-close accessor (`:303–325`) uses date equality; callers must not confuse calendar date with the start-date CME key.

[A] Configured `InBlackoutWindow` is end-exclusive and invalid/zero-width fails open; `InT1Blackout` is end-inclusive. First-N and lunch windows are code constants, while T1 windows are calendar inputs with resolved source tags. Read-time no-trade rendering uses minute-of-day and nearest±12h offset, not date identities; it is suited to intended short session views, not arbitrarily old archived plans. ETtoCT uses fixed one-hour wall-time difference and ignores DST transition ambiguity.

[A] Clock drift observation in `applyClockDriftBlock` logs/counts at absolute>60s but never alters decisions. `ClockHoldDecision` only defers **negative** beyond60s; positive skew may be old closed-market bars and widens news windows instead. Production widening is confirmed at `trader/auto_trader_calendar.go:193–195`; authoring seam calls same predicate at `auto_trader_planner.go:1032–1035`. `FeedClockDriftMs` uses last1m open+60s against wall clock, so this is a feed-age proxy, not independent NTP truth. Atomic drift availability/value are separate and carry no measurement timestamp. Clock health subprocess/state reads are best effort and observational.

### Static findings and limitations

1. **[A] PROVEN static / medium: earlier AI response preservation stops at helper boundary.** `callWithSchemaRetry` returns last nonempty output on later transport error (`engine_analysis.go:684–699`), but caller returns `nil,error` at589–590 and loses that raw output and prompts. `schema_retry_test.go:103–119` tests only the helper, despite describing preservation into the record. [B] A first malformed response followed by timeout cannot reach the caller's normal FullDecision recording path with the preserved evidence. No live instance was queried. Duration also overwrites each attempt at697; AIRequestDurationMs at612 is final attempt only, not total retry cost.
2. **[A] PROVEN static / medium: current futures output instructions contradict each other.** `engine_prompt_futures.go:220` says reasoning then JSON;231 says decision FIRST;232–239 supplies reasoning-first example. [B] This weakens the truncation-resistant contract. No model compliance/failure rate measured.
3. **[A] PROVEN static / medium: JSON format screen reads string contents as number syntax.** `engine_analysis.go:1046–1056` rejects any tilde or digit-comma-three-digits anywhere in the extracted array, including valid reasoning strings. Single-object fallback at983–987 skips this screen. Thus envelope choice can alter acceptance of otherwise identical data. Unicode replacements at1012–1033 also alter quoted reasoning content. No execution bypass demonstrated; full decision validation still follows extraction.
4. **[A] PROVEN static / low: grid parser is not a rejecting whitelist.** `grid_engine.go:504–515` warns but returns unknown actions. Downstream `trader/auto_trader_grid.go:556–558` also warns/noops, so unknown actions do not become arbitrary orders at this boundary. Supported compatibility open_long/open_short are advertised valid by parser but fall through the executor switch. This is misleading accepted-result behavior, not an exploit. Grid missing indicator fields render zero, and BOLL lower/middle are indexed when only upper length is checked at581–584; caller invariants require separate validation.
5. **[A] PROVEN static / low: widening is modulo-day, not saturating.** `calendar_blackout.go:105–108` wraps expanded boundaries independently. For sufficiently large absolute drift, an intended≥24h protection interval is represented as a smaller wrapped band. Production does pass positive stale-feed drift to this function. [B] Very old feed evidence could therefore produce surprising blackout geometry; operational occurrence and desired full-day policy unverified. Current F6 tests cover≤3minute widening, not day-scale saturation.
6. **[A] PROVEN static limitations:** `ArmedEntryPx` promises zero for bad direction but FVG/reject defaults non-short to long (`armed.go:46–59`); upstream validation must own direction. Observer parser accepts confirmed-conflict without checking confidence≥70 or event citation (`engine_prompt_observer.go:136–149`), while prompt requests it at106. Watcher at375–428 remains observation/rails-only; no order authority found in this read. `IsPlanFragment` checks substrings rather than top-level JSON keys (`repair_outcome.go:53–63`), affecting labels not parsing authority. `schema.go` map iteration makes legacy/crypto schema output ordering nondeterministic; current futures route bypasses it. Historical legacy percentage/scale-in rules are not new owner policy.

No result in this slice establishes a live-account route, an executed bad trade, or a profitable trading rule. Broker SIM enforcement, structural geometry and complete entry-gate callsites are assigned elsewhere.

### Tests and graph

[A] Fully read `schema_retry_test.go`, `arm_kind_test.go`, `clock_hold_f6_test.go`; read selected confirmation closure/order/reference tests and horizon open-market/unknown-cap tests. Tests were **not run**: root owns the consolidated test plan. The arm-kind tests pin mapping/canonicalization; F6 cases pin negative-only hold and minute widening; confirmation cases include frozen scenario38329 but this review did not rerun or inspect live row38329. Horizon fixture references65/66 are archived test provenance, not new DB claims. Test names elsewhere were inventoried only, not claimed fully reviewed.

Historical Understand Anything graph: July10@7a8adce0, **79 nodes across7 of these31 files**, 376 incident edges. File imports often expand package imports to every file (even test files), so they are not precise dependencies. Function-call/documentation edges were read separately. Corrections in graph.json: obsolete CheckDataHealth edge; grid whitelist overclaim; wrong fixMissingQuotes summary; compact futures prompt live-path overclaim; old boolean holiday model. The24 absent source files, including modern clock/confirmation/calendar/arm support, are not represented by that historical graph. Root's Go catalog supplies exact current declarations and syntax-only calls; no type-resolved caller graph is claimed. CGC evidence is parent-owned, historical, not reindexed here.

### Per-file coverage and purpose

- `kernel/arm_kind.go:1–111`: Canonical condition-to-limit/stop_entry mapping; reclaim stop, waterfall pullback limit; split sweep has kind although ordinary ArmableCondition is false. Empty authored kind derived; unknown condition not refused by mismatch helper itself.
- `kernel/armed.go:1–79`: Armable vocabulary, resting entry derivation and A+/A/B/C rank. FVG CE or edge, reject favorable tick, waterfalls delegate. Bad direction is treated as long by ArmedEntryPx despite comment promising zero; upstream validation required.
- `kernel/arms_bias_coherent.go:1–123`: Derived prompt vocabulary grouped by resolved shadow status; bias warning advisory only. Enabled arm counts even if shadowed/unarmable; valid upstream plan is assumed.
- `kernel/bar_horizon.go:1–258`: Requested/served/span/age/open-session gaps; unknown=-1 with reason. Calendar scan cap 200000, bounded 256-key mutex memo; metadata key assumes unique aligned ascending bars. No trading gate.
- `kernel/boot_integrity.go:1–209`: Startup VCS prefix plus embedded golden assertion latches atomic entry refusal. Env expectation overrides relative deploy/RELEASE; no expectation is allowed; dirty flag logged, not refused. API exposes asserted short rev.
- `kernel/breakdown_continue.go:1–304`: Symmetric waterfall conditions; completed 5m break plus later touch-and-beyond retest, reclaim permanently voids since reference. Immediate mode measures completed 1m excursion; arms pullback-only with confirm+wait_confirm. Optional ATR/distance gates skip absent ATR.
- `kernel/calendar_blackout.go:1–146`: T1 only timed events map to inclusive CT minute windows ±15m; T2 ignored. Drift rounds absolute ms up to minute and wraps bounds; no full-day saturation. Helpers return nil for no windows.
- `kernel/clock_drift.go:1–178`: Snapshot drift observer warns/counts but does not alter opens. Shared measured drift has no timestamp and separate atomics. Feed measurement uses last 1m open+60s. Authoring hold only negative drift beyond tolerance; positive ambiguity widens windows.
- `kernel/clock_health.go:1–188`: 30s configurable warning versus fixed 60s critical; logs feed/local and best-effort timedatectl with 2s timeout. Boot RTC/state-file freshness is observational only; future state timestamp classified active.
- `kernel/cme_calendar.go:1–272`: Calendar-first Globex weekly opening; shortened date halt ends17CT. Session-day starts most recent17CT including closed periods. Blackout configured window end-exclusive. Uncovered-year fallback uses approximate legacy holidays.
- `kernel/confirmation_bucket.go:1–88`: Single explicit-clock epoch-aligned bucket-end predicate; completed buckets only for close confirmations; immediate displacement uses completed1m beyond-level excursions. Closure is temporal, not evidence of full bucket coverage.
- `kernel/confirmation_evidence.go:1–158`: Touch may be met while forming but cannot supply ordered event reference; hold counts completed1m beyond closes; MSS and acceptance delegated. Second leg must follow recorded first reference; missing reference UNKNOWN, earlier event out_of_order.
- `kernel/confirmation_telemetry.go:1–60`: Atomic process counts for forming/out-of-order verdicts and logged outcome; boot parses embedded frozen replay receipt, separating audit observations from live counters.
- `kernel/displacement_feeds_forward.go:1–142`: Prompt side uses same breakdown evaluator and scope as validator for unique positive levels, both directions, sorted price. Immediate1m separately disclosed; absent ATR hides numeric floor claim.
- `kernel/engine_analysis.go:1–1133`: Main Context→risk preflight→market/snapshot→per-trader facts/ownership→StrategyEngine prompts→bounded model retry→parse/validate→sanity/stale/clock/cooldown. Entry refuses become wait or named cycle skip. Transport error discards helper-preserved response.
- `kernel/engine_prompt_futures.go:1–454`: Active futures prompt uses lowercase action array, resolved confidence/RR and instrument; static example/order contradict decision-first instruction. Standalone LONG/SHORT/NONE builder is smoke path. Unsupported symbols silently fall back MNQ.
- `kernel/engine_prompt_observer.go:1–164`: Watch-only thesis/structure prompt and enum parser; ignores action fields, clamps confidence; no trading API. Parser does not enforce citation or confirmed-conflict confidence contract itself; watcher owns rails.
- `kernel/formatter.go:1–645`: Legacy bilingual context formatters use USDT/crypto arithmetic and OI; resolved/unresolved trades differ. Latest30 bars rendered CT; Chinese preserves TF order, English lexicographically sorts. Zero equity and nil map values not guarded.
- `kernel/grid_engine.go:1–621`: Separate bilingual grid prompt/call/parser path without normal decision validation; invalid actions warned and returned. Malformed output becomes synthetic hold; market builder assumes BOLL arrays aligned and leaves many optional fields zero.
- `kernel/htf_veto.go:1–122`: Configured timeframe/default1h or cross1h+4h or4h mode; opposite confirmed trend refuses open only. Missing data fail-open, cross missing snapshot silently passes. Environment read inside nominally pure verdict.
- `kernel/no_trade_band.go:1–265`: One first5m/lunch12–13:30CT definition shared with render/gate; windows carry source tags. Read-time status on minute-of-day near±12h axis, not dated windows. T1 receives already resolved gate windows; model own sit-out prose separate.
- `kernel/price_sanity.go:1–77`: Side-independent8ATR SL/TP distance and1% entry bound; absent market/ATR fails open. Caller uses market price as entry so 1% branch cannot trip on that call. Owner levels use dailyATR. No explicit finite checks.
- `kernel/prompt_builder.go:1–376`: Legacy uppercase HOLD/PARTIAL_CLOSE/FULL_CLOSE/ADD_POSITION/OPEN_NEW/WAIT contract, bilingual fixed crypto strategy; separate from StrategyEngine. Format validator checks required fields and nonzero leverage/size, not positivity.
- `kernel/realign.go:1–112`: Owner edit/batch description plus overlay-resolved plan/live context for advisory re-examination; no mutation. PROPOSE-MERGE only meaningful with patch operations; system contract shared elsewhere.
- `kernel/regime_ledger.go:1–16`: Boot log resolves env knobs but reports literal ON as documented shipped defaults for per-strategy toggles. Does not claim to have read active strategy binding.
- `kernel/repair_outcome.go:1–64`: Classifies empty/ok/packaging/fragment/content via error strings and structural-key substring heuristic. Fragment check does not parse object keys; nested key or quoted word can suppress classification.
- `kernel/schema.go:1–555`: Bilingual field dictionary, legacy trading rules and OI interpretations; schema generation emits dictionary/OI, not TradingRules. Go map iteration makes field order nondeterministic; futures primary builder bypasses this schema.
- `kernel/session_calendar.go:1–425`: Embedded JSON parsed at init; covered years/dates resolve normal, shortened, closed. Uncovered years use legacy holiday boolean, not blanket closure. Unknown classes/unparseable closes closed. Unestablished is metadata, closure relies on row class.
- `kernel/timeframes.go:1–53`: Single case-sensitive known TF→ms table; unknown=(0,false), includes2m through1w but not arbitrary durations; no silent1m substitution.
- `kernel/transition.go:1–107`: Entry-only dead-plan/quality/transition verdicts. Active transition pauses only original bias direction; no plan/snapshot evaluated here, caller owns state. Unknown cited scenario and absent quality fail open.
- `kernel/validator_hints.go:1–216`: Registry checks declared condition tokens against defaults and rule-shaped tokens against field-specific enums; derived EntryLaw style covered. Guard is registry-scoped, not all strings in repo; valid-live vocabulary emitted for repairs.

---

<a id="section-14"></a>

> Original: [reviews/09/report.md](reviews/09/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 09 — kernel engine, clocks and risk

Reviewed source base `63968be62e44db2fb07a92883e02127b9064b0be` in `/tmp/vl-understanding-market-20260913`; `pwd`, revision and empty porcelain status verified on entry. All **30 assigned source files / 7,725 lines / 253 named functions plus 9 grouped callbacks** read in full. No source, config, database, account, order or service changes. No test run or runtime inspection was performed; root owns reproductions/repairs. This is a source-understanding result, not a production safety certification.

[A] means directly read source, not reproduced behavior. [B] means inference from stated code. All issues below are **static concerns / UNVERIFIED at runtime**. Historical source comments describing incidents are not fresh incident evidence. No live row/PnL claim is made here.

Operating context read: root AGENTS instruction, tracked `CLAUDE-canon.md`, `AUDIT-CHECKLIST.md` R1–R10 at 2281–2316, relevant `SYSTEM-MAP.md` bars/planner sections and rulebook trade/risk sections. `kernel/AGENTS.md` does not exist at this base. Lock-keeper canon supersedes obsolete hand-heartbeat instructions. Shared worktree was provided by root under its already-claimed review dispatch; worker changed only `/tmp/vl-review-09`.

Spec freshness, read at the assigned base:

```
565e8fbe Sun Sep 13 00:39:02 2026 -0500 fix: use owner daily-loss controls without requiring a per-trade cap
```

This is `git log -1 --format='%h %ad %s'` for both `docs/superpowers/SYSTEM-MAP.md` and `docs/superpowers/VL-TRADING-RULEBOOK-v1.md`. Owner's September 13 correction applies: daily-loss controls remain configurable; **no additional mandatory per-trade dollar cap** is proposed. Structural reject-arm composition lives outside this assignment and supersedes old blanket ATR stop-floor prose for that route.

### End-to-end model and authority boundaries

[A] `kernel/engine.go:96–164` defines the shared Context: account, positions, candidates, statistics, market data, structural state, plan gates, explicit market snapshot and owner tags. `Decision` at 167–198 is the serialized action/bracket/size result; `FullDecision` at 201–217 carries prompts, response and explicit SkipReason. These are high-cascade contracts. `StrategyEngine` at 253–301 holds a config pointer and mutable per-cycle prompt context; setters 305–337 do not lock. A caller must supply a nonnil config and own its synchronization; constructor 341–375 initializes VLOS/wallet routing but performs no order execution.

[A] Candidate selection `GetCandidateCoins` 405–609 normalizes static symbols, honors per-source enable switches, falls back to static when a single source is disabled, and merges sources in mixed mode. Mixed failures are logged/skipped, single-source failures propagate, unknown source errors. Exclusions are normalized at 612–635. Static lists are not deduplicated; mixed output is Go-map order. Futures remain a configured static symbol on the intended NT8 route; presence of legacy VLOS/Hyperliquid helpers is not evidence those feeds power futures.

[A] Dependency excerpts from `engine_analysis.go` establish actual ordering: pre-prompt risk branch at 110–165 resolves strategy concurrency cap and daily guard inputs; data snapshot stamped at 286–295 and fetched only if map empty; ownership checked 537 before system/user prompt calls 543/546; post-parse price armor then `applyStaleDataBlock` at 621. That means the assigned pure helpers have very different authority: prompt descriptions merely inform, `validateDecision` refuses a parsed entry, stale-data rewrites an opening action to wait, whereas regime/touch/shadow replay compute observations only. The trader/broker retains order and SIM authority.

[A] `BuildSystemPrompt` (`engine_prompt.go:19–177`) routes futures variants to the dedicated builder, with the rest being the crypto template. `BuildUserPrompt` 286–480 is shared and renders account, resolved history, positions, candidates, per-timeframe data, structure and ranking inputs. Tables share `FormatCandleTableNoted` 839–867 and `FormatIndicatorState` 885–972. `prompt_ownership.go:17–33` checks only supplied owner tags; no tags, no TraderID, or untagged fields are not evidence of ownership validation. A mismatch fails the outer cycle before the AI call.

[A] `validateDecisions` (`engine_position.go:31–38`) wraps the first per-item failure; `validateDecision` 43–310 accepts six action strings. All sizing/bracket/gate checks apply only to opens. It clamps excessive leverage; rejects nonpositive size/SL/TP and invalid SL/TP direction; resolves entry from `Decision.Price` then market snapshot; compares real reward/risk against configured floor (fallback3). Minimum confidence, ATR stop width/anchor clearance, HTF veto, transition pause, dead-plan and scenario quality follow. Dead-plan is a `GateRefusalError` (17) to bypass futile parse-repair; other refusal classes remain ordinary errors. Missing entry means R:R unassessed; missing ATR means ATR leg unassessed. No finite-number check exists inside this primitive; upstream JSON/producer contracts must supply finite data. This is a static input contract, not a demonstrated NaN injection.

### Time, sessions, risk and failure semantics

[A] `tz.go:24–87` centralizes America/Chicago loading and date/minute/second formatting. `SessionRegistry` defines NY08:30–14:45, read08:00, enabled; ASIA17:00–02:00/read16:30 and LONDON02:00–08:30/read01:30 disabled by default (`session_registry.go:86–121`). `ActiveSession` 193–200 is first window match, regardless enabled; `IsNightMode` 206–209 then checks enabled. Windows delegate wrapped half-open CT predicate. Exact-minute reads are just primitives; scheduling/dedupe is outside this file. Calendar early closes pull flat inward only if after start and before normal flat (224–247).

[A] Generic `RiskLimits.CheckPreTrade` (`risk_limits.go:64–81`) is nil-safe and treats nonpositive configured limits as disabled: daily loss strictly below negative limit, open count >= cap, combined notional > cap. `DailyGuardrails.Check` 328–342 instead trips daily loss **at equality**, honors master and per-leg toggles, and returns ForceFlat enum on loss; that enum alone does not close an order. `CheckSoft` 300–320 reports configured would-trips regardless switches and cannot enforce. `forceFlatReason` is process memory keyed trader; setters/read/clear are locked (177–210), absent is empty/pass. Reset functions 212–264 change CME session key and clear trip latches, **not account/database PnL**. Manual reset clears every trader's latch. `MaybeResetDaily` releases date mutex before taking latch mutex and reacquires; atomicity of a concurrent fresh trip and reset is not guaranteed by these separate locks.

[A] Contracts/notional resolvers 372–436 are separate from daily guard master: positive strategy value otherwise supplied fallback, Stage-A ceiling positive env/default1. `ClampStageAContracts` is an upper clamp only: n<=0 remains <=0. Thus comments promising “never0” depend on positive caller defaults. No environment value was inspected and no size change requested. `ConsistencyBreached` 446–454 requires positive today/total and prior-day profit and trips at >= percentage threshold.

[A] `stale_data.go:82–113` judges newest1m if present, else5m, against expected period-open minus one period and grace(default15s). If neither exists it uses freshest subscribed open and flat policy(default600s). This does not compare all feeds or choose freshest between1m and5m. `applyStaleDataBlock` 118–165 uses `Context.SnapshotMs`, leaves closes/holds untouched, and records refusal/counters when rewriting opens to wait. Missing snapshot or no usable bar is fail-open. `feed_policy.go:47–74` supplies flat600/in-position120s thresholds and any-timeframe age. Very coarse subscribed bars can legitimately be older than600s while forming; fallback policy is age-of-open, not receipt liveness. Negative ages remain negative.

[A] `EnsureMarketData` (`ensure_market_data.go:20–33`) stamps `time.Now` even when reusing an existing map. The outer decision path does the same at `engine_analysis.go:286–295`. The stamp is evaluation/assembly time, not a guarantee a reused map was freshly fetched. `formatPositionInfo` (`engine_prompt.go:482–520`) independently reads wall clock for holding duration. No runtime skew quantified.

[A] `FlipEvalAge` (`flip_freshness.go:55–64`) uses `AcceptanceBars` and finds last actually closed bucket; `FlipEvalAllowed` 68–78 skips missing/stale evaluation. Dependency `scenario_facts.go:176–210` **does stamp close=end−1**. `plan_lifecycle.go:390–433` evaluates death first, then flip with min hold, and skips stale state transitions. **Negative result:** the zero-CloseTime defect in the unrelated `AggregateBars` does not affect this traced flip path.

### Planner facts, disclosure and entry law

[A] `entry_law.go:38–77` is the condition-to-confirm map: fades reject/FVG touch-only, double5m only continuation, sweep split has first touch then MSS or single5m. `ValidateEntryLaw` 134–197 validates confirms/confirm2, split roles and enabled fade stop≥two MNQ ticks beyond ref. Nil/unknown scenarios rely on upstream schema checks. `ConfirmRuleRows/Table` 219–244 derive the prompt table from the actual map. Stop-entry seam default is OFF (107–109); offset2/wait6 are env-resolved.

[A] `prompt_contract.go:38–146` registers19 restriction fragments; `ValidatePromptContracts` 150–159 checks substrings and the boot formatter 164–172 tests rendered planner output contract. It guarantees those enumerated fragments, not exhaustive semantic equivalence of every validator branch. `golden_selfcheck.go:94–127` rerenders three fixed futures fixtures against embedded golden bytes, reporting hashes and first differing line. This verifies fixture contracts inside the binary, not deployed broker behavior.

[A] `void_scope.go:52–124` resolves shared provider1m depth(default2000), clamps CME session-day start to earliest served bar and records horizon using caller clock. Provider missing/empty produces no void verdict; no fabricated values. `class45_feeds_forward.go:45–100` builds minimal scenarios and delegates to `BreakdownContinueState`, probes both sides of scored levels, deduplicates and sorts. Renderer104–172 collapses bidirectional voids into CHOP and retains one-sided timestamps. Stop-floor facts 178–184 print actual multiplier/ATR, but blanket “executor widens” wording needs reconciliation with structurally composed reject arms (rulebook181–199); this worker did not rereview root-owned structural implementation.

[A] `fvg_entry.go:70–122` searches closed contiguous three-bar gaps with symbol gap floor and recent displacement; positive5m ATR enables its displacement filter, unavailable ATR leaves candidates with0DispATR. `validateOneFvgEntry` 177–274, however, **requires** sufficient5m history and positive ATR, checks origin only when labels map nonempty, CE midpoint tolerance, mode and declared direction. Candidate/validator cold-start requirements disagree. Neither scan checks whether intervening price already filled the gap; freshness here is bounded lookback, not a persistent unmitigated state. Advisory evaluator328–377 uses latest closed close for distal invalidation and counts all since-birth bar ranges for touches (including forming bars); invalidation is not latched here. A supplied unknown direction is treated short; upstream enum validation owns that boundary.

[A] `candle_disclosure.go:68–404` discloses holdings without changing OHLCV. Coverage derives from closed1m observations, calendar open intervals, epoch or17:00CT session windows, and distinguishes unknown from zero. `absentAggregateRows`204–221 and `absentSessionRows`225–243 scan whole missing windows; cap is20,000. `measureRow`261–284 checks calendar coverage of start year then bounded interval scan; coverage of a later year in a cross-year window depends on the external calendar helper (not fully audited here). `tableHeading`341–382 exposes held/requested and absent rows; same row notes flow through common candle formatter. Counts assume ordered distinct1m bars; duplicated or out-of-order input can distort counts/open price unless provider enforces its contract.

### Observations, learning and research-only paths

[A] `ComputeRegime` (`regime.go:65–131`) computes price vs dailyEMA200/hourEMA50 with0.05% deadband; dailyATR14 percentile thresholds25/75/90; realized5m log-return population SD*sqrt288*100; optional VIX-derived expected daily range, otherwiseATR; overnight gap/dATR. It does not itself remove forming bars. `RVBaselineFrom5mDays` (`regime_baseline.go:69–112`) buckets by CME day and treats>=2005m bars as complete; averages up to maxDays after testing minDays. This is a near-complete **count proxy**, not proof session ended or every expected bar exists. A developing session with200bars can enter the baseline. `regime_dark.go:32–90` marks seven fields and degraded iff count>3(default). Valid zero realized variance is classed dark and omitted by regime rendering; current types do not retain a computed flag for that field.

[A] `adherence.go:48–81` grades discipline independently of PnL. Off-plan=D, matched=A, matched-but-offband/structure=B, mismatched=C; no-trade and outside-killzone each step down. `SummarizeAdherence`94–109 excludes unknown grade letters. `SessionWindowFacts`115–131 shares actual lunch/first-minutes predicates but does not itself check news blackout. `digest.go:12–65` formats session/day PnL and keeps three full plus four headline daily digests; `LearningLine`84–116 correctly includes measured zero excursions, counts measured sample and excludes unmeasured. If no excursion measured it omits otherwise available grades entirely.

[A] Legacy `ComputeExcursion` (`mae_mfe.go:24–51`) filters by open timestamp and drops mid-bar entry's containing bar. New `ComputePathExcursion` (`excursion_path.go:52–101`) includes intersecting bars and explicitly distinguishes unavailable; it cannot resolve before-fill/after-exit extrema inside those intersecting bars. Ambiguous stop+target bars counted and separate pessimistic resolver111–123 returns stop. Both side classifiers treat unknown as short; path metrics assume ordered valid source bars. These are analytics, not execution fills.

[A] `EvaluateMatchedRandom` (`matched_random.go:108–144`) sorts types, computes reaction fraction against fixed0.5 and one-sided normal approximation, then enforces n>=1565 **before** significant effect>=.05 and p<.05/8. It protects against underpowered green results but does not construct a matched random sample. Dependency `trader/auto_trader_matched_random.go:20–52` records **closed-trade excursion proxy MFE>MAE**, linked to nearest level in latest plan for entry session, not independently sampled real touches vs controls. Weekly branch56–91 freezes SundayCT once per ISO week. Naming a result “BEATS-RANDOM” is stronger than what this sample construction establishes. Historical legacy samples were not inspected.

[A] `ShadowABForScenario` (`shadow_ab.go:51–271`) computes four alternative fills with directional risk/reward and stop-first ambiguity, two-tick roundtrip friction, then returns rows. Its source comments correctly separate record-only authority. Actual recorder `trader/armed_executor.go:962–1000` has recovery, calls once per cycle, and skips rows already stored by plan/version/scenario/rule. Therefore a first stored open outcome is not visibly refreshed at this callsite. No store behavior or resulting live rows certified.

[A] `TouchUpdate` (`touch_telemetry.go:187–278`) uses process-wide mutex state and bounded per-level ring, seeds stored ordinal per session-day, consumes new bar timestamps and returns closed episode copies. Sink/seed setters143/161 are intended one-time startup injection. Episode metrics and legacy close-side classifications are advisory; the calibrated detector is outside scope. A new episode is opened from latest observed bar only, not replayed for all unseen bars; this is sampling by caller cadence. Active episode vol/approach metrics populate only on close, while active rendering tries to display them, so active lines commonly retain n/a. No order decisions are made here.

### Evidence-backed issues to hand to root

Severity here is review priority, not live incident severity.

1. **A / static / UNVERIFIED runtime — close-confirm counterfactual clock is wrong.** `AggregateBars` (`fvg_entry.go:290–312`) never sets CloseTime; shadow closeFill (`shadow_ab.go:121–140`) uses that zero to accept a bucket as closed at any positive now. It can use a still-forming bucket's current close, including bars beyond the replay clock if input contains them. It then maps fill to FIRST1m bucket bar113–120 and replays from there172–232, admitting pre-confirmation extrema and backdating TimeToFill. Fixing close metadata alone does not fix the earlier replay start. `logShadowAB` persists once, compounding stale early measurements. No actual trade failure claimed; report-only research output can mislead evaluation.
2. **B / static / UNVERIFIED runtime — shadow/live env precedence contradicts documented control.** `condition_status.go:75–94` appends SHADOW then LIVE;34–64 returns first matching token. Setting same condition in both gives shadow despite “LIVE highest env priority.” Existing full-read tests `condition_status_test.go:53–64` use disjoint lists and only assert composition text. Configuration maps still outrank env and work as written. Affects intended enablement and resolved ledger; not a SIM bypass.
3. **B / static / UNVERIFIED runtime — touch card concurrency and stale-day state.** `TouchStateForCard`520–549 unlocks after retrieving pointer then reads active/last while `TouchUpdate` changes them under mutex. `ActiveTouchEpisodes`448–462 filters prefix only, including earlier session-day states indefinitely; no pruning in assigned registry. A prior-day still-active episode can contaminate next-day prompt facts. Requires actual concurrent call schedule for race; no race run performed.
4. **B / static / UNVERIFIED runtime — legacy5m touch metric is effectively last1m.** `closeSide5m`355–375 compares adjacent OpenTime difference<5min rather than bucket id. A continuous1m sequence repeatedly replaces one close and reports latest1m close, not last completed5m. The caller provides closed1m bars, reducing the separate theoretical empty-slice panic branch; no production panic asserted.
5. **B / static / UNVERIFIED runtime — statistical family/sample definition discrepancy.** `LevelTypeFromLabel`31–55 emits nine named families plus other, while Bonferroni divisor is8 at27. More importantly closed-trade favorable/adverse proxy does not establish an independently matched random control. Fixed cadence and N gate are positive safeguards, not remedies for that estimand mismatch.
6. **B / static / UNVERIFIED runtime — FVG candidate/validator promise and mitigation mismatch.** Candidate scan70–122 permits noATR, write validator246–259 refuses it; both find historical gap without checking subsequent fill. Root should decide desired validity semantics before any change; do not silently introduce an extra gate or enable shadow condition.
7. **C / static — generic external fetch resource limits.** `engine.go:776–820` validates URL and uses SafeHTTPClient, but reads body with unbounded io.ReadAll and does not inspect HTTP status. Negative RefreshSecs is not repaired locally. Safe client internals not read; no SSRF exploit or timeout bypass claimed. Historical graph's “size caps” assertion is unsupported by this function.
8. **C / static — narrower contracts than prose.** `ValidateSessionRegistry`151–178 accepts overlapping/duplicate sessions and does not enforce end==flat; actual active session first-wins. `CTLocation` falls backUTC yet formatters labelCT. `ResolveMaxContracts`/notional fallback can return0 when supplied0 despite NEVER0 comments. `regime_baseline` “complete” means>=200bars. Treat these as boundary conditions to verify at producers, not proof live config is invalid.

Additional prompt accuracy concerns: crypto system template says decision-first (143) but reasoning-first process/example (134,146–153); labels configured R:R/confidence AI-guided despite code enforcement (87–91); describes ratio as take_profit/stop_loss instead of reward/risk. Shared position formatter497–505 lacks futures point-value factor and uses USDT labels. `RenderVoidBreakdownLevels` final sentence170 forbids both continuation directions even when preceding line says only one side reclaimed. These are source-level disagreements; no AI behavior or losses attributed.

A concurrency cap in dependency `engine_analysis.go:137–143` returns HOLD before prompt assembly even while at maximum open positions, so AI position management is also skipped at cap. This is an existing control-path consequence; other watcher/broker management may still run. Not evaluated as a complete exit-path defect in this assignment.

### Tests and graph evidence

Fully read tests: `condition_status_test.go`, `flip_freshness_test.go`, `shadow_ab_short_test.go`, `regime_baseline_test.go`. They respectively pin default/map/env composition (not overlapping env lists), fresh/stale/no-closed flip behavior and lifecycle, long/short replay arithmetic (rows[0] mostly touch rather than every close-rule row), and count-based baseline/warming consumption. Test names discovered but **not full-read**: risk limits, candle disclosure, touch telemetry, general shadow replay. Their existence is not a passing result. Root can choose focused tests for env overlap, pre-confirmation replay, partial5m bucket, day rollover and concurrent card read. No new test files written here.

Historical Understand Anything graph read for all assigned-path nodes and touching edges: **52 nodes / 287 edges**, covering only engine.go, engine_position.go, engine_prompt.go, risk_limits.go; other26 assigned files absent. Snapshot July10@7a8adce0 is stale. `graph.json` preserves selected historical evidence and corrections. The graph falsely calls quant batch concurrent (source sequential), external reads size-capped (unbounded locally), quant source CoinAnk (VLOS client boundary), risk struct contract-capped (three fields only), and cap resolvers switch-gated (no switches in signatures). It also expands package imports into per-file/test edges; these are not literal Go imports or runtime call traces.

Current root AST inventory provides253 named declarations and9 callbacks, and source-expression call edges included in `functions.json`. Edges remain syntax-only; no method receiver/type resolution invented. CGC export is root-owned and historical; this worker did not query, reindex or certify it current. Important traced current edges: outer prompt ownership/build, stale-data postparse, AcceptanceBars true-close aggregation, and arm shadow recorder. `reads.json` records assigned full coverage and separately scoped extra reads with hashes; `functions.json` supplies every named function's exact start/end, purpose, calls and invariants. No unread assigned source remains.

---

<a id="section-15"></a>

> Original: [reviews/10/report.md](reviews/10/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 10 — kernel levels and structure

Source review at `63968be62e44db2fb07a92883e02127b9064b0be`, isolated `/tmp/vl-understanding-surfaces-20260913`; worktree clean before reading. [A] Read all 25 assigned files, 7,199 lines. `functions.json` records 217 named declarations with exact start/end lines and syntax-only call expressions; 31 callbacks are grouped under parents where applicable. `reads.json` distinguishes assigned full reads from additional excerpts. No source/config/DB modifications, test execution, live probes, or deployments. Findings below are source behavior and static risks, not demonstrated incidents.

Authority: main AGENTS instruction file, tracked CLAUDE-canon, AUDIT-CHECKLIST R1–R10 and classes 64/98. Latest tracked canon supersedes the old hand-heartbeat instruction. No lock operation was needed in this already-provisioned read-only lane. Scope follows the root's claimed dispatch. Owner correction retained: loss control is DAILY; this review proposes no mandatory per-trade cap.

Spec freshness at this base: `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap` is `git log -1` for both `docs/superpowers/SYSTEM-MAP.md` and `docs/superpowers/VL-TRADING-RULEBOOK-v1.md`. These were read as descriptive authorities, not implementation instructions. No build was based on a historical spec.

### Flow and ownership

[A] `levels.go:71–115` provides DetectedLevel, including separate ordinary JSON fields, recording-only formation-close identity, research capture pointers, and collapsed names. `lineLevel:118–120` does not invent formation-open timestamps; `zoneLevel:123–128` normalizes bounds. Legacy cross-session state identity (`level_identity.go:15–17`) uses absolute 1.25-point bins, while scenario identities use structured inputs and a hash (`scenario_level_identity.go:79–107`). These are distinct contracts.

[A] `AssembleResearchLevels` (`levels_assemble.go:212–253`) filters closed bars, uses latest close, resolves daily-range/ATR fallbacks, emits multi-day/round/opening/gap/equal/zone/volume/swing detectors plus supplied extras, attaches identity context, preserves raw research output, deduplicates kind+TF within 0.25, and returns seated/pool/raw. Older assembly entry points duplicate this sequence. Trader production call at `trader/auto_trader_planner.go:2187` consumes all outputs. The kernel is DB-blind, but injected `LevelStateProvider` is trader+symbol scoped and nil means fresh (`level_state_provider.go:18–27`); globals are intended to be installed at boot, not mutated concurrently.

[A] Multi-day extraction (`levels_multiday.go:42–270`) groups prior calendar-day anchors separately from current CME overnight anchors; prior-day/week/month minimum counts are 900/4320/10080 (`:279–281`). `mostRecentPriorDay` can select an older sufficiently populated day. These are count checks, not continuity checks. `OpeningRangeLevels` (`levels_intraday.go:122–205`) emits developing 5-minute OR and 60-minute IB, plus extensions; formation evidence remains unknown until the actual endpoint is present. Round references use 25/50/100 spacing (`:20–50`); gap-fill targets are omitted after later fill (`:56–119`).

[A] `levels_zones.go` supplies closed filtering (`:21–30`), strict k=2 pivots and equal clustering (`:34–120`), base/departure supply-demand (`:125–179`), FVG/IFVG including a three-candle continuity guard (`:207–300`), and bounded opposing-candle OB lookup (`:323–357`). HTF pass (`levels_assemble.go:344–408`) canonicalizes D/W aliases, runs four detector families on requested supported TFs with 500-bar fetch and five-bar minimum, tags provenance, and reports counts/skips. Supported set reaches 1d/3d/1w (`:582`); a base caller does not itself guarantee every TF was fetched.

[A] Swing levels (`levels_swing.go:38–177`) aggregate 5m/15m, find alternating significant strict pivots, record confirmation-close time, and retain newest three each side within 144/96 bars. Structure (`structure.go:173–394`) shares significance concepts but computes its own snapshot, HH/HL/LH/LL trend and BOS/CHoCH/MSS/sweep events; `StructureSnapshot:398–415` uses a separate StructureAggregateToMinutes implementation. The 1m MSS primitive (`mss.go:94–145`) is another distinct contract: last confirmed k=2 swing, closing break, displacement versus ATR5m, optional ordered-after bound. It is not the same definition as structure's CHoCH plus body displacement.

[A] Volume assembly (`levels_volume.go:419–429`) combines 17:00 session VWAP/deviations, 15:00 eVWAP, prior profile, naked POC, prior VWAP, settlement proxy and MID-O. `profileLevels:171–250` puts each candle's volume in one of 120 close bins and expands one row at a time. Separately `svp.go:141–392` distributes volume uniformly across fixed 1.25-point rows, splits candle direction, and expands two rows at a time around POC to 70%. Both are OHLCV approximations, not tick-level traded volume. SVP sessions use 17:00 CME roll, mark frozen via IsCMEOpen, and mark partial only from earliest bar lateness. `naked_poc.go:33–73` handles persisted profiles via a provider and later-session tick brackets; unlike `NakedPOCLevels`, this helper has no ten-day loop.

[A] Scoring (`levels_score.go:443–638`) uses proximity in daily-range units, evidence/freshness, capped distinct-family confluence, and TF/HTF modifiers. Zone grades have floors/caps and Tier-1 proximity checks. Clustering retains priority before score and excludes zones from line collapse (`:761–822`). Seating runs HTF, volume, both-side stages then cap; grade-filtered path has separate re-seating (`:657–705`). These grades are heuristics, not probabilities. Research pointer captures (`research_score.go:33–92`) travel back into raw candidate evidence without becoming JSON score inputs. Candidate recorder (`detector_recorder.go:35–107`) emits every constructor candidate with seated rank, captured score/components and cut reason; fallback reasons are inferred from inputs, not an independent rerun.

[A] There are TWO presentation layers. Map candidates (`map_candidates.go:133–279`) merge strongest-price references within fixed width, carry names/identities, test nearest inward reference against minimum target distance, then order by reachability. Projections (`map_projections.go:67–211`) are never entry candidates. Full zones (`level_zones.go:149–262`) instead consume raw detector sources, keep native or derived widths, preserve unknown widths, avoid transitive merging, isolate broad context, and rank touches/round proximity/family count/distance. `LevelZoneInputs:24–79` uses frozen series. Production `auto_trader_planner.go:2521–2523` builds/stores this view on PlannerInput. Display shortlist alone is not trade authorization; structural execution consumers outside this assignment require separate review.

[A] Scenario identity (`scenario_level_identity.go`) copies machine metadata to uniquely matching authored prices, validates IDs against their inputs, preserves source-member IDs, and fails unresolved for conflicting duplicates. `StampAuthoredIdentity:159–219` is WARN-only; `ResolveScenarioIdentity:130–146` records disagreement with the evaluator anchor beyond 3 points. It does not rewrite evaluator geometry. `EpisodeLevelID:241–268` and `EpisodeScenarioByID:273–288` preserve ambiguous attribution as NULL.

[A] D1′ (`detector_d1prime.go:120–187`) opens only on fresh exact level touches, looks after the opening candle for strict exits beyond ±kΔ, records hold/break/ambiguous span/horizon and skips overlapping episodes. Default k=3/H=12/close is environment-resolved. `HoldRate:200–219`, Wilson interval and format preserve sample size/excluded ambiguity and descriptive floor 200. `NewEpisodesSince:114–122` retains completed episodes with opens strictly beyond persisted watermark. Legacy `EvaluateLevelOutcome` still computes retired either-direction reacted flags, and consumers must not reinterpret that as hold probability. No historical calibration numbers quoted in comments were independently reproduced here.

### Static findings for root triage

1. **[A] Wrong candle supplies swing defining wick.** `levels_swing.go:108` stores pivot `OpenTime + iv`; `:167–171` searches a bar whose **OpenTime equals that close instant**, selecting the next candle when contiguous. SYSTEM-MAP says exact defining wick. This propagates to `LevelZoneInputs` and width/rank/map provenance. No runtime impact reproduced; source contradiction is direct. Also line levels do not carry FormedAtMs, so the fallback cannot recover a missing pivot wick reliably.

2. **[A] Initial volume omitted in aggregateBars.** `levels_swing.go:203–210` creates a bucket with OHLC but no volume and immediately continues; volume accumulation begins at `:219` on subsequent candles. Only SwingPointLevels calls this helper in the inspected search; its current swing logic does not use volume, so present price-selection impact is NOT established. Do not attribute the defect to StructureAggregateToMinutes, a different function.

3. **[A] Seat1HZone knob's production calls receive already capped slices.** `levels_assemble.go:32–37` and `trader/auto_trader_planner.go:2187–2191,2209–2214` call it after capped scoring; `levels_score.go:950–952` immediately returns when len<=cap. Thus these call sites cannot promote an excluded candidate. Additionally Seat1HZone (`:990–1011`) and seatHTF (`:1077–1098`) globally sort head+tail after a swap, allowing the stronger demoted row to reclaim the seat. Existing test excerpt (`levels_htf_test.go:212–256`) uses a 0.9 candidate above 0.5 demotable rows, so it does not challenge this failure direction or capped production call site. Static concern, not a claimed trade failure.

4. **[A] Prior profile cache has no symbol or input provenance key and freezes partial first results.** `levels_volume.go:134–166` uses package-global sync.Map keyed only by day, returns shared []DetectedLevel and caches any nonempty profile. Function accepts no symbol, so different bar series at same day can reuse one result; more complete later history does not refresh. CaptureIdentityContext mutates extra metadata of emitted copied levels after appending; independent caller mutation of returned cache slice remains possible. No multi-symbol incident asserted (task architecture is MNQ).

5. **[A] Empty minimum grade differs from advertised parity/side guarantee.** `levels_score.go:666–676` first scores a 2× pool, returned nearest-first, then slices first cap when minGrade empty. This is not generally identical to ScoreLevels' priority/cap order. The 2× pool side balance cannot ensure first N nearest rows retain both sides. Exact output case not executed. Golden test intentionally pins current outputs and cannot prove semantic guarantee.

6. **[A] Projection input labels overstate evidence.** `levels_assemble.go:86–87` calls ProjectSessionExtreme with first closed bar of supplied history, not a filtered session open. `:90` calls measured move with seated-map min/max, not a last completed swing. `map_projections.go:98–110` accepts any one prior-week daily bar and does not check CloseTime; DailyBarsFor returns provider data unchanged (`:206–211`). Missing-source guards exist, but full prior-week coverage and closed daily provenance are caller assumptions. `DailyRangeProxy:639–642` uses developing range fallback despite its doc saying no completed day returns zero. These distinctions matter when interpreting projected ranges; no changes authorized.

7. **[A] Role override parser does not canonicalize case as intended.** `levels_role.go:84–86` tests `!EqualFold(kind, ToUpper(kind))`, false for ordinary case variants, leaving lowercase keys unmatched. `RoleFor:125–129` overwrites an override for kinds absent in defaultRoleMap because original `ok` remains false. KindVWAP2S/KindOwner are absent; label reverse mapping maps ±2σ to plain VWAP and unknown to RN. Existing explicit ScoredLevel.Role should be preferred where available; ComputeBiasContext recomputes from display Fresh, and `flipped` is not recognized as consumed (`:109–130,208–214`). Static behavior, no config mutation.

8. **[A] Structure event order differs from comment.** `structure.go:318–377` collects descending-time events; `:381–386` reverses them and stops at three, retaining older-first inside recent window. Prompt selects max time among retained entries, so a newer event can be lost when >3 are eligible. Also lastHigh/lastLow initialize to newest even if the opposite swing is absent. No fixture reproduced.

9. **[A] HTF observability inconsistencies.** Counts/Skipped documented disjoint, but duplicate canonical TF request writes Skipped even after Counts exists (`levels_assemble.go:361–363`). TFReadLine iterates raw Requested, canonicalizes and can double-count aliases (`:458–473`). TFsWithLevels reads noncanonical requested keys (`:324–327`), missing alias counts. Detector results remain deduplicated, so issue is reported counts/reasons rather than duplicated detector production.

10. **[A] Legacy stats documentation mismatches.** `GradeOutcomeBuckets:119–141` ORs flags, not per-grade counts despite comment. Break loop at `level_stats_calc.go:94–109` skips lookback check until a first break appears, so a far-later initial break can qualify. Reacted is explicitly retired; do not revive rates. `detector_recorder.go:49` key omits TF/birth and components comment says `{}` while serializer returns `null` when absent (`research_score.go:62–70`). Captured production evidence is stronger than fallback inference but identity collision remains a static limitation.

11. **[A] Missing/nonfinite inputs are not uniformly refused.** `EvaluateMSS` skips displacement floor when ATR5m<=0 (`mss.go:127`); positive float resolvers accept +Inf, notably detector k and projection multiplier. Many loops assume finite ordered OHLC and consistent timestamps. This is an input-contract issue; upstream validation and practical reachability were not exhaustively reviewed, and no malicious input testing occurred.

### Documentation disagreements and evidence limits

[A] Rulebook Part 3 still lists HTF only through 12h (`:175`) while current whitelist includes 1d/3d/1w. SYSTEM-MAP's zone exact-wick statement conflicts with finding 1. Its PWH/PWL table row calls the 4320-bar extractor a projection although the daily projector has a separate implementation. `svp.go` header says RTH but actual grouping and lower constants clearly use 17:00 CME day. Several opening summaries in levels_score/levels_zones say zones always C/confluence-only; current HTF grading contradicts that simplification. The report does not equate source-on-branch with running binary or reproduce old historical incident counts.

[A] July-10 Understand Anything graph (7a8adce0) has zero `filePath` matches for these 25 files and therefore zero associated edges under exact current path lookup. This is an explicit negative result, not complete graph coverage. Root's AST inventory supplies syntax-only edges; no type resolution is inferred. Root owns CGC evidence export; this worker did not independently query CGC or reindex.

Tests inspected: stage_a_parity_test.go full (64-case claimed text, actual nested 4×4×4=64); levels_htf_test.go promotion/no-op excerpt; level_zones_test.go future-bar/birth handling excerpt. Test-name searches additionally locate detector parity/calibration/ambiguity, structure mirror/ATR, profile cache, map projections, level identity and scoring suites. They were not executed, and those name searches are not counted as full test reads. Broader snapshot/replay and actual call-site regression reproduction remain the root's test plan. Static read produces no claim of live exploit, live loss, or repaired behavior.

---

<a id="section-16"></a>

> Original: [reviews/11/report.md](reviews/11/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 11 — kernel plans and permissions

Source-only review at `63968be62e44db2fb07a92883e02127b9064b0be`, worktree `/tmp/vl-understanding-surfaces-20260913`. Initial `pwd`, revision and porcelain status verified [A]; tree clean. All **22 assigned files, 7,100 lines**, read in full; **215 named functions/methods** catalogued, **13 anonymous callbacks** grouped under parents. Additional dependency and test excerpts are explicitly distinguished in reads.json. No code, settings, account, DB, live service or order mutations. No tests executed; findings below are source-proven paths or static limitations, never asserted runtime incidents.

Rules consulted: main AGENTS.md instruction file, tracked CLAUDE-canon (keeper supersedes obsolete hand-heartbeats), AUDIT-CHECKLIST classes 2/7/8/11/13/15 and R1–R10, SYSTEM-MAP planner/validator sections, RULEBOOK one-setup/trade sections. `kernel/AGENTS.md` is absent in this pinned worktree. This is the assigned source revision, not an independently verified running binary. Owner correction honored: daily loss, no extra mandatory per-trade cap. Neither limits nor switches were read or changed.

Spec freshness, from this pinned tree:

- `git log -1 --format='%h %s' -- docs/superpowers/SYSTEM-MAP.md`: `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap`.
- Same command for `docs/superpowers/VL-TRADING-RULEBOOK-v1.md`: identical `565e8fbe` line.

### Data and ownership flow

[A] `PlannerInput` in planner_prompt.go:25–131 carries supplied clock, session/date, market facts, ranked references, candle coverage, weekly context, enabled indicator rendering and prior-plan continuity. `BuildPlannerPrompt` (439–736) assembles those sections; `plannerOutputContract` (743–828) appends model instructions. `RenderPlannerIndicatorBlock` (planner_indicators.go:22–47) uses executor `FormatIndicatorState`, deduplicating requested TFs and omitting unavailable output. `BuildPlannerCandleTablesAt` (planner_prompt.go:386–434) trims to displayed rows before measuring coverage, with held/requested and absent-window disclosure.

[A] The trader retry path calls `ParsePlanDocCapped` at trader/auto_trader_planner.go:1605. plan_doc.go:468–486 extracts a balanced JSON object, validates schema/arm/entry law, then invokes new-authoring economics. Default `ParsePlanDoc` deliberately skips new economics but retains default 8/3 caps; capped authoring supports up to 12/5. Validation is not one all-encompassing function: `ValidatePlanDocWithCaps` (616–766) handles schema, arms and entry law; `ValidatePlanDocWithFactsMachine` (887–963) adds market facts; additional trader-side validators remain separate. SYSTEM-MAP's claim that every write-site validator is chained through WithCaps is too broad. Collapse normalization and normalization telemetry follow parsing in trader/auto_trader_planner.go:1626–1650.

[A] Economics checks at scenario_economics.go:139–255 require new scenarios to supply geometry, first-obstacle provenance/response and stated ratios. Ratios use absolute price distances and tolerate one MNQ tick of price discrepancy. Target absent from chain needs explicit exception; sub-1R obstacle and role-use disagreement only warn. Missing legacy economics remains UNKNOWN rather than backfilled. Mutex-protected counters record checked/path-known/path-coherent/sub1/refusal observations; logging panic cannot change acceptance. This is geometry coherence, not a new daily or per-trade loss gate.

[A] Authored invalidation (plan_authored_invalidation.go:32–83) is a distinct write-time check, requiring all unique complete minute bars for the latest one/two five-minute windows. Unsupported compound prose is UNKNOWN and accepted for this check by trader/plan_liveness.go:15–41; known invalidation refuses inside the existing candidate retry loop. It neither changes existing live scenario evaluation nor independently places orders.

[A] Live plan injection is per-trader: plan_render.go:78–147 registers/looks up a sync.Map provider bundle; engine_analysis.go:477–497 resolves the deciding trader and injects RenderPlanBlock plus live status. Empty trader keys are not registered. PlanTradeDateFor prefers a valid date in plan ID; plan_chain_date.go:34–66 instead calculates session-chain dates by CT opening, handling wrapped ASIA tail with AddDate so DST does not become a fixed-24-hour approximation. dATR cache remains per trader, distinct from 5m ATR used for stale confirms.

[A] plan_lifecycle.go windows source bars by OPEN time at plan birth, aggregates rule TFs and requires touch plus acceptance for legacy consumption. Structured death/flip adds ATR deadband and configured minimum consecutive closes, mirrored for rearm; Fresh wrapper additionally skips stale data and holds young flips. Thus prose saying a single 5m close is evaluated exactly as authored omits the separate default-two-close hysteresis floor. plan_confirm.go delegates ordered machine confirmations, handles breakdown legs and renders stale/conflict advisories; it uses 5m ATR, despite one remaining comment saying dATR. Missing ATR skips advisory with a process-level bool warning guard (not synchronized).

[A] scenario_facts.go provides fixed-epoch OHLCV buckets and source-TF sweep/reclaim/reject facts versus rule-TF acceptance. scenario_state.go:85–130 resolves FVG distal edges or numeric prose anchors, then 169–219 classifies expired/invalidated/triggered/armed/waiting. Trader recording windows at plan birth and persists states (auto_trader_levelstate.go:219–243). This heuristic classifier is separate from EvaluateScenarioConfirm; its condition switch covers six older conditions, with FVG/waterfall conditions falling through false. Machine-basis labels at scenario_state.go:246–254 depend only on price coincidence with structured death/flip, not equivalent rule evaluation.

[A] one_setup.go:89–153 consumes three independently reported legs: best eligible live candidate, reject condition, evaluated permitted FadeVerdict. OFF bypasses selection. Candidate rank is grade then distance then price; projections excluded; band<=0 disables distance filtering. Caller trader/one_setup_wiring.go:45–60 refuses unresolved supplied IDs and otherwise uses explicit price fallback; live map and permission are supplied at 131–152. FadePermissionAt (fade_permission.go:129–211) independently accumulates first-N, wide OR, completed IB breakout, directional beyond-map and T1 exclusions. UNKNOWN calendar never excludes; any other evaluated input can therefore yield permitted with calendar UNKNOWN. This is actual consumed authorization now, despite fade_permission.go's obsolete 'never a gate' header.

[A] follow_plan.go:92–221 computes only records: break on completed bucket, far-side retest, passive limit through one tick, then MAE/MFE/net at 10/20 closed buckets. It cannot reach broker code. Caller trader/follow_plan_wiring.go:131–140 supplies scope/time/tape and stamps results. Touch without through is immediately final no_fill. Horizons start after retest minute; partial nonempty first bucket is possible. Null pointers distinguish unobserved break/entry/outcomes.

[A] ask_planner.go:70–126 enforces normalized reply classes and strips patches on bare disagreement, non-merge or structurally invalid patch. It does not apply anything. plan_overlay.go:29–68 decodes a private document, atomically applies each add/remove/replace/test patch, and skips failed overlays at fold time. plan_overlay_carry.go:102–152 carries owner-added absent prices; conflicts, revived deletions and structural edits become review records. Equality and ownership are inferred from base/final document differences, not original array indices.

[A] Weekly scheduling is outside session gating in trader/auto_trader_weekly.go:175–210; weekly new output is parsed and validated at 248–260. weekly_bias.go aggregates calendar-completed weeks, references, gaps and daily/IPDA ranges from ascending caller tape. Completion means past Friday close, not verified full-week minute coverage. weekly_prompt.go supplies refs-only prompts and PWH/PWL-only consumers; retained legacy directional helpers are distinct from actual refs-only rendering. Shadow reordering returns counts, never reordered live levels.

### Findings and limits, prioritized

1. **B / BROKEN [A source]: uppercase ABOVE reverses authored invalidation.** Regex at plan_authored_invalidation.go:17 is case-insensitive, but line 74 tests `m[2] == "above"` case-sensitively. `5m close ABOVE 99` with five valid minute closes at 100 parses known yet evaluates as below; with closes at 98 it falsely invalidates. Exact production refusal link: trader/plan_liveness.go:22–40. Existing full test file covers lowercase/malformed tape/compound UNKNOWN but no uppercase word. Reported to lead for isolated reproduction/repair; no runtime occurrence established.

2. **B / BROKEN [A source]: prompt contains removed and incompatible rules.** planner_prompt.go:756 demands >=3 above and >=3 below for every resolved cap, while plan_doc.go:923–930 explicitly enforces only nonzero sides; caps may be below six. planner_prompt.go:709 says no arm lunch-band predicate exists. Actual arm path calls sessionRiskGateAt (armed_executor.go:336 search), which calls sessionEntryBlockedAt (session_risk.go:123 inspected); SYSTEM-MAP:176 explicitly documents both paths. These are incorrect instructions to the model, not absent execution guards. Further stale direction guidance: final weekly-soft-law text still discusses counter-weekly bias/draw although WeeklyContextLine is refs-only; RenderPlanBlock advertises off-plan permission independent of resolved strict mode. No model-response incidence measured.

3. **B / BROKEN [A source]: accepted weekday knob is discarded.** weekly_knobs.go:23–53 parses mon..sun, but WeeklyReadDeadline:64 discards weekday and always constructs Sunday. Production scheduler uses deadline at auto_trader_weekly.go:181. Default Sunday behavior is unaffected; no current env override read.

4. **B / static validation gap [A]: weekly prices are not grounded.** ValidateWeeklyDoc (weekly_prompt.go:236–267) ignores supplied refs, accepting arbitrary positive named levels. New-output caller passes WeeklyRefSet at auto_trader_weekly.go:254, but that argument has no effect. BuildWeeklyPrompt:283 asks exact copied source prices. A fabricated positive PWH can therefore survive this validator and be rendered as weekly context. Not an observed fabrication or trade incident. Legacy directional fields are intentionally tolerated by tests; do not conflate that compatibility with missing price grounding.

5. **B / static recording gap [A]: follow scope is not a scan bound.** follow_plan.go tests ScopeEndMs only when choosing final states (123,160,217); break/retest/outcome scans use NowMs. Live recorder passes current tape and current now (follow_plan_wiring.go:131–134). A still-open prior-session record may record a next-session break or outcome if supplied bars include it. Backfill scope selection requires additional full-caller analysis; inspected backfill passes both now and scope separately. Recording-only impact. Also a forming retest minute can finalize no_fill before later ticks cross a tick; requires a live timing reproduction, not established here.

6. **B / static economic-coherence gap [A]: obstacle behind entry is not rejected.** scenarioEconomicsIssues:246–252 rejects only beyond-target obstacles. A long with entry 100, stop 90, target 120 and obstacle 95 can satisfy ratios and target membership while violating prompt's 'between entry and arm target'. Non-arm geometry checks finite positive prices/nonzero risk, not directional protective-stop/target ordering. No executable authorization is inferred from hypothetical geometry; downstream execution gates are outside this finding.

7. **C / limitations [A code, B impact]: sparse tape and heuristic truth.** aggregateToMinutes (scenario_facts.go:176–210) emits every nonempty bucket as completed after epoch end; ClosesBeyond (266–280) does not require adjacent bucket timestamps. Missing minutes or whole buckets can therefore count as consecutive observations. This differs from strict minute coverage in authored invalidation. Birth-window aggregation can include partial initial buckets; confirmation_bucket.go certifies clock closure, not completeness. Scenario machine-basis labels do not guarantee parity with buffered death logic. No live-data holes queried.

8. **C / source-local robustness [A]:** ParseWeeklyDoc:223–226 compares a last-brace index after slicing against the previous prefix offset, so sufficiently long leading prose can prevent trimming trailing prose. ComputeWeeklyFacts:49 computes IPDA from original price before fallback at 51–52. LastNWOGs ignores now and stores Monday key in Born despite Sunday comment; input must already be as-of. DailySessionBars preserves first bar CloseTime. WeeklyRuleBias compares prior completed close against the high/low of that same completed week through ComputeWeeklyFacts, so ordinary well-formed bars cannot produce its strict above-high/below-low branches. All are context/shadow artifacts, not demonstrated execution failures.

9. **C / minor patch strictness [A]:** plan_overlay.go:258–268 rejects '-' and leading zero but permits '+' through Atoi; parsePointer leaves unsupported tilde escapes intact. hasPatchOps rejects empty-path whole-document ops that ApplyPatchStrict supports. Structural carry tests a `/levels` string prefix rather than pointer segment. No unauthorized application demonstrated; HTTP validation/ownership needs its own review.

### Verification and graph comparison

Full test reads: kernel/plan_authored_invalidation_test.go (54 lines), kernel/follow_plan_test.go (159). Partial test reads: weekly_prompt_test.go:1–110 and scenario_economics_test.go:1–120. Test names for other relevant suites were inventoried only and are not claimed as full reads or executions. Existing follow tests cover both directions, no-break, touch-not-fill and forming *break*; they do not establish scope cutoff or forming-retest finalization. Economics test excerpts cover missing contract/obstacle, beyond-target and ratio contradictions, legacy compatibility and sub1R acceptance. No broad suites were run by this worker.

Historical Understand Anything graph July10@7a8adce0 has 3,121 nodes / 9,588 edges; exact filePath lookup yields **zero nodes and zero edges for all 22 assigned paths** [A]. Root-exported CGC file inventory has 781 files and **zero assigned paths** [A]. These artifacts cannot describe the new planner system. functions.json's call expressions come from pinned source AST catalog and are explicitly syntax-only, not inferred type-resolved edges. graph.json adds only four directly inspected current call-site edges. Source wins; no reindex or graph mutation performed.

No whole-repository or runtime understanding claimed. All assigned sources covered; deeper API authorization, full trader scheduling/backfill and broker placement behavior remain outside this slice.

---

<a id="section-17"></a>

> Original: [reviews/12/report.md](reviews/12/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 12 — market and other data providers

Source review at `63968be62e44db2fb07a92883e02127b9064b0be`, isolated worktree `/tmp/vl-understanding-execution-20260913`. Initial pwd/revision/status checks verified this tree and a clean porcelain status. All **49 assigned files / 7,579 lines** were read fully. `functions.json` inventories **216 named functions/methods**, exact start/end lines, semantics and observed call expressions. Anonymous callbacks are grouped under their containing declaration. `reads.json` distinguishes assigned full reads from additional dependency/test excerpts. No source/config/DB changes, runtime requests, payment calls, trades, deployments, or tests were performed.

[A] below means directly read source or measured static inventory. [B] means a consequence inferred from that source. These are static findings, not reproduced runtime incidents. No live rows were inspected, so no P&L or account claims are made. The owner's daily-loss clarification remains authoritative; the optional pure `market.PositionSize` helper is not evidence that an additional mandatory per-trade cap should exist.

### Operating and documentary evidence

Read the supplied/main instruction file and tracked `CLAUDE-canon.md`, including its corrected keeper-owned heartbeat and checked-worktree verbs. `market/AGENTS.md` and `provider/AGENTS.md` advertised by root instructions are absent in this pinned tree. This review references `docs/superpowers/AUDIT-CHECKLIST.md`, especially timestamp conventions (7), silent gates (2/19), canonicalization, fabricated values, production-boundary parity, and R1–R10 at 2281–2315. Source base is the root-assigned revision, not independently asserted to be the running binary. Historical prose is background, not fresh runtime evidence.

Document revision evidence (`git log -1 --format='%h %s' -- <path>`):

- SYSTEM-MAP: `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap`
- VL-TRADING-RULEBOOK-v1: `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap`
- AUDIT-CHECKLIST: `dfda15e1 test: isolate session clock fixtures from weekly backfill workers`

SYSTEM-MAP bars/routing sections and RULEBOOK contract/source sections were consulted; they describe NT8 as the single live futures source, contract-aware store reads, and distinct source labels. This worker did not reverify the AddOn, store migrations or live provenance guards. The additional current bridge source directly confirms NT8 cache injection and warns that the newest bar can be forming.

### End-to-end data ownership

[A] `trader/ninjatrader/bars_market_bridge.go:20–31` installs the global `market.FuturesBarsProvider` seam declared at `market/futures_data.go:14`. Its `barsFromCache:44–62` selects the cache tail and adapts it into ordinary `market.Kline`; horizon checks warn rather than refuse. `barsToKlines:72–86` derives scheduled CloseTime and does not remove forming bars. The seam is a startup-owned function variable, not a per-request transport client; tests overwrite it and restore it.

[A] `market.GetWithTimeframes`, `data.go:190–359`, normalizes symbols, inserts the primary timeframe if absent, and fetches each requested series. CME routes only to that seam; a missing provider or empty secondary series is logged and skipped, while missing primary data returns an error. XYZ assets route to Hyperliquid; other symbols use free CoinAnk Binance data. Fetch depth is `clamp(maxConfiguredPeriod+count,200,2500)`, allowing configured long-period EMA warm-up. Current EMA/RSI maps supplement legacy fixed values. Futures skip OI and funding network calls at 332–343. No fallback from the futures branch to Databento or a crypto candle feed exists here.

[A] `calculateTimeframeSeries`, `data_klines.go:162–304`, retains the requested tail's OHLCV, fixed EMA20/50, MACD line, RSI7/14, BOLL20, ATR14, and configured period maps. Insufficient history yields shortened/empty series; scalar indicator helpers often return zero. Input close order is assumed, and forming inputs remain included. `Data` and `Kline` in `types.go` are shared contracts, not source-specific types. Price changes by bars use ordinal lookback and thus assume regular spacing; preloaded `BuildDataFromKlines:723–751` instead uses CloseTime-based lookback and performs no network access.

[A] There are two different freshness concepts. `isStaleData`, `data.go:773–813`, detects five nearly identical closes with all-zero volume. It is not a timestamp-age gate. The legacy `data_freshness.go` helpers provide 90s RTH / 5m ETH age checks and >5% drift within strictly <60s, but a repository Go search found no non-test calls to `CheckDataHealth`, `IsFresh`, or `IsDriftSuspicious`. The actual additional source read, `kernel/stale_data.go`, evaluates snapshot-time feed age using period/open-time/grace semantics and neutralizes only new `open_long/open_short` decisions. Missing SnapshotMs warns and fails open; exits are exempt. This review does not conclude that futures freshness is absent because the legacy helper is unused.

[A] Completed research bars use a separate `BarResolver`, `bar_resolver.go:142–195`. It chooses the first rung producing any completed bars in the requested half-open window, with source/FromTF provenance. Native bars take precedence; the final 1m rung for coarser resolutions is persisted Own1m, not native 1m. For the 1m request only native 1m is used. Caller wiring in `trader/auto_trader_weekly.go:100–127` scopes Own1m to the currently acknowledged contract via `BarsBetweenOn`. No partial-source splicing occurs inside the resolver. `dropForming` tests elapsed period only; it does not prove all source minutes exist.

### Findings and limits, prioritized

#### A-grade static defect: weekly resolution disagrees with its consumer's calendar

[A] `bar_resolver.go:61,80–81` excludes native weekly bars explicitly because they straddle the desired Monday week, and chooses native daily bars. But `CompletedBars:185` immediately aggregates those daily bars into 10,080-minute buckets using `AggregateToTF:249–261`: `start = OpenTime / span * span`. Epoch-based seven-day boundaries are Thursday 00:00 UTC, not Monday-governed CME weeks. `CompletedBar:204–206` uses the same generic floor.

[A] This is on an actual production call chain. `trader.weeklyDailyBars:140–156` asks the resolver for `1w` and returns `s.Bars`; its comment claims these are DAILY. `runWeeklyRead:224–232` passes them into `kernel.ComputeWeeklyFacts:44–57`. That function calls `CompletedWeekCandles`, `PriorWeekRefs`, `LastNWOGs`, and `DailySessionBars`. `CompletedWeekCandles`, `kernel/weekly_bias.go:93–149`, assigns each input bar's entire OHLCV to `weekStartMonday(OpenTime)` (73–80). Once multiple days have been fused into a Thursday week, that assignment cannot recover their original dates. The loss of daily/minute granularity also matters to functions expecting weekend/daily evidence, although those downstream helpers were not fully audited here.

[B] Consequently weekly references can span the wrong constituent days, and a completed prior CME week may be unavailable until the epoch week closes. This is a source-level defect, **not a demonstrated erroneous live plan or trade**. No synthetic reproducer was added because root owns repairs/tests.

[A] Existing tests do not falsify it: `market/bar_resolver_test.go` aligns weekly fixtures by epoch floor and asserts counts/source labels. `trader/bar_source_weekly_test.go` checks that history is no longer thin and calls the same resolver twice to compare doc/watch results. It never pins a known Sunday's/Friday's extreme to independently expected Monday-week OHLC. A repair should exercise the production weekly consumer boundary with distinctive daily values around Sunday/Friday and preserve raw-enough data for all weekly-facts consumers.

#### B-grade static inconsistency: legacy futures access still enters crypto metadata paths

[A] `GetWithExchange`, `data.go:33–158`, correctly selects NT8 5m/1h for futures, but unconditionally calls Binance OI/funding at 129–137. Its reported 1h change uses twenty 5m bars (~100 minutes), and reported 4h change uses the preceding 1h bar. `Format:469–505` calls these intraday 3-minute and longer-term 4-hour fields. Thus the legacy representation silently changes units when routed to futures. Search found `trader/auto_trader_orders.go` callers at 97,335,502,650,769,833; these bodies were not fully read, so the conditions reaching each call remain outside this report.

[A] `GetBoxData`, `data_klines.go:470–494`, has only XYZ versus CoinAnk branches and no CME branch. `GetKlinesRange`, `historical.go:17–104`, is explicitly Binance-only after generic Normalize. Caller search links GetBoxData to grid regime code, not proof that NT8 day-plan execution uses it. These concerns must not be described as contamination of the correctly separated main `GetWithTimeframes` path.

#### B-grade static data-quality/refusal concerns

- [A] `market/api_client.go:103–124` type-asserts raw JSON fields after only a length check; free and paid CoinAnk Kline methods index nine fields without a length check; websocket `handleResponse:105–152` similarly indexes data. `market/historical.go:73–91` indexes/asserts seven fields without guards. [B] Malformed external rows can panic these paths. No hostile traffic or exploit test was performed, and caller recovery/reachability is not established.
- [A] CoinAnk websocket pumps send to bounded channels without a cancellation select (`depth_ws.go:86–101`, `kline_ws.go:92–152`). Closing the socket does not unblock a goroutine already blocked sending. The demux also requires an exact serialized JSON prefix/key order. [B] Slow/abandoned consumers may leave blocked goroutines; reordered valid JSON may be silently dropped. No runtime leak was measured.
- [A] `getOpenInterestData:389–392` fabricates Average as `Latest*0.999`; failed metadata is often rendered as zero, indistinguishable from a measured zero. VLOS `GetNetFlowRanking:45–96` and `GetOIRanking:50–84` return non-nil results and nil error even if all constituent requests fail. `GetCoinData:79` and `fetchOIRanking:100` accept `success:false` when code is absent/zero. These are concrete availability/value semantics, not proof of current provider failure.
- [A] `calculateTimeframeSeries` loops the original configured EMA/RSI/BOLL period slices and appends into maps keyed by period. Duplicate periods therefore append multiple values per bar into the same key. Caller configuration deduplication was not audited; this is a local precondition risk, not proof user settings can trigger it.
- [A] `AggregateToTF` assumes ascending input, accepts non-divisible src/dst periods, emits partially held buckets, and carries non-OHLCV fields from the first source bar rather than aggregating quote/trade fields. `CompletedBars` certifies temporal closure rather than data completeness; first-rung nonempty history can be arbitrarily short. Provenance is correct about source, not a coverage guarantee.
- [A] `IsCMEFuturesSymbol:50–81` accepts any string containing lowercase `.c.` and known root-dot forms; it does not accept space-qualified `MNQ 06-26`, although `futuresRoot:142–158` does. Normalize preserves lowercase bare CME roots. Actual NT8 cache callers should supply canonical roots; this review did not audit the cache's own canonicalization.
- [A] Timeframe vocabularies differ. `market/timeframe.go` omits 8h/3d/1w, while resolver and series parsing include them. Hyperliquid maps 3m→5m,2h→1h,6h→4h,3d→1d without aggregation, and `getKlinesFromHyperliquid` leaves callers' requested labels intact. Alpaca/Twelve Data have similar substitutions. These are legacy provider semantics, not NT8 resolver behavior.

#### C-grade/historical-provider limitations

[A] Databento remains historical/smoke code, not live data routing here. `GetOHLCV:44–60` advertises 1m/1h/1d and explicit contract support but always sends `stype_in=continuous`; `rawBar.toBar:88–124` accepts only rtype33 and names ohlcv-1m. `ResolveContinuous:13–28` advertises non-continuous passthrough but always requests resolution. Client retries network/5xx failures with a shared circuit breaker; 4xx responses record breaker success, and exported Timeout does not update the already-created internal HTTP client. Fixture server validates paths, not query/auth, so current tests cannot prove these advertised request variants work.

[A] `PriceToTicks:29–34` uses `math.Round`, whose halfway rule is away from zero, despite the bankers-rounding comment. `SafeMultiply:64–69` adds ticks rather than multiplying. These helpers lack finite/range checks; most indicator helpers likewise assume positive periods and finite data. `PositionSize:82–88` is a pure floor calculation only; policy ownership stays with the current daily-loss controls.

[A] Alpaca `GetBars:69–131` ignores NextPageToken and returns one page from a 30-day/2-year lookback. Twelve Data `ParseBar:235–271` parses naive timestamps as UTC without using `Meta.ExchangeTimezone`; request methods check JSON status rather than HTTP status. No provider response freshness or current remote API capability was tested.

[A] VLOS direct transport uses `security.SafeGet` (implementation outside this slice). Optional Claw402 `DoRequest:77–113` is a paid x402 boundary, not a harmless offline getter: it creates a signing function and invokes `payment.DoX402Request`. Auth query stripping truncates at the auth parameter and can drop parameters after it. No payment key/environment was read and no such call was made.

### File-by-file role inventory

The following file notes summarize every assigned source. Exact function boundaries and body call expressions are in `functions.json`; current hashes and full ranges are in `reads.json`.

- `market/api_client.go`: Binance futures REST with 30s client and SET_HTTP_CLIENT hook; no status validation; array shape/types and numeric strings trusted beyond length check.

- `market/bar_resolver.go`: Completed-bars source ladder with provenance, injected clock, native daily preference for weekly, own persisted 1m final rung. Aggregation is epoch-floor including 1w; elapsed period is completion, not constituent coverage. Native weekly exclusion says Monday but math yields Thursday UTC.

- `market/data.go`: Main multi-TF data assembly routes CME to injected NT8 bars, XYZ to Hyperliquid, crypto to CoinAnk; primary mandatory, secondary errors skipped. Legacy GetWithExchange uses futures 5m/1h under 3m/4h semantics and still requests Binance OI/funding. Configured indicators, formatting, normalization and freeze heuristic.

- `market/data_freshness.go`: Pure legacy freshness/drift helpers (90s RTH, 5m ETH, >5% within <60s); future stamps stale. No non-test callers found. Runtime entry stale gate is kernel/stale_data.go, not this module.

- `market/data_indicators.go`: SMA-seeded EMA, MACD line, Wilder RSI/ATR, population BOLL, Donchian and 72/240/500-bar boxes. Exported wrappers expose pure calculations. Except Donchian periods <=0 are not guarded.

- `market/data_klines.go`: CoinAnk exchange/interval mapping and Binance fallback; Hyperliquid remaps intervals without reaggregation. Series include forming input and retain legacy fields alongside configured maps; duplicate configured periods append duplicate values. GetBoxData has no CME routing.

- `market/databento_adapter.go`: Historical Databento Bar to Kline conversion copies millisecond open and OHLCV, leaves CloseTime/other volumes zero; nil for no input.

- `market/decimal_safe.go`: Integer-tick arithmetic; SafeMultiply adds signed ticks, not multiplication. math.Round uses halfway-away rounding despite bankers comment. PositionSize is an optional pure floor helper, not evidence of an active per-trade cap.

- `market/futures_data.go`: Global startup-injected function seam, count-tail ascending bars; provider ownership is trader/ninjatrader. Seam does not itself guarantee closed bars or freshness.

- `market/futures_symbol.go`: CME recognition and per-root point/tick/month tables; arbitrary .c. accepted, bare matching case-insensitive but Normalize preserves case. Qualified NT8 form accepted by futuresRoot but not IsCMEFuturesSymbol. Energy/metals tick lookup intentionally unknown.

- `market/historical.go`: Binance-only historical range paginator, 1500 rows, cursor=last.CloseTime+1, status checked; raw row indexing/type assertions not guarded, no cursor progress assertion or CME provider branch.

- `market/timeframe.go`: Historical validator supports subset through 1d; case/space normalized, invalid errors or Must panic; differs from resolver and kernel vocabulary (8h/3d/1w absent).

- `market/types.go`: Shared Kline/Data/series shapes, configured-period optional maps, legacy zero fields, grid enums and default alert/cleanup literals. GridDirection ratio default .7 and unknown neutral.

- `provider/alpaca/kline.go`: Stock-only IEX raw bars client with config/explicit keys and 30s timeout; fixed 30d/2y window, returns one page while ignoring next_page_token. Timeframe aliases substitute without aggregation.

- `provider/coinank/base_coin.go`: Paid coin/symbol discovery wrappers, product/exchange filters; decode envelope twice redundantly, reject Success false.

- `provider/coinank/coinank_api/base_coin.go`: Free metadata lookup with optional exchangeName/symbol/baseCoin parameters, typed success envelope.

- `provider/coinank/coinank_api/depth_ws.go`: Depth websocket dial, subscribe/unsubscribe, 1024 buffered read channel; blocked consumer can strand send pump even after conn.Close; no post-dial context selection.

- `provider/coinank/coinank_api/kline.go`: Free crypto kline GET transport with context and 30s timeout, success envelope; positional 9-column array indexed unchecked, HTTP status ignored.

- `provider/coinank/coinank_api/kline_ws.go`: Raw websocket pump and prefix-based demux into typed 1024 queues. Requires exact serialized prefix/key order and 9 array values; sends can block without cancellation. Numeric parse errors become zero.

- `provider/coinank/coinank_enum/exchange.go`: Typed CoinAnk exchange wire names; historic and unsupported runtime brokers can still have constants.

- `provider/coinank/coinank_enum/instrument_agg_sort_by.go`: Typed ranking sort field literals across price/OI/flow/liquidation windows; no executable validation.

- `provider/coinank/coinank_enum/interval.go`: Provider-specific case-sensitive timeframe constants from seconds through years; 1M month differs from 1m minute.

- `provider/coinank/coinank_enum/product_type.go`: SWAP and SPOT wire product literals.

- `provider/coinank/coinank_enum/side.go`: to/from historical traversal direction literals.

- `provider/coinank/coinank_enum/sort_type.go`: asc/desc ranking sort literals.

- `provider/coinank/coinank_enum/url.go`: Mutable global/CN paid CoinAnk base URLs.

- `provider/coinank/coinank_http.go`: Paid transport GET/POST with apikey header and context; generic success/page envelopes; shared 30s client, no HTTP status gate or body size bound.

- `provider/coinank/instrument_agg_rank.go`: Visual screener and OI/long-short/liquidation/price/volume ranks; nested success checks; page>=1, size default10, default openInterest descending.

- `provider/coinank/instruments.go`: Last-price ticker and market-cap typed paid wrappers; rich crypto fields returned after Success check.

- `provider/coinank/kline.go`: Paid kline history with optional start, mandatory end, default10 count; same unchecked 9-column response as free path.

- `provider/coinank/liquidation.go`: Exchange aggregates, coin history, symbol history and individual liquidation wrappers; optional order filters; no trading orders placed.

- `provider/coinank/net_positions.go`: Net-long/short candle wrapper, default10 size, typed history envelope.

- `provider/coinank/open_interest.go`: Seven paid OI endpoints for per-exchange/all/series/candles/aggregate/top/market-cap ratios; typed success checks and optional sizes.

- `provider/databento/client.go`: Historical basic-auth GET client, env/default singleton, mutex key update, shared breaker and retry3 for network/5xx; 4xx returned as APIError after breaker success. Public Timeout not reapplied to internal client.

- `provider/databento/contract_calendar.go`: Single-digit year/month suffix parser and third Friday UTC index expiry estimate; no root/product validation; invalid DaysUntilExpiry returns999.

- `provider/databento/historical.go`: NDJSON nested hd timestamp/rtype decode, int64 prices/1e9; GetOHLCV always continuous stype and rawBar requires rtype33 despite advertised 1h/1d/contract support.

- `provider/databento/mock_server.go`: Testing helper in non-test source hosts fixture files at two v0 routes and cleanup closes server; does not validate request query/auth.

- `provider/databento/resolve.go`: Today UTC continuous-to-raw request; always requests even for non-continuous symbols despite passthrough comment; parser picks first entry, rejects not_found/empty.

- `provider/hyperliquid/coins.go`: Once-created coin provider, meta/contexts join by index, descending volume, 24h cache with copies; refresh outside lock can duplicate concurrent fetches; errors do not serve stale cache.

- `provider/hyperliquid/kline.go`: Contextual mainnet/testnet read-only info API; candles/mids/meta, 30s timeout; interval aliases substituted not aggregated; symbol suffix rules case-sensitive and single-step.

- `provider/vlos/ai500.go`: AI500 retries3 separated2s, available flag, quadratic score sort, USDT normalization. fetch empty returns[] while available/top zero outputs can be nil.

- `provider/vlos/claw402.go`: Optional paid x402 data transport, wallet key parsed from explicit/env input; endpoint mapping and auth truncation; payment boundary called only when client enabled, never invoked in review.

- `provider/vlos/client.go`: Deprecated-service defaults still present; mutex config and optional Claw402 gateway, direct requests use security.SafeGet with auth in query; Error returns body only, auth parsing substring-based.

- `provider/vlos/coin.go`: Per-symbol/batch quant API and bilingual prompt rendering; success=false accepted if code omitted/zero, batch skips failures; price deltas x100, OI percentages pre-scaled; OI map output order unspecified.

- `provider/vlos/netflow.go`: Four sequential flow rankings; each failure logged and omitted, returns result,nil even all fail. Bilingual tables and top3 retail summary, FetchedAt request-start.

- `provider/vlos/oi.go`: Top/low OI ranks and legacy symbol wrappers; both requests may fail yet aggregate returns result,nil. success/code compatibility accepts omitted code zero. Percent fields already percent.

- `provider/vlos/price.go`: Multi-duration ranking fetch with strict Success, map copy then bilingual renderer includes only1h/4h/24h; price ratio x100.

- `provider/vlos/util.go`: Language enums and signed K/M/B decimal formatting.

- `provider/twelvedata/kline.go`: API-key query client for time-series/quotes, 30s contextual requests; API status string checked, HTTP status ignored. ParseBar parses numeric OHLC strictly but assumes UTC for naive datetime and ignores exchange timezone metadata.

### Tests, graphs, negative results and remaining scope

[A] Fully read additional tests: `market/bar_resolver_test.go`, `market/data_f8_test.go`, `market/data_emaperiods_test.go`, `trader/bar_source_weekly_test.go`, `provider/databento/historical_test.go`, and `provider/databento/resolve_test.go`. Freshness test lines 1–145 were read as excerpts. The F8 test exercises GetWithTimeframes through the injected provider and asserts EMA200 output count; EMA/BOLL/RSI/ATR tests check configured-versus-legacy equality. Databento tests use nested hd records and captured-shape mock fixtures. No tests were executed during this reading dispatch, and no green-suite claim is made.

The July10@7a8adce0 Understand Anything graph was inspected using its actual `filePath` schema. Assigned-path extraction contains 252 nodes and 1, 130 touching edges: 649 imports, 204 contains, 154 exports, 94 calls, 16 tested_by, 13 documents. `historical-subgraph.json` preserves the complete machine extraction; manual inspection covered relevant node summaries and selected dependency edges, not independent confirmation of every historical edge. `market/bar_resolver.go` is absent. Package imports are expanded into edges toward many files, so an import edge to `market/data_freshness.go` does not establish a CheckDataHealth call. Corrections include SafeMultiply's additive semantics, fabricated OI average instead of an actual delta, Hyperliquid timeout 30s versus old 10s, and default timeframe 5m versus old 1h. `graph.json` records these corrections plus directly read current call boundaries. Root owns CGC evidence export; this worker did not query, reindex, delete or treat its historical index as current.

No source file in the assignment remains unread. Additional dependency files were intentionally read only where necessary and are not represented as full subsystem audits. Live NT8 identity/account/contract protection, all grid/order caller guards, security.SafeGet implementation, x402 payment enforcement, real provider schemas beyond captured fixtures, and whether any static concern has occurred in a live plan remain unresolved by this slice. The supplied task authorizes root's separate repair work; this report itself changes no behavior.

---

<a id="section-18"></a>

> Original: [reviews/13/report.md](reviews/13/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 13 — NT8 execution adapter and AI clients

Source evidence **[A]** is pinned to `63968be62e44db2fb07a92883e02127b9064b0be`, read in the clean isolated worktree `/tmp/vl-understanding-market-20260913`. All **42 assigned files / 9,436 lines / 397 named function declarations** were manually read; 63 anonymous callbacks are grouped beneath their enclosing declarations in `functions.json`. This is a static source review, not evidence that a production trade failed or a payment exploit occurred. No source, settings, account bindings, live DB, orders or runtime were changed. No tests were executed in this worker; tests cited below were read, and reproduction is a recommended next step for the parent repair lane.

Rules referenced: root AGENTS.md; tracked `docs/superpowers/CLAUDE-canon.md` (new lock keeper rules supersede older hand-heartbeat prose); `AUDIT-CHECKLIST.md` classes 1–13 and pre-audit R1–R10; relevant SYSTEM-MAP executor/cancel and RULEBOOK trade/risk sections. Subsystem `trader/AGENTS.md` and `trader/ninjatrader/AGENTS.md` do not exist in this worktree, despite root orientation listing them. Latest document revisions read at this base:

- `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap` — SYSTEM-MAP and VL-TRADING-RULEBOOK-v1.
- `dfda15e1 test: isolate session clock fixtures from weekly backfill workers` — AUDIT-CHECKLIST.

The daily-loss correction is accepted; no historical extra mandatory per-trade cap is recommended. Parent reports its separate repair branch has boot-latch checks on the three entry adapter functions. This review remains on the unchanged base and does not claim that repair is present or validated here.

### Confirmed source defects and bounded risks

#### 1. S / BROKEN source boundary: provider-controlled asset length can panic payment signing

**[A]** `mcp/payment/x402.go:132` `SignBasePaymentHeader` decodes an external 402 header, selects `Accepts[0]`, and calls `SignX402Payment:627`. The latter passes `opt.Asset` directly to `buildDomainSeparatorDynamic:738`. Hex decoding at :750 validates hex syntax but not address length. At **:763**, `addrPadded[32-len(contractAddr):]` has a negative lower bound for any successfully decoded asset longer than 32 bytes. This is a deterministic source-level panic condition rather than an error return. The sibling `hexToAddress:799` already requires 20 bytes but is used only for transfer from/to, not the asset domain. No crafted response was sent and no running service was tested.

A malformed or compromised payment endpoint is the trigger; this says nothing about the current gateway behaving maliciously. The same signing boundary also trusts amount/network/payee/timeout and domain extras. `preflightBalance` in `claw402.go:195` checks an estimated model price, not the actual demanded payment amount. A local affordability estimate therefore is not a limit on the signature being issued. That broader trust issue is a static security risk, not a demonstrated overpayment. Needed validation: offline malformed asset fixture and explicit policy for actual signed payment terms.

#### 2. A / BROKEN account ownership: another account's row suppresses untracked materialization

**[A]** `reconcile.go:101` reads its bound account's snapshot; `:365` looks for an owner row with that account. When missing, **:376** calls `GetOpenPositionByAccountSymbol("", sym, side)` to recover old rows with empty account fields. `store/position.go:751–784` shows that empty account means **no account filter**, across all traders, newest entry first. The fallback can therefore return an unrelated row on another **nonempty** account. The assignment block only updates empty accounts at `reconcile.go:381`, but **:389–391** treats *any* non-nil owner as tracked and skips materialization.

Concrete static trace: SimA holds MNQ LONG but has no row; SimB already owns an OPEN MNQ LONG row. First lookup misses SimA; fallback returns SimB; no backfill occurs; non-nil owner suppresses the SimA debounce/materialization on every pass. This undermines later close attribution for SimA. `reconcile_untracked_test.go` tests the intended empty-account recovery and ordinary materialization, not this two-nonempty-account case. The test also correctly preserves the negative result that a wholly untracked round trip closed before materialization is not fabricated into history.

#### 3. A / BROKEN concurrency ownership: reset uses the wrong map mutex

**[A]** `TCPTrader.pending`/`pendingAt` are declared as `pendingMu`-protected (`tcp_trader.go:96–104`). Entry submission, fill subscription and `reconcile.go:118–144` use `pendingMu`. **`ResetAccountState:1173–1182` replaces both maps while holding only `mu`**. `api/handler_account.go:199` invokes this method on the underlying live adapter during account selection. The fill goroutine exists from construction independently of whether a trader decision loop is running. Concurrent assignment/read/write is not synchronized by one shared mutex and can race. No race-detector reproduction was executed here; no crash is claimed. A repair must respect existing pendingMu→mu ordering in reconcile to avoid replacing the race with lock inversion.

#### 4. A / BROKEN isolation despite an existing green assertion: balance fallback uses another account

**[A]** `TCPTrader.GetBalance:848` calls `AccountStateFor(boundAccount)` at :863 and, when unavailable, calls global `AccountState()` at :865. `provider/ninjatrader/tcp_server.go:602–624` resolves that to the shared current account. It also sets the parent balance-received flag before returning that fallback's equity. `buildTradingContext`, `trader/auto_trader_loop.go:999–1032`, reads returned equity/wallet/available values without checking the returned `account` label; API account info likewise reads numeric values at `auto_trader_decision.go:118–146`.

This is confirmed cross-account input substitution when the bound account has not reported, not proof that an incorrect position was sized in production. **`account_binding_test.go` explicitly requires this fallback** for `SimNoSnapshot` and is therefore a regression-test blind spot that encodes the unsafe compatibility choice. Bound positions intentionally do not use the same fallback. Prefer an explicit unavailable response for a missing bound account; separately define legacy unbound display behavior.

#### 5. B / BROKEN telemetry measurement: nonstream TTFB is measured before the read

**[A]** All three nonstream body-read sites (`client.go:712`, :910, :948) call `readWithTTFB` only after `HTTPClient.Do` returns. `readWithTTFB:748` starts a new clock there; `ttfbReader.Read:740–745` stamps elapsed time **before** calling the underlying `Read`. Queue time and a delayed first body byte are both excluded. A near-zero number is not evidence that the provider answered promptly. Streaming TTFB uses a request clock and first scanned line instead, so the metrics are not comparable. No latency experiment was run.

#### 6. B / BROKEN response-completion semantics: empty/unfinished SSE can succeed

**[A]** `ParseSSEStreamFullData:1519–1607` ignores malformed data chunks; its only error after the loop is `scanner.Err()`. Clean EOF with no data or no `[DONE]`/finish marker returns a non-nil result and nil error. `CallWithRequestStreamDeadlines:1380–1384` checks only result nil, not empty text or completion, and returns `sr.Text,nil`. The `ClassEmpty200` vocabulary in `failure_class.go` cannot classify this success. A complete-but-unmarked response may be acceptable by explicit provider contract; empty success and lack of completion evidence must not be silently conflated with that case. Downstream validators may still reject empty/partial documents; that does not make transport telemetry correct.

Independently, **`X402CallStream`, x402.go:515–521**, returns any nonempty text with nil error **before checking `sseErr`**. An interrupted paid response with partial text is logged complete. `stream_watchdog_test.go` covers watchdog cancellation, an actual midstream transport reset and split deadlines, not the clean-EOF acceptance or paid partial-text error swallowing. No incident or end-to-end trade failure is inferred.

#### 7. B / BROKEN historical window: backfill differs from nightly CME bounds

**[A]** `runLevelStatsDayAt:83–90` derives previous CME session-day bounds. `BackfillLevelStats:184–203` parses date-only text in CT and passes **midnight to midnight** at :196–198 while its comment says 17:00→17:00. Thus nightly and backfill use different bars for the same `dayKey`. This is a source-confirmed function defect; production use of the backfill function was not established. `level_stats_nightly_test.go` tests the nightly body with explicit 17:00 bounds, not BackfillLevelStats.

### Execution and market-data ownership map

**[A]** `transport.go:115` resolves `NT_TRANSPORT`: unset/`csv` still constructs legacy `Trader`; `tcp` starts one process-wide server through `getOrStartTCPServer:51`, then constructs `NewTCPTrader:143`. `SplitSymbolList:86` selects the first nonempty symbol as primary; extra subscriptions are maintained separately and optionally appended from `NT_EXTRA_SYMBOLS`. Startup bind errors are cached by `sync.Once`; retrying the constructor does not restart a failed listener.

`NewTCPTrader` binds a trimmed account, registers it on the server, selects the bar root, and installs a per-symbol/account fill subscription **before returning**. Fill status `rejected` removes pending and matching correlation state and notifies the rejection sink; confirmed/partial fills update the single latest fill and recent-fill ring. `OpenLong/OpenShort` call `placeEntry:324`; `PlaceLimitEntry:437` and `PlaceStopEntry:498` build resting commands. All three enforce nonempty bound account and server-reported SIM/optional allowlist; stop entry also checks far-side build. Inputs are tick-rounded and quantities converted with `int(quantity)`; the adapter does not validate every geometry/risk property itself. Midpoint entry reference is used for market commands. Limit/stop beforeSend callbacks persist registration before socket send. Pending/correlation state is installed before send and not immediately rolled back on send error.

`order_guard.go:36` gives **each adapter instance** a 55s duplicate guard and ten admitted actions per 60s window. It is not a process-wide account breaker. Market/limit/stop keys have different prefixes, so cross-type duplication is not the same key. Admission is recorded before bracket validation/send, and the dedupe map has no pruning. Limit/stop rejection paths do not increment the market path's B3 gate counters.

Local `SetStopLoss:830` and `SetTakeProfit:837` only prepare future entries. `PlaceProtectiveStop:798` is the actual standalone protection wire command, with build/account/SIM/quantity/positive-price checks. `MoveStopToBreakeven:655` resolves latest in-process signal, cached materialized identity, then persisted entry identity and refuses local-map stop widening. Successful socket send updates local stop belief; it is not a broker receipt. `ModifyBracket:597` sends both prices without this local stop-widen comparison. Whether the far-side handler and callers enforce equivalent tightening remains outside this slice's confirmed result.

`CancelOrder:590` explicitly means request sent, not broker confirmed; SYSTEM-MAP describes later fresh-book settlement and nonterminal cancel_pending. Close commands similarly await asynchronous close facts. `GetOpenOrders:1266` refuses when its injected ledger source is absent rather than returning fabricated flatness. Its comments still say NT8 has no snapshot frame, contradicted by the source's F12 hook in `transport.go:38–65` and current SYSTEM-MAP. `GetOrderStatus:1195` ignores the supplied orderID and relies on the *latest* signal; partial is reported FILLED. Its rejection branch is unit-tested by manually setting fields, but the real constructor rejection callback clears those fields, so that test is not production rejection-flow coverage.

`StartCloseSync:23` subscribes once to closes/rejects/instrument specs. `recordClose:82` resolves owner by account/symbol/side and uses the row's quantity, entry and futures point value, not the frame's possibly larger account-wide quantity. It records a deterministic exit fill, stamps corrected PnL and fires `OnPositionClosed`. Unknown owner means park the priced close. `MarkCloseConfirmed` records receipt wall time after `recordClose`, even if the database update failed. The price cache keys only account/symbol/side and consumes once within 120s, without checking entry identity or that the close follows this row's entry.

`StartPositionReconcile:69` launches a 20s ticker with no stop channel; old adapter reconcile workers are not retired by this function on reload. A positive cached account snapshot is required, but this call does not inspect age or connection state. It uses 120s entry grace, 60s flat/untracked/divergence windows and 45s pending cleanup. Repeated cached flat observations can satisfy wall-clock grace; whether server disconnect handling invalidates that cache was not fully traced here. Parked close → recent opposite-side fill → unresolved is the orphan exit chain. `netting_fills.go` chooses the latest opposite fill in a time window, without quantity/net-position trajectory proof; report this as a heuristic, not a proven identity match. `StampArmedLineageIfMatched:514` matches latest twenty filled arms by side and ±tick price, not symbol/event-time, and `RepairArmedLineage:572` can stamp older unlinked rows. Coincident prices can therefore create incorrect lineage; no affected row census was read.

`wireFuturesBarsProvider:20` installs the server cache as market data source. `barsFromCache:44` returns requested tail or whatever is held; horizon checks warn only. `barsToKlines:72` derives scheduled CloseTime and does not prove bars closed. Unknown timeframe duration returns zero at :92, but conversion still writes `OpenTime-1`, contrary to its “without derived CloseTime” message.

`WireBarPersistence:79` is process-once and owns the replay hold/store callbacks. Historical frames are close-filtered then open-stamped; live frames resolve the ring seed verdict before writing their own closed rows. Contract comes from the latest subscription ACK, else explicitly labeled store fallback, else write skipped. Historical data are queued until scale verification; existing store/live-source policy is outside this file. Boot backfill preserves source, 1m-only rehydrate adds strictly older stored bars to warm keys, and prune/integrity runs at boot then every 24h. The scale and roll hooks register after boot replay handling, not before. `replayHold.resolve:90` removes pending rows before inserting; an insert error loses them with a warning rather than retry. Queue identity is only symbol/timeframe, not contract/replay generation. Delayed persistence vs subsequent verdicts/rolls is an unresolved ordering concern requiring provider-worker tracing, not a confirmed contamination incident.

`bar_horizon_warn.go` counts every short/holed/empty observation before dedupe, suppresses boot-empty warnings for ten minutes or until backfill, and uses a fifteen-minute key window. `contract_boot_line.go` separates ACK vs store fallback and observed mismatch census; census read failures can appear as zeros. Rehydrate kept/filtered counters are aggregated over selected symbols then printed on every per-symbol line, so those lines are not per-symbol counts. `level_stats_wire.go` evaluates every trader on **MNQ** bars, uses only the latest plan per session and evaluates the whole day; it is not an as-of-planning forward-trade experiment.

### AI client, provider and payment flow

**[A]** Registry factories initialize via provider/payment package init. Seven native OpenAI-compatible wrappers install defaults and hooks; Claude translates bodies/auth/parsers. Public `AIClient` has seven methods including `ResolvedModel`. Client options apply in order; absent Provider in `NewClient:185` overwrites model/base with DeepSeek defaults even if those were separately supplied. Mutable MaxTokens/Thinking/HTTP timeout tuning is not synchronized; callers must own or clone clients. The last-call telemetry atomics are shared across concurrent calls, so independently safe loads do not establish coherent call identity.

`CallWithMessages:424` dispatches provider Call, retries substring-classified transport failures and logs each attempt. Request/plain and Request/full paths build richer messages, tools and cancellation, but do not reset/emit the same structured per-call telemetry as the simple/stream-retry wrappers. Config timeout honors canonical `AI_HTTP_TIMEOUT_SECONDS` before legacy name. Simple integer env parsing is positive-only; zero retries cannot be requested there. Streaming counts calls through StreamTries and per-client storm budget, uses schedule backoff, and creates a copied HTTP client with Timeout=0 only when an explicit total deadline is supplied. The watchdog pre-token timer accepts all scanned lines; post-token resets only actual content/reasoning, bounded by caller idle and configured post-token limit. Successful and failed telemetry fields mostly update *after* parsing returns; watchdog-time byte fields therefore can remain zero during generation. When `AI_PLAN_TRACE` is off, hooks are not installed despite later “captured unconditionally” comments.

`context_guard.go` is a heuristic, not hard fit enforcement: keep all systems and at least the newest non-system; oversized retained messages can still exceed budget. It ignores tool schema/arguments and reasoning size in Any estimation and truncates individual messages rather than complete tool exchanges. RequestBuilder validates only that some message exists; slices/maps/pointers are not deep-copied at Build. ToolChoice is a string despite documentation suggesting an object.

Claude request conversion ignores req.MaxTokens, Stop, TopP and Stream, uses last system only, and returns last text block rather than concatenating. The inherited streaming reader expects OpenAI choices/delta and is not a provider hook. These are source-observable unsupported combinations; no current user configuration or external provider behavior was queried. Native wrapper custom URLs do not implement base SetAPIKey's trailing-# handling. Exact model defaults were recorded as repository literals, not verified against current provider catalogs.

Claw402 routes model strings to fixed gateway paths, wallet-preflights simple Call and RequestFull, removes output caps for non-Claude requests and signs first offered payment terms. Unsupported model routes silently use default endpoint while telemetry retains the input model. Inherited CallWithRequest/stream methods bypass the custom payment handshake. Retry re-signing on 402 creates a new random authorization; its comment “no double-charge” is not a fact this client alone can guarantee after ambiguous server settlement. There were no payment/network calls in this review.

### Graph/source contrasts and evidence limitations

Historical Understand Anything graph (`July10@7a8adce0`) contributes 129 matching nodes and 1,110 incident edges for 28 of 42 assigned paths; **14 current files are absent**, including replay hold, persistence, horizon warnings, netting, guards and AI failure/policy telemetry. Raw selected historical graph is retained separately. File imports fan out to many files in the imported Go package; those are not type-resolved direct calls. A graph claim of six-method AIClient is stale. The payment graph description says retries avoid double charge; current source does not independently prove that. Root CGC history is also historical; this worker did not request reindex or independently receive a live CGC export. Source and root AST census outrank the old graph. `graph.json` lists bounded corrections and directly observed edges; functions.json labels AST connections syntax-only rather than inventing resolved callers.

Test limitations matter: account-binding fixtures explicitly approve the unsafe missing-snapshot fallback; order-status fixture mutates state rather than driving rejection callback; untracked reconcile tests omit two nonempty account owners; nightly stats tests omit BackfillLevelStats's date conversion; stream tests cover socket errors and timers, not successful empty EOF or X402's discarded read error. Additional tests outside these read ranges were only inventoried, not claimed fully read or passing. No live-state census, exact current account exposure, deployed C# compilation/restart receipt or merged-head test result was obtained. The findings above are reviewable source behavior with explicitly bounded triggers, not certified runtime failures.

---

<a id="section-19"></a>

> Original: [reviews/14/report.md](reviews/14/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 14 — NT8 wire and AddOn source review

Base: `63968be62e44db2fb07a92883e02127b9064b0be`, isolated `/tmp/vl-understanding-surfaces-20260913`; revision and empty porcelain verified before reading. All **20 assigned files / 10,442 lines** were read manually in bounded complete segments. `reads.json` records hashes and supplementary reads; `functions.json` inventories **306 named declarations**, including nested JSON parser methods. Closures belong to their containing method and lexical references are explicitly not type-resolved calls. No source edits, runtime orders, database access, deployment, restart, network reproduction, or tests were performed. These are source findings, not newly observed trading incidents.

Policy references: AUDIT-CHECKLIST pre-audit R1–R10 and classes 85–87 (producer omissions, self-validating pins, premature cleanup), SYSTEM-MAP execution/reconcile sections, rulebook B1/B3. Latest relevant logs at this base:

- `565e8fbe Sun Sep 13 00:39:02 2026 -0500 fix: use owner daily-loss controls without requiring a per-trade cap` — SYSTEM-MAP and rulebook.
- `dfda15e1 Sun Sep 13 00:48:28 2026 -0500 test: isolate session clock fixtures from weekly backfill workers` — AUDIT-CHECKLIST.
- `07b53e65 Thu Sep 10 17:13:53 2026 -0500 cleanup batch 1 — B5 the mutation harness that cannot fake a verdict, B6 the worktree-add check` — CLAUDE-canon.

No new mandatory per-trade cap is inferred. Parent owns the separate Go boot-gate repair; this review describes the stated base only. Repository-only C# inspection cannot establish the Windows compiled AddOn: deployment requires copy, F5, full NT8 restart and received build evidence.

### High-priority static findings

#### 14.1 — Cancel request destroys the deferred bracket before cancellation is proven

**[A] source-proven / [B] outcome conditional on broker event ordering; high severity.** `VLTraderTCPClient.HandleCancelOrder:1735–1820` retrieves then **removes** `workingEntries[signalId]` at 1770–1771, calls `Account.Cancel` at 1780, catches its failure locally at 1784–1788, and still deletes `pendingBrackets[signalId]` at 1813 and sends `cancel_order` ACK. The source state variable is logged but never gates cancellation/cleanup.

Entry placement records pending geometry before submission (`HandleSignal:1032–1047`). A later first `PartFilled` or `Filled` event calls `SubmitBracketOnEntryFill` (`OnOrderUpdate:1441–1445`). That helper first checks an existing placed bracket, otherwise requires pending geometry at 2101; absent geometry returns silently. Thus either (a) cancel returns while a first fill is still in flight, followed by removal before the fill callback, or (b) Cancel throws and the still-working entry later fills, can leave that fill without its intended SL/TP. A retry has also lost the tracked entry object. Removing a local note has execution consequences even though the removed note is not yet a broker order.

**Negative results:** The handler no longer reads or cancels `placedBrackets`, `SlOrder`, or `TpOrder`; the old direct stripping of an already-placed bracket is fixed. A partial fill that already created `placedBrackets` retains protection and subsequent cumulative fills use `AmendBracketQuantity:2164`. If fill callback consumes the intent before cleanup, bracket creation proceeds. This is a remaining deferred-intent race, not a claim that every cancel removes protection.

**Recovery is conditional, not a closure of the race.** Full C# read finds no reconstruction of pending/placed/working-entry dictionaries from account orders after NT8 restart. TCP reconnect alone preserves them because the AddOn object survives; NT8 restart does not. Go `reconcileProtectionAt` (`trader/protection_reconciler.go:267`) runs from `monitorTick` (`auto_trader_risk.go:62`) and dead-man reconnect (`auto_trader.go:114`). It requires positions plus a fresh account book, readable protective-state coverage, and a valid accepted-risk stop or matching filled arm's composed stop. With no price it only alerts; stale/missing/unreadable/unsized evidence refuses action; partial coverage alerts without placing a second stop. Its recovery command passes through `TCPTrader.PlaceProtectiveStop:798` and C# `HandlePlaceProtectiveStop:1850` and places one standalone GTC stop, **not the original SL/TP pair or a reattached bracket**. No timing guarantee or installed DLL behavior was verified.

Existing `trader/bracket_cancel_addon_test.go` is a C# text pin. `TestCancellingAnEntryNeverTouchesItsBracket` prohibits references to placed bracket legs but does not exercise asynchronous Cancel completion, thrown Cancel, or deferred-intent retention. Fix direction for the owner: retain entry and deferred geometry through in-flight cancel; retire on confirmed terminal no-fill outcome; preserve partial/full fill protection; test both orderings and failure before C# deployment.

#### 14.2 — Close target ignores the account explicitly carried by Go

**[A] source-proven / [B] cross-account close scenario; high severity.** Go `TCPTrader.sendCloseAt:750–771` sends its bound `Account`, trader ID, fresh signal ID, quantity and symbol. Server `SendClosePosition:1146` adds seq. C# `HandleClosePosition:1152–1232` never reads account/trader/seq. It selects `positionAccountBySymbol[root]`, falling back to active `account`, then limit-exits or flattens that account. The map is overwritten by the latest **full** entry fill for that root (`OnOrderUpdate:1453–1455`), regardless of which account previously held it, and any full exit removes the root (`1402`). It is not repopulated by position snapshots after restart.

Two SIM accounts holding MNQ can therefore make a close for A act on B when B's fill last wrote the shared root map. An after-the-fact echo verifier cannot prevent the wrong broker action; this handler does not capture the fresh close identity, and ordinary Flatten may emit an unknown name/seq0. SIM checks remain present: this finding does not establish a live-account trade path.

Adjacent target-resolution issue: `HandlePlaceProtectiveStop:1877–1888` attempts exact named-account lookup but uses `resolved ?? account` even when a nonempty explicit account was not found. The valid active SIM/allowlist guard may then permit a stop on a different account. Go validates its bound account from cached discovery; stale discovery is a possible precondition, not observed here. Entry `HandleSignal` correctly rejects unknown explicit accounts, so the two paths disagree.

#### 14.3 — Current C# contract metadata is discarded by Go; reconnect roll can bypass purge

**[A] source-proven / [B] roll contamination risk; high severity.** Current C# `VLBarsSubscriptionManager.EmitHistorical:480–487` and `OnBarsUpdate:535–542` include `contract` on every bar frame. Go `BarsHistoricalPayload` / `BarUpdatePayload` (`tcp_framing.go:585–600`) omit it; server handlers (`tcp_server.go:1966–1983`, `2029–2056`) decode root/timeframe/bars and enqueue no contract or generation. `observeContract` is called only by `FrameSubscribed:1944`. C# `OnConnectionReconnected:557–602` disposes/re-resolves requests and can change concrete contract but sends no subscribed ACK. Its new bar contract cannot update Go's `CurrentContract` or trigger ring purge.

Even on a Go-requested subscribe, the symbol ACK is sent after initiating requests; callbacks may emit bars before it. Purge happens on the reader goroutine while old/new bar messages are queued for a separate drainer. Since `barIngestMsg:476` has no contract/generation, queued old messages may repopulate a purged ring. Persistence obtains contract later from current subscription/store fallback (`trader/ninjatrader/bar_persist_wire.go:111–124`), not captured frame metadata. Research `facts:30` similarly infers preceding ACK and ignores emitted bar contract. Same-root bars/orders also independently resolve platform current contract at different times; sharing a resolver implementation alone does not pin an existing BarsRequest and a new entry to one contract.

`contract_roll_test.go` manually calls observeContract and seeds bars in intended order; it proves purge for that schedule, not actual C# callback/ACK ordering, reconnect-without-ACK, or queued frame generation. SYSTEM-MAP:35 and contract_roll.go comments claiming bar frames carry no contract are now false. Remediation needs provenance through decode, queue, cache and persist together, with ordering/generation tests; merely adding another ACK leaves queued old bars unresolved.

#### 14.4 — Historical channel teardown can panic the production read loop

**[A] lock/scope source proof / [B] interleaving; high severity.** `SubscribeBarsHistoryFor:419` and `UnsubscribeBarsHistoryFor:442` close channels under `histSubMu.Lock`. `readLoop` history-data/error branches obtain a channel under RLock, release it, then send (`tcp_server.go:1994–2038`). Unsubscribe/replacement between unlock and send yields send-on-closed-channel. Nonblocking select does not protect a closed channel; this path has no recover. Normal importer timeout/teardown is sufficient to create the race, no hostile client required. Account event fanout `trySendLocked/subscribeFor` correctly retains one lock around send/close, showing the safe sibling pattern. Reproduction not performed.

#### 14.5 — Missing order array becomes fresh empty broker evidence

**[A] source-proven; medium/high severity depending consumer.** `ParseOrderSnapshot:96–114` requires account but maps both omitted `orders` and explicit `null` to `[]`. `FrameOrderSnapshot` then caches at receipt time (`tcp_server.go:1864–1873`). Payload `{ "account": "Sim101" }` is therefore accepted as a known fresh empty book. This defeats the comment's explicit absent/empty contract and can feed no-order/protection-absent decisions. Positions receive path also accepts account plus missing positions as known empty (`2150–2163`). Legitimate current C# writes arrays, so malformed/partial/future emitter input is the precondition; no live malformed frame observed.

`TestParseOrderSnapshotDistinguishesEmptyFromAbsent` tests only explicit `orders:[]`; it never tests the absent half named by its title.

### Other findings and limitations

- **[A] Nonactive account book freshness gap.** Heartbeat `VLTraderTCPClient:2714` calls `SendOrderSnapshot(account,...)` for active account only, while balances/positions poll every SIM account. Nonactive routed accounts get order snapshots on their subscribed order state changes, then stale during a quiet book. Restart sends no order snapshot until heartbeat. Go protection refuses stale/absent evidence, so safety degrades to unknown rather than automatic stop recovery. No installed account configuration was read.
- **[A] Claimed liveness mirror disagrees.** Go `order_state.go:107–110` marks Accepted/Working/Suspended/PartFilled live; C# `IsLiveAtExchange:1830–1833` accepts only Working/Accepted. C# modify/move refuses the extra states while Go may count them as protection. Conservative modification refusal is not itself proof of lost protection; comments should not promise parity that does not exist.
- **[A/B] Persistence watchdog cannot detect its own blocked callback.** `bar_persist.go:79–132` executes persister and watchdog select in one worker. A hanging callback blocks the watchdog itself. Callback panics are swallowed and counters/last-success stamp still advance (`102–115`). A void callback cannot report a database write failure, so `persistFlushed` means handed to callback, not durable commit. Queue saturation still logs from enqueue, but quiet synchronous failure can remain undetected by this watchdog.
- **[A] Shared ingest eviction is broader than logged.** `enqueueBarUpdate:1659–1690` removes any oldest `barIngestMsg`; a historical batch occupies the same channel and can be evicted as `ingestDropOld`, not `ingestDropHist`. Labels claiming only forming updates are dropped are not universally true. Historical enqueues themselves wait only2s. Persistence retries up to6s can stall drainer and provoke this pressure.
- **[A] Deep replay capture has no production call.** `captureBarTruthReplay:1482` has zero call sites found across Go source; request starts global capture but bar receive never adds frames. Current API arbiter behavior beyond this provider slice remains for its owning reviewer; this function cannot prove captured replay by itself.
- **[A/B] Session peak reset repeats.** `sampleIngestDepth:1643` uses machine local time and CAS from current loaded day to same day on every sample in hour17, then clears peak. This can repeatedly reset peak throughout that hour, not once per CME17:00 roll.
- **[A/B] History AddOn ownership/threading.** `VLHistoryPull.HandleBarsHistoryRequest:85,142` accesses `inFlight` without its lock; callback and DisposeAll mutate under lock. Concurrent callbacks/reader request insertion can race the dictionary. `EmitChunks:183–223` only flushes inside an accepted timestamp iteration; trailing duplicate/nonascending timestamps can leave a partial buffer unsent or omit last=true. Normal sorted unique NT bars avoid this. Bounds are passed to BarsRequest but emitted bars are not explicitly clipped to the requested half-open open-time window; downstream importer verification not fully reviewed here.
- **[A/B] Standalone protection is not tracked for later management.** `HandlePlaceProtectiveStop` submits an ungrouped `-sl` but never adds `placedBrackets`; MoveStop/ModifyBracket cannot find it. Netting-flat sweep only walks that map, so it does not explicitly sweep such a standalone stop; Account.Flatten may cancel platform orders, but opposite-entry netting is a different trigger. No runtime persistence/NT behavior proof here.
- **[A/B] Bracket submission and retirement are best effort.** SubmitBracketOnEntryFill removes pending intent before pair submit and catches errors without restoring it (`2102`, `2145`). CancelBracketsFor collects potentially multiple matching keys but cancels only the last matched pair before deleting all keys (`1108–1147`); CancelAllBracketsFor removes tracking even on Cancel exception. These require exceptional or multi-bracket/netting states; no such state observed this run. Full limit-exit fill is treated as enough to retire entire bracket even if requested close quantity is smaller than position; owner policy one-contract reduces ordinary exposure but API quantity does not prove flatness.
- **[A] Identity is partial across management.** Only HandleSignal captures trader/seq. Move/modify/cancel/close/protective handlers do not capture their incoming identity; SendAck emits a string only. Fill/close echoes use original entry identity if present. Go known-seq verifier compares trader/account, not signalID; seq0 or absent registry entry is tolerated. Account-aware fanout helps attribution after receipt but is not broker authorization. Go bound account spelling is case-sensitive in event routing while snapshot keys are uppercased; symbols admitted case-insensitively retain their incoming spelling as BarCache keys.
- **[A/B] Connection generation not isolated.** acceptLoop exposes `s.conn` and flushes signals before receiving/validating hello; farSideBuild, books, positions and balances are retained across closeConn. An old read-loop defer invokes unqualified closeConn, potentially closing a replacement connection. Existing base Go boot repairs elsewhere are not included in this report; treat these as source observations requiring cross-lane dedup.
- **[A] Test mocks are weaker than current protocol.** MockTCPClient sends no hello or account/trader/seq by default, writes from read loop and fill goroutines without shared write mutex, and continues after read timeouts that may consume partial frames. The real Go read-loop comments explicitly explain why that timeout continuation was retired. Mock success cannot certify current C# execution ordering or identity.

### Complete file-role map and invariants

**AddOn execution:** `VLTraderTCPClient.cs` owns lifecycle/connection, session allowlist, entry and protection maps, account hooks, snapshots, state handlers, wire encoder/parser. Protocol3, expected build `2026-09-07-h1`, reconnect5s, heartbeat30s, inbound1MB, stale signal60s; Go rejects invalid/future timestamps, while C# stale check accepts unparsable or future timestamps. Entry action case-folds LONG but unknown side still takes short branch; unknown order_type takes market branch. Existing Go caller validation is relevant mitigation, not C# validation. Day entries have empty OCO; filled quantity gets GTC stop/target under signal-exit OCO. IsSimAccount uses reflected Simulation or Sim-prefix; active account is additionally guarded before routed account, so a bad active account can reject an otherwise valid routed account. JSON Parse does not enforce complete consumption and recursion is unbounded; loopback access and trusted peer limit exposure but are not cryptographic authentication.

**AddOn bars:** `VLBarsSubscriptionManager.cs` maps14 native TFs, default2000 bars, ETH template, DoNotMerge, source-zone-to-UTC close stamps, emits complete changed-index ranges, rebuilds existing requests on Go subscribe. Reconnect copies previous cursor after starting new request; callback ordering is not proven. `VLContractResolver.cs` is quarterly root/continuous normalization and date fallback. `VLInstrumentLookup.cs` asks rolling instrument's GetNextExpiry using Windows local time, resolves concrete name, falls back visibly. Qualified symbols are passthrough in name helper but still flow through next-expiry lookup in Resolve. `VLHistoryPull.cs` independently resolves explicit contracts directly and never mutates live subscription registry, with8k chunks and seq/last/error correlation.

**Go bar state:** `bar_cache.go` filters only zero-volume zero-range placeholders, converts close to open once, keeps2500 tail, preserves live/mixed against replay, ignores older live updates, and allows older store rows only after warm seed. Sorted/finite OHLCV is assumed, not fully validated. `bar_source.go` compares first-live-after-seed using0.5% AND20x positive median body by default, labels mixed and drops historical on mismatch; thresholds env-overridable. Its callback is async; scale metadata survives PurgeSymbol. `bar_persist.go` owns global4096 queue and callback batching,300ms/256 messages,6s full-queue retry and counters. Historical closed filtering occurs before its later timestamp conversion in trader persistence; this can omit newest closed interval until a later path re-derives it. `contract_roll.go` tracks subscribed contract facts/listeners and purge, with no queue epoch. `research_wire.go` records MNQ raw bar observations and execution facts, receipt/observation clocks, bounded12000 comparison window; execution parent broker_frame retains raw orders while per-order child records whitelist nested fields, so nested unknown data is not uniformly removed.

**Go transport/identity/book:** `tcp_framing.go` defines all payloads and codecs, additive v3 stamps and bytewise capability floors; WriteFrame checks errors but ignores short-write count. `tcp_server.go` owns one TCP client,60s payload-age queue, immediate management commands,32-buffer event channels and per-symbol/account fanout,4096 default ingest, subscriptions, account caches and history streams. Overflow drops are logged rather than replayed. `echo_verify.go` owns4096 pending op map, monotonic seq, account registrations and freeze on known mismatch. `order_state.go` grades unknown/pending/live/local/dying/terminal; unknown is still standing. `order_snapshot.go` stores account-scoped full books with local receipt ages and build provenance; Latest returns slices by reference, and separate Latest/Age reads can cross arrivals. Empty account is rejected for books, but equivalent validation is absent for balance/positions frames.

**Legacy/test support:** `types.go`, `csv_writer.go`, `csv_tailer.go` implement5-field signals/3-field fills, temp+rename with retry, and row-count tailing; not the live TCP path. NaN passes legacy comparison-only validation; file shrink resets cursor and same-count replacement is invisible. `mock_nt.go` produces delayed synthetic fills from CSV, dedup timestamp/direction/entry; `tcp_client_mock.go` provides local test wire peer. Neither models real cancel/fill/bracket/account lifecycle.

### Tests and historical graph comparison

Fully read supplementary tests: `order_snapshot_test.go`, `contract_roll_test.go`, and `trader/bracket_cancel_addon_test.go`. Fully read `trader/protection_reconciler.go`; exact wiring and TCPTrader excerpts are recorded separately. Other provider test files were inventoried, not all manually read and not executed. C# has source pins, not a verified NT8 execution harness in this dispatch. Recommended reproduction set: cancel failure plus first fill; cancel/fill permutations; partial-fill retention; two-account same-root close; account lookup miss for protective stop; nonactive quiet book; reconnect roll with old queued bars; history teardown concurrent send; missing/null order arrays. Parent owns reproducer edits.

Historical Understand Anything graph is July10@7a8adce0, not current: selected assignment paths match102 nodes/320 incident edges and11/20 file nodes. Read selected file summaries and C# call edges; broad package import expansion is not precise call evidence. Concrete corrections: graph AddOn protocol2 -> current3; resolver primary edge ResolveFrontMonthContract->ResolveFrontMonthContractAt is obsolete (now RollingContractName); new VLInstrumentLookup is required between symbol and NT instrument; fanout now includes account;9 added files absent. Existing HandleFrame->bars handlers, OnOrderUpdate->SubmitBracketOnEntryFill, and watchdog->reconnect remain supported by source. No graph mutation/reindex or direct CGC calls were made; parent exports historical CGC separately. `graph.json` records corrections and explicit current edges without pretending completeness.

**Verdict:** source slice fully read; several concrete boundary defects remain. Runtime outcomes are UNVERIFIED until controlled offline/NT8 SIM evidence; no incident or production state is inferred from old prose, tests, or graph.

---

<a id="section-20"></a>

> Original: [reviews/15/report.md](reviews/15/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 15 — persistence and configuration source review

### Scope and evidence

[A] Read all **41 assigned source files / 10,001 lines** at `63968be62e44db2fb07a92883e02127b9064b0be` in `/tmp/vl-understanding-market-20260913`. Initial pwd, HEAD and empty porcelain output verified. Every assigned SHA256 matched the root manifest at artifact generation. `reads.json` records complete assigned reads separately from additional caller/test/rule excerpts. `functions.json` covers **382 named declarations**, their exact start/end lines, and **30 local function literals grouped under enclosing declarations**. Connections in that file are actual AST call expressions and explicitly **not type-resolved call-graph edges**.

This is static source review. No service requests, production DB reads/writes, trading tests, configuration changes, deployment, source edits, or reproducer modifications were performed. **No runtime incident or successful exploit is claimed.** [A] means inspected source or generated inventory; [B] means implications of that source. Historical sample ids quoted in source comments were not re-queried and are not fresh incident evidence.

Applied `docs/superpowers/CLAUDE-canon.md` and audit checklist pre-audit R1–R10; especially classes 9 (binding), 23 (telemetry), 28 (canonicalization), 35 (recorded counters), 40 (corrected PNL/NULL), 53 (production-call-site parity), 99/107 (arm states), 113 (source-guard scope), 117/118 (bar provenance), 124 (history imports), and 126 (frozen structural geometry). Read root AGENTS instructions; the tracked keeper rule supersedes obsolete hand-heartbeat prose. Root selected this source revision; this worker did not assert it equals a live boot.

Spec freshness records (`git log -1 --format='%h %aI %s' -- <path>`, all ancestors of reviewed base):

- CLAUDE-canon: `07b53e65 2026-09-10T17:13:53-05:00 cleanup batch 1 — B5 the mutation harness that cannot fake a verdict, B6 the worktree-add check`
- AUDIT-CHECKLIST: `dfda15e1 2026-09-13T00:48:28-05:00 test: isolate session clock fixtures from weekly backfill workers`
- SYSTEM-MAP: `565e8fbe 2026-09-13T00:39:02-05:00 fix: use owner daily-loss controls without requiring a per-trade cap`
- VL-TRADING-RULEBOOK-v1: same `565e8fbe` line.

Owner's latest clarification is preserved: **existing daily-loss controls, no new mandatory per-trade cap**. `store/structural_geometry.go:32–59` resolves buffer/cost/RR, not a per-trade loss cap. `RiskCapUSD` at :96 is retained only for historical serialized records. No recommendation here restores that superseded policy.

### Findings, highest impact first

#### F15-1 — A / BROKEN in source: trader deletion has side effects before ownership succeeds

[A] `store/trader.go:223–229` deletes `EquitySnapshot` with only `trader_id`, ignores that delete's error, then deletes `Trader` using `(id,user_id)`. A zero-row trader deletion is success. `api/handler_trader.go:767–788` calls this function without first resolving ownership, then looks up/stops/removes the named runtime trader after success. `api/server.go:146,205–207` mounts this under auth and `planTraderOwnership`; that middleware at `api/handler_plan.go:97–142` explicitly skips paths outside `/api/plan/` and `/api/risk/`. `api/route_registry.go:28–46` adds no further authorization; `api/server.go:851–892` authenticates a JWT and stores user identity but does not check target trader ownership.

[B] A valid user targeting a different user's known trader id can reach an id-only equity deletion and runtime removal path even though the owner-scoped trader row itself is retained. This is a **statically traced cross-user side-effect risk**, not a demonstrated exploit. Review/repair both store ownership-before-cleanup and handler ownership-before-runtime effects; cover wrong-user and zero-affected-row paths in isolated fixtures. Root owns any repair/reproducer.

#### F15-2 — A / BROKEN in source: unconfirmed AI→grid switch loses preserved AI fields at serialization

[A] `store/strategy.go:554–564` restores AI fields into the in-memory merged config. `api/strategy.go:328–331` calls it and logs preservation, then :354–368 marshals and persists. `StrategyConfig.MarshalJSON` at `store/strategy.go:805–842` assigns `AIConfig` only in the non-grid branch; grid config emits no AI bundle. The Go fields themselves are `json:"-"` (:733–737). Reloading the persisted grid record therefore has no saved AI fields to restore when switching back.

[A] `store/preserve_ai_config_test.go:53–64` calls the preservation helper twice on structs without intervening marshal/unmarshal, so its round-trip does not exercise this production loss boundary (checklist 53). This is deterministic serialization behavior, not a runtime incident. Test the API persistence round-trip, not just helper self-consistency.

#### F15-3 — B / UNVERIFIED runtime: prior OR median bypasses tape provenance filters

[A] `store/fade_or_history.go:19–57` queries raw `BarHistoryDB` rows by symbol, `tf='5m'`, and time, with no source/contract filter. The full caller `trader/fade_facts.go:39–43,89–95` directly supplies this median to fade facts. Unlike generic historical mixed-contract price comparisons, each prior-session OR range can legitimately come from its own contract; the definite issue is that this reader can include **mixed, off-scale, imported, or otherwise unusable rows**, and it does not prove one clean contract within an OR bar. It also reads stored 5m aggregates rather than the store-deepened 1m aggregation described for trustworthy horizon input in SYSTEM-MAP.

[B] Bad OR ranges can affect recorded fade permission and downstream one-setup permission selection. No current row population or actual permission outcome was sampled. Existing path lint that only matches `.LastNBars(`/`.BarsBetween(` cannot certify this direct DB reader. Keep this as source-confirmed reader gap; do not claim mixed-source rows were actually selected.

#### F15-4 — A / BROKEN coverage claim: reflected knob census cannot see emitted ai_config

[A] `store/knob_registry.go:61–101` skips JSON `-` fields while reflecting `StrategyConfig`. All five AI bundle fields use that tag in `store/strategy.go:733–737`, and their emitted JSON exists only through custom marshaling to `AIStrategyConfig` (:788–795,805–842). Thus a newly added risk/indicator/coin field is outside this reflected schema traversal. `store/knob_registry_test.go:12–41` only verifies leaves the same incomplete traversal returns; `len>=40` does not prove all serialized leaves were enumerated. Leaf-name fallback at registry :106–119 can also conceal different meanings at different paths.

The function does what its reflection rules say; the broader claim that every product schema knob is automatically guarded is false. A JSON/schema-aware independent census should compare the actual serialized product schema. This finding does not establish that a particular live knob is currently misclassified.

#### F15-5 — B / UNVERIFIED runtime: follow recording and readable counters hide storage failures

[A] `store/one_setup.go:247–311` issues separate NULL-guarded updates for each known follow field and discards every field-update error, then returns only the state/backfill update error. A terminal state can therefore persist after a known-field write failed. `trader/follow_plan_wiring.go:138,233` checks only the returned error (call-site search); detailed surrounding recorder control flow was not fully reviewed in this assignment. `OneSetupCountsFor` (:190–219) sets `Readable=true` regardless of per-class query failure, and `CountFollowPlans` (:352–369) ignores all count errors after the first count.

[B] This can make a partial observation appear complete/readable and prevent later repair if terminal follow states are excluded. `OpenFollowPlans` (:316–325) selects only NULL/open/broken_no_retest/retested states. No fault-injection test was run. Prefer one transaction for coherent episode evidence and propagate/represent partial read failures.

#### Other bounded source concerns

- [A/B] **Level aging clock is refreshed by non-state writes.** `EnsureLevel` (`store/level_state.go:160–181`) calls ordinary GORM `Update("price", ...)` on existing rows; the model's UpdatedAt is autoUpdateTime (:67). `AgedFreshness` (:282–311) starts from UpdatedAt. Production `trader/auto_trader_levelstate.go:95–113` repeatedly ensures active levels; the provider at `trader/auto_trader_dayplan.go:176–188` reads aging. [B] continued price refresh can reset the aging reference; the pure test `store/level_state_aging_test.go:56–91` constructs timestamps directly and does not cover repeated EnsureLevel. Unknown applicability to a given continuously active level, not a demonstrated stale burn.
- [A] **All-unresolved holding bucket disappears.** `store/position_query.go:537–548` emits only `r.count > 0` even if unresolved count is positive. `GetFullStats` and `GetDirectionStats` retain unknown counts; this function loses a whole unknown-only cohort. `store/pnl_truth_test.go:91–102` tests unresolved rows in a bucket already containing resolved rows. Directly inspectable edge case; consumer reachability for this specific statistic was not established.
- [A] **Model-selection paths disagree on environment-only credentials.** `AIModelStore.GetDefault` → `firstEnabledUsable` filters `api_key != ''` at `store/ai_model.go:159`, before `hasUsableAPIKey` can examine environment keys (:191–207). `GetAnyEnabled` (:175–188) has no such SQL filter. Caller populations and actual environment were intentionally not inspected.
- [A] **Completion flags can suppress retries after partial migration failure.** `CorrectHistoricalPnL` (:26–73), `BackfillPnlCorrectedAll` (:93–140), and `BackfillEntryConfidence` (`store/position_backfill.go:26–65`) continue after individual write errors and then set their done flag. Confidence scan additionally limits to 2000 rows before setting done. First PNL correction lacks the later pass's positive entry/exit guard. These are static restart/retry hazards, not fresh corrupt-row findings.
- [A] **Owner-level blank-user bucket is shared fallback.** `store/owner_level.go:70–80,94–104,112–120` includes `user_id=''` for every nonempty user despite a comment implying second users see only their own. Boot migration (:44–53) usually claims blank rows for oldest trader; new/remaining blank rows are still readable/editable across users. API caller validation not exhaustively traced; no data leak demonstrated.
- [A] **Concurrent idempotency depends on caller serialization.** Alert Emit (:57–73) count-before-create lacks unique trader/event key; opportunity close (:73–109) selects NULL but UPDATE repeats only id; NextOrdinal/SaveOutcome (`touch_outcomes.go:219–244`) are separate without episode uniqueness. Weekly matched-random PK protects duplicate storage but loser receives create error. These differ from structural-geometry's transactionally deduped counter path and PlanStore's per-instance serialization. No concurrent execution was reproduced.
- [A] **Undefined input cases in pure research helpers.** `ResolveScenarioLink` (`store/scenario_link.go:58–95`) disables band/ambiguity checking when band<=0 and can index -1 if every anchor distance is NaN. `BuildContinuous` (:37–73) does not check finite basis or row contract/order; zero measured basis is forbidden even if genuinely measured. Upstream validation was not traced, so these are robustness boundaries rather than active failures.
- [A] **Analytics definitions need honest labels.** Drawdown at `position_query.go:372–398` uses literal starting equity $10,000, not trader starting balance. Sharpe is unannualized mean/sample-SD of per-trade dollar PNL. Profit factor returns zero with no losses. These calculations are source facts, not broker/account performance claims.

### End-to-end ownership and flow

#### Configuration into runtime

[A] `store/ai_model.go` keeps encrypted model keys typed as `crypto.EncryptedString`; exact user/id lookup may fall back to `default` user. Explicit new provider entries use unique id suffixes; deterministic provider choice is enabled first, newest update, smallest id. Update-with-empty-key preserves secret; clearing per-model thinking overrides writes empty strings intentionally. Cross-user/internal methods (`GetByID`, `AdoptModel`, `GetAnyEnabled`) do not supply endpoint authorization themselves.

[A] `store/trader.go:GetFullConfig` (:232–269) first resolves the user-owned trader and its user-owned model/exchange. `StrategyID` is preferred; missing or failed lookup falls back to active/default, and those strategy lookup errors are discarded. Thus a broken explicit binding may silently become another strategy; audits must join the actual trader binding and confirm runtime loading. Account choice has a separate scoped `UpdateAccount` method and is not overwritten in generic Update.

[A] `store/strategy.go` is the configuration contract shared by kernel/API/trader, not itself a trading executor. Custom Unmarshal accepts nested and old flat schema; nested AI wins if both supplied. Merge uses JSON round-trip and recursively combines maps, mutating the caller's patch normalization. ParseConfig only applies missing coin/kline defaults; ClampLimits and ValidateIndicatorPeriods are distinct calls. Numeric risk clamps/defaults are the legacy fields, while daily-loss and mechanical-exit policies are fields consumed elsewhere. New futures strategy defaults seed static MNQ and ATR/EMA/RSI, disable crypto ranking/OI sources, keep MACD/BOLL off. Static SupportedTimeframes includes 14 intervals. Timeframe/symbol aliases and CME month-family tables are local copies requiring parity with market.

[A] DayPlan root config survives type-specific serialization. Per-session settings resolve named override → strategy/default with meaningful explicit zero for some pointer caps. AcceptanceRuleFor rewrites legacy 2x5m/15m forms to 5m_close at read; DefaultDayPlanConfig still seeds `2x5m`, so literal seed and effective rule differ. Wake spacing effective default is 30 minutes despite older 10-minute comments. Replans are **recorded spend events** only for death_replan/owner_reread, keyed by trader/date/session/reset baseline; no version arithmetic determines spends. Spend mutex is process-local; malformed/read-failed counters can read as zero/full budget. Indicator fingerprint freezes only prompt-relevant settings and excludes API secrets; token estimator is static approximation with hardcoded provider-family limits, not observed provider capacity.

#### Plans and execution evidence

[A] `store/plan.go` uses stable trader-scoped chain identity and max(version)+1 inside a dedicated per-instance write goroutine. Overlay versions are independently numbered per plan/version. SQLite DDL enforces JSON validity and composite keys; ignored additive DDL/index errors can obscure schema drift. This is single-instance serialization, not a distributed transaction guarantee. Close signals shutdown but does not join an in-flight write. Lifecycle is mutable while authored doc/version stays append-only; state changes append a telemetry log and never rewrite authoring trigger. `PlanDB.StrategyID` is historically misnamed and actually holds trader id.

[A] `store/arm_state.go` is the conservative lifecycle boundary shared by Go and generated SQLite SQL: unknown states are nonterminal, cancel_pending stays exposure but is not sweepable, and armed with blank signal is authorization only. Unicode trimming/lowercase mapping is deliberately reproduced in SQL. Boot-sweep selects foreign/NULL boot ids and persists recorded sweep count. Received NT8 snapshots store raw orders JSON and clocks; accepted-risk rows append distinct broker terms with NULL for unavailable broker price, alongside simultaneous ledger terms. No update/upsert API exists for accepted-risk records, though the shared DB could still be mutated elsewhere.

[A] Structural policy has MNQ default buffer 4.5 (training p95 provenance), cost 2, and strategy RR. Explicit invalid buffer/cost becomes unknown rather than silently defaulted. `SaveStructuralGeometry` atomically stores current decision and counts each plan/version/scenario/leg/reason once per CME date. An admitted current record does not erase prior refusal count. This table does not place orders or size a live account; actual geometry/placement is the sibling trader assignment. Post-loss, shadow-AB, boot-sweep and generic counters record explicit events, not inferred historical events.

#### Tape, episodes and research

[A] Contract/source migrations label existing bars additively, using measured timestamp intersections and explicit symbol-specific historical exceptions. They do not fabricate prices. Contract readers return point/window contract labels; `WindowContract` compares endpoints, may use the sole known endpoint, and does not certify interior source quality. Live readable sources are live/historical; imported history requires separate backtest eligibility. BuildContinuous creates explicitly adjusted output and never writes it.

[A] TouchOutcomeRow is the durable shared episode schema: observed detector verdict, level formation/validity, named identity or heuristic link, nullable opportunity/fade/one-setup/follow evidence. RatesBy reads only ValidityValid; ambiguous is excluded from hold/(hold+break), reported with counts/Wilson interval and descriptive floor n<200. Recorders must supply correct formation and session-day/window because this store cannot reconstruct them. The watermark/ordinal are durable queries but failure can read as zero/one. Empty trader id in opportunity/follow helpers intentionally means all traders and is an internal maintenance convention, not safe user authorization.

[A] Scenario linking is explicitly heuristic, ambiguity NULL. Attainable entry prioritizes observed fill, then named arm assumption, then first tradeable after confirmation; the drawn level is never substituted. Episode closer delegates facts/entry observations to callbacks and preserves uncomputed attainable columns. Identity events use base64-encoded tuples; identity backfill requires full recorded inputs from exact plan version and exact existing name, leaves pre-era rows untouched, and persists per-row three-state outcomes. Follow-plan observations never imply order authority. Matched-random and excursion aggregators are global research stores, not per-user execution gates.

#### Position lifecycle, PNL and observability

[A] PositionBuilder routes open/close actions; open may merge weighted entries, partial close delegates quantity reduction, full close computes weighted exit and preserves broker cause through `ExitCauseFromBroker`. NT8 caller excerpt `trader/ninjatrader/close_sync.go:154–170` computes point-value-aware PNL then calls `ProcessTradeWithExitReason`, correcting the historical graph's old direct ProcessTrade edge. Builder itself is not an ordered-event queue or FIFO engine despite introductory prose.

[A] Position query surfaces use corrected PNL, not raw fallback. Unknown closes end consecutive-loss streaks; synthetic e7 seam rows do not count. Recent-trade list retains unresolved entries but sets resolved=false and no computed percentage. Daily activity sums only resolved valid closes but counts **all entries**, including entries whose eventual PNL remains unknown. Optional account filter is on selected APIs, not every aggregate. Source-based seam grading uses a separate classifier whose source read fails open; its SQL and Go whitespace treatment differ. Global histogram does not prove all PNL readers use the same seam definition.

[A] Alert feed uses soft dismissal preserving audit rows and refuses unacked P0 dismissal. Watchdog/cut records carry fresh/reused connection idleness and explicit unresolved resend; ResolveLatest associates only by trader/newest unresolved rather than unique call id and masks lookup errors. Idle renderer sets no timeout threshold. Wave-A backup helper runs sqlite3 `.backup` before caller-authorized migration; migration function itself does not enforce backup/flag, converts every remaining zero excursion and uses no era cutoff. Re-running after real measured zeros appear requires caller policy care. Constructors that lazily AutoMigrate mean even some apparently read-oriented helper entrypoints can attempt schema writes; none were called against production here.

### Assigned-file coverage index

The following inventory is generated from this review's manually read files. Each entry's exact named function boundaries and call expressions are in `functions.json`; detailed per-file notes are in `reads.json`.

- `store/accepted_risk.go:1–120` (6 named functions): Append-only broker acceptance evidence, distinct nullable accepted prices and mutable-ledger prices; epoch-ms clock. Constructors/counts swallow DB errors; nil Append succeeds without recording.

- `store/ai_model.go:1–504` (23 named functions): Per-user encrypted model rows, exact-id lookup with default-user fallback; deterministic enabled/newest/id provider selection. Empty API-key updates preserve secret. firstEnabledUsable filters empty DB keys before env fallback, unlike GetAnyEnabled.

- `store/alert.go:1–183` (12 named functions): Trader-scoped feed/ack/dismiss with durable soft deletion; unacked P0 cannot dismiss. Emit count-before-create dedupe has no unique trader/event constraint. PostgreSQL existing-table branch skips additive migration.

- `store/arm_state.go:1–131` (10 named functions): Canonical conservative terminal classification shared with generated SQLite SQL including Unicode trim/lower parity. Unknown state remains exposure; only armed+empty signal is unplaced. Sweep excludes cancel_pending.

- `store/attainable_entry.go:1–84` (1 named functions): Pure measured-fill > assumed arm > first tradeable-after-confirm priority. LevelPrice is deliberately never used; missing recorded confirmation price differs from never attained.

- `store/bar_contract_roll.go:1–244` (6 named functions): SQLite additive contract migration uses measured 2026-09-10 per-symbol window intersections; census and newest/point/window contract resolution. ContractAt skips mixed labels but not source labels; unknown TF falls back to one minute.

- `store/bar_source.go:1–175` (4 named functions): Measured source migration labels mixed/off-scale exceptions before legacy-live default; strict live/historical readability and separate backtest historical_import eligibility. Historical sample ids are comments, not reverified runtime evidence.

- `store/boot_sweep.go:1–88` (5 named functions): Process boot id via sync.Once; preboot sweep selects other/NULL boot and sweepable states; atomic persisted boot-swept counter; DB exposed for fixtures.

- `store/continuous.go:1–73` (1 named functions): Pure explicit-basis cumulative OHLC adjustment with continuous/mixed labels; zero basis refused. Caller owns ordering/overlap; finiteness and row-contract correspondence are not checked.

- `store/driver.go:1–281` (14 named functions): Legacy sql.DB abstraction for modernc SQLite and lib/pq PostgreSQL, environment defaults and syntax helpers. SQLite uses DELETE/FULL with one connection. Placeholder replacement is lexical, not SQL-aware.

- `store/fade_or_history.go:1–57` (1 named functions): Prior session OR median uses before location and clock, n-limited descending stored 5m rows. Raw query lacks contract/source filtering; errors collapse to (0,0).

- `store/idle_outcome.go:1–125` (3 named functions): Watchdog/cut records aggregated into fresh/reused-idle buckets with resolved/recovered/lost separation; deterministic rendering warns small n and sets no operational threshold.

- `store/indicator_fingerprint.go:1–52` (1 named functions): SHA256 first eight bytes of ordered prompt-indicator projection; no API secret or crypto ranking included. Nil/empty lists normalize by omitempty, list ordering stays meaningful.

- `store/knob_registry.go:1–215` (6 named functions): Reflection walks JSON-tagged StrategyConfig leaves, registry exact-path then leaf fallback, counted boot labels. Custom Marshaler AI fields tagged minus are invisible to enumerator; shared leaf fallback can mask collisions.

- `store/level_identity.go:1–163` (5 named functions): Base64-scoped immutable identity-event keys with allowed-kind and clock validation; counts decode durable JSON. Backfill requires exact named id in exact stored plan/version and leaves pre-era rows untouched.

- `store/level_state.go:1–383` (20 named functions): Trader-scoped price-bin state; ensure preserves burn state but price Update advances UpdatedAt. Play increment and freshness decrement separate; aging uses UpdatedAt with 17:00 CT calendar-day boundary, potentially extended by price refresh.

- `store/matched_random.go:1–138` (10 named functions): Append touch verdicts, global type tallies and ISO-week first-write snapshot. Primary-key prevents duplicate weekly rows but count/create race surfaces error. ResetWindow deletes both tables sequentially without transaction.

- `store/nt8_order_snapshot.go:1–75` (6 named functions): Received order-list JSON forensic snapshots with emitted/received clocks; insert errors returned, account+symbol latest read and timestamp prune. Gate source remains outside this table.

- `store/one_setup.go:1–436` (16 named functions): Latest per-plan scenario sidecar, once-only episode verdict stamps, day counters, forward follow-plan fields. Follow field writes individually ignore errors before state update; Readable counters can hide failed reads. Proximity stamp lacks plan-version restriction.

- `store/opportunity_backfill.go:1–102` (2 named functions): Three-state legacy classification from era/formation/scenario link. Recomputable rows marked reached_declined with session-close cause; terms not fabricated. Partial row updates, no transaction.

- `store/opportunity_close.go:1–166` (5 named functions): Optional empty trader/plan/session means all; callbacks own facts and attainable observations. NULL-selected close loop writes by id without repeating NULL predicate, allowing concurrent overwrite; records first close only under serialized use.

- `store/owner_level.go:1–120` (9 named functions): User-scoped sticky level persistence with empty legacy rows visible/editable to nonempty users; migration assigns blank owners from oldest trader. API must supply user. Internal consume/delete are id-only.

- `store/plan.go:1–547` (28 named functions): Append-only plan docs and overlay versions via per-instance single writer; lifecycle mutable with separately appended event log. StrategyID column actually stores trader id. SQLite JSON-valid checks; optional/global legacy readers retained.

- `store/pnl_correction.go:1–160` (4 named functions): Additive historical MNQ price*qty*$2 corrections and immediate close stamp. Per-row NULL guard preserves corrections, but completion flags set even after row update errors; first pass lacks nonpositive-price refusal.

- `store/position_backfill.go:1–106` (2 named functions): Once-flag confidence inference from recent action/symbol decisions around entry, capped 2000 candidate positions. -1 means looked/no result, 0 unpopulated. Flag may finish partial/error run; time association is heuristic.

- `store/position_builder.go:1–211` (6 named functions): Trade action dispatch to open/average and partial/full close persistence; broker exit reason carried explicitly. No sorting or FIFO queue in this class; no-match close skipped. Full overclose clamps qty but not supplied PNL/fee.

- `store/position_query.go:1–601` (10 named functions): Strict corrected-PNL statistics with null exclusions, account-specific full/recent reads, loss streak broken by unknown close. Holding buckets with only unresolved rows omitted; drawdown assumes starting equity 10000; Sharpe unannualized PNL ratio.

- `store/post_loss_counter.go:1–82` (5 named functions): Atomic per-trader/date/session rearm counts, latest resolved losing close among newest 20 rows, bounded nonfuture loss window. Counter only, no refusal; unlike streak unknown closes skipped.

- `store/research_record.go:1–23` (2 named functions): Panic-contained research snapshot around placement timeout; observed/receipt same supplied clock, write error nullable; telemetry never gains placement authority.

- `store/scenario_link.go:1–95` (1 named functions): Pure nearest-price heuristic with two-in-band ambiguity -> NULL; named basis and distance. Nonpositive band disables band and ambiguity checks; invalid NaN anchors can leave best index -1.

- `store/seam_grading.go:1–138` (6 named functions): Source-based test-seam classifier, SQL exclusion histogram and stamp migration. Source-read error fails open. SQL does not fully trim like Go; stamp action and counts distinguished.

- `store/shadow_ab_counter.go:1–43` (2 named functions): Persisted atomic shadow experiment count across restarts; unset/parse failures return zero. Does not authorize live experiments.

- `store/strategy.go:1–2487` (84 named functions): Custom nested JSON compatibility, limits, timeframe/symbol normalization, language/futures defaults, day/session accessors, recorded replans and reset keys, CRUD, token estimates. Grid serializer drops AI bundle despite preservation helper; ParseConfig defaults do not clamp.

- `store/structural_geometry.go:1–181` (7 named functions): Structural-stop policy default MNQ buffer 4.5 and cost 2, provenance known flags and strategy MinRR. Atomic sidecar + deduped per-day refusal counts. Historical RiskCapUSD is output compatibility only; no extra per-trade admission cap.

- `store/system_counter.go:1–39` (2 named functions): Generic persisted atomic integer increment; blank keys refused for increment, malformed read parses to zero. Counter records, never infers.

- `store/touch_outcomes.go:1–461` (15 named functions): Durable episode with validity, formation, scenario identity, opportunity, fade/one-setup/follow evidence. Rates use valid only and exclude ambiguous denominator. Ordinal/watermark reads lack unique write guard and return defaults on failure.

- `store/trade_excursion_stats.go:1–159` (6 named functions): Grouped measured MAE/MFE nearest-rank distributions with unknown-level/unmeasured counts, undefined percentiles NaN rendered dash; dimension allowlist, global rows.

- `store/trader.go:1–338` (20 named functions): User-scoped trader bindings and CRUD; runtime modes explicitly allowlisted. FullConfig resolves bound strategy then active/default on missing/error. Delete removes equity by trader id before ownership-scoped deletion and ignores zero affected.

- `store/visibility.go:1–105` (6 named functions): Visibility is any configured field/enabled, not credential completeness. Separate per-exchange required-field list still requires NTDataDir for NinjaTrader. Nil model/exchange/trader/strategy not visible.

- `store/watchdog_fire.go:1–109` (5 named functions): Durable cut/watchdog event and latest-unresolved-per-trader resend attachment; Record stamps clock. ResolveLatest swallows all lookup errors as absent; concurrent calls have no call-id identity.

- `store/wave_a_migration.go:1–201` (5 named functions): Flag predicate + sqlite3 backup helper, legacy duplicate/unverified episode classification and zero-excursion NULL conversion, boot census. Migration method itself neither checks flag nor performs backup and has no zero-age cutoff; caller must gate one-time use.

### Graph comparison and limits

[A] Historical Understand Anything graph is July 10 at `7a8adce0`, not this base. Extracted **72 nodes and 671 incident edges** for this assignment into `historical-graph.json`; only **7/41 current assigned paths** exist there: ai_model, driver, position_builder, position_query, strategy, trader, visibility. Root's historical CGC file export likewise includes only those seven paths (781 indexed files globally). Thus **34 assigned files**, including structural geometry, arm states, plans, provenance migrations and episode contracts, are absent from both historical file inventories. No reindex/delete/watch was attempted. No CGC function-query result is claimed beyond root's exported file inventory.

Historical graph edge types: 510 imports, 40 calls, 65 contains, 50 exports, four tested_by, one documents, one related. Imports fan out one package import to many store files; these are package relationships, not proof of calls to each file. Manually inspected selected node summaries and all 40 recorded call relationships; raw incident graph is saved for root. Current AST inventory supplies a broader exact syntax census, but cannot resolve interface/dynamic calls.

Explicit corrections:

- Graph `store/driver.go:openSQLite` says WAL; current :172–207 sets **DELETE**. `boolDefault` graph says env parser; actual :269–281 returns SQL TRUE/FALSE or 1/0 literal. These are substantive summary errors, not just line drift.
- Graph `store/visibility.go:IsVisibleExchange` says fully credential-complete; actual :72–89 is **any configured field OR enabled**, independent of MissingRequiredExchangeCredentialFields.
- Graph `store/ai_model.go:Get` says raw/user-prefixed id tolerance; current :96–123 checks exact id under user then default-user candidates. Graph `hasUsableAPIKey` claims placeholder refusal; current :191–207 accepts any nonblank string or provider env key. Graph UpdateWithName mentions clamps/wallet-specific handling not present in this function.
- NT8 `recordClose → ProcessTrade` historical edge is now `recordClose → ProcessTradeWithExitReason` at `trader/ninjatrader/close_sync.go:169`. Store builder then delegates to handleClose and ExitCauseFromBroker; broker-cause preservation is now part of the boundary.
- Position statistics graph omits corrected-PNL-only population and unresolved count semantics, account filters, and unknown-close streak breaking. Current function names alone do not reveal these policy changes.
- Strategy graph omits DayPlan/Regime, reset/counter accounting and latest structural-stop policy. Its language about per-symbol grid configs overstates this single `*GridStrategyConfig` field.
- Plan immutable-doc/lifecycle-mutable distinction and trader id in `PlanDB.StrategyID` have no historic graph coverage.

### Validation and remaining work

[A] Existing tests manually read: complete `preserve_ai_config_test.go`, `knob_registry_test.go`, `structural_geometry_test.go`; excerpts of `pnl_truth_test.go`, `level_state_aging_test.go`, and `arm_state_test.go`. Structural geometry fixture verifies distinct refusal count survives pending/admitted cycles and scopes trader/version. Arm-state fixture exercises production store readers with whitespace/Unicode forms, which is stronger than a standalone predicate comparison. Test paths discovered but **not fully reviewed** include AI-model multi-entry, alert dismiss, bar contract/source, one-setup and further level-state tests. No new or existing tests were executed by this worker; root owns reproduction and merged validation.

Root update received at closeout: **root reports F15-1 separately reproduced and repaired at `576bd75b`**. This review remains evidence about base `63968be`; this worker did not inspect or validate that separate fix. The principal additional actionable finding is **F15-2 AI config lost at the actual serialization boundary**; follow with **F15-4 serialized-schema census gap**, **F15-5 partial follow writes/readable counts**, and **F15-3 unfiltered prior OR history**. Root should choose isolated fixtures before patching them. None warrants altering owner trading controls or accounts.

All assigned source coverage is complete; unresolved items concern runtime state, caller reachability beyond the named excerpts, concurrent/error reproduction, imported-history interior seams, and full consumer parity. This slice does not establish whole-repository understanding or live trading correctness.

---

<a id="section-21"></a>

> Original: [reviews/16/report.md](reviews/16/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 16 — persistence and configuration source review

Base: `63968be62e44db2fb07a92883e02127b9064b0be`. Worktree `/tmp/vl-understanding-surfaces-20260913` verified at that HEAD with empty porcelain before review. All **43 assigned files, 10,006 lines and 458 named declarations** read in full. Additional caller/test excerpts are recorded separately. No source, live DB, account, environment, runtime or order mutations. No tests executed by this worker; root owns reproductions and repairs. [A] means direct source evidence, [B] static inference, not a demonstrated live failure.

Rules consulted: root AGENTS.md; tracked CLAUDE-canon (new keeper semantics supersede stale hand-heartbeat instruction); AUDIT-CHECKLIST classes 7, 19, 28, 29, 35, 40 and PART2 R1–R10. SYSTEM-MAP and corrected RULEBOOK sections on storage, bars, P&L, settings and entry economics. Latest commit for BOTH documents: `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap`. Review uses the owner correction: daily loss controls remain; no new mandatory per-trade cap is inferred. Supplied base is review target; worker did not certify the running process revision.

### Important findings and exact boundaries

1. **[A, source BROKEN] Placed terminal arm bypasses manual-cancel-wins.** `ArmedOrderStore.UpsertArm`, `store/armed_orders.go:235–385`, selects a live row first (262–263), refuses working/place_pending/unknown nonterminal and cancel_pending (270–286). However terminal+signal enters successor Create at 292–307 before same-version restriction 346–347. Production `trader/armed_executor.go:790–806` constructs a fresh candidate and invokes this when ListNonTerminal cannot find the terminal predecessor. SYSTEM-MAP:219 claims the same-version restriction also covers terminal+signal, contradicting source. `store/armed_orders_test.go:201–242` tests terminal rows WITHOUT signal IDs; `store/armed_append_only_test.go:113–136` explicitly assumes replacement without a version change. Root separately reports reproducing and repairing ordering on its repair branch; that repair is outside this immutable base review.

2. **[A, source defect; B operational impact] Successor misses provenance initialization.** The same early Create at armed_orders:307 skips BootID and ArmedUnderVersion defaults at 375–383. Real candidate literals at armed_executor:790–795 omit both. Even after moving version veto earlier, a legitimate new-version successor can therefore start with blank boot and first-authorization version zero. In-place armed refresh repairs zero version only on a subsequent upsert (314–322), while preserving its blank boot; immediate readers/sweep can see missing provenance. `store/attribution_test.go:91–129` tests initial/in-place refresh only. `attribution.go:GetArm:83–90` also still uses First without placement ordering, returning oldest placement rather than live/latest. No row count or runtime impact asserted.

3. **[A, source path defect] Foreign-boot cancellation budget reset is behind its own cap.** Store `RequestCancel:465–490` resets attempts when CancelAttemptsBoot differs. The production pass `trader/cancel_confirm.go:349–425` checks old attempts against cap at 399 before calling RequestCancel at 415. A capped old-boot row with no settlement proof hits continue and never reaches reset. `store/cancel_budget_boot_test.go:18–99` directly calls RequestCancel, bypassing the problematic production branch. Root notified for call-site reproduction; worker did not run tests.

4. **[A, source BROKEN] History recent win-rate includes unresolved denominator.** `GetHistorySummary`, position_history:39–123: recent rows loaded (105–107), corrected NULL skipped (110–113), but denominator is len(recent) (119), including skipped rows. Example static arithmetic: one resolved win + one NULL yields 50%, whereas resolved cohort is 100% n=1. This is a synthetic arithmetic example, not a live-row claim. Summary's UnresolvedExcluded is the broader GetFullStats count, not a recent-specific denominator disclosure.

5. **[A, source BROKEN] Historical current/max streak algorithm.** `calculateStreaks`, position_history:126–184, walks newest→oldest, resetting currentStreak at each transition then assigns it after the entire scan; output is the oldest run, not current run. Maximum counters update only on extending a run, so singleton maxima remain0. Zero-P&L resolved row becomes a loss (`pnl>0`); NULL is skipped and bridges runs. This is AI history presentation; the risk-breaker query is separately implemented in position_query.go and was NOT reviewed as part of this finding.

6. **[A schema declarations; B fresh-schema risk] Order/fill index name collision.** order.go:24 and :61 apply uniqueIndex idx_orders_exchange_unique / idx_fills_exchange_unique only to the external order/trade ID field; ExchangeID fields have no matching tag. InitTables:140 AutoMigrate followed by :145–147 CREATE UNIQUE INDEX IF NOT EXISTS of intended composite uses the SAME name. Inference: fresh GORM schema may retain globally unique external IDs, refusing legitimate equal IDs across exchanges. Index Exec errors are ignored. Root notified; no DB inspection/reproducer here.

7. **[A configuration sequence; B pool risk] SQLite PRAGMAs are not established per connection.** gorm.go:22–64 opens path, sets pool4 at45–47 and sends unchecked separate PRAGMA Exec at50,57–59. foreign_keys and busy_timeout are connection-local; this code alone does not ensure each pooled/recycled connection inherits them. WAL comment at54 still claims MaxOpenConns(1). No assertion about live connection settings.

8. **[A] Read-only facade access can migrate, and opening Store mutates data.** Store.New/NewWithConfig invoke initTables/defaults (store.go:63–126). Exchange.initTables triggers `cleanupIncompleteExchangeConfigs` (exchange.go:90–124), deleting incomplete configs and enabling disabled complete configs. Position zero→NULL migration runs unconditionally in store.go:239–243. Lazy constructors for candidate pool, config diffs and planner facts/rejections AutoMigrate while discarding errors. Consequently use of New against a supposedly read-only audit DB is unsafe; this worker never opens one. Existing migrations are described, not authorization to run them.

9. **[A] Other scoped truth limitations.** `fade_stamp.go:98–108` ignores sinceMs in OpenFadeCandidates. `calendar.go:108–132` says past dates stay frozen but does not test date; callers must enforce it. `GetLedgerDayTotal`, pnl_surface_guard:63–84, excludes unknown close reasons before counting unresolved NULL, so the count is not every unresolved close in the day. `CountCancelPending/CountCancelUnconfirmed/StateCensus` return zero/empty on DB error, unlike CountForeignBootCancelAttempts returning -1. Many telemetry counter readers similarly conflate missing/malformed/error with0. `grid.go:578–588` TotalPnL lacks explicit total_pnl column mapping while SQL alias has it (known naming pitfall); legacy surface relevance remains unproven. Exchange.Create/Update accept enabled but force true (exchange.go:269,305).

### End-to-end connections and ownership

**Facade and initialization.** New→InitGorm→SQL connection→initTables→initDefaultData, failing construction on most explicit migration failures; lazy facade accessors under one mutex return cached sub-stores. NewFromGorm wraps without migration, NewFromDB is legacy SQL-only and cannot supply GORM-backed methods. Store.Close closes DB but does not drain LogEventStore's writer. This is persistence authority, not an execution gateway. Generic ID-only methods rely on validated caller ownership; scope absence is not by itself a proven endpoint vulnerability.

**Authorization to broker receipt.** Production candidate literal/UpsertArm described above → `BeginPlacement` (412–425) atomic predicate armed+empty signal sets place_pending before socket send → `ApplyPlacementReceipt` (431–456) promotes only pending to working; rejected receipts can enrich an absent reason while preserving terminal state. `ExpirePlacement` (692–697) records timeout reason without freeing slot. `RequestCancel` persists intent/budget; confirmation pass reads a fresh persisted broker book with positive snapshot ID, then `ConfirmCancel` (495–504) stores cancelled+evidence ID. Store ConfirmCancel itself accepts zero snapshot and arbitrary prior state; SetState also permits arbitrary transitions. Thus semantic enforcement partly resides in callers, and races between read and unguarded writes need their own production tests. A Go state write does not prove a wire action.

**Market tape.** BarHistory.Migrate (202–258) creates schema, one-time backup/dedupe and old aggregate deletion if unique index absent, then contract/source migration delegates. InsertBars (265–324) batches200, requires contract and live/historical/mixed source, accepts all nonempty TFs, and upserts natural key symbol+TF+open-ms; historical cannot overwrite live/mixed. ImportBars (337–380) requires historical_import and does no updates, returning inserted/skipped counts; batches can partially commit before a later failure. LastNBarsOn (530–551) filters mixed/off-scale and optional contract, reverses newest selection to ascending; BarsBetweenOn (476–487) uses half-open time. Empty contract deliberately gives audit-style unfiltered data. TF retention (99–127) exempts imports; old PruneOlderThan (459–465) does not. No validation here of OHLC shape, timestamp alignment or actual closedness; upstream canonical bar writer owns these. Same-minute other-contract history collides because contract is not in PK.

**Positions, P&L and lineage.** Position.Create (415–443) inserts OPEN and then explicitly nulls unmeasured excursion pair. Guarded reconcile close (453–489) only changes still OPEN rows; other quantity/close methods are ID-only read-modify-write. Closed rows retain raw realized P&L, nullable corrected P&L and correction note. CorrectedPnL (953–958) returns value+known; EffectivePnL (942–947) remains raw fallback only for approved per-row contexts. Adherence write blocks test-seam grading; plan link writer captures plan/version/scenario. Attribution distinguishes unstamped vs UNRESOLVABLE vs linked, with CT era boundary and first-authorization version separate from last touch. LedgerDayTotal strict sum is reporting, not the daily-risk gate. StopTargetNear (decision:492–529) is a nearest-timestamp heuristic for opening stop/target; it has no account, side or action-success check and only scans earliest20 in window.

**Telemetry/research.** TouchEpisodeStore is older append-only touch telemetry and ordinal seed; TouchOutcomeStore methods in fade_stamp record one-time fade labels, and OpportunityOutcomeFor is deepest factual rung. TradeExcursionStore records measured nullable path/exit and corrected-only P&L; it cannot represent unfilled opportunities, hence outcome ladder. AbConfirmStore is independent counterfactual four-rule data with MNQ point value; repair is opt-in with online backup helper and leaves unrecomputable cases explicit. It cannot reconstruct bad original fill timing from arithmetic alone. CandidatePool records seated and cut populations, but global row pruning may split a read. PlannerReadFacts records rendered inputs even on successful planner reads; rejected prompt store retains verbatim rejected attempts/facts separately. All are observational at this boundary.

**Configuration, memory and external settings.** ResolveMinRiskReward/HTFVeto/PlanMode/OneSetup give resolved values plus source labels; knob registry provides historical consumer evidence, not executable enforcement. Config diff serializes configs and compares leaf values; caller must supply resolved configs. User/Exchange are user-scoped storage boundaries with hidden password hash/encrypted credential types, whereas TelegramConfig is a single plaintext-token row protected by an instance mutex and masked String formatting. No actual secrets read. Calendar/session-profile/digest store time-keyed memory; digest adds trader scope but lacks database uniqueness for its check-then-create natural identity. AI charge prices are hardcoded estimates per call, not provider billing.

### Historical map contrast and limits

Historical Understand Anything July10@7a8adce0 export contains **117 matching nodes, 12 matching file nodes and 1084 incident edges** for this assignment. The other31 assigned file paths are absent. Saved export is `historical-graph.json`; direct manual graph inspection covered the12 file summaries and45 non-containment file edges (mostly package-import expansion), not every exported edge. Those imports fan one store package dependency across many files; they are not proof each caller uses each store function. Current AST connections are explicitly syntax-only, not type resolved.

Corrections: historical ai_charge description says token costs; current GetModelPrice/Record charges one fixed per-call estimate. Historical decision summary says account/position snapshots persisted, but current DecisionRecordDB has no snapshot fields, LogDecision writes none, and toRecord restores none. Historical position language note says ClosePositionFully computes holding duration and P&L; current method accepts supplied P&L/time and writes them. Historical facade omits most newer plan/tape/arm/research stores. Core user/exchange/equity/grid role descriptions remain broadly useful but do not establish present enforcement. CGC historical index freshness is root-provided; this worker did not query or reindex CGC.

Stale current comments also matter: bar_history:13–22 and260–264 claim INSERT OR IGNORE/1m-only/single retention, superseded by actual upsert/all-TF/per-TF behavior. position.go:184–187 still recommends raw fallback despite CorrectedPnL law. Source comments containing old row counts are historical claims, not live facts verified here.

### Test evidence and unresolved branches

Read `armed_append_only_test.go` fully; manual-cancel/version-bump portions of armed_orders_test; attribution stability portion; cancel_budget_boot_test fully. Tests demonstrate coverage shape only, not this worker's execution results. Relevant discovered fixtures (not full-read/executed here) include bar_history_test, history_import_test, bar_contract_roll_test, class33_boot_sweep_test, position_side_casing_test, pnl_surface_guard_test, trade_excursion_test and test_seam_exclusion_test. Root handles independent tests/repair validation. No live sample IDs are asserted; IDs in source comments remain historical annotations. PostgreSQL compatibility, multi-connection races, API authorization reachability, runtime ordering and broker settlement require separate verification.

### Per-file coverage and exact named-function boundaries

See `functions.json` for all458 declarations with start/end, purpose, input/output and syntax-only call connections; per-file notes below are grounded in the full reads. Anonymous callback semantics are included under parent functions, plus episodeDetectorScope's file-level note. Registry literal contains no named function.

- **store/ab_confirm.go (1–461)** — Counterfactual four-rule rows keyed plan/version/scenario/rule; short arithmetic repair is optional, MNQ $2/point, distinguishes missing inputs/direction/bad fill geometry. Upsert update map omits direction/recompute; historical repair cannot establish original fill bar.

- **store/adherence_regrade.go (1–153)** — Opt-in regrade clears only CLOSED matched full-lineage D outside off_band/struct and test seam; backup helper precedes external orchestration. Scan IDs then update is not atomic predicate recheck.

- **store/ai_charge.go (1–169)** — Approximate fixed per-model call prices, not token billing. Today uses host-local midnight, explicit date UTC. Aggregate methods suppress query errors.

- **store/arm_normalized_counter.go (1–40)** — Atomic system_config increment; follow-up count read can observe later increments and masks absent/error/malformed to zero.

- **store/armed_orders.go (1–744)** — Durable per-slot placement history. BeginPlacement CAS precedes wire; received receipts guard terminal resurrection; pending cancel holds slot. Terminal successor precedes version veto and skips new BootID/ArmedUnderVersion; see report.

- **store/attribution.go (1–239)** — Three-way unstamped/unresolvable/linked identity; CT Aug15 era; one-time scoped sentinel conversion. GetArm still lowest-ID First despite append-only placements.

- **store/bar_history.go (1–551)** — Contract/source stamped OHLCV natural key excludes contract; live/mixed protect against historical replay. All TF stored despite old 1m-only comments. Import do-nothing collision; TF retention exempts historical_import.

- **store/calendar.go (1–145)** — Trade-date shared calendar; create-if-absent, upgrade non-live, refresh changed live payload. Store itself does not enforce today-only refresh or incoming-live source; caller owns those checks.

- **store/candidate_pool.go (1–133)** — Per-read seated AND cut candidates; batch create and separate global 20k-row pruning; LatestPool limit can cross reads and pruning can split oldest retained pool.

- **store/class47_counters.go (1–91)** — Durable wake/supersede counters; supersede scans only older-version armed/no-signal rows, then unguarded per-ID SetState updates (concurrent placement needs caller serialization).

- **store/config_diff.go (1–162)** — Sorted dotted JSON diff stored in capped 5000-row history. Resolving before diff is caller duty; function only marshals. Maps recurse despite comment saying whole-map.

- **store/decision.go (1–529)** — API/DB mapping carries prompts, actions, watch/structure, plan attribution and account scope; account/position snapshots not persisted by current model. Statistics swallow errors; TotalOpenPositions counts all rows. StopTargetNear heuristic lacks side/account/success matching.

- **store/digest.go (1–116)** — Trader+symbol+date/session/kind append-style digest; check-then-create has no composite uniqueness in model, so concurrent writes may duplicate.

- **store/episode_boot_line.go (1–56)** — Boot formatter renders supplied recorded counts, explicit backfill n/a, detector k/H and per-read delta source; anonymous resolver variable grouped here.

- **store/episode_detector_scope.go (1–33)** — Mirrors kernel detector env contract due import cycle: positive k default3, horizon default12; float finiteness not checked.

- **store/equity.go (1–209)** — UTC snapshot save; latest scoped optionally account, ascending output; older generic queries trader-only. Latest all-trader query omits account and timestamp tie resolves by map overwrite.

- **store/exchange.go (1–415)** — User-scoped credentials encrypted at type boundary; startup cleanup deletes incomplete configs and enables complete disabled configs. Create/Update ignore enabled argument and force true. NT fields retain legacy CSV metadata.

- **store/fade_stamp.go (1–120)** — One-time nullable fade permission stamp uses supplied evaluation clock; no order authority. OpenFadeCandidates ignores sinceMs; failure count nil-store returns zeros.

- **store/gorm.go (1–169)** — SQLite pool4 WAL/full sync UTC clock, PostgreSQL pool25. PRAGMAs via unchecked one-shot Exec; connection-local settings need pool-wide verification. Global handle overwritten per Init.

- **store/grid.go (1–601)** — Legacy separate grid config/instance/levels/events/regime CRUD; cascade delete transaction, generic ID methods require caller ownership. Stats suppress secondary errors; performance TotalPnL lacks explicit total_pnl tag.

- **store/knob_registry_table.go (1–183)** — Literal registry per schema leaf, live/candidate/ineffective and historical consumer anchors. Method readers explicitly revise old field-grep false negatives. Registry itself is not enforcement; no named funcs.

- **store/level_stats.go (1–108)** — Level/day/trader natural key no symbol; evaluated bool outcomes, per-grade/family unscoped aggregates. SUM(boolean) SQLite-specific portability risk.

- **store/log_event.go (1–129)** — Lazy one-writer channel1024, select-default drop counters, no recursion, event-day retention. No close/drain API; counters process-only and prune errors ignored.

- **store/opportunity_outcome.go (1–67)** — Pure deepest-observed-rung classifier prioritizes filled,armed,confirmed,reached,never-reached; no trades or writes.

- **store/order.go (1–443)** — Exchange orders/fills and watermark queries; intended composite dedup may be defeated by single-field GORM index tags. Duplicate cleanup globally deletes later IDs without remapping fill foreign keys.

- **store/plan_liveness.go (1–179)** — Versioned status freshness5min; unknown nullable tradeable, reversible evaluator vs first-death history. Atomic event/death first-write keys; death read checks exact anchor/version.

- **store/plan_qa.go (1–228)** — Trader+plan chat, proposal apply, owner decline marker and reply-count debounce/cap; raw structured fields not validated in store. Decline count requires caller write coordination.

- **store/planner_read_facts.go (1–193)** — 500-row read telemetry, JSON empty vs absent; horizon numeric validity guarded by ReadHorizons presence. EncodeVoidLevels serialization error becomes [] even for invalid float.

- **store/planner_rejected.go (1–103)** — 200-row global retained rejected verbatim prompts plus validating facts; lazy migration errors ignored, insert error returned, pruning best-effort.

- **store/pnl_surface_guard.go (1–92)** — Registry of strict P&L surfaces and ledger-day total corrected-only; unknown-reason rows filtered before unresolved count; boot zero-raw is test contract, not runtime scan.

- **store/position.go (1–961)** — Position lifecycle/raw/corrected separation, account and plan identities, guarded reconcile close but general quantity/close writers unguarded. Explicit post-create MAE/MFE NULL two writes. See report for dedup and history limits.

- **store/position_excursion_null.go (1–34)** — Startup migration converts both-zero closed MAE/MFE to NULL; assumes pair means uncomputed, with no measurement provenance predicate.

- **store/position_history.go (1–319)** — History summary combines strict aggregates but recent denominator and streak logic defects remain. Exchange closed-history import validates prices/side/times, generates dedup identity, leaves corrected NULL.

- **store/repair_counters.go (1–61)** — Atomic durable repair outcomes key; malformed number coerces0, summary suppresses read errors.

- **store/resolve_source.go (1–80)** — Shared saved/default provenance for RR, HTF veto, plan mode session precedence, one-setup enabled/grade. Daily loss policy not altered.

- **store/session_profile.go (1–105)** — Frozen symbol/session-date profile, check-then-create primary key, recent list and warming count; no contract column.

- **store/store.go (1–739)** — Facade mutex protects lazy pointers; constructors initialize/migrate and defaults; some lazy accessors migrate on first read. NewFromGorm only wraps, NewFromDB lacks GORM; Close does not drain async log writer.

- **store/telegram_config.go (1–164)** — Singleton ID1 per-instance mutex; plaintext bot token stored, String masked only; BindUser replaces binding, caller must authorize. SaveToken clears model via Save(token,empty).

- **store/touch_episode.go (1–109)** — Append-only older touch telemetry, per-day counts and max ordinal seed. Counts omit symbol except MaxTouchNumber; no natural-key uniqueness.

- **store/trade_excursion.go (1–279)** — Unique position path telemetry with nullable measured fields; resolution none marker alone does not erase existing path, close nil correction leaves prior correction. Entry interval inclusive at both ends.

- **store/user.go (1–132)** — Password-hash hidden in JSON; CRUD assumes upstream hash/auth; admin seed empty hash, global delete explicit method; DB errors during EnsureAdmin count ignored.

- **store/watch_assessment.go (1–78)** — Observation store, raw/hysteresis verdict and retrospective outcome/excursion writes. Float0 defaults do not distinguish absent excursion; final outcome first-write predicate.

- **store/zerob_counters.go (1–84)** — Recorded stop-anchor/refusal counts scoped per trader/date/session/class; caller dedup required; count reader collapses errors/malformed into0.

---

<a id="section-22"></a>

> Original: [reviews/17/report.md](reviews/17/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 17 — archived research and replay source understanding

Source pin: `63968be62e44db2fb07a92883e02127b9064b0be`. Isolated read-only worktree: `/tmp/vl-understanding-execution-20260913`. **106/106 assigned files, 8,052 lines, fully manually read; zero unread assigned files.** This slice comprises research/capture scripts and stand-alone Go harnesses, not the production trading service. 278 named functions/methods (including four shell helpers and nine named Python lambdas) are inventoried in `functions.json`; callbacks are covered under enclosing functions. Top-level scripts have file-level contracts in `reads.json` and the annex below.

Evidence: **[A]** directly read source and current source/hash/census inspection; **[B]** implications inferred from that source; no runtime incident, profitability result, live-account state, service health, or execution failure reproduced. Historical row counts below describe scripts' pinned assertions, not a new query of those rows. No assigned script was executed, no trading/network service was called, no live DB/environment was read, and no source/config/account was modified. Artifact-writing Python parsed source only. No financial/external fact claims require web research here.

### Boundaries and evidence precedence

[A] Read AGENTS instruction and CLAUDE canon, plus audit checklist R1–R10 (`AUDIT-CHECKLIST.md:2281–2308`), strict-corrected law (`:522–550`), system-map corrected analytics (`SYSTEM-MAP.md:301–309`) and research archive separation (`:404–425`). Relevant rulebook sections `VL-TRADING-RULEBOOK-v1.md:181–216,290–335` explicitly separate touch, confirmation, fill, historic populations and current policy. Freshness at base:

- AUDIT-CHECKLIST: `dfda15e1 2026-09-13T00:48:28-05:00 test: isolate session clock fixtures from weekly backfill workers`.
- SYSTEM-MAP and RULEBOOK: `565e8fbe 2026-09-13T00:39:02-05:00 fix: use owner daily-loss controls without requiring a per-trade cap`.

These are source-review prerequisites, not permission to rerun captures. Owner correction controls: loss limit is DAILY, no mandatory additional per-trade cap. Historical script references to a missing cap are archived design premises and must not be reinstated.

[A] July10 Understand Anything graph (`7a8adce0`, 3,121 nodes/9,588 edges) contains **zero assigned-path nodes/edges**. `graph.json` records that negative result. Current root Go AST inventory supplied function boundaries and syntax-only calls; Python AST and shell declarations supplied the rest. This does not make the historical CGC/UA index current. No indexing mutation was performed; no claim about type-resolved callers is made.

### End-to-end data and ownership flow

1. **Capture/census:** dated scripts read SQLite tables or previously exported CSV/JSON/logs. Completed audits generally open query-only snapshots and export row membership. Older shell invocations can open the configured source directly, one query at a time. Capture scripts may intentionally fetch HTTP and authentication context; they are not an offline-safe validation suite.
2. **Cohort construction:** strict `pnl_corrected`, resolved plan identity and test exclusions feed the completed58-trade/12-CME-day cohort. Earlier65-trade arithmetic, later65-trade strategy census and scenario/plan counts are distinct populations. They cannot be concatenated or presented as a single fresh result.
3. **Attribution:** plan/version/scenario keys, signal IDs and broker order rows provide the strongest available joins. Nearest-time/side/price heuristics and current mutable stop snapshots are weaker historical proxies. Repeated rejections, plan versions, touch episodes and actual positions have different denominators.
4. **Research transformation:** Monte Carlo, Wilson/mean intervals, excursion/ATR summaries, refusal replay, identity/style census and geometry studies generate artifacts. Complete replays distinguish necessary conditions from broker fills. Newer Go harnesses load concrete-contract/source bars, construct closed-prefix detector reads, then simulate touch policies independently over zones and sessions.
5. **Outputs:** local CSV/JSON/Markdown are report artifacts. Imports into `vl/kernel` and the frozen geometry core reuse pure logic; these do not route orders or reproduce live receipt/account/queue constraints. Production `ComposeLevelFadeGeometry` resolves scenario identity; research `ComposeFrozenLevelFadeGeometry` is given a frozen zone index (`trader/structural_geometry.go:106–125`). Daily gates, one-position conflicts, resting-order cancellation, partial fills and broker receipts are outside the simulated fill model.

### Important research limitations and correction history

#### Current reproducibility and production-contract separation

[A] Old zone harness `research/2026-09-12-backtest-zone-fade/harness/main.go:34` calls `verifyPort`; `stop_port.go:129–153` compares the old embedded `composeArmStop` signature/body with current source. Current `trader/arm_stop_anchor.go:76–88` adds structural context and forwards reject fades to `ComposeLevelFadeGeometry`. Thus the old benchmark guard detects drift at this base. **[B] It should refuse startup, not silently benchmark the old stop as current production.** No command was run to demonstrate its exit status. The guard itself compares the embedded string with production, not the actual local callable function text (`stop_port.go:45–124` versus `:158–237`); edits to only the callable copy could escape that particular guard. This is a static future-maintenance limitation, not observed divergence between those two old copies.

[A] `reports/2026-09-12-structural-stop/harness/sweep.go:84–100` directly calls the exported frozen-zone production geometry core. Its comments/branch at `:93–98` still discuss missing per-trade-cap permission. Rulebook `:195` and current owner correction supersede that premise. Do not treat clearing that historical diagnostic label as actual runtime risk authorization or restore the cap.

#### Fill, cost, target and drawdown assumptions

[A] Original zone `main.go:259–265` sets touch-A filled at the anchor whenever the band is touched and assigns short C entry `anchor+tick`, long C `anchor-tick`; `evalLine:360–366` repeats the C rule. **[B] Band intersection does not establish an anchor fill; that tick direction is favorable, so it is not adverse slippage.** Original zone `eval.go:209,228,251` skips the fill bar, uses exact stop prices even across gaps and deducts fixed two-point friction. Structural-fade `eval.go:221,240,321` retains these original-model simplifications. Gross R uses gross outcome while net expectancy uses friction-adjusted points; they are different metrics, not inconsistent accounting by themselves.

[A] Later corrected structural sweep `sweep.go:123–202` includes fill-bar ambiguity, uses adverse long+tick/short−tick entry, handles stop gaps and target-through conditions. These are meaningful archived improvements; old optimistic model assumptions must not be re-reported as newly discovered production behavior. Even the corrected model is an OHLC scenario exercise, not queue/partial-fill/receipt-latency proof. Fixed friction and `$2/point`/tick assumptions apply to the declared MNQ one-contract research context; historical gaps can exceed that friction, and a collection of independently simulated touches is not one account equity path.

[A] `zone-fade/harness/out.go:111–117` builds cumulative values starting **after** trade1; its actual same-directory `stats.go:70–85` initializes peak at `cum[0]`. **[B] Initial losses are omitted from maximum drawdown (e.g. one losing trade yields zero), so this emitted research metric understates zero-capital-baseline drawdown until a higher peak establishes.** The same helper is in assigned structure-fade `stats.go:70`. This algebraic counterexample was reasoned from source, not run as a test. `out.go:108–110` leaves profit factor0 when lossSum0; that sentinel is not a finite observed no-loss PF. `longestLosingStreak` counts flats as losses, whereas old MC `max_streak` counts negative values only; compare definitions before comparing headline streaks.

#### Causality, sample identity and uncertainty

[A] Old `vet-02-levels-data/q12_grade_vs_outcome.py:12–16` and `q12b_grade_vs_outcome_dedup.py:9–12` fall back to the whole current day when prior history is insufficient. **[B] A prospective feature interpretation would leak later tape.** Day/label/price first-seat dedup does not restore omitted contract/trader identity or statistical independence. `q10_live_dedup_wilson.py:12` and some recuts collapse contradictory outcomes lexically; completed vet01 `audit.py:144–151` explicitly preserves conflicts as ambiguous and reconstructs ordinals. These are successive research contracts, not interchangeable estimates.

[A] Old execution `q14_mae_mfe.py:29` selects5m bars with open before entry; completed `complete/q31_verified.py:39` uses close<=entry. Old `q16_funnel.py:33` invents a2minute life when stored duration is zero; completed `q31_verified.py:83–90` explicitly removes that invention. `r07_pnl_pop_SUPERSEDED.py:1` already identifies its wrong-year/latest-version defects. Preserve this correction history instead of describing the earliest drafts as current measurements.

[A] Completed `vet-01-way-it-trades-complete-data/audit.py:74–86` pins58 eligible rows and12days; `:89–104` joins signal→filled entry→Accepted stop within10seconds, improving on nearest-arm searches. Its closed-minute RV uses61 consecutive bars (`:113–117`), but cohort terciles use the whole cohort (`:118`): valid descriptive buckets, not independently trained live cutoffs. The cluster bootstrap (`:138–142`) resamples12 observed days; it does not create more independent days. Completed risk `complete/recompute.py:59–63,119–120` pins the same cohort, states cost sensitivity and omits absent days rather than fabricating zero days.

[A] Research profile lookup `structural-stop/harness/detect.go:125` / zone `detect.go:125` selects profile information by session key. **[B] Historical observation/receipt availability and concrete-contract lineage cannot be concluded from a key alone.** No actual profile rows were inspected here, so lookahead in that lookup is UNVERIFIED, not established. `structure-fade/data.go:65–111,133–150` improves contract/source selection and closes<=cutoff; it does not independently prove no duplicate same-time rows or sufficiently fresh last closed bar. Detector allfresh metadata is a modeling assumption, not a historical receipt claim.

[A] `zone-fade/main.go:28,141–147` fixes a heldout era; `main.go:405–474` emits parameter surfaces. Sample-specific Wilson thresholds are not a policy-validation guarantee. Independent-proportion intervals (`structure-fade/stats.go:26`, used in zone split logic `main.go:520–565`) do not account for paired/shared-session outcomes. Multiple zone touches share tape, and parameter selection/holdout reuse require experimental provenance. Surface dependency excerpt `zone-fade/surface.go:1–110` declares in-sample tuning and81-cell surfaces, but the full surface implementation is outside this assignment and not re-reviewed here.

#### Execution safety of archived tools

[A] `vet-07-prompts-complete-data/capture.py:45–61` performs HTTP and, on401, reads `.env` and mints a token using a database user; `vet-09-top-ten-data/q01_verify.py:15` calls health; style `validate.py:26–27` reads live account/environment context for privacy scanning. They must not be casually executed as offline tests. No secret values were read or included by this review. `vet-05-execution-data/revise/apply_edits2.py:3–30` is a report mutator and can write after replacement-count failures; it is not a read-only verifier.

### Relevant validation and unresolved branches

[A] Assigned validators were fully read: risk `complete/validate.py`, vet09 `final_validate.py`, and style `validate.py`. They validate stored populations/arithmetic/artifact coverage, not full current production behavior; some read live context. None was executed. Port drift guard is a useful source consistency check with the limitation above. Seam report `structure-fade/seam.go:36–134` has unchecked SQL/JSON errors and reported booleans; zone `main.go:52` collects results without turning each false diagnostic into a fatal failure. Static syntax census and SHA256 recheck completed, covering all assigned files. No broad suites or broker tests were appropriate for this read-only archive assignment.

Unresolved: exact historical DB rows/current receipt timing, original report artifact hashes against original execution environments, external absolute-path helpers used by old scripts, current live account/day switches, actual broker outcomes, and a full type-resolved graph are not established by this slice. A claim of a new production incident or validated profitable strategy would exceed the evidence.

### Complete per-file coverage and purpose

Paths below are relative to repository root; all are full/manual reads. Function boundaries, syntax-only call expressions and per-file risk context are in `functions.json`; exact SHA256 and read ranges are in `reads.json`.

000. `docs/superpowers/reports/2026-09-03-mc-drawdown-data/mc_drawdown.py:1–177` — Seeded iid/block/day bootstrap of historical corrected trade sample; zero-baseline maxDD; policy examples and friction-free historical calibration only.

001. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-complete-data/audit.py:1–201` — Completed vet01 snapshot audit: exact plan/version and signal/order joins; excludes test/sentinel/NULL; pins58 trades/12days; day-cluster resampling; MFE proxies explicitly unordered.

002. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-complete-data/binding.py:1–17` — Recursively visits saved configuration with allowlisted fields; saved settings are not proof of runtime binding.

003. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-complete-data/legacy_check.py:1–22` — Recomputes archived MFE-floor arithmetic and labels limitations; not an ordered fill simulator.

004. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-data/q13_enrich.py:1–91` — Enriches corrected trades with approximate nearby arms/decisions/stop snapshots; fixed UTC-5; identity not authoritative.

005. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-data/revise/r04_close.py:1–36` — Approximate close-decision ±4minute joins and exit reason extraction; historical heuristics.

006. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-data/revise/r05_mfe.py:1–55` — MFE reach and ATR ratios from external helper; winners/threshold reach not ordered trade outcomes.

007. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-data/revise/r12_tables.py:1–72` — Revised tables exclude named row IDs; whole-sample descriptive buckets, no prospective validation.

008. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/q10_live_dedup_wilson.py:1–29` — Deduplicates touches and Wilson intervals; MIN(outcome) can collapse contradictory outcomes lexically.

009. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/q12_grade_vs_outcome.py:1–88` — Grade/outcome exploration; delta fallback uses current entire day on insufficient prior days; external replay dependency.

010. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/q12b_grade_vs_outcome_dedup.py:1–69` — First-seat day/label/price dedup reduces repetition; retains current-day delta fallback and no trader/contract identity.

011. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/revise/r03_dedup.py:1–64` — Additional dedup and subgroup exploration; repeated related events and multiple comparisons remain.

012. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/revise/r04_q12b.py:1–29` — Touch/grade bucket tabulation with Wilson uncertainty; observation is not broker execution.

013. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/revise/r07_pnl_pop_SUPERSEDED.py:1–50` — Explicitly SUPERSEDED wrong-year/latest-version population analysis; archived correction trail, not current bug.

014. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/complete/supplement.py:1–82` — Supplement reads snapshot and exports diagnostics; forward minute counterfactuals censor missing tape and ambiguous chronology.

015. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/q01_store_premises.sh:1–24` — Shell schema/population premises; each sqlite query separate, not one atomic snapshot.

016. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/q06_plan_corpus.py:1–99` — Plan corpus/latest version and price-touch FIRED proxies; fixed UTC-5 and no confirmation/fill guarantee.

017. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/q07_reauthor_cost.py:1–52` — Clusters reauthor attempts over20minutes; next publication may fail closed and is not acceptance latency.

018. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/q11_wilson_pnl_2r.py:1–75` — Historical Wilson/P&L/2R summaries; nearest ±3minute stop lookup lacks exact broker identity.

019. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/q12_refusals_by_leg.py:1–25` — Refusal-by-leg event counts; repeated rows are not deduplicated opportunities.

020. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/revise/r01_rejects_arms_intents.py:1–66` — Historical reject/arm/intent ledger classification and exact exported row IDs.

021. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/revise/r11_store_rechecks.py:1–200` — Store rechecks and author geometry; MFE target-first counterfactual ignores ordered path; nearest stop association.

022. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/revise/r16_minsl_repairs_rr_wilson.py:1–60` — Minimum-stop repairs/RR with nearby cycle rows; matching lacks exact trader/side identity at some joins.

023. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/revise/r18_flap_overlap.py:1–39` — Overlap from arm created/updated lifetime and short-name grouping; timestamps are not broker lifetime.

024. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/revise/x02_refusals.py:1–45` — Raw refusal totals and hardcoded reconciliation values; event and opportunity denominators differ.

025. `docs/superpowers/reports/2026-09-05-vet-04-monitoring-data/q01_store_asof.sh:1–8` — Shell as-of schema census; live path literals are archived instructions, never executed by this review.

026. `docs/superpowers/reports/2026-09-05-vet-04-monitoring-data/q50_eod_verified.py:1–53` — EOD diagnostic marks future-updated arm state UNKNOWN; decision silence and read availability do not prove flatness.

027. `docs/superpowers/reports/2026-09-05-vet-04-monitoring-data/r07_rth_vs_atr.py:1–42` — RTH counts permit389/390 as full; simple true-range averages differ from Wilder ATR.

028. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/complete/q31_verified.py:1–94` — Completed execution audit corrects closed5m availability and zero-lifetime invention; still distinguishes proxy stop joins from broker facts.

029. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/complete/q33_metrics.py:1–15` — Hardcoded historical rate/bar-bucket display; not a fresh query.

030. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/complete/q34_integration.py:1–17` — Completed integration detail reconstructs historical snapshots and literal broker stop; no current runtime assertion.

031. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/q14_mae_mfe.py:1–74` — Older excursion audit includes5m open before entry rather than close before entry; superseded by completed q31.

032. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/q15b_cycle_drift.py:1–32` — Cycle drift pairs nearby decision and bar close at open<=cycle; can use incomplete-minute close.

033. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/q16_funnel.py:1–69` — Old funnel lacks version identity and invents2minute life for zero duration; corrected q31 explicitly avoids this.

034. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/q19_snapshot_stop_price.py:1–14` — Snapshot stop-price inventory truncates names to8characters; grouping may alias identifiers.

035. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/q20_plan_scenarios.py:1–12` — Plan-scenario lookup uses ID prefix for exploration, not canonical full identity.

036. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/q27_guard_cancel_paths.py:1–27` — Cancel-path nearby windows and close at cancel minute; not ordered broker fill evidence.

037. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/q28_guard_counterfactual.py:1–40` — Guard counterfactual skips fillbar, uses exact execution prices and no friction; approximate research only.

038. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/q34_integration.py:1–16` — Integration diagnostics duplicate completed variant; runtime bindings not verified.

039. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/revise/apply_edits2.py:1–31` — Historical markdown replacement utility writes despite failed replacement counts; never run as an offline validator.

040. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/revise/r02_legs_floor.py:1–77` — Revised floor/legs tables compare historical populations and excursion proxies, not causal policy improvements.

041. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/complete/recompute.py:1–123` — Completed risk recomputation snapshot pins58/12, exact IDs and strict corrected values; seeded day bootstrap and cost sensitivity.

042. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/complete/validate.py:1–36` — Committed-fixture risk validator checks exact58IDs/drawdown/streak and deterministic outputs; not run here.

043. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q01_census.sh:1–19` — Initial risk census has documented epoch premise error; archived supersession.

044. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q02_premise_227.sh:1–23` — Premise shell computes timestamps but queries conflicting literals; do not reuse as current cohort.

045. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q04_build_sample.py:1–30` — Initial sample excludes correction notes rather than unresolved plan sentinel; compliant recut supersedes it.

046. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q08_bars_regime.py:1–50` — Bar regime/true-range summaries lack explicit percontract continuity; retrospective descriptors.

047. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q11_daily_regime.py:1–49` — Daily simple ATR/range classification is retrospective whole-day context, not a causal feature at entry.

048. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q13_calendar.sh:1–34` — Shell discovers calendar tables; no live news-service validation.

049. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q14_weekly_intraday.py:1–37` — Realized day/week summaries for historical corrected rows; not forward risk bounds.

050. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q15_filled_arms_rate.sh:1–14` — Filled-arm shell ratios use historical literal cutoff; no present fill-rate claim.

051. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q17_gonogo_math.py:1–33` — Hardcoded65-row go/no-go arithmetic and policy examples; no current permission or recommendation.

052. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/r01_population.sh:1–17` — Population reconciliation shell enumerates exclusions and counts.

053. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/r02_build_sample_compliant.py:1–38` — Compliant sample fixes unresolved plan sentinel and exports member IDs; field created_ms actually holds entry time.

054. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/r04_limits_buckets.py:1–106` — Limits/buckets and approximate mean intervals; posthoc day direction uses full-day close only as retrospective stratification.

055. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/r05_machine_counterfactual.py:1–51` — Machine counterfactual says account-scoped but SQL lacks account restriction and groups only by day; multiaccount limitation.

056. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/r08_rr_matched.py:1–25` — Matched entry signal improves identity but mutable ledger stop/assumed one lot do not freeze initial broker risk.

057. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/r09_killswitch.py:1–42` — Kill-switch bootstrap on historical daily sequence; synthetic thresholds and tail estimates are not guarantees.

058. `docs/superpowers/reports/2026-09-05-vet-07-prompts-complete-data/capture.py:1–63` — Capture utility reads snapshots and HTTP endpoints;401 branch reads .env and mintsJWT; not offline safe, never executed.

059. `docs/superpowers/reports/2026-09-05-vet-07-prompts-data/q12_classify.py:1–55` — Priority regex prompt-refusal classifier counts rows, residualOTHER retained; not semantic complete attribution.

060. `docs/superpowers/reports/2026-09-05-vet-07-prompts-data/q14_tokens.py:1–59` — Tokenizer estimates use o200k/cl100k proxy encodings, not DeepSeek bill; runtime may retrieve encoding assets.

061. `docs/superpowers/reports/2026-09-05-vet-08-stretch-data/common.py:1–39` — Shared CT conversion,1m loads,5m aggregation and Wilder ATR; aggregation permits partial buckets.

062. `docs/superpowers/reports/2026-09-05-vet-08-stretch-data/complete-0905/analyze.py:1–56` — Completed replay analyzer preserves plan/version/scenario identities and unknown opportunities; no fabricated realized profits.

063. `docs/superpowers/reports/2026-09-05-vet-08-stretch-data/complete-0905/bounds.py:1–35` — Candidate fill bounds0..1 measure opportunity uncertainty, not queue/cancel/portfolio fill probability.

064. `docs/superpowers/reports/2026-09-05-vet-08-stretch-data/complete-0905/replay.go:1–161` — Go necessary-condition replay calls production kernel validators/confirm evaluators on closed prefixes; copied historical stop composition, not broker simulation.

065. `docs/superpowers/reports/2026-09-05-vet-08-stretch-data/q01_store_asof.sh:1–10` — Replay schema/as-of shell; no replay execution here.

066. `docs/superpowers/reports/2026-09-05-vet-08-stretch-data/q25_payoff.py:1–47` — Historical payoff stats use host-local naive era and nearest-stop association; no sentinel filter in this exploratory version.

067. `docs/superpowers/reports/2026-09-05-vet-08-stretch-data/q30_atr.py:1–29` — ATR enrichment uses host-local era and partial aggregation; older context helper.

068. `docs/superpowers/reports/2026-09-05-vet-08-stretch-data/replay.py:1–273` — Original Python replay advances120seconds but checks last1m only, uses mutable live stops, unsupported confirms false, no fees, open-end0pts; superseded qualification matters.

069. `docs/superpowers/reports/2026-09-05-vet-09-complete-data/final_validate.py:1–41` — Final archive validator checks six exports same58IDs, coverage/links/privacy patterns; not production parity.

070. `docs/superpowers/reports/2026-09-05-vet-09-top-ten-data/q01_verify.py:1–46` — Top-ten verification calls HTTPhealth and original cohort filter; not offline safe and not executed.

071. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/complete/recompute.py:1–77` — Completed idea recut pins58/12 and exports denominators, bootstrap/cost assumptions.

072. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/q03_entry_time_units.sh:1–14` — Shell compares timestamp units/entry observations; historical literal population.

073. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/q05_trades_deep.sh:1–18` — Shell deep-trade diagnostic queries historical rows; not an execution proof.

074. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/q06_planner_and_decisions.sh:1–22` — Shell planner/decision cadence uses LIKE and labels; multiple actions/empty labels can collapse.

075. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/q08_churn_silence.sh:1–20` — Shell churn/silence diagnostics count recorded events, not opportunities or actual broker cancels.

076. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/q12_exit_reasons.py:1–32` — Exit reasons inferred from nearby timestamp/price without side; native broker exit cause is separate.

077. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/r01_pop.sh:1–12` — Ideas population shell reconciles corrected exclusions.

078. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/r02_recut.py:1–66` — Ideas recut/mean intervals enumerate eligible historical groups; posthoc subgroups not validated edge.

079. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/r03_touch.sh:1–28` — Touch proxy joins and grouped diagnostics; no canonical immutable opportunity guarantee.

080. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/r04_more.sh:1–21` — Additional touch/snapshot diagnostic shell; interpretation remains descriptive.

081. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/r09_dedup.py:1–53` — Dedup uses lexical first outcome and narrow VWAP dynamic family; different dedup contracts yield different estimands.

082. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/wilson.py:1–10` — Wilson and approximate mean confidence interval helpers for static research.

083. `docs/superpowers/reports/2026-09-08-scenario-economics-data/measure.py:1–58` — Decimal frozen scenario geometry from pinned git cohort6095ca58 and DB snapshot; signed first target vs absolute arm R, no trade profitability claim.

084. `docs/superpowers/reports/2026-09-08-the-strategy-data/analyze.py:1–175` — Strategy style census preserves version/slot identities and ambiguous hold; publication close proxy and lifecycle cutoff; sample differs from Sep5 cohort.

085. `docs/superpowers/reports/2026-09-08-the-strategy-data/build_report.py:1–151` — Report generator mixes computed tables and hardcoded historical prose/runtime revision; not a self-updating current-system report.

086. `docs/superpowers/reports/2026-09-08-the-strategy-data/validate.py:1–34` — Style validator AST/fixture checks plus liveDBaccounts and.env privacy scans; not wholly offline despite header.

087. `docs/superpowers/reports/2026-09-10-scenario-level-identity-data/census.py:1–136` — Two read-only bounded DB snapshots with explicit non-atomic caveat; stage/schema and formation census.

088. `docs/superpowers/reports/2026-09-11-level-zones-evidence/overlap_replay.py:1–46` — Exploratory union-find overlap uses native overlap or anchor distance; not exact production non-transitive zone merge.

089. `docs/superpowers/reports/2026-09-11-one-setup-data/sectionC.py:1–118` — One-setup diagnostic uses host timezone, strategy_id=trader premise, fallback condition without version and nearby bar close; unresolved identity/causality.

090. `docs/superpowers/reports/2026-09-12-structural-stop/harness/audit_ledger.py:1–27` — Frozen200positive-price ledger geometry and plan inventory; copied database, not current account truth.

091. `docs/superpowers/reports/2026-09-12-structural-stop/harness/detect.go:1–146` — Closed-prefix level detection, production map helpers, allfresh/owner omissions; session profile availability/contract provenance not established by key alone.

092. `docs/superpowers/reports/2026-09-12-structural-stop/harness/summarize_overshoot.py:1–37` — Held-conditioned overshoot quantiles exclude breakouts; touchbar extremes upper bounds, training buffer not validated profitability.

093. `docs/superpowers/reports/2026-09-12-structural-stop/harness/sweep.go:1–209` — Corrected geometry sweep calls production frozen-zone core; fillbar included, adverse tick, stopgap and target-through handling; old missing per-trade-cap diagnostic superseded by daily-only owner rule.

094. `docs/superpowers/research/2026-09-12-backtest-structure-fade/harness/data.go:1–193` — Read-only DB opening and percontract/source bars; closed-prefix selectors and session windows; no explicit last-bar freshness or duplicate-time rejection.

095. `docs/superpowers/research/2026-09-12-backtest-structure-fade/harness/eval.go:1–334` — Band/line outcomes and trade model skipfillbar/exactstop;2pt friction, grossR; structural buffer grid and nearest-qualified target are research policy.

096. `docs/superpowers/research/2026-09-12-backtest-structure-fade/harness/extract.py:1–34` — JSON extraction/rendering from prior run outputs; not recomputation.

097. `docs/superpowers/research/2026-09-12-backtest-structure-fade/harness/seam.go:1–134` — Seam report counts and flags, ignores SQL/JSON errors; flags not asserted by caller; same-contract grouping check is not callsite parity.

098. `docs/superpowers/research/2026-09-12-backtest-structure-fade/harness/stats.go:1–154` — Wilson/independent difference/quantiles/RNG/streak helpers; maxDD starts at first cumulative observation; empty values often0.

099. `docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/detect.go:1–146` — Same closed-prefix detector as structural-stop harness; allfresh inputs do not reproduce historical observation receipt.

100. `docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/eval.go:1–264` — Original zone trade model skipsfillbar and exactstopgap;2point friction; touch and through variants remain hypothetical executions.

101. `docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/extract.py:1–68` — Loads saved run and C4 rendering; output describes archived simulation only.

102. `docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/main.go:1–653` — Zone main orchestrates sessions/maps/holdout and surfaces; bandtouchA alwaysfilled at anchor, C tick favorable; independent overlapping trades; startupport guard rejects changed production source.

103. `docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/out.go:1–277` — C1/C4 grouping and rendering: net points*$2, grossR, PF0 for no losses; cumulative series omits initial0 so early drawdown missed.

104. `docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/render_c1.py:1–19` — Markdown table renderer with n<30 decided flag; consume saved JSON, no computation of sample validity.

105. `docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/stop_port.go:1–237` — Legacy stop widest-wins port and startup byteguard; current production variadic structural signature differs so replay aborts; guard compares embedded string, not callable copiedbody.

---

<a id="section-23"></a>

> Original: [reviews/18/report.md](reviews/18/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 18 — research and report-source review

[A] All 106 assigned files (8,052 source lines) were manually read in full at isolated worktree `/tmp/vl-understanding-surfaces-20260913`, base `63968be62e44db2fb07a92883e02127b9064b0be`. `reads.json` records each full range and SHA-256; unread count is zero. `functions.json` records 252 named functions, including embedded shell/Python helpers; inline lambdas and callbacks are included in their enclosing function/file notes. This is source understanding of this slice, not a claim of whole-repository, runtime, or dataset validation.

[A] No reviewed research script was executed, no live DB/API/environment was read, no production/source edit was made, no orders/settings/accounts were changed, and no tests or simulations were run. Small review-only Python helpers parsed source and wrote these `/tmp` artifacts. The main instruction file was read for policy only. Initial isolated worktree status was clean. Statistical/data values appearing below are the values asserted or discussed by source, not newly measured sample results.

### Authority and freshness

[A] Read the canon and the audit checklist, including R1 fresh evidence, R2 independent arithmetic, R3 long/short symmetry, R4 exact source locations, R7 corrected PnL/NULL discipline, and isolation/publication requirements. Relevant SYSTEM-MAP PnL and RULEBOOK structural geometry/daily-loss sections were read as excerpts. For both relevant specification files, `git log -1` was `565e8fbe Sun Sep 13 00:39:02 2026 -0500 fix: use owner daily-loss controls without requiring a per-trade cap`. That owner correction supersedes old report proposals. A historical $150 cap, ATR ceiling, fixed-point cap, or 'risk_cap_missing' interpretation is not current authorization or policy. SIM-only and sacred account/config bindings remain controlling.

[A] Historical Understand Anything graph was parsed at `/home/hoang/vl-untracked-stash-20260816/.understand-anything/knowledge-graph.json` (July10@7a8adce0). Zero nodes matched the 106 assigned paths, which are September artifacts. There are consequently no relevant historical graph edges to validate. `graph.json` preserves this negative result. No claim of current CGC coverage is made; root owns CGC export. Source takes precedence over those historical indexes.

### What this slice does and how it connects

[A] These files are evidence collection, analysis, report rendering, and offline research harnesses. They are not the NT8→BarCache→kernel→risk→SIM order runtime. The September5 families gather historical SQLite row IDs, selected logs, authored plan versions, decisions and snapshot witnesses; compute descriptive and bootstrap statistics; then render frozen reports. The `complete/`, `complete-data/`, and revision scripts frequently correct or explicitly retire earlier adjacent scripts. Reading the whole lineage matters: an older 65-row risk estimate is not the revised 58-row corrected sample, and neither is a current performance measurement.

[A] Typical historical flow is `trader_positions/plans/armed_orders/decision_records/bars` through read-only SQLite snapshots into CSV/JSON, then report summaries and literal prose. Most scripts use hardcoded historical worktree and production DB paths; some open a single query-only transaction, others use independent SQLite calls. A pathname beginning `file:...mode=ro` protects DB writes but does not make the collected multi-source output an atomic runtime snapshot. Reports may combine DB transactions, later log reads, source excerpts, API GETs and fixed text from distinct moments.

[A] The September12 path instead opens an explicit copied SQLite database, splits bars by contract, freezes detector inputs at each scheduled session read, constructs levels/zones, observes first touch and subsequent horizon outcomes, and writes event/geometry artifacts. `data.go:50–196` supplies read-only access, timeframe construction, contract selection, closed-bar slicing and Chicago session windows. `detect.go:62–146` supplies closed-window detector inputs and nPOCs. Structure-fade `main.go:92–292` dispatches reads and evaluates zone events; `out.go:13–233` formats results; surface files estimate families of alternatives. Current structural-stop `main.go:32` generates penetration events or dispatches its sweep; `summarize.py:22–112` summarizes complete registered geometry/fill cells. Important unassigned current sweep/production implementations were not fully reviewed here; root's other assignments own them.

[A] September8 runtime extractor is a sensitive operational reader: `read_runtime.py` reads `.env` and `/proc` environment, mints a short-lived JWT, and GETs local endpoints. It was source-read only. September10 `mutation_driver.py` actually changes source, builds/tests and restores in `finally`; its presence under reports does not make execution read-only. It was not run. `basis_receipts.py` provides pinned SHA/public-byte checks and does not establish fresh runtime state.

### Findings affecting interpretation of currently referenced research

#### 1. nPOC historical availability needs an as-of proof

[A] `research/2026-09-12-backtest-structure-fade/harness/detect.go:125–146`, duplicated in the current structural-stop harness dependency, selects stored profile rows with `session_date <= readKey`. It does not require a profile completion timestamp or contract identity. Errors may become an empty list. Current production dependency `trader/auto_trader_dayplan.go:192–228` was read only to understand its stored-profile/touch boundary; no whole-function parity claim follows.

[B] A final same-day stored profile can therefore be available in an offline query before it could have been known at that session read. Contract mismatch is a separate possible provenance issue. This is a source-supported research leakage concern, not a reproduced incident: no live/backtest data was queried, no affected event IDs or count are known, and no claim is made that this changes the training-derived default. The remedy to interpreting the evidence is an immutable completion/contract as-of audit, not an assumed change to production.

#### 2. Overshoot calibration is a conditional geometry measure

[A] Current structural-stop events freeze the read-side map and examine future bars as offline outcomes. The complete touch bar's high/low can precede the exact touch; penetration is explicitly an upper-bound observation. The separately read `summarize_overshoot.py` dependency computes C5 held-outcome conditional overshoot percentiles on training data, retains event IDs and checks the original artifact. It does not estimate unconditional loss, risk-of-ruin, or the profitability of applying that stop to every opportunity. H12 classification starts from later closed bars; the touch close is not independent future evidence.

[B] A provisional 4.50-point p95 value derived from the held cohort can be useful as that cohort's descriptive clearance, but cannot establish total loss protection or a profitable trading policy. Large break excursions are not represented by conditioning on holds. Minute OHLC does not recover intrabar touch ordering. No recommendation to reintroduce a mandatory per-trade cap follows.

#### 3. Current summary admits prior exposure; stale cap labels remain

[A] Structural-stop `summarize.py:1–6,61–90` openly says the former held-out year has already been exposed. It uses common circular five-day block draws across cells, Bonferroni family-nine bounds and a finite-bootstrap correction. This is materially different from the older independent sign-flip surfaces below; do not transfer their exact implementation flaws to this file.

[A] `summarize.py:35` still counts `ConfiguredAdmission == 'risk_cap_missing'`, and line89 emits 'No monetary cap supplied'. Those labels reflect superseded policy at this base, not the owner's September13 requirement. They should be annotated when citing the artifact; the review did not edit them. `occupancy:38–56` correctly starts equity at zero and reserves an unfilled opportunity's touch minute, but expressly says this is not the live setup selector or attainable live equity curve. Insufficient/empty input groups can still fail in top-level resampling/printing; source assumes a populated frozen dataset.

#### 4. Changed-stop composition receipts are selected evidence

[A] `audit_compositions.py` reads copied log lines for changed stops, associates the latest preceding plan and matching authored-stop price, and tolerates one second. The included records are not the denominator of all compositions. [B] Matching price/time is insufficient to prove immutable arm identity where versions or arms share geometry. Neither incidence rates nor accepted broker initial risk can be inferred without the missing denominator/identity. No affected live order is asserted here.

### Archived exploratory limitations — not current trading bugs

[A] Old structure-fade `main.go:170–292` marks FillA/FillC on zone overlap even if the entry anchor is not touched, and its minimum-R target selection can skip a closer opposing zone. It evaluates archived alternative geometry. Current production geometry is different; these are reasons not to cite that old fill surface as attainable execution. `stop_port.go:129` compares a raw embedded legacy function string to production source, rather than comparing the executable copied function directly. The present production signature differs, so source inspection predicts refusal on rerun; this was not executed.

[A] Structure-fade `surface.go:51–138` independently sign-flips flattened cell observations, uses fixed estimated standard errors and lacks the finite-permutation +1 correction. Zone-fade `surface.go:67–296` similarly breaks cross-cell or cross-horizon shared-observation structure in its null simulations. [B] Their maxT labels do not by themselves establish valid familywise inference for correlated event alternatives. Additional multiplicative Bonferroni handling is not a proof that the underlying null construction is right. These are archived statistical limitations, not the current common-day-bootstrap implementation.

[A] Zone-fade `stats.go:70–86` starts peak equity at the first cumulative return, so an initial loss is omitted from maximum drawdown from zero. `longestLosingStreak:88` counts nonpositive, including flats, despite its name. `seam.go:36` records boolean checks without necessarily aborting; some errors are ignored and its same-contract assertion largely verifies its own grouped construction. This is not independent validation of source contract labels. Current structural-stop occupancy avoids the initial-zero drawdown defect.

[A] September5 original `vet-02/.../q11_replay.py:136–142,193–196` uses final-day extrema to choose a round-number/null price universe despite causal wording; its volume-profile approximation spreads volume over bar ranges, differing from kernel close-bin behavior. Revised complete analysis explicitly quarantines unknown formation times and requires complete causal windows. Even revised readers have edge cases such as no-predecessor binary search becoming negative indexing and empty trail divisions. These are static conditions, not reproduced sample corruptions.

[A] Original execution `q15_decision_slip.py:24` has `and/or` precedence allowing an `entry_price` field to bypass the action test; loose nearest-time matching lacks complete trader/side/symbol identity. Original `q31_verified.py` misses the sentinel plan ID exclusion; revision `apply_edits.py` explicitly corrects it and `r01_compliant.py` compares the populations. Original ideas `q07_canonical_set.py:14–39` overwrites arms by plan/scenario, uses mutable stops, allows a nearest decision one minute after entry, and divides corrected cash by a fixed $2 without position quantity. Resulting R, full-stop and MFE/target figures are proxies, not immutable accepted risk. `q10_targets_mfe.py:25–34` calls its counterfactual an upper bound; intrabar order and actual limit fills remain unknown.

[A] Risk `legacy_mc_drawdown.py` combines a no-win Bernoulli recursion with empirical paths where flats exist; complete report explicitly retires that mismatch. Revised risk scripts sample historical trade/day blocks and include zero-baseline drawdown, but do not create new market regimes. `q10_sessions_conditions.py:11` labels a mean interval as t while using 1.96. Fixed UTC-5 is common and only suitable for the pinned summer period. Various summaries use mutable ledger state or zeros as proof of original excursions; later complete reports distinguish those limitations. Historical per-trade caps or live-pilot prose are proposals only and superseded by current daily-loss/SIM instructions.

[A] Forming-candle `analysis.py` explores associations using nearest same-day price/time pairing, not immutable formation identity. Post-open/closed-episode features can overlap the outcome; five-minute queries lack contract/source and continuity guarantees; alternative horizons pre-exclude originally ambiguous rows. Source's duration and mechanical checks help interpretation but do not establish a prospective edge. Literal final result counts are frozen historical assertions.

### Report integrity, tests and non-findings

[A] Prompt complete `measure.py:4–6` is a two-site textual replay, not a fresh provider render. Its manual line categorization is exhaustive byte provenance, not objective truth of every 'fact'. Same-tokenizer comparisons and whole/group BPE caveats are soundly disclosed. `validate_artifacts.py` reconstructs spans and verifies pinned totals/maps; 120 mapped units does not prove behavioral equivalence. Exact proposed policy cuts are explicitly never applied. These are useful artifact checks, not an LLM/validator parity test. Complete scripts explicitly retire earlier prompt evidence as primary.

[A] Stretch complete `verify.py:13–23` checks closed-minute checkpoints, copied source containment, lineage census and broker text witnesses; it explicitly declines complete current-rule broker replay, attainable PnL, immutable initial risk and post-strict profitability. Reaper `reaper.py:9–16` replays an observed cancellation time using the preceding snapshot; it states interval/cache assumptions. It does not synthesize missing broker inventory. Empty output and missing account scope are static limitations.

[A] Renderers such as risk `complete/build_report.py`, prompt `finalize.py`, and execution `revise/apply_edits.py` combine hardcoded prose with generated values. The execution patcher writes even if an exact-once replacement failed. Stretch `extract.py:10–15` returns on an empty CSV without clearing an old file. Ideas `complete/source_evidence.py:6` prints a fixed commit while reading worktree files, without proving HEAD matches. [B] Blindly rerunning these in a new tree/snapshot can yield misleading provenance or mixed stale/current artifacts; this review did not do so.

[A] No source review here demonstrated a new live-trading incident, credential exposure, order/account mutation, or corrupted current corrected-PnL cohort. Credential scans/redaction are heuristics: ideas `q03_eligible.py:29–30` prints raw snapshot JSON despite its following redaction comment; runtime reader accesses secrets if run. These are cautions about script execution/output, not claims that reviewed report outputs contain secrets. No exact sample-impact count is fabricated.

### Complete assigned-file coverage ledger

Every entry below is a full manual source read. Function start/end boundaries and purposes are recorded separately in `functions.json`; statements apply to the file's historical/current role described above. Additional dependency reads are recorded separately and do not inflate106-file coverage.

#### 1. `docs/superpowers/reports/2026-09-04-research-conformance-data/d10dump/main.go`

[A] Lines 1–34. Argument DSN is trusted; comment says read-only but no DSN enforcement or argc check; production expectancy.LoadAndBuildAt supplies semantics, JSON errors ignored.

#### 2. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-complete-data/collect_sources.py`

[A] Lines 1–19. Archived fixed source spans/base label and log line captures; reads isolated old checkout, selected main logs, immutable gate source and health GET; source base label not verified dynamically. No named functions.

#### 3. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-complete-data/render_report.py`

[A] Lines 1–271. Full 271-line dense report renderer read in three bounded chunks. Explicitly supersedes old65-row/MFE-target/ATR-ceiling/session-ban/impossibility recommendations, keeps58 corrected cohort and9 immutable-risk subset. Mixture of dynamically rendered CSV and many fixed historical claims means rerun with changed inputs is not fresh verification. Does not run trading modules.

#### 4. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-complete-data/supplement.py`

[A] Lines 1–64. Offline runpy audit dependency then ATR/path/provenance sensitivity. Complete consecutive closed-minute warmup, explicit no intra-minute ordering, separates raw zero-MAE uncertainty IDs569/584. No named functions; imported helpers and predicate lambdas.

#### 5. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-data/q15_touch_episodes.py`

[A] Lines 1–45. Exploratory shape==rejection proxy mislabels itself hold; no formation/dedupe/row-ID population evidence; fixed UTC-5 and whole-hour NY split inaccurate outside narrow sample.

#### 6. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-data/q16_decision_census.py`

[A] Lines 1–27. Exploratory decision action/refusal counters; timestamps grouped textual day, no trader scope/row IDs, malformed decisions skip risk/execution log counts. Execution-log strings iterate characters if parsed as string. No named functions.

#### 7. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-data/q21_stats.py`

[A] Lines 1–155. Exploratory enriched CSV and live tape join; inherited pnl provenance not verified in this script. Five-minute ATR aggregates incomplete/gapped blocks and contracts; later complete supplement fixes closed-block continuity. MFE reach and stop tightness are unordered proxies, not executable counterfactual.

#### 8. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-data/revise/lib.py`

[A] Lines 1–23. Shared archive helpers; explicit read-only DB URI; empty quantiles/means None, empty Wilson zeros.

#### 9. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-data/revise/r01_headline.py`

[A] Lines 1–39. Archive CSV n65/n58 sensitivity with explicit unresolved ID exclusion, SD/normal CI/payoff/power calculations; requires nonempty wins/losses; no named functions. Headline labels pinned, counts printed dynamically.

#### 10. `docs/superpowers/reports/2026-09-05-vet-01-way-it-trades-data/revise/r07_replay.py`

[A] Lines 1–104. Exploratory counterfactual prototypes: unordered MFE/MAE threshold repricing, fixed $2/pt ignores quantity/costs, path omits partial entry minute and can use unfinished cutoff minute close; not trading validation.

#### 11. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/complete/audit.py`

[A] Lines 1–98. Revised audit quarantines historical formation-unknown touch rates; uses read-frozen levels, complete 60m windows, trailing prior completed days, deterministic same-read distance matching. SQL is snapshot read-only, but no contract/source or trader filtering. Negative as-of index wraps future final row if no predecessor; empty trailing history divides by zero.

#### 12. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/complete/summarize.py`

[A] Lines 1–55. Offline revised level summary explicitly says no clustered CI from one day. Many literal historical counts/source definitions; never current source validation. First exposure dedup by kind/price lacks formation identity.

#### 13. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/complete/verify_details.py`

[A] Lines 1–59. Exact-version RTH-L provenance sensitivity, fixed Sep3 RTH bar bounds, IDs preserved; numeric-trigger regex heuristic and <=1pt price match not canonical identity.

#### 14. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/q11_replay.py`

[A] Lines 1–205. Archived exploratory detector port; header lookahead-free overstates full pipeline: random null prices drawn from eventual day range and RN universe uses day high/low. Profiles approximate range-spread volume, not kernel close-bin implementation; UTC-5 fixed, no contract/source/gap guards.

#### 15. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/q13_arms_positions_by_kind.py`

[A] Lines 1–83. Exploratory plan-level matching falls back to latest available version, risks retrospective misattribution; corrected PnL NULLs counted separately. Later verify_details uses exact versions.

#### 16. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/q16_tod_gap_onrange.py`

[A] Lines 1–29. Exploratory tape descriptives exec prefix of external q11 file, inheriting fixed timezone/unfiltered tape; gap fill and drive are descriptive realized range associations, not causal results. No named functions in this file.

#### 17. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/revise/r03_ep_analysis.py`

[A] Lines 1–56. Offline archive q11 episode sensitivity; dedup by day/price/open ignores disagreement by keeping first; demonstrates overlap and small independent level-day counts.

#### 18. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/revise/r04_pool_dedup.py`

[A] Lines 1–40. Archive sensitivity exposes first-seated and ever-seated contrasts; ever-seated conditions on future reads, not causal seat efficacy. Imports q11 via exec and lacks complete-window censoring.

#### 19. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/revise/r05_reach.py`

[A] Lines 1–41. Offline archived distance/level reach summaries; numerous literal Wilson examples separate from CSV computations; no zero guard in wilson.

#### 20. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/revise/r08_pnl_cond.py`

[A] Lines 1–38. Corrected-PnL condition cells with exact plan/version matching, explicit null IDs and source/unresolvable sensitivity; no CLOSED/trader scope, repeated DB reads lack transaction.

#### 21. `docs/superpowers/reports/2026-09-05-vet-02-levels-data/revise/r09_labelcensus.py`

[A] Lines 1–18. Non-weekly archived plan-level label census; dedup sessions by date/session, not owner; parsing skips malformed docs and display prefix is not canonical kind. No named functions.

#### 22. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/complete/audit.py`

[A] Lines 1–148. Revised complete decisions audit distinguishes persisted reject share, temporal reauthor association, bar reachability, broker execution; preserves UTC offsets and Chicago registry windows. Complete-grid uses count not spacing; latest lifecycle retrospectively excludes past-active versions; successor episode lookup omits trader key. No runtime outcome reproduction.

#### 23. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/q04_decisions_parse.py`

[A] Lines 1–36. Parses action JSON including nested decisions and explicit parse/missing states; fixed UTC-5 SQL day, no trader filter; action sets count cycles rather than individual actions.

#### 24. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/q05_plan_doc_peek.py`

[A] Lines 1–17. Read-only archived plan structure peek at fixed Sep4 NY highest version; truncated output intentionally exploratory, no ownership tie-break.

#### 25. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/q06b_trigger_inforce.py`

[A] Lines 1–54. Archived FIRED means bar spans confirm price, not actual confirmation/execution. Session ends hardcoded ASIA01:30 and NY15:00 differ current registry; groups omit plan owner; inclusive end despite half-open documentation.

#### 26. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/q13_coverage_leg3_broker_levels.py`

[A] Lines 1–74. Archive mixed coverage/book/geometry probe; counterfactual uses stop-first touch bar and truncated horizon last close, explicit truncation note; no cost/queue/source/contract checks.

#### 27. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/q14_s1_episode_closes_durations.py`

[A] Lines 1–42. Archive source has concrete mislabeled 5-minute aggregation: HH:M prefix groups ten-minute buckets. Counterfactual merges flat and never-filled into zero; not present production execution.

#### 28. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/revise/r02_era_positions.py`

[A] Lines 1–71. Exploratory plan-linked corrected-PnL groups retain row IDs/nulls but plan_id IS NOT NULL admits empty/unresolvable and no date guard. Reconcile is only lineage proxy; comparisons confounded by era/path.

#### 29. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/revise/r10_population.py`

[A] Lines 1–90. Revised broad/compliant exact corrected-PnL exclusions; plan-linked population query no entry era condition, no CLOSED filter. Fixed offset and hardcoded report cohort labels.

#### 30. `docs/superpowers/reports/2026-09-05-vet-03-decisions-data/revise/x01_population.py`

[A] Lines 1–64. Explicit entry-era query, corrected PnL NULL counts and IDs. Compliant only excludes UNRESOLVABLE and still includes empty plan IDs; no CLOSED filter; source lineage descriptive.

#### 31. `docs/superpowers/reports/2026-09-05-vet-04-monitoring-data/q48_eod_verified.py`

[A] Lines 1–52. Historical EOD observation snapshot; retrospective arms updated after cutoff marked unknown; orders snapshot explicitly not position book; alerts ack current not historical. Raw account included in snapshot receipt export. Exact IDs/log line sources.

#### 32. `docs/superpowers/reports/2026-09-05-vet-04-monitoring-data/q51_complete.py`

[A] Lines 1–69. Revised monitoring snapshot calls production cmd/arm-state-sql rather than retyping lifecycle; exact broker accepted/fill stop assertions distinguish ledger drift from slippage. Literal cohort/base assertions intentionally pin historical receipt; no live claim.

#### 33. `docs/superpowers/reports/2026-09-05-vet-04-monitoring-data/q52_receipts.py`

[A] Lines 1–14. Selected NT8 and Go log lines at fixed historical times; immutable source diff and public health GET with explicit no readiness claim. No named functions.

#### 34. `docs/superpowers/reports/2026-09-05-vet-04-monitoring-data/q53_validation.py`

[A] Lines 1–22. Historical artifact consistency validator: cohort/sample IDs, broker math, source receipts, syntax and changed-path scope. HTTP200 assertion establishes response only; no trading test. No named functions.

#### 35. `docs/superpowers/reports/2026-09-05-vet-04-monitoring-data/r06_population.py`

[A] Lines 1–29. Population exclusion waterfall by source/era/unresolved/corrected NULL, row IDs retained except pre-era omitted literal count; no CLOSED check; supplemental fixed Wilson examples.

#### 36. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/complete/q11_fill_vs_bar.py`

[A] Lines 1–48. Revised entry audit adds eligible corrected population, side match for arm and fill_time_ms, preserves missing columns; nearest price/time joins not unique execution proof; no contract/source filtering in bars. No named functions.

#### 37. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/complete/q14_mae_mfe.py`

[A] Lines 1–33. Revised exit-label extraction explicitly nearest log side/price within five minutes, not execution-id proof. Unused statistics helpers remain; input log and cohort nonempty assumed.

#### 38. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/complete/q32_sources.py`

[A] Lines 1–26. Archived source spans, selected NT8 receipts and strategy subset keyed historical bound strategy; captures base comparison and no new deployment assertion. No named functions.

#### 39. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/complete/q35_complete.py`

[A] Lines 1–67. Revised execution funnel: IDs and whole/boundary-inclusive lifetime touch proxies explicitly not broker election; immutable initial risk unavailable cohort-wide; only arm35/position591 accepted geometry illustrated. filled_to_win is literal empty IDs, pinned losing fill rather than general calculation.

#### 40. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/complete/q36_logs.py`

[A] Lines 1–17. Read-only archival log extraction removes ANSI, captures line provenance, selected arm35 times and cancellation guards. No named functions.

#### 41. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/complete/q37_sources.py`

[A] Lines 1–35. Archived source comparison and selected bar/fill/strategy/arm35 evidence; snapshot states first distinct whole order JSON containing audited stop signals, not timeline proof. No named functions.

#### 42. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/complete/validate_evidence.py`

[A] Lines 1–28. Historical evidence validator exact 58-ID whitelist, corrected PnL math, funnels/geometry, independent SQL and syntax. Does not test production call sites, and string presence checks do not prove universal read-only behavior. No named functions.

#### 43. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/q11_fill_vs_bar.py`

[A] Lines 1–58. Original entry audit includes unresolved PnL population and maps source to market/limit without execution-type proof; same-price arm search lacks side filter, later complete version corrects this. No named functions.

#### 44. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/q15_decision_slip.py`

[A] Lines 1–42. Exploratory decision price/slippage matching has operator precedence bug: entry_price truthy bypasses action check; also no trader/symbol/side/unique-execution match. Values are intention-to-fill proxy, not measured broker slippage.

#### 45. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/q31_verified.py`

[A] Lines 1–92. Final archive execution audit improves explicit corrected PnL/ID receipts and immutable-risk caveats, but fixed cutoff and correction-note unresolved filter differ revised plan_id scope. Simple TR14 proxy is not Wilder actual entry ATR; all bars lack contract/source filter.

#### 46. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/q32_sources.py`

[A] Lines 1–25. Original historical source/NT8/strategy subset receipts, pinned base2a66d91c; no named functions and no deployment claim.

#### 47. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/q33_metrics.py`

[A] Lines 1–14. Fixed Wilson examples plus current DB selected arm IDs and open/EOD bar-bucket means; not inferential evidence of execution quality; fixed UTC-5.

#### 48. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/revise/apply_edits.py`

[A] Lines 1–201. Archived report patcher exact-once replacement errors still permit partial write; historical 65-to-58 correction and policy prose, not executed.

#### 49. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/revise/r01_compliant.py`

[A] Lines 1–46. Corrected 65 versus sentinel-excluded58 population with IDs, fixed UTC-5.

#### 50. `docs/superpowers/reports/2026-09-05-vet-05-execution-data/revise/r03_tape.py`

[A] Lines 1–50. Archived tape proxy: many-to-many approximate price/time join lacks trader/symbol.

#### 51. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/complete/build_report.py`

[A] Lines 1–276. Frozen complete report renderer supersedes legacy risk artifacts; mixes literal prose with dynamic JSON. Per-trade-cap proposals superseded by daily-loss owner policy.

#### 52. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/complete/context_evidence.py`

[A] Lines 1–30. Read-only snapshot calendar and all-account corrected cash sensitivity, not exact live replay.

#### 53. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/complete/legacy_mc_drawdown.py`

[A] Lines 1–177. Archived conditional resampling: Bernoulli recursion handles flats differently than iid paths; complete report retires mismatch.

#### 54. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q03_daily_dist.sh`

[A] Lines 1–10. Read-only daily corrected inventory; raw PnL only unresolved diagnostics; fixed UTC-5.

#### 55. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q06_day_mc.py`

[A] Lines 1–76. Active-day block bootstrap truncates horizon; ad hoc t critical and ICC heuristics, finite sample only.

#### 56. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q07_stops_excursions.sh`

[A] Lines 1–16. Mutable arm distance cannot prove initial broker risk; zero extrema conflate missing and measured zero.

#### 57. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q09_strategy_knobs.sh`

[A] Lines 1–30. Explicit trader binding and recursive risk-keyword extraction, no robust redaction.

#### 58. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q10_sessions_conditions.py`

[A] Lines 1–32. Corrected subgroup descriptions; claimed t interval uses normal1.96; slot is condition proxy.

#### 59. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/q12_ab_confirm_fills.sh`

[A] Lines 1–9. Commission diagnostics use all-time query despite era heading; NULL zero conflates missing.

#### 60. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/r03_rig.py`

[A] Lines 1–173. Corrected population bootstrap and streak/drawdown diagnostics; flat distinctions and initial zero equity; conditional sample only.

#### 61. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/r06_arms.py`

[A] Lines 1–50. Mutable arm distances and absolute reward/risk mask target side; ledger placements not broker acknowledgements; cap hypothesis retired.

#### 62. `docs/superpowers/reports/2026-09-05-vet-06-risk-data/r07_regime.py`

[A] Lines 1–68. Descriptive unfiltered tape regimes, mean14 TR not Wilder; no explicit complete-day check; cap hypothesis retired.

#### 63. `docs/superpowers/reports/2026-09-05-vet-07-prompts-complete-data/audit_evidence.py`

[A] Lines 1–61. Pinned reject classification first matching regex; denominator64 fixed, assertion only no UNCLASSIFIED. Manual constraint mapping validates cardinality not semantic equivalence. Prompt shortage fields sourced exact stored bytes.

#### 64. `docs/superpowers/reports/2026-09-05-vet-07-prompts-complete-data/finalize.py`

[A] Lines 1–77. Frozen prompt report publication, exact quote assertions, authored geometry expressly not realized risk. Original contradictions preserved; policy cuts never applied. Mixes literal historical totals and generated artifacts; safe only pinned snapshot.

#### 65. `docs/superpowers/reports/2026-09-05-vet-07-prompts-complete-data/measure.py`

[A] Lines 1–53. Two textual historical prompt replacements, explicitly not runtime replay. Manual line-based fact/instruction/schema allocation; BPE category sums may differ whole. Same encodings for compression, not DeepSeek billing or model equivalence.

#### 66. `docs/superpowers/reports/2026-09-05-vet-07-prompts-complete-data/validate_artifacts.py`

[A] Lines 1–39. Pinned artifact assertions validate58-row totals, spans reconstruct, maps count, docs scope and secret heuristics; no behavioral equivalence. Credential key scan does not prove all secrets absent.

#### 67. `docs/superpowers/reports/2026-09-05-vet-07-prompts-data/q07_measure.py`

[A] Lines 1–33. Heuristic prompt shape and fallback tokenizer counts; any tokenizer exception becomes null. Empty documents divide by zero; unused split_hdr argument. Historical only.

#### 68. `docs/superpowers/reports/2026-09-05-vet-07-prompts-data/q20_weekday.py`

[A] Lines 1–17. Weekday daily-bar up fraction excludes flats, assumes next calendar-day label and fixed UTC-5. No contract/source or closed-bar filter; descriptive association cannot justify weekday conviction.

#### 69. `docs/superpowers/reports/2026-09-05-vet-08-stretch-data/complete-0905/extract.py`

[A] Lines 1–79. One read-only transaction, opportunity version/session windows, corrected eligible IDs. CSV writer silently leaves previous file if empty; strategy selected by fixed prefix first row. Logs/snapshots separate observations; no current runtime reconstruction.

#### 70. `docs/superpowers/reports/2026-09-05-vet-08-stretch-data/complete-0905/logs.py`

[A] Lines 1–25. Regex log and broker excerpts with numbered provenance; lifecycle events parse Chicago time, broker account name redacted. Selected patterns and dates cannot prove full event coverage.

#### 71. `docs/superpowers/reports/2026-09-05-vet-08-stretch-data/complete-0905/reaper.py`

[A] Lines 1–19. Observed cancellation-time reaper component approximation uses previous snapshot received age<=60s; assumptions explicitly30s interval/cache survival. No counterfactual inventory. Missing account/trader scoping; empty out indexing fails.

#### 72. `docs/superpowers/reports/2026-09-05-vet-08-stretch-data/complete-0905/verify.py`

[A] Lines 1–23. Pinned complete artifact checks include exact source port containment and closed minute checkpoints. Explicitly excludes full replay/fills/PnL/initial-risk claims. Source-current containment expected to drift; no tests executed.

#### 73. `docs/superpowers/reports/2026-09-05-vet-08-stretch-data/q34_wilson.py`

[A] Lines 1–34. Literal historical Wilson table mixes retired65-era proxies and ledger event units. Zero denominator represented zero triple instead of unavailable; not fresh measured rates.

#### 74. `docs/superpowers/reports/2026-09-05-vet-09-complete-data/q01_context.py`

[A] Lines 1–13. Read-only transaction context plus health GET and source excerpts; origin/dev hash does not establish HEAD/runtime binding. Queries capture diagnostic row identities, not strategy eligibility.

#### 75. `docs/superpowers/reports/2026-09-05-vet-09-complete-data/q03_proxy_sensitivity.py`

[A] Lines 1–14. Explicit sensitivity removes uncertain zero-MAE winners569/584 without changing primary58 population. Nonempty linear quantile; stored excursion/floor-age proxies remain not validated cohort.

#### 76. `docs/superpowers/reports/2026-09-05-vet-09-top-ten-data/q03_eligible.py`

[A] Lines 1–46. Corrected-PnL canonical query differs exclusion diagnostic: sentinel plan_id exclusion missing from latter; blank plan IDs allowed. IDs and Chicago17h days present. Snapshot orders_json output not actually redacted despite comment. No execution.

#### 77. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/complete/source_evidence.py`

[A] Lines 1–12. Source-only excerpt writer prints fixed rev488ce827 but reads working tree paths without verifying HEAD or cleanliness; provenance label can diverge on rerun.

#### 78. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/q01_asof.sh`

[A] Lines 1–10. Independent read-only SQLite calls inventory freshness; no shared transaction, max-created errors collapsed to n/a. Fixed UTC-5 labels.

#### 79. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/q02_era.sh`

[A] Lines 1–20. Old first-pass era aggregation lacks sentinel plan_id exclusion; UTC-midnight start differs CT era. Does not establish current corrected canonical cohort.

#### 80. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/q04_store_summaries.sh`

[A] Lines 1–27. First-pass mutable arm/plan ledger and shadow/touch groups; raw net_pnl not corrected trade PnL, touch labels not strategy expectancy. No sample IDs for most counts.

#### 81. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/q07_canonical_set.py`

[A] Lines 1–66. Archived risk proxy: note-based unresolved exclusion, timezone-naive epoch boundary; arm join overwrites by plan/scenario without version/side/trader, mutable stop; nearest decision includes one minute future and lacks side match. Fixed2 USD conversion ignores quantity. Not initial risk or causal R evidence.

#### 82. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/q09_arms_kinds_sessions.sh`

[A] Lines 1–22. Ledger dedup plan/scenario/rounded price merges versions/legs; era funnel lacks era predicate. Decision text LIKE counts not executed trades. Touch group comparisons descriptive.

#### 83. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/q10_targets_mfe.py`

[A] Lines 1–51. Consumes archived ambiguous risk joins and unordered stored MFE. Fixed-target counterfactual explicitly upper bound; cannot establish fill ordering/profitability. Median indexes len(S) rather than filtered RR population may fail; finite data assumptions.

#### 84. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/q11_outage_executor_shadow.sh`

[A] Lines 1–18. Last/first decision times bound absence of decisions, not proved feed outage. Text intent counts and model-hours sum latency proxy, not provider billing or action execution.

#### 85. `docs/superpowers/reports/2026-09-05-vet-10-ideas-data/r10_rest.sh`

[A] Lines 1–38. Historical read-only named-row diagnostics and weekday arithmetic; median fixed offset28 assumes sample cardinality, missing initial corrected eligibility consistency. No updates.

#### 86. `docs/superpowers/reports/2026-09-08-the-strategy-data/extract.py`

[A] Lines 1–53. Historical single-transaction read-only live extraction; uniquely scopes trader and bound strategy, sanitizes identities/key prefixes. Plan/lifecycle/excursion/bars queries broader than trader scope; archived pre-contract/source projections not current replay-safe.

#### 87. `docs/superpowers/reports/2026-09-08-the-strategy-data/read_runtime.py`

[A] Lines 1–32. Source only; DO NOT EXECUTE for this review. Reads .env and /proc environment, mints short-lived JWT then GETs four local endpoints and records loaded/disk hashes; first trader only. Sanitization is heuristic, stdout health is unsanitized.

#### 88. `docs/superpowers/reports/2026-09-08-the-strategy-data/supplement.py`

[A] Lines 1–74. Offline exported cohort analysis retains IDs and effective explicit legs; checks historical 1.5 ATR floor, not current structural-stop law. Missing quantile populations/division geometry can abort.

#### 89. `docs/superpowers/reports/2026-09-10-scenario-level-identity-data/basis_receipts.py`

[A] Lines 1–34. Immutable SHA public-basis receipt generator; compares HTTP bytes exactly to git blob; explicit pinned historical revisions and output argument. No named functions.

#### 90. `docs/superpowers/reports/2026-09-10-scenario-level-identity-data/mutation_driver.py`

[A] Lines 1–37. Mutation driver EDITS SOURCE in derived repository, builds and tests, restores in finally. Not authorized to run here. Checks exact one replacement, expects build green/test nonzero, but arbitrary infrastructure test failure counts killed. No named functions.

#### 91. `docs/superpowers/reports/2026-09-11-forming-candle-test-data/analysis.py`

[A] Lines 1–382. Archived forming-candle association study, not deployable prediction validation. Nearest ±10m same-price/day joins exclude multiply claimed outcomes but do not establish formation identity. Closed-episode features can overlap outcomes; diagnostics explicitly inspect this. No snapshot transaction; horizon SQL lacks contract/source filters and skips primary ambiguous rows. Footers hardcode historical counts.

#### 92. `docs/superpowers/reports/2026-09-12-structural-stop/harness/audit_compositions.py`

[A] Lines 1–56. Copied changed-stop logs only, not denominator of all compositions. Reconstruct latest preceding plan with authored-stop price match; conditional selection and one-second tolerance cannot prove actual arm identity.

#### 93. `docs/superpowers/reports/2026-09-12-structural-stop/harness/data.go`

[A] Lines 1–193. Read-only SQLite copy; groups MNQ bars by contract/timeframe; excludes mixed/off-scale and empty/spans-roll contracts. ContractAt nearest previous minute has no staleness ceiling or equal-time contract tie breaker. Calendar session windows hardcoded. No execution calls.

#### 94. `docs/superpowers/reports/2026-09-12-structural-stop/harness/main.go`

[A] Lines 1–143. Measurement first zone touch per read; builds current kernel zone maps, complete shortlisted widths only, stores future bars intentionally for offline evaluation. H12 hold/break excludes touch close but penetration includes entire touch range; upper bound ordering caveat. Empty merged tape panics.

#### 95. `docs/superpowers/reports/2026-09-12-structural-stop/harness/stop_port.go`

[A] Lines 1–122. Frozen legacy widest-wins comparator pinned to 6b3fddf7, explicitly not current production geometry.

#### 96. `docs/superpowers/reports/2026-09-12-structural-stop/harness/summarize.py`

[A] Lines 1–112. Current structural-stop analysis acknowledges already-exposed held-out year; common five-day block draws, nine-cell family bounds; occupancy explicitly not live selector. risk_cap_missing output/text reflects superseded per-trade policy, must not restore it.

#### 97. `docs/superpowers/research/2026-09-12-backtest-structure-fade/harness/detect.go`

[A] Lines 1–146. Production detector kernels called directly but no owner levels or level-state history. Latest 30 profile POCs queried by day without contract or completion timestamp; possible future same-day profile leakage requires population verification.

#### 98. `docs/superpowers/research/2026-09-12-backtest-structure-fade/harness/main.go`

[A] Lines 1–440. Archived exploratory 16-cell geometry backtest; FillA/FillC always true at any zone overlap; target skips closer zones to meet minR; these differ from current production/updated geometry sweep. Emits all cells and held-out readouts; no actual trade calls.

#### 99. `docs/superpowers/research/2026-09-12-backtest-structure-fade/harness/out.go`

[A] Lines 1–232. Archive reporting ignores unfilled returns, zeros unresolved statistics, uses event iteration order for drawdown (not occupancy equity); MNQ fixed $2/point.

#### 100. `docs/superpowers/research/2026-09-12-backtest-structure-fade/harness/render_c1.py`

[A] Lines 1–37. Archive markdown renderer reads geometry/reference/surface JSON; index-zips era arrays without cell-identity checks; no named functions.

#### 101. `docs/superpowers/research/2026-09-12-backtest-structure-fade/harness/stop_port.go`

[A] Lines 1–237. Archive legacy comparator plus text guard; verifyPort compares embedded string to production source, not executable copied function; production signature now intentionally differs, so archive startup expected to refuse.

#### 102. `docs/superpowers/research/2026-09-12-backtest-structure-fade/harness/surface.go`

[A] Lines 1–135. Archive 16-cell significance surface; independent sign flips per flattened cell/event destroy pairing across same opportunities; fixed observed SE, maxT empirical p can equal zero, PBonf multiplies already maxT-adjusted p.

#### 103. `docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/data.go`

[A] Lines 1–193. Read-only SQLite copy; groups MNQ bars by contract/timeframe; excludes mixed/off-scale and empty/spans-roll contracts. ContractAt nearest previous minute has no staleness ceiling or equal-time contract tie breaker. Calendar session windows hardcoded. No execution calls.

#### 104. `docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/seam.go`

[A] Lines 1–134. Seam checks report booleans but do not stop replay; SQL/file/JSON errors may be ignored. Only named four timeframes checked; 06-22 delta waived but total fixed. Single-contract assertion checks grouping construction, not authoritative contract labels.

#### 105. `docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/stats.go`

[A] Lines 1–154. Local statistics despite comment claiming kernel Wilson; empty statistics become zero; maxDD starts peak at first cumulative return, excluding initial loss from zero.

#### 106. `docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/surface.go`

[A] Lines 1–295. Archive 81-cell surfaces; map events share tape but permutations treat map groups independently; hold outcomes globally shuffled per horizon, loses cross-horizon/tape dependence. No production gating.

---

<a id="section-24"></a>

> Original: [reviews/19/report.md](reviews/19/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 19 — runtime, operations and support source review

Base: `63968be62e44db2fb07a92883e02127b9064b0be`. Worktree: `/tmp/vl-understanding-market-20260913`; verified revision and initially clean porcelain. This is source understanding, not a production certification. All 95 assigned files (12,529 lines) were read in full. `reads.json` records per-file hashes/ranges/semantics; `functions.json` contains 407 named-function/method entries (279 Go declarations, Python functions, shell helpers) with 58 Go literal callbacks grouped under their owning declarations. Shell helpers inside fixture heredocs are fixture text rather than production declarations. No deploy, restart, process control, live DB read/write, account mutation, external message, or network probe was performed. No tests were executed.

Evidence: **[A]** code directly read at the base, **[B]** consequence inferred from that code, **[C]** unresolved hypothesis. “Static risk” below means an identified implementation concern, not a reproduced exploit or runtime trading failure. Root reports boot-integrity ordering repaired separately; the base and this review remain immutable.

Rules consulted: supplied main AGENTS instructions, tracked `docs/superpowers/CLAUDE-canon.md`, AUDIT-CHECKLIST pre-audit R1–R10, cutover procedure and lock classes101/102, SYSTEM-MAP runtime/Stage A sections, corrected RULEBOOK scope. The daily-loss owner correction is authoritative; this review proposes no additional per-trade cap. Latest base-local document history:

- CLAUDE-canon: `07b53e65 cleanup batch 1 — B5 the mutation harness that cannot fake a verdict, B6 the worktree-add check`.
- AUDIT-CHECKLIST: `dfda15e1 test: isolate session clock fixtures from weekly backfill workers`.
- SYSTEM-MAP and RULEBOOK: `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap`.

### Runtime wiring and ownership

[A] `main.go:40` loads dotenv, initializes logger/config, constructs mandatory RSA/AES service and installs the global crypto hook **before** store reads. SQLite path can be overridden by argv; directory creation failures log but do not immediately abort. `researchsnapshot.Start` at main:77 initializes a separate sidecar using `DBPath + .research.db`, even when the trading database is PostgreSQL. `store.NewWithConfig` then initializes schema/defaults. WARN+ log shipping attaches only after the store exists; earlier warnings are journal/file only.

[A] Boot also performs historical correction/link/seam/PNL migrations and installation-ID persistence. JWT is configured at main:150; the log proves assignment, not strength. Acceptance-rule repair runs before trader loading. Order-snapshot persistence hook is registered before `LoadTradersFromStore` at main:251, because lazy TCP server creation captures this hook. Hook copies broker account/build/orders, working count and local receipt time into the store; insert failure warns without undoing cached broker truth.

[A] At this base, trader loading precedes `kernel.AssertBootIntegrity` at main:289; the adjacent comment claiming “before any trader cycles” is not supported by order alone. This is the root-owned separately repaired finding, not another fix request. Likewise role-map application and many record-only boot backfills occur after trader load. Boot reporting calls resolved kernel/trader/store functions for prompt geometry, state gates, session risk/calendar, contract-related data and read-model counts. Several expensive maintenance actions are environment-gated and backup-first (E8/regrade/waveA); episode/fade/one-setup backfills run as boot work. These are reasons a normal application launch is not a read-only probe.

[A] Sandbox synthetic bars are installed late in boot at main:640. API server receives manager/store/crypto/configured host/port; web agent is registered and started. API and Telegram use bare goroutines. SIGINT/SIGTERM causes HTTP shutdown, `StopAll`, deferred agent/store/research cleanup and a normal exit. `deploy/vl.service` sets `Restart=on-failure`, so ordinary graceful termination does not request restart. Units are templates with actual paths substituted by the installer; installed units were not examined.

[A] `config.Init` defaults HTTP to127.0.0.1:8080 and SQLite data/data.db; insecure JWT fallback is now explicitly warned. Setting an off-loopback host warns but does **not** refuse the insecure secret combination. Transport encryption defaults false. Experience telemetry defaults true. NT account allow-list is parsed without changing accounts; trading mode defaults crypto; dormant/notional fields are labeled in source. Numeric env parsing accepts values without domain checks, including float NaN/Inf. Whether downstream consumers reject each invalid value is outside this slice.

### Priority source findings

#### A — tools labeled read-only can initialize/write the database

[A] `cmd/bars-export/main.go:34`, `cmd/gate-jwt/main.go:48`, dayplan arm/repair/sessions preview paths, and excursions reporting call `store.New`. The dependency was read: `store/store.go:63` calls `initTables` and `initDefaultData`; :144 creates tables and :242 migrates excursion zeros, while :249 initializes defaults/migrates equity. **“Dry-run” only gates the command's explicit business update, not constructor side effects.** Bars-export's “never writes” claim is false as a call-path contract. Detector-report requests `?mode=ro` yet calls the same initialization path, so it can fail during attempted schema work rather than be a clean readonly opener. `cmd/research_export` correctly uses `OpenReadOnly`; decisive-test uses read-only GORM plus `NewFromGorm` (no constructor migration), but is network-capable.

[B] A maintenance preview can change schema/defaults or legacy rows, or fail on a read-only DB. None was run against production. Scope and backup authorization must be resolved before execution; these comments are not evidence of harmlessness.

#### A — obsolete executable cutover and install surfaces bypass current procedure

[A] `deploy/leveltruth-cutover.sh:9` pins a historical build SHA; :17 checks DB open positions and :22 accepts **any** count=0 substring in two journal snapshots, without current account freshness, full five-leg cutover gate, ownership lock or branch/clean verification. :30 writes RELEASE before checking replacement binary; :36–39 swaps/may kill with only `set -u`; :44 accepts an unbound recent integrity line, potentially from the old boot. Timeout does not roll back. The header says manual-only and no timers; that is an operating instruction, not enforced owner acknowledgment. Current checklist five-leg gate is much stronger.

[A] `install.sh` and `install-stable.sh` still download unpinned `upstream github link (removed in the VL rename)` Compose assets, default installation directory to `$HOME/vl`, overwrite Compose, and start containers. `install.sh:171` optionally deletes all three trading tables with no backup or WHERE scope. Generated env files lack explicit mode600 and use Asia/Shanghai. `start.sh:305` does git pull and rebuild/start, while restart/clean/key-rotation bypass current deployment gates. `start.sh:43` tests `command -v docker compose`, not `docker compose version`, so presence of docker does not establish the Compose plugin. These are legacy capabilities, not recommended current NT8 operating paths.

[A] `railway/start.sh:9` generates new AES/RSA keys whenever env keys are missing; they are process env only. Persistent ciphertext therefore depends on provisioned stable keys across restarts. nginx `/health` always returns200 and background backend is not supervised; `tail -f /dev/null` can keep container “healthy” with backend dead. This script does not generate JWT_SECRET. No installed/deployed usage was verified.

#### A — lock helper and documentation retain competing writer paths

[A] `deploy/vl-lock.sh:94` atomic mkdir protects acquire; :132 starts a bounded detached keeper; :261 validates session and expiry at heartbeat write; :489 owner-scoped release verifies directory removal. Keeper signals are guarded by numeric handle, lock path in command line, and group-leader check. Incomplete locks have separate states3/4; stale is explicitly not proof of abandoned ownership. These mechanisms are substantially stronger than the obsolete main instruction mirror.

[A] However `cmd_with_heartbeat` at :428 still invokes a foreground beat and spawns **another** periodic writer while acquire's keeper exists. It kills only its own beater shell and does not register that writer with release. This contradicts tracked canon's “do NOT hand-beat — a second writer” rationale, even though canon still advertises this wrapper. [B] An in-flight secondary heartbeat is outside `_stop_keeper`'s group containment; the historical orphan-write class merits focused concurrency reproduction, not a claim of an observed takeover.

[A] `cmd_reclaim` :459 rewrites holder/heartbeat but retains prior expiry and does not stop/restart a keeper for the successor. Reclaiming an expired lock therefore gives one fresh stamp but subsequent beats are refused. Tool text suggests “extend explicitly,” yet dispatcher has no extend verb. `_write_meta` errors can also be overwritten by following echo success because script lacks errexit/explicit propagation at some call sites. `cmd_check` ignores a legacy flat lock even though status prints one. These are static operational consistency gaps.

[A] Full lock-test source was read, not run. It tests acquire collision, expiry, group targeting, conditional delayed-writer release, incomplete states and removal failure. Its old `with-heartbeat` liveness assertion reads `$LOCK_DIR/heartbeat` even though heartbeat is in meta, so two missing-file reads can compare equal. Claim tests validate regex shape; they do not test remote transport failure. `vl-claim.sh:56` remote lookup failure is not explicitly distinguished from no matching branch, and branch/default-data freshness is delegated to operator fetch; `claim_msg_of` does not implement its “first-parent” comment.

#### A — security claims are stronger than actual guarantees

[A] `security/url_validator.go:93` rejects non-HTTP schemes, blocked names and resolved private addresses, but allows unresolved non-IP names to proceed. `SafeHTTPClient` :169 checks `net.LookupIP(host)` then dials the original hostname at :196, causing independent resolution rather than binding the validated IP. [B] DNS changes between validation/dial leave a rebinding opportunity. No DNS manipulation or exploit was performed. Public/unknown special ranges not explicitly covered by `isPrivateIP` require dedicated tests before claiming comprehensive reserved-space rejection. Safe client is used by MCP configuration (`mcp/config.go:86`, search evidence) and external data fetch (`kernel/engine.go:788`, search evidence).

[A] `crypto/crypto.go:415` Scan suppresses decrypt failure and returns ciphertext; :449 Value suppresses encryption failure and returns original plaintext. No initialized service also permits plaintext persistence. `NewCryptoService` requires both keys and main installs it early, reducing this risk for the normal server path; many CLI store openers do not initialize crypto. This is a fail-open storage contract, not proof a plaintext credential was stored. AES nonstandard-length keys are SHA256 normalized, not password-strengthened. Already-prefixed input bypasses encryption based on prefix alone.

[A] `DecryptPayload` :284 accepts TS=0; nonzero TS only checks -1min/+5min age, no replay cache. AAD content is authenticated as bytes but parsed user/session/purpose values are not checked against caller identity; outer TS is not itself obligatorily bound into authenticated AAD. Caller authentication remains relevant: `api/crypto_handler.go:67` refuses when transport encryption is off; `api/server.go:175` registers decrypt under protected routing (search evidence). Do **not** misreport this as the old unauthenticated public decryption oracle. Existing handler test at `api/security_p0_test.go:358` pins off-state404.

#### A — Telegram binding, reload, token and concurrency limits

[A] `telegram/bot.go:24` consumes reload only while disabled or after `runBot` ends; runBot has no reload parameter or StopReceivingUpdates call. Therefore advertised live token hot reload is unwired. Fatal initialization returns from supervisor rather than retrying. `resolveToken` reads DB only despite env fallback comments.

[A] `runBot` chooses first DB user and binds first `/start` chat when unbound, without an owner pairing code/private-chat requirement in this file. Once bound, access control is by **chat ID**, not sender identity; group-chat semantics therefore matter. `/lang` occurs before ACL and can toggle shared awaitingLang for the authorized chat. [B] First-use ownership and group participant controls need product authorization review, not an assumed exploit.

[A] Bot JWT is generated only when resolved user ID changes; `auth.GenerateJWT` at auth:85 expires after24h. No renewal occurs for unchanged user. `Manager.Run` serializes per-chat turns with60s wait, but `Reset` bypasses lane and mutates unsynchronized memory during running turn. Agents/lanes are unbounded maps. Long-term compaction appends a summary already containing previous summary; it can grow indefinitely and retains its original LLM object.

[A] `telegram/agent/apicall.go:40` accepts arbitrary tool method/path and sends a bearer JWT to localhost; server endpoints provide real authorization. Prompt-only GET-after-write verification is not an enforcement gate. Native tool loop executes returned arguments without checking tool name. Ten-iteration cap returns “Operation completed” regardless of final API result. Body reads are unbounded. Existing agent tests mock GET/POST workflows and max iterations, not long-lived reload/renewal/ownership/reset concurrency; one test uses port8080 and can contact a real local API during account-context fetch, so this reviewer did not run it.

#### A — sandbox labels do not establish isolation

[A] `scripts/sandbox-up.sh:27` explicitly opens live DB mode=ro for backup despite header saying it is never opened. It scrubs some tables but does not remove model/exchange credentials; scrub command errors are hidden and ignored. Existing sandbox.db skips copying **and** scrubbing. Sandbox process runs from repo cwd and loads real dotenv; setting empty TELEGRAM_BOT_TOKEN does not override DB token resolution. Sandbox-specific loop/arm safety gates outside assignment were not audited here; retaining secrets is independently relevant even if those gates block orders. Listener wait always ends with READY, including timeout.

[A] Seed guard at `cmd/sandbox-seed/main.go:34` checks path substrings only, not resolved symlinks/file identity. It appends plans/trades and increments tests on repeated calls despite “idempotent replaces” comment; many write errors only print/are ignored. Sandbox-down's Vite regex is not worktree-scoped. These scripts must not be used as evidence that an arbitrary copy is safe to run.

### Backup, clock, diagnostics and telemetry semantics

[A] `vl-db-backup.sh:37` uses SQLite online backup, quick_check on copy, gzip partial then rename; first snapshot per ISO week is copied into weekly and retention keeps14 daily/8 weekly by default. It protects consistency better than raw database file copying. There is no concurrency lock, cleanup trap or retention validation: same-second runs collide; failed runs can leave partials despite header; zero/negative retention can delete every retained file; shell whitespace splitting breaks paths with spaces. It backs up only main DB, not keys or research sidecar. Timer file uses host-local05:00/17:30 without explicit America/Chicago and assumes host zone. User timer installer copies hardcoded main-path service definitions; observed installation/linger/restore efficacy remain unverified.

[A] Clock guard is detector-only: RTC drift decides CRITICAL at30s, NTP/Windows are measurements; missing RTC leaves statusOK with n/a, not UNKNOWN. Timestamp/state JSON written by temporary rename; overlapping direct invocations share a temp filename. Chrony remediation is root mutating installer; it writes config and uses enable --now, which need not restart already-active chrony to apply new config. No clock actions were taken.

[A] Logger text time uses `entry.Time.Format` without CT conversion, contradicting main's blanket “host TZ ignored” log. File name chosen at logger Init does not rotate at midnight; .log file is0644 and text formatting omits structured fields. WARN+ DB hook captures fields separately, but its sink contract is nonblocking/nonrecursive rather than enforced. Reinitializing logger after attaching hook can leave atomic attached flag referring to old logger. Fatal exits do not run normal deferred cleanup. `safe.Go` contains panics/callback panics but does not restart work; main does not universally use it.

[A] Gate/error counters are in-memory and reset on caller-supplied CME day differences, not durable accounting. Verified call site `trader/auto_trader_loop.go:251` rolls both from the kernel session clock. Error announce callback runs under errorMu; future reentrant callback could deadlock (current config callback only logs). Weekly map has no runtime rollover in its own implementation despite header, and far-arm comment incorrectly says unknown side increments denominator. Gate map includes observational/skew/shadow counts, so total is not strictly refused entries. Prometheus metrics are a separate process-lifetime surface.

[A] GA telemetry defaults **enabled**, sends installation/user/trader identifiers plus exchange/symbol/amount/leverage and model/token/channel data. Trade producer read at `trader/auto_trader_decision.go:411`; model callback in config. This is pseudonymous event-level data, not “only installation ID”; remote collector requests are bare goroutines with5s timeout, ignored responses/errors, no bounded queue. No telemetry settings or outbound service were inspected.

[A] Wallet cache normalizes key but queries original untrimmed address, no map eviction,30s positive cache. RPC missing result returns zero; display function suppresses failures as0.00, regular function propagates most parse/RPC errors. Address length/hex, status and response-size validation are absent locally. This supports crypto payment plumbing; no wallet or trade was queried.

### Record-only research and analytics

[A] Stage A archive is independent SQLite WAL, max1 connection, schema version1. `NewFact` emits known fields as NULL with reasons; typed nil/JSON null normalization preserves computed[]/0. Four source clocks remain distinct; capture time added at save. Batch Save validates then commits transactionally; malformed member rolls back whole batch. Recorder admission is bounded/nonblocking; builders execute on single worker, saves use2s contexts. A stuck builder/warning callback can stall worker and close (timeouts do not preempt Go functions), filling queue and counting drops while trading producer remains nonblocking. P50 measures admission only, not total capture/disk latency. Offer/Close interleaving may admit after stop check; no claim of lossless audit delivery.

[A] Export opens readonly escaped absolute URI, reads receipt membership `[from,to)`, counts NULL-receipt exclusions archive-wide, preserves source clocks and sorted revision set, serializes explicit empty object arrays and objects SHA256. Verify checks expected object counts and objects checksum, not authenticity, complete manifest semantics or rejection of extra object keys. PlanTrace snapshots invocation values and captures actual prompt/config/reply/verdict/publication; published does not equal permission, and composed initial risk stays unknown. Full callback inputs must already be immutable at producer boundaries; this package cannot guarantee producers do not capture mutable objects. No archive contents or coverage counts were read.

[A] Expectancy uses corrected PNL only and sample IDs for realized cells, raw-row reaggregation for condition/session/kind/path/era, Wilson win CI and normal mean CI with n30 floor. Missing condition/link/seam/unresolved rows are separately classified. Optional table read failure warns and loses those optional dimensions. The load query is database-global without trader/user parameter. API-level intended tenancy must be checked by its owning reviewer; this slice does not certify it. AsOf is latest included resolved position, not most recent excluded source row.

[A] `loadArms` keys plan/version/scenario, so multiple fills/rearms with same tuple overwrite without ordering; path/plannedRR attribution can be ambiguous. Excursion hit shares divide by every present excursion row even when exit_reason empty; unknown reason becomes false rather than unknown. `LevelKindFromLabel` longest prefix can accept unrelated trailing text. `FilterEra` recomputes realized cells but retains global exclusions and all E8 side table. E8 buckets omit Rule, preserve first observed rule label while mixing rules, have no row-ID list, and mark unknown/short direction suspect. This is a research labeling concern, not live gate behavior.

[A] Historical Python probes are **not production parity evidence**. Missed-turns uses5-row aggregation regardless of gaps, whole-session future swings as seats, latest plan and final high as proximity proxy. Stale-MET uses full bucket ATR at decision bucket (forming/future constituents), no source/contract filter, naive timestamp host interpretation and no row IDs. Synthetic MPM fixture proves its own high-first versus stop-first construction only. Position-plan repair hardcodes session windows and overwrites duplicate join keys in a map instead of proving exactly one match; writes immediately with no backup. These findings limit reuse of old measurements, not invalidate every historical result wholesale.

### Validation boundaries and remaining uncertainty

[A] Assigned claim/lock/mutation selftest sources were fully read. Narrow dependency reads cover store initialization, auth expiry, API decrypt guard, live telemetry producers and unit templates. Circuit-breaker test source fully read: it checks one allowance after cooldown but never reserves/tests exclusivity of that probe; `Allow` actually permits all callers after cooldown. Retry cancellation is checked only between failures, so an already-canceled context can still run first attempt. No tests run means no new PASS/FAIL claim.

Additional test files were discovered by names (not fully reviewed): calendar outage/filter, expectancy hand-computed cells/row IDs/era/minN/E8, research NULL/four clocks/admission/relative startup/wiring, logger WARN sink, gate metrics and discipline tests. Telegram agent tests were read in excerpts only. Root owns merged-head validation; this review does not duplicate its suite or infer green from test existence. GitHub push reportedly showed five dependency alerts (2high/2moderate/1low), uninvestigated; dependency manifests/advisories were not assigned and no dependency-security clearance is claimed.

[A] July10 graph subset contains154 nodes touching39/95 assigned files and981 incident edges; it is historical, not current. Read node summaries and selected runtime edges conflict with source: telemetry called opt-in/installation-only (false); Telegram API response called truncated (unbounded); crypto described partial keys, AAD identity validation and plaintext passthrough (not what these functions do); hot reload described wired (not in running loop); logger config described format field (absent). `graph.json` records corrections and evidenced current boundaries. CGC is historical per root; no fresh CGC query/reindex performed. AST calls are expressly syntax-only and not receiver/type resolution.

No runtime account bindings, NT8 build, deployed binaries, JWT value, current risk settings, broker truth, backups, user/strategy data or external payloads were inspected. The inventory describes code at one commit, never the safety or profitability of a live deployment.

### Complete assigned-file coverage

The following notes are from full source reading; exact function boundaries and local callees are in `functions.json`.

- **.github/workflows/scripts/calculate_coverage.py** (1–192): Go coverage parser uses authoritative total but unweighted average of function percentages per package; generates raw cover report and GitHub outputs.

- **.github/workflows/scripts/comment_pr.py** (1–246): Coverage PR comments via GitHub requests; first-page bot marker match; no timeout/pagination; fork JSON failure defaults non-fork; advisory only.

- **.husky/_/husky.sh** (1–36): Husky hook wrapper honors HUSKY=0, sources user rc, invokes same hook under sh -e, propagates exit.

- **branding/branding.go** (1–15): Embedded display-only product/persona text without trim.

- **calendar/calendar.go** (1–203): Weekly FF feed 10s/5MiB; high/medium currencies and CT dates; invalid event dates silently skipped, valid empty feed no fallback, timezone fallback UTC.

- **cmd/arm-state-sql/main.go** (1–19): Prints canonical store terminal/nonterminal arm SQL; no DB opened.

- **cmd/bars-export/main.go** (1–98): Export ladder bars to CSV uses store.New (migrations despite readonly claim), unfiltered BarsBetween, no source/contract columns or CSV write-error checks.

- **cmd/dayplan-arm/main.go** (1–83): Dry-run strategy arm preview; non-grid strategies, day-plan defaults; confirm persists full codec; store.New still mutates on preview.

- **cmd/dayplan-level-repair/main.go** (1–85): Burned-level repair cutoff preview; confirm ResetBurns to C; store.New initialization happens before confirm.

- **cmd/dayplan-sessions/main.go** (1–192): Existing day-plan session overrides tighten grades/caps unless allow-loosen; no validation of arbitrary grade/negative cap; roundtrip checks parse only; store.New preview side effects.

- **cmd/decisive-test/main.go** (1–164): Diagnostic hardcoded historical decision IDs/model: readonly GORM wrapper then direct DeepSeek network call after stripping prompt sections; not offline.

- **cmd/detector-report/main.go** (1–58): Detector readonly URI still invokes migrating constructor; sensitivity flag only prints text, does not recompute.

- **cmd/excursions/main.go** (1–60): Excursion report or explicit date backfill; UTC date parse, optional trader/symbol; constructor migrations; no built-in backup.

- **cmd/gate-jwt/main.go** (1–68): Local JWT mint matches dotenv→config→auth path but opens migrating store, logs may precede token.

- **cmd/levelstats-backfill/main.go** (1–103): LevelStats backfill writes by covered days and hardcoded fallback trader; unfiltered MNQ bars coverage; errors on summary ignored.

- **cmd/nq_smoke/help.go** (1–20): Smoke help documents legacy Databento/CSV matrix, omits tcp.

- **cmd/nq_smoke/main.go** (1–174): Legacy default fetches96h-old NQ window then stdin decision→CSV signal→30s fills; unknown subcommand falls into this path.

- **cmd/nq_smoke/smoke_all.go** (1–28): Runs databento,resolver,prompt,roundtrip, excludes tcp and lists skipped components as ran.

- **cmd/nq_smoke/smoke_databento.go** (1–59): Credential-dependent Databento historical shape check >=40 bars; first bar OHLC unchecked.

- **cmd/nq_smoke/smoke_prompt.go** (1–55): Offline future prompt construction, keyword omissions WARN only.

- **cmd/nq_smoke/smoke_resolver.go** (1–40): Network resolver regex root+quarter+single-digit year; credential absence skips.

- **cmd/nq_smoke/smoke_roundtrip.go** (1–61): Temp CSV/mock roundtrip; any fill passes without comparing fields.

- **cmd/nq_smoke/smoke_tcp.go** (1–81): Loopback ephemeral test TCP server/mock client and signal; any fill passes; close-then-bind port race.

- **cmd/planner_ab/main.go** (1–162): Provider A/B called offline means outside live loop, actually network+credential use and migrating store; unchecked argv; hardcoded DeepSeek endpoint; no HTTP timeout; schema only.

- **cmd/research_export/main.go** (1–46): Actual readonly archive exporter RFC3339 receipt [from,to), verifies checksum/counts and writes stdout.

- **cmd/sandbox-seed/main.go** (1–285): Sandbox seed only substring path guard (not symlink identity), initializes/mutates multiple tables; appends plans/trades despite idempotence claim; errors often ignored.

- **config/config.go** (1–277): Loopback API defaults, insecure JWT warning fallback, permissive numeric env parsing, per-process telemetry initialization; strategies own trading knobs.

- **crypto/crypto.go** (1–469): RSA OAEP browser envelope and AES-GCM ENC:v1 storage; optional timestamp; EncryptedString silently falls back on errors; key decoding hashes nonstandard lengths.

- **deploy/fix-wsl2-clock.sh** (1–78): Root chrony installer replaces config, enables service, attempts makestep, installs cron fallback; enable --now does not restart already-running service.

- **deploy/install-autostart.sh** (1–122): Root installer detects sudo user/node, appends NT_TRANSPORT=tcp, stops instances and renders/enables service units.

- **deploy/install-clock-guard.sh** (1–26): Copies/enables clock user timer, chmod script, starts one immediate measurement.

- **deploy/install-db-backup.sh** (1–23): Copies/enables backup user timer; requires existing user session/linger setup.

- **deploy/install-journald.sh** (1–34): Root journald persistence installer, vacuum-size 2G and effective config printing.

- **deploy/leveltruth-cutover.sh** (1–52): Historical hardcoded cutover revision; DB position and journal substring checks; writes RELEASE before binary validation, no lock/full flat gate/rollback, boot match unbound to new process.

- **deploy/vl-claim-test.sh** (1–56): Claim regex source-derived table pins for real malformed/routable messages; no execution.

- **deploy/vl-claim.sh** (1–106): Claim regex validates routable session; remote branch presence check, branch/empty commit/push; check/audit read local remote tracking refs.

- **deploy/vl-clock-guard.sh** (1–80): Root-free RTC/NTP/Windows drift detector, atomic JSON state; only RTC determines CRITICAL, unknown RTC still OK.

- **deploy/vl-db-backup.sh** (1–79): Online sqlite backup, quick_check, gzip/move, first ISO-week promotion, retention; no trap removes partial files, no locking or retention input validation.

- **deploy/vl-lock-test.sh** (1–493): Lock tests read fully; compressed keeper timing, refusal/expiry/group ownership/removal/incomplete pins; historical heartbeat-path assertion reads missing file; no execution.

- **deploy/vl-lock.sh** (1–525): Atomic mkdir lock and bounded detached keeper; heartbeat/session ownership, release group targeting, incomplete states; with-heartbeat adds separate writer; reclaim retains expiry and no new keeper.

- **discipline/freeze.go** (1–73): Mutex-protected in-memory trader freeze latch; first reason wins; clear owner API boundary; state gone on restart.

- **discipline/reentry_cooldown.go** (1–119): Mutex-protected stop-loss cooldown per trader/symbol/normalized side; earlier timer or absolute ATR move unlock; read/delete separate critical sections.

- **expectancy/aggregate.go** (1–693): Expectancy load positions corrected PNL, optional arms/excursion/counterfactual joins; atom-based rollups and era filter; n>=30 normal mean CI gate; E8 keys omit rule; globally scoped database query.

- **expectancy/levelkind.go** (1–77): Kernel kind vocabulary; decorated label longest prefix recovery can accept unrelated suffixed text; no dedicated unknown-kind counter here.

- **expectancy/model.go** (1–240): Typed realized/counterfactual read model, nullable optional stats, MinN30, fixed historical CT era; rows retained for reaggregation.

- **hook/hooks.go** (1–40): Global unsynchronized hook registry; generic return assertion can panic; disabled/missing hooks return nil.

- **hook/http_client_hook.go** (1–23): HTTP hook result wrappers log error and return client anyway.

- **hook/ip_hook.go** (1–19): IP hook result wrapper logs error and returns IP anyway.

- **hook/trader_hook.go** (1–42): Binance/Aster hook result wrappers log error and return client anyway.

- **install-stable.sh** (1–106): Legacy upstream stable Docker installer overwrites compose, creates keys/ShanghaiTZ if absent, no chmod/env validation, starts services.

- **install.sh** (1–294): Legacy upstream main Docker installer overwrites compose; optional unscoped trade-table deletion no backup, image pull/restart; curl health accepts HTTP errors.

- **internal/retry/circuit_breaker.go** (1–69): Circuit breaker cooldown returns Allow true to all callers, despite single-probe comment; no half-open reservation.

- **internal/retry/retry.go** (1–38): Exponential 200ms..5s retry, checks cancellation only between failed attempts, no jitter.

- **levelidentity/identity.go** (1–57): Recording identity requires seven inputs and formation-close; SHA256 raw string fields and positive finite bounds, does not canonicalize or order bounds.

- **logger/config.go** (1–13): Logger level defaults info.

- **logger/db_sink.go** (1–82): WARN+ logrus hook injected once; atomic sink replacement, trader tag extraction; callback must be nonblocking/nonrecursive by contract.

- **logger/logger.go** (1–212): Logrus stdout plus boot-date data file, no rollover; formatter local entry.Time and discards structured fields in text; wrappers and MCP adapter.

- **main.go** (1–724): Boot: dotenv/logger/config/encryption before stores; migrations and trader loading precede integrity in this base; research sidecar, boot ledgers, agent/API/Telegram then SIGTERM shutdown.

- **railway/start.sh** (1–57): Railway generates ephemeral encryption keys each unset boot, nginx health always200, backend background unsupervised then tail forever; no JWT creation.

- **researchsnapshot/archive.go** (1–286): Separate SQLite WAL archive schema1, transaction atomic validated facts, nullable clocks, receipt-range deterministic export with hash; verify counts+objects hash only.

- **researchsnapshot/fact.go** (1–106): Registered five-object evidence dictionary initializes NULL+reason; typed nil normalized; validates known object/required fields; extra fields accepted.

- **researchsnapshot/plan.go** (1–184): Per-authoring PlanTrace capture snapshots of prompts/attempts/replies/verdicts/publication/model config; publishes scenario evidence and explicitly unknown actual permission/risk.

- **researchsnapshot/recorder.go** (1–216): Nonblocking128-capacity record queue, 5ms admission check; builders/SQLite on worker,2s save timeout, counted drops, p50 admission histogram; builders themselves not bounded.

- **researchsnapshot/runtime.go** (1–147): Atomic global recorder; archive-start fail disables capture, boot counts by CT captured date and reports UNKNOWN; contained producer panics.

- **safe/go.go** (1–59): Goroutine panic recovery including callback containment; Must converts panic to error; no restart of failed goroutine.

- **safe/io.go** (1–29): Reader limit+1 detects oversized body, default10MiB.

- **scripts/arm_state.py** (1–31): Python caches canonical Go arm SQL and evaluates states in memory SQLite, no handcopied enum.

- **scripts/backfill-position-plan.py** (1–125): Immediate DB migration/backfill by row id; no backup despite unused shutil; fixed CT session windows; plans dict overwrites ambiguity instead of proving exactly one.

- **scripts/leveltruth_missed_turns.py** (1–103): Historical missed-turn estimator fixed-CDT dates and hardcoded trader/session; row-count m5 grouping, full-session future swing seating against last high, not causal production evidence.

- **scripts/mpm_resolution_fixture.py** (1–62): Synthetic OHLC aggregation ordering example; local TP-first vs SL-first assumptions; proves fixture inversion only.

- **scripts/mutate-selftest.sh** (1–93): Mutation selftest builds throwaway module with no-match/build-fail/killed/survived/no-tests and restore cases.

- **scripts/mutate.sh** (1–103): Mutation file backup/sed/cmp/build/test/restore; broad FAIL catches infrastructure errors, ignores nonzero RC without FAIL and [no test files]; trap restore on signal does not explicitly exit.

- **scripts/sandbox-down.sh** (1–11): Stops sandbox via path regex and globally matching vite.sandbox config; ports are system observations not owned-process proof.

- **scripts/sandbox-reset.sh** (1–9): Stops sandbox then removes fixed sandbox db/WAL/SHM and calls up; up reads live DB despite never-open claim.

- **scripts/sandbox-up.sh** (1–70): Copies live DB readonly into sandbox then partial scrub; keeps credentials, ignores scrub errors, existing DB not rescrubbed; same cwd dotenv loaded, synthetic mode not isolation proof; listener check still prints READY on timeout.

- **scripts/stale_met_replay.py** (1–106): Historical stale-MET replay read-only SQLite; fixed epoch buckets can use forming/future constituents at decision time, no contract/source filtering or row IDs.

- **security/url_validator.go** (1–228): SSRF scheme/name/IP checks and redirects; safe dialer re-resolves checked hostname instead of pinning IP.

- **start.sh** (1–422): Docker lifecycle wrapper; missing-key creation and forced rotation, local env permissions600, pull/update/clean/restart independent of current lock/flat gates; compose detection malformed command -v.

- **telegram/agent/agent.go** (1–286): Per-chat LLM tool loop10 iterations: arbitrary authenticated local API verbs/paths, first-turn account snapshot; no local action allowlist, max iterations falsely says completed; token minted through24h auth.

- **telegram/agent/apicall.go** (1–88): Localhost API tool, authenticated JSON request,30s timeout, no path/method allowlist and unbounded response, errors rendered into model context.

- **telegram/agent/manager.go** (1–79): Per-chat semaphore60s wait, agents/lanes never evicted; Reset bypasses lane (memory race).

- **telegram/agent/prompt.go** (1–100): Prompt embeds docs/user identity and immediate-action workflows, GET-after-write is instruction not enforcement; stale crypto default examples.

- **telegram/bot.go** (1–461): DB token, first DB user and first /start chat binding, command handling and LLM responses; running bot never receives reload; JWT not renewed for unchanged user; /lang before ACL.

- **telegram/session/memory.go** (1–105): In-memory conversation summary at3000 rough tokens; appends new summaries to old summary (growth), captured initial LLM, no locks; ResetFull clears both.

- **telemetry/bar_horizon.go** (1–50): Process-local metrics/counters or outbound GA telemetry; exact function semantics covered in functions.json. Session resets depend on callers; unknown inputs generally ignored. Horizon counters atomic and non-resetting.

- **telemetry/errors.go** (1–153): Process-local metrics/counters or outbound GA telemetry; exact function semantics covered in functions.json. Session resets depend on callers; unknown inputs generally ignored. Error aggregator holds mutex while announce callback executes; lastOccurred unused.

- **telemetry/experience.go** (1–242): Process-local metrics/counters or outbound GA telemetry; exact function semantics covered in functions.json. Session resets depend on callers; unknown inputs generally ignored. GA enabled by default; trade/model/token plus user/trader/install IDs sent asynchronously; response status ignored, no bounded queue.

- **telemetry/far_arms.go** (1–39): Process-local metrics/counters or outbound GA telemetry; exact function semantics covered in functions.json. Session resets depend on callers; unknown inputs generally ignored. Far-arm denominator increment separate from side counter; comment says unknown side counts authored but implementation does not.

- **telemetry/gate_blocks.go** (1–146): Process-local metrics/counters or outbound GA telemetry; exact function semantics covered in functions.json. Session resets depend on callers; unknown inputs generally ignored. Gate counters roll on any differing day, counts include observational/shadow names; snapshots copy state.

- **telemetry/metrics.go** (1–72): Process-local metrics/counters or outbound GA telemetry; exact function semantics covered in functions.json. Session resets depend on callers; unknown inputs generally ignored. Prometheus promauto registry labels trader/action/status and gate; latency default buckets stop10s.

- **telemetry/planner_wave.go** (1–19): Process-local metrics/counters or outbound GA telemetry; exact function semantics covered in functions.json. Session resets depend on callers; unknown inputs generally ignored. Repair regression counter process-wide; trader argument unused.

- **telemetry/shadow_conditions.go** (1–18): Process-local metrics/counters or outbound GA telemetry; exact function semantics covered in functions.json. Session resets depend on callers; unknown inputs generally ignored. Atomic shadow-arm refusal counter.

- **telemetry/weekly.go** (1–78): Process-local metrics/counters or outbound GA telemetry; exact function semantics covered in functions.json. Session resets depend on callers; unknown inputs generally ignored. Weekly shadow counters describe daily reset but only test reset present; reserved day unused.

- **wallet/balance_cache.go** (1–67): 30s normalized address cache; double-check per-address mutex; no eviction of mutex/key maps; backend called with untrimmed original address.

- **wallet/usdc.go** (1–105): Base USDC eth_call and float conversion; display helper maps errors to0.00, missing result to real zero; no status/body/address validation.

---

<a id="section-25"></a>

> Original: [reviews/20/report.md](reviews/20/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 20 — trader admission and orders

Source-only audit at `63968be62e44db2fb07a92883e02127b9064b0be`, isolated `/tmp/vl-understanding-surfaces-20260913`, initially clean. All **26 assigned files / 7,375 lines** fully read. Named-function census: **217 declarations**; callbacks grouped with enclosing declarations. No tests executed, production state inspected, settings changed, broker calls made, or source edits made. [A] means exact source inspected; [B] means consequences inferred from those paths. No finding below claims a runtime incident occurred during this audit.

Rules read: supplied/root AGENTS, tracked CLAUDE-canon, AUDIT-CHECKLIST initial classes and pre-audit R1–R10 / pre-cutover portion, class 121, relevant SYSTEM-MAP and rulebook sections. `trader/AGENTS.md` is absent in this worktree. Root owns branch claim and repairs. Spec freshness records, both at reviewed base: `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap` for SYSTEM-MAP.md and VL-TRADING-RULEBOOK-v1.md. Older narrative and embedded incident anecdotes were treated as historical claims, not fresh evidence. Current owner ruling is daily loss; no mandatory per-trade cap is proposed.

### End-to-end source model

[A] Startup calls `LoadTradersFromStore` at main.go:251. The manager builds traders and starts `Run` in a goroutine when stored IsRunning is true (manager/trader_manager.go:563, 716–727). Run immediately invokes tickOnce (trader/auto_trader.go:973). The cycle builds context and structure, then invokes `maybeManageArmedOrders` (auto_trader_loop.go:432), ahead of the decision model result and decision execution gates.

[A] `maybeManageArmedOrdersAt` (armed_executor.go:197–885) requires enabled day plan, store and NT exchange; boot-sweeps old rows, drains updates, resolves active plan/lifecycle, retires superseded unplaced versions, resolves bars/ATR/config, and evaluates session risk once. It computes one-setup verdicts and retires explicitly declined unplaced rows. Each scenario/leg obtains condition-derived order kind; reject-fade legs use frozen geometry, other plays retain authored/anchor/ATR widest-stop composition. Quality, bias, arm validity, R:R, HTF, open-position, canonical EntryGate, and one-setup admission precede UpsertArm. Shadow observations and split-sibling logic precede a separate placement pass.

[A] `runArmedPlacementAt` (:1174–1331) reads all nonterminal rows for the trader, evaluates account commitment once, then consumes rows whose state is armed. Limits require acceptable side and proximity; stops require seam and retest conditions plus stop-side adjudication. Both use slot/account guards and register BeginPlacement before sending through TCPTrader. Trailing passes reconcile stale rows, drain events, confirm placements and cancellations, record boot reconciliation and clear recovered-book alerts. The placement pass does not rerun EntryGate or the scenario authoring gates.

[A] A requested placement is place_pending until a received entry frame or fresh live book proves it. A cancellation normally enters cancel_pending, retains its slot and settles against a persisted fresh snapshot, preserving its ID as evidence. Entry fills materialize positions and attach plan lineage. Cancellation timeout keeps pending rather than inventing success. These are separate states and separate evidence requirements.

### Findings and repair boundaries

#### S1 — boot refusal does not bind ARM entry, and startup assertion is late

[A, BROKEN source contract] The only production `kernel.TradingRefused` call found is auto_trader_orders.go:205, conditional on decision open_long/open_short. No call exists in EntryGate, maybeManageArmedOrdersAt, runArmedPlacementAt, placeOneStopEntry, TestArmPlace, TestArmPlaceStop, or the inspected TCPTrader limit/stop entry methods. The default atomic is false until assertion. main.go:289 asserts after the load/autostart chain above, contradicting its own “before any trader cycles” comment.

[B] A process whose assertion refused still has an ARM path to an entry if its remaining gates permit. Stored IsRunning creates an ordering race even for the decision path. This is a proven source path discrepancy, not a demonstrated unwanted order.

Repair boundary: moving AssertBootIntegrity before LoadTradersFromStore closes startup ordering. Its direct dependencies are build metadata, release expectation and self-contained embedded golden fixtures (kernel/boot_integrity.go:122, golden_selfcheck.go:34–125), with no loaded-trader requirement identified. **EntryGate alone is insufficient**: existing armed rows are consumed later independently. An admission check must reach the armed-state branch in runArmedPlacementAt and direct test seam entries, while retaining trailing settlement/cancel/protect/exit management. Adapter-only checks would cover direct seam calls but require dependency/call-site review. Existing kernel/boot_integrity_test.go tests the latch and assertion, not either actual entry path.

#### S2 — terminal rows with a signal bypass manual-cancel-wins

[A, BROKEN] `store/armed_orders.go:291–307` creates a successor for terminal signal-bearing rows and returns before the same-version/manual-cancel check at :345. Thus the later check only protects terminal rows without signal IDs. The production caller (armed_executor.go:798–839) does not find terminal rows in ListNonTerminal and calls UpsertArm again for the same current scenario/version.

[B] Owner-cancelled or filled broker placements can be reauthorized under the same version, contrary to SYSTEM-MAP re-arm law. Existing working/place_pending and cancel_pending branches do correctly refuse replacement. Book/account guards still constrain immediate simultaneous execution; they do not restore same-version stickiness after a position/order is gone. Root was given exact ordering for repair.

#### S3 — boot sweep treats successful send as broker cancellation

[A, BROKEN] class33_boot_sweep.go:91 sends through CancelOrder and :100 writes terminal cancelled immediately on nil error, without a received cancel event or book. Its counter/log use CANCELLED. It also runs before initial fill drain (armed_executor.go:210 vs :228). Existing cancel_pending rows are excluded from ListPreBoot; `TestArmSweepLeavesPendingCancelForSnapshotConfirmation` proves only that exclusion, not the unsafe working/place_pending branch.

[B] A stale working row can become locally terminal while its broker order remains live or has filled. Guards reading fresh broker state reduce successor stacking, but the claimed settlement/evidence is still false. Current C# HandleCancelOrder is entry-only, so this finding does **not** establish removal of an existing protective stop with the current AddOn source.

#### A1 — stale book accepted by cancellation safety

[A, BROKEN freshness contract] arm_cancel_safety.go:174 and :189 discard liveBook's age. `liveBook` returns haveBook=true for any existing cache snapshot. A stale empty or entry-resting book can allow cancellation, whereas slot/account/reaper/settlement functions reject old evidence. Helpers also return true after send errors (:180–183), and the cancel re-request callback discards refusal/error then returns nil (armed_executor.go:1321–1325).

Negative result: current C# HandleCancelOrder (:1735–1818) never reads placedBrackets and only cancels workingEntries, protecting already-created brackets from this command. Whether that source is deployed was not inspected. Therefore historical stop-loss-removal narratives cannot be used as proof of the current risk. Separate AddOn issue to verify: it removes workingEntries before Cancel succeeds and unconditionally removes pendingBrackets even if cancellation fails or races a fill; root informed.

#### A2 — authorization refusals do not uniformly invalidate old authorizations

[A] oneSetupVerdictsAt panic recovery empties verdicts (:109–113); consult fails closed on missing verdict (:234), but oneSetupRetireDeclined skips missing verdicts (:379). Existing armed rows remain eligible for placement. Ordinary gate refusal cancellation loops similarly operate on signal-bearing rows; unplaced authorized rows can survive later quality/invalidation/entry gate refusal. Geometry refusal has a stronger explicit retirement path and stops the cycle if retirement fails (structural_geometry.go:244; armed_executor.go:484–499).

[B] The intended fail-closed one-setup/error behavior does not cover inheritance in all paths (AUDIT-CHECKLIST class 121). Conditions and fixture needed: existing armed row, current missing/failed verdict, fresh empty broker book, flat account, price in band. Existing one_setup_retire_test drives explicit decline, not missing verdict, and structural_geometry_wire_test correctly covers geometry retirement write failure. This warrants a production-callsite test before repair design.

#### A3 — stop outcome erased before pass bookkeeping

[A] `placeOneStopEntry` returns void (:1548) on no-op, through-cancel, slot refusal, old AddOn and send failure. Its caller (:1259–1270) always sets placedThisPass=true and cancels other arms in the plan. [B] No placement can thus retire sibling authorizations and log “reached the wire.” Stop seam/condition gates constrain reachability; no runtime seam setting was read. Return an explicit attempted/sent outcome if repairing; distinguish a failure before registration from uncertain socket outcome.

#### B findings / bounded unresolved issues

- [A] cancelSettled uses age but no cancel-request/placement timestamp. confirmPendingCancels can cite an empty snapshot still within freshness bound but older than the order request. [B] Absolute freshness does not prove causal absence. Request-time evidence ordering deserves a dedicated test.
- [A] entryGateForArm and entryGateForDecision test p.Side against lowercase strings (:365/:430), while position storage is canonical uppercase and GetOpenPositions does not normalize (store/position.go:653). Canonical EntryGate position input can be empty. Negative: arm legacy oneLiveArmGuard checks matching symbol regardless of side; oneContractGuard separately reads broker positions. Thus this is a shared-gate inconsistency, not proof ARM can add to a position.
- [A] cancelSplitSiblingOnStopOut starts from ListNonTerminal but then searches those rows for state filled (:1076–1083); filled is terminal. [B] Filled sibling is unavailable, making the cancellation path inert under current classifier. One-contract policy presently also restricts split entry usage.
- [A] production authoring creates a fresh row with State armed and only copies existing.ID (:792–805). Later churn branch checks row.State == working (:853), which cannot be true for that constructed row. Store correctly refuses working-row rewrites; error is ignored. [B] Bracket modification churn code is inert, although durable working prices are protected.
- [A] fadeFactsAt marks IB complete after 12 available session bars without verifying contiguous first-hour buckets (:97), unlike OR's exact opening-bar requirement. BackfillFadePermission uses live FuturesBarsProvider's latest 400 5m bars for a past time; older history becomes unavailable despite store bars. Calendar/map backfill coverage is partial, not full reconstruction.
- [A] BackfillOneSetupVerdicts chooses final close from a window ending openedAt+60s (:174–182) and forces BandPts=0, while production uses a daily-ATR reachability band. [B] “recomputed” does not demonstrate identical point-in-time production input; final minute can include information after episode open. Historical research must label this difference.
- [A] NoChase boot prints ATR stop-floor run ceiling even when NOCHASE_MAX_RUN_PTS overrides runtime. lastTouchFor returns the cited level itself, so known run distance equals distance-to-level, not a separate elapsed excursion measure. No-chase and far-arm remain observation-only.
- [A] live cache order reads and separately fetched persisted snapshot IDs can differ; placement/slot diagnostics may cite an ID that did not supply the actual live orders. Cancel settlement exclusively uses persisted snapshot, avoiding this particular mismatch.

### File ownership and coverage

The four artifacts attach every assigned file and named declaration. Files group by actual role:

- Admission/placement: armed_executor, entry_gate, arm_kind_compose, arm_stop_anchor, structural_geometry, invalidation_resolver, one_setup_wiring, one_contract.
- Broker evidence/lifecycle: arm_cancel_safety, cancel_confirm, place_confirm, reaper_snapshot.
- Clock/cadence: discard_burn; its deferred kicks and stop/drift reevaluation concern paid decision calls, not ARM execution.
- Research evidence: fade_facts, fade_stamp_wiring, fade_boot, follow_plan_wiring, scenario_anchor, scenario_level_identity, one_setup_boot. These write evidence/episode records; the follow recorder never reaches an arm or wire. Fade predicate also feeds live one-setup admission, so older “LABEL ONLY, no refusal” narration describes the stamp alone, not all consumers.
- Observability: arm_far_counter, no_chase, arms_boot_line, fade_surface, scenario_economics_desk, structural_geometry_boot.

Configuration binds to the trader's strategy, with individual session overrides and explicit env resolvers. Structural reject geometry freezes zone provenance, buffer, first distinct profit-side zone and costs before admission; nullable unavailable values remain distinct. It reports one-contract loss but does not impose a removed extra per-trade cap. Legacy nonreject stops still widen according to anchor/ATR rules. Stop placement seam and AddOn capability remain separate from condition-kind availability.

### Tests and historical graph

Read fully: cancel_confirm_test, one_setup_retire_test, structural_geometry_wire_test, arm_sweep_settlement_test and kernel boot integrity tests; arm_cancel_safety_test read through the ordering test. Exact source ranges appear in reads.json. Tests establish intended contracts and useful isolated wire harnesses; this audit did not run them or claim they pass. Root should use actual production entry/cancel call sites for repairs rather than pure predicates alone. New tests should cover both directions and preserve cancellation, placement settlement and protective management while refusing entries.

Historical Understand Anything graph at July10@7a8adce0 contains **zero nodes and zero incident edges for these 26 paths**. This is absence of newer subsystems in that graph, not absence of current functionality. Current AST catalog supplies 217 named declarations; call expressions are explicitly syntax-only and not type-resolved. CGC service was not queried by this worker; root owns exported historical index evidence. Current source is authoritative.

---

<a id="section-26"></a>

> Original: [reviews/21/report.md](reviews/21/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 21 — planning and context interfaces

**Complete:** 15/15 assigned files, 6,816 lines manually read in full; no unread assigned source. **197 named functions/methods** cataloged with exact boundaries, syntax-only call expressions, purpose and invariants in `functions.json`. Closures belong to enclosing functions; file-scope clock/provider callbacks are described with their owning file. Baseline `63968be62e44db2fb07a92883e02127b9064b0be`, isolated execution worktree `/tmp/vl-understanding-execution-20260913` verified clean. No source/config/account/database modifications; no live data, credentials or services accessed; no assigned function/test executed. Artifact generation parses source and verifies hashes only.

[A] = directly inspected current source. [B] = consequence inferred from that source. Findings are static code/interface defects or explicit limitations, not reproduced runtime incidents. Existing comments' historical counts/boots are not fresh evidence. This review does not certify current broker state or profitability.

### Rules and graph provenance

Common review instructions were reread. AGENTS/CLAUDE canon had been read in this worker's prior assignment; applicable audit R1–R10, SYSTEM-MAP research archive boundary, and current RULEBOOK structural-stop/daily-loss sections were read again as recorded in `reads.json`. The expected `trader/AGENTS.md` does not exist in this worktree; no dangling path was treated as an instruction.

Spec freshness (`git log -1` at the review pin):

- AUDIT-CHECKLIST: `dfda15e1 2026-09-13T00:48:28-05:00 test: isolate session clock fixtures from weekly backfill workers`.
- SYSTEM-MAP and RULEBOOK: `565e8fbe 2026-09-13T00:39:02-05:00 fix: use owner daily-loss controls without requiring a per-trade cap`.

[A] RULEBOOK:195 explicitly says the configurable loss limit is DAILY and no additional per-trade cap is required. Nothing in these 15 files implements a new mandatory per-trade dollar cap. Session entry-count limits, consecutive-loss breaker, reauthor-call budgets and daily-loss switches are different controls and should not be conflated. No recommendation here restores the removed per-trade cap.

[A] Historical Understand Anything graph (`2026-07-10`, `7a8adce0`) has **zero nodes/edges for the 15 assigned file paths**. `graph.json` preserves that negative result and adds exact current source edges. The old CGC index is not presented as current. Root Go AST symbols/calls are only a declaration census and syntax-level call list, not type-resolved or runtime reachability proof.

### Source flow and ownership

[A] **Scheduling:** `auto_trader_clock.go:87–125` invokes weekly/session reads before bar-dependent cycle skips. Weekly document precedes Sunday ASIA (`planner.go:193–255,1452–1459`); each trader/session/day has a process-local claim (`:828–843,867–872`). Separate sessions can run concurrently. `runCycle` separately performs calendar capture (`auto_trader_loop.go:196`), frozen session-profile persistence (`:262`), digests (`:283`), level/scenario observations (`:298–303`) and weekly reaction summaries. While holding, equity/context are saved before the watch-only branch (`:460–480`). Calendar's closed-hours claim must be distinguished from the weekly/session wall-clock path: its only inspected production caller remains inside runCycle.

[A] **Planner input:** `assemblePlannerInputWithCtx` (`planner.go:2143–2527`) reads root-symbol bars, HTF levels, owner-scoped sticky levels, scored pool, current regime, digests, calendar, indicator/config fingerprint and weekly references. Store-deepened1m tape is shared by candle tables, RV baseline and completed-week count (`:2275–2304,2393–2406,2458–2463`). Machine zones and identity candidates accompany authored text; research snapshot UUID ties input/candidates to the authoring trace. Model chooses scenarios; gates judge them.

[A] **Authoring:** `runPlannerReadWithTriggerClaimedCtx` (`:863–1020`) claims, clock-checks and preflights before resolving client and rendering input; model calls use stream idle/total deadlines for base clients and legacy full-body fallback otherwise. `runPlannerReadCoreObserved` (`:1480–2024`) attempts at most3 calls: transport failures resend identical text; validator failures repair or reauthor with reasons. Schema/caps, required bias, structural-label provenance, facts, FVG/breakdown rules and authored scenario economics are validated. Advisory feasibility/role/fantasy-target warnings do not reject. Scheduled/death/owner failures append NO-TRADE; opportunistic wake failures keep prior plan. Append and recorded spend are separate writes (`:1979–2012`).

[A] **Publication and consumption:** model/indicator/config hashes and attribution are persisted with the plan; owner edits carry by price identity (`:395–478`). `installActivePlanProviderAt` (`:2713–2786`) serves this trader's runnable current-session latest active plan, resolved with overlays, alongside stored remaining budget. Bad overlay validation warns and falls back to base (`:2794–2825`). Plan ID's `StrategyID` database field intentionally contains trader ID (`:2645–2647`); it is not evidence of an erroneous join by itself. Root-symbol argument is not an independent selection key in the active-plan closure because the trader/session chain owns the plan.

[A] **State and risk:** level state is trader/symbol/type/bin scoped; scenario status and metadata are trader/plan/version scoped (`auto_trader_levelstate.go:92,238,265`). Session first/lunch/news bands and optional account-scoped entry-count limits feed the arm risk gate (`auto_trader_session.go:35–54`; `session_risk.go:120–137`; `armed_executor.go:336–350`). Breaker is trader/day scoped and deliberately fail-open on query failure; unknown corrected outcomes break a known-loss streak (`store/position_query.go:92–133`). Daily-loss enforcement is outside this slice; boot labels decorative if master or daily leg is off (`session_risk.go:143–166`).

[A] **Observability:** weekly reasoning is references-only, with shadow bias and no directional decision authority (`auto_trader_weekly.go:260–301,426–489`). Watcher has no broker calls (`auto_trader_watcher.go:243–445`), ignores action-like AI fields and writes assessment records. StageA adapters preserve explicit missing costs and strict corrected P&L, separate observation/receipt/publication/permission, and state that placement return is not a broker acknowledgement (`research_snapshot.go:103–159,164–224,273–306`). `researchsnapshot.Record` queues builders; worker evaluates them later (`recorder.go:45–80,99–117`). Local slices/pointers passed to adapters therefore rely on the documented transfer/immutability convention; primitive outcome/placement fields are explicitly copied. No snapshot race was reproduced.

### Findings

#### 1. Planner arm instructions diverge from structural reject-fade execution [A/B]

[A] `planner.go:2485–2488` supplies ATR floor fields, and `kernel/planner_prompt.go:798` instructs every arm to meet an ATR minimum stop distance; otherwise omit the arm and let the AI path enter. The same prompt `:795` states resting orders are the only entry path. Current structural-aware composer `trader/arm_stop_anchor.go:76–88` delegates reject-fade geometry to the structural core instead of the legacy widest-ATR branch. RULEBOOK:193–197 explicitly removes ATR override for usable structural invalidation. Strict mode refuses non-arm entries (`entry_gate.go:184–197`).

[B] The planner can omit or distort otherwise structurally valid reject-fade arms because its instructions describe a different execution contract and suggest an unavailable strict-mode alternative. This is source-proven prompt/contract drift; no model response or rejected trade was produced here. It is unrelated to requiring a per-trade cap. `planner.go:1694–1701` also retains generic ATR feasibility warnings, and `:1977` calls zones presentation-only despite their downstream structural role.

Parent follow-up (not independently verified by this worker): root reports the structural prompt mismatch above was reproduced and fixed in repair `710ea1c8`. This report remains a review of baseline `63968be`; do not rediscover it as an unfixed repair-branch issue.

#### 2. MSS wake bypasses advertised full cadence controls [A/B]

[A] `class47_wake_cadence.go:121–126,220–224` names both `level_event` and `structure_mss` as governed. The level caller actually runs cutoff/cooldown/fast-market and any-stream checks (`auto_trader_wake_levels.go:289–360`). MSS caller (`auto_trader_transition.go:156–212`) performs only event dedupe and shared minimum-attempt interval, then calls the claimed read. Full read `planner.go:863–1020` has no class47 cadence check. Current source search found no additional production SkipForCutoff/SkipForCooldown/anyPlannerStreamOpen callsites.

[B] A fresh MSS can start a read too near flat, inside the written-version cooldown, or beside a different session's planner stream even while boot prose says otherwise. Per-chain claims still prevent duplicate same-chain calls; this is not a claim that every stream overlaps. `class47_wake_cadence_test.go:306–317` tests trigger classification only; `:165–199` tests claim visibility/log wording, not MSS callsite enforcement. No runtime timing scenario executed.

#### 3. Read-only planner-client accessor can clear global research statistics [A/B]

[A] `ResolvePlannerClient` is documented read-only (`planner.go:44–48`) but delegates to model pinning (`:67–136`) and global `dayplan_pinned_model` bookkeeping (`:141–157`). A changed pin calls `MatchedRandom.ResetWindow`, which deletes all rows from both matched-random tables (`store/matched_random.go:108–116`). AskPlanner invokes this accessor before its Q&A call (`api/handler_plan.go:1447`); realignment also invokes it (`:2184`). Neither global model key nor reset is trader-scoped.

[B] Q&A/client resolution is therefore not read-only with respect to learning statistics. Two traders with different model bindings can alternately reset the shared window. Reset and new pin writes are not atomic, and pin is written even after reset failure. No data deletion was performed in this review; these are explicit source paths. Model-change reset itself is intended policy, but its ownership and accessor side effect need visibility.

#### 4. Closing transition drops the identity needed to clear the persisted chip [A/B]

[A] Resumption/expiry reset `at.transition` to an empty struct (`auto_trader_transition.go:82–94`), then `persistTransition(false)` builds another identity-less struct (`:127–137`). `store.TransitionKey:446–451` returns empty for empty plan ID, so no inactive record is written. API reads the old version key and returns any stored Active=true state (`api/handler_plan.go:1241–1257`).

[B] The runtime gate can close while its same-version card chip remains active. Replacement naturally moves the reader to a different version; resumption/expiry on the same version exposes the stale-chip case. Existing transition test checks opening persistence (`auto_trader_transition_test.go:55–72`) but checks only memory/context on closing (`:74–135`). No endpoint or DB reproduction was run.

#### 5. Watcher structure-conflict hysteresis never advances its remembered verdict [A/B]

[A] `runWatchCycle` compares assessment conflict with `st.SCVerdict` and increments/reset counts (`auto_trader_watcher.go:395–406`), but no assignment to `SCVerdict` occurs anywhere in the inspected production source. New states start empty (`:268–271`).

[B] Repeated nonempty conflicts repeatedly reset to count1 and never reach the two-read accepted conflict from default state. This affects advisory badge and combined warning, not order authority. R1/R2/R3 thesis rails are separate and do update `Status`; they should not be described as broken by this finding. `watcher_test.go:138–155` is a source-string no-order-authority guard; it does not exercise this conflict-memory logic.

#### 6. Calendar “fail-closed” is conditional fallback, not closure on unknown coverage [A/B]

[A] Missing/invalid calendar slice loads static events (`auto_trader_calendar.go:172–188`); missing/invalid static file returns nil (`:116–128`). Empty events produce no blackout windows and are returned at `:202`; `currentT1Windows` also returns nil without store/session. No whole-session refusal exists in this function despite fail-closed alert/log wording. Planner input `planner.go:2306–2323` only reads stored slices and does not use that static fallback, although later structured windows do (`:1907–1910`).

[B] If both feed snapshot and static fallback are unavailable, this surface supplies zero news protection; the log can say fail-closed with0windows. With valid static data it is a useful fallback and should not be called wholly ineffective. No calendar outage or current fallback contents were inspected. The planner's initial calendar text and entry gate can also differ during fallback.

#### 7. Digest labels can freeze whole-day totals as separate session totals [A/B]

[A] `maybeWriteDigests` queries one current CME-day total (`planner.go:2557–2561`) before looping every runnable session currently outside its window (`:2563–2591`), and passes that same entries/P&L to `FormatSessionDigest`. SaveIfAbsent freezes whichever total first reaches that session/date key. Errors from activity read are ignored. Daily error digest is appended twice (`:2604–2608`).

[B] Multi-session summaries can include another session's activity or a premature/default total, and then seed the planner digest chain (`:2334–2336`). These are contextual summaries, not the actual daily-loss gate. Exact affected rows, session-chain-date behavior outside the read window, and runtime frequency were not queried.

#### 8. Confirmation grace is not the documented distinct-session retry contract [A/B]

[A] Missing-confirm handling is after the3-attempt loop (`planner.go:1844–1861`), so exhausted grace nulls the otherwise parsed document without giving that defect back to another attempt. Counter key is process-store global (`:2892`); `confirmGraceExhausted` increments per check (`:2896–2906`), with no trader/session identity or dedupe and before append succeeds. A compliant doc immediately exhausts it (`:2911–2915`).

[B] The comments promising distinct sessions and retry-on-reject overstate the contract. Missing confirm may already be refused by narrower upstream schema/economics rules; reachability for a particular scenario shape is unverified here. The explicit grace path itself is not a distinct-session counter.

### Other limitations and negative results

- [A] `session_risk.go:165` always says the breaker never fires on retained max-run7, although `breakerHaltN:62–66` accepts configured thresholds below/equal7. That historical clause is not valid for every resolved knob; no change to owner threshold is warranted. Multi-strategy boot returns n/a (`:265–268`), correctly avoiding arbitrary binding selection, though wording does not distinguish ambiguous from absent binding.
- [A] `matched_random.go:25–48` attributes the closed trade to the latest plan for the entry session, not the position's immutable plan/version; reaction is MFE>MAE, not a sampled random comparator. Weekly snapshots pool by type. This limits statistical interpretation, not execution permission.
- [A] `wakeTimePrice:394–402` and `planner.priceOf:641–645` return final Close without verifying closedness despite comments. Level invalidation collector `wake_levels.go:179–199` checks freshness but not close time>plan birth; it can wake on a recent already-known violation. MSS throttle `transition.go:186–187` uses real time.Since inside an injected-now function. No feed ordering/formed-bar race reproduced.
- [A] Successful weekly docs are cached under mutex; failed weekly calls write no marker (`weekly.go:300–302`), so scheduler retries on later cycles (`:182–212`) instead of only one pair for the whole week. Sunday ASIA remains deferred while no weekly doc exists (`planner.go:1452–1459`), contrary to broad comments that weekly absence affects nothing else. This is recoverable retry behavior but can extend the dependency indefinitely during failure.
- [A] Plan append precedes replan spend (`planner.go:1979–2012`); spend failure is loudly disclosed as potential over-allow. Claim prevents same-chain concurrent provider calls but owner/death budget checks occur before claim; no atomic budget/admission transaction is proven. Same trader can have different-session reads concurrently, with shared `lastRegimeHealth`, `fastTapePending` and possibly primary AI client configuration. Data race/incorrect publication is UNVERIFIED without focused concurrency testing.
- [A] StageA explicitly labels fees unknown (`research_snapshot.go:221`) and activation not permission (`:110,124`). Detector prior episodes are not historical first-availability proof (`:265`). These honest boundaries should be preserved. Snapshot capture never supplies live trading decisions in the inspected callers.
- [A] `sessionHiLoFromBins` leaves high=-Inf on empty input because it checks positive rather than negative infinity (`dayplan.go:153–169`); production caller excludes empty bins (`:123`). This is a dormant helper edge, not an observed bad persisted profile.

### Validation scope

All assigned SHA256 hashes match the manifest. Full/manual ranges cover every assigned line. Go AST census contains197 named declarations; every one has a purpose and boundary in `functions.json`. Anonymous callbacks remain explicitly grouped. Relevant source tests were located; transition test was read fully and targeted watcher/cadence/research snapshot excerpts were read. None was executed. A source review avoids the network/LLM/account activity those production paths can invoke. Suggested next verification is parent-owned minimal offline fixtures for transition persistence, watcher conflict sequence, MSS caller cadence, and model-reset scoping; no new tests or source edits were made here.

### Per-file purpose and coverage

- `trader/auto_trader_calendar.go:1–218` — Calendar producer live3h refresh/hour retry with static fallback, day/session event mapping and drift widening. Missing slice+missing static returns zero windows despite fail-closed wording; no blanket closure.

- `trader/auto_trader_dayplan.go:1–238` — Session-profile persistence and global once-installed nPOC/state providers; active plan pertrader. symbol-only profile identity versus contract-scoped historical touch bars; callback ownership and stale saved profile lineage need checking. Empty bins excluded at caller; helper empty hi=-Inf typo latent.

- `trader/auto_trader_levelstate.go:1–302` — Trader-scoped level durable state/consumption plus plan-version scenario status/meta/death recording; closed birth window and canonical identity; zero statuses returns without clearing prior key; scenario meta best-effort write followed by snapshot recording.

- `trader/auto_trader_matched_random.go:1–89` — Reaction proxy closes matched to latest entry-session plan not immutable entered version; weekly snapshot Sunday CT once ISOweek, counts pooled bytype; no random comparator built here.

- `trader/auto_trader_planner.go:1–3054` — Planning pipeline: pertrader/session claims, bar/clock preflight, model binding, frozen context/maps, three-attempt author/repair/resend validator chain, nonfatal wake versus NO-TRADE failure, append and separately recorded replan spend, overlays/provider/digests. Global model pin resets pooled statistics despite read-only accessor; universal ATR feasibility prompt diverges structural reject path; confirm grace outside retry and global/non-session counter; cached config/current state clocks not fully atomic; digest uses session-day totals for each closed-session label. No mandatory per-trade loss cap in this file.

- `trader/auto_trader_reread.go:1–140` — Owner reread gate session/market/budget and clock hold; rechecks budget then planner claim; alerts spend before actual claim; carry overlays from new version. First read free despite spending wording.

- `trader/auto_trader_session.go:1–176` — Session gate calls configured runnable, account-scoped maxTrades with query error fail-open, first/lunch/T1 shared resolver. Night-edge logs registry alone, may diverge per-session override.

- `trader/auto_trader_transition.go:1–212` — Transition open/resume/expiry state; clears identity before persisting inactive, likely stale card chip. MSS wake uses time.Since despite At clock; only basic throttle here versus full class47 level path; shared read may enforce later.

- `trader/auto_trader_wake_levels.go:1–403` — Level-wake collect/prioritize new zones/FVG/OB/iFVG/invalidation; invalidation lacks close after planbirth; ATR uses full provider slice. Full cadence cutoff/cooldown/fastmarket/any-stream guards, async nonfatal read, consumes key before read. wakeTimePrice claims closed but takes lastbar unfiltered.

- `trader/auto_trader_watcher.go:1–465` — Observer watch-only no broker calls. Approx thesis latest40 +/-time not signal; firstposition only. Structure hysteresis never writes SCVerdict so nonempty conflict never accumulates2 from empty. Rails WARN uses R1accepted independent R2 display; currentStop inferred from local intent flags not brokerreceipt.

- `trader/auto_trader_weekly.go:1–494` — Weekly async refs-only read/currentcontract resolver, no directional trade gate. Failed calls no row -> nextcycle retries pair despite onceweek wording; successful cache mutex; shadow maps never pruned resetWeek. Legacy stored1m lacks CloseTime, rely downstream semantics.

- `trader/class47_wake_cadence.go:1–241` — Pure wake cutoff/cooldown and log helpers; governs level_event/structure_mss. minutesToFlat uses sessionwindowend not resolved forcedflat offset; inspect downstream caller. Global syncmap stream indicator.

- `trader/detector_record.go:1–194` — Detector recording recover-contained, formation-windowed episodes, persisted watermark/ordinal and heuristic scenario link, candidate pool/follow telemetry. Delta computed over full scope not episodecausal; stated historical diagnostic basis. No order authority.

- `trader/research_snapshot.go:1–308` — StageA snapshots preserve explicit NULL/missing versus0, source clocks, stable identity, strictcorrected outcome exclusions/costunknown; async closures ownership relies Record sync serialization; inspect Record.

- `trader/session_risk.go:1–282` — Session risk band first, breaker strategy/env then defaults8/5; count trader/day not account. Explicit query error failopen. Daily boot decorative if either switch off; fixed maxrun7 neverfires claim false for configurableN<=7. No pertrade cap added.

---

<a id="section-27"></a>

> Original: [reviews/22/report.md](reviews/22/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment22 — trader runtime and position source review

[A] Reviewed all26 assigned source files in full (7,466 lines),0 unread,198 named functions. Base `63968be62e44db2fb07a92883e02127b9064b0be`, isolated `/tmp/vl-understanding-surfaces-20260913`; base and clean status verified before and after resumed reading. `reads.json` preserves full source ranges/hashes plus separately counted dependency/test excerpts. `functions.json` gives function start/end boundaries, purpose, local call expressions and risks. Inline callback logic is grouped with its parent. No tests, live DB/API queries, source mutations, orders, settings, or account changes were executed. Findings below are source facts [A] or explicitly conditional inferences [B], not reproduced trading incidents.

[A] Root instructions, canon, audit R1–R9 and related SYSTEM-MAP/RULEBOOK sections govern. The trader/AGENTS.md mentioned by older instructions is absent in this baseline. Relevant specifications' latest line from the prior shared-worktree authority read is `565e8fbe Sun Sep 13 00:39:02 2026 -0500 fix: use owner daily-loss controls without requiring a per-trade cap`. No mandatory per-trade cap is inferred from older comments. SIM-only, account ownership and corrected-PnL/unknown distinctions remain authoritative. Parent reports an overlapping account/boot/cancel repair branch; this review evaluates baseline63968be independently and makes no claim about those unreviewed repairs.

### Runtime flow, ownership and refusal boundaries

[A] Normal `runCycle` in auto_trader_loop.go first checks running state, calendar producers and CME open state; requires an explicit persisted account choice for NT8; drives deadman state and scheduled analytics; enforces EOD/T1 flat; builds account/position/market context. It updates structure, transition/plan-death state and the armed subsystem, saves equity, then chooses watch/bracket-only/flat AI execution. Wall-clock session and weekly reads live above bar cadence in tickOnce, not in this cycle; feed/position protection monitoring runs separately each minute. This distinction explains why data silence must be reported by monitorTick rather than a bar-triggered loop.

[A] The strategy engine returns a FullDecision or gate hold. The loop records prompt/response/latency, classifies provider errors, manages402/safe-mode state, checks primary-bar supersession, and revalidates entries or discards stale closes/waits. It sorts close-before-open and calls `executeDecisionWithRecord`. Gate refusals intentionally return nil plus actionRecord.Error; successful transport and fills are not interchangeable. Ordinary execution errors use the returned error branch.

[A] Entry wrapper order is feed availability (also closes), deadman, discipline freeze, boot integrity, owner pause, roll, consecutive losses, last-entry cutoff, session, plan mode, owner approval, then canonical live-price entryGateForDecision. Shadow annotations and plan citation are recording. Daily-loss evaluation also exists upstream in engine/context pathways outside this assignment; the wrapper is not the sole risk system. No additional cap policy is proposed. TCP placeEntry itself requires bound tradeable SIM account and duplicate/rate admission, then preloaded nonzero brackets; C# defense is another boundary not fully reviewed here.

[A] Owner pause state uses atomic milliseconds and a persistence mutex; API control writes `trader_pause_until:<id>`, Run restores it, entryPaused expires lazily. Close callbacks route through a trader-ID registry, dedupe position IDs and enqueue one delayed best-effort `post_exit` kick. Cutover status reports DB positions, broker/API positions, separately read NT8 positions, broker/ledger orders and planner claims; it is a status snapshot, not an atomic freeze.

[A] Config readers centralize per-session overrides, registry/default session enablement, plan mode, approval and caps. Explicit session enable overrides registry flags; absent session subset defaultsNY. Approval is keyed by trader and CME session day. Store AcceptanceRuleFor/ReplanCapFor handle nil receivers; their nil calls are intentional. Indicator mirror uses the same market.GetWithTimeframes family and configured periods; source alone does not establish frozen-time byte parity with every executor render. Regime baseline resolves aggregated1m complete-day input first, then5m fallback, retaining source/day/UNKNOWN metadata.

[A] Telemetry path: armed order_update live entry receipt → recordAcceptedRisk → AcceptedRisk.Append; entry materialization/decision creation → excursionOnOpen; monitorTick → excursionOnBarTick; closed-trade analytics → excursionOnClose. These writers contain errors/panics and cannot intentionally refuse trades. Episode close uses active-plan facts to close all unresolved trader touch rows; identity-linked rows check plan/version, while old nearest-scenario rows lack that protection. Watchdog hook is process-global and a resend resolves latest unresolved row by trader, not immutable call ID.

[A] Grid is a separate configured strategy: AutoTrader.Run captures IsGridStrategy; tickOnce returns through RunGridCycle before ordinary runCycle. Grid initializes weighted price levels, runs breakout/drawdown/local daily estimates, asks grid AI, dispatches grid actions, synchronizes inferred fills and writes records. Native GridTrader is preferred; fallback is the duplicate adapter in trader/interface.go, not directly the assigned types/interface.go implementation. Both contain the same problematic protective-order-as-entry fallback. No evidence was collected that the owner currently runs grid or any non-SIM path.

### Principal source-backed findings

1. **Boot sweep marks terminal on a send receipt [A].** class33_boot_sweep.go:89–105 calls CancelOrder then SetState(cancelled), increments swept and can latch completion. TCPTrader.CancelOrder:590 simply returns SendCancelOrder; tcp_server.go:1197–1210 returns WriteFrame error, not NT8 cancellation/absence confirmation. The sweep precedes fill draining in armed_executor.go:210. Therefore the ledger and counter can claim cancellation while broker cancellation remains pending. This is the known overlapping repair area. No affected order IDs or new incident are asserted.

2. **Accepted-risk telemetry can consume stale or prefix-colliding terms [A/B].** accepted_risk_hook.go:51–60 records book age but never rejects stale snapshots; OrderSnapshotCache.Latest:166 has no age filter and brokerBook merely returns the bound-account cache. Separate AgeAt/Latest reads can describe different snapshots. applyBrokerTerms:74 uses case-sensitive HasPrefix(signalID), then suffix role checks, without exact entry/child identity or symbol matching. [B] A shared prefix could contaminate terms; UUID use may make accidental collision rare, but the function contract is wider than intended. Positive live-at-exchange classification correctly rejects dying/local/unreadable states; it does not establish freshness. Caller records multiple live-state receipts, not necessarily exactly one acceptance.

3. **Some decision records overstate success/recovery [A].** runCycle resets402/safe mode after err==nil before checking nil/SkipReason, so a pre-AI guard hold can clear an outage without a successful provider call. Its execute-error branch records an action error without lowering parent Success; gate-refusal branch does lower it. Daily activity query errors leave default zero fields, and OwnerTags are stamped at context assembly rather than independently proving source provenance. These are observable source semantics, not proof of current bad rows.

4. **Pause expiry can race a newly armed pause [B].** entryPaused:99–114 loads an expired value, CAS-clears it, protects store persistence against concurrent re-pause, then returns false regardless of a replacement value. A concurrent PauseEntriesUntil before that return can be missed by this predicate invocation. Resume/expiry persistence errors are discarded, so restart state can differ from optimistic logs. Existing sequential pause tests do not exercise this interleaving. No concurrent reproducer was run.

5. **Position-read failure is conflated with flat in reconcile [A/B].** ntHeldPosition logs error and returns empty; reconcileBeforeOpenNT accepts that as flat both initially and in polling. Later open execution performs another GetPositions, and TCP account/SIM/bracket controls remain, so a bad entry is not established from this function alone. NT map-side uppercase differs from lower-case duplicate/close-quantity comparisons in outer methods. Actual TCP setters only update maps and return nil; outer prebracket ignored-error language does not prove an unprotected NT entry, because placeEntry rejects zero SL/TP. Post-fill setters update local cache, not a proved broker modification.

6. **Excursion labels exceed their evidence [A/B].** resolveExcursionLevels uses StopTargetNear, whose query searches±2min, admits unsuccessful/open-action records without side/fill identity, and limits20 ascending records. It is a nearest authored-decision proxy, not immutable accepted terms. excursionOnClose takes the last bar from a padded [exit−2min,exit+1min) query; [B] when later data exists during analytics it can classify ambiguity using a bar after the exit. StopPxFinal is assigned StopPxInitial with a stale 'no trail wave' comment, regardless of actual modifications. Contract filtering and nullable corrected PnL are good protections; they do not repair these identity/time issues.

7. **Episode telemetry can mistake definitions for observed transitions [A].** closeEpisodesForSessionClose builds Confirmed from Confirm.RefPrice and Armed from enabled authored Arm; Filled map remains empty. Old LevelID-nil rows reuse nearest scenario against current active facts without plan/version check. Store CloseOpenOpportunities derives outcomes from supplied facts, so recording vocabulary must not imply broker fills/observed confirmations. Its update is ID-only after selecting NULL outcome; concurrent closer serialization is not established here. New LevelID rows correctly refuse cross-version matching.

8. **Grid accounting and state tracking are internally inconsistent [A/B].** placeGridLimitOrder caps quantity but stores original d.Quantity; syncGridState interprets disappeared orders from net-position difference, fails to advance expectedPositionSize per inferred fill, and clears OrderID before map deletion on cancellation. Multiple missing orders can be over-attributed to one net change. auto-adjust continues after cancel failure, may overwrite multiple old positions into one nearest level, and resets tracking. Grid records each action Success=true even after errors; parent Success initializer sits inside an end-of-line comment. Daily stop PnL is allocated-margin percentage, not fill-corrected realized cash. These concern the alternative grid path, not demonstrated owner runtime.

9. **Grid fallback is not an entry implementation [A].** adapters call SetStopLoss for BUY or SetTakeProfit for SELL, returning synthetic ClientID/NEW without an entry submission. With TCPTrader those setters are map writes, CancelAllOrders is unsupported, and its CancelOrder signature does not implement the two-argument grid interface. Grid also bypasses normal entry policy gates through tickOnce. This is conditional reachability from grid configuration; no runtime enablement or live-account bypass is claimed.

10. **Diagnostic honesty issues [A].** Feed alert says the bracket still protects without checking book; holding query failure uses flat threshold. HistoryHeldBootLine reports query failure as no imported history. Watchdog table says UTC but formatsCT; last registrant's trader ID owns all fires. Half-day top comments retain deleted-file/full-holiday claims despite current unified calendar code. buildTradingContext logs missing engine then dereferences it later; constructor preconditions may prevent it, so not a reproduced panic. SafeFloat64 accepts nonfinite values and SafeInt truncates floats; these helpers are coercion, not domain validation.

### Tests and historical graph

[A] Test source reviewed, none executed: class33_boot_sweep_test.go expects terminal state after a fake cancel function returns nil, demonstrating the wrong receipt assumption is encoded rather than verified against broker settlement. It also tests failed send retry, unplaced/shadow exclusion and textual ordering. wave_a_safety_test.go and trade_excursion_hook_test.go test nil/closed-store panic containment and epoch-ms recording, not stale/prefix identity. arm_cancel_safety_test.go:155–189 validates dying bracket filtering. pause_test.go covers sequential pause/persistence/expiry/resume and source-based entry-only scope, not concurrent replacement. regime_input_window_test.go drives production estimator/rendering with deterministic complete-day tapes, checks unchanged-window golden, deeper1m preference, fallback/UNKNOWN and source wiring. Generic TraderTestSuite covers16 of19 methods; it omits GetOrderStatus/GetClosedPnL/GetOpenOrders and requires caller mocks to avoid real broker methods. These are source coverage descriptions, not test-pass claims.

[A] Historical Understand Anything extract has93 nodes and512 incident edges across9 assigned paths;17 newer files absent. Types:84 contains,372 imports,6 calls,1 inherits,4 documents,8 implements,37 exports. Existing broad summaries still describe grid/loop/interface roles, but do not prove current safety. In particular its 'fill-confirmed order recording' wording must not be read as cancellation acknowledgement or every parent decision success. types.Trader remains19 methods; grid extension adds3. Historical six calls are not a full current call graph. graph.json retains historical extract and explicit limitations; no reindex or claim of current CGC completeness.

### Scope limits and handoff

All assigned source reading and bounded dependency/test/graph review are complete. Runtime configuration, current database rows, C# execution and parent repair commits were not inspected. No financial performance, real incident count, or successful test run is inferred. Findings overlap other waves where indicated; integrating a fix requires current branch review and appropriate root-owned tests. Per-file notes below preserve lower-priority conditions; function inventory contains exact starts/ends and local syntax-call boundaries, not fabricated type-resolved edges.

### Complete assigned-file source ledger

- `trader/accepted_risk_hook.go` lines1–162: Telemetry only: immutable accepted-risk append, panic contained. applyBrokerTerms uses case-sensitive HasPrefix signal matching and suffix role; no exact identity/symbol filter. Latest book used without explicit age cutoff despite stale comment. Must trace brokerBook/cache freshness and order-update scoping before incident claim.

- `trader/auto_trader_ai402.go` lines1–122: 402 classifier substring-based; outage ID minute granularity can collide within same minute after recovery; optional balance poll uses CustomAPIKey once CME day, no provider check locally; error still consumes daily poll. Must trace serialized callsites.

- `trader/auto_trader_alerts.go` lines1–61: Alert producer day-plan gated, store error recorded. Daily prune latches day before success, so transient failure waits tomorrow. Store owns dedupe and P2 restriction.

- `trader/auto_trader_feedwatch.go` lines1–190: Feed stale alert tightens using DB open rows; query failure treated flat. Age based latest bar close may be negative for future data. Planner nil provider fails open; scheduled closed-market stale bypass intentional. Alert says OCO still protects without verifying actual book.

- `trader/auto_trader_grid.go` lines1–649: Alternative grid lifecycle bypasses normal runCycle entry gates, conditional on grid config; no claim configured live. Drawdown reads total_equity unlike totalEquity; daily local-day in-memory estimate. Emergency ignores close errors and pauses. Grid records each action success despite execution errors; parent Success field swallowed inside comment. GridCount1 divides by0. Context Levels aliases slice.

- `trader/auto_trader_grid_levels.go` lines1–485: Grid bound/weight/direction helpers duplicated locked variants. Auto-adjust continues after cancel failure, resets levels, nearest-level restoration can overwrite multiple positions into one slot with map iteration order; failed pending orders lost. adjustGrid sibling does not recompute bounds. No production incident inferred.

- `trader/auto_trader_grid_orders.go` lines1–419: Grid placement caps quantity but stores/logs d.Quantity, making ledger differ accepted request. No local bounds/state/symbol/finite validation. Position limit overwrites last matching position, query errors zero. sync disappearance heuristic reuses unchanged expected size across missing orders and clears ID before delete on cancellation. Stop PnL uses allocated margin percent not actual fills/quantity/leverage; synchronous network calls under mutex.

- `trader/auto_trader_halfdays.go` lines1–102: Half-day source now unified kernel calendar, sorted entries and boot line; old long comments still describe deleted file/full-closure behavior. maybeSeedHalfDays is deliberate no-op. Upcoming selection date-based includes already elapsed same-day close.

- `trader/auto_trader_indicator_mirror.go` lines1–54: Planner indicator mirror resolves configured timeframes/periods and uses market.GetWithTimeframes; returns fingerprint even no bars/timeframes. Needs production fetch parity trace, no fixture assumed.

- `trader/auto_trader_loop.go` lines1–1414: Full cycle orchestrates context/armed manager before balance wait, then AI/watch/skip, stale reevaluation and ordered execution. Context tags assigned current trader rather than derived provenance. Account DB selection not actual bound snapshot validation locally. AI nil/SkipReason clears outage/safeMode before checking skip. Parent success not lowered on execute error branch. buildTradingContext warns nil engine then dereferences it. Daily activity failure leaves computed-looking zero. See report call traces.

- `trader/auto_trader_orders.go` lines1–898: Entry wrapper gate order covers feed (opens/closes), deadman/freeze/boot/pause/roll/loss/session/plan/approval/canonical gate; lower open methods bypass wrapper if direct. Reconcile treats read error flat, can confirm failed snapshot and proceed (later positions read still another check). Same-side lowercase differs NT uppercase. Futures long logs prebracket failures, short ignores; post-fill setters both again. Quantity clamps default2 but invalid nonpositive inputs can floor1/NaN. Close logs success after submit without local confirmation result. Trace TCP defenses before consequence.

- `trader/auto_trader_pause.go` lines1–227: Owner pause atomic+mutex persistence; expiry CAS clears then concurrent re-pause may occur before return false, even though persistence protected. Resume/expiry ignore persistence errors. Boot config log may overstate daily enabled enforcement; trace resolved consumer.

- `trader/auto_trader_planconfig.go` lines1–258: Session overrides win registry enabled; nil config delegates store nil-safe resolvers to verify. Empty subset defaultsNY, approval key CME day. Plan strict missing registered provider fails open. No feature flag check locally on planModeBlocked. RealignCap0 falls back5. Shared clamp keeps proximity parity.

- `trader/auto_trader_postexit.go` lines1–106: Close hook registry once installs package global callback, bounded position-ID dedupe schedules delayed nonblocking kick. Exact once dispatch is best effort queue capacity; delayed timer has no explicit stopped flag. Gate semantics belong runCycle.

- `trader/bar_source_boot.go` lines1–91: Boot source fields query resolver for all TF and report unavailable. HistoryHeldBootLine conflates query error with no imported rows. No runtime reads performed.

- `trader/class33_boot_sweep.go` lines1–179: Boot sweep sends cancel then immediately marks ledger cancelled, latches success, counts broker cancellation without receipt. Failure retries; empty signal explicitly skipped. Need trace CancelOrder transport to establish async overlap (known repair lane). ledgerOpenOrders ignores symbol parameter, emits configured futures symbol and quantity1.

- `trader/class33_cutover_gate.go` lines1–158: Five read-only cutover legs: DB/API/NT8 positions, broker-vs-ledger working orders, planner claim. Nil armedTrader marked N/A even if configured NT8. Reads non-atomic; no freeze claim. Direct leg3 not recovered wrapper. Known repair overlap possible.

- `trader/contract_current.go` lines1–48: Current contract from server subscribed ACK else labeled latest usable store contract, no date inference; local code no freshness check. contractFact fallback timestamp zero honest.

- `trader/episode_close_wiring.go` lines1–131: Recording-only episode closer closes all trader open rows; legacy no-LevelID scenario nearest may use active other-version facts. New identity explicitly refuses version mismatch. Confirm.RefPrice means definition not observed confirmation; filled map never populated; ArmKind literal limit despite current arm kind expansion. Needs store semantics trace.

- `trader/grid_regime.go` lines1–312: Pure grid regime/breakout/direction rules. Confirmation increments per function call, not distinct candle; no timestamp input. Caller must supply cadence. Defaults nil config panic, invalid metrics classify without finite guards. Separate from NT structural regime.

- `trader/helpers.go` lines1–76: Map number/string helpers; SafeFloat64 admits NaN/Inf and SafeInt truncates float/range conversion, not domain validation. SafeString nil becomes <nil>. Consumers own validation.

- `trader/regime_input_window.go` lines1–112: Baseline selects aggregate1m >=5 complete days else existing5m ring; source/day metadata honest, no gate. Boot before/after measured same estimator; aggregate completeness/contract depends upstream and kernel, not proven here.

- `trader/testutil/test_suite.go` lines1–665: Generic gomonkey suite tests16 interface methods with mocked BTC/ETH inputs; omits GetOrderStatus/GetClosedPnL/GetOpenOrders. It invokes real interface methods unless caller installs mocks; not executed. No account/SIM/async failure coverage.

- `trader/trade_excursion_hook.go` lines1–220: Excursion telemetry panic-contained. Initial levels can be inferred via nearest decision2min; authored != accepted. Contract window avoids roll mixes; close ambiguity picks last bar from padded window, potentially after exit. StopPxFinal forcibly initial despite trailing existing; source comment stale. No runtime row-impact claim.

- `trader/types/interface.go` lines1–238: Trader interface exactly19 methods preserved; GridTrader embeds +3. Fallback adapter uses protective stop/takeprofit setters as supposed entry, fabricated ClientID result NEW, ignores reduce/post/position side; no actual entry semantics. GetOrderBook nil,nil,nil uncomputed ambiguity. Need venue reachability trace.

- `trader/watchdog_fire_wire.go` lines1–89: Global watchdog hook last registered trader labels all fires as that trader; acknowledged single-trader assumption. Latest unresolved fire linking lacks call identity. Table header UTC but formats CT; telemetry only.

---

<a id="section-28"></a>

> Original: [reviews/23/report.md](reviews/23/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 23 — runtime and positions

Immutable base `63968be62e44db2fb07a92883e02127b9064b0be`; worktree `/tmp/vl-understanding-market-20260913`, initially and finally porcelain-clean. All **27 assigned files / 7,481 lines** read fully in bounded outputs. Inventory: **227 named Go functions/methods**, with **30 function literals grouped under their containing declarations**. `reads.json` includes SHA256/ranges and additional dependency excerpts separately; `functions.json` supplies exact boundaries and syntax-only local calls. No tests, production probes, account/settings changes, deployment or source edits. Findings below are static source observations, not reproduced trades, vulnerabilities or incidents.

**[A]** directly read source; **[B]** consequence inferred from its control flow; **[C]** unresolved. Historical incident comments are provenance in source, not fresh measurements. Same-session orientation inherited from assignment19: main AGENTS, tracked CLAUDE-canon, AUDIT-CHECKLIST pre-audit/cutover and parity/unknown rules, SYSTEM-MAP and corrected daily-loss scope. `trader/AGENTS.md` is absent in this worktree. Relevant latest base-local document history: CLAUDE-canon `07b53e65 cleanup batch 1 — B5 the mutation harness that cannot fake a verdict, B6 the worktree-add check`; AUDIT-CHECKLIST `dfda15e1 test: isolate session clock fixtures from weekly backfill workers`; SYSTEM-MAP `565e8fbe fix: use owner daily-loss controls without requiring a per-trade cap`. This review proposes no extra mandatory per-trade cap. Root's later fixes are separate from this base.

### Runtime lifecycle and actual cleanup

[A] `NewAutoTrader` (`trader/auto_trader.go:535`) defaults identity, provider, exchange, instantiates registered AI client and one of the broker implementations, sets timeout/key, loads strategy, and initializes per-trader state. NT initialization still requires DataDir even though factory can choose TCP; transport selection belongs to its factory, not this file. NT setup registers bar persistence/nightly stats, touch episode sink and ordinal seed, then wires ledger open-order source and broker rejection sink. Balance recovery can query broker and persist a new initial balance. Nil strategy refuses after several construction side effects. Thinking-mode lookup calls `AIModel().GetByID(aiModel)` with provider name; `config.AIModelID` exists but is not used here. This may miss per-row overrides unless row ID equals provider [B].

[A] `Run` (:828) marks `isRunning=true` **before** timeframe validation, channel replacement and waitgroup Add. It restores pause, registers post-exit routing, starts drawdown monitor, broker syncs and NT close/reconcile workers, creates ticker, initializes grid if needed, runs once immediately and then serially handles ticker/kick/stop. Slow cycles delay following work; they are not canceled by ticks. `ScanInterval<=0` reaches `time.NewTicker` panic locally. Invalid timeframe or grid-init error returns without restoring running state; grid-init failure occurs after monitor/workers start. Concurrent Run/Stop can race channel replacement and waitgroup enrollment [B]; no lifecycle reservation guards duplicate Run calls.

[A] `Stop` (:1015) clears running, unregisters post-exit dispatch, closes monitor channel and waits `monitorWg` (main Run plus drawdown monitor). It does not flatten, retire arms, tear down broker, or cancel outstanding timer/provider work. Waiting is unbounded if a cycle/monitor blocks. `manager/trader_manager.go:356` RemoveTrader only calls Stop then deletes the object. NT `StartPositionReconcile` dependency at `trader/ninjatrader/reconcile.go:89–95` starts an endless ticker with no cancellation; `close_sync.go:34–79` starts three subscription-range workers without Stop integration. Same-instance `sync.Once` prevents duplicates but new instance after reload creates new once state. [B] old workers can retain old store/trader config after removal; downstream subscription replacement semantics were not fully audited here. A process exit eventually ends them, but Stop does not.

[A] `flattenPosition` (`auto_trader_clock.go:609`) optional limit route registers `time.AfterFunc`, not held in any waitgroup/cancel handle. Callback matches only normalized symbol/side in current OPEN table, not original position ID/entry generation, and can fire after Stop or against a later same-side trade [B]. It closes then cancels stops regardless of close error in that callback; immediate limit-send failure branch likewise ignores closeMarket result before cancel. Default market path does cancel only after successful close call. Broker methods' actual rejection/ack/idempotence matter; no frame was sent during review.

### Clock, entry gates and forced-flat flow

[A] `tickOnce` (`auto_trader_clock.go:955`) separates grid execution; ordinary path runs weekly then session wall-clock reads **before** cadence/dodge gates. Day-plan futures default interval mode; only explicit bar_close selects last closed primary-bar watermark. Bar close requires CloseTime<now. Interval dedup compares newest bar open/OHLCV formatted4 decimals and local position presence; store read failure is treated like no open positions for this skip. Other changing state (orders, risk pause, session cutoff, calendar) is not part of signature. Because EOD/T1 work resides in `runCycle` (`auto_trader_loop.go:314/323`), the flat+unchanged-bar skip can defer that work even if arms or cutoffs changed [B]. Wall-clock reads were hoisted; forced-flat work was not. Full runCycle preflight is assigned elsewhere; this is not a proof that every flatten is reachable during a halt.

[A] Shared dayPlanEnabled is NinjaTrader + configured PlanEnabled. Default primary5m/rootMNQ. Registry loads admin config once per CME session day per object; parse/read failure falls back shipped defaults. Midday edit is deferred by cache but process restart refreshes it immediately. Session cutoff math is wrap-aware, half-day pull-in compared relative to window. Malformed times fail open/no flatten. Legacy day-level LastEntryCT/EODFlatCT functions are not the live session cutoff path. Sscanf accepts some trailing garbage, and offsets beyond session length can resolve outside active cutoff range; upstream config validation not certified here.

[A] `enforceEODFlatAt` (:478) first retires armed entries synchronously, records confirmed/unconfirmed separately, then reads positions to catch a fill during cancellation. No active session counts as closing period. Local empty table invokes `flatTruthAt`: broker positive alarms and returns acted without inventing broker-only exits; unknown stays explicitly unverified. T1 route (:725) mirrors cancel-first, needs active session, and covers blackout start−2min through inclusive end. Empty label is indistinguishable from not due in `t1ForceFlatDue`. Both functions retry on future cycles; send success is not fill confirmation. `flatTruthAt` uses panicguarded gatePositions but its supplied clock is unused and the helper adds no receipt freshness requirement.

[A] Dead-man pure state machine starts live, observes disconnect→await-reconcile→live only after later clean query. Actual query succeeds if GetPositions and GetOpenOrders return nil errors; it does not compare sources or require zero positions/orders. State only advances from runCycle. `cancelUnfilledEntriesAfterReconnect` logs visible orders and **does not cancel them**, despite reconnect log saying sweeping; comments still say market-only/no wire cancel while current armed system supports resting entries and guarded cancellation. Protection reconcile does independently run on reconnect.

[A] Contract roll reads subscription ACK contract, parses ROOT MM-YY, thirdFriday date, default3-calendar-day inclusive window. Unknown resolution passes; initial empty value equals default dedup key, so first missing resolution may not warn. It blocks only daysLeft>=0: a stale expired contract becomes allowed after expiry; date-difference +12h rounding also misrounds negative values. Other router/resolver gates may prevent execution and were not proved here. Local parser accepts any root/month, not only quarterly equity contracts. No financial calendar recommendation is being made.

### Position producer/consumer contracts

[A] NT actual producer `trader/ninjatrader/tcp_trader.go:981` positionMap outputs both snake_case and camelCase price/PnL keys, **positive quantity** and signed `positionAmt` (short negative), leverage1, margin_used0. It contains no signal_id/entry_order_id. `AutoTrader.GetPositions` (`auto_trader_decision.go:234`) reprojects only API snake_case, recalculates crypto notional/leverage margin, and drops other identifiers. `GetAccountInfo` uses same crypto-style margin formula rather than NT per-contract margin; broker-native PNL handling corrects displayed totalPNL baseline independently. Leverage0 produces Inf; absent fields become0/default10. Scope is display/math here, not a demonstrated risk-sizing error.

[A] `recordAndConfirmOrder` (:293) formats unknown/missing orderId with `%v`, yielding literal `<nil>` accepted as nonempty. Nine sync-backed crypto exchanges return immediately and rely on their sync. NT closes return immediately to fill-confirmed close_sync. Other opens create NEW order, sleep500ms, poll5 times, reject explicit CANCELED/EXPIRED/REJECTED, but **after all pending/error results still recordPositionChange using input reference price/qty**. NT GetOrderStatus (:1195) returns pending until current entry fill; it does not validate requested orderID against current signal. [B] this route can create believed OPEN before actual fill or with stale identity; upstream entry waiting may reduce reachable cases, not proved by this function. No phantom row was observed. Open write failure now freezes new entries and emits P0, but its message assumes fill already happened. Freeze is in-memory (assignment19), not durable across process restart despite “until cleared” wording. Successful open log slices `at.id[:8]`, panic for valid short IDs; plan-link errors ignored and transient citation consumed anyway. Fees are not stamped on new position in this function; NT close accounting is elsewhere.

[A] Fill/position writes are not one transaction. OrderCreate failure still permits polling/fill with ID0; after fill-status write failure it continues. Synthetic fill IDs use current UnixNano; close-fill PNL helper is crypto price×quantity and NT closes bypass it. currentAccountName uses bound account, not streamed last-current account; this is correct multi-account attribution. saveDecision increments cycle before save and no store means no-op; GetStatus concurrently reads several run-loop fields without locks, only isRunning protected.

### Desk: source-shape and unknown-state gaps

[A] DeskStripAt (`desk_facts.go:121`) obtains first public projected position and error. **Only POSITION receives posErr**; PROTECTION/DRIFT/TARGET get nil pos and render verified FLAT on an unreadable broker. UI can therefore contradict itself in one response. `acceptedFor` (:394) requires signal_id or entry_order_id, neither survives/exists on actual GetPositions path. Thus normal open-position desk accepted-risk rows remain UNKNOWN even when the DB carries matching records [B]. This is not the protection reconciler's path: it separately queries local OPEN rows for entryOrderID.

[A] PROTECTION, TARGET and ARMS numeric readers use camelCase markPrice/entryPrice only, while the desk position is public snake_case. If identity alone were supplied, mark defaults0 and distance calculation remains wrong. Tests `desk_facts_test.go:38/48` build maps with camelCase + signal_id and call leaf formatter directly, so prove the formatter on invented inputs, not production call-site parity. Existing full-strip nil/broken-store tests check shape/date/reason but allow false FLAT and DAY0. This is checklist53-style producer/consumer divergence.

[A] `deskRealizedToday` (:543) collapses DB read failure to0/0/0 then DAY marks verified; latest500 closes can truncate high-volume day, excludes unknown-reason/UNRESOLVABLE without counting, and doesn't exclude future exit times. CorrectedPNL NULL does count separately. Guardrail display now checks both master and DailyLossEnabled, honoring owner daily-loss rule. `deskFeed` uses received FeedStatus as “link known”, not raw TCP status; a known Disconnected string with recent bars can be stateok/verified. Future bars negative age render0s; RANGE has no staleness downgrade. `deskPlanner` unknown tradeable omits Reason, no activeplan produces verifiedok with UNKNOWN text, and warn state is outside declared schema comment. `deskArms` calls all nonterminal rows resting and any nonempty signal placed, including place_pending. `deskBook` is stronger: freezes one received frame, rejects future/stale, cross-checks ledger, but classifies every gatefailure as stale even fresh working-order evidence. These are display contracts, not authorization gates.

### Protection and stop lifecycle

[A] Minute monitor (`auto_trader_risk.go:43`) checks feed, drawdown, excursions then protection; protection also reconnects and is dayplan-independent. Missing position/book, stale book, unknown stop state and unsized cover do not invent a second stop. Partial cover P0 alerts. No live cover resolves latest accepted stop or composed stop from exact filled signal (last20 arms), otherwise alert only. It reads positive `quantity` from actual NT producer, so this suspected field mismatch was **disproved**. Standalone stop capability/build refusal is surfaced; log explicitly SENT not CONFIRMED.

[A] `isProtectiveStopFor` (`protection_reconciler.go:71`) accepts **any -sl name before checking type or action**. A wrong-side BuyToCover stop named sig-sl can count as long protection [B]. `sameSymbolLoose` treats empty as match and either prefix as match; expired/root ambiguities possible. Coverage sums declared quantity without checking stop price/remaining quantity here. Existing side test deliberately namewhatever exercises shape branch, not suffix override. actOnProtection called from two goroutines (monitor/reconnect), no shared placement reservation; duplicate sends on same snapshot possible unless wire dedup handles them [B]. Action-only dedup map never clears at flat and can suppress a different/new same-side alert-only situation. Counter increments repeated place attempts and partial-cover alerts, not unique fully-unprotected positions.

[A] BE/trail default **suspended** by EXIT_MECHS_SUSPENDED; explicit false/off0 re-enables per-strategy opt-in logic. Wire seam gates both. BE flag and trail last level represent send success, not acceptance; stop-widen ban belongs downstream. Trail samples best mark each minute, not high/low since actual entry; keys have no position ID. Trail uses uppercase side while BE/prune use raw side; current NT uppercase shape agrees, arbitrary other callers may not. Arming isn't permanently latched for after-trigger-points: each tick rechecks condition.

[A] Independent drawdown exit remains active: current leveraged profit>5% AND retrace from peak>=40%, then emergency market close. This is not covered by BE/trail suspension despite boot prose listing only stop/target/EOD/invalidation. Hard assertions on broker maps in this bare monitor goroutine can panic process; entry<=0 guard doesn't validate mark/NaN. PeakPnL cache cleared only after emergency close succeeds; source search found no normal-close/prune caller. [B] prior trade peak can affect later same-symbol/side trade. Local exit success means broker call returned nil, not close fill recorded.

[A] F12 leg4 is honest about **ledger-empty fallback before first snapshot**, despite old comments saying nil fails. Once any book ever received, missing account or stale>2N fails; working count mismatch fails and nonzero alwaysfails. Authorized/no-signal unplaced arms informational. No future-age refusal in leg itself (display adds one). Override nearest stop price validates neither side nor qty and accepts NaN paths via comparisons absent input validation. Source label says ledger(no book for account) where actual leg refuses, another reporting discrepancy. BracketsBootLine labels inferred OCO shape from snapshots but does not prove chronology; mixed separate valid bracket groups become MIXED, and last entry/TIF wins display.

### Imports, analytics and legacy support

[A] Named history importer is gated defaultoff and writes via ImportBars, correlation subscriptions, perrequest deadline and context, sequential timeframe loop. Header promises echoed actual contract and bounds readback. Actual `pullOne` (`historical_import.go:116`) allows empty response Contract, stamps **requested** contract, derives first/last from incoming payload before write/skips, and never reads store bounds. Symbol/tf/window/OHLC values are not validated in this layer. Contract mismatch on later chunk claims nothingwritten although earlierchunks remain. Midstream error reported unavailable even if imported>0; mixed imported/skipped calledpartial, contrary to header's partial-failure definition. Elapsed pertimeframe uses commonrunstart (cumulative). No import performed.

[A] Current `bars_store_depth.go` uses currentContract + LastNBarsOn, refuses unknowncontract deepening and keeps nonemptyring tail authoritative. Header's contract-blind warning is stale. Merge expects sorted ring/store; it neither sorts nor checks freshness; stale nonempty ring is still eligible. Emptyring remainsempty. Excursion backfill identifies unique contract in hold±1min; no unique contract→no coverage/NULL, appropriate unresolved state. It gets initial stop/target by nearest decision120s, not signal, and sets final stop to same initialstop; moved stops and ambiguous slackbar need separate evidence. SetLevels error ignored; archive writes partial on latererror.

[A] Runtime closed analytics calls research outcome then skips alreadygraded; computes positions MAE/MFE from current futuresSymbol cache, while trade_excursion hook/backfill can read contract-scoped historical data. Therefore “same math” does not ensure same tape. No coverage still grades and prints zero MAE/MFE locally; grade becomes idempotence flag, preventing later retry of missing excursion/watch/matched-random work. Single global dayplan_analytics_since marker, newest20 ungraded batch, seam rows intentionally not graded can repeat; store query exclusion details assigned elsewhere. CorrectedPNL integrity comparison uses exact uppercase SHORT; fees excluded in simple recomputation.

[A] Owner ForceReset writes baseline latest+1 and alert before waiting/claim; failure/competing read doesn't roll marker back, returned claimed status doesn't certify a fresh valid plan. No direct positions/brackets mutation. Authored liveness accepts UNKNOWN with event and refuses knowndead; exhaustion records warning only, not wake/budget changes. Confirmation desk reads versioned dated evaluator evidence, no recomputation, explicit absent/future/stale.

[A] Shadow AB defaultoff: global lock excludes **other shadows only**; launches goroutine after live read, mutates registered shared client thinking, resets only on normal tail. A future livecall can overlap shadow and panic can leave mode changed [B]. Source search found no shadowABInFlight gate outside harness. Validator re-reads current bars/clocks and omits newer live liveness/etc checks, so “same full chain/same inputs” is too strong. Count read/write errors ignored can undermine sample billing bound. No shadow call made.

[A] Legacy `CreatePositionSnapshot` deletes all trader OPEN rows before reading broker, no transaction/backup, retains signed shortqty, loses account/lineage, logs insertfailures then returnsnil. `RebuildPositionsFromTrades` uses realizedPNL==0 as open so breakevencloses misclassified; fee isn't reduced when partially consuming openquantity, later prorations overcount; partial match still outputs whole closeqty and input is sortedinplace. Source search found no current non-test call to either exported entrypoint; risk is future/manual reuse, not current live incidence. Grid adapter emulates BUY via shortstop and sell via longtarget, not certified limit-entry semantics; unsupportedbook returnsnilnilnil. Grid regime mutates direction before pricefetch and retains change onerror, tolerates cancel failures, uses fixed display risk heuristics; source dependency confirms applyGridDirection does not reacquire gridmutex, so no recursive-lock finding asserted.

### Validation and graph limits

No tests executed. Read complete protection-reconciler test file and desk tests1–193, plus named discovery of clock, roll, dead-man, trailing, desync, untracked-position and scenario-desk tests. Tests establish intended contracts only; no freshPASS. Root owns focused reproductions/repairs and merged-head validation. Dependency manifests/advisories outside assignment; root-reported five Dependabot alerts (2high/2moderate/1low) unverified.

Historical July10@7a8adce0 graph:51 nodes for7 assignedpaths,417 incident edges exported; all51 node summaries and64 internal containment/export edges read. External353 edges not validated. Current corrections: prelaunch onlyClaw402 wallet checks, not general broker/feed; GetOrderBook neverfetches; recordAndConfirmOrder not unconditional fill-confirmed; rebuild carries realizedPNL rather than computing it; ClearPeak onlyemergencycaller. New runtime/clock/protection/desk/import files largely absent. `graph.json` carries current source boundaries. CGC historical, no query/reindex performed here. This review does not certify deployed NT8, runtime freshness, actual accounts, PNL, stop coverage, or whole-repository correctness.

### Full file coverage

- `trader/auto_trader.go:1–1253` — Factory, identity/config/provider/broker wiring, initial-balance recovery, NT bar/touch/reject/open-order sinks; Run starts single decision loop plus monitor and broker syncs. Stop waits own waitgroup but not NT workers or delayed callbacks; Run sets running before validation and lacks rollback. Model thinking lookup uses provider name instead of configured AIModelID. Feed/watchdog and default-suspended breakeven helpers included.

- `trader/auto_trader_clock.go:1–1018` — Dayplan futures cadence; weekly/session wall reads before data skip; session-relative/halfday last-entry and EOD/news cancel-before-flatten, corroborated flatness; delayed limit fallback lacks position generation/Stop ownership. Closed analytics path, grading idempotence and global epoch. Cadence dedup compares only rounded newest bar + local position table; skips other work when unchanged.

- `trader/auto_trader_decision.go:1–637` — API equity/status/position projections and immediate order/fill/position persistence. NT closes defer to actual close frames; opens still persist fallback reference after polling exhaustion, absent orderId becomes <nil>. Projection drops signal identity and camelCase consumed by desk. Position write failure freezes new entries; plan-link errors ignored. Margin math crypto-style on NT contract quantities.

- `trader/auto_trader_grid_regime.go:1–345` — Grid breakout confirmation, pause/reduce/close/direction and recovery; reduction sets field not immediate liquidation. Direction mutated before market-price read; failure leaves changed state; broker reads under grid mutex. Risk display uses fixed heuristic leverage and liquidation values.

- `trader/auto_trader_registry.go:1–41` — Global system_config session registry parsed with default fallback; per-trader session-day cache under mutex. Midday edit deferred but restart refreshes immediately; returned struct retains map/slice references.

- `trader/auto_trader_reset.go:1–141` — Owner reset rechecks session eligibility, writes latest+1 baseline before read claim, emits alert, waits30s for competing claim; planner failure not directly returned and baseline persists. No direct position/bracket mutation; concurrent reset race unresolved.

- `trader/auto_trader_risk.go:1–336` — Minute monitor feed/drawdown/excursion/protection. Hard map assertions can panic bare goroutine. Fixed current profit>5% and peak retrace>=40% market-close remains active despite BE/trail suspension. Peak cache only cleared on emergency success, not normal flat. Notional20x fallback, min12 and max3 helpers preserve separate venue controls, no new per-trade risk policy.

- `trader/auto_trader_trailing.go:1–223` — Opt-in deterministic5m ATR trail with defaults2x14 after-breakeven, best observed mark, ratchet/BE floor, wire-suspension refusal. Emits update on send success not broker confirmation. Uppercase trail key differs from raw-side BE/openKeys for noncanonical callers; no persistent per-fill identity.

- `trader/bars_store_depth.go:1–186` — Deepens nonempty shallow ring only with older current-contract store bars LastNBarsOn; unknown contract/read failure returns ring. No sort/freshness enforcement inside merge, callers promise ascending bars; old header contract-blind limitation is superseded by current contract filter.

- `trader/confirmation_desk.go:1–61` — Reads versioned scenario_meta.confirm plus observed_at, never recomputes bars. Missing per-scenario evidence UNKNOWN, future/aged record stale; snapshot global timestamp not perverdict evidence age.

- `trader/contract_roll.go:1–163` — ACK named ROOT MM-YY contract, thirdFriday CT date window default3days, block new entries within inclusive nonnegative days; unavailable fail-open and initial empty string no warning due dedup default. Past-expiry reallows, root/month not quarterly validated.

- `trader/dead_man_watchdog.go:1–106` — Pure3state link watchdog, entries blocked down/reconcile, reconnect defers probe a cycle then clean reads resume. Initial zero-state live does not require initial reconcile; no freshness/equality proof in callback itself.

- `trader/desk_facts.go:1–816` — 14row read projection, perrow panic containment, dates/source/reason,5s/15s cadence. Actual GetPositions loses accepted-risk join identity; protection/drift/target nil position conflates read error with flat; day errors fabricate verified0; range/feed age/known-status semantics incomplete. Ledger arms are not proven broker orders.

- `trader/exit_mechs_suspend.go:1–100` — Default suspension of BE and ATR trail, environment optout, wire seam and notice dedup; exit boot line on means unsuspended not perstrategy enabled. Does not suspend separate drawdown-close mechanism.

- `trader/f12_leg4.go:1–263` — Broker order-snapshot freshness2N and ledger cross-check, unplaced arms informational; no snapshot yet allows ledger-empty fallback. Override requires closest stop price but no side/quantity proof. Source label missing account says ledger although gate refuses. Future age checks absent here.

- `trader/flat_truth.go:1–99` — Corroborates local0 with successful broker query0; absence/error distinct, only local0/brokerpositive disagreement reports. Caller now unused and no standalone receipt-age validation.

- `trader/historical_import.go:1–205` — Env-gated named-contract sequential timeframe live-wire imports, request subscription/deadline and append-only store call. Empty response contract accepted and requested stamped; bounds from payload not store, no symbol/tf/window validation here. Partial-write errors labeled unavailable, skipped overlaps partial; elapsed cumulative perrun.

- `trader/interface.go:1–88` — Aliases broker types; GridTraderAdapter maps BUY to short stop and other side to long take-profit; synthetic client-ID NEW result, leverage failure tolerated. Individual-cancel unsupported errors; GetOrderBook returns nil,nil,nil unsupported, never fetches.

- `trader/level_zones.go:1–20` — Dayplan futures boot line resolves strategy ID and active-session zone options without backfill; missing DB gives UNKNOWN.

- `trader/order_book_display.go:1–51` — Account+symbol scoped projection of exactly received frame, future/stale receipt reason, unknown build preserved; available empty list[] distinct absentnil, exact trimmed symbol comparison.

- `trader/plan_liveness.go:1–76` — Authored invalidation check accepts UNKNOWN with event, refuses known born-dead within candidate retries; exhausted scenarios warn-only once persisted event, no wake/budget/arm changes; boot counts read store.

- `trader/position_desync.go:1–118` — Default-on gate cross-check live positions; no matching local symbol/side means logdesync, refuse skip and guarded orphan bracket cancel with knownflat context. Partial matching suppresses whole discrepancy; qty/freshness not independently checked, no row close here.

- `trader/position_rebuild.go:1–195` — Legacy pure FIFO fill reconstruction using realizedPnL==0 as open, in-place unstable time sort, fee allocation leaves fee unchanged after partial consume, incompletely matched close uses full qty. No current production call found by source search; zeroPNL exits misclassified.

- `trader/position_snapshot.go:1–107` — Legacy destructive snapshot deletes trader OPEN rows before broker read, no transaction/backup, signed short qty retained, perrow insert failures ignored and no account/lineage/accepted-risk identity. No current production caller found by source search.

- `trader/protection_reconciler.go:1–430` — Minute/reconnect actual broker-position and fresh order-book cover check; unknown or partial cover alert, none places accepted-risk/filled-arm stop by signal. Quantity alias confirmed actual producer. -sl suffix bypasses type/action check; symbol loose prefix/empty match; action-only dedup never pruned onflat; duplicate concurrent placement not serialized locally.

- `trader/rootfix_shadow_ab.go:1–246` — Default-off bounded sample shadow fast-mode experiment; async global shadow lock only, mutates shared liveclient thinking and resets not deferred; validator rereads current bars/time and omits newer live checks. Counts outcomes, logs but no plan publication; successful persistence count required to bound billing.

- `trader/trade_excursion_backfill.go:1–157` — Recompute closed hold excursions from uniquely identified contract window with slack; ambiguous/unknown contract no usable bars, corrected PNL retained nullable. Nearest decision stop/target not signal identity; SetLevels error ignored and stop final set initial despite possible moves.

---

<a id="section-29"></a>

> Original: [reviews/24/report.md](reviews/24/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 24 — plan UI and guide source review

Baseline `63968be62e44db2fb07a92883e02127b9064b0be`, isolated `/tmp/vl-understanding-execution-20260913`. **61/61 assigned files, 12,988 lines fully read; zero unread.** Named-function census: 184 functions/methods, plus 17 declaration/content-only file records and 243 anonymous callbacks covered within their containing functions/file. Fifteen additional dependency/test files were read fully or in explicitly recorded excerpts. This is a slice review, not whole-repository coverage.

Evidence **[A]** means exact source/test inspection; **[B]** identifies inferred runtime consequences. No browser reproduction, service calls, trade execution, live data inspection, source edits, or tests were run. A test described below was read, not demonstrated passing. Historical comments and guide performance numbers were not independently revalidated.

Rules followed: shared review instructions, main instruction file and baseline canon/checklist/SYSTEM-MAP/RULEBOOK from the preceding assignment. Spec freshness: `AUDIT-CHECKLIST.md`: `dfda15e1 2026-09-13T00:48:28-05:00 test: isolate session clock fixtures from weekly backfill workers`; SYSTEM-MAP/RULEBOOK: `565e8fbe 2026-09-13T00:39:02-05:00 fix: use owner daily-loss controls without requiring a per-trade cap`. Fixed-baseline review intentionally does not rebase. Owner daily-loss-only policy takes precedence over historical per-trade-cap assumptions.

### Purpose and end-to-end connections

`PlanCard` (`web/src/components/plan/PlanCard.tsx:33-190`) owns session/version selection, trader-scoped actions and the dashboard composition. `usePlanToday` (`usePlan.ts:17-42`) keys SWR by trader, symbol, session and optional version; historical versions do not poll. `planApi.getPlanToday` (`web/src/lib/api/plan.ts:487`) calls the authenticated HTTP client. Backend today serialization (`api/handler_plan.go:440-485`) returns document, machine level facts/windows, approval/runability, provenance and execution read models. `SessionPlanCard` (`:107-980`) renders lifecycle/no-plan states, authored versus machine evidence, historical versions, chart, scenario/zone/rules views, and edit/ask/realign dialogs.

The owner edit door sends overlays or sticky owner levels, then asks the planner to re-examine changes. `EditSheet.save` (`:113`) and `.del` (`:182`) send index patches; `BulkAddSheet.save` (`:53`) sends individual sticky levels. `SessionPlanCard.runRealign` (`:132`) requests a proposal; `RealignPanel.apply` (`:107`) and `AskPlannerPanel`'s `PlannerReply.apply` (`:128`) call the QA apply endpoint. Backend `applyPlanOverlay` (`api/handler_plan.go:1016-1079`) serializes overlay read/fold/append, validates the resulting document and price armor, and operates on the latest active plan. Ask decline is its own recorded owner QA event (`handler_plan.go:1564-1627`). Browser dialogs do not themselves authorize or place trades.

`ScenarioList`, `OrderTerms`, `StructuralGeometry`, `ArmedUnderBlock`, `ScenarioEconomics`, `OneSetupChip` and `FadePermissionChip` distinguish authored ideas, frozen geometry, evaluated outcomes, actual accepted order terms and open-position provenance. Missing outcome fields generally render UNKNOWN/unevaluated rather than fictional confirmation. `ScenarioEconomics` is absolute-distance authored arithmetic, before costs and composition; it is not realized expectancy or a guarantee of order admission.

`DeskStrip`, `AlertCenter`, `GateBlocksPanel`, `ExpectancyPanel` and `InstrumentsDrawer` fetch independent read models at different cadences. Expectancy is global, not selected-trader-only; the panel distinguishes corrected P&L/exclusions, descriptive sample floors and counterfactual data. `PlanMiniChart` fetches current market candles independently even beside a historical plan. `sessionConfig` and the timeline are browser-side static CT visual aids; backend active/runnable session fields govern the card.

`GuidePage` (`web/src/guide/GuidePage.tsx:443`) renders typed static sections via `BlockView` (`:151`) and a partial text search index (`collectHits:60`). Its one-time health revision check indicates build drift, not that every section is behaviorally correct. The only mock mode lookup (`useResolvedPlanMode:22`) reads the first public trader's resolved strategy mode once; it is not selected-trader or per-session context and explicitly falls back to “example — not a reading.”

### Source-backed findings

#### 24-1 — stale index edits can act on a different current level [A source, B consequence]

`EditSheet.tsx:123-139,182-191` sends `replace/remove /levels/${levelIndex}` without a `test` operation, plan ID, version, original value or stable level ID. `SessionPlanCard.tsx:121-125` retains the clicked fact/index in dialog state. Backend `handler_plan.go:1040-1053` fetches the latest active row, folds its overlays and applies the supplied patch. The mutex protects simultaneous backend writes, but cannot establish that a browser's old index still refers to the same level. If a replan or earlier remove/reorder occurs while the form is open, a still-valid index can overwrite/remove another level. Historical door gating (`SessionPlanCard.tsx:176-181`) is good but does not bind an already-open edit to its originating document. This is not a reproduced trade failure. Review priority: identity/concurrency guard at the mutation contract and the real UI callsite.

#### 24-2 — bulk drafts reset on unrelated parent renders [A source, B consequence]

`BulkAddSheet.tsx:37-47` clears text and busy state in an effect depending on `[open,onClose]`. The actual parent supplies a new inline `onClose` on every render (`SessionPlanCard.tsx:951-960`). A plan poll or other parent state update while the sheet is open reruns this reset, losing staged input. Clearing busy while a batch is in flight can also reopen the submission guard. No browser timing reproduction was attempted. This is separate from the form's intentional reset when first opened.

#### 24-3 — API HTTP failures masquerade as no plan / no alerts [A source, B consequence]

`httpClient.ts:212-258` converts HTTP error responses into `success:false`, while network errors throw. `plan.ts:getPlanToday:487` returns null on unsuccessful response; `usePlan.ts:28-39` therefore resolves normally and does not set SWR error. `PlanCard.tsx:174`/`SessionPlanCard.tsx:236,263-270` can present a no-plan/night state for an HTTP failure. `plan.ts:642-650` substitutes `{alerts:[],unacked:0}` on HTTP failure, and `usePlanAlerts:65-72` exposes no error channel. `AlertCenter.tsx:197,315-323` then loses its urgent banner and can show an empty feed. Claim is limited to returned HTTP failures, not all network failures. `DeskStrip.tsx:107-140` is a useful counterexample: it explicitly surfaces null/error as unreachable.

#### 24-4 — dual bias label is serialized at a different JSON location [A]

`kernel/plan_doc.go:298-301` declares top-level `bias_label`; the today handler returns the document directly (`handler_plan.go:470`). Frontend `PlanBias` (`plan.ts:14-16`) models it inside bias, `SessionPlanCard.tsx:750` passes only `doc.bias`, and `BiasBlock.tsx:73-78` reads `bias.bias_label`. No alias is inserted by the inspected serializer. The main direction still renders, but the AI/tree/regime composite label cannot arrive through this typed path. Static producer/consumer mismatch; not browser-reproduced.

#### 24-5 — realign uses calendar date during the wrapped ASIA tail [A, overlap already repaired]

`handler_plan.go:2110` derives today's CT calendar date, then `:2125` looks up the ASIA plan under it. `kernel/plan_chain_date.go:33-66` assigns the after-midnight ASIA tail to yesterday's session-instance date; today, overlay and thread consumers use this helper (`handler_plan.go:288,1040,1534`). Thus at 00:30 CT, a valid previous-date ASIA chain can yield `status:skipped, reason:no_active_plan` when owner edits trigger realignment. The helper's existing tests cover this exact midnight distinction, but not this handler callsite. **Root reports this was repaired in `a6b88b7d`; baseline finding retained solely for provenance, not a request to rediscover/fix again.** No local runtime reproduction.

#### 24-6 — bulk range preview promises information the write omits [A]

`bulkParse.ts:22-52` parses upper price; `BulkAddSheet.tsx:182-184` previews the range, but `:63-66` posts only lower price, label and note. Save therefore loses `priceHi`. Its partial-success path (`:73-86`) closes the form whenever any row succeeds and summarizes the first six attempted rows, not the successful subset, so the realign summary can name rows that were never added. `EditSheet.tsx:153-177` similarly collects grade/instruction but only sends these to a later realign proposal, not the sticky-level write; accepting a proposal is a distinct action. UI should describe the persisted contract accurately. Parser tests validate parsing ranges, not their persistence.

#### 24-7 — selection and mutation scope can diverge [A source, B operator confusion]

`PlanCard.tsx:62-65` sets selected session after the first response and never restores null. Despite its auto-follow comment, a user who never clicks a tab stays on that initial session after the active session advances. Header approve/re-read/reset controls (`:115-155`) receive only trader ID and remain outside historical/sibling-session door gating. They act on the active backend session, not necessarily the displayed card/version. The backend controls eligibility and these actions may intentionally be global; the issue is the lack of explicit action-target context beside a pinned card. Existing selected-session fetch behavior is correct: tab clicks actually change the request.

#### 24-8 — stale read-model attribution and decline inconsistencies [A source, B consequences]

`GateBlocksPanel.tsx:51-67` preserves old rows/loaded state across trader changes or failed fetches until a successful new response. The component is reused under `PlanCard.tsx:97`, so old-trader counters can temporarily appear under the new selection. `ExpectancyPanel.tsx:101-104` and `PlanMiniChart.tsx:151-197` also retain prior data on failures without an explicit stale/error state; the former's as-of is rendered date-only (`:156`), hiding intraday age. These are display risks, not altered trading gates.

`RealignPanel.tsx:187-195` “Keep as-is” only dismisses locally, unlike Ask's persisted decline (`AskPlannerPanel.tsx:144-156`). Ask hydrates applied status only (`:110-112`); backend decline adds a separate owner event rather than changing the proposal's applied flag (`handler_plan.go:1604-1627`), so remounted proposal bubbles can offer action again. This does not imply an existing decline was applied. The realign budget counts planner responses (`handler_plan.go:2145-2152,2213-2229`), not Apply clicks; manual requests bypass that auto cap. Guide “Apply costs 1/decline free” prose is inaccurate.

### Guide contradictions and daily-loss-only policy

These are currently served static guide statements, even where they discuss old waves. They should not be treated as new production engine defects merely because old exploratory results appear in prose.

| Subject | Exact evidence and interpretation |
|---|---|
| Structural stop / daily loss | `content/settings.ts:415-422` correctly says no separate per-trade dollar cap, one MNQ, structural stop not overridden by ATR. `guards.ts:13` agrees. Older universal ATR claims at `guards.ts:94-111,243`, `faq.ts:83`, `status.ts:86` conflict with this exception. Root repaired the overlapping planner instruction in `710ea1c8`; do not restore a mandatory dollar cap or universal ATR override. |
| Scenario quality | `guards.ts:145-147` and `faq.ts:97` broadly say it does not gate; `settings.ts:258` acknowledges a configured floor. `trader/armed_executor.go:2135-2138` actually refuses below-floor quality. Default low floor can admit all normal grades, but changing the setting is consequential. |
| Fade permission | `FadePermissionChip.tsx:50`/`guards.ts:326` emphasize descriptive labels; `plays.ts:16` requires permission for one-setup. Distinguish the historic recorded label (not itself re-read as authority) from fresh `FadePermissionAt` feeding `OneSetupAllowsAt` (`trader/one_setup_wiring.go:147-150`, `kernel/one_setup.go:146`). Broad “nothing refuses” wording needs that distinction. |
| Wake gates | `settings.ts:400-410` says WARN-only/still runs; `status.ts:87` describes enforcement. Assignment21 directly traced current level-wake cutoff/cooldown enforcement and a separate MSS bypass; neither should be flattened to a global WARN-only rule. |
| Consecutive loss | `settings.ts:460,557-559` says enabled with master; `guards.ts:126,230` correctly describes standalone breaker. Assignment21 traced the production independence. This is not a reason to enable/tune any live control. |
| Proximity width | `settings.ts:62` says higher K is tighter while `:69` gives K × daily range, which grows with K. `levels.ts:155` also carries older 0.3 retune text versus `settings.ts:67` 1.5. No live value was read here. |
| Realign spend | `settings.ts:164` and `planCard.ts:194` couple budget to Apply/decline. Handler counts planner replies, before an owner applies anything. |
| Weekly missing document | `weeklyBias.ts:37,131` and `WeeklyChip.tsx:14` imply no effect. Assignment21 traced a Sunday-ASIA weekly-document prerequisite: distinguish missing reference context from the scheduler's required initial weekly read. |
| “Live/current” constants | `tradingDay.ts:101,108-110` calls a literal table YOUR live config; `settings.ts:608` lists literal current overrides. They are source claims, not fetched account settings. Build revision matching cannot make constants live. |
| Historical measurements | `plays.ts:50`, `routines.ts:51`, `levels.ts:25` carry performance/sample claims without row IDs or sufficient artifact lineage in these passages. Treat as unverified historical research prose, not an admission rule or reproduced result. Newer `expectancy.ts` correctly discusses sample floors, corrected P&L, counterfactual/contract limitations and missing evidence. |

The detailed file ledger records additional minor prose drift (reset version/budget phrasing, settings hot-reload description, static RR defaults and deleted thin-side terminology). None establishes an account's current configuration. No financial/trading recommendation is made.

### Boundaries, negative results and existing tests

* HTTP-success handling for re-read/reset was investigated and **not promoted to a defect**: inspected backend failures return non-2xx (`handler_plan.go:1183-1188,1231-1236`).
* Public `/api/traders` intentionally requires no bearer token (`api/server.go:114`); the guide mode hook's first-trader/context choice is its limitation, not missing authentication on that public fetch. Protected config remains separately authenticated.
* `RulesBlock.tsx:58-65` conflates empty computed band and absent band, but current `RenderNoTradeBand` (`kernel/no_trade_band.go:213-216`) returns nil for zero machine windows and documents legacy prose fallback. This is a contract/future-compatibility risk, not a proven current empty-array production failure.
* Optional null/undefined geometry and excursion formatting branches, a multi-column grid alignment concern, and sparse fabricated version chips were identified as contract/visual limitations but not reproduced or proven reachable from the current backend contract. No claim of crash or missing production version is made.
* Alert initial seeding (`AlertCenter.tsx:162-173`) can run against the hook's initial empty array before the first response, so later-arriving P0 backlog may toast as “new.” Ref sets also survive trader changes. This is an additional static notification concern.
* `NoTradeBand.test.tsx` exercises live/elapsed/other-session windows and legacy prose fallback. It does not exercise a computed empty band. `bulkParse.test.ts` validates parsed ranges, not save payload fidelity. `P5_door.test.tsx` verifies `/levels/2` patch and prose/owner-note preservation, not a changed plan between opening and saving. `W13_integration.test.tsx` tests add → proposal from mocked backend shape, not midnight identity or parent-poll form reset. `W16_decline.test.tsx` verifies decline endpoint and in-place distinct outcome, not hydration after remount or RealignPanel's local dismissal. `kernel/plan_chain_date_test.go` covers 00:30 ASIA → prior date at the helper boundary. No tests executed.

The historical Understand Anything graph (`2026-07-10@7a8adce0`) has **zero nodes for these assigned paths**. `graph.json` contains that explicit absence and 187 actual current relative-import edges, labeled potentially type-only. It does not infer runtime calls from imports. Root's frontend symbol census is syntax-only; current source is authoritative. No CGC reindex/service change was attempted.

### Complete file-role ledger

Every assigned file follows, including pure content/type files. Function-level boundaries and lexical calls are in `functions.json`; full read ranges/hashes and test/dependency excerpts are in `reads.json`.

* `web/src/components/plan/AlertCenter.tsx`: P0banner/newtoast,P1feed,P2count, ack/dismiss/clear APIs. Firsteffectseedsfrominitialemptyhook=>backlogtoastedonceactualfetch. Failedfetchclearsbanner viaemptyfacade.

* `web/src/components/plan/ApproveButton.tsx`: Trader-only approval POST guarded; requires response.approved before success.

* `web/src/components/plan/ArmedUnderBlock.tsx`: Only server version_differs renderspositionprovenance; absentarmedversionexplicitunknown.

* `web/src/components/plan/AskPlannerPanel.tsx`: Portaled Q&A, JSON patch preview, persisted apply/decline and local tri-state; refresh effect lacks traderId, caller omitsplanId. No persisteddeclined fieldhydration, remountoffersoldproposal. Patches parsed withoutarrayvalidation.

* `web/src/components/plan/BiasBlock.tsx`: Renders direction conviction flip and bias.bias_label; verify top-level backend bias_label mismatch.

* `web/src/components/plan/BulkAddSheet.tsx`: Sequential per-rowadd partialsuccesscloses, summary includes first6 attempted notsuccessful; rangehighpreview never persisted. effect on unstableonClose resetsdraftonparentpoll.

* `web/src/components/plan/DeskStrip.tsx`: Serverrenderedrows andunknown/stalecount, dynamic5/15spoll, null/errorunreachable; age_ms frozenbetweenfetch,noindependentclock.

* `web/src/components/plan/EditSheet.tsx`: Index-based replace/remove of current levels; add ignores displayed grade/instruction (only sent to subsequent realign), editAI ignores note/tag; parseFloat positive allows partialnumeric. Form closes whilebusy possible; no versionbinding.

* `web/src/components/plan/ExpectancyPanel.tsx`: Globalconditionexpectancy60spoll, correctedPnl/exclusions/ids/NULLstats/descriptivefloor, E8counterfactualseparate; stale retainedonsilentfetchfailure, asofdateonly, cryptoexcludedfieldnotrendered.

* `web/src/components/plan/FadePermissionChip.tsx`: Recorded label plus exclusions/details/unknown; absentnotpermission; tooltip saysnogate verifyonesetupdependency.

* `web/src/components/plan/GateBlocksPanel.tsx`: Pertrader+global counters20spoll; keepsoldrows/loadedacrosstraderchangeorerror, nofetchfreshnessstate.

* `web/src/components/plan/InstrumentsDrawer.tsx`: Descriptiveadherenceoncepertrader,60sexpectancyexcursions,retiredlevelgate; failurezeroedfacaderendersno-trades; excursionnusesallconditionnnotmeasuredn.

* `web/src/components/plan/LevelOverlayPrimitive.ts`: Chart lifecycle/accessors/views/canvaslevelbands and lines; gradealpha onlyA notA+; autoscale allanchorprices notzonebounds; axes first6 gradeA regardlessstructuralcomment.

* `web/src/components/plan/LevelZoneMap.tsx`: Frozenzonemap preservesnullwidth/broad/shortlist/formation/sourcecontext; noauthoredarrayreorder.

* `web/src/components/plan/OneSetupChip.tsx`: Recordedallowed/waiting/declined/off/unevaluated distinct; descriptiveonly noexecution.

* `web/src/components/plan/OrderTerms.tsx`: Selected legintended/composed/accepted sources, rowplacement/version provenance; currentbookage frozeninpayload notclientaged.

* `web/src/components/plan/PlanCard.tsx`: Composes desk/alerts/tabs/statistics/actions/sessioncard; selected initialized from first active session then never null again, so auto-follow comment inaccurate. Approve/reset/reread outside historical door gating; active trader mutation while sibling/version displayed.

* `web/src/components/plan/PlanErrorBoundary.tsx`: Threadrendercatch resetretry, inputsoutside; Q&Aonlyboundary notgeneralplancard.

* `web/src/components/plan/PlanFooter.tsx`: Sharedversioncontrol plusdaytype/model/recordedreplans; no newbudgetformula.

* `web/src/components/plan/PlanLiveness.tsx`: Nullabletradeable=>UNKNOWN, zerooftotalpositive=>warningonly exhaustion; noaction.

* `web/src/components/plan/PlanMiniChart.tsx`: Chartpoll15s currentklinesevenhistoricalplan, sorted/dedupseconds, oldbarsretainedonsilentfailure; facts->overlaydropszonebounds/freshness.

* `web/src/components/plan/RealignPanel.tsx`: Proposal apply shares ask endpoint; Keep-as-is only localdismiss unlike Ask persisteddecline. Timed nochange/capped/failed dismiss. Manualbutton bypasscap percontract.

* `web/src/components/plan/RereadButton.tsx`: Poll backend eligibility30s, guarded confirmed force reread then success toast and delayed mutate, busy finally; gate retained across trader change until fetch.

* `web/src/components/plan/ResetButton.tsx`: Confirmed force reset restores chain; guarded/finally preserves responsiveness; success note warning. Control accepts trader only.

* `web/src/components/plan/RulesBlock.tsx`: Serverlive/elapsed/other windows; hasBand requiresnonempty, so computedempty[] re-promotesmodelnoTradeproseasrules.

* `web/src/components/plan/ScenarioEconomics.tsx`: Authored hypothetical/economics prices positivefiniteorUNKNOWN; abs-distance R beforecosts/composition, nofee orsidealignmentcheck.

* `web/src/components/plan/ScenarioList.tsx`: Recordedstate missing=>unevaluable, chips behindevaluablebranch; economics/orderterms alwaysvisible. ConfirmMETrequiresrecordedoutcome; authorqualityinformational.

* `web/src/components/plan/SessionPlanCard.tsx`: Lifecycle/no-plan rendering, historical versions/death history, chart/live facts, rules, scenarios, owner edit/ask/realign gates. Owner door uses is_active!==false, !historical,!no_trade; state not reset on trader/version. BiasBlock receives doc.bias only.

* `web/src/components/plan/SessionTabs.tsx`: Accessiblelabels/disabledserverrunnabletabs; arrowselectupdatesselectionbutnotDOMfocus.

* `web/src/components/plan/SessionTimelineStrip.tsx`: 30s clientclockmarker overstaticbands andkillzones, activefromserver; nottradingauthority.

* `web/src/components/plan/StructuralGeometry.tsx`: Server geometrysnapshot rendersreason/quantityMNQ/costrisk; undefined-safe formatter notnull-safe. Researchcandidatelabel.

* `web/src/components/plan/WeeklyChip.tsx`: Refs-only PWH/PWLthin/none, no directionalcall; none saysnothingchanges despiteweeklyreadprerequisiteonSunday.

* `web/src/components/plan/ZoneTable.tsx`: Factarrayindex forwarded to overlayeditor, conflictdimming/grades/touch/distance; 5trackgrid >5children; freshmissing=>consumed viahelper.

* `web/src/components/plan/bulkParse.ts`: Canonical single-token type parser, skipsbadlines, prefixparseFloat andrange upperweakvalidation; My level two-wordtoken unreachable.

* `web/src/components/plan/chips.tsx`: Grade/provenance/fresh/status/version/conflict/lifecyclewidgets. VersionChips still fabricates1..count despitecommentrealrecords; unknownlifecyclegoldactive; A+gradeCcolor.

* `web/src/components/plan/levelState.ts`: Freshness/sweep bucket, fixed12pointnearthreshold, signedroundedistance,3pointowner-vs-AI conflictingproseheuristicforvisualonly.

* `web/src/components/plan/sessionConfig.ts`: StaticCT visualbands mirroredregistry, onlyNYdefaultenabled, approximateDSTwarning; inWindowequalendsfull-day whereas toSegmentsempty.

* `web/src/components/plan/usePlan.ts`: SWR keys include trader/symbol/session/version, historical polling disabled; facade failures resolve values so SWR error absent. Versions lack symbol dimension. Alerts default zero with no error channel.

* `web/src/components/plan/vocab.ts`: CanonicalEnglish level/instruction/grade literals; parserusescasefold type list.

* `web/src/guide/GuidePage.tsx`: 15sectiontypedrenderer/searchpartialblockindex; healthfetchonceprefix12driftcheckunknownsilent. No persectionstampvalidation, allcontentEnglish.

* `web/src/guide/components/Example.tsx`: Dashedcaptionwrapperrealcomponentsmockprops.

* `web/src/guide/components/KnobCard.tsx`: Renders10mandatoryknobfieldsasstatictext; starrecommendationdisplay.

* `web/src/guide/components/MockPlanCard.tsx`: Realchipswithstaticmocklevels/scenarios; resolvesmodefirsttrader; confirmexamplelacksoutcome=>UNKNOWN whiledetailMET; notliveplan.

* `web/src/guide/components/useResolvedPlanMode.ts`: Readsfirsttrader thenresolvedconfig once; tradersrequest lacksbearerheaders, noselectedtrader/session, unknownlabelonfailure.

* `web/src/guide/content/buttons.ts`: Staticactionsideeffects; staleSavehotreloadprose, emergencyflat contradictory market-now/limitladder.

* `web/src/guide/content/candidates.ts`: Frozenfullzonemap/provenance/unknownwidth experimentalweights; legacyseatedcandidateordering/projectionsseparatelylabeled; noauthoredarrayreorder.

* `web/src/guide/content/expectancy.ts`: Researchrecordlateness/NULL/contractlimits; correctedPnl/sampleIDs/descriptivefloor/E8limitations; olddrawerreplacementpending andmanydisplaycolumnclaimsnotcurrentUI.

* `web/src/guide/content/faq.ts`: Staticanswerswithhistoricalincidents; broadclaims no repeatedreject/nofadeATRexception/noqualitygate stale; revisiondriftmeansdifference notnecessarilyolder.

* `web/src/guide/content/glossary.ts`: Staticterms; ThinSidecontradictsdeletedMinSide; bias/confirmadvisoryclaimsoversimplifyenforcement; rawmoneyguaranteesnotrevalidatedhere.

* `web/src/guide/content/guards.ts`: Structuraldaily-loss-onlypolicy explicit; olduniversalATRfloor/shadow/quality/fadepermissionclaimscontradictnewsections; standalonebreakeroutside mastermismatchsettings.

* `web/src/guide/content/levels.ts`: Levelkinds/gradeheuristics/HTFdetectorcoverage/roles; unvalidatedmultipliersdisclosed; stale0.3band vssettings1.5, weeklysourceshistoricnotalllive.

* `web/src/guide/content/planCard.ts`: Identity/economics/bucketcompletion/orderprovenance updated; stalequalityadvisoryvsfloor, resetv1/budget, realignapplycost; descriptivearithmeticcorrect.

* `web/src/guide/content/plays.ts`: OneSetupfadepermissionrequiredandstructuralgeometrycurrent; followcounterfactualpreregistration; broadplaycatalog/legacy75%returnclaim/ATRentrylaws coexist.

* `web/src/guide/content/routines.ts`: Staticdaily/weekly/emergencychecklists; staleSYSTEM_STATUSgreen and75%performancequestionnoIDs.

* `web/src/guide/content/settings.ts`: Staticknobcatalogwithsomecurrentstructuraldailyonlyownerpolicy; contradictoryRR3vs2, proximitydirection reversed, wakeWARNvsenforce, mastermustgatebreaker, Applychargesrealign, simulatedlivevalues notread. No mandatorypertradedollarcapinupdatedstructuralcard.

* `web/src/guide/content/status.ts`: Recentorderprovenance/pendingcancel/feedcontractchanges described; bootlineslegacyATR andoldtimeoutcoexist; rowsstaticnotlivedata.

* `web/src/guide/content/tradingDay.ts`: StaticCT schedule/wakes/lunch/EOD; claims tableYOURliveconfigbutliteral; defaultclocknotcurrentaccount.

* `web/src/guide/content/weeklyBias.ts`: Refs-only/shadowcalibration, candlecoverage/historysplice; missingweeklydocfailopenclaimcontradictsSundayASIAprerequisiteinreview21. Truncated combinedread repaired100-136.

* `web/src/guide/content/welcome.ts`: SIMarchitecture/roles/canonreferences; historicalexecutorcentricflow ignoresarmfastpath; release-before-swap cannotprovealreadyrunning.

* `web/src/guide/types.ts`: Guideblockunion/knobfields/searchtypes; sharedGUIDE_BUILT_REV0c9d4f30 stampnotproofcurrentcontent.

* `web/src/lib/api/plan.ts`: Plan document/read-model types and HTTP facade: mutations mostly structured errors; some fetch failures collapse to empty arrays/zero KPI; approve uniquely throws. Reread/reset wrappers treat HTTP success as ok without payload ok; follow backend/consumers before finding.

---

<a id="section-30"></a>

> Original: [reviews/25/report.md](reviews/25/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 25 — web shell, charts, chat and shared client contracts

Source review complete for **54/54 assigned files, 12,309 lines, 0 unread assigned files** at **63968be62e44db2fb07a92883e02127b9064b0be** in `/tmp/vl-understanding-surfaces-20260913`. `functions.json` records **205 named function/method or named factory-callback entries**, with exact start/end lines, and **162 anonymous callbacks grouped under their containing function**. This is a TypeScript AST census checked against full manual reads, not 205 exported APIs. Configuration literals/type-only files are recorded at file level. The 4,152-line translation catalog was read completely in eight successive bounded ranges. Dependency reads remain explicitly separate from assigned coverage.

**Evidence:** [A] means exact source/graph/test text read or source syntax parsed; [B] means behavior inferred from those branches; no browser, test suite, application, live endpoint, database, order or wallet operation was executed. These are static findings, not reproduced incidents. Node was used only to parse source with an already-installed TypeScript parser; Python only generated review artifacts. No source/live changes or child agents. Existing E2E is deliberately not run because its Vite proxy targets the deployed `:8080` backend. No service reindex.

Standing canon and AUDIT-CHECKLIST were read in this worker's earlier assignment context (canon full; checklist 1–115 and 2281–2348), with current SYSTEM-MAP 301–320 and VL-TRADING-RULEBOOK-v1 180–200. The relevant latest spec log was `565e8fbe Sun Sep 13 00:39:02 2026 -0500 fix: use owner daily-loss controls without requiring a per-trade cap`. This review preserves the owner's daily-loss policy and does not revive a mandatory per-trade cap. Listed `web/AGENTS.md` is absent in this baseline. Findings are independent of the concurrently repaired branch; no claim that baseline findings remain unfixed there.

### Actual end-to-end boundaries

- **Current market/equity display:** `TraderDashboardPage.tsx:827` renders `ChartTabs` with `marketOnly`, resolved exchange type and selected symbol; it separately renders `EquityChart`. `ChartTabs.tsx:142–528` owns market/symbol/interval state and renders `AdvancedChart`, which requests `/api/klines` through authenticated `httpClient`. SVP is fetched by `AdvancedChart.tsx:1262–1294` from protected `/api/klines/svp`, attached once and updated via `SessionVolumeProfile.setData:467`. The primitive consumes server-computed bins/POC/VAH/VAL; it does not calculate strategy levels or place orders. `ChartWithOrders` and `TradingViewChart` have **no current production import/caller found**, so their external/legacy feeds are not the current NT8 futures source.
- **Account and command ownership:** `traders.ts:20–183` exposes my-traders, CRUD, lifecycle, close-position and NT account selection endpoints. It passes optional account for applicable commands, while server routes own authorization/trade refusal. Structured `ApiError` retains error key/params/status for selected calls, others throw generic errors. `EquityChart` queries current account first and uses account-scoped SWR history/account keys; APIs include the account query and backend `handleEquityHistory:128–191` reads scoped snapshots. Browser controls are not backend safety gates.
- **Chat:** `AgentChatPage` uses the assigned `useAgentChatStore`, persistence helpers and five assigned presentation components. Welcome suggestions invoke the send callback, including a one-contract MNQ long instruction; execution authorization remains backend-owned. Current stream functions mutate a global store and persist a snapshot to the request's captured user ID. Steps hide tool/central-brain bookkeeping; bot text is delegated to MessageRenderer (its implementation is outside this assigned slice). User text is rendered as escaped React text.
- **Auth/setup:** Router still mounts RegisterPage, SetupPage, AgentChatPage and FAQPage; welcome renders BeginnerOnboardingPage over the traders page. AuthContext handles token storage, login/register navigation and logout. RegisterPage's config flag is presentation gating; server registration endpoint owns first-user enforcement. Setup selects beginner/advanced, with beginner producing a wallet preparation request. That request is a protected POST which may create/adopt model credentials and writes process/.env wallet settings, including on the UI's balance-refresh action. There is a separate GET current-wallet endpoint, unused by this page's refresh.
- **Credentials:** Current model configuration APIs fetch the server encryption flag, optionally fetch/import its RSA key and encrypt the request. `CryptoService.encryptSensitiveData:87–155` generates fresh AES-256-GCM key/12-byte IV, 128-bit tag and AAD with user/session/time/purpose, then wraps the key with RSA-OAEP/SHA-256 and base64url encodes. The two-stage key modal itself only concatenates/validates and calls onComplete; its label is not evidence of encryption. Backend `/crypto/decrypt` is JWT-protected at server.go:175; the unauthenticated frontend helper has no current caller, so this is a dormant broken helper, **not a currently public decryption oracle**.
- **Presentation/config:** Header/Container/LanguageSwitcher/FAQSidebar and related wrappers are render/navigation helpers. Translation keys, TS interfaces, test/build configs and branding literals do not prove backend features or defaults. Explicit source file notes below cover all assigned declarations, including dormant helpers and stores.

### Findings and limitations, prioritized by current reachability

#### 25-F1 — active market selector can send a symbol to the previous exchange [A source, B effect; medium]

`ChartTabs.tsx:181–185` calculates currentExchange as hyperliquid for that market, otherwise `exchangeId || marketConfig.exchange`. `handleMarketTypeChange:232–236` changes market/symbol but not the passed exchangeId. The dashboard supplies `ninjatrader` for an NT trader. Select Crypto: label/symbol become crypto/BTCUSDT while AdvancedChart still receives ninjatrader; Stocks/Forex/Metals similarly retain the original exchange. AdvancedChart:218 interpolates that exchange into the actual klines request. This predicts a misleading/empty market display, not an order-routing bug. The reverse case also applies when selecting NinjaTrader under another passed exchange. `ChartTabs.test.tsx` does exercise the same mobile market handler but its AdvancedChart mock destructures **only symbol**, so its assertion passes while the exchange remains wrong. Add exchange to the production-call-site assertion in a future fix.

#### 25-F2 — active FAQ includes destructive/stale operating guidance [A source, B consequence; high-priority content correction]

`translations.ts:827–828` (English), corresponding Chinese text at2213–2214, calls SQLite `data.db-wal` and `data.db-shm` lock files and tells the reader to delete them. `faqData.ts:226–227` maps that entry, FAQContent's default branch renders the answer, and AppRoutes:499 mounts FAQ. WAL may contain committed state absent from the main DB; this is not a safe generic lock-recovery instruction. The text also proposes stopping processes without this deployment's lock/flat-window rules. No command was run.

The same active FAQ says stops/targets are merely AI guidance (`faqStopLossTakeProfitAnswer`, English804–805, mapped faqData:192–193), contradicting the current protective-stop futures architecture. It lists crypto-only exchange setup, old upstream github link (removed in the VL rename) installation sources and an upstream individual's security contact. Treat it as inherited prose requiring alignment with the current runbook, not current architecture or supported deployment instructions. **Negative result:** stale `faqPRGuidelinesAnswer` points upstream, but `FAQContent` specially overrides that item with the correct project PR target; do not count that stale translation as a currently rendered wrong-PR instruction. Market/competition/data strings alone likewise do not prove removed pages exist.

#### 25-F3 — initial onboarding failure leaves a blank wallet panel [A source, B visible behavior; medium]

`BeginnerOnboardingPage.loadOnboarding:24–50` stores the failure and clears loading. Render chooses `loading ? ... : data ? ... : null`; its error box is inside the data branch. On first failure, data is null, so the body hides the stored error and provides no retry. Heading still states wallet ready. On a later refresh failure, existing data makes the error visible, so those cases differ. The skip button marks completed even while preparation is pending. Current backend handler `api/handler_onboarding.go:43–96` proves this mount/refresh request may mutate wallet/model/env state; it is not a read-only balance request. Static page flow only, no wallet generated or queried.

#### 25-F4 — hiding/reopening the live two-stage modal retains private-key state [A source, B behavior; medium]

`TwoStageKeyModal.tsx:42–347` owns part1/part2/stage and returns null on isOpen=false; neither close nor successful completion calls reset. Its caller `ExchangeConfigModal.tsx:1525–1532` keeps the same component mounted and toggles isOpen using secureInputTarget. Completion:350–359 copies the key then hides the modal. Reopening therefore retains both parts and the completed second stage, including when targeting a different exchange within the same parent instance. The two-second stage timer also has no cleanup. This is a secret-lifetime/cross-target UX issue, not demonstrated exfiltration. Clipboard diversion cannot erase clipboard history or JS memory. Final callback returns plaintext to the parent; transport encryption happens downstream.

#### 25-F5 — cross-user frontend state lacks a complete ownership boundary [A source, B scenario; medium, shared-browser/account-change condition]

`TraderStatusPanel.tsx:11–16` uses one constant SWR key, `agent-sidebar-traders`. AuthContext logout/unauthorized handlers clear auth state/storage but do not clear SWR cache; no SWRConfig/cache-clear boundary was found in web source. A different account logging in within the same SPA can initially see the prior cached trader list before revalidation. Single-user operation reduces ordinary incidence, but registration/reset/user changes are not accounted for in that key. No backend authorization bypass is claimed.

`agentChatStorage.ts:44–62,119–135` copies/falls back to guest/legacy histories for every empty user history and leaves source keys intact. Tests explicitly require this behavior; they do not test two successive users. More seriously, `AgentChatPage.runAgentStream:103–389` persists the **current** global store with captured storageUserId through helpers:81–101; user-reset effects:489–540 replace that store without aborting the existing stream. Pagehide abort exists; SPA unmount/auth switch does not invoke it. An old callback can therefore serialize the new user's current conversation under the old user's key even when botId no longer matches; this is a source-level cross-user persistence scenario, not reproduced leakage. Backend stream continuation and account-reset policy are outside this slice. A future fix needs generation/identity checks at mutation and persistence, plus explicit stream lifecycle ownership.

#### 25-F6 — current equity presentation can invent baseline/cycles and mix snapshot times [A source, B display effect; medium]

`EquityChart.tsx:133–199` drops every equity<=1 observation, falls back through truthy baselines to1000, and supplies `index+1` when cycle_number is absent. Backend history returns available_balance, total_pnl, total_pnl_pct and no cycle_number/pnl. The normal scoped NT baseline uses real snap.Balance and is an improvement over global initial_balance; do not label all selected-account curves fabricated. But missing/zero baseline data yields a fabricated number, and every ordinary returned row gets an invented cycle label. Header account equity polls15s while PNL and footer current equity use history polling30s, so they can represent different instants without an as-of label. Chart timestamps use browser-local `toLocaleTimeString` without timeZone/date at:187 despite the CT policy. `account` query errors are ignored, and absent account top value is displayed0.00 while historical values can remain populated. No financial measurement was recomputed from real data.

#### 25-F7 — recoverable config failure sticks; polling backoff contracts can be defeated [A source, B effect; medium/low]

`useSystemConfig` listens for invalidation and uses mounted guards, but successful refetch does not clear an earlier error. Dependency `lib/config.ts:11–24` retains a rejected promise indefinitely unless explicitly invalidated and does not check HTTP status before caching JSON. A transient initial failure can remain cached across normal consumers. The hook's manual refresh is the recovery route.

`useAutoRefresh:118–174` owns per-effect inFlight/visibility/backoff and clears timer/listener, but does not cancel an active request. Consumers must guard identity/late results. `MarketTicker.fetchTickers:26–46` catches errors and returns normally, so the shared hook cannot increment backoff despite its caller comment. Empty/non-success payload can erase tickers without a stale/error label. No measured polling load claim.

#### 25-F8 — global confirmation is one replaceable resolver, not a queue [A source, B scenario; low/medium]

`ConfirmDialogProvider.confirm:65–76` overwrites state.resolve. Two overlapping confirm requests leave the first promise unresolved. handleClose resolves inside a React updater; global registration has no unmount cleanup. Common usage may serialize dialogs; no actual concurrent caller incident is asserted. Historical graph explicitly says it queues requests, which the source disproves. Future verification should call the production global confirm twice, not compare duplicate mock promises.

#### 25-F9 — crypto key cache can pair a new PEM with an old key after import failure [A source, B scenario; low/medium]

`CryptoService.initialize:33–39` assigns publicKeyPEM before awaited import. If an existing key is present and a replacement import fails, PEM becomes new while key remains old. Retrying the same PEM early-returns and uses the old key. Concurrent key imports likewise lack a generation lock. The normal stable-key happy path is sound by source; no broken ciphertext was produced. Downstream APIs do await initialize; they do not silently fall back to plaintext when a required crypto config fetch fails.

#### Other bounded findings / dormant risks

- `t:4129–4152` has no English fallback and replaces only the first instance of each placeholder. Indonesian misses newer default-lock/grid-futures messages, so the UI can display raw keys. Translation claims “Never uploaded” for model wallet keys contradicts frontend-to-server configuration submission; local signing means server-local here, not browser-only. Actual reachability was located in ModelConfigModal:850; no credential inspected.
- `AgentStepPanel:59` trusts persisted step status/label, while storage validates arrays only; corrupted/older message schemas can crash rendering. No untrusted remote exploit established.
- `RegisterPage` classifies any message containing “limit” as whitelist capacity, lowercases beta code while saying case-sensitive, and renders AES-256 as fixed text. AuthContext register catches failures, so SetupPage's lack of a local catch is not independently a normal network-crash finding. Server first-user enforcement was not re-audited here.
- **Dormant:** config/modal Zustand stores have no production consumers beyond their export barrel; config-store stale-user load, unauth branch retaining private caches and e.id-vs-exchange_type heuristics are revival hazards. Likewise unused counter hook ignores target0, GitHub stats hook has request races, clipboard fallback ignores false execCommand, and stripLeadingIcons uses malformed astral escapes. They are not current observed UI failures. Color helper is used by ComparisonChart; index-derived color is only stable while list order stays fixed.
- **Dormant charts:** ChartWithOrders omits exchange from load dependencies, retains markers across chart recreation, overlaps fetches, aligns order times to epoch buckets and does not support week suffix, with a browser-local tooltip despite CT axes. TradingViewChart embeds external script and defaults crypto. Neither is the active NT8 chart.
- **Active SVP limitations:** primitive global autoscale ignores visible-range args and partial/frozen/inVA are not signaled. Anchor search limited to±30minutes may omit coarser sessions;40px minimum can exceed intersession space. These are visualization hypotheses, not flaws in kernel SVP measurement. The caller's syncSVP can reattach after toggle-off if an earlier fetch returns late because it rechecks series existence but not current enabled state (dependency issue, overlaps chart owner's slice).
- Numeric intraday CT formatter is DST-aware and existing tests establish intended seasonal labels by source. BusinessDay/string conversion turns a calendar date into UTC midnight and can display prior CT date, but current reviewed candle loaders supply numeric timestamps, so this remains a dormant input-contract edge.

### Verification quality and historical graph

Read five related test files, **none executed**. ChartTabs tests cover composition and symbol changes but omit exchange. Storage tests assert guest migration and snapshot semantics. Chart-time tests cover numeric DST/CT only; purported host-zone test does not change TZ. Trader slug tests prove immutable-id round trips and unique legacy bookmarks; no duplicate legacy-name test. RegisterPage tests import no production component and recreate regex/validation objects locally, including an asserted specialCharsRegex prop the current component does not pass. They prove their own test logic, not production PasswordChecklist parity. No SVP primitive/EquityChart/modal-lifetime test was located by targeted filename/content search; absence is scoped to that search.

Historical Understand Anything snapshot (July10@7a8adce0): **90 exact-path nodes,284 incident edges,46/54 assigned paths** were extracted/read; source wins. Eight assigned paths are absent. Current graph artifact records172 syntactic import/re-export and manually traced consumer edges, clearly distinguishes file imports from resolved function calls, and preserves historical edges. Corrections: confirm does not queue; MarketTicker now MNQ rather than spot strip; main Vite binds127.0.0.1 not historical0.0.0.0; ChartTabs now marketOnly/separate equity; config/modal stores need reachability caveat; API barrel includes plan API; chartTime and SVP are new boundaries. Historical test links helped locate the self-contained RegisterPage tests. CGC export belongs to root; no current semantic index claimed.

Potential repairs require current-branch comparison and targeted offline production-call-site tests. This source review does not assert existing tests pass, deployed rev equivalence, browser rendering measurements, current account values, correctness of all external libraries, or full repository coverage.

### Assigned file ledger

Every file below was fully read; exact hashes/ranges are in reads.json. Exact function boundaries and individual purposes are in functions.json.

- `web/e2e/fixtures.ts` (35 lines): Playwright page fixture optionally attaches CDP, creates isolated context. Cleanup not in finally if use rejects; source only, E2E not executed.

- `web/e2e/playwright.config.ts` (64 lines): Serial desktop/mobile E2E launches localhost3000 dev proxy to deployed8080. Specs may mutate/restore settings; not offline-safe authorization. E2E auth env optional. No execution.

- `web/eslint.config.js` (89 lines): ESLint TS/React rules relax explicit-any/unused/exhaustive-deps and newer hooks checks; lint success cannot prove effect dependencies or types. Config JS ignored.

- `web/src/chips-harness.tsx` (166 lines): Dev-only harness renders real chips and reread controls; patches gate only; not production entry. RereadButton action may still call real endpoint if clicked; no harness executed.

- `web/src/components/agent/AgentStepPanel.tsx` (109 lines): Step panel hides tool/central_brain steps; trusts status and label; unknown persisted status dereferences undefined style.

- `web/src/components/agent/ChatMessages.tsx` (157 lines): ChatMessages renders user text escaped and bot through MessageRenderer; meaningful execution excludes planning/tool internals; forwardRef and inline callbacks reviewed.

- `web/src/components/agent/MarketTicker.tsx` (208 lines): MNQ ticker polls authenticated endpoint; fetch catches errors so shared backoff cannot observe rejection; absent data becomes empty with no error/stale label.

- `web/src/components/agent/TraderStatusPanel.tsx` (119 lines): TraderStatusPanel SWR uses constant agent-sidebar-traders key rather than user id; auth provider cache cleanup must be traced for cross-user stale private data.

- `web/src/components/agent/WelcomeScreen.tsx` (191 lines): WelcomeScreen suggestions directly invoke onSend including one-contract MNQ long request; backend permissions own trade gating.

- `web/src/components/auth/LoginRequiredOverlay.tsx` (159 lines): Login overlay is presentation only; backdrop close and navigation links; lacks explicit dialog/focus keyboard management.

- `web/src/components/auth/OnboardingModeSelector.tsx` (75 lines): Onboarding mode selector defaults described as Base wallet/Claw402+GLM; current page says DeepSeek; no model authority here.

- `web/src/components/auth/RegisterPage.tsx` (365 lines): Registration config starts enabled and catches fetch failure, initialized false is gate; server must enforce single-user. Broad substring limit misclassifies rate-limit as whitelist; lowercases beta despite case-sensitive label. AES256 static footer is not encryption evidence.

- `web/src/components/charts/ChartTabs.tsx` (528 lines): ChartTabs wires active EquityChart/AdvancedChart; NT14 intervals; manual market selection except hyperliquid still uses passed exchangeId, causing stocks/forex UI to request original NT exchange. Missing exchange defaults hyperliquid; fetch symbols no auth/status/abort; symbol reset effect protects default.

- `web/src/components/charts/ChartWithOrders.tsx` (669 lines): Legacy ChartWithOrders comment says unmounted; source race and stale-marker issues: load effect omits exchange/height, no cancellation/inflight, created markers retained across recreated chart; order times floor assumes epoch bars (wrong session anchors), unsupported w falls60; tooltip still browser-local despite CT axes.

- `web/src/components/charts/EquityChart.tsx` (521 lines): Active equity/account SWR keys account-scoped after accounts fetch; no user id. Filters equity<=1, fabricates1000 baseline if absent/zero, current top account total may differ last-history PNL; browser-local time with no date; account error ignored. NT baseline uses first available_balance, verify backend meaning.

- `web/src/components/charts/TradingViewChart.tsx` (421 lines): Legacy TradingView crypto external widget; CT timezone explicit, language id fallsEN; cleanup clears DOM but external load no cancellation/error; verify unmounted before classifying.

- `web/src/components/charts/primitives/SessionVolumeProfile.ts` (583 lines): SVP primitive renders backend sessions/bins with per-session relative histogram scaling, latest price labels, global autoscale union; +/-30m anchor snapping can omit coarse intervals, min40width may exceed gap. partial/frozen/inVA ignored visually; no trading decisions; data validity delegated backend.

- `web/src/components/common/ConfirmDialog.tsx` (123 lines): ConfirmDialog single resolver state replaced on concurrent confirm: earlier promise stays pending; global registration no cleanup, resolve invoked from updater; dialog callbacks reviewed.

- `web/src/components/common/Container.tsx` (40 lines): Container layout-only polymorphic wrapper; defaults1920px and responsive padding.

- `web/src/components/common/Header.tsx` (76 lines): Header translations and three language setters; simple hides subtitle.

- `web/src/components/common/LanguageSwitcher.tsx` (33 lines): LanguageSwitcher maps supported language choices to context setter.

- `web/src/components/common/WhitelistFullPage.tsx` (132 lines): WhitelistFullPage navigates login or callback; blank official links still rendered as external links; generic capacity copy may not reflect actual denial.

- `web/src/components/faq/FAQSidebar.tsx` (60 lines): FAQSidebar grouped translated question navigation; active id assumed globally unique.

- `web/src/components/modals/SetupPage.tsx` (226 lines): Setup deletes browser auth/onboarding keys on mount and defaults beginner; submit minimal8 length delegates AuthContext; failure handling assumes register resolves; context state and storage may diverge.

- `web/src/components/modals/TwoStageKeyModal.tsx` (347 lines): Two-stage key input concatenates58+6 hex with optional0x; clipboard overwrite cosmetic not memory security; isOpen false does not clear parts/stage, timer has no cleanup, reopening retains secrets if component remains mounted; completion only callback (label encrypt is not actual crypto).

- `web/src/constants/branding.ts` (24 lines): Branding raw filesystem strings and static informational version; official links intentionally blank.

- `web/src/hooks/useCounterAnimation.ts` (51 lines): Counter animation end0 early-returns leaving previous displayed count; duration<=0/nonfinite not validated. RAF cleanup on deps change; fractional negative floor rounds down.

- `web/src/hooks/useGitHubStats.ts` (91 lines): GitHub repo/contributor queries, external only if used; no abort/mounted/request identity on owner/repo change, old responses can overwrite. Missing contributors rendered0, error preserves old data; days use abs ceil.

- `web/src/hooks/useSystemConfig.ts` (42 lines): System config hook ignores unmounted response and reacts invalidation; success never clears previous error, so recovered config can retain error UI. Shared cache implementation dependency not yet read.

- `web/src/i18n/translations.ts` (4152 lines): Full4152line three-locale catalog and t lookup reviewed. No ENfallback on missing nested IDkey; interpolation replaces first occurrence only. Current FAQ contains stale upstream install/PR/security contacts, deletion of SQLite WAL/SHM, guidance-only stops, crypto-only architecture and false never-uploaded key wording; reachability traced separately. Strings are not operational instructions or verified financial/model claims.

- `web/src/lib/agentChatStorage.ts` (142 lines): User-scoped chat keys but fallback/migration copies shared guest/legacy messages into any empty user history without removing origin; possible shared-browser cross-user leakage. JSON arrays unvalidated. Writers/removers can throw storage errors; streaming snapshots normalized false.

- `web/src/lib/api/index.ts` (15 lines): API object spreads six modules; later same-named properties override earlier. Thin export, not enforcement.

- `web/src/lib/api/traders.ts` (183 lines): Trader API wrappers target my-traders/config/actions/account endpoints; typed ApiError preserved only selected mutations. Nonarray getTraders becomes empty; request IDs interpolated without encoding; server authorization required. No API calls executed.

- `web/src/lib/autoRefresh.ts` (174 lines): Polling helpers pause hidden, prevent per-effect overlap and exponential backoff8x; no abort of in-flight request on effect cleanup or trader change, consumers must prevent stale application. Errors caught inside consumer cannot signal backoff. PollResume resets after30s.

- `web/src/lib/chartTime.ts` (78 lines): DST-safe CT intraday labels, but BusinessDay/date-only parsed UTC midnight then shifted to prior CT day/month/year. Needs consumers timeframe review; no runtime plot asserted.

- `web/src/lib/clipboard.ts` (30 lines): Clipboard fallback ignores execCommand false and announces success; rejected async Clipboard API does not attempt fallback. Temporary textarea cleanup not finally.

- `web/src/lib/crypto.ts` (258 lines): Hybrid RSA-OAEP SHA256 AES-GCM256 random12byte IV and authenticated user/session/time metadata. initialize sets cached PEM before await import, so failed replacement can leave old key with new PEM and future retry early-returns. No concurrent init coordination. decrypt fetch has no auth header locally; server route access must be traced. Config/encryption flag defaultfalse until fetch; callers own enforcement.

- `web/src/lib/httpClient.ts` (321 lines): Axios attaches token globally;401 clears auth and redirects with intentionally forever-pending promise, static latch reset externally.403/404 throw generic,5xx server-message returned as business failure; contract comments overgeneralize. Timeout overrides truthy only; no response shape validation.

- `web/src/lib/onboarding.ts` (37 lines): Global localStorage user mode/wallet/completion state not account scoped; no storage exception guard. Route maps beginner welcome else traders; no authorization.

- `web/src/lib/text.ts` (28 lines): Decorative prefix regex uses four-digit Unicode escape syntax for supplementary ranges (\u1F000 etc); potential unintended ASCII range stripping. Need isolated pure proof/tests, not executed.

- `web/src/pages/BeginnerOnboardingPage.tsx` (295 lines): Onboarding invokes prepare mutation on mount and balance refresh, displays returned private key; initial load failure stores error but error rendered only inside data branch -> blank body with no retry; skip marks completed even while load pending; async no user generation/cancel.

- `web/src/pages/FAQPage.tsx` (21 lines): FAQPage delegates context language to FAQLayout.

- `web/src/router/traderSlug.ts` (37 lines): Primary immutable full trader ID exact match correct; legacy name/prefix fallback intentionally ambiguous first match. No authorization itself.

- `web/src/stores/agentChatStore.ts` (42 lines): Global Zustand chat state supports resetForUser but async updater has no user generation check; stream consumers must scope callbacks. Not persisted by store itself.

- `web/src/stores/tradersConfigStore.ts` (108 lines): Global config store derives configured status by credentials/enabled, compares special venues by e.id rather than exchange_type. Unauthenticated load does not clear prior private arrays; pending authenticated loads can repopulate after reset. Errors logged/swallowed, consumer cannot use rejection for backoff.

- `web/src/stores/tradersModalStore.ts` (75 lines): Global Zustand modal/editor selections, reset explicit. Close clears selected model/exchange; user change cleanup caller responsibility.

- `web/src/types/agent.ts` (15 lines): Type-only AgentStep status union lacks failed; persisted messages have no runtime schema validation.

- `web/src/types/config.ts` (183 lines): Config contracts include UUID exchange id distinct from exchange_type, legacy NT CSV settings, and onboarding secret response; no runtime enforcement.

- `web/src/types/index.ts` (3 lines): Type barrel only.

- `web/src/types/strategy.ts` (294 lines): Strategy/grid/risk/day-plan type contracts; defaults and acceptance comments can drift from Go; no runtime enforcement.

- `web/src/utils/traderColors.ts` (31 lines): getTraderColor derives palette from current list index, so reorder changes identity color; missing ID uses first.

- `web/vite.config.ts` (41 lines): Vite loopback3000 proxy8080; no-store modules, product branding HTML replacement. No strictPort, can autochoose another port. Product name is local trusted text.

- `web/vite.sandbox.config.ts` (15 lines): Sandbox loopback3001 strictPort proxy8081, lacks main visible-product-name HTML transform. Separate config not identical plugin behavior.

- `web/vitest.config.ts` (27 lines): Vitest jsdom setup and explicit branding filesystem allowance, excludes E2E suites. Source-only config read; no test execution.

---

<a id="section-31"></a>

> Original: [reviews/26/report.md](reviews/26/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 26 — web shell, charts and chat

Source baseline: `63968be62e44db2fb07a92883e02127b9064b0be`, isolated `/tmp/vl-understanding-market-20260913`. [A] All 59 assigned files / 12,309 lines read in full; hashes match manifest; unread zero. Worktree clean before and after. No source modifications, browser/live requests, settings writes, process actions or tests executed. This is source review, not incident reproduction. [A] denotes exact source inspection; [B] denotes consequences inferred from that source. All concerns below are static unless stated otherwise.

`reads.json` records per-file semantics and seven additional dependency/test reads. `functions.json` inventories 235 named functions/wrappers, with 314 anonymous callback entries grouped by file (some callbacks belong to named wrappers). It preserves catalog source locations/excerpts and syntax-visible calls; these calls are not type-resolved. Named helpers are not omitted merely because nested. `graph.json` preserves 119 historical nodes /352 touching edges separately from current source imports. The July10 graph and CGC historical781file/5095function index are navigation aids, not current validation.

Standing instructions were read during this worker's preceding waves: canon `07b53e65`, audit checklist `dfda15e1`, SYSTEM-MAP `565e8fbe` (recorded log evidence in review23). This read-only slice references their ownership, source-of-truth, no fabricated values and producer/consumer test rules. Owner's daily-loss-only correction takes precedence over older per-trade-cap prose. No new implementation/spec was built here.

### Actual control and data flow

[A] `main.tsx:1` mounts StrictMode/BrowserRouter/Toaster. `App.tsx:7` nests Language, Auth and confirmation contexts plus SandboxBanner around `AppRoutes`. `AuthContext.tsx:61` restores local credentials after system config, `isJwtExpired:13` decodes expiry, and the provider exposes login/admin/register/reset/logout. `AppRoutes.tsx:453` owns setup/auth routing: agent and FAQ are public page routes; protected views depend on user context; unknown paths redirect home rather than use the standalone PageNotFound component. HeaderBar only controls navigation; backend authentication remains the enforcement boundary.

[A] `DashboardRoute:228` loads trader/exchange catalogs, derives selected trader using `selectedTrader.ts:74`, then polls status/account/positions/decisions/statistics. Selection prioritizes URL, valid memory, saved ID, deterministic name/ID fallback. Account selector data uses the shared accounts key; API data wrappers add account query only with a trader ID. Response TypeScript interfaces provide no runtime schema validation. `AccountInfo.ledger_day_*` is optional, reflecting unavailable computation rather than mandatory zero; generic formatters still replace missing/NaN numbers with zero.

[A] Config API wrappers handle model and exchange CRUD, catalogs and onboarding. Optional encryption fetches server flag/public key, initializes CryptoService and uses stored user/session AAD; NinjaTrader create is plain JSON. Telegram and strategy wrappers use HTTP client business envelopes. Backend gates, not these forms, determine mutation authorization. `guarded.ts:22` converts unsuccessful envelopes into ApiError; wrappers that ignore success can instead display empty data. Additional `httpClient.ts` source confirms bearer injection at request time, 30s default timeout, 401 storage cleanup/global event/full navigation, and returned business failure for some response errors. Fetch-based components do not inherit those interceptors.

[A] AgentChatPage's module-global reader/controller and Zustand store retain a stream across route navigation. `runAgentStream:103` adds user/bot messages, persists, marks loading, POSTs and parses SSE plan/step/delta/replan/done/error. `parsePlanSteps:401`, `parseStepEvent:414`, `appendStep:391` and completion helper structure the timeline. Visible times use America/Chicago. `persistMessagesSnapshotForUser:82` stores last100 snapshots with streaming false. `/clear` clears local history/draft and still sends the command to server. `stopActiveAgentStream:53` changes visible messages then aborts/cancels browser transport. No backend stop acknowledgment is obtained here; whether already-running backend tools stop is outside this slice.

[A] AdvancedChart fetches klines (`:215`), order records (`:342`), NT position-derived markers (`:457`) and pending orders (`:524`). Data effect `loadData:737` sorts/deduplicates millisecond-open timestamps into chart seconds, populates candles and volume, calculates SMA/EMA/Bollinger, derives markers and calls server SVP. `loadOpenOrders:1054` manages price lines independently; `syncSVP:1274` attaches the server profile rather than recomputing the engine's profile in browser. Chart teardown removes chart/observer/intervals, but ongoing request ownership is not invalidated. Pixel primitives are pure alignment arithmetic. ChartWithOrdersSimple is only a diagnostic count/status card.

### Priority findings and exact boundaries

1. **SSE event headers do not survive transport chunk boundaries.** [A] AgentChatPage `:182–218` retains unfinished text in `buffer`, but initializes `eventType=''` inside each `reader.read()` iteration at208. A complete `event: delta\n` in one read followed by `data: ...\n` in the next loses the event type and drops the payload. Final buffered text/decoder flush is also absent. [B] Actual arbitrary network chunking can omit deltas or step events. No browser reproduction performed. Test the production parser with every split position, including CRLF, partial Unicode, final line and multiline data. Prefix-based `mergeStreamText:191` also conflates repeated deltas with cumulative text.

2. **Old chat requests can clean up newer requests and persist the wrong user's snapshot.** [A] Stop at78–79 cancels and immediately sets loading false. A new request can begin before the old catch/tail completes. Tail379–387 releases/nulls the *global* reader and unconditionally clears loading; only controller cleanup is ownership-checked. User switch reset at498 changes the global store but does not abort the old request. Patch helpers persist the whole current store under the request's captured storageUserId even when its botId no longer exists. [B] A late old response/catch can clear new loading/reader ownership or copy current global history into an older user's local key. This is browser-local identity isolation, not demonstrated server-side data disclosure. Guest migration in `agentChatStorage.ts` intentionally copies guest/legacy history to any empty user key and does not delete the source, as its tests assert. Storage writes can throw before the stream try block or during catch, preventing normal cleanup.

3. **Chart response ownership and toggle closures are stale.** [A] `loadData:737`, `loadOpenOrders:1054` and `syncSVP:1274` lack abort/generation checks. Clearing intervals does not cancel in-flight promises, and chart refs remain populated after removal. [B] Switching symbol/timeframe/trader can let an older response paint the current chart; toggle-off during SVP fetch can reattach it. `showOrderMarkers` is captured by the data effect, while a separate toggle effect runs1171–1184; later data refresh can restore a hidden marker series. Volume toggle is read from the data-effect closure805–822 and is not managed by updateIndicators1187. Empty marker results clear the series but leave cached marker data available for toggle-on. Pending-order fetch clears lines before fetching and returns empty on errors, hiding unavailable coverage as none. Removing traderID early-returns without clearing previous lines. These are display/lifecycle risks, not executed orders.

4. **Fills in the latest forming candle can disappear.** [A] `findCandleTime:869–870` rejects orderTime greater than the maximum candle timestamp, which is the last candle's *open*. Thus a fill after that open and before the next bar can be rejected; historical gaps map to the previous candle without a maximum-gap check. [B] Markers can appear late or imply association across missing bars. NT marker path uses closed historical positions for exits; order records cap200 and there is no selected-account prop/query in this component. Selected account chart parity requires call-site/backend work outside this slice; do not assume chart overlays are account truth.

5. **Comparison histories can be assigned to the wrong trader after reorder.** [A] ComparisonChart43–49 sorts IDs only for the SWR cache key, but fetcher55–69 returns histories in original input order. CombinedData107–109 rejoins by current array index. [B] Reordering the same set under the same key can relabel existing cached curves. This component is retained legacy competition UI; current route reachability is not established here. Additionally missing history/PNL become empty/zero, forward filling carries stale values indefinitely, and the last500 minute buckets can cover far less than selected7/30days. Labels use viewer local time (and `all` hours0 selects intraday formatting).

6. **Malformed saved auth can prevent hydration completion.** [A] AuthContext61 onward parses savedUser in the success path and repeats parsing in catch; malformed JSON can reject the async hydration without setting loading false. Expiry has no timer or storage-event synchronization; missing exp is accepted locally (backend still validates JWT). LoginPage mount removes token/user storage while existing AuthContext may still identify a logged-in user. Logout leaves user_id and selection/global caches. SWR keys such as trader/exchange dashboard lists are not user scoped. [B] Authentication transitions can present stale UI until refetch/full navigation; no server authorization bypass demonstrated.

7. **Error states are represented as empty or crash rather than unavailable.** [A] `lib/config.ts:11` caches rejected config promise until invalidation and does not require response.ok. `ResolvedKnobPanel:80–89` parses any JSON and then reads `data.summary` at107; a401/error payload can crash rendering. It never resets an existing error on subsequent successful request and retains previous data during identity change. Live flag correctly prevents prior-effect writes. PositionsPanel selects first running trader, interprets unavailable positions as no positions, and classifies alphabetic MNQ as shares; it does not use the shared CME helper. Preferences load does not check `success`, and its canceled flag does not protect in-flight requests. SandboxBanner hides on absent config; WebCrypto check treats failure as encryption disabled. None proves runtime protection absent.

8. **Warmup EMA can throw.** [A] `utils/indicators.ts:34` seeds from the first period entries without guarding `data.length < period`; AdvancedChart calls it for enabled overlays. SMA/Bollinger naturally return no points for short input; EMA does not. Invalid periods and nonfinite values are not validated. Generic formatPrice/Quantity/Percent convert null/NaN to0; that violates unavailable-versus-zero presentation when callers pass uncomputed values.

### Other semantics, negative checks and retained UI limitations

[A] The reset-password form looks security-sensitive but `api/server.go:143` routes to `handleResetPasswordDisabled`, which always410 (`handler_user.go:215`). The reset-account action is now behind auth and additional gates. These are stale frontend flows, **not** a current public account-takeover finding. MessageRenderer allows only http(s) anchors and React-escapes text; no dangerouslySetInnerHTML path was found. Its regex split uses nested capture groups (renderInline7), duplicating link label/URL pieces; unclosed code fences are not emitted at end (renderMessageContent71). ChatInput clears draft before onSend and removes its keyboard listener correctly.

[A] AdvancedChart77 returns USDT for NinjaTrader, base unit is symbol text rather than contracts; tooltip date formatting remains viewer-local although shared axis helpers use CT. Historical graph's claim of CME unit adaptation is false at this baseline. Generic order_action recognition uses uppercase OPEN/CLOSE while the earlier runtime23 producer review found lowercase forms; B/S marker side still comes from raw side, so do not overstate an execution-direction error from unused defaults.

[A] HeaderBar desktop/mobile paths both auth-gate protected tabs and clean outside-click listener; selection of beginner mode is local navigation state. MetricTooltip's KaTeX only renders static formulas; it is not the engine's calculation. VLSelect lacks keyboard listbox semantics and option click remains enabled if disabled changes while open. Radix dialog/input wrappers are styling/ref forwarding; confirmToast returns false if no provider. ExchangeIcons has no image-load error fallback despite historical graph prose. PunkAvatar/getTraderAvatar are deterministic cosmetic helpers; getTraderAvatar returns a seed string, not an element. SiteFooter renders translations, not historical external branding links.

[A] FAQ content observes only elements present during an effect keyed by callback; changed filtered categories can introduce unobserved refs, and cleanup traverses current map rather than disconnecting all. Bespoke `github-projects-tasks`/`contribute-pr-guidelines` branches do not match current catalog IDs; placeholders href# and obsolete competition/community entries remain. The complete catalog, translations and decorative files were read, not treated as generated/excluded. Plan translations enforce en/zh/id and interpolate params; strategy translations use zh/en/es, so Indonesian falls back English. Flat spread merges overwrite duplicate names, including grid percentage dailyLossLimit with futures USD label. These strings do not prove corresponding current engine knobs are live. No extra mandatory per-trade risk cap is recommended.

[A] stamp-guide-rev.sh obtains revision from a running health endpoint, rejects empty/short hex and rewrites guide source; curl does not require HTTP success and sed replacement count is unchecked. It was read only, never run. Preview config is a tracked baseline file despite saying untracked; port/proxy comments cannot prove8232 backend is isolated. PostCSS/Tailwind are build/style declarations; vite-env and trading interfaces only compile-time contracts. Vitest setup mock does not model quota/security exceptions and differs for stored empty strings.

### Verification limits and follow-up coverage

[A] Full read of selectedTrader tests covers precedence, deleted ID, stable sorting and empty lists; it does not exercise DashboardRoute's effect/cache transition. Full agentChatStorage tests assert guest fallback/copy, clear and streaming snapshots; they do not exercise runAgentStream races/SSE chunking. Tests were not run. No offline reproducer was created because parent owns the test/repair plan. Source review supports the mechanisms above; actual user incidents, chart library behavior after remove, and backend stream cancellation remain unverified.

Historical graph corrections include nonexistent current calculateMACD/RSI/EMAFromValues functions, outdated findTraderBySlug ownership, claimed current-password reset fields (actual email/new password), inaccurate quote-unit support, ExchangeImage error fallback, FormulaRenderer renderToString (actual katex.render), avatar return type and footer dependencies. Its AppRoutes404 description disagrees with redirect behavior. Current source and current imports take precedence; retained historical edges are explicitly labeled historical rather than silently made current.

Root reported five remote dependency notices (2 high,2 moderate,1 low); package manifests/advisories were not assigned or investigated here. They remain unverified notices, not confirmed vulnerabilities in this slice. No whole-repository security or runtime correctness conclusion follows from these59 files.

---

<a id="section-32"></a>

> Original: [reviews/27/report.md](reviews/27/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment27 — trading controls, settings and dashboard

Baseline `63968be62e44db2fb07a92883e02127b9064b0be`, isolated `/tmp/vl-understanding-execution-20260913`. **17/17 assigned files, 9,463 lines fully manually read; zero unread.** Census:121 syntax-catalog named functions plus two manually identified `useCallback`-bound functions (123 records),317 anonymous callbacks grouped under containing functions. Nineteen dependency/test files inspected in full or recorded excerpts. Worktree porcelain clean, exact baseline verified after reading. No source/live settings/account/DB writes, service calls, tests or browser reproductions.

[A] below means exact source inspection, [B] means inferred runtime consequence. No static finding is called a demonstrated incident. This is one repository slice. Shared review instructions and prior assignment canon/checklist/SYSTEM-MAP/RULEBOOK reads apply. Freshness reference: checklist `dfda15e1 2026-09-13T00:48:28-05:00 test: isolate session clock fixtures from weekly backfill workers`; SYSTEM-MAP/RULEBOOK `565e8fbe 2026-09-13T00:39:02-05:00 fix: use owner daily-loss controls without requiring a per-trade cap`. Fixed-baseline scope does not rebase to later repairs.

### Ownership and data flow

`AITradersPage.tsx:30-792` owns trader/model/exchange CRUD dialogs and callbacks, fetches authenticated configs and polls traders every5s. `ConfigStatusGrid`, `TradersList`, `TraderRow` and `ModelCard` are controlled presentations; they cannot mutate backend state without parent callbacks. Create/edit data flows `TraderConfigModal.handleSave:183` → `handleCreateTrader:204`/`handleSaveEditTrader:240` → API. Model writes preserve row-ID distinction for multiple models of one provider; deletion refuses existing bindings. Exchange writes use encrypted request helpers. Backend remains responsible for ownership and SIM admission; UI enablement is not an authorization boundary.

`StrategyStudioPage.tsx:110-1674` owns local config/metadata drafts, normalizes legacy root AI fields into `ai_config`, preserves root `day_plan`, and composes `CoinSourceEditor`, `IndicatorEditor`, `RiskControlEditor`, `PromptSectionsEditor` plus sibling editors. `handleSaveStrategy:552-606` guards HTTP PUT and keeps edits on failure. Backend `api/strategy.go:305-340` merges omitted fields, preserves AI config across unconfirmed type switches and validates indicator periods. Preview (`:694`) uses example equity1000; test (`:741`) explicitly requests real AI and may incur provider calls. Neither was invoked. First static symbol drives futures presentation and preview market variant; this is an inference from config, not a live venue read.

`TraderDashboardPage.tsx:133-1256` receives account-scoped status/positions/decisions from `AppRoutes`, composes independently fetched plan/chart/equity/history/audit surfaces, and offers pause, emergency flatten, per-position close and account selection. `AppRoutes.tsx:315-362` includes selected account in SWR keys. `DecisionAudit` separately fetches trader/account audit records. Browser CT formatting and timers do not enforce trading sessions. Health correctly says PROCESS RESPONDING rather than claiming market-data health. Overview content stays mounted while CSS-hidden, retaining draft/poll state.

Pause invokes trader pause/resume endpoints, with failure propagated by `web/src/lib/api/traders.ts:76-92`. Per-position NT close uses the running TCP trader bound to its active account (`api/handler_trader_status.go:154-188`); response says command sent, not fill confirmed. Emergency Flat uses a separate handler whose TCP success calls `MaybeForceFlat` and global daily-reset helper (`api/handler_risk.go:105-125`, `kernel/engine_analysis.go:1106-1118`, `kernel/risk_limits.go:216-222`). This reset side effect is real; UI must not imply an HTTP response proves all positions closed. No account or order operation was executed.

### Main findings

**27-1 [A source, B draft loss] Trader form resets on parent refresh.** `TraderConfigModal.tsx:147-171` initializes local state in an effect depending on `availableModels` and `availableExchanges`, even in edit mode. `AITradersPage.tsx:151-167` rebuilds those arrays using `filter` on every render and passes them at `:730-750`; its trader poll is5s (`:112-118`). A parent render triggered by updated running/trader data or unrelated UI state can therefore restore the original form values while the owner types. SWR may suppress identical-data renders, so do not claim an unconditional five-second reset; the fresh-array parent-render path itself is definite. Editing and creating are both affected. Local form typing alone need not rerender the parent. Test the real parent/modal composition with changed poll data, not stable mocked array props.

**27-2 [A] Consecutive-loss “off” does not switch the breaker off.** `RiskControlEditor.tsx:859-881` renders enabled only for a positive knob and sets zero when toggled off. `trader/session_risk.go:59-67` explicitly defines zero as unset, then resolves `BREAKER_HALT_N` with default8; `auto_trader_orders.go:120-129` calls that same resolver. With ordinary defaults the owner sees off while the breaker still refuses at eight. The row also sits under a master-switch description despite this breaker being independent. Correct the resolved-value/control contract; do not assume the owner authorized changing any risk setting.

**27-3 [A] NT exchange edits discard NT-specific inputs.** `ExchangeConfigModal.tsx:478-502` sends edited directory/instrument/quantity to its callback. `AITradersPage.handleSaveExchangeConfig:513-597` accepts them but omits them from the update body (`:541-559`), including them only in create (`:562-584`). `api/handler_exchange.go:260-270` falls back to existing values when omitted. Thus the page can toast updated while NT inputs remain unchanged. This is analogous to the historic cadence-field omission but a different callback contract. Directory is legacy CSV context; the finding does not propose rewiring CSV or modifying SIM account bindings.

**27-4 [A source, B draft loss] Changing interface language replaces custom prompt sections.** `StrategyStudioPage.tsx:298-335` fetches language defaults and assigns all `prompt_sections`, regardless of custom text or dirty state, then marks changes. It neither confirms replacement nor binds the asynchronous response to the strategy that initiated it; switching strategy before the response can replace that new draft's sections too. The changed text can affect futures role/frequency: exact consumers are `kernel/engine_prompt_futures.go:99-104,142-146`. This is local draft mutation until Save, not automatic backend loss. The prompt editor itself (`PromptSectionsEditor.tsx:133-135`) displays defaults for empty strings; a blank frequency section therefore displays stock prose although the inspected futures builder omits an empty frequency section.

**27-5 [A source, B misleading evidence] Preview and real-AI test results are not tied to their request generation.** `StrategyStudioPage.fetchPromptPreview:694-725` writes any arriving response; the500ms debounce (`:733-739`) cancels only not-yet-started timers. A slow earlier request can replace the latest preview after edits or a strategy switch. Test responses have the same selection problem (`:741-773`). The market/mode controls reflect current draft while the displayed result may belong to another draft. Static asynchronous-race risk, not reproduced network timing.

**27-6 [A] Decision audit can remain in error after recovery.** Initial request catch sets `error` (`DecisionAudit.tsx:132-136`). Successful background callback (`:147-159`) sets records only; rendering returns the error panel first (`:171-180`). `useAutoRefresh` catches/retries with backoff (`web/src/lib/autoRefresh.ts:128-188`), so successful data can arrive while the UI remains error-latched. The callback also lacks identity cancellation for an already-in-flight background request, unlike the initial effect; an old trader/account response could temporarily overwrite new records. No API data was queried.

**27-7 [A] Close-position refresh misses current account caches.** `TraderDashboardPage.tsx:291-299` invalidates only `positions-${trader}` and `account-${trader}`. Actual keys include nonempty account suffix (`AppRoutes.tsx:315-350`). The account-selector handler already uses predicate invalidation (`TraderDashboardPage.tsx:496-511`), illustrating the correct key family. Normal5s polling eventually refreshes data, or later if it is backing off; this is not permanent data loss. Close toast says closed after a handler whose NT response says only sent (`handler_trader_status.go:185-188`).

**27-8 [A] Futures display uses incompatible monetary units/defaults.** Dashboard position Value at `TraderDashboardPage.tsx:1026-1028` is quantity×mark price, omitting contract point value. The producer explicitly uses `market.FuturesPointValue` (`trader/ninjatrader/tcp_trader.go:981-986`), with MNQ$2/point; a one-contract price100 position has $200 notional, not100. Risk editor labels `MNQ ≈ $2/pt` under “Margin / contract” (`RiskControlEditor.tsx:1068-1074`, translation `strategy-translations.ts:635-639`): point value is not margin. Estimated contract ceiling falls back10 (`RiskControlEditor.tsx:1059`) despite resolver passing through Stage-A clamp (`kernel/risk_limits.go:372-393`, default1). Values should be resolved/read or labeled illustrative; no sizing recommendation is made.

### Additional constraints and lower-priority limitations

* `StrategyStudioPage.tsx:677-685` sets AI config undefined when switching to grid, but only caches grid config. Switching back before saving (`:663-670`) cannot recover the unsaved AI draft. Futures grid selection is disabled (`:1251-1256`), limiting this to compatible strategies. Backend unconfirmed-switch preservation prevents concluding persisted AI config destruction from this local issue; the UI never sends `confirm_type_switch` despite the browser confirmation.
* `IndicatorEditor.tsx:222-235` calls parent `onChange` during render, including disabled/default templates, when raw-klines is false/missing. Parent `updateAIConfig` marks dirty; this can produce React render-update warnings and changes a draft without an explicit edit. No browser test run.
* Double-clicking a timeframe also dispatches ordinary toggle clicks (`IndicatorEditor.tsx:1011-1016`), while `setPrimaryTimeframe:203-212` does not ensure selected membership. A primary/selected inconsistency is possible; backend capability/validation is separate. Period input permits positive numeric prefixes, but inspected backend period validation rejects absurd saved periods, so no crash claim.
* `PauseButton.tsx:59-69,108-154` sets busy but its menu duration buttons/custom submit and handler lack a busy guard. Multiple pause calls are possible while the menu remains open. Failure is console-only. `EmergencyFlatButton.tsx:24-45` shows all raw JSON in green, including `triggered:false`; no fetch timeout. Triggered count in backend is not a fill receipt.
* `TraderDashboardPage.tsx:158-161,230-234,846-857` retains chart symbol across ordinary trader changes and passes it into PlanCard. `AppRoutes.tsx:418` does not key-remount the page. Switching from another symbol/trader can feed old chart context into the next plan request. No observed wrong trade is claimed.
* `AITradersPage.tsx:338-343` refuses opening a running model even though `ConfigStatusGrid.tsx:115` tooltip promises edit access. It also supplies no account-state read model to the grid (`:694-711`), so those badges stay NOT CHECKED; SYSTEM_READY is unconditional (`:638`). These are UI truth problems, not proof the backend is ready/unready.
* `DecisionAudit.flattenDecisions:65-92` invents wait/zero fields for an empty action list; an uncomputed/failed cycle should not be presented as an authored wait. Parent risk/fill/latency are repeated for every action; consumers must not read these as separate per-action execution receipts. Missing risk flag becomes failed rather than unknown. No raw-account rows read.
* VLOS “connected” only checks a nonempty key (`IndicatorEditor.tsx:244,298`), and the deprecated default key remains offered on crypto. CME correctly hides these feeds/OI/funding. No service health verification was attempted. `CoinSourceEditor` displays Static whenever first symbol is futures while preserving saved source_type, so display is not proof the saved source was normalized.
* Authentication-cache retention and null optional-field crashes were not traced to reachable current failures. Do not promote them to exploits. No security test or external API call was made.

### Owner policy, tests, historic graph

The reviewed `RiskControlEditor` has daily-loss controls and **no mandatory per-trade dollar cap**. `RiskControlEditor.test.tsx:6-31` explicitly verifies the owner policy and editing the daily limit. Keep that policy; the misleading breaker off-label and unit displays are separate problems. Existing guide contradictions are recorded in assignment24, not rediscovered here as engine defects.

Tests read, not run: `RiskControlEditor.test.tsx` daily-loss-only UI; `editPutFieldAudit.test.ts` ensures trader modal keys survive the trader update callback (does not cover exchange NT fields); `TraderDashboardPage.test.tsx` checks composition, mobile columns and keeping a mock PlanCard draft mounted while changing tabs (does not cover symbol/trader changes, account-suffixed invalidation or actual child form reset). Relevant fixes should exercise production callsites and asynchronous identity changes, not merely mirror helper arithmetic.

Historic Understand Anything excerpt has48nodes/143edges from July10@7a8adce0, read and saved. Several summaries are false today: VLOS key is not validated via SWR, PeriodInput does not upper-clamp, ModelCard does not show price, trader modal has no initial-balance picker, and config normalization does not fill arbitrary missing defaults. `graph.json` preserves history separately from86 current relative-import edges. No CGC mutation/reindex; source outranks historic graph. Syntax catalog misses two named useCallback variables, explicitly added at `StrategyStudioPage.tsx:179-198,201-246`.

### Complete per-file role ledger

* `web/src/components/strategy/CoinSourceEditor.tsx`: Static/dynamic crypto selectors and exclude list; first static symbol alone selects futures display, hides active crypto source without changing saved source_type. Duplicate symbol canonicalization in add/exclude; uppercases CME continuous suffix before recognizer, inspect shared classifier. Futures label is inference not backend-venue read.

* `web/src/components/strategy/IndicatorEditor.tsx`: Backend-supported timeframe selector withlocalfallback, multi-perioddraft parseonblur, softcountwarning. ensureRawKlines callsparentonChange duringrender even disabled. Doubleclick primary also fires toggle clicks and setPrimarydoesnotensuremembership. PeriodparseInt acceptsnumericprefix no uppercount bound. Crypto VLOS presentkeylabeledconnected withoutprobe; staticdeprecateddefaultkey retained. Quant childclick can bubble parenttoggle despiteonChange stopPropagation; verify Reacteventroute. Futureshidescryptofeeds/OI/funding correctly.

* `web/src/components/strategy/PromptSectionsEditor.tsx`: Controlled four prose sections, futures Role/Decision defaults; reset persists local literal defaults. getValue uses || default, so clearing textarea repopulates displayed fallback; draft empty value not same as explicit default. Verify backend prompt uses these knobs in strict/futures mode.

* `web/src/components/strategy/RiskControlEditor.tsx`: Controlled risk rows, blur-commit clamped number helper, crypto-only leverage/PVR/margin/minsize hidden on futures. Daily limit+master controls and always-on size caps; no mandatory per-trade dollar cap. Consecutive halt toggle maps0/off and2/on despite production0 likelydefault8; maxcontracts preview fallback10 and margin label MNQ$2/pt (pointvalue notmargin). Need backend-default/translated-label verification; staticdailyenv500 claim.

* `web/src/components/trader/AITradersPage.tsx`: SWR trader poll5s, config loads/refresh events, controlled CRUD/model/exchange modals. Enabled arrays filter each render and feed reset-dependency modal. Model in-use click still blocked contrary grid tooltip permitsediting. NT8 fields passed into save handler but omitted update payload (present create); body-scoped exchange edit otherwisecorrect, model save all rows maystale overwrite. No accountstates passed =>NOTCHECKED. SYSTEM_READY unconditional; countACTIVE_NODES includesstopped. Unauthenticated configload leavespriorallModels/exchanges state, sharedSWR keytraders lacksuserid; check auth routing/cache scope beforesecurityfinding.

* `web/src/components/trader/BeginnerGuideCards.tsx`: Four-step crypto/Claw402 quickstart; ready flags supplied by parent; static exchange lists not NT8/SIM-oriented, no own writes.

* `web/src/components/trader/ConfigStatusGrid.tsx`: Controlled model/exchange overview, usage counts, account-state statuses unknown distinct; DEX address toggle/copy delegates callbacks. Running model edit remains clickable intentionally; exchange inUse only style, parent decides policy.

* `web/src/components/trader/DecisionAudit.tsx`: Flatten actions with fabricated wait+zero fallback for empty list; initial fetch error remains latched after successful background refresh because only setRecords runs. Parent fill/risk/latency repeated for each action, action error takes precedence for refusal. Initial request cancelled on identity change, auto-refresh race needs dependency check.

* `web/src/components/trader/EmergencyFlatButton.tsx`: Confirmed trader-scoped POST with bearer, busy prevents close, raw JSON always green; no timeout or r.ok semantics. Claims resets daily PnL window; verify backend, do not call service.

* `web/src/components/trader/ModelCard.tsx`: Controlled model picker, provider icon/fallback initial and configured/selected chips; all mutation delegated onClick.

* `web/src/components/trader/PauseButton.tsx`: Pause/resume API, CT until display, blocks new only; errors console-only; menu action buttons and doPause lack busy guard despite outer disabled button so duplicate pause requests possible.

* `web/src/components/trader/Tooltip.tsx`: Hover/click local tooltip, no keyboard/focus semantics or aria association.

* `web/src/components/trader/TraderConfigModal.tsx`: Fetch strategies on open, initialize from config or first model/exchange, parent-array dependencies can reset unsaved draft; stale async strategies response can overwrite new selection. Create/edit request callback, cadence and ai-watch modes. Margin/competition controls still visible for NT8. Scan numeric min1 only max60 HTML not clamp; blank Number becomes0 then1. Save errors console-only component; parent may toast.

* `web/src/components/trader/TradersList.tsx`: Controlled row/list/loading/empty composition, unique fullID selection, model matching byid. Running edit disabled; start/stop/delete/competition delegated with no perrowpendingguard. Parent required to confirm/error-handle; status is supplied running not exchangeconnection.

* `web/src/components/trader/utils.ts`: Model display-name case mapping and underscore-short-name helper; ModelCard instead imports model-constants helper.

* `web/src/pages/StrategyStudioPage.tsx`: Strategy CRUD/normalization, rootdayplan preserved, ai/grid separation, guarded save, import/export, preview/testrealAI. Focusrefresh protectsdirtydraft. Language change replacesALLcustompromptsections defaults withoutconfirmation and asyncnotselectionbound; AI->grid->AI beforeSave discardsAIconfig. Preview debounceonlynotabort/versionguard sooutoforder/staleafterstrategyswitch; testresultalso. Confignormalizationdropsunknownfields; variantcomposedpreview vs Save onlystoredvariant ifsymbolchanged. Sidebarclickdiscardsunsaveddrafts. Some defaultlocked controls(mode/language/ensureRawKlines)stillchange localdraft, Savehidden. No AI test invoked.

* `web/src/pages/TraderDashboardPage.tsx`: Trader/account-scoped props and health/riskerrorpoll; pause/flat/accountselector, positionsclose, plan/chart/equity/audit. Close success toast afterAPIresponse notfill receipt; SWR invalidation exactunsuffixedkeys vs accountedkeys elsewhere. Positionvalue qty*price omitsfuturespointvalue. selectedChartSymbol persistsacrosstraderchange and drivesPlanCardsymbol, canleakoldcryptosymbolintofuturesdisplay. 402nullHTTPfailureclearsbanner; networkthrowpolluncaught. MissinguPnL rendered0; zeroavailablebalance percent becomes--. ProcessRESPONDING honest distinctfeed. OverviewCSS-hidden remainsmounted/polling.

---

<a id="section-33"></a>

> Original: [reviews/28/report.md](reviews/28/report.md). Historical baseline review at 63968be62e44db2fb07a92883e02127b9064b0be. Findings and line references retain their original scope; later repair dispositions override only named findings.

## Assignment 28 — trading and settings surfaces

Complete assigned-source review at **63968be62e44db2fb07a92883e02127b9064b0be**, isolated `/tmp/vl-understanding-surfaces-20260913`. **16/16 files, 9,472 lines, 110 named/helper/memo functions, 267 anonymous callbacks grouped under their owner; zero unread assigned files.** Thirty-two dependency/test files were read in full or in explicitly recorded excerpts; those are not added to assigned coverage. TypeScript AST parsing identified function boundaries; manual reads supplied semantics. No application, wallet, Telegram, live database, trading, browser or test requests were executed. Findings are static [A] source evidence and [B] consequences, not reproduced incidents.

Rules used: supplied root AGENTS, CLAUDE-canon, AUDIT-CHECKLIST (pre-audit and current rules), SYSTEM-MAP and trading rulebook excerpts carried from this worker's preceding reviews. Owner daily-loss policy supersedes old mandatory per-trade caps. Latest rulebook-related source log recorded in the review sequence: `565e8fbe Sun Sep 13 00:39:02 2026 -0500 fix: use owner daily-loss controls without requiring a per-trade cap`. This review does not recommend restoring that cap. Parent's repair branch is separate; base findings below are not claims that its later repairs remain defective.

### Actual flow and ownership

[A] Authenticated `AppRoutes:516` mounts SettingsPage. Settings modals edit local draft state; parent handlers construct model/exchange payloads and refresh server lists. API reads redact keys into `has_*` flags. Standard config writes use `configApi` transport-encryption policy; NT create explicitly bypasses encryption because it contains no credential. The Claw402 wallet-validation path is separate and directly uploads a private key in JSON. Model and exchange backend updates collect affected traders and reload configuration. Thus apparently small credential edits can affect active trader configuration; these are not harmless display-only changes.

[A] StrategyStudio mounts grid settings only for grid strategies and DayPlanEditor for futures AI strategies. Child callbacks dirty the parent config; the backend/trader resolvers decide effective values. Publish toggles update strategy metadata. TokenEstimateBar calls a pure server estimator and reports count back to the Studio; it does not perform an AI request.

[A] TraderDashboard mounts AccountSelector, server LedgerDayPnl, DecisionCard and PositionHistory; GridRiskPanel is conditional on grid strategy status. Account selection persists a trader binding, not shared NT connection selection. `handler_account.go:88–96` can report the selected account as `current` when its own account snapshot exists. AppRoutes uses this `current` for account-scoped requests. PositionHistory is only passed traderId and its API wrapper never sends account, so its history and metrics remain trader-global.

[A] Telegram handlers read a singleton configuration, not a user-specific record. The UI retrieves enabled models, saves a bot token, asks the user to send `/start`, checks binding on button press, and permits model-only updates/unbind. JWT protection does not by itself make these settings tenant-specific. This review does not characterize Telegram command authorization without its deeper implementation review.

### Findings and corrections

1. **High-priority configuration preservation: NT edit discards loaded values in the draft.** [A] ExchangeConfigModal:228–230 initializes directory empty, instrument MNQ, quantity1; its edit effect:289–304 never loads the three saved NT fields. API `safeExchangeConfigFromStore:72–74` returns them. Submit:469–497 requires entering a directory, then sends the default instrument/quantity. Backend:264–270 preserves only blank instrument/zero quantity; explicit MNQ/1 replaces the existing values at:298. [B] Editing an existing nondefault NT configuration and supplying its directory can inadvertently rewrite its instrument/quantity. No settings were changed to reproduce this. The same panel:1239–1298 still calls the connection a CSV/file bridge and requires a directory despite the current TCP architecture; API validation also retains that legacy requirement, so correcting prose alone would not fix the contract.

2. **Model and exchange blank-key edit promises conflict with browser validation.** [A] ModelConfigModal.handleSubmit:86–101 allows edits without a key and StandardProviderConfigForm explicitly describes rename-without-key, but its password input:1341–1364 remains `required`. SafeModelConfig:29–43 does not return the key. Claw form also requires a syntactically valid full key even in edit mode. Exchange forms:889–981 and submit:401–452 likewise require all saved credentials despite saved-key placeholders. [B] Native form validation blocks ordinary name/URL-only edits before the permissive submit handler runs. No browser test was run; this is a source/HTML-contract finding. Exchange account-name input is editable, but SettingsPage:350–381 and backend UpdateExchangeConfigRequest have no account_name update field, so changing that field cannot rename the row.

3. **Wallet privacy copy is directly false, and stale validation can change a deposit destination.** [A] ModelConfigModal:485–489 and:519–523 POST `private_key` to unauthenticated `/api/wallet/validate`; server:29–90 derives its address and queries balance/health. Its displayed translation `privateKeyNote` at translations:1313–1314 says “Never uploaded.” Generation:748–759 calls a server endpoint that creates the private key. This is not a client-only signing flow. [A] Debounce cleanup only clears a timer; neither automatic validation nor Test Connection guards key generation after fetch begins. Address/balance/QR:971–1009 use whichever response last updates state. [B] Key A's delayed validation can overwrite key B's display, including its deposit QR. The generated backup key is a separate state value and is not reset when input changes; copy callbacks report copied before awaiting clipboard success. Do not treat this as a demonstrated transfer or theft. Static per-call prices are catalog copy, not verified live quotations.

4. **History reintroduces raw P&L while the backend correctly excludes unresolved values.** [A] PositionHistory.effectivePnl:67–72 falls back to realized_pnl; classifyPosition:81–100 classifies ordinary close reasons with NULL corrected values as normal. PositionRow and visibleTotal:678–687 therefore display/sum those unverified raw values. Store GetFullStats:197–250 excludes NULL corrected rows and returns unresolved counts; the UI does not show those counts. `computeDayTotal:135–148` correctly excludes NULLs, so its own day subtotal can disagree with its visible-row total for the same rows. Fixture row581 explicitly exercises NULL exclusion only in computeDayTotal; it does not assert the row/visible-total behavior. This is a current dashboard surface, not an archived analysis script.

5. **History footer is not account-scoped or necessarily a complete day; sorting differs from displayed P&L.** [A] `api/data.ts:165–175` omits account, whereas the header requests an account-scoped ledger. `handler_order.go:235–249` accepts account for position rows but leaves stats trader-global; the current wrapper passes none for either. History loads only max(200,pageSize*5), capped500 server-side. Its Today(CT) sum uses this bounded window, while server header reads the full database window. [B] Accounts or days with more rows can yield discrepant totals despite “same rule” comments. Calendar-date versus CME session-day is an intentional distinction: the header and footer both use CT calendar day; only the comment claiming equality with GetSessionDayActivity is wrong. Header end calculation is fixed24h at:159 and can diverge on CT DST transition days. P&L sorting:651 uses raw realized_pnl instead of corrected display; the unused pnl_pct sorting branch additionally ignores short direction. The select never offers pnl_pct, so that branch is latent. Futures row notional:471 uses price×quantity without contract multiplier.

6. **History refresh can recover data yet remain on a permanent error screen.** [A] Initial fetch:554–578 sets error; background refresh:584–595 only sets data and never clears error; rendering:748 checks error first. [B] A transient initial failure followed by successful polling leaves the error panel until an effect dependency changes. No request generation/abort ties initial or background responses to trader/page identity, so old responses can replace new selection data. Pagination resets on filters/sort/page size, but not trader or duplicate toggle; a shrinking list can strand a page beyond its total. GridRiskPanel similarly retains old trader state, and catches errors before returning to useAutoRefresh, suppressing that helper's rejection-based backoff.

7. **Planner editor and resolver semantics differ at edge values.** [A] UI sessionRunnableUI:309–314 uses `sessions_enabled ?? ['NY']`; backend sessionEnabledForStrategy:77–78 treats an empty list as NY default. Thus [] renders NY off while the resolver allows NY. Backend comparisons trim subset names and match session overrides case-insensitively; UI subset only uppercases and override lookup is exact. Global replan cap allows0 in the editor but store ReplanCapFor:1163 resolves global0 to2 (per-session0 is honored). Realign cap allows0 but AutoTrader.RealignCap resolves it to5. An absent evening_digest displays on for any config object while backend eveningDigestEnabled returns its false zero value. These are field-specific discrepancies, not a reason to reintroduce extra financial gates.

8. **Equals-global override migration is deliberate but loses explicit intent.** [A] migrateEqualOverrides:234–256 strips matching plan_mode/quality on each render and emits a cleaned config on mount. Tests explicitly require this migration; it is not an accidental untested branch. [B] A deliberately pinned session value equal to the global value is indistinguishable from inheritance and will be erased; a later global edit changes that session. The mount effect can dirty a disabled/default strategy because neither it nor parent updateConfig:609 checks disabled/default. Final persistence protections were not exhaustively reviewed here; this is a draft-mutation finding. Session enable has no rendered clear-to-inherit action despite the comment promising one.

9. **Settings mutation and refresh are conflated; bulk model saves broaden the write set.** [A] SettingsPage.handleSaveModel serializes every configured model, not just the edited row; unrelated loaded values are replayed. Its extra-provider create branch:180–196 and CreateModelEntryRequest omit thinking_mode/reasoning_effort even when the form collected them. The new name is generated, not the supplied name. Extra-entry form state is keyed by provider and rendered under every matching provider row:604–653. Model modal does not await onSave or disable while pending. Exchange modal does guard duplicate submit. Success toasts precede awaited refresh in Settings and Telegram; failed refresh then produces a failure toast after a completed mutation. Tab fetch errors show a toast but retain empty/old lists, so “none configured”/“connected” are not reliable request/connectivity status labels.

10. **Decision status defaults and HTML structure deserve correction.** [A] Watch JSON parse failure/unknown accepted_status defaults to a green watch badge and structure `none` green dot:368–406. A neutral guardrail_skip header still has an unconditional red error footer:685–698. Backend feed/clock hints are kept hard-failure; this negative result is preserved. Prompt copy/download buttons are nested inside a toggle button:486–638, invalid interactive nesting. These affect display and accessibility, not execution. Stable timestamp+cycle keys and explicit futures leverage hiding are correctly wired by TraderDashboard.

11. **Account selector does not switch the shared stream, by design.** [A] `handleSelectAccount:194–204` explicitly decouples binding persistence from shared account display; therefore refusing to select an already-bound account is not itself a broken shared-stream switch. Baseline API resets caches before persistence and still returns200 if UpdateAccount fails:206–218; that can produce an apparent UI success without saved binding. Parent's concurrent account-control repair overlaps this backend finding; reassess at repair revision before filing duplicate work. Readonly/list and SIM/allowlist checks were read; ownership middleware internals were not fully traced here, so no new cross-user exploitation claim is made.

12. **Smaller scoped concerns.** TokenEstimateBar:36–67 has no in-flight generation guard, silently retains old estimate on error and does not notify parent count reset for null config; its progress bar uses200,000, not returned model limits. GridConfigEditor uses parseFloat/parseInt `||` defaults and HTML limits, with no handler-level relational price validation; backend authority matters. Publish div switches lack keyboard roles, but disabled mouse writes are guarded. LedgerDayPnl correctly leaves absent P&L unresolved instead of fabricating zero; finite-number guards remain absent. Aster wallet display helper returns signer while ConfigStatusGrid displays main user address, a display consistency issue without a funds-routing claim. TraderConfigViewModal has no current source import/mount found; its old identifier shortening is dormant.

### Tests, graphs and limits

[A] Fully read DayPlanEditor.test.tsx, PositionHistory.fixtures.test.ts and LedgerDayPnl.test.tsx. Tests cover planner defaults, tri-state serialization, expected equal-override migration, enable toggles, hidden legacy clocks, four row classes, corrected day sums and missing ledger state. Ledger/footer parity test constructs the header from the footer's own computed number; it does not test actual production account/window wiring. No assigned-modal/Settings/account test files were found by the targeted filename/reference search; this is not a claim that the repository has no broader tests. No tests ran in this review.

[A] Historical Understand Anything graph July10@7a8adce0: read47 exact-path nodes and142 incident edges for14 of16 assigned paths. DayPlanEditor and LedgerDayPnl are absent. Model/Settings/History/Decision ranges have moved. TokenEstimateBar “per-model limits/suggestions” description is false at baseline. PositionHistory→formatPercent is stale. Several graph `contains` edges represent rendering external components, not lexical definitions; ModelIcons calls belong to StandardProviderConfigForm. Current syntax import graph has60 local edges; manual production connections are stored separately, not mislabeled type-resolved calls. CGC remains a historical service index (781 files/5095 functions), managed by root; no reindex or completeness claim.

All named source functions have exact boundaries, purpose, component/API connections and invariant notes in functions.json. reads.json preserves full assigned hashes and bounded dependency evidence. No data-row claims are made about actual current owner records: fixture IDs cited above are test fixtures. Runtime state, deployed behavior, database contents, price catalogs, browser request timing and dependency security alerts were not validated by this assignment.

### Per-file coverage ledger

- `web/src/components/strategy/DayPlanEditor.tsx` — full 950 lines; Toggle 67–114, Segmented 116–160, NumberField 162–202, FieldRow 204–222, MODE_OPTS 224–228, migrateEqualOverrides 234–257, DayPlanEditor 259–889, DayPlanEditor.update 272–278, DayPlanEditor.sessionOf 281–282, DayPlanEditor.setSessionField 283–296, DayPlanEditor.clearSessionField 297–305, DayPlanEditor.sessionRunnableUI 309–315, DayPlanEditor.toggleTimeframe 318–325, TriStateRow 893–917, OverrideRow 920–950. Full source read. DayPlanEditor controls global and session planner options; helper Toggle/Segmented/NumberField, session CRUD, registry+subset runnable mirror and ordered timeframes. migrateEqualOverrides strips explicit plan_mode/min_scenario_quality equal to global each render; mount effect emits migration even disabled. This cannot preserve deliberate equal overrides when global later changes: trace backend/tests before final finding. Enable toggle writes explicit bool but no rendered clear-to-inherit action despite comment. NumberField clamps parsed floats but not integer steps. Structural buffer raw Number input accepts HTML-invalid values; backend authority pending. Legacy day clocks intentionally hidden; bodyDisabled gates planner widgets; structural stop outside master planner disable. TriStateRow/OverrideRow display inherit versus explicit states.

- `web/src/components/strategy/GridConfigEditor.tsx` — full 474 lines; GridConfigEditor 32–474, GridConfigEditor.updateField 38–45. GridConfigEditor + updateField are controlled crypto-grid editor and disabled guard. Literal defaults BTCUSDT/10grids/1000investment/5leverage; ATR/manual bounds, percent risk and maker/direction settings. Numeric changes parse then fallback with ||; zero replaced by defaults, HTML min/max not enforced in handler and manual bounds lack relational validation. UI leveragemax5 does not alone prove backend enforcement; current callsite pending. Crypto grid settings are not owner futures daily-loss policy.

- `web/src/components/strategy/GridRiskPanel.tsx` — full 467 lines; GridRiskPanel 21–467, GridRiskPanel.fetchRiskInfo 31–52, GridRiskPanel.getRegimeColor 58–73, GridRiskPanel.getBreakoutColor 75–88, GridRiskPanel.getPositionColor 90–94, GridRiskPanel.formatPrice 96–105, GridRiskPanel.formatUSD 107–109. GridRiskPanel fetchRiskInfo GET per-trader grid-risk with auth_token; callback catches and resolves errors so useAutoRefresh advertised backoff cannot observe rejection. Old riskInfo/loading not reset on trader change; no abort/generation. Need actual caller/mount/refresh helper dependency before operational finding. getRegimeColor/getBreakoutColor/getPositionColor thresholds; formatPrice zero->dash, variable precision; formatUSD rounds. Render trusts numeric fields toFixed and known translation keys; positionbar only upperclamped; zero liquidation distance rendered dash. Error branch hides stale data after fetch failure.

- `web/src/components/strategy/PublishSettingsEditor.tsx` — full 184 lines; PublishSettingsEditor 13–182. PublishSettingsEditor controls public/configVisible via callbacks, disabled honored in mouse handlers; div toggles lack keyboard/role semantics; hidden configVisible preserved on private; backend owns visibility enforcement.

- `web/src/components/strategy/TokenEstimateBar.tsx` — full 122 lines; TokenEstimateBar 27–122, TokenEstimateBar.tr 32–32. TokenEstimateBar debounces config POST800ms but does not abort/invalidate in-flight responses or notify parent0when config null; stale estimates persist on error; fixed200K progress ignores returned per-model_limits and suggestions.

- `web/src/components/trader/AccountSelector.tsx` — full 250 lines; AccountSelector 20–250, AccountSelector.handleTriggerRef 59–61, AccountSelector.handleClickOutside 74–79, AccountSelector.handleScroll 82–84, AccountSelector.handleSelectAccount 95–115. AccountSelector correctly separates persisted selected account from streamed current with mismatch warning and disables nonSIM entries. SWR accounts key trader-only and no refreshInterval; endpoint truth may grow stale. Selection early-return on alreadybound selected prevents switching shared stream back when current differs; inspect backend intent. Portal position updates on focus/open not resize; scroll closes.

- `web/src/components/trader/DecisionCard.tsx` — full 703 lines; formatPrice 57–62, calcPctChange 65–74, getConfidenceColor 77–82, ActionCard 85–294, DecisionCard 296–703, DecisionCard.copyToClipboard 307–314, DecisionCard.downloadAsFile 317–327. DecisionCard displays server decisions, raw escaped prompts/reasoning/log, Chicago timestamp, copy/download helpers with URL revoke. ActionCard action->colors, futures flag removes leverage; formatPrice/calcPctChange/getConfidenceColor treat missing/zero via truthiness; absolute-distance RR cannot validate stop side. Watch malformed/unknown JSON falls into green status and none green structural dot. Neutral guardrail skip header still unconditional red error footer. Guardrail feed/clock stays hard failure. Nested copy/download buttons inside prompt-toggle button invalid HTML. No API mutation or AI execution. Need actual mounted caller and server record semantics before severity.

- `web/src/components/trader/ExchangeConfigModal.tsx` — full 1535 lines; StepIndicator 79–124, ExchangeCard 127–185, ExchangeConfigModal 187–1535, ExchangeConfigModal.handleCopyIP 318–341, ExchangeConfigModal.handleSecureInputComplete 350–359, ExchangeConfigModal.maskSecret 361–369, ExchangeConfigModal.handleSelectExchange 371–374, ExchangeConfigModal.handleBack 376–383, ExchangeConfigModal.handleSubmit 385–518. ExchangeConfigModal full source. StepIndicator/ExchangeCard render static supported templates; WebCrypto gates selection only, editing skips. Initialize editing sets CEX/wallet fields but NEVER ntDataDir/ntInstrumentName/ntDefaultContractQty, so NT editing shows empty directory+MNQ+1 defaults and requires reentry directory before save overwrites existing instrument/qty. NT prose still CSV Bridge and mandatory directory contrary TCP current architecture; actual store getter contract pending. CEX fields required and submit require keys/passphrase even saved has_* flags/placeholder imply optional replacement. Back changes provider without clearing credentials so cross-provider stale state possible. handleSubmit guards pending and awaits parent onSave; parent catches errors, retained form. Account name sent but SettingsPage update excludes account_name. NT testnet state defaultfalse but no testnet UI. handleCopyIP fallback execCommand result ignored; timers uncleaned. secure key helper retains child mount on hide (assignment25 finding); Aster secure branch unused rendered direct password. Delete has no local confirmation (parent SettingsPage none). No application calls.

- `web/src/components/trader/LedgerDayPnl.tsx` — full 46 lines; ledgerDayLabel 10–26, LedgerDayPnl 28–46. ledgerDayLabel uses server ledger_day_pnl and resolved/unresolved counts; absent numericPNL prints status; typeof number accepts NaN/Infinity and missing counts default0. Does not recompute trading PNL; corrected-column display positive.

- `web/src/components/trader/ModelConfigModal.tsx` — full 1606 lines; ModelConfigModal 36–262, ModelConfigModal.handleSelectModel 72–75, ModelConfigModal.handleBack 77–84, ModelConfigModal.handleSubmit 86–101, ModelSelectionStep 266–418, Claw402ConfigForm 420–1167, Claw402ConfigForm.getClientError 455–464, Claw402ConfigForm.handleTestConnection 515–548, StandardProviderConfigForm 1169–1606. ModelConfigModal full source. Selection/edit prefers supported catalog if ID matches configured row; missing edit row explicit fallback. Back preserves apiKey/baseURL/name/thinking and can carry to new provider; submit no await/inflight guard. Edit handler allows blank key but StandardProviderConfigForm password input unconditionally required blocks browser submit, and Claw form required/isKeyValid also demands reentry. Claw402ConfigForm validates key regex then automatically POST plaintext private_key to wallet/validate with no auth header; debounce cleanup cancels timer only, in-flight old key can overwrite wallet address/balance used for deposit QR and copy. Generate wallet POST server-generated key, backup plaintext remains if apiKey later changed; copy does not await error and reuses copiedAddr flag. Validation defaults unknown balance to0.00 and compares connectivity only; save allowed by syntax not server validation. Claw submit says startTrading but parent only saves model config. Standard provider model/URL/name/thinking fields controlled; wallet provider model selection literals. No wallet/network calls executed. Actual wallet route/generator/security/privateKeyNote translation trace pending.

- `web/src/components/trader/ModelStepIndicator.tsx` — full 41 lines; ModelStepIndicator 9–41. ModelStepIndicator visual stepper; labels/current index control completed checks and active styling.

- `web/src/components/trader/PositionHistory.tsx` — full 1280 lines; formatNumber 21–29, formatDuration 32–37, formatDate 40–51, effectivePnl 67–72, classifyPosition 81–100, duplicateOfId 103–118, isTodayCT 121–130, computeDayTotal 135–148, StatCard 151–208, SymbolStatsRow 211–255, DirectionStatsCard 258–338, PositionRow 341–531, PositionHistory 533–1280, PositionHistory.fetchData 554–573, PositionHistory.uniqueSymbols 598–601, PositionHistory.classified 606–616, PositionHistory.filteredAndSortedPositions 619–666, PositionHistory.visibleTotal 678–686, PositionHistory.dayTotal 690–690, PositionHistory.paginatedPositions 702–705, PositionHistory.profitLossRatio 711–717. PositionHistory full read. effectivePnl corrected number else realized||0; classifyPosition normal unless unknown/test reason, so ordinary NULL corrected rows render/sum unverified fallback, contrary corrected-only canon, while computeDayTotal excludes them. Duplicate evidence only matching entry_order_id in fetched window; normal rows never deduped. computeDayTotal Chicago calendar date over bounded fetched max(200,pageSize*5), no unresolved count, differs session-day ledger boundary potentially. PNL sort uses raw realized vs corrected displayed; pnl_pct branch ignores short direction but not offered in select. PositionRow value=price*quantity misses futures multiplier; unknown exits still show exit_price/default0. Initial effect and background fetch can race across trader/page changes; successful background poll does not clear initial error, permanent error view until dependency reload. No pagination clamp/reset trader or duplicates change; shrinking visible rows can strand page beyond total. Row stats fetched separately global scope, nullstats fallbacks fabricate zero; netPnL subtracts fees (check backend gross/net). Anonymous refresh/callbacks grouped under component. Test/caller/backend trace pending.

- `web/src/components/trader/TelegramConfigModal.tsx` — full 515 lines; StepIndicator 10–42, TelegramConfigModal 49–447, TelegramConfigModal.handleSaveToken 80–103, TelegramConfigModal.handleUnbind 105–119, TelegramConfigModal.ModelSelector 124–156, BoundModelSelector 451–515, BoundModelSelector.handleSave 468–480. TelegramConfigModal StepIndicator and ModelSelector guide create-token/save/check-bind/unbind; loads config/models independently swallowing errors to null/[] so outage resembles unconfigured. Token regex locally validates, save/unbind duplicate guards, success toast before refresh so refresh failure emits contradictory failure after mutation. No polling for binding: explicit check-status. BoundModelSelector syncs prop model, saves model-only endpoint; selector remains editable during save and parent callback captures submitted model. Modal token only cleared after full save+refresh; no close cancellation; actual backend binding ownership and supported control commands pending. No Telegram requests executed.

- `web/src/components/trader/TraderConfigViewModal.tsx` — full 147 lines; getShortName 7–10, TraderConfigViewModal 18–147, TraderConfigViewModal.InfoRow 26–39. TraderConfigViewModal display-only config; getShortName extracts suffix not actual provider from UUID-based IDs; scan interval missing0 defaults3; relies on typed balances; no fetch or write.

- `web/src/components/trader/model-constants.ts` — full 312 lines; getModelDisplayName 26–37, getShortName 40–43, getExchangeDisplayName 254–270, isPerpDexExchange 273–277, getWalletAddress 280–302, truncateAddress 305–312. Static model/provider catalog and display helpers. Pricing/default strings are source literals not verified service prices. DefaultClaw402 flash matches policy only if backend default same. getShortName suffix lossy for UUIDentries; getExchangeDisplayName does actualIDlookup; Aster wallet address helper returns signer rather than main holder.

- `web/src/pages/SettingsPage.tsx` — full 840 lines; configBadge 27–39, SettingsPage 41–840, SettingsPage.refreshModelConfigs 85–92, SettingsPage.refreshExchangeConfigs 94–97, SettingsPage.handleRefresh 112–115, SettingsPage.handleChangePassword 121–150, SettingsPage.handleSaveModel 152–260, SettingsPage.handleDeleteModel 262–292, SettingsPage.openAddEntry 294–298, SettingsPage.cancelAddEntry 300–304, SettingsPage.handleCreateEntry 306–326, SettingsPage.handleSaveExchange 328–404, SettingsPage.handleDeleteExchange 406–416. SettingsPage active account/models/exchanges/Telegram control surface. refreshModelConfigs Promise.all config/catalog; tab/event effects no auth identity generation/cancel. Password PUT authenticated min8 only local; backend pending. handleSaveModel existing row builds full configuredModels map (stale unrelated values risk); add-provider duplicate guard createModelEntry branch drops chosen thinkingMode/reasoningEffort/name in favor generated name. Existing row blank key preservation delegated backend. handleDeleteModel confirms, fail-open advisory trader lookup but backend authoritative delete. Inline add state keyed provider, rendered beneath every same-provider row (duplicate forms). Exchange encrypted update/create, no account_name on update; all defaults sent. Save/delete success before refresh can later failure and stay modal. Configured exchange count described connected and unconditional TCP Bridge true is config badge not live proof. Modal conditional mount scrubs local state on close; outer editor requests can still resolve later. ResolvedKnobPanel and revision server-driven; no settings/live requests executed.

---

<a id="section-34"></a>

> Original: [reviews/29/report.md](reviews/29/report.md). Historical bounded independent cross-review of repair revision 99a06543, with baseline 63968be. It adds no primary source coverage and is not a review of all later repairs.

## Independent repair review — assignment 29

Baseline: `63968be62e44db2fb07a92883e02127b9064b0be`; reviewed repair: `99a06543`. Worktree `/tmp/vl-understanding-execution-20260913`, detached at repair revision and clean. Scope is all 60 changed files in that diff, their changed-function/context boundaries, and specifically recorded dependency excerpts. This is not a claim to fully reread all unchanged contents of those 60 files or a fabricated baseline assignment count. Common review instructions, canon/checklist and prior assignment context apply. No production edits, live queries, child agents or independently executed tests.

### Decision and remaining P1

[A source, B consequence] **The limit branch still loses its in-pass account commitment after an ambiguous transmission failure.** `trader/armed_executor.go:1217-1218` calculates the account contract once and initializes `placedThisPass`. At `1294-1309`, `BeginPlacement` may successfully register the first limit before `PlaceLimitEntry` returns a send error; the error branch continues without setting the pass commitment or retiring siblings. The adapter at `trader/ninjatrader/tcp_trader.go:482-495` confirms this ordering. A different scenario with no prior signal gets a free slot at `trader/cancel_confirm.go:315-323`; the stale pre-send account verdict remains free. B3 does not universally save this: its key at `tcp_trader.go:453` contains side, so an opposite-side sibling is a separate key. Both orders can therefore be attempted within one pass after an ambiguous first send. This is a concrete source-level P1 risk, not a reproduced duplicate trade. It predates this repair and overlaps the root report's explicitly open limit-path ambiguity. The repaired stop path (`armed_executor.go:1265-1270`, `1559-1628`) correctly returns registration commitment through a later send failure. Apply the same callback-result contract to limits and test the real loop with opposite-side siblings and a failure after registration. Root owns any reproducer/fix.

No further new P0/P1 is established by this review. This is bounded approval of the reviewed changes with the above remaining issue, not deployment approval or a proof that every repository boundary is safe.

### Control and execution boundaries

**Boot:** `main.go:250-265` now asserts boot integrity before `LoadTradersFromStore`; the adapter's `placeEntry`, `PlaceLimitEntry`, and `PlaceStopEntry` each check the same latch before account checks, pending-map mutation, registration or send. Market wrappers converge on `placeEntry`. Close/cancel/protection paths remain available. `main_boot_order_test.go` checks syntax ordering, while `boot_entry_refusal_test.go` exercises six adapter paths and a ready isolated loopback limit path. Neither proves a deployed NT8 binary or a real fill.

**Authorization lifetime:** `store/armed_orders.go:UpsertArm` checks same-version terminal retirement before allocating a broker-reaching successor. New version and boot-sweep exception retain distinct semantics. Successors receive current BootID and ArmedUnderVersion, a fresh placement sequence, and cleared signal/fill state. Existing mutable Version is not mistaken for original authorization provenance. Tests iterate all terminal states and explicitly increase versions in formerly contradictory append-only fixtures. This makes prior terminal rows durable evidence rather than repeat trading permission; it does not establish an atomic plan-proposal version transaction.

**Current-cycle admission:** `one_setup_wiring.go:oneSetupRetireDeclined` treats missing permission as refusal for unplaced inherited rows, propagates read/write/panic failures, and leaves broker-reaching rows to their lifecycle. `armed_executor.go:maybeManageArmedOrdersAt` stops on retirement failure and collects eligible row IDs only after this cycle's admission and successful persistence. `runArmedPlacementAt` filters only the armed placement branch against those IDs, while later settlement/reconciliation remains active. The old direct wrapper can omit the map; source search found its declaration and no non-test call. The production path supplies it. Tests exercise missing facts, injected retirement failure and current quality refusal through the actual cycle and wire fixture.

**Cancellation evidence:** parser rejects omitted/null orders while retaining explicit `[]`. In-memory snapshot payload and receipt time are read together. Both cancel safety entry points reject stale/future evidence, and failed sends return failure. Boot sweep persists cancel_pending before send; broker cancellation receipts no longer directly terminalize a pending cancel. Persisted settlement requires non-null JSON orders, valid receipt, freshness, and receipt at/after request. `ConfirmCancel` updates state and the boot-completion counter transactionally, conditional on pending state, with a positive evidence ID. Retry budgets reset by ProcessBootID before checking their cap. Store `ConfirmCancel` trusts its caller's evidence check; it does not independently validate snapshot contents. Receipt-time ordering is the implemented contract, not an acknowledgement that the broker generated a snapshot in response to that command.

Account check: `brokerBook` gets BoundAccount from the actual TCPTrader; `persistedBook` passes it into `store/nt8_order_snapshot.go:Latest`, whose SQL uses exact account and symbol equality. Empty account is not a global wildcard here. Symbol is deliberately empty because persistence records whole-account books. I found no cross-account fallback in this settlement reader. The live freshness check and persisted settlement are separate purposes; successful wire write is not broker confirmation.

**Wall clock:** `tickOnce` now invokes EOD/news cutoff handling before unchanged-bar/bar-close skips. Existing handlers cancel arms before reading/flattening positions and distinguish unconfirmed cancellation. The frozen-tape test proves an unplaced expired authorization retires at a 16:05 tick, not that a resting broker order was cancelled or filled safely. Grid still takes its separate earlier path. Potentially missing broker positions are reported rather than blindly flattened; this patch does not solve reconciliation latency.

**Protection:** `isProtectiveStopFor` requires actual stop type and closing action, rather than accepting an `-sl` suffix. Missing type/action on a potentially live named stop becomes UNKNOWN in `adjudicateProtection`; known contradictory shape contributes no coverage. Test cases cover wrong side, named limit, missing action/type and valid closing stop. This proves predicate behavior, not actual replacement receipt, C# bracket behavior, stop-price effectiveness or total protection under every malformed frame.

**History channels:** frame dispatch now holds histSubMu's read lock through nonblocking sends. Subscribe replacement and unsubscribe close channels under its write lock (`tcp_server.go:419-456`). Thus channel close cannot race a retrieved channel's send. The bounded channel still intentionally drops when full; this repair concerns lifecycle, not lossless history transport. Root's race/loopback regression is the reproduction evidence.

### Ownership, planning and configuration

The protected-group middleware examines all query trader IDs, trader path ID, and body trader ID on POST/PUT/PATCH/DELETE; conflicting selectors cannot bypass ownership by putting an owned ID first. Trader DELETE also checks ownership at handler entry, and store deletion establishes ownership inside a transaction before deleting equity children. Child-delete failure rolls back both. Router tests use real route registration/authentication and verify exact ownership refusal, plus positive owned reads. This is evidence for these selectors, not a guarantee for every differently named identifier or every global feed.

Order-fill history now reads the central store independently of a running trader and checks both parent order and fill TraderID. The test includes a foreign order and an inconsistent foreign fill attached to an owned order. The baseline test failure was stopped-runtime unavailability, not proof of historical data disclosure; the source deficiency and successful repaired contract are distinct claims.

Normal and streaming HTTP chat override request-selected numeric identity with the authenticated store-user-derived key before conversation processing. The /clear tests demonstrate separate owners' memory. This does not resolve the separately identified shared AI-client mutation problem or prove collision resistance of the existing identity-key mapper.

Ask context uses trader-scoped historical fallback and a zero creation timestamp when no stored plan exists; available bars can render no-plan context without nil dereference. Apply/realign call the shared wrap-aware session helper, refuse session gaps and use the chain's trade date across midnight. The new helper test covers gap and overnight chain, not an actual simultaneous apply/realign request race. Existing optimistic proposal/version atomicity remains outside this fix.

Strategy token overflow rejection now precedes serialization/store update/reload. The regression compares stored config and name bytes after the HTTP rejection. Warnings and clamps still belong to the existing merged-config path; this does not imply every unrelated handler rejects before every side effect.

### Prompt, guide and policy

Planner and stop-floor renderer now distinguish structural reject composition from legacy non-reject ATR floors. Authored reject prices no longer generate misleading legacy feasibility warnings, and omission is not advertised as an AI-path bypass. Structural admission still requires frozen provenance, geometry, costs, configured RR and existing daily loss guards. No mandatory per-trade dollar cap is reintroduced; nothing here proves profitability. Prompt tests assert builder output and warning behavior. The map guard's AST exception is confined to ArmFeasibilityWarnings; it does not exempt whole map assembly.

Checklist additions name the repaired classes without claiming numbered merged waves. Guide text describes pending versus confirmed boot cancellation, ownership, missing evidence, structural composition and current admission. Its broad statement that any registered attempt retains commitment is currently stronger than the remaining limit branch above. SYSTEM-MAP changed coordinates were reviewed as documentation, not independent runtime proof; historical incident numbers are not rediscovered incidents. GUIDE_BUILT_REV shipment remains the integrating/deploying owner's responsibility.

### Evidence and validation limits

Read root repair README in full (225 lines), reviewed all diff hunks and new tests, and read 21 focused after logs. Those logs report successful relevant packages; boot-sweep log also contains a store package with `[no tests to run]`, which is not store test coverage. Root's full-suite03 log was searched for FAIL/panic/race (none) and its successful tail inspected; this worker did not launch or attest process exit status. Earlier full-suite failures and fixture/map-pin fixes remain accurately described in root README. Root owns the full-suite execution at repair revision.

No additional tests were run: the new concern was already explicitly open and source-confirmed, with root notified for its test plan. No C# source exists in this diff; that separate lane and compiled AddOn remain unverified here. Historical CGC/Understand Anything graph is not evidence for September repairs. `graph.json` contains only manually evidenced current edges; `functions.json` is the named declaration inventory intersecting reviewed diff contexts, not an inventory of every unchanged function in large files. `reads.json` distinguishes diff excerpts, complete new files, evidence, and dependencies. Source worktree remained clean.

---

<a id="section-35"></a>

> Original: [reviews/30/report.md](reviews/30/report.md). Historical bounded independent cross-review of repair revision 99a06543, with baseline 63968be. It adds no primary source coverage and is not a review of all later repairs.

## Assignment 30 — independent architecture and evidence review

Scope: baseline `63968be62e44db2fb07a92883e02127b9064b0be`; reviewed repair source `99a065430cb28ced23c4992fe04ff9b13787dc3d` in isolated detached `/tmp/vl-understanding-surfaces-20260913`. This is a bounded cross-boundary synthesis, not another whole-source census. Assignment 22/25/28 full baseline reviews provide explicitly historical supporting context. Current ledger contains 20 documents/source/test files (14 full, 6 excerpts); 32 named functions begin inside read ranges. No assigned checklist item remains unread; limitations below remain unresolved evidence, not fabricated passes.

[A/static] means exact source or artifact read. [B] means inferred consequence. This worker executed no tests, app, live API, database query, backup restore, market/data script or NT8 compile. No runtime incident is claimed. Source/map diff inspection supplements the range ledger but does not count as full-source reading.

### End-to-end contract

1. **Settings and cached policy.** Baseline review28 traces SettingsPage → model/exchange APIs and DayPlanEditor → strategy configuration. Configuration is not live merely because a DB row changed; trader reload/cached strategy remains a boundary. Owner daily-loss controls are the policy, not a restored mandatory per-trade cap. Current `store/structural_geometry.go:32` resolves MNQ buffer4.5 from explicitly in-sample C5 calibration, cost2 by default, optional owner overrides and Studio RR. Unknown/invalid buffer and cost remain explicit. Calibration is not an out-of-sample profitable strategy claim.
2. **Prompt versus geometry.** Current `kernel/planner_prompt.go:798` explicitly describes reject-fade frozen-zone provenance, risk-side buffered stop, first complete profit-side target, composed RR and positive net room; it explicitly exempts structural fades from universal ATR widening and allows no-trade. `ComposeLevelFadeGeometry` resolves exact frozen identity and refuses ambiguous/missing provenance. Target search does not skip nearby zones to force desired RR. Tick rounding is directional for risk; costs affect net gain and loss reporting. Composition records have quantity0 until later admission. The structural record is not permission to place.
3. **Admission and lifecycle.** `armed_executor.go:320–405,475–545,810–894` runs session risk, OneSetup retirement, geometry persistence/refusal, durable row creation/update and a per-cycle eligible-row set. Errors retiring or persisting refusal stop placement. `entry_gate.go:147–330` runs daily-force-flat before strict-plan routing, bias/cited-scenario consistency, invalidation, shadow, execution-price RR, legacy ATR only when structural stop is unvalidated, then one-open-position. Missing invalidation/absent daily resolver intentionally pass: this gate is not a universal fail-closed fact validator. Quantity and account authorization remain downstream boundaries.
4. **Execution commitment.** Both NT8 adapters check boot-integrity refusal before bound-SIM permission. Stop additionally proves remote build capability. `tcp_trader.go:441–590` registers signal ID before sending frame. This is necessary for asynchronous fills and ambiguous transport outcomes. `armed_executor.go:1260–1330` now treats stop registration as commitment even if send returns error; limit still sets pass commitment only after success (finding below). Socket success is not fill proof.
5. **NT8 account and protection.** C# close routing at1120–1192 cancels bracket legs and looks up the symbol-to-position-account map, falling back to active account if missing, then enforces SIM. This is SIM safety, not exact bound-account identity. Broker cancellation exception logs do not prevent bracket bookkeeping removal. Those are existing residual boundaries, not fixed by Go boot/account hardening. The current protection regression requires correct order type/action even with `-sl` name; unknown fields remain unknown. No Windows binary/loaded AddOn was checked.
6. **Fills and persisted lineage.** `materializeArmedEntry:2018` requires positive fill and signal, checks uppercase and legacy lowercase duplicate position, writes account/plan/version/scenario with source armed_entry and calls excursion tracking. Repair report separately discloses that its order/fill ownership before-test failed on a stopped-trader path, not a demonstrated foreign disclosure. Preserve this distinction. Fill creation does not prove correct close accounting or fill delivery under every race.
7. **P&L and UI.** Baseline review28 fully traces corrected-column aggregation and PositionHistory. At reviewed repair revision the frontend still has `effectivePnl:67` fallback, while day-total helper requires correction presence. Corrected-only server aggregate plus an uncorrected raw row fallback is not one consistent truth surface. Structural geometry and unknown counts should be rendered as computed/unknown distinctly, never inferred from an empty array.

### Findings and discrepancies

**30-01 — current map's record-only permission promise contradicts a production consumer [A/static].** SYSTEM-MAP's appended Fade Permission W2 section says never gates. `one_setup_wiring.go:150–152` supplies `FadePermissionAt` to `OneSetupAllowsAt`, whose verdict is retired/enforced in current executor. Historical record-only stage prose must be dated or explicitly superseded. The map's own same-commit maintenance law is stronger than the limited current line-coordinate guard; passing that guard does not validate every paragraph.

**30-02 — interim reports differ from reviewed source and completion state [A/artifact].** Baseline CTO-TRADING-LOGIC explicitly pins baseline and an earlier repair revision; its pending universal-ATR prompt mismatch is fixed in reviewed99a06543. README and CHECKPOINT describe different interim counts (26/30 versus20/30 at time read), not final repository coverage. Do not silently combine them with later assignments27–30. The README UA SHA string omits bytes; actual graph hash read earlier is `23aa686864d6e1af4e6b43ae52856175356d07e8baf066fe36d0e90418a2c43f`. Parent notified. Root may update artifacts after this read; hashes preserve this snapshot.

**30-03 — limit send ambiguity remains open [A/static; B consequence].** `armed_executor.go:1297–1303` passes `BeginPlacement` into `PlaceLimitEntry` but immediately continues on error without setting `placedThisPass`; adapter can return an error after registration and socket write attempt. Other candidates use the pass's existing row snapshot. A second attempt can therefore be considered without the same commitment treatment now applied to stops. This is independently source-confirmed, already explicitly acknowledged by repair README. No duplicate order reproduced here. Fix requires production-loop regression, not a helper-only assertion.

**30-04 — corrected-only reporting is not end-to-end [A/static].** PositionHistory raw fallback for unresolved `pnl_corrected` and bounded/paginated history totals remain unchanged by reviewed Go repairs. Baseline28 documents exact frontend and store boundaries; current searches confirm the fallback still present. This affects displayed totals/classification, not proof that stored corrected results are wrong. Track unresolved count and exclude unresolved totals rather than manufacturing0/raw truth.

**30-05 — backup comment overstates cleanup [A/static].** `deploy/vl-db-backup.sh` claims any failure keeps nothing partial; Python/quick_check/gzip failure under `set -e` exits with no cleanup trap, leaving `.partial` or `.partial.gz`. Final rename is useful protection from presenting partial daily copies as completed. Weekly `cp` writes directly to the final-looking filename; interruption can leave an incomplete weekly object. These are static failure-path risks, not evidence a backup is corrupt. `prune` also word-splits paths and assumes valid retention inputs. Backup receipt shows recorded quick/integrity check metadata, not this worker verifying source backup bytes or restore behavior.

**30-06 — remaining map claims require scoped wording [A/static].** PlaceStopEntry no longer checks build first: boot latch precedes it. Older cancel prose omits new causal post-request snapshot requirement. The map lists old knobs/stage captions and historical account/daily-control observations without consistently distinguishing current runtime from dated evidence. Only narrow MAPCHECK lines are guarded; the complete606-line prose is not machine-validated. Historical retired paths can be documented, but not presented as the active implementation.

**30-07 — browser overlay writes lack identity/version binding [A/static].** `web/src/lib/api/plan.ts:667` posts trader/symbol/patch/origin with no expected plan or overlay version. PlanToday exposes version but not explicit plan_id/overlay_version in current TypeScript type. Production callers are EditSheet save and delete. An index patch based on an older draft needs opening-snapshot identity, not newly polled identity. Parent owns coordinated API repair; frontend repair will follow after this report.

**30-08 — C# residual safety limitations remain outside Go repairs [A/static].** `HandleClosePosition` uses symbol map then active SIM fallback, so a Go bound-account invariant does not establish an account-qualified C# close route. Bracket cancellation cleanup removes bookkeeping after caught exception. Existing baseline reports correctly keep these open. No AddOn restart, runtime frame or live order probe occurred.

### Tests, graph and claim honesty

[A/artifact] Repair README distinguishes focused red/green synthetic regressions from source-established helper behavior. It explicitly reports prior full-suite failures and pending final combined head checks. This review does not convert that into a final green suite. `main_boot_order_test.go` checks AST lexical call order in production main; `structural_prompt_contract_test.go` tests generated prompt wording and authored rejection-warning exemption; `protection_shape_regression_test.go` tests five synthetic shape outcomes. These establish narrower properties than live execution, broker rejection handling, or AI compliance.

[A/artifact] Postboot receipt lists geometry0, composition0, sweep0 and pending_market_open. That is an honest lack of first composition/refusal evidence. Backup JSON records size/hash/integrity metadata; reading the JSON is not verifying the backup file or a restoration. Dependency security alerts5 remain uninvestigated; no claim about vulnerability applicability or remediation.

Historical UA graph is July10@7a8adce0 (3121 nodes9588edges). CGC is historical781files5095functions; root reports new composer absent. Its Normalize caller export was only sampled and was truncated, so no full-export coverage claim. Current local syntax AST inventories and TS census are useful consistency checks, not type-resolved call graphs or full semantic read proof. Source resolves conflicts; no reindex/delete performed. Current `graph.json` has eight explicitly scoped source edges, not an inferred global execution graph.

The full baseline assignment28 reviewed16files9472lines and assignment25 reviewed54files12309lines; none of those are added to assignment30 full-source totals. Assignment30's ledger includes documents and tests, so even its14 full files must not be described as14 production-source files. No end-to-end profitability, deployment safety, restored backup, remote C# compatibility, alert resolution or final merged-suite pass is established here.
