package telegram

// Owner identity (lane owner-identity, 2026-10-14): botIdentity.refresh — the
// ONLY place runBot gets its account and mints its token
// (TestRunBotMintsOnlyThroughRefresh) — used users[0], the first-created
// account. On the live box that is a TEST account owning no trader, so the
// bot minted its JWT for an account that owns nothing. refresh now reads
// store.ResolveOwner, the one resolver api requireOwner uses too.

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"vl/auth"
	"vl/store"
	"vl/telemetry"
)

// botTestAcctID plays the live box's rowid-1 test account (ee3bd00c…).
const botTestAcctID = "ee3bd00c-1111-2222-3333-444444444444"

// botSeedTestAccountFirst creates the test account BEFORE the owner (btBoot
// creates the owner 1 h ago), so it is users[0].
func botSeedTestAccountFirst(t *testing.T, st *store.Store) {
	t.Helper()
	hash, err := auth.HashPassword("test-account-pass")
	if err != nil {
		t.Fatal(err)
	}
	earlier := time.Now().Add(-2 * time.Hour).UTC()
	if err := st.User().Create(&store.User{ID: botTestAcctID, Email: "test-account@example.test", PasswordHash: hash, CreatedAt: earlier, UpdatedAt: earlier}); err != nil {
		t.Fatal(err)
	}
	if users, err := st.User().GetAll(); err != nil || len(users) != 2 || users[0].ID != botTestAcctID {
		t.Fatalf("fixture: the test account must be users[0] (GetAll orders by created_at): %v %v", users, err)
	}
}

func botSeedTrader(t *testing.T, st *store.Store, id, userID string) {
	t.Helper()
	if err := st.Trader().Create(&store.Trader{ID: id, UserID: userID, Name: id, AIModelID: "m", ExchangeID: "x", InitialBalance: 1000}); err != nil {
		t.Fatal(err)
	}
}

// The live shape: refresh mints for the traders' owner, and the token it
// installs reaches the owner's traders on the production API.
func TestBotRefreshMintsForTheResolvedOwnerNotTheFirstAccount(t *testing.T) {
	base, st := btBoot(t)
	botSeedTestAccountFirst(t, st)
	botSeedTrader(t, st, "8d5c8af5_8ef641a7-hoang", btOwnerID)
	botSeedTrader(t, st, "8d5c8af5_396db319-12345", btOwnerID)

	ident := newBotIdentity(st, 0)
	if !ident.refresh() {
		t.Fatal("refresh with a resolvable owner = false")
	}
	if ident.userID != btOwnerID {
		t.Fatalf("the bot acts for %q — want the traders' owner %q, not the first-created test account", ident.userID, btOwnerID)
	}
	claims, err := auth.ValidateJWT(ident.token)
	if err != nil || claims.UserID != btOwnerID {
		t.Fatalf("the bot's token names %q (err %v) — want the owner %q", func() string {
			if claims == nil {
				return ""
			}
			return claims.UserID
		}(), err, btOwnerID)
	}
	code, body := btCall(t, base, "GET", "/api/my-traders", ident.token, "")
	if code != http.StatusOK || !strings.Contains(body, "8d5c8af5_8ef641a7-hoang") || !strings.Contains(body, "8d5c8af5_396db319-12345") {
		t.Fatalf("the bot's token on GET /api/my-traders = %d %s — want the owner's two traders", code, body)
	}
}

// No traders → the bot acts for the first-created user (the old rule, kept
// only where nothing better exists).
func TestBotRefreshNoTradersIsTheFirstCreatedUser(t *testing.T) {
	_, st := btBoot(t)
	botSeedTestAccountFirst(t, st)
	ident := newBotIdentity(st, 0)
	if !ident.refresh() || ident.userID != botTestAcctID {
		t.Fatalf("no traders: the bot acts for %q — want the first-created account %q", ident.userID, botTestAcctID)
	}
}

// Split ownership → NO owner: refresh mints nothing, the bot drops the
// identity it held (it acts for NOBODY, never the previous owner), the chat
// is told the bot is off, and the refusal is counted.
func TestBotRefreshActsForNobodyWhenNoOwnerResolves(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	t.Cleanup(telemetry.ResetGateBlocksForTest)
	_, st := btBoot(t)
	botSeedTestAccountFirst(t, st)
	botSeedTrader(t, st, "owner-trader", btOwnerID)

	ident := newBotIdentity(st, 0)
	if !ident.refresh() || ident.userID != btOwnerID {
		t.Fatalf("setup: the bot acts for %q, want the owner", ident.userID)
	}
	botSeedTrader(t, st, "test-acct-trader", botTestAcctID)

	if ident.refresh() {
		t.Fatal("refresh reported success with the traders split across two users")
	}
	if ident.userID != "" || ident.email != "" || ident.token != "" || ident.agents != nil {
		t.Fatalf("no owner resolves but the bot still acts for %q (token set=%v, manager set=%v) — it must act for nobody",
			ident.userID, ident.token != "", ident.agents != nil)
	}
	if msg := ident.offMsg("No account found."); !strings.Contains(msg, "No owner account is resolved") {
		t.Fatalf("the chat is told %q — want the no-owner text, not the register hint", msg)
	}
	_, table := telemetry.GateBlockSnapshot()
	if n := table[""][store.OwnerUnresolvedGate]; n != 1 {
		t.Fatalf("owner_unresolved count = %d, want 1", n)
	}
}

// An empty box keeps the registration hint (registering is the remedy there).
func TestBotOffMsgOnAnEmptyBoxIsTheRegisterHint(t *testing.T) {
	_, st := btBoot(t)
	if err := st.User().DeleteAll(); err != nil {
		t.Fatal(err)
	}
	ident := newBotIdentity(st, 0)
	if ident.refresh() {
		t.Fatal("refresh on a box with no users = true")
	}
	if got := ident.offMsg("No account found."); got != "No account found." {
		t.Fatalf("empty box: chat told %q, want the register hint", got)
	}
}
