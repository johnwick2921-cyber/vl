// W-ONE-BUTTON M5 (U1) — typed readers for the update surfaces. Every shape
// below is READ from the Go gin.H keys on dev / the M3 branch (2a58cc84) —
// cited line-by-line in the PR. A 403 on install renders "not authorized on
// this device" with the server's own text and is NEVER retried in a loop.

import { API_BASE, httpClient } from './helpers'

// The M3 gate (api/handler_updates.go updatesRefusal) refuses every
// /api/updates* request without exactly one X-NOFX-Update: 1 — before it
// reads the JWT. It is the CSRF factor: the header is not in the CORS allow
// list, so a cross-origin page cannot make a browser send it. Sent on the
// /updates* calls ONLY (updates.header.test.ts; the Go side pins the name
// against api.UpdateHeader).
const UPDATE_HEADERS = { 'X-NOFX-Update': '1' }

// ── GET /api/maintenance (trader/maintenance_status.go MaintenanceStatusView) ──
export interface MaintenanceAckView {
  received: string
  age_ms: number
  held: boolean
  job_id: string
  queued_commands: number
  build_id: string
  accept_seq: number
}

export interface MaintenanceWithdrawEnded {
  id: number
  state: string
}

export interface MaintenanceWithdrawView {
  requested: boolean
  pending: number[]
  confirmed: number[]
  filled: number[]
  ended: MaintenanceWithdrawEnded[]
  unread?: string
}

export interface MaintenanceStatusView {
  held: boolean
  state: 'clear' | 'held' | 'unreadable' | 'unconfigured'
  job_id?: string | null
  since?: string | null
  reason?: string
  in_flight_sends: number
  drained: boolean
  addon_ack?: MaintenanceAckView | null
  // withdraw is ABSENT (null) when no hold asks for one — an unread ledger
  // carries the lists absent and unread says why (L7).
  withdraw?: MaintenanceWithdrawView | null
}

// ── GET /api/health (api/server.go:669 handleHealth) — panel A revision ──
export interface HealthStatus {
  status?: string
  time?: string
  revision?: string
}

// INSTALL_AUTHZ_UNDER_REVIEW ships ON until the CTO announces M3's adversarial
// review closed; while ON the Install button is disabled with the exact text
// below and no install POST can fire (pinned by its own test).
export const INSTALL_AUTHZ_UNDER_REVIEW = true
export const INSTALL_UNDER_REVIEW_TEXT = 'install authorization under review'

// ── GET /api/installation-gate (trader/installation_gate.go InstallationGate) ──
export interface InstallationGateLeg {
  name: string
  pass: boolean
  detail: string
  source: string
}

export interface InstallationGate {
  ready: boolean
  job_id: string // "n/a" when no well-formed hold names one
  legs: InstallationGateLeg[]
  traders: string[]
  note: string
}

// ── GET /api/updates (api/handler_updates.go:213 handleUpdatesStatus) ──
// The M3 payload carries exactly {enrolled, manifest_verifier, install_enabled}.
// update_available / install_state are OPTIONAL and ABSENT today — they are the
// fields a later server rev would add for the badge's 'Update available' /
// 'Installing' states. The badge shows Unknown whenever the API does not
// affirm a state; it never invents one.
export interface UpdatesStatus {
  enrolled: boolean
  manifest_verifier: string
  install_enabled: boolean
  update_available?: boolean
  install_state?: string
}

// ── POST /api/updates/check (api/handler_updates.go:228) ──
export interface UpdatesCheck {
  checked: boolean
  reason: string
}

// ── POST /api/updates/install (api/handler_updates.go:236) — 202 {job_id};
// 400/403/409/422/503 carry {error} with the server's exact text.
export interface UpdatesInstallResult {
  ok: boolean
  job_id?: string
  error?: string
  status?: number
}

// ── GET /api/updates/jobs/:id and /jobs/:id/receipt (handler_updates.go:301) ──
// M3: every id is 404 {error:"not found"} — there is no worker yet. The M4
// worker's state machine (downloaded → … → complete | rolled_back |
// recovery_needed) lands later; every field here is OPTIONAL so the page
// renders only what the API returns, nothing it does not.
export interface UpdateJobView {
  job_id?: string
  state?: string
  step?: string
  blocker?: string
  timestamps?: Record<string, string>
  receipt_url?: string
  error?: string
  status?: number
}

export const updatesApi = {
  async health(silent = true): Promise<HealthStatus | null> {
    const res = await httpClient.request<HealthStatus>(`${API_BASE}/health`, {
      silent,
    })
    return res.success && res.data ? res.data : null
  },

  async maintenance(silent = true): Promise<MaintenanceStatusView | null> {
    const res = await httpClient.request<MaintenanceStatusView>(
      `${API_BASE}/maintenance`,
      { silent }
    )
    return res.success && res.data ? res.data : null
  },

  async installationGate(silent = true): Promise<InstallationGate | null> {
    const res = await httpClient.request<InstallationGate>(
      `${API_BASE}/installation-gate`,
      { silent }
    )
    return res.success && res.data ? res.data : null
  },

  async updatesStatus(silent = true): Promise<UpdatesStatus | null> {
    const res = await httpClient.request<UpdatesStatus>(`${API_BASE}/updates`, {
      headers: UPDATE_HEADERS,
      silent,
    })
    return res.success && res.data ? res.data : null
  },

  async check(): Promise<UpdatesCheck | null> {
    const res = await httpClient.request<UpdatesCheck>(
      `${API_BASE}/updates/check`,
      {
        method: 'POST',
        headers: UPDATE_HEADERS,
        silent: true,
      }
    )
    return res.success && res.data ? res.data : null
  },

  // The body is EXACTLY the line `updater-bootstrap authorize` prints
  // (json.Marshal of updateauth.Grant). expires_at is unix seconds as a JSON
  // NUMBER: the server parses the raw bytes (internal/updateauth/strict.go
  // rawUnixSeconds) and answers a quoted one 400 — the MAC is over its
  // decimal text, so no other encoding may alias it (PR #200 fold F1).
  async install(body: {
    release_id: string
    job_id: string
    expires_at: number
    hmac: string
  }): Promise<UpdatesInstallResult> {
    const res = await httpClient.request<{ job_id: string; error?: string }>(
      `${API_BASE}/updates/install`,
      { method: 'POST', data: body, headers: UPDATE_HEADERS, silent: true }
    )
    if (res.success && res.data?.job_id) {
      return { ok: true, job_id: res.data.job_id }
    }
    return {
      ok: false,
      error: res.data?.error || res.message,
      status: res.statusCode,
    }
  },

  async job(id: string, silent = true): Promise<UpdateJobView | null> {
    const res = await httpClient.request<UpdateJobView>(
      `${API_BASE}/updates/jobs/${encodeURIComponent(id)}`,
      { headers: UPDATE_HEADERS, silent }
    )
    if (!res.data) return null
    return { ...res.data, status: res.statusCode }
  },
}
