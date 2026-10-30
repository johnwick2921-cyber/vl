// PEP-TRUTH CARD — "Pep — what trades". The live pep state on the dashboard:
// the opening-range box, where the signal stands, trades today, stop/target and
// the time to the 15:00 CT flat. Renders NOTHING when pep is off (or unknown).
// It renders what /pep-truth sent: an absent box is "not drawn yet", never a
// box at 0; an absent countdown is not printed, never "0m".

import { pt, type PepKey } from '../../i18n/pep-translations'
import type { Language } from '../../i18n/translations'
import type { PepSignalState, PepTruth } from '../../lib/api/traders'

const fmtAsOf = (ms: number | undefined) =>
  ms !== undefined && ms > 0
    ? new Date(ms).toLocaleTimeString('en-US', {
        hour12: false,
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        timeZone: 'America/Chicago',
      })
    : '—'

// The CT calendar date ("2006-01-02", the shape /pep-truth's `day` uses) of an
// instant, and its short weekday for the "last ran" text.
const ctDay = (ms: number) =>
  new Intl.DateTimeFormat('en-CA', {
    timeZone: 'America/Chicago',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(new Date(ms))
const ctWeekday = (ms: number) =>
  new Date(ms).toLocaleDateString('en-US', {
    weekday: 'short',
    timeZone: 'America/Chicago',
  })

// D1 (verify 3:6): a time-only "as of 15:59:00 CT" reads as today. When the
// last evaluation is on an EARLIER CT day than the day the server is reporting
// (truth.day), the header names that day instead - the body of such a card says
// "no evaluation yet", and the header must not contradict it.
export function lastEvaluatedIsEarlierDay(
  ms: number | undefined,
  day: string | undefined
): boolean {
  return ms !== undefined && ms > 0 && !!day && ctDay(ms) < day
}

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

// Evaluator state word (trader/pep_truth.go pepSignalWord) -> i18n key.
export const SIGNAL_KEYS: Record<PepSignalState, PepKey> = {
  no_data: 'sigNoData',
  waiting_box: 'sigWaitingBox',
  waiting_signal: 'sigWaitingSignal',
  signal: 'sigSignal',
  blocked: 'sigBlocked',
  traded: 'sigTraded',
  position_open: 'sigPositionOpen',
  after_flat: 'sigAfterFlat',
  flat_pending: 'sigFlatPending',
}
const SIDE_STATES = new Set<PepSignalState>([
  'signal',
  'traded',
  'position_open',
  'flat_pending',
])

// The server has two conflict codes (store/pep.go PepConflictMentor /
// PepConflictDayPlan). The headline names the modes that are really both ON; an
// unknown (newer) code gets the generic text, never the mentor wording.
export function conflictKey(code: string | undefined): PepKey {
  if (code === 'pep_mode_conflict_mentor') return 'truthConflictMentor'
  if (code === 'pep_mode_conflict_day_plan') return 'truthConflictDayPlan'
  return 'truthConflictOther'
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
  if (!truth) return null
  const lang = language as Language
  if (!truth.enabled) {
    // A mentor+pep conflict is the one OFF state that must be loud: the server
    // refused to start the trader, so nothing trades and nothing else says why.
    if (truth.mode === 'conflict' || truth.conflict)
      return (
        <div
          data-testid="pep-truth-conflict"
          role="alert"
          className="rounded-lg border p-3 text-xs"
          style={{ borderColor: 'var(--vl-short)', color: 'var(--vl-short)' }}
        >
          {pt(conflictKey(truth.conflict?.code), lang)}
          {truth.conflict?.message ? ` — ${truth.conflict.message}` : ''}
        </div>
      )
    return null
  }
  const box = truth.box
  const sig = truth.signal
  const block = (truth.refusals ?? []).find((r) => r.severity === 'block')
  const wait = (truth.refusals ?? []).find((r) => r.severity === 'wait')
  const refusal = block ?? wait

  // The server's one-word evaluator state -> human text. An unknown word (a
  // newer server) prints as itself, never as a guess.
  // Style E waits for an EMA cross, not a "break": the word names the real wait.
  const sigKey = sig
    ? sig === 'waiting_signal' && truth.style === 'E'
      ? 'sigWaitingSignalE'
      : SIGNAL_KEYS[sig as PepSignalState]
    : undefined
  const sigWord = sig ? (sigKey ? pt(sigKey, lang) : sig) : '—'
  const sideShown =
    truth.side && SIDE_STATES.has(sig as PepSignalState)
      ? ` · ${truth.side.toUpperCase()}`
      : ''
  const sigText = `${sigWord}${sideShown}`

  let flat = '—'
  if (truth.flat_time_ct) {
    flat = truth.flat_time_ct + ' CT'
    if (truth.flat_countdown_sec !== undefined)
      flat +=
        truth.flat_countdown_sec > 0
          ? ` · ${pt('truthFlatIn', lang, { t: fmtCountdown(truth.flat_countdown_sec) })}`
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
          {truth.last_evaluated_ms
            ? lastEvaluatedIsEarlierDay(truth.last_evaluated_ms, truth.day)
              ? pt('truthLastRan', lang, {
                  d: `${ctWeekday(truth.last_evaluated_ms)} ${ctDay(truth.last_evaluated_ms)}`,
                  t: fmtAsOf(truth.last_evaluated_ms),
                })
              : pt('truthAsOf', lang, { t: fmtAsOf(truth.last_evaluated_ms) })
            : pt('truthNotEvaluated', lang)}
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
