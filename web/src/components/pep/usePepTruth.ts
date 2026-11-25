// PEP-TRUTH CARD — shared live-refresh hook. Same contract as useMentorTruth:
// 30s interval + focus/visibility refresh; a failed refresh keeps the last good
// payload and marks it stale; a trader switch resets before anything else (the
// previous trader's box must never show on the next one). A server without the
// endpoint (404) never produces a payload, so the card stays hidden.

import { useEffect, useState } from 'react'
import { traderApi, type PepTruth } from '../../lib/api/traders'

export interface PepTruthState {
  truth: PepTruth | null
  /** true when the most recent refresh failed and `truth` is the last good one. */
  stale: boolean
  /** 'loading' until the first answer; 'ok' once a payload arrived; 'error' when
   *  no payload ever arrived (older server, 404, network). The mode chip needs
   *  the difference: "pep is off" and "pep is unknown" are different claims. */
  status: 'loading' | 'ok' | 'error'
}

export function usePepTruth(traderId: string | undefined): PepTruthState {
  const [state, setState] = useState<PepTruthState>({
    truth: null,
    stale: false,
    status: 'loading',
  })

  useEffect(() => {
    setState({ truth: null, stale: false, status: 'loading' })
    if (!traderId) return

    let alive = true
    const load = () => {
      traderApi
        .getPepTruth(traderId)
        .then((t) => {
          if (alive) setState({ truth: t, stale: false, status: 'ok' })
        })
        .catch(() => {
          if (alive)
            setState((prev) =>
              prev.truth
                ? { truth: prev.truth, stale: true, status: 'ok' }
                : { truth: null, stale: false, status: 'error' }
            )
        })
    }

    load()
    const interval = setInterval(load, 30_000)
    const onFocus = () => {
      if (!document.hidden) load()
    }
    window.addEventListener('focus', onFocus)
    document.addEventListener('visibilitychange', onFocus)

    return () => {
      alive = false
      clearInterval(interval)
      window.removeEventListener('focus', onFocus)
      document.removeEventListener('visibilitychange', onFocus)
    }
  }, [traderId])

  return state
}
