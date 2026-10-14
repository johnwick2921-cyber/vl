import { GUIDE_BUILT_REV, type GuideSection } from '../types'

// CLASS 50 (refs-only wave, 2026-09-02) — the weekly read is REFS ONLY: the
// Sunday doc carries PWH/PWL/IPDA as price facts and NO directional call (the
// bias was anti-predictive on holdout — calibration report
// docs/superpowers/reports/2026-09-02-bias-calibration.md: holdout hit 25-28%
// raw, 45-51% called-only). The deterministic rule bias survives as SHADOW
// (shadow_bias on the doc + a log line), never read as a direction, never
// inverted. Nothing here gates, resizes or re-grades anything.

export const weeklyBias: GuideSection = {
  id: 'weekly-bias',
  num: 13,
  title: 'Weekly Refs & Planner Candles',
  tagline:
    'The Sunday read (refs only), the weekly chip, and the candle ground-truth law.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    { kind: 'h', text: 'What the Sunday read does' },
    {
      kind: 'cards',
      cards: [
        {
          title: 'One read per week (Sunday 16:30 CT) — REFS ONLY',
          body: 'At WEEKLY_READ_CT (default "sun 16:30" CT) the bot runs ONE AI read over the bars: 12 completed weekly candles, weekly references (weekly_open · PWH/PWL/PWC), the last 5 weekend gaps (NWOG), the 20/40/60-day IPDA ranges, and a prior-week recap. Since class 50 the doc carries ONLY the price facts (weekly_levels: PWH / PWL / IPDA extremes / unfilled NWOG edges) and a ≤3-line facts narrative — NO bias, no conviction, no draw, no invalidation. The doc is stored on a plans row with session=WEEKLY; a stored doc means never re-run (idempotent). A Monday boot backfills exactly once. WHICH WEEKS: CME weeks — Sunday 17:00 CT open → Friday 16:00 CT close — built from the NT8 daily bars, each placed on its own trading day (release #22). Before that the read rolled the daily bars into weeks that started THURSDAY 00:00 UTC, so PWL/PWC came from the wrong days (week 2026-10-12 printed PWL 30760.75 / PWC 30969.50; the real values were 30792.00 / 31108.75) and the IPDA and NWOG sections were always empty. weekly_open prints n/a at the Sunday read, because the week has not opened yet.',
        },
        {
          title: 'Why refs only — the calibration evidence',
          body: 'The bias-calibration run (2026-09-02, pre-registered, out-of-sample; report docs/superpowers/reports/2026-09-02-bias-calibration.md) tested the shipped weekly-structure bias against 8 years of NT8 daily bars: on holdout the reconstructed rule was ANTI-predictive (raw hit 25-28%, called-only 45-51%) and all three candidate signals failed the pre-registered usability bar. The references themselves remain price facts; the directional call was the part with no edge — it is now SHADOW only (the rule bias is stamped on the doc as shadow_bias and logged, so the anti-prediction keeps being measured). It is never read as a direction and never inverted.',
        },
        {
          title: 'Day-of-week reasoning is BANNED',
          body: 'The folklore law is hard inside the weekly read: any weekday token in the narrative rejects the doc (validator r3), and any directional token (bull/bear/long/short…) rejects it too (r4). The narrative is facts-only.',
        },
        {
          title: 'The weekly chip and prompt lines',
          body: 'The day-plan card chip renders "WEEKLY refs — PWH x · PWL y" (grey "WEEKLY none" when no doc exists). The session planner prompt carries the same refs-only line ("WEEKLY: refs only — PWH x · PWL y", thin-history suffix when applicable) and the executor prompt gets one matching line. A missing doc is fail-open: sessions render "WEEKLY: none" and nothing else changes.',
        },
        {
          title: 'Shadow mode annotates — it never blocks',
          body: 'Weekly-confluent levels still log 🌗 SHADOW grades (W5.1 — the real seating is untouched). The counter-annotation and draw-alignment shadows are retired with class 50 (there is no direction to be counter to). The rule bias shadow line ("📅 WEEKLY SHADOW") is measurement, not a gate.',
        },
      ],
    },
    { kind: 'h', text: 'The knobs (env only)' },
    {
      kind: 'knobs',
      knobs: [
        {
          label: 'WEEKLY_READ_CT',
          where: 'environment',
          what: 'When the Sunday weekly read fires ("<dow> HH:MM" CT).',
          trader: 'Trader: sets the read time of the week.',
          consumer: 'kernel/weekly_knobs.go WeeklyReadSpec',
          range: '"sun 16:30" (day 3-letter + HH:MM CT)',
          systemDefault: 'sun 16:30',
          recommended:
            '⭐ keep the default — 16:30 CT sits before the 17:00 CT Sunday open.',
          whenToTouch: 'Never in normal operation.',
          perSession: 'no — process-wide env',
        },
        {
          label: 'WEEKLY_CONFLUENCE_BAND_ATR',
          where: 'environment',
          what: 'The W5.1 shadow confluence band width in ATR5m units.',
          trader: 'Trader: wider band = more 🌗 SHADOW confluence logs.',
          consumer: 'kernel/weekly_knobs.go WeeklyConfluenceBandATR',
          range: '> 0 (float)',
          systemDefault: '0.25',
          recommended: '⭐ 0.25 — the researched default.',
          whenToTouch: 'Only for shadow studies.',
          perSession: 'no',
        },
        {
          label: 'WEEKLY_SHADOW_MULT',
          where: 'environment',
          what: 'The shadow grade multiplier for weekly-confluent levels.',
          trader: 'Trader: changes only the SHADOW reorder counter.',
          consumer: 'kernel/weekly_knobs.go WeeklyShadowMult',
          range: '> 0 (float)',
          systemDefault: '1.5',
          recommended: '⭐ 1.5 — shadow only; the real grade is untouched.',
          whenToTouch: 'Only for shadow studies.',
          perSession: 'no',
        },
        {
          label: 'PLANNER_CANDLES',
          where: 'environment',
          what: 'on (default) renders the ## Candles block (12×15m · 12×1h · 8×4h · 8×daily) in every session planner prompt.',
          trader: 'Trader: the planner gets raw candle eyes.',
          consumer: 'kernel/weekly_knobs.go PlannerCandlesEnabled',
          range: 'on | off',
          systemDefault: 'on',
          recommended: '⭐ on — candles are ground truth for structure.',
          whenToTouch: 'Off only if the token budget ever matters.',
          perSession: 'no',
        },
      ],
    },
    { kind: 'h', text: 'Planner candles' },
    {
      kind: 'cards',
      cards: [
        {
          title: 'Candles are ground truth',
          body: 'The session planner prompt carries the raw candle tables (15m, 1h, 4h, daily session candles) rendered by the SAME formatter the executor uses. The playbook line is explicit: "Candles are ground truth for structure; ranked levels and tags are summaries. On conflict, trust the candles and say so in the scenario rationale." Scenario lines citing "per candles" are counted into the planner_candle_citations telemetry counter — the P3 promotion evidence.',
        },
        {
          title: 'Every table says HELD vs REQUESTED',
          body: 'Each heading now reads "### daily session candles — HELD 3 of 8 requested rows · SHORT BY 5 …" instead of a baked "(last 8)". Measured over the stored planner prompts (n=55 with a Candles block, ids 70–143): 15m rendered 12 of 12 every time, 1h 12 of 12, 4h 8 of 8 — but the daily table rendered 2 or 3 rows, never 8, in 0 of 55. A table that IS complete says "every row PRINTED here is COMPLETE", so the disclosure is not something you only see when something is wrong. A row COUNT cannot see a whole window that is missing, so the heading also counts the rows that are NOT there — "4 WHOLE ROWS ABSENT BETWEEN HELD ROWS" — and the row after each break carries a ⛔ marker naming the CT bounds, because two rows printed next to each other are not necessarily adjacent windows. Above the tables, one TAPE line names the 1m bars held vs requested, the oldest bar and its age, and how many open 1m intervals are missing INSIDE that span.',
        },
        {
          title: 'A partial candle is marked, never invented',
          body: 'A daily session candle is bucketed by the 17:00 CT roll and its Open is simply the first bar held — so a tape that starts mid-session used to present a part-session as a whole one. Rows are now measured against the CME session calendar (kernel/session_calendar.json, shortened and closed days included) and marked on the row: ⚠PARTIAL (only part of the window is held — front-truncated rows say so, interior holes say the minutes are missing INSIDE), ⏳FORMING (the window has not closed — the minute in progress is counted on neither side, so a healthy tape is never marked PARTIAL just for having a forming bar), ❓COVERAGE-UNKNOWN (a year the calendar does not cover — never a guess). Nothing is interpolated, carried forward or synthesised to fill a hole, and no O/H/L/C/V is changed by any of it.',
        },
        {
          title: 'The store is the horizon, the ring is the cache',
          body: 'The live BarCache ring holds 2,500 bars per symbol+timeframe — about 41.7 h of 1m — and a restart rebuilds it from a 2,000-bar seed. Three reads asked for more than that could ever hold: 1m × 12,000 (200 h) twice and 5m × 3,000 (250 h). The two 1m reads now splice the persisted bars table onto the older end (measured 2026-09-09: MNQ 1m back to 2026-08-19, 21 days). Rules: an EMPTY ring is never backfilled from the store — no live feed still means no plan — a ring that already serves the ask is not second-guessed, only bars strictly older than the ring are taken so a live or forming bar is never replaced, and a failed store read simply serves the ring. Nothing here gates or blocks.',
        },
        {
          title: 'A restart no longer shortens the history',
          body: 'The bot used to lose its depth on every restart: the ring came back with the 2,000-bar seed the AddOn sends, then slowly climbed to 2,500 across a session, and the next restart reset it — while the persisted bars table held 21 days the whole time. On boot, once the AddOn replay lands, the 1m ring is now extended BACKWARDS from the store, with the counts logged. The ring CEILING is unchanged at 2,500 bars, so this restores the ring to that ceiling immediately instead of climbing to it over a session — about 41.7 h of 1m, not 21 days. The 21-day depth reaches the planner through the store splice described in the card above, which is a different mechanism. Only 1m is rehydrated: the stored 5m/15m/1h rows are NT8 aggregates this repo has judged inconsistent with their own 1m constituents, and they never enter a live regime input. It only ever adds bars older than the ring already has, so a live or forming bar is never overwritten; a symbol with no live bars at all is skipped entirely; and if the store read fails the boot simply continues on the AddOn seed.',
        },
        {
          title:
            'RV baseline names its real window — and the window got deeper',
          body: 'The regime line used to read "RV=103%-of-normal", where "normal" was a baseline averaged over about 7 complete session-days by a field called RVBaseline20d. It now reads "RV=103%-of-baseline(9 complete session-days)", or "(window UNKNOWN)" when the count was not reported. THE NUMBER MOVES, and that is the point: the estimator is byte-for-byte the same rule, but it is no longer fed a 5m ring that could only reach about 7 session-days — it is fed the store-deepened 1m tape aggregated to 5m, which reaches about 9. Measured 2026-09-09 on MNQ: 5m ring 0.876371 over 7 days, 1m tail 0.893543 over 9 days. A boot line reports the window BEFORE and AFTER in days so the step is visible and dated (📈 regime input window). If the 1m tape is ever too thin to answer, the old 5m ring read still serves it — the baseline is never turned off, and the log names which arm answered.',
        },
        {
          title: 'Weekly context (soft law, refs only)',
          body: 'Every session prompt carries a ≤3-line ## Weekly Context block — "WEEKLY: refs only — PWH 30500.25 · PWL 29980.00" or "WEEKLY: none" when no doc exists (fail-open: a missing doc changes nothing else). There is no counter-weekly direction anymore (class 50); the references are price facts the model may seat against. The executor prompt gets one matching line. The weekly doc never gates your plan.',
        },
      ],
    },
  ],
}
