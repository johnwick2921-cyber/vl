// Friend-mode dashboard banner: the connected AddOn is older than the manifest.
// Renders nothing on the owner's bot — the friend route 404s there, so the API
// client returns null and this banner stays hidden.
import { useEffect, useState } from 'react'
import { friendsApi } from '../../lib/api/friends'

const POLL_MS = 60_000

export function AddonOutdatedBanner() {
  const [outdated, setOutdated] = useState(false)

  useEffect(() => {
    let mounted = true

    const poll = () => {
      friendsApi
        .friendAddonStatus()
        .then((status) => {
          // setOutdated(!!status?.outdated) so the banner both appears AND
          // clears once the add-on is updated (or the route stops answering).
          if (mounted) setOutdated(!!status?.outdated)
        })
        .catch(() => {
          // 404 / network / any failure = not a friend bot or no signal → hide.
        })
    }

    poll()
    const id = setInterval(poll, POLL_MS)
    return () => {
      mounted = false
      clearInterval(id)
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
