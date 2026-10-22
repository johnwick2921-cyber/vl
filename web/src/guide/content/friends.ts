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
      text: 'Add provisions a new friend and returns ONE install code — a single line the friend pastes into the VL app on their PC. It carries the friend’s name, your box’s tunnel address, their two ports and their connect key (plus a Tailscale auth key only when one was set when the friend was provisioned). It is shown once, in a copy box labelled “this will not be shown again”, and is never stored or logged by the bot. Copy it immediately and send it to the friend. Add is rate limited (about 10 seconds between adds) and is refused until TUNNEL_IP is set to this box’s own Tailscale IPv4 address — the page shows the Tunnel IP it is using.',
    },
    { kind: 'h', text: 'Pause · Resume · Revoke · New key' },
    {
      kind: 'p',
      text: 'Pause stops the friend’s unit and keeps it off after a reboot (their folder and key stay). Resume starts it again. Pause, New key and Revoke are refused only when the friend’s bot definitively reports an open position; an unreachable friend can still be paused or revoked. The API accepts a force override, but the Friends page never sends it, so from the page these actions stay refused until the friend is flat. A revoked friend accepts no further action, and a paused friend must be resumed before New key. Revoke stops the unit, MOVES the friend’s folder to the trash (never deletes it) and removes their Tailscale device — the device removal needs the owner’s TAILSCALE_API_KEY; when that key is not set the page says so instead of skipping it silently. Tailscale convention: tag each friend’s device tag:<name> (e.g. tag:friend1) so revoke can find it by tag when the registry has no device id; if no device carries the tag, the page says exactly that and asks you to remove it on the Tailscale machines page. New key rotates the friend’s connect key, restarts their bot and returns a new install code (shown once): the old key stops working once the bot has restarted.',
    },
    { kind: 'h', text: 'Alerts' },
    {
      kind: 'p',
      text: 'The owner’s Telegram bot raises an alert when a friend’s bot is down, when a friend’s NinjaTrader connection has been down more than five minutes, or when a friend’s AddOn build differs from this box’s build. It also relays the result of each friend’s restart after an Update. The Friends page is the authoritative surface; the alerts are advisory.',
    },
    { kind: 'h', text: 'Owner-side setup and updates' },
    {
      kind: 'p',
      text: 'Tailscale device approval must be switched ON for the tailnet: when the friend’s PC joins, its setup window waits (up to 15 minutes) for you to approve the new device in the Tailscale admin page — it will say “my PC is waiting for approval”. The Windows (Hyper-V) firewall on this machine needs the rule “VL friend bots” allowing ports 18081, 36981, 18082 and 36982 (each friend uses web port 18080+N and NinjaTrader port 36980+N) or the friend’s PC cannot reach its bot. When you run Update on your own bot, the updater restarts every active friend’s bot on the new build once that friend is flat: a friend holding a position is retried until a time limit and then left on the old build (you are told), and a paused friend is skipped. That restart does not touch the friend’s NinjaTrader add-on — the friend updates that themselves from the VL icon.',
    },
    { kind: 'h', text: 'Honest limits (friend mode)' },
    {
      kind: 'p',
      text: 'A friend instance has no updater, no installer, no maintenance pages and no Telegram agent — those surfaces exist only on the owner’s bot and answer 404 on a friend’s bot. A friend’s bot runs under the owner’s OS user; per-friend OS-user separation (which needs sudo) is a later wave. The friend app and AddOn are obfuscated, not encrypted — obfuscation slows a casual reader but is not a security boundary; the real boundaries are the tunnel ACL, the connect key and the signature-verified downloads.',
    },
  ],
}
