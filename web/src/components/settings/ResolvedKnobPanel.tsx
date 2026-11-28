// Settings → what this build actually honours.
//
// The panel renders the registry's OWN labels. It does not derive a status from
// a field name and it does not invent wording: if a knob's status changes, the
// text here changes with it, because the text arrives with the data.
//
// Two labels, two different findings, neither of which says "dead":
//   ineffective            read; does not take effect (reason)
//   candidate-unverified   no known reader — pending verification
//   folded                 folded — no control; stored value honoured (reason)

import { useEffect, useState } from 'react'

interface ResolvedField {
  path: string
  saved: string
  resolved: string
  source: string
  line: string
}

interface Knob {
  path: string
  status: string
  ui_label: string
  consumers: string[]
  dual_level: boolean
  clamp?: string
  note?: string
}

// A count the server did not compute is ABSENT, never 0 (L7): schema is absent
// (with schema_error) if the enumeration failed, and env_shadows /
// env_shadow_paths are absent while nothing counts env shadows.
interface Summary {
  schema?: number
  schema_error?: string
  classified: number
  live: number
  ineffective: number
  candidate_unverified: number
  suspended: number
  advisory: number
  display_only: number
  infra: number
  folded: number
  env_shadows?: number
  env_shadow_paths?: string[]
}

interface Payload {
  summary: Summary
  knobs: Knob[]
  // Absent when no trader was named. Absent and empty are different answers.
  resolved?: ResolvedField[]
}

const STATUS_TONE: Record<string, string> = {
  live: 'text-emerald-400',
  ineffective: 'text-amber-400',
  'candidate-unverified': 'text-slate-400',
  advisory: 'text-sky-400',
  folded: 'text-violet-400',
  suspended: 'text-slate-500',
  infra: 'text-slate-500',
  'display-only': 'text-slate-500',
}

export function ResolvedKnobPanel({
  traderId,
  session,
}: {
  traderId?: string
  session?: string
}) {
  const [data, setData] = useState<Payload | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let live = true
    const params = new URLSearchParams()
    if (traderId) params.set('trader_id', traderId)
    if (session) params.set('session', session)
    const qs = params.toString()
    const token = localStorage.getItem('auth_token') ?? ''

    fetch(`/api/config/resolved${qs ? `?${qs}` : ''}`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
      .then((r) => r.json())
      .then((j) => live && setData(j as Payload))
      .catch(() => live && setError('could not read the registry'))
    return () => {
      live = false
    }
  }, [traderId, session])

  if (error) {
    return (
      <div data-testid="resolved-panel" className="text-sm text-red-400">
        {error}
      </div>
    )
  }
  if (!data) {
    return (
      <div data-testid="resolved-panel" className="text-sm text-slate-400">
        reading the registry…
      </div>
    )
  }

  const s = data.summary
  // Only the statuses that need explaining are listed; a live knob's behaviour
  // is the knob itself.
  const needsLabel = (data.knobs ?? []).filter((k) => k.ui_label !== '')

  return (
    <div data-testid="resolved-panel" className="space-y-4">
      <div className="text-xs text-slate-400">
        schema{' '}
        {typeof s.schema === 'number'
          ? s.schema
          : `n/a (${s.schema_error ?? 'not computed'})`}{' '}
        · classified {s.classified} · live {s.live} · ineffective{' '}
        {s.ineffective} · candidate {s.candidate_unverified} · folded{' '}
        {s.folded ?? 0}
        {typeof s.env_shadows === 'number'
          ? ` · env-shadows ${s.env_shadows}`
          : ' · env-shadows n/a (not counted)'}
      </div>

      {/* Rendered only when the server actually resolved something. An absent
          list is not an empty one, and must not read as "nothing resolves". */}
      {data.resolved && data.resolved.length > 0 && (
        <div data-testid="resolved-section" className="space-y-1">
          <div className="text-sm font-medium text-slate-200">
            saved → resolved · source
          </div>
          {data.resolved.map((f) => (
            <div
              key={f.path}
              data-testid="resolved-line"
              className="flex flex-wrap gap-x-2 font-mono text-xs text-slate-300"
            >
              <span className="text-slate-400">{f.path}</span>
              <span>{f.line}</span>
            </div>
          ))}
        </div>
      )}

      <div className="space-y-1">
        {needsLabel.map((k) => (
          <div
            key={k.path}
            data-testid="knob-row"
            className="flex flex-wrap gap-x-2 text-xs"
          >
            <span className="font-mono text-slate-300 min-w-0 break-all">
              {k.path}
            </span>
            <span className={STATUS_TONE[k.status] ?? 'text-slate-400'}>
              {k.ui_label}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}
