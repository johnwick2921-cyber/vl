package ninjatrader

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── W-EXEC-TRUTH W0 (b) — the entry latch, driven through the REAL entry
// functions over a started loopback server ─────────────────────────────────

type latchWire struct {
	s      *ntwire.TCPServer
	conn   net.Conn
	frames chan ntwire.FrameType
}

func newLatchWire(t *testing.T) *latchWire {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "latch.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel(); _ = st.Close() })
	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	frames := make(chan ntwire.FrameType, 64)
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			frames <- env.Type
		}
	}()
	top := ntwire.MinAddonBuildPictureHtf
	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: top}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400 && !ntwire.FarSideProven(s.FarSideBuildID(), top); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	drain(frames)
	return &latchWire{s: s, conn: conn, frames: frames}
}

func (w *latchWire) trader(t *testing.T) *TCPTrader {
	t.Helper()
	tr := NewTCPTrader(w.s, "MNQ", "Sim101")
	for _, side := range []string{"long", "LONG"} {
		_ = tr.SetStopLoss("MNQ", side, 1, 29000)
		_ = tr.SetTakeProfit("MNQ", side, 1, 29200)
	}
	return tr
}

// fill confirms a signal as the AddOn would, clearing the broker's pending entry.
func (w *latchWire) fill(t *testing.T, sid string) {
	t.Helper()
	if err := ntwire.WriteFrame(w.conn, ntwire.FrameFill, ntwire.FillPayload{SignalID: sid, Symbol: "MNQ", Account: "Sim101", Side: "long", Quantity: 1, FillPrice: 29100, Status: "filled", FillTime: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
}

type latchFixture struct {
	live, unverifiable bool
	ledger             []string
	ledgerErr          error
	now                time.Time
	bookCalls          atomic.Int32
}

func (f *latchFixture) source() *EntryLatchSource {
	return &EntryLatchSource{
		Book: func(time.Time) EntryLatchBookVerdict {
			f.bookCalls.Add(1)
			if f.unverifiable {
				return EntryLatchBookVerdict{Verifiable: false, Detail: "book age 3m exceeds the 1m bound"}
			}
			return EntryLatchBookVerdict{Verifiable: true, Live: f.live, Detail: "working entry ord-7"}
		},
		Ledgers: func() ([]string, error) { return f.ledger, f.ledgerErr },
		Now:     func() time.Time { return f.now },
	}
}

// Every entry function the four paths use — each one is latched.
func latchedCalls(tr *TCPTrader, stamp func(string) error) map[string]func() error {
	return map[string]func() error{
		"placeEntry": func() error { _, err := tr.OpenLong("MNQ", 1, 1); return err },
		"PlaceLimitEntry": func() error {
			_, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200, stamp)
			return err
		},
		"PlaceStopEntry": func() error {
			_, err := tr.PlaceStopEntry("MNQ", "long", 1, 29150, 29000, 29300, 0, 0, stamp)
			return err
		},
	}
}

// A live book, an unverifiable book, an open ledger row and an unreadable
// ledger each refuse ALL FOUR entry functions: typed, before the ledger stamp,
// nothing on the wire.
func TestEntryLatchRefusesAllFourEntryFunctionsOnEachClause(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fx     *latchFixture
		reason string
	}{
		{"live_book", &latchFixture{live: true}, "working_entry_or_position"},
		{"unverifiable_book", &latchFixture{unverifiable: true}, "book_unverifiable"},
		{"open_ledger_row", &latchFixture{ledger: []string{"armed#169"}}, "ledger_open"},
		{"unreadable_ledger", &latchFixture{ledgerErr: errors.New("db locked")}, "ledger_unreadable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newLatchWire(t)
			tr := w.trader(t)
			tc.fx.now = time.Now()
			tr.SetEntryLatchSource(tc.fx.source())
			var stamped atomic.Int32
			stamp := func(string) error { stamped.Add(1); return nil }
			for name, call := range latchedCalls(tr, stamp) {
				err := call()
				if !IsEntryLatched(err) || !strings.Contains(err.Error(), "one_entry_latch:"+tc.reason) {
					t.Fatalf("%s: want a one_entry_latch:%s refusal, got %v", name, tc.reason, err)
				}
			}
			if stamped.Load() != 0 {
				t.Fatalf("the ledger stamp ran %d time(s) under a latch refusal", stamped.Load())
			}
			if waitFrame(w.frames, ntwire.FrameSignal, 150*time.Millisecond) {
				t.Fatal("a signal reached the wire under a latch refusal")
			}
		})
	}
}

