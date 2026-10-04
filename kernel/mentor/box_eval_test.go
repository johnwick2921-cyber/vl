package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// Call-site tests for the box trade wire: boxEntryIntent (reject/cancel/
// filters/confluence) and the Tick loop (R1: every return trades, each
// exactly once).

func TestBoxEntryIntentRejectPlacesStopOrder(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0.5
	b := Box{Kind: FTGH, Top: 105, Bottom: 102}
	ref := market.Kline{High: 104.5, Low: 100.5, Close: 101} // touch + close below = reject
	levels := []Level{{Kind: KindKeyLevel, Price: 98}}
	out := boxEntryIntent(ref, b, []Box{b}, levels, TriggerLine{}, nil, cfg)
	if len(out) != 1 {
		t.Fatalf("reject return = %d intents, want 1", len(out))
	}
	in := out[0]
	if in.Action != PlaceStopEntry || in.Side != SideShort || in.Price != 100.5 || in.Stop != 104.5 || in.Target != 98 {
		t.Fatalf("reject intent = %+v, want SHORT entry 100.5 stop 104.5 target 98", in)
	}
	if in.Confluence {
		t.Fatal("no key level at the box and no trigger — confluence must be off")
	}
	if in.Reason == "" {
		t.Fatal("intent without a reason")
	}
}

func TestBoxEntryIntentInsideCloseCancels(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	b := Box{Kind: FTGH, Top: 105, Bottom: 102}
	ref := market.Kline{High: 105.1, Low: 103, Close: 104} // touch, close inside = cancel [D3.2 p1 @ 21:04–21:33]
	if out := boxEntryIntent(ref, b, []Box{b}, nil, TriggerLine{}, nil, cfg); len(out) != 0 {
		t.Fatalf("inside close = %d intents, want 0 (cancel)", len(out))
	}
}

func TestBoxEntryIntentTriggerBlock(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	b := Box{Kind: FTGH, Top: 105, Bottom: 102}
	ref := market.Kline{High: 104.5, Low: 100.5, Close: 101}
	levels := []Level{{Kind: KindKeyLevel, Price: 98}}
	trig := TriggerLine{Dir: SideLong, Price: 106} // short entry below the buy line → blocked
	if out := boxEntryIntent(ref, b, []Box{b}, levels, trig, nil, cfg); len(out) != 0 {
		t.Fatalf("wrong-side trigger = %d intents, want 0", len(out))
	}
}

// B3 [D3.4 p3 @ 07:38–08:22]: confluence = FTGL/FTGH entry + the 5m
// trigger agrees, NO key-level condition → the flag rides the intent for
// DS-102's exit-C / size-10.
func TestBoxEntryIntentConfluenceFlag(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0.3
	b := Box{Kind: FTGL, Top: 96, Bottom: 94}
	ref := market.Kline{High: 97.5, Low: 96.5, Close: 97} // reject for the long
	levels := []Level{
		{Kind: KindKeyLevel, Price: 97.9}, // inside the box + 2-pt band → confluence
		{Kind: KindKeyLevel, Price: 99.5}, // target ladder
	}
	trig := TriggerLine{Dir: SideLong, Price: 95}
	out := boxEntryIntent(ref, b, []Box{b}, levels, trig, nil, cfg)
	if len(out) != 1 {
		t.Fatalf("reject return = %d intents, want 1", len(out))
	}
	if !out[0].Confluence || out[0].Side != SideLong {
		t.Fatalf("intent = %+v, want LONG with Confluence=true", out[0])
	}

	// The same setup without the trigger line: confluence stays off.
	if o := boxEntryIntent(ref, b, []Box{b}, levels, TriggerLine{}, nil, cfg); len(o) != 1 || o[0].Confluence {
		t.Fatalf("no trigger line can never agree — got %+v", o)
	}

	// B3: the key levels are irrelevant — even with only the far target level
	// in the set, the FTGL + buy trigger still confluences.
	far := []Level{{Kind: KindKeyLevel, Price: 99.5}}
	if o := boxEntryIntent(ref, b, []Box{b}, far, trig, nil, cfg); len(o) != 1 || !o[0].Confluence {
		t.Fatalf("FTGL + buy trigger must confluence without a key level in the box — got %+v", o)
	}
}

