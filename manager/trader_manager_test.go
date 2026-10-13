package manager

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/store"
)

// REL18-C item 5 — the boot claim decision: the FIRST trader on a
// (symbol, account) claims it; a SECOND trader marked running on the same pair
// is refused, naming the holder. Non-NT / unbound / no-claim-pass all proceed.
func TestBootClaimFirstByCreatedAtWins(t *testing.T) {
	tm := &TraderManager{startPassClaims: make(map[string]string)}
	ex := &store.Exchange{ExchangeType: "ninjatrader", NTInstrumentName: "MNQ"}

	// Oldest (created first) claims.
	first := &store.Trader{ID: "t1", Account: "Sim101", CreatedAt: time.Now().Add(-time.Hour)}
	if holder, refused := tm.bootClaimFor(ex, first); refused {
		t.Fatalf("first trader must claim, got refused holder=%q", holder)
	}

	// A second trader on the same (symbol, account) is refused, naming t1.
	second := &store.Trader{ID: "t2", Account: "Sim101", CreatedAt: time.Now()}
	if holder, refused := tm.bootClaimFor(ex, second); !refused || holder != "t1" {
		t.Fatalf("second trader must be refused naming t1, got holder=%q refused=%v", holder, refused)
	}

	// A different ACCOUNT is a different key → proceeds.
	other := &store.Trader{ID: "t3", Account: "SimAccount1", CreatedAt: time.Now()}
	if _, refused := tm.bootClaimFor(ex, other); refused {
		t.Fatalf("a different account must not be refused")
	}

	// An unbound trader (empty account) never participates in the claim.
	unbound := &store.Trader{ID: "t4", Account: "", CreatedAt: time.Now()}
	if _, refused := tm.bootClaimFor(ex, unbound); refused {
		t.Fatalf("an unbound trader must proceed (no dedup)")
	}

	// A nil claim map (per-user reload) never refuses.
	tmReload := &TraderManager{startPassClaims: nil}
	if _, refused := tmReload.bootClaimFor(ex, second); refused {
		t.Fatalf("a nil claim map (reload) must never refuse")
	}
}

// REL18-C item 5 at the production call site (shouldAutoStart): a boot with two
// traders marked running on the same (symbol, account) starts the FIRST and
// refuses the SECOND — marking it not-running and writing a P0 naming both.
func TestShouldAutoStartBootDuplicateRefused(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "boot-autostart.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if err := st.Trader().Create(&store.Trader{ID: "t1", UserID: "u", Name: "first", Account: "Sim101", IsRunning: true, CreatedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := st.Trader().Create(&store.Trader{ID: "t2", UserID: "u", Name: "second", Account: "Sim101", IsRunning: true, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	ex := &store.Exchange{ExchangeType: "ninjatrader", NTInstrumentName: "MNQ"}
	tm := &TraderManager{startPassClaims: make(map[string]string)}

	// Oldest first — it starts.
	if !tm.shouldAutoStart(ex, &store.Trader{ID: "t1", UserID: "u", Name: "first", Account: "Sim101", IsRunning: true}, st) {
		t.Fatal("the first (oldest) trader must auto-start")
	}
	// The second on the same (symbol, account) is refused.
	if tm.shouldAutoStart(ex, &store.Trader{ID: "t2", UserID: "u", Name: "second", Account: "Sim101", IsRunning: true}, st) {
		t.Fatal("the second trader must be refused")
	}

	row, _ := st.Trader().GetByID("t2")
	if row == nil || row.IsRunning {
		t.Fatalf("the refused trader must be marked not-running, got %+v", row)
	}
	alerts, _ := st.Alert().List("t2", 10)
	found := false
	for _, a := range alerts {
		if a.Kind == "boot_duplicate_symbol_account" {
			found = true
			if !strings.Contains(a.Body, "second") || !strings.Contains(a.Body, "t1") {
				t.Fatalf("P0 must name both traders, got: %s", a.Body)
			}
		}
	}
	if !found {
		t.Fatalf("no boot_duplicate_symbol_account P0 alert written; alerts=%d", len(alerts))
	}
}
