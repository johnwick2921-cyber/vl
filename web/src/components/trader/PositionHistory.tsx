import { useState, useEffect, useMemo } from 'react'
import { api } from '../../lib/api'
import { useAutoRefresh, REFRESH_HISTORY_MS } from '../../lib/autoRefresh'
import { useLanguage } from '../../contexts/LanguageContext'
import { t, type Language } from '../../i18n/translations'
import { MetricTooltip } from '../common/MetricTooltip'
import { formatPrice, formatQuantity } from '../../utils/format'
import { VlSelect } from '../ui/select'
import type {
  HistoricalPosition,
  TraderStats,
  SymbolStats,
  DirectionStats,
} from '../../types'

interface PositionHistoryProps {
  traderId: string
}

// Format number with proper decimals (for large numbers)
function formatNumber(value: number, decimals: number = 2): string {
  if (Math.abs(value) >= 1000000) {
    return (value / 1000000).toFixed(2) + 'M'
  }
  if (Math.abs(value) >= 1000) {
    return (value / 1000).toFixed(2) + 'K'
  }
  return value.toFixed(decimals)
}

// Format duration from minutes
function formatDuration(minutes: number): string {
  if (!minutes || minutes <= 0) return '-'
  if (minutes < 60) return `${minutes.toFixed(0)}m`
  if (minutes < 1440) return `${(minutes / 60).toFixed(1)}h`
  return `${(minutes / 1440).toFixed(1)}d`
}

