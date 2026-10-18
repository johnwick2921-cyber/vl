// W-FRIEND-HOSTED Segment 6 (DS-106): the owner Friends page types (B2).
export type FriendState = 'active' | 'paused' | 'revoked'

export interface Friend {
  name: string
  web_port: number
  nt_port: number
  key_sha256: string
  state: FriendState
  created_at: string
  tailscale_device_id: string
  // Live fields (probed over the friend's own /api/health + /api/friend/manifest).
  // Absent = the friend could not be probed — never fabricated.
  bot_up: boolean
  nt_connected: boolean
  last_bar_age_ms?: number
  revision?: string
  addon_build?: string
  app_version?: string
  last_seen?: string
  probe_error?: string
}

export interface FriendsListResponse {
  friends: Friend[]
  tailscale_key_set: boolean
  tunnel_ip: string
}

export interface AddFriendResponse {
  friend: Friend
  install_code: string
  shown_once: boolean
}

export interface FriendActionResponse {
  name: string
  state?: string
  tailscale?: string
  install_code?: string
  shown_once?: boolean
}
