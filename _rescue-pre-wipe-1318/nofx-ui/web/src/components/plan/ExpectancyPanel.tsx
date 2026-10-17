// Wave 1D — the per-condition expectancy table.
//
// This panel RENDERS; it decides nothing. Two properties are load-bearing and
// both are pinned by tests:
//
//  1. It never shows a verdict the engine did not compute. min_n and the
//     promotion rule arrive IN the payload — the panel holds no copy of either,
//     so it cannot drift from the binary that computed the statuses.
//  2. It never renders an unmeasured statistic as a number. A null arrives as
//     an em dash. A 0 in the MAE column would read as "measured, and it was
//     zero", which is a different claim from "not measured yet".

import { useEffect, useState } from 'react'
import { planApi } from '../../lib/api/plan'

interface Cell {
  key: {
    condition?: string
    session?: string
    level_kind?: string
    path?: string
    era?: string
  }
  n: number
  wins: number
  losses: number
  flats: number
  sum_pnl_corrected: number
  mean: number
  sd: number
  win_rate: number
  wilson_lo: number
  wilson_hi: number
  mean_lo: number
  mean_hi: number
  t_stat: number
  median_mae: number | null
  median_mfe: number | null
  stop_hit_share: number | null
  target_hit_share: number | null
  excluded_unresolved: number
  row_ids: number[]
  descriptive_only: boolean
  status: string
}

interface E8Cell {
  key: { condition?: string; session?: string }
  rule?: string
  n: number
  // usable_n is how many of those n rows had a net_pnl worth arithmetic. When
  // it is 0 the mean is null and the row shows the reason instead of a number.
  usable_n: number
  wins: number
  losses: number
  mean: number | null
  excluded_price_scale: number
  excluded_zero_pnl: number
  counterfactual: boolean
  short_suspect: boolean
  note?: string
}

interface Payload {
  by: string
  rows: Cell[]
  counterfactual_e8: E8Cell[]
  excluded: {
    unresolved_pnl: number
    unresolvable: number
    test_seam: number
    no_condition: number
    crypto_era: number
  }
  min_n: number
  as_of_utc: string
  promotion_rule: string
}

// dash is the ONLY rendering of an absent statistic. Never 0, never "0.00".
const dash = '—'
const num = (v: number | null | undefined, dp = 2) =>
  v === null || v === undefined ? dash : v.toFixed(dp)
const pct = (v: number | null | undefined) =>
  v === null || v === undefined ? dash : `${(v * 100).toFixed(0)}%`

const label = (k: Cell['key']) =>
  [k.condition, k.session, k.level_kind, k.path, k.era]
    .filter(Boolean)
    .join(' · ') || 'all'

