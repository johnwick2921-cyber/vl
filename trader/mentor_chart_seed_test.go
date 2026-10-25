package trader

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
)

// m422 (C5): after a restart on a CLOSED market no Tick runs, so the truth
// panel must still carry the last close's overlays from the boot display seed.
// Production call site: AutoTrader.mentorSeedChartDisplay (called from
// mentorSeedAtStart) -> MentorTruthSnapshot.
func TestMentorTruthAfterClosedMarketSeedCarriesOverlays(t *testing.T) {
	at := truthAT(t)
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = false
	at.mentorEval = mentor.New(cfg)
	ev := at.mentorEval
	ev.State.HTF = mentor.HTF{FourH: mentor.TriggerLine{Dir: mentor.SideLong, Price: 90, MovedAt: 1}}
	// 260 flat warm-up bars before the trendline tape so the EMA 34 is warm
	// (a flat bar makes no swing, so the tape's lines are unchanged).
	core := mentorChartTape()
	var tape []market.Kline
	for i := 260; i >= 1; i-- {
		o := core[0].OpenTime - int64(i)*60_000
		tape = append(tape, market.Kline{OpenTime: o, CloseTime: o + 59_000, Open: 100, High: 100.5, Low: 100, Close: 100.2})
	}
	tape = append(tape, core...)
	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = nil
	defer func() { market.FuturesBarsProvider = old }()

	at.mentorSeedChartDisplay(tape) // NO Tick ever runs

	p, ok := at.MentorTruthSnapshot(time.UnixMilli(mentor.BarCloseInstant(tape[len(tape)-1])))
	if !ok {
		t.Fatal("snapshot not ok")
	}
	if p.Levels == nil || len(*p.Levels) == 0 {
		t.Fatalf("closed-market seed: levels empty: %+v", p.Levels)
	}
	if p.Trendlines == nil || len(*p.Trendlines) == 0 {
		t.Fatalf("closed-market seed: no trendlines")
	}
	if p.EMASeries == nil || len(*p.EMASeries) == 0 {
		t.Fatalf("closed-market seed: no ema_series")
	}
	if p.ChartSeededAt != mentor.BarCloseInstant(tape[len(tape)-1]) {
		t.Fatalf("chart_seeded_at = %d", p.ChartSeededAt)
	}
	if len(ev.State.Levels) != 0 {
		t.Fatalf("State.Levels must stay empty after the display seed, got %d", len(ev.State.Levels))
	}
	// The first real Tick clears the marker.
	ev.Tick(tape, mentor.BarCloseInstant(tape[len(tape)-1]))
	p, _ = at.MentorTruthSnapshot(time.Now())
	if p.ChartSeededAt != 0 {
		t.Fatalf("a real Tick must clear chart_seeded_at, got %d", p.ChartSeededAt)
	}
}

// (d) the omitempty fix: a built evaluator with nothing to draw serialises
// "levels":[] (present, empty); a not-yet-built one omits the key (computing).
func TestMentorTruthEmptyLevelsSerialiseAsEmptyArray(t *testing.T) {
	at := truthAT(t)
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	at.mentorEval = mentor.New(cfg)
	p, ok := at.MentorTruthSnapshot(time.Now())
	if !ok || p.Computing {
		t.Fatalf("ok=%v computing=%v", ok, p.Computing)
	}
	b, _ := json.Marshal(p)
	if !strings.Contains(string(b), `"levels":[]`) {
		t.Fatalf("built-but-empty levels must be [] not absent: %s", b)
	}
	at.mentorEval = nil
	p, _ = at.MentorTruthSnapshot(time.Now())
	b, _ = json.Marshal(p)
	if strings.Contains(string(b), `"levels"`) || !p.Computing {
		t.Fatalf("computing payload must omit levels: %s", b)
	}
}

// The boot path must actually call the display seed (wiring pin): without the
// call the closed-market chart is blank again. Source check on the production
// splice, since mentorSeedAtStart needs a live store.
func TestMentorSeedAtStartCallsTheChartDisplaySeed(t *testing.T) {
	src, err := os.ReadFile("mentor_tick.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	i := strings.Index(s, "func (at *AutoTrader) mentorSeedAtStart()")
	j := strings.Index(s[i:], "\n}\n")
	if i < 0 || j < 0 || !strings.Contains(s[i:i+j], "at.mentorSeedChartDisplay(") {
		t.Fatal("mentorSeedAtStart no longer calls mentorSeedChartDisplay")
	}
}
