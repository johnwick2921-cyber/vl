package ninjatrader

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// W117 a3 — production-call-site pin: a SECOND connection holds the write lock
// while a position_close frame arrives. The close must end up APPLIED (after
// the bounded busy retry) or parked + priced + receipt-persisted — NEVER lost.
//
// RED = today's code: ApplyNT8Exit's deferred tx reads first and BUSYs at the
// upgrade instantly; recordCloseOrdered logs and returns — no fill, no park,
// no receipt, and RetryPendingNT8Exits has nothing to apply later.
func TestBusyCloseFrameIsNeverDropped(t *testing.T) {
	store.SetImmediateTxBusyTimeoutForTest(120) // 5 tries ≈ 0.6s busy + 0.75s backoff
	defer store.ResetImmediateTxBusyTimeoutForTest()

	// look twice C: the parked receipt must also reach the own-reverse watch.
	unapplied := make(chan store.NT8ExitReceipt, 4)
	prevHook := OnExitUnapplied
	OnExitUnapplied = func(_ string, r store.NT8ExitReceipt) { unapplied <- r }
	t.Cleanup(func() { OnExitUnapplied = prevHook })

	f := newOrderedOnceFixture(t)
	tr, st := f.tr, f.st
	tr.StartCloseSync("t1", "ex", "ninjatrader", st) // the legacy advisory consumer runs too
	unreg, err := tr.InstallOrderedExecutions("t1", "ex", "ninjatrader", st, nil)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	defer unreg()

	now := time.Now().UTC().UnixMilli()
	pos := &store.TraderPosition{
		TraderID: "t1", ExchangeType: "ninjatrader", ExchangePositionID: "p-busy",
		Symbol: "MNQ", Side: "LONG", Quantity: 1, EntryQuantity: 1,
		EntryPrice: 29350, EntryTime: now - 60_000, EntryOrderID: "sig-busy",
		Leverage: 1, Status: "OPEN", Source: "armed_entry", Account: "Sim101",
		CreatedAt: now - 60_000, UpdatedAt: now - 60_000,
	}
	if err := st.Position().CreateOpenPosition(pos); err != nil {
		t.Fatal(err)
	}

	// The contention: a second connection holds the write lock past the
	// worker's 5-try busy budget (120ms×5 + 750ms sleeps ≈ 1.35s) — the loop
	// exhausts, the park lands, and the persist's own full busy wait blocks
	// until this holder releases at 4s.
	ctx := context.Background()
	sqlDB, err := st.GormDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	holder, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holder.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("holder BEGIN IMMEDIATE: %v", err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(4 * time.Second)
		_, _ = holder.ExecContext(ctx, "COMMIT")
		_ = holder.Close()
		close(released)
	}()
	defer func() {
		select {
		case <-released:
		default:
			_, _ = holder.ExecContext(ctx, "COMMIT")
			_ = holder.Close()
		}
	}()

	if err := ntwire.WriteFrame(f.conn, ntwire.FramePositionClose, ntwire.PositionClosePayload{
		SignalID: "sig-busy", Symbol: "MNQ", PositionSide: "long", Account: "Sim101",
		Quantity: 1, ExitPrice: 29360, ExitReason: "sl", ExitTime: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}

	// The retry loop must exhaust while the holder still holds, and the park
	// must land: a receipt persisted with applied=false and ZERO fills.
	var receipt store.NT8ExitReceipt
	deadline := time.Now().Add(6 * time.Second)
	for {
		if n := f.countFills("sig-busy"); n != 0 {
			t.Fatalf("no fill may exist while the write-lock holder is still up, got %d", n)
		}
		if e := st.GormDB().Where("account = ? AND applied = ?", "Sim101", false).First(&receipt).Error; e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("today's code returns on the FIRST busy error — no receipt persisted, no priced park: the close is LOST (the receipt row never appeared)")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// MUTANT (shown RED): drop notifyExitUnapplied after SavePendingExit.
	select {
	case r := <-unapplied:
		if r.SignalID != "sig-busy" || r.Quantity != 1 {
			t.Fatalf("the own-reverse watch got receipt %+v", r)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("a busy-parked receipt never reached the own-reverse watch")
	}

	// F-A contract: the broker's REAL price is parked for reconcile's orphan
	// close (never exit=entry pnl=0), and the flat signal is dropped.
	if price, ok := takePricedClose("Sim101", "MNQ", "LONG", time.Now().UnixMilli()); !ok || price != 29360 {
		t.Fatalf("the broker's real exit price must be parked for reconcile's orphan close, got ok=%v price=%.2f", ok, price)
	}
	row, err := st.Position().GetOpenPositionBySymbol("t1", "MNQ", "LONG")
	if err != nil || row == nil || row.Quantity != 1 {
		t.Fatalf("the row must stay OPEN with residual 1 while parked, got %+v err=%v", row, err)
	}

	// The holder is released by now; the persisted receipt applies on retry —
	// the never-dropped close lands with exactly one fill.
	tr.RetryPendingNT8Exits(st)
	deadline = time.Now().Add(5 * time.Second)
	for f.countFills("sig-busy") == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := f.countFills("sig-busy"); n != 1 {
		t.Fatalf("the parked receipt must apply later with exactly ONE exit fill, got %d", n)
	}
	var closed store.TraderPosition
	if err := st.GormDB().First(&closed, pos.ID).Error; err != nil || closed.Status != "CLOSED" {
		t.Fatalf("the applied receipt must close the row, got %+v err=%v", closed, err)
	}
	var left []store.NT8ExitReceipt
	st.GormDB().Where("account = ? AND applied = ?", "Sim101", false).Find(&left)
	if len(left) != 0 {
		t.Fatalf("no receipt may stay pending after the retry applied, got %d", len(left))
	}
}

// W117 a3 — the retry loop itself is the normal success path for BRIEF
// contention. The holder releases INSIDE the retry budget (~250ms), so a
// no-retry build would park; the retry build applies ON THE WORKER with the
// park counter untouched. RED = ntExitBusyRetries 4 → 0: the single busy
// attempt parks instead of applying.
func TestBriefBusyCloseAppliesOnTheWorkerAfterRetries(t *testing.T) {
	store.SetImmediateTxBusyTimeoutForTest(120)
	defer store.ResetImmediateTxBusyTimeoutForTest()

	f := newOrderedOnceFixture(t)
	tr, st := f.tr, f.st
	tr.StartCloseSync("t1", "ex", "ninjatrader", st)
	unreg, err := tr.InstallOrderedExecutions("t1", "ex", "ninjatrader", st, nil)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	defer unreg()

	now := time.Now().UTC().UnixMilli()
	pos := &store.TraderPosition{
		TraderID: "t1", ExchangeType: "ninjatrader", ExchangePositionID: "p-brief",
		Symbol: "MNQ", Side: "LONG", Quantity: 1, EntryQuantity: 1,
		EntryPrice: 29350, EntryTime: now - 60_000, EntryOrderID: "sig-brief",
		Leverage: 1, Status: "OPEN", Source: "armed_entry", Account: "Sim101",
		CreatedAt: now - 60_000, UpdatedAt: now - 60_000,
	}
	if err := st.Position().CreateOpenPosition(pos); err != nil {
		t.Fatal(err)
	}

	parksBefore := counterValue(telemetry.NT8ExitBusyParksTotal)

	// The holder releases at ~250ms — inside the retry budget (attempt 1 burns
	// 120ms busy, the loop sleeps 50ms, attempt 2 blocks ~80ms and applies).
	ctx := context.Background()
	sqlDB, err := st.GormDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	holder, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holder.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("holder BEGIN IMMEDIATE: %v", err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(250 * time.Millisecond)
		_, _ = holder.ExecContext(ctx, "COMMIT")
		_ = holder.Close()
		close(released)
	}()
	defer func() {
		select {
		case <-released:
		default:
			_, _ = holder.ExecContext(ctx, "COMMIT")
			_ = holder.Close()
		}
	}()

	if err := ntwire.WriteFrame(f.conn, ntwire.FramePositionClose, ntwire.PositionClosePayload{
		SignalID: "sig-brief", Symbol: "MNQ", PositionSide: "long", Account: "Sim101",
		Quantity: 1, ExitPrice: 29360, ExitReason: "sl", ExitTime: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for f.countFills("sig-brief") == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := f.countFills("sig-brief"); n != 1 {
		t.Fatalf("the close must end APPLIED on the worker after the retry loop, got %d fills — RED: ntExitBusyRetries=0 parks every brief contention", n)
	}
	var closed store.TraderPosition
	if err := st.GormDB().First(&closed, pos.ID).Error; err != nil || closed.Status != "CLOSED" {
		t.Fatalf("the applied close must close the row, got %+v err=%v", closed, err)
	}
	var left []store.NT8ExitReceipt
	st.GormDB().Where("account = ? AND applied = ?", "Sim101", false).Find(&left)
	if len(left) != 0 {
		t.Fatalf("the retry path must not park a receipt, got %d", len(left))
	}
	if _, ok := takePricedClose("Sim101", "MNQ", "LONG", time.Now().UnixMilli()); ok {
		t.Fatal("the retry path must not park a priced close for reconcile")
	}
	if delta := counterValue(telemetry.NT8ExitBusyParksTotal) - parksBefore; delta != 0 {
		t.Fatalf("the busy-park counter must stay 0 on the retry path, delta %v", delta)
	}
}

// counterValue reads a prometheus.Counter's current value directly through
// client_model — the same byte stream testutil.ToFloat64 walks. testutil is a
// test-only dependency whose transitive module (kylelemons/godebug) would force
// a go.mod change; go.mod is brand-pinned and must stay byte-identical to dev
// (CTO ruling 2026-09-25, PR #227).
func counterValue(c prometheus.Counter) float64 {
	var m dto.Metric
	_ = c.Write(&m)
	return m.GetCounter().GetValue()
}
