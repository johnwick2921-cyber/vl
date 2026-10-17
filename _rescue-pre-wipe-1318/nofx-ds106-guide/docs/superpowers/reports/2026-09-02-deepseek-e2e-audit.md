names rewritten to vl on 2026-09-30 (VL rename)
# END-TO-END AUDIT OF THE DEEPSEEK CALL SYSTEM — ROOT CAUSE (2026-09-02)

**READ-ONLY.** No code, config, knob, key or DB write; no restart, cancel, reset or cutover; no lock taken; **no live provider call made** (A3 — the running system's own calls are the only evidence). Work on `docs/deepseek-e2e-audit-0902` in worktree `~/vl-dsaudit`; `~/vl` untouched.

Evidence class on every line: **[RUNTIME]** journal/log/live state · **[DB]** query + result · **[CODE]** file:line · **[CONFIG]** resolved value · **[NET]** socket observation · **[DOC]** provider documentation. A [CODE]-only claim about live behavior is UNVERIFIED until a [RUNTIME]/[NET] line confirms it.

---

## 0. PREMISE CORRECTION AND LIVE STATE

**The dispatch says "Live rev 8a756bba (class 33)". It is not.** [RUNTIME] at audit start 2026-09-02 07:35 CT:

| item | value |
|---|---|
| PID | 2461883, started `Wed Sep 2 07:32:15 2026` |
| running binary | `vcs.revision=0d093c3b3a11fb6ea6cb19454ffa59a9f7bd9f8b`, `vcs.time=2026-09-02T12:22:31Z`, `vcs.modified=false` |
| `deploy/RELEASE` | `0d093c3b…` (equal) |
| boot line | `🔐 BOOT INTEGRITY OK — rev 0d093c3b3a11 · built 2026-09-02T12:22:31Z · expected 0d093c3b3a11 · goldens PASS` |
| dev tip | `7e7556b9` |
| main tree | porcelain clean; **no `~/vl-main.lock` present** (A2 satisfied without taking one) |

`8a756bba` was the **06:57:49** boot (class 33); it was superseded 35 minutes later by the root-fix cutover. **Four boots today** [RUNTIME]: 00:11:47 `23f56f49` (pnl-truth) · 06:27:45 `d5a6e138` (class-41 transport wave) · 06:57:49 `8a756bba` (class 33) · 07:32:15 `0d093c3b` (root-fix). Everything below distinguishes pre- and post-**06:27:45**, the class-41 policy boundary.

Resolved AI configuration in force [RUNTIME boot 07:32:15]:
```
🧠 AI params in force: model=deepseek-v4-pro client_max_tokens=32768 planner_max_tokens=65536
   temperature=0.50 top_p=omitted timeout=600s (HTTP ceiling; non-stream paths)
   planner_stream_idle=30s planner_stream_total=1200s retries=2 backoff=2s
🛰 planner client: provider_row=8ef641a7-…_deepseek stream_idle=30s stream_total=1200s
   (AI_PLAN_TOTAL_DEADLINE_SECS) http_ceiling=600s (non-stream paths only) retries=2 backoff=2s cap=65536
🔁 planner stream policy (class 41): stream_tries=3 (AI_PLAN_STREAM_TRIES, counts CALLS;
   AI_MAX_RETRIES=calls non-stream only) backoff=2s→15s→45s (AI_PLAN_STREAM_BACKOFF)
   watchdog_log=on (⏱ line on fire, per SSE line) keepalive=30s (dialer)
   serialize_executor=off resend_identical=on (transport/deadline → same prompt, no reject block)
```

---

## 1. THE HEADLINE — WHAT ACTUALLY FAILS

**The call system is not primarily failing at the transport. It is failing at the validator, and that is what costs sessions.**

Census of every planner attempt that did not yield a usable plan, 2026-08-26 → 2026-09-02 07:40 CT. Source: the `📐 planner attempt N/3 (rejected|parse/schema rejected|failed)` lines in `data/vl_2026-08-2[6-9].log`, `vl_2026-08-3*.log`, `vl_2026-09-0*.log` (the file logs are authoritative — the journal only reaches back to 08-27 13:37). **n = 194 failed attempts** [RUNTIME].

| family | n | share |
|---|---|---|
| `breakdown_continue: a close came back across X — the breakdown is void` | 37 | 19.1% |
| `arm_legs_sweep_reclaim_only` (split legs on a non-`sweep_reclaim` condition) | 28 | 14.4% |
| `flip.rule "2x5m_close" invalid (2x5m\|15m_close\|5m_close)` | 18 | 9.3% |
| `confirm/confirm2.rule not allowed for <condition>` | 13 | 6.7% |
| `arm on S<n> needs EXACTLY 2 legs (split contract), got 1` | 11 | 5.7% |
| `no JSON object found in planner output` | 7 | 3.6% |
| `fade_requires_touch` | 6 | 3.1% |
| `leg 2 must chain (wait_confirm true)` | 4 | 2.1% |
| **the 51 remaining, individually classified** — `breakdown_continue` displacement below `BD_MIN_DISP_ATR` (8), `fvg: no fresh 3-candle gap` / stale gap (7), `only 3 levels on a side, ≥4 required` (10), gap-day trigger-side rules (4), `reject_retest` invalid (2), `death.rule invalid` (2), `arm enabled on non-armable condition` (2), `confirm2.rule "displacement"/"2x5m" invalid` (5), others (11) | 51 | 26.3% |
| **MODEL-OUTPUT SUBTOTAL** | **175** | **90.2%** |
| `503 Server Overloaded` | 3 | 1.5% |
| transport / deadline / EOF / reset / empty-response | 16 | 8.2% |
| **PROVIDER + TRANSPORT SUBTOTAL** | **19** | **9.8%** |

Every one of the 51 "remaining" was inspected individually and is **also** a model-output reject — there is no unclassified residue.

**And the outcome that matters:** **17** `🚨 PLANNER FAIL-CLOSED` events in the window (17 distinct timestamps: 08-26 ×1, 08-27 ×3, 08-31 ×6, 09-01 ×6, 09-02 ×1). Grouping their quoted reasons — **17 caused by model output, 0 caused by transport, a 503, or a deadline**; `grep -icE "503|overload|transport|deadline|EOF|reset|timeout"` over all 17 → **0** [RUNTIME]. The 13 most recent, verbatim:

| when (CT) | session | fail-close reason (verbatim, truncated) |
|---|---|---|
| 08-31 01:06:50 | 2026-08-30 ASIA | `only 3 levels below price 29430.25 but the machine table offered 35 — the plan must carry ≥4 on EACH side` |
| 08-31 06:50:50 | LONDON | `S3 breakdown_continue: a close came back across 29437.00 — the breakdown is void; author a reject/retest play instead` |
| 08-31 08:33:54 | LONDON | `arm on S2 split requires confirm=touch at the sweep ref (leg 1 rests AT the level)` |
| 08-31 08:47:19 | NY | `arm legs on reject — arm_legs_sweep_reclaim_only` |
| 08-31 09:33:18 | NY | `scenario[0].confirm2.rule "1m_mss" not allowed for breakdown_continue` |
| 08-31 17:18:32 | ASIA | `arm legs on reject — arm_legs_sweep_reclaim_only` |
| 09-01 01:01:09 | 2026-08-31 ASIA | `scenario[4].confirm2.rule "touch" not allowed for breakdown_continue` |
| 09-01 01:49:31 | LONDON | `S3 breakdown_continue: a close came back across 29502.25 — the breakdown is void` |
| 09-01 17:23:14 | ASIA | `S2 breakdown_continue: a close came back across 29130.50 — the breakdown is void` |
| 09-01 18:00:41 | ASIA | `arm legs on breakdown_continue — arm_legs_sweep_reclaim_only` |
| 09-01 21:14:52 | ASIA | `S4 breakdown_continue: a close came back across 29047.75 — the breakdown is void` |
| 09-01 21:53:46 | ASIA | `arm on S1 leg 2 must chain (wait_confirm true) on its confirm rule` |
| 09-02 01:37:44 | LONDON | `S2 breakdown_continue: a close came back across 29021.25 — the breakdown is void` |

(The four earliest — 08-26 ×1, 08-27 ×3 — are omitted from the table for length; all four are likewise model-output rejects. An independent census by a second agent over the same window arrived at the same 17 and the same zero.)

**Session cost.** 2026-09-01 ASIA fail-closed **four times** (17:23, 18:00, 21:14, 21:53) before an owner reset at 23:12:44 rescued it; 2026-09-02 LONDON fail-closed at 01:37:44 and needed an owner reset at 07:15:26 [DB `plans`]. Both rescues were manual.

---

## 2. G2 — THE 09-02 01:0x 503 BURST, AND WHY IT DID **NOT** CAUSE LONDON'S FAIL-CLOSE

The dispatch asks for the burst "in full and its relation to LONDON's fail-close". They are **unrelated**, and the evidence is unambiguous.

**The burst** [RUNTIME, `data/vl_2026-09-02.log`]. In the window 00:55–01:40 CT there were **22 `class=http_status http_status=503`** failures plus one `class=client_timeout` and one `class=other`. The 503s land on an **ASIA level-event wake read**, not on LONDON:

```
09-02 01:15:47 🗓️ level wake seated OB(bull)·1h invalidated: close 29049.00 below 29085.00 (noise 4.30)
               on ASIA 2026-09-01 — waking the planner (W6, 5th wake-up).
09-02 01:15:47 🧠 planner mode: fast-market (drift 50.2 pts = 2.8×ATR5m) — reasoning downgraded to fast→low (F3)
09-02 01:15:50 📐 planner attempt 1/3 failed: still failed after 2 retries: API error (status 503):
               {"error":{"message":"Server Overloaded","type":"service_unavailable_error",…}}
09-02 01:15:50 🧩 planner attempt 2/3 reauthor+block: prompt ~6864 tokens (full-author ~6744 tokens)
09-02 01:15:53 📐 planner attempt 2/3 failed: still failed after 2 retries: API error (status 503): …
09-02 01:15:53 🧩 planner attempt 3/3 reauthor+block: prompt ~6864 tokens (full-author ~6744 tokens)
09-02 01:15:57 📐 planner attempt 3/3 failed: still failed after 2 retries: API error (status 503): …
09-02 01:15:57 🗓️ wake re-read failed for 2026-09-01 ASIA (benign — active plan kept): … (status 503) …
```

**The whole three-attempt planner budget was consumed in 7 seconds** (01:15:50 → 01:15:57) — 3 attempts × 3 calls = **9 provider calls in 7 seconds**, every one a 503, with only the pre-class-41 2-second client backoff between them. Because this was a **wake** read (`failClosed=false`) the outcome was benign and the active plan was kept. **This is a near-miss, not a non-event: the identical burst on a scheduled read would have fail-closed the session in 7 seconds.** See §J1-2.

**LONDON's fail-close was a different read entirely**, 22 minutes later, and every one of its three attempts was a *validator* reject [RUNTIME]:

```
09-02 01:32:54 🧠 planner call (reasoning=fast→low …) completed in 173.2s
09-02 01:32:54 📐 planner attempt 1/3 rejected: S1 breakdown_continue: a close came back across 29021.25 — the breakdown is void …
09-02 01:32:54 🧩 planner attempt 2/3 repair: prompt ~922 tokens (full-author ~6555 tokens)
09-02 01:35:04 🧠 planner call … completed in 130.4s
09-02 01:35:04 📐 planner attempt 2/3 parse/schema rejected: scenario[0].confirm.rule "1x5m_close" — fade_requires_touch …
09-02 01:35:04 🧩 planner attempt 3/3 reauthor+block: prompt ~6682 tokens (full-author ~6555 tokens)
09-02 01:37:44 🧠 planner call … completed in 160.3s
09-02 01:37:44 📐 planner attempt 3/3 rejected: S2 breakdown_continue: a close came back across 29021.25 — the breakdown is void …
09-02 01:37:44 🚨 PLANNER FAIL-CLOSED 2026-09-02 LONDON: S2 breakdown_continue … — writing a NO-TRADE plan
09-02 01:37:44 🗓️ PLAN written 2026-09-02 LONDON v1 (model deepseek-v4-pro, lifecycle no_trade, prompt ae539d43b6ce…)
```

All three LONDON calls **succeeded at the transport** (173.2 s, 130.4 s, 160.3 s of streamed reasoning, no 503, no cut). **Verdict: the 503 burst cost nothing; the model's inability to satisfy the `breakdown_continue` rule cost the session.**

Two further live defects visible in the same window [RUNTIME]:
- `09-02 00:59:47 ai_call model=deepseek-v4-pro duration_ms=600000 finish_reason=n/a ok=false retries=1 ttfb_ms=0 reasoning_chars=0 timeout_source=client deadline_s=600 class=client_timeout http_status=200 err="failed to read response: context deadline exceeded …"` — a **600-second executor hang**, immediately followed by `⏱ cycle overran the scan interval (10m0.047s > 2m0s) — next tick delayed, in-flight work never cancelled; intervening ticks skipped`. Ten minutes of executor blindness from one call.
- `09-02 01:03:52 … duration_ms=244270 … timeout_source=transport … class=other http_status=200 err="fail to parse AI server response: API returned empty response"` — **HTTP 200 with an empty body after 244 seconds**, labelled `class=other` and `timeout_source=transport`, which is the mislabelling described in §E4.

---

## 3. G4 — PROVIDER STATUS FOR THE WINDOW

[DOC] `https://status.deepseek.com/` fetched during this audit: **no incidents, no degraded-performance events and no maintenance windows are reported between 2026-08-26 and 2026-09-02.** Current status "All systems are operating as expected"; API uptime for Jun–Sep 2026 shown as 99.81%–100%; no incident mentions the chat-completions API, streaming, or capacity. **So the 503 bursts we observed are below the provider's own incident threshold** — they are ordinary load-shedding, not an outage, and no fallback should be justified on the basis of a published incident.

---

## 4. THE DOMINANT ROOT CAUSE — A STANDING PROMPT INSTRUCTION THAT FIGHTS A TAPE-AWARE VALIDATOR

`breakdown_continue` / `breakup_continue` appear in **82 of the 194 failed attempts (42.3%)** and in **8 of the 13 fail-closes** [RUNTIME]. Of those 82, **38 are the same rule**: `a close came back across <level> — the breakdown is void`. This is not a transport problem and not a model-quality problem. It is a **contradiction between two parts of our own system**, and the mechanism is fully traceable:

**1. The prompt issues a standing, unconditional MUST** [CODE] `kernel/planner_prompt.go:589`:
> `"If price sits BELOW PDL you MUST write a continuation short; ABOVE PDH, a continuation long. "`

**2. "Continuation" is bound to the two waterfall conditions** [CODE] `kernel/planner_prompt.go:623`:
> `"WATERFALL PLAY (F1): author breakdown_continue|breakup_continue when the tape shows one-sided delivery, a >1.2×ATR gap-and-go, or a waterfall after a failed rally — the momentum-follow class."`

**3. The prompt NEVER tells the model that a given breakdown level is already void.** [CODE] A grep of `kernel/planner_prompt.go` for `void|reclaimed|came back across` returns only `:592`, an unrelated flip/death rule. The facts snapshot carries levels, ATR, regime — but not "the tape has already closed back across 29021.25, so a continuation there is illegal".

**4. The validator computes exactly that fact at WRITE time and rejects on it** [CODE] `kernel/breakdown_continue.go:254`:
> `return fmt.Errorf("%s %s: a close came back across %.2f — the breakdown is void; %s", s.ID, s.Condition, bd.Level, BreakdownReclaimedHint)`

**5. The correction is outgunned by the instruction.** A full re-author sends the **entire standing prompt plus a reject tail** [RUNTIME]: `🧩 planner attempt 3/3 reauthor+block: prompt ~6682 tokens (full-author ~6555 tokens)` — the reject block is **~92–127 tokens against a ~6,341–6,691-token prompt, i.e. ~1.5%**, and the standing MUST at `:589` is still inside it. The model is told "you MUST write a continuation short" in the body and "do not write the continuation you just wrote" in a footnote.

**6. The live regression, in one session** [RUNTIME] 2026-09-02 LONDON:
```
01:32:54  attempt 1/3 rejected: S1 breakdown_continue: a close came back across 29021.25 — the breakdown is void
01:32:54  attempt 2/3 repair: prompt ~922 tokens          → model switches to a reject fade
01:35:04  attempt 2/3 parse/schema rejected: scenario[0].confirm.rule "1x5m_close" — fade_requires_touch
01:35:04  attempt 3/3 reauthor+block: prompt ~6682 tokens (full-author ~6555)
01:37:44  attempt 3/3 rejected: S2 breakdown_continue: a close came back across 29021.25 — the breakdown is void
01:37:44  🚨 PLANNER FAIL-CLOSED … LONDON
```
Attempt 2 **obeyed** the hint and moved to a `reject` fade — and was then killed by a *different* rule (`fade_requires_touch`, the reject fade must confirm on touch, not on a close). Attempt 3, a full re-author, **regressed to `breakdown_continue` at the very same level 29021.25**. Three calls, 463.9 s of successful streaming, session lost.

**Is this a hard deadlock?** No — and the report will not overstate it (A12). The gap-day validator only requires *a short in any condition* [CODE] `kernel/plan_doc.go:841-842` calling `hasDirection(d.Scenarios, "short")` (`:877-884`), so a plain `reject` short satisfies it. **The deadlock is in the wording, not the code**: the error message says `the plan MUST include a continuation/breakdown short scenario` and the prompt says `you MUST write a continuation short`, both of which name the one condition family the tape may have already invalidated. This is the class-34 disease (a hint that names a target the validator then punishes), one layer up: **the instruction, not the hint, is now the thing that cannot be obeyed.**

**Owner ruling candidates (no change made — A1):**
- Make `:589` tape-aware, or soften it to "a short-direction scenario" and let the model choose the condition; and align the `plan_doc.go:842` message with what the code actually requires.
- Feed the validator's own knowledge forward: put "breakdown at X is VOID (close came back at HH:MM)" into the facts block so the model never authors it.
- Put the reject block at the **top** of the re-author prompt, or drop the conflicting standing line from re-author prompts specifically (lost-in-the-middle: a 1.5% tail after 6.5k tokens is the weakest position in the context).

---

## 5. THE TRANSPORT FAILURES THAT ARE REAL — WHAT THEY COST, AND WHAT THEY DO NOT

Transport failures are only 9.8% of failed attempts and have caused **zero** fail-closes, but they are not free. Three distinct mechanisms, all [RUNTIME] over the 7-day window:

**5.1 — The 600-second ceiling, hit 16 times.** Every occurrence:
`08-26 20:40:02 · 08-27 22:19:20 · 08-28 12:41:05 · 08-28 12:51:06 · 08-30 21:20:01 · 08-30 23:30:11 · 08-31 00:58:42 · 08-31 09:22:11 · 08-31 10:35:38 · 08-31 10:51:29 · 08-31 11:53:29 · 08-31 14:27:28 · 09-01 06:00:01 · 09-01 12:33:07 · 09-01 12:43:07 · 09-02 00:59:47` (CT).
These split into **two completely different failures wearing the same label**:

- **Streamed-and-starved (n=7 with counters):** e.g. `08-31 10:51:29 … duration_ms=600001 … ttfb_ms=562 reasoning_chars=140177 timeout_source=client deadline_s=600`. First byte in 562 ms, then **140,177 characters of reasoning received**, and the call was killed at the ceiling with nothing usable. Also 126,768 (10:35:38) · 134,792 (11:53:29) · 133,667 (14:27:28) · 134,322 (09-01 06:00:01) · 73,196 (09-01 12:33:07) · 71,414 (09-01 12:43:07). This is the class-37 disease and is what the planner's 1200 s split was built for — the boot line now reads `stream_total=1200s (class 37: planner ceiling split from the HTTP ceiling)`.
- **Never-started (the newest, 09-02 00:59:47):** `duration_ms=600000 … retries=1 ttfb_ms=0 reasoning_chars=0 timeout_source=client deadline_s=600 class=client_timeout http_status=200 err="failed to read response: context deadline exceeded"`. **Zero bytes in ten minutes.** DeepSeek documents that it closes a connection if inference has not started within ~10 minutes; our non-stream HTTP ceiling is 600 s = the same ten minutes. We do not distinguish "queued behind the provider's backlog and never started" from "the network died" — both land as `timeout_source=client`, and both cost the caller a full ten minutes.

**5.2 — The executor loses its cadence, 96 times.** `⏱ cycle overran the scan interval (…) — next tick delayed, in-flight work never cancelled; intervening ticks skipped` fired **96 times** in the window; the worst was `09-02 00:59:47 (10m0.047s > 2m0s)` — a single hung call cost **ten minutes of executor blindness** on a 2-minute cadence, with the in-flight work explicitly never cancelled. Others in the last two days: 2m41s · 4m54s · 2m19s · 3m0s · 2m44s · 3m30s · 3m9s. The executor is not being killed by the provider; it is being *held* by it.

**5.3 — The 503 burst is one hour of the whole week.** All **35** call-level 503s in seven days fall in a single hour, `09-02 01:xx CT` (= 15:xx Beijing) [RUNTIME]. There is no chronic 503 condition, and **0 fail-closes cite a 503**. But the burst exposed a real hazard: **3 planner attempts × 3 calls = 9 provider calls in 7 seconds** (§2), which on a *scheduled* read would have destroyed the session in the time it takes to read this sentence. That it landed on a wake read was luck, not design.

**The mislabelling that hides all of this** [RUNTIME]: `timeout_source=transport` appears on `09-02 01:03:52 … class=other http_status=200 err="fail to parse AI server response: API returned empty response"` — an HTTP 200 with an empty body after 244 s, which is not a transport event at all. §E4 enumerates every site with this defect; the census in §1 was built by reading durations and error text rather than trusting the label.

---

## 6. SECTION C — REQUEST CONSTRUCTION

### C1 Prompt assembly

Five calling paths build prompts [CODE]: planner **full-author** (`kernel.BuildPlannerPrompt`, `kernel/planner_prompt.go:319`, invoked `trader/auto_trader_planner.go:902`) · planner **repair** (`kernel/planner_repair.go:12`, invoked `:1436`) · planner **re-author+block** (`userPrompt = prompt + rejectBlock`, `:1438`, block from `plannerRejectBlock` `:1242`) · planner **resend-identical** (class-41 M0, `:1425-1430`) · **executor** (`kernel/engine_prompt_futures.go:24` + `kernel/engine_analysis.go:526`, called at `:676` via `CallWithMessages`, **non-stream**) · **weekly** (`kernel/weekly_prompt.go:276`, non-stream) · **Ask-Planner** (`api/handler_plan.go:1398/:2150`, non-stream). All planner variants share one 173-char system prompt (`trader/auto_trader_planner.go:24`).

**The class-38 contract text** [DOC-internal `docs/superpowers/reports/2026-09-01-class38-contract-mismatch.md`, merged `c0580011`, cut over 22:22:58 CT 09-01] found **17 condition-keyed validator restrictions**, 7 of them enforced-but-unstated and one stated in the wrong spelling (`2x5m` where the enum is `2x5m_close`). It is live [RUNTIME 07:32:15]: `📜 prompt/validator contract: 17 restrictions, all stated in prompt (class 38 guard)`. Its cost: the output contract grew 10,219 → 11,284 chars (+267 tokens, ≈+4%).

**C1.5 — the token estimator is wrong by a third, always in the same direction.** [CODE] `trader/auto_trader_planner.go:1373-1379` `estimatePromptTokens = (len(s)+3)/4` — a flat 4 chars/token. Measured against the provider's own `prompt=` count on 10 paired calls [RUNTIME]:

| path | n | min err | median err | max err | real chars/token |
|---|---|---|---|---|---|
| planner (T2 estimate vs `prompt=`) | 10 | −25.10% | **−32.02%** | −32.76% | 2.69–3.00 |
| executor (exact DB char counts vs `prompt=`) | 10 | −41.66% | **−41.70%** | −42.16% | 2.31–2.33 |

Zero over-estimates in 20 pairs. The executor prompt is denser (numeric OHLCV tables) so a single divisor cannot serve both. **Consequence, stated honestly:** no cap, budget or gate reads this number today [CODE, exhaustive grep — its three call sites are all log statements], so the error costs nothing now; it only makes three log lines wrong. The moment anyone sizes a context budget or a cost estimate on it they will be low by a third to two-fifths.

### C2 Request body — what we send, verbatim

Planner stream [CODE] `mcp/client.go:976-1085` + `applyDeepSeekThinkingDefaults` `:1087-1099`:
```json
{"model":"deepseek-v4-pro",
 "messages":[{"role":"system","content":"…173 chars…"},{"role":"user","content":"…~26.7 KB…"}],
 "temperature":0.5, "max_tokens":65536, "stream":true,
 "thinking":{"type":"enabled"}, "reasoning_effort":"max"}
```
Executor non-stream [CODE] `mcp/client.go:482-530` — **a second, independent builder**:
```json
{"model":"deepseek-v4-pro",
 "messages":[{"role":"system","content":"…~11.0 KB…"},{"role":"user","content":"…~16.2 KB…"}],
 "temperature":0.5, "max_tokens":32768,
 "thinking":{"type":"enabled"}, "reasoning_effort":"low"}
```
(`stream` is absent entirely on this path; its `messages` are `[]map[string]string` and therefore **structurally cannot** carry `reasoning_content` or `tool_calls`.)

Omitted by zero-value/`omitempty`: `top_p` (boot says `top_p=omitted`), penalties, `stop`, `tools`, `response_format`, `logprobs`, `n`, `seed`, and **`stream_options` — the key exists nowhere in the codebase** [CODE, exhaustive grep for `stream_options|include_usage` → zero hits].

### C3 Headers — exactly two, and they are the documented two

`Content-Type: application/json` [CODE] `mcp/client.go:642` and `Authorization: Bearer <redacted>` [CODE] `:478-480` → `mcp/provider/deepseek.go:66-68`. Both paths share one builder; **there is no separate header path for SSE**. Not set: `Accept` (so **no `Accept: text/event-stream`**), `Connection`, `User-Agent` (Go emits `Go-http-client/1.1`), `Accept-Encoding` (Go auto-adds gzip). **Headers DeepSeek documents that we omit: none** — [DOC] their documented request is exactly those two headers, and streaming is selected by the body's `"stream": true`, not by `Accept`.

### C4 Provider rows [DB `ai_models`, 11 rows — key material never read or printed]

Two DeepSeek rows, **both enabled**, both with a key present, both with empty `custom_api_url`/`custom_model_name` (so both resolve to the package defaults `https://api.deepseek.com` + `deepseek-v4-pro` [CODE] `mcp/providers.go:19-20`):

| row | name | enabled | key | bound to |
|---|---|---|---|---|
| `8ef641a7-…_deepseek` | DeepSeek AI | 1 | present | **the only trader** (`hoang`), via `ai_model_id` |
| `396db319-…_deepseek_0751c0b6` | **DeepSeek 2** | **1** | present | **nothing** — no trader, and `planner_model` is empty on all 9 strategies [DB] |

The other 9 rows are disabled and keyless. **Every path shares one client object**: `resolvePlannerClient` returns `at.mcpClient` verbatim when the planner binding is empty [CODE] `trader/auto_trader_planner.go:64-105`; [RUNTIME] `🧠 planner model: empty binding → using primary, pinned "deepseek-v4-pro"` and boot `🛰 planner client: provider_row=8ef641a7-…_deepseek`.

**DeepSeek 2 is enabled, keyed, and unreachable.** The only selector that could pick it (`store.PickProviderModel` → `betterProviderModel` `store/ai_model.go:227-252`) fires **only when no row's id matches the trader's `ai_model_id`** — which never happens today. **There is no runtime failover anywhere** [CODE, exhaustive]: nothing re-resolves the client after a failure; a 401/402/503 on the bound row never reaches for the second. The tiebreak, were it ever to run, separates the two rows by **120 microseconds** of `updated_at`.

## 7. SECTION G1 — DOCUMENTED PROVIDER BEHAVIOR vs OURS

| # | documented behavior | our handling | verdict |
|---|---|---|---|
| a | Under load the server holds the stream with `: keep-alive` **comment lines** | `onLine()` fires on **every** scanned line — blanks and `:` comments included — **before** the `data: ` filter, and pushes to `resetCh` [CODE] `mcp/client.go:1411-1424`, watchdog `:1207-1240`. **Keep-alives DO reset our idle watchdog.** | **COMPLY** |
| a3 | Rate limiting is by **concurrency** (cap 500 for this model), not RPM; overflow → 429 | We run ≤2 concurrent calls; 429 and all 5xx are treated as provider-side (`IsProviderFailure`, resend identical) [CODE] `:263-280` | COMPLY |
| **b** | **"If the request has not started inference after 10 minutes, the server will close the connection."** | Planner: idle 30 s + total **1200 s** → survives a queue (keep-alives reset the timer), dies at 1200 s or on the server's close → `class=transport` → class-41 resend-identical. **Executor: `http.Client.Timeout = 600 s` — exactly the documented 10 minutes.** | **COMPLY (planner) / GAP (executor)** — zero margin, and our timer and theirs fire at the same instant, so the two causes are indistinguishable in our logs. This is precisely the `09-02 00:59:47` zero-byte 600 s hang in §5.1. |
| c2 | `finish_reason: "length"` | Non-stream: counter + `🚨` WARN [CODE] `:591-597`. **Stream path stores the reason and does nothing else** [CODE] `:1263-1276` | **PARTIAL GAP** — `truncated-responses=N` on the boot line counts **executor truncations only**; a truncated *plan* would never appear in it (latent, n=0) |
| c3 | `finish_reason: "content_filter"` | **Not handled on either path**; partial content is parsed as if complete | **GAP** (latent, n=0 of 3,557) |
| c4 | `finish_reason: "insufficient_system_resource"` | **Not handled, and not in `retryableErrors`** — an interrupted response is fed to the plan parser as a normal answer, most likely failing schema validation and burning an attempt on a *provider outage* dressed as a validator reason | **GAP** (latent, n=0) — the same shape class-41 fixed for HTTP status |
| d | `reasoning_content` must **not** be echoed back without `tools` (ignored); **must** be echoed with `tools` (else HTTP 400) | Planner/repair never echo it (reasoning is accumulated separately, only `ReasoningChars` kept) [CODE] `:1404-1487`; executor structurally cannot; the agent path (the only one sending `tools`) **does** echo | **COMPLY on all four paths** — though two code comments assert the opposite of the docs (§Contradictions) |
| e | Max output for `deepseek-v4-pro` = **384K**; context 1M | 65,536 (planner) / 32,768 (executor) | **COMPLY** — 65536 is legal, 17% of the ceiling |
| f2 | Thinking mode **does not support** `temperature`/`top_p`/penalties | We send `temperature: 0.5` on **every** call | **GAP (benign on the wire, misleading in the ledger)** — the provider ignores it, but the boot line advertises `temperature=0.50` as an AI param "in force". It is not in force. |
| g1 | HTTP **500** = retry after a brief pause | **`500` is missing from `retryableErrors`** [CODE] `mcp/client.go:34-52` (429/502/503/520/524 are present). The planner survives via `IsProviderFailure` (`st >= 500`), but **the executor's `CallWithMessages` loop would give up on a 500** | **PARTIAL GAP** |
| g2 | 401/402/422 = do not retry | Not retried | COMPLY |

---

## 8. SECTION B — THE CALL GRAPH: EVERY PATH THAT REACHES DEEPSEEK

Enumerated from [CODE] `grep -rn "CallWithMessages\|CallWithRequest\|CallWithRequestStream\|chat/completions" --include=*.go . | grep -v _test` — 60 hits across `kernel/ trader/ api/ agent/ telegram/ mcp/ cmd/`.

| # | path | trigger | entry file:line | stream? | policy |
|---|---|---|---|---|---|
| B1 | **Session planner read** (all 6 trigger classes) | scheduled · level_event · structure_mss · death_replan · owner_reread · owner_reset | `trader/auto_trader_planner.go:981` | **SSE** | **class 41** |
| B1f | planner non-stream fallback | same | `:984` | no | **unreachable — see A11** |
| B2 | planner retry (repair / reauthor+block / resend-identical) | attempt ≥2 | `:1418-1460` | SSE | class 41 |
| B3 | ROOT-FIX shadow A/B | after a plan write, if enabled | `trader/rootfix_shadow_ab.go:206` | SSE | class 41 — **cannot fire today** |
| B4 | **weekly read** | Sunday / boot-backfill | `trader/auto_trader_weekly.go:172` | no | 600 s ceiling |
| B5 | **executor decision loop** (highest volume, ~2 min) | every cycle | `kernel/engine_analysis.go:676` | no | 600 s ceiling |
| B6 | **watch/observer cycle** | in-position, `position_mode=watch` | `kernel/engine_prompt_observer.go:162` | no | 600 s ceiling |
| B7 | grid engine | crypto grid | `kernel/grid_engine.go:456` | no | dormant (futures) |
| B8/B9 | **Ask-Planner · Realign** | `POST /api/plan/ask` · `/plan/realign` | `api/handler_plan.go:1411` · `:2150` | no | 600 s ceiling |
| B10 | Studio test-run | `POST /api/strategies/test-run` | `api/strategy.go:833` | no | 600 s ceiling |
| B11/B12 | **Telegram agent + summariser** | any Telegram message | `telegram/agent/agent.go:224` · `telegram/session/memory.go:94` | no | 600 s ceiling |
| B13 | **AgentBeta / VLi web agent — 17 call sites** | `POST /api/agent/chat[/stream]` | `agent/central_brain.go:47,494,880`; `agent/planner_runtime.go` ×12; `agent/workflow.go` ×3; `agent/memory.go` ×2; others | no | own client, 600 s |
| B15/B16 | `cmd/planner_ab`, `cmd/decisive-test` | manual CLI | hardcode `https://api.deepseek.com/chat/completions` | no | separate binaries, not in `vl-bin` |

**All six day-plan trigger classes funnel into ONE outbound site** (`runPlannerReadWithTriggerClaimedCtx`, `:855`), which is why the class-41 policy covers the whole day-plan system at a single seam. `failClosed=true` for scheduled / death_replan / owner_reread / owner_reset; `false` for level_event and structure_mss (this is why the 01:15 503 storm was benign — §2).

**Three layered deadlines on the planner call** [CODE] `mcp/client.go:1191-1200`: idle 30 s per SSE line · total 1200 s via `context.WithTimeoutCause` · and **`cp := *hc; cp.Timeout = 0`** — the 600 s HTTP ceiling is explicitly lifted for streams, which is exactly what class 37 shipped. Worst case per read: **3 attempts × 3 client tries = 9 provider calls, ≈3 hours, with no outer wall-clock bound**; `claimPlannerRead` (`:860`) has **no expiry**, so a stuck read blocks its (trader, date, session) key indefinitely.

**Surprises in the call graph:**
- **The Telegram bot is LIVE and has no `.env` footprint.** [RUNTIME] `09-02 07:32:16 telegram/bot.go:60 Telegram bot @VLtrader_bot started`; [CONFIG] `grep -c TELEGRAM .env` → **0** — the token comes from the `telegram_configs` DB table. Any user the bot can reach drives a tool-calling loop with unbounded DeepSeek spend, invisible to the boot block.
- **The shadow A/B cannot call at all today**: `SHADOW_AB_ENABLED` is absent from `.env` **and** from `/proc/2461883/environ`, and nothing in-process can set it — a restart is required. [DB] `system_config` has no shadow rows; boot says `OFF target_n=10 done=0`.
- **Four paths never assert a reasoning mode** — weekly (`B4`), observer (`B6`), Ask-Planner and Realign (`B8/B9`) call on the **shared client** and inherit whatever the last planner (`enabled/max`) or executor (`enabled/low`) call left on it. Their cost and latency are non-deterministic.
- **A11 — the planner's non-stream fallback cannot fire.** `:984` is guarded by a `BaseClient()` type assertion that `*provider.DeepSeekClient` always satisfies. A fallback that cannot fire is not a fallback.

## 9. SECTION F — AFTER THE RESPONSE

### F1 The validator chain, in execution order [CODE] `trader/auto_trader_planner.go:1381+`

`0` provider-failure fork (`:1442`, → identical resend, no reject block, **no rejected-prompt row**) → `1` non-provider call error → `2` JSON extract/unmarshal (`kernel/plan_doc.go:423-429`) → `3` **normalizers** (`NormalizePlanDocRules`, class-39 arm-leg normalization `:1156-1190` — WARN + counter, *not* a reject) → `4` **schema gate at resolved caps** (`ParsePlanDocCapped`, maxLevels 12 / scenarioCap 5) → `5` level auto-collapse (mutates + WARN) → `6` flip-fired bias → `7` label provenance → `8` **facts + machine validator** (`ValidatePlanDocWithFactsMachine`, `kernel/plan_doc.go:799`) → `9` arm feasibility (**WARN only**) → `10` FVG re-verification → `11` **breakdown/breakup continuation** (`kernel/breakdown_continue.go:210` — §4's rule) → `12-13` flip/death sanity, prose-only death (**WARN only**) → `14` **confirm{} grace gate — POST-LOOP** → `15` grade stamping → `16` **arm gates at runtime** (`trader/armed_executor.go:1127`).

**F1 finding — stage 14 is a silent session-killer, and it is armed right now.** The confirm-grace rejection runs *after* the retry loop, sets `doc = nil`, and drops into fail-closed. **The model is never told and no attempt is spent trying to fix it.** [DB] `system_config.confirm_grace_sessions_seen = 3` and `confirmGraceSessions()` defaults to **3** (`:2481`) ⇒ **the grace window is exhausted and the gate is live.** Worse, [CODE] the counter increments on **every invocation**, while the comment at `:1648` says "the first CONFIRM_GRACE_SESSIONS (default 3) **distinct plan sessions**" — three missing-confirm writes inside one session would exhaust it (A12: comment and code disagree; both quoted).

**F1 finding — arm-gate rejects never reach the model.** Stage 16 (`ARM_MIN_RR`, `MinSLATRMult×ATR5m`, HTF veto, one-live-arm) refuses an order every cycle and logs `armRefusalClass`; none of it re-enters a prompt. The planner learns nothing from an arm it authored that can never be placed.

### F2 Retry orchestration — the decision table

Which counter a failure consumes is decided at **one line** [CODE] `:1442` via `mcp.IsProviderFailure` (`mcp/client.go:263`): `transport`, `idle_deadline`, `total_deadline`, `client_timeout`, `context` → provider-side; `http_status` → provider-side iff `>= 500 || == 429`.

| failure | in-client retry (3 tries, 2s→15s→45s)? | planner attempt consumed? | next prompt |
|---|---|---|---|
| transport (reset / EOF / stream error) | **yes** | only after 3 tries fail | **identical resend** |
| total-deadline (1200 s) · idle (30 s) · client_timeout (600 s) | **no** (but see below) | yes | **identical resend** |
| HTTP 5xx / 429 | **yes** | after tries exhausted | **identical resend** |
| HTTP 4xx (≠429) | no | yes | repair / reauthor+block |
| schema or validator reject | n/a | yes | **repair**; unparseable repair → one forced re-author |
| **confirm{} missing, grace exhausted** | n/a | **no — post-loop** | **nothing; fail-closed** |

**Is resend-identical proven live? NO — and it cannot be proven even when it fires.** [RUNTIME] since 06:27:45 CT: `grep -cE "🧩 planner attempt"` → **0**; `grep -cE "resend-identical|failed on the provider"` → **0**. Only two planner reads have run since class 41 (07:15:26, 07:21:59, both under the *previous* rev `8a756bba`), both succeeding on attempt 1 call 1, and **zero planner reads have run under the live rev `0d093c3b`**. Separately, [CODE] `shortHash(prompt)` is computed at `:1076` and stored in `plans.prompt_hash` but **never logged on the attempt lines** — so "byte-identical" will not be provable from the journal even after it fires, and provider failures deliberately write no rejected-prompt row to compare against. **The claim is uninstrumented by construction.**

The defect class-41 M0 exists to fix, captured live the day before [RUNTIME] `09-01 23:47:33`: `🛰 planner call FAILED class=transport … elapsed=270.3s … stream interrupted: unexpected EOF` → `📐 planner attempt 1/3 failed` → `🧩 planner attempt 2/3 reauthor+block` — **a pure transport EOF handed to the model as a validator reason.**

**F2 findings:** ① an off-by-one renders `attempt %d re-sends the IDENTICAL prompt` with `attempt+1`, so attempt 3 will log *"attempt 4"*, which does not exist. ② **the boot line prints a non-value**: `plannerStreamPolicyBootLine` (`:1346`) passes the literal string `"calls"` into an `AI_MAX_RETRIES=%s` verb, so the operator reads `AI_MAX_RETRIES=calls non-stream only` instead of `=2`. ③ the comment at `mcp/client.go:1321-1327` claims deadline kills are never retried in-client, but `wrapStreamDeadlineErr` appends the underlying error verbatim — a cancel surfacing as `unexpected EOF` matches the retryable token list and **would** be retried, tripling wall time [B]. ④ **A11 — the deadline machinery has never fired**: across 08-30→09-02, `class=total_deadline` → 0, `class=idle_deadline` → 0, `stream idle watchdog` → 0. `watchdog_log=on` is a claim, not evidence.

### F3 Replan budget (class 35)

Spends: `death_replan`, `owner_reread` only [CODE] `store/strategy.go:1210`. Free: scheduled reads, `level_event`, `structure_mss`, `owner_reset`, dormant/rearm markers, fail-closed markers. Cap resolved **4** for all three sessions [DB `day_plan.replan_cap`].

**Current counts — every session, today: used 0, cap 4, left 4.** [DB] `SELECT COUNT(*) FROM system_config WHERE key LIKE 'dayplan_replans_used%'` → **0**. LONDON's reset baseline is 2 (`dayplan_reset:…:2026-09-02:LONDON = 2`); NY and ASIA have no reset row (baseline 1). Today's LONDON chain is v1 fail-closed → v2 owner_reset → v3 level_event — **three versions, zero spends**, which is class 35 working as designed.

**F3 finding (A11) — the counter has never recorded a spend, ever.** [DB] `SELECT trigger_reason, COUNT(*) FROM plans WHERE trigger_reason IN ('death_replan','owner_reread')` → **0 rows all-time**. The all-time histogram is `level_event 67 · planner_fail_closed 30 · owner_reset 23 · ASIA_scheduled_read 22 · NY_scheduled_read 19 · LONDON_scheduled_read 16 · replans_exhausted 5 · structure_mss 2`. Both gates that read the budget (`deathReplanAllowed` `:735`, the owner-reread gate `auto_trader_reread.go:119`) therefore **pass unconditionally** today. Whether that is correct-and-untriggered or broken-and-silent **cannot be settled from the data**: no death re-plan has been *attempted* either, because deaths route to `dormant:flip:` / `dormant:death:` (`:298-312`), which by design skip the budget entirely.

### F4 Rejected-prompt store

Cap **200**, verified [CODE] `git show 0d093c3b:store/planner_rejected.go` `const plannerRejectedCap = 200` (raised 20→200 by owner ruling after 121 validator rejects in 72 h). **26 rows** present, newest `2026-09-02 06:37:44 UTC = 01:37:44 CT` — the LONDON fail-close.

**F4 finding — the ROOT-FIX `facts` column is in the code but NOT in the live database.** [CODE] rev `0d093c3b` declares `Facts string` and `SaveRejectedPromptWithFacts` (`:69`, caller `trader/auto_trader_planner.go:1293`), but [DB] `PRAGMA table_info(planner_rejected_prompts)` returns 9 columns with **no `facts`**. Cause: `AutoMigrate` is lazy — it runs on the first `PlannerRejected()` call (`store/store.go:398-404`, `store/planner_rejected.go:42-47`) and no reject has occurred since the 07:32:15 boot. **All 26 stored rows are facts-less**, so the offline A/B still cannot replay any fact-dependent validator (`ValidatePlanDocWithFactsMachine`, fvg, breakdown) against a single row. The capability shipped; its data is empty.

### F5 Telemetry — the last 20 planner calls

Full table in the source data; the material facts [RUNTIME]:

| field | state across 20 calls |
|---|---|
| `ttfb_ms` | 355–1,525 ms, present on all stream calls |
| `wall_ms` | 18,167–574,971 ms |
| `prompt` tokens | 9,507–9,974 full-author · 1,231–1,817 repair — **except 0 on three failed calls** |
| `completion` tokens | 8,469–34,024 — **except 0 on the same three** |
| `reasoning_chars` | 25,250–111,117 |
| `finish_reason` | `stop` ×17, **empty ×3** |
| `class=` | absent on successes by design; `transport` ×3 |
| `http_status` | `200` on 18, **including all three `class=transport` failures**; absent on the 2 oldest (binary drift) |
| **`request_id`** | **`""` on 20 of 20** |

**F5 findings:** ① **`request_id` is empty on every call in four days** — `requestIDFrom` probes `X-Request-Id`, `Request-Id`, `X-Amzn-Requestid`, `Cf-Ray` and finds none. Class 37 promised "status + provider request id ride along"; it delivers status and never an id, so **no failure in this audit can be correlated with the provider's own logs**. ② **`📊 AI call complete (stream)` is logged on FAILED calls** [CODE] `mcp/client.go:1249-1262` fires whenever `sr != nil`, *before* the error return at `:1268` — a line that says "complete" on a stream that died at 250 s with 54,986 reasoning characters discarded, carrying `prompt=0 completion=0` which are not real values. ③ **Cross-call telemetry contamination, proven** [RUNTIME]: the non-stream executor call at `09-02 07:22:05` logged `ttfb_ms=547` — a non-stream call cannot have a TTFB, and 547 is **exactly** the TTFB of the planner stream that finished at 07:21:59. The planner and executor share one `*mcp.Client` whose per-call atomics are process-global (`client.lastTTFBMs.Store(...)` at `:1249-1252` + `resolvePlannerClient` returning `at.mcpClient`). **Every field on `ai_call` is exposed to this race**, including the token counts the ROOT-FIX A/B uses as its live baseline (`RecordLiveCallMetrics`, `:1001`).

---

## 10. SECTION E — RECEIVING AND PARSING (where the silent kills live)

**Scope proof** [CODE]: `git diff --stat 0d093c3b..HEAD -- mcp/ trader/auto_trader_planner.go kernel/planner_speed.go security/url_validator.go` is **empty** — the audited source is byte-identical to the running binary. Runtime line numbers corroborate (`mcp/client.go:1279` logged at 07:44:03 is exactly where the stream-complete statement sits at HEAD).

### E1 The SSE reader

`bufio.Scanner`, **default buffer, no `Scanner.Buffer` call** [CODE] `mcp/client.go:1409`. Repo-wide `grep '.Buffer('` returns exactly one hit and it is elsewhere: `provider/databento/historical.go:66 sc.Buffer(make([]byte, 1024*1024), 8*1024*1024) // tolerate long lines`. **Somebody on this codebase already knew this failure mode and raised the limit in the Databento reader, but not in the AI reader.** So the SSE path runs at `bufio.MaxScanTokenSize` = **64 KiB per line**.

Could a delta exceed it? Not in the incremental regime we observe (deltas are ~100–400 bytes; max *aggregate* `reasoning_chars` = 140,177). But **per-line length is not instrumented anywhere** — no counter, no probe [UNVERIFIED]. If the provider or an intermediary ever emits one buffered frame, today's p50 output (23,769 tokens, per the boot line) **already exceeds** the limit [B]. **Is any observed failure consistent with it? No** — `grep "token too long"` → **n=0** across all logs.

**What makes it dangerous is the error handling, not the limit:** `ErrTooLong` → `stream interrupted: bufio.Scanner: token too long` → matches no classifier token → **`class=other`** → `IsProviderFailure` returns **false** → the planner does **not** resend; it quotes `bufio.Scanner: token too long` back to the model **as the reason its plan was rejected** [CODE] `trader/auto_trader_planner.go:1441-1460`. A stream truncated by our own buffer would look like a bad plan.

**Keep-alive handling:** `: keep-alive`, blank and comment lines correctly fail the `data: ` prefix test and are skipped [CODE] `:1423`. **But `onLine()` fires at `:1414`, BEFORE the prefix check** — so every scanned line, including keep-alives, resets the idle watchdog. See E2.

**Three defects around `[DONE]`** [CODE]: ① **`[DONE]` is never required** — a stream ending cleanly without it falls out of the loop with `scanner.Err()==nil` and returns whatever accumulated **as a success**. ② **No empty-text guard**: `:1290` guards only `sr == nil`; `sr.Text == ""` with `err == nil` returns `""` as success — while the non-stream path *does* have this guard (`API returned empty response`, `:572`). **Asymmetric.** ③ A **partial final line on a clean EOF** is handed to `json.Unmarshal`, fails, hits `:1446 continue // skip malformed chunks`, and the stream returns **success with silently-lost content** — with no malformed-chunk counter anywhere.

**The usage frame arrives unrequested.** `stream_options`/`include_usage` appear nowhere in the repo, yet **107 of 107 successful streams carried usage** and **0 of 11 failed ones did** [RUNTIME] — it is the last frame, so it never survives a cut. Useful as a failure fingerprint; also means we depend on undeclared provider behavior (§Surprises).

### E2 The idle watchdog — near-vacuous, and never fired

It measures **time since the last SSE LINE**, not the last byte and not the last delta [CODE] `mcp/client.go:1212-1240`. It kills via **`context.CancelCauseFunc`** (`cancel(ErrStreamIdleDeadline)`, `:1231`), not `body.Close()`. The resulting error is correctly labelled `class=idle_deadline` / `timeout_source=stream_idle` — the one deadline that is labelled right.

**Does it log on fire? In code yes; in production it has never fired** [RUNTIME]: `grep "watchdog FIRED"` → **0** across every log file and the journal since 08-26; `class=idle_deadline` → **0**. Since the class-41 boot it has had **16 stream requests** to fire on and fired zero times. **`watchdog_log=on` in the boot line advertises an unexercised code path as live evidence.**

**A11 — the check is close to vacuous by construction.** Because `onLine()` runs *before* the `data:` filter, a keep-alive comment resets the 30 s timer exactly as a real reasoning delta does. A stream that has **stopped producing tokens but is still heartbeating** can never trip the idle deadline; it will run to the 1200 s total instead. The watchdog detects a **dead socket**, which the transport surfaces anyway — not a **stalled generation**, which is what it was built for.

### E3 The total deadline — correct, distinguishable, and proven load-bearing

Applied as **both** a `context.WithTimeoutCause(1200s, ErrStreamTotalDeadline)` **and** a shallow client copy with `cp.Timeout = 0` [CODE] `:1192-1206` — so the 600 s HTTP ceiling is lifted for streams while **the shared client is never mutated** and the non-stream paths keep their ceiling. Error text is fully distinguishable from a transport cut.

**Proof it caused no cut since class 41** [RUNTIME]: `class=total_deadline` → **0** across all logs and the journal; a duration scan for anything near 1200 s → no hits; and **every** `ok=false` line since 06:27:45 CT is exactly **one**:
```
09-02 07:38:19 ai_call … duration_ms=27 … class=transport http_status=0
  err="failed to send request: Post \"https://api.deepseek.com/chat/completions\":
       read tcp 10.0.0.141:48638->3.173.21.63:443: read: connection reset by peer"
```
**27 milliseconds** — a reset on the *send*, i.e. a dead pooled TCP connection reused from the idle pool. It retried and won 30 s later.

**The strongest single piece of evidence in the whole audit** [RUNTIME] `09-02 07:44:03`: `duration_ms=588121 finish_reason=stop ok=true reasoning_chars=123402` — a **588.1-second** stream that completed. Under the old 600 s ceiling it had **11.9 seconds** of headroom. Class 37's split is load-bearing, not theoretical.

**A12 — two sources disagree on the historical kill count.** The code comments (`mcp/client.go:1160`, `kernel/planner_speed.go:41`) say *"11 of 80 max-reasoning planner streams at exactly 600.0s"*; a direct grep of the file logs finds **7** stream kills at 600000–600001 ms; `docs/superpowers/reports/2026-09-01-planner-api-failure.md:113` reconciles them as *"5 of the 7 stream kills"*, i.e. the 11 counts **attempts** across stream and non-stream. Both quoted; neither picked.

### E4 Error classification — the label lies, and the lie has consequences

**Two independent labels are computed from the same error and disagree by design.** `classifyAIError` (`:227-252`) produces `class=` with 8 cases. `logAICall` (`:409-424`) produces `timeout_source=` with **`source := "transport"` as the DEFAULT and only 4 overrides** — there is no `http_status` case, no `auth_config` case, no parse case. **Every remaining failure in the system is stamped `timeout_source=transport`.**

| # | site | error text | `class=` | `timeout_source=` | truth | observed |
|---|---|---|---|---|---|---|
| 1 | `:1259` stream non-200 | `API error (status 503): …` | `http_status` ✓ | **`transport`** ✗ | provider 5xx | **YES ×6** (09-02 01:15) |
| 2 | `:729` non-stream non-200 | `API returned error (status 503): …` | `http_status` ✓ | **`transport`** ✗ | provider 5xx | **YES** — 16×503, 33×402 |
| 4 | `:572` empty `choices` on HTTP **200** | `fail to parse AI server response: API returned empty response` | **`other`** | **`transport`** ✗ | provider returned nothing | **YES** — 09-02 01:03:52, **244,270 ms burned** |
| 6 | `:1482` `ErrTooLong` | `stream interrupted: bufio.Scanner: token too long` | **`other`** | **`transport`** ✗ | **our own 64 KiB buffer** | latent |
| 8 | `:1167`/`:435` no key | `AI API key not set` | `auth_config` ✓ | **`transport`** ✗ | **config error** | latent |
| 10 | genuine socket cut | `connection reset` / `unexpected EOF` | `transport` ✓ | `transport` ✓ | transport | **YES ×5** |

Of the **50** `ok=false` lines on file, the `transport` stamp is **correct on 5 and wrong on 23**. Error-text histogram [RUNTIME]: `35 failed to read response · 33 API returned error (status 402) · 16 status 503 (non-stream) · 11 stream interrupted · 6 API error (status 503) (STREAM) · 2 failed to send request · 1 fail to parse`. The wording difference `API error` vs `API returned error` is the **only** way to tell a stream 503 from a non-stream 503 in the log.

**The consequence that matters (F-6):** `IsProviderFailure` returns **false for `class=other`**, so the empty-200, the JSON parse failure and `ErrTooLong` all fall into the reject/repair branch and are **quoted back to the model as the reason its plan was rejected** — the identical disease the 01:15 503 fix was written for, **still open for every non-status failure**.

**A11 — two retry tokens are dead code.** `"stream error"` and `"INTERNAL_ERROR"` in `retryableErrors` are HTTP/2-only, and **HTTP/2 is off** (below). Zero HTTP/2 markers in any log.

### E5 / E6 Non-stream parse and truncation

`finish_reason` handling: `stop` ✓ (n=6,709) · `length` ✓ **on the non-stream path only** (n=7, all at 32,768 = the executor cap, last on 08-28 02:31:47) · **`content_filter` ✗ UNHANDLED** · **`insufficient_system_resource` ✗ UNHANDLED** — `grep -rn` for both strings returns **zero hits**; both are treated as **complete successes** with whatever partial content arrived. The second is DeepSeek's documented mid-generation capacity signal, i.e. precisely the failure a provider-overload retry should catch, and it is invisible.

**F-10 — the stream path performs NO truncation handling at all.** `TruncatedResponses.Add(1)` exists at exactly one site (`:593`, inside the non-stream parser). So the boot line `📐 planner cap: … truncation → 🚨 WARN, never silent` is **false as written for the planner** — the very reader it is printed beside — and `truncated-responses=N` in the AI-params line structurally under-reports. Headroom is currently large (max stream completion 41,743 of 65,536 = 64%), so this is latent.

**F-7 — the HTTP-200 empty/error body is never logged** [CODE] `:571-573` returns before any body logging. The 09-02 01:03:52 failure burned **244.3 seconds** and left **no evidence of what DeepSeek actually returned** — error object, empty choices, or truncated body are indistinguishable. Unattributable by construction.

**F-9 — `lastFinishReason` is never reset** (`resetCallTelemetry` `:323-333` omits it) and the stream stores it only when non-empty, so `LastFinishReason` can return a **previous call's** value — and that is the atomic the planner's truncation check reads.

### E — Surprises

- **S-1 — HTTP/2 is OFF, and nobody appears to have intended it.** `security.SafeHTTPClient` sets a custom `DialContext` for SSRF protection, which silently disables Go's HTTP/2 auto-upgrade (`ForceAttemptHTTP2` appears **nowhere** in the repo). All DeepSeek traffic is HTTP/1.1 [B + RUNTIME: zero HTTP/2 signatures, three HTTP/1.1 `unexpected EOF`s]. **This explains the `unexpected EOF` cluster class 41 was chasing** — that is the HTTP/1.1 chunked-truncation signature.
- **S-3 — the deprecated upstream website link (removed in the VL rename) key is still firing: 33 × HTTP 402** in the window, the largest failure bucket after read failures. `CLAUDE.md` records that key as dead; something still calls it.
- **S-4 — the 27 ms reset is a stale pooled connection.** The transport sets **no `IdleConnTimeout`** (zero = keep idle connections forever) and no `MaxIdleConnsPerHost` tuning [CODE] `security/url_validator.go:164-186`. The boot line's `keepalive=30s (dialer)` is the TCP keepalive on the dialer and **does not bound idle-pool residency**.
- **S-6 — stream attempt counts are not derivable from the completion lines.** 09-02 had 16 stream requests but 9 completion lines; the gap reconciles exactly to 6 stream-503s + 1 send reset. Any rate computed off `AI call complete (stream)` has a silent denominator.

---

## 11. SECTION D — TRANSPORT

### D1 The client: one constructor, and its unset fields are the story

Every DeepSeek client — executor, planner, agent, Ask-Planner — funnels through `mcp.NewClient()` → `DefaultConfig()` → **`security.SafeHTTPClient(ResolvedAITimeout())`** [CODE] `mcp/config.go:87`. `grep -rn "http.Transport{"` finds **no transport literal anywhere in `mcp/` or `provider/`**; the only one on this path is inside `SafeHTTPClient` [CODE] `security/url_validator.go:159-203`.

| field | resolved | evidence |
|---|---|---|
| `http.Client.Timeout` | **600 s** | [CONFIG] `.env AI_HTTP_TIMEOUT_SECONDS=600`; [RUNTIME] boot |
| `net.Dialer.Timeout` | **600 s — bound to the AI request ceiling** | [CODE] `:160` `Timeout: timeout` |
| `net.Dialer.KeepAlive` | 30 s (literal) | [CODE] `:161` |
| `DialContext` | non-nil — `net.LookupIP` + private-IP check **on every dial** | [CODE] `:165-186` |
| `ResponseHeaderTimeout` · `TLSHandshakeTimeout` · `MaxIdleConns` | **0 = no limit** | [CODE] zero values |
| **`IdleConnTimeout`** | **0 — Go NEVER evicts an idle pooled connection** | [CODE] zero value; Go doc "Zero means no limit" |
| `MaxIdleConnsPerHost` | 0 → effective **2** | [CODE] `DefaultMaxIdleConnsPerHost` |
| `ForceAttemptHTTP2` · `TLSClientConfig` | false · nil | [CODE] |
| **`Transport.Proxy`** | **nil — proxying DISABLED**, not `ProxyFromEnvironment` | [CODE] zero value |

**HTTP version settled two independent ways.** [CODE] Go's `Transport.protocols()` takes the branch `!ForceAttemptHTTP2 && (… || DialContext != nil || …)` → *"Be conservative and don't automatically enable http2"* → the bundled h2 transport is never configured and **ALPN offers `http/1.1` only**. [NET] corroboration: during the 07:15:49→07:21:59 planner stream that overlapped four executor calls, **two separate TCP connections** carried the two workloads — `:48474` accumulating 6.5 MB of SSE with `lastrcv` pinned at 4–44 ms, `:45368` stepping per executor call. **Under HTTP/2 both would have multiplexed onto one connection.** Max simultaneous ESTAB to the peer across the capture = **2** (818 samples at 1, 154 at 2, 0 above) — exactly `DefaultMaxIdleConnsPerHost` with HTTP/1.1 head-of-line serialization.

**The peer is `3.173.21.63:443`** — identified [RUNTIME] from the process's own error text, and per the sibling wave a **CloudFront edge**, not DeepSeek origin.

**Surprise:** `net.Dialer.Timeout` is bound to `AI_HTTP_TIMEOUT_SECONDS`, so **a TCP connect to a black-holed peer can hang 600 seconds**. There is no separate dial or TLS-handshake ceiling.

### D2 Class-41 policy: two fields are real, four are string literals

| element | enforcing code | read or literal? | [RUNTIME] proof since 06:27:45 |
|---|---|---|---|
| `stream_tries=3` | `mcp/client.go:1338-1344` | **read** (env, unset → 3) | **none — unproven.** Every stream retry ever logged reads `retrying (2/2)` (pre-class-41 fallback to `MaxRetries`); the class-41 string `(2/3)`/`(3/3)` has **never appeared** |
| `backoff=2s→15s→45s` | `:1364-1368` | **read** | **none — unproven.** The one `⏳ Waiting 2s` today is the *non-stream* loop |
| `watchdog_log=on` | `:1229-1237` | **literal** | **none.** `grep "stream idle watchdog FIRED"` across every log 08-26→09-02 → **0 hits, all time** |
| `keepalive=30s (dialer)` | `security/url_validator.go:161` | **LITERAL** — the constant `30` is passed at `auto_trader_planner.go:1353`; **nothing reads the dialer** | **PARTIAL/CONTRADICTED** — [NET] SO_KEEPALIVE is armed, but the observed probe timer peaks at **14/18/20 s**, never 30 |
| `serialize_executor=off` | **no code exists** | **LITERAL** | **proven active** — 105 of 117 streams overlapped an executor call |
| `resend_identical=on` | `auto_trader_planner.go:1418-1431` (real code) | **literal in the line** | **none — `grep "resend-identical"` → 0 hits in any log** |

**A11 — the boot line would print `keepalive=30s` even if the dialer were changed to `KeepAlive: 0`, and `serialize_executor=off` even if serialization were added.** Its own fixture (`trader/planner_client_bootline_test.go:44-51`) pins the wording by calling the function with the same hand-written literals, so **the pin cannot detect the drift it exists to prevent.**

**A12 — keepalive period disputed.** The sibling transport report states 30 s [A]; my [NET] sampling of the same socket family shows probe countdowns peaking at 20/18/14 s over 53 samples with `bytes_sent`/`bytes_received` frozen. Both agree the host default (7200 s) is overridden; they disagree on the value. Neither picked.

### D3 Connection reuse — the mechanism, proven live

**Same client, same transport, same pool, same host.** `resolvePlannerClient()` returns `at.mcpClient` whenever the planner binding is empty [CODE] `:74-79` — and [RUNTIME] it is empty on **9 of 9** planner reads today. The stream path shallow-copies only to lift `Timeout`; the comment says it: *"A shallow copy shares Transport/CheckRedirect/Jar"* [CODE] `mcp/client.go:1196-1204`. **The `*http.Transport` pointer — hence the idle pool — is identical.**

**Reuse of a long-idle connection, observed** [NET]: socket `:48474` sat idle ~106 s (`lastsnd:105820`), the planner stream went out at 07:15:49 on it, and by 07:16:10 it had `bytes_received:397203`. It survived.

**A stale pooled connection killing a call — OBSERVED end to end, not inferred** [NET]+[RUNTIME], 09-02 07:38:19:

| t | observation |
|---|---|
| 07:34:20.012 | `:48638 NEW→ESTAB` |
| 07:37:07 | last byte received on it |
| 07:38:10.022 | `HB … lastrcv:62924` — idle, still ESTAB, **still in Go's pool** |
| **07:38:19** | executor reuses it → `ai_call … duration_ms=27 … class=transport … "read tcp 10.0.0.141:48638->3.173.21.63:443: read: connection reset by peer"` |
| 07:38:20.026 | `:48638 ESTAB→GONE` |
| 07:38:22.132 | `:48246 NEW→ESTAB` — fresh conn; retry succeeded at 07:38:49 |

`duration_ms=27` is the signature: the RST arrived with the write. **Idle gap ≈72 s.** With `IdleConnTimeout=0` **Go will never evict such a connection**; the CloudFront edge reaped it and only announced it on the next write. Whether a *planner stream* can hit the same dead socket is **[B] inferred** — the mechanism is proven for the executor and the planner demonstrably reuses 106-second-idle sockets, but no planner stream has been observed dying this way.

**Bonus finding — planner streams never return their connection to the pool.** `ParseSSEStreamFull` `break`s at `[DONE]` without draining to EOF [CODE] `:1428-1430`, so `defer resp.Body.Close()` on a partially-read body makes Go close the TCP connection instead of pooling it. [NET] confirms **9 of 9** stream completions today are followed within ~1 s by `ESTAB→GONE`. (Wasteful, but incidentally protective: a stream never inherits a stale pooled socket from a *previous stream*.)

### D4 Concurrency — the 2×2, and why it exonerates overlap

Method: each call's window reconstructed as `[T_log − duration_ms, T_log]` from the `ai_call` line (exact, immune to interleaving), labelled stream vs non-stream by the nearest `📡 Request URL` line. **2,735 calls parsed, 0 unmatched** across 08-26→09-02 (08-27 excluded — a 2.0 GB log anomaly; 08-29 had zero AI calls).

| | stream CUT | stream SURVIVED |
|---|---|---|
| overlapped ≥1 executor call | 11 | 94 |
| stream alone | 5 | 7 |

**Stripping the provider outage** (all 5 `alone`+`cut` and 1 `overlap`+`cut` are the 01:15 503 storm, `class=http_status`, 450–693 ms):

| | CUT (transport/deadline) | SURVIVED | cut rate |
|---|---|---|---|
| overlapped | **10** | 94 | 9.6% |
| alone | **0** | 7 | 0% |

**Conclusion: no evidence that overlap causes cuts.** Overlap is the *default state* — 93% of surviving streams overlapped an executor call, because the executor cycles every ~2 min and streams run 40–600 s. Observing 10 of 10 cuts under overlap is exactly what a 93% base rate predicts (`P ≈ 0.48`), and the `alone` cell has n=7, which has **no power to detect anything**. This corroborates the `serialize_executor=off` rationale — **but the boot line states a conclusion drawn from an underpowered sample as settled policy.**

The ten genuine cuts: seven are the `600.0 s` `timeout_source=client` class-37 kills (08-31 ×4, 09-01 ×3), and **they stop after 09-01 12:43** — zero since the total-deadline split. Three are the 09-01 23:4x `unexpected EOF` cluster that motivated class 41.

### D5 WSL2 / Windows path

**Mirrored networking** (`/mnt/c/Users/hoang/.wslconfig` → `networkingMode=mirrored`), WSL IP **is** the LAN IP (`10.0.0.141/24`, default via `10.0.0.1`), egress **MTU 1500** on `eth0`, **no proxy** anywhere (and moot — `Transport.Proxy` is nil). **Linux conntrack is ruled out**: `nf_conntrack_tcp_timeout_established = 432000 s (5 days)` ≫ any observed idle gap. Host keepalive defaults (7200/75/9) are overridden per-socket by the Go dialer.

**The CloudFront edge is the prime suspect and the only thing the evidence directly implicates**: 72 s idle → RST on reuse. Our keepalive probes are ACKed (`timer:(keepalive,Ns,0)`, third field 0 unacked) while the edge has already released the session — a keepalive-ACKing middlebox in front of a reaped backend is exactly this shape [B]. **`IdleConnTimeout=0` is the amplifier**: whatever the edge's reap window is, Go keeps offering the dead socket forever. Any nonzero value below that window prevents the 07:38:19 failure outright.

### D6 The socket watcher — running, and blind to the one thing it was built for

`/home/hoang/vl-backups/transport-capture/sockwatch.sh`, **running as PID 2100539 since 00:14** — a **bare background bash loop**, not a systemd unit or timer, **unsupervised**: it will not survive a reboot or a stray kill, and nothing will notice.

It polls `ss -tnopi` every 250 ms. In **5,236 lines** since arming, the transition census is:
```
502 ESTAB->GONE   439 NEW->ESTAB   70 NEW->SYN-SENT   69 SYN-SENT->ESTAB   1 SYN-SENT->GONE
```
**Zero `CLOSE-WAIT`. Zero `FIN-WAIT-1`. Zero `FIN-WAIT-2`. Zero `LAST-ACK`.** The FIN-vs-RST discrimination the script was armed to provide **has never once fired** — at a 250 ms poll, sockets pass through those states faster than a sample. Every teardown is an undifferentiated `ESTAB→GONE`.

Of the 15 DeepSeek-peer teardowns it saw: 9 are normal stream ends, 3 are binary restarts, **1 is the 07:38:20 stale-pool RST — whose FIN/RST fact came from Go's error string, not from the capture** — and **2 are entirely unexplained** (00:59:48.120, 04:50:11.262). Its `->GONE` branch omits the `$info` field, so every gap measurement carries a **≤10 s quantization** from the preceding heartbeat.

## 12. SECTION I — TOOLS AND TESTS

### I1 — the structural finding

`grep -rn "SafeHTTPClient" --include=*_test.go .` → **one hit, and it is a comment explaining why the production transport is not used** (`mcp/ai_timeout_contract_test.go:30`).

> **Not a single test in the repository instantiates the production transport.** Every stream/timeout/retry fixture substitutes `&http.Client{Timeout: …}` with `http.DefaultTransport`. The SSRF `DialContext`, the `IdleConnTimeout=0` pool, the 600 s dialer, the absent `ForceAttemptHTTP2`, and executor/planner connection sharing are **all untested by construction — and every defect found in D1/D3 lives in exactly that untested region.**

**Vacuous or toy fixtures found (A11):**
- **`TestProbePeerRSTMidBodyErrorString`** (`mcp/transport_cut_probe_test.go:60`) — **has no assertion at all.** The body is `_, err := …`, `t.Logf(…)`, `_ = bufio.NewReader`. There is no `t.Fatalf`/`t.Errorf` anywhere in the function. **It passes whether the call succeeds, fails, or returns anything whatsoever** — in the very file written to characterise transport cuts.
- **`TestEffectiveAIParamsSnapshotExposesHiddenDefaults`** (`mcp/config_no_hardcode_test.go:62`) — the test process sets no `AI_*` env, so *all* Set-flags are false by construction; the assertion is guaranteed by the environment, not the code. Its condition also `&&`s **two identical string operands**.
- **The class-37 toy family, with a sibling.** `TestStreamSlowButAliveSurvivesIdle` uses `Timeout=10s` against a ~510 ms stream (the fixture class-37 named). **`TestStreamTransportResetRetryEngages` has the same disease** — a 5 s idle and a 10 s ceiling against a millisecond-scale stream, both knobs inert.
- `TestClass37TotalDeadlineAbortsWithClass` asserts `el > 3*time.Second` against a **400 ms** deadline — it would still pass if the deadline were 2.9 s. `TestT6` asserts an OR of two broad substrings that nearly any timeout error satisfies.
- **The class-41 default schedule is never exercised**: `TestClass41StreamRetryLoopFollowsSchedule` injects `{10ms, 20ms}`, so the shipped 2s→15s→45s runs in no test.

Genuine and valuable: `TestClass37LiveStreamBeyondHTTPTimeoutSurvivesUnderTotal` (actually crosses the ceiling), `TestClass37ClassifyAIErrorTable` (10 verbatim journal strings), `TestClass41ProviderFailureClasses`, `TestClass41TransportFailureResendsIdenticalPrompt` (sha256-compares the two prompts).

**Untested by anything:** connection reuse/pool behavior · a stale pooled connection RST on first write (the 07:38:19 class) · the negotiated HTTP version · `IdleConnTimeout` · the dialer timeout · `Transport.Proxy` being nil · that the planner and executor share a client.

### I2 — boot-line guards

**There is no boot-time assertion of any kind on the AI or transport configuration.** Every transport-touching boot line is a `logger.Infof`, not a gate. `🔐 BOOT INTEGRITY` is a real gate (revision prefix + prompt goldens → `tradingRefused`) but checks **the binary revision and prompt text, not the client**. `VerifyPromptGoldens` covers 3 futures *prompt* goldens and nothing about transport.

**And the one control built to stop silent defaults does not cover the transport knobs:** `AI_PLAN_STREAM_TRIES`, `AI_PLAN_STREAM_BACKOFF`, `AI_PLAN_STREAM_IDLE_SECS` and `AI_PLAN_TOTAL_DEADLINE_SECS` are **absent from the `⚠️ AI params at UNSET defaults` audit list** [CODE] `main.go:139-166`. The four values that decide whether a stream lives are silent defaults with no WARN.

### I3 — what the observability surface cannot see

1. **FIN vs RST and its direction — NOT OBSERVABLE.** 0 hits in 5,236 watcher lines; the only FIN/RST fact we hold came from a Go error string.
2. **No TLS/ALPN visibility** — nothing records the negotiated protocol. The HTTP/1.1 conclusion rests on [CODE] + the [NET] two-connection inference.
3. **No pool instrumentation** — `httptrace.ClientTrace` is used nowhere, so there is no `GotConn{Reused, WasIdle, IdleTime}` signal. "Did this request reuse a stale conn?" is only recoverable by hand-correlating socket heartbeats with `ai_call` timestamps, which is exactly what D3 required.
4. **≤10 s gap quantization** on every watcher measurement.
5. **`timeout_source=` is unreliable** — 22 of 32 "transport" lines in the window are HTTP 503s.
6. **The Windows side is opaque from Linux** in mirrored mode (Hyper-V WFP, Defender inspection, any Windows flow table).
7. **The watcher is unsupervised** — if it dies, the evidence stream ends silently.
8. **Nothing captures the peer's own close reason** — no `Connection: close` logging, no close_notify observation.

---

## 13. SECTION H — EVERY FAILURE, 7 DAYS (the census)

**Definition:** every `ai_call … ok=false` row — each individual HTTP try to `api.deepseek.com` that returned no model answer. **n = 50** over 2026-08-26 00:00 → 2026-09-02 07:43:40 CT, against **3,633** total attempts = **1.38%**. Sources: the eight file logs (verified strictly sequential, no overlap). **A12 cross-check — sources agree:** the journal holds **49** of the 50, every timestamp matching exactly including the doubled `01:15:54`; the one absentee (`08-26 20:40:02`) predates the journal's first line. Coverage difference only, no content disagreement.

The full 50-row table with verbatim error text lives in the source data; the roll-up that matters is the **reclassification**, because the logged labels are wrong on nearly half the rows:

| my class | meaning | n | share |
|---|---|---|---|
| **OUR-CEILING** | **our own `http.Client.Timeout=600s` killing a call that was working** | **15** | **30%** |
| **PROVIDER-503** | HTTP 503 at request time, sub-second | 22 | 44% |
| **PEER-CUT-MID** | reset/EOF after N seconds with bytes already received | 9 | 18% |
| **PEER-CUT-PRE** | reset on the send, zero bytes | 2 | 4% |
| **STALL-600** | HTTP 200 headers, then zero body bytes for 600 s | 1 | 2% |
| **EMPTY-200** | HTTP 200 with an empty body | 1 | 2% |

**The single largest self-inflicted category: 15 of 50 failures (30%) are our own 600-second ceiling killing calls that were succeeding.** Seven are **[A] certain** — live SSE streams with `ttfb_ms ≈ 500` that had already received **73,196 / 71,414 / 126,768 / 133,667 / 134,322 / 140,177** reasoning characters when our timer cut them at exactly 600.000 s. The other eight are **[B]**: pre-stream non-stream planner calls at exactly 600.000/600.001 s whose successful neighbours on the same path ran 371, 448, 473, 488, 532 and 545 s — the ceiling sat **inside** the normal distribution. Corroborated by the fix's own commit `75130d59`: *"http.Client.Timeout (600s) was killing LIVE max-reasoning streams (11/80 attempts 08-30→09-01)"*.

**Per day** [RUNTIME]: 08-26 1/666 (0.15%) · 08-27 1/663 (0.15%) · 08-28 6/510 (1.18%) · **08-29 0/0 — no calls at all** (Saturday, CME closed) · 08-30 2/220 (0.91%) · 08-31 7/549 (1.28%) · 09-01 8/778 (1.03%) · 09-02 **25/247 (10.12%)** — of which 22 are the single 503 burst.

**Before / after class 41** (live 06:27:45 CT 09-02): before **49/3,589 = 1.37%**; after **1/44 = 2.27%**. **The "after" cell proves nothing and must not be read as a result** — in that 76-minute window `grep -c '📐 planner attempt'` = **0**. Not one planner attempt ran, so `stream_tries=3`, the 2s→15s→45s backoff and `resend_identical` have never fired in anger; the one failure was on the non-stream executor path, which class 41 does not govern.

**The split that DOES separate — path, not hour.** Restricted to the stream era (08-31 00:08 → 09-02 07:43, n=1,458):

| path | ok | fail | n | fail rate |
|---|---|---|---|---|
| planner (SSE stream) | 125 | **17** | 142 | **11.97%** |
| executor (non-stream) | 1,296 | 20 | 1,316 | **1.52%** |

**7.9×.** That is a property of the call *shape* — long max-reasoning streams (71k–140k reasoning chars, 250–600 s wall) are exposed to both the ceiling and to mid-body peer cuts in a way that 5–170 s executor calls are not.

## 14. SECTION G3 — TIME OF DAY: THE CHINA-WORKDAY HYPOTHESIS IS **NOT** SUPPORTED

Beijing = CT + 13. Denominator is every attempt, `ok=true` and `ok=false`, because our own volume is session-shaped. (It turns out to be near-uniform, 134–196/hr outside the 16:00 CT CME break, but the rates are shown per bucket regardless.)

**Raw arithmetic:** China workday (Beijing 09:00–21:59 = CT 20:00–08:59, 13 buckets) → **40 / 2,100 = 1.905%**. Off-hours (11 buckets) → **10 / 1,533 = 0.652%**. **Ratio 2.92×** — which looks like a clear cluster.

**It is not.** The entire effect is one incident: the 09-02 00:59:47–01:15:57 burst contributes **24 of the 40** workday failures (23 in the CT-01 bucket alone). Excluding that single 12-minute event:

| | n | failures | rate |
|---|---|---|---|
| China workday | 2,076 | 16 | **0.771%** |
| off-hours | 1,533 | 10 | **0.652%** |
| ratio | | | **1.18×** |

**Plain statement: failures do not cluster in China's workday.** A 0.12 pp difference on 26 events total is noise at this n; 16 vs 10 is nowhere near separable. And the two secondary bumps point the wrong way for the hypothesis — CT-23 (Beijing 12:00) is the 09-01 EOF cluster, and CT-12 (**Beijing 01:00, the middle of China's night**) holds four failures, two of them the 09-01 12:33/12:43 OUR-CEILING pair. The largest single reclassified category, OUR-CEILING, is by construction independent of DeepSeek's load — it is our own timer.

---

## 15. SECTION J — ROOT CAUSE

### J1 — Ranked causes, each with what it explains and what it does not

**① The prompt/validator contradiction on `breakdown_continue` — the session killer.** [CODE]+[RUNTIME], §4.
*Mechanism:* `kernel/planner_prompt.go:589` issues a standing unconditional *"If price sits BELOW PDL you MUST write a continuation short"*, `:623` binds "continuation" to `breakdown_continue|breakup_continue`, the prompt is **never told a given breakdown level is already void**, and `kernel/breakdown_continue.go:254` rejects exactly that play at write time once a close has come back across it. The correction arrives as a ~92–127-token tail on a ~6,341–6,691-token prompt (≈1.5%) that still contains the MUST.
*Explains:* 37 of 194 failed attempts directly, 82 counting the whole `breakdown_continue` family (42.3%), **8 of the 17 fail-closes**, and the 09-02 LONDON regression where attempt 3 returned to the very level attempt 1 was rejected on.
*Does NOT explain:* the other reject families (`arm_legs_sweep_reclaim_only` 28, `flip.rule invalid` 18), and nothing on the transport side.

**② Our own 600-second ceiling — the largest self-inflicted transport cause, now fixed for the planner.** [RUNTIME], §13.
*Mechanism:* one `http.Client.Timeout` served both a 5-second executor call and a 10-minute max-reasoning stream. Class 37 split them (planner total → 1200 s, `cp.Timeout = 0` for streams).
*Explains:* **15 of 50 failures (30%)**, including seven streams killed with up to 140,177 reasoning characters already in hand.
*Does NOT explain:* anything after 09-01 12:43 — **zero** `timeout_source=client` stream kills since. The 588.1-second success at 07:44:03 (11.9 s inside the old ceiling) is the proof the fix is load-bearing.
*Residual:* the **executor** still runs at 600 s, and that is **exactly DeepSeek's documented 10-minute pre-inference close** — see ④.

**③ Provider load-shedding — real, bounded, and mishandled rather than harmful.** [RUNTIME], §2/§13.
*Mechanism:* DeepSeek returns sub-second 503s under capacity pressure. All **35** call-level 503s in seven days fall in **one 12-minute window**; there is no background rate and **zero 429/500/502/504** anywhere.
*Explains:* 22 of 50 failures, 8 lost executor decision cycles (#21–#28), and a near-miss that burned an entire 3-attempt planner budget in **7 seconds**.
*Does NOT explain:* any fail-close (0 of 17), and not the LONDON one — that read began 14 minutes after the last 503 and all three of its calls returned `ok=true`.
*The real defect it exposed:* the 503 body was fed back **as a validator reason**, so the planner rewrote its prompt to "fix" a provider outage and the regression detector then scolded it twice for failing to. Fixed in HEAD (`IsProviderFailure`, `st >= 500 || st == 429`); the binary that hit it predated the fix.

**④ `IdleConnTimeout = 0` against a reaping CloudFront edge — proven, and trivially fixable.** [NET]+[RUNTIME], §11 D3.
*Mechanism:* the transport never evicts an idle pooled connection; the edge reaps the session after ~72 s and announces it only on the next write. Observed end to end at 09-02 07:38:19: idle 72 s → reuse → RST at **27 ms** → retry succeeded 30 s later.
*Explains:* the two PEER-CUT-PRE failures, and it is the mechanism behind the 58.8-second "failed to send request" anomaly.
*Does NOT explain:* the mid-body cuts (bytes were flowing), the 503s, or the ceiling kills.
*Note:* every planner stream *closes* its connection rather than pooling it (9/9, because `break` at `[DONE]` leaves the body undrained), so a **stream** inheriting a dead socket from a previous stream is structurally impossible; the executor is the exposed path. A planner stream hitting a socket the *executor* left is [B] — mechanism proven, instance never observed.

**⑤ Executor/planner concurrency — EXONERATED.** [RUNTIME], §11 D4.
Stripping the 503 storm: overlapped streams cut **10/104 = 9.6%**, streams alone cut **0/7 = 0%**. But overlap is the *default state* (93% of surviving streams overlapped), so 10 of 10 cuts under overlap is exactly what chance predicts (P ≈ 0.48), and the alone-cell n=7 has no power. **No evidence overlap causes cuts** — though the boot line states that conclusion, drawn from an underpowered sample, as settled policy.

**⑥ SSE parser limits — latent, not implicated.** [CODE], §10 E1.
`bufio.Scanner` at the default 64 KiB with no `Buffer()` call, while the sibling Databento reader raises it to 1 MiB. `grep "token too long"` → **n=0**, so it has never fired. It matters because of the *handling*: `ErrTooLong` → `class=other` → not retried, not a provider failure → **quoted back to the model as the reason its plan was rejected**.

**⑦ NAT / keepalive — ruled out on the Linux side.** [RUNTIME], §11 D5. `nf_conntrack_tcp_timeout_established = 432000 s (5 days)` ≫ any observed gap. WSL2 is in **mirrored** mode on a real LAN address, MTU 1500, no proxy. The Windows-side WFP/Defender path is opaque from Linux and remains [C].

**⑧ Request-body non-compliance — no failure attributable.** [DOC]+[CODE], §7. We send the two documented headers and every field DeepSeek documents; `max_tokens=65536` is legal (17% of the 384K ceiling). The one true mismatch is `temperature` in thinking mode, which the provider ignores. **No observed failure traces to the body.**

**⑨ New, found here — unhandled `finish_reason`s.** `content_filter` and `insufficient_system_resource` appear **nowhere in the codebase** and are treated as complete successes with whatever partial content arrived. The second is DeepSeek's documented mid-generation capacity signal — precisely the failure a provider-overload retry should catch, and today it is invisible. n=0 observed, so latent.

### J2 — THE VERDICT

> **The dominant cause of session-losing failures is not DeepSeek. It is a contradiction inside our own prompt.**
>
> **17 of 17 fail-closes in seven days were caused by model output the validator refused; 0 were caused by a transport failure, a 503, or a deadline** (`grep -icE "503|overload|transport|deadline|EOF|reset|timeout"` over all 17 → 0). Of those 17, **8 name `breakdown_continue`** — the play `kernel/planner_prompt.go:589` orders the model to write and `kernel/breakdown_continue.go:254` refuses once the tape has moved.
>
> Of the 194 failed planner attempts, **175 (90%) are the model failing our schema and 19 (10%) are provider or transport** — a ratio two independent censuses reached separately.
>
> The transport failures are real and expensive — 15 of 50 were **our own 600-second ceiling** discarding up to 140,177 reasoning characters per call, and 8 executor decision cycles were lost to one 503 burst — but **not one of them cost a session.** The system's own retry machinery absorbed them.

### J3 — What is still UNVERIFIED, and the one observation that settles each

| # | unverified | the observation that would settle it (owner to approve) |
|---|---|---|
| 1 | **FIN vs RST and its direction.** The watcher has produced 0 `CLOSE-WAIT`/`FIN-WAIT` in 5,236 lines; the only RST fact we hold came from a Go error string. | `sudo apt install tcpdump`, then during a planner read: `sudo tcpdump -i eth0 -s 0 -w /tmp/ds.pcap 'host 3.173.21.63 and port 443'`. Also settles the ALPN question (#2) from the same capture. |
| 2 | **Negotiated HTTP version, directly.** [CODE]+[NET] both say HTTP/1.1; no ALPN was ever observed. | the ClientHello/ServerHello in the capture above. No-root alternative: one restart with `GODEBUG=http2debug=1`. |
| 3 | **Whether class 41 works at all.** `stream_tries=3`, the 2s→15s→45s backoff, `resend_identical` and the `⏱` watchdog line have **never executed** — 0 planner attempts since the boot, and the idle watchdog has never fired in any log, ever. | the next planner attempt meeting a transport cut or 5xx: look for `retrying (2/3)`, a 15 s or 45 s gap, `🧩 … resend-identical`, and the **absence** of `reauthor+block` after a provider-class failure. |
| 4 | **That "resend-identical" is byte-identical** — it will not be provable even when it fires, because `shortHash(prompt)` is stored in `plans.prompt_hash` but **never logged on the attempt lines**, and provider failures deliberately write no rejected-prompt row. | log the prompt hash on the `🧩` line. Uninstrumented by construction today. |
| 5 | **Whether a planner *stream* can inherit a dead pooled socket** (the executor demonstrably does). | add `httptrace.ClientTrace` `GotConn{Reused, WasIdle, IdleTime}` logging — a small additive change that turns every future reuse into a labelled data point. |
| 6 | **The CloudFront edge's actual idle-reap window.** One data point (72 s) plus two unexplained teardowns (00:59:48, 04:50:11). | the same `httptrace` line; or extend `sockwatch.sh` to print `$info` on its `->GONE` branch, cutting the ≤10 s quantization to 250 ms. |
| 7 | **What the 09-02 01:03:52 HTTP-200 body contained** — it burned 244.3 s and is unattributable because the body is never logged. | log the first ~512 bytes on the empty-choices branch. Nothing can recover the historical instance. |
| 8 | **Whether `request_id` is absent because DeepSeek sends none or because we read the wrong header.** `request_id=""` on 20 of 20 calls means **no failure in this audit can be correlated with the provider's own logs.** | log `resp.Header` keys once at debug level. |
| 9 | **Windows-side firewall / Defender / Hyper-V flow expiry** — opaque from Linux in mirrored mode. | elevated PowerShell: `Get-NetFirewallHyperVProfile`, `Get-MpPreference | Select EnableNetworkProtection`, and `pktmon start --etw`. |

### J4 — Fixed · in flight · open

**Already fixed and load-bearing:** class 37's ceiling split (**proved** by the 588.1-second success that would have died at 600 s) · class 41's `IsProviderFailure` so a 5xx no longer re-authors (the 01:15 disease) · class 39 arm-leg normalization · class 38's 17 contract restrictions now stated in the prompt · class 35's recorded replan counter (which, correctly, spent nothing across today's three-version LONDON chain).

**In flight:** ROOT-FIX part A (measured — plan JSON is only 3.9% of output, which killed part A's premise) and part B (the shadow A/B instrument, **dormant**: `SHADOW_AB_ENABLED` is unset in both `.env` and the live process environment, so it cannot fire without a restart; its `facts` column is in the code but **not yet in the live database** because `AutoMigrate` is lazy and no reject has occurred since the 07:32:15 boot).

**OPEN, with proposed owner rulings:**

1. **The `breakdown_continue` contradiction (J1-①) — the only item that has cost sessions.** Ruling needed on one of: make `:589` tape-aware; soften it to "a short-direction scenario" and let the model pick the condition (the validator already accepts any short — `hasDirection(d.Scenarios, "short")`); or feed the validator's own knowledge forward so the facts block says *"breakdown at 29021.25 is VOID (close came back 01:1x)"* and the model never authors it. Also align `plan_doc.go:842`'s message with what its code actually requires.
2. **A 503 storm can still destroy a scheduled session in seconds.** On 09-02 it burned three attempts in 7 s and was benign only because it hit a *wake* read. Class 41's backoff (2s→15s→45s) lengthens that to ~62 s but still consumes the whole budget. Ruling: should a provider-class failure consume a planner **attempt** at all, or only a client **try**?
3. **`IdleConnTimeout = 0`.** One nonzero value below the edge's reap window closes the PEER-CUT-PRE class outright. Ruling on the number.
4. **The executor's 600 s ceiling equals DeepSeek's documented 10-minute no-start close** — zero margin, and the two causes are indistinguishable in our logs. It cost 10 minutes of executor blindness on 09-02 and contributes to the 96 cycle overruns.
5. **`timeout_source=transport` is wrong on 23 of 28 classifiable failures** and `class=other` failures (empty-200, parse, `ErrTooLong`) are still fed to the model as validator reasons — the 01:15 disease, still open for every non-status failure.
6. **Provider fallback does not exist.** "DeepSeek 2" is enabled and keyed but bound to nothing, and **no code re-resolves the client after a failure**. Ruling: wire it, or disable the row so it stops implying a fallback that isn't there.
7. **Hygiene:** the boot line prints `AI_MAX_RETRIES=calls` (a format-string bug) · four of six class-41 boot fields are string literals that would not change if the code did · the four class-37/41 stream knobs are absent from the UNSET-defaults audit · `TestProbePeerRSTMidBodyErrorString` has **no assertion at all** · **no test instantiates the production transport**, so every defect in J1-④ lives in untested code · 33 × HTTP 402 from the deprecated upstream website link (removed in the VL rename) key are still firing · the socket watcher is an unsupervised bash loop.

---

## 16. CLOSEOUT

**Read-only compliance:** no code, config, knob, key or DB write; no restart, cancel, reset or cutover; **no live provider call** (A3 — every figure comes from the running system's own calls); no lock taken (none was present). The only writes are this report on `docs/deepseek-e2e-audit-0902` in `~/vl-dsaudit`.

**What the owner will still see wrong on screen:** sessions that fail-close on `breakdown_continue` and need a manual reset (2026-09-01 ASIA four times, 2026-09-02 LONDON once) · a boot line advertising `AI_MAX_RETRIES=calls`, `keepalive=30s` read from nothing, and `truncation → 🚨 WARN, never silent` which is false for the planner · `timeout_source=transport` on failures that are 503s · `request_id=""` on every call, so no failure can be traced to the provider's side · a `📊 AI call complete (stream)` line on streams that did not complete.

**A9 — commit-ref URL: NOT produced.** The branch is committed locally; a push from this session was classifier-denied in an earlier dispatch and was not re-attempted. The owner publishes with `git -C ~/vl-dsaudit push -u origin docs/deepseek-e2e-audit-0902`, after which the raw URL is `https://raw.githubusercontent.com/johnwick2921-cyber/nofx/<sha>/docs/superpowers/reports/2026-09-02-deepseek-e2e-audit.md` — **curl it for 200 before citing it** (this has 404'd twice before).
