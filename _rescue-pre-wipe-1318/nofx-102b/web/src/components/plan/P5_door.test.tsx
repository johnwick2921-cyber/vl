// P5 (FE) — edit sheet + Ask-Planner + conflict detection. api + sonner mocked so
// the tests drive the door from props and assert the API calls it makes.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import type { PlanLevelFact, PlanQAMessage } from '../../lib/api/plan'

const postOverlay = vi.fn().mockResolvedValue({ ok: true })
const addOwnerLevel = vi.fn().mockResolvedValue({ ok: true, id: 1 })
const askPlanner = vi.fn().mockResolvedValue({ ok: true, data: {} })
const getPlanThread = vi.fn().mockResolvedValue({ thread: [], kpi: {} })
const applyAsk = vi.fn().mockResolvedValue({ ok: true })

vi.mock('../../lib/api', () => ({
  api: {
    postOverlay: (...a: unknown[]) => postOverlay(...a),
    addOwnerLevel: (...a: unknown[]) => addOwnerLevel(...a),
    askPlanner: (...a: unknown[]) => askPlanner(...a),
    getPlanThread: (...a: unknown[]) => getPlanThread(...a),
    applyAsk: (...a: unknown[]) => applyAsk(...a),
  },
}))
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

import { EditSheet } from './EditSheet'
import { AskPlannerPanel } from './AskPlannerPanel'
import { detectConflicts } from './levelState'

const fact = (o: Partial<PlanLevelFact>): PlanLevelFact => ({
  price: 30000,
  label: 'PDH',
  grade: 'A',
  instruction: 'fade',
  distance: 10,
  sweep: false,
  closes_beyond: 0,
  accept_have: 0,
  accept_need: 2,
  still_valid: true,
  ...o,
})

describe('detectConflicts', () => {
  it('ghosts the AI level when an owner level opposes it at the same price', () => {
    const facts = [
      fact({ price: 30100, instruction: 'reclaim-long', origin: 'OWNER' }),
      fact({ price: 30101, instruction: 'fade', origin: 'AI' }), // AI, opposing, ~same price
      fact({ price: 29000, instruction: 'fade', origin: 'AI' }), // far away, no conflict
    ]
    const { ghosted, flagged } = detectConflicts(facts)
    expect(ghosted.has(1)).toBe(true) // AI element loses
    expect(ghosted.has(2)).toBe(false)
    expect(flagged.has(0)).toBe(true)
    expect(flagged.has(1)).toBe(true)
  })
  it('no conflict when instructions agree', () => {
    const facts = [
      fact({ price: 30100, instruction: 'fade', origin: 'OWNER' }),
      fact({ price: 30100, instruction: 'fade', origin: 'AI' }),
    ]
    expect(detectConflicts(facts).ghosted.size).toBe(0)
  })
})

describe('EditSheet', () => {
  beforeEach(() => {
    postOverlay.mockClear()
    addOwnerLevel.mockClear()
  })

  it('renders nothing when closed', () => {
    const { container } = render(
      <EditSheet
        open={false}
        traderId="t1"
        symbol="MNQ"
        language="en"
        onClose={() => {}}
        onSaved={() => {}}
      />
    )
    expect(container.querySelector('[role="dialog"]')).toBeNull()
  })

  it('edit mode posts an overlay replace at the level index', async () => {
    render(
      <EditSheet
        open
        traderId="t1"
        symbol="MNQ"
        language="en"
        level={fact({ price: 30246.5, label: '1h-LH' })}
        levelIndex={2}
        onClose={() => {}}
        onSaved={() => {}}
      />
    )
    fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(postOverlay).toHaveBeenCalled())
    const [, patch] = postOverlay.mock.calls[0]
    expect(patch[0].op).toBe('replace')
    expect(patch[0].path).toBe('/levels/2')
  })

  it('add mode posts a sticky owner level', async () => {
    render(
      <EditSheet
        open
        traderId="t1"
        symbol="MNQ"
        language="en"
        onClose={() => {}}
        onSaved={() => {}}
      />
    )
    const priceInput = screen.getByRole('dialog').querySelector('input')!
    fireEvent.change(priceInput, { target: { value: '30150' } })
    fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(addOwnerLevel).toHaveBeenCalled())
    const [, level] = addOwnerLevel.mock.calls[0]
    expect(level.price).toBe(30150)
  })
})

