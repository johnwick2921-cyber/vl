package trader

import (
	"math"
	"time"

	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// MentorTruthPanel is the read-only "what trades" snapshot the planner page and
// the trader dashboard render. Every field is derived from the LIVE evaluator
// state (under mentorEvalMu) or the pure gate helpers — never the internals the
// other lanes are rewriting. mentorEnabled() == false returns Enabled=false and
// zeroes, so the frontend can hide the card without a second call.
type MentorTruthPanel struct {
	Enabled bool  `json:"enabled"`
	AsOfMs  int64 `json:"as_of_ms"` // server stamp; the card shows "as of HH:MM:SS CT"
	// Symbol (UG-2, release #24) is the instrument the evaluator reads (the
	// trader's futures symbol). The chart draws the mentor overlays only on a
	// tab showing this symbol — a level is a price on ONE instrument.
	Symbol    string            `json:"symbol"`
	HTF       MentorHTFView     `json:"htf"`
	Trigger5m MentorTriggerView `json:"trigger_5m"`
	// Levels/Depth are ABSENT (omitempty) while the evaluator has not been
	// built yet, and `Computing` flags that state; once the evaluator exists an
	// empty level set is `[]` and an empty depth is `{}` (canon: absent ≠ []).
	Levels    *[]MentorLevelView `json:"levels,omitempty"`
	Depth     map[string]int     `json:"depth,omitempty"`
	Computing bool               `json:"computing,omitempty"`
	// ChartSeededAt (m422) is the ms of the last close the boot display seed
	// drew from; set only while no real Tick has run since. The chart shows
	// "as of last close HH:MM CT".
	ChartSeededAt    int64            `json:"chart_seeded_at,omitempty"`
	DepthLine        string           `json:"depth_line"`
	Window           MentorWindowView `json:"window"`
	DoneAfterWin     bool             `json:"done_after_win"`
	DoneAfterWinWhy  string           `json:"done_after_win_why,omitempty"`
	StopAfterLoss    bool             `json:"stop_after_loss"`
	StopAfterLossWhy string           `json:"stop_after_loss_why,omitempty"`

	// T7 (release #22) — the OTHER places the evaluator trades, read from the
	// same tick, so the chart can draw them. Pointers: ABSENT while computing,
	// `[]` / null-free once the evaluator exists (canon: absent ≠ []).
	Trendlines *[]MentorTrendlineView `json:"trendlines,omitempty"`
	ISBBoxes   *[]MentorRangeView     `json:"isb_boxes,omitempty"`
	ORB        *MentorRangeView       `json:"orb,omitempty"`   // absent until the 08:30–08:32 range is drawn
	Swing      *MentorSwingView       `json:"swing,omitempty"` // absent until the 4h EMA 34 is warm
	// EMASeries (m134) is the per-bar EMA 9/34 curve (points) — ABSENT while
	// computing, `[]` once built.
	EMASeries *[]MentorEMAPoint `json:"ema_series,omitempty"`
}

// MentorTrendlineView is one live trendline: FORMING (two anchors, no 3rd
// touch yet — valid=false) or VALID (3rd touch seen, no 5m close through —
// valid=true). Dead lines (broken by a 5m close) are never sent.
type MentorTrendlineView struct {
	Side    string  `json:"side"` // "long" = support (rising lows), "short" = resistance
	P0Ms    int64   `json:"p0_ms"`
	P0Px    float64 `json:"p0_px"`
	P1Ms    int64   `json:"p1_ms"`
	P1Px    float64 `json:"p1_px"`
	NowMs   int64   `json:"now_ms"`
	NowPx   float64 `json:"now_px"`
	InLevel bool    `json:"in_level"` // false = suppressed this bar (inside a box: the box wins)
	Valid   bool    `json:"valid"`    // m134: true = 3rd touch seen (a location); false = forming
}

// MentorEMAPoint is one point of the EMA 9/34 curve (m134).
type MentorEMAPoint struct {
	Ms    int64   `json:"ms"`
	Ema9  float64 `json:"ema9"`
	Ema34 float64 `json:"ema34"`
	// TFMin (M401) is the frame the engine used for this point: 1 or 2. Ms is
	// the open of that frame's bar (closed 2m bars only in a 2m period).
	TFMin int `json:"tf_min"`
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
	// StopSource / MovedBE (UG-1, release #24) — the FILLED leg only. "broker"
	// = Stop is the leg's working stop read from NinjaTrader's order book;
	// "entry" = the book could not be read, so Stop is the stop the leg was
	// entered with (it may already have moved). MovedBE = the evaluator has
	// emitted its +1R break-even move (a request; the book is the proof).
	StopSource string `json:"stop_source,omitempty"`
	MovedBE    bool   `json:"moved_be,omitempty"`
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
	// FormedMs / TF (m421b, additive) — box-edge levels only: the box's
	// formation-bar open (ms) and its frame ("1m"/"2m"/...), so the chart can
	// draw the box as a rectangle. No ended_ms: an escaped box is dropped
	// from the set, so a listed box is always live.
	FormedMs   int64     `json:"formed_ms,omitempty"`
	TF         string    `json:"tf,omitempty"`
	MergedFrom []float64 `json:"merged_from,omitempty"` // always empty since r22 (DisplayLevels merges nothing); kept for wire compatibility
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
		Depth:  map[string]int{},
	}
	if at == nil {
		return p, false
	}
	p.Symbol = at.futuresSymbol()
	p.Enabled = at.mentorEnabled()
	if !p.Enabled {
		return p, false
	}

	// ── evaluator state: copied under the tick's own mutex ──────────────────
	var swingArmID string // UG-1: the filled swing leg's arm, read after the unlock
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

		// Drawn levels: the DISPLAY set (mentor.DisplayLevels) — since r22 the
		// levels exactly as the trading code reads them; nothing is merged, so
		// merged_from is always empty (UG-8, release #24: the old 20-pt merge
		// comment described code that no longer exists).
		// T7: the chart set is the tick's set AFTER every append (trigger
		// lines, EMA 34 location, box edges, trendlines); before the first
		// Tick it is empty and the trading set is all there is.
		drawn := st.ChartLevels
		if len(drawn) == 0 {
			drawn = st.Levels
		}
		lvs := []MentorLevelView{} // built-but-empty serialises as [] (absent ≠ [])
		p.ChartSeededAt = st.ChartSeededAt
		for _, lv := range mentor.DisplayLevels(drawn, ev.Cfg.KeyLevelPrunePts) {
			boxTF := ""
			if lv.FormedMs > 0 {
				boxTF = lv.TF
				if boxTF == "" {
					boxTF = ev.Cfg.Box.TF
				}
				if boxTF == "" {
					boxTF = "1m"
				}
			}
			lvs = append(lvs, MentorLevelView{
				FormedMs:    lv.FormedMs,
				TF:          boxTF,
				Key:         lv.Key,
				Kind:        string(lv.Kind),
				Price:       lv.Price,
				DrawnAt:     lv.AtTime,
				VisitsToday: st.Visits[lv.Key],
				MergedFrom:  lv.MergedFrom,
			})
		}
		p.Levels = &lvs

		p.Trendlines, p.ISBBoxes, p.ORB, p.Swing = mentorChartObjects(&st, drawn)
		p.EMASeries = mentorEMASeries(&st)
		if p.Swing != nil && p.Swing.Pos != nil && st.Swing.Pos != nil {
			swingArmID = st.Swing.Pos.ArmID
		}

		// History depth snapshot (read-only: DepthMet does NOT consume the line).
		p.Depth = ev.Depths()
		if p.Depth == nil {
			p.Depth = map[string]int{}
		}
		p.DepthLine = ev.DepthMet()
	}
	at.mentorEvalMu.Unlock()

	// UG-1: the filled swing leg's stop is drawn where it RESTS (the NT8 book),
	// read outside the tick's mutex — a DB row and a book snapshot.
	if p.Swing != nil && p.Swing.Pos != nil {
		at.mentorSwingStopTruth(p.Swing.Pos, swingArmID)
	}

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

