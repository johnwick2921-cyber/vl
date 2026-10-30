package store

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"vl/logger"
)

// ── PEP MODE — the root "pep" block (owner order 2026-10-29) ─────────────────
//
// pep is a trading mode SEPARATE from mentor mode: its own code (kernel/pep),
// its own settings (this block), and no mentor rule applies. A trader is exactly
// one of {AI/planner, mentor, pep}; TradingModeConflict names the illegal
// combinations and the strategy store refuses to write one.
//
// ROOT placement (sibling of day_plan / regime) so a grid switch, which drops
// ai_config, never drops it. Additive + OFF: a nil block emits no "pep" key and
// every existing strategy stays byte-identical.
//
// EVERY NUMBER IS A SETTING. Per-style blocks (style_a / style_e) keep each
// style's numbers apart so switching style never loses the other style's values.
// Fields are POINTERS wherever 0 / false is a legal value (fee $0, warm-up 0 =
// auto, max box 0 = off, pep off): a plain omitempty would erase an explicit 0.
//
// LEAF NAMES ARE UNIQUE (pep_*, pep_a_*, pep_e_*): the knob registry falls back
// on the leaf name and mentor already owns orb_* / ema* leaves.
//
// The numbers' source is the owner's PDF "A + E — Full calculation"
// (/home/hoang/fleet-1017/out/pep-spec-coa707.pdf); the box minutes differ per
// style on DATA evidence: the PDF's style-E ledger equals the first TWO minutes'
// range (cmd/pep-backtest, 2026-10-30).

// PepConfig is the root pep block as stored.
type PepConfig struct {
	// Enabled is the master switch. nil / false = OFF (shipped default).
	Enabled *bool `json:"pep_enabled,omitempty"`
	// Style is "A" (ORB breakout, enter immediately) or "E" (EMA9 ORB, candle-close).
	// Shipped E (owner 2026-10-30 "keep default E 6").
	Style string `json:"pep_style,omitempty"`
	// Contracts is the contract count (shipped 6, owner 2026-10-30 "keep default E 6"; hard ceiling PepContractsCeiling).
	Contracts *int `json:"pep_contracts,omitempty"`
	// BoxStartCT is the box start, "HH:MM" America/Chicago (shipped 08:30).
	BoxStartCT string `json:"pep_box_start_ct,omitempty"`
	// FlatTimeCT is the time exit, "HH:MM" America/Chicago (shipped 15:00).
	FlatTimeCT string `json:"pep_flat_time_ct,omitempty"`
	// TickSize is the instrument tick (shipped 0.25).
	TickSize *float64 `json:"pep_tick_size,omitempty"`
	// SLRounding is how a stop distance is rounded to a tick: "outward" (shipped,
	// the PDF's rule: away from entry) or "nearest".
	SLRounding string `json:"pep_sl_rounding,omitempty"`
	// MaxTradesPerDay caps entries per CT day (shipped 1).
	MaxTradesPerDay *int `json:"pep_max_trades_per_day,omitempty"`
	// FeePerContractRT is the round-trip fee per contract in USD (shipped 1.0).
	// BACKTEST / REPORTING ONLY: no order or gate reads it.
	FeePerContractRT *float64 `json:"pep_fee_per_contract_usd,omitempty"`
	// MaxBoxPoints refuses a box wider than this many points. 0 / absent = OFF
	// (shipped: the PDF's numbers trade every box).
	MaxBoxPoints *float64 `json:"pep_max_box_points,omitempty"`
	// SignalMaxAgeMin is how old a signal or the newest closed 1m bar may be
	// before it is refused as stale (shipped 2).
	SignalMaxAgeMin *int `json:"pep_signal_max_age_min,omitempty"`

	StyleA *PepStyleAConfig `json:"style_a,omitempty"`
	StyleE *PepStyleEConfig `json:"style_e,omitempty"`
}

// PepStyleAConfig is style A's own numbers.
type PepStyleAConfig struct {
	BoxMinutes *int     `json:"pep_a_box_minutes,omitempty"` // shipped 5
	SLFraction *float64 `json:"pep_a_sl_fraction,omitempty"` // of the box, shipped 1.0
	TPFraction *float64 `json:"pep_a_tp_fraction,omitempty"` // of the box, shipped 1.0
	BothEdges  string   `json:"pep_a_both_edges,omitempty"`  // one 1m bar through both edges: no_trade (shipped) | nearest_open
}

