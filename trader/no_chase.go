package trader

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"vl/kernel"
	"vl/store"
)

// ── NO-CHASE (2026-09-02) — entries must be near the level they cite ────────
// Evidence, 09-02 losses forensic: four longs, four stops, −$381.50, on a day
// the market went UP (29048 → 29192 by 09:41). The BIAS was right. Positions
// 587/588 entered near 29080 and were stopped on a 30-pt dip; price then ran
// 140 pts without the bot. Positions 589 and 590 entered at 29192.50 and
// 29193.25 — AFTER that rally, at the local top — and were stopped on 50-80 pt
// pullbacks. Their MFE was 10.25 and 1.00: they never went in favour at all.
//
// The entry gate measures R:R at the fill and refuses a late entry whose
// TARGET has shrunk. It does not ask how far the entry sits from the level the
// scenario cites, nor how far price has already travelled since that level was
// touched. A chase with a still-plausible R:R passes every existing leg.
//
// WARN-FIRST BY DESIGN (A24): this leg refuses NOTHING. It measures, logs and
// counts. A week of counts is the research; the owner then rules on promotion
// against the criterion pre-registered in the report.

// NoChaseInputs are the measurements the leg needs. Zero values mean "not
// known" and the leg abstains rather than inventing a distance.
type NoChaseInputs struct {
	Entry       float64
	CitedLevel  float64 // the price of the level the scenario cites (0 = none)
	LevelKind   string  // provenance label of that level ("" = none)
	LastTouchPx float64 // price of the level's most recent touch (0 = never touched)
	HasTouch    bool
	ATR5m       float64
	MinSLMult   float64 // the SAME multiplier the stop floor uses
	// R22 P2 (MATH-PLANNER §4) — the ARM path measures from the LIVE price at
	// the pass that places it: the authored entry vs the authored confirm ref
	// is a constant of the plan (0.00×ATR for every reject-at-level arm), never
	// a chase. LiveRequired with LivePx ≤ 0 → abstain (counted no_live_price).
	LivePx       float64
	LiveRequired bool
}

// NoChaseVerdict is the leg's measurement and its opinion.
// OWNER RULING 2026-09-02: BOTH measures are recorded on every entry and the
// verdict is their OR — the week of counts decides which carries the signal,
// which is the point of not choosing now.
type NoChaseVerdict struct {
	Applicable bool // false = no cited level; every field NULL, counted separately
	DistPts    float64
	DistATR    float64
	// RunKnown false → RunPts is NULL, not zero. A zero run means "entered AT
	// the touch"; an unknown run means "we could not tell". Conflating them
	// would make the week's counts unreadable (A24: no plausible zero).
	RunKnown    bool
	RunPts      float64
	MaxDistATR  float64
	MaxRunPts   float64
	WouldRefuse bool
	DistFired   bool // which half fired — the week's table needs them apart
	RunFired    bool
	Why         string
	// R22 P2: MeasuredFrom names the price the distances were taken from
	// ("live" on the arm path, "entry" on the decision path); LiveUnknown =
	// the arm path had no live price, so nothing was measured.
	MeasuredFrom string
	LiveUnknown  bool
	// Enforced (release #25 PK-3) — the arm-path WOULD_REFUSE was ENFORCED
	// (planner_no_chase_enforce on): the line says REFUSED, not PROCEEDING.
	Enforced bool
}

// NoChaseMaxDistATR — NOCHASE_MAX_DIST_ATR, default 1.0.
//
// BELIEF LABEL [I] PROVISIONAL. This number is derived from NOTHING yet. It is
// a starting point chosen so the leg fires often enough to produce counts in a
// week; the counts are the research, and the owner rules afterwards. Do not
// cite it as a finding.
func NoChaseMaxDistATR() float64 {
	if v := strings.TrimSpace(os.Getenv("NOCHASE_MAX_DIST_ATR")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 10 {
			return f
		}
	}
	return 1.0
}

