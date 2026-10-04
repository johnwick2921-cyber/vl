> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# FINAL REPORT — fix/ledger-close-sep-risk (2026-08-19)

Standalone per the final report specification. Branch head `f6447076`,
deployed 10:17:57 CT. Times CT unless noted; evidence tiers [A]/[B].

## SECTION 1 — EXECUTIVE SUMMARY

Shipped: all 8 ledger-close phases + the P10 cadence ruling + 5 E7-v2
sweep fixes (one HIGH), 22 commits, live since 10:17:57 CT. Not
shipped: Phase 9 (post-exit rescan — never dispatched; number
reserved), one E7 finding deferred (unlocked registry RMW, rationale
§3). The bot is measurably healthier: drift is watched every 15 min,
an owner pause exists, roll/half-day calendar risk is gated with
CME-verified dates, 402 is one banner not 139 dead cycles, WARN+ logs
survive journald, prompts carry one honest snapshot instant, and the
owner's 2-min cadence is real (24 calls/h measured, entry #20201
taken organically during the soak). Single biggest residual risk: the
Labor-Day session split — a Sunday-evening ASIA entry rides into
Monday on NT8 brackets alone (mitigation: the new pause; owner call
§9). PR: **#51** (URL in §11).

## SECTION 2 — PHASE LEDGER

**P1 clock guard — DONE** · `51c2a9bf` (timer+script+installer),
`d9978599` (CLOCK_WARN_MS + boot block) · tests green Y ·
Deviation (documented): dispatch 1.2 wanted hwclock resync; hwclock
does not exist in this WSL2 and no root-free resync exists (sudo
gated, set-ntp polkit-gated) — guard is measure+alert per the
dispatch's own "use the best available and SAY SO"; owner root unit
remains the escalation. Timer installed live 08:50 CT; first cycle
logged `status=OK rtc_vs_wsl_s=-1 ntp_offset=+644.145ms`.

**P2 stopUntil — DONE** · `71ee0444`, `d4ad9875`, `26a90967` (E7 fix)
· tests green Y · **Gate position: first owner/policy gate — stage E
slot 5, after feed/dead-man/freeze/boot-integrity, before
consecutive-loss/roll/last-entry/session/plan/approval — pinned by
TestOwnerGateOrderPauseBeforeRoll.** **Persistence across restart
proven: TestPauseSurvivesRestart** (fresh AutoTrader over the same
store restores the deadline exactly); expiry auto-resumes
(TestPauseExpiryAutoResumes). Deviation: legacy `stopUntil` field +
its whole-cycle gate (loop:~248) left dormant per additive-only; the
new pause supersedes (entries-only semantics per the owner contract).

