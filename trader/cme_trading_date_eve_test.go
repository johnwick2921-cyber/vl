package trader

// m471 L1-2 / L1-3 — the 17:00-23:59 CT evening belongs to the NEXT trading
// date (kernel.CMETradingDateKey), and the trading gates now read that date's
// calendar row, the same one the data gates (kernel.IsCMETradingExpected) read.
//
//   - L1-2: Wed 2026-11-25 evening (trading date 11-26, a FULL CLOSURE) traded
//     — a mentor arm was admitted — while the data-truth checks called every
//     minute "not expected", so a feed hole could not pause entries.
//   - L1-3: Thu 12-24 / 12-31 evenings (trading dates 12-25 / 2027-01-01,
//     closed) read OPEN, so checkFeedDown raised a P0 FEED DOWN (an owner-push
//     kind) at ~17:10; and Thu 11-26 evening (trading date 11-27, shortened —
//     CME reopens 17:00) read CLOSED.
//
// Production call sites: admitEntry (mentor arm), checks.JudgeSeries with the
// shipped data-truth calendar, checkFeedDown, ownerPushKind.

import (
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth/checks"
	"vl/kernel"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
)

// eveningHoleBars: 1m bars for 08:00-15:59 CT only, so a 17:00-19:59 evening
// carries a 3-hour feed hole.
func eveningHoleBars(day time.Time) []checks.Bar {
	var out []checks.Bar
	for t := day.Add(8 * time.Hour); t.Before(day.Add(16 * time.Hour)); t = t.Add(time.Minute) {
		out = append(out, checks.Bar{T: t.UnixMilli(), O: 100, H: 101, L: 99, C: 100 + float64(t.Minute()%5), V: 10, Final: true})
	}
	return out
}

func TestThanksgivingEveTradingAndDataGatesAgree(t *testing.T) {
	ct := chicagoLoc()
	spec := checks.SeriesSpec{Name: "ema34_1m", TFMin: 1, Period: 34, EMA: true, MaxWeight: 0.05}
	open := (&ntwire.DataTruthSources{}).Open()

	at := mkPlanTrader(nil)
	at.config.StrategyConfig.RiskControl.MentorMode = true
	at.id = "m471-l12"

	// Control: an ordinary Wednesday evening is a trading session — the mentor
	// arm passes the CME gate, and the data gate catches the same feed hole.
	ctl := time.Date(2026, 11, 18, 0, 0, 0, 0, ct)
	if r, refused := at.admitEntry(admitIntent{Path: admitArm, MentorArm: true, Symbol: "MNQ", Action: "open_long", Now: ctl.Add(20 * time.Hour), Key: "ctl"}); refused && strings.HasPrefix(r, "cme_closed") {
		t.Fatalf("control: Wed 11-18 20:00 CT is open, got %q", r)
	}
	if b, why := checks.JudgeSeries(spec, eveningHoleBars(ctl), ctl.Add(20*time.Hour), open); !b {
		t.Fatalf("control: an ordinary Wed 20:00 CT with a 3h evening hole must block, got %v %q", b, why)
	}

	day := time.Date(2026, 11, 25, 0, 0, 0, 0, ct)
	for _, hhmm := range [][2]int{{17, 0}, {17, 15}, {20, 0}, {23, 59}} {
		now := day.Add(time.Duration(hhmm[0])*time.Hour + time.Duration(hhmm[1])*time.Minute)
		if kernel.IsCMEOpen(now) != kernel.IsCMETradingExpected(now) {
			t.Errorf("%s: IsCMEOpen=%v but IsCMETradingExpected=%v — the trading and data gates disagree",
				now.Format("2006-01-02 15:04"), kernel.IsCMEOpen(now), kernel.IsCMETradingExpected(now))
		}
		r, refused := at.admitEntry(admitIntent{Path: admitArm, MentorArm: true, Symbol: "MNQ", Action: "open_long", Now: now, Key: "l12"})
		if !refused || !strings.HasPrefix(r, "cme_closed") || !strings.Contains(r, "Thanksgiving") {
			t.Errorf("%s CT (trading date 11-26 = Thanksgiving, FULL CLOSURE): a mentor arm must be refused cme_closed naming the holiday, got refused=%v %q",
				now.Format("2006-01-02 15:04"), refused, r)
		}
	}
}

func TestEveningBeforeClosedTradingDateRaisesNoFeedDown(t *testing.T) {
	ct := chicagoLoc()
	rows := []struct {
		label   string
		now     time.Time
		lastBar time.Time
		want    int // feed-down alerts
	}{
		// Christmas Eve: 12:15 FINAL close; 12-25 is a closed trading date.
		{"Thu 2026-12-24 17:15", time.Date(2026, 12, 24, 17, 15, 0, 0, ct), time.Date(2026, 12, 24, 12, 14, 0, 0, ct), 0},
		// New Year's Eve: normal 16:00 close; 2027-01-01 is a closed trading date.
		{"Thu 2026-12-31 17:15", time.Date(2026, 12, 31, 17, 15, 0, 0, ct), time.Date(2026, 12, 31, 15, 59, 0, 0, ct), 0},
		// Control: an ordinary Thursday evening with no bar since the reopen IS
		// a feed outage, and must still alert.
		{"Thu 2026-12-17 17:15 (control)", time.Date(2026, 12, 17, 17, 15, 0, 0, ct), time.Date(2026, 12, 17, 15, 59, 0, 0, ct), 1},
	}
	if !ownerPushKind("P0", "feed-down") {
		t.Fatal("premise: a P0 feed-down is an owner-push kind")
	}
	for _, r := range rows {
		at, st := feedTrader(t)
		withProvider(t, []market.Kline{{OpenTime: r.lastBar.UnixMilli()}})
		at.checkFeedDown(r.now)
		if n := feedAlerts(t, st, "feed-down"); n != r.want {
			t.Errorf("%s CT: feed-down alerts=%d, want %d (IsCMEOpen=%v, trading date %s)",
				r.label, n, r.want, kernel.IsCMEOpen(r.now), kernel.CMETradingDateKey(r.now))
		}
	}
}