// noChaseMaxRunPts resolves the run ceiling from the SAME source as the stop
// floor: a chase longer than the stop is a stop with no target left. Falls back
// to the dist ceiling when no stop multiplier is resolved.
func noChaseMaxRunPts(in NoChaseInputs) float64 {
	if v := strings.TrimSpace(os.Getenv("NOCHASE_MAX_RUN_PTS")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	mult := in.MinSLMult
	if mult <= 0 {
		mult = kernel.MinSLATRMult()
	}
	if mult <= 0 || in.ATR5m <= 0 {
		return 0 // unknown — the run test abstains
	}
	return mult * in.ATR5m
}

// EvaluateNoChase is the pure leg. It never refuses; it only measures.
func EvaluateNoChase(in NoChaseInputs) NoChaseVerdict {
	v := NoChaseVerdict{MaxDistATR: NoChaseMaxDistATR(), MaxRunPts: noChaseMaxRunPts(in)}
	if in.Entry <= 0 || in.CitedLevel <= 0 {
		return v // not applicable — no level cited; the caller counts this case
	}
	px := in.Entry
	v.MeasuredFrom = "entry"
	if in.LiveRequired {
		if in.LivePx <= 0 {
			v.LiveUnknown = true
			return v // live price unknown — nothing measured, never a plausible zero
		}
		px, v.MeasuredFrom = in.LivePx, "live"
	}
	v.Applicable = true
	v.DistPts = math.Abs(px - in.CitedLevel)
	if in.ATR5m > 0 {
		v.DistATR = v.DistPts / in.ATR5m
	}
	if in.HasTouch && in.LastTouchPx > 0 {
		v.RunKnown = true
		v.RunPts = math.Abs(px - in.LastTouchPx)
	}
	var why []string
	if v.DistATR > 0 && v.DistATR > v.MaxDistATR {
		v.DistFired = true
		why = append(why, fmt.Sprintf("dist %.2f×ATR > %.2f", v.DistATR, v.MaxDistATR))
	}
	if v.RunKnown && v.MaxRunPts > 0 && v.RunPts > v.MaxRunPts {
		v.RunFired = true
		why = append(why, fmt.Sprintf("run %.1fpts > %.1f (%.1f×ATR5m stop floor)", v.RunPts, v.MaxRunPts, in.MinSLMult))
	}
	// OR — either half is enough (owner ruling 2026-09-02). A row whose run is
	// unmeasurable is judged on dist alone; it is never given the benefit of an
	// unmeasured half.
	if len(why) > 0 {
		v.WouldRefuse = true
		v.Why = strings.Join(why, " OR ")
	}
	return v
}

// NoChaseLine renders the WARN (pure — fixture-pinned). It always says the
// entry PROCEEDED, so no reader can mistake this wave for a refusal.
func NoChaseLine(path, scenario string, v NoChaseVerdict) string {
	run := "NULL(no recorded touch)"
	if v.RunKnown {
		run = fmt.Sprintf("%.1fpts", v.RunPts)
	}
	from := ""
	if v.MeasuredFrom == "live" {
		from = " from live price"
	}
	if v.Enforced {
		return fmt.Sprintf("🚫 no-chase REFUSED %s %s: dist=%.1fpts/%.2f×ATR%s run=%s (%s) — ENFORCED (planner_no_chase_enforce on), entry REFUSED",
			path, scenario, v.DistPts, v.DistATR, from, run, v.Why)
	}
	return fmt.Sprintf("🚫 no-chase WOULD_REFUSE %s %s: dist=%.1fpts/%.2f×ATR%s run=%s (%s) — WARN-first, entry PROCEEDING",
		path, scenario, v.DistPts, v.DistATR, from, run, v.Why)
}

// NoChaseBootLine is the boot line, every field READ from its resolver.
func NoChaseBootLine() string {
	return fmt.Sprintf("🚫 no-chase: max_dist=%.2f×ATR max_run=%.1f×ATR5m(stop floor) mode=warn counters=on [I] PROVISIONAL — the numbers are a starting point, the week of counts is the research",
		NoChaseMaxDistATR(), kernel.MinSLATRMult())
}

// NoChaseCounterKey is the per-path recorded counter (system_config).
func NoChaseCounterKey(path, outcome string) string {
	return "nochase_" + strings.TrimSpace(path) + "_" + strings.TrimSpace(outcome)
}

// noChaseObserver returns the OnNoChase callback for one path: it logs the WARN
// when the leg would refuse and RECORDS the outcome per path (class-35 lesson —
// a log-only tally evaporates at the next boot).
func (at *AutoTrader) noChaseObserver(path, scenario string) func(NoChaseVerdict) {
	return func(v NoChaseVerdict) {
		outcome := "ok"
		switch {
		case v.LiveUnknown:
			outcome = "no_live_price"
			at.logInfof("🚫 no-chase: %s %s has no live price at this pass — distance UNKNOWN, not counted as ok", path, scenario)
		case !v.Applicable:
			outcome = "no_level"
			at.logInfof("🚫 no-chase: %s %s cites no level price — distance UNKNOWN, not counted as ok", path, scenario)
		case v.WouldRefuse:
			outcome = "would_refuse"
			at.logWarnf("%s", NoChaseLine(path, scenario, v))
		}
		if at.store == nil {
			return
		}
		bump := func(k string) {
			if _, err := store.IncSystemCounter(at.store, k); err != nil {
				at.logWarnf("🚫 no-chase counter write failed: %v", err)
			}
		}
		bump(NoChaseCounterKey(path, outcome))
		// The week's table needs the halves apart, and needs to know how often
		// run was unmeasurable — otherwise "dist carried the signal" cannot be
		// told from "run was never available".
		if v.Applicable && !v.RunKnown {
			bump(NoChaseCounterKey(path, "run_null"))
			at.logInfof("🚫 no-chase: %s %s has no recorded touch — run stored NULL, judged on dist alone (%.2f×ATR)", path, scenario, v.DistATR)
		}
		if v.DistFired {
			bump(NoChaseCounterKey(path, "dist_fired"))
		}
		if v.RunFired {
			bump(NoChaseCounterKey(path, "run_fired"))
		}
	}
}

// citedLevelFor resolves the price of the level a scenario cites, preferring
// the confirm's ref_price (the price its closes are counted against — the most
// precise statement of "the level this play is about") and falling back to the
// plan level nearest that reference. Returns 0 when the scenario names none:
// the leg abstains rather than guessing which level was meant.
func citedLevelFor(sc kernel.PlanScenario, plan *kernel.ActivePlan) (float64, string) {
	px := 0.0
	if sc.Confirm != nil && sc.Confirm.RefPrice > 0 {
		px = sc.Confirm.RefPrice
	}
	if px <= 0 {
		return 0, ""
	}
	kind := ""
	if plan != nil {
		best := math.MaxFloat64
		for _, l := range plan.Doc.Levels {
			if d := math.Abs(l.Price - px); d < best {
				best, kind = d, l.Label
			}
		}
		if best > 1.0 { // no plan level within a point of the reference
			kind = "confirm_ref"
		}
	}
	if kind == "" {
		kind = "confirm_ref"
	}
	return px, kind
}

// lastTouchFor returns the price of the most recent recorded touch of a level,
// and whether one exists. No touch → the run test abstains; it never invents a
// touch price.
func (at *AutoTrader) lastTouchFor(levelPx float64) (float64, bool) {
	if levelPx <= 0 || at.store == nil || at.store.TouchEpisodes() == nil {
		return 0, false
	}
	n, err := at.store.TouchEpisodes().CountForLevel(at.id, kernel.CMESessionDayKey(time.Now()), "", levelPx)
	if err != nil || n == 0 {
		return 0, false
	}
	// A touch happens AT the level: the touched price IS the level price. The
	// run measures how far price has travelled from there since.
	return levelPx, true
}
