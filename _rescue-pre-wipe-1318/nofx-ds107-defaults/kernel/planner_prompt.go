package kernel

import (
	"fmt"
	"strings"
	"time"

	"nofx/market"
	"nofx/store"
)

// P3.3 — the planner input package (assembled into ONE prompt for the reasoner).
// Pure + deterministic. The 16:55 closed-market read builds entirely from stored
// data (no live feed), which is what makes it a first-class tested path.

// PlannerCalendarEvent is a session-sliced calendar entry (kernel-local to avoid
// importing the calendar package; the trader maps calendar.Event → this).
type PlannerCalendarEvent struct {
	TimeCT   string // "HH:MM" CT
	Currency string
	Title    string
	Impact   string // T1 | T2
}

// PlannerInput is everything the reasoner reads to write a plan.
type PlannerInput struct {
	// T1Currencies (W-T1-CURRENCIES, 2026-09-18) — the RESOLVED currency set
	// whose T1 events the machine hard-blocks (store.DayPlanConfig.
	// T1CurrenciesFor). Empty → the shipped default; the Calendar section tags
	// every other T1 event as advisory so the model is never told a blackout
	// the gate will not enforce.
	T1Currencies []string
	// WriteFeasibilityOn (W-WRITE-TIME-FEASIBILITY, 2026-09-18) — true when the
	// write-time feasibility knob is ON (the owner's default): the prompt
	// renders the arm-disabled-at-write rule. false → the sentence is absent and
	// the prompt is byte-identical to before the wave.
	WriteFeasibilityOn bool
	// EntryPolicyDefault / ZoneMaxPts / MinHoldMin (W-EXEC-TRUTH W3, 2026-09-23)
	// — the RESOLVED day_plan.entry_policy_default, zone_max_pts and
	// min_hold_min. The ZERO value renders the SHIPPED default (market_in_zone,
	// 10 pt, 3 min) — what production renders for a strategy that saved
	// nothing; "legacy" renders the pre-W3 prompt byte-identically.
	EntryPolicyDefault string
	ZoneMaxPts         float64
	MinHoldMin         int
	Zones              *LevelZoneMap // Uncut presentation snapshot; never used as trading inputs.
	ResearchSnapshotID string        `json:"-"` // record link, never prompt content
	TradeDate          string
	Session            string    // NY | ASIA | LONDON
	Now                time.Time // labelled CT clock line (zero → omitted)
	ReadKind           string    // e.g. "closed-market 16:55 CT read (from stored data)"
	Price              float64
	DATR               float64
	// ATR5m is the 5-minute Wilder ATR14 — the SAME series the confirm and stop
	// paths use (StaleConfirmATR5m). W3 renders map distances in it. Zero means
	// it could not be computed, and the map prints n/a rather than a 0 (A24).
	ATR5m float64
	// GeometryRefIDs (W-GEOMETRY-REFUSAL, 2026-09-18) — the resolved
	// day_plan.geometry_reference_levels knob: true = reference-anchor levels
	// with an unknown formation close carry a stable id (never NULL).
	GeometryRefIDs   bool
	Regime           RegimeBlock
	Levels           []ScoredLevel // Go-ranked, graded (P1.5) — the decision-critical block
	StructureSummary []string      // one line per timeframe
	// Structure (S1, 2026-09-16) — the STRUCTURE table; nil = knob off =
	// nothing rendered (byte-identical prompt).
	Structure *StructureMap
	// G2.2 (2026-08-24) — nearest in-band HTF zones (S/D/FVG/OB), graded, for a
	// dedicated prompt section. They exist in the data but lose the top-8 seat
	// race to structural levels (cluster collapse + seat priority), so the model
	// never saw them before. Advisory confluence — never standalone triggers.
	HTFZones []ScoredLevel
	// HTFZonesFull (S1-wave A3, 2026-08-29) — the FULL in-band graded HTF-zone
	// universe (uncapped). The prompt renders only the cap-4 HTFZones section,
	// but the model also reads the whole key-levels block and may write zones
	// the cap hid — the write-site stamp records this list so those rows carry
	// their machine grade (the 13 Demand·1h escapes).
	HTFZonesFull []ScoredLevel
	// G5 (regime wave 2026-08-21) — levels already CONSUMED at read time (role-
	// flipped), listed so the planner works around them. Advisory.
	ConsumedLevels []string

	// CLASS 45 (2026-09-02) — the feed-forward inputs. VoidBreakdownLevels is
	// the validator's OWN verdict per level (ComputeVoidBreakdownLevels →
	// BreakdownContinueState); StopFloor* are the composer's resolved floor.
	// Both empty/zero → the sections render nothing.
	VoidBreakdownLevels []VoidBreakdownLevel
	StopFloorATR5m      float64
	// LevelDisplacements (owner ruling 2026-09-03) — the MEASURED displacement
	// of every seated level, from the validator's own BreakdownContinueState
	// through ComputeLevelDisplacements. Empty → the blocks render nothing, so
	// a cold cache degrades to the previous behaviour.
	LevelDisplacements []LevelDisplacement
	StopFloorMult      float64

	// FreshFVGs (level-truth wave b2, 2026-08-27) — the machine's fresh-gap
	// candidate list the planner may author fvg_entry from. Empty list means
	// NO fvg_entry may be authored (the write-site validator refuses anything
	// else).
	FreshFVGs []FreshFvg

	// Pool (level-truth wave, 2026-08-27) — the graded PRE-SEAT candidate pool
	// the assembly produced. NOT rendered: the write site uses it for the
	// machine-grade stamp map so pool levels that lost the seat race still get
	// stamped when the model copies them into the plan.
	Pool           []ScoredLevel
	OvernightStory string
	PriorDayStory  string
	Calendar       []PlannerCalendarEvent // session-sliced (P1.8)
	DigestChain    []string               // session digests + dailies + one-liners
	OwnerNote      string
	Warming        string // non-empty → cold-start / WARMING annotation
	// FastTape (F3 fast-market wake reads, 2026-08-28) — the read fires while
	// the tape is moving fast; the prompt carries the note and the wire runs
	// reasoning=medium (FAST_MARKET_REASONING).
	FastTape     bool
	FastTapeNote string
	// PriorPlanKiller (P0.4-G, 2026-08-25) — when this read re-plans a DEAD
	// plan, the killer line (e.g. "flip-condition: 2x5m close above X → bias
	// long"). Rendered as a MANDATORY context block: a flip that already fired
	// on machine-evaluated bars must be honored in the new plan's bias, not
	// silently re-written (live bug: ASIA v3 flip fired → bias long, but v4
	// came back short-biased). Empty → no block (first reads / owner resets).
	PriorPlanKiller string
	// PriorPlanLevels (P0.4-G) — the PREVIOUS version's levels, carried into
	// every re-plan so the map keeps continuity instead of being rebuilt from
	// scratch each time (the owner's complaint: every re-run dropped all old
	// levels and the flip anchor kept moving). Rendered as a carry-over block;
	// the model keeps or re-anchors them, never silently loses the set.
	PriorPlanLevels []string
	// W11 — the executor's indicator mirror (per-TF EMA/RSI/ATR/BOLL/MACD, driven by
	// ai_config toggles), rendered once by RenderPlannerIndicatorBlock. Empty → the
	// block is omitted (disabled state = byte-identical prompt). AIConfigHash is the
	// fingerprint of the indicator config that produced it (frozen on the plan row).
	IndicatorsBlock string
	AIConfigHash    string
	// H4/H5 — the RESOLVED caps the planner is asked to write within (max_levels,
	// scenario_cap). 0 → shipped defaults, so default callers render the same
	// "max 8 / 1..3" contract as before.
	MaxLevels   int
	ScenarioCap int
	// BiasCtx (addendum 2, 2026-08-26) — the per-cycle bias-context facts line
	// (price vs VWAP/PDC, value area, nearest magnet/liquidity). Facts only.
	BiasCtx string
	// BiasCtxFacts (A1, planner-contract wave 2026-08-26) — the STRUCTURED bias
	// context the BIAS-TREE section computes from (price, PDH/PDL/PDC, value
	// area, nearest liquidity). Filled by the same site as BiasCtx.
	BiasCtxFacts BiasContext
	// CandleTables (W2b, weekly-bias wave 2026-08-30) — the rendered
	// "## Candles (oldest→latest)" block (12×15m · 12×1h · 8×4h · 8×daily),
	// built by kernel.FormatCandleTable from the 1m slice via
	// kernel.AggregateBars. Empty → the section is omitted (knob off / no
	// bars). Candles are GROUND TRUTH for structure.
	CandleTables string
	// WeeklyCtx (W3, weekly-bias wave 2026-08-30) — the ≤3-line
	// "## Weekly Context" block from the Sunday weekly read
	// (kernel.WeeklyContextLine). "WEEKLY: none" renders when no doc exists —
	// fail-open: a missing doc changes nothing else.
	WeeklyCtx string
}

