// W-FRIEND-HOSTED Segment 6 (DS-106): owner Friends page API client.
import type {
  AddFriendResponse,
  FriendActionResponse,
  FriendAddonStatus,
  FriendsListResponse,
} from '../../types'
import { API_BASE, httpClient } from './helpers'

export type FriendAction = 'pause' | 'resume' | 'revoke' | 'rotate'

// The owner bot has no /friend/addon-status route, so the probe 404s. That 404
// is the expected "owner bot" signal, but the browser logs every failed request
// to the console (not suppressible from JS) — and the banner re-polls every
// 60s. Latch the first 404 so the console shows it at most once per page load.
// The route set is fixed at server boot, so a 404 cannot turn into a 200
// without a reload. Exported reset is for tests only.
let ownerBotNoFriendRoute = false
export function resetFriendAddonProbeForTest(): void {
  ownerBotNoFriendRoute = false
}

export const friendsApi = {
  async listFriends(): Promise<FriendsListResponse> {
    const result = await httpClient.get<FriendsListResponse>(
      `${API_BASE}/owner/friends`
    )
    if (!result.success)
      throw new Error(result.message || 'Failed to fetch friends')
    return result.data!
  },

  async addFriend(name: string): Promise<AddFriendResponse> {
    const result = await httpClient.post<AddFriendResponse>(
      `${API_BASE}/owner/friends`,
      { name }
    )
    if (!result.success)
      throw new Error(result.message || 'Failed to add friend')
    return result.data!
  },

  async friendAction(
    name: string,
    action: FriendAction
  ): Promise<FriendActionResponse> {
    const result = await httpClient.post<FriendActionResponse>(
      `${API_BASE}/owner/friends/${encodeURIComponent(name)}/${action}`
    )
    if (!result.success)
      throw new Error(result.message || `Failed to ${action} friend`)
    return result.data!
  },

  // Friend bot only: whether the connected AddOn is older than the manifest.
  // Silent (no toast) and null on 404 — the owner bot has no friend route,
  // which is exactly the "hidden on the owner's bot" signal.
  async friendAddonStatus(): Promise<FriendAddonStatus | null> {
    if (ownerBotNoFriendRoute) return null
    try {
      const result = await httpClient.request<FriendAddonStatus>(
        `${API_BASE}/friend/addon-status`,
        { method: 'GET', silent: true }
      )
      if (!result.success || !result.data) return null
      return result.data
    } catch (e) {
      // httpClient throws Error('API not found') for a 404.
      if (e instanceof Error && e.message === 'API not found')
        ownerBotNoFriendRoute = true
      return null
    }
  },
}
