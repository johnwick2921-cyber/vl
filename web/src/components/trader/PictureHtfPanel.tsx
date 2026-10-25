// W-PICTURE-HTF (2026-09-20) — the two-picture opportunity panel: intended
// geometry vs the broker's answer, side by side. Read-only; every missing
// broker field renders as a dash, never a fabricated success.

import { useEffect, useState } from 'react'
import { httpClient } from '../../lib/httpClient'

export interface PictureHtfRow {
  opp_key: string
  stage: string
  stage_reason: string
  symbol: string
  direction: string
  level_role: string
  level_body_top: number
  h1_close_time_ms: number
  window_close_ms: number
  entry_ref: number
  stop_px: number
  target_px: number
  rr_estimate: number
  rr_configured: number
  momentum_stall: boolean
  signal_id: string
  submitted_at_ms: number
  broker_order_id: string
  broker_status: string
  fill_price: number
  fill_qty: number
  fill_rr: number
  reject_reason: string
  created_at_ms: number
  /** W-EXEC-TRUTH W5 — the Day Plan scenario carrying this opportunity (the
   * armed_orders row whose source_ref is opp_key). ABSENT when there is none. */
  plan_link?: PictureHtfPlanLink
}

export interface PictureHtfPlanLink {
  plan_id: string
  scenario_id: string
  plan_version: number
  arm_row_id: number
  arm_state: string
  arm_signal_id?: string
}

/** "→ Day Plan P1 · <plan_id> v3 · arm #52 working" — READ from the link;
 * a missing number reads "n/a". */
export function planLinkText(l: PictureHtfPlanLink): string {
  const n = (v: unknown) =>
    typeof v === 'number' && Number.isFinite(v) && v > 0 ? String(v) : 'n/a'
  return `→ Day Plan ${l.scenario_id || 'n/a'} · ${l.plan_id || 'n/a'} v${n(l.plan_version)} · arm #${n(l.arm_row_id)} ${l.arm_state || 'n/a'}`
}

const num = (v: number | undefined, digits = 2) =>
  v === undefined || v === 0 || Number.isNaN(v) ? '—' : v.toFixed(digits)

const age = (ms: number | undefined) => {
  if (!ms) return '—'
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000))
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m`
}

function Row({ r }: { r: PictureHtfRow }) {
  const long = r.direction === 'long'
  return (
    <tr className="border-t border-white/5">
      <td className="px-1 py-2 whitespace-nowrap">
        <span className={long ? 'text-emerald-400' : 'text-rose-400'}>
          {long ? 'LONG' : 'SHORT'}
        </span>
        <span className="text-vl-neo-text-muted">
          {' '}
          {r.level_role} {num(r.level_body_top)}
        </span>
      </td>
      <td className="px-1 py-2 text-right whitespace-nowrap">
        <div>entry {num(r.entry_ref)}</div>
        <div className="text-vl-neo-text-muted text-[10px]">
          stop {num(r.stop_px)} · target {num(r.target_px)}
        </div>
        <div className="text-vl-neo-text-muted text-[10px]">
          R:R {num(r.rr_estimate)} (min {num(r.rr_configured)})
        </div>
      </td>
      <td className="px-1 py-2 whitespace-nowrap">
        <span className="font-mono text-[11px]">
          {r.signal_id ? r.signal_id.slice(0, 13) : '—'}
        </span>
        <div className="text-vl-neo-text-muted text-[10px]">
          sent {age(r.submitted_at_ms)} ago
        </div>
      </td>
      <td className="px-1 py-2 whitespace-nowrap">
        <span className="font-mono text-[11px]">
          {r.broker_status || '—'}
          {r.broker_order_id ? ` #${r.broker_order_id.slice(0, 8)}` : ''}
        </span>
        <div className="text-vl-neo-text-muted text-[10px]">
          {r.fill_price
            ? `fill ${num(r.fill_price)} × ${num(r.fill_qty, 0)}`
            : 'no fill yet'}
          {r.fill_rr ? ` · R:R ${num(r.fill_rr)}` : ''}
        </div>
        {r.reject_reason && (
          <div className="text-rose-400 text-[10px]">{r.reject_reason}</div>
        )}
      </td>
      <td className="px-1 py-2 text-right whitespace-nowrap">
        <span className="font-mono text-[11px] text-vl-neo-gold">
          {r.stage}
        </span>
        {r.stage_reason && (
          <div className="text-vl-neo-text-muted text-[10px] max-w-[220px] truncate">
            {r.stage_reason}
          </div>
        )}
        {r.plan_link && (
          <div
            data-testid={`picture-plan-link-${r.opp_key}`}
            className="text-vl-neo-text-muted text-[10px]"
            title={
              r.plan_link.arm_signal_id
                ? `arm signal ${r.plan_link.arm_signal_id}`
                : undefined
            }
          >
            {planLinkText(r.plan_link)}
          </div>
        )}
      </td>
    </tr>
  )
}