// RenderBiasTree (A1, planner-contract wave 2026-08-26) — the machine-computed
// bias decision tree. Facts only + the branch the facts CURRENTLY match; the
// planner states which branch it took (contract requirement). Premium/discount
// = position within the value-area range; the draw = nearest opposing
// liquidity pool (the runner target).
func RenderBiasTree(price float64, levels []ScoredLevel, bc BiasContext) string {
	// S-dispatch (2026-08-27) — the structured bias facts win: post-roll the
	// seated table can drop PDH/PDL (out of the proximity band) and the tree
	// rendered "no PDH/PDL anchor". Fall back to the seated scan for legacy
	// callers/tests that only pass levels.
	pdh, pdl, pdc := bc.PDH, bc.PDL, bc.PDC
	for _, l := range levels {
		switch l.Kind {
		case KindPDH:
			if pdh <= 0 {
				pdh = l.Price
			}
		case KindPDL:
			if pdl <= 0 {
				pdl = l.Price
			}
		case KindPDC:
			if pdc <= 0 {
				pdc = l.Price
			}
		}
	}
	var b strings.Builder
	b.WriteString("## BIAS-TREE (machine branches — your reasoning MUST state the branch you took, e.g. \"bias-tree: inside-day long LOW\")\n")
	b.WriteString("  1. close > PDH → bull-continuation, conviction HIGH\n")
	b.WriteString("  2. sweep of PDH + close back inside → bear, conviction MEDIUM  (mirror: sweep PDL + close inside → bull MEDIUM)\n")
	b.WriteString("  3. inside the day (between PDH/PDL) → direction of close vs PDC (prior close), conviction LOW\n")
	b.WriteString("  4. closed OUTSIDE the prior day's range but now inside → NO bias (write neutral; trade the structure, not a thesis)\n")
	b.WriteString("  5. premium/discount: longs ONLY below the 50% mark of the dealing range, shorts ONLY above it\n")
	b.WriteString("  6. draw-on-liquidity: the runner target is the DRAW — the nearest opposing liquidity pool beyond the first target\n")
	fmt.Fprintf(&b, "  computed: PDH %.2f · PDL %.2f · PDC %.2f", pdh, pdl, pdc)
	// S-dispatch (2026-08-27) — the branch-5 premium/discount anchor is the
	// DEALING RANGE (prior-day swing hi/lo = PDH/PDL), not the value area. The
	// VA can be a few points wide (the 17:46 read printed "376% of range" off
	// a ~30pt VA); a beyond-range price now renders "beyond range (extended)"
	// instead of a >100% percentage. Value area stays as the fallback when
	// the day anchors are unknown.
	lo, hi := pdl, pdh
	if lo <= 0 || hi <= 0 || hi <= lo {
		lo, hi = bc.VAL, bc.VAH
		if lo > hi {
			lo, hi = hi, lo
		}
	}
	if hi > lo && price > 0 {
		fmt.Fprintf(&b, " · dealing range %.2f–%.2f", lo, hi)
		pos := (price - lo) / (hi - lo)
		switch {
		case pos > 1:
			b.WriteString(" · price BEYOND range high (extended) — longs disallowed by branch 5 (premium)")
		case pos < 0:
			b.WriteString(" · price BEYOND range low (extended) — shorts disallowed by branch 5 (discount)")
		case pos >= 0.5:
			fmt.Fprintf(&b, " · price at %.0f%% of the range (PREMIUM — longs disallowed by branch 5)", pos*100)
		default:
			fmt.Fprintf(&b, " · price at %.0f%% of the range (DISCOUNT — shorts disallowed by branch 5)", pos*100)
		}
	}
	if price > 0 && pdh > 0 && price > pdh {
		b.WriteString(" · facts match branch 1 (close > PDH)")
	} else if price > 0 && pdl > 0 && price < pdl {
		b.WriteString(" · facts match branch 1 mirror (close < PDL)")
	} else if price > 0 && pdh > 0 && pdl > 0 && pdc > 0 && price < pdh && price > pdl {
		dir := "flat"
		if price > pdc {
			dir = "long"
		} else if price < pdc {
			dir = "short"
		}
		fmt.Fprintf(&b, " · facts match branch 3 (inside day; close %s PDC → %s LOW)", dir, dir)
	}
	if bc.NearestLiquidity != "" {
		b.WriteString(" · draw/liquidity: " + bc.NearestLiquidity)
	}
	b.WriteString("\n\n")
	return b.String()
}

// ── CLASS 50b (live-bias replay ruling 53498adb, 2026-09-02) ────────────────
// The plan bias is a LABEL, not a direction: the AI-authored direction, the
// machine bias-tree call and the regime call are surfaced on one line so no
// single source reads as truth. These three helpers are the one shape; no MUST
// attaches to either leg anywhere.

// TreeCallWord reduces the documented bias-tree branches (RenderBiasTree) to
// one word for the label line: branch 1 → long, branch 1 mirror → short,
// branch 3 → close vs PDC, branch 5 premium/discount veto → neutral. Mirrors
// the reconstruction in the live-bias replay P2 (53498adb).
func TreeCallWord(price, pdh, pdl, pdc float64) string {
	if price <= 0 || pdh <= 0 || pdl <= 0 || pdc <= 0 {
		return "neutral"
	}
	if price > pdh {
		return "long"
	}
	if price < pdl {
		return "short"
	}
	if pdh > pdl {
		pos := (price - pdl) / (pdh - pdl)
		call := "neutral"
		switch {
		case price > pdc:
			call = "long"
		case price < pdc:
			call = "short"
		}
		// branch 5: premium (≥50% of the dealing range) disallows longs,
		// discount disallows shorts.
		if pos >= 0.5 && call == "long" {
			return "neutral"
		}
		if pos < 0.5 && call == "short" {
			return "neutral"
		}
		return call
	}
	return "neutral"
}

// RegimeCallWord reduces the regime block to one word for the label line:
// up iff both D and 1h trend up, down iff both down, else neutral (the
// live-bias replay P2 definition).
func RegimeCallWord(r RegimeBlock) string {
	if strings.EqualFold(strings.TrimSpace(r.TrendDaily), "up") &&
		strings.EqualFold(strings.TrimSpace(r.Trend1h), "up") {
		return "up"
	}
	if strings.EqualFold(strings.TrimSpace(r.TrendDaily), "down") &&
		strings.EqualFold(strings.TrimSpace(r.Trend1h), "down") {
		return "down"
	}
	return "neutral"
}

