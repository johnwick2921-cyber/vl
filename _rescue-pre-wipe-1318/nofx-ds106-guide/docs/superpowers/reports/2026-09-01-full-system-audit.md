names rewritten to vl on 2026-09-30 (VL rename)
# FULL SYSTEM AUDIT — research alignment · live status · every component · every counter · every open defect (2026-09-01)

**READ-ONLY.** No code, config, knob, DB, cutover or reset was touched. Evidence class on every line: **[RUNTIME]** journal/log/API/live state · **[DB]** query + result (`sqlite3 -readonly`) · **[CODE]** file:line · **[CONFIG]** resolved value · **[DOC]** report path. A [CODE]-only claim about live behavior is UNVERIFIED until a [RUNTIME]/[DB] line confirms it. Percentages carry n. P&L reads `pnl_corrected` only. Audit window: 2026-09-01 16:18–17:3x CT. Branch `docs/full-system-audit-0901` in worktree `~/vl-audit` (main tree untouched).

> **API access note:** protected GETs were read with a JWT minted by `cmd/gate-jwt` against a **`.backup` copy** of the DB (the live DB was never opened read-write; `gate-jwt` does not load `.env`, so `JWT_SECRET` was exported for the mint only). GET-only. Every request is visible as a 200 in the GIN journal.

---

## §I4 FIRST — WHAT ENDANGERS MONEY / TRADING TODAY

See the closeout for the ranked list. The items with live consequence tonight:

1. **ASIA has NO PLAN and the 16:30 read cannot author until bars resume [RUNTIME].** The class-32 wall-clock read fires every cycle since 16:31:05 CT, and the U1-3.2 planner preflight refuses each one: `🛑 planner_preflight_refused session=ASIA trade_date=2026-09-01 reason=stale_bars_1865s` (16:31:05), `…1985s` (16:33:05), `…2105s` (16:35:05), then 16:37/16:39/16:41 … — six refusals by 16:41, one per 2-minute cycle. Halt-fired authoring "from last stored bars" is ruled correct by class 32 (`trader/auto_trader_clock.go:89-97`) but `plannerPreflight` (`trader/auto_trader_feedwatch.go:109-131`) refuses whenever the newest 1m bar is older than `feedDownAfter()` = FEED_ALERT_S (600s) — which is ALWAYS true inside the 16:00–17:00 halt. Net effect: the plan cannot land before ~17:01 + planner wall time (405–581s observed today) ⇒ **ASIA opens planless for ~7–10 minutes, every day** — the class-32 symptom, shrunk from 30 min to ~8 min, now caused by a second gate. Outcome after 17:00 is quoted in §G4 (captured live).
2. **Replan budget shows 0 left on both of today's chains with zero spends [RUNTIME/DB]** — `/api/plan/today?session=LONDON` → `replans_left: 0, replan_cap: 4, version: 6`; `?session=NY` → `replans_left: 0, replan_cap: 4, version: 5`. NY's chain is `NY_scheduled_read · dormant:death · dormant:flip · rearmed · level_event` — no death re-plan, no owner re-read. The next machine death on either chain fail-closes into NO-TRADE. (Class 35 — fix in flight in `~/vl-class35`, not yet cut over.)
3. **Guardrail master is OFF** (`guardrails=master=OFF (soft-audit only)` in the boot line) while the trade-count would-trip line fired 263 times today (`max daily trades would trip (today=6, max=3)` at 15:59:06). Nothing enforces the daily cage. Owner's setting — reported, not judged.

---

## SECTION B — WHAT IS ACTUALLY RUNNING

### B1 Live rev, PID, boot, uptime, boot checklist [RUNTIME]

| item | value | evidence |
|---|---|---|
| PID | **1625428** | `ps -p 1625428`: STARTED `Tue Sep 1 00:43:25 2026`, ELAPSED 15:51:37 at 16:35 CT |
| systemd | `Active: active (running) since Tue 2026-09-01 00:43:30 CDT; 15h ago` · `NRestarts=106` · Memory 403.1M (peak 926.2M) · Tasks 21 | `systemctl status vl` / `show -p NRestarts` |
| prior death | `Sep 01 00:43:25 vl.service: Main process exited, code=killed, status=9/KILL` → `Scheduled restart job, restart counter is at 106` | journal — the class-34 `kill -9` cutover, expected |
| rev | `vcs.revision=fef656a4ee7c45860ad0237f48cef90c6b148d17 · vcs.time=2026-09-01T04:52:30Z · vcs.modified=false` | `go version -m vl-bin` |

**Boot checklist, line by line (journal 00:43:30 CT, `vl-34-build/…`):**

| required line | present? | verbatim |
|---|---|---|
| BOOT INTEGRITY OK + rev | ✅ | `🔐 BOOT INTEGRITY OK — rev fef656a4ee7c · built 2026-09-01T04:52:30Z · expected fef656a4ee7c · goldens PASS` |
| goldens PASS | ✅ | (same line) |
| session reads (class 32) | ✅ | `🗓 session reads (owner ruling 2026-08-31, open−30): ASIA 16:30 · LONDON 01:30 · NY 08:00 CT — windows/flats unchanged; Sunday weekly 16:30 → ASIA follows` |
| conditions live/shadow (0C) | ✅ | `🔬 conditions: live [acceptance, breakdown_continue, breakup_continue, hold, reclaim, reject, sweep_reclaim] · shadow [breakout_retest, fvg_entry] (process-level: defaults+env; per-trader resolved map prints at first arm cycle)` |
| validator hints (class 34) | ✅ | `🧪 validator hints: 6 sites — every condition token legal + live (class 34 guard)` |
| clock-health NTPSynchronized=yes | ✅ | `🕰 clock-health [boot] go=00:43 CT (05:43 UTC) nt8_last_bar=none drift_ms=n/a timesync{NTP=yes NTPSynchronized=yes} tolerance_ms=60000` |
| clock-guard | ✅ (with a stale timer) | `🛡 clock-guard [boot] rtc_vs_go=0s timer=stale last_check=2026-08-29T00:00:09Z (77h43m21s ago) last_status=OK rtc_vs_wsl_s=0 ntp_offset=+108.137ms warn_ms=30000 tolerance_ms=60000 resync=unavailable-no-root (timesyncd slews; owner root unit is the escalation path)` |
| far-side AddOn build_id | ✅ | `2026/09/01 00:43:30 INFO tcp_server: hello handshake OK protocol_version=3 source=vltrader-addon` → `00:44:00 INFO tcp_server: far-side AddOn build_id=2026-08-30-e7` |
| netting-orphan (class 27) | ✅ | `🪢 netting-orphan wave (class 27, 2026-08-31): netting-flat cancels brackets (C# sweep + Go desync cancel_order) · exit reconstruction from the netting fill (never exit=entry; unknown → UNRESOLVED+alarm) · dedupe 577/578 class · one-live-arm guard (opposite-side refused while open; kind=exit escapes) · split legs > capacity rejected (capacity=1 unless max_contracts_per_order raises)` |
| planner speed wave | ✅ | `🚀 planner speed wave (2026-08-31): retry=repair stream=on stream_idle=30s ttfb=on — reasoning/mode/cap unchanged until owner ruling` |
| entry law | ✅ | `🎛 entry law: bd_min_closes=1 bd_min_disp_atr=1.00 mss_min_disp_atr=0.50 accept_hold_min=10 stop_entry_offset_ticks=2 retest_wait_bars=6 stop_entry_seam=ON` · `🔐 confirm rules: 5 [1m_mss, 1x5m_close, 2x5m_close, time_hold, touch]` · `📜 scenario schema: 9 conditions [acceptance, breakdown_continue, breakout_retest, breakup_continue, fvg_entry, hold, reclaim, reject, sweep_reclaim]` |
| ledger boot | ✅ | `🧾 ledger boot: sessions[ASIA 17:00→02:00 CT (last-entry 01:45, flat 02:00) \| LONDON 02:00→08:30 CT (last-entry 08:15, flat 08:30) \| NY 08:30→14:45 CT (last-entry 14:30, flat 14:45)] · stop_until=none · cadence=interval 2m0s · position_mode=ai_watch (source: db) · watcher[min_conf=70 hold=2 warn_consec=2] · trailing=2.0×ATR14 arm=after_breakeven (source: studio) · stale_dodge=on reeval_drift=0.25×ATR14 · post_exit_rescan=on delay=2000ms · guardrails=master=OFF (soft-audit only) · roll=pending AddOn ACK · balance-alert=off` |
| volume/levels wave | ✅ | `🎛 volume wave: detectors=on · seats=8 · proximity=cfg(resolved per-trader; retuned 0.3) · family-confluence(cap=3) · zone-ladder=1.0/0.6/0.3/0.15 · roles=on(overrides=false) · bias_ctx=on · tier1+=VAH/VAL/SETT/nPOC (R-A13)` · `🎯 touch telemetry: band=16t(4.0pt) max_bars=12 vol_lookback=20 approach=5 — advisory, zero gates` · `📐 fvg_entry: on min_disp=1.5×ATR ce_width=20pt lookback=40 bars — advisory, zero gates` · `🔧 S-wave (2026-08-26): stale_confirm=2.0×ATR5m · eod_flat=session-end (NY 14:45 CT, R-A15)` |
| regime ledger | ✅ | `🛡️ regime ledger: htf_veto=ON (Studio regime.htf_veto, default ON) · htf_veto_tf=1h (env HTF_VETO_TF)` · `transition_standdown=ON … cap=45min` · `flip hysteresis hold=30min (env FLIP_MIN_HOLD_MIN)` · `structure engine TFs=[5m 15m 1h] (5m/15m/1h, swing k=2, min-swing 0.25×ATR, MSS body 1.5×ATR)` · `flip-eval freshness cap=90000ms` |
| AI params | ✅ (with WARN) | `🧠 AI params in force: model=deepseek-v4-pro client_max_tokens=32768 planner_max_tokens=65536 temperature=0.50 top_p=omitted timeout=600s retries=2 backoff=2s · truncated-responses=0` · `⚠️ AI params at UNSET defaults … [AI_TEMPERATURE AI_TOP_P AI_RETRY_BACKOFF_SECONDS AI_TASKSTATE_SUMMARY_MAX_TOKENS AI_TASKSTATE_INCREMENTAL_MAX_TOKENS AI_REPLANNER_MAX_TOKENS]` |
| half-days | ✅ | `📅 half-days [boot]: 4 loaded from half_days.json · next half-day: 2026-09-07 12:00 CT (Labor Day — equity futures halt 12:00 CT, reopen 17:00 CT)` |
| bars persistence / level_stats | ✅ | `📦 bars: persisting 2 symbol×tf retention=90d rows=19332 (backfilled 0)` · `📊 level_stats: 2026-08-30 fed 172 touch episode(s) across 63 level(s) (join: touch_episodes)` · `📊 level_stats: 2026-08-30 evaluated 12 seated level(s) (total rows 113) — forward validation accumulating` |
| E7 lineage repair | ✅ (test-seam row) | `🧩 reconcile: armed-fill lineage stamped — pos 572 ← TEST-E7:c15c48a6-… v0 TEST-E7 (fill 29346.25 …)` · `🩹 RepairArmedLineage: stamped 1 position(s) …` |
| weekly | ✅ | `📅 WEEKLY READ skip-fresh — week 2026-08-31 doc already stored (v2), idempotent.` |
| NT8 instrument cross-check | ✅ | `📐 NT8 instrument_info MNQ (MNQ 09-26): point_value=2 tick=0.25 — matches table ✓` |

**No required line is missing.** Boot-block surprises: (a) `roll=pending AddOn ACK` printed at boot but `/api/status` now reports `roll: resolved=true, resolved_contract="MNQ SEP26", contract_expiry=2026-09-18, roll_window_start=2026-09-15, roll_days_left=17` — the boot line predates the AddOn hello by design (`trader/auto_trader_pause.go:163-166`); (b) the clock-guard RTC timer has not run since 2026-08-29 (`timer=stale … 77h43m21s ago`, `resync=unavailable-no-root`) — the owner root unit was never installed; (c) two `WARN no enabled AI model found for store user store_user_id=default` lines from the VLi agent (chat assistant unbound — cosmetic).

### B2 RELEASE vs GUIDE_BUILT_REV vs live [RUNTIME]

| source | value |
|---|---|
| `deploy/RELEASE` | `fef656a4ee7c45860ad0237f48cef90c6b148d17` |
| `web/src/guide/types.ts:6 GUIDE_BUILT_REV` | `'fef656a4ee7c45860ad0237f48cef90c6b148d17'` |
| live binary `vcs.revision` | `fef656a4ee7c45860ad0237f48cef90c6b148d17` |
| `/api/health` → `revision` | `fef656a4ee7c` (HTTP 200) |

**All three equal ⇒ guide drift banner clear by construction** (the banner compares `GUIDE_BUILT_REV` with the status API `revision`). Marker commit `d03db52a deploy: class-34 marker — RELEASE=fef656a4 + GUIDE_BUILT_REV=fef656a4 (cutover pending owner GO)`; the cutover then happened at 00:43 CT.

### B3 Rollback slot [RUNTIME]
`vl-bin.prev.boot` (Aug 31 23:40, 70,812,520 B) → `vcs.revision=ebc37e01d7dd5f19c0e0f0ffa962388e12988f58 · vcs.time=2026-08-31T23:43:46Z · vcs.modified=false` = the class-32 build (booted 23:40:27 Aug 31). Rollback command (NOT run): `cp ~/vl/vl-bin.prev.boot ~/vl/vl-bin && echo ebc37e01d7dd5f19c0e0f0ffa962388e12988f58 > ~/vl/deploy/RELEASE && kill -9 1625428` — note it would also roll GUIDE_BUILT_REV out of step (guide banner would then show drift). 26 other `vl-bin.old.*` / `.prev.*` binaries sit in the tree (Aug 27–31) — untracked clutter, none referenced.

