package trader

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// m467 (release #62) - the owner must learn HOW LONG the bot was blind. On
// 11-03 NT8 sent no live bar 02:00 -> 17:02 CT (the box was offline, so even the
// one FEED DOWN push failed). The owner got no "still down" word in 15 hours,
// and the only BACK message (17:11, a new 17:00 episode after the daily break)
// said nothing about the 02:00 start.

// MUTANT (RED before m467): no reminder path -> only the first DOWN message in
// a 2h15m outage.
func TestFeedStillDownReminderEveryHour(t *testing.T) {
	rec, flush := installPush(t)
	at, _ := feedTrader(t)
	now := cmeOpenNow(t) // 12:00 CT Monday
	stale := now.UnixMilli() - 11*60_000 - 60_000
	withProvider(t, []market.Kline{{OpenTime: stale}})
	for m := 0; m <= 135; m++ {
		at.monitorTick(now.Add(time.Duration(m) * time.Minute))
	}
	flush()
	msgs := rec.all()
	if got := countContaining(msgs, "STILL DOWN"); got != 2 {
		t.Fatalf("a 2h15m outage wants 2 hourly STILL DOWN reminders, got %d: %q", got, msgs)
	}
	if !strings.Contains(strings.Join(msgs, "\n"), "since 08-17 11:49 CT") {
		t.Fatalf("the reminder must name when the feed went down: %q", msgs)
	}
}

// The BACK message says how long the feed was down and that entries were paused.
func TestFeedBackSaysHowLongItWasDown(t *testing.T) {
	rec, flush := installPush(t)
	at, _ := feedTrader(t)
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 11*60_000 - 60_000}})
	at.monitorTick(now)
	later := now.Add(3 * time.Hour)
	withProvider(t, []market.Kline{{OpenTime: later.UnixMilli() - 30_000 - 60_000}})
	at.monitorTick(later)
	flush()
	var back string
	for _, m := range rec.all() {
		if strings.Contains(m, "BACK") {
			back = m
		}
	}
	for _, want := range []string{"down since 08-17 11:49 CT", "3h11m", "no new entry"} {
		if !strings.Contains(back, want) {
			t.Fatalf("BACK message %q lacks %q", back, want)
		}
	}
}

// The 11-03 shape: down from 02:00 CT, CME closes 16:00 (the episode ends
// without a BACK), reopens 17:00 still blind (a new 17:00 episode), the feed
// returns 17:02. The BACK names the 02:00 start, not 17:00.
func TestFeedBackAcrossTheDailyBreakNamesTheTrueStart(t *testing.T) {
	rec, flush := installPush(t)
	at, _ := feedTrader(t)
	start := ctTimeAt(t, "2026-11-03", "02:00")
	withProvider(t, []market.Kline{{OpenTime: start.UnixMilli() - 60_000}})
	at.monitorTick(ctTimeAt(t, "2026-11-03", "02:11"))
	at.monitorTick(ctTimeAt(t, "2026-11-03", "16:30")) // closed
	at.monitorTick(ctTimeAt(t, "2026-11-03", "17:11")) // reopened, still blind
	back := ctTimeAt(t, "2026-11-03", "17:12")
	withProvider(t, []market.Kline{{OpenTime: back.UnixMilli() - 30_000 - 60_000}})
	at.monitorTick(back)
	flush()
	var msg string
	for _, m := range rec.all() {
		if strings.Contains(m, "BACK") {
			msg = m
		}
	}
	if !strings.Contains(msg, "down since 11-03 02:00 CT") || !strings.Contains(msg, "15h12m") {
		t.Fatalf("the BACK after the daily break must name the 02:00 start and 15h12m: %q (all %q)", msg, rec.all())
	}
}