// BiasLabelLine renders the dual-label line: "bias: AI <x> · tree <y> · regime <z>".
// Lowercased, trimmed — the one shape everywhere (prompt + stamped doc + card).
func BiasLabelLine(aiBias, treeCall, regimeCall string) string {
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			return "neutral"
		}
		return s
	}
	return fmt.Sprintf("bias: AI %s · tree %s · regime %s", norm(aiBias), norm(treeCall), norm(regimeCall))
}

// FantasyTargetWarnings (autopsy-response wave, 2026-08-27) — advisory WARN,
// never a fail: an armed scenario whose PLANNED R:R exceeds 6 is a
// fantasy-target flag (the 3.28–22.88 R losers in the refusal autopsy).
func FantasyTargetWarnings(doc PlanDoc) []string {
	var out []string
	for _, s := range doc.Scenarios {
		a := s.Arm
		if a == nil || !a.Enabled || a.Entry <= 0 || a.Stop <= 0 || a.Target <= 0 {
			continue
		}
		risk := a.Entry - a.Stop
		reward := a.Target - a.Entry
		if strings.EqualFold(strings.TrimSpace(s.Direction), "short") {
			risk = a.Stop - a.Entry
			reward = a.Entry - a.Target
		}
		if risk > 0 && reward/risk > 6.0 {
			out = append(out, fmt.Sprintf("%s: planned R %.1f (entry %.2f stop %.2f target %.2f) — fantasy-target flag", s.ID, reward/risk, a.Entry, a.Stop, a.Target))
		}
	}
	return out
}

// FvgDemandWarnings (grand-audit response F5, 2026-08-28) — WARN-only demand
// compliance: fresh machine FVGs existed AND at least one agreed with the plan
// bias AND the plan authored NO fvg_entry scenario → the plan owes a one-line
// reason. The reason is model prose (not reliably parseable), so this is a
// visibility WARN at write, never a fail.
func FvgDemandWarnings(doc PlanDoc, fresh []FreshFvg) []string {
	if len(fresh) == 0 {
		return nil
	}
	authored := false
	for _, s := range doc.Scenarios {
		if strings.EqualFold(strings.TrimSpace(s.Condition), "fvg_entry") {
			authored = true
			break
		}
	}
	if authored {
		return nil
	}
	bias := strings.ToLower(strings.TrimSpace(doc.Bias.Direction))
	match := 0
	for _, g := range fresh {
		if bias == "" || bias == "neutral" || strings.EqualFold(g.Direction, bias) {
			match++
		}
	}
	if match == 0 {
		return nil
	}
	return []string{fmt.Sprintf(
		"%d fresh FVG candidate(s) agree with the %q bias but no fvg_entry was authored — the plan must state why not (one line in reasoning)",
		match, bias)}
}

// ChainWarnings (A2, planner-contract wave) — validator WARN, never a fail:
// an fvg_entry scenario without a chain_after sweep precursor and whose origin
// level is not a fresh A/B zone is the bare-gap setup the research's raw-FVG
// null result warns about (no edge without the sweep → displacement chain).
func ChainWarnings(doc PlanDoc) []string {
	var out []string
	gradeOf := func(label string) string {
		for _, l := range doc.Levels {
			if strings.EqualFold(strings.TrimSpace(l.Label), strings.TrimSpace(label)) {
				return l.Grade
			}
		}
		return ""
	}
	byID := map[string]PlanScenario{}
	for _, s := range doc.Scenarios {
		byID[strings.ToUpper(strings.TrimSpace(s.ID))] = s
	}
	for _, s := range doc.Scenarios {
		if !strings.EqualFold(strings.TrimSpace(s.Condition), "fvg_entry") || s.Fvg == nil {
			continue
		}
		hasPrecursor := false
		if ref := strings.ToUpper(strings.TrimSpace(s.ChainAfter)); ref != "" {
			if p, ok := byID[ref]; ok && strings.EqualFold(strings.TrimSpace(p.Condition), "sweep_reclaim") {
				hasPrecursor = true
			}
		}
		originGrade := strings.ToUpper(gradeOf(s.Fvg.OriginLevel))
		if !hasPrecursor && originGrade != "A" && originGrade != "B" {
			out = append(out, fmt.Sprintf("fvg_entry %s has no sweep_reclaim precursor (chain_after) and origin %q is not a fresh A/B zone — the bare-gap setup has no standalone edge (raw-FVG null, 40k sample); consider chaining it after a sweep play", s.ID, s.Fvg.OriginLevel))
		}
	}
	return out
}

// BuildPlannerCandleTablesAt (W2b, weekly-bias wave 2026-08-30) renders the
// "## Candles" block from the 1m slice: last 12×15m · 12×1h · 8×4h ·
// 8×daily rows — the SAME aggregation helpers the planner already uses
// (kernel.AggregateBars on the 1m slice; daily = session-day candles).
// Empty input → "".
func BuildPlannerCandleTablesAt(bars1m []market.Kline, asked1m int, now time.Time) string {
	if len(bars1m) == 0 {
		return ""
	}
	var b strings.Builder

	// D1 (BARS HORIZON 2026-09-09) — the block declares the tape it is built
	// from BEFORE any table, because the prompt disclosed its depth nowhere.
	b.WriteString(candleTapeLine(HorizonOf(bars1m, "1m", asked1m, now)) + "\n")
	b.WriteString(candleMarkerLegend + "\n\n")

	// TAIL-TRIM FIRST, THEN MEASURE. The trim used to run after coverage was
	// computed for every row of the full aggregate — ~1,170 calendar walks per
	// planner read on the store-deepened 12,000-bar tape, ~99% of them for rows
	// that were then discarded. Only the rows that will be PRINTED are measured
	// (found in review, 2026-09-09). rows and every parallel slice are trimmed
	// together, so a marker can never land on the wrong row.
	tail := func(rows []market.Kline, n int) []market.Kline {
		if len(rows) > n {
			return rows[len(rows)-n:]
		}
		return rows
	}
	render := func(label, unit string, bars []market.Kline, cov []RowCoverage, absent []int, n int) {
		notes := make([]string, len(cov))
		for i, c := range cov {
			notes[i] = c.Marker()
			if i > 0 {
				if a := absentRowNote(absent[i], unit, bars[i-1].OpenTime, bars[i].OpenTime); a != "" {
					notes[i] = a + notes[i]
				}
			}
		}
		b.WriteString(tableHeading(label, len(bars), n, cov, absent) + "\n")
		FormatCandleTableNoted(&b, KlineBars(bars), true, notes)
	}

	agg := func(label string, bucketMs int64, n int) {
		rows := tail(AggregateBars(bars1m, bucketMs), n)
		render(label, label, rows, aggregateCoverage(bars1m, rows, bucketMs, now), absentAggregateRows(rows, bucketMs), n)
	}
	agg("15m", 15*60*1000, 12)
	agg("1h", 60*60*1000, 12)
	agg("4h", 240*60*1000, 8)

	daily := tail(DailySessionBars(bars1m), 8)
	render("daily session candles", "session-day", daily, sessionCoverage(bars1m, daily, now), absentSessionRows(daily), 8)
	return b.String()
}

