// W-FRIEND-HOSTED Segment 6 (DS-106): owner Friends page API client.
import type {
  AddFriendResponse,
  FriendActionResponse,
  FriendAddonStatus,
  FriendsListResponse,
} from '../../types'
import { API_BASE, httpClient } from './helpers'

export type FriendAction = 'pause' | 'resume' | 'revoke' | 'rotate'

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
    try {
      const result = await httpClient.request<FriendAddonStatus>(
        `${API_BASE}/friend/addon-status`,
        { method: 'GET', silent: true }
      )
      if (!result.success || !result.data) return null
      return result.data
    } catch {
      return null
    }
  },
}
