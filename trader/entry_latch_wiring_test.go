package trader

import (
	"context"
	"go/ast"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── W-EXEC-TRUTH W0 (b) — the latch's PRODUCTION evidence and wiring ───────
//
// The latch inside TCPTrader cannot read the ≤60 s book law or either ledger
// (package trader imports it). wireNT8EntryLatch hands it two sources built
// here, from the SAME definitions the armed path's oneContractGuard uses.

type latchWiringFixture struct {
	at *AutoTrader
	st *store.Store
	s  *ntwire.TCPServer
	nt *ntTrader.TCPTrader
}

func newLatchWiringFixture(t *testing.T) *latchWiringFixture {
	t.Helper()
	withMaintenanceDir(t)
	at, st := resetTrader(t, store.StrategyConfig{})
	at.id = "latch-wiring"
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })
	nt := ntTrader.NewTCPTrader(s, "MNQ", "Sim101")
	// W117 F4 — GetPositions is UNKNOWN until a snapshot or a confirmed fill;
	// the AddOn emits a positions frame on connect. Seed the known-flat book so
	// the entry path reads an empty account, not an unreadable one.
	s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
	at.trader = nt
	at.config.NinjaTraderSymbol = "MNQ"
	return &latchWiringFixture{at: at, st: st, s: s, nt: nt}
}

func (f *latchWiringFixture) book(orders []ntwire.NT8Order, at time.Time) {
	f.s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: orders}, at)
}

func TestEntryLatchBookIsScopedToTheSymbolAndFailsClosed(t *testing.T) {
	now := time.Now()
	working := func(name, sym string) ntwire.NT8Order {
		return ntwire.NT8Order{OrderID: "ord-" + name, Name: name, Symbol: sym, State: "working", Action: "buy", Type: "limit", Quantity: 1}
	}
	for _, tc := range []struct {
		name       string
		orders     []ntwire.NT8Order
		bookAt     time.Time
		noBook     bool
		verifiable bool
		live       bool
	}{
		{name: "no_book", noBook: true, verifiable: false},
		{name: "stale_book", orders: nil, bookAt: now.Add(-5 * time.Minute), verifiable: false},
		{name: "empty_fresh_book", orders: nil, bookAt: now, verifiable: true, live: false},
		{name: "working_entry_same_symbol", orders: []ntwire.NT8Order{working("sig-a", "MNQ 12-26")}, bookAt: now, verifiable: true, live: true},
		{name: "working_entry_other_symbol", orders: []ntwire.NT8Order{working("sig-b", "ES 12-26")}, bookAt: now, verifiable: true, live: false},
		{name: "working_entry_no_symbol_counts", orders: []ntwire.NT8Order{working("sig-c", "")}, bookAt: now, verifiable: true, live: true},
		{name: "protective_and_exit_children_are_not_entries", orders: []ntwire.NT8Order{working("sig-d-sl", "MNQ"), working("sig-d-tp", "MNQ"), working("sig-d-lx", "MNQ")}, bookAt: now, verifiable: true, live: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLatchWiringFixture(t)
			if !tc.noBook {
				f.book(tc.orders, tc.bookAt)
			}
			v := f.at.entryLatchBook(now, nil)
			if v.Verifiable != tc.verifiable || (tc.verifiable && v.Live != tc.live) {
				t.Fatalf("book verdict = %+v, want verifiable=%v live=%v", v, tc.verifiable, tc.live)
			}
		})
	}
}

// A position on the symbol makes the book live; a position on another symbol
// does not.
func TestEntryLatchBookSeesAPositionOnTheSymbol(t *testing.T) {
	f := newLatchWiringFixture(t)
	now := time.Now()
	f.book(nil, now)
	f.s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "ES", Side: "short", Quantity: 1, AvgPrice: 6000}})
	if v := f.at.entryLatchBook(now, nil); !v.Verifiable || v.Live {
		t.Fatalf("a position on ANOTHER symbol must not latch this one: %+v", v)
	}
	f.s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "short", Quantity: 1, AvgPrice: 29000}})
	if v := f.at.entryLatchBook(now, nil); !v.Verifiable || !v.Live {
		t.Fatalf("a position on the symbol must latch it: %+v", v)
	}
}

