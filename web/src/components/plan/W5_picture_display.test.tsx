// W-EXEC-TRUTH W5 (display) — Picture HTF as a Day Plan scenario source, as
// the owner sees it. Every assertion renders the REAL component the card
// mounts (ScenarioList, PictureGateChip, SessionPlanCard, PictureHtfPanel,
// DayPlanEditor), fed the JSON shapes the server serves.
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { ScenarioList } from './ScenarioList'
import { PictureGateChip } from './PictureGateChip'
import { SessionPlanCard } from './SessionPlanCard'
import { PictureHtfPanel } from '../trader/PictureHtfPanel'
import { httpClient } from '../../lib/httpClient'
import { DayPlanEditor } from '../strategy/DayPlanEditor'
import type { PlanOrderLeg, PlanScenario, PlanToday } from '../../lib/api/plan'

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

const UNTIL = 1790190010000 // 14:00:10 CT
const EVIDENCE = {
  opp_key: 't1|acct|MNQ|long|resistance|1790186400000|1790190000000',
  direction: 'long',
  rule: 'h1_close_break',
  rule_ver: 1,
  level_role: 'resistance',
  body_top: 31010,
  body_bot: 31000,
  h1_new_close: 31020.25,
  h1_boundary: 31010,
  entry_ref: 31004,
  stop: 30990,
  target: 31060,
  stop_source: '5m swing low @13:55',
  rr_estimate: 2.63,
  rr_floor: 2.5,
  window_open_ms: 1790190000000,
  window_close_ms: UNTIL,
}

function pictureScenario(evidence?: unknown): PlanScenario {
  return {
    id: 'P1',
    trigger: 'Picture HTF: H1 closed above the 4H body 31000–31010',
    condition: 'acceptance',
    direction: 'long',
    target_chain: [31060],
    invalid: 'n/a',
    quality: 'B',
    source: 'picture',
    machine: {
      rule: 'h1_close_break',
      rule_ver: 1,
      ref: EVIDENCE.opp_key,
      eligible_from_ms: 1790190000000,
      eligible_until_ms: UNTIL,
      ...(evidence === undefined ? {} : { evidence }),
    },
  }
}

const plannerScenario: PlanScenario = {
  id: 'S1',
  trigger: 'reclaim 31000',
  condition: 'reclaim',
  direction: 'long',
  target_chain: [31050],
  invalid: '30990',
  quality: 'A',
}

function badgeTitle(scenario: PlanScenario, evaluated = true): string | null {
  const { unmount } = render(
    <ScenarioList
      scenarios={[scenario]}
      statusMap={evaluated ? { [scenario.id]: 'armed' } : undefined}
      language="en"
    />
  )
  const el = screen.queryByTestId(`picture-source-badge-${scenario.id}`)
  const title = el ? el.getAttribute('title') : null
  if (el) expect(el.textContent).toBe('📷 PICTURE')
  unmount()
  return title
}

describe('ScenarioList — the 📷 PICTURE badge', () => {
  it('a picture scenario carries the badge; the tooltip reads the machine record and evidence', () => {
    const title = badgeTitle(pictureScenario(EVIDENCE))
    expect(title).not.toBeNull()
    expect(title).toContain('rule h1_close_break (version v1)')
    expect(title).toContain(
      'H1 close 31,020.25 vs the 4H body 31,000–31,010 (resistance)'
    )
    expect(title).toContain('window until 14:00:10 CT')
    expect(title).toContain('stop source 5m swing low @13:55')
    expect(title).toContain('R:R floor 2.5')
  })

  it('an unevaluable picture scenario still carries the badge', () => {
    expect(badgeTitle(pictureScenario(EVIDENCE), false)).toContain(
      'window until 14:00:10 CT'
    )
  })

  it('every missing number reads n/a, never 0', () => {
    const title = badgeTitle(
      pictureScenario({
        ...EVIDENCE,
        h1_new_close: undefined,
        body_bot: 0,
        rr_floor: undefined,
        stop_source: '',
      })
    )
    expect(title).toContain('H1 close n/a vs the 4H body n/a–31,010')
    expect(title).toContain('R:R floor n/a')
    expect(title).toContain('stop source n/a')
    expect(title).not.toContain('body 0')
    const noWindow = pictureScenario(EVIDENCE)
    noWindow.machine = {
      ...noWindow.machine!,
      eligible_until_ms: undefined as unknown as number,
    }
    expect(badgeTitle(noWindow)).toContain('window until n/a')
  })

  it('evidence that fails to parse reads "evidence unavailable"', () => {
    const title = badgeTitle(pictureScenario('{not json'))
    expect(title).toContain('evidence unavailable')
    expect(title).not.toContain('H1 close')
    expect(badgeTitle(pictureScenario(42))).toContain('evidence unavailable')
  })

  it('evidence stored as a JSON string is read', () => {
    expect(badgeTitle(pictureScenario(JSON.stringify(EVIDENCE)))).toContain(
      'H1 close 31,020.25'
    )
  })

  it('no evidence at all reads "evidence not recorded" (absent is not unreadable)', () => {
    const title = badgeTitle(pictureScenario(undefined))
    expect(title).toContain('evidence not recorded')
    expect(title).not.toContain('evidence unavailable')
  })

  it('a planner scenario carries no badge', () => {
    expect(badgeTitle(plannerScenario)).toBeNull()
  })
})

