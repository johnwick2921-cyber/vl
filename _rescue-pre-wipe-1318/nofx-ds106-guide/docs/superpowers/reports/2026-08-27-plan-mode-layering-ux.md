names rewritten to vl on 2026-09-30 (VL rename)
# UX fix (S) — plan_mode layering honesty (2026-08-27)

FE + docs only. Cutover may ride any flat window — no urgency; the Go boot is
unaffected (no binary change, no `deploy/RELEASE` bump).

Branch `fix/plan-mode-tristate` · code commit `da2da019b402907c4db12c9fd4584bbb1207a637` (immutable).
Report pinned via the commit-ref URL below — re-pinned to the dev merge commit `00533398` (the file exists at that exact commit, so the URL always resolves).

## What shipped

1. **Tri-state session knobs** (`web/src/components/strategy/DayPlanEditor.tsx`):
   - `plan_mode`: inherit (default, stores NOTHING) / advisory / direction / strict
   - `min_grade` + `min_scenario_quality`: inherit / A / B / C
   - `max_trades`: inherit / custom (+ number field)
   - `TriStateRow` renders the control always-visible with ⚪/🔸 state chips.
2. **Migration** — `migrateEqualOverrides`: a stored session override EQUAL to
   the global value is dropped (migrated to inherit) at editor load and
   re-emitted upstream so the next save persists the cleanup. Applies to
   `plan_mode` and `min_scenario_quality` (the two knobs with a global row).
3. **Global live-effect line** — the global `plan_mode` row now renders
   `STRICT — overridden in: ASIA, LONDON, NY ⚠` (i18n `planModeOverriddenIn`)
   whenever any session carries a differing override.
4. **Guide §7** (`web/src/guide/content/settings.ts`) — the
   inherit/override precedence rule now stated on every dual-scope knob card
   (plan mode, proximity, max re-plans, acceptance, min scenario quality,
   min levels per side) + the Session overrides card (tri-state semantics).

## 1. Migration semantics vs the owner's live state (verified)

The owner's live config is **global `plan_mode: "strict"` + sessions
`plan_mode: "advisory"`** — session ≠ global, so the migration does NOT touch
them; those three `advisory` overrides are real and survive. **The UX fix alone
does NOT change his effective mode.** Until he acts, every session still
resolves advisory (`store/strategy.go` `PlanModeFor`: session override wins).

Owner's exact click path to make strict effective (after this deploys):
1. Open **Strategy → Day Plan** for the hoang trader.
2. Expand each **Session row** (NY / ASIA / LONDON accordion).
3. On each row's **plan_mode** control choose either:
   - **STRICT** (explicit per-session strict), or
   - **inherit** (follows the global row, which is `strict`).
4. Press **Save** and confirm the "Strategy saved" toast.
After save, the global row shows no `overridden in:` warning (if inherit) or a
`STRICT`-mode per-session override.

## 2. PUT-path verification (create + edit; inherit removes the key)

Locked by two tests in `DayPlanEditor.test.tsx` (both green):

- **Edit path**: config with `NY.plan_mode = 'advisory'` → click `inherit` →
  emitted override is `undefined`, and `JSON.stringify(ny)` contains **no
  `"plan_mode"` key** — the wire never carries null or `''`.
- **Create path**: fresh config → STRICT → inherit round-trip →
  `JSON.stringify(ny)` contains none of `plan_mode`, `min_grade`,
  `min_scenario_quality`, `max_trades`.

Defense in depth on the Go side (verified unchanged): every resolver guards the
empty case — `PlanModeFor` / `MinScenarioQualityFor` require
`TrimSpace(*ov) != ""`, `MinGradeFor` returns `""` (no filter) on empty, and a
JSON `null` unmarshals to a nil pointer = inherit. So even a hostile empty
string can never be read as an override.

## Tests

- Affected: `DayPlanEditor.test.tsx` 17 passed, `GuidePage.test.tsx` 8 passed
  (27 total, includes the 4 new tri-state/migration/warning/PUT tests).
- Full FE suite: **275 passed / 33 files** (`vitest run`).
- `tsc --noEmit` clean.
- Go sanity (untouched): `go build ./...` + `go test ./store/` green.

## Pinned report

Commit-ref URL (canon — blob-SHA 404s on this repo):
`https://github.com/johnwick2921-cyber/nofx/blob/00533398eea9c9958b8fb4dcbb6f6fbb297d81f6/docs/superpowers/reports/2026-08-27-plan-mode-layering-ux.md`
