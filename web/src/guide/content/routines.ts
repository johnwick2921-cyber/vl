import { GUIDE_BUILT_REV, type GuideSection } from '../types'

export const routines: GuideSection = {
  id: 'routines',
  num: 9,
  title: 'Routines & Checklists',
  tagline: 'The day, the week, and the emergencies — as checklists.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    { kind: 'h', text: 'Daily' },
    {
      kind: 'checklists',
      items: [
        {
          title: 'Pre-market (before 17:00 CT ASIA read)',
          steps: [
            'NT8 running? TCP bridge connected (SYSTEM_STATUS green, "NT8 feed")?',
            "Calendar loaded — today's T1 red news visible?",
            'Strategy knobs unchanged unless a deliberate change + Save.',
            'Guardrails master: intentional position (ON/OFF) — noted?',
            'Chat: any overnight errors worth reading before the session?',
          ],
        },
        {
          title: 'After the plan read (16:30 / 01:30 / 08:00)',
          steps: [
            'Card present — not a NO-TRADE banner?',
            "Bias + bias-tree line agree with the day's shape?",
            'Levels: any consumed or dim rows?',
            'Armed scenario: confirm MET? entry S# sane?',
            'Approval required? → tap Approve to unlock the session-day.',
          ],
        },
        {
          title: 'End of day (after 14:45 EOD)',
          steps: [
            'All positions flat (the EOD ladder should have done it).',
            'Day summary: fills, gate-block counter, would-have-tripped.',
            "Note anything to fix in tomorrow's knobs.",
          ],
        },
      ],
    },
    { kind: 'h', text: 'Weekly' },
    {
      kind: 'checklists',
      items: [
        {
          title: 'The weekly review (30 min)',
          steps: [
            'Win rate / P&L per condition (reject-NY still 75%? — re-verify).',
            'Gate-block ledger: which gate fired most — knob or habit fix?',
            'Plan-card read-through: one full session, read as a stranger.',
            'README-VL-SYSTEM statuses: any QUEUE item now FIXED?',
          ],
        },
      ],
    },
    { kind: 'h', text: 'Emergencies' },
    {
      kind: 'checklists',
      items: [
        {
          title: 'Bot is trading wrong — the 60-second response',
          steps: [
            '1. ⏸ Pause (until session end).',
            "2. 🛑 Emergency Flat → Confirm, on EACH trader that is trading wrong — it acts on the trader on screen only. On that trader's account NT8 closes the MNQ position AND cancels every working MNQ order, resting entries included (even when flat).",
            "3. Check NT8's Orders and Positions tabs: the OTHER trader's account keeps its own position and orders — flatten that trader too if they must go.",
            '4. Screenshot the card + the chat + the gate panel.',
            '5. Diagnose with the refusal decoder (Section 6) before touching knobs.',
          ],
        },
        {
          title: 'Feed / NT8 down',
          steps: [
            'Confirm: gate "NT8 feed down" showing? Bars stale?',
            'Do NOT restart the bot first — check NT8 + the AddOn connection.',
            'If NT8 must restart: recompile check → full NT8 restart → verify TCP reconnect.',
          ],
        },
      ],
    },
  ],
}
