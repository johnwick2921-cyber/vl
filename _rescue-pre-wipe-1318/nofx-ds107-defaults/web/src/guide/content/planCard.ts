import { GUIDE_BUILT_REV, type GuideSection } from '../types'

export const planCard: GuideSection = {
  id: 'plan-card',
  num: 3,
  title: 'Reading the Plan Card',
  tagline: 'The centerpiece — every element, decoded.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    { kind: 'h', text: 'STRUCTURE panel — bias only, not entries' },
    {
      kind: 'p',
      text: "Above the level table sits the STRUCTURE panel. It exists only when the plan doc carries a structure block (day_plan.structure_map stored ON — no Studio control since W-KNOB-PRUNE 2026-09-18; the owner's stored ON is honoured) — when the block is absent the panel renders nothing, never placeholder rows. One row per timeframe (D, 4h, 1h): a trend arrow (up/down/range), the last swing high and low, the impulse range with premium/discount as a percentage of the way from the impulse low, and the scorer-ranked HTF zones, each carrying its timeframe badge and freshness label. The chart draws the same zones as bands at their raw contract prices with kind·tf labels — never back-adjusted across the roll — but only on request: by default the mini chart draws the seated levels alone (one labelled line each) and no zone bands; the 'Show zones (N)' checkbox under the mini chart draws the six HTF zones nearest to the last close, identical bands merged (a per-browser view preference, not a strategy knob). STRUCTURE is bias context only: nothing in this panel authorizes or refuses an entry.",
    },
    { kind: 'h', text: 'Scenario level identity' },
    {
      kind: 'p',
      text: 'A new scenario names the map level it trades with a level ID. The card shows that level beside the evaluator’s own anchor. At write, the named level must sit at the price the scenario trades — its trigger/confirm anchor, within the level’s own zone ±3.00 pts. A disagreement is REFUSED at write: the planner re-authors within the same three attempts, and if every attempt disagrees the read fails closed to the NO-TRADE plan. An anchor inside the named level’s own zone (an FVG’s distal edge, a reject at a supply/demand edge) is not a disagreement. Plans stored before this rule keep their IDs untouched: an old plan’s recorded disagreement is still shown and never rewritten. While a plan is live, a disagreement the evaluator observes is recorded; it does not change a trading decision, gate or order.',
    },
    {
      kind: 'p',
      text: 'The ID includes the primary reference’s symbol, kind, bounds, origin date, formation timeframe and separately captured formation close. The existing candle-open timestamp stays unchanged. Round numbers have no formation event, so their IDs remain NULL. Missing inputs never produce a partial hash.',
    },
    {
      kind: 'p',
      text: 'Legacy scenarios keep NULL IDs by design. On a new plan a NULL level ID is still accepted with WARN (a map row whose own ID is NULL cannot be named). An ID the frozen map does not carry — invented, altered, or a ref| ID whose digest matches no map row — is REFUSED at write and re-authored. A two-anchor setup (a sweep of one level, a reclaim of another) names both in sweep_level_id and reclaim_level_id: two different map IDs, each the level at its own leg’s reference price. Reusing one ID for both, or naming a level at an unrelated price, is refused. A named level can belong to several scenarios; the episode record does not choose one arbitrarily.',
    },
    {
      kind: 'p',
      text: 'Backfill counts refer to episode rows: pre-capture rows are untouched; later rows require every recorded hash input to be recomputed, otherwise they name the missing inputs as unrecomputable. No legacy scenario identity is inferred from a nearby price. Boot counters count recorded plan-version scenarios, not polling ticks.',
    },
    {
      kind: 'h',
      text: 'Scenario economics: obstacle, response and order objective',
    },
    {
      kind: 'p',
      text: 'Newly authored scenarios must state the entry zone, trigger, confirmation, structural invalidation, protective stop, first opposing obstacle with level/family/price provenance, planned response, arm target and both R values. The card separates the target path from the arm order objective. Prices and R describe authored geometry before costs, execution rounding and later stop composition; accepted broker prices remain separate. The earlier scenario-economics contract prescribed no exit policy. The structural-stop research candidate now derives reject-fade stops and targets from frozen zones; the separately recorded composition below is the admission decision.',
    },
    {
      kind: 'p',
      text: 'Legacy scenarios retain UNKNOWN by design for economics they never declared. Reading them does not invent or backfill fields and never invokes the new-authoring refusal. At new authoring, missing complete economics is a schema refusal; off-path targets without an explicit exception, obstacles beyond the arm target, and implied R inconsistent with geometry by more than one tick of price distance are contradiction refusals. EXCEPTION (owner ruling): when the machine-computed arm-target R is at or above the minimum R:R floor, the stated value is auto-corrected to the computed value and accepted — the floor gates still refuse every arm below the minimum. An obstacle below 1R and a known role/use difference are WARN plus counter only. A response at an obstacle declares intent; it does not change order management. A reduce on a single-contract arm is refused at write (one contract cannot be halved). Hypothetical geometry never authorizes an arm.',
    },
    {
      kind: 'p',
      text: 'Obstacle chain (refused at write, new plans only): target_chain must run outward from entry in the trade direction. The first obstacle must be the nearest seated map level strictly between entry and the arm target; a level the seat race cut may be named instead when it is nearer, but it is never required, because the planner never sees the cut pool. With no seated level in between, the first obstacle is the arm target itself. Every other seated level on that path must be listed in path_levels with its role (pass through, reduce or exit); a missing level is refused by name, for example “S4 omits SWG-H·5m 31043.00 (4.00 pts from entry)”. Authored obstacle-chain prices are normalized to the tick grid and the normalization is recorded, never refused. A reduce role on a single-contract arm is refused. The boot line “scenario write truth” prints the recorded count per refusal class, retries included; every count reads n/a until the first check is recorded.',
    },
    {
      kind: 'p',
      text: 'R = absolute price distance / absolute entry-to-stop distance. For two gross outcomes, +bR with probability p and −1R otherwise, and cost cR per trade: E[net R] = p*b − (1−p) − c. Setting E to zero gives p = (1+c)/(1+b). These examples are reference arithmetic, not estimated win rates, fees, or a target recommendation.',
    },
    {
      kind: 'table',
      title: 'Break-even reference arithmetic',
      head: ['Winning payoff', 'No cost', 'Illustrative cost 0.04R'],
      rows: [
        ['0.5R', '66.67%', '69.33%'],
        ['1R', '50.00%', '52.00%'],
        ['2R', '33.33%', '34.67%'],
        ['3R', '25.00%', '26.00%'],
      ],
    },
    {
      kind: 'p',
      text: 'A 50% exit at +0.5R and 50% at +3R totals +1.75R when both succeed. A single MNQ contract cannot be halved. Economics counters count new-authoring checks, including retries, since process boot; they are not independent trades, persisted-plan counts, or reconstructed legacy events. The boot line starts from these recorded process counters.',
    },
    {
      kind: 'p',
      text: 'This is the card the planner writes and the executor trades against. Below is a mock built from the REAL card components (dashed border = example). Every callout maps to a piece of it.',
    },
    { kind: 'mockCard' },
    {
      kind: 'p',
      text: 'Confirmation records name the bucket they judged, its close time, whether it was closed, and the reference event. A 5m close cannot count before that five-minute bucket ends; one completed minute inside it is insufficient. This applies to scenario confirmation, waiting arms, validation and the facts shown to the planner. A minute-only reclaim cannot void a rule that requires a completed 5m reclaim. The plan card and the desk show the recorded verdict and its evidence. The ≈ activation status remains a separate estimate; confirmation alone is not order authorization.',
    },
    {
      kind: 'p',
      text: 'A sequence needs a recorded first event and a second event strictly after it. Missing first-event evidence reads UNKNOWN and does not satisfy the condition; it never falls back to plan publication. For a touch reconstructed from minute OHLC, the closed minute establishes that the touch occurred by its end; the exact tick time is unavailable. A forming touch can be observed, but supplies no ordered reference until that minute closes. A later five-minute close may qualify even when its bucket opened before the touch.',
    },
    {
      kind: 'p',
      text: 'Immediate-mode authoring has a separate rule, 1m_displacement: it measures excursion on completed one-minute bars before a five-minute confirmation is available. This preserves preparation before the entry trigger. It never declares a five-minute confirmation or a five-minute reclaim. The ordinary displacement and void facts use completed five-minute buckets; immediate-mode displacement is named separately. Corrected facts may lead the planner to author different scenarios.',
    },
    {
      kind: 'p',
      text: 'Plan liveness: tradeable N/M counts scenarios whose current evaluator status is waiting, armed or triggered. It is not order eligibility. Missing, unevaluable or stale versioned snapshots show UNKNOWN. Zero remaining is marked EXHAUSTED and records one warning per version; exhaustion does not itself wake the planner or bypass either throttle, the session cutoff or the replan budget. First-observed invalidation records retain the exact plan version, scenario ID, judged anchor, price, cause and time. They are history: the existing evaluator can later change status. Legacy unversioned timestamps are preserved but never attached to a new version. The card names the recorded observation time, not a render time or an inferred candle-death time.',
    },
    {
      kind: 'p',
      text: 'At publication, every scenario invalid line must be exactly one of four forms: "5m close above <price>", "5m close below <price>", "2x5m close above <price>" or "2x5m close below <price>". Anything else (compound, sequential, conditional, subjective or other-timeframe wording) is REFUSED and re-authored inside the existing attempt budget; the refusal quotes each offending sentence and each is counted. The rule is then judged against EVERY five-minute group that closed between the read clock (when the prompt was assembled) and publication, plus the latest completed group, using complete minute bars. A breach in any of them refuses the candidate as born dead. The plan death line refuses the same way when it was met in that span, and a flip line that already fired refuses with a request to re-author on the flipped side. Missing or malformed minutes make that group UNKNOWN: accepted with a warning and a recorded count, never a refusal. A refused candidate is never published active; if every attempt is refused, the existing NO-TRADE plan is written. The card line "invalidation: enforced (grammar) · read … → publish … · N 5m groups judged" is read from the plan row; rows written before this check show n/a. This check is separate from the live anchor heuristic and changes no entry-gate verdict, buffer, window or flip hold.',
    },
    {
      kind: 'p',
      text: 'Order prices are shown side by side for each scenario leg: intended terms from the displayed plan (including overlays), composed terms from the selected arm ledger row, and ACCEPTED terms from a fresh received broker book. Selection uses the displayed version and highest placement sequence per leg; the first authorization version remains separate. Each leg shows its state and provenance, so split legs can disagree. The current ACCEPTED column matches the exact placement signal and its stop/target names; missing, ambiguous, stale, terminal or unconfirmed orders show UNKNOWN. It never substitutes planned prices or historical acceptance records. After a fill, the absent entry can read UNKNOWN while its live protective prices remain visible. Receipt time, age and received AddOn build accompany the book.',
    },
    {
      kind: 'callout',
      title: 'tap to expand — every element',
      items: [
        {
          title: '1 · Bias + conviction',
          body: "The big word (LONG/SHORT/NEUTRAL) + conviction (high/medium/low). Direction and conviction are the AI's read — nothing gates on them.",
          cite: 'web/src/components/plan/BiasBlock.tsx:8',
        },
        {
          title: '2 · The bias-tree line',
          body: 'The planner must NAME the machine branch it took. Branches: 1 close>PDH → bull-continuation HIGH · 2 PDH sweep+reclaim → bear MEDIUM (mirror PDL) · 3 inside the day → close vs PDC, LOW · 4 closed outside but back inside → NO bias · 5 premium/discount vs the dealing-range midpoint · 6 runner = the draw (nearest opposing pool). Out-of-range prices render "BEYOND range (extended)" — never a >100% figure.',
          cite: 'kernel/planner_prompt.go RenderBiasTree',
        },
        {
          title: '3 · v# + trigger pill',
          body: 'Version chips v1…vN (tap any to read it as it was) + the lifecycle chip (ACTIVE gold / EXPIRED / DIED / SUPERSEDED / NO-TRADE red) + why this version was written (owner_reset, level_event, NY_scheduled_read, death_replan, owner_reread…). Only death_replan and owner_reread spend the re-plan budget (class 35 recorded counter).',
          cite: 'web/src/components/plan/chips.tsx:169,252',
        },
        {
          title: '4 · Level-row anatomy',
          body: 'price · provenance label (PDH, ONH, nPOC·Tue, RN, EQH…) · planner grade A/B/C · m: machine grade (detector-side: type × freshness × confluence × HTF) · ROLE badge (what the level is FOR) · distance (gold when within 12pt) · touch chip ○ approaching ◐ touching ✕ rejected ▲ accepted · fresh dot (fresh / tested / consumed — consumed rows dim). An owner-added level (＋ Add / bulk add) is STICKY: it applies at the next planner read, not on save, and until then it is listed — and can be deleted — in the "Pending owner levels" block right under the table (chip pending → applied once the session plan seats it).',
          cite: 'web/src/components/plan/ZoneTable.tsx:28-150 · SessionPlanCard.tsx:682',
        },
        {
          title: '5 · Scenario-row anatomy',
          body: 'S# · condition (reclaim/hold/sweep_reclaim/reject/acceptance/breakout_retest/fvg_entry/breakdown_continue/breakup_continue) · direction · quality A+/A/B/C — a planner judgement, not a measured win rate — judged against the min_scenario_quality floor that MinScenarioQualityFor (store/strategy.go) resolves per session (session override → strategy value → the shipped no-restriction default). At the floor it resolves to today nothing is refused for quality; raise it in Strategy → Day Plan and the arm-time gate refuses a below-floor scenario before the resting order is placed · confirm{} chip CONFIRM MET / not met (machine, advisory; stale ones say so) · TWO-LEG confirms (breakdown/breakup plays) render leg-by-leg: "leg 1/2 MET · leg 2/2 NOT MET → overall NOT MET" — a partial never reads MET · the chip names the rule the machine COUNTED — the scenario\'s STORED confirm.rule / confirm.hold_min (a breakdown storing 2x5m_close reads 2×5m and needs two completed 5m closes); a count taken from the BD_MIN_CLOSES / ACCEPT_HOLD_MIN authoring default because nothing valid was stored reads "(authoring default)" · fvg chip IN-ZONE/ABOVE/BELOW/FILLED_INVALID · chain_after: the S# this play FOLLOWS (e.g. fvg_entry after its sweep_reclaim) · targets a→b→c · invalid line. EXECUTOR verdict (W-ARM-STATE-UI): beside the evaluator\'s 🎯 verdict sits what the EXECUTOR decided for THIS plan version — "not attempted" / "refused: <reason> (<detail>)" / "armed #<id>" / "filled #<id>" / "cancelled: <state_reason>", sourced only from armed_orders rows and the executor geometry records, with the raw reason + record time on hover; no record → nothing renders, never a dash.',
          cite: 'web/src/components/plan/ScenarioList.tsx',
        },
        {
          title: '6 · No-trade windows (the band)',
          body: "The MACHINE's sit-out list, not the plan's prose. Three sources, and until 2026-09-09 they did NOT bind the same way: the lunch and first-N bands were read by the AI-decision gate and the adherence grader and by NOTHING on the ARM path, so under plan_mode=strict — where a resting order is the only way in — neither band could refuse an entry. SINCE dispatch 104 both bands bind the arm path as well: an arm inside the band is refused, and an arm already resting when the band opens is cancelled rather than grandfathered. T1 red news bound both paths throughout. The old behaviour is stated here because it is why the check exists, not because it is still true.",
          cite: 'web/src/components/plan/RulesBlock.tsx · kernel/no_trade_band.go · boot line 🗓 no-trade band',
        },
        {
          title: '7 · Death line + flip line',
          body: 'Plan dies if … (structured death{} object, machine-evaluated every cycle) and Flips … (flip_to direction). A prose-only death gets a "PROSE-ONLY" warn at write. Since 2026-09-17 (W-FLIP-DIRECTION, class 140) the flip SIDE is judged against the bias, not only its price: a short bias flips long only on a close ABOVE the line, a long bias flips short only on a close BELOW — a plan whose flip points the other way is REJECTED at write and repaired, and one already in the store is named once in the journal as flip_direction_inverted the next time it is evaluated, active or dormant (LONDON v3 that day shipped short + flip{below → long} and could never flip on the rally). While the flip line is BREACHED (touched, ≥1 decision-TF close beyond the buffered line) and has not fired, ordinary wakes (level events, structure MSS) are DEFERRED — the flip evaluator owns the plan until it fires or price closes back — and a same-bias wake re-read that keeps the line within 3.00 pt counts its closes from the chain, not from the new version (W-FLIP-OWNS-THE-BREACH, 2026-09-17). Since the same evening (W-FLIP-LINE-SIDE-OF-PRICE, class 148) the line is also judged against PRICE: a flip line must sit on the far side of the authoring price (side above → line above price, side below → below it) because the machine only fires a line price touches from the near side after birth — a line already beyond price is REJECTED at write (the death line obeys the same law: already crossed = born dead), an unknown authoring price is a WARN not a reject, and a stored impossible line is named once as flip_line_beyond_price / death_line_beyond_price (ASIA v2 that night shipped short + flip{29747.50 above → long} with price at 29764 and could never flip).',
          cite: 'web/src/components/plan/BiasBlock.tsx · kernel/plan_doc.go PlanCondition',
        },
        {
          title: '8 · NO-TRADE banners — the two variants',
          body: 'Variant A (fail-closed): "⛔ Plan read failed — sitting out — {reason}" — the read failed after 3 attempts; safe, never stale. Variant B (AI skip-day): the AI\'s own no_trade declaration ("balance day — no A/B zone in reach, skip") — a decision, not a failure.',
          cite: 'web/src/components/plan/SessionPlanCard.tsx:474,510',
        },
        {
          title: '9 · level table rules',
          body: 'Any level count is fine — the per-side minimum is DELETED (owner ruling 2026-08-31, no ⚖ note anymore). The only hard fails left: 0 levels on a side (the 2026-08-18 one-sided-map pathology) and an empty machine map.',
          cite: 'kernel/plan_doc.go ValidatePlanDocWithFactsMachine',
        },
        {
          title: '10 · Armed chips',
          body: '⏳ armed = order authorized, not yet sent · ⏳ placement pending = command registered for sending, awaiting an NT8 receipt · 📌 working = received live entry state from NT8 (entry update or fresh broker book) · ⚡ filled = entry taken (the real fill) · ✕ rejected = received NT8 rejection (verbatim reason when supplied; h1 omits it, so reason unavailable) · cancelled/expired/refused. Command age starts at creation time; cached bar time remains a separate market fact. Payloads older than 60 seconds are refused before sending. No receipt means placement pending with the slot held, even after the wait bound; unconfirmed requests do not draw broker order lines on the chart. The arm is the fast path: it pre-commits the entry so the fill happens at the plan price, not after a 2-minute debate.',
          cite: 'web/src/components/plan/ScenarioList.tsx:218-251',
        },
        {
          title: '11 · 😴 dormant + auto-rearm',
          body: "Dormant = the plan (or its arm) was parked by a flip/death or no-active-plan — NOT dead. It auto-rearms when price closes back through the mirror buffer (0.5×ATR14, 2 decision-TF closes) and arms re-place on the next cycle. The 30-min flip hold counts from the plan's state (session birth, last flip/re-arm, or a bias change) — never from each re-read version.",
          cite: 'kernel/plan_lifecycle.go (dormant + rearm) · kernel/flip_hold_anchor.go · trader/armed_executor.go',
        },
      ],
    },
    { kind: 'h', text: 'Read it in 30 seconds' },
    {
      kind: 'checklists',
      items: [
        {
          title: 'The 6-glance read',
          steps: [
            'Bias word + conviction — what is it trying to do?',
            'Bias-tree line — which branch, and does the reasoning agree?',
            'NO-TRADE banner? — fail-closed vs skip-day changes everything.',
            'Levels — any consumed (dim) rows?',
            'Scenarios — which one is armed, and does confirm say MET?',
            'Death/flip — what kills the plan, and at what price?',
          ],
        },
      ],
    },
    { kind: 'h', text: 'The four buttons' },
    {
      kind: 'buttons',
      items: [
        {
          label: '↺ Reset planner',
          api: 'POST /api/plan/reset (api/handler_plan.go:1049)',
          sideEffects:
            'Abandons the chain (history + death reasons preserved), re-arms the full re-plan budget, clears NO-TRADE, reads a fresh plan now. Positions and brackets never touched.',
          budget: 'New chain starts at v1 with the full cap (default 4)',
          undo: 'None — but history stays readable, and nothing is deleted.',
          useWhen:
            'The plan is wrong, stale, or a fail-closed NO-TRADE sits where a tradeable day exists. Confirm text: "Abandon this plan chain and start fresh?"',
        },
        {
          label: '⟳ Re-read',
          api: 'POST /api/plan/reread (api/handler_plan.go:1001)',
          sideEffects:
            'One more planner call → a new version on the SAME chain.',
          budget:
            'Costs 1 re-read from the session budget (shows "spend one of N?")',
          undo: 'Old versions stay tappable via the version chips.',
          useWhen: 'You want a second opinion without abandoning the chain.',
        },
        {
          label: '⟳ Re-align plan',
          api: 'POST /api/plan/realign (api/handler_plan.go:1906)',
          sideEffects:
            'Planner reviews your edit and proposes a merged plan change ("would become v{n}") — you Apply merge or Keep as-is.',
          budget:
            "Consumes the re-align budget (5 per plan, or the strategy's stored realign_cap — folded, no control)",
          undo: 'Keep as-is declines; applied merges are versions (tappable).',
          useWhen:
            'After an owner level edit you want the planner to re-anchor around.',
        },
        {
          label: 'Approve',
          api: 'POST /api/plan/approve (api/handler_plan.go:1799)',
          sideEffects:
            'Grants entries for this CME session-day when approval_required is ON. One click, no modal (by design).',
          budget: 'None — one grant per session-day.',
          undo: 'None needed — it only unlocks the gate the strategy asked for.',
          useWhen:
            'The strategy has approval_required ON and you accept the plan as-is.',
        },
      ],
    },
    { kind: 'h', text: 'The roll-day chart' },
    {
      kind: 'p',
      text: "The chart across a roll derives the prior contract's segment from ITS stored 1m rows (the planner's own bucket helper) and shifts the whole prior segment by the basis measured at THAT timeframe's own seam, so the roll day shows neither a seam hole nor the ~290-point basis cliff. Where the prior contract's 1m rows have interior gaps the current contract's 1m rows cover, the gaps are filled with the current rows converted into the prior contract's price space; gaps neither contract has stay gaps (never fabricated). Derived prior bars draw muted with a one-line legend read from the response envelope — 'pre-roll (Sep) · basis-adjusted +290.00', or unadjusted with the reason when the basis pair cannot be measured; hovering a derived bar shows its raw and adjusted close. The current contract is never touched and volume is never shifted. CHART_ROLL_STITCH=legacy serves the pre-wave stored-aggregate stitch byte-identically.",
    },
    { kind: 'h', text: 'Picture HTF scenarios on the card (W5)' },
    {
      kind: 'p',
      text: "A Picture HTF setup is recorded as a Day Plan scenario before any order exists, in its own id namespace (P1, P2 …) so it never collides with the planner's S ids. Its row carries a 📷 PICTURE badge. Hover the badge: the rule and its version (h1_close_break), the H1 close against the 4H body it broke, the eligibility window ('window until HH:MM:SS CT' — it is never placed after that), the stop and where the stop came from, and the R:R floor it was admitted on. All of it is read from the scenario's machine record and the evidence frozen when the setup was handed to the plan; the card recomputes nothing, and no owner edit or AI commentary can change it. A missing number reads n/a, never 0; evidence the card cannot read says 'evidence unavailable', and a scenario recorded without evidence says 'evidence not recorded'.",
    },
    {
      kind: 'p',
      text: "Its entry line is the market_in_zone line with the source added: 'Entry: around <entry> (zone <lo>–<hi>) · <status> · source picture (h1_close_break)', and while the order is still armed '· window closes HH:MM:SS CT'. Hover the line for the method (for example market_in_zone limit — read from the order record, never guessed) and the opportunity key. The plan API serves these as source, source_ref, rule, method and eligible_until_ms on the scenario's order leg; a planner scenario's leg carries none of them, so its line and its API output are unchanged.",
    },
    {
      kind: 'p',
      text: "The 📷 chip in the header: a NEUTRAL grey '📷 PICTURE → <route>' says how Picture reaches the market (for example 'Day Plan scenario (market_in_zone limit)'); it is not a refusal. A RED '📷 PICTURE <reason>' appears only when something actually refuses Picture. Neither appears while Picture is off for the trader. Both texts are read from the trader, never composed by the card.",
    },
    {
      kind: 'p',
      text: "MACHINE-AUTHORED plan banner: when a Picture setup arrives and the session has no plan yet, the machine writes plan v1 holding only that scenario, and the card says 'MACHINE-AUTHORED plan — Picture HTF; the first AI plan supersedes it'. The AI's session read still runs and writes the next version.",
    },
    {
      kind: 'p',
      text: "composed of: under the header a small line reads what made the plan on screen, from the fold's own record — 'composed of: base + overlays o1,o3 · machine P1 (o4)': the base version, your applied overlays by overlay version, and each Picture scenario with the overlay that carried it (hover for its opportunity key). Picture scenarios are folded after your overlays and never count against the scenario cap. When the server sends no such record the line is not shown; a missing overlay number reads n/a.",
    },
    {
      kind: 'p',
      text: "Dashboard → Picture HTF opportunities: an opportunity the Day Plan took over shows, under its stage, '→ Day Plan P1 · <plan id> v<n> · arm #<row> <state>' — the armed order that carries it, read from the armed-order ledger. No line means no armed order carries that opportunity yet. If the server could not read the armed orders at all, the panel says 'Day Plan links unread: <reason>' rather than showing every row without a link.",
    },
    {
      kind: 'p',
      text: "Strategy → Day Plan → Picture HTF: under the 'Include Picture HTF setups' switch the hint reads 'Source selector: Picture HTF setups become Day Plan scenarios (limit at the far edge of a small zone, 1 contract, under the Day Plan master)'. The switch's default is unchanged (off).",
    },
  ],
}
