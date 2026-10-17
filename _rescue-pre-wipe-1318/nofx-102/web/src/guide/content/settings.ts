import { GUIDE_BUILT_REV, type GuideSection, type KnobSpec } from '../types'

// Knob inventory = the live Strategy page controls (census 2026-08-27).
// "trader" is the one-sentence trader explanation; "consumer" is the engine
// consumer (file:line, verified). Every field is mandatory and linted by test.

const dayPlan: KnobSpec[] = [
  {
    label: 'Plan mode',
    where: 'Strategy → Day Plan → top',
    what: 'How the plan constrains entries: ADVISORY informs · DIRECTION blocks against-bias entries · STRICT blocks anything not citing an armed scenario (and ALL entries with no plan).',
    trader:
      'Strict means no-plan = flat day — the plan is the law, not advice.',
    consumer:
      'store/resolve_source.go ResolvePlanMode — the one resolver (session override → strategy → advisory) · entry points store/strategy.go PlanModeFor and trader/auto_trader_planconfig.go planModeFor · direction block trader/auto_trader_planconfig.go planModeBlocked',
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
    where: 'Strategy → Day Plan → One setup (switch) + min grade (A/B/C)',
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
      'Only to restore the wide book for a comparison; the boot line names the switch and its source.',
    perSession: 'No.',
  },
  {
    label: 'Structure table (S1)',
    where:
      'Strategy → Day Plan → structure_map (editor toggle: Structure map; default OFF)',
    what: "A SECOND table the planner reads, above the entry table: D / 4h / 1h direction from the last three labelled swings (HH/HL → up, LH/LL → down, mixed → range), the last swing high/low, the last impulse and where price sits in it (pd 0..1), plus each timeframe's top 4–6 zones with their labels INTACT. Bias only — never an entry; entries still come only from the ranked 12-seat table. Stamped on the plan doc as `structure` when computed; absent otherwise.",
    trader:
      "OFF by default (unset reads OFF — every S1 knob does; nothing changes the live plan until the S4 measurement). ON adds a '## STRUCTURE — bias only, not entries' section before the level table; OFF leaves the prompt byte-identical (the existing goldens prove it). The validator is untouched (that is S3).",
    consumer:
      'store/resolve_source.go ResolveStructureMap · trader/structure_map_wire.go structureMapForRead · kernel/structure_map.go ComputeStructureMap / RenderStructureSection',
    range: 'true / false',
    systemDefault: 'OFF (nil)',
    recommended:
      "OFF until DS-R's S4 measures what the structure section changes in the plans; then the owner's call.",
    whenToTouch:
      "Only for the measured comparison. Boot line '🗺 structure: off|on(D/4h/1h)|n/a' names the knob; each read logs '🗺 structure @<session>: D=… 4h=… 1h=… zones=<n> pd4h=<0.xx>'.",
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
      'kernel/levels_score.go:389 (ScoreLevels proximityK) · trader/auto_trader_planconfig.go:47',
    range: '0.1 – 3.0 (× daily-range proxy, clamped)',
    systemDefault:
      '1.5 (the code default AND, as of 2026-09-03, the resolved live value — the saved config reads 1.5, so the 0.3 retune this card described is no longer in effect)',
    recommended:
      '⭐ 1.5 — the RESOLVED live value, read from the saved strategy config 2026-09-03 (day_plan.proximity_filter_atr = 1.5), NOT the 0.3 this card claimed since 2026-08-28. Band = K × dATR (the daily-range proxy) on BOTH the bot gate and the engine path; kernel.ResolveProximityK clamps to 0.1–3.0 and falls back to ActivationWindowK 1.5.',
    whenToTouch:
      'If the card looks too crowded or too empty for the daily range.',
    perSession:
      'Yes — session override wins; inherit (blank) = the strategy-level value.',
  },
  {
    label: 'Max levels',
    where: 'Strategy → Day Plan → Max levels 3–12',
    what: "Cap on rows in the card's levels table (the planner is asked to write within the resolved cap).",
    trader:
      '8 = a table the AI can copy without hallucinating; 12 = wider map but more copy risk.',
    consumer:
      'kernel/levels_score.go:54 DefaultMaxLevels · trader/auto_trader_planner.go:592 (maxLevels param)',
    range: '3 – 12',
    systemDefault: '8 (owner)',
    recommended: '⭐ 8 — shipped default, verified copy fidelity.',
    whenToTouch:
      'Leave alone; raise only if you find the card missing actionable levels.',
    perSession: 'Yes.',
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
    systemDefault: 'unset (legacy, byte-identical to pre-S3)',
    recommended:
      '⭐ leave UNSET until the S4 measurement decides whether HTF promotion helps.',
    whenToTouch:
      'Only after the S4 structure-gate report; then set a value and watch the seated table.',
    perSession: 'No.',
  },
  {
    label: 'HTF score multiplier (S3)',
    where: 'Strategy → Day Plan → HTF score multiplier 1.0–1.5',
    what: 'The weight applied to a higher-timeframe level\u2019s score. Q-C measured that 1.2 promotes a group that holds LESS.',
    trader:
      'UNSET = 1.2 (today\u2019s const, byte-identical scores). SAVED 1.0 = no HTF premium. The boot line reads the resolved value with its source.',
    consumer:
      'kernel/levels_score.go scoreLevelsPool (htfMult) · ResolveHtfScoreMultiplier',
    range: '1.0 – 1.5 · unset = 1.2',
    systemDefault: '1.2 (unset)',
    recommended:
      '⭐ hold at 1.2 until the S4 final; the owner may set 1.0 after.',
    whenToTouch: 'After S4 final, if the HTF premium is measured as a cost.',
    perSession: 'No.',
  },
  {
    label: 'Max scenarios',
    where: 'Strategy → Day Plan → Max scenarios 1–5',
    what: 'Cap on S# rows the planner may write.',
    trader:
      '3 = focused plays; 5 = kitchen sink, but every S# still needs a condition + invalid line.',
    consumer:
      'trader/auto_trader_planconfig.go:162 (scenarioCap) · kernel/planner_prompt.go:66 (resolved caps)',
    range: '1 – 5',
    systemDefault: '5 (owner, live)',
    recommended:
      '⭐ 5 — the live scenario cap; 3 stays a fine default elsewhere.',
    whenToTouch: 'Raise if a fast market needs more play variants.',
    perSession: 'Yes.',
  },
  {
    label: 'Max re-plans',
    where: 'Strategy → Day Plan → Max re-plans 0–4',
    what: "Re-read budget per session — a RECORDED counter (class 35): only death re-plans and owner re-reads (↻) spend it. Level-event / MSS wake reads (fast-market included), dormant flips + re-arms, the session's scheduled read, owner reset and fail-closed markers are FREE and never count. Budget exhausted = NO-TRADE terminal marker (⛔).",
    trader:
      'The v6-after-cap-4 confusion: the last chip IS the no-trade marker, not a real plan. And a chain can legitimately be v6 with the FULL budget left (2026-09-01 LONDON: six rows, zero spends) — the card\'s "re-reads left" is the recorded number, not version−1.',
    consumer:
      'store/strategy.go (ReplanCap · GetReplanBudget/SpendReplan) · trader/auto_trader_planner.go (deathReplanAllowed → runDeathReplan) · trader/auto_trader_reread.go (owner re-read gate)',
    range: '0 – 4 per session',
    systemDefault: '2 (owner)',
    recommended: '⭐ 2 — one re-read after an early death, then sit out.',
    whenToTouch:
      "Raise for violent trend days where one death shouldn't end the session.",
    perSession:
      'Yes — session override wins; inherit (blank) = the strategy-level value (ReplanCapFor).',
  },
  {
    label: 'Acceptance window',
    where: 'Strategy → Day Plan → Acceptance rule',
    what: 'The confirm clock for acceptance-type plays.',
    trader: '5m_close = tight confirm; 15m = patient.',
    consumer: 'trader/auto_trader_planconfig.go:168 (acceptanceFor)',
    range: '5m_close | 15m',
    systemDefault: '5m_close',
    recommended: '⭐ 5m_close — current config.',
    whenToTouch: 'If acceptance plays keep getting cut by the confirm clock.',
    perSession:
      'Yes — session override wins; inherit (blank) = the strategy-level value.',
  },
  {
    label: 'Require approval',
    where: 'Strategy → Day Plan → toggle',
    what: 'ON = entries are HELD until the owner taps Approve for this CME session-day.',
    trader: 'You are the gate: no approve, no entries, even on-plan.',
    consumer:
      'trader/auto_trader_orders.go:297 (approval gate) · api/handler_plan.go:129 (approvalRequired)',
    range: 'ON | OFF',
    systemDefault: 'OFF (fully automatic)',
    recommended: '⭐ OFF — SIM phase; ON is the rehearsal for live.',
    whenToTouch:
      'Turn ON to practice the live approval muscle before going live.',
    perSession: 'Yes.',
  },
  {
    label: 'Evening digest',
    where: 'Strategy → Day Plan → toggle',
    what: "A session-close summary of the day's plan, fills and gate-blocks.",
    trader: 'The daily post-mortem in chat form.',
    consumer: 'trader/auto_trader_planconfig.go:13 (evening_digest)',
    range: 'ON | OFF',
    systemDefault: 'OFF',
    recommended: '⭐ ON if you want the 14:45 wrap-up in the chat.',
    whenToTouch: 'Whenever you want more or less noise.',
    perSession: 'No — strategy-level.',
  },
  {
    label: 'Re-align cap',
    where: 'Strategy → Day Plan → Re-align cap 0–10',
    what: 'Budget of planner re-alignments per session after owner level edits.',
    trader: 'Each Apply merge costs one; decline costs nothing.',
    consumer:
      'api/handler_plan.go:1906 (realign endpoint) · store/strategy.go (RealignCap)',
    range: '0 – 10',
    systemDefault: '5 (owner)',
    recommended: '⭐ 5 — enough for a hands-on day.',
    whenToTouch: 'If you edit levels often, keep it ≥3.',
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
    label: 'Min scenario quality',
    where: 'Strategy → Day Plan → A/B/C',
    what: 'Lowest grade the planner may write. Judged against the min_scenario_quality floor that MinScenarioQualityFor (store/strategy.go) resolves per session; at the floor it resolves to today nothing is refused for quality, and raising it makes the arm-time gate refuse a below-floor scenario.',
    trader: 'C = full palette; B/A = the planner filters its own plays.',
    consumer:
      'trader/auto_trader_planner.go:592 (AssembleScoredLevelsMinGrade)',
    range: 'A | B | C',
    systemDefault: 'C',
    recommended: "⭐ C — grade is advisory; don't hide plays with it.",
    whenToTouch: 'Only if the card gets cluttered with junk scenarios.',
    perSession:
      'Yes — session override wins; inherit (blank) = the strategy-level row above.',
  },
  {
    label: '1h anchor seat',
    where: 'Strategy → Day Plan → toggle',
    what: 'Seat 1h/4h anchor levels in the card (the HTF context rows).',
    trader: 'ON = the plan sees the bigger map; the 1h/4h floors (B) apply.',
    consumer: 'trader/auto_trader_dayplan.go:53 (seat_1h_zone)',
    range: 'ON | OFF',
    systemDefault: 'ON (owner)',
    recommended: '⭐ ON — the HTF floors only exist when seated.',
    whenToTouch: 'Turn OFF only for pure 5m/15m scalping studies.',
    perSession: 'Yes.',
  },
  {
    label: 'HTF freshness by own timeframe',
    where: 'Strategy → Day Plan → toggle (levels_fresh_by_tf)',
    what: 'OFF = an HTF level decays on the 1m-touch ladder (one 1m bar into the zone marks it tested). ON = an HTF level (1h…1w) grades its freshness on ITS OWN timeframe bars only — a 4h zone is tested by a 4h bar trading into it, not by 1m noise. Grades: fresh / tested-1 / tested-2 / stale by test count.',
    trader:
      'OFF = today\u2019s scoring byte-identical. ON = HTF levels keep their seats longer; the scoring ladders (freshMult/zoneFreshMult) are unchanged.',
    consumer:
      'trader/auto_trader_dayplan.go (installLevelStateProvider → kernel.LevelFreshnessByTF)',
    range: 'ON | OFF',
    systemDefault: 'OFF',
    recommended:
      '⏳ OFF until the S4 measurement gate (round 23 structure gate) reports whether by-TF grading helps.',
    whenToTouch:
      'Only after S4 ships its report; the owner flips it in Studio.',
    perSession: 'No.',
  },
  {
    label: 'Flip re-read (W-FLIP-REREAD)',
    where: 'Strategy → Day Plan → flip_reread toggle',
    what: "When the plan's flip condition fires, the plan ALWAYS goes dormant first (wick-noise protection — unchanged). ON adds ONE free planner re-read in the flipped direction (trigger structure_flip, class-35 free like a level wake, same preflight and wake cadence). OFF = today's behaviour: the plan sleeps and the flipped bias is never authored.",
    trader:
      'ON = the flipped bias can actually materialize; the model still authors the new plan and the validator still judges it. If the new plan keeps the old bias, it is logged (the model disagreed) — never a loop: one re-read per fired flip.',
    consumer:
      'trader/auto_trader_planner.go maybeRereadAfterFlip · store.DayPlanConfig.FlipRereadEnabled',
    range: 'ON | OFF',
    systemDefault: 'OFF (legacy dormant)',
    recommended: '⭐ OFF until you have watched one flip the old way; then ON and compare.',
    whenToTouch: 'Turn ON when you want a fired flip to re-read rather than sleep.',
    perSession: 'No.',
  },
  {
    label: 'Wake triggers (5 toggles)',
    where: 'Strategy → Day Plan → Wake triggers',
    what: 'The five event classes that wake the planner mid-session: fresh S/D zones, HTF events, 15m events, invalidation, level-touch waves (the W6 wake wave).',
    trader:
      'More ON = the plan reacts to structure as it forms; wakes are advisory refreshes that can never dark a session.',
    consumer:
      'trader/auto_trader_wake_levels.go:17 (wake wave) · maybeRunSessionReadsAt',
    range: '5 × ON | OFF',
    systemDefault: 'ON (the event-diff wave, 2026-08-25)',
    recommended: '⭐ leave ON — deaths still re-plan; wakes only refine.',
    whenToTouch:
      'Disable a class only if it fires too often for a quiet session.',
    perSession: 'Yes.',
  },
  {
    label: 'Min wake interval',
    where: 'Strategy → Day Plan → Min wake interval',
    what: 'The false-alarm/detection-delay knob between wake reads (minutes).',
    trader: 'Lower = jumpier plan, higher = misses fast structure.',
    consumer: 'trader/auto_trader_wake_levels.go:23 (wake_min_interval_min)',
    range: '5 – 120 minutes',
    systemDefault: '30',
    recommended: '⭐ 30 — current config.',
    whenToTouch: 'Lower for news-heavy days, raise for grind days.',
    perSession: 'Yes.',
  },
]

