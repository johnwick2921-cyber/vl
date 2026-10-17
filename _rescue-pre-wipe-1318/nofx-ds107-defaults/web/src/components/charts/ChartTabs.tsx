import { useState, useEffect, useRef } from 'react'
import { EquityChart } from './EquityChart'
import { AdvancedChart } from './AdvancedChart'
import { useLanguage } from '../../contexts/LanguageContext'
import { t } from '../../i18n/translations'
import { chartTabs, ts } from '../../i18n/strategy-translations'
import { BarChart3, CandlestickChart, ChevronDown, Search } from 'lucide-react'
import { motion, AnimatePresence } from 'framer-motion'

interface ChartTabsProps {
  /** Show only the market when the caller renders account equity beside it. */
  marketOnly?: boolean
  traderId: string
  selectedSymbol?: string // Externally selected symbol
  updateKey?: number // Force update key
  exchangeId?: string // Exchange ID
  isFutures?: boolean // NT futures: USD instead of USDT (Plan 4.3.1)
}

type ChartTab = 'equity' | 'kline'
type Interval =
  | '1m'
  | '3m'
  | '5m'
  | '15m'
  | '30m'
  | '1h'
  | '2h'
  | '4h'
  | '6h'
  | '8h'
  | '12h'
  | '1d'
  | '3d'
  | '1w'
type MarketType =
  | 'hyperliquid'
  | 'crypto'
  | 'stocks'
  | 'forex'
  | 'metals'
  | 'ninjatrader'

interface SymbolInfo {
  symbol: string
  name: string
  category: string
}

// Market type configuration
const MARKET_CONFIG = {
  hyperliquid: {
    exchange: 'hyperliquid',
    defaultSymbol: 'BTC',
    icon: '🔷',
    labelKey: 'hyperliquid' as const,
    color: 'cyan',
    hasDropdown: true,
  },
  crypto: {
    exchange: 'binance',
    defaultSymbol: 'BTCUSDT',
    icon: '₿',
    labelKey: 'crypto' as const,
    color: 'yellow',
    hasDropdown: false,
  },
  stocks: {
    exchange: 'alpaca',
    defaultSymbol: 'AAPL',
    icon: '📈',
    labelKey: 'stocks' as const,
    color: 'green',
    hasDropdown: false,
  },
  forex: {
    exchange: 'forex',
    defaultSymbol: 'EUR/USD',
    icon: '💱',
    labelKey: 'forex' as const,
    color: 'blue',
    hasDropdown: false,
  },
  metals: {
    exchange: 'metals',
    defaultSymbol: 'XAU/USD',
    icon: '🥇',
    labelKey: 'metals' as const,
    color: 'amber',
    hasDropdown: false,
  },
  ninjatrader: {
    exchange: 'ninjatrader',
    defaultSymbol: 'MNQ',
    icon: '🥷',
    labelKey: 'ninjatrader' as const,
    color: 'red',
    hasDropdown: false,
  },
}

const INTERVALS: { value: Interval; label: string }[] = [
  { value: '1m', label: '1m' },
  { value: '5m', label: '5m' },
  { value: '15m', label: '15m' },
  { value: '30m', label: '30m' },
  { value: '1h', label: '1h' },
  { value: '4h', label: '4h' },
  { value: '1d', label: '1d' },
]

// NinjaTrader (CME futures) chart timeframes — the 14-set the NT8 AddOn
// auto-subscribes (provider/ninjatrader/tcp_server.go defaultAutoBarsTimeframes).
// Every button maps to a subscribed BarCache series. Crypto INTERVALS untouched.
const NINJATRADER_INTERVALS: { value: Interval; label: string }[] = [
  { value: '1m', label: '1m' },
  { value: '3m', label: '3m' },
  { value: '5m', label: '5m' },
  { value: '15m', label: '15m' },
  { value: '30m', label: '30m' },
  { value: '1h', label: '1h' },
  { value: '2h', label: '2h' },
  { value: '4h', label: '4h' },
  { value: '6h', label: '6h' },
  { value: '8h', label: '8h' },
  { value: '12h', label: '12h' },
  { value: '1d', label: '1d' },
  { value: '3d', label: '3d' },
  { value: '1w', label: '1w' },
]

// Infer market type from exchange ID
function getMarketTypeFromExchange(exchangeId: string | undefined): MarketType {
  if (!exchangeId) return 'hyperliquid'
  const lower = exchangeId.toLowerCase()
  if (lower.includes('hyperliquid')) return 'hyperliquid'
  if (lower.includes('ninjatrader')) return 'ninjatrader'
  // Other exchanges default to crypto type
  return 'crypto'
}