// PepStyleEConfig is style E's own numbers.
type PepStyleEConfig struct {
	BoxMinutes   *int     `json:"pep_e_box_minutes,omitempty"`   // shipped 2 (the PDF's "ORB 2 PHÚT")
	SLFraction   *float64 `json:"pep_e_sl_fraction,omitempty"`   // of the box, shipped 0.5
	TPFraction   *float64 `json:"pep_e_tp_fraction,omitempty"`   // of the box, shipped 1.0
	EMAPeriod    *int     `json:"pep_e_ema_period,omitempty"`    // shipped 9
	EMATFMin     *int     `json:"pep_e_ema_tf_min,omitempty"`    // candle minutes, shipped 2
	SignalSource string   `json:"pep_e_signal_source,omitempty"` // "ema" (owner ruling, shipped) | "close"
	WarmupBars   *int     `json:"pep_e_warmup_bars,omitempty"`   // 0 / absent = 3 x period
}

// Shipped values and vocabulary (the ONE spelling the resolver, kernel/pep, the
// effective rows, the boot line and the guide share).
const (
	PepStyleA = "A"
	PepStyleE = "E"

	PepSourceEMA   = "ema"
	PepSourceClose = "close"

	PepBothEdgesNoTrade     = "no_trade"
	PepBothEdgesNearestOpen = "nearest_open"

	PepSLRoundOutward = "outward"
	PepSLRoundNearest = "nearest"

	// PepStyleDefault / PepContractsDefault are the owner's shipped defaults
	// (2026-10-30: "keep default E 6"). Written as literals so the Studio and the
	// guide tests can read them back from this file.
	PepStyleDefault     = "E"
	PepContractsDefault = 6
	// PepContractsCeiling is the HARD maximum (SIM only; a bigger saved number is
	// clamped and logged, never honoured).
	PepContractsCeiling = 10

	PepBoxStartDefault     = "08:30"
	PepFlatTimeDefault     = "15:00"
	PepTickDefault         = 0.25
	PepMaxTradesDefault    = 1
	PepFeeDefault          = 1.0
	PepSignalMaxAgeDefault = 2
	PepABoxMinutesDefault  = 5
	PepASLFractionDefault  = 1.0
	PepATPFractionDefault  = 1.0
	PepEBoxMinutesDefault  = 2
	PepESLFractionDefault  = 0.5
	PepETPFractionDefault  = 1.0
	PepEEMAPeriodDefault   = 9
	PepEEMATFDefault       = 2
	PepMaxTradesCeiling    = 10
	PepSignalMaxAgeCeiling = 30
	PepBoxMinutesCeiling   = 120
	PepFractionCeiling     = 10.0
	PepEMAPeriodFloor      = 2
	PepEMAPeriodCeiling    = 200
	PepWarmupCeiling       = 5000
)

// PepResolved is every pep number as the bot RUNS it: shipped defaults filled,
// vocabulary canonicalised, out-of-range clamped. Clamps names each leaf that
// did not take effect as saved and why (the repo's no-silent-clamp rule).
type PepResolved struct {
	Enabled          bool
	Style            string
	Contracts        int
	BoxStartCT       string
	BoxStartMin      int
	FlatTimeCT       string
	FlatMin          int
	TickSize         float64
	SLRounding       string
	MaxTradesPerDay  int
	FeePerContractRT float64
	MaxBoxPoints     float64
	SignalMaxAgeMin  int

	A PepResolvedA
	E PepResolvedE

	// Clamps maps a leaf name to the sentence saying what was saved, what runs
	// and why. Empty when every saved value took effect as saved.
	Clamps map[string]string
}

// PepResolvedA is style A resolved.
type PepResolvedA struct {
	BoxMinutes int
	SLFraction float64
	TPFraction float64
	BothEdges  string
}

// PepResolvedE is style E resolved.
type PepResolvedE struct {
	BoxMinutes   int
	SLFraction   float64
	TPFraction   float64
	EMAPeriod    int
	EMATFMin     int
	SignalSource string
	WarmupBars   int
}

// PepEnabled is the master switch. nil config, nil block or nil flag = OFF.
func (c *StrategyConfig) PepEnabled() bool {
	return c != nil && c.Pep != nil && c.Pep.Enabled != nil && *c.Pep.Enabled
}