const risk: KnobSpec[] = [
  {
    label: 'Min confidence',
    where: 'Strategy → Risk Control',
    what: "Floor on the AI's confidence integer; below = refused.",
    trader: 'The simplest honesty gate: 60 means "be at least 60% sure".',
    consumer: 'kernel/engine_position.go:188 (confidence gate)',
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
    consumer: 'kernel/engine_analysis.go:125 (max_positions)',
    range: '1 – 3',
    systemDefault: '3 (owner)',
    recommended: '⭐ 3 — matches config; MNQ SIM never needs the extra legs.',
    whenToTouch: 'Set 1 for single-position discipline.',
    perSession: 'No.',
  },
  {
    label: 'Leverage BTC/ETH / alt',
    where: 'Strategy → Risk Control',
    what: 'Leverage multiplier per coin class for futures sizing.',
    trader:
      'Code-enforced ceiling is 10/5 (system) even if the page shows up to 20/20.',
    consumer: 'kernel/engine_analysis.go (btcEthLeverage/altcoinLeverage)',
    range: '1 – 20 (page) · code-enforced ≤10 BTC/ETH, ≤5 alt',
    systemDefault: '5 / 5 (owner) · system duality 10/5',
    recommended: '⭐ 5/5 — current config.',
    whenToTouch: 'Lower to derisk; page values above 10/5 are inert.',
    perSession: 'No.',
  },
  {
    label: 'Min risk:reward',
    where: 'Strategy → Risk Control',
    what: 'The R:R floor every entry must clear (computed on real stop/target).',
    trader: 'Raising this is the single strongest filter on bad entries.',
    consumer: 'kernel/engine_position.go:122 (validateDecisions minRiskReward)',
    range: '1 – 10 (step 0.5)',
    systemDefault: '3 (owner)',
    recommended: '⭐ 3 — current config; 4+ measurably cuts entry count.',
    whenToTouch: 'Raise to 4+ if wins are too small to cover losers.',
    perSession: 'No.',
  },
  {
    label: 'Max margin',
    where: 'Strategy → Risk Control',
    what: 'Margin ceiling per position (AI-guided).',
    trader: '90 keeps a single position from eating the account.',
    consumer: 'kernel/engine_analysis.go:529 (riskConfig.MaxMargin)',
    range: 'AI-guided (page) · default 90',
    systemDefault: '90 (owner)',
    recommended: '⭐ 90 — current config.',
    whenToTouch: 'Lower in high-vol regimes.',
    perSession: 'No.',
  },
  {
    label: 'Min position size',
    where: 'Strategy → Risk Control',
    what: 'Smallest position notional/contract count allowed.',
    trader: 'Below 12 the economics of the trade stop making sense.',
    consumer: 'kernel/engine_analysis.go:530 (riskConfig.MinPosition)',
    range: 'page numeric · default 12',
    systemDefault: '12 (owner)',
    recommended: '⭐ 12 — current config.',
    whenToTouch: 'Leave alone in SIM.',
    perSession: 'No.',
  },
  {
    label: 'Hold lock',
    where: 'Strategy → Risk Control',
    what: 'Lock positions against early exit until the hold condition clears.',
    trader: 'Stops you (and the AI) from cutting winners early.',
    consumer: 'kernel/engine_position.go (hold lock path)',
    range: 'ON | OFF',
    systemDefault: 'OFF',
    recommended: '⭐ OFF — the plan already manages exit timing.',
    whenToTouch: "ON if exits keep firing before the plan's own criteria.",
    perSession: 'No.',
  },
  {
    label: 'Wake cadence (class 47)',
    where: 'env WAKE_CUTOFF_MIN · WAKE_COOLDOWN_MIN (no Studio row yet)',
    what: 'Two OBSERVATIONS on level-event wakes, both WARN-first — nothing is suppressed. CUTOFF: a wake starting within WAKE_CUTOFF_MIN of the session flat logs "would_skip: <n> min to flat" and still runs (25m = the 15m last-entry cutoff plus the ~9.3m p90 planner call, so a read starting inside it lands after the gate has closed). COOLDOWN: a wake within WAKE_COOLDOWN_MIN of the last wake-AUTHORED plan version logs "would_skip: cooldown <m> min" and still runs — measured from the last version a wake actually WROTE, which is what makes it different from wake_min_interval_min (that paces attempts). Both counts are recorded per trader/session-day/session.',
    trader:
      'Why: 60 wake re-plans in 7 days produced 33 arm rows, 23 ever placed, 9 ever working. On 09-02 the wakes fired every ~30 minutes from 08:42 to 14:20 — the drumbeat of the throttle, not of events — and NY bought 12 plan versions. The 14:20 wake sat 10 minutes from the last-entry cutoff. Nothing is switched off yet: the counters exist so the suppression decision is made on a week of real numbers.',
    consumer:
      'trader/class47_wake_cadence.go (resolvers + lines) · trader/auto_trader_wake_levels.go (the wake path) · store/class47_counters.go (recorded counters)',
    range: 'WAKE_CUTOFF_MIN 0 (off) – 60 · WAKE_COOLDOWN_MIN 0 (off) – 120',
    systemDefault: '25m cutoff · 30m cooldown — both WARN-only',
    recommended:
      '⭐ leave as-is until a week of would_skip counts exists; then rule on suppression.',
    whenToTouch:
      'Only to widen the observation window, not to suppress — suppression is an owner ruling, not a knob flip.',
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
    label: 'Breakeven trigger — SUSPENDED (0B)',
    where: 'Strategy → Risk Control',
    what: 'Move the stop to entry after the position gains this much. SUSPENDED 2026-09-02 pending MFE data (wave 1A): the knob is retained and the trigger still evaluates, but NO move_stop frame is sent while suspended. The boot line reads BE from the strategy toggle and seam=SUSPENDED from env — the two sources the mechanics honour. It fired 2× on 09-01 with no measurement of whether it helps, and the net effect of breakeven moves is contested in the research.',
    trader:
      'While suspended your exits are: fixed stop · fixed target · EOD flat · plan invalidation/dormant. Nothing silently moves your stop.',
    consumer:
      'trader/auto_trader.go (maybeMoveStopToBreakeven → exitMechSuspendedRefuse → moveStopWire)',
    range: 'ticks · default 50 · env EXIT_MECHS_SUSPENDED=0 restores',
    systemDefault: '50 (suspended)',
    recommended:
      '⭐ leave suspended until the MFE distribution says the move pays. Wave 1A (2026-09-02) now records it: `go run ./cmd/excursions` prints MFE p50/p80/p95 per condition with the n each rests on.',
    whenToTouch:
      'Only with MFE evidence that the move pays — the distribution is in trade_excursions now, so this is answerable rather than a judgement call.',
    perSession: 'No.',
  },
  {
    label: 'Trailing stop — SUSPENDED (0B)',
    where: 'Strategy → Risk Control',
    what: 'ATR-multiplier trail. SUSPENDED 2026-09-02 pending MFE data (wave 1A): the ratchet still computes a level, but NO move_stop frame is sent while suspended. The boot line reads trail from the strategy toggle and seam=SUSPENDED from env — the two sources the mechanics honour. It ratcheted 8× on 09-01 with no measurement; a 567,000-backtest study ranks ATR/Chandelier trails in the worst group of 15 exit families, and our own tape shows $719.50 of giveback with ZERO trail exits ever.',
    trader:
      'Suspended, not deleted. Unmeasured mechanisms moving live stops is the problem — regardless of which way they cut.',
    consumer:
      'trader/auto_trader_trailing.go (maybeTrailStop → exitMechSuspendedRefuse → moveStopWire)',
    range:
      'mult 0.5–5 · period 7–28 · arm: after_breakeven | N-points | immediately · env EXIT_MECHS_SUSPENDED=0 restores',
    systemDefault: '2.0 / 14 / after_breakeven (suspended)',
    recommended:
      '⭐ leave suspended until the MFE distribution says the move pays. Wave 1A (2026-09-02) now records it: `go run ./cmd/excursions` prints MFE p50/p80/p95 per condition with the n each rests on.',
    whenToTouch: 'Only with evidence the trail beats the fixed target.',
    perSession: 'No.',
  },
  {
    label: 'Guardrails master',
    where: 'Strategy → Risk Control → Guardrails',
    what: 'Master switch for the daily guardrails stack (loss/profit caps, max trades, consecutive-loss halt, reentry cooldown, consistency, blackout windows).',
    trader:
      'Currently OFF by owner ruling — the would-have-tripped counters still display.',
    consumer: 'kernel/engine_position.go (guardrail evaluation)',
    range: 'ON | OFF',
    systemDefault: 'ON',
    recommended:
      '⭐ OFF for now (owner ruling) — re-armed after the risk audit is reviewed.',
    whenToTouch: 'ON when you want the daily circuit breakers live.',
    perSession: 'No.',
  },
  {
    label: 'Max contracts (always-on) — Stage A: 1',
    what: 'Contract cap per order — on with or without the master. 0B (2026-09-02): every resolution is clamped to the Stage-A ceiling of 1 contract — survival-first under an undemonstrated edge. Stage B (2) only at n≥30 closed trades with a POSITIVE LOWER-CI expectancy; Kelly and optimal-f are undefined without an edge estimate. Before 0B the two resolvers disagreed: arm-leg capacity said 1 while order sizing said 2, and the boot line said capacity=1.',
    where: 'Strategy → Risk Control → always-on row',
    trader:
      'THE ARITHMETIC: 0B also raised the stop floor from 1.0× to 1.5×ATR5m, which lifts dollar risk per trade by roughly 50% at constant size. That is precisely why size does NOT move at the same time.',
    consumer:
      'kernel/risk_limits.go (ResolveMaxContracts → ClampStageAContracts)',
    range:
      'page value · Stage-A ceiling 1 · env STAGE_A_CONTRACT_CAP raises it',
    systemDefault: '1 (Stage A)',
    recommended: '⭐ 1 — do not raise before the n≥30 lower-CI test.',
    whenToTouch: 'Stage B, with the expectancy table in hand.',
    perSession: 'No.',
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
    where: 'Strategy → Risk Control',
    what: 'position_value ≤ equity × ratio — the CODE-ENFORCED sizing ceiling (page values cannot bypass it).',
    trader:
      '5x BTC/ETH and 1x alt = the bot can size up to 5× equity on majors, 1× on alts.',
    consumer:
      'trader/auto_trader_risk.go:229 (enforcePositionValueRatio) · kernel/engine_analysis.go:527',
    range: 'page 1–20 · code-enforced 5 / 1',
    systemDefault: '5 / 1',
    recommended: '⭐ 5/1 — current config.',
    whenToTouch: 'Lower to derisk; values above 5/1 are inert (code ceiling).',
    perSession: 'No.',
  },
  {
    label: 'Daily loss limit',
    where: 'Strategy → Risk Control → Guardrails',
    what: 'Realized PnL ≤ −limit trips the daily-loss halt (force-flat class).',
    trader: 'The circuit breaker that ends a bad day at a known dollar number.',
    consumer:
      'kernel/risk_limits.go:184 (DailyLossLimitUSD) · engine_analysis.go:145',
    range: 'USD · env RISK_MAX_DAILY_LOSS_USD fallback',
    systemDefault: 'ON (with master) · value in Risk Control',
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
    range: 'USD · enabled with master',
    systemDefault: 'ON (with master)',
    recommended: '⭐ ON — one of the cheapest edge protections in the stack.',
    whenToTouch: 'Disable only if you deliberately want unlimited upside days.',
    perSession: 'No.',
  },
  {
    label: 'Max daily trades',
    where: 'Strategy → Risk Control → Guardrails',
    what: 'Entry count cap per session-day.',
    trader: "Stops revenge-trading after the day's quota is spent.",
    consumer: 'kernel/risk_limits.go:187 (MaxDailyTradesEnabled)',
    range: 'count · enabled with master',
    systemDefault: 'ON (with master)',
    recommended: "⭐ ON with a number that fits the strategy's hit rate.",
    whenToTouch: 'Review weekly against the win rate.',
    perSession: 'No.',
  },
  {
    label: 'Consecutive-loss halt',
    where: 'Strategy → Risk Control → Guardrails',
    what: 'N consecutive losing closes halt entries until the next session.',
    trader:
      'The streak-breaker: three losers in a row is the market telling you something.',
    consumer:
      'store/position_query.go:57 (CountConsecutiveLossesSince) · telemetry gate-block consecutive_loss',
    range: 'count · enabled with master',
    systemDefault: 'ON (with master)',
    recommended: '⭐ ON, threshold 2–3.',
    whenToTouch: 'Leave ON — this is the cheapest guardrail in the stack.',
    perSession: 'No.',
  },
  {
    label: 'Re-entry cooldown',
    where: 'Strategy → Risk Control → Guardrails',
    what: 'Minimum minutes between a close and the next entry.',
    trader: 'Prevents immediately re-entering after being stopped.',
    consumer: 'kernel/risk_limits.go (guardrail soft set)',
    range: 'minutes · enabled with master',
    systemDefault: 'ON (with master)',
    recommended: '⭐ ON — 5–15 minutes.',
    whenToTouch: "Tune to the strategy's average re-arm time.",
    perSession: 'No.',
  },
  {
    label: 'Consistency',
    where: 'Strategy → Risk Control → Guardrails',
    what: 'Max daily PnL percentage swing before the consistency rule fires.',
    trader: 'Bounds how much one day may deviate from the norm.',
    consumer: 'kernel/risk_limits.go:195 (ConsistencyMaxDayPct)',
    range: 'percent · enabled with master',
    systemDefault: 'ON (with master)',
    recommended: '⭐ ON for a smooth equity curve.',
    whenToTouch: 'Loosen only after a verified regime change.',
    perSession: 'No.',
  },
  {
    label: 'Blackout windows',
    where: 'Strategy → Risk Control → Guardrails',
    what: 'Configured CT time windows (start+end) with zero entries.',
    trader: 'Hard blackout — the bot simply will not trade inside it.',
    consumer: 'kernel/risk_limits.go:193 (BlackoutConfigured / InBlackoutNow)',
    range: 'start+end CT · enabled with master',
    systemDefault: 'OFF',
    recommended:
      '⭐ ON with 12:00–13:30 CT — the window kernel.LunchWindowCT() resolves, the same one the lunch gate reads — or your worst hours.',
    whenToTouch: 'Set for your known-bad hours from the journal.',
    perSession: 'No.',
  },
]