// BuildPlannerPrompt assembles the planner prompt: reasoning-first instruction,
// the input tables, and the schema-strict JSON contract. High-salience blocks
// (ranked levels, T1 blackouts) are positioned prominently per the contract.
func BuildPlannerPrompt(in PlannerInput) string {
	var b strings.Builder

	b.WriteString("# DAY-PLAN READER — CME MNQ futures\n")
	b.WriteString("You are a disciplined pro reading the market ONCE, carefully. Write ONE structured day plan for this session. Facts below are Go-computed; your job is JUDGMENT, not re-deriving the map.\n\n")

	fmt.Fprintf(&b, "## Session\ntrade_date %s · session %s · %s · price %.2f · dATR %.1f\n",
		in.TradeDate, in.Session, in.ReadKind, in.Price, in.DATR)
	if !in.Now.IsZero() {
		cls, cle := LunchWindowCT() // a FOURTH copy of the window lived here
		fmt.Fprintf(&b, "clock %s — EVERY time in this prompt is CT (America/Chicago): session windows, read/flat times and the lunch no-trade (%s–%s CT) are CT wall-clock. Never apply these numbers to a UTC clock.\n",
			ClockCTAndUTC(in.Now), cls, cle)
	}
	if in.Warming != "" {
		fmt.Fprintf(&b, "WARMING: %s (first-week honesty — narrate the machinery, not an edge).\n", in.Warming)
	}
	if in.FastTape {
		fmt.Fprintf(&b, "⚡ FAST TAPE — %s. Write the SHORTEST valid plan you can: fewer scenarios, tight prose. A fast market does not reward long deliberation.\n", in.FastTapeNote)
	}
	b.WriteString("\n")

	b.WriteString("## Regime\n" + in.Regime.Render() + "\n\n")

	// W2b (weekly-bias wave 2026-08-30) — the planner gets EYES: the raw
	// candle tables. Ground truth for structure; ranked levels and tags are
	// summaries. On conflict, trust the candles and say so in the scenario
	// rationale. Omitted when the knob is off / no bars (fail-open).
	if strings.TrimSpace(in.CandleTables) != "" {
		b.WriteString("## Candles (oldest→latest)\n")
		b.WriteString(in.CandleTables)
		b.WriteString("\n")
	}

	// W3 (weekly-bias wave 2026-08-30) — the Sunday weekly-bias context line.
	// Rendered ALWAYS (missing doc → "WEEKLY: none"): fail-open, a missing doc
	// changes nothing else. Soft law — informational, never a gate.
	b.WriteString("## Weekly Context\n")
	if strings.TrimSpace(in.WeeklyCtx) == "" {
		b.WriteString("WEEKLY: none\n")
	} else {
		b.WriteString(in.WeeklyCtx + "\n")
	}
	b.WriteString("\n")

	// W11 — INDICATORS mirror: the SAME per-timeframe indicator state the executor
	// sees, so the planner reasons on the exact indicators the trader trades on.
	// Omitted when no indicator is enabled (disabled state = pre-W11 prompt).
	if in.IndicatorsBlock != "" {
		b.WriteString("## Indicators (executor mirror — your ai_config toggles, computed values)\n")
		b.WriteString(in.IndicatorsBlock + "\n\n")
	}

	// S1 (2026-09-16) — the STRUCTURE table, knob-gated (nil → nothing):
	// direction only, before the entry table it must never be confused with.
	b.WriteString(RenderStructureSection(in.Structure))

	// Ranked level table — the decision-critical block, high-salience.
	// CLASS 45 E2/E3 — feed forward what the validator/composer already know:
	// which breakdown levels are VOID, and the stop floor the executor enforces.
	// Both are computed from the SAME code the enforcer runs, so the prompt can
	// never hold a second opinion. Empty inputs render nothing.
	b.WriteString(RenderVoidBreakdownLevels(in.VoidBreakdownLevels, len(in.Levels)))
	b.WriteString(RenderStopFloorLine(in.StopFloorATR5m, in.StopFloorMult))
	// The waterfall floor, stated the same way the stop floor is: the number
	// the validator judges by, and which levels can actually meet it.
	b.WriteString(RenderDisplacementFloorLine(in.StopFloorATR5m))
	b.WriteString(RenderDisplacementLines(in.LevelDisplacements, in.StopFloorATR5m))

	b.WriteString("## Ranked levels (Go-graded; you never re-sort)\n")

	// G5 (regime wave 2026-08-21) — consumed levels listed explicitly: the
	// planner must plan AROUND them (a re-test is a NEW setup, never a fresh
	// tag). Advisory — Go facts, AI judgment.
	if len(in.ConsumedLevels) > 0 {
		b.WriteString("## Consumed levels (already role-flipped — plan AROUND them; a re-test is a NEW setup)\n")
		for _, s := range in.ConsumedLevels {
			b.WriteString("- " + s + "\n")
		}
		b.WriteString("\n")
	}

	// Level-truth wave b2 (2026-08-27) — the machine's fresh-gap candidates.
	// The write-site validator re-checks every declared fvg{} against exactly
	// this list; the model must author ONLY from it (it used to invent stale
	// gaps and every read failed closed).
	b.WriteString("## FRESH FVGs (machine-computed candidates — author fvg_entry ONLY from this list; if empty, do NOT author any fvg_entry; if NON-empty and a candidate's direction agrees with your bias, an fvg_entry is EXPECTED unless you state why not in one line)\n")
	if len(in.FreshFVGs) == 0 {
		b.WriteString("(none fresh right now)\n\n")
	} else {
		for _, g := range in.FreshFVGs {
			b.WriteString(fmt.Sprintf("  %s %.2f–%.2f (age %d bars, displacement %.2f×ATR5m)\n",
				g.Direction, g.Lo, g.Hi, g.AgeBars, g.DispATR))
		}
		b.WriteString("\n")
	}
	if len(in.Levels) == 0 {
		b.WriteString("(none in range — warming forward)\n")
	} else {
		for _, l := range in.Levels {
			sign := "+"
			if l.Distance < 0 {
				sign = "-"
			}
			label := l.Label
			if isHTFSwingZone(l) {
				label = label + " (HTF)"
			}
			role := string(l.Role)
			if role == "" {
				role = string(RoleReactZone)
			}
			fmt.Fprintf(&b, "  %-9.2f %-20s grade %s  %-8s %-15s %s%.1f\n", l.Price, label, l.Grade, l.Fresh, role, sign, absF(l.Distance))
		}
		// W3 (2026-09-09) — the MERGED map the owner's card shows: overlapping
		// references as ONE candidate carrying all its names, the map role, the
		// distance in ATR5m, projections beyond the mapped range, and the entry
		// shortlist in reachability order. Rendered BELOW the ranked table, which
		// is left exactly as the scorer produced it (the score is untouched).
		candidates := BuildMapCandidates(in.Levels, in.Price, in.ATR5m, MapCandidateOpts{})
		// W-GEOMETRY-REFUSAL (2026-09-18) — with day_plan.geometry_reference_levels
		// ON (the owner's default), reference-anchor levels whose formation close
		// is unknown get a STABLE sha id instead of NULL, so the planner can author
		// ONH/ONL reject plays the executor can resolve. OFF → byte-identical map.
		if in.GeometryRefIDs {
			EnsureReferenceLevelIDs(candidates)
		}
		mb := RenderIdentityMapBlock(candidates, in.Price)
		if in.Zones != nil {
			mb = RenderScoredReferenceBlock(candidates, in.Price)
		}
		if mb != "" {
			b.WriteString("\n")
			b.WriteString(mb)
		}
	}
	b.WriteString("\n")

	// ADDENDUM (1) — the role playbook (machine facts; your judgment stays).
	if in.Zones != nil {
		b.WriteString(in.Zones.Render())
		b.WriteString("\n")
	}
	b.WriteString("## Level roles (machine-assigned, 5-line playbook)\n")
	b.WriteString(RoleLegend + "\n")

	// ADDENDUM (2) — bias-context: facts only, AI judges.
	if in.BiasCtx != "" {
		b.WriteString(in.BiasCtx + "\n\n")
	}

	if len(in.StructureSummary) > 0 {
		b.WriteString("## Structure (1 line/TF)\n")
		for _, s := range in.StructureSummary {
			b.WriteString("  " + s + "\n")
		}
		b.WriteString("\n")
	}

	// G2.2 (2026-08-24) — HTF zones as their own section: the top-8 seat race
	// hides them from the ranked table, but a 1h/4h base is exactly the
	// large-account reference the plan should know about.
	if len(in.HTFZones) > 0 {
		b.WriteString("## HTF zones (nearest first — confluence references, NEVER standalone triggers)\n")
		for _, z := range in.HTFZones {
			sign := "+"
			if z.Distance < 0 {
				sign = "-"
			}
			fmt.Fprintf(&b, "  %-9.2f %-14s grade %s  %s%.1f (HTF zone)\n", z.Price, z.Label, z.Grade, sign, absF(z.Distance))
		}
		b.WriteString("\n")
	}

	if in.OvernightStory != "" || in.PriorDayStory != "" {
		b.WriteString("## Auction story\n")
		if in.PriorDayStory != "" {
			b.WriteString("  prior day: " + in.PriorDayStory + "\n")
		}
		if in.OvernightStory != "" {
			b.WriteString("  overnight: " + in.OvernightStory + "\n")
		}
		b.WriteString("\n")
	}

	// Session-sliced calendar: T1 = HARD blackout, T2 = caution. Times CT.
	b.WriteString("## Calendar (this session's window — times CT)\n")
	if len(in.Calendar) == 0 {
		b.WriteString("  (no filtered events)\n")
	} else {
		t1Set := in.T1Currencies
		if len(t1Set) == 0 {
			t1Set = store.DefaultT1Currencies()
		}
		for _, e := range in.Calendar {
			tag := "caution — NOT a no-trade blackout; keep trading with normal discretion"
			if e.Impact == "T1" {
				// The dictated no_trade instruction (2026-09-03) says the
				// machine enforces T1 blackouts regardless and the author must
				// not list them. This tag used to order the opposite.
				tag = "HARD no-trade blackout — the machine writes and enforces it; stand aside around it"
				// W-T1-CURRENCIES: a red event outside the hard set is
				// ADVISORY — the gate will not refuse entries around it, so
				// the prompt must not claim it will.
				if hard, _ := t1CurrencyHard(e.Currency, t1Set); !hard {
					tag = fmt.Sprintf("red news, ADVISORY only — NOT a machine blackout (t1_currencies=%s); trade with discretion", T1CurrencySetLabel(t1Set))
				}
			}
			fmt.Fprintf(&b, "  %s %s %s — %s (%s)\n", e.TimeCT, e.Currency, e.Impact, e.Title, tag)
		}
	}
	b.WriteString("\n")

	if len(in.DigestChain) > 0 {
		b.WriteString("## Recent context (digest chain)\n")
		for _, d := range in.DigestChain {
			b.WriteString("  " + d + "\n")
		}
		b.WriteString("\n")
	}
	if strings.TrimSpace(in.OwnerNote) != "" {
		b.WriteString("## Owner note\n  " + in.OwnerNote + "\n\n")
	}
	if strings.TrimSpace(in.PriorPlanKiller) != "" {
		b.WriteString("## Prior plan invalidation (MANDATORY context)\n  " + in.PriorPlanKiller + "\n")
		if FlipToDirection(in.PriorPlanKiller) != "" {
			b.WriteString("  This is a flip-condition — the bias flip has ALREADY FIRED on machine-evaluated bars; your new plan MUST use that flipped direction, never the old bias.\n")
		}
		b.WriteString("\n")
	}
	if len(in.PriorPlanLevels) > 0 {
		b.WriteString("## Prior plan levels (continuity — carry over or re-anchor by price, do not silently drop the set)\n")
		for _, l := range in.PriorPlanLevels {
			b.WriteString("- " + l + "\n")
		}
		b.WriteString("\n")
	}

	// A1 (2026-08-26) — anchor-role truth: ONH/ONL are liquidity references,
	// not fade walls. Week evidence: ONH entries 14 · 21.4% win · −131 — the
	// fade framing lost; the research stat (broken intraday ~94% of days,
	// 2,827-day NQ sample) says they break nearly always. Advisory — no block.
	b.WriteString("## Anchor roles (week evidence, advisory)\n")
	b.WriteString("  ONH/ONL = LIQUIDITY/BREAKOUT references — broken intraday ~94% of days (2,827-day NQ sample). ")
	b.WriteString("Fade ONLY on a confirmed sweep-reclaim (wick through + close back inside on the decision TF). ")
	b.WriteString("Otherwise treat them as targets / breakout-retest anchors, never fade walls.\n\n")

	// A1 (planner-contract wave 2026-08-26) — BIAS DECISION TREE replaces
	// free-form bias guidance. Machine-computed branches; the planner names
	// the branch it took (contract requirement).
	if in.Price > 0 {
		b.WriteString(RenderBiasTree(in.Price, in.Levels, in.BiasCtxFacts))
		// CLASS 50b (live-bias replay ruling 53498adb, 2026-09-02) — the plan
		// bias is a LABEL, not a direction: the AI leg, the tree leg and the
		// regime leg are surfaced on ONE line so no single source reads as
		// truth. No MUST attaches to either.
		b.WriteString(BiasLabelLine("yours", TreeCallWord(in.Price, in.BiasCtxFacts.PDH, in.BiasCtxFacts.PDL, in.BiasCtxFacts.PDC), RegimeCallWord(in.Regime)))
		b.WriteString(" — labels only, no MUST on either\n\n")
	}

	// A2 — priority setup chain: the sweep → displacement → FVG-retrace play.
	b.WriteString("## Priority setup — THE CHAIN (A2, week evidence, advisory)\n")
	b.WriteString("  The A-setup: sweep of a pool → displacement → FVG retrace at a Tier-1/fresh-zone origin, inside a killzone. ")
	b.WriteString("fvg_entry scenarios SHOULD declare \"chain_after\": \"S#\" naming the sweep_reclaim that swept the origin pool. ")
	b.WriteString("entry_mode=ce is the DEFAULT; edge only for A-grade HTF-confluent origins. Stop beyond the sweep extreme; T1 = first opposing pool; the runner = the draw. ")
	b.WriteString("Citation: raw-FVG carries no standalone edge (40k-sample null) — the edge is conditional on the sweep→displacement chain.\n\n")

	// A3 — no-trade gates (≤8 lines, advisory — the plan declares them; the
	// executor still sees every cycle).
	lunchStartCT, lunchEndCT := LunchWindowCT()
	// The header used to read "advisory — declare in no_trade or skip the day"
	// over a list that includes the machine's hard-gated lunch window and
	// Tier-1 news. That ordered the author to restate exactly what the
	// no_trade instruction now forbids, and a header is read before a rule.
	b.WriteString("## No-trade gates (the machine enforces the windows below; the rest are yours to weigh — skip the day if they stack up)\n")
	b.WriteString("  - balance-day (open inside prior value area AND VAs overlap) → edges-only, or skip\n")
	b.WriteString("  - opening gap >1.2×ATR or open outside the prior range → NEVER fade; the gap is a target\n")
	b.WriteString(fmt.Sprintf("  - no A/B zone in reach AND no pool swept by %s CT → declare the skip in the plan\n", ETtoCT("10:30")))
	// SUPERSEDED by faa3526f — the paragraph below describes the line as it read
	// BEFORE that commit and is kept as provenance only. There is no ET half in
	// this prompt any more: the sentence emitted below carries the machine's CT
	// window and nothing else. Read it in the past tense.
	//
	// The hard-gate half is RESOLVED, not typed: it was a third copy of the
	// lunch window, and the F4 literal scan missed it because the bounds sit
	// unquoted inside a longer sentence. The 11:30–13:30 ET half was the lunch
	// LULL (trading lore, advisory), deliberately not the same window as the
	// machine gate — see the report for that discrepancy.
	// ONE window (owner ruling 2026-09-03). This line used to carry an
	// 11:30–13:30 ET lull BESIDE the machine's 12:00–13:30 CT gate — two
	// different windows in one sentence, in two clocks, one of them enforced.
	// The machine's is the only one, stated in CT, rendered from its resolver.
	b.WriteString(fmt.Sprintf("  - lunch %s–%s CT: no new entries (refused on the AI-decision path only — no band predicate exists in the arm path, so with plan_mode=strict nothing refuses an entry here)\n", lunchStartCT, lunchEndCT))
	b.WriteString("  - Tier-1 news → stand aside until a fresh post-news swing prints\n\n")

	// A4 — killzone weighting (advisory, not a gate).
	b.WriteString("## Killzone weighting (advisory)\n")
	b.WriteString(fmt.Sprintf("  NY AM %s–%s CT is the primary window; %s–%s CT is the premium FVG window; mind the macro minutes. ",
		ETtoCT("08:30"), ETtoCT("11:00"), ETtoCT("10:00"), ETtoCT("11:00")))
	b.WriteString("Conviction: down on Monday, up Thursday/Friday.\n\n")

	// A5 — stop-doing line: bare acceptance entries have no edge.
	b.WriteString("## STOP-DOING (week evidence, advisory)\n")
	b.WriteString("  acceptance entries WITHOUT a prior sweep + displacement are 0% win evidence — skip them (A5).\n\n")

	// 1h wave (2026-08-25) — the conditional 1h S/D mandate: emitted only when
	// a 1h supply/demand zone is actually rendered in the HTF zones section
	// (same conditional pattern as the G2.2 HTF mandate fix — a rule that asks
	// for something absent burns retries).
	has1HSD := false
	for _, z := range in.HTFZones {
		if is1HSDZone(z) {
			has1HSD = true
			break
		}
	}

	b.WriteString(plannerOutputContractFor(in.MaxLevels, in.ScenarioCap, len(in.HTFZones) > 0, has1HSD, in.WriteFeasibilityOn,
		resolvePromptEntryPolicy(in.EntryPolicyDefault, in.ZoneMaxPts, in.MinHoldMin)))
	return b.String()
}

