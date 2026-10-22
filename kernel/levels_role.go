package kernel

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"vl/market"
)

// ADDENDUM (1) — ROLE GRAMMAR (same wave as Pack B, 2026-08-26).
// Machine-assigned level_role per kind; the AI never chooses a role. Roles tell
// BOTH prompts what a level IS for: a VWAP is a magnet, ONH is liquidity to
// break, a fresh zone is a first-touch reaction reference, a consumed level is
// a target only. Env-overridable via LEVEL_ROLE_MAP ("VWAP=magnet_meanrevert,
// POC=liquidity_break,...") — partial overrides merge over the defaults.

// LevelRole is the machine-assigned functional role of a seated level.
type LevelRole string

const (
	RoleMagnetMeanRevert LevelRole = "magnet_meanrevert" // VWAP/POC/nPOC/gap — price gravitates, fades/reversions
	RoleLiquidityBreak   LevelRole = "liquidity_break"   // ONH/ONL/EQH/EQL/IB edges — resting stops, break or sweep-reclaim
	RoleReactZone        LevelRole = "react_zone"        // PDH/PDL/VAH/VAL/fresh zones — first-touch reaction
	RoleTargetOnly       LevelRole = "target_only"       // consumed / 3rd-touch / far-HTF — never an entry trigger
	RolePivot            LevelRole = "pivot"             // PDC/SETT/MID-O/round — the auction's fulcrum
)

// defaultRoleMap is the spec grammar (dispatch addendum, 2026-08-26).
var defaultRoleMap = map[LevelKind]LevelRole{
	KindVWAP: RoleMagnetMeanRevert, KindEVWAP: RoleMagnetMeanRevert, KindPDVWAP: RoleMagnetMeanRevert,
	KindPOC: RoleMagnetMeanRevert, KindNPOC: RoleMagnetMeanRevert, KindGap: RoleMagnetMeanRevert,
	KindONH: RoleLiquidityBreak, KindONL: RoleLiquidityBreak, KindEQH: RoleLiquidityBreak, KindEQL: RoleLiquidityBreak,
	KindIBH: RoleLiquidityBreak, KindIBL: RoleLiquidityBreak, KindORH: RoleLiquidityBreak, KindORL: RoleLiquidityBreak,
	KindASH: RoleLiquidityBreak, KindASL: RoleLiquidityBreak, KindLDNH: RoleLiquidityBreak, KindLDNL: RoleLiquidityBreak,
	KindPDH: RoleReactZone, KindPDL: RoleReactZone, KindRTHH: RoleReactZone, KindRTHL: RoleReactZone,
	KindSWGH: RoleReactZone, KindSWGL: RoleReactZone,
	KindVAH: RoleReactZone, KindVAL: RoleReactZone,
	KindPWH: RoleReactZone, KindPWL: RoleReactZone, KindPMH: RoleReactZone, KindPML: RoleReactZone,
	KindSupply: RoleReactZone, KindDemand: RoleReactZone, KindFVG: RoleReactZone, KindIFVG: RoleReactZone, KindOB: RoleReactZone,
	KindPDC: RolePivot, KindSETT: RolePivot, KindMIDO: RolePivot, KindRound: RolePivot,
}

var roleOverrides map[LevelKind]LevelRole

// RoleLegend is the 5-line playbook legend appended to BOTH prompts (addendum
// 1, 2026-08-26). Machine facts about what each role MEANS — the AI keeps its
// judgment on direction/timing.
const RoleLegend = "role playbook: magnet_meanrevert = price gravitates back (fade/reversion setups) · " +
	"liquidity_break = resting stops (break or sweep-reclaim, never a blind fade) · " +
	"react_zone = first-touch reaction reference (reject/hold evidence) · " +
	"target_only = take-profit / invalid reference only, NEVER an entry trigger · " +
	"pivot = the auction's fulcrum (bias flips measured against it).\n"

// ApplyRoleMapOverrides merges a LEVEL_ROLE_MAP env string over the default
// grammar (call once at boot; malformed entries are ignored, never fatal). An
// EMPTY string clears all overrides (test/boot reset).
func ApplyRoleMapOverrides(raw string) {
	if strings.TrimSpace(raw) == "" {
		roleOverrides = nil
		return
	}
	m := parseRoleMap(raw)
	if len(m) == 0 {
		return
	}
	roleOverrides = m
}

