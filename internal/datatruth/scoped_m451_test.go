package datatruth

import (
	"errors"
	"testing"
	"time"
)

// release #55 m451 - a check can say WHO its block is for; an error or a panic
// is for everyone (the check could not even say who it was about).
func TestM451ScopedBlockAppliesOnlyToItsTraderOrAccount(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	RegisterScoped("acct_check", func(string, time.Time) (bool, string, []string, error) {
		return true, "A disagrees", []string{ScopeTrader("A"), ScopeAccount("Sim101")}, nil
	})
	_, rs := Evaluate("MNQ", time.Now())
	if len(rs) != 1 {
		t.Fatalf("%+v", rs)
	}
	r := rs[0]
	if !r.AppliesTo("A", "") || !r.AppliesTo("other", "sim101") || r.AppliesTo("B", "SimAccount1") || r.AppliesTo("B", "") {
		t.Fatalf("scope matching wrong: %+v", r.Scopes)
	}
	if st := Snapshot()["acct_check"]; !st.Blocked || len(st.Scopes) != 2 {
		t.Fatalf("the snapshot carries who the block is for: %+v", st)
	}
}

func TestM451ErrorAndPanicAreUnscopedBlocks(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	RegisterScoped("errs", func(string, time.Time) (bool, string, []string, error) {
		return false, "", []string{ScopeTrader("A")}, errors.New("boom")
	})
	RegisterScoped("panics", func(string, time.Time) (bool, string, []string, error) { panic("x") })
	RegisterScoped("clear", func(string, time.Time) (bool, string, []string, error) {
		return false, "", []string{ScopeTrader("A")}, nil
	})
	b, rs := Evaluate("MNQ", time.Now())
	if !b || len(rs) != 2 {
		t.Fatalf("%v %+v", b, rs)
	}
	for _, r := range rs {
		if len(r.Scopes) != 0 || !r.AppliesTo("anyone", "") {
			t.Fatalf("a check that could not judge blocks everyone: %+v", r)
		}
	}
}
