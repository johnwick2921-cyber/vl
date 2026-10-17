package trader

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// W-EXEC-TRUTH W0 (defect 6) — A STOP ENTRY THAT NEVER REACHED THE WIRE DOES
// NOT COMMIT THE PLAN.
//
// runArmedPlacementAt latched placedThisPass and ran cancelOtherArmsInPlan on
// EVERY stop-entry outcome except a hold refusal. A guard cancel ("never
// placed"), an un-adjudicated verdict, a refused slot and an AddOn too old to
// build the order all returned the same value as a real send, so the plan's
// other arms were cancelled "one_live_entry: <S> placed" for an order that
// never existed. Live: rows 169 and 176 were cancelled that way 0.3 ms after
// the "placed" row itself was cancelled "never placed" (2026-09-21 00:30:59,
// 2026-09-22 05:50:10 UTC).
//
// The latch exists for a SEND (class 81: a send is not a settlement, so an
// ambiguous send stays pessimistic). An order provably never sent commits
// nothing.
func TestUnsentStopEntryNeverCancelsTheSiblingArm(t *testing.T) {
	for _, tc := range []struct {
		name          string
		build         string  // "" = no current far-side proof (ErrFarSideNotProven, FIX-P1A; ErrAddonBuildTooOld is only for a REPORTED old build)
		price         float64 // 29599 rests below the long trigger; 29700 = already through it (guard cancel); 0 = no price (not adjudicated)
		wantSent      bool
		wantSiblingIn string // the sibling's state after the pass
	}{
		{name: "addon_too_old", build: "", price: 29599, wantSent: false, wantSiblingIn: "armed"},
		{name: "guard_through_cancel", build: ntwire.MinAddonBuildStopSlot, price: 29700, wantSent: false, wantSiblingIn: "armed"},
		{name: "not_adjudicated", build: ntwire.MinAddonBuildStopSlot, price: 0, wantSent: false, wantSiblingIn: "armed"},
		// Positive control: a real send still commits the plan (one entry per plan).
		{name: "sent_commits", build: ntwire.MinAddonBuildStopSlot, price: 29599, wantSent: true, wantSiblingIn: "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("STOP_ENTRY_SEAM", "on")
			withMaintenanceDir(t)
			at, st := resetTrader(t, store.StrategyConfig{})
			s := ntwire.NewTCPServer(nil)
			s.SetAddrForTest("127.0.0.1:0")
			s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := s.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = s.Stop() }()
			conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
			if err != nil {
				t.Fatal(err)
			}
			waitAddonRegistered(t, s) // CTO M7: the producer must not race the accept
			defer conn.Close()
			sent := make(chan ntwire.FrameType, 16)
			go func() {
				for {
					env, err := ntwire.ReadFrame(conn)
					if err != nil {
						return
					}
					sent <- env.Type
				}
			}()
			if tc.build != "" {
				if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: tc.build}); err != nil {
					t.Fatal(err)
				}
				for deadline := time.Now().Add(2 * time.Second); !ntwire.FarSideProven(s.FarSideBuildID(), tc.build) && time.Now().Before(deadline); {
					time.Sleep(time.Millisecond)
				}
				if !ntwire.FarSideProven(s.FarSideBuildID(), tc.build) {
					t.Fatal("fixture: far-side build never proven")
				}
			}
			broker := nttrader.NewTCPTrader(s, "MNQ", "Sim101")
			// W117 F4 — the AddOn emits a positions frame on connect; seed the
			// known-flat book so the entry path reads empty, not unreadable.
			s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
			broker.StartCloseSync(at.id, "fixture", "ninjatrader", st)
			at.trader = broker
			at.config.NinjaTraderSymbol = "MNQ"
			// class 110: pinned to RTH — the admission chain reads THIS clock.
			now := rthInstant()
			s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
			ledger := st.ArmedOrders()
			stop := store.ArmedOrderDB{TraderID: at.id, PlanID: "latch", Scenario: "S1", Version: 1, State: "armed",
				Side: "long", EntryPx: 29600, StopPx: 29590, TargetPx: 29630, Kind: "stop_entry", Condition: "reclaim"}
			sibling := store.ArmedOrderDB{TraderID: at.id, PlanID: "latch", Scenario: "S2", Version: 1, State: "armed",
				Side: "long", EntryPx: 29000, StopPx: 28990, TargetPx: 29030, Condition: "reject"}
			for _, r := range []*store.ArmedOrderDB{&stop, &sibling} {
				if err := ledger.UpsertArm(r); err != nil {
					t.Fatal(err)
				}
			}

			at.runArmedPlacementAt([]market.Kline{{Close: tc.price}}, now.Add(-time.Hour).UnixMilli(), now,
				armAdmission{armAdmitKey("latch", "S1", 0): true, armAdmitKey("latch", "S2", 0): true}) // the authoring pass admitted both (G1)

			gotSignal := false
			deadline := time.After(300 * time.Millisecond)
		drain:
			for {
				select {
				case f := <-sent:
					if f == ntwire.FrameSignal {
						gotSignal = true
					}
				case <-deadline:
					break drain
				}
			}
			if gotSignal != tc.wantSent {
				t.Fatalf("signal on the wire = %v, want %v", gotSignal, tc.wantSent)
			}
			var sib store.ArmedOrderDB
			if err := ledger.DB().First(&sib, sibling.ID).Error; err != nil {
				t.Fatal(err)
			}
			if sib.State != tc.wantSiblingIn {
				t.Fatalf("sibling S2 state = %q (reason %q), want %q", sib.State, sib.StateReason, tc.wantSiblingIn)
			}
			if !tc.wantSent && strings.Contains(sib.StateReason, "one_live_entry") {
				t.Fatalf("sibling cancelled %q for a stop entry that never reached the wire", sib.StateReason)
			}
		})
	}
}

