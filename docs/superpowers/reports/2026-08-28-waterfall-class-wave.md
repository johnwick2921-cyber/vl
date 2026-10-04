> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# WATERFALL-CLASS WAVE — BUILD & PROVE (NO DEPLOY)

Branch `fix/waterfall-class` off dev (`2850e351`). Built in the isolated
worktree `~/vl-waterfall` (the main tree hosted an in-flight GAR follow-up
WIP — worktree law). **NOT deployed.** Evidence base:
`docs/superpowers/reports/2026-08-28-missed-200pt.md` (2026-08-28 -347pt crash,
zero plan-legal entries, $0-by-own-rules verdict).

## F1 [A] — the 8th condition: `breakdown_continue` / `breakup_continue`

- **Schema** (`kernel/plan_doc.go`): `PlanScenario.Breakdown` =
  `{level, level_label, entry_mode: pullback|immediate}`; condition list
  extended. `confirm` = leg 1 (the breakdown), `confirm2` = leg 2 (the failed
  retest).
- **Machine state + validator** (`kernel/breakdown_continue.go`):
  `BreakdownContinueState` evaluates both legs from the 1m snapshot with the
  same best-run semantics as `EvaluateConfirm`; `ValidateBreakdownContinueScenarios`
  re-checks at write time: ≥2 closes beyond the level (BD_CONFIRM_CLOSES),
  displacement ≥ **BD_MIN_DISP_ATR (1.0×ATR5m)**, no reclaim close, level
  within BD_MAX_LEVEL_DIST_ATR (5.0×ATR — the reference case's VWAP was 57pts
  behind price at birth), entry_mode valid, and arm chain rules.
- **ARMABLE** (`kernel/armed.go`, `plan_doc.go` ArmSpecValid): resting limit AT
  the broken level, `wait_confirm:true` chained on leg 1, `entry_mode=pullback`
  only; entry = level ± 1 tick into the trade's favor. Min-SL ≥ 1×ATR5m
  enforced at write.
- **Playbook** (`kernel/planner_prompt.go` WATERFALL PLAY paragraph): author
  when one-sided delivery / >1.2×ATR gap-and-go / waterfall after a failed
  rally — cites today's case; targets chain to next liquidity; SL beyond the
  failed pullback extreme.
- **Fixture (the tape that must work):** `TestBreakdownContinueValidatorRealTape`
  replays the 10:25-11:24 CT waterfall around VWAP 29657.39 — the plan
  validates at the 10:51 write cut and is triggerable: leg 1 MET at birth
  (displacement ≥1×ATR, no reclaim), a synthetic retest-that-fails fires leg 2.
  Rejections proven: weak displacement, reclaimed breakdown, immediate-mode
  arm, un-chained arm. Long mirror (`TestBreakupContinueMirror`) passes.

## F2 [A] — two-leg confirm rendering

- `ConfirmVerdict` now carries `legs[]`; `EvaluateScenarioConfirm` aggregates
  leg1 && leg2 with **leg 2 windowed from leg 1's first fire** (a pre-breakdown
  touch can't satisfy the retest leg).
- Plan-status (`RenderConfirmLines`) prints every leg:
  `S2 confirm: leg 1/2 1x 5m close — MET · leg 2/2 touch — NOT MET → overall NOT MET`
  — a partial NEVER prints as bare MET; only the overall verdict feeds the
  CONFLICT logic. Single-leg renders are byte-identical to before.
- Card (`web/src/components/plan/ScenarioList.tsx` ConfirmChip) renders leg
  states; the meta map (`auto_trader_levelstate.go`) now uses the two-leg
  evaluator.
- **Fixture:** `TestTwoLegConfirmRenderS2Fixture` re-produces the exact S2
  10:54 state — leg 1 MET (close 29613.50), retest never came → render shows
  both legs + `overall NOT MET`, contains no bare "— MET (".

## F3 [S] — fast-market wake reads

- `FAST_MARKET_ATR` (1.5×ATR5m): when a wake fires with |price drift| since the
  last plan write above the threshold, the read runs the
  `FAST_MARKET_REASONING` wire (default **fast** = enabled/low) and the planner
  prompt carries `⚡ FAST TAPE — price has moved X pts (Y×ATR5m) … keep the plan
  SHORT` (PlannerInput.FastTape / BuildPlannerPrompt).
- The 361.6s / 90pt-stale wake-read class dies: the mode choice is logged
  (`🧠 planner mode: fast-market (drift …×ATR5m) — reasoning downgraded to
  fast→low`), the drift baseline is the last successful plan-write price
  (`recordPlanWritePrice`).
- Target birth latency < 120s in fast regimes (verified at the next real fast
  wake; the wire switch is unit-tested).

## F4 [XS] — the visible planning gap

- Executor contract (`engine_prompt_futures.go`): a valid breakdown thesis with
  no matching scenario MUST appear in the wait reasoning as the exact phrase
  **`no breakdown scenario authored`**.
- Telemetry: `telemetry.IncBreakdownGapNoted` counts occurrences per
  session-day (surfaced under the gate-blocks endpoint as
  `no_breakdown_scenario_authored`), hooked at decision-record time.

## Gates

- `go test ./...` — **27 packages ok, 0 FAIL** (incl. new
  breakdown/two-leg/fast-wire tests).
- Goldens **re-pinned** (`futures_mnq_empty/keylevels/plan.golden`) — the F4
  line is a deliberate BASE-CONTRACT change (all futures prompts), not a
  feature-flag leak; `UPDATE_GOLDEN=1` recapture + verify PASS.
- FE: `tsc --noEmit` clean, vitest **277/33 passed**.

## STOP

Built and proven only. **Awaiting the owner's "go cutover"** — no deploy, no
RELEASE bump, no restart. The cutover (flat-gated, canon) will be executed only
on the owner's explicit ack; the binary must be rebuilt from the main checkout
at the deploy commit (worktree builds lose vcs stamping).

Pinned: https://github.com/johnwick2921-cyber/vl/blob/5be9b1b5f65ca0df1581342861aae9c87107a68e/docs/superpowers/reports/2026-08-28-waterfall-class-wave.md
