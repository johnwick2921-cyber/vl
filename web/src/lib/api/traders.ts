import type {
  TraderInfo,
  TraderConfigData,
  CreateTraderRequest,
  AccountsResponse,
} from '../../types'
import { API_BASE, httpClient } from './helpers'
import { ApiError } from '../httpClient'

/** Pep "what trades" card — GET /api/traders/:id/pep-truth.
 *
 *  PROVISIONAL SHAPE (2026-10-30): the trader lane (fix/m458-pep-trader) had not
 *  published the handler when the UI was cut, so this type is defined from the
 *  core schema it must be built from — kernel/pep types.go (Result, Box, State,
 *  Intent, Refusal, Diag) and store.ResolvePep. Every field except `enabled` is
 *  optional and the card renders only what arrived: an absent field is shown as
 *  absent, never as 0 (a missing box is "not drawn yet", not a box at 0).
 *  `enabled:false` (or a 404 from an older server) hides the card. The trader
 *  lane must match these names or change this type in the same merge. */
export interface PepTruth {
  enabled: boolean
  as_of_ms: number
  /** The instrument pep reads (e.g. "MNQ"); the chart draws the box only on a
   *  tab showing it. */
  symbol?: string
  /** The style that runs. */
  style?: 'A' | 'E'
  /** CT date the day-counters belong to ("2006-01-02"). */
  day?: string
  contracts?: number
  /** The opening range (kernel/pep Box); ABSENT until computed. */
  box?: {
    high: number
    low: number
    size: number
    start_ms: number
    end_ms: number
    /** every minute present and the box window has closed */
    drawn: boolean
  }
  /** Where the day's signal stands. */
  signal?: {
    state: 'waiting' | 'entered' | 'blocked'
    side?: 'long' | 'short'
  }
  /** Named refusals of the latest evaluation (kernel/pep Refusal). */
  refusals?: {
    code: string
    severity: 'wait' | 'block'
    detail?: string
  }[]
  trades_today?: number
  max_trades_per_day?: number
  position_open?: boolean
  /** The intent's / open position's prices (points). */
  entry?: number
  stop?: number
  target?: number
  /** The time exit, "HH:MM" CT, and the seconds left until it (<= 0 = passed). */
  flat_time_ct?: string
  flat_in_sec?: number
}

/** Mentor "what trades" panel — the live evaluator state (release #10). */
export interface MentorTruth {
  enabled: boolean
  as_of_ms: number
  /** UG-2 (release #24): the instrument the evaluator reads (e.g. "MNQ"). The
   * chart draws the mentor overlays only on a tab showing this symbol. Absent
   * on a pre-#24 server. */
  symbol?: string
  htf: {
    four_h_dir: string
    four_h_since: number
    one_h_dir: string
    one_h_since: number
    verdict: string
    verdict_why?: string
    verdict_side?: string
    gate_active: boolean
  }
  trigger_5m: { dir: string; price: number; since: number }
  /** ABSENT (undefined) while the evaluator has not built yet — see `computing`. */
  levels?: {
    key: string
    kind: string
    price: number
    drawn_at: number
    visits_today: number
    merged_from?: number[]
    /** m421b, additive: box edges (ftgh_edge/ftgl_edge) may carry the box's
     * formation / end time (ms) and frame; absent today = the chart falls
     * back to drawn_at, then the first drawn bar. */
    formed_ms?: number
    ended_ms?: number
    tf?: string
  }[]
  /** m422: ms of the last close the boot display seed drew from; set only
   *  until the first real tick (overlays are "as of last close"). */
  chart_seeded_at?: number
  /** ABSENT (undefined) while the evaluator has not built yet. */
  depth?: Record<string, number>
  /** true when mentor is ON but the evaluator has not built yet (first 1m bar). */
  computing?: boolean
  depth_line: string
  window: { start: string; minutes: number; active: boolean; ended: boolean }
  done_after_win: boolean
  done_after_win_why?: string
  stop_after_loss: boolean
  stop_after_loss_why?: string
  /** T7 (release #22) — ABSENT while computing; `[]` once built. FORMING + VALID trendlines (m134). */
  trendlines?: {
    side: string // 'long' = support, 'short' = resistance
    p0_ms: number
    p0_px: number
    p1_ms: number
    p1_px: number
    now_ms: number
    now_px: number
    in_level: boolean // false = inside a box this bar (the box wins)
    valid: boolean // m134: true = 3rd touch seen (a location); false = forming (waiting)
  }[]
  /** m134 — the per-bar EMA 9/34 curve, ABSENT while computing; `[]` once built. */
  ema_series?: {
    ms: number
    ema9: number
    ema34: number
    /** m402: the frame the engine used (1 or 2 min); ABSENT on an older backend = 1. */
    tf_min?: 1 | 2
  }[]
  /** T7 — the ISB rest boxes the evaluator holds (5m / 15m / 30m). */
  isb_boxes?: MentorRange[]
  /** T7 — the 08:30–08:32 opening range, absent until drawn. */
  orb?: MentorRange
  /** T7 — the swing line (4h EMA 34), absent until warm, with its order/leg. */
  swing?: { line: number; pending?: MentorSwingLeg; pos?: MentorSwingLeg }
}

export interface MentorRange {
  label: string
  high: number
  low: number
  dir?: string
  at_ms?: number
  status?: string
}

export interface MentorSwingLeg {
  side: string
  entry: number
  stop: number
  target: number
  /** UG-1 (release #24), filled leg only: "broker" = `stop` is the leg's
   * working stop read from NinjaTrader's order book; "entry" = the book could
   * not be read, so `stop` is the stop the leg was entered with. */
  stop_source?: 'broker' | 'entry'
  /** UG-1: the evaluator emitted its +1R break-even move (a request). */
  moved_be?: boolean
}

