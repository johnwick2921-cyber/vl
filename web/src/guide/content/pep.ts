import { GUIDE_BUILT_REV, type GuideSection, type KnobSpec } from '../types'

// Pep mode (owner order 2026-10-29). Source: the owner's PDF "A + E — Full
// calculation" (pep-spec-coa707.pdf). Every number below is the Go default in
// store/pep.go (PepResolved) / kernel/pep (DefaultConfig); the two are pinned
// equal by kernel/pep TestDefaultConfigEqualsResolvedStoreDefaults.
//
// One KnobSpec per stored setting (settingId = the stored path). Each card names
// the REAL Studio control (PepModePanel / TradingModeSelector); pepGuide.test.ts
// reads the panel's PEP_FIELDS, the Go resolver and the knob registry, so a
// card that names a control the Studio lacks, a default the bot does not run or
// a leaf the registry does not know is RED.
const PDF = 'owner PDF “A + E — Full calculation” (29/10/2026)'
const CONSUMER =
  'store/pep.go:ResolvePep → kernel/pep/config_store.go:FromSettings → kernel/pep/eval.go:Evaluate'
const SHARED = 'Strategy Studio → Pep mode accordion → “Shared by both styles”'
const TAB_A = 'Strategy Studio → Pep mode accordion → Style A tab'
const TAB_E = 'Strategy Studio → Pep mode accordion → Style E tab'
const ONE_SAVE =
  'No. One value per strategy; saved with the strategy, applied when the running trader reloads on save.'

const knob = (k: KnobSpec): KnobSpec => ({ live: true, ...k })

