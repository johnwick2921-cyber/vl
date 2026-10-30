import { GUIDE_BUILT_REV, type GuideSection, type KnobSpec } from '../types'

// Knob inventory = the live Strategy page controls (census 2026-08-27).
// "trader" is the one-sentence trader explanation; "consumer" is the engine
// consumer (file:line, verified). Every field is mandatory and linted by test.

const dayPlan: KnobSpec[] = [
  {
    label: 'Plan mode',
    where: 'Strategy → Day Plan → top',
    what: "How the plan constrains entries: ADVISORY informs · DIRECTION blocks against-bias entries · STRICT executes entries ONLY through armed plan scenarios (W3: the market_in_zone policy — a limit inside the planner's entry zone); an AI decision is refused as a market entry, and one that CITES a market_in_zone scenario is a nudge that runs that scenario's armed pass (placement only). No plan = no entries in direction/strict.",
    trader:
      'Strict means no-plan = flat day — the plan is the law, not advice. Since W3 the executor prompt\'s PLAN BLOCK header says exactly that under strict ("… off-plan is refused") instead of the advisory "a valid off-plan setup may still be traded".',
    consumer:
      "store/resolve_source.go ResolvePlanMode — the one resolver (session override → strategy → advisory) · entry points store/strategy.go PlanModeFor and trader/auto_trader_planconfig.go planModeFor · direction block trader/auto_trader_planconfig.go planModeBlocked · executor header kernel/plan_render.go RenderPlanBlockForMode via setExecutorPlanContext (the plan's own session)",
    range: 'advisory | direction | strict',
    systemDefault: 'advisory',
    recommended:
      '⭐ STRICT ×3 — the owner deliberately runs strict on ASIA/LONDON/NY (the plan is the law). ADVISORY remains the code fallback for anyone without a trusted plan writer.',
    whenToTouch: 'When you want the planner to have teeth (or to remove them).',
    perSession:
      'Yes — tri-state per session: inherit global / advisory / direction / strict. Session override wins; inherit (blank) = the global row above.',
  },
  {
    label: 'One setup (switch)',
    where:
      'Strategy → Day Plan → "One setup — arm only the single best reject (fade) level" (switch) + min grade (A/B/C)',
    what: 'The book arms ONE play — the fade (reject) — at the best level near price, only on a permitted day. Gates arm AUTHORIZATION only; never cancels a resting arm, never places. The follow side is recorded, never armed.',
    trader:
      'ON [O] by default (an unset strategy reads ON — the field is a tri-state so an unset value is never read as OFF). OFF restores the wide book byte-identically (pinned against a golden generated before the wave existed).',
    consumer:
      'store/resolve_source.go ResolveOneSetup · trader/one_setup_wiring.go oneSetupConfig · trader/armed_executor.go (the seam)',
    range: 'true / false',
    systemDefault: 'ON (nil)',
    recommended:
      "⭐ ON — the owner's ruling of 2026-09-10; the less-contradicted side of round 17, not a proven one.",
    whenToTouch:
      'Only to restore the wide book for a comparison; the boot line names the switch and its source. W3 PRECONDITION: with entry_policy_default=market_in_zone, ON declines every non-reject play (play_not_reject) — only reject arms can place; set it OFF to trade acceptance/hold/reclaim/waterfall arms under strict. The 🎛 entry law boot line WARNs while both are on. W5 (D8 ruling): one_setup governs PLANNER plays only — a Picture scenario (📷 P1…) is admitted by its own switch and one_setup never declines, retires, ranks or re-targets it; a 📷 WARN says so at boot while Picture is on and this is ON.',
    perSession: 'No.',
  },
  {
    label: 'Planner contract (A3)',
    where:
      'No Studio control — stored setting day_plan.planner_contract (API/config field).',
    what: "WAVE PLANNER A3 (2026-09-25): the prompt, the validator and the executor are ONE contract — breakdown/breakup entries must wait for the tape's confirming close, planned_order is legal only on reject / fvg_entry / sweep_reclaim leg 0, every scenario's economics must carry nonzero risk, and a REJECT fade's stop is composed by the executor from the frozen zone (edge − buffer).",
    trader:
      "ON by default (nil). Turning it OFF restores the pre-A3 prompt text byte-for-byte — the machine still refuses the same violations; only the prompt's contract wording changes.",
    consumer:
      'store/strategy.go PlannerContractOn · kernel/planner_prompt.go (contract fragments) · kernel/class45_feeds_forward.go (stop-floor qualification)',
    range: 'switch (ON/OFF)',
    systemDefault: 'ON · nil = ON',
    recommended:
      "⭐ leave ON — the contract wording is the machine's ground truth. OFF exists only to prove the prompt change is byte-reversible.",
    whenToTouch:
      'Never in normal trading. OFF is a diagnostic position for A/B studies of planner wording.',
    perSession: 'No.',
  },
  {
    label: 'One setup — minimum grade',
    where: 'Strategy → Day Plan → one_setup_min_grade',
    what: 'The lowest merged-candidate grade the best level near price may carry (A+ | A | B | C). The best level is chosen grade-first, distance-second among candidates inside the reachability band; a scenario on a lower-graded level than the best is declined level_not_best.',
    trader:
      'Raising it to A means the book arms only on A/A+ levels; a day with no such level near price arms nothing (declined level_no_candidate).',
    consumer:
      'store/resolve_source.go ResolveOneSetup · kernel/one_setup.go OneSetupBestCandidate',
    range:
      'A+ / A / B / C (a non-grade falls back to B and the boot line says so)',
    systemDefault: 'B',
    recommended:
      "⭐ B [O] — the owner's default; the record has no grade cells yet to argue for A.",
    whenToTouch: 'After the record shows the grade cells — not before.',
    perSession: 'No.',
  },
  {
    label: 'Proximity filter',
    where: 'Strategy → Day Plan → slider 0.1–3.0',
    what: 'The day-trade band around price that seats levels in the card (±K × the daily-range proxy, ~±300pt × K on MNQ).',
    trader:
      'Higher = tighter card; lower = wider card. Far levels still feed the bias-tree anchors even when unseated.',
    consumer:
      'kernel/levels_score.go ScoreLevels (proximityK) · trader/auto_trader_planconfig.go proximityFilterATR → kernel.ResolveProximityK · kernel/engine_analysis.go (engine path, same resolver)',
    range:
      '0.1 – 3.0 (× daily-range proxy); a saved value outside that range is not clamped — it falls back to 1.5',
    systemDefault:
      '1.5 (the code default: kernel.ResolveProximityK falls back to ActivationWindowK 1.5 when nothing is saved)',
    recommended:
      '⭐ 1 — the value BOTH bound strategies have SAVED (day_plan.proximity_filter_atr = 1, read 2026-10-14 for a5b7662e "MNQ" and 7a853ea3 "mnq 2"); the 1.5 this card used to call the saved value is only the code fallback. Band = K × dATR (the daily-range proxy) on BOTH the bot gate and the engine path; kernel.ResolveProximityK keeps a value inside 0.1–3.0 and replaces anything outside it (0/unset included) with 1.5 — it does not clamp. dATR (release #22) = the mean high−low of the last 2 COMPLETE CME sessions, read from the NT8 daily bars; it used to average a cut-off stub of the oldest day as if complete and fell as low as half the real range (10-14 01:44 CT: 179.62 vs ~380).',
    whenToTouch:
      'If the card looks too crowded or too empty for the daily range.',
    perSession:
      'No — strategy-level: proximityFilterATR and the engine path read day_plan.proximity_filter_atr only; a session has no override for it.',
  },
  {
    label: 'Max levels',
    where: 'Strategy → Day Plan → Max levels 3–12',
    what: "Cap on rows in the card's levels table (the planner is asked to write within the resolved cap).",
    trader:
      '8 = a table the AI can copy without hallucinating; 12 = wider map but more copy risk.',
    consumer:
      'kernel/levels_score.go DefaultMaxLevels · trader/auto_trader_planner.go resolveSessionPlanCfg (a saved day_plan.max_levels > 0 wins)',
    range:
      '3 – 12 in the Studio; the code takes any saved value > 0 with no clamp',
    systemDefault:
      '8 (kernel.DefaultMaxLevels, also the Studio default when nothing is saved)',
    recommended:
      '⭐ 12 — the value BOTH bound strategies have SAVED (day_plan.max_levels = 12, read 2026-10-14 for a5b7662e "MNQ" and 7a853ea3 "mnq 2"); 8 is only the code default.',
    whenToTouch:
      'Leave alone; raise only if you find the card missing actionable levels.',
    perSession:
      'No — strategy-level: resolveSessionPlanCfg reads day_plan.max_levels only; a session has no override for it.',
  },
  {
    label: 'HTF seats (structure-first, S3)',
    where: 'Strategy → Day Plan → HTF seats 0–6',
    what: 'How many higher-timeframe (HTF) swing/zone levels the seater may promote into the ENTRY table.',
    trader:
      'UNSET (nil) = the legacy path — today\u2019s table exactly, where the promotion is nullified by its own restore sort (class NN). SAVED 0–6 = the effective promotion: the promoted HTF seats survive. 0 = no HTF seating. The structure table (D/4h/1h, bias-only) is separate and never counts against max_levels.',
    consumer:
      'kernel/levels_score.go seatHTF/seatHTFLegacy · trader/auto_trader_planner.go resolveSessionPlanCfg',
    range: '0 – 6 · unset = legacy',
    systemDefault:
      'unset → legacy path = 2 seats (kernel.LegacyHtfSeats); saved 0 = no HTF seating',
    recommended:
      '⭐ leave UNSET until the S4 measurement decides whether HTF promotion helps.',
    whenToTouch:
      'Only after the S4 structure-gate report; then set a value and watch the seated table.',
    perSession: 'No.',
  },
  {
    label: 'Max re-plans',
    where: 'Strategy → Day Plan → Max re-plans 0–4',
    what: "Re-read budget per session — a RECORDED counter (class 35): only death re-plans and owner re-reads (↻) spend it. Level-event / MSS wake reads (fast-market included), dormant flips + re-arms, the session's scheduled read, owner reset and fail-closed markers are FREE and never count. Budget exhausted = NO-TRADE terminal marker (⛔). PRESENCE-AWARE since W1 (settings truth, 2026-09-23): a BLANK box = inherit the shipped default 2; 0 = no re-plan at all, stored and honoured at the strategy level too (before W1 a strategy-level 0 could not be saved and read as 2). Each trader's boot block prints the cap it will run: '🧮 replan cap: strategy=N[O|I] · NY=… · ASIA=… · LONDON=…' ([O] saved, [I] the shipped default).",
    trader:
      'The v6-after-cap-4 confusion: the last chip IS the no-trade marker, not a real plan. And a chain can legitimately be v6 with the FULL budget left (2026-09-01 LONDON: six rows, zero spends) — the card\'s "re-reads left" is the recorded number, not version−1.',
    consumer:
      "store/resolve_source.go ResolveReplanCap (the ONE rule: session → strategy → 2; ReplanCapFor delegates) · store/strategy.go (GetReplanBudget/SpendReplan) · trader/auto_trader_planner.go (deathReplanAllowed → runDeathReplan) · trader/auto_trader_reread.go (owner re-read gate). The re-ALIGN budget (owner level edits → ⟳ Re-align plan) is folded beside this since W-KNOB-PRUNE: constant 5 per plan unless the strategy stores realign_cap (the owner's stores 10), no control.",
    range: '0 – 4 per session · blank = inherit',
    systemDefault: 'blank → 2 [I] (shipped default)',
    recommended: '⭐ 2 — one re-read after an early death, then sit out.',
    whenToTouch:
      "Raise for violent trend days where one death shouldn't end the session.",
    perSession:
      'Yes — session override wins (0 = no re-plan in that session); inherit = the strategy-level value (ResolveReplanCap). Turning an override ON starts it at the strategy value it was inheriting.',
  },
  {
    label: 'Require approval',
    where: 'Strategy → Day Plan → toggle',
    what: 'ON = entries are HELD until the owner taps Approve for this CME session-day.',
    trader: 'You are the gate: no approve, no entries, even on-plan.',
    consumer:
      'trader/entry_admission.go admitChain (approvalRequired / approvalGranted — every entry path) · api/handler_plan.go Server.approvalRequired',
    range: 'ON | OFF',
    systemDefault: 'OFF (fully automatic)',
    recommended: '⭐ OFF — SIM phase; ON is the rehearsal for live.',
    whenToTouch:
      'Turn ON to practice the live approval muscle before going live.',
    perSession: 'Yes.',
  },
  {
    label: 'Planner retry mode',
    where: 'Environment (RETRY_MODE) — not a Strategy knob',
    what: 'How the planner retries a rejected plan. repair (default): attempt 2+ sends ONLY the rejected plan + validator errors + law excerpts — a fraction of a full re-author. reauthor: the old full-prompt retry with the verbatim reject block.',
    trader:
      'repair is the 2026-08-31 speed wave default; a malformed repair falls back to one full re-author.',
    consumer:
      'kernel/planner_speed.go (ResolvePlannerRetryMode) · trader/auto_trader_planner.go (retry loop)',
    range: 'repair | reauthor',
    systemDefault: 'repair',
    recommended:
      '⭐ repair — one env line reverts to reauthor without a deploy.',
    whenToTouch:
      'Only if repair attempts start failing parse repeatedly (watch the 🧩 repair lines).',
    perSession: 'No — process-wide.',
  },
  {
    label: 'Planner stream idle',
    where: 'Environment (AI_PLAN_STREAM_IDLE_SECS) — not a Strategy knob',
    what: 'The planner reads the model over SSE. If no chunk arrives for this long, the connection is killed and the next planner attempt fires — a stalled read dies in ~30s. A live-but-slow stream is bounded only by the planner stream total deadline below (class 37) — NOT by the 600s HTTP ceiling, which killed 11 of 80 live max-reasoning reads 2026-08-30 → 09-01 while this text claimed it never would.',
    trader:
      'Split deadlines from the latency autopsy: queue/think/stall vs slow generation.',
    consumer:
      'kernel/planner_speed.go (PlannerStreamIdleSeconds) · mcp/client.go (CallWithRequestStreamRetry)',
    range: '1 – 180 seconds',
    systemDefault: '30',
    recommended:
      '⭐ 30 — reasoning streams emit chunks, so silence means stall.',
    whenToTouch: 'Raise it if the model routinely thinks >30s without a token.',
    perSession: 'No — process-wide.',
  },
  {
    label: 'Planner stream total deadline',
    where: 'Environment (AI_PLAN_TOTAL_DEADLINE_SECS) — not a Strategy knob',
    what: "The whole-call ceiling for ONE planner attempt on the SSE path. Before class 37 the planner rode the executor's 600s HTTP ceiling (AI_HTTP_TIMEOUT_SECONDS): 11 of 80 max-reasoning full reads were killed at exactly 600.0s while reasoning was still flowing (71k–140k reasoning chars received, normal ttfb). Now a live stream dies only here; the 600s ceiling still governs every non-stream path (executor loop, weekly read, Ask-Planner). Every failed ai_call line now carries class=total_deadline|idle_deadline|client_timeout|transport|http_status plus http_status and the provider request id.",
    trader:
      'Evidence (2026-08-30 17:00 → 09-01 17:30 CT): successful max full reads n=69 p50 448s · p90 552s · p95 581s · max 599.5s (right-censored at 600); the 65536-token completion cap ≈ 1000s at the median 65 tok/s. Worst-case read wall = 3 attempts × this value.',
    consumer:
      'kernel/planner_speed.go (PlannerStreamTotalSeconds) · mcp/client.go (CallWithRequestStreamDeadlines) · trader/auto_trader_planner.go (planner call) · boot lines 🚀 planner speed wave / 🛰 planner client',
    range:
      '61 – 3600 seconds (resolved value is always > the idle deadline: total ≤ idle → idle + 60)',
    systemDefault: '1200',
    recommended:
      '⭐ 1200 — 2× the observed max success; covers the completion cap at median throughput. Lower only together with a reasoning-mode ruling (fast reads finish in 30–400s).',
    whenToTouch:
      'If ai_call lines show class=total_deadline with reasoning_chars still growing → the model needs more time (raise, or rule on reasoning effort). If reads must land before the open, lower attempts or reasoning — not this knob alone.',
    perSession: 'No — process-wide.',
  },
  {
    label: 'Fast-mode shadow A/B (measurement, default OFF)',
    where: 'Environment (SHADOW_AB_ENABLED, SHADOW_AB_N) — not a Strategy knob',
    what: 'Fires ONE extra planner call at reasoning=fast on the IDENTICAL prompt, AFTER the live max-reasoning read has finished. It writes no plan, spends no re-plan budget, and never runs at the same time as a live stream. Its output goes through the FULL validator chain offline and is logged as one 🔬 line: legal or illegal, the reject reasons, output tokens and wall time, side by side with the live max call on the same prompt. It exists because of a measurement: on 67 full-author calls (2026-08-31 → 09-02) the p50 output was 23,769 tokens and the stored plan JSON was only ~920 of them. About 96% of the output is REASONING, so the reasoning mode is the only lever that can shorten the call — shrinking the plan schema cannot.',
    trader:
      "PRE-REGISTERED PROMOTION CRITERION (written before the data, 2026-09-02): fast mode is promoted to live ONLY IF, at n≥10 shadow calls, its legal-plan rate is greater than or equal to max mode's on the same prompts AND its median wall time is at most 50% of max's. Otherwise it stays shadow or is dropped. No promotion on narrative. The earlier fast-mode rejection was n=1 and pre-dates the class-38 prompt contract, so it is stale evidence, not a verdict.",
    consumer:
      'trader/rootfix_shadow_ab.go · store/shadow_ab_counter.go (recorded sample counter shadow_ab_calls_rootfix) · boot line 🔬 shadow A/B',
    range: 'SHADOW_AB_ENABLED on|off · SHADOW_AB_N 1 – 200',
    systemDefault: 'OFF · n=10',
    recommended:
      '⭐ Turn ON for one week of reads, then read the 🔬 lines against the criterion above. Each shadow call costs one extra provider call per session read.',
    whenToTouch:
      'Turn it on when you want the fast-vs-max question answered with data. Turn it off once n is reached — the harness stops firing at the target on its own.',
    perSession: 'No — process-wide.',
  },
  {
    label: 'Planner stream retry tries + backoff',
    where:
      'Environment (AI_PLAN_STREAM_TRIES, AI_PLAN_STREAM_BACKOFF) — not a Strategy knob',
    what: "How many CALLS the planner stream path makes per planner attempt when the provider cuts the connection mid-stream (class=transport: peer FIN → 'unexpected EOF', or RST), and how long it waits between them. Class 41 (2026-09-02): the schedule is exponential — 2s → 15s → 45s (last value repeats) — replacing the fixed 2s×n wait that let call 2 die 18s after call 1 on 2026-09-01 23:47 CT. AI_MAX_RETRIES still governs the NON-stream paths (executor loop, weekly read) and also counts CALLS. A transport/deadline failure that exhausts the tries re-sends the IDENTICAL prompt on the next planner attempt with NO reject block (owner ruling class 37; the pre-fix code re-authored with the transport error text as its 'validator reason').",
    trader:
      'Evidence: 4 mid-stream cuts in 81 stream calls on 2026-09-01 (4.9 per 100; 0 in 31 on 08-31), all http_status=200 with no provider request id; reproduced in-process: a peer FIN mid-body yields exactly that error string, the idle watchdog never does (it labels itself class=idle_deadline and now logs a ⏱ line when it fires).',
    consumer:
      'mcp/config.go (StreamRetryTries, StreamRetryBackoffSchedule) · mcp/client.go (CallWithRequestStreamRetryDeadlines, watchdog ⏱ line) · trader/auto_trader_planner.go (resend-identical) · boot line 🔁 planner stream policy',
    range:
      'tries 1 – 6 · backoff: comma list of Go durations (e.g. "2s,15s,45s")',
    systemDefault: '3 tries · 2s,15s,45s',
    recommended:
      '⭐ Defaults. Worst case added wall per planner attempt = 17s (2s + 15s) before the attempt is consumed; a 4th try adds 45s more.',
    whenToTouch:
      'If 🔁 lines show call 3 still dying on the same edge flap, raise tries to 4 (adds the 45s wait). Never lower below 2 — a single cut would consume a planner attempt outright.',
    perSession: 'No — process-wide.',
  },
  {
    label: '402 banner clear (restart-safe)',
    where: 'Environment (AI402_CLEAR_ALL) — not a Strategy knob',
    what: 'How the "AI CREDIT EXHAUSTED" P0 banner clears. ON (default, release #17): every successful DeepSeek call — the decision path, the in-position watch observer, and the planner read — acknowledges EVERY open ai-payment banner for this trader, so the banner clears even after a restart (the old in-memory outage latch is gone on a fresh process). The planner path also RAISES the banner on its own 402s (it was silent before). OFF: the pre-release-#17 behaviour — only the decision path clears, and only by the in-memory event id, so a banner raised before a restart can never clear itself.',
    trader:
      'Mentor mode skips the decision path, so the planner read is the call that must clear the banner in mentor mode.',
    consumer:
      'trader/auto_trader_ai402.go (onAISuccess, on402Failure, ai402ClearAllOn) · store/alert.go (AckAllByKind) · trader/auto_trader_planner.go (planner call site) · trader/auto_trader_watcher.go (watch call site) · log line ✅ AI PAYMENT RECOVERED',
    range: 'on | off',
    systemDefault: 'ON',
    recommended:
      '⭐ ON — a stuck P0 banner is worse than a slightly eager ack.',
    whenToTouch:
      'Turn OFF only if a kill-switch is needed while the call-site wiring is audited.',
    perSession: 'No — process-wide.',
  },
  {
    label: 'Min scenario quality',
    where: 'Strategy → Day Plan → A/B/C',
    what: 'Lowest grade the planner may write. Judged against the min_scenario_quality floor that MinScenarioQualityFor (store/strategy.go) resolves per session; at the floor it resolves to today nothing is refused for quality, and raising it makes the arm-time gate refuse a below-floor scenario.',
    trader: 'C = full palette; B/A = the planner filters its own plays.',
    consumer:
      'kernel/engine_position.go validateDecision → kernel/transition.go MinScenarioQualityVerdict (decision path) · trader/armed_executor.go (arm path: DayPlanConfig.MinScenarioQualityFor(plan.Session)) · store/strategy.go DayPlanConfig.MinScenarioQualityFor',
    range: 'A | B | C',
    systemDefault: 'C',
    recommended: "⭐ C — grade is advisory; don't hide plays with it.",
    whenToTouch: 'Only if the card gets cluttered with junk scenarios.',
    perSession:
      'Yes — session override wins; inherit (blank) = the strategy-level row above.',
  },
  {
    label: 'Write-time feasibility (W-WRITE-TIME-FEASIBILITY)',
    where:
      'No Studio control — stored setting day_plan.write_time_feasibility (API/config field)',
    what: 'Before a plan is written, the write site runs the SAME gate-at-arm predicates the executor runs (min-SL 1.5×ATR5m, arm R:R floor, structural-geometry) on every enabled arm — plus the executor\'s stop-side placement guard for stop-entry arms (reclaim): a trigger already through the read-time price cancels at placement, so it is refused at write instead. Attempts 1–2: a scenario that would be refused is sent back as a repair hint naming the refusal, the numbers, and the fix ("widen the stop past the min-SL floor / raise the arm R:R / pick a mapped level with an id" — or for a stop entry: "author the trigger ahead of price, or author a reject/limit at the level"). The last attempt: the unarmable scenarios are written with arm.enabled=false + arm_disabled_reason (stop_side_wrong for wrong-side stop entries), so the plan ships instead of silently never arming. The session-risk band is NOT judged at write (it is time-based).',
    trader:
      "ON = the planner learns why its arm will not trade and can fix it; the last attempt never fail-closes for this — it writes the arm disabled. OFF = today's behaviour: an arm-feasibility WARN is logged and the plan is written as authored (the gate-at-arm chain still refuses at arm time).",
    consumer:
      'trader/auto_trader_planner.go (write-time feasibility check → armGateVerdictFor / composeArmStop geometry; the write site follows the executor to ArmGeometryVerdict at the geometry-refusal merge) · store.DayPlanConfig.WriteTimeFeasibilityEnabled',
    range: 'ON | OFF',
    systemDefault: 'ON (nil/unset = ON)',
    recommended:
      "⭐ ON. The verdicts reuse the executor's own functions, so the write site and the arm site cannot disagree.",
    whenToTouch:
      'OFF only to restore the old WARN-and-write behaviour while triaging; the boot line 🎛 entry law shows the resolved write_feas=on/off.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Entry policy default (W-EXEC-TRUTH W3)',
    where: 'Strategy → Day Plan → entry_policy_default (API/config field)',
    what: "The entry policy STAMPED at parse on every arm of a NEWLY authored plan (a stored plan is never re-stamped; an absent policy executes as legacy, byte-identical). market_in_zone: a LIMIT at the far edge of the planner's economics.entry_zone (buy → zone high, sell → zone low) — every condition is armable, a non-touch confirm chains (wait_confirm), the zone is judged at write. planned_order: the resting order at the exact entry, stamped only on reject / fvg_entry / sweep_reclaim leg 1 (elsewhere the arm stays legacy — an illegal policy is never stamped). legacy: stamp nothing — the planner prompt and the validator are the pre-W3 text byte-for-byte.",
    trader:
      'market_in_zone = "enter around the price": the plan names a small zone and the machine fills anywhere in it, never beyond it; acceptance, hold, reclaim and immediate waterfalls finally have a route under strict. The planner prompt carries ONE "ENTRY POLICY" sentence that follows this knob.',
    consumer:
      'store/resolve_source.go ResolveEntryPolicyDefault · trader/entry_policy_authoring.go plannerAuthoringOpts → kernel.ParsePlanDocForAuthoring (StampEntryPolicyDefault before ValidatePlanDocWithCaps) · kernel/planner_prompt.go PlannerInput.EntryPolicyDefault · trader/rootfix_shadow_ab.go (shadow parity)',
    range:
      'market_in_zone | planned_order | legacy (anything else → market_in_zone, named in the source)',
    systemDefault: 'market_in_zone (R4)',
    recommended:
      '⭐ market_in_zone — the W3 ruling (R4); legacy only to reproduce pre-W3 plans for a comparison.',
    whenToTouch:
      'legacy to switch the whole policy off for new plans; the 🎛 entry law boot line prints the resolved value and its origin letter.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Zone max width (W3)',
    where: 'Strategy → Day Plan → zone_max_pts (API/config field)',
    what: 'The widest economics.entry_zone (points) a market_in_zone arm may carry, judged at write (not gated by write_time_feasibility). Wider → hinted ("S# entry zone: zone_too_wide — …"), then written with the arm disabled (arm_disabled_reason zone_too_wide) on the last attempt. The prompt states the resolved value.',
    trader:
      'Caps how far "around the price" may reach: with a 10-pt zone the worst fill is at most 10 pts from the best one, and the gates judge the worst (R:R at the far edge, stop distance at the near edge).',
    consumer:
      'store/resolve_source.go ResolveZoneMaxPts · trader/write_time_feasibility.go writeTimeZoneVerdicts → kernel.ArmZoneVerdict · kernel PlannerInput.ZoneMaxPts (prompt)',
    range: '> 0 points (≤ 0 or unset → 10)',
    systemDefault: '10',
    recommended:
      '⭐ 10 — the R2 ruling; a wider zone buys fills with worse worst-case R:R.',
    whenToTouch:
      'Only after the receipts show zones refused for width that would have traded well.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Zone rest cap (W3)',
    where: 'Strategy → Day Plan → zone_rest_max_min (API/config field)',
    what: 'A market_in_zone limit that has rested longer than this many minutes (measured from its placement) is cancelled "zone rest expired" by the executor — a zone the market walked away from is not left working forever. The rest cap is no longer the only exit: a new plan version or overlay that moves the zone off the resting limit cancels it sooner ("zone moved by vN"). The same number is the TRIGGER age bound (fix F-1): a confirm that went MET longer ago than this, with no newer reaction inside the window, no longer authorizes an arm ("confirm MET but STALE").',
    trader:
      'The limit sits at the far edge of the zone; if price never comes back within the cap the order goes away rather than filling hours later in a different market.',
    consumer:
      'store/resolve_source.go ResolveZoneRestMaxMin · trader/armed_executor.go (the market_in_zone rest cap) · 🎛 entry law boot line',
    range: '> 0 minutes (≤ 0 or unset → 30)',
    systemDefault: '30',
    recommended:
      '⭐ 30 — the W3 default; there is no fill evidence yet to argue another number.',
    whenToTouch: 'After the receipts (rest duration per fill) exist.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Zone placement reach (PLANNER B1)',
    where: 'Strategy → Day Plan → zone_place_within_pts (API/config field)',
    what: 'The distance bound (points) that decides whether an armed market_in_zone arm may place: a row whose zone is farther than this from the eval price stays armed-unplaced (no rest clock, nothing on the wire) and places on a later pass once price comes within the bound; inside and short_of_zone verdicts are unchanged. With the bound ON, a rest-cap expiry (zone_rest_max_min) no longer dismantles the arm — the resting order is cancelled on request (cancel_pending, signal kept) and only once the broker book confirms it does the row return to armed-unplaced (re-placeable) so it can try again when price is near; a failed cancel send is retried and a fill during the wait attributes to the row. 0 turns the whole reach contract OFF and restores the legacy behaviour byte-for-byte (a far arm places at once and an expiry dismantles the arm).',
    trader:
      'The bound is the existing armed placement band (25 pts on MNQ, the same distance a legacy limit waits for). A zone the market walked away from is not left resting 148 points away.',
    consumer:
      'store/resolve_source.go ResolveZonePlaceWithinPts · trader/zone_placement.go (placeZoneRow beyond-proximity gate + zoneRestCap reset) · 🎛 entry law boot line',
    range: '0 = OFF (legacy). unset → 25 (the armed placement band).',
    systemDefault: '25 (ON)',
    recommended:
      '⭐ 25 — the same bound legacy limit placement already uses; no evidence yet to argue another number.',
    whenToTouch: 'After the receipts (placements vs distance per fill) exist.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Armable hold floor (W3)',
    where: 'Strategy → Day Plan → min_hold_min (API/config field)',
    what: 'The floor (minutes) on the RESOLVED hold of an ARMED market_in_zone time_hold scenario (acceptance / hold): the stored confirm.hold_min, else the ACCEPT_HOLD_MIN authoring default. Below it the plan is refused at write with its own law ("armable hold floor: …") — an unarmed scenario or a legacy arm is never judged. New plans only.',
    trader:
      'Once acceptance and hold can arm, a 1–2 minute hold would place a limit on noise; the floor keeps the armed hold honest.',
    consumer:
      'store/resolve_source.go ResolveMinHoldMin · kernel/entry_policy.go ValidateArmableHoldFloor via kernel.ParsePlanDocForAuthoring · kernel PlannerInput.MinHoldMin (prompt)',
    range: '> 0 minutes (≤ 0 or unset → 3)',
    systemDefault: '3',
    recommended: '⭐ 3 — the CTO ruling of 2026-09-23.',
    whenToTouch: 'Only with evidence that shorter armed holds fill well.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Geometry reference levels (W-GEOMETRY-REFUSAL)',
    where: 'Strategy → Day Plan → geometry_reference_levels (API/config field)',
    what: "Since the 2026-09-12 structural-stop wave, a reject play at a session reference level (ONH/ONL and the other anchor kinds) was REFUSED at arm time 100% of the time: the identity map showed id=NULL for a reference whose source window was still developing (no formation close), the planner wrote level_id null as instructed, and the executor's frozen-zone match failed with no_provenance / scenario_level_id_missing. ON (default — owner ruling 2026-09-18 'both fix now') assigns a STABLE id to reference-anchor levels whose formation close is unknown, and treats an empty zone-source tf as a wildcard (VWAP-family sources). The arm gate now logs one ⚔️ arm REFUSED WARN line per (geometry key, reason) change instead of a silent INFO-only refusal. OFF = today's behaviour byte-identical.",
    trader:
      'ON = reject plays authored at ONH/ONL/VWAP-family reference levels can arm: a reference LINE (null-width zone in the frozen map) is admitted as a zero-width band at the line, and the stop composes from the structural stop rule (line − buffer); a non-empty mismatched source tf still refuses, and two matching zones refuse as ambiguous — never a pick. The counter labelled "93/95 refusals since 09-13" is measured on the owner\'s DB 2026-09-18. Stored plans with level_id null keep the legacy WARN-only resolution.',
    consumer:
      'trader/structural_geometry.go ArmGeometryVerdict (tf wildcard) · kernel/scenario_level_identity.go EnsureReferenceLevelIDs (stable ids) · trader/armed_executor.go geometry WARN · store.DayPlanConfig.GeometryRefIDsEnabled',
    range: 'ON | OFF',
    systemDefault: 'ON (owner ruling; explicit false = legacy)',
    recommended:
      '⭐ ON — the default; OFF only to reproduce the pre-fix behaviour.',
    whenToTouch:
      'Turn OFF to compare against the pre-fix refusals; turn back ON to trade reference levels.',
    perSession: 'No.',
  },
  {
    label:
      'Flip re-read (W-FLIP-REREAD, immediate since W-FLIP-REREAD-IMMEDIATE)',
    where: 'Strategy → Day Plan → flip_reread toggle',
    what: "When the plan's flip condition fires, the plan ALWAYS goes dormant first (wick-noise protection — unchanged). ON adds ONE free planner re-read in the flipped direction (trigger structure_flip, class-35 free) that fires IMMEDIATELY — a flip read is a reaction to a machine-confirmed event (two 5m closes beyond the flip line with the ATR buffer), so it is exempt from the class-47 30m cooldown and the wake_min_interval_min throttle that pace ordinary level wakes. What still gates it: preflight (fresh bars), the class-47 cutoff (no read within 25 min of the session flat), one planner stream at a time (deferred, retried next cycle), one successful read per fired flip. OFF = today's behaviour: the plan sleeps and the flipped bias is never authored.",
    trader:
      'ON = the flipped bias can actually materialize, and on the same cycle the flip fires — an earlier level wake never delays it (the log says "🗓️ structure_flip read … — immediate" when a throttle would otherwise have held it). The write site REQUIRES the flipped bias: the model authors it or the read writes nothing and the dormant plan stands (a same-bias plan is rejected, never written). One SUCCESSFUL re-read per fired flip. A read REFUSED before launch (stale bars, cutoff, another planner stream open) is retried on the very next cycle; a read that LAUNCHED and wrote nothing (up to 3 model calls) backs off from its OWN launch for wake_min_interval_min (default 30 min) before retrying — no hard cap while the row stays dormant, bounded by the session read window. If price closes back first, the old plan re-arms as before and the re-read is skipped.',
    consumer:
      'trader/auto_trader_planner.go maybeRereadAfterFlip · store.DayPlanConfig.FlipRereadEnabled',
    range: 'ON | OFF',
    systemDefault: 'OFF (legacy dormant)',
    recommended:
      '⭐ OFF until you have watched one flip the old way; then ON and compare.',
    whenToTouch:
      'Turn ON when you want a fired flip to re-read rather than sleep.',
    perSession: 'No.',
  },
  {
    label: 'Death re-read (W-DEATH-REREAD, owner ruling 2026-09-18 12:3x CT)',
    where: 'Strategy → Day Plan → death_reread toggle',
    what: "When the plan's DEATH condition fires, the plan ALWAYS goes dormant first (wick-noise protection — unchanged). ON (the default, nil=ON) adds ONE BUDGETED planner re-read that authors a FRESH plan, bias free, with the death evidence in the read prompt (the dead version, its kill line with the price at death, the direction of the break). Unlike the flip read (free), a death re-read SPENDS one class-35 replan unit — a death is the planner being wrong, and an unbounded loop of dead plans on a trend day must stop; at budget exhausted the plan stays dormant with one WARN naming the budget. The fresh version supersedes the dormant one (superseded:death) and is protected by the same 30-min flip hold anchor plus a 10-minute birth wick (its first death check runs only after 2 full 5m closes post-birth, so the same line's noise cannot kill it). What still gates it: preflight, the class-47 cutoff, one planner stream at a time, one successful read per fired death. OFF = today's behaviour: the plan sleeps until price closes back — and if price never does, the session sits out.",
    trader:
      "ON = a dead plan re-reads once (budgeted) instead of sitting the session out when price runs away from its line. A read REFUSED before launch is retried on the very next cycle while the row stays dormant; a read that LAUNCHED and wrote nothing backs off from its OWN launch for death_reread_retry_min before retrying. death_reread_retry_min defaults to wake_min_interval_min (today's behaviour: the same 30-minute hold) and replaces ONLY this hold — 0 means retry on the very next cycle; wake_min_interval_min still paces ordinary level wakes as before. If price closes back first, the old plan re-arms as before and the re-read is skipped. The counter death_reread:<trader>:<date>:<session> records each landed re-read.",
    consumer:
      'trader/death_reread.go maybeRereadAfterDeath + deathRereadHoldMinutes · store.DayPlanConfig.DeathRereadEnabled · store.DayPlanConfig.DeathRereadRetryMinutes',
    range:
      'death_reread: ON | OFF · death_reread_retry_min: minutes (unset = wake_min_interval_min)',
    systemDefault:
      "death_reread: ON (unset; nil=ON per the owner ruling) · death_reread_retry_min: unset = today's value",
    recommended:
      '⭐ ON — the default; OFF only to reproduce the pre-fix dormant-only behaviour.',
    whenToTouch:
      'Turn OFF only for a side-by-side study of a dead plan sitting out the session.',
    perSession: 'No.',
  },
  {
    label: 'Planner fresh tape on born-dead retry (PLANNER A6)',
    where:
      'No Studio control — stored setting day_plan.planner_fresh_tape (API/config field)',
    what: "When a planner attempt is refused born-dead or flip-met (the market moved during the 9–15 minute AI read and the validator correctly refused), attempt N+1's prompt carries the COMPLETED bars between the read clock and the refusal — at most the last 30 completed 1m closes and the last 6 completed 5m closes, never the forming bar — plus the breached condition named verbatim, so the re-author reads the tape that exists now instead of retrying blind against the stale read. The born-dead check itself is unchanged: a plan whose lines are already crossed at publication is still refused. ON is the default (nil=ON).",
    trader:
      "ON = a born-dead / flip-met retry re-sights the model on the fresh tape instead of burning attempts 2/3 on the identical stale read. The block is appended to BOTH the repair prompt and the full re-author prompt, and the refusal line logs the read→publish latency. OFF = today's behaviour byte-identical (blind retry).",
    consumer:
      'kernel.PlannerFreshTape · trader/auto_trader_planner.go retry loop · store.DayPlanConfig.PlannerFreshTapeEnabled',
    range: 'ON | OFF',
    systemDefault: 'ON (unset; nil=ON)',
    recommended:
      '⭐ ON — the default; OFF only to reproduce the pre-fix blind retry.',
    whenToTouch:
      'Turn OFF only for a side-by-side study of a blind born-dead retry.',
    perSession: 'No.',
  },
  {
    label: 'Round-trip cost (structural stop)',
    where:
      'No Studio control — stored setting day_plan.structural_stop.round_trip_cost_points (API/config field)',
    what: "The point cost of a complete round trip (entry + exit friction) assumed when the structural stop is composed. Unset → 2 points (the owner's baseline research assumption); a stored non-negative value overrides it. The resolved cost is part of the structural-stop policy the arm geometry reads.",
    trader:
      '2 points is the friction assumption baked into the structural stop so the machine does not arm a stop whose net edge is eaten by costs.',
    consumer:
      'store/structural_geometry.go ResolveStructuralStop (CostPoints, default 2) · trader/structural_geometry.go ComposeLevelFadeGeometry',
    range: 'points ≥ 0 (unset → 2)',
    systemDefault: '2',
    recommended:
      "⭐ 2 — the owner's baseline assumption; change only with a measured round-trip cost.",
    whenToTouch: 'Only after measuring the actual per-trade friction.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Fade-or-wide k (W2 label)',
    where:
      'No Studio control — stored setting day_plan.fade_or_wide_k (API/config field)',
    what: "W2 LABEL knob: sets exclusion (a)'s k — it changes what is RECORDED (which level is labelled fade vs wide), never what is armed. Zero or unset → the C5 default 1.28 (p80/median, n=13).",
    trader:
      'A labelling constant only: it names the fade/wide split in the records; the arm gate never reads it to allow or refuse an entry.',
    consumer:
      'trader/fade_facts.go fadeORWideKResolved (fadeORWideKDefault = 1.28)',
    range: 'k > 0 (0 or unset → 1.28)',
    systemDefault: '1.28 (C5 p80/median n=13)',
    recommended:
      '⭐ leave unset — the C5 default; a stored value only changes the recorded label split.',
    whenToTouch: 'Only to re-label the fade/wide split in the records.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Red-news hard-block currencies (W-T1-CURRENCIES)',
    where: 'Strategy → Day Plan → t1_currencies text field (comma-separated)',
    what: "Which currencies' T1 (red) calendar events open the HARD ±15m no-trade window. Default USD: only USD red events hard-block; a red event in any other currency (a BOJ rate decision, a BoE vote) is shown as an advisory line — on the plan card, in the plan's no_trade list and in the planner prompt — and blocks nothing. Set ALL to restore the old behaviour where every red event in the session's currency filter hard-blocked. Case-insensitive; blanks are ignored; a red event with NO currency still hard-blocks (fail closed) and is named once a day in the log.",
    trader:
      'Born 2026-09-17 evening: the BOJ rate decision (JPY, 21:54 CT) put the MNQ bot into a hard blackout. You trade a US index; a JPY or GBP print is worth knowing about, not worth sitting out. The arm gate, the plan write (band + lines) and the fade facts all read ONE resolved set (store.DayPlanConfig.T1CurrenciesFor) so the card can never show a blackout the gate does not enforce. Boot line: "🔴 t1_blackout=USD(default)" / "USD,EUR(saved)" / "ALL(saved)".',
    consumer:
      'kernel/calendar_blackout.go SplitT1 · trader/auto_trader_calendar.go t1WindowsFor (arm gate) · trader/auto_trader_planner.go plannerT1Lines (plan write) · trader/fade_facts.go fadeFactsAt · kernel/planner_prompt.go Calendar section',
    range:
      'comma-separated ISO currency codes (USD, EUR, GBP, JPY, CNY) or ALL',
    systemDefault: 'USD (absent/empty = USD)',
    recommended:
      '⭐ USD for an MNQ/ES/NQ trader — the CME index products react to US prints; leave the rest advisory.',
    whenToTouch:
      'Add a currency only if you have watched its red prints move MNQ enough to want the machine to refuse entries around them; ALL only to reproduce the pre-2026-09-18 behaviour.',
    perSession:
      'No — one list for every session (the session currency filter still decides which events are shown at all).',
  },
  {
    label: 'Wake on level events (1 switch)',
    where:
      'Strategy → Day Plan → Planner wake-ups → Wake on level events (wake_on_level_events)',
    what: 'The level-event wake that re-reads the planner mid-session (the W6 wake wave), now ONE switch (W-KNOB-PRUNE, 2026-09-18). ON = HTF 1h/4h S/D zones the plan never saw and invalidation of a level it DID seat wake the planner — the two classes Rounds 23/24 kept; the 15m reversal-zone/FVG and iFVG classes ride along at their shipped-ON default. HTF order blocks stay OFF unless a legacy stored wake_on_htf_ob=true exists (both bound strategies — honoured and logged at load). Wakes are paced by the folded 30-minute interval and the class-47 cadence cutoffs.',
    trader:
      'ON = the plan reacts to structure as it forms; wakes are advisory refreshes that can never dark a session. OFF = only deaths, MSS and flips re-read. A strategy that stored the old five toggles is mapped by the engine: any of them ON → this switch ON; all five false → OFF.',
    consumer:
      'trader/auto_trader_wake_levels.go:103 (collectLevelWakeCandidates ← store.DayPlanConfig.WakeOnLevelEventsEnabled) · maybeRunSessionReadsAt',
    range: 'ON | OFF',
    systemDefault: 'ON (unset)',
    recommended: '⭐ leave ON — deaths still re-plan; wakes only refine.',
    whenToTouch:
      'Turn OFF only for a deliberately quiet, read-once session study.',
    perSession: 'No.',
  },
  {
    label: 'Picture HTF (two-picture mode)',
    where:
      'Strategy → Day Plan → Picture HTF block → "Include Picture HTF setups" (switch; greyed out while Enable Day Plan is off)',
    what: "The owner's two-picture method as a DETERMINISTIC mode (2026-09-20): a 4H body pivot → the H1 close breaks it by at least one tick → the next 5m interval (entry window, default 360s) searches a strict 5m swing for the stop and the nearest opposing 4H zone for the target. R:R below the configured minimum refuses — the nearer zone is never skipped. The AI is commentary only; timing is the rule, not the model. Since W-EXEC-TRUTH W0b every Picture entry passes the same entry rules as the AI and armed orders (see Status → One set of entry rules), trades only the trader's own instrument, and runs only while the trader is running and the Day Plan is on; since W5 it is a Day Plan scenario source: each opportunity becomes a recorded plan scenario (📷 P1…) that the plan's armed executor places as a market_in_zone limit inside its eligibility window — under every plan mode, strict included (📷 plan_gate= and the plan card show the route), exempt from one_setup, never a widened stop or a swapped target, and never placed after its window, by another run, after Stop or with the Day Plan off.",
    trader:
      'OFF by default; enabling it gates on the AddOn proving build ≥ 2026-09-20-p1 (final+emitted_at bar markers, rejection reasons) — below that the evaluator logs "mode unavailable" and never submits. Since W5 Picture never sends an order of its own (its market-entry send is retired): each opportunity is recorded as a Day Plan scenario, and the armed executor places it as a 1-contract SIM market_in_zone LIMIT with its protective bracket, through the same entry latch and one-live-entry guards as every armed order.',
    consumer:
      'store/strategy.go PictureHtfResolved · trader/picture_htf_evaluator.go (evaluation + pictureHtfCapabilityProven) · trader/picture_htf_live.go (live-bar fan-out) · trader/picture_plan_source.go (the Day Plan hand-off — the submit seam) · trader/armed_executor.go + trader/picture_scenario_exec.go + trader/zone_placement.go (placement as a market_in_zone limit) · store/picture_htf.go (opportunity ledger)',
    range:
      "switch + tick size / pivot window / swing lookback / entry window (s) / freshness (s) / min R:R (the STRICTER of this and risk control's minimum R:R applies; a value below it never loosens it; no strategy floor at all refuses)",
    systemDefault: 'OFF · defaults 0.25 / 120 / 24 / 360s / 30s / inherit',
    recommended:
      '⭐ run it on SIM and read the Picture HTF panel on the dashboard — the ledger shows intended vs broker answer side by side; the mode earns real-money trust only from recorded fills.',
    whenToTouch:
      'When activating the two-picture setup in SIM, or tightening the freshness/window to the tape.',
    perSession: 'No.',
  },
  {
    label: 'Day plan master switch',
    where: 'Strategy → Day Plan → "Enable Day Plan" switch (plan_enabled)',
    what: 'The master switch for the day-plan engine on this strategy. Default false = off.',
    trader: 'OFF = no planner authoring, no session machine for this strategy.',
    consumer: 'store.DayPlanConfig.PlanEnabled (store/strategy.go)',
    range: 'true | false',
    systemDefault: 'false (off)',
    recommended: '⭐ ON once the day-plan flow is configured — OFF until then.',
    whenToTouch: 'Turn ON to enable day-plan authoring for this strategy.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Planner model',
    where:
      'Strategy → Day Plan → "Planner model" text field (planner_model; blank = inherit primary)',
    what: 'The reasoner binding from the multi-key registry used for planner reads; empty falls back to the strategy primary model (RECON #9).',
    trader: 'Leave empty unless a dedicated planner reasoner is provisioned.',
    consumer: 'store.DayPlanConfig.PlannerModel (store/strategy.go)',
    range: 'any registered model id · empty = strategy primary',
    systemDefault: 'empty → strategy primary model (RECON #9)',
    recommended: '⭐ empty — the primary model is the shipped default.',
    whenToTouch:
      'Set it only when a dedicated planner reasoner is provisioned.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Condition status (live/shadow, 0C)',
    where: 'Strategy → Day Plan → condition_status map (API/config field)',
    what: 'Per-condition live|shadow map, resolved session override → base → LIVE → SHADOW env → defaults (fvg_entry and breakout_retest default SHADOW per owner ruling). The ARM SEAM is the only enforcement point; authoring/validation/E8 scoring stay untouched.',
    trader: 'SHADOW conditions author and validate but never arm.',
    consumer: 'store.DayPlanConfig.ConditionStatus (store/strategy.go)',
    range: 'live | shadow per condition',
    systemDefault:
      'LIVE except fvg_entry and breakout_retest (SHADOW, owner ruling)',
    recommended: '⭐ shipped defaults — no evidence yet argues another map.',
    whenToTouch: 'Only to shadow-demote a condition whose arms misfire.',
    perSession: 'Yes — session override wins; nil inherits the base map.',
  },
  {
    label: 'Session condition status override (0C)',
    where:
      'API/config field day_plan.sessions[].condition_status (no Studio control)',
    what: 'Per-session live|shadow override of the strategy-level condition_status map; nil inherits the strategy-level map.',
    trader: 'Use it to shadow a condition in one session only.',
    consumer:
      'store.DayPlanSessionOverride.ConditionStatus (store/strategy.go)',
    range: 'live | shadow per condition · nil = inherit',
    systemDefault: 'nil (inherit the base map)',
    recommended: '⭐ inherit — per-session overrides only with evidence.',
    whenToTouch: 'Only when one session needs a stricter map than the rest.',
    perSession: 'Yes — this IS the per-session field.',
  },
  {
    label: 'Structural stop buffer (points)',
    where:
      'Strategy → Day Plan → structural_stop.buffer_points (API/config field)',
    what: 'Research buffer beyond the frozen zone, composed into the stop (line − buffer). MNQ unset = 4.5 pt: the C5 calibration (6,181 in-sample HELD first touches, p95 far-edge penetration 4.3675648248 pt rounded OUTWARD to the MNQ tick — a chosen tolerance, not a validated universal buffer). A saved value overrides with owner_override[I]; a missing/uncalibrated saved value refuses.',
    trader: 'The stop composes at the frozen zone line minus this buffer.',
    consumer:
      'store.ResolveStructuralStop / StructuralBufferMNQDefault (store/structural_geometry.go)',
    range: 'points (> 0) · unset = C5 4.5',
    systemDefault: '4.5 (C5-H12-IS-6181-p95-20260912, MNQ only)',
    recommended: '⭐ leave unset — the C5 calibration is the shipped default.',
    whenToTouch:
      'Only to record an owner-researched buffer as an explicit override.',
    perSession: 'No — strategy-level.',
  },
  // ── Release #25 — the loss-fix switches (STRICT-12345 report §7, F-2/F-3/
  // F-5/F-7). Owner order "do all now": each is a switch that ships OFF
  // (COMMON-25 knob rule case b — planner numbers the owner has not given).
  // The "on 649–654" lines are fixtures from the report's numbers, never the
  // live DB.
  {
    label: 'Room gate (F-2)',
    where:
      'Strategy → Day Plan → Loss-fix switches → Room gate (planner_room_gate) + minimum R (planner_room_min_r)',
    what: 'At plan WRITE, every enabled arm is measured: r_to_obstacle = |first obstacle − arm entry| ÷ |arm entry − arm stop| — the economics the planner wrote, recomputed by the machine (the first obstacle is the nearest seated level between entry and target, already checked at write). Below planner_room_min_r: refuse → the arm is refused at write (attempts 1–2 get a repair hint, the last attempt writes it disabled with reason room_gate); tp1_at_obstacle → the arm target (leg 1 on a split) is moved to the first obstacle, and then the normal R:R floor judges the new geometry (with min_risk_reward_ratio 2 such an arm is then refused by the R:R gate, reason rr). An arm whose first obstacle is unknown is refused room_gate in both modes. Logged "🧱 room gate", counted room_gate / room_gate_tp1.',
    trader:
      'Every strict-planner loser on 10-14 stalled at its first obstacle: r_to_obstacle was 0.50 (#649), 0.60 (#650), 0.55 (#652), 0.25 (#653), 0.42 (#654) while the targets sat 2.04–3.82R away. On those five, refuse at 1.0 refuses ALL FIVE at write (no trade; the four closed ones lost −$428). tp1_at_obstacle gives an arm R:R of 0.25–0.60, below the 2.0 floor, so the R:R gate refuses them too (the report: armed anyway with the target at the nearest level they would have made +$35.5 on the four closed and +$79.5 on #654).',
    consumer:
      'trader/planner_room_gate.go roomGateAtWrite (run at the plan write site, trader/auto_trader_planner.go, before the write-time feasibility verdicts; the hint rides trader/write_time_feasibility.go) · store/planner_knobs.go ResolvePlannerRoomGate / ResolvePlannerRoomMinR · 🧱 per-trader boot line',
    range:
      'planner_room_gate: off | refuse | tp1_at_obstacle · planner_room_min_r: 0.1–10 R (read only when the gate is on)',
    systemDefault: 'off · min R 1.0 (unset)',
    recommended:
      '⭐ OFF until the owner rules — the 1.0 is the report’s suggestion, not an owner number. refuse is the honest first step; tp1_at_obstacle mostly ends in an R:R refusal while min_risk_reward_ratio is 2.',
    whenToTouch:
      'When you want the planner to stop arming trades whose first obstacle is closer than the risk.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Exit on thesis death (F-3)',
    where:
      'Strategy → Day Plan → Loss-fix switches → Exit on thesis death (planner_exit_on_thesis_death)',
    what: 'On the 60-second monitor beat, for each open PLANNER position (an armed entry linked to a plan): invalidation_only → close at market once a completed 5m close AFTER the fill is beyond the cited scenario’s own invalidation sentence ("5m close above/below <price>", "2x5m close …" — the grammar the write site enforces); all → also when the plan version that authored the trade goes dormant (dormant, superseded:flip, superseded:death) or a newer version of the same plan chain carries the opposite bias. The close is the whole position at market on this trader’s own bound account (the same close path EOD-flat uses) and the bracket is cancelled. Never a mentor position, never a mentor-mode trader, never a manual (origin=manual) position. Logged "🪦 thesis-death EXIT … reason=invalidation|plan_dormant|opposite_plan", counted thesis_death_exit (+ thesis_death_exit_<reason>).',
    trader:
      'On 10-14 every loser was held past its own thesis death. invalidation_only: #649 −$60, #650 −$76.5 (its invalidation is its stop), #652 −$19.5, #653 −$82 = −$238 instead of −$428. all: #649 +$1 (plan dormant 21:00:01), #650 −$10.5 (newer v4 short at 00:23:05), #652 −$19.5 (the invalidation close at 03:00 comes before dormant 03:22), #653 −$82 (the invalidation close at 09:00 comes before dormant 09:19:31) = −$110.5 [B — derived from the report’s per-trade exits and timestamps]. #654 was still open.',
    consumer:
      'trader/thesis_death_exit.go maybeExitOnThesisDeath (called from trader/auto_trader_risk.go monitorTick, beside auto-breakeven) · kernel.EvaluateAuthoredInvalidationAt · store PlanStore lifecycle / latest version · flattenPosition · store/planner_knobs.go ResolvePlannerExitOnThesisDeath · 🧱 per-trader boot line',
    range: 'off | invalidation_only | all',
    systemDefault: 'off (unset)',
    recommended:
      '⭐ OFF until the owner rules — it changes the "entries only" rule for planner trades. invalidation_only is the narrower step.',
    whenToTouch:
      'When you want a planner trade closed as soon as its own plan says the idea is dead, instead of waiting for the stop.',
    perSession: 'No — strategy-level.',
  },
  {
    label:
      'Enforce no-chase on arms (F-5) — the |live price − level| pullback metric',
    where:
      'Strategy → Day Plan → Loss-fix switches → Enforce no-chase (planner_no_chase_enforce)',
    what: 'The arm path’s no-chase leg (measured since 2026-09-02, WARN-first) becomes a refusal: a WOULD_REFUSE verdict refuses the placement and is counted no_chase_refused. OFF = today: it only warns and the entry proceeds. CAVEAT (rescan N4): the arm metric is |live price − cited level| — for a resting limit that is pullback distance, not a chase.',
    trader:
      'On 10-14 it would NOT have stopped #653: it printed WOULD_REFUSE at 08:54/08:55 (1.05 and 1.17 × ATR) but would have PASSED at 08:56 (≈0.55 × ATR), just before the dump filled the limit. The report records no WOULD_REFUSE for #649/#650/#652/#654. The momentum-into-zone ceiling below is the switch aimed at #650/#653.',
    consumer:
      'trader/no_chase.go EvaluateNoChase · trader/entry_gate.go EntryGate (arm path, entryGateForArm sets NoChaseEnforce) · store/planner_knobs.go ResolvePlannerNoChaseEnforce · 🧱 per-trader boot line',
    range: 'ON | OFF',
    systemDefault: 'OFF (unset)',
    recommended:
      '⭐ OFF — enforce only after the metric is redefined as momentum into the zone (rescan N4).',
    whenToTouch: 'Only after the no-chase counts show it separates losers.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Momentum into the zone ceiling (F-5)',
    where:
      'Strategy → Day Plan → Loss-fix switches → Momentum-into-zone ceiling (planner_momentum_into_zone_max_atr)',
    what: 'When > 0, the arm path refuses a placement when price moved TOWARD the zone (long: down into it, short: up into it) over the last 15 minutes (3 × 5m) by more than this many ATR5m (the arm seam’s own ATR5m). Counted momentum_into_zone. 0 = OFF.',
    trader:
      'On the report’s numbers: #650 moved 3.5 × ATR5m into its zone (−57.75 pts in 15 min) and #653 1.3 × (−70.75 pts) — any ceiling below 3.5 refuses #650, below 1.3 refuses #653. #652 is not refused (its 15-minute move was −28, away from a short’s zone). #649 and #654: the report gives no ATR5m, so not computed. The live gate judges at the pass that PLACES the order; the fixture is the move into the fill.',
    consumer:
      'trader/planner_momentum_gate.go momentumIntoZoneVerdict · trader/entry_gate.go EntryGate (arm path, via entryGateForArm) · armSeamATR5m · store/planner_knobs.go ResolvePlannerMomentumIntoZoneMaxATR · 🧱 per-trader boot line',
    range: '0 = OFF · 0–10 × ATR5m',
    systemDefault: '0 (OFF)',
    recommended:
      '⭐ OFF until the owner picks a number — the report gives two data points (3.5 and 1.3), not a threshold.',
    whenToTouch:
      'When you want to stop buying into a dump / selling into a squeeze.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Cap the ATR stop floor at the authored invalidation (F-7)',
    where:
      'Strategy → Day Plan → Loss-fix switches → ATR floor cap (planner_atr_floor_cap_at_invalidation)',
    what: 'ON: the 1.5 × ATR5m stop floor may widen the stop only up to the authored invalidation = the farther of the planner’s stop and the scenario’s invalidation-line price (when its sentence parses). If the floor lies beyond it, the arm is REFUSED — at write a repair hint / written disabled, at arm a WARN + counter atr_floor_beyond_invalidation — the stop is never widened silently. OFF = today: the floor widens the stop past the authored one ("🛑 arm stop … WIDENED").',
    trader:
      'On 10-14: #649 refused (floor 30882.78 past the authored 30873 — the −$79 loss avoided); #653 refused (floor 30427.25 past the authored 30436 — the −$178 loss avoided). #650, #652, #654 unchanged (their stops were not widened).',
    consumer:
      'trader/arm_stop_anchor.go atrFloorCapVerdict (after composeArmStop, legacy branch) · trader/write_time_feasibility.go (write) · trader/armed_executor.go (arm pass) · store/planner_knobs.go ResolvePlannerATRFloorCap · 🧱 per-trader boot line',
    range: 'ON | OFF',
    systemDefault: 'OFF (unset)',
    recommended:
      '⭐ OFF until the owner rules — it trades fewer setups (it refuses instead of widening).',
    whenToTouch:
      'When you would rather skip a setup than take a stop wider than the plan’s own invalidation.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Stale-zones wake (W26)',
    where:
      'Strategy → Day Plan → Loss-fix switches → Stale-zones wake (planner_stale_zones_max_atr)',
    what: 'When > 0, the level-event wake additionally fires ONE budget-free planner re-read when EVERY armed market_in_zone leg’s entry is farther than this many ATR5m from the last closed 1m close — the 10-14→10-15 night where 12345 sat all night with zones 100+ pts above a falling tape. Spaced by wake_min_interval_min like every other wake, and it still defers when the flip evaluator owns the plan (stale bars or a breached flip line): it re-reads only when it is safe to author. 0 = OFF.',
    trader:
      'On 10-14 the zones stayed far above for hours (17:30 → 00:00) because the 1m/5m feed was stale (the flip/death evaluator skipped on stale_bars) and later the planner calls failed with "no JSON object found" for ~2 hours. This wake would have forced a re-read in the windows where the tape WAS fresh and the flip line was not breached.',
    consumer:
      'trader/auto_trader_wake_levels.go collectLevelWakeCandidates (stale-zones candidate) · store/planner_knobs.go ResolvePlannerStaleZonesMaxATR · 🧱 per-trader boot line',
    range: '0 = OFF · 0–50 × ATR5m',
    systemDefault: '0 (OFF)',
    recommended:
      '⭐ OFF until the owner picks a number — it trades fewer dead hours but a too-low threshold re-reads on normal drift.',
    whenToTouch:
      'When you want the planner to re-read instead of sitting far from its zones all night.',
    perSession: 'No — strategy-level.',
  },
]