// An entry sent and not yet filled keeps the latch closed for the NEXT entry
// on the same account|symbol (the AI path has no ledger — this is its record);
// once it fills, the 60 s recent-send window still holds; after it, the next
// entry goes.
func TestEntryLatchQueuedThenRecentThenOpen(t *testing.T) {
	w := newLatchWire(t)
	tr := w.trader(t)
	fx := &latchFixture{now: time.Now()}
	tr.SetEntryLatchSource(fx.source())
	order, err := tr.OpenLong("MNQ", 1, 1)
	if err != nil {
		t.Fatalf("the first entry must go: %v", err)
	}
	sid, _ := order["signal_id"].(string)
	if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200); !IsEntryLatched(err) || !strings.Contains(err.Error(), "queued_entry") {
		t.Fatalf("an unfilled entry must hold the latch (queued_entry), got %v", err)
	}
	w.fill(t, sid)
	if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200); !IsEntryLatched(err) || !strings.Contains(err.Error(), "recent_send") {
		t.Fatalf("an entry sent inside the window must hold the latch (recent_send), got %v", err)
	}
	fx.now = fx.now.Add(EntryLatchRecentWindow + time.Second)
	if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200); err != nil {
		t.Fatalf("after the window a clean book admits the next entry: %v", err)
	}
}

// The latch spans traders: a second TCPTrader on the SAME server, account and
// symbol is refused inside the window (two traders, one account — P5).
func TestEntryLatchSpansTradersOnOneAccount(t *testing.T) {
	w := newLatchWire(t)
	a, b := w.trader(t), w.trader(t)
	fx := &latchFixture{now: time.Now()}
	a.SetEntryLatchSource(fx.source())
	b.SetEntryLatchSource(fx.source())
	order, err := a.OpenLong("MNQ", 1, 1)
	if err != nil {
		t.Fatalf("trader A's entry must go: %v", err)
	}
	sid, _ := order["signal_id"].(string)
	w.fill(t, sid)
	if _, err := b.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200); !IsEntryLatched(err) || !strings.Contains(err.Error(), "recent_send") {
		t.Fatalf("trader B on the same account|symbol must be refused inside the window, got %v", err)
	}
}

// The latch runs BEFORE B3: a latch refusal does not consume the dedupe slot,
// so the same entry is admitted once the book clears.
func TestEntryLatchRefusalDoesNotConsumeTheDedupeSlot(t *testing.T) {
	w := newLatchWire(t)
	tr := w.trader(t)
	fx := &latchFixture{live: true, now: time.Now()}
	tr.SetEntryLatchSource(fx.source())
	if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200); !IsEntryLatched(err) {
		t.Fatalf("fixture: must be latched, got %v", err)
	}
	fx.live = false
	if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200); err != nil {
		t.Fatalf("the same entry must be admitted once the book clears (B3 untouched): %v", err)
	}
}

// Unwired = allow, and EntryLatchWired READS the wiring (the boot line's source).
func TestEntryLatchUnwiredAllowsAndReportsUnwired(t *testing.T) {
	w := newLatchWire(t)
	tr := w.trader(t)
	if tr.EntryLatchWired() {
		t.Fatal("a fresh TCPTrader must report the latch UNWIRED")
	}
	if _, err := tr.OpenLong("MNQ", 1, 1); err != nil {
		t.Fatalf("unwired must allow: %v", err)
	}
	tr.SetEntryLatchSource((&latchFixture{now: time.Now()}).source())
	if !tr.EntryLatchWired() {
		t.Fatal("a wired TCPTrader must report the latch wired")
	}
}

