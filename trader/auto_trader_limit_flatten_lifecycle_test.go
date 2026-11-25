package trader

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"vl/store"
)

type limitFlattenRecorder struct {
	*MockTrader
	limitErr, closeErr      error
	limits, closes, cancels int
	closeStarted            chan struct{}
	closeRelease            chan struct{}
	// cancelOrders records every cancel_order by signal id (m471 L2-3); cancels
	// above counts CancelStopOrders only.
	cancelOrders []string
	limitPx      []float64
}

func (r *limitFlattenRecorder) CloseWithLimit(_ string, _ string, _ float64, px float64) (map[string]interface{}, error) {
	r.limits++
	r.limitPx = append(r.limitPx, px)
	if r.limitErr != nil {
		return nil, r.limitErr
	}
	// Same shape as TCPTrader.sendCloseAt: the close's own signal id.
	return map[string]interface{}{"status": "close_submitted", "signal_id": fmt.Sprintf("close-%d", r.limits)}, nil
}
func (r *limitFlattenRecorder) CancelOrder(signalID string) error {
	r.cancelOrders = append(r.cancelOrders, signalID)
	return nil
}
func (r *limitFlattenRecorder) CloseLong(string, float64) (map[string]interface{}, error) {
	r.closes++
	if r.closeStarted != nil {
		close(r.closeStarted)
		<-r.closeRelease
	}
	return nil, r.closeErr
}
func (r *limitFlattenRecorder) CloseShort(string, float64) (map[string]interface{}, error) {
	r.closes++
	if r.closeStarted != nil {
		close(r.closeStarted)
		<-r.closeRelease
	}
	return nil, r.closeErr
}
func (r *limitFlattenRecorder) CancelStopOrders(string) error { r.cancels++; return nil }

func limitFlattenFixture(t *testing.T, side string) (*AutoTrader, *limitFlattenRecorder, *store.TraderPosition) {
	t.Helper()
	at, st := resetTrader(t, store.StrategyConfig{})
	at.config.LimitCloseTicks = 4
	at.config.LimitCloseMarketAfterS = 3600
	r := &limitFlattenRecorder{MockTrader: &MockTrader{}}
	at.trader = r
	p := &store.TraderPosition{TraderID: at.id, Symbol: "MNQ", Side: side, Account: "Sim101", EntryOrderID: "entry-original", EntryPrice: 30000, EntryTime: time.Now().UnixMilli(), Quantity: 1, Status: "OPEN"}
	if err := st.Position().Create(p); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(at.Stop)
	return at, r, p
}

