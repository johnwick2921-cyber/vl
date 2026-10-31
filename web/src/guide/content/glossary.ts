import { GUIDE_BUILT_REV, type GuideSection } from '../types'

export const glossary: GuideSection = {
  id: 'glossary',
  num: 11,
  title: 'Glossary',
  tagline: 'A → Z of the words this system uses.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    {
      kind: 'glossary',
      terms: [
        {
          term: 'A-setup',
          def: 'The chained play: sweep → displacement → FVG retrace at a fresh Tier-1 origin.',
        },
        {
          term: 'Advisory',
          def: 'Informs but never blocks. Opposite of HARD gate.',
        },
        {
          term: 'Bias',
          def: "The AI's directional read + conviction. Nothing gates on it; branches name the machine reasoning.",
        },
        {
          term: 'Bias-tree',
          def: 'The 6 named branches the planner must choose from when writing bias.',
        },
        {
          term: 'Chain / chain_after',
          def: 'The scenario link: S2 runs only after S1 (e.g. fvg_entry after sweep_reclaim).',
        },
        {
          term: 'Confirm',
          def: "The machine's per-scenario verification (MET / not met / stale). Advisory.",
        },
        {
          term: 'Consumed (fresh dot)',
          def: 'A level whose touches are spent — dimmed, still informative.',
        },
        {
          term: 'EOD flat',
          def: 'The session-end flat: cancel resting entries, then close open positions — at market by default; when EOD_FLAT_LIMIT_TICKS > 0 (default 0) it first works a limit that many ticks away, then goes to market after EOD_FLAT_MARKET_AFTER_SEC. A SWING4H mentor position is exempt.',
        },
        {
          term: 'Fail-closed',
          def: 'Read failed after retries → sit out (safe). Never trades stale.',
        },
        {
          term: 'FVG',
          def: "Fair-value gap — the A-setup's entry surface. IN-ZONE/ABOVE/BELOW/FILLED_INVALID.",
        },
        {
          term: 'Gate-block',
          def: 'A named refusal with a counter, reset per session-day.',
        },
        {
          term: 'HARD gate',
          def: 'Refuses the trade. See the truth table (Section 6).',
        },
        { term: 'HTF', def: 'Higher timeframe (1h/4h) context floors.' },
        {
          term: 'Level role',
          def: 'What a level is FOR: magnet / liquidity / react / target-only / pivot.',
        },
        {
          term: 'MAE / MFE (excursion)',
          def: 'How far a trade went AGAINST you (maximum adverse excursion) and IN YOUR FAVOUR (maximum favorable excursion), in points, measured on the 1m tape from the bar containing the fill to the exit. Recorded per position in trade_excursions with the timestamp and bar offset of each extreme, bars held, and a count of bars that reached BOTH the stop and the target. Read them with `go run ./cmd/excursions`. A trade the tape does not cover is recorded as resolution=none with NULLs — never a zero, because before 2026-09-02 the mae column defaulted to 0 and 517 of 586 closed rows were unreadable as a result.',
        },
        {
          term: 'Ambiguous bar',
          def: 'One 1m bar whose range reached both the stop and the target. A bar carries no ordering, so the excursion record resolves it AGAINST the trade (the stop) and flags the row, rather than crediting a target that may never have been hit first. The share of such rows is printed with its n beside every distribution.',
        },
        {
          term: 'Min-side quota',
          def: 'Per-side level counts are DELETED (owner ruling 2026-08-31). The only side guard left: a plan with 0 levels on a side fails closed (2026-08-18 pathology); an empty machine map also fails closed.',
        },
        {
          term: 'No-trade',
          def: "A position: either fail-closed (failure) or the AI's skip-day declaration (decision).",
        },
        {
          term: 'PDH/PDL/PDC',
          def: 'Prior CME session (17:00→16:00 CT) high/low/close — the dealing-range anchors (m239: the session day, never the calendar day). PDC is the native 1d close (the ~15:00 CT settle, the same source as SETT); with no daily row it is the session\u2019s last 1m close, labelled \u201cPDC·last print\u201d (m239a).',
        },
        {
          term: 'Pep mode',
          def: 'A trading mode of its own (a strategy is exactly one of AI planner, mentor or pep): two FIXED opening-range rules from the owner’s PDF — style A (5-minute ORB breakout, enter immediately) and style E (EMA9 ORB, candle-close confirmation), both locked and read-only — plus Custom, the one editable style. Separate from mentor mode, SIM only, OFF by default. Section 20.',
        },
        {
          term: 'Plan mode',
          def: 'advisory / direction / strict — how the plan constrains entries.',
        },
        {
          term: 'Provenance',
          def: 'Where a level came from (PDH, ONH, nPOC·Tue, RN, EQH…).',
        },
        {
          term: 'Re-plan',
          def: 'A death-triggered re-read on the same chain (trigger death_replan). SPENDS one of replan_cap — recorded when the row lands (class 35).',
        },
        {
          term: 'Re-read',
          def: 'Owner-triggered extra planner call on the same chain (trigger owner_reread). SPENDS one of replan_cap. Wake reads (level_event / structure_mss) are re-reads too but are FREE.',
        },
        {
          term: 'Track record (pnl_corrected)',
          def: 'Every P&L figure the model and the dashboard read is pnl_corrected — never raw realized_pnl — over the BOT\'s own trades only: manual NT8 trades (origin=manual) are excluded and shown as their own line (owner ruling R1, release #22). The executor prompt states "+X over N resolved trades (K unresolved excluded)"; the AgentBeta trade tool and the API stats carry the same shape plus manual_excluded_* (P&L-truth wave, 2026-09-01).',
        },
        {
          term: 'Manual trade (origin=manual)',
          def: 'An NT8 position the bot ADOPTED with no order or arm of its own (someone traded the account by hand). Kept in the ledger and listed with a "MANUAL · excluded" badge, but never in a bot total, the track record, the day P&L, the breaker, expectancy or the risk gates\' bot P&L. Every bot figure shows the manual rows as their own line, e.g. "Manual (excluded): 4 trades, −$3,202.00" (#608 #609 #637 #640, owner ruling R1).',
        },
        {
          term: 'Session day (P&L)',
          def: 'The ONE "today" for every P&L figure and risk gate: the CME session day that starts 17:00 CT (kernel.CMESessionDayStart). LEDGER_DAY and the position-history footer used the CT calendar day until release #22. NT8\'s own session P&L is an account figure and is labelled NT8_SESSION. (Done-after-win keeps its own NY-day start, 08:30 CT by default — a mentor trading rule, owner ruling 2026-10-09.)',
        },
        {
          term: 'UNRESOLVED (trade)',
          def: 'A closed row whose pnl_corrected is NULL — no verified exit fill. It is COUNTED and EXCLUDED from every sum, average, win rate and streak, listed as "#id side entry→? UNRESOLVED" with no P&L and no percentage, and never coerced to a raw value (row 526: raw −1,458.00 vs corrected −69.43).',
        },
        {
          term: 'Seat',
          def: "A level's place in the card (proximity band + cap + floors).",
        },
        {
          term: 'Session registry',
          def: 'ASIA/LONDON/NY read times and windows, all EOD-flat.',
        },
        {
          term: 'SIM lock',
          def: 'isAccountTradeable — non-SIM accounts are refused at routing.',
        },
        {
          term: 'Thin side',
          def: 'REMOVED concept (owner ruling 2026-08-31): there is no thin-side note, knob or WARN any more. What survives: a plan with 0 levels on a side, or an empty machine map, fails closed.',
        },
        {
          term: 'Tier-1',
          def: 'The anchor family (PDH/PDL/PDC/ONH/ONL/VWAP/POC…) that qualifies origins.',
        },
        {
          term: 'Touch chip',
          def: '◐ approaching · ◐ touching · ✕ rejected · ▲ accepted — telemetry only.',
        },
        {
          term: 'Version chips',
          def: 'v1…vN plan history; the terminal ⛔ marker is the no-trade row.',
        },
      ],
    },
  ],
}
