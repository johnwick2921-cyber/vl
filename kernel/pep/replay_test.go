package pep

import "testing"

func TestReplay_A_TPandSL(t *testing.T) {
	brk := map[string]ohlc{"08:37": {25030, 25041, 25029, 25040}}
	with := func(k string, b ohlc) map[string]ohlc {
		m := map[string]ohlc{}
		for a, v := range brk {
			m[a] = v
		}
		m[k] = b
		return m
	}
	// TP: +40 pts
	r := ReplayDay(aDay(t, dayCST, with("09:10", ohlc{25060, 25080, 25059, 25070})), dayCST, cfgA(), 1500)
	if !r.Traded || r.ExitKind != ExitTP || r.GrossPts != 40 || r.Side != SideLong {
		t.Fatalf("TP day: %+v", r)
	}
	near(t, "gross usd 6 MNQ", r.GrossUSD(6, 2), 480)
	near(t, "net usd 6 MNQ ($1 RT each)", r.NetUSD(6, 2, 1), 474)
	// SL: -40 pts
	r = ReplayDay(aDay(t, dayCST, with("09:00", ohlc{25010, 25011, 25000, 25005})), dayCST, cfgA(), 1500)
	if r.ExitKind != ExitSL || r.GrossPts != -40 {
		t.Fatalf("SL day: %+v", r)
	}
	// ONE bar reaches both: the STOP counts first
	r = ReplayDay(aDay(t, dayCST, with("09:00", ohlc{25030, 25085, 24995, 25030})), dayCST, cfgA(), 1500)
	if r.ExitKind != ExitSL || r.GrossPts != -40 {
		t.Fatalf("same-bar SL-before-TP: %+v", r)
	}
}

func TestReplay_A_ShortSide(t *testing.T) {
	over := map[string]ohlc{"08:41": {25005, 25010, 24998.75, 25001}, "10:00": {24970, 24971, 24960, 24962}}
	r := ReplayDay(aDay(t, dayCST, over), dayCST, cfgA(), 1500)
	if r.Side != SideShort || r.ExitKind != ExitTP || r.GrossPts != 40 {
		t.Fatalf("%+v", r)
	}
	over["10:00"] = ohlc{25030, 25040, 25029, 25035}
	r = ReplayDay(aDay(t, dayCST, over), dayCST, cfgA(), 1500)
	if r.ExitKind != ExitSL || r.GrossPts != -40 {
		t.Fatalf("%+v", r)
	}
}

// 15:00 CT: closed at the LAST price (the close of the last bar before 15:00).
func TestReplay_FlatAtLastPrice(t *testing.T) {
	over := map[string]ohlc{"08:37": {25030, 25041, 25029, 25040}, "14:59": {25049, 25052, 25048, 25050}}
	r := ReplayDay(aDay(t, dayCST, over), dayCST, cfgA(), 1500)
	if !r.Traded || r.ExitKind != ExitFlat || r.Exit != 25050 || r.GrossPts != 10 {
		t.Fatalf("flat day: %+v", r)
	}
	// a bar AFTER 15:00 that would hit the target is never read
	over["15:02"] = ohlc{25050, 25200, 25050, 25200}
	r = ReplayDay(aDay(t, dayCST, over), dayCST, cfgA(), 1500)
	if r.ExitKind != ExitFlat || r.Exit != 25050 {
		t.Fatalf("post-flat bar leaked: %+v", r)
	}
}

// The tape ends before 15:00 (half day): closed at its last price, labelled.
func TestReplay_DataEndCutoff(t *testing.T) {
	d := "2025-11-28"
	bars := session(t, d, "07:00", "12:00", 25020, map[string]ohlc{
		"08:30": {25010, 25040, 25000, 25020}, "08:31": {25020, 25035, 25005, 25030},
		"08:32": {25030, 25036, 25010, 25015}, "08:33": {25015, 25030, 25008, 25025},
		"08:34": {25025, 25034, 25012, 25020}, "10:00": {25030, 25041, 25029, 25040},
		"11:59": {25044, 25046, 25043, 25045},
	})
	r := ReplayDay(bars, d, cfgA(), 1500)
	if r.ExitKind != ExitDataEnd || r.Exit != 25045 || r.GrossPts != 5 {
		t.Fatalf("%+v", r)
	}
}