const policyLeg: PlanOrderLeg = {
  state: 'armed',
  leg_index: 0,
  row_id: 41,
  placement_seq: 0,
  version: 2,
  armed_under_version: 2,
  side: 'long',
  intended: { entry: null, stop: null, target: null, source: 'plan v2' },
  composed: {
    entry: 31012,
    stop: 30990,
    target: 31060,
    source: 'armed_orders row 41',
  },
  accepted: { entry: null, stop: null, target: null, source: 'book' },
  book_age_ms: 0,
  build_id: 'h1',
  policy: 'market_in_zone',
  planned_entry: 31004,
  zone_lo: 31004,
  zone_hi: 31012,
}
const sourceLeg: PlanOrderLeg = {
  ...policyLeg,
  source: 'picture',
  source_ref: EVIDENCE.opp_key,
  rule: 'h1_close_break',
  method: 'market_in_zone limit',
  eligible_until_ms: UNTIL,
}

function entryLine(
  scenario: PlanScenario,
  leg: PlanOrderLeg
): { text: string | null; title: string | null } {
  const { unmount } = render(
    <ScenarioList
      scenarios={[scenario]}
      statusMap={{ [scenario.id]: 'armed' }}
      armedStates={{ [scenario.id]: { state: leg.state, legs: [leg] } }}
      language="en"
    />
  )
  const el = screen.queryByTestId('entry-policy-line-0')
  const out = {
    text: el ? el.textContent : null,
    title: el ? el.getAttribute('title') : null,
  }
  unmount()
  return out
}

const HEAD = 'Entry: around 31,004 (zone 31,004–31,012) · '

describe('EntryPolicyLine — a Picture row names its source', () => {
  it('while armed: source, rule and when the window closes', () => {
    const { text, title } = entryLine(pictureScenario(EVIDENCE), sourceLeg)
    expect(text).toBe(
      `${HEAD}Waiting for price · source picture (h1_close_break) · window closes 14:00:10 CT`
    )
    expect(title).toContain('method market_in_zone limit')
    expect(title).toContain(`opportunity ${EVIDENCE.opp_key}`)
  })

  it('once filled: the source stays, the window does not', () => {
    const { text } = entryLine(pictureScenario(EVIDENCE), {
      ...sourceLeg,
      state: 'filled',
      fill_price: 31010,
    })
    expect(text).toBe(`${HEAD}Filled 31,010 · source picture (h1_close_break)`)
  })

  it('a missing rule or deadline reads n/a', () => {
    const { text } = entryLine(pictureScenario(EVIDENCE), {
      ...sourceLeg,
      rule: undefined,
      eligible_until_ms: undefined,
    })
    expect(text).toBe(
      `${HEAD}Waiting for price · source picture (n/a) · window closes n/a`
    )
  })

  it('a planner row is unchanged', () => {
    const { text, title } = entryLine(plannerScenario, policyLeg)
    expect(text).toBe(`${HEAD}Waiting for price`)
    expect(title).not.toContain('method')
  })
})