const risk: KnobSpec[] = [
  {
    label: 'Min confidence',
    where: 'Strategy → Risk Control',
    what: "Floor on the AI's confidence integer; below = refused. AI DECISION PATH ONLY: it does NOT apply to mentor mode (mentor entries come from the course rules and carry no confidence), to pep mode (pep entries come from the two fixed PDF rules and carry no confidence) or to planner arms (under plan_mode strict, entries only through armed scenarios, this floor never runs). Since release #22 an arm entry's confidence is stored EMPTY (shown n/a) instead of a fabricated 0; rows written before release #22 still show 0.",
    trader:
      'The simplest honesty gate: 60 means "be at least 60% sure" — for AI decisions, not for mentor or pep entries or armed plan scenarios.',
    consumer:
      'kernel/engine_position.go validateDecision (confidence gate, decision path)',
    range: '50 – 100',
    systemDefault: '60 (owner)',
    recommended:
      '⭐ 60 — live config; Sep-9 ruling: the 65 raise is DEFERRED, not dead — the 60–64 band gets judged at full n (protection lives in strict + R:R + min-SL + armed meanwhile).',
    whenToTouch: 'Raise if the AI enters low-conviction junk too often.',
    perSession: 'No.',
  },
  {
    label: 'Max positions',
    where: 'Strategy → Risk Control',
    what: 'Max simultaneous open positions.',
    trader: '1 = single position; 3 = diversified.',
    consumer:
      'kernel/engine_analysis.go (ResolveConcurrentCap ← max_positions) · trader/auto_trader_risk.go enforceMaxPositions',
    range: '1 – 3',
    systemDefault: 'blank → 1 [I] (ClampLimits floor); 3 [O] (owner)',
    recommended: '⭐ 3 — matches config; MNQ SIM never needs the extra legs.',
    whenToTouch: 'Set 1 for single-position discipline.',
    perSession: 'No.',
  },
  {
    label: 'Leverage BTC/ETH / alt',
    where:
      'Strategy → Risk Control — CRYPTO strategies only; the Studio hides this row on a futures strategy',
    what: 'Exchange leverage cap per coin class for CRYPTO entries. CME futures have no exchange leverage — they size by contract count, so it does not change a futures order size.',
    trader:
      'The code clamps a saved value to 1–20 (store/strategy.go ClampLimits: MaxBTCETHLeverage = MaxAltLeverage = 20) — the same range as the page sliders, so nothing the page can save is cut.',
    consumer:
      'kernel/engine_position.go validateDecision (caps the decision’s leverage; futures sizing ignores it) · store/strategy.go ClampLimits',
    range: '1 – 20 (page and code, both classes)',
    systemDefault:
      '5 / 5 (code and page default; both bound strategies store 5 / 5)',
    recommended: 'n/a on a futures strategy — the row is hidden there.',
    whenToTouch:
      'Only on a crypto strategy; on MNQ it does nothing (the row is hidden).',
    perSession: 'No.',
  },
  {
    label: 'Min risk:reward',
    where: 'Strategy → Risk Control',
    what: 'The R:R floor every entry must clear (computed on real stop/target).',
    trader: 'Raising this is the single strongest filter on bad entries.',
    consumer:
      'kernel/engine_position.go validateDecision (R:R gate, minRiskReward; counted rr_gate) · trader/entry_gate.go EntryGate (MinRR — the same floor on the arm seam)',
    range: '1 – 10 (step 0.5)',
    systemDefault:
      '3 — the code fallback, used only when the bound strategy saves no value',
    recommended:
      '⭐ 2 — the value BOTH bound strategies have SAVED (min_risk_reward_ratio = 2 since the 2026-09-01 08:13 CT save; read 2026-10-14). It is the ONE floor for the decision path AND the arm seam (ARM_MIN_RR was deleted, owner ruling 2026-09-03). 3+ measurably cuts entry count.',
    whenToTouch: 'Raise to 3+ if wins are too small to cover losers.',
    perSession: 'No.',
  },
  {
    label: 'Max margin',
    where:
      'Strategy → Risk Control — CRYPTO strategies only; the Studio hides this row on a futures strategy',
    what: 'Margin-usage ceiling (AI-guided): it reaches the AI prompt as text ("Max Margin Usage: ≤N%"); no gate reads it. A crypto margin concept — futures margin is a per-contract bond.',
    trader: 'Prompt guidance for crypto entries only.',
    consumer:
      'kernel/engine_prompt.go (prompt text only) · store/strategy.go ClampLimits (10–100%)',
    range: '10 – 100 % (page and code)',
    systemDefault: '90 (code and page default; both bound strategies store 90)',
    recommended: 'n/a on a futures strategy — the row is hidden there.',
    whenToTouch: 'Only on a crypto strategy; on MNQ it does nothing.',
    perSession: 'No.',
  },
  {
    label: 'Min position size',
    where:
      'Strategy → Risk Control — CRYPTO strategies only; the Studio hides this row on a futures strategy',
    what: 'Smallest USD position a CRYPTO entry may open. Futures size by contract count, so the futures order path never reads it.',
    trader: 'Crypto only; no effect on MNQ.',
    consumer:
      'trader/auto_trader_risk.go enforceMinPositionSize (called only on the crypto branch of trader/auto_trader_orders.go) · kernel/engine_prompt.go (prompt text) · store/strategy.go ClampLimits (10–1000)',
    range: '10 – 1000 USD (page and code)',
    systemDefault: '12 (code and page default; both bound strategies store 12)',
    recommended: 'n/a on a futures strategy — the row is hidden there.',
    whenToTouch: 'Only on a crypto strategy; on MNQ it does nothing.',
    perSession: 'No.',
  },
  {
    label: 'Hold lock',
    where: 'Strategy → Risk Control',
    what: 'While ON and a position is open, an AI-decision close_long / close_short for that symbol and side is SUPPRESSED (logged "🔒 HOLD-LOCK", recorded as a deliberate no-op): the trade rides to its resting stop/target bracket. There is no other hold condition, and opening is untouched.',
    trader:
      'Stops the AI from cutting a trade before its own stop/target. It does not hold you back: only AI decisions are gated — Emergency Flat and the drawdown monitor close through the trader directly and bypass it.',
    consumer:
      'trader/auto_trader_orders.go holdLockSuppressesClose (asked by the AI decision loop in trader/auto_trader_loop.go for close_long / close_short)',
    range: 'ON | OFF',
    systemDefault: 'OFF (an unsaved hold_discipline reads OFF)',
    recommended:
      '⭐ OFF — the plan already manages exit timing. Note: BOTH bound strategies store hold_discipline = true (read 2026-10-14 for a5b7662e "MNQ" and 7a853ea3 "mnq 2"), so the lock is ON for the running traders.',
    whenToTouch: "ON if exits keep firing before the plan's own criteria.",
    perSession: 'No.',
  },
  {
    label: 'Wake cadence (class 47)',
    where: 'env WAKE_CUTOFF_MIN · WAKE_COOLDOWN_MIN (no Studio row yet)',
    what: 'Two cutoffs on level-event wakes, ENFORCING since the owner ruling of 2026-09-03 (they shipped WARN-first, logging "would_skip" and letting the wake run, until then). CUTOFF: a wake starting within WAKE_CUTOFF_MIN of the session flat is SKIPPED ("⏱ wake SKIPPED: <n> min to flat") — 25m = the 15m last-entry cutoff plus the ~9.3m p90 planner call, so a read starting inside it lands after the gate has closed. COOLDOWN: a wake within WAKE_COOLDOWN_MIN of the last wake-AUTHORED plan version is SKIPPED ("⏱ wake SKIPPED: cooldown <m> min …") — measured from the last version a wake actually WROTE, which is what makes it different from wake_min_interval_min (that paces attempts). A fast market waives the COOLDOWN only (WakeCadenceDecision.FastMarketBypass): drift since the last plan write ≥ FAST_MARKET_ATR (default 1.5) × ATR5m. The flip re-read and the death re-read obey the CUTOFF too, never the cooldown. Wake skips are counted per trader/session-day/session; a skipped flip or death re-read is logged, not counted.',
    trader:
      'Why: 60 wake re-plans in 7 days produced 33 arm rows, 23 ever placed, 9 ever working. On 09-02 the wakes fired every ~30 minutes from 08:42 to 14:20 — the drumbeat of the throttle, not of events — and NY bought 12 plan versions. The 14:20 wake sat 10 minutes from the last-entry cutoff. On 2026-09-03 a wake with 24 min to flat wrote LONDON v2 at 08:15:44 for an 08:30 flat, and the owner ruled both cutoffs to enforce.',
    consumer:
      'trader/class47_wake_cadence.go (resolvers, WakeCadenceDecision, lines) · trader/auto_trader_wake_levels.go (the wake path: both cutoffs) · trader/auto_trader_planner.go maybeRereadAfterFlip + trader/death_reread.go maybeRereadAfterDeath (cutoff only) · trader/auto_trader_loop.go fastMarketATR · store/class47_counters.go (recorded counters)',
    range:
      'WAKE_CUTOFF_MIN · WAKE_COOLDOWN_MIN: any whole number ≥ 0 (0 = off; no upper limit); a negative or non-numeric value falls back to the default',
    systemDefault:
      '25m cutoff · 30m cooldown — both ENFORCING (0 turns one off); the cooldown is waived at a fast market',
    recommended:
      '⭐ the defaults (25 / 30) — the owner’s enforcing ruling of 2026-09-03.',
    whenToTouch:
      'Only with an owner ruling: a lower value lets more wakes through, and 0 turns that cutoff off entirely.',
    perSession: 'No.',
  },
  {
    label: 'Automatic structural stop and target',
    where:
      'Day Plan → stop buffer; Risk Control → existing daily loss controls',
    what: 'A reject fade uses the far edge of its frozen entry zone plus the resolved buffer. The first distinct eligible target zone supplies the target. Prices are fixed before the admission checks; a failed ratio refuses the trade.',
    trader:
      'Missing structural provenance records ATR fallback but refuses entry. The owner controls daily loss through the existing daily guardrails; no separate per-trade dollar cap is required. One MNQ cannot be resized into a smaller trade. The ATR multiplier remains 1.5 but never overrides an available structural stop.',
    consumer:
      'store/structural_geometry.go:ResolveStructuralStop · trader/structural_geometry.go:ComposeLevelFadeGeometry · trader/armed_executor.go',
    range:
      'Measured training grid: 0.25, 1.25 and 4.50 MNQ points. No externally validated universal buffer.',
    systemDefault:
      '4.50 points [I], outward-rounded p95 of 6,181 in-sample held touches. Daily-loss settings retain their existing value and switches.',
    recommended:
      '[I]/[T] a codeable research candidate, not a validated replacement. No external evidence fixes its buffer or proves that it will turn the losing book positive.',
    whenToTouch:
      'Choose a buffer only after judging the full corrected sweep. The system calculates the stop price; daily-loss controls are separate. Do not change either price merely to pass 2R.',
    perSession: 'No.',
  },
  {
    label: 'Breakeven trigger (AI trades)',
    where: 'Strategy → Risk Control',
    what: 'AI-plan trades only: once an open NT8 position is this many POINTS in profit (mark vs entry), its stop moves to the entry price, once per position (a move_stop on the live bracket). It moves a stop only when BOTH switches are on: this strategy toggle (breakeven_enabled) AND the server seam (env EXIT_MECHS_SUSPENDED=0 → seam=ACTIVE; unset → SUSPENDED, the code default). With the seam SUSPENDED the trigger still evaluates, logs "⏸ auto-breakeven SUSPENDED (0B)" once and sends nothing. A SENT move is not assumed: the next fresh NinjaTrader book judges it — "🎯 auto-breakeven CONFIRMED at NT8", or "🚨 auto-breakeven NOT APPLIED at NT8" (a P0, counted; the move is re-sent on the next monitor beat). A mentor-mode trade is NEVER moved by it — the mentor exits own that stop (owner ruling 2026-10-09; see Mentor mode → The exits → mentor_trail_mode). A pep trade is never moved by it either: pep has no break-even and no trailing (PDF rule) — its stop and target sit at NT8 from the entry (Section 20). Since release #25 a non-zero breakeven_trigger_r replaces this points trigger with N × the position’s initial risk (see "Breakeven trigger in R (F-8)"); 0/unset keeps this points rule.',
    trader:
      'Read the boot line "🛑 exits: … BE=on|off(strategy) · seam=ACTIVE|SUSPENDED(env)" for what the running bot does — not this card. The live boots of 2026-10-13 18:57/18:58 CT and 2026-10-14 02:10 CT read BE=on · seam=ACTIVE on both traders with breakeven_trigger_points=40: breakeven IS moving AI-trade stops there (the mentor trader is exempt). Turn the toggle off to stop it.',
    consumer:
      'trader/auto_trader.go maybeMoveStopToBreakeven (mentor gate → exitMechSuspendedRefuse → moveStopWire) · trader/exit_mechs_suspend.go ExitPolicyBootLine',
    range:
      'points (not ticks) · unset/0 → 50 · moves a stop only with breakeven_enabled ON and env EXIT_MECHS_SUSPENDED=0',
    systemDefault:
      'toggle OFF · 50 points · seam SUSPENDED when the env is unset',
    recommended:
      '⭐ only with MFE evidence that the move pays. Wave 1A (2026-09-02) records it: `go run ./cmd/excursions` prints MFE p50/p80/p95 per condition with the n each rests on.',
    whenToTouch:
      'Only with MFE evidence that the move pays — the distribution is in trade_excursions, so this is answerable rather than a judgement call.',
    perSession: 'No.',
  },
  {
    label: 'Breakeven trigger in R (F-8)',
    where:
      'Strategy → Risk Control → Auto-breakeven → Trigger in R (breakeven_trigger_r)',
    what: 'Release #25 (STRICT-12345 F-8): when > 0, an AI/planner position moves its stop to entry once its profit reaches breakeven_trigger_r × its initial risk (|entry − initial stop|) instead of breakeven_trigger_points. Same switches as the points rule (breakeven_enabled + the server seam), same once-per-position move. An unknown initial risk falls back to the points rule, logged once and counted breakeven_r_unknown_risk — never a guessed risk. A mentor position is never moved by it (mentor mode keeps the R3 rule). 0/unset = OFF = the points rule, byte-identical.',
    trader:
      'On 10-14 at 1R: no change on #649–#653 (their best MFE was 0.63R, so neither rule would have moved them — the points rule at 40 did not either). #654’s initial risk is ≈95–113 pts depending on its fill (stop 30551.25; the report gives the placement price 30438.75, not the fill), so 1R needs that much profit; its points-rule BE moved at +40 at 10:08:27. The report’s history figure (+$595 [B] on six planner losers that reached ≥1R and still took a full stop: 623, 599, 620, 595, 607, 621) is the report’s, NOT replayed here.',
    consumer:
      'trader/breakeven_r.go breakevenTriggerFor (called by trader/auto_trader.go maybeMoveStopToBreakeven) · store/planner_knobs.go ResolveBreakevenTriggerR · 🧱 per-trader boot line',
    range:
      '0 = OFF (points rule) · 0–10 R · moves a stop only with breakeven_enabled ON and env EXIT_MECHS_SUSPENDED=0',
    systemDefault: '0 (OFF — the points rule)',
    recommended:
      '⭐ OFF until the owner rules — the report’s +$595 is a history subset [B], not a replay.',
    whenToTouch:
      'When you want breakeven to scale with each trade’s own risk instead of a fixed point distance.',
    perSession: 'No.',
  },
  {
    label: 'Trailing stop (AI trades)',
    where: 'Strategy → Risk Control',
    what: 'AI-plan trades only: once armed (after_breakeven | N points | immediately), each 60 s monitor beat ratchets the stop to best price since entry ∓ mult×ATR(period, 5m), tighten-only. Like breakeven it moves a stop only when BOTH this toggle (trailing_enabled) AND the server seam (env EXIT_MECHS_SUSPENDED=0) are on; with the seam SUSPENDED it computes a level and sends nothing. Never applied to a mentor-mode trade — the mentor runner has its own candle trail (Mentor mode → The exits) — nor to a pep trade (pep has no trailing, Section 20). Since release #32 the panel shows the "not applied while mentor mode is ON" caveat under BOTH the breakeven and the trailing toggle, read from the server’s effective origin (never a typed copy). Evidence on ATR trails: a 567,000-backtest study ranks ATR/Chandelier trails in the worst group of 15 exit families; our 09-01 tape shows $719.50 of giveback with zero trail exits.',
    trader:
      'Read the boot line "🛑 exits: … trail=on|off(strategy) · seam=…(env)". The live boots of 2026-10-13 and 2026-10-14 read trail=off · seam=ACTIVE: the ATR trail is not moving stops there.',
    consumer:
      'trader/auto_trader_trailing.go (maybeTrailStop → exitMechSuspendedRefuse → moveStopWire)',
    range:
      'mult 0.5–10 · period 5–50 (the Studio inputs) · arm: after_breakeven | N-points | immediately · moves a stop only with trailing_enabled ON and env EXIT_MECHS_SUSPENDED=0',
    systemDefault:
      'toggle OFF · 2.0 / 14 / after_breakeven · seam SUSPENDED when the env is unset',
    recommended:
      '⭐ leave OFF until the MFE distribution says the trail pays: `go run ./cmd/excursions` prints MFE p50/p80/p95 per condition with the n each rests on.',
    whenToTouch: 'Only with evidence the trail beats the fixed target.',
    perSession: 'No.',
  },
  {
    label: 'Guardrails master',
    where: 'Strategy → Risk Control → Guardrails',
    what: 'Master switch for the daily guardrails stack (loss/profit caps, max trades, consistency, blackout windows). The consecutive-loss halt and the re-entry cooldown are NOT under this switch — each bites whether the master is on or off. Since release #32, with the master OFF every child guardrail row (daily loss / daily profit / max trades / blackout / consistency) shows a red "NOT ENFORCED — Risk guardrails master is OFF" badge beside its own toggle, so a stored value that looks set is visibly not enforced.',
    trader:
      'Currently OFF by owner ruling — the would-have-tripped counters still display.',
    consumer: 'kernel/risk_limits.go ResolveStrategyGuardrails (MasterEnabled)',
    range: 'ON | OFF',
    systemDefault:
      'OFF — a strategy that never saved this switch reads OFF (since release #22, owner ruling R4). Both live strategies store OFF.',
    recommended:
      '⭐ OFF for now (owner ruling) — re-armed after the risk audit is reviewed.',
    whenToTouch: 'ON when you want the daily circuit breakers live.',
    perSession: 'No.',
  },
  {
    label: 'Max contracts (always-on) — Stage A: 1 (AI / planner path only)',
    what: 'Contract cap per order on the AI / planner path — on with or without the master. 0B (2026-09-02): every AI / planner resolution is clamped to the Stage-A ceiling of 1 contract — survival-first under an undemonstrated edge. Stage B (2) only at n≥30 closed trades with a POSITIVE LOWER-CI expectancy; Kelly and optimal-f are undefined without an edge estimate. Before 0B the two resolvers disagreed: arm-leg capacity said 1 while order sizing said 2, and the boot line said capacity=1. MENTOR MODE IS NOT CLAMPED BY THIS: a mentor-mode trader skips this resolver (resolveMaxContracts returns mentor_max_contracts) and sizes every entry from the mentor size table — 5 by default (base, confluence and big are all 5), 3 when the stop is over 20 and up to 25 pts (above 25 is refused) or an ISB sits at an old high/low or inside a range, 2 on the SWING4H (and a swing whose stop reaches 100 pts is sized down to at most 2, never refused) — capped only by mentor_max_contracts (default 20). See Mentor Mode → Sizing. PEP MODE IS NOT CLAMPED BY THIS EITHER: a pep trader sizes from its own pep_contracts setting (default 6, hard ceiling 10; Section 20).',
    where: 'Strategy → Risk Control → always-on row',
    trader:
      'THE ARITHMETIC: 0B also raised the stop floor from 1.0× to 1.5×ATR5m, which lifts dollar risk per trade by roughly 50% at constant size. That is precisely why size does NOT move at the same time.',
    consumer:
      'kernel/risk_limits.go (ResolveMaxContracts → ClampStageAContracts) · mentor bypass: trader/auto_trader_orders.go resolveMaxContracts → trader/mentor_mode.go mentorMaxContracts',
    range:
      'page value · Stage-A ceiling 1 · env STAGE_A_CONTRACT_CAP raises it · not applied in mentor mode',
    systemDefault:
      '1 (Stage A) on the AI / planner path; mentor mode: the mentor size table (5 base · 3 reduced · 2 swing, cap 20)',
    recommended: '⭐ 1 — do not raise before the n≥30 lower-CI test.',
    whenToTouch: 'Stage B, with the expectancy table in hand.',
    perSession: 'No.',
  },
  {
    label: 'Planner contracts per entry',
    what: 'The PLANNER trader\u2019s contracts per entry (dispatch-31, owner order 2026-10-16 "set to 4 at least, sell 2 first so it\u2019s equal"). Unset/0 → 1 = today\u2019s single-contract arm. A value ≥ 2 splits the planner arm AT ENTRY into equal halves: leg 1 = n/2 (floor) contracts at the plan\u2019s FIRST TARGET (target_chain[0]; the first obstacle is only the fallback, and only when it is ≥ +1R away), leg 2 = the rest at the arm\u2019s final target with the existing stop management. If leg 1\u2019s price would equal or pass the final target the split degrades to ONE n-lot bracket. The stamped total is clamped to the always-on Max contracts / order (resolveMaxContracts — max_contracts_per_order + the Stage-A ceiling), re-clamped at send, and to a hard ceiling of 10 (a typo of 40 is refused). A 2-leg sweep_reclaim arm and a Picture arm are never split. Mentor sizing is untouched (the mentor table, not this knob). Read on the planner arm path only.',
    where: 'Strategy → Risk Control → Planner contracts per entry',
    trader:
      'To actually size 4 = 2 + 2 on "mnq 2" the owner must ALSO set max_contracts_per_order = 4 AND the env STAGE_A_CONTRACT_CAP = 4 (a .env change + restart) — today the always-on clamp resolves to 1 for that strategy.',
    consumer:
      'trader/auto_trader_orders.go plannerContracts / stampPlannerSplit (clamped to resolveMaxContracts) · trader/armed_executor.go + trader/zone_placement.go (the stamped row reaches the wire as one split frame via PlaceLimitEntryWithSplit)',
    range:
      'int · unset/0 → 1 · ≥ 2 splits equal halves · ceiling 10 · clamped to resolveMaxContracts',
    systemDefault: '1 (single-contract arm)',
    recommended:
      '⭐ 1 everywhere except the owner\u2019s planner strategy (4, after raising max_contracts_per_order and STAGE_A_CONTRACT_CAP).',
    whenToTouch: 'Only on an owner ruling for the planner trader.',
    perSession: 'No — per strategy.',
  },
  {
    label: 'Notional cap (always-on)',
    where: 'Strategy → Risk Control → always-on row',
    what: 'Max notional per position — on with or without the master.',
    trader: 'The second unswitchable guardrail.',
    consumer: 'kernel/engine_position.go (notional cap path)',
    range: 'page value · default 20',
    systemDefault: '20',
    recommended: '⭐ 20 — current config.',
    whenToTouch: 'Raise only for deliberate sizing studies.',
    perSession: 'No.',
  },
  {
    label: 'Position value ratio (BTC/ETH / alt)',
    where:
      'Strategy → Risk Control — CRYPTO strategies only; the Studio hides these tiles on a futures strategy, and they only display the stored value (no input)',
    what: 'CRYPTO sizing ceiling: position_value ≤ equity × ratio. A futures entry never uses it — it is capped by the always-on notional cap (equity × max_notional_leverage) instead.',
    trader:
      '5x BTC/ETH and 1x alt = a crypto entry can size up to 5× equity on majors, 1× on alts. No effect on MNQ.',
    consumer:
      'trader/auto_trader_risk.go enforcePositionValueRatio (CME futures take the notional cap there) · kernel/engine_position.go validateDecision (posRatio, crypto symbols) · store/strategy.go ClampLimits (0.5–10)',
    range: 'no page input · the code clamps a stored value to 0.5 – 10',
    systemDefault: '5 / 1 (code default; both bound strategies store 5 / 1)',
    recommended: 'n/a on a futures strategy — the tiles are hidden there.',
    whenToTouch:
      'Only on a crypto strategy (there is no page input to change it); on MNQ it does nothing.',
    perSession: 'No.',
  },
  {
    label: 'Daily loss limit',
    where:
      'Strategy → Risk Control → Guardrails: the master switch AND the Daily loss row (toggle + $)',
    what: "OFF unless you turn it on in Strategy Studio → Risk Control: the Guardrails master switch AND the Daily loss limit row's toggle, with the dollar amount in that row's $ field (daily_loss_limit_usd). Both live strategies store both switches OFF, so the $450 there enforces nothing, and a strategy that never saved them reads OFF too (the 🛑 [trader] daily-loss gate boot line says 'OFF (default)' for an unsaved switch and 'off (saved)' for yours). With BOTH switched on, when the bot's OWN session-day P&L — realized + open, MANUAL trades excluded (rows reconcile adopted with no order of ours) — is at or past −limit: (1) every new entry is REFUSED on every path (AI decision, agent chat, planner arms, mentor, Picture), counted daily_loss; (2) the 60-second monitor beat CANCELS every resting ENTRY order (protective stops/targets are never cancelled); (3) it FLATTENS the bot's open positions through the EOD-flat path (a manual position is left alone); (4) it stays LATCHED until 17:00 CT, even if the P&L recovers, and survives a restart. Switching the gate OFF does NOT lift a latch (the fail-closed default while the owner has not ruled on lifting): entries stay refused and bot positions are re-flattened until 17:00 CT, so the trader's boot line (also printed on a Studio save) reads 'daily-loss gate: LATCHED for session-day <d> (switching the gate OFF does not lift it; Emergency Flat lifts it; it ends at 17:00 CT)' instead of OFF, and GET /api/risk/status says latched with latch_day and latch_lifts. The trip logs '🔴 DAILY LOSS LIMIT HIT' and counts daily_loss_tripped exactly once per trader and session-day, whichever side latched first: the 60-second beat, or the AI decision cycle's guardrail right after a stop-out (before release #24 that second trip was silent). An unresolved close today (pnl_corrected NULL) is excluded from the sum and counted; it refuses new MENTOR placements fail-closed (as since release 10-05-1) and never flattens on a guess. Emergency Flat on a trader clears that trader's latch only; if its loss still stands it re-trips on the next beat.",
    trader: 'The circuit breaker that ends a bad day at a known dollar number.',
    consumer:
      'trader/daily_loss_gate.go (botDayPnLAt — the one P&L read; dailyGuardVerdictAt; dailyLossBeatAt / dailyLossFlattenAt on monitorTick) · trader/entry_admission.go (every path) · kernel/risk_limits.go DailyGuardrails.Check · kernel/engine_analysis.go (AI decision cycle)',
    range: 'USD · empty/0 → env RISK_MAX_DAILY_LOSS_USD ($500 default)',
    systemDefault:
      'OFF. A strategy that never saved the switches reads OFF (since release #22, owner ruling R4 — before, it inherited master ON + daily-loss ON). The $ field, when empty/0, still falls back to the env RISK_MAX_DAILY_LOSS_USD ($500) once both switches are ON.',
    recommended: '⭐ set it to a loss you can absorb once a week.',
    whenToTouch: 'Set at the start of the week; review after every trip.',
    perSession: 'No.',
  },
  {
    label: 'Daily profit cap',
    where: 'Strategy → Risk Control → Guardrails',
    what: 'Realized PnL ≥ cap stops new entries for the day (lock-in, not close-out).',
    trader: 'Takes the win and stops overtrading a good day.',
    consumer: 'kernel/risk_limits.go:185 (DailyProfitEnabled)',
    range: 'USD · needs its own toggle AND the master',
    systemDefault:
      'OFF — an unsaved toggle resolves OFF (kernel/risk_limits.go ResolveStrategyGuardrails: DailyProfitEnabled default false); it acts only when saved ON and the master is ON',
    recommended: '⭐ ON — one of the cheapest edge protections in the stack.',
    whenToTouch: 'Disable only if you deliberately want unlimited upside days.',
    perSession: 'No.',
  },
  {
    label: 'Max daily trades',
    where: 'Strategy → Risk Control → Guardrails',
    what: 'Entry count cap per CME session-day (17:00 CT). Enforced only with the master AND its own toggle ON (both OFF today). Since release #22 it refuses on every path, MENTOR included (counted max_daily_trades / mentor max_daily_trades_refused) — before, the mentor never evaluated it.',
    trader: "Stops revenge-trading after the day's quota is spent.",
    consumer: 'kernel/risk_limits.go:187 (MaxDailyTradesEnabled)',
    range: 'count · needs its own toggle AND the master',
    systemDefault:
      'OFF — an unsaved toggle resolves OFF (ResolveStrategyGuardrails: MaxDailyTradesEnabled default false). Both bound strategies save it OFF (max_daily_trades 3 is stored but not enforced).',
    recommended: "⭐ ON with a number that fits the strategy's hit rate.",
    whenToTouch: 'Review weekly against the win rate.',
    perSession: 'No.',
  },
  {
    label: 'Consecutive-loss halt',
    where: 'Strategy → Risk Control → Guardrails',
    what: "N consecutive losing closes in one CME session-day halt NEW entries on every path (decision, agent, arm, picture) until the 17:00 CT roll, and resting entries are withdrawn. NOT gated by the guardrails master. PRESENCE-AWARE since W1 (settings truth, 2026-09-23): toggle OFF stores 0 = OFF; toggle ON or a BLANK box = inherit (env BREAKER_HALT_N when set, else 8); a number = that N. Before W1 the row showed a missing value as OFF while the runtime enforced 8, and its OFF wrote a 0 no save could store. The 🛑 boot lines print what is enforced: breaker=8[I] (shipped default), 3[O] / off[O] (saved), 5[E] / off[E] (env), or n/a when not exactly one strategy is bound (each trader's own '🛑 [trader] breaker=' line then speaks for it).",
    trader:
      'The streak-breaker: three losers in a row is the market telling you something.',
    consumer:
      'store/resolve_source.go ResolveBreakerHalt (the ONE rule: saved incl. 0 → env BREAKER_HALT_N → 8) · trader/auto_trader_orders.go consecutiveLossHaltedAt (decision/agent) · trader/session_risk.go sessionRiskGateAt (arm/picture) · trader/withdraw.go · store/position_query.go CountConsecutiveLossesSince · telemetry gate-block consecutive_loss',
    range: 'OFF (0) · inherit (blank) · 1 – N',
    systemDefault:
      'inherit → 8 [I]; BREAKER_HALT_N overrides the inherit [E] — ON, not master-gated',
    recommended: '⭐ ON, threshold 2–3.',
    whenToTouch:
      "Leave ON — this is the cheapest guardrail in the stack. CAVEAT (W1): a Studio save writes an OFF (0) together with its confirmation record (system_config settings_truth_zero:<strategy id>) in one transaction, and the 🩺 boot line and the effective chip print 'OFF — confirmed by Studio save <time CT>'. A strategy restored or imported WITHOUT that record row reads 'explicit 0 UNCONFIRMED — re-save in Studio' and refuses its trader at load until it is re-saved in the Studio. This is fail-closed, by design. A strategy-level replan cap of 0 works the same way.",
    perSession: 'No.',
  },
  {
    label: 'Re-entry cooldown',
    where: 'Strategy → Risk Control → Guardrails',
    what: 'After a STOP-LOSS exit (NT8 exit reason "sl"; target and manual exits do not arm it), a SAME-direction re-entry on that symbol is refused for this many minutes. On the AI decision path it also unlocks early once price is a full ATR15 away from the stop; the arm and Picture paths unlock on the timer only. The opposite direction and other symbols are never blocked, an agent-chat entry is not checked, and the stop record is in memory only (a restart clears it). Since release #32 the Studio shows "OFF (absent = 0)" beside the field when the key is absent (the Go truth), and the toggle writes 20 only when you turn it on.',
    trader: 'Prevents immediately re-entering after being stopped.',
    consumer:
      'trader/entry_admission.go admitChain → reentryCooldownMinutes + discipline.ReentryPeek (arm/picture) · kernel/engine_analysis.go applyReentryCooldown (decision)',
    range: 'minutes · 0 = OFF; NOT gated by the master switch',
    systemDefault: 'OFF (0) until a strategy value is saved — not master-gated',
    recommended: '⭐ ON — 5–15 minutes.',
    whenToTouch: "Tune to the strategy's average re-arm time.",
    perSession: 'No.',
  },
  {
    label: 'Consistency',
    where: 'Strategy → Risk Control → Guardrails',
    what: 'Today’s realized profit may not reach this % of the all-time total realized profit: while it does, the AI decision cycle HOLDS (no new decisions). It never fires on a losing day or before there is any prior-day profit (kernel/risk_limits.go ConsistencyBreached). Checked on the AI decision cycle only — armed, mentor and Picture entries are not checked against it.',
    trader: 'Caps how large a share of all-time profit a single day may take.',
    consumer:
      'kernel/engine_analysis.go (decision-cycle guardrail block: ConsistencyEnabled + ConsistencyBreached → HOLD, counted strategy_studio_consistency) · kernel/risk_limits.go ConsistencyBreached / ResolveStrategyGuardrails',
    range: 'percent · needs its own toggle AND the master',
    systemDefault:
      'OFF — an unsaved toggle resolves OFF (ResolveStrategyGuardrails: ConsistencyEnabled default false)',
    recommended: '⭐ ON for a smooth equity curve.',
    whenToTouch: 'Loosen only after a verified regime change.',
    perSession: 'No.',
  },
  {
    label: 'Blackout windows',
    where: 'Strategy → Risk Control → Guardrails',
    what: 'One configured daily CT window (start+end; it may wrap midnight; a malformed or zero-width window never blocks) inside which the AI decision cycle HOLDS — no new decisions.',
    trader:
      'Holds the AI decision cycle only. Armed, mentor and Picture entries are NOT checked against it (the one admission chain has no blackout step), so a resting arm can still place or fill inside the window. NT8 stops/targets keep protecting open positions.',
    consumer:
      'kernel/engine_analysis.go (decision-cycle guardrail block: BlackoutEnabled + kernel.InBlackoutWindow → HOLD, counted strategy_studio_blackout) · kernel/risk_limits.go ResolveStrategyGuardrails (BlackoutEnabled)',
    range:
      'start+end CT · needs its own toggle (blackout_enabled) AND the master',
    systemDefault:
      'OFF — an unsaved toggle resolves OFF (ResolveStrategyGuardrails: BlackoutEnabled default false)',
    recommended:
      '⭐ ON with 12:00–13:30 CT — the window kernel.LunchWindowCT() resolves, the same one the lunch gate reads — or your worst hours.',
    whenToTouch: 'Set for your known-bad hours from the journal.',
    perSession: 'No.',
  },
]