// parseRoleMap parses "KIND=role,KIND=role" → map.
func parseRoleMap(raw string) map[LevelKind]LevelRole {
	out := map[LevelKind]LevelRole{}
	for _, pair := range strings.Split(raw, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) != 2 {
			continue
		}
		kind := strings.TrimSpace(kv[0])
		if kind == "" || !strings.EqualFold(kind, strings.ToUpper(kind)) {
			kind = strings.ToUpper(kind)
		}
		role := strings.TrimSpace(kv[1])
		var lr LevelRole
		switch strings.ToLower(role) {
		case "magnet", "magnet_meanrevert":
			lr = RoleMagnetMeanRevert
		case "liquidity", "liquidity_break":
			lr = RoleLiquidityBreak
		case "react", "react_zone":
			lr = RoleReactZone
		case "target", "target_only":
			lr = RoleTargetOnly
		case "pivot":
			lr = RolePivot
		default:
			continue
		}
		out[LevelKind(kind)] = lr
	}
	return out
}

// RoleFor assigns a level's role: base role by kind (env-overridable), then
// the spec's state overrides — consumed/3rd-touch/far-HTF → target_only.
// `fresh` is the persisted freshness grade ("" = fresh).
func RoleFor(l DetectedLevel, fresh string) LevelRole {
	switch strings.ToLower(strings.TrimSpace(fresh)) {
	case "done", "consumed":
		return RoleTargetOnly // consumed → role-flipped, target only
	}
	// far-HTF (spec: target_only) applies to HTF CONTEXT marks — continuation
	// zones only. Volume-family HTF rows (nPOC·wk is HTF per naked_poc.go) keep
	// their base role: the spec lists nPOC under magnet_meanrevert explicitly.
	if l.HTF && isZoneKind(l.Kind) && l.ZonePattern != "reversal" {
		return RoleTargetOnly
	}
	base, ok := defaultRoleMap[l.Kind]
	if roleOverrides != nil {
		if o, ok2 := roleOverrides[l.Kind]; ok2 {
			base = o
		}
	}
	if !ok {
		base = RoleReactZone
	}
	return base
}

// touchCount counts how many times price traded AT OR THROUGH `price` in the
// closed bars (a bar touches when its range covers the price).
func touchCount(bars []market.Kline, price float64) int {
	n := 0
	for _, b := range bars {
		if b.Low <= price && b.High >= price {
			n++
		}
	}
	return n
}

// IsRoleOverridden reports whether env overrides are active (boot line).
func IsRoleOverridden() bool { return len(roleOverrides) > 0 }

// ── ADDENDUM (2) — BIAS-CONTEXT line (facts only, AI judges) ───────────────

// BiasContext is the per-cycle market-facts block for BOTH prompts: price vs
// VWAP / PDC, value-area position, nearest magnet, nearest liquidity. Facts
// only — no judgment, no recommendation.
type BiasContext struct {
	Price            float64
	VWAP             float64 // 0 = n/a
	PDC              float64 // 0 = n/a
	PDH, PDL         float64 // prior-day anchors; 0 = n/a (the BIAS-TREE deals in these)
	VAH, VAL         float64 // value area; 0,0 = n/a
	NearestMagnet    string
	NearestLiquidity string
}

// ComputeBiasContext builds the facts block from the closed bars + the scored
// candidate pool. Pure.
func ComputeBiasContext(bars []market.Kline, scored []ScoredLevel, now time.Time) BiasContext {
	bc := BiasContext{}
	cb := closedBars(bars, now)
	if len(cb) > 0 {
		bc.Price = cb[len(cb)-1].Close
	}
	if v, _ := vwapAndStdev(cb); v > 0 {
		bc.VWAP = v
	}
	for _, l := range scored {
		switch l.Kind {
		case KindPDC:
			if bc.PDC <= 0 {
				bc.PDC = l.Price
			}
		case KindPDH:
			if bc.PDH <= 0 {
				bc.PDH = l.Price
			}
		case KindPDL:
			if bc.PDL <= 0 {
				bc.PDL = l.Price
			}
		}
	}
	prof := profileLevels(cb, "", "")
	if len(prof) >= 3 {
		bc.VAH, bc.VAL = prof[1].Price, prof[2].Price
		if bc.VAH < bc.VAL {
			bc.VAH, bc.VAL = bc.VAL, bc.VAH
		}
	}
	if bc.Price <= 0 {
		return bc
	}
	// Nearest magnet / liquidity from the scored pool by role, nearest-first.
	type cand struct {
		label string
		dist  float64
	}
	var magnets, liquid []cand
	for _, l := range scored {
		d := l.Price - bc.Price
		switch RoleFor(l.DetectedLevel, l.Fresh) {
		case RoleMagnetMeanRevert:
			magnets = append(magnets, cand{l.Label, d})
		case RoleLiquidityBreak:
			liquid = append(liquid, cand{l.Label, d})
		}
	}
	nearest := func(cs []cand) string {
		if len(cs) == 0 {
			return "none"
		}
		sort.SliceStable(cs, func(i, j int) bool { return math.Abs(cs[i].dist) < math.Abs(cs[j].dist) })
		return fmt.Sprintf("%s (%+.1f)", cs[0].label, cs[0].dist)
	}
	bc.NearestMagnet = nearest(magnets)
	bc.NearestLiquidity = nearest(liquid)
	return bc
}

