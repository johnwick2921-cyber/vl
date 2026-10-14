package kernel

// WEEKLY-BIAS WAVE (2026-08-30) — W2 Sunday weekly read: the prompt builder,
// the fail-closed validator (r1-r6), the weekly doc schema, and the pure W4/W5
// helpers (invalidation watch, shadow grading, counter clauses, draw-alignment
// tag). Everything here is pure Go — no DB, no LLM, no gates. The trader layer
// (trader/auto_trader_weekly.go) drives it from the EXISTING cycle.
//
// LAW (W5.4): nothing in this file feeds back into seating, grades, gates or
// sizes. Shadow results are LOG/Counter-only views — the real system is frozen.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"vl/market"
)

// ── facts (what the model actually sees) ───────────────────────────────────

// WeeklyFacts is the rendered evidence package the weekly read is built from.
// FactsHash proves which facts the model saw (sha256 of the rendered facts
// sections — weekly candles + references + NWOG + IPDA + prior-week recap).
type WeeklyFacts struct {
	Now          time.Time
	Price        float64
	Weeks        []WeekCandle // ≤12 completed, oldest→latest
	Refs         WeeklyRefs
	RefsOK       bool
	NWOGs        []NWOG // last 5
	IPDA         []IPDARange
	ThinHistory  bool // CompletedWeekCount < 4
	SectionsText string
	FactsHash    string
}

// ComputeWeeklyFacts derives the full evidence package from bars stamped at
// their OPEN: 1m/1h intraday bars, or CME session-day bars from
// SessionDaysFromNativeDaily (stamped at the 17:00 CT session open). It must
// NEVER be fed pre-aggregated weekly bars (R22 P1-A: epoch weeks start on a
// Thursday, and IPDA/NWOG cannot be computed from weeks).
// Price ≤ 0 → the last bar close. Thin history is stamped, never faked.
func ComputeWeeklyFacts(bars1m []market.Kline, now time.Time, price float64) WeeklyFacts {
	f := WeeklyFacts{Now: now, Price: price}
	f.Weeks = CompletedWeekCandles(bars1m, now, 12)
	f.Refs, f.RefsOK = PriorWeekRefs(bars1m, now)
	f.NWOGs = LastNWOGs(bars1m, now, 5)
	f.IPDA = IPDA(DailySessionBars(bars1m), price)
	f.ThinHistory = CompletedWeekCount(bars1m, now) < 4
	if f.Price <= 0 && len(bars1m) > 0 {
		f.Price = bars1m[len(bars1m)-1].Close
	}
	f.SectionsText = RenderWeeklyFactsSections(f)
	f.FactsHash = WeeklyFactsHash(f)
	return f
}

