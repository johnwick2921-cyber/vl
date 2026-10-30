package trader

import (
	"time"

	"vl/kernel"
	"vl/kernel/pep"
	"vl/store"
)

// ── PEP-TRUTH: what the dashboard card reads ────────────────────────────────

// pepTruthState is the newest evaluation, kept for the snapshot.
type pepTruthState struct {
	at        time.Time
	res       pep.Result
	posOpen   int
	evaluated bool
}

// pepNoteTruth records the newest evaluation (called by pepEvalOnce).
func (at *AutoTrader) pepNoteTruth(res pep.Result, _ pep.Config, _ store.PepDayState, positions int, now time.Time) {
	at.pep.truthMu.Lock()
	at.pep.truth = pepTruthState{at: now, res: res, posOpen: positions, evaluated: true}
	at.pep.truthMu.Unlock()
}

// PepTruthBox is the opening box as the evaluator measured it.
type PepTruthBox struct {
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Size  float64 `json:"size"`
	Drawn bool    `json:"drawn"`
}

// PepTruthRefusal is one named reason the evaluator produced no entry.
type PepTruthRefusal struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // wait | block
	Detail   string `json:"detail"`
}

// PepTruth is GET /api/traders/:id/pep-truth. Every field is READ from the code
// that enforces it (the resolver, the durable day record, the evaluator's last
// result) - nothing is a typed copy.
type PepTruth struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"` // ai | mentor | pep | conflict
	// Conflict names an illegal mode combination (the trader was refused at start).
	Conflict *PepTruthConflict `json:"conflict,omitempty"`

	// Symbol is the instrument this trader's pep reads (at.futuresSymbol()):
	// the chart draws the box only on a tab showing this symbol.
	Symbol    string `json:"symbol,omitempty"`
	Style     string `json:"style,omitempty"`
	Contracts int    `json:"contracts,omitempty"`
	Day       string `json:"day,omitempty"` // CT date

	Box PepTruthBox `json:"box"`
	// Signal is the state in one word: no_data | waiting_box | waiting_signal |
	// signal | blocked | traded | position_open | after_flat | flat_pending.
	Signal   string            `json:"signal,omitempty"`
	Refusals []PepTruthRefusal `json:"refusals,omitempty"`

	TradesToday     int  `json:"trades_today"`
	MaxTradesPerDay int  `json:"max_trades_per_day,omitempty"`
	PositionOpen    bool `json:"position_open"`

	// Stop / Target of today's newest entry (the bracket sent to the wire).
	Stop   float64 `json:"stop,omitempty"`
	Target float64 `json:"target,omitempty"`
	Side   string  `json:"side,omitempty"`

	FlatTimeCT       string `json:"flat_time_ct,omitempty"`
	FlatCountdownSec int64  `json:"flat_countdown_sec"` // seconds to the flat; 0 once reached
	LastEvaluatedMs  int64  `json:"last_evaluated_ms,omitempty"`
	// Clamps names every saved leaf that did not take effect as saved.
	Clamps map[string]string `json:"clamps,omitempty"`
}

// PepTruthConflict is the mode-conflict evidence.
type PepTruthConflict struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PepTruthSnapshot builds the card's data. It re-reads the durable day record
// and the open positions on every call (never a cached count) and joins the
// evaluator's newest result.
func (at *AutoTrader) PepTruthSnapshot(now time.Time) PepTruth {
	out := PepTruth{Mode: "ai"}
	if at == nil || at.config.StrategyConfig == nil {
		return out
	}
	out.Mode = at.config.StrategyConfig.TradingMode()
	if c := at.pepModeConflict(); c != nil && at.pepSaved() {
		out.Conflict = &PepTruthConflict{Code: c.Code, Message: c.Message}
	}
	if !at.pepEnabled() {
		return out
	}
	r := at.pepResolved()
	out.Enabled = true
	out.Style, out.Contracts = r.Style, r.Contracts
	out.Symbol = at.futuresSymbol()
	out.MaxTradesPerDay = r.MaxTradesPerDay
	out.FlatTimeCT = r.FlatTimeCT
	flatMin := r.FlatMin
	if len(r.Clamps) > 0 {
		out.Clamps = r.Clamps
	}
	day := pepDayNow(now)
	out.Day = day
	loc := kernel.CTLocation()
	ct := now.In(loc)
	// a shortened day pulls the flat in (pepEffectiveFlatMin): the card says so
	if eff, pulled := pepEffectiveFlatMin(day, flatMin, r.BoxStartMin); pulled {
		flatMin = eff
		out.FlatTimeCT = pepClock(eff)
	}
	flatAt := time.Date(ct.Year(), ct.Month(), ct.Day(), flatMin/60, flatMin%60, 0, 0, loc)
	if now.Before(flatAt) {
		out.FlatCountdownSec = int64(flatAt.Sub(now) / time.Second)
	}

	if at.store != nil && day != "" {
		if ds, err := at.store.PepDay().Day(at.id, day); err == nil {
			out.TradesToday = ds.Counted
			if l := ds.Last; l != nil && l.State != store.PepDayReleased {
				out.Stop, out.Target, out.Side = l.Stop, l.Target, l.Side
			}
		}
		if ps, err := at.store.Position().GetOpenPositions(at.id); err == nil {
			out.PositionOpen = len(ps) > 0
		}
	}

	at.pep.truthMu.Lock()
	t := at.pep.truth
	at.pep.truthMu.Unlock()
	if t.evaluated {
		out.LastEvaluatedMs = t.at.UnixMilli()
	}
	// The evaluator's box, refusals and signal belong to the CT day it ran on. On
	// a day with no pass (weekend, holiday, before the first bar of the day) the
	// stored result is YESTERDAY's: merging it under today's date drew Friday's
	// box and "Refusal: pep_position_open" on a Saturday card (re-review 6, P3).
	// A stale evaluation is treated as no evaluation: no box, no refusals,
	// signal no_data - while LastEvaluatedMs still says when it last ran.
	if t.evaluated && pepDayNow(t.at) != day {
		t = pepTruthState{}
	}
	if t.evaluated {
		out.Box = PepTruthBox{High: t.res.Box.High, Low: t.res.Box.Low, Size: t.res.Box.Size, Drawn: t.res.Box.Drawn}
		for _, f := range t.res.Refusals {
			out.Refusals = append(out.Refusals, PepTruthRefusal{Code: f.Code, Severity: string(f.Severity), Detail: f.Detail})
		}
	}
	out.Signal = pepSignalWord(out, t)
	return out
}

// pepSignalWord names the card's state from the facts above.
func pepSignalWord(out PepTruth, t pepTruthState) string {
	switch {
	case out.PositionOpen && out.FlatCountdownSec == 0:
		return "flat_pending"
	case out.PositionOpen:
		return "position_open"
	case !t.evaluated:
		return "no_data"
	case len(t.res.Intents) > 0:
		return "signal"
	case t.res.HasCode(pep.CodeAfterFlat):
		return "after_flat"
	case t.res.HasCode(pep.CodeMaxTrades):
		return "traded"
	case t.res.Blocked():
		return "blocked"
	case t.res.HasCode(pep.CodeBoxNotComplete):
		return "waiting_box"
	}
	return "waiting_signal"
}