// ApplyUniverseDayAnchors (S-dispatch, 2026-08-27) fills PDH/PDL/PDC from the
// FULL detector universe when the seated table lacked them. The 17:00 CT
// session roll can leave the prior-day anchors outside the proximity band
// (live proof: the 17:46/19:02 ASIA reads rendered "no PDH/PDL anchor" — the
// prior-day PDH sat ~640pt above price and never seated). Day anchors are
// FACTS at any distance; seating must not gate them.
func ApplyUniverseDayAnchors(bc *BiasContext, all []DetectedLevel) {
	if bc == nil {
		return
	}
	for _, l := range all {
		switch l.Kind {
		case KindPDH:
			if bc.PDH <= 0 {
				bc.PDH = l.Price
			}
		case KindPDL:
			if bc.PDL <= 0 {
				bc.PDL = l.Price
			}
		case KindPDC:
			if bc.PDC <= 0 {
				bc.PDC = l.Price
			}
		}
	}
}

// VAState renders the value-area position ("" when no profile).
func (bc BiasContext) VAState() string {
	if bc.VAH <= 0 || bc.VAL <= 0 || bc.Price <= 0 {
		return ""
	}
	if bc.Price > bc.VAH {
		return "above value area"
	}
	if bc.Price < bc.VAL {
		return "below value area"
	}
	return "inside value area"
}

// Line renders the one-line bias_ctx facts block.
func (bc BiasContext) Line() string {
	var b strings.Builder
	fmt.Fprintf(&b, "bias_ctx: price %.2f", bc.Price)
	if bc.VWAP > 0 {
		fmt.Fprintf(&b, " · %.1f vs VWAP %.2f", bc.Price-bc.VWAP, bc.VWAP)
	} else {
		b.WriteString(" · VWAP n/a")
	}
	if bc.PDC > 0 {
		fmt.Fprintf(&b, " · %.1f vs PDC %.2f", bc.Price-bc.PDC, bc.PDC)
	} else {
		b.WriteString(" · PDC n/a")
	}
	if va := bc.VAState(); va != "" {
		fmt.Fprintf(&b, " · %s (%.2f–%.2f)", va, bc.VAL, bc.VAH)
	} else {
		b.WriteString(" · value area n/a")
	}
	fmt.Fprintf(&b, " · nearest magnet %s", bc.NearestMagnet)
	fmt.Fprintf(&b, " · nearest liquidity %s", bc.NearestLiquidity)
	return b.String()
}

var _ = os.Getenv // reserved: LEVEL_ROLE_MAP read at boot via ApplyRoleMapOverrides

// RoleForLabel resolves a role from a canonical display label (best-effort by
// prefix) — used at the write site where only PlanLevel labels exist.
func RoleForLabel(label string) LevelRole {
	return RoleFor(DetectedLevel{Kind: KindForLabel(label)}, "")
}

// KindForLabel reverse-maps a canonical display label to its LevelKind
// (best-effort prefix match; unknown → KindRound which is inert). Used by the
// level_stats nightly job to bucket rows into confluence families.
func KindForLabel(label string) LevelKind {
	l := strings.ToUpper(strings.TrimSpace(label))
	// m265: "PDC·last print" / "SETT·last print" carry their structural family
	// in the leading token — split at "·" (as structuralPrefix does) so they
	// resolve to KindPDC/KindSETT instead of the inert KindRound.
	if i := strings.Index(l, "·"); i > 0 {
		l = strings.TrimSpace(l[:i])
	}
	switch {
	case strings.HasPrefix(l, "VWAP+"), strings.HasPrefix(l, "VWAP−"), strings.HasPrefix(l, "VWAP"):
		return KindVWAP
	case strings.HasPrefix(l, "EVWAP"):
		return KindEVWAP
	case strings.HasPrefix(l, "PDVWAP"):
		return KindPDVWAP
	case strings.HasPrefix(l, "NPOC"):
		return KindNPOC
	case strings.HasPrefix(l, "POC"):
		return KindPOC
	case l == "VAH":
		return KindVAH
	case l == "VAL":
		return KindVAL
	case l == "SETT":
		return KindSETT
	case l == "MID-O":
		return KindMIDO
	case l == "PDH":
		return KindPDH
	case l == "PDL":
		return KindPDL
	case l == "PDC":
		return KindPDC
	case strings.HasPrefix(l, "RTH-H"):
		return KindRTHH
	case strings.HasPrefix(l, "RTH-L"):
		return KindRTHL
	case l == "ONH":
		return KindONH
	case l == "ONL":
		return KindONL
	case strings.HasPrefix(l, "EQH"):
		return KindEQH
	case strings.HasPrefix(l, "EQL"):
		return KindEQL
	case strings.HasPrefix(l, "IB-H"):
		return KindIBH
	case strings.HasPrefix(l, "IB-L"):
		return KindIBL
	case strings.HasPrefix(l, "OR-H"):
		return KindORH
	case strings.HasPrefix(l, "OR-L"):
		return KindORL
	case strings.HasPrefix(l, "AS-H"):
		return KindASH
	case strings.HasPrefix(l, "AS-L"):
		return KindASL
	case strings.HasPrefix(l, "LDN-H"):
		return KindLDNH
	case strings.HasPrefix(l, "LDN-L"):
		return KindLDNL
	case l == "PWH":
		return KindPWH
	case l == "PWL":
		return KindPWL
	case l == "PMH":
		return KindPMH
	case l == "PML":
		return KindPML
	case strings.HasPrefix(l, "GAP"):
		return KindGap
	case strings.HasPrefix(l, "RN"):
		return KindRound
	case strings.HasPrefix(l, "SUPPLY"):
		return KindSupply
	case strings.HasPrefix(l, "DEMAND"):
		return KindDemand
	case strings.HasPrefix(l, "IFVG"):
		return KindIFVG
	case strings.HasPrefix(l, "FVG"):
		return KindFVG
	case strings.HasPrefix(l, "OB"):
		return KindOB
	case strings.HasPrefix(l, "SWG-H"):
		return KindSWGH
	case strings.HasPrefix(l, "SWG-L"):
		return KindSWGL
	default:
		return KindRound
	}
}

