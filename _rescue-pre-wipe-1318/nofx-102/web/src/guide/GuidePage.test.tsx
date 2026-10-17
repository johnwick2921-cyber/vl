// GuidePage content + render tests. FE-only, no backend.
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { GuidePage, GUIDE_SECTIONS } from './GuidePage'
import { GUIDE_BUILT_REV } from './types'

// ── per-knob required-fields lint: EVERY KnobSpec field must be non-empty ──
describe('knob spec completeness', () => {
  const fields = [
    'label',
    'where',
    'what',
    'trader',
    'consumer',
    'range',
    'systemDefault',
    'recommended',
    'whenToTouch',
    'perSession',
  ] as const

  const allKnobs = GUIDE_SECTIONS.flatMap((s) =>
    s.blocks.flatMap((b) => (b.kind === 'knobs' ? b.knobs : []))
  )

  it('has exactly 52 knob cards (Section 7 census = live-page control count; W7 +6 weekly knobs, min-side card removed 2026-08-31, +2 planner-speed 2026-08-31, +1 planner stream total deadline class 37 2026-09-01, +1 planner stream retry tries+backoff class 41 2026-09-02, +1 fast-mode shadow A/B root-fix 2026-09-02, +1 stop floor + structure anchor 0B 2026-09-02), +1 wake cadence class 47 2026-09-02, −2 weekly knobs retired class 50 (WEEKLY_INVALIDATION_TF_DEFAULT, WEEKLY_COUNTER_MODE — refs-only weekly has no invalidation and no counter), +2 one-setup knobs (switch + min grade) dispatch 102 2026-09-11, +1 structure table (S1, day_plan.structure_map, default OFF) 2026-09-16, +1 by-TF freshness knob (S2, day_plan.levels_fresh_by_tf, default OFF) 2026-09-16, +2 HTF seating knobs (S3: day_plan.htf_seats, day_plan.htf_score_multiplier, both default unchanged) 2026-09-16, +1 flip re-read knob (W-FLIP-REREAD: day_plan.flip_reread, default OFF) 2026-09-17', () => {
    expect(allKnobs).toHaveLength(52)
  })

  it('every knob card fills all ten mandatory fields', () => {
    for (const k of allKnobs) {
      for (const f of fields) {
        expect(k[f], `knob "${k.label}" field ${f}`).toBeTruthy()
      }
    }
  })

  it('every recommended field states its reason', () => {
    for (const k of allKnobs) {
      expect(k.recommended.length).toBeGreaterThan(10)
    }
  })
})

describe('GuidePage', () => {
  beforeEach(() => {
    // never-resolving default: only the drift tests stub a revision
    vi.stubGlobal('fetch', vi.fn().mockReturnValue(new Promise(() => {})))
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders all 15 sections with deep-link ids', () => {
    render(<GuidePage />)
    expect(screen.getByTestId('guide-page')).toBeTruthy()
    expect(GUIDE_SECTIONS).toHaveLength(15)
    for (const s of GUIDE_SECTIONS) {
      const el = document.getElementById(s.id)
      expect(el, `section id #${s.id}`).toBeTruthy()
      expect(el!.textContent).toContain(String(s.num))
    }
  })

  it('renders the live-component examples (guide-example testids)', () => {
    render(<GuidePage />)
    // mock card renders inside its Example wrapper
    expect(
      screen.getAllByTestId('guide-example').length
    ).toBeGreaterThanOrEqual(1)
    expect(screen.getByTestId('mock-plan-card')).toBeTruthy()
    // real chips inside the mock
    expect(
      document.querySelector('[data-testid^="confirm-chip-"]')
    ).toBeTruthy()
    expect(document.querySelector('[data-testid^="fvg-chip-"]')).toBeTruthy()
  })

  it('search filters hits and links to sections', () => {
    render(<GuidePage />)
    const input = screen.getByTestId('guide-search')
    fireEvent.change(input, { target: { value: 'thin side' } })
    // the planCard callout + glossary should both surface hits
    const links = screen
      .getAllByRole('link')
      .filter((a) => a.getAttribute('href')?.startsWith('#'))
    expect(links.length).toBeGreaterThan(0)
    expect(links.some((a) => a.getAttribute('href') === '#plan-card')).toBe(
      true
    )
  })

  it('search with no matches says so', () => {
    render(<GuidePage />)
    fireEvent.change(screen.getByTestId('guide-search'), {
      target: { value: 'zzzzqqqq' },
    })
    expect(screen.getByText(/no matches/i)).toBeTruthy()
  })

  it('shows the rev-drift banner when the bot revision differs', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        json: () =>
          Promise.resolve({ status: 'ok', revision: '0ldcafe12345678' }),
      })
    )
    render(<GuidePage />)
    expect(await screen.findByTestId('guide-rev-drift')).toBeTruthy()
  })

  it('hides the drift banner when the bot revision matches', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        json: () =>
          Promise.resolve({ status: 'ok', revision: GUIDE_BUILT_REV }),
      })
    )
    render(<GuidePage />)
    // flush the fetch microtask inside act so the revision update is wrapped
    await act(async () => {})
    expect(screen.queryByTestId('guide-rev-drift')).toBeNull()
  })

  // The server sends kernel.RunningRevision(), which is shortRev() — 12 chars.
  // The previous test fed GUIDE_BUILT_REV back to itself, so it proved the
  // component agrees with itself and never that it agrees with the bot.
  it('hides the drift banner when the bot reports the SHORT rev of the same commit', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        json: () =>
          Promise.resolve({
            status: 'ok',
            revision: GUIDE_BUILT_REV.slice(0, 12),
          }),
      })
    )
    render(<GuidePage />)
    await act(async () => {})
    expect(screen.queryByTestId('guide-rev-drift')).toBeNull()
  })

  // "" means the boot assertion has not run yet — the bot does not know its own
  // rev. Not knowing is not disagreeing, so the banner must stay down.
  it('does not claim drift when the bot has no revision yet', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        json: () => Promise.resolve({ status: 'ok', revision: '' }),
      })
    )
    render(<GuidePage />)
    await act(async () => {})
    expect(screen.queryByTestId('guide-rev-drift')).toBeNull()
  })

  it('renders knob cards with all ten fields', () => {
    render(<GuidePage />)
    const knobs = screen.getAllByTestId('guide-knob')
    expect(knobs.length).toBeGreaterThanOrEqual(20)
    const first = within(knobs[0])
    for (const label of [
      'where',
      'range',
      'default',
      'per-session',
      'engine',
      'touch it when',
    ]) {
      expect(first.getByText(new RegExp(label, 'i'))).toBeTruthy()
    }
  })
})
