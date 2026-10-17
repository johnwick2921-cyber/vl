names rewritten to vl on 2026-09-30 (VL rename)
# 2026-09-01 — Planner API failure: root cause + fix (class 37)

**Dispatch:** PLANNER API FAILURE — ROOT CAUSE INVESTIGATION + FIX (owner hoang, 2026-09-01).
**Phase 1 verdict:** CONFIRMED. **Phase 2:** shipped on branch `fix/planner-stream-total-deadline`
(worktree `~/vl-planner-api`), merged to dev at `bc9ea126`; **CUT OVER 21:19:49 CT on the owner's "GO 37"** (rev `e42a0b43`, PID 1994488)
— see CUTOVER section. Behavioural proof still owed (A20). Section "Phase 2" below carries file:lines, tests, build, rollback.
All times CT (R8). Evidence tiers: **[A]** directly verified · **[B]** inferred · **[C]** speculation.
Evidence classes: [RUNTIME] journal/log lines · [DB] `data/data.db` read-only queries ·
[CODE] file:line · [CONFIG] `.env` / boot line. Window: `journalctl -u vl --since 2026-08-29 00:00`
(08-29 carried 0 AI calls — Saturday, CME closed; first AI call 08-30 09:41 CT).

## TL;DR

- **One endpoint, one model, one row is called.** Every one of the 1,342 `Request URL` lines in the
  window is `https://api.deepseek.com/chat/completions`; every one of the 1,336 `ai_call` lines is
  `model=deepseek-v4-pro` (1,322 ok / 14 failed) [A][RUNTIME]. The only trader (`hoang`) is bound to
  ai_models row `8ef641a7-…_deepseek` ("DeepSeek AI"); the "DeepSeek 2" row has **zero** traders and
  zero planner bindings — nothing calls it [A][DB].
- **The failing "API" is our own 600 s whole-request ceiling, not the provider.** 11 of the 14 failures
  are `http.Client.Timeout` kills at exactly 600,000–600,001 ms while the model was still streaming
  reasoning (71k–140k reasoning chars already received, ttfb 474–578 ms) [A][RUNTIME]. The other 3
  are TCP resets mid-stream (2 planner, 1 executor), **all recovered by the client retry** [A].
  0 × HTTP 4xx/5xx, 0 × 429, 0 × auth, 0 × DNS/TLS, 0 × idle-watchdog kills [A].
- **Who gets killed:** 11 of 80 max-reasoning full-author/re-author planner attempts (13.8 %);
  0 of 22 repair attempts; 0 of 42 fast-reasoning attempts [A]. Successful max full reads:
  n=69, p50 447.8 s, p90 552.0 s, p95 581.1 s, max 599.5 s — right-censored at the ceiling [A].
- **Root defect [CODE]:** the planner-speed wave (2026-08-31) put the planner on SSE with a 30 s idle
  watchdog and documented "the whole-request ceiling stays http.Client.Timeout (600s) — a live-but-slow
  stream is never killed" (`kernel/planner_speed.go:23-24`, `mcp/client.go:951-953`, guide
  `settings.ts:159`). Both halves cannot be true: `http.Client.Timeout` bounds the body read, so the
  live stream dies at 600 s. The 4.4 fixture used `Timeout: 10s` against a 0.5 s stream and never
  crossed the ceiling (`mcp/stream_watchdog_test.go:24`).
- **Fix (class 37):** the planner stream gets its own ceiling `AI_PLAN_TOTAL_DEADLINE_SECS`
  (default 1200 s, from the distribution: 2× max observed success; ≈ the 65,536-token cap at the median
  65 tok/s) on a per-call copy of the HTTP client with `Timeout=0`; the 600 s ceiling stays on every
  non-stream path (executor loop, weekly read, Ask-Planner). Every failed `ai_call` now carries
  `class=` + `http_status=` + `request_id=`; the planner logs `provider_row=` on failure; boot lines
  print idle/total/ceiling/retries/row.

## B1 — Outbound APIs on the planner path

| # | Call | Where | Resolved target (runtime, not file) | Used by |
|---|---|---|---|---|
| 1 | DeepSeek chat completions, **SSE stream** | `trader/auto_trader_planner.go:972` → `mcp/client.go` `CallWithRequestStreamRetry` | `https://api.deepseek.com/chat/completions`, model `deepseek-v4-pro` (boot: "DeepSeek using default BaseURL / default Model"), 96 stream requests in window | session planner reads (scheduled + wake + re-plan), attempts 1-3 |
| 2 | DeepSeek chat completions, **non-stream** | `mcp/client.go` `CallWithMessages` → `Call` | same URL/model, 1,246 requests | executor loop (2-min cadence), weekly read (`auto_trader_weekly.go:172`), Ask-Planner / realign (`api/handler_plan.go:1411,2150`), planner before 08-31 09:39 |
| 3 | ForexFactory calendar JSON | `calendar/calendar.go:40` `https://nfs.faireconomy.media/ff_calendar_thisweek.json` | feeds the T1 red-news lines into the planner prompt | 193 `calendar` WARN lines in window are the FAIL-CLOSED static fallback for the 08-30 slice — no HTTP failure lines; not on the failure path |
| 4 | Custom external data sources | `kernel/engine.go:770` `fetchSingleExternalSource` | none configured (no `external*` table with rows) | — |
| 5 | Internal HTTP `/api/plan/*` | `api/handler_plan.go` | called by the UI, never by the planner; 9 × `POST /api/plan/reset` in window (owner resets), 0 × ask/realign | — |
| — | NT8 TCP | excluded per B1 | — | bars feed the prompt; no failure in this window on the planner path |

Provider-row resolution [A][RUNTIME `trader/auto_trader_planner.go:64-110`]: every read logs
`🧠 planner model: empty binding → using primary, pinned "deepseek-v4-pro"` — `day_plan.planner_model`
is empty in all 9 strategies rows, so the planner uses the trader's primary client (row #1 below).

## B2 — Failure table (last 72 h; every `ai_call … ok=false`)

Source: `journalctl -u vl --since "2026-08-29 00:00" | grep "ai_call .*ok=false"` (14 lines) joined
to the `🧠 planner call (…) completed in`, `📊 AI call complete (stream)`, `📐 planner attempt`,
`⚠️ … retrying` lines and to `plans` / `planner_rejected_prompts` rows. Trigger class comes from the
plan row the read wrote (none written = wake/re-plan that kept the prior plan). Provider row for every
row: `8ef641a7-…_deepseek` (the only one called). Reasoning mode for every planner row: `enabled/max`.

