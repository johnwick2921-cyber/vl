package pep

// Side is the trade direction.
type Side string

const (
	SideLong  Side = "long"
	SideShort Side = "short"
)

// IntentKind says what the caller must do.
type IntentKind string

const (
	// KindEnter: open a market position with the carried stop and target.
	KindEnter IntentKind = "enter"
	// KindFlat: flatten the open position at the last price (the time exit).
	KindFlat IntentKind = "flat"
)

// Intent is what Evaluate wants done. Entry is the REFERENCE entry price (the
// box edge for style A, the signal candle's close for style E); a live caller
// that fills at market recomputes Stop/Target from StopDist/TargetDist and the
// actual fill. Prices are on the tick grid.
type Intent struct {
	Kind      IntentKind
	Side      Side
	Style     Style
	Contracts int

	Entry  float64
	Stop   float64
	Target float64

	// StopDist / TargetDist are the distances (points) from Entry; the stop is
	// rounded OUTWARD to a tick.
	StopDist   float64
	TargetDist float64

	// BoxHigh/BoxLow/BoxSize are the box this intent was built from.
	BoxHigh, BoxLow, BoxSize float64

	// SignalOpen/SignalClose are the open and close instants (ms) of the 1m bar
	// (style A) or the candle (style E) that produced the signal. Entry is
	// timed at SignalClose+1 for E and inside the bar for A.
	SignalOpen  int64
	SignalClose int64

	Day    string // CT date "2006-01-02"
	Reason string
}

// Severity separates a normal waiting state from a fail-closed block, so a
// caller counts only the blocks as gate refusals.
type Severity string

const (
	SevWait  Severity = "wait"  // nothing wrong: the signal simply has not happened yet
	SevBlock Severity = "block" // fail closed: this day/tick is refused for the named reason
)

// Refusal codes. Stable strings: telemetry counters and tests key on them.
const (
	CodeConfigInvalid  = "pep_config_invalid"
	CodeTZUnavailable  = "pep_tz_unavailable"
	CodeBarsUnordered  = "pep_bars_not_ascending"
	CodeNoBars         = "pep_no_bars"
	CodeBarsStale      = "pep_bars_stale"
	CodeAfterFlat      = "pep_after_flat_time"
	CodeMaxTrades      = "pep_max_trades_reached"
	CodePositionOpen   = "pep_position_open"
	CodeBoxNotComplete = "pep_box_not_complete"
	CodeBoxMissingMin  = "pep_box_missing_minute"
	CodeBoxFlat        = "pep_box_zero_range"
	CodeBoxTooWide     = "pep_box_too_wide"
	CodeGapAfterBox    = "pep_gap_after_box"
	CodeNoBreakYet     = "pep_no_break_yet"
	CodeBothEdges      = "pep_bar_through_both_edges"
	CodeStaleSignal    = "pep_stale_signal"
	CodeEMANotWarm     = "pep_ema_not_warm"
	CodeNoCrossYet     = "pep_no_cross_yet"
	CodeStopTooTight   = "pep_stop_below_one_tick"
)

// Refusal is one named reason Evaluate produced no entry.
type Refusal struct {
	Code     string
	Severity Severity
	Detail   string
}

// Box is the opening range.
type Box struct {
	High, Low, Size float64
	Start, End      int64 // ms; the box covers 1m bars opening in [Start, End)
	Drawn           bool  // every minute present and the box window has closed
}

// State is the one-trade-per-day memory, kept by the CALLER (durably, before
// the order is sent). Evaluate never mutates it.
type State struct {
	// Day is the CT date the counters belong to; a different day reads as zero.
	Day string
	// Trades is how many entries were taken (or deliberately consumed) today.
	Trades int
	// PositionOpen is true while a pep position is open (blocks a new entry and
	// arms the flat intent).
	PositionOpen bool
}

// AfterEntry returns the state to persist once an entry intent has been
// accepted for sending.
func (s State) AfterEntry(day string) State {
	n := s.Trades
	if s.Day != day {
		n = 0
	}
	return State{Day: day, Trades: n + 1, PositionOpen: true}
}

// Result is Evaluate's whole output.
type Result struct {
	Day     string // CT date of now
	Style   Style
	Box     Box
	Intents []Intent
	// Refusals is empty when an intent was produced.
	Refusals []Refusal
	// Diag carries read-only numbers for the dashboard and the tests.
	Diag Diag
}

// Diag is read-only diagnostics.
type Diag struct {
	Bars            int     // closed bars read
	Dropped         int     // bars not yet closed at now (ignored)
	Candles         int     // complete closed candles built (style E)
	PartialCandles  int     // candles skipped for a missing minute (style E)
	LastEMA         float64 // EMA at the newest complete candle (style E)
	Signals         int     // signals found today before the freshness check
	TradesTaken     int
	EntriesAllowed  int
	FirstBreakOpen  int64 // open ms of style A's first break bar (0 = none)
	BreakBothEdges  bool
	SignalAgeMillis int64
}

// Blocked reports whether any refusal is a fail-closed block.
func (r Result) Blocked() bool {
	for _, f := range r.Refusals {
		if f.Severity == SevBlock {
			return true
		}
	}
	return false
}

// HasCode reports whether a refusal with the code is present.
func (r Result) HasCode(code string) bool {
	for _, f := range r.Refusals {
		if f.Code == code {
			return true
		}
	}
	return false
}
