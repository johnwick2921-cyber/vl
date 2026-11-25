// W-FRIEND-HOSTED Segment 6 (DS-106): the owner's Friends page.
//
// Everything on this page is READ from the owner bot's own API
// (/api/owner/friends); the live columns are probed by the bot over each
// friend's OWN /api/health — the page never reaches into a friend's process.
// A value the bot cannot tell us prints "—", never a fabricated one. The
// install code is shown ONCE (this will not be shown again) and is never
// persisted in local state beyond the current view.

import { useCallback, useEffect, useState } from 'react'
import { friendsApi, type FriendAction } from '../lib/api/friends'
import type { Friend } from '../types'

const FRIENDS_REFRESH_MS = 10_000

export function FriendsPage() {
  const [friends, setFriends] = useState<Friend[]>([])
  const [tailscaleKeySet, setTailscaleKeySet] = useState(true)
  const [tunnelIp, setTunnelIp] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [installCode, setInstallCode] = useState('') // shown once, never persisted
  const [newName, setNewName] = useState('')
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)

  const load = useCallback(async () => {
    try {
      const res = await friendsApi.listFriends()
      setFriends(res.friends)
      setTailscaleKeySet(res.tailscale_key_set)
      setTunnelIp(res.tunnel_ip)
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load friends')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
    const id = setInterval(load, FRIENDS_REFRESH_MS)
    return () => clearInterval(id)
  }, [load])

  const addFriend = useCallback(async () => {
    const name = newName.trim()
    if (!name) return
    setBusy(true)
    setError('')
    try {
      const res = await friendsApi.addFriend(name)
      setInstallCode(res.install_code)
      setNewName('')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to add friend')
    } finally {
      setBusy(false)
    }
  }, [newName, load])

  const act = useCallback(
    async (name: string, action: FriendAction) => {
      setBusy(true)
      setError('')
      try {
        const res = await friendsApi.friendAction(name, action)
        await load()
        if (res.install_code) {
          setInstallCode(res.install_code)
        }
        if (res.tailscale === 'api_key_missing') {
          setError(
            'Tailscale API key is not set — set TAILSCALE_API_KEY in the owner .env to also remove the device.'
          )
        }
      } catch (e) {
        setError(e instanceof Error ? e.message : `Failed to ${action} friend`)
      } finally {
        setBusy(false)
      }
    },
    [load]
  )

  const copyCode = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(installCode)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      setError('Could not copy — select and copy the code manually')
    }
  }, [installCode])

  return (
    <div style={{ padding: '1.5rem', color: 'var(--vl-ivory)' }}>
      <h1 style={{ fontSize: '1.25rem', marginBottom: '0.25rem' }}>Friends</h1>
      <p style={{ color: 'var(--vl-muted)', marginTop: 0 }}>
        Let a friend run VL on their own PC without getting the code. You grant
        or remove access; they set their own strategy, AI key and SIM account.
      </p>

      {/* Health strip — read from the bot, never invented */}
      <div
        style={{
          display: 'flex',
          gap: '1.5rem',
          marginBottom: '1rem',
          fontSize: '0.85rem',
          color: 'var(--vl-muted)',
        }}
      >
        <span>Tailscale API key: {tailscaleKeySet ? 'set' : 'not set'}</span>
        <span>Tunnel IP: {tunnelIp || '—'}</span>
      </div>
      {!tailscaleKeySet && (
        <p style={{ color: 'var(--vl-warn, #f5a623)', fontSize: '0.85rem' }}>
          Set the Tailscale API key (TAILSCALE_API_KEY in the owner .env) so
          Revoke can also remove the device.
        </p>
      )}

      {/* Add */}
      <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '1rem' }}>
        <input
          placeholder="friend name (a-z0-9)"
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          disabled={busy}
          aria-label="friend name"
          style={{ padding: '0.4rem' }}
        />
        <button onClick={addFriend} disabled={busy || !newName.trim()}>
          Add
        </button>
      </div>

      {error && (
        <p
          style={{ color: 'var(--vl-warn, #f5a623)' }}
          data-testid="friends-error"
        >
          {error}
        </p>
      )}

      {/* Install code — shown once */}
      {installCode && (
        <div
          style={{
            border: '1px solid var(--vl-hair)',
            padding: '1rem',
            marginBottom: '1rem',
          }}
          data-testid="install-code-box"
        >
          <p style={{ fontWeight: 600, marginTop: 0 }}>
            Install code — this will not be shown again
          </p>
          <textarea
            readOnly
            value={installCode}
            style={{ width: '100%', minHeight: '3rem' }}
            data-testid="install-code"
          />
          <button onClick={copyCode} style={{ marginTop: '0.5rem' }}>
            {copied ? 'Copied ✓' : 'Copy'}
          </button>
          <button
            onClick={() => setInstallCode('')}
            style={{ marginTop: '0.5rem', marginLeft: '0.5rem' }}
          >
            Dismiss
          </button>
        </div>
      )}

      {/* Table */}
      {loading ? (
        <p>Loading…</p>
      ) : friends.length === 0 ? (
        <p style={{ color: 'var(--vl-muted)' }}>No friends yet.</p>
      ) : (
        <div style={{ overflowX: 'auto', maxWidth: '100%' }}>
          <table
            style={{
              width: '100%',
              borderCollapse: 'collapse',
              fontSize: '0.85rem',
            }}
          >
            <thead>
              <tr>
                {[
                  'name',
                  'bot up',
                  'NT connected',
                  'last bar age',
                  'SIM account',
                  'add-on / app',
                  'last seen',
                  'actions',
                ].map((h) => (
                  <th
                    key={h}
                    style={{
                      textAlign: 'left',
                      borderBottom: '1px solid var(--vl-hair)',
                      padding: '0.4rem',
                    }}
                  >
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {friends.map((f) => (
                <tr key={f.name} data-testid={`friend-row-${f.name}`}>
                  <td style={cellStyle}>{f.name}</td>
                  <td style={cellStyle}>
                    {f.bot_up ? 'up' : f.probe_error ? 'down' : '—'}
                  </td>
                  <td style={cellStyle}>
                    {f.bot_up ? (f.nt_connected ? 'yes' : 'no') : '—'}
                  </td>
                  <td style={cellStyle}>
                    {f.last_bar_age_ms != null
                      ? `${msToAge(f.last_bar_age_ms)}`
                      : '—'}
                  </td>
                  <td style={cellStyle}>—</td>
                  <td style={cellStyle}>
                    {f.addon_build
                      ? `${f.addon_build} / ${f.app_version || '—'}`
                      : '—'}
                  </td>
                  <td style={cellStyle}>{f.last_seen || '—'}</td>
                  <td style={cellStyle}>
                    <button
                      onClick={() =>
                        act(f.name, f.state === 'paused' ? 'resume' : 'pause')
                      }
                      disabled={busy || f.state === 'revoked'}
                      style={{ marginRight: '0.25rem' }}
                    >
                      {f.state === 'paused' ? 'Resume' : 'Pause'}
                    </button>
                    <button
                      onClick={() => act(f.name, 'rotate')}
                      disabled={busy || f.state === 'revoked'}
                      style={{ marginRight: '0.25rem' }}
                    >
                      New key
                    </button>
                    <button
                      onClick={() => act(f.name, 'revoke')}
                      disabled={busy || f.state === 'revoked'}
                    >
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

const cellStyle: React.CSSProperties = {
  borderBottom: '1px solid var(--vl-hair)',
  padding: '0.4rem',
}

function msToAge(ms: number): string {
  if (ms < 0) return '—'
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m`
  return `${Math.floor(m / 60)}h`
}