// R1 at the CALL SITE [D3.2 p2 @ 06:25]: Tick evaluates EVERY return visit
// of a live box — two reject returns in the day emit two box intents — and
// each return exactly once (BoxRefs dedup).
func TestEvaluatorBoxPathEveryReturnTrades(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0 // no EMA34 location line: the seeded level is the only target
	cfg.RoomMultiple = 0.05
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 102, 99, 101),
		mk(2, 101, 103, 95, 96),   // bottom 1: the extreme low 95
		mk(3, 96.5, 97, 96, 96.8), // confirms the extreme
		mk(4, 96.8, 97, 96.2, 96.8),
		mk(5, 96.8, 97, 95.5, 96),      // bottom 2: later confirmed higher low
		mk(6, 96.2, 97, 95.8, 96.8),    // confirms bottom 2
		mk(7, 96.8, 97, 96.2, 96.8),    // no touch
		mk(8, 96.8, 97, 96.3, 96.9),    // no touch
		mk(9, 96.9, 97, 96.4, 96.9),    // no touch (5m bucket 2 ends; high 97 keeps returns out of the ISB box)
		mk(10, 96.5, 97.1, 95.9, 97),   // REJECT return 1
		mk(11, 97.5, 97.9, 97.1, 97.6), // outside
		mk(12, 96.5, 97.2, 95.9, 97),   // REJECT return 2
	}
	e := New(cfg)
	now := bars[12].OpenTime + 59_999
	// Seed one far key level above the entry so the box has a target (the
	// tape's own key level sits below the entry).
	e.seeded = true
	e.State.Seed1mWatermark = maxInt64
	e.State.Seed1HWatermark = maxInt64
	e.State.SeedLevels = []Level{{Key: "far", Kind: KindKeyLevel, Price: 125}}
	// ORB preset (the §7 gate is drawn+escaped long; the tape alone never
	// draws an ORB and the gate would refuse every intraday entry).
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	// B9 presets: box entries obey the HTF/day gates (D5.1 p1 @16:24,
	// @19:11–20:07) — 4h long (1h silent), a normal measured day.
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}
	// Freeze the 5m trigger BELOW the entry: the B1 trigger zone must not
	// refuse an FTGL entry above its buy line, and the retest level below
	// the entry must not shadow the seeded target.
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 80, LastBucket: t0 + 5*60_000}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	boxIntents := func(ins []Intent) []Intent {
		var out []Intent
		for _, in := range ins {
			if strings.HasPrefix(in.Reason, "box edge return") {
				out = append(out, in)
			}
		}
		return out
	}
	first := boxIntents(e.Tick(bars[:13], now))
	if len(first) != 2 {
		t.Fatalf("tick 1 box intents = %d (%+v), want 2 — every return trades [D3.2 p2 @ 06:25]", len(first), first)
	}
	if first[0].Price != 97.1 || first[0].Side != SideLong || first[0].Stop != 95.9 {
		t.Fatalf("return 1 = %+v, want LONG 97.1 / stop 95.9", first[0])
	}
	if first[1].Price != 97.2 {
		t.Fatalf("return 2 = %+v, want entry 97.2", first[1])
	}
	// Next tick: no new candle — the same returns must not re-emit.
	bars2 := append(bars, mk(13, 98.5, 98.8, 97.5, 98.6))
	second := boxIntents(e.Tick(bars2[:14], bars2[13].OpenTime+59_999))
	if len(second) != 0 {
		t.Fatalf("tick 2 box intents = %d (%+v), want 0 — a return is evaluated once", len(second), second)
	}
}

