// PEP x Day Plan (review round 1, defect 4): the server refuses pep together
// with the day plan; the selector already blocks Pep while the plan is ON, and
// this pins the OTHER direction - with Pep ON the plan cannot be turned ON from
// the Day Plan editor, a warning says why BEFORE Save, and OFF always works.
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { DayPlanEditor } from './DayPlanEditor'

afterEach(cleanup)

describe('DayPlanEditor with Pep ON', () => {
  // Mutant: drop the `if (v && pepOn) return` guard -> the plan turns ON -> RED.
  it('refuses to turn the plan ON and says why', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{ plan_enabled: false }}
        onChange={onChange}
        language="en"
        pepOn
      />
    )
    expect(screen.getByTestId('dayplan-pep-warning')).toHaveTextContent(
      'cannot be turned on together with Pep'
    )
    fireEvent.click(screen.getAllByRole('switch')[0])
    expect(onChange).not.toHaveBeenCalled()
  })

  // Mutant: block OFF as well -> a loaded both-ON state cannot be repaired -> RED.
  it('a loaded both-ON state can be repaired: the plan turns OFF and the warning names the conflict', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{ plan_enabled: true }}
        onChange={onChange}
        language="en"
        pepOn
      />
    )
    expect(screen.getByTestId('dayplan-pep-warning')).toHaveTextContent(
      'both ON'
    )
    fireEvent.click(screen.getAllByRole('switch')[0])
    expect(onChange).toHaveBeenCalledTimes(1)
    expect(onChange.mock.calls[0][0].plan_enabled).toBe(false)
  })

  it('without Pep nothing changes: no warning, the plan turns ON as before', () => {
    const onChange = vi.fn()
    render(
      <DayPlanEditor
        config={{ plan_enabled: false }}
        onChange={onChange}
        language="en"
      />
    )
    expect(screen.queryByTestId('dayplan-pep-warning')).toBeNull()
    fireEvent.click(screen.getAllByRole('switch')[0])
    expect(onChange.mock.calls[0][0].plan_enabled).toBe(true)
  })

  it('the warning reads in zh and id', () => {
    const a = render(
      <DayPlanEditor
        config={{ plan_enabled: false }}
        onChange={vi.fn()}
        language="zh"
        pepOn
      />
    )
    expect(a.container).toHaveTextContent('不能与 Pep 同时开启')
    a.unmount()
    const b = render(
      <DayPlanEditor
        config={{ plan_enabled: false }}
        onChange={vi.fn()}
        language="id"
        pepOn
      />
    )
    expect(b.container).toHaveTextContent('tidak bisa diaktifkan bersama Pep')
  })
})