export const PEP_KNOBS: KnobSpec[] = [
  knob({
    label: 'Trading mode → Pep',
    settingId: 'pep.pep_enabled',
    where:
      'Strategy Studio → Risk Control accordion → “Trading mode” selector → Pep (asks to confirm; SIM-only wording)',
    what: 'The master switch. Choosing Pep writes pep.pep_enabled=true and mentor_mode=false in ONE write, so a strategy is exactly one of AI planner, Mentor or Pep.',
    trader:
      'Pep stays OFF until you pick it and save; the Day Plan must be OFF first, because the server refuses pep together with the day plan or mentor mode (pep_mode_conflict_day_plan / pep_mode_conflict_mentor).',
    consumer: 'store/pep.go:StrategyConfig.PepEnabled, TradingModeConflict',
    range: 'true / false; OFF is the shipped default on every trader',
    systemDefault: 'off',
    recommended:
      'Leave OFF until the trader has its own SIM account (owner order 2026-10-29: two traders cannot share one SIM account on MNQ).',
    whenToTouch:
      'When you want this trader to trade the opening range by the PDF rules instead of the AI planner or the mentor.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Style that runs',
    settingId: 'pep.pep_style',
    where: SHARED,
    what: 'Which of the two PDF styles trades: A (5-minute ORB breakout, enter immediately) or E (EMA9 ORB, candle-close confirmation).',
    trader:
      'Switching style never loses the other style’s numbers: each style keeps its own block (the A and E tabs).',
    consumer: CONSUMER,
    range: 'A | E (case-folded where it enters)',
    systemDefault: 'A',
    recommended: `A is the PDF’s stronger result at 6 MNQ ($17,595.90 against $10,070.10 received) — ${PDF}, p.1.`,
    whenToTouch:
      'When you want the EMA-confirmed entry (E) instead of the immediate breakout (A).',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Contracts',
    settingId: 'pep.pep_contracts',
    where: SHARED,
    what: 'How many MNQ contracts each pep entry trades.',
    trader:
      'The 2-contract AI clamp does not apply to pep. The full-box stop of style A makes a loss large: size it against your daily limit.',
    consumer: CONSUMER,
    range:
      '1 to 10 (hard ceiling 10: a bigger saved number is clamped and logged, never honoured)',
    systemDefault: '3',
    recommended: `3 on SIM first: the plan measured style A’s full-box stop at about $867 median and $2,439 worst per losing trade at 6 MNQ (PEP-PLAN-1029); the PDF shows both 3 and 6.`,
    whenToTouch: 'When you decide the size after watching SIM days.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Box start (CT)',
    settingId: 'pep.pep_box_start_ct',
    where: SHARED,
    what: 'The time of day the opening box starts, Chicago time (DST-safe).',
    trader: 'The box is the high/low of the first N minutes from this time.',
    consumer: CONSUMER,
    range: 'HH:MM, 00:00–23:59, America/Chicago',
    systemDefault: '08:30',
    recommended: `08:30 CT: the PDF’s opening range “từ 08:30 Houston” (Houston and Chicago share the clock) — ${PDF}, p.1.`,
    whenToTouch: 'Only to test a different opening time on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Flat time (CT)',
    settingId: 'pep.pep_flat_time_ct',
    where: SHARED,
    what: 'The time exit: any pep position still open is flattened at the last price.',
    trader:
      'This is pep’s own flat, not the session flat of 14:45 (which pep does not use). It must be after the box start or the shipped pair runs.',
    consumer: CONSUMER,
    range: 'HH:MM after the box start, America/Chicago',
    systemDefault: '15:00',
    recommended: `15:00 CT: the PDF closes at the last price before 15:00 Houston (the CUTOFF_PROXY rows) — ${PDF}, p.19.`,
    whenToTouch: 'Only to test an earlier exit on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Tick size',
    settingId: 'pep.pep_tick_size',
    where: SHARED,
    what: 'The instrument tick that every pep price is rounded to.',
    trader: 'MNQ moves in 0.25 points ($0.50 per contract).',
    consumer: CONSUMER,
    range:
      'greater than 0, at most 1000 (the Studio box takes 0.01 or more; the server itself accepts any value above 0; zero or negative runs the shipped value)',
    systemDefault: '0.25',
    recommended: `0.25: the MNQ tick in ${PDF}, p.3.`,
    whenToTouch: 'Never for MNQ; change only for another instrument.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Stop rounding',
    settingId: 'pep.pep_sl_rounding',
    where: SHARED,
    what: 'How a stop distance that is not on a tick is rounded: away from the entry (outward) or to the nearest tick.',
    trader: 'Outward can only widen the stop, never tighten it.',
    consumer: CONSUMER,
    range: 'outward | nearest',
    systemDefault: 'outward',
    recommended: `outward: the PDF rounds the style-E half-box stop “ra tick hợp lệ” away from the entry — ${PDF}, p.1.`,
    whenToTouch: 'Rarely; nearest is for experiments.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Max trades per day',
    settingId: 'pep.pep_max_trades_per_day',
    where: SHARED,
    what: 'How many pep entries are allowed per Chicago day.',
    trader:
      'Style A takes the first break only, whatever this says. The count is kept on disk before the order is sent, so a restart cannot fire a second entry.',
    consumer: CONSUMER,
    range: '1 to 10',
    systemDefault: '1',
    recommended: `1: the PDF takes “tối đa một lệnh/ngày” for both styles — ${PDF}, p.1.`,
    whenToTouch: 'Only to test a second entry on SIM (style E).',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Fee per contract round trip (USD)',
    settingId: 'pep.pep_fee_per_contract_usd',
    where: SHARED,
    what: 'The assumed round-trip fee per contract. It is used by the backtest and the reports ONLY.',
    trader:
      'No order and no gate reads it: changing it never changes a trade, only the backtest numbers.',
    consumer:
      'cmd/pep-backtest (report); kernel/pep Config.FeePerContractRT is never read by Evaluate',
    range: '0 to 1000; a legal 0 is kept',
    systemDefault: '1',
    recommended: `1: “Phí giả định = $1 round-trip × số MNQ” in ${PDF}, p.3.`,
    whenToTouch: 'When your broker’s real round-trip fee differs.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Max box size (points, 0 = OFF)',
    settingId: 'pep.pep_max_box_points',
    where: SHARED,
    what: 'Refuse to trade a box wider than this many points.',
    trader:
      'A wide box means a wide full-box stop. 0 is OFF (the PDF trades every box); when set, the refusal is named pep_box_too_wide.',
    consumer: CONSUMER,
    range: '0 to 100000; 0 = OFF',
    systemDefault: 'off (0)',
    recommended: `OFF to match the PDF; the plan measured the stored A boxes at about 80 pts median and 203 pts worst, so a limit is the risk dial — PEP-PLAN-1029.`,
    whenToTouch:
      'When a single full-box stop is larger than the loss you can accept.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Signal max age (min)',
    settingId: 'pep.pep_signal_max_age_min',
    where: SHARED,
    what: 'How old a break or cross, and the newest closed 1m bar, may be before it is refused as stale.',
    trader:
      'A break the bot did not see in time is never entered late, and the day is not shifted onto a later signal (pep_stale_signal / pep_bars_stale).',
    consumer: CONSUMER,
    range: '1 to 30 minutes',
    systemDefault: '2',
    recommended:
      '2: one bar of lag is tolerated, two is a late chase — a fail-closed choice of the core wave, not a PDF number.',
    whenToTouch: 'Only if the feed is slow and you accept later entries.',
    perSession: ONE_SAVE,
  }),

  knob({
    label: 'Box length (min) — Style A',
    settingId: 'pep.style_a.pep_a_box_minutes',
    where: TAB_A,
    what: 'Length of the opening box for style A, from the box start.',
    trader:
      'A box with a missing 1m bar is not drawn and no trade is taken that day.',
    consumer: CONSUMER,
    range: '1 to 120 minutes',
    systemDefault: '5',
    recommended: `5: “ORB 5 PHÚT” — ${PDF}, p.1.`,
    whenToTouch: 'Only to test another opening range on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Stop (× box) — Style A',
    settingId: 'pep.style_a.pep_a_sl_fraction',
    where: TAB_A,
    what: 'Style A stop distance as a fraction of the box.',
    trader:
      'At 1.0 the stop is the opposite edge of the box: the biggest loss pep can take.',
    consumer: CONSUMER,
    range:
      'greater than 0, at most 10 (the Studio box takes 0.05 or more; the server itself accepts any value above 0)',
    systemDefault: '1',
    recommended: `1.0: “SL cả box” — ${PDF}, p.1.`,
    whenToTouch:
      'To test a tighter stop on SIM; a smaller number stops out more often.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Target (× box) — Style A',
    settingId: 'pep.style_a.pep_a_tp_fraction',
    where: TAB_A,
    what: 'Style A target distance as a fraction of the box.',
    trader:
      'The target and the stop are placed at NT8 with the entry; there is no trailing and no break-even.',
    consumer: CONSUMER,
    range:
      'greater than 0, at most 10 (the Studio box takes 0.05 or more; the server itself accepts any value above 0)',
    systemDefault: '1',
    recommended: `1.0: “TP cả box” — ${PDF}, p.1.`,
    whenToTouch: 'To test a nearer or farther target on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'One bar through both edges',
    settingId: 'pep.style_a.pep_a_both_edges',
    where: TAB_A,
    what: 'What style A does when ONE 1m bar trades through both box edges: a 1m bar cannot say which edge broke first.',
    trader:
      'no_trade fails closed (pep_bar_through_both_edges, no trade today); nearest_open guesses that the edge nearer the bar’s open broke first.',
    consumer: CONSUMER,
    range: 'no_trade | nearest_open',
    systemDefault: 'no_trade',
    recommended:
      'no_trade: the PDF’s tick engine can order the two touches, a 1m bar cannot, so guessing would be invented data.',
    whenToTouch: 'Only to bracket the backtest; not for live SIM days.',
    perSession: ONE_SAVE,
  }),

  knob({
    label: 'Box length (min) — Style E',
    settingId: 'pep.style_e.pep_e_box_minutes',
    where: TAB_E,
    what: 'Length of the opening box for style E, from the box start.',
    trader:
      'Style E’s box is two minutes: the PDF’s own style-E ledger equals the first two minutes’ range (2025-10-20: target 73.0 pts).',
    consumer: CONSUMER,
    range: '1 to 120 minutes',
    systemDefault: '2',
    recommended: `2: “ORB 2 PHÚT” — ${PDF}, p.1; confirmed against the ledger by cmd/pep-backtest.`,
    whenToTouch: 'Only to test another opening range on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Stop (× box) — Style E',
    settingId: 'pep.style_e.pep_e_sl_fraction',
    where: TAB_E,
    what: 'Style E stop distance as a fraction of the box, rounded per “Stop rounding”.',
    trader: 'At 0.5 the stop is half the box, rounded OUTWARD to a valid tick.',
    consumer: CONSUMER,
    range:
      'greater than 0, at most 10 (the Studio box takes 0.05 or more; the server itself accepts any value above 0)',
    systemDefault: '0.5',
    recommended: `0.5: “SL nửa box (làm tròn ra tick hợp lệ)” — ${PDF}, p.1.`,
    whenToTouch: 'To test another stop size on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Target (× box) — Style E',
    settingId: 'pep.style_e.pep_e_tp_fraction',
    where: TAB_E,
    what: 'Style E target distance as a fraction of the box.',
    trader:
      'No trailing and no break-even: the stop and target sit at NT8 from the entry.',
    consumer: CONSUMER,
    range:
      'greater than 0, at most 10 (the Studio box takes 0.05 or more; the server itself accepts any value above 0)',
    systemDefault: '1',
    recommended: `1.0: “TP cả box” — ${PDF}, p.1.`,
    whenToTouch: 'To test a nearer or farther target on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'EMA period',
    settingId: 'pep.style_e.pep_e_ema_period',
    where: TAB_E,
    what: 'The EMA length of style E, on the EMA candles.',
    trader:
      'It is computed across sessions and never reset daily; until the warm-up candles exist the signal is refused (pep_ema_not_warm).',
    consumer: CONSUMER,
    range: '2 to 200',
    systemDefault: '9',
    recommended: `9: “EMA9” — ${PDF}, p.1.`,
    whenToTouch: 'Only to test another EMA on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'EMA candle (min)',
    settingId: 'pep.style_e.pep_e_ema_tf_min',
    where: TAB_E,
    what: 'The candle size the EMA is computed on, built from 1m bars on even Chicago minutes.',
    trader:
      'It must divide an hour; any other value runs the shipped 2 and says so in the eff line.',
    consumer: CONSUMER,
    range: 'one of 1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30, 60',
    systemDefault: '2',
    recommended: `2: “khung 2 phút” — ${PDF}, p.1.`,
    whenToTouch: 'Only to test another candle size on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Signal source',
    settingId: 'pep.style_e.pep_e_signal_source',
    where: TAB_E,
    what: 'What crosses the box edge to trigger style E: the EMA itself, or the candle close.',
    trader:
      'ema (owner ruling): the EMA9 crossing from <= the box high to > it is a buy, from >= the box low to < it a sell. close: the candle close crosses the edge instead.',
    consumer: CONSUMER,
    range: 'ema | close',
    systemDefault: 'ema',
    recommended:
      'ema: the owner’s ruling of 2026-10-29 (the EMA9 crosses the edge); close is the alternative the owner allowed as a setting.',
    whenToTouch: 'To compare the two readings on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Warm-up candles (0 = auto)',
    settingId: 'pep.style_e.pep_e_warmup_bars',
    where: TAB_E,
    what: 'How many complete EMA candles must exist before the EMA may signal.',
    trader:
      '0 means 3 times the EMA period; a legal 0 is kept, it is not a missing value.',
    consumer: CONSUMER,
    range: '0 to 5000; 0 = auto (3 × period)',
    systemDefault: '0 (= 3 × period)',
    recommended:
      '0: three periods leave about 0.2% of the EMA’s seed value, a core-wave choice, not a PDF number.',
    whenToTouch: 'Only if the EMA is cold after a long feed gap.',
    perSession: ONE_SAVE,
  }),
]

