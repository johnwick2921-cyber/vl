import { GUIDE_BUILT_REV, type GuideSection, type KnobSpec } from '../types'

// Mentor mode (🧑‍🏫) — the owner’s mentor’s method, built as a per-strategy
// trading mode. Everything below is read from the lane branches that implement
// it (feat/mentor-eval, feat/mentor-isb, feat/mentor-boxes-swing,
// feat/mentor-p3). Items not yet merged are marked "live: no — coming".
const mentorKnobKeys: Record<string, { settingId: string; live: boolean }> = {
  'Mentor mode': { settingId: 'mentor_mode', live: true },
  'Base contracts': { settingId: 'mentor_base_contracts', live: true },
  'Confluence contracts': {
    settingId: 'mentor_confluence_contracts',
    live: true,
  },
  'Big contracts': { settingId: 'mentor_big_contracts', live: true },
  'Max contracts': { settingId: 'mentor_max_contracts', live: true },
  'Reduced contracts': { settingId: 'mentor_reduced_contracts', live: true },
  'SWING4H contracts': { settingId: 'mentor_swing4h_contracts', live: true },
  // UG-7 (release #24): resolved and then discarded (mentor_mode.go
  // mentorContractsFor `_ = spentCap`), so live: no — as the card text says.
  'Spent-day contracts': {
    settingId: 'mentor_spent_day_contracts',
    live: false,
  },
  'Trail timeframe': { settingId: 'mentor_trail_tf', live: true },
  'Runner exit (trail mode)': { settingId: 'mentor_trail_mode', live: true },
  'Key level timeframe': { settingId: 'Config.KeyLevelTFMinutes', live: false },
  'Key levels RTH only': { settingId: 'Config.KeyLevelRTHOnly', live: false },
  'Key level prune': { settingId: 'Config.KeyLevelPrunePts', live: false },
  'EMA 34 period': { settingId: 'Config.EMAPeriod34', live: false },
  'EMA 9 period': { settingId: 'Config.EMAPeriod9', live: false },
  'EMA timeframe': { settingId: 'Config.EMATFMinutes', live: false },
  'EMA 34 location timeframe': {
    settingId: 'Config.EMALocationTFMinutes',
    live: false,
  },
  'Touch band': { settingId: 'Config.TouchBandPts', live: false },
  'ISB buffer': { settingId: 'Config.ISBBufferPts', live: false },
  'Reverse ISB at EMA 9': {
    settingId: 'risk_control.mentor_tuning.isb_reverse_ema9_enabled',
    live: true,
  },
  '4h/1h gate only in the news + open windows (print day 07:20–08:30, open 08:30–09:30 CT)':
    {
      settingId: 'risk_control.mentor_tuning.htf_gate_news_only',
      live: true,
    },
  'Execution frame: 2m after 09:00 CT (1m 08:30–09:00) for ISB + PHL/PLH, EMA 9/34, boxes, trendlines and the X8 exit':
    {
      settingId: 'risk_control.mentor_tuning.exec_2m_after_30m',
      live: true,
    },
  // Course-entry knobs (release #25, fix/mentor-course-entries).
  '15m inside-bar box needs a 5m ISB': {
    settingId: 'risk_control.mentor_tuning.15m_ib_requires_5m_isb',
    live: true,
  },
  '5m/15m box conflict end': {
    settingId: 'risk_control.mentor_tuning.tf_conflict_end',
    live: true,
  },
  'News day: wait beyond the 4h line': {
    settingId: 'risk_control.mentor_tuning.news_wait_both_lines',
    live: true,
  },
  'News day: freeze the pre-print 1h/4h read': {
    settingId: 'risk_control.mentor_tuning.news_bars_excluded',
    live: true,
  },
  'ISB rest box escape test': {
    settingId: 'risk_control.mentor_tuning.isb_box_escape',
    live: true,
  },
  'EMA 34 back after a loss': {
    settingId: 'risk_control.mentor_tuning.ema34_loss_relock',
    live: true,
  },
  'ORB edge veto': {
    settingId: 'risk_control.mentor_tuning.orb_edge_veto_pts',
    live: true,
  },
  'ORB-edge ISB candle fraction outside': {
    settingId: 'risk_control.mentor_tuning.orb_isb_fraction_outside',
    live: true,
  },
  'ORB escape: completely outside': {
    settingId: 'risk_control.mentor_tuning.orb_escape_completely',
    live: true,
  },
  'Box-return 5m trend source': {
    settingId: 'risk_control.mentor_tuning.d43_trend_source',
    live: true,
  },
  'Level entries need the 5m trigger side': {
    settingId: 'risk_control.mentor_tuning.level_entries_need_trigger_side',
    live: true,
  },
  'Trendline is a target': {
    settingId: 'risk_control.mentor_tuning.trendline_is_target',
    live: true,
  },
  'Trendline reject between two boxes': {
    settingId: 'risk_control.mentor_tuning.trendline_between_boxes',
    live: true,
  },
  'Leg reset on a wave break': {
    settingId: 'risk_control.mentor_tuning.leg_reset_on_wave_break',
    live: true,
  },
  'Flag wall as a target': {
    settingId: 'risk_control.mentor_tuning.flag_wall_target',
    live: true,
  },
  'Box new zone after escape': {
    settingId: 'risk_control.mentor_tuning.box_new_zone_after_escape',
    live: true,
  },
  'PHL min candles from old extreme': {
    settingId: 'Config.PHLMinCandlesFromExtreme',
    live: false,
  },
  'PHL target shy': {
    settingId: 'risk_control.mentor_tuning.phl_target_shy_pts',
    live: true,
  },
  'PHL entry buffer': { settingId: 'Config.PHLEntryBufferPts', live: false },
  'Wick microscalp': { settingId: 'Config.WickMicroscalpEnabled', live: false },
  'Stop ceiling': { settingId: 'Config.StopCeilingPts', live: false },
  'Room multiple': { settingId: 'Config.RoomMultiple', live: false },
  'Near-box room': {
    settingId: 'risk_control.mentor_tuning.near_box_room_multiple',
    live: true,
  },
  'Near-box skip also gates PHL/PLH': {
    settingId: 'risk_control.mentor_tuning.near_box_room_phl',
    live: true,
  },
  '2m ISB stacking: cancel candle': {
    settingId: 'risk_control.mentor_tuning.isb_stack_cancel_candle_2m',
    live: true,
  },
  'ISB tiny-stop exception': {
    settingId: 'risk_control.mentor_tuning.isb_stack_tiny_stop_pts',
    live: true,
  },
  'Evening slow: cutoff': {
    settingId: 'risk_control.mentor_tuning.evening_slow_pts',
    live: true,
  },
  'Evening slow: start hour': {
    settingId: 'risk_control.mentor_tuning.evening_slow_from_hour',
    live: true,
  },
  'Evening slow: trailing window': {
    settingId: 'risk_control.mentor_tuning.evening_slow_window_min',
    live: true,
  },
  'ISB size cut: old high/low band': {
    settingId: 'risk_control.mentor_tuning.isb_old_extreme_pts',
    live: true,
  },
  'ISB size cut: in-range gap': {
    settingId: 'risk_control.mentor_tuning.isb_in_range_gap_pts',
    live: true,
  },
  'Mid-range gap': { settingId: 'Config.RangeGapPts', live: false },
  'Day gate spent threshold': {
    settingId: 'risk_control.mentor_tuning.day_gate_spent_pts',
    live: true,
  },
  'Day gate target cap': {
    settingId: 'risk_control.mentor_tuning.day_gate_target_cap_pts',
    live: true,
  },
  'Swing EMA period': { settingId: 'SwingCfg.EMAPeriod', live: false },
  'Swing line offset': { settingId: 'SwingCfg.LineOffsetPts', live: false },
  'Swing stop distance': {
    settingId: 'SwingCfg.StopBeyondLinePts',
    live: false,
  },
  'Swing max stop': {
    settingId: 'risk_control.mentor_tuning.swing_max_stop_pts',
    live: true,
  },
  'Swing min stop': {
    settingId: 'risk_control.mentor_tuning.swing_min_stop_pts',
    live: true,
  },
  'Swing entry buffer': { settingId: 'SwingCfg.EntryBufferPts', live: false },
  'Swing target EMA period': {
    settingId: 'SwingCfg.TargetEMA5mPeriod',
    live: false,
  },
  'Swing leeway candles': { settingId: 'SwingCfg.LeewayCandles', live: false },
  'Swing hold bars': { settingId: 'SwingCfg.Hold4hBars', live: false },
  'Swing respects 5m zone': {
    settingId: 'SwingCfg.Respects5mZone',
    live: false,
  },
  'Box timeframe': { settingId: 'BoxCfg.TF', live: false },
  'Box touch band': { settingId: 'BoxCfg.TouchBandPts', live: false },
  'ORB entry gate': {
    settingId: 'risk_control.mentor_tuning.orb_gate_enabled',
    live: true,
  },
  'Leg budget': { settingId: 'mentor_leg_budget_enabled', live: true },
  'Leg reset on': { settingId: 'mentor_leg_reset_on', live: true },
  'Level revisit distance': {
    settingId: 'mentor_lvl_revisit_min_pts',
    live: true,
  },
  'EMA 34 cross cap (30m)': {
    settingId: 'mentor_ema_max_cross_30m',
    live: true,
  },
  'Loss departure distance': {
    settingId: 'mentor_loss_departure_pts',
    live: true,
  },
  '5m trigger at locations': {
    settingId: 'mentor_loc_trigger_filter',
    live: true,
  },
  'Trigger school': {
    settingId: 'risk_control.mentor_tuning.trigger_school',
    live: true,
  },
  'Ping-pong minimum gap': {
    settingId: 'risk_control.mentor_tuning.ping_pong_min_gap_pts',
    live: true,
  },
  'Ping-pong candle maximum': {
    settingId: 'risk_control.mentor_tuning.ping_pong_candle_max_pts',
    live: true,
  },
  'Ping-pong candle lookback': {
    settingId: 'risk_control.mentor_tuning.ping_pong_candle_lookback',
    live: true,
  },
  'Maximum level visits per day': {
    settingId: 'risk_control.mentor_tuning.level_max_visits',
    live: true,
  },
  'Done after a winning day': {
    settingId: 'mentor_done_after_win',
    live: true,
  },
  'Stop after a loss': { settingId: 'mentor_stop_after_loss', live: true },
  'Trading window start': { settingId: 'mentor_window_start', live: true },
  'Trading window length': { settingId: 'mentor_window_minutes', live: true },
  'Stale-data block': { settingId: 'mentor_stale_data_block', live: true },
  'Swing: 5m EMA 34 is the first target': {
    settingId: 'mentor_swing_ema34_is_first_target',
    live: true,
  },
  'Swing runner': { settingId: 'mentor_swing_runner', live: true },
  'Resonance runner TP past the old high': {
    settingId: 'mentor_resonance_runner_tp_old_high',
    live: true,
  },
  'Break-even before the 1:1 partial': {
    settingId: 'mentor_be_before_partial',
    live: true,
  },
  'ISB candle-3 partial at any profit': {
    settingId: 'mentor_isb_c3_partial_any_profit',
    live: true,
  },
  'Reverse ISB as PHL/PLH': {
    settingId: 'mentor_reverse_isb_as_phl',
    live: true,
  },
  'EMA 9 visit cap': { settingId: 'mentor_ema9_visit_cap', live: true },
  'ISB max stop': { settingId: 'mentor_isb_max_pts', live: true },
  'Key-level pair candle ratio': {
    settingId: 'mentor_keypair_candle_ratio',
    live: true,
  },
  'Fresh RTH box pair': { settingId: 'mentor_box_fresh_rth_pair', live: true },
  'Max box height': { settingId: 'mentor_box_max_height_pts', live: true },
}

// Evaluator settings that have no Studio control and no store field: the
// evaluator ships them as code constants (kernel/mentor DefaultConfig,
// SwingCfg, BoxCfg). They are documented, not tunable from the app, so their
// cards say "live: no" and "default, code constant" instead of pointing at a
// control that does not exist.
const codeConstantNote = {
  where: 'Default, code constant — no Studio control',
  whenToTouch:
    'Not tunable from the app: this is the default the evaluator ships with; changing it takes a code change.',
}

// Stored strategy settings the bot READS but the Studio has NO control for
// (UI-GUIDE-TRUTH T16, release #22): the only Studio mentor controls are the
// mode switch, the window start/length, stop-after-loss (RiskControlEditor)
// and the "Mentor method numbers" panel (MentorTuningPanel). Pinned against
// those two files by mentorKnobWhere.test.ts.
export const MENTOR_NO_STUDIO_CONTROL = [
  'mentor_base_contracts',
  'mentor_confluence_contracts',
  'mentor_big_contracts',
  'mentor_max_contracts',
  'mentor_reduced_contracts',
  'mentor_spent_day_contracts',
  'mentor_trail_tf',
  'mentor_leg_budget_enabled',
  'mentor_leg_reset_on',
  'mentor_lvl_revisit_min_pts',
  'mentor_ema_max_cross_30m',
  'mentor_loss_departure_pts',
  'mentor_loc_trigger_filter',
]

function noStudioControlWhere(settingId: string): string {
  return `No Studio control — stored strategy setting ${settingId}; the app has no field for it, so the shipped default applies unless the saved strategy config already carries a value`
}

function withMentorKnobKeys(knobs: KnobSpec[]): KnobSpec[] {
  return knobs.map((knob) => {
    const metadata = mentorKnobKeys[knob.label]
    if (!metadata) return knob
    const isConstant =
      !metadata.live && /^(Config|SwingCfg|BoxCfg)\./.test(metadata.settingId)
    if (isConstant) return { ...knob, ...metadata, ...codeConstantNote }
    if (MENTOR_NO_STUDIO_CONTROL.includes(metadata.settingId)) {
      return {
        ...knob,
        ...metadata,
        where: noStudioControlWhere(metadata.settingId),
      }
    }
    return { ...knob, ...metadata }
  })
}

