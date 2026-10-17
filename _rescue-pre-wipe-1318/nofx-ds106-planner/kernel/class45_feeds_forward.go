package kernel

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"nofx/market"
)

// ── CLASS 45 (2026-09-02) — THE PROMPT FEEDS FORWARD WHAT THE VALIDATOR KNOWS ─
//
// LONDON 2026-09-02 (planner_rejected_prompts 92/93/94), measured:
//   · 01:32:54 attempt 1 — S1 breakdown_continue at 29021.25 → "a close came
//     back across 29021.25 — the breakdown is void"
//   · 01:35:04 attempt 2 — the repair obeyed the hint, killed by fade_requires_touch
//   · 01:37:44 attempt 3 — a FULL re-author put breakdown_continue back at the
//     SAME 29021.25 → void again → fail-closed, session lost
//
// The standing order sat at 70% depth; the correction was the last 239 chars
// (59 of 6,602 tokens, under 1%, at 99% depth) and described attempt 2's fade
// defect, not attempt 1's void; the facts block named 29021.25 six times and
// never once said it was dead. The model did what it was told.
//
// Everything here CLOSES that gap by carrying the validator's own knowledge
// forward into the prompt. The void verdict is not re-derived: it is the
// validator's own predicate (BreakdownContinueState), reached through a
// level-oriented entry point, so prompt and validator cannot hold two opinions.

// VoidBreakdownLevel is one level the write-site validator WOULD refuse a
// waterfall play at, because a close came back across it.
type VoidBreakdownLevel struct {
	Price         float64
	Short         bool   // true = breakdown (short side); false = breakup (long side)
	ReclaimedAtCT string // when the reclaiming close printed ("" = unknown)
}

// BreakdownLevelReclaimed runs THE VALIDATOR'S OWN predicate for one level by
// building the minimal scenario BreakdownContinueState expects. This is the
// single source of the void verdict — never a second implementation.
//
// The reclaim TIMESTAMP is derived separately (the state struct does not carry
// one); the VERDICT always comes from the predicate.
func BreakdownLevelReclaimed(level float64, short bool, bars []market.Kline, sinceMs, nowMs int64) (bool, string) {
	if level <= 0 || len(bars) == 0 {
		return false, ""
	}
	cond := "breakup_continue"
	if short {
		cond = "breakdown_continue"
	}
	sc := PlanScenario{
		ID: "probe", Condition: cond, Direction: map[bool]string{true: "short", false: "long"}[short],
		Breakdown: &PlanBreakdownContinue{Level: level, EntryMode: "pullback"},
	}
	st := BreakdownContinueState(sc, bars, sinceMs, nowMs)
	if !st.Reclaimed {
		return false, ""
	}
	if st.ReclaimedAt == nil {
		return true, ""
	}
	return true, FormatCT(time.UnixMilli(*st.ReclaimedAt))
}

// ComputeVoidBreakdownLevels runs the predicate over every ranked level, both
// sides, and returns those the validator would refuse. Deterministic order
// (price ascending) so the prompt text is stable across cycles.
// VOID PARITY (2026-09-02): the scope is resolved, not chosen by the caller —
// the same VoidScope the write-site validator reads.
func ComputeVoidBreakdownLevels(levels []ScoredLevel, scope VoidScope, nowMs int64) []VoidBreakdownLevel {
	bars, sinceMs := scope.Bars, scope.SinceMs
	var out []VoidBreakdownLevel
	seen := map[string]bool{}
	for _, l := range levels {
		if l.Price <= 0 {
			continue
		}
		for _, short := range []bool{true, false} {
			void, at := BreakdownLevelReclaimed(l.Price, short, bars, sinceMs, nowMs)
			if !void {
				continue
			}
			key := fmt.Sprintf("%.2f:%v", l.Price, short)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, VoidBreakdownLevel{Price: l.Price, Short: short, ReclaimedAtCT: at})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Price != out[j].Price {
			return out[i].Price < out[j].Price
		}
		return out[i].Short && !out[j].Short
	})
	return out
}

