package mentor

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// TestM60WickReplay — the m465 P5 PROVE harness (opt-in, READ-ONLY): it ticks
// the evaluator over a real session exported by deploy/realday_export_day.py
// (mnq_realday.json.gz, the "MNQ 12-26" 1m bars), one Tick per closed 1m bar
// at its close instant with the last 2000 bars as the window, ONCE with the
// knob wick_microscalp OFF (the shipped default) and ONCE ON, and logs every
// entry intent that appears, disappears or changes, plus the wick_* counters.
//
//	M60_REPLAY_DIR=/tmp/realday-1030   the export directory
//	M60_REPLAY_DAY=2026-10-30          the CME session day (prior 17:00 → 16:00 CT)
//
// A changed entry's outcome is walked on the 1m bars (stop first on a bar that
// touches both — conservative; else the target; else flat at the session end):
// pts are per contract, before costs. Not modeled: the trader's own gates,
// restarts, real fills, leg-1 1R take-profit / break-even. Writes nothing.
func TestM60WickReplay(t *testing.T) {
	dir := os.Getenv("M60_REPLAY_DIR")
	if dir == "" {
		t.Skip("set M60_REPLAY_DIR + M60_REPLAY_DAY to run the m465 wick replay")
	}
	day, err := time.ParseInLocation("2006-01-02", os.Getenv("M60_REPLAY_DAY"), ctime())
	if err != nil {
		t.Fatalf("M60_REPLAY_DAY: %v", err)
	}
	prev := day.AddDate(0, 0, -1)
	fromMs := time.Date(prev.Year(), prev.Month(), prev.Day(), 17, 0, 0, 0, ctime()).UnixMilli()
	toMs := time.Date(day.Year(), day.Month(), day.Day(), 16, 0, 0, 0, ctime()).UnixMilli()
	bars := m60LoadRealDay(t, filepath.Join(dir, "mnq_realday.json.gz"))

	type entry struct {
		At, Setup           string
		Side                Side
		Price, Stop, Target float64
		Anchor              string
		Outcome             string
		Pts                 float64
		atMs                int64
	}
	run := func(on bool) ([]entry, map[string]int, map[string]int) {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.WickMicroscalpEnabled = on
		e := New(cfg)
		var out []entry
		for i := range bars {
			if bars[i].OpenTime < fromMs-2*86_400_000 || bars[i].CloseTime >= toMs {
				continue
			}
			lo := i + 1 - 2000
			if lo < 0 {
				lo = 0
			}
			ins := e.Tick(bars[lo:i+1], BarCloseInstant(bars[i]))
			if bars[i].OpenTime < fromMs {
				continue
			}
			for _, in := range ins {
				if in.Action != PlaceStopEntry && in.Action != PlaceStopLimitEntry {
					continue
				}
				en := entry{At: time.UnixMilli(bars[i].CloseTime + 1).In(ctime()).Format("01-02 15:04"), Setup: in.Setup, Side: in.Side,
					Price: in.Price, Stop: in.Stop, Target: in.Target, Anchor: LocationOf(in.AnchorKey), atMs: bars[i].CloseTime}
				en.Outcome, en.Pts = m60Walk(bars[i+1:], en.Side, en.Price, en.Stop, en.Target, toMs)
				out = append(out, en)
			}
		}
		return out, e.State.Refusals, e.State.Counters
	}
	off, _, _ := run(false)
	on, refs, ctrs := run(true)
	key := func(x entry) string { return fmt.Sprintf("%s %s %s %.2f", x.At, x.Setup, x.Side, x.Price) }
	offBy := map[string]entry{}
	for _, x := range off {
		offBy[key(x)] = x
	}
	onBy := map[string]entry{}
	for _, x := range on {
		onBy[key(x)] = x
	}
	var lines []string
	sum := 0.0
	for k, x := range onBy {
		if o, ok := offBy[k]; !ok {
			lines = append(lines, fmt.Sprintf("APPEARS   %s stop %.2f target %.2f at %q → %s %+.2f pts", k, x.Stop, x.Target, x.Anchor, x.Outcome, x.Pts))
			sum += x.Pts
		} else if o.Target != x.Target || o.Stop != x.Stop {
			lines = append(lines, fmt.Sprintf("CHANGED   %s target %.2f→%.2f stop %.2f→%.2f → OFF %s %+.2f / ON %s %+.2f pts", k, o.Target, x.Target, o.Stop, x.Stop, o.Outcome, o.Pts, x.Outcome, x.Pts))
			sum += x.Pts - o.Pts
		}
	}
	for k, o := range offBy {
		if _, ok := onBy[k]; !ok {
			lines = append(lines, fmt.Sprintf("DISAPPEARS %s stop %.2f target %.2f → was %s %+.2f pts", k, o.Stop, o.Target, o.Outcome, o.Pts))
			sum -= o.Pts
		}
	}
	sort.Strings(lines)
	var wk []string
	for k, v := range refs {
		if strings.HasPrefix(k, "wick_") {
			wk = append(wk, fmt.Sprintf("%s=%d", k, v))
		}
	}
	for k, v := range ctrs {
		if strings.HasPrefix(k, "wick_") {
			wk = append(wk, fmt.Sprintf("%s=%d", k, v))
		}
	}
	sort.Strings(wk)
	t.Logf("m465 wick replay %s: entries OFF %d / ON %d; %d moved; net %+.2f pts (1 contract, before costs); wick counters: %s\n%s",
		day.Format("2006-01-02"), len(off), len(on), len(lines), sum, strings.Join(wk, " "), strings.Join(lines, "\n"))
}