export function ChartTabs({
  marketOnly = false,
  traderId,
  selectedSymbol,
  updateKey,
  exchangeId,
  isFutures = false,
}: ChartTabsProps) {
  const { language } = useLanguage()
  const [activeTab, setActiveTab] = useState<ChartTab>('equity')
  const visibleTab = marketOnly ? 'kline' : activeTab
  const [chartSymbol, setChartSymbol] = useState<string>(
    () => MARKET_CONFIG[getMarketTypeFromExchange(exchangeId)].defaultSymbol
  )
  const [interval, setInterval] = useState<Interval>('5m')
  const [symbolInput, setSymbolInput] = useState('')
  const [marketType, setMarketType] = useState<MarketType>(() =>
    getMarketTypeFromExchange(exchangeId)
  )
  const [availableSymbols, setAvailableSymbols] = useState<SymbolInfo[]>([])
  const [showDropdown, setShowDropdown] = useState(false)
  const [searchFilter, setSearchFilter] = useState('')
  const dropdownRef = useRef<HTMLDivElement>(null)

  // Auto-switch market type when exchange ID changes
  useEffect(() => {
    const newMarketType = getMarketTypeFromExchange(exchangeId)
    setMarketType(newMarketType)
    // Reset the chart symbol to the new market's default (e.g. MNQ for
    // ninjatrader) so the chart doesn't keep requesting the previous market's
    // symbol (default 'BTC') on the wrong exchange, which returns empty klines.
    // An externally-selected symbol still wins via the selectedSymbol effect.
    if (!selectedSymbol) {
      setChartSymbol(MARKET_CONFIG[newMarketType].defaultSymbol)
    }
  }, [exchangeId, selectedSymbol])

  // Determine exchange from market type
  const marketConfig = MARKET_CONFIG[marketType]
  // Prefer passed-in exchangeId (when not hyperliquid)
  const currentExchange =
    marketType === 'hyperliquid'
      ? 'hyperliquid'
      : exchangeId || marketConfig.exchange

  // Fetch available symbol list
  useEffect(() => {
    if (marketConfig.hasDropdown) {
      fetch(`/api/symbols?exchange=${marketConfig.exchange}`)
        .then((res) => res.json())
        .then((data) => {
          if (data.symbols) {
            // Sort by category: crypto > stock > forex > commodity > index
            const categoryOrder: Record<string, number> = {
              crypto: 0,
              stock: 1,
              forex: 2,
              commodity: 3,
              index: 4,
            }
            const sorted = [...data.symbols].sort(
              (a: SymbolInfo, b: SymbolInfo) => {
                const orderA = categoryOrder[a.category] ?? 5
                const orderB = categoryOrder[b.category] ?? 5
                if (orderA !== orderB) return orderA - orderB
                return a.symbol.localeCompare(b.symbol)
              }
            )
            setAvailableSymbols(sorted)
          }
        })
        .catch((err) => console.error('Failed to fetch symbols:', err))
    }
  }, [marketType, marketConfig.exchange, marketConfig.hasDropdown])

  // Close dropdown on outside click
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (
        dropdownRef.current &&
        !dropdownRef.current.contains(event.target as Node)
      ) {
        setShowDropdown(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  // Update default symbol when switching market type
  const handleMarketTypeChange = (type: MarketType) => {
    setMarketType(type)
    setChartSymbol(MARKET_CONFIG[type].defaultSymbol)
    setShowDropdown(false)
  }

  // Filtered symbol list
  const filteredSymbols = availableSymbols.filter((s) =>
    s.symbol.toLowerCase().includes(searchFilter.toLowerCase())
  )

  // Auto-switch to kline chart when symbol selected externally
  useEffect(() => {
    if (selectedSymbol) {
      setChartSymbol(selectedSymbol)
      setActiveTab('kline')
    }
  }, [selectedSymbol, updateKey])

  // Handle manual symbol input
  const handleSymbolSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (symbolInput.trim()) {
      let symbol = symbolInput.trim().toUpperCase()
      // Auto-append USDT suffix for crypto
      if (marketType === 'crypto' && !symbol.endsWith('USDT')) {
        symbol = symbol + 'USDT'
      }
      setChartSymbol(symbol)
      setSymbolInput('')
    }
  }

  return (
    <div
      className={`nofx-glass rounded-lg border border-white/5 relative z-10 w-full flex flex-col transition-all duration-300 ${
        typeof window !== 'undefined' && window.innerWidth < 768
          ? 'h-[500px]'
          : 'h-[600px]'
      }`}
    >
      {/* 
        Premium Professional Toolbar 
        Mobile: Single row, horizontal scroll with gradient mask
        Desktop: Standard flex-wrap/nowrap
      */}
      <div
        className="relative z-20 flex flex-wrap items-center justify-between gap-y-2 px-3 py-2 shrink-0 backdrop-blur-md bg-[#0B0E11]/80 rounded-t-lg"
        style={{ borderBottom: '1px solid rgba(255, 255, 255, 0.05)' }}
      >
        {/* Left: Tab Switcher */}
        <div className="flex flex-wrap items-center gap-1">
          {!marketOnly && (
            <>
              <button
                onClick={() => setActiveTab('equity')}
                className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md text-[11px] font-medium transition-all ${
                  visibleTab === 'equity'
                    ? 'bg-nofx-gold/10 text-nofx-gold border border-nofx-gold/20 shadow-[0_0_10px_rgba(240,185,11,0.1)]'
                    : 'text-nofx-text-muted hover:text-nofx-text-main hover:bg-white/5'
                }`}
              >
                <BarChart3 className="w-3.5 h-3.5" />
                <span className="hidden md:inline">
                  {t('accountEquityCurve', language)}
                </span>
                <span className="md:hidden">Eq</span>
              </button>

              <button
                onClick={() => setActiveTab('kline')}
                className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md text-[11px] font-medium transition-all ${
                  visibleTab === 'kline'
                    ? 'bg-nofx-gold/10 text-nofx-gold border border-nofx-gold/20 shadow-[0_0_10px_rgba(240,185,11,0.1)]'
                    : 'text-nofx-text-muted hover:text-nofx-text-main hover:bg-white/5'
                }`}
              >
                <CandlestickChart className="w-3.5 h-3.5" />
                <span className="hidden md:inline">
                  {t('marketChart', language)}
                </span>
                <span className="md:hidden">Kline</span>
              </button>
            </>
          )}
          {marketOnly && (
            <strong className="text-[13px] text-nofx-text-main whitespace-nowrap">
              {t('marketChart', language)}
            </strong>
          )}
          {visibleTab === 'kline' && (
            <label className="md:hidden flex items-center gap-2 text-xs text-nofx-text-muted">
              {t('marketChart', language)}
              <select
                value={marketType}
                onChange={(e) =>
                  handleMarketTypeChange(e.target.value as MarketType)
                }
                className="max-w-[160px] rounded border border-white/20 bg-[#172330] px-2 py-1 text-white"
              >
                {(Object.keys(MARKET_CONFIG) as MarketType[]).map((type) => (
                  <option key={type} value={type}>
                    {ts(chartTabs[MARKET_CONFIG[type].labelKey], language)}
                  </option>
                ))}
              </select>
            </label>
          )}
          {/* Desktop pills and mobile select use the same market handler. */}
          {visibleTab === 'kline' && (
            <div className="hidden md:flex items-center gap-1 ml-2 border-l border-white/10 pl-2">
              {(Object.keys(MARKET_CONFIG) as MarketType[]).map((type) => {
                const config = MARKET_CONFIG[type]
                const isActive = marketType === type
                return (
                  <button
                    key={type}
                    onClick={() => handleMarketTypeChange(type)}
                    className={`px-2.5 py-1 text-[10px] font-medium rounded transition-all border ${
                      isActive
                        ? 'bg-white/10 text-white border-white/20'
                        : 'text-nofx-text-muted border-transparent hover:text-nofx-text-main hover:bg-white/5'
                    }`}
                  >
                    <span className="mr-1 opacity-70">{config.icon}</span>
                    {ts(chartTabs[config.labelKey], language)}
                  </button>
                )
              })}
            </div>
          )}
        </div>

        {/* Right: Symbol + Interval */}
        {visibleTab === 'kline' && (
          <div className="flex flex-wrap items-center gap-2 md:gap-3 w-full md:w-auto min-w-0">
            {/* Symbol Dropdown */}
            <div className="shrink-0 relative" ref={dropdownRef}>
              {marketConfig.hasDropdown ? (
                <>
                  <button
                    onClick={() => setShowDropdown(!showDropdown)}
                    className="flex items-center gap-1.5 px-2.5 py-1 bg-black/40 border border-white/10 rounded text-[11px] font-bold text-nofx-text-main hover:border-nofx-gold/30 hover:text-nofx-gold transition-all"
                  >
                    <span>{chartSymbol}</span>
                    <ChevronDown
                      className={`w-3 h-3 text-nofx-text-muted transition-transform ${showDropdown ? 'rotate-180' : ''}`}
                    />
                  </button>
                  {showDropdown && (
                    <div className="absolute top-full right-0 mt-2 w-64 bg-[#0B0E11] border border-white/10 rounded-lg shadow-[0_10px_40px_-10px_rgba(0,0,0,0.5)] z-50 overflow-hidden nofx-glass ring-1 ring-white/5">
                      <div className="p-2 border-b border-white/5">
                        <div className="flex items-center gap-2 px-2 py-1.5 bg-black/40 rounded border border-white/10 focus-within:border-nofx-gold/50 transition-colors">
                          <Search className="w-3.5 h-3.5 text-nofx-text-muted" />
                          <input
                            type="text"
                            value={searchFilter}
                            onChange={(e) => setSearchFilter(e.target.value)}
                            placeholder="Search symbol..."
                            className="flex-1 bg-transparent text-[11px] text-white placeholder-gray-600 focus:outline-none font-mono"
                            autoFocus
                          />
                        </div>
                      </div>
                      <div className="overflow-y-auto max-h-60 custom-scrollbar">
                        {['crypto', 'stock', 'forex', 'commodity', 'index'].map(
                          (category) => {
                            const categorySymbols = filteredSymbols.filter(
                              (s) => s.category === category
                            )
                            if (categorySymbols.length === 0) return null
                            const labels: Record<string, string> = {
                              crypto: 'Crypto',
                              stock: 'Stocks',
                              forex: 'Forex',
                              commodity: 'Commodities',
                              index: 'Index',
                            }
                            return (
                              <div key={category}>
                                <div className="px-3 py-1.5 text-[9px] font-bold text-nofx-text-muted/60 bg-white/5 uppercase tracking-wider">
                                  {labels[category]}
                                </div>
                                {categorySymbols.map((s) => (
                                  <button
                                    key={s.symbol}
                                    onClick={() => {
                                      setChartSymbol(s.symbol)
                                      setShowDropdown(false)
                                      setSearchFilter('')
                                    }}
                                    className={`w-full px-3 py-2 text-left text-[11px] font-mono hover:bg-white/5 transition-all flex items-center justify-between ${chartSymbol === s.symbol ? 'bg-nofx-gold/10 text-nofx-gold' : 'text-nofx-text-muted'}`}
                                  >
                                    <span>{s.symbol}</span>
                                    <span className="text-[9px] opacity-40">
                                      {s.name}
                                    </span>
                                  </button>
                                ))}
                              </div>
                            )
                          }
                        )}
                      </div>
                    </div>
                  )}
                </>
              ) : (
                <span className="px-2.5 py-1 bg-black/40 border border-white/10 rounded text-[11px] font-bold text-nofx-text-main font-mono">
                  {chartSymbol}
                </span>
              )}
            </div>

            {/* Interval Selector — wrap so ALL timeframe buttons stay visible
                (the old overflow-x-auto + flex-shrink clipped the row to ~2-4
                buttons behind a hidden scrollbar; NT8 needs all 7 reachable). */}
            <div className="flex flex-wrap items-center bg-black/40 rounded border border-white/10">
              {(marketType === 'ninjatrader'
                ? NINJATRADER_INTERVALS
                : INTERVALS
              ).map((int) => (
                <button
                  key={int.value}
                  onClick={() => setInterval(int.value)}
                  className={`px-2 py-1 text-[10px] font-medium transition-all ${
                    interval === int.value
                      ? 'bg-nofx-gold/20 text-nofx-gold'
                      : 'text-nofx-text-muted hover:text-white hover:bg-white/5'
                  }`}
                >
                  {int.label}
                </button>
              ))}
            </div>

            {/* Quick Input - Hidden on mobile, dropdown search is enough */}
            <form
              onSubmit={handleSymbolSubmit}
              className="hidden md:flex items-center shrink-0"
            >
              <input
                type="text"
                value={symbolInput}
                onChange={(e) => setSymbolInput(e.target.value)}
                placeholder="Sym"
                className="w-16 px-2 py-1 bg-black/40 border border-white/10 rounded-l text-[10px] text-white placeholder-gray-600 focus:outline-none focus:border-nofx-gold/50 font-mono transition-colors"
              />
              <button
                type="submit"
                className="px-2 py-1 bg-white/5 border border-white/10 border-l-0 rounded-r text-[10px] text-nofx-text-muted hover:text-white hover:bg-white/10 transition-all"
              >
                Go
              </button>
            </form>
          </div>
        )}
      </div>

      {/* Tab Content - Chart autosizes to this container */}
      <div className="relative flex-1 bg-[#0B0E11]/50 rounded-b-lg overflow-hidden h-full min-h-0">
        <AnimatePresence mode="wait">
          {visibleTab === 'equity' ? (
            <motion.div
              key="equity"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.2 }}
              className="h-full w-full absolute inset-0"
            >
              <EquityChart traderId={traderId} embedded isFutures={isFutures} />
            </motion.div>
          ) : (
            <motion.div
              key={`kline-${chartSymbol}-${interval}-${currentExchange}`}
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.2 }}
              className="h-full w-full absolute inset-0"
            >
              <AdvancedChart
                symbol={chartSymbol}
                interval={interval}
                traderID={traderId}
                // Dynamic auto-sizing via ResizeObserver
                exchange={currentExchange}
                onSymbolChange={setChartSymbol}
              />
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </div>
  )
}