// ResolvePep is the ONE resolver of the pep block. Every consumer (kernel/pep's
// FromSettings, the effective rows, the boot line) reads this; none re-derives a
// default. A nil config or block resolves the shipped defaults with pep OFF.
func ResolvePep(c *StrategyConfig) PepResolved {
	var p *PepConfig
	if c != nil {
		p = c.Pep
	}
	if p == nil {
		p = &PepConfig{}
	}
	sa, se := p.StyleA, p.StyleE
	if sa == nil {
		sa = &PepStyleAConfig{}
	}
	if se == nil {
		se = &PepStyleEConfig{}
	}
	r := PepResolved{Clamps: map[string]string{}}
	r.Enabled = p.Enabled != nil && *p.Enabled

	r.Style = pepVocab(r.Clamps, "pep_style", p.Style, strings.ToUpper, []string{PepStyleA, PepStyleE}, PepStyleDefault)
	r.Contracts = pepInt(r.Clamps, "pep_contracts", p.Contracts, PepContractsDefault, 1, PepContractsCeiling)
	r.SLRounding = pepVocab(r.Clamps, "pep_sl_rounding", p.SLRounding, strings.ToLower, []string{PepSLRoundOutward, PepSLRoundNearest}, PepSLRoundOutward)
	r.TickSize = pepPosFloat(r.Clamps, "pep_tick_size", p.TickSize, PepTickDefault, 1000)
	r.MaxTradesPerDay = pepInt(r.Clamps, "pep_max_trades_per_day", p.MaxTradesPerDay, PepMaxTradesDefault, 1, PepMaxTradesCeiling)
	r.FeePerContractRT = pepFloat(r.Clamps, "pep_fee_per_contract_usd", p.FeePerContractRT, PepFeeDefault, 0, 1000)
	r.MaxBoxPoints = pepFloat(r.Clamps, "pep_max_box_points", p.MaxBoxPoints, 0, 0, 100000)
	r.SignalMaxAgeMin = pepInt(r.Clamps, "pep_signal_max_age_min", p.SignalMaxAgeMin, PepSignalMaxAgeDefault, 1, PepSignalMaxAgeCeiling)

	r.BoxStartCT, r.BoxStartMin = pepClock(r.Clamps, "pep_box_start_ct", p.BoxStartCT, PepBoxStartDefault)
	r.FlatTimeCT, r.FlatMin = pepClock(r.Clamps, "pep_flat_time_ct", p.FlatTimeCT, PepFlatTimeDefault)
	if r.FlatMin <= r.BoxStartMin {
		// The pair is rejected AS A PAIR and BOTH reset to the shipped values, so
		// the note is recorded under BOTH leaves (rereview m461 P2-2): a rejected
		// 16:00 box start must not read "saved value 16:00" on its own row while
		// the bot trades the 08:30 box. The sentence is the same on both rows.
		note := fmt.Sprintf("flat time %s is not after the box start %s: the shipped %s / %s run instead",
			r.FlatTimeCT, r.BoxStartCT, PepBoxStartDefault, PepFlatTimeDefault)
		r.Clamps["pep_flat_time_ct"] = note
		r.Clamps["pep_box_start_ct"] = note
		r.BoxStartCT, r.BoxStartMin = PepBoxStartDefault, 8*60+30
		r.FlatTimeCT, r.FlatMin = PepFlatTimeDefault, 15*60
	}

	r.A.BoxMinutes = pepInt(r.Clamps, "pep_a_box_minutes", sa.BoxMinutes, PepABoxMinutesDefault, 1, PepBoxMinutesCeiling)
	r.A.SLFraction = pepPosFloat(r.Clamps, "pep_a_sl_fraction", sa.SLFraction, PepASLFractionDefault, PepFractionCeiling)
	r.A.TPFraction = pepPosFloat(r.Clamps, "pep_a_tp_fraction", sa.TPFraction, PepATPFractionDefault, PepFractionCeiling)
	r.A.BothEdges = pepVocab(r.Clamps, "pep_a_both_edges", sa.BothEdges, strings.ToLower, []string{PepBothEdgesNoTrade, PepBothEdgesNearestOpen}, PepBothEdgesNoTrade)

	r.E.BoxMinutes = pepInt(r.Clamps, "pep_e_box_minutes", se.BoxMinutes, PepEBoxMinutesDefault, 1, PepBoxMinutesCeiling)
	r.E.SLFraction = pepPosFloat(r.Clamps, "pep_e_sl_fraction", se.SLFraction, PepESLFractionDefault, PepFractionCeiling)
	r.E.TPFraction = pepPosFloat(r.Clamps, "pep_e_tp_fraction", se.TPFraction, PepETPFractionDefault, PepFractionCeiling)
	r.E.EMAPeriod = pepInt(r.Clamps, "pep_e_ema_period", se.EMAPeriod, PepEEMAPeriodDefault, PepEMAPeriodFloor, PepEMAPeriodCeiling)
	r.E.EMATFMin = pepInt(r.Clamps, "pep_e_ema_tf_min", se.EMATFMin, PepEEMATFDefault, 1, 60)
	if !pepValidTF(r.E.EMATFMin) {
		r.Clamps["pep_e_ema_tf_min"] = fmt.Sprintf("%d min does not divide an hour (1,2,3,4,5,6,10,12,15,20,30,60): the shipped %d runs instead", r.E.EMATFMin, PepEEMATFDefault)
		r.E.EMATFMin = PepEEMATFDefault
	}
	r.E.SignalSource = pepVocab(r.Clamps, "pep_e_signal_source", se.SignalSource, strings.ToLower, []string{PepSourceEMA, PepSourceClose}, PepSourceEMA)
	r.E.WarmupBars = pepInt(r.Clamps, "pep_e_warmup_bars", se.WarmupBars, 0, 0, PepWarmupCeiling)
	return r
}