### B4 Lock [RUNTIME]
`~/vl-main.lock` = `owner=hoang pid=1861644 expiry=2026-09-01T22:18:07-0500 task=class35-replan-budget+1C-touchband acquired=2026-09-01T16:18:07-0500 note=restored-by-second-agent-16:2x-after-mistaken-stale-clear`. **`kill -0 1861644` → DEAD.** Not cleared (A2). Disclosure: this session, when it briefly started the same class-35 dispatch at 16:18, mistook this lock for stale (the PID is a short-lived shell PID, not the holder's session PID), overwrote it, then restored the original line verbatim plus the note on the owner's instruction. The class-35 agent is alive (worktree `vl-class35` exists, clean) — the lock's PID field is simply not liveness-checkable. Recommendation for that agent's report: re-stamp with its long-lived session PID.

### B5 Worktrees [RUNTIME]

| worktree | branch | HEAD | merged into dev? | ahead of dev | dirty files |
|---|---|---|---|---|---|
| `~/vl` | dev | d4b38604 | — | — | 0 |
| `~/vl-audit` (this audit) | docs/full-system-audit-0901 | d4b38604 | n/a | 0 | this report |
| `~/vl-class35` | fix/class35-replan-budget | d4b38604 → ec6632f9 by 16:56 | merged to dev at ec6632f9 (16:5x CT), **not deployed** | 0 | 0 — **locked**, in flight (lock re-acquired 16:40:57, pid 1860416) |
| `~/vl-entry` | feat/entry-mechanics | 1a6878bc | yes | 0 | **4** (`kernel/entry_law_test.go`, `kernel/plan_confirm_test.go`, `kernel/plan_doc_crosscheck_test.go`, `trader/split_entry_test.go`) — locked |
| `~/vl-news` | fix/news-hygiene | 9fa92f25 | yes | 0 | 0 — removable |
| `~/vl-sec` | fix/security-hygiene | bead12ed | yes | 0 | 0 — removable |
| `~/vl-clockhold` | fix/clock-hold | 40f5ba36 | **no** | **1**: `F1 (E7 resend loop, RECONCILED with dev): manual-cancel-wins version guard in UpsertArm + limitMarketableWrongSide strict-boundary` | 0 |
| `~/vl-weekly` | feat/weekly-bias | f9da39e1 | **no** | **2**: `fix(weekly-bias): calendar-anchored week governing Monday — Sunday-morning boot mis-mapped the week one back` · `fix(weekly-bias): InvalidatedAt stamp via kernel.FormatCT` | 0 |
| `~/vl-vf` | docs/dress-rehearsal-0830 | b54a9bfc | no | 4 (docs + `cmd/vfverify` harness) | **3** (`cmd/vfverify/d1.go`, `d2.go`, `p3.go`) |
| `~/vl-cc` | docs/confirm-cost-0830 | 8f09aa84 | no | 1 (report — its content was archived to dev by 741bfc2a) | 0 |
| `~/vl-census` | docs/knob-census | 39a0481e | no | 1 (report — archived to dev by 741bfc2a) | 0 |

**Left behind (code, not docs):** `fix/clock-hold` 40f5ba36 (E7 resend-loop F1: manual-cancel-wins guard + strict wrong-side boundary) and `feat/weekly-bias` f9da39e1/59dc7144 (calendar-anchored week + InvalidatedAt CT stamp) are **unmerged code commits** — UNVERIFIED whether their content reached dev by another path; `git branch --merged dev` says no. `vl-entry` holds 4 modified test files never committed.

### B6 git [RUNTIME]
At audit start (16:18–16:35 CT): `dev` tip `d4b38604 read-only: manual vs system segregation …` (a2b2b109, 741bfc2a below it — the archive merge); main tree `git status --porcelain` → **0 lines**; `dev...origin/dev` → `0 0`.

**dev advanced during the audit [RUNTIME 16:56 CT]:** `ec6632f9 fix(class35): replan budget is a RECORDED counter, not version−baseline` is now the `dev` tip (committed 2026-09-01 16:54:27 CT; 20 files, +868/−230: `store/strategy.go`, `trader/auto_trader_planner.go`, `trader/auto_trader_reread.go`, `api/handler_plan.go`, `main.go`, `web/src/components/plan/SessionPlanCard.tsx`, guide content ×4, `docs/superpowers/AUDIT-CHECKLIST.md` +23, tests ×8; main tree still porcelain-clean, `dev == origin/dev`). This is the class-35 fix from the `vl-class35` worktree, **merged to dev but NOT cut over** — the running binary is still `fef656a4` and every live replan-budget number in this report (§E1, §G1) is the pre-fix arithmetic. This audit's branch is based on `d4b38604`; nothing in it depends on the merge.

### B7 NT8 [RUNTIME]

| item | value |
|---|---|
| far-side build_id (current heartbeat/hello) | `far-side AddOn build_id=2026-08-30-e7` (00:44:00 CT, logged once per connection; no later reconnect in the journal) |
| C# constant | `ninjascript/VLTraderTCPClient.cs:51 private const string VL_BUILD_ID = "2026-08-30-e7";` (sent at `:2243 ["build_id"] = VL_BUILD_ID`) |
| AddOn source md5, deployed == repo | `VLTraderTCPClient.cs 95bd62f5…` · `VLBarsSubscriptionManager.cs 9210261a…` · `VLContractResolver.cs 028effbc…` — **identical in both locations** |
| deployed `.cs` mtime | `2026-08-31 14:48:30 CT` |
| compiled `NinjaTrader.Custom.dll` mtime | `2026-08-31 15:01:26 CT` (13 min after the source copy) |
| NT8 process | PID 47248, `StartTime 8/31/2026 3:02:27 PM`, uptime 25.55 h at 16:35 CT (restarted 61 s after the DLL compile) |

**Can the running DLL be distinguished from the source?** Not by the wire: `build_id` was not bumped for class 27, so the heartbeat says `2026-08-30-e7` for both the pre- and post-class-27 AddOn. Circumstantial only: source copied 14:48:30 → DLL 15:01:26 → NT8 restart 15:02:27 on Aug 31, i.e. a compile-and-restart sequence AFTER the class-27 source landed, consistent with the class-27 deploy dance — but the DLL's contents are UNVERIFIED (what would verify: a bumped `VL_BUILD_ID` echoed on the hello, or a behavior-level probe such as a netting-flat bracket sweep line).

---

## SECTION G — PLAN AND CHAIN STATE NOW (16:35–16:50 CT, no session active)

### G1 Current plans [DB]/[RUNTIME]
Query: `SELECT plan_id, version, session, trigger_reason, lifecycle, created_at FROM plans WHERE trade_date >= '2026-08-31' ORDER BY created_at` (trader `8d5c8af5_…_1781246265`).

**2026-09-01 LONDON** (window ended 08:30): v1 `planner_fail_closed`/no_trade 01:49:31 · v2 `level_event`/active 02:48:27 · v3 `dormant:flip:flip-condition: 2x5m close above 29231.63 …`/dormant 04:52:43 · v4 `level_event` 05:34:30 · v5 `level_event` 07:07:32 · **v6 `level_event`/active 07:56:33** (latest). API (`?session=LONDON`): `found:true version:6 lifecycle:active replans_left:0 replan_cap:4 latest_version:6 dark_regime_count:1 degraded:false overlay_count:0 mode:strict acceptance_rule:5m_close`. Scenarios: S1 `reject short B touch` · S2 `reject short B touch`; bias short/medium, flip `5m close above 29138.00`, day_type trend, 12 levels. `scenario_status` (system_config) `{"S1":"armed","S2":"armed"}`; `scenario_meta.confirm` S1 `touch met:true ref 29123.25` (basis heuristic), S2 `not touched since plan birth` (basis machine). Armed rows: S1 v2 **cancelled** `gate changed: min_sl` (02:57:29) · S2 v6 **filled** 29138.0 (08:37:08; `;stamp_pending`) · S3 v5 cancelled `level accepted through — marketable, never placed` (07:10:01).

**2026-09-01 NY** (window ended 14:45): v1 `NY_scheduled_read` 08:06:46 · v2 `dormant:death:… 2x5m close above 29125.00` 08:53:16 · v3 `dormant:flip:… 29177.50 (… 8× 5m closes)` 09:20:44 · v4 `rearmed:2x5m close back below 29212.50` 10:24:39 · **v5 `level_event`/active 13:00:02**. API (`?session=NY`): `version:5 lifecycle:active replans_left:0 replan_cap:4 latest_version:5 dark_regime_count:1`. Scenarios S1 `reject short B touch` · S2 `reject short B touch` · S3 `sweep_reclaim long B touch`; bias short/low, flip `5m close above 29162.00`, trend, 11 levels. `scenario_status` `{"S1":"invalidated","S2":"armed","S3":"armed"}`; confirm S1 touch met (heuristic) · S2 not touched (machine) · S3 legs touch met (heuristic). Armed rows: S1 v4 cancelled `level accepted through — marketable, never placed` (11:27:07) · S2 v5 cancelled `session ended (EOD flat)` (14:45:06) · S3 v5 **filled** 29082.75 (13:33:06; `;stamp_pending`).

**2026-09-01 ASIA:** **no plan row** (`?session=ASIA` → `found:false`) — see §I4-1. Default `/api/plan/today` → `found:false, active_session:"", runnable_sessions:[ASIA,LONDON,NY], mode:strict, acceptance_rule:5m_close`, weekly block present (`bias:neutral conviction:low draw PWL 28947.75 invalidated_at 2026-08-30 17:07 CT invalidation_basis "1h close beyond 29535.00"`).

**Observation on dormancy rows:** dormant/rearm transitions appear as **new version rows** (NY v2/v3/v4), not lifecycle updates on the same version — each therefore counts as a "spent re-plan" under the current version−baseline arithmetic. This is why NY reads 0 left with zero deaths.

### G2 replan_in_flight; last 3 planner reads [RUNTIME]
`replan_in_flight:false` on every session payload; `reading:false`. Last three reads (file log, CT):
1. **NY level-event wake, 12:21–12:52:48** — attempt 1: `📐 planner attempt 1/3 failed: stream interrupted: context deadline exceeded` (12:33:07); attempt 2 `reauthor+block: prompt ~6544 tokens` → same deadline failure (12:43:07); attempt 3 → `🧠 planner call (reasoning=max wire=enabled/max cap=65536 stream idle=30s) completed in 581.1s` → `📐 planner attempt 3/3 rejected: S2 breakdown_continue: a close came back across 29125.00 — the breakdown is void` → `🗓️ wake re-read failed for 2026-09-01 NY (benign — active plan kept)`. ~31 min, three planner calls, nothing written.
2. **NY level-event wake, 12:55:06–13:00:02** — `🧠 planner mode: fast-market (drift 58.5 pts = 2.2×ATR5m) — reasoning downgraded to fast→low` · attempt 1 `completed in 245.3s` → `rejected: S1 breakdown_continue: a close came back across 29100.50 — the breakdown is void` · `🧩 planner attempt 2/3 repair: prompt ~1016 tokens (full-author ~6495 tokens)` → `completed in 50.1s` → `🗓️ PLAN written 2026-09-01 NY v5 (model deepseek-v4-pro, lifecycle active, prompt f7633096e790 …)`.
3. **ASIA scheduled read, 16:31:05 → ongoing** — `🗓 session read fired during halt (ASIA) — authoring from last stored bars (newest 5m 2026-09-01 15:55 CT, age 36m)` immediately followed by `🛑 planner_preflight_refused … reason=stale_bars_1865s`; repeated at 16:33/16:35/16:37/16:39/16:41 (+120 s each). No LLM call made, no row written, no budget consumed (by design of the preflight).

Earlier today for context: NY scheduled read 08:00 → `completed in 405.8s` → `PLAN written 2026-09-01 NY v1` at 08:06:46.

### G3 traders.is_running; last executor cycles [DB]/[RUNTIME]
`SELECT id,name,is_running FROM traders` → `8d5c8af5_…_1781246265 | hoang | 1`. `/api/status` → `is_running:true, call_count:237, runtime_minutes:508, start_time 2026-09-01T08:13:08-05:00, last_reset_time 08:13:08`. **The trader loop restarted at 08:13:08 CT inside the process** — cause: a Studio save `PUT /api/strategies/a5b7662e-7bf7-49bb-9f09-7efa48f95ac8` (GIN 200, 2.34 s) → `api/strategy.go:412 🔄 Strategy … saved — removing trader … from memory` → `⏹ Stop signal received` → reload → `🔄 Auto-starting trader`. Positions/brackets unaffected (next fill 08:36:47 landed normally). Last executor cycles: `⏭ cycle_skip=no_new_data — newest 5m bar unchanged` at 16:29:05, 16:31:05, 16:33:05, 16:35:05 (the halt; wall-clock reads still evaluated first — class 32 working as designed).

### G4 Next scheduled read; what can wake before it [CODE]/[RUNTIME]
The ASIA read window is `[16:30, 02:00)` (`trader/auto_trader_clock.go:263-274 inSessionReadWindow`, wraps midnight) and `maybeRunSessionReadsAt` (`trader/auto_trader_planner.go:190-…`) retries every tick while `existing == nil`, so the refused read **keeps retrying every 2 min until the preflight passes** (newest 1m bar age ≤ 600 s), i.e. ~17:01 when bars resume. Then LONDON 01:30 CT Sep 2, NY 08:00. Before an ASIA plan exists nothing else can wake the planner (level/MSS/fast-market wakes take `existing` as input); an owner re-read (`CanForceReread` with `row==nil` → allowed) would hit the same preflight. **Live outcome after 17:00 — see the addendum at the end of this section (captured at 17:0x).**

### G5 Today's ledger on `pnl_corrected` vs NT8 equity [DB]/[RUNTIME]
Session-day since 2026-08-31 17:00 CT (`exit_time >= 1788213600000`). Query: `SELECT id, side, quantity, entry_price, exit_price, realized_pnl, pnl_corrected, close_reason, source FROM trader_positions WHERE entry_time >= 1788213600000 OR exit_time >= 1788213600000 ORDER BY id`:

| id | side | entry → exit | pnl_corrected | close_reason | source | entry / exit (CT) |
|---|---|---|---|---|---|---|
| 581 | SHORT 1 | 29466.00 → 29370.75 | **+190.50** | sync | system | 02:52:44 / 03:06:33 |
| 582 | SHORT 1 | 29246.75 → 29182.00 | **+129.50** | sync | armed_entry | 05:23:28 / 06:05:34 |
| 583 | LONG 1 | 29213.25 → 29172.00 | **−82.50** | sync | system | 06:35:04 / 06:56:47 |
| 584 | SHORT 1 | 29138.00 → 29085.00 | **+106.00** | sync | reconcile | 08:36:47 / 08:38:19 |
| 585 | SHORT 1 | 29182.00 → 29220.75 | **−77.50** | sync | armed_entry | 09:31:08 / 09:40:40 |
| 586 | LONG 1 | 29082.75 → 29055.75 | **−54.00** | sync | reconcile | 13:33:06 / 13:41:14 |

n=6 closed · `pnl_corrected IS NULL` = **0** · excluded by close_reason (`reconcile_flat`/`unresolved`/`e7_farside_test`) = **0** · **Σ pnl_corrected = +212.00** (every row's `pnl_corrected` equals its `realized_pnl`; all six are `sync` closes). Open rows now: **0** (`/api/positions` → `[]`, `/api/open-orders?symbol=MNQ` → `[]`).

NT8 equity: `📊 Account equity: 52216.00` at 2026-08-31 17:00:04 and 17:02:04 (session-day start) → `52428.00` at 15:59:06 Sep 1 (last frame before the halt; `/api/account total_equity: 52428`). **Δ = +212.00 = ledger Σ — agrees to the cent** (no commission on Sim101). Row ids: 581–586.

**Live-surface caveat [RUNTIME]/[CODE]:** `/api/account` returns `daily_pnl: 0, total_pnl: 0, total_pnl_pct: 0` — `total_pnl` is NT8's `realized+unrealized` from the account frame (`trader/ninjatrader/tcp_trader.go:794-799 brokerNativePnL`, `trader/auto_trader_decision.go:197-201`), which is 0 while flat; `daily_pnl` is a **permanently-zero display field** (only writer `trader/auto_trader_loop.go:383 at.dailyPnL = 0`; the comment at :378-381 says so). The dashboard header cards render `account.total_pnl` (`web/src/pages/TraderDashboardPage.tsx:662,713`) → shows 0.00 while the PositionHistory day total (`computeDayTotal`, `PositionHistory.tsx:132-145`, pnl_corrected-only, A-2 rule) shows +212.00. Two P&L numbers on one screen — see §H.

### G6 Panics / ERROR / restarts since boot [RUNTIME]
Process restarts since 00:43:30: **0** (systemd `NRestarts=106` lifetime; one in-process trader reload at 08:13:08, §G3). Panics: **0** (`grep -c "panic\|goroutine .* \[running\]"` = 0 in the file log). `[ERRO]` lines in `data/vl_2026-09-01.log`: **20**, by class:

| n | class |
|---|---|
| 7 + 1 | `api/server.go:833 [Auth] Invalid token: token signature is invalid` (this audit's first mint attempts, 16:38–16:40, before `JWT_SECRET` was exported) + 1 `token has invalid claims: token is expired` (an expired browser session earlier today) |
| 6 | `🛑 planner_preflight_refused session=ASIA … stale_bars_NNNNs` (16:31–16:41, §I4-1) |
| 4 | `🚨 CLOCK EARLY-WARNING [session-roll:…]: \|drift\| 30097 / 51423 / 51675 / 53493 ms exceeds CLOCK_WARN_MS 30000ms (tolerance 60000ms not yet breached) — fix WSL2 time-sync NOW` at 02:01:29, 08:13:08, 08:31:08, 14:45:06 |
| 1 | `🚨 PLANNER FAIL-CLOSED 2026-08-31 ASIA: scenario[4].confirm2.rule "t…` (01:01:09, the ASIA v2 no_trade) |
| 1 | `🚨 PLANNER FAIL-CLOSED LONDON: S3 breakdown_continue: a c…` (01:49:31, LONDON v1 no_trade) |

**The four clock early-warnings are a measurement artifact, not clock skew [CODE]:** `kernel/clock_health.go:85-89` computes `nt8Ms = last.OpenTime + 60_000 // close of the freshest 1m bar` and `drift = now − nt8Ms` — the freshest 1m bar is the FORMING bar, so its "close" is up to 60 s in the future; the checks ran at :08 past the minute (08:13:08 → −51423 ms, 08:31:08 → −51675, 14:45:06 → −53493) and at 02:01:29 (−30097). `timesync{NTP=yes NTPSynchronized=yes}` and `ntp_offset=+108.137ms` on every line. Consequence: `ClockHoldDecision` (`kernel/clock_drift.go:166-178`) sets `widenMs = |drift|` whenever ≥ 30 s, so the T1 news windows are widened by 30–54 s on roughly half of all reads for no reason; authoring deferral needs `|drift| > 60000` with negative sign, which this artifact cannot reach (bounded < 60 s) unless the formula changes. Log-only otherwise. Reported, not fixed.

Also since boot: **348 × `GET "/wyvrn/Synapse"` → 404** from 127.0.0.1 every ~2.7 min (first seen in the journal 2026-08-27 13:40:33) — an unknown local client probing the bot's loopback API port; not in this repo (`grep -rn wyvrn` → nothing). Harmless (404, loopback), unexplained.

*(§G4 addendum — the 17:00 reopen outcome — is appended after live capture.)*

---

## SECTION C — EVERY WAVE THIS WEEK, VERIFIED LIVE

*(Investigated by a read-only sub-agent; every [RUNTIME]/[DB]/[CODE] line below was produced from the running system during this audit. Audit-lead cross-checks are marked ⟂.)*

Baseline [RUNTIME]: `~/vl` on `dev` @ `d4b38604`, porcelain clean, `dev == origin/dev`. Process PID 1625428 started `Tue Sep 1 00:43:25 2026`; C-section ran 16:32–16:45 CT. The journal is INFO-suppressed for many trader lines; `data/vl_2026-09-01.log` (23,111+ lines) is the primary runtime source. Boot-ms epoch used in queries: `1788241380000` (= 2026-09-01 00:43 CT).

### C1 — Class 27 netting-orphan — VERIFIED (code + deploy + 1 of 6 arms live-fired); 5 arms UNVERIFIABLE-NOW (no triggering event since boot)

- [RUNTIME] boot `kernel/levels_volume_boot.go:26 🪢 netting-orphan wave (class 27, 2026-08-31): netting-flat cancels brackets (C# sweep + Go desync cancel_order) · exit reconstruction from the netting fill (never exit=entry; unknown → UNRESOLVED+alarm) · dedupe 577/578 class · one-live-arm guard (opposite-side refused while open; kind=exit escapes) · split legs > capacity rejected (capacity=1 unless max_contracts_per_order raises)`.
- **C# sweep** [CODE] `ninjascript/VLTraderTCPClient.cs:1910-1943 OnPositionUpdate` → `if (e != null && e.MarketPosition == MarketPosition.Flat) { … CancelAllBracketsFor(flatRoot, acc != null ? acc.Name : ""); }`; `:1946-1994 CancelAllBracketsFor` sweeps `placedBrackets` by (root, account), `ba.Cancel(toCancel)`, logs `netting-flat bracket sweep cancelled N leg(s)`. `if (victims.Count == 0) return;` precedes the log — a flat with no tracked brackets is silent.
- **Deployed copy** [CONFIG] repo ↔ AddOns folder `VLTraderTCPClient.cs` identical (130,696 B; deployed mtime Aug 31 14:48); `NinjaTrader.Custom.dll` Aug 31 15:01; NT8 trace `trace.20260831.00001.txt`: `Session Start (Version 8.1.8.1) 2026-08-31 15:02:40:300` → NT8 restarted after the compile.
- **C# sweep runtime** [RUNTIME] NT8 `log.20260901.00000.txt`: zero lines matching `netting-flat|bracket sweep|CancelAllBrackets`. Both real closes today were OCO SL/TP exits: `06:05:34:279 … '-tp' New state='Filled'` → `'-sl' Cancel submitted` → `Market position=Flat`; `13:41:14:189 … '-sl' Filled` → `'-tp' Cancel submitted` → Flat. No netting flat occurred. **UNVERIFIABLE-NOW.**
- **Go desync cancel** [CODE] `trader/position_desync.go:45-95 skipGateDesync` → `nt.CancelOrder(row.EntryOrderID)` → `🧹 class-27 desync: cancel_order sent …`. [RUNTIME] 0 lines since boot. **UNVERIFIABLE-NOW.**
- **Exit reconstruction** [CODE] `trader/ninjatrader/netting_fills.go:37-56 recordRecentFill` (ring 32), `:63-95 takeNettingExit` (latest opposite-side fill within `[firstFlatMs−25_000, nowMs]`, else UNRESOLVED). [RUNTIME] 0 `unresolved|netting` lines since boot; [DB] 6 closes since boot all `sync`, all `pnl_corrected` non-NULL. **UNVERIFIABLE-NOW.**
- **Dedupe** [CODE] `trader/armed_executor.go:1024-1045 materializeArmedEntry` checks `GetOpenPositionBySymbol(at.id, sym, side)` AND `strings.ToLower(side)`. [RUNTIME] the race happened once: `05:23:28 reconcile.go:395 🧩 reconcile: NT8 holds UNTRACKED position MNQ SHORT @ avg 29246.75` and `05:23:28 … 🧩 armed fill S2 @ 29246.75 materialized OPEN (source=armed_entry …)` → [DB] single row id 582; rows opened since boot = 6 distinct ids (581–586), no duplicate `entry_order_id`, 0 OPEN. **VERIFIED [B]** (one race, one row).
- **One-live-arm guard** [CODE] `armed_executor.go:487-509 oneLiveArmGuard`. [RUNTIME] fired 3×: `05:35:28 ⚔️ arm REFUSED LONDON S2 leg 1: one_live_arm_guard: long arm S2 would net the open SHORT MNQ position (class 27) — opposite-side entry refused while a position is open` · `06:35:28 … LONDON S3 … short arm S3 would net the open LONG …` · `13:33:06 … NY S2 … short arm S2 would net the open LONG …`. **VERIFIED live.**
- **Leg capacity** [CODE] `armed_executor.go:516-531 armLegCapacity` → `RiskControl.MaxContractsPerOrder` else 1; refusal `:265-268 split_leg_capacity`. [CONFIG] bound strategy JSON carries `"max_contracts_per_order":2` → resolved capacity **2**, not the boot line's "capacity=1". [RUNTIME] 0 `split_leg_capacity` refusals; NY v5 S3 `sweep_reclaim` split arm authored without refusal. **UNVERIFIABLE-NOW** as a refusal.

### C2 — Ledger honesty 0A/0A-2 — VERIFIED for the 3 named predicates + close-path stamp + backfill gate; FAILED at two aggregators; NULL-coercion at five more

Exclusion set [CODE] `store/position.go:113-125`: `CloseReasonTestSeam = "e7_farside_test"`; `UnknownPnLReason = reconcile_flat || unresolved || e7_farside_test`. `EffectivePnL()` `:826-831` = `pnl_corrected` if non-nil **else `realized_pnl`** (coercion).

| Site | Predicate (quoted) | P&L column |
|---|---|---|
| `store/position_query.go:47 GetPositionStats` | `trader_id = ? AND status = ? AND pnl_corrected IS NOT NULL AND close_reason NOT IN (?, ?, ?)` | `COALESCE(pnl_corrected, realized_pnl)` under NOT NULL → pnl_corrected |
| `:85 CountConsecutiveLossesSince` | `… close_reason NOT IN (?, ?, ?) AND pnl_corrected IS NOT NULL AND exit_time >= ?` | `EffectivePnL()` (NULL pre-filtered) |
| `:120 GetSessionDayActivity` | `… close_reason NOT IN (?, ?, ?) AND pnl_corrected IS NOT NULL AND exit_time >= ?` | `COALESCE(...)` under NOT NULL |
| `:157/:169 GetFullStats` | `trader_id = ? AND status = ? AND close_reason NOT IN (?, ?, ?)` — **no `pnl_corrected IS NOT NULL`** | `EffectivePnL()` → raw on NULL rows |
| `:347 GetSymbolStats` · `:488 GetDirectionStats` | reason-only | `EffectivePnL()` coerced |
| `:417 GetHoldingTimeStats` | `trader_id = ? AND status = ? AND exit_time > 0` — **no exclusion** | `EffectivePnL()` coerced |
| `store/position_history.go:101` recent-20 · `:123 calculateStreaks` | reason-only | `EffectivePnL()` coerced |
| `store/position_query.go:238 GetRecentTrades` | `trader_id = ? AND status = ?` — no exclusion | `EffectivePnL()` coerced |
| `agent/tools.go:3359 toolGetTradeHistory` | over `GetClosedPositions` (`store/position.go:687`, no exclusion) | **`pnl := pos.RealizedPnL` raw** |
| `api/handler_plan.go:1674` `/api/plan/trades` | plan-join payload | `"realized_pnl": p.RealizedPnL` raw per row |

Consumers [CODE]: `trader/auto_trader_loop.go:1187 ctx.DailyRealizedPnL ← GetSessionDayActivity` (strict ✓); `:1234 ctx.TotalRealizedPnL = stats.TotalPnL ← GetFullStats` (coerced); `:1194 GetRecentTrades(at.id, 10, …)` feeds the prompt; `api/handler_order.go:269 GetRecentTrades`.

[DB] predicates evaluated for the active trader, Sim101 ⟂(audit-lead re-ran with the exact trader id): strict (`GetPositionStats` rule) **n=105, Σ +304.32**; `GetFullStats` rule **n=220, Σ −203.68**; the 115 NULL rows coerced to raw contribute **−508.00**. Trader-global: strict n=121 −242.68 vs GetFullStats n=236 −750.68. ⟂ **The live executor prompt carries the coerced number:** `decision_records` id 35906 (15:59:43 CT) contains `Total Trades: 220` · `Total PnL: -203.68 USDT` · a "Recent Completed Trades" row `Entry 29459.0000 Exit 0.0000 | Profit: +0.00 USDT (+100.00%)` (= row 579, `unresolved`). This is a corrected-column-law (A4) violation on the surface the model reads.

Whole table [DB]: 582 CLOSED; `pnl_corrected NULL = 357` (UNRESOLVED, excluded), 225 resolved Σ −3073.27; by reason `sync 513 (313 NULL, −3078.27)`, `reconcile_flat 62 (39 NULL, 0.0)`, `unresolved 4 (4 NULL)`, `e7_farside_test 3 (1 NULL, +5.0)`. Aggregator-eligible across all traders: n=200, Σ −3078.27, wins 65 (32.5%, n=200).

- **Backfill** [CODE] `main.go:117-119 st.BackfillPnlCorrectedAll()`, early-return on `pnl_backfill_all_2026_08_27_done`. [DB] `pnl_correction_2026_08_20_done|1`, `pnl_backfill_all_2026_08_27_done|1`. VERIFIED (gate).
- **Close-path stamp** [CODE] `trader/ninjatrader/close_sync.go:188-192 StampPnlCorrectedOnClose(owner.ID, realizedPnL, realizedPnL)`. [DB] today's 6 closes n=6, NULL=0, Σ +212.00 (§G5). VERIFIED. [B] the crypto path `store/position_history.go CreateFromClosedPnL/SyncClosedPositions` never writes `pnl_corrected` → those rows stay NULL and are excluded (honest by exclusion).
- **Test-seam exclusion at every aggregator:** VERIFIED at 8 sites; **FAILED** at `GetHoldingTimeStats` (latent — only reachable via uncalled `GetHistorySummary`) and `agent/tools.go toolGetTradeHistory` (live AgentBeta tool, raw `realized_pnl`, no exclusion).

### C3 — Open−30 read times — VERIFIED

- [DB] `SELECT value FROM system_config WHERE key='session_registry'` → ASIA `read_ct 16:30` (window 17:00→02:00, flat 02:00, `enabled:false`) · LONDON `01:30` (02:00→08:30, `enabled:false`) · NY `08:00` (08:30→14:45, `enabled:true`) + `half_days {2026-09-06:12:00, 2026-11-25:12:00, 2026-11-26:12:15, 2026-12-23:12:15}`.
- [CODE] `kernel/session_registry.go:90/99/108` `ReadCT: "16:30" // owner ruling 2026-08-31: open−30 (was 16:55)` / `"01:30"` / `"08:00"`; loader `trader/auto_trader_registry.go:19-41`.
- [CONFIG] registry `enabled:false` for ASIA/LONDON is overridden per trader: strategy `sessions:[{NY},{ASIA enable:true},{LONDON enable:true}]` → `sessionRunnable` resolves all three runnable (a registry-only reader would conclude those sessions are off).
- [RUNTIME] boot line quoted in §B1; LONDON planner call `01:31:30 📡 … Request URL (stream idle=30s)`; NY `08:00:01`; ASIA fired `16:31:05` (§C9).

### C4 — Weekly render — VERIFIED: neutral+date on planner line, executor line, chip; zero "none"; zero strikethrough

- [DB] `plans` `2026-08-31:WEEKLY:…` v2 active `bias=neutral, conviction=low, invalidated_at="2026-08-30 17:07 CT"` (v1 `bear`, `invalidated_at` NULL).
- [CODE] `kernel/weekly_prompt.go:303-316 WeeklyContextLine` → `"WEEKLY: none"` only when `d == nil`; `InvalidatedAt != ""` → `"WEEKLY: neutral (invalidated <ts>)"`; `:321-331 WeeklyExecutorLine` same. Callers `trader/auto_trader_planner.go:1992`, `kernel/engine_analysis.go:445`.
- **Planner line** [DB] `planner_rejected_prompts` ids 72/73/74 contain `WEEKLY: neutral (invalidated 2026-08-30 17:07 CT)`; 20 rows, 15 carry `WEEKLY:`, **0** carry `WEEKLY: none` (the 5 without are short repair prompts).
- **Executor line** [DB] `decision_records` 35905/35906 `system_prompt` contains `WEEKLY: neutral (invalidated 2026-08-30 17:07 CT)`.
- **Chip** [CODE] `web/src/components/plan/WeeklyChip.tsx:40-43` `invalidated ? 'neutral' : rawBias`; `:73-74 // B4 … NO strikethrough, NO opacity drop`; no `textDecoration`. Served by the vite dev server (PID 329, `127.0.0.1:3000`, up since Aug 21) → the source is what renders. `GUIDE_BUILT_REV` = running rev.

### C5 — Repair retry — VERIFIED

- [CONFIG] `RETRY_MODE` absent from `.env`/environ → `kernel/planner_speed.go:12-19 ResolvePlannerRetryMode` default `"repair"`. [RUNTIME] boot `🚀 planner speed wave (2026-08-31): retry=repair stream=on stream_idle=30s ttfb=on`.
- [RUNTIME] repair prompt sizes today: `00:51:52 🧩 planner attempt 2/3 repair: prompt ~1251 tokens (full-author ~6263 tokens)` (20.0%) · `02:47:32 ~1024/~6559` (15.6%) · `03:50:12 ~872/~6529` (13.4%) · `08:21:12 ~1553/~6530` (23.8%) · `12:59:12 ~1016/~6495` (15.6%). Over all 22 repair prompts today: **mean 17.5%, min 13.4%, max 23.8% (n=22)**.
- [RUNTIME] outcomes: 22 repair calls; **13 `🧩 repair returned unparseable output — falling back to a full re-author next attempt`** (59%, n=22); 1 `🎯 repair regression` (12:43:07). 12 `PLAN written` today: 7 after a repair-size call (LONDON v2/v4/v6, NY v2/v3/v4/v5), 5 after a full-author call (ASIA v2, LONDON v1/v3/v5, NY v1).

### C6 — Streaming + split deadlines — VERIFIED

- [CODE] `mcp/client.go:954 CallWithRequestStreamIdle` (SSE; idle timer `:983-1000`; `:970` logs `📡 … Request URL (stream idle=%ds)`); planner `trader/auto_trader_planner.go:951-953 CallWithRequestStreamRetry(req, nil, plannerStreamIdle())`; `kernel/planner_speed.go:25-32` env `AI_PLAN_STREAM_IDLE_SECS` default 30. Whole-request ceiling `trader/auto_trader_loop.go:171 SetTimeout(mcp.ResolvedAITimeout())`; `mcp/config.go:89-95 AI_HTTP_TIMEOUT_SECONDS → AI_TIMEOUT_SECONDS → 300`.
- [CONFIG] idle **30s** (unset → default); `.env:32 AI_HTTP_TIMEOUT_SECONDS=600` → **600s**.
- [RUNTIME] stream path used: `00:44:40 📡 [MCP …] Request URL (stream idle=30s): https://api.deepseek.com/chat/completions`; ceiling proven `12:33:07 [WARN] mcp/client.go:237 ai_call … duration_ms=600000 finish_reason=n/a ok=false retries=1 ttfb_ms=474 reasoning_chars=73196 timeout_source=client deadline_s=600 err="stream interrupted: context deadline exceeded …"` (again 12:43:07, reasoning_chars=71414); transport retry `01:46:33 … timeout_source=transport … connection reset by peer` → `01:46:35 ⚠️ AI API stream failed, retrying (2/2)` → `01:49:31 ✓ AI API stream retry succeeded`.

### C7 — Planner telemetry — VERIFIED (one label caveat)

- [RUNTIME] `12:52:48 📊 AI call complete (stream): completion=28408 prompt=9771 finish_reason=stop reasoning_chars=92053 ttfb_ms=506 wall_ms=581051` · `12:52:48 ai_call model=deepseek-v4-pro duration_ms=581051 finish_reason=stop ok=true retries=1 ttfb_ms=506 reasoning_chars=92053` · `12:59:12 … completion=15085 prompt=9654 … reasoning_chars=48546 ttfb_ms=498 wall_ms=245294` · `13:00:02 … completion=3456 prompt=1409 … reasoning_chars=9525 ttfb_ms=548 wall_ms=50086`.
- T1/T2 [RUNTIME] `12:55:07 📝 prompt render (T2): 0ms ~6495 tokens` · `🗺️ map assembly (T1): 0ms` (`auto_trader_planner.go:884,:915`).
- Rejected-prompt store [DB] `planner_rejected_prompts` → `20 | 08:21 CT … 12:59:12 CT`; writer `auto_trader_planner.go:1232 SaveRejectedPrompt`.
- Caveat [CODE] `mcp/client.go:1066 logAICall(start, err, attempt)` → **`retries=` is the attempt number** (`retries=1` = first attempt, zero retries). Executor (non-stream) calls log `ttfb_ms=0` — TTFB is only measured on the stream path.

### C8 — 0C shadow map — VERIFIED (resolution); refusal path UNVERIFIABLE-NOW; counter not surfaced

- [CODE] `kernel/condition_status.go:99-104 KnownConditions` (9); defaults `:26-29` shadow `fvg_entry`, `breakout_retest`; resolver `:35-60` session > base > env(`SHADOW_CONDITIONS`) > defaults. Enforcement `trader/armed_executor.go:30, :272-323`, `:296 telemetry.IncShadowedArmRefusal()`, `:299 "⚔️ arm REFUSED %s %s: condition_shadowed (…)"`.
- [CONFIG] no `condition_status` in the strategy JSON, `SHADOW_CONDITIONS` absent → **7 live / 2 shadow**. [RUNTIME] boot line (§B1) + per-trader `02:49:29 🔬 conditions: live [acceptance, breakdown_continue, breakup_continue, hold, reclaim, reject, sweep_reclaim] · shadow [breakout_retest, fvg_entry] (per-trader resolved, 0C shadow demotion)`.
- Counter [CODE] `telemetry/shadow_conditions.go` `ShadowedArmRefusalCount` has **no callers** (no API, no boot line) → not observable. By `grep -c condition_shadowed` = 0 since boot it must be 0. ⟂ `/api/risk/gate-blocks` (§E6) lists no shadow reason today either.
- Since boot: today's plans carry only `reject` / `sweep_reclaim` (the literal `fvg_entry` in docs is narrative: `"No fvg_entry: FRESH FVG list is empty"`); `armed_orders` since boot `cancelled 2, filled 2`, no `state='shadowed'`; `ab_confirm_log` 82 rows, `is_counterfactual=1` → **0**. **No shadowed scenario was authored since boot → no refusal line, no E8 counterfactual row. Said so.** 08-31 plans did carry `breakout_retest` (LONDON v2 S3, NY v5 S2, NY v6 S1) but the 08-31 log has 0 `condition_shadowed` lines — UNVERIFIED whether they predate the 0C binary or never reached the arm seam.

### C9 — Class 32 wall-clock reads — read FIRED (VERIFIED) but the planner call was REFUSED every tick by the 3.2 preflight → FAILED on intent

- [CODE] `trader/auto_trader_clock.go:98-113 evaluateWallClockSessionReads` → `maybeRunSessionReadsAt(now)`; halt line `:129-133`; invoked at `:837` at the top of `tickOnce` before the bar-close gate / no-new-data dedup.
- [RUNTIME] `16:31:05 [INFO] 🗓 session read fired during halt (ASIA) — authoring from last stored bars (newest 5m 2026-09-01 15:55 CT, age 36m)` then, same second: `16:31:05 [ERRO] 🛑 planner_preflight_refused session=ASIA trade_date=2026-09-01 reason=stale_bars_1865s — refusing to call the LLM with no market data (the 0-scenario fail-closed stub class). The read window will retry next cycle.` Repeats 16:33 (1985s), 16:35 (2105s), 16:37, 16:39 (2345s), 16:41 (2465s), 16:43, 16:45, 16:47 (2825s) — every 2-minute tick. [DB] no `2026-09-01:ASIA` plan row; `day_plan_alerts` id 556 `P1 planner-preflight preflight:2026-09-01:ASIA`.
- Why [CODE] `trader/auto_trader_feedwatch.go:109-131 plannerPreflight`: `age, haveBars := at.feedNewestBarAge(now); if haveBars && age <= feedDownAfter() { return true }` else refuse; `:22-24 feedDownAfter = LoadFeedPolicy().FlatAlertMs`; `kernel/feed_policy.go:27 DefaultFeedAlertSeconds = 600`. [CONFIG] `FEED_ALERT_S` unset → **600s**. Bars stop at 16:00 during the halt, so at 16:30 the newest 1m bar is always ≥1800s old > 600s: **the 16:30 halt read is structurally unreachable**. The plan can only land after the 17:00 reopen bar plus a 50–600s planner call — the exact "no plan at the open" outcome class 32 was written to remove, now produced by a different gate. The live outcome after 17:00 is quoted in §G4-addendum.

### C10 — Class 34 validator hints — VERIFIED (boot guard, 6 sites, reject-block suffix, live reject with hint); SURPRISE: the suffix is absent from repair prompts (the default retry path)

- [RUNTIME] boot `🧪 validator hints: 6 sites — every condition token legal + live (class 34 guard)` (`kernel/levels_volume_boot.go:34-38`, `kernel/validator_hints.go:64-83 ValidateValidatorHints`).
- [CODE] registry `kernel/validator_hints.go:50-60`: (1) `breakdown_continue.go reclaimed` → "author a `reject` play instead (do NOT combine condition names; `reject_retest` is not a valid condition)"; (2) `breakdown_continue.go displacement` → "author a normal `reject` play instead (…)"; (3) `plan_doc.go arm-legs contract` → "(the split entry is the sweep_reclaim contract; other conditions arm single)"; (4) `planner_repair.go breakdown law` → "BREAKDOWN-CONTINUE LAW: … author a `reject` play instead of breakdown_continue (…)"; (5) `planner_repair.go arm-split law` → "ARM-SPLIT LAW: … Only sweep_reclaim conditions arm split; …"; (6) `planner_repair.go entry-law confirm` → "ENTRY-LAW CONFIRM LAW: breakdown_continue takes 1 confirming close + displacement >= BD_MIN_DISP_ATR x ATR5m OR stop-entry (E7); …".
- Live-condition suffix [CODE] `validator_hints.go:86-104 LiveConditionsLine` → `"\nValid conditions: [%s] (use exactly ONE token from this list; do NOT combine condition names)."`; caller `trader/auto_trader_planner.go:1215 plannerRejectBlock` (re-author tail). [DB] `planner_rejected_prompts` id 73 (attempt 3, 12:52:48) contains `Valid conditions: [acceptance, breakdown_continue, breakup_continue, hold, reclaim, reject, sweep_reclaim] (use exactly ONE token …)`; rows 64/67/70/72/73 (full re-author prompts, ≈25.8k chars) carry it; **rows 63/66/69 (attempt-2 repair prompts, 4.3–4.7k chars) do not** — `kernel/planner_repair.go BuildPlannerRepairPrompt/lawExcerptsFor` never call `LiveConditionsLine`.
- Live rejects carrying the new hint since boot [RUNTIME]: `00:51:52 📐 planner attempt 1/3 rejected: S3 breakdown_continue: a close came back across 29502.25 — the breakdown is void; author a `reject` play instead (do NOT combine condition names; `reject_retest` is not a valid condition)`; `12:52:48` attempt 3 S2 @29125.00; `12:59:12` attempt 1 S1 @29100.50 (quoted in full in the closeout).

### C11 — Research archive merge — VERIFIED (INDEX path differs from the dispatch's expectation)

- [RUNTIME] `git log --oneline -5` → `d4b38604 read-only: manual vs system segregation …` · `a2b2b109 docs: research archive merge report …` · `741bfc2a docs: archive 38 stranded research reports to dev + RESEARCH INDEX …` · `a552d2c5 research inventory 2026-09-01 …` · `b8f68db1 EOD verification 2026-08-31 …`.
- `git diff --stat a2b2b109^..a2b2b109 -- . ':!docs'` → empty; same for `741bfc2a`. `git show --name-only 741bfc2a` → 39 files (+9,246): **38 under `docs/superpowers/reports/` + `docs/superpowers/research/INDEX.md`** (`# RESEARCH INDEX — everything commissioned 2026-08-17 → 2026-09-01`). `docs/superpowers/reports/INDEX.md` does not exist — the index lives in `research/`.

### C — Surprises
1. C9: the 16:30 ASIA halt read can never produce a plan (preflight vs halt-fire contradiction) — see §I4-1.
2. C2: "test-seam exclusion at every aggregator" fails at `GetHoldingTimeStats` (latent) and `agent/tools.go:3359 toolGetTradeHistory` (live AgentBeta tool, raw `realized_pnl`).
3. C2: two ledger surfaces disagree by $508 for the live trader (strict vs `GetFullStats`); the coerced figure is in the live executor prompt (`Total PnL: -203.68 USDT`, 220 trades) while the strict corrected set is +304.32 over 105.
4. C10: the `Valid conditions: […]` vocabulary reaches only re-author retries, not repair retries — and repair is the default.
5. C5: 13 of 22 repair calls today (59%) returned unparseable output.
6. C6: two consecutive planner calls hit the 600s ceiling (12:33:07, 12:43:07; reasoning_chars 73k/71k, completion=0).
7. C8: `arms_refused_shadowed` is write-only.
8. C7: `retries=N` is the attempt number.
9. C1: resolved leg capacity is 2, not the boot line's "capacity=1".
10. In-process trader reload at 08:13:08 CT (Studio save during NY) — §G3.
11. C3: registry `enabled:false` for ASIA/LONDON; reads happen via per-session strategy overrides.
12. C11: the INDEX is `docs/superpowers/research/INDEX.md`.

### C — UNVERIFIED (what would verify)
- C1 C# sweep / Go desync / exit reconstruction / split-leg refusal live — need a netting flat, a store-open-vs-broker-flat desync, a netting close, a >capacity split arm respectively.
- C1 leg-capacity JSON path — `json_extract(config,'$.risk_control.max_contracts_per_order')` returned empty although the literal is in the config; verify with `json_tree`.
- C8 refusal line + E8 counterfactual row — need a plan authoring `fvg_entry`/`breakout_retest` under the 0C binary.
- C4 chip in a browser — source + API + DB verified; a Playwright snapshot would close it.
- C9 post-17:00 behavior — §G4-addendum.
- C2 "hidden rows excluded from the list" claim in the 0A-2 report — `GetRecentTrades` applies no exclusion; the handler the report meant was not traced.

---

## SECTION D — RESEARCH ALIGNMENT: EVERY VERDICT vs LIVE STATE

*(Two read-only sub-agents, D1–D13 and D14–D26. Resolved values follow base → session override → env → default and are quoted from the resolver + the boot block. ⟂ = audit-lead cross-check / correction.)*

Context both agents established: bound strategy `a5b7662e-7bf7-49bb-9f09-7efa48f95ac8` ("MNQ", `updated_at 2026-09-01 13:13:06Z` = 08:13 CT — the Studio save that hot-reloaded the trader, §G3), so DB JSON == in-memory config. `.env` knob names present: `HTF_VETO_MODE` only (none of MIN_SL_ATR_MULT / ARM_MIN_RR / BD_MIN_CLOSES / FLIP_ATR_BUFFER / STALE_REEVAL_DRIFT_ATR / FAST_MARKET_ATR / TOUCH_BAND_TICKS / WEEKLY_* / RETRY_MODE are set).

### D — Master table

| Row | Research verdict | Live resolved value / state | Wave | Status |
|---|---|---|---|---|
| D1 Entry confirmation | no 15m; fades touch-only; BD_MIN_CLOSES=1 + displacement; 5-rule vocab | 5 rules `[1m_mss, 1x5m_close, 2x5m_close, time_hold, touch]`; 15m rejected at write (legacy eval mapping kept for stored docs); reject/fvg_entry touch-only (`fade_requires_touch`); `bd_min_closes=1 bd_min_disp_atr=1.00` (env unset → defaults) | entry-mechanics E1–E9 (08-30 17:10) | **LIVE** |
| D2 Confirm cost −$681 / honest-wait +$1,974.5 | pre-law figures | **No post-entry-law recompute exists** (grep of reports → none); E8 `ab_confirm_log` n=82 unconsumed and 20/82 rows sign-broken (E7) | E8 | **IGNORED** |
| D3 Stop floor | 1.5–2.5×ATR (citation not found in repo) | `MIN_SL_ATR_MULT` unset → `kernel/min_sl.go:18-23 const 1.0`; boot `🛑 min-sl guard: atr_mult=1.0 level_clearance=2tick(s)`; ATR-leg rejects fire (`MIN-SL REJECT … sl_too_tight: 15.5 < 1.0×ATR (24.9)` 05:15:36) | 0B | **IGNORED** ([C], "deliberate first step") |
| D4 Stop anchoring | 0/5 biggest-loser stops on a seated level | **No anchoring logic exists** — validation only (ATR floor; ≥2-tick clearance vs cited anchor `engine_position.go:239-256`; fade-arm ≥2 ticks `entry_law.go:160-176`); the clearance leg has fired **0 times** | 0B (I5) | **IGNORED** |
| D5 BE+40 / ATR trail | 0B suspends; dead wire for non-signal entries | **LIVE**: `breakeven_enabled:true trigger 40`, `trailing_enabled:true` 2.0×ATR14 arm after_breakeven; 14 BE fires + 25 trail moves logged all-time (today 03:00:29 +40.5pt, 05:51:28 +41.0pt); 24 `no open entry to move the stop` failures **all before GAR-F1 (08-28), 0 since**; `ModifyBracket` frames ever: **0** | 0B | **LIVE, contradicts 0B** (not shipped) |
| D6 ARM_MIN_RR | owner-ruled 2.0 | env unset → 2.0; boot `arm_rr=2.0 (gate-at-arm only; market-entry floor 3.0 unchanged)`; **no hard geometric gate in the plan validator** (only advisory `ArmFeasibilityWarnings` `plan_doc.go:490-527`); AI-path floor = strategy `min_risk_reward_ratio` = **2.0 since 08:13 CT** (was 3.0 — `📐 R:R eval … (min 3.00)` 06:35:04 → `(min 2.00)` 15:08:00); gate-at-arm binds first for arms | 08-27 ruling | **LIVE** (boot text stale) |
| D7 Size | 1 contract | `max_contracts_per_order` **2** in DB → clamp 2 (`kernel/risk_limits.go:268-279 ResolveMaxContracts`, always-on), split-leg capacity 2; fills since 08-18: qty 1.0 ×221, **2.0 ×1** (pos 574, 08-31 04:00 UTC, pnl_corrected −1.0) | 0B | **LIVE cap 2, practice 1** |
| D8 EOD flat | premise "default 15 → 14:30" | `store/strategy.go:1013 DefaultEODFlatOffsetMin = 0` → **14:45** (R-A15); boot `NY … flat 14:45`; Guide/FE 14:45; stale comment `:996-1000` says "nil → default 15"; lunch hard-gate 12:00–13:30 (`auto_trader_session.go:120-121`) vs `ny_pm 13:00–14:45` **overlap still present** | R-A15 | **LIVE 14:45** — the 14:30 premise is CONTRADICTED-BY-LIVE (14:30 = last-entry) |
| D9 Killzones | NO PREMIUM (33.0% in n=285 vs 33.0% out n=282) | still used: planner advisory weighting (`planner_prompt.go:534-537`, boot `killzone_weights=on`) + adherence step-down "entered outside a killzone" (`adherence.go:76-79`); not an entry gate; **0 commits touching killzone since the verdict** | — | **IGNORED** |
| D10 Proximity | owner 1.0; unit dATR | DB `proximity_filter_atr: 1` → `ResolveProximityK` 1.0; `levels_score.go:414 band := proximityK * dATR` (DailyRangeProxy); runtime bands ±148…±265 pt; boot literal `retuned 0.3` and Guide "⭐ 0.3 LIVE since 08-28" are **wrong** | GAR-F2 | **LIVE 1.0**; boot/Guide CONTRADICTED-BY-LIVE |
| D11 min_side_levels | deleted 08-31 | no field, no resolver, no FE row, no boot line, no chip; only `const MinSideLevels = 3` seating-balance target | e86ae805 | **LIVE (deleted)** |
| D12 Grader | invented multipliers; spec divergences | all present (below): zoneSize 1.25…0.50 · TF 1.0/1.1/1.2/1.3 · reversal 1.1 · fresh 1.0/0.8/0.6/0.5 (anchors) vs 1.0/0.6/0.3/0.15 (zones) · confluence 1+0.2·n cap 3 · HTF ×1.2 · iFVG = FVG weights on a shadowed family · ±1σ share `KindVWAP` · anchors decay despite the comment · B2 12-tick Tier-1 override | 3A | **LIVE** (divergences intact) |
| D13 Seated count | "seven lines in 50 points" | last 5 plans (NY v1–v5 today): 12/12/11/12/11 levels; ≤10pt clusters are pairs (one triple 14.12pt); densest 50pt window = 4 lines | 2A | complaint not reproduced on these 5 |
| D14 Cluster tolerance | keep 3pt; re-express as volatility-scaled | `levels_score.go:674-685 const LevelClusterTicks = 12` → 3.00 pt, compile-time (`price` discarded); no boot print | 3C | LIVE (fixed) · 3C QUEUED |
| D15 Touch band | replace for measurement with k×Δ | `TOUCH_BAND_TICKS` unset → 16t = 4.0pt (boot `🎯 touch telemetry: band=16t(4.0pt)`); **three "touch" definitions coexist** (16t band; `LevelTouchTolPoints = 4.0` const in level_stats; zero-band wick `levelTouched` in plan_lifecycle) | 1C | LIVE (fixed) · 1C in flight (no parked recommendation exists yet — see ⟂) |
| D16 Flip | buffer ≥1.0; two-stage; max-3 breaker; no weekly cascade | `FLIP_ATR_BUFFER` unset → **0.5**; dormant *replaces* the flip (never flips bias, same version); **no flip counter**; weekly invalidation has **no consumer** in the session lifecycle; NY today cycled dormant/rearm 7× | 4A | **CONTRADICTED-BY-LIVE** (all four parts) |
| D17 Weekly bias | shadow/WARN; closed weeks only; sticky-nil fix | `WEEKLY_COUNTER_MODE` unset → warn; `weekly_bias.go:82-92` completed weeks only; only successful loads cached (`auto_trader_weekly.go:241-269`, in `fef656a4`); DOA guard in binary, unproven on a Sunday | — | LIVE |
| D18 min_confidence | 60; 65/60 mismatch | DB 60; both defaults = `store.SafeDefaultMinConfidence = 60` (`store/strategy.go:75-81`, `engine_prompt_futures.go:63-67`) — **mismatch not present**; no `confidence too low` line in the window | — | LIVE (60); mismatch FIXED |
| D19 HTF veto | owner `cross`; 1h-only cost $352 | `.env HTF_VETO_MODE=cross` → boot `🛡️ htf veto: mode=cross tf=1h`; recompute exists: `2026-08-29-weekend-audit-p2.md:42` "HTF-veto-cross n=9 −$114.0 SAVING … KEEP cross"; cross has not fired since 08-30 | — | LIVE (cross) |
| D20 Sweep / BOS | zero depth; never reprices | `kernel/structure.go:340-342` `if b.High > lastHigh.price && b.Close <= lastHigh.price` → **one tick qualifies**; `lastHigh/lastLow` assigned once per run (`:298-309`), only read in the event loop → **never repriced within a run**; the dispatch's `scenario_facts.go:248-297` / `levels_role.go:33-45` ranges do not hold this logic | — | LIVE (as researched) |
| D21 stale_reeval | SAVING; leave alone | `STALE_REEVAL_DRIFT_ATR` unset → 0.25 (`discard_burn.go:38`); boot `reeval_drift=0.25×ATR14`; untouched since 08-29; applies to market entries only (armed fills: `stale_reeval NOT applied`) | — | LIVE, untouched |
| D22 FAST_MARKET_ATR / cadence | keep 1.5; cadence is the bottleneck | 1.5 (`auto_trader_loop.go:77-88`, no boot print); `DefaultWakeMinIntervalMin = 30` (`store/strategy.go:1349`; comments at :953/:959 say "10"); runtime `SKIPPED: 30m elapsed < wake_min_interval_min (30m)`; **no change since 08-29** | — | LIVE 1.5 · cadence UNCHANGED |
| D23 Monte Carlo rig | build | `grep -ri monte cmd/ kernel/ trader/ store/` → 0 | 1E | QUEUED (absent) |
| D24 MAE/MFE intrabar | build | `kernel/mae_mfe.go:24-52` = 1m-bar High/Low excursion computed **once after close** (`auto_trader_clock.go:724-726`, 2000-bar window), entry bar excluded; columns `DEFAULT 0` (never NULL); since 08-25 n=33, mae≠0 28, mfe≠0 29 | 1A | PARTIAL — 1m proxy LIVE, intrabar QUEUED |
| D25 Candidate pool + unseated outcomes | build | only seated levels: `level_stats` 113 rows (PK trader/day/price/label), `touch_episodes` 776, `level_state` 498; **no read_id/candidate_id/seated flag/propensity**; `position_plan_join` is a VIEW (582 rows) | 1B | QUEUED (absent) |
| D26 Per-condition expectancy | build | none in `api/`; aggregates have no condition grouping; positions carry `cited_scenario_id`/`plan_version` but no condition column | 1D | QUEUED (absent) |

### D — Evidence detail (selected; the sub-agents' full quotes are preserved here)

**D1** [CODE] `kernel/entry_law.go:29-72` law table (`"reject": {Allowed: {"touch": true}, FadeTouch: true}`, `"breakdown_continue"/"breakup_continue": {1x5m_close, 2x5m_close}` "2x5m legal ONLY here"); `:141-144 fade_requires_touch`; `kernel/breakdown_continue.go:65-72 bdConfirmCloses` default 1; 15m dead for authorship `kernel/plan_doc.go:598-599, :615-616, :642-643` (`confirm_rule_15m_removed`), one legacy eval path `kernel/plan_confirm.go:23-31 case "15m_close": // legacy: stored docs only`. [RUNTIME] `fade_requires_touch` rejections ×4 today (00:55:17, 03:22:33, 03:51:16, 07:27:12). [DB] confirm rules in plans since 08-30: `reject|touch|29`, `sweep_reclaim|touch|11`, `hold||8`, `breakdown_continue|1x5m_close|3`, … the two off-law rows (`reject|2x5m_close`, `sweep_reclaim|15m_close`) belong to `2026-08-30:ASIA v1` written 17:08:56 CT — 2 minutes before the entry-law cutover (`kill -9 482741 17:10:42`) → pre-law doc.

**D2** [DOC] `2026-08-30-confirm-cost-forensics.md:80 | total | 30 | 0 | −809.1 | +128.0 | −681.1 |`; `2026-08-28-grand-audit-bcde-verdict.md:44 | AI declines (fresh-MET) | 35 | 26 | 9 | 0 | +1,974.5 | COSTING |`. `grep -rln "post-entry-law"` over reports → none; `2026-09-01-research-inventory.md:54` "confirm-cost forensics … **NO ACTION**". [DB] `ab_confirm_log` 82 rows (08-30 18:37 → 09-01 13:35), by rule `1x5m_close 27 · 2x5m_close 26 · 1m_mss 22 · touch 7`; outcomes `open 62 / stop 8 / target 12` — but see E7 (sign bug) before any use.

**D5** [DB] `risk_control: breakeven_enabled:true, breakeven_trigger_points:40, trailing_enabled:true, trailing_atr_period:14` (no mult/arm → defaults `auto_trader_trailing.go:25-26` 2.0, `TrailArmAfterBreakeven`). [RUNTIME] `09-01 03:00:29 🎯 auto-breakeven: MNQ SHORT +40.5 pts in profit → stop moved to breakeven (entry 29466.00)`; `05:51:28 … +41.0 pts … (entry 29246.75)`; `📈 trailing_moved` ×8 05:51:28→06:05:28 (e.g. `old=29244.29 new=29242.75 best=29203.25 atr=19.75 mult=2.0`). All-time: BE fires 14, trail moves 25, `move-stop send failed … no open entry to move the stop` 24 (08-25 ×20, 08-27 ×4), **0 since GAR-F1**. [CODE] dead wire `trader/ninjatrader/tcp_trader.go:611-620 MoveStopToBreakeven … sid := t.resolveEntrySignalID(...); if sid == "" { return fmt.Errorf("ninjatrader/tcp: no open entry to move the stop …") }`; resolver `:588-604` tries `lastEntrySignalID` → `entryOrderID[key]` → persisted `p.EntryOrderID`. Frame sends are not individually logged (`provider/ninjatrader/tcp_server.go:1067-1081 SendMoveStop` has no log line); NT8-side acknowledgement of the stop move is UNVERIFIED.

**D6** [CODE] `trader/armed_executor.go:54-66 armMinRR` default 2.0, hard gate `:1138-1149`; validator advisory `kernel/plan_doc.go:490-527 ArmFeasibilityWarnings` "Advisory only — the write succeeds"; AI-path `kernel/engine_position.go:149-154 effRR := minRiskReward; if effRR <= 0 { effRR = 3.0 }` with `minRiskReward` = strategy `min_risk_reward_ratio` (clamp `store/strategy.go:194-202`). [DB] `"min_risk_reward_ratio":2`. [RUNTIME] `06:35:04 📐 R:R eval … R:R=3.07 (min 3.00) → PASS` → `15:08:00 … R:R=2.00 (min 2.00) → PASS`, `15:24:46 R:R=2.14 (min 2.00)`. ⟂ The 08:13 save is the only event between those lines (§G3); no config-diff line was logged for the change.

**D7** [CODE] `trader/auto_trader_orders.go:25 const maxFuturesContracts = 2.0`, `:52-58 resolveMaxContracts → kernel.ResolveMaxContracts(rc.MaxContractsPerOrder, 2)`; `kernel/risk_limits.go:268-279` "ALWAYS ON for futures … NEVER returns 0". [DB] `"max_contracts_per_order":2,"max_contracts_enabled":false`. [DB] fills since 08-18 `1.0|221`, `2.0|1` (id 574).

**D8** [CODE] `store/strategy.go:1006-1014` "R-A15 … NY flattens at 14:45 CT (the standing R5 ruling), not the drifted 14:30" → `DefaultLastEntryOffsetMin = 15`, `DefaultEODFlatOffsetMin = 0`; `trader/auto_trader_clock.go:455-478 enforceEODFlatAt`; registry `WindowEndCT: "14:45"`, `FlatCT: "14:45"`. Guide `tradingDay.ts:39-43 time: '14:45', label: 'EOD flat'`; `:110 ['NY', '08:25', '08:30 → 14:45', …]` (**read time 08:25 stale** vs live 08:00); `faq.ts:74`; FE `sessionConfig.ts:65-67`. Legacy fields `"last_entry_ct":"13:00","eod_flat_ct":"14:45"` persist in the JSON but are UNREACHABLE (`auto_trader_clock.go:303-313, :343-348`). Lunch gate `auto_trader_session.go:120-121 InBlackoutWindow(now, "12:00", "13:30")` + `adherence.go:121`; `session_registry.go:112 {ny_pm 13:00–14:45}` → overlap 13:00–13:30; `auto_trader_session.go:19-22` "Reconciling the killzone semantics is a P4 admin decision."

**D9** [DOC] `2026-08-30-cheap-five-knob-verdicts.md:205-206` `| In killzone | 285 | 94 | 33.0% | −$123.77 | −$0.43 |` / `| Out killzone | 282 | 93 | 33.0% | −$395.50 | −$1.40 |`; `:227-231` "**Verdict: NO PREMIUM**". [CODE] `kernel/planner_prompt.go:534-537 "## Killzone weighting (advisory)"`; `kernel/adherence.go:76-79 if !in.InKillzone { grade = stepDown(grade, 1); … "entered outside a killzone" }` → `trader_positions.adherence_grade`. [DB] grades since 08-25 (n=37): A 13, B 10, C 4, D 7, F 3 — reasons not persisted, so the killzone share is UNVERIFIED. `git log --since=2026-08-30 -S killzone -- kernel trader store web/src` → 0.

**D10** [CODE] `kernel/plan_lifecycle.go:25-30 ResolveProximityK` (0.1–3.0 else 1.5); `kernel/levels_score.go:414 band := proximityK * dATR`; `kernel/levels_assemble.go:285-291` "DailyRangeProxy … NOT an ATR". [RUNTIME] `🗺️ seated N/M in-band levels (proximity band ±Xpt …)` today: ±175 (00:44), ±159, ±148, ±265 (04:00), ±252 (06:00–08:00), ±236 (08:13), ±209, ±185 (10:01–13:01), ±184 (14:01–15:59). Stale text: boot `🎛 volume wave: … proximity=cfg(resolved per-trader; retuned 0.3)`; Guide `settings.ts:25-40` "⭐ 0.3 — LIVE since 2026-08-28 11:59 (owner save)"; `levels.ts:123` "retuned 0.3 → ≈±100pt".

**D12** [CODE] `kernel/levels_score.go`: weights `:86-121` (1.0 PDH/PDL/PDC/RTHH/RTHL/PWH/PWL/PMH/PML; 0.85 ONH/ONL/NPOC/SWGH/SWGL/EVWAP/PDVWAP; **0.90 KindVWAP, KindPOC** `:96-97` "Pack B (owner override 2026-08-26) — VOLUME FAMILY weights, provisional"; 0.85 KindVWAP2S; 0.80 VAH/VAL/SETT; 0.60 MIDO; 0.70 ASH/ASL/LDNH/LDNL/ORH/ORL/IBH/IBL/EQH/EQL; 0.55 Round/Gap; 0.30 zones); zone base `:149-154` `KindOB {1m:.40,15m:.50,1h:.70,4h:.72}`, **`KindFVG, KindIFVG, KindSupply, KindDemand` all `{1m:.35,15m:.45,1h:.65,4h:.65}`** (iFVG emitted at `levels_zones.go:253`, feeds the shadowed `fvg_entry`); `:157 zoneTFMult = {1m:1.0,15m:1.1,1h:1.2,4h:1.3}`; `:160 zoneReversalBonus = 1.1`; `:137-142` "EFFECTIVE 4h:1m spread ≈2.3× … DOCUMENTED, NOT CHANGED"; `:205-223 zoneSizeMult` (dATR units) ≤0.30→1.25, ≤0.60→1.10, ≤1.00→1.0, ≤1.50→0.85, ≤2.50→0.70, else 0.50; `:461-465` confluence cap `CONFLUENCE_CAP` unset → 3; `:482` zones `zoneEvidence × zoneSizeMult × fm × (1 + 0.20*effConf) × zoneTFMult`, `:484` lines `typeEvidence × fm × (1 + 0.20*effConf) × htf`, `:475-478 htf = 1.2 if l.HTF`; freshness anchors `:359-372 freshMult 1.0/0.8/0.6/0.5`, zones `:379-391 1.0/0.6/0.3/0.15`; comment `:374-377` "ANCHORS keep the original ladder — 'anchors no-decay' per spec" **vs** `:434 fm := freshMult(fRaw)` applied to every non-zone kind, and the writer `trader/auto_trader_levelstate.go:96-149` decays all active levels via `ls.RecordPlay` → `store/level_state.go:192-200` with **no kind exemption**; VWAP bands `kernel/levels_volume.go:54-55 lineLevel(KindVWAP, vwap±sd, "VWAP±1σ")` share `KindVWAP`; B2 `:257 const Tier1ProximityTicks = 12`, `:299-323 withinTier1Proximity`, applied `:514-516 if grade != "C" && !withinTier1Proximity(l, inBand, price) { grade = "C" }`. [RUNTIME] boot `zone-ladder=1.0/0.6/0.3/0.15 · family-confluence(cap=3)` matches.

**D13** [DB] `plans.doc.levels` for `2026-09-01:NY` v1–v5: v5 (13:00:02) 11 levels — 29248.19 eVWAP·A, 29209.25 Supply·1h·C, 29197.5 SWG-H·5m·A, 29162 SWG-H·5m·A, 29130 SWG-L·5m·A, 29125 OR-H·C, 29100.5 SWG-L·5m·A, 29082.75 ONL·A, 29062.75 OR-L·A, 29040 SWG-L·15m·A, 28960.89 VWAP−2σ·A (cluster 29125–29130 = 5.00pt; densest 50pt window 4 lines 29082.75..29130.00); v4 12 (pairs 29209.25/29212.50 = 3.25pt Supply·1h/OB(bull)·1h; 29261.19/29266.78 = 5.59pt VWAP/eVWAP); v3 11 (29177.5/29182 = 4.5pt SWG-L·5m/SWG-L·15m); v2 12 (28963.03/28970.38 = 7.35pt VWAP−2σ/OB(bull)·4h; 29177.5/29182); v1 12 (triple 29085.00/29094.31/29099.12 = 14.12pt OB(bull)·1h/VWAP−2σ/Demand·4h; 29177.5/29182). Spans 287.3/284.5/211.7/290.5/269.0 pt. Max lines in any 50-pt window = 4.

**D14/D15** [CODE] `kernel/levels_score.go:674-685 const LevelClusterTicks = 12; func clusterToleranceFor(price float64) float64 { _ = price; return LevelClusterTicks * 0.25 }` (consumers `:535`, `plan_doc.go:800`, `auto_trader_planner.go:1367`); `kernel/touch_telemetry.go:28-36 TouchBandTicks` env default 16, `:70 TouchBandPoints`; `kernel/level_stats_calc.go:20 LevelTouchTolPoints = 4.0`; `kernel/plan_lifecycle.go:190-201 levelTouched … if b.Low <= level && b.High >= level`. ⟂ **Correction to the D14–26 sub-agent:** it reported "No /home/hoang/vl-class35 directory … no class35/touchband branch" — at audit time `git worktree list` shows `/home/hoang/vl-class35 d4b38604 [fix/class35-replan-budget] locked` and the lock was re-acquired at 16:40:57 by pid 1860416 (`note=re-acquired-after-stale-holder-1861644-dead`). The 1C work is in flight in that worktree; it had produced no commit (`ahead_of_dev=0`) when checked. No parked 1C recommendation exists yet in any report.

**D16** [CODE] `kernel/plan_lifecycle.go:322-329 FlipATRBuffer` default 0.5, applied `:255`; boot `🧬 plan lifecycle: hysteresis=buffer0.5×ATR14 confirm=2close(s) · flip/death→dormant+auto-rearm (version unchanged, budget untouched) · exec_reasoning=fast→low plan_reasoning=max`; `trader/auto_trader_planner.go:295-313` structured killer → `UpdatePlanLifecycle(…, "dormant", marker)` then `continue` (never re-plans, never flips bias); re-arm `:260-268`; `grep -rn -i "flipCount|flip_count|maxFlips|MAX_FLIPS"` → none; weekly `trader/auto_trader_weekly.go:277-336 maybeCheckWeeklyInvalidation` appends a WEEKLY row only, `grep weekly_invalidated|InvalidatedAt` outside the weekly files → no consumers. [RUNTIME] NY today: `09:15:08 v2 DORMANT (death)` → `09:51:07 v3 DORMANT (death)` → `10:11:07 v3 REARMED` → `10:13:07 v3 DORMANT — flip-condition … (8× 5m closes)` → `10:51:07 v4 DORMANT (death)` → `11:15:07 v4 REARMED` → `11:17:07 v4 DORMANT — flip-condition … (10× 5m closes)` — seven transitions, no breaker. ⟂ Note the boot line's "version unchanged" is contradicted by the plans table: dormant/rearm transitions appear as new version rows (§G1) — the D-agent read them as in-place `UpdatePlanLifecycle`, the E-agent found `store/plan.go:286-293 UpdatePlanLifecycle` overwrites `trigger_reason` in place. Both are true of different rows: v2/v3/v4 were WRITTEN by level-event wake reads (journal `08:43:08 … waking the planner (W6, 5th wake-up)` → `08:53:16 PLAN written NY v2` etc.) and their `trigger_reason` was later overwritten by the dormant/rearm transition — provenance destroyed (E1 finding 2).

**D17** [CODE] `kernel/weekly_knobs.go:96-105 WeeklyCounterMode` default warn; `kernel/weekly_bias.go:82-92 weekCompletedAt/CompletedWeekCandles`; `auto_trader_weekly.go:241-269` "ONLY successful loads are cached" (commit 2bc58ed9, present in `fef656a4`); DOA guard `:205 ApplyWeeklyDOA` (59dc9460). [RUNTIME] `08-30 17:01:18 📅 WEEKLY READ starting …` → `17:07:15 WEEKLY READ written 2026-08-31 v1 bias=bear …` → `17:07:18 📅 WEEKLY INVALIDATED bear @ 29535.00 (1h, v2) — bias→neutral, no auto-flip; no re-read until next Sunday.`; `09-01 01:01:09 🌗 SHADOW wk-confl PDH@29538.75 (ASIA) real=A shadow=4.5 — view only`. Note the 08-30 read fired 17:01 (pre-class-32) and v1 lived 3 s before invalidation (the F5 DOA case).

**D18** [CODE] `store/strategy.go:75-81 SafeDefaultMinConfidence = 60` ("Was 65 here vs a literal 60 in engine_prompt_futures.go … Aligned to 60"); `:215-216` clamp; `kernel/engine_prompt_futures.go:63-67`; gate `kernel/engine_position.go:200`. [DB] 60. Boot `watcher[min_conf=70 …]` is the ai_watch watcher, not the entry gate.

**D19** [CODE] `kernel/htf_veto.go:39-50, :86-98` (cross = 1h AND 4h oppose; missing snapshot fails open). [RUNTIME] boot `🛡️ htf veto: mode=cross tf=1h (1h|cross|4h via HTF_VETO_MODE; cross = 1h AND 4h agree)`. [DOC] `2026-08-29-weekend-audit-p2.md:33,42,48`. Whether that replay read `pnl_corrected` is not stated → UNVERIFIED.

**D20** [CODE] `kernel/structure.go:340-342` (TRENDING_UP) `if b.High > lastHigh.price && b.Close <= lastHigh.price { events = append(events, StructureEvent{Type: "SWEEP", Dir: "up", …` and `:363`, `:368-375` mirrors; `:298-309 lastHigh, lastLow := newest, newest` + two backward scans, then read-only in `:313-377`; pivots `:200-244` (k = `structureSwingK()`, min-move `structureMinSwingATR()*atr`). [RUNTIME] stored executor prompt (14:39:36 UTC) `1h: TRENDING_UP (HH 29571.00 @01:00 CT) · last event: SWEEP-up 5m @09:20 CT`; 35 of n=1109 decision rows since 08-31 carry `SWEEP`, 26 `CHoCH-`, 0 `BOS-`/`MSS-` tokens (`StructurePromptLine` `structure.go:412`).

**D22** [CODE] `trader/auto_trader_loop.go:77-88 fastMarketATR` default 1.5, consumer `auto_trader_planner.go:1200`; `store/strategy.go:1349 DefaultWakeMinIntervalMin = 30`, `:1388-1393 WakeMinIntervalMinutes`; throttle `auto_trader_wake_levels.go:260-262`. [RUNTIME] fast-market reads `02:45:29 (drift 60.0 pts = 2.7×ATR5m)`, 03:17 (3.8×), 03:47 (5.8×), 04:19 (8.3×), 04:50 (9.4×) — ~30 min apart, paced by the throttle (`03:15:29 🗓️ structure MSS on LONDON … SKIPPED: 30m elapsed < wake_min_interval_min (30m)`).

**D24** [DB] `SELECT COUNT(*), SUM(mae<>0), SUM(mfe<>0) … exit_time >= 1787634000000` → 33 / 28 / 29 (both nonzero 25; both zero 1); `pnl_corrected IS NULL` 4 (excluded). Sample: `584 | mae 0.0 | mfe 58.0 | +106.0 | SHORT 1 min` · `585 | 38.75 | 0.0 | −77.5 | 9 min` · `586 | 27.5 | 0.0 | −54.0 | 8 min`.

**D25** [DB] `.schema level_stats` → `(trader_id, session_day, price, label, kind, grade, role, family, touched, reacted, broke_clean, chopped, created_at)`; `touch_episodes` (776): `label, level_price, touch_number, opened_at_ms, closed_at_ms, bars_in, penetration_pts, wick_pen_pts, body_pen_pts, close_1m, close_5m, vol_ratio, approach_atr, shape`; `level_state` (498). [DOC] `2026-08-30-cheap-five-knob-verdicts.md:30-33` "The raw excluded universe (machine candidate pool) is **not persisted anywhere**".

**D26** [CODE] `store/position_query.go:40,:47,:117` (pnl_corrected-first, no condition grouping); `GetFullStats` → boot `📈 Trading stats: 214 trades, 36.9% win rate, PF=0.93` (coerced, see E5). "1B-PRE" appears nowhere in `docs/`.

### D — Surprises (both agents, deduplicated; ⟂ corrections applied)
1. **AI-path R:R floor dropped 3.0 → 2.0 at 08:13:06 CT** via the Studio save; no config-diff line logged; boot line stale ("market-entry floor 3.0 unchanged").
2. **Proximity "0.3" is fiction in two places** (boot literal + Guide) vs DB 1.0 and runtime ±184 pt — GUIDE CONTENT LAW drift on the running rev.
3. `store/strategy.go:996-1000` comment "nil → default 15" vs `DefaultEODFlatOffsetMin = 0` (`:1013`); `:953/:959` say wake default "10" vs const 30.
4. `ab_confirm_log` money columns mis-scaled on short scenarios (E7).
5. `max_contracts_per_order = 2` → clamp 2 / split capacity 2 vs the "1 contract" research and the boot text "capacity=1"; one 2-lot fill exists (574).
6. Lunch gate 12:00–13:30 still overlaps ny_pm 13:00–14:45.
7. Legacy `last_entry_ct`/`eod_flat_ct` fields persist in the JSON but are unreachable.
8. `ModifyBracket` never sent in any log.
9. The min-SL clearance leg has never fired; anchoring half of A3 dormant in practice.
10. Bound strategy `a5b7662e…` has `is_active=0` while two "MNQ SIM Default" rows (one with an empty `id`) carry `is_active=1,is_default=1`.
11. Guide `tradingDay.ts:110` lists the NY read at 08:25 (live 08:00).
12. Boot prints `📊 Using CoinAnk API for all market data` on a futures-only deployment (cosmetic).
13. Three distinct "touch" definitions coexist (D15).
14. Structured flip does not flip — it parks; NY cycled 7× today.
15. `mae`/`mfe` `DEFAULT 0` — cannot distinguish computed-zero from absent; entry bar excluded.
16. `GetFullStats` does not exclude NULL `pnl_corrected` (E5).
17. ⟂ The D14–26 agent's "vl-class35 does not exist" claim was wrong (worktree present, lock re-acquired 16:40:57).

### D — UNVERIFIED
- D3: a research source stating "1.5–2.5×ATR stop floor" — not found in the repo (nearest: v5 build plan `stop_atr_mult: 2.5`); would verify: the owner naming the source.
- D5: the non-signal dead wire at runtime since GAR-F1 (no manual NT8 entry reached +40 pts since 08-28); NT8-side acknowledgement of the 09-01 stop moves (NT8 Orders tab / log).
- D6: the exact fields changed by the 08:13 save (inferred `min_risk_reward_ratio` 3→2); would verify: `~/vl-backups/auto/` before/after diff.
- D7: split-arm capacity 2 at runtime (no refusal/acceptance line either way).
- D9: killzone share of adherence step-downs (reasons not persisted).
- D14: cluster tolerance at runtime (no boot print, no collapse line).
- D18: min_confidence gate live-fire in the window.
- D19: whether HTF cross has ever fired since 08-30; whether the 08-29 replay used `pnl_corrected`.
- D17: DOA guard + class-32 16:30 weekly read on the next Sunday.
- "0B" / "1B-PRE" labels — no document names them.

---

## SECTION E — THE COUNTER AUDIT

*(Read-only sub-agent; ⟂ audit-lead confirmations — the gate-block API payload, the strict/coerced totals, the prompt text, and the 584/586 lineage state were re-derived independently.)*

Enum strings [CODE `store/position.go:103/111/117`]: `close_reason ∈ {reconcile_flat, unresolved, e7_farside_test}` = `UnknownPnLReason`; [DB] `source ∈ {system 563, reconcile 11, armed_entry 5, e7_farside_test 3}`; `close_reason ∈ {sync 513, reconcile_flat 62, unresolved 4, e7_farside_test 3}`; all 582 rows CLOSED; `pnl_corrected NULL = 357 / NOT NULL = 225`. Three SIGKILL restarts inside this session-day (17:34:21, 23:40:22 Aug 31, 00:43:25 Sep 1) [RUNTIME] — relevant to every in-memory counter.

### E — Master table

| # | Counter | Counts what | Recorded vs inferred | Excludes unresolved / reconcile_flat / test-seam / duplicates | P&L column | Verdict |
|---|---|---|---|---|---|---|
| E1 | Replan budget `ReplansLeftFrom` | re-plans used per chain | **INFERRED** = `version − baseline` (row count) | n/a — NO-TRADE markers, owner-reset rows, wake reads and dormant/rearm rows all consume versions | n/a | **DEFECT (class 35)** — live: LONDON v6 → 0 left, NY v5 → 0 left, zero deaths |
| E2 | Guardrail ENTRIES (`GetSessionDayActivity` cnt) | rows with `entry_time ≥ session start` (+account) | inferred from rows | **excludes NOTHING** (no close_reason/source/status filter) | n/a | DEFECT latent — today's 6 are all real; 08-30 counted 3 e7 rows |
| E3 | Guardrail DAILY-LOSS sum | `SUM(COALESCE(pnl_corrected, realized_pnl))` under `pnl_corrected IS NOT NULL` | rows | excludes 3 reasons + NULL | pnl_corrected | OK (enforcement OFF) |
| E4 | `CountConsecutiveLossesSince` | tail losing streak | rows, `exit_time DESC` | excludes 3 reasons + NULL | pnl_corrected (NULL pre-filtered) | OK (knob OFF) — today's streak 2 (586, 585) |
| E5a | `GetPositionStats` | totals | rows | excludes 3 reasons + NULL; surfaces `excluded_null_pnl` | pnl_corrected | OK (strict) |
| E5b | `GetFullStats` | totals/winrate/PF/Sharpe/DD | rows | 3 reasons; **NOT NULL** | `EffectivePnL()` → **coerces 115 NULL rows to raw** | **DEFECT** — feeds the AI prompt `Total PnL: -203.68 (220 trades)` vs strict `+304.32 (105)`; `/position-history` stat cards; the consistency rule |
| E5c | `GetSymbolStats` / `GetDirectionStats` | per-symbol / per-side | rows | 3 reasons; NULL coerced | coerced | DEFECT (same coercion; sides mixed-case `LONG 276 / SHORT 303 / long 1 / short 2`) |
| E5d | `GetHoldingTimeStats` | hold buckets | rows | **nothing** (`exit_time > 0` only) | coerced | DEFECT (n=246 incl. 10 unknown-reason + 121 NULL) — latent (uncalled) |
| E5e | `GetRecentTrades` | last N → AI prompt + `/trades` | rows | **nothing** | coerced; unknown rows render `+0.00` | **DEFECT live**: prompt row 579 `Profit: +0.00 (+100.00%)`, row 577 (reconcile_flat duplicate) `+0.00` |
| E5f | `GetHistorySummary` + streaks | summary | rows | recent/streaks 3 reasons, NULL coerced; AvgHolding nothing | coerced | DEFECT (latent) |
| E6a | `telemetry.IncGateBlock` (43 names) | gate blocks per trader/session-day | **RECORDED** | n/a | n/a | OK design; **in-memory, wiped by each of 3 restarts**; rollover summary only if a loop crosses 17:00 |
| E6b | `armsRefusedShadowed` | shadowed arm refusals | RECORDED | n/a | n/a | write-only — **no reader** |
| E6c | RR / min-SL / one-live-arm / split-leg refusals (`⚔️ arm REFUSED`) | — | **LOG-ONLY**, deduped per (plan,version,scenario,verdict) | n/a | n/a | NOT a counter — uncountable from logs by construction |
| E6d | `planner_preflight_refused` | — | LOG + P1 alert row | n/a | n/a | not counted |
| E6e | guardrail "WOULD have tripped" | — | LOG + `RecordError` (in-memory) + alert | n/a | n/a | not durable |
| E7 | E8 shadow A/B `ab_confirm_log` | 4 counterfactual rules per armed scenario/version | RECORDED (unique key) | n/a | `net_pnl` from **1m-bar replay** (correct source) | **DEFECT**: short-side sign bug → 20/82 rows garbage (rr≈−0.998, mae≈58k pts); short `touch` rows dropped; no direction column; no in-binary reader |
| E8a | `touch_episodes.touch_number` | touch ordinal per level | INFERRED from **process memory** | n/a | n/a | DEFECT: resets on every restart (sequences 1..9,1,2 in DB) |
| E8b | `level_stats` | touched/reacted/broke_clean/chopped per seated level/day | computed nightly (17:05 CT) from persisted 1m bars | n/a | n/a | OK-ish; touch ordinal not persisted; only the LAST plan version's levels evaluated; today not yet written |
| E9 | Dashboard KPIs | see below | — | — | mixed | partial |
| E10 | raw `realized_pnl` sites | see below | — | — | 4 READER-RAW sites | findings |

### E1 — Replan budget (class 35) — live state
[CODE] `store/strategy.go:1202-1221` (`ReplansUsedFrom = version − baseline`; `MayReplanFrom = used < cap`; `ReplansLeftFrom = max(0, cap − used)`); consumers `api/handler_plan.go:215`, `trader/auto_trader_planner.go:2209` (executor prompt), `trader/auto_trader_reread.go:70,76,119`, death path `auto_trader_planner.go:328/336`. [CONFIG] `replan_cap = 4` strategy-level and per-session (NY/ASIA/LONDON) → `ReplanCapFor` 4. [DB] no `dayplan_reset:*:2026-09-01:*` key → baseline 1 for both chains; **no `dayplan_replans_used:*` key exists anywhere** — the "used" count is never recorded. Chains: §G1. Computed: LONDON `ReplansLeftFrom(6,1,4)` = 0, NY `ReplansLeftFrom(5,1,4)` = 0, `MayReplanFrom` false on both → the next legacy death on either chain writes NO-TRADE (`writeNoTradePlan`, gate `plan_replans_exhausted`). ⟂ `/api/plan/today` confirms `replans_left: 0, replan_cap: 4` for both (§G1). Zero `DIED` lines for NY today; v2/v3/v4/v5 were written by level-event wake reads (`08:43:08 … waking the planner (W6, 5th wake-up)` → `08:53:16 PLAN written NY v2`; `09:20:44 v3`; `10:24:39 v4`; `13:00:02 v5`). **Provenance loss** [CODE] `store/plan.go:286-293 UpdatePlanLifecycle` overwrites `trigger_reason` in place — the original trigger of v2/v3/v4 is destroyed (journal narrates `09:51 NY v3 DORMANT — death-condition`, DB now shows v3 = `dormant:flip`, v4 = `rearmed`).

### E2 — Guardrail ENTRIES
[CODE] `store/position_query.go:129-137` `Where("trader_id = ? AND entry_time >= ?", …)` (+account) → `Count`. No `status`/`close_reason`/`source`/duplicate filter → e7 test-seam rows, `reconcile_flat` duplicates (e.g. 577 "class-27 backfill: duplicate of row 578"), netting orphans and reconcile-materialized entries all count. Callers `trader/auto_trader_loop.go:1184` (guardrail `TradesToday`), `auto_trader_session.go:86` (session `max_trades`), `auto_trader_planner.go:2053` (digest). [DB] today (`entry_time >= 1788213600000`, Sim101) → **6** = rows 581 system/sync · 582 armed_entry/sync · 583 system/sync · **584 reconcile/sync (plan_version 0, cited '', grade D)** · 585 armed_entry/sync · **586 reconcile/sync (0, '', D)**. [RUNTIME] `15:21:06 … 🔍 guardrail WOULD have tripped (master OFF, not enforced): max daily trades would trip (today=6, max=3)` (every cycle 15:21–15:59; 263 would-trip lines today). [CONFIG] `max_daily_trades = 3`, `max_daily_trades_enabled = false`, `guardrails_enabled = false` → `CheckSoft` only (`kernel/risk_limits.go:211/:241`). Test-seam exposure: rows 572/573/574 (e7, 08-30 22:34–23:00 CT) counted as 3 entries on 2026-08-30.

### E3 / E4 — quoted predicates
`store/position_query.go:116-121` `Select("COALESCE(SUM(COALESCE(pnl_corrected, realized_pnl)), 0) as total").Where("trader_id = ? AND status = ? AND close_reason NOT IN (?, ?, ?) AND pnl_corrected IS NOT NULL AND exit_time >= ?")` → [DB] today `212.0, n=6`. `:80-100 CountConsecutiveLossesSince` same predicate, `Order("exit_time DESC")`, `if r.EffectivePnL() < 0 { n++ } else { break }` → today 2 (586 −54.0, 585 −77.5, then 584 +106.0). [CONFIG] `daily_loss_limit_usd = 450`, `daily_loss_enabled = false`; `consecutive_loss_halt` absent → 0 → gate OFF (`trader/auto_trader_orders.go:116-118`).

### E5 — Stats aggregators
`EffectivePnL()` [CODE `store/position.go:826-831`] returns `*PnlCorrected` if set else `RealizedPnL`. Predicates quoted in §C2's table. ⟂ [DB] exact trader, Sim101: strict **105 / +304.32**; `GetFullStats` rule **220 / −203.68**; NULL rows coerced 115 / −508.00 raw. [RUNTIME/DB] live prompt (`decision_records` 35906, 15:59:43 CT): `Total Trades: 220 · Total PnL: -203.68 USDT`; "Recent Completed Trades" item 8 `MNQ short | Entry 29459.0000 Exit 0.0000 | Profit: +0.00 USDT (+100.00%)` = row 579 (`unresolved`); item 10 `Entry 29413 Exit 29413 | Profit: +0.00` = row 577 (`reconcile_flat` duplicate). `GetGradedClosedPositions` (`store/position.go:278`) has no reason exclusion; feeds `/api/plan/trades`.

### E6 — Refusal / gate-block counters
[CODE] `telemetry/gate_blocks.go:38-45 IncGateBlock` (in-memory map); `:67-81 RolloverGateBlocks` clears at session-day change; API `api/handler_gate_blocks.go:31-55`. Every reason string (43): `stale_data` · `feed_down, dead_man, frozen, boot_integrity, stop_until, contract_roll_resolved, consecutive_loss, last_entry, session_gate, plan_mode, approval_required` (`auto_trader_orders.go:151-311`) · `rr_gate ×2, min_sl_gate ×2, htf_veto, transition_standdown, executor_plan_dead, scenario_below_min_quality` (`engine_position.go:177-302`) · `night_transition` · `level_burned_retouch` · `task18_cme_closed, task19_contract_roll, task21_concurrent_cap, strategy_studio_daily, strategy_studio_blackout, strategy_studio_consistency, prompt_ownership, reentry_cooldown, price_sanity` (`engine_analysis.go:68-731`) · dynamic `gate` (`tcp_trader.go:349`) · `plan_replans_exhausted, arm_authored, planner_fail_closed, plan_off_plan, plan_matched, plan_cited_mismatch` (`auto_trader_planner.go:771-2304`) · `stale_bar_discarded, superseded_wait, decline_fresh_met, stale_reeval_refused` (`auto_trader_loop.go:777-800`) · `weekly_counter, weekly_counter_block, weekly_counter_resize, weekly_read_failed` · `pnl_integrity_mismatch` (`auto_trader_clock.go:768`) · `clock_skew_observed, no_breakdown_scenario_authored`.

⟂ [RUNTIME] `GET /api/risk/gate-blocks` (authenticated, 16:4x CT) → `session_day_utc 2026-08-31T22:00:00Z` · `summary: gate-block summary (46 total): arm_authored=18 superseded_wait=7 level_burned_retouch=5 decline_fresh_met=4 clock_skew_observed=3 min_sl_gate=2 night_transition=2 plan_matched=2 planner_fail_closed=2 stale_reeval_refused=1`. **Covers only 00:43:30 → now** (the two earlier restarts in this session-day wiped their tallies). No shadow-refusal reason present. Only journal summary in the window: `Aug 31 17:00:04 📊 gate-block summary: no gate blocks recorded` (PID 1391022).

RR / min-SL / one-live-arm / split-leg refusals: log-only via `armRefusalChanged` dedupe (`auto_trader.go:419-423`) — today's deduped lines: 15 `⚔️ arm REFUSED` (5 R:R, 8 stop-too-close, 3 one_live_arm_guard); true counts unknowable by construction. `stale_reeval outcome=refused` (15:08:00) IS counted. `planner_preflight_refused`: log + `emitAlert("P1","planner-preflight")` (`day_plan_alerts` id 556). "WOULD have tripped": log + `RecordError` (in-memory) + `SoftGuardrailFunc` alert.

### E7 — E8 shadow A/B
[DB schema] `ab_confirm_log` unique `(plan_id, version, scenario, rule)`; columns `fill_px, mfe, mae, outcome, rr, atr5m, mfe_r, mae_r, time_to_*_bars, net_pnl, ambiguous, is_counterfactual`. Writer `trader/armed_executor.go:555-593 logShadowAB` → `kernel/shadow_ab.go:51-247 ShadowABForScenario` (1m-bar replay; `NetPnL = (exitPx−FillPx)*pv − frictionUSD` `:238-243`). No reader in the binary. [DB] `outcome: open 62 (−28.92) · stop 8 (−467,132.0) · target 12 (−1,282,664.74)`; sign-broken rows (`rr<0 AND mae>1000`): **20 of 82** — e.g. id 78 `S2 1m_mss reject entry 29105.5 stop 29189 target 29100.5 rr −0.9985 mae 58220 net_pnl −7.5`. Mechanism [CODE `shadow_ab.go:71-77` vs `:154`]: `stop, target, ref` are negated for shorts but `row.FillPx = f.px` stays in raw price space → `risk = 29105.5 + 29189`; the touch rule's negated fill is `< 0` and dropped at `:155` → **no `touch` rows for short scenarios** (7 touch rows vs 22–27 for other rules). No `direction` column.

### E8 — Level touch / reaction stats
`touch_episodes` (`store/touch_episode.go`): one row per CLOSED episode; ordinal [CODE `kernel/touch_telemetry.go:99 opened int // episodes ever opened this process`, `:166 st.opened++`] — **process memory, never seeded from `CountForLevel`** (no caller). [DB] session_day 2026-08-31: 316 rows; `SWG-L·15m 29483.25 → 1,2,…,9,1,2`; `SWG-L·15m 29477.0 → 1,2 | 1,2,3,4 | 1,2` (breaks at the 23:40 and 00:43 restarts). Outcome basis `close_1m` = last closed 1m bar (`:194`), `close_5m` (`:196`); episode closes when `dist > band` or `BarsIn ≥ maxBars` (`:198-206`); `shape ∈ acceptance|rejection|chop` (`:358-366`) on bar close, not intrabar; 2026-08-31: 77 acceptance / 239 rejection / 0 chop. `level_stats` (`trader/ninjatrader/level_stats_wire.go:56-165`): boot + 17:05 CT for the previous day from persisted 1m bars; `touched` intrabar `Low/High ±4` (`level_stats_calc.go:74-76`), `reacted`/`broke_clean` on closes (`:88-108`); only the last plan version per session (`last := vers[len(vers)-1]`); no touch ordinal/count persisted; latest `session_day = 2026-08-30` (12 rows).

### E9 — Dashboard KPIs summing P&L
- `web/src/components/trader/PositionHistory.tsx:134-148 computeDayTotal` — **pnl_corrected only**, skips NULL/unknown/test/duplicate (strict, matches E3) → +212.00 today.
- `PositionHistory.tsx:66-71 effectivePnl` (row display) and `:651` sort — fall back to raw `realized_pnl`.
- `PositionHistory.tsx:212-320, 803` stat cards ← `/api/…/position-history` → `api/handler_order.go:219-225` = `GetFullStats/GetSymbolStats/GetDirectionStats` (coerced).
- `web/src/pages/TraderDashboardPage.tsx:662-717 account.total_pnl` ← `trader/auto_trader_decision.go:197-206` NT8 `brokerNativePnL` realized+unrealized (`tcp_trader.go:798`) → **0.00 while flat**; `daily_pnl` permanently 0 (§G5).
- `api/handler_plan.go:1674` `/api/plan/trades`: `"realized_pnl": p.RealizedPnL` raw.
- `api/handler_trader_status.go:414,489`: literal `RealizedPnL: 0` placeholders.
- AI prompt: `trader/auto_trader_loop.go:1215,1234` → coerced (`kernel/engine_prompt.go:314-320`, `kernel/formatter.go:203-214,470-481`).

### E10 — Every raw `realized_pnl` / `RealizedPnL` site (non-test)
**WRITER**: `store/position.go:148, :389, :411, :482-516, :549, :816`; `store/position_builder.go:157`; `store/position_history.go:183, :214, :255`; `store/order.go:67`; `trader/auto_trader_decision.go:597-616`; `trader/ninjatrader/close_sync.go:250`; `trader/position_rebuild.go:34-187`; crypto brokers (`trader/{aster,hyperliquid,bybit,lighter,kucoin,gate,bitget,okx,binance}/*`); `trader/types/interface.go:16,36`; `kernel/engine.go:86,155,157`; `kernel/risk_limits.go:180-239` (consumes `ctx.DailyRealizedPnL`, which is corrected). **CORRECTOR**: `store/pnl_correction.go:42, :107`. **READER-CORRECTED (strict)**: `store/position_query.go:40, :117`. **READER-CORRECTED-COERCING**: `store/position.go:830`; `position_query.go:256` → `trader/auto_trader_loop.go:1215, :1234` → `kernel/engine_prompt.go:314,320`, `kernel/formatter.go:203,214,470,481`; `web/src/components/trader/PositionHistory.tsx:68-71`. **READER-RAW (findings)**: `api/handler_plan.go:1674`; `PositionHistory.tsx:651` (sort); `trader/ninjatrader/tcp_trader.go:798` (broker-native, by design); `api/handler_trader_status.go:414,489` (hard-coded 0); `agent/tools.go:3359` (§C2). **TEST**: 100+ lines across `*_test.go` (store/pnl_correction*, trader/exchange_sync_test 22, trader/binance/*, ninjatrader/*, kernel/guardrails_test 9, …). No `.sql` files reference the column.

### E — Findings (defects)
1. **Class 35 live-confirmed** on both chains today; no `dayplan_replans_used` record exists; fix in flight (`~/vl-class35`).
2. **Provenance loss**: `UpdatePlanLifecycle` overwrites `plans.trigger_reason` in place.
3. **Entries count** has no exclusion for test-seam / duplicate / unresolved / source (today's 6 real; 08-30 counted 3 e7 rows).
4. **NULL coercion in five aggregators** (`GetFullStats`, `GetSymbolStats`, `GetDirectionStats`, `GetHoldingTimeStats`, `GetHistorySummary/streaks`) — the live AI prompt reads `Total PnL: −203.68 (220 trades)` vs strict `+304.32 (105)`; the consistency rule uses the coerced number.
5. **`GetRecentTrades` / `GetHoldingTimeStats` / `GetGradedClosedPositions`** have no unknown-reason exclusion; the prompt shows an `unresolved` row as `+0.00 (+100.00%)` and a duplicate as `+0.00`.
6. **E8 short-side sign bug** (`kernel/shadow_ab.go:71-77` vs `:154`): 20/82 rows garbage; short `touch` rows dropped; no `direction` column — any Sep-9 tally over this table is invalid as-is.
7. **Touch ordinal is a process-lifetime counter** — resets at every restart (3 today); `touch_number` inferred, not recorded.
8. **Refusal counters**: RR/min-SL/one-live-arm/split-leg are log-only + deduped → uncountable; `armsRefusedShadowed` write-only; preflight/would-trip not durable; all `IncGateBlock` tallies in-memory (wiped 3× this session-day).
9. **Armed-fill lineage stamp never completes for reconcile-materialized rows** ⟂ confirmed [DB]: 584 and 586 sit at `plan_version 0, cited_scenario_id '', source reconcile, adherence_grade D, entry_order_id empty`, while 582/585 (`armed_entry`) carry v3/S2 and v3/S1; `armed_orders` 24 (LONDON v6 S2, signal `6b5b89c6…`) and 28 (NY v5 S3, `d5fed440…`) still `filled ;stamp_pending`. [RUNTIME] `08:37:08` / `13:33:06 ⚡ armed fill … stamp pending (reconcile completes it)`. Chain [B]: `StampArmedLineageIfMatched` ran before the ledger row was `filled`; the fill handler's `GetOpenPositionBySymbol(side="short")` (`store/position.go:616`, case-sensitive `side = ?`) missed a row stored `SHORT`; `RepairArmedLineage` runs only inside `reconcileOnce`. Consequence: adherence grades wrong for 2 of today's 6 trades; `plan_off_plan` over-counted; the F1 attribution join loses these rows.
10. `api/handler_plan.go:1674` emits raw `realized_pnl` on a user-facing feed.

### E — Surprises
- `trader_positions.side` casing mixed (`long 1, short 2` on 576/577/579, all `armed_entry`) → `GetDirectionStats` buckets them separately; `GetHistorySummary` ignores them (canonical-casing law).
- Journal vs DB disagree on NY v3/v4 trigger (in-place overwrite).
- 2816 `log_events` WARN rows today (931 guardrail/gate-block/REFUSED) — a text ledger nothing aggregates.
- `/api/risk/gate-blocks` and `/api/plan/reread` polled every ~20–60 s from 127.0.0.1 between 08:00 and 08:12 (the owner's browser, per the Studio save that followed).
- ⟂ The E-agent's "clock drift 91–210s behind the feed ×3" refers to `kernel/clock_drift.go:83 ⚠️ clock-drift DETECTED (no entry block): local clock is BEHIND the feed by 210s (>60s)` (02:52:43) — a second, separate drift signal; its measurement basis is quoted in §H.

### E — UNVERIFIED
- The exact call-time state that left 584/586 unstamped ([B] chain above).
- Whether the 20 sign-broken E8 rows are exactly the short-direction set (no `direction` column).
- The class-35 fix contents (`~/vl-class35` not opened).

---

## SECTION F — OPEN DEFECTS: CURRENT STATE OF EACH

*(Read-only sub-agent; "now" for the 7-day window = 2026-09-01 16:40:01 CT. ⟂ = audit-lead cross-check.)*

| id | defect | current state | evidence |
|---|---|---|---|
| F1 | Attribution join (`plans.strategy_id` = trader id; version leak; ~37% unresolvable) | `strategy_id` still holds the trader id (1 distinct value; 187/187 rows match `traders.id`). `position_plan_join` view exists, plan_id-first (`store/position.go:352-357`). **Last 7 days: 5/20 eligible closed positions unresolvable = 25% (n=20)** — ids **566, 571, 580, 584, 586**, all `source=reconcile` with empty plan fields; legacy 4-key fallback 0 hits. All-time 462/513 (90%); since 08-01 168/219 (77%). The version leak is moot for plan_id-stamped rows (trader-scoped plan_id `store/plan.go:89-91`); the live hole is the **reconcile-materialized entry path** (`trader/ninjatrader/reconcile.go:405-421` writes no plan fields; only armed-fill lineage stamps). The 08-27 "37%" was a different method/window (`2026-08-27-mega-research-mnq.md:31`). | [DB] [CODE] [DOC] |
| F2 | move_stop dead wire for non-signal entries | Narrowed, not gone: GAR-F1 (`84543213`, 08-28 10:43) added the DB `EntryOrderID` fallback (`tcp_trader.go:587-604`). `no open entry to move the stop`: 08-25 ×20, 08-27 ×4, **0 since 08-28**; 08-28 13:20:09 a trailing move succeeded. A reconcile-materialized manual open with `EntryOrderID=""` still returns the error — [CODE]-only, UNVERIFIED live. | [RUNTIME] [CODE] |
| F3 | min_sl authoring WARN → arm → later `gate changed: min_sl` cancel | **Still live.** WARN site `trader/auto_trader_planner.go:1406-1417` (`⚔️ arm feasibility: %s (WARN — write proceeds; the gate-at-arm chain enforces)`, text `plan_doc.go:521`); cancel site `armed_executor.go:347-370` (`✕ armed cancel (gate changed min_sl)`). 29 feasibility WARNs since 08-30; **6 cancels**: 08-30 17:36:53 ASIA S4 · 18:41:46 ASIA S2 · 19:35:53 ASIA S3 · 19:45:58 ASIA S3 · 08-31 08:17:51 LONDON S2 · **09-01 02:57:29 LONDON S1**. Ledger rows 12, 18, 23 carry `gate changed: min_sl` (⟂ row 23 quoted in §G1). | [RUNTIME] [DB] [CODE] |
| F4 | Weekly read data-gated (class-32 sibling) | **Confirmed.** `maybeRunWeeklyRead` is called inside `runCycle` (`trader/auto_trader_loop.go:198-202`), which runs only after the bar-close gate and `skipNoNewData` (`auto_trader_clock.go:848-875`); the wall-clock path `evaluateWallClockSessionReads` → `maybeRunSessionReadsAt` contains no weekly reference. Spec Sunday 16:30 (`kernel/weekly_knobs.go:23-24`). Last Sunday 08-30: `cycle_skip=no_new_data` 16:21→16:35, read started **17:01:18**, written 17:07:15 (31 min late). Next proof: Sunday 2026-09-06 16:30 — expect the same lateness. ⟂ And even if moved to wall-clock, the preflight (§C9) would refuse it during the halt. | [CODE] [RUNTIME] [DB] |
| F5 | Class 33 — cutover flat gate has no in-flight-work leg | PART 3 steps 1–7 (`AUDIT-CHECKLIST.md:310-326`): step 4 flat gate = API positions `[]` + DB OPEN=0 + NT8 open-orders ×2 + endpoint. **No in-flight / `replan_in_flight` / planner-read leg.** `:284` "(Class 33 is unoccupied …)". | [DOC] |
| F6 | Checklist gaps | PART 1 entries present: **1–26, 30, 31, 32, 34**; missing **27, 28, 29, 33**. No `27.` entry; `grep -i "netting\|orphan"` → 0 — the class-27 wave (a0c7ff0b, C# + Go) never appended its entry. Header `:10` "THE 26 BUG CLASSES" / `:3` "18 proven" both stale (30 entries exist). | [DOC] |
| F7 | AddOn build_id not bumped for class 27 | `VL_BUILD_ID = "2026-08-30-e7"` (`VLTraderTCPClient.cs:51`, last changed 00d77870); class-27 commit a0c7ff0b changed the file (+81/−1, `// CLASS-27 (2026-08-31 netting-orphan)` at :1917) without touching it. Repo md5 == deployed md5 (3/3); heartbeat `far-side AddOn build_id=2026-08-30-e7`. The handshake cannot prove the far side is current (§B7). | [CODE] [RUNTIME] |
| F8 | Two E7 marker hashes | Both ancestors of dev, linear: `3a38ab9f^ = 3fb19f41`. **3fb19f41 is canonical** (changes `deploy/RELEASE` → 59dc9460…, `web/src/guide/types.ts`, park report +57); 3a38ab9f only appends +11 lines to the park report; RELEASE identical at both. Agrees with `2026-08-31-eod-verification.md:72`. | [CODE] |
| F9 | ARMED_TEST_SEAM | **ON.** `.env` has one `ARMED_TEST_SEAM=` line resolving true; boot `⚔️ armed_orders=on place_band=100t stale_working=15m test_seam=ON arm_rr=2.0 …`; code default OFF, `Sim101`-gated (`armed_executor.go:1339-1364`). A live order-placing debug seam is on in production `.env`. | [CONFIG] [RUNTIME] [CODE] |
| F10 | Orphan lineage (`-sl`/`-tp` ids dropped) | Still true. C# names legs `signalId + "-sl"/"-tp"` (`:1703-1711`), strips the suffix before sending `signal_id` (`:1264-1269`, `:1470-1478`) but also sends `order_name` intact; Go `OrderUpdatePayload.OrderName` (`tcp_framing.go:149-151`) has **no consumer**; close-sync owner lookup is by (account, symbol, side) (`close_sync.go:116`). | [CODE] |
| F11 | Ratchet-ack logging | Still intent-only: `📈 trailing_moved … stop ratcheted` logs when `SendMoveStop` (a bare `WriteFrame`, `tcp_server.go:1067-1080`) returns nil (`auto_trader_trailing.go:183-193`, `tcp_trader.go:629-641`). C# emits `SendAck("move_stop")` / `"move_stop_error"` (`:1682`, `:1657/:1678`; no-live-bracket → warn, no ack `:1642`); Go's `case FrameAck:` (`:1744-1753`) only stamps `lastAckTime` + seq-verify — **the ack exists on the wire and is discarded**. | [CODE] [RUNTIME] |
| F12 | Working-order snapshot frame absent | Confirmed: 26 Go frame types (`tcp_framing.go`), 11 C#-emitted; `positions` is a position snapshot; no order-list frame on either side. | [CODE] |
| F13 | Token-footer % (chars not tokens) | **Not reproduced at the located site.** `store/strategy.go:2194-2196` chars→tokens `/4` (CJK `/2`), `:2288-2293 total := subtotal*115/100; pct := total*100/limit` over estimated tokens → `TokenEstimateBar.tsx:11 usage_pct`. Other chars/4 sites (`auto_trader_planner.go:1277-1281`, `handler_plan.go:2214-2224`) are log/cost-only. Which "footer" the defect meant is UNVERIFIED. | [CODE] |
| F14 | Facts not stored with rejected prompts | Confirmed: `planner_rejected_prompts` columns `id, trader_id, trade_date, session, prompt_hash, attempt, reject_reason, prompt_text, created_at` (`store/planner_rejected.go:13-23`, cap 20); no facts/snapshot column — facts survive only as prose in `prompt_text`. | [CODE] [DB] |
| F15 | No manual/system flag | `DISTINCT source` = `system 563, reconcile 11, e7_farside_test 3, armed_entry 5` — **no 'manual'**; `Source` is a free string `default:system` (`store/position.go:159`); writers: `"sync"`, `"armed_entry"`, `"reconcile"`, `"snapshot"`. | [DB] [CODE] |
| F16 | Journald flood | Go rate-limit shipped `871583a9` (08-27 18:06): order_update → DEBUG, 1-in-500 sample, 1-line/min summary (`armed_executor.go:930-983`). journald drop-in `/etc/systemd/journald.conf.d/vl.conf`: `SystemMaxUse=2G`, `RateLimitIntervalSec=30`, `RateLimitBurst=200000`; `journalctl --disk-usage` → **1.9G (at cap)**. Per-day lines: 08-26 **0 (`-- No entries --`)**, 08-27 5,401,570 (+907,173 suppressed), 08-28 95,727, 08-29 35,299, 08-30 79,448, 08-31 209,108, 09-01 54,830 (to 16:40). Oldest entry `Aug 27 13:37:42`. **The journal is now a rolling ~5-day window**; file logs (`vl_2026-08-26.log` 61,872 lines) are the only 08-26 source. | [CODE] [CONFIG] [RUNTIME] |
| F17 | 334 June/July positions untraceable | `COUNT(*) WHERE entry_time < 2026-08-01` = **334** (May 8 / Jun 324 / Jul 2); 333 `pnl_corrected IS NULL`. NT8 trace dir earliest `trace.20260801.00000.txt`. Noted only in `2026-09-01-manual-system-segregation.md:24`; not in any UI. | [DB] [DOC] |
| F18 | DeepSeek-2 old key revoked? Binnie re-clone sent? | **Both UNVERIFIED.** `.env` has 0 DEEPSEEK-named variables (keys live encrypted in `ai_models`; two DeepSeek rows, both `updated_at 2026-08-29 22:38:46 UTC`). `2026-08-30-pre-livefire-verify.md:263` says DeepSeek 2 still held the old key; `2026-08-31-eod-verification.md:71` says unverifiable. No artifact records a re-clone message. Would verify: the DeepSeek console key list; the partner repo's remote state / owner statement. | [CONFIG] [DB] [DOC] |
| F19 | 08-31 canon laws in CLAUDE.md? | **lock-liveness: in CLAUDE.md (:147).** canonical-casing, no-fabricated-values, sample-id: **NOT in CLAUDE.md, NOT in the checklist** — only in wave reports (`2026-08-31-netting-orphan-wave.md:15`, `2026-08-31-0a2-ledger-honesty.md:84`, `2026-08-31-eod-verification.md:61`). corrected-column: NOT in CLAUDE.md; present as checklist **R7** (`:301-303` "pnl_corrected everywhere + excluded_null_pnl for the 354 legacy NULL rows" — the DB count is 357). | [DOC] |

### F — Surprises
1. `position_plan_join` comment says unresolvable rows "keep plan_id='UNRESOLVABLE'"; in the DB they carry `''` (5/5) — any consumer testing for the sentinel misses them.
2. All five 7-day unresolvable positions are `source=reconcile` — the AI market-entry path materialized through reconcile is structurally plan-blind; this is the current attribution hole, not the 08-27 version leak.
3. `ARMED_TEST_SEAM` ON in production `.env` while the code documents "default OFF".
4. AddOn `build_id` unchanged across the class-27 change → the E7 capability handshake cannot distinguish the far-side build.
5. Journal retention: 08-26 gone, 907k suppressed on 08-27, disk at 1.9G/2G.
6. Checklist header "26"/"18 proven" vs 30 entries; class-27 never appended — an AUDIT-PLAYBOOK LAW miss.
7. Only 3 `armed_orders` rows carry `gate changed: min_sl` vs 6 log cancels — the unique `(plan_id, scenario)` index means later states overwrite `state_reason` [B].
8. C# emits `move_stop`/`move_stop_error` acks that Go discards.
9. `OrderUpdatePayload.OrderName` parsed, never read.
10. NULL `pnl_corrected` rows 357 vs checklist R7's 354.
11. Last Sunday's weekly read fired 31 min late by exactly the class-32 mechanism; class 32's fix does not include the weekly path.

### F — UNVERIFIED
- F2 residual manual-open path (code-only). F3: the working placement between 17:09:18 REFUSED and 17:36:53 cancel (08-30 ASIA S4) not fetched. F4: next Sunday's behavior. F7: DLL contents (inferred from mtimes + the 00:43:30 hello). F13: which "token footer". F18: both items.

---

## SECTION H — CONTRADICTIONS (two sources disagree)

| # | source A | source B | what is true | evidence |
|---|---|---|---|---|
| H1 | Guide `settings.ts:38-39` "⭐ 0.3 — LIVE since 2026-08-28 11:59"; boot `proximity=cfg(… retuned 0.3)` | DB `proximity_filter_atr: 1`; runtime bands ±148…±265 pt | **1.0 is live**; Guide + boot literal are wrong. The knob NAME (`_atr`) is a misnomer — unit is dATR (`levels_score.go:414`); the Guide's unit wording ("daily-range proxy") is correct, its value is not | §D10 |
| H2 | `levels_score.go:374-377` comment "ANCHORS keep the original ladder — 'anchors no-decay' per spec" | `:434 fm := freshMult(fRaw)` applied to every non-zone kind; writer `auto_trader_levelstate.go:96-149` has no kind exemption | **Anchors decay** (1.0/0.8/0.6/0.5); the comment lies | §D12 |
| H3 | client chip formula `SessionPlanCard.tsx` (at fef656a4/d4b38604) `replansSpent = noTradeVersion ? max(0, noTradeVersion−2) : max(0, version−1)` | API `replans_left = ReplansLeftFrom(version, baseline, cap)` | Two inferred formulas that disagree after any reset (baseline ≠ 1) and both count wake/dormant rows as spends. ⟂ `ec6632f9` (merged to dev 16:5x, not deployed) replaces both with a recorded counter; **live surfaces still run the old pair** | §E1, §B6 |
| H4 | Master plan H3 / dispatch premise "live EOD flat = 14:30 CT (session_end − 15)"; `store/strategy.go:996-1000` comment "nil → default 15" | `DefaultEODFlatOffsetMin = 0` (`:1013`, R-A15); boot `flat 14:45`; registry `FlatCT 14:45`; Guide/FE 14:45 | **14:45 is live**; 14:30 is the last-ENTRY cutoff. The master plan's "14:30" correction is itself wrong against this binary | §D8 |
| H5 | "gate default 65 vs prompt default 60 for unset strategies" (master plan H5, dispatch D18) | `store/strategy.go:75-81 SafeDefaultMinConfidence = 60` shared by clamp and prompt | **Refuted** — aligned to 60 on 2026-08-19; no mismatch in code | §D18 |
| H6 | boot `arm_rr=2.0 (gate-at-arm only; market-entry floor 3.0 unchanged)` | strategy `min_risk_reward_ratio: 2` since 08:13 CT; `📐 R:R eval … (min 2.00)` from 15:08:00 | **Both floors are 2.0 now**; the boot text (printed once inside a `sync.Once`) is stale after the Studio save | §D6 |
| H7 | boot `🧬 plan lifecycle: … flip/death→dormant+auto-rearm (version unchanged, budget untouched)` | plans table: NY v2 `dormant:death`, v3 `dormant:flip`, v4 `rearmed` are **separate version rows**; API `replans_left 0` with 0 deaths | Dormant transitions themselves are in-place, but the wake reads they permit append versions whose `trigger_reason` is then overwritten by the transition — provenance lost, budget "spent" by inference | §E1, §G1 |
| H8 | class 32: "Halt-fired authoring from last stored bars is RULED CORRECT" (`auto_trader_clock.go:89-97`) | `plannerPreflight` refuses any read with newest 1m bar older than 600 s (`feedwatch.go:109-131`) | **The preflight wins**; the halt read is unreachable (§I4-1) | §C9 |
| H9 | `🚨 CLOCK EARLY-WARNING … fix WSL2 time-sync NOW` ×4 today; `⚠️ clock-drift DETECTED (no entry block): local clock is BEHIND the feed by 210s` (02:52:43) | `timesync{NTP=yes NTPSynchronized=yes}`, `ntp_offset=+108.137ms` on the same lines | The clock is fine. Both signals measure `now − (freshestBar + interval)` against the **forming** bar (`clock_health.go:85-89` +60_000; `clock_drift.go:33-45` newest 1m-else-5m + interval), so negative "drift" of up to one bar interval is structural. Side effect: T1 news windows widened by 30–54 s on ~half of reads (`ClockHoldDecision` `clock_drift.go:166-178`) | §G6 |
| H10 | dashboard header `account.total_pnl` → **0.00** (NT8 realized+unrealized while flat); `daily_pnl` permanently 0 | PositionHistory day total → **+212.00** (pnl_corrected) | Two P&L numbers on one screen; the ledger is right | §G5, §E9 |
| H11 | AI prompt `Total Trades: 220 · Total PnL: -203.68 USDT` (GetFullStats, NULL→raw) | strict corrected set `105 trades, +304.32`; `GetPositionStats` −242.68 (trader-global) | The model is fed a coerced figure; corrected-column law violated on the prompt surface; a `+0.00 (+100.00%)` phantom row (579, unresolved) is in the same list | §C2, §E5 |
| H12 | journal `09:51 NY v3 DORMANT — death-condition …` | DB `v3 trigger_reason = dormant:flip`, `v4 = rearmed` | In-place overwrite by `UpdatePlanLifecycle` (`store/plan.go:286-293`); the journal is the only record of the original trigger | §E1 |
| H13 | checklist header "THE 26 BUG CLASSES" / "18 proven"; R7 "354 legacy NULL rows" | 30 numbered entries; 357 NULL rows | Header and R7 stale; classes 27/28/29/33 missing, class 27 never appended | §F6, §F19 |
| H14 | `position_plan_join` comment: unresolvable rows carry `plan_id='UNRESOLVABLE'` | DB: `''` | Sentinel never written | §F1 |
| H15 | Guide `tradingDay.ts:110` NY read `08:25` | registry/boot NY read `08:00` | Guide stale (class-18 drift on the running rev) | §D8 |
| H16 | `store/strategy.go:953/:959` "wake default 10" | `DefaultWakeMinIntervalMin = 30` (`:1349`); runtime `wake_min_interval_min (30m)` | 30; comments lie | §D22 |
| H17 | registry `session_registry` `enabled:false` for ASIA and LONDON | reads fire for all three (per-session strategy `enable:true`) | Runnable via override; a registry-only reader would conclude the sessions are off | §C3 |
| H18 | boot `roll=pending AddOn ACK` | `/api/status roll: resolved MNQ SEP26, expiry 2026-09-18, window 2026-09-15` | Timing artifact (boot line precedes the hello) | §B1 |
| H19 | dispatch F1 premise "~37% unresolvable" | 7-day window 25% (n=20); different method | Both true of their windows; the join hole moved from version leak to reconcile-materialized entries | §F1 |
| H20 | boot `📊 Using CoinAnk API for all market data (WebSocket cache disabled)` | data rides the NT8 BarCache | Cosmetic stale line | §B1 |
| H21 | 0A-2 report "hidden rows excluded from the list" | `GetRecentTrades` applies no exclusion; prompt shows row 579 | Which surface the report meant is untraced; the prompt list is NOT excluded | §C2 |
| H22 | boot `capacity=1 unless max_contracts_per_order raises` | resolved 2 (`max_contracts_per_order: 2`) | Text is conditionally true but the boot never prints the resolved number; one 2-lot fill exists (574) | §D7 |
| H23 | `0C` boot `conditions: … shadow [breakout_retest, fvg_entry]` (enforced at the arm seam) | grader still weights `KindIFVG` = FVG weights and `fvg_entry` detector `on` at boot | Consistent by design (author + score, never place) — recorded so nobody reads the detector line as "fvg live" | §D12 |
| H24 | D14–26 sub-agent: "no vl-class35 worktree exists" | `git worktree list` → present; lock re-acquired 16:40:57 | Agent error, corrected (process note) | §D |
| H25 | class-32 read scheduling (`ReadCT 16:30`) | `plannerPreflight` + `weekly` path both still data-gated in effect | Two scheduled reads (ASIA daily, weekly Sunday) cannot author inside the halt | §C9, §F4 |

---

## SECTION I — VERDICT

### I1 — Component → research verdict → live state → wave → status

| component | research verdict | live state (resolved) | wave | status |
|---|---|---|---|---|
| Entry confirmation (5-rule vocab, touch fades, BD 1 close+disp) | adopt | LIVE as researched; enforcement fires (`fade_requires_touch` ×4 today) | entry-mechanics | **LIVE AND PROVEN** |
| Class 27 netting-orphan (6 arms) | ship | one-live-arm guard fired 3× today; sweep/desync/reconstruction/split-cap untriggered | class 27 | LIVE; 1/6 PROVEN, 5/6 UNPROVEN (event: a netting flat / store-open-vs-broker-flat / netting close / >2-leg split) |
| Ledger honesty 0A/0A-2 | pnl_corrected everywhere | strict at 3 sites; coerced at 5; raw at 2 | 0A-2 | LIVE BUT PARTIAL — **corrected-column law violated on the AI prompt** |
| Open−30 reads | 16:30/01:30/08:00 | registry DB row + boot line; LONDON 01:31:30, NY 08:00:01 fired | 08-31 ruling | **LIVE AND PROVEN** (ASIA read fires but cannot author — see below) |
| Class 32 wall-clock reads | fire on wall-clock in the halt | fires (16:31:05 …) but preflight refuses | class 32 | LIVE, **FAILED on intent** |
| Weekly render | neutral+date, no "none", no strike | planner/executor/chip all correct | weekly | **LIVE AND PROVEN** |
| Repair retry | RETRY_MODE=repair | repair prompts 13–24% of full-author (n=22); 59% unparseable fallback | speed wave | LIVE AND PROVEN (with a 59% fallback rate) |
| Streaming + split deadlines | SSE, 30s idle / 600s | proven incl. two 600s ceiling hits | speed wave | **LIVE AND PROVEN** |
| Telemetry | ttfb/T1/T2/reasoning/rejected store/retries | all present; `retries=` is attempt number | speed wave | LIVE AND PROVEN (label caveat) |
| 0C shadow map | fvg_entry + breakout_retest shadow | resolved 7 live / 2 shadow; no shadowed scenario authored since 0C | 0C | LIVE BUT UNPROVEN (event: a plan authoring `fvg_entry`/`breakout_retest` → refusal line + E8 counterfactual row) |
| Class 34 validator hints | 6 sites legal + live | boot guard; live rejects carry the hint; suffix absent from repair prompts | class 34 | LIVE AND PROVEN (gap: repair path) |
| Research archive merge | 38 reports + index | verified; index at `research/INDEX.md` | docs | LIVE AND PROVEN |
| Stop floor 1.5–2.5×ATR | raise | 1.0 [C] | 0B | **IGNORED** (queued in plan, not shipped) |
| Stop anchoring to structure | anchor | none exists; clearance leg never fired | 0B/I5 | **IGNORED** |
| BE+40 / ATR trail | suspend | LIVE and firing (2 BE, 8 trail moves today) | 0B | **CONTRADICTED-BY-LIVE** (suspension not shipped; dead wire narrowed by GAR-F1) |
| ARM_MIN_RR 2.0 | 2.0 | 2.0 both floors (AI floor since 08:13) | ruling | **LIVE** (boot text stale) |
| Size 1 contract | 1 | clamp 2, practice 1 (one 2-lot fill exists) | 0B | CONTRADICTED-BY-LIVE (cap), practice aligned |
| EOD flat | 14:45 (R-A15) | 14:45 | R-A15 | LIVE AND PROVEN (`session ended (EOD flat)` cancel 14:45:06 today) |
| Lunch gate vs ny_pm | reconcile | overlap 13:00–13:30 present | P4 admin | IGNORED (owner decision pending) |
| Killzones NO PREMIUM | demote advisory | still weighted + graded | — | **IGNORED** |
| Proximity 1.0 dATR | measure | 1.0 live; Guide/boot say 0.3 | GAR-F2 | LIVE; docs CONTRADICTED-BY-LIVE |
| min_side_levels | delete | deleted | 08-31 | LIVE AND PROVEN |
| Grader multipliers | neutralize (3A) | all invented values live; 6 spec divergences intact | 3A | **IGNORED** (queued) |
| Seated cap 6/8 | cap | 11–12 seated per read | 2A | QUEUED |
| Cluster tolerance 3pt | keep, re-unit | fixed const | 3C | QUEUED |
| Touch band 16t | calibrate k×Δ | fixed; three touch definitions | 1C | QUEUED (in flight) |
| Flip hygiene (buffer ≥1.0, two-stage, breaker, no cascade) | ship 4A | 0.5; parks not flips; no breaker; no cascade | 4A | **CONTRADICTED-BY-LIVE** (all four) |
| Weekly bias shadow | warn, closed weeks, sticky-nil fix | as researched | weekly | LIVE (DOA guard UNPROVEN until a Sunday) |
| min_confidence | 60 | 60; mismatch fixed | — | LIVE |
| HTF veto cross | cross | cross | ruling | LIVE (unfired since 08-30) |
| Sweep depth / BOS repricing | fix | zero depth; never reprices | post-3C | IGNORED (not docketed) |
| stale_reeval 0.25 | leave | 0.25 untouched | — | LIVE AND PROVEN (refusal 15:08:00) |
| FAST_MARKET_ATR 1.5 / cadence 30m | keep / fix cadence | 1.5 / 30 unchanged | — | LIVE (cadence finding IGNORED) |
| Monte Carlo rig | build | absent | 1E | QUEUED |
| MAE/MFE intrabar | build | 1m proxy after close | 1A | PARTIAL / QUEUED |
| Candidate pool + outcomes | build | absent | 1B | QUEUED |
| Per-condition expectancy | build | absent; attribution 25% blind | 1D (1B-PRE) | QUEUED |
| Replan budget | counters record events | live infers; both chains at 0 with 0 spends; fix merged to dev (ec6632f9), not deployed | class 35 | **LIVE DEFECT — fix STAGED** |
| Guardrail entries count | exclude test/dup rows | excludes nothing | hygiene | LIVE DEFECT (latent today) |
| Attribution join | fix | 25% blind (n=20), reconcile path | 1B-PRE | LIVE DEFECT |
| E8 shadow A/B | counterfactual source | 20/82 rows sign-broken; no reader | E8 | LIVE DEFECT (new) |
| Touch ordinal | record | process-memory, resets on restart | E8/1B | LIVE DEFECT (new) |
| Armed-fill lineage stamp | complete on fill | 584/586 never stamped | class 27 / GAR | LIVE DEFECT (new) |

### I2 — Buckets
- **LIVE AND PROVEN (this audit, from live/DB):** entry law; open−30 reads (LONDON, NY); weekly render; repair retry; streaming + 600s ceiling; telemetry; class-34 hints on live rejects; archive merge; one-live-arm guard; EOD flat 14:45; min_side_levels removal; stale_reeval refusal; BE/trail firing (against the plan); ledger close-path stamp (6/6 today); guardrail soft-audit lines; class-32 read firing on wall-clock.
- **LIVE BUT UNPROVEN (with the proving event):** C# netting sweep (a netting flat in NT8 → `netting-flat bracket sweep cancelled N leg(s)`); Go desync cancel (store-open vs broker-flat → `🧹 class-27 desync`); exit reconstruction (a netting close → reconstructed exit ≠ entry); split-leg refusal (>2-leg split arm); 0C arm-seam refusal + E8 counterfactual (a plan authoring a shadowed condition); weekly DOA guard + Sunday 16:30 weekly read (Sunday 2026-09-06 16:30 CT — expected to be 31 min late again, F4); HTF cross veto (a 1h+4h opposing setup); min_confidence gate (a 50–59 confidence open); cluster tolerance (no boot print); leg capacity 2 at runtime.
- **QUEUED (plan wave exists, nothing shipped):** 0B stop floor/anchoring/BE-trail suspension (partly CONTRADICTED because BE/trail run); 1A intrabar; 1B pool; 1C band (in flight); 1D per-condition; 1E Monte Carlo; 2A cap/render/slim; 3A neutralization; 3C construction; 4A flip hygiene; 1B-PRE attribution.
- **IGNORED (research delivered, nothing done, not docketed):** killzone NO-PREMIUM (still weighted and graded); wake cadence bottleneck; sweep penetration depth + BOS repricing; lunch/ny_pm overlap; confirm-cost / honest-wait post-law recompute; stop anchoring; the Guide/boot "0.3" proximity fiction; checklist class-27 entry; canon laws not in CLAUDE.md.
- **CONTRADICTED-BY-LIVE (research says X, system does not-X, no ruling):** BE+40 and ATR trail LIVE (research: suspend); flip buffer 0.5 (≥1.0) + no two-stage/breaker/cascade rule; size clamp 2 (research: 1); grader multipliers live (research: neutralize) incl. anchors decaying against the spec; master-plan "14:30 flat" vs live 14:45 (the plan is wrong, not the binary); Guide 0.3 vs live 1.0; boot "market-entry floor 3.0" vs live 2.0.

### I3 — The five most important things, in order
1. **The ASIA read cannot author inside the halt** (§I4-1): class 32 moved the read to wall-clock and the preflight refuses it until 17:00; the fix that was declared verified last night produces the same planless open by a different gate, and the weekly read has the same shape (F4). One ruling needed: either the preflight exempts halt-fired scheduled reads (the class-32 contract) or the read time moves to open+1 — not both silent.
2. **Replan budget at 0 on both chains with zero spends** (class 35) — the fix is merged to dev (ec6632f9) but not cut over; until it is, any machine death fail-closes the session. Cutover needs a flat window, the in-flight check, and the owner's GO (A3).
3. **The corrected-column law is broken on the surface the model reads** — `GetFullStats` coerces 115 NULL rows to raw (−508.00), the prompt says `Total PnL −203.68 (220 trades)` where the strict truth is `+304.32 (105)`, and an `unresolved` row renders `+0.00 (+100.00%)`. Five aggregators + `GetRecentTrades` + `agent/tools.go` need the `IS NOT NULL` + reason predicate (or an UNRESOLVED marker), and `/api/plan/trades` emits raw.
4. **Attribution is 25% blind on the reconcile path and today's two armed fills never got stamped** (584, 586: `plan_version 0`, grade D, `;stamp_pending`) — 1D/1B-PRE and every per-condition table depend on this; the class-27 lineage repair only runs inside `reconcileOnce`.
5. **Instrumentation that infers instead of records**: E8 short-side sign bug (20/82 rows garbage — the Sep-9 confirm-rule verdict would read poison), touch ordinals reset on every restart (3 today), refusal counters log-only/deduped, gate-block tallies in-memory (wiped 3× this session-day), `UpdatePlanLifecycle` destroying `trigger_reason`. Counters record events; they do not infer them — the same disease, five more instances.

### I4 — What endangers money / trading TODAY (also at the top of this file)
1. No ASIA plan possible before ~17:08 CT (preflight vs halt-fire) — planless open; entries refused in `strict` mode until a plan lands.
2. Both live chains at `replans_left 0` with zero spends — a machine death tonight fail-closes the session (NO-TRADE) for a budget never spent.
3. Guardrail master OFF while `max daily trades would trip (today=6, max=3)` fired 263 times today — owner's setting, nothing enforces the cage.
4. BE+40 and the ATR trail are LIVE and firing (2 BE, 8 trail moves today) against a research verdict to suspend — an unmeasured exit mechanism managing every open trade, with the ratchet ack discarded (F11) so a rejected move would be logged as success.
5. `ARMED_TEST_SEAM=on` in production `.env` (SIM-gated) and `max_contracts_per_order=2` (a 2-lot fill already happened, 574) — both widen the blast radius beyond the "1 contract, no debug seams" posture the plan assumes.

---

## CLOSEOUT NOTES

- **Read-only compliance:** no file in the main tree, no config, no knob, no DB row, no order, no restart was touched. The only writes: this report on `docs/full-system-audit-0901` (worktree `~/vl-audit`), a `.backup` DB copy + JWT token in the session scratchpad (outside the repo), and the GIN journal lines this audit's GETs produced.
- **What the owner will still see wrong on screen:** dashboard header P&L `0.00` beside a +212.00 day total (H10); plan cards saying `0 re-reads left` on chains that spent nothing (H3, until ec6632f9 is cut over); the Guide's proximity "⭐ 0.3 LIVE" and NY read "08:25" (H1, H15); the boot block's "market-entry floor 3.0", "retuned 0.3", "capacity=1" (H6, H1, H22); `Total PnL −203.68` in every stored executor prompt (H11); four red `CLOCK EARLY-WARNING` lines a day that are artifacts (H9); an ASIA card that stays "not found" until ~17:08 every day (I4-1).
- **Rollback command (for reference, NOT run):** `cp ~/vl/vl-bin.prev.boot ~/vl/vl-bin && echo ebc37e01d7dd5f19c0e0f0ffa962388e12988f58 > ~/vl/deploy/RELEASE && kill -9 1625428` (then GUIDE_BUILT_REV would drift — see §B3).
- **A9 commit-ref URL — NOT produced.** `git push -u origin docs/full-system-audit-0901` from `~/vl-audit` was denied by the session's auto-mode classifier (no workaround attempted). The branch exists locally with this report committed; the owner publishes it with `git -C ~/vl-audit push -u origin docs/full-system-audit-0901`, after which the raw URL is `https://raw.githubusercontent.com/johnwick2921-cyber/nofx/<sha>/docs/superpowers/reports/2026-09-01-full-system-audit.md` (curl it for 200 before citing — this has 404'd twice before).

---

## §G4 ADDENDUM — THE 17:00 REOPEN, CAPTURED LIVE [RUNTIME]

- 16:57:05 / 16:59:05 CT: last two halt-fired reads, both refused (`stale_bars_3425s`, `stale_bars_3545s`); newest 5m bar still 15:55 CT (age 62–64 m). ASIA card `found:false reading:false`.
- **17:01:05 CT — first cycle after the reopen (`AI decision cycle #238`)**: preflight passed; `🧠 planner model: empty binding → using primary, pinned "deepseek-v4-pro"` → `🕰 clock-hold: T1 news windows widened by |drift| 54155ms for 2026-09-01 ASIA (F6)` (the forming-bar artifact, §H9, now inflating the ASIA plan's news windows by 54 s) → `📡 [MCP …] Request URL (stream idle=30s): https://api.deepseek.com/chat/completions`. API `?session=ASIA` → `found:false, reading:true, replan_in_flight:false`. The executor also ran (`📝 Decision record saved … cycle=25985` 17:01:22) under `strict` mode with no plan.
- 17:01:05: `📊 gate-block summary (46 total): arm_authored=18 superseded_wait=7 level_burned_retouch=5 decline_fresh_met=4 clock_skew_observed=3 min_sl_gate=2 night_transition=2 plan_matched=2 planner_fail_closed=2 stale_reeval_refused=1` — the session-day rollover summary fired on the first loop after 17:00 (§E6 confirmed: it exists only when a loop crosses 17:00; the counters then reset).
- 17:01:06: `🕰 clock-health [session-roll:ASIA] go=17:01 CT … nt8_last_bar=17:02 CT … drift_ms=-53999 timesync{NTP=yes NTPSynchronized=yes}` → `🚨 CLOCK EARLY-WARNING [session-roll:ASIA]: |drift| 53999ms …` — fifth artifact warning of the day, 6 s after the reopen, with NTP healthy.
- **Net: the ASIA session opened at 17:00 with no plan; the read began 31 minutes after its 16:30 schedule and the plan can land no earlier than the planner's wall time (50–600 s today).** The outcome of this read is quoted below once captured.
- 17:03:10 `🗺️ seated 24/622 in-band levels (proximity band ±377pt, 24 of them retained)` — tonight's proximity band is ±377 pt (K=1.0 × a larger daily-range proxy after today's move; §D10), and the pre-planner seating is 24 candidates per read (the planner then writes 11–12; §D13).
- 17:04:55 `📊 level_stats: 2026-08-31 evaluated 32 seated level(s) (total rows 145) — forward validation accumulating` — the nightly level_stats job ran for the previous session-day (§E8).
- **17:10:09 — attempt 1 finished after 543.6 s** (`📊 AI call complete (stream): completion=25619 prompt=9494 finish_reason=stop reasoning_chars=84446 ttfb_ms=655 wall_ms=543621`) and was **rejected**: `📐 planner attempt 1/3 rejected: S1 breakdown_continue: a close came back across 29130.50 — the breakdown is void; author a `reject` play instead (do NOT combine condition names; `reject_retest` is not a valid condition)` (the class-34 hint, live) → `🧩 planner attempt 2/3 repair: prompt ~1120 tokens (full-author ~6343 tokens)`. At 17:12:45 CT the repair was still running and the ASIA card still read `found:false, reading:true` — **12 min 45 s after the open, no plan**. Final outcome below.
- **17:15:51 — attempt 2 (repair) finished after 342.4 s and was rejected** by the entry-law seam: `📐 planner attempt 2/3 parse/schema rejected: scenario[0].confirm.rule "1x5m_close" — fade_requires_touch (a reject fade enters on the touch at the level, never on a close-confirm …)` → `🧩 repair returned unparseable output — falling back to a full re-author next attempt` → `🧩 planner attempt 3/3 reauthor+block: prompt ~6470 tokens (full-author ~6343 tokens)`. This is the §C5 pattern (repair fallback rate 59% today) and the §C10 gap in one read: the repair prompt does not carry the `Valid conditions` line, and the model answered the attempt-1 `breakdown_continue → reject` hint with a `reject` play confirmed on a close instead of a touch. At 17:16:59 CT the ASIA card still read `found:false, reading:true` — **17 minutes after the open, no plan**; attempt 3 is the last before the scheduled read fail-closes into NO-TRADE (`failClosed=true` for scheduled reads, `trader/auto_trader_planner.go:832-833`).
- **17:23:14 — attempt 3 (full re-author) rejected → FAIL-CLOSED.** `🚨 PLANNER FAIL-CLOSED 2026-09-01 ASIA: S2 breakdown_continue: a close came back across 29130.50 — the breakdown is void; author a `reject` play instead (do NOT combine condition names; `reject_retest` is not a valid condition) — writing a NO-TRADE …` → `🗓️ PLAN written 2026-09-01 ASIA v1 (model deepseek-v4-pro, lifecycle no_trade, prompt 1338de68604e, ai_config a28d83f159084145)`. [DB] `plans` `2026-09-01:ASIA v1 planner_fail_closed no_trade 17:23:14`. [RUNTIME] `/api/plan/today?session=ASIA` → `found:true version:1 lifecycle:no_trade trigger_reason:planner_fail_closed replans_left:4 replan_cap:4`; doc = the Go stub (`S0 hold long B`, bias neutral/low, 12 levels carried). **Net for tonight: ASIA opened planless at 17:00 and sits out on a NO-TRADE marker from 17:23:14 — three planner attempts (543.6 s + 342.4 s + ~440 s) each rejected the same `breakdown_continue`-void class; the halt-refused 16:30 read cost the 31 minutes that would otherwise have absorbed one of those retries before the open.** The card can be re-opened only by an owner reset (`the owner reset is the escape hatch`, `auto_trader_reread.go:73`) or a level-event wake (`auto_trader_planner.go:281-283`).

### CUTOVER DURING THE AUDIT — CLASS 35 WENT LIVE AT 17:24:00 CT [RUNTIME]
- `Sep 01 17:23:55 systemd[1]: vl.service: Main process exited, code=killed, status=9/KILL` → `17:24:00 Started vl.service` → `🔐 BOOT INTEGRITY OK — rev ec6632f9de41 · built 2026-09-01T21:54:27Z · expected ec6632f9de41 · goldens PASS` → new boot line `🧮 replan budget: recorded-counter (class 35) — spends: death_replan, owner_reread · free: <S>_scheduled_read, level_event, structure_mss (incl. fast-market), owner_reset, dor…`. New PID **1908258** (started 17:23:59). `go version -m vl-bin` → `vcs.revision=ec6632f9… vcs.modified=false`; `deploy/RELEASE` = `GUIDE_BUILT_REV` = `ec6632f9…` (marker `b51f8f03 deploy: class-35 marker — RELEASE=ec6632f9 + GUIDE_BUILT_REV=ec6632f9`); class-35 report `155cd4dc` on dev; rollback slot `vl-bin.prev.boot` now = `fef656a4…` (the binary this audit measured). Lock: `owner=hoang pid=1860416 expiry=2026-09-01T23:30:00-0500 task=class35-cutover acquired=17:13:31` — `kill -0 1860416` → **ALIVE**. Main tree porcelain 0, dev tip `155cd4dc`.
- **Supersedes** §B1–B3 (live rev/PID/rollback), §B6, and the "fix merged, not deployed" statements in §E1/§H3/§I: the recorded-counter budget is now live; the ASIA v1 NO-TRADE row written 49 s BEFORE the cutover shows `replans_left 4` under the new code (v1 = 0 spends). Everything else in this report was measured on `fef656a4` (00:43:30 → 17:23:55 CT) and stands as the pre-cutover baseline. The cutover happened while a scheduled ASIA read had just fail-closed (17:23:14) and no read was in flight — rule A6 satisfied by timing; the 16:45–17:10 CT window (A7) was respected (kill at 17:23:55).

---

## ADDENDUM 2026-09-03 — §D-9 chain upgraded [B] → [A]; peer-session resolution

*(Additive. The baseline text above is unedited and remains the `fef656a4` snapshot of 2026-09-01. This addendum records what a later session proved on a later rev, and one correction of my own.)*

### A1 — The §D-9 mechanism is confirmed, and it is the deterministic one

§D-9 offered this chain at **[B]** (inferred): "`StampArmedLineageIfMatched` ran before the ledger row was `filled`; the fill handler's `GetOpenPositionBySymbol(side=\"short\")` (`store/position.go:616`, case-sensitive `side = ?`) missed a row stored `SHORT`; `RepairArmedLineage` runs only inside `reconcileOnce`."

Session **vl-06** measured this on the live store 2026-09-03 and reports it **[A]**:

- `armed_orders.side` is always lowercase (long 19 / short 17); `trader_positions.side` is overwhelmingly uppercase (LONG 280 / SHORT 304 against long 1 / short 2).
- Against position 591: `side='short'` matched **0** rows, `side='SHORT'` matched **1**. `=` on a plain TEXT column is case-sensitive.
- The fill handler passes the ARMED row's lowercase side, so the fill-time lookup **could never** find an armed-entry position regardless of timing.
- Determinism is the tell: **10 of 10** filled rows at `fill_quantity=0`. A race is not deterministic.

Fixed with `UPPER(side)=UPPER(?)` at that lookup and two siblings — the USDT retry and `GetOpenPositionByAccountSymbol`, which close-sync routes through, where the same compare loses a priced close. Commits: **95e9a4d0** (ordering/materialization leg) and **664ab6b7** (case-fold leg), on `fix/invalidation-wired`. Not deployed at time of writing; awaiting the owner's GO.

Both mechanisms are real. The materialization race (armed row 35: filled 09:03:53, position 591 materialized 09:05:14, an 81 s gap) got there first and **masked** the deterministic one.

### A2 — Correction: 584 and 586 are row IDs, not a ratio

In cross-session chat I described this finding as "584 of 586 armed fills never lineage-stamped". **That was wrong and it was mine.** 584 and 586 are `trader_positions` row IDs (see the §G5 ledger table, rows 581–586, and §I3-4: "today's **two** armed fills"). The baseline finding is **two** unstamped armed fills on 2026-09-01, ids 584 and 586, out of six closed rows that session-day. The error did not reach this report — only the chat relay — but it propagated into a peer session's draft before being caught there. Recorded here so the archive carries the correction next to the finding.

The **25% blind** figure is a genuine ratio and is a different measurement: §F1, 5 of 20 eligible closed positions unresolvable over 7 days, ids 566, 571, 580, 584, 586. The two lists overlap because they concern the same two rows.

A re-run of the §D-9 column set on the current rev returns **3** unstamped, against this report's baseline of **2** — consistent, and consistent specifically with a small persistent every-day miss rather than an intermittent race.

### A3 — Open, quoted both ways (A12)

vl-06 reports `;stamp_pending` is transient by design, trimmed in `reconcile.go`, and that no row carries it now — so the defect is visible only in `fill_quantity`. §D-9 of this report records `armed_orders` 24 and 28 as **still** `filled ;stamp_pending` when read at 16:40 CT on 2026-09-01, against fills at 08:37:08 and 13:33:06 — carried ~8 h and ~3 h. Both hold if reconcile trimmed them later. Not resolved here; flagged because "transient" is the assumption that makes `fill_quantity` the sole symptom.

### A4 — Probe added to the class-59 checklist by vl-06

1. Ask which **branch** a write sits on, not merely whether the write exists. A write on a path almost nothing takes is worse than a read nobody performs, because it produces a green proof.
2. When a log line names a **cause**, check the code can actually distinguish that cause from the alternatives. `"position row not materialized yet — stamp pending"` prints whenever `pos == nil`, which is true under either mechanism, while asserting the race as fact. It was quoted as live proof of the race and proves only `pos == nil`.

### A5 — §F1's "25% blind" has become three failure states; and the grade-reset predicate keys on the wrong thing

*(Measured 2026-09-03 ~11:40 CT, read-only: `sqlite3 'file:/home/hoang/vl/data/data.db?mode=ro'` + `sed` over two source files. No writes. Prompted by vl-06's observation that late-stamped positions keep a permanent off-plan D.)*

**A5.1 — The reset predicate catches a subset, not nothing [A].** `trader/ninjatrader/reconcile.go` clears the grade so W5 can regrade, but only for `F`:

```go
// armed-fill plan in hand (grade ≠ F is the STEP-7 proof).
if p.Status == "CLOSED" && p.AdherenceGrade == "F" {
    if err := st.Position().SetAdherence(p.ID, ""); err != nil { … }
}
```

`kernel/adherence.go GradeAdherence` grades base **D** for `in.OffPlan || !in.Cited` ("off-plan (no scenario cited)"), then steps down once per penalty — `if in.InNoTrade { grade = stepDown(grade, 1) }`, `if !in.InKillzone { grade = stepDown(grade, 1) }` — over `gradeLetters = []string{"A","B","C","D","F"}`. D is second-to-last, so **one** penalty step takes an uncited close from D to F.

So an uncited close grades **F when either penalty applies, D when neither does**. The predicate is therefore not looking for an impossible value (an earlier characterisation, corrected here) — it is looking for a value that occurs in a *subset*. `RepairArmedLineage` silently succeeds on penalised uncited rows and silently fails on clean ones, which is why spot-checking would not catch it: a sampled F row shows the repair working. [DB] confirmation, all three `plan_version 0`, `source=reconcile`: **566 = F**, **571 = F**, **580 = D**.

The correct fix keys on the *absence of lineage*, not on a grade letter that encodes lineage plus two unrelated penalties. **It must not simply also match `D`** — row **580** is a genuinely uncited close that has earned its D, and a predicate widened to `F` OR `D` would clear a correct grade along with the wrong ones. (Constraint pair credited to vl-06.)

**A5.2 — §F1's five ids are now three different failures wearing one ratio.**

| id | lineage now | grade | state |
|---|---|---|---|
| 566, 571 | `plan_version 0` | **F** | still blind, but eligible for the reset — repair can reach them |
| 580 | `plan_version 0` | **D** | blind AND ineligible — no path repairs it |
| 584, 586 | healed: **v6/S2**, **v5/S3**, `entry_order_id` set | **D** | columns resolved, grade permanently wrong |

"25% blind (5 of 20)" was accurate when measured and is now misleading as a single number. Recorded here rather than restated as a ratio, because collapsing three states into one ratio is what hid this.

**A5.3 — Population, and it is not a reconcile-path defect [DB].** Closed rows with `plan_version>0 AND cited_scenario_id<>''`, by grade: **A 30 · B 22 · D 6 · C 5**. The six D ids: **530, 575, 582, 584, 586, 591**. Discount **530** — its `cited_scenario_id` is the literal sentinel `off-plan`, so D is correct. Five are genuinely stuck: **575, 582, 584, 586, 591**. By source: reconcile 4 · armed_entry 1 · system 1.

> **CORRECTED — see A6 (same day).** The paragraph below concludes that 582 puts the ordering problem outside
> the reconcile path. That conclusion is WRONG: 582 has `plan_matched=0`, which makes its D explicable without
> any ordering defect. The genuinely-stuck count is **4, not 5**, and every one of the four is reconcile-sourced.
> The 591 "post-fix" characterisation is also wrong. A6 supersedes this paragraph.

**582 is `source=armed_entry`** — §D-9 of this report records it carrying v3/S2 correctly, i.e. it stamped on the *fill-time* path — and it still grades D. The grade is therefore computed before the stamp on the armed path too. This is a general ordering problem between grading and stamping, not a reconcile-path problem, and the case-fold fix (664ab6b7) does not address it. **591** is a post-fix row from 2026-09-03 and is in the stuck list.

**Not remediated.** Correcting the existing rows is a DB write and needs the owner's explicit authorisation under the guarded-write rule; this section is measurement only.

**A5.4 — Probe (vl-06, class 59, third):** when a repair path clears a value to trigger a recompute, check it matches the value the broken path actually writes. Extended by A5.1: check it matches *every* value that path can write, not the representative one — a predicate that matches a subset fails silently and samples clean.

### A6 — Correction of A5.3: the stuck count is 4, all four are reconcile-sourced, and 582 settles nothing

*(2026-09-03, read-only. vl-06 challenged two conclusions in A5.3; both challenges are correct and I verified them independently against the live store. This section supersedes A5.3's count, its 582 conclusion, and its 591 characterisation. A5.3's population figures and the A5.2 three-state split are unaffected.)*

**A6.1 — The discriminator A5.3 lacked [A].** `GradeAdherence` bases a cited row that matched direction at **A** unless the band is `off_band` or `struct` (both base B). Only two penalties exist, each one step, so **base A can reach C at worst — never D**. A row with `plan_matched=1` and a band outside `{off_band, struct}` sitting at D is therefore impossible from correctly-ordered grading. That is the clean test A5.3 did not apply.

Applying it [DB] — `plan_matched` and `plan_band` on the six D rows:

| id | source | plan_version | cited | plan_matched | plan_band | verdict |
|---|---|---|---|---|---|---|
| 530 | system | 2 | `off-plan` | 0 | *(empty)* | honest — sentinel → `OffPlan` → base D |
| 575 | reconcile | 3 | S2 | **1** | `armed_fill` | **impossible D** |
| 582 | armed_entry | 3 | S2 | **0** | *(empty)* | **explicable — not evidence** |
| 584 | reconcile | 6 | S2 | **1** | `armed_fill` | **impossible D** |
| 586 | reconcile | 5 | S3 | **1** | `armed_fill` | **impossible D** |
| 591 | reconcile | 2 | S1 | **1** | `armed_fill` | **impossible D** |

**Stuck: 4 — ids 575, 584, 586, 591.** Not 5.

**A6.2 — 582 does not move the defect off the reconcile path, and A5.3 was wrong to say it does.** With `plan_matched=0`, 582 takes `GradeAdherence`'s `default` branch — base **C**, "cited a scenario but the direction mismatched" — and one penalty gives D honestly. Worse for A5.3's reasoning: a *grade-before-stamp* would produce base D (`!Cited`) with no penalty, which is **also** D at `plan_matched=0`. The two hypotheses are observationally identical on this row, so it cannot discriminate in either direction.

**Whether the `armed_entry` path grades before it stamps is OPEN.** No row in the current data settles it, and A5.3's claim that it does is withdrawn. Stating it as open is the point — a wrong answer closes the question.

**A6.3 — The reversal A5.3 got backwards.** All four impossible-D rows are **`source=reconcile`** (575, 584, 586, 591). A5.3 concluded "this is not a reconcile-path problem" on the strength of 582; with 582 excluded, the evidence points the other way — every impossible-D row is on the reconcile path. This is not proof of reconcile-specificity, because the armed path has one D row and that row is undecidable; it is the absence of any counter-example.

**A6.4 — 591 is not a post-fix regression (A5.3 wrong, correction is vl-06's).** 591's armed row filled 09:03:53, the position materialized 09:05:14, and the boot carrying 664ab6b7 was 11:10:33 — it was graded roughly two hours before the fix existed.

**A6.5 — Denominator, and a trap in the discriminator [DB].** Closed rows carrying a grade: **71**. So an adherence rate computed today under-reports plan-following by **4 in 71** — not 5, not 7. Caveat for anyone re-running A6.1's predicate: without a lineage clause it also returns **572** (`plan_matched=1`, `plan_band='armed_fill'`, grade D, `plan_version 0`, `cited_scenario_id='TEST-E7'`), whose `source` and `close_reason` are both `e7_farside_test`. That is an `ARMED_TEST_SEAM` artifact, not a trade, and it must be excluded — the same test-seam contamination §D-3 of this report flags in `store/position_query.go`'s unfiltered counts.

**Still nothing remediated** by either session — the rows need an owner-authorised DB write.