export const pep: GuideSection = {
  id: 'pep',
  num: 20,
  title: 'Pep Mode',
  tagline:
    'Two fixed opening-range rules from the owner’s PDF: A (5-minute breakout) and E (EMA9 cross). Separate from mentor mode.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    {
      kind: 'callout',
      title: 'What pep is, and what it is not',
      items: [
        {
          title: 'SIM only, OFF by default, its own mode',
          body: 'Pep is a trading mode of its own: its own code (kernel/pep), its own settings (the root "pep" block of the strategy) and no mentor rule applies to it. A strategy is exactly one of AI/planner, mentor or pep. Saving a strategy with pep ON together with mentor mode, or together with the day plan, is refused with a named error (pep_mode_conflict_mentor / pep_mode_conflict_day_plan); the strategy store refuses it for every writer, not only the Studio. Pep is OFF on every trader until the owner turns it on, and every trade path stays NinjaTrader SIM.',
        },
        {
          title: 'Build status of this page',
          body: 'This build carries the settings (stored, validated, clamped and logged, shown in the effective-settings rows), the pure evaluator, the stored-history backtest, the Studio controls (Trading mode selector and Pep mode panel) and the dashboard (mode chip, Pep card, box on the chart). The order path that places a pep trade and the 15:00 flat on a live position ship in the pep trader wave. Until that wave is merged, turning pep ON places no order. The integrator updates this callout when it lands.',
        },
      ],
    },
    { kind: 'h', text: 'The two styles' },
    {
      kind: 'table',
      title: 'Style A and style E (PDF "A + E — Full calculation")',
      head: ['', 'Style A: ORB 5m breakout', 'Style E: EMA9 ORB, candle-close'],
      rows: [
        [
          'Box (opening range)',
          'High/low of the first 5 minutes from 08:30 CT',
          'High/low of the first 2 minutes from 08:30 CT (the PDF calls it "ORB 2 PHÚT")',
        ],
        [
          'Entry',
          'The first side to trade THROUGH a box edge (a print at the edge is not through it), entered immediately',
          'On a CLOSED 2m candle: EMA9 (2m candles, carried across sessions, never reset daily) crossing from <= the box high to > it is a buy; from >= the box low to < it is a sell. Entered at that candle’s close.',
        ],
        [
          'Stop',
          'The full box',
          'Half the box, rounded OUTWARD to a valid tick',
        ],
        ['Target', 'The full box', 'The full box'],
        ['Trailing / break-even', 'None', 'None'],
        ['Trades per day', '1 (the first break only)', '1'],
        [
          'Time exit',
          'Flat at 15:00 CT at the last price',
          'Flat at 15:00 CT at the last price',
        ],
      ],
    },
    {
      kind: 'p',
      text: 'The style-E box is two minutes, not five. The PDF’s own style-E ledger rows equal the first two minutes’ range: on 2025-10-20 the E target is 73.0 points, which is the high-low of the 08:30 and 08:31 bars, while the style-A box that day is 86.75. The box length is a setting per style, so both are editable.',
    },
    { kind: 'h', text: 'It fails closed' },
    {
      kind: 'table',
      title: 'Named refusals (each is counted, none is silent)',
      head: ['Code', 'Meaning'],
      rows: [
        [
          'pep_box_missing_minute',
          'A 1m bar inside the box window is missing: no trade that day.',
        ],
        [
          'pep_gap_after_box',
          'A 1m bar is missing after the box and before any break (style A): the first break cannot be told.',
        ],
        [
          'pep_bar_through_both_edges',
          'One 1m bar traded through both edges (style A): a 1m bar cannot say which came first. Setting pep_a_both_edges chooses no_trade (default) or nearest_open.',
        ],
        [
          'pep_ema_not_warm',
          'Fewer complete 2m candles than the warm-up (3 x the EMA period by default): the EMA is not trusted yet.',
        ],
        [
          'pep_stale_signal',
          'The break or cross is older than pep_signal_max_age_min: it is not entered late, and the day is not shifted onto a later signal.',
        ],
        [
          'pep_bars_stale',
          'The newest closed 1m bar is older than pep_signal_max_age_min.',
        ],
        [
          'pep_box_too_wide',
          'Only when pep_max_box_points is set (OFF by default).',
        ],
        [
          'pep_max_trades_reached / pep_position_open / pep_after_flat_time',
          'Normal waiting states, not faults.',
        ],
      ],
    },
    { kind: 'h', text: 'Where to set it' },
    {
      kind: 'p',
      text: 'Strategy Studio, futures strategies only. (1) Risk Control accordion → “Trading mode”: AI planner, Mentor or Pep. Choosing one turns the others off in the same write, and Pep and Mentor ask first. (2) Pep mode accordion: the shared numbers, then a Style A tab and a Style E tab. Every field shows a concrete value (never “blank = default”) and a grey “eff” line under it: the value the bot RUNS, from the SAVED strategy. Every save sends the whole pep block, so a stale stored number can never survive an edit. Pep is a futures mode and is hidden for a crypto strategy. With Pep ON, the Day Plan block shows a warning and will not turn the plan on (it can always be turned off); the server refuses the combination either way. On the dashboard a mode chip (AI / Mentor / Pep A|E, or “Mode conflict” if mentor and pep were both saved ON) sits beside the strategy name, the “Pep — what trades” card shows the box, the signal state, trades today, stop/target and the countdown to the flat time, and the box is drawn on the chart once the server names the instrument it reads.',
    },
    { kind: 'h', text: 'Every number is a setting' },
    {
      kind: 'p',
      text: 'The stored path is pep.<name>; style numbers live in pep.style_a and pep.style_e so switching style never loses the other style’s numbers. A value outside its range is clamped, the clamp is logged and returned as a save warning, and the effective-settings row names it. The saved value is never rewritten. A legal 0 (fee $0, max box 0 = off, warm-up 0 = auto) is kept.',
    },
    {
      kind: 'cards',
      cards: [
        {
          title: 'Style A — ORB 5m breakout, enter immediately',
          tag: 'PDF p.1, p.4–10',
          body: 'Box: high/low of the first 5 minutes from 08:30 CT. The first side to trade THROUGH a box edge is entered. Stop = the full box, target = the full box, no trailing, one trade a day, flat at 15:00 CT. PDF result at 6 MNQ: $17,595.90 received (113 trades, 5 funded accounts used); at 3 MNQ $6,959.25 (121 trades). The shipped numbers on this page are the PDF’s.',
        },
        {
          title: 'Style E — EMA9 ORB 2m, candle-close confirmation',
          tag: 'PDF p.1, p.12–18',
          body: 'Box: high/low of the first 2 minutes from 08:30 CT. On a CLOSED 2m candle the EMA9 (2m candles, carried across sessions) crossing the box high is a buy, crossing the box low a sell. Stop = half the box rounded OUTWARD to a tick, target = the full box, no trailing, one trade a day, flat at 15:00 CT. PDF result at 6 MNQ: $10,070.10 received (106 trades, 3 funded accounts used); at 3 MNQ $3,773.25 (115 trades).',
        },
        {
          title: 'What the PDF numbers are, and are not',
          tag: 'PDF p.1, p.19',
          body: 'Money received after the 90/10 split, before purchase and evaluation fees, on a 122-session sample (20/10/2025 to 19/04/2026), from a trade-print proxy. Not a promise of fills or future results. The database holds 110 usable days of the 122, so the backtest is compared day by day, never by total.',
        },
      ],
    },
    {
      kind: 'knobs',
      knobs: PEP_KNOBS,
    },
    { kind: 'h', text: 'How it was checked against the PDF' },
    {
      kind: 'p',
      text: 'cmd/pep-backtest replays the same kernel/pep code over the stored MNQ 1m history (2025-10-20 to 2026-04-17) and compares it day by day with the PDF’s 6-contract ledgers. The imported 1m bars are close-stamped (one minute late) and are shifted back 60 seconds first; 2m candles are built from 1m on even Chicago minutes; a stop is filled only when a bar trades through it; the same bar reaching both stop and target counts the stop; the position is closed at the last price before 15:00. The database holds 110 usable days of the PDF’s 122 (17 days around the December and March contract rolls are missing), so the comparison is per day, never by total. The comparison and its mismatches are written to pep-backtest-vs-pdf.md.',
    },
  ],
}
