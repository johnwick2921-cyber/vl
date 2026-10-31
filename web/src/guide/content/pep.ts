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
const SHARED = 'Strategy Studio → Pep mode accordion → “Shared by all styles”'
const RULES =
  'Strategy Studio → Pep mode accordion → “Style rules” (editable in Custom; read-only, locked to the PDF, in A and E)'
const TAB_A =
  'Strategy Studio → Pep mode accordion → “Style rules” → Style A (locked, read-only)'
const TAB_E =
  'Strategy Studio → Pep mode accordion → “Style rules” → Style E (locked, read-only)'
const TAB_C =
  'Strategy Studio → Pep mode accordion → “Style rules” → Style Custom (editable)'
// m461 (owner ruling 2026-10-30, "A or E all follow their rule, cannot change any
// way, they add custom"): A and E are the PDF and nothing stored changes them;
// the resolver store.ResolvePep ignores a saved A / E / rule number for them. A
// saved number stays in the database untouched, and is simply not used.
const LOCKED_AE =
  'LOCKED to the PDF since 2026-10-30: this value cannot be changed for style A or E, a saved number (if any) is ignored and stays in the database untouched, and the Studio shows it read-only. For different numbers choose the Custom style.'
const ONE_SAVE =
  'No. One value per strategy; saved with the strategy, applied when the running trader reloads on save.'

const knob = (k: KnobSpec): KnobSpec => ({ live: true, ...k })

