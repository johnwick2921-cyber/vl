# 2026-10-17 knob recheck Q2 — DS-102

Base: `origin/release/2026-10-16-32` (`a8b849161`). Slice: `/home/hoang/fleet-1017/knobs-q01.txt` (72 knobs). READ-ONLY; no fixes. Three states per knob: **MISSING** (nil/absent), **OFF** (explicit false/0), **ON** (explicit true/value).

## Summary (corrected — three-state columns added)

- **MATCH**: **44** (screen echoes stored/default value in all three states; bot resolves the same)
- **HIDDEN**: **15** (bot behaviour with NO Studio control — guide text only)
- **DEAD**: **7** (registry Live but crypto-only reader; inert on futures)
- **INEFFECTIVE**: **3** (registry Ineffective, no consumer)
- **CANDIDATE**: **2** (no production reader)
- **FOLDED**: **1** (control removed, stored value honoured)

Total 44+15+7+3+2+1 = **72**. **Zero MISMATCH in any of the three states at the #32 base.**

> Correction to my first bridge report: counts were 58 MATCH / 9 HIDDEN / 12 DEAD — those were wrong. HIDDEN is 15 (the earlier "9" was only the size-table subset; "12 DEAD" double-counted INEFFECTIVE/CANDIDATE/FOLDED). The table below is authoritative.

Three-state cells are `Studio → Bot` for MISSING / OFF(0) / ON(N). `—` = no control (HIDDEN) or no state semantics (meta).

## Findings (non-MATCH)

### F1 [A] — #571 deletes the `mentor_done_after_win` toggle (merge regression)
`mentor_done_after_win` is **MATCH at release #32**: toggle present (`web/src/components/strategy/MentorModeToggle.tsx:81,176,355,360-370`), bot `mentorDoneAfterWinEnabled` returns `v != nil && *v` (nil→OFF, `trader/mentor_tick.go`), eff resolver (`trader/effective_settings.go`, `mentor_done_after_win` + `_day_start`). But `git show origin/fix/mentor-knob-truth:web/src/components/strategy/MentorModeToggle.tsx | grep stopAfterWin` → **empty**; `git show .../web/src/types/strategy.ts | grep mentor_done_after_win` → **empty**. #571 rewrites the whole component (+258 lines) from a pre-#568 base and has no done-after-win control. A naive merge REVERTS the owner's 10-16 order. **The merge must keep #568's toggle.** Severity P0 (screen would hide a switch whose rule is still live in the bot).

### F2 [A] — #571 flips `exec_2m_after_30m` default label to OFF (merge regression, another checker's slice but cross-cited)
Release #32 TS: `exec_2m_after_30m?: boolean // X5-10 — unset = ON` (`web/src/types/strategy.ts:306`); bot `mentorExec2mKnobOn` returns `true` for nil (`trader/mentor_live_pos.go:802-805`); UI default `MENTOR_TUNING_DEFAULTS.exec_2m_after_30m = true`. MATCH. #571 diff changes the TS comment to `unset = OFF (the 1m ISB read)` — stale vs release #31/#565's ON ruling. Flag for DS-103/DS-106's merge.

### F3 [A] — Mentor size-table knobs have NO Studio control (HIDDEN)
`mentor_base_contracts` (nil→4), `mentor_confluence_contracts` (4), `mentor_big_contracts` (4), `mentor_reduced_contracts` (4), `mentor_spent_day_contracts` (2, dormant), `mentor_max_contracts` (ceiling 20) — consumers `trader/mentor_mode.go` `mentorKnobs`/`mentorSizeFor`; `git grep` in `web/src/components/strategy/*.tsx` finds no control (only `mentor_swing4h_contracts` and `mentor_isb_max_pts` are in `MentorCourseExitsPanel.tsx`). The owner cannot see the shipped 4=2+2 defaults. Severity P1 (display/visibility; sizing is correct, just invisible).