func pepValidTF(tf int) bool {
	switch tf {
	case 1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30, 60:
		return true
	}
	return false
}

// pepInt resolves an *int: nil → def; outside [lo,hi] → the nearest bound, noted.
func pepInt(clamps map[string]string, leaf string, v *int, def, lo, hi int) int {
	if v == nil {
		return def
	}
	switch {
	case *v < lo:
		clamps[leaf] = fmt.Sprintf("saved %d is below the minimum %d: %d runs", *v, lo, lo)
		return lo
	case *v > hi:
		clamps[leaf] = fmt.Sprintf("saved %d is above the maximum %d: %d runs", *v, hi, hi)
		return hi
	}
	return *v
}

// pepFloat resolves a *float64: nil → def; NaN/Inf → def, noted; outside
// [lo,hi] → the nearest bound, noted.
func pepFloat(clamps map[string]string, leaf string, v *float64, def, lo, hi float64) float64 {
	if v == nil {
		return def
	}
	x := *v
	if math.IsNaN(x) || math.IsInf(x, 0) {
		clamps[leaf] = fmt.Sprintf("saved %v is not a number: the shipped %v runs", x, def)
		return def
	}
	switch {
	case x < lo:
		clamps[leaf] = fmt.Sprintf("saved %v is below the minimum %v: %v runs", x, lo, lo)
		return lo
	case x > hi:
		clamps[leaf] = fmt.Sprintf("saved %v is above the maximum %v: %v runs", x, hi, hi)
		return hi
	}
	return x
}

// pepPosFloat resolves a *float64 that must be strictly positive (a tick, a
// fraction of the box): nil → def; NaN, zero or negative → def, noted (a stop
// of 0.01 x box would be an instant stop-out, so the shipped value runs instead
// of the nearest bound); above hi → hi, noted.
func pepPosFloat(clamps map[string]string, leaf string, v *float64, def, hi float64) float64 {
	if v == nil {
		return def
	}
	x := *v
	switch {
	case math.IsNaN(x) || math.IsInf(x, 0) || x <= 0:
		clamps[leaf] = fmt.Sprintf("saved %v must be greater than 0: the shipped %v runs", x, def)
		return def
	case x > hi:
		clamps[leaf] = fmt.Sprintf("saved %v is above the maximum %v: %v runs", x, hi, hi)
		return hi
	}
	return x
}

// pepVocab canonicalises a vocabulary string where it ENTERS (canon 28): trimmed
// and case-folded; an empty value is the default silently (absent); a non-empty
// value outside the vocabulary runs the default and says so.
func pepVocab(clamps map[string]string, leaf, v string, fold func(string) string, vocab []string, def string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	f := fold(v)
	for _, w := range vocab {
		if f == w {
			return w
		}
	}
	clamps[leaf] = fmt.Sprintf("saved %q is not one of %s: %q runs", v, strings.Join(vocab, "|"), def)
	return def
}

// pepClock canonicalises "H:MM"/"HH:MM" to "HH:MM" and minutes after midnight.
func pepClock(clamps map[string]string, leaf, v, def string) (string, int) {
	v = strings.TrimSpace(v)
	if v == "" {
		m, _ := parseHHMM(def)
		return def, m
	}
	if m, ok := parseHHMM(v); ok {
		return fmt.Sprintf("%02d:%02d", m/60, m%60), m
	}
	clamps[leaf] = fmt.Sprintf("saved %q is not HH:MM (00:00-23:59): the shipped %s runs", v, def)
	m, _ := parseHHMM(def)
	return def, m
}