// mentorEMASeries copies the evaluator's per-bar EMA 9/34 curve (display only,
// m134). nil when the evaluator has not built one yet.
func mentorEMASeries(st *mentor.State) *[]MentorEMAPoint {
	if len(st.ChartEMA) == 0 {
		return nil
	}
	pts := make([]MentorEMAPoint, 0, len(st.ChartEMA))
	for _, c := range st.ChartEMA {
		pts = append(pts, MentorEMAPoint{Ms: c.Ms, Ema9: c.Ema9, Ema34: c.Ema34, TFMin: c.TFMin})
	}
	return &pts
}

// mentorChartObjects (T7, release #22) copies the evaluator's non-level
// objects into the panel views. Pure over the copied State; nothing is drawn
// that the evaluator did not hold this tick.
func mentorChartObjects(st *mentor.State, drawn []mentor.Level) (*[]MentorTrendlineView, *[]MentorRangeView, *MentorRangeView, *MentorSwingView) {
	tls := []MentorTrendlineView{}
	for _, tl := range st.ChartTrendlines {
		if tl.Dead || tl.P1T == tl.P0T {
			continue // broken by a 5m close, or a degenerate (flat) line
		}
		v := MentorTrendlineView{
			Side: string(tl.Side),
			P0Ms: tl.P0T, P0Px: tl.P0Px,
			P1Ms: tl.P1T, P1Px: tl.P1Px,
			Valid: tl.ValidAt != 0, // m134: FORMING (no 3rd touch) is valid=false
		}
		// The level the evaluator placed for this line this bar (only a VALID line
		// is a location; a forming line has no level). None = suppressed (box).
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
			swing.Pos = &MentorSwingLegView{Side: string(o.Side), Entry: o.Entry, Stop: o.Stop, Target: o.Target,
				StopSource: "entry", MovedBE: o.MovedBE}
		}
	}
	return &tls, &boxes, orb, swing
}

