// MENTOR-TRUTH PANEL (release #10) — shared live-refresh hook.
//
// The "what trades" truth changes every 1m tick, so a once-per-mount fetch
// would show stale truth (the exact class this wave exists to fix). This hook
// refreshes on a 30s interval AND on window focus; the server stamps each
// payload with `as_of_ms` so the card can show "as of HH:MM:SS CT".
//
// A failed refresh must NOT blank the card (and must not flip the bias card's
// "advice only" label): the hook keeps the last good payload and marks it
// stale. Only a successful payload with enabled=false hides the card.

import { useEffect, useState } from 'react'
import { traderApi, type MentorTruth } from '../../lib/api/traders'

export interface MentorTruthState {
  truth: MentorTruth | null
  /** true when the most recent refresh failed and `truth` is the last good one. */
  stale: boolean
}

export function useMentorTruth(traderId: string | undefined): MentorTruthState {
  const [state, setState] = useState<MentorTruthState>({
    truth: null,
    stale: false,
  })

  useEffect(() => {
    if (!traderId) {
      setState({ truth: null, stale: false })
      return
    }

    let alive = true
    const load = () => {
      traderApi
        .getMentorTruth(traderId)
        .then((t) => {
          if (alive) setState({ truth: t, stale: false })
        })
        .catch(() => {
          // Keep the last good payload; only mark it stale.
          if (alive)
            setState((prev) =>
              prev.truth ? { truth: prev.truth, stale: true } : prev
            )
        })
    }

    load()
    const interval = setInterval(load, 30_000)
    const onFocus = () => {
      if (!document.hidden) load()
    }
    window.addEventListener('focus', onFocus)
    const onVis = () => {
      if (!document.hidden) load()
    }
    document.addEventListener('visibilitychange', onVis)

    return () => {
      alive = false
      clearInterval(interval)
      window.removeEventListener('focus', onFocus)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [traderId])

  return state
}
