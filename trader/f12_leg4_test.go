// F12 — cutover leg 4 reads the BROKER, and the override guard becomes a
// checked condition instead of a blanket refusal.
//
// Leg 4 was the last leg answered by our own bookkeeping. These pin the four
// ways it must FAIL (a working order, a stale book, no book at all, and a
// ledger/broker disagreement) and the one way it may pass. A leg that cannot be
// evaluated FAILS — the gate's own rule, applied to the gate itself.

package trader

import (
	"strings"
	"testing"
	"time"

	nt "vl/provider/ninjatrader"
)

func snapWith(orders ...nt.NT8Order) nt.OrderSnapshotPayload {
	// Every fixture order is stamped MNQ: the frame is account-scoped and leg 4
	// filters by instrument, so an order with no symbol would silently drop out.
	for i := range orders {
		if orders[i].Symbol == "" {
			orders[i].Symbol = "MNQ"
		}
	}
	return nt.OrderSnapshotPayload{
		Account: "Sim101", BuildID: "2026-09-03-f12",
		EmittedMs: 1, Orders: orders,
	}
}

func TestLeg4PassesOnAFreshEmptyBrokerBook(t *testing.T) {
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)
	c := nt.NewOrderSnapshotCache()
	c.PutAt(snapWith(), now.Add(-10*time.Second))

	leg := Leg4FromBrokerAt(c, "Sim101", "MNQ", 30*time.Second, nil, now)
	if !leg.Pass {
		t.Fatalf("fresh empty book must PASS: %+v", leg)
	}
	if !strings.Contains(leg.Source, "broker") {
		t.Errorf("source must name the broker, got %q", leg.Source)
	}
	if !strings.Contains(leg.Detail, "0") {
		t.Errorf("detail must state the count, got %q", leg.Detail)
	}
}

func TestLeg4FailsWithAWorkingOrderAndNamesIt(t *testing.T) {
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)
	c := nt.NewOrderSnapshotCache()
	c.PutAt(snapWith(nt.NT8Order{
		OrderID: "NT-7", Name: "VL-S1-stop", Type: "stop",
		StopPrice: 29475, Quantity: 1, State: "Working",
	}), now.Add(-5*time.Second))

	leg := Leg4FromBrokerAt(c, "Sim101", "MNQ", 30*time.Second, nil, now)
	if leg.Pass {
		t.Fatalf("a working order must FAIL leg 4")
	}
	if !strings.Contains(leg.Detail, "NT-7") {
		t.Errorf("the failing order must be NAMED, got %q", leg.Detail)
	}
}

// A book we have not heard about in over 2N seconds is not evidence of a flat
// book — it is evidence of nothing.
func TestLeg4FailsOnAStaleBook(t *testing.T) {
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)
	c := nt.NewOrderSnapshotCache()
	c.PutAt(snapWith(), now.Add(-61*time.Second)) // > 2 × 30s

	leg := Leg4FromBrokerAt(c, "Sim101", "MNQ", 30*time.Second, nil, now)
	if leg.Pass {
		t.Fatalf("a stale book must FAIL")
	}
	if !strings.Contains(strings.ToLower(leg.Detail), "stale") {
		t.Errorf("detail must say stale and the age, got %q", leg.Detail)
	}
	if !strings.Contains(leg.Detail, "61") {
		t.Errorf("detail must quote the age, got %q", leg.Detail)
	}
}

