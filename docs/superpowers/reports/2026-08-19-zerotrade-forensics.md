> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# 5-DAY ZERO-TRADE FORENSIC POSTMORTEM — the drought was a layer cake: K2 truncation under everything, K1 clock-kill on every survivor, a 41-hour K10 wall, a 4-hour credit outage, and six K5 refusals at the end. Zero unexplained proposals.

READ-ONLY run 2026-08-19 01:09–01:5x CT. No code, config, env, deploy, or restart was touched. Every timestamp below is America/Chicago (CT).

## 1 · Window + deploy timeline (Phase 0)

**Window:** last 5 completed trading days = Wed 2026-08-13 → Tue 2026-08-18 session-instances (ASIA/LONDON/NY per day; no ASIA Fri; Sun ASIA(16) included; ASIA(18) partial — still open at analysis time, and dark since 00:53, see §8). **Precision that reframes the incident:** trading was NORMAL through **Aug 13 14:49 CT** (17 filled entries Aug 12–13; last fill `trader_positions` entry_time 1786650571252 = 08-13 14:49:31 CT). The drought is **Aug 13 ~15:00 CT → Aug 18 21:54 CT**, ~3.6 trading days, ending with the timegate deploy.

| segment (CT) | commit | evidence | K-state live | conf |
|---|---|---|---|---|
| ≤08-13 17:59 | pre-C2 | last fill 14:49; e2f2561e commit ts | trading normal; K2 latent | HIGH |
| 08-13 17:59 → 08-16 18:03 | e2f2561e era (exact boots unknown — journal rotated) | `git log e2f2561e` 08-13 17:59:36 CT | **K1 ACTIVE** (C2 converts every open→wait) + K2 + K4 | **LOW** (no boot lines) |
| 08-16 18:03 / 19:44 / 20:18 | 7aa521a1+dirty / 359ace1c / 8e7b816a | this repo's own session logs (journal rotated; MED) | K1+K2+K4; death-loop fixes land 20:18 | MED |
| **08-16 21:14:30 → 08-17 ~15:07** | 8e7b816a booted with RELEASE=184fe200 → **BOOT REFUSED** | london-zero-trades report §3 (boot line quoted) | **K10 WALL — every entry refused** + K1 + K2 + K4 | HIGH (quoted boot line) |
| 08-17 ~03:00–15:07 deploys | 570c6c32 (H8 fix, 02:02), 1c418a1f (10:22), 8d5cfa1f (15:07) | commit ts; **first DAY-PLAN block in a stored prompt: 08-17 08:03 CT** (DB, 356 rows since) | K4 ends ~08:03; K10 ends ≤15:07; K1+K2 continue | MED |
| 08-18 02:45–07:03 | (any) | 139× HTTP 402 in `decision_records` | **K11-NEW credit outage** | HIGH |
| 08-18 10:57–11:25 | f6478923 (max_tokens 2000→8000) + 1b5139c0 (+.env 32768, mtime 11:25:13) | commit ts; total-root-cause report | **K2 ends ~11:00** | HIGH |
| 08-18 14:27:35 | 1a6dcf74 | journal boot line | **K1 ends** (C2 → log-only, feed-stamped signals) | HIGH |
| 08-18 15:19 / 18:25 | c42b7280 (K7 planner reachability) / bb966a04 | journal boot lines | K5/K6/K3 still live | HIGH |
| 08-18 21:54:03 | 4ebd779a (timegate: K3+K5+K6 fixes) | journal boot line | **all K-causes closed** | HIGH |
| 08-19 00:53 | same binary, **WSL VM reboot** (PID 208) | journal boots list | **NT8 wire DOWN since** (§8) | HIGH |

## 2 · Evidence inventory + gaps

