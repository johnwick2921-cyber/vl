import { GUIDE_BUILT_REV, type GuideSection } from '../types'

export const status: GuideSection = {
  id: 'status',
  num: 10,
  title: 'Status & Signals',
  tagline: 'Every indicator strip, banner, and log line — decoded.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    { kind: 'h', text: 'Scenario activation and order authorization' },
    {
      kind: 'p',
      text: 'Scenario activation describes the evaluator’s view of the setup. In activation window and confirmation MET do not authorize or place an order. The separate order authorized chip means the ledger has an authorization; working, filled and cancelled describe that ledger record, not a direct broker settlement. Prices and order selection are addressed separately. CANCEL PENDING is a fourth state and it means exactly what it says: a cancel was sent and NO broker book has confirmed the order is gone, so the order may still be resting. Since 2026-09-10 a cancel that times out, or that could not be sent because the NT8 link was down, is HELD at cancel pending rather than written cancelled — cancelled is the word that frees the slot for a replacement, and an unconfirmed cancel must never free it. The settlement pass confirms it against a snapshot or re-requests up to a cap; only a book that no longer lists the order may promote it.',
    },
    { kind: 'h', text: 'Dashboard layout and Desk loading' },
    {
      kind: 'p',
      text: 'Overview shows Market Chart beside Account Equity on wide screens and stacks them on narrow screens. The futures Planner, including Desk, follows both charts. Switching to Decisions hides Overview without unmounting Planner: its polling and local state continue. Entry, Mark and Value remain available in the horizontally scrollable position table on phones. The mobile market selector uses the same market choices as the desktop pills. While the first Desk read is pending, DESK shows Loading; this is not a claim that any fact is current. The Desk toggle announces whether the rows are expanded or collapsed.',
    },
    { kind: 'h', text: 'Research snapshot recorder' },
    {
      kind: 'p',
      text: 'The research archive (data/data.db.research.db) keeps every fact the pipeline records; nothing here changes what the bot trades. The recorder is ON unless RESEARCH_SNAPSHOT is explicitly 0 or false in .env. When on, it writes one rollup line per minute (RESEARCH_LOG_EVERY_S, default 60) with rows-per-object, drops and queue depth — there is no per-fact narration any more. Drop notices are WARN-level, coalesced to one line per minute with the delta. RESEARCH_RETAIN_DAYS (unset = never prune; set = prune) removes rows older than that many days in bounded batches after boot and daily, and the archive is never VACUUMed automatically — on a ~77 GB file that step is a manual, owner-approved one.',
    },
    { kind: 'h', text: 'Where this page comes from' },
    {
      kind: 'p',
      text: "The UI you are reading is served by the bot's own process at http://localhost:8080. That is the production path and the one this Guide assumes. Until 2026-09-03 there was no production path at all: the interface was served by a Vite DEVELOPMENT server on port 3000, started by hand, supervised by nothing, and the built bundle on disk had been stale since 08-31 while the Go server answered 404 at its own root. A reboot of the machine, or anything that stopped that one node process, took the whole interface with it and left the bot trading blind to its operator. Port 3000 still works and is still the right thing to use while developing — it rebuilds on save — but nothing depends on it any more.",
    },
    {
      kind: 'p',
      text: "The boot log says which path is live and how old the bundle is: '🖥 ui: served-by=go-static build=<timestamp>'. If that bundle is older than the binary running it, the line adds STALE and how far behind it is, and it is logged as a warning rather than as information — because a stale bundle means the screen is showing you a build that is not the one making your trading decisions. 'served-by=none' means no bundle was found at web/dist; the API keeps working and only the interface is missing, which is why the bot does not refuse to start over it.",
    },
    { kind: 'h', text: 'What the E8 side-table can be used for' },
    {
      kind: 'p',
      text: "The A/B counterfactual table records what a confirm rule WOULD have done. Until 2026-09-03 every short row in it was arithmetic across two price spaces: the replay mirrored stop/target into negative prices so the excursion signs read nicely, while the fill and the stored bracket stayed real — so risk came out as 58 430 instead of 21.50 and RR sat pinned near −1. Measured with direction read from the plan: 121 short rows, 109 with a broken RR; all 67 long rows were always clean. The boot line now states what survives: 'e8: rows=188 usable=55 · unrecomputable fill-bar=54 no-inputs=12'. USABLE is the only count a ruling may rest on. The 54 fill-bar rows keep their numbers and are labelled, because the same bug also broke the close-rule comparison — their fill came from the wrong bar, and clean arithmetic on a wrong fill is a precise answer about the wrong moment.",
    },
    { kind: 'h', text: 'Why a green test suite has an expiry time' },
    {
      kind: 'p',
      text: 'On 2026-09-03 a suite was verified green at 11:00 and was red at 14:50 with no code change in between, and both readings were honest. A test pinned its fixture to a fixed date but called production code that asked the operating system what time it was; once a cadence guard began enforcing, the answer started depending on how many minutes were left before the session flat. The lesson is not about that one test: any rule that consults the clock makes every test that reaches it a function of the hour it ran.',
    },
    {
      kind: 'p',
      text: "The repair is a seam. The entry point keeps reading the wall clock and does nothing else; the rule underneath takes the time as an argument. Production behaviour is identical — the bot still asks the OS — but a test can now state its own hour instead of borrowing the machine's. Thirteen time-dependent rules are listed in clock-seams.list at the repo root, and a lint test reads that file and fails the build if any of them loses its seam or grows a second line. Adding a new time-dependent rule without one does not compile past the gate.",
    },
    {
      kind: 'table',
      title: 'Clock seams — every time-dependent rule and its status',
      head: ['Rule', 'Seamed'],
      rows: [
        ['latestClosedPrimaryBarMs — which bar has closed by now', 'yes'],
        ['recordClosedTradeAnalytics — MAE/MFE + adherence at exit', 'yes'],
        ['maybeRecordClosedTradeAnalytics — the once-per-close stamp', 'yes'],
        ['entryBlockedByLastEntry — the per-session last-entry gate', 'yes'],
        ['enforceEODFlat — the session flat', 'yes'],
        ['enforceT1ForceFlat — the T1 force-flat', 'yes'],
        ['observeTransitionStanddown — the transition timer', 'yes'],
        ['maybeWakePlannerOnMSS — the structure wake', 'yes'],
        ['maybeWakePlannerOnLevelEvents — the level wake (fixed first)', 'yes'],
        ['weeklyConfluenceShadow — the weekly shadow read', 'yes'],
        ['weeklyScenarioGrade — the active-session grade', 'yes'],
        ['ResetDailyPnL — the manual daily-window reset', 'yes'],
        ['barPersistSummary — the 60s counter summary', 'yes'],
        [
          'ForceReset poll deadline — a real wait, not a rule',
          'deliberately not',
        ],
        [
          'tickOnce — loop entry; its clock use already delegates',
          'deliberately not',
        ],
        [
          'NowCT — the clock accessor itself; consumers all take a time',
          'deliberately not',
        ],
      ],
    },
    {
      kind: 'p',
      text: 'The three marked \u201cdeliberately not\u201d are listed with their reasons in the same file. An unexplained absence from a list is how the list stops being trusted, so the exclusions are written down beside the inclusions rather than left to be rediscovered.',
    },
    { kind: 'h', text: 'The update hold (maintenance)' },
    {
      kind: 'p',
      text: "When an update is about to replace the bot or the NinjaTrader AddOn, it first puts the whole installation on HOLD. The hold is one file (data/updater/hold.json). While it is present, no NEW entry is sent from anywhere: the AI's opens, armed orders, Picture HTF entries and new planner reads are all refused, and the NinjaTrader AddOn refuses new entries too. Everything that protects or closes a position keeps working: stops, targets, breakeven and trailing moves, cancels and closes. An arm that could not be placed stays armed and places once the hold clears. A Picture HTF opportunity seen during the hold is refused for good, because its entry window is only seconds long. A file that exists but cannot be read counts as held.",
    },
    {
      kind: 'p',
      text: "Resume does not lift the hold. It clears a trader's own pause and nothing else, and the log says new entries stay refused. Nothing in the web app or the API can write or clear the hold. Only the local operator tool (maintenance-hold set | status | clear) can, and from the one-button update, the updater itself. The tool refuses to run as root, and refuses unless it finds the bot's database in the folder it is pointed at (--install-dir, the bot's own folder), so a hold is never written where the bot does not look. 'status' shows the path it read. A plan reset or re-read is refused with 'an update is in progress — plan reads resume when it completes'.",
    },
    {
      kind: 'p',
      text: "A hold can also WITHDRAW resting entry orders: 'maintenance-hold set --withdraw-entries' writes the hold with withdraw_entries, and only that flag asks for it — a plain hold never withdraws anything. Two money-safety trips withdraw on their own, with no hold: the consecutive-loss breaker and the daily force-flat, so an entry already resting at NinjaTrader cannot fill after a loss limit tripped. The withdraw cancels ENTRY orders only — armed orders carrying a broker signal — through the filled-order guard the armed cancels use; stops, targets and closes are never touched, and an order that may already have filled is left alone and logged. Armed orders are the whole set of resting entries: the AI enters at market, and since W5 a Picture HTF opportunity is itself an armed order (a market_in_zone limit), withdrawn like any other. A cancelled order is shown as pending until NinjaTrader confirms it — an order update saying cancelled, or its absence from a fresh saved order snapshot — and is never shown as done before that. An arm that was never sent stays armed and is refused by the entry rules. GET /api/maintenance shows withdraw for the current hold's job: pending (asked, no answer yet), confirmed (cancelled, on evidence), filled (the entry filled before the cancel landed — that is a position, never a withdraw) and ended (any other end, with its state). When the bot could not read its records the lists are absent and 'unread' says why — never an empty list for rows nobody read. withdraw is null when the hold did not ask for a withdraw. A cancel re-request the filled-order guard refuses is not sent and is not counted as a re-request.",
    },
    {
      kind: 'p',
      text: "Where to read it: the 🔒 maintenance boot line; GET /api/maintenance (held, job, since, sends still in flight, whether the bot has drained, and the AddOn's acknowledgement); and GET /api/installation-gate, the one verdict an update needs before it may continue. That gate checks every trader and every NinjaTrader account at once. It fails while any planner read is running, while any entry send or queued entry is in flight, while any trader is not a NinjaTrader TCP trader, while the AddOn has not acknowledged this hold, while any non-SIM connection is connected, while any NinjaTrader connection is between states (connecting, connection lost), and while any account holds a position or a working order of any kind. A trader that ran once and has since been removed is listed but only blocks the gate if it is still running. Any leg it cannot check counts as a failure. The Settings → Updates page shows the hold, the drain, the AddOn ack and the gate verdict all on one screen, refreshed every ten seconds. The gate-block table counts refusals as 'maintenance_hold'. An entry that was waiting in the reconnect queue when the hold landed is dropped, never sent later, and counted as 'maintenance_drop'. If a write of it had already started, it is counted as 'maintenance_drop_attempted' and stays pending until it is reconciled with NinjaTrader.",
    },
    {
      kind: 'h',
      text: 'The "rev" on every guide section, and what a release is',
    },
    {
      kind: 'p',
      text: "Every section of this guide shows a rev in its header. That is the commit the guide was BUILT for, and the banner at the top compares it against the bot that is actually running. If they disagree the guide is describing a different build than the one taking your trades, and it says so rather than letting you read the wrong thing. A build that is not a release shows 'dev' — never a real-looking commit id, because a plausible id you cannot check is worse than an honest 'dev'.",
    },
    {
      kind: 'p',
      text: 'That rev is now supplied when the guide is built, instead of being typed into a source file by hand. The hand-typed version went stale: updating it was a step in the deploy procedure, and a step in a procedure is a step that gets skipped when someone is in a hurry — which is exactly when a guide that disagrees with the bot does the most damage. A release build with no rev, or a rev that is not a real 40-character commit id, now FAILS to build at all rather than shipping something unverifiable.',
    },
    {
      kind: 'p',
      text: 'A release itself is cut from a tagged, approved commit — never from whatever happens to be on the development branch at the time — and, once the release environment is set up with you as its required reviewer, nothing is produced until you approve it. Until that is set up the build still runs every check below, but the approval step is not there to stop it — so treat "nobody can cut a release without me" as true only after you have set it up. The build proves it came from a clean tree, packages only an allow-list of files, scans both the staged files and the finished archive for anything secret, and signs a manifest that is checked before the archive is published. The installation keeps its own data and settings: they are never shipped inside a release and never overwritten by one.',
    },
    {
      kind: 'p',
      text: 'Before a release can claim you are able to go BACK to an older version, that rollback is actually performed first: the old build creates a database, the new build upgrades it, and then the old build is started again on the upgraded database. That last step is the one that matters, because an upgrade that removes something the old build still reads only fails at the moment you need to go back. A version pair that has not passed all three is published as untested rather than quietly left out.',
    },
    { kind: 'h', text: 'What happens when the bot updates itself' },
    {
      kind: 'p',
      text: "An update is performed as a sequence of steps, and each one hands back a receipt saying what it actually did — not what it intended to do. The receipt records the values the step READ: the revision stamped into the new program, the checksum of the file, whether the database copy opened. If a step refuses, it still gives you its receipt, because the moment a step fails is the moment you most need to know what it saw.",
    },
    {
      kind: 'p',
      text: "Nothing is swapped until the new program has been proven to BE the version it claims. If it carries no build stamp at all, the refusal says so and tells you the cause — a program built in the wrong kind of folder carries no stamp, and that is a different problem from having the wrong program. Two different causes never share one message, because a message that names the wrong cause sends you to fix something that is not broken.",
    },
    {
      kind: 'p',
      text: "Then the database is copied while the bot is still running, and the copy is OPENED and checked before the update continues. A backup nobody has opened is just a file, and the moment you need it is the worst possible moment to discover that. Only after that do the three pieces of an installation move together — the program, the screens you are reading now, and the marker that records which version is live. All three or none: an update that swapped only the program would leave these screens describing a different build than the one taking your trades.",
    },
    {
      kind: 'p',
      text: "Restarting is deliberate about WHICH program it stops. It identifies the running bot by more than its process number, because numbers get reused, and stopping the wrong thing because it inherited a number is exactly the accident worth preventing. If it cannot confirm the bot is still the one it measured, it refuses and changes nothing.",
    },
    {
      kind: 'p',
      text: "Afterwards the new version has to prove itself twice: it must write a fresh start-up line NEWER than the restart, and it must report the expected version when asked directly. An old line from a previous start does not count — that line was written by a program that is no longer running and says nothing about the one that is. If either proof is missing, the update rolls back to the previous version and then proves THAT one came back, rather than assuming it did.",
    },
    { kind: 'h', text: 'One entry at a time (the entry latch)' },
    {
      kind: 'p',
      text: "Four ways could send a new entry to NinjaTrader: the AI decision, an armed order from the plan, a Picture HTF entry, and the side doors (agent chat, the debug test trade, the test-arm check). Since W5 Picture HTF has no send of its own — its opportunity is a Day Plan scenario that the plan's armed executor places — so three remain. Each used to check only its own records, so two of them could each send an entry for the same account and instrument before either fill was visible to the other. Every entry now passes one latch inside the NinjaTrader connection, per account and instrument. The latch refuses the entry when the order book is older than two snapshot intervals or missing, when the book shows a working entry order or the account holds a position on that instrument, when an armed or Picture order is placed and not yet finished, when an entry was sent and has not yet filled or been rejected, or when an entry was sent on that account and instrument in the last 60 seconds. Stops, targets, breakeven moves, cancels and closes never pass through it. A refusal is counted as 'one_entry_latch:<reason>' in the gate-block table and logged once per change of reason. An arm that is refused stays armed and places later. An explicitly authored exit leg is also refused while a position is open (no stored plan has ever authored one).",
    },
    {
      kind: 'p',
      text: "The boot line '🚦 entry latch' READS whether the latch has its evidence: latch=wired means it checks everything above; latch=UNWIRED means nothing installed its evidence and it lets every entry through, which is a defect to report, never a setting.",
    },
    {
      kind: 'p',
      text: "Inside one trader, an AI decision's entry and an agent-chat entry are sent one at a time, under one lock: the send — and, on a broker that takes the stop and target from a separate set before the send, that set too — happens inside the lock, and the stop and target set again after the send take the same lock. An AI decision and an agent-chat entry sent at the same moment therefore each go out with their own bracket, never each other's. On NinjaTrader nothing is set before the send: the entry carries its own stop and target inside the send itself, and the stop kept on record for that side is written only once the send was attempted, so a send refused before it reached NinjaTrader leaves no bracket behind and a later breakeven move is judged against the real stop. A send whose outcome is unknown keeps its stop on record, because the entry may be live with exactly that stop. Armed orders and the debug test trade do not take this lock: each carries its own stop and target in its order.",
    },
    { kind: 'h', text: 'One set of entry rules (every way in)' },
    {
      kind: 'p',
      text: "Every new entry now asks the same questions, in the same order, whichever way it comes: the AI decision; an armed order, at the moment it is placed; a Picture HTF entry, before it claims the opportunity and again just before it is sent; and an entry typed in the agent chat. The order is: price feed connected → the reconnect check settled (dead-man) → not frozen → boot integrity → no owner pause → no maintenance hold → contract roll resolved → the loss breaker and session risk → the last-entry cutoff → the session open (AI) or CME open (armed, Picture) → plan mode → approval → the re-entry cooldown (armed, Picture) → the entry gate. Before this each way ran its own shorter list — Picture checked only the maintenance hold, its own positions and its own pending rows — so a gate that stopped one way did not always stop the others. A refusal names the gate that refused it and is counted in the gate-block table under that gate's name. An armed or Picture refusal is logged and counted once per change of gate, not once per tick: a reason that carries a moving price (the cooldown's distance from the stop, an R:R at the live price) no longer counts as a new refusal every time the price moves.",
    },
    {
      kind: 'p',
      text: "An armed order is placed only in a pass whose plan checks passed for it in that SAME pass. Before, a leg the plan checks refused (for example after the daily force-flat tripped) could still be placed in that pass, because it was already armed from an earlier one. A refused leg stays armed and places in a later pass that admits it; it is counted as 'arm_not_admitted', once per change.",
    },
    {
      kind: 'p',
      text: "Picture HTF runs only while the trader is running and the Day Plan is on. It trades only its own instrument: an MNQ trader never acts on ES bars. Its minimum R:R is the stricter of its own setting and the strategy's. Missing evidence — no entry, stop or target, a stop or target on the wrong side, no 5-minute ATR, unreadable positions, no strategy floor — refuses. Since W5 a Picture opportunity is a Day Plan scenario (📷 P1, P2…) and the plan's own armed executor places it, so plan_gate= reads 'Day Plan scenario (market_in_zone limit) · plan_mode=<mode>' — under strict it is judged like any cited plan scenario, never refused for being Picture — and the plan card shows that route in neutral, with a red refusal only when one is real (the trader stopped, the Day Plan master off). The executor runs every shared plan check on it (direction bias, minimum scenario quality, the HTF veto, shadow, the entry gate), never widens its stop and never swaps its target, and requires R:R at the far edge of its zone to clear Picture's own floor. It is placed only inside its eligibility window and only by the run that recorded it: an unplaced row past the window is closed 'picture eligibility window closed — never placed', a resting limit past it gets its cancel requested, a row recorded by a previous run (a reload) is closed 'recorded by a previous run (epoch …, live …) — not placed'; Stop and Day Plan OFF close its unplaced rows and request the cancel of a resting one; at Stop with no fresh broker book the resting row is held cancel_pending and no cancel is sent blind (it is re-requested once a fresh book exists). A late Stop from a trader instance that a reload has already replaced leaves the replacing run's rows alone; Day Plan OFF still closes every Picture row of the trader. Under plan_mode=direction a Picture scenario on a machine plan (no AI plan yet — its 'neutral' bias means no AI opinion) is judged on its own direction; on an AI plan the AI's bias governs, and the refusal names which rule judged it ('direction: plan bias short'). It is back-inside-invalidated when the newest completed 5m close returns into the broken 4H body. one_setup does not apply to it (one_setup governs planner plays only), and a Picture placement never cancels the plan's unplaced planner arms (nor a planner placement a Picture arm): the one-live-entry guards refuse the second order until the first is closed. A refusal before it claims the opportunity writes nothing, so a passing pause or feed flap does not kill that hour's opportunity; the next frame asks again. A hand-off refused before it recorded a scenario settles the row 'refused' (never sent) instead of leaving it ambiguous; a recorded one settles 'planned'. Picture itself never sends: its old market-entry send is retired.",
    },
    {
      kind: 'p',
      text: "Picture only draws the higher picture when it can prove it has the history for it. It needs a full window of COMPLETED 4-hour candles and refuses until it has them, saying how many it actually counted ('insufficient depth 118/124') rather than a bare refusal. A candle counts as completed only when the AddOn proved it closed — a candle still forming is never treated as finished data, and a pivot is only drawn when all four of the candles around it have closed, so a level can no longer be declared on partial evidence or be used before the candle that confirms it has closed.",
    },
    {
      kind: 'p',
      text: 'It also checks the bars belong to the instrument it is trading. Every bar frame names its contract, and a frame naming a different front month than this trader is on is ignored and counted — across a contract roll the symbol alone cannot tell two contracts apart. Before the first connection acknowledgement the contract is not yet known; those frames are still used, but the count of them is on the 📷 boot line so the gap is visible rather than silent, and it says so in the log if it lasts more than a minute.',
    },
    {
      kind: 'p',
      text: 'The entry window belongs to the 1-hour close that confirmed the break — the 5-minute interval that follows it, once. It used to be measured from whichever 5-minute candle was newest, so every new candle pushed the window forward and a break confirmed hours earlier stayed enterable. Freshness is judged on when the data was EMITTED and when we received it, not on how long ago the candle closed: NinjaTrader sends a closed candle on the first tick of the next one, so an ordinary few seconds of lateness is late, not stale, and is still tradeable inside the window. A candle that only reaches us after the window shut is reported as never actionable rather than refused — there was no opportunity to turn down.',
    },
    {
      kind: 'p',
      text: 'A bar frame that arrives long after it was sent is kept as data but is not treated as news: it goes to the chart and the history, and does not trigger an entry. A stopped trader stops receiving frames entirely, and an evaluation already under way when a trader is stopped or restarted is refused at the last step before the order — so a frame in flight can never send on behalf of a trader that is gone. The 📷 boot line READS all of this: the contract in use (or n/a before it is known), and the counts of foreign-contract frames, unknown-contract frames, frames refused as too old, frames whose age could not be judged, and evaluations skipped for want of a completed candle.',
    },
    {
      kind: 'p',
      text: "An entry typed in the agent chat goes through the same chain as an AI decision, plan mode included: under strict it is refused. A chat entry must carry its own stop AND target: without both it is refused when proposed, before you are asked to confirm, and again at the gate ('no explicit stop', counted under entry_gate). With them, the entry gate's R:R and stop-distance checks judge it like any decision, on its own stop and target. The reply says what happened: 'refused by the admission gate' only for a gate refusal; 'send failed at the broker' for a broker error; 'OPENED but UNPROTECTED' if the broker opened it and its stop/target then failed to set (set a stop at once). The stop floor's ATR is read from the NT8 bar cache, so on any venue other than an NT8 (CME) trader a chat entry is refused ('ATR5m unknown').",
    },
    {
      kind: 'p',
      text: "Where a chat entry goes: a CME symbol typed in the chat — MNQ, ES, NQ and the rest, in any form (MNQ, MNQU6, MNQ.c.0) — is never read as a stock ticker. It goes only to the one running NinjaTrader trader whose instrument has that root, and is refused by name if none is running or if two or more are: the chat never picks an account for you. A stock whose ticker is also a CME root (CL, ES, NG) is read as the future, and a chat close of a CME symbol goes to that NinjaTrader trader too. So a chat entry can be sent on an NT8 (CME) trader, and only there. It is then sent through the AI decision's own execute path: reconcile-before-open (which, exactly as for an AI entry, may first flatten a position no ledger row explains), the max-positions limit, the same-side check, and an order row under its signal id, so its fill is tracked like an AI entry's. The contracts you typed are sent as typed or refused — 'refused: quantity N exceeds the max-contracts cap C', or 'refused: quantity N is not a whole number of contracts' — never reduced. The order carries your own stop and target, rounded to the tick like every entry (the stop away from the entry). A refusal by those rails replies 'entry refused before any send (the same execute-side rails as an AI decision): <reason>'. If reconcile had already sent a flatten first, the reply never says the orphan was closed — the flatten may have failed or not been confirmed in time; it says 'a flatten of a position no ledger row explains was SENT first (reconcile-before-open) — it is flat only if the reason below says so, check NT8; the entry was NOT sent: <reason>'. A refusal inside the NinjaTrader connection (the maintenance hold, the entry latch, an unbound or non-SIM account, the duplicate-order guard) is a broker-side failure, not a refusal by those rails: the reply says 'entry send failed at the broker (not an admission refusal): <error>', and if an orphan flatten went out first the error reads 'an orphan flatten was sent first (reconcile-before-open); then the entry failed at the broker: <error>'. On NinjaTrader the reply waits about 3 seconds for the fill confirmation, and up to about 35 seconds more when an orphan must be flattened first.",
    },
    {
      kind: 'p',
      text: "Before an AI entry, a position NinjaTrader already holds is flattened only when nothing on our books explains it. It is left alone — and the AI entry refused, counted as 'reconcile_owned' — when, for that account, instrument and side, an armed order carrying a broker signal or a sent Picture entry is live; when an open position's entry order is one of those; or when one of them filled in the last two minutes (twice the reconciler's grace) on any trader bound to that account, running or stopped, and is not recorded as a position yet (a fill recorded a moment after the check began counts as inside those two minutes, never as too old); or when another trader has an open position recorded on that account, instrument and side, however old. A warning names that row. If NinjaTrader no longer holds that trader's position, the row is stale: whenever NinjaTrader holds a position on that side of this account and instrument, the stale row explains it, so every AI entry there is refused and a real orphan on that side is never flattened, until the row is closed (a known limit; a detector for stale rows is owed). If any of these records cannot be read, the position counts as explained: the AI entry is refused and nothing is flattened. A position nothing explains is flattened as before.",
    },
    { kind: 'h', text: 'Market data on the futures path' },
    {
      kind: 'p',
      text: "On the CME futures trading path — every AI entry, close and entry check — the bot reads only NinjaTrader's own bars and makes no call to Binance or any other outside market-data service. Open interest and funding are crypto-perpetual ideas with no CME symbol behind them, so on futures they are shown as n/a, never a made-up 0: in the AI prompt (when the Open Interest indicator is switched on) and on the 📊 market data boot line. A NinjaTrader trader set up with a non-CME symbol is refused by the market read rather than sent to a crypto data source, and the boot line names it as REFUSED. Before this, every AI entry, close and entry check asked Binance for MNQ's open interest and funding. That request could never succeed and put a third-party network wait inside an entry decision. In the AI decision prompt on the crypto path, a funding value that could not be fetched now reads n/a instead of 0. Not yet removed (the next step, W-NO-BINANCE Part B): the chat assistant's background market watcher and daily briefs, and the chat page's price ticker, still ask Binance for prices; they are not part of trading.",
    },
    {
      kind: 'p',
      text: "A '1h' or '4h' price change means that much wall time. It is measured on bar close times: the latest close against the close of the bar that closed at least 1 hour (or 4 hours) earlier, on the finest bars the read has. When the bars do not reach back that far, or the chart's timeframe is too coarse to measure the window (fewer than 4 bars fit in it — a 1h chart cannot give a 1h change, only 'the previous close'), the change reads n/a, never 0. It appears on the crypto paths only: the BTC line in the AI prompt, the grid prompt and the assistant's market context; the MNQ decision prompt does not print it. Before this (W1, 2026-09-23) the futures '1h' was 100 minutes of 5m bars, the '4h' was the previous bar's close, and a short series read 0.",
    },
    { kind: 'h', text: 'The boot ledger, line by line' },
    {
      kind: 'code',
      title: 'the lines printed at startup, in order',
      lines: [
        '🔐 BOOT INTEGRITY OK — rev <sha> [+dirty] · built <ts>',
        "📷 picture-htf: mode=<on|off> rule=v1 SIM-only data=… addon=<proven|not proven> (build=…, need ≥ …) plan_gate=Day Plan scenario (market_in_zone limit) · plan_mode=<advisory|direction|strict> — per trader, READ: Picture's route and the plan mode it runs under (W5: there is no strict refusal any more)  ← W-EXEC-TRUTH W0b, W5",
        '🖼 picture rows in flight (the latch\'s + installation gate\'s set, every trader): <none|N — #<id> <stage> submitted_at=<time CT|n/a>; …> · armed picture orders: <none|M — #<id> <state> signal=<short-id|n/a>; …> · run_epoch=<epoch|n/a> — per trader, READ from the Picture ledger, the non-terminal armed_orders rows with source=picture (a live Picture order since W5), and the live run epoch; a row listed here holds the entry latch ("latched by picture row #<id>")  ← W-EXEC-TRUTH W5, U4',
        '📷 one_setup=ON[O|I] governs planner plays only — Picture scenarios are admitted by their own switch … — a WARN, printed only while Picture is on and one_setup resolves ON  ← W-EXEC-TRUTH W5 (D8 ruling)',
        '📊 market data: futures traders=<n> bars=<NT8 BarCache|UNWIRED|n/a> · oi/funding: <n/a (no external market data on the futures path)|n/a (no NinjaTrader trader loaded)>[ · REFUSED on the NinjaTrader venue (non-CME symbol): <symbols>] · non-futures traders=<m> — every field READ: the counts from the loaded traders, bars from whether the NinjaTrader bar feed is wired, oi/funding from the market read\'s own route decision over the symbols those traders trade  ← W-NO-BINANCE A (replaces the old "Using CoinAnk API for all market data" line, which was wrong on futures)',
        '🚦 entry latch: latch=<wired|UNWIRED|n/a> key=<ACCOUNT|SYMBOL> book≤<2×snapshot interval> recent=1m0s — per trader, READ from the NinjaTrader connection: wired means the one entry latch has its book and ledger evidence  ← W-EXEC-TRUTH W0a',
        '🔒 maintenance: hold=<clear|held|unreadable|unconfigured> job=<id|n/a> since=<time|n/a> addon_ack=<held|released job=<id> build=<id>|n/a> — the installation update hold, every field READ. addon_ack is n/a at startup because the NinjaTrader AddOn has not connected yet; an AddOn older than 2026-09-22-m2 never acks, so it stays n/a  ← W-ONE-BUTTON M2',
        '🧾 P&L surfaces: <N> aggregators strict-corrected, 0 raw (corrected-column guard) — every P&L figure the model and the dashboard read is pnl_corrected; unresolved rows are counted and excluded, never coerced',
        '🛑 exits: stop=max(anchor+clr, 1.5×ATR5m) · anchor_max=3.0×ATR5m · BE=n/a(strategy) · trail=n/a(strategy) · seam=SUSPENDED(env) · size=1 · re-arm-after-sweep=on (0B) — the whole exit posture, every field READ from the source the mechanics honour: BE/trail print n/a until each trader loads its strategy, then on/off from the strategy toggles; seam comes from env EXIT_MECHS_SUSPENDED (class NN)',
        '⏱ wakes: cutoff=25m(enforce) cooldown=30m(enforce, fast-market≥1.5×ATR exempt) cross-session=on stale-arm-expiry=on (class 47) — ENFORCING since 2026-09-03: a level_event wake with under 25 min to the flat is SKIPPED (its read would land after the last-entry gate closes), and so is one within 30 min of the last wake-authored version — UNLESS price has drifted ≥ FAST_MARKET_ATR (1.5×) from the plan being traded, which bypasses the cooldown and logs "cooldown bypassed: fast market <drift>×ATR". The 25-min cutoff is never exempted: a re-plan with 20 minutes left is a re-plan with 20 minutes left, fast or not. Scheduled reads, death re-plans and owner resets are untouched. cross-session defers WAKES (never scheduled reads) while a planner stream is open; stale-arm expiry retires never-placed arms from superseded plan versions',
        '    · expected <sha> · goldens PASS      ← code matches deploy record',
        '🧯 nt8 history at subscribe: MNQ 1m=2000/2000 5m=2000/2000 … 1h=n/a/2000 — received/asked per timeframe; n/a is "not answered yet", never zero  ← dispatch 101',
        '🧯 ring rehydrated MNQ 1m [O 2026-09-16]: nt8=<n> store_live=<n> store_hist=<n> (post-drop excluded=<bool>) import=<n> (refused at the door — guard iii) total=<t>/<cap> — every number read; then "🧯 ring rehydrate done [O …]: <k> of <n> pairs deepened"  ← dispatch 101',
        '🧮 planner tape [NT8-only, CTO ruling 2026-09-16] @<t>: MNQ 1m contract=<c> · import rows on contract=<n> (excluded from every planner door) · tape NT8-only=<a> rows vs with imports=<b> rows (Δ<a-b>) · regime baseline NT8-only=<x> vs with imports=<y> (Δ<x-y>) · chart keeps imports, labelled  ← 101 follow-up',
        '🗺 structure: off|on(D/4h/1h)|n/a — the S1 structure-table knob, READ from the bound strategy; per read: 🗺 structure @<session>: D=<up|down|range> 4h=… 1h=… zones=<n> pd4h=<0.xx>  ← S1 (2026-09-16)',
        "📈 chart: across-roll=on[O] · prior contracts fill strictly before the current contract's first live row · step never adjusted · limit max=20000 · decision readers=current-contract-only  ← dispatch 101",
        '📜 planner playbook: playbook=v2 bias_tree=on …',
        '🛡 plan facts guards: 0-side + empty map fail-closed …',
        '🚀 planner speed wave: retry=repair stream=on stream_idle=30s stream_total=1200s …',
        '🛰 planner client: provider_row=<ai_models id> stream_idle=30s stream_total=1200s http_ceiling=600s …  ← class 37 (per trader)',
        '🧪 validator hints: N sites — condition tokens legal+live, rule tokens in-field',
        '📜 prompt/validator contract: N restrictions, all stated in prompt  ← class 38',
        '⚖ arm normalizer: legs on non-sweep → single arm + WARN  ← class 39',
        '🔁 planner stream policy (class 41): stream_tries=3 backoff=2s→15s→45s watchdog_log=on keepalive=30s serialize_executor=off resend_identical=on  ← class 41 (per trader)',
        '🛡 cutover safety (class 33): gate legs=5 · leg4=<broker|ledger (no snapshot yet)|STALE> · boot sweep cancelled <N> pre-boot arm(s) (<M> authorized-but-never-placed left for this process)  ← class 33',
        '✂ planner schema: 9 top-level fields, ALL consumed … plan JSON ~920 tokens of a 23,769-token p50 output (3.9%); reasoning is ~96%  ← root-fix part A (measured, no cut shipped)',
        '🔬 shadow A/B (root-fix part B): OFF target_n=10 done=0 … promotion criterion: legal-rate ≥ max AND median wall ≤50% of max at n≥10',
        '🩹 repair (class 44): contract=full-doc restated head+tail · vocab-suffix=on · law excerpts=all-matching · outcomes recorded  ← class 44',
        '📊 bars: 1w nt8_agg via 1d since 2020-11-11 · 1d nt8 since 2020-11-11 · … · ladder(1w)=[1d 1m] · native 1w EXCLUDED · retention 1m=90d coarse=forever  ← class 45',
        '🛰 planner client: tries=3 backoff=2s→15s→45s keepalive_set=30s observed=n/a watchdog=pre600s/post90s(data) resend_identical=true serialize=false storm_cap=5 trace=true  ← class 49 (every field READ from its enforcer)',
        '⏱ watchdog fired: post gap=93.2s (limit 1m30s, call age 214.0s)  ← only when a generation actually stalls',
        '🔌 conn trace: closed_by=peer_fin reused=true bytes=54986 elapsed=250.1s  ← who ended the stream (INFERRED)',
        '🌩 storm cap reached: 5 provider call(s) this read ≥ cap 5  ← a 503 burst is not retried harder',
        '⚙ config diff (studio_save): min_risk_reward_ratio 3 → 2  ← one line per RESOLVED knob a save changed, plus a config_changes row',
        '🛡 boot sweep CANCELLED pre-boot arm (class 33): <session> <S#> … signal=<id> — the process that placed it is gone  ← only when a restart orphaned a resting order',
        "🧷 brackets: entry-oco=<own(none)|SHARED(id)|n/a> · bracket-oco=<on-fill(shared)|MIXED|n/a> · state-source=<broker|none> · protective-tif=<Gtc|Day|n/a> · reconcile-on-reconnect=on · can-place-stop=<yes|no (addon <build>)> · unprotected-found=<n>  ← bracket-OCO separation. At startup most fields read n/a ON PURPOSE: they describe what the NinjaTrader AddOn does, and the bot can only know that from a book it has not received yet. A Go constant asserting the AddOn's behaviour is exactly the failure this line exists to catch — if entry-oco ever reads SHARED(...), the entry is back in its bracket's cancel group and cancelling it can take the stop.",
        "🛑 session risk: daily=<$N>[O] [DECORATIVE (guardrails master off, daily_loss_enabled off — both must be on)] · breaker=<N|off>[O|E|I] warn=<M>[I] (not master-gated[; never fires on the retained tape, max run 7, ids 585-591]) · no-trade-band=arm+decision · post-loss counter=on(<K>m) · flat@<close>=position+arms+pending  ← session risk limits. Every threshold carries its evidence tier: [O] is an owner ruling (a saved value), [E] is the process environment (e.g. BREAKER_HALT_N, used only when the strategy saved nothing), [I] is invented and not yet measured (the shipped default). The breaker is READ from the one bound strategy (W1): breaker=n/a (<why>) when zero or several strategies are bound — each trader then prints its own '🛑 [trader] breaker=…' line — and the retained-tape clause rides only an N of 8 or more. Beside it at trader load: '🧮 replan cap: strategy=… · NY=… · ASIA=… · LONDON=…' with the same origin tags, and at boot one '🩺 settings truth [<strategy id>]' line per stored strategy (stored value, what the pre-W1 binary enforced, what this one enforces, CHANGED/UNCHANGED) plus a summary; a CHANGED row no Studio save confirmed refuses its trader with a '⛔ settings truth' line. The line says DECORATIVE in those words whenever a limit is configured but not enforced, because a limit that is displayed and not enforced is worse than none — it is a limit someone is relying on.",
        '🎛 volume wave …   ← wave detector knobs',
        '🎯 touch telemetry …',
        '📐 fvg_entry …',
        '🔧 S-wave …',
        '',
        '+dirty usually = an untracked file (.env.bak…) — Go vcs.modified',
        'counts untracked files. NOT a code change.',
      ],
    },
    { kind: 'h', text: 'Which day is it? (the session calendar)' },
    {
      kind: 'p',
      text: 'CME does not simply open or close. Most US holidays are EARLY CLOSES, not closures, and the bot used to treat every one of them as a full shutdown \u2014 the code said so itself: "for v1 we treat them as full closures and refuse to trade." On Labor Day 2026 that cost a whole session: MNQ traded 980 bars across 153.50 points while the dashboard read CME CLOSED (holiday) beside a live bar, and no LONDON plan was ever read.',
    },
    {
      kind: 'p',
      text: 'A date is now one of three things, and the calendar is DATA (kernel/session_calendar.json), not code. CLOSED \u2014 no trading at all. SHORTENED \u2014 a TRADING day: reads fire, arms are allowed, and the bot is flat at the stated early close, the same discipline as 14:45 at an earlier time. NORMAL \u2014 the ordinary weekly rules decide, and a date absent from the file is normal.',
    },
    {
      kind: 'p',
      text: 'Every row cites its source, so you can tell a published fact from a decision. Where nobody has established a date it is CLOSED and SAYS SO \u2014 a guessed trading day is worse than a missed one \u2014 and the count of unestablished dates rides the boot line so an unchecked calendar cannot look like a checked one. A year the calendar has never covered falls back to the old holiday rule, which errs closed, and the boot line names the year.',
    },
    {
      kind: 'p',
      text: 'You will see it in two places. The boot line: \u201c\ud83d\uddd3 session calendar: today=shortened close=12:00 CT source=CME published \u00b7 unknown-dates=5 \u00b7 dates=14 covered=[2026] \u00b7 backoff=3m0s\u201d. And the DESK strip\u2019s MODE row, which now ends with the day\u2019s classification, its close time and where that came from \u2014 the row that used to say only \u201choliday\u201d.',
    },
    {
      kind: 'p',
      text: 'One more thing changed with it: while the market is shut the loop idles on a deliberate 3-minute backoff, which is longer than the 2-minute scan interval it was being measured against. That comparison logged 165 \u201ccycle overran the scan interval\u201d warnings in a single day, every one of them guaranteed rather than diagnostic. The closed path no longer raises it. A real overrun on a trading day still does.',
    },
    { kind: 'h', text: 'The DESK strip (top of the plan card)' },
    {
      kind: 'p',
      text: 'Twelve rows, one per fact you need mid-session, from a single read of /api/desk. Every row carries the age of the newest input it used, so nothing on it is undated. A row the engine could not compute says UNKNOWN and gives its reason — it never shows a zero, a dash, or the last value it happened to have. A source older than its own bound turns amber with its age rather than quietly showing you a stale number as if it were current.',
    },
    {
      kind: 'table',
      head: ['Row', 'What it tells you'],
      rows: [
        [
          'MODE',
          'plan_mode, session, CT clock — and process/feed/link/book named SEPARATELY, never as one green word.',
        ],
        [
          'POSITION',
          'Side, size, entry, mark and unrealized P&L in points and dollars. FLAT when there is nothing on.',
        ],
        [
          'PROTECTION',
          "The stop NT8 ACCEPTED — not the one in our ledger — with the distance and the dollars at risk if it fills. UNKNOWN when no accepted record exists yet; it never falls back to the ledger's number.",
        ],
        [
          'DRIFT',
          'Shown only when the ledger and the broker disagree about the stop. Arm 35 was 3.371527 points apart.',
        ],
        [
          'TARGET',
          'The accepted target, its distance, and the dollars if it fills.',
        ],
        [
          'DAY',
          'Realized P&L on pnl_corrected against the ENFORCED daily limit, naming its source (Studio or the env fallback) and whether the guardrails master is even on. Rows with no corrected P&L are excluded AND counted.',
        ],
        [
          'ARMS',
          'Every resting arm: scenario, side, kind, price, distance from mark, age, and whether it actually reached the broker.',
        ],
        [
          'BOOK',
          'Broker order count against the ledger, dated from receipt of that snapshot. Receipt time, true age and received AddOn build are visible together. No received book or missing link state says UNKNOWN; a fresh bar does not supply a missing link status.',
        ],
        [
          'FEED',
          'Age of the newest 1m bar, the NT8 link state, and the AddOn build the broker is actually running.',
        ],
        ['PLANNER', 'Idle, or a read in flight and since when.'],
        [
          'RANGE',
          "The session's high, low and range against ATR5m — on the CURRENT contract only. Since 2026-09-10 a range that suddenly reads ten times ATR is not a market event; it was the contract roll, and the ring no longer holds both contracts at once.",
        ],
        [
          'LAST FILL',
          'The most recent fill. Slippage reads UNKNOWN because the intended price is not stored beside the fill.',
        ],
      ],
    },
    {
      kind: 'p',
      text: 'It refreshes every 5 seconds while a position or an arm is live and every 15 seconds otherwise — the server decides which, from what is actually live. It has no unread count and nothing to acknowledge: P0 alerts are already acknowledged only 40.8% of the time (62 of 152), and a second queue would make that worse.',
    },
    { kind: 'h', text: 'PROCESS::RESPONDING (dashboard header)' },
    {
      kind: 'p',
      text: 'This header used to read SYSTEM_STATUS::ONLINE. It answers exactly one question — did the HTTP process reply to /api/health — and it stayed green through 113 minutes of feed silence on 2026-09-03. It was renamed so it can only be read as what it is. For whether the system is actually working, read the DESK strip: feed, link and book are separate facts and each is stated separately.',
    },
    { kind: 'h', text: 'SYSTEM_STATUS strip (dashboard)' },
    {
      kind: 'table',
      head: ['Item', 'Meaning'],
      rows: [
        [
          'NT8 feed',
          'TCP bridge alive + bars flowing — the single source of truth.',
        ],
        ['Boot integrity', 'Running binary == deploy record (goldens PASS).'],
        ['Dead-man watchdog', 'Kernel heartbeats ok.'],
        ['Trader frozen', 'The trader loop is stuck — investigate.'],
        [
          'Clock drift',
          'Host clock vs NT8 clock mismatch. Red-news windows widen by at most 2 minutes a side for it (CLASS 145); a reading over 5 minutes is feed age (halt or gap), not the clock.',
        ],
        [
          '402 banner',
          'An upstream model API returned HTTP 402 (billing) — the model is down for payment, not code.',
        ],
      ],
    },
    { kind: 'h', text: 'Gate-block labels — the full list' },
    {
      kind: 'code',
      title: 'every label the gate panel can show',
      lines: [
        'NT8 feed down · Dead-man watchdog · Trader frozen · Boot integrity',
        'Consecutive-loss halt · Past last-entry time · Outside session window',
        'Against the plan · Awaiting approval · Clock drift',
        'Duplicate order dropped · Order rate breaker · Burned level re-touched',
        'Night/day transition · Force-flat window (T1 lead / EOD flat)',
        '',
        'Duplicate order dropped / Order rate breaker count market, limit and stop-entry orders;',
        'a refused attempt that never reached NT8 does not block its retry.',
        'Force-flat window: with the Day Plan on, an entry refused in the 2 minutes before a T1',
        'blackout or past an in-session EOD flat — every path and trigger (scan, live-bar pass,',
        'AI decision, agent chat).',
        '',
        'Reset: at the 17:00 session roll, and on bot restart.',
      ],
    },
    { kind: 'h', text: 'Stream cuts vs connection idleness' },
    {
      kind: 'p',
      text: "GET /api/risk/stream-cuts. Every early end of an AI stream — a peer FIN ('cut') or our own watchdog ('watchdog') — grouped by how long the connection had been idle before the call reused it, with what the identical resend then did. Born from 2026-09-03 08:11:38: a planner stream died to a peer FIN at 283.4s with 50,489 reasoning chars in, on a connection reused after 101,212ms idle; the resend that succeeded rode one idle 34,935ms. If cuts cluster above some idle threshold, setting IdleConnTimeout below it is the whole fix and needs nothing from the provider. NOTHING IS SET — the ruling was three more cuts before deciding. An unresolved resend counts as unresolved, never as a loss, and a connection that was not reused gets its own bucket so fresh dials never read as evidence about idleness. idle_before_ms and conn_reused ride every ai_call log line now, so this is greppable as well as queryable.",
    },
    { kind: 'h', text: 'The tape is one contract' },
    {
      kind: 'p',
      text: "MODE now names the contract the bars are on — 'contract=MNQ 12-26 since 21:15:03 (subscribed@21:15:03)' — and, after a roll, 'ROLLED from MNQ 09-26 at …'. That value comes from the AddOn's subscription ACK, the one frame that names the instrument; it is never derived from a date, and 'n/a' means no ACK has arrived yet, never a guess. On 2026-09-10 at 21:15 CT the subscription rolled September → December on a reconnect, the bar ring kept ~2,000 September bars under the December ones, and a ~292-point step presented to every reader as a move: the desk strip showed a 359-point RANGE, and the 21:29 plan seated 7 of its 12 levels on the retired scale plus one 'fair-value gap' that was the roll itself. Now every stored bar carries its contract, the ring is purged and reseeded on roll, every reader filters to the current contract, and a bar whose window spans the roll is 'unrecomputable:spans_roll' — excluded, never read as one series, never deleted. The boot line '📜 contract:' prints the current contract with its source, the count of stored bars per contract, how many the filter kept out, and the last roll.",
    },
    { kind: 'h', text: 'One contract, two sources — every bar names its feed' },
    {
      kind: 'p',
      text: "The roll fix booted into a chart that was still discontinuous, and the research archive said why: for the 22:37 CT minute on 2026-09-10, same subscription, same label, NT8's live feed (bar_update, fact 16516009) closed at 29358.25 and its replay (bars_historical, fact 16518205) at 29068.25 — ~290 points apart for the SAME contract. A contract column cannot separate one contract from itself. Since then every bar carries its source: 'live' (the minute as it traded), 'historical' (a replay the ring has judged on the live scale), 'mixed' (a boot or roll minute with one side on each scale — never read) or 'replay:off-scale' (rows two boots wrote on the wrong scale before this fix — never read, overwritten by the next live bar or verified replay; 98 rows, MNQ 51 / ES 47, 21:15–22:38 CT that night). A replay never overwrites a live bar, in the ring or in the store. A replay is HELD out of the store until the first live bar after it lets the ring compare scales: agree → released as historical into the minutes live never wrote; disagree → discarded, the ring drops the seed, refills from the store's live rows, and a P0 prints both closes. An empty minute reads as a gap; a wrong-scale minute reads as the largest move of the day. The boot line '📼 bar source:' prints the census by source, the hold's held/released/discarded counts, both thresholds (0.50% of price AND 20× the seed's median bar body, [I]) and every mismatch this process. The NT8 side — whatever merge/back-adjust policy puts the replay on another scale — is filed for the AddOn wave with those two facts as the evidence.",
    },
    { kind: 'h', text: 'Dashboard chart depth — the store, not just the ring' },
    {
      kind: 'p',
      text: "GET /api/klines for a futures symbol splices the persisted bars table onto the older end of the live ring when the ask exceeds what the ring served (the ring caps at 2,500 bars per symbol+timeframe; the dashboard asks 5,000). The current-contract splice comes first — a ring bar is never replaced by a stored one, an EMPTY ring is never backfilled from the store (no live feed still shows no candles), and a failed store read serves the ring alone. Then, since 2026-09-16 [O], the chart shows EVERY stored contract: prior contracts fill strictly BEFORE the current contract's first live bar, every kline carries its `contract`, and the roll is a visible basis step (MNQ 09-26 → 12-26 was +301.50 at 09-07 04:40 CT) that is never back-adjusted. A 'prior' contract is literally not the current one — the current contract's imported rows that sit in holes of the prior series stay out, so a hole stays a hole rather than becoming a 300-point spike. NOFX_CHART_ACROSS_ROLL=off restores the current-contract-only chart without a rebuild; the resolved value is on the '📈 chart:' boot line. The BOT still decides on the current contract only — the decision readers are contract-pure and no level, plan or arm path reads across the roll.",
    },
    {
      kind: 'h',
      text: "Ring depth — NT8's replay, the store, and the one door",
    },
    {
      kind: 'p',
      text: "NT8 answers every subscription with up to 2,000 bars per timeframe; the '🧯 nt8 history at subscribe:' boot line prints received/asked per timeframe (n/a for a timeframe NT8 has not answered yet — HTF replays land late, and n/a is not zero). On 2026-09-16 the 5m chart was 11 hours deep with 2,000 bars delivered: a zero-bar reconnect replay had re-armed the scale check, the next live bar was judged against a reference eleven hours old, 146 points of overnight move read as a scale break and 1,999 5m + 1,832 1m bars were dropped. Since then the scale check judges NEIGHBOURS only — the replay's last bar and a live bar within two intervals of it; an older reference is a time gap, not a scale gap, and is SKIPPED with both ages printed ('🕳 scale check SKIPPED') while the check stays armed. An empty replay re-arms nothing. A CONFIRMED break asks NT8 for its full replay again — once per symbol per boot; a second break in the same boot prints 'second scale break this boot — replay on another contract, restart the AddOn' and stops, because a timed retry would loop.",
    },
    {
      kind: 'p',
      text: "Every symbol×timeframe ring is also refilled from the store — at boot and after a drop — through ONE door ['i want fuull data', 2026-09-16, superseding the 1m-only condition of 2026-09-09]: after a drop only the store's LIVE rows come back (the replay rows are what the drop judged); every refilled row enters stamped 'historical' (replay-grade to this process — never a sacred live bar, so the '📼 bar source:' census now counts store-backed rows under historical too); imported history ('historical_import', the 09-07..09-14 file) is REFUSED at that door and the per-timeframe line prints the refused count ('🧯 ring rehydrated MNQ 1m [O 2026-09-16]: nt8=2000 store_live=2500 store_hist=0 … import=0 (refused at the door — guard iii) total=2500/2500'); the contract is the current one. The store's non-1m depth on a young contract is shallower than NT8's own 2,000-bar replay, so those rings usually read 'not deepened' — the re-request, not the store, is what carries them after a true break.",
    },
    {
      kind: 'p',
      text: "The planner's tape is NT8's own (CTO ruling under the owner's delegation, 2026-09-16). Every planner door — the 12,000-bar 1m candle tape, the weekly reader's weeks and window, the POC-touch historical leg — reads only bars NT8 produced (live, or replay verified on the live scale); imported history never reaches a decision, while the chart keeps it, labelled. On 2026-09-16, 426 imported 12-26 1m bars sat inside the planner's tape. Removing them moves what the regime baseline and the levels are fed, so the '🧮 planner tape' boot line prints the import count on the contract, the tape length and the regime baseline BOTH ways, measured through the same estimator (the rule did not change — the same input both ways is a zero delta).",
    },
    { kind: 'h', text: 'Traffic light — one glance' },
    {
      kind: 'p',
      text: "GREEN = bot running, gates quiet, plan armed (or flat by plan). AMBER = gates firing repeatedly — read the ledger, don't override. RED = feed down / frozen / boot mismatch — use the emergency checklist (Section 9). The card's NO-TRADE banner is not a light: it is a state.",
    },
  ],
}
