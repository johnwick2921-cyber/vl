// Package pep is the PURE, deterministic evaluator of the "pep" trading mode
// (owner order 2026-10-29; source: the owner's PDF "A + E — Full calculation",
// /home/hoang/fleet-1017/out/pep-spec-coa707.pdf).
//
// pep is a mode SEPARATE from mentor mode (owner ruling): its own code, its own
// settings, and no mentor rule applies to it. A trader is exactly one of
// {AI/planner, mentor, pep}. This package imports NOTHING from kernel/mentor.
//
//	Style A  ORB 5m breakout, enter immediately: the box is the high/low of the
//	         first 5 minutes from 08:30 CT; the first side to trade through a
//	         box edge is entered; SL = full box, TP = full box; no trailing.
//	Style E  EMA9 ORB 2m, candle-close confirmation: EMA9 on 2m candles
//	         (carried ACROSS sessions) crossing the box edge on a CLOSED 2m
//	         candle; SL = half box rounded OUTWARD to a tick, TP = full box.
//
// Both: max 1 trade a day, flat by 15:00 CT at the last price.
//
// BOX LENGTH IS PER STYLE (data-proved 2026-10-30). The PDF's own style-E
// ledger rows equal the FIRST TWO MINUTES' range ("ORB 2 PHÚT"): on 2025-10-20
// the E take-profit is 73.0 pts = high-low of 08:30+08:31, while the style-A box
// (5 minutes) is 86.75. So A defaults to a 5 minute box and E to a 2 minute
// box; both are settings.
//
// EVALUATE IS PURE. Closed FINAL 1m bars + now + Config + State in, intents and
// NAMED refusals out. No time.Now, no mutable package state (the only package
// var is the cached, immutable America/Chicago location), no I/O, no logging.
// Every refusal carries a code from the exported Code* constants so a caller can
// count it (the repo's no-silent-refusal rule).
package pep

import (
	"fmt"
	"math"
	"strings"
)

// Style names a pep entry style.
type Style string

const (
	StyleA Style = "A" // ORB breakout, enter on the first trade through a box edge
	StyleE Style = "E" // EMA9 ORB, candle-close confirmation
	// StyleCustom is the editable style: Config.Custom picks the entry type and
	// every number. A and E are the PDF's; the store resolver locks them.
	StyleCustom Style = "Custom"
)

// EntryType is Custom's entry rule: an A-like breakout or an E-like EMA cross.
type EntryType string

const (
	EntryBreakout EntryType = "breakout"
	EntryEMACross EntryType = "ema_cross"
)

// SignalSource is style E's cross reference: the EMA crossing the box edge
// (the owner's ruling, the default), or the candle CLOSE crossing the edge.
type SignalSource string

const (
	SourceEMA   SignalSource = "ema"
	SourceClose SignalSource = "close"
)

// BothEdges is style A's policy for ONE 1m bar that trades through BOTH box
// edges (the 1m bar cannot say which came first; the PDF's tick engine can).
type BothEdges string

const (
	// BothEdgesNoTrade fails closed: no trade today, named reason. The default.
	BothEdgesNoTrade BothEdges = "no_trade"
	// BothEdgesNearestOpen assumes the edge nearer the bar's open broke first.
	// Used by the backtest to bracket the unknown; a bar-level guess.
	BothEdgesNearestOpen BothEdges = "nearest_open"
)

// SLRounding is the stop-distance tick rounding.
type SLRounding string

const (
	RoundOutward SLRounding = "outward"
	RoundNearest SLRounding = "nearest"
)

// Hard limits. Contracts above the ceiling are refused, never clamped here (the
// store resolver clamps and LOGS; this package is the last line).
const (
	MaxContractsCeiling = 10
	// DefaultContracts is the shipped contract count (owner 2026-10-30: 6).
	DefaultContracts = 6
)

// ParamsA holds style A's own numbers (kept apart from E's so switching style
// never loses the other style's values).
type ParamsA struct {
	BoxMinutes int       // length of the opening box from BoxStart (default 5)
	SLFraction float64   // stop distance as a fraction of the box (default 1.0)
	TPFraction float64   // target distance as a fraction of the box (default 1.0)
	BothEdges  BothEdges // one bar through both edges (default no_trade)
}

