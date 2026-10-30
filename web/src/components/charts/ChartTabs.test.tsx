import {
  render,
  screen,
  fireEvent,
  cleanup,
  waitFor,
} from '@testing-library/react'
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { ChartTabs } from './ChartTabs'
vi.mock('../../contexts/LanguageContext', () => ({
  useLanguage: () => ({ language: 'en' }),
}))
vi.mock('./EquityChart', () => ({
  EquityChart: () => <div data-testid="equity" />,
}))
vi.mock('./AdvancedChart', () => ({
  AdvancedChart: ({
    symbol,
    selectedAccount,
    mentorLevels,
    mentorTrendlines,
  }: {
    symbol: string
    selectedAccount?: string
    mentorLevels?: unknown[]
    mentorTrendlines?: unknown[]
  }) => (
    <div
      data-testid="market"
      data-account={selectedAccount}
      data-mentor-levels={mentorLevels?.length ?? 0}
      data-mentor-trendlines={mentorTrendlines?.length ?? 0}
    >
      {symbol}
    </div>
  ),
}))
beforeEach(() =>
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => ({ json: async () => ({ symbols: [] }) }))
  )
)
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})
describe('ChartTabs composition modes', () => {
  it('keeps the original equity default for callers without marketOnly', () => {
    render(<ChartTabs traderId="test" exchangeId="ninjatrader" />)
    expect(screen.getByTestId('equity')).toBeInTheDocument()
    expect(screen.queryByRole('combobox')).toBeNull()
  })
  it('shows market only, including when the prop changes after mount', async () => {
    const { rerender } = render(
      <ChartTabs traderId="test" exchangeId="ninjatrader" />
    )
    rerender(<ChartTabs traderId="test" exchangeId="ninjatrader" marketOnly />)
    await screen.findByTestId('market')
    expect(screen.queryByTestId('equity')).toBeNull()
    expect(
      screen.queryByRole('button', { name: 'Account Equity Curve' })
    ).toBeNull()
  })
  it('passes selected account changes into the order snapshot scope', () => {
    const { rerender } = render(
      <ChartTabs traderId="test" selectedAccount="SimA" marketOnly />
    )
    expect(screen.getByTestId('market')).toHaveAttribute('data-account', 'SimA')
    rerender(<ChartTabs traderId="test" selectedAccount="SimB" marketOnly />)
    expect(screen.getByTestId('market')).toHaveAttribute('data-account', 'SimB')
  })
  it('offers the remaining market through the same mobile selection path', async () => {
    render(<ChartTabs traderId="test" exchangeId="ninjatrader" marketOnly />)
    const select = screen.getByRole('combobox', { name: 'Market Chart' })
    fireEvent.change(select, { target: { value: 'ninjatrader' } })
    expect((await screen.findAllByText('MNQ')).length).toBeGreaterThan(0)
  })
})

