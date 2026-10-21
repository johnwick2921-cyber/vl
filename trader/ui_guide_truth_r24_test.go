package trader

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// UI-GUIDE-TRUTH (release #24) — the dashboard chart draws what these
// production call sites serve: MentorTruthSnapshot (/api/mentor/truth) and
// ledgerOpenOrders (/api/open-orders via TCPTrader.GetOpenOrders).

// ug1SwingTrader builds a mentor trader holding a FILLED swing leg the way the
// production pieces leave it: the armed row in the ledger, the arm in the
// trader registry (mentorRegisterLiveArm), and the evaluator's swing position
// (SwingState.RebuildSwingPos) — entered long at 31020 with its stop at 31000.
func ug1SwingTrader(t *testing.T, movedBE bool) (*AutoTrader, string) {
	t.Helper()
	at, st := f2Trader(t)
	at.id = "ug1"
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	sig := "ug1-swing-sig"
	armID := "swing-" + strconv.FormatInt(swingRestartOpenTime, 10)
	row := swingRestartArm(at.id, sig, armID, 1, 31020, 31000, 31080)
	if err := st.ArmedOrders().UpsertArm(row); err != nil {
		t.Fatal(err)
	}
	at.mentorRegisterLiveArm(armID, row.ID, "long", 31020, row.Scenario)
	t.Cleanup(func() { at.mentorUnregisterLiveArm(armID) })
	sw := &at.mentorEval.State.Swing
	if !sw.RebuildSwingPos(mentor.SwingRebuild{
		Side: mentor.SideLong, Entry: 31020, EntryStop: 31000, LiveStop: 31000,
		Target: 31080, EntryBucket: swingRestartOpenTime, ArmID: armID, MovedBE: movedBE,
	}) {
		t.Fatal("RebuildSwingPos refused the fixture")
	}
	sw.Line, sw.EmaCount = 31005, mentor.FourHEMA34Min // a warm 4h EMA 34: the swing view renders
	return at, sig
}

func ug1PinBook(t *testing.T, orders []ntwire.NT8Order, ok bool) {
	t.Helper()
	prev := mentorStopBook
	mentorStopBook = func(*nttrader.TCPTrader) (ntwire.OrderSnapshotPayload, time.Time, bool) {
		return ntwire.OrderSnapshotPayload{Orders: orders}, time.Now(), ok
	}
	t.Cleanup(func() { mentorStopBook = prev })
}

// UG-1 (E-1): after the swing's +1R break-even move the evaluator still holds
// the ENTRY stop (manageSwingPosition sets only MovedBE). The chart must draw
// the stop working at NT8 — here the BE at the entry, 31020 — never 31000.
// Mutant: drop the broker override in mentorSwingStopTruth → RED.
func TestMentorTruthSwingStopIsTheBrokerStopAfterBreakEven(t *testing.T) {
	at, sig := ug1SwingTrader(t, true)
	ug1PinBook(t, []ntwire.NT8Order{
		{Name: sig + "-sl", State: "Cancelled", StopPrice: 30990, Quantity: 1, Symbol: "MNQ"}, // a dead stop is never read
		f2StopOrder(sig+"-sl", 31020, 1),
	}, true)

	p, ok := at.MentorTruthSnapshot(time.Now())
	if !ok || p.Swing == nil || p.Swing.Pos == nil {
		t.Fatalf("no swing position in the panel: ok=%v swing=%+v", ok, p.Swing)
	}
	got := p.Swing.Pos
	if got.Stop != 31020 || got.StopSource != "broker" {
		t.Fatalf("swing stop = %.2f (%s), want the NT8 working stop 31020.00 (broker)", got.Stop, got.StopSource)
	}
	if !got.MovedBE {
		t.Errorf("moved_be = false, want true (the evaluator emitted the BE move)")
	}
	// The evaluator's own belief is untouched (read by nothing but the panel).
	if s := at.mentorEval.State.Swing.Pos.Stop; s != 31000 {
		t.Errorf("evaluator swing Stop mutated to %.2f, want the entry stop 31000 untouched", s)
	}
	b, _ := json.Marshal(p.Swing.Pos)
	if !strings.Contains(string(b), `"stop":31020`) || !strings.Contains(string(b), `"stop_source":"broker"`) {
		t.Errorf("wire = %s, want stop 31020 from the broker", b)
	}
}

// UG-1: no readable book → the entry stop is drawn and SAYS so; a BE is never
// guessed from MovedBE alone (the move may have been refused).
func TestMentorTruthSwingStopFallsBackToTheEntryStopNamed(t *testing.T) {
	at, _ := ug1SwingTrader(t, true)
	ug1PinBook(t, nil, false)
	p, _ := at.MentorTruthSnapshot(time.Now())
	if p.Swing == nil || p.Swing.Pos == nil {
		t.Fatal("no swing position in the panel")
	}
	if p.Swing.Pos.Stop != 31000 || p.Swing.Pos.StopSource != "entry" {
		t.Fatalf("swing stop = %.2f (%s), want the entry stop 31000 labelled entry", p.Swing.Pos.Stop, p.Swing.Pos.StopSource)
	}
}