// TestBoxReturnExactlyOneEntry — BOX PATH DECISION (CTO 12:38:50Z): one box
// return visit yields EXACTLY ONE entry intent — the box path's. The box
// edges are OUT of the level touch loop, so the level path must not add a
// second entry for the same return. Mutant (edges back in the loop) → 2 != 1.
func TestBoxReturnExactlyOneEntry(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.RoomMultiple = 0.05
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100), mk(1, 100, 102, 99, 101), mk(2, 101, 103, 95, 96),
		mk(3, 96.5, 97, 96, 96.8), // confirms the extreme (low 96 > 95)
		mk(4, 96.8, 97, 96.2, 96.8),
		mk(5, 96.8, 97, 95.5, 96),   // bottom 2: the later confirmed higher low
		mk(6, 96.2, 97, 95.8, 96.8), // confirms bottom 2
		mk(7, 96.5, 97.1, 95.9, 97), // the ONE reject return (touches 96, closes above)
	}
	e := New(cfg)
	now := bars[7].OpenTime + 59_999
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	// Presets so the LEVEL path could emit a PHL from the edge too (under the
	// mutant): trigger long, 4h long (1h silent), day measured OK.
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 93}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}

	ins := e.Tick(bars[:8], now)
	entries := 0
	for _, in := range ins {
		if in.Action == PlaceStopEntry {
			entries++
		}
	}
	if entries != 1 {
		t.Fatalf("one box return yielded %d entries, want EXACTLY ONE: %+v", entries, ins)
	}
	// The box edges must NEVER be classified by the level touch loop — if
	// they were, the same return would trade twice (box path + level path).
	for _, lvl := range BoxEdgeLocations(BoxesBuild(bars, cfg.Box, time.UnixMilli(now))) {
		if tr, ok := e.State.Touches[lvl.Key]; ok && tr.Outcome != TouchNone {
			t.Fatalf("box edge %s was touched as a LEVEL (outcome %q) — the edge must be out of the level touch loop", lvl.Key, tr.Outcome)
		}
	}

	if ins[0].Reason != "" && !strings.HasPrefix(ins[0].Reason, "box edge return") && entries > 0 {
		// the single entry must be the box path's
		var boxOne bool
		for _, in := range ins {
			if in.Action == PlaceStopEntry && strings.HasPrefix(in.Reason, "box edge return") {
				boxOne = true
			}
		}
		if !boxOne {
			t.Fatalf("the single entry is not the box path's: %+v", ins)
		}
	}
}

// TestBoxThirdTouchIsTheTrade — item 12 (CTO ruling 23:06Z): "the FAILURE
// defines the box… the trade is the 3rd touch." Tick-level pin through the
// production path: the bottom-2 candle itself (the touch that completes the
// pair) is a formation candle — the box is born only at bottom 2's
// confirmation, so the bottom-2 touch emits NOTHING; the FIRST return after
// formation (the 3rd touch overall) is the trade. Mutant (FormedAt =
// seq[nearest].idx, walk the confirming bar) → RED: an entry fires on the
// confirming bar before the return.
func TestBoxThirdTouchIsTheTrade(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.RoomMultiple = 0.05
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 102, 99, 101),
		mk(2, 101, 103, 95, 96),   // bottom 1: the extreme low 95
		mk(3, 96.5, 97, 96, 96.8), // confirms the extreme
		mk(4, 96.8, 97, 96.2, 96.8),
		mk(5, 96.8, 97, 95.5, 96),   // bottom 2: touches the future top edge (close 96)
		mk(6, 96.2, 97, 95.8, 96.8), // bottom 2's confirming bar — box born here
		mk(7, 96.5, 97.1, 95.9, 97), // the first return = the 3rd touch
	}
	e := New(cfg)
	preset := func(now int64) {
		e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	}
	boxEntries := func(ins []Intent) []Intent {
		var out []Intent
		for _, in := range ins {
			if strings.HasPrefix(in.Reason, "box edge return") && in.Action == PlaceStopEntry {
				out = append(out, in)
			}
		}
		return out
	}

	// Tick through the bottom-2 candle (bar 5): the pair is not yet formed —
	// no box, no entry. The bottom-2 touch is never the trade.
	now := bars[5].OpenTime + 59_999
	preset(now)
	if got := boxEntries(e.Tick(bars[:6], now)); len(got) != 0 {
		t.Fatalf("bottom-2 tick emitted %d box entries, want 0 — the bottom-2 touch is a formation candle", len(got))
	}

	// Tick through the return (bar 7): the first return after formation is
	// the trade — exactly one box entry.
	now = bars[7].OpenTime + 59_999
	preset(now)
	if got := boxEntries(e.Tick(bars[:8], now)); len(got) != 1 {
		t.Fatalf("return tick emitted %d box entries, want 1 — the 3rd touch is the trade", len(got))
	}
}

