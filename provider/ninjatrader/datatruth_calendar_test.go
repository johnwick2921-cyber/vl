package ninjatrader

import (
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	"vl/kernel"
)

// ── #55 m449 — ONE CALENDAR FOR EVERY DATA GATE ─────────────────────────────
//
// m445 (the hole tracker) expects a bar only where kernel.IsCMETradingExpected
// says so (the weekly schedule AND a trading date the calendar does not call
// closed). m446's checks used kernel.IsCMEOpen alone, which reads the evening
// before Thanksgiving / Christmas / New Year's Day as open: on gap-free data
// that carries no bars on those evenings the lookback checks paused entries for
// the whole evening (418 min, with an owner push) and again after the reopen,
// while the phantom open-but-barless slots sat in their open-aware windows.
//
// MUTANT (shown RED): DataTruthSources.Open back to kernel.IsCMEOpen alone ->
// both pins RED.

var calendarEves = []time.Time{
	time.Date(2026, 11, 25, 20, 0, 0, 0, kernel.CTLocation()), // Wed before Thanksgiving
	time.Date(2026, 12, 24, 20, 0, 0, 0, kernel.CTLocation()), // Thu before Christmas
	time.Date(2026, 12, 31, 20, 0, 0, 0, kernel.CTLocation()), // Thu before New Year's Day
}

func TestDataTruthOpenIsTheExpectedTradingCalendar(t *testing.T) {
	open := (&DataTruthSources{}).Open()
	for _, eve := range calendarEves {
		if kernel.IsCMEOpen(eve) == false {
			t.Fatalf("fixture: %s should read open by the instant alone", eve)
		}
		if open(eve.UnixMilli()) {
			t.Fatalf("%s: a trading date the calendar calls closed is not 'open' for the m446 checks", eve.Format("01-02 15:04"))
		}
	}
	// minute for minute, the m446 calendar IS the m445 calendar over the holiday weeks
	from := time.Date(2026, 11, 23, 0, 0, 0, 0, kernel.CTLocation())
	to := time.Date(2027, 1, 8, 0, 0, 0, 0, kernel.CTLocation())
	for m := from; m.Before(to); m = m.Add(time.Minute) {
		if open(m.UnixMilli()) != kernel.IsCMETradingExpected(m) {
			t.Fatalf("calendars disagree at %s", m.Format("2006-01-02 15:04"))
		}
	}
	// and an ordinary Wednesday evening still trades
	if !open(time.Date(2026, 10, 28, 20, 0, 0, 0, kernel.CTLocation()).UnixMilli()) {
		t.Fatal("an ordinary evening is open")
	}
}

// Gap-free data that is present exactly where the calendar EXPECTS bars must not
// pause the lookback checks on a holiday eve or after its reopen.
func TestGapFreeDataAroundAHolidayEveDoesNotPauseTheLookbackChecks(t *testing.T) {
	freshLedger(t)
	c := NewBarCache(6000)
	var bars []Bar
	start := time.Date(2026, 11, 20, 8, 0, 0, 0, kernel.CTLocation())
	end := time.Date(2026, 11, 27, 4, 0, 0, 0, kernel.CTLocation())
	for m := start; m.Before(end); m = m.Add(time.Minute) {
		if kernel.IsCMETradingExpected(m) {
			bars = append(bars, Bar{T: m.UnixMilli(), O: 25000, H: 25001, L: 24999, C: 25000, V: 10, Final: true})
		}
	}
	// SeedHistorical takes NT8's CLOSE-stamped frames and open-stamps them once at
	// ingress (openStampBars): seeding open-stamped rows would shift every frame
	// one bucket early and leave the newest slot of each day missing - invisible to
	// the price-tolerant EMA/RSI specs on this flat tape, but not to m450's strict
	// presence read of the last 12 native 5m bars (mentor_strongday_5m). The store
	// rows (SwingInputs below) stay open-stamped, as the store holds them.
	closeStamped := func(in []Bar, tfMin int) []Bar {
		out := make([]Bar, len(in))
		for i, b := range in {
			b.T += int64(tfMin) * 60_000
			out[i] = b
		}
		return out
	}
	c.SeedHistorical("MNQ", "1m", closeStamped(bars, 1))
	for _, tf := range []struct {
		name string
		min  int
	}{{"3m", 3}, {"5m", 5}, {"15m", 15}, {"1h", 60}} {
		var hb []Bar
		for _, b := range bars {
			if b.T/60_000%int64(tf.min) == 0 { // the frame's own bucket opens
				hb = append(hb, b)
			}
		}
		c.SeedHistorical("MNQ", tf.name, closeStamped(hb, tf.min))
	}
	ingestLastSum.Store(time.Now().Unix())
	src := &DataTruthSources{Cache: c, SnapshotTTL: time.Nanosecond}
	conv := func(in []Bar) []checks.Bar {
		out := make([]checks.Bar, len(in))
		for i, b := range in {
			out[i] = checks.Bar{T: b.T, O: b.O, H: b.H, L: b.L, C: b.C, V: b.V, Final: true}
		}
		return out
	}
	var m5 []Bar
	for _, b := range bars {
		if b.T/60_000%5 == 0 {
			m5 = append(m5, b)
		}
	}
	WireDataTruth(src, func(string, time.Time) ([]checks.Bar, []checks.Bar, float64, error) {
		return conv(bars), conv(m5), 0, nil // the store holds exactly what the calendar expects
	})
	for _, at := range []time.Time{
		time.Date(2026, 11, 25, 22, 0, 0, 0, kernel.CTLocation()), // the eve itself
		time.Date(2026, 11, 27, 2, 30, 0, 0, kernel.CTLocation()), // after the reopen
	} {
		_, reasons := datatruth.Evaluate("MNQ", at)
		for _, r := range reasons {
			if r.Check == "indicator_lookback" || r.Check == "indicator_lookback_session" {
				t.Fatalf("%s: %s paused on gap-free data (%s)", at.Format("01-02 15:04"), r.Check, r.Detail)
			}
		}
	}
}
