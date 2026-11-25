package mentor

import (
	"encoding/json"
	"sort"
)

// ArmSnapshot is the per-arm evaluator snapshot (m134 DS-108, observability):
// the mentor state the arm was authored under, so a later audit can see the
// indicators the entry read. Written at authoring time onto the armed row.
// Everything is READ from State — nothing here trades, and nothing is ever
// fabricated: a zero/empty field means the evaluator had no value for it.
type ArmSnapshot struct {
	EMA9  float64 `json:"ema9,omitempty"`
	EMA34 float64 `json:"ema34,omitempty"`

	Trigger5m SnapshotTrigger `json:"trigger_5m,omitempty"`
	Trigger1h SnapshotTrigger `json:"trigger_1h,omitempty"`
	Trigger4h SnapshotTrigger `json:"trigger_4h,omitempty"`

	// HTFVerdict is "agree_long" / "agree_short" / "conflict" / "" (neither
	// line drawn yet).
	HTFVerdict string `json:"htf_verdict,omitempty"`

	ORB SnapshotORB `json:"orb,omitempty"`

	// Trendlines are the VALID and FORMING (two-anchor, not dead) lines.
	Trendlines []SnapshotTrendline `json:"trendlines,omitempty"`

	Swing SnapshotSwing `json:"swing,omitempty"`

	// NearestLevels is up to 4 levels nearest the entry price (from the
	// display set: key levels, old extremes, EMA, trigger lines, box edges).
	NearestLevels []SnapshotLevel `json:"nearest_levels,omitempty"`
}

// SnapshotTrigger is one trigger-line read.
type SnapshotTrigger struct {
	Dir   string  `json:"dir,omitempty"`
	Price float64 `json:"price,omitempty"`
}

// SnapshotORB is the §7 step-0 opening-range read.
type SnapshotORB struct {
	High    float64 `json:"high,omitempty"`
	Low     float64 `json:"low,omitempty"`
	Drawn   bool    `json:"drawn"`
	Escaped string  `json:"escaped,omitempty"`
}

// SnapshotTrendline is one trendline. Valid = the 3rd touch landed and the
// line is not dead; Forming = two anchors drawn, not yet valid, not dead.
type SnapshotTrendline struct {
	Side    string  `json:"side,omitempty"`
	P0Px    float64 `json:"p0_px,omitempty"`
	P1Px    float64 `json:"p1_px,omitempty"`
	Valid   bool    `json:"valid"`
	Forming bool    `json:"forming"`
}

// SnapshotSwing is the §8 4h-EMA34 swing read.
type SnapshotSwing struct {
	Line    float64 `json:"line,omitempty"`
	Pending bool    `json:"pending"`
	Open    bool    `json:"open"`
}

// SnapshotLevel is one nearest level.
type SnapshotLevel struct {
	Kind  string  `json:"kind,omitempty"`
	Price float64 `json:"price,omitempty"`
}

// ArmSnapshot renders the per-arm snapshot at authoring time. entryPrice
// anchors the nearest-levels pick; a 0 entry just skips that section.
func (s *State) ArmSnapshot(entryPrice float64) ArmSnapshot {
	ema9, ema34, _ := s.EMAUsed() // the EMA the engine used (1m or 2m by the execution schedule)
	a := ArmSnapshot{
		EMA9:      ema9,
		EMA34:     ema34,
		Trigger5m: SnapshotTrigger{Dir: string(s.Trigger.Dir), Price: s.Trigger.Price},
		Trigger1h: SnapshotTrigger{Dir: string(s.HTF.OneH.Dir), Price: s.HTF.OneH.Price},
		Trigger4h: SnapshotTrigger{Dir: string(s.HTF.FourH.Dir), Price: s.HTF.FourH.Price},
		ORB: SnapshotORB{
			High:    s.ORB.High,
			Low:     s.ORB.Low,
			Drawn:   s.ORB.Drawn,
			Escaped: string(s.ORB.Escaped),
		},
		Swing: SnapshotSwing{
			Line:    s.Swing.Line,
			Pending: s.Swing.Pending != nil,
			Open:    s.Swing.Pos != nil,
		},
	}
	if ok, dir := HTFAgreement(s.HTF); ok {
		a.HTFVerdict = "agree_" + string(dir)
	} else if s.HTF.FourH.Dir != "" && !oneHSilent(s.HTF) && s.HTF.OneH.Dir != "" {
		a.HTFVerdict = "conflict"
	}
	for _, tl := range s.ChartTrendlines {
		if tl.Dead {
			continue
		}
		a.Trendlines = append(a.Trendlines, SnapshotTrendline{
			Side:    string(tl.Side),
			P0Px:    tl.P0Px,
			P1Px:    tl.P1Px,
			Valid:   tl.ValidAt != 0,
			Forming: tl.ValidAt == 0,
		})
	}
	if entryPrice > 0 {
		type dist struct {
			l Level
			d float64
		}
		var ds []dist
		for _, l := range s.ChartLevels {
			if l.Price <= 0 {
				continue
			}
			d := l.Price - entryPrice
			if d < 0 {
				d = -d
			}
			ds = append(ds, dist{l: l, d: d})
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i].d < ds[j].d })
		for i := 0; i < len(ds) && i < 4; i++ {
			a.NearestLevels = append(a.NearestLevels, SnapshotLevel{
				Kind:  string(ds[i].l.Kind),
				Price: ds[i].l.Price,
			})
		}
	}
	return a
}

// JSON marshals the snapshot. It never fails for this struct's fields, but the
// caller still surfaces an error rather than writing a broken snapshot.
func (a ArmSnapshot) JSON() (string, error) {
	b, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
