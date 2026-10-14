// T6 (UI-GUIDE-TRUTH, class 28) — one canonical side for a working-order line.
//
// The server served every SHORT arm as BUY (a lowercase compare against UPPER
// ledger rows); the chart then titled it "Limit BUY". The server is fixed; the
// chart also canonicalizes, so a casing variant ("sell", "Short") can never
// flip a line again: SELL wins when either field says sell/short.
export function openOrderSideLabel(order: {
  side?: string
  position_side?: string
}): 'BUY' | 'SELL' | '?' {
  const side = (order.side ?? '').trim().toUpperCase()
  const pos = (order.position_side ?? '').trim().toUpperCase()
  if (side === 'SELL' || (side === '' && pos === 'SHORT')) return 'SELL'
  if (side === 'BUY' || (side === '' && pos === 'LONG')) return 'BUY'
  return '?'
}