// Dispatch the exact callback used by time.AfterFunc without wall-clock sleeps.
func fireLimitFlatten(t *testing.T, at *AutoTrader, p *store.TraderPosition) *pendingLimitFlatten {
	t.Helper()
	pending := at.limitFlattens[p.ID]
	if pending == nil {
		t.Fatal("production flatten did not schedule fallback")
	}
	pending.timer.Stop()
	at.finishLimitFlatten(pending)
	return pending
}
func TestLimitFlattenPreservesProtectionOnCloseFailure(t *testing.T) {
	for _, side := range []string{"LONG", "SHORT"} {
		for _, immediate := range []bool{false, true} {
			t.Run(side+map[bool]string{false: " delayed", true: " immediate"}[immediate], func(t *testing.T) {
				at, r, p := limitFlattenFixture(t, side)
				r.closeErr = errors.New("close refused")
				if immediate {
					r.limitErr = errors.New("limit refused")
				}
				at.flattenPosition(p, "test")
				if !immediate {
					fireLimitFlatten(t, at, p)
				}
				if r.closes != 1 || r.cancels != 0 {
					t.Fatalf("closes=%d cancels=%d", r.closes, r.cancels)
				}
			})
		}
	}
}
func TestLimitFlattenOriginalPositionClosesOnce(t *testing.T) {
	at, r, p := limitFlattenFixture(t, "LONG")
	at.flattenPosition(p, "test")
	pending := fireLimitFlatten(t, at, p)
	at.finishLimitFlatten(pending)
	if r.closes != 1 || r.cancels != 1 {
		t.Fatalf("closes=%d cancels=%d", r.closes, r.cancels)
	}
}
func TestLimitFlattenRejectsReplacement(t *testing.T) {
	for _, mode := range []string{"closed row and new position", "changed entry lineage", "caller mutation", "ambiguous open rows"} {
		t.Run(mode, func(t *testing.T) {
			at, r, p := limitFlattenFixture(t, "LONG")
			at.flattenPosition(p, "test")
			db := at.store.GormDB()
			if mode == "changed entry lineage" {
				if err := db.Model(&store.TraderPosition{}).Where("id = ?", p.ID).Update("entry_order_id", "replacement").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if mode != "ambiguous open rows" {
					if err := db.Model(&store.TraderPosition{}).Where("id = ?", p.ID).Update("status", "CLOSED").Error; err != nil {
						t.Fatal(err)
					}
				}
				replacement := *p
				replacement.ID = 0
				replacement.EntryOrderID = "replacement"
				if err := at.store.Position().Create(&replacement); err != nil {
					t.Fatal(err)
				}
				if mode == "caller mutation" {
					*p = replacement
				}
			}
			// Use the scheduled original identity, even when the caller's object changed.
			for _, pending := range at.limitFlattens {
				pending.timer.Stop()
				at.finishLimitFlatten(pending)
			}
			if r.closes != 0 || r.cancels != 0 {
				t.Fatalf("replacement touched: closes=%d cancels=%d", r.closes, r.cancels)
			}
		})
	}
}
func TestLimitFlattenStopInvalidatesPendingCallback(t *testing.T) {
	at, r, p := limitFlattenFixture(t, "LONG")
	at.isRunning = true
	at.setupStopMonitorForTest()
	at.flattenPosition(p, "test")
	pending := at.limitFlattens[p.ID]
	at.Stop()
	at.finishLimitFlatten(pending) // Simulate an already-queued timer callback.
	at.flattenPosition(p, "after stop")
	// m471 L2 review-1: the record is KEPT across Stop (the -lx may still rest —
	// the AddOn ignores cancel_order for it), with its timer retired.
	if r.limits != 1 || r.closes != 0 || r.cancels != 0 || at.limitFlattens[p.ID] != pending || !pending.fallbackRetired {
		t.Fatalf("stop leaked work: %+v", r)
	}
	// Old callback remains invalid even if a subsequent Run reopens scheduling.
	at.limitFlattenMu.Lock()
	at.limitFlattenStopped = false
	at.limitFlattenMu.Unlock()
	at.flattenPosition(p, "new run") // escalates the kept exit to ONE market close, no second limit
	if r.limits != 1 || r.closes != 1 {
		t.Fatalf("new run: limits=%d closes=%d, want the kept exit escalated once", r.limits, r.closes)
	}
	at.finishLimitFlatten(pending)
	if r.closes != 1 {
		t.Fatal("old generation closed new run position")
	}
}
func TestLimitFlattenRefusesMissingDurableIdentity(t *testing.T) {
	at, r, p := limitFlattenFixture(t, "LONG")
	p.ID = 0
	at.flattenPosition(p, "test")
	if len(at.limitFlattens) != 0 || r.closes != 0 {
		t.Fatal("scheduled fallback without durable identity")
	}
}

func TestLimitFlattenRunningTimerJoinsStop(t *testing.T) {
	at, r, p := limitFlattenFixture(t, "LONG")
	r.closeStarted = make(chan struct{})
	r.closeRelease = make(chan struct{})
	at.flattenPosition(p, "timer")
	at.limitFlattenMu.Lock()
	at.limitFlattens[p.ID].timer.Reset(0)
	at.limitFlattenMu.Unlock()
	select {
	case <-r.closeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timer did not execute")
	}
	stopped := make(chan struct{})
	go func() { at.Stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("Stop returned while callback was dispatching")
	default:
	}
	close(r.closeRelease)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not join callback")
	}
	// m471 L2 review-1: the sent market close keeps its record (and its hold)
	// across Stop.
	if r.closes != 1 || r.cancels != 1 || at.limitFlattens[p.ID] == nil || at.limitFlattens[p.ID].marketSentAt.IsZero() {
		t.Fatalf("incomplete callback lifecycle: %+v", r)
	}
}
