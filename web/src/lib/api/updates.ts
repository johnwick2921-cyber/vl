// W-ONE-BUTTON M5 (U1) — typed readers for the update surfaces. Every shape
// below is READ from the Go gin.H keys on dev / the M3 branch (2a58cc84) —
// cited line-by-line in the PR. A 403 on install renders "not authorized on
// this device" with the server's own text and is NEVER retried in a loop.

import { API_BASE, httpClient } from './helpers'

// The M3 gate (api/handler_updates.go updatesRefusal) refuses every
// /api/updates* request without exactly one X-VL-Update: 1 — before it
// reads the JWT. It is the CSRF factor: the header is not in the CORS allow
// list, so a cross-origin page cannot make a browser send it. Sent on the
// /updates* calls ONLY (updates.header.test.ts; the Go side pins the name
// against api.UpdateHeader).
const UPDATE_HEADERS = { 'X-VL-Update': '1' }

/** The updatesStatus answer plus the HTTP code, so callers can tell a 403
 *  not-enrolled refusal (back off, stop polling) from any other failure. */
export interface UpdatesStatusResult {
  status: UpdatesStatus | null
  statusCode?: number
}

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
  // release #22: an update job in a pre-hold state (requested → preflight)
  // freezes NEW entries until it ends or passes the 30-minute job rule.
  install_pending?: MaintenanceInstallPendingView
}

export interface MaintenanceInstallPendingView {
  pending: boolean
  job_id?: string
  state?: string
  phase?: string
  created_at?: string
  clears_by?: string
  unread?: number
}

// ── GET /api/health (api/handler_health.go handleHealth) — panel A revision ──
export interface HealthStatus {
  status?: string
  time?: string
  revision?: string
  // m386: the Friends page flag (VL_FRIENDS_PAGE). Absent (older bot) or false
  // → the Friends nav + route are hidden and /api/owner/friends 404s.
  friends_page?: boolean
  // m420b → m420a/B6: market/feed truth for the top status banner. Absent (an
  // older bot) → the banner renders nothing; it never infers either state.
  cme_open?: boolean
  feed_stale?: boolean
  // optional extras: seconds the feed has been down (market-open time only),
  // and the server's own next-reopen instant (RFC3339).
  feed_stale_secs?: number
  cme_reopens_at?: string
  // m444: the 1m bar holes the bot tracks (api/handler_health.go bars_holes;
  // BarHoleView). Absent (older bot / no store) → no MISSING BARS line.
  bars_holes?: BarHole[]
  // m445: the data-integrity pause (bar_integrity.go). Absent = an older bot or
  // no NT8 bridge; the banner then renders nothing.
  trading_paused?: {
    paused: boolean
    reasons?: string[]
    windows?: string[]
    // the WIDEST paused scope (all_entries | mentor_entries | ai_entries |
    // trader_entries | swing_and_deep_entries | swing_entries); absent on an older bot
    scope?: string
    // every blocked leg (a narrower one is never hidden behind a wider one)
    scopes?: string[]
    // release #55: the data-truth checks (bad print, frame mismatch, indicator
    // lookback, ingest loss, contract roll, broker agreement, fill price)
    // currently blocking; reasons then carries 'data_truth'
    data_truth_checks?: string[]
    // release #55 round 3: who a scoped check's block is FOR (acct:<ACCOUNT> /
    // trader:<id>) and which entry paths it concerns (ai | mentor). An absent
    // entry = the whole symbol / every path.
    data_truth_scopes?: Record<string, string[]>
    data_truth_readers?: Record<string, string[]>
  }
}

// One tracked 1m bar hole. state: refilling (the bot is re-asking NT8) |
// unfilled (NT8 has no history) | retired (older than NT8's replay window —
// the bot cannot re-ask). window is the CT text ("10-26 17:00–21:57 CT").
export interface BarHole {
  symbol: string
  state: 'refilling' | 'unfilled' | 'retired' | string
  window?: string
  missing_min?: number
  source?: 'live' | 'audit' | string
}

// INSTALL_AUTHZ_UNDER_REVIEW is the ONE constant gating the install control.
// UPDATER-USABLE-V1 final commit (separate on purpose): flipped to false so
// the install control's enabled state comes from the server alone
// (install_enabled AND worker_listening) — the CTO can take or drop this
// commit after his own adversarial pass. While true, the Install button is
// disabled with the exact text below and no install POST can fire (pinned by
// UpdatesPage.authzReview.test.tsx).
export const INSTALL_AUTHZ_UNDER_REVIEW = false
export const INSTALL_UNDER_REVIEW_TEXT = 'install authorization under review'

