# 2026-10-17 knob recheck Q2 — DS-102

Base: `origin/release/2026-10-16-32` (`a8b849161`). Slice: `/home/hoang/fleet-1017/knobs-q01.txt` (72 knobs). READ-ONLY; no fixes. Three states per knob: **MISSING** (nil/absent), **OFF** (explicit false/0), **ON** (explicit true/value).

## Summary

- **MATCH** (screen default = bot default, all three states): **58**
- **HIDDEN** (bot behaviour with NO Studio control): **9** — all mentor size/trail/leg knobs the owner cannot see or set
- **DEAD** (registry KnobLive but only a crypto-path reader; inert on futures): **12**
- **INEFFECTIVE** (registry KnobIneffective, no consumer): **3**
- **CANDIDATE** (no production reader found): **2**
- **FOLDED** (control removed, stored value honoured): **1**

**No P0/P1 screen-vs-bot MISMATCH at the release #32 base.** The `mentor_done_after_win` shape (screen OFF / bot ON) is gone: the toggle exists, nil→OFF, and the eff resolver reads it. But two **#571-introduced regressions** must be flagged for the merge (see F1/F2): #571 is based on dev and would (1) DELETE the `mentor_done_after_win` Studio toggle, and (2) flip the `exec_2m_after_30m` default label to OFF while the bot is ON.

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

## Table (72 knobs)

