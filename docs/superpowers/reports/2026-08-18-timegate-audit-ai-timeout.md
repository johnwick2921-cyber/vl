> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# Time-gate audit + AI timeout fix — both zero-trade causes were literals from the NY-only era; 57 gates audited, 8 BUG rows, C# clean

**Deploy integrity (0.1): PASS.** Running binary `bb966a04` vs HEAD `49dd83c9` — the gap was docs-only (zero code files), so all code was current. Now deployed `4ebd779a` (boot integrity OK — rev==expected, goldens PASS).

## Root causes

**Incident A (AI timeouts).** The live path is the non-streaming `Call`; the deaths are `http.Client.Timeout` firing mid-`io.ReadAll`. Two defects: `mcp.DefaultConfig` computed `Timeout` from `AI_TIMEOUT_SECONDS` but built the transport with the `DefaultTimeout` CONSTANT (class 4 — env computed then discarded), and `applyDecisionCallTimeout` overrode the futures decision client to a literal 180s (class 3). The observed 150565ms call was a SUCCESS 30s under that cap — proof reasoning responses now legitimately run 150s+ since max_tokens 2000→32768; the slower tail crossed 180s and died. Completing late is safe (`staleBarDiscard` discards decisions computed on superseded bars); dying mid-read never is.

**Incident B (Asia zero-trade).** `entryBlockedByLastEntry` = `timeReachedCT(now, "13:00")` — a DAY-scoped wall-clock test, true 13:00 CT→midnight, built when NY was the only session. It refused the entire Asia evening (live: 21:00 CT refusal). Classes 2+3. Its class-7 twin `enforceEODFlat` (`>=14:45`, day-scoped) never fired on Asia only because last-entry guaranteed no Asia positions — fixing one without the other would have force-flattened every new Asia position on sight.

## Timeout chain, before → after

| link | before | after |
|---|---|---|
| http.Client.Timeout (executor, ninjatrader) | **literal 180s** (SetTimeout) | `mcp.ResolvedAITimeout()` — env-driven, default 300s |
| http.Client.Timeout (planner/default clients) | SafeHTTPClient(**constant** 300s); env discarded | same resolved value — executor/planner can no longer diverge |
| env | `AI_TIMEOUT_SECONDS` computed, never wired | `AI_HTTP_TIMEOUT_SECONDS` (canonical) → `AI_TIMEOUT_SECONDS` (legacy) → 300 |
| Transport (SafeHTTPClient) | dial timeout = whole budget; no per-phase limits | unchanged (SSRF-guard transport; per-phase split noted, not needed) |
| ctx on decision path | none (loop is single-goroutine; tick never cancels) | unchanged + overrun WARN + T7 pins it |
| stream idle watchdog | 180s `AI_STREAM_IDLE_TIMEOUT_SECS` (stream path only) | unchanged (not on the live non-streaming path) |
| retry wrapper | 3 attempts, no per-attempt deadline beyond client | unchanged + structured `ai_call` line per attempt |

Structured log (1.4): `ai_call model=<m> duration_ms=<d> finish_reason=<r> ok=<bool>` + on failure `timeout_source=client|context|transport deadline_s=<n>`.

## Full time-gate audit (Phase 3.2)

3 audit lanes + adversarial verification (5 agents, 89 raw rows, deduped below). C# lane (VLTraderTCPClient / VLBarsSubscriptionManager / legacy vltrader): **16 gates, 0 BUG** — signal staleness and watchdogs use UTC-epoch or exchange time correctly; no AddOn change needed, protocol untouched.

