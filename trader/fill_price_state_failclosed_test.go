package trader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	ntTrader "vl/trader/ninjatrader"
)

// m55r3-h2 (release #55 round-3, no-bypass lens, defect 1): an UNREADABLE
// fill_price state file used to fail OPEN - the guard "started empty", and then
// overwrote the unreadable file with an empty state: a standing pause could not
// be told from none, entries were admitted, and the evidence was destroyed.
//
// Through the production boot call (RegisterFillPrice -> EnableState) and the
// production verdict (dataTruthReasons, the call every entry door makes):
//   - the unreadable state BLOCKS the symbol's new entries for every trader;
//   - the file is MOVED ASIDE (bytes intact), never overwritten, never deleted;
//   - the block survives a second restart (it is persisted in the fresh file);
//   - the owner's symbol ack (the URL in the pause text) lifts it, and the lift
//     itself survives a restart.
//
// MUTANT: EnableState goes back to "start empty and persist over the file"
// (drop the unreadable block / the moveAside) -> RED.
func TestFillPriceUnreadableStateBlocksEntriesAndIsMovedAsideNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data", "vl.db.fillprice.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	garbage := []byte(`{"version":1,"strikes":{"MNQ|SIM101":{"ids":["e1","e2"` + "\x00\x00 half a write")
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	prevPath, _ := fillPriceStatePath.Load().(string)
	SetFillPriceStatePath(path)
	t.Cleanup(func() { SetFillPriceStatePath(prevPath) })
	savedDefault := checks.DefaultFills()
	t.Cleanup(func() { checks.SetDefaultFills(savedDefault) })
	t.Cleanup(datatruth.Reset)

	boot := func() *checks.FillGuard { // a process start
		datatruth.Reset()
		g := checks.NewFillGuard(checks.DefaultFillParams(), nil)
		checks.SetDefaultFills(g)
		RegisterFillPrice()
		return g
	}
	now := agreeT0
	blocked := func(sym string) (bool, string) {
		v := dataTruthReasons(sym, ntTrader.IntegrityDeepOpenMin, now, truthAsker{id: "t1", account: "SIM101"})
		return v.Blocked && strings.Contains(v.Reason, checks.CheckFillPrice+":"), v.Reason
	}

	g1 := boot()
	if b, why := blocked("MNQ"); !b {
		t.Fatalf("an unreadable fill_price state must BLOCK new entries (it cannot tell a standing pause from none): %q", why)
	}
	// the evidence: moved aside with its bytes, and the live path no longer holds it
	bads, _ := filepath.Glob(path + ".bad-*")
	if len(bads) != 1 {
		t.Fatalf("the unreadable file must be moved aside to <path>.bad-<utc>: %v", bads)
	}
	if got, err := os.ReadFile(bads[0]); err != nil || string(got) != string(garbage) {
		t.Fatalf("the moved-aside file must keep its bytes (err %v)", err)
	}
	if b, why := blocked("MNQ"); !strings.Contains(why, "ack-fill-price?symbol=MNQ") || !strings.Contains(why, filepath.Base(bads[0])) {
		t.Fatalf("the pause text must carry the ack remedy and name the moved file (blocked=%v): %q", b, why)
	}

	// a SECOND restart reads the fresh, valid file - the block must still stand
	boot()
	if b, why := blocked("MNQ"); !b {
		t.Fatalf("the unreadable-state block must survive the next restart (the fresh file is readable): %q", why)
	}
	if n, _ := filepath.Glob(path + ".bad-*"); len(n) != 1 {
		t.Fatalf("a readable file is not moved aside again: %v", n)
	}

	// the owner's ack (the exact remedy in the text) lifts it for that symbol
	g3 := boot()
	if !g3.Ack("MNQ") {
		t.Fatal("the ack URL's call (Ack(MNQ)) must report that a block was standing")
	}
	if b, why := blocked("MNQ"); b {
		t.Fatalf("an acknowledged unreadable-state block must lift: %q", why)
	}
	_ = g1
	boot()
	if b, why := blocked("MNQ"); b {
		t.Fatalf("the acknowledgement must survive a restart: %q", why)
	}
}

// An unreadable state file that CANNOT be moved aside must never be written over
// either: the block then lives in memory only.
func TestFillPriceUnreadableStateThatCannotBeMovedIsNeverWrittenOver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vl.db.fillprice.json")
	garbage := []byte("{not json")
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	// a read-only directory: the file reads, fails to parse, and cannot be renamed
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if f, err := os.Create(filepath.Join(dir, "probe")); err == nil { // running as root: the fixture cannot bite
		f.Close()
		t.Skip("directory permissions are not enforced here")
	}
	prevPath, _ := fillPriceStatePath.Load().(string)
	SetFillPriceStatePath(path)
	t.Cleanup(func() { SetFillPriceStatePath(prevPath) })
	savedDefault := checks.DefaultFills()
	t.Cleanup(func() { checks.SetDefaultFills(savedDefault) })
	t.Cleanup(datatruth.Reset)
	datatruth.Reset()
	g := checks.NewFillGuard(checks.DefaultFillParams(), nil)
	checks.SetDefaultFills(g)
	RegisterFillPrice()
	g.Flush(2 * time.Second)
	b, rs := datatruth.Evaluate("MNQ", agreeT0)
	if !b || len(rs) == 0 || !strings.Contains(rs[0].Detail, "could NOT be moved aside") {
		t.Fatalf("an unreadable state that cannot be moved aside still blocks, and says so: %v %+v", b, rs)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(garbage) {
		t.Fatalf("nothing may be written over the unreadable file (err %v)", err)
	}
	if !g.Ack("MNQ") {
		t.Fatal("the ack still works with the block in memory only")
	}
	if b, _ := datatruth.Evaluate("MNQ", agreeT0); b {
		t.Fatal("ack lifts the memory-only block")
	}
}

// m55r4-h2: through the production boot call and verdict, a failed install of the
// replacement state (here: a directory sits in the temp file's place, so the write
// cannot start) must not lift the block at the NEXT boot. The old order (move the
// file aside, then write) left no file at the path after that failure.
//
// MUTANT: EnableState moves the file aside before the replacement is written -> the
// second boot sees no file, RED.
func TestFillPriceUnreadableStateInstallFailureStillBlocksTheNextBoot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vl.db.fillprice.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	prevPath, _ := fillPriceStatePath.Load().(string)
	SetFillPriceStatePath(path)
	t.Cleanup(func() { SetFillPriceStatePath(prevPath) })
	savedDefault := checks.DefaultFills()
	t.Cleanup(func() { checks.SetDefaultFills(savedDefault) })
	t.Cleanup(datatruth.Reset)
	boot := func() {
		datatruth.Reset()
		g := checks.NewFillGuard(checks.DefaultFillParams(), nil)
		checks.SetDefaultFills(g)
		RegisterFillPrice()
		g.Flush(2 * time.Second)
	}
	for i := 1; i <= 2; i++ {
		boot()
		v := dataTruthReasons("MNQ", ntTrader.IntegrityDeepOpenMin, agreeT0, truthAsker{id: "t1", account: "SIM101"})
		if !v.Blocked || !strings.Contains(v.Reason, checks.CheckFillPrice+":") {
			t.Fatalf("boot %d: a failed install must keep new entries blocked: %q", i, v.Reason)
		}
	}
}
