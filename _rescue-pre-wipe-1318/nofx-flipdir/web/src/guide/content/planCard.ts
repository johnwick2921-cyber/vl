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
      text: "Above the level table sits the STRUCTURE panel. It exists only when the plan doc carries a structure block (day_plan.structure_map ON) — when the block is absent the panel renders nothing, never placeholder rows. One row per timeframe (D, 4h, 1h): a trend arrow (up/down/range), the last swing high and low, the impulse range with premium/discount as a percentage of the way from the impulse low, and the scorer-ranked HTF zones, each carrying its timeframe badge and freshness label. The chart draws the same zones as bands at their raw contract prices with kind·tf labels — never back-adjusted across the roll. STRUCTURE is bias context only: nothing in this panel authorizes or refuses an entry.",
    },
    { kind: 'h', text: 'Scenario level identity' },
    {
      kind: 'p',
      text: 'A new scenario can name the candidate shown on its map with a level ID. The card shows that candidate beside the evaluator’s own anchor. A disagreement is recorded; it does not change a trading decision, gate or order.',
    },
    {
      kind: 'p',
      text: 'The ID includes the primary reference’s symbol, kind, bounds, origin date, formation timeframe and separately captured formation close. The existing candle-open timestamp stays unchanged. Round numbers have no formation event, so their IDs remain NULL. Missing inputs never produce a partial hash.',
    },
    {
      kind: 'p',
      text: 'Legacy scenarios keep NULL IDs by design. Missing or unknown IDs on new plans are accepted with WARN and recorded counters for the first two boots. A refusal requires a later owner ruling after at least five plans have been measured. A named level can belong to several scenarios; the episode record does not choose one arbitrarily.',
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
      text: 'Legacy scenarios retain UNKNOWN by design for economics they never declared. Reading them does not invent or backfill fields and never invokes the new-authoring refusal. At new authoring, missing complete economics is a schema refusal; off-path targets without an explicit exception, obstacles beyond the arm target, and implied R inconsistent with geometry by more than one tick of price distance are contradiction refusals. EXCEPTION (owner ruling): when the machine-computed arm-target R is at or above the minimum R:R floor, the stated value is auto-corrected to the computed value and accepted — the floor gates still refuse every arm below the minimum. An obstacle below 1R and a known role/use difference are WARN plus counter only. A response at an obstacle declares intent; it does not change order management or make a half-contract exit executable. Hypothetical geometry never authorizes an arm.',
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
      text: 'At publication, supported authored invalidation rules are checked against the latest completed five-minute windows using complete minute bars. Explicit one- or two-close above/below price rules can refuse a candidate and re-author inside the existing attempt budget; a refused candidate is never published active. Conditional annotations, compound, sequential, subjective or unsupported wording, and missing or malformed tape, are UNKNOWN and accepted for this check with a warning and recorded count. This check is separate from the live anchor heuristic and changes no entry-gate verdict.',
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
          body: 'price · provenance label (PDH, ONH, nPOC·Tue, RN, EQH…) · planner grade A/B/C · m: machine grade (detector-side: type × freshness × confluence × HTF) · ROLE badge (what the level is FOR) · distance (gold when within 12pt) · touch chip ○ approaching ◐ touching ✕ rejected ▲ accepted · fresh dot (fresh / tested / consumed — consumed rows dim).',
          cite: 'web/src/components/plan/ZoneTable.tsx:28-150 · SessionPlanCard.tsx:682',
        },
        {
          title: '5 · Scenario-row anatomy',
          body: 'S# · condition (reclaim/hold/sweep_reclaim/reject/acceptance/breakout_retest/fvg_entry/breakdown_continue/breakup_continue) · direction · quality A+/A/B/C — a planner judgement, not a measured win rate — judged against the min_scenario_quality floor that MinScenarioQualityFor (store/strategy.go) resolves per session (session override → strategy value → the shipped no-restriction default). At the floor it resolves to today nothing is refused for quality; raise it in Strategy → Day Plan and the arm-time gate refuses a below-floor scenario before the resting order is placed · confirm{} chip CONFIRM MET / not met (machine, advisory; stale ones say so) · TWO-LEG confirms (breakdown/breakup plays) render leg-by-leg: "leg 1/2 MET · leg 2/2 NOT MET → overall NOT MET" — a partial never reads MET · fvg chip IN-ZONE/ABOVE/BELOW/FILLED_INVALID · chain_after: the S# this play FOLLOWS (e.g. fvg_entry after its sweep_reclaim) · targets a→b→c · invalid line.',
          cite: 'web/src/components/plan/ScenarioList.tsx',
        },
        {
          title: '6 · No-trade windows (the band)',
          body: "The MACHINE's sit-out list, not the plan's prose. Three sources, and until 2026-09-09 they did NOT bind the same way: the lunch and first-N bands were read by the AI-decision gate and the adherence grader and by NOTHING on the ARM path, so under plan_mode=strict — where a resting order is the only way in — neither band could refuse an entry. SINCE dispatch 104 both bands bind the arm path as well: an arm inside the band is refused, and an arm already resting when the band opens is cancelled rather than grandfathered. T1 red news bound both paths throughout. The old behaviour is stated here because it is why the check exists, not because it is still true.",
          cite: 'web/src/components/plan/RulesBlock.tsx · kernel/no_trade_band.go · boot line 🗓 no-trade band',
        },
        {
          title: '7 · Death line + flip line',
          body: 'Plan dies if … (structured death{} object, machine-evaluated every cycle) and Flips … (flip_to direction). A prose-only death gets a "PROSE-ONLY" warn at write.',
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
          body: 'Dormant = the plan (or its arm) was parked by a flip/death or no-active-plan — NOT dead. It auto-rearms when price closes back through the mirror buffer (0.5×ATR14, 2 decision-TF closes) and arms re-place on the next cycle.',
          cite: 'kernel/plan_lifecycle.go (dormant + rearm) · trader/armed_executor.go',
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
          budget: 'Consumes the re-align budget (realign_cap, default 5)',
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
  ],
}
