# Arm-state correction and cutover receipt — 2026-09-10

## HOLD — correction merged; no cutover

[A] PR #97 merged as **8140f8f208287f115697584b77797bf6ea751865**. The owner-requested sweep correction is **41023d0c**: `SweepableArmStateSQL` derives non-terminal MINUS cancel_pending, leaving pending cancellations to the snapshot settlement pass. Leg 4 and the arm-state source guard remain intact. The production pin reproduced the regression before the correction (`swept=1 sends=1 state=cancelled snapshot=0`) and passes afterward (`swept=0`, no-book pending=1, resting-book pending=1, empty book cancelled with the exact persisted snapshot id). This was my branch's regression, not a dev defect.

## Clean-clone failures were pre-existing, not introduced by PR #97

[A] The full suite at merged HEAD **8140f8f2**, in `/tmp/arm-state-cutover-20260910/nofx`, FAILED before the binary build. Kernel reported four bare clock formats. Frontend reported **413 PASS / 1 FAIL**: the brand-scope hash still pinned the lock script from before its independently authorized keeper change. No build, dist, RELEASE swap or kill followed those failures.

[A] Both failures reproduce at the PR's first parent **2f80d743** in a separate clone named `nofx`; PR #97 has no diff in those affected source files. [Timezone parent failure](2026-09-10-arm-state-cutover-data/tz-parent-red.txt), [brand-scope parent failure](2026-09-10-arm-state-cutover-data/brand-parent-red.txt).

The earlier arm-state worktree suite exited zero but its timezone guard was VACUOUS: it climbed until a directory name ended in `nofx`, reached `/` from `/tmp/nofx-arm-state`, then swallowed missing-directory errors. The clean-clone requirement exposed the defect. The guard now resolves the package's repository parent, requires go.mod and propagates walk errors. It scans the same source in either clone naming scheme.

## Minimal follow-up

The four clock formats route through existing CT helpers. ClockHHMMCT delegates to CloseHHMMCT; seconds displays use ClockCTSeconds with the duplicate literal suffix removed. [Six before/after production-renderer samples](2026-09-10-arm-state-cutover-data/clock-parity.json) are byte-identical across winter and summer. No trading predicate, clock source or output text changes.

The sole updated brand baseline is `deploy/nofx-lock.sh` at **97a6525cb6d10d6c8898b2d277c0fe7581872c24**, already merged by the lock-keeper lane and authorized before this cutover. Its SHA256 is `670a405be9a7c80b866bb20855fbf774227bc4f374152bfc74963265e257387b`; the prior baseline was `bcd82c52dec5814f3862dc1eb47ece85d7f8766bb34ee2568a61ebd2cd0d4a6e`. This records the authorized keeper revision; it does not add a bypass or alter the lock script. The hash verifier and other protected hashes remain. Real source mutations restoring a bare time layout and removing the keeper start are both rejected; [mutation receipts](2026-09-10-arm-state-cutover-data/followup-mutations.json).

[Per-file source freshness](2026-09-10-arm-state-cutover-data/source-freshness.json) names the exact sources used. The keeper's tracked canon was read; acquire starts its heartbeat, and no manual heartbeat writer is used.

## Live prerequisites observed so far

[A] At 11:38:57 CT, the normal owner-authenticated browser read `/api/cutover-gate` HTTP 200 and all five legs PASS: DB open=0, API positions=0, NT8 count=0, broker working=0 / ledger=0 (snapshot age 16s), no planner read. This is an observation, not a reusable cutover permit; the actual swap requires another fresh read after validation/build/dist.

[A] Main is on dev and the deploy lock is free, but `.CLAUDE.md.swp` belongs to a LIVE nano editor, pid **2203882**. The owner was asked to close it cleanly. No file was removed and no lock was acquired over a dirty main tree. The running service remains pid **1953256**, clean binary rev **8941ec68612cc019edc3002b999272ab2ed20516**. This is the rollback baseline; no boot of arm-state has occurred yet.

### Dev advanced during cutover preparation

Before merging the validation follow-up, dev advanced to `757eb578`, bringing `ace51598` from `fix/lock-defects-release-meta-halfbuilt`. Its report records owner-pinned scope and 101 passing lock checks. I integrated that branch and updated the protected lock baseline to its exact SHA256 `46fcbf76478c43943fb7607bd2929fe3629371ae5b8571ec9da8e1fa6e96c6ab`; this supersedes the keeper-only hash above. I authored neither lock implementation; the deployed tree must carry and be validated with both. Their report is [Three lock defects](2026-09-10-lock-three-defects.md).

## Fresh lock suite blocks cutover

[A] My own isolated `bash deploy/nofx-lock-test.sh` at the merged lock-defects code returned **99 PASS / 2 FAIL**, unlike the other lane’s recorded 101/0. Both failures are in the corrupt keeper.pid fixture: release returned rc=1 instead of expected 0, and printed that the lock directory still existed after rm instead of claiming release. The fixture intentionally replaces the stop handle while the keeper is alive; I did not modify the script, its expected result, or its fixture. [Full fresh lock-suite output](2026-09-10-arm-state-cutover-data/lock-tests.txt). This is an unresolved cutover blocker, alongside the active main-tree nano editor. No RELEASE write, dist publication, binary swap or kill has occurred.

## Final merged-head validation: HOLD, not shipped

[A] PR #98 merged the display/validation follow-up as **a8b66cd0f463d2c2afb16a29e245de2dfa947433**. From the clean clone named `nofx`, frontend **58 files / 414 tests PASS** and TypeScript PASS. The timezone and brand-baseline failures are resolved. The full Go suite **FAILS seven trader fixtures** when its execution crosses noon CT: six shadow-demotion tests and TestSplitArmWritesTwoLedgerRows. The actual lunch gate reports `no_trade_band: lunch no-trade window (12:00–13:30 CT)` and prevents the setup expected by the fixtures. [Failure excerpts](2026-09-10-arm-state-cutover-data/final-go-failures.txt), [structured receipt](2026-09-10-arm-state-cutover-data/final-validation.json).

[A] The same seven failures reproduce for the same lunch-gate reason at pre-PR parent **2f80d743** ([parent reproduction](2026-09-10-arm-state-cutover-data/lunch-parent-red.txt)). These are pre-existing wall-clock dependencies, not regressions introduced by either PR. Earlier pre-noon PASS results are not a substitute. The arm-state sweep/settlement and leg-4 pins remain green.

Repairing the fixture clock fully also requires addressing the plan-provider wall-clock seam: installActivePlanProvider uses time.Now inside the installed provider while the arm manager already offers maybeManageArmedOrdersAt. Merely passing a fixed clock to the arm manager leaves those readers on different dates/times. No such production seam or fixture change was made during this cutover attempt. The owner was asked whether those repairs and the two lock checks should stay with their lanes or expand this lane.

**No build, Guide stamp, dist, RELEASE change, binary swap, kill or lock acquisition occurred.** The requested boot remains on hold for the test failures and the active main-tree editor. The initial GO stands for a fresh flat gate after those blockers are resolved; no order-specific exception or gate bypass has been introduced.