// ── GET /api/installation-gate (trader/installation_gate.go InstallationGate) ──
export interface InstallationGateLeg {
  name: string
  pass: boolean
  detail: string
  source: string
}

// UPDATER-NT8-CLOSED: the gate's nt8_absent verdict — present when an NT8 TCP
// trader exists to measure the link; eligible when the link has been down
// ≥60s continuously (measured from the server's per-connection record, never
// inferred from a stale ack); ready only when eligible AND every ledger leg
// passes on its own evidence. Legs stay ABSENT (not []) when not eligible.
export interface NT8AbsentView {
  eligible: boolean
  ready: boolean
  link_down_since?: string
  legs?: InstallationGateLeg[]
}

export interface InstallationGate {
  ready: boolean
  job_id: string // "n/a" when no well-formed hold names one
  legs: InstallationGateLeg[]
  traders: string[]
  note: string
  nt8_absent?: NT8AbsentView | null
}

// ── GET /api/updates (api/handler_updates.go:495 handleUpdatesStatus) ──
// The M3 payload carried exactly {enrolled, manifest_verifier, install_enabled};
// #206's ruling adds worker_listening — MEASURED by the route at request time
// (a 250 ms bounded dial of the worker socket), never inferred. The UI gates
// Install on BOTH flags being true and shows the exact reason otherwise.
// update_available / install_state are OPTIONAL and ABSENT today — they are the
// fields a later server rev would add for the badge's 'Update available' /
// 'Installing' states. The badge shows Unknown whenever the API does not
// affirm a state; it never invents one.
export interface UpdatesStatus {
  enrolled: boolean
  manifest_verifier: string
  install_enabled: boolean
  worker_listening?: boolean
  update_available?: boolean
  install_state?: string
}

// ── POST /api/updates/check (api/handler_updates.go:228) ──
export interface UpdatesCheck {
  // P0 field-names hotfix (owner 10-02 12:2x CT): the SERVER's real wire
  // shape. api/handler_updates.go answers verified_ready as {checked,
  // available, ready, tag, target_commitish, source_sha, reason} and
  // up_to_date as the same minus source_sha. The fixture consumed by the
  // vitest (./fixtures/updates-check.json) is generated by the Go parity
  // test from the SAME builders the handler uses.
  checked: boolean
  reason: string
  available?: boolean
  ready?: boolean
  tag?: string
  target_commitish?: string
  source_sha?: string
}

// ── POST /api/updates/install (api/handler_updates.go:236) — 202 {job_id};
// 400/403/409/422/503 carry {error} with the server's exact text.
export interface UpdatesInstallResult {
  ok: boolean
  job_id?: string
  error?: string
  status?: number
}

// ── The authorization line (UPDATER-USABLE-V1 A) ───────────────────────────
// The page's paste box takes the ONE line `vl-updater-bootstrap authorize
// <release_id>` prints: json.Marshal of updateauth.Grant, exactly
// {release_id, job_id, expires_at, hmac}. The parser checks the SHAPE only —
// the MAC is the server's to verify — but it must preserve the wire truth:
// expires_at travels as a JSON NUMBER (unix seconds). A quoted one is refused
// HERE with text that says so, because the server would answer 400 anyway and
// the MAC is over the decimal text — no other encoding may alias it
// (internal/updateauth/strict.go rawUnixSeconds, PR #200 fold F1).
export interface InstallAuthorization {
  release_id: string
  job_id: string
  expires_at: number
  hmac: string
}

export type InstallAuthorizationParse =
  | { ok: true; body: InstallAuthorization }
  | { ok: false; error: string }

