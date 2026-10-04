// W-EXEC-TRUTH W1 (g) — GET /api/strategies/:id/effective (api/strategy_effective.go).
//
// Every settings row with its EFFECTIVE value, ORIGIN and SCOPE, resolved by the
// server from the STORED strategy row with the production resolvers. The UI
// renders what arrives; it never derives an effective value, an origin or a
// default of its own.

import { httpClient } from '../httpClient'
import { API_BASE } from './helpers'

/** What the stored row carries at the path. `value` is ABSENT when nothing is
 *  stored — absent and a stored 0 / [] are different answers. */
export interface EffectiveStored {
  present: boolean
  value?: unknown
}

/** One settings row (mirrors trader.EffectiveKnob). `effective` is the value
 *  the runtime uses — or a string starting "n/a" when it is not known, or
 *  "redacted" for a secret. */
export interface EffectiveKnob {
  path: string
  status: string
  ui_label: string
  stored: EffectiveStored
  effective: unknown
  origin: string
  scope: string
  resolver: string
  resolved: boolean
}

export interface EffectiveCoverage {
  resolved: number
  total: number
  unresolved: string[]
  not_enumerated: string[]
}

export interface StrategyEffectiveResponse {
  strategy_id: string
  session: string | null
  venue: string | null
  settings: EffectiveKnob[]
  coverage: EffectiveCoverage
  /** MENTOR_PLACE gate of the running process (read-only). Absent on an older
   *  server — the UI then says the gate status is unavailable. */
  mentor_place?: boolean
}

export type EffectiveByPath = Record<string, EffectiveKnob>

export const strategyEffectiveApi = {
  /** Silent GET; any failure resolves to null so a row renders no chip rather
   *  than a fabricated value. */
  async getStrategyEffective(
    strategyId: string,
    session?: string,
    venue?: string
  ): Promise<StrategyEffectiveResponse | null> {
    const params = new URLSearchParams()
    if (session) params.set('session', session)
    if (venue) params.set('venue', venue)
    const qs = params.toString()
    try {
      const res = await httpClient.request<StrategyEffectiveResponse>(
        `${API_BASE}/strategies/${encodeURIComponent(strategyId)}/effective${qs ? `?${qs}` : ''}`,
        { silent: true }
      )
      return res.success && res.data ? res.data : null
    } catch {
      return null
    }
  },
}

/** path → row, for the editors to look a row up by its schema path. */
export function effectiveByPath(
  resp: StrategyEffectiveResponse | null | undefined
): EffectiveByPath {
  const out: EffectiveByPath = {}
  for (const k of resp?.settings ?? []) out[k.path] = k
  return out
}

/** What the Studio editors read. `byPath` holds the strategy-level rows (the
 *  fetch WITHOUT ?session=, so a strategy row is never answered with one
 *  session's override); `bySession[S]` holds the rows fetched with ?session=S,
 *  which is where the per-session `day_plan.sessions.<leaf>` rows are answered.
 *  A session missing from `bySession` was not fetched or failed — its rows
 *  render no chip, never a borrowed value. */
export interface StudioEffective {
  byPath: EffectiveByPath
  bySession: Partial<Record<string, EffectiveByPath>>
  /** MENTOR_PLACE gate from the strategy-level reply; absent = unknown. */
  mentorPlace?: boolean
}

/** Builds the editors' lookup from the four responses. A response answers a
 *  session only when the server says it resolved THAT session (its `session`
 *  field); the strategy-level map only takes a session-less reply. A reply for
 *  the wrong scope fills nothing — no chip beats a chip from another scope. */
export function studioEffective(
  base: StrategyEffectiveResponse | null | undefined,
  sessions: Record<string, StrategyEffectiveResponse | null | undefined>
): StudioEffective {
  const bySession: Partial<Record<string, EffectiveByPath>> = {}
  for (const [name, resp] of Object.entries(sessions)) {
    if (resp && resp.session === name) bySession[name] = effectiveByPath(resp)
  }
  const out: StudioEffective = {
    byPath: base && base.session == null ? effectiveByPath(base) : {},
    bySession,
  }
  if (base && base.session == null && typeof base.mentor_place === 'boolean') {
    out.mentorPlace = base.mentor_place
  }
  return out
}