// TestPingPongVerdict — PING PONG (CTO 13:24:53Z): between an FTGL floor and an
// FTGH ceiling the edge trade needs a >=50-pt gap; below that it is refused.
func TestPingPongVerdict(t *testing.T) {
	floor := Box{Kind: FTGL, Top: 100, Bottom: 90, Key: "ftgl"}
	ceil := Box{Kind: FTGH, Top: 210, Bottom: 150, Key: "ftgh"}

	if ok, _ := pingPongVerdict([]Box{floor}, nil, 110, Config{}); !ok {
		t.Fatal("a single box around the price is not a ping-pong context")
	}
	if ok, _ := pingPongVerdict([]Box{floor, ceil}, nil, 110, Config{}); !ok {
		t.Fatalf("gap 50 (150-100) must be allowed, got refused")
	}
	narrow := ceil
	narrow.Bottom = 140 // gap 40
	if ok, reason := pingPongVerdict([]Box{floor, narrow}, nil, 110, Config{}); ok {
		t.Fatal("gap 40 must be refused")
	} else if reason != "ping_pong_range_too_small" {
		t.Fatalf("reason = %q, want ping_pong_range_too_small", reason)
	}
}

// TestBoxEntryPingPongRange — the call site: a box return between two boxes
// fires only when the ping-pong range is >= 50 pts.
func TestBoxEntryPingPongRange(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0.05
	floor := Box{Kind: FTGL, Top: 100, Bottom: 90, Key: "ftgl:100.00:90.00"}
	ceil := Box{Kind: FTGH, Top: 210, Bottom: 150, Key: "ftgh:210.00:150.00"}
	// the return: a reject at the FTGL top edge from above
	ref := market.Kline{High: 101, Low: 99, Close: 101}
	levels := []Level{{Key: "k", Kind: KindKeyLevel, Price: 160}}
	trig := TriggerLine{Dir: SideLong, Price: 95}

	out := boxEntryIntent(ref, floor, []Box{floor, ceil}, levels, trig, nil, cfg)
	if len(out) != 1 {
		t.Fatalf("gap 50 must fire one entry, got %d", len(out))
	}

	narrow := ceil
	narrow.Bottom = 140 // gap 40
	out = boxEntryIntent(ref, floor, []Box{floor, narrow}, levels, trig, nil, cfg)
	if len(out) != 0 {
		t.Fatalf("gap 40 must refuse, got %+v", out)
	}
}

// TestLocTriggerFilterBox — v5_loc_notrig mirror: false switches the 5m
// trigger filter off for BOX rejects.
func TestLocTriggerFilterBox(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0.05
	floor := Box{Kind: FTGL, Top: 100, Bottom: 90, Key: "ftgl:100.00:90.00"}
	ref := market.Kline{High: 101, Low: 99, Close: 101}
	levels := []Level{{Key: "k", Kind: KindKeyLevel, Price: 160}}
	// the long entry sits BELOW a sell trigger: the filter refuses it.
	sellTrig := TriggerLine{Dir: SideShort, Price: 105}
	cfg.TriggerSchool = 2 // school 2: the box path waits for the trigger (B20)

	if out := boxEntryIntent(ref, floor, []Box{floor}, levels, sellTrig, nil, cfg); len(out) != 0 {
		t.Fatalf("trigger filter ON must refuse the against-trigger box entry, got %+v", out)
	}
	cfg.LocTriggerFilter = false
	if out := boxEntryIntent(ref, floor, []Box{floor}, levels, sellTrig, nil, cfg); len(out) != 1 {
		t.Fatalf("LocTriggerFilter=false must allow the box entry, got %+v", out)
	}
}

// TestWrongTriggerSideBox — B2 (10-03 ruling): every entry, the box path
// included, must be on the trigger side. The old two-line band is gone (B1).
func TestWrongTriggerSideBox(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0.05
	cfg.TriggerSchool = 2 // B20 school 2: the box path waits for the 5m trigger
	floor := Box{Kind: FTGL, Top: 100, Bottom: 90, Key: "ftgl:100.00:90.00"}
	ref := market.Kline{High: 101, Low: 99, Close: 101}
	levels := []Level{{Key: "k", Kind: KindKeyLevel, Price: 160}}
	// a sell trigger at 120: the entry price (~99) sits on the WRONG side.
	trig := TriggerLine{Dir: SideShort, Price: 120}

	out := boxEntryIntent(ref, floor, []Box{floor}, levels, trig, nil, cfg)
	if len(out) != 0 {
		t.Fatalf("entry on the wrong trigger side must refuse, got %+v", out)
	}
	// the same box with a BUY trigger above the entry is allowed by the trigger
	// gate (target ladder may still refuse it — that is the target, not the side).
	buy := TriggerLine{Dir: SideLong, Price: 95}
	if ok, _, _ := TriggerVerdict(buy, 99); !ok {
		t.Fatal("fixture: above the buy line the trigger side is correct")
	}
}