// AI-prompt and regime knobs (FIX-LABELS 2026-09-26): the editable System
// Prompt sections, the custom prompt, the language, and the regime HTF veto.
const prompt: KnobSpec[] = [
  {
    label: 'Custom prompt',
    where: 'Strategy → AI config → custom_prompt (ai_config field)',
    what: 'Extra prompt text appended to the System Prompt for a personalized trading style. Empty = nothing appended; the output format and the risk rules are fixed and cannot be overridden from here.',
    trader:
      'The text rides verbatim into the executor prompt ("# Personalized Strategy") AND — since release #22 (owner ruling) — into the PLANNER prompt as a delimited OWNER GUIDANCE block (<<<OWNER_GUIDANCE … OWNER_GUIDANCE>>>) that never overrides the machine rules. Before #22 the planner never saw it, so under plan_mode strict it could not shape a single entry. Every plan write logs only its length and a short hash (🗣️ owner guidance in the planner prompt … len=… sha256=…), never the text.',
    consumer:
      'store/strategy.go AIStrategyConfig.CustomPrompt (ai_config.custom_prompt) · kernel/engine_prompt_futures.go (executor) · kernel/owner_guidance.go + kernel/planner_prompt.go (planner)',
    range: 'free text · empty = nothing appended',
    systemDefault: 'empty',
    recommended: '⭐ keep it short — it appends verbatim to the prompt.',
    whenToTouch: 'To add a personal trading-style instruction.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Language (prompt + data formatting)',
    where: 'Strategy → language (ai_config field)',
    what: '"zh" for Chinese, "en" for English — this determines the language used for data formatting AND prompt generation.',
    trader: 'The prompt and every formatted data block follow this language.',
    consumer: 'store/strategy.go StrategyConfig.Language',
    range: 'en | zh',
    systemDefault: 'en',
    recommended: '⭐ en — the planner reads English structure best.',
    whenToTouch: 'Switch for a Chinese-language prompt + formatted data.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Role definition (prompt section)',
    where: 'Strategy → AI config → prompt_sections.role_definition',
    what: 'The editable role-definition section of the System Prompt (title + description): the AI identity and core objectives.',
    trader:
      'Author the persona; the fixed parts (output format, risk rules) stay.',
    consumer: 'store/strategy.go PromptSectionsConfig.RoleDefinition',
    range: 'free text',
    systemDefault: 'shipped default section text',
    recommended:
      '⭐ keep the shipped identity unless you want a different persona.',
    whenToTouch: 'To change the AI persona wording.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Entry standards (prompt section)',
    where: 'Strategy → AI config → prompt_sections.entry_standards',
    what: 'The editable entry-standards section: entry signal conditions and avoidances.',
    trader:
      'Author what qualifies as an entry; the risk gates still hard-block.',
    consumer: 'store/strategy.go PromptSectionsConfig.EntryStandards',
    range: 'free text',
    systemDefault: 'shipped default section text',
    recommended:
      '⭐ keep the shipped standards unless you have a specific setup.',
    whenToTouch: 'To tighten or loosen the authored entry criteria.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Decision process (prompt section)',
    where: 'Strategy → AI config → prompt_sections.decision_process',
    what: 'The editable decision-process section: how the AI reasons to a decision.',
    trader:
      'Author the reasoning path; the validator and risk gates still decide.',
    consumer: 'store/strategy.go PromptSectionsConfig.DecisionProcess',
    range: 'free text',
    systemDefault: 'shipped default section text',
    recommended:
      '⭐ keep the shipped process unless you want a different reasoning style.',
    whenToTouch: 'To change the authored reasoning steps.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'HTF trend veto (regime G1)',
    where: 'Strategy → Regime → htf_veto (API/config field)',
    what: 'Refuse NEW entries opposing the CONFIRMED HTF trend (G2 structure). nil → ON (shipped default, dispatch 1.3); false = pre-wave behaviour (no trend veto).',
    trader:
      'ON = counter-trend entries are refused while the HTF trend is confirmed.',
    consumer:
      'store/strategy.go RegimeConfig.HTFVeto · store/resolve_source.go ResolveHTFVeto',
    range: 'ON | OFF',
    systemDefault: 'ON (nil/absent = ON)',
    recommended:
      '⭐ ON — the shipped default; OFF only to reproduce pre-wave behaviour.',
    whenToTouch: 'Turn OFF only for a pre-wave comparison study.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Transition stand-down (regime G4)',
    where:
      'No Studio control — stored setting regime.transition_standdown (API/config field)',
    what: 'Pause plan-direction entries while an unconfirmed counter-trend CHoCH/MSS is outstanding (G4). nil → ON (shipped default); false = pre-wave behaviour (no transition stand-down). The stand-down state advances on the transition state machine and is surfaced to the plan card via the stored transition key.',
    trader:
      'ON = the planner holds off direction entries while the regime transition is unresolved; a genuine transition must confirm before plan-direction entries resume.',
    consumer:
      'store/strategy.go StrategyConfig.TransitionStanddownEnabled (nil → ON) · trader/auto_trader_transition.go observeTransitionStanddown → kernel.TransitionStanddownVerdict',
    range: 'ON | OFF',
    systemDefault: 'ON (nil/absent = ON)',
    recommended:
      '⭐ ON — the shipped default; OFF only to reproduce pre-wave behaviour.',
    whenToTouch: 'Turn OFF only for a pre-wave comparison study.',
    perSession: 'No — strategy-level.',
  },
]

