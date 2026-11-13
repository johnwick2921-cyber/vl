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
  '4h/1h gate only in the news + open windows (ON = the default since oct round 2; OFF = all session, release #59)':
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
  // m465 P1 (release #60): early 15m read, standing-box mode C, trigger escalation.
  'Early 15m read (two 5m candles)': {
    settingId: 'risk_control.mentor_tuning.15m_early_read',
    live: true,
  },
  'Mode C reads the standing 15m + 5m boxes': {
    settingId: 'risk_control.mentor_tuning.mtf_confluence_source',
    live: true,
  },
  'Trigger escalation 5m → 15m → 30m': {
    settingId: 'risk_control.mentor_tuning.trigger_escalation',
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
  'Box trade: a trendline in the path is a partial, not the room cap': {
    settingId: 'risk_control.mentor_tuning.box_trendline_partial_not_room',
    live: true,
  },
  'PHL/PLH inside the 5m ISB box: same direction as a scalp': {
    settingId: 'risk_control.mentor_tuning.phl_5m_box_same_dir_allowed',
    live: true,
  },
  'HTF-opposed range halt (rest of the day)': {
    settingId: 'risk_control.mentor_tuning.htf_opposed_range_halt_pts',
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
  'Rolling PHL/PLH: 1m old-extreme swing width': {
    settingId: 'risk_control.mentor_tuning.phl_old_extreme_1m_k',
    live: true,
  },
  'PHL entry buffer': { settingId: 'Config.PHLEntryBufferPts', live: false },
  'Wick microscalp': {
    settingId: 'risk_control.mentor_tuning.wick_microscalp',
    live: true,
  },
  'Wick safe play': {
    settingId: 'risk_control.mentor_tuning.wick_safe_play',
    live: true,
  },
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
  'Near-box room for PHL/PLH': {
    settingId: 'risk_control.mentor_tuning.near_box_room_phl_multiple',
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
  // m465 P3 (release #60) structure-fidelity knobs.
  'Trendline entry touch': {
    settingId: 'risk_control.mentor_tuning.trendline_entry_touch',
    live: true,
  },
  'Near-box: between two boxes exempt': {
    settingId: 'risk_control.mentor_tuning.near_box_between_exempt',
    live: true,
  },
  'PHL flat low': {
    settingId: 'risk_control.mentor_tuning.phl_flat_low',
    live: true,
  },
  'Reverse ISB: EMA 9 touch': {
    settingId: 'risk_control.mentor_tuning.isb_reverse_touch',
    live: true,
  },
  'PHL/PLH room multiple': {
    settingId: 'risk_control.mentor_tuning.phl_room_multiple',
    live: true,
  },
  'ISB first target is the fill candle close': {
    settingId: 'risk_control.mentor_tuning.isb_target_fill_close',
    live: true,
  },
  'ISB with no level beyond is traded (fill candle close)': {
    settingId: 'risk_control.mentor_tuning.isb_no_level_fill_close',
    live: true,
  },
  'Trigger lines are read from the 08:30 open': {
    settingId: 'risk_control.mentor_tuning.trigger_from_open',
    live: true,
  },
  'Rolling ladder ends on a close through the left extreme': {
    settingId: 'risk_control.mentor_tuning.rolling_left_break_cancel',
    live: true,
  },
  'Rolling ladder: a touch of an earlier leg does not own the entry': {
    settingId: 'risk_control.mentor_tuning.rolling_stale_touch_releases',
    live: true,
  },
  'Rolling ladder: no keypair sit-out on the tape old extreme': {
    settingId: 'risk_control.mentor_tuning.rolling_keypair_tape_exempt',
    live: true,
  },
  'HTF trendline: broken line flips, never deleted': {
    settingId: 'risk_control.mentor_tuning.htf_trendline_flip',
    live: true,
  },
  'Trendline: maximum steepness': {
    settingId: 'risk_control.mentor_tuning.trendline_max_slope_pts_per_bar',
    live: true,
  },
  'Trendline: minimum bounce': {
    settingId: 'risk_control.mentor_tuning.trendline_min_bounce_pts',
    live: true,
  },
  'Knock Knock counts per zone, pre-market included': {
    settingId: 'risk_control.mentor_tuning.knock_knock_zone',
    live: true,
  },
  'Trendline: first-bottom anchor': {
    settingId: 'risk_control.mentor_tuning.trendline_first_bottom_anchor',
    live: true,
  },
  'Box flip is reversible': {
    settingId: 'risk_control.mentor_tuning.box_flip_reversible',
    live: true,
  },
  'Carried box stays live': {
    settingId: 'risk_control.mentor_tuning.box_carried_stays_live',
    live: true,
  },
  'Box: a big outside bar is both a peak and a bottom': {
    settingId: 'risk_control.mentor_tuning.box_swing_both_roles',
    live: true,
  },
  'Box pair: pre-open peaks count': {
    settingId: 'risk_control.mentor_tuning.box_preopen_lookback_min',
    live: true,
  },
  'PHL/PLH minimum distance from the old low': {
    settingId: 'risk_control.mentor_tuning.phl_min_above_old_extreme_pts',
    live: true,
  },
  'Key level: delete on a 1H body close': {
    settingId: 'risk_control.mentor_tuning.key_level_delete_on_1h_close',
    live: true,
  },
  'Key level: break and retest': {
    settingId: 'risk_control.mentor_tuning.key_level_break_retest',
    live: true,
  },
  'Key level: re-arm after the run': {
    settingId: 'risk_control.mentor_tuning.key_level_rearm',
    live: true,
  },
  'Reverse ISB: trend from the EMA lines': {
    settingId: 'risk_control.mentor_tuning.isb_reverse_trend',
    live: true,
  },
  'Counter-5m scalp: reduced size': {
    settingId: 'risk_control.mentor_tuning.counter_5m_reduce_size',
    live: true,
  },
  'Key level: history from the current contract': {
    settingId: 'risk_control.mentor_tuning.key_level_history_source',
    live: true,
  },
  'Key level: Uno Reverse after a hold': {
    settingId: 'risk_control.mentor_tuning.uno_key_levels',
    live: true,
  },
  'Session-open wick + gap edge levels': {
    settingId: 'risk_control.mentor_tuning.session_open_levels',
    live: true,
  },
  'Line order cancel: current line price': {
    settingId: 'risk_control.mentor_tuning.level_arm_line_cancel',
    live: true,
  },
  'PHL higher low: whole pullback': {
    settingId: 'risk_control.mentor_tuning.phl_pullback_low',
    live: true,
  },
  // m466 P1 (release #61) boxes, breaks, ranges.
  '5m ISB box: merged inside bar': {
    settingId: 'risk_control.mentor_tuning.mentor_5m_box_source',
    live: true,
  },
  'Break direction lock': {
    settingId: 'risk_control.mentor_tuning.mentor_break_direction_lock',
    live: true,
  },
  'Too-small range guard': {
    settingId: 'risk_control.mentor_tuning.mentor_small_range_guard',
    live: true,
  },
  'Mid-range: trade with the rejection': {
    settingId: 'risk_control.mentor_tuning.mentor_mid_range_reject_side',
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
  'Resonance on any same-way inside bar': {
    settingId: 'mentor_resonance_raw_isb',
    live: true,
  },
  'Smaller size on a 2m entry': {
    settingId: 'mentor_2m_reduce_size',
    live: true,
  },
  'Break-even before the 1:1 partial': {
    settingId: 'mentor_be_before_partial',
    live: true,
  },
  'Break-even offset (ticks)': {
    settingId: 'risk_control.mentor_tuning.be_offset_ticks',
    live: true,
  },
  'Swing: one filled inside-bar trade per 4h candle': {
    settingId: 'risk_control.mentor_tuning.mentor_swing_isb_one_per_candle',
    live: true,
  },
  'Crossing inside bars: lookback': {
    settingId: 'risk_control.mentor_tuning.crossing_isb_lookback',
    live: true,
  },
  'Crossing inside bars: minimum count': {
    settingId: 'risk_control.mentor_tuning.crossing_isb_min',
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
  'Stop-size ladder': { settingId: 'mentor_stop_size_ladder', live: true },
  'Wide-stop contracts': {
    settingId: 'mentor_wide_stop_contracts',
    live: true,
  },
  'Ladder max stop': { settingId: 'mentor_wide_stop_max_pts', live: true },
  'Key-level pair candle ratio': {
    settingId: 'mentor_keypair_candle_ratio',
    live: true,
  },
  'Fresh RTH box pair': { settingId: 'mentor_box_fresh_rth_pair', live: true },
  'Max box height': { settingId: 'mentor_box_max_height_pts', live: true },
  'Swing skip stop (đừng vô)': {
    settingId: 'mentor_swing_max_stop_pts',
    live: true,
  },
  'Candle-2 entry: the stop covers the longer wick': {
    settingId: 'risk_control.mentor_tuning.touch_stop_cover_later_wick',
    live: true,
  },
  'ISB loss box': { settingId: 'mentor_isb_loss_box', live: true },
  'Opening 5m range break': { settingId: 'mentor_open5m_break', live: true },
  'News wait: which lines': { settingId: 'mentor_news_wait_mode', live: true },
  'News: read the 1h/4h lines before the print': {
    settingId: 'mentor_news_read_lead_min',
    live: true,
  },
  'Swing inside bar — close back': {
    settingId: 'mentor_swing_isb_close_back',
    live: true,
  },
  'Swing inside-bar order kept on a close through': {
    settingId: 'mentor_swing_isb_keep',
    live: true,
  },
  'Swing runner take-profit': {
    settingId: 'mentor_swing_runner_tp',
    live: true,
  },
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
          body: 'Every mentor entry goes out as a stop-LIMIT with the limit at the stop price ("đừng đặt buy stop market nữa… đặt buy stop limit đi") — always, no setting turns it off. Each order carries its own expiry: an ISB expires at the close of the next candle of the frame it was read on (m406), and while each following candle\u2019s BODY stays inside I1 the evaluator keeps it to the next close (stack candles 2 and 3; a body outside I1 or a 4th candle cancels it) — the keep is decided BEFORE the expiry sweep runs on that candle (until release #22 the sweep ran first and cancelled 16 kept ISB orders); the swing at the close of the current 4h candle (the order is dropped only AFTER the 4h candle\u2019s last 5m bar has been checked for a fill, because NinjaTrader keeps the order live until that instant: a fill on that last bar is a real position and gets its break-even at +1R and its one-4h-candle hold-close, release #53 m437); a LEVEL (PHL/PLH/EMA) order is stamped to rest until the next 15:00 CT — the RTH window end, an inference, no lesson states it — or until a later candle closes through the level, whichever comes first. The close-through is read on the frame the order was placed on (a closed 2m/5m candle for a 2m/5m order, never a 1m wick inside a still-forming candle) and it is a cancel and nothing else: the touch it was placed off is spent, so the same level is not re-armed off the same candle on the next tick — only a NEW touch candle can trade the level again [Ngày 2.3 part1 @21:24, @23:48–24:02]. The armed pass cancels an unfilled order when it lapses. ON TOP of every expiry, the EOD flat cancels every INTRADAY mentor order (on an ordinary day everything except the SWING4H) whenever no session window is open: from the NY close at 14:45 CT until the 17:00 CT reopen. The same pass then closes every open INTRADAY position (at market by default) — an entry that already filled does not ride past the flat either; on an ordinary day only the SWING4H position is exempt (see "EOD flat" in the exits). On a shortened day (12:15 CT close on 11-27 and 12-24) the flat is 5 minutes before the early close (12:10 CT) and the SWING4H is NOT exempt: its order is cancelled and its position closed with the rest. The ASIA/LONDON/NY windows count for this whether or not their trading switch is on, and a registered half-day pulls the 14:45 earlier. So in practice a level order never lives past 14:45 CT: one placed in the morning is cancelled at 14:45 the same day, 15 minutes before its own 15:00 stamp; one authored between 14:45 and 17:00 is cancelled on the same pass, within milliseconds (rows 256, 282, 283 and 343 died 1–3 ms after they were written); one placed in the evening or overnight rests until 14:45 CT the next trading day. No NEW mentor entry is placed from 15:45 CT (the lead into the 16:00 daily halt) until the 17:00 reopen — on a shortened day from 15 minutes before the early close (12:00 CT on a 12:15 day) until the next reopen. A mentor entry without an expiry is refused, never sent as a stop-market. Other (non-mentor) orders keep their own order types.',
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
          body: 'The planner page and the trader dashboard show a card titled "Mentor — what trades". It is read-only and reports what the mentor evaluator holds RIGHT NOW: the 4h trigger direction and since-when, the 1h trigger, the 5m trigger (price + since), the HTF verdict (follow / sit-out / no-trigger) and whether the HTF gate is active (htf_gate_news_only), the mentor levels in effect (price, a readable kind name, drawn-at, visits today — the 4h trigger line and the D4.3 5m-wick targets (wick_microscalp) are tagged "target only": they join the level set for the target ladder but the touch loop never trades them; a D4.3 wick zone (wick_zone, knob wick_microscalp) is listed by its two edges and drawn on the chart as one rectangle; the 1h trigger line is not a target and is not listed (release #52 B5)), the history depth line, and the next-window / day-stop state (done-after-win, stop-after-loss, window — a -1 window reads "no time window" and still names the 15:45 CT entry cutoff and the 14:45 CT EOD flat; it never means any hour). It never mutates the evaluator — it reads under the existing locks and never blocks the tick.',
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
          title:
            'Levels and touches (release #61): the open wick, the gap edge, the Uno flip, the line cancel, the whole pullback',
          body: 'Five course rules from the full video read, each ON by default with a knob back to the pre-#61 behaviour (Mentor method numbers). (1) The session OPEN candle’s wick is an old high/low: the first 5m candle of the CME session (17:00–17:05 CT) — "cái râu này nó sẽ là cái đỉnh" (29,136.00, Sun 13 Sep) — joins the old-high/low candidates with no left-neighbour test; the usual unbroken test and nearest pick decide (knob session_open_levels). (2) The GAP edge is a level: when the open candle left empty space of 20 pts or more to the prior session’s last close, that last close (Friday 29,391.25 in the lesson) is drawn as a "gap edge" — a location, a target and an obstacle — until a 1m candle trades through it; the bot never trades the gap fill itself ("em không đánh theo gap") (same knob). (3) Uno Reverse: a key level that HELD today and then breaks flips its role once — the backtest from the other side is a fresh first touch and its reject arms a PHL/PLH (counter level_broken_uno); a first touch that closes through, or a second break after the flip, still makes it ISB-only for the day (knob uno_key_levels). (4) A resting order on a trendline or the EMA 34 is cancelled by a close through the line where it is NOW, not where it was when the order was placed, and a trendline that died cancels its order (level_arm_cancel_current_line / level_arm_line_gone; knob level_arm_line_cancel). (5) A PHL needs a higher low for the WHOLE pullback: a pullback candle that already went at or under the prior low refuses it (phl_pullback_lower_low, mirror plh_pullback_higher_high; knob phl_pullback_low).',
          cite: 'G10 D3.1-R26 [D3.1 p1 @11:32–12:22] · G11 D3.1-R20/R16/R23 [D3.1 p1 @07:48–09:51] · G2 D1.3-R21 [D1.3 p1 @12:31–12:58] vs D5.2-R22 [D5.2 p1 @19:44–20:25] · G17 D2.3-R34 [D2.3 p1 @23:35–24:03] · G41 D2.2-R59 [D2.2 p3 @08:25–09:25] · kernel/mentor/session_open_levels.go · kernel/mentor/uno_key_level.go · kernel/mentor/level_arm_line.go · kernel/mentor/phl.go phlPullbackRefusal',
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
          body: 'Candle 2’s BODY inside candle 1’s FULL range (wicks included). Direction = candle 1’s colour (“nó chỉ có 2 màu thôi, nó không có màu thứ 3” [D1.4 p1 @09:20–09:38]); a DOJI candle 1 has no colour, so the bot REFUSES it — the course is silent on a doji, this fail-closed refusal is OURS (CTO ruling 2026-10-03; the old doji-long was audited wrong) and awaits an owner ruling. Entry at the ISB candle’s own extreme + 1.5 pt (6 ticks) — exactly that on the wire, nothing is added at placement — stop 1.5 beyond its opposite extreme, no fixed stop size — skip a stop above 20 pts (release #61, D4.1-R12 “inside bar mà nó hai mươi mấy điểm, làm ơn giúp em đừng vô” [D4.1 p1 @05:41–06:08]; the D3.3 “20–25 reduce size” option no longer applies to an ISB). The FIRST inside-bar candle is I1 — the order sits at its extremes and the containment test is against I1. A following candle that does not fill and whose BODY is NOT inside I1 → cancel at once. Body inside I1 → keep. I1 counts as candle 1 of the stack; candles 2 and 3 inside → keep; the 4th not filling → cancel (it becomes a 5m inside bar = rest). The course counts 1m candles; on the 2m the same 4 is OURS (knob isb_stack_cancel_candle_2m), and the September tiny-stop exception (isb_stack_tiny_stop_pts, OFF by default since oct round 2 G2 — October cancels at the 4th candle, OctD4.3-R57 [@56:00]; 12.5 = the worked example’s measured stop, D5.1-R26 [D5.1 p2 @02:24–04:41]) keeps an arm whose stop is at or under that many points past the cancel candle.',
          tag: 'D1.4 p1',
        },
        {
          title: 'PHL / PLH — the pullback',
          body: 'Uptrend pullback → buy stop 1 point over the previous candle’s high, stop 1 point under that candle’s low (2 points of buffer in total, [Oct D2.3 @ 46:20-47:20]; Sept drew 0.50 rays, D2.2-R10), target NEAR the old high (5 pts short of it — the 5 is ours, knob phl_target_shy_pts). Downtrend bounce → the exact mirror. The pullback stop can ROLL: with NO drawn level touched, the buy/sell stop is re-armed to EACH closed pullback candle’s extreme until it fills — "Mình ra một lệnh buy stop trên đỉnh của từng cây nến đã đóng xong" [D2.2 p1 @02:44–03:34, 11:05–11:36]. This rolling entry is ON by default (release #52) and works as a LADDER (release #59, m464): the ladder STARTS only when the pullback SHOWS — a RED candle in an uptrend, a GREEN candle in a downtrend [D2.2 p1 @01:52–01:57, @10:47–10:51] — and once it runs EVERY closed candle is the next rung, whatever its colour: the mentor’s own last rung is the small GREEN candle of an uptrend pullback (he draws the buy stop 29,396.25 over its high 29,395.75 and the stop 29,386.00 under its low 29,386.50; since the release #59 fix round the bot’s rung is exactly that — entry 0.50 over the high, stop 0.50 under the low, the drawn rays) [D2.2-R7 p1 @02:26–03:27; D2.2-R9/R10 p1 @04:14–05:43]. On the REAL candles of the lesson’s replay day (Fri 11 Sep 2026) the bot computes the mentor’s rungs to the tick, but at 10:14 it REFUSES the last one (rolling_phl_15m_box_ban): the 10:00 15m candle closes at that very instant as an inside bar of the RED 09:45 one — a SHORT 15m ISB box (29,433.00 / 29,360.00) around a LONG rung — and from Day 4 on the course bans every PHL/PLH inside a standing 15m ISB box and every lower-timeframe trade against a 15m ISB until price leaves it (“đừng trade. Đợi nó thoát cả luôn” [D4.2-R8/R10 p1 @04:01–04:03, @05:34–05:37]); with the tape’s own 5m trigger (it flips short at 29,410 on the pullback) the same rung is also refused rolling_room at phl_room_multiple=2 (the September 2R room; the default is 1 since answers-1112). A Day-2 example against Day-4 rules: the bot follows the later rules, every skipped rung is named and counted, and the conflict is an open question for the mentor (release #59 review). Each rung is checked on its own: a rung refused by the 25-pt stop ceiling, the room rule, the 1:1 floor, a higher low that broke, a location ban or the 5m trigger line (like every entry it must agree with the 5m trigger [X8 @06:25, @08:37–08:48]) is SKIPPED with its name counted (rolling_phl_stop_ceiling, rolling_room, rolling_phl_trigger_side, …), the older resting stop is cancelled because the ladder has moved past it, and the ladder goes on to the next closed candle — a refused rung never ends the ladder (before #59 it did, so the 10:13 candle of the lesson’s worked buy, a 29-pt stop, killed the ladder and the mentor’s 10:14 entry was never placed). The ladder ENDS when a candle breaks the newest rung — the fill when it was armed; when that rung had been refused it is the reaction we have no order for, counted rolling_ladder_reacted_no_order, and the bot does not chase it — and when the 4h side flips, no old extreme is left to target, or a level touch owns the entry. The breaking candle never starts a new ladder; the next pullback candle can. It follows the 4h direction (inside the 08:30–09:30 CT open window and the news period the 4h/1h gate also applies, so a 1h trigger opposite the 4h sits the roll out; outside them the 4h alone is the trend — the roll has no clock window of its own, and a resting roll is not cancelled when the open window closes), and it needs a nearest old high/low, the 1–2 candle wait and a higher low. It obeys the same location bans as every PHL/PLH: inside a standing 5m/15m/30m ISB box, between an FTGL and an FTGH, mid-range between two levels, next to a box (the nearest box edge in the trade direction closer than 2 x the candle’s range) or when the candle is as big as the rank to the next key level, that rung is refused (counted as rolling_<name>, e.g. rolling_phl_5m_box_ban) and a resting stop is cancelled — the next candle’s rung is read again [D3.4 p2 @07:27–10:35; D3.2 p2 @08:34–08:55, @21:53–23:08; D2.2 p3 @14:03–16:03; X4 @06:31]. A level that has left the level set no longer "owns" the entry, so a stale touch can never switch the roll off for the rest of the day. A candle TOUCHING A DRAWN LEVEL is a different entry: the FIRST touching candle is the reference and never rolls [D2.3 p1 @17:55–19:12; D5.3 p1 @22:10]. Wait 1–2 more pullback candles; a higher low is required (a flat low is an FTGL, not a PHL — since release #60 (D2.2-R64, "nó ngang phè… failure to go lower" [D2.2 p3 @07:44–10:15]) a low within the day’s "same price" distance of the prior low (half the average candle range of the last 20 closed candles, the owner’s m428 R2 measure) is refused phl_flat_low_is_ftgl, mirror plh_flat_high_is_ftgh; knob phl_flat_low). The old high is the nearest UNBROKEN old high, and its role is fixed when it is drawn: a PHL (long) only ever targets an old HIGH and a PLH (short) an old LOW — an old low is never a long’s target. For the ROLLING ladder (phl audit PHL-1) the old high/low is the 1-minute swing the pullback started from — the swing made minutes before it, as in every worked example of Oct D2.3 [OctD2.3-R30 @ 26:50, R42 @ 41:23] — read once 4 candles stand to its right (knob phl_old_extreme_1m_k); until then the rung is refused rolling_phl_old_extreme_unconfirmed, and a pullback whose old high/low is already within reach has no target and no setup [OctD2.3-R53 @ 54:00]. The 5m/15m swing levels stay the runner’s next target. A rolling candle dropped because no old extreme is left, or because a level touch owns the entry, is counted (rolling_phl_no_old_extreme_on_side, rolling_level_touch_owns). Inside a standing 5m (or 15m/30m) ISB box, or between an FTGL and an FTGH, there is no PHL/PLH at all (only the ISB) [D3.4 p2 @07:27–10:35; D3.2 p2 @08:51].',
          tag: 'D2.2 p1',
        },
        {
          title: 'SWING4H — 4h EMA 34',
          body: 'The line is the 4h EMA 34 known at the start of the current 4h bar (17:00 CT anchor), drawn BEFORE price gets there. If a 4h bucket that the CME calendar says was OPEN has no bars in the stored 1m tape (a data hole, not a weekend or a halt), the line is missing one fold and is wrong by a few tenths of a point (about 0.4 pt during the 10-28 swing trades): the bot marks the line STALE and refuses new SWING4H entries (counted swing_line_gap_stale, named on the boot line) until the hole is refilled and the line re-seeded; a hole older than about 68 folded 4h closes has decayed to nothing and is ignored (release #56, m456). A contract roll is not a hole: a minute that any stitched contract stores counts as present, and the shortened 4h buckets on the two DST-change days are read as the line folds them (release #56, m456). Release #59 (m463, owner order 2026-11-01 “full of it”) makes the swing exactly D5.2: (1) Switch straight down to the 5m; the touch must be LITERAL — the 5m candle’s range must reach the line itself, a wick into it counts (“bắt buộc phải đụng, không được gần đụng”; “đụng ít đụng nhiều gì vẫn phải đụng” [D5.2 p2 @09:12–09:27, @13:56–14:05]). The old 5-pt “line offset toward the approach” is gone — it armed trade 707 off a candle 1.28 pts short of the line; a near miss is now only counted (swing_near_touch). (2) Only the FIRST touching candle counts, with 2 candles of leeway (“lúc nào cũng phải lấy cây nến đầu tiên. Đừng bao giờ lấy cây nến thứ 2”; “em sẽ du di là 2 cây nến” [D5.2 p1 @13:23–13:40, @15:37–15:51]). Reject (closes back on the approach side) → stop order tight on the candle, stop 30 beyond the line. After those two candles there is NO further plain reject in that 4h candle however far price travels in between (swing_not_first_touch) — the old “re-arm once price left the line by the stop distance” rule is removed (it re-armed trades 709/710). (3) Any 5m CLOSE beyond the line — the touching candle, a later candle, or a gap over the line — makes the level INVALID for the rest of that 4h candle (“level đó invalid, không chạm vô nữa, không đụng vô nữa” [D5.2 p3 @19:15–21:56]): any resting order is cancelled and every later reject-shaped touch is refused (swing_level_invalid). The ONLY way back in: a candle closes back across the line and, ONE 5m beat later, an inside bar of that close-back candle (its body inside candle 1’s full range) also closes back → 5m inside bar on the 4H line’s side with the stop AT the line, no time limit [D5.2 p1 @20:31–21:57 · p2 @10:23–10:51]. An inside-bar order that is CANCELLED unfilled (a later 5m close back through the line) does not use the level up — the next inside bar of that 4h candle may enter (“xuất hiện inside bar, tôi vô lệnh”, D5.2-R25; release #59 review: on 10-28 the 18:25 inside bar was cancelled at 18:40 and the 19:00 inside bar was then refused with no note at all); an inside-bar order DROPPED before it was ever placed (inside a box — inside_any_box, a stale line, the 5m trigger zone, or a trader refusal) does not use it up either (isbTF audit: on 10-28 the 17:50 inside bar was dropped inside a box and the next 17 inside bars of that 4h candle were refused \u201calready filled\u201d although nothing had filled); once an inside-bar order has FILLED, later inside bars of that 4h candle are refused, named swing_isb_spent. One setup per level: if the reject order already FILLED and price then closes beyond the line (the trade going against you, short of its stop), the level is invalid but the open swing stays the level’s trade — the inside bar is refused (swing_position_open) and never arms a second swing beside it [D5.2-R19/R23 p1 @13:23–13:40, p3 @19:15–21:56] (release #59 fix round; before it a second arm could fill and the bot lost the first leg’s break-even/hold tracking). Candle 1’s colour does NOT matter (“không quan trọng màu” [D5.2 p3 @06:24–06:52]) — the colour rule borrowed from the 1m ISB is removed, and a doji candle 1 is fine for the swing. Release #61 (m466 P4): by default the close back is NOT required — the inside bar ALONE is the setup, even while price still closes beyond the line (“chị đợi đóng ngược lại cũng được, không thành vấn đề, nhưng chị đợi hơi lâu” [D5.2 p3 @04:49–05:09]); its stop order must sit on the trade side of the line (else named swing_isb_entry_beyond_line, not spent), stop AT the line; mentor_swing_isb_close_back=required restores the close-back rule. And a resting INSIDE-BAR order is no longer cancelled by a later close through the line — only the reject order is (“Để cái cây inside bar này cho em” [D5.2 p2 @10:23–10:51]; mentor_swing_isb_keep, default ON); it rests until it fills or the 4h candle closes. The split swing’s runner has no take-profit by default (mentor_swing_runner_tp=none — held by the 4h [D5.2 p1 @05:40–06:14]). (4) A stop of ~100 pts → DO NOT ENTER (“100 điểm quá nhiều rủi ro” — his own example is 91.75 pts; 30–40 or 50–60 are fine with 2 contracts [D5.2 p2 @19:34–21:43]): a swing whose stop is ABOVE mentor_swing_max_stop_pts (default 80 — “có những lúc mà cái nến 5 phút nó tới 7-80 điểm… cứ vô 1-2 [MNQ] cũng được… risk reward phải 1-1” [Sept D5.2 p2 @05:25–05:33]; 90 before the answers-1112 build, set 90 to revert) is refused, on the reject and the inside-bar entry alike (swing_max_stop at the evaluator, R31 at the trader’s rule gate — one number). This REVERSES the earlier owner ruling m186 (“take 1–2 MNQ and sit”), on the owner’s order of 2026-11-01 after being shown the quote. A tall reject candle is what makes a reject stop wide: the entry is the candle’s extreme, the stop 30 beyond the line (trades 709/710: 102.25 pts). The B5 sizing cap (swing_max_stop_pts, default 100 → at most 2 MNQ) only bites if you raise the skip above it. (5) When the 4h candle closes the instance is over (“9h đó là xong rồi… mình không có lệnh nào hết” [D5.2 p3 @12:16–13:18]): the line re-bases, the first-touch window resets, and an unfilled order expires at the candle’s close. The candle’s LAST 5m bar is only readable at that close: it is read against ITS OWN candle’s line (not the next one), and a setup on it arms nothing — the instance is over (named swing_candle_closed; release #59 review — before it, that bar was read against the NEXT candle’s line and could arm a swing off a line it never touched). Bars of 4h candles that closed before the bot could read them (a feed gap, or the first minutes after a cold start) are never walked for entries either (named once) — at the MARKET close when the CME shuts inside the candle (the 13:00 CT candle ends at the 16:00 halt, a Friday at the weekend close, an early-close holiday at its close), and the trader cancels a due order on the closed path, so nothing rests into the 17:00 reopen (trade 678 filled 41 s after the reopen off an expiry left inside the break). (6) The swing follows the 4H EMA 34, not the 5m trend: price below the 4h EMA = resistance → short at the touch, above it = support → long (“4 giờ đụng resistance thì mình đánh xuống chứ không lẽ đánh lên?” [D5.2 p3 @18:07–18:25]); no 5m-trend filter. The side comes from where price is against the line only — the EMA’s slope is not read (the course only shows price below a falling EMA and above a rising one; a counter-slope case is an open question for the mentor). Late in the day — an entry inside the LAST 4h bucket (13:00–16:00 CT normal; the shortened close’s bucket on early-close days) — the swing target is cut to at most 30 pts, but never below 1:1 — with a wider stop the target stays at 1:1 [derived: D5.2 p2 @07:00–07:58 “market sắp đóng cửa” → the last 4h bucket; 30 = top of 20–30 @07:21; 1:1 floor 03-CHECKLIST.md:160]. The line folds EVERY closed 4h candle — after a feed gap of 4h or more the candles that opened and closed unseen are folded from the bars on the next tick ("swing 4h EMA34 line folded N 4h closes after a feed gap"). A gap longer than the live slice (~25 h) hides closes the bot cannot read: the line is marked STALE and SWING4H entries are refused (counted swing_line_gap_stale) until a restart — or a history refill (see the re-seed below) — reseeds it from the store; never a guessed line. Every swing refusal is logged as a note and counted in the mentor funnel.',
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
          'FTGH/FTGL boxes, drawn on the frame the bot TRADES — 1m regular candles from the 08:30 CT open to 09:00, closed 2m candles from 09:00 on while exec_2m_after_30m is ON (all-1m while it is OFF) [D4.1 p1 @22:57–23:21: "you trade the 2-minute, you draw the 2-minute"]; a box keeps the frame it was drawn on. A box entry trails, breaks even and runs the X8 exit on the frame the box was DRAWN on (m417). Paired by role from two CONFIRMED swings: two swings that are LEVEL (within the day\u2019s own flat-near measure, in either order) form the box and it anchors on PEAK 1, the EARLIER one, even when the later wick is higher ("Nếu đỉnh 2 có râu cao hơn ... mình sẽ bay theo đỉnh 1" [Oct D3.3 @ 19:31]; FTGL is the mirror [Oct D3.3 @ 43:14]; m473 B2 — it replaces the September extreme-plus-next-swing pairing [D3.2 p1 @01:21–04:25]). The anchor swing is the day\u2019s most extreme confirmed one (highest high / lowest low; if it has no level partner the next most extreme is tried) and its partner is the FIRST confirmed swing level with it, on either side (phl audit PHL-2: \u201cTui lấy cái râu đỉnh 1 làm chuẩn\u201d [Oct D3.3 @ 19:31], \u201cFirst Touch is the best\u201d [Oct D3.3 @ 20:33]; before the audit it was the level swing nearest in time); the box anchors on the earlier of the two, and peak 2\u2019s wick is never included [Oct D3.3 @ 17:37]. An early small level pair still never beats the prominent peaks (the extreme is tried first). The NEW zone after an escape is built the same way from the swings after the escape, anchored on the new extreme only (10-30 E10: 09:25 bottom 28,614.75 + 10:42 bottom, box 28,614.75\u201328,626.50 at 10:43 \u2014 the mentor drew 28,615.00\u201328,625.25; before the audit that frame paired the extreme with the nearest later non-exceeding swing whatever the gap, e.g. the 11-10 07:52 V reversal 27,598.50\u201327,691.00); the body-edge partner rule is not built (the mentor has not given its numbers). The trade is the 3rd touch',
          'A box edge is used again and again; an escape (a 1m BODY closes outside — always the 1m, also for a 2m-drawn box) does NOT kill the box — it stays and every later return trades [B4] until a newer box of the same role forms: one floor and one ceiling are live at a time [D3.2 p2 @09:14]. A box standing at the RTH open — formed in RTH, or already confirmed before 08:30 from the pre-open peaks (about 08:00) — is VALID until the NEXT RTH open, 08:30 CT, about 23–24 hours ("valid cho tới khi market open ngày mai ... 8 giờ rưỡi sáng" [Oct D3.3 @ 40:40–41:20]; m473 B1) — it is carried past the 17:00 CT session roll (log: "carried past the 17:00 CT roll") and expires at the next 08:30 (log: "carried box ... expired"); a restart rebuilds the carried box too (the seed replays the previous trading day\u2019s latch, so a Friday box is still on the chart after a Sunday restart); a box formed in the evening (17:00 CT or later) is valid to the FIRST open after it formed (Thu 18:00 to Fri 08:30; Sun 18:00 to Mon 08:30 — the video says tomorrow\u2019s open, 23\u201324 h, not 38\u201386 h), the newest box per role still wins and a carried box only fills a role that has no box of its own; a carried box whose formation candle has scrolled out of the bar window is dropped, never read through a stale index. The first FTGH and the first FTGL of the trading day are latched until the 17:00 CT session roll — never redrawn on a later higher high / lower low; the one exception (owner 2026-10-25, m429): a fresh box that forms in RTH (after 08:30 CT) REPLACES the evening box of the same role, and a role with no fresh RTH box keeps its evening box [D4.1 p2 @02:39, @02:58]; the log says "box latched …" and "box latch cleared at the 17:00 CT session roll". A restart REPLAYS the latch from the 17:00 roll minute by minute, so it lands on the boxes the running bot drew (boot line "mentor seed: box latch replayed from the 17:00 CT roll … FTGH … · FTGL …") and judges only the returns that come AFTER the restart — the old ones are history. A latched box stands on EVERY tick, also when that minute\u2019s fresh scan would not draw its role (release #23: it used to vanish on those ticks — 50 on 10-14)',
        ],
        [
          'Trendline',
          'Two CONFIRMED two-sided structural swings (left fractal + the next bar confirms it), at least 5 bars apart (a floor that is OURS, not the course — the mentor only says a trendline must slope [D2.3 p1 @06:26–07:00]; an open question to him; a 1H/4H line needs 2 HTF bars; bars of the frame the line is drawn on: the 1m in the first 30 minutes after the 08:30 CT open, the closed 2m candles after that — the mentor draws his trendlines on the 2m, "Thường em sẽ vẽ trendline khung 2 phút" [X9 @03:35–03:49], and trades the 1m only until 09:00 [Routine trước khi trade @00:00:34–00:01:14]; exec_2m_after_30m OFF = the 1m all day; the break below still reads closed 5m candles), joined — a low and a HIGHER low (support) or a high and a LOWER high (resistance); never horizontal: two same-role swing wicks at (nearly) the SAME price are a flat SUPPORT (two lows) or RESISTANCE (two highs) level — drawn and traded as an old low / old high, labelled "support" / "resistance" — never a trendline, and "near" is the day\u2019s own measure, HALF the average high-low range of the last 20 closed candles of the traded frame (a quiet day ~4–5 pts, a run day 10–20 pts; owner ruling 2026-10-25, Q2). EVERY consecutive pair of today\u2019s qualifying swings draws a line, plus at most ONE non-adjacent line per side at a time (the most significant extreme joined to the later swing, angled and uncrossed; a better or continuing one replaces it), so a side never fans. An UNBROKEN drawn line stays all session — it is DELETED (not drawn, not a level) the moment a closed 5m candle is WHOLLY beyond it, and a 1H/4H trendline breaks the same way, on a 5m candle wholly beyond it, not on the 1H close (owner rulings R1/R3, 2026-10-25) — what counts as wholly beyond: (a support line: the whole candle, wicks included, below the line; a resistance line: the whole candle above it; a close beyond the line with a wick still back at it is NOT a break; the line is read at BOTH the candle\u2019s open and its close time and the candle must be beyond both, so on a sloped line a candle that traded on the line\u2019s own side earlier in its five minutes is not a break) — "n\u1ebfn n\u00f3 \u0111\u1ee5ng level m\u00e0 n\u00f3 \u0110\u00d3NG D\u01af\u1edaI level… n\u00f3 G\u00c3Y level" [D2.3 p1 @14:36–15:17] on the 5m [X9 @06:25] — or the session ends (owner ruling R4; "never erase during the session, delete at end of day" [D4.1 p2 @01:17]). A VALID line carries across the 08:30/09:00 frame switch until a 5m candle wholly beyond it breaks it; FORMING lines are redrawn on the new frame (m407). A pair that price crossed between its own two points (a 1m wick beyond the line by more than the 0.5-pt touch band) is not a trendline and is not drawn ("TRENDLINE chứ không phải CẦU TUỘT" [X9 @01:22]) — counted once per day as trendline_crossed_between_anchors. The log says when a line turns VALID, when one is BROKEN and when the day\u2019s lines are deleted',
          'Becomes a location only on the 3rd touch, and that touch must be a RETURN ("ph\u1ea3i V\u1ec0 TEST L\u1ea6N TH\u1ee8 3 th\u00ec m\u1edbi VALID" [D3.2 p2 @03:45]): price first LEAVES the line \u2014 a confirmed swing high above a support line, or a confirmed swing low below a resistance line, beyond the touch band \u2014 and then comes BACK to touch it — a REJECTION: the candle\u2019s low (high) comes within the 0.5-pt band of the line (0.5 is ours, not the course — the mentor says a near-touch is fine on the 1m and a real touch is needed on the 5m, with no number [Ngày 5.2 part2 @00:18:30–00:18:45]) and it closes on the line\u2019s own side; a wick that goes beyond the band, or a close through the line, is not a touch. The bars that confirm the 2nd swing and still sit on the line never count. A closed 5m candle WHOLLY beyond it discards it ("đóng dưới NGUYÊN CÂY" [X9 @06:28–06:45]; a close beyond the line alone is not a break; a break is never a reversal and NOT an entry). Box beats trendline: a trendline whose price sits inside a live box\u2019s [Bottom, Top] is NOT a location. A respected level (a key level, an old high/low or an FTGH/FTGL box edge — the mentor\u2019s own example is the failure-to-go-higher/lower box, c52 W9) within 2 pts of the line (the 2 pts is OURS, not the course: he gives no number) beats a FRESH trendline — but only on its FIRST test, the 3rd touch that just closed; a valid line past its 3rd-touch return has itself been respected and is no longer suppressed by a nearby level ("chọn cái mà nó đã respect nhiều lần thay vì … cái mà nó mới quay về, nó test lần đầu tiên … Đừng chọn cái nó mới tạo" [D2.3 p1 @02:19–03:45]). Release #60 (D2.3-R7, SUPERSEDED by m472 A2: the October course D3.2 says two touches are enough “Em chỉ cần 2 touch là được mà” [Oct D3.2 @ 31:17], so the DEFAULT is now third; the strict 4th-touch read below is opt-in via knob trendline_entry_touch=fourth): the 3rd touch is the TEST, not the entry — "thà nó về một lần, rồi nó test, rồi nó hold rồi, là lần thứ ba đó, thì lần thứ tư mới dám tự tin mình vô" [D2.3 p1 @02:34–02:47, 03:03–03:16] — so on the candle of its 3rd touch a 1m/2m line is NOT a location (refused trendline_third_touch_is_test, counted); from the next candle it is, and the entry comes on a later touch (knob trendline_entry_touch; 1h/4h lines unchanged)',
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
          body: 'The 5m ISB candle is boxed; only a same-direction 1m ISB trades inside it. Since the ISB5 audit (October OctD4.3-R59 "bọc từ head to toe") every 5m candle that closes inside the standing box MERGES into it — the box widens to that candle’s high/low and keeps the first inside bar’s direction (mentor_5m_box_source = merge; newest = the #61–#66 replace-with-newest read); a 1m CLOSE outside the box breaks it and trading is allowed; since m473 the box itself is deleted only when a candle of its own timeframe (5m/15m/30m) closes outside (isb_box_escape = close5m; close = the old 1m delete), and after the break only setups in the break direction trade until a close back inside (mentor_break_direction_lock). A 1m close back inside does not re-arm the box ban (the video states only the exit and the own-timeframe delete); a newer inside-bar pair replaces a broken 15m/30m box at once, and the dashboard marks a broken box "broken long/short". A PHL/PLH inside the box is refused phl_5m_box_ban; with “PHL/PLH inside the 5m ISB box: same direction as a scalp” on (answers-1112 Q9, default off) one on the box’s own side trades as a scalp to the box edge [Oct D4.3 @1:08:24–1:11:45]. The box does NOT replace the trigger line (isbTF audit, October overrides Sept D4.1-23): a 1m ISB inside the box needs BOTH the box direction AND the governing trigger side — above a 5m SELL line no long while the 5m still has a sell trigger ("Tuyệt đối không được … khung 5 phút vẫn đang có sell trigger", OctD4.3-R28), refused isb_trigger_side / isb_trigger_side_mismatch exactly as with no box. Before the audit the standing box hid the trigger, and on the course\u2019s own 11-02 chart the bot went long at 09:35 above the 09:30 sell line.',
          cite: 'D3.4 p2 @09:09–09:39 — ISB rest box, same-direction only · D4.1-R37 p1 @22:31–22:49 · D4.1-R34 p1 @20:36–21:03 · D4.1-R31 p1 @18:27–20:33 · OctD4.3-R28 @ 22:00 · OctD4.3-R29 @ 23:20 · OctD4.3-R69 @ 1:07:26 (trigger AND box)',
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
          title: 'Skip an ISB stop above 20',
          body: 'Release #61 (m466 G23): an ISB stop (entry to stop, the ISB range + 2 × 1.5) above 20 pts must not be taken — "nếu như một cái setup inside bar mà nó hai mươi mấy điểm, làm ơn giúp em đừng vô, làm ơn đừng vô… lệnh inside bar phải là một lệnh có stop loss nhỏ" [D4.1 p1 @05:41–06:08]; his own stop that morning was 20 points [D4.1 p1 @02:39–03:20], so exactly 20 is taken. D4.1 is a LATER lesson than D3.3 (20–25 = reduce size, above 25 skip [D3.3 p1 @01:09–02:09]), so for an ISB the 20–25 reduce option is gone; PHL/PLH and box stops keep the D3.3 rule (reduced at 20–25, skipped above 25). Example: the D3.4 short ISB at 29,034.25 with its stop at 29,056.75 (22.50 pts) is refused (isb_stop_over_max). A stored mentor_isb_max_pts (e.g. 25) still wins. Since 2026-11-10 that refusal is the ladder-OFF behaviour: with the stop-size ladder ON (the default, owner order) a big ISB, PHL/PLH or box stop is sized DOWN instead — 20–30 pts → 3 (“tầm 20 điểm, 30 điểm… Tôi vô size 3” [OctD2.2-R73 @01:12:32]), 30–40 → 1 MNQ (“Trade vẫn được. 1 MNQ” [OctD2.3-R47 @48:12]), above 40 not traded; the target never grows [OctD2.2-R49 @40:47]. The D3.4 example above is then 3 MNQ.',
          cite: 'm466 G23 — CourseExits.ISBMaxStopPts 20 · D4.1 p1 @05:41–06:08 (twenty-something: do not enter) · D4.1 p1 @02:39–03:20 (his 20-pt stop) · D3.3 p1 @01:09–02:09 (PHL/PLH keep 20–25 reduce, > 25 skip)',
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
          body: 'A rejected touch that does not become a PHL/PLH always names its reason in the funnel line, counted ONCE per visit of the level (not every minute the touch stands): phl_5m_box_ban, phl_mid_range_boxed, phl_ema_crossing_30m, phl_ema_loss_block, phl_not_location, phl_no_reject_entry, phl_level_arm_resting — beside the existing phl_15m_box_ban, phl_30m_box_ban, level_visit_cap, room, near_box and the rest. Before release #22 the first five dropped silently. The PHL’s room and trigger-side drops (room, isb_trigger_side) and keypair_candle_too_big also count once per visit now (they counted every minute). level_invalid_first_touch counts each level the first touch invalidated by closing on the wrong side [D5.2 p1 @19:51] — course-correct, and now visible. The EMA 34 is ONE line (the 1m EMA 34 [OctD5.3-R9 @02:00]): while the location-gate line ema34_htf is that same line (the default, location TF = EMA TF = 1m) its first-touch invalidation is logged and counted once, under ema34_htf (levels audit LL-6 — before, every one was logged and counted twice, ema34 and ema34_htf). The ORB logs its range once a day and its escape side + read time on the first escape and on every side change (“🧑‍🏫 ORB …”). The 15m/5m conflict also names every drop it causes (release #26): isb_tf_conflict (first_box), isb_tf_conflict_both_boxes (both_boxes, the held half), isbrev_tf_conflict (a reverse ISB) and phl_tf_conflict (a candidate PHL/PLH). The boot line also reads the key levels as "key levels N live · M deleted" — the old "levels 92" counted the deleted ones too (27 were live).',
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
          body: 'Like the 5m ISB rest box, the latest CLOSED 15m and 30m inside-bar candles are boxed and extended right. Inside a standing box, nothing trades against that box\u2019s direction — a 1m ISB is gated on the box direction, PHL/PLH and box returns inside are banned. When a timeframe\u2019s ISBs are CROSSING (2 opposite ISBs within the last 3 closed buckets) its TRIGGER line is not valid and the next timeframe\u2019s trigger governs (5m → 15m → 30m); the box gates are NOT lifted by the crossing (isbTF audit: D3.4-R43 escalates only the trigger, and the boxes stay no-trade zones against their direction — OctD4.3-R70 @ 68:24, R61 @ 59:56; before the audit a crossing skipped the 5m/15m/30m box gates and the CE-1 15m rule). A 1m candle that CLOSES beyond the edge lets you trade, but the box itself is deleted only when a candle of its OWN timeframe (5m/15m/30m) closes outside it (isb_box_escape, default close5m since m473 — Oct D4.3 @ 65:00-66:13; close = the 1m delete, the #61–m472 default, D4.1-R34 "nó đóng bên ngoài cái box là at least"; body = open AND close beyond, the #25–#60 default), and the side it exits is the side you trade (mentor_break_direction_lock, D4.1-R55 "thoát đầu nào thì mình trade đầu đó"). While a 15m box stands, a 1m ISB in its direction also needs a 5m ISB in that direction (15m_ib_requires_5m_isb, default require_5m_isb). Release #60: the 15m box is read EARLY — two 5m candles of the forming 15m inside the last 15m box it at once, provisionally, and refuse under _forming names until the 15m closes (15m_early_read, D4.2-R2); mode C holds while the standing 15m and 5m boxes agree (mtf_confluence_source, D4.2-R17); and, only with trigger_escalation ON (OFF by default since oct round 2 G3 — October reads the 5m only), when the 5m inside bars cross the 15m (then 30m) TRIGGER line governs the side (D3.4-R43).',
          cite: 'D4.1 p2 @07:35–08:11 · D4.2 p1 @16:28–16:46 · D3.4 p2 @17:07–17:49 · D4.1 p1 @20:36–21:03 (close escape, #61) · D4.1 p1 @07:47–08:07 (exit side) · D4.2 p1 @17:09–17:13 (5m ISB under a 15m box) · D4.2-R2 p1 @00:08–00:39 · D4.3-R21 @12:20–12:32 (early 15m read) · D4.2-R17 p1 @14:57–15:37 (mode C, standing boxes) · D3.4-R43 p2 @17:07–17:49 (trigger escalation)',
        },
        {
          title: 'The 4h trigger line is a target, never a location',
          body: 'The 4h trigger line joins the target ladder as target-only: the next-level target can land on it when it sits between the entry and the next key level, but it is never a PHL/PLH location and the touch loop never classifies it. The 1h trigger line is NOT a target (release #52 B5): the course names the 4h line as the way back ("Về lại 4 giờ nè anh chị") and never the 1h; the 1h only decides direction.',
          cite: 'D4.4 p2 @05:04–05:44 · kernel/mentor/mentor.go KindHTFTrigger · kernel/mentor/eval.go (htf_4h_trigger level only; the touch loop skips KindHTFTrigger) · kernel/mentor/chart_seed.go · kernel/mentor/location.go levelIsLocation',
        },
        {
          title:
            'Wick microscalp — "follow trend 5, kiếm lệnh 1" (advanced, OFF by default)',
          body: 'm465 (release #60), knob wick_microscalp, default OFF — the mentor calls it advanced and asks people who still get muddled trading ranges not to practise it first ("làm ơn đừng… đừng có tập khúc này đầu liền"). (1) TREND: the 5m trend read left to right — the d43_trend_source knob (default structure: the confirmed 5m swing highs/lows); a ranging read lets the run\u2019s own side decide ("cho dù là khung 5 phút nó đang ranging"). (2) RUN: on every CLOSED 5m candle the last 3 closed 5m candles are read; at least 2 must pull the wick WITH the trend (lower wicks for a long, upper for a short) and the summed with-trend wick must beat the summed against wick — one odd candle is tolerated ("technically nhìn ra nó vẫn có sự rút râu từ dưới rút lên"). Refusals, each named and counted: wick_run_too_short (only 1), wick_mixed_direction (the wicks pull both ways), wick_against_trend (upper wicks in an uptrend / lower wicks in a downtrend — "đang xu hướng giảm mà rút dưới là tôi không biết đường tôi đánh"). (3) ZONE: a box from the lowest to the highest wick extreme of the run, at least 5.00 and at most 14.25 pts tall measured from the extreme wick (his boxes: 5.00 / ~10 / 14.00 / 14.25 pts), drawn on the chart as a rectangle and extended right; one per side; a new run at a NEW place replaces it; it dies on a 1m close beyond its far edge (wick_zone_broken) or at the 17:00 CT trading-day roll. (4) ENTRY: never at market ("Vô market là nó đâm cho lủng bụng"); the zone\u2019s near edge is a LOCATION, so a 1m PHL/PLH whose reference candle dips into the box and closes back out arms through the ordinary touch loop, and a 1m ISB touching the box is the ordinary ISB (all its gates unchanged). A visit to the zone that produced no setup is named wick_zone_no_setup; a PHL/PLH read off the box on the WRONG side is dropped (wick_zone_wrong_side). (5) TARGET: the nearby 5m wicks — the highs of the last 12 closed 5m candles that carry an upper wick, above the box, at most 100 pts away (lows for a short) — "Target thì là những cái râu của khung 5 phút", never "100-200 điểm". The nearest one beyond the entry is the target when it leaves at least 1R of room (a wick target needs 1R, D1.4 "at least 1:1" — every other target keeps the 2R room rule); a wick closer than 1R is named wick_target_no_room and the target falls through to the next level. (6) SAFE PLAY (knob wick_safe_play, OFF): the setup waits until its reference candle CLOSED outside the box (under it for a short, over it for a long), else wick_safe_play_wait. Not built: the 30-second / 45-second views (NT8 sends 1m bars only).',
          cite: 'D4.3-R1/R2/R3/R5/R8/R10/R11/R12/R13/R15/R17/R19/R20/R27 · D4.3 p1 @01:27–02:02, @03:22–03:44, @04:44–05:46, @06:03–06:42, @09:17–09:46, @12:43–13:44, @14:07–15:47 · kernel/mentor/wick_zone.go · kernel/mentor/wick_microscalp.go · risk_control.mentor_tuning.wick_microscalp / wick_safe_play',
        },
        {
          title: 'Intraday confluence (exit C, size 5)',
          body: 'LONG = entry at an FTGL AND the 5m BUY trigger agrees; SHORT = entry at an FTGH AND the 5m SELL trigger agrees. The trigger is judged at the price you actually enter at (the rejecting candle’s high for a long, low for a short), NOT at the box edge — a buy line that sits between the box top and your entry counts, because you enter above it [D3.4 p3 @07:49–08:01; C52 W1]. No key-level condition [B3]. If you entered first and the trigger flips to your side afterwards, the trade upgrades to C and leg 1’s take-profit is moved from 1R out to 2R at NinjaTrader (a real modify on leg 1; a leg 1 that already took its 1R partial, or a one-bracket position, has nothing to move) — “vô rồi mới đợi trigger… hold at least risk reward 1-2” [D3.4 p3 @10:07–10:33, @11:01–11:05]. It also fires on timeframe agreement: the 15m ISB, the 5m ISB and the entry side all align (an ISB/PHL/PLH, not just a box) — the 5m trigger line is NOT part of this rule: D4.2 names only the three timeframes agreeing, opposite higher frames = no trade [D4.2 p1 @12:16–12:47, @14:30–14:52, @15:16–15:36; C52 W1 B9, owner order 2026-10-26]. Then: hold at least 1:2, the stop is never moved up (exit C), size 5 — confluence is a longer hold, it never adds contracts, and the big tier is removed [D3.4 p3 @07:52] (the course 1.1 L1103 “Cái sizing của em Cố định là 5 MNQ”; m186 reverts the 2026-10-16 “set to 4” flatten).',
          cite: 'R2 · D3.4 p3 @07:38, @07:49–08:01, @10:07–10:33 · D4.2 p1 @12:16–12:47, @15:16–15:36',
        },
        {
          title: 'A loss at a place blocks it',
          body: 'After a LOSS at a level, a box edge or the EMA line (entry filled, then the stop hit), that place is blocked until a departure candle. Two losses at one place → that place is off for the day (the “off for the day” part is OURS, stricter than the course: “thua 2 lần ở cùng 1 chỗ thì là mọi người đang sai” — and “đừng trade ở 1 khu vực, trade hoài ngay chỗ đó không có được cái gì đâu”; no day-long rule is stated, and the departure wait is OURS [D4.2 p2 @06:58–07:37]). An ISB loss (release #60, knob mentor_isb_loss_box, default range) boxes the WHOLE 5m range that held the trade — every closed 5m candle from the one the entry filled in through the one the stop hit in — not only the entry-to-stop band: “Market này có tổng cộng 3 phần: Trend, Range và Trend… em đợi cho cái điểm entry nằm ở vòng vòng bên ngoài” [D4.2 p2 @02:41–03:17]; his Box 1 28,880.50–28,934.00 is exactly the 08:30 5m candle. An entry inside that range — either side — is refused loss_range_blocked for the rest of the trading day; a 1m candle lying completely outside it frees only entries OUTSIDE the range (“em đợi cho cái điểm entry nằm ở vòng vòng bên ngoài” [D4.2 p2 @02:41–03:17]; “Tôi sẽ không trade bên trong khu vực này” [@03:43–04:07]) — a wick out and straight back is not a different zone. Each new range gets its own box (“tạo thêm cái range nữa… Đợi thoát ra đánh tiếp” [@03:06–03:31]); a loss whose range overlaps an earlier ISB loss range of the day joins it, and two losses there turn it off for the day (loss_box_off_day) — Winnie’s re-entries in the 57-pt chop 29,020.50 / 28,963.50, “cứ đè đây đánh, đè đây đánh” [@04:13–05:03].',
          cite: 'E2 · G2 · D4.2 p2 @01:56 · D4.2-R27/R28/R30 p2 @02:41–05:03',
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
          'The NEWEST 5m ISB candle is boxed and extended right (a newer 5m inside bar replaces the box — release #61); the 08:30 CT 5m candle (the first of the RTH day) is never itself the inside bar, the box starts from the 08:35 candle at the earliest (m473 B4, Oct D4.3 @ 57:20-57:35); only a same-direction 1m ISB trades inside; a 1m candle that CLOSES beyond the edge lets you trade, but the box is deleted only when a candle of its own timeframe closes outside (isb_box_escape, default close5m since m473, Oct D4.3 @ 65:00-66:13; close = the 1m delete, default #61–m472; body = open AND close beyond, #25–#60); after it breaks, only setups in the break direction until a close back inside (break_counter_setup)',
          'isb_box_escape = close5m (default) · mentor_5m_box_source = merge · mentor_break_direction_lock = ON · D4.1-R34 p1 @20:36–21:03 · OctD4.3-R59 @ 58:00 · D4.1-R31 @18:27–20:33',
        ],
        [
          'Too-small range',
          'Knob ON only (default OFF): the last 15 closed 1m candles span ≤ 61.50 pts with two candles ≥ 30.00 pts → no entry inside that range until a 1m close outside it (range_too_small_big_candles) — release #61',
          'mentor_small_range_guard = OFF (default since #61 final — count / margin unstated, on the mentor list) · D4.2-R33 p2 @06:16–06:30',
        ],
        [
          'Mid-range rejection side',
          'Between the floor and the ceiling box, after a candle rejects the ceiling no LONG entry until a candle reaches the floor (mirror for a floor reject) — mid_range_against_rejection, release #61',
          'mentor_mid_range_reject_side = ON · D2.2-R71 p3 @14:28–15:58',
        ],
        [
          'Day gate (§7)',
          'Measure the Globex run (17:00 → 08:30 CT) before the open. Conflict on a spent day → the machine is OFF for the day (latched at 08:30). The day gate is frozen AT the 08:30 read: a restart after 08:30 RESTORES that decision (the seed replays the 08:30 4h/1h read and the frozen run, then replays the one-way DayOff recheck over the closed 1h bars since — only when mentor_day_off_recheck is ON; course-defaults-30 turns it OFF, so the seed replays no clear), so a restart never re-decides the day — the log line reads "RESTORED from the 08:30 read". Only when the 08:30 read is not possible (no bars closed by 08:30) does the line read "re-read at boot … not the 08:30 decision". Spent + agree → trade but cap the target at 15 pts; on a spent day skip any setup whose stop is over 15. Answers-1112 Q1 extra rule (knob htf_opposed_range_halt_pts, default 0 = OFF): when set, the same rule is also re-read every minute AFTER the open — 4h/1h opposed and the Globex day (17:00 CT → now) at/above the knob → no new entries for the rest of the day.',
          'DayGateSpentPts 265 (release #60, D5.1-R18) · DayGateTargetCapPts 15 · htf_opposed_range_halt_pts 0 (off)',
        ],
        [
          '4h/1h direction',
          'Since oct round 2 (D2-G7) the DEFAULT gates only the news period and the 08:30–09:30 CT open (October: "khung 5 phút only" [OctD4.3-R9 @07:28]); htf_gate_news_only = OFF restores the all-session text that follows. ALL SESSION (release #59, m462 — the course\u2019s final review: "Lúc nào khi anh chị open ra, không cần phải là news" [D5.1-R9, D5.1 p1 @08:46–09:13] and "Trigger 4h, trigger 1h nằm ngược nhau… mình không làm được gì hết trơn" [D5.1-R11, D5.1 p1 @08:29–09:52, @11:27–11:50]): the 4h/1h direction gate applies at every minute of the session, not only around news and the open. One shared check (HTFEntryCheck) covers every intraday setup — ISB, reverse ISB, PHL/PLH at a level and the rolling PHL/PLH, box returns — and a backstop filter re-checks every entry before it leaves the evaluator; each refusal is named per setup (…_htf_conflict / …_htf_side_mismatch / …_htf_no_4h) and counted. A strategy that STORED htf_gate_news_only = ON keeps the old windows (print day 07:20–08:30 CT and the 08:30–09:30 CT open; outside them not gated). SWING4H is EXEMPT: its direction is the 4h EMA 34, not the trigger lines [D5.2-R7, D5.2 p3 @15:04–18:25]; planner/AI entries have their own G1 HTF veto. The live trader inherits this with no config edit (the default flipped from news + open to all session). Entries only WITH the 4h trigger. At the OPEN (08:30–09:30 CT) a with-4h entry additionally waits for the 5m range to break in the 4h’s direction — "không phải là cứ cắm đầu mà cứ vô lệnh buy market … chuyển về khung 5 phút … nó break được cái rank khung 5 phút" [D4.4 p2 @04:41–06:00]; the opening 5m range is the first 5m candle (08:30–08:35) [example: the ORB’s 08:30 anchor]. Release #61: the break is a CLOSED 1m candle outside the range — "một cây nến một phút… đóng bên ngoài cái box là at least" [D4.1 p1 @20:36–21:03], "nó break được cái range khung 5 phút, nó đi lên rồi, tôi biết tôi đánh" [D4.4 p2 @05:15–05:36] (on 14 Sep the mentor acted at the 08:43 1m escape of the 28,880.50–28,933.75 range) — switch mentor_open5m_break (1m_close default; 5m_close = the pre-#61 rule, a closed 5m candle, up to 4 minutes later). The 1h counts only when it fired at or after the 4h — compared by the MINUTE each line really fired, not by which candle opened first (an earlier 1h trigger is ignored — it is silent; W7 B3): 1h agreeing or silent → follow the 4h; 1h opposite → sit out until it flips. No 4h trigger → nothing to follow. The 4h/1h lines (and the 5m trigger line) are seeded from history at boot by replaying every stored 1m bar through the same intrabar break rule the running bot uses, so a restart lands on exactly the line a bot that never stopped would hold — including a candle that broke both sides (which side broke first decides). The boot line prints each line as "<tf> trigger <dir> @<price> since <CT>" (the minute the line fired; the 4h/1h lines carry the break minute since release #52, the 5m line its bucket open), and every 4h/1h move is logged as it happens ("4h trigger moved long @… → short @…"). On a red-folder print day the seed freezes each replayed day\u2019s print-window trigger read exactly as the running bot did (release #29: the read is frozen from the print to the 08:30 CT open — the candles are never rewritten; release #61: the freeze starts 30 minutes BEFORE the print, mentor_news_read_lead_min; release #23 dropped the 07:20–07:35 print candles). History that reaches the store AFTER the boot — NinjaTrader\u2019s replay is held until the first live bar, and a refilled bar hole — re-seeds the trigger lines, the swing line, the EMA 34/9 on the execution frame (1m 08:30–09:00 CT, otherwise 2m) and the key-level deletion on the next tick, keeping every latch, arm, leg and visit count; the log line "🧑‍🏫 mentor re-seed after a history refill (…): CHANGED|UNCHANGED … before → after" names what moved (counted reseed_after_refill / reseed_changed). 10-14: the 08:22 boot seeded before the 03:44–07:47 hole reached the store and traded on 1h short @30790.75 instead of 30716.00.',
          '—',
        ],
        [
          'Room rule',
          'The room to the FIRST available level must be at least 2× the first take-profit — leg 1 at 1:1 → 2R; a confluence (mode C) leg 1 at 1:2 → 4R [D5.3 p1 @09:16–10:17 · D2.2 p3 @12:13 · D1.2 p1 @07:41–08:45]. Release #30: the EMA 9 / EMA 34 lines are PARTIAL points along the way, not the level the room (and the 1:1 floor) measure — "ta ghẹt tôi đầu tiên phải là về gần đỉnh cũ … tiếp tục là EMA 34 … những cái chỗ này là những cái chỗ anh chị bán bớt" [D3.3 p1 @05:15–07:38]; an EMA must not by itself kill the trade (the room/floor read the next non-EMA level — the nearest old high/low, a key level, or a trendline target; the EMA is the target only when no other level stands beyond). The room is measured to the LEVEL, also on a spent day: the 15-pt take-profit cap shortens the take-profit, never the room — "dù target của em là 15 điểm … từ điểm entry xuống target nó phải từ 30 điểm đổ lên" [D5.3 p1 @09:32–09:50]. So a spent-day setup needs stop ≤ 15 (the 1:1 floor on the capped take-profit), room ≥ 2R to the level, AND room to the level ≥ 2× the capped take-profit — his worked figure: target 15 → room 30 (a take-profit sitting right at the level is refused). A PHL/PLH stop never exceeds 25 pts (SWING4H exempt — its ceiling is 100)',
          'RoomMultiple 2 · StopCeilingPts 25 (PHL/PLH and box stops only) · mentor_isb_max_pts 20 (release #61, D4.1-R12 — the ISB and reverse ISB only; the trader’s R8 gate reads the SAME knob for an ISB, PHL/PLH/BOX keep 25) · swing_max_stop_pts 100 (the swing SIZING cap, ≤ 2 MNQ) · mentor_swing_max_stop_pts 80 (the swing SKIP — above it the swing is not entered, m463 D5.2 p2 @19:34–21:43; 80 from “7-80 điểm” [Sept D5.2 p2 @05:25–05:33], 90 reverts) · stop-size ladder ON: the ISB 20 and PHL/PLH/box 25 above are the ladder-OFF ceilings — with the ladder a non-swing stop up to mentor_wide_stop_max_pts 40 is sized down (20–30 → 3, 30–40 → mentor_wide_stop_contracts 1, never larger than the reduced tier) [OctD2.2-R73 @01:12:32, OctD2.3-R47 @48:12]',
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
          'OFF by default since m472 A1 (the October course: "Trade khung 1 phút, chứ không là khung 2 phút. Khung 2 phút nó quét bạn xác" [Oct D4.2 @ 22:56–23:46]; repeated [Oct D5.4 @ 03:09–03:24], [Oct D5.5 @ 09:16–09:20]) — the 1m all day. The rest of this row describes the schedule when you turn it ON (the September rule): the named setups (ISB + PHL/PLH) are read on the 1m chart ONLY during the RTH open +30 minutes (08:30–09:00 CT — "a 2m stop cannot cover the setup at open volume"); at every other minute they are the 2m ("khung 2 phút ÍT BỊ QUÉT RÂU" — the 1m wicks sweep stops, so the 2m read is quieter), overnight and the Europe open (3am Texas) included. A SLOW evening/overnight (15:00–03:00 CT, the 1m range since the RTH close under 100 pts) reads the 5m instead ("Trade khung 5 phút only … cho buổi tối" [X11 @17:40–18:32, quote @18:03–18:07]); the 100-pt cutoff, the 15:00 start and the cumulative window are ours — X11 names 100–200 pts as a STRONG night, where the 2m stays, and the owner fixed no number (Q6) [derived: X11 @17:40–18:26]; evening_slow_pts / evening_slow_from_hour / evening_slow_window_min make all three tunable. The higher-TF lines and the box/ORB ESCAPE test stay a 1m body close; boxes are DRAWN on the traded frame (1m 08:30–09:00 CT, closed 2m otherwise, never 5m); trendlines and the EMA 9/34 follow the same 1m/2m clock. An ISB or PHL/PLH entered on the 2m trails its runner on CLOSED 2m candles, its candle-3 partial reads the 2m fill candle, and its break-even reads the 2m close — "trade khung nào thì gồng theo khung đó" [X8 @18:02–18:14] (only the 1:1 live rule keeps the 1m close); after a restart that frame is not known and the runner trails the 1m (WARN trail_tf_unknown_after_restart).',
          'mentor_exec_2m_after_30m = false (default OFF since m472 A1, [Oct D4.2 @ 22:56–23:46]; true = the September 2m schedule) · X5 @00:07–01:17 · X11 @17:06–19:35 · "Routine trước khi trade" @00:41–01:14',
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
          title:
            'Trading window — the RTH band by default (08:30 to 15:15 CT), since m472 A6',
          body: 'Since m472 A6 an unset window is the RTH band, 08:30 to about 15:15 CT = 405 minutes (the black band on the chart, screen-read, no spoken number [Oct D5.2 @ 01:00–01:20]; the Asia / Europe hours are the blue zone he does not trade [Oct D4.3]); the September 60-minute window 08:30–09:30 [D1.2 p1 @24:04–24:15] is no longer the default. A stored 60 (or 30 / 90 / 120) still limits the day, and a stored -1 is the owner\u2019s explicit opt-out (no window). A window length of -1 ("No window (EOD rules still apply)") removes the time window only — it never means any hour: no NEW mentor entry is placed from 15:45 CT until the 17:00 CT reopen, and from the EOD flat (NY 14:45 CT; earlier on a registered half-day) until 17:00 every intraday mentor order is cancelled and an open intraday position is closed. On a shortened day (12:15 CT close on 11-27 and 12-24) the last mentor entry is 15 minutes before the early close (12:00 CT) and the flat is 5 minutes before it (12:10 CT), the SWING4H included. With -1 stored, mentor arms are authored around the clock inside those limits — both bound strategies save -1 today (row 362 was written at 01:02 CT); an unset strategy gets the RTH band instead. The SWING4H is exempt from the window and from the ordinary EOD flat either way.',
          cite: 'October D5.2 @ 01:00–01:20 (RTH band 08:30–15:15 CT) supersedes September D1.2 p1 @24:04–24:15 ("đặt một cái timer… đúng một tiếng", 08:30 + 60) · mentor_window_start / mentor_window_minutes — live · trader/mentor_tick.go mentorWindowActive (minutes ≤ 0 = no window) · 15:45 cutoff: trader/mentor_mode.go mentorPastDailyHaltCutoff · EOD flat: trader/auto_trader_clock.go enforceEODFlatAt',
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
          title: 'Spent 265+ overnight AND 4h/1h disagree at 08:30 → day OFF',
          body: 'The day gate reads the Globex run at 08:30 CT. If the overnight daily candle already ran 265+ pts (release #60, D5.1-R18: the course says 300–400, and its worked example — a −265.00 run, "nó chạy gần được 300 điểm… nó chạy gần hết cây daily luôn rồi", "phải còn 100 điểm" [D5.1 p1 @17:56–18:14] — was already spent) AND the 4h and 1h disagree, the machine is OFF for the day (latched at 08:30) — no box/ISB trades all day, the swing exempt. course-defaults-30 (item 4): the re-check is OFF (mentor_day_off_recheck, nil → OFF) — the day STAYS off, a whole-day latch. The pre-30 owner ruling (2026-10-09) that re-checked the OFF on every closed 1h bar is superseded.',
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
          'Reduced (a PHL/PLH/box stop of 20–25 pts — an ISB stop above 20 is skipped since release #61; also an ISB at an old high/low or inside a range; the 5m pullback warning)',
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
          'A PHL/PLH filled and then an ISB in the SAME direction within 3 candles AFTER the fill candle — counted in candles of the frame the entry was read on (1m, 2m or 5m; the fill candle itself is not counted — ours, the course does not say [D2.4 p1 @01:44–01:56, @04:44–04:53; X8 @18:02–18:14]). Release #60 (D2.4-R2): ANY inside bar of that frame whose candle 1 has the trade’s colour counts — read straight off the closed candles, even when that ISB’s own entry was refused (its stop above the ISB max, room, location…): “trong vòng 3 cây nến sau đó xuất hiện một setup Inside bar cùng chiều” [D2.4 p1 @01:37, @04:54] (switch mentor_resonance_raw_isb, ON; OFF = only an ISB that passes every placement gate except the never-add gate, as before; counter resonance_armed_raw_isb). The position flips to mode A (do not trail it); leg 1’s 1:1 take-profit stays, and the RUNNER is NOT capped: its resting PHL/PLH take-profit is moved far out of reach (600 points beyond the entry — ours, the course only says “let it run”; the AddOn cannot delete a take-profit leg) so it exits only by its break-even stop, the X8 trap-dodge or the EOD flat [D2.4 p1 @01:58–02:09, @02:24–02:47]. With mentor_resonance_runner_tp_old_high ON the runner’s take-profit is instead moved to the level beyond the old high (the bot’s older inference); a one-bracket position is never touched. A position REBUILT from the broker after a restart has an unknown fill age, so its 3-candle window is treated as already spent and a later same-side ISB cannot flip it to A (release #53 m437). The stop does NOT move at the arm: on the CLOSED 1m candle, a split position’s RUNNER moves to break-even only AFTER leg 1’s 1:1 partial is confirmed filled at NinjaTrader (leg 1 keeps its original stop until its take-profit — “mình bán bớt rồi… thì cứ để break even cho nó chạy” [D2.4 p1 @03:36–03:45]); a one-bracket position has no partial, so it goes to break-even once the close is at least 0.25R beyond entry (a CTO default the owner may change)',
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
          'No intraday exit path rides through the end of the day. At the EOD flat the bot first cancels the resting intraday orders, then reads its open positions and closes each one through the trader close path — at market by default (the server setting EOD_FLAT_LIMIT_TICKS above 0 tries a limit first, then market). An A, B or C trade or an ISB runner still open is closed there, target or not. Only the SWING4H position is exempt — held by the 4h (log "SWING4H position … EXEMPT — held by the 4h"). When: whenever no session window is open — from the NY close at 14:45 CT until the 17:00 CT reopen, so the first bot cycle at or after 14:45 CT closes it — or earlier inside a session when a registered half-day (or a session’s eod_flat_offset_min) pulls the flat in. On a half-day the flat is 5 minutes BEFORE the early close (12:10 CT on a 12:15 day), never at the close minute when the market halts, and the SWING4H is NOT exempt that day (its 4h hold ends at the early close; log "SWING4H position … NOT exempt on a shortened day"). It runs while mentor mode (or the day plan) is ON. A position the broker reports but the bot’s own table does not is never auto-closed: the bot logs "NOT FLAT" for the owner and counts flat_claim_refused_broker_disagrees. (trader/auto_trader_clock.go enforceEODFlatAt → flattenPosition) Authoring is refused inside the flat window too: no intraday mentor arm is written once the session has ended (or inside a forced-flat window such as the news lead), because the armed pass would cancel it within milliseconds; the refusal is counted as the gate "Mentor — flat window (EOD / forced flat)" (placement_refused_eod_flat). SWING4H is exempt.',
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
          body: 'Each mentor setup kind (lvl, isb, isb-reverse, phl/plh, box, swing) is counted per session day: how many entries the evaluator emitted, and how many reached the ledger. If a kind crosses 3 emitted with 0 reached the ledger while the market is open, a P1 alert fires once that day naming the kind, the window and the last refusal reason (e.g. "lvl: 7 emitted, 0 reached the ledger since 10-12 17:00 CT — last refusal: none recorded (silent drop)"). Refusals that are YOUR OWN RULES working do not count toward the 3: the trading window, the end-of-day flat window, done-for-the-day after a win, stop after a loss, the daily loss limit and the news hold. A trader with a 60-minute window refuses every out-of-window setup by design, and used to get a false "stalled" P1 for it (15 of the 27 stalled alerts in the log before release #54 were exactly that); when some of the emitted setups were refused by those rules the alert says how many. Alert-only — no trading behaviour changes. The per-kind counts are read from the gate-blocks endpoint (setup_stalls; policy_refused is the count of your-own-rules refusals). Mentor refusals also appear on the gate-blocks table by name: mentor_rule_r8, mentor_rule_r9, mentor_rule_r31 (a swing stop above mentor_swing_max_stop_pts, release #59), mentor_bad_geometry, mentor_untagged_setup, mentor_size_refused, mentor_resonance_hold and mentor_place_off (dry run). Box returns dropped inside the box rules are counted box_trigger_side, box_ping_pong, box_inside_box, box_stop_ceiling, box_no_target and box_room. The tally is in-memory: a restart (every release install) and the 17:00 CT session-day rollover both reset it.',
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
          body: 'Two or three 5m candles with wicks all the same way make a small zone the mentor fades (“advanced, đừng có tập khúc này đầu liền”). BUILT in m465 (release #60) behind the Studio switch wick_microscalp, default OFF — see "Wick microscalp" in the setups above. Still open: the 30-second / 45-second views he mentions (NT8 sends 1m bars only) and the mentor questions on the exact box height and the tolerance for one odd candle.',
          cite: 'D4.3 p1 @00:00–00:25, @01:23–01:50, @06:30–06:44 · owner decision B15 · m465 P5',
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
          what: 'Size when the stop is 20–25 pts, where the band top is the setup’s own ceiling (trader/mentor_mode.go mentorStopCeilingFor → mentorContractsFor): 25 for PHL/PLH/box stops; for an ISB the mentor_isb_max_pts knob — 20 since release #61 (D4.1-R12), so the ISB 20–25 band is UNREACHABLE at default (an ISB stop above 20 is skipped first); it is reached only with a stored mentor_isb_max_pts above 20. A SWING4H is EXEMPT from this band (the swing tier ranks first, so a swing stop in (20,25] sizes swing4h, not the reduced tier). A spent day does NOT use this tier — it keeps the normal size and cuts the target and the runner. The reduced tier actually reduces: 3 (D4.1 p1 @04:37–05:04 “giảm size”). Release #60: an ISB/PHL/PLH entry read on the 2m (or 5m) also sizes this tier (tier exec_2m_reduce, switch “Smaller size on a 2m entry”, D3.1 p1 @14:09–14:35). Stop-size ladder (2026-11-10, default ON): any non-swing stop in (20, 30] — an ISB included — sizes at most this tier (tier stop_ladder_reduced, “tầm 20 điểm, 30 điểm… Tôi vô size 3” [OctD2.2-R73 @01:12:32]).',
          trader: '3 contracts on the reduced tier.',
          consumer: 'trader/mentor_mode.go mentorReducedContractsDefault',
          range: 'int',
          systemDefault: '3',
          recommended:
            '3 — the course’s reduced tier (D4.1 p1 @04:37–05:04 rules 2+3 “giảm size”; m186 reverts the 2026-10-16 “no longer cuts below 4”). The band is a 20–25 pt PHL/PLH/box stop (an ISB above 20 is skipped since release #61 — D4.1 p1 @05:41–06:08). An ISB at an old high/low or in a range also sizes 3.',
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
          what: 'NOT READ FOR SIZING. The value is resolved and then discarded (trader/mentor_mode.go mentorContractsFor: `_ = spentCap`). A spent day (§7: the day already ran 265+, release #60 D5.1-R18) keeps the normal size; what it cuts is the target (15-pt cap) and the runner — at most 2 contracts run, a fixed code constant (mentorSpentDayRunnerCap), not this knob [R09].',
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
          what: 'Only RTH candles count for key levels. The last 1H candle (14:30) runs to 15:15 CT, not 15:00: 1m bars that open before 15:15 fold into it (m472 A7, [Oct D5.2 @ 02:14–02:40]). INERT at the default key-level timeframe (60): the 1H series is already anchored at the 08:30 RTH open, so this switch only changes the walk for another key-level timeframe (experimentation).',
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
          what: 'R7: an ISB pointing AGAINST the trend at EMA 9 reverses — uptrend → long buy stop above the ISB high + 1.5-pt buffer; downtrend → the mirror (sell stop below the ISB low − 1.5-pt buffer). The entry buffer is mandatory on the 1m [D1.4 p1 @ 22:26–23:26]. Requires price to actually reach EMA 9, read on the frame the pair is read on (C52-W4): the 2m EMA 9 against a 2m pair, the 1m EMA 9 against a 1m pair — "anh trade khung nào thì anh bật khung đó, khung M1 cũng được, khung M2 cũng được" [Ngày 5.4 ISB đảo chiều p1 @08:15–08:40]. The touch is the candle GROUP (release #60, D5.4-R7): the EMA 9 must lie inside the mother + ISB candles’ combined range — "nó không cần đụng, nó cứ đụng vòng vòng ngay EMA 9 là được … không quan trọng là nó đụng ngay tại EMA 9 hay là nó lố EMA 9" [D5.4 p1 @03:35–03:46], on screen the 09:45–09:48 candles straddle the line; price must still reach it (knob isb_reverse_touch; isb_candle = the ISB candle alone, pre-#60), and the slow-evening 5m pair keeps the 1m EMA 9 until the mentor says which frame the evening lines are drawn on. It carries a target (the next level beyond, with the 1:1 floor and the spent-day cap) and runs through the normal ISB gates — the 30-pt stop ceiling, 4h side, near-box room — before it can place; with it ON, the normal ISB does not arm the opposite side on the same candle pair.',
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
            '4h/1h gate only in the news + open windows (ON = the default since oct round 2; OFF = all session, release #59)',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D2-G7 (oct round 2): ON (the DEFAULT, unset) = the 4h/1h direction gate applies ONLY in the news period (print day 07:20–08:30 CT) and the 08:30–09:30 CT open window; outside them the 5m trigger side and the 1m structure decide — October\u2019s only big timeframe is the 5m ("khung 5 phút only. Ở thời điểm hiện tại mình chỉ sử dụng khung 5 phút" [OctD4.3-R9 @07:28]; the slide\u2019s 1H line is ignored [OctD4.3-R2 @01:56]) and no October lesson states an all-session 4h/1h gate or a 4h/1h conflict sit-out (the FINAL report\u2019s "Oct D5.1-R9" is September\u2019s, reread D5.1-RR2-01). OFF (a stored false) = the m462 September behaviour described next. m462 (release #59): OFF = the 4h/1h direction gate applies ALL SESSION — every minute, Asia, London, NY and the evening — to every intraday mentor setup: ISB, reverse ISB (also when it is retagged PHL/PLH), PHL/PLH at a level, the rolling PHL/PLH pullback, and FTGH/FTGL box returns (their trendline / wick-microscalp / flag-wall targets ride on those same entries). The course\u2019s final review overrides the D4.4 "news first" caveat: "Lúc nào khi anh chị open ra, không cần phải là news… nếu là news thì 1h trước news và 4h trước news; còn nếu là open cũng vậy" [D5.1-R9, D5.1 p1 @08:46–09:13, @11:27] and "Trigger 4h, trigger 1h nằm ngược nhau… cứng ngắt nằm giữa cái range luôn, mình không làm được gì hết trơn" [D5.1-R11, D5.1 p1 @08:29–09:52, @11:27–11:50 — on 15 Sep the conflict held until 11–12 h, long after the open]. The three cases [D4.4 p1 @14:08–15:34]: (1) 4h and 1h trigger the same way → trade only that side; (2) the 4h triggered and the 1h has nothing new (no line, or its last move is OLDER than the 4h\u2019s) → follow the 4h; (3) the 1h points opposite the 4h → sit out until the 1h flips to the 4h. No 4h trigger at all → nothing to follow. Every refusal is NAMED and counted per setup: isb_ / isbrev_ / phl_ / box_ / rolling_ + htf_conflict (case 3), htf_side_mismatch (the entry is against the 4h), htf_no_4h (no 4h line). Until #59 the default was ON (news + open only), so the bot traded straight through conflicts: trades 669, 677, 698, 705 (4h vs 1h opposite) and 716 (4h + 1h short, a long ISB). SWING4H is NOT gated by the 4h/1h triggers: its direction is the 4h EMA 34 — "4 giờ đụng resistance thì mình đánh xuống chứ không lẽ đánh lên?" [D5.2-R7, D5.2 p3 @15:04–18:25] (trade 715 is the swing\u2019s own call). The §7 day gate is unchanged (a conflict on a spent day still switches the day OFF at 08:30). ON (only when you STORE it) = the old D4.4-11 windows: a red-folder print day from 07:20 CT to the 08:30 CT open plus the 08:30–09:30 CT open window (the 60 minutes are OUR number); outside them ordinary intraday entries are not HTF-gated [D4.4 p1 @13:55–14:06 "đánh níu nên sử dụng cho níu trước đi"].',
          trader:
            'ON — news + open only (default since oct round 2 D2-G7; was all session since release #59, m462). A strategy that STORED false keeps the all-day gate.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (HTFGateNewsOnly) · kernel/mentor/htf_direction.go HTFGateActive / HTFEntryCheck / htfEntryFilter · kernel/mentor/eval.go (ISB, reverse ISB, box) · kernel/mentor/phl.go (PHL/PLH) · kernel/mentor/rolling_phl.go (rolling)',
          range: 'true / false',
          systemDefault: 'true',
          recommended:
            'ON — October: the 5m is the only big frame [OctD4.3-R9 @07:28]. Turn it OFF only to restore the September all-session gate [D5.1-R9/R11/R20] (it refuses counter-4h and 4h/1h-conflict entries at every minute).',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label:
            'Execution frame: 2m after 09:00 CT (1m 08:30–09:00) for ISB + PHL/PLH, EMA 9/34, boxes, trendlines and the X8 exit',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'X5-10: OFF by default since m472 A1 (1m all day [Oct D4.2 @ 22:56–23:46]). The rest of this cell describes the schedule when you turn it ON: the named setups (ISB + PHL/PLH) are read on the 1m ONLY during the RTH open +30 minutes (08:30–09:00 CT — "a 2m stop cannot cover the setup at open volume"), and on the 2m at every other minute — overnight and the Europe open (3am Texas) included ("khung 2 phút ÍT BỊ QUÉT RÂU" [X5 @00:07–01:17; X11 @17:06–19:35]). A SLOW evening/overnight (15:00–03:00 CT, the 1m range since the RTH close under 100 pts) reads the 5m instead ("Trade khung 5 phút only … cho buổi tối" [X11 @17:40–18:32, quote @18:03–18:07]); the 100-pt cutoff, the 15:00 start and the cumulative window are ours — X11 names 100–200 pts as a STRONG night, where the 2m stays, and the owner fixed no number (Q6) [derived: X11 @17:40–18:26]; evening_slow_pts / evening_slow_from_hour / evening_slow_window_min make all three tunable. The ISB pair and PHL/PLH swap; the higher-TF lines and the box/ORB ESCAPE test stay a 1m body close; boxes are DRAWN on the traded frame (1m 08:30–09:00 CT, closed 2m otherwise, never 5m); trendlines and the EMA 9/34 follow the same 1m/2m clock. An ISB or PHL/PLH entered on the 2m trails its runner on CLOSED 2m candles and its candle-3 partial reads the 2m fill candle (the frame follows the entry frame [X8 @18:02–18:14]).',
          trader:
            'OFF (since m472 A1, [Oct D4.2 @ 22:56–23:46]: "Trade khung 1 phút, chứ không là khung 2 phút"). With the switch ON the course read of September applies — the 1m for the first 30 min after the open, then the 2m ("sau 30 phút em sẽ chuyển qua khung 2 phút", "khung 2 phút ít bị quét râu"). The EMA 9/34 levels and the EMA 34 location line move to the 2m on the same schedule (M401), and the chart’s EMA curve carries the frame it was drawn on (tf_min).',
          consumer:
            'kernel/mentor/eval.go Tick (Exec2mAfter30m → closedBucketsTF(bars, 2, now)) · trader/mentor_tuning.go mentorTuningResolve',
          range: 'true / false',
          systemDefault: 'false (m472 A1, [Oct D4.2 @ 22:56–23:46])',
          recommended:
            'OFF — the 1m all day [Oct D4.2 @ 22:56–23:46] (supersedes the release #31 default ON). Turn ON only to restore the September 2m schedule.',
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
          label: 'Early 15m read (two 5m candles)',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D4.2-R2 [D4.2 p1 @00:08–00:39] "3 cây 5 phút mà nó cứ xào quần xào quần ngay chỗ đó mà nó không đi đâu hết... ngay lập tức mình biết khung 15 phút đang tạo inside bar" (three 5m candles churning in one place, going nowhere — you know at once the 15m is making an inside bar) · @07:40 "2, 3 cây 5 phút anh chị đã có được 1 cây 15 phút" · D4.3-R21 [D4.3 @12:20–12:32] "2 cái nến 5 phút, 3 cái nến 5 phút, nó đang làm cái nến 15 phút rồi đó" · D4.2-R11 [@11:40–11:42] the benefit is defensive: "anh chị sẽ né được rất nhiều đoạn ranging và bị bớt thua". ON (default, release #60): when no 15m box stands and the FORMING 15m candle already holds exactly TWO closed 5m candles whose body (open of the 1st, close of the 2nd) sits inside the last closed 15m candle (wicks included), that forming candle is boxed at once — a PROVISIONAL 15m inside-bar box (the high/low of the two 5m candles, direction = the colour of the last closed 15m). Every 15m-box rule applies to it and refuses under its own name: isb_15m_box_dir_forming (a 1m ISB against it), isb_15m_needs_5m_isb_forming, isb_tf_conflict_forming (a 5m box the other way), phl_15m_box_ban_forming, box_isb_ban_15m_forming (isbrev_… for the reverse ISB). At the 15m close (the 3rd 5m — the 5m and the 15m close together) the real 15m box replaces it (counted isb_15m_forming_confirmed) or, if the 15m closed as no inside bar, it is dropped (isb_15m_forming_dropped); a tf_conflict_end both_boxes hold that only the provisional box started is settled when that 15m candle closes, even if the provisional box was already escaped: closed as no inside bar → the hold is released (tf_conflict_forming_released) — no 15m inside bar ever existed; closed as an inside bar → the hold now rests on a real 15m inside bar and stays until both boxes escape. The early read needs the 15m candle immediately before the forming one as its mother, so it never reads across the 16:00–17:00 CT halt, a weekend or a missing bucket. It only ever REFUSES — it never counts for mode C. WE-1 (Mon 14 Sep): the 15m mother 29,026.00 / 28,970.75, the next 5m candles churn inside it.',
          trader:
            'ON — the course reads the 15m from the 5m at once; OFF restores the closed-15m read only (pre-#60).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/mtf_forming15.go forming15mISB / advance15mBox · kernel/mentor/eval.go Tick (the 15m box gates)',
          range: 'true / false',
          systemDefault: 'true',
          recommended:
            'ON — D4.2-R2 / D4.2-R11 / D4.3-R21 (release #60, owner order 2026-11-02 "fix everything the video check found").',
          whenToTouch: 'Only on an owner ruling.',
          perSession: 'No.',
        },
        {
          label: 'Mode C reads the standing 15m + 5m boxes',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D4.2-R17 [D4.2 p1 @14:57–15:37] "tại sao hồi sáng em lại không move stop loss mà em đặt stop loss ở dưới để cho nó chạy, tại vì đây nó cộng hưởng quá nhiều thứ... Khung 15 phút nó cũng kêu mình làm vậy, khung 5 phút cũng kêu mình làm vậy" (I did not move the stop — I left it below and let it run, because so much resonates: the 15m says so, the 5m says so). WE-2: the 15m box 29,079.25 / 28,985.00, the 5m inside bar (the red 10:10 candle) 29,048.50 / 29,017.25, the 1m entry ~29,040, ran ~+100. standing_boxes (default, release #60) = mode C while a CLOSED 15m inside-bar box stands in the entry direction AND the 5m agrees — a standing 5m box the same way, or the last 5m box (drawn after the 15m candle) pointed and escaped the same way (the same 5m test as "15m inside-bar box needs a 5m ISB"). A provisional (early-read) 15m box never counts. last_pair = the pre-#60 read: the LATEST closed 5m and 15m candle pairs must both be inside bars — one 5m candle after the 5m inside bar that read was gone, though both boxes still stood.',
          trader:
            'standing_boxes — the course holds while both boxes say so; last_pair restores the pre-#60 read.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/mtf.go MTFConfluenceStanding / mtfConfluence · kernel/mentor/eval.go (ISB, reverse ISB, PHL/PLH emit)',
          range: 'standing_boxes / last_pair',
          systemDefault: 'standing_boxes',
          recommended: 'standing_boxes — D4.2-R17 (release #60).',
          whenToTouch: 'Only on an owner ruling.',
          perSession: 'No.',
        },
        {
          label: 'Trigger escalation 5m → 15m → 30m',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'G3 (oct round 2): default OFF — October reads the 5m only ("khung 5 phút only. Ở thời điểm hiện tại mình chỉ sử dụng khung 5 phút" [OctD4.3-R9 @07:28]) and never repeats the September escalation; the 15m ISB box (OctD4.3-R60) is unchanged. The September rule, ON: D3.4-R43 [D3.4 p2 @17:07–17:49]: when inside bars cross back and forth so the 5m buy/sell trigger is not valid, go to the 15m; if that fails, the 30m; then the 1h and the 4h. ON (release #60 default): the bot keeps a 15m and a 30m trigger line beside the 5m one (same break rule, seeded from history at boot). When the last 3 closed 5m candles hold 2 inside bars pointing opposite ways (the "crossing" read — no new number; since the isbTF audit it moves only the trigger, never a box gate), the 15m trigger line governs every trigger read of the intraday setups: the ISB side, the reverse ISB trend, the PHL/PLH level side, the trigger-retest direction, box returns and the trigger-box zone. When the 15m inside bars cross too, the 30m governs. The 1h/4h steps are the existing 4h/1h direction gate. Refusals carry the TF: isb_trigger_side_15m / isb_trigger_side_mismatch_15m (… _30m, isbrev_…, phl_level_trigger_side_15m, trigger_retest_wrong_way_15m, box_trigger_side_15m); every switch is logged and counted once (trigger_escalated_15m / trigger_escalated_30m / trigger_escalation_ended). Before #60, a standing 5m inside-bar box hid the trigger while the crossing skipped that box’s gate — in exactly that chop nothing governed the side. Since the isbTF audit a standing box never hides the trigger (OctD4.3-R28/R69). The trigger-retest LEVEL stays the 5m line; the rolling PHL/PLH (X8 "nhìn khung 5 phút") and the swing zone stay on the 5m.',
          trader:
            'OFF — October reads the 5m only (default since oct round 2 G3); ON = the September D3.4-R43 escalation.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/trigger_escalate.go governingTrigger · kernel/mentor/eval.go Tick (every gating trigger read) · kernel/mentor/seed.go (15m/30m lines seeded)',
          range: 'true / false',
          systemDefault: 'false',
          recommended:
            'OFF — October [OctD4.3-R9 @07:28]; ON restores D3.4-R43 (release #60). The crossing count (2 opposite ISBs in 3 buckets) is the open mentor question Q10.',
          whenToTouch: 'Only on an owner ruling.',
          perSession: 'No.',
        },
        {
          label: '5m/15m box conflict end',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D4.2 p1 @07:54–08:02 "Khi anh chị thấy nó messed up như vậy — Đừng trade. Đợi nó thoát cả luôn" (when you see it messed up like that — do not trade; wait until it escapes entirely). While a 5m box and a 15m box conflict, nothing trades. A box return met during the conflict is refused box_tf_conflict and used up — it is not traded late once the conflict ends (release #61 review round 2; before, it was skipped with no count and could be placed minutes later above/below the market). first_box = the conflict ends when the small 5m box escapes; both_boxes (default, course-defaults-30) = no trade until BOTH the 5m and the 15m boxes have escaped (refusals counted isb_tf_conflict_both_boxes). The course says "đợi nó thoát cả luôn" (wait for it to escape ENTIRELY).',
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
          what: 'D4.4 p1 @21:55–22:12 "Đợi canh nó nằm trên hai cái đường line này, kiếm insert bar đu vô… Hoặc anh chị chỉ cần đợi nó nằm ở trên cái đường line 4 giờ" (wait for price to sit above both lines and take an inside bar — or at least above the 4h line). ON (default, course-defaults-30) = on a red-folder print day, from the print window until the 08:30 CT RTH open, an intraday entry needs the last close beyond the 4h trigger line on its own side (above it for a long, below it for a short); refusals counted news_wait_4h_line. Release #61 (switch “News wait: which lines”, mentor_news_wait_mode, default both): the close must ALSO be beyond the pre-print 1h line on its side (refusal news_wait_1h_line) — the course’s first method, both lines; with no 1h line, or a 1h line older than the 4h one (“mình sẽ coi 4 giờ thôi” D4.4-R12), the 4h alone. Example: 4h 29,342.25, 1h 29,360 — a long with the close at 29,350 waits; at 29,365 it may go. OFF = no such wait (the print window itself still refuses). The 30-second ISB of the same lesson (@23:08–23:13 "Đánh inside bar lệnh long 30 giây cho em") is NOT built: NinjaTrader streams 1m bars and up, there are no sub-minute bars, and the bot does not fake them.',
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
          what: 'D4.4 p1 @18:13–18:25 "Chứ tính cái lúc trước news, chứ không phải tính cái lúc news ra rồi nha… Còn cái khúc mà news ra rồi thì không tính" (read the trigger from before the news, not from when it prints; what prints after the news does not count). ON (default) = the 1h/4h trigger read FREEZES at its pre-print values from 30 minutes before the print (07:00 for a 07:30 print — release #61, D4.4-R6, switch “News: read the 1h/4h lines before the print”, mentor_news_read_lead_min; before #61 it started at the 07:20 print window) until the 08:30 CT day-gate read — the 1h/4h candles are never rewritten (no bar is deleted), so after 08:30 the lines resume on whole candles. OFF = the item-18 print-window drop (07:20–07:35 only), byte-identical. Knob rule case (a): the course line, no ruling against.',
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
          what: 'D4.1-R34 [D4.1 p1 @20:36–21:03] "Break là sao? Là một cây nến một phút anh chị trade, nó đóng bên ngoài cái box là at least… cái thân nến nó chỉ nằm bên ngoài mà cái râu nến nằm bên trong cái box, OK bình thường không sao hết. Cái đó là at least" (what is a break? the 1-minute candle you trade CLOSES outside the box — that is the minimum; a whole candle outside is fine, a body outside with the wick inside is fine) and D3.4-R34 "nó đóng một cái nến một phút ở ngoài" [D3.4 p2 @05:37–06:01]. close (default since release #61, m466 G20/G49) = the 5m/15m/30m inside-bar rest box (and the #60 provisional forming-15m box) is escaped and deleted when a 1m candle CLOSES beyond the edge — the same candle read the PHL/PLH 5m-box ban already used, so the two gates now agree on one candle; body = the #25–#60 default (OPEN and CLOSE both beyond the edge). The FTGH/FTGL boxes keep their own body escape (D3.2-R16). A stored body still wins.',
          trader: 'close — the course minimum (release #61).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/isb_box.go ISBBoxEscapeWith · kernel/mentor/mtf_forming15.go advance15mBox',
          range: 'close / body',
          systemDefault: 'close',
          recommended:
            'close — D4.1-R34 p1 @20:36–21:03 (a close-only test satisfies every sentence of it; the body test rejects the first).',
          whenToTouch: 'Rarely — body only to restore the #25–#60 test.',
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
          what: 'G2 (oct round 2): default OFF (0). October cancels at the 4th non-filling candle unconditionally ("Lập tức cancel lệnh luôn" [OctD4.3-R57 @56:00]; the body-inside-I1 hold is its one named exception, R53 @51:00) and no October note repeats the September exception below; store 12.5 to restore it. Release #52 B8; default changed in release #60 (D5.1-R26). With a very small stop the mentor kept a resting ISB past the 4th candle: "stop loss của mình lúc đó rất nhỏ, có 12 điểm… cancel làm chi trời… Anh chị có một cái quyền: thấy stop loss nhỏ… cứ việc đặt lệnh, mặc dù sau 3 cây" [Ngày 5.1 Go over lại tất cả part2 @02:24–04:41]; the on-screen stop is 29,285.75 → 29,273.25 = 12.50 pts (50 ticks), so the September value is 12.5. An arm whose ISB stop (candle range + 2 × the 1.5-pt buffer) is at or under it is NOT cancelled at the cancel candle; it still cancels the moment a body leaves I1. Blank = 0 = off (October); 0.01 is effectively off too (no ISB stop is under 3 pts).',
          trader: 'OFF (0) — October [OctD4.3-R57 @56:00]; was 12.5, D5.1-R26.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (ISBStackTinyStopPts) · kernel/mentor/isb.go ISBStackTick',
          range: '0.01–25 pts (blank = 0 = off)',
          systemDefault: '0',
          recommended:
            'OFF — October cancels at the 4th candle [OctD4.3-R57 @56:00]. 12.5 restores the September hold (Ngày 5.1 part2 @02:24–04:41).',
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
          label: 'Trendline entry touch',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #60 (D2.3-R7, the full video read). "thà nó về một lần, rồi nó test, rồi nó hold rồi, là lần thứ ba đó, thì lần thứ tư mới dám tự tin mình vô" (better that it comes back once, tests, holds — that is the third time — and only on the fourth do I dare go in) [D2.3 p1 @02:34–02:47, 03:03–03:16]; "test lần thứ 3 thì mới valid" (D3.2-R34) [D3.2 p2 @02:43–04:11]. fourth (the stricter opt-in, the September D2.3-R7 behaviour) = on the candle of its 3rd touch a 1m/2m trendline is NOT a location — refused and counted trendline_third_touch_is_test — and it stays refused while price keeps sitting on the line; only after a candle LEAVES the line (clear of it by more than its 0.5-pt touch band) does it become a location, so the entry comes on a NEW, 4th touch (“thì lần thứ tư mới dám tự tin mình vô” [D2.3 p1 @02:34–02:47]). While held it is still a valid line, so it stays a target. third (default since m472 A2) = the line arms on its 3rd-touch candle, the return that touches it [Oct D3.2 @ 31:00-31:17, 41:20-41:27]; the October lesson says two points make a line and he enters on the contact after two bottoms. 1h/4h lines are unchanged.',
          trader:
            'third — the October course [Oct D3.2 @ 31:00-31:17, 41:20-41:27]; fourth is the stricter September opt-in.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (TrendlineEntryTouch) · kernel/mentor/trendline.go TrendlineLevelsTouch',
          range: 'fourth / third',
          systemDefault: 'third (m472 A2)',
          recommended:
            'third — Oct D3.2 @ 31:00-31:17, 41:20-41:27 (supersedes Sept D2.3 p1 @02:34–02:47).',
          whenToTouch:
            'Rarely — fourth only to restore the stricter September entry.',
          perSession: 'No.',
        },
        {
          label: 'Near-box: between two boxes exempt',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #60 (D3.2-R29/R30). The mentor refused E6 — a short ISB candle (~29,015–29,025) sitting on the top of the FTGL box 28,980.75–29,014.00 with the FTGH box drawn above it: "sát box, gần box, gần supply quá, mình không được đánh" [D3.2 p1 @22:12–23:08] — and allowed the ISB in the middle because "nó đâu có làm gì đâu" (nothing was near it) [@23:14–24:41]. OFF (default) = no blanket "between two boxes" exemption: the middle setup passes on its own room (the Near-box room card). ON = the pre-#60 exemption (one box wholly above AND one wholly below → never near). An entry inside the box that opposes its side is refused near_box_inside_opposing either way.',
          trader: 'OFF — the course.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (NearBoxBetweenExempt) · kernel/mentor/box.go nearBoxRefusal',
          range: 'true / false',
          systemDefault: 'false (off)',
          recommended:
            'OFF — D3.2 p1 @22:12–24:41 (E6 refused, the middle case judged on its own).',
          whenToTouch: 'Rarely — ON only to restore the pre-#60 exemption.',
          perSession: 'No.',
        },
        {
          label: 'PHL flat low',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #60 (D2.2-R64/R65). "đang đi kiếm một cái lệnh có đáy cao hơn đáy trước đó; mà ngay chỗ này nó ngang phè" (we look for a low higher than the previous low; here it is dead flat) [D2.2 p3 @07:44–09:46]; "Cái chỗ này anh đang gọi nó là failure to go lower" [@09:46–10:15] — on screen the pink box 29,369.00–~29,380.25. flat_near (default) = a PHL whose low sits above the prior low by no more than the day’s "same price" distance (half the average high-low range of the last 20 closed candles of the traded frame — the owner’s m428 R2 measure, no new number) is refused phl_flat_low_is_ftgl; the mirror refuses a PLH on a flat high (plh_flat_high_is_ftgh). The flat pair is already a flat support/resistance level, so the FTGL reading of that place still trades. strict = the pre-#60 strict comparison only.',
          trader: 'flat_near — the course.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (PHLFlatLow) · kernel/mentor/phl.go phlFlatExtremeRefusal',
          range: 'flat_near / strict',
          systemDefault: 'flat_near',
          recommended: 'flat_near — D2.2 p3 @07:44–10:15.',
          whenToTouch: 'Rarely — strict only to restore the pre-#60 check.',
          perSession: 'No.',
        },
        {
          label: 'Reverse ISB: EMA 9 touch',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #60 (D5.4-R7). "nó không cần đụng, nó cứ đụng vòng vòng ngay EMA 9 là được … không quan trọng là nó đụng ngay tại EMA 9 hay là nó lố EMA 9" (it need not touch exactly — hovering around the EMA 9 is enough, at it or overshooting it) [D5.4 p1 @03:35–03:46]; on screen the 09:45–09:48 candles straddle the line as a group. pair (default) = the EMA 9 must lie inside the mother + ISB candles’ combined range; isb_candle = the pre-#60 read (inside the ISB candle’s own range). A group that never reached the line is still refused.',
          trader: 'pair — the course.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (ISBReverseTouch) · kernel/mentor/isb_reverse.go ReverseISBAtEMA9',
          range: 'pair / isb_candle',
          systemDefault: 'pair',
          recommended: 'pair — D5.4 p1 @03:35–03:46.',
          whenToTouch: 'Rarely — isb_candle only to restore the pre-#60 read.',
          perSession: 'No.',
        },
        {
          label: 'Key level: delete on a 1H body close',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.key_level_delete_on_1h_close)',
          what: 'Levels audit LL-1. A key level is the OPEN of the 1H RTH candle where the candle colour changes (slide 31; Oct D5.2-R9). The deck (slide 31 step 4, September) deleted a level for good once a later 1H RTH candle’s BODY closed through it. October does not: the mentor walks the 1h RTH chart back and draws a line at every colour change — “vẽ bao nhiêu tùy thích… Key Level nó là quá khứ” [OctD5.2-R13..R16 @04:10–07:12] — and his final stack on 11-03 (frame s_00333, 20:38 CT) holds lines a body crossed long before, e.g. 29,466.50 (crossed by the 10-29 14:30 candle) and the WE3 quiz level 27,997.75, which a 1H body crossed on 07-29 [OctD5.2-R19/R20 @07:20–07:42; WE3 @09:12–10:05; a quiz, not a trade]. What the course states is “draw as many as you like”; it never says a body close deletes a level or that it does not, so keeping them is an inference [B] from his drawn stack (open mentor question). off (default) = a body close leaves the level drawn; the 20-pt keep-the-newer prune still applies, so two lines are never closer than 20 pts. on = the deck deletion. Over the 11-04 seed the bot had deleted 11 of the mentor’s lines with on; with off it keeps every colour-change line the prune keeps (e.g. 89 instead of 28 from 07-20 on). What it costs: over the 90-day replay of a5b7662e (levels audit) the denser set refuses more entries through the existing rules that read the NEXT key level — the X4 key-pair candle (keypair_candle_too_big 1,858 → 6,385), the ISB room rule (room 1,321 → 1,913) and the 1:1 target floor (isb_target_below_floor 2,170 → 2,876) — so entry orders fall from 3,080 to 1,719 (ISB 2,020 → 963); with on, 3,052. It is also DENSER than the mentor: between 29,000 and 30,150 on 11-03 the bot keeps 39 lines with off, 8 with on, and his chart shows 14 — he does not draw every colour change the 20-pt prune keeps (open mentor question).',
          trader:
            'off — October [OctD5.2-R13..R20]. Mentor question asked (LL-1).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (KeyLevelDeleteOn1HClose) · kernel/mentor/eval.go Tick → advanceKeyLevelDeletion · kernel/mentor/seed.go seedHistoryState',
          range: 'on / off',
          systemDefault: 'off',
          recommended: 'off — October wins over the deck [OctD5.2-R13..R20].',
          whenToTouch:
            'Only on a mentor answer — on restores the deck / September deletion.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'PHL/PLH room multiple',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.phl_room_multiple)',
          what: 'Oct round 2 D2-G2, default moved by answers-1112 Q2 (verdict confirmed, high). The room rule asks how far the target must be from the entry, as a multiple of the stop. September (Sept D5.3 METHOD s7) asked 2x for every setup. October asks 1:1 for a PHL/PLH: “khi em đặt stop-loss 40 điểm em cũng phải kiếm cái target 40 điểm … Risk reward 1-1 … at least là cũng 35 điểm” (a 40-pt stop needs a 40-pt target) [Oct D2.3 @49:37–49:56, frame s_02972]. The worked trade shows it: WE10 (Thu 29 Oct ~19:53, PLH short at 28,980) first draws a 29-pt target (R:R 3.31, frame s_04120), then he drags it in to the old low and takes Stop 8.75 / Target 15.00 / R:R 1.71 (frame s_04126). The “1,2” at 27:33 is one trade’s result (the tool on s_01661 reads 1.8), not a minimum. 1 (default) = a PHL/PLH needs a target at least as far as its stop; WE6, WE7 (1.14R) and WE10 (1.71R) are no longer refused room / rolling_room. A target inside 1R is still refused by the 1:1 floor. A confluence PHL/PLH asks 1 × its 2R leg 1 = 2R at the default (“at least là risk reward 1-2” [Sept D3.4 p3 @07:38–08:09], the report’s Q2 final setting) and 4R at 2. 2 = the September 2R room (today before this change). The ISB, box and swing paths keep the 2x room_multiple. The near-box skip on a PHL/PLH is a separate 2x — see “Near-box room for PHL/PLH”.',
          trader: '1 — October [Oct D2.3 @49:37–49:56; WE10 s_04126].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (PHLRoomMultiple) · kernel/mentor/phl.go phlPLHR2',
          range: '0–10',
          systemDefault: '1',
          recommended:
            '1 — October [Oct D2.3 @49:37–49:56 “Risk reward 1-1”; WE10 R:R 1.71 taken].',
          whenToTouch: 'Only to restore the September 2R room: set 2.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Near-box room for PHL/PLH',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.near_box_room_phl_multiple)',
          what: 'answers-1112 Q2 — an owner question. A PHL/PLH whose nearest box edge in the trade direction is closer than this × the reference candle’s range is refused near_box. The course shows the “sát box” skip on an ISB only [Ngày 3.2 FTGH FTGL part1 @00:22:44–00:23:10] and October asks a PHL/PLH for 1:1 room [Oct D2.3 @49:37–49:56], so the 2x on a PHL/PLH is ours, not the course. Blank (default) = follow the “Near-box room” card (2 today) — unchanged behaviour. An explicit value sets the PHL/PLH leg only (the ISB keeps “Near-box room”); 0 turns the PHL/PLH leg off, the same as switching off “Near-box skip also gates PHL/PLH”.',
          trader:
            'blank — follows Near-box room (2×); ours, the owner decides.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (NearBoxRoomPHLMultiple) · kernel/mentor/eval.go phlNearBoxRefuses',
          range: '0–10 × (blank = follow Near-box room; 0 = off)',
          systemDefault: 'blank (follows Near-box room = 2)',
          recommended:
            'blank (today) until the owner rules — no course number exists for a PHL/PLH box room; 1 would match the October 1:1 room.',
          whenToTouch:
            'Only on an owner ruling — set 1 to give a PHL/PLH the same 1:1 room near a box, 0 to drop the skip for PHL/PLH.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'ISB first target is the fill candle close',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.isb_target_fill_close)',
          what: 'Oct round 2 D2-G3. OctD2.2-R35/R37 [Oct D2.2 @28:40, 30:03]: the ISB gives its own first target — sell part when the trend candle that fills the order closes (“target đầu tiên… bán bớt… khi cây nến trend phiêu lệnh… đóng”); R34 [@28:12] puts level targets (tops and bottoms) aside as “not taught yet” and the 1:1 theory with them. ON (knob; default OFF — the course does not say an ISB with a level inside 1R may be traded, [B]): an ISB with no level beyond its entry (isb_missing_target) or only a level inside 1R (isb_target_below_floor) is no longer refused. Since answers-1112 the no-level case is ALSO traded by default through “ISB with no level beyond is traded” (isb_no_level_fill_close, default on), so this knob’s own remaining job is the level-inside-1R case; its first target is the +1R leg-1 take-profit the trader already owns (partial at the fill candle’s close), the runner keeps the level ladder, and the 2R room-to-a-level rule has no level to measure. Replay 30 Oct 13:26 CT (mother 13:25 red, ISB 13:26): refused isb_target_below_floor before, sell stop 28,307.50 fills 13:27 and the low is 28,240.50 by 13:35. OFF (default) = the September rule: a level target at least 1R away with 2R room.',
          trader: 'off — today; on is an owner/mentor call [B].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (ISBTargetFillClose) · kernel/mentor/eval.go Tick (ISB target stage)',
          range: 'on / off',
          systemDefault: 'off',
          recommended:
            'off until the mentor confirms [OctD2.2-R35/R37 @28:40, 30:03 say only the first partial; R34 says real targets come later; Oct D2.1-R13 level to level is the argument for the ladder].',
          whenToTouch:
            'Only on a mentor answer — set true to trade a no-target ISB at +1R.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'ISB with no level beyond is traded (fill candle close)',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.isb_no_level_fill_close)',
          what: 'answers-1112 Q3 (verdict corrected, confidence medium). Oct D2.2 [@28:37-28:56]: “target đầu tiên của mình mà mình phải bán bớt… là khi cây nến trend phiêu lĩnh của mình… Đóng” — the first target, where you sell part, is when the candle that fills the order closes; [@30:24-30:43] “Target của Insha Bar là khi nến trend… phiêu lệnh… Đóng thì mình sẽ bán bớt, không quan tâm nó làm gì hết” (no matter what it does next); Sept D1.4 p1 [@12:12-13:27] “Bắt buộc” (mandatory). The opposing line is Sept D4.1 p1 [@01:39-02:02] “một cái setup là anh chị provide … 3 thứ … entry, stop loss, target … không có một trong ba … Bắt buộc lúc nào cũng phải đợi” (a setup provides entry, stop and target; missing one, wait) — the line the old refusal cites; for an ISB the fill candle’s close IS that target, so the setup still has all three. ON (default): an ISB with NO level beyond its entry is traded — its first target is the +1R leg-1 take-profit the trader owns, pulled in to the fill candle’s close when +1R is not reached inside that candle (mentorISBPartialTP), and the room-to-a-level rule has no level to measure. Worked example (Oct D2.2 @39:00-39:59, frame s_02392, Fri 30 Oct ~08:37): short ISB, entry at the NC3 low 28,709.50, stop at its top 28,735.75 (26.25 pts — “Stop Loss của anh chị là 27 điểm”), first target 28,683.25 (“đợi 27 điểm, anh chị bán”) or the fill close (“Hoặc ngay tới đây, anh chị bán bớt”). It fires ONLY when no target level stands beyond at all: a level inside 1R stays refused isb_target_below_floor (isb_target_fill_close owns that case, default off), and a 4h-trigger or 5m-wick level skipped for room is still a level ahead (isb_missing_target, as before). OFF = today’s isb_missing_target refusal exactly.',
          trader: 'on — the October rule; off reverts.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (ISBNoLevelFillClose) · kernel/mentor/eval.go Tick (ISB target stage, targetLevelBeyond)',
          range: 'on / off',
          systemDefault: 'on',
          recommended:
            'on [Oct D2.2 @28:37-28:56, @30:24-30:43; Sept D1.4 p1 @12:12-13:27]. What to do when the fill candle closes against you is deferred in the course (Oct D2.2 @30:03-30:13 “ngày mai mình sẽ được học”); the +1R TP keeps resting then.',
          whenToTouch:
            'Set false only to get back the old refusal of an ISB with no level beyond (isb_missing_target).',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Trigger lines are read from the 08:30 open',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.trigger_from_open)',
          what: 'Oct round 2 G4. OctD4.3-R5 [Oct D4.3 @03:10]: the 5m trend is judged from the Open to the current bar — “từ lúc Open ra… từ cái mảng đen nha chứ không phải tính cái mảng xanh” (the black RTH zone from 08:30 CT, not the blue overnight zone). ON (default): at the 08:30 bucket the 5m (and 15m/30m) trigger line is emptied and that candle only becomes the reference for the next one, so the open candle never breaks the 08:25 Globex candle. Fri 30 Oct: 08:25 L 28,788.25, 08:30 L 28,684.50 — the old read drew a sell line at 28,788.25 from 08:30 (shorts allowed anywhere under it, ~104 pts above the RTH read); the RTH read has no line until 08:35 (L 28,668.25) draws the sell line at 28,684.50. The 1h/4h lines are not re-anchored. OFF = the whole-history read.',
          trader: 'on — October [OctD4.3-R5 @03:10].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (TriggerFromOpen) · kernel/mentor/filters.go triggerTickAt / isRTHOpenBucket',
          range: 'on / off',
          systemDefault: 'on',
          recommended: 'on — October [OctD4.3-R5 @03:10].',
          whenToTouch:
            'Only to restore the overnight-reference read: set false.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Rolling ladder ends on a close through the left extreme',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.rolling_left_break_cancel)',
          what: 'Oct round 2 D2-G6. The mentor’s cancel demo (Thu 29 Oct, WE12): the old-high ray 28,876.50 sits on the 20:57 swing high; a green candle closes above it (the next bar opens 28,880.25) and he says “nó break cái đỉnh này rồi, thì [lower high] gì tầm này nữa” — there is no PLH any more [OctD2.3-R69 @78:10; frames s_04713, s_04718, s_04720] (mirror for a PHL under its higher low). on (default, October) = a 1m candle that CLOSES beyond the ladder’s left extreme (the lower high / higher low the leg started from) ends the rolling ladder: the resting stop is cancelled and the drop is counted rolling_left_extreme_broken; a wick that closes back inside is not a break, and a new leg reads a new left extreme. off = the earlier behaviour: each rung is refused phl_not_lower_high / phl_not_higher_low on its own and the ladder restarts on the next counter-trend candle.',
          trader: 'on — October [OctD2.3-R69].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (RollingLeftBreakCancel) · kernel/mentor/rolling_phl.go rollingPHLTick',
          range: 'on / off',
          systemDefault: 'on',
          recommended: 'on — October [OctD2.3-R69 @78:10].',
          whenToTouch: 'Only to restore the per-rung refusal: set false.',
          perSession: 'No — per strategy.',
        },
        {
          label:
            'Rolling ladder: a touch of an earlier leg does not own the entry',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.rolling_stale_touch_releases)',
          what: 'Fix 1112 B (WE6, Tue 27 Oct). A level touch-reject normally owns the entry (the level path, first touch fixed) and the rolling pullback ladder stands aside. WE6’s pullback [OctD2.3 @13:37–28:33] was blocked all the way by a 1H level (29,668.50) that price had rejected at 08:40 — an hour before the 09:36 old high the ladder targets. on = a touch candle older than the old extreme the ladder targets belongs to an earlier leg and no longer owns this one; a touch inside the pullback still owns it. off (default) = today: any live touch-reject owns the entry. Default OFF: after this gate WE6 is still refused by the 5m box ban / mid-range ban, so the worked example is not reproduced end to end and the flip waits for the mentor. (answers-1112 Q9: WE6 is a long inside a LONG 5m box, so “PHL/PLH inside the 5m ISB box: same direction as a scalp” lifts the box ban for it when on; the mid-range ban stays.)',
          trader: 'off — default until the mentor rules on the box ban.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (RollingStaleTouchReleases) · kernel/mentor/rolling_phl.go rollingPHLTick',
          range: 'on / off',
          systemDefault: 'off',
          recommended:
            'off — today [OctD2.3 @13:37–28:33, @35:46–39:09 worked examples; not yet reproduced end to end].',
          whenToTouch: 'To trial the fix: set true.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Rolling ladder: no keypair sit-out on the tape old extreme',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.rolling_keypair_tape_exempt)',
          what: 'Fix 1112 B (WE7, Fri 30 Oct). The keypair sit-out (a candle as big as the gap between two key levels, X4) paired the rolling ladder’s old low 28,288.75 — a 1m tape swing — with a 1H level 5.75 pts below and refused the 13.5-pt 13:25 candle, the exact rung the mentor sold [OctD2.3 @35:46–39:09]. X4 pairs two key levels he drew. on = the sit-out is not applied when the anchor is the ladder’s tape old extreme (key-level touches keep it). off (default) = today. Default OFF: after this gate WE7 is still refused by the 5m box ban and the 2R PHL room multiple. (answers-1112: the room is 1:1 since Q2, and WE7 is a short inside a SHORT 5m box, so “PHL/PLH inside the 5m ISB box: same direction as a scalp” lifts the box ban for it when on — Q9.)',
          trader:
            'off — default until the mentor rules on the box ban and the room.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (RollingKeypairTapeExempt) · kernel/mentor/eval.go phlLocationBan',
          range: 'on / off',
          systemDefault: 'off',
          recommended:
            'off — today [OctD2.3 @13:37–28:33, @35:46–39:09 worked examples; not yet reproduced end to end].',
          whenToTouch: 'To trial the fix: set true.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'HTF trendline: broken line flips, never deleted',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.htf_trendline_flip)',
          what: 'Oct round 2 D3-G12. Oct D3.2: “khung giờ lớn mình để luôn, mình không bao giờ xóa” [OctD3.2-R20 @08:27] — a 1H/4H line is never deleted; the candles under a broken line see it as resistance, “đụng resistance thì drop” [R29 @14:18–14:47]; R32 [@20:20] the Friday long at the 4H falling line. ON (default): a persistent 1H/4H line broken by a closed 5m candle wholly beyond it is kept with its role flipped (a broken support is resistance and the reverse) and is a location again after price returns and touches it. OFF: the m428 delete. The 1m lines still die when broken. The seed already builds the HTF lines from the stitched full history.',
          trader: 'on — October [OctD3.2-R20/R29].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (HTFTrendlineFlip) · kernel/mentor/eval.go htFTrendlines · trendline.go flipHTFTrendline',
          range: 'on / off',
          systemDefault: 'on',
          recommended: 'on — October [OctD3.2-R20 @08:27].',
          whenToTouch: 'Only to restore the delete-on-break: set false.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Trendline: maximum steepness',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.trendline_max_slope_pts_per_bar)',
          what: 'Oct round 2 D3-G14. Oct D3.2 [@29:12–29:38]: a trendline at 30–45 degrees is good, at 80–85 degrees it is a slide (“cầu tuột”) and is not drawn. Degrees depend on the chart scale, so the bot needs a points-per-bar number: N pts per build-frame bar — a line steeper than N is refused. Ships OFF (0); the course states no number.',
          trader:
            '0 (off) — the course gives degrees, not pts/bar; mentor question.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (TrendlineMaxSlopePtsPerBar) · kernel/mentor/trendline.go newSlopedLineMax',
          range: '0–1000 (0 = off)',
          systemDefault: '0',
          recommended:
            '0 (off) — the course states no number [OctD3.2-R35/R36 @24:00; R40/R41 @29:12].',
          whenToTouch: "On a mentor answer — 0 is today's behaviour.",
          perSession: 'No — per strategy.',
        },
        {
          label: 'Trendline: minimum bounce',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.trendline_min_bounce_pts)',
          what: 'Oct round 2 D3-G10. Oct D3.2 [@03:13, 24:00, 24:34]: a low after which price only drifts sideways is rejected (“đi ngang phè”), a bottom counts only if price bounced up a lot, take the clearest, and if it is not clear do not draw. N points: a swing anchors an intraday trendline only when price bounced at least N from it before the next same-role swing. The course states no number, so it ships OFF (0). Sun 1 Nov: the 17:25 low 28,643.25 bounced 16 points, drifted, then dumped to 28,621 — the line drawn from it is the kind the mentor rejects.',
          trader:
            '0 (off) — the course states no bounce size; mentor question.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (TrendlineMinBouncePts) · kernel/mentor/trendline.go swingBounced',
          range: '0–1000 (0 = off)',
          systemDefault: '0',
          recommended:
            '0 (off) — the course states no number [OctD3.2-R35/R36 @24:00; R40/R41 @29:12].',
          whenToTouch: "On a mentor answer — 0 is today's behaviour.",
          perSession: 'No — per strategy.',
        },
        {
          label: 'Knock Knock counts per zone, pre-market included',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.knock_knock_zone)',
          what: 'Oct round 2 D2-G10. Oct D2.1 [@21:18–23:43]: “người ta sẽ không còn muốn vô ở những lần sau cái lần thứ 3”, “lần thứ 4 là … có khả năng break nhiều nhất”, “resistor mở lên nó test 3 lần, 4 lần … đừng vô short ngay chỗ đó nữa” — and the tests before the open count (frame s_01351: the 28,816.25 line drawn from the far left, 07:37). ON (default): the tests are counted per PRICE ZONE (a horizontal S/R re-keyed at slightly different prices is one zone) and the zone table lives until the 17:00 roll, not the 08:30 latch; the 4th test of a zone is refused (level_visit_cap). OFF: the per-level-key count that resets at 08:30.',
          trader: 'on — October [OctD2.1-R22/R25/R34].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (KnockKnockZone) · kernel/mentor/knock_zone.go',
          range: 'on / off',
          systemDefault: 'on',
          recommended: 'on — October [OctD2.1-R34 @23:24].',
          whenToTouch: 'Only to restore the per-level-key count: set false.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Trendline: first-bottom anchor',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.trendline_first_bottom_anchor)',
          what: 'Oct round 2 D3-G9. A trendline joins the FIRST bottom of the move to the later higher bottoms — “hai cái đáy, rồi giá quay lại cái đường” [OctD3.2-R59 @41:20], “nối 2 cái đáy lại rồi kéo dài ra” [R46 @33:00], the first anchor is the first bottom of the move [R45 @31:20]. EX7/EX10 (Sun 1 Nov): 17:07 28,595.00 → 17:29 28,612.00, touched at 18:32, 18:40 and about 20:00. The 17:29 low is LOWER than the swing lows between, so the consecutive pair cannot form that line. ON (default): the non-adjacent slot is refilled from the same first anchor (the new leg replaces the old one). OFF: consecutive pairs plus the one non-adjacent line only.',
          trader: 'on — October [OctD3.2-R45/R46/R59].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (TrendlineFirstBottomAnchor) · kernel/mentor/trendline.go trendlinesBuildFrameB5Opts',
          range: 'on / off',
          systemDefault: 'on',
          recommended: 'on — October [OctD3.2-R59 @41:20].',
          whenToTouch: 'Only to restore the consecutive-pair pick: set false.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Box flip is reversible',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.box_flip_reversible)',
          what: 'Oct round 2 D3-G7. After a body closes through an FTGH the mentor gives two ways [OctD3.3-R46 @37:40]: an ISB long, or wait — “đợi nó quay về nó bắt test, nó đóng ngược lại level, thì level này lại tiếp tục là Resistance”; R48 [@39:20] lets it drop, retests the box from below and sells (polyline 28,802.75 → 28,731.25 → 28,812.50 → 28,686.25). ON (default): a later candle whose whole body is back on the original side of the box (below an FTGH’s bottom, above an FTGL’s top) restores the role, so the retest short is reachable. OFF: the September D14 flip is permanent for the box’s life.',
          trader: 'on — October [OctD3.3-R46/R48].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (BoxFlipReversible) · kernel/mentor/box.go boxFlipped',
          range: 'on / off',
          systemDefault: 'on',
          recommended: 'on — October [OctD3.3-R46 @37:40].',
          whenToTouch: 'Only to restore the permanent Uno flip: set false.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Carried box stays live',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.box_carried_stays_live)',
          what: 'Oct round 2 D3-G4. A box standing at the 08:30 open is valid until the next open — “vẫn còn valid trên chart Chủ nhật” [OctD3.3-R27 @17:00], “valid cho tới sáng ngày mai” [R51 @41:28]. E9: Sun 1 Nov about 21:53 price retests the FRIDAY FTGH 28,808.25 and the course shorts it for 17.50 pts. ON (default): the carried box stays the role’s live box over any box that forms later in the evening (m473 let a Sunday-evening box hide it, and the bot went long from flipped evening boxes at 21:55–21:56). OFF: a carried box only fills a role the live set lacks.',
          trader: 'on — October [OctD3.3-R27/R51].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (BoxCarriedStaysLive) · kernel/mentor/box_validity.go carriedBoxesAt',
          range: 'on / off',
          systemDefault: 'on',
          recommended: 'on — October [OctD3.3-R27 @17:00].',
          whenToTouch:
            'Only to restore the m473 fill-an-empty-role rule: set false.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Box: a big outside bar is both a peak and a bottom',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.box_swing_both_roles)',
          what: 'Oct round 2 D3-G2. On Thu 29 Oct the 09:05 candle (O 29,619, H 29,629.25, L 29,586, C 29,588.50) is the highest of its neighbours AND the lowest, and it is peak 2 of the course’s red FTGH 29,621.25–29,632.75: “lấy cái râu cao nhất của cái đỉnh 1, kéo xuống cái thân gần nhất của cái đỉnh 2” [OctD3.3-R25 @15:46; frame s_02696]. ON (default): such a bar counts as a swing high and a swing low in the box builder, so the box forms at 09:06 (29,619.00–29,629.75). OFF: the September switch keeps it as a low only and the pair falls to the 09:19 high (29,605.75–29,629.75, formed 09:20).',
          trader: 'on — October [OctD3.3-R25].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (BoxSwingBothRoles) · kernel/mentor/box.go swings3Opt',
          range: 'on / off',
          systemDefault: 'on',
          recommended: 'on — October [OctD3.3-R25 @15:46].',
          whenToTouch: 'Only to restore the one-role swing switch: set false.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Box pair: pre-open peaks count',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.box_preopen_lookback_min)',
          what: "Oct round 2 D3-G1. The course's own FTGH box (Fri 30 Oct, Oct D3.3) is drawn from two PRE-OPEN peaks — peak 1 about 08:00 (high 28,818.25) and peak 2 about 08:29 (body top 28,812.50) — and neither is the session extreme: higher swings stand to the left (06:58, the overnight 03:59 wick). “Bên tay trái mình có thấy một cái đỉnh, bên tay phải … 1 cái đỉnh … Mình vẽ được 1 cái box” [OctD3.3-R23 @17:13]; “bên đây mình đã vẽ được rồi thì nó sẽ valid cho tới sáng ngày mai” [R51 @41:28]; “trước khi Market Open tầm 10 phút thì em coi có … nếu mà nó có thì em vẽ” [R52 @42:00]. N minutes (default 10, R52): a pair of pre-open peaks that forms in the last N minutes before 08:30 CT is the morning box and replaces the overnight pair — a pair that formed earlier (Thu 29 Oct 08:11) does not; 0 = only boxes formed from 08:30 replace it (the earlier behaviour).",
          trader: '10 — October [OctD3.3-R52 @42:00 “tầm 10 phút”].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (BoxPreOpenLookbackMin) · kernel/mentor/course_exits.go boxBuildBars / freshRTHReplaces',
          range: '0–240 minutes (0 = off)',
          systemDefault: '10',
          recommended: '10 — October [OctD3.3-R52 @42:00].',
          whenToTouch: 'Only to restore the build-from-08:30 read: set 0.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'PHL/PLH minimum distance from the old low',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.phl_min_above_old_extreme_pts)',
          what: 'Oct round 2 G7. Oct D4.2 [@36:45–40:20]: the PHL entry must be above the old low it stands on — “nên cho nó cao hơn tầm 15, 15 điểm” [R41] — and in the Sun 01 Nov example an entry 6.50 pts above the old low “em cũng không gọi nó là cái Potential High Low” [R42]. The mentor calls the 15 experience, not a hard law [R39]. 15 = a PHL/PLH (level touch or rolling ladder) whose entry is less than 15 pts from the old low it stands on (PLH: old high) is refused phl_min_above_old_extreme. The Oct D2.3 worked trades (WE6, WE7, WE10) all still arm with it on. 0 (default) = off, the earlier behaviour (only the flat-low and higher-low checks): the mentor calls 15 experience, not law [R39], so it ships as a knob until he rules.',
          trader:
            '0 (off) — the mentor calls 15 experience [OctD4.2-R39]; mentor question.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (PHLMinAbovePriorPts) · kernel/mentor/phl.go phlPLHR2',
          range: '0–100 (0 = off)',
          systemDefault: '15',
          recommended: '15 — October [OctD4.2-R41 @40:14].',
          whenToTouch: 'On a mentor answer — 0 restores the earlier behaviour.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Key level: break and retest',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.key_level_break_retest)',
          what: 'Levels audit LL-2. When the FIRST candle that touches a key level closes THROUGH it, September made the level invalid — ISB-only for the rest of the session [D5.2 p1 @19:44–20:25]. October trades that break: “khi nến nằm ở dưới đường line level này, nó breakdown… Level này đang là kháng cự” [OctD5.2-R20 @07:42]; worked example at 28,154.50 on 11-03 [R24 @13:35]: the red 18:26 candle closes below the support, then “nó quay ngược lên, nó bắt test… vẫn là nến chạm level… Thứ 2 nữa là nến đóng dưới” — and the sell stop goes on the FIRST retest candle that touches [R25 @14:16]; the only checks are the touch, then where the candle closes [R22/R23 @12:16, 12:29]. on (default) = that first-touch break cancels the resting order, flips the level’s role and starts a new visit (counter level_broken_retest, a note names the level); the next candle that touches from the new side and closes back on it is the reference, and the PHL/PLH runs through every normal gate (on the 11-03 example the 18:29 retest candle is 40 pts tall, so the 25-pt stop ceiling names it phl_stop_ceiling). One flip per level per session day, shared with the Uno Reverse of a level that held (uno_key_levels): a second close through after the flip is ISB-only for the day. off = the September ISB-only rule. Over the 91 replay days 260 of 539 first-touch breaks were followed by exactly this retest (115 in RTH).',
          trader:
            'on — October [OctD5.2-R20/R24/R25]. Mentor question asked: how many retests count after one break (not in the course; the bot reads one flip per day).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (KeyLevelBreakRetest) · kernel/mentor/uno_key_level.go unoKeyLevelBreak → levelFlip',
          range: 'on / off',
          systemDefault: 'on',
          recommended: 'on — October [OctD5.2-R20/R24/R25 @07:42–14:16].',
          whenToTouch:
            'Only on a mentor answer — off restores the September first-touch ISB-only rule.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Key level: re-arm after the run',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.key_level_rearm)',
          what: 'Oct round 2 D3-G15. A key level that went ISB-only — its second wrong-way close after the one flip of the day (break_retest / uno_key_levels), the September “once invalid, only inside bars” [D5.2 p2 @20:48] — stays ISB-only until the day rolls. October D3.2 says: if there is no inside bar after the hold, “đợi cho nó chạy, xong nó về đây nó test lại cái level này và nó một lần nữa nến đóng trên” — wait for the run, the retest of the level and one more close on the right side, then the entry [OctD3.2-R78 @ 60:00, marked changed vs September]. off (default, today) = unchanged. on = the level behaves like a trendline: the invalidity clears when price DEPARTS (a candle that no longer touches the level from the visit’s approach side), so the retest that closes back on the right side is a fresh first touch and can arm a PHL/PLH through every normal gate; a wrong-way close on that new visit is ISB-only again until the next departure. Ships OFF [B]: the passage is garbled ASR (“unsure on words”) and its drawn sketch is a TRENDLINE (28,786 → ~28,774 → 28,795), which the bot already re-arms; extending it to static key levels is an inference. Moving lines, box edges and old highs/lows keep their own rules.',
          trader:
            'off — today. Mentor question: does a key level that closed through twice re-arm after the run and the retest?',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (KeyLevelRearm) · kernel/mentor/eval.go Tick touch loop (rearm) · kernel/mentor/uno_key_level.go keyLevelRearmOn',
          range: 'on / off',
          systemDefault: 'off',
          recommended:
            'off — today; the course passage is OctD3.2-R78 [@ 60:00], a [B] reading (not from the course for key levels).',
          whenToTouch:
            'Only on a mentor answer — on re-arms an ISB-only key level on departure.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Reverse ISB: trend from the EMA lines',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.isb_reverse_trend)',
          what: 'Oct round 2 D5.3-R40/R43/R44 (+ reread OctD5.3-RR2-1). At the EMA 9 the market offers two orders — the inside bar for the long, or the potential lower high for the short — and “lúc nào cũng chọn cái hướng theo trend” [OctD5.3-R40 @17:33, R44 @20:44]. The trend is the lines: an uptrend is price above all four — fish red/white, EMA 34, EMA 9 [OctD5.3-R43 @20:00]. ema (default, October) = the reverse ISB trades WITH that trend: EMA 9 above the EMA 34 and price above the 34 = long, the mirror = short; the fish is not built, so the two lines the bot has. Price between the lines (EMA 9 below the 34 with price above it, or the reverse) reads no trend and no reverse order. The 5m trigger side is NOT consulted, and the ISB rest-box gate (5m/15m/30m) tests the side of the ORDER against the box direction. Worked example Tue 27 Oct 10:10 CT: price above the EMA 34 (~29,688) on the EMA 9 (~29,722), the 1m ISBs point short, the 5m trigger flipped SELL — the mentor takes the LONG; the bot used to read the SELL trigger as the trend and emitted nothing (now a buy stop at the ISB high + the 1-pt buffer, 29,721.75). trigger = today: the governing 5m trigger’s side is the trend.',
          trader:
            'ema — October [OctD5.3-R40/R43/R44]. The fish line is absent, so the EMA 9/34 alignment stands in for the four-line test; the course warns against making the four-line test a gate for other setups [D3.1-R4], so only the reverse ISB reads it.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (ISBReverseTrend) · kernel/mentor/eval.go reverseISBTrend / reverseISBBoxAllows · kernel/mentor/isb_reverse.go emaAlignedTrend',
          range: 'ema / trigger',
          systemDefault: 'ema',
          recommended: 'ema — October [OctD5.3-R43 @20:00].',
          whenToTouch:
            'Only to restore the 5m-trigger read — trigger is today’s behaviour.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Counter-5m scalp: reduced size',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.counter_5m_reduce_size)',
          what: 'Oct round 2 D5.4-SCALP (minimal). School 1 lets the bot enter against the 5m trigger when the 1m is at its spot (the 1m EMA 34, a 1m key level). October calls that a scalp: “giảm size lại” [OctD5.4-R11 @04:47], “ăn ít lại” [R13 @05:00]. on (default) = a PHL/PLH entry (incl. the reverse ISB read as a PHL/PLH) on the side OPPOSITE to the governing 5m trigger is stamped counter_5m and sizes the reduced tier (mentor_reduced_contracts); the course names no count. off = today (the same table as a with-trigger entry). NOT built: the leg-1 cap below 1R, an entry AT the EMA, and the 30-second bars the mentor uses (no sub-minute feed).',
          trader:
            'on — October [OctD5.4-R11/R13]. The reduced count (3) is the course’s “giảm size” tier from D4.1, not a number the D5.4 lesson states.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (Counter5mReduceSize) · kernel/mentor/eval.go stampCounter5m · trader/mentor_mode.go mentorContractsFor (counter_5m)',
          range: 'on / off',
          systemDefault: 'on',
          recommended: 'on — October [OctD5.4-R11 @04:47].',
          whenToTouch:
            'Only to restore the single size table — off is today’s behaviour.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Key level: history from the current contract',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.key_level_history_source)',
          what: 'Levels audit LL-4. The key-level walk reads the 1H RTH candles back through every stored contract. Before LL-4 every candle older than the roll (MNQ 09-26 → 12-26 on 09-11/09-14) was the OLDER contract shifted by ONE measured roll gap (291.0). The mentor draws on MNQZ2026’s own chart, scrolling back to the late-July lows [OctD5.2-R16 @06:18] — 28,154.50, 27,817.75, 27,997.75, 27,753.25 — and the basis drifts: the 07-29 10:30 candle is GREEN on MNQ 12-26 (O 27,825.50 C 27,831.25, a level where he drew 27,817.75) and RED on MNQ 09-26 + 291 (no level at all); 07-29 13:30 is 28,060.00 on 12-26 (he drew 28,062.25). own (default) = every pre-roll candle the current contract’s native bars cover COMPLETELY is rebuilt from them — every 1m bar, else every 15m quarter, else both 30m halves (the 14:30 candle ends 15:15, so it is never read from 30m); anything not fully covered keeps the back-adjusted candle, so there is never a hole. The boot log names how many candles came from each source (“key-level history: N pre-roll 1H RTH candles rebuilt from MNQ 12-26’s own bars …”). stitched = every pre-roll candle back-adjusted (pre-LL-4). [B] Part of the drift is thin back-month prints — but those are the prints the mentor’s chart shows.',
          trader: 'own — the mentor’s own chart [OctD5.2-R16 @06:18].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (KeyLevelHistorySource) · trader/mentor_tick.go mentorFullKeyLevelHistory → kernel/mentor/key_level_own_history.go OwnContractKeyLevel1H',
          range: 'own / stitched',
          systemDefault: 'own',
          recommended: 'own — OctD5.2-R16 @06:18.',
          whenToTouch:
            'Rarely — stitched only to restore the pre-LL-4 back-adjusted history.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Key level: Uno Reverse after a hold',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #61 (G2, D1.3-R21). "Kháng cự bị phá → sẽ thành hỗ trợ khi backtest / Hỗ trợ bị phá → sẽ thành kháng cự khi backtest" (broken resistance becomes support on the backtest, and the mirror) [D1.3 p1 @12:31–12:58]; his drawing: the level 29,104.25 bounces, breaks, then is backtested from below and rejects. on (default) = a KEY level that HELD today (at least one reject visit) and then breaks — a later visit whose first candle closes through it, or a later candle of the held visit closing through it — is NOT made ISB-only: its role flips once (counter level_broken_uno, the resting order is still cancelled), and the next touch from the other side is a fresh first touch, so the backtest reject arms a PHL/PLH through every normal gate. A wrong-way FIRST visit (the level never held — "cây nến chạm đầu tiên đóng ở trên level… invalid" [D5.2 p1 @19:44–20:25]) is, since levels audit LL-2, the October break-and-retest (knob key_level_break_retest, default on): it flips too and its first retest from the other side trades [OctD5.2-R20/R24/R25]; with that knob off it stays ISB-only for the day. A second wrong-way close after a flip stays ISB-only for the day. One flip per level per session day. Moving lines, box edges and old highs/lows keep their own rules. off = the pre-#61 ISB-only on any break.',
          trader: 'on — the course.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (UnoKeyLevels) · kernel/mentor/uno_key_level.go unoKeyLevelBreak / unoSameVisitBreak',
          range: 'on / off',
          systemDefault: 'on',
          recommended: 'on — D1.3 p1 @12:31–12:58.',
          whenToTouch:
            'Rarely — off only to restore the pre-#61 ISB-only rule.',
          perSession: 'No.',
        },
        {
          label: 'Session-open wick + gap edge levels',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #61 (G10 D3.1-R26, G11 D3.1-R20). "đây là mới lúc open… cái râu này nó sẽ là cái đỉnh" (at the open, this wick will be the high — 29,136.00, Sun 13 Sep 17:00) [D3.1 p1 @11:58–12:22]; "Cái cạnh dưới của cái gap, lúc nào nó cũng sẽ là resistance… Level là resistance" (the gap’s edge is always resistance — it is a level) [@09:41–09:51], the gap marked from Friday’s last close 29,391.25 down to the Sunday first candle. on (default) = (1) the first 5m candle of the current CME session (17:00–17:05 CT, after the daily halt or a weekend) is an old high AND an old low candidate with no left-neighbour test (the fractal pivots could not see it because the Friday candle to its left is higher); the ordinary unbroken test and nearest pick decide; (2) when that candle left empty space to the prior session’s last close (gap down: its high under the last close; gap up: its low over it) of at least 20 pts (the key-level prune distance — two lines closer keep the newer one, the open wick), the far edge (the prior last close) is a level (gap_edge: a location, a target and an obstacle) until a 1m candle trades through it — a fill may take days. There is no gap-fill entry ("em không đánh theo gap", R23). off = neither.',
          trader: 'on — the course.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (SessionOpenLevels) · kernel/mentor/session_open_levels.go sessionOpenPivots / gapEdgeLevels',
          range: 'on / off',
          systemDefault: 'on',
          recommended: 'on — D3.1 p1 @09:41–12:22.',
          whenToTouch: 'Rarely — off only to restore the pre-#61 level set.',
          perSession: 'No.',
        },
        {
          label: 'Line order cancel: current line price',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #61 (G17, D2.3-R34). "Mình làm gì? Mình cancel. Cancel. Chẳng làm gì khác hết" (what do we do? cancel — nothing else) [D2.3 p1 @23:35–24:03], when a later candle closes through the level. A trendline moves (~0.45 pt a minute on a typical slope), so the level of a resting order on a trendline or the EMA 34 is where the line is NOW. current_line (default) = the close-through cancel reads the trendline’s price at the checked candle’s close (the EMA 34: this minute’s line), counted level_arm_cancel_current_line when the frozen price would have kept the order; a trendline that died (5m close through) or left the set cancels its order (level_arm_line_gone). Key levels, old highs/lows, box and gap edges, the 5m trigger retest and the wick zone keep the price they were placed at. placement_price = the pre-#61 frozen placement price for every order.',
          trader: 'current_line — the course.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (LevelArmLineCancel) · kernel/mentor/level_arm_line.go levelArmLinePrice · eval.go levelArmCancels',
          range: 'current_line / placement_price',
          systemDefault: 'current_line',
          recommended: 'current_line — D2.3 p1 @23:35–24:03.',
          whenToTouch:
            'Rarely — placement_price only to restore the pre-#61 read.',
          perSession: 'No.',
        },
        {
          label: 'PHL higher low: whole pullback',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #61 (G41, D2.2-R59). "nghĩa là anh đang đi kiếm một cái lệnh có đáy cao hơn đáy trước đó" (we are looking for a trade whose low is higher than the previous low) [D2.2 p3 @08:25–09:25], compared with the lows on the left [@02:51–03:41]. pullback (default) = a PHL is refused phl_pullback_lower_low when ANY candle of the pullback — from the candle after the old high up to the signal candle — printed a low at or under the prior low (the low the leg to the old high started from); mirror for a PLH: plh_pullback_higher_high. Before #61 only the signal candle’s low was compared, so a pullback that had already broken the prior low passed once its newest candle’s low sat higher. The rolling PHL ladder keeps its own rules. signal = the pre-#61 signal-candle-only check.',
          trader: 'pullback — the course.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (PHLPullbackLow) · kernel/mentor/phl.go phlPullbackRefusal',
          range: 'pullback / signal',
          systemDefault: 'pullback',
          recommended: 'pullback — D2.2 p3 @08:25–09:25.',
          whenToTouch: 'Rarely — signal only to restore the pre-#61 check.',
          perSession: 'No.',
        },
        {
          label: '5m ISB box: merged inside bar',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'ISB5 audit (October course, OctD4.3-R59 [Oct D4.3 @ 58:00]): "1 cái nến 5 phút, 2 cái nến 5 phút, 3 cái nến 5 phút nó vẫn chưa thoát được cái box này thì nó thành 1 cái Inside Bar ... bọc từ head to toe" — the 5m candles that stay inside the box become ONE inside bar. merge (default) = every 5m candle that CLOSES inside the standing box widens it to that candle’s high and low (counted isb_5m_box_merged); the box keeps the FIRST inside bar’s time and direction (OctD4.3-R54 "always measured against the first inside bar") and is deleted only when a 5m candle closes outside it (OctD4.3-R67). A 1m close outside the box as it stands is still the break (OctD4.3-R66) — but when that 5m candle then closes back inside, the merge takes the break back (isb_5m_box_merge_unbroken) and the break lock clears on that close (break_lock_failed). While the merged box stands, crossing 5m inside-bar pairs inside it do not lift the same-direction rule (OctD4.3-R70 "chỉ có quyền được đánh Insha bar cùng chiều với khung 5 phút"; OctD4.3-R9 the 5m is the only big frame). On the course day (Mon 2 Nov) this draws the mentor’s box exactly: 28,639.75–28,748.75 from the 08:35 candle, deleted on the 09:10 close. The course states no size limit for a merged box, so there is none (a cap would be a new knob with no course number). The long/short label of a merged box is the first inside bar’s mother colour — the course does not say; it is on the mentor question list. newest = the #61–#66 read (September D4.1-R37 [D4.1 p1 @22:31–22:49] "box the inside bar NEAREST in time"): a NEWER closed 5m inside bar replaces the standing box (isb_5m_box_replaced, _opposite when the direction flips). first = the pre-#61 read (the first box stands until it escapes).',
          trader: 'merge — the October course.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (FiveMinBoxSource) · kernel/mentor/eval.go Tick (the R5 5m box) · kernel/mentor/isb_box.go mergeISBBox5m',
          range: 'merge / newest / first',
          systemDefault: 'merge',
          recommended: 'merge — Oct D4.3 @ 58:00 (OctD4.3-R59).',
          whenToTouch:
            'Rarely — newest restores the #61–#66 replace-with-newest read; first the pre-#61 read.',
          perSession: 'No.',
        },
        {
          label: 'Break direction lock',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #61 (m466 G21 D4.1-R31/R33 + G29 D4.1-R55). "Break ra phải có setup… nếu nó break đầu trên thì xuất hiện inside bar cho lệnh long hoặc potential high low… còn nếu break đầu dưới thì… inside bar cho lệnh short hoặc potential low high" [D4.1 p1 @18:27–20:33], for "the 5m box, anything — một cái vùng range, failure to go higher/lower, inside-bar box"; "thoát [đầu] nào thì mình trade [đầu] đó" [D4.1 p1 @07:47–08:07]. ON (default): a BREAK — an FTGH/FTGL box’s first body escape, or a 5m/15m/30m ISB rest box’s 1m-close escape — sets a lock to the break side. While it stands every intraday entry on the OTHER side (ISB, reverse ISB, PHL/PLH level and rolling rungs, box returns) is refused break_counter_setup. It clears when a 1m candle closes back through the broken edge (the failed break, D4.1-R30 "bull flag đi lên xong bắn ngược xuống, cũng như không" [D4.1 p1 @17:43–18:29], counted break_lock_failed), on an opposite break (break_lock_flipped) or at the 17:00 CT roll. A lock set by the EARLY (provisional) 15m box also ends when that 15m candle closes as no inside bar — there never was a 15m inside bar to break (counted break_lock_forming_released, D4.2-R11 [D4.2 p1 @07:20–08:15]); a lock that early break had replaced (an FTGH/FTGL or ISB break that never failed) comes back then — or when the early break itself fails — unless its own edge was closed back through meanwhile (counted break_lock_restored, release #61 review round 2). The break candle itself is never an entry (unchanged). The swing is exempt. The lock refuses NEW entries only: an opposite-side stop order already resting before the break is not cancelled by the lock (its own setup cancels it — level broken, box escaped, expiry). Note: a pullback that breaks a 5m inside-bar box DOWN in an uptrend now refuses the rolling buy rungs until a close back above that box (pinned on the D2.2 tape, TestM61BreakLockRefusesRollingRungAfterFiveMinBreakDown).',
          trader: 'ON — the course.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (BreakDirectionLock) · kernel/mentor/break_lock.go advanceBreakLock / courseRangeFilter · kernel/mentor/eval.go Tick',
          range: 'true / false',
          systemDefault: 'true',
          recommended: 'ON — D4.1 p1 @18:27–20:33.',
          whenToTouch: 'Only on an owner ruling.',
          perSession: 'No.',
        },
        {
          label: 'Too-small range guard',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #61 (m466 G33 D4.2-R33). "từ đây trên đây có 50-60 điểm, mà cây nến vừa vậy 30-40 điểm… bên trong một cái range nó quá nhỏ đi, đừng bao giờ đụng tới" (50-60 points top to bottom with 30-40-point candles — a range that small, never touch it) [D4.2 p2 @06:16–06:30]. The numbers are the video’s measurement, not ours: the 1m chart 09:44–09:59 spans 61.50 pts with a 33.75 / 34.50-pt candle. DEFAULT OFF (release #61 final): the course gives only this one worked example — no count of 30–40-pt candles and no breakout margin — and three review rounds kept finding mis-latches (a latch on the breakout candle itself; a churn close just outside the prior 14 wicks read as a break with no margin), so the guard stays available behind this knob and the count + margin are on the mentor list. A stored true still wins. ON: when the last 15 closed 1m candles span at most 61.50 pts and at least TWO of them are 30.00 pts or more (the video’s two candles; one outlier wick in a chop of teens-sized candles is the normal range of D4.2-R31 “nến tầm mười mấy điểm”), every intraday entry priced inside that range is refused range_too_small_big_candles until a 1m candle CLOSES outside it (counted range_small_set / range_small_cleared). The range stays the box it latched, and the close outside ENDS it: a new too-small range needs 15 candles that all came after that break, so the break-direction entry is never refused by a range re-drawn around the breakout candle (release #61 review round). A window whose NEWEST candle closes outside the 14 before it is a break, not a range: nothing latches and that candle counts as the break (range_small_breakout, D4.1-R34 [D4.1 p1 @20:36–21:03] — release #61 review round 2). The box ping-pong 50-pt rule is unchanged (it is the same idea for two boxes).',
          trader:
            'OFF — the course states no candle count or breakout margin (mentor question).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (SmallRangeGuard) · kernel/mentor/range_small.go advanceSmallRange · kernel/mentor/break_lock.go courseRangeFilter',
          range: 'true / false',
          systemDefault: 'false',
          recommended:
            'OFF until the mentor states the candle count and breakout margin — D4.2 p2 @06:16–06:30 is one worked example.',
          whenToTouch: 'Only on an owner ruling.',
          perSession: 'No.',
        },
        {
          label: 'Mid-range: trade with the rejection',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'Release #61 (m466 G43 D2.2-R71). "nếu thật sự nó vừa reject FTGH thì về tới đây tôi đánh short xuống tiếp chứ… mắc mớ gì tôi đánh ngược lên? Nó mới reject thì đánh theo hướng nó reject" [D2.2 p3 @14:28–15:58]. ON (default): between the floor box and the ceiling box (where only an inside bar may trade, D2.2-R72), after a 1m candle rejects the ceiling (High at or above its bottom, Close below it) a LONG entry is refused mid_range_against_rejection until a later candle reaches the box below (the ceiling is an FTGH, or a box that flipped — an un-flipped FTGL hit from below is no FTGH reject, release #61 review round 2); the mirror for a floor reject refuses SHORT entries until a candle reaches the box above. The newest rejection replaces an older one (counted mid_range_reject_long / _short / _done). Release #61 also fixed the between-two-boxes read itself (G37, no knob): a box’s role is the side of price it is on — an FTGH that escaped up and is now below price is the floor (Uno Reverse, "Kháng cự bị phá → hỗ trợ khi backtest" [D1.3 p1 @12:31–12:58]); a box that never flipped keeps its own role only (an FTGH merely wicked through is no floor).',
          trader: 'ON — the course.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (MidRangeRejectSide) · kernel/mentor/reject_side.go advanceMidRangeReject · kernel/mentor/break_lock.go courseRangeFilter · kernel/mentor/eval.go midRangeBoxed',
          range: 'true / false',
          systemDefault: 'true',
          recommended: 'ON — D2.2 p3 @14:28–15:58.',
          whenToTouch: 'Only on an owner ruling.',
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
          what: 'D3.3 p1 @05:18–05:35 "Target là về những level kế tiếp… gần đỉnh cũ, EMA 34, EMA 9, con cá, trendline, anything… những cái mà nó ngán đường anh chị trên đường đi" (the target is the next level — near the old high, EMA 34, EMA 9, the fish, a trendline, anything that blocks your path) and @07:57–08:04 "Thì cứ target nè: box, đỉnh cũ, EMA 34, trendline" (so target: the box, the old high, EMA 34, the trendline). true (default) = a valid trendline in the trade’s path is a target and counts as the first obstacle for the room rule — except on a BOX trade while “Box trade: a trendline in the path is a partial, not the room cap” is on (answers-1112 Q12, default off since the 2026-11-12 review — the owner’s opt-in): there the line is a place to sell part and the room reads the next real level beyond it; false = the pre-#25 rule (a trendline is a location only, never a target). Knob rule case (a), ON.',
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
          label:
            'Box trade: a trendline in the path is a partial, not the room cap',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.box_trendline_partial_not_room)',
          what: 'answers-1112 Q12 (verdict confirmed, confidence medium). At a box edge you take the trade; a trendline on the way is where you sell part, not a reason to skip: “Đụng hỗ trợ thì cứ việc đánh lên… Target là về những level kế tiếp… Về trendline / Anything / Những cái mà nó ngán đường anh chị… Nó là những target… Là những cái chỗ anh chị bán bớt” (at support just go long; the targets are the next levels on the way — a trendline, anything that blocks your path; those are where you sell part) [Sept D3.3 p1 @05:15–07:46]. His chart Sun 13 Sep (frame f_000338): FTGL box under a falling trendline from the 18:30 top — “Đụng cái box, cái target anh chị muốn đánh lên đâu, có phải là đánh lên trendline không… biên bông khá là nhỏ… nếu mà thoát ra khỏi trendline thì biên bông nó sẽ bự hơn… mình canh mình đánh lên tới trên đây” (touch the box, the target is the trendline; a small range; if it gets out past the trendline the range is bigger, aim up to here) [Sept D3.2 p2 @11:06–11:39]; “giữa một cái đường line và một cái box… tin cái box hơn” (between a line and a box, trust the box) [@03:26–03:59]. October: “đụng support, đánh lên” [Oct D3.3 @46:00–46:27]; no October lesson refuses a box trade because a trendline is close. Default OFF since the 2026-11-12 review: the report lists this build under “Builds, all default OFF”, the video aims past the line only once price gets out of it (“nếu mà thoát ra khỏi trendline” [Sept D3.2 p2 @11:11–11:39]), and no lesson states a room to a trendline — so ON is the owner’s opt-in. ON: when a box trade’s target would be a trendline and a REAL level (old high/low, key level, box edge, HTF line) stands beyond it, the target and the 2R room rule read that level; the line rides as the runner’s first partial (leg 3) when it sits past the 1:1 leg-1 exit — he sells part only at 1:1 or better [Oct D1.2 @11:00–11:40]. A line with nothing real beyond stays the target (today); a real level still caps the room. ISB, PHL/PLH and swing targets are unchanged. The tape (13 Sep, 20:24): the candle touches the FTGL [28,981.00–29,003.75] and closes 29,008.25 → buy stop 29,013.50, stop 29,000.25 (13.25 pts); the bot’s line is at 29,028.57 (1.14R) → today refused box_room; ON → target the FTGH bottom 29,096.75 (6.3R), partial at the line; price reached 29,078.00 by 21:01. On that tape the 20:24 return is still refused earlier by the standing 5m ISB box (box_isb_ban), which this knob does not touch. 90-day replay (data.db read-only, default settings, vs today): BOX entries 20 → 32 (RTH 4 → 11); 3 BOX entries that already traded now aim past their trendline; ISB, PHL/PLH and swing unchanged. The two 10-07 RTH box longs come back (13:08 and 14:54 CT), but there the line sits only 0.25R and 0.07R above the entry. On 1m bars, of the 12 new entries 11 fill: 5 reach 1:1 without touching the stop, 6 have the stop inside the fill candle (order unknown) — of those 3 then stop out, 2 reach 1:1, 1 reaches only the line. The owner decides whether to turn it on. OFF (default) = today exactly.',
          trader:
            'off — owner’s opt-in (review 2026-11-12); on = Sept D3.3 p1 / D3.2 p2, October silent.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (BoxTrendlinePartialNotRoom) → kernel/mentor/box_trade.go boxTargetPastTrendline (boxEntryIntentWhy) · kernel/mentor/eval.go runnerPartialLine (leg-3 partial) · trader/mentor_actions.go leg2_qty/leg2_tp (mode B, runner ≥ 2, AddOn leg-3 proof)',
          range: 'on / off',
          systemDefault: 'off',
          recommended:
            'off until the owner rules (review 2026-11-12; report “Builds, all default OFF”). On reads [Sept D3.3 p1 @05:15–07:46; Sept D3.2 p2 @11:06–11:39]. The partial at the line only goes out when the runner is ≥ 2 contracts in mode B and the AddOn proves the leg-3 build; otherwise leg 1 sells at 1:1 and the runner rides to the target. Not built: counting only a line that has had its 3rd test (Sept D3.2 p2 @03:26–03:59) — no trendline cap is kept on a box trade, so there is nothing to restrict.',
          whenToTouch:
            'Set true only on the owner’s ruling; false (or unset) keeps today’s rule (a trendline in the path caps a box trade’s room).',
          perSession: 'No — per strategy.',
        },
        {
          label: 'PHL/PLH inside the 5m ISB box: same direction as a scalp',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.phl_5m_box_same_dir_allowed)',
          what: 'answers-1112 Q9 (verdict confirmed, confidence medium). Inside a standing 5m inside-bar box he trades only the box’s own direction: “muốn tray bên trong InshaBar 05 phút thì anh chị phải đánh cùng chiều với InshaBar 05 phút” (to trade inside the 5m inside bar you must trade the same way as the 5m inside bar) [Oct D4.3 @1:08:24–1:08:39]; “Chứ không có 5 phút đang Inshabashort vô đây đánh lệnh lon nha” (not a long in here while the 5m is a short inside bar) [@1:09:46–1:09:51]. He then allows a lower-high short at the 1m mini peaks inside a SHORT box, because it “vẫn là đúng theo cái Insa Bar cho lệnh Short bên ngoài” (still agrees with the short inside bar outside) [@1:11:20–1:11:24] — but only for a trained eye: “đây là kinh nghiệm cho anh chị nào đã nhìn quen rồi / Chứ những anh chị khóa mới chưa nhìn quen thì đừng” (this is experience for those whose eye is used to it; new students, don’t) [@1:10:58–1:11:03], and “muốn trade một cách thoải mái thì đừng” (if you want to trade comfortably, don’t) [@1:12:00–1:12:11]. It has no target of its own: “Chỉ cần xuống gần cạnh dưới bóc là anh chị đã out rồi. Chỉ là scap thôi” (just get near the box’s bottom edge and you are out; only a scalp) [@1:11:40–1:11:45]. October widens September, where only a same-direction ISB traded inside the box [Sept D3.4 p2 @07:27–10:35]. on = while the 5m ISB box stands (its direction is the colour of its first candle), a PHL/PLH on the box’s own side is NOT refused phl_5m_box_ban; its target is capped at the box’s far edge (box high for a long, box low for a short) and the runner does not ride past it; the 1:1 floor and the PHL room rule read the capped target; an entry at or past the edge is refused phl_5m_box_scalp_no_room. The opposite side stays phl_5m_box_ban; the 15m/30m box bans, the mid-range bans and box trades inside the box (box_isb_ban) are unchanged. The mentor’s own WE6 (Tue 27 Oct, long box 29,611.25–29,673.50, buy stop 29,645.00, target 29,668.50) and WE7 (Fri 30 Oct, short box 28,288.75–28,333.00, sell stop 28,316.25, target 28,293.75) both go with their box, and with this on the rolling ladder arms both rungs — on the live tape other gates still refuse them (the 1H touch and mid-range on WE6, X4 and the room on WE7). His one example scalp took 14–15 pts (heard in the report’s large-v3 re-listen @1:11:27–1:11:57, not in the stored transcript); no point number is wired — the box edge is the cap. 90-day replay (data.db read-only, default settings, knob on vs off): 34 new PHL/PLH entries (PHL 274 → 297, PLH 257 → 268; RTH PHL 11 → 12), every target inside its box (6 capped at the edge, 1R–2.32R), no existing entry changed or lost; phl_5m_box_ban 3,384 → 1,188 and rolling_phl_5m_box_ban 1,829 → 615 — most lifted cases die at a later gate (15m/30m box bans, near_box, the 1:1 floor); 1 phl_5m_box_scalp_no_room. off (default) = today exactly.',
          trader:
            'off — experienced only [Oct D4.3 @1:10:58–1:11:03]; the owner decides.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (PHL5mBoxSameDirAllowed) → kernel/mentor/eval.go phlLocationBanFor (phl5mBoxSameDir) · phl5mBoxScalpEdge / withPHL5mBoxScalpEdge (level touch loop) · kernel/mentor/rolling_phl.go rollingPHLTick',
          range: 'on / off',
          systemDefault: 'off',
          recommended:
            'off [Oct D4.3 @1:10:58–1:11:03, @1:12:00–1:12:11 — not for new students, not for comfortable trading]. Turn on only if the owner rules the bot trades as an experienced trader.',
          whenToTouch:
            'Set true to let a same-direction PHL/PLH scalp to the box edge; false (or unset) keeps today’s ban.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'HTF-opposed range halt (rest of the day)',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.htf_opposed_range_halt_pts)',
          what: 'answers-1112 Q1 extra rule. His morning read: “Khung 4 giờ nó đang trigger gì … Khung 1 giờ … Nếu như 2 khung trigger cùng với nhau Ok Mình đánh theo khung 4 giờ … Còn nếu như 2 khung giờ đang ngược nhau / Mà khung daily nó đã chạy được 300-400 điểm rồi / Tắt máy nghỉ luôn cho em” (if the 4h and 1h agree, trade with the 4h; if they are opposed and the daily candle has already run 300-400 points, shut the machine off) [Sept D5.1 p1 @19:03–19:26]. The daily candle is measured from the Asia open through pre-market (“nó phải chạy từ Asia / Nó chạy qua pre-market” [@16:55–17:00]), so the range here is the Globex day, 17:00 CT → now, not RTH high-low. The §7 day gate already reads this rule ONCE at 08:30 with the day gate spent threshold (265). This knob is the re-read AFTER the open: from 08:30 to the 17:00 CT roll, at every minute, when the 4h and 1h triggers are opposed and the Globex day has run at least this many points, every new mentor entry is refused for the rest of the trading day (isb_/phl_/box_htf_opposed_range_halt), even if the 1h flips back later. The 4h swing is exempt, as it is from the day-off. A restart restores the halt from the bars (the log says RESTORED). On his own example day (Tue 15 Sep, 4h sell / 1h buy) the daily candle was 264.5 at 08:30 and 287.00 at its 14:52 low (his evening measure: −286.75), so a value of 300 would never have fired that day; 265 fires at 09:52.',
          trader:
            '0 = off — the video gives a range (300–400), not one number, and it is September; October uses the 5m only [Oct D4.3 @07:26–07:38].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (HTFOpposedRangeHaltPts) → kernel/mentor/htf_range_halt.go htfRangeHaltLatch (Tick, after the §7 day gate) · htfRangeHaltLeave (where intents leave Tick) · SeedHTFRangeHalt (restart, via SeedDayLatch)',
          range: '0 (off) or 50–2000 pts',
          systemDefault: '0 (off)',
          recommended:
            '0 [Sept D5.1 p1 @19:22–19:26 gives 300–400]. If the owner turns it on, 300 is the lower edge of his range; anything at or below the day gate spent threshold (265) also repeats the 08:30 read every minute after the open.',
          whenToTouch:
            'Set a number of points to stop new entries for the rest of the day once the 4h and 1h are opposed on a day that has already run that far; 0 (or unset) keeps today’s behaviour.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Trendline reject between two boxes',
          where: 'Strategy → Risk control → Mentor method numbers',
          what: 'D3.2 p2 @10:56–11:20 "Đụng trendline, nên đóng dưới, đánh ngược lại xuống dưới… dưới cái box" (touch the trendline, close below, trade back down… to the box) — against the same lesson @08:34–08:55 "Nếu như nằm bên giữa cái box mình sẽ không được bắt bất kỳ một lệnh Potential 2 low" (between the boxes you may not take any potential-2-low). OFF (default since oct round 2 D3-G8) = every level PHL/PLH between two boxes, trendline included, is refused (phl_mid_range_boxed): October’s only statement on the middle of two boxes is “nếu như ở giữa 2 box chỉ được quyền đánh Inza Ba” — between two boxes only the inside bar may be traded [OctD3.3-R59 @46:27, frames 45:03–47:03], and no October lesson repeats the trendline exception. ON = the September D3.2 p2 exemption: a trendline reject between an FTGL and an FTGH may trade.',
          trader: 'OFF — October [OctD3.3-R59 @46:27].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · kernel/mentor/eval.go midRangeBoxed',
          range: 'true / false',
          systemDefault: 'false',
          recommended:
            'OFF — October wins over September [OctD3.3-R59 @46:27].',
          whenToTouch:
            'Only on a mentor answer — ON restores the September exemption.',
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
          what: 'D3.4 p3 recovered2 @00:00–00:21 "khi nào nó rớt xuống đây, nó bật ngược lên, nó tạo cái failure to go lower dưới này nè… thì anh chị mới có được cái range giữa cái này và cái này" (when it drops down here, bounces back, and makes a failure-to-go-lower down here — only then do you get the range between this and this) — a range escape makes a NEW zone, not a redraw. D3.2 p2 @09:47–09:54 "Đừng vẽ vùng thứ 3… Rối lắm" (do not draw a third zone… it gets messy) caps it: the new zone must sit entirely outside the escaped range. D4.1 p2 @02:38–03:31: a higher high / lower low does NOT redraw the drawn box, and the drawn box is never deleted. true (default) = after a standing FTGH/FTGL box escapes (a closed 1m body fully outside), ONE new same-role box is latched from confirmed swings that form after the escape and lie wholly beyond the old range (a new FTGL below the floor, a new FTGH above the ceiling) — since the phl audit (PHL-2) with the same pairing as the first box: the NEW extreme swing paired with the first swing LEVEL with it (the horizontal-line test [Oct D3.3 @ 14:20]), the box drawn from peak 1\u2019s wick [Oct D3.3 @ 19:31]; a pair of swings further apart than the day\u2019s flat-near measure is no box (before the audit 519 of 917 post-escape boxes were such pairs); the new box is the one live box of its role — the live set is always at most one floor and one ceiling, so the escaped box retires from the live reads (a floor escape never drops the standing ceiling), one new zone per further escape. false = the pre-#29 rule (the first box per role per day is the only box). Knob rule case (a), ON.',
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
          label: 'Rolling PHL/PLH: 1m old-extreme swing width',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.phl_old_extreme_1m_k)',
          what: 'phl audit PHL-1. The rolling PHL/PLH targets "gần đỉnh cũ / gần đáy cũ" — near the old high / old low [OctD2.3-R30 @ 26:50, R42 @ 41:23]. In every worked example of Oct D2.3 that old extreme is the 1-minute swing the pullback started from, made minutes earlier (10-27 09:36 H 29,673.50; 10-30 13:21 L 28,288.75; 10-29 20:53 L 28,825.00; 10-29 19:48 L 28,958.75). The bot reads it from the 1m tape: the newest bar that beat its k bars to the left and that no later bar traded beyond, confirmed once k bars stand to its right. Until it is confirmed the rung is refused rolling_phl_old_extreme_unconfirmed (the ladder goes on); an older, farther swing is never used in its place, and with no old extreme beyond the candle the rung is refused rolling_phl_no_old_extreme_on_side ("làm gì có target … không cấu tạo được một setup" [OctD2.3-R53 @ 54:00]). The 5m/15m swing levels stay the runner’s next target. The course gives NO width: 4 is the only value that reproduces all four worked examples on the real bars (2 or 3 read the 10-27 09:40 lower high 29,662 and the 1:1 floor refuses the mentor’s trade; 5 confirms the 10-30 and 10-29 lows one candle after his fill). -1 restores the source used before this audit (the 5m/15m pivots of the level set, which on 11-09 armed four 21:44–21:47 sell stops with a target 43 pts away from an 11:15 low).',
          trader:
            'Blank = 4. Before the audit the old extreme was a confirmed 5m/15m pivot: a 1m low was available 10–15 minutes later or never, so the 10-30 13:25 and 10-29 19:52 course trades had no target and were dropped silently.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (PHLOldExtreme1mK) · kernel/mentor/phl_tape_extreme.go phlTapeOldExtreme · kernel/mentor/rolling_phl.go rollingPHLTick',
          range: '1–10 bars (-1 = the pre-audit 5m/15m pivots)',
          systemDefault: '4',
          recommended:
            '4 — reproduces the four Oct D2.3 worked examples (TestPHL1CourseWorkedExamples); change it only on a mentor ruling.',
          whenToTouch: 'Only on a mentor ruling.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'PHL entry buffer',
          where: 'Strategy → Mentor mode → PHL/PLH',
          what: 'The PHL/PLH order sits this far BEYOND the candle extreme (outward) — the October course moves the entry 1 point past the broken candle\u2019s high/low, never exactly at it. This is the whole offset on the wire. Its partner, the PHL stop buffer, puts the stop 1 point beyond the candle\u2019s OTHER extreme (not when the EMA 34 nuance puts the stop AT the EMA). Entry and stop move together: 2 points of buffer in total, so the risk is the candle range + 2.00 (the 25-pt stop ceiling and the 2x room rule see that bigger risk) [Oct D2.3 @ 46:20-47:20]. Sept was 0.50 / 0.50 [D2.2-R10].',
          trader: '1.00 pt (stop buffer 1.00 pt).',
          consumer:
            'kernel/mentor/mentor.go PHLEntryBufferPts + PHLStopBufferPts · kernel/mentor/phl.go',
          range: 'pts',
          systemDefault: '1.00 / 1.00',
          recommended:
            '1.00 / 1.00 — [Oct D2.3 @ 46:20-47:20] OctD2.3-R46: entry 1 point beyond the candle low (short), stop 1 point beyond the candle high, 2 points in total; it supersedes the Sept 0.50 / 0.50 rays [D2.2-R10 p1 @04:14–05:43].',
          whenToTouch: 'Rarely.',
          perSession: 'No.',
        },
        {
          label: 'Wick microscalp',
          where: 'Strategy → Mentor mode → Mentor method numbers',
          what: 'm465 (release #60) — the whole D4.3 setup: ≥2 of the last 3 closed 5m candles pulling the wick WITH the 5m trend (lower wicks long, upper wicks short; one odd candle tolerated) draw a 5.00–14.25-pt zone box, extended right until a 1m close beyond its far edge; a 1m ISB/PHL at the box arms through the ordinary paths; the target is the nearby 5m wicks (≤100 pts). Every refusal named: wick_run_too_short, wick_mixed_direction, wick_against_trend, wick_zone_broken, wick_zone_no_setup, wick_zone_wrong_side, wick_target_no_room, wick_safe_play_wait.',
          trader: 'OFF.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve → kernel/mentor/wick_zone.go wickZoneLevels / wickZoneLeave · kernel/mentor/wick_microscalp.go wickTargetLadder',
          range: 'true / false',
          systemDefault: 'OFF',
          recommended:
            'OFF — "làm ơn đừng… đừng có tập khúc này đầu liền, từ từ rập vô sau" [D4.3 @04:55–05:17]: advanced; practise it after the core setups.',
          whenToTouch: 'Only after the core setups are proven on SIM.',
          perSession: 'No.',
        },
        {
          label: 'Wick safe play',
          where: 'Strategy → Mentor mode → Mentor method numbers',
          what: 'm465 (release #60) — D4.3-R20: while the wick microscalp is ON, a setup at a wick zone counts only once its reference candle CLOSED outside the box on the trade side (under a supply box for a short, over a demand box for a long); otherwise it waits (wick_safe_play_wait). Read only while wick_microscalp is ON.',
          trader: 'OFF.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve → kernel/mentor/wick_zone.go wickZoneLeave',
          range: 'true / false',
          systemDefault: 'OFF',
          recommended:
            'OFF — "Mọi người vẫn có thể trade bình thường, là thêm một sự confirm" [D4.3 @14:07–14:27]: optional.',
          whenToTouch:
            'When the zone entries take too many touches that close inside the box.',
          perSession: 'No.',
        },
        {
          label: 'Stop ceiling',
          where: 'Strategy → Mentor mode → risk',
          what: 'The evaluator’s hard stop ceiling for PHL/PLH stops and box-return stops — its only two readers. The ISB is not checked against it: the ISB (and the reverse ISB) meet the mentor_isb_max_pts ceiling (default 20 since release #61 — D4.1-R12; the SAME knob the trader R8 gate reads for an ISB), which this value does not move. The trader’s R8 gate keeps 25 for PHL/PLH/box stops.',
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
          what: 'Release #52 B11. Refuse an ISB or PHL/PLH whose nearest box edge in the trade direction is closer than this × its own risk — the setup is "sát box" ("Insaba Sword ngay chỗ đó mình không được đánh… sát box gần box gần supply quá mình không được đánh" [Ngày 3.2 FTGH FTGL part1 @00:22:44–00:23:10]). The mentor gives NO distance, so the 2× is OURS (the same room the target rule demands), not the course. Release #60 (D3.2-R29/R30): an entry INSIDE the box that opposes its side (an FTGL for a short, an FTGH for a long) is refused near_box_inside_opposing — the mentor’s E6, a short ISB sitting on the FTGL top with the FTGH drawn above [D3.2 p1 @22:12–23:08]; and "between two boxes" is no longer a blanket exemption — the middle ISB the mentor took was fine because nothing was near it ("nó đâu có làm gì đâu" [@23:14–24:41]), so it passes on its own room (the "Near-box: between two boxes exempt" card restores the old exemption). Blank = 2; an explicit 0 turns the refusal off for every setup.',
          trader:
            '2× — ours, not the course. Between two boxes the setup still needs its own room (release #60).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (NearBoxRoomMultiple) · kernel/mentor/box.go nearBoxRefusal',
          range: '0–10 × (0 = off)',
          systemDefault: '2',
          recommended:
            '2 until the mentor names the distance — his own example (an ISB short 10–15 pts above an FTGL with a 5–6 pt risk) is 2–3R, so a strict 2× may refuse the 3R end. The between-two-boxes case (D3.2-R30 @23:14–24:41) passes on its own room since release #60.',
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
          what: 'The Globex run (17:00 → 08:30 CT) at/above this before the open = a spent day. Release #60 (D5.1-R18): 265 — the rule says "already run 300–400 before the open → 80–90% it ranges" [D5.1 p1 @16:05–16:11], and its worked example — a −265.00 run (1,060 ticks): "Một cây nến daily chạy 300 điểm, mà tới đây nó chạy gần được 300 điểm… nó chạy gần hết cây daily luôn rồi", "phải còn 100 điểm" [D5.1 p1 @17:56–18:14] — had nothing left to trade.',
          trader: '265 pts — the worked example’s spent day (D5.1-R18).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (DayGateSpentPts)',
          range: '50–2000 pts',
          systemDefault: '265',
          recommended:
            '265 — D5.1 p1 @17:56–18:14 (was 300 before release #60).',
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
          what: 'The swing SIZING cap (B5): a swing whose stop is at/above this goes out at no more than min(your swing size, 2) MNQ — it is NOT a skip. The SKIP is “Swing skip stop (đừng vô)” (mentor_swing_max_stop_pts, default 80 since the answers-1112 build, 90 at m463): with that default a stop above 80 is refused before sizing, so this cap only bites if you raise the skip above it. The swing is EXEMPT from the ISB stop ceiling (mentor_isb_max_pts, default 20 since release #61).',
          trader: '100 pts — size ≤ 2 MNQ at/above it.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (SwingMaxStopPts) → trader/mentor_mode.go mentorSwingWideStopCap',
          range: 'pts',
          systemDefault: '100',
          recommended:
            '100 — “anh chị có thể lấy 1 hay 2 contract” [D5.2 p2 @21:25–21:43] (the size half of the rule; the skip half is mentor_swing_max_stop_pts).',
          whenToTouch: 'Never below 25+30.',
          perSession: 'No.',
        },
        {
          label: 'Swing skip stop (đừng vô)',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25) → SWING max stop',
          what: 'A SWING4H whose stop is ABOVE this many points is NOT entered — the clean reject (entry at the touching candle’s extreme, stop 30 beyond the line) and the inside-bar way back in (stop AT the line) alike. Refused at the evaluator (counted swing_max_stop, with a note naming the stop) and again at the trader’s rule gate (R31) — one number. Release #59 (m463) REVERSES the earlier owner ruling m186 (“take 1–2 MNQ and sit”), on the owner’s order of 2026-11-01 (“full of it”) after being shown the quote.',
          trader:
            'Course: “mà stop loss ở level gần cả trăm điểm thì không ngon nữa… Phải chi 30-40, 50-60 ok, vô 2 contracts được… nhưng 100 điểm quá nhiều rủi ro” [D5.2 p2 @19:34–21:43] — his own example, 91.75 pts, is the one he skips.',
          consumer:
            'trader/mentor_course_exits.go mentorCourseExitsResolve → kernel/mentor/swing4h.go SwingCfg.SkipStopPts (swingStopRefusal) · trader/mentor_mode.go mentorRuleGate (R31)',
          range: 'pts, 0 → 80 (no OFF — unset/0 resolves to 80)',
          systemDefault: '80',
          recommended:
            '80 — “có những lúc mà cái nến 5 phút nó tới 7-80 điểm, 5-60 điểm cứ vô 1-2 [MNQ] cũng được… nhưng risk reward phải 1-1” [Sept D5.2 p2 @05:25–05:33], said over his own 71.75-pt example (entry 28,689, stop 28,615.75 — taken). Nothing is named between 80 and ~100; ~100 is “đừng vô” [p2 @19:41–19:44] and his 91.75 is skipped [p2 @21:06–21:43]. October gives no swing number, so September stands. The 60–80 size step (1–2 MNQ) is not built yet. Set 90 to get the pre-answers-1112 behaviour back exactly (80.25–90 taken again).',
          whenToTouch:
            'Only with a new owner ruling; 90 = the old (m463) ceiling.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Candle-2 entry: the stop covers the longer wick',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.touch_stop_cover_later_wick)',
          what: 'answers-1112 Q7 (verdict corrected, confidence medium). Two candles touch the level and both close back. The order belongs on the FIRST touch candle; if you enter on candle 2 anyway (“em đã du di cho anh chị là được đặt lệnh sell stop ở cây nến 2”), the stop still follows candle 1 — “bắt buộc phải đi theo cây đầu tiên… chứ không phải là đặt ở cây nến 2” [Oct D5.2 @18:49–19:25] — EXCEPT when candle 2’s wick is longer: “ngoại trừ nếu như mà cây râu của cây nến đỏ nó dài hơn cây nến xanh á, thì ok, thì mình đặt ở cây nến đỏ… mà cái stop loss nó bự hơn… đặt cây nến đỏ đi” [@19:42–20:03]. The stop is never inside the further wick. Where the bot enters on candle 2: ONLY the 4h swing’s leeway re-arm (“du di 2 cây nến” [Sept D5.2 p1 @15:37–15:51]); a level-touch PHL/PLH always keeps its order on candle 1, so this knob never touches it. Default OFF since the 2026-11-12 review: carrying a level-trade rule onto the swing re-arm (whose stop is line ± 30, not a wick) is a [B] port the report marks “needs owner sign-off”, and its one replay effect dropped a winner — so ON is the owner’s opt-in. ON: on that re-arm, when candle 2’s wick reaches further than candle 1’s AND past the swing’s line+30 stop, the stop widens to candle 2’s extreme + 1 pt (the course’s 1-pt stop ray [Oct D2.3 @46:20–47:20]); otherwise the line+30 stop already sits beyond both wicks and nothing changes. Widen only. The wider stop still meets “Swing skip stop (đừng vô)” (80) and the trader’s R31 gate. His chart (frames s_00875/s_01185, level 28,154.50): candle 2 H 28,158.25, candle 1’s wick ≈ 28,161 — candle 1 is the further one there, so the exception does not fire on screen. Example: line 28,154.50, candle 1 H 28,161, candle 2 H 28,190 L 28,123.50 → candle 2’s sell stop 28,123.50 with stop 28,191.00 (was 28,184.50); candle 2 H 28,210 → stop 28,211.00 is 87.50 pts, refused swing_max_stop. 90-day replay (data.db read-only): 1 of 9 candle-2 swing re-arms changed — Thu 9 Jul 09:20 CT long, line ≈ 29,675.87: candle 2 (O 29,691.00 H 29,714.25 L 29,613.75 C 29,711.25) wicked 62 pts under the line, so today’s stop 29,645.87 sat 32 pts inside that wick (68.38 pts, taken; on the 5m bars it reached its target); covered at 29,612.75 it is 101.50 pts and refused swing_max_stop. OFF (default) = today exactly.',
          trader:
            'off — owner’s opt-in (review 2026-11-12); on = the October rule carried onto the swing re-arm [B].',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve (TouchStopCoverLaterWick) → kernel/mentor/swing4h.go SwingCfg.CoverLaterWick (swingLaterWickStop) · trader/mentor_mode.go mentorRuleGate (R31) reads the wider stop',
          range: 'on / off',
          systemDefault: 'off',
          recommended:
            'off until the owner rules (review 2026-11-12; report Q7 “needs owner sign-off”). On reads [Oct D5.2 @19:42–20:03]. Not built: applying it to a CANDLE-1 entry (the order resting on candle 1 while a later candle wicks further) — the report’s [B] extension; the course states no buffer or candle count for it.',
          whenToTouch:
            'Set true only on the owner’s ruling; false (or unset) keeps the plain line+30 stop on a candle-2 swing re-arm.',
          perSession: 'No — per strategy.',
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
          what: 'The 4h level touch window is the first 2 touching 5m candles of the 4h candle. The entry may be armed off either of them; if either closes beyond the level there is NO order (cancel any resting arm; the level is invalid). After the window NO further plain reject in that 4h candle (m463: the old re-arm-after-the-stop-distance rule is gone; counted swing_not_first_touch). SWING4H only — every other setup keeps first-touch = 1 candle.',
          trader: '2 candles.',
          consumer: 'kernel/mentor/swing4h.go SwingCfg.LeewayCandles',
          range: 'candles',
          systemDefault: '2',
          recommended:
            '2 — D5.2 p1 @ 15:21–15:48 (“du di là 2 cây nến”); only the first touch counts [D5.2 p1 @13:23–13:40].',
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
            '08:30 + 405 minutes (the RTH band to 15:15 CT) by default since m472 A6 [Oct D5.2 @ 01:00–01:20]; an unset/zero strategy value inherits it, a stored -1 stays no window, a stored 60 / 30 / 90 / 120 is kept as the September window. The "Window length" box offers 30 / 60 / 90 / 120 minutes or "No window (EOD rules still apply)", which saves -1 and removes the time window only — not any hour: no NEW mentor entry from 15:45 CT until the 17:00 CT reopen — on a shortened day from 15 minutes before the early close, 12:00 CT on a 12:15 day (trader/mentor_mode.go mentorPastDailyHaltCutoff), and from the EOD flat (NY 14:45 CT; earlier on a registered half-day) until 17:00 every intraday mentor order is cancelled and an open intraday position is closed (trader/auto_trader_clock.go enforceEODFlatAt; the SWING4H is exempt from an ordinary day\u2019s flat, NOT from a shortened day\u2019s 12:10 CT flat). A set window only adds a further limit. A stored 0 cannot mean "no window" because 0 means unset. Same Save, same reload as the switch.',
          consumer: 'trader/mentor_tick.go mentorWindowGate',
          range: '30 / 60 / 90 / 120 minutes, or -1 = no window',
          systemDefault: '-1 (no window, m472 A6)',
          recommended:
            '405 minutes from 08:30 — the RTH band to 15:15 CT [Oct D5.2 @ 01:00–01:20]; -1 (no window) is an owner choice.',
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
          what: 'With 2+ swing contracts, split the swing: leg 1 (half, rounded up) takes profit at the first target (max(1R, the 5m EMA 34)); the runner is held by the 4h — break-even at +1R and the close at the end of ONE 4h candle [D5.2 p1 @06:03–06:20] (the swing’s own rules), with NO take-profit by default (mentor_swing_runner_tp=none — see “Swing runner take-profit”; capped = the pre-#61 max(100 pts, 2× the first-target distance)). The hold-close never closes more than NinjaTrader still holds. With 1 contract the switch does nothing (counted swing_runner_needs_2_contracts).',
          trader:
            'Course: “Nhưng đừng vô 1 MNQ / Vô Addict là 2 của MNQ để có đường bắn bớt” (not 1 MNQ — at least 2, so you can take some off) [D5.2 p2 @06:08–06:23]; “gồng theo khung 4 giờ” [D5.2 p1 @05:52]; “hô tới đâu luôn” [D5.2 p2 @05:50]. He swings 1 MNQ himself (“Em vô đúng 1 MNQ thôi” [D5.2 p1 @00:43]). The course names no runner take-profit (“rồi tới đâu thì tới”), so since release #61 (m466 P4) the runner has none by default; the old 100-pt cap (the top of the course’s 50–100 pts per 4h candle) is the capped option. Case (b): sizing is the owner’s call.',
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
          label: 'Resonance on any same-way inside bar',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'ON (default, release #60): after a PHL/PLH fill, on every CLOSED candle of the frame the entry was read on (1m, 2m or 5m), if that candle is an inside bar of the one before it (body inside its full range) and the one before has the trade’s colour (green for a long, red for a short), and it is candle 1, 2 or 3 after the fill candle, the position flips to mode A (resonance) — once. Nothing is placed and no stop moves at that moment: the runner’s break-even waits for leg 1’s 1:1 partial exactly as before. The inside bar does NOT have to be a tradeable ISB entry: an ISB whose own entry the bot refused (stop above the ISB max, room, location, a gate) still counts. OFF: only a same-way ISB entry that passes every placement gate except the never-add gate arms it (m151).',
          trader:
            'Course: “sau khi nó khớp lệnh của mình, trong vòng 3 cây nến sau đó xuất hiện một setup Inside bar cùng chiều, thì đó là một setup cộng hưởng” (after it fills, within the next 3 candles a same-way inside bar appears — that is the resonance) [D2.4 p1 @01:37, @04:54]; “inside bar cho lệnh long, cùng chiều nha anh chị, chứ không phải inside bar lệnh short” [D2.4 p1 @03:56, @04:42–04:44]. He names no gate on that inside bar — it is a hold signal, not a new entry.',
          consumer:
            'trader/mentor_resonance_raw.go mentorResonanceRawTick ← trader/mentor_exit_drive.go mentorExitDrive (mentorResonanceRawDrive)',
          range: 'ON / OFF',
          systemDefault: 'ON (unset = ON)',
          recommended:
            'ON — D2.4-R2 is explicit. Counter resonance_armed_raw_isb (and the shared resonance_armed); the log line says “resonance ARMED on a RAW inside bar”.',
          whenToTouch:
            'OFF only to go back to the older rule (the resonance needs a placeable ISB entry).',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Smaller size on a 2m entry',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'ON (default, release #60): an ISB, PHL or PLH entry READ on the 2m (or the slow-evening 5m) sizes the reduced tier (mentor_reduced_contracts, 3) — tier exec_2m_reduce. It ranks after the ISB old-extreme / in-range cuts and the 5m pullback warning, before the 20–25-pt stop band. SWING4H (its own tier) and box entries are not touched. OFF: a 2m entry sizes like a 1m entry.',
          trader:
            'Course: “khi anh chị vô lệnh khung 2 phút, stop loss lớn hơn khung 1 phút. Thì bây giờ stop loss lớn, mình phải làm gì? Mình phải giảm size” (a 2m entry has a bigger stop than a 1m one — so reduce size) [D3.1 p1 @14:09–14:35]; his 2m entry candle measures 15.50 pts / 62 ticks. He gives NO count: the cut is the bot’s reduced tier (3) — the exact number is an open mentor question.',
          consumer:
            'trader/mentor_mode.go mentorContractsFor (tier exec_2m_reduce) ← mentorSizeFor (Intent.ExecTFMin)',
          range: 'ON / OFF',
          systemDefault: 'ON (unset = ON)',
          recommended:
            'ON — D3.1-R30. Counter tier_exec_2m_reduce; the size line names the frame and the stop.',
          whenToTouch:
            'OFF only if the mentor says a 2m entry keeps the 1m size.',
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
          label: 'Break-even offset (ticks)',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.be_offset_ticks)',
          what: 'How many ticks PAST the entry price the break-even stop goes, on the profit side (long: above the entry; short: below). OFF by default: unset, 0 or out of range (0 to 20) is exactly today’s break-even at the entry price. The October course shows the stop one point beyond the entry (entry 10, stop 11) [Oct D4.2 @ 13:00–13:25], but the knob is in ticks and the video states no tick count, so it ships OFF (m472 A5).',
          trader:
            'Applies to the resonance break-even, break-even before the 1:1 partial, and break-even after the partial, and to the swing break-even. A bad value is counted and ignored.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve · trader/mentor_trail_r22.go mentorBEStop',
          range: '0 (OFF) to 20 ticks',
          systemDefault: 'OFF',
          recommended:
            'OFF — the video shows one point past the entry but no tick count [Oct D4.2 @ 13:00–13:25].',
          whenToTouch: 'Only on an owner ruling.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Swing: one filled inside-bar trade per 4h candle',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.mentor_swing_isb_one_per_candle)',
          what: 'After a 5m close through the 4h line made the level invalid, its inside-bar order may fill once. ON (default, today’s behaviour): after that order FILLED and its trade ended, a later inside bar of the same 4h candle whose stop order sits on the trade side of the line is refused swing_isb_spent; the refusal names the bar’s distance from the line and this knob. OFF: that inside bar enters again. The course states NO such limit — the inside bar has no time limit (“xuất hiện inside bar, tôi vô lệnh”, D5.2-R25); “one setup per level” (D5.2-R19/R23) is the bot’s reading of it, so the limit ships as a knob at today’s value (isbTF audit F4). An inside bar whose stop order would rest beyond the line is never a setup at this level: it is refused swing_isb_entry_beyond_line whatever this knob says (before the audit such bars, up to 339 pts away on 11-10, were named spent “at the invalid 4h line”).',
          trader:
            'An inside-bar order that was dropped before it was placed (inside a box, a stale line, the trigger zone, a trader refusal) never spends the level (isbTF-F3).',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve → kernel/mentor/swing4h.go walkSwingBar (SwingCfg.ISBRepeatAfterFill = !knob)',
          range: 'true / false',
          systemDefault: 'ON (unset = ON — today’s behaviour)',
          recommended:
            'ON until the mentor answers — the limit is the bot’s [B] reading, not a course number [D5.2-R25 p2 @10:23–10:51; D5.2-R19/R23 p1 @13:23–13:40].',
          whenToTouch: 'Only on an owner ruling or a mentor answer.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Crossing inside bars: lookback',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.crossing_isb_lookback)',
          what: 'How many CLOSED buckets the crossing read looks back over. When the last N closed 5m candles hold inside bars pointing both ways, the 5m trigger is not valid and the 15m trigger governs (the 30m when the 15m crosses too) — D3.4-R43 “inside bars cross back and forth”. The course states no window: 3 is today’s reading (isbTF audit F6, mentor question Q10). The crossing read changes only WHICH trigger line governs; it never lifts a box gate (isbTF-F2). Over the 91-day replay of a5b7662e (isbTF audit) the escalation usually lasted exactly one 5m candle (2,024 of 3,028 episodes).',
          trader:
            'Unset or 0 = 3. Out of range is counted (tuning_bad_value) and ignored.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve → kernel/mentor/trigger_escalate.go governingTrigger (crossingISBsCfg)',
          range: '3 to 12 buckets',
          systemDefault: '3',
          recommended:
            '3 until the mentor answers Q10 — the course gives no number [D3.4-R43 p2 @17:07–17:49].',
          whenToTouch: 'Only on an owner ruling or a mentor answer.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Crossing inside bars: minimum count',
          where:
            'No Studio control — set it in the strategy JSON (risk_control.mentor_tuning.crossing_isb_min)',
          what: 'How many inside-bar pairs (pointing BOTH ways) the crossing window must hold before the trigger escalates. The course states no count: 2 is today’s reading (isbTF audit F6, mentor question Q10). With the default lookback of 3 there are at most 2 pairs, so only 2 is possible there.',
          trader:
            'Unset or 0 = 2. It must be below the lookback; out of range is counted (tuning_bad_value) and ignored.',
          consumer:
            'trader/mentor_tuning.go mentorTuningResolve → kernel/mentor/trigger_escalate.go governingTrigger (crossingISBsCfg)',
          range: '2 to lookback − 1',
          systemDefault: '2',
          recommended:
            '2 until the mentor answers Q10 — the course gives no number [D3.4-R43 p2 @17:07–17:49].',
          whenToTouch: 'Only on an owner ruling or a mentor answer.',
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
          label: 'ISB loss box',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25) → ISB loss box',
          what: 'What an ISB stop-out blocks. range (default): the whole 5m range that held the trade — every closed 5m candle from the fill candle’s through the stop-out candle’s — joined with the entry-to-stop band; an entry inside it, long or short, is refused loss_range_blocked for the rest of the trading day; a 1m candle completely outside the range frees only entries outside it. A later ISB loss whose range overlaps it joins the same box; two losses there → off for the day (loss_box_off_day). band: the pre-#60 rule — only the entry-to-stop band is blocked. A fill registered after a restart has lost the candles before the fill, so its range starts at the fill candle (counted loss_range_seed_only).',
          trader:
            'Course: “tôi bọc cái khu vực này lại. Tôi sẽ không trade bên trong khu vực này. Tôi đợi nó đi ra một cái khu vực khác” [D4.2 p2 @03:43–04:07]; Box 1 28,880.50–28,934.00 = the 08:30 5m candle (D4.3 Ex.1 H 28,933.75 L 28,880.50).',
          consumer:
            'trader/mentor_course_exits.go mentorCourseExitsResolve → kernel/mentor/limits.go rangeLoss (Config.ISBLossBox)',
          range:
            'range | band · unset → range (an unknown value → range, counted isb_loss_box_bad_value)',
          systemDefault: 'range',
          recommended:
            'range — D4.2-R27/R28/R30 [D4.2 p2 @02:41–05:03]: enter only outside the range you lost in.',
          whenToTouch: 'Only with a new owner ruling.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Opening 5m range break',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25) → Opening 5m range break',
          what: 'In the open window (08:30–09:30 CT), with price still on the wrong side of the 4h trigger line, a with-4h entry waits for the opening 5m range (the 08:30–08:35 candle) to break in the 4h’s direction (refusal open_5m_range_not_broken). 1m_close (default, release #61): a CLOSED 1m candle at or after 08:35 that closes outside the range is the break. 5m_close: a closed 5m candle outside the range (the pre-#61 rule — up to 4 minutes later). Example 14 Sep: range 28,880.50–28,933.75; the 08:43 1m candle closes 28,940 → longs allowed at 08:44 (5m_close: not until 08:45).',
          trader:
            'Course: “nó break được cái range khung 5 phút, nó đi lên rồi, tôi biết tôi đánh” [D4.4 p2 @05:15–05:36]; the break minimum is “một cây nến một phút… đóng bên ngoài cái box là at least” [D4.1 p1 @20:36–21:03]. The mentor acted at the 08:43 1m escape.',
          consumer:
            'trader/mentor_course_exits.go mentorCourseExitsResolve → kernel/mentor/open5m.go open5mBrokenWith (CourseExits.Open5mBreak)',
          range:
            '1m_close | 5m_close · unset → 1m_close (an unknown value → 1m_close, counted open5m_break_bad_value)',
          systemDefault: '1m_close',
          recommended: '1m_close — D4.4-R40 + D4.1 p1 @20:36–21:03.',
          whenToTouch: 'Only with a new owner ruling.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'News wait: which lines',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25) → News wait',
          what: 'What the news wait (“News day: wait beyond the 4h line”, ON) needs between a red-folder print and the 08:30 CT open. both (default, release #61): the last close beyond the 4h trigger line AND the pre-print 1h line on the trade side (refusals news_wait_4h_line / news_wait_1h_line); with no 1h line, or a 1h line older than the 4h one, the 4h alone. 4h_only: the 4h line only (pre-#61). Example: 4h 29,342.25, 1h 29,360 — a long with the close at 29,350 is refused news_wait_1h_line; at 29,365 it passes.',
          trader:
            'Course: “Đợi canh nó nằm trên hai cái đường line này, kiếm inside bar đu vô… Hoặc anh chị chỉ cần đợi nó nằm ở trên cái đường line 4 giờ” [D4.4 p1 @21:48–22:12]; case 2 — the 1h has nothing: “mình sẽ coi 4 giờ thôi, không cần coi 1 giờ” [D4.4 p1 @18:48–18:52].',
          consumer:
            'trader/mentor_course_exits.go mentorCourseExitsResolve → kernel/mentor/news_print.go newsWaitFilter (CourseExits.NewsWaitMode)',
          range:
            'both | 4h_only · unset → both (an unknown value → both, counted news_wait_mode_bad_value)',
          systemDefault: 'both',
          recommended: 'both — D4.4-R22, the course’s first method.',
          whenToTouch: 'Only with a new owner ruling.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'News: read the 1h/4h lines before the print',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25) → Read the 1h/4h lines before the news',
          what: 'On a red-folder print day (with “News day: freeze the pre-print 1h/4h read” ON), the 1h/4h trigger read freezes this many minutes BEFORE the 07:30 print and holds until the 08:30 CT read. Default 30 (release #61): from 07:00 — a 07:05 1m bar that breaks the 1h line does not move it; a 06:55 bar does. The order hold around the print stays 07:20–07:35, and the news period for the wait and the stored news-only gate still starts at 07:20 — only the trigger READ moves. 0 = the pre-#61 start (07:20, the print window).',
          trader:
            'Course: “Một giờ thì đợi gần trước news, tầm 30 phút. Còn 4 giờ thì mình cứ kẻ ở dưới trước đi”; the board: “(trước news 30’)” [D4.4 p1 @05:28–05:36; board 14:33–15:36].',
          consumer:
            'trader/mentor_course_exits.go mentorNewsReadLeadMin · trader/mentor_tick.go mentorNewsPrintWindowsFromEvents (PrintAtMs) → kernel/mentor/news_print.go newsHTFWindows / newsReadStart',
          range:
            'minutes 0–120 · unset → 30 · 0 = from the print window (07:20) · outside 0–120 → 30, counted news_read_lead_min_bad_value',
          systemDefault: '30',
          recommended: '30 — D4.4-R6 “tầm 30 phút”.',
          whenToTouch: 'Only with a new owner ruling.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Swing inside bar — close back',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25) → SWING: does the inside bar after an invalid cross need a close back?',
          what: 'After a 5m close through the 4h line made the level invalid, the only way back in is a 5m inside bar (its body inside the previous candle’s full range) with the stop AT the line. not_required (default, release #61): the inside bar ALONE is the setup — even while that candle and the inside bar still close beyond the line; the buy stop (long) / sell stop (short) must sit on the trade side of the line, or it is refused swing_isb_entry_beyond_line (named, and the level is not used up — a later inside bar may still enter). required: the pre-#61 rule — candle 1 AND its inside bar must both close back across the line first. Either way the stop is AT the line and swing_isb_spent is unchanged.',
          trader:
            'Course, the student’s 11 Sep 09:10 trade (entry 29,424.25, stop at the line 29,405.25, +39.5 / +42.25): “Chị vô đúng mà… thế là đúng… còn nếu chị đợi đóng ngược lại cũng được, không thành vấn đề, nhưng chị đợi hơi lâu” [D5.2 p3 @04:49–05:09]; “cái điểm entry ở inside bar, basically nó vẫn nằm trên line… tùy góc nhìn” [D5.2 p3 @07:42–08:36] (D5.2-R26).',
          consumer:
            'trader/mentor_course_exits.go mentorCourseExitsResolve → applyMentorCourseExits → kernel/mentor/swing4h.go walkSwingBar (SwingCfg.ISBCloseBackRequired)',
          range:
            'not_required | required · unset → not_required (an unknown value → not_required, counted swing_isb_close_back_bad_value)',
          systemDefault: 'not_required',
          recommended:
            'not_required — D5.2-R26 [p3 @04:49–05:09]: the student’s entry before the close back was “đúng”. On the recorded 10-28/29 tape this default adds three inside-bar entries that stopped out (17:50 long −14.47, 23:55 short −4.04, 01:45 long −14.27 pts) and loses the 19:00 inside bar (+53.72) and trade 715 (−9.77) — required is the stricter option.',
          whenToTouch:
            'required to wait for the close back (“valid but slower”).',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Swing inside-bar order kept on a close through',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25) → SWING: the inside-bar order survives a later 5m close through the line',
          what: 'ON (default, release #61): a resting swing INSIDE-BAR order is NOT cancelled when a later 5m candle closes back through the line; it rests until it fills, the 4h candle that carries the line closes (its expiry, D5.2-R27) or the day/EOD rules end it. The REJECT order is still cancelled by any close through the line (D5.2-R22). OFF: the pre-#61 behaviour — the inside-bar order is cancelled too (and, cancelled unfilled, does not use the level up).',
          trader:
            'Course, 11 Aug (level 29,553.50, inside-bar long 29,577.75, stop at the level): “Dù nó có xuống 1 cây nữa, nó trồi được 5 cây nữa. Để cái cây inside bar này cho em” [D5.2 p2 @10:23–10:51] (D5.2-R25).',
          consumer:
            'trader/mentor_course_exits.go mentorCourseExitsResolve → applyMentorCourseExits → kernel/mentor/swing4h.go stepSwingBar (SwingCfg.ISBCancelOnCloseThrough = !keep)',
          range: 'ON / OFF · unset → ON',
          systemDefault: 'ON',
          recommended:
            'ON — D5.2-R25 [p2 @10:23–10:51]. On the recorded 10-28 tape the kept 18:20 inside-bar long (29,730.75) fills and stops at the line (−85.22 pts) where the pre-#61 cancel let the 19:00 inside bar take +53.72.',
          whenToTouch: 'OFF to cancel the inside-bar order on a close through.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Swing runner take-profit',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25) → SWING: runner take-profit',
          what: 'For a split swing (Swing runner ON, 2+ contracts): none (default, release #61) = the runner has NO take-profit — it is held by the 4h and exits by its stop / break-even, the 4h hold-close or the EOD flat. NinjaTrader’s bracket needs a take-profit leg, so it is parked 1,000 pts beyond the entry (counted swing_runner_no_tp; e.g. long 29,500 → 30,500). capped = the pre-#61 cap max(100 pts, 2 × the first-target distance) (long 29,500, first target 29,540 → 29,600). Leg 1 always takes the first target (max(1R, the 5m EMA 34)).',
          trader:
            'Course: “em đánh một lệnh lower high ở khung 5 phút nhưng em gồng theo khung 4 giờ” (one 4h candle = 152 pts) [D5.2 p1 @05:40–06:14] (D5.2-R6); first target the 5m EMA 34, then let it run (D5.2-R32).',
          consumer:
            'trader/mentor_course_exits.go mentorSwingRunnerLegs ← trader/mentor_actions.go mentorArmIntent',
          range:
            'none | capped · unset → none (an unknown value → none, counted swing_runner_tp_bad_value)',
          systemDefault: 'none',
          recommended:
            'none — D5.2-R6 [p1 @05:40–06:14]: the runner is held by the 4h; 1,000 pts is ours (the bracket needs a number), not the course.',
          whenToTouch: 'capped to bank the runner at the old cap.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'ISB max stop',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'Refuse an ISB (and the reverse ISB) whose stop — entry to stop, the ISB range + 2 × 1.5 — is ABOVE this many points, counted isb_stop_over_max / isbrev_stop_over_max. Default 20 since release #61 (m466 G23, D4.1-R12): exactly 20 is taken, 20.25 is refused; the D3.3 20–25 reduced tier is unreachable for an ISB at default. The trader’s R8 gate reads the SAME knob for an ISB and is the top of the ISB reduced band; PHL/PLH/BOX stops keep the D3.3 25 (R8 and the 20–25 band), or this knob when you store it above 25. Since 2026-11-10 this is the ladder-OFF ceiling: with the stop-size ladder ON (the default) an ISB stop above it is NOT refused up to the ladder max stop (40) — it is sized down instead (20–30 → 3, 30–40 → 1 MNQ; see “Stop-size ladder”).',
          trader:
            'Course: “nếu như một cái setup inside bar mà nó hai mươi mấy điểm, làm ơn giúp em đừng vô, làm ơn đừng vô… lệnh inside bar phải là một lệnh có stop loss nhỏ” [D4.1 p1 @05:41–06:08]; his own stop was 20 points [D4.1 p1 @02:39–03:20]. D4.1 is later than D3.3 (“hơn 20, 25 đổ lại ok, chứ mà trên nữa thì thôi” [D3.3 p1 @01:59–02:09]), which stays the PHL/PLH rule.',
          consumer:
            'kernel/mentor/course_exits.go isbOverMaxStop ← kernel/mentor/isb.go ISBStopVerdict · trader/mentor_mode.go mentorStopCeilingFor → mentorRuleGate (R8) + mentorContractsFor (band top) ← trader/mentor_actions.go mentorDispatchEntry',
          range:
            'pts, 0 → 20 (the knob has no OFF — unset/0 resolves to the 20 default)',
          systemDefault: '20',
          recommended:
            '20 — D4.1 p1 @05:41–06:08 (the twenty-something ISB: do not enter). 25 = the pre-#61 rule.',
          whenToTouch: 'Only to change the stop ceiling.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Stop-size ladder',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'ON (default, owner order 2026-11-10 “stop loss can adjust contract to fit like mentor said”): a big stop is SIZED DOWN instead of refused, for every non-swing setup — ISB, reverse ISB, PHL/PLH (the rolling ladder too) and box. Stop up to 20 pts → the normal tiers. Stop in (20, 30] → at most the reduced tier (3), tier stop_ladder_reduced. Stop in (30, Ladder max stop] → at most the wide-stop contracts (1 MNQ), tier stop_ladder_wide. Above the ladder max stop (40) → refused as before (isb_stop_over_max / phl_stop_ceiling / box_stop_ceiling / R8). The ladder only ever LOWERS the size (the smaller of the setup’s own tier and the ladder cap) and never touches the target — so a big stop still needs a target at least as far away (1:1 floor, room rule) and many wide setups are still refused there. SWING4H keeps its own tiers. OFF = the bot before 2026-11-10 exactly: an ISB above mentor_isb_max_pts (20) and a PHL/PLH/box stop above 25 are refused.',
          trader:
            'Course: “Cái nến INSHABA này tầm 20 điểm, 30 điểm... Tôi vô size 3 hoặc tôi không trade... 12 điểm. Ok, tôi vô size 5” [OctD2.2-R73 @01:12:32]; “40 điểm thì mình làm gì? Mình không trade. ... Trade vẫn được. 1 MNQ” [OctD2.3-R47 @48:12]; “giảm size only chứ không có tăng target” [OctD2.2-R49 @40:47]. This replaces the D4.1 “inside bar mà nó hai mươi mấy điểm… đừng vô” refusal [D4.1 p1 @05:41–06:08] by owner order (October wins over September). The exact cut-off between “1 MNQ” and “do not trade” around 40 is an open mentor question (mentor question 11).',
          consumer:
            'kernel/mentor/course_exits.go StopLadderCeiling ← isb.go ISBStopVerdict · phl.go phlPLHR2 · box_trade.go boxEntryIntentWhy · trader/mentor_mode.go mentorRuleGate (R8) + mentorStopLadderCap ← mentorContractsFor ← mentorSizeFor',
          range: 'ON / OFF',
          systemDefault: 'ON (unset = ON)',
          recommended:
            'ON — the owner’s order and OctD2.2-R73 / OctD2.3-R47. Counters tier_stop_ladder_reduced / tier_stop_ladder_wide; the size line names the tier, the stop and the citation.',
          whenToTouch:
            'OFF only to go back to refusing every ISB above 20 and every PHL/PLH/box stop above 25.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Wide-stop contracts',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'With the stop-size ladder ON: the most contracts a non-swing entry may carry when its stop is above 30 pts (up to the ladder max stop). The ladder takes the smaller of this and the tier the setup earned — it never sizes up.',
          trader:
            'Course: “Trade vẫn được. 1 MNQ ... 1 MNQ 40 điểm cũng có 80 đô” (you can still trade it — 1 MNQ; 1 MNQ × 40 pts is $80) [OctD2.3-R47 @48:12].',
          consumer:
            'trader/mentor_course_exits.go mentorCourseExitsResolve → trader/mentor_mode.go mentorStopLadderCap',
          range:
            'contracts, 0 or unset → 1 (a negative value → 1, counted wide_stop_contracts_bad_value)',
          systemDefault: '1',
          recommended: '1 — OctD2.3-R47.',
          whenToTouch:
            'Only if the mentor gives another count for a 30–40-pt stop.',
          perSession: 'No — per strategy.',
        },
        {
          label: 'Ladder max stop',
          where:
            'Strategy Studio → Risk control → 🧑‍🏫 Course exits & rules switches (#25)',
          what: 'With the stop-size ladder ON: a non-swing stop ABOVE this many points is refused — the kernel (ISB, reverse ISB, PHL/PLH, box) and the trader’s R8 gate read the same number. It never narrows a setup’s ladder-OFF ceiling: a stored mentor_isb_max_pts above it still wins.',
          trader:
            'Course: “40 điểm thì mình làm gì? Mình không trade” [OctD2.3-R47 @48:12]; why — a 40-pt stop needs a 40-pt target, or at least about 35 (“40 điểm em cũng phải kiếm cái target 40 điểm ... Hoặc là at least là cũng 35 điểm” [OctD2.3-R48 @49:38]).',
          consumer:
            'kernel/mentor/course_exits.go StopLadderCeiling · trader/mentor_mode.go mentorStopLadderRefuseCeiling (R8)',
          range: 'pts, 0 or unset → 40',
          systemDefault: '40',
          recommended: '40 — OctD2.3-R47/R48.',
          whenToTouch:
            'Only to change where a big stop stops being traded at all.',
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