// RenderWeeklyFactsSections renders the five facts sections, in spec order:
// Weekly candles · Weekly references · NWOG table · IPDA · Prior week recap.
func RenderWeeklyFactsSections(f WeeklyFacts) string {
	var b strings.Builder

	b.WriteString("## Weekly candles (12 completed weeks, oldest → latest)\n")
	if len(f.Weeks) == 0 {
		b.WriteString("(no completed weeks stored — thin history)\n")
	} else {
		b.WriteString("Time(CT)  Open  High  Low  Close  Volume  Struct\n")
		for _, w := range f.Weeks {
			b.WriteString(w.RenderRow() + "\n")
		}
	}
	b.WriteString("\n")

	b.WriteString("## Weekly references\n")
	if !f.RefsOK {
		b.WriteString("(no completed prior week — thin history)\n")
	} else {
		// The Sunday 16:30 CT read runs BEFORE the week's first print (17:00
		// CT), so weekly_open is not known yet: it prints n/a, never 0.00 (L7).
		wo := "n/a (the week has not opened)"
		if f.Refs.WeeklyOpen > 0 {
			wo = fmt.Sprintf("%.2f", f.Refs.WeeklyOpen)
		}
		fmt.Fprintf(&b, "weekly_open %s · PWH %.2f · PWL %.2f · PWC %.2f\n",
			wo, f.Refs.PWH, f.Refs.PWL, f.Refs.PWC)
	}
	b.WriteString("\n")

	b.WriteString("## NWOG (last 5 weekend gaps, oldest → latest)\n")
	if len(f.NWOGs) == 0 {
		b.WriteString("(none computed — thin history)\n")
	} else {
		b.WriteString("born  hi  lo  CE  filled\n")
		for _, g := range f.NWOGs {
			filled := "no"
			if g.Filled {
				filled = "yes"
			}
			fmt.Fprintf(&b, "%s  %.2f  %.2f  %.2f  %s\n", g.Born, g.Hi, g.Lo, g.CE, filled)
		}
	}
	b.WriteString("\n")

	b.WriteString("## IPDA (trailing dealing ranges)\n")
	for _, r := range f.IPDA {
		if r.PosPct < 0 {
			fmt.Fprintf(&b, "%dd: insufficient history\n", r.Days)
			continue
		}
		fmt.Fprintf(&b, "%dd: high %.2f · low %.2f · price at %.0f%% of range\n", r.Days, r.High, r.Low, r.PosPct*100)
	}
	b.WriteString("\n")

	b.WriteString("## Prior week recap (facts only)\n")
	if len(f.Weeks) < 2 {
		b.WriteString("(insufficient history for a prior-week recap)\n")
	} else {
		prev := f.Weeks[len(f.Weeks)-1]
		rng := prev.High - prev.Low
		loc := 0.0
		if rng > 0 {
			loc = (prev.Close - prev.Low) / rng * 100
		}
		fmt.Fprintf(&b, "range %.1f pts · close at %.0f%% of range · touched/swept refs: none (touch data not computed)\n",
			rng, loc)
	}
	b.WriteString("\n")
	return b.String()
}

// WeeklyFactsHash is the sha256 hex of the rendered facts sections — the audit
// proof of exactly which facts the model saw (stored on the weekly plan row).
func WeeklyFactsHash(f WeeklyFacts) string {
	sum := sha256.Sum256([]byte(RenderWeeklyFactsSections(f)))
	return hex.EncodeToString(sum[:])
}

// WeeklyRefSet returns every computed reference the validator's r3 rule tests
// the draw against: IPDA extremes (computed ranges only), PWH, PWL, and the
// edges of UNFILLED weekend gaps — exactly the spec's r3 set (weekly_open is
// NOT draw material; it is a level-only reference).
func WeeklyRefSet(f WeeklyFacts) []float64 {
	seen := map[float64]bool{}
	add := func(p float64) {
		if p > 0 {
			seen[p] = true
		}
	}
	for _, r := range f.IPDA {
		if r.PosPct >= 0 {
			add(r.High)
			add(r.Low)
		}
	}
	add(f.Refs.PWH)
	add(f.Refs.PWL)
	for _, g := range f.NWOGs {
		if !g.Filled {
			add(g.Hi)
			add(g.Lo)
		}
	}
	out := make([]float64, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

// ── the weekly doc ──────────────────────────────────────────────────────────

// WeeklyDoc is the JSON the Sunday read stores on the plans row
// (session="WEEKLY"). Schema EXACT per spec W2.2.
//
// REFS-ONLY WAVE (2026-09-02, class 50): the doc carries PWH/PWL/IPDA as PRICE
// FACTS and NO directional call. Bias/Conviction/Draw/Invalidation remain in
// the schema ONLY so pre-wave stored rows still parse; nothing reads them as a
// direction (the chips and prompts render refs-only). The deterministic bias
// rule survives as ShadowBias/ShadowWhy — stamped at write, never read by any
// consumer, kept so the anti-prediction keeps being measured (calibration
// report 2026-09-02).
type WeeklyDoc struct {
	Bias         string             `json:"bias"`       // legacy (pre-wave) — never read as direction
	Conviction   string             `json:"conviction"` // legacy (pre-wave)
	Draw         WeeklyDraw         `json:"draw"`       // legacy (pre-wave)
	Invalidation WeeklyInvalidation `json:"invalidation"`
	WeeklyLevels []WeeklyLevel      `json:"weekly_levels"`
	Narrative    string             `json:"narrative"` // ≤3 lines, plain auction language
	// stamped at write (never model-written):
	FactsHash     string `json:"facts_hash,omitempty"`
	ThinHistory   bool   `json:"thin_history,omitempty"`
	InvalidatedAt string `json:"invalidated_at,omitempty"` // W4 mid-week invalidation stamp (legacy — refs-only docs never invalidate)
	ShadowBias    string `json:"shadow_bias,omitempty"`    // class 50: the rule bias that WOULD have been called — shadow only
	ShadowWhy     string `json:"shadow_why,omitempty"`
}

// WeeklyDraw is the draw_on_liquidity object.
type WeeklyDraw struct {
	Name string  `json:"name"` // the reference name (PWH / PWL / IPDA-20d-high / NWOG edge …)
	Px   float64 `json:"px"`
}

// WeeklyInvalidation is the MANDATORY invalidation price + basis.
type WeeklyInvalidation struct {
	Px    float64 `json:"px"`
	Basis string  `json:"basis"` // e.g. "1h close beyond 30300.00"
}

// WeeklyLevel is one weekly-class level row.
type WeeklyLevel struct {
	Name string  `json:"name"`
	Px   float64 `json:"px"`
}

// ParseWeeklyDoc extracts the weekly doc JSON from a raw model reply (tolerant
// of markdown fences / prose around the object).
func ParseWeeklyDoc(raw string) (*WeeklyDoc, error) {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		if j := strings.Index(s, "```"); j > 0 {
			s = s[:j]
		}
	}
	if i := strings.Index(s, "{"); i >= 0 {
		s = s[i:]
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[:j+1]
		}
	}
	var d WeeklyDoc
	if err := json.Unmarshal([]byte(s), &d); err != nil {
		return nil, fmt.Errorf("weekly doc JSON malformed: %v", err)
	}
	return &d, nil
}