const coinSource: KnobSpec[] = [
  {
    label: 'Source type',
    where: 'Strategy → Coin source → Source Type',
    what: 'Which symbol universe the engine trades from: static (the list below). Any non-static value falls back to static. An empty stored value reads "static".',
    trader:
      'The engine fetches candidates ONLY from the chosen source; static is the fallback list inside every branch.',
    consumer:
      'kernel/engine.go — the SourceType switch over the strategyConfigSchema enum; default "static" when empty.',
    range: 'static',
    systemDefault: 'static (empty string reads static)',
    recommended: 'static for CME futures (MNQ) — the only live source.',
    whenToTouch: 'To change which universe feeds the strategy.',
    perSession: 'No.',
  },
  {
    label: 'Static coin list',
    where: 'Strategy → Coin source → Static Coins',
    what: 'The symbol list used when source_type = static — and the fallback list used by every other source branch when its own fetch is skipped or empty.',
    trader: 'Symbols traded live (e.g. [MNQ] on the futures path).',
    consumer:
      'kernel/engine.go — StaticCoins iterated in every SourceType branch.',
    range: 'comma-separated symbols',
    systemDefault: 'empty',
    recommended: 'Only symbols you actually trade.',
    whenToTouch: 'To add/remove a traded symbol.',
    perSession: 'No.',
  },
  {
    label: 'Excluded coins',
    where: 'Strategy → Coin source → Excluded',
    what: 'Symbols filtered out of the candidate set from the static source. The filter applies to every branch result.',
    trader:
      'An excluded symbol never reaches the engine as a candidate, whatever the source.',
    consumer:
      'kernel/engine.go filterExcludedCoins — applied to candidates and direct lists in every branch (~470-543).',
    range: 'comma-separated symbols',
    systemDefault: 'empty (nothing excluded)',
    recommended: 'Exclude symbols you never want the strategy to touch.',
    whenToTouch: 'When a symbol must be banned from the candidate set.',
    perSession: 'No.',
  },
]

