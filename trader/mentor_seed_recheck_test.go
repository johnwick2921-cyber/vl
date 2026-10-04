package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

// The live MNQ 12-26 store had 94 of the 102 closed 4h candles at boot. The
// trader seeds ONCE (mentorSeedAtStart) and the depth snapshot behind
// mentorSourcesMissing froze at 94, so the placement gate refused every entry
// until a restart even after the evaluator had warmed. This pin runs the
// production sequence — seed from the store, then mentorEvalOnce per tick —
// and requires the placement gate to clear by itself.
func TestMentorSeedDepthRecheckClearsPlacementGateWithoutRestart(t *testing.T) {
	st := mentorSeedStore(t)
	now := time.Now().UnixMilli()

	// the history length (hours) that seeds exactly 94 closed 4h candles
	var rows []store.BarHistoryDB
	for h := int64(360); h <= 400; h++ {
		cand := mentorSeedBars1mHours(now, h)
		bars := storeBarsToKlines(cand, 60_000)
		if d := mentor.SeedDepths(bars, mentorAgg1H(bars), now)["4h EMA34"]; d == 94 {
			rows = cand
			break
		}
	}
	if rows == nil {
		t.Fatal("no fixture length seeds exactly 94 closed 4h candles")
	}
	if err := store.NewBarHistoryStore(st.GormDB()).InsertBars(rows); err != nil {
		t.Fatalf("InsertBars: %v", err)
	}

	at := &AutoTrader{
		id: "t-seed-recheck",
		config: AutoTraderConfig{
			StrategyConfig: &store.StrategyConfig{
				RiskControl: store.RiskControlConfig{MentorMode: true},
			},
		},
		store: st,
	}
	wireMentorPlacementSeams(t)
	mentorSourceDepthSource = nil // the real seam, wired by the seed below
	t.Cleanup(func() {
		mentorSourceDepthSource = nil
		mentorSeedDepths = nil
	})
	at.mentorSeedAtStart()

	short := func() bool {
		return strings.Contains(strings.Join(at.mentorSourcesMissing(), ", "), "history: 4h EMA34 (")
	}
	if !short() || len(at.mentorEval.SourcesMissing()) == 0 {
		t.Fatalf("seeded at 94/102: want both gates refusing; trader %v, evaluator %v",
			at.mentorSourcesMissing(), at.mentorEval.SourcesMissing())
	}

	// Live ticks: the stored tail plus synthetic closed 1m bars for ~40h.
	tail := storeBarsToKlines(rows, 60_000)
	for i := range tail {
		tail[i].CloseTime = tail[i].OpenTime + 59_999
	}
	b0 := now - ((now - 22*3600_000) % (4 * 3600_000)) // current 4h bucket start
	feed := append([]market.Kline(nil), tail[max(0, len(tail)-2*24*60):]...)
	px := feed[len(feed)-1].Close
	cleared := int64(0)
	for ms := b0 - 3600_000; ms < b0+40*3600_000 && cleared == 0; ms += 60_000 {
		if ms <= feed[len(feed)-1].OpenTime {
			continue
		}
		px += 0.05
		feed = append(feed, market.Kline{OpenTime: ms, CloseTime: ms + 59_999, Open: px - 0.05, High: px + 0.5, Low: px - 0.5, Close: px, Volume: 1})
		if (ms/60_000)%5 != 0 {
			continue
		}
		win := feed
		if len(win) > 2*24*60 {
			win = win[len(win)-2*24*60:]
		}
		at.mentorEvalOnce(win)
		if !short() && len(at.mentorEval.SourcesMissing()) == 0 {
			cleared = ms
		}
	}
	if cleared == 0 {
		t.Fatalf("the placement gate never cleared: trader %v, evaluator %v, depths %v",
			at.mentorSourcesMissing(), at.mentorEval.SourcesMissing(), at.mentorEval.Depths())
	}
	if d, ok := mentorSourceDepth("4h EMA34"); !ok || d < 102 {
		t.Fatalf("4h depth seam after the clear = %d/%v, want >= 102", d, ok)
	}
	if l := at.mentorEval.TakeDepthMet(); l != "" {
		t.Fatalf("the depth-met line was not consumed (and logged) by the trader: %q", l)
	}
}
