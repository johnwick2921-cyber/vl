//go:build !windows

package ninjatrader

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
)

// m55r3-h2 (release #55 round-3 review, exits-never-delayed lens, defect 2): the
// fill_price guard fsynced its state file INLINE in the exit-recording path
// (recordClose / recordCloseOrdered -> noteFill -> Record -> apply -> persist),
// so a stalled disk or an fsync spike stalled the ordered worker and every later
// position_close / fill / order_update frame queued behind it.
//
// The state write is now queued to an ordered, coalescing writer off the exit
// path. Proof at the PRODUCTION exit handler: the state file's temp name is a
// FIFO with no reader (a write to it blocks until one appears); a far exit is
// recorded through recordCloseOrdered; the handler must return promptly and the
// position must close - and once the disk "recovers" the stalled write lands and
// the strike is on disk (same crash-safety, later).
//
// MUTANT: persist() writes inline again (call writeFileAtomic instead of
// queueing) -> the handler blocks and the test goes RED at the deadline.
func TestFillGuardExitRecordingNeverWaitsOnTheStateFileWrite(t *testing.T) {
	f := newFillGuardFx(t)
	f.openRow(t, "sig-async1")
	path := filepath.Join(t.TempDir(), "fill.json")
	if _, err := f.g.EnableState(path); err != nil {
		t.Fatal(err)
	}
	tmp := path + ".tmp" // writeFileAtomic's temp name; free after EnableState's rename
	if err := syscall.Mkfifo(tmp, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	release := func() { // the disk "recovers": a reader appears
		if r, err := os.OpenFile(tmp, os.O_RDONLY|syscall.O_NONBLOCK, 0); err == nil {
			defer r.Close()
			time.Sleep(50 * time.Millisecond)
		}
	}
	t.Cleanup(func() { release(); f.g.Flush(5 * time.Second) })

	done := make(chan struct{})
	go func() {
		defer close(done)
		f.tr.recordCloseOrdered("t1", "ex", "ninjatrader", f.st, ntwire.PositionClosePayload{
			SignalID: "sig-async1", Symbol: "MNQ", PositionSide: "long", Account: "Sim101", Quantity: 1,
			ExitPrice: 29100, ExitReason: "sl", ExitTime: f.at(), Seq: 11,
		})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("recordCloseOrdered is blocked on the fill_price state-file write: an exit must never wait on disk")
	}
	if s := f.g.Stats(); s.Far != 1 {
		t.Fatalf("the far exit is still counted: %+v", s)
	}
	if open, _ := f.st.Position().GetOpenPositions("t1"); len(open) != 0 {
		t.Fatalf("the exit must still close the position, %d still open", len(open))
	}
	if f.g.Flush(150 * time.Millisecond) {
		t.Fatal("fixture: the state write should still be stalled behind the FIFO")
	}
}