### F4 [A] — `mentor_trail_tf` and the leg/tuning knobs are HIDDEN
`mentor_trail_tf` (1m default), `mentor_leg_budget_enabled` (nil→ON), `mentor_leg_reset_on` (close), `mentor_lvl_revisit_min_pts` (0), `mentor_ema_max_cross_30m` (ON=2), `mentor_loc_trigger_filter` (nil→ON), `mentor_day_off_recheck` (nil→OFF), `mentor_loss_departure_pts` (0=OFF), `mentor_done_after_win_day_start` (08:30) — all have bot readers but no Studio control (`grep` in `web/src/components/strategy/*` empty). Severity P1/P2 (visibility only; the defaults are correct).

### F5 [A] — Crypto-only knobs are DEAD on futures (registry CLASS 151)
`grid_config`, `grid_count`, `leverage`, `lower_price`, `max_drawdown_pct`, `headers`, `method` are KnobLive in the registry but their only consumers are the crypto grid/prompt/data path (`kernel/grid_engine.go`, `kernel/engine.go`) — inert on the live ninjatrader strategies. Severity P1 (screen shows a control that does nothing on futures).

### F6 [A] — INEFFECTIVE knobs (registry KnobIneffective)
`max_contracts_enabled`, `max_margin_usage`, `min_position_size` — no production consumer (registry Notes: "no consumer — crypto-era toggle", "prompt text only", "crypto path hardcodes 12/60"). Severity P1/P2.

### F7 [A] — CANDIDATE knobs (no reader found)
`max_trades`, `longer_count` — registry KnobCandidate (field-grep found no behavioural reader). `longer_count` is clamp-filled/fingerprinted but no production reader; `max_trades` is a session leaf. Severity P2 (cosmetic / dormant).

## Table (72 rows)

Legend: M=MATCH · H=HIDDEN · D=DEAD(crypto) · I=INEFFECTIVE · C=CANDIDATE · F=FOLDED.
Cells are `Studio → Bot` per state: MISSING / OFF(0) / ON(N). `—` = no control or no state semantics.

