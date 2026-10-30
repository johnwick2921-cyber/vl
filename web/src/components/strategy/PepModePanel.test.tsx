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
    expect(screen.getByTestId('pep-pep_contracts')).toHaveValue(3)
    expect(screen.getByTestId('pep-pep_box_start_ct')).toHaveValue('08:30')
    expect(screen.getByTestId('pep-pep_flat_time_ct')).toHaveValue('15:00')
    expect(screen.getByTestId('pep-pep_tick_size')).toHaveValue(0.25)
    expect(screen.getByTestId('pep-pep_sl_rounding')).toHaveValue('outward')
    expect(screen.getByTestId('pep-pep_max_trades_per_day')).toHaveValue(1)
    expect(screen.getByTestId('pep-pep_fee_per_contract_usd')).toHaveValue(1)
    expect(screen.getByTestId('pep-pep_max_box_points')).toHaveValue(0)
    expect(screen.getByTestId('pep-pep_signal_max_age_min')).toHaveValue(2)
    // style A is the running style, so its tab is open
    expect(screen.getByTestId('pep-pep_a_box_minutes')).toHaveValue(5)
    expect(screen.getByTestId('pep-pep_a_sl_fraction')).toHaveValue(1)
    expect(screen.getByTestId('pep-pep_a_both_edges')).toHaveValue('no_trade')
  })

  it('the E tab shows the E numbers (2m box, half-box stop, EMA 9 on 2m, ema source)', () => {
    setup(undefined)
    fireEvent.click(screen.getByTestId('pep-tab-E'))
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

  it('opens on the style that RUNS', () => {
    setup({ pep_style: 'E' })
    expect(screen.getByTestId('pep-tab-E')).toHaveAttribute(
      'aria-selected',
      'true'
    )
    expect(screen.getByTestId('pep-pep_e_ema_period')).toBeInTheDocument()
  })

  // Mutant: onChange(f.set(block, n)) without fullPepBlock -> RED (a partial
  // block would let a stale stored number survive the server-side merge).
  it('an edit sends the WHOLE pep block with every leaf concrete', () => {
    const { onChange } = setup({ pep_contracts: 3 })
    commitNumber('pep-pep_contracts', '6')
    expect(onChange).toHaveBeenCalledTimes(1)
    const sent = onChange.mock.calls[0][0] as PepConfig
    expect(sent.pep_contracts).toBe(6)
    expect(sent).toEqual({ ...fullPepBlock(undefined), pep_contracts: 6 })
  })

  // Mutant: `const block = config ?? {}` (the stored partial block) -> RED.
  it('a stored PARTIAL block is completed with the shipped defaults in what is sent', () => {
    const { onChange } = setup({ pep_contracts: 3, style_a: {} })
    commitNumber('pep-pep_tick_size', '0.5')
    const sent = onChange.mock.calls[0][0] as PepConfig
    expect(sent.pep_flat_time_ct).toBe('15:00')
    expect(sent.style_e?.pep_e_ema_period).toBe(9)
    expect(sent.style_a?.pep_a_box_minutes).toBe(5)
  })

  it('clamps to the hard ceiling of 10 contracts on commit', () => {
    const { onChange } = setup()
    commitNumber('pep-pep_contracts', '25')
    expect((onChange.mock.calls[0][0] as PepConfig).pep_contracts).toBe(10)
  })

  it('an empty commit keeps the stored value, a 0 max-box stays a legal 0', () => {
    const { onChange } = setup({ pep_contracts: 4 })
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
  it('a stored lowercase style "e" shows as E (the server case-folds it), with the E tab open', () => {
    setup({ pep_style: 'e' })
    expect(screen.getByTestId('pep-pep_style')).toHaveValue('E')
    expect(screen.getByTestId('pep-pep_e_ema_period')).toBeInTheDocument()
  })

  it('the E signal source is a setting (ema | close)', () => {
    const { onChange } = setup({ pep_style: 'E' })
    fireEvent.change(screen.getByTestId('pep-pep_e_signal_source'), {
      target: { value: 'close' },
    })
    expect(
      (onChange.mock.calls[0][0] as PepConfig).style_e?.pep_e_signal_source
    ).toBe('close')
  })

  it('a bad HH:MM is refused with a message and writes nothing; a good one writes the block', () => {
    const { onChange } = setup()
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
    expect(screen.getByText(/Dipakai bersama kedua gaya/)).toBeInTheDocument()
    unmount()
    render(<PepModePanel onChange={() => {}} language="zh" />)
    expect(screen.getByText('两种风格共用')).toBeInTheDocument()
  })
})
