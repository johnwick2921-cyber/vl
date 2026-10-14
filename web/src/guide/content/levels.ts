import { GUIDE_BUILT_REV, type GuideSection } from '../types'

export const levels: GuideSection = {
  id: 'levels',
  num: 4,
  title: 'The Level System',
  tagline: 'levels = WHERE · roles = WHAT-FOR · grades = HOW-STRONG.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    {
      kind: 'p',
      text: 'Every plan and every decision hangs on levels. A level is a price the machine detected (structure, volume, imbalance); a role says what to DO with it; and a grade is a letter — A, B or C — that gradeFromScore (kernel/levels_score.go) assigns by thresholding a score built from hand-set evidence weights, a freshness ladder, a confluence multiplier and a timeframe multiplier, then capped by the zone-timeframe and Tier-1 proximity rules. It is a SEATING PRIORITY label, not a probability: nothing in the scoring path reads an outcome table, and no grade has ever been calibrated against what those levels actually did.',
    },
    { kind: 'h', text: 'The kinds — Anchors / Volume / Zones' },
    {
      kind: 'cards',
      cards: [
        {
          title: 'PDH / PDL / PDC',
          body: "Prior-day high/low/close — the dealing-range anchors the bias-tree branches on. Why care: the tree's branches 1–3 are defined on them. Role: react zone. Grade: A (coverage-guarded — a truncated day is omitted, never fabricated).",
          tag: 'Anchors',
        },
        {
          title: 'ONH / ONL',
          body: 'Overnight (Asia+London composite) high/low. Why care: backtested 94.2% broken eventually (2,827-day NQ) — treat as liquidity, fade only on a confirmed sweep-reclaim. Role: liquidity/breakout.',
          tag: 'Anchors · 94.2%',
        },
        {
          title: 'RTH-H / RTH-L · AS-H/L · LDN-H/L',
          body: 'Session-scoped highs/lows. Why care: they delimit where each shift traded. Role: react zone.',
          tag: 'Anchors',
        },
        {
          title: 'OR-H / OR-L · IB-H / IB-L',
          body: "Opening-range (5m) and initial-balance (60m) extremes + extensions. Why care: the first-hour range defines the day's frame. Role: react zone.",
          tag: 'Anchors',
        },
        {
          title: 'PWH/PWL · PMH/PML',
          body: 'Prior week / prior month extremes. Why care: the HTF context the 1h/4h sections quote. Role: target_only for far-HTF continuation zones.',
          tag: 'Anchors',
        },
        {
          title: 'VWAP ±1σ · eVWAP',
          body: 'Session volume-weighted average + its band; eVWAP anchored at the last 15:00 CT cash close that traded (Friday\u2019s on a Sunday night or Monday — release #22; it used to anchor on an empty Sunday and copy session VWAP). Why care: the institutional magnet — mean-revert entries look here. Role: magnet/mean-revert.',
          tag: 'Volume',
        },
        {
          title: 'POC / VAH / VAL · nPOC · SETT · MID-O',
          body: 'Value-area profile (point of control, area high/low), naked POC, settlement, mid-of-overnight. The prior-day profile, pdVWAP and SETT come from the PRIOR CME SESSION that traded \u2014 Friday\u2019s on Monday (release #22: \u201cthe calendar day before\u201d was Saturday, so all three were missing every Monday). MID-O is the 17:00\u219208:30 overnight midpoint and now exists in the evening too (it used to vanish 17:00\u201323:59). Why care: where the market did business — magnets and targets. Tier-1 since R-A13.',
          tag: 'Volume',
        },
        {
          title: 'S/D · FVG · iFVG · OB',
          body: "Supply/demand zones, fair-value gaps (inverted when filled), order blocks. Why care: the playbook's entry surface (sweep → displacement → FVG retrace). FVG gap floor 2pt/8t; session-break guard on.",
          tag: 'Zones',
        },
        {
          title: 'EQH / EQL',
          body: "Equal highs/lows (3-tick tolerance) — resting liquidity. Why care: sweeps of equal highs are the A-setup's opening move.",
          tag: 'Zones',
        },
        {
          title: 'RN (round numbers)',
          body: '100/50/25-point steps generated inside the band. Why care: self-fulfilling pauses; the card labels them RN.',
          tag: 'Zones',
        },
      ],
    },
    { kind: 'h', text: 'Grading pipeline' },
    {
      kind: 'code',
      title: 'evidence × freshness × confluence × TF, with floors/caps',
      lines: [
        'zones: zoneEvidence(kind, TF, reversal×1.1) × zoneSizeMult(0.5–1.25)',
        '       × freshness × (1 + 0.20×confluence) × zoneTFMult(1.0/1.1/1.2/1.3)',
        'lines: typeEvidence(kind) × freshness × (1 + 0.20×conf) × htf(×1.2)',
        '',
        'freshness ladder (zones): 1.0 / 0.6 / 0.3 / 0.15',
        'confluence: distinct families only, cap 3 → ×1.6 max',
        '  the executor KEY LEVELS ·xN = that same distinct-family count',
        "  (R22: a merged cluster keeps its keeper's count — it used to SUM",
        "  its members' overlapping counts and showed up to ·x119)",
        '',
        'floors/caps: 1m zones forced C · 15m forced B (both ways) ·',
        '  1h/4h floor B (may reach A) · above-C only within 12 ticks',
        '  of a Tier-1 anchor (B2 gate)',
        '',
        '3 FVGs of the same family = 1 family entry (one seat, not three)',
      ],
    },
    { kind: 'h', text: 'Which timeframes the detectors run on' },
    {
      kind: 'p',
      text: 'Four detector families — equal highs/lows, supply/demand zones, fair-value gaps and order blocks — run with the SAME definition on each timeframe they are asked for, and only on timeframes inside the detection set: 15m, 30m, 1h, 2h, 4h, 6h, 8h, 12h, 1d, 3d and 1w. The set is a gate, not a schedule — nothing runs all eleven by default. The planner asks for its planner_timeframes (shipped default D, 4h, 1h, 15m; D is read as 1d); the executor’s KEY LEVELS pass, which has no list of its own, runs DefaultHTFDetectionTFs — 5 timeframes: 15m, 1h, 4h, 1d and 1w. 30m, 2h, 6h, 8h, 12h and 3d run only when a strategy lists them. Before 10 September the set stopped at 12h, so no daily or weekly swing, zone, order block or gap could reach a plan: of 297 stored plans, none carried one. The strategy config had been asking for daily structure the whole time — planner_timeframes reads ["D","4h","1h","15m","5m"] and names D first — but the detector only recognised the spelling "1d", so "D" was dropped without a word. Anything below 15m stays out on purpose: intraday noise adds nothing to higher-timeframe structure, and swing detection already covers 5m and 15m.',
    },
    {
      kind: 'code',
      title: 'what a timeframe contributes, and what it cannot',
      lines: [
        'detection set (the gate): 15m 30m 1h 2h 4h 6h 8h 12h 1d 3d 1w',
        'default set (KEY LEVELS pass): 15m 1h 4h 1d 1w',
        'planner: its own planner_timeframes, inside the gate',
        'detectors per timeframe: 4 (same definition on each)',
        '',
        'a level now carries THREE separate facts:',
        '  formation timeframe — which tf it formed on',
        '  lookback window     — how many bars were searched there',
        '  age at read         — how long ago it formed',
        'none of the three can be derived from the others: 500 weekly',
        'bars and 500 quarter-hour bars are the same lookback in bars',
        'and nine years apart in time.',
        '',
        'same price on two timeframes = ONE candidate, BOTH names',
        '  "29657.38 — Supply·1h · EQH·1d"   (one credit, not two)',
        'same price on the SAME timeframe = one level (dedupe)',
        '',
        'a timeframe with too few bars emits NOTHING and says why —',
        'never a level built from a partial window.',
      ],
    },
    {
      kind: 'p',
      text: 'Daily and weekly levels are graded as if they were 4h. That is a CLASSIFICATION, not a measurement: the alternative was to let them fall through to the 1-minute noise floor, where a daily zone would score with the weakest evidence on the board and be capped at grade C. Nothing here establishes that a daily level is stronger than a 1h one. The research round that governs this wave found the ×1.2 higher-timeframe weight has no tested foundation at all, and it is carried unchanged and marked untested. Whether the daily family deserves the 4h tier, its own, or none is a question for measurement, not for the person who wired it.',
    },
    { kind: 'h', text: 'The 5 roles — what a level is FOR' },
    {
      kind: 'cards',
      cards: [
        {
          title: 'MAGNET / MEAN-REVERT',
          body: 'VWAP family, POC. Invited play: fade extremes back to it, target it. Forbidden play: breakout through it without a sweep story.',
          tag: 'ROLE',
        },
        {
          title: 'LIQUIDITY / BREAKOUT',
          body: 'ONH/ONL, EQH/EQL. Invited play: wait for the sweep, then the reclaim — never fade the first poke. The ONH story: fade-the-poke was the −131 week; sweep-reclaim is the house rule.',
          tag: 'ROLE',
        },
        {
          title: 'REACT ZONE',
          body: 'PDH/PDL, session highs/lows, OR/IB. Invited play: first-touch reactions with confirmation. Forbidden: chasing a close beyond without a retest.',
          tag: 'ROLE',
        },
        {
          title: 'TARGET ONLY',
          body: 'Far-HTF continuation zones. Invited play: take profit / trail. Forbidden: entry against the HTF trend at these.',
          tag: 'ROLE',
        },
        {
          title: 'PIVOT',
          body: "The bias-flip reference (env LEVEL_ROLE_MAP). The plan's flip line anchors here.",
          tag: 'ROLE',
        },
      ],
    },
    { kind: 'h', text: 'Freshness by own timeframe' },
    {
      kind: 'p',
      text: "Freshness by own timeframe (levels_fresh_by_tf) — FOLDED by W-KNOB-PRUNE (2026-09-18, S4c: freshness separates nothing): no Studio control; OFF (the 1m-touch ladder) unless the strategy already stores ON, which the engine honours and logs at load (the owner's MNQ strategy does). ON = HTF levels grade fresh / tested-1 / tested-2 / stale on their own timeframe bars. The scoring tables never change in either mode; only the freshness string HTF levels carry differs.",
    },
    { kind: 'h', text: 'Seats & band' },
    {
      kind: 'p',
      text: 'Why 8 (max_levels — the code default, kernel.DefaultMaxLevels; both bound strategies save 12 today, see Settings → Max levels): a tight table the AI can actually copy without hallucinating. The ±band (proximity_filter_atr × daily-range, retuned 0.3 → ≈±100pt): day-trade relevance — far levels still exist in the detector universe (they now feed the bias-tree anchors even unseated). Per-side counts are DELETED (owner ruling 2026-08-31): the old min_side_levels knob is gone and the ⚖ thin-side note is gone — the only side guard left is the 0-levels-on-a-side fail (the 2026-08-18 one-sided-map pathology).',
    },
    { kind: 'h', text: 'WHERE THE BARS COME FROM (class 45)' },
    {
      kind: 'p',
      text: 'Every chart, level and indicator is built from bars, and until 2026-09-02 they came from two places that did not know about each other. NinjaTrader streams a deep history into memory — over seven years of weekly bars, five years of daily — but only one-minute bars were ever written to disk, so the rest vanished on every restart. The weekly panel built its weeks from those one-minute bars, which start on 19 August, found two complete weeks against a minimum of four, and showed "thin". It was starved beside a full pantry. Now one resolver answers every request for completed bars, preferring NinjaTrader\'s own series and falling back through coarser-to-finer sources, and every timeframe is written to disk so nothing is lost on restart. One caution worth knowing: NinjaTrader\'s weekly bars run Friday to Thursday, while every week in this system runs Monday to Friday. Those bars are stored for research and deliberately never used for the weekly view, which is built from daily bars instead so the weeks line up with the ones you see. The weekly bias signal itself is unchanged by this — only the data feeding it.',
    },
  ],
}