// RenderVoidBreakdownLevels is the facts-block line (E2). Empty when none —
// silence means "nothing is void", never an empty header.
func RenderVoidBreakdownLevels(v []VoidBreakdownLevel, seatedTotal int) string {
	if len(v) == 0 {
		return ""
	}
	// CHOP COLLAPSE (owner ruling 2026-09-03). Measured on the first live
	// read-facts row (00:00:56 CT, ASIA): void=18 across NINE levels, ALL nine
	// void on BOTH sides — eighteen lines to say "these nine are chop". The
	// class-53 session-day window bounded the LOOKBACK but not the crossing
	// frequency, so it could not fix this; the doubling is intra-session
	// oscillation. A level void both ways carries no direction, so it collapses
	// into one aggregated line. A ONE-SIDED void keeps its own line, because
	// side + reclaim time is real directional news.
	type agg struct {
		short, long         bool
		whenShort, whenLong string
	}
	byPrice := map[float64]*agg{}
	var order []float64
	for _, x := range v {
		a, ok := byPrice[x.Price]
		if !ok {
			a = &agg{}
			byPrice[x.Price] = a
			order = append(order, x.Price)
		}
		if x.Short {
			a.short, a.whenShort = true, x.ReclaimedAtCT
		} else {
			a.long, a.whenLong = true, x.ReclaimedAtCT
		}
	}
	var chop []string
	type oneSided struct {
		price float64
		side  string
		when  string
	}
	var singles []oneSided
	for _, p := range order {
		a := byPrice[p]
		switch {
		case a.short && a.long:
			chop = append(chop, fmt.Sprintf("%.2f", p))
		case a.short:
			singles = append(singles, oneSided{p, "breakdown", a.whenShort})
		default:
			singles = append(singles, oneSided{p, "breakup", a.whenLong})
		}
	}
	var b strings.Builder
	b.WriteString("## VOID breakdown levels (a close came back across since the break, THIS session day — the write-site validator REFUSES a waterfall play at these)\n")
	if len(chop) > 0 {
		seated := ""
		if seatedTotal > 0 {
			seated = fmt.Sprintf(" (%d of %d seated)", len(chop), seatedTotal)
		}
		fmt.Fprintf(&b, "- CHOP (broken and reclaimed both ways this session): %s%s — waterfall plays at these will be refused; prefer touch/fade plays there.\n",
			strings.Join(chop, " · "), seated)
	}
	for _, s := range singles {
		when := ""
		if s.when != "" {
			when = fmt.Sprintf(" (reclaimed %s)", s.when)
		}
		fmt.Fprintf(&b, "- %.2f %s%s\n", s.price, s.side, when)
	}
	b.WriteString("- do NOT author breakdown_continue or breakup_continue at these prices. Any other condition is legal there.\n\n")
	return b.String()
}

// RenderStopFloorLine is the facts-block line (E3): the floor the composer will
// enforce, from the SAME resolver it uses. On 2026-09-02 all 13 armed stops were
// widened because the planner was never told this number, and every widening
// silently cut the planned R:R toward the 2.0 arm gate.
func RenderStopFloorLine(atr5m, mult float64) string {
	if atr5m <= 0 || mult <= 0 {
		return ""
	}
	return fmt.Sprintf("## Minimum stop distance this cycle\n%.1f pts (%.1f×ATR5m %.2f, resolved). Stops tighter than this are WIDENED by the executor before the R:R gate sees them — author stops AND targets consistent with it, or your R:R will not survive the widening.\n\n",
		mult*atr5m, mult, atr5m)
}

// PromptFeedsForwardBootLine (E6) — every field READ, never a literal.
func PromptFeedsForwardBootLine(voidLevels int, atr5m, mult float64) string {
	// HONESTY (checklist class 49): at boot there are no bars, so the void list
	// is NOT "zero levels are void" — it is "not computed yet". A negative count
	// means uncomputed and prints as n/a; 0 means measured-and-empty. The floor
	// MULTIPLIER is known at boot even when ATR is not, so it is always stated.
	floor := fmt.Sprintf("%.1f×ATR5m (n/a — no ATR yet)", mult)
	if atr5m > 0 && mult > 0 {
		floor = fmt.Sprintf("%.1f×ATR5m=%.1fpts", mult, mult*atr5m)
	}
	levels := fmt.Sprintf("void-levels=%d", voidLevels)
	if voidLevels < 0 {
		levels = "void-levels=n/a (computed per read)"
	}
	// The waterfall floor rides the same line (owner ruling 2026-09-03): it is
	// enforced at write and was, until now, the one floor the author was never
	// shown. Same honesty rule — the multiplier is known at boot, the points
	// are not.
	disp := fmt.Sprintf("%.1f×ATR5m (n/a — no ATR yet)", bdMinDispATR())
	if atr5m > 0 {
		disp = fmt.Sprintf("%.1f×ATR5m=%.1fpts", bdMinDispATR(), bdMinDispATR()*atr5m)
	}
	return fmt.Sprintf("prompt feeds forward: %s · stop-floor=%s · waterfall-displacement-floor=%s (stated per level) · reject-block=top+tail (class 45)",
		levels, floor, disp)
}

// planDocGapDownMessage is the gap-down refusal text, exported here so the pin
// can assert the MESSAGE matches the RULE (which tests direction only).
func planDocGapDownMessage() string { return gapDownDirectionMessage }
