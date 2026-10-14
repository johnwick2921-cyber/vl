import { GUIDE_BUILT_REV, type GuideSection } from '../types'

export const tradingDay: GuideSection = {
  id: 'trading-day',
  num: 2,
  title: 'The Trading Day',
  tagline: 'One CME session-day, 24 hours, in CT.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    {
      kind: 'timeline',
      items: [
        {
          time: '17:00',
          label: 'Session roll + ASIA read',
          detail:
            'The CME session-day rolls; the ASIA plan is read (scheduled read 16:30, session opens 17:00 — open−30 per owner ruling 2026-08-31). The 16:30 read runs INSIDE the 16:00–17:00 halt on purpose: since class 36 the planner preflight skips its bar-freshness check for scheduled reads while the market is closed and authors from the last stored bars (loud 🗓 preflight-bypass line). Gate-block counters reset at the roll.',
        },
        {
          time: '01:30',
          label: 'LONDON read',
          detail:
            'London plan read; session runs 02:00–08:30 CT. DST can shift the window ±1h (the card warns).',
        },
        {
          time: '08:00',
          label: 'NY read',
          detail:
            'The main event. NY runs 08:30–14:45 CT. The 08:00 read produces the plan the executor trades all day.',
        },
        {
          time: '12:00–13:30',
          label: 'Lunch gate',
          detail:
            'Hard no-entry window for AI and planner entries. One definition (kernel.LunchWindowCT) read by the entry gate, the adherence grader and the plan card, so all three refuse, score and display the same window. The card only shows it while it can still refuse something. Mentor mode is exempt: its arms are not refused by the lunch band (trader/session_risk.go mentorWaivesSessionBand).',
          shade: true,
        },
        {
          time: '14:45',
          label: 'EOD flat',
          detail:
            'For a Day-Plan or mentor trader, every resting entry is cancelled first (intraday mentor orders included), then open positions are force-flattened at session end — a market close by default; the limit-then-market ladder runs only when the server env sets EOD_FLAT_LIMIT_TICKS above 0 (default 0; R-A15 ruling). A SWING4H mentor position and its order are exempt (held by the 4h). No new entry is accepted after the flat; mentor mode places no new entry from 15:45 CT until the 17:00 reopen.',
        },
        {
          time: '16:00–17:00',
          label: 'Halt',
          detail:
            'CME daily halt — no executor decisions, no orders, no arms (unchanged). The PLANNER still authors on schedule during the halt (class 36 split: planner authors regardless of market state; executor never trades in a halt). The safe window for deploys and restarts.',
          shade: true,
        },
      ],
    },
    { kind: 'h', text: 'The executor cycle (every ~2 minutes)' },
    {
      kind: 'code',
      title: 'six plain steps',
      lines: [
        '1. WAKE  — cycle timer (~2 min) or a post-exit kick',
        '2. SENSE — 10 detector groups run on the 1m snapshot + HTF re-runs',
        '3. PROMPT — market facts + the live plan → DeepSeek',
        '4. DECIDE — the AI proposes an action (or none)',
        '5. GATE   — risk gates review (plan_mode, min-conf, min-SL, veto…)',
        '6. ACT    — an order on the trader’s bound SIM account, or a documented refusal',
      ],
    },
    { kind: 'h', text: 'Plans rewrite themselves — the 5 wake triggers' },
    {
      kind: 'callout',
      title: 'a level-event wake fires a fresh planner read',
      items: [
        {
          title: 'Seated-level invalidation',
          body: 'A seated zone closes beyond its noise band → the plan may be stale.',
          cite: 'trader/auto_trader_wake_levels.go maybeWakePlannerOnLevelEventsAt (trigger_reason="level_event")',
        },
        {
          title: 'New 15m / HTF zone',
          body: 'A new zone forms after the active plan version was written: a 15m reversal supply/demand zone, a 15m FVG, or a 1h/4h supply/demand zone of any pattern. No distance-to-price filter applies.',
          cite: 'trader/auto_trader_wake_levels.go collectLevelWakeCandidates',
        },
        {
          title: 'HTF order block',
          body: '1h/4h OB forms. OFF by default and there is no Studio control: it runs only when the strategy already stores the legacy wake_on_htf_ob=true AND wake_on_level_events is ON.',
          cite: 'store/strategy.go (*DayPlanConfig).WakeOnHTFOrderBlocks',
        },
        {
          title: 'iFVG inversion',
          body: 'A filled FVG inverts → the imbalance flipped. There is no separate iFVG switch: ONE strategy setting, wake_on_level_events (Strategy → Day Plan → Wake on level events, ON by default), turns the seated-invalidation, 15m/HTF zone, 15m FVG and iFVG wakes on or off together. The MSS wake below is not behind it.',
          cite: 'store/strategy.go (*DayPlanConfig).WakeOnLevelEventsEnabled · trader/auto_trader_wake_levels.go collectLevelWakeCandidates',
        },
        {
          title: 'Structure MSS',
          body: 'A 15m MSS (market-structure shift) event newer than the active plan version, read from the 15m structure of the 1m snapshot — one wake per MSS event.',
          cite: 'trader/auto_trader_transition.go maybeWakePlannerOnMSSAt (trigger_reason="structure_mss")',
        },
        {
          title: 'Deferred while a flip line is breached',
          body: "Level-event and MSS wakes are deferred while the active plan's flip line is breached (or the flip evaluation is skipped for stale bars) — the flip evaluator owns the plan until it fires or price closes back; scheduled reads and death re-plans are unaffected.",
          cite: 'trader/auto_trader_flip_breach.go wakeDeferredByFlip · kernel/flip_breach.go (W-FLIP-OWNS-THE-BREACH)',
        },
      ],
    },
    { kind: 'h', text: 'The sessions (shipped clocks)' },
    {
      kind: 'p',
      text: 'This table is static text: it shows the SHIPPED session clocks (kernel/session_registry.go DefaultSessionRegistry) and the default EOD-flat offset of 0 min — not your live config. Your strategy may differ: which sessions run (shipped default: NY only), plan_mode, min grade, max trades and the flat/last-entry offsets per session live in Strategy → Day Plan → Sessions accordion — check Studio. ASIA trades the evening; London is the second shift; NY is the main session.',
    },
    {
      kind: 'table',
      title: 'session registry (the times every gate uses)',
      head: ['Session', 'Read', 'Window (CT)', 'EOD flat'],
      rows: [
        ['ASIA', '16:30', '17:00 → 02:00', 'session end − 0 min'],
        ['LONDON', '01:30', '02:00 → 08:30', 'session end − 0 min'],
        ['NY', '08:00', '08:30 → 14:45', 'session end − 0 min (R-A15)'],
      ],
    },
    {
      kind: 'h',
      text: 'EOD flat — a market close by default (the limit-then-market ladder is opt-in)',
    },
    {
      kind: 'p',
      text: 'At EOD flat (and T1 force-flat) a Day-Plan or mentor trader cancels its resting entries first, then closes open positions at MARKET by default. The limit-then-market ladder runs only when the server env sets EOD_FLAT_LIMIT_TICKS above 0 (default 0): it first works a LIMIT that many ticks favorable of the last 1m close, then goes to MARKET after EOD_FLAT_MARKET_AFTER_SEC (0 or unset = 10 s) (trader/auto_trader_clock.go flattenPosition). Why 14:45: on a half-day (the session calendar lists it — e.g. 12:15 CT on 11-27 and 12-24) the flat is pulled IN to the early close and the last entry to the early close minus its offset, looked up on the date the session closes. At the early close the market is already shut, so for a Day-Plan or mentor trader the closed-market path runs the same flat (arms cancelled first, then positions) before it idles (trader/auto_trader_loop.go retireBookAfterEarlyCloseAt → enforceEODFlatAt, which does nothing unless Day Plan or mentor mode is on).',
    },
  ],
}
