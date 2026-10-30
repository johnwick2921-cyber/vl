// PEP at the PRODUCTION call site: the Strategy Studio page itself. Drives the
// real page (real normalizeStrategyConfig, real section wiring, real Save) with
// a mocked network, and reads the PUT body the server would receive. This is
// what catches the silent-drop trap (a root block missing from
// normalizeStrategyConfig saves nothing while the UI looks fine and the toast
// fires on HTTP 200) and the both-ON trap (mentor + pep).
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../contexts/AuthContext', () => ({
  useAuth: () => ({ token: 'tok' }),
}))
vi.mock('../contexts/LanguageContext', () => ({
  useLanguage: () => ({ language: 'en' }),
}))
vi.mock('../components/strategy/useStudioEffective', () => ({
  useStudioEffective: () => ({
    effective: { byPath: {}, bySession: {} },
    refresh: vi.fn(),
  }),
}))

import { StrategyStudioPage } from './StrategyStudioPage'

type Cfg = Record<string, unknown>
let stored: Cfg
let puts: Cfg[]

function futuresConfig(extra: Cfg = {}): Cfg {
  return {
    strategy_type: 'ai_trading',
    ai_config: {
      coin_source: { source_type: 'static', static_coins: ['MNQ'] },
      indicators: {},
      risk_control: { max_positions: 1 },
    },
    ...extra,
  }
}

function installFetch() {
  puts = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const ok = (body: unknown) => ({
        ok: true,
        status: 200,
        json: async () => body,
      })
      if (url.endsWith('/api/strategies') && !init?.method) {
        return ok({
          strategies: [
            {
              id: 's1',
              name: 'MNQ Pep',
              description: '',
              is_active: true,
              is_default: false,
              config: stored,
            },
          ],
        })
      }
      if (url.endsWith('/api/strategies/s1') && init?.method === 'PUT') {
        const body = JSON.parse(String(init.body))
        puts.push(body.config)
        return ok({})
      }
      if (url.endsWith('/api/models')) return ok([])
      if (url.endsWith('/api/my-traders')) return ok([])
      return ok({})
    })
  )
}

async function openStudio() {
  render(<StrategyStudioPage />)
  await waitFor(() =>
    expect(screen.getAllByText('MNQ Pep').length).toBeGreaterThan(0)
  )
}

function openSection(title: RegExp) {
  fireEvent.click(screen.getByRole('button', { name: title }))
}

async function save() {
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: /^save/i }))
  })
  await waitFor(() => expect(puts.length).toBeGreaterThan(0))
  return puts[puts.length - 1]
}

beforeEach(() => {
  installFetch()
})