Legend: M = MATCH · H = HIDDEN · D = DEAD(crypto) · I = INEFFECTIVE · C = CANDIDATE · F = FOLDED.
Bot default = nil/absent resolution (release #32). Screen default = Studio control default/placeholder.

| knob | verdict | bot default (nil/absent) | screen default | states checked | registry | eff row | tier |
|---|---|---|---|---|---|---|---|
| geometry_reference_levels | M | ON (owner ruling) | ON (day-plan) | T/O/F | Live | yes | A |
| grid_config | D | — (crypto) | grid editor | — | Live | no | A |
| grid_count | D | — (crypto) | grid editor | — | Live | no | A |
| guardrails_enabled | M | OFF (nil) | OFF | T/O/F | Live | yes | A |
| headers | D | — (crypto fetch) | — | — | Live | no | A |
| hold_discipline | M | OFF | OFF | T/O/F | Live | yes | A |
| htf_seats | M | 2 (nil) | — | 0..6 | Live | yes | A |
| htf_veto | M | (resolver) | day-plan | T/O/F | Live | yes | A |
| id | M | — | — | — | Live | no | B |
| indicators | M | — | indicator editor | — | Live | no | B |
| is_active | M | — | — | — | Live | no | B |
| is_default | M | — | — | — | Live | no | B |
| is_public | M | — | Publish | — | Live | no | B |
| klines | M | — | klines editor | — | Live | no | B |
| language | M | en | — | — | Live | no | B |
| last_entry_offset_min | M | (resolver) | day-plan | 0..N | Live | yes | A |
| level | M | — | — | — | Live | no | B |
| levels_fresh_by_tf | F | OFF (folded) | (removed) | — | Folded | no | A |
| leverage | D | — (crypto) | grid editor | — | Live | no | A |
| longer_count | C | (clamp only) | klines | — | Candidate | no | A |
| longer_timeframe | M | 4h | 4h | — | Live | no | B |
| lower_price | D | — (crypto) | grid editor | — | Live | no | A |
| max_contracts_enabled | I | — (no consumer) | (parse-only) | — | Ineffective | no | A |
| max_contracts_per_order | M | 2 | 2 | 1..N | Live | yes | A |
| max_daily_trades | M | OFF (nil) | OFF | T/O/F | Live | yes | A |
| max_daily_trades_enabled | M | OFF (nil) | OFF | T/O/F | Live | yes | A |
| max_drawdown_pct | D | — (crypto) | grid editor | — | Live | no | A |
| max_levels | M | (resolver) | day-plan | 0..N | Live | yes | A |
| max_margin_usage | I | 1.0 clamp (prompt only) | risk field | — | Ineffective | no | A |
| max_notional_leverage | M | 20 | 20 | — | Live | yes | A |
| max_positions | M | 3 | 3 (1–3) | 1..3 | Live | yes | A |
| max_trades | C | (session leaf) | per-session | — | Candidate | yes(session) | B |
| mentor_base_contracts | H | 4 | none | — | Live | no | A |
| mentor_be_before_partial | M | ON (nil) | ON | T/O/F | Live | no | A |
| mentor_big_contracts | H | 4 | none | — | Live | no | A |
| mentor_box_fresh_rth_pair | M | OFF (nil) | OFF | T/O/F | Live | no | A |
| mentor_box_max_height_pts | M | 0=off | 0=off | 0..N | Live | no | A |
| mentor_confluence_contracts | H | 4 | none | — | Live | no | A |
| mentor_day_off_recheck | H | OFF (nil) | none | — | Live | no | A |
| mentor_done_after_win | M | OFF (nil) | OFF | T/O/F | Live | yes | A |
| mentor_done_after_win_day_start | H | 08:30 | none | — | Live | yes | A |
| mentor_ema9_visit_cap | M | shared | shared | shared/per_level/exempt | Live | no | A |
| mentor_ema_max_cross_30m | H | ON=2 | none | — | Live | no | A |
| mentor_isb_c3_partial_any_profit | M | ON (nil) | ON | T/O/F | Live | no | A |
| mentor_isb_max_pts | M | 30 | 30 | 0..N | Live | no | A |
| mentor_keypair_candle_ratio | M | 1 (0→1) | 1 | 0..N | Live | no | A |
| mentor_leg_budget_enabled | H | ON (nil) | none | — | Live | no | A |
| mentor_leg_reset_on | H | close | none | — | Live | no | A |
| mentor_loc_trigger_filter | H | ON (nil) | none | — | Live | no | A |
| mentor_loss_departure_pts | H | 0=OFF | none | — | Live | no | A |
| mentor_lvl_revisit_min_pts | H | 0 | none | — | Live | no | A |
| mentor_max_contracts | H | 20 (ceiling) | text only | — | Live | yes | A |
| mentor_mode | M | OFF | OFF | T/O/F | Live | no | A |
| mentor_reduced_contracts | H | 4 | none | — | Live | no | A |
| mentor_resonance_runner_tp_old_high | M | ON (nil) | ON | T/O/F | Live | no | A |
| mentor_reverse_isb_as_phl | M | ON (nil) | ON | T/O/F | Live | no | A |
| mentor_spent_day_contracts | H | 2 (dormant) | none | — | Live | no | A |
| mentor_stale_data_block | M | ON (nil) | ON | T/O/F | Live | no | A |
| mentor_stop_after_loss | M | OFF (nil) | OFF | T/O/F | Live | no | A |
| mentor_swing4h_contracts | M | 2 | 2 | 1..N | Live | no | A |
| mentor_swing_ema34_is_first_target | M | ON (nil) | ON | T/O/F | Live | no | A |
| mentor_swing_runner | M | ON (nil) | ON | T/O/F | Live | no | A |
| mentor_trail_mode | M | candle | candle | candle/off | Live | no | A |
| mentor_trail_tf | H | 1m | none | — | Live | no | A |
| mentor_window_minutes | M | 60 | 60 | 30/60/90/120 | Live | no | A |
| mentor_window_start | M | 08:30 | 08:30 | HH:MM | Live | no | A |
| method | D | — (crypto fetch) | — | — | Live | no | A |
| min_confidence | M | 60 | 60 | 0..100 | Live | yes | A |
| min_grade | M | B | B | A/B/C | Live | yes(session) | A |
| min_hold_min | M | 3 | 3 | 0..N | Live | yes | A |
| min_position_size | I | — (crypto hardcode) | risk field | — | Ineffective | no | A |
| min_risk_reward_ratio | M | 3.0 | (stored 2) | 1..10 | Live | yes | A |