// F1 — THE TRANSITION. The Go side boots before the AddOn is reloaded, so an
// old DLL sends no snapshots. Blocking every cutover until NT8 is reloaded
// would make the wave undeployable, so leg 4 falls back to the ledger — and the
// SOURCE says so every time. The rule is "never a SILENT fallback", not "never
// a fallback"; this test pins the loudness, which is the part that can rot.
func TestLeg4FallsBackToTheLedgerLoudlyBeforeTheAddOnIsReloaded(t *testing.T) {
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)
	c := nt.NewOrderSnapshotCache() // nothing ever received

	leg := Leg4FromBrokerAt(c, "Sim101", "MNQ", 30*time.Second, nil, now)
	if !leg.Pass {
		t.Fatalf("an empty ledger with no snapshot yet must not block the Go boot: %+v", leg)
	}
	if !strings.Contains(strings.ToLower(leg.Source), "ledger") ||
		!strings.Contains(strings.ToLower(leg.Source), "no snapshot yet") {
		t.Errorf("the fallback must be NAMED in the source, got %q", leg.Source)
	}
	if !strings.Contains(leg.Detail, "LEDGER") {
		t.Errorf("the detail must say whose answer this is, got %q", leg.Detail)
	}
	// and it still fails when the ledger itself holds an arm
	busy := Leg4FromBrokerAt(c, "Sim101", "MNQ", 30*time.Second,
		[]OpenOrder{{OrderID: "arm-1", Symbol: "MNQ"}}, now)
	if busy.Pass {
		t.Errorf("a resting arm must still FAIL during the fallback")
	}
}

// Once snapshots ARE arriving, a key with no book is a regression, not a cold
// start, and the two must not render the same.
func TestLeg4FailsWhenSnapshotsArriveButNotForThisAccount(t *testing.T) {
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)
	c := nt.NewOrderSnapshotCache()
	c.PutAt(nt.OrderSnapshotPayload{Account: "SimOther", BuildID: "b", Orders: []nt.NT8Order{}},
		now.Add(-5*time.Second))

	leg := Leg4FromBrokerAt(c, "Sim101", "MNQ", 30*time.Second, nil, now)
	if leg.Pass {
		t.Fatalf("a live link with no book for this ACCOUNT must FAIL, not fall back")
	}
	if !strings.Contains(leg.Source, "broker") {
		t.Errorf("this is a broker-side absence and the source must say so: %q", leg.Source)
	}
}

// The ledger stops being the answer and becomes the cross-check.
func TestLeg4FailsWhenTheLedgerAndTheBrokerDisagree(t *testing.T) {
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)
	c := nt.NewOrderSnapshotCache()
	c.PutAt(snapWith(), now.Add(-5*time.Second)) // broker says flat

	ledger := []OpenOrder{{OrderID: "arm-9", Symbol: "MNQ"}} // ledger says one arm
	leg := Leg4FromBrokerAt(c, "Sim101", "MNQ", 30*time.Second, ledger, now)
	if leg.Pass {
		t.Fatalf("a broker/ledger disagreement must FAIL even when the broker is flat")
	}
	d := leg.Detail
	if !strings.Contains(d, "broker 0") || !strings.Contains(d, "ledger 1") {
		t.Errorf("detail must quote BOTH counts, got %q", d)
	}
	if !strings.Contains(d, "arm-9") {
		t.Errorf("the diff must name the row, got %q", d)
	}
}

func TestLeg4PassesWhenBothAgreeTheBookIsEmpty(t *testing.T) {
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)
	c := nt.NewOrderSnapshotCache()
	c.PutAt(snapWith(), now.Add(-1*time.Second))
	leg := Leg4FromBrokerAt(c, "Sim101", "MNQ", 30*time.Second, []OpenOrder{}, now)
	if !leg.Pass {
		t.Fatalf("broker flat + ledger flat must PASS: %+v", leg)
	}
}

// ─────────────────────────────────────────────────────────────────────
// E3 — the override guard. Before F12 the rule was a blanket "no override with
// a position open", written after 0B waived flat with position 588 open and the
// resting stop could not be verified. It becomes a CHECK.
// ─────────────────────────────────────────────────────────────────────

func TestOverrideAllowedWhenTheBrokerShowsTheStop(t *testing.T) {
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)
	c := nt.NewOrderSnapshotCache()
	c.PutAt(snapWith(nt.NT8Order{
		OrderID: "NT-stop", Type: "stop", StopPrice: 29475.00,
		Quantity: 1, State: "Working",
	}), now.Add(-3*time.Second))

	ok, reason := OverrideAllowedAt(c, "Sim101", "MNQ", 29475.00, 0.5, 30*time.Second, now)
	if !ok {
		t.Fatalf("a verified resting stop must allow the override: %s", reason)
	}
}

