package trader

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vl/internal/ownerpush"
	"vl/market"
	ntTrader "vl/trader/ninjatrader"
)

// m469 — a FLAPPING bar-hole pause is ONE episode for the owner.
//
// The live night (vl_2026-11-04.log, log_events): from 11-05 21:02 to 11-06
// 05:03 CT NT8 bars arrived ~10 minutes late, so a 3-minute hole paused every
// trader for 2 minutes in every 10 (paused xx:02, resumed xx:04) - 49 times -
// then a last pause from 05:04 stood until 08:42 (3h38m). Every pause and every
// resume was its own owner push, per trader, at the 3/hour budget cap: 188 of
// 220 owner pushes from 11-04 17:10 to 11-06 13:45 were this.
//
// The rule (the data-truth merge rule, datatruth.ReopenMergeWindow): the FIRST
// pause of an episode is pushed at once, per trader, and so is its FIRST resume
// (nothing about it waits in memory, so a restart inside the window can never
// leave "TRADING PAUSED" as the owner's last word); a pause back within the
// merge window of a resume is the same episode (logged and counted, not
// pushed); such a merged episode gets ONE summary, with the count and the
// longest pause, once the pause has stayed away for the window. The hourly
// STILL PAUSED reminder keeps running for any unbroken pause that lasts.
//
// MUTANT (each shown RED in the wave report):
//   M1 integrityReopenMerge() returns 0          -> FlapNight (54 pushes for one trader) RED
//   M2 an un-pushed episode waits for a resume too -> TestIntegrityUnpushedPauseIsNeverMergedAway RED
//   M3 no resume flushed before a new episode     -> TestIntegrityMissedWindowSendsTheResumeBeforeTheNewPause RED
//   M4 the reminder clock is the episode's since  -> FlapNight RED (a reminder on every re-pause)
//   M5 the first resume waits for the window      -> TestIntegrityIsolatedPauseResumesAtOnce RED
//   M6 a merged re-pause is not counted           -> TestIntegrityMergedRePauseIsStillCountedAsAPause RED

// flapNight replays the 11-05/06 pattern at 1-minute resolution through the
// PRODUCTION beat (monitorTick) for three traders, with the owner-push channel
// on the same fake clock (its 3-per-hour budget is real). It returns every
// delivered owner message.
func flapNight(t *testing.T, until time.Time, blockedAt func(now time.Time) bool) []string {
	t.Helper()
	start := ctTimeAt(t, "2026-11-05", "21:00")
	var tickNs atomic.Int64
	tickNs.Store(start.UnixNano())
	clock := func() time.Time { return time.Unix(0, tickNs.Load()) }

	oldClk := ownerPushClock
	ownerPushClock = clock
	t.Cleanup(func() { ownerPushClock = oldClk })
	rec := &pushRec{}
	p := ownerpush.New(rec.send)
	p.SetClockForTest(clock)
	t.Cleanup(ownerpush.Use(p))

	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline {
		return []market.Kline{{OpenTime: clock().UnixMilli() - 30_000, Final: true}} // the feed itself is live
	}
	t.Cleanup(func() { market.FuturesBarsProvider = old })
	fk := &intFake{blockedFor: func(int) bool { return blockedAt(clock()) }}
	armIntegrityGate(t, fk)

	var traders []*AutoTrader
	for _, name := range []string{"hoang", "12345", "4567489"} {
		at, _ := feedTrader(t)
		at.id = "m469-" + name
		at.name = "Trader " + name
		traders = append(traders, at)
	}
	for now := start; !now.After(until); now = now.Add(time.Minute) {
		tickNs.Store(now.UnixNano())
		for _, at := range traders {
			at.monitorTick(now)
		}
		p.Flush()
	}
	return rec.all()
}

// flapBlocked is the night's verdict at a minute: the 49 two-minute flaps
// (21:02+10k .. 21:04+10k, k=0..48), then - when longFrom is set - one unbroken
// pause from longFrom to longTo.
func flapBlocked(t *testing.T, longFrom, longTo time.Time) func(time.Time) bool {
	first := ctTimeAt(t, "2026-11-05", "21:02")
	return func(now time.Time) bool {
		if !longFrom.IsZero() && !now.Before(longFrom) && now.Before(longTo) {
			return true
		}
		d := now.Sub(first)
		if d < 0 || d >= 49*10*time.Minute {
			return false
		}
		return d%(10*time.Minute) < 2*time.Minute
	}
}

func traderMsgs(msgs []string, name string) []string {
	var out []string
	for _, m := range msgs {
		if strings.Contains(m, "— Trader "+name+":") || strings.Contains(m, "— Trader "+name+",") {
			out = append(out, m)
		}
	}
	return out
}

