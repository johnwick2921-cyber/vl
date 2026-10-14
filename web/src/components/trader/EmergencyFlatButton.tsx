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

import { useState } from 'react'

interface Props {
  traderId: string
}

export function EmergencyFlatButton({ traderId }: Props) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<string | null>(null)

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
      const data = await r.json()
      setResult(JSON.stringify(data, null, 2))
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e)
      setResult(`Error: ${msg}`)
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
                className="text-xs bg-black/50 p-3 rounded mb-4 overflow-auto max-h-40 text-green-300 font-mono whitespace-pre-wrap"
              >
                {result}
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