// The ledger source lists PLACED non-terminal rows only: an armed row with a
// signal in place_pending/working/cancel_pending, a Picture row working or
// stamped. An unplaced arm, an unstamped Picture claim and a row on another
// account are not listed.
func TestEntryLatchLedgersListPlacedRowsOnTheAccount(t *testing.T) {
	f := newLatchWiringFixture(t)
	led := f.st.ArmedOrders()
	mk := func(trader, state, sid string) *store.ArmedOrderDB {
		r := &store.ArmedOrderDB{TraderID: trader, PlanID: "p", Scenario: "S-" + trader + "-" + state, Version: 1, State: "armed", Side: "long", EntryPx: 29000, StopPx: 28990, TargetPx: 29030}
		if err := led.UpsertArm(r); err != nil {
			t.Fatal(err)
		}
		if sid != "" {
			if err := led.BeginPlacement(r.ID, sid); err != nil {
				t.Fatal(err)
			}
			if state != "place_pending" {
				if err := led.SetState(r.ID, state, "fixture"); err != nil {
					t.Fatal(err)
				}
			}
		}
		return r
	}
	unplaced := mk(f.at.id, "armed", "")
	working := mk(f.at.id, "working", "sig-work")
	// A trader bound to ANOTHER account (store binding) — not ours.
	if err := f.st.Trader().Create(&store.Trader{ID: "other-acct", Name: "o", Account: "Sim202"}); err != nil {
		t.Fatal(err)
	}
	foreign := mk("other-acct", "working", "sig-foreign")
	for _, p := range []*store.PictureHtfOpportunityDB{
		{OppKey: "pic-claimed", TraderID: f.at.id, Account: "Sim101", Symbol: "MNQ", Stage: "confirmed"},
		{OppKey: "pic-working", TraderID: f.at.id, Account: "Sim101", Symbol: "MNQ", Stage: "confirmed"},
	} {
		if _, _, err := f.st.PictureHtfClaim(p); err != nil {
			t.Fatal(err)
		}
	}
	if won, err := f.st.PictureHtfClaimSubmission("pic-claimed", "claim-1"); err != nil || !won {
		t.Fatalf("fixture: %v %v", won, err)
	}
	if err := f.st.PictureHtfTransition("pic-working", "working", "fixture"); err != nil {
		t.Fatal(err)
	}

	ids, err := f.at.entryLatchLedgers(nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(ids, " ")
	if !strings.Contains(joined, "armed#"+latchItoa(working.ID)) {
		t.Fatalf("a working armed row with a signal must be listed: %v", ids)
	}
	for _, not := range []string{"armed#" + latchItoa(unplaced.ID) + "(", "armed#" + latchItoa(foreign.ID) + "("} {
		if strings.Contains(joined, not) {
			t.Fatalf("listed a row the latch must not count (%s): %v", not, ids)
		}
	}
	// W5 (L12, CTO 1790197560916): a Picture row is named by its id and stage,
	// never by its opportunity key (the key embeds the account name).
	picName := func(key string) string {
		r, ok, err := f.st.PictureHtfGet(key)
		if err != nil || !ok {
			t.Fatalf("picture row %s: %v %v", key, ok, err)
		}
		return "latched by picture row #" + latchItoa(r.ID) + " ("
	}
	if !strings.Contains(joined, picName("pic-working")) || strings.Contains(joined, picName("pic-claimed")) {
		t.Fatalf("a working Picture row is listed, an unstamped claim is not: %v", ids)
	}
	if strings.Contains(joined, "pic-working") || strings.Contains(joined, "pic-claimed") {
		t.Fatalf("the latch text must never carry the opportunity key: %v", ids)
	}
	// Once the claim is stamped (a send was started), it counts.
	if err := f.st.PictureHtfStampSignal("pic-claimed", "claim-1", "broker-sig"); err != nil {
		t.Fatal(err)
	}
	ids, _ = f.at.entryLatchLedgers(nil)
	if !strings.Contains(strings.Join(ids, " "), picName("pic-claimed")) {
		t.Fatalf("a stamped Picture claim must be listed: %v", ids)
	}
}

// wireNT8EntryLatch installs both sources; the TCPTrader then READS wired.
func TestWireNT8EntryLatchInstallsTheSources(t *testing.T) {
	f := newLatchWiringFixture(t)
	if f.nt.EntryLatchWired() {
		t.Fatal("fixture: a fresh TCPTrader is unwired")
	}
	if !strings.Contains(entryLatchBootLine(f.at), "latch=UNWIRED") {
		t.Fatalf("the boot line must READ UNWIRED before wiring: %q", entryLatchBootLine(f.at))
	}
	wireNT8EntryLatch(f.at, f.nt)
	if !f.nt.EntryLatchWired() {
		t.Fatal("wireNT8EntryLatch must install the latch's sources")
	}
	if line := entryLatchBootLine(f.at); !strings.Contains(line, "latch=wired") {
		t.Fatalf("the boot line must READ wired after wiring: %q", line)
	}
}

// NewAutoTrader calls wireNT8EntryLatch(at, nt) DIRECTLY and UNCONDITIONALLY
// inside its TCPTrader block (the M2 permit pattern — review 3 F2).
func TestNewAutoTraderCallsWireNT8EntryLatchUnconditionally(t *testing.T) {
	var block *ast.IfStmt
	for _, st := range parseFuncBody(t, "auto_trader.go", "NewAutoTrader") {
		ifs, ok := st.(*ast.IfStmt)
		if !ok || ifs.Init == nil {
			continue
		}
		as, ok := ifs.Init.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			continue
		}
		if ta, ok := as.Rhs[0].(*ast.TypeAssertExpr); ok && isSelector(ta.X, "at", "trader") {
			if star, ok := ta.Type.(*ast.StarExpr); ok && isSelector(star.X, "ntTrader", "TCPTrader") {
				block = ifs
			}
		}
	}
	if block == nil {
		t.Fatal("NewAutoTrader has no top-level TCPTrader block")
	}
	for _, st := range block.Body.List {
		if es, ok := st.(*ast.ExprStmt); ok {
			if c, ok := es.X.(*ast.CallExpr); ok {
				if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "wireNT8EntryLatch" && len(c.Args) == 2 {
					if a0, ok := c.Args[0].(*ast.Ident); ok && a0.Name == "at" {
						if a1, ok := c.Args[1].(*ast.Ident); ok && a1.Name == "nt" {
							return
						}
					}
				}
			}
		}
	}
	t.Fatal("NewAutoTrader must call wireNT8EntryLatch(at, nt) as a direct, unconditional statement in its TCPTrader block")
}

