package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── W-EXEC-TRUTH W3 (e) D13 — the strict nudge at executeDecisionWithRecordAt ─
//
// Every case drives the decision path's production function on the fixture
// clock (executeDecisionWithRecord is its one-line wall-clock delegate): the
// admission chain refuses the market entry exactly as today, and only the
// leg-0 strict refusal of a decision citing a matched market_in_zone scenario
// runs ONE armed pass for that scenario.

func strictZoneRig(t *testing.T, id string, doc kernel.PlanDoc) *zoneRig {
	t.Helper()
	r := newZoneRig(t, id, doc)
	r.at.config.StrategyConfig.DayPlan.PlanMode = "strict"
	return r
}

func nudge(r *zoneRig, cited string, at time.Time) (*store.DecisionAction, error) {
	rec := &store.DecisionAction{Action: "open_long", Symbol: "MNQ"}
	err := r.at.executeDecisionWithRecordAt(&kernel.Decision{Action: "open_long", Symbol: "MNQ", CitedScenario: cited, StopLoss: 98, TakeProfit: 110}, rec, at)
	return rec, err
}

func strictRefusal() string {
	return StrictNonArmRefusalPrefix + ", and this is a decision-path market entry"
}

// cited + inside → the decision stays refused, ONE frame at the far bound,
// and the record carries "placed limit …"; a second nudge reads "already
// working".
func TestStrictNudgePlacesOnceThenReportsAlreadyWorking(t *testing.T) {
	r := strictZoneRig(t, "w3-nudge-place", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTape(100.0, r.now, 0))
	rec, err := nudge(r, "S1", r.now)
	if err != nil {
		t.Fatalf("executeDecisionWithRecordAt: %v", err)
	}
	if rec.Success {
		t.Fatal("the decision must stay refused — never a Path flip")
	}
	if !strings.HasPrefix(rec.Error, strictRefusal()+" · 🚦 placed limit 100.50 signal ") {
		t.Fatalf("record must carry the refusal + the executor verdict, got %q", rec.Error)
	}
	sigs, _ := r.drain()
	if len(sigs) != 1 || sigs[0].LimitPrice != 100.5 || sigs[0].OrderType != "limit" {
		t.Fatalf("the nudge must place exactly one limit at 100.50: %+v", sigs)
	}
	if !strings.HasSuffix(rec.Error, sigs[0].SignalID) {
		t.Fatalf("the verdict must name the signal it sent (%s): %q", sigs[0].SignalID, rec.Error)
	}
	rec2, _ := nudge(r, "S1", r.now.Add(time.Second))
	if want := strictRefusal() + " · 🚦 already working: limit 100.50 signal " + sigs[0].SignalID; rec2.Error != want {
		t.Fatalf("second nudge:\n got %q\nwant %q", rec2.Error, want)
	}
	if s2, _ := r.drain(); len(s2) != 0 {
		t.Fatalf("a second nudge must not place again: %+v", s2)
	}
}

// cited but the chained confirm is not met → nothing placed, the verdict says
// why.
func TestStrictNudgeCitedButUnconfirmedPlacesNothing(t *testing.T) {
	r := strictZoneRig(t, "w3-nudge-unconf", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, true)))
	r.setTape(zoneTape(105.0, r.now, 0)) // lows ≥ 100.55: the 100 touch never happened
	rec, _ := nudge(r, "S1", r.now)
	if !strings.HasPrefix(rec.Error, strictRefusal()+" · 🚦 waiting: confirm not met (") {
		t.Fatalf("an unconfirmed cited scenario must report 'waiting: confirm not met', got %q", rec.Error)
	}
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("an unconfirmed scenario must place nothing: %+v", sigs)
	}
}

// short of the zone → "waiting: price below zone …".
func TestStrictNudgeShortOfZoneWaits(t *testing.T) {
	r := strictZoneRig(t, "w3-nudge-short", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTapeReacted(99.0, r.now))
	rec, _ := nudge(r, "S1", r.now)
	if want := strictRefusal() + " · 🚦 waiting: price below zone 99.50–100.50 (last 99.00)"; rec.Error != want {
		t.Fatalf("\n got %q\nwant %q", rec.Error, want)
	}
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("short of the zone places nothing: %+v", sigs)
	}
}

// Off-plan, uncited, and a planned_order arm never nudge: the record is the
// refusal alone and NO pass runs (the price would otherwise place).
func TestStrictNudgeNeverFiresOffPlanUncitedOrPlannedOrder(t *testing.T) {
	for _, c := range []struct {
		name, cited, policy string
	}{
		{"off-plan", "off-plan", kernel.EntryPolicyMarketInZone},
		{"uncited", "", kernel.EntryPolicyMarketInZone},
		{"unknown id", "S7", kernel.EntryPolicyMarketInZone},
		{"planned_order", "S1", kernel.EntryPolicyPlannedOrder},
		{"legacy", "S1", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := strictZoneRig(t, "w3-nudge-no-"+strings.ReplaceAll(c.name, " ", "-"), zoneDoc(zoneScenario("S1", c.policy, zone, false)))
			r.setTape(zoneTape(100.0, r.now, 0))
			rec, _ := nudge(r, c.cited, r.now)
			if strings.Contains(rec.Error, "🚦") || rec.Error == "" || rec.Success {
				t.Fatalf("%s must be refused without a nudge, got success=%v %q", c.name, rec.Success, rec.Error)
			}
			if sigs, _ := r.drain(); len(sigs) != 0 {
				t.Fatalf("%s: no armed pass may run: %+v", c.name, sigs)
			}
		})
	}
}