// TestBoxPathRecordedTape13Sep — the recorded golden frame (Sun 13 Sep 2026,
// mnq_1m_2026-09-13_boxframe) replayed through Evaluator.Tick: the dedicated
// box path must FIRE on the real tape (the 1,777-killed-by-midRangeBoxed
// census is gone). The ORB gate is OFF: the tape is Sunday Globex and has no
// RTH ORB to draw. If zero entries fire, the test reports the per-gate
// census of where the first box return died.
func TestBoxPathRecordedTape13Sep(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.OrbGateEnabled = false // Globex tape: no RTH ORB exists
	bars := loadFixture(t, "mnq_1m_2026-09-13_boxframe", "1m")

	e := New(cfg)
	boxEntries := 0
	for i := 2; i <= len(bars); i++ {
		now := bars[i-1].CloseTime + 1
		ins := e.Tick(bars[:i], now)
		for _, in := range ins {
			if in.Action == PlaceStopEntry && strings.HasPrefix(in.Reason, "box edge return") {
				boxEntries++
			}
		}
	}
	// Death census over the FULL tape when nothing fired.
	census := map[string]int{}
	returns := 0
	now := bars[len(bars)-1].CloseTime + 1
	boxes := BoxesBuild(bars, cfg.Box, time.UnixMilli(now))
	levels := Levels(bars, cfg, now)
	for _, b := range boxes {
		for _, r := range BoxReturnBars(bars, b, b.FormedAt, cfg.Box) {
			returns++
			ref := bars[r.RefBar]
			census[censusBoxReturn(ref, b, boxes, levels, e.State.Trigger, bars, cfg)]++
		}
	}
	// Census verdict: every death must be one of the ruling-sanctioned
	// refusals — fact 4 (a close inside the box cancels, not a trade), the
	// 5m trigger filter (F1/L3: the trigger stays ON in the base) or the room
	// rule (a return whose next-level target lacks room — B1 removed the
	// two-line band, so those deaths now reach the room check). The CTO's
	// fallback for a non-firing tape is exactly this census.
	if returns == 0 {
		t.Fatal("recorded tape: no box returns at all — the box path is dead")
	}
	for gate, n := range census {
		switch gate {
		case "reject (close inside the box)", "trigger verdict", "room",
			"ping_pong_range_too_small", "ping_pong_candle_too_big":
		default:
			t.Fatalf("recorded tape: %d returns died at an UNEXPECTED gate %q (full census %v)",
				n, gate, census)
		}
	}
	t.Logf("recorded tape 13 Sep: %d box-return entries fired; %d returns, deaths: %v",
		boxEntries, returns, census)
}

// censusBoxReturn names the first gate that kills a box return (for the death
// census when a recorded tape fires nothing).
func censusBoxReturn(ref market.Kline, b Box, boxes []Box, levels []Level, trig TriggerLine, bars []market.Kline, cfg Config) string {
	if !BoxReturnReject(b, ref) {
		return "reject (close inside the box)"
	}
	price := ref.High
	side := SideLong
	if b.Kind != FTGL {
		price, side = ref.Low, SideShort
	}
	if cfg.LocTriggerFilter && cfg.TriggerSchool != 1 {
		if ok, ts, _ := TriggerVerdict(trig, price); !ok || ts != "" && ts != side {
			return "trigger verdict"
		}
	}
	if ok, reason := pingPongVerdict(boxes, bars, price, cfg); !ok {
		return reason
	}
	if InsideAnyBox(boxes, price) {
		return "inside a box"
	}
	if abs(price-chooseStop(ref, side)) > cfg.StopCeilingPts {
		return "stop ceiling"
	}
	if nextLevelBeyond(levels, price, side) == 0 {
		return "no level beyond"
	}
	return "room"
}

func chooseStop(ref market.Kline, side Side) float64 {
	if side == SideLong {
		return ref.Low
	}
	return ref.High
}

