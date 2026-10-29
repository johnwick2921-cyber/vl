package trader

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"vl/logger"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── m445 round 6 (D-new1): the log and the push say what is REALLY paused ──
//
// The deploy scenario: the 10-26 overnight hole sits inside the SWING window
// (24720 open minutes) but left the intraday one (1500) on 10-27. A mentor
// trader is therefore paused for SWING4H only; intraday mentor setups trade.
// dataIntegrityBeat used to log "NEW entries refused on every path" and push
// "New entries are blocked and resting entry orders are being cancelled", and
// its log tail promised "Resumes by itself once the bars are measured filled" —
// all false. Pins run through monitorTick (the production call site).
//
// Mutants (each shown RED):
//   M1  integrityPauseScope always returns pauseScopeAll        -> SwingOnly + Narrow RED
//   M2  integrityPauseWording ignores the scope (always "every path") -> SwingOnly RED
//   M3  tail always "Resumes by itself once the bars are measured filled" -> tail asserts RED
//   M4  drop the e.scope != scope block                          -> Widen/Narrow RED

func captureWarnLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := logger.Log.Out
	logger.Log.SetOutput(&buf)
	t.Cleanup(func() { logger.Log.SetOutput(old) })
	return &buf
}

func scopeTextTrader(t *testing.T, mentor bool) *AutoTrader {
	t.Helper()
	if !mentor {
		at, _ := feedTrader(t)
		at.name = "Planner A"
		return at
	}
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.exchange = "ninjatrader"
	at.name = "Mentor A"
	seedMeasured(at)
	return at
}

func TestSwingOnlyPauseIsToldAsSwingOnlyInTheLogAndThePush(t *testing.T) {
	rec, flush := installPush(t)
	logs := captureWarnLog(t)
	at := scopeTextTrader(t, true)
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})
	fk := &intFake{clearsAtMs: ctTimeAt(t, "2026-11-19", "19:57").UnixMilli()}
	fk.blockedFor = func(openMin int) bool { return openMin == ntTrader.IntegritySwingOpenMin }
	armIntegrityGate(t, fk)

	at.monitorTick(now)
	flush()
	msgs := rec.all()
	if n := countContaining(msgs, "TRADING PAUSED"); n != 1 {
		t.Fatalf("one pause push, got %d: %q", n, msgs)
	}
	push, log := msgs[0], logs.String()
	for name, text := range map[string]string{"push": push, "log": log} {
		for _, must := range []string{
			"SWING4H entries refused",
			"intraday mentor setups trade normally",
			"only resting SWING4H entry orders are withdrawn",
			"Clears by itself 11-19 19:57 CT",
			"imported",
			"exits, stops",
		} {
			if !strings.Contains(text, must) {
				t.Fatalf("the %s of a SWING4H-only pause must say %q: %q", name, must, text)
			}
		}
		for _, mustNot := range []string{
			"on every path",
			"New entries are blocked and resting entry orders are being cancelled",
			"Resumes by itself once the bars are measured filled",
		} {
			if strings.Contains(text, mustNot) {
				t.Fatalf("the %s of a SWING4H-only pause must NOT claim %q: %q", name, mustNot, text)
			}
		}
	}
}

// The control: the same trader blocked in the INTRADAY window really refuses
// everything, and the text says so; a non-mentor trader is always "every path".
func TestFullPauseStillSaysEveryPath(t *testing.T) {
	for name, mentor := range map[string]bool{"mentor intraday blocked": true, "non-mentor": false} {
		t.Run(name, func(t *testing.T) {
			rec, flush := installPush(t)
			logs := captureWarnLog(t)
			at := scopeTextTrader(t, mentor)
			now := cmeOpenNow(t)
			withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})
			fk := &intFake{clearsAtMs: ctTimeAt(t, "2026-11-06", "13:57").UnixMilli()}
			armIntegrityGate(t, fk) // blocked in every window
			at.monitorTick(now)
			flush()
			msgs := rec.all()
			if len(msgs) != 1 {
				t.Fatalf("one push: %q", msgs)
			}
			for name, text := range map[string]string{"push": msgs[0], "log": logs.String()} {
				if !strings.Contains(text, "NEW entries refused on every path") || !strings.Contains(text, "Clears by itself 11-06 13:57 CT") {
					t.Fatalf("the %s of a full pause must say every path and its clear time: %q", name, text)
				}
				if strings.Contains(text, "intraday mentor setups trade normally") {
					t.Fatalf("the %s claims intraday trades while the pause refuses it: %q", name, text)
				}
			}
		})
	}
}

// A pause that changes scope mid-episode is told: widening is pushed, narrowing
// is pushed, and the resume still comes once.
func TestPauseScopeChangeIsToldWhileTheEpisodeStands(t *testing.T) {
	rec, flush := installPush(t)
	at := scopeTextTrader(t, true)
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})
	fk := &intFake{clearsAtMs: ctTimeAt(t, "2026-11-19", "19:57").UnixMilli()}
	fk.blockedFor = func(openMin int) bool { return openMin == ntTrader.IntegritySwingOpenMin }
	armIntegrityGate(t, fk)

	at.monitorTick(now)
	fk.blockedFor = nil // the intraday window is blocked too: widen
	at.monitorTick(now.Add(time.Minute))
	fk.blockedFor = func(openMin int) bool { return openMin == ntTrader.IntegritySwingOpenMin }
	at.monitorTick(now.Add(2 * time.Minute)) // back to swing only: narrow
	fk.blockedFor = func(int) bool { return false }
	at.monitorTick(now.Add(3 * time.Minute)) // measured filled: resume
	flush()
	msgs := rec.all()
	if countContaining(msgs, "TRADING PAUSED") != 1 || countContaining(msgs, "PAUSE WIDENED") != 1 ||
		countContaining(msgs, "PAUSE NARROWED") != 1 || countContaining(msgs, "TRADING RESUMED") != 1 {
		t.Fatalf("want 1 paused / 1 widened / 1 narrowed / 1 resumed: %q", msgs)
	}
	for _, m := range msgs {
		if strings.Contains(m, "PAUSE WIDENED") && !strings.Contains(m, "NEW entries refused on every path") {
			t.Fatalf("a widening push must say every path: %q", m)
		}
		if strings.Contains(m, "PAUSE NARROWED") && !strings.Contains(m, "intraday mentor setups trade normally again") {
			t.Fatalf("a narrowing push must say intraday trades again: %q", m)
		}
	}
}