**P3 roll block — DONE** · `ef66d44e`, `cc7e45db` (E7 DST fix) ·
tests green Y · **Resolved contract + expiry printed in: (a) the E1
ledger-boot line (`roll=pending AddOn ACK` at boot, resolved after
ACK), (b) `GetStatus["roll"]` (dashboard/status API: resolved
contract, expiry, window start, days left, blocked).** **CME source
cited (in-code + here): archived original of CME's ProductCalendar
API for product 146 — NQU26 lastTrade = settlement = 2026-09-18
(https://web.archive.org/web/20260710162138id_/https://www.cmegroup.com/CmeWS/mvc/ProductCalendar/Future/146);
termination 8:30 CT per Rulebook ch. 359 (35902.G).** Live ACK today:
`subscription ACK symbol=MNQ contract="MNQ 09-26"` (10:18:03).
Deviation: gate fail-opens on unresolved contract (dispatch 3.5) and
window uses inclusive calendar days (default 3 → Sep 15–18; Sep 14
blocks at env ≥4 — boundary documented).

**P4 HalfDays — DONE** · `a37aff45`, + E7 fixes `e3e22a15` (HIGH
key-space), `4d17cbae` (session-relative pull-in), `14f93d11`
(deletion honored) · tests green Y · **Seeded table:**

| Calendar date | Early close CT | Label | Registry key (session-day) |
|---|---|---|---|
| 2026-09-07 | 12:00 | Labor Day — halt 12:00, reopen 17:00 | 2026-09-06 |
| 2026-11-26 | 12:00 | Thanksgiving Day — halt 12:00 | 2026-11-25 |
| 2026-11-27 | 12:15 | Day after Thanksgiving — close 12:15 (settle 12:00) | 2026-11-26 |
| 2026-12-24 | 12:15 | Christmas Eve — close 12:15 (settle 12:00) | 2026-12-23 |
| 2026-12-31 | — | deliberately ABSENT (CME: normal equity session) | — |

Live seed line 10:18:03: `📅 half-days seeded: 4 entries updated…`;
boot: `next half-day: 2026-09-07 12:00 CT (Labor Day…)`. **Sep 7 sim
result (TestHalfDaySeedUsesSessionDayKeys +
TestHalfDayPullsNYCutoffsForward): Monday 11:00 CT keys 2026-09-06 →
pull-in HITS (last-entry 12:00−offset, e.g. 11:30 at offset 30; flat
12:00 at offset 0); Tuesday Sep 8 MISSES (the wrong-day flatten class
killed by the E7 fix).** Deviation: none vs spec; the isCMEHoliday
full-closure collision is an owner item (§9).

**P5 402/balance — DONE** · `d9a23407` · tests green Y · **Dedup
proof: Test402BurstOneAlertThenAutoClear — a 139-burst raises exactly
ONE unacked P0; the first success auto-acks it; a NEW outage
re-alerts.** Typed `decision_records.error_class`; DeepSeek
GET /user/balance poll default OFF (AI_BALANCE_WARN). Deviation:
banner rides the day-plan-gated alert bus (owner's trader qualifies).

**P6 log→DB — DONE** · `85ec608f` · tests green Y (incl. 50k-flood
non-blocking + retention) · live: 78 WARN+ rows shipped in the last
hour. Deviation: INFO-level lines and slog-based tcp_server lines not
shipped (by design, stated).

**P7 snapshot — DONE** · `ce1a3988` · tests green Y · one instant per
cycle, `Snapshot: HH:MM:SS CT` in every stored prompt (E2 quote §4).
Zero golden churn.

**P8 B4 fallback — DONE** · `1021c65b` · tests green Y · absence =
WARN + fail-open; no hidden clock in the predicate.

**P9 post-exit rescan — DEFERRED (never dispatched).** No Phase 9
instruction ever reached this agent; the number is reserved.
Re-entry path: dispatch it against the reconciled position source
(PR #50's guarded path — this branch's parent, so ordering is
enforced by construction; consume `skipGateDesync`-checked state or
broker-live GetPositions, never raw store rows). **9.3 answer: YES,
an anti-revenge cooldown exists today — the B7 re-entry armor
(stage D, gate 25): after a stop-loss exit, same-direction re-entry
on that symbol is rewritten to wait until per-strategy
`ReentryCooldownMinutes` elapses OR price moves ≥1×ATR15 from the
stop. It is per-strategy opt-in, DEFAULT 0=OFF → owner-decision flag
in §9.**

**P10 cadence — DONE** · `e065b01e`, `a4aa9e7c` · tests green Y ·
**Default confirmed "interval"** (empty/garbage resolve to interval;
only explicit "bar_close" keeps the legacy gate — TestCadenceModeResolution).
**Studio help text, final wording:** interval mode: "AI evaluates
every N minutes (mode=interval), including the forming bar." ·
bar-close mode: "AI evaluates once per closed primary bar; the
interval only sets check frequency (mode=bar_close)." (en/zh/id.)
**Boot line sample (live 10:18:03):** `📊 Loading trader hoang:
ScanIntervalMinutes=2 (source=Studio/DB), cadence=interval 2m0s,
mode=interval (DB empty → default)`.

## SECTION 3 — E7-v2 SWEEP RESULTS

(v1 sweep stalled at 09:36 with 0 results and was killed; v2 = 4
hunk-scoped finders + adversarial verifiers, 13 agents, all done, 2
findings refuted by verifiers.)

| Finder | Hunted | Findings → verdict |
|---|---|---|
| find:wiring | never-wired + computed-then-discarded across all 10 phases | **ZERO findings** — every producer/hook/route/field verified wired |
| find:time | day-scope/time/literal-on-path | 4 raised: **(HIGH) half-day calendar-vs-session-day-key skew → CONFIRMED-FIXED `e3e22a15`** · out-of-window pull-in disables custom cutoffs → CONFIRMED-FIXED `4d17cbae` · roll daysLeft DST truncation → CONFIRMED-FIXED `cc7e45db` · pause until_ct DST wrap + loose parse → CONFIRMED-FIXED `26a90967` |
| find:races | concurrency/correctness | 2 raised: pause expiry CAS clobbers concurrent re-pause persist → CONFIRMED-FIXED `26a90967` (store-write mutex) · SeedHalfDaysIntoRegistry unlocked RMW vs admin registry save → **CONFIRMED-DEFERRED** (once-per-session-day producer vs rare admin save, whole-row last-writer-wins, self-heals next session-day; a cross-path lock spans store+api layers — small but not additive-trivial) |
| find:twins | twin-path/multi-instance | 1 raised: half_days.json deletion never pruned from registry → CONFIRMED-FIXED `14f93d11` (producer-owned-keys ledger) |

## SECTION 4 — E-VERIFICATION MATRIX

| E | Verdict | Evidence |
|---|---|---|
| E1 | PASS | boot block below |
| E2 | PASS | trace below (rows 30098 + 30123) |
| E3 | PASS | roll sims: Sep 15/16/17/18 blocked, Sep 14 open (env 5 blocks it), Sep 21+DEC26 passes (~88d); message core `MNQ SEP26 expires 2026-09-18`; half-day: Monday hit / Tuesday miss / exact 11:30/12:00 values (unit sims, quoted §2-P4) |
| E4 | PASS | pause set→`"paused until HH:MM CT (owner)"`→resume→clear; restart-persist; expiry auto-resume; 402 139-burst→1 alert→auto-clear; #48 feed-gap suite green post-rebase |
| E5 | PASS | precedence pinned on emitted refusal strings: stop_until < contract-roll < consecutive-loss (a paused refusal names the pause); half-day vs session cutoff resolves session-relative, log names the resolved hhmm |
| E6 | PASS | soak below |
| E7 | PASS | §3 (1 deferred, disclosed) |
| E13 | PASS | delta table below |
| E14 | PASS | mode=bar_close: barCloseGate untouched + wiring pinned (TestBarCloseModeKeepsLegacyGate); legacy tests green |
| E15 | PASS (unit) / live-line PENDING | dedup proven by TestSkipNoNewData; during the soak the NY market never produced an identical bar signature (bars mutate every 2-min tick), so no live line exists yet — it will first appear in a maintenance/weekend quiet period. Replacement evidence: the unit test + the exact line format: `⏭ cycle_skip=no_new_data — newest 5m bar unchanged since the last cycle and flat; not burning an AI call on an identical snapshot.` |
| E16 | PASS | trader-update API does RemoveTrader → reload → re-Run on save: mode applies WITHOUT bot restart (handler_trader.go:716-729) |
| E17 | PASS | discard data below |

**E1 boot block (10:18:03 CT, PID 20199, verbatim):**
```
🔐 BOOT INTEGRITY OK — rev f6447076cffa +dirty · built 2026-08-19T15:17:34Z · expected f6447076cffa · goldens PASS
🕰 clock-health [boot] go=10:18 CT (15:18 UTC) nt8_last_bar=none drift_ms=n/a timesync{NTP=yes NTPSynchronized=yes} tolerance_ms=60000
🛡 clock-guard [boot] rtc_vs_go=1s timer=active last_check=2026-08-19T15:15:11Z (2m52s ago) last_status=OK rtc_vs_wsl_s=1 ntp_offset=+915.726ms warn_ms=30000 tolerance_ms=60000 resync=unavailable-no-root (timesyncd slews; owner root unit is the escalation path)
📅 half-days [boot]: 4 loaded from half_days.json · next half-day: 2026-09-07 12:00 CT (Labor Day — equity futures halt 12:00 CT, reopen 17:00 CT)
🧾 log-shipping active: WARN+ → log_events (retention 30 days; async, drop-on-overload)
📊 Loading trader hoang: ScanIntervalMinutes=2 (source=Studio/DB), cadence=interval 2m0s, mode=interval (DB empty → default)
🧾 ledger boot: sessions[ASIA 17:00→02:00 CT (last-entry 01:45, flat 01:45) | LONDON 02:00→08:30 CT (last-entry 08:15, flat 08:15) | NY 08:30→14:45 CT (last-entry 14:30, flat 14:30)] · stop_until=none · cadence=interval 2m0s · roll=pending AddOn ACK · balance-alert=off
✅ Trader auto-started successfully
2026/08/19 10:18:03 INFO tcp_server: hello handshake OK protocol_version=3 source=vltrader-addon
2026/08/19 10:18:03 INFO tcp_server: subscription ACK symbol=MNQ contract="MNQ 09-26"
2026/08/19 10:19:06 INFO wire_liveness last_frame_age=0s frames_per_min=58358 bar_age="1m=6s 5m=246s 15m=246s"
```
("+dirty": two known non-branch worktree diffs — a gofmt-whitespace
test edit from the sibling session and the live deploy/RELEASE stamp;
expected==rev so integrity is OK.)

**E2 live cycle traces:** clean-wait cycle #20176, **DB row 30098** —
stored SYSTEM prompt: `## Clock\n10:22 CT (15:22 UTC) · Snapshot:
10:22:15 CT — ALL times in this prompt are CT…` · stored USER prompt
market block: `current 5m bar: FORMING (closes 10:25 CT) — prior bars
closed` · decision `wait` · `✓ MNQ wait succeeded` (all execute gates
un-tripped). Entry cycle #20201, **DB row 30123** — decision JSON:
`{"symbol":"MNQ","action":"open_short","stop_loss":29672.5,
"take_profit":29514,"confidence":65,…}` → gates passed (no refusal in
the row; confidence 65 ≥ 60; R:R gate passed) → wire → NT8 fill:
position row #524 SHORT 1.0 @ 29650.75, entry 11:18:15 CT, OPEN →
next cycles #20202-#20206: `🧘 skip-while-open: holding 1 open (MNQ
SHORT) — AI decision skipped … (snapshot+equity recorded…)` every
~2 min (the #49 heartbeat contract at interval cadence, live).

**E6 soak (10:18:03 → 11:27:06 CT, 69 min):** 32 cycles (#20175–
#20206) · 27 paid AI calls ≈ **24.2 calls/hour** (bar-close baseline
this morning 08:22–09:22: 10/h → **2.4×**, inside the accepted 2–2.5×
cost note) · refusals by gate: 0 (no stop_until/roll/session/
last-entry trips) · **B4 false stale discards: 0** ✓ · supersession
discards: 5 (E17) · no_new_data skips: 0 (active market) · trades:
**1 organic entry** (#20201, SHORT @ 29650.75, still open at report
time) + 5 in-position heartbeats.

**E13 tick-to-cycle-start deltas (start = save − AI duration):**
starts 10:20:17, 22:24, 24:34, 27:25, 28:42, 30:52, 32:55, 35:00,
37:05, 39:14, 41:18, 43:25, 45:30, 47:35, 49:44, 51:49, 53:53, 55:59,
58:05, 11:00:19, 04:23, 06:26, 08:30, 10:38, 12:42, 14:47, 16:53 →
gaps 125–131s across the board (2-min ticks + 5–11s drift), one 4-min
gap after the 237s call at #20194 (in-flight rule, expected). The
2-minute Studio interval is honored between cycle starts.

**E17 discard rate (interval mode, 69-min soak):** 5 supersession
discards (#20177, #20182, #20184, #20189, #20200) = **4.3/hour =
18.5% of paid calls**; per-TF split: 5×5m (primary), 0×1m, 0×15m.
Mechanism each time: an AI call ≥65s spanned a 5m close. This is the
seed data for the parked discard-burn dispatch — at 24 calls/h, ~1 in
5 paid calls is discarded near closes.

## SECTION 5 — CUTOVER RECORD

Flat-window check 10:17:44 CT: 0 OPEN rows, latest cycle #20174
complete, trader running · deploy 10:17:57 CT · `d154bb44` →
`f6447076` · deploy/RELEASE =
`f6447076cffa5b13a7a60b3ebecb20bb5302bd18` · PID 76816 → **20199** ·
AddOn reconnect 10:18:03 (hello v3; ACKs `MNQ 09-26`, `ES 09-26`) ·
first post-cutover AI call: cycle **#20175**, saved 10:21:16, 59,205
ms, "MNQ wait succeeded".

## SECTION 6 — REGRESSION PROOF

`go test ./...` = **exit 0, zero failures** (run post-E7-fixes at
head `f6447076`; also `cd web && tsc --noEmit && npm run build`
clean). Prior suites explicitly green in that run: #45 timegate
(T1–T7, tz-guard, ai-timeout contract), #48 stale-guard
(stale_data_test, feedwatch, open-stamp/placeholder), #49 in-position
ordering (intrade_contract_test), #50 desync guard
(position_desync_test). Tests MODIFIED by this branch:
`kernel/stale_data_test.go` + `kernel/w16_stale_gate_test.go` (P8:
fixtures gained SnapshotMs — they pinned the removed hidden-clock
fallback; new absence→fail-open test added) and
`trader/halfdays_test.go` (E7 HIGH: expectations moved to session-day
key space). No other pre-existing test touched.

## SECTION 7 — DIFF ACCOUNTING

`git diff --stat main..HEAD` (spans the open PR stack #45→#50 plus
this branch — main has none of it merged): **85 files, +6,174/−395**.
Full stat pasted in the PR body appendix (86 lines, verbatim).
This branch alone (base `d154bb44`): 50 files, +3,275/−78.
Files touched outside the phase plans, each justified:
`trader/auto_trader_decision.go` (GetStatus: truthful stop_until +
roll block — the 2.2/3.6 status surfaces), `mcp/balance.go` +
`store/alert.go` (P5 named in-phase), `deploy/*` (P1 new units),
`trader/gate_order_test.go` + `trader/cadence_mode_test.go`
(E-verification artifacts), `manager/trader_manager.go` (P10 config
plumbing + boot line). Drive-bys: none. **New dependencies: NONE**
(stdlib + existing go.mod only). New env keys:

| Key | Default | .env.example |
|---|---|---|
| CLOCK_WARN_MS | 30000 | Y |
| CLOCK_GUARD_WARN_S / VL_CLOCK_STATE | 30 / data/clock-guard-state.json | Y |
| ROLL_BLOCK_DAYS_BEFORE_EXPIRY | 3 | Y |
| VL_HALF_DAYS | half_days.json | Y |
| AI_BALANCE_WARN | unset (OFF) | Y |
| LOG_DB_RETENTION_DAYS | 30 | Y |
| POSITION_RECONCILE (PR #50 base) | on | Y |
| INTRADE_FEED_ALERT_S (PR #49 base) | 120 | Y |

DB migrations (all additive AutoMigrate, idempotent):
`decision_records.error_class` (column), `log_events` (table),
`traders.cadence_mode` (column), plus system_config keys
(`trader_pause_until:*`, `half_days_seeded_keys`). Rollback notes
(documented, NOT executed): `ALTER TABLE decision_records DROP COLUMN
error_class;` · `DROP TABLE log_events;` · `ALTER TABLE traders DROP
COLUMN cadence_mode;`. Postgres path: existing-table AutoMigrate skip
is the pre-existing house convention; SQLite (live) migrates cleanly.

## SECTION 8 — LIVE STATE AT REPORT TIME (11:27 CT)

Cycle #20206 (in-position heartbeat) · last 3 PAID AI calls: #20199
11:13:35 52.8s wait · #20200 11:16:18 91.6s (superseded/discarded) ·
#20201 11:18:16 82.9s **open_short EXECUTED** · open positions: 1 —
MNQ SHORT 1.0 @ 29650.75 (row #524, SL 29672.5 / TP 29514, NT8
bracket live) · session NY 08:30→14:45, resolved last-entry 14:30,
flat 14:30, next half-day 2026-09-07 · clock: rtc_vs_go=1s, guard
timer active (last check 2m52s before boot), NTP offset +915ms ·
wire_liveness 11:27:06: `last_frame_age=0s frames_per_min=56744
bar_age="1m=6s 5m=126s 15m=726s"` · balance-alert: off (env unset) ·
log_events rows last hour: **78**.

## SECTION 9 — OWNER DECISION QUEUE

1. **P9 cooldown policy:** B7 exists but is OFF (ReentryCooldownMinutes=0).
   Recommend: arm it (e.g. 15 min) OR fold into the Phase 9 dispatch.
2. **Labor-Day session split:** Sunday-evening ASIA entries ride into
   Monday unmanaged (isCMEHoliday full-closes Sep 7; session gate sits
   above EOD-flat). Recommend: use the new pause Sunday evening Sep 6,
   or approve an isCMEHoliday refinement dispatch.
3. **Trader auto-resume:** RESOLVED this cutover — `✅ Trader
   auto-started successfully` with is_running=1; the morning's 934s
   gap was owner-stop, not a defect. No action.
4. **E7 deferred finding:** half-days seed RMW vs concurrent admin
   registry save (rare, self-healing). Recommend: accept; fold a lock
   into the next store-layer touch.
5. **P10 cost data:** 24.2 calls/h ≈ 2.4× bar-close, inside your
   accepted range — but 18.5% of paid calls are discarded at 5m closes
   (E17). Recommend: keep interval 2m; let the discard-burn dispatch
   decide whether to pre-close gate or accept the burn.
6. **Dec 24 / Nov 26-27 half-days are inert** while those dates remain
   full closures in code (Dec 31 is wrongly a closure — a lost trading
   day, conservative). Same refinement dispatch as item 2.
7. **journald suppression** (>100k msgs/30s): P6 preserves WARN+, but
   recommend `sudo bash deploy/install-journald.sh` + demoting
   per-frame INFO logs in a future branch.

## SECTION 10 — RESIDUAL RISK + NEXT

Ranked, what can still block/degrade a valid trade today:
1. Supersession discard burn (18.5% of calls near 5m closes) — cost +
   missed setups; parked discard-burn dispatch now has its seed data.
2. AI-call latency tail (161–237s observed) — eats the 2-min cadence
   and feeds risk 1; AI_HTTP_TIMEOUT currently defaults 300s.
3. Labor-Day split (§9.2) — date-bound (Sep 6–7).
4. DeepSeek balance (§9.5 armed OFF) — a 402 outage now alerts once
   and self-clears, but decisions still stop while broke; setting
   AI_BALANCE_WARN gives runway warning.
Known-open NOT in this branch: discard-burn dispatch (parked, seeded
by E17) · journald INFO-flood fix (U3 half-fixed: DB tee shipped,
source flood remains) · interval-config surface: SUPERSEDED by P10
(the Studio field is now the real cadence) · Phase 9 post-exit rescan
(reserved). Recommended next dispatch order: (1) discard-burn (E17
data fresh), (2) Phase 9 + B7 arming, (3) isCMEHoliday refinement
before Sep 4, (4) journald flood.

## SECTION 11 — PR

PR **#51** — https://github.com/johnwick2921-cyber/vl/pull/51
(number parsed from the `gh pr create` output URL).
