import { GUIDE_BUILT_REV, type GuideSection } from '../types'

export const guards: GuideSection = {
  id: 'guards',
  num: 6,
  title: 'Guards & Safety',
  tagline: 'What can hard-block a trade vs what only informs.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    { kind: 'h', text: 'One open position per instrument' },
    {
      kind: 'p',
      text: "While any position is open, EVERY entry is refused — either side, any plan version, on the arm path and the decision path alike: 'refused: position 591 open (v2 S1 short); no adds, no flips' (owner ruling 2026-09-03). The predecessor guard refused only the OPPOSITE side, because on a netting account an opposite-side fill silently nets the position; a same-side add was explicitly out of scope. The block on re-arming the SAME scenario in the same plan version lives in the store (MANUAL-CANCEL-WINS) and survives a restart, but a NEW plan version re-authorizes a terminal row — so a v3 S1 short could have added to a v2 S1 short position that was still open. An arm explicitly authored as an exit leg is exempt: that is how a position gets flattened.",
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
      text: 'The AddOn sends its working-order book every 30 seconds and whenever an order changes state. Leg 4 compares that book with placed ledger orders. An armed row with no signal id is only an authorization: it appears on a separate informational line and does not fail the gate. A placement awaiting a broker receipt still counts as working/unconfirmed and blocks cutover, even if the book is empty. Working orders at either source, or disagreement between them, fail the leg. Terminal rows are excluded by one shared classifier, also used by audit queries. A book older than 60 seconds is refused as stale. Before the first AddOn snapshot, the existing explicitly labelled ledger fallback remains; it is not broker proof.',
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
      text: "Editing the AddOn source changes nothing until you recompile it in NinjaTrader (F5) and restart NT8 — NinjaTrader keeps executing the DLL it last compiled. The bot now prints, at every boot, the build id it has RECEIVED on the wire next to the one it expects: '🔌 nt8 addon: build_id=… expected=… match=yes|NO'. It says NO — loudly, every boot — until a frame from the running AddOn proves otherwise. A build id read from our own source would report success for a change that never landed, which is precisely how a distributed change gets believed without being made.",
    },
    {
      kind: 'p',
      text: 'From 2026-09-05 that build id does more than report. A stop entry is REFUSED unless the received build proves it will hand NinjaTrader the trigger in the stop-price argument — because the build before it did not, and the result was an order NinjaTrader accepted, listed in its book, and then never worked: a stop with a trigger of zero. Twenty-two of them over two days, none filled, no reject and no error anywhere. Nothing in a log that reads the value we meant to send can catch that; only a build the far side proves by answering can. Limits are untouched by the floor, and the bot states the posture directly on its first armed cycle — and again if it changes: "🎯 stop-entry: seam=… · slots=… · guard=stop-side · unknown=no-op · addon build_id=… expected=… match=…", where slots reads "unproven" until a new enough build answers. Read the seam field FIRST: it is the switch the placement branch consults before any of the others, and from the 2026-09-05 cutover it reads OFF — no stop entry is placed at all, by owner ruling, until a wave lands that confirms a broker cancel instead of assuming one. Limits are unaffected either way. It is not a boot line in the strict sense: it needs a plan with arms and a bound NinjaTrader connection, so a quiet day may not print it at all.',
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
          'No bars, no bridge, no trading — cycle skipped or refused.',
        ],
        [
          'Unprotected position (2026-09-07)',
          'HARD',
          "Every minute while a position is open, and again the moment the NinjaTrader link comes back: is there a LIVE stop at the broker for it? If the book shows none, a P0 is raised AND a stop is placed — at the price the broker itself accepted, or failing that the plan's composed stop. It is the one check that acts rather than reports, because a position with no stop has no safe amount of waiting. Three things stop it acting: no fresh book, a protective order in a state this build cannot read, or a live stop whose quantity the book does not carry. Each says so in the journal and does nothing — an unknown is not permission to place a SECOND stop beside an invisible one. A PARTIALLY covered position is raised, never patched. Born from 2026-09-06 23:37:02, when a cancel meant for an already-filled entry took its stop with it and the position ran 8h19m unprotected with nothing in the bot looking.",
        ],
        [
          'Boot sweep (class 33)',
          'HARD',
          'At boot, before anything is armed: every resting order left behind by the PREVIOUS process is cancelled at NinjaTrader and marked cancelled in the ledger (reason boot_sweep). A cancel that FAILS leaves the row live and retries — the ledger never goes clean while an order might still be at the broker. On 2026-09-02 00:16 CT, before this existed, two arms outlived their process for 15 minutes and briefly double-ordered S3. Since 2026-09-07 the sweep asks the broker before each cancel: a pre-boot row whose entry FILLED before the restart is left alone, because the only orders under that signal are its stop and target.',
        ],
        [
          'plan_mode direction/strict',
          'HARD',
          'Refuses entries against plan bias (direction) or without a cited scenario (strict); no plan + direction/strict = no trades.',
        ],
        [
          'min_confidence',
          'HARD',
          'Confidence below the floor (default 60) → entry refused.',
        ],
        [
          'MIN-SL (env MIN_SL_ATR_MULT, 1.0)',
          'HARD',
          'Stop closer than the floor (×ATR + 2-tick clearance) → refused.',
        ],
        [
          'HTF veto',
          'HARD',
          "Entry against the HTF regime at a veto anchor → refused. MODE (HTF_VETO_MODE): 1h | cross | 4h — LIVE = cross: vetoes only when 1h AND 4h both agree (the 2026-08-28 autopsy: 1h-only blocked 3 would-have-won arms = +$352, 4h was RANGING at all 7 → cross blocks nothing the evidence doesn't support).",
        ],
        [
          'ARM floors (ARM_MIN_RR 2.0)',
          'HARD',
          'The resting-order gate: R:R ≥ 2.0 AND stop ≥ 1.0×ATR5m or the arm is REFUSED every cycle.',
        ],
        [
          'Entry gate (class 48) — ONE gate, BOTH paths',
          'HARD',
          'Before any order leaves — resting arm or AI market entry — the SAME chain runs: scenario direction vs the cited scenario, shadow map (0C: breakout_retest + fvg_entry are authored + scored but NEVER placed), R:R vs min_risk_reward_ratio judged at the LIVE execution price (not the prompt snapshot), min-SL ×ATR5m, one-live-arm. Refusals are recorded per path. (2026-09-02: 587 and 589 filled BELOW the 2.0 floor because the floor was judged on a stale snapshot; 589/590 traded the shadowed breakout_retest.)',
        ],
        [
          'T1 red news blackout',
          'HARD',
          'No entries in the ±15m window around T1 events (calendar).',
        ],
        [
          'Lunch / session windows / EOD flat',
          'HARD',
          'Outside an enabled session window, inside the lunch or first-N no-trade band (the lunch window is the one kernel.LunchWindowCT() resolves — read, never a literal), or past the session close: no NEW entry, and flat at session end. Since 2026-09-09 the band binds the ARM path too. Until then it was read by the AI-decision gate and the adherence grader and by NOTHING on the arm path, so under plan_mode=strict — where a resting order is the only way in — it refused nothing. An arm inside the band is now refused, and an arm already resting when the band opens is CANCELLED rather than grandfathered: refusing only NEW arms while one placed at 11:58 rests into 12:00 is a band that stops authoring and not entering.',
        ],
        [
          'Consecutive-loss breaker',
          'HARD',
          'After N consecutive losing closes in one CME session-day, no new entry on EITHER path until the 17:00 CT roll. N is 8 by default and the owner may set it; a WARN at 5 counts and surfaces without refusing. This is the ONE session limit that is not gated by the guardrails master, so it bites whether or not that switch is on — and since 2026-09-09 it is wired to the ARM path as well as the decision path, which under plan_mode=strict is the only path that trades. Honest about its own reach: on the retained tape the longest run of losers is SEVEN, so a threshold of 8 would never have fired. An UNRESOLVABLE P&L ends a run rather than bridging it — an unknown outcome must not push the desk toward a halt.',
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
          cite: 'kernel/engine_position.go:188',
        },
        {
          title: 'no matched scenario cited (strict mode)',
          body: "plan_mode=strict and the action didn't cite an armed S#. The plan is the law.",
          cite: 'trader/auto_trader_planconfig.go:206-249',
        },
        {
          title: 'against the plan (direction mode)',
          body: 'The bias is long and the entry is short. Advisory says fine; direction says no.',
        },
        {
          title: '|entry−SL| below MIN_SL_ATR_MULT × ATR',
          body: 'The stop is too tight for the volatility floor.',
          cite: 'kernel/engine_position.go:196',
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
          'Entries not citing an armed scenario; ANY entry with no active plan',
          'Only on-plan, scenario-cited entries.',
        ],
      ],
    },
    {
      kind: 'p',
      text: 'Strict\'s warning, plain: "no plan = no trades" — a fail-closed day in strict mode is a flat day, by design. Strict is the optional NY experiment. Per-session overrides exist (Strategy → Day Plan → Sessions).',
    },
    { kind: 'h', text: 'Guardrails + SIM lock' },
    {
      kind: 'p',
      text: 'Risk guardrails: TWO switches must be on before the daily loss limit does anything. The master (guardrails_enabled) arms the whole block, and daily_loss_enabled arms this leg — both are OFF today, so the configured $450 limit is DECORATIVE and enforces nothing until both move. The desk strip DAY line says exactly that, in those words. The master also arms the daily profit/trade limits, re-entry cooldown, blackout windows, max-contracts and notional caps; the always-on pair (max contracts/order, notional cap) needs no toggle. The consecutive-loss breaker is deliberately OUTSIDE all of this and works regardless. Would-have-tripped counters are visible in the dashboard. SIM lock: every account list is filtered to SIM; the bot cannot route to a live NT account — do not try.',
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
    { kind: 'h', text: 'THE FIVE-LEG CUTOVER GATE (class 33)' },
    {
      kind: 'p',
      text: 'Before any restart of the bot, GET /api/cutover-gate answers all five legs in one payload: (1) open positions in the database, (2) positions from the API, (3) the NinjaTrader positions snapshot for the bound account, (4) working orders — the broker book cross-checked against placed or unconfirmed ledger rows; armed rows without a signal id appear separately and do not fail this leg, and (5) in-flight planner work. ready:false means HOLD. Legs 4 and 5 are new on 2026-09-02: leg 4 used to be a stub that always answered empty, so it passed at every cutover from 35 to 41 including one with two orders resting; leg 5 did not exist, so a kill on 2026-08-31 17:34 CT landed mid-read and the planner chain died silently. A leg that cannot be evaluated counts as failed.',
    },
    { kind: 'h', text: 'WHEN THE MODEL CALL FAILS (class 49)' },
    {
      kind: 'p',
      text: 'Every failed call to the model now carries one label saying who failed: the socket died, the provider returned an error, one of our own deadlines fired, the answer never arrived, or the plan itself was rejected. That last one is the only case the model can do anything about, so it is the only case where the failure text is sent back to it. Before this, an empty answer or a broken connection was handed to the model as though its plan had been wrong, which is nonsense it then tried to fix. The old label was also usually incorrect: it defaulted to blaming the network and was right about five times in fifty. The stall detector was rebuilt too. It used to reset whenever the provider sent a keep-alive tick, which meant a generation could stall for twenty minutes while looking alive, and it had never once fired. It now watches for real output and only counts silence in the answer itself, and it says so in the log when it fires. Finally, when the provider is overloaded and returning errors, the bot no longer retries harder: the number of calls one plan read may make is capped, and hitting that cap is logged.',
    },
  ],
}
