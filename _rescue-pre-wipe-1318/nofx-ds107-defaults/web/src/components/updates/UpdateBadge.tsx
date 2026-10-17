// W-ONE-BUTTON M5 (U3) — the update badge in the app header. Polls
// GET /api/updates every 60 s; Unknown on any error; never a spinner forever
// (the spinner exists only until the FIRST fetch settles, whichever way).

import { useEffect, useRef, useState } from 'react'
import { updatesApi, type UpdatesStatus } from '../../lib/api/updates'

export type BadgeState =
  | 'up-to-date'
  | 'update-available'
  | 'installing'
  | 'blocked'
  | 'unknown'

const POLL_MS = 60_000

/** Pure state mapping, pinned per state by its own test. A field the API does
 *  not affirm → unknown; the badge never derives an availability verdict. */
export function badgeFromStatus(s: UpdatesStatus | null): BadgeState {
  if (!s) return 'unknown'
  if (s.install_enabled === false) return 'blocked'
  if (s.install_state === 'installing') return 'installing'
  if (s.update_available === true) return 'update-available'
  if (s.update_available === false) return 'up-to-date'
  return 'unknown'
}

const LABELS: Record<BadgeState, string> = {
  'up-to-date': 'Up to date',
  'update-available': 'Update available',
  installing: 'Installing',
  blocked: 'Blocked',
  unknown: 'Unknown',
}

const COLORS: Record<BadgeState, string> = {
  'up-to-date': 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30',
  'update-available': 'bg-sky-500/15 text-sky-400 border-sky-500/30',
  installing: 'bg-amber-500/15 text-amber-400 border-amber-500/30',
  blocked: 'bg-red-500/15 text-red-400 border-red-500/30',
  unknown: 'bg-zinc-700/40 text-zinc-400 border-zinc-600/40',
}

export function UpdateBadge() {
  const [status, setStatus] = useState<UpdatesStatus | null>(null)
  const [settled, setSettled] = useState(false)
  const timer = useRef<number | null>(null)

  useEffect(() => {
    let alive = true
    const fetchOnce = async () => {
      // A spinner that can hang forever is a checklist class: the first fetch
      // races a 10 s cap, so the badge settles to Unknown no matter what.
      let s: UpdatesStatus | null = null
      try {
        s = await Promise.race([
          updatesApi.updatesStatus(),
          new Promise<null>((resolve) =>
            window.setTimeout(() => resolve(null), 10_000)
          ),
        ])
      } catch {
        s = null
      }
      if (!alive) return
      setStatus(s)
      setSettled(true)
    }
    fetchOnce()
    timer.current = window.setInterval(fetchOnce, POLL_MS)
    return () => {
      alive = false
      if (timer.current !== null) window.clearInterval(timer.current)
    }
  }, [])

  const state = badgeFromStatus(status)

  return (
    <span
      data-testid="update-badge"
      data-state={state}
      title={`Updates: ${LABELS[state]}`}
      className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-medium leading-4 ${COLORS[state]}`}
    >
      {!settled ? <span className="animate-pulse">…</span> : LABELS[state]}
    </span>
  )
}