| gate | file:line | tz | scope | value source | wired | verdict |
|---|---|---|---|---|---|---|
| contract-roll entry block (<=5 days to quarterly expiry) | `kernel/engine.go:1007-1013` | UTC | day | literal 5 days, 3rd-Friday convent | yes | **BUG** |
| enforceEODFlat force-flatten | `trader/auto_trader_clock.go:224-256` | America/Chicago | day | config eod_flat_ct (default litera | yes | **BUG** |
| entryBlockedByLastEntry last-entry cutoff | `trader/auto_trader_clock.go:209-218` | America/Chicago | day | config last_entry_ct (default lite | yes | **BUG** |
| registry per-session FlatCT / EffectiveFlatCT (session fla | `kernel/session_registry.go:214-227` | America/Chicago | session | registry | defined-not-called | **BUG** |
| rolling-24h daily P&L reset | `trader/auto_trader_loop.go:232-237` | n/a | day | literal 24h rolling | computed-not-consumed | **BUG** |
| session digest "session closed" check | `trader/auto_trader_planner.go:955-973` | America/Chicago | day | registry WindowEndCT + CMESessionD | yes | **BUG** |
| stopUntil risk-pause gate | `trader/auto_trader_loop.go:222-230` | n/a | tick | literal zero-value time | defined-not-called | **BUG** |
| weekly matched-random freeze (Sunday, ISO week) | `trader/auto_trader_matched_random.go:57-69` | server-local | day | literal Sunday + ISOWeek | yes | **BUG** |
| B3 order dedupe + rate breaker | `trader/ninjatrader/order_guard.go:17-59` | UTC | order | literal 55s dedupe window, 10 acti | yes | **OK** |
| B4 stale-data entry block (2x bar interval) | `kernel/stale_data.go:16,43-88` | UTC | tick | literal 2x 1m/5m interval | yes | **OK** |
| C2 clock-drift guard (report-only) — verify | `kernel/clock_drift.go:26,52-84` | UTC | tick | literal 60s vs feed-bar clock | yes | **OK** |
| H8 registry-veto fix (sessionRunnable resolver) — verify o | `trader/auto_trader_planconfig.go:104-121;` | n/a | session | config override > registry+session | yes | **OK** |
| IsCMEOpen weekend/holiday/daily-break gate | `kernel/cme_calendar.go:17-35` | America/Chicago | session | literal Globex schedule (Sun 17:00 | yes | **OK** |
| MaybeResetDaily session-day window reset | `kernel/risk_limits.go:152-169` | America/Chicago | day | CMESessionDayKey of the passed now | yes | **OK** |
| SessionDef.IsReadTime exact-minute primitive | `kernel/session_registry.go:245-254` | America/Chicago | session | registry ReadCT | defined-not-called | **OK** |
| ShouldSkipDecisionCycle (kernel CME-closed twin) | `kernel/engine.go:981-990` | America/Chicago | session | config TradingMode + IsCMEOpen | yes | **OK** |
| Strategy Studio time blackout window | `kernel/engine_analysis.go:179-186` | America/Chicago | day | config BlackoutStartCT/EndCT (togg | yes | **OK** |
| T1 red-news blackout (calendar -> blocking) | `trader/auto_trader_session.go:123-127` | America/Chicago | day | calendar feed (ForexFactory) + lit | yes | **OK** |
| TCP heartbeat interval / ack timeout | `provider/ninjatrader/tcp_server.go:34-39` | n/a | tick | literal 30s ping / 60s ack close | yes | **OK** |
| active-plan validity window (executor plan lookup) | `trader/auto_trader_planner.go:1057-1110` | America/Chicago | plan | registry + config via sessionRunna | yes | **OK** |
| alert-feed prune (once per session-day, 7d cutoff) | `trader/auto_trader_alerts.go:35-61` | America/Chicago | day | literal 7 days; P0/P1 never pruned | yes | **OK** |
| approval gate (per CME session-day grant) | `trader/auto_trader_planconfig.go:163-177` | America/Chicago | day | config approval_required + system_ | yes | **OK** |
| backoffWhileClosed idle backoff | `trader/auto_trader_loop.go:688-700` | n/a | tick | literal 3m in 10s stop-responsive  | yes | **OK** |
| barCloseGate bar-close cadence (day-plan) | `trader/auto_trader_clock.go:107-115` | UTC | tick | bar CloseTime watermark (epoch ms) | yes | **OK** |
| calendar producer schedule (fetch + throttle) | `trader/auto_trader_calendar.go:24-79` | America/Chicago | day | literal 1h retry throttle + skip-f | yes | **OK** |
| closed-trade analytics epoch (dayplan_analytics_since) | `trader/auto_trader_clock.go:323-350` | UTC | boot | first-run epoch stamp (system_conf | yes | **OK** |
| cmeSessionClosedSkip (hoisted whole-cycle session gate + e | `trader/auto_trader_loop.go:651-682` | America/Chicago | session | config TradingMode=futures | yes | **OK** |
| consecutive-loss halt (session-day) | `trader/auto_trader_orders.go:107-130` | America/Chicago | day | config ConsecutiveLossHalt (0=OFF) | yes | **OK** |
| daily digest roll window [15:00,16:00) CT | `trader/auto_trader_clock.go:167-176` | America/Chicago | day | literal window + config evening_di | yes | **OK** |
| daily guardrails (loss/profit/max-trades) on session-day P | `kernel/engine_analysis.go:140-196` | America/Chicago | day | config per-strategy limits (env fa | yes | **OK** |
| data_freshness suite (90s RTH / 5m ETH / 5% drift / IsRTH) | `market/data_freshness.go:43-50,73-163` | America/Chicago | tick | literal thresholds | defined-not-called | **OK** |
| dead-man watchdog (disconnect -> block -> reconcile -> res | `trader/dead_man_watchdog.go:91-106` | n/a | tick | link state (event-driven, no timer | yes | **OK** |
| decisionCallTimeout 180s AI-call cap | `trader/auto_trader_loop.go:55` | n/a | tick | literal 180s (evidence-derived) | yes | **OK** |
| defer-until-balance boot gate (closest thing to a boot gra | `trader/auto_trader_loop.go:249-259` | n/a | boot | first account_balance frame (no ti | yes | **OK** |
| drawdown monitor schedule | `trader/auto_trader_risk.go:18-38` | n/a | tick | literal 1-minute ticker | yes | **OK** |
| effectiveEODFlatCT half-day early-close pull-in | `trader/auto_trader_clock.go:178-190` | America/Chicago | day | registry HalfDays keyed by CMESess | yes | **OK** |
| feed-connected gate (IsFeedConnected 90s live-bar override | `provider/ninjatrader/tcp_server.go:1016-1040` | UTC | tick | literal 90s | yes | **OK** |
| first-5m no-trade sub-window | `trader/auto_trader_session.go:132-139` | America/Chicago | session | registry WindowStartCT + literal 5 | yes | **OK** |
| gate-block counter + error-event day rollover | `trader/auto_trader_loop.go:139-144` | America/Chicago | day | literal 17:00 CT session-day bound | yes | **OK** |
| killzones (registry high-probability windows) | `kernel/session_registry.go:35-49,105-108,236-243;` | America/Chicago | session | registry | computed-not-consumed | **OK** |
| last_entry_ct / eod_flat_ct config plumbing (dual-codec ch | `store/strategy.go:897-902,1114-1115;` | n/a | day | config | yes | **OK** |
| lunch no-trade window 12:00-13:30 CT | `trader/auto_trader_session.go:120-122` | America/Chicago | day | literal "12:00"-"13:30" | yes | **OK** |
| night-mode edge observer | `trader/auto_trader_session.go:141-168` | America/Chicago | session | registry | yes | **OK** |
| per-session trade cap (since session-instance start) | `trader/auto_trader_session.go:54-95` | America/Chicago | session | config sessions[].max_trades (abse | yes | **OK** |
| plan-chain identity PlanChainTradeDate/SessionInstanceStar | `kernel/plan_chain_date.go:34-66` | America/Chicago | plan | registry | yes | **OK** |
| plan-death acceptance interval (2x5m / 15m-close) | `kernel/scenario_facts.go:58-91` | UTC | plan | config acceptance_rule via per-ses | yes | **OK** |
| planner read window inSessionReadWindow (W1) | `trader/auto_trader_clock.go:147-165` | America/Chicago | session | registry ReadCT/WindowEndCT | yes | **OK** |
| planner weekend/holiday guard (session-instance open) | `trader/auto_trader_planner.go:176-179` | America/Chicago | session | registry + IsCMEOpen | yes | **OK** |
| re-entry cooldown after stop-loss exit | `discipline/reentry_cooldown.go:74-106` | UTC | order | config ReentryCooldownMinutes (0=O | yes | **OK** |
| re-plan budget schedule (death -> replan -> NO-TRADE) | `trader/auto_trader_planner.go:209-249` | n/a | plan | config replan_cap (live store read | yes | **OK** |
| reconcile-before-open flatten await | `trader/auto_trader_orders.go:302-312` | UTC | order | literal 35s (heartbeat-aware) / 50 | yes | **OK** |
| scan-loop ticker schedule | `trader/auto_trader.go:775-808` | n/a | tick | config ScanInterval | yes | **OK** |
| session entry gate (enabled-window only) | `trader/auto_trader_session.go:27-48` | America/Chicago | session | registry windows + config sessions | yes | **OK** |
| session-registry cache refresh (admin edits) | `trader/auto_trader_registry.go:28-41` | America/Chicago | day | registry (system_config) cached pe | yes | **OK** |
| signal freshness (feed-stamped timestamp; C# 60s reject tw | `trader/ninjatrader/tcp_trader.go:193-210` | UTC | order | feed bar clock, local-clock fallba | yes | **OK** |
| staleBarDiscard (decision spanned a bar close) | `trader/auto_trader_loop.go:64-66` | UTC | tick | bar CloseTime comparison | yes | **OK** |

TOTAL: 56 unique gates

## BUG rows fixed (Phase 3.4)

| gate | class | commit |
|---|---|---|
| last-entry cutoff (day→session scope) | 2+3 | `862bcd41` |
| EOD flat (day→session twin) | 2+7 | `889c2437` |
| AI call timeout chain | 3+4+7 | `33d7eed2` |
| session-digest wrap-blind close test | 2 | `039f4bb8` |
| weekly matched-random freeze in host tz | 1 | `844c9580` |
| TimeoutSet honesty + Request-path top_p | 7 | `8944e07a` |
| UI timelines browser-local (owner contract: Houston for all viewers) | 1 (presentation) | `12f92bf7` |
| FlatCT dead field / stale planner-client comment / zero-only dailyPnL | 5/7/4 | `8218b4ea` (comment-truth) |
| clock health (3.5) | — | `4ebd779a` |

## Found but NOT fixed (section 9)

- **stopUntil risk-pause gate** — class 5, benign fail-open: nothing ever assigns it, the pause can never fire. Wiring it up creates new trading behavior → owner decision. Size S.
- **contract-roll entry block** — class 5, inert: live symbol is continuous "MNQ" so `NextExpiry` never matches; near-expiry entries are NOT blocked. Real gap; wiring could block entries near quarterly roll → owner decision. Size S–M.
- **rolling-24h dailyPnL** — class 2+4, cosmetic: field is never written (always 0), display-only. Documented in-code, left.
- **claw402 5m timeout + x402 90s stream idle literals** — class 7 minor, payment paths; deliberately untouched (not the AI decision path).
- **HalfDays has no producer** — the half-day early-close pull-in is correct but dormant until the calendar (or admin) populates `HalfDays`; the comment claiming P1.8 populates it is wrong. Size S.
- **killzones** — rendered into prompts + adherence grading only; no hard gate. Matches spec (grading-only) per the checklist; noted, not a bug.
- **dp.LastEntryCT / dp.EODFlatCT legacy config** — now bypassed; if the owner had set them they are no longer honored (replaced by per-session offsets). Both were unset in the live config.
- **Ask-Planner / agent paths share the planner client** — timeout now identical everywhere, so harmless; noted for the record.

## Verification (Phase 4)

T1–T7 PASS (details in test files): T1 21:00 CT ASIA passes the gate (the exact refused instant) · T2 NY offset 105 → cutoff 13:00, 13:05 refused "(NY)", 12:55 passes · T3 London 05:00 passes / 08:20 refused "(LONDON)" · T4 15:30 CT between sessions → not last-entry's refusal · +wrapped ASIA (01:50 refused "01:45 CT (ASIA)") and DST 2026-03-08 rows · T5 2s-delayed mock server completes under an 8s ceiling · T6 3s response vs 1s ceiling fails with the Client.Timeout signature, deadline_s truthful · T7 a ticker fires 20+ times during an in-flight call, nothing cancelled.

Exit bar: `go build/vet/test ./...` green · `-race` clean (kernel/trader/mcp/store) · goldens byte-identical · tsc + `npm run build` OK · vitest 27/29 files (the 2 failures are the documented pre-existing pair). Config-truth: last_entry_offset_min/eod_flat_offset_min survive the codec (pinned).

## Live boot log (Phase 4 excerpt)

```
21:54:03 🔐 BOOT INTEGRITY OK — rev 4ebd779a778e +dirty · built 2026-08-19T02:51:48Z · expected 4ebd779a778e · goldens PASS
21:54:03 🕰 clock-health [boot] go=21:54 CT (02:54 UTC) nt8_last_bar=none drift_ms=n/a timesync{NTP=yes NTPSynchronized=yes} tolerance_ms=60000
21:57:04 🕰 clock-health [session-roll:ASIA] go=21:57 CT (02:57 UTC) nt8_last_bar=21:59 CT (02:59 UTC) drift_ms=-116013 timesync{NTP=yes NTPSynchronized=yes} tolerance_ms=60000
21:57:04 🚨 CLOCK CRITICAL [session-roll:ASIA]: |drift| 116013ms exceeds C2 tolerance 60000ms — check WSL2 time-sync … Log-only: no trading gate added.
```

The new line caught the REAL WSL↔NT8 skew on its first session roll: WSL ~2 min behind NT8 while timesyncd claims synchronized — the exact condition C2's feed-stamping exists for. (Measurement includes up to one bar-interval of quantization from the in-progress bar; true skew ≥56s regardless.) Resolved session windows at the restart: ASIA 17:00→02:00 cutoff 01:45 flat 01:45 · LONDON 02:00→08:30 cutoff 08:15 flat 08:15 · NY 08:30→14:45 cutoff 14:30 flat 14:30 (defaults; per-session offsets configurable).

## PR

https://github.com/johnwick2921-cyber/vl/pull/45 — **#45** (parsed from the `gh pr create` output URL).

**Note on process:** the dispatch's branch+PR flow was followed; the deploy runs from the branch (RELEASE = branch HEAD `4ebd779a`). Merging #45 to main keeps rev continuity. Owner action: none required — deployed and verified live.
