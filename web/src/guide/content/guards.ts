import { GUIDE_BUILT_REV, type GuideSection } from '../types'

export const guards: GuideSection = {
  id: 'guards',
  num: 6,
  title: 'Guards & Safety',
  tagline: 'What can hard-block a trade vs what only informs.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    {
      kind: 'p',
      text: 'An owner plan edit names the revision it was drafted against. If the plan or its overlays moved while the sheet was open, the server refuses the stale draft (409) and the sheet shows the reason inline — a stale edit can never overwrite a newer one.',
    },
    {
      kind: 'p',
      text: 'Chat memory and clear requests belong to the signed-in owner. A numeric conversation ID in a request cannot select another owner’s history, for either normal or streaming chat.',
    },
    {
      kind: 'h',
      text: 'Structural stop and first-zone target — research candidate',
    },
    {
      kind: 'p',
      text: '[I]/[T] a codeable research candidate, not a validated replacement. No external evidence fixes its buffer or proves that it will turn the losing book positive.',
    },
    {
      kind: 'p',
      text: 'If a structural decision cannot be recorded, or an older refused authorization cannot be retired safely, new placement pauses for that cycle.',
    },
    {
      kind: 'p',
      text: 'For the reject fade, the stop is beyond the whole entry zone by the resolved buffer; the target is the near edge of the first distinct eligible zone. Freeze both, then check costs, the existing R:R floor; the existing daily-loss and other entry guards then apply. Refuse unsuitable geometry without resizing, moving the target, or changing the stop to pass. Missing frozen provenance records ATR fallback and refuses admission. Legacy point references are not reconstructed into zones.',
    },
    {
      kind: 'p',
      text: 'The provisional MNQ buffer is 4.50 points [I]: outward-rounded p95 of 6,181 in-sample held first touches. The sweep compares 0.25, 1.25 and 4.50 points. These conditional minute-bar measurements do not establish a 95% win rate or a profitable strategy. The plan card shows the recorded composed prices and exact refusal reason.',
    },
    { kind: 'h', text: 'One open position per instrument' },
    {
      kind: 'p',
      text: "While any position is open, EVERY entry is refused — either side, any plan version, on the arm path and the decision path alike: 'refused: position 591 open (v2 S1 short); no adds, no flips' (owner ruling 2026-09-03). The predecessor guard refused only the OPPOSITE side, because on a netting account an opposite-side fill silently nets the position; a same-side add was explicitly out of scope. The block on re-arming the SAME scenario in the same plan version lives in the store (MANUAL-CANCEL-WINS) and survives a restart, but a NEW plan version re-authorizes a terminal row — so a v3 S1 short could have added to a v2 S1 short position that was still open. An arm explicitly authored as an exit leg is exempt: that is how a position gets flattened.",
    },
    { kind: 'h', text: 'One trader per (symbol, account)' },
    {
      kind: 'p',
      text: 'Two traders can never trade the same account on the same symbol — a second one would double-apply the same fills. Starting a trader while another running trader already holds its (symbol, account) is refused with a 409 that names the other trader. At boot, if two traders in the database share a (symbol, account) and both are marked running, the first by created_at starts and the second is refused, marked not-running, and a P0 names both. A refused duplicate never falls back to a legacy path.',
    },
    { kind: 'h', text: 'Stop before changing the account' },
    {
      kind: 'p',
      text: "A running trader's NT8 account cannot be changed — the account dropdown is disabled while it runs. Selecting an account that is already bound to another trader on the same symbol is refused with a 409 that names that trader. Stop the trader first, then choose the account, then start it.",
    },
    { kind: 'h', text: 'Invalidation is execution-wired' },
    {
      kind: 'p',
      text: "The scenario evaluator publishes a verdict every cycle — '🎯 scenario S1 → ≈invalidated @ 29285.00 (price accepted through the level against the trade)'. Until 2026-09-03 that verdict was display-only and the arm seam never read it: on 09-03 the system reached the verdict at 08:50:54 and armed that same S1 short at 29285 twelve minutes later, filled at 09:03, stopped at 09:20 for −$140. The arm gate now refuses on it, calling the SAME evaluator the display path calls, and records the refusal under its own class 'invalidated'. If the evaluator cannot reach a verdict (no bars, or an UNEVALUABLE scenario) the arm PROCEEDS and the log says 'invalidation check unavailable' — an unresolved check is not a refusal. The refusal names WHEN the verdict was reached, from a stamp written once on the transition; absent that stamp it says 'an earlier cycle' rather than passing the check time off as the verdict time.",
    },
    { kind: 'h', text: 'A position states the plan it was armed under' },
    {
      kind: 'p',
      text: "The plan card shows the LIVE plan. On 2026-09-03 it showed NY v3 S1 long, written 09:15, while the account held a position armed under v2 S1 short — both called 'S1', and the owner read long on a short. When a position is open and was entered under a different version than the one on screen, the card now states the position's own terms first ('Position armed under v2 S1 short @ 29285.00') and the plan on screen second ('Plan now v3 — the rows below are THIS plan, not the position above'). It renders nothing when flat or when the two agree. Arms authorized before 2026-09-03 10:28 have no recorded version and render 'version not recorded', never 'v0'. (web/src/components/plan/ArmedUnderBlock.tsx · api/handler_plan_position_provenance.go)",
    },

    { kind: 'h', text: 'The flat gate now asks the broker about orders' },
    {
      kind: 'p',
      text: "Before a cutover the bot checks five things and refuses to swap binaries unless all five are clear. Four of them asked NinjaTrader. The fifth — are there working orders resting? — asked the bot's OWN records, because the AddOn never sent anything describing the broker's order book. That leg could confirm what we believed and could not detect the one thing it exists to detect: our records and the broker disagreeing. It had been passing on that basis since cutover 35.",
    },
    {
      kind: 'p',
      text: 'The AddOn sends its working-order book every 30 seconds and whenever an order changes state. Leg 4 compares that book with placed ledger orders. An armed row with no signal id is only an authorization: it appears on a separate informational line and does not fail the gate. A placement awaiting a broker receipt still counts as working/unconfirmed and blocks cutover, even if the book is empty. Working orders at either source, or disagreement between them, fail the leg. Terminal rows are excluded by one shared classifier, also used by audit queries. A book older than 60 seconds is refused as stale. Before the first AddOn snapshot, the existing explicitly labelled ledger fallback remains; it is not broker proof. The Settings → Updates page shows the installation-wide gate, which includes the cutover legs of every trader, with the exact reason text of every leg.',
    },
    { kind: 'h', text: 'Overriding the gate with a position open' },
    {
      kind: 'p',
      text: 'On 2026-09-02 a cutover went ahead with a position open and the resting stop could not be verified — nothing on the wire could say whether it was there. The rule after that was blunt: no override while a position is open. That was the right answer to a question nothing could answer, but it was a blanket no standing in for a missing measurement.',
    },
    {
      kind: 'p',
      text: 'The question is answerable now, so it gets asked instead: is there a working STOP order for this instrument, at the price we expect, within tolerance, in a book fresh enough to believe? If yes the override is allowed. If the stop is missing, at the wrong price, or the book is stale or absent, it is refused — and the refusal tells you what it found versus what it expected, rather than just saying no. A stale answer is never treated as a permissive one.',
    },
    { kind: 'h', text: 'Which AddOn build is actually running' },
    {
      kind: 'p',
      text: "An AI entry becomes a position only when NinjaTrader reports a fill for that exact order. Each entry is matched by its own signal id. A fill records the position at the real fill price. A rejection records the order as rejected, with no position and no 'Filled' alert. No answer within about three seconds leaves the order recorded as submitted and unconfirmed, with a warning, and no position: if it fills later, the position is picked up from NinjaTrader's own position list and tagged with that entry's own signal id from the fill NinjaTrader reported, so breakeven and trailing can move its stop. A fill counts when it landed no more than 105 seconds (the 45-second entry-confirmation grace plus the 60-second untracked grace) BEFORE the bot first saw the untracked position; there is no upper bound, so a fill reported after that first sighting counts too. The order is settled first, the tag comes last: the order is marked filled at the real fill price and its fill is recorded as usual, both in one store write. On a store failure neither is written: the order stays waiting for its fill, the position stays untagged, and a WARN names the order and the signal. If only the tag fails after the order settled, the order stays filled and the position untagged, again with a WARN. It only claims an order still waiting for its fill, and a signal that already explains another position is never used again. If two entries filled on the same side in that window, or the order was already settled, the position stays untagged rather than guess. When NinjaTrader reported no same-side fill of the bot's own in that window (for example after a restart emptied the list), the bot may match one of its own armed orders by price, but only one that filled inside that same window and whose signal explains no other position; an older armed order is never matched there. The 🧩 log line says which it was: the bot's late AI fill, its late armed fill, an armed fill matched by price, UNTAGGED with the reason (a failed database read is named as a failure), or a manual/NinjaTrader-side entry when every read succeeded and found nothing of the bot's own. Not yet bounded (owed): the one-time lineage repair that runs when a trader starts (every boot and trader reload) still matches filled armed orders by price with no time limit, so after a restart an untagged position can still take on an older armed order's plan link. Before this, an entry that was only queued, or even rejected, was recorded as an open position at the market price, and a fill belonging to an armed or Picture order could be recorded as the AI's.",
    },
    {
      kind: 'p',
      text: "A short position is seen as a position. NinjaTrader reports a short with a negative quantity, and the check run before an AI entry used to read that as flat, so an AI entry could net against a held short. The one-open-position rule and the same-side checks also compared 'long' against the stored 'LONG' and never fired; they now read sides the same way everywhere.",
    },
    {
      kind: 'p',
      text: "A stop entry that never reached NinjaTrader no longer cancels the plan's other arms. Only an order that was sent, or whose send failed after it was recorded, counts as 'placed' and closes the plan to its other arms.",
    },
    {
      kind: 'p',
      text: "Editing the AddOn source changes nothing until you reload the AddOn in NinjaTrader (F5) or restart NT8 — NinjaTrader keeps executing the DLL it last compiled. The bot now prints, at every boot, the build id it has RECEIVED on the wire next to the one it expects: '🔌 nt8 addon: build_id=… expected=… match=yes|NO'. It says NO — loudly, every boot — until a frame from the running AddOn proves otherwise. A build id read from our own source would report success for a change that never landed, which is precisely how a distributed change gets believed without being made.",
    },
    {
      kind: 'p',
      text: 'From 2026-09-05 that build id does more than report. A stop entry is REFUSED unless the received build proves it will hand NinjaTrader the trigger in the stop-price argument — because the build before it did not, and the result was an order NinjaTrader accepted, listed in its book, and then never worked: a stop with a trigger of zero. Twenty-two of them over two days, none filled, no reject and no error anywhere. Nothing in a log that reads the value we meant to send can catch that; only a build the far side proves by answering can. Limits are untouched by the floor, and the bot states the posture directly on its first armed cycle — and again if it changes: "🎯 stop-entry: seam=… · slots=… · guard=stop-side · unknown=no-op · addon build_id=… expected=… match=…", where slots reads "unproven" until a new enough build answers. Read the seam field FIRST: it is the switch the placement branch consults before any of the others. Two owner rulings set it: 2026-09-05 OFF — no stop entry is placed at all until a wave lands that confirms a broker cancel instead of assuming one — and 2026-09-18 07:52 CT ON, once the far side proved its stop-price frames. The boot line that proves which posture is live reads "🎯 stop-entry: seam=on · … match=yes"; the switch is the per-machine env STOP_ENTRY_SEAM (kernel/entry_law.go). Limits are unaffected either way. It is not a boot line in the strict sense: it needs a plan with arms and a bound NinjaTrader connection, so a quiet day may not print it at all.',
    },
    { kind: 'h', text: 'CAN-HARD-BLOCK vs ADVISORY-ONLY (the truth table)' },
    {
      kind: 'table',
      head: ['Gate', 'Kind', 'What it does'],
      rows: [
        [
          'SIM lock (isAccountTradeable)',
          'HARD',
          'Refuses non-SIM accounts at account routing — the bot cannot go live.',
        ],
        [
          'Feed down / dead-man / freeze / boot integrity',
          'HARD',
          'No bars, no bridge, no trading — cycle skipped or refused. The dead-man watchdog is sampled on every trader’s 1-minute monitor beat as well as its decision cycle, and it compares the NinjaTrader connection’s accept number between samples: a disconnect and reconnect that both fell between two samples (2026-10-14, trader 12345 holding a position) now blocks NEW entries until a clean reconciliation, exactly like one it saw. A clean reconciliation now means a positions snapshot AND an order snapshot RECEIVED on the new connection (after its accept number) — two reads that merely did not error could be answered from the old link’s cached frames. Resting entry orders are LEFT resting and listed by name while entries are blocked (whether to cancel them on a reconnect is an owner decision).',
        ],
        [
          'Stale data / clock step (release #22)',
          'HARD',
          'A NEW entry needs a fresh 1m feed on two clocks: the bar age on the wall clock (~75 s) AND the arrival of the last live 1m frame on the monotonic clock (135 s). Any wall-clock step of 6 s or more (CLOCK_STEP_FREEZE_MS, default 6000 ms — raised from 2000 ms because the box’s normal 2–5 s NTP corrections held entries) holds NEW entries for 120 s of monotonic time (CLOCK_STEP_HOLD_S); steps of 2–6 s are logged and counted, never held — except inside the CME open/close boundary (15:59–16:01 and 16:59–17:01 CT), where any step of 2 s or more still holds because it can flip IsCMEOpen; smaller steps from 100 ms (CLOCK_STEP_DETECT_MS) are logged and counted only. The boot line “🕰 clock-step guard” and /api/health print the values the bot actually read. Exits and protection are never held. It binds every NEW entry path: AI decisions, mentor arms and — since OPS NEW-2 (10-14) — planner and Picture-scenario arms at their send point, where a refused row stays armed (never cancelled) and places on the first pass after the feed is fresh. Counted as stale_data on the AI decision path and for planner arms (a planner arm held by a step counts as clock_step, once per row per episode), and as mentor_stale_data for a refused mentor placement; every step logs “🕰 clock step”. A planner arm goes through the same one-verdict kernel.EntryFeedStale, so a wired live-frame provider reporting no live 1m frame since boot reads stale (fail-closed) even when the 1m cache is empty. Disclosure (m168): raising the freeze to 6000 ms makes the 10-21 00:44–02:56 live-refused mentor arms placeable — 00:44 short 29822.50, 01:24 short 29815.25 (likely filled, ~−12.75 pt, displacing the 666 slot), plus 02:14 / 02:34 / 02:56 (which the HTF direction gate refuses anyway); the owner’s chrony fix (12:32 CT) cuts the step rate.',
        ],
        [
          'Data truth (release #54, m446)',
          'HARD (new entries only)',
          'A NEW entry for a symbol is paused while its bars cannot be trusted — exits, stops, break-even, trail, closes, protection and flatten never consult it. Eight named checks: bad_print (a 1m bar whose move is far outside normal and is confirmed by neither ES, volume nor the next bar — a real news bar is always confirmed), cross_frame (NT8’s 3m/5m/15m bar must equal the same span built from the stored 1m: a difference over 0.5 pt is RECORDED, and it PAUSES entries only when the worst price field is more than 2 pt off, or an indicator would move by more than its tolerance, or the volume is more than 40% off on a 3m/5m bar — a smaller tick-skew, and a volume-only difference on the 15m that no indicator reads, are only COUNTED in /api/health data_truth.cross_frame.counted and the log, never paused), indicator_lookback / indicator_lookback_swing4h / indicator_lookback_levels (every EMA, ATR, RSI, MACD, the 4h swing line and the prior-session levels must stand on its full bar count with no material hole), indicator_lookback_session (the session-anchored volume levels — session VWAP and its ±1/±2σ bands, eVWAP, the prior session’s pdVWAP, POC/VAH/VAL and naked POC, and the overnight MID-O — are sums over a whole session, so one hole or one volume-short bar skews them for hours, not for a few bars; they are recomputed with the missing volume restored from the 5m, and entries pause when VWAP or a sigma band moves more than 2 pt, or the prior profile’s POC/VAH/VAL more than 2.5 pt, or a slot exists in neither frame, or — because a check that cannot judge fails closed — no 1m or 5m bar at all is stored for the last 10 minutes of an OPEN market; the CME break, the weekend and the first ten minutes after a reopen never count as silence), ingest_loss (a FINAL or HISTORICAL bar the ingest queue dropped is data loss until the minute is back in the bar cache — a lost FINAL clears once its minute is present and a later bar exists, which proves it closed but not that its last ticks were kept, and a lost seed batch also clears when its minutes have left the ring, so the lookback checks are what hold a shallow ring; a dropped message that carried no real bar owes nothing; the bot asks NinjaTrader for the lost span again on its own — the same re-request the missing-bars tracker uses, standard or deep, under the same rules (only in the first 15 seconds of a minute, never while the link, the feed or the market is down — it waits and asks when that clears — and within the same per-symbol, per-CME-session-day budget of 40 requests) — and the pause lifts when the bars are back in the ring. IF THAT BUDGET IS SPENT nothing more is asked of NinjaTrader that session day and the pause STANDS; it ends when the bars arrive by any other replay, when the lost minute ages out of the ring (about 41 hours of 1m), or when you restart the bot (the loss list lives in memory, so a restart starts clean; NinjaTrader’s history replay refills the ring and the bar gap audit’s first pass re-reads the stored bars for any minute that never reached them). The pause text and the log say so; intrabar drops self-heal and only their rate is logged) and contract_roll (two contracts in one window, or a live contract that differs from the contract the seed batches — the history the ring was built from — named; a seed that names no contract is unknown history and pauses). WHEN IT RESUMES: a wrong bar is not forgiven by the clock. A bad print or a mismatching 3m/5m bar keeps the pause until it is repaired or replaced (the check re-runs on every evaluation), or until its influence on every indicator that reads it has decayed below that indicator’s tolerance — each error is charged only to the part of it an indicator reads: EMA, RSI and MACD read the CLOSE, ATR reads the high/low RANGE, and a 1m close reaches a 3m/5m close only when it is the last minute of that bucket. So a 69 pt wrong close (10-19 08:58) and a 163 pt wick that closes back at its open (10-19 09:19) each hold the AI path about 75 minutes (its EMA/RSI/MACD read the close, its ATR the range), a 56 pt wrong 5m low holds about 75 minutes, and no single bar holds more than 8 hours. A MENTOR trader additionally reads raw 1m highs and lows (the Globex session extreme the day gate measures, the rolling PHL buy stop, the boxes, the trendline touches, and the 3m/5m/15m candles it builds from the 1m), so a wrong 1m high or low — more than 2 pt off — keeps a mentor trader paused for the full 8 hours, whatever the close did. WHO IS PAUSED: a data-truth pause names the entry paths it concerns. The AI, planner, Picture and chat paths read the 3m/5m/15m frames and the whole AI indicator set; a MENTOR trader reads the 1m ring (EMA 34/9, raw highs and lows) and ONE native frame, the 5m, for its strong-day test (any of the last 12 closed 5m bars with a range of 50 pt or more). So a bad bar that only the AI path’s 3m/5m indicators read does not pause a mentor trader, a native 5m bar pauses a mentor trader only when its range crosses the 50 pt line the wrong way, for the 12 bars the test reads (60 minutes), and a native 5m bar that is MISSING from those last 12 pauses a mentor trader too (the lost bar may be the strong one). The owner push and the log line say “(mentor traders)” or “(AI/planner traders)” when the pause is scoped. A frame no indicator reads (the 15m trigger bars) holds for its own length. A session-volume pause lasts as long as the 1m store is wrong: a session with a hole keeps pausing the NEXT session too (its pdVWAP and profile are read all day) until the hole is repaired or the session after it begins. KNOWN LIMIT: for the AI path, past its hold the wrong bar is still inside raw high/low ranges (1h ranges, PDH/PDL) that the swing and levels checks judge only as the prior session. A check that cannot judge — an error, a panic, a missing source — pauses (fail closed), and so does a required check nobody wired (it appears in /api/health as blocked, “not wired”). One WARN and one owner push per episode, each check on its own owner-push budget; a check that re-blocks within 30 minutes of clearing is the same episode (no second push, counted as reopens). /api/health carries data_truth: {check: {blocked, episodes, reopens, last_reason}} (plus the ingest-loss and fill-price figures). TWO MORE CHECKS GUARD THE ACCOUNT SIDE: broker_agreement (what the bot believes it holds, and its stop and target orders, must agree with what NinjaTrader reports for the account; a disagreement that outlives the settle time pauses new entries) and fill_price (a fill far from the market it printed in — judged against the stored 1m bars of the fill minute — counts as a strike; two far fills in a CME session pause new entries until you acknowledge). HOW IT IS WIRED: every way a new entry can reach the market (the AI decision, chat, the planner and Day-Plan arms, the Picture, the mentor arms and their placement pass, the SIM test doors) asks ONE verdict, and the data-truth checks are part of it; a check that has not started yet (the first minutes after a restart, until NinjaTrader’s history replay has landed in the bar cache and the cache has been rebuilt from the store) holds entries back QUIETLY — no pause alert, nothing judged on a ring that has not arrived; if the replay never lands the checks judge anyway after 10 minutes and say what is missing. All the bar checks use the same market calendar as the missing-bars gate, so the evening before Thanksgiving, Christmas Day and New Year’s Day (a closed trading date) never counts as missing data. The SWING4H line check pauses SWING4H entries only, and it judges the same contract-stitched series the strategy reads (after a roll the older contract supplies the history up to its last bar, so a roll alone never pauses it); it re-judges whenever the stored bars change, so a repair (a bar import) lifts it on the next read, not at the next 4h bucket. The prior-session levels check also pauses when there is no stored bar at all for the last 5 sessions while the market is open. the two account checks (broker_agreement, fill_price) pause only the trader(s) whose account they are about — one account’s disagreement or far fills never pause a trader on another account (/api/health trading_paused.data_truth_scopes names who); every other check pauses all new entries for the symbol. They do NOT cancel orders already resting at the broker. The pause shows on the dashboard as TRADING PAUSED — data check failed: followed by the check names, and in /api/health trading_paused (reasons data_truth, data_truth_checks). One owner push per episode.',
        ],
        [
          'Broker agreement (release #54, m447)',
          'HARD (new entries only)',
          'A NEW entry of a trader is paused while the bot’s own picture of THAT trader’s account disagrees with NinjaTrader’s (each trader is judged on its own account; another trader’s disagreement or stale book does not pause it) — exits, stops, break-even, trail, closes, protection and flatten never consult it, and the check only reads: it never closes, opens or edits a position (that stays the position reconcile’s job). It compares what the reconcile already compares: NT8 holds a position the bot has no row for (and no armed or Picture fill explains it, and no entry of the SAME side is still awaiting its fill — a resting limit of the other side excuses nothing), the bot has an OPEN row NT8 does not hold (a row younger than 120 s is still waiting for its positions frame), or the quantities differ; the armed ledger says an order is working but NT8’s fresh order book does not list it, NT8 still lists a live order for an arm the ledger already settled cancelled/expired/rejected, or NT8 has a live ENTRY order (a bot-style uuid name) that no ledger row, position or pending entry of this trader names; the bound account is not offered by NT8, or an open row sits on a different account. SETTLE WINDOW 60 s: a disagreement pauses entries only after it has been seen for 60 s — the reconcile’s own debounce for the same verdicts (3 passes of 20 s) and twice the AddOn’s 30 s order-snapshot period, so an order just placed or a fill frame in flight is not a disagreement. It is sampled on the reconcile loop every 20 s whether or not anything tries to enter. It pauses at once (fail closed) when it cannot compare: no bound account, no positions frame for the account, an unreadable store (including the armed / Picture ledger it reads to excuse a position — an unreadable ledger never clears a standing pause), or — while the CME is open — a broker order book older than 120 s. While the CME is closed nothing is measured (nothing can open; a quiet broker is expected) but nothing is forgotten either: a disagreement that was blocking when the market closed is still blocking at the reopen and is re-measured on the very first tick against its old clock (no new 60 s to hide in); it ends only when a measurement finds the two views equal. The same holds for the position disagreements across the one check that cannot read the positions frame because an entry just landed. A trader that is not under the check at all (stopped, or not yet started) is not let through on the other traders’ silence while the CME is open. Every pause text ends with a REMEDY line: a quantity mismatch is cleared by closing the position (or trading it to the row’s quantity) in NT8 — the bot never edits a row quantity and /api/risk/clear-freeze does NOT lift this pause; an NT8-flat row and an untracked NT8 position are closed or written by the reconcile itself within about 60 s; a live order nothing names is cancelled in NT8 (the pause lifts within one 30 s order snapshot); a ledger order the broker lacks is cancelled by the reaper after 15 quiet minutes; an unoffered or mismatching account needs the account opened in NT8 or the trader rebound. One WARN and one owner push per episode naming the exact disagreement; entries resume when the two views measure equal. Born from the 2026-10-22 SimAccount1 SELL limit 29458.50 that rested 30 minutes under a uuid no ledger row named and then filled into position 687, born from reconcile.',
        ],
        [
          'Fill price (release #54, m447)',
          'HARD (new entries only)',
          'Every entry and exit fill the bot records is judged once against its own market: the stored 1m bars of the fill’s minute (two bars before, one after) with a 12-tick (3 pt on MNQ/ES) margin. A fill outside that band means either the bars lost the print or the recorded price is wrong — a mis-parsed frame, a stale quote, the wrong contract. A fill waits until its minute’s bar has closed (a forming bar holds only the range so far); with no bar for the minute it falls back to the order’s own price ±10 pt (or the AddOn’s reported slippage), and with neither it is counted unjudged — a missing bar is the bar-gap checks’ episode, not a second one. ONE far fill raises ONE owner alert and a counter and pauses nothing; the SECOND far fill EVENT for the same symbol (however the frame spells it: mnq, MNQ 12-26) on the same ACCOUNT in one CME session pauses NEW entries of the traders on that account until you acknowledge (POST /api/risk/ack-fill-price?symbol=MNQ&account=Sim101 — the exact URL is in the pause text; without account= every account of the symbol is acknowledged) or the next session begins. A fill EVENT is one real print, however many frames report it: the legs of a split bracket (one position_close frame per leg) and the partials of one entry are ONE strike, and the same print on a mirrored account is a strike on that account only. A fill the guard cannot judge because its own record is malformed (a price of zero or less, no fill time, no id) is counted as a far fill and logged, never dropped. It is not a slippage monitor: a stop that fills 20 points through its stop in a fast market printed inside the bars and is never far. Acknowledging ends the pause only: the NEXT far fill after your acknowledgement pauses again. A late or replayed far fill from an EARLIER session is alerted and counted but neither joins nor lifts the current session’s pause. The strikes and your acknowledgement are kept in a small file beside the database (.fillprice.json): a restart — a deploy is a kill -9 — does not lift a standing pause or forget an acknowledgement, and an UNREADABLE file fails closed (m55r3): the bot cannot tell whether a pause was standing, so new entries of every trader on the symbol stay paused, you are told by owner push, the unreadable file is MOVED ASIDE (never deleted or overwritten) to <file>.bad-<UTC time> in the same folder, and the pause lasts until you acknowledge it (the same POST /api/risk/ack-fill-price?symbol=MNQ, once per symbol you trade — it is in the pause text; without account=); the pause itself is kept in the new file, so a second restart does not lift it, and the new file is written and synced BEFORE the old one is moved, so even a crash or a full disk during that step leaves the pause standing at the next boot. The acknowledgement reply says "persisted": false when the disk did not take it inside two seconds (it is live, but a restart before the disk catches up re-pauses — acknowledge again). The file is written off the exit path (an exit never waits on disk), a moment after each far fill or acknowledgement. One entry fill is judged once however many frames report it (the AddOn sends both an order update and a fill frame for it). Measured on the last 30 days: the 246 broker fills and entries are none far; the same fills 300 points off are all far; of the 95 recorded close prices on closed positions exactly two are far (positions 635 and 639, recorded 14 to 30 points away from the real prints, later corrected from the NT8 receipts) — different sessions, so one alert each and no pause. Exits are never delayed or refused by it.',
        ],
        [
          'Market data unavailable (fail closed)',
          'HARD',
          'When the primary symbol has no market data for a cycle (the fetch failed after one retry), the AI decision is skipped with a named reason (market_data_unavailable, counted on the /api/risk/gate-blocks table). The AI is never called on absent prices, so it cannot fabricate an entry from nothing. The skip withholds a NEW AI decision this cycle (entry or exit); protection is unaffected — any resting broker stop stays in place, and the separate protection paths (the unprotected-position guard, force-flat) run on their own schedule, not the AI cycle.',
        ],
        [
          'Unprotected position (2026-09-07, tightened 2026-10-11)',
          'HARD',
          "Every minute while a position is open, and again the moment the NinjaTrader link comes back: is there a LIVE stop at the broker for it? If the book shows none, a P0 is raised AND a stop is placed. For a MENTOR position the stop placed is the driver's CURRENT stop — the moved break-even / 1:1 / trail stop, never the original entry stop. If a FRESH last price is already THROUGH that current stop, the stop would have filled, so the position is FLATTENED at market instead. The only price that may flatten is the newest 1-minute close judged fresh by the same stale-data rule that blocks mentor entries (~75 s bar age on the wall clock, AND a live 1m frame received within 135 s on the monotonic clock, so a wall-clock step back can no longer make a dead feed's last close look fresh; a clock-step hold never freezes protection) AND only while the CME is open — while the CME is closed (the 16:00–17:00 halt, the weekend) there is no fresh price, so a SWING4H held across the break is never flattened on a stale close. The flatten P0 is raised once per EPISODE: a new episode (the verdict leaving flatten and returning) raises a fresh P0, while repeats inside one episode stay silent and the close is re-sent with a retry count. No fresh price — no 1m bars yet, a stale feed, or the CME closed — and it does NOT flatten: it places the driver's stop and the P0 says the stop side could not be checked. A stale close, or the position mark's fall-back-to-entry, can never flatten a healthy position. For any other position the stop placed is the price the broker itself accepted, or failing that the plan's composed stop. It is the one check that acts rather than reports, because a position with no stop has no safe amount of waiting. Three things stop it acting: no fresh book, a protective order in a state this build cannot read, or a live stop whose quantity the book does not carry. Each says so in the journal and does nothing — an unknown is not permission to place a SECOND stop beside an invisible one. A PARTIALLY covered position is raised, never patched. Kill-switch: env PROTECTION_MENTOR_STOP (default ON). Born from 2026-09-06 23:37:02, when a cancel meant for an already-filled entry took its stop with it and the position ran 8h19m unprotected with nothing in the bot looking.",
        ],
        [
          'Boot sweep (class 33)',
          'HARD',
          'At boot, before anything is armed: every resting order left behind by the PREVIOUS process is cancelled at NinjaTrader and marked cancelled in the ledger (reason boot_sweep). A cancel that FAILS leaves the row live and retries — the ledger never goes clean while an order might still be at the broker. On 2026-09-02 00:16 CT, before this existed, two arms outlived their process for 15 minutes and briefly double-ordered S3. Since 2026-09-07 the sweep asks the broker before each cancel: a pre-boot row whose entry FILLED before the restart is left alone, because the only orders under that signal are its stop and target.',
        ],
        [
          'plan_mode direction/strict',
          'HARD',
          'Refuses entries against plan bias (direction); under strict every decision-path entry is refused — entries execute ONLY through armed plan scenarios, and a decision that cites a market_in_zone scenario runs that scenario\'s armed pass (a nudge: placement only, the record still reads refused + " · 🚦 <verdict>"). No plan + direction/strict = no trades.',
        ],
        [
          'min_confidence',
          'HARD',
          'Confidence below the floor (default 60) → entry refused. Decision path only: a plan arm carries no AI confidence, so under strict this gate never runs (since release #22 an arm entry stores its confidence empty / n/a; rows written before release #22 still show 0).',
        ],
        [
          'MIN-SL (env MIN_SL_ATR_MULT, default 1.5)',
          'HARD',
          'Stop closer than the floor (×ATR + 2-tick clearance) → refused. On CME futures, judged on the tick-rounded prices the broker receives (the stop rounds away from the entry).',
        ],
        [
          'HTF veto',
          'HARD',
          'Entry against the HTF regime at a veto anchor → refused. MODE (HTF_VETO_MODE): 1h | cross | 4h — LIVE = cross: vetoes only when 1h AND 4h both agree (the 2026-08-28 autopsy: 1h-only blocked 3 would-have-won arms = +$352, 4h was RANGING at all 7 → cross blocks nothing the evidence doesn\'t support). FIXED (F-4): until this fix cross could NEVER veto — the cycle snapshot held 5m/15m/1h only, so it never found a 4h trend and passed every entry without a word (zero htf_veto refusals since 08-28; on 10-14 the 1h read TRENDING_DOWN all through the long #653). The 4h trend now comes from the bar cache\'s own 4h series, and a missing TF is said out loud (🛡️ HTF veto (cross) SKIPPED …). A one-way move also counts: a TF whose 3-swing set reads RANGING but whose newest closed bar broke the last swing extreme by ≥ STRUCTURE_ONEWAY_ATR (1.0) × ATR reads TRENDING in that direction for the veto only (the refusal says "one-way: …"); a genuine range still never vetoes, and every other reader of the structure trend is unchanged. JUDGED AT WRITE TOO (2026-10-16): when the planner writes a plan, its feasibility check now runs this veto on the same snapshot the arm pass builds (the 1m cache for the 1h, the native 4h series for the 4h). Before, that check passed no snapshot at all, so every plan write logged "🛡️ HTF veto (cross) SKIPPED — no 1h+4h" (136 times on 10-15) and a counter-trend arm was published, then refused only when it tried to arm (10-15 NY S1 and S2 longs). Now a counter-trend arm goes back to the planner as a repair hint; after the last attempt it is written with its arm disabled (arm_disabled_reason veto). SKIPPED now means one thing only: the bar cache had no bars for that timeframe.',
        ],
        [
          'ARM floors (the strategy min R:R)',
          'HARD',
          "The resting-order gate: R:R ≥ the bound strategy's min_risk_reward_ratio (2 on both bound strategies; ARM_MIN_RR was deleted 2026-09-03 — one floor for the arm seam and the decision path) AND stop ≥ the MIN-SL floor (1.5×ATR5m, kernel.MinSLATRMultDefault) or the arm is REFUSED every cycle.",
        ],
        [
          'Entry gate (class 48) — ONE gate, BOTH paths',
          'HARD',
          "Before any order leaves — resting arm or AI market entry — the SAME chain runs: scenario direction vs the cited scenario, shadow map (0C: breakout_retest + fvg_entry are authored + scored but NEVER placed), R:R vs min_risk_reward_ratio judged at the LIVE execution price (not the prompt snapshot), min-SL ×ATR5m, one-live-arm (on CME futures, R:R and min-SL are judged on the tick-rounded prices that will be sent, and a rounding that crosses a floor is refused, not sent; a stop-entry's R:R is still judged at the plan entry, not the offset trigger). An agent-chat entry is judged by the same R:R and min-SL checks on its own stop and target, and is refused without them. Refusals are recorded per path. (2026-09-02: 587 and 589 filled BELOW the 2.0 floor because the floor was judged on a stale snapshot; 589/590 traded the shadowed breakout_retest.)",
        ],
        [
          'T1 red news blackout',
          'HARD',
          'No entries in the ±15m window around T1 events (calendar) whose currency is in day_plan.t1_currencies — default USD only (W-T1-CURRENCIES, 2026-09-18). A red event in any other currency (the 2026-09-17 BOJ rate decision, JPY, 21:54 CT, which hard-blocked the MNQ bot) is an ADVISORY line on the card, in the plan\'s no_trade list and in the prompt — visible, never blocking; set ALL to restore the old every-currency block. An event with no currency still hard-blocks (fail closed). When the host clock measurably disagrees with the NT8 feed the band is widened, but only by a CAPPED amount: 2 minutes a side at most (the 60 s tolerance plus one boundary minute), and the card says "(clock drift)" only when the measured skew is between 60 s and 5 min. A larger positive reading is the AGE of the last bar — a CME halt or a feed gap — not the clock; since 2026-09-17 (CLASS 145) it widens nothing beyond the cap and the journal says "feed stale Nm — halt or gap". While the Day Plan is on, entries are also refused from 2 minutes BEFORE the window (the force-flat lead, when open positions are flattened), on every path and trigger — scan, live-bar pass and AI decision — and any unfilled arm is cancelled. While the window stays open, a cancel NinjaTrader has not yet confirmed is re-sent at most once every 30 seconds per order, not on every scan and live-bar pass, so a lost cancel is still retried without the sweep blocking each pass on a dark book. These refusals are counted as force_flat_window, not as the no-trade band. Before that, the ASIA read authored inside the 16:00–17:00 halt widened the BOJ ±15m band by 39 minutes a side: an hour and three-quarters of the session blocked by a clock that was never wrong.',
        ],
        [
          'Calendar slice missing (fail-closed alert)',
          'HARD',
          'The "Calendar slice missing" P0 fires ONLY when the calendar feed was NOT fetched for that trade date and the static file does not cover it — the bot then holds the standard 07:30 CT print window fail-closed. Since CAL-EMPTY-DAY, a fetched date with NO events is stored as an empty slice ("[]"), so an event-free day does NOT alert: the gate reads "fetched, nothing scheduled" and opens with no T1 blackout (the log says "📅 calendar: <date> fetched — no events scheduled"). The alert means the feed genuinely did not reach that date, not that the day had no news.',
        ],
        [
          'Lunch / session windows / EOD flat',
          'HARD',
          'Outside an enabled session window, inside the lunch or first-N no-trade band (the lunch window is the one kernel.LunchWindowCT() resolves — read, never a literal), or past the session close: no NEW entry, and flat at session end. MENTOR-MODE EXCEPTION: a mentor arm is NOT refused by the day-plan session LIST — the lunch band, the first-5m band, outside all session windows, a session not enabled for the strategy, and the trade cap are all waived for mentor rows, because the mentor has its own window and news rules (trader/session_risk.go mentorWaivesSessionBand; live: mentor row 251 filled at 13:25 CT on 10-05, inside the lunch band). The T1 red-news blackout, the consecutive-loss breaker and the force-flat windows are NEVER waived and refuse a mentor arm even when a waived band is also open (#45 P1). The one exception inside them: a SWING4H mentor arm is exempt from the in-session EOD-flat entry refusal only (the course holds the swing by the 4h) — the T1 blackout and the 2-minute T1 force-flat lead still refuse it, and an intraday ISB stays refused after the EOD flat (trader/session_risk.go isEODFlatWindowRisk). Since 2026-09-09 the band binds the ARM path too. Until then it was read by the AI-decision gate and the adherence grader and by NOTHING on the arm path, so under plan_mode=strict — where a resting order is the only way in — it refused nothing. An arm inside the band is now refused, and an arm already resting when the band opens is CANCELLED rather than grandfathered: refusing only NEW arms while one placed at 11:58 rests into 12:00 is a band that stops authoring and not entering. A cancel still unconfirmed by NinjaTrader is re-sent at most once every 30 seconds per order while the band stays open, not on every pass. A session whose EOD flat is set earlier than its last-entry cutoff refuses entries from the flat on, not only at last-entry (counted as force_flat_window). Past a session’s last-entry cutoff the arm path no longer WRITES a new arm row for that session’s plan ("🕒 arm NOT authored … last_entry", counted once), and a resting zone row is not counted as WAITING any more — it is refused as last_entry. When a band cancels a row whose own session has already CLOSED, the cancel names the close ("session ended (LONDON closed; NY is active)"), not the next session’s band.',
        ],
        [
          'Consecutive-loss breaker',
          'HARD',
          'After N consecutive losing closes in one CME session-day, no new entry on EITHER path until the 17:00 CT roll. N is 8 by default and the owner may set it — since W1 (settings truth, 2026-09-23) presence-aware: a saved number wins, a saved 0 is OFF, and a BLANK strategy value inherits env BREAKER_HALT_N (0 = off) else 8; the 🛑 boot line tags each [O] saved, [E] env, [I] default; a WARN at 5 counts and surfaces without refusing. This is the ONE session limit that is not gated by the guardrails master, so it bites whether or not that switch is on — and since 2026-09-09 it is wired to the ARM path as well as the decision path, which under plan_mode=strict is the only path that trades. Honest about its own reach: on the retained tape the longest run of losers is SEVEN, so a threshold of 8 would never have fired — the boot line says so only when N ≥ 8. A stored 0 from before W1 meant "inherit" then and "OFF" now: the trader bound to it is REFUSED at load (the ⛔ line names the strategy, the field and both meanings) until a Studio save confirms it — the conversion never silently disables a breaker. An UNRESOLVABLE P&L ends a run rather than bridging it — an unknown outcome must not push the desk toward a halt.',
        ],
        [
          'Owner pause (stop_until)',
          'HARD',
          'POST /api/traders/:id/pause sets a deadline; until it passes, NEW entries are refused on every path (AI decision, agent chat, arm, Picture). Closes, EOD-flat, the 60-second monitor guards and NT8 brackets continue. Off unless the owner pauses; not gated by the guardrails master. The deadline is kept in system_config (trader_pause_until:<trader>) and restored after a restart; the first check at or after it auto-resumes, and POST /api/traders/:id/resume clears it at once. Logs "⏸ stop_until"; counted as stop_until. (trader/auto_trader_pause.go entryPausedAt, asked in trader/entry_admission.go admitChain)',
        ],
        [
          'Maintenance hold (update install)',
          'HARD',
          'While the updater holds the installation (<data>/updater/hold.json; an unreadable hold file holds too, fail-closed), NEW entries are refused on every path, and the NT8 entry send itself must also pass a send-side permit that is refused while held. Position management continues. Set only by an update job, never by a strategy setting; resuming a stop_until pause does not lift it — entries resume when the update completes. Logs "🔒 maintenance hold"; counted as maintenance_hold. (trader/maintenance_gate.go MaintenanceHeld / MaintenanceEntryPermit, asked in trader/entry_admission.go admitChain)',
        ],
        [
          'Install pending (update preflight, release #22)',
          'HARD',
          'While an update job sits in a pre-hold state (requested, downloaded, verified or preflight_ok — read-only from the updater’s job files), NEW entries are frozen on every path, so the bot stops placing while the preflight waits for it to be flat. It clears when the job leaves those states (at maintenance_held the hold above takes over) or once the job is older than the updater’s 30-minute limit (updaterjob.MaxJobAge). Exits, protection, closes and plan reads continue. Always on; nothing to configure. A job file that cannot be read is skipped, WARNed and counted (install_pending_unreadable) once per file version — it never freezes entries. Logs "⏳ install pending"; refusals are counted as install_pending, each new job once as install_pending_episode. (trader/install_pending.go InstallPending, asked in trader/entry_admission.go admitChain)',
        ],
        [
          'Daily guardrails + daily-loss latch (release #22)',
          'HARD',
          'ONE verdict asked by every entry path (AI decision, agent chat, planner arms, mentor arms, Picture): the daily loss limit on the bot’s own session-day P&L (realized + open, manual trades excluded), the daily profit target and max daily trades. OFF by default: nothing trips unless the guardrails master (guardrails_enabled) AND that leg’s own toggle are on, and a switch never saved reads OFF (both bound strategies store them OFF). A loss breach LATCHES until 17:00 CT and survives a restart (system_config daily_loss_latch:<trader>); while latched the 60-second monitor beat cancels resting entry orders and flattens the bot’s own positions. The trip logs "🔴 DAILY LOSS LIMIT HIT" once and counts daily_loss_tripped; refusals are counted as daily_loss, daily_profit_target or max_daily_trades. Only a definite limit refuses here; an unknown (an unresolved close today, a failed read) refuses only at the mentor’s authoring step. The AI decision cycle also runs the kernel’s own check first and holds the cycle (counted strategy_studio_daily). Details: Settings → Risk Control knobs → Daily loss limit. (trader/daily_loss_gate.go dailyGuardVerdictAt, asked in trader/entry_admission.go admitChain)',
        ],
        [
          'Re-entry cooldown (B7)',
          'HARD',
          'After a STOP-LOSS exit (NT8 exit reason "sl"; target and manual exits do not arm it), a SAME-direction re-entry on that symbol is refused until the cooldown minutes pass — or, on the AI decision path only, until price is a full ATR15 away from the stop, whichever comes first. Decision path: the entry is turned into WAIT (kernel/engine_analysis.go applyReentryCooldown); arm and Picture paths: refused in trader/entry_admission.go admitChain through discipline.ReentryPeek, timer only. OFF by default — risk_control.reentry_cooldown_minutes 0 or unset is OFF, and neither bound strategy stores a value; not gated by the guardrails master. The stop record lives in memory only, so a restart clears it. An agent-chat entry is not checked by it. Logs "⛔ re-entry cooldown"; counted as reentry_cooldown on both paths.',
        ],
        [
          'Side-quota (0-on-a-side / empty map)',
          'HARD',
          'A one-sided plan or an empty machine map fail-closes the read.',
        ],
        [
          'Confirm MET / stale-MET',
          'ADVISORY',
          'Informs the AI + card. Never blocks.',
        ],
        ['Touch chips ○◐✕▲', 'ADVISORY', 'Telemetry only.'],
        [
          'fvg IN-ZONE/ABOVE/BELOW/FILLED_INVALID',
          'ADVISORY',
          'Informs. Never blocks.',
        ],
        [
          'quality A+/A/B/C + m: machine grade',
          'ADVISORY',
          'Informational (D3 ruling) — no gate consumes them.',
        ],
        ['scenario status dots', 'ADVISORY', 'Read-only backend state.'],
        [
          'chain warnings / role mismatches',
          'ADVISORY',
          'Warn at write, never a fail.',
        ],
      ],
    },
    { kind: 'h', text: 'The refusal decoder' },
    {
      kind: 'callout',
      title: 'every refusal is a named string — here is the translation',
      items: [
        {
          title: 'confidence too low (N), must be ≥M',
          body: "The AI's confidence was under min_confidence. Not a bug — the bar.",
          cite: 'kernel/engine_position.go validateDecision (min-confidence check)',
        },
        {
          title: 'no matched scenario cited (strict mode)',
          body: "plan_mode=strict and the action didn't cite an armed S#. The plan is the law. Even a CITED decision is never a market entry under strict (entry_gate: refused: strict — … ARM path only): if the cited scenario is an enabled market_in_zone arm, the decision nudges one armed pass for it and the record carries the executor's verdict.",
          cite: 'trader/auto_trader_planconfig.go planModeBlockedAt',
        },
        {
          title: 'against the plan (direction mode)',
          body: 'The bias is long and the entry is short. Advisory says fine; direction says no.',
        },
        {
          title: 'sl_too_tight: D < M×ATR (A) — widen or skip',
          body: 'The stop is too tight for the volatility floor: |entry−SL| (D) is below MIN_SL_ATR_MULT (M) × the 5m ATR (A).',
          cite: 'kernel/engine_position.go validateDecision → kernel/min_sl.go MinSLVerdict',
        },
        {
          title: 'past last-entry time · outside session window · lunch',
          body: 'Clock gates — see Section 2 timeline.',
        },
        {
          title: 'only N levels above price … must carry ≥Q on EACH side',
          body: 'AI-caused omission (the map had them) → the read retries; machine-caused → now a ⚖ WARN and the plan writes.',
          cite: 'kernel/plan_doc.go ValidatePlanDocWithFactsMachine',
        },
        {
          title: 'awaiting approval',
          body: 'approval_required is ON and nobody approved this session-day. Tap Approve.',
        },
        {
          title: 'gate-block counters',
          body: '"Refused this session" panel shows every label + count; reset at the 17:00 roll and on restart.',
          cite: 'web/src/components/plan/GateBlocksPanel.tsx',
        },
        {
          title:
            'room_gate · thesis_death_exit · no_chase_refused · momentum_into_zone · atr_floor_beyond_invalidation · breakeven_r_unknown_risk (release #25, all OFF by default)',
          body: 'The STRICT-12345 loss-fix switches. They count only after the owner turns one on in Studio (Day Plan → Loss-fix switches; Risk Control → Auto-breakeven → Trigger in R). room_gate: the first obstacle is closer than planner_room_min_r × risk (room_gate_tp1 = the target was moved to it instead). thesis_death_exit: a planner position closed at market because its own invalidation closed, its plan went dormant, or a newer plan turned the other way. no_chase_refused / momentum_into_zone: an arm placement refused for chasing / for price racing into the zone. atr_floor_beyond_invalidation: the ATR stop floor would sit past the authored invalidation, so the arm was refused instead of widened. breakeven_r_unknown_risk: breakeven in R had no initial risk and used the points rule. Settings → Day Plan cards carry what each would have done on #649–#654.',
          cite: 'web/src/guide/content/settings.ts (the F-2/F-3/F-5/F-7/F-8 cards)',
        },
        {
          title: 'attempted-entry replay guard (FIX-DOUBLE-ENTRY)',
          body: 'An entry frame whose socket write was STARTED (it may have reached NinjaTrader) is never re-sent blindly after a reconnect. Before re-sending, the bot requires FRESH broker truth taken after the reconnect: the order named by the signal id in a new order snapshot, or a fill echoing it → the frame is settled and dropped ("found at broker — not resent"). Absent from a fresh snapshot it is re-sent once; no fresh snapshot within the wait → dropped and counted (gate-block "attempted_entry_unverified"). Always ON — ATTEMPTED_ENTRY_VERIFY_WAIT_S only tunes the wait. A missed entry is acceptable; a double entry is not.',
          cite: 'provider/ninjatrader/attempted_guard.go',
        },
      ],
    },
    { kind: 'h', text: 'plan_mode — the three levels' },
    {
      kind: 'table',
      head: ['Mode', 'Blocks', 'Allows'],
      rows: [
        [
          'advisory (default)',
          'Nothing',
          'Everything — the plan informs, the AI decides.',
        ],
        [
          'direction',
          'Entries against the plan bias',
          'Entries with the bias; anything not direction-conflicting.',
        ],
        [
          'strict',
          'EVERY decision-path market entry; ANY entry with no active plan',
          "Only armed plan scenarios (market_in_zone: a limit inside the planner's entry zone); a decision citing one is a nudge for its armed pass.",
        ],
      ],
    },
    {
      kind: 'p',
      text: 'Strict\'s warning, plain: "no plan = no trades" — a fail-closed day in strict mode is a flat day, by design. Per-session overrides exist (Strategy → Day Plan → Sessions). Since W3 the executor prompt says the same thing the gate does: its PLAN BLOCK header renders the RESOLVED mode\'s rule for the plan\'s session — strict "entries execute ONLY through armed plan scenarios (market_in_zone policy); an AI decision is a nudge — cite the scenario; off-plan is refused", direction the bias rule, advisory the old "a valid off-plan setup may still be traded". Before W3 every mode read the advisory line (decision 45139 read it under strict while log 92685 refused the entry).',
    },
    { kind: 'h', text: 'Guardrails + SIM lock' },
    {
      kind: 'p',
      text: 'Risk guardrails: the daily loss limit is OFF unless you turn it on in Strategy Studio → Risk Control — TWO switches must be on: the master (guardrails_enabled) arms the whole block, and the Daily loss limit row\u2019s toggle (daily_loss_enabled) arms this leg, with the dollar amount in that row\u2019s $ field. Both are OFF today, so the configured $450 limit is DECORATIVE and enforces nothing until both move; a switch never saved reads OFF too (since release #22). The desk strip DAY line says exactly that, in those words. When both are ON and the bot\u2019s own day P&L (realized + open, manual trades excluded) reaches −limit, every path refuses new entries, the 60-second beat cancels resting entry orders and flattens the bot\u2019s positions, and the gate stays latched until 17:00 CT. Max daily trades and the profit target (each with its own toggle) refuse on every path too, mentor included. Min confidence gates AI decisions only — it does not apply to mentor mode. The master also arms the daily profit/trade limits, the blackout window and the consistency rule (those last two are checked on the AI decision cycle only). Max contracts/order and the notional cap are NOT under the master: they are always on and have no toggle (the old max_contracts_enabled / notional_cap_enabled fields are parse-only). MENTOR MODE: max_contracts_per_order is NOT applied to mentor entries — a mentor-mode trader sizes from its own table (mentor_base_contracts …, hard cap mentor_max_contracts, default 20), so the per-order cap below is decorative for the mentor. The re-entry cooldown is deliberately OUTSIDE the master, like the consecutive-loss breaker: 0 = OFF until a strategy value is saved, and a saved value enforces regardless of the master switch. The consecutive-loss breaker is also outside all of this and works regardless. Would-have-tripped counters are visible in the dashboard. SIM lock: every account list is filtered to SIM; the bot cannot route to a live NT account — do not try.',
    },
    { kind: 'h', text: 'WHEN A PLAN IS REJECTED: THE REPAIR RETRY (class 44)' },
    {
      kind: 'p',
      text: "A rejected plan is retried by REPAIR: the model is sent back its own output, the validator's reasons, and the law it broke, and asked to return the complete corrected plan. Repair is the default retry and it is cheap — a fraction of a full re-author. Measured across 2026-09-01, 18 of 28 repairs were rejected again, and the reason was not what anyone assumed: only one failed to parse, and that was a fractional contract size where a whole number was required. The other seventeen parsed perfectly and were rejected on their values, ten of them because the model wrote a confirmation rule that does not exist in that field. It had never been shown the list. The repair prompt now carries the same vocabulary the validator judges by, states the confirmation rules and says plainly that death and flip use a different vocabulary, and attaches every relevant law rather than only the first one that matched. It also repeats the return format at the top and the bottom, because a single instruction in front of a wall of text is the one most likely to be missed.",
    },
    {
      kind: 'h',
      text: 'WHAT THE PLANNER IS TOLD, AND WHAT IT WAS NOT (class 50)',
    },
    {
      kind: 'p',
      text: "The plan is written by a model that could not see three things the validator judges it by, so it kept being rejected for rules it was never shown. First, the prompt ordered a whole play, not a direction: below the prior day's low it said you MUST write a continuation short. When a level has already been taken back, the validator voids exactly that play — so the instruction and the rule contradicted each other, and the model lost attempts obeying the prompt. The order is now a DIRECTION, with the legal conditions named and the choice left to the model. Second, every breakdown level that price has already closed back across is now listed in the prompt as void, decided by the same code the validator runs rather than by a second copy of the logic that could drift from it. Third, the minimum stop distance is stated up front: since 0B a stop is floored at 1.5×ATR5m, and the planner was never told the number it had to clear. It is now printed with the current reading, so an authored stop can be right the first time instead of being silently widened at arm time.",
    },
    {
      kind: 'p',
      text: 'The fourth change is about memory. When a plan is rejected and rewritten, the correction used to name only the most recent defect. On 2026-09-02 the London read showed the cost: attempt 1 was rejected for writing into a voided breakdown, attempt 2 for a fade that needed a touch, and attempt 3 was told only about the fade — so it fixed the fade and walked straight back into the void it had been corrected about two attempts earlier. The correction block now carries every distinct defect seen so far in that read, in the order they appeared, and it appears twice: once at the very top, ahead of the playbook, and once at the very end. Roughly 240 tokens on a 6,600-token prompt. A single instruction in front of a wall of text is the one most likely to be missed, which is the same reason the repair prompt repeats its return format.',
    },
    {
      kind: 'h',
      text: 'THE VOID LIST AND THE VALIDATOR NOW READ ONE TAPE (class 51)',
    },
    {
      kind: 'p',
      text: 'The plan prompt lists the levels where a waterfall play is already dead, and the validator refuses those plays when a plan arrives. Both were asking the same question of the same code — and handing it different tape. The prompt looked only at the current session day; the validator looked at everything it held, over a shorter history. So a level broken and taken back before the 17:00 evening boundary was dead to the validator and invisible in the prompt. On 2 September at 20:58 the prompt listed eight levels as void, left out the overnight low, and the plan was rejected on exactly that level. The parity test written to prevent this had passed twenty tapes in a row, because it handed both sides the same inputs itself: it checked that the two pieces of code agree, never that the running system gives them the same thing to agree about.',
    },
    {
      kind: 'p',
      text: "There is now one resolver. Neither side chooses a window or a slice; both read what it returns. The window is the CME session day, and this is the part that changes behaviour: the VALIDATOR narrowed to match the prompt, so a level broken and reclaimed days ago no longer voids a play today. That means slightly FEWER rejections, not more. The first attempt did the opposite — it widened the prompt to the validator's full history — and on the real tape that marked twenty entries across twelve levels, a list that effectively says author no waterfall play anywhere. The list is also compact now: one line per level, with both sides folded into it when both are dead.",
    },
    {
      kind: 'p',
      text: 'Separately, every read now records what the model was told: the void list, the minimum stop distance and the ATR behind it, the bias labels and the resolved window, whether the read succeeded or failed. Before this, a rendered prompt was kept only when a read was REJECTED, so the better the system got the less evidence it left — the 2 September fix could be proven live only because a read happened to fail. Five hundred reads are kept.',
    },
    {
      kind: 'h',
      text: 'EVERY POSITION SAYS WHAT IT KNOWS ABOUT ITS PLAN (class 52)',
    },
    {
      kind: 'p',
      text: 'A closed trade either links to the plan that produced it, or it does not. Until now "does not" was written two different ways: some rows said UNRESOLVABLE, others were simply blank — and blank is also what a row looks like before anything has stamped it. So no report could tell "we looked and there was nothing to join to" from "nobody has looked yet". There is now one value and one place that decides which of the three states a row is in. A position created from an unrecognised broker position is stamped UNRESOLVABLE the moment it is created, with a line in the journal, because that path knows an account, a symbol, a side and a price and nothing else — there is no order of ours behind it to trace. A link is never guessed.',
    },
    {
      kind: 'p',
      text: 'Worth knowing what this did NOT turn out to be. It was dispatched on a belief that a quarter of recent trades could not be traced to a plan. Measured before building: since the day-plan era began, every system and every armed entry carries a link, eight of eleven reconciled rows do, September had none missing, and the two trades named as unstamped were already fully stamped. The real gap was three rows across three weeks, none with an arm within thirty minutes of it. The older history — several hundred crypto-era trades — is left exactly as it was, because marking those UNRESOLVABLE would claim we searched for a plan that never existed.',
    },
    {
      kind: 'p',
      text: 'Second fix, same wave: an armed order now records the plan version it was ARMED under, once, and never rewrites it. Its existing version field still moves when a later plan version touches the row, and is now documented as meaning exactly that. Before this, an arm\'s version was whatever last touched it, which is why an audit asking "which version armed this?" could not answer honestly.',
    },
    { kind: 'h', text: 'HOW A LEVEL IS MEASURED — THE DETECTOR (1B)' },
    {
      kind: 'p',
      text: 'Every number this system has published about how levels behave — 84% reactions, 70.3%, 75.1% — was an artifact of how the question was asked, not something the market did. Two detectors were running. One called a touch a REJECTION whenever the closing price was still on the side it arrived from, which on a coin-flip tape is true about 69% of the time before the market does anything at all. The other counted ANY move away from the level as a reaction, so price blasting straight through scored the same as price refusing to go. Neither could have returned a low number. Three different definitions of "at the level" were in use at once, two of them a fixed four points — which means one thing on a quiet morning and nothing at all on a violent one.',
    },
    {
      kind: 'p',
      text: "The replacement is deliberately dull: put two barriers an equal distance either side of the level, and see which one price reaches first. Equal distances mean a market with no opinion is a coin flip by construction, so any departure from 50/50 is the market talking rather than the instrument. The distance is k×Δ, where Δ is the tape's own average minute-to-minute movement, re-measured per period — so the band widens when the market does. Calibrated on real MNQ data with the moves shuffled into random order, it reads 0.4988, and through the live recording path 0.4920. Both are the coin flip you want to see from an instrument that has no opinion of its own.",
    },
    {
      kind: 'p',
      text: 'Episodes that cannot be resolved — price spans both barriers in one bar, or the horizon runs out — are RECORDED and counted, then excluded from the rate. That matters: a rate that quietly discards its hard cases flatters itself. Every rate ships with its sample size and a confidence interval, and below 200 observations it is labelled DESCRIPTIVE ONLY, because at that size it describes a sample rather than estimating a property. Two tables now record all of it: one row per episode, and one row per candidate level per plan read — including the levels that were NOT chosen, with the reason each was cut. Without the rejected ones you can measure how the chosen levels performed and never whether the choosing was any good.',
    },
    { kind: 'h', text: 'WHEN YOU SAVE IN STRATEGY STUDIO (class 44)' },
    {
      kind: 'p',
      text: 'Saving reloads the running trader in place. Every save now prints one line per setting that actually changed, with the old and new values as the trader will resolve them, and stores the same rows so the change is answerable later. A save that changes nothing says so. This exists because on 2026-09-01 at 08:13 a save moved the minimum risk-to-reward from 3 to 2 in the middle of the New York session and nothing anywhere recorded it; the change had to be reconstructed afterwards from its effects. It was the third silent settings change that week.',
    },
    { kind: 'h', text: 'THE UNIT ABOVE THE TOUCH — AN OPPORTUNITY' },
    {
      kind: 'p',
      text: 'The detector answers per TOUCH. An experiment asks per OPPORTUNITY, and those are not the same thing: a level touched three times in a session is three touches and one chance. So each touch row now records what the CHANCE came to — never_reached, reached_declined, confirmed_not_armed, armed_not_filled, filled — and the cause it closed on. Every episode closes by session end; a row left open would be skipped in silence by anything counting outcomes, and the denominator would be quietly wrong.',
    },
    {
      kind: 'p',
      text: 'The point of that ladder is the second rung. A setup that was reached and declined is a ZERO-TRADE OUTCOME, not a missing row — and until now it was indistinguishable from one never reached at all, because neither was written down. A rate computed over the survivors of that is a rate over the wrong population.',
    },
    {
      kind: 'p',
      text: 'The link from a touch to the scenario written on it is a HEURISTIC and is labelled as one. A scenario names no level — it carries a trigger and an invalidation in free text and nothing else — so the tie can only ever be nearest-by-price. The column is called ScenarioNearest for that reason, it always records its basis, and it is NULL whenever two scenarios sit inside the map cluster width (kernel.LevelClusterTicks, the same tolerance the level merge uses to decide two references are the same reference) or nothing is close. Ambiguity is NULL, never nearest-wins: a tie-break would manufacture certainty the data does not contain.',
    },
    {
      kind: 'p',
      text: 'The entry recorded is the ATTAINABLE one, with its assumption named — an observed fill is a measurement, a resting-limit fill is an assumption, a first-tradeable-after-confirm is an approximation of one, and the LEVEL price is none of those and never appears. A touch is not a fill.',
    },
    {
      kind: 'p',
      text: 'The backfill over history recomputed NOTHING, and that is the finding rather than a shortfall: formation time is absent on 96.2% of in-era rows and the scenario link is a new column, so no historical row carries the inputs. Every per-opportunity figure this system reports therefore begins at the boot that shipped this; anything earlier is honestly unrecoverable rather than quietly missing.',
    },
    {
      kind: 'h',
      text: 'IS THE BOOK ALLOWED TO FADE RIGHT NOW? — A LABEL, NEVER A GATE',
    },
    {
      kind: 'p',
      text: 'The book is a level fade, and until 2026-09-10 it had no permission step: it faded every day the same way, and on 2026-09-03 it sold into a +483-point run. Every scenario now carries a fade-permission label — permitted, excluded (with the exclusion named and what it measured against what), or not evaluated — and every episode is stamped with its label at the moment it OPENS. Nothing reads that label to refuse. An excluded scenario is authorized, armed, placed and traded exactly as a permitted one. The arm path cannot even see the column; a test fails if it ever can.',
    },
    {
      kind: 'p',
      text: "Why only a label. The research (round 11 §1) found no reliable early range-vs-trend classifier for MNQ and named a published claim that should NOT be adopted. So instead of a classifier there are five PRE-DECLARED exclusions, each evaluated independently and each computed only from what is knowable at the moment of evaluation — never from the completed session. (a) opening range wider than k× the prior-session median, k resolved from the bound strategy or the tape's own 80th percentile (1.28, n=13); (b) price beyond the initial balance and holding a CLOSED 5-minute bucket there, evaluated continuously; (c) price past every seated reference in the scenario's direction; (d) inside a Tier-1 news blackout — UNKNOWN when the calendar has no slice, and UNKNOWN never excludes; (e) the first N minutes after the open, reusing the existing no-trade band's N.",
    },
    {
      kind: 'p',
      text: 'What the label would NOT have caught, stated here because a label that implies protection it lacks is worse than no label. On 2026-09-03 the New York session authorized exactly three arms, and the one that FILLED — short at 29285.00 at 09:02 — is covered by none of the five: the opening range was 0.77× the median (narrow, not wide); the initial balance did not exist until 09:30; and price never cleared the authored map, because the planner re-seated its levels ahead of price all morning. On the one day we have, this label would have permitted the damaging trade. That is the null the E3 experiment is pre-registered against: the label has no known coverage, and twenty sessions of stamped episodes will show whether any exclusion acquires some.',
    },
    {
      kind: 'p',
      text: "The label is fixed at the episode's open and never rewritten. A row that says not evaluated says exactly that — it is not permission, and the column is NULL rather than false so the two can never be confused. The desk strip counts today's permitted, excluded and not-evaluated episodes from the table; the boot line prints the same counts, names which resolver set k, and carries this coverage note in its own text.",
    },
    { kind: 'h', text: 'THE FIVE-LEG CUTOVER GATE (class 33)' },
    {
      kind: 'p',
      text: 'Before any restart of the bot, GET /api/cutover-gate answers all five legs in one payload: (1) open positions in the database, (2) positions from the API, (3) the NinjaTrader positions snapshot for the bound account, (4) working orders — the broker book cross-checked against placed or unconfirmed ledger rows; armed rows without a signal id appear separately and do not fail this leg, and (5) in-flight planner work. ready:false means HOLD. Legs 4 and 5 are new on 2026-09-02: leg 4 used to be a stub that always answered empty, so it passed at every cutover from 35 to 41 including one with two orders resting; leg 5 did not exist, so a kill on 2026-08-31 17:34 CT landed mid-read and the planner chain died silently. A leg that cannot be evaluated counts as failed.',
    },
    {
      kind: 'h',
      text: 'DATA TRUTH — WHAT PAUSES ENTRIES AND WHAT DOES NOT (release #54)',
    },
    {
      kind: 'p',
      text: 'Measured on the stored MNQ tape (10-19 .. 10-29): the 27 one-minute bars over 60 pt in the last 7 days are all real — data-release, FOMC and open minutes with ES moving the same way and 4-18x the usual volume — so none pauses anything. The two real bad prints in the store (10-19 08:58 and 09:19, a 163 pt wick on a flat ES) are the ones the bad_print rule catches. NT8’s native 4h series holds 94 off-grid stub bars every month (a partial 12:00 CT bar); they are not 17:00-anchored candles and are never part of the swing line. The swing 4h EMA34 the bot used on 10-28 (29645.53) matches the line recomputed from every frame (29645.11) within 0.42 pt — the single defect was a 4-hour hole in the 1m store (10-26 17:00-21:00 CT) that one step of the recurrence skipped; the check measures that gap against the other frames and pauses only when it exceeds 1 pt or a bucket exists in no frame. The same 4-hour hole is the larger problem for the volume levels: with 13% of the volume present six hours into that session, the session VWAP read from the 1m was 72 pt below the same VWAP built from the 5m (still 63 pt at 12 hours), and the next day the prior session’s POC read 254 pt away from the restored profile — a decaying EMA forgets such a hole, a session sum never does, so those levels are recomputed with the missing volume restored and pause entries when they move. On every other session of 10-12 to 10-27 the recomputation moves the VWAP by at most 0.7 pt, so nothing pauses; the two real defects found are the 10-26 hole and a run of 1m bars stored with truncated volume on 10-19 at 21:15 CT (VWAP 2.9 pt off). The 10-29 08:15-08:30 CT open is the other real case: the native 3m/5m/15m bars (rows 2058086-2058088) stopped at about 08:27 — close 28.5 pt off, volume 6523 vs 15501 — while the 1m was complete, so the AI path paused from 08:30 to 09:44 and mentor traders (the 5m range of 28 pt against 54.5 flips their strong-day test) to 09:29; that is incorrect data, not a calibration artifact, and a repair of the native bars from the 1m would clear it sooner. Not covered: the volume levels of sessions older than the prior one (naked POCs of earlier days come from the stored-profile path), and a lost final bar whose last ticks were never delivered.',
    },
    { kind: 'h', text: 'WHEN THE MODEL CALL FAILS (class 49)' },
    {
      kind: 'p',
      text: 'Every failed call to the model now carries one label saying who failed: the socket died, the provider returned an error, one of our own deadlines fired, the answer never arrived, or the plan itself was rejected. That last one is the only case the model can do anything about, so it is the only case where the failure text is sent back to it. Before this, an empty answer or a broken connection was handed to the model as though its plan had been wrong, which is nonsense it then tried to fix. The old label was also usually incorrect: it defaulted to blaming the network and was right about five times in fifty. The stall detector was rebuilt too. It used to reset whenever the provider sent a keep-alive tick, which meant a generation could stall for twenty minutes while looking alive, and it had never once fired. It now watches for real output and only counts silence in the answer itself, and it says so in the log when it fires. Finally, when the provider is overloaded and returning errors, the bot no longer retries harder: the number of calls one plan read may make is capped, and hitting that cap is logged.',
    },
  ],
}