// stampThenFailPlacer stamps the ledger (the send is under way) and then fails
// — the frame's fate is unknown, which is exactly the case the latch stays
// pessimistic for.
type stampThenFailPlacer struct{ calls int }

func (p *stampThenFailPlacer) PlaceStopEntry(symbol, side string, quantity float64, stopPx, sl, tp float64, beforeSend ...func(string) error) (string, error) {
	p.calls++
	for _, register := range beforeSend {
		if err := register("sid-ambiguous"); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("send stop-entry signal: write tcp: broken pipe")
}

// The outcome placeOneStopEntry reports is exactly "did a send start":
// refused BEFORE the ledger stamp is NOT_SENT, failed AFTER it is COMMITTED
// (class 81 pessimism), a real send is COMMITTED.
func TestPlaceOneStopEntryOutcomeFollowsTheLedgerStamp(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{})
	at.id = "stop-outcome"
	r := store.ArmedOrderDB{ID: 7, TraderID: at.id, PlanID: "2026-09-23:NY", Version: 1, Session: "NY", Scenario: "S1", Side: "LONG", EntryPx: 100, StopPx: 95, TargetPx: 110, Kind: "stop_entry"}
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	if d.Action != stopEntryPlace {
		t.Fatalf("fixture must be an otherwise-placeable arm, got %q", d.Action)
	}
	if got := at.placeOneStopEntry(&fakePlacer{err: fmt.Errorf("refused before the stamp")}, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceNotSent {
		t.Fatalf("a refusal before the ledger stamp must be NOT_SENT, got %d", got)
	}
	if got := at.placeOneStopEntry(&stampThenFailPlacer{}, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceCommitted {
		t.Fatalf("a failure after the ledger stamp must be COMMITTED (ambiguous send), got %d", got)
	}
	if got := at.placeOneStopEntry(&fakePlacer{}, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceCommitted {
		t.Fatalf("a real send must be COMMITTED, got %d", got)
	}
	cancelled := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 200) // already through the trigger
	if cancelled.Action != stopEntryCancel {
		t.Fatalf("fixture must be a guard cancel, got %q", cancelled.Action)
	}
	if got := at.placeOneStopEntry(&fakePlacer{}, &fakeLedger{}, r, cancelled, 200, rthInstant(), freeSlot()); got != stopPlaceNotSent {
		t.Fatalf("a guard cancel is never sent, got %d", got)
	}
}
