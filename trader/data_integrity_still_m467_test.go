package trader

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// m467 (release #62): a bar-hole TRADING PAUSE that lasts is news again every
// hour, and the resume withdraws a reminder still held (an outage that also
// took the network).
//
// MUTANT (RED before m467): no reminder -> one PAUSED push in 2h10m.
func TestIntegrityStillPausedReminderHourly(t *testing.T) {
	rec, flush := installPush(t)
	at, _ := feedTrader(t)
	at.name = "Mentor A"
	now := cmeOpenNow(t)
	var tick time.Time
	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline {
		return []market.Kline{{OpenTime: tick.UnixMilli() - 30_000, Final: true}} // the feed is live
	}
	t.Cleanup(func() { market.FuturesBarsProvider = old })
	fk := &intFake{}
	armIntegrityGate(t, fk)
	for m := 0; m <= 130; m++ {
		tick = now.Add(time.Duration(m) * time.Minute)
		at.monitorTick(tick)
	}
	flush()
	msgs := rec.all()
	if n := countContaining(msgs, "TRADING STILL PAUSED"); n != 2 {
		t.Fatalf("a 2h10m pause wants 2 hourly STILL PAUSED reminders, got %d: %q", n, msgs)
	}
	if n := countContaining(msgs, "TRADING PAUSED"); n != 1 {
		t.Fatalf("still exactly one PAUSED push: %q", msgs)
	}
	var last string
	for _, m := range msgs {
		if strings.Contains(m, "STILL PAUSED") {
			last = m
		}
	}
	for _, w := range []string{"Mentor A", "since 08-17 12:00 CT", "2h00m", "exits, stops and protection are unaffected"} {
		if !strings.Contains(last, w) {
			t.Fatalf("reminder %q lacks %q", last, w)
		}
	}
}
