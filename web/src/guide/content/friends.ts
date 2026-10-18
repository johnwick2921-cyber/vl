import { GUIDE_BUILT_REV, type GuideSection } from '../types'

export const friends: GuideSection = {
  id: 'friends',
  num: 18,
  title: 'Friends',
  tagline: 'Let a friend run VL on their own PC without ever getting the code.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    { kind: 'h', text: 'The Friends page' },
    {
      kind: 'p',
      text: 'Friends is the owner-only screen for granting and removing access. A friend runs VL on their own PC; their bot runs on this box in its own folder (/home/hoang/vl-friends/<name>) with its own database and its own secret, and never receives the source code. Everything on the page is READ from this bot’s own /api/owner/friends — the live columns (bot up · NT connected · last bar age · add-on / app version · last seen) are probed by the bot over the friend’s own health endpoint, never reached into directly. A value the bot cannot tell you prints “—” — it is never invented.',
    },
    { kind: 'h', text: 'The columns' },
    {
      kind: 'p',
      text: 'name is the friend id you typed. bot up means the friend’s bot answered its own /api/health. NT connected means the friend’s NinjaTrader AddOn socket is up. last bar age is the youngest bar age across the friend’s running traders. SIM account is reserved for the friend’s account name once the friend bot exposes it. add-on / app is the build the friend’s AddOn and app report. last seen is the last successful probe this bot made (in-memory, since this bot started).',
    },
    { kind: 'h', text: 'Add and the install code' },
    {
      kind: 'p',
      text: 'Add provisions a new friend and returns ONE install code — a single line the friend pastes into the VL app on their PC. It is shown once, in a copy box labelled “this will not be shown again”, and is never stored or logged by the bot. Copy it immediately and send it to the friend. Add is rate limited: wait between adds.',
    },
    { kind: 'h', text: 'Pause · Resume · Revoke · New key' },
    {
      kind: 'p',
      text: 'Pause stops the friend’s unit (their bot stops running; their folder and key stay). Resume starts it again. Revoke stops the unit, MOVES the friend’s folder to the trash (never deletes it) and removes their Tailscale device — the device removal needs the owner’s TAILSCALE_API_KEY; when that key is not set the page says so instead of skipping it silently. New key rotates the friend’s connect key and returns a new install code (shown once): the old key stops working the moment the new one is written.',
    },
    { kind: 'h', text: 'Alerts' },
    {
      kind: 'p',
      text: 'The owner’s Telegram bot raises an alert when a friend’s bot is down, when a friend’s NinjaTrader connection has been down more than five minutes, or when a friend’s AddOn build is older than this box’s build. The Friends page is the authoritative surface; the alerts are advisory.',
    },
  ],
}
