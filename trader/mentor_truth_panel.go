package trader

import (
	"math"
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

	// T7 (release #22) — the OTHER places the evaluator trades, read from the
	// same tick, so the chart can draw them. Pointers: ABSENT while computing,
	// `[]` / null-free once the evaluator exists (canon: absent ≠ []).
	Trendlines *[]MentorTrendlineView `json:"trendlines,omitempty"`
	ISBBoxes   *[]MentorRangeView     `json:"isb_boxes,omitempty"`
	ORB        *MentorRangeView       `json:"orb,omitempty"`   // absent until the 08:30–08:32 range is drawn
	Swing      *MentorSwingView       `json:"swing,omitempty"` // absent until the 4h EMA 34 is warm
}

// MentorTrendlineView is one VALID, live trendline (3rd touch seen, no 5m close
// through): the two swing anchors and the value at the current bar.
type MentorTrendlineView struct {
	Side    string  `json:"side"` // "long" = support (rising lows), "short" = resistance
	P0Ms    int64   `json:"p0_ms"`
	P0Px    float64 `json:"p0_px"`
	P1Ms    int64   `json:"p1_ms"`
	P1Px    float64 `json:"p1_px"`
	NowMs   int64   `json:"now_ms"`
	NowPx   float64 `json:"now_px"`
	InLevel bool    `json:"in_level"` // false = suppressed this bar (inside a box: the box wins)
}

// MentorRangeView is a high/low range the evaluator holds (an ISB rest box or
// the ORB).
type MentorRangeView struct {
	Label  string  `json:"label"` // "ISB 5m" / "ISB 15m" / "ISB 30m" / "ORB"
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Dir    string  `json:"dir,omitempty"`
	AtMs   int64   `json:"at_ms,omitempty"`
	Status string  `json:"status,omitempty"` // ORB: "inside" / "escaped long" / "escaped short"
}

// MentorSwingView is the §8 swing line (the 4h EMA 34 in force) and, when one
// exists, the swing's resting order or filled leg with its stop and target.
type MentorSwingView struct {
	Line    float64             `json:"line"`
	Pending *MentorSwingLegView `json:"pending,omitempty"`
	Pos     *MentorSwingLegView `json:"pos,omitempty"`
}

// MentorSwingLegView is a swing order or position: entry, stop and target.
type MentorSwingLegView struct {
	Side   string  `json:"side"`
	Entry  float64 `json:"entry"`
	Stop   float64 `json:"stop"`
	Target float64 `json:"target"`
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
	Key         string    `json:"key"`
	Kind        string    `json:"kind"`
	Price       float64   `json:"price"`
	DrawnAt     int64     `json:"drawn_at"` // AtTime ms
	VisitsToday int       `json:"visits_today"`
	MergedFrom  []float64 `json:"merged_from,omitempty"` // raw old-extreme prices this drawn line absorbed
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

		// Drawn levels: the DISPLAY set (course 5.3 @15:59–16:50 — lines closer
		// than 20 pts are one line). TRADING still reads st.Levels (raw); only
		// the panel draws the cleaned view, with every absorbed price named in
		// merged_from.
		// T7: the chart set is the tick's set AFTER every append (trigger
		// lines, EMA 34 location, box edges, trendlines); before the first
		// Tick it is empty and the trading set is all there is.
		drawn := st.ChartLevels
		if len(drawn) == 0 {
			drawn = st.Levels
		}
		for _, lv := range mentor.DisplayLevels(drawn, ev.Cfg.KeyLevelPrunePts) {
			p.Levels = append(p.Levels, MentorLevelView{
				Key:         lv.Key,
				Kind:        string(lv.Kind),
				Price:       lv.Price,
				DrawnAt:     lv.AtTime,
				VisitsToday: st.Visits[lv.Key],
				MergedFrom:  lv.MergedFrom,
			})
		}

		p.Trendlines, p.ISBBoxes, p.ORB, p.Swing = mentorChartObjects(&st, drawn)

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

// mentorChartObjects (T7, release #22) copies the evaluator's non-level
// objects into the panel views. Pure over the copied State; nothing is drawn
// that the evaluator did not hold this tick.
func mentorChartObjects(st *mentor.State, drawn []mentor.Level) (*[]MentorTrendlineView, *[]MentorRangeView, *MentorRangeView, *MentorSwingView) {
	tls := []MentorTrendlineView{}
	for _, tl := range st.ChartTrendlines {
		if tl.ValidAt == 0 || tl.Dead || tl.P1T == tl.P0T {
			continue // not yet a location (no 3rd touch) or closed through
		}
		v := MentorTrendlineView{Side: string(tl.Side), P0Ms: tl.P0T, P0Px: tl.P0Px, P1Ms: tl.P1T, P1Px: tl.P1Px}
		// The level the evaluator placed for this line this bar (same line =
		// same value at the level's bar). None = suppressed (inside a box).
		for _, lv := range drawn {
			if lv.Kind != mentor.KindTrendline || lv.AtTime <= tl.P1T {
				continue
			}
			at := tl.P0Px + (tl.P1Px-tl.P0Px)/float64(tl.P1T-tl.P0T)*float64(lv.AtTime-tl.P0T)
			if math.Abs(at-lv.Price) < 1e-6 {
				v.NowMs, v.NowPx, v.InLevel = lv.AtTime, lv.Price, true
				break
			}
		}
		tls = append(tls, v)
	}
	boxes := []MentorRangeView{}
	for _, b := range []struct {
		label string
		box   *mentor.ISBBox
	}{{"ISB 5m", st.ISBBox}, {"ISB 15m", st.ISBBox15m}, {"ISB 30m", st.ISBBox30m}} {
		if b.box != nil && b.box.High > 0 && b.box.Low > 0 {
			boxes = append(boxes, MentorRangeView{Label: b.label, High: b.box.High, Low: b.box.Low, Dir: string(b.box.Dir), AtMs: b.box.AtTime})
		}
	}
	var orb *MentorRangeView
	if st.ORB.Drawn && st.ORB.High > 0 && st.ORB.Low > 0 {
		status := "inside"
		if st.ORB.Escaped != "" {
			status = "escaped " + string(st.ORB.Escaped)
		}
		orb = &MentorRangeView{Label: "ORB", High: st.ORB.High, Low: st.ORB.Low, AtMs: st.ORB.Day, Status: status}
	}
	var swing *MentorSwingView
	if st.Swing.Line > 0 && st.Swing.EmaCount >= mentor.FourHEMA34Min {
		swing = &MentorSwingView{Line: st.Swing.Line}
		if o := st.Swing.Pending; o != nil {
			swing.Pending = &MentorSwingLegView{Side: string(o.Side), Entry: o.Entry, Stop: o.Stop, Target: o.Target}
		}
		if o := st.Swing.Pos; o != nil {
			swing.Pos = &MentorSwingLegView{Side: string(o.Side), Entry: o.Entry, Stop: o.Stop, Target: o.Target}
		}
	}
	return &tls, &boxes, orb, swing
}