describe('PictureGateChip — route vs refusal', () => {
  const ROUTE = 'Day Plan scenario (market_in_zone limit)'
  const REFUSAL = 'Day Plan says no (plan not active)'

  it('a route renders the NEUTRAL chip, and no red chip', () => {
    render(<PictureGateChip picture={{ enabled: true, route: ROUTE }} />)
    const chip = screen.getByTestId('picture-route-chip')
    expect(chip.textContent).toBe(`📷 PICTURE → ${ROUTE}`)
    expect(chip.getAttribute('style')).not.toContain('var(--vl-short)')
    expect(screen.queryByTestId('picture-gate-chip')).toBeNull()
  })

  it('a refusal renders the red chip, and no route chip', () => {
    render(
      <PictureGateChip
        picture={{ enabled: true, route: '', refusal: REFUSAL }}
      />
    )
    const chip = screen.getByTestId('picture-gate-chip')
    expect(chip.textContent).toContain(REFUSAL)
    expect(chip.getAttribute('style')).toContain('var(--vl-short)')
    expect(screen.queryByTestId('picture-route-chip')).toBeNull()
  })

  it('a route that is also refused shows both, the refusal red', () => {
    render(
      <PictureGateChip
        picture={{ enabled: true, route: ROUTE, refusal: REFUSAL }}
      />
    )
    expect(screen.getByTestId('picture-gate-chip').textContent).toContain(
      REFUSAL
    )
    expect(screen.getByTestId('picture-route-chip').textContent).toContain(
      ROUTE
    )
  })

  it('neither when Picture is disabled', () => {
    const { container } = render(
      <PictureGateChip
        picture={{ enabled: false, route: ROUTE, refusal: REFUSAL }}
      />
    )
    expect(container.textContent).toBe('')
  })

  it('neither when both are empty', () => {
    const { container } = render(
      <PictureGateChip picture={{ enabled: true, route: '', refusal: '' }} />
    )
    expect(container.textContent).toBe('')
  })
})

describe('SessionPlanCard — machine plan banner and composed-of line', () => {
  const found = (extra: Partial<PlanToday>): PlanToday => ({
    trade_date: '2026-09-23',
    session: 'NY',
    night: false,
    mode: 'direction',
    is_active: true,
    found: true,
    version: 1,
    lifecycle: 'active',
    model_id: 'machine',
    replans_left: 2,
    weekly: null,
    doc: {
      reasoning: 'MACHINE-AUTHORED plan (Picture HTF)',
      bias: { direction: 'neutral', conviction: 'low', flip_condition: 'n/a' },
      levels: [],
      scenarios: [pictureScenario(EVIDENCE)],
      no_trade: [],
      death_condition: 'n/a',
      day_type: 'range',
    },
    level_facts: [],
    ...extra,
  })
  const mount = (plan: PlanToday) =>
    render(
      <SessionPlanCard
        plan={plan}
        traderId="t1"
        symbol="MNQ"
        exchange="ninjatrader"
        language="en"
      />
    )

  it('a machine plan shows the banner', () => {
    mount(found({ machine_plan: true }))
    expect(screen.getByTestId('machine-plan-banner').textContent).toContain(
      'MACHINE-AUTHORED plan — Picture HTF; the first AI plan supersedes it'
    )
  })

  it('an AI plan shows no banner', () => {
    mount(found({ machine_plan: false }))
    expect(screen.queryByTestId('machine-plan-banner')).toBeNull()
  })

  it('composed_of renders the fold record', () => {
    mount(
      found({
        composed_of: {
          user_overlays: [1, 3],
          machine: [
            { overlay_version: 4, scenario_id: 'P1', ref: EVIDENCE.opp_key },
          ],
        },
      })
    )
    expect(screen.getByTestId('plan-composed-of').textContent).toBe(
      'composed of: base + overlays o1,o3 · machine P1 (o4)'
    )
  })

  it('machine only, and a missing overlay number reads n/a', () => {
    mount(
      found({
        composed_of: {
          user_overlays: [],
          machine: [
            {
              overlay_version: undefined as unknown as number,
              scenario_id: 'P1',
              ref: EVIDENCE.opp_key,
            },
          ],
        },
      })
    )
    expect(screen.getByTestId('plan-composed-of').textContent).toBe(
      'composed of: base · machine P1 (n/a)'
    )
  })

  it('absent composed_of renders nothing', () => {
    mount(found({}))
    expect(screen.queryByTestId('plan-composed-of')).toBeNull()
    expect(screen.queryByTestId('machine-plan-banner')).toBeNull()
  })

  it('the card mounts the picture scenario badge', () => {
    mount(found({ scenario_status: { P1: 'armed' } }))
    expect(screen.getByTestId('picture-source-badge-P1')).toBeTruthy()
  })
})

