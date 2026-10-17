names rewritten to vl on 2026-09-30 (VL rename)
# G-DISPATCH — IN-APP USER GUIDE PAGE (2026-08-27)

## 1. Scope + laws

- **Dispatch:** "IN-APP USER GUIDE PAGE — COMPLETE SPEC (FINAL, v2)" on
  `feat/guide-page` → PR **#84**.
- **Laws honored:** VERIFY-FIRST (every engine cite grepped before writing),
  REALITY-FIRST (mock card built from REAL components only), FE-only (zero
  engine/Go changes), SIM-only, additive-only (no `git add -A`, one register
  item per commit).

## 2. What ships

### 5th nav page (`/guide`, auth-gated)

- `web/src/router/paths.ts` — `guide` page union + `ROUTES.guide` + `PAGE_PATHS`.
- `web/src/router/AppRoutes.tsx` — `<GuidePage />` route inside `AppChrome`.
- `web/src/components/common/HeaderBar.tsx` — 📖 Guide tab (desktop + mobile).

### Live-component exports (additive)

- `ScenarioList.tsx`: `QualityChip` (existing), NEW `ConfirmVerdict` /
  `ConfirmChip`, `FvgState` / `FvgStateChip`.
- `ZoneTable.tsx`: NEW `TouchChip` (approaching/touching/rejected/accepted).
- `chips.tsx` / `BiasBlock.tsx`: already exported (verified signatures).

### `web/src/guide/` (all new)

- `types.ts` — `GUIDE_BUILT_REV = '717acd34'`, `GuideSection`/`GuideBlock` union
  (p/h/cards/timeline/callout/table/code/checklists/faq/glossary/live/mockCard/
  knobs/buttons), `KnobSpec` (10 mandatory fields), `SearchHit`.
- `components/Example.tsx` — dashed-border live-example wrapper
  (`data-testid="guide-example"`).
- `components/KnobCard.tsx` — renders all 10 fields with ⭐ recommended.
- `components/MockPlanCard.tsx` — Section-3 mock built ONLY from real components
  (`BiasBlock`, `VersionChips`, `LifecycleChip`, `GradeChip`, `ProvenanceChip`,
  `FreshDot`, `StatusDot`, `QualityChip`, `ConfirmChip`, `FvgStateChip`,
  `TouchChip`). Includes: thin-side ⚖ note, bias + tree line, 4 level rows with
  roles + one consumed-dim row, S1/S2/S3 scenarios (S2 fvg_entry chain_after S1,
  FvgStateChip IN_ZONE #1, stale CONFIRM MET), death+flip, v3 footer.
- `content/*.ts` — 12 sections:
  1. welcome — what the system is, stack diagram, boot truth, quick start.
  2. trading-day — 17:00/01:55/08:25 reads, lunch, 14:45 EOD, session registry,
     executor steps, wake triggers.
  3. plan-card — mock card + 9 tap-to-expand callouts (bias, tree, versions,
     level/scenario anatomy, no-trade windows, death/flip, BOTH no-trade
     variants, thin-side) + 30-second read + the 4 buttons (Reset / Re-read /
     Re-align / Approve with api cites + budgets).
  4. levels — ~17 kind cards with verified stats (94.2% ON — n not recorded in that wave, pre-retention week: treat as anecdote per class-16 law; 75% reject-NY — fresh reconstruction at the same cut: n=7, 4W, +$386.5) and
     [UNVERIFIED] marks, grading pipeline, freshness ladder, floors/caps, 5
     roles, seats/band.
  5. plays — 7 playbook cards + the A-setup chain + no-trade-as-a-trade.
  6. guards — CAN-HARD-BLOCK vs ADVISORY truth table (17 rows), refusal
     decoder (8 entries), plan_mode table, guardrails + SIM lock.
  7. settings — **35 knob cards** (Day Plan 14, Risk 20, Sessions 1) = the
     live-page control count (100% match, exact-count linted by test).
  8. buttons — 6 dashboard actions + emergency-flat click order.
  9. routines — daily/weekly/emergency checklists.
  10. status — boot-ledger decode, SYSTEM_STATUS, gate-block labels, traffic
      light.
  11. glossary — 27 terms.
  12. faq — the 12 mandatory questions with mechanism + anchor links.
- `GuidePage.tsx` — sticky index (mobile chips), search across
  sections/glossary/FAQ/knob names, `#id` deep links, rev-drift banner
  (`GUIDE_BUILT_REV` vs `GET /api/health` revision → amber on drift).
- `GuidePage.test.tsx` — 10 vitest cases.

## 3. Knob census (Section 7) — verified consumer cites

Grepped before writing (not from memory):

- proximity: `kernel/levels_score.go:389` (ScoreLevels proximityK) ·
  `trader/auto_trader_planconfig.go:47`
- max_levels: `kernel/levels_score.go:54` (DefaultMaxLevels) ·
  `trader/auto_trader_planner.go:592`
- min_side_levels: `store/strategy.go:967` (MinSideLevelsFor) ·
  `kernel/levels_score.go:720` (MinSideLevels)
- replan_cap: `store/strategy.go:929` · `trader/auto_trader_planner.go:241`
- plan_mode: `trader/auto_trader_planconfig.go:158` (planModeFor) · 206-249
  (direction block)
- approval: `trader/auto_trader_orders.go:297` · `api/handler_plan.go:129`
- min_confidence: `kernel/engine_position.go:188`
- max_positions: `kernel/engine_analysis.go:125`
- min_risk_reward: `kernel/engine_position.go:122`
- position-value ratio: `trader/auto_trader_risk.go:229` ·
  `kernel/engine_analysis.go:527`
- guardrails: `kernel/risk_limits.go:178-199` (DailyGuardrails) ·
  `store/position_query.go:57` (consecutive losses)
- wakes: `trader/auto_trader_wake_levels.go:17-23` (wake wave,
  wake_min_interval_min)

## 4. Gates

- `npx tsc --noEmit` → **0 errors**.
- `npx vitest run` → **272/272 pass** (10 new guide tests).
- `npm run build` → green.

## 5. Commits (feat/guide-page)

1. `df63b037` scaffolding — routes, nav tab, live-component exports, page shell.
2. `56093efa` 12 typed content sections.
3. `ea5f846a` tests — knob-spec lint, render, search, deep links, drift banner.
4. `73be7cb8` docs — README-VL-SYSTEM refresh (rev 717acd34, §9 statuses, §10
   as-built waves).
5. `2caaf676` complete knob census (35 knobs = live-page count).

## 6. PR

**https://github.com/johnwick2921-cyber/nofx/pull/84**

## 7. Notes / follow-ups

- The `+dirty` boot flag remains explained by the untracked `.env.bak.0825-2157`
  (untouched, not committed).
- Guide built against rev `717acd34`; the drift banner will flag any future
  deploy until the guide rev is bumped.