// UG-2 (release #24): a mentor level is a price on ONE instrument. The chart
// draws the overlays only on a tab showing the mentor's symbol, and says so
// on any other tab (never a silent hide).
// Mutant: pass mentorLevels through unconditionally → RED.
describe('UG-2 mentor overlays follow the symbol', () => {
  const levels = [{ price: 30000, label: 'mentor key level' }]
  const tls = [
    {
      side: 'long',
      p0_ms: 0,
      p0_px: 1,
      p1_ms: 60_000,
      p1_px: 2,
      now_ms: 0,
      now_px: 0,
      in_level: true,
    },
  ]
  it('draws them on the mentor symbol tab', () => {
    render(
      <ChartTabs
        traderId="t"
        exchangeId="ninjatrader"
        marketOnly
        selectedSymbol="MNQ"
        mentorLevels={levels}
        mentorTrendlines={tls}
        mentorSymbol="MNQ"
      />
    )
    const m = screen.getByTestId('market')
    expect(m).toHaveAttribute('data-mentor-levels', '1')
    expect(m).toHaveAttribute('data-mentor-trendlines', '1')
    expect(screen.getByTestId('mentor-overlay-note')).toHaveTextContent(
      'mentor lines: MNQ'
    )
  })
  it('hides them on another symbol and says why', async () => {
    render(
      <ChartTabs
        traderId="t"
        exchangeId="ninjatrader"
        marketOnly
        selectedSymbol="ES"
        mentorLevels={levels}
        mentorTrendlines={tls}
        mentorSymbol="MNQ"
      />
    )
    // AnimatePresence (mode="wait") keeps the MNQ chart until its exit
    // completes; wait for the ES chart itself.
    await waitFor(() =>
      expect(screen.getByTestId('market')).toHaveTextContent('ES')
    )
    const m = screen.getByTestId('market')
    expect(m).toHaveAttribute('data-mentor-levels', '0')
    expect(m).toHaveAttribute('data-mentor-trendlines', '0')
    expect(screen.getByTestId('mentor-overlay-note')).toHaveTextContent(
      'mentor lines hidden — they belong to MNQ, this chart shows ES'
    )
  })
  it('UG-3: a stale read is marked on the chart', () => {
    render(
      <ChartTabs
        traderId="t"
        exchangeId="ninjatrader"
        marketOnly
        selectedSymbol="MNQ"
        mentorLevels={levels}
        mentorSymbol="MNQ"
        mentorStaleSinceMs={Date.UTC(2026, 9, 14, 15, 7)}
      />
    )
    expect(screen.getByTestId('mentor-overlay-note')).toHaveTextContent(
      'mentor lines: MNQ · stale since 10:07 CT'
    )
  })

  it('m421b C8: an EMA-only read still shows the overlays (ema_series counts)', () => {
    render(
      <ChartTabs
        traderId="t"
        exchangeId="ninjatrader"
        marketOnly
        selectedSymbol="MNQ"
        mentorEMASeries={[{ ms: 1, ema9: 1, ema34: 2 }]}
        mentorSymbol="MNQ"
      />
    )
    expect(screen.getByTestId('mentor-overlay-note')).toBeInTheDocument()
  })
})

// PEP: the opening-range box lines follow the same symbol rule as the mentor
// lines (one instrument, said when hidden) and share the one price-line channel.
// Mutant: draw pepLines unconditionally (drop the sameChartSymbol gate) -> RED.
describe('pep box lines follow the symbol', () => {
  const lines = [
    { price: 100, label: 'pep box high 100' },
    { price: 80, label: 'pep box low 80' },
  ]
  it('draws them on the pep symbol tab', () => {
    render(
      <ChartTabs
        traderId="t"
        exchangeId="ninjatrader"
        marketOnly
        selectedSymbol="MNQ"
        pepLines={lines}
        pepSymbol="MNQ"
      />
    )
    expect(screen.getByTestId('market')).toHaveAttribute(
      'data-mentor-levels',
      '2'
    )
    expect(screen.getByTestId('pep-overlay-note')).toHaveTextContent(
      'pep box lines: MNQ'
    )
  })
  it('hides them on another symbol and says why', async () => {
    render(
      <ChartTabs
        traderId="t"
        exchangeId="ninjatrader"
        marketOnly
        selectedSymbol="ES"
        pepLines={lines}
        pepSymbol="MNQ"
      />
    )
    await waitFor(() =>
      expect(screen.getByTestId('market')).toHaveTextContent('ES')
    )
    expect(screen.getByTestId('market')).toHaveAttribute(
      'data-mentor-levels',
      '0'
    )
    expect(screen.getByTestId('pep-overlay-note')).toHaveTextContent(
      'pep box lines hidden — they belong to MNQ, this chart shows ES'
    )
  })
  it('mentor and pep lines share the one channel without displacing each other', () => {
    render(
      <ChartTabs
        traderId="t"
        exchangeId="ninjatrader"
        marketOnly
        selectedSymbol="MNQ"
        mentorLevels={[{ price: 1, label: 'mentor x' }]}
        mentorSymbol="MNQ"
        pepLines={lines}
        pepSymbol="MNQ"
      />
    )
    expect(screen.getByTestId('market')).toHaveAttribute(
      'data-mentor-levels',
      '3'
    )
  })
})
