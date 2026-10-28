package trader

import (
	"fmt"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── C52 W1 A2: a confluence upgrade moves leg 1's TP to 2R ─────────────────
// [D3.4 p3 @10:07–10:33, @11:01–11:05].

type tpModCall struct {
	signalID, side string
	wireLeg        int
	tp             float64
}

// recordModifyBracket swaps the modify_bracket wire for a recorder.
func recordModifyBracket(t *testing.T) *[]tpModCall {
	t.Helper()
	var calls []tpModCall
	old := modifyBracketWire
	modifyBracketWire = func(_ *ntTrader.TCPTrader, signalID, side string, wireLeg int, newTP float64) error {
		calls = append(calls, tpModCall{signalID, side, wireLeg, newTP})
		return nil
	}
	t.Cleanup(func() { modifyBracketWire = old })
	return &calls
}

// splitLivePos is a mode-B school-1 position: 2 contracts, leg 1 (wire 1) with
// its +1R TP resting, the runner (wire 2) with the trade target.
func splitLivePos(side, mode string, entry, r, leg1TP, target float64) *mentorLivePos {
	return &mentorLivePos{
		Pos: mentorPosition{Side: side, Mode: mode, Entry: entry, R: r, Origin: "ISB",
			Contracts: 2, Leg1: 1, Leg2: 1, Leg1TP: leg1TP, Target: target},
		Legs: [3]mentorLeg{
			{SignalID: "sig", Qty: 1, TP: leg1TP, Wire: 1},
			{SignalID: "sig", Qty: 1, TP: target, Final: true, Wire: 2},
		},
	}
}

var confluenceUpgradeIntent = func(side mentor.Side) mentor.Intent {
	return mentor.Intent{Action: mentor.ConfluenceUpgrade, Side: side,
		Reason: "trigger flipped to the trade's side [B20, D3.4 p3 @09:17]"}
}

// The production handler: an upgrade of a split mode-B long moves leg 1's TP
// from +1R (110) to +2R (120) through modify_bracket, leg-scoped to wire leg 1.
// MUTANT: drop the mentorLeg1TPToC call in mentorConfluenceUpgrade -> no call
// is recorded -> RED.
func TestMentorConfluenceUpgradeMovesLeg1TPToTwoR(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	calls := recordModifyBracket(t)
	at.mentorRegisterLivePos("sig", splitLivePos("long", "B", 100, 10, 110, 140))

	at.mentorConfluenceUpgrade(confluenceUpgradeIntent(mentor.SideLong))

	if len(*calls) != 1 {
		t.Fatalf("want exactly one modify_bracket, got %+v", *calls)
	}
	if c := (*calls)[0]; c.signalID != "sig" || c.side != "long" || c.wireLeg != 1 || c.tp != 120 {
		t.Fatalf("modify = %+v, want sig/long/wire 1/TP 120 (entry + 2R)", c)
	}
	if got := at.mentorLivePos["sig"].Pos.Mode; got != "C" {
		t.Fatalf("mode = %q, want C", got)
	}
	if got := at.mentorCountSnapshot()["confluence_tp_sent"]; got != 1 {
		t.Fatalf("confluence_tp_sent = %d, want 1", got)
	}
	// A second upgrade (already C) and the per-candle reconcile re-send nothing
	// while the first modify awaits its NT8 verdict.
	at.mentorConfluenceUpgrade(confluenceUpgradeIntent(mentor.SideLong))
	at.mentorExitDrivePos(nil, at.mentorLivePos["sig"], 105, 106, 104, 1000)
	if len(*calls) != 1 {
		t.Fatalf("a pending modify must not be re-sent, got %+v", *calls)
	}
}

func TestMentorConfluenceUpgradeShortMirror(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	calls := recordModifyBracket(t)
	at.mentorRegisterLivePos("sig", splitLivePos("short", "B", 100, 10, 90, 60))
	at.mentorConfluenceUpgrade(confluenceUpgradeIntent(mentor.SideShort))
	if len(*calls) != 1 || (*calls)[0].tp != 80 || (*calls)[0].side != "short" || (*calls)[0].wireLeg != 1 {
		t.Fatalf("short mirror: want one modify to 80 on wire leg 1, got %+v", *calls)
	}
}

// Nothing to move: leg 1 already gone, a single bracket, or a TP already at 2R.
func TestMentorConfluenceUpgradeLeg1TPNoOps(t *testing.T) {
	cases := []struct {
		name string
		lp   func() *mentorLivePos
	}{
		{"leg 1 already scaled out at 1R", func() *mentorLivePos {
			lp := splitLivePos("long", "B", 100, 10, 110, 140)
			lp.Pos.Scaled = true
			return lp
		}},
		{"a single bracket on the wire", func() *mentorLivePos {
			lp := splitLivePos("long", "B", 100, 10, 110, 140)
			lp.Legs[0].Wire = 0
			lp.Legs[1] = mentorLeg{}
			return lp
		}},
		{"leg 1 TP already at 2R", func() *mentorLivePos {
			return splitLivePos("long", "B", 100, 10, 120, 140)
		}},
		{"a 2R line past the runner's own TP", func() *mentorLivePos {
			return splitLivePos("long", "B", 100, 10, 110, 115)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
			calls := recordModifyBracket(t)
			at.mentorRegisterLivePos("sig", c.lp())
			at.mentorConfluenceUpgrade(confluenceUpgradeIntent(mentor.SideLong))
			if len(*calls) != 0 {
				t.Fatalf("nothing to move, got %+v", *calls)
			}
			if got := at.mentorLivePos["sig"].Pos.Mode; got != "C" {
				t.Fatalf("the branch still switches to C, got %q", got)
			}
		})
	}
}

// A mode-C ENTRY (leg 1 already at ~2R, a few ticks of slippage included) is
// never re-modified by the per-candle reconcile.
func TestMentorModeCEntryLeg1TPUntouched(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	calls := recordModifyBracket(t)
	// authored entry 100, filled 100.5 -> the wire TP stayed at 120 (1.95R)
	lp := splitLivePos("long", "C", 100.5, 10, 120, 140)
	at.mentorRegisterLivePos("sig", lp)
	at.mentorExitDrivePos(nil, lp, 105, 106, 104, 1000)
	if len(*calls) != 0 {
		t.Fatalf("a mode-C entry's 2R leg-1 TP must not be re-modified, got %+v", *calls)
	}
}

// A resting arm upgraded to C before it filled: the fill carries the entry
// frame's +1R leg-1 TP; the first drive pass on the now-C position moves it to
// 2R. MUTANT: remove the reconcile call from the mode-C case of
// mentorExitDrivePos -> no call is recorded -> RED.
func TestMentorModeCAfterFillReconcilesLeg1TP(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	calls := recordModifyBracket(t)
	lp := splitLivePos("long", "C", 100, 10, 110, 140) // C mode, but the wire TP is 1R
	at.mentorRegisterLivePos("sig", lp)
	at.mentorExitDrivePos(nil, lp, 105, 106, 104, 1000)
	if len(*calls) != 1 || (*calls)[0].tp != 120 || (*calls)[0].wireLeg != 1 {
		t.Fatalf("want one modify to 120 on wire leg 1, got %+v", *calls)
	}
}

// A refused send is retried per candle but only a bounded number of times.
func TestMentorConfluenceLeg1TPRetryIsBounded(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	n := 0
	old := modifyBracketWire
	modifyBracketWire = func(_ *ntTrader.TCPTrader, _, _ string, _ int, _ float64) error {
		n++
		return fmt.Errorf("boom")
	}
	t.Cleanup(func() { modifyBracketWire = old })
	lp := splitLivePos("long", "C", 100, 10, 110, 140)
	at.mentorRegisterLivePos("sig", lp)
	for i := 0; i < 10; i++ {
		at.mentorExitDrivePos(nil, lp, 105, 106, 104, int64(1000+i))
	}
	if n != mentorConfluenceTPMaxTries {
		t.Fatalf("sends = %d, want the bound %d", n, mentorConfluenceTPMaxTries)
	}
}

// End to end through the evaluator: the kernel's school-1 flip upgrade reaches
// the trader and moves leg 1's TP (the production emit -> handler path).
func TestMentorConfluenceUpgradeEndToEndMovesLeg1TP(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	calls := recordModifyBracket(t)
	at.mentorRegisterLivePos("sig-long", splitLivePos("long", "B", 100, 10, 110, 140))
	oldBars := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(_, _ string, _ int) []market.Kline { return nil }
	t.Cleanup(func() { market.FuturesBarsProvider = oldBars })

	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	e := mentor.New(cfg)
	base := time.Date(2026, 10, 2, 9, 0, 0, 0, kernel.CTLocation()).UnixMilli()
	e.State.SchoolOneSide = mentor.SideLong
	e.State.Trigger = mentor.TriggerLine{Dir: mentor.SideShort, Price: 95, LastBucket: base,
		LastBar: market.Kline{OpenTime: base, High: 100, Low: 94, Close: 95}}
	at.mentorEval = e
	bars := []market.Kline{
		{OpenTime: base + 4*60_000, CloseTime: base + 5*60_000 - 1, Open: 95, High: 99, Low: 94, Close: 96},
		{OpenTime: base + 5*60_000, CloseTime: base + 6*60_000 - 1, Open: 99, High: 101, Low: 98, Close: 100.5},
	}
	at.mentorEvalOnce(bars)

	if len(*calls) != 1 || (*calls)[0].tp != 120 || (*calls)[0].wireLeg != 1 {
		t.Fatalf("the kernel's confluence upgrade must move leg 1 to 2R (120) on wire leg 1, got %+v", *calls)
	}
}
