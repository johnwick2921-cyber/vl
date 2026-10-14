import { GUIDE_BUILT_REV, type GuideSection } from '../types'

export const buttons: GuideSection = {
  id: 'buttons',
  num: 8,
  title: 'Buttons & Actions',
  tagline: 'The dashboard controls — what each one actually does.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    {
      kind: 'buttons',
      items: [
        {
          label: '⏸ Pause (30 min / 1 hour / Until session end / Custom)',
          api: 'dashboard PauseButton menu',
          sideEffects:
            'Stops entries (plan stays armed) — clock-gated resume, or custom until a timestamp.',
          budget: 'None.',
          undo: 'Resume at will (unless session ended).',
          useWhen:
            'You need the bot to stop taking entries without flattening.',
        },
        {
          label: '🛑 Emergency Flat',
          api: 'dashboard Emergency Flat modal → Confirm',
          sideEffects:
            "Acts on THIS trader only (the one on screen): sends ONE market flatten to NinjaTrader for its bound account's position (NT8 closes it at market and cancels its protective bracket; the close is confirmed separately). It does NOT use the EOD limit-then-market ladder, does NOT cancel resting entry orders, and does NOT pause the trader — it does not sit out. It also clears THIS trader's daily-loss latch (other traders' latches stand); if its own loss still stands the gate re-trips on the next 60-second beat. The result shows positions_flattened = the open positions NT8 reported when the flatten was sent (null if that read failed). Two-step: Cancel / Confirm.",
          budget: 'None — emergency path.',
          undo: 'The position is closed; the trader keeps running — use Pause to stop new entries.',
          useWhen:
            'Something is wrong: runaway positions, feed confusion, or you just want out.',
        },
        {
          label: 'Account selector',
          api: 'dashboard AccountSelector',
          sideEffects:
            'Switches the account the bot routes to — SIM only (isAccountTradeable).',
          budget: 'None.',
          undo: 'Switch back.',
          useWhen: 'Checking which SIM account is bound.',
        },
        {
          label: 'Strategy → Save',
          api: 'PUT /api/strategy',
          sideEffects:
            'Persists all knob changes; toast "Strategy saved" + `saved {MM/DD, HH:MM} CT` chip. Config is cached at trader-load — a strategy change needs the trader reload or restart to go hot.',
          budget: 'None.',
          undo: 'Re-edit + re-save.',
          useWhen: 'After every knob change (the ritual).',
        },
        {
          label: 'Positions → Close',
          api: 'dashboard positions table',
          sideEffects: 'Closes one position now.',
          budget: 'None.',
          undo: 'Re-enter only via the planner.',
          useWhen:
            'A single position needs to go without flattening everything.',
        },
        {
          label: '🧹 Clear (chat)',
          api: 'AgentPage quick chip',
          sideEffects: 'Clears the chat history / context window.',
          budget: 'None.',
          undo: 'History is gone — not recoverable.',
          useWhen: 'The chat context is stale or bloated.',
        },
      ],
    },
    {
      kind: 'callout',
      title: 'emergency flat — the exact click order',
      items: [
        {
          title: 'Two steps, on purpose',
          body: 'Tap Emergency Flat → the modal names the trader it acts on → tap Confirm. There is no single-click flatten — that is the point. To stop new entries as well, Pause the trader.',
        },
        {
          title: 'What it will NOT do',
          body: "It never deletes the plan, never touches the strategy, never changes the day's P&L or trade records, and never touches another trader. It does clear this trader's daily-loss latch (a still-standing loss re-trips it), and it does not pause entries or cancel resting entry orders.",
        },
      ],
    },
  ],
}