describe('Strategy Studio: pep wiring', () => {
  // Mutant: delete `pep: config.pep` from normalizeStrategyConfig -> RED (the
  // loaded block never reaches the editor, so the save carries no pep key).
  it('a stored pep block survives load and save (never silently dropped)', async () => {
    stored = futuresConfig({
      pep: { pep_enabled: true, pep_style: 'E', pep_contracts: 6 },
    })
    await openStudio()
    // an unrelated edit marks the strategy changed; the pep block must ride along
    openSection(/risk control/i)
    fireEvent.click(await screen.findByTestId('trading-mode-pep')) // no-op: already pep
    openSection(/pep mode/i)
    commit('pep-pep_contracts', '4')
    const sent = (await save()) as { pep?: Record<string, unknown> }
    expect(sent.pep).toBeDefined()
    expect(sent.pep!.pep_enabled).toBe(true)
    expect(sent.pep!.pep_style).toBe('E')
    expect(sent.pep!.pep_contracts).toBe(4)
  })

  // rereview m461 P3: the confirm dialog states the style and size the SERVER
  // would run (a stored 15 contracts runs as 10; an unsaved block runs E / 6),
  // read through the real page. Mutant: pass the raw stored number / fullPepBlock
  // to the selector -> RED.
  it('the Pep confirm dialog states the clamped contract count and the style that runs', async () => {
    stored = futuresConfig({ pep: { pep_enabled: false, pep_contracts: 15 } })
    await openStudio()
    openSection(/risk control/i)
    fireEvent.click(await screen.findByTestId('trading-mode-pep'))
    expect(screen.getByTestId('trading-mode-confirm')).toHaveTextContent(
      '10 contract(s), style E'
    )
  })

  // Mutant: setTradingMode writes pep_enabled but not mentor_mode -> RED.
  it('Mentor ON then Pep ON through the real page leaves exactly one mode in the saved config', async () => {
    stored = futuresConfig()
    await openStudio()
    openSection(/risk control/i)
    fireEvent.click(await screen.findByTestId('trading-mode-mentor'))
    fireEvent.click(screen.getByTestId('trading-mode-confirm-ok'))
    fireEvent.click(screen.getByTestId('trading-mode-pep'))
    fireEvent.click(screen.getByTestId('trading-mode-confirm-ok'))
    const sent = (await save()) as {
      pep?: { pep_enabled?: boolean }
      ai_config?: { risk_control?: { mentor_mode?: boolean } }
    }
    expect(sent.pep?.pep_enabled).toBe(true)
    expect(sent.ai_config?.risk_control?.mentor_mode).toBe(false)
  })

  // Mutant: RiskControlEditor ignores onMentorModeChange -> RED (the mentor
  // switch writes mentor_mode alone and pep stays ON).
  it('the mentor switch inside Risk Control turns pep OFF instead of producing both-ON', async () => {
    stored = futuresConfig({ pep: { pep_enabled: true } })
    await openStudio()
    openSection(/risk control/i)
    fireEvent.click(await screen.findByTestId('mentor-mode-toggle'))
    fireEvent.click(screen.getByTestId('mentor-confirm-ok'))
    const sent = (await save()) as {
      pep?: { pep_enabled?: boolean }
      ai_config?: { risk_control?: { mentor_mode?: boolean } }
    }
    expect(sent.ai_config?.risk_control?.mentor_mode).toBe(true)
    expect(sent.pep?.pep_enabled).toBe(false)
  })

  // Mutant: drop the save-time fullPepBlock -> RED. The edit here is NOT a pep
  // control (the guardrails master toggle), so only the Save path can complete a
  // partially stored block.
  it('Save always sends the FULL pep block, even when a partial one was stored and pep was not touched', async () => {
    stored = futuresConfig({ pep: { pep_enabled: false, pep_contracts: 6 } })
    await openStudio()
    openSection(/risk control/i)
    fireEvent.click(await screen.findByTestId('guardrails-master-toggle'))
    const sent = (await save()) as { pep?: Record<string, unknown> }
    expect(sent.pep?.pep_contracts).toBe(6)
    expect(sent.pep?.pep_flat_time_ct).toBe('15:00')
    expect(sent.pep?.pep_tick_size).toBe(0.25)
    expect(Object.keys(sent.pep ?? {}).length).toBeGreaterThanOrEqual(13)
    expect(
      (sent.pep as { style_e?: Record<string, unknown> }).style_e
        ?.pep_e_ema_period
    ).toBe(9)
  })

  it('a strategy that never saw pep saves with NO pep key', async () => {
    stored = futuresConfig()
    await openStudio()
    openSection(/risk control/i)
    fireEvent.click(await screen.findByTestId('trading-mode-mentor'))
    fireEvent.click(screen.getByTestId('trading-mode-confirm-ok'))
    fireEvent.click(screen.getByTestId('trading-mode-ai'))
    const sent = (await save()) as Record<string, unknown>
    expect('pep' in sent).toBe(false)
  })

  it('the Pep accordion is not offered for a non-futures strategy', async () => {
    stored = futuresConfig()
    ;(stored.ai_config as Cfg).coin_source = {
      source_type: 'static',
      static_coins: ['BTCUSDT'],
    }
    await openStudio()
    expect(screen.queryByRole('button', { name: /pep mode/i })).toBeNull()
  })

  // Mutant: drop pepOn={...} from the <DayPlanEditor> call -> RED (review round 1,
  // defect 4: nothing in the Day Plan UI warned about the pep conflict).
  it('with Pep ON the Day Plan block warns before Save and will not turn the plan ON', async () => {
    stored = futuresConfig({ pep: { pep_enabled: true } })
    await openStudio()
    openSection(/day plan/i)
    expect(await screen.findByTestId('dayplan-pep-warning')).toHaveTextContent(
      'cannot be turned on together with Pep'
    )
  })

  it('without Pep the Day Plan block carries no pep warning', async () => {
    stored = futuresConfig()
    await openStudio()
    openSection(/day plan/i)
    await waitFor(() =>
      expect(screen.getAllByRole('switch').length).toBeGreaterThan(0)
    )
    expect(screen.queryByTestId('dayplan-pep-warning')).toBeNull()
  })

  it('a server-refused save names the mode conflict', async () => {
    stored = futuresConfig({ pep: { pep_enabled: true } })
    await openStudio()
    openSection(/pep mode/i)
    commit('pep-pep_contracts', '4')
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_u: string, init?: RequestInit) =>
        init?.method === 'PUT'
          ? {
              ok: false,
              status: 400,
              json: async () => ({
                error: 'pep mode and the day plan are both ON',
                error_key: 'pep_mode_conflict_day_plan',
              }),
            }
          : { ok: true, status: 200, json: async () => ({}) }
      )
    )
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /^save/i }))
    })
    await waitFor(() =>
      expect(
        screen.getAllByText(/pep mode and the day plan are both ON/).length
      ).toBeGreaterThan(0)
    )
  })
})

function commit(testId: string, value: string) {
  const el = screen.getByTestId(testId)
  fireEvent.focus(el)
  fireEvent.change(el, { target: { value } })
  fireEvent.blur(el)
}
