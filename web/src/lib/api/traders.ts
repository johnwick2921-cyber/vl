import type {
  TraderInfo,
  TraderConfigData,
  CreateTraderRequest,
  AccountsResponse,
} from '../../types'
import { API_BASE, httpClient } from './helpers'
import { ApiError } from '../httpClient'

/** Mentor "what trades" panel — the live evaluator state (release #10). */
export interface MentorTruth {
  enabled: boolean
  as_of_ms: number
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
  }[]
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
