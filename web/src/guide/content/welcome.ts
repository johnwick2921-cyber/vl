import { PRODUCT_NAME } from '../../constants/branding'
import { GUIDE_BUILT_REV, type GuideSection } from '../types'

export const welcome: GuideSection = {
  id: 'welcome',
  num: 1,
  title: 'Welcome',
  tagline: 'What this thing is, in one screen.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    {
      kind: 'cards',
      cards: [
        {
          title: PRODUCT_NAME,
          body: 'One structure-permissioned, level-anchored MNQ SIM trader — advisory-first. The Go engine computes the map; the AI (deepseek-v4-pro) writes one day-plan per session, then decides every cycle against it. In MENTOR MODE (a per-strategy switch — the owner’s mentor trader runs it) the AI places no entries at all: the mentor rules engine places them and the AI plan is advice only (Section 17). PEP MODE (another per-strategy switch, OFF by default, SIM only) is a third choice: a trader is exactly one of AI planner, mentor or pep, and pep places its entries by two fixed opening-range rules from the owner’s PDF (Section 20). You hold the knobs.',
          tag: 'SIM',
        },
        {
          title: 'Three principles',
          body: 'Advisory — most machine output informs, it does not block. Evidence — stats are cited or marked [UNVERIFIED]. Self-scoring — the system grades its own discipline (GPA), levels (level_stats) and refusals (gate-blocks).',
        },
        {
          title: 'Your role',
          body: 'Session windows, plan_mode, budgets and guardrails are yours. Destructive buttons ask first. Everything else runs itself. See Section 9 for the daily routines.',
        },
      ],
    },
    { kind: 'h', text: 'The stack' },
    {
      kind: 'code',
      title: 'tap a layer to see its job',
      lines: [
        'NT8 (Tradovate MNQ bars, SIM accounts)   ← SIM only, never live',
        '  │ TCP (bars / account / positions / orders)',
        'C# AddOn (NinjaTrader AddOns folder)  ← compile + NT8 restart to change',
        '  │ framed messages (ninjascript/vltrader_tcp_PROTOCOL.md)',
        'Go bot (vl-bin) ─── BarCache + bars table (SQLite data/data.db = "memory")',
        '  ├─ PLANNER: DeepSeek → 1 day-plan per session (advisory JSON)',
        '  ├─ EXECUTOR: every ~2 min → DeepSeek decision → risk gates → its bound SIM account',
        '  └─ WATCHER: in-position advisory only (zero order authority)',
      ],
    },
    {
      kind: 'p',
      text: 'Boot truth: the bot refuses to trade unless its revision and prompt goldens pass — `🔐 BOOT INTEGRITY OK — rev … · goldens PASS` (kernel/boot_integrity.go). The SIM lock is hard: non-SIM accounts are refused at the account-routing layer (trader/ninjatrader/tcp_trader.go isAccountTradeable). Each trader trades its own bound NinjaTrader SIM account (the Account shown on its dashboard), so two traders can run on two different SIM accounts.',
    },
    { kind: 'h', text: 'Canon — the files that ARE the answer' },
    {
      kind: 'p',
      text: 'When this Guide and the repo disagree, the repo wins. These are the files that hold the answer rather than describe it, so a question about how the bot behaves ends at one of them instead of at somebody\u2019s memory.',
    },
    {
      kind: 'table',
      title: 'where each kind of truth lives',
      head: ['File', 'What it settles'],
      rows: [
        [
          'clock-seams.list (repo root)',
          'Every rule whose verdict can change with the wall clock, and the three that are deliberately excluded with their reasons. A lint test reads this file: adding a time-dependent rule without an \u2026At seam fails the build. Born from a suite that was honestly green at 11:00 and honestly red at 14:50.',
        ],
        [
          'docs/superpowers/AUDIT-CHECKLIST.md',
          'Every bug class the system has actually suffered, each with its probe and its law. New classes are appended in the same PR that fixes them.',
        ],
        [
          'deploy/RELEASE',
          'The revision the running binary was built from. It is written BEFORE the swap, so it can never name a build that is not running.',
        ],
        [
          'kernel/boot_integrity.go',
          'Whether the bot is allowed to trade at all \u2014 revision match plus prompt goldens, refused loudly rather than assumed.',
        ],
        [
          'ninjascript/vltrader_tcp_PROTOCOL.md',
          'The wire between the Go bot and the NT8 AddOn. The two sides change in lockstep or not at all.',
        ],
      ],
    },
    { kind: 'h', text: 'Quick start' },
    {
      kind: 'p',
      text: 'Three jumps: read the plan card (Section 3) · learn what levels are (Section 4) · understand why it sits out (Section 6 + FAQ "Why did the bot sit out all session?").',
    },
  ],
}
