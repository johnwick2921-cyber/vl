// PEP on the PRODUCTION dashboard: the real page composes the mode chip, the
// Pep truth card and the box lines on the chart from the two truth hooks. The
// hooks are mocked (their own contracts are pinned beside them); everything
// from the page down — chip rule, card, ChartTabs symbol gating, the price-line
// channel — is real.
import type { ComponentProps, ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { TraderDashboardPage } from './TraderDashboardPage'
import type { PepTruth } from '../lib/api/traders'
import {
  GOLDEN_CONFLICT,
  GOLDEN_OFF,
  GOLDEN_WAITING_SIGNAL,
} from '../components/pep/pepTruthGolden'

const hooks = vi.hoisted(() => ({
  mentor: { truth: null, stale: false } as {
    truth: unknown
    stale: boolean
  },
  pep: { truth: null, stale: false, status: 'loading' } as {
    truth: unknown
    stale: boolean
    status: 'loading' | 'ok' | 'error'
  },
  chartLevels: [] as { price: number; label: string }[],
}))

vi.mock('../contexts/LanguageContext', () => ({
  useLanguage: () => ({ language: 'en' }),
}))
vi.mock('../lib/api', () => ({ api: {} }))
vi.mock('../lib/api/plan', () => ({
  planApi: { getRiskErrors: async () => ({ rows: [] }) },
}))
vi.mock('../components/charts/AdvancedChart', () => ({
  AdvancedChart: ({
    symbol,
    mentorLevels,
  }: {
    symbol: string
    mentorLevels?: { price: number; label: string }[]
  }) => {
    hooks.chartLevels = mentorLevels ?? []
    return <div data-testid="market-chart">{symbol}</div>
  },
}))
vi.mock('../components/charts/EquityChart', () => ({
  EquityChart: () => <div data-testid="equity-chart" />,
}))
vi.mock('../components/plan/PlanCard', () => ({ PlanCard: () => null }))
vi.mock('../components/mentor/MentorTruthCard', () => ({
  MentorTruthCard: () => null,
}))
vi.mock('../components/mentor/useMentorTruth', () => ({
  useMentorTruth: () => hooks.mentor,
}))
vi.mock('../components/pep/usePepTruth', () => ({
  usePepTruth: () => hooks.pep,
}))
vi.mock('../components/trader/AccountSelector', () => ({
  AccountSelector: () => null,
}))
vi.mock('../components/trader/EmergencyFlatButton', () => ({
  EmergencyFlatButton: () => null,
}))
vi.mock('../components/trader/PauseButton', () => ({
  PauseButton: () => null,
  isPauseActive: () => false,
  pauseUntilCT: () => '',
}))
vi.mock('../components/trader/PositionHistory', () => ({
  PositionHistory: () => null,
}))
vi.mock('../components/trader/DecisionAudit', () => ({
  DecisionAudit: () => null,
}))
vi.mock('../components/common/DeepVoidBackground', () => ({
  DeepVoidBackground: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
}))
vi.mock('../components/common/PunkAvatar', () => ({
  PunkAvatar: () => null,
  getTraderAvatar: () => 1,
}))

function props(futures = true): ComponentProps<typeof TraderDashboardPage> {
  return {
    selectedTrader: {
      trader_id: 'test-123456789',
      trader_name: 'Test trader',
      ai_model: 'deepseek',
      exchange_id: 'ex',
      strategy_name: 'MNQ Pep',
    },
    selectedTraderId: 'test-123456789',
    exchanges: [
      {
        id: 'ex',
        exchange_type: futures ? 'ninjatrader' : 'binance',
        name: 'Test',
        account_name: 'Test SIM',
        enabled: true,
        type: 'cex',
      },
    ],
    positions: [],
    decisions: [],
    decisionsLimit: 5,
    onDecisionsLimitChange: vi.fn(),
    onTraderSelect: vi.fn(),
    onNavigateToTraders: vi.fn(),
    lastUpdate: '',
    language: 'en',
  }
}

// The trader lane's REAL JSON (pepTruthGolden.ts), parsed - not a UI-shaped
// object. The server sends no `symbol` yet; the chart tests below add the one
// field the contract test (pepTruthContract.test.ts) requires of the trader lane.
const pepGolden = JSON.parse(GOLDEN_WAITING_SIGNAL) as PepTruth
const pepOn: PepTruth = { ...pepGolden, symbol: 'MNQ' }
const pepOff = JSON.parse(GOLDEN_OFF) as PepTruth

beforeEach(() => {
  hooks.mentor = { truth: null, stale: false }
  hooks.pep = { truth: null, stale: false, status: 'loading' }
  hooks.chartLevels = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => ({ json: async () => ({ status: 'ok', symbols: [] }) }))
  )
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('dashboard: pep', () => {
  // Mutant: delete <PepTruthCard> from the page -> RED.
  it('shows the Pep truth card with the box, signal, trades and countdown when pep is ON', async () => {
    hooks.pep = { truth: pepOn, stale: false, status: 'ok' }
    render(<TraderDashboardPage {...props()} />)
    await screen.findByTestId('market-chart')
    const card = screen.getByTestId('pep-truth-card')
    expect(card).toHaveTextContent('Pep — what trades · E · SIM only')
    expect(screen.getByTestId('pep-truth-box')).toHaveTextContent(
      'high 25100.50 · low 25020.25 · size 80.25'
    )
    expect(screen.getByTestId('pep-truth-signal')).toHaveTextContent(
      'waiting for a break'
    )
    expect(screen.getByTestId('pep-truth-trades')).toHaveTextContent('0 / 1')
    expect(screen.getByTestId('pep-truth-flat')).toHaveTextContent(
      '15:00 CT · in 3h 05m'
    )
  })

  it('hides the card when pep is OFF or unknown', async () => {
    hooks.pep = {
      truth: pepOff,
      stale: false,
      status: 'ok',
    }
    const { unmount } = render(<TraderDashboardPage {...props()} />)
    await screen.findByTestId('market-chart')
    expect(screen.queryByTestId('pep-truth-card')).toBeNull()
    unmount()
    hooks.pep = { truth: null, stale: false, status: 'error' }
    render(<TraderDashboardPage {...props()} />)
    await screen.findByTestId('market-chart')
    expect(screen.queryByTestId('pep-truth-card')).toBeNull()
  })

  // Mutant: drop pepLines={pepLines} from the <ChartTabs> call -> RED.
  it('draws the box high/low on the chart through the price-line channel', async () => {
    hooks.pep = { truth: pepOn, stale: false, status: 'ok' }
    render(<TraderDashboardPage {...props()} />)
    await screen.findByTestId('market-chart')
    expect(hooks.chartLevels.map((l) => l.price)).toEqual([25100.5, 25020.25])
    expect(hooks.chartLevels[0].label).toContain('pep box high')
    expect(screen.getByTestId('pep-overlay-note')).toHaveTextContent(
      'pep box lines: MNQ'
    )
  })

  it('draws NO box line while the box is not drawn yet (never a line at 0)', async () => {
    hooks.pep = {
      truth: {
        ...pepOn,
        box: { ...pepOn.box!, drawn: false, high: 0, low: 0 },
      },
      stale: false,
      status: 'ok',
    }
    render(<TraderDashboardPage {...props()} />)
    await screen.findByTestId('market-chart')
    expect(hooks.chartLevels).toEqual([])
    expect(screen.getByTestId('pep-truth-box-undrawn')).toBeInTheDocument()
  })

  it('does not draw pep lines on a chart showing another symbol, and says why', async () => {
    hooks.pep = {
      truth: { ...pepOn, symbol: 'ES' },
      stale: false,
      status: 'ok',
    }
    render(<TraderDashboardPage {...props()} />)
    await screen.findByTestId('market-chart')
    expect(hooks.chartLevels).toEqual([])
    expect(screen.getByTestId('pep-overlay-note')).toHaveTextContent(
      'pep box lines hidden — they belong to ES'
    )
  })

  // Defect 1 of review round 1: the server's raw JSON carries no symbol, so the
  // box cannot be placed on a chart. That must be SAID, never a silent no-draw.
  it('the server JSON as sent today (no symbol): card live, lines held back, the chart says why', async () => {
    hooks.pep = { truth: pepGolden, stale: false, status: 'ok' }
    render(<TraderDashboardPage {...props()} />)
    await screen.findByTestId('market-chart')
    expect(screen.getByTestId('pep-truth-box')).toHaveTextContent(
      'high 25100.50'
    )
    expect(screen.getByTestId('pep-truth-signal')).toHaveTextContent(
      'waiting for a break'
    )
    expect(hooks.chartLevels).toEqual([])
    expect(screen.getByTestId('pep-overlay-note')).toHaveTextContent(
      'pep box lines hidden — they belong to ?'
    )
  })

  it('a mentor+pep conflict reads as a conflict chip and a loud card, not as pep', async () => {
    hooks.pep = {
      truth: JSON.parse(GOLDEN_CONFLICT) as PepTruth,
      stale: false,
      status: 'ok',
    }
    hooks.mentor = { truth: { enabled: false }, stale: false }
    render(<TraderDashboardPage {...props()} />)
    await screen.findByTestId('market-chart')
    expect(screen.getByTestId('mode-chip')).toHaveAttribute(
      'data-mode',
      'conflict'
    )
    expect(screen.getByTestId('pep-truth-conflict')).toHaveTextContent(
      'NOT started'
    )
  })

  it('mode chip: Pep E / Mentor / AI only when the truths have answered', async () => {
    hooks.pep = { truth: pepOn, stale: false, status: 'ok' }
    const a = render(<TraderDashboardPage {...props()} />)
    await screen.findByTestId('market-chart')
    expect(screen.getByTestId('mode-chip')).toHaveTextContent('Pep E')
    a.unmount()

    hooks.pep = {
      truth: pepOff,
      stale: false,
      status: 'ok',
    }
    hooks.mentor = { truth: { enabled: true }, stale: false }
    const b = render(<TraderDashboardPage {...props()} />)
    await screen.findByTestId('market-chart')
    expect(screen.getByTestId('mode-chip')).toHaveTextContent('Mentor')
    b.unmount()

    hooks.mentor = { truth: { enabled: false }, stale: false }
    const c = render(<TraderDashboardPage {...props()} />)
    await screen.findByTestId('market-chart')
    expect(screen.getByTestId('mode-chip')).toHaveTextContent('AI')
    c.unmount()

    // unknown pep (no answer) must NOT read as "AI"
    hooks.pep = { truth: null, stale: false, status: 'loading' }
    render(<TraderDashboardPage {...props()} />)
    await screen.findByTestId('market-chart')
    expect(screen.queryByTestId('mode-chip')).toBeNull()
  })

  it('no chip and no card for a crypto trader', async () => {
    hooks.pep = { truth: pepOn, stale: false, status: 'ok' }
    render(<TraderDashboardPage {...props(false)} />)
    await screen.findByTestId('market-chart')
    expect(screen.queryByTestId('mode-chip')).toBeNull()
    expect(screen.queryByTestId('pep-truth-card')).toBeNull()
  })
})