function throwApiError(
  message: string,
  errorKey?: string,
  errorParams?: Record<string, string>,
  statusCode?: number
): never {
  throw new ApiError(message, errorKey, errorParams, statusCode)
}

export const traderApi = {
  async getTraders(silent?: boolean): Promise<TraderInfo[]> {
    const result = await httpClient.request<TraderInfo[]>(
      `${API_BASE}/my-traders`,
      { silent }
    )
    if (!result.success) throw new Error('Failed to fetch trader list')
    return Array.isArray(result.data) ? result.data : []
  },

  async getPublicTraders(): Promise<any[]> {
    const result = await httpClient.get<any[]>(`${API_BASE}/traders`)
    if (!result.success) throw new Error('Failed to fetch public trader list')
    return result.data!
  },

  async createTrader(request: CreateTraderRequest): Promise<TraderInfo> {
    const result = await httpClient.post<TraderInfo>(
      `${API_BASE}/traders`,
      request
    )
    if (!result.success) {
      throwApiError(
        result.message || 'Failed to create trader',
        result.errorKey,
        result.errorParams,
        result.statusCode
      )
    }
    return result.data!
  },

  async deleteTrader(traderId: string): Promise<void> {
    const result = await httpClient.delete(`${API_BASE}/traders/${traderId}`)
    if (!result.success) throw new Error('Failed to delete trader')
  },

  async startTrader(traderId: string): Promise<void> {
    const result = await httpClient.post(
      `${API_BASE}/traders/${traderId}/start`
    )
    if (!result.success) {
      throwApiError(
        result.message || 'Failed to start trader',
        result.errorKey,
        result.errorParams,
        result.statusCode
      )
    }
  },

  async stopTrader(traderId: string): Promise<void> {
    const result = await httpClient.post(`${API_BASE}/traders/${traderId}/stop`)
    if (!result.success) throw new Error('Failed to stop trader')
  },

  // P2 (ledger-close 2026-08-19) — stop_until pause: NEW entries only.
  async pauseTrader(
    traderId: string,
    body: { minutes?: number; until_ct?: string; until?: string }
  ): Promise<void> {
    const result = await httpClient.post(
      `${API_BASE}/traders/${traderId}/pause`,
      body
    )
    if (!result.success) throw new Error('Failed to pause trader')
  },

  async resumeTrader(traderId: string): Promise<void> {
    const result = await httpClient.post(
      `${API_BASE}/traders/${traderId}/resume`
    )
    if (!result.success) throw new Error('Failed to resume trader')
  },

  async toggleCompetition(
    traderId: string,
    showInCompetition: boolean
  ): Promise<void> {
    const result = await httpClient.put(
      `${API_BASE}/traders/${traderId}/competition`,
      { show_in_competition: showInCompetition }
    )
    if (!result.success)
      throw new Error('Failed to update competition visibility')
  },

  async closePosition(
    traderId: string,
    symbol: string,
    side: string
  ): Promise<{ message: string }> {
    const result = await httpClient.post<{ message: string }>(
      `${API_BASE}/traders/${traderId}/close-position`,
      { symbol, side }
    )
    if (!result.success) throw new Error('Failed to close position')
    return result.data!
  },

  async updateTraderPrompt(
    traderId: string,
    customPrompt: string
  ): Promise<void> {
    const result = await httpClient.put(
      `${API_BASE}/traders/${traderId}/prompt`,
      { custom_prompt: customPrompt }
    )
    if (!result.success) throw new Error('Failed to update custom prompt')
  },

  async getTraderConfig(traderId: string): Promise<TraderConfigData> {
    const result = await httpClient.get<TraderConfigData>(
      `${API_BASE}/traders/${traderId}/config`
    )
    if (!result.success) throw new Error('Failed to fetch trader config')
    return result.data!
  },

  async getMentorTruth(traderId: string, silent = true): Promise<MentorTruth> {
    const result = await httpClient.request<MentorTruth>(
      `${API_BASE}/traders/${traderId}/mentor-truth`,
      { method: 'GET', silent }
    )
    if (!result.success || !result.data) {
      throw new Error('Failed to fetch mentor truth panel')
    }
    return result.data
  },

  async getPepTruth(traderId: string, silent = true): Promise<PepTruth> {
    const result = await httpClient.request<PepTruth>(
      `${API_BASE}/traders/${traderId}/pep-truth`,
      { method: 'GET', silent }
    )
    if (!result.success || !result.data) {
      throw new Error('Failed to fetch pep truth card')
    }
    return result.data
  },

  async updateTrader(
    traderId: string,
    request: CreateTraderRequest
  ): Promise<TraderInfo> {
    const result = await httpClient.put<TraderInfo>(
      `${API_BASE}/traders/${traderId}`,
      request
    )
    if (!result.success) {
      throwApiError(
        result.message || 'Failed to update trader',
        result.errorKey,
        result.errorParams,
        result.statusCode
      )
    }
    return result.data!
  },

  async getAccounts(traderId: string): Promise<AccountsResponse> {
    const result = await httpClient.get<AccountsResponse>(
      `${API_BASE}/accounts?trader_id=${traderId}`
    )
    if (!result.success) throw new Error('Failed to fetch accounts')
    return result.data!
  },

  async selectAccount(
    traderId: string,
    accountName: string
  ): Promise<{ message: string }> {
    const result = await httpClient.post<{ message: string }>(
      `${API_BASE}/account/select?trader_id=${traderId}`,
      { account: accountName }
    )
    if (!result.success) {
      throwApiError(
        result.message || 'Failed to select account',
        result.errorKey,
        result.errorParams,
        result.statusCode
      )
    }
    return result.data!
  },
}