// ValidateWeeklyDoc is the FAIL-CLOSED validator, REFS-ONLY (class 50): the
// doc is accepted when it carries the weekly-class reference facts — nothing
// directional is asked, checked, or rewarded. Returns "" on acceptance.
func ValidateWeeklyDoc(d *WeeklyDoc, refs []float64, thinHistory bool) string {
	if d == nil {
		return "r1: empty doc"
	}
	// r1 — the references ARE the doc: at least one weekly_level with a real px
	// and a non-empty name (PWH/PWL/IPDA extremes/NWOG edges).
	if len(d.WeeklyLevels) == 0 {
		return "r1: weekly_levels empty — the doc must carry the reference facts (PWH/PWL/IPDA)"
	}
	for _, l := range d.WeeklyLevels {
		if strings.TrimSpace(l.Name) == "" || l.Px <= 0 {
			return fmt.Sprintf("r1: weekly_level %q px %.2f invalid (name non-empty, px > 0)", l.Name, l.Px)
		}
	}
	// r2 — narrative ≤ 3 lines.
	if n := strings.Count(strings.TrimSpace(d.Narrative), "\n") + 1; n > 3 {
		return fmt.Sprintf("r2: narrative has %d lines (max 3)", n)
	}
	// r3 — day-of-week reasoning tokens FORBIDDEN.
	if HasDayOfWeekTokens(d.Narrative) {
		return "r3: narrative contains day-of-week reasoning (FORBIDDEN)"
	}
	// r4 — a directional call is FORBIDDEN anywhere in the doc (refs-only law:
	// bull/bear/long/short/upside/downside tokens reject).
	low := strings.ToLower(d.Narrative)
	for _, tok := range []string{"bull", "bear", "long", "short", "upside", "downside", "biased", "bias"} {
		if strings.Contains(low, tok) {
			return fmt.Sprintf("r4: directional token %q in narrative — the weekly doc is REFS ONLY (no bias call)", tok)
		}
	}
	return ""
}

