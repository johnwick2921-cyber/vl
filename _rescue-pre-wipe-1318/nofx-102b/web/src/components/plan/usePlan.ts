// P4 — Day-Plan SWR hooks. The PlanCard self-fetches (matches DecisionAudit /
// GridRiskPanel): pass a trader_id, get back the plan + alerts with polling. Keys
// are null when there's no trader so SWR skips the fetch entirely.

import useSWR from 'swr'
import { api } from '../../lib/api'
import type {
  PlanToday,
  PlanAlertsResponse,
  PlanVersionsResponse,
} from '../../lib/api/plan'

const PLAN_REFRESH_MS = 15_000
const ALERTS_REFRESH_MS = 20_000
const VERSIONS_REFRESH_MS = 30_000

export function usePlanToday(
  traderId?: string,
  symbol?: string,
  session?: string,
  version?: number
) {
  // session AND version are part of the KEY: switching tabs or opening an older
  // version must refetch, not reuse the previously cached plan.
  const key = traderId
    ? `plan-today-${traderId}${symbol ? `-${symbol}` : ''}${session ? `-${session}` : ''}${version ? `-v${version}` : ''}`
    : null
  const { data, error, isLoading, mutate } = useSWR<PlanToday | null>(
    key,
    () => api.getPlanToday(traderId as string, symbol, true, session, version),
    {
      // A historical version is IMMUTABLE — polling it would re-fetch a plan
      // that cannot change, and the "live" refresh cadence would be a lie.
      refreshInterval: version ? 0 : PLAN_REFRESH_MS,
      revalidateOnFocus: false,
      dedupingInterval: 5000,
    }
  )
  return { plan: data ?? null, error: error as unknown, isLoading, mutate }
}

// ITEM 15 — every stored version of the session's plan, for the version chips.
// Cheap and slow-moving (a new row only on a re-plan), so it polls lazily.
export function usePlanVersions(
  traderId?: string,
  session?: string,
  tradeDate?: string
) {
  const key =
    traderId && session
      ? `plan-versions-${traderId}-${session}${tradeDate ? `-${tradeDate}` : ''}`
      : null
  const { data, mutate } = useSWR<PlanVersionsResponse | null>(
    key,
    () => api.getPlanVersions(traderId as string, session as string, tradeDate),
    { refreshInterval: VERSIONS_REFRESH_MS, revalidateOnFocus: false }
  )
  return {
    versions: data?.versions ?? [],
    latestVersion: data?.latest_version ?? 0,
    mutate,
  }
}

export function usePlanAlerts(traderId?: string) {
  const key = traderId ? `plan-alerts-${traderId}` : null
  const { data, mutate } = useSWR<PlanAlertsResponse>(
    key,
    () => api.getPlanAlerts(traderId as string),
    { refreshInterval: ALERTS_REFRESH_MS, revalidateOnFocus: false }
  )
  return { alerts: data?.alerts ?? [], unacked: data?.unacked ?? 0, mutate }
}
