import { GUIDE_BUILT_REV, type GuideSection } from '../types'

export const faq: GuideSection = {
  id: 'faq',
  num: 12,
  title: 'FAQ',
  tagline: 'The questions everyone asks.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    {
      kind: 'faq',
      items: [
        {
          q: 'Why did the bot sit out all session?',
          a: 'Look at the card: a ⛔ fail-closed banner (read failed — safe, never stale) or a no_trade declaration (the AI decided to skip — balance day, no A/B zone). Then check the gate-block ledger for the refusing label.',
          mechanism:
            'Read attempts WARN in the log: "📐 planner attempt N/3 …".',
          link: '#plan-card',
        },
        {
          q: 'Why was my trade refused?',
          a: 'Every refusal is a named string — confidence too low, against the plan, stop below MIN_SL, past last-entry, awaiting approval… The refusal decoder table maps each to its meaning and knob.',
          mechanism:
            'kernel/engine_position.go · trader/auto_trader_planconfig.go',
          link: '#guards',
        },
        {
          q: 'Why did the HTF veto switch to cross mode?',
          a: 'The 2026-08-28 autopsy replayed the 7 real vetoed arms: the 1h-only veto blocked 3 would-have-won entries (+$352) while 4h was RANGING at all 7 timestamps — it vetoed nothing the evidence supported. cross mode vetoes only when 1h AND 4h agree on the counter-trend, so the $352/0 split becomes impossible by construction.',
          mechanism:
            'HTF_VETO_MODE=cross in .env · kernel/htf_veto.go HTFVetoVerdict',
          link: '#guards',
        },
        {
          q: 'What happens on a fast-market wake?',
          a: 'When price has drifted more than FAST_MARKET_ATR × ATR5m since the last plan write, a wake read re-plans with fast reasoning (FAST TAPE) instead of waiting out the stale plan — the plan gets eyes sooner exactly when the tape is moving.',
          mechanism:
            'trader/auto_trader_loop.go fastMarket… · FAST_MARKET_REASONING',
          link: '#settings',
        },
        {
          q: 'Is any of this real money?',
          a: 'No. Every path is NinjaTrader SIM. isAccountTradeable blocks non-SIM accounts at routing, and the SIM lock must not be weakened.',
          link: '#guards',
        },
        {
          q: 'Why does the header PNL differ from the day total in the position history?',
          a: 'PNL:: is the NT8-native total (equity − initial balance) and can sit at 0.00 all day. LEDGER_DAY:: beside it is the ledger day total — the SAME rule as the position-history footer: rows closed today CT, unknown-P&L / test-seam reasons out, strict pnl_corrected, unresolved rows counted and excluded. When the backend cannot compute it the chip says UNRESOLVED rather than showing a zero.',
          mechanism:
            '/api/account carries ledger_day_pnl / ledger_day_resolved / ledger_day_unresolved (store.GetLedgerDayTotal); the boot line 🧾 states the corrected-column guard.',
          link: '#status',
        },
        {
          q: 'I restarted the bot mid-setup — is my resting order dead until the next plan read?',
          a: 'No, not since 0B (2026-09-02). A restart cancels pre-boot orders at the broker (the class-33 boot sweep) because the process that owned them is gone. Those swept rows now re-arm under the SAME plan version and the journal says so: "⚖ re-armed after boot sweep". A cancel YOU made still sticks until the next plan version — manual-cancel-wins is unchanged; only the machine\'s own housekeeping is re-armable.',
          mechanism:
            'store/armed_orders.go UpsertArm: terminal + same version = stays terminal, EXCEPT rows whose state_reason starts with "boot_sweep".',
          link: '#guards',
        },
        {
          q: 'I restarted NinjaTrader while holding a trade — does my stop survive?',
          a: 'The stop itself usually does: working orders are re-synced from the broker. What did NOT survive, until 2026-09-07, was the bot\'s memory of it — the AddOn kept the stop/target pairing in memory only, rebuilt from nowhere, so after a restart auto-breakeven and bracket edits silently did nothing and no one was checking whether a stop was there at all. Now the bot asks the broker directly, every minute a position is open and again on reconnect, and if there is genuinely no stop it raises a P0 and places one. Two honest caveats: on the reconnect edge the answer can still read "UNVERIFIED" for a moment — since release #20 the AddOn sends an order snapshot for every SIM account the instant it connects (SendAllOrderSnapshots on connect), and the check re-asks a minute later; and protective orders are now GTC rather than Day, so a stop no longer expires at session end while the position it protects lives on.',
          mechanism:
            'trader/protection_reconciler.go adjudicateProtection, on monitorTick and the dead-man reconnect edge; ninjascript SubmitBracketOnEntryFill places SL/TP TimeInForce.Gtc.',
          link: '#guards',
        },
        {
          q: 'Why was a plan rejected twice for the same reason?',
          a: 'It should not happen any more. Before class 50 (2026-09-02) the rewrite prompt named only the LAST defect, so a chain could be corrected about a fade, fix the fade, and walk back into the voided breakdown it had been rejected for two attempts earlier — which is exactly what the London read did that morning. The rewrite now carries every distinct defect of that read, oldest first, and it is printed twice: at the very top of the prompt, ahead of the playbook, and again at the very end. The attempt line says how many distinct defects are riding.',
          mechanism:
            'trader/auto_trader_planner.go addDistinctReject → plannerRejectHeader + plannerRejectTail, logged as "reauthor+block(top+tail, N distinct)".',
          link: '#guards',
        },
        {
          q: 'Why does the plan sometimes fight the prompt?',
          a: "It used to. The prompt ordered a play ('below the prior day\u2019s low you MUST write a continuation short') while the validator voids a continuation into a level price has already reclaimed, so obeying the prompt earned a rejection. Since class 50 the prompt orders a DIRECTION and names the legal conditions, lists every breakdown level already reclaimed as void (decided by the validator\u2019s own code, not a second copy), and prints the minimum stop distance the arm gate will enforce (1.5\u00d7ATR5m, with the current reading) so a stop can be authored right the first time.",
          mechanism:
            'kernel/class45_feeds_forward.go ComputeVoidBreakdownLevels (calls BreakdownContinueState) + RenderStopFloorLine → the planner prompt.',
          link: '#guards',
        },
        {
          q: 'Why did my stop get wider than the plan said?',
          a: "Since 0B every armed stop is composed: beyond the nearest seated level on the risk side plus 2 ticks, floored at 1.5×ATR5m, widest wins — and never tighter than the planner authored. The arm logs 🛑 with the chosen stop, the anchor level, the ATR floor and which one bound. If nothing seated sits within 3×ATR on the risk side the line says stop_unanchored and the ATR floor governs. Note the R:R gate then judges the WIDER stop, so some arms are now refused at the strategy's min R:R (2 on both bound strategies; ARM_MIN_RR was deleted 2026-09-03) that would previously have rested. Exception: a reject fade keeps the frozen structural stop it was authored with (trader/arm_stop_anchor.go). On CME futures the chosen stop is then rounded to the tick AWAY from the entry (a long's stop down, a short's up), using the instrument's own tick (M2K/RTY 0.10, YM 1.00, NQ/ES 0.25) in any symbol form, so the order NT8 receives is never inside the floor it passed.",
          mechanism:
            'trader/arm_stop_anchor.go composeArmStop → the arm gate → the ledger row → placement.',
          link: '#settings',
        },
        {
          q: 'What does the ⛔ NO-TRADE chip mean?',
          a: 'The re-plan budget is exhausted: the last version row is the terminal marker, not a real plan. Reset (↺) re-arms the budget and reads fresh.',
          mechanism:
            'replan_cap=4 legitimately ends at a row labelled v6 (marker) — and, since class 35, a v6 chain can also have the FULL budget left: only death re-plans and owner re-reads spend (recorded counter); wake reads and dormant flips are free.',
          link: '#plan-card',
        },
        {
          q: 'What do the level grades mean?',
          a: "A/B/C = the planner's confidence in a level (evidence × freshness × confluence × TF). m: is the machine grade. The planner grade is informational, but the grade is NOT purely decorative: the strategy's min_grade filters which levels are SEATED on the card at all (kernel/levels_assemble.go ScoreLevelsMinGradeFullSeats; live value B).",
          link: '#levels',
        },
        {
          q: 'Do I need to approve plans?',
          a: 'Only if approval_required is ON. Then entries are HELD until you tap Approve for that CME session-day. Default is OFF (fully automatic).',
          link: '#plan-card',
        },
        {
          q: 'Why did the card say ⚖ thin side?',
          a: 'It no longer does — the thin-side concept is REMOVED (owner ruling 2026-08-31): per-side counts no longer exist — no knob, no WARN, no ⚖ note. The two surviving guards: a plan with 0 levels on a side fails closed (2026-08-18 one-sided-map pathology), and an empty machine map fails closed.',
          link: '#plan-card',
        },
        {
          q: 'What is the A-setup?',
          a: 'sweep (liquidity taken) → displacement → FVG retrace, chained as S1 sweep_reclaim → S2 fvg_entry. A bare FVG with no sweep precursor gets a WARN at write — it has no standalone edge.',
          link: '#plays',
        },
        {
          q: 'Why did the bot flatten at 14:45?',
          a: 'EOD flat: the session ends flat by design — resting entries are cancelled, then positions close at market (the limit-ticks-then-market ladder runs only when the server env sets EOD_FLAT_LIMIT_TICKS above 0; default 0). The session registry enforces it for all three sessions. Exception: a SWING4H mentor position is held through the close (it is managed by the 4h candle).',
          link: '#trading-day',
        },
        {
          q: 'NT8 is down — what do I do?',
          a: 'Nothing on the bot first: no bars means no decisions, and the gates (feed down / dead-man) say so. Fix NT8 + the AddOn connection (copy → F5 compile → full restart), then verify the TCP reconnect.',
          link: '#routines',
        },
        {
          q: "A knob change didn't do anything — why?",
          a: 'Either it wasn\'t saved (toast + saved-chip), the value is above a code ceiling, the guardrail master is OFF, the knob has no reader (the guide card says so), or the reload that Save triggers failed — Save reloads every running trader bound to the strategy, and on a failed reload the log says "saved but trader reload failed … restart to apply".',
          link: '#settings',
        },
        {
          q: 'Why does the guide banner warn about revision drift?',
          a: 'The banner compares the commit this page bundle was BUILT from with the commit the running bot reports (first 12 characters). Amber means only that the two differ — it cannot tell older from newer, and it cannot catch a guide sentence that is wrong about the very build it ships with (the guide-truth tests do that). Reload the page after an update; if it stays amber, the bot and the UI bundle are from different builds.',
          link: '#welcome',
        },
      ],
    },
  ],
}
