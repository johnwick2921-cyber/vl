// W16/R3 — "why was my entry refused?" made answerable from the UI.
//
// telemetry.IncGateBlock has counted every refusal since B6, and
// GET /api/risk/gate-blocks has served the table the whole time — with ZERO
// frontend consumers. The counters were real and invisible.
//
// This panel is deliberately honest about what the counter IS: an in-memory
// tally for the CURRENT CME session-day that resets at 17:00 CT and on restart.
// It says so, rather than letting an empty table read as "nothing was blocked".
// The per-decision reason lives on the decision row itself (DecisionAudit).

import { useEffect, useState } from 'react'
import { tp } from '../../i18n/plan-translations'
import type { Language } from '../../i18n/translations'
import { planApi } from '../../lib/api/plan'

// Human labels for the gate names telemetry emits. An unknown gate falls back to
// its raw name rather than being hidden — a refusal we cannot label is still a
// refusal the owner must see. Exported for the guide's label-list pin
// (guideTruth.test.ts) — the guide copies these strings, the test proves it.
export const GATE_LABELS: Record<string, { en: string; icon: string }> = {
  feed_down: { en: 'NT8 feed down', icon: '📡' },
  dead_man: { en: 'Dead-man watchdog', icon: '⏱' },
  frozen: { en: 'Trader frozen', icon: '🧊' },
  boot_integrity: { en: 'Boot integrity', icon: '🔐' },
  consecutive_loss: { en: 'Consecutive-loss halt', icon: '🛑' },
  last_entry: { en: 'Past last-entry time', icon: '🕒' },
  session_gate: { en: 'Outside session window', icon: '🗓' },
  plan_mode: { en: 'Against the plan', icon: '📋' },
  approval_required: { en: 'Awaiting approval', icon: '✋' },
  clock_skew_observed: { en: 'Clock drift', icon: '⏰' },
  b3_order_dedup: { en: 'Duplicate order dropped', icon: '👯' },
  b3_rate_breaker: { en: 'Order rate breaker', icon: '🚨' },
  level_burned_retouch: { en: 'Burned level re-touched', icon: '🔥' },
  // P2-3a (release #26): a night/day transition is an EVENT, not a refusal —
  // it no longer reaches the gate-block table, so it has no label here.
  maintenance_hold: { en: 'Update hold (maintenance)', icon: '🔒' },
  install_pending: { en: 'Update pending (preflight freeze)', icon: '⏳' },
  // OPS NEW-2 (10-14): planner arms now ask the stale-data verdict; the AI
  // path's B4 has counted stale_data all along.
  stale_data: { en: 'Stale 1m feed', icon: '⛔' },
  clock_step: { en: 'Clock-step hold (wall clock jumped)', icon: '🕰' },
  maintenance_drop: {
    en: 'Queued entry dropped by the update hold',
    icon: '🔒',
  },
  maintenance_drop_attempted: {
    en: 'Dropped entry may have reached NT8',
    icon: '⚠️',
  },
  // W-EXEC-TRUTH W0b — the one admission chain counts every gate under its
  // own name on every entry path; each needs a label (pinned by
  // trader/w0b_guide_test.go).
  stop_until: { en: 'Owner pause', icon: '⏸' },
  contract_roll_resolved: { en: 'Contract roll unresolved', icon: '🔁' },
  no_trade_band: { en: 'No-trade band', icon: '🚫' },
  force_flat_window: {
    en: 'Force-flat window (T1 lead / EOD flat)',
    icon: '🔚',
  },
  cme_closed: { en: 'CME closed', icon: '🔕' },
  // PR #200 F9: a failed current-password compare on PUT /api/user/password
  // (process-wide, under the "" key — it shows on every trader's panel).
  credential_current_password_wrong: {
    en: 'Wrong current password (password change)',
    icon: '🔑',
  },
  reentry_cooldown: { en: 'Re-entry cooldown after a stop', icon: '⏳' },
  entry_gate: { en: 'Entry gate', icon: '🚦' },
  trader_stopped: { en: 'Trader stopped (Picture)', icon: '⏹' },
  day_plan_off: { en: 'Day Plan off (Picture)', icon: '📴' },
  arm_not_admitted: {
    en: 'Arm not admitted this pass',
    icon: '⏸',
  },
  reconcile_owned: {
    en: 'Held position belongs to a ledger order',
    icon: '⛔',
  },
  market_in_zone_short_of_zone: {
    en: 'Price short of the entry zone (waiting)',
    icon: '⏳',
  },
  market_in_zone_unknown: {
    en: 'Entry zone not adjudicated (no fresh price)',
    icon: '⚠️',
  },
  market_in_zone_zone_refused: {
    en: 'Entry zone invalid — leg never placed',
    icon: '⚔️',
  },
  // Mentor mode (P0 fix/mentor-ai-off): with mentor_mode ON every entry
  // comes from the mentor evaluator — AI/planner entries and AI closes on
  // mentor-owned positions are refused under these names.
  mentor_ai_entries_off: {
    en: 'Mentor mode — AI entries off',
    icon: '🚫',
  },
  mentor_ai_closes_off: {
    en: 'Mentor mode — AI closes off (mentor-owned position)',
    icon: '🙅',
  },
  // r22 RISK-GATES — the daily guardrails on every entry path (pinned by
  // trader/w0b_guide_test.go), and the trip itself.
  daily_loss: { en: 'Daily loss limit hit (latched to 17:00 CT)', icon: '🔴' },
  daily_loss_tripped: {
    en: 'Daily loss limit TRIPPED (entries cancelled, bot flattened)',
    icon: '🔴',
  },
  daily_profit_target: { en: 'Daily profit target reached', icon: '🎯' },
  max_daily_trades: { en: 'Max daily trades reached', icon: '🔢' },
  // r22 RISK-GATES (P2-3) — every mentor placement refusal, by gate.
  mentor_stale_data: { en: 'Mentor — stale 1m feed', icon: '🧑‍🏫' },
  mentor_stale_intent: { en: 'Mentor — stale intent', icon: '🧑‍🏫' },
  mentor_sources_missing: { en: 'Mentor — sources missing', icon: '🧑‍🏫' },
  mentor_window: { en: 'Mentor — outside trading window', icon: '🧑‍🏫' },
  mentor_eod_flat: {
    en: 'Mentor — flat window (EOD / forced flat)',
    icon: '🧑‍🏫',
  },
  mentor_done_after_win: {
    en: 'Mentor — done for the day (win)',
    icon: '🧑‍🏫',
  },
  mentor_stop_after_loss: { en: 'Mentor — stop after a loss', icon: '🧑‍🏫' },
  mentor_news_hold: { en: 'Mentor — news hold', icon: '🧑‍🏫' },
  mentor_never_add: { en: 'Mentor — never add', icon: '🧑‍🏫' },
  mentor_expiry_missing: {
    en: 'Mentor — order expiry missing',
    icon: '🧑‍🏫',
  },
  mentor_no_chase: { en: 'Mentor — no chase', icon: '🧑‍🏫' },
  mentor_daily_loss: { en: 'Mentor — daily loss limit', icon: '🧑‍🏫' },
  mentor_max_daily_trades: { en: 'Mentor — max daily trades', icon: '🧑‍🏫' },
  mentor_daily_profit_target: {
    en: 'Mentor — daily profit target',
    icon: '🧑‍🏫',
  },
  mentor_daily_guardrail_unverified: {
    en: 'Mentor — daily guardrail unverified',
    icon: '🧑‍🏫',
  },
  // m426 (review of m425b) — the mentor refusal classes the gate-blocks table
  // now counts by name (trader/mentor_actions.go), so the panel shows words.
  mentor_rule_r8: { en: 'Mentor — rule R8 refused', icon: '🧑‍🏫' },
  mentor_rule_r9: { en: 'Mentor — rule R9 refused', icon: '🧑‍🏫' },
  mentor_rule_other: { en: 'Mentor — other rule refused', icon: '🧑‍🏫' },
  mentor_bad_geometry: { en: 'Mentor — bad stop/target geometry', icon: '🧑‍🏫' },
  mentor_untagged_setup: { en: 'Mentor — untagged setup', icon: '🧑‍🏫' },
  mentor_size_refused: { en: 'Mentor — size refused', icon: '🧑‍🏫' },
  mentor_same_side_working: {
    en: 'Mentor — same-side order already working',
    icon: '🧑‍🏫',
  },
  mentor_resonance_hold: { en: 'Mentor — resonance hold', icon: '🧑‍🏫' },
  mentor_place_off: { en: 'Mentor — placement off (dry run)', icon: '🧑‍🏫' },
  // Release #25 (STRICT-12345 F-2/F-3/F-5/F-7/F-8) — the loss-fix switches,
  // all OFF by default; these count only once the owner turns one on.
  room_gate: { en: 'Room gate — first obstacle under min R', icon: '🧱' },
  room_gate_tp1: { en: 'Room gate — TP1 moved to the obstacle', icon: '🧱' },
  thesis_death_exit: { en: 'Thesis-death exit (planner position)', icon: '🪦' },
  no_chase_refused: { en: 'No-chase enforced (arm refused)', icon: '🚫' },
  momentum_into_zone: { en: 'Momentum into the zone too fast', icon: '🌊' },
  atr_floor_beyond_invalidation: {
    en: 'ATR stop floor past the authored invalidation',
    icon: '🛑',
  },
  breakeven_r_unknown_risk: {
    en: 'Breakeven in R — initial risk unknown (points rule used)',
    icon: '🎯',
  },
  // PEP MODE (owner order 2026-10-29) — every named refusal kernel/pep can
  // return (kernel/pep/types.go Code*). Pinned by pepGateLabels.test.ts, which
  // scans the Go constants, so a new refusal without a label is RED.
  pep_config_invalid: { en: 'Pep — settings invalid', icon: '🧭' },
  pep_tz_unavailable: { en: 'Pep — Chicago timezone unavailable', icon: '🧭' },
  pep_bars_not_ascending: { en: 'Pep — 1m bars out of order', icon: '🧭' },
  pep_no_bars: { en: 'Pep — no 1m bars', icon: '🧭' },
  pep_bars_stale: { en: 'Pep — stale 1m feed', icon: '🧭' },
  pep_after_flat_time: { en: 'Pep — after the flat time', icon: '🧭' },
  pep_max_trades_reached: {
    en: 'Pep — max trades for the day reached',
    icon: '🧭',
  },
  pep_position_open: { en: 'Pep — position already open', icon: '🧭' },
  pep_box_not_complete: { en: 'Pep — box window not closed yet', icon: '🧭' },
  pep_box_missing_minute: {
    en: 'Pep — box minute missing (no trade today)',
    icon: '🧭',
  },
  pep_box_zero_range: { en: 'Pep — box has zero range', icon: '🧭' },
  pep_box_too_wide: { en: 'Pep — box wider than the max box size', icon: '🧭' },
  pep_gap_after_box: {
    en: 'Pep — gap after the box (first break unknown)',
    icon: '🧭',
  },
  pep_no_break_yet: { en: 'Pep — no break of the box yet', icon: '🧭' },
  pep_bar_through_both_edges: {
    en: 'Pep — one bar through both box edges',
    icon: '🧭',
  },
  pep_stale_signal: {
    en: 'Pep — signal too old (not entered late)',
    icon: '🧭',
  },
  pep_ema_not_warm: { en: 'Pep — EMA not warmed up', icon: '🧭' },
  pep_no_cross_yet: { en: 'Pep — no EMA cross yet', icon: '🧭' },
  pep_stop_below_one_tick: { en: 'Pep — stop under one tick', icon: '🧭' },
}

