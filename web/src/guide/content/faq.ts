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
          q: 'What are the money numbers on the dashboard, and why do they differ?',
          a: 'Each one now says what it is (release #22). NT8_SESSION:: in the header is an ACCOUNT figure from NT8: this NT8 session\'s realized + unrealized P&L, which NT8 resets every session — it is not a total and not equity − initial balance (the old "PNL::" chip had that tooltip over the session number). The Total Equity card shows "Account vs initial" = equity − the configured initial balance, also an account figure. The "Bot P&L (record)" card is the BOT\'s own closed trades (pnl_corrected) with the manual NT8 trades on their own line, e.g. "Manual (excluded): 4 trades, −$3,202.00". It covers the SELECTED account only, while Position History\'s "Total P&L · Bot trades only" covers all accounts — so the two can differ (since release #24 each says its scope: the card adds e.g. "16 bot trades (−547.00) on other accounts not counted here", counted by the server; the history tile reads "all accounts"; which scope is the record is still an owner decision). LEDGER_DAY:: is the bot\'s P&L for the CME session day that starts 17:00 CT — the same day every risk gate counts — with unresolved rows counted and excluded and any manual rows in the day named in the chip. The position-history footer\'s "Total P&L (last N loaded)" sums only the N rows loaded in that table (resolved rows only; the unresolved count sits beside it), so it is not the all-history card; the VALUE column is entry × qty × the contract\'s $/pt (MNQ $2). The position-history footer\'s "Session day (from 17:00 CT)" total is the same figure, and it also includes partial legs already booked on trades that are still OPEN (dated by last activity); the daily-loss gate counts CLOSED trades only. When a figure cannot be computed it reads n/a or UNRESOLVED, never a zero.',
          mechanism:
            '/api/account: nt8_session_pnl, account_pnl_vs_initial(_pct), total_pnl_source; bot_record_pnl / bot_record_trades / manual_excluded_* (store.GetFullStats); ledger_day_* (store.GetLedgerDayTotal over kernel.CMESessionDayStart). The boot lines 🧾 state the corrected-column guard and the manual-origin count.',
          link: '#status',
        },
        {
          q: "Why does hoang's record no longer show the −$4,226?",
          a: 'Owner ruling R1 (2026-10-14). Four big shorts on hoang\'s account — #608, #609, #637 and #640, 40/40/20/20 contracts, together −$3,202.00 — were MANUAL NT8 trades that the reconcile loop adopted into the bot\'s ledger. They were 75.8% of the −$4,226.18 record; the bot\'s own trades come to about −$1,024 over 164 trades. They are removed from the bot record by EXCLUSION: the rows are kept, marked origin=manual, listed in the position history with a "MANUAL · excluded" badge, and shown as their own line beside every bot figure. Nothing is deleted. From release #22 any NT8 position the bot adopts with no order or arm of its own is marked manual the moment it is adopted (logged "🧾 manual origin: … flagged origin=manual"). An adoption the bot can tie to its own fill, or cannot decide about, stays a bot trade.',
          mechanism:
            'trader_positions.origin (bot|manual); store.BotOriginSQL in every P&L aggregator; boot migration RunManualOriginMigration (backup first, WHERE id AND trader id, idempotent); store.BotRealizedPnL is the bot-only day read for gates.',
          link: '#status',
        },
        {
          q: 'I restarted the bot mid-setup — is my resting order dead until the next plan read?',
          a: 'No, not since 0B (2026-09-02). A restart asks the broker to cancel pre-boot orders (the class-33 boot sweep) because the process that owned them is gone: each is logged "🛡 boot sweep cancel REQUESTED …" and held cancel_pending, and is cancelled only once a fresh NT8 order book shows it gone. Then the swept scenario re-arms under the SAME plan version, as the next placement (the cancelled row keeps its record). A cancel YOU made still sticks until the next plan version — manual-cancel-wins is unchanged; only the machine\'s own housekeeping is re-armable. Since the release #22 follow-up this holds for EVERY entry policy. An order that reached NinjaTrader and then FILLED, or was cancelled WITHOUT the bot asking (your cancel in NT8, a broker cancel, an order gone from the book), is spent for that plan version: "⏹ re-arm REFUSED … slot spent", counted arm_slot_spent. A cancel the bot itself requested (a no-trade band, an install or maintenance withdraw, a gate change, a re-spec, EOD) still re-arms within the version, as before. Before this, only market_in_zone rows were held to it: a planned_order or legacy limit you cancelled in NT8 would have been placed again under the same version, and a filled one was re-placed at the same level (rows 129, 196). A NEW plan version re-arms.',
          mechanism:
            'trader/class33_boot_sweep.go sweepPreBootArmsWith → ledger.RequestCancel (cancel_pending, BootSweepReason) → trader/cancel_confirm.go confirmPendingCancels → ConfirmCancel; store/armed_orders.go UpsertArm refuses a cancel_pending row and mints the next placement for a settled one (the "boot_sweep" state_reason prefix, store.IsBootSweepReason, exempts it from the same-version pins). store/armed_orders.go UpsertArm: terminal + same version = stays terminal (a row that reached the broker returns ErrArmSlotSpent — every market_in_zone ending; any other non-mentor policy unless cancelled with cancel_requested_at_ms > 0, the bot’s own request, stamped at the send by StampCancelRequested; mentor rows are governed by their own expiry), EXCEPT rows whose state_reason starts with "boot_sweep"; trader/armed_executor.go armSlotSpentRefused says it once and counts it.',
          link: '#guards',
        },
        {
          q: 'I restarted NinjaTrader while holding a trade — does my stop survive?',
          a: 'The stop itself usually does: working orders are re-synced from the broker. What did NOT survive, until 2026-09-07, was the bot\'s memory of it — the AddOn kept the stop/target pairing in memory only, rebuilt from nowhere, so after a restart auto-breakeven and bracket edits silently did nothing and no one was checking whether a stop was there at all. Now the bot asks the broker directly, every minute a position is open and again on reconnect, and if there is genuinely no stop it raises a P0 and places one — at the TIGHTEST stop the broker is known to have held for that trade (a breakeven or trail move this process made, or the last price the broker book showed for its stop), never wider; the plan’s original stop is used only when no such price is known, and if the latest price is already through that stop the position is flattened instead (release #22 follow-up: before it, a trade whose stop had moved to breakeven would have been re-protected at its ORIGINAL, wider stop). Two honest caveats: on the reconnect edge the answer can still read "UNVERIFIED" for a moment — since release #20 the AddOn sends an order snapshot for every SIM account the instant it connects (SendAllOrderSnapshots on connect), and the check re-asks a minute later; and protective orders are now GTC rather than Day, so a stop no longer expires at session end while the position it protects lives on.',
          mechanism:
            'trader/protection_reconciler.go adjudicateProtectionWith + knownBrokerStopFor (TCPTrader.KnownStopForSignal, nt8_order_snapshots history), on monitorTick and the dead-man reconnect edge; PlaceProtectiveStop rounds the stop AWAY from the entry (WireStop); ninjascript SubmitBracketOnEntryFill places SL/TP TimeInForce.Gtc.',
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
          a: "Since 0B every armed stop is composed: beyond the nearest seated level on the risk side plus 2 ticks, floored at 1.5×ATR5m, widest wins — and never tighter than the planner authored. The arm logs 🛑 with the chosen stop, the anchor level, the ATR floor and which one bound — and since release #22 a widening says by how much: \"(authored X WIDENED +P pts = $D/contract past the authored stop)\", each widened arm counted once (arm_stop_widened). The floor itself is unchanged (the owner's open question; #649 paid $19.56 more than its authored stop). If nothing seated sits within 3×ATR on the risk side the line says stop_unanchored and the ATR floor governs. Note the R:R gate then judges the WIDER stop, so some arms are now refused at the strategy's min R:R (2 on both bound strategies; ARM_MIN_RR was deleted 2026-09-03) that would previously have rested. Exception: a reject fade keeps the frozen structural stop it was authored with (trader/arm_stop_anchor.go). On CME futures the chosen stop is then rounded to the tick AWAY from the entry (a long's stop down, a short's up), using the instrument's own tick (M2K/RTY 0.10, YM 1.00, NQ/ES 0.25) in any symbol form, so the order NT8 receives is never inside the floor it passed.",
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
          a: 'EOD flat, and only for a Day-Plan or mentor trader: the one EOD-flat rule (enforceEODFlatAt) does nothing for a trader with neither on. For those, the session ends flat by design — resting entries are cancelled first, then positions close at market (the limit-ticks-then-market ladder runs only when the server env sets EOD_FLAT_LIMIT_TICKS above 0; default 0). The session registry enforces it for all three sessions. Exception: a SWING4H mentor position is exempt from this 14:45 flat and held on its 4h candle (its arms are not cancelled either) — but a swing is never carried through a market close: the swing hold is cut at the 16:00 CT halt or a weekend, and the position is flattened on the last bar before it.',
          link: '#trading-day',
        },
        {
          q: 'NT8 is down — what do I do?',
          a: 'Nothing on the bot first: no bars means no decisions, and the gates (feed down / dead-man) say so. Fix NT8 + the AddOn connection (the Update button builds, places and verifies the AddOn DLL for you — no manual copy or F5, and it also works with NT8 closed; the AddOn only loads at the next NT8 start), then verify the TCP reconnect. Right after the link comes back (or the bot starts) the bars it holds are the old ones from the store, so for up to 90 s it says “⏳ feed: waiting for the first live bar after reconnect” instead of FEED DOWN, and counts it; the first live bar ends the wait, and if none arrives in those 90 s “🚨 FEED DOWN” fires exactly as before.',
          link: '#routines',
        },
        {
          q: 'The chart has a gap or a jump after NT8 lost its connection — do I restart?',
          a: "No. The bot finds the missing minutes itself ('🕳 bars HOLE detected') and asks NinjaTrader for them again at +1/+3/+10/+30 min once the feed is back; '✅ bars hole FILLED' means done. Only if it prints '🚨 bars hole UNFILLED … NinjaTrader has no history for <from>–<to>' do you act. First read the end of that line: if its replay overlap verdicts show disagreed above 0, a replay NT8 sent was on another price scale and was held out of the store, so the window can be empty for a scale reason rather than missing history — read the 🚨 scale lines before reloading. Otherwise NT8 lacks the data: right-click a chart of the symbol that line names (MNQ or ES) → Reload All Historical Data, and the bot picks it up on one of its next 15-minute tries (8 at most per hole, about 2 hours); after those are spent a second message tells you to restart the bot after the reload. A hole older than NT8's standard replay (its last 2000 1m bars) is asked for with a DEEP 1m request (at most 4800 bars, about 3.5 CME sessions; 2 scheduled asks + 2 slow ones) — a bot restart does not reach it, so for that case the phone message names the by-hand POST /api/nt/bar-arbiter backfill instead of a restart. Only a hole further back than 4800 bars is RETIRED: no re-request can bring it back. The persistent gap audit (every 10 minutes, last 7 CME session days, cross-checked against 3m/5m) finds such holes and keeps them listed; the dashboard shows a red 'MISSING BARS: <symbol> <window CT>, <n> min' line while a hole is unfilled or retired: an unfilled one adds 'NT8: Reload All Historical Data'; a retired one says it is further back than the bot can re-read and clears when it leaves the audit window. GET /api/health → bars_holes shows what is still open.",
          link: '#status',
        },
        {
          q: 'The dashboard says TRADING PAUSED — missing bars … what do I do?',
          a: "Nothing is wrong with your settings: the bot found 1m bars missing from the data its decisions read (or cannot tell yet) and refuses every NEW entry until they are back — financial product, no trading on lost data. Exits, stops and protection keep working. Resting entry orders are cancelled. It resumes by itself when the bars are measured filled or the stretch ages out of the window. To speed it up: if the stretch is recent (inside NT8's last ~2000 1m bars, about 33 trading hours) open a chart of the symbol in NinjaTrader, right-click → Reload All Historical Data, or just wait for the bot's own re-requests. An OLDER stretch cannot be reloaded back: the pause text then says when it clears by itself (it leaves the data window) and that importing the missing minutes (POST /api/admin/bars/import, HISTORICAL_IMPORT_SEAM=on) clears it sooner. If the banner names a scope (e.g. 'SWING4H entries only') the other setups are trading normally. GET /api/health → trading_paused names the window, the scope and the reason.",
          link: '#status',
        },
        {
          q: "A knob change didn't do anything — why?",
          a: 'Either it wasn\'t saved (toast + saved-chip), the value is above a code ceiling, the guardrail master is OFF, the knob has no reader (the guide card says so), or the reload that Save triggers failed — Save reloads every running trader bound to the strategy, and on a failed reload the log says "saved but trader reload failed … restart to apply".',
          link: '#settings',
        },
        {
          q: 'Why does the guide banner warn about revision drift?',
          a: 'The banner compares the commit this page bundle was BUILT from with the commit the running bot reports (first 12 characters). Amber means only that the two differ — it cannot tell older from newer, and it cannot catch a guide sentence that is wrong about the very build it ships with (the guide-truth tests do that). Reload the page after an update; if it stays amber, the bot and the UI bundle are from different builds. Two other banners are not drift: "NOT A RELEASE BUILD" means this bundle was built without a release sha (a development build, rev "dev") and is never treated as matching; "CANNOT VERIFY THE BUILD" means the bundle carries no usable sha (rev "unknown") or /api/health did not answer (web/src/guide/guideRevBanner.ts).',
          link: '#welcome',
        },
      ],
    },
  ],
}