// UG-2 (E-3): /api/open-orders?symbol=<other> must not serve THIS trader's
// MNQ rows — the chart drew them on any symbol tab. "" stays the whole book
// (the dead-man reconcile probe and the post-reconnect sweep ask that).
// Mutant: make ledgerSymbolMatches return true → RED.
func TestLedgerOpenOrdersAnswerOnlyTheTradersSymbol(t *testing.T) {
	at, st := f2Trader(t) // NinjaTraderSymbol MNQ
	row := &store.ArmedOrderDB{
		TraderID: at.id, PlanID: "p", Scenario: "S1", Side: "LONG", State: store.StateWorking,
		SignalID: "ug2-sig", EntryPx: 30000, StopPx: 29980, TargetPx: 30040, Kind: "limit",
	}
	if err := st.ArmedOrders().UpsertArm(row); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		sym  string
		want int
	}{
		{"MNQ", 1}, {"mnq", 1}, {"MNQZ6", 1}, {"MNQ 12-26", 1}, {"", 1},
		{"ES", 0}, {"NQ", 0}, {"AAPL", 0}, {"EUR/USD", 0},
	} {
		got, err := at.ledgerOpenOrders(tc.sym)
		if err != nil {
			t.Fatalf("%q: %v", tc.sym, err)
		}
		if got == nil || len(got) != tc.want {
			t.Errorf("ledgerOpenOrders(%q) = %d row(s) (nil=%v), want %d", tc.sym, len(got), got == nil, tc.want)
		}
	}
}

// UG-2, Go side: the panel names the instrument its levels belong to.
func TestMentorTruthNamesItsSymbol(t *testing.T) {
	at, _ := f2Trader(t)
	at.config.NinjaTraderSymbol = "MES"
	at.mentorEval = mentor.New(mentor.DefaultConfig())
	p, ok := at.MentorTruthSnapshot(time.Now())
	if !ok || p.Symbol != "MES" {
		t.Fatalf("panel symbol = %q (ok=%v), want MES", p.Symbol, ok)
	}
}

// UG-5 (E-9): a split mentor stop-entry carries leg 1's OWN take-profit for
// leg1_qty contracts (row 362: 2 of 3 at 30725.25) besides the runner's
// target. Both reach the chart. Mutant: drop the Leg1 copy → RED.
func TestLedgerOpenOrdersServeLegOneTakeProfit(t *testing.T) {
	at, st := f2Trader(t)
	split := &store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor:" + at.id, Scenario: "isb-1-9", Side: "SHORT", State: store.StateWorking,
		SignalID: "ug5-split", EntryPx: 30742.75, StopPx: 30760.25, TargetPx: 30703, Kind: "stop_entry",
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(3), Leg1Qty: store.IntPtr(2), Leg1TP: 30725.25,
	}
	single := &store.ArmedOrderDB{
		TraderID: at.id, PlanID: "p", Scenario: "S2", Side: "LONG", State: store.StateWorking,
		SignalID: "ug5-single", EntryPx: 30000, StopPx: 29980, TargetPx: 30040, Kind: "limit",
	}
	for _, r := range []*store.ArmedOrderDB{split, single} {
		if err := st.ArmedOrders().UpsertArm(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := at.ledgerOpenOrders("MNQ")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]int{}
	for i, o := range got {
		by[o.OrderID] = i
	}
	s, ok := by["ug5-split"]
	if !ok {
		t.Fatalf("split row not served: %+v", got)
	}
	if o := got[s]; o.Leg1TP != 30725.25 || o.Leg1Qty != 2 || o.TargetPrice != 30703 || o.Quantity != 3 {
		t.Errorf("split row = leg1 %.2f×%d, runner %.2f, qty %.0f; want leg1 30725.25×2, runner 30703, qty 3", o.Leg1TP, o.Leg1Qty, o.TargetPrice, o.Quantity)
	}
	if o := got[by["ug5-single"]]; o.Leg1TP != 0 || o.Leg1Qty != 0 {
		t.Errorf("single-bracket row carries a leg-1 TP %.2f×%d, want absent", o.Leg1TP, o.Leg1Qty)
	}
	b, _ := json.Marshal(got[s])
	if !strings.Contains(string(b), `"leg1_tp":30725.25`) || !strings.Contains(string(b), `"leg1_qty":2`) {
		t.Errorf("wire = %s, want leg1_tp and leg1_qty", b)
	}
}
