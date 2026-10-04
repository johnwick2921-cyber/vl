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
// refusal the owner must see.
const GATE_LABELS: Record<string, { en: string; icon: string }> = {
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
  night_transition: { en: 'Night/day transition', icon: '🌙' },
  maintenance_hold: { en: 'Update hold (maintenance)', icon: '🔒' },
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