| # | knob | verdict | MISSING | OFF/0 | ON/N | tier |
|---|---|---|---|---|---|---|
| 1 | geometry_reference_levels | M | ON→ON | OFF→OFF | ON→ON | A |
| 2 | grid_config | D | shown→ignored | shown→ignored | shown→ignored | A |
| 3 | grid_count | D | shown→ignored | shown→ignored | shown→ignored | A |
| 4 | guardrails_enabled | M | OFF→OFF | OFF→OFF | ON→ON | A |
| 5 | headers | D | shown→ignored | shown→ignored | shown→ignored | A |
| 6 | hold_discipline | M | OFF→OFF | OFF→OFF | ON→ON | A |
| 7 | htf_seats | M | 2→2 | 0→0 | N→N | A |
| 8 | htf_veto | M | def→def | OFF→OFF | ON→ON | A |
| 9 | id | M | — | — | — | B |
| 10 | indicators | M | — | — | — | B |
| 11 | is_active | M | — | — | — | B |
| 12 | is_default | M | — | — | — | B |
| 13 | is_public | M | — | — | — | B |
| 14 | klines | M | — | — | — | B |
| 15 | language | M | en→en | — | — | B |
| 16 | last_entry_offset_min | M | def→def | 0→0 | N→N | A |
| 17 | level | M | — | — | — | B |
| 18 | levels_fresh_by_tf | F | removed→OFF | removed→honoured | removed→honoured | A |
| 19 | leverage | D | shown→ignored | shown→ignored | shown→ignored | A |
| 20 | longer_count | C | — | — | — | A |
| 21 | longer_timeframe | M | 4h→4h | — | — | B |
| 22 | lower_price | D | shown→ignored | shown→ignored | shown→ignored | A |
| 23 | max_contracts_enabled | I | shown→ignored | shown→ignored | shown→ignored | A |
| 24 | max_contracts_per_order | M | 2→2 | 1→1 | N→N | A |
| 25 | max_daily_trades | M | OFF→OFF | OFF→OFF | ON→ON | A |
| 26 | max_daily_trades_enabled | M | OFF→OFF | OFF→OFF | ON→ON | A |
| 27 | max_drawdown_pct | D | shown→ignored | shown→ignored | shown→ignored | A |
| 28 | max_levels | M | def→def | 0→0 | N→N | A |
| 29 | max_margin_usage | I | shown→ignored | shown→ignored | shown→ignored | A |
| 30 | max_notional_leverage | M | 20→20 | 20→20 | 20→20 | A |
| 31 | max_positions | M | 3→3 | 1→1 | 2→2 | A |
| 32 | max_trades | C | — | — | — | B |
| 33 | mentor_base_contracts | H | —→4 | —→4 | —→N | A |
| 34 | mentor_be_before_partial | M | ON→ON | OFF→OFF | ON→ON | A |
| 35 | mentor_big_contracts | H | —→4 | —→4 | —→N | A |
| 36 | mentor_box_fresh_rth_pair | M | OFF→OFF | OFF→OFF | ON→ON | A |
| 37 | mentor_box_max_height_pts | M | 0→off | 0→off | N→N | A |
| 38 | mentor_confluence_contracts | H | —→4 | —→4 | —→N | A |
| 39 | mentor_day_off_recheck | H | —→OFF | —→OFF | —→ON | A |
| 40 | mentor_done_after_win | M | OFF→OFF | OFF→OFF | ON→ON | A |
| 41 | mentor_done_after_win_day_start | H | —→08:30 | —→08:30 | —→N | A |
| 42 | mentor_ema9_visit_cap | M | shared→shared | shared→shared | per_level→per_level | A |
| 43 | mentor_ema_max_cross_30m | H | —→ON(2) | —→ON(2) | —→N | A |
| 44 | mentor_isb_c3_partial_any_profit | M | ON→ON | OFF→OFF | ON→ON | A |
| 45 | mentor_isb_max_pts | M | 30→30 | 0→0 | N→N | A |
| 46 | mentor_keypair_candle_ratio | M | 1→1 | 1→1 | N→N | A |
| 47 | mentor_leg_budget_enabled | H | —→ON | —→OFF | —→ON | A |
| 48 | mentor_leg_reset_on | H | —→close | —→off | —→N | A |
| 49 | mentor_loc_trigger_filter | H | —→ON | —→OFF | —→ON | A |
| 50 | mentor_loss_departure_pts | H | —→OFF | —→OFF | —→N | A |
| 51 | mentor_lvl_revisit_min_pts | H | —→0 | —→0 | —→N | A |
| 52 | mentor_max_contracts | H | —→20 | —→20 | —→N | A |
| 53 | mentor_mode | M | OFF→OFF | OFF→OFF | ON→ON | A |
| 54 | mentor_reduced_contracts | H | —→4 | —→4 | —→N | A |
| 55 | mentor_resonance_runner_tp_old_high | M | ON→ON | OFF→OFF | ON→ON | A |
| 56 | mentor_reverse_isb_as_phl | M | ON→ON | OFF→OFF | ON→ON | A |
| 57 | mentor_spent_day_contracts | H | —→2 | —→2 | —→N | A |
| 58 | mentor_stale_data_block | M | ON→ON | OFF→OFF | ON→ON | A |
| 59 | mentor_stop_after_loss | M | OFF→OFF | OFF→OFF | ON→ON | A |
| 60 | mentor_swing4h_contracts | M | 2→2 | 1→1 | N→N | A |
| 61 | mentor_swing_ema34_is_first_target | M | ON→ON | OFF→OFF | ON→ON | A |
| 62 | mentor_swing_runner | M | ON→ON | OFF→OFF | ON→ON | A |
| 63 | mentor_trail_mode | M | candle→candle | off→off | candle→candle | A |
| 64 | mentor_trail_tf | H | —→1m | —→1m | —→N | A |
| 65 | mentor_window_minutes | M | 60→60 | 60→60 | N→N | A |
| 66 | mentor_window_start | M | 08:30→08:30 | 08:30→08:30 | N→N | A |
| 67 | method | D | shown→ignored | shown→ignored | shown→ignored | A |
| 68 | min_confidence | M | 60→60 | 0→0 | N→N | A |
| 69 | min_grade | M | B→B | C→C | A→A | A |
| 70 | min_hold_min | M | 3→3 | 0→0 | N→N | A |
| 71 | min_position_size | I | shown→ignored | shown→ignored | shown→ignored | A |
| 72 | min_risk_reward_ratio | M | 3.0→3.0 | 3.0→3.0 | N→N | A |
