package market

// FuturesBarsProvider, when non-nil, supplies OHLCV klines for CME futures
// symbols from the live NT8 bar feed (provider/ninjatrader BarCache),
// bypassing the legacy crypto market path entirely. It is wired at startup by the
// ninjatrader transport layer (trader/ninjatrader) so the market package
// itself takes no provider/trader dependency — keeping the dependency edge
// one-directional (trader/ninjatrader -> market) and cycle-free.
//
// Contract: returns up to the most recent `count` bars for (symbol,
// timeframe) in ascending time order, or nil if none are cached yet. A nil
// provider (crypto-only build, or NT transport not started) means the
// futures branch in GetWithTimeframes simply skips the symbol.
var FuturesBarsProvider func(symbol, timeframe string, count int) []Kline

// FuturesFeedMode, when non-nil, returns the broker market-data mode reported
// by the live NT8 feed_status frame: "realtime" | "delayed" | "" ("" = UNKNOWN
// — absent, unsupported, or not yet received). Wired by trader/ninjatrader the
// same way as FuturesBarsProvider; the market package takes no provider
// dependency. Consumers must treat "" as UNKNOWN, never as realtime.
var FuturesFeedMode func() string