describe('PictureHtfPanel — a planned opportunity links to its Day Plan scenario', () => {
  const ROW = {
    opp_key: EVIDENCE.opp_key,
    stage: 'planned',
    stage_reason: 'Day Plan scenario P1',
    symbol: 'MNQ',
    direction: 'long',
    level_role: 'resistance',
    level_body_top: 31010,
    h1_close_time_ms: 1790190000000,
    window_close_ms: UNTIL,
    entry_ref: 31004,
    stop_px: 30990,
    target_px: 31060,
    rr_estimate: 2.63,
    rr_configured: 2.5,
    momentum_stall: false,
    signal_id: 'picture-htf-1790190000000',
    submitted_at_ms: 0,
    broker_order_id: '',
    broker_status: '',
    fill_price: 0,
    fill_qty: 0,
    fill_rr: 0,
    reject_reason: '',
    created_at_ms: 1790190000000,
  }
  const serve = (body: unknown) =>
    vi.spyOn(httpClient, 'request').mockResolvedValue({
      success: true,
      data: body,
    })

  it('renders "→ Day Plan P1 · <plan_id> v<n>" from plan_link', async () => {
    serve({
      rows: [
        {
          ...ROW,
          plan_link: {
            plan_id: 't1:2026-09-23:NY',
            scenario_id: 'P1',
            plan_version: 3,
            arm_row_id: 52,
            arm_state: 'working',
            arm_signal_id: 'arm-sig-52',
          },
        },
      ],
      count: 1,
    })
    render(<PictureHtfPanel traderId="t1" />)
    const link = await screen.findByTestId(`picture-plan-link-${ROW.opp_key}`)
    expect(link.textContent).toBe(
      '→ Day Plan P1 · t1:2026-09-23:NY v3 · arm #52 working'
    )
  })

  it('no plan_link → no link line', async () => {
    serve({ rows: [ROW], count: 1 })
    render(<PictureHtfPanel traderId="t1" />)
    await waitFor(() => expect(screen.getByText('planned')).toBeTruthy())
    expect(screen.queryByTestId(`picture-plan-link-${ROW.opp_key}`)).toBeNull()
    expect(screen.queryByTestId('picture-plan-links-unread')).toBeNull()
  })

  it('an unread link ledger is said, not shown as "no link"', async () => {
    serve({
      rows: [ROW],
      count: 1,
      plan_links_unread: 'arm ledger unavailable',
    })
    render(<PictureHtfPanel traderId="t1" />)
    expect(
      (await screen.findByTestId('picture-plan-links-unread')).textContent
    ).toContain('arm ledger unavailable')
  })
})

describe('DayPlanEditor — the Picture switch is a source selector', () => {
  it('the hint sits under the Picture toggle', () => {
    render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={vi.fn()}
        language="en"
      />
    )
    const hint = screen.getByTestId('picture-htf-source-hint')
    expect(hint.textContent).toBe(
      'Source selector: Picture HTF setups become Day Plan scenarios (limit at the far edge of a small zone, 1 contract, under the Day Plan master)'
    )
    const toggle = screen.getByTestId('picture-htf-toggle')
    expect(
      toggle.compareDocumentPosition(hint) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
  })

  it('translated', () => {
    render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={vi.fn()}
        language="zh"
      />
    )
    expect(screen.getByTestId('picture-htf-source-hint').textContent).toContain(
      '来源选择'
    )
  })
})

describe('the guide says what the card shows (L5)', () => {
  it('the planCard section quotes the rendered strings', async () => {
    const { planCard } = await import('../../guide/content/planCard')
    const { tp } = await import('../../i18n/plan-translations')
    const text = JSON.stringify(planCard)
    expect(text).toContain(tp('machinePlanBanner', 'en'))
    expect(text).toContain(tp('pictureHtfSourceHint', 'en'))
    expect(text).toContain(
      'composed of: base + overlays o1,o3 · machine P1 (o4)'
    )
    expect(text).toContain('source picture (h1_close_break)')
    expect(text).toContain('window closes HH:MM:SS CT')
    expect(text).toContain('evidence unavailable')
  })
})
