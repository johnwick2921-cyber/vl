// MENTOR-TRUTH PANEL (release #10) — shared live-refresh hook.
//
// The "what trades" truth changes every 1m tick, so a once-per-mount fetch
// would show stale truth (the exact class this wave exists to fix). This hook
// refreshes on a 30s interval AND on window focus; the server stamps each
// payload with `as_of_ms` so the card can show "as of HH:MM:SS CT".

import { useEffect, useState } from 'react'
import { traderApi, type MentorTruth } from '../../lib/api/traders'

export function useMentorTruth(
  traderId: string | undefined
): MentorTruth | null {
  const [truth, setTruth] = useState<MentorTruth | null>(null)

  useEffect(() => {
    if (!traderId) {
      setTruth(null)
      return
    }

    let alive = true
    const load = () => {
      traderApi
        .getMentorTruth(traderId)
        .then((t) => {
          if (alive) setTruth(t)
        })
        .catch(() => {
          if (alive) setTruth(null)
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

  return truth
}
