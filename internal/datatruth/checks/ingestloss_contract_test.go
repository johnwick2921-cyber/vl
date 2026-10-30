package checks

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLedgerFinalLossBlocksUntilVerified(t *testing.T) {
	l := NewLedger(100)
	now := time.Unix(1_800_000_000, 0)
	l.RecordLoss(LossFinal, "MNQ", "1m", []int64{1000, 2000}, now)
	present := map[int64]bool{}
	verify := func(sym, tf string, ts int64, kind string) (bool, error) { return present[ts], nil }
	b, why := l.Judge("MNQ", verify)
	if !b || !strings.Contains(why, "2 lost bar(s)") {
		t.Fatalf("must block: %v %q", b, why)
	}
	if b, _ := l.Judge("ES", verify); b {
		t.Fatal("another symbol's loss must not block ES")
	}
	present[1000] = true
	if b, _ := l.Judge("MNQ", verify); !b || l.Pending() != 1 {
		t.Fatalf("one still unverified: pending=%d", l.Pending())
	}
	present[2000] = true
	if b, why := l.Judge("MNQ", verify); b {
		t.Fatalf("all verified -> clear: %s", why)
	}
	if l.Pending() != 0 {
		t.Fatal("verified entries must be removed")
	}
}

func TestLedgerVerifyErrorStaysBlocked(t *testing.T) {
	l := NewLedger(10)
	l.RecordLoss(LossHistorical, "MNQ", "5m", []int64{5}, time.Now())
	b, why := l.Judge("MNQ", func(string, string, int64, string) (bool, error) { return true, errors.New("store down") })
	if !b || !strings.Contains(why, "store down") {
		t.Fatalf("cannot-tell must stay blocked: %v %q", b, why)
	}
}

func TestLedgerIntrabarOnlyLogsRate(t *testing.T) {
	l := NewLedger(10)
	now := time.Unix(1_800_000_000, 0)
	for i := 0; i < 20; i++ {
		l.RecordLoss(LossIntrabar, "MNQ", "1m", nil, now)
	}
	if b, _ := l.Judge("MNQ", func(string, string, int64, string) (bool, error) { return true, nil }); b {
		t.Fatal("intrabar drops self-heal and must NOT pause")
	}
	if r := l.IntrabarPerMin(now); r != 4 {
		t.Fatalf("rate = %v, want 4/min", r)
	}
	if r := l.IntrabarPerMin(now.Add(10 * time.Minute)); r != 0 {
		t.Fatalf("rate must age out: %v", r)
	}
}

// The ledger cap overflow is the only debt not tied to a named minute. It is
// kept as a RANGE and clears only when the whole-range verifier says the span is
// whole; with no verifier wired it blocks (fail closed) and cannot clear.
func TestLedgerOverflowIsARangeClearedByWholeVerifier(t *testing.T) {
	l := NewLedger(2)
	now := time.Now()
	l.RecordLoss(LossHistorical, "MNQ", "1m", []int64{1, 2, 3, 4}, now) // 3 and 4 are beyond the cap
	ok := func(string, string, int64, string) (bool, error) { return true, nil }
	b, why := l.Judge("MNQ", ok)
	if !b || !strings.Contains(why, "beyond the ledger cap") || !strings.Contains(why, "no range verifier") {
		t.Fatalf("overflow with no verifier must block: %v %q", b, why)
	}
	whole := false
	var gotFrom, gotTo int64
	l.SetWhole(func(sym, tf string, from, to int64, kind string) (bool, error) {
		gotFrom, gotTo = from, to
		return whole, nil
	})
	if b, why = l.Judge("MNQ", ok); !b || !strings.Contains(why, "not yet verified whole") {
		t.Fatalf("range not whole must block: %v %q", b, why)
	}
	if gotFrom != 3 || gotTo != 4 {
		t.Fatalf("range verified = [%d,%d], want the overflowed span [3,4]", gotFrom, gotTo)
	}
	whole = true
	if b, why = l.Judge("MNQ", ok); b {
		t.Fatalf("range verified whole must clear: %s", why)
	}
	if b, _ = l.Judge("MNQ", ok); b {
		t.Fatal("a cleared range must not come back")
	}
}