describe('AskPlannerPanel', () => {
  beforeEach(() => {
    askPlanner.mockClear()
    getPlanThread.mockClear()
  })

  it('renders the thread + sends a question', async () => {
    const thread: PlanQAMessage[] = [
      {
        id: 1,
        role: 'owner',
        content: 'why supply here?',
        evidence: '',
        point_class: '',
        verdict: '',
        patch: '',
        applied: false,
        created_at: 1,
      },
      {
        id: 2,
        role: 'planner',
        content: 'holding',
        evidence: 'bias long',
        point_class: 'BARE-DISAGREEMENT',
        verdict: 'DEFEND',
        patch: '',
        applied: false,
        created_at: 2,
      },
    ]
    getPlanThread.mockResolvedValueOnce({ thread, kpi: {} })
    render(
      <AskPlannerPanel
        open
        traderId="t1"
        symbol="MNQ"
        language="en"
        onClose={() => {}}
        onApplied={() => {}}
      />
    )
    await waitFor(() =>
      expect(screen.getByText('why supply here?')).toBeInTheDocument()
    )
    expect(screen.getByText(/bias long/)).toBeInTheDocument()

    const input = screen.getByPlaceholderText(/ask about today/i)
    fireEvent.change(input, { target: { value: 'are you sure?' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() =>
      expect(askPlanner).toHaveBeenCalledWith('t1', 'are you sure?', 'MNQ')
    )
  })

  it('a bare-disagreement reply shows no Apply button', async () => {
    const thread: PlanQAMessage[] = [
      {
        id: 2,
        role: 'planner',
        content: 'holding',
        evidence: 'e',
        point_class: 'BARE-DISAGREEMENT',
        verdict: 'DEFEND',
        patch: '',
        applied: false,
        created_at: 2,
      },
    ]
    getPlanThread.mockResolvedValueOnce({ thread, kpi: {} })
    render(
      <AskPlannerPanel
        open
        traderId="t1"
        symbol="MNQ"
        language="en"
        onClose={() => {}}
        onApplied={() => {}}
      />
    )
    await waitFor(() =>
      expect(screen.getAllByText(/DEFEND/).length).toBeGreaterThan(0)
    )
    expect(screen.queryByText(/Apply merge/i)).not.toBeInTheDocument()
  })
})

describe('EditSheet — prefill integrity (UI verification 2026-08-18)', () => {
  beforeEach(() => {
    postOverlay.mockClear()
    addOwnerLevel.mockClear()
  })

  it('AI level: the sheet shows the EXACT instruction the card shows', () => {
    const lvl = fact({
      price: 30217.75,
      label: 'EQH',
      origin: 'AI',
      instruction: 'hold / defend for long structure',
    })
    render(
      <EditSheet
        open
        traderId="t1"
        symbol="MNQ"
        language="en"
        level={lvl}
        levelIndex={0}
        onClose={() => {}}
        onSaved={() => {}}
      />
    )
    const input = screen.getByTestId('instruction-input') as HTMLInputElement
    expect(input.value).toBe('hold / defend for long structure')
  })

  it('AI level save preserves the prose instruction (never rewritten by the vocab)', async () => {
    const lvl = fact({
      price: 30217.75,
      label: 'EQH',
      origin: 'AI',
      instruction: 'hold / defend for long structure',
    })
    render(
      <EditSheet
        open
        traderId="t1"
        symbol="MNQ"
        language="en"
        level={lvl}
        levelIndex={0}
        onClose={() => {}}
        onSaved={() => {}}
      />
    )
    fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(postOverlay).toHaveBeenCalled())
    const [, patch] = postOverlay.mock.calls[0]
    expect(patch[0].value.instruction).toBe('hold / defend for long structure')
    // AI-origin: no owner write-through keys.
    expect(patch[0].value.note).toBeUndefined()
    expect(patch[0].value.scenario_tag).toBeUndefined()
  })

  it('OWNER level: scenario tag prefills and note+tag ride the save patch', async () => {
    const lvl = fact({
      price: 30166.25,
      label: '👤 PDL',
      origin: 'OWNER',
      note: 'my shelf',
      scenario_id: 'S1',
      instruction: 'reclaim',
    })
    render(
      <EditSheet
        open
        traderId="t1"
        symbol="MNQ"
        language="en"
        level={lvl}
        levelIndex={3}
        scenarioIds={['S1', 'S2']}
        onClose={() => {}}
        onSaved={() => {}}
      />
    )
    // note input prefilled
    const noteInput = screen.getByDisplayValue('my shelf')
    expect(noteInput).toBeTruthy()
    // S1 chip selected
    const s1 = screen.getAllByText('S1')
    expect(s1.length).toBeGreaterThan(0)
    fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(postOverlay).toHaveBeenCalled())
    const [, patch] = postOverlay.mock.calls[0]
    expect(patch[0].value.note).toBe('my shelf')
    expect(patch[0].value.scenario_tag).toBe('S1')
  })
})