const indicators: KnobSpec[] = [
  {
    label: 'Raw klines',
    where: 'Strategy → Indicators → Raw OHLCV',
    what: 'Raw OHLCV klines — always enabled; required for AI analysis. The editor force-sets it true on mount (futures included).',
    trader: 'There is no OFF position: the engine needs raw bars.',
    consumer:
      'store/strategy.go IndicatorConfig.EnableRawKlines; IndicatorEditor force-set on mount.',
    range: 'true (fixed)',
    systemDefault: 'true',
    recommended: 'Leave it on — it is not optional.',
    whenToTouch: 'Never.',
    perSession: 'No.',
  },
  {
    label: 'EMA',
    where: 'Strategy → Indicators → EMA toggle + periods',
    what: 'Exponential Moving Average block on/off. Default periods [20, 50] (store/strategy.go IndicatorConfig.EMAPeriods + GetDefaultStrategyConfig).',
    trader:
      'OFF removes EMA from the AI indicator text. ON prints only WARMED values: a period-P EMA needs about P + 2.3×P bars (EMA200 → 661) before its starting average weighs under 1%; a timeframe without that many bars prints "EMA200: n/a (not warmed — needs 661 bars)" (release #22 — the old 220-bar read printed an SMA200 under the EMA200 label, 20–60 pts off the chart).',
    consumer:
      'engine_prompt.go/engine_analysis.go read Indicators.EMAPeriods; market.EMAWarmupBars sets the fetch depth and the warm-up gate.',
    range: 'bool + comma periods',
    systemDefault:
      'ON in the new-strategy template (store/strategy.go GetDefaultStrategyConfig → applyFuturesIndicatorDefaults), periods [20, 50]; an absent field reads off',
    recommended: 'Per strategy taste.',
    whenToTouch: 'To add/remove EMA from analysis.',
    perSession: 'No.',
  },
  {
    label: 'MACD',
    where: 'Strategy → Indicators → MACD toggle',
    what: 'MACD block on/off. The C# export currently returns only the MACD line (one float64); signal/histogram are not exposed.',
    trader: 'OFF removes MACD from the AI indicator text.',
    consumer: 'store/strategy.go IndicatorConfig.EnableMACD.',
    range: 'bool',
    systemDefault: 'off (also off in the new-strategy template)',
    recommended: 'Per strategy taste.',
    whenToTouch: 'To add/remove MACD.',
    perSession: 'No.',
  },
  {
    label: 'RSI',
    where: 'Strategy → Indicators → RSI toggle + periods',
    what: 'Relative Strength Index block on/off. Default periods [7, 14].',
    trader: 'OFF removes RSI from the AI indicator text.',
    consumer:
      'store/strategy.go IndicatorConfig.EnableRSI + RSIPeriods default [7,14].',
    range: 'bool + comma periods',
    systemDefault:
      'ON in the new-strategy template (store/strategy.go GetDefaultStrategyConfig → applyFuturesIndicatorDefaults), periods [7, 14]; an absent field reads off',
    recommended: 'Per strategy taste.',
    whenToTouch: 'To add/remove RSI.',
    perSession: 'No.',
  },
  {
    label: 'ATR',
    where: 'Strategy → Indicators → ATR toggle + period',
    what: 'Average True Range block on/off. Default period [14].',
    trader: 'OFF removes ATR from the AI indicator text.',
    consumer:
      'store/strategy.go IndicatorConfig.EnableATR + ATRPeriods default [14].',
    range: 'bool + comma periods',
    systemDefault:
      'ON in the new-strategy template (store/strategy.go GetDefaultStrategyConfig → applyFuturesIndicatorDefaults), period [14]; an absent field reads off',
    recommended: 'Per strategy taste.',
    whenToTouch: 'To add/remove ATR.',
    perSession: 'No.',
  },
  {
    label: 'Bollinger Bands',
    where: 'Strategy → Indicators → BOLL toggle + period',
    what: 'Bollinger Bands block on/off. Default period [20]; the standard-deviation multiplier is fixed at 2.',
    trader: 'OFF removes BOLL from the AI indicator text.',
    consumer:
      'store/strategy.go IndicatorConfig.EnableBOLL + BOLLPeriods default [20] (multiplier fixed 2).',
    range: 'bool + comma periods',
    systemDefault:
      'off (also off in the new-strategy template); period [20] when on',
    recommended: 'Per strategy taste.',
    whenToTouch: 'To add/remove BOLL.',
    perSession: 'No.',
  },
  {
    label: 'Volume profile',
    where: 'Strategy → Indicators → Volume toggle',
    what: 'Volume profile block on/off.',
    trader: 'OFF removes the volume block from analysis.',
    consumer: 'store/strategy.go IndicatorConfig.EnableVolume.',
    range: 'bool',
    systemDefault:
      'ON in the new-strategy template (store/strategy.go GetDefaultStrategyConfig); an absent field reads off',
    recommended: 'Per strategy taste.',
    whenToTouch: 'To add/remove volume profile.',
    perSession: 'No.',
  },
  {
    label: 'Open interest',
    where: 'Strategy → Indicators → OI toggle',
    what: 'Open-interest block on/off.',
    trader: 'OFF removes OI from the AI indicator text.',
    consumer: 'store/strategy.go IndicatorConfig.EnableOI.',
    range: 'bool',
    systemDefault:
      'off (the new-strategy template turns it off: store/strategy.go applyFuturesIndicatorDefaults)',
    recommended: 'Per strategy taste.',
    whenToTouch: 'To add/remove OI.',
    perSession: 'No.',
  },
  {
    label: 'Funding rate',
    where: 'Strategy → Indicators → Funding toggle',
    what: 'Funding-rate block on/off. CME futures have no funding rate — the editor hides it on the futures path.',
    trader: 'OFF removes funding rate from the AI text (crypto only).',
    consumer: 'store/strategy.go IndicatorConfig.EnableFundingRate.',
    range: 'bool',
    systemDefault:
      'ON in the new-strategy template (store/strategy.go GetDefaultStrategyConfig) but inert on futures, where the editor hides it; an absent field reads off',
    recommended: 'Per strategy taste.',
    whenToTouch: 'To add/remove funding rate.',
    perSession: 'No.',
  },
  {
    label: 'SVP',
    where: 'Strategy → Indicators → SVP toggle',
    what: 'Session volume profile block. Default OFF. When ON, the futures AI prompt gains the POC / value-area (VAH/VAL) line.',
    trader: 'ON adds the SVP line to the futures prompt.',
    consumer:
      'store/strategy.go IndicatorConfig.EnableSVP (comment: futures AI prompt line, default OFF).',
    range: 'bool',
    systemDefault: 'OFF',
    recommended: 'OFF until you want volume-at-price context in the prompt.',
    whenToTouch: 'To add the SVP line to the futures prompt.',
    perSession: 'No.',
  },
  {
    label: 'Primary timeframe',
    where: 'Strategy → Indicators → Klines → Primary TF',
    what: 'The primary decision timeframe: "1m" | "3m" | "5m" | "15m" | "1h" | "4h".',
    trader: 'The main bar series the engine decides on.',
    consumer: 'store/strategy.go KlineConfig.PrimaryTimeframe.',
    range: '1m | 3m | 5m | 15m | 1h | 4h',
    systemDefault: 'per strategy',
    recommended: 'Match your trading horizon.',
    whenToTouch: 'To change the decision timeframe.',
    perSession: 'No.',
  },
  {
    label: 'Primary count',
    where: 'Strategy → Indicators → Klines → Primary count',
    what: 'How many primary-timeframe klines feed analysis.',
    trader: 'Fewer bars = less context, more bars = longer prompts.',
    consumer: 'store/strategy.go KlineConfig.PrimaryCount.',
    range: 'positive int',
    systemDefault: 'per strategy',
    recommended: 'Per strategy taste.',
    whenToTouch: 'To widen/narrow the analysis window.',
    perSession: 'No.',
  },
  {
    label: 'Multi-timeframe',
    where: 'Strategy → Indicators → Klines → Multi-TF toggle + list',
    what: 'Whether multi-timeframe analysis is enabled, and the selected timeframe list (supports multi-selection).',
    trader: 'OFF = primary TF only.',
    consumer:
      'store/strategy.go KlineConfig.EnableMultiTimeframe / SelectedTimeframes.',
    range: 'bool + list',
    systemDefault: 'off',
    recommended: 'Per strategy taste.',
    whenToTouch: 'To add/remove higher timeframes.',
    perSession: 'No.',
  },
]

