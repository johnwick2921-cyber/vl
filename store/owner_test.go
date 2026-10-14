package store

// Owner identity (lane owner-identity, 2026-10-14): the ONE resolver both
// owner checks use (api requireOwner, telegram botIdentity.refresh). Driven
// through the real Store over a real SQLite file — the same ResolveOwner the
// production call sites and the 👤 boot line read.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/telemetry"
)

// The live box's shape (CTO facts 11:5x CT, sqlite3 -readonly): users rowid 1
// is a TEST account created first that owns no trader; rowid 2 is the owner,
// created ~4 h later, owning both traders.
const (
	ownTestAcctID = "ee3bd00c-0000-4000-8000-000000000001"
	ownOwnerID    = "396db319-0000-4000-8000-000000000002"
)

func ownerFixture(t *testing.T) *Store {
	t.Helper()
	st, err := New(filepath.Join(t.TempDir(), "owner.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func ownerSeedUser(t *testing.T, st *Store, id string, created time.Time) {
	t.Helper()
	if err := st.User().Create(&User{ID: id, Email: id[:8] + "@example.test", PasswordHash: "x", CreatedAt: created, UpdatedAt: created}); err != nil {
		t.Fatalf("seed user %s: %v", id, err)
	}
}

func ownerSeedTrader(t *testing.T, st *Store, id, userID string) {
	t.Helper()
	if err := st.Trader().Create(&Trader{ID: id, UserID: userID, Name: id, AIModelID: "m", ExchangeID: "x", InitialBalance: 1000}); err != nil {
		t.Fatalf("seed trader %s: %v", id, err)
	}
}

// liveShape seeds the live box: the test account first, then the owner with
// both traders.
func ownerLiveShape(t *testing.T, st *Store) {
	t.Helper()
	base := time.Date(2026, 5, 29, 23, 42, 51, 0, time.UTC)
	ownerSeedUser(t, st, ownTestAcctID, base)
	ownerSeedUser(t, st, ownOwnerID, base.Add(4*time.Hour+16*time.Minute))
	ownerSeedTrader(t, st, "8d5c8af5_8ef641a7-hoang", ownOwnerID)
	ownerSeedTrader(t, st, "8d5c8af5_396db319-12345", ownOwnerID)
}

func TestResolveOwnerLiveShapeIsTheTradersOwnerNotTheFirstAccount(t *testing.T) {
	st := ownerFixture(t)
	ownerLiveShape(t, st)
	if users, _ := st.User().GetAll(); len(users) != 2 || users[0].ID != ownTestAcctID {
		t.Fatalf("fixture: the test account must be users[0] (GetAll orders by created_at): %v", users)
	}
	o := st.ResolveOwner()
	if o.UserID != ownOwnerID || o.Source != OwnerSourceTraders || o.OwnedTraders != 2 || o.TotalTraders != 2 || o.User == nil || o.User.ID != ownOwnerID {
		t.Fatalf("live shape resolved to %+v — want the traders' owner %s (source traders · 2 of 2)", o, ownOwnerID)
	}
	if got, want := o.BootLine(), "👤 owner: user 396db319 (source traders · owns 2 of 2 traders)"; got != want {
		t.Fatalf("boot line\n got %q\nwant %q", got, want)
	}
}

func TestResolveOwnerNoTradersIsTheFirstCreatedUser(t *testing.T) {
	st := ownerFixture(t)
	base := time.Date(2026, 5, 29, 23, 42, 51, 0, time.UTC)
	ownerSeedUser(t, st, ownOwnerID, base.Add(time.Hour))
	ownerSeedUser(t, st, ownTestAcctID, base) // created FIRST, inserted second
	o := st.ResolveOwner()
	if o.UserID != ownTestAcctID || o.Source != OwnerSourceFirstUser || o.TotalTraders != 0 {
		t.Fatalf("no traders resolved to %+v — want the first-created user %s", o, ownTestAcctID)
	}
	if got, want := o.BootLine(), "👤 owner: user ee3bd00c (source first-user (no traders) · owns 0 of 0 traders)"; got != want {
		t.Fatalf("boot line\n got %q\nwant %q", got, want)
	}
}

func TestResolveOwnerRefusesSplitOwnership(t *testing.T) {
	st := ownerFixture(t)
	ownerLiveShape(t, st)
	ownerSeedTrader(t, st, "test-acct-trader", ownTestAcctID)
	o := st.ResolveOwner()
	if o.Resolved() || o.User != nil || o.Source != "" {
		t.Fatalf("split ownership resolved to %+v — want NO owner", o)
	}
	if !strings.Contains(o.Reason, "the 3 traders belong to 2 users (396db319 ×2, ee3bd00c ×1)") {
		t.Fatalf("split reason = %q", o.Reason)
	}
	if got, want := o.BootLine(), "👤 owner: NONE — the 3 traders belong to 2 users (396db319 ×2, ee3bd00c ×1); owner-only routes refuse, the Telegram bot is off"; got != want {
		t.Fatalf("boot line\n got %q\nwant %q", got, want)
	}
}

func TestResolveOwnerRefusesTradersOfAMissingUser(t *testing.T) {
	st := ownerFixture(t)
	ownerSeedUser(t, st, ownTestAcctID, time.Now().Add(-time.Hour).UTC())
	ownerSeedTrader(t, st, "orphan-trader", "8ef641a7-gone-from-users")
	o := st.ResolveOwner()
	if o.Resolved() {
		t.Fatalf("traders of a user missing from users resolved to %+v — want NO owner (never the first account)", o)
	}
	if want := "the 1 traders belong to user 8ef641a7, which is not in users"; o.Reason != want {
		t.Fatalf("reason = %q, want %q", o.Reason, want)
	}
}

func TestResolveOwnerNoUsersIsNoOwner(t *testing.T) {
	st := ownerFixture(t)
	o := st.ResolveOwner()
	if o.Resolved() || o.Reason != OwnerReasonNoUsers {
		t.Fatalf("empty box resolved to %+v — want NO owner, reason %q", o, OwnerReasonNoUsers)
	}
}

func TestResolveOwnerStoreErrorIsNoOwner(t *testing.T) {
	st := ownerFixture(t)
	ownerLiveShape(t, st)
	if err := st.GormDB().Exec("DROP TABLE traders").Error; err != nil {
		t.Fatal(err)
	}
	o := st.ResolveOwner()
	if o.Resolved() || !strings.HasPrefix(o.Reason, "traders unreadable (") || o.TotalTraders != -1 {
		t.Fatalf("a traders read error resolved to %+v — want NO owner (fail closed)", o)
	}
}

// The resolver only READS: the rows of both tables are byte-identical after
// every shape it resolves.
func TestResolveOwnerNeverWrites(t *testing.T) {
	st := ownerFixture(t)
	ownerLiveShape(t, st)
	snap := func() string {
		var b strings.Builder
		users, _ := st.User().GetAll()
		for _, u := range users {
			b.WriteString(u.ID + "|" + u.Email + "|" + u.PasswordHash + "|" + u.CreatedAt.String() + "|" + u.UpdatedAt.String() + "\n")
		}
		traders, _ := st.Trader().ListAll()
		for _, tr := range traders {
			b.WriteString(tr.ID + "|" + tr.UserID + "|" + tr.UpdatedAt.String() + "\n")
		}
		return b.String()
	}
	before := snap()
	for i := 0; i < 3; i++ {
		_ = st.ResolveOwner()
	}
	if after := snap(); after != before {
		t.Fatalf("ResolveOwner changed the users/traders rows:\nbefore %s\nafter  %s", before, after)
	}
}

// RecordOwnerRefusal counts every refusal under the owner_unresolved gate.
func TestRecordOwnerRefusalCounts(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	t.Cleanup(telemetry.ResetGateBlocksForTest)
	o := Owner{Reason: "the 3 traders belong to 2 users"}
	RecordOwnerRefusal("test site", o)
	RecordOwnerRefusal("test site", o)
	_, table := telemetry.GateBlockSnapshot()
	if n := table[""][OwnerUnresolvedGate]; n != 2 {
		t.Fatalf("owner_unresolved count = %d, want 2 (table %v)", n, table)
	}
}
