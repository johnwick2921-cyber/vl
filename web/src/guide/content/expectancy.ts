import { GUIDE_BUILT_REV, type GuideSection } from '../types'

export const expectancy: GuideSection = {
  id: 'expectancy',
  num: 14,
  title: 'Expectancy Table',
  tagline:
    'What each play has actually paid — and when that number is allowed to mean anything.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    { kind: 'h', text: 'Stage A research record' },
    {
      kind: 'p',
      text: 'The research snapshot keeps five linked kinds of evidence: market receipts, candidate scores and selection, plan inputs and authoring attempts, scenario permissions, and execution outcomes. It records existing decisions. It does not change scoring, confirmation, order authorization, targets or sizing.',
    },
    {
      kind: 'p',
      text: 'NULL means the field was not captured; its reason travels with the record. A measured zero remains zero. Observation, receipt, publication and permission times are separate. A late historical bar becomes available at receipt, even when its market timestamp is earlier. A scenario activation is not an order authorization. A fill or book price without an entry link remains unclassified; protective prices do not become entry prices.',
    },
    {
      kind: 'p',
      text: 'A boot line with schema=UNKNOWN means the research archive is unavailable; no capture is proven live. Only exported research bundles may supply the experiments in the trading-policy research. The manifest identifies the writing revision, schema, row counts and checksum. Missing records and dropped batches limit what can be concluded. The boot latency measures queue admission; it does not measure the full planner read. Earlier empty components cannot be recovered as original measurements, and historical contract adjustment policy remains unknown when the feed did not supply it.',
    },

    { kind: 'h', text: 'What the table is' },
    {
      kind: 'p',
      text: 'One row per play, sliced by condition, session, level kind, entry path (armed or decision), and era (before or after the 0B exit-sanity boot on 2026-09-02 07:49:06 CT). Each row shows how many closed trades it rests on, how many won, the total and average result, the spread, an interval on the win rate, an interval on the average, and — this is the part that makes it auditable — the exact position ids the row was computed from. Any number in the table can be recomputed by anyone holding the ids. Every prior money verdict in this system was irreproducible because no stored artifact carried its sample.',
    },
    { kind: 'h', text: 'What the table is NOT' },
    {
      kind: 'p',
      text: 'It is not a verdict, and below the floor it does not offer one. A row with fewer than 30 closed trades reads DESCRIPTIVE ONLY: the numbers are shown, and no PASSES or FAILS is rendered from them. Today almost every row will say that, and that is the table working, not the table failing. It is also not a gate: nothing in this panel can block, size, or route a trade, and no setting on this page changes what the bot does.',
    },
    { kind: 'h', text: 'The promotion criterion, verbatim' },
    {
      kind: 'code',
      title:
        'Pre-registered — chosen before the data was seen, and computed, never typed',
      lines: [
        'PASSES  ⟺  n ≥ 30',
        '          AND mean > 0',
        '          AND the 95% interval on the mean excludes 0',
        '',
        'FAILS            n ≥ 30 and the above does not hold',
        'NOT ENOUGH DATA  n < 30  (DESCRIPTIVE ONLY — no verdict is rendered)',
      ],
    },
    {
      kind: 'p',
      text: 'Pre-registered means the floor and the rule were fixed in advance and are computed by the same binary that renders the row. The panel does not carry its own copy of either — both arrive in the payload — so the page can never show a status the engine did not compute.',
    },
    { kind: 'h', text: 'Which money the table reads' },
    {
      kind: 'p',
      text: "pnl_corrected, only. A trade whose corrected P&L is unresolved is EXCLUDED and COUNTED — it never falls back to the raw recorded figure and it never counts as a zero. The count of what was excluded sits beside the table, by reason: unresolved P&L, manual NT8 trades (origin=manual — not the bot's, release #22), unresolvable plan link, test-seam artifacts, and rows with no recoverable play. Realized R is the trade's P&L over the risk of the WHOLE position (entry quantity × stop distance × $/pt); until release #22 it used the first partial fill's size and read a 5-lot's −1.00R as −5.00R. Crypto-era trades from before the day-plan era are ABSENT rather than excluded: they were never in scope, and calling them excluded would imply we looked at them and refused.",
    },
    { kind: 'h', text: 'Blanks are honest, zeros would not be' },
    {
      kind: 'p',
      text: "MAE, MFE, and the stop-hit and target-hit shares render as blank until the trade-excursion recorder has rows to give them. A blank means not measured; a zero would read as a measurement, and these are different facts. Rows closed before 2026-09-05 record close_reason 'sync' — which says how the row was written, not how the trade ended — so their stop-hit and target-hit shares stay blank and are never inferred from price or from the sign of the P&L. From that date the cause is written from NT8's own exit event ('stop', 'target', 'manual'), and a close with no broker event still records 'sync' rather than a guess. Note that a stop can be profitable: two rows in this store exited at their protective stop in profit, so 'stopped out' and 'lost' are not the same column.",
    },
    { kind: 'h', text: 'The counterfactual block is a different table' },
    {
      kind: 'p',
      text: 'Below the realized rows sits the E8 shadow block: what the shadowed confirm rules WOULD have produced. It is kept in its own table with its own column set and every row is labelled counterfactual, because a counterfactual number and a realized one must never be added, averaged, or compared as if they were the same quantity. While the E8 short-side sign defect is open, short rows carry SHORT ROWS SUSPECT — and rows whose direction cannot be recovered stay suspect too, since a side we cannot determine is a side we cannot clear.',
    },
    {
      kind: 'table',
      title: 'Reading a row',
      head: ['Column', 'Means', 'Watch for'],
      rows: [
        [
          'n',
          'closed trades behind the row',
          'below 30 the row carries no verdict',
        ],
        [
          'mean',
          'average result per trade, on corrected P&L',
          'a large mean on a small n is noise wearing a number',
        ],
        [
          'mean interval',
          '95% interval on that average',
          'if it straddles 0 the play has not been shown to pay',
        ],
        [
          'win rate + interval',
          'share of winners, Wilson interval',
          'a high rate with a negative mean is small wins and large losses',
        ],
        [
          'excluded',
          'rows in this key with unresolved P&L',
          'a big number here means the row is thinner than n suggests',
        ],
        [
          'ids',
          'the exact positions behind every figure',
          'this is what makes the row checkable',
        ],
      ],
    },
    { kind: 'h', text: 'The instruments drawer underneath' },
    {
      kind: 'p',
      text: 'Below the table sits one collapsed dropdown — "Instruments · discipline · MAE/MFE · level gate (descriptive)". It holds the three measurements that are not yet good enough to decide anything with, which is exactly why they are folded away: giving them the same weight as the table above would imply they carry the same authority. Every row inside names its source and its sample size, so no number in there has to be taken on trust.',
    },
    {
      kind: 'table',
      title: 'What is in the drawer, and what each one is worth today',
      head: ['Instrument', 'Source', 'State'],
      rows: [
        [
          'DISCIPLINE',
          'GET /api/plan/trades — adherence summary',
          'Real GPA over graded trades, but the test-seam rows are NOT yet excluded from it. It says so on the row rather than implying an exclusion that has not happened.',
        ],
        [
          'MAE/MFE',
          'trade_excursions, via the expectancy rows',
          'Reads "no excursion rows yet" until the excursion recorder has data. It never touches the legacy mae/mfe columns on the position row: those default to 0, so averaging the non-zero ones selects a biased subset and reports it as the whole.',
        ],
        [
          'LEVEL GATE',
          'level_stats, via GET /api/plan/stats',
          'Legacy — retired, pending its replacement. Shown as raw touch counts and a frozen weekly verdict, never as a live rate with a significance claim. When the replacement lands this reads p(hold) with a sample size and an interval, and stays DESCRIPTIVE ONLY below n=200.',
        ],
      ],
    },
    { kind: 'h', text: 'Where it lives' },
    {
      kind: 'p',
      text: "Studio → Expectancy. The panel reads GET /api/expectancy and shows the timestamp of the last closed trade it includes, not the time you opened the page — a table built now over stale data must not look fresh. The boot log carries the same counts on the '📊 expectancy' line, read from the table the process actually computed.",
    },
  ],
}