// ParamsE holds style E's own numbers.
type ParamsE struct {
	BoxMinutes int          // length of the opening box (default 2: the PDF's "ORB 2 PHÚT")
	SLFraction float64      // stop fraction of the box, rounded OUTWARD to a tick (default 0.5)
	TPFraction float64      // target fraction of the box (default 1.0)
	EMAPeriod  int          // default 9
	EMATFMin   int          // candle timeframe in minutes (default 2)
	Source     SignalSource // default ema
	// WarmupBars is how many CLOSED complete candles must precede the first
	// candle that may signal. 0 = 3 x EMAPeriod (0.8^27 ≈ 0.2% residual seed).
	WarmupBars int
}

// ParamsCustom holds the Custom style's numbers (seeded from style E's).
type ParamsCustom struct {
	Entry      EntryType
	BoxMinutes int
	SLFraction float64
	TPFraction float64
	BothEdges  BothEdges    // breakout only
	EMAPeriod  int          // ema_cross only
	EMATFMin   int          // ema_cross only
	Source     SignalSource // ema_cross only
	WarmupBars int          // ema_cross only; 0 = 3 x EMAPeriod
}

// Config is every number pep reads. Nothing is hardcoded in Evaluate.
type Config struct {
	Style     Style
	Contracts int

	// BoxStartMin is the box start as minutes after CT midnight (08:30 = 510).
	BoxStartMin int
	// FlatMin is the time-exit as minutes after CT midnight (15:00 = 900).
	FlatMin int
	// TickSize is the instrument tick (MNQ 0.25).
	TickSize float64
	// SLRounding says how the stop distance is rounded to a tick: outward (the
	// PDF's rule: away from entry, the default) or nearest.
	SLRounding SLRounding
	// MaxTradesPerDay caps entries a CT day (default 1).
	MaxTradesPerDay int
	// MaxBoxPoints refuses a box wider than this many points; 0 = OFF (default,
	// matching the PDF).
	MaxBoxPoints float64
	// SignalMaxAgeMin is how old (minutes) a signal or the newest closed bar may
	// be before it is refused as stale (a break the bot never saw in time is not
	// entered late). Default 2.
	SignalMaxAgeMin int
	// FeePerContractRT is the round-trip fee per contract in USD. BACKTEST and
	// REPORTING ONLY: Evaluate never reads it.
	FeePerContractRT float64

	A      ParamsA
	E      ParamsE
	Custom ParamsCustom
}

// DefaultConfig returns the shipped numbers (the PDF's). OFF is a store-level
// switch (PepConfig.Enabled); the evaluator has no on/off.
func DefaultConfig() Config {
	return Config{
		Style:            StyleE, // owner 2026-10-30: shipped default style E
		Contracts:        DefaultContracts,
		BoxStartMin:      8*60 + 30,
		FlatMin:          15 * 60,
		TickSize:         0.25,
		SLRounding:       RoundOutward,
		MaxTradesPerDay:  1,
		MaxBoxPoints:     0,
		SignalMaxAgeMin:  2,
		FeePerContractRT: 1.0,
		A: ParamsA{
			BoxMinutes: 5,
			SLFraction: 1.0,
			TPFraction: 1.0,
			BothEdges:  BothEdgesNoTrade,
		},
		E: ParamsE{
			BoxMinutes: 2,
			SLFraction: 0.5,
			TPFraction: 1.0,
			EMAPeriod:  9,
			EMATFMin:   2,
			Source:     SourceEMA,
			WarmupBars: 0,
		},
		Custom: ParamsCustom{
			Entry: EntryEMACross, BoxMinutes: 2, SLFraction: 0.5, TPFraction: 1.0,
			BothEdges: BothEdgesNoTrade, EMAPeriod: 9, EMATFMin: 2, Source: SourceEMA,
		},
	}
}