// wireNT8EntryLatch passes both production sources as a direct statement.
func TestWireNT8EntryLatchUsesTheProductionSources(t *testing.T) {
	body := parseFuncBody(t, "entry_latch_wiring.go", "wireNT8EntryLatch")
	src := ""
	for _, st := range body {
		if es, ok := st.(*ast.ExprStmt); ok {
			if c, ok := es.X.(*ast.CallExpr); ok {
				if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "SetEntryLatchSource" {
					src = "found"
				}
			}
		}
	}
	if src == "" {
		t.Fatal("wireNT8EntryLatch must call nt.SetEntryLatchSource(...) as a direct statement")
	}
	b := readSource(t, "entry_latch_wiring.go")
	for _, want := range []string{"Book:    at.entryLatchBook", "Ledgers: at.entryLatchLedgers"} {
		if !strings.Contains(b, want) {
			t.Fatalf("wireNT8EntryLatch must pass %q", want)
		}
	}
}

// The boot line is emitted from Run (READ, L7).
func TestRunEmitsTheEntryLatchBootLine(t *testing.T) {
	if !strings.Contains(readSource(t, "auto_trader.go"), "entryLatchBootLine(at)") {
		t.Fatal("Run must log entryLatchBootLine(at) so the latch wiring is READ at boot")
	}
}

func readSource(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func latchItoa(n int64) string { return strconv.FormatInt(n, 10) }