// BuildWeeklyPrompt assembles the full Sunday read prompt: facts sections +
// the Instructions block + the exact output JSON schema. REFS-ONLY (class 50):
// the model lists the reference facts and writes a FACTS-ONLY narrative — no
// directional call exists anymore (calibration 2026-09-02: the bias was
// anti-predictive; nothing reads it as direction).
func BuildWeeklyPrompt(f WeeklyFacts) string {
	var b strings.Builder
	b.WriteString("# WEEKLY READ — CME MNQ futures\n")
	b.WriteString("You are a disciplined weekly-reference compiler. Read the facts ONCE and write ONE weekly reference doc for the coming week. Facts below are Go-computed; your job is to LIST the references, not to call a direction.\n\n")
	fmt.Fprintf(&b, "read time: %s (all times CT) · last stored price %.2f\n\n", ClockCTAndUTC(f.Now), f.Price)

	b.WriteString(f.SectionsText)

	b.WriteString("## Instructions (THE RULES — violations are rejected)\n")
	b.WriteString("1. weekly_levels lists the weekly-class PRICE FACTS the week should respect: PWH, PWL, the computed IPDA extremes, and unfilled NWOG edges. 3-6 entries, each from the facts above, px copied EXACTLY.\n")
	b.WriteString("2. NO directional call: no bias, no conviction, no draw, no invalidation, no long/short/bull/bear language — a direction anywhere = reject. The doc is REFS ONLY.\n")
	b.WriteString("3. Day-of-week reasoning is FORBIDDEN (any weekday token in the narrative = reject).\n")
	b.WriteString("4. narrative ≤ 3 lines, plain auction language, FACTS ONLY (where the references sit, what fills them) — no marketing, no prediction.\n\n")

	b.WriteString("## OUTPUT — one JSON object, no prose outside it\n")
	b.WriteString("```json\n")
	b.WriteString(`{"weekly_levels":[{"name":"<ref name>","px":<n>}],"narrative":"≤3 lines, facts only"}`)
	b.WriteString("\n```\n")
	b.WriteString("weekly_levels: the 3-6 weekly-class references the week should respect (drawn from the facts above).\n")
	return b.String()
}

// weeklyLevelPx returns the first weekly_level with the given name (0 when
// absent) — the refs-only renderer's lookup.
func weeklyLevelPx(d *WeeklyDoc, name string) float64 {
	if d == nil {
		return 0
	}
	for _, l := range d.WeeklyLevels {
		if strings.EqualFold(strings.TrimSpace(l.Name), name) && l.Px > 0 {
			return l.Px
		}
	}
	return 0
}

// WeeklyContextLine renders the ≤3-line injection block for the session
// planner prompt. REFS-ONLY (class 50): "WEEKLY: refs only — PWH x · PWL y";
// thin history appends the completed-week count. Never a direction.
func WeeklyContextLine(d *WeeklyDoc, thinWeeks int) string {
	if d == nil {
		return "WEEKLY: none"
	}
	pwh, pwl := weeklyLevelPx(d, "PWH"), weeklyLevelPx(d, "PWL")
	if pwh <= 0 || pwl <= 0 {
		if d.ThinHistory {
			return fmt.Sprintf("WEEKLY: refs only (thin history %dw)", thinWeeks)
		}
		return "WEEKLY: refs only"
	}
	line := fmt.Sprintf("WEEKLY: refs only — PWH %.2f · PWL %.2f", pwh, pwl)
	if d.ThinHistory {
		line += fmt.Sprintf(" (thin history %dw)", thinWeeks)
	}
	return line
}

// WeeklyExecutorLine renders the one executor-prompt context line (W3).
// REFS-ONLY (class 50): the same refs-only line; "" only when no doc exists.
func WeeklyExecutorLine(d *WeeklyDoc) string {
	if d == nil {
		return ""
	}
	pwh, pwl := weeklyLevelPx(d, "PWH"), weeklyLevelPx(d, "PWL")
	if pwh <= 0 || pwl <= 0 {
		return "WEEKLY: refs only"
	}
	return fmt.Sprintf("WEEKLY: refs only — PWH %.2f · PWL %.2f", pwh, pwl)
}