// TestBoxPathRecordedTapeWeekdayRTH — the same census test on a WEEKDAY RTH
// tape (Tue 15 Sep 2026, mnq_1m_2026-09-15_rth) with the ORB gate ON: either
// at least one box entry fires, or every return's death is a
// ruling-sanctioned gate, named.
func TestBoxPathRecordedTapeWeekdayRTH(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true // ORB gate stays ON: the RTH tape can draw a real ORB
	bars := loadFixture(t, "mnq_1m_2026-09-15_rth", "1m")

	e := New(cfg)
	boxEntries := 0
	for i := 2; i <= len(bars); i++ {
		now := bars[i-1].CloseTime + 1
		for _, in := range e.Tick(bars[:i], now) {
			if in.Action == PlaceStopEntry && strings.HasPrefix(in.Reason, "box edge return") {
				boxEntries++
			}
		}
	}

	now := bars[len(bars)-1].CloseTime + 1
	boxes := BoxesBuild(bars, cfg.Box, time.UnixMilli(now))
	levels := Levels(bars, cfg, now)
	census := map[string]int{}
	returns := 0
	for _, b := range boxes {
		for _, r := range BoxReturnBars(bars, b, b.FormedAt, cfg.Box) {
			returns++
			ref := bars[r.RefBar]
			gate := censusBoxReturn(ref, b, boxes, levels, e.State.Trigger, bars, cfg)
			if gate == "trigger verdict" && cfg.LocTriggerFilter {
				gate = "trigger verdict"
			}
			census[gate]++
		}
	}
	if returns == 0 {
		t.Skipf("weekday RTH tape 15 Sep: no box returns on this tape (0 boxes/returns) — nothing to census")
	}
	if boxEntries > 0 {
		t.Logf("weekday RTH tape 15 Sep: %d box-return entries FIRED; %d returns, deaths: %v", boxEntries, returns, census)
		return
	}
	for gate, n := range census {
		switch gate {
		case "reject (close inside the box)", "trigger verdict", "room",
			"no level beyond", "stop ceiling", "ping_pong_range_too_small", "inside a box":
		default:
			t.Fatalf("weekday RTH tape: %d returns died at an UNEXPECTED gate %q (census %v)",
				n, gate, census)
		}
	}
	t.Logf("weekday RTH tape 15 Sep: 0 box-return entries fired; %d returns, deaths: %v", returns, census)
}

// TestBoxEntryDayAndHTFGates — B9 [D5.1 p1 @16:24, @19:11–20:07]: box trades
// obey the 4h/1h direction, the day-off and the spent cap like every setup.
func TestBoxEntryDayAndHTFGates(t *testing.T) {
	fixture := func(htf HTF, day DayLatch) (*Evaluator, []market.Kline, int64) {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.KeyLevelTFMinutes = 1
		cfg.EMAPeriod34 = 0
		cfg.EMAPeriod9 = 0
		cfg.RoomMultiple = 0.05
		t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
		mk := func(i int, o, h, l, c float64) market.Kline {
			return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
		}
		bars := []market.Kline{
			mk(0, 100, 101, 99, 100),
			mk(1, 100, 102, 99, 101),
			mk(2, 101, 103, 95, 96),
			mk(3, 96.5, 97, 96, 96.8), // confirms the extreme (low 96 > 95)
			mk(4, 96.8, 97, 96.2, 96.8),
			mk(5, 96.8, 97, 95.5, 96),   // bottom 2: later confirmed higher low
			mk(6, 96.2, 97, 95.8, 96.8), // confirms bottom 2
			mk(7, 96.5, 97.1, 95.9, 97), // FTGL reject return → LONG
		}
		e := New(cfg)
		now := bars[7].OpenTime + 59_999
		e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
		// Frozen per-trading-day latch so Tick's recompute keeps the preset
		// (a zero-Key latch re-reads as not-measured on this synthetic tape).
		if day.Key == "" {
			day.Key = tradingDayKey(time.UnixMilli(now).In(ctime()))
		}
		e.State.HTF = htf
		e.State.Day = day
		return e, bars, now
	}
	boxEntries := func(ins []Intent) []Intent {
		var out []Intent
		for _, in := range ins {
			if strings.HasPrefix(in.Reason, "box edge return") {
				out = append(out, in)
			}
		}
		return out
	}

	// (a) no 4h direction → box_htf_blocked, zero box entries.
	e, bars, now := fixture(HTF{}, DayLatch{Verdict: DayTrade})
	if got := boxEntries(e.Tick(bars, now)); len(got) != 0 {
		t.Fatalf("no-4h tick emitted %d box entries, want 0", len(got))
	}
	if e.State.Refusals["box_htf_blocked"] == 0 {
		t.Fatalf("ledger = %v, want box_htf_blocked counted", e.State.Refusals)
	}

	// (b) 4h SHORT vs a LONG box entry → box_htf_side_mismatch.
	e, bars, now = fixture(HTF{FourH: TriggerLine{Dir: SideShort, Price: 99}}, DayLatch{Verdict: DayTrade})
	if got := boxEntries(e.Tick(bars, now)); len(got) != 0 {
		t.Fatalf("opposite-HTF tick emitted %d box entries, want 0", len(got))
	}
	if e.State.Refusals["box_htf_side_mismatch"] == 0 {
		t.Fatalf("ledger = %v, want box_htf_side_mismatch counted", e.State.Refusals)
	}

	// (c) DayOff → box_day_off, zero box entries.
	e, bars, now = fixture(HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}, DayLatch{Verdict: DayOff})
	if got := boxEntries(e.Tick(bars, now)); len(got) != 0 {
		t.Fatalf("day-off tick emitted %d box entries, want 0", len(got))
	}
	if e.State.Refusals["box_day_off"] == 0 {
		t.Fatalf("ledger = %v, want box_day_off counted", e.State.Refusals)
	}

	// (d) DaySpent + HTF long → the entry emits (the cap path does not
	// refuse); the spent cap value itself is pinned on the ISB path.
	e, bars, now = fixture(HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}, DayLatch{Verdict: DaySpent})
	if got := boxEntries(e.Tick(bars, now)); len(got) != 1 {
		t.Fatalf("spent-day tick emitted %d box entries, want 1 (cap, not refusal)", len(got))
	}
}

