package trader

import (
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// m430 (owner 2026-10-25 09:14 CT, "Update must work in ANY scenario"): NT8
// linked over TCP but its broker connection down sends accounts_list count=0.
// The sim_accounts leg must treat that EMPTY live list as "no evidence" and
// vouch from the persisted census, with the NT8-closed path's fail-closed rules.

func m430Persist(t *testing.T, f *gateFixture, isSim bool) {
	t.Helper()
	if err := f.st.NTAccountsLast().Upsert([]store.NTAccountLast{
		{Name: "Sim101", IsSim: isSim, Connection: "SimulationConnection", ReceivedAtMs: time.Now().UnixMilli()},
	}); err != nil {
		t.Fatal(err)
	}
}

// (a) wire up + empty list + persisted SIM row -> PASS, honest text.
func TestInstallationGateM430EmptyLiveListUsesPersistedPass(t *testing.T) {
	f := newGateFixture(t)
	f.srv.SetAccountsList([]ntwire.AccountInfo{}, "")
	m430Persist(t, f, true)
	l, ok := legOf(f.run(), "sim_accounts")
	if !ok || !l.Pass {
		t.Fatalf("sim_accounts must pass from the persisted census: %+v", l)
	}
	for _, w := range []string{"account list is empty", "last census", "last seen"} {
		if !strings.Contains(l.Detail, w) {
			t.Fatalf("detail %q must say %q", l.Detail, w)
		}
	}
}

// (b) wire up + empty list + no persisted row -> FAIL.
func TestInstallationGateM430EmptyLiveListNoPersistedFails(t *testing.T) {
	f := newGateFixture(t)
	f.srv.SetAccountsList([]ntwire.AccountInfo{}, "")
	mustFail(t, f.run(), "sim_accounts", "account list is empty and no persisted account")
}

// (c) wire up + empty list + persisted non-SIM row -> FAIL.
func TestInstallationGateM430EmptyLiveListPersistedNonSimFails(t *testing.T) {
	f := newGateFixture(t)
	f.srv.SetAccountsList([]ntwire.AccountInfo{}, "")
	m430Persist(t, f, false)
	mustFail(t, f.run(), "sim_accounts", "is not SIM (last seen")
}

// (d) wire up + NON-empty list lacking the bound account -> FAIL even though a
// persisted SIM row exists (the live list stays authoritative).
func TestInstallationGateM430NonEmptyLiveListMissingAccountFails(t *testing.T) {
	f := newGateFixture(t)
	f.srv.SetAccountsList([]ntwire.AccountInfo{{Name: "Other", IsSim: true}}, "Other")
	m430Persist(t, f, true)
	mustFail(t, f.run(), "sim_accounts", "not SIM-tradeable (live)")
}

// (e) wire up + NON-empty list with the bound account non-SIM -> FAIL even
// with a persisted SIM row.
func TestInstallationGateM430NonEmptyLiveListNonSimFails(t *testing.T) {
	f := newGateFixture(t)
	f.srv.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: false}}, "Sim101")
	m430Persist(t, f, true)
	mustFail(t, f.run(), "sim_accounts", "not SIM-tradeable (live)")
}

// (f) wire down -> the existing persisted path, text unchanged.
func TestInstallationGateM430WireDownPersistedUnchanged(t *testing.T) {
	f := newGateFixture(t)
	f.wire.Connected = false
	f.srv.SetAccountsList([]ntwire.AccountInfo{}, "")
	m430Persist(t, f, true)
	l, ok := legOf(f.run(), "sim_accounts")
	if !ok || !l.Pass || !strings.Contains(l.Detail, "(persisted, last seen") || strings.Contains(l.Detail, "empty") {
		t.Fatalf("wire-down path changed: %+v", l)
	}
	f2 := newGateFixture(t)
	f2.wire.Connected = false
	mustFail(t, f2.run(), "sim_accounts", "NT8 closed and no persisted account")
}