const grid: KnobSpec[] = [
  {
    label: 'Trading pair',
    where: 'Strategy → Grid → Symbol',
    what: 'The grid trading pair symbol.',
    trader: 'The market the grid engine trades.',
    consumer: 'store/strategy.go GridStrategyConfig.Symbol.',
    range: 'symbol string',
    systemDefault: 'empty',
    recommended: 'A liquid pair.',
    whenToTouch: 'To change the grid market.',
    perSession: 'No.',
  },
  {
    label: 'Grid count',
    where: 'Strategy → Grid → Grid Count',
    what: 'Number of grid levels (5-50).',
    trader: 'More levels = finer spacing, smaller per-level size.',
    consumer: 'store/strategy.go GridStrategyConfig.GridCount.',
    range: '5-50',
    systemDefault: 'per strategy',
    recommended: 'Per volatility taste.',
    whenToTouch: 'To change grid granularity.',
    perSession: 'No.',
  },
  {
    label: 'Total investment',
    where: 'Strategy → Grid → Investment',
    what: 'Total investment for the grid.',
    trader: 'The whole grid is sized from this number.',
    consumer: 'store/strategy.go GridStrategyConfig.TotalInvestment.',
    range: 'amount',
    systemDefault: '0',
    recommended: 'Only what you can afford to grid.',
    whenToTouch: 'To resize the grid.',
    perSession: 'No.',
  },
  {
    label: 'Leverage',
    where: 'Strategy → Grid → Leverage',
    what: 'Grid leverage (1-20).',
    trader: 'Exchange leverage applied to grid positions.',
    consumer: 'store/strategy.go GridStrategyConfig.Leverage (comment: 1-20).',
    range: '1-20',
    systemDefault: 'per strategy',
    recommended: 'Low for grids.',
    whenToTouch: 'To change grid leverage.',
    perSession: 'No.',
  },
  {
    label: 'Upper price',
    where: 'Strategy → Grid → Upper Price',
    what: 'Upper price boundary. 0 = auto-calculate from ATR.',
    trader: 'Grid ceiling.',
    consumer: 'store/strategy.go GridStrategyConfig.UpperPrice.',
    range: 'price or 0',
    systemDefault: '0 (ATR auto)',
    recommended: 'Auto unless you want a hard band.',
    whenToTouch: 'To pin a hard upper band.',
    perSession: 'No.',
  },
  {
    label: 'Lower price',
    where: 'Strategy → Grid → Lower Price',
    what: 'Lower price boundary. 0 = auto-calculate from ATR.',
    trader: 'Grid floor.',
    consumer: 'store/strategy.go GridStrategyConfig.LowerPrice.',
    range: 'price or 0',
    systemDefault: '0 (ATR auto)',
    recommended: 'Auto unless you want a hard band.',
    whenToTouch: 'To pin a hard lower band.',
    perSession: 'No.',
  },
  {
    label: 'Use ATR bounds',
    where: 'Strategy → Grid → Auto-calculate Bounds (ATR)',
    what: 'Whether to use ATR to auto-calculate the grid bounds.',
    trader: 'ON computes upper/lower from ATR.',
    consumer: 'store/strategy.go GridStrategyConfig.UseATRBounds.',
    range: 'bool',
    systemDefault: 'false',
    recommended: 'ON for volatility-adaptive grids.',
    whenToTouch: 'To switch between ATR and manual bounds.',
    perSession: 'No.',
  },
  {
    label: 'ATR multiplier',
    where: 'Strategy → Grid → ATR Multiplier',
    what: 'ATR multiplier for bound calculation (default 2.0).',
    trader: 'Wider multiplier = wider grid band.',
    consumer: 'store/strategy.go GridStrategyConfig.ATRMultiplier.',
    range: 'float',
    systemDefault: '2.0',
    recommended: '2.0 — the shipped default.',
    whenToTouch: 'To widen/narrow ATR bounds.',
    perSession: 'No.',
  },
  {
    label: 'Distribution',
    where: 'Strategy → Grid → Distribution',
    what: 'Position distribution across grid levels: "uniform" | "gaussian" | "pyramid".',
    trader: 'Shapes per-level sizing.',
    consumer: 'store/strategy.go GridStrategyConfig.Distribution.',
    range: 'uniform | gaussian | pyramid',
    systemDefault: 'per strategy',
    recommended: 'Per strategy taste.',
    whenToTouch: 'To reshape level sizing.',
    perSession: 'No.',
  },
  {
    label: 'Max drawdown %',
    where: 'Strategy → Grid → Max Drawdown (%)',
    what: 'Maximum drawdown percentage before emergency exit.',
    trader: 'Triggers the grid emergency exit.',
    consumer: 'store/strategy.go GridStrategyConfig.MaxDrawdownPct.',
    range: 'percent',
    systemDefault: '0',
    recommended: 'Set a real stop for the grid.',
    whenToTouch: 'To set the emergency exit.',
    perSession: 'No.',
  },
  {
    label: 'Stop loss %',
    where: 'Strategy → Grid → Stop Loss (%)',
    what: 'Stop loss percentage per position.',
    trader: 'Per-position grid stop.',
    consumer: 'store/strategy.go GridStrategyConfig.StopLossPct.',
    range: 'percent',
    systemDefault: '0',
    recommended: 'Per strategy taste.',
    whenToTouch: 'To set the per-position stop.',
    perSession: 'No.',
  },
  {
    label: 'Daily loss limit %',
    where: 'Strategy → Grid → Daily Loss Limit (%)',
    what: 'Daily loss limit percentage.',
    trader: 'Halts the grid on a daily loss.',
    consumer: 'store/strategy.go GridStrategyConfig.DailyLossLimitPct.',
    range: 'percent',
    systemDefault: '0',
    recommended: 'Per strategy taste.',
    whenToTouch: 'To set the daily loss cap.',
    perSession: 'No.',
  },
  {
    label: 'Maker only',
    where: 'Strategy → Grid → Maker Only Orders',
    what: 'Use maker-only orders for lower fees.',
    trader: 'ON = limit orders only.',
    consumer: 'store/strategy.go GridStrategyConfig.UseMakerOnly.',
    range: 'bool',
    systemDefault: 'false',
    recommended: 'Per fee preference.',
    whenToTouch: 'To force maker orders.',
    perSession: 'No.',
  },
  {
    label: 'Direction auto-adjust',
    where: 'Strategy → Grid → Enable Direction Adjust',
    what: 'Enable automatic grid direction adjustment based on box breakouts.',
    trader: 'ON re-biases the grid on breakouts.',
    consumer: 'store/strategy.go GridStrategyConfig.EnableDirectionAdjust.',
    range: 'bool',
    systemDefault: 'false',
    recommended: 'Per strategy taste.',
    whenToTouch: 'To let the grid follow breakouts.',
    perSession: 'No.',
  },
  {
    label: 'Direction bias ratio',
    where: 'Strategy → Grid → Bias Strength',
    what: 'Direction bias ratio for long_bias/short_bias modes (default 0.7 = 70%/30%).',
    trader: 'Shapes the long/short split when bias is active.',
    consumer: 'store/strategy.go GridStrategyConfig.DirectionBiasRatio.',
    range: '0-1',
    systemDefault: '0.7',
    recommended: '0.7 (70/30).',
    whenToTouch: 'To reweight the bias split.',
    perSession: 'No.',
  },
]

