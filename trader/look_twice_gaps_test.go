package trader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"vl/logger"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
	"vl/trader/types"
)

// ── LOOK TWICE, the mutants no test saw (full test of 9acafc81f) ─────────────
//
// One case per rule that could be taken out of 9acafc81f without any test turning
// red, named by its id in fulltest-71/C1-table.log. They drive the rule itself (the
// gate, one pass, one send) instead of a replay: the replays run in real time.

func gapWire(t *testing.T, id string) *lookWire {
	t.Helper()
	w := newLookWire(t, "gap-"+id, ctInstant(t, "2026-11-18 10:00:05.000"))
	w.price(27740)
	return w
}

// gate is one pass of the second-look gate on the newest book. The hold is proven
// (its link check is answered) when prep says so.
func (w *lookWire) gate(v protectionVerdict, qty int, avg float64, prep func(*protectionHold)) (p *protectionProof, why string) {
	return w.gateOn(v, qty, avg, time.Time{}, prep)
}

func (w *lookWire) gateOn(v protectionVerdict, qty int, avg float64, bookAt time.Time, prep func(*protectionHold)) (p *protectionProof, why string) {
	w.protState(func() {
		if bookAt.IsZero() {
			_, bookAt, _ = w.s.OrderSnapshots().LatestReceived("Sim101")
		}
		start := w.at.beginProtectionLookLocked(w.s, 3*time.Second)
		if h := w.at.protHolds["MNQ|SHORT"]; h != nil && prep != nil {
			prep(h)
		}
		p = w.at.protectionMayActLocked(v, "MNQ|SHORT", "MNQ", "SHORT", qty, avg, w.s, "Sim101", bookAt, false, start, 3*time.Second, "t")
		if h := w.at.protHolds["MNQ|SHORT"]; h != nil {
			why = h.lastWhy
		}
	})
	return p, why
}

func ackedLook(h *protectionHold) { h.proven = true }

var gapPlace = protectionVerdict{Action: protectionPlace, StopPx: stop1193, Source: "t", Why: "naked."}

// I06: another QUANTITY is another position, even when no fill explains it.
func TestLookTwiceGap_QuantityChangeIsAFirstLook(t *testing.T) {
	w := gapWire(t, "qty")
	w.positions(short(3, entry1193))
	w.book("periodic")
	w.sync()
	w.gate(gapPlace, 3, entry1193, nil)
	w.jump(3100 * time.Millisecond)
	w.positions(short(1, entry1193))
	w.sync()
	if p, why := w.gate(gapPlace, 1, entry1193, ackedLook); p != nil || !strings.Contains(why, "first look") {
		t.Fatalf("Short 3, then Short 1: the look at 1 is its first one, got proof %v why %q", p != nil, why)
	}
	w.jump(3100 * time.Millisecond)
	if p, why := w.gate(gapPlace, 1, entry1193, ackedLook); p == nil {
		t.Fatalf("control: the second look at 1 acts, got %q", why)
	}
}

// I10 (with D20, its twin in LastExecution): an EXIT fill after the first look, more
// than 3 s old by the second one, and a frame after it that shows the same size.
func TestLookTwiceGap_ExitFillSinceTheFirstLookIsAFirstLook(t *testing.T) {
	w := gapWire(t, "exitfill")
	w.positions(short(1, entry1193))
	w.book("periodic")
	w.sync()
	w.gate(gapPlace, 1, entry1193, nil)
	w.jump(time.Second)
	w.orderUpdate("old-tp", "filled", 27700, 1)
	w.positions(short(1, entry1193))
	w.sync()
	w.jump(3100 * time.Millisecond)
	if p, why := w.gate(gapPlace, 1, entry1193, ackedLook); p != nil || !strings.Contains(why, "first look") {
		t.Fatalf("an exit filled since the first look: want a first look, got proof %v why %q", p != nil, why)
	}
}

// I23: rule 6 (no second stop on an old book) is about a STOP. It must not hold a
// FLATTEN: with the price through it, a stop that was refused protects nothing.
func TestLookTwiceGap_ASentStopDoesNotHoldTheFlatten(t *testing.T) {
	w := gapWire(t, "r6flat")
	w.positions(short(1, entry1193))
	w.book("periodic")
	w.sync()
	if err := w.s.SendPlaceProtectiveStop(ntwire.PlaceProtectiveStopPayload{Symbol: "MNQ", SignalID: "p", PositionSide: "SHORT", Quantity: 1, StopPrice: stop1193, Account: "Sim101"}); err != nil {
		t.Fatal(err)
	}
	flat := protectionVerdict{Action: protectionFlatten, StopPx: stop1193, Why: "through."}
	w.gate(flat, 1, entry1193, nil)
	w.jump(3100 * time.Millisecond)
	if p, why := w.gate(gapPlace, 1, entry1193, ackedLook); p != nil || !strings.Contains(why, "no order book has arrived since") {
		t.Fatalf("control: a second STOP waits for a newer book, got proof %v why %q", p != nil, why)
	}
	if p, why := w.gate(flat, 1, entry1193, ackedLook); p == nil {
		t.Fatalf("the FLATTEN must not wait for a newer book: %q", why)
	}
}