// RoleMismatches (addendum 1, validator WARN — never a fail) — scans the plan's
// scenarios against the ROLE of the plan level each confirm{} ref_price cites:
// a magnet_meanrevert level used as a breakout/acceptance trigger, or a
// liquidity_break level faded with a plain reject, are role-vs-scenario
// mismatches. Returns human-readable lines for the journal.
func RoleMismatches(doc *PlanDoc) []string {
	if doc == nil || len(doc.Scenarios) == 0 || len(doc.Levels) == 0 {
		return nil
	}
	roleAt := func(price float64) (LevelRole, string) {
		best := -1
		bd := math.MaxFloat64
		for i, l := range doc.Levels {
			d := math.Abs(l.Price - price)
			if d < bd {
				bd, best = d, i
			}
		}
		if best < 0 || bd > 3.0 {
			return "", ""
		}
		return RoleForLabel(doc.Levels[best].Label), doc.Levels[best].Label
	}
	var out []string
	for _, sc := range doc.Scenarios {
		// FVG ENTRY MODEL (2026-08-26) — an fvg_entry anchored to a
		// liquidity_break origin gets the SAME sweep-reclaim caution WARN as
		// other plays (no exemption): the origin is a liquidity pool, the gap
		// entry is a fade of that pool.
		if strings.EqualFold(strings.TrimSpace(sc.Condition), "fvg_entry") && sc.Fvg != nil {
			if RoleForLabel(sc.Fvg.OriginLevel) == RoleLiquidityBreak {
				out = append(out, fmt.Sprintf("S%s fvg_entry anchored on %s (liquidity_break) — sweep-reclaim caution: enter only on a confirmed reclaim close, never the wick alone",
					sc.ID, sc.Fvg.OriginLevel))
			}
			continue
		}
		if sc.Confirm == nil || sc.Confirm.RefPrice <= 0 {
			continue
		}
		role, label := roleAt(sc.Confirm.RefPrice)
		if role == "" {
			continue
		}
		cond := strings.ToLower(strings.TrimSpace(sc.Condition))
		switch {
		case role == RoleMagnetMeanRevert && (cond == "breakout_retest" || cond == "acceptance"):
			out = append(out, fmt.Sprintf("S%s cites %s (%s) but uses condition %q — a magnet is a reversion reference, not a breakout trigger",
				sc.ID, label, role, sc.Condition))
		case role == RoleLiquidityBreak && cond == "reject":
			out = append(out, fmt.Sprintf("S%s fades %s (%s) with a plain reject — liquidity fades need sweep_reclaim (wick through + close back)",
				sc.ID, label, role))
		case role == RolePivot && (cond == "breakout_retest" || cond == "acceptance"):
			out = append(out, fmt.Sprintf("S%s cites %s (%s) with condition %q — pivots are bias references; breakout triggers belong to liquidity levels",
				sc.ID, label, role, sc.Condition))
		case role == RoleTargetOnly:
			out = append(out, fmt.Sprintf("S%s uses %s (%s) as its confirm reference — target_only levels are never entry triggers",
				sc.ID, label, role))
		}
	}
	return out
}
