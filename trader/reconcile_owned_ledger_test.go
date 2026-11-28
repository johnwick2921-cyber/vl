package trader

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"vl/logger"
	"vl/store"
)

// ── W1b E10 — the pre-open reconcile (c) answers a LEDGER question with the
// ledger: a stopped trader's fresh fill, another trader's OPEN row on this
// account, and every unreadable read EXPLAIN the held position — never flatten
// it ───────────────────────────────────────────────────────────────────────

// warnCapture collects WARN lines emitted while a test runs.
type warnCapture struct {
	mu    sync.Mutex
	lines []string
}

func (h *warnCapture) Levels() []logrus.Level { return []logrus.Level{logrus.WarnLevel} }
func (h *warnCapture) Fire(e *logrus.Entry) error {
	h.mu.Lock()
	h.lines = append(h.lines, e.Message)
	h.mu.Unlock()
	return nil
}
func (h *warnCapture) has(sub string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, l := range h.lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func captureWarns(t *testing.T) *warnCapture {
	t.Helper()
	prev := logrus.LevelHooks{}
	for lvl, hs := range logger.Log.Hooks {
		prev[lvl] = append([]logrus.Hook(nil), hs...)
	}
	h := &warnCapture{}
	logger.Log.AddHook(h)
	t.Cleanup(func() { logger.Log.ReplaceHooks(prev) })
	return h
}

// refusedNotFlattened drives the production pre-open reconcile and asserts the
// held SHORT is explained (⛔ refusal naming want), never flattened.
func (w *reconcileWire) refusedNotFlattened(t *testing.T, want string) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- w.at.reconcileBeforeOpenNT("MNQ", "long") }()
	// A refusal returns at once; a flatten waits for its fill (35 s). Once it has
	// returned nothing more can be sent, so the wire is read then.
	var err error
	returned := true
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		returned = false
	}
	if w.closeSent(300 * time.Millisecond) {
		w.s.SeedPositionsForTest("Sim101", nil)
		if !returned {
			<-done
		}
		t.Fatal("the held position was FLATTENED")
	}
	if !returned {
		t.Fatal("reconcile never returned")
	}
	if !errors.Is(err, errPositionOwned) || !strings.Contains(err.Error(), want) {
		t.Fatalf("the held position must be explained (%q): %v", want, err)
	}
}

// (iii) is ledger-wide: a trader that STOPPED (not in the running registry)
// between its fill and our open still owns that fill; the position is its.
// This replaces the W0b limit pin TestReconcileFreshFillOfAStoppedTraderIsNotSeen.
func TestReconcileFreshFillOfAStoppedTraderExplainsThePosition(t *testing.T) {
	w := newReconcileWire(t)
	w.otherRunningTraderWithFreshFill(t, false) // NOT registered: stopped
	w.refusedNotFlattened(t, "not yet materialized")
}