// I24: a pass that is not settled drops the link check that is out. Its answer says
// nothing about the settled look that comes later.
func TestLookTwiceGap_UnsettledDropsTheLinkCheck(t *testing.T) {
	w := gapWire(t, "unsettled")
	w.noAcks()
	w.positions(short(1, entry1193))
	w.book("periodic")
	w.sync()
	w.gate(gapPlace, 1, entry1193, nil)
	w.jump(3100 * time.Millisecond)
	w.gate(gapPlace, 1, entry1193, nil)
	if w.at.outstandingProtectionProbe() == nil {
		t.Fatal("fixture: the settled look must send the link check")
	}
	_, bookAt, _ := w.s.OrderSnapshots().LatestReceived("Sim101")
	if p, why := w.gateOn(gapPlace, 1, entry1193, bookAt.Add(-time.Second), nil); p != nil || !strings.Contains(why, "newer order book") {
		t.Fatalf("fixture: want an unsettled look, got %q", why)
	}
	if w.at.outstandingProtectionProbe() != nil {
		t.Fatal("the link check of the settled look before this one is still waited for")
	}
}

// J05 and I26: a ledger that cannot be read holds, tells the owner at once, and uses
// up the link check's answer.
func TestLookTwiceGap_UnreadableLedgerHolds(t *testing.T) {
	w := gapWire(t, "ledger")
	var deny atomic.Bool
	if err := w.st.GormDB().Callback().Query().Before("gorm:query").Register("gap:deny", func(tx *gorm.DB) {
		if deny.Load() && tx.Statement.Table == "trader_positions" {
			_ = tx.AddError(errors.New("database is locked"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	w.positions(short(1, entry1193))
	w.book("periodic")
	w.sync()
	w.gate(gapPlace, 1, entry1193, nil)
	w.jump(3100 * time.Millisecond)
	deny.Store(true)
	p, why := w.gate(gapPlace, 1, entry1193, ackedLook)
	deny.Store(false)
	if p != nil || !strings.Contains(why, "could not be read") {
		t.Fatalf("J05: our own close cannot be ruled out, so nothing may be sent: proof %v why %q", p != nil, why)
	}
	if ps := w.p0s(); len(ps) != 1 || ps[0].Kind != alertKindProtectionHeld {
		t.Fatalf("J05: the owner must hear it at once, got %+v", ps)
	}
	w.protState(func() {
		if w.at.protHolds["MNQ|SHORT"].proven {
			t.Error("I26: the answer of the link check speaks for one pass")
		}
	})
}

// G13: an answer read by a pass that then broke off (a contained panic) is not the
// next pass's.
func TestLookTwiceGap_AnAnswerIsOnePasses(t *testing.T) {
	at := &AutoTrader{}
	h := &protectionHold{proven: true}
	at.protHolds = map[string]*protectionHold{"k": h}
	at.beginProtectionLookLocked(ntwire.NewTCPServer(nil), 3*time.Second)
	if h.proven {
		t.Fatal("a new pass begins without an answer")
	}
}

// I37: no link to check = hold.
func TestLookTwiceGap_LinkCheckThatCannotBeSentHolds(t *testing.T) {
	at, h := &AutoTrader{}, &protectionHold{}
	if why := at.protectionProbeLocked(h, ntwire.NewTCPServer(nil)); !strings.Contains(why, "could not be checked") || h.probe != nil {
		t.Fatalf("why %q probe %v", why, h.probe)
	}
}

// G25: two rows at one average keep their holds when they swap places.
func TestLookTwiceGap_HoldKeysRankByQuantity(t *testing.T) {
	row := func(qty float64) map[string]interface{} {
		return map[string]interface{}{"symbol": "MNQ", "side": "SHORT", "quantity": qty, "entryPrice": 100.0}
	}
	if got := protectionHoldKeys([]map[string]interface{}{row(1), row(2)}); got[0] != "MNQ|SHORT" || got[1] != "MNQ|SHORT#2" {
		t.Fatalf("1, 2: %v", got)
	}
	if got := protectionHoldKeys([]map[string]interface{}{row(2), row(1)}); got[0] != "MNQ|SHORT#2" || got[1] != "MNQ|SHORT" {
		t.Fatalf("2, 1: %v", got)
	}
}

// K16: no average is never two equal looks.
func TestLookTwiceGap_NoAverageNeverReachesASecondLook(t *testing.T) {
	avg := positionAvg(map[string]interface{}{"symbol": "MNQ"})
	if !math.IsNaN(avg) {
		t.Fatalf("avg = %v", avg)
	}
	w := gapWire(t, "noavg")
	w.positions(short(1, 0)) // what a zero would match
	w.book("periodic")
	w.sync()
	for i := 0; i < 3; i++ {
		if p, why := w.gate(gapPlace, 1, avg, ackedLook); p != nil || !strings.Contains(why, "first look") {
			t.Fatalf("look %d: proof %v why %q", i, p != nil, why)
		}
		w.jump(3100 * time.Millisecond)
	}
}

// J04, J07: rule 5's two ends.
func TestLookTwiceGap_OwnDeath(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 47, 30, 0, time.UTC)
	dead := func(ago time.Duration) store.OwnDeath {
		return store.OwnDeath{Last: &store.TraderPosition{ID: 7, ExitTime: now.Add(-ago).UnixMilli()}, ReceiptQty: 1, EntryQty: 1}
	}
	h := &protectionHold{since: now.Add(-5 * time.Second)}
	var entered, reentered ntwire.ProtectionEvidence
	entered.EntryOpened = now.Add(-time.Hour)
	reentered.EntryOpened, reentered.Exit = now.Add(-time.Second), now.Add(-2*time.Second)
	for name, c := range map[string]struct {
		d    store.OwnDeath
		ev   ntwire.ProtectionEvidence
		veto bool
	}{
		"J04 its exit frames are lost, only the old entry is on record": {dead(2 * time.Second), entered, true},
		"a newer entry after the exit":                                  {dead(2 * time.Second), reentered, false},
		"J07 59 s ago":                                                  {dead(59 * time.Second), entered, true},
		"J07 61 s ago":                                                  {dead(61 * time.Second), entered, false},
		"no row":                                                        {store.OwnDeath{}, entered, false},
	} {
		if why, veto := protectionOwnDeath(c.d, nil, c.ev, now, h); veto != c.veto || (why != "") != c.veto {
			t.Errorf("%s: veto %v (%q), want %v", name, veto, why, c.veto)
		}
	}
	if _, veto := protectionOwnDeath(store.OwnDeath{}, errors.New("locked"), entered, now, h); !veto {
		t.Error("J05: an unreadable ledger holds")
	}
}

// ── the re-look goroutine ────────────────────────────────────────────────────

// relookTrader is a running trader with one open hold and no wire: its re-look has
// nothing to look at.
func relookTrader(t *testing.T) *AutoTrader {
	t.Helper()
	at := &AutoTrader{id: "gap-relook"}
	at.setupStopMonitorForTest()
	at.isRunning = true
	at.protHolds = map[string]*protectionHold{"MNQ|SHORT": {}}
	t.Cleanup(func() { at.cancelStopMonitor(); at.monitorWg.Wait() })
	return at
}

func (at *AutoTrader) armForGap() (run context.Context, wake chan struct{}) {
	at.protMu.Lock()
	defer at.protMu.Unlock()
	at.armProtectionRelookLocked()
	return at.protRelookRun, at.protRelookWake
}

func ends(t *testing.T, within time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatal(what)
	}
}

func TestLookTwiceGap_RelookStartsOnlyForARunningTrader(t *testing.T) {
	t.Setenv(lookTwiceEnv, "30000")
	at := relookTrader(t)
	at.isRunning = false
	if run, _ := at.armForGap(); run != nil {
		t.Fatal("K05: a stopped trader started a re-look (Stop is past its wait)")
	}
	at.isRunning = true
	at.cancelStopMonitor()
	if run, _ := at.armForGap(); run != nil {
		t.Fatal("K02: the Run has ended")
	}
}

func TestLookTwiceGap_OneRelookPerRun(t *testing.T) {
	t.Setenv(lookTwiceEnv, "30000")
	at := relookTrader(t)
	run1, wake1 := at.armForGap()
	run2, wake2 := at.armForGap()
	if run1 == nil || run1 != run2 || wake1 != wake2 {
		t.Fatal("K03: the second beat started a second goroutine")
	}
	// K14: … and the Run's end ends its 30 s wait.
	at.cancelStopMonitor()
	ends(t, 3*time.Second, "K14: Stop must not wait out the re-look's timer", at.monitorWg.Wait)
}

func TestLookTwiceGap_RelookLoopEndsWithoutAHold(t *testing.T) {
	fastConfirm(t)
	at := relookTrader(t) // its pass finds no wire: nothing to look at
	run := at.protectionRunCtx()
	ends(t, 6*time.Second, "K15: the loop runs on with no hold left", func() { at.protectionRelookLoop(run, make(chan struct{}, 1)) })
}

// relook is one background pass on the caller's goroutine.
func (w *lookWire) relook() bool {
	return w.at.protectionPass(time.Time{}, protectionRelookTrigger, w.at.protectionRunCtx())
}

// openHold is a hold that opened since ago and is waiting.
func (w *lookWire) openHold(since time.Duration, qty int, avg float64) {
	w.protState(func() {
		w.at.protHolds = map[string]*protectionHold{"MNQ|SHORT": {run: w.at.protectionRunCtx(), qty: qty, avg: avg,
			since: w.now().Add(-since), lastLook: w.now().Add(-3 * time.Second)}}
	})
}

// L08: the registration goes in the SAME pass that ends the last hold. A beat in
// between would leave its new hold to a goroutine that is about to end.
func TestLookTwiceGap_LastHoldDropsTheRegistration(t *testing.T) {
	w := gapWire(t, "reg")
	w.positions()
	w.book("periodic")
	w.sync()
	w.openHold(3*time.Second, 1, entry1193)
	w.protState(func() {
		w.at.protRelookRun, w.at.protRelookWake = w.at.protectionRunCtx(), make(chan struct{}, 1)
	})
	if more := w.relook(); more || w.relookAlive() {
		t.Fatalf("flat: more %v, registered %v", more, w.relookAlive())
	}
}

// L12: positions that cannot be read are the BEAT's to count.
func TestLookTwiceGap_RelookReadErrorIsTheBeats(t *testing.T) {
	w := gapWire(t, "readerr")
	t.Cleanup(func() { protectionUnverifiedSince.Delete(w.at.id) })
	w.openHold(3*time.Second, 1, entry1193)
	w.protState(func() { w.at.trader = nttrader.NewTCPTrader(w.s, "MNQ", "Sim999") })
	w.relook()
	if _, ok := protectionUnverifiedSince.Load(w.at.id); ok {
		t.Fatal("a re-look started the UNVERIFIED spell")
	}
	w.at.reconcileProtectionAt(w.now(), "monitor")
	if _, ok := protectionUnverifiedSince.Load(w.at.id); !ok {
		t.Fatal("control: the beat starts it")
	}
	w.protState(func() { w.at.trader = w.nt })
}

// L14, L16b, L30, L31: a re-look of a covered position with an open hold leaves the
// beat's records alone - the pending-stop sightings, the two-rows P0, the over-covered
// count and the UNVERIFIED spell.
func TestLookTwiceGap_RelookLeavesTheBeatsRecords(t *testing.T) {
	w := gapWire(t, "records")
	w.positions(short(1, entry1193), short(1, 27990))
	w.book("periodic", sl("a-sl", stop1193, 1, "Working"), sl("b-sl", stop1193, 1, "Working"))
	w.sync()
	w.openHold(3*time.Second, 1, entry1193)
	protectionUnverifiedSince.Store(w.at.id, int64(7))
	seen := w.at.id + "|gone-sl"
	pendingSLSeenMap.Store(seen, &pendingSLSeen{Count: 1})
	t.Cleanup(func() { protectionUnverifiedSince.Delete(w.at.id); pendingSLSeenMap.Delete(seen) })
	over := OverCoveredFound(w.at.id)
	twoRows := func() (n int) {
		for _, a := range w.p0s() {
			if a.Kind == alertKindProtectionTwoRows {
				n++
			}
		}
		return n
	}
	w.relook()
	if w.holds() != 0 {
		t.Fatalf("fixture: the re-look must have seen the cover, holds %d", w.holds())
	}
	if _, ok := pendingSLSeenMap.Load(seen); !ok {
		t.Error("L14: the pending sightings are counted per BEAT")
	}
	if twoRows() != 0 {
		t.Error("L16b: the two-rows P0 is the beat's")
	}
	if n := OverCoveredFound(w.at.id) - over; n != 0 {
		t.Errorf("L30: a re-look counted the over-covered position %d times", n)
	}
	if v, ok := protectionUnverifiedSince.Load(w.at.id); !ok || v.(int64) != 7 {
		t.Error("L31: the UNVERIFIED spell is the beat's")
	}
	w.at.reconcileProtectionAt(w.now(), "monitor")
	if _, ok := pendingSLSeenMap.Load(seen); ok || twoRows() != 1 || OverCoveredFound(w.at.id) == over {
		t.Error("control: the beat does all of them")
	}
	if _, ok := protectionUnverifiedSince.Load(w.at.id); ok {
		t.Error("control: the beat ends the spell")
	}
}

// L23c: a standing alert-only verdict is silent - but not over an OPEN hold, which
// still owes the owner its 35 s P0.
func TestLookTwiceGap_StandingAlertDoesNotSilenceAnOpenHold(t *testing.T) {
	w := gapWire(t, "standing")
	w.positions(short(1, 27733.50)) // no row, no price: alert-only
	w.book("periodic")
	w.sync()
	protectionLastAction.Store(w.at.id+"|MNQ|SHORT", protectionAlertOnly.String())
	w.relook() // no hold: silent
	w.mustHaveNoP0("a standing alert, no hold")
	w.openHold(36*time.Second, 1, 27733.50)
	w.relook()
	if ps := w.p0s(); len(ps) != 1 || ps[0].Kind != alertKindProtectionHeld {
		t.Fatalf("an open hold of 36 s owes its P0, got %+v", ps)
	}
	w.mustBeSilent("alert-only")
}

// L34-L38: what ends a place / flatten episode.
func TestLookTwiceGap_EpisodeState(t *testing.T) {
	at := &AutoTrader{id: "gap-episode"}
	const key = "gap-episode|MNQ|SHORT"
	t.Cleanup(func() { at.sweepStaleProtectionState(nil) })
	at.noteProtectionCovered("MNQ", "SHORT")
	if a := at.lastProtectionAction("MNQ", "SHORT"); a != "" {
		t.Fatalf("L35: nothing was acted on, so nothing is recorded, got %q", a)
	}
	protectionLastAction.Store(key, "place")
	protectionPlaceEpisode.Store(key, int64(1))
	protectionFlattenEpisode.Store(key, int64(2))
	protectionFlattenRetries.Store(key, int64(3))
	noteSendOutcome(key, true, true, false)
	at.noteProtectionCovered("MNQ", "SHORT")
	if _, ok := protectionPlaceEpisode.Load(key); ok {
		t.Error("L37: covered ends the place episode (the next naked spell is a new P0)")
	}
	if _, ok := protectionFlattenRetries.Load(key); ok || protectionFlattenStart(key) != 0 {
		t.Error("L36/L38: covered ends the flatten episode")
	}
	at.sweepStaleProtectionState([]map[string]interface{}{{"symbol": "MNQ", "side": "SHORT"}})
	if s := sendsSoFar(key); !s.told {
		t.Fatal("control: a held side keeps its state")
	}
	at.sweepStaleProtectionState(nil)
	if s := sendsSoFar(key); s != (protectionSends{}) {
		t.Errorf("L34: flat forgets what was sent, got %+v", s)
	}
}

// ── the sends and their P0s ──────────────────────────────────────────────────

// denyAlerts makes the alert table refuse rows while on is set.
func denyAlerts(t *testing.T, st *store.Store) (on *atomic.Bool) {
	t.Helper()
	on = new(atomic.Bool)
	if err := st.GormDB().Callback().Create().Before("gorm:create").Register("gap:deny-alerts", func(tx *gorm.DB) {
		if on.Load() && strings.Contains(tx.Statement.Table, "alert") {
			_ = tx.AddError(errors.New("database is locked"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	return on
}

// N06b, N18, V14: a refusal whose row was refused still reached the PHONE, so the
// retry that sends must say so there ("do not add another"). Place and flatten.
func TestLookTwiceGap_ARefusalToldOnlyByPhoneIsAnswered(t *testing.T) {
	for name, v := range map[string]protectionVerdict{
		"place":   {Action: protectionPlace, StopPx: 31163, NeedQty: 1, Source: "plan", Why: "naked."},
		"flatten": {Action: protectionFlatten, StopPx: 31163, NeedQty: 1, Why: "through."},
	} {
		at, st := resetTrader(t, store.StrategyConfig{})
		pl, cl := &placeTrader{}, &closeFailTrader{}
		at.id, at.exchange, at.trader = "gap-phone-"+name, "ninjatrader", pl
		if v.Action == protectionFlatten {
			at.trader = cl
		}
		t.Cleanup(func() { at.sweepStaleProtectionState(nil) })
		deny := denyAlerts(t, st)
		t0 := time.Now()
		for i, err := range []error{fmt.Errorf("refused"), nil} {
			deny.Store(i == 0)
			pl.err, cl.err = err, err
			at.actOnProtection(v, "MNQ", "LONG", 1, "sig", "t", 0, t0.Add(time.Duration(i)*time.Minute))
		}
		all, err := st.Alert().List(at.id, 10)
		n := 0
		for _, a := range all {
			if strings.Contains(a.Title, "now SENT") {
				n++
			}
		}
		if err != nil || n != 1 {
			t.Errorf("%s: want one 'now SENT' P0, got %d of %d rows (%v)", name, n, len(all), err)
		}
	}
}

// N12: after a stop of the same episode WAS sent, a refusal must not tell the owner
// to set one by hand (two stops can double-exit).
func TestLookTwiceGap_ARefusalAfterASendDoesNotAskForAHandStop(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{})
	tr := &placeTrader{}
	at.id, at.exchange, at.trader = "gap-hand", "ninjatrader", tr
	t.Cleanup(func() { at.sweepStaleProtectionState(nil) })
	v := protectionVerdict{Action: protectionPlace, StopPx: 31163, NeedQty: 1, Source: "plan", Why: "naked."}
	deny := denyAlerts(t, st)
	t0 := time.Now()
	deny.Store(true)
	at.actOnProtection(v, "MNQ", "LONG", 1, "sig", "t", 0, t0) // sent; nobody was told
	deny.Store(false)
	tr.err = fmt.Errorf("refused")
	at.actOnProtection(v, "MNQ", "LONG", 1, "sig", "t", 0, t0.Add(time.Minute))
	all, err := st.Alert().List(at.id, 10)
	if err != nil || len(all) != 1 || !strings.Contains(all[0].Title, "could NOT be sent") || strings.Contains(all[0].Body, "Set a stop by hand") {
		t.Fatalf("got %d (%v): %+v", len(all), err, all)
	}
}

// countTrader counts what reaches a broker that has no guarded sends.
type countTrader struct {
	MockTrader
	closes, stops int
}

func (c *countTrader) CloseLong(string, float64) (map[string]interface{}, error) {
	c.closes++
	return map[string]interface{}{}, nil
}

func (c *countTrader) CloseShort(string, float64) (map[string]interface{}, error) {
	c.closes++
	return map[string]interface{}{}, nil
}

func (c *countTrader) PlaceProtectiveStop(string, string, int, float64, string, string) error {
	c.stops++
	return nil
}

// guardedTrader also has the guarded close.
type guardedTrader struct {
	countTrader
	sides []string
}

func (g *guardedTrader) CloseAllIf(side string, guard func() error) error {
	if err := guard(); err != nil {
		return err
	}
	g.sides = append(g.sides, side)
	return nil
}

var (
	_ types.Trader = (*countTrader)(nil)
	_ types.Trader = (*guardedTrader)(nil)
)

func withheld() error { return fmt.Errorf("%w: flat", ntwire.ErrSendWithheld) }

// N19, N20, O02: a broker with no guarded send is asked just before; one with no
// standalone stop says so.
func TestLookTwiceGap_UnguardedBrokerAsksBeforeTheSend(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{})
	tr := &countTrader{}
	at.id, at.exchange, at.trader = "gap-unguarded", "ninjatrader", tr
	now := time.Now()
	for _, side := range []string{"LONG", "SHORT"} {
		if err := at.placeProtectiveStopIf("MNQ", side, 1, 100, "s", "t", withheld); !errors.Is(err, ntwire.ErrSendWithheld) || tr.stops != 0 {
			t.Fatalf("N20: err %v, stops %d", err, tr.stops)
		}
		p := &store.TraderPosition{TraderID: at.id, Symbol: "MNQ", Side: side}
		if got := at.flattenPositionAtMarket(p, "t", now, withheld); got != marketFlatWithheld || tr.closes != 0 {
			t.Fatalf("O02: outcome %v, closes %d", got, tr.closes)
		}
	}
	if err := at.placeProtectiveStopIf("MNQ", "LONG", 1, 100, "s", "t", func() error { return nil }); err != nil || tr.stops != 1 {
		t.Fatalf("control: err %v, stops %d", err, tr.stops)
	}
	at.trader = struct{ types.Trader }{tr} // the 19 methods of a broker and nothing else
	if err := at.placeProtectiveStopIf("MNQ", "LONG", 1, 100, "s", "t", nil); !errors.Is(err, errNoStandaloneStop) {
		t.Fatalf("N19: a stop nobody can place is not a stop that was sent: %v", err)
	}
}

// O01, O03, O04: the guarded close is the one used, for the side held, and a withheld
// one is not logged as a close that failed.
func TestLookTwiceGap_GuardedCloseIsUsed(t *testing.T) {
	var log bytes.Buffer
	old := logger.Log.Out
	logger.Log.SetOutput(&log)
	t.Cleanup(func() { logger.Log.SetOutput(old) })
	at, _ := resetTrader(t, store.StrategyConfig{})
	tr := &guardedTrader{}
	at.id, at.exchange, at.trader = "gap-guarded", "ninjatrader", tr
	if err := at.sendMarketCloseIf("MNQ", "LONG", "t", withheld); !errors.Is(err, ntwire.ErrSendWithheld) {
		t.Fatalf("O01: the guard was not asked by the guarded close: %v", err)
	}
	if strings.Contains(log.String(), "failed") {
		t.Fatalf("O04: a withheld close is not a failed one: %s", log.String())
	}
	for _, side := range []string{"LONG", "SHORT"} {
		if err := at.sendMarketCloseIf("MNQ", side, "t", func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(tr.sides, ","); got != "long,short" || tr.closes != 0 {
		t.Fatalf("O01/O03: guarded closes %q, unguarded %d", got, tr.closes)
	}
}

// O06: escalating a resting limit exit to the market is a close too.
func TestLookTwiceGap_EscalationIsGuarded(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{})
	tr := &countTrader{}
	at.id, at.exchange, at.trader = "gap-esc", "ninjatrader", tr
	now := time.Now()
	pos := store.TraderPosition{TraderID: at.id, Symbol: "MNQ", Side: "LONG"}
	at.limitFlattenMu.Lock()
	at.registerLimitFlattenLocked(&pendingLimitFlatten{position: pos, key: flattenKeyOf(&pos), tag: "t", after: time.Hour,
		closeSignalID: "lx-1", sentAt: now, deadline: now.Add(time.Hour)}, false)
	at.limitFlattenMu.Unlock()
	if got := at.flattenPositionAtMarket(&pos, "t", now, withheld); got != marketFlatWithheld || tr.closes != 0 {
		t.Fatalf("outcome %v, closes %d", got, tr.closes)
	}
	if got := at.flattenPositionAtMarket(&pos, "t", now, func() error { return nil }); got != marketFlatSent || tr.closes != 1 {
		t.Fatalf("control: outcome %v, closes %d", got, tr.closes)
	}
}

// Q06, Q08, Q16, Q17: what may cap a stop.
func TestLookTwiceGap_OwnRowCap(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{})
	at.id = "gap-own"
	for i, sym := range []string{"MNQ", "MES"} {
		p := &store.TraderPosition{TraderID: at.id, ExchangeType: "ninjatrader", Symbol: sym, Side: "SHORT", Quantity: 1, EntryQuantity: 1,
			EntryPrice: 100, EntryOrderID: fmt.Sprint(i), EntryTime: 1, Leverage: 1, Status: "OPEN", Source: "t", Account: "Sim101"}
		if err := st.Position().Create(p); err != nil {
			t.Fatal(err)
		}
	}
	if got := at.ownOpenRow("Sim101", "MNQ", "SHORT"); got != (protRowSum{1, 100}) {
		t.Errorf("Q06: another symbol's row is not ours there: %+v", got)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if got := at.ownOpenRow("Sim101", "MNQ", "SHORT"); got != (protRowSum{}) {
		t.Errorf("Q08: an unreadable ledger caps nothing (a smaller stop leaves contracts naked): %+v", got)
	}
	if got := protectionTick("no-such-symbol"); got != 0.25 {
		t.Errorf("Q16: tick %v", got)
	}
	rows := protectionTwoRows([]map[string]interface{}{
		{"symbol": "MNQ", "side": "SHORT", "quantity": 1.0, "entryPrice": 100.0},
		{"symbol": "MNQ", "side": "SHORT", "quantity": 0.0, "entryPrice": 90.0}})
	if n := len(rows["MNQ|SHORT"]); n != 1 {
		t.Errorf("Q17: a row of zero is not a position, got %d rows", n)
	}
}

// ── own reverse ──────────────────────────────────────────────────────────────

// overClosed is a closed row of 1 whose exits filled 2: one receipt of 1 applied, one
// of 1 (a "Close") not. NT8 holds what held says.
func overClosed(t *testing.T, id, symbol, side string, held ...ntwire.OpenPosition) (*lookWire, store.NT8ExitReceipt) {
	t.Helper()
	w := gapWire(t, "or-"+id)
	now := w.now().UnixMilli()
	p := w.row("e1", side, 1, 27733.50, w.now().Add(-time.Minute))
	if err := w.st.GormDB().Model(p).Updates(map[string]any{"status": "CLOSED", "exit_time": now - 1000, "symbol": symbol}).Error; err != nil {
		t.Fatal(err)
	}
	mk := func(id string, pos int64, applied bool, sig string, ms int64) store.NT8ExitReceipt {
		r := store.NT8ExitReceipt{ID: id, PositionID: pos, Applied: applied, Account: "Sim101", Symbol: symbol, Side: side, SignalID: sig,
			TraderID: w.at.id, Quantity: 1, Price: 27753.25, PointValue: 2, ExitMs: ms, ReceivedMs: ms}
		if err := w.st.GormDB().Create(&r).Error; err != nil {
			t.Fatal(err)
		}
		return r
	}
	mk("applied", p.ID, true, "e1", now-1000)
	r := mk("over", 0, false, "Close", now)
	w.positions(held...)
	w.sync()
	return w, r
}

func (w *lookWire) reverseCheck(r store.NT8ExitReceipt) (bool, string) {
	return w.at.ownReverseCheck(w.s, w.nt, r, w.now(), "")
}

func TestOwnReverseGap_Check(t *testing.T) {
	t.Run("R23 a long over-closed is a SHORT reverse", func(t *testing.T) {
		w, r := overClosed(t, "long", "MNQ", "LONG", short(1, 27753.25))
		if done, why := w.reverseCheck(r); !done || why != "" {
			t.Fatalf("done %v why %q", done, why)
		}
		if ps := w.reverseP0s(); len(ps) != 1 || !strings.Contains(ps[0].Body, "SHORT MNQ ×1 @ 27753.25") {
			t.Fatalf("%+v", ps)
		}
	})
	t.Run("R20 R21 not stored, then applied", func(t *testing.T) {
		w, r := overClosed(t, "applied", "MNQ", "SHORT", long(1, 27753.25))
		gone := r
		gone.ID = "no-such"
		if done, why := w.reverseCheck(gone); !done || !strings.Contains(why, "not stored") {
			t.Fatalf("R21: done %v why %q", done, why)
		}
		w.st.GormDB().Model(&store.NT8ExitReceipt{}).Where("id = ?", r.ID).Update("applied", true)
		if done, why := w.reverseCheck(r); !done || why != "" || len(w.reverseP0s()) != 0 {
			t.Fatalf("R20: a receipt that only beat its row: done %v why %q", done, why)
		}
	})
	t.Run("R22b a frame of the connection before", func(t *testing.T) {
		w, r := overClosed(t, "link", "MNQ", "SHORT", long(1, 27753.25))
		w.redial()
		if done, _ := w.reverseCheck(r); done || len(w.reverseP0s()) != 0 {
			t.Fatal("the positions of an older connection prove nothing: wait")
		}
	})
	t.Run("R25 another symbol held", func(t *testing.T) {
		w, r := overClosed(t, "symbol", "MNQ", "SHORT", ntwire.OpenPosition{Symbol: "MES", Side: "long", Quantity: 1, AvgPrice: 27753.25})
		if done, why := w.reverseCheck(r); !done || !strings.Contains(why, "holds no LONG") {
			t.Fatalf("done %v why %q", done, why)
		}
	})
	t.Run("R30 a symbol with no tick size", func(t *testing.T) {
		if market.FuturesTickSize("ZZZ9") > 0 {
			t.Skip("fixture: ZZZ9 has a tick size")
		}
		w, r := overClosed(t, "tick", "ZZZ9", "SHORT", ntwire.OpenPosition{Symbol: "ZZZ9", Side: "long", Quantity: 1, AvgPrice: 27753.25})
		if done, why := w.reverseCheck(r); !done || !strings.Contains(why, "no tick size") {
			t.Fatalf("done %v why %q", done, why)
		}
	})
	t.Run("R39 the ledger cannot be read", func(t *testing.T) {
		w, r := overClosed(t, "ledger", "MNQ", "SHORT", long(1, 27753.25))
		var deny atomic.Bool
		if err := w.st.GormDB().Callback().Query().Before("gorm:query").Register("gap:deny", func(tx *gorm.DB) {
			if deny.Load() && tx.Statement.Table == "trader_positions" {
				_ = tx.AddError(errors.New("database is locked"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		deny.Store(true)
		if done, why := w.reverseCheck(r); done || !strings.Contains(why, "could not be read") {
			t.Fatalf("a busy database is not an answer: done %v why %q", done, why)
		}
		deny.Store(false)
		if done, why := w.reverseCheck(r); !done || why != "" || len(w.reverseP0s()) != 1 {
			t.Fatalf("control: the next look finds the reverse: done %v why %q", done, why)
		}
	})
	t.Run("R41 two receipts 5 s apart are one over-close", func(t *testing.T) {
		w, r := overClosed(t, "group", "MNQ", "SHORT", long(2, 27753.25))
		b := r
		b.ID, b.ExitMs = "over-2", r.ExitMs+5000
		if err := w.st.GormDB().Create(&b).Error; err != nil {
			t.Fatal(err)
		}
		if done, why := w.reverseCheck(b); !done || why != "" || len(w.reverseP0s()) != 1 {
			t.Fatalf("done %v why %q P0s %d", done, why, len(w.reverseP0s()))
		}
	})
}

// R36: a book that is not there, or is old, is not "no stop".
func TestOwnReverseGap_StopTextNeverGuesses(t *testing.T) {
	w := gapWire(t, "or-stop")
	if got := w.at.ownReverseStopText("MNQ", "LONG", w.now()); got != "its stop is UNKNOWN" {
		t.Fatalf("no book: %q", got)
	}
	w.book("periodic")
	w.sync()
	if got := w.at.ownReverseStopText("MNQ", "LONG", w.now()); got != "no stop" {
		t.Fatalf("control: %q", got)
	}
	if got := w.at.ownReverseStopText("MNQ", "LONG", w.now().Add(snapshotMaxAge()+time.Minute)); got != "its stop is UNKNOWN" {
		t.Fatalf("an old book: %q", got)
	}
}

// R07, R09: the watch belongs to the Run and Stop waits for it.
func TestOwnReverseGap_WatchBelongsToTheRun(t *testing.T) {
	w, r := overClosed(t, "run", "MNQ", "SHORT", long(1, 27753.25))
	began := traderEvents(w.at.id, "own_reverse_watch")
	w.at.watchOwnReverse(r)
	if n := traderEvents(w.at.id, "own_reverse_watch") - began; n != 1 {
		t.Fatalf("fixture: the watch must start, counted %.0f", n)
	}
	joined := make(chan struct{})
	go func() { w.at.monitorWg.Wait(); close(joined) }()
	select {
	case <-joined:
		t.Fatal("R09: Stop would not wait for the watch")
	case <-time.After(150 * time.Millisecond):
	}
	w.at.cancelStopMonitor()
	ends(t, 2*time.Second, "the Run's end must end the watch", func() { <-joined })
	w.at.watchOwnReverse(r) // isRunning is still set: Stop cancels first, then clears it
	if n := traderEvents(w.at.id, "own_reverse_watch") - began; n != 1 {
		t.Fatal("R07: a watch was started on a Run that has ended")
	}
}

// R10: whose close it was is decided at the instant the fill was READ. The ordered
// worker may hand the receipt over much later (it waits on the database).
func TestOwnReverseGap_AttributionAtTheFillInstant(t *testing.T) {
	ownReverseDelay = 10 * time.Millisecond
	t.Cleanup(func() { ownReverseDelay = ownReverseDelayDefault })
	w, r := overClosed(t, "worker", "MNQ", "SHORT", long(1, 27753.25))
	if _, err := w.nt.CloseShort("MNQ", 0); err != nil {
		t.Fatal(err)
	}
	w.jump(time.Second)
	w.orderUpdate("e1-sl", "filled", 27753.25, 1) // the read loop saw an exit fill here
	w.positions(long(1, 27753.25))
	w.sync()
	w.jump(15 * time.Second) // the worker's wait: past ownReverseSendWindow
	w.at.watchOwnReverse(r)
	w.mustFlagReverse(5 * time.Second)
	if p := w.reverseP0s()[0]; !strings.Contains(p.Title, "The bot's close") {
		t.Fatalf("our close 1 s before the fill is ours however late the worker is: %q", p.Title)
	}
}

// R19: nothing readable for 70 s ends the watch, and says so.
func TestOwnReverseGap_GivesUpAfter70s(t *testing.T) {
	ownReverseDelay = 10 * time.Millisecond
	t.Cleanup(func() { ownReverseDelay = ownReverseDelayDefault })
	w, r := overClosed(t, "giveup", "MNQ", "SHORT", long(1, 27753.25))
	w.redial() // no positions frame on this connection
	before := traderEvents(w.at.id, "own_reverse_unresolved")
	run, cancel := context.WithCancel(context.Background())
	defer cancel()
	ends(t, 3*time.Second, "the watch never ends", func() {
		w.at.ownReverseLoop(run, w.s, w.nt, r, w.now().Add(-ownReverseGiveUp-time.Second), "")
	})
	if n := traderEvents(w.at.id, "own_reverse_unresolved") - before; n != 1 {
		t.Fatalf("counted %.0f", n)
	}
}

// V16, V17 (logging only): the boot log prints both settings in force.
func TestLookTwiceGap_BootLinesArePrinted(t *testing.T) {
	src, err := os.ReadFile("../main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{"trader.ProtectionConfirmBootLine()", "trader.OwnReverseBootLine()"} {
		if !strings.Contains(string(src), call) {
			t.Errorf("main.go no longer prints %s", call)
		}
	}
}
