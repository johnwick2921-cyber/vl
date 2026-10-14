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
            'Stops NEW entries only (the admission gate refuses them; the plan stays armed). Orders ALREADY RESTING at the broker are NOT cancelled and can still fill while paused — cancel them yourself if that matters. Stops, targets, the EOD flat and position management continue. Resumes by itself at the deadline; Custom takes a number of minutes.',
          budget: 'None.',
          undo: 'Resume at will (unless session ended).',
          useWhen:
            'You need the bot to stop taking entries without flattening.',
        },
        {
          label: '🛑 Emergency Flat',
          api: 'dashboard Emergency Flat modal → Confirm',
          sideEffects:
            'Market-flattens ALL positions NOW + sits out (EOD-flat ladder: EOD_FLAT_LIMIT_TICKS then EOD_FLAT_MARKET_AFTER_SEC). Two-step: Cancel / Confirm.',
          budget: 'None — emergency path.',
          undo: 'Positions are closed; the day can continue after review.',
          useWhen:
            'Something is wrong: runaway positions, feed confusion, or you just want out.',
        },
        {
          label: 'Account selector',
          api: 'dashboard AccountSelector',
          sideEffects:
            'Switches the account the bot routes to — SIM only (isAccountTradeable).',
          budget: 'None.',
          undo: 'Switch back — only while the trader is stopped (the selector is disabled while it runs).',
          useWhen: 'Checking which SIM account is bound.',
        },
        {
          label: 'Strategy → Save',
          api: 'PUT /api/strategies/:id',
          sideEffects:
            'Persists all knob changes; toast "Strategy saved" + `saved {MM/DD, HH:MM} CT` chip. Save RELOADS every running trader bound to that strategy at once (api/strategy.go: remove + reload from the store, open position re-attached, NT8 stop/target guard it during the swap), so the change is live on the next cycle — no manual restart. If that reload fails the config is still saved and the log says "restart to apply".',
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
          body: 'Tap Emergency Flat → the modal shows scope (all positions) → tap Confirm. Cancel is the bigger button on the left. There is no single-click flatten — that is the point.',
        },
        {
          title: 'What it will NOT do',
          body: "It never deletes the plan, never touches the strategy, never resets the day's stats. It closes positions and the day continues flat.",
        },
      ],
    },
  ],
}
