// W-ONE-BUTTON M5 (U2) — UpdatesPage truth-rule tests at the component call
// site: API-only truth, n/a for the unknowable, absent job ≠ [], the install
// button disabled with its exact text while the review is open.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

const mocks = vi.hoisted(() => ({
  health: vi.fn(),
  maintenance: vi.fn(),
  installationGate: vi.fn(),
  updatesStatus: vi.fn(),
  check: vi.fn(),
  install: vi.fn(),
  job: vi.fn(),
  getTraders: vi.fn(),
  getStrategyEffective: vi.fn(),
  getExchangeConfigs: vi.fn(),
  request: vi.fn(),
}))
vi.mock('../lib/api/updates', () => ({
  updatesApi: {
    health: mocks.health,
    maintenance: mocks.maintenance,
    installationGate: mocks.installationGate,
    updatesStatus: mocks.updatesStatus,
    check: mocks.check,
    install: mocks.install,
    job: mocks.job,
  },
  INSTALL_AUTHZ_UNDER_REVIEW: true,
  INSTALL_UNDER_REVIEW_TEXT: 'install authorization under review',
}))
vi.mock('../lib/api/traders', () => ({
  traderApi: { getTraders: mocks.getTraders },
}))
vi.mock('../lib/api/strategyEffective', () => ({
  strategyEffectiveApi: { getStrategyEffective: mocks.getStrategyEffective },
}))
vi.mock('../lib/api/config', () => ({
  configApi: { getExchangeConfigs: mocks.getExchangeConfigs },
}))
vi.mock('../lib/httpClient', () => ({
  httpClient: { request: mocks.request },
}))
vi.mock('../router/selectedTrader', () => ({
  loadStoredTraderId: () => undefined,
}))
vi.mock('../contexts/LanguageContext', () => ({
  useLanguage: () => ({ language: 'en' }),
}))

import UpdatesPage from './UpdatesPage'

const heldMaintenance = {
  held: true,
  state: 'held' as const,
  job_id: 'job-7',
  since: '2026-09-24T06:00:00Z',
  in_flight_sends: 0,
  drained: false,
  addon_ack: null,
}

beforeEach(() => {
  for (const fn of Object.values(mocks)) fn.mockReset()
  mocks.health.mockResolvedValue({
    status: 'ok',
    time: '2026-09-24T06:00:00Z',
    revision: 'abc123',
  })
  mocks.maintenance.mockResolvedValue({
    held: false,
    state: 'clear',
    in_flight_sends: 0,
    drained: true,
    addon_ack: null,
  })
  mocks.installationGate.mockResolvedValue({
    ready: false,
    job_id: 'n/a',
    legs: [],
    traders: [],
    note: 'every leg must pass',
  })
  mocks.updatesStatus.mockResolvedValue({
    enrolled: true,
    manifest_verifier: 'stub',
    install_enabled: false,
  })
  mocks.getTraders.mockResolvedValue([])
  mocks.getExchangeConfigs.mockResolvedValue([])
})

