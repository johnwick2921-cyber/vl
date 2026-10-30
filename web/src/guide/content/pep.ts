import { GUIDE_BUILT_REV, type GuideSection } from '../types'

// Pep mode (owner order 2026-10-29). Source: the owner's PDF "A + E — Full
// calculation" (pep-spec-coa707.pdf). Every number below is the Go default in
// store/pep.go (PepResolved) / kernel/pep (DefaultConfig); the two are pinned
// equal by kernel/pep TestDefaultConfigEqualsResolvedStoreDefaults.
//
// This section deliberately has NO `knobs` blocks: per-knob cards must name the
// real Studio control, and the Studio panel ships in the pep web wave. The
// settings are listed in a table keyed by the exact stored path instead.
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
          body: 'This build carries the settings (stored, validated, clamped and logged, shown in the effective-settings rows), the pure evaluator and the stored-history backtest. The order path that places a pep trade, the 15:00 flat on a live position and the Studio panel ship in the pep trader wave. Until that wave is merged, turning pep ON places no order. The integrator updates this callout when it lands.',
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
    { kind: 'h', text: 'Every number is a setting' },
    {
      kind: 'p',
      text: 'The stored path is pep.<name>; style numbers live in pep.style_a and pep.style_e so switching style never loses the other style’s numbers. A value outside its range is clamped, the clamp is logged and returned as a save warning, and the effective-settings row names it. The saved value is never rewritten. A legal 0 (fee $0, max box 0 = off, warm-up 0 = auto) is kept.',
    },
    {
      kind: 'table',
      title: 'Pep settings (default / range)',
      head: ['Stored path', 'Default', 'Range / vocabulary', 'What it does'],
      rows: [
        ['pep.pep_enabled', 'off', 'true / false', 'Master switch.'],
        ['pep.pep_style', 'A', 'A | E', 'Which style runs.'],
        [
          'pep.pep_contracts',
          '3',
          '1 to 10 (hard ceiling 10)',
          'Contract count; the 2-contract AI clamp does not apply to pep.',
        ],
        [
          'pep.pep_box_start_ct',
          '08:30',
          'HH:MM, America/Chicago',
          'Box start (DST-safe).',
        ],
        [
          'pep.pep_flat_time_ct',
          '15:00',
          'HH:MM after the box start',
          'Time exit at the last price. The session flat (14:45) is not used.',
        ],
        ['pep.pep_tick_size', '0.25', '> 0', 'MNQ tick.'],
        [
          'pep.pep_sl_rounding',
          'outward',
          'outward | nearest',
          'How the stop distance is rounded to a tick.',
        ],
        [
          'pep.pep_max_trades_per_day',
          '1',
          '1 to 10',
          'Entries per CT day (style A takes the first break only).',
        ],
        [
          'pep.pep_fee_per_contract_usd',
          '1.0',
          '>= 0',
          'Round-trip fee per contract. Backtest and reporting only.',
        ],
        [
          'pep.pep_max_box_points',
          'off (0)',
          '>= 0; 0 = off',
          'Refuse a box wider than this many points.',
        ],
        [
          'pep.pep_signal_max_age_min',
          '2',
          '1 to 30',
          'Staleness limit for a signal and for the newest bar.',
        ],
        [
          'pep.style_a.pep_a_box_minutes',
          '5',
          '1 to 120',
          'Style A box length.',
        ],
        [
          'pep.style_a.pep_a_sl_fraction',
          '1.0',
          '> 0, at most 10',
          'Stop as a fraction of the box.',
        ],
        [
          'pep.style_a.pep_a_tp_fraction',
          '1.0',
          '> 0, at most 10',
          'Target as a fraction of the box.',
        ],
        [
          'pep.style_a.pep_a_both_edges',
          'no_trade',
          'no_trade | nearest_open',
          'One 1m bar through both edges.',
        ],
        [
          'pep.style_e.pep_e_box_minutes',
          '2',
          '1 to 120',
          'Style E box length.',
        ],
        [
          'pep.style_e.pep_e_sl_fraction',
          '0.5',
          '> 0, at most 10',
          'Stop as a fraction of the box.',
        ],
        [
          'pep.style_e.pep_e_tp_fraction',
          '1.0',
          '> 0, at most 10',
          'Target as a fraction of the box.',
        ],
        ['pep.style_e.pep_e_ema_period', '9', '2 to 200', 'EMA period.'],
        [
          'pep.style_e.pep_e_ema_tf_min',
          '2',
          'a divisor of 60',
          'Candle minutes, built from 1m on even CT minutes.',
        ],
        [
          'pep.style_e.pep_e_signal_source',
          'ema',
          'ema | close',
          'ema: the EMA crosses the box edge (owner ruling). close: the candle close crosses it.',
        ],
        [
          'pep.style_e.pep_e_warmup_bars',
          '0 (= 3 x period)',
          '0 to 5000',
          'Complete candles needed before the EMA may signal.',
        ],
      ],
    },
    { kind: 'h', text: 'How it was checked against the PDF' },
    {
      kind: 'p',
      text: 'cmd/pep-backtest replays the same kernel/pep code over the stored MNQ 1m history (2025-10-20 to 2026-04-17) and compares it day by day with the PDF’s 6-contract ledgers. The imported 1m bars are close-stamped (one minute late) and are shifted back 60 seconds first; 2m candles are built from 1m on even Chicago minutes; a stop is filled only when a bar trades through it; the same bar reaching both stop and target counts the stop; the position is closed at the last price before 15:00. The database holds 110 usable days of the PDF’s 122 (17 days around the December and March contract rolls are missing), so the comparison is per day, never by total. The comparison and its mismatches are written to pep-backtest-vs-pdf.md.',
    },
  ],
}