// Any refusal that is not the leg-0 strict refusal never nudges: the hold,
// the feed-down gate and the consecutive-loss breaker.
func TestStrictNudgeNeverFiresOnOtherGateRefusals(t *testing.T) {
	t.Run("maintenance hold", func(t *testing.T) {
		dir := withMaintenanceDir(t)
		r := strictZoneRig(t, "w3-nudge-hold", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
		r.setTape(zoneTape(100.0, r.now, 0))
		setHold(t, dir, "job-nudge")
		rec, _ := nudge(r, "S1", r.now)
		if !strings.HasPrefix(rec.Error, "maintenance_hold") || strings.Contains(rec.Error, "🚦") {
			t.Fatalf("hold: %q", rec.Error)
		}
		if sigs, _ := r.drain(); len(sigs) != 0 {
			t.Fatalf("hold: nothing sent: %+v", sigs)
		}
	})
	t.Run("feed down", func(t *testing.T) {
		r := strictZoneRig(t, "w3-nudge-feed", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
		r.setTape(zoneTape(100.0, r.now, 0))
		if err := ntwire.WriteFrame(r.conn, ntwire.FrameFeedStatus, ntwire.FeedStatusPayload{PriceStatus: "ConnectionLost", Time: time.Now().UTC().Format(time.RFC3339)}); err != nil {
			t.Fatal(err)
		}
		nt := r.at.armedTrader()
		parityWaitFor(t, "feed_status consumed", func() bool { return !nt.IsFeedConnected() })
		rec, _ := nudge(r, "S1", r.now)
		if !strings.HasPrefix(rec.Error, "feed_down") || strings.Contains(rec.Error, "🚦") {
			t.Fatalf("feed down: %q", rec.Error)
		}
		if sigs, _ := r.drain(); len(sigs) != 0 {
			t.Fatalf("feed down: nothing sent: %+v", sigs)
		}
	})
	t.Run("consecutive-loss breaker", func(t *testing.T) {
		r := strictZoneRig(t, "w3-nudge-breaker", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
		r.setTape(zoneTape(100.0, r.now, 0))
		r.at.config.StrategyConfig.RiskControl.ConsecutiveLossHalt = store.IntPtr(1)
		exit := r.now.Add(-10 * time.Minute)
		neg := -52.0
		if err := r.st.GormDB().Create(&store.TraderPosition{TraderID: r.at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG", Quantity: 1,
			EntryPrice: 100, ExitPrice: 99, RealizedPnL: neg, PnlCorrected: &neg, Status: "CLOSED", CloseReason: "sync",
			EntryTime: exit.Add(-5 * time.Minute).UnixMilli(), ExitTime: exit.UnixMilli()}).Error; err != nil {
			t.Fatal(err)
		}
		rec, _ := nudge(r, "S1", r.now)
		if !strings.HasPrefix(rec.Error, "consecutive_loss_halt") || strings.Contains(rec.Error, "🚦") {
			t.Fatalf("breaker: %q", rec.Error)
		}
		if sigs, _ := r.drain(); len(sigs) != 0 {
			t.Fatalf("breaker: nothing sent: %+v", sigs)
		}
	})
}

// The nudge's pass is SCOPED: another scenario's armed row is skipped before
// armAdmitted, so no arm_not_admitted is counted for it and it is not placed.
func TestStrictNudgeIsScopedToTheCitedScenario(t *testing.T) {
	dir := withMaintenanceDir(t)
	r := strictZoneRig(t, "w3-nudge-scope", zoneDoc(
		zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false),
		zoneScenario("S2", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTape(100.0, r.now, 0))
	// A scan pass under the hold authors BOTH rows and places neither.
	setHold(t, dir, "job-scope")
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	if err := store.ClearMaintenanceHold(dir, "job-scope"); err != nil {
		t.Fatal(err)
	}
	if s, _ := r.drain(); len(s) != 0 {
		t.Fatalf("fixture: the held pass must not send: %+v", s)
	}
	before := gateBlocks(r.at.id, "arm_not_admitted")
	rec, _ := nudge(r, "S2", r.now.Add(time.Second))
	if !strings.Contains(rec.Error, "🚦 placed limit 100.50 signal ") {
		t.Fatalf("the cited S2 must place: %q", rec.Error)
	}
	sigs, _ := r.drain()
	if len(sigs) != 1 {
		t.Fatalf("exactly one frame (S2): %+v", sigs)
	}
	if got := r.row("S2"); got.SignalID != sigs[0].SignalID {
		t.Fatalf("the placed row must be S2's: %+v", got)
	}
	if gateBlocks(r.at.id, "arm_not_admitted") != before {
		t.Fatal("a scoped pass must not count arm_not_admitted for a scenario it did not author")
	}
}