- **journald: Aug 18 13:54 CT onward ONLY** (1.9G of the 2G cap; `journalctl --list-boots`). Everything earlier — boot lines, C2 WARN conversions, gate logs for Aug 13–17 — is unrecoverable. This forces DB-first analysis and caps Aug 13–17 boot attribution at LOW/MED.
- **`decision_records` (30,018 rows, complete over the window)** — timestamps UTC-text; per-row prompt, raw_response, decision_json, error/risk fields, latency. **K2-era caveat:** raw_responses ~950–1250 chars are RETRY STUBS (total-root-cause report §1) — usable for count, NOT for content.
- **`plans` / `plan_overlays` / `session_profiles`** — full; plan_id format gains a trader suffix mid-window (cross-trader fix).
- **`trader_positions` (516 rows, ALL closed)** — verified: zero entries after 08-13 14:49 CT. `trader_fills` newest = same instant.
- **Gate-block telemetry** — in-memory counters + journal summaries only → lost for Aug 13–17.
- **clock-health lines** — exist only from 08-18 21:54 (this fix's own deploy).
- **NT8-side logs** — not readable in this run (wire down; Windows path not mounted for logs). Gap stated.
- **Cannot ever be known:** per-conversion K1 log lines Aug 13–17 (journal rotated); the model's TRUE judgment on K2-era cycles (first-pass responses were never stored); 60-min follow-through for old proposals (bar cache truncated, currently empty).

## 3 · THE LEDGER (unit: session-instance; drought portion Aug 13 15:00 → Aug 18 24:00)

Both traders combined ("hoang" + "15m"; "15m" stopped by owner 08-18 12:07, `is_running=0`).

| day | sess | scans | ok / to / api-err / stale | decisions L/S/W | proposals (incl. K1-recovered) | exec | refused | died | U | rec? |
|---|---|---|---|---|---|---|---|---|---|---|
| 08-13 | ASIA | 347 | 345/2/0/0 | 0/0/345 | 1 (K1-conv) | 0 | 1 K1 | 0 | 0 | Y |
| 08-14 | LONDON | 260 | 260/0/0/0 | 0/0/260 | 14 (K1-conv) | 0 | 14 K1 | 0 | 0 | Y |
| 08-14 | NY | 250 | 248/2/0/0 | 0/0/248 | 1 (K1-conv) | 0 | 1 K1 | 0 | 0 | Y |
| 08-16 | ASIA | 142 | 142/0/0/0 | 0/0/142 | 1 (K1-conv) | 0 | 1 K1 | 0 | 0 | Y |
| 08-17 | LONDON | 104 | 104/0/0/0 | 0/0/104 | 0 | 0 | 0 | 0 | 0 | Y |
| 08-17 | NY | 192 | 182/0/0/10 | 0/0/182 | 2 (K1-conv) | 0 | 2 K1 | 0 | 0 | Y |
| 08-17 | ASIA | 276 | 252/2/0/21 | 0/0/252 | 2 (K1-conv) | 0 | 2 K1 | 0 | 0 | Y |
| 08-17 | (gap) | 39 | 35/0/0/4 | 0/0/35 | 1 (K1-conv) | 0 | 1 K1* | 0 | 0 | Y |
| 08-18 | LONDON | 181 | 35/0/**139**/7 | 0/0/35 | 1 (K1-conv) | 0 | 1 K1 (+139 calls lost K11) | 0 | 0 | Y |
| 08-18 | NY | 73 | 72/0/0/1 | 0/0/72 | 3 (K1-conv) | 0 | 3 K1 | 0 | 0 | Y |
| 08-18 | ASIA | 82 | 71/5/0/0 | 0/6†/71 | 6 | 0 | **6 K5** | 0 | 0 | Y |

† the six ASIA(18) proposals were recorded as REFUSED rows (risk_check_error), direction short. \* 15:03 CT, outside any session.
Timeouts (K3) drought-total: **11** (08-13 22:08 ×2 · 08-14 11:06/11:07 · 08-17 21:48 · 08-18 01:08, 19:35, 20:35, 20:59, 21:08, 21:23) — none after the 21:54 fix. `none_returned`=42 rows are **no-call session-gap records** (raw_response length 0), not K2.
**Reconciliation:** re-derived two ways — outcome-shape census (`success/error/risk` GROUP BY: 6 last_entry, 11 read-timeouts, 139+122 API-402, 43 stale, 1 schema-parse) matches the table totals; calls ≥ decisions ≥ proposals ≥ refused+died holds on every row. **K1-recovered caveat:** the 26 converted proposals are wait-rows still carrying SL/TP+confidence (C2 rewrote `d.Action` in place, `git show e2f2561e:kernel/clock_drift.go` — the fields survive); this is a **lower bound** (a conversion that zeroed nothing else is invisible) and one recovered row (id 29439) literally contains `"action": "open_long"` in its reasoning text.

## 4 · Attribution detail (every blocked/lost item)

- **6 × K5** — 08-18 20:28/20:29/20:37/20:43/20:49/21:33 CT (ids 29974/75/77/78/79/88), `last_entry_cutoff: past last-entry 13:00 CT`, all short, all pre-21:54-fix. HIGH.
- **26 × K1** — the recovered conversions listed per session above (ids incl. 29851/29853/29880 on 08-18 NY morning — matching 1a6dcf74's commit-body "7 valid entries today converted"; my recovery finds 4 that morning: the commit's count includes conversions that left no SL/TP residue — consistent with lower-bound). Segment check: all 26 fall inside the K1-live window (08-13 17:59 → 08-18 14:27). MED (field-residue inference; no journal lines survive).
- **11 × K3** — timeouts listed above; the four on 08-18 evening (19:35–21:23) all precede the 21:54 fix. Consistency alarm scan: **zero** timeouts post-fix. HIGH.
- **139 × K11-NEW** — HTTP 402 "Insufficient Balance", 08-18 02:45→07:03 CT, killing effectively all of LONDON(18)'s calls (35 of 181 scans got answers). Not in the taxonomy — see §5. HIGH.
- **K2 (era, not per-item)** — every ok-call from the window start until 08-18 ~11:00 produced a retry-stub decision (total-root-cause §1: wire-replay showed finish_reason=length at 2000 AND 4000 AND 8000 tokens on the live prompt; stored raws are stubs). ≈2,900 drought cycles are therefore **degraded evidence**, tagged K2-manufactured-wait. The completed judgment, when the wire test forced completion, was also wait on the tested cycles — so K2's *proposal* cost is indeterminate, its *evidence* cost is total. HIGH for the mechanism, INDETERMINATE for per-cycle counterfactuals.
- **K10 (era)** — boot-refusal wall 08-16 21:14:30 → ≤08-17 15:07 (london report §3 quotes the refusal line; end bounded by 8d5cfa1f build ts + e2e audit's STEP 0). No proposal hit the wall (K1/K2 upstream already suppressed) — cost is counterfactual only. HIGH start / MED end.
- **K4/H8 (era)** — zero DAY-PLAN blocks in any stored prompt before **08-17 08:03 CT** (DB grep: first of 356 plan-bearing prompts), plus registry veto per london report §8. Ends with the 570c6c32-bearing deploy. HIGH (DB) / MED (deploy instant).
- **Consistency alarms fired:** none post-fix — no K5-shaped refusal, no K3 timeout, no silent handoff loss after each fix's live time. The 08-17 ASIA plan chain that LOOKED like a recurring death loop (6 versions) is actually v3/v5/v6 `owner_reset` + v4 `planner_fail_closed` — no death-condition deaths post-death-fix. ✓

## 5 · U-BUCKET (nothing implemented)

- **U1 · NT8 wire down since the 00:53 VM reboot — LIVE NOW.** Zero `bar_update` frames post-boot (journal count = 0); `/api/klines` returns empty; ASIA(18) v3/v4 planner reads fail-closed 01:04/01:08 CT ("scenarios count 0 invalid") because the planner called with NO market data. Fail-closed behaved correctly; the FEED is the emergency. Root cause: Windows-side NT8 did not survive/restart with the VM. Proposed: P0 alert "no bar frame for >10 min while CME open" (S) + a planner pre-flight "refuse to CALL with an empty bar window" (S — saves the API spend and the confusing 0-scenario stub). **Still live in HEAD: yes (operational).**
- **U2 · K11 credit outage class.** No balance floor/alert exists; the bot burned 4.3 h of London retrying 402s. Proposed: treat 402 as non-retryable + P0 alert (S). Still live in HEAD: yes.
- **U3 · Journald 2G cap ate the forensic window.** 5-day postmortems are impossible beyond ~1 day of chatty logging (16,943 msgs suppressed in one burst). Proposed: raise cap / per-unit rate-limit relief / ship key WARN+ERRO lines to a DB table (S–M). Still live: yes.
- **U4 · Map-vs-market snapshot skew** (e2e audit Part 1 mismatch): plan/level distances computed off a snapshot ~2 min older than the market block in the SAME prompt. Proposed: single snapshot instant for both (S). Still live in HEAD: yes (no commit addresses it).
- Not bugs, documented: the "15m" trader was stopped by the owner 08-18 12:07 (`is_running=0`); the OFF-gap no-call records; ASIA(17) `owner_reset` chain.

## 6 · Counterfactual (gate-logic re-evaluation, no fills simulated)

Re-evaluated all 32 lost proposals (6 K5-refused + 26 K1-converted) against HEAD gates (per-session cutoffs ASIA 01:45 / LONDON 08:15 / NY 14:30; C2 log-only; boot OK; wired handoff):

| class | n | detail |
|---|---|---|
| **WOULD-PASS-NOW** | **31** | all 6 K5 shorts (20:28–21:33 CT ≪ ASIA cutoff 01:45); 25 of 26 K1 conversions (in-session, pre-cutoff) |
| LEGIT-REFUSAL | 1 | the 08-17 15:03 CT conversion — outside any session under HEAD; the session gate correctly owns it |
| INDETERMINATE | 0 gate-wise | (fill quality, slippage, and the AI's post-gate behavior are not simulable) |

**lost_trades_count = 31** (upper bound of gate-passable proposals; lower bound of K1 recovery). Per day: 08-13: 1 · 08-14: 15 · 08-16: 1 · 08-17: 4 · 08-18: 10. 60-min follow-through: **not computed** — the bar archive for those instants is unavailable (cache truncated at ~41 h and currently empty); stating a direction-vs-move table would be manufactured evidence.

## 7 · Silent sessions + 7b/7c input-pipeline audit

**7a.** Sessions with zero surviving proposals: LONDON(17) — and, before recovery, most others. Era split does the classifying: pre-08-18-11:00 silence = **K2-manufactured waits** (evidence-degraded, not judgment); LONDON(18) = **K11**; post-11:00 genuine waits = **prompt-discipline suppression**, proven by the other session's decisive tests: plan block STRIPPED → 3/3 then 5/5 real cycles still wait; control trader (no day-plan) 36/36 waits (why-no-trades §, end-to-end §2d). **K7 in the strict sense (unreachable levels) is a minor contributor:** plan spans vs session ranges show levels WERE in reach on the big days — 08-17 range 610 pts (29514–30124, SVP store) vs ASIA(17) plan v6 levels 29853–30124; 08-18 NY plan levels 29680.75–30092 vs the day's 29680.75 low (level = the low itself). The planner-reachability fix (c42b7280) plus prompt-wording fixes (aeaf7076) landed 08-18 15:19/deployed. LEGIT-NO-SETUP counts cannot be separated from K2-era manufacture before 08-18 — stated as indeterminate rather than guessed.

**7b · Read-workflow verdict table** (stage diagram = planner→plans table→ActivePlanProvider→system-prompt PLAN block→snapshot(bars/price/indicators/news)→input prompt→AI→parse→decision; full 17-hop live trace already proven in the e2e audit, cited):

| stage | scope | fresh | units | wired | verdict |
|---|---|---|---|---|---|
| plan load (session-instance) | OK post-08-17 08:03 (session+trader-keyed; 356 prompts carry it) | OK | — | **was DEAD (K4) before 08-17** | SOUND at HEAD |
| plan staleness | fail-closed writes NO-TRADE (never silent-stale) — proven live tonight 01:04 | OK | — | OK | SOUND (by design) |
| owner overlay → prompt | e2e verified edited levels flow; overlay carry by price identity | OK | — | OK | SOUND |
| Ask-Planner thread | **display/Q&A only — does NOT feed the executor prompt** (owner should know) | — | — | n/a | SOUND, stated |
| levels vs price basis | same NT8 continuous-MNQ feed for planner and snapshot (single FuturesBarsProvider); June-style calendar-spread mismatch ABSENT | OK | OK | OK | SOUND |
| snapshot freshness | drought avg AI latency 57.6 s, max 149.7 s, 11 >120 s → decision acts on a snapshot up to ~2.5 min old; **no snapshot-age guard besides stale-bar discard + (now log-only) C2** | **watch** | — | OK | residual risk §8 |
| map-vs-market instant | ~2-min internal skew (U4) | BUG(S) | — | — | report-only |
| input truncation | prompts ~15.4 k chars ≈ well under context; K2 was OUTPUT-side | OK | — | — | SOUND |
| timezone in prompt | Time(CT) labels present in all samples; dual CT/UTC labels through 08-17, CT-only after | OK | — | — | SOUND |
| parse-back | prose-JSON recovery (2ddf3a58) + parse-fail is RECORDED (`schema_parse_failed`, 1 in window), not silent-waited; reasoning backfilled; confidence 0–100 vs min 60 same scale | OK | OK | OK | SOUND |

**7c · Prompt reasonableness (9 samples, ids 29067…29927).** All 9 complete (bars 6 TFs, price, indicators, session id, CT times; plan block present in-session post-08-17). Verdicts: 7 SOUND · 2 FLAWED — (i) id 29067 (LONDON 08-17 02:36) pre-plan-era: no plan block at all (K4, defect class 5); (ii) the strongest flaw is not one prompt but the standing pair every prompt carries: *"No trade inside sideway zone becareful"* (owner box) + *"react AT levels, not between them"* — the proven behavioral suppressor (defect class: shadow-config-by-prompt; wording softened in aeaf7076).

## 8 · TONIGHT'S RESIDUAL RISK

1. **NT8 wire is DOWN right now** (U1) — zero bars since the 00:53 reboot; ASIA(18) tail untradeable regardless of code. First owner action at wake: start NT8 on Windows; the AddOn reconnects on its own.
2. **Clock-drift headroom unknown at this instant** — last measurable line (21:57 CT session-roll) showed **drift −116,013 ms vs the 60,000 ms C2 tolerance** (WSL behind NT8; includes ≤60 s in-progress-bar quantization). C2 is log-only now (signals feed-stamped), so this cannot block entries — but the VM reboot at 00:53 likely resynced the clock, and no bar exists yet to measure against. Headroom number: **indeterminate until the feed returns; last known: −56 s beyond tolerance.**
3. **Snapshot-age at decision** (7b): a legal 300 s call can act on a ~5-min-old snapshot with only stale-bar discard guarding it — reasonableness finding, unquantified cost.
4. U2 (no 402 floor), U4 (2-min map skew), and the LOW-confidence Aug 13–16 boot segments (which cap how certain §4's K1 era-attribution can ever be) remain open.
5. Prompt-discipline suppression: fixed by wording only; if genuine waits persist with reachable levels, the next levers are the owner's personalized line and Entry-Standards block (why-no-trades §7 — owner decision, not a code bug).

## 9 · EXECUTIVE VERDICT

**Yes — the drought is fully explained, with zero unexplained blocked proposals (U count for proposals: 0).** Of the 43 concrete lost items (32 proposals + 11 timeouts): **K1 60%** (26), **K3 26%** (11), **K5 14%** (6); environment-level causes stack beneath them — K2 degraded ~2,900 cycles' evidence until 08-18 11:00, K10 walled all entries for ~41 h, K4 hid the plan from every prompt before 08-17 08:03, K11 ate 4.3 h of London calls, K6 never fired only because K5 upstream guaranteed no position. The AI's input pipeline is **sound at HEAD** (7b) — its weakest link is not a read at all but the prompt's own entry-discipline wording, and the operationally weakest link **tonight is the NT8 feed, which is down at the time of writing**. Attribution certainty is HIGH from 08-18 13:54 (journal) and MED/LOW before (rotated logs; DB-only inference).

## 10 · PR

https://github.com/johnwick2921-cyber/vl/pull/46 — **#46** (parsed from the `gh pr create` output URL).
