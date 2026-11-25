import { GUIDE_BUILT_REV, type GuideSection } from '../types'

// Friend app (VL-Setup.exe) + installer — S8 (DS-108). The friend's side of
// W-FRIEND-HOSTED: they run one exe, paste one code, and the app does the rest.
export const friendsApp: GuideSection = {
  id: 'friendsApp',
  num: 19,
  title: 'Friend app (VL-Setup)',
  tagline:
    'How your friend installs VL on their own PC — without ever receiving the code.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    {
      kind: 'p',
      text: 'A friend tests VL on their own PC and never gets the code. Their PC receives only NinjaTrader, a compiled (scrambled) add-on, and a small app. Their bot runs on your box, in its own folder with its own database, login and ports — it cannot see your traders, database or keys.',
    },
    { kind: 'h', text: 'What the friend does (one time, ~5 minutes)' },
    {
      kind: 'checklists',
      items: [
        {
          title: 'Install',
          steps: [
            'They already have NinjaTrader 8 with live MNQ data and their own SIM account, and have opened NinjaTrader 8 at least once (setup stops with a message if NinjaTrader’s folders do not exist yet).',
            'They run VL-Setup.exe and paste the install code you sent (shown once, in the Friends page) — step [1/6].',
            'Steps [2/6] and [3/6]: the app installs Tailscale (Windows asks permission once — they click Yes) and joins your tailnet, then waits up to 15 minutes for YOU to approve their device in Tailscale.',
            'Steps [4/6] and [5/6]: it downloads the compiled add-on, checks its signature and SHA-256, and places VL.AddOn.dll in Documents\\NinjaTrader 8\\bin\\Custom. If NinjaTrader is open they must close it and open it again once so it loads the add-on.',
            'Step [6/6]: it puts a VL icon on their desktop and Start Menu and opens their dashboard.',
          ],
        },
        {
          title: 'Every day',
          steps: [
            'They double-click the VL icon — their dashboard opens in its own window.',
            'They log in, set their own AI key, strategy and settings.',
          ],
        },
      ],
    },
    { kind: 'h', text: 'The update pop-up' },
    {
      kind: 'p',
      text: 'Each time the friend double-clicks the VL icon, the app opens their dashboard and then asks their bot for a signed version list (up to 15 seconds). Only a newer ADD-ON triggers a pop-up: “VL update available (add-on old → new). Yes = update now, No = later.” Yes downloads the new build and checks its signature and SHA-256 (a bad download refuses and nothing changes), waits until the bot is flat, asks them to close NinjaTrader, moves the old add-on aside (never deletes it) and swaps the new one in, then says “restart NinjaTrader to load it”. If a trade is open it says the update is postponed and offers it again next time. An app-only update is not downloaded by this pop-up. If the bot cannot be reached the app says “VL cannot reach your bot — open Tailscale and check it says Connected.”',
    },
    { kind: 'h', text: 'Out-of-date add-on banner' },
    {
      kind: 'callout',
      title: 'The dashboard says when the add-on is old',
      items: [
        {
          title: 'A banner on the friend’s dashboard',
          body: 'When the add-on the friend is running is older than the latest build, their dashboard shows a banner: “Your NinjaTrader add-on is out of date — double-click the VL icon to update.” The owner’s dashboard never shows this banner.',
        },
      ],
    },
    { kind: 'h', text: 'connect.json — the add-on’s connection settings' },
    {
      kind: 'p',
      text: 'The app writes one small settings file so the friend’s NinjaTrader add-on knows which box and port to talk to: Documents\\NinjaTrader 8\\VL\\connect.json. It holds the tunnel host, the NinjaTrader port and the connect key — nothing else. VL-Setup writes it during install; do not edit it by hand — a corrupt file makes the add-on refuse to connect rather than silently fall back.',
    },
    {
      kind: 'callout',
      title: 'Windows SmartScreen warning',
      items: [
        {
          title: 'The exe is not code-signed yet',
          body: 'Windows SmartScreen warns about the unsigned VL-Setup.exe on first run. That is expected: a code-signing certificate is an optional later step, not part of this release. The friend clicks “More info → Run anyway”.',
        },
      ],
    },
    {
      kind: 'callout',
      title: 'No source on the friend’s PC',
      items: [
        {
          title: 'Compiled + scrambled only',
          body: 'The friend’s NinjaTrader folder gets only VL.AddOn.dll — compiled and obfuscated. No .cs source file is ever placed on their PC, so the NinjaScript Editor has nothing to open.',
        },
      ],
    },
    {
      kind: 'callout',
      title: 'Friend mode limits (honest)',
      items: [
        {
          title: 'A friend bot is cut down',
          body: 'Friend mode has no updater, installer, maintenance pages or Telegram. The friend sets their own AI key and strategy; everything else is the same SIM-only trading.',
        },
        {
          title: 'Same OS user on your box',
          body: 'A friend bot runs under the owner’s OS user on your box — separated by its own folder, database, login and ports, not by an OS account. OS-user separation is a later wave.',
        },
        {
          title: 'Obfuscation is not encryption',
          body: 'The compiled add-on is scrambled, not encrypted. A determined expert could still study it; what it stops is casual reading and the NinjaScript Editor (no .cs file is present).',
        },
      ],
    },
  ],
}
