# RESEARCH INDEX — everything commissioned 2026-08-17 → 2026-09-01

**STANDING RULE (owner, 2026-09-03): a docs branch merges to dev the SAME DAY it is pushed — a report not on dev does not exist.** Eight branches were merged under that rule on 2026-09-03 (dev `338867f9`), all verified docs-only (40 files, 40 under `docs/`) and each report curl'd 200 at raw/dev.

Archive merged to dev 2026-09-01 by `docs/research-archive-merge` (docs-only; no code, no content edits). Each stranded artifact keeps its original filename; collisions are suffixed with the branch slug. Source branch + original commit sha recorded per row. Reports are historical artifacts — a wrong report stays wrong; corrections happen in later waves, not here.

Legend — Action: **shipped** (wave name) · **queued** · **owner-ruled** · **NONE**. Conflicts: →DUPLICATE (agree) / →CONTRADICTION (owner-ruled).

| Date | Title | Path on dev | Original branch · sha | Verdict (one line) | Action | Conflicts |
|---|---|---|---|---|---|---|
| 08-17 | reverse-trace audit | docs/superpowers/reports/2026-08-18-reverse-trace-audit.md | dev `283e75b5` | 22 values traced, 0 invented | NONE | — |
| 08-17 | end-to-end audit | docs/superpowers/reports/2026-08-18-end-to-end-audit.md | dev `f673ce5e` | one decision traced bar→order, 17 hops, 1 mismatch | NONE | — |
| 08-17 | full verification | docs/superpowers/reports/2026-08-18-full-verification.md | dev `a4025b97` | 34 pass, 13 findings, 2 high | partial fixes | — |
| 08-17 | total sweep | docs/superpowers/reports/2026-08-18-total-sweep.md | dev `7d98c619` | 8 error types censused, ASIA enabled | cleanup-train | — |
| 08-17 | consumed-without-touch | docs/superpowers/reports/2026-08-19-consumed-without-touch.md | dev `448142f1` | 0 of 7 burns justified | **shipped** touch-gate fix `91333cfb` | — |
| 08-18 | why-no-trades | docs/superpowers/reports/2026-08-18-why-no-trades.md | dev `00ca0f32` | decisive test + session autopsy | NONE | — |
| 08-18 | aug-14 bisect | docs/superpowers/reports/2026-08-19-aug14-bisect.md | dev `5d58ca3f` | 79 calls / 0 proposals; provider-side drift | NONE | — |
| 08-18 | partner-vs-us A/B | docs/superpowers/reports/2026-08-19-partner-vs-us.md | dev `7ec664c9` | pipeline exonerated | NONE | — |
| 08-18 | total root-cause | docs/superpowers/reports/2026-08-19-total-root-cause.md | dev `de5a54ba` | truncation zeroed decisions | **shipped** `f6478923` | — |
| 08-18 | total e2e investigation | docs/superpowers/reports/2026-08-19-total-e2e-investigation.md | dev `63062e5e` | clock-drift guard converted entries to wait | **shipped** clock fixes | — |
| 08-18 | timegate audit | docs/superpowers/reports/2026-08-18-timegate-audit-ai-timeout.md | dev `8d8cf492` | 57 gates, 8 BUG rows | **shipped** same wave | — |
| 08-19 | 5-day zero-trade postmortem | docs/superpowers/reports/2026-08-19-zerotrade-forensics.md | `docs/forensics-zerotrade-2026-08-19` `765ac11a` | postmortem of zero-trade stretch | partial | overlaps why-no-trades |
| 08-19 | strategy-controls census | docs/superpowers/reports/2026-08-19-strategy-controls-census.md | `docs/strategy-controls-census` `e12e3846` | 246 controls; 12-row DEAD/PARTIAL register | partial | — |
| 08-19 | controls runtime verify | docs/superpowers/reports/2026-08-19-controls-runtime-verify.md | `docs/controls-runtime-verify` `1522cfa2` | min-conf 60 not 65; gate-65 vs prompt-60 defect | partial; defect closure UNKNOWN | →CONTRADICTION spec-65 (owner: 60) |
| 08-19 | decision anatomy | docs/superpowers/reports/2026-08-19-decision-anatomy.md | `docs/decision-anatomy` `2d4a706e` | canonical map, 87-step gate order | NONE (reference) | — |
| 08-19 | breakeven audit | docs/superpowers/reports/2026-08-19-breakeven-audit.md | `hotfix/breakeven-dead` `7b687b78` | NOT A BUG — fired end-to-end mid-audit | NONE | — |
| 08-19 | ledger-close FINAL | docs/superpowers/reports/2026-08-19-ledger-close-FINAL.md | `fix/ledger-close-sep-risk` `cc34308e` | 11-section close report | **shipped** final-bundle | — |
| 08-20 | brand census | docs/superpowers/reports/2026-08-20-brand-census-docs-brand-census.md | `docs/brand-census` `23582b2a` | docs branding census | NONE | collision-suffixed (dev already had a brand-census.md) |
| 08-21 | shift-day loss forensics | docs/superpowers/reports/2026-08-21-shift-day-loss-forensics.md | `docs/research-import-shift-forensics` `d070c932` | shift-day loss root-cause | partial | — |
| 08-22 | master audit v2 | docs/superpowers/reports/2026-08-22-master-audit-v2.md | dev (merged) | 76 rows, 22 findings | fixed in waves | — |
| 08-22 | research conformance | docs/superpowers/reports/2026-08-22-research-conformance.md | dev (merged) | ATR WILDER mismatch | **shipped** ATR fix | — |
| 08-22 | research full readthrough | docs/superpowers/reports/2026-08-22-research-full-readthrough.md | dev (merged) | plan-card research readthrough | NONE | — |
| 08-24 | level-grading full audit | docs/superpowers/reports/2026-08-24-level-grading-full-audit.md | dev | zone-grading v3 basis | **shipped** v3 grading | — |
| 08-25 | deep-status accounting | docs/superpowers/reports/2026-08-25-deep-status-full-accounting.md | dev | flip/death chain accounting | **shipped** plan-lifecycle wave | — |
| 08-25 | 1h-timeframe research | docs/superpowers/reports/2026-08-25-1h-timeframe-research-wave.md | dev | 1h A-capable, TFmult ≈2.3× | **shipped** 1h-wave R2/R4 | — |
| 08-26 | fvg-entry model | docs/superpowers/reports/2026-08-26-fvg-entry-model.md | dev | no tradeable edge after honest costs | **shipped** 0C shadow demotion | →CONTRADICTION FVG-demand prompts (0C ruling) |
| 08-26 | volume-wave phase-0 stop | docs/superpowers/reports/2026-08-26-volume-wave-phase0-stop.md | dev | Pack B NO-GO: no stored bars | queued → bar persistence shipped 08-28 | — |
| 08-26 | settings census | docs/superpowers/reports/2026-08-26-settings-census.md | `docs/settings-week-audit` `dda9777c` | settings census | NONE | — |
| 08-26 | week-in-review | docs/superpowers/reports/2026-08-26-week-in-review.md | `docs/settings-week-audit` `dda9777c` | week review | NONE | — |
| 08-26 | london drought v1 | docs/superpowers/reports/2026-08-26-london-drought.md | `docs/london-drought` `607b8861` | flip-vs-chop; latency-vs-drift — do NOT act | NONE | →DUPLICATE of v2 (agree) |
| 08-27 | london drought v2 | docs/superpowers/reports/2026-08-27-london-drought.md | `docs/london-drought-2026-08-27` `f962d648` | 212 cycles/191 waits; no_trade can't self-revive | NONE | →DUPLICATE of v1 (agree) |
| 08-27 | mega-research MNQ | docs/superpowers/reports/2026-08-27-mega-research-mnq.md | `docs/mega-research-mnq` `2cea2029` | S1 proximity ±530pt; calibration queue | **shipped** proximity 0.3; queue Sep-9 | — |
| 08-27 | master recheck | docs/superpowers/reports/2026-08-27-master-recheck.md | `docs/master-recheck` `4b65eeeb` | cutover clean, scoreboard half-deployed | partial | — |
| 08-27 | level-system verify | docs/superpowers/reports/2026-08-27-level-system-verify.md | `docs/level-system-verify` `d6aa9669` | weights off spec; grades not predictive | NONE (documented) | — |
| 08-27 | deep-verify-22 | docs/superpowers/reports/2026-08-27-deep-verify-22.md | `docs/deep-verify-22` `95049c0c` | EOD armed race; ingest cap; partner drift | partial (S-list closer) | — |
| 08-27 | refusal autopsy | docs/superpowers/reports/2026-08-27-refusal-autopsy.md | `docs/refusal-autopsy` `589f7865` | per-gate would-have replay | **queued Sep 3** | — |
| 08-28 | grand audit | docs/superpowers/reports/2026-08-28-grand-audit.md | `docs/grand-audit` `104f0d3d` | Part A verdict A zero-broken | **shipped** GAR wave | — |
| 08-28 | grand audit B-E verdict | docs/superpowers/reports/2026-08-28-grand-audit-bcde-verdict.md | `docs/grand-audit` `104f0d3d` | move_stop dead cell; decline leak +$1974.5 | **shipped** GAR F1-F6 | — |
| 08-28 | missed-200pt | docs/superpowers/reports/2026-08-28-missed-200pt.md | `docs/missed-200pt` `673e9240` | the 200pt missed move | partial (wake fixes) | — |
| 08-28 | weekend audit p1 | docs/superpowers/reports/2026-08-29-weekend-audit-p1.md | `docs/weekend-audit-p1` `c18bd3a2` | S1 8th-condition BROKEN; S2 closes_dropped | **shipped** pre-reopen hotfix | — |
| 08-28 | weekend audit p2 | docs/superpowers/reports/2026-08-29-weekend-audit-p2.md | `docs/weekend-audit-p2` `b964dc8e` | week −$176.5 NY-only; gates net −$511.8 SAVING | **owner-ruled** (Sep-9 docket) | — |
| 08-28 | e2e verification | docs/superpowers/reports/2026-08-28-e2e-verification.md | `docs/e2e-verify` `03957e58` | S-1 stored bars ≠ live truth; S-2 VWAP 1.74pt | **shipped** bar-truth wave | VWAP dup w/ final-verify |
| 08-28 | final verify v2 | docs/superpowers/reports/2026-08-28-final-verify.md | `docs/final-verify` `91995ad4` | VWAP residual died (data bug) | **shipped** forensics-hygiene | resolves e2e S-2 |
| 08-28 | london forensics | docs/superpowers/reports/2026-08-28-london-forensics.md | `docs/london-forensics` `a5595503` | #567 armed-fill; 32768 truncation | **shipped** london-fix wave | — |
| 08-29 | zone-math verification | docs/superpowers/reports/2026-08-29-zone-math-total-verification.md | `docs/zone-math-verification` `6919aa8b` | 6 zone classes verified | NONE | — |
| 08-29 | total audit (15 agents) | docs/superpowers/reports/2026-08-29-total-audit-15.md | `docs/total-audit-15` `35c3aad9` | S-list + conditional Sunday GO | consumed by Sunday cutover | overlaps p1/p2 |
| 08-29 | dryrun replay scope | docs/superpowers/reports/2026-08-29-dryrun-replay-scope.md | `docs/dryrun-replay` `4b0ba249` | libfaketime FAILED; live-fire = integration test | owner accepted | — |
| 08-29 | pre-livefire verify v1 | docs/superpowers/reports/2026-08-29-pre-livefire-verify.md | `docs/pre-livefire-verify` `85095811` | 17/18 PASS; S1 watchdog unwired BROKEN | **shipped** s1-watchdog-wire | →DUPLICATE of 0830 |
| 08-30 | pre-livefire verify 0830 | docs/superpowers/reports/2026-08-30-pre-livefire-verify-docs-pre-livefire-verify-0830.md | `docs/pre-livefire-verify-0830` `a290920e` | P5 verdict + owner-pending list | same | →DUPLICATE of v1 (superset); collision-suffixed |
| 08-30 | dress rehearsal 0830 | docs/superpowers/reports/2026-08-30-pre-livefire-verify.md | `docs/dress-rehearsal-0830` `b54a9bfc` | session plan review + weekly r1-r6 + isolation | NONE | shares path with 0830 (kept both) |
| 08-30 | knob census | docs/superpowers/reports/2026-08-30-knob-census.md | `docs/knob-census` `39a0481e` | every money-path number labeled; top-10 unvalidated | **NONE — orphaned finding** | — |
| 08-30 | cheap-five verdicts | docs/superpowers/reports/2026-08-30-cheap-five-knob-verdicts.md | `docs/cheap-five` `9298f9d4` | five knob validations | **NONE — orphaned finding** | — |
| 08-30 | confirm-cost forensics | docs/superpowers/reports/2026-08-30-confirm-cost-forensics.md | `docs/confirm-cost-0830` `8f09aa84` | close-confirms net-cost ≈ −$681 | **NONE — orphaned finding** | — |
| 08-30 | massive-move audit | docs/superpowers/reports/2026-08-30-massive-move-audit.md | `docs/massive-move-audit` `151ef42b` (origin) | −204pt sell-off; 5 bug-class candidates | **shipped** via weekly/armed waves | — |
| 08-31 | planner latency autopsy | docs/superpowers/reports/2026-08-31-planner-latency-autopsy.md | `docs/planner-latency-autopsy` `168e5282` | ~99.5% provider generation | **shipped** planner-speed wave; T1/T2/T4 gaps open | — |
| 09-01 | EOD verification | docs/superpowers/reports/2026-08-31-eod-verification.md | dev `b8f68db1` | six waves verified live | verification | — |
| 09-02 | level-kind replay | docs/superpowers/reports/2026-09-02-level-kind-replay.md | docs/level-kind-replay-0902 `3961f873` | merged 2026-09-03 by `fix/hygiene-0903` docs sweep — see the report | (unclassified: verdict/action not yet triaged) | — |
| 09-02 | bias calibration | docs/superpowers/reports/2026-09-02-bias-calibration.md | docs/bias-calibration-0902 `2deab3c8` | merged 2026-09-03 by `fix/hygiene-0903` docs sweep — see the report | (unclassified: verdict/action not yet triaged) | — |
| 09-02 | live bias replay | docs/superpowers/reports/2026-09-02-live-bias-replay.md | docs/live-bias-replay-0902 `53498adb` | merged 2026-09-03 by `fix/hygiene-0903` docs sweep — see the report | (unclassified: verdict/action not yet triaged) | — |
| 09-02 | belief census | docs/superpowers/reports/2026-09-02-belief-census.md | docs/belief-census-0902 `ee64a494` | merged 2026-09-03 by `fix/hygiene-0903` docs sweep — see the report | (unclassified: verdict/action not yet triaged) | — |
| 09-02 | bar-source audit | docs/superpowers/reports/2026-09-02-bar-source-audit.md | docs/bar-source-audit-0902 `593dcf9e` | merged 2026-09-03 by `fix/hygiene-0903` docs sweep — see the report | (unclassified: verdict/action not yet triaged) | — |
| 09-02 | today's losses forensic | docs/superpowers/reports/2026-09-02-today-losses-forensic.md | docs/today-losses-0902 `53288973` | merged 2026-09-03 by `fix/hygiene-0903` docs sweep — see the report | (unclassified: verdict/action not yet triaged) | — |
| 09-03 | no-trade forensic | docs/superpowers/reports/2026-09-03-no-trade-forensic.md | docs/no-trade-forensic-0903 `9c42b342` | merged 2026-09-03 by `fix/hygiene-0903` docs sweep — see the report | (unclassified: verdict/action not yet triaged) | — |
| 09-03 | MC drawdown rig | docs/superpowers/reports/2026-09-03-mc-drawdown.md | docs/mc-drawdown-0903 `77e1cdfc` | expectancy indistinguishable from zero (CI −$31…+$18, n=64); maxDD@50 p95 $1,677; the 3-trade cap forfeits $24.54/day, the $450 limit is inert | **INPUT to size/limit rulings** — no verdict by stop-line | — |

Static literature (on dev): `docs/research/PLAN-CARD-DESIGN-SYSTEM.md`, `VL-DAYPLAN-FULL-SPEC-(2).md`, `AI-Trader-Analysis-Report.md`, `Strategy-Studio-Complete-Plan.md`, `VL_Trading_System_*.md`, `docs/research/plan-card/`.

Orphaned findings (carried from the inventory §B): knob-census top-10 unvalidated · cheap-five · confirm-cost −$681 · mega-research calibration queue · weekend-p2 min-conf/proximity buckets (Sep-9) · level-system weight divergences · strategy-controls DEAD register · controls-runtime gate-65 defect (closure UNKNOWN) · london-drought open questions · latency T1/T2/T4 instrumentation + completion diet.
Duplicates (§D): london-drought ×2 (agree) · pre-livefire ×2 (superset) · VWAP residual ×2 (resolved).
Contradictions (§E): fvg_entry study vs demand prompts (0C shadow) · min-conf 65 vs 60 (60) · proximity (0.3) · side-quota (removed) · VWAP residual (data bug) · volume weights (documented, live code wins).