func TestOverrideRefusedWhenNoStopIsResting(t *testing.T) {
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)
	c := nt.NewOrderSnapshotCache()
	c.PutAt(snapWith(nt.NT8Order{
		OrderID: "NT-entry", Type: "limit", LimitPrice: 29450, Quantity: 1, State: "Working",
	}), now.Add(-3*time.Second))

	ok, reason := OverrideAllowedAt(c, "Sim101", "MNQ", 29475.00, 0.5, 30*time.Second, now)
	if ok {
		t.Fatalf("no resting stop must REFUSE the override")
	}
	if !strings.Contains(strings.ToLower(reason), "stop") {
		t.Errorf("reason must say what was missing, got %q", reason)
	}
}

func TestOverrideRefusedWhenTheStopIsAtTheWrongPrice(t *testing.T) {
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)
	c := nt.NewOrderSnapshotCache()
	c.PutAt(snapWith(nt.NT8Order{
		OrderID: "NT-stop", Type: "stop", StopPrice: 29500.00, Quantity: 1, State: "Working",
	}), now.Add(-3*time.Second))

	ok, reason := OverrideAllowedAt(c, "Sim101", "MNQ", 29475.00, 0.5, 30*time.Second, now)
	if ok {
		t.Fatalf("a stop 25 points away must REFUSE (tolerance 0.5)")
	}
	if !strings.Contains(reason, "29500") || !strings.Contains(reason, "29475") {
		t.Errorf("reason must quote found vs expected, got %q", reason)
	}
}

// The 0B case exactly: the book cannot be verified, so the override stays
// refused. A stale answer is not a permissive one.
func TestOverrideRefusedWhenTheBookIsStaleOrAbsent(t *testing.T) {
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)

	stale := nt.NewOrderSnapshotCache()
	stale.PutAt(snapWith(nt.NT8Order{
		OrderID: "NT-stop", Type: "stop", StopPrice: 29475, Quantity: 1, State: "Working",
	}), now.Add(-120*time.Second))
	if ok, reason := OverrideAllowedAt(stale, "Sim101", "MNQ", 29475, 0.5, 30*time.Second, now); ok {
		t.Errorf("a stale book must refuse even though it contains the right stop: %q", reason)
	}

	absent := nt.NewOrderSnapshotCache()
	if ok, reason := OverrideAllowedAt(absent, "Sim101", "MNQ", 29475, 0.5, 30*time.Second, now); ok {
		t.Errorf("an absent book must refuse: %q", reason)
	}
}

// The boot line must never assert a source it did not resolve. Before F12 it
// printed "leg4=ledger" as a literal; once the broker answers, that literal is
// a lie printed at every boot.
func TestLeg4SourceLabelTracksReality(t *testing.T) {
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)

	if got := Leg4SourceLabel(nil, "Sim101", "MNQ", now); !strings.Contains(got, "ledger") {
		t.Errorf("no cache → ledger, got %q", got)
	}
	empty := nt.NewOrderSnapshotCache()
	if got := Leg4SourceLabel(empty, "Sim101", "MNQ", now); !strings.Contains(got, "no snapshot yet") {
		t.Errorf("cold start must say why, got %q", got)
	}
	fresh := nt.NewOrderSnapshotCache()
	fresh.PutAt(snapWith(), now.Add(-5*time.Second))
	if got := Leg4SourceLabel(fresh, "Sim101", "MNQ", now); got != "broker" {
		t.Errorf("a fresh book means broker, got %q", got)
	}
	stale := nt.NewOrderSnapshotCache()
	stale.PutAt(snapWith(), now.Add(-10*time.Minute))
	if got := Leg4SourceLabel(stale, "Sim101", "MNQ", now); got != "STALE" {
		t.Errorf("a stale book must not read as broker, got %q", got)
	}
}

