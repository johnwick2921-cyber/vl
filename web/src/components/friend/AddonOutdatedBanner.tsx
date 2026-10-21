// Friend-mode dashboard banner: the connected AddOn is older than the manifest.
// Renders nothing on the owner's bot — the friend route 404s there, so the API
// client returns null and this banner stays hidden.
import { useEffect, useState } from 'react'
import { friendsApi } from '../../lib/api/friends'

export function AddonOutdatedBanner() {
  const [outdated, setOutdated] = useState(false)

  useEffect(() => {
    let mounted = true
    friendsApi
      .friendAddonStatus()
      .then((status) => {
        if (mounted && status && status.outdated) setOutdated(true)
      })
      .catch(() => {
        // 404 / network / any failure = not a friend bot or no signal → hide.
      })
    return () => {
      mounted = false
    }
  }, [])

  if (!outdated) return null

  return (
    <div
      data-testid="addon-outdated-banner"
      className="mb-4 rounded-lg border border-vl-neo-gold/60 bg-vl-neo-gold/10 px-4 py-3 text-vl-neo-gold font-bold text-center animate-scale-in"
    >
      Your NinjaTrader add-on is out of date — double-click the VL icon to
      update.
    </div>
  )
}
