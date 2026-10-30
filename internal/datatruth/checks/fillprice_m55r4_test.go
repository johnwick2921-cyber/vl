//go:build !windows

package checks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// m55r4-h2 (release #55 round-4, restart lens): moving an unreadable state file
// aside must never leave the path EMPTY. The old order was rename-then-write: a
// crash, a full disk or a failed write between the two left no file at the path,
// and the next boot read "no file" = no pause = the block was silently lifted.
//
// installReplacing writes+syncs the replacement first, hard-links the old file to
// the .bad name (a link cannot overwrite), and only then renames. If the .bad name
// is taken the link fails, and the path must STILL hold the old bytes.
//
// MUTANT: rename the replacement over the path BEFORE the link (or drop the link
// step) -> the path holds the new bytes / the old bytes vanish, RED.
func TestInstallReplacingKeepsTheOldFileAtThePathUntilTheBadNameIsSecured(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fill.json")
	bad := path + ".bad-x"
	old := []byte("{corrupt")
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("an earlier evidence file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := installReplacing(path, bad, []byte(`{"version":1}`)); err == nil {
		t.Fatal("a taken .bad name must refuse (a link never overwrites)")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(old) {
		t.Fatalf("the path must still hold the unreadable file (err %v, got %q)", err, got)
	}
	if got, _ := os.ReadFile(bad); string(got) != "an earlier evidence file" {
		t.Fatalf("an existing .bad file is never overwritten: %q", got)
	}

	// the happy path: new bytes at the path, old bytes under the .bad name
	bad2 := path + ".bad-y"
	if err := installReplacing(path, bad2, []byte(`{"version":1}`)); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != `{"version":1}` {
		t.Fatalf("the replacement is at the path: %q", got)
	}
	if got, _ := os.ReadFile(bad2); string(got) != string(old) {
		t.Fatalf("the old bytes are kept under the .bad name: %q", got)
	}
}

// Through the production entry (EnableState): a failure while installing the
// replacement leaves the unreadable file at the path, so a SECOND boot is blocked
// again - the block is not lifted by the failure. The failure is a directory in
// the temp name's place (the write cannot start); the old code had already renamed
// the file away by then.
//
// MUTANT: EnableState goes back to moveAside-first then persist -> the path is
// empty after the failed write and the second boot reads "no file", RED.
func TestFillGuardUnreadableStateWriteFailureLeavesTheBlockStandingAtTheNextBoot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fill.json")
	garbage := []byte("{corrupt")
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)

	g1, _ := newTestGuard(nil)
	if _, err := g1.EnableState(path); err == nil {
		t.Fatal("the corrupt file is reported")
	}
	if b, _, _ := g1.Check()("MNQ", now); !b {
		t.Fatal("the unreadable-state block stands in memory")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(garbage) {
		t.Fatalf("the unreadable file is still at the path, untouched (err %v, got %q)", err, got)
	}

	g2, _ := newTestGuard(nil) // the next boot
	if _, err := g2.EnableState(path); err == nil {
		t.Fatal("the next boot still finds an unreadable file")
	}
	if b, _, _ := g2.Check()("MNQ", now); !b {
		t.Fatal("a failed install must not lift the block at the next boot")
	}
}

// A successful install: the new file carries the block (a second boot is blocked
// from a READABLE file) and the old bytes are kept, both names present.
func TestFillGuardUnreadableStateInstallKeepsBothFilesAndTheBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fill.json")
	garbage := []byte("{corrupt")
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	g1, _ := newTestGuard(nil)
	if _, err := g1.EnableState(path); err == nil {
		t.Fatal("reported")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var d fillStateDisk
	if err := json.Unmarshal(b, &d); err != nil || d.Unreadable == nil {
		t.Fatalf("the path holds a complete file carrying the unreadable record: %v %+v", err, d)
	}
	bads, _ := filepath.Glob(path + ".bad-*")
	if len(bads) != 1 || filepath.Base(bads[0]) != filepath.Base(d.Unreadable.BadFile) {
		t.Fatalf("one .bad file, named in the record: %v %q", bads, d.Unreadable.BadFile)
	}
	if got, _ := os.ReadFile(bads[0]); string(got) != string(garbage) {
		t.Fatalf("the evidence keeps its bytes: %q", got)
	}
	now := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	g2, _ := newTestGuard(nil)
	if _, err := g2.EnableState(path); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := g2.Check()("MNQ", now); !b {
		t.Fatal("the second boot (a readable file) is still blocked")
	}
	// the remedy text names only a remedy that works: the ack, not a file swap that
	// the next write would overwrite
	_, why, _ := g2.Check()("MNQ", now)
	if strings.Contains(why, "restore a repaired copy") || !strings.Contains(why, "ack-fill-price?symbol=MNQ") {
		t.Fatalf("remedy text: %q", why)
	}
}

// AckDurable says false when the disk did not take the acknowledgement inside the
// bounded wait (a stalled write): the ack is live in memory, and the caller can
// tell the owner a restart before the disk catches up re-pauses.
//
// MUTANT: ack() returns durable=true unconditionally -> RED.
func TestAckDurableIsFalseWhenTheStateWriteStalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fill.json")
	g, _ := newTestGuard(nil)
	if _, err := g.EnableState(path); err != nil {
		t.Fatal(err)
	}
	tmp := path + ".tmp"
	if err := syscall.Mkfifo(tmp, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	now := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := now.UnixMilli() / minuteMs * minuteMs
	g.Record(FillObs{ID: "b1", Symbol: "MNQ", Kind: "exit", Account: "Sim101", Price: 0, AtMs: m + 5_000}, nil, now)
	g.Record(FillObs{ID: "b2", Symbol: "MNQ", Kind: "exit", Account: "Sim101", Price: 0, AtMs: m + 6_000}, nil, now)
	acked, durable := g.AckDurable("MNQ")
	if !acked || durable {
		t.Fatalf("acked=%v durable=%v: the ack is live but the stalled disk did not take it", acked, durable)
	}
	if b, _, _ := g.Check()("MNQ", now); b {
		t.Fatal("the in-memory ack lifts the pause at once")
	}
	r, err := os.OpenFile(tmp, os.O_RDONLY|syscall.O_NONBLOCK, 0) // release the stalled writer
	if err == nil {
		g.Flush(5 * time.Second)
		r.Close()
	}
}