// Real case 2026-11-11 08:16 / 11-12 00:20 / 07:03 CT: trader 12345 held an
// armed fill (armed_orders row `filled`, terminal, so the ledger is empty) and
// NT8 held its two protective orders. The leg printed "broker 2 vs ledger 0 —
// MISMATCH", which reads as a ledger fault. The ledger records ENTRY placements
// by design; protective brackets of an open position are a third category.
func TestLeg4ProtectiveBracketsOfOpenPositionAreNamedNotMismatch(t *testing.T) {
	now := time.Date(2026, 11, 12, 13, 0, 0, 0, time.UTC)
	c := nt.NewOrderSnapshotCache()
	c.PutAt(snapWith(
		nt.NT8Order{OrderID: "26ac8d91d38a41539253155507ecf0ef", Action: "sell", Type: "stop", StopPrice: 30000, Quantity: 5, State: "Accepted", OCO: "x"},
		nt.NT8Order{OrderID: "a850b67886904cdc9e3649b56c60f0c2", Action: "sell", Type: "limit", LimitPrice: 30100, Quantity: 5, State: "Working", OCO: "x"},
	), now.Add(-5*time.Second))
	pos := []Leg4Position{{Symbol: "MNQ", Long: true, Qty: 5}}

	leg := Leg4FromBrokerWithPositionsAt(c, "Sim101", "MNQ", 30*time.Second, nil, pos, now)
	if leg.Pass {
		t.Fatalf("working orders at the broker must still FAIL leg 4: %+v", leg)
	}
	if strings.Contains(leg.Detail, "MISMATCH") {
		t.Errorf("protective brackets are not a ledger mismatch: %q", leg.Detail)
	}
	if !strings.Contains(leg.Detail, "protective") || !strings.Contains(leg.Detail, "26ac8d91") {
		t.Errorf("detail must name the brackets as protective: %q", leg.Detail)
	}
}

// FLAT with the same two orders: orphans. Must stay a hard mismatch.
func TestLeg4OrphanOrdersWhileFlatStillMismatch(t *testing.T) {
	now := time.Date(2026, 11, 12, 13, 0, 0, 0, time.UTC)
	c := nt.NewOrderSnapshotCache()
	c.PutAt(snapWith(
		nt.NT8Order{OrderID: "o1", Action: "sell", Type: "stop", StopPrice: 30000, Quantity: 5, State: "Accepted"},
		nt.NT8Order{OrderID: "o2", Action: "sell", Type: "limit", LimitPrice: 30100, Quantity: 5, State: "Working"},
	), now.Add(-5*time.Second))
	leg := Leg4FromBrokerWithPositionsAt(c, "Sim101", "MNQ", 30*time.Second, nil, nil, now)
	if leg.Pass || !strings.Contains(leg.Detail, "MISMATCH") {
		t.Fatalf("flat + broker orders must be MISMATCH: %+v", leg)
	}
}

// Entry-side orders (same side as the position) are never "protective".
func TestLeg4SameSideOrderWithOpenPositionStillMismatch(t *testing.T) {
	now := time.Date(2026, 11, 12, 13, 0, 0, 0, time.UTC)
	c := nt.NewOrderSnapshotCache()
	c.PutAt(snapWith(
		nt.NT8Order{OrderID: "e1", Action: "buy", Type: "limit", LimitPrice: 29900, Quantity: 5, State: "Working"},
	), now.Add(-5*time.Second))
	pos := []Leg4Position{{Symbol: "MNQ", Long: true, Qty: 5}}
	leg := Leg4FromBrokerWithPositionsAt(c, "Sim101", "MNQ", 30*time.Second, nil, pos, now)
	if leg.Pass || !strings.Contains(leg.Detail, "MISMATCH") {
		t.Fatalf("an unexplained same-side order must stay MISMATCH: %+v", leg)
	}
}