func parseHHMM(s string) (int, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || len(parts[1]) != 2 || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// PepClampNotes returns the clamp sentences sorted by leaf, each prefixed by
// the leaf name — the text logged and returned as a save warning.
func (r PepResolved) PepClampNotes() []string {
	keys := make([]string, 0, len(r.Clamps))
	for k := range r.Clamps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// Leaves that share ONE sentence (a rejected box-start / flat-time pair)
	// print once, naming every leaf, so a save does not warn twice for one cause.
	out := make([]string, 0, len(keys))
	at := map[string]int{} // sentence -> index in out
	leaves := map[string][]string{}
	for _, k := range keys {
		txt := r.Clamps[k]
		if i, dup := at[txt]; dup {
			leaves[txt] = append(leaves[txt], "pep."+k)
			out[i] = strings.Join(leaves[txt], " + ") + ": " + txt
			continue
		}
		at[txt] = len(out)
		leaves[txt] = []string{"pep." + k}
		out = append(out, "pep."+k+": "+txt)
	}
	return out
}

// LogPepClamps logs every clamp of the strategy's pep block, once per call, at
// WARN. Called where the value ENTERS the running system (a Studio save, the
// trader load) so a clamped number is never silent. A config with no pep block,
// or with nothing clamped, logs nothing.
func LogPepClamps(where string, c *StrategyConfig) []string {
	if c == nil || c.Pep == nil {
		return nil
	}
	notes := ResolvePep(c).PepClampNotes()
	for _, n := range notes {
		logger.Warnf("🧭 pep clamp (%s): %s", where, n)
	}
	return notes
}

// ── TRADING-MODE EXCLUSIVITY ─────────────────────────────────────────────────

// Mode-conflict codes (the API's error_key and the boot line share them).
const (
	PepConflictMentor  = "pep_mode_conflict_mentor"
	PepConflictDayPlan = "pep_mode_conflict_day_plan"
)

// ModeConflictError names an illegal combination of trading modes in ONE
// strategy. A trader is exactly one of {AI/planner, mentor, pep}.
type ModeConflictError struct {
	Code    string // PepConflictMentor | PepConflictDayPlan
	Message string
}

func (e *ModeConflictError) Error() string { return e.Message }

// TradingModeConflict returns a named error when pep is ON together with mentor
// mode or the day plan, nil otherwise. It reads the SAVED flags, never a
// resolver, so a half-saved combination is still refused.
func (c *StrategyConfig) TradingModeConflict() *ModeConflictError {
	if !c.PepEnabled() {
		return nil
	}
	if c.RiskControl.MentorMode {
		return &ModeConflictError{
			Code: PepConflictMentor,
			Message: "pep mode and mentor mode are both ON in this strategy (pep.pep_enabled and risk_control.mentor_mode): " +
				"a trader is exactly one of {AI/planner, mentor, pep}. Turn one of them off.",
		}
	}
	if c.DayPlan != nil && c.DayPlan.PlanEnabled {
		return &ModeConflictError{
			Code: PepConflictDayPlan,
			Message: "pep mode and the day plan are both ON in this strategy (pep.pep_enabled and day_plan.plan_enabled): " +
				"a trader is exactly one of {AI/planner, mentor, pep}. Turn one of them off.",
		}
	}
	return nil
}

// guardStrategyModes is the strategy store's write guard (every writer of the
// strategies table goes through Create / Update / their *WithExplicitZeros
// forms: the Studio, the agent tools, the cmd/ repair tools). It refuses a row
// whose config turns pep ON together with mentor mode or the day plan, with the
// named *ModeConflictError. An unparseable config is not this guard's business
// (nil): the other save-time validation reports that.
func guardStrategyModes(configJSON string) error {
	// Go's JSON decoder matches keys case-insensitively, so the fast path must
	// too: a hand-written "PEP_ENABLED" still reaches the parse below.
	if !strings.Contains(strings.ToLower(configJSON), "pep_enabled") {
		return nil // no pep block saved: nothing to conflict, and no parse cost
	}
	var c StrategyConfig
	if err := jsonUnmarshalString(configJSON, &c); err != nil {
		return nil
	}
	if e := c.TradingModeConflict(); e != nil {
		return e
	}
	return nil
}

func jsonUnmarshalString(s string, v any) error { return json.Unmarshal([]byte(s), v) }
