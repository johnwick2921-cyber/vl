// MENTOR-TRUTH PANEL (release #10) — "Mentor — what trades".
//
// The AI planner's bias is ADVICE; in mentor mode the mentor evaluator places
// the trades. This card surfaces the LIVE evaluator truth the planner page was
// missing: the 4h/1h/5m trigger directions, the HTF gate verdict, the mentor
// key levels in effect (with today's visits), the history depth, and the
// window / day-stop state. It renders nothing when mentor mode is OFF.

import { useState } from 'react'
import type { MentorTruth } from '../../lib/api/traders'

// MENTOR-CARD-EXPAND (release #12): the owner's last choice persists per
// browser under this key. Read/write are try/catch-guarded — a storage
// failure (private mode, quota) must fall back to EXPANDED, never crash.
const STORAGE_KEY = 'vl.mentorCard.open'

function readStoredOpen(): boolean {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    return v !== 'false' // absent or anything but "false" → expanded
  } catch {
    return true
  }
}

function writeStoredOpen(open: boolean): void {
  try {
    localStorage.setItem(STORAGE_KEY, String(open))
  } catch {
    // ignore — persistence is best-effort
  }
}

const fmtTime = (ms: number) =>
  ms > 0
    ? new Date(ms).toLocaleString('en-US', {
        hour12: false,
        month: 'short',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      })
    : '—'

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

function dirWord(dir: string): string {
  if (dir === 'long') return 'LONG'
  if (dir === 'short') return 'SHORT'
  return '—'
}

function dirColor(dir: string): string {
  if (dir === 'long') return 'var(--vl-long)'
  if (dir === 'short') return 'var(--vl-short)'
  return 'var(--vl-muted)'
}

export function MentorTruthCard({
  truth,
  stale = false,
}: {
  truth: MentorTruth | null
  stale?: boolean
}) {
  const [open, setOpen] = useState<boolean>(readStoredOpen)
  const toggle = () => {
    const next = !open
    writeStoredOpen(next)
    setOpen(next)
  }
  if (!truth || !truth.enabled) return null

  return (
    <div
      className="flex flex-col gap-2 rounded-lg border p-3"
      style={{
        borderColor: 'var(--vl-border)',
        background: 'var(--vl-panel)',
        fontFamily: 'var(--vl-font-ui)',
      }}
    >
      <button
        type="button"
        aria-expanded={open}
        onClick={toggle}
        className="flex w-full items-center justify-between text-left"
        style={{
          background: 'none',
          border: 'none',
          padding: 0,
          cursor: 'pointer',
          font: 'inherit',
        }}
      >
        <span className="flex items-center gap-1.5">
          <span aria-hidden style={{ width: '0.75em', flex: 'none' }}>
            {open ? '▾' : '▸'}
          </span>
          <span
            className="text-[10px] uppercase tracking-widest"
            style={{ color: 'var(--vl-warn)' }}
          >
            Mentor — what trades
          </span>
        </span>
        <span className="text-[10px]" style={{ color: 'var(--vl-faint)' }}>
          {truth.htf.gate_active ? 'HTF gate ON' : 'HTF gate OFF'} · as of{' '}
          {fmtAsOf(truth.as_of_ms)} CT
        </span>
      </button>

      {stale && (
        <div
          className="text-[10px] font-semibold"
          style={{ color: 'var(--vl-warn)' }}
        >
          stale — last update {fmtAsOf(truth.as_of_ms)} CT
        </div>
      )}

      {open && (
        <>
      {/* trigger directions */}
      <div className="flex gap-4 text-[11px]">
        {[
          ['4h', truth.htf.four_h_dir, truth.htf.four_h_since],
          ['1h', truth.htf.one_h_dir, truth.htf.one_h_since],
          ['5m', truth.trigger_5m.dir, truth.trigger_5m.since],
        ].map(([tf, dir, since]) => (
          <div key={tf as string} className="flex flex-col gap-0.5">
            <span style={{ color: 'var(--vl-faint)' }}>{tf}</span>
            <span
              className="text-[12px] font-semibold"
              style={{ color: dirColor(dir as string) }}
            >
              {dirWord(dir as string)}
            </span>
            <span
              className="vl-num text-[10px]"
              style={{ color: 'var(--vl-muted)' }}
            >
              {fmtTime(since as number)}
            </span>
          </div>
        ))}
      </div>

      {/* HTF verdict */}
      <div className="text-[11px]">
        <span style={{ color: 'var(--vl-faint)' }}>HTF verdict: </span>
        <span
          className="font-semibold"
          style={{
            color:
              truth.htf.verdict === 'follow'
                ? 'var(--vl-long)'
                : truth.htf.verdict === 'sit-out'
                  ? 'var(--vl-short)'
                  : 'var(--vl-muted)',
          }}
        >
          {truth.htf.verdict}
          {truth.htf.verdict_side ? ` ${dirWord(truth.htf.verdict_side)}` : ''}
        </span>
        {truth.htf.verdict_why && (
          <div className="text-[10px]" style={{ color: 'var(--vl-faint)' }}>
            {truth.htf.verdict_why}
          </div>
        )}
      </div>

      {/* mentor key levels (distinct style: the levels that actually trade).
          levels is ABSENT while the evaluator computes its first bar, and `[]`
          once it has built with nothing drawn — never dereference a null. */}
      {truth.computing ? (
        <div className="text-[11px]" style={{ color: 'var(--vl-muted)' }}>
          levels: computing (first 1m bar)
        </div>
      ) : (truth.levels ?? []).length > 0 ? (
        <div className="flex flex-col gap-0.5">
          <span
            className="text-[10px] uppercase tracking-widest"
            style={{ color: 'var(--vl-faint)' }}
          >
            Key levels in effect
          </span>
          {(truth.levels ?? []).map((l) => (
            <div
              key={l.key}
              className="flex items-baseline justify-between text-[11px]"
            >
              <span style={{ color: 'var(--vl-muted)' }}>{l.kind}</span>
              <span className="vl-num" style={{ color: 'var(--vl-warn)' }}>
                {l.price.toFixed(2)}
              </span>
              <span
                className="vl-num"
                style={{ color: 'var(--vl-faint)' }}
                title="drawn at"
              >
                {fmtTime(l.drawn_at)}
              </span>
              <span className="vl-num" style={{ color: 'var(--vl-faint)' }}>
                {l.visits_today} visits
              </span>
            </div>
          ))}
        </div>
      ) : (
        <div className="text-[11px]" style={{ color: 'var(--vl-muted)' }}>
          no levels
        </div>
      )}

      {/* history depth */}
      {(truth.depth_line || Object.keys(truth.depth ?? {}).length > 0) && (
        <div className="text-[10px]" style={{ color: 'var(--vl-faint)' }}>
          {truth.depth_line ||
            Object.entries(truth.depth ?? {})
              .map(([k, v]) => `${k}=${v}`)
              .join(' · ')}
        </div>
      )}

      {/* window + day-stop */}
      <div
        className="flex flex-wrap gap-x-3 gap-y-0.5 text-[10px]"
        style={{ color: 'var(--vl-muted)' }}
      >
        <span>
          window {truth.window.start}/{truth.window.minutes}:
          {truth.window.active
            ? ' open'
            : truth.window.ended
              ? ' ended'
              : ' disabled'}
        </span>
        {truth.done_after_win && (
          <span style={{ color: 'var(--vl-warn)' }}>done-after-win</span>
        )}
        {truth.stop_after_loss && (
          <span style={{ color: 'var(--vl-warn)' }}>stop-after-loss</span>
        )}
      </div>
        </>
      )}
    </div>
  )
}