export const mentor: GuideSection = {
  id: 'mentor',
  num: 17,
  title: 'Mentor Mode',
  tagline:
    'The mentor’s method: three setups, five locations, hard filters, small and patient.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    {
      kind: 'callout',
      title: 'SIM only, OFF by default',
      items: [
        {
          title: 'This is a Simulated-mode strategy',
          body: 'Mentor mode trades on NinjaTrader SIM, never live. mentor_mode is per strategy and defaults OFF; while OFF, nothing in this package is consulted and the bot is byte-identical.',
          cite: 'Owner setting — SIM only, OFF by default · trader/mentor_mode.go mentorEnabled · kernel/mentor/mentor.go Config.Enabled',
        },
        {
          title: 'Entries are stop orders only — never limit',
          body: '"mình KHÔNG BẮT LIMIT nữa… chỉ có set lệnh BUY STOP, SELL STOP thôi." You wait at a level and a stop order puts you in; no chasing, no market entries.',
          cite: 'LEVELS ruling 2026-10-03',
        },
        {
          title: 'Entries become stop-LIMIT',
          body: 'Every mentor entry goes out as a stop-LIMIT with the limit at the stop price ("đừng đặt buy stop market nữa… đặt buy stop limit đi") — always, no setting turns it off. Each order carries its own expiry: an ISB expires at the close of the next candle of the frame it was read on (m406), and while each following candle\u2019s BODY stays inside I1 the evaluator keeps it to the next close (stack candles 2 and 3; a body outside I1 or a 4th candle cancels it) — the keep is decided BEFORE the expiry sweep runs on that candle (until release #22 the sweep ran first and cancelled 16 kept ISB orders); the swing at the close of the current 4h candle (the order is dropped only AFTER the 4h candle\u2019s last 5m bar has been checked for a fill, because NinjaTrader keeps the order live until that instant: a fill on that last bar is a real position and gets its break-even at +1R and its one-4h-candle hold-close, release #53 m437); a LEVEL (PHL/PLH/EMA) order is stamped to rest until the next 15:00 CT — the RTH window end, an inference, no lesson states it — or until a later candle closes through the level, whichever comes first. The close-through is read on the frame the order was placed on (a closed 2m/5m candle for a 2m/5m order, never a 1m wick inside a still-forming candle) and it is a cancel and nothing else: the touch it was placed off is spent, so the same level is not re-armed off the same candle on the next tick — only a NEW touch candle can trade the level again [Ngày 2.3 part1 @21:24, @23:48–24:02]. The armed pass cancels an unfilled order when it lapses. ON TOP of every expiry, the EOD flat cancels every INTRADAY mentor order (everything except the SWING4H) whenever no session window is open: from the NY close at 14:45 CT until the 17:00 CT reopen. The same pass then closes every open INTRADAY position (at market by default) — an entry that already filled does not ride past the flat either; only the SWING4H position is exempt (see "EOD flat" in the exits). The ASIA/LONDON/NY windows count for this whether or not their trading switch is on, and a registered half-day pulls the 14:45 earlier. So in practice a level order never lives past 14:45 CT: one placed in the morning is cancelled at 14:45 the same day, 15 minutes before its own 15:00 stamp; one authored between 14:45 and 17:00 is cancelled on the same pass, within milliseconds (rows 256, 282, 283 and 343 died 1–3 ms after they were written); one placed in the evening or overnight rests until 14:45 CT the next trading day. No NEW mentor entry is placed from 15:45 CT (the lead into the 16:00 daily halt) until the 17:00 reopen. A mentor entry without an expiry is refused, never sent as a stop-market. Other (non-mentor) orders keep their own order types.',
          cite: 'D1.4 p1 @ 24:41–24:55 (stop-LIMIT, never stop-market) · D4.2 p2 @ 08:21–09:01 (ISB keep while inside I1) · trader/armed_event_pass.go (mentor pass before the armed pass) · 15:00 RTH-end = inference, no lesson · trader/mentor_tick.go mentorIntentExpiry · kernel/mentor/eval.go levelArmCancels · EOD cancel: trader/auto_trader_clock.go enforceEODFlatAt + trader/armed_executor.go cancelIntradayMentorArms · EOD position close: enforceEODFlatAt → flattenPosition, SWING4H skipped by trader/mentor_mode.go isSwingPosition · windows from kernel/session_registry.go DefaultSessionRegistry (NY flat 14:45, ASIA open 17:00) · 15:45 cutoff: trader/mentor_mode.go mentorPastDailyHaltCutoff · pinned by trader/guide_mentor_lifetime_test.go',
        },
      ],
    },
    {
      kind: 'callout',
      title: 'Mentor — what trades (live truth panel)',
      items: [
        {
          title: 'Read-only live evaluator state',
          body: 'The planner page and the trader dashboard show a card titled "Mentor — what trades". It is read-only and reports what the mentor evaluator holds RIGHT NOW: the 4h trigger direction and since-when, the 1h trigger, the 5m trigger (price + since), the HTF verdict (follow / sit-out / no-trigger) and whether the HTF gate is active (htf_gate_news_only), the mentor levels in effect (price, a readable kind name, drawn-at, visits today — the 4h trigger line and the wick-microscalp extreme are tagged "target only": they join the level set for the target ladder but the touch loop never trades them; the 1h trigger line is not a target and is not listed (release #52 B5)), the history depth line, and the next-window / day-stop state (done-after-win, stop-after-loss, window — a -1 window reads "no time window" and still names the 15:45 CT entry cutoff and the 14:45 CT EOD flat; it never means any hour). It never mutates the evaluator — it reads under the existing locks and never blocks the tick.',
          cite: 'Engineering rule — read-only truth panel (release #10), not from the course · trader/mentor_truth_panel.go MentorTruthSnapshot · api/handler_mentor_truth.go · target-only kinds: kernel/mentor/eval.go (the touch loop skips KindHTFTrigger / KindWickMicroscalp) · web/src/components/mentor/MentorTruthCard.tsx MENTOR_TARGET_ONLY_KINDS · web/src/components/mentor/mentorWindowLabel.ts',
        },
        {
          title: 'The AI planner bias is advice-only when mentor is ON',
          body: 'When mentor mode is ON, the AI planner bias card is re-labelled "AI planner — advice only (mentor mode places the trades)". The mentor engine places the entries; the AI planner only supplies the bias. When mentor mode is OFF the bias card keeps its unchanged label.',
          cite: 'Engineering rule — frontend label (advice-only), not from the course · web/src/components/plan/BiasBlock.tsx adviceOnly · web/src/components/plan/PlanCard.tsx',
        },
        {
          title: 'Live refresh + what the chart draws',
          body: 'The card refreshes every 30s and on window focus, and stamps each snapshot "as of HH:MM:SS CT" (server as_of_ms). The dashboard price chart draws, in mentor amber, every place the evaluator holds on the latest tick: key levels, the old high and the old low (the one nearest unbroken pair — the same two lines the bot trades, never merged or folded), the EMA 9/34 curves (the evaluator\'s EMA on the mentor\'s execution frame — 1m for the first 30 minutes after the 08:30 CT open, then 2m — drawn bar by bar, each point read only after its own bar has closed, and labelled with the frame the engine uses NOW: "1m EMA 9"/"1m EMA 34" or "2m EMA 9"/"2m EMA 34", so a 1h/4h chart never reads them as that chart\'s own EMA; never a flat line), the EMA 34 location as a short right-edge segment labelled "EMA34 location (2m, current value)" (or 1m) at its CURRENT value (never a full-width flat line through history), the 5m trigger line, the 4h trigger line (the 1h line decides direction only and is no longer drawn, release #52 B5), each FTGH/FTGL box as a filled amber RECTANGLE from the bar it formed on to its end (or the last bar while it is live; the box top and bottom are its edges, not two full-width lines), the ISB rest boxes (5m/15m/30m high and low), the ORB high/low with its escape state, the swing line (the warm 4h EMA 34) and a filled swing leg\'s SL/TP — the SL is the stop WORKING at NinjaTrader, read from its order book, so after the +1R break-even move it is drawn at the entry and labelled "break-even (NT8)"; when the book cannot be read the line is the stop the leg was entered with and says "entry stop (NT8 book unread)" — and every trendline as a sloped segment: a FORMING line (two swings, no 3rd touch yet) is thin, dashed and labelled "waiting for 3rd touch" — at most the newest 3 forming lines per side are drawn; a VALID line (3rd touch) is solid and extended to the right edge, labelled "trend line ✓" (a valid line inside a box is labelled "not traded" and is drawn thin/dashed); EVERY line — forming, valid, traded or not — extends to the right edge ("kéo dài": the mentor extends the line to see where price meets it), only a traded valid line is solid; its points sit on the candles the chart actually drew, on every timeframe. These mentor lines belong to ONE instrument (the trader\'s, e.g. MNQ): they are drawn only on that symbol\'s chart, and any other symbol\'s chart says "mentor lines hidden — they belong to MNQ". After a restart (for example over a weekend with the market closed) the overlays — key levels, EMA curves, boxes and trendlines — show from the LAST CLOSE, read-only, labelled "Chart overlays as of last close HH:MM CT" until the first new bar arrives. If a refresh fails the last lines stay drawn and every drawn level or trendline reads "stale since HH:MM CT" (the EMA curves are rebuilt each refresh). Working orders are drawn from the order ledger, for the chart\'s symbol only: a mentor stop-entry is an orange "Stop entry SELL 3 · SL … · TP …" line at its ENTRY price, never a red "SL" at its stop — and a split mentor entry names BOTH take-profits, "TP1 <leg 1 TP> ×<leg 1 contracts> · TP2 <runner target> ×<the rest>" (row 362: TP1 30725.25 ×2 · TP2 30703 ×1); a planner limit draws with its entry zone. Nothing is drawn that the bot does not hold. The open position\'s working stop and target (read from the NT8 order book through /api/positions) are drawn as lines, and its entry arrow sits at the real open time; when the book cannot be read nothing is drawn. The chart\'s timeframe buttons include 2m (between 1m and 3m): 2m candles are built from the 1m bars on even Chicago-time minutes, the same buckets the mentor trades, and the still-forming candle is shown unfinished.',
          cite: 'Engineering rule — frontend refresh + chart overlays (release #22 T5/T7; release #24 UG-1..UG-6), not from the course · web/src/components/mentor/useMentorTruth.ts · web/src/components/charts/chartOverlays.ts (mentorChartLines, mentorTrendSegments, mentorEMACurves, mentorStaleMark, sameChartSymbol, orderPriceLines) · web/src/components/charts/ChartTabs.tsx · trader/mentor_truth_panel.go mentorChartObjects / mentorEMASeries / mentorSwingStopTruth · trader/class33_boot_sweep.go ledgerOpenOrders · kernel/mentor/eval.go State.ChartLevels · kernel/mentor/ema.go emaSeriesChart / ChartEMA',
        },
        {
          title:
            'Old highs/lows: ONE old high and ONE old low — the chart draws what trades',
          body: 'The bot keeps exactly one old high and one old low: the NEAREST UNBROKEN old high above price and the nearest unbroken old low below it. They are picked from ALL the 5m/15m swing pivots of the lookback (12 h of 5m, 24 h of 15m; every pivot price is a 1m wick) — release #23: the bot used to keep only the newest 3 per side BEFORE testing them, so an older unbroken high behind 3 broken ones was invisible (10-13 08:30: old low 30613.25 missing); a pivot drops out the moment a later closed 1m candle trades beyond it (a wick above an old high breaks it — the candle need not close). The candle being judged is measured against the extremes that stood BEFORE it. Either side can be empty — at a fresh high of the window there is no old high above. The panel and the chart draw exactly these two lines, never folded into a key level and never pruned: the lines you see are the lines the bot sizes, targets and builds legs from. (Before release #22 the bot used 12 raw pivots, most already broken, while the chart drew a cleaned 3–5.)',
          cite: '§6 ladder "nearest old high → EMA 34 → next level" [D3.3 p1 @04:11, 07:12] · §2.1 rule 2 [D4.1 p1 @08:05 written] · law 2 / D3.4 p1 @08:19 (a break needs no close) · kernel/mentor/eval.go swingOldExtremeLevels · kernel/mentor/level_display.go DisplayLevels',
        },
        {
          title: 'Fold it away',
          body: 'Click the card title to fold it away or open it again; the page remembers your choice.',
          cite: 'Engineering rule — frontend UI (fold), not from the course · web/src/components/mentor/MentorTruthCard.tsx localStorage vl.mentorCard.open',
        },
        {
          title: 'Stop moves are verified against the broker',
          body: 'Every mentor stop move is re-checked against the broker\u2019s own order book (order_snapshot every 30 s) — an NT8 refusal replies with NO ack, so a sent frame is not proof it applied. Applied → "🧑‍🏫 stop move CONFIRMED at NT8". A broker stop TIGHTER than wanted is kept as-is, never widened. Not applied (wider) → "🚨 STOP MOVE NOT APPLIED at NT8" + ONE P0 per episode + ONE re-send of the same price. Still not applied → the bot adopts the broker\u2019s actual stop as its belief, fires one "manage by hand" P0, and latches the leg — no automatic stop moves until a snapshot shows the broker matching the belief again. A stale book delays the verdict, never guesses. The exit-driver line now prints broker=… beside stop=….',
          cite: 'Engineering rule — broker-book verification, not from the course · trader/mentor_exit_drive.go mentorVerifyPendingStopMoves · provider/ninjatrader/order_snapshot.go',
        },
      ],
    },
    {
      kind: 'h',
      text: 'The three setups',
    },
    {
      kind: 'cards',
      cards: [
        {
          title: 'Inside bar (ISB)',
          body: 'Candle 2’s BODY inside candle 1’s FULL range (wicks included). Direction = candle 1’s colour (“nó chỉ có 2 màu thôi, nó không có màu thứ 3” [D1.4 p1 @09:20–09:38]); a DOJI candle 1 has no colour, so the bot REFUSES it — the course is silent on a doji, this fail-closed refusal is OURS (CTO ruling 2026-10-03; the old doji-long was audited wrong) and awaits an owner ruling. Entry at the ISB candle’s own extreme + 1.5 pt (6 ticks) — exactly that on the wire, nothing is added at placement — stop 1.5 beyond its opposite extreme, no fixed stop size — reduce the size for a 20–25 pt stop and skip above 25. The FIRST inside-bar candle is I1 — the order sits at its extremes and the containment test is against I1. A following candle that does not fill and whose BODY is NOT inside I1 → cancel at once. Body inside I1 → keep. I1 counts as candle 1 of the stack; candles 2 and 3 inside → keep; the 4th not filling → cancel (it becomes a 5m inside bar = rest). The course counts 1m candles; on the 2m the same 4 is OURS (knob isb_stack_cancel_candle_2m), and an optional tiny-stop exception (isb_stack_tiny_stop_pts, OFF) keeps a very small-stop arm past the cancel candle.',
          tag: 'D1.4 p1',
        },
        {
          title: 'PHL / PLH — the pullback',
          body: 'Uptrend pullback → buy stop at the previous candle’s high, stop at that candle’s low, target NEAR the old high (5 pts short of it — the 5 is ours, knob phl_target_shy_pts). Downtrend bounce → the exact mirror. The pullback stop can ROLL: with NO drawn level touched, the buy/sell stop is re-armed to EACH closed pullback candle’s extreme until it fills — "Mình ra một lệnh buy stop trên đỉnh của từng cây nến đã đóng xong" [D2.2 p1 @02:44–03:34, 11:05–11:36]. This rolling entry is ON by default (release #52): it starts only when the pullback SHOWS — a RED candle in an uptrend, a GREEN candle in a downtrend [D2.2 p1 @01:52–01:57, @10:47–10:51] — and, like every entry, it must agree with the 5m trigger line (against it, or past the wrong side of it, the roll is refused and a resting stop is cancelled) [X8 @06:25, @08:37–08:48]. A with-trend candle mid-pullback does not roll or start a stop and leaves a still-valid resting stop alone, but if the leg died (the 4h side flipped, the trigger now disagrees, the higher low broke) the resting stop is cancelled on that candle too. It follows the 4h direction (inside the 08:30–09:30 CT open window and the news period the 4h/1h gate also applies, so a 1h trigger opposite the 4h sits the roll out; outside them the 4h alone is the trend — the roll has no clock window of its own, and a resting roll is not cancelled when the open window closes), and it needs a nearest old high/low, the 1–2 candle wait and a higher low. It obeys the same location bans as every PHL/PLH: inside a standing 5m/15m/30m ISB box, between an FTGL and an FTGH, mid-range between two levels, next to a box (the nearest box edge in the trade direction closer than 2 x the candle’s range) or when the candle is as big as the rank to the next key level, the roll is refused (counted as rolling_<name>, e.g. rolling_phl_5m_box_ban) and a resting stop is cancelled [D3.4 p2 @07:27–10:35; D3.2 p2 @08:34–08:55, @21:53–23:08; D2.2 p3 @14:03–16:03; X4 @06:31]. A level that has left the level set no longer "owns" the entry, so a stale touch can never switch the roll off for the rest of the day. A candle TOUCHING A DRAWN LEVEL is a different entry: the FIRST touching candle is the reference and never rolls [D2.3 p1 @17:55–19:12; D5.3 p1 @22:10]. Wait 1–2 more pullback candles; a higher low is required (a flat low is an FTGL, not a PHL). The old high is the nearest UNBROKEN old high, and its role is fixed when it is drawn: a PHL (long) only ever targets an old HIGH and a PLH (short) an old LOW — an old low is never a long’s target. Inside a standing 5m (or 15m/30m) ISB box, or between an FTGL and an FTGH, there is no PHL/PLH at all (only the ISB) [D3.4 p2 @07:27–10:35; D3.2 p2 @08:51].',
          tag: 'D2.2 p1',
        },
        {
          title: 'SWING4H — 4h EMA 34',
          body: 'The line is the 4h EMA 34 known at the start of the current 4h bar (17:00 CT anchor), drawn BEFORE price gets there. If a 4h bucket that the CME calendar says was OPEN has no bars in the stored 1m tape (a data hole, not a weekend or a halt), the line is missing one fold and is wrong by a few tenths of a point (about 0.4 pt during the 10-28 swing trades): the bot marks the line STALE and refuses new SWING4H entries (counted swing_line_gap_stale, named on the boot line) until the hole is refilled and the line re-seeded; a hole older than about 68 folded 4h closes has decayed to nothing and is ignored (release #56, m456). A contract roll is not a hole: a minute that any stitched contract stores counts as present, and the shortened 4h buckets on the two DST-change days are read as the line folds them (release #56, m456). Switch straight down to the 5m and wait for a LITERAL touch. Reject (closes back on the approach side) → stop order tight on the candle, stop 30 beyond the line. Through-close → cancel, and the level is INVALID for the rest of that 4h candle (only an inside bar may trade it; a later touch that closes back arms no ordinary reject) — release #54: a close THROUGH the line is latched even when the one-setup-per-approach guard is still set from an earlier arm (it used to be skipped, and the next bar then armed a normal reject at the invalid level — 10-28 17:35 CT, trade 707); then a candle closes back across the line and, ONE 5m beat later, an inside bar of that close-back candle whose colour matches the side (red candle 1 → short, green → long) → 5m inside bar with the stop AT the line, ONE inside-bar entry per 4h candle (once it has fired, a later close-back plus inside bar in the same candle is not traded) [D5.2 p2 @20:41–21:04 · D5.2 p1 @21:25–21:40 · D1.4 p1 @09:20–09:36]. Stop at or beyond swing_max_stop_pts (default 100) → take 1–2 MNQ and sit: the entry is never skipped and the stop is never widened, but the size is capped at min(your swing size, 2) MNQ at the trader\u2019s rule gate — a larger swing size knob cannot override it (tier swing4h_wide_stop) [D5.2 p2 @21:25–21:43]. Under the ceiling the swing sizes at your normal swing size. One setup per approach until price leaves the line by the stop distance — the course only says the FIRST touching candle counts (“Kệ nó, nó đụng lần đầu là được”; never the 2nd candle) [D5.2 p3 (recovered) @11:24; p1 @13:26–13:37] and gives no re-arm rule, so the departure distance that re-arms the line is OURS and a re-touch is an open mentor question. Late in the day — an entry inside the LAST 4h bucket (13:00–16:00 CT normal; the shortened close’s bucket on early-close days) — late in the day the swing target is cut to at most 30 pts, but never below 1:1 — with a wider stop the target stays at 1:1 [derived: D5.2 p2 @07:00–07:58 “market sắp đóng cửa” → the last 4h bucket; 30 = top of 20–30 @07:21; 1:1 floor 03-CHECKLIST.md:160]. The line folds EVERY closed 4h candle — after a feed gap of 4h or more the candles that opened and closed unseen are folded from the bars on the next tick ("swing 4h EMA34 line folded N 4h closes after a feed gap"). A gap longer than the live slice (~25 h) hides closes the bot cannot read: the line is marked STALE and SWING4H entries are refused (counted swing_line_gap_stale) until a restart — or a history refill (see the re-seed below) — reseeds it from the store; never a guessed line.',
          tag: 'D5.2',
        },
      ],
    },
    {
      kind: 'h',
      text: 'The five locations',
    },
    {
      kind: 'table',
      title:
        'A PHL/PLH is only valid at one of these (the ISB may be anywhere — see the ruling below)',
      head: ['Location', 'Where it comes from', 'Note'],
      rows: [
        [
          'Key levels',
          '1H RTH colour changes — a line at the OPEN of the new-colour candle (never the wick), from the market open',
          'Full stored history of the current contract plus older contracts back-adjusted while a roll gap can be measured (today: from 2026-06-03, 2 contracts); pruned < 20 pts apart (keep the more recent); deleted when a 1H candle CLOSES through it',
        ],
        [
          'EMA 34',
          'The trading chart’s EMA 34 — on the frame you trade (R3, M401): 1m from 08:30 to 09:00 CT, 2m otherwise while "2m execution" is ON; 1m all day when it is OFF',
          'The 4h EMA 34 belongs to the SWING4H setup only',
        ],
        [
          '5m trigger-line retest',
          'A candle breaks the previous 5m extreme (wick included); the retest of that line is a location',
          'Between two opposing trigger lines: NO trade at all, ISB included',
        ],
        [
          'Box edge',
          'FTGH/FTGL boxes, drawn on the frame the bot TRADES — 1m regular candles from the 08:30 CT open to 09:00, closed 2m candles from 09:00 on while exec_2m_after_30m is ON (all-1m while it is OFF) [D4.1 p1 @22:57–23:21: "you trade the 2-minute, you draw the 2-minute"]; a box keeps the frame it was drawn on. A box entry trails, breaks even and runs the X8 exit on the frame the box was DRAWN on (m417). Paired by role with no tolerance — the extreme (lowest low / highest high) pairs with the NEXT confirmed swing AFTER it that fails to exceed it (a higher low / lower high); the trade is the 3rd touch',
          'A box edge is used again and again; an escape (a 1m BODY closes outside — always the 1m, also for a 2m-drawn box) does NOT kill the box — it stays and every later return trades [B4] until a newer box of the same role forms: one floor and one ceiling are live at a time [D3.2 p2 @09:14]. The first FTGH and the first FTGL of the trading day are latched until the 17:00 CT session roll — never redrawn on a later higher high / lower low; the one exception (owner 2026-10-25, m429): a fresh box that forms in RTH (after 08:30 CT) REPLACES the evening box of the same role, and a role with no fresh RTH box keeps its evening box [D4.1 p2 @02:39, @02:58]; the log says "box latched …" and "box latch cleared at the 17:00 CT session roll". A restart REPLAYS the latch from the 17:00 roll minute by minute, so it lands on the boxes the running bot drew (boot line "mentor seed: box latch replayed from the 17:00 CT roll … FTGH … · FTGL …") and judges only the returns that come AFTER the restart — the old ones are history. A latched box stands on EVERY tick, also when that minute\u2019s fresh scan would not draw its role (release #23: it used to vanish on those ticks — 50 on 10-14)',
        ],
        [
          'Trendline',
          'Two CONFIRMED two-sided structural swings (left fractal + the next bar confirms it), at least 5 bars apart (a floor that is OURS, not the course — the mentor only says a trendline must slope [D2.3 p1 @06:26–07:00]; an open question to him; a 1H/4H line needs 2 HTF bars; bars of the frame the line is drawn on: the 1m in the first 30 minutes after the 08:30 CT open, the closed 2m candles after that — the mentor draws his trendlines on the 2m, "Thường em sẽ vẽ trendline khung 2 phút" [X9 @03:35–03:49], and trades the 1m only until 09:00 [Routine trước khi trade @00:00:34–00:01:14]; exec_2m_after_30m OFF = the 1m all day; the break below still reads closed 5m candles), joined — a low and a HIGHER low (support) or a high and a LOWER high (resistance); never horizontal: two same-role swing wicks at (nearly) the SAME price are a flat SUPPORT (two lows) or RESISTANCE (two highs) level — drawn and traded as an old low / old high, labelled "support" / "resistance" — never a trendline, and "near" is the day\u2019s own measure, HALF the average high-low range of the last 20 closed candles of the traded frame (a quiet day ~4–5 pts, a run day 10–20 pts; owner ruling 2026-10-25, Q2). EVERY consecutive pair of today\u2019s qualifying swings draws a line, plus at most ONE non-adjacent line per side at a time (the most significant extreme joined to the later swing, angled and uncrossed; a better or continuing one replaces it), so a side never fans. An UNBROKEN drawn line stays all session — it is DELETED (not drawn, not a level) the moment a closed 5m candle is WHOLLY beyond it, and a 1H/4H trendline breaks the same way, on a 5m candle wholly beyond it, not on the 1H close (owner rulings R1/R3, 2026-10-25) — what counts as wholly beyond: (a support line: the whole candle, wicks included, below the line; a resistance line: the whole candle above it; a close beyond the line with a wick still back at it is NOT a break; the line is read at BOTH the candle\u2019s open and its close time and the candle must be beyond both, so on a sloped line a candle that traded on the line\u2019s own side earlier in its five minutes is not a break) — "n\u1ebfn n\u00f3 \u0111\u1ee5ng level m\u00e0 n\u00f3 \u0110\u00d3NG D\u01af\u1edaI level… n\u00f3 G\u00c3Y level" [D2.3 p1 @14:36–15:17] on the 5m [X9 @06:25] — or the session ends (owner ruling R4; "never erase during the session, delete at end of day" [D4.1 p2 @01:17]). A VALID line carries across the 08:30/09:00 frame switch until a 5m candle wholly beyond it breaks it; FORMING lines are redrawn on the new frame (m407). A pair that price crossed between its own two points (a 1m wick beyond the line by more than the 0.5-pt touch band) is not a trendline and is not drawn ("TRENDLINE chứ không phải CẦU TUỘT" [X9 @01:22]) — counted once per day as trendline_crossed_between_anchors. The log says when a line turns VALID, when one is BROKEN and when the day\u2019s lines are deleted',
          'Becomes a location only on the 3rd touch, and that touch must be a RETURN ("ph\u1ea3i V\u1ec0 TEST L\u1ea6N TH\u1ee8 3 th\u00ec m\u1edbi VALID" [D3.2 p2 @03:45]): price first LEAVES the line \u2014 a confirmed swing high above a support line, or a confirmed swing low below a resistance line, beyond the touch band \u2014 and then comes BACK to touch it — a REJECTION: the candle\u2019s low (high) comes within the 0.5-pt band of the line (0.5 is ours, not the course — the mentor says a near-touch is fine on the 1m and a real touch is needed on the 5m, with no number [Ngày 5.2 part2 @00:18:30–00:18:45]) and it closes on the line\u2019s own side; a wick that goes beyond the band, or a close through the line, is not a touch. The bars that confirm the 2nd swing and still sit on the line never count. A closed 5m candle WHOLLY beyond it discards it ("đóng dưới NGUYÊN CÂY" [X9 @06:28–06:45]; a close beyond the line alone is not a break; a break is never a reversal and NOT an entry). Box beats trendline: a trendline whose price sits inside a live box\u2019s [Bottom, Top] is NOT a location. A respected level (a key level, an old high/low or an FTGH/FTGL box edge — the mentor\u2019s own example is the failure-to-go-higher/lower box, c52 W9) within 2 pts of the line (the 2 pts is OURS, not the course: he gives no number) beats a FRESH trendline — but only on its FIRST test, the 3rd touch that just closed; a valid line past its 3rd-touch return has itself been respected and is no longer suppressed by a nearby level ("chọn cái mà nó đã respect nhiều lần thay vì … cái mà nó mới quay về, nó test lần đầu tiên … Đừng chọn cái nó mới tạo" [D2.3 p1 @02:19–03:45])',
        ],
      ],
    },
    {
      kind: 'callout',
      title: 'OWNER RULING — the ISB may be taken ANYWHERE',
      items: [
        {
          title: 'PHL/PLH still need a location',
          body: 'Only the ISB is anywhere. The PHL/PLH and the SWING4H still need their locations above.',
          cite: 'OWNER RULING 00:1x CT',
        },
        {
          title: 'No trade between a box and the trigger line',
          body: 'There is ONE 5m trigger line (it moves on a reversal), so there is no band “between two opposing trigger lines” — that was a misread. The no-trade zone is between an FTGL and the BUY trigger line (mirror: an FTGH and the SELL line): price must escape BOTH before anything trades, ISB included ("khỏi đánh, đợi nó thoát ra khỏi 2 cái").',
          cite: 'D3.4 p1 @16:38–17:19 · B1 ruling 2026-10-03 · kernel/mentor/eval.go triggerBoxZoneVerdict (not the swing: swingZoneGate only drops the wrong side of the one line)',
        },
        {
          title: 'The 5m trigger side still gates',
          body: 'After a buy trigger, entries are long only and above the line ("BẤT KỲ TRƯỜNG HỢP NÀO") — a short ISB above a buy line (or a long below a sell line) is refused. While a 5m ISB box stands, the box direction governs instead.',
          cite: 'D3.4 p1 @09:30–10:38 · D4.3 @14:40',
        },
        {
          title: 'Only same-direction inside a 5m ISB box',
          body: 'The latest 5m ISB candle is boxed; only a same-direction 1m ISB trades inside it.',
          cite: 'D3.4 p2 @09:09–09:39 — ISB rest box, same-direction only · ISB rest box filter — merged',
        },
        {
          title: 'Smaller size at an old high/low',
          body: 'An ISB right at an old high/low trades the reduced size (3 contracts), not the base size. "At" = the ISB candle reaches within ±2 pts (ours, not the course — knob isb_old_extreme_pts) of THE nearest unbroken old high or THE nearest unbroken old low (one line each — see "Old highs/lows" above). On 10-13 that is 20 of the 390 RTH candles (5.1%; 18 while the pivots were capped at 3 per side, before release #23); before release #22 the 12-pivot list flagged 183 of them (47%). Written rule 2 [D4.1 p1 @04:37–05:04: "ở ngay đỉnh hoặc đáy cũ… giảm size… là cái thứ 2"] — wired: the evaluator flags the ISB and the size table cuts it.',
          cite: 'D4.1 p1 @04:37–05:04 — "ở ngay đỉnh hoặc đáy cũ… giảm size" · trader/mentor_mode.go mentorSizeFor · kernel/mentor/eval.go isbFlags',
        },
        {
          title: 'Smaller size when the ISB is in a range',
          body: 'An ISB traded inside a range also trades the reduced size (3 contracts). "In range" means: between an FTGL below and an FTGH above; inside the standing 5m ISB rest box; between two key levels closer than the ping-pong minimum (50 pts — borrowed from the ping-pong gate, ours, not the course; knob isb_in_range_gap_pts); and — once built — a 15m ISB range. Written rule 3 [D4.1 p1 @04:37–05:04: "Khi trade isb in-range bắt buộc giảm size"]. The cut beats the confluence size. A spent day does NOT change the size (it cuts the target to 15 pts and the runner to 2 contracts instead).',
          cite: 'D4.1 p1 @04:37–05:04 — "Khi trade isb in-range bắt buộc giảm size" · trader/mentor_mode.go mentorSizeFor · kernel/mentor/eval.go isbFlagsFor',
        },
        {
          title: '5m pullback warning',
          body: 'In a 5m UPTREND (the 5m swing structure: higher high and higher low), a closed 5m candle that breaks the prior 5m candle’s low warns that a pullback is coming, so a LONG ISB sizes down to the reduced size (“market chuẩn bị pullback… hạn chế vô lệnh, giảm size lại cũng được” [Go over lớp học @00:14:50–00:15:02]). The mirror is built: in a 5m DOWNTREND a candle that breaks the prior candle’s HIGH warns a SHORT (“cũng ngược lại” [@00:15:05–00:15:17]). The warning is DIRECTION-AWARE since release #52: it cuts only the entry that trades WITH the 5m trend; a short taken in the uptrend’s own pullback (or a long in the downtrend’s bounce) is the trade the warning predicts and keeps its size. No clear 5m structure means no warning. Scope: ISB and reverse ISB only. The course calls the cut optional (“cũng được”) and the bot applies it as mandatory; extending it to PHL/PLH and making it optional are open owner calls.',
          cite: 'Go over lớp học part1 @00:13:21–00:15:19 · trader/mentor_mode.go mentorSizeFor · kernel/mentor/eval.go pullbackWarning5m / isbFlagsFor (side-aware) · kernel/mentor/box.go structureTrend5m',
        },
        {
          title: 'Skip a stop of 30 or more',
          body: 'An ISB stop above 25 pts must not be taken ("trên nữa thì thôi" [D3.3 p1 @02:04–02:08] — those orders are not worth trading). A stop of 20–25 pts cuts to the reduced tier ("giảm size" [D3.3 p1 @01:09–01:20]); a stop of 20 or less keeps the base size. This reverts course-defaults-30 (owner order "move to 30") — the course skips the twenties [D4.1 p1 @05:41].',
          cite: 'W-MENTOR-EXACT (owner order 10-21 "i want all exactly like mentor") — CourseExits.ISBMaxStopPts 25 · D3.3 p1 @01:09–02:09 (20–25 reduce, > 25 skip) · D4.1 p1 @05:41 (twenties skip)',
        },
      ],
    },
    {
      kind: 'callout',
      title: 'Rulings 2026-10-03 — locations, boxes, exits',
      items: [
        {
          title: 'Per-visit level references',
          body: 'A level reference lasts until a closed candle departs the level; every NEW visit (a closed candle not touching it since the last reference) gets its own reference candle and stop. One level no longer blocks itself for the whole day. A wrong-way close marks a level ISB-only (only ISBs may trade there): a moving line (EMA / trigger retest) clears that mark only when the visit departs (the touching candle is gone) — never on drift alone, because the EMA re-prices every tick; a key level clears it at the next session day. When the trader REFUSES the placement from a level or PHL/PLH touch (a gate, the size, the EOD-flat window…), that touch is spent for its reference bar: the same bar is not re-emitted every tick, and the level can trade again from the NEXT reference candle (a rolling PHL re-arms on its next reference candle as always) [D2.3 p1 @17:55–18:51: the first candle that touches is the reference candle; the order is never moved to the next candle].',
          cite: 'L1 — lvl_revisit_min_pts, default 0 · D5.2 p2 @20:48 (item 22)',
        },
        {
          title: 'Every PHL/PLH drop is named',
          body: 'A rejected touch that does not become a PHL/PLH always names its reason in the funnel line, counted ONCE per visit of the level (not every minute the touch stands): phl_5m_box_ban, phl_mid_range_boxed, phl_ema_crossing_30m, phl_ema_loss_block, phl_not_location, phl_no_reject_entry, phl_level_arm_resting — beside the existing phl_15m_box_ban, phl_30m_box_ban, level_visit_cap, room, near_box and the rest. Before release #22 the first five dropped silently. The PHL’s room and trigger-side drops (room, isb_trigger_side) and keypair_candle_too_big also count once per visit now (they counted every minute). level_invalid_first_touch counts each level the first touch invalidated by closing on the wrong side [D5.2 p1 @19:51] — course-correct, and now visible. The ORB logs its range once a day and its escape side + read time on the first escape and on every side change (“🧑‍🏫 ORB …”). The 15m/5m conflict also names every drop it causes (release #26): isb_tf_conflict (first_box), isb_tf_conflict_both_boxes (both_boxes, the held half), isbrev_tf_conflict (a reverse ISB) and phl_tf_conflict (a candidate PHL/PLH). The boot line also reads the key levels as "key levels N live · M deleted" — the old "levels 92" counted the deleted ones too (27 were live).',
          cite: 'Engineering rule — every refusal logged and counted · kernel/mentor/eval.go refuseOncePerVisit · kernel/mentor/seed.go SeedLine',
        },
        {
          title: 'Box ping-pong needs ≥ 50 pts',
          body: 'A reference candle touching one of the two boxes is NOT mid-range: touch the lower box → trade up, touch the upper → trade down — but only when the gap between the FTGL top and the FTGH bottom is at least 50 pts; below that the entry is refused (ping_pong_range_too_small). PHL/PLH anywhere ELSE between the boxes stays banned.',
          cite: 'D3.2 p2 @07:50–09:14 · D4.2 p2 @05:17',
        },
        {
          title: 'Every box return trades',
          body: 'The box is drawn from two touches — the extreme and the next confirmed swing after it that failed to exceed it ("lần thứ 3 mới vô lệnh": the 3rd touch is the trade). Each return visit gets a reference candle and the same reject rule (close OUTSIDE the box on the approach side = trade; close inside = cancel). EVERY return trades, the first one included. An escape does not delete the box; a box leaves the live set only at end of day or when a newer box of the same role forms (one floor and one ceiling are live at a time). After a body escape the role flips (Uno Reverse): a broken FTGH becomes support and a broken FTGL becomes resistance, and the next returns come from the new side with the same reject rule and gates. On the SAME closed bar, when a box-edge reject and an inside bar both fire on the same side, the mentor goes to the inside bar ("Nên về xuống InstaBar… em lại an toàn với InstaBar hơn") — the box reject is refused as phl_isb_preferred_at_box, but ONLY when the inside bar\'s stop is no larger than the box reject\'s (his reason is the smaller stop); otherwise both stay [D3.3 p1 @08:12–08:56].',
          cite: 'R1 · D3.2 p2 @06:25 · D14 slide 17 · X2 @02:36–03:25',
        },
        {
          title: '15m and 30m ISB rest boxes + the escalation ladder',
          body: 'Like the 5m ISB rest box, the latest CLOSED 15m and 30m inside-bar candles are boxed and extended right. Inside a standing box, nothing trades against that box\u2019s direction — a 1m ISB is gated on the box direction, PHL/PLH and box returns inside are banned. When a timeframe\u2019s ISBs are CROSSING (2 opposite ISBs within the last 3 closed buckets) that timeframe is unreadable and its box gate is skipped — the next timeframe up governs (5m → 15m → 30m). A 1m candle with its OPEN and CLOSE both beyond the edge deletes the box (isb_box_escape, default body; close = the older close-only test). While a 15m box stands, a 1m ISB in its direction also needs a 5m ISB in that direction (15m_ib_requires_5m_isb, default require_5m_isb).',
          cite: 'D4.1 p2 @07:35–08:11 · D4.2 p1 @16:28–16:46 · D3.4 p2 @17:07–17:49 · D4.1 p1 @20:52–21:02 (body escape) · D4.2 p1 @17:09–17:13 (5m ISB under a 15m box)',
        },
        {
          title: 'The 4h trigger line is a target, never a location',
          body: 'The 4h trigger line joins the target ladder as target-only: the next-level target can land on it when it sits between the entry and the next key level, but it is never a PHL/PLH location and the touch loop never classifies it. The 1h trigger line is NOT a target (release #52 B5): the course names the 4h line as the way back ("Về lại 4 giờ nè anh chị") and never the 1h; the 1h only decides direction.',
          cite: 'D4.4 p2 @05:04–05:44 · kernel/mentor/mentor.go KindHTFTrigger · kernel/mentor/eval.go (htf_4h_trigger level only; the touch loop skips KindHTFTrigger) · kernel/mentor/chart_seed.go · kernel/mentor/location.go levelIsLocation',
        },
        {
          title: 'Wick microscalp (advanced, OFF by default)',
          body: '2+ consecutive CLOSED 5m candles rejecting with wicks the SAME way (lower wicks in an uptrend, upper wicks in a downtrend) put a bounded target at their far wick — target-only, never a location. OFF by default ("đừng có tập khúc này đầu tiên").',
          cite: 'D4.3 @00:00–03:20, @09:27–09:46 · WickMicroscalpEnabled',
        },
        {
          title: 'Intraday confluence (exit C, size 5)',
          body: 'LONG = entry at an FTGL AND the 5m BUY trigger agrees; SHORT = entry at an FTGH AND the 5m SELL trigger agrees. The trigger is judged at the price you actually enter at (the rejecting candle’s high for a long, low for a short), NOT at the box edge — a buy line that sits between the box top and your entry counts, because you enter above it [D3.4 p3 @07:49–08:01; C52 W1]. No key-level condition [B3]. If you entered first and the trigger flips to your side afterwards, the trade upgrades to C and leg 1’s take-profit is moved from 1R out to 2R at NinjaTrader (a real modify on leg 1; a leg 1 that already took its 1R partial, or a one-bracket position, has nothing to move) — “vô rồi mới đợi trigger… hold at least risk reward 1-2” [D3.4 p3 @10:07–10:33, @11:01–11:05]. It also fires on timeframe agreement: the 15m ISB, the 5m ISB and the entry side all align (an ISB/PHL/PLH, not just a box) — the 5m trigger line is NOT part of this rule: D4.2 names only the three timeframes agreeing, opposite higher frames = no trade [D4.2 p1 @12:16–12:47, @14:30–14:52, @15:16–15:36; C52 W1 B9, owner order 2026-10-26]. Then: hold at least 1:2, the stop is never moved up (exit C), size 5 — confluence is a longer hold, it never adds contracts, and the big tier is removed [D3.4 p3 @07:52] (the course 1.1 L1103 “Cái sizing của em Cố định là 5 MNQ”; m186 reverts the 2026-10-16 “set to 4” flatten).',
          cite: 'R2 · D3.4 p3 @07:38, @07:49–08:01, @10:07–10:33 · D4.2 p1 @12:16–12:47, @15:16–15:36',
        },
        {
          title: 'A loss at a place blocks it',
          body: 'After a LOSS at a level, a box edge or the EMA line (entry filled, then the stop hit), that place is blocked until a departure candle. Two losses at one place → that place is off for the day (the “off for the day” part is OURS, stricter than the course: “thua 2 lần ở cùng 1 chỗ thì là mọi người đang sai” — and “đừng trade ở 1 khu vực, trade hoài ngay chỗ đó không có được cái gì đâu”; no day-long rule is stated, and the departure wait is OURS [D4.2 p2 @06:58–07:37]).',
          cite: 'E2 · G2 · D4.2 p2 @01:56',
        },
        {
          title: 'Leg budget — two entries per leg',
          body: 'A leg runs from the PHL/PLH to the old extreme and allows at most 2 entries (the PHL/PLH + a same-direction ISB). A stop-out inside the leg closes it; a NEW leg starts on a 1m CLOSE beyond the prior high (long) / low (short) — or, for a leg closed by a stop-out, on a 1m close beyond the losing trade\u2019s wave extreme (leg_reset_on_wave_break, default ON).',
          cite: 'G1 · X15 @00:36–02:46 · X-FOMO @06:10–06:22 (wave break)',
        },
      ],
    },
    {
      kind: 'h',
      text: 'The filters',
    },
    {
      kind: 'table',
      title: 'What keeps you out',
      head: ['Filter', 'Rule', 'Knob'],
      rows: [
        [
          'Trigger zone',
          'Between two opposing 5m trigger lines there is NO trade at all, ISB included — wait for it to escape both',
          '—',
        ],
        [
          'ISB rest box',
          'The latest 5m ISB candle is boxed and extended right; only a same-direction 1m ISB trades inside; a 1m candle with its open AND close beyond the edge deletes the box (isb_box_escape, default body; close = the older close-only test)',
          'isb_box_escape = body (default) · D4.1 p1 @20:52–21:02',
        ],
        [
          'Day gate (§7)',
          'Measure the Globex run (17:00 → 08:30 CT) before the open. Conflict on a spent day → the machine is OFF for the day (latched at 08:30). The day gate is frozen AT the 08:30 read: a restart after 08:30 RESTORES that decision (the seed replays the 08:30 4h/1h read and the frozen run, then replays the one-way DayOff recheck over the closed 1h bars since — only when mentor_day_off_recheck is ON; course-defaults-30 turns it OFF, so the seed replays no clear), so a restart never re-decides the day — the log line reads "RESTORED from the 08:30 read". Only when the 08:30 read is not possible (no bars closed by 08:30) does the line read "re-read at boot … not the 08:30 decision". Spent + agree → trade but cap the target at 15 pts; on a spent day skip any setup whose stop is over 15',
          'DayGateSpentPts 300 · DayGateTargetCapPts 15',
        ],
        [
          '4h/1h direction',
          'NEWS + OPEN (the mentor’s rule; #45 reverts owner decision A 2026-10-21 m134): the 4h/1h direction gate applies (1) on a red-folder PRINT day from the print window’s start (07:20 CT) all the way to the 08:30 CT open — the post-print news trade follows the 4h, a 4h/1h conflict sits out (W7, release #52: "có news hay không có news đi nữa thì mình nhắm theo khung 4 giờ … khung 4 giờ trigger buy mà khung 1 giờ trigger sell … nghỉ trade khúc đó" [D4.4 p1 @13:18–13:33, @18:03–18:52]); and (2) in the 08:30–09:30 CT open window (the 60 minutes are OUR number — the course gives no length; owner-accepted INVENTED, mentor question open). On a day with NO print the 07:20–07:35 window is ordinary trading and is not gated. Outside them, ordinary intraday entries are NOT HTF-gated — when there is no 4h trigger, when the 1h opposes the 4h, or when the entry side opposes the 4h. Scope: this gate covers ISB, reverse-ISB, PHL/PLH and box authoring only. SWING4H is EXEMPT (its own §8 path never consults the 4h/1h verdict), and planner/AI entries have their own G1 HTF veto. The live trader inherits this with no config edit (the default flipped back from all-day to news + open). Entries only WITH the 4h trigger. At the OPEN (08:30–09:30 CT) a with-4h entry additionally waits for the 5m range to break in the 4h’s direction — "không phải là cứ cắm đầu mà cứ vô lệnh buy market … chuyển về khung 5 phút … nó break được cái rank khung 5 phút" [D4.4 p2 @04:41–06:00]; the opening 5m range is the first 5m candle (08:30–08:35) [example: the ORB’s 08:30 anchor]. The 1h counts only when it fired at or after the 4h — compared by the MINUTE each line really fired, not by which candle opened first (an earlier 1h trigger is ignored — it is silent; W7 B3): 1h agreeing or silent → follow the 4h; 1h opposite → sit out until it flips. No 4h trigger → nothing to follow. The 4h/1h lines (and the 5m trigger line) are seeded from history at boot by replaying every stored 1m bar through the same intrabar break rule the running bot uses, so a restart lands on exactly the line a bot that never stopped would hold — including a candle that broke both sides (which side broke first decides). The boot line prints each line as "<tf> trigger <dir> @<price> since <CT>" (the minute the line fired; the 4h/1h lines carry the break minute since release #52, the 5m line its bucket open), and every 4h/1h move is logged as it happens ("4h trigger moved long @… → short @…"). On a red-folder print day the seed freezes each replayed day\u2019s print-window trigger read exactly as the running bot did (release #29: the read is frozen from the print to the 08:30 CT open — the candles are never rewritten; release #23 dropped the 07:20–07:35 print candles). History that reaches the store AFTER the boot — NinjaTrader\u2019s replay is held until the first live bar, and a refilled bar hole — re-seeds the trigger lines, the swing line, the EMA 34/9 on the execution frame (1m 08:30–09:00 CT, otherwise 2m) and the key-level deletion on the next tick, keeping every latch, arm, leg and visit count; the log line "🧑‍🏫 mentor re-seed after a history refill (…): CHANGED|UNCHANGED … before → after" names what moved (counted reseed_after_refill / reseed_changed). 10-14: the 08:22 boot seeded before the 03:44–07:47 hole reached the store and traded on 1h short @30790.75 instead of 30716.00.',
          '—',
        ],
        [
          'Room rule',
          'The room to the FIRST available level must be at least 2× the first take-profit — leg 1 at 1:1 → 2R; a confluence (mode C) leg 1 at 1:2 → 4R [D5.3 p1 @09:16–10:17 · D2.2 p3 @12:13 · D1.2 p1 @07:41–08:45]. Release #30: the EMA 9 / EMA 34 lines are PARTIAL points along the way, not the level the room (and the 1:1 floor) measure — "ta ghẹt tôi đầu tiên phải là về gần đỉnh cũ … tiếp tục là EMA 34 … những cái chỗ này là những cái chỗ anh chị bán bớt" [D3.3 p1 @05:15–07:38]; an EMA must not by itself kill the trade (the room/floor read the next non-EMA level — the nearest old high/low, a key level, or a trendline target; the EMA is the target only when no other level stands beyond). The room is measured to the LEVEL, also on a spent day: the 15-pt take-profit cap shortens the take-profit, never the room — "dù target của em là 15 điểm … từ điểm entry xuống target nó phải từ 30 điểm đổ lên" [D5.3 p1 @09:32–09:50]. So a spent-day setup needs stop ≤ 15 (the 1:1 floor on the capped take-profit), room ≥ 2R to the level, AND room to the level ≥ 2× the capped take-profit — his worked figure: target 15 → room 30 (a take-profit sitting right at the level is refused). A PHL/PLH stop never exceeds 25 pts (SWING4H exempt — its ceiling is 100)',
          'RoomMultiple 2 · StopCeilingPts 25 (PHL/PLH and box stops only) · mentor_isb_max_pts 25 (the ISB and reverse ISB only — the SAME knob the trader’s R8 gate reads; P2-1 one number) · swing_max_stop_pts 100',
        ],
        [
          'ORB gate (opening range)',
          'ORB = the high and the low of the FIRST 2-minute candle of the regular session (08:30–08:32 CT), drawn only once that candle has completed. NO trade inside the ORB and NO reversal trade at either edge. Trade only after price has LEFT the box — escape test: a 1m candle whose BODY is wholly outside, its open AND its close beyond the edge (the course minimum, the same test as the box escape [D4.1 p1 @20:36]; a candle that opens inside and closes one tick out is NOT an escape; the ORB source itself says "completely outside" [extra "Routine trước khi trade" part1 @06:47–07:16], so the wick-inclusive test is the knob orb_escape_completely, default OFF — the wick reading is an analogy to the X7 ping-pong box [X7 @09:21–11:07], the owner flips it). Direction follows the side of the LATEST escape: below → shorts only (ISB short / PLH); above → longs only. It is not locked for the day: a later 1m body wholly beyond the OTHER edge switches the side ("cho tới khi nào nó crack" [X7 @07:42–07:51]); a candle that only pushes back inside switches nothing. Every escape and every switch is logged with its candle. The escape picks the side; it is NOT an entry. No ORB for pre-market. The SWING4H is exempt. An ISB whose entry candle is not mostly OUTSIDE the range on the trade side is also skipped (the separate rule-5 card below, counter orb_isb_mostly_inside).',
          'orb_gate_enabled = true (default ON) · orb_escape_completely = false (default OFF) · X5 @01:52, 02:36, 02:57–03:47, 05:42 · X7 @07:42–07:51 · D4.1 p1 @20:36 (the FTGH/FTGL box minimum, the body test) · "Routine trước khi trade" part1 @06:47–07:16 (completely outside)',
        ],
        [
          'News 07:30 CT (print)',
          'On a red-folder 07:30 print day (from the day’s calendar), a mentor entry is refused through the print window (07:20–07:35 CT); if the calendar slice is missing or unreadable the window is held anyway (fail-closed, counted news_hold_no_calendar); on an ordinary day the window refuses nothing. At print −10m on a red-folder print day, live INTRADAY mentor arms are cancelled by ArmID and intraday mentor positions are flattened before the 07:30 print — the SWING4H is exempt (held by the 4h). The cancel-and-flatten at −10m is our conservative reading [B], not a course sentence: the course gives three reasons not to be IN the print (the candle jerks and "banh 2 đầu", the machine can lag, and you cannot size the candle) and "if you cannot get out, you are blown too" — it never says to close an open trade before it. Whether an open runner is held through the print is a question for the mentor. The 07:30 print candle never moves the 1h/4h trigger lines: the evaluator drops the print-window bars from the higher-timeframe feed, so a news spike is not a real break.',
          'F11 · R12 · D4.4 p1 @18:13, @22:15 · the −10m flatten is an inference [B] from D4.4 p1 @20:44–21:46 (10-04 CTO re-read DAY-4 row D4.4-08), not a course instruction',
        ],
        [
          'Execution frame: 2m after 09:00 CT (1m 08:30–09:00) for ISB + PHL/PLH, EMA 9/34, boxes, trendlines and the X8 exit',
          'ON by default — the named setups (ISB + PHL/PLH) are read on the 1m chart ONLY during the RTH open +30 minutes (08:30–09:00 CT — "a 2m stop cannot cover the setup at open volume"); at every other minute they are the 2m ("khung 2 phút ÍT BỊ QUÉT RÂU" — the 1m wicks sweep stops, so the 2m read is quieter), overnight and the Europe open (3am Texas) included. A SLOW evening/overnight (15:00–03:00 CT, the 1m range since the RTH close under 100 pts) reads the 5m instead ("Trade khung 5 phút only … cho buổi tối" [X11 @17:40–18:32, quote @18:03–18:07]); the 100-pt cutoff, the 15:00 start and the cumulative window are ours — X11 names 100–200 pts as a STRONG night, where the 2m stays, and the owner fixed no number (Q6) [derived: X11 @17:40–18:26]; evening_slow_pts / evening_slow_from_hour / evening_slow_window_min make all three tunable. The higher-TF lines and the box/ORB ESCAPE test stay a 1m body close; boxes are DRAWN on the traded frame (1m 08:30–09:00 CT, closed 2m otherwise, never 5m); trendlines and the EMA 9/34 follow the same 1m/2m clock. An ISB or PHL/PLH entered on the 2m trails its runner on CLOSED 2m candles, its candle-3 partial reads the 2m fill candle, and its break-even reads the 2m close — "trade khung nào thì gồng theo khung đó" [X8 @18:02–18:14] (only the 1:1 live rule keeps the 1m close); after a restart that frame is not known and the runner trails the 1m (WARN trail_tf_unknown_after_restart).',
          'mentor_exec_2m_after_30m = true (default ON) · X5 @00:07–01:17 · X11 @17:06–19:35 · "Routine trước khi trade" @00:41–01:14',
        ],
        [
          'ORB-edge ISB candle fraction outside',
          'An ISB at the ORB edge trades only when at least 80% (the knob) of the ISB candle — its high-minus-low range — lies OUTSIDE the opening range on the trade side: "80% 90% là cái nến nó nằm ở ngoài… anh chị có quyền take risk… điểm entry… nằm gần cái resistance quá thì thôi bỏ đi" [extra "Routine trước khi trade" part1 @07:46–09:04]. It is the fraction of the candle, NOT a points distance (that is the separate CE-7 orb_edge_veto_pts). A candle whose entry point just ticks outside the edge but whose body is still mostly inside the range is skipped and counted orb_isb_mostly_inside. The knob orb_isb_fraction_outside defaults to 0.80; 0 turns the rule OFF (the ISB trades at the edge at any fraction); the rule is part of the ORB gate (orb_gate_enabled), ISB entries only, RTH only.',
          'orb_isb_fraction_outside = 0.80 (default ON; 0 = off) · extra "Routine trước khi trade" part1 @07:46–09:04',
        ],
      ],
    },
    {
      kind: 'callout',
      title: 'Daily limits',
      items: [
        {
          title: 'Done for the day after a win',
          body: 'ON by default (the course default; m186 reverts the 2026-10-16 owner order). After a mentor trade closes in profit and the day is net positive, no new mentor entries until the next NY open (08:30 CT); resting entry orders are cancelled. An explicit false turns it off.',
          cite: 'Owner setting — done-after-win (D1.2 p1 @20:53 "thắng rồi… quyền được nghỉ ngơi"; p2 @06:09 "làm ơn đóng máy lại") · mentor_done_after_win — nil → ON (the course default)',
        },
        {
          title: 'Trading window — 08:30–09:30 CT by default',
          body: 'With the default window (start 08:30, 60 minutes) the day is only 08:30–09:30 CT. A window length of -1 ("No window (EOD rules still apply)") removes the time window only — it never means any hour: no NEW mentor entry is placed from 15:45 CT until the 17:00 CT reopen, and from the EOD flat (NY 14:45 CT; earlier on a registered half-day) until 17:00 every intraday mentor order is cancelled and an open intraday position is closed. Inside those limits mentor arms are authored around the clock — both bound strategies save -1 today (row 362 was written at 01:02 CT). A window, when set, only adds a further limit. The SWING4H is exempt from the window and from the EOD flat either way.',
          cite: 'Owner setting — window 08:30 + 60 (D1.2 p1 @24:04–24:15 "đặt một cái timer… đúng một tiếng") · mentor_window_start / mentor_window_minutes — live · trader/mentor_tick.go mentorWindowActive (minutes ≤ 0 = no window) · 15:45 cutoff: trader/mentor_mode.go mentorPastDailyHaltCutoff · EOD flat: trader/auto_trader_clock.go enforceEODFlatAt',
        },
        {
          title:
            'Daily guardrails apply to the mentor (OFF until you switch them on)',
          body: 'OFF unless you turn it on in Strategy Studio → Risk Control (the Guardrails master AND the Daily loss limit toggle, with its $ field; both OFF today, and an unsaved switch reads OFF). When ON, the daily loss limit covers the mentor exactly as every other path: the bot\u2019s own session-day P&L (realized + open, manual trades excluded) at or past −limit REFUSES every new mentor placement, the 60-second monitor beat CANCELS every resting mentor entry (SWING4H included — a dollar stop has no swing exemption) and FLATTENS the bot\u2019s open positions, and the gate stays LATCHED until 17:00 CT even if the P&L recovers. Max daily trades and the daily profit target, when switched on, now refuse mentor placements too (counted max_daily_trades_refused / daily_profit_refused) — before release #22 the mentor never evaluated them. An unresolved close (pnl_corrected NULL) FAILS CLOSED: the placement is refused (counted daily_loss_refused). Min confidence does NOT apply to mentor entries: they come from the course rules and carry no AI confidence.',
          cite: 'Engineering rule — mentorDailyLossGate → dailyGuardVerdictAt (one daily-guardrail verdict for every path) · trader/mentor_tick.go mentorDailyLossGate, trader/daily_loss_gate.go — live. The course’s own figure is deck slide 14: “Lỗ tối đa một ngày không được vượt quá 30% account” (max daily loss ≤ 30% of the account; never spoken in the videos). The gate is the OWNER’s switch — OFF by owner ruling R4; the owner sets the $',
        },
        {
          title: 'Never widen a stop, never add, never flip',
          body: 'A stop only ever moves toward break-even. Never add to a position, and never flip while one is open: ANY entry (same or opposite side) is refused while a mentor position is open — flat first (owner ruling 2026-09-03 "no adds, no flips"). Once flat, the next tick may place the other side (the box "Uno reverse" trades after the stop, never before). A fill also cancels every other working mentor entry on both sides (one working entry per side + OCO) before it can fill. The never-widen check judges a move against the stop of the position being moved (its own entry stop or its last move), never against a stop an earlier position on the same side left behind: before 2026-10-29 a long that had been moved to break-even at 29700 made the NEXT long\u2019s break-even at 29522 read as a widen and refuse for ~85 minutes (95 refusals since 10-23).',

          cite: 'D1.2 p2 @00:08–00:24 — "Không bao giờ được dời lệnh buy stop của mình xuống cây nến kế tiếp" (never widen); D1.1 p1 @17:00–17:44 (never add); owner ruling 2026-09-03 "no adds, no flips" — live',
        },
        {
          title: '"One loss → done" is NOT used',
          body: 'His personal one-loss rule stays his. The machine does not copy it.',
          cite: 'OWNER RULING 00:1x CT',
        },
        {
          title: 'Spent 300+ overnight AND 4h/1h disagree at 08:30 → day OFF',
          body: 'The day gate reads the Globex run at 08:30 CT. If the overnight daily candle already ran 300+ pts AND the 4h and 1h disagree, the machine is OFF for the day (latched at 08:30) — no box/ISB trades all day, the swing exempt. course-defaults-30 (item 4): the re-check is OFF (mentor_day_off_recheck, nil → OFF) — the day STAYS off, a whole-day latch. The pre-30 owner ruling (2026-10-09) that re-checked the OFF on every closed 1h bar is superseded.',
          cite: 'Ngày 5.1 p1 @19:22 — "Còn nếu như 2 khung giờ đang ngược nhau / Mà khung daily nó đã chạy được 300-400 điểm rồi / Tắt máy nghỉ luôn cho em" (shut down for the day; @19:40 "Ngày hôm đó không đáng để trade") — the course has NO re-check later in the day. course-defaults-30: the re-check is OFF (mentor_day_off_recheck, nil → OFF) — the day stays off. The pre-30 owner ruling (2026-10-09) that added the re-check is superseded.',
        },
      ],
    },
    {
      kind: 'h',
      text: 'Sizing',
    },
    {
      kind: 'table',
      title: 'The size table (SIM)',
      head: ['Tier', 'Default contracts', 'Knob'],
      rows: [
        ['Base', '5', 'mentor_base_contracts'],
        ['Confluence (many things agree)', '5', 'mentor_confluence_contracts'],
        ['Big (the hard tier)', '5', 'mentor_big_contracts'],
        ['Hard cap', '20', 'mentor_max_contracts'],
        [
          'Reduced (stop 20–25 pts; also an ISB at an old high/low or inside a range; the 5m pullback warning)',
          '3',

          'mentor_reduced_contracts',
        ],
        [
          'SWING4H',
          '2',
          'mentor_swing4h_contracts — lesson 5.2 p2 @06:08: “Nhưng đừng vô 1 MNQ / Vô Addict là 2 của MNQ để có đường bắn bớt” (not 1 MNQ — at least 2 MNQ to have room to take some off) — course-defaults-30',
        ],
        [
          'Spent day (§7)',
          'normal — the runner/target are cut, not the size (15-pt target cap + runner cap 2) [R09]',
          'mentor_spent_day_contracts',
        ],
      ],
    },
    {
      kind: 'p',
      text: 'The risk-control max_contracts_per_order knob does NOT apply in mentor mode: mentor entries size from this table, capped only by mentor_max_contracts (default 20).',
    },
    {
      kind: 'p',
      text: 'MENTOR-SIZE-5 (course D1.1 p2 L1103 “Cố định là 5 MNQ”; m186 reverts the 2026-10-16 MENTOR-SIZE-4 flatten): the base, confluence and big tiers all size a FIXED 5 MNQ — the mentor does not add contracts on confluence or a big move as a rule. The reduced tier (D4.1 p1 @04:37–05:04 rules 2+3 “giảm size”) is 3 — it actually reduces. The SWING4H (2) and the spent-day tier (normal size, 15-pt target cap + runner cap 2) are unchanged.',
    },
    {
      kind: 'h',
      text: 'The exits',
    },
    {
      kind: 'table',
      title: 'Three paths, chosen at the fill',
      head: ['Path', 'Rule'],
      rows: [
        [
          'A — resonance',
          'A PHL/PLH filled and then an ISB in the SAME direction within 3 candles AFTER the fill candle — counted in candles of the frame the entry was read on (1m, 2m or 5m; the fill candle itself is not counted — ours, the course does not say [D2.4 p1 @01:44–01:56, @04:44–04:53; X8 @18:02–18:14]) — where that ISB passes every placement gate EXCEPT the never-add gate: the position flips to mode A (do not trail it); leg 1’s 1:1 take-profit stays, and the RUNNER is NOT capped: its resting PHL/PLH take-profit is moved far out of reach (600 points beyond the entry — ours, the course only says “let it run”; the AddOn cannot delete a take-profit leg) so it exits only by its break-even stop, the X8 trap-dodge or the EOD flat [D2.4 p1 @01:58–02:09, @02:24–02:47]. With mentor_resonance_runner_tp_old_high ON the runner’s take-profit is instead moved to the level beyond the old high (the bot’s older inference); a one-bracket position is never touched. A position REBUILT from the broker after a restart has an unknown fill age, so its 3-candle window is treated as already spent and a later same-side ISB cannot flip it to A (release #53 m437). The stop does NOT move at the arm: on the CLOSED 1m candle, a split position’s RUNNER moves to break-even only AFTER leg 1’s 1:1 partial is confirmed filled at NinjaTrader (leg 1 keeps its original stop until its take-profit — “mình bán bớt rồi… thì cứ để break even cho nó chạy” [D2.4 p1 @03:36–03:45]); a one-bracket position has no partial, so it goes to break-even once the close is at least 0.25R beyond entry (a CTO default the owner may change)',
        ],
        [
          'B — normal',
          '2 or more contracts: ONE entry with a split exit — leg 1 (half, rounded up) takes profit at 1:1 measured from the actual entry price, and the runner holds toward the target. With a runner of 2 or more contracts the runner splits AGAIN (release #31): half of the runner (rounded down) takes profit at the first target line beyond leg 1’s 1:1 exit (EMA 34/9, old high/low, HTF trigger, box edge, trendline target) — the runner-partial line, strictly between leg 1’s take-profit and the trade target; with no line in that gap (or the line is at/past the target) the runner stays one leg — and the remainder holds to the target or until the EOD flat closes it, whichever comes first (5 contracts → 3/1/1, 3 → 2/1; 1 contract = a single leg; see EOD flat below). The runner’s stop follows mentor_trail_mode. candle (the default, owner ruling R3, release #22): NOTHING moves until leg 1’s 1:1 take-profit is CONFIRMED filled at NinjaTrader (the broker’s fill receipt, or its position size dropping to the runner — never a price touch); then EVERY runner contract’s stop goes to break-even = the entry (the runner-partial line contract and the trailing remainder alike — a contract whose own take-profit already filled is closed and never moved) (an off-grid averaged entry rounds toward profit), on the first close past the entry — if price is back through it, the original stop stays until a close is past it; then on every CLOSED candle of the frame the entry carries — the 1m, the 2m for an ISB or PHL/PLH read on the 2m under exec_2m_after_30m, the 5m on a slow evening, or the frame the box was drawn on for a box entry; an entry whose frame is unknown (an ORB/trendline entry) trails the 1m [X8 @18:02–18:14 "trade khung nào thì gồng theo khung đó"] — it moves to one tick beyond that candle’s low (long) / high (short), only when that is tighter — a lower low (long) / higher high (short) moves nothing. off = the older rule: both stops to break-even at HALF the distance to the target, live 1:1 behind the target, trail after the 1:1 point. Either way every move is checked against the stop NinjaTrader actually holds — never wider than it (a stop you tightened by hand is kept), nothing is sent while the broker book is stale, nothing is sent to a leg that is already closed — and each move is confirmed or reported from the next broker book. trail_tf: 1m default, off = break-even only; 30s/45s are REFUSED at load (the bot has no sub-minute bars — NinjaTrader streams 1m and up) and run as 1m, WARN + trail_tf_subminute_refused. Spent day: at most 2 contracts run — the rest leave with leg 1. The 2-pair split needs the AddOn build 2026-10-04-d1; the 3-pair (leg-3) split needs 2026-10-16-a — below that the runner stays one leg (the 2-pair plan). An AddOn older than 2026-10-04-d1 gets one bracket for the whole position, which keeps the older rule',
        ],
        [
          'C — confluence',
          'Leg 1 holds to at least 1:2 — the stop does not move up. A trade that BECOMES confluence after the entry (the trigger flips to its side) gets leg 1’s take-profit moved to 2R with a real NinjaTrader modify, tried up to 3 times and checked against the broker book [D3.4 p3 @10:07–10:33]',
        ],
        [
          'Wire 1:1 check',
          'A mentor stop entry goes out AT its planned price: the setup\u2019s own buffer (ISB 1.5, PHL/PLH 1.0, box and SWING4H none — at the candle\u2019s extreme) is the whole offset. Until release #22 every mentor stop entry went out 2 more ticks past it (the planner\u2019s stop-entry offset, which now applies to planner stop entries only). A target sitting at exactly 1R (the floor, e.g. the swing’s first target) moves with it, so the trade is still exactly 1:1 from the real entry; a LEVEL target cannot move — if the real entry would leave it under 1:1 the entry is refused (stop_entry:wire_rr_below_1) [D1.2 p1 @08:02–08:33]',
        ],
        [
          'SWING4H',
          'First target = at least 1R: the warmed 5m EMA 34 when it sits FURTHER than 1R on the profitable side, else exactly 1R (the old 50-pt fallback is gone — kernel/mentor/swing4h.go swingFirstTarget); BE at +1R (a 5m close at or through +1R earns it; the stop move goes through the same signal-scoped, broker-verified path as the intraday legs, and an unapplied BE stays owed and is re-sent on every closed 5m bar until the broker holds it); hold ONE full 4h candle after the entry candle (the hold ends at the close of the next 4h candle on the 17:00 CT grid — a swing entered late in its candle is NOT closed minutes later — but it is NEVER carried through the 16:00 CT halt or a weekend: the deadline is capped at the market close) [D5.2 p1 @06:03–06:20]. Release #54 (swing exit state): the hold-close is a marketable LIMIT through the market on this one position (the exit X8 uses — it cancels only this position’s bracket, not every resting arm on the account), with one market close if the position is still open on the next bar; it too stays owed until the position is confirmed closed. A swing whose stop or target already filled is cleared at the top of the next tick, and a BE or close is refused unless the account still holds a position carrying that arm’s own signal id — a finished swing can no longer move another trade’s stop or flatten the instrument hours later',
        ],
        [
          'ISB partial',
          'The ISB follows B, plus the course’s candle-3 rule (“phải BÁN BỚT. Bắt buộc” — you MUST sell part [D1.4 p1 @11:59–13:28]). R1: leg 1’s +1R take-profit ALWAYS rests at NinjaTrader from the fill, every setup, every knob value. When the fill candle closes in profit SHORT of +1R (leg 1 did NOT fill inside the candle), leg 1’s take-profit is MOVED to a marketable limit at that close through the existing modify_bracket path — long: close − 8 ticks (2.00 pts); short: close + 8 ticks — so NinjaTrader fills it immediately at the best price: a market exit in effect, never a resting limit, with no AddOn change (the 8-tick offset is a named constant, mentorISBMarketableTickOffset). The limit is CLAMPED to never book a loss — long ≥ entry + 1 tick, short ≤ entry − 1 tick [D1.2 p1 @13:31–13:33] (mentorClampMarketableTP). If it has not filled by the next 1m close it is re-modified to the new close ∓ 8 ticks, once per candle, never back to “none” — the leg keeps its stop throughout; a failed send is retried ONCE on the next candle, then left to the +1R TP (fail-safe). A close PAST +1R means the +1R TP already filled inside the candle (the 1:1 rule); a losing close moves nothing (the +1R TP keeps resting — fail-safe on any skip, restart or failed send). A one-bracket position is never partially sold (the modify cannot target a leg the wire does not carry). The runner then follows mentor_trail_mode (see B).',
        ],
        [
          'Line partials (ONE only; more are NOT built)',
          'The course names several lines BETWEEN entry and target as places to sell part — near the old high/low, EMA 34, EMA 9, the trendline [D3.3 p1 @07:25–07:47 “những cái chỗ này là những cái chỗ anh chị bán bớt”; targets @05:15–06:00] — and gives NO size per line. What is built is ONE extra partial and only in the B exit: with a runner of 2 or more contracts the runner sells half (rounded down) at the FIRST line strictly between leg 1’s 1:1 exit and the target (the runner-partial line; it needs the AddOn build 2026-10-16-a, below that the runner stays one leg). A second or third line partial is NOT built: it needs the mentor’s answer on what fraction to sell at each line (open question B12). Until then nothing closes more than the course says.',
        ],
        [
          'Path missing at the fill',
          'The path is chosen at entry and held in memory. If that is lost before the fill (a restart, or a second copy of the trader), the fill reads the path stored on the order at authoring; with none stored, SWING4H uses swing and everything else the course default B (normal: trail each closed candle) [D2.4 p1 @ 07:01]. There is no strategy setting for a default path. Never an empty path driven as B silently: the log line "🧑‍🏫 mentor exit mode for …" names the path and where it came from, once per order',
        ],
        [
          'Restart rebuild',
          'At boot the bot rebuilds every open mentor position from the broker’s own book (the -sl/-sl2 stops and -tp/-tp2 targets), so the runner resumes at the right stage instead of being lost: leg 1 gone (broker size down to the runner) and the runner stop short of the entry → break-even next; the runner stop already at/through the entry → the candle trail continues. If the book has no stop for a position the bot manages nothing and raises a P0 (never guesses a stop). The boot line reads "🧑‍🏫 mentor positions rebuilt: N (refused M)". Server switch MENTOR_RESTART_REBUILD=off disables the rebuild.',
        ],
        [
          'EOD flat',
          'No intraday exit path rides through the end of the day. At the EOD flat the bot first cancels the resting intraday orders, then reads its open positions and closes each one through the trader close path — at market by default (the server setting EOD_FLAT_LIMIT_TICKS above 0 tries a limit first, then market). An A, B or C trade or an ISB runner still open is closed there, target or not. Only the SWING4H position is exempt — held by the 4h (log "SWING4H position … EXEMPT — held by the 4h"). When: whenever no session window is open — from the NY close at 14:45 CT until the 17:00 CT reopen, so the first bot cycle at or after 14:45 CT closes it — or earlier inside a session when a registered half-day (or a session’s eod_flat_offset_min) pulls the flat in. It runs while mentor mode (or the day plan) is ON. A position the broker reports but the bot’s own table does not is never auto-closed: the bot logs "NOT FLAT" for the owner and counts flat_claim_refused_broker_disagrees. (trader/auto_trader_clock.go enforceEODFlatAt → flattenPosition) Authoring is refused inside the flat window too: no intraday mentor arm is written once the session has ended (or inside a forced-flat window such as the news lead), because the armed pass would cancel it within milliseconds; the refusal is counted as the gate "Mentor — flat window (EOD / forced flat)" (placement_refused_eod_flat). SWING4H is exempt.',
        ],
      ],
    },
    {
      kind: 'callout',
      title: 'X8 trap-dodge exit',
      items: [
        {
          title: 'A fake breakout through the EMA 34 exits the position',
          body: 'When a position is open, a CLOSED candle on the frame the position was ENTERED on — for an ISB/PHL the frame it was read on (1m 08:30–09:00 CT, 2m after, 5m on a slow evening), for a box entry the frame the box was DRAWN on (1m 08:30–09:00 CT, 2m otherwise), and for an ORB/trendline entry or a position whose frame is unknown (e.g. rebuilt after a restart) the 2m that closes through that frame’s own EMA 34 against the 5m trigger trend — ABOVE it while the 5m trigger is on the SELL side (a candle swept the prior low), BELOW it while the trigger is on the BUY side — is the fake breakout the course warns about ("thì out lại tại đó chứ đừng ngồi hô"). The bot exits the position with one marketable limit order through the trader close path (so other resting entry orders on the account are NOT cancelled; only this position’s own stop/target bracket is) — with no fresh price, or if the limit is still unfilled after 15 seconds, it sends a market close instead, which at NinjaTrader flattens the contract and cancels every working order for it; the SWING4H is exempt (held by the 4h). A close through the EMA 34 alone never fires — the 5m sweep is the trigger [X8 part1 @07:31–08:07].',
          cite: 'X8 "Cách né trap & Setup gồng lời" part1 @07:31–08:07 · kernel/mentor/filters.go X8TrapDodge · trader/mentor_exit_drive.go mentorX8TrapDodgeExit',
        },
      ],
    },
    {
      kind: 'callout',
      title: 'AI-era exits are off in mentor mode',
      items: [
        {
          title: 'Auto-breakeven · trailing · drawdown emergency close',
          body: 'While mentor mode is ON these AI-era mechanics do not apply to a mentor trade — the mentor’s exits own the stop (owner ruling 2026-10-09). The 60-second monitor’s drawdown emergency close (profit > 5% AND drawdown ≥ 40% from the peak) logs "not applied — mentor mode ON" instead of closing the position.',
          cite: 'Owner ruling 2026-10-09 · trader/auto_trader.go maybeMoveStopToBreakeven · trader/auto_trader_trailing.go maybeTrailStop · trader/auto_trader_risk.go checkPositionDrawdown',
        },
      ],
    },
    {
      kind: 'callout',
      title: 'Stalled-setup alarm',
      items: [
        {
          title: 'A setup that is seen but never trades raises a P1',
          body: 'Each mentor setup kind (lvl, isb, isb-reverse, phl/plh, box, swing) is counted per session day: how many entries the evaluator emitted, and how many reached the ledger. If a kind crosses 3 emitted with 0 reached the ledger while the market is open, a P1 alert fires once that day naming the kind, the window and the last refusal reason (e.g. "lvl: 7 emitted, 0 reached the ledger since 10-12 17:00 CT — last refusal: none recorded (silent drop)"). Refusals that are YOUR OWN RULES working do not count toward the 3: the trading window, the end-of-day flat window, done-for-the-day after a win, stop after a loss, the daily loss limit and the news hold. A trader with a 60-minute window refuses every out-of-window setup by design, and used to get a false "stalled" P1 for it (15 of the 27 stalled alerts in the log before release #54 were exactly that); when some of the emitted setups were refused by those rules the alert says how many. Alert-only — no trading behaviour changes. The per-kind counts are read from the gate-blocks endpoint (setup_stalls; policy_refused is the count of your-own-rules refusals). Mentor refusals also appear on the gate-blocks table by name: mentor_rule_r8, mentor_rule_r9, mentor_bad_geometry, mentor_untagged_setup, mentor_size_refused, mentor_resonance_hold and mentor_place_off (dry run). Box returns dropped inside the box rules are counted box_trigger_side, box_ping_pong, box_inside_box, box_stop_ceiling, box_no_target and box_room. The tally is in-memory: a restart (every release install) and the 17:00 CT session-day rollover both reset it.',
          cite: 'Engineering rule (release #22, owner order 2026-10-13) — not from the course · trader/mentor_stall_alarm.go · telemetry/setup_stall.go · api/handler_gate_blocks.go',
        },
      ],
    },
    {
      kind: 'callout',
      title: 'Not built — course items waiting for a mentor answer',
      items: [
        {
          title: 'Sub-minute frames (15s / 30s / 45s)',
          body: 'The course mentions the 30-second inside bar for the news entry, 15s/30s ISBs as the most frequent, and a 30s/45s trail. The bot has NO sub-minute bars (NinjaTrader streams 1m and up through our bridge), so every one of these runs on the 1m instead: the news ISB, the entry reference, the trail (30s/45s are refused at load and run as 1m). Building them would need 30-second bars from NinjaTrader ticks — a lockstep Go + AddOn change and the full AddOn deploy dance — or an owner ruling that the 1m stands in.',
          cite: 'D4.4 p1 @23:03–23:25 (30s inside bar for the news long) · not built — infrastructure decision, no code',
        },
        {
          title: 'More than one line partial',
          body: 'Selling part at EVERY line on the way (old high/low, EMA 34, EMA 9, trendline) is not built — only ONE runner-partial line exists, in the B exit only (see Exits). The course gives no fraction per line.',
          cite: 'D3.3 p1 @07:25–07:47 · open mentor question B12',
        },
        {
          title: '“Not respected, switch the EMA off”',
          body: 'The mentor switches the EMA 34 off when price slashes up and down through it (“xiên lên xiên xuống … không respect một chút nào”) and trades the ORB instead. There is no number for it (crossings in what window?), and “xiên” may mean the line itself slanting, so the bot only relocks the EMA after a LOSS in that zone (until_zone_exit); the first losing EMA 34 trade in a choppy zone is still taken. The numeric cross gate was removed in #45 (mentor_ema_max_cross_30m = 0).',
          cite: 'D4.2 p1 @22:12–22:35, @23:33–24:21 · open mentor question B14',
        },
        {
          title: 'Same-direction wick zone entry on the 5m',
          body: 'Two or three 5m candles with wicks all the same way make a small zone the mentor fades (“advanced, đừng có tập khúc này đầu tiên”). The bot has only a TARGET-only wick level (wick_microscalp, OFF, no setter) — no entry. Not built; recorded as the mentor’s advanced trade, not part of the standard setups.',
          cite: 'D4.3 p1 @00:00–00:25, @01:23–01:50, @06:30–06:44 · owner decision B15',
        },
        {
          title: 'Trend-day nine-line test',
          body: 'When price breaks all nine session lines (Asia / Europe / US high, low, middle) in one direction the market is confirmed trending and the mentor says do not scalp reversals (small size, or PHL/PLH stop orders only). No session high/low/middle lines exist in the bot and the way to build them is an open question, so nothing gates a counter-trend (reverse ISB) entry on a trend day. It is 2025 material; nothing in the 2026 course contradicts it.',
          cite: 'Major & Key Levels part2 @00:00:43–00:02:06 · Cách vẽ supply & demand trong ngày part1 @00:00:05–00:00:41 · open mentor question B16 (Q8)',
        },
        {
          title: 'VWAP ±1 standard deviation module',
          body: 'The 2025 VWAP + ±1 sd bands (three support/resistance lines, a target ladder, a size tier, a ranging detector, an ORB re-entry) are NOT built in mentor mode. It is unknown whether the mentor still teaches it (zero hits in the 2026 course), so nothing is built until he answers. VWAP exists only in the non-mentor planner.',
          cite: 'VWAP Stddiv part1 @00:01:44–00:20:32 · open mentor question B24 (Q6)',
        },
        {
          title: 'Smaller size when trading the 2m',
          body: 'The mentor says a 2m stop is larger than a 1m stop, so reduce size ("khung 2 phút … stop loss lớn hơn khung 1 phút … mình phải giảm size lại"). He gives no number, so there is NO frame-based size cut: the size table reads the stop (20–25 pts = reduced tier) and the setup flags only, never the frame. A 2m ISB with a small stop trades the normal size.',
          cite: 'D3.1 p1 @00:14:07–00:14:47 · open mentor question B26',
        },
      ],
    },
    {
      kind: 'h',
      text: 'Every knob, with its default and source',
    },
    {
      kind: 'knobs',
      knobs: withMentorKnobKeys([
        {
          label: 'Mentor mode',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Mentor mode (per strategy), top of the section',
          what: 'Turns the mentor method on for this strategy. SIM only. While ON, the AI-era exit mechanics — auto-breakeven and trailing — are not applied to mentor trades: the mentor\u2019s exits own the stop (owner ruling 2026-10-09).',
          trader:
            'A switch at the top of Risk control. Turning it ON asks you to confirm (it names the strategy and the order gate); turning it OFF is immediate. Press Save: the running trader reloads on save, so no restart is needed. A second, read-only line under it shows the order gate: "Orders: DRY RUN (MENTOR_PLACE off)" means every mentor entry is only sized and logged, nothing is sent; "Orders: SIM orders ON (MENTOR_PLACE=1)" means mentor entries are placed on the SIM account. The two are separate: the switch picks the method, MENTOR_PLACE (a server setting, not a button) allows orders. Start with the dry run. While Mentor mode is ON, a funnel line logs every 15 minutes (and on change): bars · entry intents · other actions (level invalidations, confluence upgrades, arm keeps/cancels) · kernel refusals · trader refusals · authored · placed · filled; "bars" includes any closed bars the evaluator caught up after the event path missed them (counter mentor_bars_caught_up, one WARN with the range) — the visibility for why entries were dropped (e.g. orb_not_drawn).',
          consumer:
            'trader/mentor_mode.go mentorEnabled · kernel/mentor/mentor.go Config.Enabled',
          range: 'true / false',
          systemDefault: 'OFF',
          recommended:
            'OFF — Owner setting: SIM only. Mentor entries are always stop-LIMIT. When you do turn it ON, leave MENTOR_PLACE off first and watch the dry-run lines.',
          whenToTouch:
            'SIM only. Start with the dry run (MENTOR_PLACE off), then enable orders when the funnel shows the entries you expect.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Base contracts',
          where: 'Strategy → Mentor mode → sizing',
          what: 'The default contract count for a mentor entry.',
          trader:
            '5 contracts on SIM per setup — the course fixed 5 (D1.1 p2 L1103).',
          consumer: 'trader/mentor_mode.go mentorBaseContractsDefault',
          range: 'int',
          systemDefault: '5',
          recommended:
            '5 — the course’s “fixed 5” (1.1 p2 L1103); m186 reverts the 2026-10-16 “set to 4” flatten.',
          whenToTouch: 'When you deliberately re-size the method.',
          perSession: 'No.',
        },
        {
          label: 'Confluence contracts',
          where: 'Strategy → Mentor mode → sizing',
          what: 'Size when many things agree (the confluence path).',
          trader:
            '5 contracts when everything lines up — the tier still reports WHY, but the count stays 5.',
          consumer: 'trader/mentor_mode.go mentorConfluenceContractsDefault',
          range: 'int',
          systemDefault: '5',
          recommended:
            '5 — the course 1.1 p2 L1103 “fixed 5 MNQ”; the confluence tier no longer adds contracts.',
          whenToTouch: 'Only if the owner re-sizes the method deliberately.',
          perSession: 'No.',
        },
        {
          label: 'Big contracts',
          where: 'Strategy → Mentor mode → sizing',
          what: 'The big tier — it still reports WHY the setup qualified (4h AND 1h both point the entry’s side, a silent 1h does not count, room ≥ 2× the risk, target ≥ 30 pts), but the count is 5.',
          trader:
            '5 contracts on a big tier — the tier no longer adds contracts.',
          consumer: 'trader/mentor_mode.go mentorBigContractsDefault',
          range: 'int',
          systemDefault: '5',
          recommended:
            '5 — the course 1.1 p2 L1103 “fixed 5 MNQ”; mentor_max_contracts (20) is a ceiling only.',
          whenToTouch: 'Never above the hard cap.',
          perSession: 'No.',
        },
        {
          label: 'Max contracts',
          where: 'Strategy → Mentor mode → sizing',
          what: 'The hard ceiling across all tiers. The risk-control max_contracts_per_order knob is NOT applied in mentor mode — this is the cap that governs.',
          trader: 'No mentor position ever exceeds this.',
          consumer: 'trader/mentor_mode.go mentorMaxContractsDefault',
          range: 'int',
          systemDefault: '20',
          recommended:
            '20 — Owner setting: CTO shipped size table (P3); a ceiling only, no tier sizes to it by default.',
          whenToTouch: 'To tighten the cap.',
          perSession: 'No.',
        },
        {
          label: 'Reduced contracts',
          where: 'Strategy → Mentor mode → sizing',
          what: 'Size when the stop is 20–25 pts, where the ceiling is the mentor_isb_max_pts knob (default 25 — trader/mentor_mode.go mentorContractsFor). A SWING4H is EXEMPT from this band (the swing tier ranks first, so a swing stop in (20,25] sizes swing4h, not the reduced tier). A spent day does NOT use this tier — it keeps the normal size and cuts the target and the runner. The reduced tier actually reduces: 3 (D4.1 p1 @04:37–05:04 “giảm size”).',
          trader: '3 contracts on the reduced tier.',
          consumer: 'trader/mentor_mode.go mentorReducedContractsDefault',
          range: 'int',
          systemDefault: '3',
          recommended:
            '3 — the course’s reduced tier (D4.1 p1 @04:37–05:04 rules 2+3 “giảm size”; m186 reverts the 2026-10-16 “no longer cuts below 4”). The band is a 20–25 pt stop (top = mentor_isb_max_pts, default 25 — W-MENTOR-EXACT). An ISB at an old high/low or in a range also sizes 3.',
          whenToTouch: 'With the 20–25 reduced band.',
          perSession: 'No.',
        },
        {
          label: 'SWING4H contracts',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25) → SWING contracts (release #25: the course’s swing_contracts knob)',
          what: 'Size for the 4h EMA 34 swing setup. With 2+ contracts AND the Swing runner switch ON, the swing splits into leg 1 at the first target + a runner.',
          trader:
            '2 contracts on the swing (lesson 5.2 p2 @06:08: “Vô Addict là 2 của MNQ để có đường bắn bớt”); a strong day can never raise it.',
          consumer: 'trader/mentor_mode.go mentorSwing4HContractsDefault',
          range: 'int',
          systemDefault: '2',
          recommended:
            '2 — D5.2 p2 @06:08 "Nhưng đừng vô 1 MNQ / Vô Addict là 2 của MNQ để có đường bắn bớt" (not 1 MNQ — at least 2 to take some off); course-defaults-30.',
          whenToTouch:
            'Owner sizing call (CX-2): 1 is his own cash-account size ("Em vô đúng 1 MNQ thôi" [D5.2 p1 @00:43]).',
          perSession: 'No.',
        },
        {
          label: 'Spent-day contracts',
          where: 'Strategy → Mentor mode → sizing',
          what: 'NOT READ FOR SIZING. The value is resolved and then discarded (trader/mentor_mode.go mentorContractsFor: `_ = spentCap`). A spent day (§7: the day already ran 300+) keeps the normal size; what it cuts is the target (15-pt cap) and the runner — at most 2 contracts run, a fixed code constant (mentorSpentDayRunnerCap), not this knob [R09].',
          trader:
            'Changing it changes nothing today — the course\'s "on range days hold 1–2 contracts only" is carried by the fixed runner cap of 2.',
          consumer:
            'none (resolved by mentorKnobs, then discarded) — runner cap: trader/mentor_mode.go mentorSpentDayRunnerCap = 2',
          range: 'int (no effect)',
          systemDefault: '2 (no effect)',
          recommended:
            'Leave it — it has no reader. Engineering rule (R09, owner ruling 2026-10-04): the spent-day behaviour is the 15-pt target cap + the runner cap 2.',
          whenToTouch: 'Never — it does nothing until a code change wires it.',
          perSession: 'No.',
        },
        {
          label: 'Trail timeframe',
          where: 'Strategy → Mentor mode → exits',
          what: 'The candle-trail timeframe for the B exit. A position entered on the 2m (exec_2m_after_30m) trails closed 2m candles instead of the 1m — the frame follows the entry [X8 @18:02–18:14] — unless this is off, which stops the trail for EVERY entry (break-even only). 30s and 45s are refused when the strategy loads (no sub-minute bars exist) and run as 1m, logged + counted once per load (trail_tf_subminute_refused).',
          trader: '1m — the stop trails behind each closed 1m candle.',
          consumer:
            'trader/mentor_mode.go mentorTrailTF / mentorTrailTFLoadCheck',
          range: '1m / off (30s / 45s refused → 1m)',
          systemDefault: '1m',
          recommended:
            '1m — the course frame [D2.4 p1 @08:35 move-stop; X8 @15:40].',
          whenToTouch: 'off = the video-8 legacy, kept as a knob.',
          perSession: 'No.',
        },
        {
          label: 'Runner exit (trail mode)',
          where: 'Strategy → Risk Control → 🧑‍🏫 Mentor mode → Runner exit',
          what: 'What moves the runner’s stop after a split mentor entry (course-defaults-30, owner "fix all"). candle: FIRST, on the first closed candle whose close is ≥ +0.5R, BOTH legs’ stops move to break-even BEFORE leg 1’s 1:1 partial fills (mentor_be_before_partial ON by default — this reverses owner ruling R3); WHILE leg 1’s take-profit is still unfilled, leg 1 keeps the live 1:1 stop on every closed candle after the one that armed break-even (stop = 2 × close − leg 1’s target, never wider, never on or through the market [D1.2 p1 @14:09–16:07 “Risk reward … lúc nào cũng phải maintain ở mức 1-1”]); THEN, once leg 1’s 1:1 take-profit is CONFIRMED filled at NinjaTrader, the runner trails one tick beyond each closed candle’s low (long) / high (short), only ever tighter. off: the older rule — break-even at half the distance to the trade target, live 1:1, trail after the 1:1 point.',
          trader:
            'candle — BE at +0.5R first, then after the 1:1 partial fills the rest is at break-even within one closed candle, then follows each closed candle [D2.4 p1 @16:17 “CỨ BÁN BỚT, xong rồi mình GỒNG lệnh”; @07:01 “move stop THEO TỪNG CÂY NẾN ĐÃ ĐÓNG XONG”; X8 @15:40 the candle’s low].',
          consumer:
            'trader/mentor_trail_r22.go mentorTrailMode · mentorExitDriveCandle (called from trader/mentor_exit_drive.go mentorExitDrivePos)',
          range:
            'candle / off · any other value fails closed to candle and is counted (trail_mode_bad_value)',
          systemDefault: 'candle',
          recommended:
            'candle — the course’s normal case [D2.4 p1 @16:17; X8 @15:40] with BE at +0.5R first (mentor_be_before_partial ON, owner "fix all"). On 10-12 row 313 leg 1 banked 1:1 and the runner, still at its original stop, lost the full 1R a minute later.',
          whenToTouch:
            'off only to compare against the pre-release-#22 rule; a one-bracket position (1 contract, or an older AddOn) always uses the older rule.',
          perSession: 'No.',
        },
        {
          label: 'Key level timeframe',
          where: 'Strategy → Mentor mode → levels',
          what: 'The TF of the colour-change walk that draws key levels.',
          trader: '1H — "90%-95% các levels quan trọng xuất phát từ khung 1H".',
          consumer: 'kernel/mentor/mentor.go KeyLevelTFMinutes',
          range: 'minutes',
          systemDefault: '60 (1H RTH, from the market open)',
          recommended: '60 — slide 31, the written spec.',
          whenToTouch: 'Other TFs exist for experimentation only.',
          perSession: 'No.',
        },
        {
          label: 'Key levels RTH only',
          where: 'Strategy → Mentor mode → levels',
          what: 'Only RTH candles count for key levels. INERT at the default key-level timeframe (60): the 1H series is already anchored at the 08:30 RTH open, so this switch only changes the walk for another key-level timeframe (experimentation).',
          trader:
            'ON — external hours are not counted ("Không tính external hours").',
          consumer: 'kernel/mentor/mentor.go KeyLevelRTHOnly',
          range: 'true / false',
          systemDefault: 'true',
          recommended: 'true — the slide says RTH only.',
          whenToTouch: 'Never — the slide is explicit.',
          perSession: 'No.',
        },
        {
          label: 'Key level prune',
          where: 'Strategy → Mentor mode → levels',
          what: 'Two levels closer than this: delete one, keep the more recent.',
          trader: '20 pts.',
          consumer: 'kernel/mentor/mentor.go KeyLevelPrunePts',
          range: 'pts',
          systemDefault: '20',
          recommended: '20 — D5.3 p1 @ 16:08–16:44.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'EMA 34 period',
          where: 'Strategy → Mentor mode → indicators',
          what: 'The EMA 34 period for the level lines.',
          trader: '34.',
          consumer: 'kernel/mentor/mentor.go EMAPeriod34',
          range: 'period',
          systemDefault: '34',
          recommended:
            '34 — Engineering rule: the method names it exactly, not from the course.',
          whenToTouch: 'Never — the method names it.',
          perSession: 'No.',
        },
        {
          label: 'EMA 9 period',
          where: 'Strategy → Mentor mode → indicators',
          what: 'The EMA 9 period (the reverse-ISB home).',
          trader: '9.',
          consumer: 'kernel/mentor/mentor.go EMAPeriod9',
          range: 'period',
          systemDefault: '9',
          recommended:
            '9 — Engineering rule: the method names it (the reverse-ISB home), not from the course.',
          whenToTouch: 'Never.',
          perSession: 'No.',
        },
        {
          label: 'EMA timeframe',
          where: 'Strategy → Mentor mode → indicators',
          what: 'The TF the EMA 34/9 lines are computed on. While "2m execution" is ON and this stays at 1, the lines follow the execution frame (1m 08:30–09:00 CT, 2m otherwise); any value above 1 is an explicit override.',
          trader:
            '1m, or the execution frame (2m after 09:00 CT) with 2m execution ON.',
          consumer: 'kernel/mentor/mentor.go EMATFMinutes',
          range: 'minutes',
          systemDefault: '1',
          recommended:
            '1 — the lines follow the execution frame ("anh TRADE KHUNG NÀO thì anh BẬT KHUNG ĐÓ" [D5.4 @08:15]; "sau 30 phút em sẽ chuyển qua khung 2 phút" [Routine trước khi trade part1 @00:00:34–00:01:14]); with 2m execution OFF it is the 1m all day.',
          whenToTouch: 'Experimentation only.',
          perSession: 'No.',
        },
        {
          label: 'EMA 34 location timeframe',
          where: 'Strategy → Mentor mode → locations',
          what: 'The EMA 34 timeframe for the LOCATION gate (R3: intraday = the trading chart).',
          trader:
            'The EMA 34 of the frame you trade — 1m 08:30–09:00 CT, 2m otherwise with 2m execution ON (1m all day when OFF); the 4h EMA 34 is ONLY the swing.',
          consumer: 'kernel/mentor/mentor.go EMALocationTFMinutes',
          range: 'minutes',
          systemDefault: '1',
          recommended:
            '1 — D5.4 p1 @ 05:16–05:25 "Đụng EMA chính đi rồi vô lệnh" (the intraday chart).',
          whenToTouch: 'Experimentation only.',
          perSession: 'No.',
        },
        {
          label: 'Touch band',
          where: 'Strategy → Mentor mode → touches',
          what: 'How close counts as touching a level.',
          trader:
            '0 — a literal touch. On the 1m a near-touch is allowed ("chưa gần đụng… gần gần gần đụng"), but the 4h swing must literally touch ("bắt buộc là phải đụng nha").',
          consumer: 'kernel/mentor/mentor.go TouchBandPts',
          range: 'pts',
          systemDefault: '0',
          recommended:
            '0 — the literal touch [D3.3 p1 @00:13; D5.2 p2 @18:36 "chưa gần đụng… bắt buộc là phải đụng nha" — near-touch ok on the 1m, the 4h swing must touch].',
          whenToTouch: 'Never — the literal touch is the method.',
          perSession: 'No.',
        },
        {
          label: 'ISB buffer',
          where: 'Strategy → Mentor mode → ISB',
          what: 'Buffer on BOTH the entry and the stop, outward. It is the WHOLE entry offset on the wire: 1.5 = the order goes out 6 ticks past the extreme (until release #22 the executor added 2 more ticks, so it went out at 2.0).',
          trader: '1.5 pts.',
          consumer:
            'kernel/mentor/mentor.go ISBBufferPts · trader/armed_executor.go stopEntryWireOffset (0 for mentor orders)',
          range: 'pts',
          systemDefault: '1.5',
          recommended: '1.5 — D1.4 p1 @ 22:22–22:30.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Reverse ISB at EMA 9',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'R7: an ISB pointing AGAINST the trend at EMA 9 reverses — uptrend → long buy stop above the ISB high + 1.5-pt buffer; downtrend → the mirror (sell stop below the ISB low − 1.5-pt buffer). The entry buffer is mandatory on the 1m [D1.4 p1 @ 22:26–23:26]. Requires price to actually reach EMA 9, read on the frame the pair is read on (C52-W4): the 2m EMA 9 against a 2m pair, the 1m EMA 9 against a 1m pair — "anh trade khung nào thì anh bật khung đó, khung M1 cũng được, khung M2 cũng được" [Ngày 5.4 ISB đảo chiều p1 @08:15–08:40]. The touch is exact for now (the EMA 9 must lie inside the candle’s range; the course’s loose “vòng vòng EMA 9” band waits for the mentor’s width), and the slow-evening 5m pair keeps the 1m EMA 9 until the mentor says which frame the evening lines are drawn on. It carries a target (the next level beyond, with the 1:1 floor and the spent-day cap) and runs through the normal ISB gates — the 30-pt stop ceiling, 4h side, near-box room — before it can place; with it ON, the normal ISB does not arm the opposite side on the same candle pair.',
          trader: 'ON — R-C owner ruling 2026-10-04.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (ISBReverseEMA9Enabled) · kernel/mentor/eval.go (reverse ISB emit)',
          range: 'true / false',
          systemDefault: 'true',
          recommended: 'ON — [D5.4], R-C ruling.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label:
            '4h/1h gate only in the news + open windows (print day 07:20–08:30, open 08:30–09:30 CT)',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D4.4-11: ON (default) = news + open: the gate applies on a red-folder PRINT day from 07:20 CT through the 08:30 CT open (W7: the post-print news trade follows the 4h; 4h/1h opposite sits out — D4.4 p1 @13:18–13:33, @18:03–18:52) AND in the 08:30–09:30 CT open window ("the open is like news" D4.4 p2 @00:35–00:46). The 60-minute open window is OURS: the course gives no length (borrowed from the mentor’s 60-minute entry window [example: D1.2 p1 @23:52–24:59]; owner-accepted INVENTED; mentor question B28 open). On a day with no print the 07:20–07:35 window is ordinary trading and is not gated; ordinary intraday entries outside those windows are not HTF-gated. OFF = the all-day gate (no 4h trigger means nothing to follow, and a 1h against the 4h sits out all day). The course: "Đánh thường đánh níu cũng được / Nhưng mà đánh níu nên sử dụng cho níu trước đi / Đừng sử dụng cho thường / Thường bắt đầu khó lắm" (D4.4 p1 @13:55–14:06 — trading normal or news is fine, but use the HTF read for news first) and "em chỉ hướng dẫn mọi người cái news thôi" (D4.4 p1 @24:48–24:51). #45 reverts owner decision A 2026-10-21 (m134): the default is ON (news-only, the mentor’s news-first rule). The m134 all-day default is reverted.',
          trader:
            'ON — news + open is the default (the mentor’s rule; #45 reverts owner decision A 2026-10-21 m134).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (HTFGateNewsOnly) · kernel/mentor/htf_direction.go HTFGateActive / HTFVerdict',
          range: 'true / false',
          systemDefault: 'true',
          recommended:
            'ON — news + open is the default (the mentor’s rule; #45 reverts owner decision A 2026-10-21 m134: D4.4 p1 @13:55 "đánh níu nên sử dụng cho níu trước đi", @24:48 "em chỉ hướng dẫn mọi người cái news thôi"; the open is like news D4.4 p2 @00:35). #43 ships ALL-DAY (decision A); #45 narrows it to news + open per "exactly like mentor", which RE-ALLOWS counter-trend entries outside those windows — the owner must see that.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label:
            'Execution frame: 2m after 09:00 CT (1m 08:30–09:00) for ISB + PHL/PLH, EMA 9/34, boxes, trendlines and the X8 exit',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'X5-10: ON (default) = the named setups (ISB + PHL/PLH) are read on the 1m ONLY during the RTH open +30 minutes (08:30–09:00 CT — "a 2m stop cannot cover the setup at open volume"), and on the 2m at every other minute — overnight and the Europe open (3am Texas) included ("khung 2 phút ÍT BỊ QUÉT RÂU" [X5 @00:07–01:17; X11 @17:06–19:35]). A SLOW evening/overnight (15:00–03:00 CT, the 1m range since the RTH close under 100 pts) reads the 5m instead ("Trade khung 5 phút only … cho buổi tối" [X11 @17:40–18:32, quote @18:03–18:07]); the 100-pt cutoff, the 15:00 start and the cumulative window are ours — X11 names 100–200 pts as a STRONG night, where the 2m stays, and the owner fixed no number (Q6) [derived: X11 @17:40–18:26]; evening_slow_pts / evening_slow_from_hour / evening_slow_window_min make all three tunable. The ISB pair and PHL/PLH swap; the higher-TF lines and the box/ORB ESCAPE test stay a 1m body close; boxes are DRAWN on the traded frame (1m 08:30–09:00 CT, closed 2m otherwise, never 5m); trendlines and the EMA 9/34 follow the same 1m/2m clock. An ISB or PHL/PLH entered on the 2m trails its runner on CLOSED 2m candles and its candle-3 partial reads the 2m fill candle (the frame follows the entry frame [X8 @18:02–18:14]).',
          trader:
            'ON — the course reads the 1m for the first 30 min after the open, then the 2m ("sau 30 phút em sẽ chuyển qua khung 2 phút", "khung 2 phút ít bị quét râu"). The EMA 9/34 levels and the EMA 34 location line move to the 2m on the same schedule (M401), and the chart’s EMA curve carries the frame it was drawn on (tf_min).',
          consumer:
            'kernel/mentor/eval.go Tick (Exec2mAfter30m → closedBucketsTF(bars, 2, now)) · trader/mentor_tuning.go mentorTuningResolve',
          range: 'true / false',
          systemDefault: 'true',
          recommended:
            'ON — release #31 default ("Routine trước khi trade" part1 @00:41–01:14; Go over @17:27). OFF restores the 1m read all day.',
          whenToTouch: 'Only on an owner ruling.',
          perSession: 'No.',
        },
        {
          label: '15m inside-bar box needs a 5m ISB',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D4.2 p1 @17:09–17:13 "Nếu như khung 5 phút xuất hiện Insaba cho lệnh short thì em trade. Còn không em không trade" (if the 5m shows an inside bar for the short I trade; otherwise I don’t) · @14:24–14:34 "Nếu như khung 5 xuất hiện INSA 3 cùng chiều với khung 15 thì tôi kiếm lệnh — tôi dùng 1 phút tôi kiếm lệnh tôi trade" (a 5m inside bar in the 15m direction — then I look for the entry on the 1m) · option 1 @01:06–01:14 "Tôi không trade luôn… Đợi nó thoát khỏi range rồi mình trade" (I do not trade at all — wait for it to leave the range). While a 15m inside-bar box stands: require_5m_isb (default) = a 1m ISB trades only when a 5m ISB in the 15m box’s direction stands (or formed after the 15m candle and escaped the same way); no_trade_until_escape = no ISB at all until the 15m box escapes; off = the pre-#25 behaviour (a 1m ISB in the 15m direction passes without any 5m ISB). Refusals are counted isb_15m_needs_5m_isb / isb_15m_until_escape (isbrev_… for the reverse ISB). Default require_5m_isb — knob rule case (a), ON: both course options forbid what the bot did, and he names it as a month of his own losses (@15:41–16:46).',
          trader:
            'require_5m_isb — case (a). no_trade_until_escape is his stricter option 1; off restores the pre-#25 pass.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/eval.go Tick ISB path (fifteenMinISBGate)',
          range: 'off / require_5m_isb / no_trade_until_escape',
          systemDefault: 'require_5m_isb',
          recommended:
            'require_5m_isb — D4.2 p1 @17:09–17:13, @14:24–14:34; knob rule (a): course, no ruling against.',
          whenToTouch:
            'Only on an owner ruling (no_trade_until_escape = his option 1, @01:06–01:14).',
          perSession: 'No.',
        },
        {
          label: '5m/15m box conflict end',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D4.2 p1 @07:54–08:02 "Khi anh chị thấy nó messed up như vậy — Đừng trade. Đợi nó thoát cả luôn" (when you see it messed up like that — do not trade; wait until it escapes entirely). While a 5m box and a 15m box conflict, nothing trades. first_box = the conflict ends when the small 5m box escapes; both_boxes (default, course-defaults-30) = no trade until BOTH the 5m and the 15m boxes have escaped (refusals counted isb_tf_conflict_both_boxes). The course says "đợi nó thoát cả luôn" (wait for it to escape ENTIRELY).',
          trader: 'both_boxes — course-defaults-30.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/eval.go Tick (5m/15m conflict)',
          range: 'first_box / both_boxes',
          systemDefault: 'both_boxes',
          recommended:
            'both_boxes — D4.2 p1 @07:54–08:02 "đợi nó thoát cả luôn".',
          whenToTouch: 'Only on an owner ruling.',
          perSession: 'No.',
        },
        {
          label: 'News day: wait beyond the 4h line',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D4.4 p1 @21:55–22:12 "Đợi canh nó nằm trên hai cái đường line này, kiếm insert bar đu vô… Hoặc anh chị chỉ cần đợi nó nằm ở trên cái đường line 4 giờ" (wait for price to sit above both lines and take an inside bar — or at least above the 4h line). ON (default, course-defaults-30) = on a red-folder print day, from the print window until the 08:30 CT RTH open, an intraday entry needs the last close beyond the 4h trigger line on its own side (above it for a long, below it for a short); refusals counted news_wait_4h_line. OFF = no such wait (the print window itself still refuses). The 30-second ISB of the same lesson (@23:08–23:13 "Đánh inside bar lệnh long 30 giây cho em") is NOT built: NinjaTrader streams 1m bars and up, there are no sub-minute bars, and the bot does not fake them.',
          trader: 'ON — course-defaults-30.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/news_print.go newsWaitFilter',
          range: 'true / false',
          systemDefault: 'true',
          recommended: 'ON — D4.4 p1 @21:55–22:12.',
          whenToTouch: 'Only on an owner ruling.',
          perSession: 'No.',
        },
        {
          label: 'News day: freeze the pre-print 1h/4h read',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D4.4 p1 @18:13–18:25 "Chứ tính cái lúc trước news, chứ không phải tính cái lúc news ra rồi nha… Còn cái khúc mà news ra rồi thì không tính" (read the trigger from before the news, not from when it prints; what prints after the news does not count). ON (default) = the 1h/4h trigger read FREEZES at its pre-print values from the print window start until the 08:30 CT day-gate read — the 1h/4h candles are never rewritten (no bar is deleted), so after 08:30 the lines resume on whole candles. OFF = the item-18 print-window drop (07:20–07:35 only), byte-identical. Knob rule case (a): the course line, no ruling against.',
          trader:
            'ON — case (a): the course reads the trigger from before the print and never rewrites candles after the open.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/news_print.go newsHTFFrozen / newsBarFrozen',
          range: 'true / false',
          systemDefault: 'true',
          recommended:
            'ON — D4.4 p1 @18:13–18:25; the freeze is the faithful reading (the old behaviour deleted the 07:20–08:30 bars and corrupted every later 1h/4h candle all day).',
          whenToTouch: 'Only on an owner ruling.',
          perSession: 'No.',
        },
        {
          label: 'ISB rest box escape test',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D4.1 p1 @20:52–21:02 "cái thân nến nó chỉ nằm ở cái thân nến nằm bên ngoài mà cái râu nến nằm bên trong cái box ok bình thường không sao hết. Cái đó là at least" (the body outside with the wick still inside the box is fine — that is the minimum). body (default) = the 5m/15m/30m inside-bar rest box is escaped (and deleted) only when a 1m candle’s OPEN and CLOSE are both beyond the edge — the same test as the FTGH/FTGL boxes and the ORB escape; close = the older close-only test (pre-#25). Knob rule case (a), ON: the course line, no ruling against.',
          trader: 'body — case (a).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/isb_box.go ISBBoxEscape',
          range: 'close / body',
          systemDefault: 'body',
          recommended:
            'body — D4.1 p1 @20:52–21:02; knob rule (a): course, no ruling against.',
          whenToTouch: 'Rarely — close only to compare with the pre-#25 test.',
          perSession: 'No.',
        },
        {
          label: 'EMA 34 back after a loss',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D4.2 p2 @01:33–01:39 "Tắt EMA 34 đi. Đợi xíu nữa nó thoát khỏi cái vùng đó đi. Rồi tôi bật EMA 34 lên lại tôi ngồi tôi trade" (turn EMA 34 off, wait for price to leave that zone, then turn it back on and trade). until_zone_exit (default) = after one loss at the EMA 34, the EMA 34 location comes back when a CLOSED 1m candle lies completely outside the losing trade’s entry-to-stop zone (wicks included — the same departure test as the G2 loss box); session = off until the 17:00 CT roll (the pre-#25 behaviour). mentor_loss_departure_pts still applies as the numeric fallback. Knob rule case (a), ON.',
          trader: 'until_zone_exit — case (a).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/ema_rules.go emaLossTick',
          range: 'session / until_zone_exit',
          systemDefault: 'until_zone_exit',
          recommended:
            'until_zone_exit — D4.2 p2 @01:33–01:39; knob rule (a): course, no ruling against.',
          whenToTouch:
            'Rarely — session only to compare with the pre-#25 rule.',
          perSession: 'No.',
        },
        {
          label: 'ORB edge veto',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'X5 @08:52–09:09 "cái điểm entry của mình… mà nằm gần cái resistance quá thì thôi bỏ đi" (if my entry sits too close to the resistance — skip it); @08:20–08:28 the ORB upper edge is the resistance. 0 (default) = off. N > 0 = inside RTH, after the ORB escape, an entry whose price is within N points of the ORB edge is refused (counted orb_edge_too_near). Knob rule case (b), OFF: the course gives no number, so the veto stays off until the owner sets one.',
          trader: '0 = off — case (b): no number in the course.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/orb.go ORBVerdict',
          range: 'pts (0 = off; 0.25–50 accepted)',
          systemDefault: '0 (off)',
          recommended:
            '0 — X5 @08:52–09:09 names no distance; knob rule (b): off until an owner ruling sets N.',
          whenToTouch: 'Only when the owner picks a distance.',
          perSession: 'No.',
        },
        {
          label: 'ORB-edge ISB candle fraction outside',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Rule 5 (release #31): "80% 90% là cái nến nó nằm ở ngoài… anh chị có quyền take risk… điểm entry… nằm gần cái resistance quá thì thôi bỏ đi" (80% 90% of the candle sits outside… only then may you take the risk; if the entry sits too close to the resistance, skip it) [extra "Routine trước khi trade" part1 @07:46–09:04]. An ISB at the ORB edge trades only when at least this fraction of the ISB candle (its high-minus-low range) lies OUTSIDE the opening range on the trade side. It is the fraction of the candle, NOT a points distance (that is the separate CE-7 orb_edge_veto_pts above). A mostly-inside candle is skipped and counted orb_isb_mostly_inside. 0.80 (default) = ON; the rule is part of the ORB gate (orb_gate_enabled), ISB entries only, RTH only.',
          trader:
            '0.80 — the course names 80%–90%; the fraction measure, not a distance.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/orb.go orbISBMostlyInside',
          range: '0.01–1 (fraction of the candle)',
          systemDefault: '0.80',
          recommended:
            '0.80 — "Routine trước khi trade" part1 @07:46–09:04; the fraction the course gives.',
          whenToTouch: 'Only on an owner ruling.',
          perSession: 'No.',
        },
        {
          label: 'ORB escape: completely outside',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #52 B19. The ORB source: "Completely nằm ở ngoài, completely nằm ở ngoài mình mới trade. Còn không nếu mà nó vẫn còn vòng vòng trong ORB 2 phút mình không trade" (trade only when the candle is COMPLETELY outside; while it still hovers in the 2-minute ORB, no trade) [extra "Routine trước khi trade" part1 @06:47–07:16]. OFF (default) = the ORB opens on a 1m candle whose BODY is wholly outside (open AND close beyond the edge, wicks may stay inside — the minimum the course gives for the FTGH/FTGL box, D4.1 p1 @20:36). ON = the ORB opens only when the whole candle, wicks included, is outside (low above the ORB high for a long, high below the ORB low for a short). The wick reading is the X7 ping-pong definition ("a touching wick does not count" [X7 @09:21–11:07]) carried to the ORB — an analogy, not a sentence about the ORB — so the default stays OFF until the owner rules. Changes only WHEN the ORB opens (first hour); the side rule, the edge veto and the ISB fraction rule are untouched.',
          trader: 'OFF — the body test; the wick reading is our analogy [B].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (OrbEscapeCompletely) · kernel/mentor/orb.go orbEscapeSide',
          range: 'true / false',
          systemDefault: 'false (off)',
          recommended:
            'OFF until the owner rules — "Routine trước khi trade" part1 @06:47–07:16 says "completely" but X7 @09:21–11:07 defines it only for the ping-pong box.',
          whenToTouch: 'Only on an owner ruling.',
          perSession: 'No.',
        },
        {
          label: 'Near-box skip also gates PHL/PLH',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #52 B11. The course shows the "sát box" skip on an ISB only [Ngày 3.2 FTGH FTGL part1 @00:22:44–00:23:10]; applying it to a PHL/PLH (risk = the reference candle’s range) is OUR extension. ON (default) keeps today’s behaviour; OFF scopes the near-box refusal back to the ISB and the reverse ISB. The distance itself is the "Near-box room" card.',
          trader: 'ON — ours, an extension of the lesson.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (NearBoxRoomPHL) · kernel/mentor/eval.go (PHL/PLH near_box)',
          range: 'true / false',
          systemDefault: 'true (on)',
          recommended:
            'ON — ours, not from the course: the mentor says whether the skip applies to PHL/PLH, the lesson shows an ISB.',
          whenToTouch: 'Only when the mentor rules on it.',
          perSession: 'No.',
        },
        {
          label: '2m ISB stacking: cancel candle',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #52 B8. The stacking arithmetic is stated in 1M candles — 2 × 1m = a 2m, 3 × 1m = a 3m, a 4th would be a 4m that does not exist so it jumps to the 5m [Ngày 4.2 Multitime frame 15_ part2 @00:08:53–00:09:03, @00:15:24–00:16:08] — and the course never counts 2m candles. m406 reuses the 1m count on the arm’s own frame: a 2m ISB arm is kept through 3 stacked 2m candles and cancelled on the 4th. This is that candle number for the 2m (3–12). The 1m and 5m arms keep the course 4. Blank = 4.',
          trader:
            '4 — the 1m course count reused on the 2m; ours, not the course.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (ISBStackCancelCandle2m) · kernel/mentor/isb.go ISBStackTick',
          range: '3–12 candles',
          systemDefault: '4',
          recommended:
            '4 — ours, not from the course: the mentor says how many 2m candles stack before the cancel.',
          whenToTouch: 'Only when the mentor gives the number.',
          perSession: 'No.',
        },
        {
          label: 'ISB tiny-stop exception',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #52 B8. With a very small stop the mentor kept a resting ISB past the 4th candle: "stop loss của mình ở thời điểm đó rất nhỏ. Có 12 điểm… Trade nó là take risk" [Ngày 5.1 Go over lại tất cả part2 @02:50–03:40, @04:10–04:58]. He frames it as personal discretion and gives no threshold, so it is OFF. Set a number and an arm whose ISB stop (candle range + 2 × the 1.5-pt buffer) is at or under it is NOT cancelled at the cancel candle; it still cancels the moment a body leaves I1. Blank / 0 = off.',
          trader: 'OFF — the number is the mentor’s to give (he said 11–12).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (ISBStackTinyStopPts) · kernel/mentor/isb.go ISBStackTick',
          range: '0.01–25 pts (blank = off)',
          systemDefault: '0 (off)',
          recommended:
            'OFF — not from the course: Ngày 5.1 part2 @02:50–03:40 gives no threshold; a number here is a guess.',
          whenToTouch: 'Only on an owner ruling.',
          perSession: 'No.',
        },
        {
          label: 'Evening slow: cutoff',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #52 B10. A SLOW night reads the 5m: "buổi tối là trade khung 5 phút only… nếu mắc kẹt nó chạy 100–200 điểm thì em không nói… nhưng nếu nó chạy cần mấy chục điểm thì nên trade khung 5 phút" [extra Go over lớp học part1 @17:40–18:26]. The night is slow when its high–low range is under this many points. The 100 is OURS: the owner’s answer (Q6, 2026-10-25) is that "slow" means the candles are not running much and NO number is fixed. The start hour and the trailing window are the next two cards. Blank = 100.',
          trader: '100 pts — ours, not the course.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (EveningSlowPts) · kernel/mentor/eval.go eveningSlow',
          range: '1–1000 pts',
          systemDefault: '100',
          recommended:
            '100 — ours, not from the course: X11 @17:40–18:26 says only "mấy chục điểm" vs "100–200 điểm".',
          whenToTouch: 'Only when the mentor gives the number.',
          perSession: 'No.',
        },
        {
          label: 'Evening slow: start hour',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #52 B10. The CT hour the evening-slow range starts at: 15 = the RTH close (default — a volatile 15:00–16:00 US afternoon alone can then keep the whole night on the 2m), 17 = the Globex reopen ("buổi tối"). Overnight 00:00–02:59 CT reads yesterday’s start hour. Ours, not the course.',
          trader: '15 — ours.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (EveningSlowFromHour) · kernel/mentor/eval.go eveningSlow',
          range: '15–17',
          systemDefault: '15',
          recommended:
            '15 (unchanged) — ours, not from the course: X11 @17:40–18:26 says "buổi tối" without an hour.',
          whenToTouch: 'Only when the mentor gives the window.',
          perSession: 'No.',
        },
        {
          label: 'Evening slow: trailing window',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #52 B10. 0 / blank (default) = the evening range is cumulative since the start hour, so the night can leave "slow" once and never come back. A number N reads only the last N minutes of bars, so an early burst cannot disable the 5m schedule for the whole night. Ours, not the course.',
          trader: '0 (cumulative) — ours.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (EveningSlowWindowMin) · kernel/mentor/eval.go eveningSlow',
          range: '1–600 minutes (blank = cumulative)',
          systemDefault: '0',
          recommended:
            '0 (unchanged) — ours, not from the course: X11 @17:40–18:26 names no window.',
          whenToTouch: 'Only when the mentor gives the window.',
          perSession: 'No.',
        },
        {
          label: 'ISB size cut: old high/low band',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #52 B27. The size cut for an ISB "ngay đỉnh hoặc đáy cũ" [Ngày 4.1 Go over ngày 3 part1 @00:04:31–00:05:19] gives no distance. "At" = the ISB candle’s range reaches within ± this many points of an old high/low. The ±2 is OURS, not the course. Blank = 2; an explicit 0 = an exact touch.',
          trader: '2 pts — ours, not the course.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (ISBOldExtremePts) · kernel/mentor/eval.go touchesOldExtreme',
          range: '0–20 pts',
          systemDefault: '2',
          recommended:
            '2 — ours, not from the course: Ngày 4.1 part1 @00:04:31–00:05:19 gives no distance.',
          whenToTouch: 'Only when the mentor gives the number.',
          perSession: 'No.',
        },
        {
          label: 'ISB size cut: in-range gap',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #52 B27. The size cut for an ISB "in range" [Ngày 4.1 Go over ngày 3 part1 @00:04:31–00:05:19] has no definition in the course. One of the readings the bot uses is price between two KEY levels closer than this many points; the 50 was borrowed from the ping-pong gate (X4/D4.2) and is OURS. Blank = follow the ping-pong minimum (so an owner who tuned it keeps the same reading).',
          trader: 'The ping-pong minimum (50 pts) — ours, borrowed.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (ISBInRangeGapPts) · kernel/mentor/eval.go inNarrowRange',
          range: '0.01–500 pts (blank = the ping-pong minimum)',
          systemDefault: '0 (inherit the ping-pong minimum, 50)',
          recommended:
            'Blank — ours, not from the course: Ngày 4.1 part1 @00:04:31–00:05:19 defines no range in points.',
          whenToTouch: 'Only when the mentor gives the definition.',
          perSession: 'No.',
        },
        {
          label: 'Box-return 5m trend source',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D4.3 @12:43–12:54 "Nhìn từ bên trái qua bên phải… Đỉnh cao, đỉnh thấp hơn, đỉnh thấp hơn — thì nó đang là xu hướng gì — Giảm" (read left to right: a high, a lower high, a lower high — what trend is it? Down). The D4.3 "no box return against the 5m trend" gate (refusal box_against_5m_trend) reads its trend from: structure (default, course-defaults-30) = 5m swing structure (higher highs + higher lows = up; lower highs + lower lows = down; mixed = no trend, no veto); trigger_line = the 5m trigger line.',
          trader: 'structure — course-defaults-30.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/eval.go box-return trend gate (box_against_5m_trend)',
          range: 'trigger_line / structure',
          systemDefault: 'structure',
          recommended:
            'structure — D4.3 @12:43–12:54, @14:52–15:01 (read the 5m swing structure).',
          whenToTouch: 'Rarely — trigger_line only to restore the pre-30 rule.',
          perSession: 'No.',
        },
        {
          label: 'Level entries need the 5m trigger side',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D3.4 p1 @17:48–18:00 "Bất kỳ trường hợp nào đi nữa, nếu như anh chị đã kẻ ra được cái đường buy trigger rồi thì nếu như nằm ở dưới đường buy trigger không được đánh" (in any case, once you have drawn the buy trigger line, below it you may not trade) — against D3.4 p3 @09:57–10:27 school 1 (enter at the FTGL first, wait for the trigger after) and @12:38 "anh cứ chọn 1 trong 2 thôi" (just pick one of the two); the owner chose school 1. ON = under school 1 the 5m trigger side ALSO binds every level reject (key level, EMA 34, trendline, old extreme) — school 1 stays only at the FTGL/FTGH boxes. OFF (default) = school 1 for every location. Knob rule case (b), OFF: owner ruling (school 1).',
          trader: 'OFF — case (b): the owner ruled school 1.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/eval.go level PHL/PLH path',
          range: 'true / false',
          systemDefault: 'false',
          recommended:
            'OFF — owner ruling: school 1 (D3.4 p3 @09:57–10:27, @12:38) over D3.4 p1 @17:48–18:00; knob rule (b).',
          whenToTouch:
            'Only if the owner reverses the school-1 ruling for levels.',
          perSession: 'No.',
        },
        {
          label: 'Trendline is a target',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D3.3 p1 @05:18–05:35 "Target là về những level kế tiếp… gần đỉnh cũ, EMA 34, EMA 9, con cá, trendline, anything… những cái mà nó ngán đường anh chị trên đường đi" (the target is the next level — near the old high, EMA 34, EMA 9, the fish, a trendline, anything that blocks your path) and @07:57–08:04 "Thì cứ target nè: box, đỉnh cũ, EMA 34, trendline" (so target: the box, the old high, EMA 34, the trendline). true (default) = a valid trendline in the trade’s path is a target and counts as the first obstacle for the room rule; false = the pre-#25 rule (a trendline is a location only, never a target). Knob rule case (a), ON.',
          trader: 'ON — case (a).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/eval.go nextLevelBeyond (target ladder)',
          range: 'true / false',
          systemDefault: 'true',
          recommended:
            'ON — D3.3 p1 @05:18–05:35, @07:57–08:04; knob rule (a): course, no ruling against.',
          whenToTouch: 'Rarely — OFF only to compare with the pre-#25 rule.',
          perSession: 'No.',
        },
        {
          label: 'Trendline reject between two boxes',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D3.2 p2 @10:56–11:20 "Đụng trendline, nên đóng dưới, đánh ngược lại xuống dưới… dưới cái box" (touch the trendline, close below, trade back down… to the box) — against the same lesson @08:34–08:55 "Nếu như nằm bên giữa cái box mình sẽ không được bắt bất kỳ một lệnh Potential 2 low" (between the boxes you may not take any potential-2-low). ON (default, course-defaults-30) = a trendline reject between an FTGL and an FTGH may trade; OFF = every level PHL/PLH between two boxes is refused (phl_mid_range_boxed), only the ISB trades there. The course trades the trendline reject, so it is ON.',
          trader: 'ON — course-defaults-30.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/eval.go midRangeBoxed',
          range: 'true / false',
          systemDefault: 'true',
          recommended: 'ON — D3.2 p2 @10:56–11:04 (the course trades it).',
          whenToTouch: 'Rarely — OFF only to restore the pre-30 rule.',
          perSession: 'No.',
        },
        {
          label: 'Leg reset on a wave break',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'X-FOMO extra @06:10–06:22 "Nếu mà anh chị đã thua một lệnh á, đợi cho nó break được cái đỉnh, hoặc đợi cho nó break được cái đáy của con sóng đó, thì nó sẽ tạo 1 cái range mới để mình tiếp tục mình trade" (after a losing trade, wait for the high to break OR the low of that wave to break — that makes a new range to keep trading). The course is silent on the leg budget; this extra decides. true (default) = a leg closed by a stop-out ALSO resets when a 1m candle closes beyond the losing trade’s wave extreme (long: below the wave low from the fill through the stop-out candle; short: above the wave high); false = only a close beyond the old high/low resets it (pre-#25). Knob rule case (a), ON.',
          trader: 'ON — case (a).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/limits.go resetBreaks',
          range: 'true / false',
          systemDefault: 'true',
          recommended:
            'ON — X-FOMO extra @06:10–06:22; knob rule (a): no ruling against.',
          whenToTouch: 'Rarely — OFF only to compare with the pre-#25 rule.',
          perSession: 'No.',
        },
        {
          label: 'Flag wall as a target',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'X9 extra @05:47–06:12 "Nếu mà anh chị vô cái lệnh Potential high low chỗ này, thì cái target anh chị về chỗ nào? … Có phải là target của anh chị là chỉ là trên cái đỉnh của cái flag không, là trên cạnh trên không? Thì nếu như anh chị nào vẫn muốn vô thì mình vẫn scalping được, không sai" (a trade taken inside the flag targets its upper edge — a scalp is still fine). Ported from PR #416. ON (default, course-defaults-30) = when a POTENTIAL HIGH/LOW (PHL/PLH) entry sits strictly INSIDE a live flag (the NEAREST unbroken support line below the entry and the NEAREST unbroken resistance line above it — lines of the traded frame first, a 1H/4H line only when no traded-frame line of that side brackets the entry; the oldest pair of the day is no longer the flag, c52 W9), the OPPOSITE wall becomes the target if it is closer than the normal target; a wall that leaves less than 1:1 refuses the entry (flag_wall_room; flag_wall_target_used counts each re-target). ISB, box, ORB and 4h-swing entries are NOT cut to a flag wall — the mentor\u2019s sentence is about a Potential high/low order only (c52 W9 B22). OFF = targets come from the level ladder only (where trendline_is_target already puts a VALID trendline).',
          trader: 'ON — course-defaults-30.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/flag_wall.go applyFlagWallTarget (Tick defer)',
          range: 'true / false',
          systemDefault: 'true',
          recommended: 'ON — X9 @05:47–06:12.',
          whenToTouch: 'Rarely — OFF only to restore the pre-30 rule.',
          perSession: 'No.',
        },
        {
          label: 'Box new zone after escape',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D3.4 p3 recovered2 @00:00–00:21 "khi nào nó rớt xuống đây, nó bật ngược lên, nó tạo cái failure to go lower dưới này nè… thì anh chị mới có được cái range giữa cái này và cái này" (when it drops down here, bounces back, and makes a failure-to-go-lower down here — only then do you get the range between this and this) — a range escape makes a NEW zone, not a redraw. D3.2 p2 @09:47–09:54 "Đừng vẽ vùng thứ 3… Rối lắm" (do not draw a third zone… it gets messy) caps it: the new zone must sit entirely outside the escaped range. D4.1 p2 @02:38–03:31: a higher high / lower low does NOT redraw the drawn box, and the drawn box is never deleted. true (default) = after a standing FTGH/FTGL box escapes (a closed 1m body fully outside), ONE new same-role box is latched from confirmed swings that form after the escape and lie wholly beyond the old range (a new FTGL below the floor, a new FTGH above the ceiling); the new box is the one live box of its role — the live set is always at most one floor and one ceiling, so the escaped box retires from the live reads (a floor escape never drops the standing ceiling), one new zone per further escape. false = the pre-#29 rule (the first box per role per day is the only box). Knob rule case (a), ON.',
          trader: 'ON — case (a).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/box.go latchPostEscapeBoxes / boxAfterEscapeBuild',
          range: 'true / false',
          systemDefault: 'true',
          recommended:
            'ON — D3.4 p3 recovered2 @00:00–00:21; knob rule (a): course, no ruling against.',
          whenToTouch: 'Rarely — OFF only to compare with the pre-#29 rule.',
          perSession: 'No.',
        },
        {
          label: 'PHL min candles from old extreme',
          where: 'Strategy → Mentor mode → PHL/PLH',
          what: 'The entry must be at least this many candles from the old extreme (wait 1–2 more pullback candles).',
          trader: '3.',
          consumer: 'kernel/mentor/mentor.go PHLMinCandlesFromExtreme',
          range: 'candles',
          systemDefault: '3',
          recommended: '3 — D2.2 p2 @ 05:25.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'PHL target shy',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #52 C16. The PHL/PLH target sits this far short of the old high/low — NEAR it, not at it ("cái target của potential high low là gần đỉnh cũ… chứ không phải exactly ngay tại đáy cũ" [Ngày 4.1 Go over ngày 3 part1 @00:07:21–00:07:40; Ngày 2.2 part1 @00:06:11–00:06:22]). The course gives NO distance, so the 5 is OURS, not the mentor’s. It decides whether the 1:1 floor and the room rule pass, so it shapes which PHL/PLH exist. Blank = 5; an explicit 0 puts the target AT the old extreme.',
          trader:
            '5 pts — ours, not the course; the mentor has not named a number.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (PHLTargetShyPts) · kernel/mentor/phl.go phlTarget',
          range: '0–50 pts',
          systemDefault: '5',
          recommended:
            '5 until the mentor names the distance — the worked example [D2.2 p1 @ 06:50] shows a target near the high, not a number.',
          whenToTouch: 'Only when the mentor gives the number.',
          perSession: 'No.',
        },
        {
          label: 'PHL entry buffer',
          where: 'Strategy → Mentor mode → PHL/PLH',
          what: 'The PHL/PLH order sits this far BEYOND the candle extreme (outward) — the course draws the entry ~1 pt past the broken candle\u2019s high/low, never exactly at it. This is the whole offset on the wire (until release #22 the executor added 2 more ticks, so it went out 1.5 past). The stop stays at the candle\u2019s extreme.',
          trader: '1.0 pt.',
          consumer:
            'kernel/mentor/mentor.go PHLEntryBufferPts · kernel/mentor/phl.go',
          range: 'pts',
          systemDefault: '1.0',
          recommended:
            '1.0 — D2.2 p1 @ 06:50 drawn (high 29,396.25 → entry 29,397.25).',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Wick microscalp',
          where: 'Strategy → Mentor mode → setups',
          what: 'D4.3 advanced wick read: 2+ consecutive CLOSED 5m candles rejecting with wicks the SAME way (lower wicks in an uptrend, upper wicks in a downtrend) put a bounded target at their far wick — target-only, never a location. OFF by default ("đừng có tập khúc này đầu tiên").',
          trader: 'OFF.',
          consumer: 'kernel/mentor/mentor.go WickMicroscalpEnabled',
          range: 'true / false',
          systemDefault: 'OFF',
          recommended:
            'OFF — D4.3 @00:00–03:20 advanced; the course says practise it later.',
          whenToTouch: 'Only after the core setups are proven on SIM.',
          perSession: 'No.',
        },
        {
          label: 'Stop ceiling',
          where: 'Strategy → Mentor mode → risk',
          what: 'The evaluator’s hard stop ceiling for PHL/PLH stops and box-return stops — its only two readers. The ISB is not checked against it: the ISB (and the reverse ISB) meet the mentor_isb_max_pts ceiling (default 25, the SAME knob the trader R8 gate reads), which this value does not move.',
          trader: '25 pts — the SWING4H is exempt (its ceiling is 100).',
          consumer:
            'kernel/mentor/mentor.go StopCeilingPts — read only by kernel/mentor/phl.go phlPLHR2 and kernel/mentor/box_trade.go boxEntryIntent',
          range: 'pts',
          systemDefault: '25',
          recommended: '25 — D3.3 p1 @ 02:04.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Room multiple',
          where: 'Strategy → Mentor mode → risk',
          what: 'The room to the FIRST available level must be at least this × the first take-profit — leg 1 at 1:1 → 2R; a confluence (mode C) leg 1 at 1:2 → 4R [D5.3 p1 @09:16–10:17 · D2.2 p3 @12:13 · D1.2 p1 @07:41–08:45]. Applies to PHL/PLH, the ISB, the reverse ISB and the box (the swing reject: see below). Always measured to the LEVEL — on a spent day the 15-pt take-profit cap does not shrink it, and the room must also be ≥ this × the capped take-profit (target 15 → room 30) [D5.3 p1 @09:32–09:50]. The swing reject measures its 5m EMA 34 as its FIRST TARGET, not an obstacle (mentor_swing_ema34_is_first_target, default ON since release #25); with that switch OFF the swing keeps the old rule (an on-side 5m EMA 34 closer than 2R refuses it).',
          trader: '2×.',
          consumer: 'kernel/mentor/mentor.go RoomMultiple',
          range: '×',
          systemDefault: '2',
          recommended: '2 — D5.3 p1 @ 09:16.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Near-box room',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #52 B11. Refuse an ISB or PHL/PLH whose nearest box edge in the trade direction is closer than this × its own risk — the setup is "sát box" ("Insaba Sword ngay chỗ đó mình không được đánh… sát box gần box gần supply quá mình không được đánh" [Ngày 3.2 FTGH FTGL part1 @00:22:44–00:23:10]). The mentor gives NO distance, so the 2× is OURS (the same room the target rule demands), not the course. Exempt only between two DIFFERENT boxes: one wholly above the entry and another wholly below. A price inside a single box is not exempt. Blank = 2; an explicit 0 turns the refusal off for every setup.',
          trader:
            '2× — ours, not the course. A setup between two different boxes is allowed.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (NearBoxRoomMultiple) · kernel/mentor/box.go nearBoxRefusal',
          range: '0–10 × (0 = off)',
          systemDefault: '2',
          recommended:
            '2 until the mentor names the distance — his own example (an ISB short 10–15 pts above an FTGL with a 5–6 pt risk) is 2–3R, so a strict 2× may refuse the 3R end. Between-two-boxes exemption row 25 @ 23:14–24:03.',
          whenToTouch:
            'Only when the mentor gives the number. Zero disables the near-box refusal entirely.',
          perSession: 'No.',
        },
        {
          label: 'Mid-range gap',
          where: 'Strategy → Mentor mode → filters',
          what: 'Refuse PHL/PLH when price is within this many points of a pair of levels bracketing it. Zero disables this proximity filter.',
          trader:
            '0 = OFF. The separate FTGL/FTGH box no-trade rule is not controlled by this knob.',
          consumer: 'kernel/mentor/filters.go Config.RangeGapPts',
          range: 'pts (0 = off)',
          systemDefault: '0 (disabled)',
          recommended:
            '0 — Engineering rule: code constant (optional proximity filter), not from the course.',
          whenToTouch:
            'Only when deliberately tuning the level-distance filter.',
          perSession: 'No.',
        },
        {
          label: 'Day gate spent threshold',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'The Globex run (17:00 → 08:30 CT) at/above this before the open = a spent day.',
          trader:
            '300 pts — "already run 300–400 before the open → 80–90% it ranges".',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (DayGateSpentPts)',
          range: 'pts',
          systemDefault: '300',
          recommended: '300 — D5.1 p1 @ 15:57.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Day gate target cap',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'On a spent+agree day, cap the target distance; skip any setup whose stop is over this.',
          trader: '15 pts — "15 điểm bán, 10 điểm bán".',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (DayGateTargetCapPts)',
          range: 'pts',
          systemDefault: '15',
          recommended: '15 — D5.1 p1.',
          whenToTouch: 'With the spent tier.',
          perSession: 'No.',
        },
        {
          label: 'Trigger school',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'School 1 allows level and box entries before the 5m trigger agrees, then upgrades on a later flip; school 2 waits for agreement. The trigger-side and no-trade-zone rules still apply.',
          trader: '1 = enter at the level; 2 = wait for the 5m trigger.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (TriggerSchool)',
          range: '1 / 2',
          systemDefault: '1',
          recommended:
            '1 — Owner setting: school 1 = enter at the level (the mentor’s own approach, B20); school 2 = wait for the 5m trigger.',
          whenToTouch: 'Only to compare school 1 with school 2.',
          perSession: 'No.',
        },
        {
          label: 'Ping-pong minimum gap',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'The FTGL-to-FTGH gap must be strictly greater than this before a box-edge ping-pong entry is allowed.',
          trader: '50 pts. Tunable in Studio (blank = default).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (PingPongMinGapPts)',
          range: 'pts',
          systemDefault: '50',
          recommended: '50 — B21 gap ruling.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Ping-pong candle maximum',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Reject the box ping-pong if the largest candle in the lookback exceeds this size. Above 0 it also switches on the key-level pair rule: the touch candle AS IT PRINTS (the current closed 1m candle) as big as the gap between the touched key level and the next one → sit out [X4 @06:31]; counted once per visit (keypair_candle_too_big).',
          trader: '20 pts. Tunable in Studio (blank = default).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (PingPongCandleMaxPts)',
          range: 'pts',
          systemDefault: '20',
          recommended: '20 — B21 candle-size ruling.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Ping-pong candle lookback',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'How many closed 1m candles to scan for the largest-candle gate on the box ping-pong. The key-level pair rule does not use it — it reads the current candle only.',
          trader: '30 closed 1m candles. Tunable in Studio (blank = default).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (PingPongCandleLookback)',
          range: 'closed 1m candles',
          systemDefault: '30',
          recommended: '30 — B21 lookback.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Maximum level visits per day',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Allow this many reject visits to each level in a day; later visits are refused (level_visit_cap). A "level" is one real line: a key level, an old high/low, one trendline — and the 5m trigger line AT ITS CURRENT PRICE (each reversal draws a new line, so a moved line starts at zero). The EMA 34 lines are never capped: they re-price every candle, and the course gives them their own rules instead (too many crossings in 30 minutes → off; a loss at the line → off until price leaves). Zero disables the cap.',
          trader: '3 visits; 0 = off. Tunable in Studio (blank = default).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (LevelMaxVisits)',
          range: 'visits (0 = off)',
          systemDefault: '3',
          recommended:
            '3 — B23 “knock knock” ruling (D1.3 p1 @10:23–11:43: a level tested 1, 2, at most 3 times holds; more and it breaks).',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'ORB entry gate',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Require price to escape the first 2-minute RTH opening range before intraday entries (a 1m BODY wholly outside — open AND close; the wick-inclusive "completely outside" test is the separate knob orb_escape_completely, OFF by default); after that only the side of the latest escape trades — a later body escape beyond the other edge switches it. The 4h swing setup is exempt.',
          trader: 'ON — no entry inside the opening range.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (OrbGateEnabled)',
          range: 'true / false',
          systemDefault: 'true',
          recommended:
            'ON — X5 @01:52, 02:36, 03:29–03:47 (§7 step-0 gate, extras).',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Swing EMA period',
          where: 'Strategy → Mentor mode → SWING4H',
          what: 'The 4h EMA period for the swing line.',
          trader: '34.',
          consumer: 'kernel/mentor/swing4h.go SwingCfg.EMAPeriod',
          range: 'period',
          systemDefault: '34',
          recommended:
            '34 — Engineering rule: the swing line period, not from the course.',
          whenToTouch: 'Never.',
          perSession: 'No.',
        },
        {
          label: 'Swing line offset',
          where: 'Strategy → Mentor mode → SWING4H',
          what: 'The line placement offset — toward the approaching price (the 4h is too coarse to place exactly).',
          trader: '5 pts.',
          consumer: 'kernel/mentor/swing4h.go SwingCfg.LineOffsetPts',
          range: 'pts (5–10)',
          systemDefault: '5',
          recommended:
            '5 — R8 (RULES-FIX v3): the line sits 5–10 pts toward the approaching price; 5 is the low end of that range (kernel/mentor/swing4h.go DefaultSwingCfg) — not the size table.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Swing stop distance',
          where: 'Strategy → Mentor mode → SWING4H',
          what: 'The clean-rejection stop distance beyond the line.',
          trader: '30 pts — 30–60 allowed.',
          consumer: 'kernel/mentor/swing4h.go SwingCfg.StopBeyondLinePts',
          range: 'pts',
          systemDefault: '30',
          recommended: '30 — D5.2 table.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Swing max stop',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'A swing stop at/above this → do not enter. The swing is EXEMPT from the ISB stop ceiling (mentor_isb_max_pts, default 25).',
          trader: '100 pts.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (SwingMaxStopPts)',
          range: 'pts',
          systemDefault: '100',
          recommended: '100 — D5.2 table.',
          whenToTouch: 'Never below 25+30.',
          perSession: 'No.',
        },
        {
          label: 'Swing min stop',
          where:
            'No Studio control — stored strategy setting risk_control.mentor_tuning.swing_min_stop_pts (API/config field)',
          what: 'A swing stop NARROWER than this → do not enter (degenerate geometry). 0 = OFF — the course states no minimum swing stop (only the 100-pt ceiling and "a stop near 100 → don\'t enter" [06-SIZING]).',
          trader: 'OFF (0).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (SwingMinStopPts) · kernel/mentor/swing4h.go SwingCfg.MinStopPts',
          range: 'pts · 0 = OFF',
          systemDefault: '0 (OFF)',
          recommended:
            '0 — R5 (new behaviour behind a knob, OFF): the course states no minimum [06-SIZING: 30–60 pt stops, near 100 → "đừng vô"].',
          whenToTouch: 'Only with a new owner ruling; 0 = today’s behaviour.',
          perSession: 'No.',
        },
        {
          label: 'Swing entry buffer',
          where: 'Strategy → Mentor mode → SWING4H',
          what: 'Buffer beyond the reference candle’s extreme. R8: NO buffer — the order sits tight on the candle, on the wire too (until release #22 the executor added 2 ticks to every mentor stop entry).',
          trader: '0.',
          consumer: 'kernel/mentor/swing4h.go SwingCfg.EntryBufferPts',
          range: 'pts',
          systemDefault: '0',
          recommended: '0 — R8: the order sits tight on the candle.',
          whenToTouch: 'Never — R8 removed the buffer.',
          perSession: 'No.',
        },
        {
          label: 'Swing target EMA period',
          where: 'Strategy → Mentor mode → SWING4H',
          what: 'The first target is max(1R, the warmed 5m EMA 34) on the profitable side — the EMA wins only when it sits further than 1R; absent or on the wrong side, the target is exactly 1R.',
          trader: '34 on the 5m.',
          consumer: 'kernel/mentor/swing4h.go SwingCfg.TargetEMA5mPeriod',
          range: 'period',
          systemDefault: '34',
          recommended:
            '34 — D5.2 p2 @11:17 "TARGET 1-1 TRƯỚC" (first target = the warmed 5m EMA 34).',
          whenToTouch: 'Never.',
          perSession: 'No.',
        },
        {
          label: 'Swing leeway candles',
          where: 'Strategy → Mentor mode → SWING4H',
          what: 'The 4h level touch window is the first 2 touching 5m candles. The entry may be armed off either of them; if either of those two candles closes beyond the level there is NO order (cancel any resting arm, spend the level). SWING4H only — every other setup keeps first-touch = 1 candle.',
          trader: '2 candles.',
          consumer: 'kernel/mentor/swing4h.go SwingCfg.LeewayCandles',
          range: 'candles',
          systemDefault: '2',
          recommended: '2 — D5.2 p1 @ 15:21–15:48.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Swing hold bars',
          where: 'Strategy → Mentor mode → SWING4H',
          what: 'Hold to the close of the N-th 4h candle AFTER the entry candle (the entry candle itself never counts; real 17:00 CT-anchored candles; capped at the market close — never held through the 16:00 CT halt or a weekend).',
          trader:
            '1 — hold to the close of ONE full 4h candle after the entry candle [D5.2 p1 @06:03–06:20].',
          consumer: 'kernel/mentor/swing4h.go SwingCfg.Hold4hBars',
          range: '4h candles',
          systemDefault: '1',
          recommended: '1 — D5.2 p1 @ 06:03–06:20.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Swing respects 5m zone',
          where: 'Strategy → Mentor mode → SWING4H',
          what: 'Gate swing entries on the 5m trigger zone when true.',
          trader:
            'OFF — §8 is a self-contained 4h → 5m procedure; nothing ties it to the trigger lines.',
          consumer: 'kernel/mentor/swing4h.go SwingCfg.Respects5mZone',
          range: 'true / false',
          systemDefault: 'false',
          recommended:
            'false [C] — Engineering rule: not stated in the method, not from the course.',
          whenToTouch: 'Experimentation only.',
          perSession: 'No.',
        },
        {
          label: 'Box timeframe',
          where: 'Strategy → Mentor mode → boxes',
          what: 'The FTGH/FTGL box timeframe. 1m REGULAR is the course frame; 2m and 5mha are 2025 extras, off by default. 5mha runs the same FTGH/FTGL two-swing algorithm on 5m Heikin Ashi candles — it is NOT the 2025 one-colour-change supply/demand zone (X10) and NOT the 2026 box.',
          trader: '1m.',
          consumer: 'kernel/mentor/box.go BoxCfg.TF',
          range: '1m / 2m / 5mha',
          systemDefault: '1m',
          recommended: '1m — D3.3 part1_06-25 frame.',
          whenToTouch: 'Experimentation only.',
          perSession: 'No.',
        },
        {
          label: 'Box touch band',
          where: 'Strategy → Mentor mode → boxes',
          what: 'A wick within this of a box edge counts as a touch (every return to the box is a trade, the first one included).',
          trader: '0.25 pts.',
          consumer: 'kernel/mentor/box.go BoxCfg.TouchBandPts',
          range: 'pts',
          systemDefault: '0.25',
          recommended:
            '0.25 — Engineering rule: code constant (wick touch tolerance), not from the course.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Leg budget',
          where: 'Strategy → Mentor mode → limits',
          what: 'At most two entries per leg (the PHL/PLH plus a same-direction ISB); a stop-out closes the leg; a 1m close beyond the prior extreme starts a new one — and a stopped-out leg also resets on a 1m close beyond the losing wave\u2019s extreme (leg_reset_on_wave_break, default ON).',
          trader: 'ON — the method runs the budget (X15 @00:36–02:46).',
          consumer:
            'kernel/mentor/limits.go Config.LegBudgetEnabled · trader/mentor_mode.go mentorLegBudgetEnabled (stored key mentor_leg_budget_enabled)',
          range: 'true / false',
          systemDefault: 'true',
          recommended: 'true — G1 ruling 2026-10-03.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Leg reset on',
          where: 'Strategy → Mentor mode → limits',
          what: 'What opens a new leg after the budget is spent: "close" (default) = a 1m close beyond the prior high/low; "touch" = a wick reaching it (kernel/mentor/limits.go resetBreaks). A leg closed by a stop-out also resets on a 1m close beyond the losing wave\u2019s extreme (leg_reset_on_wave_break, default ON).',
          trader: 'close.',
          consumer:
            'kernel/mentor/limits.go Config.LegResetOn (resetBreaks) · trader/mentor_mode.go mentorLegResetOn (stored key mentor_leg_reset_on)',
          range: 'close | touch',
          systemDefault: 'close',
          recommended: 'close — G1 ruling 2026-10-03.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Level revisit distance',
          where: 'Strategy → Mentor mode → levels',
          what: 'Extra departure distance (pts) a closed candle must clear before a new visit starts a new level reference.',
          trader: '0 — the method states none.',
          consumer:
            'kernel/mentor/touch.go visitTick (Config.LvlRevisitMinPts) · trader/mentor_mode.go mentorLvlRevisitMinPts',
          range: 'pts',
          systemDefault: '0',
          recommended:
            '0 — L1 ruling 2026-10-03 (the mentor never states one).',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'EMA 34 cross cap (30m)',
          where: 'Strategy → Mentor mode → EMA34',
          what: 'Cap crossings around the EMA line in a 30m window (“xien len xien xuong”). The mentor gave no number, so the question is open (Q21).',
          trader:
            'OFF (removed, #45) — the mentor gives no count; the “switched off only while it is not respected” rule is the loss relock (E2), not a cross count.',
          consumer: 'kernel/mentor ema_max_cross_30m',
          range: 'int / 0 = off (reachable since W32)',
          systemDefault: '0',
          recommended:
            '0 — REMOVED default (#45): “xien len xien xuong” (D4.2 p1 @22:27-22:30) is qualitative; the mentor never gives a count or a window, and the frame at @22:28 shows the indicator already OFF. An explicit 2-4 re-enables the numeric gate.',
          whenToTouch: 'Only when the mentor answers Q21.',
          perSession: 'No.',
        },
        {
          label: 'Loss departure distance',
          where: 'Strategy → Mentor mode → limits',
          what: 'Structure is the loss area. A fixed-point departure is an optional fallback: when greater than zero, a later closed candle this far from the loss price can unblock a place.',
          trader: '0 = OFF; structural departure remains the primary rule.',
          consumer:
            'mentor_loss_departure_pts → kernel/mentor Config.LossDeparturePts',
          range: 'pts',
          systemDefault: '0 (OFF)',
          recommended:
            '0 — B22 makes structure primary; fixed points are fallback only.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: '5m trigger at locations',
          where: 'Strategy → Mentor mode → levels',
          what: 'The 5m trigger filter at levels and boxes (“bat buoc”). Inert under trigger_school 1 (enter at the level — the filter is only consulted when the 5m trigger school is ON). OFF only for the sensitivity row that measures what it costs.',
          trader: 'ON.',
          consumer: 'kernel/mentor location trigger filter',
          range: 'true / false',
          systemDefault: 'true',
          recommended:
            'true — L3 ruling 2026-10-03; v5_loc_notrig is the off-row.',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Done after a winning day',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Mentor mode → "Stop for the day after a winning trade"',
          what: 'Stop new mentor entries for the mentor\u2019s NY day after a winning trade closes and the day\u2019s net P&L is positive. ON by default (the course default; m186 reverts the 2026-10-16 owner order: nil → ON, an explicit false turns it off). The day boundary is 08:30 CT (the NY open) — owner ruling 2026-10-09; a win at 18:06 CT no longer blocks the next NY open.',
          trader:
            'ON by default; an explicit false disables this stop rule. The day boundary is the knob mentor_done_after_win_day_start ("HH:MM" CT; unset → 08:30, the NY open). Set it to 17:00 to restore the old CME-session-day behaviour.',
          consumer: 'trader/mentor_tick.go mentorDoneAfterWinGate',
          range: 'true / false',
          systemDefault: 'true (unset = ON)',
          recommended:
            'ON — the course default (D1.2 p1 @20:53, p2 @06:09 "làm ơn đóng máy lại"). Day boundary: 08:30 CT (owner ruling 2026-10-09).',
          whenToTouch: 'Rarely.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Stop after a loss',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Mentor mode → "Stop for the day after a losing trade"',
          what: 'STOP-AFTER-LOSS: once a mentor trade closes today with a net LOSS (both legs combined, pnl_corrected < 0), refuse new mentor entries until the next session day (17:00 CT) [D1.2 p1 @ 23:34]. A breakeven close (0) is NOT a loss. Fail-closed while ON: an unwired source or an unresolved close (NULL pnl_corrected) refuses.',
          trader:
            'OFF by default (the switch reads ON only when the value is true). Flip the switch to turn it ON, then press Save: the running trader reloads on save, so no restart is needed. An explicit false turns it back OFF.',
          consumer: 'trader/mentor_tick.go mentorStopAfterLossGate',
          range: 'true / false',
          systemDefault: 'false (unset = OFF)',
          recommended:
            'OFF — enabled only on an owner ruling (the loss-stop is his personal routine).',
          whenToTouch: 'Only on an owner ruling.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Trading window start',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Mentor mode → "Trading window start (CT)"',
          what: 'The CT time when the entry window opens; the rule blocks new entries outside the window. The 4h swing is NOT limited by this window: the mentor takes it whenever price touches the 4h EMA 34 (Ngày 5.2 @06:27).',
          trader:
            '08:30 CT by default; SWING4H is exempt. A time box under the Mentor mode switch: type or pick HH:MM (anything else is not saved and the box says so). A line under it previews the window ("Mentor trades 08:30–09:30 CT"). Press Save like the switch: the running trader reloads, no restart. The window may cross midnight: 23:00 with 120 minutes is open until 01:00 CT the next day.',
          consumer: 'trader/mentor_tick.go mentorWindowGate',
          range: 'HH:MM CT',
          systemDefault: '08:30',
          recommended: '08:30 CT — owner ruling.',
          whenToTouch: 'Only when the approved trading window changes.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Trading window length',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Mentor mode → "Window length"',
          what: 'How long the entry window stays open after its start time; SWING4H is exempt. The 4h swing is NOT limited by this window: the mentor takes it whenever price touches the 4h EMA 34 (Ngày 5.2 @06:27).',
          trader:
            '60 minutes by default; an unset/zero strategy value inherits 60. The "Window length" box offers 30 / 60 / 90 / 120 minutes or "No window (EOD rules still apply)", which saves -1 and removes the time window only — not any hour: no NEW mentor entry from 15:45 CT until the 17:00 CT reopen (trader/mentor_mode.go mentorPastDailyHaltCutoff), and from the EOD flat (NY 14:45 CT; earlier on a registered half-day) until 17:00 every intraday mentor order is cancelled and an open intraday position is closed (trader/auto_trader_clock.go enforceEODFlatAt; the SWING4H is exempt from the flat). A set window only adds a further limit. A stored 0 cannot mean "no window" because 0 means unset. Same Save, same reload as the switch.',
          consumer: 'trader/mentor_tick.go mentorWindowGate',
          range: '30 / 60 / 90 / 120 minutes, or -1 = no window',
          systemDefault: '60',
          recommended: '60 minutes — owner ruling.',
          whenToTouch: 'Only when the approved trading window changes.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Stale-data block',
          where:
            'No Studio control — ON by default; the strategy setting mentor_stale_data_block=false turns it off',
          what: 'Refuse NEW mentor arms (authoring and the armed placement pass) while the live 1m feed is stale — the same B4 formula the AI path uses (expected-open − 1 bar − 15s grace ≈ 75 s) PLUS, since release #22, two legs that never read the wall clock: the last live 1m frame must have ARRIVED within 135 s on the monotonic clock, and for 120 s (CLOCK_STEP_HOLD_S, default 120) after any wall-clock step of 6 s or more (CLOCK_STEP_FREEZE_MS, default 6000 ms) NEW entries are held; steps of 2–6 s are logged and counted, never held — except inside the CME open/close boundary (15:59–16:01 and 16:59–17:01 CT), where any step of 2 s or more still holds. The wall-clock rule alone failed OPEN when the clock stepped back (2026-10-14: −125 s at 00:23:00): a dead feed read as fresh. While CME is closed (the daily 16:00–17:00 break, weekends) the feed legs never fire. Exits and protection are never blocked, and already-resting broker orders are not cancelled by it.',
          trader:
            'ON by default (nil → ON — the same default posture as B4); an explicit false disables it. One WARN and one "stale_data_episodes" funnel count per stale episode, never per tick (each refused arm counts "placement_refused_stale_data"). The block fails CLOSED when a live-frame provider is wired but reports no live 1m frame since boot (even with an empty 1m cache); it fails OPEN only when the 1m cache is empty AND no bar/receipt provider is wired (a cold start — the seed and the FEED DOWN alert own that case).',
          consumer:
            'trader/mentor_mode.go mentorStaleDataBlocked · kernel/feed_clock.go EntryFeedStale (wall rule StaleEntryGateFeed + monotonic receipt age + clock-step hold)',
          range: 'true / false',
          systemDefault: 'true (unset = ON)',
          recommended:
            'ON — never trade on a delayed 1m feed; B4 already refuses the AI path the same way.',
          whenToTouch: 'Rarely — only to explicitly disable the block.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Swing: 5m EMA 34 is the first target',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'The swing’s 5m EMA 34 is its FIRST TARGET. Before release #25 the room rule measured that same EMA as an OBSTACLE needing 2R of room, and every swing whose 5m EMA 34 sat closer than 2× its ~30+ pt stop (about 60 pts) was refused — the mentor’s own 135-pt showcase among them. ON: no room refusal against the EMA; the target stays max(1R, the 5m EMA 34).',
          trader:
            'Course: “Target đầu tiên anh chị, hold thẳng về EMA 34 5 phút luôn cho em” (first target: hold straight to the 5m EMA 34) [D5.2 p2 @01:44–01:48]; “anh chị nên cái target đầu tiên là EMA 34 của 5 phút” [D5.2 p2 @06:14]. D5.2 applies no room rule to the swing. Case (a): the course, no ruling against.',
          consumer:
            'kernel/mentor/course_exits.go swingRoomFilterRefuses ← runSwing · trader/mentor_course_exits.go mentorSwingEMA34FirstTarget',
          range: 'ON / OFF',
          systemDefault: 'ON (unset = ON)',
          recommended:
            'ON — D5.2 p2 @01:44 calls the 5m EMA 34 the first target (case a). OFF restores the pre-#25 refusal (R68).',
          whenToTouch: 'Only to restore the old room refusal for the swing.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Swing runner',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25); size = mentor_swing4h_contracts',
          what: 'With 2+ swing contracts, split the swing: leg 1 (half, rounded up) takes profit at the first target (max(1R, the 5m EMA 34)); the runner is held by the 4h — break-even at +1R and the close at the end of ONE 4h candle [D5.2 p1 @06:03–06:20] (the swing’s own rules), with its take-profit capped at max(100 pts, 2× the first-target distance). The hold-close never closes more than NinjaTrader still holds. With 1 contract the switch does nothing (counted swing_runner_needs_2_contracts).',
          trader:
            'Course: “Nhưng đừng vô 1 MNQ / Vô Addict là 2 của MNQ để có đường bắn bớt” (not 1 MNQ — at least 2, so you can take some off) [D5.2 p2 @06:08–06:23]; “gồng theo khung 4 giờ” [D5.2 p1 @05:52]; “hô tới đâu luôn” [D5.2 p2 @05:50]. He swings 1 MNQ himself (“Em vô đúng 1 MNQ thôi” [D5.2 p1 @00:43]). The 100-pt cap is the top of the course’s 50–100 pts per 4h candle — the course names no runner take-profit [C]. Case (b): sizing is the owner’s call.',
          consumer:
            'trader/mentor_course_exits.go mentorSwingRunnerLegs ← trader/mentor_actions.go mentorArmIntent · mentorSwingCloseQty ← mentorClosePosition',
          range: 'ON / OFF',
          systemDefault: 'ON (unset = ON; swing size 2 — course-defaults-30)',
          recommended:
            'ON — course-defaults-30 (D5.2 p2 @06:19 says at least 2). The swing size defaults to 2 so the split has a runner.',
          whenToTouch: 'OFF to go back to a single swing bracket.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Resonance runner TP past the old high',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'Mode A (a same-direction ISB within 3 candles after a PHL/PLH fill, in candles of the entry frame, passing every placement gate except the never-add gate): the position flips to mode A. OFF (the default): the RUNNER has NO cap — its resting take-profit is moved far out of reach (600 points beyond the entry, ours) on the runner’s own wire leg (the AddOn cannot delete a take-profit leg), so it exits only by its break-even stop, X8 or the EOD flat; leg 1’s 1:1 stays; the runner’s break-even goes out on the closed 1m candle after leg 1’s 1:1 partial is confirmed. ON: the RUNNER’s take-profit is instead moved to the level beyond the old high (the bot’s older inference). A one-bracket position is never touched.',
          trader:
            'Course: “70-80% là nó sẽ phá luôn cái đỉnh bên tay trái” (70–80% it breaks the left high) [D2.4 p1 @02:02–02:09]; “Để stop loss ở break even / Cho nó chạy” (stop at break-even, let it run) [D2.4 p1 @02:24–02:47]. The course names NO runner target — “let it run” — so the default is no cap (c52 W2 A3, owner order 2026-10-26 “align everything to the course”). Before this the unset default was OFF in the live resolver but ON on the other surfaces, and OFF only logged: the runner kept its PHL/PLH take-profit, shy of the old high.',
          consumer:
            'trader/mentor_course_exits.go mentorResonanceRunnerTP ← trader/mentor_exit_drive.go mentorArmResonanceOnISB',
          range: 'ON / OFF',
          systemDefault: 'OFF (unset = OFF = no cap)',
          recommended:
            'OFF — D2.4 p1 @02:02, @02:24–02:47 (no runner target, “cho nó chạy”). Counters resonance_runner_uncapped / resonance_runner_uncap_not_split; ON: resonance_runner_tp_sent / _none / _not_split / _not_beyond.',
          whenToTouch:
            'ON only to cap the runner at the level beyond the old high again (the bot’s older inference, not the course).',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Break-even before the 1:1 partial',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'ON (default, course-defaults-30): on the first CLOSED candle at least half-way to leg 1’s 1:1 (+0.5R, and past the entry by a tick), BOTH legs’ stops go to break-even — before the partial fills. OFF: owner ruling R3 (superseded by course-defaults-30) — nothing moves until leg 1’s 1:1 is CONFIRMED filled at NinjaTrader, then the runner goes to break-even and trails.',
          trader:
            'Course: entry 10, stop 0, target 20, price at 15 → “mua stop loss về break even là 10 … ở mức 1-1” (move the stop to break-even) [D1.2 p1 @12:41–13:12]; “Bắt buộc … lúc nào cũng phải maintain risk reward ở mức 1-1” (compulsory: keep the live R:R at 1:1) [D1.2 p1 @15:56]. course-defaults-30 (owner “fix all”) reverses R3.',
          consumer:
            'trader/mentor_course_exits.go mentorBEBeforePartialStep ← trader/mentor_trail_r22.go mentorExitDriveCandle (stage 0)',
          range: 'ON / OFF',
          systemDefault: 'ON (unset = ON — course-defaults-30)',
          recommended: 'ON — D1.2 p1 @12:41–13:12, @15:56 (mandatory 1:1).',
          whenToTouch: 'OFF only to restore the R3 behaviour.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'ISB candle-3 partial at any profit',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'ON (default, today’s rule): when the ISB fill candle closes in profit short of 1:1, leg 1’s take-profit is MOVED to a marketable limit at that close through the existing modify_bracket path — long: close − 8 ticks (2.00 pts); short: close + 8 ticks (R1, no AddOn change), clamped to never book a loss (long ≥ entry + 1 tick, short ≤ entry − 1 tick) — re-modified to the new close ∓ 8 ticks each later candle until it fills, with a failed send retried ONCE on the next candle. OFF: leg 1 keeps its +1R take-profit — no candle-3 sale (counted isb_partial_wait_1r). A losing close or a close past +1R always moves nothing (the +1R TP fills or keeps resting).',
          trader:
            'Course, in conflict: “khi cây nến trend kế tiếp đóng là chỗ đó mình phải bán bớt. Bắt buộc. Ở thời điểm hiện tại bắt buộc” (sell part at the candle-3 close — compulsory for now) [D1.4 p1 @12:51–13:07] vs “Còn nếu mà nó đóng lỗ hay là nó đóng còn ít điểm quá … xíu mình sẽ được học” (too few points — learn later, never taught) [D2.1 @11:08–11:23] and “anh làm gì anh cũng phải bán bớt ở 1:1” [D2.2 p3 @12:13]. Case (b): ambiguous → today’s rule stays the default.',
          consumer:
            'trader/mentor_course_exits.go mentorISBC3PartialAnyProfit ← trader/mentor_trail_r22.go mentorISBPartialStep',
          range: 'ON / OFF',
          systemDefault: 'ON (unset = today’s rule)',
          recommended:
            'Leave unset (today’s rule) — D1.4 p1 @12:51 vs D2.2 p3 @12:13 conflict (case b).',
          whenToTouch: 'If the owner rules “sell at 1:1, never smaller”.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Reverse ISB as PHL/PLH',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'ON (default, course-defaults-30): the reverse ISB at EMA 9 is tagged PHL (long) / PLH (short) as the mentor names it — so it exits as a PHL/PLH (no ISB candle-3 partial; the resonance watch), takes the leg budget’s PHL slot, and KEEPS the ISB location size flags (inside a range / at an old extreme → 3). The stop stays the inside bar ± 1.5. OFF: tagged ISB, as before.',
          trader:
            'Course: “Nó là bộ tên solo 2, chứ nó không phải là Insa 3 cho lệnh long” (it is a Potential … 2, not an ISB long) [D5.4 @03:52–03:58]; “Thì tôi đặt lệnh Potential Low 2” [D5.4 @07:56]. course-defaults-30 (owner “fix all”) turns it ON.',
          consumer:
            'kernel/mentor/course_exits.go reverseISBRetag ← kernel/mentor/eval.go (reverse ISB branch)',
          range: 'ON / OFF',
          systemDefault: 'ON (unset = ON — course-defaults-30)',
          recommended:
            'ON — D5.4 @03:52–03:58, @07:33–07:37 (the mentor names it the PHL/PLH).',
          whenToTouch: 'OFF to keep the ISB tag.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'EMA 9 visit cap',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'How the knock-knock cap (3 visits per level per day) counts EMA 9 touches. shared (default): every EMA 9 touch counts against ONE daily budget. per_level: each EMA 9 price counts on its own (like the trigger retest). exempt: EMA 9 is never capped (like EMA 34).',
          trader:
            'The course gives EMA 9 no visit rule: “EMA 9 … là một cái EMA gồng lời” (an EMA for holding a winner) [D5.4 @00:46]; knock knock is about one level tested “1 lần, 2 lần, maybe 3 lần” [D1.3 @10:59–11:13]. Not stated → case (b), shared.',
          consumer:
            'kernel/mentor/course_exits.go visitKeyFor / visitCapAppliesFor ← kernel/mentor/eval.go · trader/mentor_course_exits.go mentorEMA9VisitCap',
          range: 'shared / per_level / exempt (a bad value → shared, counted)',
          systemDefault: 'shared',
          recommended:
            'shared — not from the course: no EMA 9 visit rule is stated (case b).',
          whenToTouch:
            'If EMA 9 rejects are being capped on days it moves a lot.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'ISB max stop',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'Refuse an ISB (and the reverse ISB) whose stop is ABOVE this many points, counted isb_stop_over_max / isbrev_stop_over_max. Default 25 (W-MENTOR-EXACT, owner order 2026-10-21 "i want all exactly like mentor" — "trên nữa thì thôi" [D3.3 p1 @02:04–02:08]): a stop of 20–25 pts is the REDUCED tier, a stop above 25 is REFUSED. This SAME knob is also the trader’s R8 ceiling for every non-swing setup (a PHL/PLH/BOX stop above it is refused R8, even below the kernel’s own StopCeilingPts 25) and the top of the 20–25 reduced band.',
          trader:
            'Course: “nếu như một cái setup insidebar mà nó hai mươi mấy điểm … làm ơn giúp em đừng vô” (an ISB in the twenties — don’t enter) [D4.1 p1 @05:34–05:48]. The line is the course-exact 25 (W-MENTOR-EXACT, owner order 2026-10-21 "i want all exactly like mentor" — “trên nữa thì thôi” [D3.3 p1 @02:04–02:08]).',
          consumer:
            'kernel/mentor/course_exits.go isbOverMaxStop ← kernel/mentor/isb.go ISBStopVerdict · trader/mentor_mode.go mentorRuleGate (R8) + mentorContractsFor (band top) ← trader/mentor_actions.go mentorDispatchEntry',
          range:
            'pts, 0 → 30 (the knob has no OFF — unset/0 resolves to the 30 default)',
          systemDefault: '30',
          recommended:
            '30 — course-defaults-30 owner order; D4.1 p1 @05:41 (the course\u2019s twenties skip, moved to 30 by the owner).',
          whenToTouch: 'Only to change the stop ceiling.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Key-level pair candle ratio',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'The X4 key-level pair rule sits out when the gap to the next key level is at most this × the range of the CURRENT closed 1m candle (the touch candle as it prints; not the largest of the last 30). 1 (default) = today’s rule.',
          trader:
            'Extra X4: “Một cái nến nó bằng cái rank của 2 cái level rồi thì ngồi im” (one candle as big as the gap between two levels → sit still) [X4 @06:36]. The course gives only numbers — 50-pt range with “mười mấy” candles, 50–60 with 30–40-pt candles “đừng bao giờ đụng tới” [D4.2 p2 @05:32–06:35]; no ratio (such as 3×) is spoken → case (b), 1.',
          consumer:
            'kernel/mentor/course_exits.go keypairCandleRatio ← kernel/mentor/eval.go (keypair_candle_too_big)',
          range: '× (0 or unset = 1; a negative value → 1, counted)',
          systemDefault: '1',
          recommended:
            '1 — X4 @06:36 (one candle = the gap → sit still); no course ratio is spoken (case b).',
          whenToTouch: 'If the owner adopts a stricter ratio.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Fresh RTH box pair',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'ON (default, owner ruling 2026-10-25 "if there is a new box use it, if not use the old box"): a fresh FTGH/FTGL that forms in RTH (after 08:30 CT, built from RTH candles only) REPLACES the evening (17:00+) box of THAT role; a role with no fresh RTH box keeps its evening box, and a box formed in RTH stands to the session roll. OFF: one box per role per 17:00 CT session — an overnight box stands through RTH.',
          trader:
            'Built (#51 m429, default ON, owner ruling 2026-10-25 “Có box mới thì dùng box mới; không có thì giữ box cũ”): a fresh RTH FTGH/FTGL replaces the evening box of its role. The course’s own statement is a NEW zone after price leaves the range and builds a new FTGL (“khi nào nó rớt xuống đây nó bật ngược lên nó tạo cái failure to go lower dưới này … thì anh chị mới có được cái range” [D3.4 p3 @00:00–00:21]); “để nguyên đó tới cuối ngày” holds for a drawn box that nothing replaces [D4.1 p2 @02:57–03:08]. The 08:30 fresh-pair timing itself is not stated in the course.',
          consumer:
            'kernel/mentor/course_exits.go boxBuildBars / freshRTHReplaces ← kernel/mentor/box.go latchBoxes + seed.go seedBoxLatch',
          range: 'ON / OFF',
          systemDefault: 'ON',
          recommended:
            'ON — Owner ruling 2026-10-25 (m429): new box if one forms, otherwise the old one.',
          whenToTouch:
            'Switch OFF only to get one box per role per 17:00 CT session again.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Max box height',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'A built FTGH/FTGL taller than this many points is not a box — never latched, never a location or a ban (counted box_too_tall, noted once per box). 0 = off. 10-14 RTH had an 88.75-pt FTGL.',
          trader:
            'No box height is stated in the course or the box extras → case (b), off.',
          consumer:
            'kernel/mentor/course_exits.go filterTallBoxes ← buildBoxes ← kernel/mentor/eval.go',
          range: 'pts, 0 = off',
          systemDefault: '0 (off)',
          recommended:
            'off — Owner setting: open decision; no height is stated, not from the course (case b).',
          whenToTouch: 'If very tall boxes keep banning entries.',
          perSession: 'No — per strategy.',
        },
      ]),
    },
  ],
}