// A dropped message that carried no real bar lost no minute: it opens no debt
// (review round 1, D1 - it used to block the symbol until restart).
func TestLedgerRecordsNothingWhenNoBarWasLost(t *testing.T) {
	l := NewLedger(10)
	for _, kind := range []string{LossFinal, LossHistorical} {
		l.RecordLoss(kind, "MNQ", "1m", nil, time.Now())
		l.RecordLoss(kind, "MNQ", "1m", []int64{}, time.Now())
	}
	if b, why := l.Judge("MNQ", func(string, string, int64, string) (bool, error) { return false, nil }); b {
		t.Fatalf("nothing was lost, nothing may block: %s", why)
	}
}

func TestJudgeContract(t *testing.T) {
	cases := []struct {
		name    string
		f       ContractFacts
		blocked bool
	}{
		{"single", ContractFacts{Live: "MNQ 12-26", History: "MNQ 12-26", Window: []string{"MNQ 12-26"}}, false},
		{"case/space", ContractFacts{Live: "mnq  12-26", History: "MNQ 12-26"}, false},
		{"two in window", ContractFacts{Live: "MNQ 03-27", History: "MNQ 03-27", Window: []string{"MNQ 12-26", "MNQ 03-27"}}, true},
		{"live != history", ContractFacts{Live: "MNQ 03-27", History: "MNQ 12-26", Window: []string{"MNQ 03-27"}}, true},
		{"live unknown", ContractFacts{History: "MNQ 12-26"}, true},
		{"history unknown", ContractFacts{Live: "MNQ 12-26"}, true},
		{"nothing known", ContractFacts{}, true},
		{"window differs from live", ContractFacts{Live: "MNQ 03-27", History: "MNQ 03-27", Window: []string{"MNQ 12-26"}}, true},
	}
	for _, c := range cases {
		if b, why := JudgeContract(c.f); b != c.blocked {
			t.Errorf("%s: blocked=%v (%s), want %v", c.name, b, why, c.blocked)
		}
	}
}

// m55r3-h1: the ingest_loss pause text says what lifts it, including the
// spent-budget case (the bot's own NT8 re-request shares a per-session-day
// budget; once spent nothing more is asked). Every owner-facing remedy must work.
// MUTANT (shown RED): drop the `msg += ingestLossRemedy(...)` calls in Judge -> RED, for the
// pending-bar arm AND the overflow arm.
func TestJudgePauseTextNamesWhatClearsIt(t *testing.T) {
	want := []string{"budget", "nothing more is asked", "ages out of the ring", "restart the bot"}
	l := NewLedger(100)
	l.RecordLoss(LossFinal, "MNQ", "1m", []int64{1000}, time.Unix(1_800_000_000, 0))
	b, why := l.Judge("MNQ", func(string, string, int64, string) (bool, error) { return false, nil })
	if !b {
		t.Fatal("must block")
	}
	for _, w := range want {
		if !strings.Contains(why, w) {
			t.Fatalf("the pause text must say %q: %s", w, why)
		}
	}
	// the overflow arm (more bars lost than the ledger holds) carries it too
	l2 := NewLedger(1)
	l2.RecordLoss(LossFinal, "MNQ", "1m", []int64{1000, 2000, 3000}, time.Unix(1_800_000_000, 0))
	l2.SetWhole(func(string, string, int64, int64, string) (bool, error) { return false, nil })
	if b, why := l2.Judge("MNQ", func(string, string, int64, string) (bool, error) { return false, nil }); !b || !strings.Contains(why, "restart the bot") {
		t.Fatalf("overflow arm: %v %q", b, why)
	}
	// a clear verdict says nothing
	if b, why := NewLedger(1).Judge("MNQ", nil); b || why != "" {
		t.Fatalf("empty ledger: %v %q", b, why)
	}
}
