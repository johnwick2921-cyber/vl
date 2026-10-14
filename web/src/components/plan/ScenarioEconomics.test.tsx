import { render, screen, cleanup } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { ScenarioList } from './ScenarioList'
import type { PlanScenario } from '../../lib/api/plan'
afterEach(cleanup)
const base = {
  id: 'S1',
  trigger: 'declared setup',
  condition: 'reject',
  direction: 'long',
  target_chain: [29671.42, 29721.25],
  invalid: '5m below 29640',
  quality: 'B',
}
it('legacy missing obstacle and R stay UNKNOWN even with an arm', () => {
  const scenario = {
    ...base,
    arm: { entry: 29664.5, stop: 29640, target: 29721.25 },
  }
  render(<ScenarioList scenarios={[scenario]} language="en" />)
  const text = screen.getByTestId('economics-S1').textContent
  for (const want of [
    'First obstacle UNKNOWN',
    'Obstacle R UNKNOWN',
    'Arm R UNKNOWN',
  ])
    expect(text).toContain(want)
})
it('E3 renders both Rs and sub-1R fact even with unevaluable activation', () => {
  const scenario = {
    ...base,
    arm: { entry: 29664.5, stop: 29640, target: 29721.25 },
    economics: {
      version: 1,
      entry_zone: [29664.5, 29664.5],
      first_obstacle: {
        price: 29671.42,
        level: 'declared reference',
        family: 'reference',
        response: 'pass_through',
      },
      r_to_obstacle: 6.92 / 24.5,
      r_to_arm_target: 56.75 / 24.5,
    },
  }
  render(
    <ScenarioList
      scenarios={[scenario]}
      meta={{ unevaluable: ['S1'] }}
      language="en"
    />
  )
  const text = screen.getByTestId('economics-S1').textContent
  for (const want of [
    'Obstacle R 0.282449',
    'Arm R 2.316327',
    'Sub-1R first obstacle',
    'pass through',
    'Arm target (order objective) 29721.25',
  ])
    expect(text).toContain(want)
})
// Measured C2 rows; expectations use raw prices, not authored R or UI helpers.
const rows = [
  [170, 29351.47, 29408.52, 29280.88, 29280.88],
  [180, 29182, 29212.5, 29154.38, 29154.38],
  [182, 29123.25, 29147.25, 29085, 29100.81],
  [185, 29085, 29125, 29062.75, 29062.75],
  [187, 29100.5, 29130, 29082.75, 29082.75],
  [252, 29611.25, 29481.5, 29720, 29720],
]
for (const [id, entry, stop, target, obstacle] of rows)
  it(`E5 row ${id} recomputes geometry`, () => {
    const scenario = {
      ...base,
      arm: { entry, stop, target },
      economics: {
        version: 1,
        entry_zone: [entry, entry],
        first_obstacle: {
          price: obstacle,
          level: 'declared reference',
          family: 'reference',
          response: 'pass_through',
        },
        r_to_obstacle: 999,
        r_to_arm_target: 999,
      },
    } as PlanScenario
    render(<ScenarioList scenarios={[scenario]} language="en" />)
    const text = screen.getByTestId('economics-S1').textContent
    expect(text).toContain(
      `Obstacle R ${(Math.abs(obstacle - entry) / Math.abs(entry - stop)).toFixed(6)}`
    )
    expect(text).toContain(
      `Arm R ${(Math.abs(target - entry) / Math.abs(entry - stop)).toFixed(6)}`
    )
  })
// R22 F3 — the plan view shows WHY an arm was disabled at write: the
// flip-coherence class carries its readable reason; other classes show as-is.
it('R22 flip_incoherent shows its reason on the plan view', () => {
  const scenario = {
    ...base,
    arm: {
      entry: 29664.5,
      stop: 29640,
      target: 29721.25,
      enabled: false,
      arm_disabled_reason: 'flip_incoherent',
    },
  }
  render(<ScenarioList scenarios={[scenario]} language="en" />)
  const text = screen.getByTestId('economics-S1').textContent
  expect(text).toContain(
    "disabled at write: flip_incoherent — entry sits past the plan's own flip line, against the flip"
  )
})