// The ledger-wide fill read still honours the window: a stopped trader's fill
// OLDER than twice the untracked grace explains nothing — flattened as before.
func TestReconcileOldFillOfAStoppedTraderStillFlattens(t *testing.T) {
	w := newReconcileWire(t)
	w.otherRunningTraderWithFreshFill(t, false)
	if err := w.st.ArmedOrders().DB().Model(&store.ArmedOrderDB{}).Where("trader_id = ?", "reconcile-other").
		UpdateColumn("updated_at", time.Now().Add(-10*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.at.reconcileBeforeOpenNT("MNQ", "long") }()
	if !w.closeSent(3 * time.Second) {
		t.Fatal("an old fill of a stopped trader must not explain the position")
	}
	w.s.SeedPositionsForTest("Sim101", nil)
	if err := <-done; err != nil {
		t.Fatalf("after the confirmed flatten the open proceeds: %v", err)
	}
}

// (ii') an OPEN trader_positions row of ANOTHER trader (running or not) on this
// account, instrument and side explains the held position, however old — and
// a WARN names the blocking row so a stale one can be found.
func TestReconcileAnotherTradersOpenRowExplainsThePosition(t *testing.T) {
	w := newReconcileWire(t)
	warns := captureWarns(t)
	old := time.Now().Add(-30 * time.Minute).UnixMilli()
	row := &store.TraderPosition{TraderID: "reconcile-stopped", ExchangeID: "nt", ExchangeType: "ninjatrader",
		ExchangePositionID: "reconcile_MNQ_SHORT_1", Symbol: "MNQ", Side: "SHORT", Quantity: 1, EntryPrice: 29000,
		EntryTime: old, Status: "OPEN", Source: "reconcile", PlanID: store.PlanUnresolvable, Account: "Sim101", CreatedAt: old, UpdatedAt: old}
	if err := w.st.Position().CreateOpenPosition(row); err != nil { // the materializer's writer
		t.Fatal(err)
	}
	w.refusedNotFlattened(t, fmt.Sprintf("position #%d", row.ID))
	if !warns.has(fmt.Sprintf("OPEN row #%d of trader reconcile-stopped", row.ID)) {
		t.Fatalf("a blocking OPEN row of another trader must be named in a WARN (row #%d)", row.ID)
	}
}

// Another trader's OPEN row on a DIFFERENT account explains nothing.
func TestReconcileAnotherAccountsOpenRowDoesNotExplain(t *testing.T) {
	w := newReconcileWire(t)
	row := &store.TraderPosition{TraderID: "reconcile-elsewhere", ExchangeID: "nt", ExchangeType: "ninjatrader",
		ExchangePositionID: "elsewhere_1", Symbol: "MNQ", Side: "SHORT", Quantity: 1, EntryPrice: 29000,
		EntryTime: time.Now().UnixMilli(), Status: "OPEN", Account: "Sim102"}
	if err := w.st.Position().CreateOpenPosition(row); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.at.reconcileBeforeOpenNT("MNQ", "long") }()
	if !w.closeSent(3 * time.Second) {
		t.Fatal("a row on another account must not explain this account's position")
	}
	w.s.SeedPositionsForTest("Sim101", nil)
	if err := <-done; err != nil {
		t.Fatalf("after the confirmed flatten the open proceeds: %v", err)
	}
}

// Every read in ledgerExplainsPosition that FAILS explains the position
// (fail-closed): an unreadable ledger is never read as "no owner".
func TestReconcileLedgerReadErrorIsExplainedNeverFlattened(t *testing.T) {
	for _, table := range []string{"armed_orders", "picture_htf_opportunities", "trader_positions"} {
		t.Run(table, func(t *testing.T) {
			w := newReconcileWire(t)
			if err := w.st.GormDB().Exec(fmt.Sprintf("ALTER TABLE %s RENAME TO %s_unreadable", table, table)).Error; err != nil {
				t.Fatal(err)
			}
			w.refusedNotFlattened(t, "unreadable")
		})
	}
}

// W1b E10 verifier repair 6 — the LATER reads in ledgerExplainsPosition each
// have their own fail-closed guard, pinned one by one: a column only that read
// needs is renamed away, so the earlier reads succeed and exactly that read
// fails. Every such failure EXPLAINS the held position and names its read —
// never a flatten.
func TestReconcileLaterLedgerReadErrorIsExplainedNeverFlattened(t *testing.T) {
	for _, tc := range []struct{ name, table, column, want string }{
		{"ListFilledSinceAllTraders", "armed_orders", "updated_at", "(armed filled: "},
		{"PictureHtfFilledSinceAll", "picture_htf_opportunities", "updated_at", "(picture filled: "},
		{"ListFilled_per_id", "armed_orders", "trader_id", "(armed filled of reconcile-owned: "},
		{"PictureHtfByTrader", "picture_htf_opportunities", "trader_id", "(picture ledger of reconcile-owned: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newReconcileWire(t)
			if err := w.st.GormDB().Exec(fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s_unreadable", tc.table, tc.column, tc.column)).Error; err != nil {
				t.Fatal(err)
			}
			w.refusedNotFlattened(t, tc.want)
		})
	}
}