| # | CT time | pid / rev | path | read · attempt | trigger class | duration ms | ttfb ms | reasoning chars (rate) | tokens in/out | finish | HTTP | error (verbatim) | client retry | outcome |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| K1 | 08-30 21:20:01 | 746955 / cd1d1de3 | non-stream | ASIA(08-30) wake · 1/3 | level wake (no write) | 600000 | – | – | est ~6.4k / – | n/a | 200 (headers) | `failed to read response: context deadline exceeded (Client.Timeout or context cancellation while reading body)` | none (not retryable) | attempt 2 21:29:12 552.0 s parse-reject · attempt 3 21:38:12 539.5 s reject → read exhausted, prior plan kept |
| K2 | 08-30 23:30:11 | 848146 / 06f1dc4e | non-stream | ASIA wake · 3/3 | level wake (no write) | 600001 | – | – | – | n/a | 200 | same text | none | attempts 1-2 parse-rejected (454.6 s, 475.4 s) → exhausted, no write |
| K3 | 08-31 00:58:42 | 932579 / 59dc9460 | non-stream | ASIA read · 2/3 | → `planner_fail_closed` | 600000 | – | – | – | n/a | 200 | same text | none | attempt 3 01:06:50 488.5 s reject → **plans 2026-08-30:ASIA v3 planner_fail_closed @01:06:50** |
| T1 | 08-31 08:44:54 | 1123319 / e86ae805 | non-stream | NY read · 3/3 | → `planner_fail_closed` | 272226 | – | – | – | n/a | 200 | `failed to read response: read tcp 10.0.0.141:45938->3.173.21.63:443: read: connection reset by peer` | `retrying (2/2)` 08:44:56 → ok in 143 s | attempt wall 417.0 s, parse-reject → **NY v1 planner_fail_closed @08:47:19** (3 validator rejects; the reset cost time only) |
| K4 | 08-31 09:22:11 | 1123319 / e86ae805 | non-stream | NY read · 1/3 | → `planner_fail_closed` | 600000 | – | – | – | n/a | 200 | Client.Timeout text | none | attempts 2-3 rejected (290.7 s, 375.9 s) → **NY v2 planner_fail_closed @09:33:18** |
| K5 | 08-31 10:35:38 | 1224017 / 5bf48951 (speed wave) | **stream** | NY wake · 1/3 | wake (aborted) | 600000 | 512 | 126,768 (211/s) | est 6354 / 0 | n/a | 200 | `stream interrupted: context deadline exceeded (Client.Timeout or context cancellation while reading body)` | none | read aborted by the 10:39:29 restart (pid 1252813) |
| K6 | 08-31 10:51:29 | 1252813 / 2bc58ed9 | stream | NY wake · 1/3 | wake (no write) | 600001 | 562 | 140,177 (233/s) | est 6411 / 0 | n/a | 200 | same | none | attempt 2 10:57:35 366.6 s parse-reject · attempt 3 10:58:55 79.7 s reject → no write |
| K7 | 08-31 11:53:29 | 1252813 | stream | NY wake · 1/3 | → `level_event` | 600000 | 499 | 134,792 (224/s) | est 6361 / 0 | n/a | 200 | same | none | attempt 2 11:58:55 325.9 s **accepted → NY v4 @11:58:55** (plan landed 15.5 min after the wake) |
| K8 | 08-31 14:27:28 | 1252813 | stream | NY wake · 1/3 | → `level_event` | 600001 | 493 | 133,667 (222/s) | est 6336 / 0 | n/a | 200 | same | none | attempt 2 parse-reject · attempt 3 14:34:06 108.3 s **accepted → NY v7** (16.6 min) |
| T2 | 09-01 01:46:33 | 1625428 / fef656a4 | stream | LONDON(09-01) read · 3/3 | → `planner_fail_closed` | 211443 | 504 | 42,482 | 0 / 0 | n/a | 200 | `stream interrupted: read tcp 10.0.0.141:47328->3.173.21.63:443: read: connection reset by peer` | `AI API stream failed, retrying (2/2)` 01:46:35 → ok 175.9 s | attempt wall 389.4 s, reject → **LONDON v1 planner_fail_closed @01:49:31** (3 validator rejects) |
| K9 | 09-01 06:00:01 | 1625428 | stream | LONDON wake · 1/3 | wake (no write) | 600000 | 578 | 134,322 (223/s) | est 6531 / 0 | n/a | 200 | Client.Timeout text | none | attempt 2 534.1 s parse-reject · attempt 3 38.5 s parse-reject → no write |
| T3 | 09-01 06:10:36 | 1625428 | non-stream (**executor loop, not planner**) | – | – | 31535 | 0 | 7,092 (**stale**, inherited from the 06:09:33 stream) | – | n/a | – | `failed to read response: read tcp 10.0.0.141:46622->3.173.21.63:443: read: connection reset by peer` | `AI API call failed, retrying (2/2)` 06:10:38 → ok | executor cycle recovered |
| K10 | 09-01 12:33:07 | 1625428 | stream | NY wake · 1/3 | wake (no write) | 600000 | 474 | 73,196 (**121/s**) | est 6442 / 0 | n/a | 200 | Client.Timeout text | none | → K11 |
| K11 | 09-01 12:43:07 | 1625428 | stream | same read · 2/3 (re-author carrying K10's error text as the "validator reason") | wake (no write) | 600000 | 500 | 71,414 (**119/s**) | est 6544 / 0 | n/a | 200 | same | none | attempt 3 12:52:48 581.1 s (158/s) validator-reject → no write; `planner_rejected_prompts` ids **71, 72** store the transport text as a reject reason |

Notes [A]: "200 (headers)" — every kill had a normal ttfb, i.e. the provider had answered and was
streaming; the HTTP status is not printed by the old binary (fixed in Phase 2). Pre-speed-wave rows
(K1-K4, T1) ran the non-stream path and carry no ttfb/reasoning fields (the `client.go:225` format).
The **599.5 s success** the dispatch cites is 08-31 17:10:03 (ASIA read attempt 1, `wall_ms=599526`,
`finish_reason=stop`, ttfb 2218 ms) — 474 ms under the ceiling.

## B3 — Classification

| Class | n | First / last | Provider row | Path | Notes |
|---|---|---|---|---|---|
| **600 s total-deadline kill (`http.Client.Timeout`) on a LIVE stream** | **11** | 08-30 21:20:01 / 09-01 12:43:07 | 8ef641a7-…_deepseek | 4 non-stream (pre 08-31 09:39) + 7 stream | all planner, all `reasoning=max`, all full-author/re-author attempts; reasoning still arriving (71k-140k chars) |
| Transport reset mid-stream (`connection reset by peer`, peer 3.173.21.63:443, local 10.0.0.141) | 3 | 08-31 08:44:54 / 09-01 06:10:36 | same | 2 non-stream + 1 stream | 2 planner + 1 executor; **3/3 recovered** by the client retry (`retries=2`, backoff 2 s) |
| 30 s idle-deadline kill (`timeout_source=context`) | 0 | — | — | — | never fired: reasoning chunks arrive continuously |
| HTTP 4xx auth/key | 0 | — | — | — | grep `status 40|401|403|unauthorized|invalid api key` over MCP/ai_call lines = 0 |
| HTTP 429 / 5xx | 0 | — | — | — | grep `status 429|5[0-9][0-9]|rate.?limit` = 0 |
| DNS / TLS / connection-level | 0 | — | — | — | grep `no such host|tls:|handshake` = 0 |
| Schema / validator rejects (NOT API failures) | 20 stored rows (ids 58-77) in 72 h, of which 2 (71, 72) are actually K10/K11's transport text | — | — | — | separated: validator work; 7 of the 9 `planner_fail_closed` writes in the window were pure validator exhaustion with zero API failure |
| Wrong base URL / model string on a row | 0 | — | — | — | both rows resolve to defaults (B6) |

## B4 — Which one keeps failing

The DeepSeek chat/completions endpoint at `https://api.deepseek.com`, model `deepseek-v4-pro`, row
`8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek` ("DeepSeek AI") — because it is the only one called
(n = 1,336 ai_call, 14 failures, 1.0 %). Within it the failures are **one class**: 11 of 14 are the
600 s ceiling on live max-reasoning planner streams (n = 80 such attempts → 13.8 % kill rate); the
remaining 3 are recovered TCP resets. The "DeepSeek 2" row (`396db319-…_deepseek_0751c0b6`) is not
called by anything: `traders.ai_model_id LIKE '%0751c0b6%'` → 0 rows; the trader that once used it
(the 08-18 audit's "other trader 0751c0b6") no longer exists in `traders`.

## B5 — Candidate verdicts

| Cand. | Verdict | Evidence |
|---|---|---|
| (a) 600 s total deadline too short for max-reasoning authors | **CONFIRMED [A]** | 11/80 max full attempts killed at 600000-600001 ms with reasoning chars still growing (5 of the 7 stream kills at the normal 211-233 chars/s → the model simply needed > 600 s; successes reached 135,262 chars in 484 s). Success distribution p50 447.8 / p90 552.0 / p95 581.1 / max 599.5 s is right-censored. Repair (n=22) and fast (n=42) attempts: 0 kills. |
| (b) 30 s idle deadline trips during reasoning gaps | **RULED OUT [A]** for the window | 0 kills with `timeout_source=context`; every stream kill says `Client.Timeout`; ttfb 355-2218 ms; reasoning deltas stream continuously. |
| (c) second DeepSeek row misconfigured and routed to | **RULED OUT as a cause [A]; key content UNVERIFIABLE (A25)** | 0 traders bound; `day_plan.planner_model` empty in all 9 strategies; every read logs "empty binding → using primary"; both rows' non-key columns identical (B6). Whether row 2 holds an old key cannot be checked without decrypting it — irrelevant while nothing calls it. |
| (d) provider-side 429 / 5xx | **RULED OUT [A]**; provider **throughput variance is a CONTRIBUTING factor [B]** | 0 non-200 statuses. But reasoning throughput fell to 119-121 chars/s for K10/K11 (12:33-12:43) and 155-167 chars/s for the 12:52-17:23 reads vs 230-279 chars/s 08:00-12:06 the same day — at half speed a normal 95k-char read needs ~800 s, so the fixed ceiling kills more often when the provider is slow. |
| (e) retry arithmetic masks then exhausts | **RULED OUT [A]** (arithmetic quoted) | Planner loop = 3 attempts (`auto_trader_planner.go:1334`); client `MaxRetries` = `AI_MAX_RETRIES=2` (`.env:33`) → worst case 3 × 2 = 6 provider calls. Observed: the ceiling kill is **not** retried at the client level (`IsRetryableError` matches the lowercase token "timeout"; the Go error says "Client.Timeout" — case-sensitive, `mcp/client.go:33-49,606-615`; 0 `retrying` lines follow any of the 11 kills), so each kill costs exactly 600 s and one planner attempt. Only the 3 transport resets used the client retry (2/2), each once. Worst observed read: K10+K11+attempt 3 = 12:23:07 → 12:52:48 = 29 min 41 s. |
| (f) WSL2 proxy / network | **RULED OUT as the main cause [A]**; minor class | 3 resets in 1,336 calls (0.22 %), all `read: connection reset by peer` from 3.173.21.63:443 at 31-272 s into a call, all recovered; 0 DNS/TLS errors; keepalive 30 s (`security/url_validator.go:161`). |
| (g) malformed / oversized request | **RULED OUT [A]** | prompt estimate on killed attempts 6336-6544 tokens vs 6240-6691 on successes; provider-counted prompt on successes 9402-9935; identical request builder; `finish_reason=length` count 0 (`truncated-responses=0` at every boot). |

## B6 — Both DeepSeek rows, from the resolver (no keys)

Query: `SELECT id,user_id,name,provider,enabled,custom_api_url,custom_model_name,thinking_mode,reasoning_effort,length(api_key),created_at,updated_at FROM ai_models WHERE provider='deepseek'`.

| Column | `8ef641a7-…_deepseek` "DeepSeek AI" | `396db319-…_deepseek_0751c0b6` "DeepSeek 2" | Differ? |
|---|---|---|---|
| user_id | 396db319-… | 396db319-… | no |
| provider / enabled | deepseek / 1 | deepseek / 1 | no |
| custom_api_url → resolved base URL | "" → `https://api.deepseek.com` (boot line) | "" → same default | no |
| custom_model_name → resolved model | "" → `deepseek-v4-pro` | "" → same default | no |
| thinking_mode / reasoning_effort → resolved | "" / "" → env defaults `enabled` / `max` (`DEEPSEEK_THINKING_MODE`, `AI_REASONING_EFFORT` unset) | same | no |
| api_key | length 92, AES-GCM ciphertext (random nonce, `crypto/crypto.go:206-215`) | length 92 | ciphertexts differ — proves nothing (nonce); **not compared further (A25)** |
| created_at | 2026-05-30 03:16:52Z | 2026-08-08 13:00:39Z | yes |
| updated_at | 2026-08-29 22:38:46.4338Z | 2026-08-29 22:38:46.4337Z | same instant (bulk re-encrypt on 08-29) |
| bound traders | 1 — `hoang` (`8d5c8af5_…_deepseek_1781246265`, the only traders row, `is_running=1`) | **0** | — |
| planner binding | primary (all `day_plan.planner_model` empty) | none | — |

Client config in force (resolved [CONFIG] `.env` via godotenv + boot line 09-01 17:24:00 of the running
binary ec6632f9; the process env is not readable from this shell, so `.env` is the resolved source):
`AI_HTTP_TIMEOUT_SECONDS=600` (`.env:32`) → `http.Client.Timeout` 600 s and `timeout=600s` on the boot
line · `AI_MAX_RETRIES=2` (`.env:33`) → `retries=2` · backoff 2 s (default) · `AI_MAX_TOKENS=32768`
(`.env:19`, executor cap) · `AI_PLAN_MAX_TOKENS` unset → 65536 · `AI_PLAN_STREAM_IDLE_SECS` unset → 30 s
· `RETRY_MODE` unset → repair · `AI_PLAN_REASONING` unset → max · `FAST_MARKET_REASONING` unset → low.
The boot-time masked key fingerprint line exists in the journal and is deliberately not quoted here.

## B7 — Root cause (one paragraph)

The planner's SSE call still runs under `http.Client.Timeout = AI_HTTP_TIMEOUT_SECONDS = 600 s`
[CONFIG `.env:32`; CODE `mcp/config.go:69,76` builds the transport with `ResolvedAITimeout()`, and
`mcp/client.go:1005` issues the stream request through that same `client.HTTPClient`], while the
speed-wave code and guide assert the opposite ("a live-but-slow stream is never killed" —
`kernel/planner_speed.go:23-24`, `mcp/client.go:951-953`, `web/src/guide/content/settings.ts:159`)
[CODE]. `http.Client.Timeout` bounds the body read, so any max-reasoning attempt still streaming at
600.0 s dies with `stream interrupted: context deadline exceeded (Client.Timeout …)` — 11 of 80 such
attempts between 08-30 21:20 and 09-01 12:43 CT, each with a normal ttfb and 71k-140k reasoning
chars already received [RUNTIME]. The max-reasoning duration distribution sits directly against the
ceiling (successes p50 447.8 s, p95 581.1 s, max 599.5 s; the kills needed more) [RUNTIME], and
provider throughput variance (119-121 chars/s at 12:33-12:43 vs 211-279 chars/s elsewhere) pushes
more reads over it [RUNTIME, B]. Nothing else fails: 0 non-200 statuses, 0 auth, 0 429, 0 idle-kills,
and the second DeepSeek row is unbound [RUNTIME][DB]. The 3 TCP resets are a separate, minor,
self-healing class. Because the kill is not retryable at the client level (case-sensitive token
mismatch) and the planner loop treats it like a validator reject, each kill costs exactly one of the
three attempts plus 600 s, and its transport text is fed back to the model as a "validator reason"
(`planner_rejected_prompts` 71-72) — without a class token on the `ai_call` line, which is why it
read as "the API keeps failing".

## Secondary findings (observability, not fixed unless stated)

1. **Stale telemetry on failure [A]** — T3's `ai_call` line reports `reasoning_chars=7092 ttfb_ms=0`
   inherited from the previous (planner) call because the atomics are only written on success
   (`mcp/client.go:1013-1015`). **Fixed in Phase 2** (`resetCallTelemetry` at call start).
2. **Timeout text fed to the model as a validator reason [A]** — `runPlannerReadCoreWithFactsGrades`
   handles `call()` errors and validator rejects identically (`auto_trader_planner.go:1348-1354`):
   `plannerRejectBlock(lastErr)` puts "stream interrupted: context deadline exceeded…" into attempt 2's
   prompt tail and `SaveRejectedPrompt` stores it (ids 71, 72). **NOT changed** — Section F forbids
   planner-prompt changes in this dispatch; owner ruling requested (skip the reject block for
   transport/deadline classes; tag the stored row with the class).
3. **Client-level retry never covers the ceiling kill [A]** — `retryableErrors` contains "timeout",
   the Go error says "Client.Timeout" (case-sensitive `strings.Contains`). Kept as-is on purpose
   (retrying a 600 s kill would double the loss; the planner loop owns that retry) and **pinned by a
   test in Phase 2** so a future "fix" cannot silently make it 3 × 2 × 600 s.
4. **Fixture gap [A]** — `TestStreamSlowButAliveSurvivesIdle` used `http.Client{Timeout: 10s}` with a
   ~0.5 s stream; the ceiling was never crossed, so the false "never killed" claim passed CI. Phase 2
   adds the crossing fixture.


## Phase 2 — FIX (class 37), shipped to branch, PARKED before cutover

Gate check: B7 = CONFIRMED; the fix is a split of one deadline plus observability (14 files, +639/−30,
no prompt/validator/schedule change). Lock: `~/vl-main.lock` acquired (owner hoang/claude, pid 1906840
alive, expiry 21:35 CT, task planner-api-failure-0901); worktree `~/vl-planner-api` on branch
`fix/planner-stream-total-deadline` (locked), base `origin/dev` = `795f67f7`. **The main tree was never
edited** (porcelain clean throughout; only the ignored `vl-bin.next` was staged).

### Commits on the branch

| Commit | What |
|---|---|
| `638af5ed` | this report, Phase 1 |
| `75130d59` | the fix + tests + guide + checklist (below) |
| `225bc367` | deploy marker — `deploy/RELEASE` and `GUIDE_BUILT_REV` = `75130d593c2ae2a98abe4500c62f859111475bef` |

### The change, file:line (at `75130d59`)

| File | Lines | Change |
|---|---|---|
| `kernel/planner_speed.go` | 22-59 | `PlannerStreamTotalSeconds()` — `AI_PLAN_TOTAL_DEADLINE_SECS`, default **1200**, junk → default, `total <= idle → idle + 60`; the false "never killed" comment replaced |
| `mcp/client.go` | 211-306 | sentinels `ErrStreamIdleDeadline` / `ErrStreamTotalDeadline`; `classifyAIError` (total_deadline · idle_deadline · client_timeout · http_status · auth_config · transport · context · other); `requestIDFrom` (X-Request-Id / Request-Id / X-Amzn-Requestid / Cf-Ray); `resetCallTelemetry`; accessors `LastErrClass` / `LastHTTPStatus` / `LastRequestID` |
| `mcp/client.go` | 322-357 | `logAICall`: `timeout_source` gains `planner_total` / `stream_idle`; failure line gains `class= http_status= request_id=`; success line gains `http_status= request_id=` |
| `mcp/client.go` | 509, 531-532 | non-stream `Call`: telemetry reset at start; status + request id captured |
| `mcp/client.go` | 1071-1223 | `CallWithRequestStreamIdle` → thin wrapper (total 0 = legacy ceiling); **`CallWithRequestStreamDeadlines(req, onChunk, idle, total)`**: `context.WithTimeoutCause(total)` on a shallow copy of `HTTPClient` with `Timeout=0` (line 1126; shared Transport, shared client untouched), idle watchdog via `WithCancelCause`; `wrapStreamDeadlineErr` names the killer from `context.Cause` and appends `ctx.Err()` so the legacy "context canceled" grep survives |
| `mcp/client.go` | 1226-1268 | `CallWithRequestStreamRetry` → wrapper; **`CallWithRequestStreamRetryDeadlines`** (same retry flow; deadline kills are not retryable by construction) |
| `trader/auto_trader_planner.go` | 972-976 | planner calls `CallWithRequestStreamRetryDeadlines(req, nil, plannerStreamIdle(), plannerStreamTotal())` |
| `trader/auto_trader_planner.go` | 984-992 | on error: `🛰 planner call FAILED class=… provider_row=… model=… http_status=… request_id=… elapsed=… idle=… total=…` (row id, never the key); `🧠 planner call (…) completed in` line gains `total=%ds` |
| `trader/auto_trader_planner.go` | 1283-1302 | `plannerStreamTotal()`, `plannerClientBootLine()` (pure), `logPlannerClientBootLine()` |
| `trader/auto_trader_dayplan.go` | 74 | per-trader boot: `🛰 planner client: provider_row=<row> stream_idle=30s stream_total=1200s (AI_PLAN_TOTAL_DEADLINE_SECS) http_ceiling=600s (non-stream paths only) retries=2 backoff=2s cap=65536` |
| `trader/auto_trader.go` · `manager/trader_manager.go` | 235 · 602 | `AutoTraderConfig.AIModelID` (the ai_models row id) filled from `aiModelCfg.ID` |
| `kernel/levels_volume_boot.go` | 23-24 | `🚀 planner speed wave … stream_idle=%ds stream_total=%ds (class 37: planner ceiling split from the HTTP ceiling)` |
| `main.go` | 136-137 | `🧠 AI params in force: … timeout=600s (HTTP ceiling; non-stream paths) planner_stream_idle=30s planner_stream_total=1200s retries=2 …` |
| `web/src/guide/content/settings.ts` | 157-190 | idle entry's false sentence corrected; new knob **Planner stream total deadline** (what / evidence / consumer / range / default 1200 / when to touch) |
| `web/src/guide/content/status.ts` | 19-20 | boot ledger lines updated (`stream_total=1200s`, `🛰 planner client`) |
| `docs/superpowers/AUDIT-CHECKLIST.md` | 309-345 | **class 37** appended (36 is held by the concurrent dispatch) |

Behaviour unchanged, on purpose: `http.Client.Timeout` = 600 s still bounds every non-stream call
(executor loop, weekly read, Ask-Planner, realign) and every legacy `CallWithRequestStream` caller;
`AI_MAX_RETRIES=2`, backoff 2 s, `MaxRetryTimes` unchanged; reasoning mode / cap unchanged (owner
ruling pending); planner prompts, validators and read schedule untouched (Section F).

### Why 1200 s (C1 — from the distribution, not a blind raise)

- Successful max full/re-author attempts (n = 69): p50 447.8 s · p90 552.0 s · p95 581.1 s · max 599.5 s —
  right-censored at 600, so the true p95 is above 581 s.
- Killed attempts carried 71k-140k reasoning chars at 600 s; the largest *successful* read had 135,262
  chars in 484 s. Completion throughput on successes: p10 48.9 · p50 65.3 · p90 75.5 tok/s → the
  65,536-token planner cap is ≈ 1,000 s at the median and ≈ 1,340 s at p10.
- 1200 = 2× the observed max success; covers the cap at median throughput; the two provider-slow kills
  (K10/K11 at ~120 chars/s, 73k chars) would have needed ≈ 800-1,000 s. Anything above 1200 trades
  plan latency for completion odds — that is the owner's ruling (worst-case read wall = 3 × 1200 s = 60 min
  vs 30 min today). The knob is env-only: `AI_PLAN_TOTAL_DEADLINE_SECS=600` restores today's value with the
  new class logging (a "soft rollback" without a binary swap).
- The 30 s idle deadline is kept unchanged — candidate (b) was RULED OUT (0 idle kills; ttfb ≤ 2.2 s; the
  idle timer also covers the dial/queue phase before the first byte, as the autopsy intended).

### Tests (A8/C6 — written to fail on the old code, all green) [A]

`go test ./mcp ./kernel ./trader -count=1` → ok. Full suite `go test ./... -count=1`: first run (pre-fix
mcp) 26 ok / 1 FAIL (the 3 mcp tests fixed below); re-run on `75130d59`: see Closeout.

| Test | Pins |
|---|---|
| `mcp.TestClass37LiveStreamBeyondHTTPTimeoutSurvivesUnderTotal` | a 720 ms live stream vs `http.Client.Timeout=300ms` completes under total=5 s; the shared client's Timeout is not mutated; `request_id`/status captured; success line carries `request_id` — **fails on the pre-fix code** (Client.Timeout killed it) |
| `mcp.TestClass37LegacyIdleCallKeepsHTTPTimeoutCeiling` | total=0 path still dies at Client.Timeout, error text carries `Client.Timeout`, class `client_timeout`, `ai_call … class=client_timeout`, **not retryable** (the observed 11-kills/0-retries behaviour, pinned) |
| `mcp.TestClass37TotalDeadlineAbortsWithClass` | endless live stream dies at total=400 ms with `ErrStreamTotalDeadline`, `class=total_deadline`, no client-level retry, `LastErrClass` reads it |
| `mcp.TestClass37IdleDeadlineClass` | silent stream dies with `ErrStreamIdleDeadline`, legacy `context canceled` text preserved, `class=idle_deadline` |
| `mcp.TestClass37FailureResetsStaleTelemetry` | after a successful call, a hijack-closed transport failure reports chars=0 ttfb=0 status=0 id="" and class `transport` (the T3 defect) |
| `mcp.TestClass37ClassifyAIErrorTable` | the verbatim 08-30..09-01 journal strings → classes (both Client.Timeout shapes, both TCP-reset shapes, 429/502, no-such-host, key-not-set, bare cancel, sentinels) |
| `mcp.TestStreamIdleSilenceAborts` (existing) | still green: Go ≥1.21 returns `context.Cause` as the read error, so `wrapStreamDeadlineErr` appends `ctx.Err()` explicitly |
| `kernel.TestClass37PlannerStreamTotalDefaultAndOverride` / `…AlwaysExceedsIdle` | default 1200, idle default 30 unchanged, override 900, junk → 1200, total ≤ idle → idle + 60 |
| `trader.TestClass37PlannerClientBootLineWording` / `…StreamTotalResolvedInTrader` | boot-line tokens (row, idle, total, ceiling, retries, cap; never `sk-`), resolver reaches the trader |

Fixture note: the first version of the stale-telemetry test dialed `127.0.0.1:1` expecting an instant
refusal; on WSL2 the connect hung and the 1 s idle watchdog fired first (class `idle_deadline`). Replaced
with a hijack-and-close server — deterministic, and a reminder that the idle timer also bounds the dial.

### Build, stage, rollback (A4/A13) [A]

```
git clone --no-local ~/vl-planner-api <scratch>/clone-75130d59 && git checkout 75130d593c2ae2a98abe4500c62f859111475bef
go build -o vl-bin .
go version -m vl-bin:
	build	vcs.revision=75130d593c2ae2a98abe4500c62f859111475bef
	build	vcs.time=2026-09-01T22:53:33Z
	build	vcs.modified=false
sha256 8473a5f255c50980c491ddfa5159d278bda16183a3e62661404b1a4cb745e119  (70,899,768 bytes)
cp → /home/hoang/vl/vl-bin.next        # staged 2026-09-01 ~18:00 CT; no vl-bin.next existed (class 35's was consumed at 17:23:54)
```
Frontend: `npm run build` in the worktree → `✓ built in 4.90s` (guide content compiles). The served
`web/dist` in `~/vl` is untracked and must be rebuilt at cutover (`cd ~/vl/web && npm run build`) or the
guide keeps the old text while the drift banner shows.

Running binary (unchanged by this dispatch): `/home/hoang/vl/vl-bin` = `ec6632f9` (class 35, PID
1908258, boot 17:24:00 CT). Between `ec6632f9` and `75130d59`: `4d88b46e`, `795f67f7` (docs only),
`638af5ed` (docs), `75130d59` (this fix) — one non-docs commit.

**Cutover (owner GO only; flat window outside 16:45-17:10 CT; no planner read in flight; no live arms):**
```
# A5 flat-gate quadruple + A6 in-flight, quoted fresh:
sqlite3 -readonly ~/vl/data/data.db "SELECT COUNT(*) FROM trader_positions WHERE status='OPEN'"      # expect 0
curl -s -H "Authorization: Bearer $JWT" 'http://127.0.0.1:8080/api/positions?trader_id=<id>&account=Sim101'   # expect []
journalctl -u vl --since -5min | grep 'positions snapshot'                                                 # expect count=0
curl -s -H "Authorization: Bearer $JWT" 'http://127.0.0.1:8080/api/open-orders?trader_id=<id>&account=Sim101&symbol=MNQ'   # expect []
curl -s -H "Authorization: Bearer $JWT" 'http://127.0.0.1:8080/api/plan/today?trader_id=<id>&symbol=MNQ' | grep -o 'replan_in_flight[^,]*'   # expect false
journalctl -u vl --since -20min | grep -E 'planner model:|planner call \('                                 # last 'planner model' must be followed by its 'planner call' line (no read in flight)
# swap — one boot, one marker (RELEASE already = 75130d59 on the branch; merge first so ~/vl/deploy/RELEASE matches):
cd ~/vl && git merge --ff-only fix/planner-stream-total-deadline   # or merge the PR, then git pull --ff-only
cd ~/vl/web && npm run build
cd ~/vl && cp vl-bin vl-bin.prev.boot && mv vl-bin vl-bin.old.ec6632f9 && mv vl-bin.next vl-bin && kill -9 $(systemctl show -p MainPID --value vl)
# within 90 s expect:
#   🔐 BOOT INTEGRITY OK — rev 75130d59 · expected 75130d59 · goldens PASS
#   🧠 AI params in force: … timeout=600s (HTTP ceiling; non-stream paths) planner_stream_idle=30s planner_stream_total=1200s …
#   🚀 planner speed wave (2026-08-31): retry=repair stream=on stream_idle=30s stream_total=1200s (class 37 …)
#   🛰 planner client: provider_row=8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek stream_idle=30s stream_total=1200s …
```
**Rollback (exact):**
```
cd ~/vl && mv vl-bin vl-bin.bad.75130d59 && cp vl-bin.prev.boot vl-bin && printf 'ec6632f9de41060b52398f41f9ffbbf840814c40' > deploy/RELEASE && kill -9 $(systemctl show -p MainPID --value vl) && git checkout -- deploy/RELEASE web/src/guide/types.ts
# soft rollback of the VALUE only (keeps the new class logging): echo 'AI_PLAN_TOTAL_DEADLINE_SECS=600' >> ~/vl/.env && kill -9 $(systemctl show -p MainPID --value vl)
```

### Proof (D) — the true proof has NOT yet occurred

The proof is the next max-reasoning planner read completing **past 600 s** under the new binary, or a
`class=total_deadline` line replacing today's `Client.Timeout` text. The binary is staged, not running;
no cutover was performed (A3 — owner GO required; the 16:45-17:10 window has passed but the ASIA read
chain is live). Until then the only proof is fixture-level: `TestClass37LiveStreamBeyondHTTPTimeoutSurvivesUnderTotal`
crosses a real `http.Client.Timeout` on a real TCP stream and passes only on the new code.

### What the owner will STILL see wrong (A15)

- **Until cutover:** the running `ec6632f9` still kills every max-reasoning attempt that needs > 600 s
  (`stream interrupted: context deadline exceeded (Client.Timeout …)`, ~14 % of full reads), its `ai_call`
  failure lines carry no `class=`, a failed executor call can still print the previous call's
  `reasoning_chars`, and the guide drift banner shows (`GUIDE_BUILT_REV` = `75130d59` ≠ running rev).
- **After cutover:** a slow max read may legitimately run up to 20 min per attempt (60 min per 3-attempt
  read) before the plan lands — plans on the slow tail arrive later than today's 10-min kill, by design;
  lower `AI_PLAN_TOTAL_DEADLINE_SECS` or rule on reasoning effort if open−30 timing matters more.
- **Not fixed (Section F):** a deadline/transport failure is still fed to attempt 2 as a "validator
  reason" (`plannerRejectBlock`) and stored in `planner_rejected_prompts` (rows 71-72 today) — the
  offline A/B set is polluted by non-validator rows. Owner ruling requested: skip the reject block and
  tag the stored row for `class ∈ {total_deadline, idle_deadline, client_timeout, transport}`.
- **Not fixed:** `IsRetryableError` still lists the dead token "timeout" (never matches a Go
  `Client.Timeout` error) — left alone on purpose and pinned by `TestClass37LegacyIdleCallKeepsHTTPTimeoutCeiling`.
- The per-call telemetry atomics are shared by the executor and planner goroutines (pre-existing); a
  reset by one during the other's *final store→log* microsecond window could still cross — accepted.
- **Owner-side, not this dispatch:** whether the unused "DeepSeek 2" row holds a stale key (A25 — cannot
  be checked without decrypting); delete or re-key it in Settings.

## Appendix B — queries and commands behind every figure (A21)

```
# failure lines (14) and their neighbours
journalctl -u vl --since "2026-08-29 00:00" --no-pager -o short-iso | grep -E "ai_call |planner call \(|planner attempt|AI API (stream|call) failed|📊 AI call complete|Request URL \(stream|🧩 planner|📐 planner|🗓️|prompt render"
# counts by model/outcome and by URL
journalctl -u vl --since "2026-08-29 00:00" --no-pager | grep -o "ai_call model=[^ ]* .*ok=[a-z]*" | sed -E 's/duration_ms=[0-9]+ //; s/finish_reason=[^ ]+ //' | awk '{print $1,$2,$NF}' | sort | uniq -c
journalctl -u vl --since "2026-08-29 00:00" --no-pager | grep -o "Request URL[^:]*: https://[^ ]*" | sort | uniq -c
# non-200 / auth / rate-limit / DNS / TLS / idle sweep (0 hits)
journalctl -u vl --since "2026-08-29 00:00" --no-pager | grep -E "MCP|ai_call|planner" | grep -iE "status [45][0-9][0-9]|HTTP [45][0-9][0-9]|rate.?limit|429|context canceled|idle timeout|no such host|tls:|handshake|401|403|unauthorized|invalid api key"
# boots / revs
journalctl -u vl --since "2026-08-29 00:00" --no-pager -o short-iso | grep -E "BOOT INTEGRITY|AI params in force|Request URL \(stream"
# DB (read-only)
sqlite3 -readonly data/data.db "SELECT id,user_id,name,provider,enabled,custom_api_url,custom_model_name,thinking_mode,reasoning_effort,length(api_key),created_at,updated_at FROM ai_models"
sqlite3 -readonly data/data.db "SELECT id,name,ai_model_id,exchange_id,strategy_id,is_running FROM traders"
sqlite3 -readonly data/data.db "SELECT id, json_extract(config,'$.day_plan.planner_model') FROM strategies"
sqlite3 -readonly data/data.db "SELECT COUNT(*) FROM traders WHERE ai_model_id LIKE '%0751c0b6%'"
sqlite3 -readonly data/data.db "SELECT (a.api_key=b.api_key),(a.custom_api_url=b.custom_api_url),(a.custom_model_name=b.custom_model_name),(a.thinking_mode=b.thinking_mode),(a.reasoning_effort=b.reasoning_effort) FROM ai_models a, ai_models b WHERE a.id='8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek' AND b.id='396db319-d3fe-4d63-9b97-516ff0008f53_deepseek_0751c0b6'"   # → 0|1|1|1|1
sqlite3 -readonly data/data.db "SELECT id,trader_id,trade_date,session,attempt,substr(reject_reason,1,140),created_at FROM planner_rejected_prompts WHERE created_at >= datetime('now','-72 hours') ORDER BY id"   # ids 58-77
sqlite3 -readonly data/data.db "SELECT version,trade_date,session,trigger_reason,lifecycle,datetime(created_at,'-5 hours') FROM plans WHERE created_at >= datetime('now','-72 hours') ORDER BY created_at"
# config in force
grep -nE "^(AI_|RETRY_MODE|DEEPSEEK_|FAST_MARKET)" .env      # keys redacted in the report
# per-attempt table + stats
python3 scratch/parse_attempts2.py journal_wide_72h.txt      # Appendix A; awk over it for the n=80/69/22/42 splits and p50/p90/p95
```

## CLOSEOUT ADDENDUM (18:0x CT) — supersedes the stage/cutover commands in §Phase 2

**Owner questions answered (mid-dispatch, ~17:58 CT).** (1) The main-tree lock was taken 17:35 CT when I
moved into Phase 2 on the dispatch's evidence gate (B7 CONFIRMED + small fix), **not on owner approval** —
released 17:59:06 CT; re-acquired for ~1 s at 18:00:19 CT only to check/unstage my own binary (see below),
released again. Phase 1 itself ran lock-free and read-only. Nothing in `~/vl` was edited by this dispatch
at any point (porcelain clean before/after; the only file I ever created there was the ignored
`vl-bin.next`, since replaced by class 36's). (2) Phase-1 report raw URL, pinned to the Phase-1 commit:
`https://raw.githubusercontent.com/johnwick2921-cyber/nofx/638af5ed/docs/superpowers/reports/2026-09-01-planner-api-failure.md`
→ HTTP 200 (51,940 bytes); branch-tip raw URL → HTTP 200.

**Concurrency surprise (A23, reported, not acted on beyond my own branch).** While this dispatch ran, class
36 (`17efeea9` + marker `7089d271` + reports `b2c2ff92`, `a1a6e255`) landed on `dev`, and at 17:59 CT the
class-36 dispatch staged ITS binary as `~/vl/vl-bin.next` (sha `d2f724a9…`) — i.e. my earlier staged
file (sha `8473a5f2…`) was already gone when I went to remove it; nothing of mine remains in `~/vl`.
PR #87 became CONFLICTING (`deploy/RELEASE`, `AUDIT-CHECKLIST.md` — both appended after class 35 —,
`web/src/guide/types.ts`). Resolution, on my branch only: merged `origin/dev` in (`e42a0b43`; checklist
keeps **36 then 37**, my "(36 is held by…)" note removed), rebuilt, re-marked.

**Combined build (36 + 37), clean clone at the merge rev [A]:**
```
vcs.revision=e42a0b43b4bead2c5d2207958d8a0bde2d65be11 · vcs.time=2026-09-01T23:00:31Z · vcs.modified=false
sha256 75746bb7c0b1c35ebd7bf15dd54edbb97f76094869df1a296b1ef93259728913  (70,905,488 bytes)
parked at ~/vl-planner-api/vl-bin.next   ← the WORKTREE, not ~/vl (class 36's stage is untouched)
marker 3c3f5465: deploy/RELEASE + GUIDE_BUILT_REV = e42a0b43…
```
Tests on the merge: `go test ./mcp ./kernel ./trader -count=1` ok · full suite `go test ./... -count=1`:
**27 packages ok, 0 FAIL, EXIT=0** (e42a0b43) · frontend `npm run build` ✓ (4.35 s).
PR #87 after the push: `mergeable=MERGEABLE` (state UNSTABLE = no CI checks configured).
Commit URLs (HTTP 200): `75130d59…` (fix), `225bc367` (first marker), `dad07771` (Phase-2 report),
`e42a0b43…` (merge), `3c3f5465` (combined marker); PR https://github.com/johnwick2921-cyber/nofx/pull/87 → 200.

**Sequencing for the owner (ONE BOOT, ONE MARKER — two valid orders):**
- **(a) class 36 first, class 37 later:** cut over class 36 from its own stage per its report; afterwards
  merge PR #87 into dev (`git merge --ff-only fix/planner-stream-total-deadline` in `~/vl` — the branch
  already contains dev), `cp ~/vl-planner-api/vl-bin.next ~/vl/vl-bin.next`, then the §Phase 2
  cutover block with `vl-bin.old.17efeea9` and the rollback's RELEASE = `17efeea9…`.