// plannerOutputContract is the schema-strict, reasoning-first output spec. The
// level/scenario caps are the RESOLVED config values (H4/H5) — the prompt must
// ask for what validation will accept, so a raised max_levels/scenario_cap both
// gets requested AND passes instead of fail-closing every read against a
// hardcoded 8/3.
func plannerOutputContract(maxLevels, maxScenarios int, hasHTFZones, has1HSDZone bool, writeFeas bool) string {
	// W-EXEC-TRUTH W3: the five-argument form is the LEGACY-policy rendering
	// (the pre-W3 text byte-identical — every existing contract test keeps
	// judging exactly what it judged). Production (BuildPlannerPrompt) calls
	// plannerOutputContractFor with the RESOLVED policy; entry_policy_prompt_test
	// runs the class-38 contract over every policy's rendering.
	return plannerOutputContractFor(maxLevels, maxScenarios, hasHTFZones, has1HSDZone, writeFeas,
		entryPolicyPromptInput{Policy: EntryPolicyDefaultLegacy})
}

// plannerOutputContractFor renders the output contract under the resolved
// entry policy (W3 (h)).
func plannerOutputContractFor(maxLevels, maxScenarios int, hasHTFZones, has1HSDZone bool, writeFeas bool, ep entryPolicyPromptInput) string {
	maxL, maxS := resolvePlanCaps(maxLevels, maxScenarios)
	htfRule := ""
	if hasHTFZones {
		htfRule = " — plus the HTF zones section, where you MUST include at least ONE HTF zone row in your levels as a confluence reference, never as a standalone trigger"
	}
	if has1HSDZone {
		htfRule += " — the nearest 1h supply/demand zone row in that section MUST be one of your included rows (setup-rung context, never a standalone trigger)"
	}
	return "## OUTPUT — one JSON object, reasoning FIRST, no prose outside it\n" +
		"{\n" +
		`  "reasoning": "<your read: what the auction is doing and why this plan — ≤200 words, decision-focused>",` + "\n" +
		`  "bias": {"direction": "long|short|neutral", "conviction": "high|medium|low", "flip_condition": "<explicit>"},` + "\n" +
		fmt.Sprintf(`  "levels": [{"price": <n>, "label": "<PDH|ONH|nPOC…>", "grade": "A|B|C", "instruction": "<verb>"}],  // max %d, MUST include ≥3 below AND ≥3 above the current price`, maxL) + "\n" +
		fmt.Sprintf(`  "scenarios": [{"id": "S1", "level_id": "<candidate id from map, or null when map id is NULL>", "trigger": "<setup>", "condition": "reclaim|hold|sweep_reclaim|reject|acceptance|breakout_retest|fvg_entry|breakdown_continue|breakup_continue", "direction": "long|short", "target_chain": [<n>,…], "invalid": "<ONE sentence in the invalid GRAMMAR below>", "quality": "A+|A|B|C", "chain_after": "<S# of the sweep_reclaim this fvg_entry follows, or omit>", "confirm": {"rule": "touch|1x5m_close|2x5m_close|1m_mss|time_hold", "ref_price": <n>, "side": "above|below", "hold_min": <n>} (hold_min is time_hold ONLY — the minutes of 1m closes your prose states; the machine counts EXACTLY the stored rule: 2x5m_close waits for TWO completed 5m closes, a time_hold waits hold_min minutes), "confirm2": {"rule": "<leg 2 rule>", "ref_price": <n>, "side": "above|below"} (OPTIONAL second trigger leg — a two-leg setup MUST carry both legs; the machine renders EVERY leg and a partial NEVER reads MET), "fvg": {"fvg_lo": <n>, "fvg_hi": <n>, "entry_mode": "edge|ce", "displacement_atr": <n>, "origin_level": "<label>", "direction": "long|short"}, "breakdown": {"level": <n>, "level_label": "<label>", "entry_mode": "pullback|immediate"} (REQUIRED iff condition==breakdown_continue|breakup_continue — author ONLY at a level whose MEASURED displacement in the block above is ≥ the stated floor; a level reading \"none — no break\" or below the floor is REFUSED at write — see the WATERFALL PLAY rule), "arm": {"enabled": true, "entry": <n>, "stop": <n>, "target": <n>, "wait_confirm": true, "legs": [{"entry": <n>, "stop": <n>, "target": <n>, "size": 1, "wait_confirm": false, "rule": "<rule>"}, …] (ONLY if condition is sweep_reclaim — the split contract, EXACTLY 2 legs; EVERY other condition arms SINGLE: omit legs)}}],  // 1..%d — confirm{} is REQUIRED per scenario; fvg{} REQUIRED iff condition=="fvg_entry" (ce is COMPUTED, never written); breakdown{} REQUIRED iff waterfall-class; chain_after is OPTIONAL; %s — see the ARMED ORDERS + ENTRY LAW rules`, maxS, ep.armLegalClause()) + "\n" +
		AuthoredInvalidationGrammarLine() + "\n" + // W2 A1 — the grammar the born check enforces (class 38 row)
		NoTradeSchemaExample() + "\n" + // S3 (2026-09-16) — the relation contract: always rendered (the
		// class-38 guard asserts the fragment), conditionally relevant.
		"RELATION FIELDS (S3): relation_d and relation_4h are VALIDATOR-STAMPED from the structure table (with-trend | counter-trend | range) — the validator stamps relation_d / relation_4h itself, the model never writes them. Your own claim may go in relation_claimed and is kept but never trusted. A counter-trend scenario is FLAGGED, never blocked. " + `  "death_condition": "<the single line that invalidates this whole plan>",` + "\n" +
		`  "death": {"price": <level>, "side": "below|above", "rule": "2x5m|5m_close"},` + "\n" +
		`  "flip": {"price": <level>, "side": "below|above", "rule": "2x5m|5m_close", "flip_to": "long|short"},  // NOTE: death/flip rules use their OWN vocabulary (2x5m | 5m_close) — NEVER the confirm enum; do not move a token between the two` + "\n" +
		`  "day_type": "trend|balance|<optional>"` + "\n" +
		"}\n" +
		"Rules: levels chosen ONLY from the ranked table above" + htfRule + "; levels MUST be at least 3 points apart — near-duplicates are merged by the system; S/D & FVG are confluence, never standalone. " +
		"Copy the EXACT label from the table row for the price you choose — never re-label a table level as a different anchor (a zone price relabeled 'PDH/PDL/PDC' is a phantom level and is REJECTED at write). " +
		"Quality: A+ = highest-conviction setup, A = strong, B = workable, C = machine-demoted (trigger level consumed at write — G5) — use C honestly for a demoted setup, never as a default. " +
		"The scenario MIX must follow the regime + day_type: a trend-down day gets breakdown/pullback-short plays, a trend-up day the reverse, balance days get two-sided plays — do NOT default to 2 longs + 1 rally-rejection short on every day. " +
		// CLASS 45 (2026-09-02): this used to order a CONDITION ("a continuation
		// short"), which the WATERFALL PLAY rule below binds to
		// breakdown_continue/breakup_continue. When every breakdown level was
		// already void the order was UNSATISFIABLE, and the model obeyed it into
		// a guaranteed reject (LONDON 01:32→01:37, three attempts, session lost).
		// It now orders a DIRECTION and names the legal conditions.
		"If price sits BELOW PDL the plan MUST include a SHORT-direction scenario (ANY legal condition — reject, breakdown_continue, acceptance, sweep_reclaim, hold, reclaim); ABOVE PDH, a LONG-direction scenario. Pick the condition the TAPE supports: if a breakdown level is listed as VOID above, author a different condition there. " +
		"A1: your reasoning MUST open by naming the bias-tree branch you took (e.g. \"bias-tree: inside-day long LOW\"), then argue from it. " +
		"A2: an fvg_entry SHOULD chain after a sweep_reclaim (chain_after: S#) — bare gaps at non-A/B origins get a WARN at write, not a reject. " + "A2b (machine grounding, 2026-08-27): author an fvg_entry scenario ONLY from the ## FRESH FVGs list above — copy its direction and lo–hi EXACTLY. If the list is empty, do NOT author any fvg_entry (invented/stale gaps are REJECTED at write). " + "A2c (FVG demand, 2026-08-28): when ## FRESH FVGs is NON-empty and at least one candidate's direction agrees with your bias, you SHOULD author an fvg_entry from that candidate; if you decide not to, state the reason in ONE line in your reasoning (e.g. 'no fvg_entry: nearest fresh gap is 30pt away — outside my reach'). " + "death.flip objects are MACHINE-EVALUATED — choose levels from your level list and a rule; they must match the prose lines. " +
		"The flip and death MUST be DIFFERENT events: never the same level AND same rule for both (a flip at the same tick death fires is void). A short-biased plan's flip sits BELOW its death line or uses a stricter rule, so the flip can actually fire. " +
		// W-FLIP-DIRECTION (2026-09-17): the SIDE is judged now, not only the number.
		"flip side must oppose the bias: short bias flips long on a close ABOVE; long bias flips short on a close BELOW (a short bias with flip.side below is REJECTED — it could never flip on a rally). " +
		// W-FLIP-LINE-SIDE-OF-PRICE (2026-09-17): the side is judged against PRICE too.
		"a flip line must sit on the far side of price at authoring: side above → the line is ABOVE the current price, side below → BELOW it (a line already beyond price on its own side can never be touched from the near side, so it never fires — REJECTED at write); the death line obeys the same law (a death line already crossed is a plan born dead). " +
		"Every scenario's confirm{} is MACHINE-EVALUATED the same way: rule + ref_price + side, and ref_price MUST equal a number written in that scenario's trigger/invalid prose. " +
		// ENTRY-MECHANICS E1/E2 (2026-08-30) — the per-condition entry law.
		// 15m confirms are DEAD (schema reject confirm_rule_15m_removed).
		"ENTRY LAW (per condition — the machine REJECTS violations by name): reject → touch ONLY (fade_requires_touch) with a structure stop ≥2 ticks beyond the level · fvg_entry → touch ONLY, entry price inside the FVG edge..CE band · sweep_reclaim → leg-1 touch at the sweep ref, leg-2 1m_mss (1x5m_close accepted as the leg-2 alternative) · reclaim → 1x5m_close or 1m_mss, never 2x5m_close · breakout_retest → touch at the retest + stop-entry fallback, 1x5m_close legal for the break leg · acceptance/hold → time_hold (price holds beyond ref for ACCEPT_HOLD_MIN minutes of 1m closes) with 1x5m_close as fallback · breakdown/breakup_continue → 1 confirming close + displacement ≥ BD_MIN_DISP_ATR×ATR5m (BD_MIN_CLOSES=" + fmt.Sprintf("%d", bdConfirmCloses()) + ") or stop-entry — 2x5m_close is legal ONLY here (everywhere else: 2x5m_reserved). Default confirm = 1x5m_close (single close). " +
		"SCENARIO ECONOMICS CONTRACT (required for NEW AUTHORING; legacy reads remain UNKNOWN): every scenario states entry zone, trigger, confirm{}, structural invalidation (invalid), protective stop, first opposing obstacle with level/family/price provenance, planned response there, arm target and both implied R values. " +
		"Include economics:{entry_zone:[low,high],first_obstacle:{price:<n>,level:<label>,family:<family>,response:pass_through|reduce|exit|decline_setup},r_to_obstacle:<n>,r_to_arm_target:<n>,target_path_exception:<reason if needed>,role_exceptions:[{level:<label>,use:entry|target|invalidation,reason:<why>}]} on EVERY scenario. " +
		"With arm{}, read entry/stop/target from that arm ONLY; do not duplicate them in economics. Without an arm, provide economics.geometry:{entry:<n>,stop:<protective stop>,target:<arm objective>} as hypothetical geometry; this NEVER authorizes an arm or changes which conditions are armable. " +
		"R = abs(price-entry)/abs(entry-stop), from the same proposed geometry, before rounding/costs. State r_to_obstacle and r_to_arm_target EXACTLY as computed (6+ decimals, never a rounded shorthand like 2.0); the machine recomputes both and refuses a stated R that drifts more than one tick of price distance from geometry, though an arm-target R whose computed value meets the minimum floor is auto-corrected to the computed value and accepted. The FIRST opposing obstacle must be between entry and the arm target; name the action there. A sub-1R obstacle is WARN only and remains admissible. No structural, fixed-R, ATR, partial, trailing or mandatory-1R target policy is prescribed. A reduce response needs more than one contract: reduce on a single-contract arm (every single arm, or split legs summing to 1 contract) is REFUSED at write as infeasible — one contract cannot be halved; use pass_through or exit there. " +
		"NEW-authoring REFUSALS: missing complete economics/obstacle; arm target absent from target_chain within one MNQ tick without target_path_exception; obstacle beyond arm target; stated R differing from geometry by more than one tick of price distance (an arm-target R whose machine-computed value meets the minimum R:R floor is auto-corrected and accepted instead — never round R values). Role/use differences are WARN + counter only; state role_exceptions when using a level differently. Entry exclusion never removes a level as a possible target, obstacle or invalidation reference. " +
		ScenarioWriteTruthSentences() + // W-EXEC-TRUTH W2 A3+A4 (correction; unconditional)
		"target_chain is GUIDANCE for the executor AI (which sets the actual take_profit) — it is validated for reachability at write time but never enforced at execution (D2 ruling). " +
		// WAVE 2 armed orders (2026-08-27) — the arming authorization. The LLM
		// chooses WHAT to arm; Go manages WHEN it fills (tick-level).
		// Autopsy-response wave: armable A/B setups SHOULD carry arm{} (the
		// resting order is the fast path); sweep_reclaim retraces chain via
		// wait_confirm; fantasy targets (planned R > 6) are WARN-flagged.
		"ARM SPLIT vs ARM SINGLE (class 38 — the validator refuses every other shape): legs[] are the sweep_reclaim SPLIT contract and nothing else — EXACTLY 2 legs, confirm=touch at the sweep ref, leg 1 rests there (wait_confirm false) and leg 2 chains (wait_confirm true) on confirm2 = 1m_mss or 1x5m_close with leg 2's rule EQUAL to confirm2.rule, and the top-level entry/stop/target mirror leg 1. " + ep.armSingleClause() +
		"ARMED ORDERS (the resting order IS the fast path — prefer it over a 2-minute debate at the touch). " +
		ep.armsFollowBias() + // W3 (h): the ENTRY POLICY sentence + ARMS FOLLOW THE BIAS
		"WHICH CONDITIONS CAN BE ARMED — " + ArmableConditionsLineFor(ResolvedConditionStatuses(nil, nil, ShadowConditionsEnv()), ep.Policy) + " " +
		ep.entryTypeSentence() +
		"Every armed scenario at quality A or B SHOULD carry arm{} — enabled:true + EXACT entry/stop/target " + ep.breakoutRetestClause() + ". A setup the planner believes in gets a resting order, not a mid-touch argument. Long: stop < entry < target. Short: target < entry < stop. CHAINED ARMS: when a sweep_reclaim you believe in confirms, its RETRACE entry should already be resting — author that retrace as its own arm with wait_confirm:true, or add wait_confirm:true to the sweep scenario's arm: the system holds the arm dormant until the scenario's confirm{} is machine-MET, then places it (the sweep fast path). " + ep.neverArmClause() + " Keep targets REAL: a planned R:R above ~6 is a fantasy target and gets WARN-flagged at write. " + ep.placementClause() + " FEASIBILITY CONTRACT: an arm{} MUST be gate-feasible or it is REFUSED every cycle and learns nothing — R:R = |target\u2212entry| \u00f7 |stop\u2212entry| must be \u2265 2.0 (ARM_MIN_RR) AND the stop distance must be \u2265 " + fmt.Sprintf("%.1f", MinSLATRMult()) + "\u00d7 the current 5m ATR (the facts list the session ATR5m — cite the live value; a 10-point stop when ATR5m is ~16 is an instant refuse). " + ep.omitArmClause() + " " + ep.waterfallArmsClause() +
		// A2 (2026-08-26) — condition×session guidance from the week ledger:
		// reject 75% win +665 in NY RTH vs acceptance 0% −157 and sweep_reclaim
		// 0% −192. Advisory truth, not a hard rule.
		"Condition×session guidance (week evidence): reject-based setups are best in NY RTH (75% win, +665 this week); acceptance needs a clear displacement or skip (0% win this week); sweep_reclaim requires the reclaim CLOSE on the decision TF, never the wick alone (0% win this week). " +
		// WATERFALL PLAY (F1, 2026-08-28) — the momentum-follow class the 2026-08-28
		// -347pt crash exposed (missed-200pt report: every authored short was a
		// retest-FADE; no continuation entry existed, $0-by-own-rules). Author a
		// breakdown_continue (short) / breakup_continue (long) when: one-sided
		// delivery, a >1.2×ATR gap-and-go, or a waterfall after a failed rally.
		// breakdown{} carries the broken LEVEL (the retest) + entry_mode
		// (pullback = the shallow-retrace-that-fails entry, armable; immediate =
		// enter on the CONFIRMING close (BD_MIN_CLOSES, default 1), AI path).
		// The confirm{} is leg 1 (the close beyond the level — machine-verified
		// displacement ≥ BD_MIN_DISP_ATR×ATR5m, no reclaim close at write);
		// confirm2{} is leg 2 (the retest that fails to reclaim). The machine
		// renders both legs and a partial NEVER reads MET. Targets chain to the
		// next liquidity below (above for longs); SL beyond the failed pullback
		// extreme, ≥1×ATR5m.
		"WATERFALL PLAY (F1): author breakdown_continue|breakup_continue when the tape shows one-sided delivery, a >1.2×ATR gap-and-go, or a waterfall after a failed rally — the momentum-follow class. breakdown{} = broken level + entry_mode; confirm = leg 1 (breakdown), confirm2 = leg 2 (failed retest). " + ep.waterfallEntryModes(fmt.Sprintf("%d", bdConfirmCloses())) + // B3 (2026-08-26) — the ≤5-line noise-filter gate: the plan may include		// at most 5 near-duplicate LINE rows (within 3 points of each other);
		// keep the strongest anchor, drop the crowd.
		"NOISE FILTER (≤5): at most 5 of your included level rows may be line-levels clustered within 3 points of each other — keep the strongest of any such cluster, never a crowd. " +
		// FVG ENTRY MODEL (2026-08-26) — the 5th condition's ≤6-line playbook.
		"FVG ENTRY (fvg_entry): author when displacement off a Tier-1 level (VWAP/POC/PDH/PDL/ON/IB/fresh S/D) leaves a FRESH gap toward the trade direction · prefer the FIRST retrace (the freshness ladder applies to the gap as a zone) · entry_mode=ce for gaps > 20pts, edge for tighter · SL beyond the DISTAL edge · targets chain to the next liquidity (EQH/EQL, ON, PDH/PDL). Citations: NQ gap sweet spot 20–80pts; 1h+ fill ~70–80%; displacement floor per MSS research. " +
		NoTradeInstruction() +
		"Respect the no-trade windows. If you cannot form a credible plan, say so in reasoning and output a neutral/no-trade plan. " +
		// W2b (weekly-bias wave) — candle ground-truth law.
		"Candles are ground truth for structure; ranked levels and tags are summaries. On conflict, trust the candles and say so in the scenario rationale. " +
		// W3 (weekly-bias wave) — soft weekly law.
		"Weekly guidance (soft law): counter-weekly scenarios are allowed but must state their justification (an HTF level or a sweep-reclaim of the draw); target chains toward the draw are preferred. The weekly bias never gates your plan.\n" +
		writeTimeFeasibilitySentence(writeFeas)
}

// writeTimeFeasibilitySentence renders the write-time feasibility contract
// sentence only when the knob is ON (the owner's default). OFF leaves the
// contract byte-identical to before the wave (L4). The class-38 guard asserts
// the fragments through the ON path.
func writeTimeFeasibilitySentence(on bool) string {
	if !on {
		return ""
	}
	return "WRITE-TIME FEASIBILITY: a scenario whose arm would be refused by the gate-at-arm chain " +
		"(stop too close to the min-SL floor, arm R:R below the arm minimum, a level the structural-geometry gate refuses, " +
		"or a stop-entry trigger already through price) " +
		"is repaired first; after the last repair attempt it is written with arm.enabled=false " +
		"and arm_disabled_reason naming the refusal. "
}

func absF(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
