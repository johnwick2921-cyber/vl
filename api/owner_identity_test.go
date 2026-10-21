package api

// Owner identity (lane owner-identity, 2026-10-14): requireOwner at its
// production call site — POST /api/telegram/bind-code through the production
// router — on the live box's shape. requireOwner used users[0] (the
// first-created account); on the live box that is a TEST account owning no
// trader, while both traders belong to the second account. The owner is now
// store.ResolveOwner: the traders' one owner, else (no traders) the first
// user, else NOBODY.

import (
	"net/http"
	"testing"
	"time"

	"vl/store"
	"vl/telemetry"
)

// oiTestAcctID plays the live box's rowid-1 test account (ee3bd00c…):
// created BEFORE the owner, owning no trader.
const (
	oiTestAcctID    = "ee3bd00c-1111-2222-3333-444444444444"
	oiTestAcctEmail = "test-account@example.test"
)

// oiSeedTestAccountFirst creates the test account an hour BEFORE the admin
// (newUpdEnv creates the admin 1 h ago), so it is users[0], and returns its
// JWT.
func oiSeedTestAccountFirst(t *testing.T, e *updEnv) string {
	t.Helper()
	earlier := time.Now().Add(-2 * time.Hour).UTC()
	if err := e.st.User().Create(&store.User{ID: oiTestAcctID, Email: oiTestAcctEmail, PasswordHash: "x", CreatedAt: earlier, UpdatedAt: earlier}); err != nil {
		t.Fatalf("seed test account: %v", err)
	}
	users, err := e.st.User().GetAll()
	if err != nil || len(users) < 2 || users[0].ID != oiTestAcctID {
		t.Fatalf("fixture: the test account must be users[0] (GetAll orders by created_at): %v %v", users, err)
	}
	return mintJWT(t, oiTestAcctID, oiTestAcctEmail, time.Now().Add(-5*time.Second), time.Now().Add(time.Hour), updSecret)
}

func oiSeedTrader(t *testing.T, e *updEnv, id, userID string) {
	t.Helper()
	if err := e.st.Trader().Create(&store.Trader{ID: id, UserID: userID, Name: id, AIModelID: "m", ExchangeID: "x", InitialBalance: 1000}); err != nil {
		t.Fatalf("seed trader %s: %v", id, err)
	}
}

func oiOwnerUnresolvedCount() int {
	_, table := telemetry.GateBlockSnapshot()
	return table[""][store.OwnerUnresolvedGate]
}

// The live shape: the test account first, the owner (the enrolled admin)
// second with BOTH traders → the owner's claims pass, the test account's fail.
func TestRequireOwnerTrustsTheTradersOwnerNotTheFirstAccount(t *testing.T) {
	e := newUpdEnv(t)
	testTok := oiSeedTestAccountFirst(t, e)
	oiSeedTrader(t, e, "8d5c8af5_8ef641a7-hoang", updAdminID)
	oiSeedTrader(t, e, "8d5c8af5_396db319-12345", updAdminID)

	if w := telegramBindCall(t, e, e.tok); w.Code != http.StatusOK {
		t.Fatalf("the traders' owner on the owner-only route: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	if w := telegramBindCall(t, e, testTok); w.Code != http.StatusForbidden {
		t.Fatalf("the first-created TEST account (owns no trader) on the owner-only route: want 403, got %d (%s)", w.Code, w.Body.String())
	}
}

// No traders → the first-created user is the owner (the old rule, kept only
// where nothing better exists).
func TestRequireOwnerNoTradersIsTheFirstCreatedUser(t *testing.T) {
	e := newUpdEnv(t)
	testTok := oiSeedTestAccountFirst(t, e)

	if w := telegramBindCall(t, e, testTok); w.Code != http.StatusOK {
		t.Fatalf("no traders: the first-created account must be the owner: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	if w := telegramBindCall(t, e, e.tok); w.Code != http.StatusForbidden {
		t.Fatalf("no traders: a later account is not the owner: want 403, got %d (%s)", w.Code, w.Body.String())
	}
}

// Traders split across two users → NO owner: both accounts are refused, and
// every refusal is counted under owner_unresolved.
func TestRequireOwnerRefusesEveryoneOnSplitOwnership(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	t.Cleanup(telemetry.ResetGateBlocksForTest)
	e := newUpdEnv(t)
	testTok := oiSeedTestAccountFirst(t, e)
	oiSeedTrader(t, e, "owner-trader", updAdminID)
	oiSeedTrader(t, e, "test-acct-trader", oiTestAcctID)

	for name, tok := range map[string]string{"owner": e.tok, "first-created test account": testTok} {
		if w := telegramBindCall(t, e, tok); w.Code != http.StatusForbidden {
			t.Fatalf("split ownership, %s: want 403 (no owner resolves), got %d (%s)", name, w.Code, w.Body.String())
		}
	}
	if n := oiOwnerUnresolvedCount(); n != 2 {
		t.Fatalf("owner_unresolved count = %d, want 2 (one per refusal)", n)
	}
}

// The traders' user_id is not in users → NO owner, never the first account.
func TestRequireOwnerRefusesWhenTheTradersUserIsMissing(t *testing.T) {
	e := newUpdEnv(t)
	oiSeedTrader(t, e, "orphan-trader", "8ef641a7-0000-gone-from-users")
	if w := telegramBindCall(t, e, e.tok); w.Code != http.StatusForbidden {
		t.Fatalf("traders of a missing user: the only (first) account must be refused, got %d (%s)", w.Code, w.Body.String())
	}
}
