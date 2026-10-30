package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
)

// ── PEP ON A SHORTENED (EARLY-CLOSE) DAY ────────────────────────────────────
//
// kernel/session_calendar.json lists 2026-11-27 and 2026-12-24 as closing at
// 12:15 CT; CMEClosedReason flips to closed at that minute. Pep used only its
// own 15:00 flat time: a break at 12:10 sent a real entry four minutes before
// the halt, and an open position was never closed at the halt (the flat then
// fired into a closed market, every beat, until the 17:00 reopen) - re-review
// lens 3, P2. Now the day's EFFECTIVE flat time is the earlier of the saved flat
// time and (early close - pepEarlyCloseMarginMin), read from the calendar.
//
// MUTANT (all pins below RED): make pepEffectiveFlatMin return (flatMin, false).

var pepHalfDays = []struct{ date, close, flat string }{
	{"2026-11-27", "12:15", "12:10"},
	{"2026-12-24", "12:15", "12:10"},
}

func TestPepEffectiveFlatMinIsPullInOnly(t *testing.T) {
	box := 8*60 + 30
	if got, pulled := pepEffectiveFlatMin("2026-10-29", 15*60, box); got != 900 || pulled {
		t.Fatalf("an ordinary day moved the flat time: %d %v", got, pulled)
	}
	if got, pulled := pepEffectiveFlatMin("2026-11-27", 15*60, box); got != 12*60+10 || !pulled {
		t.Fatalf("the day after Thanksgiving: %d %v, want 12:10", got, pulled)
	}
	// pull-in only: a saved flat time already before the close is not pushed later
	if got, pulled := pepEffectiveFlatMin("2026-11-27", 11*60, box); got != 11*60 || pulled {
		t.Fatalf("a saved 11:00 flat was moved: %d %v", got, pulled)
	}
	// never at or before the box start (the validator's own floor)
	if got, pulled := pepEffectiveFlatMin("2026-11-27", 15*60, 12*60+10); got != 15*60 || pulled {
		t.Fatalf("a flat at/before the box start was produced: %d %v", got, pulled)
	}
	if got, pulled := pepEffectiveFlatMin("", 15*60, box); got != 900 || pulled {
		t.Fatalf("an empty day moved the flat time: %d %v", got, pulled)
	}
	// the margin is READ against the calendar's own close, not a typed 12:15
	for _, d := range kernel.SessionShortenedDays() {
		c, ok := kernel.SessionEarlyCloseCTForKey(d.Date)
		if !ok {
			continue
		}
		var h, m int
		if _, err := time.Parse("15:04", c); err != nil {
			t.Fatalf("%s: %v", d.Date, err)
		}
		h, m = int(c[0]-'0')*10+int(c[1]-'0'), int(c[3]-'0')*10+int(c[4]-'0')
		want := h*60 + m - pepEarlyCloseMarginMin
		if want <= box || want >= 15*60 {
			continue
		}
		if got, pulled := pepEffectiveFlatMin(d.Date, 15*60, box); got != want || !pulled {
			t.Errorf("%s (close %s): flat %d pulled %v, want %d", d.Date, c, got, pulled, want)
		}
	}
}

// An entry signal at 12:10 on a shortened day is NOT sent (the old build sent
// long MNQ x3 four minutes before the halt); one at 12:00 still is (control:
// the day is not simply shut).
func TestPepNoEntryNearTheEarlyClose(t *testing.T) {
	for _, hd := range pepHalfDays {
		// control: a break at 12:00 evaluated at 12:01:02 CT enters
		w := newPepWire(t, pepCfgOf("A", 3))
		pepFeed(t, pepBarsA(t, hd.date, "12:01", map[string]pepBar{"12:00": {25030, 25041.25, 25029, 25040}}))
		pepLive(t, 25041)
		w.at.pepEventPassAt(ctTimeAt(t, hd.date, "12:01").Add(2 * time.Second))
		waitSignal(t, w)

		// the break bar at 12:10 evaluated at 12:11:02: refused, nothing on the wire
		w = newPepWire(t, pepCfgOf("A", 3))
		pepFeed(t, pepBarsA(t, hd.date, "12:11", map[string]pepBar{"12:10": {25030, 25041.25, 25029, 25040}}))
		pepLive(t, 25041)
		now := ctTimeAt(t, hd.date, "12:11").Add(2 * time.Second)
		w.at.pepEventPassAt(now)
		w.noSignal(t, hd.date+" break at 12:10 (flat 12:10, close 12:15)")
		if reason, past := w.at.pepPastEntryCutoff(now); !past {
			t.Errorf("%s: the admission cutoff let 12:11 CT through", hd.date)
		} else if reason == "" {
			t.Errorf("%s: no reason", hd.date)
		}
		// an ordinary day's 12:11 is not past the cutoff
		if _, past := w.at.pepPastEntryCutoff(ctTimeAt(t, "2026-10-29", "12:11")); past {
			t.Errorf("an ordinary day's 12:11 was past the cutoff")
		}
	}
}