// N7 part 1 — ForgetPending (the confirmed-cancel hook) drops the queued-entry
// marker. A cancelled entry otherwise holds the latch as "queued_entry" until a
// fill, a rejection, or the 45s stale sweep — none of which a cancel produces —
// so the entry that follows the cancel would be refused and lost. The pin
// proves the pending marker outlives the 60s recent-send window on its own, and
// that ForgetPending is what clears it.
func TestForgetPendingClearsQueuedEntry(t *testing.T) {
	w := newLatchWire(t)
	tr := w.trader(t)
	fx := &latchFixture{now: time.Now()}
	tr.SetEntryLatchSource(fx.source())
	order, err := tr.OpenLong("MNQ", 1, 1)
	if err != nil {
		t.Fatalf("the first entry must go: %v", err)
	}
	sid, _ := order["signal_id"].(string)
	if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200); !IsEntryLatched(err) || !strings.Contains(err.Error(), "queued_entry") {
		t.Fatalf("an unfilled entry must hold the latch (queued_entry), got %v", err)
	}
	// Past the recent-send window the pending marker ALONE still holds the
	// latch: t.pending is cleared only by fill/reject, the 45s sweep, or
	// ForgetPending — never by a cancel.
	fx.now = fx.now.Add(EntryLatchRecentWindow + time.Second)
	if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200); !IsEntryLatched(err) || !strings.Contains(err.Error(), "queued_entry") {
		t.Fatalf("the pending marker must outlive the recent window (queued_entry), got %v", err)
	}
	// The confirmed cancel settles the entry — the next one must be admitted.
	tr.ForgetPending(sid)
	if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200); err != nil {
		t.Fatalf("after ForgetPending the next entry must be admitted, got %v", err)
	}
}

// N7 part 1b — a confirmed cancel is not a duplicate-send risk, so ForgetPending
// must also drop the recent-send stamp: a new entry 10s after the confirmed
// cancel must NOT be refused by the 60s window.
func TestForgetPendingClearsRecentSendWindow(t *testing.T) {
	w := newLatchWire(t)
	tr := w.trader(t)
	fx := &latchFixture{now: time.Now()}
	tr.SetEntryLatchSource(fx.source())
	order, err := tr.OpenLong("MNQ", 1, 1)
	if err != nil {
		t.Fatalf("the first entry must go: %v", err)
	}
	sid, _ := order["signal_id"].(string)
	tr.ForgetPending(sid)
	fx.now = fx.now.Add(10 * time.Second)
	if _, err := tr.PlaceLimitEntry("MNQ", "long", 1, 29100, 29000, 29200); err != nil {
		t.Fatalf("10s after a confirmed cancel the next entry must be admitted, got %v", err)
	}
}

// Protection, management and exits never take the latch: with the book live
// they all still reach the wire.
func TestEntryLatchDoesNotGateProtectionManagementOrExits(t *testing.T) {
	w := newLatchWire(t)
	tr := w.trader(t)
	tr.SetEntryLatchSource((&latchFixture{live: true, now: time.Now()}).source())
	tr.mu.Lock()
	tr.lastEntrySignalID = "sig-open-1"
	tr.mu.Unlock()
	for _, st := range []struct {
		name string
		call func() error
		want ntwire.FrameType
	}{
		{"PlaceProtectiveStop", func() error { return tr.PlaceProtectiveStop("MNQ", "long", 1, 29000, "sig-open-1", "test") }, ntwire.FramePlaceProtectiveStop},
		{"CancelOrder", func() error { return tr.CancelOrder("sig-open-1") }, ntwire.FrameCancelOrder},
		{"ModifyBracket", func() error { return tr.ModifyBracket("sig-open-1", 29010, 29190) }, ntwire.FrameModifyBracket},
		{"CloseLong", func() error { _, err := tr.CloseLong("MNQ", 1); return err }, ntwire.FrameClosePosition},
	} {
		if err := st.call(); err != nil {
			t.Fatalf("%s must not be latched: %v", st.name, err)
		}
		if !waitFrame(w.frames, st.want, 2*time.Second) {
			t.Fatalf("%s: its frame never reached the wire", st.name)
		}
	}
}