func TestIntegrityFlapNight1105IsOnePausePerTrader(t *testing.T) {
	t.Run("the 49 flaps alone: 1 pause + its resume + 1 summary per trader", func(t *testing.T) {
		msgs := flapNight(t, ctTimeAt(t, "2026-11-06", "05:40"), flapBlocked(t, time.Time{}, time.Time{}))
		for _, name := range []string{"hoang", "12345", "4567489"} {
			mine := traderMsgs(msgs, name)
			if len(mine) != 3 || countContaining(mine, "TRADING PAUSED") != 1 || countContaining(mine, "TRADING RESUMED") != 2 {
				t.Fatalf("trader %s: 49 flaps must reach the owner as 1 pause + its resume + 1 summary, got %d: %q", name, len(mine), mine)
			}
			if !strings.Contains(mine[0], "TRADING PAUSED") || !strings.Contains(mine[1], "TRADING RESUMED") || !strings.Contains(mine[2], "TRADING RESUMED") {
				t.Fatalf("trader %s: the pause first, then its resume, the summary last: %q", name, mine)
			}
			if strings.Contains(mine[1], "came and went") || !strings.Contains(mine[1], "(paused 2m0s)") {
				t.Fatalf("trader %s: the first resume is the plain one, sent at 21:04: %q", name, mine[1])
			}
			for _, w := range []string{"came and went 49 times", "from 11-05 21:02 to 11-06 05:04 CT", "longest 2m", "no new pause in the 30 minutes since"} {
				if !strings.Contains(mine[2], w) {
					t.Fatalf("trader %s: the summary lacks %q: %q", name, w, mine[2])
				}
			}
		}
	})
	t.Run("the whole night to 08:43: + the hourly reminders of the unbroken 3h38m pause", func(t *testing.T) {
		longFrom, longTo := ctTimeAt(t, "2026-11-06", "05:05"), ctTimeAt(t, "2026-11-06", "08:43")
		msgs := flapNight(t, ctTimeAt(t, "2026-11-06", "09:20"), flapBlocked(t, longFrom, longTo))
		for _, name := range []string{"hoang", "12345", "4567489"} {
			mine := traderMsgs(msgs, name)
			if countContaining(mine, "TRADING PAUSED") != 1 || countContaining(mine, "TRADING RESUMED") != 2 || countContaining(mine, "TRADING STILL PAUSED") != 3 || len(mine) != 6 {
				t.Fatalf("trader %s: want 1 pause + its resume + 3 hourly STILL PAUSED (06:05, 07:05, 08:05) + 1 summary, got %d: %q", name, len(mine), mine)
			}
			if !strings.Contains(mine[len(mine)-1], "TRADING RESUMED") {
				t.Fatalf("trader %s: nothing may say RESUMED before the pause is over: %q", name, mine)
			}
			for _, w := range []string{"came and went 50 times", "from 11-05 21:02 to 11-06 08:43 CT", "longest 3h38m"} {
				if !strings.Contains(mine[len(mine)-1], w) {
					t.Fatalf("trader %s: the summary lacks %q: %q", name, w, mine[len(mine)-1])
				}
			}
			for _, m := range mine {
				if strings.Contains(m, "STILL PAUSED") && !strings.Contains(m, "since 11-06 05:05 CT") {
					t.Fatalf("trader %s: a reminder counts the UNBROKEN pause (since 05:05), not the flapping episode: %q", name, m)
				}
			}
		}
	})
}

// A single, real pause is told AT ONCE, and so is its resume, on the beat that
// clears it - nothing waits in memory, so a restart, an Update or a stopped
// trader inside the merge window cannot leave "TRADING PAUSED" as the owner's
// last word (review finding: the resume used to wait 30 minutes, in memory
// only). Nothing more follows across the window; a hole after it is a new
// episode.
func TestIntegrityIsolatedPauseResumesAtOnce(t *testing.T) {
	rec, flush := installPush(t)
	at, _ := feedTrader(t)
	at.name = "Mentor A"
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})
	fk := &intFake{}
	armIntegrityGate(t, fk)
	k := integrityReopenMerge()
	if k != 30*time.Minute {
		t.Fatalf("the merge window is the data-truth rule's 30 minutes, got %s", k)
	}

	at.dataIntegrityBeat(now)
	flush()
	if msgs := rec.all(); len(msgs) != 1 || !strings.Contains(msgs[0], "TRADING PAUSED") {
		t.Fatalf("the first pause is pushed at its first beat: %q", msgs)
	}
	fk.blockedFor = func(int) bool { return false }
	cleared := now.Add(3 * time.Minute)
	at.dataIntegrityBeat(cleared)
	flush()
	msgs := rec.all()
	if n := countContaining(msgs, "TRADING RESUMED"); n != 1 {
		t.Fatalf("the resume of a single pause is pushed on the beat that clears it: %q", msgs)
	}
	for _, w := range []string{"Mentor A", "(paused 3m0s)"} {
		if !strings.Contains(msgs[1], w) {
			t.Fatalf("the resume lacks %q: %q", w, msgs[1])
		}
	}
	for m := time.Minute; m <= k+time.Minute; m += time.Minute {
		at.dataIntegrityBeat(cleared.Add(m))
	}
	flush()
	if got := rec.all(); len(got) != 2 {
		t.Fatalf("nothing more after a single pause's resume, across the whole window: %q", got)
	}
	fk.blockedFor = nil
	at.dataIntegrityBeat(cleared.Add(k + 5*time.Minute))
	flush()
	if n := countContaining(rec.all(), "TRADING PAUSED"); n != 2 {
		t.Fatalf("a hole after the window is a NEW episode, pushed at once: %q", rec.all())
	}
}