// An open position is closed at the early flat time, not held through the halt.
func TestPepFlatBeatClosesBeforeTheEarlyClose(t *testing.T) {
	for _, hd := range pepHalfDays {
		w := newPepWire(t, pepCfgOf("A", 3))
		entry := ctTimeAt(t, hd.date, "09:10").UnixMilli()
		if err := w.st.Position().Create(&store.TraderPosition{
			TraderID: w.at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
			Quantity: 3, EntryPrice: 25041, Status: "OPEN", EntryTime: entry, CreatedAt: entry, UpdatedAt: entry,
		}); err != nil {
			t.Fatal(err)
		}
		w.at.pepFlatBeat(ctTimeAt(t, hd.date, "12:09").Add(30 * time.Second))
		select {
		case <-w.cls:
			t.Fatalf("%s: flattened before the early flat time", hd.date)
		case <-time.After(300 * time.Millisecond):
		}
		w.at.pepFlatBeat(ctTimeAt(t, hd.date, hd.flat).Add(5 * time.Second))
		select {
		case <-w.cls:
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: the open position was not flattened at %s CT (market halts %s)", hd.date, hd.flat, hd.close)
		}
	}
}

// The card tells the owner the day's real flat time and counts down to it.
func TestPepTruthShowsTheEarlyFlat(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	now := ctTimeAt(t, "2026-11-27", "09:00")
	tr := w.at.PepTruthSnapshot(now)
	want := int64(ctTimeAt(t, "2026-11-27", "12:10").Sub(now) / time.Second)
	if tr.FlatTimeCT != "12:10" || tr.FlatCountdownSec != want {
		t.Fatalf("flat %q countdown %d, want 12:10 / %d", tr.FlatTimeCT, tr.FlatCountdownSec, want)
	}
	ord := w.at.PepTruthSnapshot(ctTimeAt(t, pepDay, "09:00"))
	if ord.FlatTimeCT != "15:00" {
		t.Fatalf("an ordinary day's card says %q", ord.FlatTimeCT)
	}
}

// DST/ordinary flat is untouched by the calendar read (control for the pull-in).
func TestPepOrdinaryDayFlatIsStill1500(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 3))
	for _, d := range []string{"2026-11-02", "2026-03-09", "2026-11-03"} {
		if cfg := w.at.pepConfigAt(ctTimeAt(t, d, "10:00")); cfg.FlatMin != 900 {
			t.Errorf("%s: flat %d, want 900", d, cfg.FlatMin)
		}
	}
}

// THE BOX STAYS ON THE CARD (lens 6 P2): after the signal pass, the NEXT pass
// (position open / trade taken) used to wipe the box on the snapshot.
//
// MUTANT: remove diagBox() from kernel/pep Evaluate's early refusals -> RED.
func TestPepTruthKeepsTheBoxAfterTheTradeIsTaken(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 6))
	pepFeed(t, pepBarsA(t, pepDay, "08:39", pepBreakUp))
	pepLive(t, 25041)
	now := pepNow(t)
	w.at.pepEventPassAt(now)
	waitSignal(t, w)
	// the next final 1m bar: a second pass, now refused as "already traded"
	w.at.pepEventPassAt(now.Add(time.Minute))
	tr := w.at.PepTruthSnapshot(now.Add(2 * time.Minute))
	if tr.TradesToday != 1 || tr.Stop == 0 {
		t.Fatalf("setup: trade facts %+v", tr)
	}
	if !tr.Box.Drawn || tr.Box.High != 25040 || tr.Box.Low != 25000 || tr.Box.Size != 40 {
		t.Fatalf("after the trade the card's box is %+v (the trade shows stop %.2f, the box must show too)", tr.Box, tr.Stop)
	}
	// the refusal that ended the second pass is the named one, so this pin does
	// exercise the early-refusal path
	got := ""
	for _, r := range tr.Refusals {
		got += r.Code + " "
	}
	if got == "" {
		t.Fatalf("no refusal on the second pass: %+v", tr)
	}
}
