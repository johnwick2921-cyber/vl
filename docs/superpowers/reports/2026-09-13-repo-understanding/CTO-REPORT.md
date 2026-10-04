> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# CTO repository and trading-process report

Generated from preserved Markdown sources by `tools/build-reports.py`. This is an editable assembled report; make durable corrections in the linked originals and rebuild. Baseline source scope is **1,049 files / 250,582 lines at 63968be62e44db2fb07a92883e02127b9064b0be**, across 28 primary reviews plus two bounded independent reviews. Source review is not runtime verification, deployment approval, or profitability evidence.

**Final verification is recorded only in [CHECKPOINT.md](CHECKPOINT.md#final-verification).** Packaging is not an additional audit or test pass. Current disposition includes committed ordered-execution and entry-receipt lifetime repairs; original numbered appendices remain historical.

Relative links have been rebased to the original artifacts; fragment links point to the original document to avoid duplicate-heading ambiguity. Code fences and original source files are preserved. Machine-readable baseline consistency evidence remains in [coverage-validation.json](coverage-validation.json) and [publication-validation.json](publication-validation.json); the checkpoint describes its limits.

## Contents

- [PACKAGING-UPDATES.md](#section-1)
- [CTO-TRADING-LOGIC.md](#section-2)
- [REPAIR-STATUS.md](#section-3)
- [CORE-TRACE.md](#section-4)
- [CHECKPOINT.md](#section-5)

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
