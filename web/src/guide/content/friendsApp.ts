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
            'They already have NinjaTrader 8 with live MNQ data and their own SIM account.',
            'They run VL-Setup.exe and paste the install code you sent (shown once, in the Friends page).',
            'The app quietly installs Tailscale, writes its connection settings, downloads and verifies the compiled add-on, and places a VL icon on their desktop.',
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
      text: 'Each time the app starts it asks their bot for a signed version list. When the add-on or the app is newer, a pop-up says “VL update available — Update now / Later”. Update now downloads the new build, checks its signature and its SHA-256 (a bad download refuses and nothing changes), waits until the bot is flat, moves the old add-on aside (never deletes it), and asks them to restart NinjaTrader. It never updates during a trade.',
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