const PEP_KNOBS_RAW: KnobSpec[] = [
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
    what: 'Which style trades: A (5-minute ORB breakout, enter immediately) or E (EMA9 ORB, candle-close confirmation), both fixed to the owner’s PDF, or Custom, the one style whose every number you can edit.',
    trader:
      'A and E are locked to the PDF: the Studio shows their rules read-only and the bot ignores any number saved for them. Custom starts as a copy of style E’s PDF numbers, so a fresh Custom trades like E until you change it. Switching style never deletes a saved number.',
    consumer: CONSUMER,
    range: 'A | E | Custom (case-folded where it enters)',
    systemDefault: 'E',
    recommended: `E: the owner’s shipped default (2026-10-30, “keep default E 6”). The PDF’s result at 6 MNQ is larger for style A ($17,595.90 against $10,070.10 received, ${PDF}, p.1) but A’s full-box stop is the bigger loss per trade; E’s stop is half the box.`,
    whenToTouch:
      'When you want the immediate breakout (A) instead of the EMA-confirmed entry (E).',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Contracts',
    settingId: 'pep.pep_contracts',
    where: SHARED,
    what: 'How many MNQ contracts each pep entry trades.',
    trader:
      'Editable in EVERY style (A, E and Custom). The 2-contract AI clamp does not apply to pep. The full-box stop of style A makes a loss large: size it against your daily limit.',
    consumer: CONSUMER,
    range:
      '1 to 10 (hard ceiling 10: a bigger saved number is clamped and logged, never honoured)',
    systemDefault: '6',
    recommended: `6: the owner’s shipped default (2026-10-30, “keep default E 6”), the PDF’s headline size. SIM only. From the 2026-10-30 integrated backtest (style A, 110 traded days, 46 of them stopped out; pep-backtest-integrated-1030) a full-box stop cost $832.50 median and $1,959 worst per losing trade at 6 MNQ ($12 a point); the widest box in that data, 244.5 points on 2026-03-02, would have cost $2,934 had it been stopped. The shipped default, style E (2-minute box, half-box stop; 107 traded days, 63 of them stopped out, same run), cost $336 median and $801 worst per losing trade at 6 MNQ: a stop-out is smaller but more frequent. Size it against your daily limit.`,
    whenToTouch: 'When you decide the size after watching SIM days.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Box start (CT)',
    settingId: 'pep.pep_box_start_ct',
    where: RULES,
    what: 'The time of day the opening box starts, Chicago time (DST-safe).',
    trader:
      'Style A and E: locked at 08:30. Only the Custom style reads the saved value. The box is the high/low of the first N minutes from this time.',
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
    where: RULES,
    what: 'The time exit: any pep position still open is flattened at the last price.',
    trader:
      'Style A and E: locked at 15:00. Only the Custom style reads the saved value. This is pep’s own flat, not the session flat of 14:45 (which pep does not use). It must be after the box start or the shipped pair runs. On a shortened CME day (the session calendar: the day after Thanksgiving and Christmas Eve, 12:15 CT close; Labor Day 2026, 12:00 CT close) the flat is pulled in to 5 minutes before the close, 12:10 CT (11:55 CT for a 12:00 CT close), and no entry is taken from then on; the card shows that time. A flat time you saved earlier than that is kept.',
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
    where: RULES,
    what: 'How a stop distance that is not on a tick is rounded: away from the entry (outward) or to the nearest tick.',
    trader:
      'Style A and E: locked at outward. Only the Custom style reads the saved value. Outward can only widen the stop, never tighten it.',
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
    where: RULES,
    what: 'How many pep entries are allowed per Chicago day.',
    trader:
      'Style A and E: locked at 1. Only the Custom style reads the saved value. Style A takes the first break only, whatever this says. The count is kept on disk before the order is sent, so a restart cannot fire a second entry.',
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
    where: RULES,
    what: 'Custom only: refuse to trade a box wider than this many points. The A and E rules have no cap.',
    trader:
      'Style A and E: no cap (the PDF trades every box). Only the Custom style reads the saved value. A wide box means a wide full-box stop. 0 is OFF (the PDF trades every box); when set, the refusal is named pep_box_too_wide.',
    consumer: CONSUMER,
    range: '0 to 100000; 0 = OFF',
    systemDefault: 'off (0)',
    recommended: `OFF to match the PDF; the 2026-10-30 integrated backtest measured the stored boxes at: style A (5-minute box, 110 traded days) 80.0 pts median, 121.75 pts 90th percentile and 244.5 pts widest; style E (2-minute box, the shipped default, 107 traded days) 59.75 pts median, 99.25 pts 90th percentile and 150.75 pts widest (both widest on 2026-03-02). The 90th percentile is the nearest-rank value: sort the boxes and take the ceil(0.9 × days)-th (the 99th of 110 for A, the 97th of 107 for E). Read a limit against the style that runs: a limit of 121.75 (A’s 90th percentile) would have cut only 2 of E’s 107 boxes. It is the risk dial — pep-backtest-integrated-1030.`,
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
      'It must divide an hour: a value that does not (7, 8 ...) runs the shipped 2; a value below 1 runs 1 and above 60 runs 60 (the eff line says so in each case).',
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
  // ── Custom (m461): the editable style, seeded from style E's PDF values ──
  knob({
    label: 'Entry type — Custom',
    settingId: 'pep.style_custom.pep_c_entry_type',
    where: TAB_C,
    what: 'Which entry rule the Custom style trades: ema_cross (E-like) or breakout (A-like).',
    trader:
      'ema_cross waits for the EMA (or the candle close) to cross a box edge on a CLOSED candle and enters at that candle’s close; breakout enters immediately on the first trade through a box edge. Only the rows of the chosen type show in the Studio.',
    consumer: CONSUMER,
    range: 'ema_cross | breakout',
    systemDefault: 'ema_cross',
    recommended:
      'ema_cross: Custom is seeded from style E’s PDF values, so a fresh Custom trades like E until you change it.',
    whenToTouch:
      'When you want an A-like immediate breakout with your own numbers.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Box length (min) — Custom',
    settingId: 'pep.style_custom.pep_c_box_minutes',
    where: TAB_C,
    what: 'Length of the Custom opening box, from the Custom box start.',
    trader:
      'A box with a missing 1m bar is not drawn and no trade is taken that day.',
    consumer: CONSUMER,
    range: '1 to 120 minutes',
    systemDefault: '2',
    recommended: `2: a copy of style E’s box (“ORB 2 PHÚT”, ${PDF}, p.1).`,
    whenToTouch: 'To test another opening range on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Stop (× box) — Custom',
    settingId: 'pep.style_custom.pep_c_sl_fraction',
    where: TAB_C,
    what: 'Custom stop distance as a fraction of the box, rounded per “Stop rounding”.',
    trader:
      'At 1.0 the stop is the opposite edge of the box. With “Stop rounding” = outward the distance is rounded away from the entry to a valid tick, so rounding can only widen the stop.',
    consumer: CONSUMER,
    range:
      'greater than 0, at most 10 (the Studio box takes 0.05 or more; the server itself accepts any value above 0)',
    systemDefault: '0.5',
    recommended: `0.5: a copy of style E’s half-box stop (${PDF}, p.1).`,
    whenToTouch: 'To test another stop size on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Target (× box) — Custom',
    settingId: 'pep.style_custom.pep_c_tp_fraction',
    where: TAB_C,
    what: 'Custom target distance as a fraction of the box.',
    trader:
      'The stop and target sit at NT8 from the entry; there is no trailing and no break-even.',
    consumer: CONSUMER,
    range:
      'greater than 0, at most 10 (the Studio box takes 0.05 or more; the server itself accepts any value above 0)',
    systemDefault: '1',
    recommended: `1.0: a copy of the PDF’s full-box target (${PDF}, p.1).`,
    whenToTouch: 'To test a nearer or farther target on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'One bar through both edges — Custom',
    settingId: 'pep.style_custom.pep_c_both_edges',
    where: TAB_C,
    what: 'Custom, entry type breakout only: what to do when ONE 1m bar trades through both box edges.',
    trader:
      'no_trade fails closed (pep_bar_through_both_edges); nearest_open guesses that the edge nearer the bar’s open broke first. Not shown (and not read) for ema_cross.',
    consumer: CONSUMER,
    range: 'no_trade | nearest_open',
    systemDefault: 'no_trade',
    recommended:
      'no_trade: a 1m bar cannot order the two touches, so guessing would be invented data.',
    whenToTouch: 'Only to bracket a backtest; not for live SIM days.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'EMA period — Custom',
    settingId: 'pep.style_custom.pep_c_ema_period',
    where: TAB_C,
    what: 'Custom, entry type ema_cross only: the EMA length on the EMA candles.',
    trader:
      'Computed across sessions and never reset daily; until the warm-up candles exist the signal is refused (pep_ema_not_warm).',
    consumer: CONSUMER,
    range: '2 to 200',
    systemDefault: '9',
    recommended: `9: a copy of style E’s “EMA9” (${PDF}, p.1).`,
    whenToTouch: 'To test another EMA on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'EMA candle (min) — Custom',
    settingId: 'pep.style_custom.pep_c_ema_tf_min',
    where: TAB_C,
    what: 'Custom, entry type ema_cross only: the candle size the EMA is computed on, built from 1m bars on even Chicago minutes.',
    trader:
      'It must divide an hour: a value that does not (7, 8 ...) runs the shipped 2; a value below 1 runs 1 and above 60 runs 60 (the eff line says so in each case).',
    consumer: CONSUMER,
    range: 'one of 1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30, 60',
    systemDefault: '2',
    recommended: `2: a copy of style E’s “khung 2 phút” (${PDF}, p.1).`,
    whenToTouch: 'To test another candle size on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Signal source — Custom',
    settingId: 'pep.style_custom.pep_c_signal_source',
    where: TAB_C,
    what: 'Custom, entry type ema_cross only: what crosses the box edge, the EMA itself or the candle close.',
    trader:
      'ema: the EMA crossing from <= the box high to > it is a buy, from >= the box low to < it a sell. close: the candle close crosses the edge instead.',
    consumer: CONSUMER,
    range: 'ema | close',
    systemDefault: 'ema',
    recommended:
      'ema: a copy of style E’s rule (the owner’s ruling of 2026-10-29).',
    whenToTouch: 'To compare the two readings on SIM.',
    perSession: ONE_SAVE,
  }),
  knob({
    label: 'Warm-up candles (0 = auto) — Custom',
    settingId: 'pep.style_custom.pep_c_warmup_bars',
    where: TAB_C,
    what: 'Custom, entry type ema_cross only: how many complete EMA candles must exist before the EMA may signal.',
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

/** A and E knobs are LOCKED (m461): the card says so in its own words and its
 *  range reads "locked", not an editable range. */
const PEP_KNOBS_AE_LOCKED = (k: KnobSpec): KnobSpec =>
  k.settingId?.startsWith('pep.style_a.') ||
  k.settingId?.startsWith('pep.style_e.')
    ? {
        ...k,
        trader: `${LOCKED_AE} ${k.trader}`,
        range: `locked to the PDF value ${k.systemDefault} (not editable; Custom has the editable range)`,
        whenToTouch: 'Never: locked. Use the Custom style for other numbers.',
      }
    : k

export const PEP_KNOBS: KnobSpec[] = PEP_KNOBS_RAW.map(PEP_KNOBS_AE_LOCKED)

export const pep: GuideSection = {
  id: 'pep',
  num: 20,
  title: 'Pep Mode',
  tagline:
    'Two FIXED opening-range rules from the owner’s PDF, A (5-minute breakout) and E (EMA9 cross), plus Custom, the one editable style. Separate from mentor mode.',
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
          title: 'Which Risk Control settings pep ignores',
          body: 'With pep ON the AI places no entry (no LLM call). The AI-only Risk Control knobs do not apply: break-even and trailing (pep has neither; the stop and target sit at NT8 from the entry) and the entry R:R / minimum-stop-vs-ATR checks (pep’s stop and target ARE the box). The Risk Control page does not grey the controls out for pep, but their grey “eff” lines do say it: break-even and trailing read “not applied while pep mode is ON”, the minimum-confidence and the contracts-per-order lines read “not used in pep mode” (the contracts line names pep_contracts, the size pep actually sends), and the planner stamp size reads “not used”. Still enforced for pep: daily guardrails, the session breaker, the owner pause, maintenance hold, feed-down, data-integrity, contract roll and the SIM-only block.',
        },
        {
          title: 'Build status of this page',
          body: 'This build carries the settings (stored, validated, clamped and logged, shown in the effective-settings rows), the pure evaluator, the stored-history backtest, the trader wiring (the entry order, the durable one-trade-per-day record, the 15:00 CT flat, the pep data window and the pep-truth endpoint, GET /api/traders/:id/pep-truth, plus a trading_mode field on /api/my-traders) AND the web side (the Trading mode selector and Pep mode panel in the Studio, the mode chip, the Pep card and the box on the chart in the dashboard). Pep is OFF on every trader until you switch it on; it places SIM orders only.',
        },
      ],
    },
    { kind: 'h', text: 'The two styles' },
    {
      kind: 'table',
      title:
        'Style A and style E (PDF "A + E — Full calculation"): fixed, not editable',
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
      text: 'The style-E box is two minutes, not five. The PDF’s own style-E ledger rows equal the first two minutes’ range: on 2025-10-20 the E target is 73.0 points, which is the high-low of the 08:30 and 08:31 bars, while the style-A box that day is 86.75. Styles A and E are LOCKED to these PDF rules (owner ruling 2026-10-30: they follow their rule and cannot be changed): the bot ignores any number saved for them, and the Studio shows them read-only. The A and E rules have no box cap, one trade a day and a 15:00 CT flat. Only Contracts (and the tick, the fee and the stale-signal age, which are not rules) stay editable in every style. For any different number, use the Custom style.',
    },
    { kind: 'h', text: 'The Custom style (the editable one)' },
    {
      kind: 'p',
      text: 'Custom is the style you can change. It starts as a copy of style E’s PDF values (2-minute box from 08:30 CT, EMA9 on 2m candles, signal source ema, stop half the box rounded outward, target the full box, 1 trade a day, flat 15:00 CT), so a fresh Custom trades like E until you edit it. You choose the entry type, ema_cross (E-like: a cross of the EMA or the candle close over a box edge on a closed candle) or breakout (A-like: the first trade through a box edge, enter immediately), and then every number: box start and length, EMA period, EMA candle size and signal source (ema_cross only), the stop and target fractions of the box (the stop is rounded away from the entry to a tick when Stop rounding is outward), what to do when one bar trades through both edges (breakout only), the flat time, the trades per day and the optional maximum box size. The maximum box size applies to Custom only: the A and E rules have no cap. Contracts are shared and editable in every style (ceiling 10, default 6).',
    },
    { kind: 'h', text: 'How a pep trade is placed' },
    {
      kind: 'table',
      title: 'From a closed 1m bar to the wire',
      head: ['Step', 'What happens'],
      rows: [
        [
          'Evaluate',
          'Every FINAL 1m bar (and the 2-minute cycle as a fallback) runs the pure kernel/pep evaluator on this trader’s own instrument (never a hard-coded MNQ). It reads the trade count from the DATABASE every pass, so a restart or a Studio save never resets it.',
        ],
        [
          'Wire check',
          'The bracket is built from the evaluator’s distances around the live price and judged on the WIRE-ROUNDED prices (stop rounded away from the entry, target to the nearest tick). Refused when the live price is more than half the stop distance from the signal’s reference entry (pep_slippage), when rounding distorts the reward:risk by more than 10% (pep_rr_distorted), or when a side collapses (pep_bracket_wrong_side). These two tolerances are fixed safety rails, not settings.',
        ],
        [
          'Admission',
          'The one entry chain, on its own pep path: feed down, dead-man, frozen, boot integrity, data integrity and data truth (on pep’s own window and reader), owner pause, maintenance hold, install pending, contract roll, the daily guardrails, the CME calendar and the stale-feed check all still apply. Every other entry (AI, chat, planner arm, Picture) is refused while pep is ON (pep_ai_entries_off), and a pep entry on a trader that is not in pep mode is refused (pep_not_enabled).',
        ],
        [
          'Durable claim',
          'After admission and BEFORE anything is sent, the day’s trade is written to the database. A refusal before the wire releases it; once the order is about to reach the wire the slot stays consumed whatever happens next. A record that cannot be read or written refuses the entry (pep_day_unreadable / pep_day_unwritable).',
        ],
        [
          'The order',
          'ONE market order that carries its own stop and target to NT8 (never two resting stops, so it can never fill both ways). The size must be exactly pep_contracts: the 2-contract AI clamp does not apply, and a sizing that is not that number is refused, never shrunk or grown.',
        ],
        [
          'After the fill',
          'Nothing moves the stop or target: no break-even, no trailing, no thesis-death exit, no drawdown emergency close, no AI close. A broker position that pep did not open is never flattened by pep and blocks its entry (pep_broker_not_flat).',
        ],
        [
          'The 15:00 CT flat',
          'pep’s own time exit (pep_flat_time_ct), checked every minute on the wall clock, not only on a bar: it cancels any stray resting order of the trader, then closes the position at market through the trader close path (that close cancels the native stop and target bracket afterwards, as the existing end-of-day flat does), and checks flatness against the broker on the next beat for 15 minutes; a position still open after 3 attempts raises a P0 alert. The session flat at 14:45 CT never acts on a pep trader. No new entry is admitted at or after the flat time (pep_after_flat_time). On a shortened day (a 12:15 CT close in the session calendar gives 12:10 CT; a 12:00 CT close gives 11:55 CT) the flat time is 5 minutes before the halt: a fixed safety margin, not a setting, so the flat is never sent into a closed market.',
        ],
      ],
    },
    {
      kind: 'p',
      text: 'Data window: pep reads the opening box and, for style E, the EMA warm-up, so its entry data check covers from the box start minus the warm-up and a 6-minute margin up to now (30 open minutes at least), not the 12,000-minute window the AI path reads. A hole in the box or the warm-up pauses pep; an old hole pep never reads does not. The window starts at a FIXED time (the box start minus the warm-up) and grows with the clock, so a hole inside it does not slide out of it by itself: the pause lasts the rest of that Chicago day unless the missing minutes are filled in (imported), and the log and the owner push say so instead of promising a clear time. The next day’s window starts after it. Pep rests no entry order, so a pause withdraws nothing. Data-truth blocks scoped to the pep or mentor reader apply to pep (it reads the same raw 1m bars as the mentor); a block that only the AI indicators read does not.',
    },
    {
      kind: 'p',
      text: 'A strategy with pep ON together with mentor mode or the day plan is refused when the trader starts: one P0 alert (pep_mode_conflict), one counted refusal, the trader marked not running. It is never silently resolved in favour of either mode.',
    },
    { kind: 'h', text: 'It fails closed' },
    {
      kind: 'table',
      title: 'Named refusals (faults are counted; waiting states are not)',
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
          'One 1m bar traded through both edges (style A): a 1m bar cannot say which came first. The style-A rule is no_trade (locked); Custom, entry type breakout, can choose nearest_open.',
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
          'Normal waiting states, not faults, and not counted as gate blocks.',
        ],
      ],
    },
    { kind: 'h', text: 'Where to set it' },
    {
      kind: 'p',
      text: 'Strategy Studio, futures strategies only. (1) Risk Control accordion → “Trading mode”: AI planner, Mentor or Pep. Choosing one turns the others off in the same write, and Pep and Mentor ask first. (2) Pep mode accordion: the shared block (the style picker A / E / Custom, Contracts, tick, fee, stale-signal age), then the “Style rules” of the chosen style. Style A and E show their PDF rules READ-ONLY with a “locked to the PDF” badge and note; only Custom shows editable inputs. Every field shows a concrete value (never “blank = default”) and a grey “eff” line under it: the value the bot RUNS, from the SAVED strategy. Every save sends the whole pep block, so a stale stored number can never survive an edit. Pep is a futures mode and is hidden for a crypto strategy. With Pep ON, the Day Plan block shows a warning and will not turn the plan on (it can always be turned off); the server refuses the combination either way. On the dashboard a mode chip (AI / Mentor / Pep A|E|Custom, or “Mode conflict” if mentor and pep were both saved ON) sits beside the strategy name, the “Pep — what trades” card shows the box, the signal state, trades today, stop/target and the countdown to the flat time, and the box is drawn on the chart once the server names the instrument it reads. The box stays on the card and the chart for the rest of the day, after the trade is taken, while the position is open, while the flat is closing it, and after the flat time. A day with no evaluation yet (a weekend, a holiday, before the first bar) shows no box, no refusal and “no evaluation yet”, never an earlier day’s; the card’s header then reads “last ran Thu 2026-10-29 15:59:00 CT” (the day it really ran), never a bare “as of” time that would look like today; on a style E trader the waiting state reads “waiting for an EMA cross”, on style A “waiting for a break”. A chart line from a stale read carries the stale mark in your language.',
    },
    { kind: 'h', text: 'Every number is a setting' },
    {
      kind: 'p',
      text: 'The stored path is pep.<name>; the numbers live in pep.style_a, pep.style_e and pep.style_custom so switching style never deletes a saved number. Styles A and E are LOCKED: store.ResolvePep ignores every number saved for them (the style_a and style_e blocks and the rule leaves box start, flat time, stop rounding, trades per day and max box), the effective-settings rows read “locked to the PDF (style A|E)”, and the saved values stay in the database untouched, unused. Only Custom reads them. For Custom, a value outside its range is clamped, the clamp is logged and returned as a save warning, and the effective-settings row names it. The saved value is never rewritten. A legal 0 (fee $0, max box 0 = off, warm-up 0 = auto) is kept.',
    },
    {
      kind: 'cards',
      cards: [
        {
          title: 'Style A — ORB 5m breakout, enter immediately',
          tag: 'PDF p.1, p.4–10',
          body: 'Box: high/low of the first 5 minutes from 08:30 CT. The first side to trade THROUGH a box edge is entered. Stop = the full box, target = the full box, no trailing, one trade a day, flat at 15:00 CT. PDF result at 6 MNQ: $17,595.90 received (113 trades, 5 funded accounts used); at 3 MNQ $6,959.25 (121 trades). The shipped numbers on this page are the PDF’s and are locked.',
        },
        {
          title: 'Style E — EMA9 ORB 2m, candle-close confirmation',
          tag: 'PDF p.1, p.12–18',
          body: 'Box: high/low of the first 2 minutes from 08:30 CT. On a CLOSED 2m candle the EMA9 (2m candles, carried across sessions) crossing the box high is a buy, crossing the box low a sell. Stop = half the box rounded OUTWARD to a tick, target = the full box, no trailing, one trade a day, flat at 15:00 CT. Locked: it cannot be edited. PDF result at 6 MNQ: $10,070.10 received (106 trades, 3 funded accounts used); at 3 MNQ $3,773.25 (115 trades).',
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
      text: 'cmd/pep-backtest replays the same kernel/pep code over the stored MNQ 1m history (2025-10-20 to 2026-04-17) and compares it day by day with the PDF’s 6-contract ledgers. Imported 1m bars carry the close-stamp convention (one minute late) unless they are tagged open-stamped (epoch_floor_open, written by the fixed importer and the owner-run shift tool): only the old close-stamped rows are shifted back 60 seconds, an open-stamped row is read as it is; 2m candles are built from 1m on even Chicago minutes; a stop is filled only when a bar trades through it (a live resting stop at NT8 triggers on a touch, so a day whose bar only touches the stop can be a stop-out live where the replay shows a win, for example 2026-03-25 style E); the same bar reaching both stop and target counts the stop; the position is closed at the last price before 15:00. The database holds 110 usable days of the PDF’s 122 (17 days around the December and March contract rolls are missing), so the comparison is per day, never by total. The comparison and its mismatches are written to pep-backtest-vs-pdf.md.',
    },
  ],
}