const sessions: KnobSpec[] = [
  {
    label: 'Session overrides (ASIA / LONDON / NY)',
    where: 'Strategy → Day Plan → Sessions accordion',
    what: 'Per-session override rows: min grade, min scenario quality, max trades, plan mode, max re-plans, acceptance window. Min grade, quality, max trades and plan mode are tri-state: inherit (blank) = the strategy-level row; an explicit value wins. Stored values that EQUAL the strategy level are auto-migrated to inherit. (min side levels REMOVED — owner ruling 2026-08-31: the per-side count concept is deleted.)',
    trader:
      'The current rows: min_grade B · min_scenario_quality C · max_trades 7/10/10 (ASIA/LONDON/NY) · plan_mode strict ×3 · max re-plans 4 · acceptance 5m_close.',
    consumer:
      'store/strategy.go:921-975 (per-session resolvers) · trader/auto_trader_planconfig.go:158-168',
    range:
      'per-session rows; the four tri-state knobs inherit (blank) = strategy value, explicit = override',
    systemDefault:
      'ASIA 16:30 read 17:00→02:00 · LONDON 01:30 02:00→08:30 · NY 08:00 08:30→14:45 (all EOD-flat) — reads moved to open−30 by owner ruling 2026-08-31; class 36: scheduled reads author during the halt/weekend from stored bars (preflight freshness check bypassed for scheduled classes only)',
    recommended:
      '⭐ keep the current rows — they ARE the deployed session map.',
    whenToTouch: 'Only with a deliberate session-thesis change.',
    perSession: 'N/A (they define it).',
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
          'A field-level grep found no reader. That is not proof: a method-based reader would not appear in it. Since 2026-09-11 the method-level check is CODE (store/knob_method_readers_test.go): every candidate row is re-checked for accessor-method readers on every test run, and a row with one fails the build. The first run found seven — the six wake_on_* / wake_min_interval_min knobs and acceptance_rule — all live all along; they now show live with their call sites. The rows still listed here genuinely have no reader either way.',
        ],
        ['advisory', '—', 'Feeds prompt text only, never a gate.'],
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
    { kind: 'h', text: 'Risk Control knobs' },
    { kind: 'knobs', knobs: risk },
    { kind: 'h', text: 'Session map' },
    { kind: 'knobs', knobs: sessions },
    { kind: 'h', text: 'Env-only knobs (not Studio)' },
    {
      kind: 'callout',
      title: 'The 9 env knobs — .env only, never a Studio slider',
      items: [
        {
          title: 'ARM_MIN_RR = 2.0',
          body: 'The gate-at-arm R:R floor for resting orders. RESOLVED 2026-09-03: the market-entry floor is ALSO 2.0, not 3.0 — the entry gate reads the BOUND strategy (MNQ, a5b7662e), whose min_risk_reward_ratio is 2 since the 2026-09-01 08:13 CT save. Both paths therefore refuse below 2.0. The 3.0 this card used to claim is the hardcoded fallback the gate uses only when the bound config has no value, and it is also what the unbound preset 均衡策略 carries — which is how the wrong number got here.',
        },
        {
          title: 'HTF_VETO_MODE = cross',
          body: 'Veto mode: 1h | cross | 4h — LIVE = cross (1h AND 4h must agree; the $352/0 autopsy).',
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
          title: 'INGEST_QUEUE_CAP = 1024',
          body: 'Bar-ingest queue depth (peak_depth is logged; 0 drops is the invariant).',
        },
        {
          title: 'AI_PLAN_MAX_TOKENS = 65536',
          body: 'Planner completion budget — truncation is a 🚨 WARN, never silent.',
        },
        {
          title: 'PERSIST_STALL_WATCHDOG_S = 60',
          body: 'Bar-persist silence alarm: no successful flush for N seconds while live bar frames are FLOWING → loud ERROR (the Friday ~2h GORM stall can never go silent again). Frame-aware: an idle wire (weekend, the daily break, NT8 closed) stays silent — no cry-wolf.',
        },
      ],
    },
    { kind: 'h', text: 'The save ritual' },
    {
      kind: 'p',
      text: 'Every Strategy-page change must be SAVED to take effect. Ritual: make the change → press Save → "Strategy saved" toast → the `saved {MM/DD, HH:MM} CT` chip updates. Unsaved changes are inert — and the knob-vs-code truth is: a page value above a code ceiling (e.g. leverage 20 vs system 10) saves but does nothing.',
    },
    {
      kind: 'callout',
      title: 'knob-vs-code — the four patterns',
      items: [
        {
          title: 'Wired + clamped',
          body: 'Page value used, code clamps to the system ceiling (leverage 20 → 10/5).',
          cite: 'kernel/engine_analysis.go:125',
        },
        {
          title: 'Wired + per-session',
          body: 'Session override wins over strategy value (plan_mode, proximity, caps).',
          cite: 'store/strategy.go:921-975',
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
          body: 'condition_status map, resolved per-condition: session override → strategy base → env (SHADOW_CONDITIONS / LIVE_CONDITIONS) → defaults. Defaults this wave: fvg_entry = shadow, breakout_retest = shadow, all others = live. sweep_reclaim is NOT shadowed (docketed for the Sep-9 court, pre-registered criterion, do not touch).',
        },
        {
          title:
            'Pre-registered promotion criterion (fix this now, never loosen it later)',
          body: "A shadowed condition returns to LIVE only if, at n ≥ 30 shadow setups on our own tape, its net-of-friction expectancy LOWER CONFIDENCE BOUND exceeds zero. Otherwise it remains shadowed, or is deleted at the court's discretion. No promotion on narrative. No promotion on a good week. No promotion because the model likes authoring it. No promotion on a point estimate without its interval.",
        },
      ],
    },
  ],
}
