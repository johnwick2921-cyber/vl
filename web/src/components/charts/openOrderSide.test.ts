import { describe, it, expect } from 'vitest'
import { openOrderSideLabel } from './openOrderSide'

describe('T6 — a SHORT working order is drawn as SELL', () => {
  it('canonicalizes every casing', () => {
    expect(openOrderSideLabel({ side: 'SELL', position_side: 'SHORT' })).toBe(
      'SELL'
    )
    expect(openOrderSideLabel({ side: 'sell', position_side: 'short' })).toBe(
      'SELL'
    )
    expect(openOrderSideLabel({ side: '', position_side: 'Short' })).toBe(
      'SELL'
    )
    expect(openOrderSideLabel({ side: 'buy', position_side: 'LONG' })).toBe(
      'BUY'
    )
    expect(openOrderSideLabel({})).toBe('?')
  })
})