export function PictureHtfPanel({ traderId }: { traderId: string }) {
  const [rows, setRows] = useState<PictureHtfRow[] | null>(null)
  // W5 — the server could not read the Day Plan links: say so, so an absent
  // link is never read as "never became a Day Plan scenario".
  const [linksUnread, setLinksUnread] = useState('')

  useEffect(() => {
    if (!traderId) return
    let live = true
    const poll = () => {
      // C11: the shared httpClient attaches the Bearer token; the old bare
      // fetch() had no auth header, so every poll was a 401 and the panel was
      // always empty.
      httpClient
        .request<{ rows?: PictureHtfRow[]; plan_links_unread?: string }>(
          `/api/picture-htf/opportunities?trader_id=${encodeURIComponent(traderId)}`,
          { silent: true }
        )
        .then((res) => {
          if (!live) return
          const j = res.success ? res.data : undefined
          setRows(Array.isArray(j?.rows) ? j.rows : [])
          setLinksUnread(
            typeof j?.plan_links_unread === 'string' ? j.plan_links_unread : ''
          )
        })
        .catch(() => live && setRows(null))
    }
    poll()
    const iv = setInterval(poll, 30_000)
    return () => {
      live = false
      clearInterval(iv)
    }
  }, [traderId])

  return (
    <section
      className="min-w-0 vl-neo-glass p-6 animate-slide-in relative overflow-hidden group"
      style={{ animationDelay: '0.2s' }}
    >
      <div className="absolute top-0 right-0 p-3 opacity-10 group-hover:opacity-20 transition-opacity">
        <div className="w-24 h-24 rounded-full bg-indigo-500 blur-3xl" />
      </div>
      <div className="flex items-center justify-between mb-5 relative z-10">
        <h2 className="text-lg font-bold flex items-center gap-2 text-vl-neo-text-main uppercase tracking-wide">
          <span className="text-indigo-400">📷</span> Picture HTF opportunities
        </h2>
        {rows !== null && (
          <span className="text-xs text-vl-neo-text-muted font-mono">
            {rows.length} recorded
          </span>
        )}
      </div>
      {linksUnread && (
        <div
          data-testid="picture-plan-links-unread"
          className="text-[10px] text-rose-400 mb-2"
        >
          Day Plan links unread: {linksUnread}
        </div>
      )}
      {rows === null ? (
        <div className="text-xs text-vl-neo-text-muted">
          reading the ledger…
        </div>
      ) : rows.length === 0 ? (
        <div className="text-xs text-vl-neo-text-muted">
          No two-picture opportunities recorded — the mode evaluates native
          4H/1H/5m bar events (enabled in the strategy's Day Plan block).
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead className="text-left border-b border-white/5">
              <tr>
                <th className="px-1 pb-3 font-semibold text-vl-neo-text-muted whitespace-nowrap text-left">
                  Setup
                </th>
                <th className="px-1 pb-3 font-semibold text-vl-neo-text-muted whitespace-nowrap text-right">
                  Intended
                </th>
                <th className="px-1 pb-3 font-semibold text-vl-neo-text-muted whitespace-nowrap text-left">
                  Command
                </th>
                <th className="px-1 pb-3 font-semibold text-vl-neo-text-muted whitespace-nowrap text-left">
                  Broker answer
                </th>
                <th className="px-1 pb-3 font-semibold text-vl-neo-text-muted whitespace-nowrap text-right">
                  Stage
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <Row key={r.opp_key} r={r} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
