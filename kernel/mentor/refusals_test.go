package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestEveryRefusalNamesReason — B-rules (CTO 13:51:31Z): every evaluator filter
// that DROPS an intent carries a named reason + counter, like the replay
// funnel stages, so DS-105 can diff refusals against the replay.
func TestEveryRefusalNamesReason(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true

	entry := func(side Side, price float64) Intent {
		return Intent{Action: PlaceStopEntry, Side: side, Price: price, Reason: "test"}
	}

	// ORB stages (post-open: 09:00 CT, so the pre-market exemption is off).
	postOpen := auditMs(2026, 9, 15, 9, 0, 0)
	orb := ORB{Day: 0, High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	_, r := orbGateFilter([]Intent{entry(SideShort, 92)}, orb, postOpen, cfg)
	if len(r) != 1 || r[0] != "orb_wrong_side" {
		t.Fatalf("wrong-side ORB refusal = %v", r)
	}
	_, r = orbGateFilter([]Intent{entry(SideLong, 88)}, orb, postOpen, cfg)
	if len(r) != 1 || r[0] != "orb_inside" {
		t.Fatalf("inside ORB refusal = %v", r)
	}
	_, r = orbGateFilter([]Intent{entry(SideLong, 92)}, ORB{Day: 0, Drawn: false}, postOpen, cfg)
	if len(r) != 1 || r[0] != "orb_not_drawn" {
		t.Fatalf("not-drawn ORB refusal = %v", r)
	}
	_, r = orbGateFilter([]Intent{entry(SideLong, 92)}, ORB{Day: 0, High: 90, Low: 85, Drawn: true}, postOpen, cfg)
	if len(r) != 1 || r[0] != "orb_not_escaped" {
		t.Fatalf("not-escaped ORB refusal = %v", r)
	}

	// Box ban.
	boxes := []Box{{Kind: FTGL, Top: 96, Bottom: 94}}
	_, r = boxBanFilter([]Intent{entry(SideLong, 95)}, boxes, market.Kline{Close: 95})
	if len(r) != 1 || r[0] != "inside_any_box" {
		t.Fatalf("box-ban refusal = %v", r)
	}

	// Fail-closed.
	_, r = failClosedFilter([]Intent{entry(SideLong, 92)})
	if len(r) != 1 || r[0] != "seed_missing_source" {
		t.Fatalf("fail-closed refusal = %v", r)
	}

	// G1 leg budget: a filled leg refuses the second PHL.
	l := &Limits{Long: &Leg{Side: SideLong, Extreme: 120, Entries: 1}}
	out := l.Apply([]Intent{entry(SideLong, 100)}, market.Kline{}, market.Kline{}, 0,
		[]Level{{Key: "k", Kind: KindKeyLevel, Price: 95}}, cfg)
	if len(out) != 0 || l.Refusals["leg_budget_second_phl"] != 1 {
		t.Fatalf("second-PHL refusal = %v (out %d)", l.Refusals, len(out))
	}

	// G2 loss box: a blocked place refuses.
	l2 := &Limits{}
	l2.Places = map[string]*Place{"k": {Blocked: true}}
	in := entry(SideLong, 100)
	in.AnchorKey = "k" // the setup's place, as the emit site stamps it
	out = l2.Apply([]Intent{in}, market.Kline{}, market.Kline{}, 0,
		[]Level{{Key: "k", Kind: KindKeyLevel, Price: 95}}, cfg)
	if len(out) != 0 || l2.Refusals["loss_box_blocked"] != 1 {
		t.Fatalf("loss-box refusal = %v (out %d)", l2.Refusals, len(out))
	}

	// K2: an old extreme with no coincident key level is not a location.
	l3 := &Limits{}
	out = l3.Apply([]Intent{{Action: PlaceStopEntry, Side: SideLong, Price: 100,
		AnchorKey: string(KindOldExtreme) + ":104", Anchor: 104}}, market.Kline{}, market.Kline{}, 0,
		[]Level{{Key: "k", Kind: KindKeyLevel, Price: 95}}, cfg)
	if len(out) != 0 || l3.Refusals["orphan_not_location"] != 1 {
		t.Fatalf("orphan refusal = %v (out %d)", l3.Refusals, len(out))
	}
}

// TestRefusalLedgerInTick — the Tick call site records refusals on the state:
// a seeded-missing evaluator refuses every entry and the ledger names it.
func TestRefusalLedgerInTick(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	Seed(e, nil, 0) // every source missing → fail-closed

	bars := []market.Kline{
		{Open: 121, High: 121.5, Low: 120.5, Close: 120, OpenTime: auditMs(2026, 9, 15, 9, 0, 0), CloseTime: auditMs(2026, 9, 15, 9, 0, 0) + 59_999},
		{Open: 98, High: 106, Low: 97, Close: 105, OpenTime: auditMs(2026, 9, 15, 9, 1, 0), CloseTime: auditMs(2026, 9, 15, 9, 1, 0) + 59_999},
		{Open: 103, High: 104, Low: 99, Close: 100, OpenTime: auditMs(2026, 9, 15, 9, 2, 0), CloseTime: auditMs(2026, 9, 15, 9, 2, 0) + 59_999},
	}
	now := bars[2].CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.Tick(bars, now)
	if e.State.Refusals["seed_missing_source"] == 0 {
		t.Fatalf("Tick must record the fail-closed refusal; ledger = %v", e.State.Refusals)
	}
}