const pictureHtf: KnobSpec[] = [
  {
    label: 'Picture HTF — tick size',
    where: 'Strategy → Day Plan → Picture HTF → Tick size',
    what: 'One instrument tick (default 0.25 for MNQ); the H1 close must cross by at least one tick.',
    trader: 'Guards the H1 confirmation crossing.',
    consumer:
      'store/strategy.go PictureHtfConfig.TickSize (tick_size; PictureHtfResolved).',
    range: 'float',
    systemDefault: '0.25 (MNQ)',
    recommended: 'The instrument tick.',
    whenToTouch: 'Never unless the instrument changes.',
    perSession: 'No.',
  },
  {
    label: 'Picture HTF — pivot window',
    where: 'Strategy → Day Plan → Picture HTF → Pivot window',
    what: 'Latest N completed 4H candles for body-pivot discovery (default 120).',
    trader: 'Wider = more pivot context.',
    consumer:
      'store/strategy.go PictureHtfConfig.PivotWindow (pivot_window; PictureHtfResolved).',
    range: 'int',
    systemDefault: '120',
    recommended: '120 — the shipped default.',
    whenToTouch: 'To widen/narrow pivot discovery.',
    perSession: 'No.',
  },
  {
    label: 'Picture HTF — swing lookback',
    where: 'Strategy → Day Plan → Picture HTF → Swing lookback',
    what: 'Preceding N completed 5m candles for the structural swing stop (default 24).',
    trader: 'Shapes the structural swing stop.',
    consumer:
      'store/strategy.go PictureHtfConfig.SwingLookback (swing_lookback; PictureHtfResolved).',
    range: 'int',
    systemDefault: '24',
    recommended: '24 — the shipped default.',
    whenToTouch: 'To widen/narrow the swing stop.',
    perSession: 'No.',
  },
  {
    label: 'Picture HTF — entry window',
    where: 'Strategy → Day Plan → Picture HTF → Entry window',
    what: 'Submission allowed within N seconds of the new 5m interval (default 360).',
    trader: 'Limits when a Picture entry may be sent.',
    consumer:
      'store/strategy.go PictureHtfConfig.EntryWindowSec (entry_window_sec; PictureHtfDefaultEntryWindowSec = 360).',
    range: 'seconds',
    systemDefault: '360',
    recommended: '360 — the shipped default.',
    whenToTouch: 'To tighten/loosen the submission window.',
    perSession: 'No.',
  },
  {
    label: 'Picture HTF — freshness limit',
    where: 'Strategy → Day Plan → Picture HTF → Freshness limit',
    what: 'Max accepted age of the freshest bar frame at evaluation and send (default 30s).',
    trader: 'Stale frames are refused.',
    consumer:
      'store/strategy.go PictureHtfConfig.FreshnessSec (freshness_sec; PictureHtfDefaultFreshnessSec = 30).',
    range: 'seconds',
    systemDefault: '30',
    recommended: '30 — the shipped default.',
    whenToTouch: 'To tighten/loosen data freshness.',
    perSession: 'No.',
  },
  {
    label: 'Picture HTF — minimum R:R',
    where: 'Strategy → Day Plan → Picture HTF → Minimum R:R',
    what: "0 = the strategy's configured minimum R:R; the nearest eligible zone is never skipped to find a better one.",
    trader: 'R:R floor for Picture plays.',
    consumer:
      'store/strategy.go PictureHtfConfig.MinRR (min_rr; PictureHtfResolved).',
    range: 'float or 0',
    systemDefault: '0 (inherit risk-control floor)',
    recommended: '0 (inherit).',
    whenToTouch: 'To set a stricter floor than risk control.',
    perSession: 'No.',
  },
]

const sessions: KnobSpec[] = [
  {
    label: 'Session overrides (ASIA / LONDON / NY)',
    where: 'Strategy → Day Plan → Sessions accordion',
    what: 'Per-session override rows: min grade, min scenario quality, max trades, plan mode, max re-plans (the acceptance-window override row was removed by W-KNOB-PRUNE 2026-09-18 — one rule exists; a stored override is read by nothing). Min grade, quality, max trades and plan mode are tri-state: inherit (blank) = the strategy-level row; an explicit value wins. Stored values that EQUAL the strategy level are auto-migrated to inherit. (min side levels REMOVED — owner ruling 2026-08-31: the per-side count concept is deleted.)',
    trader:
      'The current rows: min_grade B · min_scenario_quality C · max_trades 7/10/10 (ASIA/LONDON/NY) · plan_mode strict ×3 · max re-plans 4 (a stored acceptance 5m_close ×3 is inert). STRICT-12345 F-9 (release #25 note): 3 of 4 strict-planner losses on 10-14 (#649 #650 #652) were ASIA/LONDON; the switches are in Studio → Day plan → sessions (ASIA/LONDON enable + min_grade + max_trades — the spec default is min_grade A, max_trades 1). No saved value was changed by release #25.',
    consumer:
      'store/strategy.go DayPlanSessionOverride + DayPlanConfig.SessionOverride (per-session resolvers) · store/resolve_source.go ResolvePlanMode / ResolveReplanCap · trader/auto_trader_planconfig.go planModeFor',
    range:
      'per-session rows; the four tri-state knobs inherit (blank) = strategy value, explicit = override',
    systemDefault:
      'ASIA 16:30 read 17:00→02:00 · LONDON 01:30 02:00→08:30 · NY 08:00 08:30→14:45 (all EOD-flat) — reads moved to open−30 by owner ruling 2026-08-31; class 36: scheduled reads author during the halt/weekend from stored bars (preflight freshness check bypassed for scheduled classes only)',
    recommended:
      '⭐ keep the current rows — they ARE the deployed session map.',
    whenToTouch: 'Only with a deliberate session-thesis change.',
    perSession: 'N/A (they define it).',
  },
  {
    label: 'Cancel confirmation — report regime',
    where: 'Environment only (no Strategy-page row)',
    what: 'env CANCEL_CONFIRM_REQUIRE_REPORT (default OFF). OFF: a requested cancel is confirmed by the order’s absence from a fresh broker snapshot (2026-09-06 wave). ON: a cancel is DONE only when the AddOn reports the order Cancelled (or Filled, which is the filled path) for that order id — the positive per-order report. Until then the arm slot is BUSY (no re-place), a timeout prints one owner-visible CENSUS WARN naming every unconfirmed id, and cancels are re-requested up to the cap, never silently promoted.',
    trader:
      'The knob is the Mentor-mode gate (cancel/re-place every few candles). With it OFF the bot is byte-identical to today. An AddOn below build 2026-10-03-c1 fails closed while ON: no confirmation → no stop-entry placement.',
    consumer:
      'trader/cancel_confirm.go (cancelConfirmRequireReport · confirmPendingCancelsReport · slotReportBlock in armSlotGuard) · store/armed_orders.go (RecordCancelReport · ConfirmCancelByReport) · the AddOn echoes the order’s state on every cancel_order (VL_BUILD_ID 2026-10-03-c1)',
    range:
      'off | on · default off · companion envs CANCEL_CONFIRM_TIMEOUT_S (90) · CANCEL_REREQUEST_MAX (5)',
    systemDefault: 'off',
    recommended:
      '⭐ keep OFF for the AI path; the owner turns it ON before Mentor mode places its first live (SIM) stop entry.',
    whenToTouch:
      'Only with a deliberate move of the Mentor-mode gate — and do NOT turn ON until: (1) lost-report recovery works across every SIM account (SendCancelReport currently scans only the active account); (2) the slot refusal also raises the book-outage P0 with an uncertified or disconnected AddOn; (3) part-filled entries can never be cancelled by a report; (4) an operator path exists to clear a cancel_pending row that can never receive a report.',
    perSession: 'No.',
  },
]

