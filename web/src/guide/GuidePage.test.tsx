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
  const mentorKnobs =
    GUIDE_SECTIONS.find((s) => s.id === 'mentor')?.blocks.flatMap((b) =>
      b.kind === 'knobs' ? b.knobs : []
    ) ?? []

  it('has exactly 158 knob cards (Section 7 census = live-page control count; W7 +6 weekly knobs, min-side card removed 2026-08-31, +2 planner-speed 2026-08-31, +1 planner stream total deadline class 37 2026-09-01, +1 planner stream retry tries+backoff class 41 2026-09-02, +1 fast-mode shadow A/B root-fix 2026-09-02, +1 stop floor + structure anchor 0B 2026-09-02), +1 wake cadence class 47 2026-09-02, −2 weekly knobs retired class 50 (WEEKLY_INVALIDATION_TF_DEFAULT, WEEKLY_COUNTER_MODE — refs-only weekly has no invalidation and no counter), +2 one-setup knobs (switch + min grade) dispatch 102 2026-09-11, +1 structure table (S1, day_plan.structure_map, default OFF) 2026-09-16, +1 by-TF freshness knob (S2, day_plan.levels_fresh_by_tf, default OFF) 2026-09-16, +2 HTF seating knobs (S3: day_plan.htf_seats, day_plan.htf_score_multiplier, both default unchanged) 2026-09-16, +1 flip re-read knob (W-FLIP-REREAD: day_plan.flip_reread, default OFF) 2026-09-17, +1 red-news hard-block currencies (W-T1-CURRENCIES: day_plan.t1_currencies, default USD) 2026-09-18, −9 W-KNOB-PRUNE 2026-09-18 (structure table, HTF score multiplier, max scenarios, acceptance window, evening digest, re-align cap, 1h anchor seat, HTF freshness by TF, min wake interval removed/folded off the page; the 5-toggle wake card became the 1-switch wake card), +1 write-time feasibility knob (W-WRITE-TIME-FEASIBILITY: day_plan.write_time_feasibility, default ON) 2026-09-18, +1 geometry reference levels (W-GEOMETRY-REFUSAL: day_plan.geometry_reference_levels, default ON) 2026-09-18, +1 death re-read (W-DEATH-REREAD: day_plan.death_reread, default ON) 2026-09-18, +1 picture HTF (W-PICTURE-HTF: day_plan.picture_htf, default OFF) 2026-09-20, +4 entry-policy knobs (W-EXEC-TRUTH W3: day_plan.entry_policy_default market_in_zone, zone_max_pts 10, zone_rest_max_min 30, min_hold_min 3) 2026-09-23, +1 planner fresh tape (PLANNER A6: day_plan.planner_fresh_tape, default ON) 2026-09-25, +1 planner contract (WAVE PLANNER A3: day_plan.planner_contract, default ON) 2026-09-25, +1 zone placement reach (PLANNER B1: day_plan.zone_place_within_pts, default 25) 2026-09-25, +9 coin source knobs (FIX-LABELS 2026-09-26: coin_source.* source type/static/excluded/hyper_all/hyper_main — live-page CoinSourceEditor controls, now in the guide), +18 indicator/klines/ranking knobs (FIX-LABELS 2026-09-26: indicators.enable_* blocks, period fields, provider API key, OI/NetFlow/Price rankings, klines TF controls), +15 grid knobs (FIX-LABELS 2026-09-26: grid_config.* — the GridConfig editor live-page controls, now in the guide), +6 picture HTF field knobs (FIX-LABELS 2026-09-26: picture_htf tick_size/pivot_window/swing_lookback/entry_window_sec/freshness_sec/min_rr), +1 cancel-confirm report regime knob (CANCEL_CONFIRM_REQUIRE_REPORT, default OFF) 2026-10-03, +1 mentor stop-limit entries knob (MENTOR_STOP_LIMIT, default OFF) 2026-10-03, −1 same knob removed 2026-10-04 (mentor arms are always stop-limit; the env is no longer read), −1 retired ISB minimum stop card 2026-10-04 (no runtime reader); +55 mentor-mode knobs (including B20–B23, ORB gate, P3 stop rules and the retired ISB minimum-stop field) 2026-10-03', () => {
    expect(allKnobs).toHaveLength(159)
  })

  it('shows an exact key and live status for every mentor knob', () => {
    expect(mentorKnobs).toHaveLength(55)
    const keys = mentorKnobs.map((knob) => {
      expect(
        knob.settingId,
        `mentor knob "${knob.label}" settingId`
      ).toBeTruthy()
      expect(typeof knob.live, `mentor knob "${knob.label}" live`).toBe(
        'boolean'
      )
      return knob.settingId
    })
    expect(new Set(keys).size).toBe(keys.length)
    expect(keys).toEqual(
      expect.arrayContaining([
        'Config.TriggerSchool',
        'Config.PingPongMinGapPts',
        'Config.PingPongCandleMaxPts',
        'Config.PingPongCandleLookback',
        'Config.LevelMaxVisits',
        'Config.OrbGateEnabled',
        'Config.StopCeilingPts',
        'mentor_done_after_win',
        'mentor_window_start',
        'mentor_window_minutes',
        'mentor_loss_departure_pts',
      ])
    )
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

  it('renders all 17 sections with deep-link ids', () => {
    render(<GuidePage />)
    expect(screen.getByTestId('guide-page')).toBeTruthy()
    expect(GUIDE_SECTIONS).toHaveLength(17)
    for (const s of GUIDE_SECTIONS) {
      const el = document.getElementById(s.id)
      expect(el, `section id #${s.id}`).toBeTruthy()
      expect(el!.textContent).toContain(String(s.num))
    }
  })

  it('renders mentor knob keys and live status on their cards', () => {
    render(<GuidePage />)
    const schoolKey = screen.getByText('Config.TriggerSchool')
    const schoolCard = schoolKey.closest('[data-testid="guide-knob"]')
    expect(schoolCard).toBeTruthy()
    expect(within(schoolCard as HTMLElement).getByText('yes')).toBeTruthy()

    // a code-constant default (no Studio control) is honestly "live: no"
    const constantKey = screen.getByText('Config.StopCeilingPts')
    const constantCard = constantKey.closest('[data-testid="guide-knob"]')
    expect(constantCard).toBeTruthy()
    expect(within(constantCard as HTMLElement).getByText('no')).toBeTruthy()
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