// WeeklyRuleBias is the SHADOW-ONLY deterministic reconstruction of the old
// Tier-A bias rule (calibration 2026-09-02, control reconstruction): bull when
// the prior completed week closes above its weekly_open AND breaks-and-holds
// PWH; bear symmetric vs PWL; else neutral. It exists so the anti-prediction
// keeps being measured — THE LAW: nothing reads its output as a direction; it
// is stamped on the doc as shadow_bias/shadow_why and logged.
func WeeklyRuleBias(f WeeklyFacts) (bias, why string) {
	c := 0.0
	if len(f.Weeks) > 0 {
		c = f.Weeks[len(f.Weeks)-1].Close
	}
	wo, pwh, pwl := f.Refs.WeeklyOpen, f.Refs.PWH, f.Refs.PWL
	if wo <= 0 || pwh <= 0 || pwl <= 0 || c <= 0 {
		return "neutral", "refs unavailable"
	}
	if c > wo && c > pwh {
		return "bull", fmt.Sprintf("close %.2f > weekly_open %.2f AND > PWH %.2f (break-and-hold)", c, wo, pwh)
	}
	if c < wo && c < pwl {
		return "bear", fmt.Sprintf("close %.2f < weekly_open %.2f AND < PWL %.2f (break-and-hold)", c, wo, pwl)
	}
	return "neutral", fmt.Sprintf("close %.2f inside weekly_open %.2f .. (PWH %.2f / PWL %.2f)", c, wo, pwh, pwl)
}

// ApplyWeeklyDOA (F5, 2026-08-30) — legacy bias write-time guard. SHADOW ONLY
// since class 50 (refs-only docs carry no bias/invalidation and nothing calls
// this from the trader); kept for the offline measurement of pre-wave rows and
// the pure-function tests. Returns true when it stamped neutral.
func ApplyWeeklyDOA(doc *WeeklyDoc, bars []market.Kline, now time.Time) bool {
	if doc == nil || strings.TrimSpace(doc.InvalidatedAt) != "" {
		return false
	}
	bias := strings.ToLower(strings.TrimSpace(doc.Bias))
	if (bias != "bull" && bias != "bear") || doc.Invalidation.Px <= 0 {
		return false
	}
	if !WeeklyInvalidationCrossed(bias, doc.Invalidation.Px, bars) {
		return false
	}
	doc.Bias = "neutral"
	doc.InvalidatedAt = FormatCT(now)
	return true
}

// ── W4 — mid-week invalidation watch (pure) ─────────────────────────────────

// WeeklyInvalidationBasisTF extracts the timeframe token from the basis string
// ("1h close beyond 30300.00" → "1h"). Falls back to the caller's default.
func WeeklyInvalidationBasisTF(basis string) string {
	f := strings.Fields(strings.TrimSpace(basis))
	if len(f) > 0 {
		tok := strings.ToLower(f[0])
		if strings.ContainsAny(tok, "mhd") {
			return tok
		}
	}
	return ""
}

// WeeklyInvalidationCrossed reports whether any CLOSED bar of the basis TF has
// crossed the invalidation price for the given bias (bull → a close below px;
// bear → a close above px). Neutral / px ≤ 0 → never crossed.
func WeeklyInvalidationCrossed(bias string, px float64, bars []market.Kline) bool {
	if px <= 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(bias)) {
	case "bull":
		for _, k := range bars {
			if k.Close < px {
				return true
			}
		}
	case "bear":
		for _, k := range bars {
			if k.Close > px {
				return true
			}
		}
	}
	return false
}

// ── W5 — shadow mode (view-only, THE LAW: real system frozen) ──────────────

// WeeklyShadowLevel is the lightweight seating view the shadow re-rank uses.
type WeeklyShadowLevel struct {
	Price float64
	Label string
	Grade string
}