// TestBoxEntryBannedInsideISBBox — B11 [D3.4 p2 @07:58–08:21]: inside the
// standing 5m-ISB box only a same-direction ISB trades; box trades never.
func TestBoxEntryBannedInsideISBBox(t *testing.T) {
	fixture := func() (*Evaluator, []market.Kline, int64) {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.KeyLevelTFMinutes = 1
		cfg.EMAPeriod34 = 0
		cfg.EMAPeriod9 = 0
		cfg.RoomMultiple = 0.05
		t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
		mk := func(i int, o, h, l, c float64) market.Kline {
			return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
		}
		bars := []market.Kline{
			mk(0, 100, 101, 99, 100),
			mk(1, 100, 102, 99, 101),
			mk(2, 101, 103, 95, 96),
			mk(3, 96.5, 97, 96, 96.8), // confirms the extreme (low 96 > 95)
			mk(4, 96.8, 97, 96.2, 96.8),
			mk(5, 96.8, 97, 95.5, 96),   // bottom 2: later confirmed higher low
			mk(6, 96.2, 97, 95.8, 96.8), // confirms bottom 2
			mk(7, 96.5, 97.1, 95.9, 97), // the return
		}
		e := New(cfg)
		now := bars[7].OpenTime + 59_999
		e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
		return e, bars, now
	}
	boxEntries := func(ins []Intent) []Intent {
		var out []Intent
		for _, in := range ins {
			if strings.HasPrefix(in.Reason, "box edge return") {
				out = append(out, in)
			}
		}
		return out
	}

	// No ISB box: the return trades.
	e, bars, now := fixture()
	if got := boxEntries(e.Tick(bars, now)); len(got) != 1 {
		t.Fatalf("no-ISB-box tick emitted %d box entries, want 1", len(got))
	}

	// A standing 5m-ISB box covering the return (ref close 97 is inside
	// [95, 100]; the last candle's body stays inside too, so the box does
	// not escape on this tick): the box trade is refused, named + counted.
	e, bars, now = fixture()
	e.State.ISBBox = &ISBBox{High: 100, Low: 95}
	if got := boxEntries(e.Tick(bars, now)); len(got) != 0 {
		t.Fatalf("inside-ISB-box tick emitted %d box entries, want 0", len(got))
	}
	if e.State.Refusals["box_isb_ban"] == 0 {
		t.Fatalf("ledger = %v, want box_isb_ban counted", e.State.Refusals)
	}

	// ISB box elsewhere: the entry fires again.
	e, bars, now = fixture()
	e.State.ISBBox = &ISBBox{High: 105, Low: 103}
	if got := boxEntries(e.Tick(bars, now)); len(got) != 1 {
		t.Fatalf("elsewhere-ISB-box tick emitted %d box entries, want 1", len(got))
	}
}
