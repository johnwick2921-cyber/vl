import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { ChartTabs } from './ChartTabs'
vi.mock('../../contexts/LanguageContext', () => ({
  useLanguage: () => ({ language: 'en' }),
}))
vi.mock('./EquityChart', () => ({
  EquityChart: () => <div data-testid="equity" />,
}))
vi.mock('./AdvancedChart', () => ({
  AdvancedChart: ({ symbol }: { symbol: string }) => (
    <div data-testid="market">{symbol}</div>
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
  it('offers all six markets through the same mobile selection path', async () => {
    render(<ChartTabs traderId="test" exchangeId="ninjatrader" marketOnly />)
    const select = screen.getByRole('combobox', { name: 'Market Chart' })
    expect(screen.getAllByRole('option')).toHaveLength(6)
    fireEvent.change(select, { target: { value: 'crypto' } })
    expect(await screen.findByText('BTCUSDT')).toBeInTheDocument()
    fireEvent.change(select, { target: { value: 'ninjatrader' } })
    expect(await screen.findByText('MNQ')).toBeInTheDocument()
  })
})