// WeeklyShadowRefs returns the weekly-class reference set for the shadow
// confluence band, computed cheaply from 1m bars: PWH, PWL, weekly_open,
// computed IPDA extremes and unfilled NWOG edges (the W5.1 set — weekly_open
// IS included here, unlike the r3 draw set).
func WeeklyShadowRefs(bars1m []market.Kline, now time.Time, price float64) []float64 {
	f := ComputeWeeklyFacts(bars1m, now, price)
	seen := map[float64]bool{}
	for _, p := range WeeklyRefSet(f) {
		seen[p] = true
	}
	if f.Refs.WeeklyOpen > 0 {
		seen[f.Refs.WeeklyOpen] = true
	}
	out := make([]float64, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

// WeeklyConfluent reports whether a level sits within `band` (in ATR5m units
// × the caller's ATR) of any weekly-class reference.
func WeeklyConfluent(lv WeeklyShadowLevel, refs []float64, band, atr5m float64) bool {
	if atr5m <= 0 || band <= 0 {
		return false
	}
	for _, p := range refs {
		if math.Abs(lv.Price-p) <= band*atr5m {
			return true
		}
	}
	return false
}

// WeeklyShadowReorder computes the shadow view of the seated list: levels
// within the weekly confluence band get grade×mult. Returns the confluent
// count and how many of the top-N positions WOULD change under a stable
// re-sort by shadow score (a reorder count > 0 = the shadow seating differs
// from the real seating). PURE VIEW: the input is never mutated, nothing is
// returned that could re-enter the real seating path.
func WeeklyShadowReorder(levels []WeeklyShadowLevel, refs []float64, band, atr5m, mult float64) (confluent, reorder int) {
	if len(levels) == 0 {
		return 0, 0
	}
	scored := make([]float64, len(levels))
	for i, lv := range levels {
		s := float64(GradeRank(lv.Grade))
		if WeeklyConfluent(lv, refs, band, atr5m) {
			confluent++
			s *= mult
		}
		scored[i] = s
	}
	// stable re-rank by shadow score desc; count index changes vs the real order.
	order := make([]int, len(levels))
	for i := range order {
		order[i] = i
	}
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && scored[order[j]] > scored[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	for i, idx := range order {
		if idx != i {
			reorder++
		}
	}
	return confluent, reorder
}

// WeeklyCounterClauses renders the ⚖️ WEEKLY-COUNTER annotation clauses for an
// entry opposing the weekly bias — ONLY the clauses that would have changed
// the trade under the hypothetical Sep-9 hard rules. Aligned entries and
// neutral/low-conviction docs return nil (silent). grade: the scenario/level
// grade ("A"|"A+"|"B"|"C"…); rr: the planned risk:reward (0 = unknown → no
// RR clause).
func WeeklyCounterClauses(bias, conviction, side, grade string, rr float64) []string {
	bias = strings.ToLower(strings.TrimSpace(bias))
	conviction = strings.ToLower(strings.TrimSpace(conviction))
	if bias != "bull" && bias != "bear" {
		return nil // neutral weekly → nothing to oppose
	}
	if conviction != "med" && conviction != "high" {
		return nil // low conviction → the hypothetical rules would not fire
	}
	side = strings.ToLower(strings.TrimSpace(side))
	aligned := (bias == "bull" && side == "long") || (bias == "bear" && side == "short")
	if side != "long" && side != "short" {
		return nil // non-entry → silent
	}
	if aligned {
		return nil // aligned entry → silent
	}
	var clauses []string
	clauses = append(clauses, "would-halve-size")
	g := strings.ToUpper(strings.TrimSpace(grade))
	if g != "A" && g != "A+" {
		clauses = append(clauses, "would-require-A-grade")
	}
	if rr > 0 && rr < 4.0 {
		clauses = append(clauses, "would-need-RR≥4.0")
	}
	return clauses
}

// WeeklyDrawAlignTags returns the draw-alignment tag for a decision row:
// toward_draw | away | neutral. Longs with the draw above the entry are
// toward_draw; shorts with the draw below are toward_draw; anything else
// with an active directional weekly doc is away; neutral/no-doc → neutral.
func WeeklyDrawAlignTag(bias, side string, drawPx, entry float64) string {
	bias = strings.ToLower(strings.TrimSpace(bias))
	if (bias != "bull" && bias != "bear") || drawPx <= 0 || entry <= 0 {
		return "neutral"
	}
	side = strings.ToLower(strings.TrimSpace(side))
	switch side {
	case "long":
		if entry < drawPx {
			return "toward_draw"
		}
		return "away"
	case "short":
		if entry > drawPx {
			return "toward_draw"
		}
		return "away"
	}
	return "neutral"
}
