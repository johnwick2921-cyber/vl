import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ request: vi.fn() }))
vi.mock('../httpClient', () => ({
  httpClient: { request: mocks.request },
}))

import {
  effectiveByPath,
  strategyEffectiveApi,
  studioEffective,
} from './strategyEffective'

describe('strategyEffectiveApi.getStrategyEffective', () => {
  beforeEach(() => {
    // A block body: returning the mock (a function) would register it as a
    // teardown that vitest CALLS after each test.
    mocks.request.mockReset()
  })

  it('asks the effective route for the strategy and the named session, silently', async () => {
    mocks.request.mockResolvedValue({
      success: true,
      data: {
        strategy_id: 's/1',
        session: 'NY',
        venue: null,
        settings: [],
        coverage: {},
      },
    })
    const out = await strategyEffectiveApi.getStrategyEffective('s/1', 'NY')
    expect(mocks.request).toHaveBeenCalledWith(
      '/api/strategies/s%2F1/effective?session=NY',
      {
        silent: true,
      }
    )
    expect(out?.session).toBe('NY')
  })

  it('resolves null on a refusal — never a fabricated payload', async () => {
    mocks.request.mockResolvedValue({ success: false, statusCode: 404 })
    expect(await strategyEffectiveApi.getStrategyEffective('x')).toBeNull()
  })

  it('resolves null when the request throws', async () => {
    mocks.request.mockImplementation(() => Promise.reject(new Error('network')))
    expect(await strategyEffectiveApi.getStrategyEffective('x')).toBeNull()
  })
})

describe('effectiveByPath', () => {
  it('indexes rows by schema path; no payload is an empty map', () => {
    expect(effectiveByPath(null)).toEqual({})
    const row = {
      path: 'day_plan.plan_mode',
      status: 'live',
      ui_label: 'live',
      stored: { present: false },
      effective: 'advisory',
      origin: 'shipped default',
      scope: 'strategy',
      resolver: 'store.ResolvePlanMode',
      resolved: true,
    }
    const m = effectiveByPath({
      strategy_id: 's',
      session: null,
      venue: null,
      settings: [row],
      coverage: { resolved: 1, total: 1, unresolved: [], not_enumerated: [] },
    })
    expect(m['day_plan.plan_mode']).toBe(row)
  })
})

describe('studioEffective mentorPlace', () => {
  const reply = (session: string | null, mentor_place?: boolean) => ({
    strategy_id: 's',
    session,
    venue: null,
    settings: [],
    coverage: { resolved: 0, total: 0, unresolved: [], not_enumerated: [] },
    ...(mentor_place === undefined ? {} : { mentor_place }),
  })

  it('carries the MENTOR_PLACE gate of the strategy-level reply, true and false', () => {
    expect(studioEffective(reply(null, true), {}).mentorPlace).toBe(true)
    expect(studioEffective(reply(null, false), {}).mentorPlace).toBe(false)
  })

  it('leaves it absent (unknown) when the server did not send it or the reply is session-scoped', () => {
    expect('mentorPlace' in studioEffective(reply(null), {})).toBe(false)
    expect('mentorPlace' in studioEffective(reply('NY', true), {})).toBe(false)
    expect('mentorPlace' in studioEffective(null, {})).toBe(false)
  })
})
