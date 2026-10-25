package trader

import (
	"encoding/json"
	"testing"
	"time"

	"vl/kernel/mentor"
)

// m421b: a latched box's edges carry formed_ms (Box.FormedAtMs) and tf in the
// /api/mentor/truth JSON, so the chart can draw the box as a rectangle.
// Production path: a real Evaluator.Tick, then MentorTruthSnapshot, then the
// wire. Mutant: drop FormedMs/TF from BoxEdgeLocations -> RED.
func TestMentorTruthBoxEdgesCarryFormedMsAndTF(t *testing.T) {
	at := truthAT(t)
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = false
	at.mentorEval = mentor.New(cfg)
	bars := mentorChartTape()
	now := mentor.BarCloseInstant(bars[len(bars)-1])
	at.mentorEval.Tick(bars, now)
	p, ok := at.MentorTruthSnapshot(time.UnixMilli(now))
	if !ok {
		t.Fatalf("snapshot not ok")
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Levels []struct {
			Kind     string `json:"kind"`
			FormedMs int64  `json:"formed_ms"`
			EndedMs  int64  `json:"ended_ms"`
			TF       string `json:"tf"`
		} `json:"levels"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	edges := 0
	for _, l := range wire.Levels {
		if l.Kind != "ftgh_edge" && l.Kind != "ftgl_edge" {
			if l.FormedMs != 0 || l.TF != "" {
				t.Errorf("non-box level %s must not carry formed_ms/tf: %+v", l.Kind, l)
			}
			continue
		}
		edges++
		if l.FormedMs <= 0 || l.TF != "1m" || l.EndedMs != 0 {
			t.Errorf("box edge %+v: want formed_ms>0, tf=1m, no ended_ms", l)
		}
	}
	if edges == 0 {
		t.Fatalf("no box edges on the wire: %s", b)
	}
}
