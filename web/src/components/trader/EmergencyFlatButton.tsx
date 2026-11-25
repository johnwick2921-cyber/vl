// Plan 4 Task 23 — EmergencyFlatButton
//
// Red "Emergency Flat" button + confirmation modal that POSTs to
// /api/risk/force-flat?trader_id=X. Used on TraderDashboardPage at the
// top of the actions area.
//
// Test-ids (mandatory — Playwright runbook selects by them):
//   data-testid="emergency-flat-button"
//   data-testid="confirm-flat-modal"
//   data-testid="confirm-flat-button"
//   data-testid="cancel-flat-button"

// m443/N6 — the result is GREEN only when the server answered 2xx AND said
// triggered === true. Everything else (HTTP 200 {triggered:false} because NT8
// is down, 401 on an expired token, a non-JSON proxy page, a network error) is
// RED "NOT SENT: <reason>" — in a panic a flatten that never left must never
// look like one that did.

import { useState } from 'react'

interface Props {
  traderId: string
}

interface FlatResult {
  sent: boolean
  text: string
}

// notSent builds the red result line.
function notSent(reason: string): FlatResult {
  return { sent: false, text: `NOT SENT: ${reason}` }
}

// interpretForceFlat reads the /api/risk/force-flat answer. body is the parsed
// JSON, or null when the body was not JSON.
export function interpretForceFlat(
  status: number,
  ok: boolean,
  body: unknown
): FlatResult {
  const b =
    body && typeof body === 'object' ? (body as Record<string, unknown>) : null
  if (ok && b && b.triggered === true) {
    return { sent: true, text: JSON.stringify(b, null, 2) }
  }
  const field = (k: string) =>
    b && typeof b[k] === 'string' && (b[k] as string).trim()
      ? (b[k] as string).trim()
      : ''
  let reason = field('reason') || field('error') || field('message')
  if (status === 401) {
    reason = `HTTP 401 — session expired, log in again${reason ? ` (${reason})` : ''}`
  } else if (!ok) {
    reason = `HTTP ${status}${reason ? ` — ${reason}` : ''}`
  } else if (!reason) {
    // 2xx but no triggered:true and no stated reason: still not confirmed sent.
    reason = 'the server did not confirm the flatten was sent'
  }
  return notSent(reason)
}

export function EmergencyFlatButton({ traderId }: Props) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<FlatResult | null>(null)

  async function confirmFlat() {
    setBusy(true)
    setResult(null)
    try {
      const token = localStorage.getItem('auth_token') ?? ''
      const r = await fetch(
        `/api/risk/force-flat?trader_id=${encodeURIComponent(traderId)}`,
        {
          method: 'POST',
          headers: {
            Authorization: `Bearer ${token}`,
            'Content-Type': 'application/json',
          },
        }
      )
      let data: unknown = null
      try {
        data = await r.json()
      } catch {
        data = null // non-JSON body (proxy error page etc.)
      }
      setResult(interpretForceFlat(r.status, r.ok, data))
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e)
      setResult(notSent(`network error — ${msg}`))
    } finally {
      setBusy(false)
    }
  }

  function closeModal() {
    if (busy) return
    setOpen(false)
    setResult(null)
  }

  return (
    <>
      <button
        data-testid="emergency-flat-button"
        type="button"
        onClick={() => setOpen(true)}
        className="bg-red-600 hover:bg-red-700 active:bg-red-800 text-white font-bold py-2 px-4 rounded-lg transition-colors shadow-[0_0_12px_rgba(220,38,38,0.4)]"
        title="Emergency flat this trader's position (one market flatten to NinjaTrader)"
      >
        Emergency Flat
      </button>

      {open && (
        <div
          role="dialog"
          aria-modal="true"
          data-testid="confirm-flat-modal"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm"
          onClick={closeModal}
        >
          <div
            className="vl-neo-glass max-w-md w-full mx-4 rounded-lg p-6 border border-red-600/40"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 className="text-xl font-bold text-red-400 mb-3">
              Confirm Emergency Flat
            </h2>
            <p className="text-sm text-vl-neo-text-muted mb-4">
              This sends ONE market flatten to NinjaTrader for the bound account
              of trader{' '}
              <code className="font-mono text-vl-neo-gold">{traderId}</code>{' '}
              only — no other account is touched. On that account NinjaTrader
              closes the MNQ position AND cancels every working MNQ order,
              resting entry orders included. It also clears this trader&apos;s
              daily-loss lock (if its loss still stands, the lock returns on the
              next 60-second check). It does not pause the trader — use Pause to
              stop new entries. Cannot be undone.
            </p>

            {result && (
              <pre
                data-testid="emergency-flat-result"
                data-sent={result.sent ? 'true' : 'false'}
                role={result.sent ? 'status' : 'alert'}
                className={`text-xs bg-black/50 p-3 rounded mb-4 overflow-auto max-h-40 font-mono whitespace-pre-wrap ${
                  result.sent
                    ? 'text-green-300'
                    : 'text-red-400 border border-red-600 font-bold'
                }`}
              >
                {result.text}
              </pre>
            )}

            <div className="flex gap-2 justify-end">
              <button
                data-testid="cancel-flat-button"
                type="button"
                disabled={busy}
                onClick={closeModal}
                className="bg-gray-600 hover:bg-gray-700 disabled:opacity-50 text-white py-2 px-4 rounded transition-colors"
              >
                Cancel
              </button>
              <button
                data-testid="confirm-flat-button"
                type="button"
                disabled={busy}
                onClick={confirmFlat}
                className="bg-red-600 hover:bg-red-700 active:bg-red-800 disabled:opacity-50 text-white font-bold py-2 px-4 rounded transition-colors"
              >
                {busy ? 'Flattening...' : 'Confirm'}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