// /api/health trading_paused.counts: the episode counter stays ONE PER PAUSE
// (telemetry/data_integrity.go promises "how many times did missing data pause
// trading today is a read"), so a pause merged into its episode for the owner
// push is still counted - episodes and resumes move together.
func TestIntegrityMergedRePauseIsStillCountedAsAPause(t *testing.T) {
	_, flush := installPush(t)
	at, _ := feedTrader(t)
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})
	fk := &intFake{}
	armIntegrityGate(t, fk)
	before := telemetryEpisodes(ntTrader.IntegrityClassHole)
	for i := 0; i < 3; i++ { // paused 2 min in every 10: one owner episode
		t0 := now.Add(time.Duration(i) * 10 * time.Minute)
		fk.blockedFor = nil
		at.dataIntegrityBeat(t0)
		fk.blockedFor = func(int) bool { return false }
		at.dataIntegrityBeat(t0.Add(2 * time.Minute))
	}
	flush()
	if got := telemetryEpisodes(ntTrader.IntegrityClassHole); got != before+3 {
		t.Fatalf("three pauses (two merged into the first for the owner push) must count 3, got %d", got-before)
	}
}

// The merge window passed with no beat to see it (a skipped beat), and the next
// beat is a new pause: the earlier episode's summary goes out FIRST, then the
// new pause - never a pause swallowed, never a resume after it. A single pause
// had its resume at once; a flapped one gets its summary before the new pause.
func TestIntegrityMissedWindowSendsTheResumeBeforeTheNewPause(t *testing.T) {
	for _, flapped := range []bool{false, true} {
		rec, flush := installPush(t)
		at, _ := feedTrader(t)
		now := cmeOpenNow(t)
		withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})
		fk := &intFake{}
		armIntegrityGate(t, fk)
		at.dataIntegrityBeat(now)
		fk.blockedFor = func(int) bool { return false }
		at.dataIntegrityBeat(now.Add(2 * time.Minute))
		last := now.Add(2 * time.Minute)
		if flapped {
			fk.blockedFor = nil
			at.dataIntegrityBeat(now.Add(10 * time.Minute)) // back inside the window: merged
			fk.blockedFor = func(int) bool { return false }
			last = now.Add(12 * time.Minute)
			at.dataIntegrityBeat(last)
		}
		fk.blockedFor = nil
		at.dataIntegrityBeat(last.Add(integrityReopenMerge() + time.Minute))
		flush()
		msgs := rec.all()
		want := []string{"TRADING PAUSED", "TRADING RESUMED", "TRADING PAUSED"}
		if flapped {
			want = []string{"TRADING PAUSED", "TRADING RESUMED", "came and went 2 times", "TRADING PAUSED"}
		}
		ok := len(msgs) == len(want)
		for i := 0; ok && i < len(want); i++ {
			ok = strings.Contains(msgs[i], want[i])
		}
		if !ok {
			t.Fatalf("flapped=%v: want %q in that order, got %q", flapped, want, msgs)
		}
	}
}

// Only a pause the owner WAS told waits for a resume push. An unknown detector
// inside its grace is never pushed; when it clears, the owner hears nothing at
// all (no "resumed" for a pause he never heard of), and a real hole that
// follows minutes later is a pause he has not heard of - it must be pushed.
func TestIntegrityUnpushedPauseIsNeverMergedAway(t *testing.T) {
	for _, holeAfter := range []bool{false, true} {
		rec, flush := installPush(t)
		at, _ := feedTrader(t)
		now := cmeOpenNow(t)
		withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})
		fk := &intFake{class: ntTrader.IntegrityClassUnknown}
		armIntegrityGate(t, fk)
		at.dataIntegrityBeat(now) // unknown, inside the grace: not pushed
		fk.blockedFor = func(int) bool { return false }
		at.dataIntegrityBeat(now.Add(time.Minute))
		if !holeAfter {
			for m := time.Duration(2); m <= 40; m++ {
				at.dataIntegrityBeat(now.Add(m * time.Minute))
			}
			flush()
			if msgs := rec.all(); len(msgs) != 0 {
				t.Fatalf("an untold pause that cleared must reach the owner as nothing at all: %q", msgs)
			}
			continue
		}
		fk.blockedFor, fk.class = nil, ntTrader.IntegrityClassHole
		at.dataIntegrityBeat(now.Add(5 * time.Minute)) // a real hole, inside the window
		flush()
		if n := countContaining(rec.all(), "TRADING PAUSED"); n != 1 {
			t.Fatalf("a hole after an UNTOLD pause must be pushed, not merged into it: %q", rec.all())
		}
	}
}