describe('UpdatesPage', () => {
  it('renders only API truth: revision read, addon_ack null shows "no ack yet"', async () => {
    render(<UpdatesPage />)
    await waitFor(() => expect(screen.getByText('abc123')).toBeTruthy())
    expect(screen.getByText('no ack yet')).toBeTruthy()
    // gate ready=false is the API's own verdict, rendered exactly as returned
    expect(screen.getByText('false')).toBeTruthy()
  })

  it('shows "no update job" when the API names none', async () => {
    render(<UpdatesPage />)
    await waitFor(() => expect(screen.getByText('no update job')).toBeTruthy())
  })

  it('a 404 job keeps the server text, not a fabricated state', async () => {
    mocks.maintenance.mockResolvedValue(heldMaintenance)
    mocks.job.mockResolvedValue({ error: 'not found', status: 404 })
    render(<UpdatesPage />)
    await waitFor(() => expect(screen.getByText(/not found/)).toBeTruthy())
  })

  it('install stays disabled with the exact review text and never POSTs', async () => {
    render(<UpdatesPage />)
    await waitFor(() =>
      expect(
        screen.getByText('install authorization under review')
      ).toBeTruthy()
    )
    const button = screen.getByTestId('update-button') as HTMLButtonElement
    expect(button.disabled).toBe(true)
    expect(mocks.install).not.toHaveBeenCalled()
  })

  it('button shows Blocked when the API says install_enabled=false', async () => {
    mocks.updatesStatus.mockResolvedValue({
      enrolled: true,
      manifest_verifier: 'stub',
      install_enabled: false,
    })
    render(<UpdatesPage />)
    await waitFor(() => expect(screen.getByText('Blocked')).toBeTruthy())
  })

  it('gate legs render with each leg’s exact detail text', async () => {
    mocks.installationGate.mockResolvedValue({
      ready: false,
      job_id: 'n/a',
      legs: [
        {
          name: 'hold',
          pass: false,
          detail: 'held: job-7 since 2026-09-24',
          source: 'maintenance hold file',
        },
      ],
      traders: ['t1'],
      note: 'every leg must pass',
    })
    render(<UpdatesPage />)
    await waitFor(() =>
      expect(screen.getByText('held: job-7 since 2026-09-24')).toBeTruthy()
    )
  })

  it('4H completed bars: N = PivotWindow+4 from the effective settings, n stays n/a', async () => {
    mocks.getTraders.mockResolvedValue([
      { trader_id: 't1', trader_name: 'T', ai_model: 'm', strategy_id: 's/1' },
    ])
    mocks.getStrategyEffective.mockResolvedValue({
      strategy_id: 's/1',
      session: null,
      venue: null,
      settings: [
        {
          path: 'picture_htf.pivot_window',
          status: 'resolved',
          ui_label: 'PivotWindow',
          stored: { present: true, value: 8 },
          effective: 8,
          origin: 'stored',
          scope: 'strategy',
          resolver: 'direct',
          resolved: true,
        },
      ],
      coverage: { resolved: 1, total: 1, unresolved: [], not_enumerated: [] },
    })
    render(<UpdatesPage />)
    await waitFor(() => expect(screen.getByText(/of 12/)).toBeTruthy())
    // the completed count is n/a — never computed in the browser
    expect(screen.getAllByText('n/a').length).toBeGreaterThan(0)
  })

  it('P1: reload is a two-step confirm that POSTs the exact 4h body through httpClient', async () => {
    const rawFetch = vi.fn()
    vi.stubGlobal('fetch', rawFetch)
    mocks.getTraders.mockResolvedValue([
      {
        trader_id: 't1',
        trader_name: 'T',
        ai_model: 'm',
        strategy_id: 's/1',
        exchange_id: 'ex1',
      },
    ])
    mocks.getExchangeConfigs.mockResolvedValue([
      { id: 'ex1', nt_instrument_name: 'MNQ' },
    ])
    mocks.getStrategyEffective.mockResolvedValue({
      strategy_id: 's/1',
      session: null,
      venue: null,
      settings: [
        {
          path: 'picture_htf.pivot_window',
          status: 'resolved',
          ui_label: 'PivotWindow',
          stored: { present: true, value: 8 },
          effective: 8,
          origin: 'stored',
          scope: 'strategy',
          resolver: 'direct',
          resolved: true,
        },
      ],
      coverage: { resolved: 1, total: 1, unresolved: [], not_enumerated: [] },
    })
    mocks.request.mockResolvedValue({
      success: true,
      data: {
        ok: true,
        note: 'deep bars_subscribe sent; run action=diff after ~30s.',
      },
    })
    render(<UpdatesPage />)

    await waitFor(() => {
      const btn = screen.getByTestId('reload-history') as HTMLButtonElement
      expect(btn.disabled).toBe(false)
    })

    // step 1: click asks for confirmation, nothing is sent yet
    fireEvent.click(screen.getByTestId('reload-history'))
    expect(
      screen.getByText('Send a 4H backfill of 12 bars to NT8?')
    ).toBeTruthy()
    expect(mocks.request).not.toHaveBeenCalled()

    // step 2: confirm POSTs the exact body through httpClient (auth header)
    fireEvent.click(screen.getByTestId('confirm-backfill'))
    await waitFor(() => expect(mocks.request).toHaveBeenCalledTimes(1))
    expect(mocks.request).toHaveBeenCalledWith('/api/nt/bar-arbiter', {
      method: 'POST',
      data: {
        trader_id: 't1',
        action: 'backfill',
        symbol: 'MNQ',
        timeframe: '4h',
        bars_back: 12,
      },
    })
    // the server's response text is shown
    await waitFor(() =>
      expect(screen.getByText(/deep bars_subscribe sent/)).toBeTruthy()
    )
    // the request goes through httpClient, never raw fetch
    expect(rawFetch).not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it('P1: the reload button is DISABLED with "PivotWindow unknown" when the knob is null', async () => {
    mocks.getTraders.mockResolvedValue([
      { trader_id: 't1', trader_name: 'T', ai_model: 'm' },
    ])
    render(<UpdatesPage />)
    await waitFor(() =>
      expect(screen.getByText('PivotWindow unknown')).toBeTruthy()
    )
    expect(
      (screen.getByTestId('reload-history') as HTMLButtonElement).disabled
    ).toBe(true)
    expect(mocks.request).not.toHaveBeenCalled()
  })

  it('P1: the reload button is DISABLED with "Futures symbol unknown" when the trader row has none', async () => {
    mocks.getTraders.mockResolvedValue([
      {
        trader_id: 't1',
        trader_name: 'T',
        ai_model: 'm',
        strategy_id: 's/1',
        exchange_id: 'ex1',
      },
    ])
    mocks.getStrategyEffective.mockResolvedValue({
      strategy_id: 's/1',
      session: null,
      venue: null,
      settings: [
        {
          path: 'picture_htf.pivot_window',
          status: 'resolved',
          ui_label: 'PivotWindow',
          stored: { present: true, value: 8 },
          effective: 8,
          origin: 'stored',
          scope: 'strategy',
          resolver: 'direct',
          resolved: true,
        },
      ],
      coverage: { resolved: 1, total: 1, unresolved: [], not_enumerated: [] },
    })
    render(<UpdatesPage />)
    await waitFor(() =>
      expect(screen.getByText('Futures symbol unknown')).toBeTruthy()
    )
    expect(
      (screen.getByTestId('reload-history') as HTMLButtonElement).disabled
    ).toBe(true)
    expect(mocks.request).not.toHaveBeenCalled()
  })
})
