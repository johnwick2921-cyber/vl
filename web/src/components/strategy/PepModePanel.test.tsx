import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { PepConfig } from '../../types'
import type { StudioEffective } from '../../lib/api/strategyEffective'
import { PEP_FIELDS, PepModePanel } from './PepModePanel'
import { fullPepBlock } from './pepModel'

const registry = readFileSync(
  resolve(process.cwd(), '../store/knob_registry_pep.go'),
  'utf8'
)

function setup(config?: PepConfig, effective?: StudioEffective, lang = 'en') {
  const onChange = vi.fn()
  render(
    <PepModePanel
      config={config}
      onChange={onChange}
      language={lang}
      effective={effective}
    />
  )
  return { onChange }
}

function commitNumber(testId: string, value: string) {
  const el = screen.getByTestId(testId)
  fireEvent.focus(el)
  fireEvent.change(el, { target: { value } })
  fireEvent.blur(el)
}

describe('PepModePanel controls', () => {
  // Mutant: drop one PEP_FIELDS entry (or add a field without a registry leaf) -> RED.
  it('PEP_FIELDS is every registered pep leaf except the master switch (which lives in the Trading mode selector)', () => {
    const registered = [...registry.matchAll(/live\("(pep\.[a-z_.]+)"/g)].map(
      (m) => m[1]
    )
    expect(PEP_FIELDS.map((f) => f.path).sort()).toEqual(
      registered.filter((p) => p !== 'pep.pep_enabled').sort()
    )
  })

  it('shows a CONCRETE value in every shared field with no stored block (the shipped default)', () => {
    setup(undefined)
    expect(screen.getByTestId('pep-pep_contracts')).toHaveValue(6)
    expect(screen.getByTestId('pep-pep_tick_size')).toHaveValue(0.25)
    expect(screen.getByTestId('pep-pep_fee_per_contract_usd')).toHaveValue(1)
    expect(screen.getByTestId('pep-pep_signal_max_age_min')).toHaveValue(2)
    // the rule rows (read-only under the shipped style E) show the PDF values
    expect(screen.getByTestId('pep-pep_box_start_ct')).toHaveValue('08:30')
    expect(screen.getByTestId('pep-pep_flat_time_ct')).toHaveValue('15:00')
    expect(screen.getByTestId('pep-pep_sl_rounding')).toHaveValue('outward')
    expect(screen.getByTestId('pep-pep_max_trades_per_day')).toHaveValue(1)
    expect(screen.getByTestId('pep-pep_max_box_points')).toHaveValue(0)
    // style E is the running (shipped) style, so its rules show: a 2 minute box
    expect(screen.getByTestId('pep-pep_e_box_minutes')).toHaveValue(2)
    expect(screen.getByTestId('pep-pep_e_sl_fraction')).toHaveValue(0.5)
    expect(screen.getByTestId('pep-pep_e_signal_source')).toHaveValue('ema')
  })

  it('the E rules show the E numbers (2m box, half-box stop, EMA 9 on 2m, ema source)', () => {
    setup(undefined)
    expect(screen.getByTestId('pep-pep_e_box_minutes')).toHaveValue(2)
    expect(screen.getByTestId('pep-pep_e_sl_fraction')).toHaveValue(0.5)
    expect(screen.getByTestId('pep-pep_e_tp_fraction')).toHaveValue(1)
    expect(screen.getByTestId('pep-pep_e_ema_period')).toHaveValue(9)
    expect(screen.getByTestId('pep-pep_e_ema_tf_min')).toHaveValue('2')
    expect(screen.getByTestId('pep-pep_e_signal_source')).toHaveValue('ema')
    expect(screen.getByTestId('pep-pep_e_warmup_bars')).toHaveValue(0)
    expect(
      screen.queryByTestId('pep-pep_a_box_minutes')
    ).not.toBeInTheDocument()
  })

  // m461 PIN (UI): A and E show their rules READ-ONLY with a "locked to the PDF"
  // badge and note, whatever is stored; Custom shows editable inputs; contracts
  // are editable in all three. Mutant: drop `|| locked` from FieldRow's disabled
  // (or the lockedValue passed for A/E) -> RED.
  it.each(['A', 'E'] as const)(
    'style %s: every rule control is disabled, shows the PDF value (not the stored one) and the locked note',
    (style) => {
      setup({
        pep_style: style,
        pep_flat_time_ct: '14:00',
        pep_max_trades_per_day: 4,
        pep_max_box_points: 90,
        style_a: { pep_a_box_minutes: 9, pep_a_sl_fraction: 3 },
        style_e: { pep_e_ema_period: 20, pep_e_box_minutes: 7 },
      })
      expect(screen.getByTestId('pep-locked-note')).toHaveTextContent(
        'Locked to the PDF'
      )
      const rules = within(screen.getByTestId(`pep-style-${style}-block`))
      const ctrls = rules
        .getAllByTestId(/^pep-pep_/)
        .filter(
          (el) => !el.getAttribute('data-testid')!.match(/-(row|locked|error)$/)
        )
      expect(ctrls.length).toBeGreaterThanOrEqual(9)
      for (const el of ctrls) expect(el).toBeDisabled()
      expect(rules.getAllByText('locked to the PDF').length).toBe(ctrls.length)
      expect(screen.getByTestId('pep-pep_flat_time_ct')).toHaveValue('15:00')
      expect(screen.getByTestId('pep-pep_max_trades_per_day')).toHaveValue(1)
      expect(screen.getByTestId('pep-pep_max_box_points')).toHaveValue(0)
      if (style === 'A') {
        expect(screen.getByTestId('pep-pep_a_box_minutes')).toHaveValue(5)
        expect(screen.getByTestId('pep-pep_a_sl_fraction')).toHaveValue(1)
      } else {
        expect(screen.getByTestId('pep-pep_e_ema_period')).toHaveValue(9)
        expect(screen.getByTestId('pep-pep_e_box_minutes')).toHaveValue(2)
      }
      // contracts / fee (the shared block) stay editable under A / E
      for (const id of [
        'pep-pep_contracts',
        'pep-pep_fee_per_contract_usd',
        'pep-pep_style',
      ])
        expect(screen.getByTestId(id)).toBeEnabled()
    }
  )

  it.each(['A', 'E', 'Custom'])(
    'contracts are editable in style %s',
    (style) => {
      const { onChange } = setup({ pep_style: style })
      commitNumber('pep-pep_contracts', '4')
      expect((onChange.mock.calls[0][0] as PepConfig).pep_contracts).toBe(4)
    }
  )

  it('a locked control writes nothing even if an event reaches it', () => {
    const { onChange } = setup({ pep_style: 'A' })
    fireEvent.change(screen.getByTestId('pep-pep_a_both_edges'), {
      target: { value: 'nearest_open' },
    })
    fireEvent.change(screen.getByTestId('pep-pep_flat_time_ct'), {
      target: { value: '14:30' },
    })
    expect(onChange).not.toHaveBeenCalled()
  })

  it('style Custom: editable inputs seeded from E, saved values shown, the picker names Custom', () => {
    const { onChange } = setup({
      pep_style: 'custom',
      pep_flat_time_ct: '14:00',
      style_custom: { pep_c_box_minutes: 7 },
    })
    expect(screen.queryByTestId('pep-locked-note')).toBeNull()
    expect(screen.getByTestId('pep-pep_style')).toHaveValue('Custom')
    expect(screen.getByTestId('pep-pep_c_entry_type')).toHaveValue('ema_cross')
    expect(screen.getByTestId('pep-pep_c_box_minutes')).toHaveValue(7)
    expect(screen.getByTestId('pep-pep_c_sl_fraction')).toHaveValue(0.5)
    expect(screen.getByTestId('pep-pep_c_ema_period')).toHaveValue(9)
    expect(screen.getByTestId('pep-pep_flat_time_ct')).toHaveValue('14:00')
    for (const id of [
      'pep-pep_c_entry_type',
      'pep-pep_c_box_minutes',
      'pep-pep_c_sl_fraction',
      'pep-pep_c_tp_fraction',
      'pep-pep_c_ema_period',
      'pep-pep_c_ema_tf_min',
      'pep-pep_c_signal_source',
      'pep-pep_c_warmup_bars',
      'pep-pep_box_start_ct',
      'pep-pep_flat_time_ct',
      'pep-pep_sl_rounding',
      'pep-pep_max_trades_per_day',
      'pep-pep_max_box_points',
    ])
      expect(screen.getByTestId(id), id).toBeEnabled()
    // breakout hides the EMA rows and shows the both-edges policy
    expect(screen.queryByTestId('pep-pep_c_both_edges')).toBeNull()
    commitNumber('pep-pep_c_box_minutes', '9')
    expect(
      (onChange.mock.calls[0][0] as PepConfig).style_custom?.pep_c_box_minutes
    ).toBe(9)
    commitNumber('pep-pep_max_box_points', '120')
    expect((onChange.mock.calls[1][0] as PepConfig).pep_max_box_points).toBe(
      120
    )
    fireEvent.change(screen.getByTestId('pep-pep_c_entry_type'), {
      target: { value: 'breakout' },
    })
    expect(
      (onChange.mock.calls[2][0] as PepConfig).style_custom?.pep_c_entry_type
    ).toBe('breakout')
  })

  // P3 (c): the resolver case-folds the entry type, so must the UI.
  // Mutant: drop .toLowerCase() from isBreakout -> RED.
  it('a stored entry type "Breakout" (any case) shows the breakout rows', () => {
    setup({
      pep_style: 'Custom',
      style_custom: { pep_c_entry_type: ' Breakout ' },
    })
    expect(screen.getByTestId('pep-pep_c_both_edges')).toBeEnabled()
    expect(screen.getByTestId('pep-pep_c_entry_type')).toHaveValue('breakout')
    expect(screen.queryByTestId('pep-pep_c_ema_period')).toBeNull()
  })

  // Reviewer P2 (m461): the tick and the signal age are RULES under A / E.
  it.each(['A', 'E'] as const)(
    'style %s: tick and signal age are read-only PDF values',
    (style) => {
      setup({ pep_style: style, pep_tick_size: 1, pep_signal_max_age_min: 9 })
      for (const [id, v] of [
        ['pep-pep_tick_size', 0.25],
        ['pep-pep_signal_max_age_min', 2],
      ] as const) {
        expect(screen.getByTestId(id)).toBeDisabled()
        expect(screen.getByTestId(id)).toHaveValue(v)
      }
      expect(screen.getByTestId('pep-pep_contracts')).toBeEnabled()
      expect(screen.getByTestId('pep-pep_fee_per_contract_usd')).toBeEnabled()
    }
  )
  it('Custom: tick and signal age are editable and show the saved values', () => {
    setup({
      pep_style: 'Custom',
      pep_tick_size: 0.5,
      pep_signal_max_age_min: 5,
    })
    expect(screen.getByTestId('pep-pep_tick_size')).toBeEnabled()
    expect(screen.getByTestId('pep-pep_tick_size')).toHaveValue(0.5)
    expect(screen.getByTestId('pep-pep_signal_max_age_min')).toHaveValue(5)
  })

  it('Custom breakout shows both-edges and hides the EMA rows', () => {
    setup({
      pep_style: 'Custom',
      style_custom: { pep_c_entry_type: 'breakout' },
    })
    expect(screen.getByTestId('pep-pep_c_both_edges')).toBeEnabled()
    expect(screen.queryByTestId('pep-pep_c_ema_period')).toBeNull()
    expect(screen.queryByTestId('pep-pep_c_signal_source')).toBeNull()
  })

  it('opens on the style that RUNS', () => {
    setup({ pep_style: 'E' })
    expect(screen.getByTestId('pep-style-E-block')).toBeInTheDocument()
    expect(screen.getByTestId('pep-pep_e_ema_period')).toBeInTheDocument()
  })

  // The rules shown are the style the SERVER runs: a junk stored style runs
  // the shipped default E (store.ResolvePep). Mutant: `=== 'E' ? 'E' : 'A'` -> RED.
  it('a junk stored style shows the E rules (the style the server runs)', () => {
    setup({ pep_style: 'Z' })
    expect(screen.getByTestId('pep-pep_e_box_minutes')).toBeInTheDocument()
    expect(screen.queryByTestId('pep-pep_a_box_minutes')).toBeNull()
  })

  it('a stored style A shows the A rules with their own 5 minute box', () => {
    setup({ pep_style: 'a' })
    expect(screen.getByTestId('pep-pep_a_box_minutes')).toHaveValue(5)
  })

  // rereview m461 P3: the translated option hints are shown, not the raw token.
  // Mutant: render o.value -> the zh text below is missing -> RED.
  it('enum options show the translated hint', () => {
    setup(undefined, undefined, 'zh')
    const sel = screen.getByTestId('pep-pep_sl_rounding')
    expect(within(sel).getByText('outward（向外）')).toBeInTheDocument()
  })

  // Mutant: onChange(f.set(block, n)) without fullPepBlock -> RED (a partial
  // block would let a stale stored number survive the server-side merge).
  it('an edit sends the WHOLE pep block with every leaf concrete', () => {
    const { onChange } = setup({ pep_contracts: 3 })
    commitNumber('pep-pep_contracts', '4')
    expect(onChange).toHaveBeenCalledTimes(1)
    const sent = onChange.mock.calls[0][0] as PepConfig
    expect(sent.pep_contracts).toBe(4)
    expect(sent).toEqual({ ...fullPepBlock(undefined), pep_contracts: 4 })
  })

  // Mutant: `const block = config ?? {}` (the stored partial block) -> RED.
  it('a stored PARTIAL block is completed with the shipped defaults in what is sent', () => {
    const { onChange } = setup({ pep_contracts: 3, style_a: {} })
    commitNumber('pep-pep_contracts', '5')
    const sent = onChange.mock.calls[0][0] as PepConfig
    expect(sent.pep_flat_time_ct).toBe('15:00')
    expect(sent.style_custom?.pep_c_ema_period).toBe(9)
    expect(sent.style_e?.pep_e_ema_period).toBe(9)
    expect(sent.style_a?.pep_a_box_minutes).toBe(5)
  })

  it('clamps to the hard ceiling of 10 contracts on commit', () => {
    const { onChange } = setup()
    commitNumber('pep-pep_contracts', '25')
    expect((onChange.mock.calls[0][0] as PepConfig).pep_contracts).toBe(10)
  })

  it('an empty commit keeps the stored value, a 0 max-box stays a legal 0', () => {
    const { onChange } = setup({ pep_contracts: 4, pep_style: 'Custom' })
    commitNumber('pep-pep_contracts', '')
    expect((onChange.mock.calls[0][0] as PepConfig).pep_contracts).toBe(4)
    commitNumber('pep-pep_max_box_points', '0')
    expect((onChange.mock.calls[1][0] as PepConfig).pep_max_box_points).toBe(0)
  })

  it('switching the running style keeps the other style numbers', () => {
    const { onChange } = setup({
      pep_style: 'A',
      style_e: { pep_e_ema_period: 12 },
    })
    fireEvent.change(screen.getByTestId('pep-pep_style'), {
      target: { value: 'E' },
    })
    const sent = onChange.mock.calls[0][0] as PepConfig
    expect(sent.pep_style).toBe('E')
    expect(sent.style_e?.pep_e_ema_period).toBe(12)
    expect(sent.style_a?.pep_a_box_minutes).toBe(5)
  })

  // Mutant: drop `.toUpperCase()` from the pep_style getter in PEP_FIELDS -> the
  // select has no matching option for a stored lowercase "e" -> RED (review round
  // 1, M3: the server case-folds the style, so a stored "e" is legal and runs E).
  it('a stored lowercase style "e" shows as E (the server case-folds it), with the E rules open', () => {
    setup({ pep_style: 'e' })
    expect(screen.getByTestId('pep-pep_style')).toHaveValue('E')
    expect(screen.getByTestId('pep-pep_e_ema_period')).toBeInTheDocument()
  })

  it('the Custom signal source is a setting (ema | close)', () => {
    const { onChange } = setup({ pep_style: 'Custom' })
    fireEvent.change(screen.getByTestId('pep-pep_c_signal_source'), {
      target: { value: 'close' },
    })
    expect(
      (onChange.mock.calls[0][0] as PepConfig).style_custom?.pep_c_signal_source
    ).toBe('close')
  })

  it('a bad HH:MM is refused with a message and writes nothing; a good one writes the block', () => {
    const { onChange } = setup({ pep_style: 'Custom' })
    const t = screen.getByTestId('pep-pep_flat_time_ct')
    fireEvent.change(t, { target: { value: '25:99' } })
    expect(onChange).not.toHaveBeenCalled()
    fireEvent.change(t, { target: { value: '14:30' } })
    expect((onChange.mock.calls[0][0] as PepConfig).pep_flat_time_ct).toBe(
      '14:30'
    )
  })

  it('banner says OFF with the settings kept, or ON with the style', () => {
    const { rerender } = render(
      <PepModePanel onChange={() => {}} language="en" />
    )
    expect(screen.getByTestId('pep-state-banner')).toHaveTextContent(
      'Pep is OFF'
    )
    rerender(
      <PepModePanel
        config={{ pep_enabled: true, pep_style: 'E' }}
        onChange={() => {}}
        language="en"
      />
    )
    expect(screen.getByTestId('pep-state-banner')).toHaveTextContent(
      'Pep is ON: style E, SIM only'
    )
  })

  it('a disabled panel writes nothing', () => {
    const onChange = vi.fn()
    render(<PepModePanel onChange={onChange} language="en" disabled />)
    fireEvent.change(screen.getByTestId('pep-pep_style'), {
      target: { value: 'E' },
    })
    expect(onChange).not.toHaveBeenCalled()
  })
})

describe('PepModePanel effective lines', () => {
  const knob = (path: string, effective: unknown, origin: string) => ({
    path,
    status: 'live',
    ui_label: '',
    stored: { present: true, value: effective },
    effective,
    origin,
    scope: 'strategy',
    resolver: 'store.ResolvePep',
    resolved: true,
  })
  it('renders the SERVER value under its own field, including a clamp origin', () => {
    const byPath = {
      'pep.pep_contracts': knob(
        'pep.pep_contracts',
        10,
        'clamp (store.ResolvePep): saved 25 is above the maximum 10: 10 runs'
      ),
    }
    setup({ pep_contracts: 25 }, { byPath, bySession: {} })
    const row = screen.getByTestId('pep-pep_contracts-row')
    expect(within(row).getByTestId('effective-chip')).toHaveTextContent(
      'eff 10 · clamp (store.ResolvePep): saved 25 is above the maximum 10'
    )
    // the input shows the SAVED number, the chip the value the bot RUNS
    expect(screen.getByTestId('pep-pep_contracts')).toHaveValue(25)
    // no other row invents a chip
    expect(screen.getAllByTestId('effective-chip')).toHaveLength(1)
  })
})

describe('PepModePanel i18n', () => {
  it('renders id and zh labels and help', () => {
    const { unmount } = render(
      <PepModePanel onChange={() => {}} language="id" />
    )
    expect(screen.getByText(/Dipakai bersama semua gaya/)).toBeInTheDocument()
    unmount()
    render(<PepModePanel onChange={() => {}} language="zh" />)
    expect(screen.getByText('所有风格共用')).toBeInTheDocument()
  })
})
