// m386 — the Friends page is OFF unless VL_FRIENDS_PAGE=1. The web reads the
// flag from the EXISTING /api/health status endpoint (friends_page) rather than
// inventing a new public endpoint. While the value is unknown (loading, error,
// or an older bot that predates the field), the hook returns false so the
// Friends surface stays hidden by default — a box that opted in reveals it the
// moment health reports true.
import useSWR from 'swr'
import { updatesApi } from '../lib/api/updates'

export function useFriendsPageEnabled(): boolean {
  const { data } = useSWR('friends-page-flag', () => updatesApi.health(true), {
    revalidateOnFocus: false,
    refreshInterval: 60_000,
  })
  return data?.friends_page === true
}
