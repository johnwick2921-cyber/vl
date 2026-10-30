package pep

import (
	"sort"
	"time"

	"vl/market"
)

// ExitKind says how a replayed trade ended.
type ExitKind string

const (
	ExitTP      ExitKind = "TP"
	ExitSL      ExitKind = "SL"
	ExitFlat    ExitKind = "FLAT"     // the 15:00 CT time exit, at the last price
	ExitDataEnd ExitKind = "DATA_END" // the tape ended before the flat time (half day / feed end): last price
)

// DayResult is one replayed CT day.
type DayResult struct {
	Date  string
	Style Style
	Box   Box
	// Traded is true when an enter intent was produced and played out.
	Traded bool
	Side   Side
	Entry  float64
	Stop   float64
	Target float64
	// Exit fields are set when Traded.
	Exit     float64
	ExitKind ExitKind
	EntryAt  int64 // ms
	ExitAt   int64 // ms
	// GrossPts is the signed points result per contract (no fee).
	GrossPts float64
	// Refusal is the last refusal code when the day did not trade.
	Refusal       string
	RefusalDetail string
	// BothEdges is true when the first break was a 1m bar through both edges.
	BothEdges bool
	Intent    Intent
}

// terminal refusals cannot clear later in the same day on a fixed tape.
var terminal = map[string]bool{
	CodeBoxMissingMin: true, CodeBoxFlat: true, CodeBoxTooWide: true, CodeGapAfterBox: true,
	CodeBothEdges: true, CodeStaleSignal: true, CodeStopTooTight: true, CodeConfigInvalid: true,
	CodeTZUnavailable: true, CodeBarsUnordered: true,
}

// ReplayDay replays one CT day through the SAME Evaluate the live loop calls: at
// every closed 1m bar from the box end to the flat time it hands Evaluate the
// last `window` closed bars (the live feed reads 1500) and now = bar close + 1.
// The first enter intent is played out against the following bars:
//
//   - stop and target are checked bar by bar, the STOP FIRST when one bar
//     reaches both (pessimistic, as the PDF's stress case);
//   - at the flat time the position is closed at the last price (the close of
//     the last bar that opened before the flat time);
//   - a tape that ends before the flat time closes at its last price
//     (ExitDataEnd, the PDF's CUTOFF_PROXY).
//
// date is "YYYY-MM-DD" in CT; series is ascending OPEN-stamped 1m bars of ONE
// contract.
func ReplayDay(series []market.Kline, date string, cfg Config, window int) DayResult {
	out := DayResult{Date: date, Style: cfg.Style}
	loc, err := chicago()
	if err != nil {
		out.Refusal, out.RefusalDetail = CodeTZUnavailable, err.Error()
		return out
	}
	d, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		out.Refusal, out.RefusalDetail = CodeConfigInvalid, err.Error()
		return out
	}
	y, mo, dd := d.Date()
	at := func(minOfDay int) int64 { return time.Date(y, mo, dd, minOfDay/60, minOfDay%60, 0, 0, loc).UnixMilli() }
	boxEnd := at(cfg.BoxStartMin) + int64(cfg.BoxMinutes())*minuteMs
	flatAt := at(cfg.FlatMin)
	if window < 1 {
		window = 1500
	}

	// first bar index that closes at/after the box end
	i := sort.Search(len(series), func(k int) bool { return series[k].CloseTime+1 >= boxEnd })
	var st State
	for ; i < len(series) && series[i].OpenTime < flatAt; i++ {
		now := series[i].CloseTime + 1
		lo := i + 1 - window
		if lo < 0 {
			lo = 0
		}
		r := Evaluate(series[lo:i+1], now, cfg, st)
		out.Box = r.Box
		if r.Diag.BreakBothEdges {
			out.BothEdges = true
		}
		if len(r.Intents) == 1 && r.Intents[0].Kind == KindEnter {
			in := r.Intents[0]
			out.Intent = in
			out.Traded = true
			out.Side, out.Entry, out.Stop, out.Target = in.Side, in.Entry, in.Stop, in.Target
			start := i + 1 // E: enter at the candle close, scan the NEXT bars
			if in.Style == StyleA {
				start = i // A: entered inside the break bar itself
			}
			if in.Style == StyleA {
				out.EntryAt = in.SignalOpen
			} else {
				out.EntryAt = in.SignalClose + 1
			}
			playOut(&out, series, start, flatAt)
			return out
		}
		if len(r.Refusals) > 0 {
			f := r.Refusals[len(r.Refusals)-1]
			// the closing "after flat time" tick must not hide the day's real reason
			if f.Code != CodeAfterFlat || out.Refusal == "" {
				out.Refusal, out.RefusalDetail = f.Code, f.Detail
			}
			if terminal[f.Code] {
				return out
			}
		}
	}
	if out.Refusal == "" {
		out.Refusal = CodeNoBars
		out.RefusalDetail = "no closed bar between the box end and the flat time"
	}
	return out
}

func playOut(out *DayResult, series []market.Kline, start int, flatAt int64) {
	long := out.Side == SideLong
	last := -1 // index of the last bar seen that opened before the flat time
	for j := start; j < len(series); j++ {
		b := series[j]
		if b.OpenTime >= flatAt {
			break
		}
		last = j
		if long {
			if b.Low <= out.Stop {
				finish(out, out.Stop, ExitSL, b.CloseTime)
				return
			}
			if b.High >= out.Target {
				finish(out, out.Target, ExitTP, b.CloseTime)
				return
			}
		} else {
			if b.High >= out.Stop {
				finish(out, out.Stop, ExitSL, b.CloseTime)
				return
			}
			if b.Low <= out.Target {
				finish(out, out.Target, ExitTP, b.CloseTime)
				return
			}
		}
	}
	if last < 0 {
		// nothing after the entry: close at the entry (zero), data ended
		finish(out, out.Entry, ExitDataEnd, out.EntryAt)
		return
	}
	// the tape reached the flat time (or ended): close at the last price
	kind := ExitDataEnd
	if last+1 < len(series) && series[last+1].OpenTime >= flatAt {
		kind = ExitFlat
	} else if last+1 >= len(series) && series[last].CloseTime+1 >= flatAt {
		kind = ExitFlat
	}
	finish(out, series[last].Close, kind, series[last].CloseTime)
}

func finish(out *DayResult, px float64, kind ExitKind, at int64) {
	out.Exit, out.ExitKind, out.ExitAt = px, kind, at
	if out.Side == SideLong {
		out.GrossPts = px - out.Entry
	} else {
		out.GrossPts = out.Entry - px
	}
}

// GrossUSD is the day's gross P&L for the contract count at the point value.
func (d DayResult) GrossUSD(contracts int, pointValue float64) float64 {
	return d.GrossPts * pointValue * float64(contracts)
}

// NetUSD subtracts the round-trip fee per contract (reporting only).
func (d DayResult) NetUSD(contracts int, pointValue, feePerContractRT float64) float64 {
	if !d.Traded {
		return 0
	}
	return d.GrossUSD(contracts, pointValue) - feePerContractRT*float64(contracts)
}
