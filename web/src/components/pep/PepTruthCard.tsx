// PEP-TRUTH CARD — "Pep — what trades". The live pep state on the dashboard:
// the opening-range box, where the signal stands, trades today, stop/target and
// the time to the 15:00 CT flat. Renders NOTHING when pep is off (or unknown).
// It renders what /pep-truth sent: an absent box is "not drawn yet", never a
// box at 0; an absent countdown is not printed, never "0m".

import { pt } from '../../i18n/pep-translations'
import type { Language } from '../../i18n/translations'
import type { PepTruth } from '../../lib/api/traders'

const fmtAsOf = (ms: number) =>
  ms > 0
    ? new Date(ms).toLocaleTimeString('en-US', {
        hour12: false,
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        timeZone: 'America/Chicago',
      })
    : '—'

const px = (n: number | undefined) =>
  n === undefined || !(n > 0)
    ? '—'
    : Number.isInteger(n)
      ? String(n)
      : n.toFixed(2)

export function fmtCountdown(sec: number): string {
  const s = Math.max(0, Math.floor(sec))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  return h > 0 ? `${h}h ${String(m).padStart(2, '0')}m` : `${m}m`
}

export function PepTruthCard({
  truth,
  stale = false,
  language,
}: {
  truth: PepTruth | null
  stale?: boolean
  language: string
}) {
  if (!truth || !truth.enabled) return null
  const lang = language as Language
  const box = truth.box
  const sig = truth.signal
  const block = (truth.refusals ?? []).find((r) => r.severity === 'block')
  const wait = (truth.refusals ?? []).find((r) => r.severity === 'wait')
  const refusal = block ?? wait

  const sigText =
    sig?.state === 'entered'
      ? `${pt('truthSignalEntered', lang)}${sig.side ? ` · ${sig.side.toUpperCase()}` : ''}`
      : sig?.state === 'blocked'
        ? pt('truthSignalBlocked', lang)
        : sig?.state === 'waiting'
          ? pt('truthSignalWaiting', lang)
          : '—'

  let flat = '—'
  if (truth.flat_time_ct) {
    flat = truth.flat_time_ct + ' CT'
    if (truth.flat_in_sec !== undefined)
      flat +=
        truth.flat_in_sec > 0
          ? ` · ${pt('truthFlatIn', lang, { t: fmtCountdown(truth.flat_in_sec) })}`
          : ` · ${pt('truthFlatDone', lang)}`
  }

  const cell = 'flex flex-col'
  const k = { color: 'var(--vl-faint)' }
  return (
    <div
      data-testid="pep-truth-card"
      className="flex flex-col gap-2 rounded-lg border p-3"
      style={{
        borderColor: 'var(--vl-border)',
        background: 'var(--vl-panel)',
        fontFamily: 'var(--vl-font-ui)',
      }}
    >
      <div className="flex w-full items-center justify-between">
        <span
          className="text-[10px] uppercase tracking-widest"
          style={{ color: 'var(--vl-warn)' }}
        >
          {pt('truthTitle', lang)} · {truth.style ?? '?'} ·{' '}
          {pt('truthSim', lang)}
        </span>
        <span className="text-[10px]" style={{ color: 'var(--vl-faint)' }}>
          {pt('truthAsOf', lang, { t: fmtAsOf(truth.as_of_ms) })}
        </span>
      </div>
      {stale && (
        <div
          className="text-[10px]"
          style={{ color: 'var(--vl-warn)' }}
          data-testid="pep-truth-stale"
        >
          {pt('truthStale', lang)}
        </div>
      )}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-2 text-xs">
        <div className={cell} data-testid="pep-truth-box">
          <span className="text-[10px]" style={k}>
            {pt('truthBox', lang)}
          </span>
          {box && box.drawn ? (
            <span className="font-mono">
              {pt('truthBoxHigh', lang)} {px(box.high)} ·{' '}
              {pt('truthBoxLow', lang)} {px(box.low)} ·{' '}
              {pt('truthBoxSize', lang)} {px(box.size)}
            </span>
          ) : (
            <span data-testid="pep-truth-box-undrawn">
              {pt('truthBoxNotDrawn', lang)}
            </span>
          )}
        </div>
        <div className={cell} data-testid="pep-truth-signal">
          <span className="text-[10px]" style={k}>
            {pt('truthSignal', lang)}
          </span>
          <span>{sigText}</span>
          {refusal && (
            <span
              className="text-[10px] font-mono"
              style={{
                color:
                  refusal.severity === 'block'
                    ? 'var(--vl-short)'
                    : 'var(--vl-faint)',
              }}
              title={refusal.detail}
              data-testid="pep-truth-refusal"
            >
              {pt('truthRefusal', lang)}: {refusal.code}
            </span>
          )}
        </div>
        <div className={cell} data-testid="pep-truth-trades">
          <span className="text-[10px]" style={k}>
            {pt('truthTrades', lang)}
          </span>
          <span className="font-mono">
            {truth.trades_today ?? '—'}
            {truth.max_trades_per_day !== undefined
              ? ` / ${truth.max_trades_per_day}`
              : ''}
            {truth.position_open ? ` · ${pt('truthPosition', lang)}` : ''}
          </span>
        </div>
        <div className={cell} data-testid="pep-truth-flat">
          <span className="text-[10px]" style={k}>
            {pt('truthFlat', lang)}
          </span>
          <span className="font-mono">{flat}</span>
        </div>
        {(truth.stop !== undefined || truth.target !== undefined) && (
          <div className={cell} data-testid="pep-truth-stoptarget">
            <span className="text-[10px]" style={k}>
              {pt('truthStopTarget', lang)}
            </span>
            <span className="font-mono">
              {px(truth.stop)} / {px(truth.target)}
            </span>
          </div>
        )}
      </div>
    </div>
  )
}