// Format date
function formatDate(dateStr: string): string {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  if (isNaN(date.getTime())) return '-'
  return date.toLocaleDateString('zh-CN', {
    timeZone: 'America/Chicago', // owner contract: Houston time for every viewer
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

// ── LEDGER-SURFACE HONESTY (2026-08-31, no engine changes) ─────────────────
// The ledger excludes close_reason IN (reconcile_flat, unresolved) from every
// P&L sum, and the E7 closeout promised e7_farside_test rows are excluded from
// strategy P&L. This surface mirrors those rules so the VISIBLE rows and the
// VISIBLE totals agree with the ledger.

// Reasons whose P&L is UNKNOWN (rendered "—", never summed).
export const UNKNOWN_PNL_REASONS = ['reconcile_flat', 'unresolved']
// Test-seam rows (E7 far-side experiments) — never real trades, never summed.
export const TEST_SEAM_REASONS = ['e7_farside_test']

export type PositionKind =
  | 'normal'
  | 'unresolved'
  | 'duplicate'
  | 'hidden'
  | 'manual'

// R22 PNL-TRUTH (owner ruling R1): an NT8 position the bot adopted with no
// order/arm of its own. LISTED with a badge, NEVER in a bot total — the
// server's store.IsManualOrigin twin.
export function isManualOrigin(p: Pick<HistoricalPosition, 'origin'>): boolean {
  return (p.origin ?? '').trim().toLowerCase() === 'manual'
}

// effectivePnl mirrors the store aggregate rule: corrections win, then realized.
export function effectivePnl(p: HistoricalPosition): number {
  if (typeof p.pnl_corrected === 'number' && !isNaN(p.pnl_corrected)) {
    return p.pnl_corrected
  }
  return p.realized_pnl || 0
}

// classifyPosition — deterministic state per the dispatch:
//   unresolved  → shown, "—" + "exit unknown" badge
//   duplicate   → hidden unless the "show duplicates" toggle; greyed + "duplicate of #N"
//   hidden      → reconcile_flat without duplicate evidence + test-seam rows — excluded entirely
//   normal      → shown, P&L rendered
// A duplicate is a reconcile_flat row whose entry_order_id matches a REAL
// (non-unknown, non-test) close — the 575/576 and 578/577 pairs.
export function classifyPosition(
  p: HistoricalPosition,
  all: HistoricalPosition[]
): PositionKind {
  const reason = (p.close_reason || '').trim()
  if (TEST_SEAM_REASONS.includes(reason)) return 'hidden'
  if (isManualOrigin(p)) return 'manual'
  if (reason === 'unresolved') return 'unresolved'
  if (UNKNOWN_PNL_REASONS.includes(reason)) {
    const dupeOf = all.find(
      (q) =>
        q.id !== p.id &&
        !!q.entry_order_id &&
        q.entry_order_id === p.entry_order_id &&
        !UNKNOWN_PNL_REASONS.includes((q.close_reason || '').trim()) &&
        !TEST_SEAM_REASONS.includes((q.close_reason || '').trim())
    )
    return dupeOf ? 'duplicate' : 'hidden'
  }
  return 'normal'
}

// duplicateOfId returns the REAL row id a duplicate row shadows (0 = none).
export function duplicateOfId(
  p: HistoricalPosition,
  all: HistoricalPosition[]
): number {
  const reason = (p.close_reason || '').trim()
  if (!UNKNOWN_PNL_REASONS.includes(reason)) return 0
  const dupeOf = all.find(
    (q) =>
      q.id !== p.id &&
      !!q.entry_order_id &&
      q.entry_order_id === p.entry_order_id &&
      !UNKNOWN_PNL_REASONS.includes((q.close_reason || '').trim()) &&
      !TEST_SEAM_REASONS.includes((q.close_reason || '').trim())
  )
  return dupeOf ? dupeOf.id : 0
}

// manualFooterLine is the visible separate line for the manual rows in the
// list: "Manual (excluded): 4 trades, −$3,202.00" (resolved P&L only).
export function manualFooterLine(rows: HistoricalPosition[]): string {
  const n = rows.length
  if (n === 0) return 'Manual (excluded): none'
  let sum = 0
  for (const p of rows) {
    if (typeof p.pnl_corrected === 'number' && !isNaN(p.pnl_corrected)) {
      sum += p.pnl_corrected
    }
  }
  sum = Math.round(sum * 100) / 100
  const abs = Math.abs(sum).toLocaleString('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
  return `Manual (excluded): ${n} trade${n === 1 ? '' : 's'}, ${sum >= 0 ? '+' : '−'}$${abs}`
}

// computeDayTotal mirrors the ledger's GetSessionDayActivity rule exactly:
// rows closed in the CURRENT CME SESSION DAY, close_reason NOT
// unknown/test-seam, not a duplicate, not manual, and pnl_corrected present
// (legacy unverified rows excluded — the A-2 rule).
//
// historyScopeLabel (RT-7, release #24): the scope of the history total, as
// the server names it ("all accounts" — /positions/history reads GetFullStats
// with no account), so it cannot be mistaken for the account-scoped
// "Bot P&L (record)" card. An older server sends none: "scope n/a".
export function historyScopeLabel(scope?: string): string {
  return scope && scope.trim() !== '' ? scope : 'scope n/a'
}

// R22 PNL-TRUTH: ONE "today". This used the browser's CT CALENDAR day while
// every risk gate counts from 17:00 CT; the session-day start now comes from
// the server (kernel.CMESessionDayStart via /positions/history). Without it
// the day total is UNKNOWN — null, never a guessed window.
export function computeDayTotal(
  positions: HistoricalPosition[],
  sessionDayStartMs?: number
): number | null {
  if (typeof sessionDayStartMs !== 'number' || sessionDayStartMs <= 0) {
    return null
  }
  const kinds = new Map<number, PositionKind>()
  for (const p of positions) {
    kinds.set(p.id, classifyPosition(p, positions))
  }
  let total = 0
  for (const p of positions) {
    if (kinds.get(p.id) !== 'normal') continue
    const exitMs = new Date(p.exit_time || 0).getTime()
    if (!exitMs || exitMs < sessionDayStartMs) continue
    if (typeof p.pnl_corrected !== 'number' || isNaN(p.pnl_corrected)) continue
    total += p.pnl_corrected
  }
  return Math.round(total * 100) / 100
}

// Stats Card Component with formula tooltip
function StatCard({
  title,
  value,
  suffix,
  color,
  icon,
  subtitle,
  metricKey,
  language = 'en',
}: {
  title: string
  value: string | number
  suffix?: string
  color?: string
  icon: string
  subtitle?: string
  metricKey?: string
  language?: string
}) {
  return (
    <div
      className="rounded-lg p-4 transition-all duration-200 hover:scale-[1.02]"
      style={{
        background: 'linear-gradient(135deg, #1E2329 0%, #181C21 100%)',
        border: '1px solid #2B3139',
        boxShadow: '0 4px 12px rgba(0, 0, 0, 0.2)',
      }}
    >
      <div className="flex items-center gap-2 mb-2">
        <span className="text-lg">{icon}</span>
        <span className="text-xs" style={{ color: '#848E9C' }}>
          {title}
        </span>
        {metricKey && (
          <MetricTooltip metricKey={metricKey} language={language} size={12} />
        )}
      </div>
      <div className="flex items-baseline gap-1">
        <span
          className="text-xl font-bold font-mono"
          style={{ color: color || '#EAECEF' }}
        >
          {value}
        </span>
        {suffix && (
          <span className="text-sm" style={{ color: '#848E9C' }}>
            {suffix}
          </span>
        )}
      </div>
      {subtitle && (
        <div className="text-xs mt-1" style={{ color: '#848E9C' }}>
          {subtitle}
        </div>
      )}
    </div>
  )
}

// Symbol Stats Row
function SymbolStatsRow({ stat }: { stat: SymbolStats }) {
  const totalPnl = stat.total_pnl || 0
  const winRate = stat.win_rate || 0
  const pnlColor = totalPnl >= 0 ? '#0ECB81' : '#F6465D'
  const winRateColor =
    winRate >= 60 ? '#0ECB81' : winRate >= 40 ? '#F0B90B' : '#F6465D'

  return (
    <div
      className="flex items-center justify-between p-3 rounded-lg transition-all duration-200 hover:bg-white/5"
      style={{ borderBottom: '1px solid #2B3139' }}
    >
      <div className="flex items-center gap-3">
        <span className="font-mono font-semibold" style={{ color: '#EAECEF' }}>
          {stat.symbol || ''}
        </span>
        <span className="text-xs" style={{ color: '#848E9C' }}>
          {stat.total_trades || 0} trades
        </span>
      </div>
      <div className="flex items-center gap-6">
        <div className="text-right">
          <div className="text-xs" style={{ color: '#848E9C' }}>
            Win Rate
          </div>
          <div
            className="font-mono font-semibold"
            style={{ color: winRateColor }}
          >
            {winRate.toFixed(1)}%
          </div>
        </div>
        <div className="text-right min-w-[80px]">
          <div className="text-xs" style={{ color: '#848E9C' }}>
            P&L
          </div>
          <div className="font-mono font-semibold" style={{ color: pnlColor }}>
            {totalPnl >= 0 ? '+' : ''}
            {formatNumber(totalPnl)}
          </div>
        </div>
      </div>
    </div>
  )
}

// Direction Stats Card
function DirectionStatsCard({
  stat,
  language,
}: {
  stat: DirectionStats
  language: Language
}) {
  const isLong = (stat.side || '').toLowerCase() === 'long'
  const iconColor = isLong ? '#0ECB81' : '#F6465D'
  const totalPnl = stat.total_pnl || 0
  const winRate = stat.win_rate || 0
  const tradeCount = stat.trade_count || 0
  const avgPnl = stat.avg_pnl || 0
  const pnlColor = totalPnl >= 0 ? '#0ECB81' : '#F6465D'

  return (
    <div
      className="rounded-lg p-4"
      style={{
        background: 'linear-gradient(135deg, #1E2329 0%, #181C21 100%)',
        border: `1px solid ${iconColor}33`,
      }}
    >
      <div className="flex items-center gap-2 mb-3">
        <span className="text-xl">{isLong ? '📈' : '📉'}</span>
        <span className="font-bold uppercase" style={{ color: iconColor }}>
          {stat.side || 'Unknown'}
        </span>
      </div>
      <div className="grid grid-cols-4 gap-4">
        <div>
          <div className="text-xs mb-1" style={{ color: '#848E9C' }}>
            {t('positionHistory.trades', language)}
          </div>
          <div className="font-mono font-semibold" style={{ color: '#EAECEF' }}>
            {tradeCount}
          </div>
        </div>
        <div>
          <div className="text-xs mb-1" style={{ color: '#848E9C' }}>
            {t('positionHistory.winRate', language)}
          </div>
          <div
            className="font-mono font-semibold"
            style={{
              color:
                winRate >= 60
                  ? '#0ECB81'
                  : winRate >= 40
                    ? '#F0B90B'
                    : '#F6465D',
            }}
          >
            {winRate.toFixed(1)}%
          </div>
        </div>
        <div>
          <div className="text-xs mb-1" style={{ color: '#848E9C' }}>
            {t('positionHistory.totalPnL', language)}
          </div>
          <div className="font-mono font-semibold" style={{ color: pnlColor }}>
            {totalPnl >= 0 ? '+' : ''}
            {formatNumber(totalPnl)}
          </div>
        </div>
        <div>
          <div className="text-xs mb-1" style={{ color: '#848E9C' }}>
            {t('positionHistory.avgPnL', language)}
          </div>
          <div
            className="font-mono font-semibold"
            style={{ color: avgPnl >= 0 ? '#0ECB81' : '#F6465D' }}
          >
            {avgPnl >= 0 ? '+' : ''}
            {formatNumber(avgPnl)}
          </div>
        </div>
      </div>
    </div>
  )
}

// Position Row Component
function PositionRow({
  position,
  kind,
  duplicateOf,
}: {
  position: HistoricalPosition
  kind: PositionKind
  duplicateOf: number
}) {
  const side = position.side || ''
  const isLong = side.toUpperCase() === 'LONG'
  const realizedPnl = effectivePnl(position)
  // Canon class 40: a NULL pnl_corrected is UNRESOLVED — it must READ
  // "unresolved", never 0 or a fabricated realized_pnl number.
  const pnlUnresolved =
    typeof position.pnl_corrected !== 'number' || isNaN(position.pnl_corrected)
  // unresolved and duplicate rows render "—" (their P&L is unknown / not a
  // real trade). normal rows render the ledger-effective number.
  const pnlUnknown =
    !pnlUnresolved && (kind === 'unresolved' || kind === 'duplicate')
  const isProfitable = realizedPnl >= 0
  const sideColor = isLong ? '#0ECB81' : '#F6465D'
  const pnlColor = isProfitable ? '#0ECB81' : '#F6465D'

  // Calculate holding time
  const entryTime = position.entry_time
    ? new Date(position.entry_time).getTime()
    : 0
  const exitTime = position.exit_time
    ? new Date(position.exit_time).getTime()
    : 0
  const holdingMinutes =
    entryTime && exitTime && exitTime > entryTime
      ? (exitTime - entryTime) / 60000
      : 0

  // Calculate PnL percentage based on entry price
  const entryPrice = position.entry_price || 0
  const exitPrice = position.exit_price || 0
  let pnlPct = 0
  if (entryPrice > 0) {
    if (isLong) {
      pnlPct = ((exitPrice - entryPrice) / entryPrice) * 100
    } else {
      pnlPct = ((entryPrice - exitPrice) / entryPrice) * 100
    }
  }

  // Use entry_quantity for display (original position size)
  const displayQty = position.entry_quantity || position.quantity || 0

  return (
    <tr
      className="transition-all duration-200 hover:bg-white/5"
      style={{
        borderBottom: '1px solid #2B3139',
        opacity: kind === 'duplicate' ? 0.55 : 1,
      }}
    >
      {/* Symbol */}
      <td className="py-3 px-4">
        <div className="flex items-center gap-2">
          <span
            className="font-mono font-semibold"
            style={{ color: '#EAECEF' }}
          >
            {position.symbol || ''}
          </span>
          <span
            className="px-2 py-0.5 rounded text-xs font-semibold uppercase"
            style={{
              background: `${sideColor}22`,
              color: sideColor,
              border: `1px solid ${sideColor}44`,
            }}
          >
            {side}
          </span>
          {kind === 'duplicate' && duplicateOf > 0 && (
            <span
              className="px-2 py-0.5 rounded text-xs font-semibold"
              style={{
                background: '#2B3139',
                color: '#848E9C',
                border: '1px solid #3A424B',
              }}
              title="Same NT8 position as another row — not a separate trade"
            >
              duplicate of #{duplicateOf}
            </span>
          )}
          {kind === 'manual' && (
            <span
              data-testid="manual-origin-badge"
              className="px-2 py-0.5 rounded text-xs font-semibold"
              style={{
                background: '#2B3139',
                color: '#F0B90B',
                border: '1px solid #3A424B',
              }}
              title="Manual NT8 trade: the bot adopted this position with no order or arm of its own. It is listed here but is NOT in any bot total (owner ruling R1)."
            >
              MANUAL · excluded
            </span>
          )}
          {kind === 'unresolved' && (
            <span
              className="px-2 py-0.5 rounded text-xs font-semibold"
              style={{
                background: '#2B3139',
                color: '#F0B90B',
                border: '1px solid #3A424B',
              }}
              title="NT8 went flat but no close frame and no netting fill exist — the real exit price is unknown"
            >
              exit unknown
            </span>
          )}
        </div>
      </td>

      {/* Entry Price */}
      <td
        className="py-3 px-4 text-right font-mono"
        style={{ color: '#EAECEF' }}
      >
        {formatPrice(entryPrice)}
      </td>

      {/* Exit Price */}
      <td
        className="py-3 px-4 text-right font-mono"
        style={{ color: '#EAECEF' }}
      >
        {formatPrice(exitPrice)}
      </td>

      {/* Quantity */}
      <td
        className="py-3 px-4 text-right font-mono"
        style={{ color: '#848E9C' }}
      >
        {formatQuantity(displayQty)}
      </td>

      {/* Position Value (Entry Price * Quantity) */}
      <td
        className="py-3 px-4 text-right font-mono"
        style={{ color: '#EAECEF' }}
      >
        {formatNumber(entryPrice * displayQty)}
      </td>

      {/* P&L */}
      <td className="py-3 px-4 text-right">
        {pnlUnresolved ? (
          <div
            className="font-mono font-semibold"
            style={{ color: '#F0B90B' }}
            title="pnl_corrected is NULL — the real exit P&L is unknown (excluded from every total)"
          >
            unresolved
          </div>
        ) : pnlUnknown ? (
          <div
            className="font-mono font-semibold"
            style={{ color: '#848E9C' }}
            title={
              kind === 'duplicate'
                ? 'Duplicate row — the P&L belongs to the real trade it shadows'
                : 'Exit unknown — NT8 went flat without a close frame or netting fill'
            }
          >
            —
          </div>
        ) : (
          <>
            <div
              className="font-mono font-semibold"
              style={{ color: pnlColor }}
            >
              {isProfitable ? '+' : ''}
              {formatNumber(realizedPnl)}
            </div>
            <div className="text-xs" style={{ color: pnlColor }}>
              {pnlPct >= 0 ? '+' : ''}
              {pnlPct.toFixed(2)}%
            </div>
          </>
        )}
      </td>

      {/* Fee - show more precision for small fees */}
      <td
        className="py-3 px-4 text-right font-mono text-xs"
        style={{ color: '#848E9C' }}
      >
        -
        {(position.fee || 0) < 0.01 && (position.fee || 0) > 0
          ? (position.fee || 0).toFixed(4)
          : (position.fee || 0).toFixed(2)}
      </td>

      {/* Duration */}
      <td
        className="py-3 px-4 text-center text-sm"
        style={{ color: '#848E9C' }}
      >
        {formatDuration(holdingMinutes)}
      </td>

      {/* Exit Time */}
      <td className="py-3 px-4 text-right text-xs" style={{ color: '#848E9C' }}>
        {formatDate(position.exit_time)}
      </td>
    </tr>
  )
}

export function PositionHistory({ traderId }: PositionHistoryProps) {
  const { language } = useLanguage()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [positions, setPositions] = useState<HistoricalPosition[]>([])
  const [stats, setStats] = useState<TraderStats | null>(null)
  const [symbolStats, setSymbolStats] = useState<SymbolStats[]>([])
  const [directionStats, setDirectionStats] = useState<DirectionStats[]>([])
  const [sessionDayStartMs, setSessionDayStartMs] = useState<
    number | undefined
  >(undefined)
  const [statsScope, setStatsScope] = useState<string | undefined>(undefined)

  // Pagination state
  const [pageSize, setPageSize] = useState<number>(20)
  const [currentPage, setCurrentPage] = useState<number>(1)

  // Filter state
  const [filterSymbol, setFilterSymbol] = useState<string>('all')
  const [filterSide, setFilterSide] = useState<string>('all')
  const [sortBy, setSortBy] = useState<'time' | 'pnl' | 'pnl_pct'>('time')
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc')
  const [showDuplicates, setShowDuplicates] = useState<boolean>(false)

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        setError(null)
        // Fetch more data than needed to support filtering, but respect pageSize for initial load
        const data = await api.getPositionHistory(
          traderId,
          Math.max(200, pageSize * 5),
          true
        )
        setPositions(data.positions || [])
        setStats(data.stats)
        setSymbolStats(data.symbol_stats || [])
        setDirectionStats(data.direction_stats || [])
        setSessionDayStartMs(data.session_day_start_ms)
        setStatsScope(data.stats_scope)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load history')
      } finally {
        setLoading(false)
      }
    }

    if (traderId) {
      fetchData()
    }
  }, [traderId, pageSize])

  // Silent background refresh (auto-refresh layer): re-pulls the same window
  // WITHOUT the loading spinner, so new closed trades appear on their own.
  // Pagination/filter/sort/scroll all live in separate local state and are
  // untouched by a data update — the owner is never yanked off their page.
  useAutoRefresh(async () => {
    if (!traderId) return
    const data = await api.getPositionHistory(
      traderId,
      Math.max(200, pageSize * 5),
      true
    )
    setPositions(data.positions || [])
    setStats(data.stats)
    setSymbolStats(data.symbol_stats || [])
    setDirectionStats(data.direction_stats || [])
    setSessionDayStartMs(data.session_day_start_ms)
    setStatsScope(data.stats_scope)
  }, REFRESH_HISTORY_MS)

  // Get unique symbols for filter
  const uniqueSymbols = useMemo(() => {
    const symbols = new Set(positions.map((p) => p.symbol))
    return Array.from(symbols).sort()
  }, [positions])

  // LEDGER-SURFACE HONESTY — classify every row once. hidden rows (test-seam +
  // evidence-less reconcile_flat) never render; duplicates render only when the
  // toggle is on; totals only ever sum normal rows.
  const classified = useMemo(() => {
    const kinds = new Map<number, PositionKind>()
    const dupes = new Map<number, number>()
    for (const p of positions) {
      kinds.set(p.id, classifyPosition(p, positions))
      if (kinds.get(p.id) === 'duplicate') {
        dupes.set(p.id, duplicateOfId(p, positions))
      }
    }
    return { kinds, dupes }
  }, [positions])

  // Filtered and sorted positions (before pagination)
  const filteredAndSortedPositions = useMemo(() => {
    let result = [...positions]

    // Ledger-honesty exclusions: test-seam + evidence-less reconcile_flat rows
    // are never visible; duplicates only when the toggle is on.
    result = result.filter((p) => {
      const kind = classified.kinds.get(p.id) ?? 'normal'
      if (kind === 'hidden') return false
      if (kind === 'duplicate' && !showDuplicates) return false
      return true
    })

    // Apply filters
    if (filterSymbol !== 'all') {
      result = result.filter((p) => p.symbol === filterSymbol)
    }
    if (filterSide !== 'all') {
      result = result.filter(
        (p) => (p.side || '').toUpperCase() === filterSide.toUpperCase()
      )
    }

    // Apply sorting
    result.sort((a, b) => {
      let comparison = 0
      switch (sortBy) {
        case 'time':
          comparison =
            new Date(a.exit_time || 0).getTime() -
            new Date(b.exit_time || 0).getTime()
          break
        case 'pnl':
          comparison = (a.realized_pnl || 0) - (b.realized_pnl || 0)
          break
        case 'pnl_pct': {
          const aPrice = a.entry_price || 1
          const bPrice = b.entry_price || 1
          const aPct = (((a.exit_price || 0) - aPrice) / aPrice) * 100
          const bPct = (((b.exit_price || 0) - bPrice) / bPrice) * 100
          comparison = aPct - bPct
          break
        }
      }
      return sortOrder === 'desc' ? -comparison : comparison
    })

    return result
  }, [
    positions,
    classified,
    showDuplicates,
    filterSymbol,
    filterSide,
    sortBy,
    sortOrder,
  ])

  // The visible total sums ONLY normal rows with a RESOLVED pnl_corrected —
  // so the rows on screen and the number beneath them always agree
  // (duplicates/"—"/"unresolved" rows contribute nothing).
  const visibleTotal = useMemo(() => {
    let sum = 0
    for (const p of filteredAndSortedPositions) {
      if (classified.kinds.get(p.id) !== 'normal') continue
      if (typeof p.pnl_corrected !== 'number' || isNaN(p.pnl_corrected))
        continue
      sum += p.pnl_corrected
    }
    return Math.round(sum * 100) / 100
  }, [filteredAndSortedPositions, classified])

  // Canon class 40: the unresolved count is SHOWN alongside the totals.
  const unresolvedCount = useMemo(
    () =>
      positions.filter(
        (p) => typeof p.pnl_corrected !== 'number' || isNaN(p.pnl_corrected)
      ).length,
    [positions]
  )

  // Day total — the SAME rule the ledger's session-day query uses
  // (unknown/test-seam/duplicate excluded, corrections win, A-2 NULLs out).
  const dayTotal = useMemo(
    () => computeDayTotal(positions, sessionDayStartMs),
    [positions, sessionDayStartMs]
  )

  // R22 R1 — the manual NT8 rows in this list, as their own visible line.
  const manualRows = useMemo(
    () => positions.filter((p) => isManualOrigin(p)),
    [positions]
  )

  // Pagination calculations
  const totalFilteredCount = filteredAndSortedPositions.length
  const totalPages = Math.ceil(totalFilteredCount / pageSize)

  // Reset to page 1 when filters change
  useEffect(() => {
    setCurrentPage(1)
  }, [filterSymbol, filterSide, sortBy, sortOrder, pageSize])

  // Paginated positions (for display)
  const paginatedPositions = useMemo(() => {
    const startIndex = (currentPage - 1) * pageSize
    return filteredAndSortedPositions.slice(startIndex, startIndex + pageSize)
  }, [filteredAndSortedPositions, currentPage, pageSize])

  // For backwards compatibility, keep filteredPositions as the paginated result
  const filteredPositions = paginatedPositions

  // Calculate profit/loss ratio (avg win / avg loss)
  const profitLossRatio = useMemo(() => {
    if (!stats) return 0
    const avgWin = stats.avg_win || 0
    const avgLoss = stats.avg_loss || 0
    if (avgLoss === 0) return avgWin > 0 ? Infinity : 0
    return avgWin / avgLoss
  }, [stats])

  if (loading) {
    return (
      <div
        className="flex items-center justify-center p-12"
        style={{ color: '#848E9C' }}
      >
        <div className="animate-spin mr-3">
          <svg className="w-6 h-6" fill="none" viewBox="0 0 24 24">
            <circle
              className="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              strokeWidth="4"
            />
            <path
              className="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
            />
          </svg>
        </div>
        {t('positionHistory.loading', language)}
      </div>
    )
  }

  if (error) {
    return (
      <div
        className="rounded-lg p-6 text-center"
        style={{
          background: 'rgba(246, 70, 93, 0.1)',
          border: '1px solid rgba(246, 70, 93, 0.3)',
          color: '#F6465D',
        }}
      >
        {error}
      </div>
    )
  }

  if (positions.length === 0) {
    return (
      <div
        className="rounded-lg p-12 text-center"
        style={{
          background: 'linear-gradient(135deg, #1E2329 0%, #181C21 100%)',
          border: '1px solid #2B3139',
        }}
      >
        <div className="text-4xl mb-4">📊</div>
        <div
          className="text-lg font-semibold mb-2"
          style={{ color: '#EAECEF' }}
        >
          {t('positionHistory.noHistory', language)}
        </div>
        <div style={{ color: '#848E9C' }}>
          {t('positionHistory.noHistoryDesc', language)}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Overall Stats - Row 1: Core Metrics */}
      {stats && (
        <div className="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-5 gap-4">
          <StatCard
            icon="📊"
            title={t('positionHistory.totalTrades', language)}
            value={stats.total_trades || 0}
            subtitle={t('positionHistory.winLoss', language, {
              win: stats.win_trades || 0,
              loss: stats.loss_trades || 0,
            })}
            language={language}
          />
          <StatCard
            icon="🎯"
            title={t('positionHistory.winRate', language)}
            value={(stats.win_rate || 0).toFixed(1)}
            suffix="%"
            color={
              (stats.win_rate || 0) >= 60
                ? '#0ECB81'
                : (stats.win_rate || 0) >= 40
                  ? '#F0B90B'
                  : '#F6465D'
            }
            metricKey="win_rate"
            language={language}
          />
          <StatCard
            icon="💰"
            title={t('positionHistory.totalPnL', language)}
            value={
              ((stats.total_pnl || 0) >= 0 ? '+' : '') +
              formatNumber(stats.total_pnl || 0)
            }
            color={(stats.total_pnl || 0) >= 0 ? '#0ECB81' : '#F6465D'}
            subtitle={`Bot trades only · ${historyScopeLabel(statsScope)} · ${stats.manual_excluded_line ?? 'Manual (excluded): n/a'} · ${t('positionHistory.fee', language)}: -${formatNumber(stats.total_fee || 0)}`}
            metricKey="total_return"
            language={language}
          />
          <StatCard
            icon="📈"
            title={t('positionHistory.profitFactor', language)}
            value={(stats.profit_factor || 0).toFixed(2)}
            color={
              (stats.profit_factor || 0) >= 1.5
                ? '#0ECB81'
                : (stats.profit_factor || 0) >= 1
                  ? '#F0B90B'
                  : '#F6465D'
            }
            subtitle={t('positionHistory.profitFactorDesc', language)}
            metricKey="profit_factor"
            language={language}
          />
          <StatCard
            icon="⚖️"
            title={t('positionHistory.plRatio', language)}
            value={
              profitLossRatio === Infinity ? '∞' : profitLossRatio.toFixed(2)
            }
            color={
              profitLossRatio >= 1.5
                ? '#0ECB81'
                : profitLossRatio >= 1
                  ? '#F0B90B'
                  : '#F6465D'
            }
            subtitle={t('positionHistory.plRatioDesc', language)}
            metricKey="expectancy"
            language={language}
          />
        </div>
      )}

      {/* Overall Stats - Row 2: Advanced Metrics */}
      {stats && (
        <div className="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-5 gap-4">
          <StatCard
            icon="📉"
            title={t('positionHistory.sharpeRatio', language)}
            value={(stats.sharpe_ratio || 0).toFixed(2)}
            color={
              (stats.sharpe_ratio || 0) >= 1
                ? '#0ECB81'
                : (stats.sharpe_ratio || 0) >= 0
                  ? '#F0B90B'
                  : '#F6465D'
            }
            subtitle={t('positionHistory.sharpeRatioDesc', language)}
            metricKey="sharpe_ratio"
            language={language}
          />
          <StatCard
            icon="🔻"
            title={t('positionHistory.maxDrawdown', language)}
            value={(stats.max_drawdown_pct || 0).toFixed(1)}
            suffix="%"
            color={
              (stats.max_drawdown_pct || 0) <= 10
                ? '#0ECB81'
                : (stats.max_drawdown_pct || 0) <= 20
                  ? '#F0B90B'
                  : '#F6465D'
            }
            metricKey="max_drawdown"
            language={language}
          />
          <StatCard
            icon="🏆"
            title={t('positionHistory.avgWin', language)}
            value={'+' + formatNumber(stats.avg_win || 0)}
            color="#0ECB81"
            metricKey="avg_trade_pnl"
            language={language}
          />
          <StatCard
            icon="💸"
            title={t('positionHistory.avgLoss', language)}
            value={'-' + formatNumber(stats.avg_loss || 0)}
            color="#F6465D"
            language={language}
          />
          <StatCard
            icon="💵"
            title={t('positionHistory.netPnL', language)}
            value={
              ((stats.total_pnl || 0) - (stats.total_fee || 0) >= 0
                ? '+'
                : '') +
              formatNumber((stats.total_pnl || 0) - (stats.total_fee || 0))
            }
            color={
              (stats.total_pnl || 0) - (stats.total_fee || 0) >= 0
                ? '#0ECB81'
                : '#F6465D'
            }
            subtitle={t('positionHistory.netPnLDesc', language)}
            language={language}
          />
        </div>
      )}

      {/* Direction Stats */}
      {directionStats.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {directionStats.map((stat) => (
            <DirectionStatsCard
              key={stat.side}
              stat={stat}
              language={language}
            />
          ))}
        </div>
      )}

      {/* Symbol Performance */}
      {symbolStats.length > 0 && (
        <div
          className="rounded-lg p-4"
          style={{
            background: 'linear-gradient(135deg, #1E2329 0%, #181C21 100%)',
            border: '1px solid #2B3139',
          }}
        >
          <div className="flex items-center gap-2 mb-4">
            <span className="text-lg">🏅</span>
            <span className="font-semibold" style={{ color: '#EAECEF' }}>
              {t('positionHistory.symbolPerformance', language)}
            </span>
          </div>
          <div className="space-y-1">
            {symbolStats.slice(0, 10).map((stat) => (
              <SymbolStatsRow key={stat.symbol} stat={stat} />
            ))}
          </div>
        </div>
      )}

      {/* Position List */}
      <div
        className="rounded-lg overflow-hidden"
        style={{
          background: 'linear-gradient(135deg, #1E2329 0%, #181C21 100%)',
          border: '1px solid #2B3139',
        }}
      >
        {/* Filters */}
        <div
          className="flex flex-wrap items-center gap-4 p-4"
          style={{ borderBottom: '1px solid #2B3139' }}
        >
          <div className="flex items-center gap-2">
            <span className="text-sm" style={{ color: '#848E9C' }}>
              {t('positionHistory.symbol', language)}:
            </span>
            <VlSelect
              value={filterSymbol}
              onChange={(val) => setFilterSymbol(val)}
              options={[
                {
                  value: 'all',
                  label: t('positionHistory.allSymbols', language),
                },
                ...uniqueSymbols.map((s) => ({
                  value: s,
                  label: (s || '').replace('USD', ''),
                })),
              ]}
              className="rounded px-3 py-1.5 text-sm"
              style={{
                background: '#0B0E11',
                border: '1px solid #2B3139',
                color: '#EAECEF',
              }}
            />
          </div>

          <div className="flex items-center gap-2">
            <span className="text-sm" style={{ color: '#848E9C' }}>
              {t('positionHistory.side', language)}:
            </span>
            <div
              className="flex rounded overflow-hidden"
              style={{ border: '1px solid #2B3139' }}
            >
              {['all', 'LONG', 'SHORT'].map((side) => (
                <button
                  key={side}
                  onClick={() => setFilterSide(side)}
                  className="px-3 py-1.5 text-sm capitalize transition-colors"
                  style={{
                    background: filterSide === side ? '#2B3139' : 'transparent',
                    color: filterSide === side ? '#EAECEF' : '#848E9C',
                  }}
                >
                  {side === 'all' ? t('positionHistory.all', language) : side}
                </button>
              ))}
            </div>
          </div>

          <div className="flex items-center gap-2 ml-auto">
            <button
              onClick={() => setShowDuplicates((v) => !v)}
              className="px-3 py-1.5 text-sm rounded transition-colors"
              style={{
                background: showDuplicates ? '#2B3139' : 'transparent',
                border: '1px solid #2B3139',
                color: showDuplicates ? '#EAECEF' : '#848E9C',
              }}
              title="Duplicate rows shadow the same NT8 position as a real trade (their P&L belongs to the real row)"
            >
              duplicates: {showDuplicates ? 'shown' : 'hidden'}
            </button>
            <span className="text-sm" style={{ color: '#848E9C' }}>
              {t('positionHistory.sort', language)}:
            </span>
            <VlSelect
              value={`${sortBy}-${sortOrder}`}
              onChange={(val) => {
                const [by, order] = val.split('-') as [
                  'time' | 'pnl' | 'pnl_pct',
                  'asc' | 'desc',
                ]
                setSortBy(by)
                setSortOrder(order)
              }}
              options={[
                {
                  value: 'time-desc',
                  label: t('positionHistory.latestFirst', language),
                },
                {
                  value: 'time-asc',
                  label: t('positionHistory.oldestFirst', language),
                },
                {
                  value: 'pnl-desc',
                  label: t('positionHistory.highestPnL', language),
                },
                {
                  value: 'pnl-asc',
                  label: t('positionHistory.lowestPnL', language),
                },
              ]}
              className="rounded px-3 py-1.5 text-sm"
              style={{
                background: '#0B0E11',
                border: '1px solid #2B3139',
                color: '#EAECEF',
              }}
            />
          </div>
        </div>

        {/* Table */}
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr style={{ background: '#0B0E11' }}>
                <th
                  className="py-3 px-4 text-left text-xs font-semibold uppercase tracking-wider"
                  style={{ color: '#848E9C' }}
                >
                  {t('positionHistory.symbol', language)}
                </th>
                <th
                  className="py-3 px-4 text-right text-xs font-semibold uppercase tracking-wider"
                  style={{ color: '#848E9C' }}
                >
                  {t('positionHistory.entry', language)}
                </th>
                <th
                  className="py-3 px-4 text-right text-xs font-semibold uppercase tracking-wider"
                  style={{ color: '#848E9C' }}
                >
                  {t('positionHistory.exit', language)}
                </th>
                <th
                  className="py-3 px-4 text-right text-xs font-semibold uppercase tracking-wider"
                  style={{ color: '#848E9C' }}
                >
                  {t('positionHistory.qty', language)}
                </th>
                <th
                  className="py-3 px-4 text-right text-xs font-semibold uppercase tracking-wider"
                  style={{ color: '#848E9C' }}
                >
                  {t('positionHistory.value', language)}
                </th>
                <th
                  className="py-3 px-4 text-right text-xs font-semibold uppercase tracking-wider"
                  style={{ color: '#848E9C' }}
                >
                  {t('positionHistory.pnl', language)}
                </th>
                <th
                  className="py-3 px-4 text-right text-xs font-semibold uppercase tracking-wider"
                  style={{ color: '#848E9C' }}
                >
                  {t('positionHistory.fee', language)}
                </th>
                <th
                  className="py-3 px-4 text-center text-xs font-semibold uppercase tracking-wider"
                  style={{ color: '#848E9C' }}
                >
                  {t('positionHistory.duration', language)}
                </th>
                <th
                  className="py-3 px-4 text-right text-xs font-semibold uppercase tracking-wider"
                  style={{ color: '#848E9C' }}
                >
                  {t('positionHistory.closedAt', language)}
                </th>
              </tr>
            </thead>
            <tbody>
              {filteredPositions.map((position) => (
                <PositionRow
                  key={position.id}
                  position={position}
                  kind={classified.kinds.get(position.id) ?? 'normal'}
                  duplicateOf={classified.dupes.get(position.id) ?? 0}
                />
              ))}
            </tbody>
          </table>
        </div>

        {/* Footer with Pagination */}
        <div
          className="flex flex-wrap items-center justify-between gap-4 p-4 text-sm"
          style={{ borderTop: '1px solid #2B3139', color: '#848E9C' }}
        >
          {/* Left: Count info */}
          <div className="flex items-center gap-4">
            <span>
              {t('positionHistory.showingPositions', language, {
                count: totalFilteredCount,
                total: positions.length,
              })}
            </span>
            {totalFilteredCount > 0 && (
              <span>
                {t('positionHistory.totalPnL', language)}:{' '}
                <span
                  style={{
                    color: visibleTotal >= 0 ? '#0ECB81' : '#F6465D',
                  }}
                >
                  {visibleTotal >= 0 ? '+' : ''}
                  {formatNumber(visibleTotal)}
                </span>
              </span>
            )}
            <span title="Bot trades closed in the CME session day (from 17:00 CT — the risk gates' day). Same exclusion rule as the ledger: unknown / test-seam / duplicate / manual rows excluded, corrections win.">
              Session day (from 17:00 CT):{' '}
              {dayTotal === null ? (
                <span className="font-mono">n/a</span>
              ) : (
                <span
                  className="font-mono font-semibold"
                  style={{ color: dayTotal >= 0 ? '#0ECB81' : '#F6465D' }}
                >
                  {dayTotal >= 0 ? '+' : ''}
                  {formatNumber(dayTotal)}
                </span>
              )}
            </span>
            {manualRows.length > 0 && (
              <span
                data-testid="manual-excluded-footer"
                className="font-mono"
                style={{ color: '#F0B90B' }}
                title="Manual NT8 trades the bot adopted with no order of its own — listed, never in a bot total (owner ruling R1)"
              >
                {manualFooterLine(manualRows)}
              </span>
            )}
            {unresolvedCount > 0 && (
              <span
                className="font-mono"
                style={{ color: '#F0B90B' }}
                title="pnl_corrected is NULL — these rows read unresolved and are excluded from every total (canon class 40)"
              >
                {unresolvedCount} unresolved
              </span>
            )}
          </div>

          {/* Right: Pagination controls */}
          <div className="flex items-center gap-3">
            {/* Page size selector */}
            <div className="flex items-center gap-2">
              <span className="text-xs" style={{ color: '#848E9C' }}>
                {language === 'zh' ? '每页' : 'Per page'}:
              </span>
              <VlSelect
                value={pageSize}
                onChange={(val) => setPageSize(Number(val))}
                options={[
                  { value: 20, label: '20' },
                  { value: 50, label: '50' },
                  { value: 100, label: '100' },
                ]}
                className="rounded px-2 py-1 text-sm"
                style={{
                  background: '#0B0E11',
                  border: '1px solid #2B3139',
                  color: '#EAECEF',
                }}
              />
            </div>

            {/* Page navigation */}
            {totalPages > 1 && (
              <div className="flex items-center gap-1">
                <button
                  onClick={() => setCurrentPage(1)}
                  disabled={currentPage === 1}
                  className="px-2 py-1 rounded text-xs transition-colors disabled:opacity-30"
                  style={{
                    background: currentPage === 1 ? 'transparent' : '#2B3139',
                    color: '#EAECEF',
                  }}
                >
                  «
                </button>
                <button
                  onClick={() => setCurrentPage((p) => Math.max(1, p - 1))}
                  disabled={currentPage === 1}
                  className="px-2 py-1 rounded text-xs transition-colors disabled:opacity-30"
                  style={{
                    background: currentPage === 1 ? 'transparent' : '#2B3139',
                    color: '#EAECEF',
                  }}
                >
                  ‹
                </button>
                <span className="px-3 text-xs" style={{ color: '#EAECEF' }}>
                  {currentPage} / {totalPages}
                </span>
                <button
                  onClick={() =>
                    setCurrentPage((p) => Math.min(totalPages, p + 1))
                  }
                  disabled={currentPage === totalPages}
                  className="px-2 py-1 rounded text-xs transition-colors disabled:opacity-30"
                  style={{
                    background:
                      currentPage === totalPages ? 'transparent' : '#2B3139',
                    color: '#EAECEF',
                  }}
                >
                  ›
                </button>
                <button
                  onClick={() => setCurrentPage(totalPages)}
                  disabled={currentPage === totalPages}
                  className="px-2 py-1 rounded text-xs transition-colors disabled:opacity-30"
                  style={{
                    background:
                      currentPage === totalPages ? 'transparent' : '#2B3139',
                    color: '#EAECEF',
                  }}
                >
                  »
                </button>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
