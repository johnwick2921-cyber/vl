// C-fix (2026-08-18) — minimal FE consumers for the two discipline endpoints
// that had real data and ZERO frontend readers:
//   GET /api/plan/trades  → adherence grades + GPA (P5.5)
//   GET /api/plan/stats   → frozen weekly matched-random verdicts + warming
//                            progress (P5.6, never recomputed live)
//
// Honesty rules this panel exactly as the endpoints do: no graded trades yet →
// says so; weekly is null until the first Sunday eval → says so; progress is
// labelled WARMING with raw counts and no significance claim.

import { useEffect, useState } from 'react'
import {
  planApi,
  type PlanStatsResponse,
  type PlanTradesResponse,
} from '../../lib/api/plan'
import type { Language } from '../../i18n/translations'

export function DisciplinePanel({
  traderId,
  language,
}: {
  traderId?: string
  language: Language
}) {
  const [trades, setTrades] = useState<PlanTradesResponse | null>(null)
  const [stats, setStats] = useState<PlanStatsResponse | null>(null)

  useEffect(() => {
    if (!traderId) return
    let alive = true
    const load = async () => {
      const [t, s] = await Promise.all([
        planApi.getPlanTrades(traderId),
        planApi.getPlanStats(traderId),
      ])
      if (!alive) return
      setTrades(t)
      setStats(s)
    }
    load()
    return () => {
      alive = false
    }
  }, [traderId])

  if (!traderId || !trades || !stats) return null

  const { summary } = trades
  const en = language !== 'zh'
  const gradeChips = ['A', 'B', 'C', 'D', 'F']
    .map((g) => {
      const n = summary.counts?.[g] ?? 0
      if (n === 0) return ''
      return `${g}×${n}`
    })
    .filter(Boolean)
    .join(' ')

  // MAE/MFE viz (4.4) — same averaging rule as the digest learning line:
  // count only trades carrying excursion data (mae > 0 || mfe > 0).
  const excursions = trades.trades.filter((t) => t.mae > 0 || t.mfe > 0)
  const avgMAE =
    excursions.length > 0
      ? excursions.reduce((s, t) => s + t.mae, 0) / excursions.length
      : 0
  const avgMFE =
    excursions.length > 0
      ? excursions.reduce((s, t) => s + t.mfe, 0) / excursions.length
      : 0
  const excMax = Math.max(avgMAE, avgMFE, 0.01)

  return (
    <div
      className="flex flex-col gap-1 rounded-lg px-3 py-2 text-xs"
      style={{
        background: 'var(--vl-card-2, rgba(127,127,127,0.06))',
        border: '1px solid var(--vl-hair)',
      }}
    >
      <div className="flex items-baseline gap-2">
        <span className="font-semibold" style={{ color: 'var(--vl-muted)' }}>
          {en ? 'DISCIPLINE' : '纪律'}
        </span>
        {summary.total > 0 ? (
          <span>
            {en ? 'adherence' : '计划贴合度'}{' '}
            <span className="font-mono font-semibold">
              GPA {summary.gpa.toFixed(2)}
            </span>{' '}
            <span style={{ color: 'var(--vl-muted)' }}>({summary.total})</span>{' '}
            {gradeChips && <span className="font-mono">{gradeChips}</span>}
          </span>
        ) : (
          <span style={{ color: 'var(--vl-muted)' }}>
            {en ? 'no graded trades yet' : '暂无已评级交易'}
          </span>
        )}
      </div>
      {excursions.length > 0 && (
        <div className="flex items-center gap-2">
          <span className="font-semibold" style={{ color: 'var(--vl-muted)' }}>
            {en ? 'MAE/MFE' : 'MAE/MFE'}
          </span>
          <span className="font-mono" style={{ color: 'var(--vl-short)' }}>
            −{avgMAE.toFixed(1)}
          </span>
          <span
            className="font-mono"
            style={{ color: 'var(--vl-long, #26a69a)' }}
          >
            +{avgMFE.toFixed(1)}
          </span>
          <span style={{ color: 'var(--vl-muted)' }}>
            pts · n={excursions.length}
          </span>
          <span className="flex items-center gap-0.5">
            <span
              title={`avg MAE ${avgMAE.toFixed(1)} pts`}
              style={{
                display: 'inline-block',
                width: `${Math.max(4, (avgMAE / excMax) * 48)}px`,
                height: '4px',
                borderRadius: '2px',
                background: 'var(--vl-short)',
                opacity: 0.85,
              }}
            />
            <span
              title={`avg MFE ${avgMFE.toFixed(1)} pts`}
              style={{
                display: 'inline-block',
                width: `${Math.max(4, (avgMFE / excMax) * 48)}px`,
                height: '4px',
                borderRadius: '2px',
                background: 'var(--vl-long, #26a69a)',
                opacity: 0.85,
              }}
            />
          </span>
        </div>
      )}
      <div className="flex items-baseline gap-2">
        <span className="font-semibold" style={{ color: 'var(--vl-muted)' }}>
          {en ? 'RANDOM-GATE' : '随机对照'}
        </span>
        {stats.weekly ? (
          <span>
            {en
              ? `frozen ${stats.weekly.iso_week}:`
              : `已冻结 ${stats.weekly.iso_week}：`}{' '}
            {(stats.weekly.verdicts ?? [])
              .map((v) => `${v.level_type} ${v.status}`)
              .join(' · ') || (en ? 'no verdicts' : '无结论')}
          </span>
        ) : (
          <span style={{ color: 'var(--vl-muted)' }}>
            {en ? 'first eval Sunday' : '首次评估：周日'}
          </span>
        )}
      </div>
      {stats.progress.length > 0 && (
        <div
          className="flex flex-wrap gap-x-3"
          style={{ color: 'var(--vl-muted)' }}
        >
          {stats.progress.map((p) => (
            <span key={p.level_type}>
              {p.level_type}{' '}
              <span className="font-mono">
                {p.n}/{p.target_n}
              </span>{' '}
              {p.warming ? (en ? 'WARMING' : '预热中') : ''}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