// Validate returns a non-nil error naming the first out-of-range number. The
// evaluator refuses (CodeConfigInvalid) on any error rather than guessing.
func (c Config) Validate() error {
	var bad []string
	if c.Style != StyleA && c.Style != StyleE && c.Style != StyleCustom {
		bad = append(bad, fmt.Sprintf("style %q (want A, E or Custom)", c.Style))
	}
	if c.Contracts < 1 || c.Contracts > MaxContractsCeiling {
		bad = append(bad, fmt.Sprintf("contracts %d (want 1..%d)", c.Contracts, MaxContractsCeiling))
	}
	if c.BoxStartMin < 0 || c.BoxStartMin >= 24*60 {
		bad = append(bad, fmt.Sprintf("box start %d min (want 0..1439)", c.BoxStartMin))
	}
	if c.FlatMin <= c.BoxStartMin || c.FlatMin > 24*60 {
		bad = append(bad, fmt.Sprintf("flat time %d min must be after the box start %d", c.FlatMin, c.BoxStartMin))
	}
	if !(c.TickSize > 0) || math.IsNaN(c.TickSize) || math.IsInf(c.TickSize, 0) {
		bad = append(bad, fmt.Sprintf("tick size %v (want > 0)", c.TickSize))
	}
	if c.SLRounding != RoundOutward && c.SLRounding != RoundNearest {
		bad = append(bad, fmt.Sprintf("sl rounding %q (want outward or nearest)", c.SLRounding))
	}
	if c.MaxTradesPerDay < 1 {
		bad = append(bad, fmt.Sprintf("max trades per day %d (want >= 1)", c.MaxTradesPerDay))
	}
	if c.MaxBoxPoints < 0 || math.IsNaN(c.MaxBoxPoints) {
		bad = append(bad, fmt.Sprintf("max box points %v (want >= 0; 0 = off)", c.MaxBoxPoints))
	}
	if c.SignalMaxAgeMin < 1 {
		bad = append(bad, fmt.Sprintf("signal max age %d min (want >= 1)", c.SignalMaxAgeMin))
	}
	if c.FeePerContractRT < 0 || math.IsNaN(c.FeePerContractRT) {
		bad = append(bad, fmt.Sprintf("fee %v (want >= 0)", c.FeePerContractRT))
	}
	if err := c.A.validate(); err != nil {
		bad = append(bad, "style A: "+err.Error())
	}
	if err := c.E.validate(); err != nil {
		bad = append(bad, "style E: "+err.Error())
	}
	if c.Style == StyleCustom {
		if err := c.Custom.validate(); err != nil {
			bad = append(bad, "style Custom: "+err.Error())
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("pep config invalid: %s", strings.Join(bad, "; "))
}

func fractionOK(f float64) bool { return f > 0 && f <= 10 && !math.IsNaN(f) }

func (a ParamsA) validate() error {
	var bad []string
	if a.BoxMinutes < 1 || a.BoxMinutes > 120 {
		bad = append(bad, fmt.Sprintf("box minutes %d (want 1..120)", a.BoxMinutes))
	}
	if !fractionOK(a.SLFraction) {
		bad = append(bad, fmt.Sprintf("sl fraction %v (want (0,10])", a.SLFraction))
	}
	if !fractionOK(a.TPFraction) {
		bad = append(bad, fmt.Sprintf("tp fraction %v (want (0,10])", a.TPFraction))
	}
	if a.BothEdges != BothEdgesNoTrade && a.BothEdges != BothEdgesNearestOpen {
		bad = append(bad, fmt.Sprintf("both-edges policy %q", a.BothEdges))
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(bad, ", "))
}

func (e ParamsE) validate() error {
	var bad []string
	if e.BoxMinutes < 1 || e.BoxMinutes > 120 {
		bad = append(bad, fmt.Sprintf("box minutes %d (want 1..120)", e.BoxMinutes))
	}
	if !fractionOK(e.SLFraction) {
		bad = append(bad, fmt.Sprintf("sl fraction %v (want (0,10])", e.SLFraction))
	}
	if !fractionOK(e.TPFraction) {
		bad = append(bad, fmt.Sprintf("tp fraction %v (want (0,10])", e.TPFraction))
	}
	if e.EMAPeriod < 2 || e.EMAPeriod > 200 {
		bad = append(bad, fmt.Sprintf("ema period %d (want 2..200)", e.EMAPeriod))
	}
	if !validTFMin(e.EMATFMin) {
		bad = append(bad, fmt.Sprintf("ema timeframe %d min (want a divisor of 60: 1,2,3,4,5,6,10,12,15,20,30,60)", e.EMATFMin))
	}
	if e.Source != SourceEMA && e.Source != SourceClose {
		bad = append(bad, fmt.Sprintf("signal source %q", e.Source))
	}
	if e.WarmupBars < 0 || e.WarmupBars > 5000 {
		bad = append(bad, fmt.Sprintf("warmup bars %d (want 0..5000; 0 = 3 x period)", e.WarmupBars))
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(bad, ", "))
}

func (p ParamsCustom) validate() error {
	var bad []string
	if p.Entry != EntryBreakout && p.Entry != EntryEMACross {
		bad = append(bad, fmt.Sprintf("entry type %q", p.Entry))
	}
	if p.BoxMinutes < 1 || p.BoxMinutes > 120 {
		bad = append(bad, fmt.Sprintf("box minutes %d (want 1..120)", p.BoxMinutes))
	}
	if !fractionOK(p.SLFraction) {
		bad = append(bad, fmt.Sprintf("sl fraction %v (want (0,10])", p.SLFraction))
	}
	if !fractionOK(p.TPFraction) {
		bad = append(bad, fmt.Sprintf("tp fraction %v (want (0,10])", p.TPFraction))
	}
	if p.Entry == EntryBreakout && p.BothEdges != BothEdgesNoTrade && p.BothEdges != BothEdgesNearestOpen {
		bad = append(bad, fmt.Sprintf("both-edges policy %q", p.BothEdges))
	}
	if p.Entry == EntryEMACross {
		if err := p.emaParams().validate(); err != nil {
			bad = append(bad, err.Error())
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(bad, ", "))
}

func (p ParamsCustom) emaParams() ParamsE {
	return ParamsE{BoxMinutes: p.BoxMinutes, SLFraction: p.SLFraction, TPFraction: p.TPFraction,
		EMAPeriod: p.EMAPeriod, EMATFMin: p.EMATFMin, Source: p.Source, WarmupBars: p.WarmupBars}
}

// validTFMin: a candle timeframe must divide an hour so every candle boundary
// falls on a CT wall-clock multiple and UTC-epoch flooring is DST-safe (CT is a
// whole-hour UTC offset).
func validTFMin(tf int) bool {
	switch tf {
	case 1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30, 60:
		return true
	}
	return false
}

// IsBreakout reports whether the active rule is the A-like breakout (style A,
// or Custom with entry type breakout); false = the E-like EMA cross.
func (c Config) IsBreakout() bool {
	switch c.Style {
	case StyleA:
		return true
	case StyleCustom:
		return c.Custom.Entry == EntryBreakout
	}
	return false
}

// EMA is the active EMA-cross parameter set (style E's, or Custom's).
func (c Config) EMA() ParamsE {
	if c.Style == StyleCustom {
		return c.Custom.emaParams()
	}
	return c.E
}

// BothEdgesPolicy is the active one-bar-through-both-edges policy.
func (c Config) BothEdgesPolicy() BothEdges {
	if c.Style == StyleCustom {
		return c.Custom.BothEdges
	}
	return c.A.BothEdges
}

// BoxMinutes is the active style's box length.
func (c Config) BoxMinutes() int {
	switch c.Style {
	case StyleE:
		return c.E.BoxMinutes
	case StyleCustom:
		return c.Custom.BoxMinutes
	}
	return c.A.BoxMinutes
}

// SLFraction is the active style's stop fraction of the box.
func (c Config) SLFraction() float64 {
	switch c.Style {
	case StyleE:
		return c.E.SLFraction
	case StyleCustom:
		return c.Custom.SLFraction
	}
	return c.A.SLFraction
}

// TPFraction is the active style's target fraction of the box.
func (c Config) TPFraction() float64 {
	switch c.Style {
	case StyleE:
		return c.E.TPFraction
	case StyleCustom:
		return c.Custom.TPFraction
	}
	return c.A.TPFraction
}

// WarmupCandles is the active EMA rule's resolved warm-up (0 = 3 x period).
func (c Config) WarmupCandles() int {
	e := c.EMA()
	if e.WarmupBars > 0 {
		return e.WarmupBars
	}
	return 3 * e.EMAPeriod
}