export function GateBlocksPanel({
  traderId,
  language,
}: {
  traderId?: string
  language: Language
}) {
  const [rows, setRows] = useState<{ gate: string; count: number }[]>([])
  const [sessionDay, setSessionDay] = useState('')
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    if (!traderId) return
    let alive = true
    const load = async () => {
      const res = await planApi.getGateBlocks(traderId)
      if (!alive || !res) return
      const mine = res.by_trader?.[traderId] ?? {}
      // Process-wide gates (B3) are filed under the empty trader id; show them too
      // rather than dropping a real refusal on a technicality.
      const shared = res.by_trader?.[''] ?? {}
      const merged: Record<string, number> = { ...mine }
      for (const [g, n] of Object.entries(shared))
        merged[g] = (merged[g] ?? 0) + n
      setRows(
        Object.entries(merged)
          .map(([gate, count]) => ({ gate, count }))
          .sort((a, b) => b.count - a.count)
      )
      setSessionDay(res.session_day_utc ?? '')
      setLoaded(true)
    }
    void load()
    const t = setInterval(load, 20_000)
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [traderId])

  if (!traderId || !loaded) return null

  return (
    <div
      data-testid="gate-blocks-panel"
      className="p-3 flex flex-col gap-2"
      style={{
        background: 'var(--vl-card)',
        border: '1px solid var(--vl-hair)',
        borderRadius: 'var(--vl-radius-card)',
        fontFamily: 'var(--vl-font-ui)',
      }}
    >
      <div className="flex items-baseline justify-between">
        <span
          className="text-[10px] uppercase tracking-widest"
          style={{ color: 'var(--vl-faint)' }}
        >
          {tp('gateBlocksHeader', language)}
        </span>
        <span className="text-[9px]" style={{ color: 'var(--vl-faint)' }}>
          {sessionDay ? sessionDay.slice(0, 10) : ''}
        </span>
      </div>

      {rows.length === 0 ? (
        <div
          data-testid="gate-blocks-empty"
          className="text-[11px]"
          style={{ color: 'var(--vl-muted)' }}
        >
          {tp('gateBlocksNone', language)}
        </div>
      ) : (
        <div className="flex flex-col gap-1">
          {rows.map((r) => {
            const meta = GATE_LABELS[r.gate]
            return (
              <div
                key={r.gate}
                data-testid={`gate-block-${r.gate}`}
                className="flex items-center justify-between text-[11px]"
              >
                <span style={{ color: 'var(--vl-ivory)' }}>
                  {meta ? `${meta.icon} ${meta.en}` : r.gate}
                </span>
                <span
                  className="vl-num px-1.5 rounded"
                  style={{
                    color: 'var(--vl-short)',
                    background: 'rgba(224,108,108,0.10)',
                  }}
                >
                  {r.count}
                </span>
              </div>
            )
          })}
        </div>
      )}

      <div
        className="text-[9px] leading-snug"
        style={{ color: 'var(--vl-faint)' }}
      >
        {tp('gateBlocksNote', language)}
      </div>
    </div>
  )
}

export default GateBlocksPanel