// m60Walk walks a resting stop entry over the following 1m bars: it fills when
// a bar trades through the entry; then stop first on a bar touching both,
// else the target, else flat at the session end. Unfilled → "UNFILLED".
func m60Walk(after []market.Kline, side Side, entry, stop, target float64, endMs int64) (string, float64) {
	filled := false
	last := entry
	for _, b := range after {
		if b.CloseTime >= endMs {
			break
		}
		if !filled {
			if side == SideLong && b.High >= entry || side == SideShort && b.Low <= entry {
				filled = true
			} else {
				continue
			}
		}
		last = b.Close
		hitStop := side == SideLong && b.Low <= stop || side == SideShort && b.High >= stop
		hitTgt := target != 0 && (side == SideLong && b.High >= target || side == SideShort && b.Low <= target)
		switch {
		case hitStop:
			return "STOP", m60Pts(side, entry, stop)
		case hitTgt:
			return "TARGET", m60Pts(side, entry, target)
		}
	}
	if !filled {
		return "UNFILLED", 0
	}
	return "ENDED", m60Pts(side, entry, last)
}

func m60Pts(side Side, entry, exit float64) float64 {
	if side == SideShort {
		return entry - exit
	}
	return exit - entry
}

// m60LoadRealDay reads the "MNQ 12-26" 1m bars of a realday export.
func m60LoadRealDay(t *testing.T, path string) []market.Kline {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	var doc struct {
		Bars map[string][][]json.RawMessage `json:"bars"`
	}
	if err := json.NewDecoder(gz).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	var out []market.Kline
	for _, r := range doc.Bars["1m"] {
		if len(r) < 6 {
			continue
		}
		var ot, o, h, l, c float64
		var contract string
		if json.Unmarshal(r[0], &ot) != nil || json.Unmarshal(r[1], &o) != nil || json.Unmarshal(r[2], &h) != nil ||
			json.Unmarshal(r[3], &l) != nil || json.Unmarshal(r[4], &c) != nil || json.Unmarshal(r[5], &contract) != nil {
			continue
		}
		if contract != "MNQ 12-26" {
			continue
		}
		oo := int64(ot)
		out = append(out, market.Kline{OpenTime: oo, CloseTime: oo + 59_999, Open: o, High: h, Low: l, Close: c, Final: true})
	}
	if len(out) < 100 {
		t.Fatalf("%s: only %d MNQ 12-26 bars", path, len(out))
	}
	return out
}
