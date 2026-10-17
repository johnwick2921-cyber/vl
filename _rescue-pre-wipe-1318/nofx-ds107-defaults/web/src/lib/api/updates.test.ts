// W-ONE-BUTTON M5 (U1) — shape pins for the updates API client. Each fixture
// quotes the Go gin.H keys / JSON tags verbatim so a server-side rename or a
// shape drift fails here, at the component call site, before it reaches the
// page.

import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ request: vi.fn() }))
vi.mock('../httpClient', () => ({
  httpClient: { request: mocks.request },
}))

import { updatesApi } from './updates'

describe('updatesApi shape pins', () => {
  beforeEach(() => mocks.request.mockReset())

  it('GET /api/maintenance pins the MaintenanceStatusView keys', async () => {
    mocks.request.mockResolvedValue({
      success: true,
      data: {
        held: true,
        state: 'held',
        job_id: 'job-7',
        since: '2026-09-24T06:00:00Z',
        reason: 'operator hold',
        in_flight_sends: 0,
        drained: true,
        addon_ack: {
          received: '2026-09-24T06:00:00Z',
          age_ms: 1200,
          held: true,
          job_id: 'job-7',
          queued_commands: 0,
          build_id: 'b9',
          accept_seq: 4,
        },
        withdraw: null,
      },
    })
    const out = await updatesApi.maintenance()
    expect(mocks.request).toHaveBeenCalledWith('/api/maintenance', {
      silent: true,
    })
    expect(out?.state).toBe('held')
    expect(out?.addon_ack?.build_id).toBe('b9')
    expect(out?.withdraw).toBeNull()
  })

  it('GET /api/maintenance pins the withdraw shape (trader/withdraw.go WithdrawView)', async () => {
    mocks.request.mockResolvedValue({
      success: true,
      data: {
        held: true,
        state: 'held',
        in_flight_sends: 0,
        drained: false,
        addon_ack: null,
        withdraw: {
          requested: true,
          pending: [11, 12],
          confirmed: [],
          filled: [3],
          ended: [{ id: 9, state: 'rejected' }],
        },
      },
    })
    const out = await updatesApi.maintenance()
    expect(out?.addon_ack).toBeNull()
    expect(out?.withdraw?.pending).toEqual([11, 12])
    expect(out?.withdraw?.ended[0].state).toBe('rejected')
  })

  it('GET /api/installation-gate pins the InstallationGate keys', async () => {
    mocks.request.mockResolvedValue({
      success: true,
      data: {
        ready: false,
        job_id: 'n/a',
        legs: [
          {
            name: 'hold',
            pass: false,
            detail: 'held: job-7',
            source: 'maintenance hold file',
          },
        ],
        traders: ['t1'],
        note: 'every leg must pass',
      },
    })
    const out = await updatesApi.installationGate()
    expect(mocks.request).toHaveBeenCalledWith('/api/installation-gate', {
      silent: true,
    })
    expect(out?.ready).toBe(false)
    expect(out?.legs[0].name).toBe('hold')
  })

  it('GET /api/updates pins the status keys (enrolled/manifest_verifier/install_enabled)', async () => {
    mocks.request.mockResolvedValue({
      success: true,
      data: {
        enrolled: true,
        manifest_verifier: 'stub',
        install_enabled: false,
      },
    })
    const out = await updatesApi.updatesStatus()
    expect(out?.enrolled).toBe(true)
    expect(out?.install_enabled).toBe(false)
  })

  it('POST /api/updates/check pins {checked, reason}', async () => {
    mocks.request.mockResolvedValue({
      success: true,
      data: { checked: false, reason: 'no release source in this build' },
    })
    const out = await updatesApi.check()
    expect(mocks.request).toHaveBeenCalledWith('/api/updates/check', {
      method: 'POST',
      headers: { 'X-NOFX-Update': '1' },
      silent: true,
    })
    expect(out).toEqual({
      checked: false,
      reason: 'no release source in this build',
    })
  })

  it('POST /api/updates/install 202 pins {job_id}; a refusal carries the server text', async () => {
    mocks.request.mockResolvedValueOnce({
      success: true,
      statusCode: 202,
      data: { job_id: 'job-9' },
    })
    expect(
      await updatesApi.install({
        release_id: 'r1',
        job_id: 'job-9',
        expires_at: 1893456000,
        hmac: 'x',
      })
    ).toEqual({ ok: true, job_id: 'job-9' })

    mocks.request.mockResolvedValueOnce({
      success: false,
      statusCode: 403,
      // the server's ONE 403 body for every /updates refusal (api/handler_updates.go
      // errForbiddenBody); 'install: MAC mismatch' is a server LOG category, never a body
      data: { error: 'forbidden' },
      message: '',
    })
    const refused = await updatesApi.install({
      release_id: 'r1',
      job_id: 'job-9',
      expires_at: 1893456000,
      hmac: 'x',
    })
    expect(refused.ok).toBe(false)
    expect(refused.error).toBe('forbidden')
    expect(refused.status).toBe(403)
  })

  it('GET /api/health pins the {status, time, revision} keys', async () => {
    mocks.request.mockResolvedValue({
      success: true,
      data: { status: 'ok', time: '2026-09-24T06:00:00Z', revision: 'abc123' },
    })
    const out = await updatesApi.health()
    expect(mocks.request).toHaveBeenCalledWith('/api/health', { silent: true })
    expect(out?.revision).toBe('abc123')
  })

  it('GET /api/updates/jobs/:id pins the M3 404 {error:"not found"} shape', async () => {
    mocks.request.mockResolvedValue({
      success: false,
      statusCode: 404,
      data: { error: 'not found' },
    })
    const out = await updatesApi.job('job-1')
    expect(mocks.request).toHaveBeenCalledWith('/api/updates/jobs/job-1', {
      headers: { 'X-NOFX-Update': '1' },
      silent: true,
    })
    expect(out).toEqual({ error: 'not found', status: 404 })
  })
})
