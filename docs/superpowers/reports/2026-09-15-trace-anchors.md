# TRACE ANCHORS — tables, counters, journal, API (read at origin/dev `8f5f26ee`, 2026-09-15, [A])

Every "why did X happen / not happen" question resolves through these anchors.
All queries are READ-ONLY (`sqlite3 -readonly`).

## 1. SQLite tables (file:line of definition)

**Raw DDL:** `system_config` (store.go:147) · `plans` (plan.go:111) ·
`plan_overlays` (plan.go:131) · `plan_lifecycle_log` (plan.go:525) ·
`armed_orders` (armed_orders.go:136) · `trade_excursions` (trade_excursion.go:107)
· `ab_confirm_log` (ab_confirm.go:83).

**GORM:** `decision_records` (decision.go:72) · `bars` (bar_history.go:62, PK
(symbol,tf,open_time_ms), contract-stamped) · `log_events` (log_event.go:46) ·
`touch_episodes` (touch_episode.go:38) · `touch_outcomes` (touch_outcomes.go:197)
· `strategies` (strategy.go:713) · `traders` (trader.go:74) · `users` ·
`exchanges` · `ai_models` · `ai_charges` · `trader_positions` (position.go:231,
status OPEN/CLOSED) · `trader_orders` · `trader_fills` ·
`trader_equity_snapshots` · `accepted_risk` · `level_state` (level_state.go:96)
· `level_stats` · `owner_levels` · `session_profiles` · `calendar_slices` ·
`day_plan_digests` · `day_plan_alerts` · `plan_qa` · `planner_read_facts`
(planner_read_facts.go:112) · `planner_rejected_prompts` · `candidate_pool` ·
`matched_random_verdicts` · `matched_random_weekly` · `nt8_order_snapshots` ·
`config_changes` (config_diff.go:32) · `watch_assessments` · `watchdog_fires`.

## 2. Durable counters (system_config keys)

| Key pattern | Meaning | Writer |
|---|---|---|
| `arm_refusals_0b:<trader>:<date>:<session>:<class>` | per-session-day refusal census | zerob_counters.go:48 |
| `arm_stop_unanchored_0b` | stop-anchor misses | zerob_counters.go:24 |
| `one_setup:<trader>:<planID>:v<version>` | one-setup verdicts (planID embeds date:session) | one_setup.go:48 |
| `plan_dormant_since:<planID>:<version>` | value = "0" when rearmed, epoch-ms when dormant | auto_trader_planner.go:592 |
| `uncarried_edits:<plan_id>:v<version>` / `confirm_grace_sessions_seen` | owner-edit carry | plan.go:440 / planner |
| class47 wake counters / `ArmSupersededKey` / shadow-ab / post-loss | wake+seam counters | class47_counters.go etc. |

Census query:
```bash
sqlite3 -readonly data/data.db "SELECT key,value FROM system_config WHERE key LIKE 'arm_refusals_0b%<DATE>%' ORDER BY key;"
```

## 3. Journal greps (journalctl -u nofx)

| Question | grep |
|---|---|
| Boot proof | `BOOT INTEGRITY OK` (also `bars integrity OK`, goldens PASS) |
| Economics honesty (R4) | `scenario economics:` → `contradictions refused= corrected=` |
| Arm refused | `armed` / `geometry_` / `one_setup:` / `entry_gate` |
| Breakeven/trailing | `auto-breakeven` / `trailing_armed` / `move_stop` |
| Planner rejects | `planner attempt` / `parse/schema rejected` |
| Config changes | `config diff (studio_save)` → `reloaded N running trader(s)` |
| Far arms (advisory) | `arm far:` |
| Clock drift | `CLOCK EARLY-WARNING` (log-only WSL2 issue) |
| Position truth | `positions snapshot account=` (count=0 = flat) |

## 4. API (JWT mint + curl)

Mint (one-liner): HS256 with `.env` JWT_SECRET; header {alg HS256, typ JWT};
claims sub + user_id=`396db319-d3fe-4d63-9b97-516ff0008f53`,
email=`johnwick2921@gmail.com`, iss=`nofxAI`, exp now+3600. (Working token
cached at `/tmp/pw-token`.)

| Intent | Endpoint |
|---|---|
| Health/rev | `GET /api/health` |
| Open positions | `GET /api/positions?trader_id=<id>` |
| Closed history | `GET /api/positions/history?trader_id=<id>&limit=200` |
| Gate-block census | `GET /api/risk/gate-blocks?trader_id=<id>` |
| Risk honesty (not_enforced) | `GET /api/risk/status?trader_id=<id>` |
| Desk strip (resolved limits + sources) | `GET /api/desk?trader_id=<id>` |
| Current plan | `GET /api/plan/today?trader_id=<id>` |
| Knob narration | `GET /api/config/resolved` |
| Errors | `GET /api/risk/errors?trader_id=<id>` |

Trader id: `8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265`.

## 5. Refusal reason → owner of the string (file:line)

| Reason | File:line |
|---|---|
| `daily_force_flat` | entry_gate.go:172 |
| `strict` (4 variants) | entry_gate.go:183-193 |
| `against plan bias` / `direction mismatch` / `invalidated at` / `SHADOW` | entry_gate.go:206/217/249/257 |
| `R:R below floor` | entry_gate.go:280 + armed_executor.go:2150 |
| `too close` (min-SL) | entry_gate.go:291 + armed_executor.go:2162 |
| `one_open_position` | entry_gate.go:303 |
| `consecutive_loss` / `no_trade_band` | session_risk.go:101/:95 |
| `geometry_<reason>` (19 reasons) | structural_geometry.go:31-205 |
| `one_setup:*` (8) | one_setup_wiring.go + store/one_setup.go:175-184 |
| `stop_entry:*` | armed_executor.go:1632-1641 |
| `slot_*` / `cancel_unconfirmed` / `broker_book` | cancel_confirm.go |
| `one_contract_*` | one_contract.go:201-214 |
| `flat_claim_refused_broker_disagrees` | auto_trader_clock.go:560/767 |
| `planner_fail_closed` / `arm_authored` / `plan_*` | auto_trader_planner.go |

## 6. Verification shortcuts for a fresh session (5 minutes)

```bash
# code truth
cd /home/hoang/nofx-rrfix   # or any clean worktree at origin/dev
git fetch origin && git log -1 --oneline origin/dev
grep -n 'corrected' kernel/scenario_economics.go | head -3      # R4 in place
grep -n 'armMinRRFor(nil)' trader/auto_trader_planner.go        # R4 call site
grep -n 'EXIT_MECHS_SUSPENDED' /home/hoang/nofx/.env            # unsuspend
# live truth
curl -s http://127.0.0.1:8080/api/health
sqlite3 -readonly /home/hoang/nofx/data/data.db "SELECT COUNT(*) FROM trader_positions WHERE status='OPEN';"
journalctl -u nofx --since '30 min ago' -o cat | grep -a 'scenario economics:' | tail -1
```

## 7. Known honest gaps (do not "fix" silently)

- research snapshots log `missing=map[…]` per event type (fields not captured) —
  by design.
- `/api/risk/status` `not_enforced` array — by design (D6d).
- `plan_dormant_since` value: **"0" = rearmed, epoch-ms = dormant** (corrected by
  the 20-agent sweep — there is no literal "2|0" value).
- touch-reference-missing by design (`plan_confirm.go`); subagent-documented
  anomalies live in session memory R1/R2.