export const settings: GuideSection = {
  id: 'settings',
  num: 7,
  title: 'Settings & Knobs',
  tagline:
    'Every knob on the Strategy page, what it really does, and who reads it.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    {
      kind: 'p',
      text: 'AgentBeta uses the authenticated user’s configured AI model for each conversation request. Another user’s request cannot replace that selection. If your account has no enabled model, configure one; it does not inherit another account’s credentials.',
    },
    {
      kind: 'p',
      text: 'Every knob card below names the engine consumer (file:line) that reads it — so you always know whether a slider is real or decorative. FE persists but NO production code reads: nothing here is in that category; the three that used to be (plan_mode, proximity_filter_atr, …) are wired now.',
    },
    { kind: 'h', text: 'What the status labels mean' },
    {
      kind: 'p',
      text: 'A knob can fail to matter in two different ways, and the Settings page keeps them apart on purpose. "No consumer" and "cannot take effect" are different findings, so they get different words — and neither of them says "dead". Nothing is removed from the registry on the strength of a grep.',
    },
    {
      kind: 'table',
      title: 'Registry statuses',
      head: ['Status', 'Renders as', 'What it means'],
      rows: [
        ['live', '(no label)', 'A consumer reads it and behaviour changes.'],
        [
          'ineffective',
          'read; does not take effect (reason)',
          'A consumer WAS found and it cannot gate — e.g. the value reaches prompt text only. The audit checked this one; the reason says what it found.',
        ],
        [
          'candidate-unverified',
          'no known reader — pending verification',
          'A field-level grep found no reader. That is not proof: a method-based reader would not appear in it. Since 2026-09-11 the method-level check is CODE (store/knob_method_readers_test.go): every candidate row is re-checked for accessor-method readers on every test run, and a row with one fails the build. The first run found seven — the six wake_on_* / wake_min_interval_min knobs and acceptance_rule — all live all along; they showed live with their call sites until W-KNOB-PRUNE (2026-09-18) collapsed the five wake toggles into wake_on_level_events and folded the interval and acceptance rule. The rows still listed here genuinely have no reader either way.',
        ],
        ['advisory', '—', 'Feeds prompt text only, never a gate.'],
        [
          'folded',
          'folded — no control; stored value honoured (reason)',
          'W-KNOB-PRUNE (owner ruling 2026-09-18): the Studio control is gone and the engine keeps the shipped default as a constant; the stored field stays readable, a saved non-default is honoured and logged once at trader load ("⚙ folded knob <name>=<value> honoured from stored config").',
        ],
        [
          'suspended',
          '—',
          'A standing ruling disables it; the value is preserved.',
        ],
        [
          'infra',
          '—',
          'Ports, paths, keys — not a trading knob. Never carries a value on the wire.',
        ],
      ],
    },
    { kind: 'h', text: 'What the ⚙ settings boot line counts' },
    {
      kind: 'p',
      text: 'schema= is the number of setting paths the bot actually SAVES — every key the strategy save writes, found by saving a fully filled-in config and reading the keys back, not by reading the Go struct tags. Since W1 (2026-09-23) that includes the ai_config.* blocks (risk_control, indicators, coin_source, prompt_sections, custom_prompt): they are stored under ai_config, and the old count skipped them entirely, so a new risk or indicator field could land with no classification and no ⚠ UNCLASSIFIED warning. The W1 build counted 167 paths where the old count read 75; the number on your boot line is the one that is true for the running binary. The boot line and the Settings page panel read the same enumeration, so they cannot disagree.',
    },
    {
      kind: 'p',
      text: 'env-shadows reads "n/a (not counted)": nothing counts which environment variables override a saved knob yet, so the line says so instead of printing a 0 nobody measured. The /api/config/resolved summary leaves env_shadows out for the same reason, and the Settings panel shows n/a.',
    },
    {
      kind: 'p',
      text: "Effective value · origin · scope (W1). Every Risk Control and Day Plan row in the Studio shows a chip under it such as 'eff 3 · saved value · strategy' (the per-session rows in the NY / ASIA / LONDON accordion read that session's answer; a row the server did not answer shows no chip, never a guessed one). The chips re-read after a successful Save. The min-confidence note 'unset/0 → default 60' and the Futures Risk panel's '≤ 10' / 'equity × 20' fallbacks are gone: the chip and the panel print the value the server resolved for the saved strategy, or n/a when it could not be read. EFFECTIVE is the value the running bot uses, computed on the server from the SAVED strategy with the same functions the bot calls — unsaved edits do not change it until you save. ORIGIN says where that value came from: saved value · schema default / shipped default · strategy value · session override · env NAME (a process environment variable) · clamp (…) (a range or ceiling cut it) · suspended (EXIT_MECHS_SUSPENDED) · backfilled default (filled in when the saved block was empty) · code constant (folded) (no control; a constant applies unless a value is stored). '— saved X not used' means you saved X and something else won. SCOPE says where to change it: strategy · session:NY / ASIA / LONDON · process env · venue:ninjatrader. 'n/a — no resolver registered' means the server has no production resolver for that field yet — it never guesses; the coverage count says how many rows are resolved. Secrets always read 'redacted'. Source: GET /api/strategies/:id/effective?session=NY. Known limit: the Studio's own save path still writes some defaults back as values (an unset min R:R is saved as 3, min confidence 0 as 60, max positions 0 as 1), so a strategy saved from the Studio shows those as 'saved value' — pinned by a test until the save path is fixed.",
    },
    {
      kind: 'code',
      title: 'boot line shape (the numbers are read at boot, never typed)',
      lines: [
        '⚙ settings: schema=<paths> classified=<rows> live=<n> ineffective=<n> candidate-unverified=<n> suspended=<n> advisory=<n> display-only=<n> infra=<n> folded=<n> · env-shadows=n/a (not counted)',
      ],
    },
    {
      kind: 'p',
      text: 'Most saved paths are still classified by their last name (min_risk_reward_ratio), because the registry is keyed that way. Where one last name means two different things, the registry carries the full path instead: the seven ai_config.indicators.external_data_sources.* fields read "ineffective" — nothing in the engine fetches external data — rather than borrowing the live "name" and "type" rows of unrelated settings.',
    },
    { kind: 'h', text: 'saved → resolved · source' },
    {
      kind: 'p',
      text: 'What you saved is not always what the engine uses. Each line shows the saved value, the value the engine will actually resolve, and the rule that produced the difference. "(unset)" never borrows the resolved value: a field you never set and a field you set to its default are different facts, and the line exists to keep them apart.',
    },
    {
      kind: 'code',
      title: 'GET /api/config/resolved?trader_id=…&session=NY',
      lines: [
        'risk_control.min_risk_reward_ratio   2 → 2 · saved value',
        'day_plan.plan_mode                   (unset) → advisory · shipped default',
        'regime.htf_veto                      (unset) → true · shipped default',
      ],
    },
    {
      kind: 'p',
      text: 'The source comes from the same resolver the engine calls (store/resolve_source.go), not a second copy in the page — so if a resolution rule changes, this line changes with it instead of quietly disagreeing.',
    },
    { kind: 'h', text: 'Day Plan knobs' },
    { kind: 'knobs', knobs: dayPlan },
    { kind: 'h', text: 'Folded and removed knobs (W-KNOB-PRUNE, 2026-09-18)' },
    {
      kind: 'p',
      text: 'Owner ruling 2026-09-18 on the Round 23/24 research verdicts: seven knobs removed, five folded, two dead fields deleted. A FOLDED knob has no Studio control; the engine uses the constant below unless the strategy already stores another value, which is honoured at read and logged once at trader load ("⚙ folded knob <name>=<value> honoured from stored config") — so no removal silently changed a value the owner had set. Every removal is pinned by a before/after golden at the shipped default (kernel/knob_prune_pin_test.go, trader/knob_prune_pin_test.go); the one deliberate behaviour change is the HTF weight.',
    },
    {
      kind: 'table',
      title: 'Fate of each knob',
      head: ['Knob', 'Fate', 'Now', 'Why (evidence)'],
      rows: [
        [
          'htf_score_multiplier',
          'REMOVED',
          'constant 1.0 (was 1.2)',
          'Round 23 Q-C: 1.2 promoted a group that holds LESS. Changes seating — 33 of 64 stage-A fixtures change membership; the identity fixture keeps every seat and its order.',
        ],
        [
          'seat_1h_zone',
          'REMOVED',
          '1h S/D seat guarantee unconditional (was ON, never stored)',
          'Round 24: no zone kind/TF beats random; the switch was never off.',
        ],
        [
          'levels_fresh_by_tf',
          'FOLDED',
          'OFF unless stored (both bound strategies store ON)',
          'S4c: freshness separates nothing; the stored ON is honoured until the owner clears it, then the grader is deleted.',
        ],
        [
          'structure_map',
          'FOLDED',
          'OFF unless stored (both bound strategies store ON)',
          'Advisory text only; keep OFF until Round 25.',
        ],
        [
          'wake_on_15m_zone · wake_on_htf_ob · wake_on_ifvg · wake_on_htf_zone · wake_on_seated_invalidation',
          'COLLAPSED',
          'one switch wake_on_level_events (ON); OBs only via a legacy stored wake_on_htf_ob=true (both bound strategies)',
          'Rounds 23/24 kept HTF zones + seated invalidation; the rest ride along at their shipped default; any legacy ON → ON.',
        ],
        [
          'wake_min_interval_min',
          'FOLDED',
          '30 unless stored',
          'The value stays; the control goes.',
        ],
        [
          'acceptance_rule',
          'FOLDED',
          'one rule, 1×5m close, always',
          'The dropdown had one option since the 2026-08-30 entry-mechanics addendum.',
        ],
        [
          'realign_cap',
          'FOLDED',
          '5 per plan unless stored (owner stores 10)',
          'Overlaps the re-plan section; the ⟳ Re-align budget keeps working.',
        ],
        [
          'evening_digest',
          'FOLDED',
          'OFF unless stored true (every live strategy stores true)',
          'Owner verdict: constant off unless stored on.',
        ],
        [
          'scenario_cap',
          'FOLDED',
          '3 unless stored (owner stores 5)',
          'Owner verdict.',
        ],
        [
          'last_entry_ct · eod_flat_ct',
          'DELETED',
          'field gone; old stored values ignored',
          'Unreachable in the clock since the P2 session-scope redesign (2026-08-18); the per-session offsets are the live clock.',
        ],
      ],
    },
    { kind: 'h', text: 'Risk Control knobs' },
    { kind: 'knobs', knobs: risk },
    { kind: 'h', text: 'AI prompt knobs' },
    { kind: 'knobs', knobs: prompt },
    { kind: 'knobs', knobs: coinSource },
    { kind: 'knobs', knobs: indicators },
    { kind: 'knobs', knobs: grid },
    { kind: 'knobs', knobs: pictureHtf },
    { kind: 'h', text: 'Session map' },
    { kind: 'knobs', knobs: sessions },
    { kind: 'h', text: 'Env-only knobs (not Studio)' },
    {
      kind: 'callout',
      title: 'Env-only knobs — .env only, never a Studio slider',
      items: [
        {
          title: 'ARM_MIN_RR — DELETED (no longer read)',
          body: 'There is no ARM_MIN_RR any more, env or code default (owner ruling 2026-09-03, R1 "one R:R floor"; trader/armed_executor.go). The resting-order seam and the decision path read the SAME floor: the bound strategy\'s min_risk_reward_ratio — 2 on both bound strategies since the 2026-09-01 08:13 CT save. 3.0 is only the fallback when the bound config has no value. Setting ARM_MIN_RR in .env does nothing.',
        },
        {
          title: 'HTF_VETO_MODE = cross',
          body: "Veto mode: 1h | cross | 4h — LIVE = cross (1h AND 4h must agree; the $352/0 autopsy). The 4h trend is read from the bar cache's native 4h series (F-4 — before it, cross never saw a 4h and never vetoed).",
        },
        {
          title: 'STRUCTURE_ONEWAY_ATR = 1.0',
          body: "HTF veto only (F-4): a TF whose 3-swing trend is RANGING reads TRENDING when its newest closed bar closed at least this many ATR beyond the last swing extreme (a crash that has not made a new fractal low yet). 1.0 = the repo's displacement unit for a broken level (BD_MIN_DISP_ATR). The boot regime ledger prints the resolved value.",
        },
        {
          title: 'HTF_VETO_TF = 1h',
          body: 'The veto timeframe when mode is 1h.',
        },
        {
          title: 'FAST_MARKET_ATR = 1.5',
          body: 'Wake-read fast threshold: |price drift| since the last write > K×ATR5m → fast re-plan.',
        },
        {
          title: 'FAST_MARKET_REASONING = fast',
          body: 'The reasoning wire for fast-market wake reads (FAST TAPE).',
        },
        {
          title: 'BD_MIN_DISP_ATR = 1.0',
          body: 'Breakdown/breakup displacement floor in ATR5m multiples.',
        },
        {
          title: 'FVG_ENTRY_MIN_DISP_ATR = 1.5',
          body: 'FVG displacement floor in ATR5m multiples.',
        },
        {
          title: 'INGEST_QUEUE_CAP = 4096',
          body: 'Bar-ingest queue depth (peak_depth is logged; 0 drops is the invariant).',
        },
        {
          title: 'AI_PLAN_MAX_TOKENS = 65536',
          body: 'Planner completion budget — truncation is a 🚨 WARN, never silent.',
        },
        {
          title:
            'RETENTION_DECISION_DAYS / RETENTION_EQUITY_DAYS / RETENTION_NT8_SNAPSHOT_DAYS / RETENTION_LEVEL_STATS_DAYS = 0',
          body: 'Table retention — WIRED but DISABLED (owner ruling 2026-09-26: "keep the database, we are testing"). 0 = keep forever; a value N runs the daily prune against decision_records / equity snapshots / NT8 order snapshots / level_stats. Trades, fills, receipts and plans are NEVER prunable. The boot line reads the resolved knobs and live row counts.',
        },
        {
          title: 'LOG_RETENTION_DAYS = 0',
          body: "Age-based prune of data/vl_YYYY-MM-DD.log at logger init, where the date is America/Chicago (CT) — the same zone every log line timestamp renders in, so file date and line times always agree on any machine clock. 0 = keep every file (the default — the owner has not ruled on logs). Never deletes today's or the running boot's file.",
        },
        {
          title: 'BARS_HOLE_MIN_GAP_MIN = 3',
          body: "Bar-hole threshold: this many OPEN-market minutes with no stored 1m bar between two stored bars is a hole, and the bot re-requests NinjaTrader's history for it at +1/+3/+10/+30 min (never while the feed is down), then every 15 min while CME is open (8 slow tries per hole in total) after one loud UNFILLED line naming the NT8 history reload; requests go out only in the first 15 s of a minute, and a hole older than NT8's 2000-bar replay reach is retired. The daily break, weekends and holiday closes never count. Only 1m is tracked: a re-request rebuilds every timeframe's history at once, so a 1m hole's refill also heals the 2m/5m bars. The boot line '🕳 bar-hole backfill:' prints the value and its source.",
        },
        {
          title: 'PERSIST_STALL_WATCHDOG_S = 60',
          body: 'Bar-persist silence alarm: no successful flush for N seconds while live bar frames are FLOWING → loud ERROR (the Friday ~2h GORM stall can never go silent again). Frame-aware: an idle wire (weekend, the daily break, NT8 closed) stays silent — no cry-wolf. At the 16:00 CT break and the weekend close the bot also writes the LAST bar(s) of the session from the ring (live if NT8 said final, live_partial if not; a better stored row is never overwritten), and a replay held across a restart inside the break is judged against the stored live rows (released if on the same price scale, discarded if not).',
        },
      ],
    },
    { kind: 'h', text: 'The save ritual' },
    {
      kind: 'p',
      text: 'Every Strategy-page change must be SAVED to take effect. Ritual: make the change → press Save → "Strategy saved" toast → the `saved {MM/DD, HH:MM} CT` chip updates. Unsaved changes are inert — and the knob-vs-code truth is: a value outside the code’s own range is clamped into it when the strategy is saved (store/strategy.go ClampLimits — e.g. min R:R 1–10, max positions 1–3).',
    },
    {
      kind: 'callout',
      title: 'knob-vs-code — the four patterns',
      items: [
        {
          title: 'Wired + clamped',
          body: 'Page value used; the code clamps a saved value into its own range (min R:R 1–10, max positions 1–3; crypto leverage 1–20, the same as its page sliders).',
          cite: 'store/strategy.go ClampLimits',
        },
        {
          title: 'Wired + per-session',
          body: 'Session override wins over strategy value (plan_mode, proximity, caps).',
          cite: 'store/strategy.go DayPlanConfig.SessionOverride',
        },
        {
          title: 'Inert without master',
          body: 'Guardrail rows do nothing while the master is OFF — the counters still show would-have-tripped.',
        },
        {
          title: 'Always-on',
          body: 'Max contracts + notional cap ignore the master entirely.',
        },
      ],
    },
    { kind: 'h', text: 'Condition shadow demotion (owner ruling 2026-08-31)' },
    {
      kind: 'callout',
      title: 'fvg_entry + breakout_retest are SHADOW — no orders, ever',
      items: [
        {
          title: 'fvg_entry — tested null, twice',
          body: 'An external study of ~40,000 fair-value gaps across ES, NQ, GC and SI (2019-2026, 1-minute base data) found the reaction is real — roughly 5 percentage points above a matched-random level, positive in 34 of 36 cells — but carries NO tradeable edge after honest costs. The apparent edge (win rate ~73%, profit factor ~2.4) was an intrabar look-ahead artifact: resolving exits on 1-minute data collapsed it to ~50% and ~1.0. The most-marketed 5m and 15m timeframes performed WORST. Our own forensics independently returned the same null. Descriptive reaction real ≠ tradeable edge — that distinction is the entire finding.',
        },
        {
          title:
            'breakout_retest — no evidence anywhere, plus one direct negative',
          body: 'It rests on role reversal ("broken support becomes resistance"), which has NEVER been rigorously quantified on ANY market in the published literature — an axiom in practitioner texts, defined-but-untested in curricula, with only anonymous vendor backtests of undisclosed methodology circulating. The MNQ-specific falsification study reports an 80.7% stop-out rate on pullback/retest entries after breakouts.',
        },
        {
          title: 'Enforcement site: the ARM SEAM — and why',
          body: 'The planner MAY still author them, the validator MAY still accept them, and E8 MUST still score them — that counterfactual data is the whole justification for shadowing instead of deleting. The arm executor is the single choke point that guarantees zero exposure: a shadowed scenario writes an inert "shadowed" ledger row, no order frame ever reaches NT8, and any resting order authored before the ruling is cancelled on the first cycle (reason condition_shadowed, counter arms_refused_shadowed).',
          cite: 'trader/armed_executor.go · kernel/condition_status.go',
        },
        {
          title: 'The knob',
          body: 'condition_status map, resolved per-condition: session override → strategy base → LIVE_CONDITIONS → SHADOW_CONDITIONS → defaults. A condition named in BOTH env lists resolves LIVE (LIVE_CONDITIONS outranks SHADOW_CONDITIONS whatever order they are written in); a strategy or session setting outranks both env lists. Defaults this wave: fvg_entry = shadow, breakout_retest = shadow, all others = live. sweep_reclaim is NOT shadowed (docketed for the Sep-9 court, pre-registered criterion, do not touch).',
        },
        {
          title:
            'Pre-registered promotion criterion (fix this now, never loosen it later)',
          body: "A shadowed condition returns to LIVE only if, at n ≥ 30 shadow setups on our own tape, its net-of-friction expectancy LOWER CONFIDENCE BOUND exceeds zero. Otherwise it remains shadowed, or is deleted at the court's discretion. No promotion on narrative. No promotion on a good week. No promotion because the model likes authoring it. No promotion on a point estimate without its interval.",
        },
      ],
    },
    { kind: 'h', text: 'Auth & API hardening (FIX-SEC, 2026-09-26)' },
    {
      kind: 'p',
      text: 'Logout blacklists the session in the database now, not just in memory — a restart can no longer revive a logged-out token for the rest of its 24-hour life (fingerprints only; the token itself is never stored). Expired revocations are pruned at boot and on every new logout.',
    },
    {
      kind: 'p',
      text: 'Login is rate-limited per IP and per account: five failed attempts within five minutes arm a one-minute block, ten arm a fifteen-minute block, and the 429 names Retry-After. The unknown-email path burns the same password cost as the known one, so response time no longer reveals whether an account exists. A successful login resets both counters.',
    },
    {
      kind: 'p',
      text: 'Telegram binding now needs a one-time code: in the dashboard (owner session) open the Telegram bind dialog, copy the 8-character code, and send /bind <code> from the chat. The first /start no longer binds blindly; the code is single-use and expires after 10 minutes.',
    },
    {
      kind: 'p',
      text: 'Owner-only routes (today: the Telegram bind code) accept only the box OWNER’s own session, never a machine token. Machine tokens (the Telegram bot’s, gate-jwt’s) may only read and reduce risk — stop, pause, close a position, flatten; every other change answers 403 “the bot may read and reduce risk, not add it” (see Updates → Changing the password). The owner is the account that owns every trader — not simply the first account registered; the first account counts only while there are no traders. When the traders belong to more than one account, or to one that no longer exists, there is no owner and those routes refuse (logged and counted as owner_unresolved in the gate-block table). The 👤 owner boot line names the owner and the rule that chose it. The boot line now prints the JWT secret state truthfully: configured (custom) or INSECURE DEFAULT with a warning.',
    },
  ],
}