export function parseInstallAuthorization(
  text: string
): InstallAuthorizationParse {
  // G1: a terminal-wrapped paste carries hard newlines where the terminal
  // folded the ONE printed line. json.Marshal output is compact and escapes
  // control characters, so a raw newline can never sit inside the hmac —
  // stripping them is safe. Un-wrapped pastes are byte-identical.
  const trimmed = text.replace(/\r/g, '').replace(/\n/g, '').trim()
  if (!trimmed) {
    return { ok: false, error: 'paste the authorization line first' }
  }
  let raw: unknown
  try {
    raw = JSON.parse(trimmed)
  } catch {
    return {
      ok: false,
      error:
        'not JSON — paste the one line vl-updater-bootstrap authorize prints',
    }
  }
  if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) {
    return { ok: false, error: 'not the authorization object' }
  }
  const r = raw as Record<string, unknown>
  for (const k of ['release_id', 'job_id', 'expires_at', 'hmac'] as const) {
    if (!(k in r)) {
      return { ok: false, error: `missing ${k}` }
    }
  }
  if (
    typeof r.release_id !== 'string' ||
    typeof r.job_id !== 'string' ||
    typeof r.hmac !== 'string'
  ) {
    return {
      ok: false,
      error: 'release_id, job_id and hmac must be strings',
    }
  }
  // expires_at MUST be a number here: Number() coercion would alias the
  // quoted form the server refuses, so the paste is judged as parsed.
  if (typeof r.expires_at !== 'number' || !Number.isInteger(r.expires_at)) {
    return {
      ok: false,
      error:
        'expires_at must be an unquoted whole number of unix seconds — the server refuses a quoted one',
    }
  }
  return {
    ok: true,
    body: {
      release_id: r.release_id,
      job_id: r.job_id,
      expires_at: r.expires_at,
      hmac: r.hmac,
    },
  }
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
  // blocker_since anchors the page's "Waiting: … elapsed" — the job's
  // updated_at at the moment the blocker was last written. Absent when no
  // blocker is recorded (L7: absent ≠ empty).
  blocker_since?: string
  timestamps?: Record<string, string>
  receipt_url?: string
  error?: string
  // UI-GUIDE N-5: the worker's recovery text (stale at start/resume, the
  // attempts cap) — those set ONLY this, never error.
  recovery_reason?: string
  status?: number
}

// ── POST /api/updates/install-with-password (owner password flow) ──
// Same result shape as /api/updates/install; the password travels ONLY in this
// request body and is never echoed by the page.
export interface InstallWithPasswordResult {
  ok: boolean
  job_id?: string
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

  async updatesStatus(silent = true): Promise<UpdatesStatusResult> {
    const res = await httpClient.request<UpdatesStatus>(`${API_BASE}/updates`, {
      headers: UPDATE_HEADERS,
      silent,
    })
    // statusCode is undefined on success and carries the refusal code (403
    // not-enrolled) on failure — the badge/page backoff hangs off it ([5]).
    return {
      status: res.success && res.data ? res.data : null,
      statusCode: res.statusCode,
    }
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

  // The body is EXACTLY the line `vl-updater-bootstrap authorize` prints
  // (json.Marshal of updateauth.Grant, parsed by parseInstallAuthorization
  // above — the caller pastes, never retypes). The INLINE shape stays here
  // on purpose: api/handler_updates_web_body_test.go reads it to prove the
  // web client's declared keys/types are what ParseInstallRequest accepts.
  // expires_at is unix seconds as a JSON NUMBER: the server parses the raw
  // bytes (internal/updateauth/strict.go rawUnixSeconds) and answers a
  // quoted one 400 — the MAC is over its decimal text, so no other encoding
  // may alias it (PR #200 fold F1).
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

  async installWithPassword(
    releaseId: string,
    password: string
  ): Promise<InstallWithPasswordResult> {
    const payload = { release_id: releaseId, password }
    const res = await httpClient.request<{ job_id: string; error?: string }>(
      `${API_BASE}/updates/install-with-password`,
      { method: 'POST', data: payload, headers: UPDATE_HEADERS, silent: true }
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

  // W-ADDON-ONE-CLICK: a HELD job is retried from the page with the owner's
  // password (same proof as Update) — never `vl-updater resume` from a
  // terminal. The password travels ONLY in this request body and is never
  // echoed. Same refusal shape as install-with-password (429 = lockout).
  async retry(
    jobId: string,
    password: string
  ): Promise<InstallWithPasswordResult> {
    const payload = { job_id: jobId, password }
    const res = await httpClient.request<{ job_id: string; error?: string }>(
      `${API_BASE}/updates/retry`,
      { method: 'POST', data: payload, headers: UPDATE_HEADERS, silent: true }
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

  // The receipt route sits behind the same M3 gate as every /updates* call;
  // a bare <a href> navigation cannot carry X-VL-Update and 403s. The page
  // downloads through this method instead (OQ-7).
  async receipt(
    id: string,
    silent = true
  ): Promise<{ data: unknown; error?: string } | null> {
    const res = await httpClient.request<unknown>(
      `${API_BASE}/updates/jobs/${encodeURIComponent(id)}/receipt`,
      { headers: UPDATE_HEADERS, silent }
    )
    if (!res.success) return { data: null, error: res.message }
    return { data: res.data }
  },
}
