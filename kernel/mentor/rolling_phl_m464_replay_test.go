package mentor

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// TestM464RollingReplay — the m464 PROVE harness (opt-in, READ-ONLY): it ticks
// the evaluator over an exported MNQ 1m tape exactly like the live loop (one
// Tick per closed 1m bar at its close instant, the last 2000 bars as the
// window — kernel.AISVPBarCount) with the LIVE MNQ strategy's mentor config
// (no mentor_tuning block, so DefaultConfig + the trader's rc resolvers at
// their unset defaults), and writes one JSON line per ROLLING PHL/PLH rung that
// left Tick, with its outcome read from the bars:
//
//	M464_REPLAY_CSV=/path/mnq_1m.csv      ot,o,h,l,c,v (open-time ms, CT tape)
//	M464_REPLAY_FROM=2026-10-02            first CT date whose rungs are reported
//	M464_REPLAY_OUT=/path/out.ndjson       a NEW file (an existing one is refused)
//
// It uses only base-compatible API (Tick, State.RollingPHL.ArmID, Refusals),
// so the SAME file runs on the pre-fix tree for the before/after diff.
//
// What it does NOT model (stated, not silent): the trader's own gates (window,
// news, done-after-win, daily loss, leg budget from real fills), restarts /
// re-seeds, and broker fills — a rung "fills" when a later 1m bar trades
// through its entry while it rests (placed at the close of its candle, live
// until the close of the candle whose tick cancels it). The outcome walks the
// 1m bars from the fill: stop first on a bar that touches both (conservative),
// else the target, else flat at the 15:00 CT session end (ENDED). The bot's
// leg-1 1R take-profit / break-even are NOT simulated; mfe_pts and hit_1r say
// whether 1R was reached before the exit bar.
func TestM464RollingReplay(t *testing.T) {
	path := os.Getenv("M464_REPLAY_CSV")
	if path == "" {
		t.Skip("set M464_REPLAY_CSV (+ M464_REPLAY_FROM, M464_REPLAY_OUT) to run the m464 rolling replay")
	}
	outPath := os.Getenv("M464_REPLAY_OUT")
	if outPath == "" {
		t.Fatal("M464_REPLAY_OUT is required")
	}
	if _, err := os.Stat(outPath); err == nil {
		t.Fatalf("%s exists — the replay writes only a NEW file", outPath)
	}
	from, err := time.ParseInLocation("2006-01-02", os.Getenv("M464_REPLAY_FROM"), ctime())
	if err != nil {
		t.Fatalf("M464_REPLAY_FROM: %v", err)
	}
	bars := m413Load(t, path)
	if len(bars) < 100 {
		t.Fatalf("tape too short: %d bars", len(bars))
	}

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RealFillOnly = true
	cfg.LossDeparturePts = 0
	cfg.LegBudgetEnabled = true
	cfg.LegResetOn = "close"
	cfg.LocTriggerFilter = true
	cfg.DayOffRecheck = false
	cfg.LvlRevisitMinPts = 0
	cfg.EmaMaxCross30m = 0
	e := New(cfg)

	type rung struct {
		ArmID   string  `json:"arm_id"`
		Placed  string  `json:"placed_ct"`
		RefBar  string  `json:"ref_bar_ct"`
		Colour  string  `json:"ref_colour"`
		Side    Side    `json:"side"`
		Entry   float64 `json:"entry"`
		Stop    float64 `json:"stop"`
		Target  float64 `json:"target"`
		Ended   string  `json:"ended_ct,omitempty"`
		EndWhy  string  `json:"end_why,omitempty"`
		Filled  string  `json:"filled_ct,omitempty"`
		Outcome string  `json:"outcome,omitempty"`
		ExitPx  float64 `json:"exit_px,omitempty"`
		R       float64 `json:"r,omitempty"`
		MFE     float64 `json:"mfe_pts,omitempty"` // best excursion before the exit bar
		Hit1R   bool    `json:"hit_1r,omitempty"`  // the leg-1 1R take-profit was reached before the exit bar
		placeI  int
		endI    int
	}
	var rungs []*rung
	byID := map[string]*rung{}
	ct := func(ms int64) string { return time.UnixMilli(ms).In(ctime()).Format("2006-01-02 15:04") }
	const window = 2000
	ledger, prevLedger := map[string]int{}, map[string]int{}
	for i := 1; i < len(bars); i++ {
		lo := i + 1 - window
		if lo < 0 {
			lo = 0
		}
		out := e.Tick(bars[lo:i+1], BarCloseInstant(bars[i]))
		if bars[i].OpenTime >= from.UnixMilli() {
			for k := range prevLedger { // a day rollover dropped the key: restart from 0
				if _, ok := e.State.Refusals[k]; !ok {
					if _, ok2 := e.State.Counters[k]; !ok2 {
						prevLedger[k] = 0
					}
				}
			}
			for _, m := range []map[string]int{e.State.Refusals, e.State.Counters} {
				for k, v := range m {
					if !strings.HasPrefix(k, "rolling_") {
						continue
					}
					if d := v - prevLedger[k]; d > 0 {
						ledger[k] += d
					} else if d < 0 { // day rollover reset
						ledger[k] += v
					}
					prevLedger[k] = v
				}
			}
		}
		for _, in := range out {
			switch {
			case in.Action == PlaceStopEntry && strings.HasPrefix(in.Reason, "PHL/PLH rolling pullback"):
				if bars[i].OpenTime < from.UnixMilli() {
					continue
				}
				colour := "doji"
				// The reference (exec) candle closed at RefBarMs; rebuild its
				// open/close from its 1m constituents (1m / 2m / 5m frame).
				tf := int64(in.ExecTFMin)
				if tf <= 0 {
					tf = 1
				}
				refOpen := in.RefBarMs + 1 - tf*60_000
				var ref market.Kline
				for k := i; k >= 0 && bars[k].OpenTime >= refOpen; k-- {
					if bars[k].OpenTime > in.RefBarMs {
						continue
					}
					if ref.Close == 0 {
						ref.Close = bars[k].Close
					}
					ref.Open = bars[k].Open
					ref.OpenTime = bars[k].OpenTime
				}
				if ref.Close > ref.Open {
					colour = "green"
				} else if ref.Close < ref.Open {
					colour = "red"
				}
				r := &rung{ArmID: in.ArmID, Placed: ct(bars[i].CloseTime + 1), RefBar: ct(ref.OpenTime), Colour: colour,
					Side: in.Side, Entry: in.Price, Stop: in.Stop, Target: in.Target, placeI: i, endI: -1}
				rungs = append(rungs, r)
				byID[in.ArmID] = r
			case in.Action == CancelArm:
				if r, ok := byID[in.ArmID]; ok && r.endI < 0 {
					r.endI = i
					r.Ended = ct(bars[i].CloseTime + 1)
					r.EndWhy = in.Reason
				}
			}
		}
	}
	// Fill + outcome from the bars.
	for _, r := range rungs {
		last := r.endI
		if last < 0 {
			last = len(bars) - 1
		}
		fi := -1
		for k := r.placeI + 1; k <= last && k < len(bars); k++ {
			if r.Side == SideLong && bars[k].High >= r.Entry || r.Side == SideShort && bars[k].Low <= r.Entry {
				fi = k
				break
			}
		}
		if fi < 0 {
			continue
		}
		r.Filled = ct(bars[fi].OpenTime)
		day := time.UnixMilli(bars[fi].OpenTime).In(ctime())
		flat := time.Date(day.Year(), day.Month(), day.Day(), 15, 0, 0, 0, ctime()).UnixMilli()
		if bars[fi].OpenTime >= flat {
			flat = time.Date(day.Year(), day.Month(), day.Day()+1, 15, 0, 0, 0, ctime()).UnixMilli()
		}
		risk := r.Entry - r.Stop
		if r.Side == SideShort {
			risk = r.Stop - r.Entry
		}
		for k := fi; k < len(bars); k++ {
			b := bars[k]
			if b.OpenTime >= flat {
				r.Outcome, r.ExitPx = "ENDED_15CT", bars[k-1].Close
				break
			}
			stopHit := r.Side == SideLong && b.Low <= r.Stop || r.Side == SideShort && b.High >= r.Stop
			if !stopHit {
				fav := b.High - r.Entry
				if r.Side == SideShort {
					fav = r.Entry - b.Low
				}
				if fav > r.MFE {
					r.MFE = fav
				}
			}
			tgtHit := r.Side == SideLong && b.High >= r.Target || r.Side == SideShort && b.Low <= r.Target
			if stopHit {
				r.Outcome, r.ExitPx = "STOP", r.Stop
				break
			}
			if tgtHit {
				r.Outcome, r.ExitPx = "TARGET", r.Target
				break
			}
		}
		r.Hit1R = risk > 0 && r.MFE >= risk
		if r.Outcome != "" && risk > 0 {
			pts := r.ExitPx - r.Entry
			if r.Side == SideShort {
				pts = r.Entry - r.ExitPx
			}
			r.R = float64(int(pts/risk*100)) / 100
		}
	}
	f, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range rungs {
		_ = enc.Encode(r)
	}
	var names []string
	for k, v := range ledger {
		names = append(names, fmt.Sprintf("%s=%d", k, v))
	}
	sort.Strings(names)
	_ = enc.Encode(map[string]any{"summary": true, "rungs": len(rungs), "rolling_ledger": names})
	t.Logf("m464 replay: %d rolling rungs written to %s; rolling ledger since FROM %v", len(rungs), outPath, names)
}
