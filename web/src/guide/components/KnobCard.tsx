// KnobCard — renders ALL TEN mandatory fields of a KnobSpec. The content test
// asserts every field is non-empty, so each slot below is always populated.
import type { KnobSpec } from '../types'

const label = {
  color: 'var(--vl-faint)',
  fontFamily: 'var(--vl-font-ui)',
} as const

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div className="text-[11px] leading-snug">
      <span className="mr-1 font-bold uppercase tracking-wider" style={label}>
        {k}
      </span>
      <span style={{ color: 'var(--vl-ivory)' }}>{v}</span>
    </div>
  )
}

export function KnobCard({ knob }: { knob: KnobSpec }) {
  return (
    <article
      data-testid="guide-knob"
      className="rounded-xl p-3 flex flex-col gap-1.5"
      style={{
        background: 'var(--vl-card)',
        border: '1px solid var(--vl-hair)',
      }}
    >
      <header className="flex items-start justify-between gap-2">
        <span className="text-sm font-bold" style={{ color: 'var(--vl-gold)' }}>
          {knob.label}
        </span>
        {knob.recommended.startsWith('⭐') && (
          <span
            className="shrink-0 text-[10px] uppercase tracking-wider"
            style={{ color: 'var(--vl-gold)', fontFamily: 'var(--vl-font-ui)' }}
          >
            ⭐ recommended
          </span>
        )}
      </header>
      <p className="text-[12px]" style={{ color: 'var(--vl-ivory)' }}>
        {knob.what}
      </p>
      <p className="text-[11px]" style={{ color: 'var(--vl-muted)' }}>
        {knob.trader}
      </p>
      <div
        className="flex flex-col gap-0.5 rounded-lg p-2"
        style={{ background: 'var(--vl-card2)' }}
      >
        {knob.settingId && <Row k="setting id" v={knob.settingId} />}
        {knob.live !== undefined && (
          <Row k="live" v={knob.live ? 'yes' : 'no'} />
        )}
        <Row k="where" v={knob.where} />
        <Row k="range" v={knob.range} />
        <Row k="default" v={knob.systemDefault} />
        <Row k="per-session" v={knob.perSession} />
        <Row k="engine" v={knob.consumer} />
      </div>
      <p className="text-[11px]" style={{ color: 'var(--vl-ivory)' }}>
        {knob.recommended}
      </p>
      <p className="text-[11px]" style={{ color: 'var(--vl-muted)' }}>
        <span className="font-bold uppercase tracking-wider" style={label}>
          touch it when ·{' '}
        </span>
        {knob.whenToTouch}
      </p>
    </article>
  )
}
