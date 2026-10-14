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
            "Acts on THIS trader only (the one on screen): sends ONE market close to NinjaTrader naming its bound account, and the AddOn (build 2026-10-14-a or newer — the boot line's `nt8 addon:` part must read match=yes) flattens that account and no other; if that account cannot be found, or is not SIM, the AddOn refuses, logs it and flattens nothing. On that account NT8's Flatten closes the MNQ position at market AND cancels EVERY working MNQ order — the protective bracket and any resting entry orders too. The bot's ledger learns of those cancels from NinjaTrader's next order reports, not from this button. It is sent even when the trader is flat (then it only cancels that account's working MNQ orders). Another trader's account, position and orders are never touched. It does NOT use the EOD limit-then-market ladder and does NOT pause the trader — it does not sit out. It also clears THIS trader's daily-loss latch (other traders' latches stand); if its own loss still stands the gate re-trips on the next 60-second beat. The result shows positions_flattened = the open positions NT8 reported when the flatten was sent (null if that read failed); NT8 confirms each close separately. Two-step: Cancel / Confirm.",
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
          sideEffects:
            "Closes this trader's MNQ position now: one market close naming its bound account, routed by the AddOn exactly like Emergency Flat — NT8's Flatten closes the whole MNQ position on that account and cancels that account's working MNQ orders, resting entries included. Another trader's account is never touched.",
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
          body: "It never deletes the plan, never touches the strategy, never changes the day's P&L or trade records, and never touches another trader's account, position or orders. It does clear this trader's daily-loss latch (a still-standing loss re-trips it), and it does not pause entries. It DOES cancel this trader's resting MNQ entry orders on its own account (NT8's Flatten cancels every working order for the contract) — to stop the bot placing new ones, Pause the trader.",
        },
      ],
    },
  ],
}