- **(b) one combined boot:** merge PR #87 first (RELEASE becomes `e42a0b43…`), replace
  `~/vl/vl-bin.next` with the combined build above (it contains class 36 + 37), one cutover, one boot
  line set: `BOOT INTEGRITY OK — rev e42a0b43 · expected e42a0b43`, `planner_stream_total=1200s`,
  `🛰 planner client: …`, plus class 36's own boot line. Rollback: `printf 'ec6632f9de41060b52398f41f9ffbbf840814c40' > deploy/RELEASE`
  and `cp vl-bin.prev.boot vl-bin`, `kill -9 $(systemctl show -p MainPID --value vl)`.
Either way: flat-gate quadruple + in-flight check + window first (§Phase 2), and note that at 17:53:33 CT an
ASIA read was in flight (attempt 3/3 re-author) under the running `ec6632f9`.

**Live surface right now (running ec6632f9, 17:24:00 boot):** the ASIA read at 17:38→17:47:32 CT took
532.3 s (attempt 1, parse-rejected), attempt 2 360.8 s (rejected) — both under the ceiling; the class-37
kill shape has not recurred since the cutover window, and will recur on the next > 600 s read until the
combined build runs.

**Worktree state at closeout:** `~/vl-planner-api` on `fix/planner-stream-total-deadline` @ `3c3f5465`
(unlocked, kept for the owner's cutover; ignored `vl-bin.next` inside), scratch clones in the session
scratchpad only. `~/vl-main.lock`: absent.

## CUTOVER — DONE (owner GO "GO 37", 2026-09-01) [A]

**Context correction vs the closeout addendum:** class 36 was cut over by its own dispatch at 18:01:06 CT
(`🔐 BOOT INTEGRITY OK — rev 17efeea9fc59 · expected 17efeea9 · goldens PASS`, PID 1941026), so the
running binary already carried class 36 and this cutover added **class 37 only** — sequencing option (b)
in the addendum, with 36 arriving ahead of it rather than in the same boot.

**Pre-flight, all quoted fresh at 21:19:43 CT (A5/A6/A7):**

| Gate | Value |
|---|---|
| A5 1/4 DB `trader_positions status='OPEN'` | **0** |
| A5 2/4 `GET /api/positions?…&account=Sim101` | **`[]`** |
| A5 3/4 NT8 positions snapshot (journal 21:19:xx) | **`count=0`** Sim101 · **`count=0`** SimAccount1 |
| A5 4/4 `GET /api/open-orders?…&symbol=MNQ` | **`[]`** |
| A6 `GET /api/plan/today` | **`"replan_in_flight":false`** · `active_session ASIA` · `lifecycle no_trade` · `version 3` · `replans_left 4` of `replan_cap 4` |
| A6 reads started vs completed since the last plan write | **0 vs 0** (no read in flight) |
| A7 live arms (`armed_orders` state not terminal) | **0** (19 cancelled · 9 filled) |
| A7 window | **21:19 CT** — outside 16:45-17:10; ASIA open, plan `no_trade` |

**A6 wait honoured:** at 21:13 CT a planner read WAS in flight (attempt 3/3 from 21:05:17). Per A6/class 33
the cutover waited; the attempt completed at 21:14:51 in **575.0 s** (rejected: `S4 breakdown_continue …`)
and the chain closed with `🗓️ PLAN written 2026-09-01 ASIA v3 … lifecycle no_trade` at 21:14:52. That
575.0 s attempt is one more near-miss on the old ceiling — 25 s of margin — measured on the very binary
being replaced.

**Merge + swap:**
```
~/vl-planner-api: git merge origin/dev (b7715a73, docs-only) → bc9ea126, RELEASE kept at e42a0b43, pushed
~/vl:             git merge --ff-only fix/planner-stream-total-deadline → dev = bc9ea126, pushed
                    RELEASE=e42a0b43  GUIDE_BUILT_REV=e42a0b43
                    cp ~/vl-planner-api/vl-bin.next → ~/vl/vl-bin.next
                    sha256 75746bb7c0b1c35e…  vcs.revision=e42a0b43…  vcs.modified=false  (70,905,488 bytes)
21:19:44 CT  cp vl-bin vl-bin.prev.boot && mv vl-bin vl-bin.old.17efeea9 && mv vl-bin.next vl-bin && kill -9 1941026
21:19:49 CT  systemd relaunched (Restart=on-failure) → PID 1994488
```

**Boot lines (A19 — one boot, one marker) [A]:**
```
21:19:49 🔐 BOOT INTEGRITY OK — rev e42a0b43b4be · built 2026-09-01T23:00:31Z · expected e42a0b43b4be · goldens PASS
21:19:49 🧠 AI params in force: model=deepseek-v4-pro client_max_tokens=32768 planner_max_tokens=65536
         temperature=0.50 top_p=omitted timeout=600s (HTTP ceiling; non-stream paths)
         planner_stream_idle=30s planner_stream_total=1200s retries=2 backoff=2s · truncated-responses=0
21:19:49 🚀 planner speed wave (2026-08-31): retry=repair stream=on stream_idle=30s stream_total=1200s
         (class 37: planner ceiling split from the HTTP ceiling) ttfb=on
21:19:49 🛰 planner client: provider_row=8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek stream_idle=30s
         stream_total=1200s (AI_PLAN_TOTAL_DEADLINE_SECS) http_ceiling=600s (non-stream paths only)
         retries=2 backoff=2s cap=65536
21:19:49 🗓 preflight: … (class 36, unchanged — 36 survives the swap)
```
Post-boot health: `✅ Trader auto-started successfully`, NinjaTrader close-sync + position-reconcile up,
executor `ai_call … ok=true` at 21:19:53, `positions snapshot count=0` at 21:20:01, **zero** error-level
lines since boot.

**Proof status (A20).** The boot is proven; the *behavioural* proof is not. It is the next max-reasoning
planner read that runs past 600 s and completes (impossible on the old binary), or an `ai_call …
class=total_deadline` line at 1200 s. Neither has occurred yet — ASIA sits on a `no_trade` plan, so the
next read is a level wake or the 01:30 CT LONDON scheduled read. **Owed evidence:** quote that line when
it lands. Until then class 37 is SHIPPED-UNPROVEN (R6: EVENT-WAIT).

**Rollback (still valid, exact):**
```
cd ~/vl && mv vl-bin vl-bin.bad.e42a0b43 && cp vl-bin.prev.boot vl-bin \
  && printf '17efeea9fc5909473a40e60418428b521a2f1574' > deploy/RELEASE \
  && kill -9 $(systemctl show -p MainPID --value vl) && git checkout -- deploy/RELEASE web/src/guide/types.ts
# value-only soft rollback (keeps the new class= telemetry):
echo 'AI_PLAN_TOTAL_DEADLINE_SECS=600' >> ~/vl/.env && kill -9 $(systemctl show -p MainPID --value vl)
```
`vl-bin.prev.boot` = the 17efeea9 binary (class 36), also kept as `vl-bin.old.17efeea9`.

**What the owner will still see wrong (A15), post-cutover:**
- A slow max read may now run up to 20 min per attempt (60 min for a 3-attempt read) before the plan
  lands. That is the deliberate trade: fewer fail-closed sessions, later plans on the slow tail.
- Deadline/transport failures are still fed to attempt 2 as a "validator reason" and stored in
  `planner_rejected_prompts` — unchanged by this wave (prompt paths were out of scope). Owner ruling open.
- The guide drift banner should now be CLEAR (`GUIDE_BUILT_REV` = running rev = `e42a0b43`); the FE is
  served by the vite dev server from `~/vl/web/src`, which the merge updated, so no `dist` rebuild was
  needed.
- `~/vl` holds 30+ historical `vl-bin.old.*`/`.prev.*` binaries (~2 GB); unrelated to this wave, worth
  a prune.

## Appendix A — every planner attempt in the window (144 rows)

Generated from the journal by `scratch/parse_attempts2.py` (joins the `🧠 planner call` line to the preceding `ai_call`, `📊`, `🧩`/`📝` lines and the following `📐` line; the backward scan stops at the previous planner-call line). `stream` S = SSE path (post 08-31 09:39), N = non-stream. `attempt` `1?` = attempt 1 (no `🧩` line exists for attempt 1). `est_ptok` = our T2 estimate; `prov_ptok`/`ctok` = provider usage (0 on a kill — no usage frame arrives). `rchars` = reasoning chars received. `src` = `timeout_source`. Outcomes are the next `📐` line; `accepted?` = no reject line followed (the plan was written).

| t | pid | reason | stream | attempt | mode | est_ptok | prov_ptok | ctok | rchars | ttfb | dur_s | ok | retries | finish | src | err | outcome |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 08-30 17:08:56 | 482741 | max | N | 1? | full | - | - | - | - | - | 457.5 | true | - | stop | - | - | accepted? |
| 08-30 18:35:34 | 746955 | max | N | 1? | full | - | - | - | - | - | 523.4 | true | - | stop | - | - | accepted? |
| 08-30 18:45:26 | 746955 | max | N | 1? | full | - | - | - | - | - | 580.0 | true | - | stop | - | - | planner attempt 1/3 parse/schema rejected: scenario[0].confirm.rule "1x5m_close" — sweep_l |
| 08-30 18:51:30 | 746955 | max | N | 1? | full | - | - | - | - | - | 364.2 | true | - | stop | - | - | planner attempt 2/3 rejected: S1 breakdown_continue: a close came back across 29347.75 — t |
| 08-30 18:58:57 | 746955 | max | N | 1? | full | - | - | - | - | - | 447.8 | true | - | stop | - | - | planner attempt 3/3 rejected: S3 breakdown_continue: a close came back across 29347.75 — t |
| 08-30 19:12:42 | 746955 | fast→low | N | 1? | full | - | - | - | - | - | 296.3 | true | - | stop | - | - | planner attempt 1/3 rejected: S1 breakdown_continue: a close came back across 29292.25 — t |
| 08-30 19:15:17 | 746955 | fast→low | N | 1? | full | - | - | - | - | - | 155.1 | true | - | stop | - | - | planner attempt 2/3 rejected: S1 breakdown_continue: measured displacement 0.00 pts < BD_M |
| 08-30 19:18:45 | 746955 | fast→low | N | 1? | full | - | - | - | - | - | 208.6 | true | - | stop | - | - | planner attempt 3/3 parse/schema rejected: flip{price 29251.28} does not match any number  |
| 08-30 19:47:36 | 746955 | max | N | 1? | full | - | - | - | - | - | 455.3 | true | - | stop | - | - | planner attempt 1/3 rejected: S1 breakdown_continue: a close came back across 29381.79 — t |
| 08-30 19:55:45 | 746955 | max | N | 1? | full | - | - | - | - | - | 489.1 | true | - | stop | - | - | planner attempt 2/3 rejected: S4 breakdown_continue: a close came back across 29367.75 — t |
| 08-30 20:02:35 | 746955 | max | N | 1? | full | - | - | - | - | - | 410.6 | true | - | stop | - | - | planner attempt 3/3 rejected: S1 breakdown_continue: a close came back across 29381.79 — t |
| 08-30 20:12:52 | 746955 | fast→low | N | 1? | full | - | - | - | - | - | 171.4 | true | - | stop | - | - | planner attempt 1/3 rejected: S1 breakdown_continue: a close came back across 29303.75 — t |
| 08-30 20:15:36 | 746955 | fast→low | N | 1? | full | - | - | - | - | - | 164.4 | true | - | stop | - | - | planner attempt 2/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 08-30 20:18:28 | 746955 | fast→low | N | 1? | full | - | - | - | - | - | 171.8 | true | - | stop | - | - | planner attempt 3/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 08-30 20:42:52 | 746955 | fast→low | N | 1? | full | - | - | - | - | - | 171.6 | true | - | stop | - | - | planner attempt 1/3 rejected: S1 breakdown_continue: a close came back across 29313.25 — t |
| 08-30 20:46:23 | 746955 | fast→low | N | 1? | full | - | - | - | - | - | 210.8 | true | - | stop | - | - | planner attempt 2/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 08-30 20:49:50 | 746955 | fast→low | N | 1? | full | - | - | - | - | - | 207.1 | true | - | stop | - | - | planner attempt 3/3 rejected: S2 breakdown_continue: a close came back across 29300.50 — t |
| 08-30 21:20:01 | 746955 | max | N | 1? | full | - | - | - | - | - | 600.0 | false | - | n/a | client | failed to read response: context deadline exceeded (Client.Timeout or  | planner attempt 1/3 failed: failed to read response: context deadline exceeded (Client.Tim |
| 08-30 21:29:12 | 746955 | max | N | 1? | full | - | - | - | - | - | 552.0 | true | - | stop | - | - | planner attempt 2/3 parse/schema rejected: arm legs on reject — arm_legs_sweep_reclaim_onl |
| 08-30 21:38:12 | 746955 | max | N | 1? | full | - | - | - | - | - | 539.5 | true | - | stop | - | - | planner attempt 3/3 rejected: S2 breakdown_continue: a close came back across 29301.75 — t |
| 08-30 21:43:14 | 746955 | fast→low | N | 1? | full | - | - | - | - | - | 193.5 | true | - | stop | - | - | planner attempt 1/3 rejected: S1 breakdown_continue: the tape shows NO confirming close be |
| 08-30 21:46:12 | 746955 | fast→low | N | 1? | full | - | - | - | - | - | 177.7 | true | - | stop | - | - | planner attempt 2/3 rejected: S1 breakdown_continue: measured displacement 0.00 pts < BD_M |
| 08-30 21:51:00 | 746955 | fast→low | N | 1? | full | - | - | - | - | - | 288.1 | true | - | stop | - | - | planner attempt 3/3 rejected: S1 breakdown_continue: the tape shows NO confirming close be |
| 08-30 22:19:48 | 746955 | max | N | 1? | full | - | - | - | - | - | 587.1 | true | - | stop | - | - | planner attempt 1/3 rejected: price 29361.00 is BELOW PDL 29437.00 (gap-down) — the plan M |
| 08-30 22:28:37 | 746955 | max | N | 1? | full | - | - | - | - | - | 529.4 | true | - | stop | - | - | planner attempt 2/3 parse/schema rejected: scenario[1].confirm2.rule "touch" not allowed f |
| 08-30 22:41:13 | 848146 | max | N | 1? | full | - | - | - | - | - | 396.5 | true | - | stop | - | - | planner attempt 1/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 08-30 22:50:56 | 848146 | max | N | 1? | full | - | - | - | - | - | 583.0 | true | - | stop | - | - | planner attempt 2/3 rejected: S1 breakdown_continue: a close came back across 29333.25 — t |
| 08-30 22:57:42 | 848146 | max | N | 1? | full | - | - | - | - | - | 406.2 | true | - | stop | - | - | planner attempt 3/3 rejected: S1 breakdown_continue: a close came back across 29333.25 — t |
| 08-30 23:12:16 | 848146 | max | N | 1? | full | - | - | - | - | - | 454.6 | true | - | stop | - | - | planner attempt 1/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 08-30 23:20:11 | 848146 | max | N | 1? | full | - | - | - | - | - | 475.4 | true | - | stop | - | - | planner attempt 2/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 08-30 23:30:11 | 848146 | max | N | 1? | full | - | - | - | - | - | 600.0 | false | - | n/a | client | failed to read response: context deadline exceeded (Client.Timeout or  | planner attempt 3/3 failed: failed to read response: context deadline exceeded (Client.Tim |
| 08-31 00:48:42 | 932579 | max | N | 1? | full | - | - | - | - | - | 448.0 | true | - | stop | - | - | planner attempt 1/3 rejected: only 3 levels below price 29430.25 but the machine table off |
| 08-31 00:58:42 | 932579 | max | N | 1? | full | - | - | - | - | - | 600.0 | false | - | n/a | client | failed to read response: context deadline exceeded (Client.Timeout or  | planner attempt 2/3 failed: failed to read response: context deadline exceeded (Client.Tim |
| 08-31 01:06:50 | 932579 | max | N | 1? | full | - | - | - | - | - | 488.5 | true | - | stop | - | - | planner attempt 3/3 rejected: only 3 levels below price 29430.25 but the machine table off |
| 08-31 06:35:18 | 932579 | max | N | 1? | full | - | - | - | - | - | 382.9 | true | - | stop | - | - | planner attempt 1/3 parse/schema rejected: arm on S1 needs EXACTLY 2 legs (split contract) |
| 08-31 06:43:28 | 932579 | max | N | 1? | full | - | - | - | - | - | 490.0 | true | - | stop | - | - | planner attempt 2/3 parse/schema rejected: arm on S1 needs EXACTLY 2 legs (split contract) |
| 08-31 06:50:50 | 932579 | max | N | 1? | full | - | - | - | - | - | 442.3 | true | - | stop | - | - | planner attempt 3/3 rejected: S3 breakdown_continue: a close came back across 29437.00 — t |
| 08-31 07:27:26 | 1077758 | max | N | 1? | full | - | - | - | - | - | 550.3 | true | - | stop | - | - | planner attempt 1/3 parse/schema rejected: scenario[1].confirm2.rule "1m_mss" not allowed  |
| 08-31 07:32:46 | 1077758 | max | N | 1? | full | - | - | - | - | - | 319.9 | true | - | stop | - | - | planner attempt 2/3 rejected: S1 breakdown_continue: a close came back across 29437.00 — t |
| 08-31 07:40:35 | 1077758 | max | N | 1? | full | - | - | - | - | - | 469.5 | true | - | stop | - | - | accepted? |
| 08-31 07:47:30 | 1077758 | fast→low | N | 1? | full | - | - | - | - | - | 300.8 | true | - | stop | - | - | planner attempt 1/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 08-31 07:50:36 | 1077758 | fast→low | N | 1? | full | - | - | - | - | - | 185.8 | true | - | stop | - | - | planner attempt 2/3 parse/schema rejected: scenario[0].confirm2.rule "touch" not allowed f |
| 08-31 07:52:53 | 1077758 | fast→low | N | 1? | full | - | - | - | - | - | 136.6 | true | - | stop | - | - | planner attempt 3/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 08-31 08:21:51 | 1123319 | max | N | 1? | full | - | - | - | - | - | 427.3 | true | - | stop | - | - | planner attempt 1/3 rejected: S1 breakdown_continue: a close came back across 29399.57 — t |
| 08-31 08:27:12 | 1123319 | max | N | 1? | full | - | - | - | - | - | 321.2 | true | - | stop | - | - | planner attempt 2/3 parse/schema rejected: arm on S2 needs EXACTLY 2 legs (split contract) |
| 08-31 08:33:54 | 1123319 | max | N | 1? | full | - | - | - | - | - | 401.5 | true | - | stop | - | - | planner attempt 3/3 parse/schema rejected: arm on S2 split requires confirm=touch at the s |
| 08-31 08:34:13 | 1123319 | max | N | 1? | full | - | - | - | - | - | 502.0 | true | - | stop | - | - | planner attempt 1/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 08-31 08:40:22 | 1123319 | max | N | 1? | full | - | - | - | - | - | 368.7 | true | - | stop | - | - | planner attempt 2/3 rejected: S1 breakdown_continue: a close came back across 29437.00 — t |
| 08-31 08:47:19 | 1123319 | max | N | 1? | full | - | - | - | - | - | 417.0 | true | - | stop | - | - | planner attempt 3/3 parse/schema rejected: arm legs on reject — arm_legs_sweep_reclaim_onl |
| 08-31 09:22:11 | 1123319 | max | N | 1? | full | - | - | - | - | - | 600.0 | false | - | n/a | client | failed to read response: context deadline exceeded (Client.Timeout or  | planner attempt 1/3 failed: failed to read response: context deadline exceeded (Client.Tim |
| 08-31 09:27:02 | 1123319 | max | N | 1? | full | - | - | - | - | - | 290.7 | true | - | stop | - | - | planner attempt 2/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 08-31 09:33:18 | 1123319 | max | N | 1? | full | - | - | - | - | - | 375.9 | true | - | stop | - | - | planner attempt 3/3 parse/schema rejected: scenario[0].confirm2.rule "1m_mss" not allowed  |
| 08-31 09:48:58 | 1224017 | max | S | 1? | full | 6263 | 9406 | 30421 | 102847 | 667 | 499.6 | true | 1 | stop | - | - | planner attempt 1/3 rejected: S1 breakdown_continue: a close came back across 29351.47 — t |
| 08-31 09:52:26 | 1224017 | max | S | 2 | repair | 1109 | 1556 | 13635 | 47093 | 418 | 208.1 | true | 1 | stop | - | - | accepted? |
| 08-31 10:02:49 | 1224017 | max | S | 1? | full | 6376 | 9597 | 34938 | 116495 | 538 | 551.6 | true | 1 | stop | - | - | planner attempt 1/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 08-31 10:05:41 | 1224017 | max | S | 2 | repair | 1340 | 1864 | 11698 | 40230 | 519 | 171.3 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: arm legs on reject — arm_legs_sweep_reclaim_onl |
| 08-31 10:14:13 | 1224017 | max | S | 3 | reauthor+block | 6439 | 9657 | 31732 | 110096 | 647 | 512.0 | true | 1 | stop | - | - | planner attempt 3/3 rejected: gap-down at 29401.00 (< PDL 29437.00): the short scenario's  |
| 08-31 10:35:38 | 1224017 | max | S | 1? | full | 6354 | 0 | 0 | 126768 | 512 | 600.0 | false | 1 | n/a | client | stream interrupted: context deadline exceeded (Client.Timeout or conte | planner attempt 1/3 failed: stream interrupted: context deadline exceeded (Client.Timeout  |
| 08-31 10:51:29 | 1252813 | max | S | 2 | reauthor+block | 6411 | 0 | 0 | 140177 | 562 | 600.0 | false | 1 | n/a | client | stream interrupted: context deadline exceeded (Client.Timeout or conte | planner attempt 1/3 failed: stream interrupted: context deadline exceeded (Client.Timeout  |
| 08-31 10:57:35 | 1252813 | max | S | 2 | reauthor+block | 6432 | 9638 | 26169 | 84424 | 485 | 366.6 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 08-31 10:58:55 | 1252813 | max | S | 3 | repair | 1290 | 1841 | 6556 | 18825 | 524 | 79.7 | true | 1 | stop | - | - | planner attempt 3/3 rejected: S1 breakdown_continue: a close came back across 29356.69 — t |
| 08-31 11:20:40 | 1252813 | max | S | 1? | full | 6379 | 9602 | 36004 | 113699 | 497 | 551.4 | true | 1 | stop | - | - | planner attempt 1/3 parse/schema rejected: arm on S2 needs EXACTLY 2 legs (split contract) |
| 08-31 11:24:11 | 1252813 | max | S | 2 | repair | 1359 | 1958 | 13245 | 43396 | 512 | 210.7 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: arm on S2 leg 2 must chain (wait_confirm true)  |
| 08-31 11:31:48 | 1252813 | max | S | 3 | reauthor+block | 6426 | 9651 | 29619 | 98385 | 513 | 457.5 | true | 1 | stop | - | - | planner attempt 3/3 rejected: S1 breakdown_continue: a close came back across 29357.14 — t |
| 08-31 11:53:29 | 1252813 | max | S | 1? | full | 6361 | 0 | 0 | 134792 | 499 | 600.0 | false | 1 | n/a | client | stream interrupted: context deadline exceeded (Client.Timeout or conte | planner attempt 1/3 failed: stream interrupted: context deadline exceeded (Client.Timeout  |
| 08-31 11:58:55 | 1252813 | max | S | 2 | reauthor+block | 6417 | 9641 | 21597 | 67421 | 495 | 325.9 | true | 1 | stop | - | - | accepted? |
| 08-31 12:19:42 | 1252813 | max | S | 1? | full | 6336 | 9552 | 25435 | 78074 | 463 | 373.1 | true | 1 | stop | - | - | planner attempt 1/3 parse/schema rejected: arm on S3 needs EXACTLY 2 legs (split contract) |
| 08-31 12:21:45 | 1252813 | max | S | 2 | repair | 1208 | 1802 | 9501 | 29178 | 396 | 123.0 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: arm on S3 leg 2 must chain (wait_confirm true)  |
| 08-31 12:30:09 | 1252813 | max | S | 3 | reauthor+block | 6383 | 9601 | 35739 | 119166 | 668 | 504.4 | true | 1 | stop | - | - | accepted? |
| 08-31 12:53:49 | 1252813 | max | S | 1? | full | 6341 | 9568 | 31484 | 101488 | 530 | 500.2 | true | 1 | stop | - | - | accepted? |
| 08-31 13:49:13 | 1252813 | fast→low | S | 1? | full | 6399 | 9537 | 8287 | 23874 | 477 | 104.6 | true | 1 | stop | - | - | planner attempt 1/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 08-31 13:49:56 | 1252813 | fast→low | S | 2 | repair | 994 | 1362 | 3781 | 10854 | 375 | 43.2 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: arm on S2 needs EXACTLY 2 legs (split contract) |
| 08-31 13:51:32 | 1252813 | fast→low | S | 3 | reauthor+block | 6443 | 9585 | 7837 | 22130 | 632 | 96.1 | true | 1 | stop | - | - | planner attempt 3/3 parse/schema rejected: arm legs on reject — arm_legs_sweep_reclaim_onl |
| 08-31 14:27:28 | 1252813 | max | S | 1? | full | 6336 | 0 | 0 | 133667 | 493 | 600.0 | false | 1 | n/a | client | stream interrupted: context deadline exceeded (Client.Timeout or conte | planner attempt 1/3 failed: stream interrupted: context deadline exceeded (Client.Timeout  |
| 08-31 14:32:18 | 1252813 | max | S | 2 | reauthor+block | 6393 | 9602 | 19372 | 61713 | 499 | 290.1 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: arm legs on reject — arm_legs_sweep_reclaim_onl |
| 08-31 14:34:06 | 1252813 | max | S | 3 | repair | 1164 | 1674 | 7572 | 24674 | 355 | 108.3 | true | 1 | stop | - | - | accepted? |
| 08-31 17:10:03 | 1391022 | max | S | 1? | full | 6240 | 9424 | 35527 | 117063 | 2218 | 599.5 | true | 1 | stop | - | - | planner attempt 1/3 rejected: S3 breakdown_continue: a close came back across 29517.00 — t |
| 08-31 17:15:17 | 1391022 | max | S | 2 | repair | 1350 | 2001 | 18168 | 64152 | 402 | 313.2 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: scenario[2].condition "reject_retest" invalid |
| 08-31 17:18:32 | 1391022 | max | S | 3 | reauthor+block | 6281 | 9468 | 13238 | 38933 | 506 | 195.2 | true | 1 | stop | - | - | planner attempt 3/3 parse/schema rejected: arm legs on reject — arm_legs_sweep_reclaim_onl |
| 08-31 17:27:07 | 1391022 | max | S | 1? | full | 6237 | 9402 | 23992 | 77827 | 500 | 374.0 | true | 1 | stop | - | - | planner attempt 1/3 rejected: S3 breakdown_continue: a close came back across 29497.75 — t |
| 08-31 17:31:12 | 1391022 | max | S | 2 | repair | 1073 | 1580 | 16310 | 57401 | 366 | 245.3 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: scenario[2].condition "reject_retest" invalid |
| 09-01 00:51:52 | 1625428 | max | S | 1? | full | 6263 | 9426 | 28527 | 91193 | 710 | 431.8 | true | 1 | stop | - | - | planner attempt 1/3 rejected: S3 breakdown_continue: a close came back across 29502.25 — t |
| 09-01 00:55:17 | 1625428 | max | S | 2 | repair | 1251 | 1758 | 13379 | 48183 | 416 | 205.8 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: scenario[2].confirm.rule "1x5m_close" — fade_re |
| 09-01 01:01:09 | 1625428 | max | S | 3 | reauthor+block | 6390 | 9551 | 23874 | 75415 | 513 | 351.4 | true | 1 | stop | - | - | planner attempt 3/3 parse/schema rejected: scenario[4].confirm2.rule "touch" not allowed f |
| 09-01 01:40:27 | 1625428 | max | S | 1? | full | 6366 | 9516 | 32279 | 104471 | 540 | 537.3 | true | 1 | stop | - | - | planner attempt 1/3 parse/schema rejected: scenario[3].confirm2.rule "touch" not allowed f |
| 09-01 01:43:02 | 1625428 | max | S | 2 | repair | 1272 | 1865 | 10717 | 33517 | 389 | 154.8 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: scenario[3].confirm2.rule "2x5m" invalid (touch |
| 09-01 01:49:31 | 1625428 | max | S | 3 | reauthor+block | 6463 | 9533 | 11541 | 33972 | 534 | 389.4 | true | 2 | stop | - | - | planner attempt 3/3 rejected: S3 breakdown_continue: a close came back across 29502.25 — t |
| 09-01 02:47:32 | 1625428 | fast→low | S | 1? | full | 6559 | 9723 | 8159 | 23760 | 501 | 123.2 | true | 1 | stop | - | - | planner attempt 1/3 rejected: S2 breakdown_continue: a close came back across 29447.50 — t |
| 09-01 02:48:27 | 1625428 | fast→low | S | 2 | repair | 1024 | 1389 | 4081 | 11750 | 397 | 54.2 | true | 1 | stop | - | - | accepted? |
| 09-01 03:21:16 | 1625428 | fast→low | S | 1? | full | 6564 | 9753 | 17214 | 56699 | 498 | 227.4 | true | 1 | stop | - | - | planner attempt 1/3 rejected: S1 breakdown_continue: a close came back across 29354.00 — t |
| 09-01 03:22:33 | 1625428 | fast→low | S | 2 | repair | 948 | 1309 | 6862 | 21351 | 383 | 77.0 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: scenario[0].confirm.rule "1x5m_close" — fade_re |
| 09-01 03:24:27 | 1625428 | fast→low | S | 3 | reauthor+block | 6691 | 9878 | 9145 | 28327 | 560 | 113.5 | true | 1 | stop | - | - | planner attempt 3/3 rejected: gap-down at 29345.75 (< PDL 29354.00): the short scenario's  |
| 09-01 03:50:12 | 1625428 | fast→low | S | 1? | full | 6529 | 9707 | 11477 | 35860 | 541 | 162.8 | true | 1 | stop | - | - | planner attempt 1/3 rejected: S1 breakdown_continue: measured displacement 0.00 pts < BD_M |
| 09-01 03:51:16 | 1625428 | fast→low | S | 2 | repair | 872 | 1211 | 4860 | 16376 | 557 | 64.5 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: scenario[0].confirm.rule "1x5m_close" — fade_re |
| 09-01 03:52:53 | 1625428 | fast→low | S | 3 | reauthor+block | 6656 | 9832 | 7499 | 22540 | 531 | 96.7 | true | 1 | stop | - | - | planner attempt 3/3 rejected: S2 breakdown_continue: measured displacement 0.00 pts < BD_M |
| 09-01 04:23:14 | 1625428 | fast→low | S | 1? | full | 6565 | 9734 | 19378 | 59711 | 530 | 225.1 | true | 1 | stop | - | - | planner attempt 1/3 parse/schema rejected: arm on S2 needs EXACTLY 2 legs (split contract) |
| 09-01 04:24:17 | 1625428 | fast→low | S | 2 | repair | 1160 | 1569 | 5115 | 13372 | 400 | 62.9 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: plan JSON unmarshal: json: cannot unmarshal num |
| 09-01 04:26:01 | 1625428 | fast→low | S | 3 | reauthor+block | 6671 | 9839 | 8442 | 26486 | 550 | 104.4 | true | 1 | stop | - | - | planner attempt 3/3 rejected: S1 breakdown_continue: a close came back across 29201.50 — t |
| 09-01 04:52:43 | 1625428 | fast→low | S | 1? | full | 6575 | 9753 | 10471 | 32661 | 640 | 162.6 | true | 1 | stop | - | - | accepted? |
| 09-01 05:28:23 | 1625428 | max | S | 1? | full | 6534 | 9822 | 33388 | 112536 | 509 | 502.3 | true | 1 | stop | - | - | planner attempt 1/3 parse/schema rejected: arm enabled on non-armable condition "reclaim"  |
| 09-01 05:30:00 | 1625428 | max | S | 2 | repair | 1081 | 1583 | 7349 | 24333 | 391 | 97.2 | true | 1 | stop | - | - | planner attempt 2/3 rejected: S1 breakdown_continue: measured displacement 0.00 pts < BD_M |
| 09-01 05:34:30 | 1625428 | max | S | 3 | repair | 1091 | 1605 | 17560 | 62118 | 566 | 270.0 | true | 1 | stop | - | - | accepted? |
| 09-01 06:00:01 | 1625428 | max | S | 1? | full | 6531 | 0 | 0 | 134322 | 578 | 600.0 | false | 1 | n/a | client | stream interrupted: context deadline exceeded (Client.Timeout or conte | planner attempt 1/3 failed: stream interrupted: context deadline exceeded (Client.Timeout  |
| 09-01 06:08:55 | 1625428 | max | S | 2 | reauthor+block | 6632 | 9889 | 32966 | 111562 | 775 | 534.1 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: arm legs on reject — arm_legs_sweep_reclaim_onl |
| 09-01 06:09:33 | 1625428 | max | S | 3 | repair | 1298 | 1761 | 3203 | 7092 | 401 | 38.5 | true | 1 | stop | - | - | planner attempt 3/3 parse/schema rejected: scenario[1].confirm2.rule "1m_mss" not allowed  |
| 09-01 06:29:41 | 1625428 | max | S | 1? | full | 6532 | 9828 | 31642 | 107061 | 543 | 580.6 | true | 1 | stop | - | - | planner attempt 1/3 parse/schema rejected: scenario[1].confirm2.rule "1m_mss" not allowed  |
| 09-01 06:33:02 | 1625428 | max | S | 2 | repair | 1186 | 1738 | 11611 | 37551 | 396 | 201.1 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: scenario[1].confirm2.rule "displacement" invali |
| 09-01 06:40:56 | 1625428 | max | S | 3 | reauthor+block | 6631 | 9935 | 25340 | 80841 | 539 | 473.8 | true | 1 | stop | - | - | planner attempt 3/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 09-01 06:59:59 | 1625428 | max | S | 1? | full | 6527 | 9818 | 32947 | 106267 | 506 | 598.8 | true | 1 | stop | - | - | planner attempt 1/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 09-01 07:01:15 | 1625428 | max | S | 2 | repair | 1342 | 1932 | 4563 | 12420 | 403 | 75.9 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: arm legs on reject — arm_legs_sweep_reclaim_onl |
| 09-01 07:07:32 | 1625428 | max | S | 3 | reauthor+block | 6634 | 9918 | 23769 | 80546 | 537 | 376.8 | true | 1 | stop | - | - | accepted? |
| 09-01 07:26:41 | 1625428 | fast→low | S | 1? | full | 6558 | 9729 | 15827 | 50434 | 502 | 400.4 | true | 1 | stop | - | - | planner attempt 1/3 rejected: S2 breakdown_continue: measured displacement 0.00 pts < BD_M |
| 09-01 07:27:12 | 1625428 | fast→low | S | 2 | repair | 998 | 1365 | 3056 | 7774 | 351 | 30.8 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: scenario[1].confirm.rule "1x5m_close" — fade_re |
| 09-01 07:28:49 | 1625428 | fast→low | S | 3 | reauthor+block | 6685 | 9854 | 8714 | 26630 | 694 | 97.6 | true | 1 | stop | - | - | planner attempt 3/3 rejected: S2 breakdown_continue: measured displacement 0.00 pts < BD_M |
| 09-01 07:53:47 | 1625428 | fast→low | S | 1? | full | 6555 | 9726 | 10529 | 32413 | 512 | 226.5 | true | 1 | stop | - | - | planner attempt 1/3 rejected: S1 breakdown_continue: measured displacement 0.00 pts < BD_M |
| 09-01 07:56:33 | 1625428 | fast→low | S | 2 | repair | 983 | 1334 | 7696 | 24805 | 495 | 165.8 | true | 1 | stop | - | - | accepted? |
| 09-01 08:06:46 | 1625428 | max | S | 1? | full | 6337 | 9517 | 23759 | 75946 | 522 | 405.8 | true | 1 | stop | - | - | accepted? |
| 09-01 08:21:12 | 1625428 | max | S | 1? | full | 6530 | 9822 | 41743 | 135262 | 665 | 484.4 | true | 1 | stop | - | - | planner attempt 1/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 09-01 08:23:19 | 1625428 | max | S | 2 | repair | 1553 | 2232 | 11182 | 34905 | 527 | 127.2 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: arm on S2 needs EXACTLY 2 legs (split contract) |
| 09-01 08:26:49 | 1625428 | max | S | 3 | reauthor+block | 6619 | 9910 | 17843 | 57213 | 640 | 209.9 | true | 1 | stop | - | - | planner attempt 3/3 parse/schema rejected: arm on S2 leg 2 must chain (wait_confirm true)  |
| 09-01 08:49:32 | 1625428 | max | S | 1? | full | 6463 | 9731 | 29825 | 98741 | 491 | 384.7 | true | 1 | stop | - | - | planner attempt 1/3 rejected: S1 breakdown_continue: a close came back across 29062.75 — t |
| 09-01 08:53:16 | 1625428 | max | S | 2 | repair | 1303 | 1870 | 17481 | 60045 | 362 | 223.7 | true | 1 | stop | - | - | accepted? |
| 09-01 09:19:53 | 1625428 | fast→low | S | 1? | full | 6522 | 9714 | 12069 | 38307 | 508 | 165.8 | true | 1 | stop | - | - | planner attempt 1/3 rejected: S2 breakdown_continue: a close came back across 29085.00 — t |
| 09-01 09:20:44 | 1625428 | fast→low | S | 2 | repair | 951 | 1269 | 4332 | 12711 | 372 | 50.3 | true | 1 | stop | - | - | accepted? |
| 09-01 10:21:57 | 1625428 | max | S | 1? | full | 6464 | 9732 | 28857 | 95402 | 489 | 409.2 | true | 1 | stop | - | - | planner attempt 1/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 09-01 10:22:16 | 1625428 | max | S | 2 | repair | 1183 | 1717 | 1963 | 2626 | 364 | 19.5 | true | 1 | stop | - | - | planner attempt 2/3 rejected: S1 breakdown_continue: a close came back across 29170.00 — t |
| 09-01 10:24:39 | 1625428 | max | S | 3 | repair | 1139 | 1646 | 10249 | 35638 | 500 | 143.3 | true | 1 | stop | - | - | accepted? |
| 09-01 10:48:38 | 1625428 | fast→low | S | 1? | full | 6519 | 9715 | 16868 | 53018 | 503 | 210.4 | true | 1 | stop | - | - | planner attempt 1/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 09-01 10:49:05 | 1625428 | fast→low | S | 2 | repair | 1101 | 1494 | 2612 | 5568 | 373 | 27.1 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: scenario[0].confirm2.rule "1m_mss" not allowed  |
| 09-01 10:50:49 | 1625428 | fast→low | S | 3 | reauthor+block | 6641 | 9844 | 8595 | 28203 | 663 | 104.4 | true | 1 | stop | - | - | planner attempt 3/3 rejected: S1 breakdown_continue: a close came back across 29281.75 — t |
| 09-01 11:25:30 | 1625428 | max | S | 1? | full | 6469 | 9742 | 27247 | 88787 | 487 | 383.5 | true | 1 | stop | - | - | planner attempt 1/3 parse/schema rejected: scenario[2].confirm2.rule "1m_mss" not allowed  |
| 09-01 11:27:28 | 1625428 | max | S | 2 | repair | 1080 | 1605 | 8976 | 29336 | 487 | 117.5 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: scenario[2].confirm2.rule "displacement" invali |
| 09-01 11:32:19 | 1625428 | max | S | 3 | reauthor+block | 6568 | 9849 | 21972 | 69545 | 782 | 291.0 | true | 1 | stop | - | - | planner attempt 3/3 parse/schema rejected: arm on S1 needs EXACTLY 2 legs (split contract) |
| 09-01 11:58:32 | 1625428 | max | S | 1? | full | 6451 | 9702 | 33700 | 110679 | 498 | 444.8 | true | 1 | stop | - | - | planner attempt 1/3 parse/schema rejected: arm legs on breakdown_continue — arm_legs_sweep |
| 09-01 11:59:57 | 1625428 | max | S | 2 | repair | 1174 | 1726 | 6565 | 20526 | 368 | 85.2 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: arm on S2 needs EXACTLY 2 legs (split contract) |
| 09-01 12:06:32 | 1625428 | max | S | 3 | reauthor+block | 6540 | 9790 | 28098 | 95393 | 423 | 394.9 | true | 1 | stop | - | - | planner attempt 3/3 parse/schema rejected: scenario[0].confirm2.rule "1m_mss" not allowed  |
| 09-01 12:33:07 | 1625428 | max | S | 1? | full | 6442 | 0 | 0 | 73196 | 474 | 600.0 | false | 1 | n/a | client | stream interrupted: context deadline exceeded (Client.Timeout or conte | planner attempt 1/3 failed: stream interrupted: context deadline exceeded (Client.Timeout  |
| 09-01 12:43:07 | 1625428 | max | S | 2 | reauthor+block | 6544 | 0 | 0 | 71414 | 500 | 600.0 | false | 1 | n/a | client | stream interrupted: context deadline exceeded (Client.Timeout or conte | planner attempt 2/3 failed: stream interrupted: context deadline exceeded (Client.Timeout  |
| 09-01 12:52:48 | 1625428 | max | S | 3 | reauthor+block | 6544 | 9771 | 28408 | 92053 | 506 | 581.1 | true | 1 | stop | - | - | planner attempt 3/3 rejected: S2 breakdown_continue: a close came back across 29125.00 — t |
| 09-01 12:59:12 | 1625428 | fast→low | S | 1? | full | 6495 | 9654 | 15085 | 48546 | 498 | 245.3 | true | 1 | stop | - | - | planner attempt 1/3 rejected: S1 breakdown_continue: a close came back across 29100.50 — t |
| 09-01 13:00:02 | 1625428 | fast→low | S | 2 | repair | 1016 | 1409 | 3456 | 9525 | 548 | 50.1 | true | 1 | stop | - | - | accepted? |
| 09-01 17:10:09 | 1625428 | max | S | 1? | full | 6343 | 9494 | 25619 | 84446 | 655 | 543.6 | true | 1 | stop | - | - | planner attempt 1/3 rejected: S1 breakdown_continue: a close came back across 29130.50 — t |
| 09-01 17:15:51 | 1625428 | max | S | 2 | repair | 1120 | 1620 | 17869 | 64729 | 368 | 342.4 | true | 1 | stop | - | - | planner attempt 2/3 parse/schema rejected: scenario[0].confirm.rule "1x5m_close" — fade_re |
| 09-01 17:23:14 | 1625428 | max | S | 3 | reauthor+block | 6470 | 9619 | 21623 | 73714 | 533 | 442.7 | true | 1 | stop | - | - | planner attempt 3/3 rejected: S2 breakdown_continue: a close came back across 29130.50 — t |

---

## LIVE EVIDENCE — first sightings of the class-37 telemetry (2026-09-01 23:47-23:53 CT) [A]

On the class-38 binary (`c0580011`, which carries class 37), one ASIA level-wake read hit three
provider-side mid-body EOFs; every failure line carried a class:

```
23:47:13 ai_call … duration_ms=250094 ok=false retries=1 ttfb_ms=510 reasoning_chars=54986 timeout_source=transport
         deadline_s=600 class=transport http_status=200 request_id="" err="stream interrupted: unexpected EOF"
23:47:13 ⏳ Waiting 2s before retry...
23:47:15 ⚠️  AI API stream failed, retrying (2/2)...
23:47:15 📡 Request URL (stream idle=30s total=1200s) …                       ← identical request re-sent
23:47:33 ai_call … duration_ms=18167 ok=false retries=2 ttfb_ms=807 reasoning_chars=3073 … class=transport http_status=200 request_id=""
23:47:33 🛰 planner call FAILED class=transport provider_row=8ef641a7-…_deepseek model=deepseek-v4-pro http_status=200
         request_id="" elapsed=270.3s idle=30s total=1200s — still failed after 2 retries: stream interrupted: unexpected EOF
23:52:41 ai_call … duration_ms=307957 ok=false retries=1 ttfb_ms=685 reasoning_chars=69503 … class=transport http_status=200 request_id=""
23:52:43 ⚠️  AI API stream failed, retrying (2/2)... → landed 23:55:06 (attempt wall 453.7 s)
```
Rate: 4 stream transport cuts in 81 stream calls on 2026-09-01 (4.9 per 100; 0 of 31 on 08-31; the SSE
path did not exist before 08-31 09:39). `http_status=200` on every cut — the provider accepted each
request and closed the body mid-stream. `request_id=""` — DeepSeek sent none of X-Request-Id /
Request-Id / X-Amzn-Requestid / Cf-Ray. The negotiated protocol is **HTTP/1.1** (SafeHTTPClient sets
a custom DialContext without ForceAttemptHTTP2, which disables Go's automatic h2; probed 2026-09-02
00:0x CT with a bare key-less GET: SafeHTTPClient → HTTP/1.1, stock transport → HTTP/2.0). DeepSeek's
status page showed no incident for the window.

**Class 37's own proof (a read past 600 s under the 1200 s ceiling) is still outstanding:** longest
successful read since its boot is 543.6 s; the 1200 s ceiling has not yet been needed.
