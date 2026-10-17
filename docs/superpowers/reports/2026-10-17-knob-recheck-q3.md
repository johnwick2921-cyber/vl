# Knob recheck Q3 — "OFF on screen, ON in the bot" hunt (release #32)

- Lane: DS-103
- Base: `origin/release/2026-10-16-32` @ `a8b849161`
- Branch: `audit/knob-recheck-q3` (claim `ee93a632a`)
- Knob list: `/home/hoang/fleet-1017/knobs-q02.txt` (52 registry paths)
- Three-state comparison per the CTO 2026-10-17 order: **MISSING** / **OFF·false·0** / **ON·true·value**, each as "Studio shows | bot does".
- `#571` (fix/mentor-knob-truth @ 0d48f5a26) read in full; a mismatch #571 already fixes is `FIXED-BY-571`, not a new finding.

## Summary

| Verdict | Count |
|---|---|
| MISMATCH (P0/P1) | **0** |
| HIDDEN (no Studio control, bot behaviour) | 3 |
| DEAD (no consumer / inert on futures) | 3 |
| FIXED-BY-571 (effective row added by #571; screen already matches bot) | 14 |
| MATCH | 31 |
| P2 cosmetic (stale comment only) | 1 |
| **Total** | **52** |

**No P0 and no P1 in Q3.** The "OFF on screen, ON in the bot" shape (the `mentor_done_after_win` bug) does not occur in this list. Every boolean knob with a Studio control has a screen default that matches the Go nil default; the only knobs with hidden bot behaviour are documented "No Studio control" in the guide, and the dead knobs have no screen at all.

## Findings (the non-MATCH rows)

### HIDDEN — bot behaviour with no Studio control

| Knob | Bot (nil/absent) | Screen | Effective row | Severity |
|---|---|---|---|---|
| `planner_contract` | ON (A3 contract: confirming close, planned_order entry policy, composed-stop REJECT) — `store/strategy.go:PlannerContractOn`, registry note "nil=ON" | No control; guide `settings.ts:44` "No Studio control — stored setting day_plan.planner_contract (API/config field)" | yes (#569) | P2 |
| `planner_fresh_tape` | ON (A6 born-dead retry re-sights the fresh tape) — registry note "nil=true (ON, shipped default)" | No control; guide `settings.ts:419` "No Studio control — stored setting day_plan.planner_fresh_tape (API/config field)" | yes (#569) | P2 |
| `realign_cap` | constant 5 unless a stored value exists (owner stores 10, honoured + logged) — `KnobFolded`, control removed | control removed (W-KNOB-PRUNE) | yes | P2 (folded) |

### DEAD — no consumer, or inert on futures

| Knob | Evidence | Severity |
|---|---|---|
| `notional_cap_enabled` | `KnobIneffective`, `Consumers: nil` — "crypto-era toggle; the futures notional cap (equity × max_notional_leverage) is always active regardless of this stored flag". The real cap is visible via `max_notional_leverage` (RiskControlEditor.tsx:1290 + effective row). No Studio control for `notional_cap_enabled`. | P2 |
| `quant_data` | registry "crypto path only; inert on futures (CLASS 151)". No Studio control. | P2 |
| `refresh_secs` | registry "crypto data fetch (FetchExternalData has no production caller); inert on futures". No Studio control. | P2 |

### P2 cosmetic — stale comment only (screen and bot agree)

| Knob | Finding |
|---|---|
| `flag_wall_target` | `web/src/types/strategy.ts` type comment says `[false]`, but the actual default is ON: `MentorTuningPanel.tsx` `MENTOR_TUNING_DEFAULTS.flag_wall_target = true` (the placeholder the screen shows) and the registry "nil → ON (course-defaults-30)" agree. The `[false]` comment is stale. |

## FIXED-BY-571 — the 14 `risk_control.mentor_tuning.*` knobs

All 14 rows go through `trader.mentorTuningResolve`. On release #32 they have **no effective-settings row** (the owner cannot see the value the bot RUNS); `#571` adds a resolver for every one (`mentorTuningRow` in `trader/effective_settings.go`). For each, the **Studio placeholder already matches the bot nil default** (`MentorTuningPanel.tsx` `MENTOR_TUNING_DEFAULTS` mirrors `kernel/mentor DefaultConfig`), so there is no screen-vs-bot mismatch to fix — only the missing effective row, which #571 supplies.

| Knob | nil/absent → bot | Screen placeholder | Verdict |
|---|---|---|---|
| `15m_ib_requires_5m_isb` | `require_5m_isb` | `require_5m_isb` | FIXED-BY-571 (MATCH) |
| `box_new_zone_after_escape` | ON | ON (`true`) | FIXED-BY-571 (MATCH) |
| `d43_trend_source` | `structure` | `structure` | FIXED-BY-571 (MATCH) |
| `day_gate_spent_pts` | 300 | 300 | FIXED-BY-571 (MATCH) |
| `day_gate_target_cap_pts` | 15 | 15 | FIXED-BY-571 (MATCH) |
| `ema34_loss_relock` | `until_zone_exit` | `until_zone_exit` | FIXED-BY-571 (MATCH) |
| `exec_2m_after_30m` | ON | ON (`true`) | FIXED-BY-571 (MATCH) |
| `flag_wall_target` | ON | ON (`true`) | FIXED-BY-571 (MATCH) |
| `htf_gate_news_only` | OFF | OFF (`false`) | FIXED-BY-571 (MATCH) |
| `isb_box_escape` | `body` | `body` | FIXED-BY-571 (MATCH) |
| `isb_reverse_ema9_enabled` | ON | ON (`true`) | FIXED-BY-571 (MATCH) |
| `leg_reset_on_wave_break` | ON | ON (`true`) | FIXED-BY-571 (MATCH) |
| `level_entries_need_trigger_side` | OFF | OFF (`false`) | FIXED-BY-571 (MATCH) |
| `level_max_visits` | 3 (0 = no cap) | 3 | FIXED-BY-571 (MATCH) |

## MATCH — one line each (screen mirrors Go default in all three states)

- `min_scenario_quality` — screen `?? 'C'`, Go ""/C = no restriction (`kernel/engine_position.go:300`). MATCH.
- `model_limits` — API-computed metadata, not a stored leaf (registry). No control; excluded from schema walk. MATCH (not a knob).
- `name` — strategy identifier, shown as the strategy name. MATCH.
- `one_setup_enabled` — screen `!== false` → ON; Go nil=ON. MATCH.
- `one_setup_min_grade` — screen `?? 'B'`; Go empty=B. MATCH.
- `picture_htf` (parent) — nil=OFF; screen `enabled === true` → OFF. MATCH.
- `picture_htf.enabled` — screen OFF; Go nil=OFF. MATCH.
- `picture_htf.entry_window_sec` — placeholder 360; Go default 360. MATCH.
- `picture_htf.freshness_sec` — placeholder 30; Go default 30. MATCH.
- `picture_htf.min_rr` — 0 = strategy min R:R; Go `MinRR` 0 = configured min. MATCH.
- `picture_htf.pivot_window` — placeholder 120; Go default 120. MATCH.
- `picture_htf.swing_lookback` — placeholder 24; Go default 24. MATCH.
- `picture_htf.tick_size` — placeholder 0.25; Go default 0.25 (MNQ). MATCH.
- `plan_enabled` — screen default `false`; Go `PlanEnabled` default false (master switch). MATCH.
- `plan_mode` — screen default `advisory`; Go default advisory. MATCH.
- `planner_atr_floor_cap_at_invalidation` — nil=off (today); screen reflects off-by-default enum. MATCH.
- `planner_contracts` — screen blank + effective line (1); Go unset/0 → 1. MATCH.
- `planner_exit_on_thesis_death` — screen `|| 'off'`; Go off default. MATCH.
- `planner_model` — screen blank; Go empty → primary model (resolver added #569). MATCH.
- `planner_momentum_into_zone_max_atr` — 0=off default; screen 0/off. MATCH.
- `planner_no_chase_enforce` — screen `=== true` → OFF; Go nil=WARN-first (does not refuse). MATCH.
- `planner_room_gate` — screen `|| 'off'`; Go off default. MATCH.
- `planner_room_min_r` — screen blank; Go unset=1.0. MATCH.
- `planner_stale_zones_max_atr` — 0=off default. MATCH.
- `planner_timeframes` — screen `['D','4h','1h','15m']`; Go default same. MATCH.
- `primary_count` — IndicatorEditor control; Go `klines.primary_count`. MATCH.
- `primary_timeframe` — IndicatorEditor control; Go `klines.primary_timeframe`. MATCH.
- `prompt_variant` — StrategyStudioPage Phase-2 "Market + Mode" selector (combines into `prompt_variant`); Go `resolvePromptVariant` (no saved → default). MATCH.
- `proximity_filter_atr` — screen 1.5; Go default 1.5. MATCH.
- `publish_config` — StrategyStudioPage pass-through; Go `api/strategy.go:42`. MATCH.
- `reentry_cooldown_minutes` — numeric, control present; Go `kernel/engine_analysis.go:619`. MATCH.
- `replan_cap` — screen seed 2; Go `ResolveReplanCap` (per-session → strategy → default). MATCH.