func TestReplay_NoTradeDaysNameTheReason(t *testing.T) {
	r := ReplayDay(aDay(t, dayCST, nil, "08:32"), dayCST, cfgA(), 1500)
	if r.Traded || r.Refusal != CodeBoxMissingMin {
		t.Fatalf("%+v", r)
	}
	r = ReplayDay(aDay(t, dayCST, map[string]ohlc{"08:37": {25010, 25045, 24995, 25020}}), dayCST, cfgA(), 1500)
	if r.Traded || r.Refusal != CodeBothEdges || !r.BothEdges {
		t.Fatalf("%+v", r)
	}
	r = ReplayDay(aDay(t, dayCST, nil), dayCST, cfgA(), 1500)
	if r.Traded || r.Refusal != CodeNoBreakYet {
		t.Fatalf("%+v", r)
	}
}

// E enters at the candle CLOSE and the exit scan starts on the NEXT bar.
func TestReplay_E_EntryAtCandleClose(t *testing.T) {
	// entry 25100, SL 25085, TP 25130
	r := ReplayDay(eDay(t, dayCST, map[string]ohlc{"09:00": {25100, 25131, 25099, 25120}}), dayCST, cfgE(), 1500)
	if !r.Traded || r.Side != SideLong || r.Entry != 25100 || r.ExitKind != ExitTP || r.GrossPts != 30 {
		t.Fatalf("%+v", r)
	}
	if r.EntryAt != ctms(t, dayCST, "08:36") {
		t.Fatalf("entry at %d", r.EntryAt)
	}
	// stop first when one bar spans both
	r = ReplayDay(eDay(t, dayCST, map[string]ohlc{"09:00": {25100, 25131, 25084, 25120}}), dayCST, cfgE(), 1500)
	if r.ExitKind != ExitSL || r.GrossPts != -15 {
		t.Fatalf("%+v", r)
	}
	// the signal candle's own bars never count as post-entry: a spike INSIDE the
	// 08:34-08:36 candle that reaches the target is not a fill
	r = ReplayDay(eDay(t, dayCST, map[string]ohlc{"08:35": {25100, 25140, 25100, 25100}}), dayCST, cfgE(), 1500)
	if r.ExitKind == ExitTP && r.ExitAt <= ctms(t, dayCST, "08:36") {
		t.Fatalf("pre-entry bar leaked into the exit scan: %+v", r)
	}
}

// The replay hands Evaluate only the last `window` bars (live reads 1500): a
// window too small to warm the EMA9 fails closed.
func TestReplay_WindowIsHonoured(t *testing.T) {
	bars := eDay(t, dayCST, nil)
	if r := ReplayDay(bars, dayCST, cfgE(), 1500); !r.Traded {
		t.Fatalf("1500 window: %+v", r)
	}
	if r := ReplayDay(bars, dayCST, cfgE(), 40); r.Traded || r.Refusal == "" {
		t.Fatalf("a 40-bar window cannot warm the EMA9 or hold the 08:30 box all day: %+v", r)
	}
}

// The PDF engine's convention (2026-03-25 E: the stop level was TOUCHED to the
// tick and the PDF still books the target): with StopThrough a touch does not
// fill the stop; the default fills on a touch (a resting stop triggers there).
func TestReplay_StopThroughConvention(t *testing.T) {
	over := map[string]ohlc{
		"08:37": {25030, 25041, 25029, 25040},
		"09:00": {25020, 25025, 25000, 25010}, // low == the 25000 stop exactly
		"09:30": {25060, 25080, 25059, 25070}, // then the 25080 target
	}
	r := ReplayDay(aDay(t, dayCST, over), dayCST, cfgA(), 1500)
	if r.ExitKind != ExitSL || r.GrossPts != -40 {
		t.Fatalf("default (touch fills): %+v", r)
	}
	r = ReplayDayOpts(aDay(t, dayCST, over), dayCST, cfgA(), 1500, ReplayOpts{StopThrough: true})
	if r.ExitKind != ExitTP || r.GrossPts != 40 {
		t.Fatalf("stop-through (touch ignored): %+v", r)
	}
	// ... but a bar that trades beyond the stop by a tick does stop out
	over["09:00"] = ohlc{25020, 25025, 24999.75, 25010}
	r = ReplayDayOpts(aDay(t, dayCST, over), dayCST, cfgA(), 1500, ReplayOpts{StopThrough: true})
	if r.ExitKind != ExitSL {
		t.Fatalf("a print through the stop must stop out: %+v", r)
	}
}
