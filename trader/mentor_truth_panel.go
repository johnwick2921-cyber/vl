package trader

import (
	"time"

	"vl/kernel/mentor"
)

// MentorTruthPanel is the read-only "what trades" snapshot the planner page and
// the trader dashboard render. Every field is derived from the LIVE evaluator
// state (under mentorEvalMu) or the pure gate helpers — never the internals the
// other lanes are rewriting. mentorEnabled() == false returns Enabled=false and
// zeroes, so the frontend can hide the card without a second call.
type MentorTruthPanel struct {
	Enabled   bool              `json:"enabled"`
	AsOfMs    int64             `json:"as_of_ms"` // server stamp; the card shows "as of HH:MM:SS CT"
	HTF       MentorHTFView     `json:"htf"`
	Trigger5m MentorTriggerView `json:"trigger_5m"`
	// Levels/Depth are ABSENT (omitempty) while the evaluator has not been
	// built yet, and `Computing` flags that state; once the evaluator exists an
	// empty level set is `[]` and an empty depth is `{}` (canon: absent ≠ []).
	Levels           []MentorLevelView `json:"levels,omitempty"`
	Depth            map[string]int    `json:"depth,omitempty"`
	Computing        bool              `json:"computing,omitempty"`
	DepthLine        string            `json:"depth_line"`
	Window           MentorWindowView  `json:"window"`
	DoneAfterWin     bool              `json:"done_after_win"`
	DoneAfterWinWhy  string            `json:"done_after_win_why,omitempty"`
	StopAfterLoss    bool              `json:"stop_after_loss"`
	StopAfterLossWhy string            `json:"stop_after_loss_why,omitempty"`
}

// MentorHTFView is the §5.4 4h/1h direction state + the gate verdict.
type MentorHTFView struct {
	FourHDir    string `json:"four_h_dir"`   // "long" | "short" | "" (no line)
	FourHSince  int64  `json:"four_h_since"` // ms since the 4h line last moved
	OneHDir     string `json:"one_h_dir"`
	OneHSince   int64  `json:"one_h_since"`
	Verdict     string `json:"verdict"` // "follow" | "sit-out" | "no-trigger"
	VerdictWhy  string `json:"verdict_why,omitempty"`
	VerdictSide string `json:"verdict_side,omitempty"` // the side to follow when ok
	GateActive  bool   `json:"gate_active"`            // HTFGateActive at now
}

// MentorTriggerView is the §5.1 5m trigger line.
type MentorTriggerView struct {
	Dir   string  `json:"dir"`
	Price float64 `json:"price"`
	Since int64   `json:"since"` // MovedAt (bucket open ms)
}

// MentorLevelView is one mentor key level in effect, with today's visits.
type MentorLevelView struct {
	Key         string  `json:"key"`
	Kind        string  `json:"kind"`
	Price       float64 `json:"price"`
	DrawnAt     int64   `json:"drawn_at"` // AtTime ms
	VisitsToday int     `json:"visits_today"`
}

// MentorWindowView is the trading-window knob's live state.
type MentorWindowView struct {
	Start   string `json:"start"`   // "08:30"
	Minutes int    `json:"minutes"` // 60; <=0 disabled
	Active  bool   `json:"active"`
	Ended   bool   `json:"ended"`
}

// MentorTruthSnapshot returns the live panel payload. It never blocks the tick:
// the evaluator state is read and copied under mentorEvalMu (the same critical
// section the tick takes) and released before the pure gate helpers run.
// ok=false = mentor mode OFF (or the evaluator has not been built yet).
func (at *AutoTrader) MentorTruthSnapshot(now time.Time) (MentorTruthPanel, bool) {
	p := MentorTruthPanel{
		AsOfMs: now.UnixMilli(),
		Levels: []MentorLevelView{},
		Depth:  map[string]int{},
	}
	if at == nil {
		return p, false
	}
	p.Enabled = at.mentorEnabled()
	if !p.Enabled {
		return p, false
	}

	// ── evaluator state: copied under the tick's own mutex ──────────────────
	at.mentorEvalMu.Lock()
	ev := at.mentorEval
	if ev != nil {
		st := ev.State

		// §5.4 HTF: the 4h and 1h trigger lines + the verdict + the gate state.
		htf := st.HTF
		p.HTF = MentorHTFView{
			FourHDir:   string(htf.FourH.Dir),
			FourHSince: htf.FourH.MovedAt,
			OneHDir:    string(htf.OneH.Dir),
			OneHSince:  htf.OneH.MovedAt,
			GateActive: mentor.HTFGateActive(now.UnixMilli(), ev.Cfg),
		}
		if ok, side, why := mentor.HTFVerdict(htf); ok {
			p.HTF.Verdict, p.HTF.VerdictSide, p.HTF.VerdictWhy = "follow", string(side), why
		} else if htf.FourH.Dir == "" {
			p.HTF.Verdict, p.HTF.VerdictWhy = "no-trigger", why
		} else {
			p.HTF.Verdict, p.HTF.VerdictWhy = "sit-out", why
		}

		// §5.1 5m trigger line.
		p.Trigger5m = MentorTriggerView{Dir: string(st.Trigger.Dir), Price: st.Trigger.Price, Since: st.Trigger.MovedAt}

		// Key levels in effect + today's visits (copied, not shared).
		for _, lv := range st.Levels {
			p.Levels = append(p.Levels, MentorLevelView{
				Key:         lv.Key,
				Kind:        string(lv.Kind),
				Price:       lv.Price,
				DrawnAt:     lv.AtTime,
				VisitsToday: st.Visits[lv.Key],
			})
		}

		// History depth snapshot (read-only: DepthMet does NOT consume the line).
		p.Depth = ev.Depths()
		if p.Depth == nil {
			p.Depth = map[string]int{}
		}
		p.DepthLine = ev.DepthMet()
	}
	at.mentorEvalMu.Unlock()

	// ── pure gate helpers (no evaluator state; safe outside the lock) ───────
	start, minutes := at.mentorWindowKnobs()
	active, _ := mentorWindowActive(start, minutes, now)
	ended, _ := at.mentorWindowEnded(now)
	p.Window = MentorWindowView{Start: start, Minutes: minutes, Active: active, Ended: ended}

	if trip, why := at.mentorDoneAfterWinTripped(); trip {
		p.DoneAfterWin, p.DoneAfterWinWhy = true, why
	}
	if trip, why := at.mentorStopAfterLossTrip(); trip {
		p.StopAfterLoss, p.StopAfterLossWhy = true, why
	}

	if ev == nil {
		// Evaluator not built yet (first 1m bar hasn't closed): levels/depth are
		// ABSENT (not `[]`/`{}`) and `computing` tells the card to say so, so the
		// frontend never dereferences a null list.
		p.Computing = true
		p.Levels = nil
		p.Depth = nil
		return p, true
	}
	return p, true
}
