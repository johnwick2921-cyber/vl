package checks

import (
	"testing"
	"time"
)

// TestBadPrintRealNewsBarsNeverPause is the "must NOT pause on real market
// moves" proof: every window below is a real news / open / fast-tape stretch of
// the store (CPI 208 pt, FOMC 112 pt, the 10-15 15:56 96 pt sell, 08:30 prints)
// and the replay - exercised exactly as production does, bar by bar, forming
// then closed - never flags a bar.
func TestBadPrintRealNewsBarsNeverPause(t *testing.T) {
	w := loadWindows(t)
	p := DefaultBadPrint()
	total := 0
	for name, win := range w {
		if len(name) < 5 || name[:5] != "news_" {
			continue
		}
		mnq, es := toBars(win.Mnq), toBars(win.Es)
		if len(mnq) < 40 {
			t.Fatalf("%s: fixture too short (%d)", name, len(mnq))
		}
		total += len(mnq)
		if got := replayBadPrint(mnq, es, p); len(got) != 0 {
			for ts, r := range got {
				t.Errorf("%s: REAL bar %s flagged (first eval %s)", name, time.UnixMilli(ts).UTC().Format("15:04Z"), time.UnixMilli(r[0]).UTC().Format("15:04:05Z"))
			}
		}
	}
	if total < 800 {
		t.Fatalf("expected a substantial real sample, got %d bars", total)
	}
}

// TestBadPrintRealBadPrintsPause pins the two real bad prints in the store
// (rows 2020662 and 2020732): flagged while inside the window, gone after.
func TestBadPrintRealBadPrintsPause(t *testing.T) {
	win := loadWindows(t)["bad_1019_0858"]
	mnq, es := toBars(win.Mnq), toBars(win.Es)
	rowOf := map[int64]float64{}
	for _, r := range win.Mnq {
		rowOf[int64(f(r[0]))] = rowID(r)
	}
	got := replayBadPrint(mnq, es, DefaultBadPrint())
	want := map[float64]bool{2020662: false, 2020732: false}
	for ts, r := range got {
		id := rowOf[ts]
		if _, ok := want[id]; !ok {
			t.Errorf("unexpected flag on row %.0f (%s)", id, time.UnixMilli(ts).UTC().Format("15:04Z"))
			continue
		}
		want[id] = true
		// flagged from its own minute, and no longer once 30+ minutes old
		if r[0] > ts+90_000 {
			t.Errorf("row %.0f first flagged late (%d s after open)", id, (r[0]-ts)/1000)
		}
		if r[1] > ts+int64(DefaultBadPrint().WindowMin)*minuteMs+minuteMs {
			t.Errorf("row %.0f still flagged %d min after open", id, (r[1]-ts)/minuteMs)
		}
	}
	for id, ok := range want {
		if !ok {
			t.Errorf("bad print row %.0f was NOT flagged", id)
		}
	}
}

// TestBadPrintStale1014 reconstructs the 10-14 20:00 CT stale print from its
// documented values (provider/ninjatrader/bar_stale_print.go: MNQ o 29931.25 /
// h 30754.0 / l 29927.0 / c 30753.75; ES c 7845.25 against a real ~7744) on the
// REAL surrounding bars of the store.
func TestBadPrintStale1014(t *testing.T) {
	win := loadWindows(t)["stale_1014_2000"]
	mnq, es := toBars(win.Mnq), toBars(win.Es)
	at := int64(0)
	for i := range mnq {
		if time.UnixMilli(mnq[i].T).UTC().Format("15:04") == "01:00" { // 20:00 CT
			mnq[i].H, mnq[i].C = 30754.0, 30753.75
			at = mnq[i].T
			if i+1 < len(mnq) {
				// the print was a stale-series replay, the real tape resumes
				// from the real level on the next minute
				_ = i
			}
		}
	}
	for i := range es {
		if es[i].T == at {
			es[i].C, es[i].H = 7845.25, 7845.25
		}
	}
	if at == 0 {
		t.Fatal("20:00 CT bar not found in fixture")
	}
	got := replayBadPrint(mnq, es, DefaultBadPrint())
	if _, ok := got[at]; !ok {
		t.Fatalf("the 10-14 stale print was not flagged: %v", got)
	}
	// the clean version of the same window flags nothing
	clean := toBars(win.Mnq)
	if g := replayBadPrint(clean, toBars(win.Es), DefaultBadPrint()); len(g) != 0 {
		t.Fatalf("clean 10-14 window flagged: %v", g)
	}
}

// TestBadPrintClearsWhenReplaced: the suspect bar is replaced by a re-request
// (the corrected values) -> the very next evaluation is clear.
func TestBadPrintClearsWhenReplaced(t *testing.T) {
	win := loadWindows(t)["bad_1019_0858"]
	mnq, es := toBars(win.Mnq), toBars(win.Es)
	var badT int64
	for _, r := range win.Mnq {
		if rowID(r) == 2020732 {
			badT = int64(f(r[0]))
		}
	}
	now := time.UnixMilli(badT + 90_000)
	cut := func(b []Bar) (o []Bar) {
		for _, x := range b {
			if x.T <= now.UnixMilli() {
				o = append(o, x)
			}
		}
		return
	}
	if len(SuspectBars(cut(mnq), cut(es), now, DefaultBadPrint())) == 0 {
		t.Fatal("precondition: bad bar must be suspect")
	}
	for i := range mnq {
		if mnq[i].T == badT { // re-request returns the real minute: no 163 pt wick
			mnq[i].L = 29836.25
		}
	}
	for _, s := range SuspectBars(cut(mnq), cut(es), now, DefaultBadPrint()) {
		if s.T == badT {
			t.Fatalf("a replaced bar must clear: %+v", s)
		}
	}
}

// TestBadPrintMissingPartnerFailsClosed: an extreme bar with no ES bar for that
// minute cannot be confirmed by ES or volume - only continuation can.
func TestBadPrintMissingPartnerFailsClosed(t *testing.T) {
	win := loadWindows(t)["bad_1019_0858"]
	mnq := toBars(win.Mnq)
	var badT int64
	for _, r := range win.Mnq {
		if rowID(r) == 2020732 {
			badT = int64(f(r[0]))
		}
	}
	now := time.UnixMilli(badT + 30_000)
	var m []Bar
	for _, x := range mnq {
		if x.T <= now.UnixMilli() {
			m = append(m, x)
		}
	}
	found := false
	for _, s := range SuspectBars(m, nil, now, DefaultBadPrint()) {
		if s.T == badT && s.ESRatio == -1 {
			found = true
		}
	}
	if !found {
		t.Fatal("no partner: the 163 pt wick must stay suspect (ES missing cannot confirm)")
	}
}