export function ExpectancyPanel() {
  const [data, setData] = useState<Payload | null>(null)
  const [open, setOpen] = useState<Record<number, boolean>>({})

  useEffect(() => {
    let alive = true
    const load = async () => {
      const res = (await planApi.getExpectancy()) as Payload | null
      if (alive && res) setData(res)
    }
    void load()
    const t = setInterval(load, 60_000)
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [])

  if (!data) return null

  const head = {
    color: 'var(--vl-faint)',
    fontFamily: 'var(--vl-font-ui)',
  } as const

  return (
    <div
      data-testid="expectancy-panel"
      className="p-3 flex flex-col gap-2"
      style={{
        background: 'var(--vl-card)',
        border: '1px solid var(--vl-hair)',
        borderRadius: 'var(--vl-radius-card)',
        fontFamily: 'var(--vl-font-ui)',
      }}
    >
      <div className="flex items-baseline justify-between">
        <span className="text-[10px] uppercase tracking-widest" style={head}>
          Expectancy · by {data.by}
        </span>
        {/* The data's own clock, not the page's. A table built now over stale
            rows must not look fresh. */}
        <span
          data-testid="expectancy-as-of"
          className="text-[9px]"
          style={head}
        >
          as of {data.as_of_utc ? data.as_of_utc.slice(0, 10) : dash}
        </span>
      </div>

      {data.rows.length === 0 ? (
        <div
          data-testid="expectancy-empty"
          className="text-[11px]"
          style={{ color: 'var(--vl-muted)' }}
        >
          No closed trades resolve to a play yet — nothing to measure. This is
          an empty sample, not a zero result.
        </div>
      ) : (
        <div className="flex flex-col gap-1">
          <div
            className="grid text-[9px] uppercase tracking-wider"
            style={{
              ...head,
              gridTemplateColumns:
                '1.6fr .4fr .5fr .6fr .7fr .5fr .5fr .8fr .4fr',
            }}
          >
            <span>row</span>
            <span className="text-right">n</span>
            <span className="text-right">w/l</span>
            <span className="text-right">mean</span>
            <span className="text-right">mean 95%</span>
            <span className="text-right">win%</span>
            <span className="text-right">MAE</span>
            <span className="text-right">status</span>
            <span className="text-right">ids</span>
          </div>

          {data.rows.map((r, i) => (
            <div key={i} className="flex flex-col">
              <div
                data-testid={`expectancy-row-${i}`}
                className="grid text-[11px] items-baseline"
                style={{
                  gridTemplateColumns:
                    '1.6fr .4fr .5fr .6fr .7fr .5fr .5fr .8fr .4fr',
                }}
              >
                <span style={{ color: 'var(--vl-ivory)' }}>{label(r.key)}</span>
                {/* n leads every row: the floor is a property of n, so the
                    reader meets the sample before meeting any statistic. */}
                <span
                  className="vl-num text-right"
                  style={{ color: 'var(--vl-ivory)' }}
                >
                  {r.n}
                </span>
                <span
                  className="vl-num text-right"
                  style={{ color: 'var(--vl-muted)' }}
                >
                  {r.wins}/{r.losses}
                </span>
                <span
                  className="vl-num text-right"
                  style={{
                    color: r.mean >= 0 ? 'var(--vl-long)' : 'var(--vl-short)',
                  }}
                >
                  {num(r.mean)}
                </span>
                <span
                  className="vl-num text-right"
                  style={{ color: 'var(--vl-muted)' }}
                >
                  {num(r.mean_lo)}…{num(r.mean_hi)}
                </span>
                <span
                  className="vl-num text-right"
                  style={{ color: 'var(--vl-muted)' }}
                >
                  {pct(r.win_rate)}
                </span>
                <span
                  data-testid={`expectancy-mae-${i}`}
                  className="vl-num text-right"
                  style={{ color: 'var(--vl-muted)' }}
                >
                  {num(r.median_mae, 1)}
                </span>
                <span
                  data-testid={`expectancy-status-${i}`}
                  className="text-right text-[10px]"
                  style={{
                    color: r.descriptive_only
                      ? 'var(--vl-faint)'
                      : r.status === 'PASSES'
                        ? 'var(--vl-long)'
                        : 'var(--vl-short)',
                  }}
                >
                  {/* Below the floor the panel says DESCRIPTIVE ONLY and shows
                      no verdict at all — not a greyed-out one. */}
                  {r.descriptive_only ? 'DESCRIPTIVE ONLY' : r.status}
                </span>
                <button
                  data-testid={`expectancy-ids-toggle-${i}`}
                  className="text-right text-[10px]"
                  style={{ color: 'var(--vl-faint)' }}
                  onClick={() => setOpen((o) => ({ ...o, [i]: !o[i] }))}
                >
                  {open[i] ? '−' : `${r.row_ids.length}`}
                </button>
              </div>
              {open[i] && (
                <div
                  data-testid={`expectancy-ids-${i}`}
                  className="vl-num text-[10px] pl-2 pb-1"
                  style={{ color: 'var(--vl-muted)', wordBreak: 'break-all' }}
                >
                  ids: {r.row_ids.join(', ')}
                  {r.excluded_unresolved > 0 &&
                    ` · ${r.excluded_unresolved} excluded (unresolved P&L)`}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {/* The honesty ledger. An excluded row that is not counted is a row that
          silently shrank the denominator. */}
      <div
        data-testid="expectancy-excluded"
        className="text-[10px]"
        style={{ color: 'var(--vl-faint)' }}
      >
        excluded — {data.excluded.unresolved_pnl} unresolved P&L ·{' '}
        {data.excluded.unresolvable} unresolvable · {data.excluded.test_seam}{' '}
        test-seam · {data.excluded.no_condition} no play. Floor: n ≥{' '}
        {data.min_n}. {data.promotion_rule}
      </div>

      {data.counterfactual_e8.length > 0 && (
        <div
          data-testid="expectancy-counterfactual"
          className="flex flex-col gap-1 pt-1"
          style={{ borderTop: '1px dashed var(--vl-hair)' }}
        >
          <span className="text-[9px] uppercase tracking-wider" style={head}>
            counterfactual (E8) — never comparable with a realized row above
          </span>
          {data.counterfactual_e8.map((c, i) => (
            <div
              key={i}
              data-testid={`expectancy-cf-${i}`}
              className="flex items-baseline justify-between text-[10px]"
              style={{ color: 'var(--vl-muted)' }}
            >
              <span>
                {[c.key.condition, c.key.session, c.rule]
                  .filter(Boolean)
                  .join(' · ')}
              </span>
              <span className="vl-num">
                n {c.n} ({c.usable_n} usable) · {c.wins}/{c.losses} · mean{' '}
                {num(c.mean)}
                {c.excluded_price_scale > 0
                  ? ` · ${c.excluded_price_scale} not a P&L`
                  : ''}
                {c.excluded_zero_pnl > 0
                  ? ` · ${c.excluded_zero_pnl} uncomputed`
                  : ''}
                {c.short_suspect ? ' · SHORT ROWS SUSPECT (E8 sign bug)' : ''}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
