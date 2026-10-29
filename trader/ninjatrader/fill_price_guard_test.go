package ninjatrader

import (
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth/checks"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// m447 (7) pins at the PRODUCTION fill handlers: handleFillInbound (entry fill
// frame), recordClose and recordCloseOrdered (the position_close frame, legacy
// and ordered-worker paths). Each is driven with the real handler, a real store
// and a real server bar cache holding the closed 1m bars of the fill's minute.

type fillGuardFx struct {
	tr     *TCPTrader
	st     *store.Store
	g      *checks.FillGuard
	alerts *[]string
	m      int64 // fill minute (ms)
}

func newFillGuardFx(t *testing.T) *fillGuardFx {
	t.Helper()
	tr, st := newLeg1HookFixture(t)
	var alerts []string
	g := checks.NewFillGuard(checks.DefaultFillParams(), nil)
	g.SetAlert(func(kind, text string) { alerts = append(alerts, kind+" :: "+text) })
	saved := checks.DefaultFills
	checks.DefaultFills = g
	t.Cleanup(func() { checks.DefaultFills = saved })
	m := (time.Now().UTC().UnixMilli() - 3*60_000) / 60_000 * 60_000
	var bars []ntwire.Bar
	for i := int64(-2); i <= 1; i++ {
		bars = append(bars, ntwire.Bar{T: m + i*60_000, O: 29000, H: 29005, L: 28998, C: 29002, V: 10, Final: true})
	}
	tr.server.BarCache().Upsert("MNQ", "1m", bars)
	return &fillGuardFx{tr: tr, st: st, g: g, alerts: &alerts, m: m}
}

func (f *fillGuardFx) at() string { return time.UnixMilli(f.m + 20_000).UTC().Format(time.RFC3339) }

func (f *fillGuardFx) openRow(t *testing.T, sig string) {
	t.Helper()
	now := time.Now().UTC().UnixMilli()
	if err := f.st.Position().CreateOpenPosition(&store.TraderPosition{
		TraderID: "t1", ExchangeType: "ninjatrader", ExchangePositionID: "p-" + sig, Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryQuantity: 1, EntryPrice: 29000, EntryTime: now - 300_000, EntryOrderID: sig, Leverage: 1,
		Status: "OPEN", Source: "armed_entry", Account: "Sim101", CreatedAt: now - 300_000, UpdatedAt: now - 300_000,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFillGuardOrderedExitFarPriceIsCountedAndAlertedButTheExitStillRecords(t *testing.T) {
	f := newFillGuardFx(t)
	f.openRow(t, "sig-x1")
	f.tr.recordCloseOrdered("t1", "ex", "ninjatrader", f.st, ntwire.PositionClosePayload{
		SignalID: "sig-x1", Symbol: "MNQ", PositionSide: "long", Account: "Sim101", Quantity: 1,
		ExitPrice: 29100, ExitReason: "sl", ExitTime: f.at(), Seq: 7,
	})
	if s := f.g.Stats(); s.Far != 1 || s.Judged != 1 || len(*f.alerts) != 1 || !strings.Contains((*f.alerts)[0], "29100.00") {
		t.Fatalf("a 95-point-away exit must be counted + alerted once: %+v %v", s, *f.alerts)
	}
	// NEVER blocks the exit: the close still recorded.
	if open, _ := f.st.Position().GetOpenPositions("t1"); len(open) != 0 {
		t.Fatalf("the exit must still close the position, %d still open", len(open))
	}
}

func TestFillGuardOrderedExitInsideTheBarsIsQuiet(t *testing.T) {
	f := newFillGuardFx(t)
	f.openRow(t, "sig-x2")
	f.tr.recordCloseOrdered("t1", "ex", "ninjatrader", f.st, ntwire.PositionClosePayload{
		SignalID: "sig-x2", Symbol: "MNQ", PositionSide: "long", Account: "Sim101", Quantity: 1,
		ExitPrice: 29004.25, ExitReason: "tp", ExitTime: f.at(), Seq: 8,
	})
	if s := f.g.Stats(); s.Far != 0 || s.Judged != 1 || len(*f.alerts) != 0 {
		t.Fatalf("a real print inside the bars is quiet: %+v %v", s, *f.alerts)
	}
}

func TestFillGuardLegacyRecordCloseIsJudgedToo(t *testing.T) {
	f := newFillGuardFx(t)
	f.openRow(t, "sig-x3")
	f.tr.recordClose("t1", "ex", "ninjatrader", f.st, store.NewPositionBuilder(f.st.Position()), ntwire.PositionClosePayload{
		SignalID: "sig-x3", Symbol: "MNQ", PositionSide: "long", Account: "Sim101", Quantity: 1,
		ExitPrice: 28900, ExitReason: "sl", ExitTime: f.at(), Seq: 9,
	})
	if s := f.g.Stats(); s.Far != 1 || len(*f.alerts) != 1 {
		t.Fatalf("the legacy close handler must judge the exit price: %+v %v", s, *f.alerts)
	}
}

func TestFillGuardEntryFillFrameFarPriceIsCountedAndTheFillStillRecords(t *testing.T) {
	f := newFillGuardFx(t)
	fill := ntwire.FillPayload{SignalID: "sig-e1", Symbol: "MNQ", Account: "Sim101", Status: "filled",
		FillPrice: 29500, FillTime: f.at(), Side: "buy", Quantity: 1, SlippageTicks: 1}
	f.tr.handleFillInbound(fill)
	if s := f.g.Stats(); s.Far != 1 || len(*f.alerts) != 1 {
		t.Fatalf("an entry fill 495 points off the bars: %+v %v", s, *f.alerts)
	}
	f.tr.mu.Lock()
	got := f.tr.lastFill
	f.tr.mu.Unlock()
	if got.SignalID != "sig-e1" || got.FillPrice != 29500 {
		t.Fatalf("the fill must be recorded exactly as before, got %+v", got)
	}
	// a rejected or duplicate reply is not a fill and is not judged
	f.tr.handleFillInbound(ntwire.FillPayload{SignalID: "sig-e2", Symbol: "MNQ", Account: "Sim101", Status: "rejected", FillPrice: 1})
	if s := f.g.Stats(); s.Judged != 1 {
		t.Fatalf("a rejection is not a fill: %+v", s)
	}
}

func TestFillGuardSecondFarFillPausesThroughTheRegisteredCheck(t *testing.T) {
	f := newFillGuardFx(t)
	for i, px := range []float64{29500, 28000} {
		f.tr.handleFillInbound(ntwire.FillPayload{SignalID: "sig-p" + string(rune('a'+i)), Symbol: "MNQ", Account: "Sim101",
			Status: "filled", FillPrice: px, FillTime: f.at(), Side: "buy", Quantity: 1})
	}
	blocked, why, err := f.g.Check()("MNQ", time.Now())
	if err != nil || !blocked || !strings.Contains(why, "2 far fill(s) this session") {
		t.Fatalf("two far fills pause: blocked=%v why=%q err=%v", blocked, why, err)
	}
}
