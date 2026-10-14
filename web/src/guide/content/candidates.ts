import { GUIDE_BUILT_REV, type GuideSection } from '../types'

// W3 — candidates, not entitlements (2026-09-09).
//
// Block kinds are deliberately p / h / cards / callout / knobs: those are the
// ones collectHits() indexes (GuidePage.tsx:58-147), so every sentence here is
// reachable from the guide's search box. A `code` or `table` block would be
// invisible to it.
export const candidates: GuideSection = {
  id: 'candidates',
  num: 15,
  title: 'Candidates, Not Entitlements',
  tagline: 'A level on the map is not a permission to trade it.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    { kind: 'h', text: 'Level zones — the full read-time map' },
    {
      kind: 'p',
      text: 'The Level zones panel freezes every detector reference from the planner read, including references that lost a scored seat. Native bands retain their bounds. Point widths use max(defining wick, k × ATR of the source timeframe), with k=0.5 [I]; a missing defining wick or ATR stays NULL. Round numbers use a 2-point band [I]. The chart itself still draws the authored levels; the full zone map is a separate card panel.',
    },
    {
      kind: 'p',
      text: 'A reference joins the single nearest compatible cluster: original anchors must be within m × ATR5m, m=0.5 [I], and the resulting band must be no wider than 1.0 × ATR5m [I]. The first anchor stays fixed; clusters never join transitively. Native bands above the broad threshold of 1.0 × ATR5m [I], or above the merge width cap, remain separate context. All source names survive. Unknown source widths are counted and remain separate: an unknown width cannot pass the maximum-width condition.',
    },
    {
      kind: 'p',
      text: 'The five display families are swing-structure, volume-node, round-number, session/derived, and imbalance. Their count is capped at 3 [I]. The score confluence input and score HTF multiplier are unchanged. The zone shortlist never reads that score: its rank is log(1 + known prior touches) + round proximity + capped families − distance/ATR5m, with weights 1 each [I]. Unknown touch counts remain UNKNOWN and contribute no touch bonus. Prior touches require a known formation and complete available post-formation 1m tape; they are not a historical hit rate. A shortlist place gives no permission to trade.',
    },
    {
      kind: 'p',
      text: 'Test ranges [I]: width k 0.25–0.75; merge m 0.25–0.75; maximum width and broad threshold 0.5–1.5 ATR5m; round width 1–4 points; family cap 1–5; each rank weight 0.25–2. The shortlist cap uses the bound session configuration unchanged (12 on the measured owner read). Zones versus lines and multi-timeframe confluence remain UNTESTED. Prior-touch and round-number research motivates the features, not these weights. Historical plans are not backfilled.',
    },
    { kind: 'h', text: 'Legacy scored map and execution roles' },
    {
      kind: 'p',
      text: 'The map used to hand every seated level to the model as if each were a place to trade. It was not — a level is a reference. This section is about the difference between a reference the machine drew and an entry it is willing to offer, and about what now exists BEYOND the edges of the map.',
    },

    { kind: 'h', text: 'The whole map is kept' },
    {
      kind: 'p',
      text: 'Nothing is ever deleted for failing to qualify as an entry. A level cut from the entry shortlist stays in the map, in the plan document and on the chart, carrying a role that says what it IS for: a target, an obstacle, or an invalidation. Exclusion is not invalidation — that is a standing rule of this system, not a new one.',
    },

    { kind: 'h', text: 'Overlapping references merge into one candidate' },
    {
      kind: 'p',
      text: 'A prior close, a VWAP band and a supply zone within a few points of each other are not three confirmations. They are three NAMES for one price, and often three descriptions of the same historical event. The map now shows them as ONE candidate wearing all its names — "29657.38 — Supply·1h · PDC · VWAP+1σ" — and that candidate counts once.',
    },
    {
      kind: 'callout',
      title: 'What the merge does and does not touch',
      items: [
        {
          title: 'What merging does NOT do',
          body: 'It does not change any score. The confluence term that feeds the grade is untouched by this wave, so a price wearing three names still earns the score it earned before. Merging changes what you SEE and what gets ranked, not what gets computed.',
          cite: 'kernel/map_candidates.go — presentation and ordering only',
        },
        {
          title: 'Why not fix the score too',
          body: 'Because nobody has measured whether confluence is worth anything. The research this wave is built on lists confluence as a hypothesis requiring incremental testing, and records the only direct test as contradicted. Changing the score on an untested belief would be inventing an edge, not finding one. Experiment E4 is the controlled comparison; two findings are already written down waiting for it.',
        },
      ],
    },

    {
      kind: 'h',
      text: 'A level is an entry candidate only if it has somewhere to go',
    },
    {
      kind: 'p',
      text: 'This is the one new refusal in this wave. To be offered as an ENTRY, a candidate needs an opposing reference — the next merged candidate in the trade direction — far enough away to be worth reaching. The default distance is the stop floor, which makes a first target sitting inside the risk impossible by construction. A candidate that fails this stays on the map as a target or an obstacle, and the map says why in words.',
    },
    {
      kind: 'callout',
      title: 'The scope of the refusal',
      items: [
        {
          title: 'It refuses candidacy, never your scenario',
          body: 'If the model has already authored a scenario, this rule does not touch it. It governs which levels are OFFERED as entries before anything is authored. Nothing that was tradeable yesterday becomes untradeable because of it.',
        },
      ],
    },

    { kind: 'h', text: 'The shortlist is ordered by reachability' },
    {
      kind: 'p',
      text: 'Among entry candidates the NEAREST is listed first, measured in ATR. A level 150 points away may be beautifully graded and still be a level price will not reach today. The score is carried on every row and shown — it simply ranks second. This ordering is unvalidated: it is a starting point for measurement, not a discovered edge, and it is labelled that way on the boot line and here.',
    },

    { kind: 'h', text: 'Projections — what exists beyond the map' },
    {
      kind: 'p',
      text: 'On 2026-09-03 the market ran 483 points. Once price passed the highest level on the map there was nothing above it, so there was no target left and the fade book kept selling into a trend. The map now carries references projected beyond its own edges so that cannot happen silently.',
    },
    {
      kind: 'cards',
      cards: [
        {
          title: 'Prior-week high / low',
          body: 'Read from DAILY bars. These were emitted before but could never appear: the guard needs three full days and the intraday ring holds about 33 hours, so they were silently unreachable. The guard was NOT relaxed — relaxing it would compute a "prior-week high" from a day and a half, which is not one. The prior week is the CME week Monday→Friday by TRADING day (release #22): NT8 stamps a daily bar on the calendar day BEFORE its trading day, and the old window took last Tuesday→Friday plus THIS Monday, up to ~250 pts off. From Sunday 17:00 CT the week that just closed is the prior week.',
          tag: 'daily source',
        },
        {
          title: 'Round numbers beyond the range',
          body: 'The round-number detector already existed for prices near the map. Its window now extends outward past the mapped edges.',
          tag: 'extended window',
        },
        {
          title: 'ATR-projected session extreme',
          body: 'The session open plus and minus one daily ATR. A day that travels a full daily ATR from its open is ordinary, so this projects the ordinary case rather than an outlier.',
          tag: 'unvalidated',
        },
        {
          title: 'Measured move',
          body: 'The height of the last completed swing, carried forward from the break. Inside the swing there is no completed break, so nothing is projected rather than a guess.',
          tag: 'unvalidated',
        },
      ],
    },
    {
      kind: 'callout',
      title: 'How to read a projection',
      items: [
        {
          title: 'A projection is arithmetic, not evidence',
          body: 'Every projected reference carries the word "projection" and the method that produced it, on the card and in the model\'s own table, and carries NO detector grade — an empty grade renders as a dash rather than a letter it did not earn. Projections are targets and obstacles only — a projection is never offered as an entry.',
        },
      ],
    },

    { kind: 'h', text: 'The numbers behind it' },
    {
      kind: 'cards',
      cards: [
        {
          title: 'merge width',
          body: 'One zone-width — 12 ticks = 3.00 pt on MNQ. References closer than this are treated as one reference.',
          tag: 'unvalidated',
        },
        {
          title: 'minimum target distance',
          body: 'The stop floor, 1.5 x ATR5m by default. Below this a level is not offered as an entry.',
          tag: 'unvalidated',
        },
        {
          title: 'entry order',
          body: 'Reachability — nearest first, measured in ATR. The score is carried and shown but ranks second.',
          tag: 'E4 pending',
        },
        {
          title: 'seats (max_levels)',
          body: 'Per-trader; package default 8, hard cap 12. It is a TOTAL, not per side. Merging and candidacy do not change it.',
          tag: 'resolved per trader',
        },
        {
          title: 'session-extreme multiple',
          body: 'One daily ATR either side of the session open (SESSION_EXTREME_ATR_K).',
          tag: 'unvalidated',
        },
      ],
    },
    {
      kind: 'callout',
      title: 'Honesty about what is proven',
      items: [
        {
          title: 'Everything here is marked unvalidated on purpose',
          body: 'Merging, the target minimum, the reachability order and two of the four projection methods are all inventions until they are measured. They are labelled that way on the boot line, in the code and in this page. If a later page tells you one of them is proven, check that the measurement exists before believing it.',
        },
      ],
    },
  ],
}
