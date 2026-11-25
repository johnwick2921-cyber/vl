package manager

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/internal/ownerpush"
	"vl/store"
)

// m471 L3-4 — a trader refused at boot because another trader already owns its
// (symbol, account) is a P0 the owner is now TOLD: it was marked not-running and
// does not trade after the restart. The row was written straight through
// st.Alert().Emit (never emitAlert), so it was never pushed, and the trader-only
// P0 census never saw it. Drives the production call site shouldAutoStart.
//
// MUTANT (shown RED): drop the ownerpush.Push in refuseBootDuplicate -> "the
// owner must be told a trader was not started at boot" -> RED.
func TestBootDuplicateRefusalReachesTheOwner(t *testing.T) {
	var mu sync.Mutex
	var got []string
	p := ownerpush.New(func(s string) { mu.Lock(); got = append(got, s); mu.Unlock() })
	t.Cleanup(ownerpush.Use(p))

	st, err := store.New(filepath.Join(t.TempDir(), "boot-dup-push.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	firstID := "first-trader-id-0123456789abcdef0123456789abcdef0123456789abcdef"
	secondID := "second-trader-id-0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := st.Trader().Create(&store.Trader{ID: firstID, UserID: "u", Name: "Mentor A", Account: "Sim101", IsRunning: true, CreatedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := st.Trader().Create(&store.Trader{ID: secondID, UserID: "u", Name: "Mentor B", Account: "Sim101", IsRunning: true, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	ex := &store.Exchange{ExchangeType: "ninjatrader", NTInstrumentName: "MNQ"}
	tm := &TraderManager{startPassClaims: make(map[string]string)}

	if !tm.shouldAutoStart(ex, &store.Trader{ID: firstID, UserID: "u", Name: "Mentor A", Account: "Sim101", IsRunning: true}, st) {
		t.Fatal("the first trader must auto-start")
	}
	if tm.shouldAutoStart(ex, &store.Trader{ID: secondID, UserID: "u", Name: "Mentor B", Account: "Sim101", IsRunning: true}, st) {
		t.Fatal("the second trader must be refused")
	}
	p.Flush()

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("the owner must be told a trader was not started at boot: %d message(s) %q", len(got), got)
	}
	msg := got[0]
	for _, want := range []string{"P0", "was not started", `"Mentor B"`, `"Mentor A"`, "MNQ / Sim101", "not-running"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("boot-duplicate push lacks %q: %q", want, msg)
		}
	}
	if strings.Contains(msg, "[redacted]") {
		t.Fatalf("the push must name traders by display name, not their token-shaped ids: %q", msg)
	}
	if !ownerpush.IsP0Key("boot_duplicate_symbol_account|x|y") {
		t.Fatal("boot_duplicate_symbol_account must be classed P0 by the channel (never dropped by an outage)")
	}
}
