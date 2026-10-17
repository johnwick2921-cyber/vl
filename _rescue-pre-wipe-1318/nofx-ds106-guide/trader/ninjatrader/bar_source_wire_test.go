package ninjatrader

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// BAR-SOURCE WAVE, WIRE PIN. The persister has received `historical bool` since
// 2026-08-28 and used it only to convert close-stamps. The flag now names the
// feed on every row it writes. A replay frame is 'historical', a live frame is
// 'live', and a bar the ring labelled mixed stays mixed under either flag.
func TestPersisterStampsTheFeedItWasHandedTheBarsFrom(t *testing.T) {
	bars := []ntwire.Bar{
		{T: 1_000, O: 1, H: 2, L: 0.5, C: 1.5, V: 3},
		{T: 2_000, O: 1, H: 2, L: 0.5, C: 1.5, V: 3, Source: ntwire.BarSourceMixed},
	}
	live := barRowsForPersist("MNQ", "1m", "MNQ 12-26", false, bars)
	if len(live) != 2 || live[0].Source != store.BarSourceLive {
		t.Fatalf("a live frame must write source=live, got %+v", live)
	}
	if live[1].Source != store.BarSourceMixed {
		t.Fatalf("a bar the ring labelled mixed must stay mixed on a live frame, got %q", live[1].Source)
	}
	replay := barRowsForPersist("MNQ", "1m", "MNQ 12-26", true, bars)
	if replay[0].Source != store.BarSourceHistorical {
		t.Fatalf("a replay frame must write source=historical, got %q", replay[0].Source)
	}
	if replay[1].Source != store.BarSourceMixed {
		t.Fatalf("a bar the ring labelled mixed must stay mixed on a replay frame, got %q", replay[1].Source)
	}
	for _, r := range append(live, replay...) {
		if r.Contract != "MNQ 12-26" {
			t.Fatalf("every row carries the contract it was received under, got %q", r.Contract)
		}
	}
}

// THE MAPPING IS CALLED FROM THE PERSISTER, not merely defined. Text-level pin
// (class 113 caveat: this certifies the name appears inside the SetBarPersister
// closure, not that the worker invoked it — the worker's own path is exercised
// by the ntwire persist tests).
func TestPersisterClosureCallsTheMapping(t *testing.T) {
	src, err := os.ReadFile("bar_persist_wire.go")
	if err != nil {
		t.Fatal(err)
	}
	closure := regexp.MustCompile(`(?s)ntwire\.SetBarPersister\(func\(historical bool.*?\n\t\t\}\)\n`).Find(src)
	if closure == nil {
		t.Fatal("could not locate the SetBarPersister closure")
	}
	if !regexp.MustCompile(`barRowsForPersist\(symbol, tf, contract, historical, closed\)`).Match(closure) {
		t.Fatal("the persister closure no longer calls barRowsForPersist with the frame's historical flag — the source stamp is unwired")
	}
}

// THE 📼 LINE IS READ, NEVER LITERAL (A24/45): every number on it comes from
// the census, the ring's detections and the hold, and n/a is printed where
// nothing is known.
func TestSourceBootLineIsReadNotLiteral(t *testing.T) {
	census := map[string]int64{store.BarSourceLive: 51087, store.BarSourceHistorical: 5, store.BarSourceMixed: 3, "": 0}
	at := time.Date(2026, 9, 10, 22, 40, 12, 0, time.FixedZone("CDT", -5*3600))
	mm := []ntwire.ScaleMismatch{
		{Symbol: "MNQ", Timeframe: "1m", At: at, LastHistoricalC: 29068.25, FirstLiveC: 29358.25, DeltaPts: 290, HistoricalDropped: 2000},
		{Symbol: "ES", Timeframe: "1m", At: at, LastHistoricalC: 1, FirstLiveC: 2, DeltaPts: 1, HistoricalDropped: 1},
	}
	line := SourceBootLine("MNQ", census, mm, 0.005, "replay-hold: held=0 released=0 discarded=2000")
	for _, want := range []string{
		"MNQ live=51087 historical=5 mixed=3 off-scale=0 null=0",
		"unverified-replay-held=on",
		"replay-hold: held=0 released=0 discarded=2000",
		"threshold=0.50%[env:default] AND 20x[env:default] median body [I]", // R5 removes the source words with the envcompat package
		"1m@22:40:12 CT Δ=290.00 (replay 29068.25 vs live 29358.25, 2000 dropped)",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("boot line lacks %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "ES") {
		t.Fatalf("another symbol's mismatch leaked onto MNQ's line:\n%s", line)
	}
	bare := SourceBootLine("MNQ", map[string]int64{}, nil, 0.005, "")
	if !strings.Contains(bare, "mismatches this process: none") || !strings.Contains(bare, "replay-hold: n/a") {
		t.Fatalf("a cold line must say none / n/a, not invent:\n%s", bare)
	}
}