// mentorSwingStopTruth (UG-1, release #24) replaces the filled swing leg's
// drawn stop with the stop that is really working at NinjaTrader.
//
// WHY: the evaluator's swing position keeps the stop it was ENTERED with.
// Its +1R break-even move (kernel/mentor/swing4h.go manageSwingPosition) sets
// only MovedBE and emits the intent; the trader's mentorMoveStopBE sends the
// move to NT8 (moveStopWire) and writes no price anywhere else. So after a BE
// move the chart kept drawing the original stop. The moved stop lives in ONE
// place: NT8's order book (the swing row's "<signal>-sl"). The evaluator's
// Stop is read by nothing but this panel, and it is left untouched — a refused
// move must keep showing the stop that is really there.
//
// No book, no row, no working stop → the entry stop stays, labelled
// stop_source "entry" — never a guessed break-even.
func (at *AutoTrader) mentorSwingStopTruth(v *MentorSwingLegView, armID string) {
	if v == nil {
		return
	}
	v.StopSource = "entry"
	if px, ok := at.mentorSwingBrokerStop(armID); ok {
		v.Stop, v.StopSource = px, "broker"
	}
}

// mentorSwingBrokerStop reads the swing arm's working stop from the bound
// account's NT8 order book: registry → ledger row → signal → "<signal>-sl".
func (at *AutoTrader) mentorSwingBrokerStop(armID string) (float64, bool) {
	if at == nil || armID == "" || at.store == nil || at.store.ArmedOrders() == nil {
		return 0, false
	}
	arm, ok := at.mentorLiveArmFor(armID)
	if !ok {
		return 0, false
	}
	var r store.ArmedOrderDB
	if err := at.store.ArmedOrders().DB().First(&r, arm.RowID).Error; err != nil || r.SignalID == "" {
		return 0, false
	}
	book, _, ok := mentorStopBook(at.armedTrader())
	if !ok {
		return 0, false
	}
	working := ntwire.OrderSnapshotPayload{Orders: book.WorkingOrders()}
	for _, o := range mentorBrokerStopOrders(working, r.SignalID, 0) {
		if o.StopPrice > 0 {
			return o.StopPrice, true
		}
	}
	return 0, false
}
